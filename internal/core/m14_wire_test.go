package core

// Test của phần ghép M14 (mục 6.13 của docs/KE-HOACH.md): phép kiểm mới có nằm
// đúng bộ không (site thường, site tạm khoá, stack), tên cách sửa tự động của
// từng ID, và Plan sửa mà Fixes dựng cho từng tên. Bản thân phép kiểm và Step
// đã có test ở gói checks và steps; ở đây chỉ thử chỗ nối.

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/htaccess"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
)

func TestAutoFixNameM14(t *testing.T) {
	for _, tc := range []struct {
		id     string
		status checks.Status
		want   string
	}{
		{"php.opcache:8.3", checks.Fail, FixPHPHarden},
		{"php.opcache:8.3", checks.Warn, ""},
		{"php.opcache:8.3", checks.Unknown, ""}, // không chạy được php-fpm: chưa biết sửa gì
		{"php.opcache:8.3", checks.Pass, ""},
		// Ngoại lệ của luật "cảnh báo không có sửa tự động" (mục 6.13).
		{"php.protectproc:8.3", checks.Warn, FixPHPHarden},
		{"php.protectproc:8.3", checks.Pass, ""},
		{"php.protectproc:8.3", checks.Unknown, ""},
		{"log.perm:shop.example.com", checks.Fail, FixLogPerms},
		{"log.perm:shop.example.com", checks.Warn, ""},
		{"log.perm:shop.example.com", checks.Pass, ""},
		{"web.hardened:shop.example.com", checks.Fail, FixWebRewrite},
		{"web.hardened:shop.example.com", checks.Unknown, ""},
		{"web.hardened:shop.example.com", checks.Pass, ""},
		// Ba phép còn lại của M14 chỉ có sửa tay: .htaccess là file của site,
		// vhost lạ là file tpan không ghi.
		{"web.htaccess:shop.example.com", checks.Fail, ""},
		{"web.htaccess:shop.example.com", checks.Warn, ""},
		{"web.server_name.unique", checks.Fail, ""},
		{"web.default.rejects", checks.Fail, ""},
	} {
		if got := AutoFixName(checks.Result{ID: tc.id, Status: tc.status}); got != tc.want {
			t.Errorf("AutoFixName(%q, %s) = %q, muốn %q", tc.id, tc.status, got, tc.want)
		}
	}
}

// hardenEnv là một máy giả vừa cài stack nginx bằng Plan thật (nên đã có file
// ini và drop-in), kèm Fixes trên cùng máy và store.
func hardenEnv(t *testing.T) (*Stack, *sim, *Fixes) {
	t.Helper()
	s, m, st := newTestStack(t)
	in, err := s.PrepareInstall(ctx, Nginx)
	if err != nil {
		t.Fatal(err)
	}
	if rep, err := in.Execute(ctx, nil); err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("cài stack: %v %s %s", err, rep.Job.Status, rep.Job.Error)
	}
	return s, m, &Fixes{Sys: m, Settings: st, Store: st, Journal: st}
}

// stackResult chạy bộ phép kiểm của stack và trả kết quả có ID id.
func stackResult(t *testing.T, s *Stack, id string) checks.Result {
	t.Helper()
	cs, _, err := s.Checks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range checks.RunAll(ctx, s.Sys, cs) {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("bộ phép kiểm của stack không có %s", id)
	return checks.Result{}
}

func TestFixPHPHardenM14(t *testing.T) {
	s, m, f := hardenEnv(t)
	const opcache, proc = "php.opcache:8.3", "php.protectproc:8.3"
	for _, id := range []string{opcache, proc} {
		if r := stackResult(t, s, id); r.Status != checks.Pass {
			t.Fatalf("máy vừa cài: %s %s %s", id, r.Status, r.Summary.Vi)
		}
	}

	run := func(r checks.Result) {
		t.Helper()
		x, err := f.Prepare(ctx, r)
		if err != nil {
			t.Fatalf("Prepare %s: %v", r.ID, err)
		}
		if x.Plan.Kind != "fix."+FixPHPHarden || x.Name != FixPHPHarden || x.Plan.Target != "8.3" {
			t.Errorf("Fix = %s/%s, Target %q", x.Plan.Kind, x.Name, x.Plan.Target)
		}
		// Đúng hai bước của steps.PHPHardening: ini trước, drop-in sau.
		if got := stepIDsOf(x); !slices.Equal(got, []string{"php.harden.opcache:8.3", "php.harden.proc:8.3"}) {
			t.Errorf("bước %v", got)
		}
		// Verify là cả hai phép kiểm của bản đó, dù kết quả đưa vào là phép nào.
		if got := checkIDs(t, m, x.Plan.Verify); !slices.Equal(got, []string{opcache, proc}) {
			t.Errorf("Verify %v", got)
		}
		if _, err := x.Preview(ctx); err != nil {
			t.Fatalf("Preview: %v", err)
		}
		rep, err := x.Execute(ctx, nil)
		if err != nil || rep.Job.Status != engine.JobSucceeded {
			t.Fatalf("Execute: %v %s (%s); kiểm %s", err, rep.Job.Status, rep.Job.Error, failedChecks(rep.Checks))
		}
	}

	// Ai đó xoá file ini: php.opcache lỗi, sửa tự động ghi lại.
	ini := templates.PHPHardeningINIPath("8.3")
	if err := m.Remove(ini); err != nil {
		t.Fatal(err)
	}
	r := stackResult(t, s, opcache)
	if r.Status != checks.Fail || AutoFixName(r) != FixPHPHarden {
		t.Fatalf("mất file ini: %s %q", r.Status, AutoFixName(r))
	}
	run(r)
	if got, _ := m.ReadFile(ini); string(got) != string(templates.PHPHardeningINI()) {
		t.Errorf("file ini sau khi sửa:\n%s", got)
	}
	if r := stackResult(t, s, opcache); r.Status != checks.Pass {
		t.Errorf("sau khi sửa: %s %s", r.Status, r.Summary.Vi)
	}

	// Drop-in bị gỡ: chỉ là cảnh báo nhưng vẫn sửa được, và Plan phải restart
	// dịch vụ (máy giả chỉ trả "invisible" sau một lần restart có drop-in).
	if err := m.Remove(templates.PHPDropInPath("8.3")); err != nil {
		t.Fatal(err)
	}
	r = stackResult(t, s, proc)
	if r.Status != checks.Warn || AutoFixName(r) != FixPHPHarden {
		t.Fatalf("mất drop-in: %s %q", r.Status, AutoFixName(r))
	}
	m.ResetRan()
	run(r)
	var ran []string
	for _, c := range m.Ran() {
		ran = append(ran, c.String())
	}
	reload, restart := slices.Index(ran, "systemctl daemon-reload"), slices.Index(ran, "systemctl restart php8.3-fpm")
	if reload < 0 || restart < reload {
		t.Errorf("muốn daemon-reload rồi restart php8.3-fpm, có: %v", ran)
	}
	if r := stackResult(t, s, proc); r.Status != checks.Pass {
		t.Errorf("sau khi sửa: %s %s", r.Status, r.Summary.Vi)
	}
}

// Bản PHP trong ID đến từ bên ngoài (API, CLI). Nó đi vào đường dẫn file, tên
// chương trình (php-fpm<bản>) và tên dịch vụ, nên sai dạng thì bị từ chối trước
// khi có bước nào hay lệnh nào.
func TestFixPHPHardenRefusalsM14(t *testing.T) {
	_, m, f := hardenEnv(t)
	before := m.Snapshot()
	for _, r := range []checks.Result{
		{ID: "php.opcache:../x", Status: checks.Fail},
		{ID: "php.opcache:../../etc/passwd", Status: checks.Fail},
		{ID: "php.opcache:8.3; rm -rf /", Status: checks.Fail},
		{ID: "php.opcache:$(id)", Status: checks.Fail},
		{ID: "php.opcache:8.3\n8.4", Status: checks.Fail},
		{ID: "php.opcache:8", Status: checks.Fail},
		{ID: "php.opcache:", Status: checks.Fail},
		{ID: "php.opcache", Status: checks.Fail},
		{ID: "php.protectproc:../x", Status: checks.Warn},
		{ID: "php.protectproc:8.3/../8.4", Status: checks.Warn},
		// Tên sửa không khớp phép kiểm.
		{ID: "php.opcache:8.3", Status: checks.Fail, AutoFix: FixLogPerms},
	} {
		m.ResetRan()
		x, err := f.Prepare(ctx, r)
		if x != nil {
			t.Errorf("%q: Prepare trả Plan có %d bước", r.ID, len(x.Plan.Steps))
		}
		if got := refusedCode(t, err); got != "no_auto_fix" {
			t.Errorf("%q: code %s, muốn no_auto_fix", r.ID, got)
		}
		for _, c := range m.Ran() {
			t.Errorf("%q: Prepare chạy lệnh %s", r.ID, c.String())
		}
	}
	if len(m.Snapshot()) != len(before) {
		t.Error("Prepare bị từ chối mà hệ thống file đổi")
	}
}

func TestFixLogPermsM14(t *testing.T) {
	e := newFixEnv(t, Nginx, fixDom)
	l := site.LayoutOf(mustDomain(t, fixDom))
	const id = "log.perm:" + fixDom
	if r := e.recheck(t, fixDom, id); r.Status != checks.Pass {
		t.Fatalf("site vừa tạo: %s %s", r.Status, r.Summary.Vi)
	}

	// Như site do bản trước M14 tạo: thư mục 0755, access.log của root:root 0644.
	if err := e.m.Chmod(l.LogDir, 0o755); err != nil {
		t.Fatal(err)
	}
	e.m.SetFile(l.AccessLog, []byte("dòng log cũ\n"), 0o644, system.Root)
	// File logrotate của bản trước M14 (postrotate gửi USR1): Plan sửa quyền
	// log phải ghi lại nó, không thì sau lần xoay vòng đầu nginx không mở lại
	// được log trong thư mục 0750.
	e.m.SetDir("/etc/logrotate.d", 0o755, system.Root)
	e.m.SetFile(logrotateCnf, []byte("# bản cũ\nkill -USR1\n"), 0o644, system.Root)
	r := e.recheck(t, fixDom, id)
	if r.Status != checks.Fail || AutoFixName(r) != FixLogPerms {
		t.Fatalf("quyền log sai: %s %q", r.Status, AutoFixName(r))
	}
	x := e.fix(t, r)
	want := append([]string{"file:" + logrotateCnf}, stepIDs(engine.Plan{Steps: steps.SiteLogs(l)})...)
	if !slices.Equal(stepIDsOf(x), want) {
		t.Errorf("bước %v, muốn %v (file logrotate rồi steps.SiteLogs)", stepIDsOf(x), want)
	}
	if got := checkIDs(t, e.m, x.Plan.Verify); !slices.Equal(got, []string{id}) {
		t.Errorf("Verify %v", got)
	}
	if r := e.recheck(t, fixDom, id); r.Status != checks.Pass {
		t.Errorf("sau khi sửa: %s %s", r.Status, r.Summary.Vi)
	}
	// Sửa quyền không được xoá log đang có.
	if got, _ := e.m.ReadFile(l.AccessLog); string(got) != "dòng log cũ\n" {
		t.Errorf("access.log sau khi sửa: %q", got)
	}

	// Domain trong ID đến từ bên ngoài: phải là một site active trong Store.
	if _, err := e.f.Prepare(ctx, checks.Result{ID: "log.perm:none.example.com", Status: checks.Fail}); refusedCode(t, err) != "site_not_found" {
		t.Error("site không có: muốn site_not_found")
	}
	e.m.ResetRan()
	for _, bad := range []string{"../../etc", "a.com; rm -rf /", "$(id)", ""} {
		for _, kind := range []string{"log.perm:", "web.hardened:"} {
			if x, err := e.f.Prepare(ctx, checks.Result{ID: kind + bad, Status: checks.Fail}); err == nil {
				t.Errorf("%q: muốn lỗi, có Plan %d bước", kind+bad, len(x.Plan.Steps))
			}
		}
	}
	for _, c := range e.m.Ran() {
		t.Errorf("Prepare với domain sai chạy lệnh %s", c.String())
	}
}

// web.hardened: vhost do bản tpan trước M14 ghi (thiếu dòng chặn) được ghi lại
// từ template hiện tại bằng Plan của web.vhost, và Verify có thêm web.hardened.
func TestFixVhostHardenedM14(t *testing.T) {
	for _, tc := range []struct {
		web WebServer
		// old đổi vhost hiện tại thành dạng bản cũ ghi.
		old func(conf string) string
	}{
		{Nginx, func(conf string) string { return strings.ReplaceAll(conf, templates.NginxSymlinkGuard, "") }},
		{Apache, func(conf string) string {
			return strings.ReplaceAll(conf, "AllowOverride "+htaccess.OverrideOptions(), "AllowOverride All")
		}},
	} {
		t.Run(string(tc.web), func(t *testing.T) {
			e := newFixEnv(t, tc.web, fixDom)
			l := site.LayoutOf(mustDomain(t, fixDom))
			conf := l.NginxConf
			if tc.web == Apache {
				conf = l.ApacheConf
			}
			const id = "web.hardened:" + fixDom
			if r := e.recheck(t, fixDom, id); r.Status != checks.Pass {
				t.Fatalf("site vừa tạo: %s %s", r.Status, r.Summary.Vi)
			}
			good, err := e.m.ReadFile(conf)
			if err != nil {
				t.Fatal(err)
			}
			old := tc.old(string(good))
			if old == string(good) {
				t.Fatal("vhost mới không có dòng chặn để gỡ: template đã đổi?")
			}
			e.m.SetFile(conf, []byte(old), 0o644, system.Root)
			// Như trên máy nâng từ bản trước M14: sổ managed_files giữ đúng
			// sha256 của vhost cũ. "Khớp sổ nhưng khác template" cũng là hình
			// dạng của bản sửa tay đã nhận; với web.hardened vẫn phải ghi lại
			// (trên máy ảo, bản đầu bỏ bước ghi và job hỏng ở kiểm chứng).
			sum := sha256.Sum256([]byte(old))
			e.f.Files = fileRecords{conf: hex.EncodeToString(sum[:])}

			r := e.recheck(t, fixDom, id)
			if r.Status != checks.Fail || AutoFixName(r) != FixWebRewrite {
				t.Fatalf("vhost cũ: %s %q", r.Status, AutoFixName(r))
			}
			x := e.fix(t, r)
			if got := checkIDs(t, e.m, x.Plan.Verify); !slices.Contains(got, id) || !slices.Contains(got, "web.vhost:"+fixDom) {
				t.Errorf("Verify %v; muốn có %s và web.vhost", got, id)
			}
			if got, _ := e.m.ReadFile(conf); string(got) != string(good) {
				t.Errorf("vhost sau khi sửa khác bản của template:\n%s", got)
			}
			if r := e.recheck(t, fixDom, id); r.Status != checks.Pass {
				t.Errorf("sau khi sửa: %s %s", r.Status, r.Summary.Vi)
			}
		})
	}
}

// Bộ phép kiểm của site theo trạng thái: site tạm khoá có log.perm (log vẫn
// nằm trên đĩa) nhưng không có web.hardened hay web.htaccess (vhost tạm khoá
// không trỏ vào public nên không có gì để chặn).
func TestSiteStateChecksM14(t *testing.T) {
	d := mustDomain(t, fixDom)
	const logPerm, hardened, ht = "log.perm:" + fixDom, "web.hardened:" + fixDom, "web.htaccess:" + fixDom
	for _, tc := range []struct {
		name string
		web  WebServer
		st   site.Site
		want []string
		not  []string
	}{
		{"tạm khoá, nginx", Nginx, site.Site{Suspended: account.SuspendSelf}, []string{logPerm}, []string{hardened, ht}},
		{"tạm khoá, apache", Apache, site.Site{Suspended: account.SuspendSelf}, []string{logPerm}, []string{hardened, ht}},
		{"DB khoá, nginx", Nginx, site.Site{DBSuspended: account.SuspendSelf}, []string{logPerm, hardened}, []string{ht}},
		{"DB khoá, apache", Apache, site.Site{DBSuspended: account.SuspendSelf}, []string{logPerm, hardened, ht}, nil},
		{"thường, nginx", Nginx, site.Site{}, []string{logPerm, hardened}, []string{ht}},
		{"thường, apache", Apache, site.Site{}, []string{logPerm, hardened, ht}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := tc.st
			st.Domain, st.Status, st.PHPVersion = d, site.Active, "8.3"
			cfg := StackConfig{Web: tc.web, PHPVersion: "8.3"}
			// verify=true (kiểm chứng sau thao tác) phải có cùng các phép này.
			for _, verify := range []bool{false, true} {
				ids := checkIDs(t, system.NewFake(), siteStateChecks(st, cfg, nil, verify))
				for _, id := range tc.want {
					if n := countOf(ids, id); n != 1 {
						t.Errorf("verify=%v: %s xuất hiện %d lần, muốn 1: %v", verify, id, n, ids)
					}
				}
				for _, id := range tc.not {
					if slices.Contains(ids, id) {
						t.Errorf("verify=%v: không được có %s: %v", verify, id, ids)
					}
				}
			}
		})
	}
}

func countOf(ids []string, id string) int {
	n := 0
	for _, x := range ids {
		if x == id {
			n++
		}
	}
	return n
}

// Stack.Checks: mỗi bản PHP đang cài có đúng một cặp php.opcache và
// php.protectproc (bản mặc định từ stackChecks, bản khác từ isolationChecks);
// hai phép định tuyến chỉ có khi Stack biết danh sách site.
func TestStackChecksM14(t *testing.T) {
	se := newSiteEnv(t, Nginx)
	se.add(t, fixDom)
	// Hai bản PHP đang cài (core.phpIsInstalled: có /etc/php/<bản>/fpm và file chạy).
	for _, v := range []string{"8.3", "8.4"} {
		se.m.SetDir("/etc/php/"+v+"/fpm", 0o755, system.Root)
		se.m.SetFile("/usr/sbin/php-fpm"+v, []byte("ELF giả\n"), 0o755, system.Root)
	}
	// Thư mục cấu hình còn lại của một bản đã gỡ: không được tính.
	se.m.SetDir("/etc/php/8.1/fpm", 0o755, system.Root)

	routing := []string{"web.server_name.unique", "web.default.rejects"}
	ids := func(s *Stack) []string {
		t.Helper()
		cs, installed, err := s.Checks(ctx)
		if err != nil || !installed {
			t.Fatalf("Checks: installed=%v, %v", installed, err)
		}
		return checkIDs(t, system.NewFake(), cs)
	}

	got := ids(&Stack{Sys: se.m, Settings: se.st})
	for _, id := range []string{"php.opcache:8.3", "php.protectproc:8.3", "php.opcache:8.4", "php.protectproc:8.4"} {
		if n := countOf(got, id); n != 1 {
			t.Errorf("%s xuất hiện %d lần, muốn 1: %v", id, n, got)
		}
	}
	for _, id := range append([]string{"php.opcache:8.1", "php.protectproc:8.1"}, routing...) {
		if slices.Contains(got, id) {
			t.Errorf("không được có %s (Sites nil, 8.1 đã gỡ): %v", id, got)
		}
	}

	// Sites khác nil (health): thêm hai phép định tuyến, mỗi phép một lần.
	s := &Stack{Sys: se.m, Settings: se.st, Sites: se.sites}
	got = ids(s)
	for _, id := range routing {
		if n := countOf(got, id); n != 1 {
			t.Errorf("%s xuất hiện %d lần, muốn 1: %v", id, n, got)
		}
	}

	// web.default.rejects thử thêm cổng 443 khi có site active bật SSL, và chỉ
	// khi đó: mỗi cổng một lệnh curl, tức một dòng bằng chứng.
	probes := func() int {
		t.Helper()
		cs, _, err := s.Checks(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range checks.RunAll(ctx, system.NewFake(), cs) {
			if r.ID == "web.default.rejects" {
				return len(r.Evidence)
			}
		}
		t.Fatal("không có web.default.rejects")
		return 0
	}
	if n := probes(); n != 1 {
		t.Errorf("chưa site nào có SSL: %d lệnh curl, muốn 1", n)
	}
	st := se.sites.sites[fixDom]
	st.SSL = true
	se.sites.sites[fixDom] = st
	if n := probes(); n != 2 {
		t.Errorf("có site SSL: %d lệnh curl, muốn 2", n)
	}
	// Site SSL nằm trong thùng rác thì không tính.
	st.Status = site.Trashed
	se.sites.sites[fixDom] = st
	if n := probes(); n != 1 {
		t.Errorf("site SSL đã vào thùng rác: %d lệnh curl, muốn 1", n)
	}
}
