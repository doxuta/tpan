package server

// Test lớp phân quyền (mục 6.12): duyệt MỌI route trong bảng routes() với ba
// vai, handler thật được thay bằng handler giả nên chỉ kiểm guard. Thêm route
// mà quên luật thì mount panic (TestMountRouteThieuLuat), và route mới tự vào
// vòng duyệt ở đây; placeholder lạ trong mẫu làm test đỏ cho tới khi có giá trị.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/apitoken"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// Ba vai của test: admin, user chủ (A) và user khác (B).
const (
	authzA      = "usera"
	authzB      = "userb"
	authzSite   = "a.example.com"
	authzDB     = "usera_shop"
	authzJob    = "00000000000000aa"
	authzStatus = 299 // handler giả trả mã này: "đã tới handler"
)

// authzEnv: store thật có dữ liệu mẫu của A, ba phiên dựng thẳng trong store
// (không đăng nhập, khỏi tốn argon2 mỗi vai).
type authzEnv struct {
	t        *testing.T
	st       *store.Store
	s        *Server
	f        *system.FakeSystem
	now      time.Time
	cookies  map[string]*http.Cookie // theo tên user
	incident string
	backup   string
	dbPath   string
}

func newAuthzEnv(t *testing.T) *authzEnv {
	t.Helper()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "tpan.db")
	st, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	e := &authzEnv{t: t, st: st, f: system.NewFake(), now: authT0, cookies: map[string]*http.Cookie{}, dbPath: dbPath}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(st.SetAdminPasswordHash(ctx, "x"))
	for _, n := range []string{authzA, authzB} {
		must(st.CreateUser(ctx, account.User{Name: n, Role: account.RoleUser, Created: e.now}, "x"))
	}
	d, err := validate.ParseDomain(authzSite)
	must(err)
	must(st.SaveSite(ctx, site.Site{Domain: d, Status: site.Active, PHPVersion: "8.3", Created: e.now, Owner: authzA}))
	must(st.SaveStandaloneDB(ctx, account.Database{
		Name: authzDB, Owner: authzA, Charset: "utf8mb4", Collation: "utf8mb4_unicode_ci",
		Status: account.DBActive, Created: e.now,
	}))
	// Job trên site của A: store suy chủ từ target.
	must(st.CreateJob(ctx, engine.Job{ID: authzJob, Kind: "site.add", Target: authzSite, Status: engine.JobSucceeded, Started: e.now}))
	b, err := st.AddBackup(ctx, store.Backup{Domain: authzSite, Kind: site.BackupFull, Path: "/var/backups/tpan/a.tar.zst", Created: e.now})
	must(err)
	e.backup = b.ID
	tr, err := st.RecordResults(ctx, authzSite, []checks.Result{{
		ID: "http.ok:" + authzSite, Stage: checks.StageEndToEnd, Status: checks.Fail,
		Title: i18n.T("site trả 200", "site returns 200"),
	}}, e.now)
	must(err)
	if len(tr.Opened) != 1 {
		t.Fatalf("muốn mở 1 sự cố, có %d", len(tr.Opened))
	}
	e.incident = strconv.FormatInt(tr.Opened[0].ID, 10)

	for _, n := range []string{account.AdminName, authzA, authzB} {
		e.cookies[n] = e.session(n, "")
	}
	e.s = newServer(Deps{Sys: e.f, Store: st, Hostname: "may-thu", Now: func() time.Time { return e.now }})
	t.Cleanup(e.s.api.wg.Wait)
	return e
}

// session ghi thẳng một phiên của user vào store và trả cookie của nó.
func (e *authzEnv) session(user, viewAs string) *http.Cookie {
	e.t.Helper()
	token := "token-" + user + "-" + viewAs
	ss := store.Session{TokenSHA256: authTokenHash(token), ID: "id" + user + viewAs, IP: "192.0.2.1", Created: e.now, UserName: user}
	if err := e.st.CreateSession(context.Background(), ss); err != nil {
		e.t.Fatal(err)
	}
	if viewAs != "" {
		if err := e.st.SetViewAs(context.Background(), ss.TokenSHA256, viewAs); err != nil {
			e.t.Fatal(err)
		}
	}
	return &http.Cookie{Name: authCookiePlain, Value: token}
}

// do gửi request qua h với cookie (nil: chưa đăng nhập) và header cùng origin,
// để lớp chống CSRF không che kết quả.
func (e *authzEnv) do(h http.Handler, method, path, body string, c *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.RemoteAddr = "192.0.2.1:40000"
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	if c != nil {
		r.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

var authzDist = fstest.MapFS{"index.html": {Data: []byte("<html>tpan</html>")}}

// fakeRoutes là bảng route thật với handler giả.
func (e *authzEnv) fakeRoutes() (http.Handler, []route) {
	rs := e.s.routes(authzDist)
	for i := range rs {
		rs[i].h = func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(authzStatus) }
	}
	mux := http.NewServeMux()
	e.s.mount(mux, rs)
	return e.s.secure(mux), rs
}

// fill thay placeholder trong đường dẫn: đối tượng của A cho luật owner, giá
// trị bất kỳ cho các luật khác. missing: giá trị không có thật (để so 404).
func (e *authzEnv) fill(t *testing.T, rt route, path string, missing bool) string {
	t.Helper()
	val := map[string]string{"domain": authzSite, "name": authzA, "id": "x1", "seq": "1", "table": "wp_posts"}
	if rt.rule.kind == ruleOwner {
		switch rt.rule.obj {
		case objJob:
			val["id"] = authzJob
		case objIncident:
			val["id"] = e.incident
		case objBackup:
			val["id"] = e.backup
		case objDB:
			val["name"] = authzDB
		}
		if missing {
			val = map[string]string{"domain": "none.example.com", "id": "999999", "name": "userb_none", "table": "wp_posts"}
		}
	}
	for strings.Contains(path, "{") {
		i := strings.Index(path, "{")
		j := strings.Index(path[i:], "}")
		key := path[i+1 : i+j]
		v, ok := val[key]
		if !ok {
			t.Fatalf("route %s: placeholder {%s} chưa có giá trị trong test", rt.pattern, key)
		}
		path = path[:i] + v + path[i+j+1:]
	}
	return path
}

func splitPattern(p string) (method, path string) {
	if m, rest, ok := strings.Cut(p, " "); ok {
		return m, rest
	}
	return http.MethodGet, p
}

// token ghi thẳng một token (M20) của user với phạm vi scopes, sống từ
// created tới expires; trả token thật.
func (e *authzEnv) token(user, scopes string, created, expires time.Time) string {
	e.t.Helper()
	raw := apitoken.New()
	id := authTokenHash(raw)[:16]
	if err := e.st.CreateToken(context.Background(), store.TokenRow{
		ID: id, User: user, Name: "t", Scopes: scopes, Hint: apitoken.Hint(raw), Created: created, Expires: expires,
	}, apitoken.Hash(raw)); err != nil {
		e.t.Fatal(err)
	}
	return raw
}

// doToken gửi request bằng token (header Authorization, không cookie).
func (e *authzEnv) doToken(h http.Handler, method, path, raw string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader("{}"))
	r.RemoteAddr = "192.0.2.1:40000"
	r.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

// scopesExcept là quyền ghi mọi nhóm trừ g ("" là mọi nhóm).
func scopesExcept(g apitoken.Group) string {
	var out []string
	for _, x := range apitoken.Groups {
		if x != g {
			out = append(out, string(x)+":write")
		}
	}
	return strings.Join(out, ",")
}

// TestPhanQuyenMoiRoute duyệt mọi route đã đăng ký với ba vai, và (M20) với
// token: đủ phạm vi (của admin và của A), thiếu phạm vi, hết hạn, thu hồi.
func TestPhanQuyenMoiRoute(t *testing.T) {
	e := newAuthzEnv(t)
	h, rs := e.fakeRoutes()
	later := e.now.Add(time.Hour)
	tokAdmin := e.token(account.AdminName, scopesExcept(""), e.now, later)
	tokA := e.token(authzA, scopesExcept(""), e.now, later)
	tokExpired := e.token(account.AdminName, scopesExcept(""), e.now.Add(-2*time.Hour), e.now.Add(-time.Hour))
	tokRevoked := e.token(account.AdminName, scopesExcept(""), e.now, later)
	if err := e.st.RevokeToken(context.Background(), authTokenHash(tokRevoked)[:16], "", e.now); err != nil {
		t.Fatal(err)
	}
	tokLack := map[apitoken.Group]string{}
	for _, g := range apitoken.Groups {
		tokLack[g] = e.token(account.AdminName, scopesExcept(g), e.now, later)
	}
	if len(rs) < 90 {
		t.Fatalf("bảng route chỉ có %d dòng; có bị bỏ sót không?", len(rs))
	}
	for _, rt := range rs {
		method, raw := splitPattern(rt.pattern)
		path := e.fill(t, rt, raw, false)
		t.Run(rt.pattern, func(t *testing.T) {
			code := func(who string) int {
				var c *http.Cookie
				if who != "" {
					c = e.cookies[who]
				}
				return e.do(h, method, path, "{}", c).Code
			}
			if rt.rule.kind == rulePublic {
				if got := code(""); got != authzStatus {
					t.Fatalf("public, chưa đăng nhập: mã %d, muốn tới handler", got)
				}
				return
			}
			if got := code(""); got != http.StatusUnauthorized {
				t.Errorf("chưa đăng nhập: mã %d, muốn 401", got)
			}
			if got := code(account.AdminName); got != authzStatus {
				t.Errorf("admin: mã %d, muốn tới handler", got)
			}
			wantA, wantB := authzStatus, authzStatus
			switch rt.rule.kind {
			case ruleOwner:
				wantB = http.StatusNotFound
			case ruleAdmin:
				wantA, wantB = http.StatusForbidden, http.StatusForbidden
			}
			if got := code(authzA); got != wantA {
				t.Errorf("A (chủ): mã %d, muốn %d", got, wantA)
			}
			if got := code(authzB); got != wantB {
				t.Errorf("B (user khác): mã %d, muốn %d", got, wantB)
			}
			if rt.rule.kind == ruleOwner {
				// "Của người khác" và "không có" phải ra đúng cùng một câu trả
				// lời, không thì B dò được tên site, DB của người khác.
				other := e.do(h, method, path, "{}", e.cookies[authzB])
				gone := e.do(h, method, e.fill(t, rt, raw, true), "{}", e.cookies[authzB])
				if !sameNotFound(other, gone, e.backup) {
					t.Errorf("B: 404 của đồ người khác khác 404 của đồ không có:\n%s\n%s", other.Body, gone.Body)
				}
			}
			// M20: token. Hết hạn và thu hồi bị chặn trước mọi thứ khác.
			tok := func(raw string, wantCode int, wantErr string) {
				t.Helper()
				rec := e.doToken(h, method, path, raw)
				if rec.Code != wantCode || (wantErr != "" && errCode(t, rec) != wantErr) {
					t.Errorf("token: mã %d %s, muốn %d %s", rec.Code, rec.Body, wantCode, wantErr)
				}
			}
			tok(tokExpired, http.StatusUnauthorized, "token_expired")
			tok(tokRevoked, http.StatusUnauthorized, "token_revoked")
			if rt.tok == "" {
				// Chỉ phiên (B5): token đủ mọi phạm vi vẫn 403.
				tok(tokAdmin, http.StatusForbidden, "token_forbidden")
				tok(tokA, http.StatusForbidden, "token_forbidden")
				return
			}
			tok(tokAdmin, authzStatus, "")
			// Token của A: phạm vi đủ, quyền là của A (B4).
			tok(tokA, wantA, "")
			tok(tokLack[rt.tok], http.StatusForbidden, "token_scope")
		})
	}
}

// sameNotFound: cùng mã và cùng mã lỗi; câu chữ chỉ khác ở chính giá trị trên
// đường dẫn (domain, tên DB, id backup), thứ người gọi đã tự gõ.
func sameNotFound(a, b *httptest.ResponseRecorder, extra ...string) bool {
	var ea, eb ErrorBody
	if json.Unmarshal(a.Body.Bytes(), &ea) != nil || json.Unmarshal(b.Body.Bytes(), &eb) != nil {
		return false
	}
	norm := func(s string) string {
		for _, v := range append([]string{authzSite, "none.example.com", authzDB, "userb_none", "999999"}, extra...) {
			s = strings.ReplaceAll(s, v, "X")
		}
		return s
	}
	return a.Code == b.Code && ea.Error.Code == eb.Error.Code && norm(ea.Error.Message.Vi) == norm(eb.Error.Message.Vi)
}

// TestRouteM18ChiAdmin: mọi route của M18 có trong bảng và là luật admin (B5).
// TestPhanQuyenMoiRoute ở trên đã duyệt chúng với ba vai; test này chặn việc
// một route bị đổi luật hay bị xoá khỏi bảng mà không ai thấy.
func TestRouteM18ChiAdmin(t *testing.T) {
	rules := map[string]rule{}
	for _, rt := range newServer(Deps{}).routes(authzDist) {
		rules[rt.pattern] = rt.rule
	}
	for _, p := range []string{
		"GET /api/v1/backups/target",
		"POST /api/v1/backups/target",
		"DELETE /api/v1/backups/target",
		"POST /api/v1/backups/target/test",
		"POST /api/v1/backups/{id}/push",
		"POST /api/v1/backups/{id}/pull",
		"GET /api/v1/backups/remote",
		"POST /api/v1/cloud-init/render",
	} {
		r, ok := rules[p]
		if !ok {
			t.Errorf("thiếu route %s", p)
			continue
		}
		if r != adminOnly {
			t.Errorf("%s: luật %+v, muốn admin", p, r)
		}
	}
}

// TestAuthzTable (M19, B10): mọi route của M19 có trong bảng với đúng luật.
// Ba vai (admin qua, user khác 404, xem như 403 khi ghi) đã được
// TestPhanQuyenMoiRoute và TestXemNhuChiDoc duyệt cho mọi route; test này chặn
// việc một route M19 bị đổi luật hay bị xoá mà không ai thấy.
func TestAuthzTable(t *testing.T) {
	rules := map[string]rule{}
	for _, rt := range newServer(Deps{}).routes(authzDist) {
		rules[rt.pattern] = rt.rule
	}
	for p, want := range map[string]rule{
		"GET /api/v1/sites/{domain}/names":              ownerSite,
		"POST /api/v1/sites/{domain}/names":             ownerSite,
		"POST /api/v1/sites/{domain}/names/{name}/mode": ownerSite,
		"DELETE /api/v1/sites/{domain}/names/{name}":    ownerSite,
		"GET /api/v1/sites/{domain}/redirects":          ownerSite,
		"POST /api/v1/sites/{domain}/redirects":         ownerSite,
		"DELETE /api/v1/sites/{domain}/redirects/{id}":  ownerSite,
		"GET /api/v1/domains":                           anyone,
	} {
		r, ok := rules[p]
		if !ok {
			t.Errorf("thiếu route %s", p)
			continue
		}
		if r != want {
			t.Errorf("%s: luật %+v, muốn %+v", p, r, want)
		}
	}
}

// TestMountRouteThieuLuat: route quên luật thì server không khởi động.
func TestMountRouteThieuLuat(t *testing.T) {
	s := newServer(Deps{})
	ok := func(http.ResponseWriter, *http.Request) {}
	for name, rt := range map[string]route{
		"không luật":            {pattern: "GET /api/v1/moi", h: ok},
		"owner không đối tượng": {pattern: "GET /api/v1/moi/{x}", rule: rule{kind: ruleOwner}, h: ok},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("mount phải panic với route không khai luật")
				}
			}()
			s.mount(http.NewServeMux(), []route{rt})
		})
	}
}

// TestXemNhuChiDoc: phiên "xem như" chặn mọi request thay đổi ở guard, trừ thoát.
func TestXemNhuChiDoc(t *testing.T) {
	e := newAuthzEnv(t)
	h, rs := e.fakeRoutes()
	view := e.session(account.AdminName, authzA)
	for _, rt := range rs {
		method, raw := splitPattern(rt.pattern)
		if rt.rule.kind == rulePublic || method == http.MethodGet {
			continue
		}
		rec := e.do(h, method, e.fill(t, rt, raw, false), "{}", view)
		want := http.StatusForbidden
		if rt.allowReadOnly {
			want = authzStatus
		}
		if rec.Code != want {
			t.Errorf("%s trong phiên xem như: mã %d, muốn %d", rt.pattern, rec.Code, want)
		}
		if want == http.StatusForbidden && errCode(t, rec) != "read_only_view" {
			t.Errorf("%s: mã lỗi %s, muốn read_only_view", rt.pattern, errCode(t, rec))
		}
	}
	// GET qua, với vai của user được xem: route admin bị 403, đồ của A tới được.
	if rec := e.do(h, "GET", "/api/v1/users", "", view); rec.Code != http.StatusForbidden {
		t.Errorf("xem như A, GET /users: mã %d, muốn 403", rec.Code)
	}
	if rec := e.do(h, "GET", "/api/v1/sites/"+authzSite, "", view); rec.Code != authzStatus {
		t.Errorf("xem như A, GET site của A: mã %d, muốn tới handler", rec.Code)
	}
}
