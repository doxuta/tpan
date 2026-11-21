package server

// Test của API M18 (api_m18.go): không lộ khoá ở bất cứ đầu ra nào (B3), user
// thường không chạm được kho (B5), R1 (giữ khoá chỉ khi đích không đổi chỗ),
// R13 (file khoá hỏng không bị chép ra), cloud-init qua API. Đi qua core thật
// trên FakeSystem với kho giả s3.Mem.

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/s3"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

// Hai khoá đánh dấu: dài, duy nhất, để quét mọi đầu ra.
const (
	m18Access   = "AKIAM18KHONGLOTEST0001QZ"
	m18Secret   = "m18SecretKhongDuocLo-0123456789abcdefQZ"
	m18Endpoint = "https://s3.example.com"
	m18Bucket   = "tpan-thu"
	m18Prefix   = "tpan/may"
	m18User     = "usera"
)

// m18Env là apiEnv với Server đầy đủ (newServer: có phần đăng nhập, để gọi qua
// guard), kho giả, khoá lượt đẩy trong bộ nhớ, và sổ đếm số lần dựng kho theo
// endpoint (R1).
type m18Env struct {
	*apiEnv
	mem *s3.Mem
	bmu sync.Mutex
	// builds đếm số lần core dựng kho cho từng endpoint.
	builds map[string]int
}

func newM18Env(t *testing.T) *m18Env {
	t.Helper()
	e := &m18Env{apiEnv: apiNewEnv(t), mem: s3.NewMem(), builds: map[string]int{}}
	e.m9Machine(t)
	e.s = newServer(e.s.deps)
	e.s.api.lockWait = 100 * time.Millisecond
	e.s.remoteLock = system.NewMemLock()
	e.s.newS3Store = func(cfg s3.Config) (steps.ObjectStore, error) {
		e.bmu.Lock()
		e.builds[cfg.Endpoint]++
		e.bmu.Unlock()
		return e.mem, nil
	}
	t.Cleanup(e.s.api.wg.Wait)
	return e
}

func (e *m18Env) built(ep string) int {
	e.bmu.Lock()
	defer e.bmu.Unlock()
	return e.builds[ep]
}

// call gọi thẳng handler h với danh tính id (không qua guard).
func m18Call(h http.HandlerFunc, id account.Identity, method, target, body string, pathValues ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.RemoteAddr = "192.0.2.10:40000"
	r = r.WithContext(context.WithValue(r.Context(), identityKey{}, id))
	for i := 0; i+1 < len(pathValues); i += 2 {
		r.SetPathValue(pathValues[i], pathValues[i+1])
	}
	rec := httptest.NewRecorder()
	h(rec, r)
	return rec
}

var m18UserID = account.Identity{Name: m18User, Role: account.RoleUser}

func targetBody(endpoint, access, secret string) string {
	b, _ := json.Marshal(TargetRequest{Endpoint: endpoint, Bucket: m18Bucket, Prefix: m18Prefix, AccessKey: access, SecretKey: secret})
	return string(b)
}

// setTargetAPI đặt đích qua API (chạy thật) và chờ job xong.
func (e *m18Env) setTargetAPI(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	rec := apiCall(e.s.handleBackupTargetSet, "POST", "/api/v1/backups/target", targetBody(m18Endpoint, m18Access, m18Secret))
	js := apiDecode[JobStarted](t, rec, http.StatusAccepted)
	e.s.api.wg.Wait()
	job, err := e.st.Job(context.Background(), js.JobID)
	if err != nil || job.Kind != "backup.target.set" || job.Status != engine.JobSucceeded {
		evs, _ := e.st.Events(context.Background(), js.JobID, 0)
		t.Fatalf("job đặt đích = %+v, %v; sự kiện: %+v", job, err, evs)
	}
	return rec
}

// syncNow chạy một vòng của bộ đẩy nền ngay (không chờ nhịp).
func (e *m18Env) syncNow() {
	e.s.pusher.round(context.Background(), syncFunc(func(ctx context.Context, opt core.SyncOptions, onEvent func(engine.Event)) (core.SyncResult, error) {
		return e.s.apiRemote().Sync(ctx, opt, onEvent)
	}))
}

// m18Scan đòi data (thân câu trả lời, dòng DB…) không chứa chuỗi nào trong
// secrets: nguyên văn, và sau khi giải base64 mọi chuỗi trong JSON (Preview
// mang nội dung file dạng base64 ở old, new).
func m18Scan(t *testing.T, where string, data []byte, secrets ...string) {
	t.Helper()
	check := func(s, how string) {
		for i, sec := range secrets {
			if strings.Contains(s, sec) {
				t.Errorf("%s: %s chứa bí mật số %d", where, how, i)
			}
		}
	}
	check(string(data), "nguyên văn")
	var v any
	if json.Unmarshal(data, &v) != nil {
		return
	}
	var walk func(any)
	walk = func(x any) {
		switch x := x.(type) {
		case string:
			for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
				if b, err := enc.DecodeString(x); err == nil {
					check(string(b), "base64")
				}
			}
		case []any:
			for _, y := range x {
				walk(y)
			}
		case map[string]any:
			for _, y := range x {
				walk(y)
			}
		}
	}
	walk(v)
}

// m18ScanDB quét mọi ô của mọi bảng trong tpan.db (settings, audit, jobs,
// job_events, backups…).
func m18ScanDB(t *testing.T, dbPath string, secrets ...string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type = 'table'")
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, n)
	}
	_ = rows.Close()
	for _, tb := range tables {
		rs, err := db.Query(`SELECT * FROM "` + tb + `"`)
		if err != nil {
			t.Fatal(err)
		}
		cols, _ := rs.Columns()
		var cells []string
		for rs.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rs.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			for _, v := range vals {
				if b, ok := v.([]byte); ok {
					v = string(b)
				}
				cells = append(cells, fmt.Sprint(v))
			}
		}
		_ = rs.Close()
		data, _ := json.Marshal(cells)
		m18Scan(t, "bảng "+tb, data, secrets...)
	}
}

// TestKhongLoKhoaS3 (B3): đặt đích qua API với hai khoá đánh dấu, rồi đi qua
// mọi đầu ra (GET, xem trước, câu lỗi, job và sự kiện, nhật ký, cả tpan.db):
// không chỗ nào có access key đầy đủ hay secret key. Kèm R1 và R13.
func TestKhongLoKhoaS3(t *testing.T) {
	e := newM18Env(t)
	ctx := context.Background()
	blog := e.m9Site(t, "blog.example.com")
	b1 := e.m9Backup(t, blog, core.BackupFull, "blog.example.com-20261119-020000.tar.gz", time.Hour)
	b2 := e.m9Backup(t, blog, core.BackupFull, "blog.example.com-20261118-020000.tar.gz", 25*time.Hour)
	var outs []struct {
		where string
		body  []byte
	}
	keep := func(where string, rec *httptest.ResponseRecorder) *httptest.ResponseRecorder {
		outs = append(outs, struct {
			where string
			body  []byte
		}{where, rec.Body.Bytes()})
		return rec
	}

	// Xem trước rồi chạy thật.
	pv := apiDecode[engine.Preview](t, keep("xem trước đặt đích", apiCall(e.s.handleBackupTargetSet, "POST",
		"/api/v1/backups/target?dry_run=1", targetBody(m18Endpoint, m18Access, m18Secret))), 200)
	if pv.Kind != "backup.target.set" {
		t.Fatalf("Preview = %+v", pv)
	}
	keep("đặt đích", e.setTargetAPI(t))
	// Phép quét dưới không rỗng nghĩa: hai khoá thật sự đã tới máy, trong file
	// root 0600, và sổ đếm dựng kho có đếm.
	if data, _ := e.f.ReadFile(steps.S3KeysFile); !strings.Contains(string(data), m18Access) || !strings.Contains(string(data), m18Secret) {
		t.Fatalf("%s không có hai khoá vừa đặt", steps.S3KeysFile)
	}
	if e.built(m18Endpoint) == 0 {
		t.Fatal("sổ đếm dựng kho không đếm")
	}

	tv := apiDecode[core.TargetView](t, keep("GET target", apiCall(e.s.handleBackupTarget, "GET", "/api/v1/backups/target", "")), 200)
	if !tv.Configured || tv.AccessKeyTail != m18Access[len(m18Access)-4:] || tv.Endpoint != m18Endpoint {
		t.Errorf("TargetView = %+v", tv)
	}

	// Đẩy: xem trước, xếp hàng, rồi một vòng của bộ đẩy nền.
	keep("xem trước đẩy", apiCall(e.s.handleBackupPush, "POST", "/api/v1/backups/x/push?dry_run=1", "", "id", b1.ID))
	q := apiDecode[QueuedResponse](t, keep("đẩy", apiCall(e.s.handleBackupPush, "POST", "/api/v1/backups/x/push", "", "id", b1.ID)), http.StatusAccepted)
	if !q.Queued || q.Note.Vi == "" {
		t.Errorf("QueuedResponse = %+v", q)
	}
	e.syncNow()
	if row, _, _ := e.st.Backup(ctx, b1.ID); row.Remote != site.RemoteStored || row.RemoteKey == "" {
		t.Fatalf("sau lượt đẩy, dòng %s = %+v", b1.ID, row)
	}
	tid, _, err := core.CurrentTargetID(ctx, e.st)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.MarkBackupPushFailed(ctx, b2.ID, tid, "kho trả lỗi 503 SlowDown", false); err != nil {
		t.Fatal(err)
	}

	keep("xem trước tải về", apiCall(e.s.handleBackupPull, "POST", "/api/v1/backups/x/pull?dry_run=1", "", "id", b1.ID))
	keep("GET remote", apiCall(e.s.handleBackupRemote, "GET", "/api/v1/backups/remote", ""))
	keep("GET backups admin", apiCall(e.s.handleBackups, "GET", "/api/v1/backups", ""))
	keep("GET backups user", m18Call(e.s.handleBackups, m18UserID, "GET", "/api/v1/backups", ""))
	keep("xem trước gỡ đích", apiCall(e.s.handleBackupTargetRemove, "DELETE", "/api/v1/backups/target?dry_run=1", ""))
	jobs := apiDecode[[]engine.Job](t, keep("GET jobs", apiCall(e.s.handleJobs, "GET", "/api/v1/jobs", "")), 200)
	kinds := map[string]bool{}
	for _, j := range jobs {
		kinds[j.Kind] = true
		keep("GET jobs/"+j.ID, apiCall(e.s.handleJob, "GET", "/api/v1/jobs/x", "", "id", j.ID))
	}
	if !kinds["backup.target.set"] || !kinds["backup.push"] {
		t.Errorf("thiếu job đặt đích hoặc job đẩy: %v", kinds)
	}

	// Câu lỗi.
	for name, body := range map[string]string{
		"endpoint sai":    targetBody("ftp://s3.example.com", m18Access, m18Secret),
		"secret quá ngắn": targetBody(m18Endpoint, m18Access, "ngan"),
		"access sai dạng": targetBody(m18Endpoint, m18Access+" "+m18Secret, m18Secret),
	} {
		rec := keep("lỗi "+name, apiCall(e.s.handleBackupTargetSet, "POST", "/api/v1/backups/target", body))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: mã %d, muốn 400: %s", name, rec.Code, rec.Body)
		}
	}

	// R1: đích đã có; endpoint khác và hai khoá rỗng thì 400 s3_keys_invalid,
	// và kho của endpoint mới không bao giờ được dựng (không request nào ký
	// bằng khoá thật tới host do phiên admin chọn), cả lúc xem trước.
	const evil = "https://k.example.com"
	for _, qs := range []string{"?dry_run=1", ""} {
		rec := keep("R1"+qs, apiCall(e.s.handleBackupTargetSet, "POST", "/api/v1/backups/target"+qs, targetBody(evil, "", "")))
		if got := apiErrCode(t, rec, http.StatusBadRequest); got != s3.CodeKeysInvalid {
			t.Errorf("R1%s: mã lỗi %q, muốn %s", qs, got, s3.CodeKeysInvalid)
		}
	}
	if n := e.built(evil); n != 0 {
		t.Errorf("R1: kho của %s được dựng %d lần, muốn 0", evil, n)
	}
	// Cùng đích, khoá rỗng: giữ khoá đang lưu được (xem trước).
	if rec := apiCall(e.s.handleBackupTargetSet, "POST", "/api/v1/backups/target?dry_run=1", targetBody(m18Endpoint, "", "")); rec.Code != 200 {
		t.Errorf("cùng đích, giữ khoá: mã %d: %s", rec.Code, rec.Body)
	}

	for _, o := range outs {
		m18Scan(t, o.where, o.body, m18Access, m18Secret)
	}
	for _, o := range outs {
		if o.where == "GET backups user" && (strings.Contains(string(o.body), "remote_key") || strings.Contains(string(o.body), "remote_error")) {
			t.Errorf("user thường thấy remote_key, remote_error: %s", o.body)
		}
		if o.where == "GET backups admin" && (!strings.Contains(string(o.body), "remote_key") || !strings.Contains(string(o.body), "remote_error")) {
			t.Errorf("admin phải thấy remote_key, remote_error: %s", o.body)
		}
	}
	m18ScanDB(t, e.dbPath, m18Access, m18Secret)

	// R13: s3.json bị sửa tay sai dạng, mang một chuỗi đánh dấu. Không câu
	// trả lời nào chép nội dung file.
	const r13 = "R13NoiDungFileKhongDuocChep9876"
	e.f.SetFile(steps.S3KeysFile, []byte(`{"access_key": "`+r13+`", "secret_key": `), 0o600, system.Root)
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"GET target":        apiCall(e.s.handleBackupTarget, "GET", "/api/v1/backups/target", ""),
		"đặt đích giữ khoá": apiCall(e.s.handleBackupTargetSet, "POST", "/api/v1/backups/target?dry_run=1", targetBody(m18Endpoint, "", "")),
		"thử đích":          apiCall(e.s.handleBackupTargetTest, "POST", "/api/v1/backups/target/test", ""),
		"xem trước đẩy":     apiCall(e.s.handleBackupPush, "POST", "/api/v1/backups/x/push?dry_run=1", "", "id", b1.ID),
		"xem trước tải":     apiCall(e.s.handleBackupPull, "POST", "/api/v1/backups/x/pull?dry_run=1", "", "id", b1.ID),
		"GET remote":        apiCall(e.s.handleBackupRemote, "GET", "/api/v1/backups/remote", ""),
	} {
		if rec.Code < 400 {
			t.Errorf("R13 %s: mã %d, muốn lỗi: %s", name, rec.Code, rec.Body)
		}
		m18Scan(t, "R13 "+name, rec.Body.Bytes(), r13, m18Access, m18Secret)
	}
	e.s.api.wg.Wait()
	m18ScanDB(t, e.dbPath, r13, m18Access, m18Secret)
}

// m18Session ghi một phiên của user vào store và trả cookie (như authzEnv).
func (e *m18Env) session(t *testing.T, user string) *http.Cookie {
	t.Helper()
	token := "token-m18-" + user
	ss := store.Session{TokenSHA256: authTokenHash(token), ID: "m18" + user, IP: "192.0.2.1", Created: e.now, UserName: user}
	if err := e.st.CreateSession(context.Background(), ss); err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: authCookiePlain, Value: token}
}

// TestUserKhongDungDuocS3 (B5): user thường bị 403 ở mọi route M18 (cả
// ?dry_run=1); không thấy job đẩy, tải, gói, đặt đích nào trong GET /jobs; mở
// thẳng job đẩy thì 404; GET /backups không có remote_key, remote_error; và
// không chỗ nào hiện endpoint hay bucket.
func TestUserKhongDungDuocS3(t *testing.T) {
	e := newM18Env(t)
	ctx := context.Background()
	if err := e.st.CreateUser(ctx, account.User{Name: m18User, Role: account.RoleUser, Created: e.now}, "x"); err != nil {
		t.Fatal(err)
	}
	d := apiDomain(t, "a.example.com")
	if err := e.st.SaveSite(ctx, site.Site{Domain: d, Status: site.Active, PHPVersion: "8.3", Created: e.now, Owner: m18User}); err != nil {
		t.Fatal(err)
	}
	l := site.LayoutOf(d)
	e.f.SetDir(l.Public, 0o2750, system.Owner{User: l.User, Group: site.WebGroup})
	b1 := e.m9Backup(t, l, core.BackupFull, "a.example.com-20261119-020000.tar.gz", time.Hour)
	b2 := e.m9Backup(t, l, core.BackupFull, "a.example.com-20261118-020000.tar.gz", 25*time.Hour)
	e.setTargetAPI(t)
	apiDecode[QueuedResponse](t, apiCall(e.s.handleBackupPush, "POST", "/api/v1/backups/x/push", "", "id", b1.ID), http.StatusAccepted)
	e.syncNow()
	tid, _, _ := core.CurrentTargetID(ctx, e.st)
	if err := e.st.MarkBackupPushFailed(ctx, b2.ID, tid, "kho "+m18Endpoint+" "+m18Bucket+" trả lỗi", false); err != nil {
		t.Fatal(err)
	}
	var pushJob string
	jobs, _ := e.st.Jobs(ctx, 0)
	for _, j := range jobs {
		if j.Kind == "backup.push" {
			pushJob = j.ID
		}
	}
	if pushJob == "" {
		t.Fatalf("không có job đẩy: %+v", jobs)
	}

	h := e.s.handler(fstest.MapFS{"index.html": {Data: []byte("<html></html>")}})
	user := e.session(t, m18User)
	do := func(method, path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader("{}"))
		r.RemoteAddr = "192.0.2.1:40000"
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		r.AddCookie(user)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/v1/backups/target"},
		{"POST", "/api/v1/backups/target"},
		{"POST", "/api/v1/backups/target?dry_run=1"},
		{"DELETE", "/api/v1/backups/target"},
		{"DELETE", "/api/v1/backups/target?dry_run=1"},
		{"POST", "/api/v1/backups/target/test"},
		{"POST", "/api/v1/backups/" + b1.ID + "/push"},
		{"POST", "/api/v1/backups/" + b1.ID + "/push?dry_run=1"},
		{"POST", "/api/v1/backups/" + b1.ID + "/pull"},
		{"POST", "/api/v1/backups/" + b1.ID + "/pull?dry_run=1"},
		{"GET", "/api/v1/backups/remote"},
		{"POST", "/api/v1/cloud-init/render"},
	} {
		rec := do(c.method, c.path)
		if rec.Code != http.StatusForbidden || errCode(t, rec) != "forbidden" {
			t.Errorf("user %s %s: mã %d, muốn 403 forbidden: %s", c.method, c.path, rec.Code, rec.Body)
		}
	}

	visible := []struct {
		where string
		rec   *httptest.ResponseRecorder
	}{
		{"GET /jobs", do("GET", "/api/v1/jobs?limit=500")},
		{"GET /backups", do("GET", "/api/v1/backups")},
	}
	if rec := do("GET", "/api/v1/jobs/"+pushJob); rec.Code != http.StatusNotFound {
		t.Errorf("user mở job đẩy: mã %d, muốn 404: %s", rec.Code, rec.Body)
	} else {
		visible = append(visible, struct {
			where string
			rec   *httptest.ResponseRecorder
		}{"GET /jobs/" + pushJob, rec})
	}
	userJobs := apiDecode[[]engine.Job](t, visible[0].rec, 200)
	for _, j := range userJobs {
		if strings.HasPrefix(j.Kind, "backup.target") || j.Kind == "backup.push" || j.Kind == "backup.pull" || j.Kind == "backup.panel" {
			t.Errorf("user thấy job %s %s", j.Kind, j.ID)
		}
	}
	ov := apiDecode[BackupsOverview](t, visible[1].rec, 200)
	if len(ov.Items) != 2 {
		t.Fatalf("user phải thấy 2 bản của site mình, có %d", len(ov.Items))
	}
	for _, v := range visible {
		m18Scan(t, v.where, v.rec.Body.Bytes(), "s3.example.com", m18Bucket, m18Access, m18Secret)
		if b := v.rec.Body.String(); strings.Contains(b, "remote_key") || strings.Contains(b, "remote_error") {
			t.Errorf("%s: user thấy remote_key/remote_error: %s", v.where, b)
		}
	}
	// Restore của user vẫn như M13: tới được handler (job chạy ở nền).
	if rec := do("POST", "/api/v1/backups/"+b1.ID+"/restore"); rec.Code != http.StatusAccepted {
		t.Errorf("user khôi phục bản của mình: mã %d, muốn 202: %s", rec.Code, rec.Body)
	}
	e.s.api.wg.Wait()
}

// TestKhoiPhucBanChiConTrenKho: bản chỉ còn trên kho (local=0, stored ở đích
// đang đặt) thì restore qua API trả 409 backup_not_on_machine (core), kèm chỉ
// đường tải về; không job nào chạy.
func TestKhoiPhucBanChiConTrenKho(t *testing.T) {
	e := newM18Env(t)
	ctx := context.Background()
	blog := e.m9Site(t, "blog.example.com")
	b := e.m9Backup(t, blog, core.BackupFull, "blog.example.com-20261119-020000.tar.gz", time.Hour)
	e.setTargetAPI(t)
	tid, _, _ := core.CurrentTargetID(ctx, e.st)
	if ok, err := e.st.MarkBackupStored(ctx, b.ID, "sites/blog.example.com/x.tar.gz", tid, e.now); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if _, err := e.st.ForgetBackupLocal(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.f.Remove(b.Path); err != nil {
		t.Fatal(err)
	}
	before, _ := e.st.Jobs(ctx, 0)
	for _, q := range []string{"?dry_run=1", ""} {
		rec := apiCall(e.s.handleBackupRestore, "POST", "/api/v1/backups/x/restore"+q, "", "id", b.ID)
		if got := apiErrCode(t, rec, http.StatusConflict); got != core.CodeBackupNotOnMachine {
			t.Errorf("restore%s: mã lỗi %q, muốn %s", q, got, core.CodeBackupNotOnMachine)
		}
	}
	if after, _ := e.st.Jobs(ctx, 0); len(after) != len(before) {
		t.Errorf("restore bị từ chối mà vẫn có job mới")
	}
}

// TestDayVaTaiVeQuaAPI: đẩy chạy thật chỉ xếp hàng (202, không job trong
// request), lời từ chối của core về thẳng người gọi; tải về chạy ở nền và
// không giữ chỗ "đang có job" của API.
func TestDayVaTaiVeQuaAPI(t *testing.T) {
	e := newM18Env(t)
	ctx := context.Background()
	blog := e.m9Site(t, "blog.example.com")
	b := e.m9Backup(t, blog, core.BackupFull, "blog.example.com-20261119-020000.tar.gz", time.Hour)
	sqlB := e.m9Backup(t, blog, core.BackupSQL, "blog.example.com-db-20261119-030000.sql", time.Hour)

	// Chưa đặt đích: 409 s3_not_configured, cả đẩy lẫn tải.
	for name, h := range map[string]http.HandlerFunc{"đẩy": e.s.handleBackupPush, "tải": e.s.handleBackupPull} {
		if got := apiErrCode(t, apiCall(h, "POST", "/api/v1/backups/x/z", "", "id", b.ID), http.StatusConflict); got != s3.CodeNotConfigured {
			t.Errorf("%s khi chưa đặt đích: %q", name, got)
		}
	}
	e.setTargetAPI(t)
	if got := apiErrCode(t, apiCall(e.s.handleBackupPush, "POST", "/api/v1/backups/x/push", "", "id", sqlB.ID), http.StatusNotFound); got != "backup_not_found" {
		t.Errorf("đẩy bản sql: %q", got)
	}
	if got := apiErrCode(t, apiCall(e.s.handleBackupPush, "POST", "/api/v1/backups/x/push", "", "id", "a;rm -rf /"), http.StatusNotFound); got != "backup_not_found" {
		t.Errorf("đẩy id lạ: %q", got)
	}
	// Bản chưa lên kho, đang có trên máy: tải về là backup_already_local.
	if got := apiErrCode(t, apiCall(e.s.handleBackupPull, "POST", "/api/v1/backups/x/pull", "", "id", b.ID), http.StatusConflict); got != core.CodeBackupAlreadyLocal {
		t.Errorf("tải bản đang có trên máy: %q", got)
	}
	jobsBefore, _ := e.st.Jobs(ctx, 0)
	apiDecode[QueuedResponse](t, apiCall(e.s.handleBackupPush, "POST", "/api/v1/backups/x/push", "", "id", b.ID), http.StatusAccepted)
	if jobs, _ := e.st.Jobs(ctx, 0); len(jobs) != len(jobsBefore) {
		t.Errorf("POST push chạy job ngay trong request")
	}
	if q := e.s.pusher.take(); len(q) != 1 || q[0] != b.ID {
		t.Fatalf("hàng chờ = %v", q)
	}
	e.s.pusher.enqueueQuiet([]string{b.ID})
	e.syncNow()

	// Bản đã lên kho mà file trên máy mất: tải về ở nền (R16), không claim.
	if err := e.f.Remove(b.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.ForgetBackupLocal(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	e.s.api.claim() // đang có job khác của máy: tải về vẫn chạy (B10)
	apiDecode[QueuedResponse](t, apiCall(e.s.handleBackupPull, "POST", "/api/v1/backups/x/pull", "", "id", b.ID), http.StatusAccepted)
	e.s.api.wg.Wait()
	e.s.api.release()
	row, _, _ := e.st.Backup(ctx, b.ID)
	if !row.Local {
		t.Errorf("sau khi tải về, dòng chưa local: %+v", row)
	}
	a := hAudit(t, e.apiEnv, "backup.pull")
	if len(a) == 0 || a[len(a)-1].Result != "ok" || a[len(a)-1].JobID == "" || a[len(a)-1].Target != b.ID {
		t.Errorf("nhật ký backup.pull = %+v", a)
	}
	if p := hAudit(t, e.apiEnv, "backup.push"); len(p) == 0 || p[len(p)-1].Result != "queued" {
		t.Errorf("nhật ký backup.push = %+v", p)
	}
	if s := hAudit(t, e.apiEnv, "backup.target.set"); len(s) == 0 || s[0].Target != m18Endpoint+" "+m18Bucket+" "+m18Prefix {
		t.Errorf("nhật ký backup.target.set = %+v", s)
	}
}

// TestCloudInitRenderAPI: thiếu sha256 ra 400 cloudinit_invalid; user_data
// không chứa mật khẩu rõ; câu trả lời có admin_password 32 ký tự; nhật ký
// không ghi thân.
func TestCloudInitRenderAPI(t *testing.T) {
	e := newM18Env(t)
	sum := strings.Repeat("ab", 32)
	call := func(body string) *httptest.ResponseRecorder {
		return apiCall(e.s.handleCloudInitRender, "POST", "/api/v1/cloud-init/render", body)
	}
	for name, body := range map[string]string{
		"thiếu cả hai": `{"web":"nginx","base":"https://example.com/tpan/v0.3.0"}`,
		"thiếu arm64":  `{"web":"nginx","base":"https://example.com/tpan/v0.3.0","sha256_amd64":"` + sum + `"}`,
		"web lạ":       `{"web":"iis","base":"https://example.com/tpan/v0.3.0","sha256_amd64":"` + sum + `","sha256_arm64":"` + sum + `"}`,
	} {
		if got := apiErrCode(t, call(body), http.StatusBadRequest); got != core.CodeCloudInitInvalid {
			t.Errorf("%s: mã lỗi %q, muốn %s", name, got, core.CodeCloudInitInvalid)
		}
	}
	rec := call(`{"web":"nginx","base":"https://example.com/tpan/v0.3.0","sha256_amd64":"` + sum + `","sha256_arm64":"` + sum + `"}`)
	res := apiDecode[CloudInitResponse](t, rec, 200)
	if len(res.AdminPassword) != 32 {
		t.Errorf("admin_password dài %d, muốn 32", len(res.AdminPassword))
	}
	if res.UserData == "" || res.Bytes != len(res.UserData) {
		t.Errorf("user_data rỗng hoặc bytes sai: %d / %d", res.Bytes, len(res.UserData))
	}
	ud, _ := json.Marshal(res.UserData)
	m18Scan(t, "user_data", ud, res.AdminPassword)
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q", rec.Header().Get("Cache-Control"))
	}
	m18ScanDB(t, e.dbPath, res.AdminPassword, sum)
}
