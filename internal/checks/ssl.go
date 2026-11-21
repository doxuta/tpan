package checks

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

// Phép kiểm SSL của M5 (mục 6.3 và 6.7 của docs/KE-HOACH.md): chặng DNS, chặng
// TLS, và hai phép gọi thật vào site qua cổng 80 và 443. Hợp đồng ở ssl_types.go.
// Tên hàm không xuất của file này có tiền tố "ssl" để không đụng file khác.

// sslNow là đồng hồ của phép kiểm hạn chứng chỉ. Là biến để test đặt giờ cố
// định: hạn chứng chỉ là chuyện của ngày tháng, test dùng giờ thật sẽ hỏng
// vào một hôm nào đó mà không ai sửa gì.
var sslNow = time.Now

// sslWarnDays: còn dưới số ngày này là Warn (mục 6.7). certbot gia hạn khi còn
// 30 ngày, nên còn dưới 14 ngày nghĩa là gia hạn tự động đã hỏng ít nhất hai tuần.
const sslWarnDays = 14

// curlTLSFailure là mã thoát 35 của curl: bắt tay TLS thất bại. Với -k thì đây
// là lỗi phía máy chủ (cổng 443 có nghe nhưng không nói TLS, hoặc không có chứng chỉ để đưa ra).
const curlTLSFailure = 35

// getentNotFound là mã thoát của getent khi không có bản ghi: câu trả lời bình
// thường của DNS chứ không phải lỗi chạy lệnh.
const getentNotFound = 2

// sslClean bỏ ký tự điều khiển và cắt ngắn chuỗi lấy từ ngoài (tên trong
// chứng chỉ do CA điền, output của lệnh) trước khi đưa vào Summary.
func sslClean(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	if len(s) > 120 {
		n := 120
		for n > 0 && !utf8.RuneStart(s[n]) { // không cắt giữa một ký tự nhiều byte
			n--
		}
		s = s[:n] + "..."
	}
	return strings.TrimSpace(s)
}

// ---- Chặng DNS ----

// sslGetent chạy `getent <db> <domain>` (db là ahostsv4 hoặc ahostsv6) và trả
// các địa chỉ tìm được, đã bỏ trùng (getent in mỗi địa chỉ ba lần: STREAM,
// DGRAM, RAW). ok=false khi getent thoát bằng mã khác 0 và 2: lỗi chứ không phải "không có bản ghi".
func sslGetent(ctx context.Context, sys system.System, db, dom string) (addrs []netip.Addr, ev Evidence, ok bool, err error) {
	res, ev, err := execCmd(ctx, sys, system.Cmd{Name: "getent", Args: []string{db, dom}})
	if err != nil {
		return nil, ev, false, err
	}
	if res.ExitCode != 0 && res.ExitCode != getentNotFound {
		return nil, ev, false, nil
	}
	for _, ln := range strings.Split(res.Stdout, "\n") {
		f := strings.Fields(ln)
		if len(f) == 0 {
			continue
		}
		a, perr := netip.ParseAddr(f[0])
		// ahostsv6 trả địa chỉ IPv4 dưới dạng ::ffff:a.b.c.d khi tên chỉ có bản ghi A:
		// đó không phải bản ghi AAAA, bỏ đi kẻo coi nhầm là có IPv6.
		if perr != nil || a.Is4In6() || containsAddr(addrs, a) {
			continue
		}
		addrs = append(addrs, a)
	}
	return addrs, ev, true, nil
}

func containsAddr(xs []netip.Addr, a netip.Addr) bool {
	for _, x := range xs {
		if x == a {
			return true
		}
	}
	return false
}

// sslMachineAddrs đọc địa chỉ global của máy từ output của `ip -o addr show
// scope global`, mỗi dòng dạng:
//
//	2: eth0    inet 203.0.113.7/24 brd 203.0.113.255 scope global eth0\       valid_lft forever ...
//	2: eth0    inet6 2001:db8::7/64 scope global \       valid_lft forever ...
func sslMachineAddrs(out string) []netip.Addr {
	var addrs []netip.Addr
	for _, ln := range strings.Split(out, "\n") {
		f := strings.Fields(ln)
		for i := 0; i+1 < len(f); i++ {
			if f[i] != "inet" && f[i] != "inet6" {
				continue
			}
			if p, err := netip.ParsePrefix(f[i+1]); err == nil && !containsAddr(addrs, p.Addr()) {
				addrs = append(addrs, p.Addr())
			}
			break
		}
	}
	return addrs
}

// sslShare cho biết có địa chỉ nào của dns nằm trong mine không.
func sslShare(dns, mine []netip.Addr) bool {
	for _, a := range dns {
		if containsAddr(mine, a) {
			return true
		}
	}
	return false
}

func sslJoinAddrs(as []netip.Addr) string {
	s := make([]string, len(as))
	for i, a := range as {
		s[i] = a.String()
	}
	return strings.Join(s, ", ")
}

// DNSPointsHere kiểm bản ghi A (và AAAA) của domain có trỏ về máy này không.
//
//   - Không có bản ghi A: Fail (Let's Encrypt không tới được máy).
//   - Có A nhưng không địa chỉ nào là của máy: Warn. Máy sau NAT (nhiều VPS đám
//     mây chỉ thấy IP riêng trên card mạng) trông như vậy dù DNS đúng, nên không
//     chặn ở đây: bước thử HTTP của Plan SSL mới là người quyết.
//   - Có AAAA mà không trỏ về máy: Warn. Let's Encrypt ưu tiên IPv6 khi có AAAA,
//     nên một AAAA cũ làm hỏng việc xin chứng chỉ dù A đúng.
func DNSPointsHere(l site.Layout) Check {
	b := siteBase("dns.a", StageDNS, l,
		i18n.T("Bản ghi DNS của tên miền trỏ về máy này", "The domain's DNS records point to this machine"))
	if g := siteGuard(b, l, false); g != nil {
		return g
	}
	dom := l.Domain.String()
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		a4, ev4, ok, err := sslGetent(ctx, sys, "ahostsv4", dom)
		if err != nil {
			return b.cantRun(err, ev4)
		}
		if !ok {
			return b.unknown(i18n.T("getent báo lỗi (mã "+strconv.Itoa(ev4.ExitCode)+")", "getent failed (exit "+strconv.Itoa(ev4.ExitCode)+")"), ev4)
		}
		a6, ev6, ok, err := sslGetent(ctx, sys, "ahostsv6", dom)
		if err != nil {
			return b.cantRun(err, ev4, ev6)
		}
		if !ok {
			return b.unknown(i18n.T("getent báo lỗi (mã "+strconv.Itoa(ev6.ExitCode)+")", "getent failed (exit "+strconv.Itoa(ev6.ExitCode)+")"), ev4, ev6)
		}
		ipRes, evIP, err := execCmd(ctx, sys, system.Cmd{Name: "ip", Args: []string{"-o", "addr", "show", "scope", "global"}})
		if err != nil {
			return b.cantRun(err, ev4, ev6, evIP)
		}

		showIP := line("ip", "-o", "addr", "show", "scope", "global")
		recheck := line("getent", "ahostsv4", dom)
		if len(a4) == 0 {
			// Nói luôn địa chỉ để trỏ về: tpan không sửa được DNS (bản ghi nằm ở
			// nhà cung cấp tên miền), nên câu này là toàn bộ cách sửa.
			target := i18n.T("địa chỉ IP public của máy", "the machine's public IP address")
			if ipRes.ExitCode == 0 {
				var v4 []netip.Addr
				for _, a := range sslMachineAddrs(ipRes.Stdout) {
					if a.Is4() {
						v4 = append(v4, a)
					}
				}
				if len(v4) > 0 {
					ips := sslJoinAddrs(v4)
					target = i18n.T("IP public của máy (card mạng có: "+ips+"; VPS sau NAT thì lấy IP public ở trang quản lý VPS)",
						"the machine's public IP (network cards have: "+ips+"; behind NAT use the public IP from the VPS control panel)")
				}
			}
			return b.bad(Fail,
				i18n.T(dom+" chưa có bản ghi A", dom+" has no A record"),
				i18n.T("Tên miền chưa trỏ về đâu cả, hoặc bản ghi chưa lan tới máy này. tpan không tự sửa được: bản ghi nằm ở nơi quản lý DNS của tên miền. Vào đó tạo bản ghi A cho "+dom+" trỏ về "+target.Vi+", rồi chờ vài phút và bấm kiểm lại.",
					"The domain does not point anywhere yet, or the record has not reached this machine. tpan cannot fix this itself: the record lives in the domain's DNS panel. Create an A record for "+dom+" pointing to "+target.En+", then wait a few minutes and check again."),
				[]string{showIP, recheck}, ev4, ev6, evIP)
		}
		if ipRes.ExitCode != 0 {
			return b.unknown(i18n.T("ip báo lỗi (mã "+strconv.Itoa(ipRes.ExitCode)+"), không biết địa chỉ của máy", "ip failed (exit "+strconv.Itoa(ipRes.ExitCode)+"), the machine addresses are unknown"), ev4, ev6, evIP)
		}
		mine := sslMachineAddrs(ipRes.Stdout)

		switch {
		case !sslShare(a4, mine):
			return b.bad(Warn,
				i18n.T("Bản ghi A của "+dom+" là "+sslJoinAddrs(a4)+", không địa chỉ nào nằm trên máy này", "The A record of "+dom+" is "+sslJoinAddrs(a4)+", none of which is on this machine"),
				i18n.T("Tên miền trỏ về máy khác, hoặc máy này đứng sau NAT (IP public không hiện trên card mạng, thường gặp ở VPS đám mây). Nếu là NAT thì vẫn xin chứng chỉ được; bước thử HTTP sẽ cho câu trả lời thật.",
					"The domain points to another machine, or this machine is behind NAT (the public IP is not on the network card, common on cloud VPSes). If it is NAT you can still get a certificate; the HTTP probe will give the real answer."),
				[]string{showIP, recheck}, ev4, ev6, evIP)
		case len(a6) > 0 && !sslShare(a6, mine):
			return b.bad(Warn,
				i18n.T("Bản ghi AAAA của "+dom+" là "+sslJoinAddrs(a6)+", không phải địa chỉ của máy này", "The AAAA record of "+dom+" is "+sslJoinAddrs(a6)+", which is not an address of this machine"),
				i18n.T("Let's Encrypt ưu tiên IPv6 khi tên miền có bản ghi AAAA, nên xin chứng chỉ sẽ đi vào máy khác và thất bại. Sửa AAAA cho đúng máy này hoặc xoá nó.",
					"Let's Encrypt prefers IPv6 when the domain has an AAAA record, so the certificate request would reach another machine and fail. Fix the AAAA record to this machine or remove it."),
				[]string{showIP, line("getent", "ahostsv6", dom)}, ev4, ev6, evIP)
		}
		return b.pass(i18n.T(dom+" trỏ về máy này (A: "+sslJoinAddrs(a4)+")", dom+" points to this machine (A: "+sslJoinAddrs(a4)+")"), ev4, ev6, evIP)
	})
}

// ---- Chặng TLS ----

// TLSCert đọc chứng chỉ của site và kiểm nó khớp tên miền và còn hạn.
//
// Đọc file bằng Go (ReadFile rồi crypto/x509), không gọi openssl: chạy ở mọi
// lần health mà không cần thêm chương trình, và hạn được tính bằng cùng đồng
// hồ với test. Chỉ chứng chỉ ĐẦU TIÊN của fullchain.pem được xét; các chứng chỉ
// sau là của CA trung gian.
//
// Bằng chứng không có lệnh nào để ghi (không có tiến trình con), nên Command là
// "read <đường dẫn> (tpan)" và Output là thứ Go đọc ra từ file, không phải nội
// dung file. Chữ "(tpan)" như các phép kiểm khác báo đây là việc của chính tpan.
func TLSCert(l site.Layout) Check {
	b := siteBase("tls.cert", StageTLS, l,
		i18n.T("Chứng chỉ SSL có, khớp tên miền và còn hạn", "The SSL certificate exists, matches the domain and is not about to expire"))
	if g := siteGuard(b, l, false, l.CertFullchain()); g != nil {
		return g
	}
	dom, file := l.Domain.String(), l.CertFullchain()
	recheck := line("certbot", "certificates", "--cert-name", dom)
	issue := line("tpan", "ssl", "issue", dom)
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		ev := Evidence{Command: "read " + file + " (tpan)"}
		data, err := sys.ReadFile(file)
		if errors.Is(err, fs.ErrNotExist) {
			ev.ExitCode, ev.Output = 1, "No such file or directory"
			return b.bad(Fail,
				i18n.T("Chưa có chứng chỉ cho "+dom, "There is no certificate for "+dom),
				i18n.T("Chưa xin chứng chỉ cho tên miền này, hoặc file bị xoá. Trình duyệt sẽ báo không an toàn (hoặc không vào được cổng 443).",
					"No certificate has been issued for this domain, or the file was deleted. Browsers will warn it is not secure (or cannot reach port 443)."),
				[]string{line("ls", "-l", l.CertDir()), issue, recheck}, ev)
		}
		if err != nil {
			ev.ExitCode, ev.Output = -1, err.Error()
			return b.unknown(i18n.T("không đọc được "+file+" ("+err.Error()+")", "could not read "+file+" ("+err.Error()+")"), ev)
		}

		cert, perr := sslFirstCert(data)
		if perr != nil {
			ev.ExitCode, ev.Output = 1, perr.Error()
			return b.bad(Fail,
				i18n.T("File "+file+" không đọc được như một chứng chỉ", file+" cannot be read as a certificate"),
				i18n.T("File hỏng hoặc không phải chứng chỉ PEM ("+sslClean(perr.Error())+"). Xin lại chứng chỉ.",
					"The file is damaged or not a PEM certificate ("+sslClean(perr.Error())+"). Issue the certificate again."),
				[]string{line("head", "-n", "3", file), issue, recheck}, ev)
		}

		now := sslNow()
		issuer := cert.Issuer.CommonName
		if issuer == "" {
			issuer = cert.Issuer.String()
		}
		issuer = sslClean(issuer)
		ev.Output = fmt.Sprintf("subject: %s\nSAN: %s\nissuer: %s\nnotBefore: %s\nnotAfter: %s",
			sslClean(cert.Subject.String()), sslClean(strings.Join(cert.DNSNames, ", ")), issuer,
			cert.NotBefore.UTC().Format(time.RFC3339), cert.NotAfter.UTC().Format(time.RFC3339))

		remain := cert.NotAfter.Sub(now)
		days := int(remain / (24 * time.Hour))
		switch {
		case cert.VerifyHostname(dom) != nil:
			return b.bad(Fail,
				i18n.T("Chứng chỉ không dành cho "+dom+" (có: "+sslClean(strings.Join(cert.DNSNames, ", "))+")", "The certificate is not for "+dom+" (it covers: "+sslClean(strings.Join(cert.DNSNames, ", "))+")"),
				i18n.T("File chứng chỉ của "+dom+" lại cấp cho tên khác nên trình duyệt từ chối. Thường do file bị thay hoặc xin nhầm tên.",
					"The certificate file of "+dom+" is issued for other names, so browsers reject it. Usually the file was replaced or requested for the wrong name."),
				[]string{line("openssl", "x509", "-in", file, "-noout", "-ext", "subjectAltName"), issue, recheck}, ev)
		case remain <= 0:
			return b.bad(Fail,
				i18n.T("Chứng chỉ của "+dom+" đã hết hạn từ "+cert.NotAfter.UTC().Format("2006-01-02"), "The certificate of "+dom+" expired on "+cert.NotAfter.UTC().Format("2006-01-02")),
				i18n.T("Gia hạn tự động không chạy được. Trình duyệt chặn trang bằng cảnh báo đỏ. Xem log của certbot để biết vì sao gia hạn thất bại.",
					"Automatic renewal did not work. Browsers block the page with a red warning. Read the certbot log to see why renewal failed."),
				[]string{line("tail", "-n", "30", "/var/log/letsencrypt/letsencrypt.log"), line("certbot", "renew", "--cert-name", dom), recheck}, ev)
		case cert.NotBefore.After(now):
			return b.bad(Fail,
				i18n.T("Chứng chỉ của "+dom+" chưa có hiệu lực (từ "+cert.NotBefore.UTC().Format("2006-01-02")+")", "The certificate of "+dom+" is not valid yet (from "+cert.NotBefore.UTC().Format("2006-01-02")+")"),
				i18n.T("Đồng hồ của máy đang chạy chậm so với thật nên chứng chỉ vừa cấp trông như ở tương lai.",
					"The machine clock is behind real time, so a just-issued certificate looks like it is from the future."),
				[]string{line("timedatectl", "status"), recheck}, ev)
		case remain < sslWarnDays*24*time.Hour:
			return b.bad(Warn,
				i18n.T("Chứng chỉ của "+dom+" chỉ còn "+strconv.Itoa(days)+" ngày", "The certificate of "+dom+" has only "+strconv.Itoa(days)+" days left"),
				i18n.T("certbot gia hạn khi còn 30 ngày, nên còn dưới 14 ngày nghĩa là gia hạn tự động đã không chạy. Xem bộ hẹn giờ certbot và thử gia hạn tay.",
					"certbot renews at 30 days left, so under 14 days means automatic renewal did not run. Check the certbot timer and try renewing by hand."),
				[]string{line("systemctl", "status", "certbot.timer", "--no-pager"), line("certbot", "renew", "--cert-name", dom), recheck}, ev)
		}
		staging := strings.Contains(strings.ToUpper(issuer), "(STAGING)")
		viS := fmt.Sprintf("Chứng chỉ khớp %s, còn %d ngày, do %s cấp", dom, days, issuer)
		enS := fmt.Sprintf("The certificate matches %s, %d days left, issued by %s", dom, days, issuer)
		if staging {
			viS += " (chứng chỉ thử, trình duyệt không tin)"
			enS += " (test certificate, browsers do not trust it)"
		}
		return b.pass(i18n.T(viS, enS), ev)
	})
}

// sslFirstCert đọc chứng chỉ CERTIFICATE đầu tiên trong dữ liệu PEM.
func sslFirstCert(data []byte) (*x509.Certificate, error) {
	for {
		var blk *pem.Block
		blk, data = pem.Decode(data)
		if blk == nil {
			return nil, errors.New("không có khối CERTIFICATE / no CERTIFICATE block")
		}
		if blk.Type != "CERTIFICATE" {
			continue
		}
		c, err := x509.ParseCertificate(blk.Bytes)
		if err != nil {
			return nil, fmt.Errorf("đọc chứng chỉ: %w", err)
		}
		return c, nil
	}
}

// CertRenewDryRun chạy `certbot renew --dry-run` cho đúng chứng chỉ của site:
// certbot làm cả quy trình gia hạn với máy chủ thử của Let's Encrypt nhưng không
// ghi chứng chỉ mới. Đây là cách duy nhất để biết gia hạn tự động SẼ chạy được
// trước khi chứng chỉ sắp hết hạn. Nặng (gọi ra internet, vài chục giây) nên chỉ
// dùng ở kiểm chứng của Plan SSL và e2e, không ở health định kỳ.
//
// server là địa chỉ thư mục ACME (https://...) khi không dùng Let's Encrypt, rỗng
// là để certbot theo file renewal của chứng chỉ. Nó đi vào argv nên phải là URL
// http hoặc https đúng dạng: một chuỗi bắt đầu bằng "-" sẽ thành cờ của certbot.
func CertRenewDryRun(l site.Layout, server string) Check {
	b := siteBase("tls.renew", StageTLS, l,
		i18n.T("Gia hạn thử chứng chỉ (certbot renew --dry-run) thành công", "A trial renewal (certbot renew --dry-run) succeeds"))
	if g := siteGuard(b, l, false); g != nil {
		return g
	}
	dom := l.Domain.String()
	if server != "" {
		u, err := url.Parse(server)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || strings.ContainsAny(server, " \t\r\n") {
			return checkFunc(func(context.Context, system.System) Result {
				return b.unknown(i18n.T("địa chỉ máy chủ ACME không hợp lệ", "the ACME server address is not valid"))
			})
		}
	}
	// --no-random-sleep-on-renew: chạy không có terminal (như từ tpan) thì
	// certbot tự chờ ngẫu nhiên tới 8 phút trước khi gia hạn, kể cả --dry-run,
	// để các bộ hẹn giờ trên mọi máy không gọi Let's Encrypt cùng lúc. Ở đây là
	// lần kiểm do người bấm, chờ thì job (và khoá toàn cục) bị giữ vô ích; đo
	// trên máy ảo: 221 giây (docs/thay-doi/M5.md, mục "Kết quả kiểm cho mục 16").
	args := []string{"renew", "--dry-run", "--no-random-sleep-on-renew", "--cert-name", dom, "--non-interactive"}
	if server != "" {
		args = append(args, "--server", server)
	}
	cmd := system.Cmd{Name: "certbot", Args: args}
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		res, ev, err := execCmd(ctx, sys, cmd)
		if err != nil {
			return b.cantRun(err, ev)
		}
		if res.ExitCode == 0 {
			return b.pass(i18n.T("certbot gia hạn thử "+dom+" thành công", "certbot's trial renewal of "+dom+" succeeded"), ev)
		}
		why := sslClean(firstLineWith(joinOutput(res), "Error", "error", "failed", "Failed"))
		return b.bad(Fail,
			i18n.T("Gia hạn thử "+dom+" thất bại (mã "+strconv.Itoa(res.ExitCode)+": "+why+")", "Trial renewal of "+dom+" failed (exit "+strconv.Itoa(res.ExitCode)+": "+why+")"),
			i18n.T("Gia hạn thật sẽ thất bại cùng cách này. Nguyên nhân hay gặp: đường /.well-known/acme-challenge/ không tới được từ ngoài (web server, tường lửa, DNS), hoặc đã hết hạn mức của Let's Encrypt.",
				"A real renewal would fail the same way. Common causes: the /.well-known/acme-challenge/ path is not reachable from outside (web server, firewall, DNS), or the Let's Encrypt rate limit is used up."),
			[]string{line("tail", "-n", "50", "/var/log/letsencrypt/letsencrypt.log"), cmd.String()}, ev)
	})
}

// ---- Gọi thật qua cổng 80 và 443 ----

// sslCurlProblem xử lý các mã thoát của curl mà HTTPRedirect và HTTPSSite
// chung: không nối được, hết giờ, máy chủ đóng kết nối, bắt tay TLS hỏng, hoặc
// curl lỗi khác. handled=false khi curl đã nhận được phản hồi HTTP (người gọi xử lý tiếp).
func sslCurlProblem(b base, l site.Layout, res system.Result, ev Evidence, port, recheck string) (r Result, handled bool) {
	dom := l.Domain.String()
	switch res.ExitCode {
	case 0:
		return Result{}, false
	case curlConnectFailed, curlTimeout:
		vi, en := "không kết nối được tới 127.0.0.1 cổng "+port, "cannot connect to 127.0.0.1 port "+port
		causeVi, causeEn := "Web server không chạy hoặc không nghe cổng "+port+".", "The web server is not running or does not listen on port "+port+"."
		if res.ExitCode == curlTimeout {
			vi, en = "web server không trả lời trong 15 giây", "the web server did not answer within 15 seconds"
			causeVi, causeEn = "Web server hoặc PHP treo hay quá tải.", "The web server or PHP is hung or overloaded."
		}
		return b.bad(Fail, i18n.T("Lỗi: "+vi, "Failed: "+en), i18n.T(causeVi, causeEn),
			[]string{line("systemctl", "status", "nginx", "apache2", "--no-pager"), line("ss", "-tlnp"), line("tail", "-n", "20", l.LogDir+"/error.log"), recheck}, ev), true
	case curlEmptyReply, curlRecvFailure:
		return b.bad(Fail,
			i18n.T("Web server đóng kết nối không trả gì cho "+dom, "The web server closed the connection without replying for "+dom),
			i18n.T("Request mang tên "+dom+" rơi vào site mặc định (nginx đóng kết nối, mã 444): vhost của site chưa được bật hoặc chưa được nạp.",
				"The request for "+dom+" fell into the default site (nginx drops the connection, code 444): the site vhost is not enabled or not loaded."),
			siteVhostFix(dom, recheck), ev), true
	case curlTLSFailure:
		return b.bad(Fail,
			i18n.T("Bắt tay TLS với cổng 443 thất bại", "The TLS handshake with port 443 failed"),
			i18n.T("Cổng 443 có nghe nhưng không phục vụ TLS cho "+dom+": vhost 443 chưa có hoặc trỏ tới chứng chỉ hỏng.",
				"Port 443 is open but does not serve TLS for "+dom+": the 443 vhost is missing or points to a broken certificate."),
			[]string{line("ss", "-tlnp"), line("tail", "-n", "20", l.LogDir+"/error.log"), line("tpan", "ssl", "issue", dom), recheck}, ev), true
	}
	return b.unknown(i18n.T("curl báo lỗi (mã "+strconv.Itoa(res.ExitCode)+": "+sslClean(firstLineWith(res.Stderr, "curl"))+")", "curl failed (exit "+strconv.Itoa(res.ExitCode)+": "+sslClean(firstLineWith(res.Stderr, "curl"))+")"), ev), true
}

// sslLocation lấy giá trị đầu tiên của header Location trong phần header curl in ra.
func sslLocation(headers string) string {
	for _, ln := range strings.Split(headers, "\n") {
		ln = strings.TrimSpace(ln)
		if len(ln) > 9 && strings.EqualFold(ln[:9], "location:") {
			return strings.TrimSpace(ln[9:])
		}
	}
	return ""
}

// HTTPRedirect kiểm cổng 80 của site đã có SSL trả 301 sang https://<domain>/.
// Chỉ nhìn header, không theo chuyển hướng (không có -L): mã 302 hay 307 cũng là
// lỗi (trình duyệt không nhớ chuyển hướng tạm, và công cụ SEO coi là chưa chuyển hẳn).
func HTTPRedirect(l site.Layout) Check {
	b := siteBase("http.redirect", StageWeb, l,
		i18n.T("Cổng 80 chuyển hướng 301 sang https", "Port 80 redirects with 301 to https"))
	if g := siteGuard(b, l, false, l.LogDir); g != nil {
		return g
	}
	dom := l.Domain.String()
	target := "http://" + dom + "/"
	resolve := dom + ":80:127.0.0.1"
	// -D - đưa header ra stdout, -o /dev/null bỏ thân; nhãn mã ở cuối như HTTPSite.
	cmd := system.Cmd{Name: "curl", Args: []string{"-sS", "--max-time", "15", "--resolve", resolve, "-o", "/dev/null", "-D", "-", "-w", siteHTTPMarker + "%{http_code}", target}}
	recheck := line("curl", "-sS", "--max-time", "15", "--resolve", resolve, "-o", "/dev/null", "-D", "-", target)
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		res, ev, err := execCmd(ctx, sys, cmd)
		if err != nil {
			return b.cantRun(err, ev)
		}
		headers, code := siteSplitCode(res.Stdout)
		ev.Output = siteHTTPEvidence(code, headers, res.Stderr)
		if r, handled := sslCurlProblem(b, l, res, ev, "80", recheck); handled {
			return r
		}
		if code == "" {
			return b.unknown(i18n.T("curl không in mã trạng thái", "curl printed no status code"), ev)
		}
		loc := sslLocation(headers)
		want := "https://" + dom + "/"
		fix := []string{line("tpan", "ssl", "issue", dom), recheck}
		switch {
		case code == "301" && strings.HasPrefix(strings.ToLower(loc), want):
			return b.pass(i18n.T("Cổng 80 chuyển hướng 301 tới "+sslClean(loc), "Port 80 redirects with 301 to "+sslClean(loc)), ev)
		case code == "301":
			return b.bad(Fail,
				i18n.T("Cổng 80 chuyển hướng 301 nhưng tới \""+sslClean(loc)+"\", không phải "+want, "Port 80 redirects with 301 but to \""+sslClean(loc)+"\", not "+want),
				i18n.T("Vhost chuyển hướng sai đích (sang http, sang tên khác, hoặc thiếu Location). Ghi lại vhost từ template của tpan.",
					"The vhost redirects to the wrong target (to http, to another name, or no Location). Rewrite the vhost from the tpan template."), fix, ev)
		case code == "302" || code == "303" || code == "307" || code == "308":
			return b.bad(Fail,
				i18n.T("Cổng 80 chuyển hướng bằng mã "+code+", cần 301", "Port 80 redirects with code "+code+", it must be 301"),
				i18n.T("Chuyển hướng tạm không được trình duyệt và công cụ tìm kiếm ghi nhớ. Thường do plugin WordPress hay vhost sửa tay.",
					"A temporary redirect is not remembered by browsers and search engines. Usually a WordPress plugin or a hand-edited vhost."), fix, ev)
		}
		return b.bad(Fail,
			i18n.T("Cổng 80 trả "+code+" thay vì chuyển hướng sang https", "Port 80 answered "+code+" instead of redirecting to https"),
			i18n.T("Vhost cổng 80 chưa có luật chuyển hướng: khách vào bằng http vẫn thấy bản không mã hoá. Ghi lại vhost bản có SSL.",
				"The port 80 vhost has no redirect rule: visitors using http still get the unencrypted version. Rewrite the vhost with SSL."), fix, ev)
	})
}

// sslDomainRE khớp domain đứng riêng như một tên máy trong trang. Cùng luật với
// HTTPSite (site.go) vì luật đó nằm cứng trong thân HTTPSite chứ không có hàm riêng
// để gọi; nếu sửa một nơi phải sửa cả nơi kia.
func sslDomainRE(dom string) *regexp.Regexp {
	return regexp.MustCompile(`(^|[^a-z0-9.-])` + regexp.QuoteMeta(dom) + `($|[^a-z0-9.-]|\.($|[^a-z0-9-]))`)
}

// HTTPSSite là HTTPSite cho site đã có SSL: https, cổng 443, nối thẳng vào
// 127.0.0.1. Có -k vì chứng chỉ staging không được tin; chứng chỉ đã có phép
// kiểm riêng (TLSCert) nên ở đây chỉ hỏi "web server có phục vụ đúng site qua TLS không".
func HTTPSSite(l site.Layout) Check {
	b := siteBase("https.site", StageEndToEnd, l,
		i18n.T("Gọi HTTPS vào site trả 200 và đúng là WordPress của site", "An HTTPS request to the site returns 200 and is the site's WordPress"))
	if g := siteGuard(b, l, true, l.LogDir, l.PHPErrorLog, l.Public); g != nil {
		return g
	}
	dom := l.Domain.String()
	domRE := sslDomainRE(dom)
	target := "https://" + dom + "/"
	resolve := dom + ":443:127.0.0.1"
	cmd := system.Cmd{Name: "curl", Args: []string{"-sS", "-k", "--max-time", "15", "--resolve", resolve, "-w", siteHTTPMarker + "%{http_code}", target}}
	recheck := line("curl", "-sS", "-k", "--max-time", "15", "--resolve", resolve, "-o", "/dev/null", "-w", "%{http_code}\n", target)
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		res, ev, err := execCmd(ctx, sys, cmd)
		if err != nil {
			return b.cantRun(err, ev)
		}
		body, code := siteSplitCode(res.Stdout)
		ev.Output = siteHTTPEvidence(code, body, res.Stderr)
		if r, handled := sslCurlProblem(b, l, res, ev, "443", recheck); handled {
			return r
		}
		if code == "" {
			return b.unknown(i18n.T("curl không in mã trạng thái", "curl printed no status code"), ev)
		}

		switch code {
		case "200":
			low := strings.ToLower(body)
			isWP := false
			for _, s := range siteWPSigns {
				if strings.Contains(low, s) {
					isWP = true
					break
				}
			}
			switch {
			case !isWP:
				return b.bad(Fail,
					i18n.T("Trả 200 qua https nhưng trang không phải WordPress", "Answered 200 over https but the page is not WordPress"),
					i18n.T("Có thứ khác trả lời cho "+dom+" ở cổng 443: trang mặc định của web server, một vhost khác, hoặc WordPress chưa được cài vào public.",
						"Something else answers for "+dom+" on port 443: the web server default page, another vhost, or WordPress is not installed in public."),
					[]string{line("ls", "-l", l.Public), line("curl", "-sS", "-k", "--max-time", "15", "--resolve", resolve, target), recheck}, ev)
			case !domRE.MatchString(low):
				return b.bad(Fail,
					i18n.T("Trả 200 và là WordPress nhưng trang không nhắc tới "+dom+": có thể là site khác", "Answered 200 and it is WordPress, but the page never mentions "+dom+": it may be another site"),
					i18n.T("Request tới "+dom+" ở cổng 443 có vẻ rơi nhầm vào vhost của site khác, hoặc địa chỉ site (home/siteurl) trong WordPress chưa đổi sang https://"+dom+".",
						"The request for "+dom+" on port 443 seems to have landed in another site's vhost, or the site address (home/siteurl) in WordPress is not set to https://"+dom+"."),
					[]string{line("grep", "-Rl", dom, "/etc/nginx/sites-enabled", "/etc/apache2/sites-enabled"),
						siteRunAsOn(sys, l, "wp", "option", "get", "home", "--path="+l.Public), recheck}, ev)
			}
			return b.pass(i18n.T("Site trả 200 qua https, đúng là WordPress của "+dom, "The site answers 200 over https and is the WordPress of "+dom), ev)
		case "502", "503", "504":
			return b.bad(Fail,
				i18n.T("Site trả "+code+" qua https: web server không nói chuyện được với PHP", "The site answered "+code+" over https: the web server cannot talk to PHP"),
				i18n.T("Pool PHP-FPM của site không chạy, socket sai chủ hay quyền, hoặc pool quá tải (504).",
					"The site's PHP-FPM pool is not running, the socket has the wrong owner or mode, or the pool is overloaded (504)."),
				[]string{line("ls", "-l", "/run/php"), line("systemctl", "status", "php*-fpm.service", "--no-pager"), line("tail", "-n", "20", l.LogDir+"/error.log"), recheck}, ev)
		case "500":
			return b.bad(Fail,
				i18n.T("Site trả 500 qua https: PHP hoặc WordPress báo lỗi", "The site answered 500 over https: PHP or WordPress reported an error"),
				i18n.T("Thường là lỗi PHP trong plugin hay theme, hoặc WordPress không vào được DB (\"Error establishing a database connection\").",
					"Usually a PHP error in a plugin or theme, or WordPress cannot reach the DB (\"Error establishing a database connection\")."),
				[]string{line("tail", "-n", "20", l.PHPErrorLog), line("tail", "-n", "20", l.LogDir+"/error.log"),
					siteRunAsOn(sys, l, "wp", "db", "check", "--path="+l.Public), recheck}, ev)
		case "301", "302", "303", "307", "308":
			return b.bad(Fail,
				i18n.T("Site trả "+code+" qua https, đáng lẽ 200", "The site answered "+code+" over https, it should be 200"),
				i18n.T("Trang https lại chuyển đi chỗ khác. Thường là WordPress vẫn đặt địa chỉ site (home/siteurl) là http nên nó chuyển https về http, còn web server chuyển http về https: vòng lặp chuyển hướng.",
					"The https page redirects elsewhere. Usually WordPress still has the site address (home/siteurl) set to http, so it sends https back to http while the web server sends http to https: a redirect loop."),
				[]string{siteRunAsOn(sys, l, "wp", "option", "get", "home", "--path="+l.Public),
					siteRunAsOn(sys, l, "wp", "option", "update", "home", "https://"+dom, "--path="+l.Public),
					siteRunAsOn(sys, l, "wp", "option", "update", "siteurl", "https://"+dom, "--path="+l.Public), recheck}, ev)
		}
		return b.bad(Fail,
			i18n.T("Site trả "+code+" qua https, đáng lẽ 200", "The site answered "+code+" over https, it should be 200"),
			i18n.T("Request https không tới được trang chủ WordPress: bị chặn (403) hoặc không tìm thấy (404). Xem vhost 443 và địa chỉ site trong WordPress.",
				"The https request does not reach the WordPress home page: a block (403) or not found (404). Check the 443 vhost and the site address in WordPress."),
			[]string{siteRunAsOn(sys, l, "wp", "option", "get", "home", "--path="+l.Public), line("ls", "-l", l.Public), recheck}, ev)
	})
}

// ---- Gom lại ----

// SiteChecksSSL là SiteChecks cho site đã có chứng chỉ, theo thứ tự chặng của
// request: TLS rồi tới web, pool, WordPress, DB, rồi gọi thật qua cổng 80 (phải
// là 301) và 443. HTTPSite bị thay vì cổng 80 giờ chỉ còn việc chuyển hướng.
// phpVersion là bản PHP site đang dùng (chọn socket của pool).
func SiteChecksSSL(web, phpVersion string, l site.Layout) []Check {
	return []Check{
		TLSCert(l),
		VhostEnabled(web, l),
		PoolSocket(l, phpVersion),
		WPChecksums(l),
		WPConfigPerm(l),
		DBAccess(l),
		HTTPRedirect(l),
		HTTPSSite(l),
	}
}
