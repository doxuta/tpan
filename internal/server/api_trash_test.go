package server

// Test của API M6 (api_trash.go, trash_timer.go). Hai nhóm:
//   - không phụ thuộc thân core của đợt 2: nhiều mật khẩu một job, lọc sự
//     kiện của POST /apply, đầu vào sai, export, bộ hẹn giờ (hàm purge giả);
//   - đi qua core thật trên FakeSystem (thùng rác, khôi phục, xoá hẳn, apply,
//     số ngày giữ): kiểm hợp đồng HTTP (mã, kind của job, nhật ký), không đòi
//     job thành công, vì FakeSystem ở đây không giả lập máy (việc đó là test
//     của core và của demo).

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/validate"
)

// trashSite ghi một site đang ở thùng rác thẳng vào store.
func (e *apiEnv) trashSite(t *testing.T, dom string) validate.Domain {
	t.Helper()
	d := apiDomain(t, dom)
	st := site.Site{
		Domain: d, Status: site.Trashed, PHPVersion: "8.3", Created: e.now.Add(-48 * time.Hour),
		TrashedAt: e.now.Add(-time.Hour), PurgeAt: e.now.Add(6 * 24 * time.Hour),
	}
	if err := e.st.SaveSite(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	return d
}

// trashAuditIP đọc cột ip của các dòng nhật ký của action (hAudit không đọc ip).
func trashAuditIP(t *testing.T, e *apiEnv, action string) []string {
	t.Helper()
	db, err := sql.Open("sqlite", e.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	rows, err := db.Query("SELECT ip FROM audit WHERE action = ? ORDER BY rowid", action)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var ip string
		if err := rows.Scan(&ip); err != nil {
			t.Fatal(err)
		}
		out = append(out, ip)
	}
	return out
}

// trashJobKind đọc kind của job vừa chạy (sau wg.Wait).
func trashJobKind(t *testing.T, e *apiEnv, id string) string {
	t.Helper()
	job, err := e.st.Job(context.Background(), id)
	if err != nil {
		t.Fatalf("job %s: %v", id, err)
	}
	if job.Status == engine.JobRunning {
		t.Fatalf("job %s vẫn running sau khi goroutine xong", id)
	}
	return job.Kind
}

// ---------------------------------------------------------------- nhiều mật khẩu

func TestAPIJobSecretsOnce(t *testing.T) {
	e := apiNewEnv(t)
	want := []AdminSecret{
		{URL: "http://a.example.com/wp-admin/", User: "admin", Password: "Pw1SecretValueAaa111"},
		{URL: "http://b.example.com/wp-admin/", User: "admin", Password: "Pw2SecretValueBbb222"},
	}
	if !e.s.api.claim() {
		t.Fatal("claim")
	}
	rec := httptest.NewRecorder()
	e.s.jobStart(rec, httptest.NewRequest("POST", "/api/v1/apply", nil), jobOpts{action: "apply", target: "2 site"},
		func(ctx context.Context, onEvent func(engine.Event)) (jobResult, error) {
			rep, err := engine.New(e.f, engine.Options{Journal: e.st, OnEvent: onEvent}).
				Execute(ctx, engine.Plan{Kind: "apply", Target: "2 site"})
			return jobResult{status: rep.Job.Status, secrets: want}, err
		})
	js := apiDecode[JobStarted](t, rec, 202)
	e.s.api.wg.Wait()

	first := apiDecode[JobDetail](t, apiCall(e.s.handleJob, "GET", "/", "", "id", js.JobID), 200)
	if first.Secret != nil || len(first.Secrets) != 2 || first.Secrets[1] != want[1] {
		t.Fatalf("lần GET đầu: Secret %+v, Secrets %+v", first.Secret, first.Secrets)
	}
	second := apiCall(e.s.handleJob, "GET", "/", "", "id", js.JobID)
	if d := apiDecode[JobDetail](t, second, 200); d.Secrets != nil || d.Secret != nil {
		t.Fatalf("mật khẩu hiện lần thứ hai: %+v", d)
	}
	if strings.Contains(second.Body.String(), `"secrets"`) {
		t.Fatal("JSON lần hai vẫn có trường secrets (phải bỏ hẳn)")
	}
}

// ---------------------------------------------------------------- lọc sự kiện

// POST /apply: Execute chạy job hàng loạt rồi các job SSL; SSE và runs chỉ
// gắn với job đầu.
func TestAPIApplyOnlyFirstJobInFeed(t *testing.T) {
	e := apiNewEnv(t)
	if !e.s.api.claim() {
		t.Fatal("claim")
	}
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	var second atomic.Value
	rec := httptest.NewRecorder()
	e.s.jobStart(rec, httptest.NewRequest("POST", "/api/v1/apply", nil), jobOpts{action: "apply", target: "2 site"},
		func(ctx context.Context, onEvent func(engine.Event)) (jobResult, error) {
			on := firstJobOnly(onEvent)
			eng := engine.New(e.f, engine.Options{Journal: e.st, OnEvent: on})
			rep, err := eng.Execute(ctx, engine.Plan{Kind: "apply", Target: "2 site"})
			if err != nil {
				return jobResult{}, err
			}
			<-release
			ssl, err := eng.Execute(ctx, engine.Plan{Kind: "site.ssl", Target: "a.example.com"})
			second.Store(ssl.Job.ID)
			return jobResult{status: rep.Job.Status}, err
		})
	js := apiDecode[JobStarted](t, rec, 202)
	run := e.s.api.run(js.JobID)
	if run == nil {
		t.Fatal("job đầu không có trong runs")
	}
	unblock()
	e.s.api.wg.Wait()

	sslID, _ := second.Load().(string)
	if sslID == "" || sslID == js.JobID {
		t.Fatalf("job SSL id = %q, job đầu %q", sslID, js.JobID)
	}
	run.feed.mu.Lock()
	evs := append([]engine.Event(nil), run.feed.events...)
	run.feed.mu.Unlock()
	starts := 0
	for _, ev := range evs {
		if ev.JobID != js.JobID {
			t.Fatalf("SSE của job đầu có sự kiện của job %s: %+v", ev.JobID, ev)
		}
		if ev.Kind == engine.EventJobStart {
			starts++
		}
	}
	if starts != 1 || len(evs) == 0 || evs[len(evs)-1].Kind != engine.EventJobDone {
		t.Fatalf("sự kiện của job đầu = %+v", evs)
	}
	// Job SSL vẫn có lịch sử riêng trong store.
	if sevs, _ := e.st.Events(context.Background(), sslID, 0); len(sevs) == 0 {
		t.Fatal("job SSL không có sự kiện trong store")
	}
	if a := hAudit(t, e, "apply"); len(a) != 1 || a[0].JobID != js.JobID || a[0].Result != "ok" {
		t.Fatalf("nhật ký apply = %+v", a)
	}
}

// ---------------------------------------------------------------- apply: đầu vào

func TestAPIApplyBadInput(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	var big strings.Builder
	big.WriteString(`{"defaults":{"admin_email":"a@example.com"},"sites":[`)
	for i := range core.MaxSitesEntries + 1 {
		if i > 0 {
			big.WriteString(",")
		}
		fmt.Fprintf(&big, `{"domain":"site-%d.example.com","title":"%s"}`, i, strings.Repeat("t", 100))
	}
	big.WriteString(`]}`)
	if big.Len() <= 64<<10 {
		t.Fatalf("thân thử chỉ %d byte, phải quá 64 KiB", big.Len())
	}
	cases := []struct {
		name, body, code string
	}{
		{"không phải JSON", `{`, "bad_request"},
		{"trường lạ", `{"sites":[],"x":1}`, "bad_request"},
		{"chèn lệnh", `{"sites":[{"domain":"a.com; rm -rf /"}]}`, validate.CodeDomainInvalid},
		{"$(id)", `{"sites":[{"domain":"$(id).example.com"}]}`, validate.CodeDomainInvalid},
		{"xuống dòng", `{"sites":[{"domain":"a.example.com\nb"}]}`, validate.CodeDomainInvalid},
		{"email sai", `{"defaults":{"admin_email":"khong-phai-email"},"sites":[{"domain":"a.example.com"}]}`, validate.CodeEmailInvalid},
		{"tiêu đề có xuống dòng", `{"sites":[{"domain":"a.example.com","title":"a\nb"}]}`, "title_invalid"},
		{"trùng sau chuẩn hoá", `{"sites":[{"domain":"A.example.com"},{"domain":"a.example.com"}]}`, "sites_file_invalid"},
		// Quá 64 KiB vẫn được đọc hết (không phải bad_request): lỗi là quá số dòng.
		{"quá số dòng", big.String(), "sites_file_invalid"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, q := range []string{"", "?dry_run=1"} {
				rec := apiCall(e.s.handleApply, "POST", "/api/v1/apply"+q, c.body)
				if got := apiErrCode(t, rec, 400); got != c.code {
					t.Fatalf("%s: code = %q, muốn %q", q, got, c.code)
				}
			}
		})
	}
	if jobs, _ := e.st.Jobs(context.Background(), 0); len(jobs) != 0 {
		t.Fatalf("đầu vào sai mà vẫn có job: %v", jobs)
	}
	if !e.s.api.claim() {
		t.Fatal("đầu vào sai mà chưa nhả chỗ chạy job")
	}
}

// ---------------------------------------------------------------- export

func TestAPIExport(t *testing.T) {
	e := apiNewEnv(t)
	e.addSite(t, "b.example.com")
	e.addSite(t, "a.example.com")
	e.trashSite(t, "old.example.com")
	rec := apiCall(e.s.handleExport, "GET", "/api/v1/export", "")
	if rec.Code != 200 {
		t.Fatalf("mã = %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/yaml; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); cd != `attachment; filename="sites.yaml"` {
		t.Errorf("Content-Disposition = %q", cd)
	}
	f, err := core.ParseSitesFile(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("đọc lại file export: %v\n%s", err, rec.Body.String())
	}
	var got []string
	for _, s := range f.Sites {
		got = append(got, s.Domain)
	}
	if strings.Join(got, ",") != "a.example.com,b.example.com" {
		t.Fatalf("site trong export = %v (site thùng rác không được có)", got)
	}
}

// ---------------------------------------------------------------- bộ hẹn giờ

// trashTestTimer là bộ hẹn giờ với hàm purge giả đếm số lần gọi.
func trashTestTimer(e *apiEnv, res []core.PurgeResult) (*trashTimer, *atomic.Int32) {
	var calls atomic.Int32
	tt := e.s.newTrashTimer()
	tt.purge = func(context.Context, time.Time) ([]core.PurgeResult, error) {
		calls.Add(1)
		return res, nil
	}
	return tt, &calls
}

func TestTrashTimerSkipsWhenBusy(t *testing.T) {
	e := apiNewEnv(t)
	tt, calls := trashTestTimer(e, nil)
	if !e.s.api.claim() {
		t.Fatal("claim")
	}
	if tt.round(context.Background()) {
		t.Fatal("đang có job mà vòng dọn vẫn chạy")
	}
	if calls.Load() != 0 {
		t.Fatal("đang có job mà vẫn gọi purge")
	}
	// Vòng bị bỏ không được nhả chỗ của job đang chạy.
	if e.s.api.claim() {
		t.Fatal("vòng dọn bị bỏ đã nhả chỗ chạy job của người khác")
	}
	e.s.api.release()

	// Khoá toàn cục bị tiến trình khác giữ (CLI): cũng bỏ, và nhả chỗ đã giành.
	unlock, err := e.f.Lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tt.round(context.Background()) || calls.Load() != 0 {
		t.Fatal("khoá bị giữ mà vòng dọn vẫn chạy")
	}
	unlock()
	if !e.s.api.claim() {
		t.Fatal("vòng bị bỏ vì khoá mà chưa nhả chỗ chạy job")
	}
	e.s.api.release()
}

func TestTrashTimerRoundAudits(t *testing.T) {
	e := apiNewEnv(t)
	tt, calls := trashTestTimer(e, []core.PurgeResult{
		{Domain: apiDomain(t, "a.example.com"), Report: engine.Report{Job: engine.Job{ID: "job-a", Status: engine.JobSucceeded}}},
		{Domain: apiDomain(t, "b.example.com"), Report: engine.Report{Job: engine.Job{ID: "job-b", Status: engine.JobRolledBack}}},
		{Domain: apiDomain(t, "c.example.com"), Error: "không dựng được Plan"},
	})
	// purge chạy trong lúc giữ chỗ chạy job.
	inner := tt.purge
	tt.purge = func(ctx context.Context, now time.Time) ([]core.PurgeResult, error) {
		if e.s.api.claim() {
			t.Error("purge chạy mà không giữ chỗ chạy job")
		}
		if !now.Equal(e.clock()) {
			t.Errorf("now = %v, muốn đồng hồ của server %v", now, e.clock())
		}
		return inner(ctx, now)
	}
	if !tt.round(context.Background()) || calls.Load() != 1 {
		t.Fatalf("vòng không chạy (purge gọi %d lần)", calls.Load())
	}
	a := hAudit(t, e, auditPurgeExpired)
	if len(a) != 3 ||
		a[0].Target != "a.example.com" || a[0].Result != "ok" || a[0].JobID != "job-a" ||
		a[1].Result != string(engine.JobRolledBack) || a[1].JobID != "job-b" ||
		a[2].Result != "error" || a[2].JobID != "" {
		t.Fatalf("nhật ký = %+v", a)
	}
	for _, ip := range trashAuditIP(t, e, auditPurgeExpired) {
		if ip != auditIPTimer {
			t.Fatalf("ip = %q, muốn %q", ip, auditIPTimer)
		}
	}
	if !e.s.api.claim() {
		t.Fatal("vòng xong mà chưa nhả chỗ chạy job")
	}
	e.s.api.release()
}

// Run: vòng đầu sau first, rồi mỗi every; dừng khi ctx huỷ.
func TestTrashTimerRun(t *testing.T) {
	e := apiNewEnv(t)
	tt, calls := trashTestTimer(e, nil)
	tt.first, tt.every = time.Millisecond, time.Millisecond
	rounds := make(chan bool, 16)
	tt.onRound = func(ran bool) {
		select {
		case rounds <- ran:
		default:
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); tt.Run(ctx) }()
	for i := range 3 {
		select {
		case ran := <-rounds:
			if !ran {
				t.Fatalf("vòng %d bị bỏ dù không có job", i)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("chỉ có %d vòng", i)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run không dừng khi ctx huỷ")
	}
	if calls.Load() < 3 {
		t.Fatalf("purge gọi %d lần", calls.Load())
	}

	// Khoảng mặc định: mỗi giờ, vòng đầu vài phút sau khi khởi động.
	def := e.s.newTrashTimer()
	if def.every != time.Hour || def.first <= 0 || def.first > 10*time.Minute || def.purge == nil {
		t.Fatalf("mặc định: first %v, every %v", def.first, def.every)
	}
}

// ---------------------------------------------------------------- qua core thật

// Các test dưới gọi thân core của đợt 2 (thùng rác, apply).

func TestAPITrashSettings(t *testing.T) {
	e := apiNewEnv(t)
	ov := apiDecode[TrashOverview](t, apiCall(e.s.handleTrash, "GET", "/api/v1/trash", ""), 200)
	if ov.Days != core.DefaultTrashDays || ov.Sites == nil || len(ov.Sites) != 0 {
		t.Fatalf("chưa đặt gì: %+v", ov)
	}
	if !strings.Contains(apiCall(e.s.handleTrash, "GET", "/", "").Body.String(), `"sites":[]`) {
		t.Error("thùng rác rỗng phải ra [] chứ không phải null")
	}

	ov = apiDecode[TrashOverview](t, apiCall(e.s.handleTrashSettings, "POST", "/", `{"days":3}`), 200)
	if ov.Days != 3 {
		t.Fatalf("days = %d sau khi đặt 3", ov.Days)
	}
	for _, bad := range []string{`{"days":-1}`, fmt.Sprintf(`{"days":%d}`, core.MaxTrashDays+1)} {
		if got := apiErrCode(t, apiCall(e.s.handleTrashSettings, "POST", "/", bad), 400); got != "trash_days_invalid" {
			t.Errorf("%s: code = %q", bad, got)
		}
	}
	if got := apiErrCode(t, apiCall(e.s.handleTrashSettings, "POST", "/", `{"days":"7"}`), 400); got != "bad_request" {
		t.Errorf("days là chuỗi: code = %q", got)
	}
	a := hAudit(t, e, "trash.settings")
	if len(a) != 3 || a[0].Target != "3" || a[0].Result != "ok" || a[1].Result != "trash_days_invalid" {
		t.Fatalf("nhật ký = %+v", a)
	}
}

func TestAPITrashList(t *testing.T) {
	e := apiNewEnv(t)
	e.addSite(t, "live.example.com")
	e.trashSite(t, "old.example.com")
	ov := apiDecode[struct {
		Days  int           `json:"days"`
		Sites []apiSiteJSON `json:"sites"`
	}](t, apiCall(e.s.handleTrash, "GET", "/api/v1/trash", ""), 200)
	if len(ov.Sites) != 1 || ov.Sites[0].Domain != "old.example.com" || ov.Sites[0].Status != string(site.Trashed) {
		t.Fatalf("thùng rác = %+v", ov)
	}
}

// DELETE /sites/{domain} giờ là đưa vào thùng rác: kind site.rm, nhật ký site.rm.
func TestAPISiteDeleteTrashes(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	d := e.addSite(t, "old.example.com")

	p := apiDecode[engine.Preview](t, apiCall(e.s.handleSiteDelete, "DELETE", "/?dry_run=1", "", "domain", "old.example.com"), 200)
	if p.Kind != "site.rm" || len(p.Steps) == 0 {
		t.Fatalf("Preview = %+v", p)
	}
	if st, ok, _ := e.st.Site(context.Background(), d); !ok || st.Status != site.Active {
		t.Fatal("xem trước mà site đã đổi")
	}

	js := apiDecode[JobStarted](t, apiCall(e.s.handleSiteDelete, "DELETE", "/", "", "domain", "old.example.com"), 202)
	e.s.api.wg.Wait()
	if k := trashJobKind(t, e, js.JobID); k != "site.rm" {
		t.Fatalf("kind = %q", k)
	}
	if a := hAudit(t, e, "site.rm"); len(a) != 1 || a[0].JobID != js.JobID || a[0].Target != "old.example.com" {
		t.Fatalf("nhật ký = %+v", a)
	}
	// Dù job ra sao, site không bao giờ bị xoá khỏi bảng sites: đưa vào thùng
	// rác thì dòng ở lại (trashed), job hỏng thì vẫn active.
	if _, ok, _ := e.st.Site(context.Background(), d); !ok {
		t.Fatal("DELETE /sites xoá mất dòng của site (phải là thùng rác)")
	}
}

func TestAPISiteRestore(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	e.addSite(t, "live.example.com")
	e.trashSite(t, "old.example.com")

	for range 2 { // lần hai vẫn 409: lời từ chối phải nhả chỗ chạy job
		if got := apiErrCode(t, apiCall(e.s.handleSiteRestore, "POST", "/", "", "domain", "live.example.com"), 409); got != "site_not_trashed" {
			t.Fatalf("site active: code = %q", got)
		}
	}
	if got := apiErrCode(t, apiCall(e.s.handleSiteRestore, "POST", "/", "", "domain", "none.example.com"), 404); got != "site_not_found" {
		t.Errorf("không có site: code = %q", got)
	}
	if got := apiErrCode(t, apiCall(e.s.handleSiteRestore, "POST", "/", "", "domain", "a.com;id"), 400); got != validate.CodeDomainInvalid {
		t.Errorf("domain sai: code = %q", got)
	}

	p := apiDecode[engine.Preview](t, apiCall(e.s.handleSiteRestore, "POST", "/?dry_run=1", "", "domain", "old.example.com"), 200)
	if p.Kind != "site.restore" || len(p.Steps) == 0 {
		t.Fatalf("Preview = %+v", p)
	}
	if !e.s.api.claim() {
		t.Fatal("claim")
	}
	if got := apiErrCode(t, apiCall(e.s.handleSiteRestore, "POST", "/", "", "domain", "old.example.com"), 409); got != "job_running" {
		t.Fatalf("đang có job: code = %q", got)
	}
	e.s.api.release()

	js := apiDecode[JobStarted](t, apiCall(e.s.handleSiteRestore, "POST", "/", "", "domain", "old.example.com"), 202)
	e.s.api.wg.Wait()
	if k := trashJobKind(t, e, js.JobID); k != "site.restore" {
		t.Fatalf("kind = %q", k)
	}
	a := hAudit(t, e, "site.restore")
	// site_not_trashed hai lần, site_not_found (lời từ chối sau khi giành chỗ
	// cũng ghi nhật ký), job_running, rồi job thật. Domain sai (400) thì không.
	if len(a) != 5 || a[0].Result != "site_not_trashed" || a[2].Result != "site_not_found" ||
		a[3].Result != "job_running" || a[4].JobID != js.JobID {
		t.Fatalf("nhật ký = %+v", a)
	}
}

func TestAPITrashPurge(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	e.addSite(t, "live.example.com")
	e.trashSite(t, "old.example.com")

	if got := apiErrCode(t, apiCall(e.s.handleTrashPurge, "DELETE", "/", "", "domain", "live.example.com"), 409); got != "site_not_trashed" {
		t.Fatalf("site active: code = %q", got)
	}
	if got := apiErrCode(t, apiCall(e.s.handleTrashPurge, "DELETE", "/", "", "domain", "../../etc/passwd"), 400); got != validate.CodeDomainInvalid {
		t.Errorf("domain sai: code = %q", got)
	}
	p := apiDecode[engine.Preview](t, apiCall(e.s.handleTrashPurge, "DELETE", "/?dry_run=1", "", "domain", "old.example.com"), 200)
	if p.Kind != "site.purge" || len(p.Steps) == 0 {
		t.Fatalf("Preview = %+v", p)
	}
	js := apiDecode[JobStarted](t, apiCall(e.s.handleTrashPurge, "DELETE", "/", "", "domain", "old.example.com"), 202)
	e.s.api.wg.Wait()
	if k := trashJobKind(t, e, js.JobID); k != "site.purge" {
		t.Fatalf("kind = %q", k)
	}
	if a := hAudit(t, e, "site.purge"); len(a) != 2 || a[0].Result != "site_not_trashed" || a[1].JobID != js.JobID {
		t.Fatalf("nhật ký = %+v", a)
	}
}

func TestAPIApply(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	e.addSite(t, "live.example.com")

	// Mọi dòng đã có: xem trước vẫn 200 (giao diện cần Items), chạy thì 409.
	const skipOnly = `{"sites":[{"domain":"live.example.com"}]}`
	pv := apiDecode[core.ApplyPreview](t, apiCall(e.s.handleApply, "POST", "/?dry_run=1", skipOnly), 200)
	if len(pv.Items) != 1 || pv.Items[0].Action != core.ApplySkip || pv.Preview != nil {
		t.Fatalf("xem trước khi không có gì để làm = %+v", pv)
	}
	if got := apiErrCode(t, apiCall(e.s.handleApply, "POST", "/", skipOnly), 409); got != "nothing_to_do" {
		t.Fatalf("code = %q", got)
	}
	if jobs, _ := e.st.Jobs(context.Background(), 0); len(jobs) != 0 {
		t.Fatalf("không có gì để làm mà vẫn có job: %v", jobs)
	}

	const one = `{"defaults":{"admin_email":"a@example.com"},"sites":[{"domain":"live.example.com"},{"domain":"new.example.com","title":"Mới"}]}`
	pv = apiDecode[core.ApplyPreview](t, apiCall(e.s.handleApply, "POST", "/?dry_run=1", one), 200)
	if len(pv.Items) != 2 || pv.Preview == nil || len(pv.Preview.Steps) == 0 {
		t.Fatalf("xem trước = %+v", pv)
	}
	actions := map[string]core.ApplyAction{}
	for _, it := range pv.Items {
		actions[it.Domain] = it.Action
	}
	if actions["live.example.com"] != core.ApplySkip || actions["new.example.com"] != core.ApplyCreate {
		t.Fatalf("việc từng dòng = %v", actions)
	}
	if jobs, _ := e.st.Jobs(context.Background(), 0); len(jobs) != 0 {
		t.Fatalf("xem trước mà có job: %v", jobs)
	}

	if !e.s.api.claim() {
		t.Fatal("claim")
	}
	if got := apiErrCode(t, apiCall(e.s.handleApply, "POST", "/", one), 409); got != "job_running" {
		t.Fatalf("đang có job: code = %q", got)
	}
	e.s.api.release()

	js := apiDecode[JobStarted](t, apiCall(e.s.handleApply, "POST", "/", one), 202)
	e.s.api.wg.Wait()
	trashJobKind(t, e, js.JobID)
	a := hAudit(t, e, "apply")
	if len(a) != 3 || a[0].Result != "nothing_to_do" || a[0].Target != "1 site" ||
		a[1].Result != "job_running" || a[2].JobID != js.JobID || a[2].Target != "2 site" {
		t.Fatalf("nhật ký = %+v", a)
	}
	if !e.s.api.claim() {
		t.Fatal("job xong mà chưa nhả chỗ chạy job")
	}
	e.s.api.release()
}

// Lời từ chối cả lô (stack chưa cài) về thẳng người gọi và nhả chỗ chạy job.
func TestAPIApplyRefused(t *testing.T) {
	e := apiNewEnv(t)
	const one = `{"defaults":{"admin_email":"a@example.com"},"sites":[{"domain":"new.example.com"}]}`
	for range 2 {
		rec := apiCall(e.s.handleApply, "POST", "/", one)
		if rec.Code != 409 {
			t.Fatalf("mã = %d: %s", rec.Code, rec.Body.String())
		}
	}
	if !e.s.api.claim() {
		t.Fatal("lời từ chối không nhả chỗ chạy job")
	}
	e.s.api.release()
}
