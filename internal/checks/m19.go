package checks

// Phép kiểm của M19 (docs/thay-doi/M19-thiet-ke.md mục 3.8): tên phụ của site.
//
//   - site.alias.dns:<tên>   DNS của tên phụ trỏ về máy (chặng DNS).
//   - site.alias.http:<tên>  tên phụ trả lời đúng kiểu qua 127.0.0.1 (đầu cuối).
//   - ssl.names:<tên chính>  bộ tên trong chứng chỉ khớp bộ tên của site (chặng TLS).
//   - names.root.shared      không tên miền gốc nào có tên của hai chủ (cấp máy).
//
// Bất biến B4: ba phép đầu CHỈ cảnh báo, không bao giờ Fail. Thêm một tên phụ
// chưa trỏ DNS hay chưa xin chứng chỉ là việc còn phải làm, không phải sự cố;
// Fail mở sự cố và làm `tpan health` thoát 1.
//
// Gói steps import gói này (harden_m14.go) nên ở đây không import steps được:
// luật đạt của AliasHTTP chép lại luật của steps.NameProbe (S2), và tên trong
// chứng chỉ đọc bằng sslFirstCert thay vì steps.CertNames. Hai chỗ phải giữ
// khớp nhau; đổi luật ở một nơi thì đổi nơi kia.

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// NamedSite là một site cùng các tên phụ active của nó, đầu vào của
// ServerNameUnique. Tên chính lấy từ Layout.Domain; Names chỉ cần các tên
// phụ (có kèm tên chính cũng được, tên trùng được bỏ).
type NamedSite struct {
	Layout site.Layout
	Names  []validate.Domain
}

// allNames trả tên chính rồi các tên phụ, không trùng, theo thứ tự.
func (n NamedSite) allNames() []string {
	out := []string{n.Layout.Domain.String()}
	for _, d := range n.Names {
		if s := d.String(); !d.IsZero() && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

// aliasDNSTimeout là hạn tra DNS cho mỗi tên (S18): một resolver chậm không
// được kéo cả lượt health dài ra theo số tên (tối đa 24 tên phụ mỗi site).
const aliasDNSTimeout = 2 * time.Second

// ---- site.alias.dns ----

// AliasDNS kiểm bản ghi A (và AAAA) của tên phụ name có trỏ về máy này không,
// cùng thân với DNSPointsHere (dnsPointsHere). Bọc Soft: thiếu bản ghi cũng
// chỉ là Warn. Mỗi lần chạy có hạn aliasDNSTimeout; quá hạn thì Unknown.
func AliasDNS(l site.Layout, name validate.Domain) Check {
	b := base{id: "site.alias.dns:" + name.String(), stage: StageDNS,
		title: i18n.T("Bản ghi DNS của tên phụ trỏ về máy này", "The alias domain's DNS records point to this machine")}
	if l.Domain.IsZero() || name.IsZero() {
		return checkFunc(func(context.Context, system.System) Result {
			return b.unknown(i18n.T("thiếu tên site hoặc tên phụ", "the site or the alias name is missing"))
		})
	}
	inner := dnsPointsHere(b, name.String())
	return Soft(checkFunc(func(ctx context.Context, sys system.System) Result {
		ctx, cancel := context.WithTimeout(ctx, aliasDNSTimeout)
		defer cancel()
		return inner.Run(ctx, sys)
	}))
}

// ---- site.alias.http ----

// Nhãn của -w: như steps.nameProbe*. Đứng sau thân, mỗi nhãn sau một ký tự
// xuống dòng; tìm từ cuối lên nên thân hay header do site đặt không đánh lừa
// được. Giá trị header không chứa được xuống dòng.
const (
	aliasLocLabel  = "\nTPAN_LOCATION="
	aliasXRBLabel  = "\nTPAN_REDIRECT_BY="
	aliasCodeLabel = "\nTPAN_HTTP_CODE="
	// aliasBodyMax: thân do PHP của site sinh, chỉ đọc đủ để tìm dấu hiệu WordPress.
	aliasBodyMax = 64 << 10
	aliasHdrMax  = 2 << 10
)

// aliasWPSigns như steps.nameProbeWPSigns: dấu hiệu trang là WordPress.
var aliasWPSigns = []string{"wp-content", "wp-includes", `content="wordpress`}

type aliasAnswer struct{ code, location, redirectBy, body string }

// aliasSplit tách stdout của aliasCurl: <thân>\nTPAN_LOCATION=..\n
// TPAN_REDIRECT_BY=..\nTPAN_HTTP_CODE=<mã>. Thiếu nhãn thì mã rỗng.
func aliasSplit(out string) aliasAnswer {
	cut := func(label string) (string, bool) {
		i := strings.LastIndex(out, label)
		if i < 0 {
			return "", false
		}
		v := out[i+len(label):]
		out = out[:i]
		return strings.TrimRight(v, "\r"), true
	}
	code, ok1 := cut(aliasCodeLabel)
	xrb, ok2 := cut(aliasXRBLabel)
	loc, ok3 := cut(aliasLocLabel)
	if len(out) > aliasBodyMax {
		out = out[:aliasBodyMax]
	}
	if !ok1 || !ok2 || !ok3 {
		return aliasAnswer{body: out}
	}
	return aliasAnswer{code: strings.TrimSpace(code), redirectBy: aliasHeader(xrb), location: aliasHeader(loc), body: out}
}

// aliasHeader cắt ngắn và bỏ ký tự điều khiển: Location do site đặt, đi vào Summary.
func aliasHeader(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > aliasHdrMax {
		s = s[:aliasHdrMax]
	}
	return sslClean(s)
}

// aliasCurl gọi <scheme>://<host>/ về 127.0.0.1, không theo chuyển hướng
// (như steps.nameCurl).
func aliasCurl(host string, https bool) system.Cmd {
	scheme, port := "http", "80"
	args := []string{"-sS", "--max-time", "10"}
	if https {
		scheme, port = "https", "443"
		args = append(args, "-k") // kiểm tên, không kiểm CA (chứng chỉ có thể do máy ACME thử cấp)
	}
	args = append(args, "--resolve", host+":"+port+":127.0.0.1", "-r", "0-65535",
		"-w", `\nTPAN_LOCATION=%header{location}\nTPAN_REDIRECT_BY=%header{x-redirect-by}\nTPAN_HTTP_CODE=%{http_code}`,
		scheme+"://"+host+"/")
	return system.Cmd{Name: "curl", Args: args}
}

// aliasJudge áp luật đạt của steps.NameProbe.plan lên câu trả lời a. Trả lý do
// (rỗng khi đạt).
func aliasJudge(a aliasAnswer, mode site.NameMode, redirectTo string) string {
	if redirectTo != "" || mode == site.ModeRedirect {
		switch {
		case a.code != "301":
			return fmt.Sprintf("mong 301 về %s, nhận mã %q", redirectTo, a.code)
		case a.location != redirectTo:
			return fmt.Sprintf("mong 301 về %s, nhận Location %q", redirectTo, a.location)
		case a.redirectBy != "":
			return fmt.Sprintf("mong 301 do vhost trả, nhận X-Redirect-By %q (trang của site, không phải khối chuyển tên phụ)", a.redirectBy)
		}
		return ""
	}
	switch a.code {
	case "200", "206":
		low := strings.ToLower(a.body)
		for _, sign := range aliasWPSigns {
			if strings.Contains(low, sign) {
				return ""
			}
		}
		return "trả " + a.code + " nhưng trang không phải WordPress (vhost khác hay trang mặc định đang trả lời cho tên này)"
	case "301", "302":
		if strings.EqualFold(a.redirectBy, "wordpress") {
			return ""
		}
		return fmt.Sprintf("trả %s về %q mà không có X-Redirect-By: WordPress", a.code, a.location)
	}
	return fmt.Sprintf("mong 200 với trang WordPress, nhận mã %q", a.code)
}

// AliasHTTP gọi thật tên phụ host về 127.0.0.1 và kiểm nó trả lời đúng kiểu
// mode, theo cùng luật với bước steps.NameProbe lúc thêm tên (S2). Site SSL
// mà tên chưa có trong chứng chỉ (covered) thì gọi cổng 80 và mong 301 về
// https://<tên chính>/, bất kể kiểu. Bọc Soft: chỉ Warn.
//
// Một lần gọi, không thử lại: health chạy lại sau.
func AliasHTTP(l site.Layout, host validate.Domain, mode site.NameMode, ssl bool, covered []validate.Domain) Check {
	b := base{id: "site.alias.http:" + host.String(), stage: StageEndToEnd,
		title: i18n.T("Tên phụ trả lời đúng kiểu khi gọi vào web server", "The alias domain answers as configured when called on the web server")}
	bad := func(why i18n.Text) Check {
		return checkFunc(func(context.Context, system.System) Result { return b.unknown(why) })
	}
	switch {
	case l.Domain.IsZero() || host.IsZero():
		return bad(i18n.T("thiếu tên site hoặc tên phụ", "the site or the alias name is missing"))
	case mode != site.ModeServe && mode != site.ModeRedirect:
		return bad(i18n.T("kiểu của tên phụ phải là serve hay redirect (nhận được \""+string(mode)+"\")", "the alias mode must be serve or redirect (got \""+string(mode)+"\")"))
	}
	name, primary := host.String(), l.Domain.String()
	https := ssl && slices.Contains(covered, host)
	redirectTo := ""
	scheme := "http"
	if ssl {
		scheme = "https"
		if !https {
			// Tên chưa có trong chứng chỉ không bao giờ nằm ở 443 (B4): cổng 80
			// chuyển về tên chính.
			redirectTo = "https://" + primary + "/"
		}
	}
	if redirectTo == "" && mode == site.ModeRedirect {
		redirectTo = scheme + "://" + primary + "/"
	}
	cmd := aliasCurl(name, https)
	port := "80"
	recheckArgs := []string{"-sS", "--max-time", "10"}
	if https {
		port = "443"
		recheckArgs = append(recheckArgs, "-k")
	}
	recheck := line("curl", append(recheckArgs, "--resolve", name+":"+port+":127.0.0.1", "-o", "/dev/null", "-w", "%{http_code} %{redirect_url}\n", cmd.Args[len(cmd.Args)-1])...)
	fix := []string{line("sudo", "tpan", "site", "alias", "set", primary, name, "--mode", string(mode))}

	return Soft(checkFunc(func(ctx context.Context, sys system.System) Result {
		res, ev, err := execCmd(ctx, sys, cmd)
		if err != nil {
			return b.cantRun(err, ev)
		}
		switch {
		case res.ExitCode == curlEmptyReply || res.ExitCode == curlRecvFailure || (https && res.ExitCode == curlTLSFailure):
			return b.bad(Fail,
				i18n.T("Web server không trả lời cho "+name+" (đóng kết nối hay từ chối bắt tay)", "The web server does not answer for "+name+" (connection closed or handshake refused)"),
				i18n.T("Request mang tên "+name+" rơi vào vhost mặc định: vhost của site chưa khai tên này, hoặc chưa được nạp.", "A request for "+name+" fell into the default vhost: the site's vhost does not declare this name, or it is not loaded."),
				append(fix, recheck), ev)
		case res.ExitCode != 0:
			return b.unknown(i18n.T("curl báo lỗi (mã "+strconv.Itoa(res.ExitCode)+": "+sslClean(firstLineWith(res.Stderr, "curl"))+")", "curl failed (exit "+strconv.Itoa(res.ExitCode)+": "+sslClean(firstLineWith(res.Stderr, "curl"))+")"), ev)
		}
		a := aliasSplit(res.Stdout)
		ev.Output = clip("HTTP " + a.code + "\nLocation: " + a.location + "\nX-Redirect-By: " + a.redirectBy)
		if a.code == "" {
			return b.unknown(i18n.T("curl không in mã trả lời", "curl printed no status code"), ev)
		}
		if why := aliasJudge(a, mode, redirectTo); why != "" {
			return b.bad(Fail,
				i18n.T(name+" trả lời sai kiểu: "+why, name+" answers the wrong way: "+why),
				i18n.T("Vhost của site không phục vụ tên này như đã khai (kiểu "+string(mode)+"), hoặc một vhost khác đang giữ tên. Vẽ lại vhost bằng lệnh đầu, rồi kiểm lại.",
					"The site's vhost does not serve this name as declared (mode "+string(mode)+"), or another vhost holds the name. Redraw the vhost with the first command, then check again."),
				append(fix, recheck), ev)
		}
		return b.pass(i18n.T(name+" trả lời đúng kiểu "+string(mode), name+" answers as the "+string(mode)+" mode requires"), ev)
	}))
}

// ---- ssl.names ----

// Kết quả tra DNS của một tên trong SSLNames.
const (
	dnsHere      = iota // trỏ về máy (Pass)
	dnsNone             // không có bản ghi A (Fail)
	dnsElsewhere        // A hay AAAA không phải của máy (Warn): cũng là NAT
	dnsUnsure           // không tra được
)

// sslNamesList nối tối đa 5 tên cho Summary.
func sslNamesList(xs []string) string {
	if len(xs) > 5 {
		return strings.Join(xs[:5], ", ") + fmt.Sprintf(" và %d tên khác / and %d more", len(xs)-5, len(xs)-5)
	}
	return strings.Join(xs, ", ")
}

// SSLNames kiểm bộ tên trong chứng chỉ của site khớp bộ tên của site (names:
// tên chính và mọi tên phụ active):
//
//   - tên của site mà DNS trỏ về máy nhưng chưa có trong chứng chỉ: chưa được
//     phủ (chạy `tpan ssl issue`);
//   - tên trong chứng chỉ mà site không còn: lần gia hạn của CẢ site sẽ xin lại
//     tên đó và hỏng;
//   - tên trong chứng chỉ vẫn là của site nhưng DNS đã đi chỗ khác: gia hạn hỏng.
//
// Chỉ Pass, Warn hay Unknown. DNS Warn của một tên (A không phải của máy) chỉ
// tính là "đi chỗ khác" khi chính tên chính trỏ về máy: máy sau NAT thì mọi tên
// đều Warn mà DNS vẫn đúng. Tên chính không xét ở đây (dns.a và tls.cert xét).
// Không tra DNS nếu không có tên nào cần tra: site chưa có tên phụ không tốn lệnh.
func SSLNames(l site.Layout, names []validate.Domain) Check {
	b := siteBase("ssl.names", StageTLS, l,
		i18n.T("Chứng chỉ SSL phủ đúng các tên của site", "The SSL certificate covers the site's names"))
	if g := siteGuard(b, l, false, l.CertFullchain()); g != nil {
		return g
	}
	primary, file := l.Domain.String(), l.CertFullchain()
	want := []string{primary}
	for _, d := range names {
		if s := d.String(); !d.IsZero() && !slices.Contains(want, s) {
			want = append(want, s)
		}
	}
	recheck := line("openssl", "x509", "-in", file, "-noout", "-ext", "subjectAltName")
	issue := line("sudo", "tpan", "ssl", "issue", primary)

	return checkFunc(func(ctx context.Context, sys system.System) Result {
		ev := Evidence{Command: "read " + file + " (tpan)"}
		data, err := sys.ReadFile(file)
		if err != nil {
			ev.ExitCode, ev.Output = -1, err.Error()
			if errors.Is(err, fs.ErrNotExist) {
				ev.ExitCode, ev.Output = 1, "No such file or directory"
				return b.unknown(i18n.T("chưa có chứng chỉ để đối chiếu (tls.cert báo việc này)", "there is no certificate to compare (tls.cert reports that)"), ev)
			}
			return b.unknown(i18n.T("không đọc được "+file+" ("+sslClean(err.Error())+")", "could not read "+file+" ("+sslClean(err.Error())+")"), ev)
		}
		cert, perr := sslFirstCert(data)
		if perr != nil {
			ev.ExitCode, ev.Output = 1, perr.Error()
			return b.unknown(i18n.T("File "+file+" không đọc được như một chứng chỉ (tls.cert báo việc này)", "The file "+file+" cannot be read as a certificate (tls.cert reports that)"), ev)
		}
		var san []string
		for _, n := range cert.DNSNames {
			if n = strings.ToLower(n); !slices.Contains(san, n) {
				san = append(san, n)
			}
		}

		// Tra DNS một tên trong hạn aliasDNSTimeout. Bản thân phép dnsPointsHere
		// dùng lại để mọi quy tắc "trỏ về máy" nằm một chỗ.
		lookup := func(name string) int {
			dctx, cancel := context.WithTimeout(ctx, aliasDNSTimeout)
			defer cancel()
			switch dnsPointsHere(base{id: "ssl.names.dns:" + name, stage: StageDNS, title: b.title}, name).Run(dctx, sys).Status {
			case Pass:
				return dnsHere
			case Fail:
				return dnsNone
			case Warn:
				return dnsElsewhere
			}
			return dnsUnsure
		}
		states := map[string]string{} // tên -> kết quả tra, làm bằng chứng
		note := func(name string, st int) {
			states[name] = [...]string{"points here", "no A record", "points elsewhere", "unknown"}[st]
		}

		var missing, stale, gone []string
		for _, n := range want[1:] {
			if slices.Contains(san, n) {
				continue
			}
			st := lookup(n)
			note(n, st)
			if st == dnsHere {
				missing = append(missing, n)
			}
		}
		var primaryHere, primaryDone bool
		for _, n := range san {
			if n == primary {
				continue
			}
			if !slices.Contains(want, n) {
				stale = append(stale, n)
				continue
			}
			st := lookup(n)
			note(n, st)
			switch st {
			case dnsElsewhere:
				if !primaryDone {
					primaryHere, primaryDone = lookup(primary) == dnsHere, true
				}
				if primaryHere {
					gone = append(gone, n)
				}
			case dnsNone:
				gone = append(gone, n)
			}
		}

		var sb strings.Builder
		sb.WriteString("SAN: " + sslClean(strings.Join(san, ", ")))
		keys := make([]string, 0, len(states))
		for k := range states {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			sb.WriteString("\ndns " + sslClean(k) + ": " + states[k])
		}
		ev.Output = clip(sb.String())

		if len(missing)+len(stale)+len(gone) == 0 {
			return b.pass(i18n.T(fmt.Sprintf("Chứng chỉ phủ đủ %d tên của site và không thừa tên nào", len(want)),
				fmt.Sprintf("The certificate covers all %d names of the site and has no extra name", len(want))), ev)
		}
		var vi, en []string
		var fix []string
		if len(missing) > 0 {
			vi = append(vi, "tên chưa có trong chứng chỉ dù DNS đã trỏ về máy: "+sslNamesList(missing))
			en = append(en, "names not in the certificate although their DNS points here: "+sslNamesList(missing))
		}
		if len(stale) > 0 {
			vi = append(vi, "chứng chỉ còn tên site không còn dùng: "+sslNamesList(stale))
			en = append(en, "the certificate still has names the site no longer uses: "+sslNamesList(stale))
		}
		if len(gone) > 0 {
			vi = append(vi, "tên trong chứng chỉ mà DNS không còn trỏ về máy: "+sslNamesList(gone))
			en = append(en, "names in the certificate whose DNS no longer points here: "+sslNamesList(gone))
		}
		if len(missing)+len(stale) > 0 {
			fix = append(fix, issue)
		}
		for _, n := range gone[:min(len(gone), 3)] {
			fix = append(fix, line("getent", "ahostsv4", n))
		}
		return b.bad(Warn,
			i18n.T("Bộ tên trong chứng chỉ chưa khớp bộ tên của site: "+strings.Join(vi, "; "), "The certificate's names do not match the site's names: "+strings.Join(en, "; ")),
			i18n.T("Khách vào tên chưa được phủ sẽ thấy lỗi chứng chỉ. Tên không còn dùng hay không còn trỏ về máy nằm trong chứng chỉ thì lần gia hạn tự động xin lại đúng bộ tên đó và hỏng cả đơn, tức cả site mất chứng chỉ khi hết hạn. Xin lại chứng chỉ cho đúng bộ tên (lệnh đầu), hoặc trỏ lại DNS, hoặc gỡ tên phụ khỏi site (tpan site alias rm).",
				"Visitors on a name that is not covered see a certificate error. A name that is unused or no longer points here and sits in the certificate makes the automatic renewal ask for exactly that set and fail as a whole, so the whole site loses its certificate at expiry. Issue the certificate again for the right set (first command), point the DNS back, or remove the alias from the site (tpan site alias rm)."),
			append(fix, recheck), ev)
	})
}

// ---- names.root.shared ----

// OwnedNames là các tên (tên chính và tên phụ) của một site và chủ của nó.
type OwnedNames struct {
	Site  validate.Domain
	Owner string
	Names []validate.Domain
}

// NamesRootShared báo tên miền gốc (eTLD+1) nào có tên thuộc hai chủ trở lên.
// Từ M28 và M19 luật tạo tên không cho điều này xảy ra với user thường; dữ
// liệu từ trước đó (hoặc admin cố ý gắn tên cho khách) thì có thể có. Chỉ Pass
// hay Warn: tpan không tự gỡ, vì bên nào đúng là quyết định của người vận hành.
// Hàm thuần, không chạy lệnh nào.
func NamesRootShared(sites []OwnedNames) Check {
	b := base{id: "names.root.shared", stage: StageTPAN,
		title: i18n.T("Không tên miền gốc nào có tên của hai chủ khác nhau", "No registrable domain has names of two different owners")}
	type group struct {
		owners map[string]bool
		sites  []string
	}
	groups := map[string]*group{}
	for _, s := range sites {
		for _, n := range s.Names {
			root, err := validate.RegisteredDomain(n)
			if err != nil {
				continue // tên là hậu tố công cộng: không có tên miền gốc để chia
			}
			g := groups[root.String()]
			if g == nil {
				g = &group{owners: map[string]bool{}}
				groups[root.String()] = g
			}
			// Admin được miễn luật tên miền gốc (M28 P4, M19 S1): tên của admin
			// không tính là "chủ thứ hai", để site admin cố ý gắn cho khách
			// không cảnh báo mãi.
			if s.Owner != account.AdminName && s.Owner != "" {
				g.owners[s.Owner] = true
			}
			if !slices.Contains(g.sites, s.Site.String()) {
				g.sites = append(g.sites, s.Site.String())
			}
		}
	}
	return checkFunc(func(context.Context, system.System) Result {
		var shared []string
		for root, g := range groups {
			if len(g.owners) > 1 {
				shared = append(shared, root)
			}
		}
		if len(shared) == 0 {
			return b.pass(i18n.T("Mỗi tên miền gốc chỉ có tên của một chủ", "Each registrable domain has names of one owner only"))
		}
		sort.Strings(shared)
		var out []string
		for _, root := range shared {
			g := groups[root]
			sort.Strings(g.sites)
			out = append(out, root+": "+strings.Join(g.sites, ", "))
		}
		first := groups[shared[0]]
		vi := fmt.Sprintf("Tên miền gốc %s có tên của %d chủ khác nhau (site: %s)", shared[0], len(first.owners), sslNamesList(first.sites))
		en := fmt.Sprintf("The registrable domain %s has names of %d different owners (sites: %s)", shared[0], len(first.owners), sslNamesList(first.sites))
		if n := len(shared) - 1; n > 0 {
			vi += fmt.Sprintf("; còn %d tên miền gốc khác", n)
			en += fmt.Sprintf("; %d more registrable domains", n)
		}
		return b.bad(Warn, i18n.T(vi, en),
			i18n.T("Từ khi có luật tên miền gốc, user thường không còn tạo được tên dưới tên miền gốc của người khác; chỗ này là dữ liệu từ trước, hoặc do admin cố ý gắn tên. Chủ của tên miền gốc ngoài đời có thể không phải cả hai bên, và chủ của một tên con (www, blog...) có thể chiếm tên bằng chứng chỉ của chính họ. Xem hai site, rồi gỡ tên của bên không đúng (tpan site alias rm hay tpan site rm). tpan không tự gỡ.",
				"Since the registrable-domain rule, an ordinary user can no longer create a name under another owner's registrable domain; this is older data, or the admin did it on purpose. The real-world owner of the domain may be neither side, and the holder of a sub-name (www, blog...) can obtain certificates for it. Look at both sites, then remove the name of the wrong party (tpan site alias rm or tpan site rm). tpan removes nothing by itself."),
			[]string{line("tpan", "site", "list"), line("sudo", "tpan", "health")},
			Evidence{Command: "read site_names (tpan)", Output: clip(strings.Join(out, "\n"))})
	})
}
