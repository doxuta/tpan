package server

// Test handler M17 (api_m17.go, hai handler của api_m13.go): qua guard thật
// (authzEnv: admin, usera là chủ của a.example.com, userb là người khác), máy là
// FakeSystem, store thật.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/cage"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// newM17Env là authzEnv trên máy đã cài stack (nginx, PHP 8.3): Plan của lồng
// đòi stack có trong bảng settings.
func newM17Env(t *testing.T) *authzEnv {
	t.Helper()
	e := newAuthzEnv(t)
	for k, v := range map[string]string{core.SettingWeb: "nginx", core.SettingPHPVersion: "8.3"} {
		if err := e.st.SetSetting(context.Background(), k, v); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

// m17Call gửi request qua guard thật với cookie của user (account.AdminName là admin).
func (e *authzEnv) m17Call(who, method, path, body string) *httptest.ResponseRecorder {
	e.t.Helper()
	return e.do(e.s.handler(authzDist), method, path, body, e.cookies[who])
}

func m17Decode[T any](t *testing.T, rec *httptest.ResponseRecorder, want int) T {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("mã %d, muốn %d; thân: %s", rec.Code, want, rec.Body)
	}
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("thân không phải JSON đúng kiểu: %v\n%s", err, rec.Body)
	}
	return v
}

func m17ErrCode(t *testing.T, rec *httptest.ResponseRecorder, want int) string {
	t.Helper()
	return m17Decode[ErrorBody](t, rec, want).Error.Code
}

// siteCaged lưu một site đang trong lồng và đặt file cgroup của nó (RAM đang
// dùng memMiB). owner "" là admin.
func (e *authzEnv) siteCaged(domain, owner string, caged bool, memMiB int) site.Site {
	e.t.Helper()
	d, err := validate.ParseDomain(domain)
	if err != nil {
		e.t.Fatal(err)
	}
	st := site.Site{Domain: d, Status: site.Active, PHPVersion: "8.3", Created: e.now, Owner: owner, Caged: caged}
	if err := e.st.SaveSite(context.Background(), st); err != nil {
		e.t.Fatal(err)
	}
	if caged {
		dir := st.Layout().CageCgroup()
		put := func(name, body string) { e.f.SetFile(dir+"/"+name, []byte(body), 0o644, system.Owner{}) }
		put("memory.current", fmt.Sprintf("%d\n", memMiB<<20))
		put("memory.peak", fmt.Sprintf("%d\n", memMiB<<21))
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

// ---------------------------------------------------------------- số liệu

func TestM17UsageLocTheoChu(t *testing.T) {
	e := newM17Env(t)
	// Chưa có site nào trong lồng: [] chứ không phải null.
	if rec := e.m17Call(authzA, "GET", "/api/v1/usage", ""); rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("chưa có lồng: mã %d, %q", rec.Code, rec.Body)
	}
	e.siteCaged("c.example.com", authzA, true, 100)
	e.siteCaged("d.example.com", authzB, true, 120)
	e.siteCaged("e.example.com", authzA, false, 0) // ngoài lồng: không có số liệu

	doms := func(who string) string {
		var out []string
		for _, u := range m17Decode[[]core.SiteUsage](t, e.m17Call(who, "GET", "/api/v1/usage", ""), 200) {
			if !u.Usage.Running {
				t.Errorf("%s: %s không Running", who, u.Domain)
			}
			out = append(out, u.Domain+"/"+u.Owner)
		}
		return strings.Join(out, ",")
	}
	if got := doms(authzA); got != "c.example.com/"+authzA {
		t.Errorf("A thấy %q", got)
	}
	if got := doms(authzB); got != "d.example.com/"+authzB {
		t.Errorf("B thấy %q", got)
	}
	if got := doms(account.AdminName); got != "c.example.com/"+authzA+",d.example.com/"+authzB {
		t.Errorf("admin thấy %q", got)
	}
	// Bất biến 6: số trả về là số đọc từ cgroup lúc gọi.
	us := m17Decode[[]core.SiteUsage](t, e.m17Call(authzA, "GET", "/api/v1/usage", ""), 200)
	if us[0].Usage.MemCurrent != 100<<20 || us[0].Usage.TasksMax != 64 {
		t.Errorf("số liệu không khớp cgroup: %+v", us[0].Usage)
	}
}

func TestM17SiteUsage(t *testing.T) {
	e := newM17Env(t)
	e.siteCaged("c.example.com", authzA, true, 100)
	e.siteCaged("e.example.com", authzA, false, 0)

	d := m17Decode[SiteUsageDetail](t, e.m17Call(authzA, "GET", "/api/v1/sites/c.example.com/usage", ""), 200)
	if d.Domain != "c.example.com" || d.Usage.MemCurrent != 100<<20 || d.History.Range != "day" || d.History.Domain != "c.example.com" {
		t.Errorf("chi tiết = %+v", d)
	}
	rec := e.m17Call(authzA, "GET", "/api/v1/sites/c.example.com/usage?range=week", "")
	if !strings.Contains(rec.Body.String(), `"points":[]`) || m17Decode[SiteUsageDetail](t, rec, 200).History.Range != "week" {
		t.Errorf("range=week: %s", rec.Body)
	}
	if got := m17ErrCode(t, e.m17Call(authzA, "GET", "/api/v1/sites/c.example.com/usage?range=year", ""), 400); got != "args_invalid" {
		t.Errorf("range lạ: mã lỗi %q", got)
	}
	// Site ngoài lồng: 404 usage_not_caged, kể cả với admin.
	for _, who := range []string{authzA, account.AdminName} {
		if got := m17ErrCode(t, e.m17Call(who, "GET", "/api/v1/sites/e.example.com/usage", ""), 404); got != core.CodeUsageNotCaged {
			t.Errorf("%s, site ngoài lồng: mã lỗi %q", who, got)
		}
	}
	// Site người khác: 404 của guard, cùng mã với site không có.
	other := m17Decode[ErrorBody](t, e.m17Call(authzB, "GET", "/api/v1/sites/c.example.com/usage", ""), 404)
	gone := m17Decode[ErrorBody](t, e.m17Call(authzB, "GET", "/api/v1/sites/none.example.com/usage", ""), 404)
	if other.Error.Code != "site_not_found" || gone.Error.Code != "site_not_found" {
		t.Errorf("site người khác %q, site không có %q", other.Error.Code, gone.Error.Code)
	}
	// Domain sai dạng: 400 trước mọi thứ.
	for _, bad := range []string{"a%3Brm", "%24%28id%29.com", "..%2f..%2fetc"} {
		if rec := e.m17Call(account.AdminName, "GET", "/api/v1/sites/"+bad+"/usage", ""); rec.Code != 400 && rec.Code != 404 {
			t.Errorf("domain %q: mã %d", bad, rec.Code)
		}
	}
}

func TestM17Suggestions(t *testing.T) {
	e := newM17Env(t)
	if got := m17ErrCode(t, e.m17Call(authzA, "GET", "/api/v1/usage/suggestions", ""), 403); got != "forbidden" {
		t.Errorf("user thường: %q", got)
	}
	rec := e.m17Call(account.AdminName, "GET", "/api/v1/usage/suggestions", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"sites":[]`) || !strings.Contains(rec.Body.String(), `"packages":[]`) {
		t.Fatalf("chưa có site trong lồng: mã %d, %s", rec.Code, rec.Body)
	}
	e.siteCaged("c.example.com", authzA, true, 100)
	rep := m17Decode[core.SuggestionReport](t, e.m17Call(account.AdminName, "GET", "/api/v1/usage/suggestions", ""), 200)
	if len(rep.Sites) != 1 || !rep.Sites[0].LowData || rep.Sites[0].Samples != 0 || rep.Sites[0].Reasons == nil {
		t.Errorf("gợi ý khi chưa có mẫu: %+v", rep.Sites)
	}
}

// ---------------------------------------------------------------- phân quyền và đầu vào

func TestM17RouteAdminTuChoiUserThuong(t *testing.T) {
	e := newM17Env(t)
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/v1/cage"},
		{"POST", "/api/v1/sites/" + authzSite + "/cage"},
		{"GET", "/api/v1/usage/suggestions"},
		{"GET", "/api/v1/security/probe"},
		{"POST", "/api/v1/security/probe"},
		{"POST", "/api/v1/security/probe/cleanup"},
		{"POST", "/api/v1/packages/default"},
	} {
		if got := m17ErrCode(t, e.m17Call(authzA, c.method, c.path, `{"enabled":true}`), 403); got != "forbidden" {
			t.Errorf("%s %s: %q", c.method, c.path, got)
		}
	}
	if len(e.f.Ran()) != 0 {
		t.Errorf("bị từ chối mà vẫn chạy lệnh: %v", e.f.Ran())
	}
}

func TestM17SiteCageDauVaoSai(t *testing.T) {
	e := newM17Env(t)
	cases := []struct {
		path, body, code string
	}{
		{"/api/v1/sites/" + authzSite + "/cage", `{}`, "bad_request"},                       // thiếu enabled: không ngầm hiểu là "ra lồng"
		{"/api/v1/sites/" + authzSite + "/cage", `{"enabled":true,"x":1}`, "bad_request"},   // trường lạ
		{"/api/v1/sites/" + authzSite + "/cage", `{"enabled":"yes"}`, "bad_request"},        // sai kiểu
		{"/api/v1/sites/a%3Brm%20-rf/cage", `{"enabled":true}`, validate.CodeDomainInvalid}, // domain tấn công
		{"/api/v1/sites/%24%28id%29.example.com/cage", `{"enabled":true}`, validate.CodeDomainInvalid},
		{"/api/v1/security/probe", `{"only":"x; id"}`, "args_invalid"},
		{"/api/v1/security/probe?dry_run=1", `{"only":"ca-hai"}`, "args_invalid"},
	}
	for _, c := range cases {
		if got := m17ErrCode(t, e.m17Call(account.AdminName, "POST", c.path, c.body), 400); got != c.code {
			t.Errorf("%s %s: mã lỗi %q, muốn %q", c.path, c.body, got, c.code)
		}
	}
	if len(e.f.Ran()) != 0 {
		t.Errorf("đầu vào sai mà vẫn chạy lệnh: %v", e.f.Ran())
	}
}

// Site không có: 404 site_not_found, không phải 500 (cả hai hướng).
func TestM17SiteCageSiteKhongCo(t *testing.T) {
	e := newM17Env(t)
	for _, body := range []string{`{"enabled":true}`, `{"enabled":false}`} {
		if got := m17ErrCode(t, e.m17Call(account.AdminName, "POST", "/api/v1/sites/none.example.com/cage?dry_run=1", body), 404); got != "site_not_found" {
			t.Errorf("%s: %q", body, got)
		}
	}
}

// Gói của chủ chưa bật lồng: vào lồng bị từ chối 409, ghi audit, không đổi máy.
func TestM17SiteCageGoiChuaBatLong(t *testing.T) {
	e := newM17Env(t)
	if got := m17ErrCode(t, e.m17Call(account.AdminName, "POST", "/api/v1/sites/"+authzSite+"/cage", `{"enabled":true}`), 409); got != core.CodeCageNotInPackage {
		t.Fatalf("mã lỗi %q", got)
	}
	a := hAudit(t, &apiEnv{dbPath: e.dbPath}, "cage.enable")
	if len(a) != 1 || a[0].Target != authzSite || a[0].Result != core.CodeCageNotInPackage {
		t.Errorf("audit = %+v", a)
	}
	// Ra khỏi lồng một site không ở trong lồng.
	if got := m17ErrCode(t, e.m17Call(account.AdminName, "POST", "/api/v1/sites/"+authzSite+"/cage?dry_run=1", `{"enabled":false}`), 409); got != core.CodeCageNotCaged {
		t.Errorf("ra lồng site ngoài lồng: %q", got)
	}
}

func TestM17Cage(t *testing.T) {
	e := newM17Env(t)
	rows := m17Decode[[]core.CageRow](t, e.m17Call(account.AdminName, "GET", "/api/v1/cage", ""), 200)
	if len(rows) != 1 || rows[0].Domain != authzSite || rows[0].Caged || rows[0].Want || rows[0].State != "ngoai" || rows[0].Usage != nil {
		t.Errorf("hàng đầu: %+v", rows)
	}
}

// ---------------------------------------------------------------- thử tấn công

func TestM17ProbeCleanupKhongCoGiDeDon(t *testing.T) {
	e := newM17Env(t)
	for _, path := range []string{"/api/v1/security/probe/cleanup", "/api/v1/security/probe/cleanup?dry_run=1"} {
		if got := m17ErrCode(t, e.m17Call(account.AdminName, "POST", path, ""), 409); got != "nothing_to_do" {
			t.Errorf("%s: %q", path, got)
		}
	}
	if len(e.f.Ran()) != 0 {
		t.Errorf("không có gì để dọn mà chạy lệnh: %v", e.f.Ran())
	}
}

func TestM17ProbeStatus(t *testing.T) {
	e := newM17Env(t)
	st := m17Decode[ProbeStatus](t, e.m17Call(account.AdminName, "GET", "/api/v1/security/probe", ""), 200)
	if st.Last != nil || st.Pending == nil || len(st.Pending) != 0 {
		t.Errorf("chưa có lượt nào: %+v", st)
	}
	if !strings.Contains(e.m17Call(account.AdminName, "GET", "/api/v1/security/probe", "").Body.String(), `"pending":[]`) {
		t.Error("pending rỗng phải là []")
	}
	run := cage.ProbeRun{ID: "0123456789abcdef", Started: e.now, State: "running", DomainA: "tpan-probe-a-0123456789ab.test", DomainB: "tpan-probe-b-0123456789ab.test"}
	if err := e.st.StartProbeRun(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	st = m17Decode[ProbeStatus](t, e.m17Call(account.AdminName, "GET", "/api/v1/security/probe", ""), 200)
	if st.Last != nil || len(st.Pending) != 1 || st.Pending[0].ID != run.ID {
		t.Errorf("một lượt đang chạy: %+v", st)
	}
	// Đang có lượt chưa dọn: lượt mới bị từ chối probe_running (409).
	if got := m17ErrCode(t, e.m17Call(account.AdminName, "POST", "/api/v1/security/probe?dry_run=1", `{}`), 409); got != core.CodeProbeRunning {
		t.Errorf("lượt mới khi còn lượt chưa dọn: %q", got)
	}
	// Dọn: xem trước có Plan probe.cleanup, không chạm máy.
	pv := m17Decode[engine.Preview](t, e.m17Call(account.AdminName, "POST", "/api/v1/security/probe/cleanup?dry_run=1", ""), 200)
	if pv.Kind != "probe.cleanup" || len(pv.Steps) == 0 {
		t.Errorf("xem trước dọn: %+v", pv)
	}
}

func TestM17ProbeDryRun(t *testing.T) {
	e := newM17Env(t)
	pv := m17Decode[ProbePreview](t, e.m17Call(account.AdminName, "POST", "/api/v1/security/probe?dry_run=1", `{"only":"ngoai"}`), 200)
	if pv.Kind != "isolation.probe" || pv.Run.Only != "ngoai" || len(pv.Run.ID) != 16 ||
		!strings.HasPrefix(pv.Run.DomainA, "tpan-probe-a-") || !strings.HasPrefix(pv.Run.DomainB, "tpan-probe-b-") ||
		!site.IsProbeDomain(pv.Run.DomainA) || !site.IsProbeDomain(pv.Run.DomainB) {
		t.Errorf("xem trước: %+v", pv)
	}
	// Xem trước không để lại dòng nào trong sổ.
	if pending, _ := e.st.PendingProbeRuns(context.Background()); len(pending) != 0 {
		t.Errorf("xem trước mà đã ghi sổ: %+v", pending)
	}
}

// ---------------------------------------------------------------- sửa gói, sửa user

// pro là gói có lồng và đủ bảy trường; usera dùng nó.
func (e *authzEnv) goiPro() account.Package {
	e.t.Helper()
	ctx := context.Background()
	p := account.Package{
		Name: "pro", MaxSites: 3, MaxDBs: 2, DiskMB: 1000, MaxBackups: 3,
		Cage: true, CPUPercent: 50, RAMMB: 256, Tasks: 64, IOReadMB: 20, IOWriteMB: 10, PHPWorkers: 3, Created: e.now,
	}
	if err := e.st.SavePackage(ctx, p); err != nil {
		e.t.Fatal(err)
	}
	u, ok, err := e.st.User(ctx, authzA)
	if err != nil || !ok {
		e.t.Fatal(ok, err)
	}
	u.Package = "pro"
	if err := e.st.UpdateUser(ctx, u); err != nil {
		e.t.Fatal(err)
	}
	return p
}

func (e *authzEnv) pkg(name string) account.Package {
	e.t.Helper()
	p, ok, err := e.st.Package(context.Background(), name)
	if err != nil || !ok {
		e.t.Fatalf("đọc gói %s: %v %v", name, ok, err)
	}
	return p
}

const m17OldFour = `"max_sites":5,"max_dbs":4,"disk_mb":2000,"max_backups":6`

// Gói không có site trong lồng: như trước M17 (200 PackageRow, không job), và
// thân chỉ có bốn giới hạn cũ thì bảy trường của lồng giữ nguyên.
func TestM17SuaGoiKhongCoSiteTrongLong(t *testing.T) {
	e := newM17Env(t)
	want := e.goiPro()
	row := m17Decode[PackageRow](t, e.m17Call(account.AdminName, "POST", "/api/v1/packages/pro", `{`+m17OldFour+`}`), 200)
	got := e.pkg("pro")
	want.MaxSites, want.MaxDBs, want.DiskMB, want.MaxBackups = 5, 4, 2000, 6
	if got != want || row.Package != want {
		t.Fatalf("thiếu bảy trường mà trần đổi:\n có   %+v\n muốn %+v", got, want)
	}
	if a := hAudit(t, &apiEnv{dbPath: e.dbPath}, "package.edit"); len(a) != 1 || a[0].Result != "ok" || a[0].Target != "pro" {
		t.Errorf("audit = %+v", a)
	}

	// Giao diện trước M17 gửi kèm "name" (thân cũ là account.Package): trùng
	// tên trên đường dẫn thì nhận như thân không có name.
	m17Decode[PackageRow](t, e.m17Call(account.AdminName, "POST", "/api/v1/packages/pro", `{"name":"pro",`+m17OldFour+`}`), 200)
	if got := e.pkg("pro"); got != want {
		t.Errorf("thân có name trùng đường dẫn: %+v, muốn %+v", got, want)
	}

	// Có mặt thì đổi đúng trường đó; trường khác giữ nguyên.
	m17Decode[PackageRow](t, e.m17Call(account.AdminName, "POST", "/api/v1/packages/pro", `{`+m17OldFour+`,"ram_mb":512}`), 200)
	want.RAMMB = 512
	if got := e.pkg("pro"); got != want {
		t.Errorf("đổi một trường: %+v, muốn %+v", got, want)
	}
}

func TestM17SuaGoiTuChoi(t *testing.T) {
	e := newM17Env(t)
	e.goiPro()
	for _, c := range []struct {
		name, body string
		status     int
		code       string
	}{
		{"pro", `{` + m17OldFour + `,"ram_mb":10}`, 400, core.CodePackageLimitInvalid},
		{"pro", `{` + m17OldFour + `,"cage":false}`, 400, core.CodePackageLimitsNeedCage}, // còn trần mà tắt lồng
		{"khong_co", `{` + m17OldFour + `}`, 404, core.CodePackageNotFound},
		{"pro", `{` + m17OldFour + `,"ram_mb":"nhieu"}`, 400, "bad_request"},
		{"pro", `{` + m17OldFour + `,"name":"khac"}`, 400, "args_invalid"}, // thân cũ có name: phải trùng đường dẫn
		{"pro", `{` + m17OldFour + `,"them":1}`, 400, "bad_request"},
		{"a%3Brm", `{}`, 400, validate.CodePackageNameInvalid},
	} {
		if got := m17ErrCode(t, e.m17Call(account.AdminName, "POST", "/api/v1/packages/"+c.name, c.body), c.status); got != c.code {
			t.Errorf("%s %s: mã lỗi %q, muốn %q", c.name, c.body, got, c.code)
		}
	}
	if got := e.pkg("pro"); got.RAMMB != 256 || got.MaxSites != 3 {
		t.Errorf("bị từ chối mà gói đã đổi: %+v", got)
	}
}

// Có site trong lồng dùng gói và trần đổi: ?dry_run=1 trả Preview của cage.sync
// và không ghi gì; không dry_run thì 202, và dòng gói đã ghi với bảy trường còn
// nguyên khi thân không nhắc tới.
func TestM17SuaGoiCoSiteTrongLong(t *testing.T) {
	e := newM17Env(t)
	e.goiPro()
	e.siteCaged("c.example.com", authzA, true, 100)

	pv := m17Decode[engine.Preview](t, e.m17Call(account.AdminName, "POST", "/api/v1/packages/pro?dry_run=1", `{`+m17OldFour+`,"ram_mb":512}`), 200)
	if pv.Kind != "cage.sync" || len(pv.Steps) == 0 {
		t.Fatalf("xem trước: %+v", pv)
	}
	if got := e.pkg("pro"); got.RAMMB != 256 || got.MaxSites != 3 {
		t.Fatalf("dry_run mà gói đã đổi: %+v", got)
	}

	started := m17Decode[JobStarted](t, e.m17Call(account.AdminName, "POST", "/api/v1/packages/pro", `{`+m17OldFour+`,"ram_mb":512}`), 202)
	if started.JobID == "" {
		t.Fatal("202 mà không có job_id")
	}
	e.s.api.wg.Wait()
	got := e.pkg("pro")
	if got.RAMMB != 512 || got.MaxSites != 5 || !got.Cage || got.CPUPercent != 50 || got.Tasks != 64 || got.IOReadMB != 20 || got.IOWriteMB != 10 || got.PHPWorkers != 3 {
		t.Errorf("dòng gói sau job: %+v", got)
	}
	if a := hAudit(t, &apiEnv{dbPath: e.dbPath}, "package.edit"); len(a) != 1 || a[0].JobID != started.JobID {
		t.Errorf("audit = %+v", a)
	}
}

func TestM17SuaGoiDryRunKhongSiteKhongGhi(t *testing.T) {
	e := newM17Env(t)
	e.goiPro()
	pv := m17Decode[engine.Preview](t, e.m17Call(account.AdminName, "POST", "/api/v1/packages/pro?dry_run=1", `{`+m17OldFour+`}`), 200)
	if pv.Kind != "package.edit" || pv.Steps == nil || len(pv.Steps) != 0 {
		t.Errorf("xem trước không có job: %+v", pv)
	}
	if got := e.pkg("pro"); got.MaxSites != 3 {
		t.Errorf("dry_run mà gói đã đổi: %+v", got)
	}
	if a := hAudit(t, &apiEnv{dbPath: e.dbPath}, "package.edit"); len(a) != 0 {
		t.Errorf("dry_run mà có audit: %+v", a)
	}
}

func TestM17SuaUser(t *testing.T) {
	e := newM17Env(t)
	e.goiPro()
	// Không đổi gói: như trước M17.
	row := m17Decode[UserRow](t, e.m17Call(account.AdminName, "POST", "/api/v1/users/"+authzA, `{"full_name":"A Nguyen"}`), 200)
	if row.User.FullName != "A Nguyen" || row.User.Package != "pro" {
		t.Errorf("không đổi gói: %+v", row.User)
	}
	// Đổi gói khi user chưa có site trong lồng: cũng như cũ.
	m17Decode[UserRow](t, e.m17Call(account.AdminName, "POST", "/api/v1/users/"+authzA, `{"package":"default"}`), 200)
	if u, _, _ := e.st.User(context.Background(), authzA); u.Package != "default" {
		t.Errorf("gói của user = %q", u.Package)
	}
	e.m17Call(account.AdminName, "POST", "/api/v1/users/"+authzA, `{"package":"pro"}`)

	// Có site trong lồng và trần đổi (pro → default: về 0): job.
	e.siteCaged("c.example.com", authzA, true, 100)
	pv := m17Decode[engine.Preview](t, e.m17Call(account.AdminName, "POST", "/api/v1/users/"+authzA+"?dry_run=1", `{"package":"default"}`), 200)
	if pv.Kind != "cage.sync" {
		t.Fatalf("xem trước: %+v", pv)
	}
	if u, _, _ := e.st.User(context.Background(), authzA); u.Package != "pro" {
		t.Fatalf("dry_run mà gói của user đã đổi: %q", u.Package)
	}
	started := m17Decode[JobStarted](t, e.m17Call(account.AdminName, "POST", "/api/v1/users/"+authzA, `{"package":"default","full_name":"A2"}`), 202)
	e.s.api.wg.Wait()
	// Mọi thay đổi của thân áp ở móc before, trước job.
	if u, _, _ := e.st.User(context.Background(), authzA); u.Package != "default" || u.FullName != "A2" {
		t.Errorf("user sau job: %+v", u)
	}
	if a := hAudit(t, &apiEnv{dbPath: e.dbPath}, "user.edit"); len(a) == 0 || a[len(a)-1].JobID != started.JobID {
		t.Errorf("audit = %+v", a)
	}
	// User không có: 404.
	if got := m17ErrCode(t, e.m17Call(account.AdminName, "POST", "/api/v1/users/khong_co", `{"package":"default"}`), 404); got != core.CodeUserNotFound {
		t.Errorf("user không có: %q", got)
	}
}

// ---------------------------------------------------------------- hợp đồng nhỏ

// Mã HTTP của các lời từ chối M17 (apiErrorStatus) khớp mục 3.10 của thiết
// kế: usage_not_caged là 404, ba mã đầu vào là 400, còn lại 409.
func TestM17BangMaHTTP(t *testing.T) {
	for code, want := range map[string]int{
		core.CodeUsageNotCaged: 404, core.CodePackageLimitInvalid: 400,
		core.CodePackageLimitsNeedCage: 400, core.CodeDomainReserved: 400,
	} {
		rec := httptest.NewRecorder()
		apiFail(rec, httptest.NewRequest(http.MethodGet, "/", nil), &core.RefusedError{Code: code})
		if rec.Code != want {
			t.Errorf("%s: mã %d, muốn %d", code, rec.Code, want)
		}
	}
	// Mã khác giữ mã của apiErrorStatus.
	rec := httptest.NewRecorder()
	apiFail(rec, httptest.NewRequest(http.MethodGet, "/", nil), &core.RefusedError{Code: core.CodeCageNotInPackage})
	if rec.Code != http.StatusConflict {
		t.Errorf("cage_not_in_package: %d", rec.Code)
	}
}

func TestM17PackageEditOver(t *testing.T) {
	cur := account.Package{Name: "p", MaxSites: 1, Cage: true, CPUPercent: 10, RAMMB: 100, Tasks: 40, IOReadMB: 2, IOWriteMB: 3, PHPWorkers: 4}
	got := PackageEdit{MaxSites: 9}.over(cur)
	want := cur
	want.MaxSites = 9
	if got != want {
		t.Errorf("không có trường mới: %+v", got)
	}
	one, no := 7, false
	got = PackageEdit{Cage: &no, RAMMB: &one, PHPWorkers: &one}.over(cur)
	want = cur
	want.MaxSites, want.Cage, want.RAMMB, want.PHPWorkers = 0, false, 7, 7
	if got != want {
		t.Errorf("có trường mới: %+v", got)
	}
}

// ---------------------------------------------------------------- job

// Ra khỏi lồng: dry_run trả Preview của cage.disable và không đổi máy; không
// dry_run thì 202 kèm job id, audit ghi domain và job. Kết quả của job trên máy
// giả không thuộc test này.
func TestM17SiteCageJob(t *testing.T) {
	e := newM17Env(t)
	e.goiPro()
	c := e.siteCaged("c.example.com", authzA, true, 100)
	// Ra lồng cần thấy hàng rào của site trên máy (không thì cage_not_caged).
	e.f.SetFile(c.Layout().CageFence(), []byte("# fence\n"), 0o644, system.Owner{})
	e.f.SetFile("/sys/fs/cgroup/cgroup.controllers", []byte("cpu memory pids io\n"), 0o644, system.Owner{})
	e.f.Handle([]string{"systemctl", "--version"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "systemd 255 (255.4-1ubuntu8)\n"}
	})
	pv := m17Decode[engine.Preview](t, e.m17Call(account.AdminName, "POST", "/api/v1/sites/c.example.com/cage?dry_run=1", `{"enabled":false}`), 200)
	if pv.Kind != "cage.disable" || pv.Target != "c.example.com" || len(pv.Steps) == 0 {
		t.Fatalf("xem trước: %+v", pv)
	}
	if got := mutatingRan(e.f); len(got) != 0 {
		t.Errorf("dry_run mà đã đổi máy: %v", got)
	}
	started := m17Decode[JobStarted](t, e.m17Call(account.AdminName, "POST", "/api/v1/sites/c.example.com/cage", `{"enabled":false}`), 202)
	e.s.api.wg.Wait()
	a := hAudit(t, &apiEnv{dbPath: e.dbPath}, "cage.disable")
	if started.JobID == "" || len(a) != 1 || a[0].Target != "c.example.com" || a[0].JobID != started.JobID {
		t.Errorf("job %q, audit %+v", started.JobID, a)
	}
}

// mutatingRan là các lệnh đã chạy mà không chỉ đọc (đủ cho test này: systemctl
// đổi trạng thái, ghi qua systemd-run).
func mutatingRan(f *system.FakeSystem) []string {
	var out []string
	for _, c := range f.Ran() {
		switch c.Name {
		case "systemctl", "systemd-run", "nginx", "php-fpm8.3":
			if len(c.Args) > 0 && (c.Args[0] == "is-active" || c.Args[0] == "is-enabled" || c.Args[0] == "show") {
				continue
			}
			out = append(out, c.String())
		}
	}
	return out
}
