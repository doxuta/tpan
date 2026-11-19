package checks

// Hợp đồng M4: các phép kiểm mới của health (mục 6.7 của docs/KE-HOACH.md):
// phát hiện lệch, RAM và swap, và tự kiểm chính tpan.
//
// Gói vẫn chỉ import system và i18n. Thứ cần DB (toàn vẹn, job treo, nhịp của
// bộ hẹn giờ) được đưa vào bằng hàm, gói health nối chúng với store.
//
// Mọi Result không đạt có Cause và ManualFix (dòng cuối là lệnh kiểm lại), như
// các phép kiểm của M1, M2.
//
// Giống WPCLI và Disk (files.go): các phép kiểm ở đây đọc file bằng Go qua
// system.System chứ không chạy lệnh, nên Evidence.Command là nhãn "(tpan)" nói
// thật việc đã làm; dòng cuối của ManualFix mới là lệnh thật để tự kiểm lại.

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

var sha256RE = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

// Drift (ID "drift:<path>", chặng stage): so sha256 của file đang có với
// wantSHA256 (hex, giá trị ở bảng managed_files lúc tpan ghi). Khớp: Pass.
// Khác: Warn (không phải Fail: máy có thể vẫn chạy đúng, nhưng lần chạy lại
// Plan sẽ ghi đè bản sửa tay), Summary nêu đúng đường dẫn, Evidence có sha256
// hai bên. File không còn: Fail. Cách sửa tay: xem diff bằng `tpan health
// --fix --dry-run`, hoặc tự đưa file về; AutoFix do core điền (mục 6.7: ghi
// lại bản của tpan, hoặc nhận bản sửa tay làm chuẩn mới).
// Đọc file bằng sys.ReadFile, tính sha256 bằng Go (như WPCLI).
func Drift(p, wantSHA256 string, stage Stage) Check {
	b := base{id: "drift:" + p, stage: stage,
		title: i18n.T("File "+p+" còn đúng như tpan đã ghi", "File "+p+" is still as tpan wrote it")}
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		// Mã mong đợi hỏng là lỗi của người gọi, không phải của máy: nói là
		// chưa kiểm được thay vì báo lệch giả.
		if !sha256RE.MatchString(wantSHA256) {
			return b.unknown(i18n.T("mã sha256 mong đợi phải gồm 64 ký tự hex", "the expected sha256 must be 64 hex characters"))
		}
		want := strings.ToLower(wantSHA256)
		ev := Evidence{Command: "sha256sum " + p + " (tpan)"}

		data, err := sys.ReadFile(p)
		if errors.Is(err, fs.ErrNotExist) {
			ev.ExitCode, ev.Output = 1, "không có file / no such file\nmong đợi / expected: "+want
			return b.bad(Fail,
				i18n.T("File "+p+" không còn", "File "+p+" is gone"),
				i18n.T("File do tpan quản lý đã bị xoá hoặc đổi tên. Thiếu nó thì phần cấu hình tương ứng không còn tác dụng.",
					"A file managed by tpan was deleted or renamed. Without it the matching configuration has no effect."),
				[]string{line("tpan", "health", "--fix", "--dry-run"), line("ls", "-l", p)}, ev)
		}
		if err != nil {
			ev.ExitCode, ev.Output = -1, err.Error()
			return b.unknown(i18n.T("không đọc được file ("+err.Error()+")", "could not read the file ("+err.Error()+")"), ev)
		}
		sum := sha256.Sum256(data)
		got := hex.EncodeToString(sum[:])
		ev.Output = got + "  " + p + "\nmong đợi / expected: " + want
		if got == want {
			return b.pass(i18n.T("File "+p+" đúng như tpan đã ghi", "File "+p+" is as tpan wrote it"), ev)
		}
		return b.bad(Warn,
			i18n.T("File "+p+" khác bản tpan đã ghi (có thể bị sửa tay)", "File "+p+" differs from what tpan wrote (it may have been edited by hand)"),
			i18n.T("Có người hoặc chương trình khác sửa file này. Máy có thể vẫn chạy đúng, nhưng lần tới tpan dựng lại cấu hình sẽ ghi đè bản sửa tay.",
				"Someone or something else changed this file. The machine may still work, but the next time tpan rebuilds the configuration it will overwrite the manual edit."),
			[]string{line("tpan", "health", "--fix", "--dry-run"), line("sha256sum", p)}, ev)
	})
}

// DriftStage chọn chặng cho phép kiểm lệch theo đường dẫn, để lệch hiện đúng
// ga trên bản đồ tuyến: /etc/nginx/ và /etc/apache2/ là StageWeb; /etc/php/
// là StagePHP; dưới /var/www/ là StageWordPress; /etc/tpan/ và
// /etc/systemd/system/tpan* là StageTPAN; còn lại (logrotate, mariadb.conf.d)
// là StageMachine.
func DriftStage(p string) Stage {
	// Clean trước để "/etc/nginx/../tpan/x" không bị xếp nhầm vào chặng web.
	p = path.Clean(p)
	switch {
	case strings.HasPrefix(p, "/etc/nginx/"), strings.HasPrefix(p, "/etc/apache2/"):
		return StageWeb
	case strings.HasPrefix(p, "/etc/php/"):
		return StagePHP
	case strings.HasPrefix(p, "/var/www/"):
		return StageWordPress
	case p == "/etc/tpan", strings.HasPrefix(p, "/etc/tpan/"), strings.HasPrefix(p, "/etc/systemd/system/tpan"):
		return StageTPAN
	}
	return StageMachine
}

// Ngưỡng RAM theo mục 6.7: RAM còn dùng được dưới 10% là cảnh báo, dưới 5% là
// lỗi; swap dùng quá 50% là cảnh báo.
const (
	memWarnPct  = 10.0
	memFailPct  = 5.0
	swapWarnPct = 50.0
)

// Memory (ID "memory:ram", StageMachine): đọc /proc/meminfo. RAM còn dùng
// được (MemAvailable) dưới 10% tổng: Warn; dưới 5%: Fail. Có swap và swap đã
// dùng trên 50%: Warn. Không có swap: Pass kèm ghi chú trong Summary (máy nhỏ
// không swap là chuyện thường; trang Bảo mật của M5 mới gợi ý thêm swap).
func Memory() Check {
	const file = "/proc/meminfo"
	b := base{id: "memory:ram", stage: StageMachine,
		title: i18n.T("RAM và swap còn đủ dùng", "RAM and swap are not running out")}
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		ev := Evidence{Command: "cat " + file + " (tpan)"}
		data, err := sys.ReadFile(file)
		if err != nil {
			ev.ExitCode, ev.Output = -1, err.Error()
			return b.unknown(i18n.T("không đọc được "+file+" ("+err.Error()+")", "could not read "+file+" ("+err.Error()+")"), ev)
		}
		// Dùng MemAvailable chứ không MemFree: MemFree không tính page cache mà
		// kernel trả lại được ngay, nên máy khoẻ vẫn trông như sắp hết RAM.
		kv := parseMeminfo(string(data))
		var out []string
		for _, k := range []string{"MemTotal", "MemAvailable", "SwapTotal", "SwapFree"} {
			v, ok := kv[k]
			if !ok {
				ev.ExitCode, ev.Output = 1, clip(strings.Join(out, "\n"))
				return b.unknown(i18n.T(file+" thiếu "+k, file+" has no "+k), ev)
			}
			out = append(out, fmt.Sprintf("%s: %d kB", k, v))
		}
		ev.Output = strings.Join(out, "\n")
		total, avail, swapTotal, swapFree := kv["MemTotal"]*1024, kv["MemAvailable"]*1024, kv["SwapTotal"]*1024, kv["SwapFree"]*1024
		if total == 0 || avail > total || swapFree > swapTotal {
			return b.unknown(i18n.T("số liệu bộ nhớ vô lý", "the memory numbers make no sense"), ev)
		}
		availPct := float64(avail) / float64(total) * 100
		ramVi := fmt.Sprintf("RAM còn dùng được %.1f%% (%s trên %s)", availPct, humanBytes(avail), humanBytes(total))
		ramEn := fmt.Sprintf("RAM available is %.1f%% (%s of %s)", availPct, humanBytes(avail), humanBytes(total))

		var swapVi, swapEn string
		swapBad := false
		if swapTotal == 0 {
			swapVi, swapEn = "máy không có swap", "the machine has no swap"
		} else {
			usedPct := float64(swapTotal-swapFree) / float64(swapTotal) * 100
			swapVi = fmt.Sprintf("swap đã dùng %.1f%%", usedPct)
			swapEn = fmt.Sprintf("swap is %.1f%% used", usedPct)
			swapBad = usedPct > swapWarnPct
		}

		st := Pass
		switch {
		case availPct < memFailPct:
			st = Fail
		case availPct < memWarnPct || swapBad:
			st = Warn
		}
		sumVi, sumEn := ramVi+"; "+swapVi, ramEn+"; "+swapEn
		if st == Pass {
			return b.pass(i18n.T(sumVi, sumEn), ev)
		}
		fix := []string{line("ps", "-eo", "pid,user,%mem,comm", "--sort=-%mem")}
		if swapBad {
			fix = append(fix, line("swapon", "--show"))
		}
		fix = append(fix, line("free", "-h"))
		return b.bad(st, i18n.T(sumVi, sumEn),
			i18n.T("Máy sắp hết RAM: PHP-FPM mở quá nhiều worker hoặc MariaDB dùng nhiều. Hết hẳn thì kernel tự giết một tiến trình (OOM killer), thường là MariaDB hoặc PHP, và site lỗi theo.",
				"The machine is running out of RAM: PHP-FPM opened too many workers or MariaDB uses a lot. When it is gone the kernel kills a process (OOM killer), often MariaDB or PHP, and sites fail with it."),
			fix, ev)
	})
}

// parseMeminfo đọc các dòng "Key:   123 kB" của /proc/meminfo thành map, đơn
// vị kB. Dòng không đúng dạng thì bỏ qua.
func parseMeminfo(s string) map[string]uint64 {
	kv := map[string]uint64{}
	for l := range strings.Lines(s) {
		k, rest, ok := strings.Cut(l, ":")
		f := strings.Fields(rest)
		if !ok || len(f) == 0 {
			continue
		}
		if n, err := strconv.ParseUint(f[0], 10, 64); err == nil {
			kv[strings.TrimSpace(k)] = n
		}
	}
	return kv
}

// humanBytes: số byte dạng người đọc (KiB, MiB, GiB).
func humanBytes(n uint64) string {
	const k = 1024.0
	f := float64(n)
	switch {
	case f >= k*k*k:
		return fmt.Sprintf("%.1f GiB", f/(k*k*k))
	case f >= k*k:
		return fmt.Sprintf("%.0f MiB", f/(k*k))
	case f >= k:
		return fmt.Sprintf("%.0f KiB", f/k)
	}
	return fmt.Sprintf("%d B", n)
}

// FreeSpace (ID "disk.free:<path>", chặng stage): chỗ trống tuyệt đối (như
// cột Avail của df, DiskUsage.Free) dưới minBytes là Fail. Khác Disk (phần
// trăm): dùng cho /var/lib/tpan, để kịch bản làm đầy đĩa tới 90% chỉ làm chặng
// "máy" lỗi, không kéo theo chặng "tpan" khi vẫn còn hàng GB trống.
func FreeSpace(p string, minBytes uint64, stage Stage) Check {
	b := base{id: "disk.free:" + p, stage: stage,
		title: i18n.T("Ổ đĩa chứa "+p+" còn ít nhất "+humanBytes(minBytes)+" trống", "The disk holding "+p+" has at least "+humanBytes(minBytes)+" free")}
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		if !path.IsAbs(p) {
			return b.unknown(i18n.T("đường dẫn phải tuyệt đối (nhận được \""+p+"\")", "the path must be absolute (got \""+p+"\")"))
		}
		ev := Evidence{Command: "statfs " + p + " (tpan)"}
		du, err := sys.StatFS(p)
		if err != nil {
			ev.ExitCode, ev.Output = -1, err.Error()
			return b.unknown(i18n.T("không đọc được dung lượng ("+err.Error()+")", "could not read the disk size ("+err.Error()+")"), ev)
		}
		if du.Total == 0 || du.Free > du.Total {
			ev.ExitCode, ev.Output = 1, fmt.Sprintf("total=%d free=%d", du.Total, du.Free)
			return b.unknown(i18n.T("số liệu dung lượng vô lý", "the disk size numbers make no sense"), ev)
		}
		ev.Output = fmt.Sprintf("total=%d free=%d min=%d", du.Total, du.Free, minBytes)
		if du.Free >= minBytes {
			return b.pass(i18n.T("Ổ đĩa chứa "+p+" còn trống "+humanBytes(du.Free), "The disk holding "+p+" has "+humanBytes(du.Free)+" free"), ev)
		}
		return b.bad(Fail,
			i18n.T("Ổ đĩa chứa "+p+" chỉ còn trống "+humanBytes(du.Free)+" (cần ít nhất "+humanBytes(minBytes)+")",
				"The disk holding "+p+" has only "+humanBytes(du.Free)+" free (at least "+humanBytes(minBytes)+" needed)"),
			i18n.T("Hết chỗ thì SQLite của tpan, log và bản sao lưu không ghi được nữa.",
				"Without free space tpan's SQLite database, logs and backups can no longer be written."),
			[]string{
				line("du", "-xh", "--max-depth=1", "/var"),
				line("apt-get", "clean"),
				line("journalctl", "--vacuum-size=200M"),
				line("df", "-h", p),
			}, ev)
	})
}

// certWarnDays: dưới số ngày này chứng chỉ panel bị cảnh báo (mục 6.7).
const certWarnDays = 14

// PanelCert (ID "tpan.cert:<path>", StageTPAN): đọc chứng chỉ PEM của panel,
// parse bằng crypto/x509, tính số ngày còn lại so với now(). Dưới 14 ngày:
// Warn; hết hạn: Fail; không có file: Unknown (máy chưa chạy tpan setup, hoặc
// --demo). now nil thì dùng time.Now.
func PanelCert(p string, now func() time.Time) Check {
	if now == nil {
		now = time.Now
	}
	b := base{id: "tpan.cert:" + p, stage: StageTPAN,
		title: i18n.T("Chứng chỉ HTTPS của panel còn hạn", "The panel's HTTPS certificate is not expiring")}
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		ev := Evidence{Command: "parse " + p + " (tpan)"}
		data, err := sys.ReadFile(p)
		if errors.Is(err, fs.ErrNotExist) {
			ev.ExitCode, ev.Output = 1, "không có file / no such file"
			return b.unknown(i18n.T("không có "+p+" (máy chưa chạy tpan setup, hoặc đang ở chế độ --demo)",
				p+" is missing (tpan setup has not run, or this is --demo mode)"), ev)
		}
		if err != nil {
			ev.ExitCode, ev.Output = -1, err.Error()
			return b.unknown(i18n.T("không đọc được file ("+err.Error()+")", "could not read the file ("+err.Error()+")"), ev)
		}
		check := line("openssl", "x509", "-noout", "-enddate", "-in", p)
		cert, perr := firstCert(data)
		if perr != nil {
			ev.ExitCode, ev.Output = 1, perr.Error()
			return b.bad(Fail,
				i18n.T(p+" không phải chứng chỉ đọc được", p+" is not a readable certificate"),
				i18n.T("File chứng chỉ bị hỏng hoặc bị ghi đè bằng thứ khác. Panel không dựng được HTTPS với nó.",
					"The certificate file is damaged or was overwritten with something else. The panel cannot serve HTTPS with it."),
				renewCertFix(p, check), ev)
		}
		left := cert.NotAfter.Sub(now())
		days := int(left / (24 * time.Hour))
		ev.Output = fmt.Sprintf("subject=%s notAfter=%s còn %d ngày", cert.Subject.String(), cert.NotAfter.UTC().Format(time.RFC3339), days)
		switch {
		case left <= 0:
			return b.bad(Fail,
				i18n.T("Chứng chỉ của panel đã hết hạn lúc "+cert.NotAfter.UTC().Format("2006-01-02"), "The panel's certificate expired on "+cert.NotAfter.UTC().Format("2006-01-02")),
				i18n.T("Chứng chỉ tự ký của panel đã quá hạn: trình duyệt và lệnh gọi API sẽ từ chối kết nối HTTPS.",
					"The panel's self-signed certificate is past its date: browsers and API clients will refuse the HTTPS connection."),
				renewCertFix(p, check), ev)
		case left < certWarnDays*24*time.Hour:
			return b.bad(Warn,
				i18n.T(fmt.Sprintf("Chứng chỉ của panel còn %d ngày", days), fmt.Sprintf("The panel's certificate has %d days left", days)),
				i18n.T("Chứng chỉ sắp hết hạn. Hết hạn thì trình duyệt từ chối vào panel; nên đổi trước.",
					"The certificate is about to expire. Once it does, browsers refuse the panel; renew it first."),
				renewCertFix(p, check), ev)
		}
		return b.pass(i18n.T(fmt.Sprintf("Chứng chỉ của panel còn %d ngày", days), fmt.Sprintf("The panel's certificate has %d days left", days)), ev)
	})
}

// firstCert lấy chứng chỉ đầu tiên (khối PEM "CERTIFICATE") trong data.
func firstCert(data []byte) (*x509.Certificate, error) {
	for {
		var blk *pem.Block
		blk, data = pem.Decode(data)
		if blk == nil {
			return nil, errors.New("không có khối PEM CERTIFICATE / no PEM CERTIFICATE block")
		}
		if blk.Type == "CERTIFICATE" {
			c, err := x509.ParseCertificate(blk.Bytes)
			if err != nil {
				return nil, fmt.Errorf("đọc chứng chỉ: %w", err)
			}
			return c, nil
		}
	}
}

// renewCertFix: dời chứng chỉ cũ đi để `tpan setup` sinh cặp mới (setup giữ
// chứng chỉ đang có nếu nó còn khớp khoá), rồi khởi động lại panel.
func renewCertFix(p, check string) []string {
	return []string{line("mv", p, p+".cu"), line("tpan", "setup"), line("systemctl", "restart", "tpan"), check}
}

// FileMode (ID "tpan.perm:<path>", chặng stage): path tồn tại, không phải
// symlink, quyền đúng perm và chủ đúng owner (so như normOwner: rỗng là root).
// Sai: Fail, ManualFix là chmod/chown đúng giá trị rồi `ls -l`. Không có:
// Unknown. Dùng cho /var/lib/tpan/tpan.db (0600), /etc/tpan/tls/key.pem
// (0600), /etc/tpan (0700).
func FileMode(p string, perm uint32, owner system.Owner, stage Stage) Check {
	wantUser, wantGroup := orRoot(owner.User), orRoot(owner.Group)
	b := base{id: "tpan.perm:" + p, stage: stage,
		title: i18n.T(fmt.Sprintf("%s có quyền %04o, chủ %s:%s", p, perm, wantUser, wantGroup),
			fmt.Sprintf("%s has mode %04o, owner %s:%s", p, perm, wantUser, wantGroup))}
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		ev := Evidence{Command: "stat " + p + " (tpan)"}
		// -d: nếu p là thư mục (/etc/tpan) thì `ls -l` sẽ liệt kê ruột nó chứ không cho thấy quyền của chính nó.
		recheck := line("ls", "-ld", p)
		info, err := sys.Stat(p)
		if errors.Is(err, fs.ErrNotExist) {
			ev.ExitCode, ev.Output = 1, "không có file / no such file"
			return b.unknown(i18n.T("không có "+p, p+" does not exist"), ev)
		}
		if err != nil {
			ev.ExitCode, ev.Output = -1, err.Error()
			return b.unknown(i18n.T("không đọc được thông tin ("+err.Error()+")", "could not stat it ("+err.Error()+")"), ev)
		}
		gotUser, gotGroup := orRoot(info.Owner.User), orRoot(info.Owner.Group)
		gotPerm := uint32(info.Mode.Perm())
		ev.Output = fmt.Sprintf("mode %04o, chủ %s:%s", gotPerm, gotUser, gotGroup)
		if info.Mode&fs.ModeSymlink != 0 {
			ev.Output = "symlink, " + ev.Output
			return b.bad(Fail,
				i18n.T(p+" là symlink", p+" is a symlink"),
				i18n.T("Chỗ này phải là file hay thư mục thật do tpan tạo. Symlink có thể dẫn quyền và nội dung sang chỗ khác mà tpan không kiểm.",
					"This must be a real file or directory made by tpan. A symlink can point permissions and content somewhere tpan does not check."),
				[]string{line("readlink", "-f", p), recheck}, ev)
		}
		var vi, en []string
		var fix []string
		if gotPerm != perm {
			vi = append(vi, fmt.Sprintf("quyền là %04o, phải là %04o", gotPerm, perm))
			en = append(en, fmt.Sprintf("mode is %04o, it must be %04o", gotPerm, perm))
			fix = append(fix, line("chmod", fmt.Sprintf("%04o", perm), p))
		}
		if gotUser != wantUser || gotGroup != wantGroup {
			vi = append(vi, fmt.Sprintf("chủ là %s:%s, phải là %s:%s", gotUser, gotGroup, wantUser, wantGroup))
			en = append(en, fmt.Sprintf("owner is %s:%s, it must be %s:%s", gotUser, gotGroup, wantUser, wantGroup))
			fix = append(fix, line("chown", wantUser+":"+wantGroup, p))
		}
		if len(vi) == 0 {
			return b.pass(i18n.T(fmt.Sprintf("%s: quyền %04o, chủ %s:%s đúng", p, perm, wantUser, wantGroup),
				fmt.Sprintf("%s: mode %04o, owner %s:%s are correct", p, perm, wantUser, wantGroup)), ev)
		}
		return b.bad(Fail,
			i18n.T(p+": "+strings.Join(vi, "; "), p+": "+strings.Join(en, "; ")),
			i18n.T("Quyền hoặc chủ bị đổi. Với file chứa khoá, mật khẩu băm hay dữ liệu của panel, quyền rộng hơn nghĩa là user khác trên máy đọc được.",
				"The mode or owner was changed. For files holding keys, password hashes or panel data, a wider mode means other users on the machine can read them."),
			append(fix, recheck), ev)
	})
}

// TPANDatabase (ID "tpan.db:integrity", StageTPAN): gọi integrity (PRAGMA
// integrity_check). "ok": Pass; chuỗi khác: Fail, Evidence là nguyên văn;
// integrity trả lỗi: Unknown.
func TPANDatabase(integrity func(ctx context.Context) (string, error)) Check {
	const db = "/var/lib/tpan/tpan.db"
	b := base{id: "tpan.db:integrity", stage: StageTPAN,
		title: i18n.T("Cơ sở dữ liệu của tpan toàn vẹn", "tpan's own database is intact")}
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		ev := Evidence{Command: "PRAGMA integrity_check (tpan)"}
		if integrity == nil {
			return b.unknown(i18n.T("chưa nối với cơ sở dữ liệu", "no database is connected"))
		}
		out, err := integrity(ctx)
		if err != nil {
			ev.ExitCode, ev.Output = -1, clip(err.Error())
			return b.unknown(i18n.T("không chạy được PRAGMA integrity_check ("+err.Error()+")", "could not run PRAGMA integrity_check ("+err.Error()+")"), ev)
		}
		ev.Output = clip(out)
		if strings.TrimSpace(out) == "ok" {
			return b.pass(i18n.T("PRAGMA integrity_check trả ok", "PRAGMA integrity_check returned ok"), ev)
		}
		ev.ExitCode = 1
		return b.bad(Fail,
			i18n.T("PRAGMA integrity_check báo lỗi: "+firstLineWith(out), "PRAGMA integrity_check reported a problem: "+firstLineWith(out)),
			i18n.T("File tpan.db bị hỏng (mất điện giữa lúc ghi, đĩa lỗi, hoặc bị ghi đè). Site vẫn chạy, nhưng tpan có thể mất lịch sử và cài đặt.",
				"tpan.db is damaged (power loss during a write, a bad disk, or an overwrite). Sites keep running, but tpan may lose its history and settings."),
			[]string{
				line("cp", "-a", db, db+".hong"),
				line("apt-get", "install", "-y", "sqlite3"),
				line("sqlite3", db, "PRAGMA integrity_check"),
			}, ev)
	})
}

// StuckJob mô tả một job treo cho phép kiểm TPANJobs.
type StuckJob struct {
	ID      string
	Kind    string
	Target  string
	Started time.Time
}

// maxJobsInSummary: Summary chỉ liệt kê chừng này job; phần còn lại nằm trong Evidence.
const maxJobsInSummary = 5

// TPANJobs (ID "tpan.jobs:stuck", StageTPAN): stuck trả các job còn
// "running" quá lâu. Rỗng: Pass; có: Fail, Summary liệt kê id và loại job.
// stuck lỗi: Unknown.
func TPANJobs(stuck func(ctx context.Context) ([]StuckJob, error)) Check {
	b := base{id: "tpan.jobs:stuck", stage: StageTPAN,
		title: i18n.T("Không có job nào bị treo", "No job is stuck")}
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		ev := Evidence{Command: "jobs still running (tpan)"}
		if stuck == nil {
			return b.unknown(i18n.T("chưa nối với cơ sở dữ liệu", "no database is connected"))
		}
		jobs, err := stuck(ctx)
		if err != nil {
			ev.ExitCode, ev.Output = -1, clip(err.Error())
			return b.unknown(i18n.T("không đọc được danh sách job ("+err.Error()+")", "could not read the job list ("+err.Error()+")"), ev)
		}
		if len(jobs) == 0 {
			ev.Output = "không có / none"
			return b.pass(i18n.T("Không có job nào chạy quá lâu", "No job has been running for too long"), ev)
		}
		var lines, short []string
		for i, j := range jobs {
			desc := j.ID + " (" + j.Kind + ")"
			lines = append(lines, fmt.Sprintf("%s %s %s bắt đầu / started %s", j.ID, j.Kind, j.Target, j.Started.UTC().Format(time.RFC3339)))
			if i < maxJobsInSummary {
				short = append(short, desc)
			}
		}
		list := strings.Join(short, ", ")
		if len(jobs) > maxJobsInSummary {
			list += fmt.Sprintf(" và %d job nữa", len(jobs)-maxJobsInSummary)
		}
		listEn := strings.Join(short, ", ")
		if len(jobs) > maxJobsInSummary {
			listEn += fmt.Sprintf(" and %d more", len(jobs)-maxJobsInSummary)
		}
		ev.ExitCode, ev.Output = 1, clip(strings.Join(lines, "\n"))
		return b.bad(Fail,
			i18n.T(fmt.Sprintf("%d job chạy quá lâu: %s", len(jobs), list), fmt.Sprintf("%d job(s) running for too long: %s", len(jobs), listEn)),
			i18n.T("Job đã bắt đầu nhưng không thấy kết thúc: tiến trình tpan có thể bị tắt giữa chừng, hoặc một lệnh bên trong bị treo. Job treo giữ khoá nên thao tác mới có thể bị từ chối.",
				"A job started but never finished: the tpan process may have been killed midway, or a command inside it hangs. A stuck job holds the lock, so new operations may be refused."),
			[]string{
				line("journalctl", "-u", "tpan", "-n", "100", "--no-pager"),
				line("systemctl", "status", "tpan"),
				line("tpan", "health"),
			}, ev)
	})
}

// TPANScheduler (ID "tpan.scheduler:health", StageTPAN): lastBeat trả lần
// gần nhất bộ hẹn giờ health trong `tpan serve` chạy (ok=false: chưa bao giờ).
// Cách now() không quá maxAge: Pass; quá, hoặc chưa bao giờ: Warn ("tpan serve
// có đang chạy không?", ManualFix: systemctl status tpan). lastBeat lỗi:
// Unknown. now nil thì dùng time.Now.
func TPANScheduler(lastBeat func(ctx context.Context) (t time.Time, ok bool, err error), maxAge time.Duration, now func() time.Time) Check {
	if now == nil {
		now = time.Now
	}
	b := base{id: "tpan.scheduler:health", stage: StageTPAN,
		title: i18n.T("Bộ hẹn giờ health của tpan còn chạy", "tpan's health scheduler is still running")}
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		ev := Evidence{Command: "last health run (tpan)"}
		if lastBeat == nil {
			return b.unknown(i18n.T("chưa nối với cơ sở dữ liệu", "no database is connected"))
		}
		t, ok, err := lastBeat(ctx)
		if err != nil {
			ev.ExitCode, ev.Output = -1, clip(err.Error())
			return b.unknown(i18n.T("không đọc được nhịp của bộ hẹn giờ ("+err.Error()+")", "could not read the scheduler's heartbeat ("+err.Error()+")"), ev)
		}
		cause := i18n.T("`tpan serve` có đang chạy không? Bộ hẹn giờ nằm trong tiến trình đó; nó tắt thì không ai kiểm máy định kỳ và sự cố mới không được phát hiện.",
			"Is `tpan serve` running? The scheduler lives in that process; if it is down nobody checks the machine periodically and new incidents go unnoticed.")
		fix := []string{line("journalctl", "-u", "tpan", "-n", "50", "--no-pager"), line("systemctl", "status", "tpan")}
		if !ok {
			ev.ExitCode, ev.Output = 1, "chưa bao giờ / never"
			return b.bad(Warn,
				i18n.T("Bộ hẹn giờ health chưa chạy lần nào", "The health scheduler has never run"), cause, fix, ev)
		}
		age := now().Sub(t)
		ev.Output = fmt.Sprintf("lần gần nhất / last: %s, cách đây / ago: %s", t.UTC().Format(time.RFC3339), age.Round(time.Second))
		if age > maxAge {
			mins, maxMins := int(age/time.Minute), int(maxAge/time.Minute)
			return b.bad(Warn,
				i18n.T(fmt.Sprintf("Bộ hẹn giờ health chạy lần cuối %d phút trước (quá %d phút)", mins, maxMins),
					fmt.Sprintf("The health scheduler last ran %d minutes ago (more than %d)", mins, maxMins)),
				cause, fix, ev)
		}
		return b.pass(i18n.T("Bộ hẹn giờ health vẫn chạy", "The health scheduler is running"), ev)
	})
}
