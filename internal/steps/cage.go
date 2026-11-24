package steps

// Step của lồng theo site (M17, mục 3.5 của docs/thay-doi/M17-thiet-ke.md).
//
// Bốn Step (CageFiles, CageSocket, CageReap, CageRestart) và hàm dựng vhost
// trong lồng (SiteVhostCaged). Chỉ đụng bảy file của l.CageFiles(), các thư
// mục /etc/tpan/cage, /etc/tpan/cage/<slug>, tpan-cage-<slug>-.service.d và
// thư mục socket của lồng; /etc/tpan chỉ được tạo khi chưa có.

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/cage"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
)

var (
	_ engine.Step = (*CageFiles)(nil)
	_ engine.Step = (*CageSocket)(nil)
	_ engine.Step = (*CageReap)(nil)
	_ engine.Step = (*CageRestart)(nil)
)

// Thời hạn chờ của các bước lồng. Biến (không phải hằng) để test rút ngắn.
var (
	// cageSocketWait: systemd tạo file socket ngay khi unit socket vào active;
	// 20 giây là trần của mục 3.5 cho máy chậm, không phải thời gian thường gặp.
	cageSocketWait = 20 * time.Second
	// cageReapWait: SIGKILL được giao bất đồng bộ, kill() trả về trước khi
	// tiến trình thật sự chết; chờ tới khi /proc không còn nó.
	cageReapWait = 5 * time.Second
	cageWaitTick = 100 * time.Millisecond
)

// cageJournalLines là số dòng journal kèm vào lời lỗi của CageSocket.
const cageJournalLines = 20

// tpanEtc là /etc/tpan, thuộc `tpan setup` (0700). Bước của lồng chỉ tạo nó
// khi chưa có (máy dùng CLI mà chưa chạy setup), không đổi quyền, không xoá.
var tpanEtc = path.Dir(site.CageConfRoot)

// ---- Kiểm tham số ----

// cageCheckLayout: Layout phải đúng là LayoutOf(Domain) và user phải đúng dạng
// tp_<slug> của chính slug đó. Tên unit, tên slice ghép từ slug; user đi vào
// `id -u`, `getent` và AsUser của lớp bọc (lớp bọc chỉ nhận dạng này).
func cageCheckLayout(l site.Layout) error {
	if err := webCheckLayout(l); err != nil {
		return err
	}
	if slug, ok := site.CageSlugOfUser(l.User); !ok || slug != l.Slug.String() {
		return fmt.Errorf("user %q của %s không đúng dạng tp_<slug>", l.User, l.Domain)
	}
	return nil
}

func cageCheck(l site.Layout, php string) error {
	if err := cageCheckLayout(l); err != nil {
		return err
	}
	if !phpVersion.MatchString(php) {
		return fmt.Errorf("bản PHP %q không hợp lệ (dạng 8.3)", php)
	}
	return nil
}

// ---- Lệnh dùng chung ----

// cageFPMTest kiểm cú pháp cấu hình FPM của lồng bằng root ở máy thật, với -R
// (cho FPM chạy bằng root: pool của lồng không có dòng `user`, thiếu -R thì -t
// báo "user has not been defined", mục 2.1, t4). Không chạy dưới uid của site:
// mã của site cùng uid giết được tiến trình -t (kill -9 -1, pkill) hay lấp số
// tiến trình của slice, và đây là bước của Plan chính sách (cage.enable,
// cage.sync): site không được làm hỏng nó (soát cuối M17, N1; thử trên máy ảo:
// -t -R đạt với cấu hình tốt, thoát 78 với cấu hình hỏng). Cấu hình do tpan
// sinh, -t không nạp mã nào của site.
func cageFPMTest(l site.Layout, php string) system.Cmd {
	return system.Cmd{Name: "php-fpm" + php, Args: []string{"-t", "-R", "--fpm-config", l.CageFPMConf()}}
}

// cageFenceTest chạy `php-fpm -t` dưới user của site qua lớp bọc, tức trong
// hàng rào và slice của site: chứng minh systemd dựng được không gian riêng
// của lồng (lỗi gắn, 226/NAMESPACE) và PHP chạy được trong đó, trước khi bật
// socket. Chỉ CageSocket (bật lồng) dùng, qua runFenceTest.
func cageFenceTest(l site.Layout, php string) system.Cmd {
	return system.Cmd{Name: "php-fpm" + php, Args: []string{"-t", "--fpm-config", l.CageFPMConf()}, AsUser: l.User}
}

// runFenceTest: lệnh trong lồng bị giết hay không có tài nguyên để chạy
// (cage.ErrKilled: tín hiệu, oom-kill, Result=resources khi mã của site lấp
// pids.max của slice) là việc mã của site làm được, nên không làm bước hỏng;
// lệnh vẫn vào log của job. Mọi lỗi khác (systemd không dựng được unit, -t
// thoát khác 0 trong lồng) là hàng rào hay cấu hình hỏng: bước hỏng.
func runFenceTest(ctx context.Context, sys system.System, l site.Layout, php string) error {
	test := cageFenceTest(l, php)
	err := runTest(ctx, sys, &test, "dựng hàng rào của lồng")
	if errors.Is(err, cage.ErrKilled) {
		return nil
	}
	return err
}

var cageDaemonReload = system.Cmd{Name: "systemctl", Args: []string{"daemon-reload"}}

func cageResetFailed(l site.Layout) system.Cmd {
	return system.Cmd{Name: "systemctl", Args: []string{"reset-failed", l.CageServiceUnit(), l.CageSocketUnit()}}
}

func systemctlNow(verb, unit string) system.Cmd {
	return system.Cmd{Name: "systemctl", Args: []string{verb, "--now", unit}}
}

func isSocket(sys system.System, p string) bool {
	fi, err := sys.Stat(p)
	return err == nil && fi.Mode&fs.ModeSocket != 0
}

// waitCageSocket chờ tới khi p là socket. Khác waitForPath: hết giờ là lỗi,
// vì socket không có thì site ra 502 và bước không được báo là xong.
func waitCageSocket(ctx context.Context, sys system.System, p string) error {
	deadline := time.Now().Add(cageSocketWait)
	for {
		if isSocket(sys, p) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("sau %v vẫn chưa có socket %s", cageSocketWait, p)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("chờ socket %s: %w", p, ctx.Err())
		case <-time.After(cageWaitTick):
		}
	}
}

// removeEmptyDir gỡ thư mục p khi nó rỗng; không có hay còn đồ thì để nguyên.
func removeEmptyDir(sys system.System, p string) error {
	names, err := sys.ReadDir(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("đọc thư mục %s: %w", p, err)
	}
	if len(names) > 0 {
		return nil
	}
	if err := sys.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("gỡ thư mục rỗng %s: %w", p, err)
	}
	return nil
}

// cageCleanLine bỏ ký tự điều khiển (kể cả ESC của mã màu terminal, CR) và ký
// tự định dạng vô hình (đổi chiều chữ, zero-width), rồi cắt dòng quá dài. Dòng
// journal do PHP của site ghi: site điều khiển được từng byte của nó.
func cageCleanLine(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == unicode.ReplacementChar {
			return -1
		}
		return r
	}, s)
	if r := []rune(s); len(r) > 300 {
		s = string(r[:300]) + "…"
	}
	return s
}

// withCageJournal thêm 20 dòng cuối journal của dịch vụ và socket của lồng vào
// lỗi, đã làm sạch và đánh dấu từng dòng là output của site: lời lỗi đi tới log
// job, giao diện và bộ não AI, nên không được để dòng do site viết trông như
// lời của tpan. Lỗi của chính journalctl bỏ qua: không che lỗi chính.
func withCageJournal(ctx context.Context, sys system.System, l site.Layout, err error) error {
	res, _ := sys.Run(ctx, system.Cmd{Name: "journalctl", Args: []string{
		"-u", l.CageServiceUnit(), "-u", l.CageSocketUnit(), "--no-pager", "-o", "cat", "-n", strconv.Itoa(cageJournalLines)}})
	lines := strings.Split(strings.TrimRight(res.Stdout, "\n"), "\n")
	if len(lines) > cageJournalLines {
		lines = lines[len(lines)-cageJournalLines:]
	}
	var b strings.Builder
	for _, ln := range lines {
		if ln = cageCleanLine(ln); strings.TrimSpace(ln) != "" {
			b.WriteString("\n[output của site] " + ln)
		}
	}
	if b.Len() == 0 {
		return err
	}
	return fmt.Errorf("%w\nCác dòng cuối journal của %s và %s. Đây là output của site (log của PHP-FPM có dòng do PHP của site ghi), không phải lời của tpan:%s",
		err, l.CageServiceUnit(), l.CageSocketUnit(), b.String())
}

// ---- Thư mục của lồng ----

// cageDir là một thư mục của lồng: quyền mong đợi và trạng thái trước bước.
type cageDir struct {
	path    string
	want    fs.FileMode
	existed bool
	perm    fs.FileMode
	owner   system.Owner
}

func captureDir(sys system.System, p string, want fs.FileMode) (cageDir, error) {
	d := cageDir{path: p, want: want}
	fi, err := sys.Stat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return d, nil
	}
	if err != nil {
		return d, fmt.Errorf("đọc trạng thái %s: %w", p, err)
	}
	if !fi.Mode.IsDir() {
		return d, fmt.Errorf("%s đã có nhưng không phải thư mục", p)
	}
	d.existed, d.perm, d.owner = true, fi.Mode.Perm(), normOwner(fi.Owner)
	return d, nil
}

func (d cageDir) ok() bool { return d.existed && d.perm == d.want && d.owner == system.Root }

// ensure đưa thư mục về quyền mong đợi. Thư mục cha đã có (gọi theo thứ tự
// cha trước con), nên MkdirAll chỉ tạo đúng một cấp.
func (d cageDir) ensure(sys system.System) error {
	if !d.existed {
		if err := sys.MkdirAll(d.path, d.want, system.Root); err != nil {
			return fmt.Errorf("tạo thư mục %s: %w", d.path, err)
		}
		return nil
	}
	if d.owner != system.Root {
		if err := sys.Chown(d.path, system.Root); err != nil {
			return fmt.Errorf("đổi chủ %s: %w", d.path, err)
		}
	}
	if d.perm != d.want {
		if err := sys.Chmod(d.path, d.want); err != nil {
			return fmt.Errorf("đổi quyền %s: %w", d.path, err)
		}
	}
	return nil
}

// restoreBefore (đường trả lại, trước khi trả file): thư mục vốn có thì dựng
// lại nếu đã bị gỡ và trả quyền, chủ cũ.
func (d cageDir) restoreBefore(sys system.System) error {
	if !d.existed {
		return nil
	}
	fi, err := sys.Stat(d.path)
	if errors.Is(err, fs.ErrNotExist) {
		if err := sys.MkdirAll(d.path, d.perm, d.owner); err != nil {
			return fmt.Errorf("dựng lại thư mục %s: %w", d.path, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("đọc trạng thái %s: %w", d.path, err)
	}
	if normOwner(fi.Owner) != d.owner {
		if err := sys.Chown(d.path, d.owner); err != nil {
			return fmt.Errorf("trả chủ cũ của %s: %w", d.path, err)
		}
	}
	if fi.Mode.Perm() != d.perm {
		if err := sys.Chmod(d.path, d.perm); err != nil {
			return fmt.Errorf("trả quyền cũ của %s: %w", d.path, err)
		}
	}
	return nil
}

// restoreAfter (đường trả lại, sau khi trả file): thư mục do bước tạo thì gỡ.
func (d cageDir) restoreAfter(sys system.System) error {
	if d.existed {
		return nil
	}
	if err := sys.Remove(d.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("gỡ thư mục %s đã tạo: %w", d.path, err)
	}
	return nil
}

// ================================================================ CageFiles

// CageFiles ghi (hoặc gỡ, khi Remove) bảy file của lồng của Layout:
// l.CageFiles() (slice, socket, dịch vụ, hàng rào, php-fpm.conf, passwd, group),
// nội dung từ templates.CageSlice, CageSocketUnit, CageServiceUnit, CageFence,
// CageFPMConf(l, Limits.Workers()); passwd và group mỗi file 4 dòng từ
// `getent passwd root www-data <user> nobody` và `getent group root www-data
// <user> nogroup` (kiểm từng dòng đúng dạng và đúng 4 tên trước khi ghi).
// Quyền: file 0644 root:root; /etc/tpan/cage/<slug> 0755; /etc/tpan/cage 0700;
// /etc/tpan chỉ tạo khi chưa có (0700), không bao giờ đổi quyền hay xoá.
//
// Check: cả bảy file đúng nội dung, quyền, chủ; Remove: không còn file hay thư
// mục nào của lồng.
//
// Apply (ghi): giữ bản cũ của mọi file và trạng thái active của socket và dịch
// vụ; chỉ ghi file khác nội dung, quyền hay chủ, theo thứ tự slice, socket,
// dịch vụ, php-fpm.conf, passwd, group, HÀNG RÀO SAU CÙNG (lớp bọc lệnh coi
// "có hàng rào" là "có lồng"); `systemctl daemon-reload`. Nếu file dịch vụ hay
// php-fpm.conf vừa được ghi: `php-fpm<PHP> -t --fpm-config <l.CageFPMConf()>`
// với AsUser = user của site (qua lớp bọc nên chạy trong lồng); hỏng thì đi
// đường trả lại và báo lỗi, chưa đụng tới dịch vụ. Rồi nếu dịch vụ đang chạy
// và một trong dịch vụ, hàng rào, php-fpm.conf, passwd, group vừa được ghi:
// `systemctl try-restart <dịch vụ>`; lệnh này hỏng thì ghi vào log (OnOutput
// không có: trả về trong lời nhắn của bước qua một dòng output) nhưng KHÔNG
// làm bước lỗi (mục 3.5: dịch vụ sống hay chết sau đó do phép kiểm và Plan gọi
// bước lo).
// Apply (gỡ): gỡ hàng rào TRƯỚC TIÊN, rồi các file còn lại, thư mục
// /etc/tpan/cage/<slug> và .service.d; /etc/tpan/cage nếu rỗng; daemon-reload;
// `systemctl stop 'tpan-cage-<slug>-run-*'`, `systemctl reset-failed
// 'tpan-cage-<slug>-*'` (bỏ qua lỗi).
// Đường trả lại (lỗi giữa Apply, và Undo): trả mọi file về bản cũ (file mới thì
// xoá, thư mục mới thì gỡ), daemon-reload, `systemctl reset-failed <dịch vụ>
// <socket>`, socket từng active thì `systemctl start <socket>` và chờ file
// socket (tối đa 20 giây), dịch vụ từng active thì `systemctl start <dịch vụ>`.
// Không đưa về được thì Undo trả lỗi.
// Info().Files: đúng năm file của l.CageManagedFiles() (Delete khi Remove);
// passwd và group không bao giờ khai.
type CageFiles struct {
	Layout site.Layout
	PHP    string // bản PHP của site (dòng ExecStart)
	Limits account.Limits
	Remove bool

	// Trạng thái trước Apply, cho đường trả lại.
	prev                  map[string]saved
	dirs                  []cageDir
	sockActive, svcActive bool
	applied               bool
	// restarted: Apply đã try-restart dịch vụ với cấu hình mới, nên đường trả
	// lại phải restart (không phải start) để bản cũ chạy lại.
	restarted bool
}

// order là thứ tự ghi: hàng rào sau cùng. Lớp bọc lệnh (internal/cage) coi
// "có file hàng rào" là "site có lồng" và đưa lệnh của site vào systemd-run;
// hàng rào có trước php-fpm.conf hay passwd thì lệnh đầu tiên vào lồng thiếu
// đồ nó cần.
func (s *CageFiles) order() []string {
	l := s.Layout
	return []string{l.CageSliceFile(), l.CageSocketFile(), l.CageServiceFile(), l.CageFPMConf(), l.CagePasswd(), l.CageGroup(), l.CageFence()}
}

// dirSpecs là các thư mục của lồng, cha trước con.
func (s *CageFiles) dirSpecs() []cageDir {
	return []cageDir{
		{path: site.CageConfRoot, want: 0o700},
		{path: s.Layout.CageConfDir(), want: 0o755},
		{path: s.Layout.CageFenceDir(), want: 0o755},
	}
}

// render sinh năm file của CageManagedFiles từ template (template tự kiểm
// Layout, slug, bản PHP và số worker).
func (s *CageFiles) render() (map[string][]byte, error) {
	l := s.Layout
	slice, err := templates.CageSlice(l, s.Limits)
	if err != nil {
		return nil, err
	}
	sock, err := templates.CageSocketUnit(l)
	if err != nil {
		return nil, err
	}
	svc, err := templates.CageServiceUnit(l, s.PHP)
	if err != nil {
		return nil, err
	}
	fence, err := templates.CageFence(l)
	if err != nil {
		return nil, err
	}
	fpm, err := templates.CageFPMConf(l, s.Limits.Workers())
	if err != nil {
		return nil, err
	}
	return map[string][]byte{l.CageSliceFile(): slice, l.CageSocketFile(): sock, l.CageServiceFile(): svc,
		l.CageFence(): fence, l.CageFPMConf(): fpm}, nil
}

// Want trả nội dung mong đợi của một trong năm file của CageManagedFiles (cho
// sửa tự động file.rewrite: sha256 mong đợi). ok=false với đường dẫn khác.
func (s *CageFiles) Want(path string) ([]byte, bool) {
	m, err := s.render()
	if err != nil {
		return nil, false
	}
	b, ok := m[path]
	return b, ok
}

func cageGetentPasswd(l site.Layout) system.Cmd {
	return system.Cmd{Name: "getent", Args: []string{"passwd", "root", "www-data", l.User, "nobody"}}
}

func cageGetentGroup(l site.Layout) system.Cmd {
	return system.Cmd{Name: "getent", Args: []string{"group", "root", site.WebGroup, l.User, "nogroup"}}
}

// cageGetent chạy getent và kiểm output trước khi nó thành file passwd hay
// group của lồng: đúng một dòng cho mỗi tên, theo đúng thứ tự, mỗi dòng đúng
// số trường và tên ở trường đầu, không ký tự điều khiển. Lệch là lỗi: thà không
// ghi gì còn hơn ghi một passwd lạ mà mọi tiến trình trong lồng sẽ đọc.
func cageGetent(ctx context.Context, sys system.System, cmd system.Cmd, fields int) ([]byte, error) {
	db, names := cmd.Args[0], cmd.Args[1:]
	res, err := sys.Run(ctx, cmd)
	if err != nil {
		return nil, withStdout(fmt.Errorf("đọc %s của %s: %w", db, strings.Join(names, " "), err), res)
	}
	lines := strings.Split(strings.TrimSuffix(res.Stdout, "\n"), "\n")
	if len(lines) != len(names) {
		return nil, fmt.Errorf("getent %s trả %d dòng, muốn đúng %d (%s)", db, len(lines), len(names), strings.Join(names, " "))
	}
	for i, ln := range lines {
		f := strings.Split(ln, ":")
		if len(f) != fields || f[0] != names[i] || strings.ContainsFunc(ln, unicode.IsControl) {
			return nil, fmt.Errorf("dòng %d của getent %s không đúng dạng (muốn %d trường, tên %s): %q", i+1, db, fields, names[i], ln)
		}
	}
	return []byte(strings.Join(lines, "\n") + "\n"), nil
}

// wantAll là nội dung mong đợi của cả bảy file (kèm passwd, group từ getent).
func (s *CageFiles) wantAll(ctx context.Context, sys system.System) (map[string][]byte, error) {
	if err := cageCheck(s.Layout, s.PHP); err != nil {
		return nil, err
	}
	m, err := s.render()
	if err != nil {
		return nil, err
	}
	l := s.Layout
	if m[l.CagePasswd()], err = cageGetent(ctx, sys, cageGetentPasswd(l), 7); err != nil {
		return nil, err
	}
	if m[l.CageGroup()], err = cageGetent(ctx, sys, cageGetentGroup(l), 4); err != nil {
		return nil, err
	}
	return m, nil
}

func (s *CageFiles) removeCmds() (stopRun, reset system.Cmd) {
	l := s.Layout
	// Dấu * là mẫu tên unit do chính systemctl so, không qua shell nào.
	return system.Cmd{Name: "systemctl", Args: []string{"stop", l.CageRunPrefix() + "*"}},
		system.Cmd{Name: "systemctl", Args: []string{"reset-failed", "tpan-cage-" + l.Slug.String() + "-*"}}
}

func (s *CageFiles) Info() engine.StepInfo {
	l := s.Layout
	d := l.Domain.String()
	info := engine.StepInfo{ID: "cage.files:" + d}
	if s.Remove {
		stopRun, reset := s.removeCmds()
		info.Title = i18n.T("Gỡ file của lồng của "+d, "Remove the cage files of "+d)
		info.Why = i18n.T(
			"Site ra khỏi lồng: unit, hàng rào và cấu hình PHP-FPM riêng của lồng không còn dùng. Để sót hàng rào thì lệnh tpan chạy thay cho site (wp, cron) vẫn bị đưa vào một lồng không còn dịch vụ PHP nào đứng sau. Hàng rào được gỡ trước tiên; bước này hoàn tác được.",
			"The site leaves its cage: the cage's units, fence and own PHP-FPM configuration are no longer used. A leftover fence would keep sending the commands tpan runs for the site (wp, cron) into a cage with no PHP service behind it. The fence is removed first; this step can be undone.")
		info.Commands = cmdStrings(&cageDaemonReload, &stopRun, &reset)
		info.Danger = true
		for _, p := range l.CageManagedFiles() {
			info.Files = append(info.Files, engine.FileChange{Path: p, Delete: true})
		}
		return info
	}
	pw, gr := cageGetentPasswd(l), cageGetentGroup(l)
	test := cageFPMTest(l, s.PHP)
	restart := systemctl("try-restart", l.CageServiceUnit())
	info.Title = i18n.T("Ghi file của lồng của "+d, "Write the cage files of "+d)
	info.Why = i18n.T(
		"Lồng là một dịch vụ PHP-FPM riêng chạy bằng "+l.User+" trong slice có trần tài nguyên, sau một hàng rào chỉ cho thấy thư mục của site. Bảy file này mô tả lồng đó cho systemd và PHP-FPM; thiếu hay sai một file thì PHP của site chạy không có rào, không có trần, hoặc không chạy. Hàng rào được ghi sau cùng, cấu hình FPM được kiểm trong lồng trước khi dịch vụ khởi động lại.",
		"The cage is a dedicated PHP-FPM service running as "+l.User+" in a slice with resource limits, behind a fence that only shows the site's own directory. These seven files describe that cage to systemd and PHP-FPM; if one is missing or wrong, the site's PHP runs without the fence, without limits, or not at all. The fence is written last and the FPM configuration is checked inside the cage before the service restarts.")
	info.Commands = cmdStrings(&pw, &gr, &cageDaemonReload, &test, &restart)
	m, _ := s.render() // lỗi thì Apply báo; Info không trả lỗi được
	for _, p := range l.CageManagedFiles() {
		info.Files = append(info.Files, engine.FileChange{Path: p, New: m[p]})
	}
	return info
}

func (s *CageFiles) Check(ctx context.Context, sys system.System) (bool, error) {
	l := s.Layout
	if s.Remove {
		if err := cageCheckLayout(l); err != nil {
			return false, err
		}
		for _, p := range append(l.CageFiles(), l.CageFenceDir(), l.CageConfDir()) {
			if ok, err := absent(sys, p); err != nil || !ok {
				return false, err
			}
		}
		return true, nil
	}
	want, err := s.wantAll(ctx, sys)
	if err != nil {
		return false, fmt.Errorf("kiểm file của lồng %s: %w", l.Domain, err)
	}
	for _, d := range s.dirSpecs() {
		cd, err := captureDir(sys, d.path, d.want)
		if err != nil || !cd.ok() {
			return false, err
		}
	}
	for _, p := range s.order() {
		if ok, err := fileIs(sys, p, want[p]); err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}

// capture giữ trạng thái trước Apply: bảy file, các thư mục, socket và dịch vụ
// có đang active không.
func (s *CageFiles) capture(ctx context.Context, sys system.System) error {
	l := s.Layout
	var err error
	if s.sockActive, err = unitActive(ctx, sys, l.CageSocketUnit()); err != nil {
		return err
	}
	if s.svcActive, err = unitActive(ctx, sys, l.CageServiceUnit()); err != nil {
		return err
	}
	s.prev = map[string]saved{}
	for _, p := range l.CageFiles() {
		if s.prev[p], err = capture(sys, p); err != nil {
			return err
		}
	}
	s.dirs = s.dirSpecs()
	for i, d := range s.dirs {
		if s.dirs[i], err = captureDir(sys, d.path, d.want); err != nil {
			return err
		}
	}
	return nil
}

func (s *CageFiles) Apply(ctx context.Context, sys system.System) error {
	l := s.Layout
	if s.Remove {
		if err := s.applyRemove(ctx, sys); err != nil {
			return fmt.Errorf("gỡ file của lồng %s: %w", l.Domain, err)
		}
		return nil
	}
	if err := s.applyWrite(ctx, sys); err != nil {
		return fmt.Errorf("ghi file của lồng %s: %w", l.Domain, err)
	}
	return nil
}

func (s *CageFiles) applyWrite(ctx context.Context, sys system.System) error {
	l := s.Layout
	// Mọi thứ có thể hỏng mà không cần đụng máy (tham số, template, getent)
	// được kiểm trước khi ghi file đầu tiên.
	want, err := s.wantAll(ctx, sys)
	if err != nil {
		return err
	}
	if err := s.capture(ctx, sys); err != nil {
		return err
	}
	// /etc/tpan (của tpan setup) và /etc/systemd/system: chỉ tạo khi chưa có,
	// không ghi nhớ để trả lại. /etc/tpan không bao giờ bị đổi quyền hay xoá.
	for _, d := range []cageDir{{path: tpanEtc, want: 0o700}, {path: site.SystemdDir, want: 0o755}} {
		cd, err := captureDir(sys, d.path, d.want)
		if err != nil {
			return err
		}
		if !cd.existed {
			if err := cd.ensure(sys); err != nil {
				return err
			}
		}
	}
	fail := func(err error) error {
		if rerr := s.rollback(ctx, sys); rerr != nil {
			return errors.Join(err, fmt.Errorf("trả lại như cũ: %w", rerr))
		}
		return err
	}
	for _, d := range s.dirs {
		if err := d.ensure(sys); err != nil {
			return fail(err)
		}
	}
	written := map[string]bool{}
	for _, p := range s.order() {
		ok, err := fileIs(sys, p, want[p])
		if err != nil {
			return fail(err)
		}
		if ok {
			continue
		}
		if err := sys.WriteFileAtomic(p, want[p], 0o644, system.Root); err != nil {
			return fail(fmt.Errorf("ghi %s: %w", p, err))
		}
		written[p] = true
	}
	if len(written) == 0 {
		s.applied = true
		return nil
	}
	// Trần trong file slice có hiệu lực ngay sau daemon-reload (mục 2.4), nên
	// chỉ đổi slice thì không cần khởi động lại gì.
	if res, err := sys.Run(ctx, cageDaemonReload); err != nil {
		return fail(withStdout(fmt.Errorf("nạp lại systemd: %w", err), res))
	}
	if written[l.CageServiceFile()] || written[l.CageFPMConf()] {
		test := cageFPMTest(l, s.PHP)
		if err := runTest(ctx, sys, &test, "ghi cấu hình PHP-FPM của lồng"); err != nil {
			return fail(err)
		}
	}
	// passwd và group được gắn vào lồng theo inode: ghi bằng rename thì lồng
	// đang chạy vẫn thấy bản cũ, nên đổi chúng cũng phải khởi động lại.
	if s.svcActive && (written[l.CageServiceFile()] || written[l.CageFence()] || written[l.CageFPMConf()] ||
		written[l.CagePasswd()] || written[l.CageGroup()]) {
		// Lỗi bỏ qua (mục 3.5): file đã đúng và -t đã đạt; dịch vụ sống hay
		// chết sau đó là việc của phép kiểm cage.socket, cage.runtime và của
		// Plan gọi bước. Lệnh và stderr của nó vẫn vào log job (engine ghi mọi
		// lệnh và từng dòng output của bước).
		s.restarted = true
		_, _ = sys.Run(ctx, systemctl("try-restart", l.CageServiceUnit()))
	}
	s.applied = true
	return nil
}

func (s *CageFiles) applyRemove(ctx context.Context, sys system.System) error {
	l := s.Layout
	if err := cageCheckLayout(l); err != nil {
		return err
	}
	if err := s.capture(ctx, sys); err != nil {
		return err
	}
	fail := func(err error) error {
		if rerr := s.rollback(ctx, sys); rerr != nil {
			return errors.Join(err, fmt.Errorf("trả lại như cũ: %w", rerr))
		}
		return err
	}
	// Hàng rào trước tiên: từ lúc nó mất, lệnh của site thôi bị đưa vào một
	// lồng đang bị gỡ dở.
	files := append([]string{l.CageFence()}, slices.DeleteFunc(l.CageFiles(), func(p string) bool { return p == l.CageFence() })...)
	for _, p := range files {
		if err := sys.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fail(fmt.Errorf("xoá %s: %w", p, err))
		}
	}
	for _, p := range []string{l.CageFenceDir(), l.CageConfDir()} {
		if err := sys.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fail(fmt.Errorf("gỡ thư mục %s: %w", p, err))
		}
	}
	if err := removeEmptyDir(sys, site.CageConfRoot); err != nil {
		return fail(err)
	}
	if res, err := sys.Run(ctx, cageDaemonReload); err != nil {
		return fail(withStdout(fmt.Errorf("nạp lại systemd: %w", err), res))
	}
	// Unit tạm của lệnh chạy thay cho site còn sót (tpan chết giữa chừng) thì
	// dừng và dọn. Không có unit nào khớp thì systemctl có thể báo lỗi: bỏ qua.
	stopRun, reset := s.removeCmds()
	_, _ = sys.Run(ctx, stopRun)
	_, _ = sys.Run(ctx, reset)
	s.applied = true
	return nil
}

// rollback là đường trả lại chung của lỗi giữa Apply và của Undo (cả hai nhánh
// ghi và gỡ). Làm hết mọi việc dù một việc hỏng, rồi gộp lỗi: Undo nói thật
// khi site chưa về được như cũ.
func (s *CageFiles) rollback(ctx context.Context, sys system.System) error {
	l := s.Layout
	var errs []error
	add := func(err error) {
		if err != nil {
			errs = append(errs, err)
		}
	}
	for _, d := range s.dirs {
		add(d.restoreBefore(sys))
	}
	// File đang đúng như bản cũ thì không ghi lại: ghi lại là inode và mtime
	// mới. Hàng rào mới hơn lúc dịch vụ khởi động làm cage.runtime đòi khởi
	// động lại oan; passwd, group thay inode thì lồng đang chạy mất liên kết.
	restore := func(prev saved) {
		if cur, err := capture(sys, prev.path); err != nil || !reflect.DeepEqual(cur, prev) {
			add(prev.restore(sys))
		}
	}
	// Hàng rào: nếu trước đó chưa có thì xoá trước mọi file khác; nếu có thì
	// trả lại sau cùng. Cùng luật với lúc ghi: lệnh chỉ vào lồng khi đủ file.
	fence, hasFence := s.prev[l.CageFence()]
	if hasFence && !fence.exists {
		restore(fence)
	}
	for _, p := range s.order() {
		if prev, ok := s.prev[p]; ok && p != l.CageFence() {
			restore(prev)
		}
	}
	if hasFence && fence.exists {
		restore(fence)
	}
	for i := len(s.dirs) - 1; i >= 0; i-- {
		add(s.dirs[i].restoreAfter(sys))
	}
	if res, err := sys.Run(ctx, cageDaemonReload); err != nil {
		add(withStdout(fmt.Errorf("nạp lại systemd: %w", err), res))
	}
	// Dịch vụ hay socket có thể đã vào failed (chạm giới hạn khởi động, t14
	// P1); khi đó `start` bị từ chối cho tới khi có reset-failed. Unit chưa
	// từng nạp thì systemd báo lỗi: bỏ qua.
	_, _ = sys.Run(ctx, cageResetFailed(l))
	if s.sockActive {
		if res, err := sys.Run(ctx, systemctl("start", l.CageSocketUnit())); err != nil {
			add(withStdout(fmt.Errorf("bật lại socket %s: %w", l.CageSocketUnit(), err), res))
		} else {
			add(waitCageSocket(ctx, sys, l.CageSocket()))
		}
	}
	if s.svcActive {
		// Apply đã khởi động lại dịch vụ với file mới (đổi bản PHP, đổi số
		// worker): `start` không làm gì với dịch vụ đang chạy, dịch vụ sẽ chạy
		// mãi cấu hình mới trong khi file đã về bản cũ. restart đưa đúng bản cũ
		// chạy lại (mục 3.6: đổi bản PHP hỏng thì site về bản cũ).
		verb := "start"
		if s.restarted {
			verb = "restart"
		}
		if res, err := sys.Run(ctx, systemctl(verb, l.CageServiceUnit())); err != nil {
			add(withStdout(fmt.Errorf("bật lại dịch vụ %s: %w", l.CageServiceUnit(), err), res))
		}
	}
	return errors.Join(errs...)
}

func (s *CageFiles) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("file của lồng %s: %w", s.Layout.Domain, errNotApplied)
	}
	if err := s.rollback(ctx, sys); err != nil {
		return fmt.Errorf("trả file của lồng %s về như cũ: %w", s.Layout.Domain, err)
	}
	return nil
}

// =============================================================== CageSocket

// CageSocket bật (hoặc tắt, khi Disable) socket của lồng.
//
// Bật: Check đúng khi unit socket enabled và active, file l.CageSocket() có và
// là socket. Apply: `php-fpm<PHP> -t --fpm-config <conf>` với AsUser (trong
// lồng); `systemctl reset-failed <socket> <dịch vụ>` (bỏ qua lỗi "not loaded");
// `systemctl enable --now <socket>`; chờ tối đa 20 giây tới khi file socket
// có. KHÔNG khởi động dịch vụ (request đầu tiên đánh thức nó): không gì do mã
// của site điều khiển làm bước này hỏng. Hỏng thì lỗi kèm 20 dòng cuối
// `journalctl -u <dịch vụ> -u <socket> --no-pager -o cat`, đã bỏ ký tự điều
// khiển và ghi rõ là output của site. Undo: `disable --now` socket, `stop`
// dịch vụ.
// Tắt: Check đúng khi socket không enabled, không active, dịch vụ không active.
// Apply: `systemctl disable --now <socket>`, `systemctl stop <dịch vụ>`,
// `systemctl stop <slice>`, gỡ thư mục l.CageRunDir() nếu rỗng. Danger.
// Undo: reset-failed, enable --now socket, chờ file socket; dịch vụ từng chạy
// thì start lại.
type CageSocket struct {
	Layout  site.Layout
	PHP     string
	Disable bool

	sockEnabled, sockActive, svcActive bool
	dirExisted                         bool
	applied                            bool
}

func (s *CageSocket) Info() engine.StepInfo {
	l := s.Layout
	d := l.Domain.String()
	sock, svc := l.CageSocketUnit(), l.CageServiceUnit()
	if s.Disable {
		c1, c2, c3 := systemctlNow("disable", sock), systemctl("stop", svc), systemctl("stop", l.CageSlice())
		return engine.StepInfo{
			ID:    "cage.socket:" + d,
			Title: i18n.T("Tắt socket và dịch vụ PHP của lồng của "+d, "Stop the cage PHP socket and service of "+d),
			Why: i18n.T(
				"Site ra khỏi lồng hoặc bị xoá: socket và dịch vụ PHP riêng của lồng phải dừng, không thì PHP của site vẫn chạy (và vẫn nhận request) ở chỗ tpan không còn quản lý. Request đang chạy được chờ tối đa 25 giây; trong lúc đó và sau đó site không trả lời qua lồng.",
				"The site leaves its cage or is deleted: the cage's own PHP socket and service must stop, otherwise the site's PHP keeps running (and answering requests) somewhere tpan no longer manages. Running requests get up to 25 seconds; during and after that the site does not answer through the cage."),
			Commands: cmdStrings(&c1, &c2, &c3),
			Danger:   true,
		}
	}
	test := cageFPMTest(l, s.PHP)
	reset := system.Cmd{Name: "systemctl", Args: []string{"reset-failed", sock, svc}}
	enable := systemctlNow("enable", sock)
	return engine.StepInfo{
		ID:    "cage.socket:" + d,
		Title: i18n.T("Bật socket PHP của lồng của "+d, "Start the cage PHP socket of "+d),
		Why: i18n.T(
			"Web server chuyển file .php của "+d+" tới socket này; systemd giữ socket và đánh thức dịch vụ PHP của lồng khi có request đầu tiên. Thiếu nó thì site ra 502. Không bật khi khởi động thì sau lần khởi động lại máy site chết cho tới khi có người bật tay.",
			"The web server sends "+d+"'s .php requests to this socket; systemd holds the socket and wakes the cage's PHP service on the first request. Without it the site returns 502. If it is not enabled at boot, the site is down after the next reboot until someone starts it by hand."),
		Commands: cmdStrings(&test, &reset, &enable),
	}
}

// state ghi lại trạng thái trước Apply để Undo trả về đúng chỗ đó.
func (s *CageSocket) state(ctx context.Context, sys system.System) error {
	l := s.Layout
	var err error
	if s.sockEnabled, err = unitEnabled(ctx, sys, l.CageSocketUnit()); err != nil {
		return err
	}
	if s.sockActive, err = unitActive(ctx, sys, l.CageSocketUnit()); err != nil {
		return err
	}
	if s.svcActive, err = unitActive(ctx, sys, l.CageServiceUnit()); err != nil {
		return err
	}
	_, err = sys.Stat(l.CageRunDir())
	switch {
	case err == nil:
		s.dirExisted = true
	case errors.Is(err, fs.ErrNotExist):
		s.dirExisted = false
	default:
		return fmt.Errorf("đọc trạng thái %s: %w", l.CageRunDir(), err)
	}
	return nil
}

func (s *CageSocket) Check(ctx context.Context, sys system.System) (bool, error) {
	l := s.Layout
	if err := cageCheckLayout(l); err != nil {
		return false, err
	}
	enabled, err := unitEnabled(ctx, sys, l.CageSocketUnit())
	if err != nil {
		return false, err
	}
	active, err := unitActive(ctx, sys, l.CageSocketUnit())
	if err != nil {
		return false, err
	}
	if !s.Disable {
		return enabled && active && isSocket(sys, l.CageSocket()), nil
	}
	svc, err := unitActive(ctx, sys, l.CageServiceUnit())
	return !enabled && !active && !svc, err
}

func (s *CageSocket) Apply(ctx context.Context, sys system.System) error {
	l := s.Layout
	if s.Disable {
		if err := s.applyDisable(ctx, sys); err != nil {
			return fmt.Errorf("tắt lồng của %s: %w", l.Domain, err)
		}
		return nil
	}
	if err := s.applyEnable(ctx, sys); err != nil {
		return fmt.Errorf("bật socket của lồng %s: %w", l.Domain, err)
	}
	return nil
}

func (s *CageSocket) applyEnable(ctx context.Context, sys system.System) error {
	l := s.Layout
	if err := cageCheck(l, s.PHP); err != nil {
		return err
	}
	if err := s.state(ctx, sys); err != nil {
		return err
	}
	test := cageFPMTest(l, s.PHP)
	if err := runTest(ctx, sys, &test, "bật lồng"); err != nil {
		return err
	}
	if err := runFenceTest(ctx, sys, l, s.PHP); err != nil {
		return err
	}
	// Bỏ trạng thái failed và bộ đếm giới hạn khởi động còn lại từ lần trước
	// (mục 2.1: quá giới hạn thì cả socket lẫn dịch vụ vào failed). Unit chưa
	// từng nạp thì systemd báo "not loaded": bỏ qua.
	_, _ = sys.Run(ctx, system.Cmd{Name: "systemctl", Args: []string{"reset-failed", l.CageSocketUnit(), l.CageServiceUnit()}})
	if res, err := sys.Run(ctx, systemctlNow("enable", l.CageSocketUnit())); err != nil {
		err = withCageJournal(ctx, sys, l, withStdout(fmt.Errorf("bật %s: %w", l.CageSocketUnit(), err), res))
		return joinRevert(err, s.undoEnable(ctx, sys))
	}
	if err := waitCageSocket(ctx, sys, l.CageSocket()); err != nil {
		return joinRevert(withCageJournal(ctx, sys, l, err), s.undoEnable(ctx, sys))
	}
	s.applied = true
	return nil
}

func (s *CageSocket) applyDisable(ctx context.Context, sys system.System) error {
	l := s.Layout
	if err := cageCheckLayout(l); err != nil {
		return err
	}
	if err := s.state(ctx, sys); err != nil {
		return err
	}
	// Socket trước: từ đó không request nào đánh thức dịch vụ lại. stop dịch
	// vụ chờ request đang chạy tới process_control_timeout (25 giây). stop
	// slice dừng nốt lệnh tạm của site còn chạy trong slice.
	for _, c := range []system.Cmd{systemctlNow("disable", l.CageSocketUnit()), systemctl("stop", l.CageServiceUnit()), systemctl("stop", l.CageSlice())} {
		if res, err := sys.Run(ctx, c); err != nil {
			return joinRevert(withStdout(fmt.Errorf("%s: %w", c.String(), err), res), s.undoDisable(ctx, sys))
		}
	}
	// RemoveOnStop chỉ gỡ file socket, thư mục của nó còn lại.
	if err := removeEmptyDir(sys, l.CageRunDir()); err != nil {
		return joinRevert(err, s.undoDisable(ctx, sys))
	}
	s.applied = true
	return nil
}

func joinRevert(err, rerr error) error {
	if rerr != nil {
		return errors.Join(err, fmt.Errorf("trả lại như cũ: %w", rerr))
	}
	return err
}

// undoEnable trả socket và dịch vụ về đúng trạng thái trước Apply (thường là
// `disable --now` socket và `stop` dịch vụ).
func (s *CageSocket) undoEnable(ctx context.Context, sys system.System) error {
	l := s.Layout
	sock := l.CageSocketUnit()
	var cmds []system.Cmd
	switch {
	case !s.sockEnabled && !s.sockActive:
		cmds = append(cmds, systemctlNow("disable", sock))
	case !s.sockEnabled:
		cmds = append(cmds, systemctl("disable", sock))
	case !s.sockActive:
		cmds = append(cmds, systemctl("stop", sock))
	}
	if !s.svcActive {
		// Request tới socket có thể đã đánh thức dịch vụ trong lúc chờ.
		cmds = append(cmds, systemctl("stop", l.CageServiceUnit()))
	}
	var errs []error
	for _, c := range cmds {
		if res, err := sys.Run(ctx, c); err != nil {
			errs = append(errs, withStdout(fmt.Errorf("%s: %w", c.String(), err), res))
		}
	}
	if !s.dirExisted {
		errs = append(errs, removeEmptyDir(sys, l.CageRunDir()))
	}
	return errors.Join(errs...)
}

// undoDisable bật lại đúng những gì đã chạy trước Apply.
func (s *CageSocket) undoDisable(ctx context.Context, sys system.System) error {
	l := s.Layout
	sock := l.CageSocketUnit()
	_, _ = sys.Run(ctx, system.Cmd{Name: "systemctl", Args: []string{"reset-failed", l.CageServiceUnit(), sock}})
	var cmds []system.Cmd
	switch {
	case s.sockEnabled && s.sockActive:
		cmds = append(cmds, systemctlNow("enable", sock))
	case s.sockEnabled:
		cmds = append(cmds, systemctl("enable", sock))
	case s.sockActive:
		cmds = append(cmds, systemctl("start", sock))
	}
	for _, c := range cmds {
		if res, err := sys.Run(ctx, c); err != nil {
			return withStdout(fmt.Errorf("%s: %w", c.String(), err), res)
		}
	}
	if s.sockActive {
		if err := waitCageSocket(ctx, sys, l.CageSocket()); err != nil {
			return err
		}
	}
	if s.svcActive {
		if res, err := sys.Run(ctx, systemctl("start", l.CageServiceUnit())); err != nil {
			return withStdout(fmt.Errorf("bật lại %s: %w", l.CageServiceUnit(), err), res)
		}
	}
	return nil
}

func (s *CageSocket) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("socket của lồng %s: %w", s.Layout.Domain, errNotApplied)
	}
	var err error
	if s.Disable {
		err = s.undoDisable(ctx, sys)
	} else {
		err = s.undoEnable(ctx, sys)
	}
	if err != nil {
		return fmt.Errorf("trả socket của lồng %s về như cũ: %w", s.Layout.Domain, err)
	}
	return nil
}

// ================================================================= CageReap

// cageUID là uid của user của site, từ `id -u`. uid 0 bị từ chối: bước này
// không bao giờ được giết tiến trình của root.
func cageUID(ctx context.Context, sys system.System, l site.Layout) (string, error) {
	if err := cageCheckLayout(l); err != nil {
		return "", err
	}
	cmd := system.Cmd{Name: "id", Args: []string{"-u", l.User}}
	res, err := sys.Run(ctx, cmd)
	if err != nil {
		return "", withStdout(fmt.Errorf("đọc uid của %s: %w", l.User, err), res)
	}
	uid := strings.TrimSpace(res.Stdout)
	if n, err := strconv.ParseUint(uid, 10, 32); err != nil || n == 0 {
		return "", fmt.Errorf("uid của %s là %q: không phải số khác 0", l.User, uid)
	}
	return uid, nil
}

// cageCgroupPath là đường cgroup của slice như /proc/<pid>/cgroup in ra
// (/tpan.slice/tpan-<slug>.slice).
func cageCgroupPath(l site.Layout) string {
	return strings.TrimPrefix(l.CageCgroup(), path.Dir(site.CageCgroupRoot))
}

// procField trả giá trị của dòng "<key>:" trong /proc/<pid>/status.
func procField(status []byte, key string) string {
	for _, ln := range strings.Split(string(status), "\n") {
		if v, ok := strings.CutPrefix(ln, key+":"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// cgroupUnder: dòng "0::" (cgroup v2) của /proc/<pid>/cgroup nằm ở cg hoặc
// dưới nó. So bằng dấu bằng hoặc tiền tố có "/" ở cuối: tpan-a.slice không
// nhận nhầm tpan-ab.slice. Không có dòng "0::" thì coi là ngoài.
func cgroupUnder(cgroup []byte, cg string) bool {
	for _, ln := range strings.Split(string(cgroup), "\n") {
		if p, ok := strings.CutPrefix(ln, "0::"); ok {
			return p == cg || strings.HasPrefix(p, cg+"/")
		}
	}
	return false
}

// cageStray: pid còn sống, mang uid của site, ở ngoài slice của site.
//
// Uid là uid thật hoặc uid hiệu lực (hai trường đầu của dòng "Uid:"): tiến
// trình nào chạy với quyền của site theo một trong hai nghĩa đó đều mở được
// /proc/<pid>/root của nhau (K1.1b). Tiến trình zombie bỏ qua: nó không còn
// chạy mã, không còn gốc hệ file nào để mở, và chỉ biến mất khi cha của nó
// đọc mã thoát (không lệnh kill nào làm nhanh hơn). Đọc lỗi (tiến trình vừa
// thoát) thì bỏ qua.
func cageStray(sys system.System, pid, uid, cg string) bool {
	st, err := sys.ReadFile("/proc/" + pid + "/status")
	if err != nil {
		return false
	}
	ids := strings.Fields(procField(st, "Uid"))
	if len(ids) < 2 || (ids[0] != uid && ids[1] != uid) {
		return false
	}
	if state := procField(st, "State"); strings.HasPrefix(state, "Z") || strings.HasPrefix(state, "X") {
		return false
	}
	cgroup, err := sys.ReadFile("/proc/" + pid + "/cgroup")
	return err == nil && !cgroupUnder(cgroup, cg)
}

// cageScan trả các pid mang uid của site đang chạy ngoài slice cg.
func cageScan(sys system.System, uid, cg string) ([]string, error) {
	names, err := sys.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("đọc /proc: %w", err)
	}
	var pids []string
	for _, n := range names {
		if n == "" || strings.Trim(n, "0123456789") != "" {
			continue
		}
		if cageStray(sys, n, uid, cg) {
			pids = append(pids, n)
		}
	}
	return pids, nil
}

// cageCrontab là crontab riêng của user của site trong spool của cron (Debian).
// Mã của site cài được nó lúc còn ở pool chung (crontab là setgid, pool chung
// không có NoNewPrivileges), rồi cron (root) chạy nó dưới uid của site ngoài
// hàng rào và ngoài slice mãi mãi, kể cả sau khi vào lồng (soát cuối M17, N3).
// tpan không bao giờ dùng crontab của user: việc định kỳ của site nằm ở
// /etc/cron.d.
func cageCrontab(l site.Layout) string { return "/var/spool/cron/crontabs/" + l.User }

func cageCrontabRemove(l site.Layout) system.Cmd {
	return system.Cmd{Name: "crontab", Args: []string{"-r", "-u", l.User}}
}

// cageKillAll là `kill -KILL -1` chạy dưới user của site (AsUser: qua lớp bọc
// thì trong lồng). kill(-1) gửi tín hiệu tới mọi tiến trình cùng uid trên máy
// trong một lượt duyệt của kernel (lồng không có PID namespace riêng), nên một
// chuỗi tiến trình "fork rồi tự thoát" không nhảy pid thoát được như với
// `kill <pid>` (soát cuối M17, N2). Nó giết cả tiến trình của site trong
// slice: chỉ chạy khi đằng nào cũng phải giết (cage.enable trước khi request
// đầu vào lồng, cage.restart ngay trước khi khởi động lại).
func cageKillAll(l site.Layout) system.Cmd {
	return system.Cmd{Name: "kill", Args: []string{"-KILL", "-1"}, AsUser: l.User}
}

// cageReap gỡ crontab riêng của user của site, giết mọi tiến trình của site ở
// ngoài slice và chờ chúng biến mất. Mỗi vòng: `kill -KILL -1` dưới uid của
// site, rồi `kill -KILL <pid>` bằng root cho những pid quét thấy (đường dự
// phòng khi mã của site lấp số tiến trình của slice và unit tạm của lớp bọc
// không khởi động được), rồi quét lại; tới cageReapWait mà còn thì lỗi.
func cageReap(ctx context.Context, sys system.System, l site.Layout) error {
	uid, err := cageUID(ctx, sys, l)
	if err != nil {
		return err
	}
	if err := cageCrontabGone(ctx, sys, l); err != nil {
		return err
	}
	cg := cageCgroupPath(l)
	deadline := time.Now().Add(cageReapWait)
	for {
		pids, err := cageScan(sys, uid, cg)
		if err != nil || len(pids) == 0 {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("sau %v vẫn còn tiến trình của %s ngoài lồng: %s", cageReapWait, l.User, strings.Join(pids, " "))
		}
		// Mọi lỗi bỏ qua trừ ctx: chính lệnh kill cũng có thể bị giết theo
		// (KilledError), hay unit tạm không lên được; lần quét sau là câu trả lời.
		_, _ = sys.Run(ctx, cageKillAll(l))
		if ctx.Err() != nil {
			return fmt.Errorf("giết tiến trình của %s ngoài lồng: %w", l.User, ctx.Err())
		}
		// pid có thể đã được dùng lại giữa lần quét và lúc giết: đọc lại ngay
		// trước kill, chỉ giữ pid vẫn cùng uid và vẫn ngoài slice (mục 5, K3).
		var kill []string
		for _, p := range pids {
			if cageStray(sys, p, uid, cg) {
				kill = append(kill, p)
			}
		}
		if len(kill) > 0 {
			res, err := sys.Run(ctx, system.Cmd{Name: "kill", Args: append([]string{"-KILL"}, kill...)})
			// Mã thoát khác 0: thường là một pid vừa tự thoát ("No such process").
			// Lần quét sau mới là câu trả lời; chỉ lỗi không chạy được kill mới dừng.
			if _, isEx := isExit(err); err != nil && !isEx {
				return withStdout(fmt.Errorf("giết tiến trình của %s ngoài lồng: %w", l.User, err), res)
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("chờ tiến trình của %s thoát: %w", l.User, ctx.Err())
		case <-time.After(cageWaitTick):
		}
	}
}

// cageCrontabGone gỡ crontab riêng của user của site (cageCrontab) bằng root.
// `crontab -r` thoát khác 0 khi user không có crontab: không phải lỗi; file
// còn sau lệnh thì mới là lỗi.
func cageCrontabGone(ctx context.Context, sys system.System, l site.Layout) error {
	if _, err := sys.Stat(cageCrontab(l)); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	res, rerr := sys.Run(ctx, cageCrontabRemove(l))
	if _, err := sys.Stat(cageCrontab(l)); !errors.Is(err, fs.ErrNotExist) {
		if rerr == nil {
			rerr = fmt.Errorf("file vẫn còn")
		}
		return withStdout(fmt.Errorf("gỡ crontab riêng của %s (%s): %w", l.User, cageCrontab(l), rerr), res)
	}
	return nil
}

// cageNoStray: không còn tiến trình nào của site ở ngoài slice, và user của
// site không có crontab riêng.
func cageNoStray(ctx context.Context, sys system.System, l site.Layout) (bool, error) {
	uid, err := cageUID(ctx, sys, l)
	if err != nil {
		return false, err
	}
	if _, err := sys.Stat(cageCrontab(l)); !errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	pids, err := cageScan(sys, uid, cageCgroupPath(l))
	return len(pids) == 0, err
}

// CageReap gỡ crontab riêng của user của site (`crontab -r -u <user>`, soát
// cuối M17 N3) và giết (SIGKILL) mọi tiến trình có uid là user của site mà
// không nằm trong slice của site (mục 3.5, soát thiết kế điểm 4). Uid: `id -u
// <user>`. Đọc /proc bằng System (ReadDir("/proc"), ReadFile status và cgroup
// của từng số; tiến trình biến mất giữa chừng thì bỏ qua). Giết theo vòng
// (cageReap): `kill -KILL -1` dưới uid của site, rồi `kill -KILL <pid>...`
// bằng root cho pid đọc lại vẫn cùng uid và vẫn ngoài slice. Check đúng khi
// không còn tiến trình nào như vậy và không còn crontab. Undo không làm gì.
type CageReap struct {
	Layout site.Layout
}

func cageReapCmds(l site.Layout) []string {
	id := system.Cmd{Name: "id", Args: []string{"-u", l.User}}
	cron := cageCrontabRemove(l)
	all := cageKillAll(l)
	kill := system.Cmd{Name: "kill", Args: []string{"-KILL", "<pid>..."}}
	return cmdStrings(&id, &cron, &all, &kill)
}

func (s *CageReap) Info() engine.StepInfo {
	l := s.Layout
	d := l.Domain.String()
	return engine.StepInfo{
		ID:    "cage.reap:" + d,
		Title: i18n.T("Dừng tiến trình của "+l.User+" còn chạy ngoài lồng", "Stop the processes of "+l.User+" still running outside the cage"),
		Why: i18n.T(
			"Một tiến trình cùng user với site mà chạy ngoài lồng (pool PHP cũ, lệnh wp dở dang) là cửa thoát: mã trong lồng mở được /proc/<pid>/root của nó và đọc, ghi được hệ file thật của máy. Bước này giết các tiến trình đó trước khi mã của site được chạy trong lồng.",
			"A process running as the site's user outside the cage (the old PHP pool, an unfinished wp command) is a way out: code inside the cage can open its /proc/<pid>/root and read and write the machine's real file system. This step kills those processes before the site's code runs inside the cage."),
		Commands: cageReapCmds(l),
		Danger:   true,
		NoUndo: i18n.T(
			"Tiến trình đã giết không trả lại được; đó là tiến trình của site chạy ngoài hàng rào.",
			"Killed processes cannot be brought back; they were the site's processes running outside the fence."),
	}
}

func (s *CageReap) Check(ctx context.Context, sys system.System) (bool, error) {
	ok, err := cageNoStray(ctx, sys, s.Layout)
	if err != nil {
		return false, fmt.Errorf("tìm tiến trình của %s ngoài lồng: %w", s.Layout.Domain, err)
	}
	return ok, nil
}

func (s *CageReap) Apply(ctx context.Context, sys system.System) error {
	if err := cageReap(ctx, sys, s.Layout); err != nil {
		return fmt.Errorf("dừng tiến trình của %s ngoài lồng: %w", s.Layout.Domain, err)
	}
	return nil
}

func (s *CageReap) Undo(context.Context, system.System) error { return nil }

// ============================================================== CageRestart

// CageRestart là sửa tự động cage.restart (mục 3.5, 3.7): chạy
// templates.CageMySQLDirCmd bằng root ở máy thật, rồi việc của CageReap, rồi
// `systemctl try-restart <dịch vụ>`. Check đúng khi dịch vụ không chạy, hoặc
// khi mọi điều của checks.CageRuntime đạt. Undo không làm gì (NoUndo).
type CageRestart struct {
	Layout site.Layout
}

func (s *CageRestart) Info() engine.StepInfo {
	l := s.Layout
	d := l.Domain.String()
	mk := system.Cmd{Name: templates.CageMySQLDirCmd[0], Args: templates.CageMySQLDirCmd[1:]}
	restart := systemctl("try-restart", l.CageServiceUnit())
	return engine.StepInfo{
		ID:    "cage.restart:" + d,
		Title: i18n.T("Khởi động lại dịch vụ PHP của lồng của "+d, "Restart the cage PHP service of "+d),
		Why: i18n.T(
			"Tiến trình PHP đang chạy của lồng giữ những gì nó thấy lúc khởi động: hàng rào cũ, bản PHP đã bị thay khi cập nhật gói, hay một /run/mysqld đã mất (site 500 mãi dù MariaDB đã lên). Bước này tạo lại /run/mysqld, dừng tiến trình của site còn ở ngoài lồng, rồi khởi động lại dịch vụ; request tới trong lúc đó xếp hàng trên socket, không rớt.",
			"The cage's running PHP process keeps what it saw at start: an old fence, a PHP binary replaced by a package update, or a /run/mysqld that is gone (the site returns 500 even after MariaDB is back). This step recreates /run/mysqld, stops the site's processes still outside the cage, then restarts the service; requests arriving meanwhile queue on the socket instead of failing."),
		Commands: append(append(cmdStrings(&mk), cageReapCmds(l)...), restart.String()),
		Danger:   true,
		NoUndo: i18n.T(
			"Khởi động lại không đổi cấu hình nào, và tiến trình đã giết không trả lại được.",
			"A restart changes no configuration, and killed processes cannot be brought back."),
	}
}

// mountLine là một dòng của /proc/<pid>/mountinfo: trường root (thứ 4) và loại
// hệ file (trường đầu tiên sau dấu "-").
type mountLine struct{ root, fstype string }

// parseMountinfo trả dòng cuối cho mỗi điểm gắn: nhiều lần gắn chồng lên một
// điểm thì tiến trình thấy lần sau cùng.
func parseMountinfo(b []byte) map[string]mountLine {
	m := map[string]mountLine{}
	for _, ln := range strings.Split(string(b), "\n") {
		f := strings.Fields(ln)
		if len(f) < 5 {
			continue
		}
		ml := mountLine{root: f[3]}
		if i := slices.Index(f[5:], "-"); i >= 0 && 5+i+1 < len(f) {
			ml.fstype = f[5+i+1]
		}
		m[f[4]] = ml
	}
	return m
}

// cageShow chạy `systemctl show <args> --value <unit>` và trả giá trị đã cắt
// khoảng trắng.
func cageShow(ctx context.Context, sys system.System, unit string, args ...string) (string, error) {
	cmd := system.Cmd{Name: "systemctl", Args: append(append([]string{"show"}, args...), "--value", unit)}
	res, err := sys.Run(ctx, cmd)
	if err != nil {
		return "", withStdout(fmt.Errorf("hỏi systemd %s: %w", unit, err), res)
	}
	return strings.TrimSpace(res.Stdout), nil
}

// runtimeOK kiểm tiến trình chính đang chạy của lồng theo các điều của
// cage.runtime (mục 3.7), kể cả hai điều chỉ là cảnh báo ở phép kiểm (exe đã
// bị thay, hàng rào mới hơn lúc dịch vụ khởi động): đó chính là hai trường hợp
// bước này sinh ra để sửa. Đọc lỗi (tiến trình vừa thoát) là "chưa đạt": Apply
// khởi động lại dịch vụ.
func (s *CageRestart) runtimeOK(ctx context.Context, sys system.System, pid string) (bool, error) {
	l := s.Layout
	p := "/proc/" + pid
	st, err := sys.ReadFile(p + "/status")
	if err != nil {
		return false, nil
	}
	if procField(st, "NoNewPrivs") != "1" {
		return false, nil
	}
	if capEff, err := strconv.ParseUint(procField(st, "CapEff"), 16, 64); err != nil || capEff != 0 {
		return false, nil
	}
	if cgroup, err := sys.ReadFile(p + "/cgroup"); err != nil || !cgroupUnder(cgroup, cageCgroupPath(l)) {
		return false, nil
	}
	mi, err := sys.ReadFile(p + "/mountinfo")
	if err != nil {
		return false, nil
	}
	mounts := parseMountinfo(mi)
	for _, mp := range []string{"/var", "/run", tpanEtc} {
		if mounts[mp].fstype != "tmpfs" {
			return false, nil
		}
	}
	// Lệnh gắn /run/mysqld có dấu "-": thư mục chưa có lúc lồng khởi động thì
	// lồng chạy mà không bao giờ thấy MariaDB (mục 2.8, t11). Chỉ đòi khi
	// MariaDB đang chạy: máy không dùng MariaDB thì không có gì để gắn.
	mariadb, err := unitActive(ctx, sys, "mariadb")
	if err != nil {
		return false, err
	}
	if my, ok := mounts["/run/mysqld"]; mariadb && (!ok || strings.Contains(my.root, "//deleted")) {
		return false, nil
	}
	if r, ok := mounts["/run/systemd/resolve"]; ok && strings.Contains(r.root, "//deleted") {
		return false, nil
	}
	// passwd, group riêng bị kernel gỡ khi user trên máy đổi (cageRebindIdentity).
	if mounts["/etc/passwd"].root != l.CagePasswd() || mounts["/etc/group"].root != l.CageGroup() {
		return false, nil
	}
	exe, err := sys.Readlink(p + "/exe")
	if err != nil || strings.HasSuffix(exe, " (deleted)") {
		return false, nil
	}
	// Hàng rào sửa sau lúc dịch vụ vào active: tiến trình đang chạy vẫn mang
	// hàng rào cũ. Không đọc được một trong hai mốc thì không phán điều này
	// (cage.state lo chuyện thiếu hàng rào).
	at, err := cageShow(ctx, sys, l.CageServiceUnit(), "--timestamp=unix", "-p", "ActiveEnterTimestamp")
	if err != nil {
		return false, err
	}
	active, aerr := strconv.ParseInt(strings.TrimPrefix(at, "@"), 10, 64)
	res, serr := sys.Run(ctx, system.Cmd{Name: "stat", Args: []string{"-c", "%Y", l.CageFence()}})
	fence, ferr := strconv.ParseInt(strings.TrimSpace(res.Stdout), 10, 64)
	if aerr == nil && serr == nil && ferr == nil && fence > active {
		return false, nil
	}
	return true, nil
}

func (s *CageRestart) Check(ctx context.Context, sys system.System) (bool, error) {
	l := s.Layout
	// Tiến trình của site ngoài slice là điều của cả máy trong cage.runtime:
	// đòi cả khi dịch vụ đang ngủ, không thì sửa tự động không bao giờ chạy.
	ok, err := cageNoStray(ctx, sys, l)
	if err != nil || !ok {
		return false, wrapErr("kiểm lồng đang chạy của "+l.Domain.String(), err)
	}
	pid, err := cageShow(ctx, sys, l.CageServiceUnit(), "-p", "MainPID")
	if err != nil {
		return false, err
	}
	if pid == "" || pid == "0" {
		return true, nil // lồng đang ngủ: request đầu tiên khởi động nó với file mới
	}
	if strings.Trim(pid, "0123456789") != "" {
		return false, fmt.Errorf("MainPID của %s là %q, không phải số", l.CageServiceUnit(), pid)
	}
	return s.runtimeOK(ctx, sys, pid)
}

func wrapErr(what string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", what, err)
}

func (s *CageRestart) Apply(ctx context.Context, sys system.System) error {
	l := s.Layout
	if err := cageCheckLayout(l); err != nil {
		return err
	}
	// Lỗi bỏ qua, như dấu "-" của chính dòng này trong unit socket: máy không
	// cài MariaDB thì không có user mysql và cũng không cần thư mục này.
	_, _ = sys.Run(ctx, system.Cmd{Name: templates.CageMySQLDirCmd[0], Args: slices.Clone(templates.CageMySQLDirCmd[1:])})
	if err := cageReap(ctx, sys, l); err != nil {
		return fmt.Errorf("khởi động lại lồng của %s: %w", l.Domain, err)
	}
	if res, err := sys.Run(ctx, systemctl("try-restart", l.CageServiceUnit())); err != nil {
		return withStdout(fmt.Errorf("khởi động lại %s: %w", l.CageServiceUnit(), err), res)
	}
	return nil
}

func (s *CageRestart) Undo(context.Context, system.System) error { return nil }

// ======================================================= cageRebindIdentity

// cageIdentityUnits là mẫu tên dịch vụ PHP của mọi lồng (systemctl list-units).
const cageIdentityUnits = "tpan-cage-*-php.service"

// cageRebindIdentity gắn lại passwd, group riêng của mọi lồng đang chạy lên
// /etc/passwd, /etc/group trong mount namespace của nó. Gọi ngay sau mỗi lệnh
// đổi user hay nhóm trên máy (useradd, userdel, usermod -aG, gpasswd -d): các
// lệnh đó thay /etc/passwd, /etc/group bằng rename, và kernel gỡ mọi lần gắn
// file lên đúng chỗ đó trong mount namespace khác, nên dịch vụ PHP của lồng đã
// chạy từ trước thấy lại passwd của cả máy (tên user tp_* của mọi site; gặp ở
// e2e M17). `systemctl bind --read-only` gắn lại mà không khởi động lại dịch
// vụ (thử trên systemd 255 dưới hàng rào của lồng: được, chạy lại thì thay lần
// gắn cũ chứ không chồng thêm).
//
// Không bao giờ trả lỗi: đây là việc phụ của bước đổi user (bước đó đã xong),
// và mã của site làm hỏng được nó (tự giết tiến trình chính đúng lúc). Lệnh và
// output vẫn vào log của job. Lồng nào không gắn lại được thì cage.runtime của
// health báo lỗi, kèm sửa tự động cage.restart (khởi động lại thì namespace
// mới có đủ lần gắn); đó cũng là đường bắt các thay đổi user không do tpan
// làm (gói apt tạo user hệ thống).
func cageRebindIdentity(ctx context.Context, sys system.System) {
	res, err := sys.Run(ctx, system.Cmd{Name: "systemctl", Args: []string{
		"list-units", "--type=service", "--state=active", "--no-legend", "--plain", cageIdentityUnits}})
	if err != nil {
		return
	}
	for _, ln := range strings.Split(res.Stdout, "\n") {
		f := strings.Fields(ln)
		if len(f) == 0 {
			continue
		}
		unit := f[0]
		rest, ok1 := strings.CutPrefix(unit, "tpan-cage-")
		slug, ok2 := strings.CutSuffix(rest, "-php.service")
		if !ok1 || !ok2 {
			continue
		}
		if _, ok := site.CageSlugOfUser("tp_" + slug); !ok {
			continue // slug lạ: không ghép nó vào đường dẫn
		}
		dir := site.CageConfRoot + "/" + slug
		for _, p := range [][2]string{{dir + "/passwd", "/etc/passwd"}, {dir + "/group", "/etc/group"}} {
			_, _ = sys.Run(ctx, system.Cmd{Name: "systemctl", Args: []string{"bind", "--read-only", unit, p[0], p[1]}})
		}
	}
}

// ============================================================ SiteVhostCaged

// SiteVhostCaged ghi vhost bản trong lồng (templates.SiteVhostCaged: socket là
// l.CageSocket()) vào sites-available rồi bật bằng symlink, như SiteVhost và
// SiteVhostSSL (ssl chọn bản có cổng 443). Cùng đường dẫn file với vhost ngoài
// lồng: vào lồng và ra lồng là ghi đè cùng một file.
func SiteVhostCaged(l site.Layout, web, acmeRoot string, ssl bool, mode site.HtaccessMode) ([]engine.Step, error) {
	if err := webCheckLayout(l); err != nil {
		return nil, fmt.Errorf("dựng bước vhost trong lồng: %w", err)
	}
	v, err := webVhostOf(l, web)
	if err != nil {
		return nil, fmt.Errorf("dựng bước vhost trong lồng: %w", err)
	}
	conf, err := templates.SiteVhostCaged(l, web, acmeRoot, ssl, mode)
	if err != nil {
		return nil, fmt.Errorf("dựng bước vhost trong lồng %s: %w", web, err)
	}
	d := l.Domain.String()
	title := i18n.T("Ghi vhost "+web+" cho "+d+" (PHP trong lồng)", "Write "+web+" vhost for "+d+" (caged PHP)")
	if ssl {
		title = i18n.T("Ghi vhost "+web+" có HTTPS cho "+d+" (PHP trong lồng)", "Write "+web+" HTTPS vhost for "+d+" (caged PHP)")
	}
	return []engine.Step{
		&File{Path: v.conf, Content: conf, Perm: 0o644, Owner: system.Root, Test: v.test, Reload: v.reload,
			Title: title,
			Why: i18n.T(
				"Vhost nói cho "+web+" biết request tới "+d+" lấy file ở đâu và chuyển .php tới đâu. Site ở trong lồng nên .php phải tới socket của lồng; vhost còn trỏ socket của pool chung thì site ra 502 hoặc chạy PHP ngoài lồng.",
				"The vhost tells "+web+" where to find files for "+d+" and where to send .php requests. The site is caged, so .php must go to the cage's socket; a vhost still pointing at the shared pool's socket gives 502 or runs PHP outside the cage.")},
		&Symlink{Target: v.conf, Link: v.link, Test: v.test, Reload: v.reload},
	}, nil
}
