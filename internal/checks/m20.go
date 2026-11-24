package checks

// Phép kiểm của M20 (docs/thay-doi/M20-thiet-ke.md mục 3.10, 11.1, 11.2 ý 7,
// 11.4, 11.6): hai bước của user, đồng hồ, file khoá niêm phong bí mật, và
// chứng chỉ Let's Encrypt của panel. Tất cả thuộc StageTPAN.
//
// Như health.go: dữ liệu trong tpan.db vào qua hàm người gọi truyền (gói này
// không import store). Hàm đó lỗi thì phép kiểm ra Unknown, không bao giờ làm
// hỏng vòng health.

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// panelFullchain là chứng chỉ của lineage riêng của panel (core.PanelCertDir;
// gói này không import core nên chép lại hằng).
const panelFullchain = "/etc/letsencrypt/live/tpan-panel/fullchain.pem"

// maxNamesInSummary: Summary chỉ liệt kê chừng này tên user; đủ danh sách ở Evidence.
const maxNamesInSummary = 5

// twoFAStatus là lệnh tra trạng thái hai bước, dòng kiểm lại của hai phép kiểm user.
var twoFAStatus = line("tpan", "user", "2fa", "status")

// nameList ghép tên user (đã làm sạch) cho Summary: tối đa maxNamesInSummary
// tên, phần còn lại nói bằng số.
func nameList(names []string) (vi, en string) {
	short := make([]string, 0, maxNamesInSummary)
	for i, n := range names {
		if i == maxNamesInSummary {
			break
		}
		short = append(short, sslClean(n))
	}
	vi, en = strings.Join(short, ", "), strings.Join(short, ", ")
	if more := len(names) - len(short); more > 0 {
		vi += fmt.Sprintf(" và %d người nữa", more)
		en += fmt.Sprintf(" and %d more", more)
	}
	return vi, en
}

// namesCheck là thân chung của hai phép kiểm "liệt kê user": list lỗi là
// Unknown, rỗng là Pass, có tên thì bad dựng kết quả Warn.
func namesCheck(b base, what string, list func(ctx context.Context) ([]string, error), pass i18n.Text,
	bad func(vi, en string, ev Evidence) Result) Check {
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		ev := Evidence{Command: what + " (tpan)"}
		if list == nil {
			return b.unknown(i18n.T("chưa nối với cơ sở dữ liệu", "no database is connected"))
		}
		names, err := list(ctx)
		if err != nil {
			ev.ExitCode, ev.Output = -1, clip(err.Error())
			return b.unknown(i18n.T("không đọc được tình trạng hai bước ("+sslClean(err.Error())+")",
				"could not read the two-step status ("+sslClean(err.Error())+")"), ev)
		}
		if len(names) == 0 {
			ev.Output = "none"
			return b.pass(pass, ev)
		}
		clean := make([]string, len(names))
		for i, n := range names {
			clean[i] = sslClean(n)
		}
		ev.ExitCode, ev.Output = 1, clip(strings.Join(clean, "\n"))
		vi, en := nameList(names)
		return bad(vi, en, ev)
	})
}

// AdminsWithout2FA (ID "tpan.2fa:admins", StageTPAN): list trả tên các admin
// đang hoạt động chưa bật đăng nhập hai bước. Rỗng: Pass. Có tên: Warn, liệt kê
// tên và cách bật (trang Tài khoản). list lỗi: Unknown.
func AdminsWithout2FA(list func(ctx context.Context) ([]string, error)) Check {
	b := base{id: "tpan.2fa:admins", stage: StageTPAN,
		title: i18n.T("Mọi admin đã bật đăng nhập hai bước", "Every admin has two-step login turned on")}
	return namesCheck(b, "admins without two-step login", list,
		i18n.T("Mọi admin đang hoạt động đã bật đăng nhập hai bước", "Every active admin has two-step login turned on"),
		func(vi, en string, ev Evidence) Result {
			return b.bad(Warn,
				i18n.T("Admin chưa bật đăng nhập hai bước: "+vi, "Admins without two-step login: "+en),
				i18n.T("Admin chỉ có mật khẩu thì lộ mật khẩu (đoán, dùng lại ở trang khác, bị lừa) là đủ để người khác điều khiển cả máy. Mỗi admin tự bật: đăng nhập panel, mở trang Tài khoản, mục Đăng nhập hai bước, quét mã QR bằng app xác thực trên điện thoại.",
					"An admin with only a password is one leaked password (guessed, reused elsewhere, phished) away from someone else running the whole machine. Each admin turns it on themselves: sign in to the panel, open the Account page, Two-step login, and scan the QR code with an authenticator app on a phone."),
				[]string{twoFAStatus}, ev)
		})
}

// TwoFAAttempts (ID "tpan.2fa:attempts", StageTPAN): list trả tên các user bị
// khoá bước hai trong 7 ngày qua (dòng audit login.2fa_locked, kết quả locked;
// health dựng list từ audit). Có tên: Warn, vì người nhập sai đã qua được
// bước mật khẩu. list lỗi: Unknown.
func TwoFAAttempts(list func(ctx context.Context) ([]string, error)) Check {
	b := base{id: "tpan.2fa:attempts", stage: StageTPAN,
		title: i18n.T("Không ai đang dò mã hai bước", "Nobody is guessing two-step codes")}
	return namesCheck(b, "users locked at the second step", list,
		i18n.T("Không có user nào bị nhập sai mã hai bước nhiều lần", "No user has had many wrong two-step codes"),
		func(vi, en string, ev Evidence) Result {
			// Mật khẩu của user thường đặt lại bằng `tpan user passwd`, của
			// admin bằng `tpan admin passwd`; tự đổi thì ở trang Tài khoản.
			fix := []string{line("tpan", "user", "passwd") + " <user>", line("tpan", "admin", "passwd")}
			return b.bad(Warn,
				i18n.T("Có người nhập sai mã hai bước nhiều lần cho "+vi+": mật khẩu của họ có thể đã lộ. Đổi mật khẩu.",
					"Someone entered many wrong two-step codes for "+en+": their password may have leaked. Change the password."),
				i18n.T("Bước hai chỉ hỏi mã sau khi mật khẩu đã đúng, nên người nhập sai mã đã biết mật khẩu. Hai bước vẫn đang chặn họ, nhưng nên đổi mật khẩu ngay. Cảnh báo tự hết khi 7 ngày qua không còn lần khoá nào.",
					"The second step only asks for a code after the password was right, so whoever typed the wrong codes knows the password. Two-step login is still stopping them, but change the password now. The warning clears once there has been no lock-out for 7 days."),
				append(fix, twoFAStatus), ev)
		})
}

// clockCmd hỏi systemd đồng hồ đã đồng bộ NTP chưa; in đúng "yes" hoặc "no"
// (thấy trên máy ảo, docs/bang-chung/M20-nghien-cuu/thu-vm.log phần 1).
var clockCmd = system.Cmd{Name: "timedatectl", Args: []string{"show", "-p", "NTPSynchronized", "--value"}}

// Clock (ID "tpan.clock", StageTPAN): hỏi anyEnabled trước; chưa ai bật hai
// bước thì Pass mà không chạy lệnh nào (đồng hồ chỉ quan trọng cho mã TOTP, và
// health của máy chưa dùng hai bước giữ nguyên như bản cũ: không thêm lệnh).
// Có người bật: `timedatectl show -p NTPSynchronized --value`; "yes" Pass,
// "no" Warn. Lệnh không chạy được, mã thoát khác 0, output lạ, anyEnabled
// lỗi: Unknown.
func Clock(anyEnabled func(ctx context.Context) (bool, error)) Check {
	b := base{id: "tpan.clock", stage: StageTPAN,
		title: i18n.T("Đồng hồ của máy được đồng bộ (NTP)", "The machine clock is synchronised (NTP)")}
	recheck := clockCmd.String()
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		if anyEnabled == nil {
			return b.unknown(i18n.T("chưa nối với cơ sở dữ liệu", "no database is connected"))
		}
		on, err := anyEnabled(ctx)
		if err != nil {
			return b.unknown(i18n.T("không đọc được tình trạng hai bước ("+sslClean(err.Error())+")",
				"could not read the two-step status ("+sslClean(err.Error())+")"))
		}
		if !on {
			return b.pass(i18n.T("Chưa ai bật đăng nhập hai bước nên chưa cần kiểm đồng hồ",
				"Nobody uses two-step login yet, so the clock does not need checking"))
		}
		res, ev, err := execCmd(ctx, sys, clockCmd)
		if err != nil {
			return b.cantRun(err, ev)
		}
		if res.ExitCode != 0 {
			return b.unknown(i18n.T("timedatectl báo lỗi (mã "+strconv.Itoa(res.ExitCode)+")", "timedatectl failed (exit "+strconv.Itoa(res.ExitCode)+")"), ev)
		}
		switch strings.TrimSpace(res.Stdout) {
		case "yes":
			return b.pass(i18n.T("Đồng hồ đã đồng bộ NTP", "The clock is synchronised over NTP"), ev)
		case "no":
		default:
			// FakeSystem và lệnh lạ trả rỗng: không đoán là đạt.
			return b.unknown(i18n.T("timedatectl trả output lạ", "timedatectl printed something unexpected"), ev)
		}
		return b.bad(Warn,
			i18n.T("Đồng hồ của máy chưa đồng bộ NTP", "The machine clock is not synchronised over NTP"),
			i18n.T("Mã sáu số của đăng nhập hai bước đổi mỗi 30 giây theo giờ. Đồng hồ máy chủ trôi quá một bước so với điện thoại thì mã đúng cũng bị từ chối và user chỉ còn vào được bằng mã dự phòng.",
				"Two-step codes change every 30 seconds by the clock. If the server clock drifts more than one step from the phone, correct codes are refused and users can only get in with recovery codes."),
			[]string{line("timedatectl", "set-ntp", "true"), line("systemctl", "restart", "systemd-timesyncd"), line("timedatectl", "status"), recheck}, ev)
	})
}

// secretKeySize là cỡ khoá AES-256 (mfa.Key).
const secretKeySize = 32

// SecretKey (ID "tpan.secretkey", StageTPAN): file khoá niêm phong bí mật hai
// bước. CHỈ Stat (lstat: symlink lộ ra là symlink), không bao giờ đọc nội dung
// (mục 11.6). Không ai bật hai bước: Pass "chưa ai bật" (khoá chưa cần, `tpan
// serve` tạo nó ở lần setup đầu). Có người bật: file thường, chủ root:root, quyền
// 0600, cỡ 32 byte thì Pass; mất hay sai thì Fail (họ chỉ còn mã dự phòng).
// anyEnabled lỗi, Stat lỗi khác "không có": Unknown.
func SecretKey(p string, anyEnabled func(ctx context.Context) (bool, error)) Check {
	b := base{id: "tpan.secretkey", stage: StageTPAN,
		title: i18n.T("File khoá của đăng nhập hai bước còn nguyên", "The two-step login key file is intact")}
	recheck := line("ls", "-l", p)
	reset := line("tpan", "user", "2fa", "reset") + " <user>"
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		ev := Evidence{Command: "stat " + p + " (tpan)"}
		if anyEnabled == nil {
			return b.unknown(i18n.T("chưa nối với cơ sở dữ liệu", "no database is connected"))
		}
		on, err := anyEnabled(ctx)
		if err != nil {
			return b.unknown(i18n.T("không đọc được tình trạng hai bước ("+sslClean(err.Error())+")",
				"could not read the two-step status ("+sslClean(err.Error())+")"))
		}
		if !on {
			return b.pass(i18n.T("Chưa ai bật đăng nhập hai bước nên chưa cần file khoá", "Nobody uses two-step login yet, so the key file is not needed"))
		}
		lost := i18n.T("User đã bật hai bước nhưng tpan không mở được bí mật của họ: họ chỉ còn vào được bằng mã dự phòng. Không có cách lấy lại khoá; gỡ hai bước của từng user rồi để họ bật lại.",
			"Users have two-step login on but tpan cannot open their secrets: they can only sign in with recovery codes. The key cannot be recovered; reset two-step login for each user and let them turn it on again.")
		info, err := sys.Stat(p)
		if errors.Is(err, fs.ErrNotExist) {
			ev.ExitCode, ev.Output = 1, "No such file or directory"
			return b.bad(Fail, i18n.T("Không có file khoá "+p, "The key file "+p+" is missing"), lost,
				[]string{twoFAStatus, reset, recheck}, ev)
		}
		if err != nil {
			ev.ExitCode, ev.Output = -1, clip(err.Error())
			return b.unknown(i18n.T("không đọc được thông tin file ("+sslClean(err.Error())+")", "could not stat the file ("+sslClean(err.Error())+")"), ev)
		}
		user, group := orRoot(info.Owner.User), orRoot(info.Owner.Group)
		ev.Output = fmt.Sprintf("type %s, mode %04o, owner %s:%s, size %d", fileKind(info.Mode), uint32(info.Mode.Perm()), user, group, info.Size)
		switch {
		case !info.Mode.IsRegular() || info.Size != secretKeySize:
			ev.ExitCode = 1
			return b.bad(Fail,
				i18n.T(fmt.Sprintf("File khoá %s hỏng (%s, %d byte; phải là file thường %d byte)", p, fileKind(info.Mode), info.Size, secretKeySize),
					fmt.Sprintf("The key file %s is damaged (%s, %d bytes; it must be a regular file of %d bytes)", p, fileKind(info.Mode), info.Size, secretKeySize)),
				lost, []string{twoFAStatus, reset, recheck}, ev)
		case info.Mode.Perm() != 0o600 || user != "root" || group != "root":
			ev.ExitCode = 1
			return b.bad(Fail,
				i18n.T(fmt.Sprintf("File khoá %s có quyền %04o, chủ %s:%s (phải là 0600, root:root)", p, uint32(info.Mode.Perm()), user, group),
					fmt.Sprintf("The key file %s has mode %04o, owner %s:%s (it must be 0600, root:root)", p, uint32(info.Mode.Perm()), user, group)),
				i18n.T("Quyền rộng hơn thì user khác trên máy đọc được khoá và mở được bí mật hai bước trong bản sao tpan.db; tpan cũng từ chối dùng khoá như vậy nên không ai qua được bước hai bằng mã sáu số.",
					"A wider mode lets other users on the machine read the key and open the two-step secrets in any copy of tpan.db; tpan also refuses to use such a key, so nobody gets past the second step with a six-digit code."),
				[]string{line("chown", "root:root", p), line("chmod", "0600", p), recheck}, ev)
		}
		return b.pass(i18n.T("File khoá "+p+" đúng: file thường 32 byte, 0600, root", "The key file "+p+" is correct: a regular 32-byte file, 0600, root"), ev)
	})
}

// fileKind gọi tên loại file cho Evidence và Summary.
func fileKind(m fs.FileMode) string {
	switch {
	case m&fs.ModeSymlink != 0:
		return "symlink"
	case m.IsDir():
		return "directory"
	case m.IsRegular():
		return "regular file"
	}
	return "special file"
}

// PanelACMECert (ID "tpan.panelcert:<d>", StageTPAN): chứng chỉ Let's Encrypt
// của panel ở lineage tpan-panel. Mất file, đọc không ra chứng chỉ, SAN không có
// d, hết hạn hay chưa tới hạn: Fail. Còn dưới 14 ngày: Warn. Bên ký có chữ
// STAGING hay "Fake LE" (chứng chỉ thử, trình duyệt không tin; mục 11.4): Warn.
// Còn lại: Pass "còn N ngày, do <bên ký> ký". now nil thì time.Now.
func PanelACMECert(d validate.Domain, now func() time.Time) Check {
	if now == nil {
		now = time.Now
	}
	dom := d.String()
	b := base{id: "tpan.panelcert:" + dom, stage: StageTPAN,
		title: i18n.T("Chứng chỉ Let's Encrypt của panel còn đúng và còn hạn", "The panel's Let's Encrypt certificate is right and not expiring")}
	recheck := line("certbot", "certificates", "--cert-name", "tpan-panel")
	again := line("tpan", "panel", "domain", "set", dom)
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		if d.IsZero() {
			return b.unknown(i18n.T("thiếu tên miền của panel", "the panel domain is missing"))
		}
		ev := Evidence{Command: "read " + panelFullchain + " (tpan)"}
		data, err := sys.ReadFile(panelFullchain)
		if errors.Is(err, fs.ErrNotExist) {
			ev.ExitCode, ev.Output = 1, "No such file or directory"
			return b.bad(Fail,
				i18n.T("Không có chứng chỉ của panel ("+panelFullchain+")", "The panel certificate is missing ("+panelFullchain+")"),
				i18n.T("Tên miền của panel đã đặt nhưng chứng chỉ của nó không còn (bị xoá tay?). Panel đang dùng lại chứng chỉ tự ký, trình duyệt cảnh báo. Xin lại bằng cách đặt lại tên miền.",
					"The panel domain is set but its certificate is gone (deleted by hand?). The panel is back on its self-signed certificate and browsers warn. Request it again by setting the domain again."),
				[]string{again, recheck}, ev)
		}
		if err != nil {
			ev.ExitCode, ev.Output = -1, clip(err.Error())
			return b.unknown(i18n.T("không đọc được "+panelFullchain+" ("+sslClean(err.Error())+")", "could not read "+panelFullchain+" ("+sslClean(err.Error())+")"), ev)
		}
		cert, perr := sslFirstCert(data)
		if perr != nil {
			ev.ExitCode, ev.Output = 1, clip(perr.Error())
			return b.bad(Fail,
				i18n.T(panelFullchain+" không đọc được như một chứng chỉ", panelFullchain+" cannot be read as a certificate"),
				i18n.T("File hỏng hoặc bị ghi đè; panel không dùng được nó. Xin lại bằng cách đặt lại tên miền.",
					"The file is damaged or overwritten; the panel cannot use it. Request it again by setting the domain again."),
				[]string{again, recheck}, ev)
		}
		t := now()
		issuer := cert.Issuer.CommonName
		if issuer == "" {
			issuer = cert.Issuer.String()
		}
		issuer = sslClean(issuer)
		names := sslClean(strings.Join(cert.DNSNames, ", "))
		ev.Output = fmt.Sprintf("SAN: %s\nissuer: %s\nserial: %X\nnotBefore: %s\nnotAfter: %s", names, issuer, cert.SerialNumber,
			cert.NotBefore.UTC().Format(time.RFC3339), cert.NotAfter.UTC().Format(time.RFC3339))
		left := cert.NotAfter.Sub(t)
		days := int(left / (24 * time.Hour))
		switch {
		case cert.VerifyHostname(dom) != nil:
			return b.bad(Fail,
				i18n.T("Chứng chỉ của panel không dành cho "+dom+" (có: "+names+")", "The panel certificate is not for "+dom+" (it covers: "+names+")"),
				i18n.T("Chứng chỉ trên đĩa cấp cho tên khác, nên panel không phục vụ nó cho "+dom+" và trình duyệt cảnh báo. Thường do đổi tên giữa chừng hỏng.",
					"The certificate on disk is for another name, so the panel does not serve it for "+dom+" and browsers warn. Usually a rename that failed halfway."),
				[]string{again, recheck}, ev)
		case left <= 0:
			return b.bad(Fail,
				i18n.T("Chứng chỉ của panel đã hết hạn từ "+cert.NotAfter.UTC().Format("2006-01-02"), "The panel certificate expired on "+cert.NotAfter.UTC().Format("2006-01-02")),
				i18n.T("Gia hạn tự động (certbot.timer) không chạy được. Trình duyệt chặn panel bằng cảnh báo đỏ.",
					"Automatic renewal (certbot.timer) did not work. Browsers block the panel with a red warning."),
				[]string{line("tail", "-n", "30", "/var/log/letsencrypt/letsencrypt.log"), line("certbot", "renew", "--cert-name", "tpan-panel"), recheck}, ev)
		case cert.NotBefore.After(t):
			return b.bad(Fail,
				i18n.T("Chứng chỉ của panel chưa có hiệu lực (từ "+cert.NotBefore.UTC().Format("2006-01-02")+")", "The panel certificate is not valid yet (from "+cert.NotBefore.UTC().Format("2006-01-02")+")"),
				i18n.T("Đồng hồ của máy đang chạy chậm so với thật nên chứng chỉ vừa cấp trông như ở tương lai.",
					"The machine clock is behind real time, so a just-issued certificate looks like it is from the future."),
				[]string{line("timedatectl", "status"), recheck}, ev)
		case left < sslWarnDays*24*time.Hour:
			return b.bad(Warn,
				i18n.T("Chứng chỉ của panel chỉ còn "+strconv.Itoa(days)+" ngày", "The panel certificate has only "+strconv.Itoa(days)+" days left"),
				i18n.T("certbot gia hạn khi còn 30 ngày, nên còn dưới 14 ngày nghĩa là gia hạn tự động đã không chạy. Xem bộ hẹn giờ certbot và thử gia hạn tay.",
					"certbot renews at 30 days left, so under 14 days means automatic renewal did not run. Check the certbot timer and try renewing by hand."),
				[]string{line("systemctl", "status", "certbot.timer", "--no-pager"), line("certbot", "renew", "--cert-name", "tpan-panel"), recheck}, ev)
		case strings.Contains(strings.ToUpper(issuer), "STAGING") || strings.Contains(issuer, "Fake LE"):
			return b.bad(Warn,
				i18n.T("Chứng chỉ của panel là chứng chỉ thử ("+issuer+"), trình duyệt không tin", "The panel certificate is a test certificate ("+issuer+"), browsers do not trust it"),
				i18n.T("Chứng chỉ do máy chủ staging của Let's Encrypt ký: dùng để thử, trình duyệt vẫn cảnh báo như chứng chỉ tự ký. Tắt staging rồi xin lại.",
					"The certificate was signed by Let's Encrypt's staging server: it is for testing and browsers warn as with a self-signed one. Turn staging off and request it again."),
				[]string{line("tpan", "ssl", "settings", "--staging=false"), again, recheck}, ev)
		}
		return b.pass(i18n.T(fmt.Sprintf("Chứng chỉ của panel cho %s còn %d ngày, do %s ký", dom, days, issuer),
			fmt.Sprintf("The panel certificate for %s has %d days left, signed by %s", dom, days, issuer)), ev)
	})
}

// Nhịp thử lại của PanelServes: đệm setting của panel là 2 giây (mục 11.2 ý 5),
// nên ngay sau job panel có thể còn phục vụ chứng chỉ cũ một lúc. Là biến để
// test rút ngắn.
var (
	panelServesEvery = 500 * time.Millisecond
	panelServesFor   = 10 * time.Second
)

// portRE: cổng 1 tới 5 chữ số (giá trị đi vào argv của openssl).
var portRE = regexp.MustCompile(`^[0-9]{1,5}$`)

// PanelServes (ID "tpan.panelserves:<d>", StageTPAN): panel đang phục vụ đúng
// chứng chỉ của lineage cho tên d. `systemctl is-active tpan` không phải
// "active" (chạy từ CLI lúc dịch vụ dừng): Unknown. Rồi `openssl s_client
// -connect 127.0.0.1:<port> -servername <d>` (stdin rỗng nên openssl đóng kết
// nối ngay sau bắt tay), lấy khối PEM đầu, so serial với fullchain của lineage;
// khác thì thử lại mỗi 500 ms tới 10 giây rồi Fail. Không cần tin CA (pebble
// trong e2e): so serial chứng minh GetCertificate đã chọn đúng.
func PanelServes(d validate.Domain, port string) Check {
	dom := d.String()
	b := base{id: "tpan.panelserves:" + dom, stage: StageTPAN,
		title: i18n.T("Panel phục vụ chứng chỉ Let's Encrypt cho "+dom, "The panel serves the Let's Encrypt certificate for "+dom)}
	active := system.Cmd{Name: "systemctl", Args: []string{"is-active", "tpan"}}
	probe := system.Cmd{Name: "openssl", Args: []string{"s_client", "-connect", "127.0.0.1:" + port, "-servername", dom}}
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		if d.IsZero() {
			return b.unknown(i18n.T("thiếu tên miền của panel", "the panel domain is missing"))
		}
		if n, err := strconv.Atoi(port); !portRE.MatchString(port) || err != nil || n < 1 || n > 65535 {
			return b.unknown(i18n.T("cổng của panel không hợp lệ", "the panel port is not valid"))
		}
		resA, evA, err := execCmd(ctx, sys, active)
		if err != nil {
			return b.cantRun(err, evA)
		}
		if resA.ExitCode != 0 || strings.TrimSpace(resA.Stdout) != "active" {
			return b.unknown(i18n.T("dịch vụ tpan không chạy nên không có panel để hỏi", "the tpan service is not running, so there is no panel to ask"), evA)
		}
		evF := Evidence{Command: "read " + panelFullchain + " (tpan)"}
		data, err := sys.ReadFile(panelFullchain)
		if err != nil {
			evF.ExitCode, evF.Output = -1, clip(err.Error())
			return b.unknown(i18n.T("không đọc được chứng chỉ của lineage ("+sslClean(err.Error())+")", "could not read the lineage certificate ("+sslClean(err.Error())+")"), evA, evF)
		}
		want, perr := sslFirstCert(data)
		if perr != nil {
			evF.ExitCode, evF.Output = 1, clip(perr.Error())
			return b.unknown(i18n.T("chứng chỉ của lineage không đọc được", "the lineage certificate cannot be read"), evA, evF)
		}
		evF.Output = fmt.Sprintf("serial: %X", want.SerialNumber)

		deadline := time.Now().Add(panelServesFor)
		var evS Evidence
		var got string // serial đang phục vụ, "" khi không lấy được chứng chỉ
		for attempt := 1; ; attempt++ {
			res, ev, err := execCmd(ctx, sys, probe)
			if err != nil {
				return b.cantRun(err, evA, evF, ev)
			}
			got = ""
			if c, cerr := sslFirstCert([]byte(res.Stdout)); cerr == nil {
				if c.SerialNumber.Cmp(want.SerialNumber) == 0 {
					ev.Output = fmt.Sprintf("served serial: %X (attempt %d)", c.SerialNumber, attempt)
					return b.pass(i18n.T("Panel phục vụ đúng chứng chỉ của lineage cho "+dom+fmt.Sprintf(" (serial %X)", c.SerialNumber),
						"The panel serves the lineage certificate for "+dom+fmt.Sprintf(" (serial %X)", c.SerialNumber)), evA, evF, ev)
				}
				got = fmt.Sprintf("%X", c.SerialNumber)
			}
			evS = ev
			if !time.Now().Add(panelServesEvery).Before(deadline) {
				break
			}
			select {
			case <-ctx.Done():
				return b.unknown(i18n.T("bị huỷ lúc chờ panel", "cancelled while waiting for the panel"), evA, evF, evS)
			case <-time.After(panelServesEvery):
			}
		}
		cause := i18n.T("Panel không chọn chứng chỉ Let's Encrypt khi được hỏi bằng tên "+dom+": tên trong setting panel.domain khác, chứng chỉ không khớp khoá hay đã hết hạn, hoặc tiến trình tpan đang chạy là bản cũ chưa biết chọn theo tên.",
			"The panel does not pick the Let's Encrypt certificate when asked for "+dom+": the panel.domain setting differs, the certificate does not match its key or has expired, or the running tpan process is an old build that cannot pick by name.")
		fix := []string{line("journalctl", "-u", "tpan", "-n", "50", "--no-pager"), line("systemctl", "restart", "tpan"), probe.String()}
		if got == "" {
			return b.bad(Fail,
				i18n.T("Panel ở cổng "+port+" không trả chứng chỉ nào cho "+dom, "The panel on port "+port+" returned no certificate for "+dom),
				cause, fix, evA, evF, evS)
		}
		return b.bad(Fail,
			i18n.T("Panel phục vụ chứng chỉ serial "+got+" cho "+dom+fmt.Sprintf(", không phải %X của lineage", want.SerialNumber),
				"The panel serves certificate serial "+got+" for "+dom+fmt.Sprintf(", not %X from the lineage", want.SerialNumber)),
			cause, fix, evA, evF, evS)
	})
}
