package server

// Test của API M9 (api_m9.go). Đi qua core thật trên FakeSystem: kiểm hợp
// đồng HTTP (mã, Preview, kind của job, nhật ký), không đòi job thành công,
// vì FakeSystem ở đây không giả lập mariadb-dump hay tar (việc đó là test của
// core và của demo). Job chạy thật hỏng ngay trong Apply, trước kiểm chứng.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// m9Machine cho FakeSystem trả lời các lệnh đo mà Prepare* của backup và DB
// gọi: cỡ DB (câu information_schema.TABLES qua stdin của mariadb), du của cây
// public, sha256sum tính trên nội dung file giả. Câu SQL khác trả như stack().
func (e *apiEnv) m9Machine(t *testing.T) {
	t.Helper()
	e.stack(t)
	e.f.Handle([]string{"mariadb", "-N", "-B"}, func(c system.Cmd) system.Result {
		sql := string(c.Stdin)
		if strings.Contains(sql, "information_schema.TABLES") {
			return system.Result{Stdout: "1048576\n"}
		}
		// M34: câu "DB và user DB có trong MariaDB không" đi theo bảng sites:
		// site có trong tpan thì DB và user DB của nó có. Trả "0 0" cho mọi
		// tên (như trước) là nói mọi DB đã bị xoá ngoài tpan, và các thao tác
		// sao lưu, khôi phục, nhập ở đây sẽ đi nhánh của M34.
		if strings.Contains(sql, "mysql.global_priv") && strings.Contains(sql, "SCHEMATA") {
			sites, _ := e.st.Sites(context.Background())
			for _, st := range sites {
				if strings.Contains(sql, "SCHEMA_NAME='"+st.Layout().DB+"'") {
					return system.Result{Stdout: "1\t1\n"}
				}
			}
		}
		return system.Result{Stdout: "0\t0\n"}
	})
	e.f.Handle([]string{"du"}, func(c system.Cmd) system.Result {
		return system.Result{Stdout: "4096\t" + c.Args[len(c.Args)-1] + "\n"}
	})
	e.f.Handle([]string{"sha256sum"}, func(c system.Cmd) system.Result {
		p := c.Args[len(c.Args)-1]
		data, err := e.f.ReadFile(p)
		if err != nil {
			return system.Result{ExitCode: 1, Stderr: "sha256sum: " + p + ": No such file or directory\n"}
		}
		sum := sha256.Sum256(data)
		return system.Result{Stdout: hex.EncodeToString(sum[:]) + "  " + p + "\n"}
	})
}

// m9Site là một site active có cây public trên máy giả.
func (e *apiEnv) m9Site(t *testing.T, dom string) site.Layout {
	t.Helper()
	l := site.LayoutOf(e.addSite(t, dom))
	e.f.SetDir(l.Public, 0o2750, system.Owner{User: l.User, Group: site.WebGroup})
	return l
}

// m9Backup ghi một file backup giả vào thư mục backup của site và dòng của nó
// vào store; trả dòng đã ghi (có id).
func (e *apiEnv) m9Backup(t *testing.T, l site.Layout, kind core.BackupKind, name string, ago time.Duration) core.Backup {
	t.Helper()
	p := site.BackupDir(l) + "/" + name
	data := []byte("backup giả " + name + "\n")
	e.f.SetFile(p, data, 0o600, system.Root)
	sum := sha256.Sum256(data)
	b, err := e.st.AddBackup(context.Background(), core.Backup{
		Domain: l.Domain.String(), Kind: kind, Reason: core.ReasonManual,
		Path: p, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:]),
		Created: e.now.Add(-ago),
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// m9Job đọc job vừa chạy (sau wg.Wait) và đòi đúng kind, target.
func m9Job(t *testing.T, e *apiEnv, id, kind, target string) {
	t.Helper()
	job, err := e.st.Job(context.Background(), id)
	if err != nil || job.Kind != kind || job.Target != target || job.Status == engine.JobRunning {
		t.Fatalf("job %s = %+v, %v; muốn kind %s, target %s, đã xong", id, job, err, kind, target)
	}
}

// m9NoJobs đòi chưa có job nào (xem trước và lời từ chối không chạy gì).
func m9NoJobs(t *testing.T, e *apiEnv) {
	t.Helper()
	if jobs, _ := e.st.Jobs(context.Background(), 0); len(jobs) != 0 {
		t.Fatalf("có job dù chỉ xem trước hoặc bị từ chối: %+v", jobs)
	}
}

func TestM9RoutesNeedSession(t *testing.T) {
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/v1/backups"},
		{"POST", "/api/v1/sites/a.example.com/backups"},
		{"POST", "/api/v1/backups/0123456789abcdef/restore"},
		{"POST", "/api/v1/backups/settings"},
		{"GET", "/api/v1/cron"},
		{"POST", "/api/v1/databases/a.example.com/password"},
		{"POST", "/api/v1/databases/a.example.com/dump"},
		{"POST", "/api/v1/databases/a.example.com/import"},
	} {
		rec := httptest.NewRecorder()
		testHandler().ServeHTTP(rec, httptest.NewRequest(c.method, c.path, strings.NewReader("{}")))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: mã = %d, muốn 401", c.method, c.path, rec.Code)
		}
	}
}

// ---------------------------------------------------------------- GET /backups

func TestAPIBackupsList(t *testing.T) {
	e := apiNewEnv(t)
	e.m9Machine(t)

	rec := apiCall(e.s.handleBackups, "GET", "/api/v1/backups", "")
	ov := apiDecode[BackupsOverview](t, rec, 200)
	if ov.Settings != (core.BackupSettings{Keep: core.DefaultBackupKeep}) || len(ov.Items) != 0 {
		t.Fatalf("máy chưa có backup: %+v", ov)
	}
	if !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Errorf("danh sách rỗng phải ra [] chứ không phải null: %s", rec.Body.String())
	}
	if got := jsonKeys(t, rec.Body.Bytes()); !slices.Equal(got, []string{"items", "settings"}) {
		t.Errorf("khoá của BackupsOverview = %v", got)
	}

	blog := e.m9Site(t, "blog.example.com")
	shop := e.m9Site(t, "shop.example.com")
	old := e.m9Backup(t, blog, core.BackupFull, "blog.example.com-20261117-020000.tar.gz", 48*time.Hour)
	sql := e.m9Backup(t, blog, core.BackupSQL, "blog.example.com-db-20261118-020000.sql", 24*time.Hour)
	e.m9Backup(t, shop, core.BackupFull, "shop.example.com-20261119-020000.tar.gz", time.Hour)

	rec = apiCall(e.s.handleBackups, "GET", "/api/v1/backups?domain=blog.example.com", "")
	ov = apiDecode[BackupsOverview](t, rec, 200)
	if len(ov.Items) != 2 || ov.Items[0].ID != sql.ID || ov.Items[1].ID != old.ID {
		t.Fatalf("backup của blog (mới nhất trước) = %+v", ov.Items)
	}
	var raw struct {
		Items []json.RawMessage `json:"items"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	// Khớp kiểu Backup của web/src/api/types.ts (trường rỗng có omitempty).
	// M18 thêm local, remote (không omitempty: giao diện cần biết bản nằm ở
	// đâu); kiểu TypeScript thêm hai trường này ở phần giao diện của M18.
	if got := jsonKeys(t, raw.Items[0]); !slices.Equal(got, []string{"created", "domain", "id", "kind", "local", "location", "path", "reason", "remote", "sha256", "size"}) {
		t.Errorf("khoá của Backup = %v", got)
	}
	if ov := apiDecode[BackupsOverview](t, apiCall(e.s.handleBackups, "GET", "/api/v1/backups", ""), 200); len(ov.Items) != 3 {
		t.Errorf("mọi backup = %d dòng, muốn 3", len(ov.Items))
	}
	for _, q := range []string{"a.com%3B%20rm%20-rf%20%2F", "%24(id).example.com", "..%2F..%2Fetc%2Fpasswd"} {
		if got := apiErrCode(t, apiCall(e.s.handleBackups, "GET", "/api/v1/backups?domain="+q, ""), 400); got != validate.CodeDomainInvalid {
			t.Errorf("?domain=%s: code = %q", q, got)
		}
	}
}

// ---------------------------------------------------------------- tạo backup

func TestAPISiteBackup(t *testing.T) {
	e := apiNewEnv(t)
	e.m9Machine(t)
	e.m9Site(t, "blog.example.com")
	call := func(q, dom string) *httptest.ResponseRecorder {
		return apiCall(e.s.handleSiteBackup, "POST", "/api/v1/sites/x/backups"+q, "", "domain", dom)
	}

	p := apiDecode[engine.Preview](t, call("?dry_run=1", "blog.example.com"), 200)
	if p.Kind != "backup.create" || p.Target != "blog.example.com" || len(p.Steps) != 1 {
		t.Fatalf("Preview = %+v", p)
	}
	m9NoJobs(t, e)

	if got := apiErrCode(t, call("", "khong-co.example.com"), 404); got != "site_not_found" {
		t.Errorf("site không có: code = %q", got)
	}
	if got := apiErrCode(t, call("", "a.com; rm -rf /"), 400); got != validate.CodeDomainInvalid {
		t.Errorf("domain chèn lệnh: code = %q", got)
	}
	// Đĩa gần đầy: từ chối trước khi dựng Plan, cả khi xem trước.
	e.f.SetDisk("/", system.DiskUsage{Total: 10 << 20, Free: 1 << 20})
	for _, q := range []string{"?dry_run=1", ""} {
		if got := apiErrCode(t, call(q, "blog.example.com"), 409); got != core.CodeDiskSpaceLow {
			t.Errorf("%q đĩa đầy: code = %q", q, got)
		}
	}
	m9NoJobs(t, e)
	e.f.SetDisk("/", system.DiskUsage{Total: 100 << 30, Free: 60 << 30})

	js := apiDecode[JobStarted](t, call("", "blog.example.com"), 202)
	e.s.api.wg.Wait()
	m9Job(t, e, js.JobID, "backup.create", "blog.example.com")
	a := hAudit(t, e, "backup.create")
	if len(a) != 3 || a[0].Result != "site_not_found" || a[1].Result != core.CodeDiskSpaceLow ||
		a[2].JobID != js.JobID || a[2].Target != "blog.example.com" {
		t.Errorf("nhật ký backup.create = %+v", a)
	}

	// Đang có job thì 409 job_running.
	e.s.api.claim()
	defer e.s.api.release()
	if got := apiErrCode(t, call("", "blog.example.com"), 409); got != "job_running" {
		t.Fatalf("code = %q", got)
	}
}

// ---------------------------------------------------------------- khôi phục

func TestAPIBackupRestore(t *testing.T) {
	e := apiNewEnv(t)
	e.m9Machine(t)
	blog := e.m9Site(t, "blog.example.com")
	full := e.m9Backup(t, blog, core.BackupFull, "blog.example.com-20261118-020000.tar.gz", 24*time.Hour)
	sql := e.m9Backup(t, blog, core.BackupSQL, "blog.example.com-db-20261118-030000.sql", 23*time.Hour)
	call := func(q, id string) *httptest.ResponseRecorder {
		return apiCall(e.s.handleBackupRestore, "POST", "/api/v1/backups/x/restore"+q, "", "id", id)
	}

	p := apiDecode[engine.Preview](t, call("?dry_run=1", full.ID), 200)
	if p.Kind != "backup.restore" || p.Target != "blog.example.com" || len(p.Steps) < 5 {
		t.Fatalf("Preview = %+v", p)
	}
	m9NoJobs(t, e)

	// Id lạ: 404, không in lại, nhật ký ghi "?". Id đúng dạng mà không có: 404
	// kèm id. Xem trước không ghi nhật ký.
	for _, id := range []string{"a;rm -rf /", "../../etc/passwd", "ABCDEF", strings.Repeat("a", 65)} {
		rec := call("", id)
		if got := apiErrCode(t, rec, 404); got != "backup_not_found" {
			t.Errorf("id %q: code = %q", id, got)
		}
		if strings.Contains(rec.Body.String(), id) {
			t.Errorf("id lạ %q bị in lại: %s", id, rec.Body.String())
		}
	}
	if got := apiErrCode(t, call("?dry_run=1", "0123456789abcdef"), 404); got != "backup_not_found" {
		t.Errorf("xem trước id không có: code = %q", got)
	}
	if got := apiErrCode(t, call("", "0123456789abcdef"), 404); got != "backup_not_found" {
		t.Errorf("id không có: code = %q", got)
	}
	// Bản sql không khôi phục cả site được (core: backup_not_found).
	if got := apiErrCode(t, call("", sql.ID), 404); got != "backup_not_found" {
		t.Errorf("khôi phục bản sql: code = %q", got)
	}
	// File bị sửa sau lúc ghi: sha256 khác.
	e.f.SetFile(full.Path, []byte("đã bị sửa\n"), 0o600, system.Root)
	if got := apiErrCode(t, call("", full.ID), 409); got != "backup_corrupt" {
		t.Errorf("file hỏng: code = %q", got)
	}
	m9NoJobs(t, e)
	e.f.SetFile(full.Path, []byte("backup giả blog.example.com-20261118-020000.tar.gz\n"), 0o600, system.Root)

	js := apiDecode[JobStarted](t, call("", full.ID), 202)
	e.s.api.wg.Wait()
	m9Job(t, e, js.JobID, "backup.restore", "blog.example.com")

	// Site trong thùng rác: khôi phục site trước.
	old := site.LayoutOf(e.trashSite(t, "old.example.com"))
	ob := e.m9Backup(t, old, core.BackupFull, "old.example.com-20261110-020000.tar.gz", 9*24*time.Hour)
	if got := apiErrCode(t, call("", ob.ID), 409); got != "site_trashed" {
		t.Errorf("site trong thùng rác: code = %q", got)
	}

	a := hAudit(t, e, "backup.restore")
	want := []struct{ target, result string }{
		{"?", "backup_not_found"}, {"?", "backup_not_found"}, {"?", "backup_not_found"}, {"?", "backup_not_found"},
		{"0123456789abcdef", "backup_not_found"},
		{"blog.example.com", "backup_not_found"},
		{"blog.example.com", "backup_corrupt"},
		{"blog.example.com", "ok"},
		{"old.example.com", "site_trashed"},
	}
	if len(a) != len(want) {
		t.Fatalf("nhật ký backup.restore có %d dòng, muốn %d: %+v", len(a), len(want), a)
	}
	for i, w := range want {
		if a[i].Target != w.target || (w.result != "ok" && a[i].Result != w.result) {
			t.Errorf("nhật ký %d = %+v, muốn %+v", i, a[i], w)
		}
	}
	if a[7].JobID != js.JobID {
		t.Errorf("nhật ký của job khôi phục không có job id: %+v", a[7])
	}
}

// ---------------------------------------------------------------- cài đặt

func TestAPIBackupSettings(t *testing.T) {
	e := apiNewEnv(t)
	// Máy Ubuntu luôn có /etc/cron.d (gói cron).
	e.f.SetDir("/etc/cron.d", 0o755, system.Root)
	call := func(q, body string) *httptest.ResponseRecorder {
		return apiCall(e.s.handleBackupSettings, "POST", "/api/v1/backups/settings"+q, body)
	}
	for _, c := range []struct{ body, code string }{
		{`{`, "bad_request"},
		{`{"keep":3,"at":"02:30","x":1}`, "bad_request"},
		{`{"keep":"3","at":"02:30"}`, "bad_request"},
		{`{"keep":0,"at":"02:30"}`, "backup_keep_invalid"},
		{`{"keep":31,"at":"02:30"}`, "backup_keep_invalid"},
		{`{"keep":3,"at":"25:00"}`, "backup_at_invalid"},
		{`{"keep":3,"at":"02:30\n* * * * * root id"}`, "backup_at_invalid"},
	} {
		for _, q := range []string{"?dry_run=1", ""} {
			if got := apiErrCode(t, call(q, c.body), 400); got != c.code {
				t.Errorf("%s %s: code = %q, muốn %s", q, c.body, got, c.code)
			}
		}
	}
	m9NoJobs(t, e)

	p := apiDecode[engine.Preview](t, call("?dry_run=1", `{"keep":5,"at":"02:30"}`), 200)
	if p.Kind != "backup.settings" || len(p.Steps) != 1 {
		t.Fatalf("Preview = %+v", p)
	}
	m9NoJobs(t, e)

	js := apiDecode[JobStarted](t, call("", `{"keep":5,"at":"02:30"}`), 202)
	e.s.api.wg.Wait()
	m9Job(t, e, js.JobID, "backup.settings", "backup")
	if job, _ := e.st.Job(context.Background(), js.JobID); job.Status != engine.JobSucceeded {
		t.Fatalf("job cài đặt = %+v", job)
	}
	ov := apiDecode[BackupsOverview](t, apiCall(e.s.handleBackups, "GET", "/", ""), 200)
	if ov.Settings != (core.BackupSettings{Keep: 5, At: "02:30"}) {
		t.Errorf("sau khi đặt: %+v", ov.Settings)
	}
	if _, err := e.f.Stat(site.BackupCronFile); err != nil {
		t.Errorf("chưa có file cron của lịch backup: %v", err)
	}

	a := hAudit(t, e, "backup.settings")
	if n := len(a); n != 5 || a[n-1].JobID != js.JobID || a[n-1].Target != `keep=5 at="02:30"` {
		t.Fatalf("nhật ký backup.settings = %+v", a)
	}
	// Ký tự xuống dòng của người gọi không vào nhật ký nguyên dạng.
	for _, x := range a {
		if strings.ContainsAny(x.Target, "\n\r") {
			t.Errorf("nhật ký có xuống dòng: %q", x.Target)
		}
	}
}

// ---------------------------------------------------------------- cron

func TestAPICron(t *testing.T) {
	e := apiNewEnv(t)
	e.m9Machine(t)
	blog := e.m9Site(t, "blog.example.com")
	e.trashSite(t, "old.example.com")
	e.f.SetFile(site.CronFile(blog), []byte("*/5 * * * * root /usr/local/bin/tpan cron run blog.example.com\n"), 0o644, system.Root)
	run := core.CronRun{Domain: "blog.example.com", Started: e.now.Add(-time.Minute), Duration: 1500 * time.Millisecond, Status: core.CronFail, ExitCode: 1, Output: "PHP Fatal error"}
	if err := e.st.SaveCronRun(context.Background(), run); err != nil {
		t.Fatal(err)
	}

	rec := apiCall(e.s.handleCron, "GET", "/api/v1/cron", "")
	ov := apiDecode[core.CronOverview](t, rec, 200)
	if len(ov.Sites) != 2 {
		t.Fatalf("Sites = %+v", ov.Sites)
	}
	by := map[string]core.CronEntry{}
	for _, s := range ov.Sites {
		by[s.Domain] = s
	}
	b, o := by["blog.example.com"], by["old.example.com"]
	if !b.Enabled || b.Last == nil || b.Last.Status != core.CronFail || b.Last.Output != "PHP Fatal error" || b.Schedule != core.CronEvery {
		t.Errorf("blog = %+v", b)
	}
	if o.Enabled || o.Last != nil {
		t.Errorf("site trong thùng rác = %+v", o)
	}
	if ov.Backup.Keep != core.DefaultBackupKeep || ov.BackupFile != "" {
		t.Errorf("lịch backup = %+v, file %q", ov.Backup, ov.BackupFile)
	}
	// Khớp CronOverview, CronEntry, CronRun của web/src/api/types.ts.
	if got := jsonKeys(t, rec.Body.Bytes()); !slices.Equal(got, []string{"backup", "sites"}) {
		t.Errorf("khoá của CronOverview = %v", got)
	}
	var raw struct {
		Sites []json.RawMessage `json:"sites"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	for _, s := range raw.Sites {
		var one struct {
			Domain string          `json:"domain"`
			Last   json.RawMessage `json:"last"`
		}
		_ = json.Unmarshal(s, &one)
		if one.Domain != "blog.example.com" {
			continue
		}
		if got := jsonKeys(t, s); !slices.Equal(got, []string{"command", "domain", "enabled", "file", "last", "schedule"}) {
			t.Errorf("khoá của CronEntry = %v", got)
		}
		if got := jsonKeys(t, one.Last); !slices.Equal(got, []string{"domain", "duration_ns", "exit_code", "output", "started", "status"}) {
			t.Errorf("khoá của CronRun = %v", got)
		}
	}

	// Máy không có site nào: [] chứ không null.
	e2 := apiNewEnv(t)
	if body := apiCall(e2.s.handleCron, "GET", "/", "").Body.String(); !strings.Contains(body, `"sites":[]`) {
		t.Errorf("không site: %s", body)
	}
}

// ---------------------------------------------------------------- thao tác DB

func TestAPIDBOps(t *testing.T) {
	e := apiNewEnv(t)
	e.m9Machine(t)
	blog := e.m9Site(t, "blog.example.com")
	shop := e.m9Site(t, "shop.example.com")
	sql := e.m9Backup(t, blog, core.BackupSQL, "blog.example.com-db-20261118-020000.sql", 24*time.Hour)
	full := e.m9Backup(t, blog, core.BackupFull, "blog.example.com-20261118-030000.tar.gz", 23*time.Hour)
	shopSQL := e.m9Backup(t, shop, core.BackupSQL, "shop.example.com-db-20261118-020000.sql", 24*time.Hour)
	e.trashSite(t, "old.example.com")

	type op struct {
		action string
		h      http.HandlerFunc
		body   string
		kind   string
	}
	ops := []op{
		{"db.passwd", e.s.handleDBPassword, "", "db.passwd"},
		{"db.dump", e.s.handleDBDump, "", "db.dump"},
		{"db.import", e.s.handleDBImport, `{"backup_id":"` + sql.ID + `"}`, "db.import"},
	}
	call := func(o op, q, dom, body string) *httptest.ResponseRecorder {
		return apiCall(o.h, "POST", "/api/v1/databases/x/x"+q, body, "domain", dom)
	}
	for _, o := range ops {
		t.Run(o.action, func(t *testing.T) {
			p := apiDecode[engine.Preview](t, call(o, "?dry_run=1", "blog.example.com", o.body), 200)
			if p.Kind != o.kind || p.Target != "blog.example.com" || len(p.Steps) == 0 {
				t.Fatalf("Preview = %+v", p)
			}
			if got := apiErrCode(t, call(o, "", "a.com; rm -rf /", o.body), 400); got != validate.CodeDomainInvalid {
				t.Errorf("domain chèn lệnh: code = %q", got)
			}
			if got := apiErrCode(t, call(o, "", "khong-co.example.com", o.body), 404); got != "site_not_found" {
				t.Errorf("site không có: code = %q", got)
			}
			js := apiDecode[JobStarted](t, call(o, "", "blog.example.com", o.body), 202)
			e.s.api.wg.Wait()
			m9Job(t, e, js.JobID, o.kind, "blog.example.com")
			a := hAudit(t, e, o.action)
			if len(a) != 2 || a[0].Result != "site_not_found" || a[1].JobID != js.JobID || a[1].Target != "blog.example.com" {
				t.Errorf("nhật ký %s = %+v", o.action, a)
			}
		})
	}

	// Đổi mật khẩu và nhập chỉ cho site active; xuất nhận cả site trong thùng rác.
	if got := apiErrCode(t, call(ops[0], "", "old.example.com", ""), 409); got != "site_trashed" {
		t.Errorf("đổi mật khẩu site trong thùng rác: code = %q", got)
	}
	if p := apiDecode[engine.Preview](t, call(ops[1], "?dry_run=1", "old.example.com", ""), 200); p.Kind != "db.dump" {
		t.Errorf("xuất DB site trong thùng rác: %+v", p)
	}

	// Nhập: chỉ bản sql của đúng site đó, sha256 khớp.
	imp := ops[2]
	for _, c := range []struct{ name, body, code string }{
		{"bản full", `{"backup_id":"` + full.ID + `"}`, "backup_not_found"},
		{"bản của site khác", `{"backup_id":"` + shopSQL.ID + `"}`, "backup_not_found"},
		{"id không có", `{"backup_id":"0123456789abcdef"}`, "backup_not_found"},
		{"id rỗng", `{}`, "backup_not_found"},
		{"id chèn lệnh", `{"backup_id":"x; rm -rf /"}`, "backup_not_found"},
	} {
		if got := apiErrCode(t, call(imp, "?dry_run=1", "blog.example.com", c.body), 404); got != c.code {
			t.Errorf("%s: code = %q, muốn %s", c.name, got, c.code)
		}
	}
	for _, body := range []string{`{`, `{"backup_id":1}`, `{"backup_id":"a","x":1}`} {
		if got := apiErrCode(t, call(imp, "", "blog.example.com", body), 400); got != "bad_request" {
			t.Errorf("%s: code = %q", body, got)
		}
	}
	e.f.SetFile(sql.Path, []byte("đã bị sửa\n"), 0o600, system.Root)
	if got := apiErrCode(t, call(imp, "", "blog.example.com", imp.body), 409); got != "backup_corrupt" {
		t.Errorf("bản xuất hỏng: code = %q", got)
	}
}
