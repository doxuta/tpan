package main

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/server"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// m17Pro là gói có lồng và đủ bảy trường.
var m17Pro = account.Package{
	MaxSites: 3, MaxDBs: 2, DiskMB: 1000, MaxBackups: 3,
	Cage: true, CPUPercent: 50, RAMMB: 256, Tasks: 64, IOReadMB: 20, IOWriteMB: 10, PHPWorkers: 3,
}

// m17Site dựng một site của owner; caged đặt cả file cgroup và hàng rào trên máy giả.
func (c *cli) m17Site(domain, owner string, caged, files bool) site.Site {
	c.t.Helper()
	d, err := validate.ParseDomain(domain)
	if err != nil {
		c.t.Fatal(err)
	}
	st := site.Site{Domain: d, Status: site.Active, PHPVersion: "8.3", Owner: owner, Caged: caged, Created: time.Now()}
	if files {
		c.fake.SetFile(st.Layout().CageFence(), []byte("# fence\n"), 0o644, system.Owner{})
	}
	if caged {
		dir := st.Layout().CageCgroup()
		put := func(name, body string) { c.fake.SetFile(dir+"/"+name, []byte(body), 0o644, system.Owner{}) }
		put("memory.current", fmt.Sprintf("%d\n", 100<<20))
		put("memory.peak", fmt.Sprintf("%d\n", 150<<20))
		put("memory.max", "268435456\n")
		put("memory.events", "max 0\noom_kill 0\n")
		put("cpu.stat", "usage_usec 1234567\nnr_throttled 0\nthrottled_usec 0\n")
		put("cpu.max", "50000 100000\n")
		put("pids.current", "7\n")
		put("pids.peak", "9\n")
		put("pids.max", "64\n")
	}
	return st
}

// m17Seed dựng máy mẫu: stack nginx; usera dùng gói pro (muốn lồng); và
//
//	a: usera, trong lồng, có file     (cage status: trong)
//	b: usera, ngoài lồng              (can-vao: gói muốn mà chưa vào)
//	b2: usera, ngoài lồng             (can-vao)
//	c: admin, trong lồng, có file     (can-ra: gói default không muốn lồng)
//	d: admin, ngoài lồng              (ngoai)
func (c *cli) m17Seed() {
	c.t.Helper()
	c.trashSeed(true,
		c.m17Site("a.example.com", "usera", true, true),
		c.m17Site("b.example.com", "usera", false, false),
		c.m17Site("b2.example.com", "usera", false, false),
		c.m17Site("c.example.com", "", true, true),
		c.m17Site("d.example.com", "", false, false))
	c.seedUser("usera", "pro", m17Pro)
	c.fake.SetFile("/sys/fs/cgroup/cgroup.controllers", []byte("cpu memory pids io\n"), 0o644, system.Owner{})
	c.fake.Handle([]string{"systemctl", "--version"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "systemd 255 (255.4-1ubuntu8)\n"}
	})
}

func (c *cli) pkg(name string) account.Package {
	c.t.Helper()
	var p account.Package
	c.withStore(func(ctx context.Context, st *store.Store) {
		var ok bool
		var err error
		if p, ok, err = st.Package(ctx, name); err != nil || !ok {
			c.t.Fatalf("đọc gói %s: %v %v", name, ok, err)
		}
	})
	return p
}

func m17JSON[T any](t *testing.T, out string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("stdout không phải JSON đúng kiểu: %v\n%s", err, out)
	}
	return v
}

// ---------------------------------------------------------------- đối số

func TestM17ArgsRejectedBeforeAnything(t *testing.T) {
	for _, bad := range []string{"a;rm -rf /", "$(id)", "../../etc/passwd", "a\nb", strings.Repeat("a", 300), "Khách"} {
		for _, args := range [][]string{
			{"cage", "status", bad},
			{"cage", "enable", bad, "--yes"},
			{"cage", "disable", bad, "--dry-run"},
			{"usage", bad},
			{"usage", bad, "--suggest"},
			{"package", "edit", bad, "--ram-mb", "128", "--yes"},
			{"package", "set", bad, "--cage", "on", "--yes"},
			{"package", "add", bad, "--cage", "on", "--yes"},
			{"isolation", "probe", "--only", bad, "--dry-run"},
		} {
			c := newCLI(t)
			code, out, errs := c.run(args...)
			if code != 1 || !strings.Contains(errs, "Từ chối:") || out != "" {
				t.Errorf("%q: mã %d, stdout %q, stderr %q", args, code, out, errs)
			}
			if len(c.fake.Ran()) != 0 || c.dbExists() {
				t.Errorf("%q: chạy lệnh %v hoặc tạo DB", args, c.fake.Ran())
			}
		}
	}
}

func TestM17CageChonSite(t *testing.T) {
	for _, args := range [][]string{
		{"cage", "enable", "--dry-run"},                           // không chọn gì
		{"cage", "enable", "a.example.com", "--all", "--dry-run"}, // chọn hai cách
		{"cage", "disable", "--all", "--stale", "--dry-run"},
		{"cage", "disable", "a.example.com", "--stale", "--dry-run"},
	} {
		c := newCLI(t)
		code, out, errs := c.run(args...)
		if code != 1 || !strings.Contains(errs, "Từ chối:") || out != "" || c.dbExists() || len(c.fake.Ran()) != 0 {
			t.Errorf("%q: mã %d, stdout %q, stderr %q", args, code, out, errs)
		}
	}
	// enable không có --stale: cờ không tồn tại.
	if code, _, _ := newCLI(t).run("cage", "enable", "--stale", "--dry-run"); code == 0 {
		t.Error("enable --stale phải bị từ chối")
	}
	// Không phải root: từ chối trước khi mở DB (trừ --dry-run).
	c := newCLI(t)
	c.root = false
	code, _, errs := c.run("cage", "enable", "a.example.com", "--yes")
	if code != 1 || !strings.Contains(errs, "root") || c.dbExists() {
		t.Errorf("không root: mã %d, %s", code, errs)
	}
}

// --dry-run mà chưa có tpan.db thì không tạo DB, không chạy lệnh.
func TestM17DryRunKhongTaoDB(t *testing.T) {
	for _, args := range [][]string{
		{"cage", "enable", "a.example.com", "--dry-run"},
		{"cage", "disable", "--all", "--dry-run"},
		{"cage", "status"},
		{"usage"},
		{"usage", "--suggest"},
		{"isolation", "probe", "--dry-run"},
	} {
		c := newCLI(t)
		code, _, errs := c.run(args...)
		if c.dbExists() || len(c.m17Mutating()) != 0 {
			t.Errorf("%q: mã %d, tạo DB hoặc đổi máy (%v), %s", args, code, c.m17Mutating(), errs)
		}
	}
	c := newCLI(t)
	if code, out, _ := c.run("cage", "status", "--json"); code != 0 || strings.TrimSpace(out) != "[]" {
		t.Errorf("status khi chưa có DB: mã %d, %q", code, out)
	}
}

// ---------------------------------------------------------------- status

func TestM17CageStatus(t *testing.T) {
	c := newCLI(t)
	c.m17Seed()
	code, out, errs := c.run("cage", "status", "--json")
	rows := m17JSON[[]core.CageRow](t, out)
	if code != 0 || len(rows) != 5 {
		t.Fatalf("mã %d, %d dòng, %s", code, len(rows), errs)
	}
	state := map[string]string{}
	for _, r := range rows {
		state[r.Domain] = r.State
	}
	want := map[string]string{
		"a.example.com": "trong", "b.example.com": "can-vao", "b2.example.com": "can-vao",
		"c.example.com": "can-ra", "d.example.com": "ngoai",
	}
	for d, s := range want {
		if state[d] != s {
			t.Errorf("%s: %q, muốn %q", d, state[d], s)
		}
	}
	for _, r := range rows {
		if (r.Usage != nil) != r.Caged {
			t.Errorf("%s: Usage chỉ có khi Caged (caged %v, usage %v)", r.Domain, r.Caged, r.Usage != nil)
		}
	}

	code, out, _ = c.run("cage", "status", "a.example.com", "--json")
	if one := m17JSON[[]core.CageRow](t, out); code != 0 || len(one) != 1 || one[0].Domain != "a.example.com" || one[0].Usage == nil || one[0].Usage.MemCurrent != 100<<20 {
		t.Errorf("một site: mã %d, %+v", code, one)
	}
	if code, out, _ = c.run("cage", "status"); code != 0 || !strings.Contains(out, "a.example.com") || !strings.Contains(out, "trong lồng") || !strings.Contains(out, "cần ra khỏi lồng") {
		t.Errorf("bảng: mã %d\n%s", code, out)
	}
	if code, out, _ = c.run("cage", "status", "none.example.com", "--json"); code != 1 || refusalCode(t, out) != "site_not_found" {
		t.Errorf("site không có: mã %d, %s", code, out)
	}
}

// ---------------------------------------------------------------- --all, --stale

func batchDomains(t *testing.T, out string) []string {
	t.Helper()
	var ds []string
	for _, it := range m17JSON[[]cageBatchItem](t, out) {
		ds = append(ds, it.Domain)
	}
	slices.Sort(ds)
	return ds
}

// Mỗi cờ chọn đúng site theo CageRow (mục 3.10), --dry-run không đổi gì.
func TestM17CageAllChonDungSite(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want []string
	}{
		{[]string{"cage", "enable", "--all"}, []string{"b.example.com", "b2.example.com"}},
		{[]string{"cage", "disable", "--all"}, []string{"a.example.com", "c.example.com"}},
		{[]string{"cage", "disable", "--stale"}, []string{"c.example.com"}},
	} {
		c := newCLI(t)
		c.m17Seed()
		code, out, errs := c.run(append(tc.args, "--dry-run", "--json")...)
		if got := batchDomains(t, out); !slices.Equal(got, tc.want) {
			t.Errorf("%q: chọn %v, muốn %v (mã %d, %s)", tc.args, got, tc.want, code, errs)
		}
		if m := c.m17Mutating(); len(m) != 0 {
			t.Errorf("%q: --dry-run đổi máy: %v", tc.args, m)
		}
		c.withStore(func(ctx context.Context, st *store.Store) {
			for _, d := range []string{"a.example.com", "c.example.com"} {
				s, _, _ := st.Site(ctx, mustDomain(t, d))
				if !s.Caged {
					t.Errorf("%q: --dry-run mà %s đã ra khỏi lồng", tc.args, d)
				}
			}
		})
	}

	// Không có site nào để làm: mã 0, [] chứ không phải null.
	c := newCLI(t)
	c.trashSeed(true, c.m17Site("d.example.com", "", false, false))
	if code, out, _ := c.run("cage", "disable", "--stale", "--json"); code != 0 || strings.TrimSpace(out) != "[]" {
		t.Errorf("không có site nào: mã %d, %q", code, out)
	}
}

func mustDomain(t *testing.T, s string) validate.Domain {
	t.Helper()
	d, err := validate.ParseDomain(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// Không --yes và không terminal (hay --json): từ chối confirm_required, không chạy gì.
func TestM17CageAllCanXacNhan(t *testing.T) {
	c := newCLI(t)
	c.m17Seed()
	code, out, _ := c.run("cage", "disable", "--stale", "--json")
	if code != 1 || refusalCode(t, out) != "confirm_required" || len(c.m17Mutating()) != 0 {
		t.Errorf("mã %d, %s", code, out)
	}
	// Có terminal mà trả lời không: huỷ, mã 1.
	c.terminal, c.stdin = true, "n\n"
	code, out, _ = c.run("cage", "disable", "--stale")
	if code != 1 || !strings.Contains(out, "c.example.com") || !strings.Contains(out, "Đã huỷ") || len(c.m17Mutating()) != 0 {
		t.Errorf("huỷ: mã %d\n%s", code, out)
	}
}

// Site này hỏng không kéo site khác; mã thoát khác 0 khi có site hỏng. Hai site
// chờ vào lồng đều không có file pool/vhost đúng mẫu nên bị từ chối: cả hai
// phải có dòng kết quả với lỗi, và lệnh thoát 1.
func TestM17CageAllSiteHongKhongKeoSiteKhac(t *testing.T) {
	c := newCLI(t)
	c.m17Seed()
	code, out, errs := c.run("cage", "enable", "--all", "--yes", "--json")
	items := m17JSON[[]cageBatchItem](t, out)
	if len(items) != 2 {
		t.Fatalf("mã %d, %d dòng; stdout %s; stderr %s", code, len(items), out, errs)
	}
	bad := 0
	for _, it := range items {
		if it.Error != "" || it.Report == nil || it.Report.Job.Status != engine.JobSucceeded {
			bad++
		}
		if it.Error == "" && it.Report == nil {
			t.Errorf("%s: không có kết quả lẫn lỗi", it.Domain)
		}
	}
	if (bad > 0) != (code == 1) {
		t.Errorf("mã thoát %d với %d site hỏng", code, bad)
	}
	// Stdout chỉ có đúng một tài liệu JSON.
	dec := json.NewDecoder(strings.NewReader(out))
	var first, second json.RawMessage
	if err := dec.Decode(&first); err != nil || dec.Decode(&second) == nil {
		t.Errorf("stdout phải có đúng một tài liệu JSON (%v)", err)
	}
}

// Chạy thật một site ra khỏi lồng: có audit, dòng sites đổi.
func TestM17CageDisableMotSite(t *testing.T) {
	c := newCLI(t)
	c.m17Seed()
	code, out, errs := c.run("cage", "disable", "c.example.com", "--yes", "--json")
	var rep engine.Report
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("mã %d, stdout không phải Report: %v\n%s\n%s", code, err, out, errs)
	}
	if rep.Job.Kind != "cage.disable" || rep.Job.Target != "c.example.com" {
		t.Errorf("job = %+v", rep.Job)
	}
	if n := c.audits("cage.disable"); n != 1 {
		t.Errorf("audit cage.disable = %d", n)
	}
	if rep.Job.Status == engine.JobSucceeded {
		c.withStore(func(ctx context.Context, st *store.Store) {
			if s, _, _ := st.Site(ctx, mustDomain(t, "c.example.com")); s.Caged {
				t.Error("job thành công mà site vẫn caged")
			}
		})
	}
}

// ---------------------------------------------------------------- usage

func TestM17Usage(t *testing.T) {
	c := newCLI(t)
	c.m17Seed()
	code, out, errs := c.run("usage", "--json")
	list := m17JSON[[]core.SiteUsage](t, out)
	if code != 0 || len(list) != 2 || list[0].Domain != "a.example.com" || list[1].Domain != "c.example.com" {
		t.Fatalf("danh sách: mã %d, %+v, %s", code, list, errs)
	}
	if list[0].Usage.MemCurrent != 100<<20 || list[0].Limits.RAMMB != 256 || list[0].Owner != "usera" {
		t.Errorf("a: %+v", list[0])
	}
	code, out, _ = c.run("usage", "a.example.com", "--json")
	if one := m17JSON[core.SiteUsage](t, out); code != 0 || one.Domain != "a.example.com" || one.Usage.TasksMax != 64 {
		t.Errorf("một site: mã %d, %+v", code, one)
	}
	if code, out, _ = c.run("usage"); code != 0 || !strings.Contains(out, "a.example.com") || !strings.Contains(out, "100/256") {
		t.Errorf("bảng: mã %d\n%s", code, out)
	}
	// Site ngoài lồng: lời từ chối usage_not_caged; không có: site_not_found.
	if code, out, _ = c.run("usage", "d.example.com", "--json"); code != 1 || refusalCode(t, out) != core.CodeUsageNotCaged {
		t.Errorf("site ngoài lồng: mã %d, %s", code, out)
	}
	if code, out, _ = c.run("usage", "none.example.com", "--json"); code != 1 || refusalCode(t, out) != "site_not_found" {
		t.Errorf("site không có: mã %d, %s", code, out)
	}
	if n := len(c.m17Mutating()); n != 0 {
		t.Errorf("usage chạy lệnh đổi máy (%d)", n)
	}
}

func TestM17UsageSuggestVaHenGio(t *testing.T) {
	c := newCLI(t)
	c.m17Seed()
	code, out, errs := c.run("usage", "--suggest", "--json")
	rep := m17JSON[core.SuggestionReport](t, out)
	if code != 0 || len(rep.Sites) != 2 || !rep.Sites[0].LowData || rep.Sites[0].Samples != 0 || len(rep.Packages) == 0 {
		t.Fatalf("mã %d, %+v, %s", code, rep, errs)
	}
	code, out, _ = c.run("usage", "a.example.com", "--suggest", "--json")
	one := m17JSON[core.SuggestionReport](t, out)
	if code != 0 || len(one.Sites) != 1 || one.Sites[0].Domain != "a.example.com" || len(one.Packages) != 1 || one.Packages[0].Package != "pro" {
		t.Errorf("một site: mã %d, %+v", code, one)
	}
	if code, out, _ = c.run("usage", "d.example.com", "--suggest", "--json"); code != 1 || refusalCode(t, out) != core.CodeUsageNotCaged {
		t.Errorf("site ngoài lồng: mã %d, %s", code, out)
	}
	if code, out, _ = c.run("usage", "--suggest"); code != 0 || !strings.Contains(out, "a.example.com") || !strings.Contains(out, "Chỉ là gợi ý") {
		t.Errorf("bảng: mã %d\n%s", code, out)
	}

	// Lệnh ẩn: ghi mẫu ngay, rồi gộp.
	code, out, errs = c.run("usage", "--sample", "--json")
	if got := m17JSON[map[string]any](t, out); code != 0 || got["sampled"] != float64(2) {
		t.Fatalf("--sample: mã %d, %s %s", code, out, errs)
	}
	c.withStore(func(ctx context.Context, st *store.Store) {
		n, err := st.UsageSamples(ctx, "a.example.com", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
		if err != nil || len(n) != 1 {
			t.Errorf("mẫu của a: %d, %v", len(n), err)
		}
	})
	if code, out, errs = c.run("usage", "--rollup", "--json"); code != 0 || m17JSON[map[string]any](t, out)["rolled_up"] != true {
		t.Errorf("--rollup: mã %d, %s %s", code, out, errs)
	}
	if code, _, _ = c.run("usage", "--sample", "a.example.com"); code != 1 {
		t.Errorf("--sample kèm domain phải bị từ chối: mã %d", code)
	}
	// Cờ ẩn không hiện trong help.
	if _, help, _ := c.run("usage", "--help"); strings.Contains(help, "--sample") || strings.Contains(help, "--rollup") {
		t.Error("help hiện cờ ẩn")
	}
}

// ---------------------------------------------------------------- package

func TestM17PackageCoLong(t *testing.T) {
	c := newCLI(t)
	c.trashSeed(true)
	code, out, errs := c.run("package", "add", "pro", "--max-sites", "3", "--cage", "on", "--cpu", "50", "--ram-mb", "256", "--tasks", "64",
		"--io-read-mb", "20", "--io-write-mb", "10", "--php-workers", "3", "--yes", "--json")
	row := m17JSON[server.PackageRow](t, out)
	if code != 0 || !row.Package.Cage || row.Package.RAMMB != 256 || row.Package.CPUPercent != 50 || row.Package.Tasks != 64 ||
		row.Package.IOReadMB != 20 || row.Package.IOWriteMB != 10 || row.Package.PHPWorkers != 3 || row.Package.MaxSites != 3 {
		t.Fatalf("add: mã %d, %+v, %s", code, row.Package, errs)
	}

	// Cờ không ghi thì giữ nguyên, kể cả bảy trường của lồng; `set` là tên khác của edit.
	want := c.pkg("pro")
	want.MaxSites = 9
	if code, out, errs = c.run("package", "set", "pro", "--max-sites", "9", "--yes", "--json"); code != 0 {
		t.Fatalf("set: mã %d, %s %s", code, out, errs)
	}
	if got := c.pkg("pro"); got != want {
		t.Errorf("sau set:\n có   %+v\n muốn %+v", got, want)
	}
	want.RAMMB = 512
	if code, out, errs = c.run("package", "edit", "pro", "--ram-mb", "512", "--yes", "--json"); code != 0 {
		t.Fatalf("edit: mã %d, %s %s", code, out, errs)
	}
	if got := c.pkg("pro"); got != want {
		t.Errorf("sau edit --ram-mb:\n có   %+v\n muốn %+v", got, want)
	}
	if n := c.audits("package.edit"); n != 2 {
		t.Errorf("audit package.edit = %d", n)
	}

	// Bị từ chối: ghi nhận trước khi ghi gì, gói giữ nguyên.
	for _, tc := range []struct {
		args []string
		code string
	}{
		{[]string{"--ram-mb", "10"}, core.CodePackageLimitInvalid},
		{[]string{"--cage", "off"}, core.CodePackageLimitsNeedCage}, // còn trần mà tắt lồng
		{[]string{"--cage", "maybe"}, "args_invalid"},
	} {
		code, out, _ := c.run(append([]string{"package", "edit", "pro", "--yes", "--json"}, tc.args...)...)
		if code != 1 || refusalCode(t, out) != tc.code {
			t.Errorf("%q: mã %d, %s", tc.args, code, out)
		}
	}
	if got := c.pkg("pro"); got != want {
		t.Errorf("bị từ chối mà gói đã đổi: %+v", got)
	}
	if code, out, _ = c.run("package", "edit", "pro", "--yes", "--json"); code != 1 || refusalCode(t, out) != "args_invalid" {
		t.Errorf("không cờ nào: mã %d, %s", code, out)
	}
	if code, out, _ = c.run("package", "edit", "khong_co", "--cpu", "50", "--yes", "--json"); code != 1 || refusalCode(t, out) != core.CodePackageNotFound {
		t.Errorf("gói không có: mã %d, %s", code, out)
	}
}

// Có site trong lồng dùng gói và trần đổi: --dry-run in Preview của cage.sync
// mà không ghi dòng gói; chạy thật ghi dòng gói rồi chạy job.
func TestM17PackageEditCoSiteTrongLong(t *testing.T) {
	c := newCLI(t)
	c.m17Seed()
	before := c.pkg("pro")

	code, out, errs := c.run("package", "edit", "pro", "--ram-mb", "512", "--dry-run", "--json")
	pv := m17JSON[engine.Preview](t, out)
	if code != 0 || pv.Kind != "cage.sync" || len(pv.Steps) == 0 {
		t.Fatalf("dry-run: mã %d, %+v, %s", code, pv, errs)
	}
	if got := c.pkg("pro"); got != before || len(c.m17Mutating()) != 0 {
		t.Errorf("dry-run mà đã đổi gói (%+v) hay máy (%v)", got, c.m17Mutating())
	}
	if code, out, _ = c.run("package", "edit", "pro", "--ram-mb", "512", "--json"); code != 1 || refusalCode(t, out) != "confirm_required" {
		t.Errorf("không --yes: mã %d, %s", code, out)
	}
	if got := c.pkg("pro"); got != before {
		t.Errorf("chưa xác nhận mà gói đã đổi: %+v", got)
	}

	code, out, errs = c.run("package", "edit", "pro", "--ram-mb", "512", "--yes", "--json")
	var rep engine.Report
	if err := json.Unmarshal([]byte(out), &rep); err != nil || rep.Job.Kind != "cage.sync" {
		t.Fatalf("mã %d, stdout không phải Report của cage.sync: %v\n%s\n%s", code, err, out, errs)
	}
	got := c.pkg("pro")
	before.RAMMB = 512
	if got != before {
		t.Errorf("dòng gói sau job:\n có   %+v\n muốn %+v", got, before)
	}
	if n := c.audits("package.edit"); n != 1 {
		t.Errorf("audit package.edit = %d", n)
	}
}

func TestM17UserEditDoiGoi(t *testing.T) {
	c := newCLI(t)
	c.m17Seed()
	// Không đổi gói: đường cũ, không job.
	if code, out, errs := c.run("user", "edit", "usera", "--full-name", "A Nguyen", "--yes", "--json"); code != 0 || !strings.Contains(out, "A Nguyen") {
		t.Fatalf("không đổi gói: mã %d, %s %s", code, out, errs)
	}
	if len(c.m17Mutating()) != 0 {
		t.Errorf("sửa tên mà chạy lệnh: %v", c.m17Mutating())
	}
	// Đổi gói của user có site trong lồng (pro → default: trần về 0): job.
	code, out, errs := c.run("user", "edit", "usera", "--package", "default", "--dry-run", "--json")
	pv := m17JSON[engine.Preview](t, out)
	if code != 0 || pv.Kind != "cage.sync" {
		t.Fatalf("dry-run: mã %d, %+v, %s", code, pv, errs)
	}
	c.withStore(func(ctx context.Context, st *store.Store) {
		if u, _, _ := st.User(ctx, "usera"); u.Package != "pro" {
			t.Errorf("dry-run mà đã đổi gói của user: %q", u.Package)
		}
	})
	code, out, errs = c.run("user", "edit", "usera", "--package", "default", "--full-name", "A2", "--yes", "--json")
	var rep engine.Report
	if err := json.Unmarshal([]byte(out), &rep); err != nil || rep.Job.Kind != "cage.sync" {
		t.Fatalf("mã %d, stdout không phải Report của cage.sync: %v\n%s\n%s", code, err, out, errs)
	}
	c.withStore(func(ctx context.Context, st *store.Store) {
		if u, _, _ := st.User(ctx, "usera"); u.Package != "default" || u.FullName != "A2" {
			t.Errorf("user sau job: %+v", u)
		}
	})
}

// ---------------------------------------------------------------- isolation probe

func TestM17IsolationProbe(t *testing.T) {
	c := newCLI(t)
	// Chưa có DB và stack: từ chối rõ, không tạo DB.
	code, out, _ := c.run("isolation", "probe", "--dry-run", "--json")
	if code != 1 || refusalCode(t, out) != "setup_required" || c.dbExists() {
		t.Errorf("chưa có DB: mã %d, %s", code, out)
	}
	// --cleanup đi cùng --only là sai.
	if code, out, _ = c.run("isolation", "probe", "--cleanup", "--only", "ngoai", "--json"); code != 1 || refusalCode(t, out) != "args_invalid" {
		t.Errorf("--cleanup --only: mã %d, %s", code, out)
	}

	c.trashSeed(true)
	code, out, errs := c.run("isolation", "probe", "--only", "ngoai", "--dry-run", "--json")
	pv := m17JSON[server.ProbePreview](t, out)
	if code != 0 || pv.Kind != "isolation.probe" || pv.Run.Only != "ngoai" || !site.IsProbeDomain(pv.Run.DomainA) || !site.IsProbeDomain(pv.Run.DomainB) {
		t.Fatalf("dry-run: mã %d, %+v, %s", code, pv, errs)
	}
	if m := c.m17Mutating(); len(m) != 0 {
		t.Errorf("--dry-run đổi máy: %v", m)
	}
	// Không --yes, không terminal: từ chối, chưa ghi sổ.
	if code, out, _ = c.run("isolation", "probe", "--json"); code != 1 || refusalCode(t, out) != "confirm_required" {
		t.Errorf("không --yes: mã %d, %s", code, out)
	}
	// Không có lượt nào chưa dọn: --cleanup nói vậy và thoát 0.
	if code, out, _ = c.run("isolation", "probe", "--cleanup", "--json"); code != 0 || m17JSON[map[string]any](t, out)["cleaned"] != false {
		t.Errorf("--cleanup khi không có gì: mã %d, %s", code, out)
	}
}

// printProbeReport: bảng, chi tiết ô lộ, và dòng tổng kết.
func TestM17InBaoCaoProbe(t *testing.T) {
	var out strings.Builder
	e := &env{stdout: &out, stderr: &out, lang: "vi"}
	printProbeReport(e, core.ProbeReport{
		ID: "0123456789abcdef", Web: "nginx", DomainA: "a.test", DomainB: "b.test", Exposed: 1,
		Results: []core.ProbeResult{
			{Path: "L1", Title: i18nOf("include wp-config"), Outside: core.ProbeCell{Status: core.ProbeBlocked}, Inside: core.ProbeCell{Status: core.ProbeBlocked}},
			{Path: "C1", Title: i18nOf("đọc /var/www"), Outside: core.ProbeCell{Status: core.ProbeNeedsCage}, Inside: core.ProbeCell{Status: core.ProbeExposed, Detail: i18nOf("thấy thư mục của B")}},
			{Path: "C3", Title: i18nOf("RAM 320 MB"), Outside: core.ProbeCell{Status: core.ProbeSkipped, Detail: i18nOf("máy ít RAM")}, Inside: core.ProbeCell{Status: core.ProbeNA}},
		},
	})
	for _, want := range []string{"L1", "chặn", "cần lồng", "LỘ", "thấy thư mục của B", "bỏ qua", "máy ít RAM", "Có 1 ô LỘ"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("thiếu %q trong:\n%s", want, out.String())
		}
	}
}

func i18nOf(s string) i18n.Text { return i18n.T(s, s) }

// m17Mutating là lệnh đã chạy mà không chỉ đọc. mutating() của main_test chỉ
// biết các lệnh chỉ đọc của các mốc trước; xem trước của lồng còn hỏi getent,
// systemctl --version, list-units và wp core is-installed.
func (c *cli) m17Mutating() []string {
	var bad []string
	for _, cmd := range c.fake.Ran() {
		a := strings.Join(cmd.Args, " ")
		switch {
		case cmd.Name == "getent" || cmd.Name == "dpkg-query" || cmd.Name == "ss" || cmd.Name == "a2query":
			continue
		case cmd.Name == "systemctl" && (strings.HasPrefix(a, "is-active") || strings.HasPrefix(a, "is-enabled") ||
			strings.HasPrefix(a, "--version") || strings.HasPrefix(a, "list-units") || strings.HasPrefix(a, "show")):
			continue
		case cmd.Name == "wp" && (strings.HasPrefix(a, "core is-installed") || strings.HasPrefix(a, "option get")):
			continue
		case cmd.Name == "mariadb" && strings.HasPrefix(string(cmd.Stdin), "SELECT"):
			continue
		}
		bad = append(bad, cmd.String())
	}
	return bad
}

// Vòng lặp chạy hết các site dù site đầu hỏng giữa chừng (pool.d không có nên
// job của mỗi site hoàn tác): mỗi site một job riêng, thoát mã 1.
func TestM17CageDisableAllMoiSiteMotJob(t *testing.T) {
	c := newCLI(t)
	c.m17Seed()
	code, out, errs := c.run("cage", "disable", "--all", "--yes", "--json")
	items := m17JSON[[]cageBatchItem](t, out)
	if len(items) != 2 || items[0].Report == nil || items[1].Report == nil || items[0].Report.Job.ID == items[1].Report.Job.ID {
		t.Fatalf("mã %d, muốn mỗi site một job: %s %s", code, out, errs)
	}
	for _, it := range items {
		if it.Report.Job.Kind != "cage.disable" || it.Report.Job.Target != it.Domain {
			t.Errorf("%s: job %+v", it.Domain, it.Report.Job)
		}
	}
	failed := items[0].Report.Job.Status != engine.JobSucceeded || items[1].Report.Job.Status != engine.JobSucceeded
	if failed != (code == 1) {
		t.Errorf("mã thoát %d, trạng thái %s và %s", code, items[0].Report.Job.Status, items[1].Report.Job.Status)
	}
	if n := c.audits("cage.disable"); n != 2 {
		t.Errorf("audit cage.disable = %d", n)
	}
}
