package core

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

func TestParseAptInst(t *testing.T) {
	out := strings.Join([]string{
		"NOTE: This is only a simulation!",
		"Inst libc6 [2.39-0ubuntu8.9] (2.39-0ubuntu8.12 Ubuntu:24.04/noble-updates, Ubuntu:24.04/noble-security [arm64])",
		"Inst linux-image-6.8.0-90-generic (6.8.0-90.91 Ubuntu:24.04/noble-updates [arm64])",
		"Inst libperl5.38t64 [5.38.2-3.2build2] (5.38.2-3.2ubuntu0.2 Ubuntu:24.04/noble-updates [arm64]) [perl:arm64 ]",
		"Inst libgcc-s1:i386 [14.2.0-4ubuntu2] (14.2.0-4ubuntu2~24.04 Ubuntu:24.04/noble-updates [i386])",
		"Inst libgcc-s1 [14.2.0-4ubuntu2] (14.2.0-4ubuntu2~24.04 Ubuntu:24.04/noble-updates [arm64])",
		"Inst nginx [1.30.6-1~noble] (1.30.7-1~noble nginx:packages.nginx.org [arm64])",
		"Conf libc6 (2.39-0ubuntu8.12 Ubuntu:24.04/noble-updates [arm64])",
		"Inst hỏng không có ngoặc",
		"Inst x [1.0 (2.0 kho [arm64])",
		"",
	}, "\n")
	want := []PendingPackage{
		{Name: "libc6", From: "2.39-0ubuntu8.9", To: "2.39-0ubuntu8.12", Security: true, Origins: []string{"Ubuntu:24.04/noble-updates", "Ubuntu:24.04/noble-security"}},
		{Name: "linux-image-6.8.0-90-generic", To: "6.8.0-90.91", Origins: []string{"Ubuntu:24.04/noble-updates"}},
		{Name: "libperl5.38t64", From: "5.38.2-3.2build2", To: "5.38.2-3.2ubuntu0.2", Origins: []string{"Ubuntu:24.04/noble-updates"}},
		{Name: "libgcc-s1", From: "14.2.0-4ubuntu2", To: "14.2.0-4ubuntu2~24.04", Origins: []string{"Ubuntu:24.04/noble-updates"}},
		{Name: "nginx", From: "1.30.6-1~noble", To: "1.30.7-1~noble", Origins: []string{"nginx:packages.nginx.org"}},
	}
	if got := parseAptInst(out); !reflect.DeepEqual(got, want) {
		t.Errorf("parseAptInst:\n got %+v\nwant %+v", got, want)
	}
	if got := parseAptInst(""); len(got) != 0 {
		t.Errorf("output rỗng: %+v", got)
	}
}

func TestDBLine(t *testing.T) {
	for v, want := range map[string]string{
		"1:10.11.14-0ubuntu0.24.04.1": "10.11",
		"1:11.8.5+maria~ubu2404":      "11.8",
		"11.4.2":                      "11.4",
		"":                            "",
		"lạ":                          "",
	} {
		if got := dbLineOfVersion(v); got != want {
			t.Errorf("dbLineOfVersion(%q) = %q, muốn %q", v, got, want)
		}
	}
	if cmpDBLine("10.11", "10.6") <= 0 || cmpDBLine("11.4", "10.11") <= 0 || cmpDBLine("11.8", "11.8") != 0 || cmpDBLine("10.11", "11.4") >= 0 {
		t.Error("cmpDBLine so sai thứ tự số")
	}
}

// ---------------------------------------------------------------- môi trường giả

const aptSimOut = `Inst nginx [1.24.0-2ubuntu7.4] (1.24.0-2ubuntu7.5 Ubuntu:24.04/noble-updates, Ubuntu:24.04/noble-security [arm64])
Inst nginx-common [1.24.0-2ubuntu7.4] (1.24.0-2ubuntu7.5 Ubuntu:24.04/noble-updates [all])
Inst php8.3-fpm [8.3.6-0ubuntu0.24.04.5] (8.3.6-0ubuntu0.24.04.6 Ubuntu:24.04/noble-security [arm64])
Inst php8.3-cli [8.3.6-0ubuntu0.24.04.5] (8.3.6-0ubuntu0.24.04.6 Ubuntu:24.04/noble-security [arm64])
Inst php8.4-cli [8.4.1-1] (8.4.2-1 deb.sury.org:noble/main [arm64])
Inst mariadb-server [1:10.11.13-0ubuntu0.24.04.1] (1:10.11.14-0ubuntu0.24.04.1 Ubuntu:24.04/noble-updates [arm64])
Inst libcurl4t64 [8.5.0-2ubuntu10.5] (8.5.0-2ubuntu10.6 Ubuntu:24.04/noble-security [arm64])
Inst php8.3-newext (8.3.6-0ubuntu0.24.04.6 Ubuntu:24.04/noble-updates [arm64])
Inst libc6 [2.39-0ubuntu8.9] (2.39-0ubuntu8.12 Ubuntu:24.04/noble-security [arm64])
Inst vim [2:9.1.0016-1ubuntu7.8] (2:9.1.0016-1ubuntu7.9 Ubuntu:24.04/noble-updates [arm64])
`

const mariadbAPI = `{"major_releases":[
 {"release_id":"13.0","release_status":"Stable","release_support_type":"Rolling","release_eol_date":null},
 {"release_id":"12.3","release_status":"Stable","release_support_type":"Long Term Support","release_eol_date":"2029-06-12"},
 {"release_id":"11.8","release_status":"Stable","release_support_type":"Long Term Support","release_eol_date":"2030-02-13"},
 {"release_id":"13.2","release_status":"Alpha","release_support_type":"Long Term Support","release_eol_date":null},
 {"release_id":"10.11","release_status":"Stable","release_support_type":"Long Term Support","release_eol_date":"2028-02-16"},
 {"release_id":"10.6","release_status":"Stable","release_support_type":"Long Term Support","release_eol_date":"2026-07-06"}
]}`

type updEnv struct {
	u     *Updates
	f     *system.FakeSystem
	set   mapSettings
	sites *memSites
}

// newUpdEnv: máy nginx + PHP 8.3 (và 8.4) đã cài, MariaDB 10.11 của Ubuntu,
// apt có gói chờ như aptSimOut, mạng trả danh sách dòng MariaDB.
func newUpdEnv(t *testing.T) *updEnv {
	t.Helper()
	f := system.NewFake()
	f.SetFile("/etc/os-release", []byte("ID=ubuntu\nVERSION_ID=\"24.04\"\nVERSION_CODENAME=noble\n"), 0o644, system.Root)
	f.SetDir("/etc/php/8.3/fpm", 0o755, system.Root)
	f.SetDir("/etc/php/8.4/fpm", 0o755, system.Root)
	f.SetDir("/etc/php/8.2", 0o755, system.Root) // chỉ còn cli: không phải bản FPM
	f.Handle([]string{"apt-get", "-s", "-o", "Debug::NoLocking=1", "dist-upgrade"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: aptSimOut}
	})
	f.Handle([]string{"dpkg-query", "-W", "-f", `${db:Status-Status}\t${Version}`, "mariadb-server"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "installed\t1:10.11.14-0ubuntu0.24.04.1"}
	})
	f.Handle([]string{"curl", "-fsS", "--max-time", "5", mariadbReleasesURL}, func(system.Cmd) system.Result {
		return system.Result{Stdout: mariadbAPI}
	})
	set := mapSettings{SettingWeb: "nginx", SettingPHPVersion: "8.3"}
	ms := newMemSites()
	return &updEnv{u: &Updates{Sys: f, Settings: set, Sites: ms, Version: "v1.0.0"}, f: f, set: set, sites: ms}
}

func (e *updEnv) addSite(t *testing.T, dom string, st site.Status) {
	t.Helper()
	if err := e.sites.SaveSite(ctx, site.Site{Domain: mustDomain(t, dom), Status: st, PHPVersion: "8.3"}); err != nil {
		t.Fatal(err)
	}
}

func pkgNames(ps []PendingPackage) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Name)
	}
	return out
}

// ---------------------------------------------------------------- Status

func TestUpdatesStatus(t *testing.T) {
	e := newUpdEnv(t)
	e.f.SetFile(RepoSourcesPath(RepoNginx), []byte("x"), 0o644, system.Root)
	e.f.SetFile(RepoSourcesPath(RepoSury), []byte("x"), 0o644, system.Root)
	e.f.SetFile(tpanOld, []byte("bản cũ"), 0o755, system.Root)

	st, err := e.u.Status(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	wantPkgs := []string{"libcurl4t64", "mariadb-server", "nginx", "nginx-common", "php8.3-cli", "php8.3-fpm", "php8.3-newext", "php8.4-cli"}
	if got := pkgNames(st.Packages); !slices.Equal(got, wantPkgs) {
		t.Errorf("gói của stack = %v, muốn %v", got, wantPkgs)
	}
	if !st.Packages[0].Security || st.Packages[1].Security {
		t.Errorf("cờ bảo mật sai: %+v", st.Packages[:2])
	}
	if st.Others != 2 || st.OthersSecurity != 1 {
		t.Errorf("gói khác = %d (bảo mật %d), muốn 2 (1)", st.Others, st.OthersSecurity)
	}
	if !slices.Equal(st.AutoOrigins, []string{OriginNginx, OriginSury}) {
		t.Errorf("AutoOrigins = %v", st.AutoOrigins)
	}
	if st.Auto.ID != "updates.auto" {
		t.Errorf("Auto = %+v", st.Auto)
	}
	m := st.MariaDB
	if m.Source != SourceUbuntu || m.Line != "10.11" || m.Version != "1:10.11.14-0ubuntu0.24.04.1" {
		t.Errorf("MariaDB = %+v", m)
	}
	if want := []MariaDBLine{{"11.8", "2030-02-13"}, {"12.3", "2029-06-12"}}; !reflect.DeepEqual(m.Upgrades, want) {
		t.Errorf("Upgrades = %+v, muốn %+v", m.Upgrades, want)
	}
	if st.Self != (SelfStatus{Version: "v1.0.0", URL: DefaultSelfURL, HasOld: true}) {
		t.Errorf("Self = %+v", st.Self)
	}
	if !st.ListsUpdated.IsZero() {
		t.Errorf("ListsUpdated = %v", st.ListsUpdated)
	}
	// Status chỉ đọc: không lấy khoá, không apt-get update.
	for _, c := range e.f.Ran() {
		if c.Name == "apt-get" && c.Args[0] == "update" {
			t.Errorf("refresh=false mà chạy %s", c.String())
		}
	}
}

func TestUpdatesStatusOfflineAndEmpty(t *testing.T) {
	e := newUpdEnv(t)
	e.f.Handle([]string{"curl"}, func(system.Cmd) system.Result {
		return system.Result{ExitCode: 6, Stderr: "curl: (6) Could not resolve host"}
	})
	e.f.Handle([]string{"curl", "-fsS", "--max-time", "5", mariadbReleasesURL}, func(system.Cmd) system.Result {
		return system.Result{ExitCode: 6}
	})
	e.f.Handle([]string{"apt-get", "-s", "-o", "Debug::NoLocking=1", "dist-upgrade"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "0 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.\n"}
	})
	st, err := e.u.Status(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.MariaDB.Upgrades) != 0 || st.MariaDB.Line != "10.11" {
		t.Errorf("không mạng: MariaDB = %+v", st.MariaDB)
	}
	// Mảng rỗng phải ra [] trong JSON (giao diện coi là mảng, không null).
	b, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{`"packages":[]`, `"auto_origins":[]`, `"upgrades":[]`} {
		if !strings.Contains(string(b), k) {
			t.Errorf("JSON thiếu %s: %s", k, b)
		}
	}
	// refresh=true: apt-get update có chờ khoá dpkg.
	var refreshed bool
	for _, c := range e.f.Ran() {
		refreshed = refreshed || c.String() == aptUpdateCmd.String()
	}
	if !refreshed {
		t.Error("refresh=true mà không chạy apt-get update")
	}
}

// ---------------------------------------------------------------- update.apply

func TestPrepareApply(t *testing.T) {
	e := newUpdEnv(t)
	e.addSite(t, "a.example.com", site.Active)
	e.addSite(t, "b.example.com", site.Active)
	e.addSite(t, "c.example.com", site.Trashed)
	p, err := e.u.PrepareApply(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"apt.update:lists",
		"apt.upgrade:libcurl4t64,mariadb-server,nginx,nginx-common,php8.3-cli,php8.3-fpm,php8.4-cli",
		"service.restart:nginx",
		"service.restart:php8.3-fpm",
		"service.restart:php8.4-fpm",
		"service.restart:mariadb",
	}
	if got := stepIDs(p.Plan); !slices.Equal(got, want) {
		t.Errorf("bước:\n got %v\nwant %v", got, want)
	}
	if p.Plan.Kind != "update.apply" {
		t.Errorf("Kind = %q", p.Plan.Kind)
	}
	// Dựng Plan không đổi máy (chỉ apt-get -s, dpkg-query, đọc file).
	for _, cmd := range e.f.Ran() {
		if cmd.Name != "apt-get" || cmd.Args[0] != "-s" {
			t.Errorf("PrepareApply chạy lệnh ngoài mô phỏng apt: %s", cmd.String())
		}
	}
	// Kiểm chứng: stack, php-fpm8.4 -t (bản ngoài mặc định), hai site active;
	// site trong thùng rác không có.
	ids := checkIDs(t, e.f, p.Plan.Verify)
	for _, id := range []string{"service.active:nginx", "config.phpfpm:8.4"} {
		if !slices.Contains(ids, id) {
			t.Errorf("kiểm chứng thiếu %s: %v", id, ids)
		}
	}
	var a, b, c bool
	for _, id := range ids {
		a = a || strings.HasSuffix(id, ":a.example.com")
		b = b || strings.HasSuffix(id, ":b.example.com")
		c = c || strings.Contains(id, "c.example.com")
	}
	if !a || !b || c {
		t.Errorf("phép kiểm site: a=%v b=%v c=%v (%v)", a, b, c, ids)
	}
}

// checkIDs chạy các phép kiểm trên máy giả để lấy ID (Lazy chỉ có ID khi chạy).
func checkIDs(t *testing.T, sys system.System, cs []checks.Check) []string {
	t.Helper()
	var ids []string
	for _, r := range checks.RunAll(ctx, sys, cs) {
		ids = append(ids, r.ID)
	}
	return ids
}

func TestPrepareApplyApacheOnlyPHP(t *testing.T) {
	e := newUpdEnv(t)
	e.set[SettingWeb] = "apache"
	e.f.Handle([]string{"apt-get", "-s", "-o", "Debug::NoLocking=1", "dist-upgrade"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "Inst apache2-bin [2.4.58-1ubuntu8.7] (2.4.58-1ubuntu8.8 Ubuntu:24.04/noble-security [arm64])\nInst certbot [2.9.0-1] (2.9.0-1ubuntu0.1 Ubuntu:24.04/noble-updates [all])\n"}
	})
	p, err := e.u.PrepareApply(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := stepIDs(p.Plan), []string{"apt.update:lists", "apt.upgrade:apache2-bin,certbot", "service.restart:apache2"}; !slices.Equal(got, want) {
		t.Errorf("bước = %v, muốn %v", got, want)
	}
	if cmds := p.Plan.Steps[2].Info().Commands; !slices.Contains(cmds, "apachectl configtest") {
		t.Errorf("restart apache2 không kiểm config trước: %v", cmds)
	}
}

func TestPrepareApplyNothing(t *testing.T) {
	e := newUpdEnv(t)
	// Chỉ có gói ngoài stack và một gói stack mới (chưa cài): không có gì để nâng.
	e.f.Handle([]string{"apt-get", "-s", "-o", "Debug::NoLocking=1", "dist-upgrade"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "Inst vim [1] (2 Ubuntu:24.04/noble-updates [arm64])\nInst php8.3-new (1 Ubuntu:24.04/noble-updates [arm64])\n"}
	})
	_, err := e.u.PrepareApply(ctx)
	if code := refusedCode(t, err); code != "nothing_to_update" {
		t.Errorf("mã = %q", code)
	}
}

// aptRefresh chạy mỗi lần (Check false tới khi Apply xong).
func TestAptRefresh(t *testing.T) {
	f := system.NewFake()
	s := &aptRefresh{}
	if done, _ := s.Check(ctx, f); done {
		t.Fatal("Check trước Apply phải false")
	}
	if err := s.Apply(ctx, f); err != nil {
		t.Fatal(err)
	}
	if done, _ := s.Check(ctx, f); !done {
		t.Fatal("Check sau Apply phải true")
	}
	if ran := f.Ran(); len(ran) != 1 || ran[0].String() != "apt-get update -o DPkg::Lock::Timeout=300" {
		t.Errorf("lệnh = %v", ran)
	}
	f.Handle([]string{"apt-get", "update"}, func(system.Cmd) system.Result { return system.Result{ExitCode: 100, Stderr: "E: lỗi mạng"} })
	if err := (&aptRefresh{}).Apply(ctx, f); err == nil || !strings.Contains(err.Error(), "làm mới danh sách gói") {
		t.Errorf("lỗi không có ngữ cảnh: %v", err)
	}
}

// ---------------------------------------------------------------- update.auto

func TestPrepareAuto(t *testing.T) {
	e := newUpdEnv(t)
	e.f.SetFile(RepoSourcesPath(RepoNginx), []byte("x"), 0o644, system.Root)
	e.f.SetFile(RepoSourcesPath(RepoMariaDB), []byte("x"), 0o644, system.Root)
	p, err := e.u.PrepareAuto(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := stepIDs(p.Plan), []string{"apt.install:unattended-upgrades", "file:" + unattendedConf}; !slices.Equal(got, want) {
		t.Fatalf("bước = %v, muốn %v", got, want)
	}
	files := p.Plan.Steps[1].Info().Files
	if len(files) != 1 {
		t.Fatalf("Files = %+v", files)
	}
	conf := string(files[0].New)
	for _, s := range []string{`APT::Periodic::Unattended-Upgrade "1";`, `"origin=nginx";`, `"origin=MariaDB";`} {
		if !strings.Contains(conf, s) {
			t.Errorf("file thiếu %s:\n%s", s, conf)
		}
	}
	if strings.Contains(conf, "sury") || strings.Contains(conf, "origin=origin") {
		t.Errorf("origin sai:\n%s", conf)
	}
	if len(p.Plan.Verify) != 1 || checkIDs(t, e.f, p.Plan.Verify)[0] != "updates.auto" {
		t.Error("kiểm chứng phải là updates.auto")
	}

	// Không kho ngoài nào: không có khối Origins-Pattern.
	p, err = newUpdEnv(t).u.PrepareAuto(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if conf := string(p.Plan.Steps[1].Info().Files[0].New); strings.Contains(conf, "Origins-Pattern") {
		t.Errorf("không kho ngoài mà có Origins-Pattern:\n%s", conf)
	}
}

// ---------------------------------------------------------------- mariadb.upgrade

func TestPrepareMariaDB(t *testing.T) {
	e := newUpdEnv(t)
	e.addSite(t, "a.example.com", site.Active)
	e.f.Handle([]string{"dpkg-query", "-W", "-f", "${Package}\\t${db:Status-Status}\\n"}, func(system.Cmd) system.Result {
		// Mã 1: mẫu galera-* không khớp gói nào, nhưng vẫn in các gói mariadb-*.
		return system.Result{ExitCode: 1, Stdout: "mariadb-server\tinstalled\nmariadb-client\tinstalled\nmariadb-test\tnot-installed\nmariadb-common\tinstalled\n",
			Stderr: "dpkg-query: no packages found matching galera-*\n"}
	})
	before := time.Now().Format("20060102-")
	p, err := e.u.PrepareMariaDB(ctx, "11.8")
	if err != nil {
		t.Fatal(err)
	}
	ids := stepIDs(p.Plan)
	wantPrefix := []string{"mariadb.dump:", "service.stop:mariadb", "apt.repo:mariadb", "apt.upgrade:mariadb-client,mariadb-common,mariadb-server", "settings:stack.db_source=mariadb.org:stack.db_line=11.8", "service:mariadb", "mariadb.upgrade:localhost"}
	if len(ids) != len(wantPrefix) {
		t.Fatalf("bước = %v", ids)
	}
	for i, w := range wantPrefix {
		if !strings.HasPrefix(ids[i], w) {
			t.Errorf("bước %d = %q, muốn bắt đầu bằng %q (%v)", i, ids[i], w, ids)
		}
	}
	if dump := strings.TrimPrefix(ids[0], "mariadb.dump:"); !strings.HasPrefix(dump, "/var/lib/tpan/backups/mariadb-upgrade-"+before) || !strings.HasSuffix(dump, ".sql") {
		t.Errorf("file xuất = %q", dump)
	}
	if title := p.Plan.Title.Vi; !strings.Contains(title, "Một chiều") || !strings.Contains(title, "đường lùi") || !strings.Contains(title, "10.11") {
		t.Errorf("Title không nói rõ một chiều và đường lùi: %q", title)
	}
	if why := p.Plan.Steps[0].Info().Why.Vi; !strings.Contains(why, "đường lùi") {
		t.Errorf("Why của bước đầu không nói đường lùi: %q", why)
	}
	if len(p.Plan.Verify) == 0 {
		t.Error("thiếu kiểm chứng")
	}
	// Bước ghi settings nằm ngay sau nâng gói và ghi đúng nguồn, dòng mới; kho
	// không hoàn tác.
	if err := p.Plan.Steps[4].Apply(ctx, e.u.Sys); err != nil {
		t.Fatal(err)
	}
	if e.set[SettingDBSource] != "mariadb.org" || e.set[SettingDBLine] != "11.8" {
		t.Errorf("settings sau nâng = %v", e.set)
	}
	if p.Plan.Steps[2].Info().NoUndo.Vi == "" {
		t.Error("bước thêm kho phải nói vì sao không hoàn tác")
	}
}

func TestPrepareMariaDBRefusals(t *testing.T) {
	for _, tc := range []struct{ line, code string }{
		{"a.com; rm -rf /", "mariadb_line_invalid"},
		{"$(id)", "mariadb_line_invalid"},
		{"11", "mariadb_line_invalid"},
		{"11.8\n", "mariadb_line_invalid"},
		{"10.11", "mariadb_not_newer"},
		{"10.6", "mariadb_not_newer"},
	} {
		e := newUpdEnv(t)
		_, err := e.u.PrepareMariaDB(ctx, tc.line)
		if code := refusedCode(t, err); code != tc.code {
			t.Errorf("%q: mã %q, muốn %q", tc.line, code, tc.code)
		}
		if tc.code == "mariadb_line_invalid" && len(e.f.Ran()) != 0 {
			t.Errorf("%q: dòng sai mà vẫn chạy lệnh %v", tc.line, e.f.Ran())
		}
	}
	// Chưa cài MariaDB.
	e := newUpdEnv(t)
	e.f.Handle([]string{"dpkg-query", "-W", "-f", `${db:Status-Status}\t${Version}`, "mariadb-server"}, func(system.Cmd) system.Result {
		return system.Result{ExitCode: 1}
	})
	_, err := e.u.PrepareMariaDB(ctx, "11.8")
	if code := refusedCode(t, err); code != "stack_not_installed" {
		t.Errorf("chưa cài: mã %q", code)
	}
}
