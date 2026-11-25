package server

// Test handler thật của M13 qua bảng route thật (guard + handler): lọc theo
// chủ, phiên "xem như", đăng nhập theo tên, lỗ POST /sites. Dữ liệu mẫu và ba
// phiên ở authz_test.go (newAuthzEnv).

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// authzTrashedDB là một DB rời của A đang ở thùng rác.
const authzTrashedDB = "usera_old"

func m13Decode[T any](t *testing.T, rec *httptest.ResponseRecorder, want int) T {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("mã %d, muốn %d; thân %s", rec.Code, want, rec.Body)
	}
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("thân không đọc được: %v (%s)", err, rec.Body)
	}
	return v
}

// TestDanhSachLocTheoChu: B không thấy site, job, DB, backup, sự cố của A; A
// thấy đủ; admin thấy mọi thứ.
func TestDanhSachLocTheoChu(t *testing.T) {
	e := newAuthzEnv(t)
	h := e.s.handler(authzDist)
	if err := e.st.SaveStandaloneDB(context.Background(), account.Database{
		Name: authzTrashedDB, Owner: authzA, Status: account.DBTrashed, Created: e.now, TrashedAt: e.now,
	}); err != nil {
		t.Fatal(err)
	}
	// Stack đã cài PHP 8.3 (bản của site A) để /php có dòng liệt kê site.
	if err := e.st.SetSetting(context.Background(), core.SettingPHPVersion, "8.3"); err != nil {
		t.Fatal(err)
	}
	e.f.SetDir("/etc/php/8.3/fpm/pool.d", 0o755, system.Root)
	e.f.SetFile("/usr/sbin/php-fpm8.3", []byte("ELF"), 0o755, system.Root)
	get := func(path, who string) *httptest.ResponseRecorder {
		return e.do(h, "GET", path, "", e.cookies[who])
	}
	for _, who := range []string{account.AdminName, authzA, authzB} {
		mine := who != authzB // admin và A thấy đồ của A

		sites := m13Decode[[]site.Site](t, get("/api/v1/sites", who), 200)
		if got := slices.ContainsFunc(sites, func(s site.Site) bool { return s.Domain.String() == authzSite }); got != mine {
			t.Errorf("%s: /sites có site của A = %v, muốn %v", who, got, mine)
		}
		jobs := m13Decode[[]engine.Job](t, get("/api/v1/jobs", who), 200)
		if got := slices.ContainsFunc(jobs, func(j engine.Job) bool { return j.ID == authzJob }); got != mine {
			t.Errorf("%s: /jobs có job của A = %v, muốn %v", who, got, mine)
		}
		dbs := m13Decode[[]DatabaseRow](t, get("/api/v1/databases", who), 200)
		for _, name := range []string{authzDB, site.LayoutOf(apiDomain(t, authzSite)).DB} {
			if got := slices.ContainsFunc(dbs, func(d DatabaseRow) bool { return d.DB == name }); got != mine {
				t.Errorf("%s: /databases có %s = %v, muốn %v", who, name, got, mine)
			}
		}
		bk := m13Decode[BackupsOverview](t, get("/api/v1/backups", who), 200)
		if got := len(bk.Items) == 1; got != mine {
			t.Errorf("%s: /backups có %d bản, muốn bản của A = %v", who, len(bk.Items), mine)
		}
		incs := m13Decode[[]store.Incident](t, get("/api/v1/incidents", who), 200)
		if got := len(incs) == 1; got != mine {
			t.Errorf("%s: /incidents có %d, muốn sự cố của A = %v", who, len(incs), mine)
		}
		ov := m13Decode[Overview](t, get("/api/v1/overview", who), 200)
		if got := len(ov.Sites) == 1; got != mine {
			t.Errorf("%s: tổng quan có %d site", who, len(ov.Sites))
		}
		if isAdmin := who == account.AdminName; (ov.Quota == nil) != isAdmin {
			t.Errorf("%s: Quota = %+v; chỉ user thường có Quota", who, ov.Quota)
		}

		// Các danh sách mà core không lọc theo chủ: server lọc (ownedDomains).
		ssl := m13Decode[SSLOverview](t, get("/api/v1/ssl", who), 200)
		if got := slices.ContainsFunc(ssl.Sites, func(x core.SSLSiteRow) bool { return x.Domain.String() == authzSite }); got != mine {
			t.Errorf("%s: /ssl có site của A = %v, muốn %v", who, got, mine)
		}
		cron := m13Decode[core.CronOverview](t, get("/api/v1/cron", who), 200)
		if got := slices.ContainsFunc(cron.Sites, func(c core.CronEntry) bool { return c.Domain == authzSite }); got != mine {
			t.Errorf("%s: /cron có site của A = %v, muốn %v", who, got, mine)
		}
		php := m13Decode[core.PHPList](t, get("/api/v1/php", who), 200)
		if got := slices.ContainsFunc(php.Versions, func(v core.PHPVersionRow) bool { return slices.Contains(v.Sites, authzSite) }); got != mine {
			t.Errorf("%s: /php có site của A = %v, muốn %v", who, got, mine)
		}
		if rec := get("/api/v1/export", who); rec.Code != 200 || strings.Contains(rec.Body.String(), authzSite) != mine {
			t.Errorf("%s: /export: %d %s", who, rec.Code, rec.Body)
		}
		trash := m13Decode[TrashOverview](t, get("/api/v1/trash", who), 200)
		if got := slices.ContainsFunc(trash.DBs, func(d account.Database) bool { return d.Name == authzTrashedDB }); got != mine {
			t.Errorf("%s: /trash có DB rời của A = %v, muốn %v", who, got, mine)
		}
	}
	// ?user= chỉ admin dùng được; user thường gửi thì vẫn chỉ thấy job của mình.
	if jobs := m13Decode[[]engine.Job](t, get("/api/v1/jobs?user=usera", authzB), 200); len(jobs) != 0 {
		t.Errorf("B lọc ?user=usera vẫn phải không thấy gì, có %d", len(jobs))
	}
}

// TestXemNhuThat: vào xem như A, GET thấy đúng của A, POST bị chặn, thoát rồi
// thao tác lại được.
func TestXemNhuThat(t *testing.T) {
	e := newAuthzEnv(t)
	h := e.s.handler(authzDist)
	admin := e.cookies[account.AdminName]

	info := m13Decode[SessionInfo](t, e.do(h, "POST", "/api/v1/users/"+authzA+"/view-as", "", admin), 200)
	if info.User == nil || info.User.Name != authzA || info.User.Role != account.RoleUser || info.User.ViewedBy != account.AdminName {
		t.Fatalf("sau view-as: %+v", info.User)
	}
	sites := m13Decode[[]site.Site](t, e.do(h, "GET", "/api/v1/sites", "", admin), 200)
	if len(sites) != 1 || sites[0].Owner != authzA {
		t.Errorf("xem như A thấy site %+v", sites)
	}
	rec := e.do(h, "POST", "/api/v1/sites", `{"domain":"x.example.com","email":"a@example.com"}`, admin)
	if rec.Code != http.StatusForbidden || errCode(t, rec) != "read_only_view" {
		t.Fatalf("POST trong phiên xem như: %d %s", rec.Code, rec.Body)
	}
	info = m13Decode[SessionInfo](t, e.do(h, "POST", "/api/v1/session/view-as/exit", "", admin), 200)
	if info.User == nil || info.User.Name != account.AdminName || info.User.ViewedBy != "" {
		t.Fatalf("sau khi thoát: %+v", info.User)
	}
	// Thoát rồi: POST tới handler (thân sai nên 400, không còn 403).
	if rec := e.do(h, "POST", "/api/v1/sites", `{`, admin); rec.Code != http.StatusBadRequest {
		t.Errorf("sau khi thoát, POST: mã %d, muốn 400", rec.Code)
	}
}

// TestDangNhapTheoTen: tên lạ ra đúng lời sai mật khẩu, user bị khoá ra 403
// khi đúng mật khẩu và phiên đang mở của họ mất hiệu lực.
func TestDangNhapTheoTen(t *testing.T) {
	e := newAuthzEnv(t)
	h := e.s.handler(authzDist)
	ctx := context.Background()
	hash, err := store.HashPassword(authTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.CreateUser(ctx, account.User{Name: "userc", Role: account.RoleUser, Created: e.now}, hash); err != nil {
		t.Fatal(err)
	}
	login := func(name, pw, ip string) *httptest.ResponseRecorder {
		b, _ := json.Marshal(LoginRequest{Name: name, Password: pw})
		r := httptest.NewRequest("POST", "/api/v1/login", bytes.NewReader(b))
		r.RemoteAddr = ip + ":40000"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}

	unknown := login("khongco", authTestPassword, "192.0.2.50")
	wrong := login("userc", "sai-mat-khau-dai", "192.0.2.51")
	if unknown.Code != http.StatusUnauthorized || errCode(t, unknown) != "wrong_password" || unknown.Body.String() != wrong.Body.String() {
		t.Errorf("tên lạ phải ra y như sai mật khẩu:\n%d %s\n%d %s", unknown.Code, unknown.Body, wrong.Code, wrong.Body)
	}

	rec := login("userc", authTestPassword, "192.0.2.52")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("đăng nhập userc: %d %s", rec.Code, rec.Body)
	}
	c := rec.Result().Cookies()[0]
	info := m13Decode[SessionInfo](t, e.do(h, "GET", "/api/v1/session", "", c), 200)
	if info.User == nil || info.User.Name != "userc" || info.User.Role != account.RoleUser {
		t.Fatalf("GET /session: %+v", info.User)
	}

	// Khoá user: phiên đang mở thành chưa đăng nhập (và bị xoá), đăng nhập lại 403.
	u, _, err := e.st.User(ctx, "userc")
	if err != nil {
		t.Fatal(err)
	}
	u.Status = account.Suspended
	if err := e.st.UpdateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	if rec := e.do(h, "GET", "/api/v1/sites", "", c); rec.Code != http.StatusUnauthorized {
		t.Errorf("phiên của user bị khoá: mã %d, muốn 401", rec.Code)
	}
	if list, _ := e.st.SessionsOf(ctx, "userc"); len(list) != 0 {
		t.Errorf("phiên của user bị khoá phải bị xoá, còn %d", len(list))
	}
	if rec := login("userc", authTestPassword, "192.0.2.53"); rec.Code != http.StatusForbidden || errCode(t, rec) != core.CodeUserSuspended {
		t.Errorf("đăng nhập user bị khoá: %d %s", rec.Code, rec.Body)
	}
	if rec := login("userc", "sai-mat-khau-dai", "192.0.2.54"); rec.Code != http.StatusUnauthorized {
		t.Errorf("user bị khoá, sai mật khẩu: mã %d, muốn 401 (không lộ là đang khoá)", rec.Code)
	}
}

// TestTaoSiteCuaNguoiKhac: lỗ của POST /sites (route any, core chạy lại site
// đã có mà bỏ qua chủ). Cả xem trước lẫn chạy thật.
func TestTaoSiteCuaNguoiKhac(t *testing.T) {
	e := newAuthzEnv(t)
	h := e.s.handler(authzDist)
	b := e.cookies[authzB]
	for _, q := range []string{"", "?dry_run=1"} {
		rec := e.do(h, "POST", "/api/v1/sites"+q, `{"domain":"`+authzSite+`","email":"b@example.com"}`, b)
		if rec.Code != http.StatusNotFound || errCode(t, rec) != "site_not_found" {
			t.Errorf("B chạy lại site của A%s: %d %s", q, rec.Code, rec.Body)
		}
		// Cùng mã HTTP và mã lỗi với site không có; câu chữ là của việc tạo
		// site (siteCreateRefused) và không nhắc A.
		if strings.Contains(rec.Body.String(), authzA) {
			t.Errorf("lời từ chối nhắc tên chủ site: %s", rec.Body)
		}
		rec = e.do(h, "POST", "/api/v1/sites"+q, `{"domain":"new.example.com","email":"b@example.com","owner":"`+authzA+`"}`, b)
		if rec.Code != http.StatusForbidden || errCode(t, rec) != "forbidden" {
			t.Errorf("B tạo site cho A%s: %d %s", q, rec.Code, rec.Body)
		}
		rec = e.do(h, "POST", "/api/v1/dbs"+q, `{"name":"x","owner":"`+authzA+`"}`, b)
		if rec.Code != http.StatusForbidden || errCode(t, rec) != "forbidden" {
			t.Errorf("B tạo DB rời cho A%s: %d %s", q, rec.Code, rec.Body)
		}
	}
	// Nhật ký ghi người thật đã làm (cột actor).
	db, err := sql.Open("sqlite", e.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var actor, result string
	if err := db.QueryRow("SELECT actor, result FROM audit WHERE action = 'site.add' ORDER BY rowid LIMIT 1").Scan(&actor, &result); err != nil {
		t.Fatal(err)
	}
	if actor != authzB || result != "site_not_found" {
		t.Errorf("audit site.add: actor %q result %q, muốn %q site_not_found", actor, result, authzB)
	}
}

// TestKhoiPhucBackupSangSiteNguoiKhac: backup sống lâu hơn site. Site của A
// đã xoá hẳn, B tạo lại cùng domain: bản backup (vẫn của A) không được ghi đè
// site của B, dù guard owner:backup cho A qua.
func TestKhoiPhucBackupSangSiteNguoiKhac(t *testing.T) {
	e := newAuthzEnv(t)
	h := e.s.handler(authzDist)
	if err := e.st.SaveSite(context.Background(), site.Site{
		Domain: apiDomain(t, authzSite), Status: site.Active, PHPVersion: "8.3", Created: e.now, Owner: authzB,
	}); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"", "?dry_run=1"} {
		rec := e.do(h, "POST", "/api/v1/backups/"+e.backup+"/restore"+q, "", e.cookies[authzA])
		if rec.Code != http.StatusNotFound || errCode(t, rec) != "backup_not_found" {
			t.Errorf("A khôi phục bản của mình vào site của B%s: %d %s", q, rec.Code, rec.Body)
		}
	}
}

// TestXemNhuKhongLayMatKhau: GET job trong phiên "xem như" không lấy mất mật
// khẩu hiện một lần của chủ.
func TestXemNhuKhongLayMatKhau(t *testing.T) {
	e := newAuthzEnv(t)
	h := e.s.handler(authzDist)
	e.s.api.secrets[authzJob] = heldSecret{db: &DBSecret{DB: authzDB, User: authzDB, Password: "bi-mat"}}
	view := e.session(account.AdminName, authzA)
	if d := m13Decode[JobDetail](t, e.do(h, "GET", "/api/v1/jobs/"+authzJob, "", view), 200); d.DBSecret != nil {
		t.Fatal("phiên xem như không được thấy mật khẩu")
	}
	if d := m13Decode[JobDetail](t, e.do(h, "GET", "/api/v1/jobs/"+authzJob, "", e.cookies[authzA]), 200); d.DBSecret == nil || d.DBSecret.Password != "bi-mat" {
		t.Fatalf("A phải còn nhận được mật khẩu: %+v", d.DBSecret)
	}
}

// TestJobGhiNguoiChay: ctx của job giữ người chạy mà guard gắn (engine.WithActor),
// không bị cắt khi request xong.
func TestJobGhiNguoiChay(t *testing.T) {
	e := apiNewEnv(t)
	got := make(chan string, 1)
	r := httptest.NewRequest("POST", "/", nil)
	r = r.WithContext(engine.WithActor(r.Context(), authzA))
	if !e.s.api.claim() {
		t.Fatal("claim")
	}
	e.s.jobStart(httptest.NewRecorder(), r, jobOpts{action: "x"}, func(ctx context.Context, onEvent func(engine.Event)) (jobResult, error) {
		got <- engine.ActorFrom(ctx)
		onEvent(engine.Event{JobID: "j1", Seq: 1, Kind: engine.EventJobStart})
		return jobResult{status: engine.JobSucceeded}, nil
	})
	if a := <-got; a != authzA {
		t.Errorf("actor trong job = %q, muốn %q", a, authzA)
	}
}

// TestCoLapKhongLoTenSiteNguoiKhac: phép kiểm cô lập "isolation:<x>:<y>" chạy
// cho mọi cặp site, nên kết quả trên site của A nêu tên site của B. User
// thường không được thấy chúng ở tổng quan, sự cố, chi tiết job và SSE (e2e
// M13 trên máy ảo: A thấy tên site của B và của admin). Cặp cả hai site đều của
// A vẫn hiện; admin thấy hết.
func TestCoLapKhongLoTenSiteNguoiKhac(t *testing.T) {
	e := newAuthzEnv(t)
	h := e.s.handler(authzDist)
	ctx := context.Background()
	const a2, bSite = "a2.example.com", "b.example.com"
	for _, x := range []struct{ d, owner string }{{a2, authzA}, {bSite, authzB}} {
		d, err := validate.ParseDomain(x.d)
		if err != nil {
			t.Fatal(err)
		}
		if err := e.st.SaveSite(ctx, site.Site{Domain: d, Status: site.Active, PHPVersion: "8.3", Created: e.now, Owner: x.owner}); err != nil {
			t.Fatal(err)
		}
	}
	mine := "isolation:" + authzSite + ":" + a2
	foreign := "isolation:" + authzSite + ":" + bSite
	res := func(id string, st checks.Status) checks.Result {
		return checks.Result{ID: id, Stage: checks.StageWordPress, Status: st, Title: i18n.T(id, id)}
	}
	// Phép cô lập với site của B hỏng: mở một sự cố trên site của A.
	tr, err := e.st.RecordResults(ctx, authzSite, []checks.Result{res(mine, checks.Pass), res(foreign, checks.Fail)}, e.now)
	if err != nil || len(tr.Opened) != 1 {
		t.Fatalf("RecordResults: %+v, %v", tr, err)
	}
	inc := strconv.FormatInt(tr.Opened[0].ID, 10)
	for i, id := range []string{mine, foreign} {
		c := res(id, checks.Pass)
		if err := e.st.AddEvent(ctx, engine.Event{JobID: authzJob, Seq: i + 1, Time: e.now, Kind: engine.EventCheck, Check: &c}); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.st.AddEvent(ctx, engine.Event{JobID: authzJob, Seq: 3, Time: e.now, Kind: engine.EventJobDone, Message: "succeeded"}); err != nil {
		t.Fatal(err)
	}

	for _, who := range []string{authzA, account.AdminName} {
		admin := who == account.AdminName
		c := e.cookies[who]
		for _, path := range []string{"/api/v1/overview", "/api/v1/incidents", "/api/v1/jobs/" + authzJob, "/api/v1/jobs/" + authzJob + "/events"} {
			rec := e.do(h, "GET", path, "", c)
			body := rec.Body.String()
			if rec.Code != 200 {
				t.Fatalf("%s %s: %d %s", who, path, rec.Code, body)
			}
			if got := strings.Contains(body, bSite); got != admin {
				t.Errorf("%s %s: có tên site của B = %v, muốn %v", who, path, got, admin)
			}
			// Cặp của chính A vẫn hiện (bộ lọc không giấu quá tay). Danh sách
			// sự cố chỉ có sự cố của cặp với B.
			if path != "/api/v1/incidents" && !strings.Contains(body, mine) {
				t.Errorf("%s %s: mất phép cô lập %s", who, path, mine)
			}
		}
		rec := e.do(h, "GET", "/api/v1/incidents/"+inc, "", c)
		if want := map[bool]int{true: 200, false: 404}[admin]; rec.Code != want {
			t.Errorf("%s GET /incidents/%s: %d, muốn %d", who, inc, rec.Code, want)
		}
	}
	// SSE: sự kiện bị giấu không gửi nhưng job.done vẫn tới.
	if body := e.do(h, "GET", "/api/v1/jobs/"+authzJob+"/events", "", e.cookies[authzA]).Body.String(); !strings.Contains(body, "event: job.done") || strings.Contains(body, "id: 2\n") {
		t.Errorf("SSE của A: %s", body)
	}
}
