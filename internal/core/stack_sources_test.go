package core

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
)

const osReleaseNoble = "NAME=\"Ubuntu\"\nID=ubuntu\nVERSION_ID=\"24.04\"\nVERSION_CODENAME=noble\n"

func repoOf(t *testing.T, p engine.Plan, name string) *steps.AptRepo {
	t.Helper()
	for _, s := range p.Steps {
		if r, ok := s.(*steps.AptRepo); ok && r.Name == name {
			return r
		}
	}
	t.Fatalf("Plan không có bước thêm kho %s: %q", name, stepIDs(p))
	return nil
}

func TestStackSourcesPlan(t *testing.T) {
	adapt := []string{
		"service:nginx",
		"dir:/etc/nginx/sites-available",
		"dir:/etc/nginx/sites-enabled",
		"file:/etc/nginx/conf.d/tpan.conf",
		"absent:/etc/nginx/conf.d/default.conf",
		"user.group:nginx:www-data",
		"absent:/etc/nginx/sites-enabled/default",
		"file:/etc/nginx/sites-available/tpan-default.conf",
		"symlink:/etc/nginx/sites-enabled/tpan-default.conf",
	}
	for _, tc := range []struct {
		name  string
		opts  InstallOptions
		repos []string
		mid   []string
	}{
		{"nginx.org", InstallOptions{WebSource: SourceNginxStable}, []string{"apt.repo:nginx"}, adapt},
		{"mariadb.org", InstallOptions{DBSource: SourceMariaDB, DBLine: "11.8"}, []string{"apt.repo:mariadb"}, []string{
			"absent:/etc/nginx/sites-enabled/default",
			"file:/etc/nginx/sites-available/tpan-default.conf",
			"symlink:/etc/nginx/sites-enabled/tpan-default.conf",
			"service:nginx",
		}},
		{"cả hai", InstallOptions{WebSource: SourceNginxMainline, DBSource: SourceMariaDB, DBLine: "11.4"}, []string{"apt.repo:nginx", "apt.repo:mariadb"}, adapt},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, m, _ := newTestStack(t)
			m.SetFile("/etc/os-release", []byte(osReleaseNoble), 0o644, system.Root)
			in, err := s.PrepareInstallWith(ctx, Nginx, tc.opts)
			if err != nil {
				t.Fatalf("PrepareInstallWith: %v", err)
			}
			want := slices.Concat(tc.repos, wantCommonHead, tc.mid, wantCommonTail)
			pkgs, _ := Packages(in.Config)
			want[len(tc.repos)+1] = "apt.install:" + strings.Join(pkgs, ",")
			if got := stepIDs(in.Plan); !reflect.DeepEqual(got, want) {
				t.Errorf("thứ tự bước:\ncó    %q\nmuốn  %q", got, want)
			}
			for _, s := range in.Plan.Steps[:len(tc.repos)] {
				if !s.Info().Danger {
					t.Errorf("bước %s thêm kho ngoài mà không Danger", s.Info().ID)
				}
			}
			if in.Config.WebSource == SourceNginxMainline {
				if r := repoOf(t, in.Plan, RepoNginx); !strings.Contains(string(r.Sources), "/packages/mainline/ubuntu") || !strings.Contains(string(r.Sources), "Suites: noble") {
					t.Errorf("kho mainline: %s", r.Sources)
				}
			}
			if in.Config.DBSource == SourceMariaDB {
				if r := repoOf(t, in.Plan, RepoMariaDB); !strings.Contains(string(r.Sources), "/"+tc.opts.DBLine+"/repo/ubuntu") {
					t.Errorf("kho MariaDB không đúng dòng %s: %s", tc.opts.DBLine, r.Sources)
				}
			}
		})
	}
}

func TestStackSourcesRefusals(t *testing.T) {
	t.Run("không đọc được tên mã Ubuntu", func(t *testing.T) {
		s, _, _ := newTestStack(t) // os-release của sim không có VERSION_CODENAME
		_, err := s.PrepareInstallWith(ctx, Nginx, InstallOptions{WebSource: SourceNginxStable})
		if refusedCode(t, err) != "precondition_failed" {
			t.Fatal("muốn precondition_failed")
		}
		var re *RefusedError
		errors.As(err, &re)
		if !strings.Contains(re.Message.Vi, "VERSION_CODENAME") {
			t.Errorf("lời từ chối không nói thiếu gì: %q", re.Message.Vi)
		}
	})

	t.Run("lần đầu, lựa chọn sai", func(t *testing.T) {
		for _, tc := range []struct {
			web  WebServer
			opts InstallOptions
			code string
		}{
			{Apache, InstallOptions{WebSource: SourceNginxStable}, "source_unsupported"},
			{Nginx, InstallOptions{WebSource: "nginx.org; id"}, "source_unsupported"},
			{Nginx, InstallOptions{DBSource: SourceMariaDB}, "mariadb_line_invalid"},
			{Nginx, InstallOptions{DBSource: SourceMariaDB, DBLine: "../11.8"}, "mariadb_line_invalid"},
			{Nginx, InstallOptions{DBLine: "11.8"}, "mariadb_line_invalid"},
		} {
			s, m, _ := newTestStack(t)
			if _, err := s.PrepareInstallWith(ctx, tc.web, tc.opts); refusedCode(t, err) != tc.code {
				t.Errorf("%s %+v: muốn %s", tc.web, tc.opts, tc.code)
			}
			if len(m.Ran()) != 0 {
				t.Errorf("%+v: từ chối mà vẫn chạy lệnh %v", tc.opts, m.Ran())
			}
		}
	})

	t.Run("chạy lại với nguồn khác đã ghi", func(t *testing.T) {
		s, m, st := newTestStack(t)
		m.SetFile("/etc/os-release", []byte(osReleaseNoble), 0o644, system.Root)
		mustSet(t, st, SettingWeb, "nginx")
		mustSet(t, st, SettingWebSource, "nginx.org")
		mustSet(t, st, SettingDBSource, "mariadb.org")
		mustSet(t, st, SettingDBLine, "11.4")
		for _, opts := range []InstallOptions{
			{WebSource: SourceUbuntu},
			{WebSource: SourceNginxMainline},
			{DBSource: SourceUbuntu},
			{DBLine: "11.8"},
		} {
			if _, err := s.PrepareInstallWith(ctx, Nginx, opts); refusedCode(t, err) != "source_change_unsupported" {
				t.Errorf("%+v: muốn source_change_unsupported", opts)
			}
		}
		// Bỏ trống hoặc trùng thì giữ như đã ghi, kể cả qua PrepareInstall (stack repair).
		for _, opts := range []InstallOptions{{}, {WebSource: SourceNginxStable, DBSource: SourceMariaDB, DBLine: "11.4"}} {
			in, err := s.PrepareInstallWith(ctx, Nginx, opts)
			if err != nil {
				t.Fatalf("%+v: %v", opts, err)
			}
			if in.Config.WebSource != SourceNginxStable || in.Config.DBSource != SourceMariaDB || in.Config.DBLine != "11.4" {
				t.Errorf("%+v: Config = %+v", opts, in.Config)
			}
			ids := stepIDs(in.Plan)
			if ids[0] != "apt.repo:nginx" || ids[1] != "apt.repo:mariadb" {
				t.Errorf("chạy lại mất bước kho: %q", ids[:3])
			}
		}
		in, err := s.PrepareInstall(ctx, Nginx)
		if err != nil || in.Config.WebSource != SourceNginxStable {
			t.Errorf("PrepareInstall không giữ nguồn đã ghi: %+v %v", in, err)
		}
	})
}

// Nguồn và dòng MariaDB được ghi trước khi chạy, như stack.web (mục 6.1): job
// hỏng giữa chừng thì lần chạy lại vẫn biết máy đang dùng kho nào.
func TestStackSourcesWrittenBeforeExecute(t *testing.T) {
	s, m, st := newTestStack(t)
	m.SetFile("/etc/os-release", []byte(osReleaseNoble), 0o644, system.Root)
	in, err := s.PrepareInstallWith(ctx, Nginx, InstallOptions{WebSource: SourceNginxStable, DBSource: SourceMariaDB, DBLine: "11.8"})
	if err != nil {
		t.Fatal(err)
	}
	// Sim không có mktemp, gpg: bước thêm kho đầu tiên hỏng, job dừng ngay.
	rep, err := in.Execute(ctx, nil)
	if err != nil || rep.Job.Status == engine.JobSucceeded {
		t.Fatalf("muốn job hỏng ở bước kho: %v %s", err, rep.Job.Status)
	}
	for k, want := range map[string]string{
		SettingWeb: "nginx", SettingWebSource: "nginx.org", SettingDBSource: "mariadb.org",
		SettingDBLine: "11.8", SettingPHPSource: "ubuntu",
	} {
		if v, _, _ := st.GetSetting(ctx, k); v != want {
			t.Errorf("%s = %q, muốn %q", k, v, want)
		}
	}
	if _, ok, _ := st.GetSetting(ctx, SettingPHPVersion); ok {
		t.Error("php_version được ghi dù job hỏng")
	}
	// Chạy lại không cờ: giữ nguồn đã ghi, bỏ kiểm máy sạch.
	in2, err := s.PrepareInstall(ctx, Nginx)
	if err != nil || in2.Config.DBLine != "11.8" {
		t.Errorf("chạy lại: %v %+v", err, in2)
	}
}

// Pool www của gói bị gỡ, tpan-idle thay chỗ, và kiểm chứng có php.www_pool.
func TestStackInstallIdlePool(t *testing.T) {
	s, m, _ := newTestStack(t)
	www := "/etc/php/8.3/fpm/pool.d/www.conf"
	m.SetFile(www, []byte("[www]\n"), 0o644, system.Root)
	in, err := s.PrepareInstall(ctx, Nginx)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := in.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("Execute: %v %s %s", err, rep.Job.Status, rep.Job.Error)
	}
	if _, err := m.Stat(www); err == nil {
		t.Error("www.conf còn sau khi cài stack")
	}
	if _, err := m.Stat("/etc/php/8.3/fpm/pool.d/tpan-idle.conf"); err != nil {
		t.Errorf("thiếu tpan-idle.conf: %v", err)
	}
	if r := checkByID(rep.Checks, "php.www_pool:8.3"); r == nil || r.Status != checks.Pass {
		t.Errorf("kiểm chứng php.www_pool:8.3 = %+v", r)
	}
}

// M14 (mục 6.13): cài stack ghi file ini OPcache và drop-in ProtectProc của
// bản PHP vừa dò được, rồi restart PHP-FPM sau daemon-reload (drop-in chỉ có
// hiệu lực với dịch vụ khởi động sau khi systemd đọc nó).
func TestStackInstallHardening(t *testing.T) {
	s, m, _ := newTestStack(t)
	in, err := s.PrepareInstall(ctx, Nginx)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := in.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("Execute: %v %s %s", err, rep.Job.Status, rep.Job.Error)
	}
	for p, want := range map[string][]byte{
		templates.PHPHardeningINIPath("8.3"): templates.PHPHardeningINI(),
		templates.PHPDropInPath("8.3"):       templates.PHPDropIn(),
	} {
		if got, err := m.ReadFile(p); err != nil || string(got) != string(want) {
			t.Errorf("%s: %v\n%s", p, err, got)
		}
	}
	var ran []string
	for _, c := range m.Ran() {
		ran = append(ran, c.String())
	}
	reload, restart := slices.Index(ran, "systemctl daemon-reload"), slices.Index(ran, "systemctl restart php8.3-fpm")
	if reload < 0 || restart < reload {
		t.Errorf("daemon-reload ở %d, restart php8.3-fpm ở %d; muốn restart sau daemon-reload", reload, restart)
	}
	// Hai bước mới đứng ngay sau pool tpan-idle và dùng đúng bản đã dò.
	ids := stepIDs(in.Plan)
	if i := slices.Index(ids, "php.idle:8.3"); i < 0 || !slices.Equal(ids[i+1:i+3], []string{"php.harden.opcache:8.3", "php.harden.proc:8.3"}) {
		t.Errorf("thứ tự bước sau khi dò bản PHP: %q", ids)
	}
}

func checkByID(rs []checks.Result, id string) *checks.Result {
	for i := range rs {
		if rs[i].ID == id {
			return &rs[i]
		}
	}
	return nil
}

// Máy đã cài thêm 8.4 (tpan php install): chạy lại stack install giữ bản mặc
// định 8.3, pool tpan-idle và kiểm chứng vẫn của 8.3, không đổi gì.
func TestStackRerunKeepsPHPVersion(t *testing.T) {
	s, m, st := newTestStack(t)
	in, err := s.PrepareInstall(ctx, Nginx)
	if err != nil {
		t.Fatal(err)
	}
	if rep, err := in.Execute(ctx, nil); err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("cài lần đầu: %v %s", err, rep.Job.Error)
	}
	m.SetDir("/etc/php/8.4/fpm/pool.d", 0o755, system.Root)
	m.units["php8.4-fpm"] = unitState{active: true, enabled: true}

	in2, err := s.PrepareInstall(ctx, Nginx)
	if err != nil {
		t.Fatal(err)
	}
	if in2.Config.PHPVersion != "8.3" {
		t.Errorf("Config.PHPVersion = %q", in2.Config.PHPVersion)
	}
	if id := in2.Plan.Steps[slices.IndexFunc(in2.Plan.Steps, func(s engine.Step) bool {
		_, ok := s.(*steps.PHPIdlePool)
		return ok
	})].Info().ID; id != "php.idle:8.3" {
		t.Errorf("pool tpan-idle của bản %s; muốn 8.3", id)
	}
	before := m.Snapshot()
	rep, err := in2.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("chạy lại: %v %s %s", err, rep.Job.Status, rep.Job.Error)
	}
	for _, r := range rep.Steps {
		if r.Outcome != "skipped" {
			t.Errorf("chạy lại: bước %s outcome %s", r.ID, r.Outcome)
		}
	}
	if !reflect.DeepEqual(before, m.Snapshot()) {
		t.Error("chạy lại làm đổi hệ thống file")
	}
	if v, _, _ := st.GetSetting(ctx, SettingPHPVersion); v != "8.3" {
		t.Errorf("php_version = %q sau khi chạy lại; muốn giữ 8.3", v)
	}
	for _, id := range []string{"service.active:php8.3-fpm", "config.phpfpm:8.3", "php.www_pool:8.3"} {
		if checkByID(rep.Checks, id) == nil {
			t.Errorf("kiểm chứng thiếu %s", id)
		}
	}
	for _, r := range rep.Checks {
		if strings.Contains(r.ID, "8.4") {
			t.Errorf("kiểm chứng nhắm bản 8.4: %s", r.ID)
		}
	}
}

// nginx.org: chạy Plan (không gồm bước thêm kho, cần gpg thật) trên sim với bố
// cục của gói nginx.org. Lần đầu thêm nginx vào www-data và khởi động lại;
// chạy lại thì mọi bước bỏ qua và không khởi động lại gì.
func TestNginxOrgAdaptation(t *testing.T) {
	m := newSim(t)
	groups := map[string][]string{"nginx": {"nginx"}}
	m.Handle([]string{"apt-get", "install", "-y"}, func(c system.Cmd) system.Result {
		m.mu.Lock()
		defer m.mu.Unlock()
		for _, p := range c.Args[2:] {
			if p != "nginx" {
				m.install(p)
				continue
			}
			// Gói của nginx.org: conf.d/default.conf, không có sites-*.
			m.pkgs[p] = true
			m.SetFile("/etc/nginx/conf.d/default.conf", []byte("server { listen 80; }\n"), 0o644, system.Root)
			m.SetDir("/etc/logrotate.d", 0o755, system.Root)
		}
		return system.Result{}
	})
	m.Handle([]string{"id", "-nG"}, func(c system.Cmd) system.Result {
		m.mu.Lock()
		defer m.mu.Unlock()
		g, ok := groups[c.Args[1]]
		if !ok {
			return system.Result{ExitCode: 1, Stderr: "id: no such user\n"}
		}
		return system.Result{Stdout: strings.Join(g, " ") + "\n"}
	})
	m.Handle([]string{"usermod", "-aG"}, func(c system.Cmd) system.Result {
		m.mu.Lock()
		defer m.mu.Unlock()
		groups[c.Args[2]] = append(groups[c.Args[2]], c.Args[1])
		return system.Result{}
	})

	cfg := StackConfig{Web: Nginx, WebSource: SourceNginxStable, DBSource: SourceUbuntu, PHPSource: SourceUbuntu}
	pkgs, err := Packages(cfg)
	if err != nil {
		t.Fatal(err)
	}
	run := func() engine.Report {
		t.Helper()
		php := new(string)
		plan, err := installPlan(cfg, pkgs, php, "2.12.0", fakePharSHA())
		if err != nil {
			t.Fatal(err)
		}
		rep, err := engine.New(m, engine.Options{}).Execute(ctx, plan)
		if err != nil || rep.Job.Status != engine.JobSucceeded {
			t.Fatalf("Execute: %v %s %s%s", err, rep.Job.Status, rep.Job.Error, failedChecks(rep.Checks))
		}
		return rep
	}
	run()
	if !slices.Contains(groups["nginx"], "www-data") {
		t.Errorf("nginx không vào nhóm www-data: %v", groups)
	}
	if _, err := m.Stat("/etc/nginx/conf.d/default.conf"); err == nil {
		t.Error("conf.d/default.conf của gói nginx.org còn")
	}
	inc, err := m.ReadFile("/etc/nginx/conf.d/tpan.conf")
	if err != nil || !strings.Contains(string(inc), "include /etc/nginx/sites-enabled/*;") {
		t.Errorf("conf.d/tpan.conf = %q, %v", inc, err)
	}
	if target, err := m.Readlink("/etc/nginx/sites-enabled/tpan-default.conf"); err != nil || target == "" {
		t.Errorf("site mặc định chưa bật: %q %v", target, err)
	}
	restarts := func() int {
		n := 0
		for _, c := range m.Ran() {
			if c.Name == "systemctl" && slices.Equal(c.Args, []string{"restart", "nginx"}) {
				n++
			}
		}
		return n
	}
	if n := restarts(); n != 1 {
		t.Errorf("lần đầu khởi động lại nginx %d lần; muốn 1", n)
	}

	m.ResetRan()
	before := m.Snapshot()
	rep := run()
	for _, r := range rep.Steps {
		if r.Outcome != "skipped" {
			t.Errorf("chạy lại: bước %s outcome %s", r.ID, r.Outcome)
		}
	}
	if n := restarts(); n != 0 {
		t.Errorf("chạy lại vẫn khởi động lại nginx %d lần", n)
	}
	for _, c := range m.Ran() {
		if c.Name == "usermod" {
			t.Errorf("chạy lại chạy %s", c.String())
		}
	}
	if !reflect.DeepEqual(before, m.Snapshot()) {
		t.Error("chạy lại làm đổi hệ thống file")
	}
}

func TestGroupRestart(t *testing.T) {
	setup := func(member bool, restartCode int) (*system.FakeSystem, *groupRestart, *bool) {
		f := system.NewFake()
		in := member
		f.Handle([]string{"id", "-nG", "nginx"}, func(system.Cmd) system.Result {
			if in {
				return system.Result{Stdout: "nginx www-data\n"}
			}
			return system.Result{Stdout: "nginx\n"}
		})
		f.Handle([]string{"usermod"}, func(system.Cmd) system.Result { in = true; return system.Result{} })
		f.Handle([]string{"gpasswd"}, func(system.Cmd) system.Result { in = false; return system.Result{} })
		f.Handle([]string{"systemctl", "restart"}, func(system.Cmd) system.Result {
			return system.Result{ExitCode: restartCode, Stderr: "giả lập lỗi\n"}
		})
		return f, &groupRestart{
			add:  &steps.UserInGroup{User: "nginx", Group: "www-data"},
			unit: "nginx",
			test: &system.Cmd{Name: "nginx", Args: []string{"-t"}},
		}, &in
	}
	ran := func(f *system.FakeSystem) []string {
		var out []string
		for _, c := range f.Ran() {
			out = append(out, c.String())
		}
		return out
	}

	t.Run("chưa trong nhóm: thêm rồi khởi động lại", func(t *testing.T) {
		f, s, in := setup(false, 0)
		if done, err := s.Check(ctx, f); done || err != nil {
			t.Fatalf("Check = %v, %v", done, err)
		}
		if err := s.Apply(ctx, f); err != nil {
			t.Fatal(err)
		}
		want := []string{"id -nG nginx", "usermod -aG www-data nginx", "nginx -t", "systemctl restart nginx"}
		if got := ran(f)[1:]; !reflect.DeepEqual(got, want) {
			t.Errorf("lệnh = %q; muốn %q", got, want)
		}
		if done, _ := s.Check(ctx, f); !done || !*in {
			t.Error("sau Apply Check phải đúng")
		}
		if err := s.Undo(ctx, f); err != nil || *in {
			t.Errorf("Undo: %v, còn trong nhóm %v", err, *in)
		}
		if !s.Info().Danger || !slices.Contains(s.Info().Commands, "systemctl restart nginx") {
			t.Errorf("Info = %+v", s.Info())
		}
	})

	t.Run("đã trong nhóm: không lệnh thay đổi nào", func(t *testing.T) {
		f, s, _ := setup(true, 0)
		if done, err := s.Check(ctx, f); !done || err != nil {
			t.Fatalf("Check = %v, %v", done, err)
		}
		if got := ran(f); !reflect.DeepEqual(got, []string{"id -nG nginx"}) {
			t.Errorf("lệnh = %q", got)
		}
	})

	t.Run("khởi động lại lỗi: bỏ user ra khỏi nhóm", func(t *testing.T) {
		f, s, in := setup(false, 1)
		err := s.Apply(ctx, f)
		if err == nil || !strings.Contains(err.Error(), "khởi động lại nginx") {
			t.Fatalf("Apply = %v; muốn lỗi khởi động lại", err)
		}
		if *in {
			t.Error("Apply lỗi mà user vẫn trong nhóm")
		}
	})
}
