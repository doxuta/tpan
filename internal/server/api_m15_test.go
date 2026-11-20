package server

// Test API của M15 (nút cập nhật TPAN trong panel, mục 6.14). Mọi request đi
// qua handler đầy đủ như trình duyệt gọi: lớp chống CSRF (secure), guard phân
// quyền, rồi handler thật, trên FakeSystem và store thật. Phiên của ba vai lấy
// từ authz_test.go.

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/buildinfo"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

const (
	selfLatest = "v9.9.9"
	selfOld    = core.TpanBin + ".old"
)

var (
	selfSumAMD = strings.Repeat("a", 64)
	selfSumARM = strings.Repeat("b", 64)
	// selfIsActive là lệnh duy nhất GET /self được chạy.
	selfIsActive = []string{"systemctl", "is-active", core.SelfUnit + ".service"}
)

type selfEnv struct {
	*authzEnv
	h http.Handler
}

// newSelfEnv: nguồn tải mặc định trả lời hai lệnh curl của steps.SelfLatest;
// systemctl is-active không có handler nên in rỗng (đơn vị tạm không chạy);
// systemd-run không có handler nên thành công.
func newSelfEnv(t *testing.T) *selfEnv {
	t.Helper()
	e := &selfEnv{authzEnv: newAuthzEnv(t)}
	e.h = e.s.handler(authzDist)
	e.f.Handle([]string{"curl"}, func(c system.Cmd) system.Result {
		switch c.Args[len(c.Args)-1] {
		case core.DefaultSelfURL + "/SHA256SUMS":
			return system.Result{Stdout: selfSumAMD + "  tpan-linux-amd64\n" + selfSumARM + "  tpan-linux-arm64\n"}
		case core.DefaultSelfURL + "/VERSION":
			return system.Result{Stdout: selfLatest + "\n"}
		}
		return system.Result{ExitCode: 22, Stderr: "curl: (22) The requested URL returned error: 404\n"}
	})
	return e
}

// admin gọi API bằng phiên admin, cùng origin.
func (e *selfEnv) admin(method, path, body string) *httptest.ResponseRecorder {
	return e.do(e.h, method, path, body, e.cookies[account.AdminName])
}

// ran trả argv của các lệnh tên name đã chạy; name rỗng là mọi lệnh.
func (e *selfEnv) ran(name string) [][]string {
	var out [][]string
	for _, c := range e.f.Ran() {
		if name == "" || c.Name == name {
			out = append(out, append([]string{c.Name}, c.Args...))
		}
	}
	return out
}

// busy cho systemd trả lời "đơn vị tạm đang chạy".
func (e *selfEnv) busy() {
	e.f.Handle(selfIsActive, func(system.Cmd) system.Result { return system.Result{Stdout: "active\n"} })
}

func (e *selfEnv) hasOld() {
	e.f.SetFile(selfOld, []byte("ELF cũ"), 0o755, system.Root)
}

func (e *selfEnv) setting(key string) (string, bool) {
	e.t.Helper()
	v, ok, err := e.st.GetSetting(context.Background(), key)
	if err != nil {
		e.t.Fatal(err)
	}
	return v, ok
}

// audit đọc các dòng nhật ký self.* (store không có hàm đọc audit).
func (e *selfEnv) audit() []store.AuditEntry {
	e.t.Helper()
	db, err := sql.Open("sqlite", e.dbPath)
	if err != nil {
		e.t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	rows, err := db.Query("SELECT action, target, result, ip, actor, job_id FROM audit WHERE action LIKE 'self.%' ORDER BY rowid")
	if err != nil {
		e.t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []store.AuditEntry
	for rows.Next() {
		var a store.AuditEntry
		if err := rows.Scan(&a.Action, &a.Target, &a.Result, &a.IP, &a.Actor, &a.JobID); err != nil {
			e.t.Fatal(err)
		}
		out = append(out, a)
	}
	return out
}

// selfSameRun so hai lượt chạy; thời điểm so bằng Equal vì JSON không giữ múi giờ.
func selfSameRun(a, b core.SelfRun) bool {
	same := a.Started.Equal(b.Started) && a.Finished.Equal(b.Finished)
	a.Started, a.Finished, b.Started, b.Finished = time.Time{}, time.Time{}, time.Time{}, time.Time{}
	return same && a == b
}

// wantAudit: nhật ký self.* có đúng một dòng, do admin ở 192.0.2.1 để lại.
func (e *selfEnv) wantAudit(action, result string) {
	e.t.Helper()
	want := store.AuditEntry{Action: action, Target: buildinfo.Version, Result: result, IP: "192.0.2.1", Actor: account.AdminName}
	if a := e.audit(); len(a) != 1 || a[0] != want {
		e.t.Errorf("audit = %+v, muốn đúng một dòng %+v", a, want)
	}
}

// ---------------------------------------------------------------- GET /self

func TestSelfChuaCoGi(t *testing.T) {
	e := newSelfEnv(t)
	rec := e.admin("GET", "/api/v1/self", "")
	st := apiDecode[SelfState](t, rec, 200)
	if st.Version != buildinfo.Version || st.URL != core.DefaultSelfURL || st.HasOld || st.Busy || st.Last != nil || st.Check != nil {
		t.Errorf("SelfState = %+v", st)
	}
	// Khung JSON khớp web/src/api/m15-types.ts: last và check vắng hẳn.
	if got := jsonKeys(t, rec.Body.Bytes()); !slices.Equal(got, []string{"busy", "has_old", "url", "version"}) {
		t.Errorf("khoá của SelfState = %v", got)
	}
	// Không ra mạng, không chạy file cũ: chỉ một lệnh hỏi systemd.
	if got := e.ran(""); len(got) != 1 || !slices.Equal(got[0], selfIsActive) {
		t.Errorf("GET /self chạy %v, muốn chỉ %v", got, selfIsActive)
	}

	e.hasOld()
	e.busy()
	if st := apiDecode[SelfState](t, e.admin("GET", "/api/v1/self", ""), 200); !st.HasOld || !st.Busy {
		t.Errorf("có .old và đơn vị tạm đang chạy: %+v", st)
	}
	if n := len(e.ran(selfOld)); n != 0 {
		t.Errorf("GET /self đã chạy file cũ %d lần", n)
	}
}

// Lượt còn ghi "running" mà đơn vị tạm không chạy quá 15 giây: trả
// "interrupted", nhưng không ghi chữ đó vào settings.
func TestSelfLastInterrupted(t *testing.T) {
	e := newSelfEnv(t)
	mark, err := json.Marshal(core.SelfRun{
		Action: core.SelfActionUpdate, Status: core.SelfRunning, From: "v0.2.0",
		Actor: account.AdminName, IP: "192.0.2.1", Started: e.now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.SetSetting(context.Background(), core.SettingSelfLast, string(mark)); err != nil {
		t.Fatal(err)
	}
	last := func() core.SelfRun {
		t.Helper()
		st := apiDecode[SelfState](t, e.admin("GET", "/api/v1/self", ""), 200)
		if st.Last == nil {
			t.Fatal("thiếu last")
		}
		return *st.Last
	}

	e.now = e.now.Add(selfInterruptAfter)
	if l := last(); l.Status != core.SelfRunning || l.From != "v0.2.0" || l.Actor != account.AdminName {
		t.Errorf("vừa đủ 15 giây: last = %+v, muốn còn running", l)
	}
	e.now = e.now.Add(time.Second)
	if l := last(); l.Status != core.SelfInterrupted {
		t.Errorf("quá 15 giây, đơn vị tạm không chạy: status = %q, muốn interrupted", l.Status)
	}
	if v, _ := e.setting(core.SettingSelfLast); v != string(mark) {
		t.Errorf("GET /self đã sửa settings: %s", v)
	}
	// Đơn vị tạm còn chạy thì lâu mấy cũng là running (tải chậm).
	e.busy()
	e.now = e.now.Add(10 * time.Minute)
	if l := last(); l.Status != core.SelfRunning {
		t.Errorf("đơn vị tạm đang chạy: status = %q, muốn running", l.Status)
	}
}

// ---------------------------------------------------------------- kiểm bản mới

func TestSelfKiemBanMoi(t *testing.T) {
	e := newSelfEnv(t)
	rec := e.admin("POST", "/api/v1/self/update?dry_run=1", "")
	st := apiDecode[SelfState](t, rec, 200)
	if st.Check == nil {
		t.Fatalf("thiếu check: %s", rec.Body)
	}
	wantSum := map[string]string{"amd64": selfSumAMD, "arm64": selfSumARM}[runtime.GOARCH]
	want := core.SelfCheck{
		Current: buildinfo.Version, Latest: selfLatest, URL: core.DefaultSelfURL,
		Asset: "tpan-linux-" + runtime.GOARCH, SHA256: wantSum,
	}
	if c := st.Check; c.SelfCheck != want || !c.CheckedAt.Equal(e.now) || c.Error != nil {
		t.Errorf("check = %+v, muốn %+v lúc %s", c, want, e.now)
	}
	if st.Version != buildinfo.Version || st.URL != core.DefaultSelfURL {
		t.Errorf("câu trả lời của dry_run phải là SelfState đủ: %+v", st)
	}
	var raw struct {
		Check json.RawMessage `json:"check"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	if got := jsonKeys(t, raw.Check); !slices.Equal(got, []string{"asset", "checked_at", "current", "latest", "sha256", "up_to_date", "url"}) {
		t.Errorf("khoá của check = %v", got)
	}
	if n := len(e.ran("curl")); n != 2 {
		t.Fatalf("kiểm bản mới chạy curl %d lần, muốn 2 (SHA256SUMS, VERSION)", n)
	}
	// Chỉ đọc: không khởi đơn vị tạm, không ghi settings, không ghi nhật ký.
	if n := len(e.ran("systemd-run")); n != 0 {
		t.Errorf("dry_run đã chạy systemd-run %d lần", n)
	}
	if _, ok := e.setting(core.SettingSelfLast); ok {
		t.Error("dry_run đã ghi update.self_last")
	}
	if a := e.audit(); len(a) != 0 {
		t.Errorf("dry_run ghi nhật ký: %+v", a)
	}

	// GET sau đó còn nhớ lần kiểm, không ra mạng lại.
	e.now = e.now.Add(time.Hour)
	got := apiDecode[SelfState](t, e.admin("GET", "/api/v1/self", ""), 200)
	if got.Check == nil || got.Check.SelfCheck != want || !got.Check.CheckedAt.Equal(st.Check.CheckedAt) {
		t.Errorf("GET sau khi kiểm: check = %+v", got.Check)
	}
	if n := len(e.ran("curl")); n != 2 {
		t.Errorf("GET /self đã ra mạng (curl %d lần)", n)
	}
}

func TestSelfKiemAuto(t *testing.T) {
	e := newSelfEnv(t)
	check := func(query string, wantCurls int, wantAt time.Time) {
		t.Helper()
		st := apiDecode[SelfState](t, e.admin("POST", "/api/v1/self/update?dry_run=1"+query, ""), 200)
		if st.Check == nil || st.Check.Latest != selfLatest || !st.Check.CheckedAt.Equal(wantAt) {
			t.Errorf("%q: check = %+v, muốn kiểm lúc %s", query, st.Check, wantAt)
		}
		if n := len(e.ran("curl")); n != wantCurls {
			t.Errorf("%q: curl đã chạy %d lần, muốn %d", query, n, wantCurls)
		}
	}
	t0 := e.now
	check("&auto=1", 2, t0) // chưa có lần nào: kiểm thật

	e.now = t0.Add(SelfCheckTTL)
	check("&auto=1", 2, t0) // còn trong hạn: trả lại lần cũ, không ra mạng

	t1 := e.now
	check("", 4, t1)          // người bấm nút: luôn kiểm mới
	check("&auto=yes", 6, t1) // chỉ đúng chữ "1" mới là auto

	e.now = t1.Add(SelfCheckTTL + time.Minute)
	check("&auto=1", 8, e.now) // quá hạn: kiểm lại
}

// Lần kiểm đã nhớ nói về một nguồn tải. Nguồn tải đổi (settings
// update.self_url) thì nó không còn đúng: GET không trả nó nữa, và auto=1
// kiểm lại dù chưa hết hạn.
func TestSelfKiemQuenKhiDoiNguon(t *testing.T) {
	e := newSelfEnv(t)
	if st := apiDecode[SelfState](t, e.admin("POST", "/api/v1/self/update?dry_run=1", ""), 200); st.Check == nil {
		t.Fatal("thiếu check sau lần kiểm đầu")
	}
	const other = "https://example.com/ban-phat-hanh"
	if err := e.st.SetSetting(context.Background(), core.SettingSelfURL, other); err != nil {
		t.Fatal(err)
	}
	if st := apiDecode[SelfState](t, e.admin("GET", "/api/v1/self", ""), 200); st.URL != other || st.Check != nil {
		t.Errorf("sau khi đổi nguồn tải: url = %s, check = %+v; muốn không còn check", st.URL, st.Check)
	}
	before := len(e.ran("curl"))
	st := apiDecode[SelfState](t, e.admin("POST", "/api/v1/self/update?dry_run=1&auto=1", ""), 200)
	if len(e.ran("curl")) == before {
		t.Error("auto=1 sau khi đổi nguồn tải không hỏi nguồn mới")
	}
	if st.Check == nil || st.Check.URL != other {
		t.Errorf("check sau khi đổi nguồn tải = %+v, muốn của %s", st.Check, other)
	}
}

// Không hỏi được nguồn tải vẫn là 200: lý do nằm ở check.error, và lần lỗi
// cũng được nhớ để auto=1 không chờ mạng lại ở mọi lần tải trang.
func TestSelfKiemLoi(t *testing.T) {
	e := newSelfEnv(t)
	e.f.Handle([]string{"curl"}, func(system.Cmd) system.Result {
		return system.Result{ExitCode: 6, Stderr: "curl: (6) Could not resolve host: github.com\n"}
	})
	bad := func(rec *httptest.ResponseRecorder) {
		t.Helper()
		st := apiDecode[SelfState](t, rec, 200)
		c := st.Check
		if c == nil || c.Error == nil {
			t.Fatalf("thiếu check.error: %s", rec.Body)
		}
		if !strings.Contains(c.Error.Vi, "Không hỏi được nguồn tải") || !strings.Contains(c.Error.Vi, "SHA256SUMS") ||
			!strings.Contains(c.Error.En, "download source") {
			t.Errorf("check.error = %+v", c.Error)
		}
		if c.Current != buildinfo.Version || c.URL != core.DefaultSelfURL || c.Asset != "tpan-linux-"+runtime.GOARCH || c.Latest != "" || c.SHA256 != "" || c.UpToDate {
			t.Errorf("check khi lỗi = %+v", c.SelfCheck)
		}
		if st.Version != buildinfo.Version {
			t.Errorf("phần còn lại của SelfState phải còn đủ: %+v", st)
		}
	}
	bad(e.admin("POST", "/api/v1/self/update?dry_run=1", ""))
	if n := len(e.ran("curl")); n != 1 {
		t.Fatalf("curl chạy %d lần, muốn 1 (SHA256SUMS hỏng thì không hỏi VERSION)", n)
	}
	e.now = e.now.Add(time.Hour)
	bad(e.admin("POST", "/api/v1/self/update?dry_run=1&auto=1", ""))
	bad(e.admin("GET", "/api/v1/self", ""))
	if n := len(e.ran("curl")); n != 1 {
		t.Errorf("auto=1 sau lần lỗi vẫn ra mạng (curl %d lần)", n)
	}
	// Người bấm nút thì hỏi lại ngay.
	bad(e.admin("POST", "/api/v1/self/update?dry_run=1", ""))
	if n := len(e.ran("curl")); n != 2 {
		t.Errorf("bấm kiểm lại sau lần lỗi: curl %d lần, muốn 2", n)
	}
}

// Người gọi đóng trang giữa lúc kiểm: lỗi "context canceled" không nói gì về
// nguồn tải, không được nhớ thành "không hỏi được" cho 6 giờ sau.
func TestSelfKiemBiHuyKhongNho(t *testing.T) {
	e := newSelfEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest("POST", "/api/v1/self/update?dry_run=1", nil).WithContext(ctx)
	e.s.handleSelfUpdate(httptest.NewRecorder(), r)
	if c := e.s.selfCheck.get(core.DefaultSelfURL); c != nil {
		t.Errorf("lần kiểm bị huỷ vẫn được nhớ: %+v", c)
	}
}

// ---------------------------------------------------------------- chạy thật

// Dòng lệnh của đơn vị tạm là hằng: thân request, query lạ, header đều không
// tới được argv hay nguồn tải.
func TestSelfUpdateChay(t *testing.T) {
	e := newSelfEnv(t)
	const evil = "https://evil.example.com/x"
	rec := e.admin("POST", "/api/v1/self/update?url="+evil+"&version=v6.6.6&action=rollback&unit=evil-unit",
		`{"url":"`+evil+`","version":"v6.6.6","action":"rollback","arch":"mips","actor":"userb","ip":"203.0.113.9"}`)
	run := apiDecode[SelfStarted](t, rec, http.StatusAccepted).Run
	want := core.SelfRun{
		Action: core.SelfActionUpdate, Status: core.SelfRunning, From: buildinfo.Version,
		Actor: account.AdminName, IP: "192.0.2.1", Started: e.now,
	}
	if !selfSameRun(run, want) {
		t.Errorf("run = %+v, muốn %+v", run, want)
	}

	got := e.ran("")
	if len(got) != 2 || !slices.Equal(got[0], selfIsActive) {
		t.Fatalf("lệnh đã chạy = %v, muốn is-active rồi systemd-run", got)
	}
	// Phần tử thứ 6 là dòng mô tả (hằng của core); bỏ nó ra rồi so từng chữ,
	// nên thừa hay thiếu một tham số nào cũng lộ.
	argv := got[1]
	if len(argv) < 6 {
		t.Fatalf("argv của systemd-run = %q", argv)
	}
	wantArgv := []string{"systemd-run", "--unit", core.SelfUnit, "--collect", "--description", core.TpanBin, "internal", "self-update"}
	if rest := slices.Concat(argv[:5], argv[6:]); !slices.Equal(rest, wantArgv) {
		t.Errorf("argv của systemd-run = %q", argv)
	}
	for _, c := range e.f.Ran() {
		line := c.String() + string(c.Stdin)
		for _, bad := range []string{"evil", "v6.6.6", "mips", "userb", "203.0.113.9", "192.0.2.1", account.AdminName, "rollback"} {
			if strings.Contains(line, bad) {
				t.Errorf("lệnh %q mang chữ %q từ request", line, bad)
			}
		}
	}

	// Nguồn tải vẫn là hằng: settings update.self_url không bị ai ghi.
	if v, ok := e.setting(core.SettingSelfURL); ok || v != "" {
		t.Errorf("update.self_url = %q sau request có url lạ", v)
	}
	mark, _ := e.setting(core.SettingSelfLast)
	var saved core.SelfRun
	if err := json.Unmarshal([]byte(mark), &saved); err != nil || !selfSameRun(saved, want) {
		t.Errorf("update.self_last = %s (%v), muốn %+v", mark, err, want)
	}
	e.wantAudit("self.update", "started")

	// Chỗ chạy job đã nhả: lượt chạy nằm ở tiến trình khác.
	if !e.s.api.claim() {
		t.Error("sau Start, chỗ chạy job chưa được nhả")
	}
	e.s.api.release()

	// Màn hỏi lại ngay sau đó: lượt vừa ghi còn "running".
	st := apiDecode[SelfState](t, e.admin("GET", "/api/v1/self", ""), 200)
	if st.Last == nil || !selfSameRun(*st.Last, want) {
		t.Errorf("GET /self sau khi bấm: last = %+v", st.Last)
	}
}

func TestSelfRollbackChay(t *testing.T) {
	e := newSelfEnv(t)
	e.hasOld()
	rec := e.admin("POST", "/api/v1/self/rollback", `{"action":"update"}`)
	run := apiDecode[SelfStarted](t, rec, http.StatusAccepted).Run
	if run.Action != core.SelfActionRollback || run.Status != core.SelfRunning || run.Actor != account.AdminName {
		t.Errorf("run = %+v", run)
	}
	got := e.ran("systemd-run")
	if len(got) != 1 || len(got[0]) < 6 ||
		!slices.Equal(got[0][6:], []string{core.TpanBin, "internal", "self-update", "--rollback"}) ||
		!slices.Equal(got[0][:5], []string{"systemd-run", "--unit", core.SelfUnit, "--collect", "--description"}) {
		t.Errorf("systemd-run = %q", got)
	}
	// Máy test không có /var/lib/tpan/tpan.db: không chạy thử file cũ.
	if n := len(e.ran(selfOld)); n != 0 {
		t.Errorf("đã chạy file cũ %d lần", n)
	}
	e.wantAudit("self.rollback", "started")
}

// Bản xem trước của "Quay về bản trước": hai tên bản cho hộp xác nhận.
func TestSelfRollbackXemTruoc(t *testing.T) {
	e := newSelfEnv(t)
	if rec := e.admin("POST", "/api/v1/self/rollback?dry_run=1", ""); rec.Code != http.StatusConflict || errCode(t, rec) != "self_no_old" {
		t.Errorf("chưa có .old: %d %s", rec.Code, rec.Body)
	}
	e.hasOld()
	e.f.Handle([]string{selfOld, "version"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "tpan v0.1.2 (commit abc1234, build 2026-10-01T00:00:00Z)\n"}
	})
	rec := e.admin("POST", "/api/v1/self/rollback?dry_run=1", "")
	if p := apiDecode[core.SelfRollbackPreview](t, rec, 200); p.Current != buildinfo.Version || p.Old != "v0.1.2" {
		t.Errorf("preview = %+v", p)
	}
	if got := jsonKeys(t, rec.Body.Bytes()); !slices.Equal(got, []string{"current", "old"}) {
		t.Errorf("khoá của SelfRollbackPreview = %v", got)
	}
	if n := len(e.ran("systemd-run")); n != 0 {
		t.Errorf("dry_run đã chạy systemd-run %d lần", n)
	}
	if a := e.audit(); len(a) != 0 {
		t.Errorf("dry_run ghi nhật ký: %+v", a)
	}
}

// Các lời từ chối của chạy thật: 409, một dòng nhật ký mang mã từ chối, không
// khởi đơn vị tạm.
func TestSelfTuChoi(t *testing.T) {
	for _, c := range []struct {
		name, path, action, code string
		setup                    func(e *selfEnv)
		// noCmd: không lệnh nào được chạy (bị chặn trước khi chạm vào máy).
		noCmd bool
	}{
		{name: "đơn vị tạm đang chạy", path: "/api/v1/self/update", action: "self.update", code: core.CodeSelfBusy,
			setup: func(e *selfEnv) { e.busy() }},
		{name: "rollback khi đơn vị tạm đang chạy", path: "/api/v1/self/rollback", action: "self.rollback", code: core.CodeSelfBusy,
			setup: func(e *selfEnv) { e.hasOld(); e.busy() }},
		{name: "rollback không có .old", path: "/api/v1/self/rollback", action: "self.rollback", code: "self_no_old",
			setup: func(*selfEnv) {}},
		{name: "panel đang có job", path: "/api/v1/self/update", action: "self.update", code: "job_running", noCmd: true,
			setup: func(e *selfEnv) {
				if !e.s.api.claim() {
					e.t.Fatal("không giành được chỗ chạy job")
				}
			}},
		{name: "demo: update", path: "/api/v1/self/update", action: "self.update", code: "demo_no_self_update", noCmd: true,
			setup: func(e *selfEnv) { e.s.deps.Demo = true }},
		{name: "demo: rollback", path: "/api/v1/self/rollback", action: "self.rollback", code: "demo_no_self_update", noCmd: true,
			setup: func(e *selfEnv) { e.s.deps.Demo = true; e.hasOld() }},
		// Demo chặn trước cả "đang có job": máy giả không có gì để chờ.
		{name: "demo khi đang có job", path: "/api/v1/self/update", action: "self.update", code: "demo_no_self_update", noCmd: true,
			setup: func(e *selfEnv) { e.s.deps.Demo = true; e.s.api.claim() }},
		{name: "systemd-run báo đã có đơn vị cùng tên", path: "/api/v1/self/update", action: "self.update", code: core.CodeSelfBusy,
			setup: func(e *selfEnv) {
				e.f.Handle([]string{"systemd-run"}, func(system.Cmd) system.Result {
					return system.Result{ExitCode: 1, Stderr: "Failed to start transient service unit: Unit tpan-self-update.service was already loaded or has a fragment file.\n"}
				})
			}},
		{name: "systemd-run hỏng", path: "/api/v1/self/update", action: "self.update", code: core.CodeSelfStart,
			setup: func(e *selfEnv) {
				e.f.Handle([]string{"systemd-run"}, func(system.Cmd) system.Result {
					return system.Result{ExitCode: 1, Stderr: "Failed to connect to bus: No such file or directory\n"}
				})
			}},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newSelfEnv(t)
			c.setup(e)
			holding := e.s.api.busy
			rec := e.admin("POST", c.path, `{"url":"https://evil.example.com"}`)
			if rec.Code != http.StatusConflict || errCode(t, rec) != c.code {
				t.Fatalf("mã %d %s, muốn 409 %s", rec.Code, rec.Body, c.code)
			}
			var body ErrorBody
			_ = json.Unmarshal(rec.Body.Bytes(), &body)
			if body.Error.Message.Vi == "" || body.Error.Message.En == "" {
				t.Errorf("lời nhắn phải có hai ngôn ngữ: %+v", body.Error.Message)
			}
			e.wantAudit(c.action, c.code)
			if c.noCmd {
				if got := e.ran(""); len(got) != 0 {
					t.Errorf("đã chạy lệnh: %v", got)
				}
				if _, ok := e.setting(core.SettingSelfLast); ok {
					t.Error("đã ghi update.self_last")
				}
			}
			// Bị từ chối trước khi khởi đơn vị tạm thì systemd-run không được gọi
			// (hai ca cuối là chính systemd-run từ chối).
			if n := len(e.ran("systemd-run")); n != 0 && !strings.HasPrefix(c.name, "systemd-run") {
				t.Errorf("bị từ chối mà vẫn chạy systemd-run %d lần", n)
			}
			// Lời từ chối không được giữ (hay nhả nhầm) chỗ chạy job.
			if e.s.api.busy != holding {
				t.Errorf("chỗ chạy job: trước %v, sau %v", holding, e.s.api.busy)
			}
			// Không lượt nào đang chạy thật: màn không được treo ở "running".
			if st := apiDecode[SelfState](t, e.admin("GET", "/api/v1/self", ""), 200); st.Last != nil && st.Last.Status == core.SelfRunning {
				t.Errorf("sau lời từ chối, last còn running: %+v", st.Last)
			}
		})
	}
}

// Chế độ demo: chạy thật bị chặn, nhưng "Kiểm bản mới" và bản xem trước vẫn
// chạy (trên máy giả) để xem được giao diện.
func TestSelfDemoVanKiemDuoc(t *testing.T) {
	e := newSelfEnv(t)
	e.s.deps.Demo = true
	st := apiDecode[SelfState](t, e.admin("POST", "/api/v1/self/update?dry_run=1", ""), 200)
	if st.Check == nil || st.Check.Latest != selfLatest || st.Check.UpToDate {
		t.Errorf("demo, kiểm bản mới: %+v", st.Check)
	}
}

// ---------------------------------------------------------------- phân quyền, CSRF

// User thường và người chưa đăng nhập không tới được handler thật: không lệnh
// nào chạy, settings và nhật ký không đổi.
func TestSelfChiAdmin(t *testing.T) {
	e := newSelfEnv(t)
	e.hasOld()
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/v1/self"},
		{"POST", "/api/v1/self/update"},
		{"POST", "/api/v1/self/update?dry_run=1"},
		{"POST", "/api/v1/self/update?dry_run=1&auto=1"},
		{"POST", "/api/v1/self/rollback"},
		{"POST", "/api/v1/self/rollback?dry_run=1"},
	} {
		if rec := e.do(e.h, c.method, c.path, "{}", e.cookies[authzA]); rec.Code != http.StatusForbidden || errCode(t, rec) != "forbidden" {
			t.Errorf("user thường %s %s: %d %s, muốn 403 forbidden", c.method, c.path, rec.Code, rec.Body)
		}
		if rec := e.do(e.h, c.method, c.path, "{}", nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("chưa đăng nhập %s %s: mã %d, muốn 401", c.method, c.path, rec.Code)
		}
	}
	// Admin đang "xem như" user A mang vai của A: cũng không bấm được.
	if rec := e.do(e.h, "POST", "/api/v1/self/update", "", e.session(account.AdminName, authzA)); rec.Code != http.StatusForbidden {
		t.Errorf("phiên xem như POST /self/update: mã %d, muốn 403", rec.Code)
	}
	e.selfUntouched()
}

// Trang khác gửi POST bằng cookie của admin (CSRF): bị chặn trước guard.
func TestSelfCSRF(t *testing.T) {
	e := newSelfEnv(t)
	e.hasOld()
	for _, path := range []string{"/api/v1/self/update", "/api/v1/self/rollback", "/api/v1/self/update?dry_run=1"} {
		r := httptest.NewRequest("POST", path, strings.NewReader("{}"))
		r.RemoteAddr = "192.0.2.1:40000"
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		r.Header.Set("Origin", "https://evil.example.com")
		r.AddCookie(e.cookies[account.AdminName])
		rec := httptest.NewRecorder()
		e.h.ServeHTTP(rec, r)
		if rec.Code != http.StatusForbidden || errCode(t, rec) != "csrf" {
			t.Errorf("cross-site POST %s: %d %s, muốn 403 csrf", path, rec.Code, rec.Body)
		}
	}
	e.selfUntouched()
}

// selfUntouched: không lệnh nào chạy, không lần kiểm nào được nhớ, settings và
// nhật ký của self-update còn nguyên.
func (e *selfEnv) selfUntouched() {
	e.t.Helper()
	if got := e.ran(""); len(got) != 0 {
		e.t.Errorf("đã chạy lệnh trên máy: %v", got)
	}
	for _, k := range []string{core.SettingSelfLast, core.SettingSelfURL} {
		if v, ok := e.setting(k); ok {
			e.t.Errorf("settings %s đã bị ghi: %q", k, v)
		}
	}
	if a := e.audit(); len(a) != 0 {
		e.t.Errorf("nhật ký self.* có dòng: %+v", a)
	}
	if c := e.s.selfCheck.get(core.DefaultSelfURL); c != nil {
		e.t.Errorf("đã nhớ một lần kiểm: %+v", c)
	}
}
