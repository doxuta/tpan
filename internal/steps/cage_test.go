package steps

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
	"github.com/doxuta/tpan/internal/validate"
)

// ---- Máy giả của lồng ----
//
// machine của machine_test.go không đủ cho lồng: `start` không đổi trạng thái,
// `disable --now` không dừng unit, và không có MainPID, /proc, getent, id.
// cageFake là FakeSystem cộng một systemd nhỏ có trạng thái (active, enabled,
// failed), một tiến trình chính giả của dịch vụ trong /proc, và ghi lại thứ
// tự ghi và xoá file.

type cageUnit struct{ Active, Enabled, Failed bool }

type cageFake struct {
	*system.FakeSystem
	l site.Layout

	units    map[string]cageUnit
	pid      int // MainPID của dịch vụ; 0 khi không chạy
	nextPID  int
	clock    int64 // giây, tăng mỗi lần dịch vụ khởi động
	activeAt int64
	fenceAt  int64

	mariadb     bool
	fpmFail     bool
	restartFail bool
	enableFail  bool
	noSocket    bool // enable --now socket mà systemd không tạo file socket
	killExit    int
	uid         string
	passwd      string // khác rỗng: output của getent passwd thay cho bản chuẩn
	group       string

	writes, removes []string
}

func (f *cageFake) WriteFileAtomic(p string, data []byte, perm fs.FileMode, owner system.Owner) error {
	f.writes = append(f.writes, p)
	return f.FakeSystem.WriteFileAtomic(p, data, perm, owner)
}

func (f *cageFake) Remove(p string) error {
	f.removes = append(f.removes, p)
	return f.FakeSystem.Remove(p)
}

func cageLayout(t *testing.T) site.Layout {
	t.Helper()
	d, err := validate.ParseDomain("shop.example.com")
	if err != nil {
		t.Fatal(err)
	}
	return site.LayoutOf(d)
}

const siteUID = "1001"

func cagePasswdOut(l site.Layout) string {
	return "root:x:0:0:root:/root:/bin/bash\n" +
		"www-data:x:33:33:www-data:/var/www:/usr/sbin/nologin\n" +
		l.User + ":x:" + siteUID + ":" + siteUID + "::" + l.Dir + ":/usr/sbin/nologin\n" +
		"nobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin\n"
}

func cageGroupOut(l site.Layout) string {
	return "root:x:0:\nwww-data:x:33:\n" + l.User + ":x:" + siteUID + ":\nnogroup:x:65534:\n"
}

// goodMountinfo là mountinfo của tiến trình chính trong lồng như đo ở t1.
const goodMountinfo = "1 0 8:1 / / ro,relatime shared:1 - ext4 /dev/sda1 rw\n" +
	"2 1 0:30 / /var rw,nosuid - tmpfs tmpfs rw\n" +
	"3 1 0:31 / /run rw,nosuid - tmpfs tmpfs rw\n" +
	"4 1 0:32 / /etc/tpan rw - tmpfs tmpfs rw\n" +
	"5 3 0:25 /mysqld /run/mysqld rw shared:9 - tmpfs tmpfs rw\n" +
	"6 3 0:25 /systemd/resolve /run/systemd/resolve rw - tmpfs tmpfs rw\n"

func procStatus(uid, state, capEff, nnp string) string {
	return "Name:\tphp-fpm8.3\nState:\t" + state + "\nUid:\t" + uid + "\t" + uid + "\t" + uid + "\t" + uid +
		"\nCapEff:\t" + capEff + "\nNoNewPrivs:\t" + nnp + "\n"
}

func newCageFake(t *testing.T) *cageFake {
	t.Helper()
	l := cageLayout(t)
	f := &cageFake{FakeSystem: system.NewFake(), l: l, units: map[string]cageUnit{},
		nextPID: 4000, clock: 1000, uid: siteUID}
	// Máy đã chạy tpan setup: có /etc/tpan 0700 và thư mục unit của systemd.
	f.SetDir(tpanEtc, 0o700, system.Root)
	f.SetDir(site.SystemdDir, 0o755, system.Root)
	f.SetDir("/proc", 0o555, system.Root)
	// /run/tpan-cage là thư mục cha chung của socket mọi lồng (systemd tạo ở lần
	// bật lồng đầu tiên, mất khi máy khởi động lại); ở đây coi như đã có.
	f.SetDir(site.CageRunRoot, 0o755, system.Root)
	f.SetFile("/proc/meminfo", []byte("MemTotal: 1 kB\n"), 0o444, system.Root)

	ok := system.Result{}
	exit := func(code int, out, errs string) system.Result {
		return system.Result{ExitCode: code, Stdout: out, Stderr: errs}
	}
	f.Handle([]string{"getent", "passwd"}, func(system.Cmd) system.Result {
		if f.passwd != "" {
			return exit(0, f.passwd, "")
		}
		return exit(0, cagePasswdOut(l), "")
	})
	f.Handle([]string{"getent", "group"}, func(system.Cmd) system.Result {
		if f.group != "" {
			return exit(0, f.group, "")
		}
		return exit(0, cageGroupOut(l), "")
	})
	f.Handle([]string{"id", "-u"}, func(system.Cmd) system.Result { return exit(0, f.uid+"\n", "") })
	f.Handle([]string{"php-fpm8.3", "-t"}, func(system.Cmd) system.Result {
		if f.fpmFail {
			return exit(78, "", "ERROR: [pool tpan-x] pm.max_children must be a positive value\n")
		}
		return exit(0, "", "NOTICE: configuration file test is successful\n")
	})
	f.Handle([]string{"stat", "-c", "%Y"}, func(system.Cmd) system.Result {
		return exit(0, strconv.FormatInt(f.fenceAt, 10)+"\n", "")
	})
	f.Handle([]string{"journalctl"}, func(system.Cmd) system.Result {
		return exit(0, "php-fpm started\n\x1b[31mWARNING\x1b[0m: child said \u202ehi\r\nFatal error in /var/www/x.php\n", "")
	})
	f.Handle([]string{"kill", "-KILL"}, func(c system.Cmd) system.Result {
		for _, p := range c.Args[1:] {
			f.killProc(p)
		}
		return exit(f.killExit, "", "")
	})
	f.Handle([]string{"systemctl"}, func(c system.Cmd) system.Result {
		a := c.Args
		unit := a[len(a)-1]
		u := f.units[unit]
		now := len(a) > 2 && a[1] == "--now"
		switch a[0] {
		case "is-active":
			if u.Active {
				return exit(0, "active\n", "")
			}
			if u.Failed {
				return exit(3, "failed\n", "")
			}
			return exit(3, "inactive\n", "")
		case "is-enabled":
			if u.Enabled {
				return exit(0, "enabled\n", "")
			}
			return exit(1, "disabled\n", "")
		case "daemon-reload":
		case "reset-failed":
			for _, n := range a[1:] {
				if x, found := f.units[n]; found {
					x.Failed = false
					f.units[n] = x
				}
			}
		case "enable":
			if f.enableFail {
				return exit(1, "", "Job for "+unit+" failed.\n")
			}
			u.Enabled = true
			f.units[unit] = u
			if now {
				return f.start(unit)
			}
		case "disable":
			u.Enabled = false
			f.units[unit] = u
			if now {
				f.stop(unit)
			}
		case "start":
			return f.start(unit)
		case "stop":
			f.stop(unit)
		case "restart":
			// restart khởi động cả unit đang dừng (khác try-restart).
			if u.Active {
				f.stop(unit)
			}
			return f.start(unit)
		case "try-restart":
			if f.restartFail {
				return exit(1, "", "Job for "+unit+" failed because the control process exited with error code.\n")
			}
			if u.Active {
				f.stop(unit)
				return f.start(unit)
			}
		case "show":
			switch {
			case slices.Contains(a, "MainPID"):
				return exit(0, strconv.Itoa(f.pid)+"\n", "")
			case slices.Contains(a, "ActiveEnterTimestamp"):
				return exit(0, "@"+strconv.FormatInt(f.activeAt, 10)+"\n", "")
			}
		}
		return ok
	})
	return f
}

// start: systemd từ chối start một unit đang failed (chạm giới hạn khởi động)
// cho tới khi có reset-failed (t14 P1).
func (f *cageFake) start(unit string) system.Result {
	u := f.units[unit]
	if u.Failed {
		return system.Result{ExitCode: 1, Stderr: "Job for " + unit + " failed: start-limit-hit\n"}
	}
	u.Active = true
	f.units[unit] = u
	switch unit {
	case f.l.CageSocketUnit():
		if !f.noSocket {
			f.SetSocket(f.l.CageSocket(), 0o660, system.Owner{User: "www-data", Group: "www-data"})
		}
	case f.l.CageServiceUnit():
		if f.pid == 0 {
			f.nextPID++
			f.clock++
			f.pid, f.activeAt = f.nextPID, f.clock
			f.setMain(procStatus(f.uid, "S (sleeping)", "0000000000000000", "1"), goodMountinfo, "/usr/sbin/php-fpm8.3")
		}
	}
	return system.Result{}
}

func (f *cageFake) stop(unit string) {
	u := f.units[unit]
	u.Active = false
	f.units[unit] = u
	switch unit {
	case f.l.CageSocketUnit():
		_ = f.FakeSystem.Remove(f.l.CageSocket()) // RemoveOnStop
	case f.l.CageServiceUnit():
		if f.pid != 0 {
			f.killProc(strconv.Itoa(f.pid))
			f.pid = 0
		}
	}
}

// setMain đặt /proc của tiến trình chính đang chạy của dịch vụ.
func (f *cageFake) setMain(status, mountinfo, exe string) {
	p := "/proc/" + strconv.Itoa(f.pid)
	f.SetFile(p+"/status", []byte(status), 0o444, system.Root)
	f.SetFile(p+"/cgroup", []byte("0::"+cageCgroupPath(f.l)+"/"+f.l.CageServiceUnit()+"\n"), 0o444, system.Root)
	f.SetFile(p+"/mountinfo", []byte(mountinfo), 0o444, system.Root)
	f.SetSymlink(exe, p+"/exe")
}

// setProc đặt một tiến trình bất kỳ trong /proc giả.
func (f *cageFake) setProc(pid, uid, state, cgroup string) {
	p := "/proc/" + pid
	f.SetFile(p+"/status", []byte(procStatus(uid, state, "0000000000000000", "0")), 0o444, system.Root)
	f.SetFile(p+"/cgroup", []byte(cgroup), 0o444, system.Root)
}

func (f *cageFake) killProc(pid string) {
	p := "/proc/" + pid
	for _, n := range []string{"status", "cgroup", "mountinfo", "exe"} {
		_ = f.FakeSystem.Remove(p + "/" + n)
	}
	_ = f.FakeSystem.Remove(p)
}

// whole là hệ thống file giả (trừ /proc: pid của dịch vụ đổi mỗi lần khởi
// động) cộng trạng thái unit, để so trước và sau Undo.
func (f *cageFake) whole() any {
	snap := f.Snapshot()
	maps.DeleteFunc(snap, func(p string, _ system.FakeEntry) bool { return strings.HasPrefix(p, "/proc") })
	// Unit không active, không enabled, không failed giống hệt unit chưa có.
	units := maps.Clone(f.units)
	maps.DeleteFunc(units, func(_ string, u cageUnit) bool { return u == cageUnit{} })
	return []any{snap, units}
}

// wholeDiff liệt kê đường dẫn và unit khác nhau giữa hai lần whole(), để lỗi
// của test đọc được thay vì in nguyên hệ thống file.
func wholeDiff(a, b any) string {
	x, y := a.([]any), b.([]any)
	sa, sb := x[0].(map[string]system.FakeEntry), y[0].(map[string]system.FakeEntry)
	var out []string
	for p, e := range sa {
		if o, ok := sb[p]; !ok {
			out = append(out, "mất "+p)
		} else if !reflect.DeepEqual(e, o) {
			out = append(out, "đổi "+p)
		}
	}
	for p := range sb {
		if _, ok := sa[p]; !ok {
			out = append(out, "thêm "+p)
		}
	}
	if !reflect.DeepEqual(x[1], y[1]) {
		out = append(out, "unit "+fmt.Sprint(x[1])+" thành "+fmt.Sprint(y[1]))
	}
	slices.Sort(out)
	return strings.Join(out, "; ")
}

// cageReadOnly: lệnh chỉ hỏi, được chạy khi engine Check một bước đã xong.
func cageReadOnly(c system.Cmd) bool {
	switch c.Name {
	case "getent", "id", "stat", "journalctl":
		return true
	case "systemctl":
		return c.Args[0] == "is-active" || c.Args[0] == "is-enabled" || c.Args[0] == "show"
	}
	return false
}

func cageExecute(t *testing.T, f *cageFake, s engine.Step) engine.Report {
	t.Helper()
	rep, err := engine.New(f, engine.Options{}).Execute(context.Background(),
		engine.Plan{Kind: "test", Target: "test", Steps: []engine.Step{s}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return rep
}

func shortCageWaits(t *testing.T) {
	t.Helper()
	sw, rw, tw := cageSocketWait, cageReapWait, cageWaitTick
	cageSocketWait, cageReapWait, cageWaitTick = 50*time.Millisecond, 50*time.Millisecond, time.Millisecond
	t.Cleanup(func() { cageSocketWait, cageReapWait, cageWaitTick = sw, rw, tw })
}

// cageLifecycle là bốn trường hợp chuẩn của skill tpan-step (lỗi có ngữ cảnh
// nằm ở test riêng của từng Step).
type cageLifecycle struct {
	name   string
	setup  func(f *cageFake)
	step   func(l site.Layout) engine.Step
	noUndo bool
}

func (lc cageLifecycle) run(t *testing.T) {
	ctx := context.Background()
	shortCageWaits(t)
	t.Run("máy sạch rồi Apply thì Check true", func(t *testing.T) {
		f := newCageFake(t)
		lc.setup(f)
		s := lc.step(f.l)
		if done, err := s.Check(ctx, f); err != nil || done {
			t.Fatalf("Check trước Apply = %v, %v; muốn false, nil", done, err)
		}
		if err := s.Apply(ctx, f); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if done, err := s.Check(ctx, f); err != nil || !done {
			t.Fatalf("Check sau Apply = %v, %v; muốn true, nil", done, err)
		}
	})
	t.Run("chạy lại thì không lệnh thay đổi nào chạy", func(t *testing.T) {
		f := newCageFake(t)
		lc.setup(f)
		if rep := cageExecute(t, f, lc.step(f.l)); rep.Job.Status != engine.JobSucceeded {
			t.Fatalf("lần đầu: %s %s", rep.Job.Status, rep.Job.Error)
		}
		before := f.whole()
		f.ResetRan()
		f.writes, f.removes = nil, nil
		rep := cageExecute(t, f, lc.step(f.l))
		if got := rep.Steps[0].Outcome; got != "skipped" {
			t.Fatalf("lần hai: outcome %q, muốn skipped (%s)", got, rep.Steps[0].Error)
		}
		for _, c := range f.Ran() {
			if !cageReadOnly(c) {
				t.Errorf("lần hai chạy lệnh thay đổi máy: %s", c.String())
			}
		}
		if len(f.writes)+len(f.removes) > 0 || !reflect.DeepEqual(before, f.whole()) {
			t.Errorf("lần hai làm đổi máy: ghi %v, xoá %v", f.writes, f.removes)
		}
	})
	if lc.noUndo {
		t.Run("NoUndo có lý do", func(t *testing.T) {
			if lc.step(cageLayout(t)).Info().NoUndo.IsZero() {
				t.Error("Undo không làm gì nhưng Info().NoUndo trống")
			}
		})
		return
	}
	t.Run("Apply rồi Undo thì máy về như cũ", func(t *testing.T) {
		f := newCageFake(t)
		lc.setup(f)
		before := f.whole()
		s := lc.step(f.l)
		if err := s.Apply(ctx, f); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if reflect.DeepEqual(before, f.whole()) {
			t.Fatal("Apply không đổi gì; test không kiểm được Undo")
		}
		if err := s.Undo(ctx, f); err != nil {
			t.Fatalf("Undo: %v", err)
		}
		if after := f.whole(); !reflect.DeepEqual(before, after) {
			t.Errorf("sau Undo máy khác trước Apply: %s", wholeDiff(before, after))
		}
	})
}

var cageLim = account.Limits{CPUPercent: 100, RAMMB: 512, Tasks: 64}

func cageFiles(l site.Layout) *CageFiles {
	return &CageFiles{Layout: l, PHP: "8.3", Limits: cageLim}
}

// applied đưa máy giả về trạng thái "lồng đã có file", không ghi lại lệnh nào.
func (f *cageFake) applied(t *testing.T) {
	t.Helper()
	if err := cageFiles(f.l).Apply(context.Background(), f); err != nil {
		t.Fatalf("dựng lồng có sẵn: %v", err)
	}
	f.ResetRan()
	f.writes, f.removes = nil, nil
}

// running: socket và dịch vụ của lồng đang chạy.
func (f *cageFake) running() {
	f.units[f.l.CageSocketUnit()] = cageUnit{Enabled: true}
	f.start(f.l.CageSocketUnit())
	f.start(f.l.CageServiceUnit())
	f.fenceAt = f.activeAt - 10
	f.ResetRan()
}

func cageRanHas(f *cageFake, name string, args ...string) bool {
	for _, c := range f.Ran() {
		if c.Name == name && len(c.Args) >= len(args) && slices.Equal(c.Args[:len(args)], args) {
			return true
		}
	}
	return false
}

// ranIndex trả vị trí của lệnh đầu tiên khớp, -1 khi không có.
func cageRanIndex(f *cageFake, name string, args ...string) int {
	for i, c := range f.Ran() {
		if c.Name == name && slices.Equal(c.Args, args) {
			return i
		}
	}
	return -1
}

// ================================================================ CageFiles

func TestCageFilesLifecycle(t *testing.T) {
	for _, lc := range []cageLifecycle{
		{name: "ghi trên máy sạch", setup: func(*cageFake) {}, step: func(l site.Layout) engine.Step { return cageFiles(l) }},
		{
			name: "ghi đè lồng cũ khác trần, dịch vụ đang ngủ",
			setup: func(f *cageFake) {
				if err := (&CageFiles{Layout: f.l, PHP: "8.3"}).Apply(context.Background(), f); err != nil {
					t.Fatal(err)
				}
			},
			step: func(l site.Layout) engine.Step { return cageFiles(l) },
		},
		{
			name:  "gỡ",
			setup: func(f *cageFake) { f.applied(t) },
			step:  func(l site.Layout) engine.Step { return &CageFiles{Layout: l, PHP: "8.3", Remove: true} },
		},
	} {
		t.Run(lc.name, lc.run)
	}
}

func TestCageFilesThuTuGhiVaQuyen(t *testing.T) {
	ctx := context.Background()
	f := newCageFake(t)
	l := f.l
	if err := cageFiles(l).Apply(ctx, f); err != nil {
		t.Fatal(err)
	}
	want := []string{l.CageSliceFile(), l.CageSocketFile(), l.CageServiceFile(), l.CageFPMConf(), l.CagePasswd(), l.CageGroup(), l.CageFence()}
	if !slices.Equal(f.writes, want) {
		t.Errorf("thứ tự ghi %v\nmuốn %v (hàng rào sau cùng)", f.writes, want)
	}
	for p, perm := range map[string]fs.FileMode{tpanEtc: 0o700, site.CageConfRoot: 0o700, l.CageConfDir(): 0o755, l.CageFenceDir(): 0o755} {
		fi, err := f.Stat(p)
		if err != nil || !fi.Mode.IsDir() || fi.Mode.Perm() != perm || normOwner(fi.Owner) != system.Root {
			t.Errorf("%s: %v %v, muốn thư mục %v root", p, fi, err, perm)
		}
	}
	for _, p := range l.CageFiles() {
		if ok, err := fileIs(f, p, nil); err != nil || !ok {
			t.Errorf("%s không phải file 0644 root:root", p)
		}
	}
	if b, _ := f.ReadFile(l.CagePasswd()); string(b) != cagePasswdOut(l) {
		t.Errorf("passwd = %q", b)
	}
	// Dịch vụ không chạy: có -t (file dịch vụ, php-fpm.conf mới) mà không try-restart.
	if !cageRanHas(f, "php-fpm8.3", "-t", "--fpm-config", l.CageFPMConf()) || cageRanHas(f, "systemctl", "try-restart") {
		t.Errorf("lệnh: %v", ranStrings(f.Ran()))
	}
}

func TestCageFilesTaoEtcTpanKhiChuaCo(t *testing.T) {
	ctx := context.Background()
	f := newCageFake(t)
	if err := f.FakeSystem.Remove(tpanEtc); err != nil {
		t.Fatal(err)
	}
	s := cageFiles(f.l)
	if err := s.Apply(ctx, f); err != nil {
		t.Fatal(err)
	}
	if fi, err := f.Stat(tpanEtc); err != nil || fi.Mode.Perm() != 0o700 {
		t.Fatalf("/etc/tpan: %v %v, muốn 0700", fi, err)
	}
	if err := s.Undo(ctx, f); err != nil {
		t.Fatal(err)
	}
	// /etc/tpan thuộc tpan setup: bước không bao giờ xoá nó, kể cả khi chính
	// bước đã tạo ra.
	if _, err := f.Stat(tpanEtc); err != nil {
		t.Errorf("Undo xoá /etc/tpan: %v", err)
	}
	if _, err := f.Stat(site.CageConfRoot); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Undo chưa gỡ %s: %v", site.CageConfRoot, err)
	}
}

func TestCageFilesKhongDoiQuyenEtcTpan(t *testing.T) {
	f := newCageFake(t)
	f.SetDir(tpanEtc, 0o750, system.Root)
	if err := cageFiles(f.l).Apply(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	if fi, _ := f.Stat(tpanEtc); fi.Mode.Perm() != 0o750 {
		t.Errorf("/etc/tpan bị đổi quyền thành %v", fi.Mode.Perm())
	}
}

func TestCageFilesChiDoiSlice(t *testing.T) {
	f := newCageFake(t)
	f.applied(t)
	f.running()
	lim := cageLim
	lim.CPUPercent = 50
	s := &CageFiles{Layout: f.l, PHP: "8.3", Limits: lim}
	if err := s.Apply(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.writes, []string{f.l.CageSliceFile()}) {
		t.Errorf("ghi %v, muốn chỉ file slice", f.writes)
	}
	if !cageRanHas(f, "systemctl", "daemon-reload") || cageRanHas(f, "php-fpm8.3") || cageRanHas(f, "systemctl", "try-restart") {
		t.Errorf("chỉ đổi slice: muốn daemon-reload, không -t, không try-restart; lệnh %v", ranStrings(f.Ran()))
	}
	// Undo chỉ ghi lại đúng file đã đổi: ghi lại hàng rào (mtime mới) thì
	// cage.runtime tưởng hàng rào mới hơn dịch vụ và đòi khởi động lại oan.
	f.writes = nil
	if err := s.Undo(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.writes, []string{f.l.CageSliceFile()}) {
		t.Errorf("Undo ghi %v, muốn chỉ file slice", f.writes)
	}
}

// Apply đã try-restart dịch vụ với cấu hình mới (đổi số worker, đổi bản PHP);
// Undo phải restart để bản cũ chạy lại: `start` không làm gì với dịch vụ đang
// chạy, dịch vụ sẽ chạy mãi cấu hình mới trong khi file đã về bản cũ.
func TestCageFilesUndoKhoiDongLaiBanCu(t *testing.T) {
	ctx := context.Background()
	shortCageWaits(t)
	lim := cageLim
	lim.PHPWorkers = 8
	f := newCageFake(t)
	f.applied(t)
	f.running()
	before := f.whole()
	s := &CageFiles{Layout: f.l, PHP: "8.3", Limits: lim}
	if err := s.Apply(ctx, f); err != nil {
		t.Fatal(err)
	}
	newPID := f.pid
	f.ResetRan()
	if err := s.Undo(ctx, f); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if cageRanIndex(f, "systemctl", "restart", f.l.CageServiceUnit()) < 0 {
		t.Fatalf("Undo phải restart dịch vụ đang chạy cấu hình mới; lệnh %v", ranStrings(f.Ran()))
	}
	if f.pid == newPID || !f.units[f.l.CageServiceUnit()].Active {
		t.Error("dịch vụ không được khởi động lại với file cũ")
	}
	if !reflect.DeepEqual(before, f.whole()) {
		t.Errorf("sau Undo máy khác trước: %s", wholeDiff(before, f.whole()))
	}
}

// Apply không khởi động lại gì (chỉ đổi slice): Undo không restart dịch vụ.
func TestCageFilesUndoKhongRestartKhiKhongCan(t *testing.T) {
	ctx := context.Background()
	shortCageWaits(t)
	lim := cageLim
	lim.RAMMB = 384
	f := newCageFake(t)
	f.applied(t)
	f.running()
	s := &CageFiles{Layout: f.l, PHP: "8.3", Limits: lim}
	if err := s.Apply(ctx, f); err != nil {
		t.Fatal(err)
	}
	f.ResetRan()
	if err := s.Undo(ctx, f); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if cageRanHas(f, "systemctl", "restart") {
		t.Fatalf("chỉ đổi slice mà Undo vẫn restart dịch vụ: %v", ranStrings(f.Ran()))
	}
}

func TestCageFilesDoiFPMConf(t *testing.T) {
	lim := cageLim
	lim.PHPWorkers = 8
	t.Run("dịch vụ đang chạy: -t trong lồng rồi try-restart", func(t *testing.T) {
		f := newCageFake(t)
		f.applied(t)
		f.running()
		oldPID := f.pid
		if err := (&CageFiles{Layout: f.l, PHP: "8.3", Limits: lim}).Apply(context.Background(), f); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(f.writes, []string{f.l.CageFPMConf()}) {
			t.Errorf("ghi %v, muốn chỉ php-fpm.conf", f.writes)
		}
		test := cageRanIndex(f, "php-fpm8.3", "-t", "--fpm-config", f.l.CageFPMConf())
		restart := cageRanIndex(f, "systemctl", "try-restart", f.l.CageServiceUnit())
		if test < 0 || restart < test {
			t.Fatalf("muốn -t rồi try-restart; lệnh %v", ranStrings(f.Ran()))
		}
		if u := f.Ran()[test].AsUser; u != f.l.User {
			t.Errorf("-t chạy bằng %q, muốn %q (trong lồng)", u, f.l.User)
		}
		if f.pid == oldPID {
			t.Error("dịch vụ không được khởi động lại")
		}
	})
	t.Run("dịch vụ đang ngủ: -t, không try-restart", func(t *testing.T) {
		f := newCageFake(t)
		f.applied(t)
		if err := (&CageFiles{Layout: f.l, PHP: "8.3", Limits: lim}).Apply(context.Background(), f); err != nil {
			t.Fatal(err)
		}
		if !cageRanHas(f, "php-fpm8.3", "-t") || cageRanHas(f, "systemctl", "try-restart") {
			t.Errorf("lệnh %v", ranStrings(f.Ran()))
		}
	})
	t.Run("-t hỏng: mọi file về như cũ, chưa try-restart", func(t *testing.T) {
		f := newCageFake(t)
		f.applied(t)
		f.running()
		before := f.whole()
		f.fpmFail = true
		err := (&CageFiles{Layout: f.l, PHP: "8.3", Limits: lim}).Apply(context.Background(), f)
		if err == nil || !strings.Contains(err.Error(), "pm.max_children") || !strings.Contains(err.Error(), f.l.Domain.String()) {
			t.Fatalf("Apply = %v, muốn lỗi kèm output của -t và domain", err)
		}
		if cageRanHas(f, "systemctl", "try-restart") {
			t.Errorf("-t hỏng mà vẫn try-restart: %v", ranStrings(f.Ran()))
		}
		// Chỉ php-fpm.conf bị ghi rồi trả lại; hàng rào không bị ghi lại.
		if !slices.Equal(f.writes, []string{f.l.CageFPMConf(), f.l.CageFPMConf()}) {
			t.Errorf("ghi %v, muốn php-fpm.conf mới rồi bản cũ", f.writes)
		}
		if !reflect.DeepEqual(before, f.whole()) {
			t.Error("-t hỏng mà file chưa về như cũ")
		}
	})
	t.Run("-t hỏng trên máy sạch: không còn file hay thư mục nào", func(t *testing.T) {
		f := newCageFake(t)
		before := f.whole()
		f.fpmFail = true
		if err := cageFiles(f.l).Apply(context.Background(), f); err == nil {
			t.Fatal("Apply không lỗi")
		}
		if !reflect.DeepEqual(before, f.whole()) {
			t.Errorf("còn sót: %s", wholeDiff(before, f.whole()))
		}
	})
	t.Run("try-restart hỏng: bước vẫn thành công", func(t *testing.T) {
		f := newCageFake(t)
		f.applied(t)
		f.running()
		f.restartFail = true
		s := &CageFiles{Layout: f.l, PHP: "8.3", Limits: lim}
		if err := s.Apply(context.Background(), f); err != nil {
			t.Fatalf("Apply = %v, muốn nil (try-restart hỏng không làm bước lỗi)", err)
		}
		if !cageRanHas(f, "systemctl", "try-restart") {
			t.Error("không có try-restart")
		}
		if done, err := s.Check(context.Background(), f); err != nil || !done {
			t.Errorf("Check sau Apply = %v, %v", done, err)
		}
	})
}

func TestCageFilesLoiGhiFileThu4(t *testing.T) {
	f := newCageFake(t)
	l := f.l
	// Có sẵn một slice cũ khác nội dung: phải được trả về đúng bản cũ.
	f.SetFile(l.CageSliceFile(), []byte("[Slice]\n# bản cũ\n"), 0o644, system.Root)
	before := f.whole()
	boom := errors.New("đĩa đầy")
	f.Fail("write", l.CageFPMConf(), boom)
	err := cageFiles(l).Apply(context.Background(), f)
	if !errors.Is(err, boom) || !strings.Contains(err.Error(), l.CageFPMConf()) {
		t.Fatalf("Apply = %v, muốn lỗi kèm đường dẫn file thứ 4", err)
	}
	if !slices.Equal(f.writes[:3], []string{l.CageSliceFile(), l.CageSocketFile(), l.CageServiceFile()}) {
		t.Errorf("ghi %v", f.writes)
	}
	if !reflect.DeepEqual(before, f.whole()) {
		t.Errorf("3 file trước chưa về như cũ: %s", wholeDiff(before, f.whole()))
	}
	if cageRanHas(f, "php-fpm8.3") {
		t.Error("ghi hỏng mà vẫn chạy -t")
	}
}

func TestCageFilesUndoSauKhiFailed(t *testing.T) {
	ctx := context.Background()
	shortCageWaits(t)
	lim := cageLim
	lim.PHPWorkers = 8
	setup := func(t *testing.T) (*cageFake, *CageFiles, any) {
		f := newCageFake(t)
		f.applied(t)
		f.running()
		before := f.whole()
		s := &CageFiles{Layout: f.l, PHP: "8.3", Limits: lim}
		if err := s.Apply(ctx, f); err != nil {
			t.Fatal(err)
		}
		// Trần mới làm dịch vụ chết liên tục: cả dịch vụ và socket vào failed,
		// file socket bị gỡ (t14 P1).
		f.stop(f.l.CageServiceUnit())
		f.stop(f.l.CageSocketUnit())
		for _, u := range []string{f.l.CageServiceUnit(), f.l.CageSocketUnit()} {
			x := f.units[u]
			x.Failed = true
			f.units[u] = x
		}
		f.ResetRan()
		return f, s, before
	}
	t.Run("reset-failed, start socket, restart dịch vụ", func(t *testing.T) {
		f, s, before := setup(t)
		if err := s.Undo(ctx, f); err != nil {
			t.Fatalf("Undo: %v", err)
		}
		l := f.l
		dr := cageRanIndex(f, "systemctl", "daemon-reload")
		rf := cageRanIndex(f, "systemctl", "reset-failed", l.CageServiceUnit(), l.CageSocketUnit())
		ss := cageRanIndex(f, "systemctl", "start", l.CageSocketUnit())
		// Apply đã try-restart dịch vụ với php-fpm.conf mới, nên đường trả lại
		// restart (start không làm gì với dịch vụ đang chạy).
		sv := cageRanIndex(f, "systemctl", "restart", l.CageServiceUnit())
		if dr < 0 || rf < dr || ss < rf || sv < ss {
			t.Fatalf("muốn daemon-reload, reset-failed, start socket, restart dịch vụ theo thứ tự; lệnh %v", ranStrings(f.Ran()))
		}
		if !isSocket(f, l.CageSocket()) || !f.units[l.CageServiceUnit()].Active {
			t.Error("socket hay dịch vụ chưa chạy lại")
		}
		if !reflect.DeepEqual(before, f.whole()) {
			t.Errorf("sau Undo máy khác trước: %s", wholeDiff(before, f.whole()))
		}
	})
	t.Run("không bật lại được socket thì Undo lỗi", func(t *testing.T) {
		f, s, _ := setup(t)
		f.noSocket = true
		err := s.Undo(ctx, f)
		if err == nil || !strings.Contains(err.Error(), f.l.CageSocket()) {
			t.Fatalf("Undo = %v, muốn lỗi nói socket chưa có", err)
		}
	})
}

func TestCageFilesGoVaUndo(t *testing.T) {
	ctx := context.Background()
	f := newCageFake(t)
	f.applied(t)
	// Một site khác cũng có lồng: /etc/tpan/cage không được gỡ.
	other := site.CageConfRoot + "/khac"
	f.SetDir(other, 0o755, system.Root)
	before := f.whole()
	s := &CageFiles{Layout: f.l, PHP: "8.3", Remove: true}
	if err := s.Apply(ctx, f); err != nil {
		t.Fatal(err)
	}
	l := f.l
	if len(f.removes) == 0 || f.removes[0] != l.CageFence() {
		t.Errorf("thứ tự gỡ %v, muốn hàng rào trước tiên", f.removes)
	}
	for _, p := range append(l.CageFiles(), l.CageConfDir(), l.CageFenceDir()) {
		if _, err := f.Stat(p); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s còn sau khi gỡ", p)
		}
	}
	if _, err := f.Stat(other); err != nil {
		t.Errorf("gỡ nhầm thư mục của site khác: %v", err)
	}
	if !cageRanHas(f, "systemctl", "stop", l.CageRunPrefix()+"*") || !cageRanHas(f, "systemctl", "reset-failed", "tpan-cage-"+l.Slug.String()+"-*") {
		t.Errorf("thiếu dọn unit tạm: %v", ranStrings(f.Ran()))
	}
	if cageRanHas(f, "getent") {
		t.Error("gỡ mà vẫn gọi getent (user có thể đã bị xoá)")
	}
	if err := s.Undo(ctx, f); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, f.whole()) {
		t.Errorf("Undo chưa trả đủ: %s", wholeDiff(before, f.whole()))
	}
	if n := len(f.writes); n == 0 || f.writes[n-1] != l.CageFence() {
		t.Errorf("Undo của gỡ ghi %v, muốn hàng rào sau cùng", f.writes)
	}

	t.Run("lồng cuối cùng: gỡ cả /etc/tpan/cage, không đụng /etc/tpan", func(t *testing.T) {
		f := newCageFake(t)
		f.applied(t)
		if err := (&CageFiles{Layout: f.l, PHP: "8.3", Remove: true}).Apply(ctx, f); err != nil {
			t.Fatal(err)
		}
		if _, err := f.Stat(site.CageConfRoot); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s còn: %v", site.CageConfRoot, err)
		}
		if _, err := f.Stat(tpanEtc); err != nil {
			t.Errorf("/etc/tpan bị gỡ: %v", err)
		}
	})
}

func TestCageFilesGetentLa(t *testing.T) {
	l := cageLayout(t)
	pw := strings.Split(strings.TrimSuffix(cagePasswdOut(l), "\n"), "\n")
	cases := []struct {
		name, passwd, group string
		missing             bool
	}{
		{name: "thiếu một dòng", passwd: strings.Join(pw[:3], "\n") + "\n"},
		{name: "thừa một dòng", passwd: cagePasswdOut(l) + "evil:x:0:0::/:/bin/sh\n"},
		{name: "sai thứ tự", passwd: strings.Join([]string{pw[1], pw[0], pw[2], pw[3]}, "\n") + "\n"},
		{name: "thiếu trường", passwd: strings.Replace(cagePasswdOut(l), ":/usr/sbin/nologin\nnobody", "\nnobody", 1)},
		{name: "thừa trường", passwd: strings.Replace(cagePasswdOut(l), "root:x:0:0:", "root:x:0:0:x:", 1)},
		{name: "tên khác", passwd: strings.Replace(cagePasswdOut(l), "nobody:", "nobodyx:", 1)},
		{name: "ký tự điều khiển", passwd: strings.Replace(cagePasswdOut(l), "root:/root", "root\x1b:/root", 1)},
		{name: "group sai dạng", group: "root:x:0\nwww-data:x:33:\n" + l.User + ":x:1001:\nnogroup:x:65534:\n"},
		{name: "getent không thấy tên", missing: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newCageFake(t)
			f.passwd, f.group = c.passwd, c.group
			if c.missing {
				f.Handle([]string{"getent", "passwd"}, func(system.Cmd) system.Result { return system.Result{ExitCode: 2} })
			}
			before := f.whole()
			s := cageFiles(f.l)
			if _, err := s.Check(context.Background(), f); err == nil {
				t.Error("Check không lỗi")
			}
			err := s.Apply(context.Background(), f)
			if err == nil || !strings.Contains(err.Error(), "getent") && !strings.Contains(err.Error(), "passwd") {
				t.Fatalf("Apply = %v, muốn lỗi nói về getent", err)
			}
			if len(f.writes) > 0 || !reflect.DeepEqual(before, f.whole()) {
				t.Errorf("getent lạ mà vẫn ghi: %v", f.writes)
			}
		})
	}
}

func TestCageFilesWantVaInfo(t *testing.T) {
	l := cageLayout(t)
	s := cageFiles(l)
	tmpl := map[string]func() ([]byte, error){
		l.CageSliceFile():   func() ([]byte, error) { return templates.CageSlice(l, cageLim) },
		l.CageSocketFile():  func() ([]byte, error) { return templates.CageSocketUnit(l) },
		l.CageServiceFile(): func() ([]byte, error) { return templates.CageServiceUnit(l, "8.3") },
		l.CageFence():       func() ([]byte, error) { return templates.CageFence(l) },
		l.CageFPMConf():     func() ([]byte, error) { return templates.CageFPMConf(l, cageLim.Workers()) },
	}
	for p, gen := range tmpl {
		want, err := gen()
		if err != nil {
			t.Fatal(err)
		}
		got, ok := s.Want(p)
		if !ok || string(got) != string(want) {
			t.Errorf("Want(%s) = %v, khác template", p, ok)
		}
	}
	for _, p := range []string{l.CagePasswd(), l.CageGroup(), "/etc/passwd", l.CageFenceDir()} {
		if _, ok := s.Want(p); ok {
			t.Errorf("Want(%s) ok, muốn false", p)
		}
	}

	info := s.Info()
	var paths []string
	for _, fc := range info.Files {
		paths = append(paths, fc.Path)
		if want, _ := s.Want(fc.Path); fc.Delete || string(fc.New) != string(want) {
			t.Errorf("Info().Files %s: nội dung khác Want", fc.Path)
		}
	}
	if !slices.Equal(paths, l.CageManagedFiles()) {
		t.Errorf("Info().Files = %v, muốn đúng năm file %v", paths, l.CageManagedFiles())
	}
	rm := (&CageFiles{Layout: l, PHP: "8.3", Remove: true}).Info()
	paths = nil
	for _, fc := range rm.Files {
		paths = append(paths, fc.Path)
		if !fc.Delete || fc.New != nil {
			t.Errorf("Info().Files khi gỡ %s: muốn Delete", fc.Path)
		}
	}
	if !slices.Equal(paths, l.CageManagedFiles()) || !rm.Danger {
		t.Errorf("Info khi gỡ: Files %v, Danger %v", paths, rm.Danger)
	}
}

// ============================================================ Info chung, tham số

func TestCageInfo(t *testing.T) {
	l := cageLayout(t)
	all := []engine.Step{
		cageFiles(l), &CageFiles{Layout: l, Remove: true},
		&CageSocket{Layout: l, PHP: "8.3"}, &CageSocket{Layout: l, Disable: true},
		&CageReap{Layout: l}, &CageRestart{Layout: l},
	}
	for _, s := range all {
		info := s.Info()
		if !strings.HasPrefix(info.ID, "cage.") || !strings.HasSuffix(info.ID, ":"+l.Domain.String()) {
			t.Errorf("ID %q", info.ID)
		}
		for _, txt := range []string{info.Title.Vi, info.Title.En, info.Why.Vi, info.Why.En} {
			if txt == "" || strings.Contains(txt, "chưa viết") || strings.Contains(txt, "not written") {
				t.Errorf("%s: Title hay Why trống hoặc còn chữ chưa viết: %q", info.ID, txt)
			}
		}
		if len(info.Commands) == 0 {
			t.Errorf("%s: không khai lệnh", info.ID)
		}
	}
	if !all[3].Info().Danger || !all[4].Info().Danger || all[2].Info().Danger {
		t.Error("Danger sai: tắt lồng và giết tiến trình phải Danger, bật socket thì không")
	}
}

func TestCageThamSoSai(t *testing.T) {
	ctx := context.Background()
	good := cageLayout(t)
	bad := good
	bad.User = "root"
	layouts := map[string]site.Layout{"Layout zero": {}, "user không phải tp_<slug>": bad}
	for name, l := range layouts {
		for _, s := range []engine.Step{
			&CageFiles{Layout: l, PHP: "8.3"}, &CageFiles{Layout: l, Remove: true},
			&CageSocket{Layout: l, PHP: "8.3"}, &CageSocket{Layout: l, Disable: true},
			&CageReap{Layout: l}, &CageRestart{Layout: l},
		} {
			f := newCageFake(t)
			if _, err := s.Check(ctx, f); err == nil {
				t.Errorf("%s, %s: Check không lỗi", name, s.Info().ID)
			}
			if err := s.Apply(ctx, f); err == nil {
				t.Errorf("%s, %s: Apply không lỗi", name, s.Info().ID)
			}
			if len(f.Ran()) > 0 || len(f.writes) > 0 {
				t.Errorf("%s, %s: tham số sai mà vẫn chạy %v", name, s.Info().ID, ranStrings(f.Ran()))
			}
		}
	}
	for _, php := range []string{"", "8.3; id", "$(id)", "../8.3"} {
		f := newCageFake(t)
		for _, s := range []engine.Step{&CageFiles{Layout: good, PHP: php}, &CageSocket{Layout: good, PHP: php}} {
			if err := s.Apply(ctx, f); err == nil {
				t.Errorf("PHP %q: %s Apply không lỗi", php, s.Info().ID)
			}
		}
		if len(f.Ran()) > 0 {
			t.Errorf("PHP %q: vẫn chạy %v", php, ranStrings(f.Ran()))
		}
	}
}

// =============================================================== CageSocket

func TestCageSocketLifecycle(t *testing.T) {
	for _, lc := range []cageLifecycle{
		{name: "bật", setup: func(f *cageFake) { f.applied(t) }, step: func(l site.Layout) engine.Step { return &CageSocket{Layout: l, PHP: "8.3"} }},
		{
			name:  "bật khi socket đã enabled mà không chạy",
			setup: func(f *cageFake) { f.applied(t); f.units[f.l.CageSocketUnit()] = cageUnit{Enabled: true} },
			step:  func(l site.Layout) engine.Step { return &CageSocket{Layout: l, PHP: "8.3"} },
		},
		{
			name:  "tắt",
			setup: func(f *cageFake) { f.applied(t); f.running() },
			step:  func(l site.Layout) engine.Step { return &CageSocket{Layout: l, Disable: true} },
		},
	} {
		t.Run(lc.name, lc.run)
	}
}

func TestCageSocketBat(t *testing.T) {
	ctx := context.Background()
	shortCageWaits(t)
	t.Run("-t trong lồng, reset-failed trước enable, không start dịch vụ", func(t *testing.T) {
		f := newCageFake(t)
		f.applied(t)
		l := f.l
		if err := (&CageSocket{Layout: l, PHP: "8.3"}).Apply(ctx, f); err != nil {
			t.Fatal(err)
		}
		test := cageRanIndex(f, "php-fpm8.3", "-t", "--fpm-config", l.CageFPMConf())
		reset := cageRanIndex(f, "systemctl", "reset-failed", l.CageSocketUnit(), l.CageServiceUnit())
		enable := cageRanIndex(f, "systemctl", "enable", "--now", l.CageSocketUnit())
		if test < 0 || reset < test || enable < reset {
			t.Fatalf("muốn -t, reset-failed, enable --now theo thứ tự; lệnh %v", ranStrings(f.Ran()))
		}
		if f.Ran()[test].AsUser != l.User {
			t.Errorf("-t chạy bằng %q", f.Ran()[test].AsUser)
		}
		if cageRanHas(f, "systemctl", "start", l.CageServiceUnit()) || f.units[l.CageServiceUnit()].Active {
			t.Error("bật socket mà khởi động cả dịch vụ")
		}
	})
	t.Run("socket và dịch vụ đang failed: reset-failed rồi bật được", func(t *testing.T) {
		f := newCageFake(t)
		f.applied(t)
		f.units[f.l.CageSocketUnit()] = cageUnit{Enabled: true, Failed: true}
		f.units[f.l.CageServiceUnit()] = cageUnit{Failed: true}
		s := &CageSocket{Layout: f.l, PHP: "8.3"}
		if err := s.Apply(ctx, f); err != nil {
			t.Fatal(err)
		}
		if done, err := s.Check(ctx, f); err != nil || !done {
			t.Errorf("Check sau Apply = %v, %v", done, err)
		}
		if f.units[f.l.CageServiceUnit()].Failed {
			t.Error("dịch vụ vẫn failed")
		}
	})
	t.Run("-t hỏng: không enable gì", func(t *testing.T) {
		f := newCageFake(t)
		f.applied(t)
		f.fpmFail = true
		err := (&CageSocket{Layout: f.l, PHP: "8.3"}).Apply(ctx, f)
		if err == nil || !strings.Contains(err.Error(), f.l.Domain.String()) {
			t.Fatalf("Apply = %v", err)
		}
		if cageRanHas(f, "systemctl", "enable") {
			t.Error("-t hỏng mà vẫn enable")
		}
	})
	for name, prep := range map[string]func(f *cageFake){
		"enable hỏng":            func(f *cageFake) { f.enableFail = true },
		"socket không xuất hiện": func(f *cageFake) { f.noSocket = true },
	} {
		t.Run(name+": lỗi kèm journal đã làm sạch, máy về như cũ", func(t *testing.T) {
			f := newCageFake(t)
			f.applied(t)
			before := f.whole()
			prep(f)
			err := (&CageSocket{Layout: f.l, PHP: "8.3"}).Apply(ctx, f)
			if err == nil {
				t.Fatal("Apply không lỗi")
			}
			msg := err.Error()
			for _, want := range []string{f.l.Domain.String(), "output của site", "[output của site] Fatal error in /var/www/x.php", "WARNING"} {
				if !strings.Contains(msg, want) {
					t.Errorf("lỗi thiếu %q:\n%s", want, msg)
				}
			}
			if strings.ContainsAny(msg, "\x1b\r\u202e") {
				t.Errorf("lỗi còn ký tự điều khiển: %q", msg)
			}
			if !cageRanHas(f, "journalctl", "-u", f.l.CageServiceUnit(), "-u", f.l.CageSocketUnit(), "--no-pager", "-o", "cat") {
				t.Errorf("không đọc journal: %v", ranStrings(f.Ran()))
			}
			if !reflect.DeepEqual(before, f.whole()) {
				t.Errorf("bật hỏng mà máy chưa về như cũ: %s", wholeDiff(before, f.whole()))
			}
		})
	}
}

func TestCageSocketTat(t *testing.T) {
	ctx := context.Background()
	shortCageWaits(t)
	f := newCageFake(t)
	f.applied(t)
	f.running()
	l := f.l
	if err := (&CageSocket{Layout: l, Disable: true}).Apply(ctx, f); err != nil {
		t.Fatal(err)
	}
	d := cageRanIndex(f, "systemctl", "disable", "--now", l.CageSocketUnit())
	sv := cageRanIndex(f, "systemctl", "stop", l.CageServiceUnit())
	sl := cageRanIndex(f, "systemctl", "stop", l.CageSlice())
	if d < 0 || sv < d || sl < sv {
		t.Fatalf("muốn disable --now socket, stop dịch vụ, stop slice; lệnh %v", ranStrings(f.Ran()))
	}
	if _, err := f.Stat(l.CageRunDir()); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("thư mục socket rỗng chưa được gỡ: %v", err)
	}

	t.Run("thư mục socket còn đồ thì để nguyên", func(t *testing.T) {
		f := newCageFake(t)
		f.applied(t)
		f.running()
		f.SetFile(f.l.CageRunDir()+"/khac", nil, 0o644, system.Root)
		if err := (&CageSocket{Layout: f.l, Disable: true}).Apply(ctx, f); err != nil {
			t.Fatal(err)
		}
		if _, err := f.Stat(f.l.CageRunDir()); err != nil {
			t.Errorf("gỡ thư mục còn đồ: %v", err)
		}
	})
	t.Run("stop dịch vụ hỏng: lỗi có ngữ cảnh, socket bật lại", func(t *testing.T) {
		f := newCageFake(t)
		f.applied(t)
		f.running()
		f.Handle([]string{"systemctl", "stop", f.l.CageServiceUnit()}, func(system.Cmd) system.Result {
			return system.Result{ExitCode: 1, Stderr: "Job failed\n"}
		})
		err := (&CageSocket{Layout: f.l, Disable: true}).Apply(ctx, f)
		if err == nil || !strings.Contains(err.Error(), f.l.CageServiceUnit()) || !strings.Contains(err.Error(), f.l.Domain.String()) {
			t.Fatalf("Apply = %v", err)
		}
		if !isSocket(f, f.l.CageSocket()) {
			t.Error("socket chưa được bật lại sau khi tắt hỏng")
		}
	})
}

// ================================================================= CageReap

// reapProcs đặt /proc giả: tiến trình của site trong và ngoài slice, của user
// khác, zombie, và tiến trình biến mất giữa chừng.
func reapProcs(f *cageFake) {
	in := "0::" + cageCgroupPath(f.l) + "/" + f.l.CageServiceUnit() + "\n"
	f.setProc("100", siteUID, "S (sleeping)", in)
	f.setProc("101", siteUID, "S (sleeping)", "0::"+cageCgroupPath(f.l)+"\n")
	f.setProc("200", siteUID, "S (sleeping)", "0::/system.slice/php8.3-fpm.service\n")
	f.setProc("201", siteUID, "R (running)", "0::/user.slice/user-0.slice/session-1.scope\n")
	f.setProc("202", siteUID, "S (sleeping)", "0::/tpan.slice/tpan-khac.slice/tpan-cage-khac-php.service\n")
	f.setProc("300", "1002", "S (sleeping)", "0::/system.slice/php8.3-fpm.service\n")
	f.setProc("400", siteUID, "Z (zombie)", "0::/system.slice/php8.3-fpm.service\n")
	// 500: thư mục còn, status đã mất (tiến trình thoát giữa ReadDir và ReadFile).
	f.SetDir("/proc/500", 0o555, system.Root)
	// 501: status còn, cgroup đã mất.
	f.SetFile("/proc/501/status", []byte(procStatus(siteUID, "S (sleeping)", "0", "0")), 0o444, system.Root)
}

func TestCageReapLifecycle(t *testing.T) {
	cageLifecycle{
		name:   "CageReap",
		setup:  reapProcs,
		step:   func(l site.Layout) engine.Step { return &CageReap{Layout: l} },
		noUndo: true,
	}.run(t)
}

func TestCageReapChiGietNgoaiSlice(t *testing.T) {
	ctx := context.Background()
	shortCageWaits(t)
	f := newCageFake(t)
	reapProcs(f)
	if err := (&CageReap{Layout: f.l}).Apply(ctx, f); err != nil {
		t.Fatal(err)
	}
	var kills []string
	for _, c := range f.Ran() {
		if c.Name == "kill" {
			kills = append(kills, c.String())
		}
	}
	if !slices.Equal(kills, []string{"kill -KILL 200 201 202"}) {
		t.Errorf("kill: %v, muốn chỉ ba tiến trình của site ngoài slice", kills)
	}
	for _, pid := range []string{"100", "101", "300", "400"} {
		if _, err := f.Stat("/proc/" + pid + "/status"); err != nil {
			t.Errorf("tiến trình %s bị giết nhầm", pid)
		}
	}
	if !cageRanHas(f, "id", "-u", f.l.User) {
		t.Error("không lấy uid bằng id -u")
	}

	t.Run("kill thoát 1 vì một pid vừa chết: vẫn thành công", func(t *testing.T) {
		f := newCageFake(t)
		reapProcs(f)
		f.killExit = 1
		if err := (&CageReap{Layout: f.l}).Apply(ctx, f); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("tiến trình không chết: lỗi nêu pid", func(t *testing.T) {
		f := newCageFake(t)
		reapProcs(f)
		f.Handle([]string{"kill", "-KILL"}, func(system.Cmd) system.Result { return system.Result{} })
		err := (&CageReap{Layout: f.l}).Apply(ctx, f)
		if err == nil || !strings.Contains(err.Error(), "200") || !strings.Contains(err.Error(), f.l.Domain.String()) {
			t.Fatalf("Apply = %v", err)
		}
	})
	t.Run("id lỗi hay uid 0: lỗi, không giết gì", func(t *testing.T) {
		for _, uid := range []string{"0", "abc", ""} {
			f := newCageFake(t)
			reapProcs(f)
			f.uid = uid
			if err := (&CageReap{Layout: f.l}).Apply(ctx, f); err == nil || !strings.Contains(err.Error(), "uid") {
				t.Errorf("uid %q: Apply = %v", uid, err)
			}
			if cageRanHas(f, "kill") {
				t.Errorf("uid %q: vẫn kill", uid)
			}
		}
		f := newCageFake(t)
		f.Handle([]string{"id", "-u"}, func(system.Cmd) system.Result {
			return system.Result{ExitCode: 1, Stderr: "id: 'tp_x': no such user\n"}
		})
		if _, err := (&CageReap{Layout: f.l}).Check(ctx, f); err == nil || !strings.Contains(err.Error(), "no such user") {
			t.Errorf("Check = %v, muốn lỗi kèm stderr của id", err)
		}
	})
}

func TestCgroupUnder(t *testing.T) {
	cg := "/tpan.slice/tpan-a.slice"
	for in, want := range map[string]bool{
		"0::/tpan.slice/tpan-a.slice\n":                           true,
		"0::/tpan.slice/tpan-a.slice/tpan-cage-a-php.service\n":   true,
		"0::/tpan.slice/tpan-ab.slice/tpan-cage-ab-php.service\n": false,
		"0::/tpan.slice/tpan-a.slicex\n":                          false,
		"0::/system.slice/php8.3-fpm.service\n":                   false,
		"1:name=systemd:/tpan.slice/tpan-a.slice\n":               false,
		"": false,
	} {
		if got := cgroupUnder([]byte(in), cg); got != want {
			t.Errorf("cgroupUnder(%q) = %v, muốn %v", in, got, want)
		}
	}
}

// ============================================================== CageRestart

func TestCageRestartCheck(t *testing.T) {
	ctx := context.Background()
	good := procStatus(siteUID, "S (sleeping)", "0000000000000000", "1")
	cases := []struct {
		name string
		prep func(f *cageFake)
		want bool
	}{
		{"lồng chạy đúng", func(*cageFake) {}, true},
		{"dịch vụ đang ngủ", func(f *cageFake) { f.stop(f.l.CageServiceUnit()) }, true},
		{"NoNewPrivs 0", func(f *cageFake) {
			f.setMain(strings.Replace(good, "NoNewPrivs:\t1", "NoNewPrivs:\t0", 1), goodMountinfo, "/usr/sbin/php-fpm8.3")
		}, false},
		{"CapEff khác 0", func(f *cageFake) {
			f.setMain(strings.Replace(good, "0000000000000000", "00000000000000c0", 1), goodMountinfo, "/usr/sbin/php-fpm8.3")
		}, false},
		{"tiến trình chính ngoài slice", func(f *cageFake) {
			f.SetFile("/proc/"+strconv.Itoa(f.pid)+"/cgroup", []byte("0::/system.slice/x.service\n"), 0o444, system.Root)
		}, false},
		{"không có tmpfs ở /etc/tpan", func(f *cageFake) {
			f.setMain(good, strings.Replace(goodMountinfo, "4 1 0:32 / /etc/tpan rw - tmpfs tmpfs rw\n", "", 1), "/usr/sbin/php-fpm8.3")
		}, false},
		{"/var gắn chồng bằng ext4", func(f *cageFake) {
			f.setMain(good, goodMountinfo+"9 1 8:1 /x /var rw - ext4 /dev/sda1 rw\n", "/usr/sbin/php-fpm8.3")
		}, false},
		{"MariaDB chạy mà lồng không thấy /run/mysqld", func(f *cageFake) {
			f.mariadb = true
			f.setMain(good, strings.Replace(goodMountinfo, "5 3 0:25 /mysqld /run/mysqld rw shared:9 - tmpfs tmpfs rw\n", "", 1), "/usr/sbin/php-fpm8.3")
		}, false},
		{"MariaDB không chạy và không có /run/mysqld", func(f *cageFake) {
			f.setMain(good, strings.Replace(goodMountinfo, "5 3 0:25 /mysqld /run/mysqld rw shared:9 - tmpfs tmpfs rw\n", "", 1), "/usr/sbin/php-fpm8.3")
		}, true},
		{"/run/mysqld đã bị xoá", func(f *cageFake) {
			f.mariadb = true
			f.setMain(good, strings.Replace(goodMountinfo, "/mysqld /run/mysqld", "/mysqld//deleted /run/mysqld", 1), "/usr/sbin/php-fpm8.3")
		}, false},
		{"/run/systemd/resolve đã bị xoá", func(f *cageFake) {
			f.setMain(good, strings.Replace(goodMountinfo, "/systemd/resolve /run", "/systemd/resolve//deleted /run", 1), "/usr/sbin/php-fpm8.3")
		}, false},
		{"bản PHP đã bị thay", func(f *cageFake) { f.setMain(good, goodMountinfo, "/usr/sbin/php-fpm8.3 (deleted)") }, false},
		{"hàng rào mới hơn lúc dịch vụ khởi động", func(f *cageFake) { f.fenceAt = f.activeAt + 5 }, false},
		{"còn tiến trình của site ngoài slice", func(f *cageFake) {
			f.setProc("200", siteUID, "S (sleeping)", "0::/system.slice/php8.3-fpm.service\n")
		}, false},
		{"còn tiến trình ngoài slice khi lồng ngủ", func(f *cageFake) {
			f.stop(f.l.CageServiceUnit())
			f.setProc("200", siteUID, "S (sleeping)", "0::/system.slice/php8.3-fpm.service\n")
		}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newCageFake(t)
			f.applied(t)
			f.running()
			f.Handle([]string{"systemctl", "is-active", "mariadb"}, func(system.Cmd) system.Result {
				if f.mariadb {
					return system.Result{}
				}
				return system.Result{ExitCode: 3}
			})
			c.prep(f)
			got, err := (&CageRestart{Layout: f.l}).Check(ctx, f)
			if err != nil || got != c.want {
				t.Errorf("Check = %v, %v; muốn %v", got, err, c.want)
			}
		})
	}
}

func TestCageRestartLifecycle(t *testing.T) {
	cageLifecycle{
		name: "CageRestart",
		setup: func(f *cageFake) {
			f.applied(t)
			f.running()
			f.setMain(procStatus(siteUID, "S (sleeping)", "0000000000000000", "1"), goodMountinfo, "/usr/sbin/php-fpm8.3 (deleted)")
			f.setProc("200", siteUID, "S (sleeping)", "0::/system.slice/php8.3-fpm.service\n")
		},
		step:   func(l site.Layout) engine.Step { return &CageRestart{Layout: l} },
		noUndo: true,
	}.run(t)
}

func TestCageRestartApply(t *testing.T) {
	ctx := context.Background()
	shortCageWaits(t)
	setup := func(t *testing.T) *cageFake {
		f := newCageFake(t)
		f.applied(t)
		f.running()
		f.fenceAt = f.activeAt + 5
		f.setProc("200", siteUID, "S (sleeping)", "0::/system.slice/php8.3-fpm.service\n")
		return f
	}
	f := setup(t)
	if err := (&CageRestart{Layout: f.l}).Apply(ctx, f); err != nil {
		t.Fatal(err)
	}
	mk := cageRanIndex(f, templates.CageMySQLDirCmd[0], templates.CageMySQLDirCmd[1:]...)
	kill := cageRanIndex(f, "kill", "-KILL", "200")
	restart := cageRanIndex(f, "systemctl", "try-restart", f.l.CageServiceUnit())
	if mk < 0 || kill < mk || restart < kill {
		t.Fatalf("muốn tạo /run/mysqld, giết tiến trình ngoài slice, try-restart; lệnh %v", ranStrings(f.Ran()))
	}
	if f.Ran()[mk].AsUser != "" {
		t.Error("lệnh tạo /run/mysqld phải chạy bằng root ở máy thật")
	}

	t.Run("try-restart hỏng: lỗi có ngữ cảnh", func(t *testing.T) {
		f := setup(t)
		f.restartFail = true
		err := (&CageRestart{Layout: f.l}).Apply(ctx, f)
		if err == nil || !strings.Contains(err.Error(), f.l.CageServiceUnit()) || !strings.Contains(err.Error(), "control process") {
			t.Fatalf("Apply = %v", err)
		}
	})
}

// ============================================================ SiteVhostCaged

func TestSiteVhostCaged(t *testing.T) {
	l := cageLayout(t)
	for _, web := range []string{"nginx", "apache"} {
		for _, ssl := range []bool{false, true} {
			steps, err := SiteVhostCaged(l, web, "/var/lib/tpan/acme", ssl, site.HtaccessSafe)
			if err != nil {
				t.Fatalf("%s ssl=%v: %v", web, ssl, err)
			}
			want, err := templates.SiteVhostCaged(l, web, "/var/lib/tpan/acme", ssl, site.HtaccessSafe)
			if err != nil {
				t.Fatal(err)
			}
			v, _ := webVhostOf(l, web)
			file, ok1 := steps[0].(*File)
			link, ok2 := steps[1].(*Symlink)
			if len(steps) != 2 || !ok1 || !ok2 {
				t.Fatalf("%s ssl=%v: bước %T", web, ssl, steps)
			}
			if file.Path != v.conf || string(file.Content) != string(want) || !reflect.DeepEqual(file.Test, v.test) || !reflect.DeepEqual(file.Reload, v.reload) {
				t.Errorf("%s ssl=%v: File %s khác vhost ngoài lồng về đường dẫn, nội dung hay lệnh", web, ssl, file.Path)
			}
			if !strings.Contains(string(file.Content), l.CageSocket()) {
				t.Errorf("%s ssl=%v: vhost không trỏ socket của lồng", web, ssl)
			}
			if link.Target != v.conf || link.Link != v.link {
				t.Errorf("%s ssl=%v: symlink %s -> %s", web, ssl, link.Link, link.Target)
			}
		}
	}
	if _, err := SiteVhostCaged(l, "caddy", "/var/lib/tpan/acme", false, site.HtaccessSafe); err == nil {
		t.Error("web lạ mà không lỗi")
	}
	if _, err := SiteVhostCaged(site.Layout{}, "nginx", "/var/lib/tpan/acme", false, site.HtaccessSafe); err == nil {
		t.Error("Layout zero mà không lỗi")
	}
}
