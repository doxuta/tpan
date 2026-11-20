package steps

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
	"github.com/doxuta/tpan/internal/validate"
)

// suspendBase là máy có site đang chạy: pool và cron của site có trên đĩa.
func suspendBase(t *testing.T, m *machine, l site.Layout) {
	t.Helper()
	webBase(m, l)
	m.SetDir("/etc/cron.d", 0o755, system.Root)
	pool, err := templates.SitePool(l, "8.3")
	if err != nil {
		t.Fatal(err)
	}
	cron, err := templates.SiteCron(l)
	if err != nil {
		t.Fatal(err)
	}
	m.SetFile(l.PoolConf("8.3"), pool, 0o644, system.Root)
	m.SetFile(site.CronFile(l), cron, 0o644, system.Root)
}

// suspendedBase là máy có site đang tạm khoá: hai file đã mang đuôi .suspended.
func suspendedBase(t *testing.T, m *machine, l site.Layout) {
	t.Helper()
	suspendBase(t, m, l)
	for _, p := range []string{l.PoolConf("8.3"), site.CronFile(l)} {
		if err := m.Rename(p, p+SuspendedSuffix); err != nil {
			t.Fatal(err)
		}
	}
}

// Bốn trường hợp của skill tpan-step cho mọi bước của tạm khoá.
func TestSuspendLifecycle(t *testing.T) {
	l := webLayout(t)
	on := func(m *machine) { suspendBase(t, m, l) }
	off := func(m *machine) { suspendedBase(t, m, l) }
	vhost := func(web string, ssl bool, i int) func() engine.Step {
		return func() engine.Step { return webStepsOK(t)(SiteVhostSuspended(l, web, webAcme, ssl))[i] }
	}
	page := func() engine.Step { return webStepOK(t)(SuspendedPage(l)) }
	cases := []lifecycle{
		{name: "PoolSuspend", setup: on, step: func() engine.Step { return webStepOK(t)(PoolSuspend(l, "8.3")) }},
		{name: "PoolUnsuspend", setup: off, step: func() engine.Step { return webStepOK(t)(PoolUnsuspend(l, "8.3")) }},
		{name: "CronSuspend", setup: on, step: func() engine.Step { return webStepOK(t)(CronSuspend(l)) }},
		{name: "CronUnsuspend", setup: off, step: func() engine.Step { return webStepOK(t)(CronUnsuspend(l)) }},
		{name: "SiteVhostSuspended nginx file", setup: func(m *machine) { webBase(m, l) }, step: vhost("nginx", false, 0)},
		{name: "SiteVhostSuspended apache ssl file", setup: func(m *machine) { webBase(m, l) }, step: vhost("apache", true, 0)},
		{name: "SiteVhostSuspended nginx link", setup: func(m *machine) { webBase(m, l); execute(t, m, vhost("nginx", false, 0)()) }, step: vhost("nginx", false, 1)},
		{name: "SuspendedPage máy sạch", setup: func(m *machine) {}, step: page},
		{name: "SuspendedPage có thư mục", setup: func(m *machine) { m.SetDir(templates.SuspendedDir, 0o755, system.Root) }, step: page},
	}
	for _, c := range cases {
		t.Run(c.name, c.run)
	}
}

// Đổi tên đúng chỗ, đúng lệnh kiểm và reload, và khai file đúng cho engine
// ghi managed_files: tạm khoá chỉ bỏ dòng của file .conf; mở khoá ghi lại
// .conf với nội dung template.
func TestSuspendRenamePaths(t *testing.T) {
	l := webLayout(t)
	ctx := context.Background()
	m := newMachine(t)
	suspendBase(t, m, l)
	pool := l.PoolConf("8.3")
	orig, _ := m.ReadFile(pool)

	s := webStepOK(t)(PoolSuspend(l, "8.3"))
	info := s.Info()
	if len(info.Files) != 1 || info.Files[0].Path != pool || !info.Files[0].Delete {
		t.Errorf("Files của tạm khoá = %+v; muốn đúng một dòng Delete %s", info.Files, pool)
	}
	if !info.Danger || info.Title.Vi == "" || info.Title.En == "" || info.Why.Vi == "" || info.Why.En == "" {
		t.Errorf("Info thiếu Danger, Title hay Why: %+v", info)
	}
	if err := s.Apply(ctx, m); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(ranStrings(m.Ran()), " | "); got != "php-fpm8.3 -t | systemctl reload php8.3-fpm" {
		t.Errorf("lệnh = %q", got)
	}
	if _, err := m.Stat(pool); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("%s còn sau khi tạm khoá: %v", pool, err)
	}
	if got, _ := m.ReadFile(pool + SuspendedSuffix); !bytes.Equal(got, orig) {
		t.Error("file .suspended không giữ nguyên nội dung pool")
	}

	u := webStepOK(t)(PoolUnsuspend(l, "8.3"))
	ui := u.Info()
	if len(ui.Files) != 2 || ui.Files[0].Path != pool+SuspendedSuffix || !ui.Files[0].Delete ||
		ui.Files[1].Path != pool || ui.Files[1].Delete || !bytes.Equal(ui.Files[1].New, orig) {
		t.Errorf("Files của mở khoá = %+v", ui.Files)
	}
	if r := u.(*Rename); r.WaitFor != l.SocketFor("8.3") {
		t.Errorf("WaitFor = %q; muốn socket của pool", r.WaitFor)
	}
	if err := u.Apply(ctx, m); err != nil {
		t.Fatal(err)
	}
	if got, _ := m.ReadFile(pool); !bytes.Equal(got, orig) {
		t.Error("mở khoá không trả lại đúng file pool")
	}

	c := webStepOK(t)(CronSuspend(l))
	if r := c.(*Rename); r.From != site.CronFile(l) || r.To != site.CronFile(l)+".suspended" || r.Test != nil || r.Reload != nil {
		t.Errorf("CronSuspend = %+v; cron không cần kiểm hay reload", r)
	}
}

// Kiểm cú pháp lỗi: file về tên cũ, lỗi nói rõ lệnh và file.
func TestSuspendRenameTestFails(t *testing.T) {
	l := webLayout(t)
	ctx := context.Background()
	m := newMachine(t)
	suspendBase(t, m, l)
	m.failCmd([]string{"php-fpm8.3", "-t"}, 1, "lỗi cú pháp giả")
	before := m.whole()
	err := webStepOK(t)(PoolSuspend(l, "8.3")).Apply(ctx, m)
	if err == nil {
		t.Fatal("Apply không lỗi khi kiểm cú pháp lỗi")
	}
	for _, s := range []string{"kiểm cấu hình", l.PoolConf("8.3"), "lỗi cú pháp giả"} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("lỗi thiếu %q: %v", s, err)
		}
	}
	if !equalWhole(before, m.whole()) {
		t.Error("máy khác trước Apply sau khi kiểm lỗi")
	}
}

// Reload lỗi: đổi tên ngược lại và nạp lại cấu hình cũ.
func TestSuspendRenameReloadFails(t *testing.T) {
	l := webLayout(t)
	ctx := context.Background()
	m := newMachine(t)
	suspendBase(t, m, l)
	m.failCmd([]string{"systemctl", "reload", "php8.3-fpm"}, 1, "reload hỏng")
	err := webStepOK(t)(PoolSuspend(l, "8.3")).Apply(ctx, m)
	if err == nil || !strings.Contains(err.Error(), "reload hỏng") {
		t.Fatalf("Apply = %v; muốn lỗi reload", err)
	}
	if _, err := m.Stat(l.PoolConf("8.3")); err != nil {
		t.Errorf("pool không về tên cũ: %v", err)
	}
}

// File đích đã có (sót từ một lần trước): bị đè, và Undo trả lại nó.
func TestSuspendRenameKeepsExistingTarget(t *testing.T) {
	l := webLayout(t)
	ctx := context.Background()
	m := newMachine(t)
	suspendBase(t, m, l)
	old := []byte("; bản .suspended cũ\n")
	m.SetFile(l.PoolConf("8.3")+SuspendedSuffix, old, 0o600, system.Root)
	before := m.whole()
	s := webStepOK(t)(PoolSuspend(l, "8.3"))
	if err := s.Apply(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err := s.Undo(ctx, m); err != nil {
		t.Fatal(err)
	}
	if !equalWhole(before, m.whole()) {
		t.Error("Undo không trả lại file .suspended cũ")
	}
}

// Không có file nào để đổi tên: Check đạt (pool đã không được nạp), không lệnh nào chạy.
func TestSuspendRenameNothing(t *testing.T) {
	l := webLayout(t)
	m := newMachine(t)
	webBase(m, l)
	rep := execute(t, m, webStepOK(t)(PoolSuspend(l, "8.3")))
	if rep.Steps[0].Outcome != "skipped" || len(m.Ran()) != 0 {
		t.Errorf("outcome %s, lệnh %v; muốn bỏ qua, không lệnh nào", rep.Steps[0].Outcome, ranStrings(m.Ran()))
	}
	m.SetDir(l.PoolConf("8.3"), 0o755, system.Root)
	if _, err := webStepOK(t)(PoolSuspend(l, "8.3")).Check(context.Background(), m); err == nil {
		t.Error("Check không lỗi khi pool là thư mục")
	}
	if err := (&Rename{From: "/a", To: "/b"}).Undo(context.Background(), m); !errors.Is(err, errNotApplied) {
		t.Errorf("Undo khi chưa Apply = %v", err)
	}
}

// Khoá user dồn bước của nhiều site vào một Plan: trang tạm khoá chung mang ID
// riêng từng site (ID phải duy nhất trong Plan), site sau thấy file đã đúng.
func TestSuspendedPageTwoSites(t *testing.T) {
	a := webLayout(t)
	db, err := validate.ParseDomain("b.example.com")
	if err != nil {
		t.Fatal(err)
	}
	b := site.LayoutOf(db)
	m := newMachine(t)
	pa, pb := webStepOK(t)(SuspendedPage(a)), webStepOK(t)(SuspendedPage(b))
	if pa.Info().ID == pb.Info().ID {
		t.Fatalf("hai site cùng ID %s", pa.Info().ID)
	}
	if f := pa.Info().Files; len(f) != 1 || f[0].Path != templates.SuspendedPage {
		t.Errorf("Files = %+v; muốn trang tạm khoá để engine ghi managed_files", f)
	}
	rep := execute(t, m, pa, pb)
	if rep.Job.Status != engine.JobSucceeded || rep.Steps[0].Outcome != "applied" || rep.Steps[1].Outcome != "skipped" {
		t.Errorf("job %s, bước %+v", rep.Job.Status, rep.Steps)
	}
	fi, err := m.Stat(templates.SuspendedDir)
	if err != nil || fi.Mode.Perm() != 0o755 {
		t.Errorf("thư mục trang: %v %v", fi.Mode, err)
	}
	fi, err = m.Stat(templates.SuspendedPage)
	if err != nil || fi.Mode.Perm() != 0o644 || fi.Owner.User != "" && fi.Owner.User != "root" {
		t.Errorf("trang: %v %+v %v", fi.Mode, fi.Owner, err)
	}
}

func TestSuspendBadParams(t *testing.T) {
	l := webLayout(t)
	bad := l
	bad.Pool = "tpan-x; rm -rf /"
	for name, err := range map[string]error{
		"pool bản PHP lạ":   second(PoolSuspend(l, "8.3; id")),
		"pool layout sửa":   second(PoolUnsuspend(bad, "8.3")),
		"cron layout zero":  second(CronSuspend(site.Layout{})),
		"cron layout sửa":   second(CronUnsuspend(bad)),
		"vhost web lạ":      secondSlice(SiteVhostSuspended(l, "caddy", webAcme, false)),
		"vhost acme lạ":     secondSlice(SiteVhostSuspended(l, "nginx", "../../etc/passwd", false)),
		"vhost layout zero": secondSlice(SiteVhostSuspended(site.Layout{}, "nginx", webAcme, true)),
		"rename tương đối":  (&Rename{From: "etc/x", To: "/etc/y"}).validate(),
		"rename trùng":      (&Rename{From: "/etc/x", To: "/etc/x"}).validate(),
		"rename có ..":      (&Rename{From: "/etc/x/../shadow", To: "/etc/y"}).validate(),
		"rename xuống dòng": (&Rename{From: "/etc/x\n", To: "/etc/y"}).validate(),
	} {
		if err == nil {
			t.Errorf("%s: không lỗi", name)
		}
	}
}

func second(_ engine.Step, err error) error        { return err }
func secondSlice(_ []engine.Step, err error) error { return err }

// equalWhole so hai ảnh chụp máy (machine.whole).
func equalWhole(a, b any) bool { return reflect.DeepEqual(a, b) }
