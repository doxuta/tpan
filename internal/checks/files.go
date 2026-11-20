package checks

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strings"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

// Ba phép kiểm dưới đây không chạy lệnh nào: chúng đọc file hoặc dung lượng qua
// system.System bằng Go. Evidence.Command vì thế là nhãn "(tpan)" nói thật việc
// đã làm, không giả làm một lệnh shell chưa từng chạy. Dòng cuối của ManualFix
// vẫn là lệnh thật để người vận hành tự kiểm lại.

const wpPath = "/usr/local/bin/wp"

var sha512RE = regexp.MustCompile(`^[0-9a-fA-F]{128}$`)

// WPCLI kiểm /usr/local/bin/wp: là file thường, quyền 0755 và sha512 khớp
// giá trị mong đợi (core lấy từ bản đã ghim). Tính sha512 bằng Go qua ReadFile,
// không gọi sha512sum, nên chạy giống hệt trên FakeSystem lẫn máy thật.
//
// Stat không đi theo symlink: nếu /usr/local/bin/wp là một symlink thì phép
// kiểm báo lỗi. Chủ ý: tpan tự tải về một file thường, symlink nghĩa là có ai khác đặt vào.
func WPCLI(sha512hex string) Check {
	b := base{id: "wpcli.binary:wp", stage: StageMachine,
		title: i18n.T("WP-CLI có sẵn, đúng quyền và đúng mã sha512", "WP-CLI is present, with the right mode and sha512")}
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		if !sha512RE.MatchString(sha512hex) {
			return b.unknown(i18n.T("mã sha512 mong đợi phải gồm 128 ký tự hex", "the expected sha512 must be 128 hex characters"))
		}
		want := strings.ToLower(sha512hex)
		statEv := Evidence{Command: "stat " + wpPath + " (tpan)"}

		info, err := sys.Stat(wpPath)
		if errors.Is(err, fs.ErrNotExist) {
			statEv.ExitCode, statEv.Output = 1, "No such file or directory"
			return b.bad(Fail,
				i18n.T("WP-CLI chưa được cài (không có "+wpPath+")", "WP-CLI is not installed ("+wpPath+" is missing)"),
				i18n.T("Chưa cài hoặc file đã bị xoá. Không có WP-CLI thì không tạo được site WordPress.",
					"It was never installed, or the file was deleted. Without WP-CLI no WordPress site can be created."),
				[]string{line("tpan", "stack", "install"), line("ls", "-l", wpPath)}, statEv)
		}
		if err != nil {
			statEv.ExitCode, statEv.Output = -1, err.Error()
			return b.unknown(i18n.T("không đọc được thông tin file ("+err.Error()+")", "could not stat the file ("+err.Error()+")"), statEv)
		}
		statEv.Output = fmt.Sprintf("mode %04o, %d bytes, owner %s:%s", uint32(info.Mode.Perm()), info.Size, orRoot(info.Owner.User), orRoot(info.Owner.Group))
		if !info.Mode.IsRegular() {
			return b.bad(Fail,
				i18n.T(wpPath+" không phải file thường", wpPath+" is not a regular file"),
				i18n.T("Có thứ khác (symlink, thư mục) nằm ở chỗ của WP-CLI. TPAN chỉ tin một file thường do chính nó tải về.",
					"Something else (a symlink, a directory) sits where WP-CLI should be. TPAN only trusts a regular file it downloaded itself."),
				[]string{line("ls", "-l", wpPath), line("tpan", "stack", "install"), line("ls", "-l", wpPath)}, statEv)
		}

		data, err := sys.ReadFile(wpPath)
		shaEv := Evidence{Command: "sha512sum " + wpPath + " (tpan)"}
		if err != nil {
			shaEv.ExitCode, shaEv.Output = -1, err.Error()
			return b.unknown(i18n.T("không đọc được file ("+err.Error()+")", "could not read the file ("+err.Error()+")"), statEv, shaEv)
		}
		sum := sha512.Sum512(data)
		got := hex.EncodeToString(sum[:])
		shaEv.Output = got + "  " + wpPath + "\nexpected: " + want

		var vi, en, fix []string
		if info.Mode.Perm() != 0o755 {
			vi = append(vi, fmt.Sprintf("quyền là %04o, phải là 0755", uint32(info.Mode.Perm())))
			en = append(en, fmt.Sprintf("mode is %04o, it must be 0755", uint32(info.Mode.Perm())))
			fix = append(fix, line("chmod", "755", wpPath))
		}
		if got != want {
			vi = append(vi, "mã sha512 không khớp bản đã ghim")
			en = append(en, "the sha512 does not match the pinned version")
			fix = append(fix, line("tpan", "stack", "install"))
		}
		if len(vi) == 0 {
			return b.pass(i18n.T("WP-CLI có sẵn, quyền 0755, sha512 khớp", "WP-CLI is present, mode 0755, sha512 matches"), statEv, shaEv)
		}
		return b.bad(Fail,
			i18n.T("WP-CLI có vấn đề: "+strings.Join(vi, "; "), "WP-CLI has a problem: "+strings.Join(en, "; ")),
			i18n.T("File WP-CLI bị đổi hoặc tải dở. Chạy một file không đúng bản đã ghim là chạy mã lạ bằng quyền root.",
				"The WP-CLI file was changed or half downloaded. Running a file that is not the pinned version means running unknown code as root."),
			append(fix, line("sha512sum", wpPath)), statEv, shaEv)
	})
}

func orRoot(s string) string {
	if s == "" {
		return "root"
	}
	return s
}

// Ngưỡng đĩa theo mục 6.7: cảnh báo từ 80%, lỗi từ 90%.
const (
	diskWarnPct = 80.0
	diskFailPct = 90.0
)

// Disk kiểm mức dùng của phân vùng chứa path: dưới 80% Pass, từ 80% tới dưới
// 90% Warn, từ 90% Fail. Dùng DiskUsage.Free (phần user thường dùng được, như
// cột Avail của df) nên phần dành riêng cho root không làm đĩa trông trống hơn thực tế.
func Disk(p string) Check {
	b := base{id: "disk.usage:" + p, stage: StageMachine,
		title: i18n.T("Ổ đĩa chứa "+p+" còn chỗ trống", "The disk holding "+p+" has free space")}
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
		pct := float64(du.Total-du.Free) / float64(du.Total) * 100
		ev.Output = fmt.Sprintf("total=%d free=%d used=%.1f%%", du.Total, du.Free, pct)
		fix := []string{
			line("du", "-xh", "--max-depth=1", "/var"),
			line("apt-get", "clean"),
			line("journalctl", "--vacuum-size=200M"),
			line("df", "-h", p),
		}
		switch {
		case pct >= diskFailPct:
			return b.bad(Fail,
				i18n.T(fmt.Sprintf("Ổ đĩa chứa %s đã dùng %.1f%%, gần đầy", p, pct), fmt.Sprintf("The disk holding %s is %.1f%% used, almost full", p, pct)),
				i18n.T("Đĩa gần đầy: MariaDB, log và backup đều có thể ngừng ghi được, site lỗi theo.",
					"The disk is nearly full: MariaDB, logs and backups may all stop writing, and sites fail with them."),
				fix, ev)
		case pct >= diskWarnPct:
			return b.bad(Warn,
				i18n.T(fmt.Sprintf("Ổ đĩa chứa %s đã dùng %.1f%%", p, pct), fmt.Sprintf("The disk holding %s is %.1f%% used", p, pct)),
				i18n.T("Đĩa sắp đầy. Dọn bớt log, bản sao lưu cũ hoặc gói đã tải trước khi chạm 90%.",
					"The disk is filling up. Clear old logs, backups or downloaded packages before it reaches 90%."),
				fix, ev)
		}
		return b.pass(i18n.T(fmt.Sprintf("Ổ đĩa chứa %s đã dùng %.1f%%", p, pct), fmt.Sprintf("The disk holding %s is %.1f%% used", p, pct)), ev)
	})
}

// OSRelease kiểm máy đúng là bản hệ điều hành TPAN hỗ trợ, bằng cách đọc
// /etc/os-release (ID và VERSION_ID), ví dụ OSRelease("ubuntu", "24.04").
// File này thiếu (không phải Linux kiểu systemd) tính là Fail; đọc không được
// vì lý do khác là Unknown.
func OSRelease(id, versionID string) Check {
	const file = "/etc/os-release"
	b := base{id: "os.release:" + id + "-" + versionID, stage: StageMachine,
		title: i18n.T("Hệ điều hành là "+id+" "+versionID, "The operating system is "+id+" "+versionID)}
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		ev := Evidence{Command: "cat " + file + " (tpan)"}
		fix := []string{line("cat", file)}
		why := i18n.T("TPAN chỉ được thử trên "+id+" "+versionID+". Hệ điều hành khác có tên gói và đường dẫn khác, nên cài vào dễ hỏng.",
			"TPAN has only been tested on "+id+" "+versionID+". Other systems use other package names and paths, so installing there is likely to break.")
		data, err := sys.ReadFile(file)
		if errors.Is(err, fs.ErrNotExist) {
			ev.ExitCode, ev.Output = 1, "No such file or directory"
			return b.bad(Fail, i18n.T("Không có "+file+": không nhận ra hệ điều hành", file+" is missing: the operating system is not recognised"), why, fix, ev)
		}
		if err != nil {
			ev.ExitCode, ev.Output = -1, err.Error()
			return b.unknown(i18n.T("không đọc được "+file+" ("+err.Error()+")", "could not read "+file+" ("+err.Error()+")"), ev)
		}
		ev.Output = clip(strings.TrimRight(string(data), "\n"))
		kv := parseOSRelease(string(data))
		if kv["ID"] == id && kv["VERSION_ID"] == versionID {
			return b.pass(i18n.T("Đây là "+id+" "+versionID, "This is "+id+" "+versionID), ev)
		}
		gotVi, gotEn := orUnknown(kv["ID"])+" "+orUnknown(kv["VERSION_ID"]), orUnknown(kv["ID"])+" "+orUnknown(kv["VERSION_ID"])
		return b.bad(Fail,
			i18n.T("Máy đang chạy "+gotVi+", không phải "+id+" "+versionID, "This machine runs "+gotEn+", not "+id+" "+versionID),
			why, fix, ev)
	})
}

func orUnknown(s string) string {
	if s == "" {
		return "?"
	}
	return s
}

// parseOSRelease đọc các dòng KEY=VALUE của os-release; VALUE có thể nằm trong
// nháy kép hoặc nháy đơn (VERSION_ID="24.04").
func parseOSRelease(s string) map[string]string {
	kv := map[string]string{}
	for _, ln := range strings.Split(s, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		k, v, ok := strings.Cut(ln, "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		kv[strings.TrimSpace(k)] = v
	}
	return kv
}
