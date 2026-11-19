package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/health"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// m12PHP cho FakeSystem trả lời như máy có PHP 8.3 của Ubuntu đã cài và kho
// biết thêm 8.4 (cũng của Ubuntu, để Plan cài không cần thêm kho sury). curl
// tới php.net và WordPress.org trả JSON mẫu; trả về con trỏ đếm số lần gọi curl.
func (e *apiEnv) m12PHP() *int {
	e.f.SetDir("/etc/php/8.3/fpm/pool.d", 0o755, system.Root)
	e.f.SetFile("/usr/sbin/php-fpm8.3", []byte("ELF"), 0o755, system.Root)
	e.f.SetFile("/etc/os-release", []byte("ID=ubuntu\nVERSION_CODENAME=noble\n"), 0o644, system.Root)
	e.f.Handle([]string{"apt-cache", "search"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "php8.3-fpm - FPM-CGI binary\nphp8.4-fpm - FPM-CGI binary\n"}
	})
	e.f.Handle([]string{"apt-cache", "madison"}, func(c system.Cmd) system.Result {
		var b strings.Builder
		for _, p := range c.Args[1:] {
			b.WriteString(" " + p + " | 8.x-0ubuntu1 | http://archive.ubuntu.com/ubuntu noble-updates/universe amd64 Packages\n")
		}
		return system.Result{Stdout: b.String()}
	})
	curls := new(int)
	e.f.Handle([]string{"curl", "-fsS"}, func(c system.Cmd) system.Result {
		e.mu.Lock()
		*curls++
		e.mu.Unlock()
		switch u := c.Args[len(c.Args)-1]; {
		case strings.Contains(u, "php.net"):
			return system.Result{Stdout: `{"8":{"8.3":{"state":"security","active_support_end":"2025-12-31T00:00:00+00:00","security_support_end":"2027-12-31T00:00:00+00:00"}}}`}
		case strings.Contains(u, "serve-happy"):
			return system.Result{Stdout: `{"recommended_version":"8.3","minimum_version":"7.4","is_supported":true}`}
		}
		return system.Result{ExitCode: 22}
	})
	return curls
}

func (e *apiEnv) curlCount(n *int) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return *n
}

// m12Apt cho `apt-get -s ... dist-upgrade` và mô phỏng nâng gói trả out.
func (e *apiEnv) m12Apt(out string) {
	e.f.Handle([]string{"apt-get", "-s"}, func(system.Cmd) system.Result { return system.Result{Stdout: out} })
}

const m12Pending = "Inst nginx [1.24.0-2ubuntu7.4] (1.24.0-2ubuntu7.5 Ubuntu:24.04/noble-updates [amd64])\n" +
	"Inst curl [8.5.0-2ubuntu10.6] (8.5.0-2ubuntu10.7 Ubuntu:24.04/noble-updates, Ubuntu:24.04/noble-security [amd64])\n" +
	"Inst tzdata [2025b-0ubuntu0.24.04] (2025b-0ubuntu0.24.04.1 Ubuntu:24.04/noble-updates [all])\n"

// jsonKeys là các khoá của một object JSON, sắp theo chữ.
func jsonKeys(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("không phải object: %s", raw)
	}
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	slices.Sort(ks)
	return ks
}

func TestM12RoutesNeedSession(t *testing.T) {
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/v1/php"},
		{"POST", "/api/v1/php/install"},
		{"POST", "/api/v1/php/remove"},
		{"POST", "/api/v1/sites/a.example.com/php"},
		{"GET", "/api/v1/updates"},
		{"POST", "/api/v1/updates"},
		{"POST", "/api/v1/updates/auto"},
		{"POST", "/api/v1/mariadb/upgrade"},
	} {
		rec := httptest.NewRecorder()
		testHandler().ServeHTTP(rec, httptest.NewRequest(c.method, c.path, strings.NewReader("{}")))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: mã = %d, muốn 401", c.method, c.path, rec.Code)
		}
	}
}

// ---------------------------------------------------------------- GET /php

func TestAPIPHPList(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	e.m12PHP()
	e.addSite(t, "shop.example.com")

	rec := apiCall(e.s.handlePHP, "GET", "/api/v1/php", "")
	list := apiDecode[core.PHPList](t, rec, 200)
	if len(list.Versions) != 2 || list.Versions[0].Version != "8.3" || list.Versions[1].Version != "8.4" {
		t.Fatalf("Versions = %+v", list.Versions)
	}
	if v := list.Versions[0]; !v.Installed || !v.Default || !slices.Equal(v.Sites, []string{"shop.example.com"}) || v.Support == nil || v.Support.State != "security" {
		t.Errorf("8.3 = %+v", v)
	}
	if v := list.Versions[1]; v.Installed || v.Default || v.Sites == nil {
		t.Errorf("8.4 = %+v", v)
	}
	if !list.NotesOK || list.WPRecommended != "8.3" {
		t.Errorf("NotesOK=%v WPRecommended=%q", list.NotesOK, list.WPRecommended)
	}

	// Khung JSON khớp web/src/api/types.ts (PHPList, PHPVersionRow, PHPSupport).
	var raw struct {
		Versions []json.RawMessage `json:"versions"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	if got := jsonKeys(t, rec.Body.Bytes()); !slices.Equal(got, []string{"notes_ok", "sury_enabled", "versions", "wp_recommended"}) {
		t.Errorf("khoá của PHPList = %v", got)
	}
	if got := jsonKeys(t, raw.Versions[0]); !slices.Equal(got, []string{"default", "installed", "sites", "source", "support", "version"}) {
		t.Errorf("khoá của PHPVersionRow = %v", got)
	}
	if !strings.Contains(string(raw.Versions[1]), `"sites":[]`) {
		t.Errorf("sites rỗng phải là [], không phải null: %s", raw.Versions[1])
	}
}

// Ghi chú gọi mạng (5 giây mỗi nguồn): tải trang lần hai trong một giờ không
// gọi curl, nhưng danh sách bản đã cài luôn mới.
func TestAPIPHPNotesCached(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	curls := e.m12PHP()

	apiDecode[core.PHPList](t, apiCall(e.s.handlePHP, "GET", "/", ""), 200)
	if n := e.curlCount(curls); n != 2 {
		t.Fatalf("lần đầu gọi curl %d lần, muốn 2 (php.net, WordPress.org)", n)
	}

	// 8.4 vừa cài xong (job php.install): lần tải sau thấy ngay, không gọi mạng.
	e.f.SetDir("/etc/php/8.4/fpm/pool.d", 0o755, system.Root)
	e.f.SetFile("/usr/sbin/php-fpm8.4", []byte("ELF"), 0o755, system.Root)
	e.advance(30 * time.Minute)
	list := apiDecode[core.PHPList](t, apiCall(e.s.handlePHP, "GET", "/", ""), 200)
	if n := e.curlCount(curls); n != 2 {
		t.Errorf("lần hai trong một giờ vẫn gọi curl (tổng %d)", n)
	}
	if !list.Versions[1].Installed || !list.NotesOK || list.WPRecommended != "8.3" || list.Versions[0].Support == nil {
		t.Errorf("lần hai thiếu ghi chú hoặc danh sách cũ: %+v", list)
	}

	// Quá một giờ thì hỏi lại.
	e.advance(31 * time.Minute)
	apiDecode[core.PHPList](t, apiCall(e.s.handlePHP, "GET", "/", ""), 200)
	if n := e.curlCount(curls); n != 4 {
		t.Errorf("quá một giờ mà không hỏi lại (curl %d lần, muốn 4)", n)
	}

	// Bản mặc định đổi thì ghi chú WordPress (hỏi theo bản mặc định) phải hỏi lại.
	if err := e.st.SetSetting(context.Background(), core.SettingPHPVersion, "8.4"); err != nil {
		t.Fatal(err)
	}
	apiDecode[core.PHPList](t, apiCall(e.s.handlePHP, "GET", "/", ""), 200)
	if n := e.curlCount(curls); n != 6 {
		t.Errorf("bản mặc định đổi mà dùng ghi chú cũ (curl %d lần, muốn 6)", n)
	}
}

// Không mạng: danh sách vẫn đủ, notes_ok false; lần sau trong vài phút không chờ mạng lại.
func TestAPIPHPNotesOffline(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	e.m12PHP()
	curls := 0
	e.f.Handle([]string{"curl", "-fsS"}, func(system.Cmd) system.Result {
		e.mu.Lock()
		curls++
		e.mu.Unlock()
		return system.Result{ExitCode: 28, Stderr: "curl: (28) timed out\n"}
	})
	list := apiDecode[core.PHPList](t, apiCall(e.s.handlePHP, "GET", "/", ""), 200)
	if list.NotesOK || len(list.Versions) != 2 {
		t.Fatalf("không mạng: %+v", list)
	}
	apiDecode[core.PHPList](t, apiCall(e.s.handlePHP, "GET", "/", ""), 200)
	if n := e.curlCount(&curls); n != 2 {
		t.Errorf("không mạng: lần hai vẫn chờ mạng (curl %d lần)", n)
	}
	e.advance(phpNotesRetry + time.Second)
	apiDecode[core.PHPList](t, apiCall(e.s.handlePHP, "GET", "/", ""), 200)
	if n := e.curlCount(&curls); n != 4 {
		t.Errorf("không mạng: sau %v phải thử lại (curl %d lần)", phpNotesRetry, n)
	}
}

// ---------------------------------------------------------------- POST PHP

func TestAPIPHPBadInput(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	e.m12PHP()
	type call struct {
		name string
		h    http.HandlerFunc
		path []string
	}
	calls := []call{
		{"install", e.s.handlePHPInstall, nil},
		{"remove", e.s.handlePHPRemove, nil},
		{"site.php", e.s.handleSitePHP, []string{"domain", "shop.example.com"}},
	}
	for _, c := range calls {
		for _, body := range []string{`{`, `{"version":"8.4","x":1}`, `{"version":8.4}`, `[]`} {
			if got := apiErrCode(t, apiCall(c.h, "POST", "/", body, c.path...), 400); got != "bad_request" {
				t.Errorf("%s %s: code = %q", c.name, body, got)
			}
		}
		// Bản PHP sai là lời từ chối của core: 409 kèm mã, không chạy job.
		for _, v := range []string{"8.3; rm -rf /", "$(id)", "../../etc/passwd", "8.3\n8.4", "", strings.Repeat("8", 300)} {
			b, _ := json.Marshal(PHPVersionBody{Version: v})
			for _, q := range []string{"/", "/?dry_run=1"} {
				if got := apiErrCode(t, apiCall(c.h, "POST", q, string(b), c.path...), 409); got != "php_version_invalid" {
					t.Errorf("%s %q %s: code = %q", c.name, v, q, got)
				}
			}
		}
	}
	for _, dom := range []string{"a.com; rm -rf /", "$(id).example.com", "../../etc/passwd", strings.Repeat("a", 300) + ".com"} {
		if got := apiErrCode(t, apiCall(e.s.handleSitePHP, "POST", "/", `{"version":"8.4"}`, "domain", dom), 400); got != validate.CodeDomainInvalid {
			t.Errorf("domain %q: code = %q", dom, got)
		}
	}
	if jobs, _ := e.st.Jobs(context.Background(), 0); len(jobs) != 0 {
		t.Fatalf("đầu vào sai mà có job: %v", jobs)
	}
	if a := hAudit(t, e, "php.install"); len(a) == 0 || a[0].Result != "php_version_invalid" {
		t.Errorf("audit php.install = %+v", a)
	}
}

func TestAPIPHPRefusals(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	e.m12PHP()
	e.addSite(t, "shop.example.com")
	for _, c := range []struct {
		name string
		h    http.HandlerFunc
		body string
		path []string
		code string
	}{
		{"gỡ bản mặc định", e.s.handlePHPRemove, `{"version":"8.3"}`, nil, "php_default"},
		{"site sang bản chưa cài", e.s.handleSitePHP, `{"version":"8.4"}`, []string{"domain", "shop.example.com"}, "php_not_installed"},
		{"site đã ở bản đó", e.s.handleSitePHP, `{"version":"8.3"}`, []string{"domain", "shop.example.com"}, "php_same_version"},
	} {
		if got := apiErrCode(t, apiCall(c.h, "POST", "/", c.body, c.path...), 409); got != c.code {
			t.Errorf("%s: code = %q, muốn %s", c.name, got, c.code)
		}
	}
	if got := apiErrCode(t, apiCall(e.s.handleSitePHP, "POST", "/", `{"version":"8.4"}`, "domain", "khong-co.example.com"), 404); got != "site_not_found" {
		t.Errorf("site không có: code = %q", got)
	}
	if a := hAudit(t, e, "site.php"); len(a) != 3 || a[0].Target != "shop.example.com" || a[0].Result != "php_not_installed" {
		t.Errorf("audit site.php = %+v", a)
	}
}

func TestAPIPHPInstallDryRunAndJob(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	e.m12PHP()
	p := apiDecode[engine.Preview](t, apiCall(e.s.handlePHPInstall, "POST", "/?dry_run=1", `{"version":"8.4"}`), 200)
	if p.Kind != "php.install" || len(p.Steps) == 0 {
		t.Fatalf("Preview = %+v", p)
	}
	if jobs, _ := e.st.Jobs(context.Background(), 0); len(jobs) != 0 {
		t.Fatalf("xem trước mà có job: %v", jobs)
	}

	js := apiDecode[JobStarted](t, apiCall(e.s.handlePHPInstall, "POST", "/", `{"version":"8.4"}`), 202)
	e.s.api.wg.Wait()
	job, err := e.st.Job(context.Background(), js.JobID)
	if err != nil || job.Kind != "php.install" || job.Target != "8.4" || job.Status == engine.JobRunning {
		t.Fatalf("job = %+v, %v", job, err)
	}
	if a := hAudit(t, e, "php.install"); len(a) != 1 || a[0].JobID != js.JobID || a[0].Target != "8.4" {
		t.Errorf("audit = %+v", a)
	}

	// Đang có job thì 409 job_running.
	e.s.api.claim()
	defer e.s.api.release()
	if got := apiErrCode(t, apiCall(e.s.handlePHPInstall, "POST", "/", `{"version":"8.4"}`), 409); got != "job_running" {
		t.Fatalf("code = %q", got)
	}
}

// ---------------------------------------------------------------- cập nhật

func TestAPIUpdatesStatus(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	e.m12Apt(m12Pending)
	rec := apiCall(e.s.handleUpdates, "GET", "/api/v1/updates", "")
	st := apiDecode[core.UpdateStatus](t, rec, 200)
	if len(st.Packages) != 2 || st.Packages[0].Name != "curl" || !st.Packages[0].Security || st.Others != 1 || st.OthersSecurity != 0 {
		t.Errorf("UpdateStatus = %+v", st)
	}
	if got := jsonKeys(t, rec.Body.Bytes()); !slices.Equal(got, []string{"auto", "auto_origins", "mariadb", "others", "others_security", "packages", "self"}) {
		t.Errorf("khoá của UpdateStatus = %v", got)
	}
	var raw struct {
		Packages []json.RawMessage `json:"packages"`
		MariaDB  json.RawMessage   `json:"mariadb"`
		Self     json.RawMessage   `json:"self"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	if got := jsonKeys(t, raw.Packages[0]); !slices.Equal(got, []string{"from", "name", "origins", "security", "to"}) {
		t.Errorf("khoá của PendingPackage = %v", got)
	}
	if got := jsonKeys(t, raw.MariaDB); !slices.Equal(got, []string{"line", "source", "upgrades", "version"}) {
		t.Errorf("khoá của MariaDBStatus = %v", got)
	}
	if got := jsonKeys(t, raw.Self); !slices.Equal(got, []string{"has_old", "url", "version"}) {
		t.Errorf("khoá của SelfStatus = %v", got)
	}
	// Không có apt-get update nào (GET không làm mới danh sách gói, không lấy khoá).
	for _, c := range e.f.Ran() {
		if c.Name == "apt-get" && slices.Contains(c.Args, "update") {
			t.Errorf("GET /updates chạy %s", c.String())
		}
	}

	// Máy không có gì chờ: mọi danh sách là [] chứ không null.
	e.m12Apt("")
	body := apiCall(e.s.handleUpdates, "GET", "/", "").Body.String()
	for _, k := range []string{`"packages":[]`, `"auto_origins":[]`, `"upgrades":[]`} {
		if !strings.Contains(body, k) {
			t.Errorf("thiếu %s trong %s", k, body)
		}
	}
}

func TestAPIUpdateApply(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	e.m12Apt("")
	if got := apiErrCode(t, apiCall(e.s.handleUpdateApply, "POST", "/", ""), 409); got != "nothing_to_update" {
		t.Fatalf("code = %q", got)
	}
	if a := hAudit(t, e, "update.apply"); len(a) != 1 || a[0].Result != "nothing_to_update" {
		t.Errorf("audit = %+v", a)
	}

	e.m12Apt(m12Pending)
	p := apiDecode[engine.Preview](t, apiCall(e.s.handleUpdateApply, "POST", "/?dry_run=1", ""), 200)
	if p.Kind != "update.apply" {
		t.Fatalf("Preview = %+v", p)
	}

	js := apiDecode[JobStarted](t, apiCall(e.s.handleUpdateApply, "POST", "/", ""), 202)
	e.s.api.wg.Wait()
	job, err := e.st.Job(context.Background(), js.JobID)
	if err != nil || job.Kind != "update.apply" || job.Status == engine.JobRunning {
		t.Fatalf("job = %+v, %v", job, err)
	}
	// Sau job đổi gói, health đầy đủ đã chạy (nó ghi lần chạy đầy đủ gần nhất).
	if v, ok, _ := e.st.GetSetting(context.Background(), health.SettingLastRun); !ok || v == "" {
		t.Error("health đầy đủ chưa chạy sau update.apply")
	}
}

func TestAPIUpdateAuto(t *testing.T) {
	e := apiNewEnv(t)
	p := apiDecode[engine.Preview](t, apiCall(e.s.handleUpdateAuto, "POST", "/?dry_run=1", ""), 200)
	if p.Kind != "update.auto" || len(p.Steps) != 2 {
		t.Fatalf("Preview = %+v", p)
	}
	js := apiDecode[JobStarted](t, apiCall(e.s.handleUpdateAuto, "POST", "/", ""), 202)
	e.s.api.wg.Wait()
	if job, err := e.st.Job(context.Background(), js.JobID); err != nil || job.Kind != "update.auto" {
		t.Fatalf("job = %+v, %v", job, err)
	}
	if a := hAudit(t, e, "update.auto"); len(a) != 1 || a[0].JobID != js.JobID {
		t.Errorf("audit = %+v", a)
	}
}

func TestAPIMariaDBUpgrade(t *testing.T) {
	e := apiNewEnv(t)
	e.stack(t)
	e.f.SetFile("/etc/os-release", []byte("ID=ubuntu\nVERSION_CODENAME=noble\n"), 0o644, system.Root)
	e.f.Handle([]string{"dpkg-query", "-W"}, func(c system.Cmd) system.Result {
		if slices.Contains(c.Args, "mariadb-server") {
			return system.Result{Stdout: "installed\t1:10.11.13-0ubuntu0.24.04.1"}
		}
		return system.Result{Stdout: "mariadb-server\tinstalled\nmariadb-client\tinstalled\n"}
	})
	for _, body := range []string{`{`, `{"line":"11.8","x":1}`, `{"line":11.8}`} {
		if got := apiErrCode(t, apiCall(e.s.handleMariaDBUpgrade, "POST", "/", body), 400); got != "bad_request" {
			t.Errorf("%s: code = %q", body, got)
		}
	}
	for line, code := range map[string]string{"11.8; rm -rf /": "mariadb_line_invalid", "": "mariadb_line_invalid", "10.6": "mariadb_not_newer", "10.11": "mariadb_not_newer"} {
		b, _ := json.Marshal(MariaDBUpgradeBody{Line: line})
		if got := apiErrCode(t, apiCall(e.s.handleMariaDBUpgrade, "POST", "/", string(b)), 409); got != code {
			t.Errorf("%q: code = %q, muốn %s", line, got, code)
		}
	}
	p := apiDecode[engine.Preview](t, apiCall(e.s.handleMariaDBUpgrade, "POST", "/?dry_run=1", `{"line":"11.8"}`), 200)
	if p.Kind != "mariadb.upgrade" || p.Target != "11.8" {
		t.Fatalf("Preview = %+v", p)
	}
	if jobs, _ := e.st.Jobs(context.Background(), 0); len(jobs) != 0 {
		t.Fatalf("từ chối và xem trước mà có job: %v", jobs)
	}
	if a := hAudit(t, e, "mariadb.upgrade"); len(a) != 4 {
		t.Errorf("audit mariadb.upgrade có %d dòng, muốn 4 (mỗi lời từ chối một dòng)", len(a))
	}
}
