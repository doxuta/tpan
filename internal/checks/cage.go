package checks

// Phép kiểm của lồng theo site (M17, mục 3.7 của docs/thay-doi/M17-thiet-ke.md).
// Sáu phép kiểm mới và hai hàm gom (SiteChecksCaged, SiteChecksSSLCaged). Mọi
// phép kiểm ở đây chỉ đọc; ID đúng dạng "<loại>:<domain>" của hợp đồng, chặng PHP
// (riêng probe.leftover là chặng tpan). AutoFix để trống: core điền theo ID.

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/cage"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
)

var (
	// cagePidRE: tên thư mục tiến trình trong /proc (bỏ self, thread-self, sys...).
	cagePidRE = regexp.MustCompile(`^[0-9]{1,10}$`)
	// cageUIDRE: uid in ra bởi `id -u`.
	cageUIDRE = regexp.MustCompile(`^[0-9]{1,10}$`)
)

// cageMaxList là số mục tối đa đưa vào một dòng bằng chứng hay lệnh gợi ý.
const cageMaxList = 10

// cageGuard như siteGuard, thêm: slug có, và tên unit, slice hợp lệ với systemctl
// (tên bắt đầu bằng "-" sẽ thành cờ). Trả nil khi ổn.
func cageGuard(b base, l site.Layout, needUser bool, paths ...string) Check {
	if g := siteGuard(b, l, needUser, paths...); g != nil {
		return g
	}
	why := i18n.Text{}
	if l.Slug.IsZero() {
		why = i18n.T("thông tin site không có slug", "the site layout has no slug")
	}
	for _, u := range []string{l.CageSocketUnit(), l.CageServiceUnit(), l.CageSlice()} {
		if !unitNameRE.MatchString(u) {
			why = i18n.T("tên unit của lồng không hợp lệ (\""+u+"\")", "the cage unit name is not valid (\""+u+"\")")
		}
	}
	if why.IsZero() {
		return nil
	}
	return checkFunc(func(context.Context, system.System) Result { return b.unknown(why) })
}

// cageProblems gom các điều sai của một phép kiểm, mỗi điều một câu hai thứ tiếng.
type cageProblems struct{ vi, en []string }

func (p *cageProblems) add(vi, en string) { p.vi, p.en = append(p.vi, vi), append(p.en, en) }
func (p *cageProblems) any() bool         { return len(p.vi) > 0 }
func (p *cageProblems) text(prefixVi, prefixEn string) i18n.Text {
	return i18n.T(prefixVi+strings.Join(p.vi, "; "), prefixEn+strings.Join(p.en, "; "))
}

// cageUnitState chạy `systemctl is-active <unit>` và trả chữ trạng thái (active,
// inactive, failed, activating...). Mã thoát 3 của lệnh này là câu trả lời bình
// thường, không phải lỗi. state rỗng khi output không giống một trạng thái.
func cageUnitState(ctx context.Context, sys system.System, unit string) (state string, ev Evidence, err error) {
	res, ev, err := execCmd(ctx, sys, system.Cmd{Name: "systemctl", Args: []string{"is-active", unit}})
	if err != nil {
		return "", ev, err
	}
	first, _, _ := strings.Cut(strings.TrimSpace(res.Stdout), "\n")
	if first = strings.TrimSpace(first); stateRE.MatchString(first) {
		state = first
	}
	return state, ev, nil
}

// cageGone cho biết lỗi đọc /proc/<pid>/... là "tiến trình đã thoát": ENOENT
// khi mở, hoặc ESRCH khi đọc giữa chừng (không phải fs.ErrNotExist).
func cageGone(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ESRCH)
}

// ---- cage.socket ----

// CageSocket: "cage.socket:<domain>", chặng PHP. File l.CageSocket() là
// socket www-data:www-data 0660; unit socket active (`systemctl is-active`);
// dịch vụ không failed. Sai một điều: Fail (sửa tự động cage.start).
//
// Chỉ kiểm thứ systemd giữ. Dịch vụ PHP của lồng KHÔNG cần đang chạy: nó được
// đánh thức bởi request đầu tiên (socket activation), nên "inactive" là bình thường.
func CageSocket(l site.Layout) Check {
	b := siteBase("cage.socket", StagePHP, l,
		i18n.T("Socket của lồng có, unit socket đang chạy và dịch vụ PHP không hỏng", "The cage socket exists, its socket unit is running and the PHP service is not failed"))
	sock := l.CageSocket()
	if g := cageGuard(b, l, false, sock); g != nil {
		return g
	}
	sockUnit, svcUnit := l.CageSocketUnit(), l.CageServiceUnit()
	recheck := line("ls", "-l", sock)
	const wantMode = fs.FileMode(0o660)

	return checkFunc(func(ctx context.Context, sys system.System) Result {
		info, statEv, missing, err := siteStat(sys, sock)
		if err != nil {
			return siteStatUnknown(b, sock, err, statEv)
		}
		evs := []Evidence{statEv}
		sockState, ev, err := cageUnitState(ctx, sys, sockUnit)
		evs = append(evs, ev)
		if err != nil {
			return b.cantRun(err, evs...)
		}
		svcState, ev, err := cageUnitState(ctx, sys, svcUnit)
		evs = append(evs, ev)
		if err != nil {
			return b.cantRun(err, evs...)
		}
		switch {
		case sockState == "":
			return b.unknown(i18n.T("systemctl is-active "+sockUnit+" không trả một trạng thái đọc được", "systemctl is-active "+sockUnit+" did not return a readable state"), evs...)
		case svcState == "":
			return b.unknown(i18n.T("systemctl is-active "+svcUnit+" không trả một trạng thái đọc được", "systemctl is-active "+svcUnit+" did not return a readable state"), evs...)
		}

		var p cageProblems
		switch {
		case missing:
			p.add("không có file socket "+sock, "the socket file "+sock+" is missing")
		case info.Mode&fs.ModeSocket == 0:
			p.add(sock+" tồn tại nhưng không phải socket", sock+" exists but is not a socket")
		default:
			if u, g := orRoot(info.Owner.User), orRoot(info.Owner.Group); u != "www-data" || g != site.WebGroup {
				p.add("chủ socket là "+u+":"+g+", phải là www-data:"+site.WebGroup, "socket owner is "+u+":"+g+", it must be www-data:"+site.WebGroup)
			}
			if info.Mode.Perm() != wantMode {
				p.add(fmt.Sprintf("quyền socket là %04o, phải là 0660", uint32(info.Mode.Perm())), fmt.Sprintf("socket mode is %04o, it must be 0660", uint32(info.Mode.Perm())))
			}
		}
		if sockState != "active" {
			p.add("unit socket "+sockUnit+" ở trạng thái "+sockState+", phải là active", "the socket unit "+sockUnit+" is "+sockState+", it must be active")
		}
		if svcState == "failed" {
			p.add("dịch vụ "+svcUnit+" đang ở trạng thái failed", "the service "+svcUnit+" is in the failed state")
		}
		if !p.any() {
			return b.pass(i18n.T("Socket của lồng có (www-data:"+site.WebGroup+", 0660), unit socket active, dịch vụ PHP "+svcState,
				"The cage socket exists (www-data:"+site.WebGroup+", 0660), the socket unit is active, the PHP service is "+svcState), evs...)
		}
		return b.bad(Fail, p.text("Socket của lồng sai: ", "The cage socket is wrong: "),
			i18n.T("Web server nói chuyện với PHP của site qua socket này, và systemd giữ nó (không phải site). Socket mất, sai chủ, sai quyền, unit socket dừng hay dịch vụ đã thành failed (chạy lỗi quá nhiều lần) thì web server không chuyển được request PHP: site trả lỗi 502.",
				"The web server talks to the site's PHP through this socket, and systemd (not the site) holds it. If the socket is gone, has the wrong owner or mode, its unit is stopped or the service is failed (it crashed too many times), the web server cannot pass PHP requests on: the site answers 502."),
			[]string{line("systemctl", "status", sockUnit, svcUnit, "--no-pager"),
				line("journalctl", "-u", svcUnit, "-u", sockUnit, "--no-pager", "-n", "20"),
				line("systemctl", "reset-failed", sockUnit, svcUnit),
				line("systemctl", "enable", "--now", sockUnit),
				recheck}, evs...)
	})
}

// ---- cage.runtime ----

// cageStray là một tiến trình cùng uid với site mà nằm ngoài slice của site.
type cageStray struct{ pid, cgroup string }

// cageInSlice cho biết nội dung /proc/<pid>/cgroup có đường dẫn nằm dưới slice
// (cgroup v2: "0::/tpan.slice/tpan-<slug>.slice/tpan-cage-<slug>-php.service").
func cageInSlice(cgroupFile, slice string) bool {
	for _, ln := range strings.Split(cgroupFile, "\n") {
		p := ln
		if i := strings.LastIndex(ln, ":"); i >= 0 {
			p = ln[i+1:]
		}
		if strings.Contains(p+"/", "/"+slice+"/") {
			return true
		}
	}
	return false
}

// cageUIDOf đọc uid thật (trường đầu của dòng "Uid:") từ nội dung /proc/<pid>/status.
// Cùng trường với steps.CageReap, để phép kiểm báo lỗi thì sửa tự động gỡ được đúng thứ đó.
func cageUIDOf(status string) string {
	f := strings.Fields(procField(status, "Uid"))
	if len(f) == 0 {
		return ""
	}
	return f[0]
}

// procField trả phần sau "Key:" của dòng đầu tiên mang khoá đó trong file dạng
// /proc/<pid>/status ("Key:\tgiá trị"); không có thì rỗng.
func procField(status, key string) string {
	for _, ln := range strings.Split(status, "\n") {
		if k, v, ok := strings.Cut(ln, ":"); ok && k == key {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// cageScanStrays đọc /proc bằng System (không chạy lệnh): mọi tiến trình có uid
// là uid thì phải nằm dưới slice. Tiến trình thoát giữa chừng thì bỏ qua.
func cageScanStrays(sys system.System, uid, slice string) (strays []cageStray, scanned int, err error) {
	names, err := sys.ReadDir("/proc")
	if err != nil {
		return nil, 0, err
	}
	for _, n := range names {
		if !cagePidRE.MatchString(n) {
			continue
		}
		st, err := sys.ReadFile("/proc/" + n + "/status")
		if err != nil {
			continue // đã thoát giữa chừng
		}
		scanned++
		if cageUIDOf(string(st)) != uid {
			continue
		}
		cg, err := sys.ReadFile("/proc/" + n + "/cgroup")
		if err != nil || cageInSlice(string(cg), slice) {
			continue
		}
		first, _, _ := strings.Cut(strings.TrimSpace(string(cg)), "\n")
		strays = append(strays, cageStray{pid: n, cgroup: first})
	}
	return strays, scanned, nil
}

// cageMount là một dòng /proc/<pid>/mountinfo: thư mục gốc của lần gắn (trường
// 4), điểm gắn (trường 5), loại hệ thống file (sau dấu " - ").
type cageMount struct{ root, point, fstype string }

func cageParseMounts(s string) []cageMount {
	var out []cageMount
	for _, ln := range strings.Split(s, "\n") {
		i := strings.Index(ln, " - ")
		if i < 0 {
			continue
		}
		pre, post := strings.Fields(ln[:i]), strings.Fields(ln[i+3:])
		if len(pre) < 5 || len(post) < 1 {
			continue
		}
		out = append(out, cageMount{root: pre[3], point: pre[4], fstype: post[0]})
	}
	return out
}

// cageMountsAt trả các dòng gắn ở đúng điểm gắn point. Một điểm có thể có nhiều
// dòng (lớp gốc rồi lớp phủ lên), nên người dùng hàm này tự chọn "có ít nhất một".
func cageMountsAt(ms []cageMount, point string) []cageMount {
	var out []cageMount
	for _, m := range ms {
		if m.point == point {
			out = append(out, m)
		}
	}
	return out
}

// cageDeleted: thư mục nguồn của lần gắn đã bị xoá và tạo lại (mountinfo ghi
// "//deleted" ở trường gốc): lồng giữ inode cũ, không thấy thư mục mới.
func cageDeleted(ms []cageMount) bool {
	for _, m := range ms {
		if strings.Contains(m.root, "//deleted") {
			return true
		}
	}
	return false
}

// cageShowProps tách output `systemctl show` ("Khoá=giá trị" mỗi dòng).
func cageShowProps(out string) map[string]string {
	m := map[string]string{}
	for _, ln := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(ln, "="); ok {
			m[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return m
}

// cageUnixTime đọc thời điểm dạng systemd in với --timestamp=unix ("@1763700000");
// cũng nhận số trần. ok=false khi rỗng hay lạ (dịch vụ chưa từng chạy).
func cageUnixTime(s string) (int64, bool) {
	n, err := strconv.ParseInt(strings.TrimPrefix(strings.TrimSpace(s), "@"), 10, 64)
	return n, err == nil && n > 0
}

// CageRuntime: "cage.runtime:<domain>", chặng PHP (mục 3.7). Cho cả máy: không
// có tiến trình nào cùng uid với site ở ngoài slice của site (đọc /proc như
// steps.CageReap), có là Fail. Khi dịch vụ đang chạy (MainPID khác 0; không
// chạy thì phần này Pass, "lồng đang ngủ"), đọc tiến trình chính:
// /proc/<pid>/status có "NoNewPrivs:\t1" và CapEff bằng 0; /proc/<pid>/cgroup
// nằm dưới tpan-<slug>.slice; /proc/<pid>/mountinfo có tmpfs ở /var, /run,
// /etc/tpan, có dòng gắn /run/mysqld (chỉ đòi khi `systemctl is-active mariadb`
// là active), dòng gắn /run/systemd/resolve không mang "//deleted", và hai dòng
// gắn passwd, group riêng của lồng lên /etc/passwd, /etc/group. Sai một
// điều: Fail. /proc/<pid>/exe (Readlink) kết thúc bằng " (deleted)", hoặc
// `systemctl show --timestamp=unix -p ActiveEnterTimestamp` sớm hơn
// `stat -c %Y <l.CageFence()>`: Warn. Sửa tự động: cage.restart.
//
// Dòng gắn /run/systemd/resolve: không đòi phải có (máy không chạy
// systemd-resolved thì lệnh gắn có dấu "-" bỏ qua); có mà mang "//deleted" thì Fail.
func CageRuntime(l site.Layout) Check {
	b := siteBase("cage.runtime", StagePHP, l,
		i18n.T("Lồng đang chạy đúng hàng rào: không tiến trình lạc ngoài lồng, đủ quyền bị cắt, đủ thư mục riêng",
			"The cage runs inside its fence: no stray process outside the cage, privileges dropped, private directories in place"))
	if g := cageGuard(b, l, true); g != nil {
		return g
	}
	svc, slice := l.CageServiceUnit(), l.CageSlice()
	psCmd := line("ps", "-u", l.User, "-o", "pid,cgroup,args")
	statusCmd := line("systemctl", "status", svc, "--no-pager")

	return checkFunc(func(ctx context.Context, sys system.System) Result {
		var evs []Evidence

		// 1. Cả máy: uid của site, rồi mọi tiến trình mang uid đó phải ở trong slice.
		idRes, ev, err := execCmd(ctx, sys, system.Cmd{Name: "id", Args: []string{"-u", l.User}})
		evs = append(evs, ev)
		if err != nil {
			return b.cantRun(err, evs...)
		}
		uid := strings.TrimSpace(idRes.Stdout)
		if idRes.ExitCode != 0 || !cageUIDRE.MatchString(uid) {
			return b.unknown(i18n.T("không lấy được uid của user "+l.User, "could not get the uid of user "+l.User), evs...)
		}
		strays, scanned, err := cageScanStrays(sys, uid, slice)
		scanEv := Evidence{Command: "scan /proc for uid " + uid + " outside " + slice + " (tpan)"}
		if err != nil {
			scanEv.ExitCode, scanEv.Output = -1, err.Error()
			return b.unknown(i18n.T("không đọc được /proc ("+err.Error()+")", "could not read /proc ("+err.Error()+")"), append(evs, scanEv)...)
		}
		scanEv.Output = fmt.Sprintf("%d processes read, %d with uid %s outside %s", scanned, len(strays), uid, slice)
		for i, s := range strays {
			if i == cageMaxList {
				scanEv.Output += fmt.Sprintf("\n... and %d more", len(strays)-cageMaxList)
				break
			}
			scanEv.Output += "\npid " + s.pid + " " + s.cgroup
		}
		if len(strays) > 0 {
			scanEv.ExitCode = 1
		}
		evs = append(evs, scanEv)

		var fails, warns cageProblems
		var unclear []string // lý do chưa kiểm được (một câu, tiếng Anh ngắn kèm tiếng Việt qua i18n khi dùng)
		fixes := []string{}
		recheck := statusCmd
		if len(strays) > 0 {
			fails.add(fmt.Sprintf("còn %d tiến trình của %s chạy ở ngoài lồng", len(strays), l.User),
				fmt.Sprintf("%d processes of %s are still running outside the cage", len(strays), l.User))
			kill := []string{"-KILL"}
			for i, s := range strays {
				if i == cageMaxList {
					break
				}
				kill = append(kill, s.pid) // s.pid đã qua cagePidRE
			}
			fixes = append(fixes, psCmd, line("kill", kill...))
			recheck = psCmd
		}

		// 2. Dịch vụ có đang chạy không, từ khi nào.
		showRes, showEv, err := execCmd(ctx, sys, system.Cmd{Name: "systemctl",
			Args: []string{"show", "--timestamp=unix", "-p", "MainPID", "-p", "ActiveEnterTimestamp", svc}})
		evs = append(evs, showEv)
		if err != nil {
			return b.cantRun(err, evs...)
		}
		props := cageShowProps(showRes.Stdout)
		pid, perr := strconv.Atoi(props["MainPID"])
		_, hasPID := props["MainPID"]
		pidStr := strconv.Itoa(pid)
		switch {
		case showRes.ExitCode != 0 || !hasPID || perr != nil || pid < 0:
			unclear = append(unclear, "systemctl show "+svc+" did not return MainPID")
		case pid > 0:
			cageInspectMain(ctx, sys, l, pidStr, props["ActiveEnterTimestamp"], &evs, &fails, &warns, &unclear)
		}

		// 3. Gộp: Fail thắng, rồi chưa kiểm được, rồi Warn.
		switch {
		case fails.any():
			fixes = append(fixes, cageRestartFixes(svc)...)
			return b.bad(Fail, fails.text("Lồng chạy sai hàng rào: ", "The cage does not run inside its fence: "),
				i18n.T("Hàng rào của lồng chỉ có tác dụng với tiến trình nằm trong nó. Tiến trình của site chạy ngoài lồng, hay dịch vụ đang chạy với mount, quyền hay thư mục đã lệch (ví dụ khởi động lúc MariaDB chưa có thư mục socket, hoặc bị đổi sau đó), nghĩa là mã của site đang không bị giam như tpan cam kết, hoặc không nối được DB.",
					"The cage fence only protects processes inside it. A site process running outside the cage, or a service running with mounts, privileges or directories that drifted (for example started before MariaDB had its socket directory, or changed afterwards), means the site's code is not confined as tpan promises, or cannot reach the DB."),
				append(fixes, recheck), evs...)
		case len(unclear) > 0:
			return b.unknown(i18n.T("không đọc được trạng thái tiến trình chính của lồng ("+strings.Join(unclear, "; ")+")",
				"could not read the state of the cage main process ("+strings.Join(unclear, "; ")+")"), evs...)
		case warns.any():
			return b.bad(Warn, warns.text("Lồng đang chạy bản cũ: ", "The cage is running an old version: "),
				i18n.T("Dịch vụ của lồng vẫn chạy với bản PHP đã bị thay (gói PHP vừa cập nhật mà dịch vụ chưa khởi động lại) hoặc với hàng rào cũ (file hàng rào mới hơn lúc dịch vụ khởi động). Site vẫn chạy, nhưng chưa dùng cấu hình mới nhất.",
					"The cage service still runs a replaced PHP binary (the PHP package was just updated and the service was not restarted) or an old fence (the fence file is newer than the service start). The site still works, but it does not use the latest configuration yet."),
				append(cageRestartFixes(svc), statusCmd), evs...)
		case pid0(props):
			return b.pass(i18n.T("Lồng đang ngủ (dịch vụ chưa chạy, request đầu tiên sẽ đánh thức nó); không có tiến trình lạ của "+l.User+" ngoài lồng",
				"The cage is asleep (the service is not running, the first request will wake it); no stray process of "+l.User+" outside the cage"), evs...)
		}
		return b.pass(i18n.T("Lồng chạy đúng hàng rào: không có tiến trình lạ ngoài lồng, đủ quyền đã bị cắt, đủ thư mục riêng, bản PHP và hàng rào còn mới",
			"The cage runs inside its fence: no stray process outside the cage, privileges dropped, private directories in place, PHP binary and fence up to date"), evs...)
	})
}

// pid0 cho biết MainPID đọc được và bằng 0 (dịch vụ không chạy).
func pid0(props map[string]string) bool {
	n, err := strconv.Atoi(props["MainPID"])
	return err == nil && n == 0
}

// cageRestartFixes là các lệnh gợi ý để khởi động lại lồng: tạo thư mục socket
// của MariaDB (đúng lệnh unit socket chạy), rồi khởi động lại dịch vụ.
func cageRestartFixes(svc string) []string {
	return []string{line(templates.CageMySQLDirCmd[0], templates.CageMySQLDirCmd[1:]...), line("systemctl", "try-restart", svc)}
}

// cageInspectMain đọc tiến trình chính (pid) và ghi các điều sai vào fails, các
// điều chỉ đáng cảnh báo vào warns, các việc không đọc được vào unclear.
func cageInspectMain(ctx context.Context, sys system.System, l site.Layout, pid, activeEnter string,
	evs *[]Evidence, fails, warns *cageProblems, unclear *[]string) {
	slice, fence := l.CageSlice(), l.CageFence()
	proc := "/proc/" + pid + "/"
	read := func(name string) (string, bool) {
		b, err := sys.ReadFile(proc + name)
		if err != nil {
			why := "could not read " + proc + name + ": " + err.Error()
			if cageGone(err) {
				why = "the main process " + pid + " exited while it was read (the service may be restarting)"
			}
			*unclear = append(*unclear, why)
			*evs = append(*evs, Evidence{Command: "cat " + proc + name + " (tpan)", ExitCode: -1, Output: why})
			return "", false
		}
		return string(b), true
	}

	if status, ok := read("status"); ok {
		nnp, capEff := procField(status, "NoNewPrivs"), procField(status, "CapEff")
		*evs = append(*evs, Evidence{Command: "cat " + proc + "status (tpan)", Output: "NoNewPrivs: " + nnp + "\nCapEff: " + capEff})
		if nnp != "1" {
			fails.add("tiến trình chính "+pid+" không có NoNewPrivs=1 (đang là \""+nnp+"\")", "main process "+pid+" does not have NoNewPrivs=1 (it is \""+nnp+"\")")
		}
		if v, err := strconv.ParseUint(capEff, 16, 64); err != nil || v != 0 {
			fails.add("tiến trình chính "+pid+" còn quyền (CapEff \""+capEff+"\"), phải bằng 0", "main process "+pid+" still has capabilities (CapEff \""+capEff+"\"), it must be 0")
		}
	}
	if cg, ok := read("cgroup"); ok {
		first, _, _ := strings.Cut(strings.TrimSpace(cg), "\n")
		*evs = append(*evs, Evidence{Command: "cat " + proc + "cgroup (tpan)", Output: first})
		if !cageInSlice(cg, slice) {
			fails.add("tiến trình chính "+pid+" không nằm dưới "+slice, "main process "+pid+" is not under "+slice)
		}
	}

	// mariadb có chạy không: chỉ lúc đó lồng mới đòi thấy /run/mysqld.
	dbState, dbEv, err := cageUnitState(ctx, sys, "mariadb")
	*evs = append(*evs, dbEv)
	if err != nil {
		*unclear = append(*unclear, "could not run systemctl is-active mariadb: "+err.Error())
	}
	if mi, ok := read("mountinfo"); ok {
		ms := cageParseMounts(mi)
		var shown []string
		for _, p := range []string{"/var", "/run", "/etc/tpan", "/run/mysqld", "/run/systemd/resolve", "/etc/passwd", "/etc/group"} {
			for _, m := range cageMountsAt(ms, p) {
				shown = append(shown, m.point+" "+m.fstype+" root="+m.root)
			}
		}
		*evs = append(*evs, Evidence{Command: "cat " + proc + "mountinfo (tpan)", Output: strings.Join(shown, "\n")})
		for _, p := range []string{"/var", "/run", "/etc/tpan"} {
			isTmpfs := false
			for _, m := range cageMountsAt(ms, p) {
				isTmpfs = isTmpfs || m.fstype == "tmpfs"
			}
			if !isTmpfs {
				fails.add("không thấy tmpfs ở "+p+" trong lồng", "no tmpfs at "+p+" inside the cage")
			}
		}
		my := cageMountsAt(ms, "/run/mysqld")
		switch {
		case cageDeleted(my):
			fails.add("dòng gắn /run/mysqld trỏ vào thư mục đã bị xoá (//deleted): lồng không thấy MariaDB",
				"the /run/mysqld mount points at a deleted directory (//deleted): the cage cannot see MariaDB")
		case len(my) == 0 && dbState == "active":
			fails.add("MariaDB đang chạy nhưng lồng không có dòng gắn /run/mysqld: site sẽ không nối được DB",
				"MariaDB is running but the cage has no /run/mysqld mount: the site cannot reach the DB")
		}
		if cageDeleted(cageMountsAt(ms, "/run/systemd/resolve")) {
			fails.add("dòng gắn /run/systemd/resolve trỏ vào thư mục đã bị xoá (//deleted): site không phân giải được tên miền",
				"the /run/systemd/resolve mount points at a deleted directory (//deleted): the site cannot resolve names")
		}
		// passwd, group riêng của lồng: useradd, userdel, usermod -aG trên máy
		// thay /etc/passwd, /etc/group bằng rename, và kernel gỡ mọi lần gắn
		// file lên đúng chỗ đó trong mount namespace khác. Dịch vụ đang chạy
		// từ trước lúc đó thấy lại passwd của cả máy (tên user tp_* của mọi
		// site), trong khi lệnh mới trong lồng (cage.fence) vẫn thấy bản riêng.
		for _, f := range []struct{ point, src string }{{"/etc/passwd", l.CagePasswd()}, {"/etc/group", l.CageGroup()}} {
			ok := false
			for _, m := range cageMountsAt(ms, f.point) {
				ok = ok || m.root == f.src
			}
			if !ok {
				fails.add("tiến trình chính không còn dòng gắn "+f.src+" lên "+f.point+" (user hay nhóm trên máy vừa đổi sau khi lồng khởi động): mã của site đọc được "+f.point+" của cả máy",
					"the main process no longer has "+f.src+" mounted on "+f.point+" (a user or group changed on the machine after the cage started): the site's code can read the machine's "+f.point)
			}
		}
	}

	// Cảnh báo: bản PHP bị thay (exe mang " (deleted)").
	exe, err := sys.Readlink(proc + "exe")
	exeEv := Evidence{Command: "readlink " + proc + "exe (tpan)"}
	if err != nil {
		exeEv.ExitCode, exeEv.Output = -1, err.Error()
	} else {
		exeEv.Output = exe
		if strings.HasSuffix(exe, " (deleted)") {
			warns.add("chương trình PHP của lồng đã bị thay (exe mang \"(deleted)\"), dịch vụ vẫn chạy bản cũ", "the cage's PHP binary was replaced (exe ends with \"(deleted)\"), the service still runs the old one")
		}
	}
	*evs = append(*evs, exeEv)

	// Cảnh báo: hàng rào mới hơn lúc dịch vụ khởi động.
	started, haveStart := cageUnixTime(activeEnter)
	stRes, stEv, err := execCmd(ctx, sys, system.Cmd{Name: "stat", Args: []string{"-c", "%Y", fence}})
	*evs = append(*evs, stEv)
	if err == nil && stRes.ExitCode == 0 && haveStart {
		if mt, perr := strconv.ParseInt(strings.TrimSpace(stRes.Stdout), 10, 64); perr == nil && mt > started {
			warns.add("file hàng rào mới hơn lúc dịch vụ khởi động ("+time.Unix(mt, 0).UTC().Format(time.RFC3339)+" so với "+time.Unix(started, 0).UTC().Format(time.RFC3339)+")",
				"the fence file is newer than the service start ("+time.Unix(mt, 0).UTC().Format(time.RFC3339)+" vs "+time.Unix(started, 0).UTC().Format(time.RFC3339)+")")
		}
	}
}

// ---- cage.fence ----

// cageFenceProbe là một trong bảy lệnh của phép kiểm hàng rào. verdict trả
// 0 (đạt), 1 (sai, hàng rào hở) hay 2 (chưa rõ: lệnh chạy nhưng không phải câu
// trả lời sạch).
type cageFenceProbe struct {
	cmd     system.Cmd
	wantVi  string
	wantEn  string
	verdict func(res system.Result, out string) int
}

// cageLines tách stdout thành các dòng không rỗng.
func cageLines(s string) []string {
	var out []string
	for _, ln := range strings.Split(s, "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			out = append(out, ln)
		}
	}
	return out
}

// cageSameSet: hai danh sách có cùng các phần tử (không kể thứ tự), không trùng.
func cageSameSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	g, w := append([]string(nil), got...), append([]string(nil), want...)
	sort.Strings(g)
	sort.Strings(w)
	for i := range g {
		if g[i] != w[i] {
			return false
		}
	}
	return true
}

// CageFence: "cage.fence:<domain>", chặng PHP. Bảy lệnh argv chạy với
// AsUser = l.User (đi qua lớp bọc của lồng nên chạy trong hàng rào), mỗi lệnh
// một dòng bằng chứng (mục 2.3): `id -un` ra l.User; `ls -A /var/www` ra đúng
// l.Domain; `ls -A /tmp` rỗng; `ls -A /run` ra "mysqld" và "systemd";
// `find /proc -maxdepth 1 -type d -regex /proc/[0-9]+ ! -user <l.User>` rỗng;
// `getent passwd` đúng 4 dòng (root, www-data, l.User, nobody); `test -w /etc`
// mã 1. Một lệnh sai: Fail. Người gọi bọc Heavy (chạy mỗi giờ).
//
// Khác hợp đồng một điều: trước khi chạy, Stat file hàng rào (l.CageFence()).
// Lớp bọc lệnh chỉ đưa lệnh vào lồng khi file đó có; thiếu file thì bảy lệnh
// sẽ chạy dưới uid của site Ở NGOÀI hàng rào, đúng cái cửa mà thiết kế (mục
// 3.4, điểm 4) đã đóng. Nên thiếu file là Fail và không chạy lệnh nào.
func CageFence(l site.Layout) Check {
	b := siteBase("cage.fence", StagePHP, l,
		i18n.T("Hàng rào của lồng giữ: lệnh chạy dưới user của site chỉ thấy đồ của mình",
			"The cage fence holds: a command run as the site user sees only its own things"))
	if g := cageGuard(b, l, true); g != nil {
		return g
	}
	dom, user, fence := l.Domain.String(), l.User, l.CageFence()
	probes := []cageFenceProbe{
		{system.Cmd{Name: "id", Args: []string{"-un"}, AsUser: user},
			"in ra " + user, "print " + user,
			func(r system.Result, _ string) int {
				return cageVerdict(r.ExitCode == 0 && strings.TrimSpace(r.Stdout) == user)
			}},
		{system.Cmd{Name: "ls", Args: []string{"-A", "/var/www"}, AsUser: user},
			"chỉ thấy " + dom, "show only " + dom,
			func(r system.Result, _ string) int {
				return cageVerdict(r.ExitCode == 0 && cageSameSet(cageLines(r.Stdout), []string{dom}))
			}},
		{system.Cmd{Name: "ls", Args: []string{"-A", "/tmp"}, AsUser: user},
			"/tmp rỗng", "an empty /tmp",
			func(r system.Result, _ string) int {
				return cageVerdict(r.ExitCode == 0 && len(cageLines(r.Stdout)) == 0)
			}},
		{system.Cmd{Name: "ls", Args: []string{"-A", "/run"}, AsUser: user},
			"/run chỉ có mysqld và systemd", "/run holds only mysqld and systemd",
			func(r system.Result, _ string) int {
				// mysqld chỉ có khi MariaDB đang chạy lúc lệnh khởi động (lệnh gắn
				// có dấu "-", mục 2.8): thiếu nó không phải hàng rào hở.
				got := cageLines(r.Stdout)
				return cageVerdict(r.ExitCode == 0 && (cageSameSet(got, []string{"mysqld", "systemd"}) || cageSameSet(got, []string{"systemd"})))
			}},
		{system.Cmd{Name: "find", Args: []string{"/proc", "-maxdepth", "1", "-type", "d", "-regex", "/proc/[0-9]+", "!", "-user", user}, AsUser: user},
			"không thấy tiến trình của user khác", "no process of another user is visible",
			func(r system.Result, _ string) int {
				switch {
				case len(cageLines(r.Stdout)) > 0:
					return 1
				case r.ExitCode != 0:
					return 2 // find lỗi (tiến trình biến mất giữa chừng...): chưa chứng minh được gì
				}
				return 0
			}},
		{system.Cmd{Name: "getent", Args: []string{"passwd"}, AsUser: user},
			"danh sách user chỉ có root, www-data, " + user + ", nobody", "the user list holds only root, www-data, " + user + ", nobody",
			func(r system.Result, _ string) int {
				var names []string
				for _, ln := range cageLines(r.Stdout) {
					n, _, _ := strings.Cut(ln, ":")
					names = append(names, n)
				}
				return cageVerdict(r.ExitCode == 0 && cageSameSet(names, []string{"root", "www-data", user, "nobody"}))
			}},
		{system.Cmd{Name: "test", Args: []string{"-w", "/etc"}, AsUser: user},
			"không ghi được vào /etc", "cannot write to /etc",
			func(r system.Result, out string) int {
				switch {
				case r.ExitCode == 0:
					return 1
				case r.ExitCode == 1 && strings.TrimSpace(out) == "":
					return 0
				}
				return 2 // mã 1 kèm lỗi, hay mã lạ: không phải câu trả lời "bị từ chối" sạch
			}},
	}

	return checkFunc(func(ctx context.Context, sys system.System) Result {
		_, fenceEv, missing, err := siteStat(sys, fence)
		if err != nil {
			return siteStatUnknown(b, fence, err, fenceEv)
		}
		if missing {
			return b.bad(Fail,
				i18n.T("Không có file hàng rào "+fence+": bảy lệnh thử không chạy (chúng sẽ chạy ngoài lồng)", "The fence file "+fence+" is missing: the seven probe commands were not run (they would run outside the cage)"),
				i18n.T("Site được đánh dấu là ở trong lồng nhưng file hàng rào không còn. Không có nó thì lệnh chạy dưới user của site không vào lồng, tức không có hàng rào nào cả.",
					"The site is marked as caged but the fence file is gone. Without it commands run as the site user do not enter the cage, so there is no fence at all."),
				[]string{line("ls", "-l", fence), line("tpan", "site", "add", dom), line("ls", "-l", fence)}, fenceEv)
		}

		evs := []Evidence{}
		var bad, unclear []cageFenceProbe
		for _, p := range probes {
			res, ev, err := execCmd(ctx, sys, p.cmd)
			evs = append(evs, ev)
			if err != nil {
				// Gồm lệnh trong lồng bị dừng (cage.KilledError): chưa kiểm được, không phải hàng rào hở.
				return b.cantRun(err, evs...)
			}
			switch p.verdict(res, ev.Output) {
			case 1:
				bad = append(bad, p)
			case 2:
				unclear = append(unclear, p)
			}
		}
		if len(bad) > 0 {
			var vi, en []string
			fix := []string{line("ls", "-l", fence), line("systemctl", "cat", l.CageServiceUnit(), "--no-pager"), line("tpan", "site", "add", dom)}
			for _, p := range bad {
				vi, en = append(vi, system.Cmd{Name: p.cmd.Name, Args: p.cmd.Args}.String()+" phải "+p.wantVi), append(en, system.Cmd{Name: p.cmd.Name, Args: p.cmd.Args}.String()+" must give "+p.wantEn)
			}
			// Kiểm lại bằng đúng lệnh đã sai, in như sẽ chạy thật (systemd-run với site trong lồng).
			for _, p := range bad {
				fix = append(fix, system.Describe(sys, p.cmd))
			}
			return b.bad(Fail,
				i18n.T("Hàng rào của lồng hở: "+strings.Join(vi, "; "), "The cage fence leaks: "+strings.Join(en, "; ")),
				i18n.T("Lệnh chạy dưới user của site thấy nhiều hơn phần của site (file của site khác, /tmp chung, tiến trình khác, hay ghi được vào /etc). Hàng rào (file fence.conf) bị sửa, bị xoá, hoặc systemd chưa nạp lại. Một site bị chiếm khi đó có thể nhìn sang site khác.",
					"A command run as the site user sees more than the site's own part (other sites' files, a shared /tmp, other processes, or can write to /etc). The fence file (fence.conf) was edited, deleted, or systemd has not reloaded it. A compromised site could then look into other sites."),
				fix, evs...)
		}
		if len(unclear) > 0 {
			var names []string
			for _, p := range unclear {
				names = append(names, system.Cmd{Name: p.cmd.Name, Args: p.cmd.Args}.String())
			}
			return b.unknown(i18n.T("lệnh không cho câu trả lời sạch: "+strings.Join(names, "; "), "the command did not give a clean answer: "+strings.Join(names, "; ")), evs...)
		}
		return b.pass(i18n.T("Hàng rào giữ: cả bảy lệnh thử dưới "+user+" chỉ thấy đồ của "+dom, "The fence holds: all seven probe commands as "+user+" see only the things of "+dom), evs...)
	})
}

func cageVerdict(ok bool) int {
	if ok {
		return 0
	}
	return 1
}

// ---- cage.limits ----

const (
	cageMiB = int64(1) << 20 // MemoryMax: systemd đọc "M" là 2^20
	cageMB  = int64(1000000) // IOReadBandwidthMax: systemd đọc "M" là 10^6
)

// cageAmount viết một con số cgroup cho người đọc; n <= 0 là không giới hạn.
func cageAmount(n int64, unit string) (vi, en string) {
	if n <= 0 {
		return "không giới hạn", "unlimited"
	}
	s := strconv.FormatInt(n, 10) + " " + unit
	return s, s
}

// CageLimits: "cage.limits:<domain>", chặng PHP. Đọc cgroup của slice (cùng
// cách đọc với cage.ReadUsage) và so memory.max, memory.swap.max, cpu.max,
// pids.max, io.max với want. Thư mục cgroup chưa có: Pass kèm câu "lồng chưa
// chạy từ khi máy khởi động; trần sẽ áp khi lồng chạy". Lệch: Fail, nêu từng
// trường (mong đợi, đang áp).
//
// Quy đổi (mục 3.7): file không có hay rỗng là không giới hạn, "max" cũng vậy;
// cpu.max "q p" là q*100/p %; io.max chỉ so rbps và wbps (bỏ qua số thiết bị);
// MB là 10^6 byte, MiB là 2^20. Gói có trần đĩa mà io.max rỗng sau khi áp: Warn
// (không có sửa tự động), trừ khi đã có trường khác lệch (Fail, câu về đĩa vẫn
// nằm trong Summary).
func CageLimits(l site.Layout, want account.Limits) Check {
	b := siteBase("cage.limits", StagePHP, l,
		i18n.T("Trần tài nguyên của lồng (RAM, swap, CPU, tiến trình, đĩa) đúng như gói", "The cage resource limits (RAM, swap, CPU, tasks, disk) match the package"))
	if g := cageGuard(b, l, false); g != nil {
		return g
	}
	dir := l.CageCgroup()
	recheck := line("systemctl", "show", l.CageSlice(), "-p", "MemoryMax", "-p", "MemorySwapMax", "-p", "CPUQuotaPerSecUSec", "-p", "TasksMax", "-p", "IOReadBandwidthMax", "-p", "IOWriteBandwidthMax")
	cgFiles := line("grep", "-H", ".", dir+"/memory.max", dir+"/memory.swap.max", dir+"/cpu.max", dir+"/pids.max", dir+"/io.max")

	return checkFunc(func(ctx context.Context, sys system.System) Result {
		u, err := cage.ReadUsage(sys, l, time.Now())
		if err != nil {
			return b.unknown(i18n.T("không đọc được cgroup của lồng ("+err.Error()+")", "could not read the cage cgroup ("+err.Error()+")"),
				Evidence{Command: "read " + dir + " (tpan)", ExitCode: -1, Output: err.Error()})
		}
		if !u.Running {
			return b.pass(i18n.T("Lồng chưa chạy từ khi máy khởi động; trần sẽ áp khi lồng chạy", "The cage has not run since the machine started; the limits will apply when it runs"),
				Evidence{Command: "stat " + dir + " (tpan)", ExitCode: 1, Output: "No such file or directory"})
		}
		// ReadUsage trả 0 cho cả "file không có" lẫn "0" ở memory.swap.max. Hai
		// nghĩa khác nhau (không giới hạn / cấm swap), nên đọc lại file khi gặp 0.
		swap := u.SwapMax
		if swap == 0 {
			raw, err := sys.ReadFile(dir + "/memory.swap.max")
			if err != nil || strings.TrimSpace(string(raw)) == "" {
				swap = -1
			}
		}

		var p cageProblems
		var out []string
		// field so một trần: want và got là số đã đổi về cùng đơn vị; 0 là không giới hạn.
		field := func(name string, want, got int64, unit string) {
			wv, we := cageAmount(want, unit)
			gv, ge := cageAmount(got, unit)
			out = append(out, fmt.Sprintf("%s: want %s, applied %s", name, we, ge))
			if want != got {
				p.add(name+": mong đợi "+wv+", đang áp "+gv, name+": expected "+we+", applied "+ge)
			}
		}
		field("memory.max", int64(want.RAMMB)*cageMiB, u.MemMax, "byte")
		// Có trần RAM thì cấm swap (0), không thì không giới hạn (-1).
		wantSwap := int64(-1)
		if want.RAMMB > 0 {
			wantSwap = 0
		}
		out = append(out, fmt.Sprintf("memory.swap.max: want %s, applied %s", cageSwapText(wantSwap), cageSwapText(swap)))
		if wantSwap != swap {
			p.add("memory.swap.max: mong đợi "+cageSwapVi(wantSwap)+", đang áp "+cageSwapVi(swap), "memory.swap.max: expected "+cageSwapText(wantSwap)+", applied "+cageSwapText(swap))
		}
		var gotPct int64
		if u.CPUQuotaUs > 0 && u.CPUPeriod > 0 {
			gotPct = u.CPUQuotaUs * 100 / u.CPUPeriod
		}
		field("cpu.max", int64(want.CPUPercent), gotPct, "%")
		field("pids.max", int64(want.Tasks), u.TasksMax, "task")

		// io.max chỉ so rbps và wbps. Gói có trần đĩa mà không có dòng nào: loại đĩa
		// này không nhận trần (LVM, btrfs, ZFS chưa thử): cảnh báo, không sửa được.
		diskWarn := (want.IOReadMB > 0 || want.IOWriteMB > 0) && u.IORBps == 0 && u.IOWBps == 0
		if diskWarn {
			out = append(out, "io.max: empty although the package sets a disk limit")
		} else {
			field("io.max rbps", int64(want.IOReadMB)*cageMB, u.IORBps, "byte/s")
			field("io.max wbps", int64(want.IOWriteMB)*cageMB, u.IOWBps, "byte/s")
		}
		ev := Evidence{Command: "read " + dir + " (tpan)", Output: strings.Join(out, "\n")}

		const diskVi = "trần đĩa không áp được trên loại đĩa này (io.max rỗng sau khi áp; LVM, btrfs, ZFS chưa thử)"
		const diskEn = "the disk limit cannot be applied on this kind of disk (io.max is empty after applying; LVM, btrfs, ZFS not tried)"
		switch {
		case p.any():
			if diskWarn {
				p.add(diskVi, diskEn)
			}
			return b.bad(Fail, p.text("Trần của lồng lệch gói: ", "The cage limits differ from the package: "),
				i18n.T("Trần trong cgroup (thứ kernel thật sự áp) khác trần của gói: file slice bị sửa tay, hoặc gói vừa đổi mà chưa đồng bộ. Trần sai thì site có thể ăn quá phần của mình, hoặc bị bóp quá chặt.",
					"The limits in the cgroup (what the kernel really applies) differ from the package: the slice file was edited by hand, or the package just changed and was not synced. Wrong limits let the site use more than its share, or throttle it too hard."),
				[]string{line("systemctl", "cat", l.CageSlice(), "--no-pager"), cgFiles, line("tpan", "cage", "status", l.Domain.String()), recheck}, ev)
		case diskWarn:
			return b.bad(Warn, i18n.T("Trần RAM, CPU và tiến trình đúng gói, nhưng "+diskVi, "The RAM, CPU and task limits match the package, but "+diskEn),
				i18n.T("Trần đĩa được đặt qua io.max của cgroup, và kernel chỉ nhận nó trên một số loại đĩa. Ở đây io.max rỗng: site không bị giới hạn tốc độ đọc ghi dù gói có đặt. Chưa có cách sửa tự động.",
					"The disk limit is set through the cgroup io.max, and the kernel accepts it only on some kinds of disk. Here io.max is empty: the site's read and write speed is not limited although the package sets it. There is no automatic fix yet."),
				[]string{line("lsblk", "-o", "NAME,TYPE,MOUNTPOINT"), line("cat", dir+"/io.max"), recheck}, ev)
		}
		return b.pass(i18n.T("Trần của lồng đúng gói (RAM, swap, CPU, tiến trình, đĩa)", "The cage limits match the package (RAM, swap, CPU, tasks, disk)"), ev)
	})
}

func cageSwapText(n int64) string {
	if n < 0 {
		return "unlimited"
	}
	return strconv.FormatInt(n, 10) + " byte"
}

func cageSwapVi(n int64) string {
	if n < 0 {
		return "không giới hạn"
	}
	return strconv.FormatInt(n, 10) + " byte"
}

// ---- cage.state ----

// CageState: "cage.state:<domain>", chặng PHP. caged là sites.caged, want là
// cờ lồng của gói. (a) want khác caged: Warn. (b) caged mà thiếu file hàng rào
// (l.CageFence()) hoặc còn file pool chung (/etc/php/*/fpm/pool.d/<pool>.conf,
// đọc bằng ReadDir từng bản); hoặc không caged mà còn file hàng rào: Fail.
// Core sinh phép này cho site có caged, want, hoặc có file hàng rào.
//
// Có cả (a) lẫn (b) thì Fail (câu của (a) vẫn nằm trong Summary): (b) là máy sai
// so với bảng, nặng hơn việc chưa kịp đồng bộ với gói.
func CageState(l site.Layout, caged, want bool) Check {
	b := siteBase("cage.state", StagePHP, l,
		i18n.T("Trạng thái lồng của site khớp bảng và gói", "The site's cage state matches the database and the package"))
	fence := l.CageFence()
	if g := cageGuard(b, l, false, fence); g != nil {
		return g
	}
	if !sitePoolRE.MatchString(l.Pool) {
		return checkFunc(func(context.Context, system.System) Result {
			return b.unknown(i18n.T("tên pool không hợp lệ (\""+l.Pool+"\")", "the pool name is not valid (\""+l.Pool+"\")"))
		})
	}
	dom := l.Domain.String()
	poolGlob := "/etc/php/*/fpm/pool.d/" + l.Pool + ".conf" // dấu * để shell mở rộng: không qua line()
	recheck := line("ls", "-l", fence)

	return checkFunc(func(ctx context.Context, sys system.System) Result {
		_, fenceEv, fenceMissing, err := siteStat(sys, fence)
		if err != nil {
			return siteStatUnknown(b, fence, err, fenceEv)
		}
		evs := []Evidence{fenceEv}

		var shared []string
		if caged { // chỉ cần biết pool chung khi site đang là "caged"
			poolEv := Evidence{Command: "ls " + poolGlob + " (tpan)"}
			vers, err := sys.ReadDir("/etc/php")
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				poolEv.ExitCode, poolEv.Output = -1, err.Error()
				return b.unknown(i18n.T("không đọc được /etc/php ("+err.Error()+")", "could not read /etc/php ("+err.Error()+")"), append(evs, poolEv)...)
			}
			for _, v := range vers {
				if !phpVersionRE.MatchString(v) {
					continue
				}
				pf := path.Join("/etc/php", v, "fpm/pool.d", l.Pool+".conf")
				_, _, missing, err := siteStat(sys, pf)
				if err != nil {
					poolEv.ExitCode, poolEv.Output = -1, err.Error()
					return b.unknown(i18n.T("không đọc được "+pf+" ("+err.Error()+")", "could not read "+pf+" ("+err.Error()+")"), append(evs, poolEv)...)
				}
				if !missing {
					shared = append(shared, pf)
				}
			}
			poolEv.Output = strings.Join(shared, "\n")
			if poolEv.Output == "" {
				poolEv.Output = "no shared pool file"
			}
			evs = append(evs, poolEv)
		}

		var fail, warn cageProblems
		fixes := []string{}
		switch {
		case caged && fenceMissing:
			fail.add("site ở trong lồng (caged=1) nhưng thiếu file hàng rào "+fence, "the site is caged (caged=1) but the fence file "+fence+" is missing")
			fixes = append(fixes, line("tpan", "cage", "enable", dom))
		case !caged && !fenceMissing:
			fail.add("site không ở trong lồng (caged=0) nhưng còn file hàng rào "+fence, "the site is not caged (caged=0) but the fence file "+fence+" is still there")
			fixes = append(fixes, line("tpan", "cage", "disable", dom))
		}
		if len(shared) > 0 {
			fail.add("site ở trong lồng nhưng còn file pool chung "+strings.Join(shared, ", "), "the site is caged but a shared pool file is still there: "+strings.Join(shared, ", "))
			if len(fixes) == 0 {
				fixes = append(fixes, line("tpan", "cage", "enable", dom))
			}
		}
		switch {
		case caged && !want:
			warn.add("gói của chủ site không còn muốn lồng nhưng site vẫn ở trong lồng", "the owner's package no longer wants a cage but the site is still caged")
			if len(fixes) == 0 {
				fixes = append(fixes, line("tpan", "cage", "disable", dom))
			}
		case !caged && want:
			warn.add("gói của chủ site muốn lồng nhưng site chưa ở trong lồng", "the owner's package wants a cage but the site is not caged yet")
			if len(fixes) == 0 {
				fixes = append(fixes, line("tpan", "cage", "enable", dom))
			}
		}
		switch {
		case fail.any():
			for i := range warn.vi {
				fail.add(warn.vi[i], warn.en[i])
			}
			return b.bad(Fail, fail.text("Trạng thái lồng của site lệch giữa bảng và máy: ", "The site's cage state differs between the database and the machine: "),
				i18n.T("Bảng sites nói một đằng, file trên máy nói một nẻo (một lần bật hoặc tắt lồng bị dừng giữa chừng, hoặc file bị xoá tay). Site có thể đang chạy trong pool chung trong khi tưởng là ở trong lồng, hoặc ngược lại.",
					"The sites table says one thing and the files on the machine another (a cage enable or disable stopped halfway, or files were deleted by hand). The site may run in the shared pool while believed to be caged, or the other way round."),
				append(fixes, recheck), evs...)
		case warn.any():
			return b.bad(Warn, warn.text("Lồng chưa khớp gói: ", "The cage does not match the package: "),
				i18n.T("Chủ site đổi gói (hoặc gói đổi cờ lồng) nhưng đổi gói không tự bật hay tắt lồng: mọi thay đổi trên site đang chạy phải là một Plan có xem trước. Site vẫn chạy bình thường.",
					"The owner changed package (or the package changed its cage flag), but changing the package does not enable or disable the cage by itself: every change on a running site must be a Plan with a preview. The site keeps working."),
				append(fixes, recheck), evs...)
		}
		if caged {
			return b.pass(i18n.T("Site ở trong lồng, đủ file hàng rào, không còn pool chung, khớp gói", "The site is caged, the fence file is there, no shared pool is left, and it matches the package"), evs...)
		}
		return b.pass(i18n.T("Site ngoài lồng, không còn file hàng rào, khớp gói", "The site is not caged, no fence file is left, and it matches the package"), evs...)
	})
}

// ---- probe.leftover ----

// ProbeLeftover: "probe.leftover:tpan", chặng TPAN. left là mô tả các lượt thử
// tấn công chưa dọn (sổ probe_runs, Cleaned = false, đã bắt đầu quá 2 giờ
// hoặc không có job nào đang chạy: core quyết). Rỗng: Pass. Có: Fail.
// Không đọc máy: core đã đọc sổ. left là mô tả do tpan sinh (không phải nội
// dung của site), nhưng vẫn chỉ đưa tối đa cageMaxList mục vào câu kết quả.
func ProbeLeftover(left []string) Check {
	b := base{id: "probe.leftover:tpan", stage: StageTPAN,
		title: i18n.T("Không còn site thử và user thử của phép thử tấn công", "No test sites or users of the attack probe are left behind")}
	return checkFunc(func(context.Context, system.System) Result {
		if len(left) == 0 {
			return b.pass(i18n.T("Không còn gì sót lại của phép thử tấn công", "Nothing is left behind by the attack probe"))
		}
		shown := left
		if len(shown) > cageMaxList {
			shown = shown[:cageMaxList]
		}
		list := strings.Join(shown, "; ")
		if len(left) > len(shown) {
			list += fmt.Sprintf("; ... (%d more)", len(left)-len(shown))
		}
		return b.bad(Fail,
			i18n.T(fmt.Sprintf("Còn %d lượt thử tấn công chưa dọn: %s", len(left), list), fmt.Sprintf("%d attack probe runs are not cleaned up: %s", len(left), list)),
			i18n.T("Phép thử tấn công tạo hai site thử cùng user và DB của chúng, rồi dọn đi. Một lượt bị dừng giữa chừng (máy tắt, tpan bị giết) để lại các site thử đó: chúng chiếm tài nguyên, và tên của chúng lộ ra trong danh sách site.",
				"The attack probe creates two test sites with their users and databases, then removes them. A run that stopped halfway (machine off, tpan killed) leaves those test sites behind: they use resources and their names show up in the site list."),
			[]string{line("tpan", "isolation", "probe", "--cleanup", "--dry-run"), line("tpan", "isolation", "probe", "--cleanup", "--yes"), line("tpan", "isolation", "probe", "--cleanup", "--dry-run")})
	})
}

// ---- Gom lại ----

// SiteChecksCaged là SiteChecks của site trong lồng: như SiteChecks(web, php,
// l) nhưng thay PoolSocket bằng CageSocket(l), CageRuntime(l),
// Heavy(CageFence(l)), CageLimits(l, want). SiteChecksSSLCaged tương tự với
// SiteChecksSSL.
func SiteChecksCaged(web string, l site.Layout, want account.Limits) []Check {
	return []Check{
		VhostEnabled(web, l),
		CageSocket(l),
		CageRuntime(l),
		Heavy(CageFence(l)),
		CageLimits(l, want),
		Heavy(WPChecksums(l)),
		WPConfigPerm(l),
		DBAccess(l),
		HTTPSite(l),
	}
}

func SiteChecksSSLCaged(web string, l site.Layout, want account.Limits) []Check {
	return []Check{
		TLSCert(l),
		VhostEnabled(web, l),
		CageSocket(l),
		CageRuntime(l),
		Heavy(CageFence(l)),
		CageLimits(l, want),
		WPChecksums(l),
		WPConfigPerm(l),
		DBAccess(l),
		HTTPRedirect(l),
		HTTPSSite(l),
	}
}
