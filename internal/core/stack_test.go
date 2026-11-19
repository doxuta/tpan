package core

import (
	"context"
	"errors"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

var ctx = context.Background()

func stepIDs(p engine.Plan) []string {
	ids := make([]string, len(p.Steps))
	for i, s := range p.Steps {
		ids[i] = s.Info().ID
	}
	return ids
}

func mustSet(t *testing.T, st *store.Store, key, value string) {
	t.Helper()
	if err := st.SetSetting(ctx, key, value); err != nil {
		t.Fatalf("ghi setting %s: %v", key, err)
	}
}

func refusedCode(t *testing.T, err error) string {
	t.Helper()
	var re *RefusedError
	if !errors.As(err, &re) {
		t.Fatalf("lỗi = %v; muốn *RefusedError", err)
	}
	if re.Message.Vi == "" || re.Message.En == "" {
		t.Errorf("RefusedError %s thiếu lời ở một ngôn ngữ: %+v", re.Code, re.Message)
	}
	return re.Code
}

func TestPackages(t *testing.T) {
	// Mục 3: không bao giờ có php, php<bản> hay libapache2-mod-php*.
	banned := regexp.MustCompile(`^(php|php[0-9.]+|libapache2-mod-php.*)$`)
	for _, tc := range []struct {
		web       WebServer
		want, not string
	}{
		{Nginx, "nginx", "apache2"},
		{Apache, "apache2", "nginx"},
	} {
		pkgs, err := Packages(StackConfig{Web: tc.web})
		if err != nil {
			t.Fatalf("%s: %v", tc.web, err)
		}
		for _, p := range pkgs {
			if banned.MatchString(p) {
				t.Errorf("%s: danh sách gói có %q (bị cấm, kéo theo mod_php)", tc.web, p)
			}
		}
		for _, p := range []string{tc.want, "mariadb-server", "php-fpm", "php-mysql", "certbot", "curl"} {
			if !slices.Contains(pkgs, p) {
				t.Errorf("%s: thiếu gói %q trong %v", tc.web, p, pkgs)
			}
		}
		if slices.Contains(pkgs, tc.not) {
			t.Errorf("%s: có gói của web server kia %q", tc.web, tc.not)
		}
	}

	if _, err := Packages(StackConfig{Web: Nginx, DBSource: "mariadb.org"}); refusedCode(t, err) != "source_unsupported" {
		t.Error("nguồn DB khác ubuntu phải bị từ chối")
	}
	if _, err := Packages(StackConfig{Web: "caddy"}); refusedCode(t, err) != "web_invalid" {
		t.Error("web lạ phải bị từ chối")
	}
}

var (
	wantCommonHead = []string{
		"apt.update:lists",
		"", // apt.install:<gói>, so riêng
		"service:mariadb",
		"file:/etc/mysql/mariadb.conf.d/99-tpan.cnf",
		"mariadb.secure:localhost",
		"php.detect:/etc/php",
		"dir:/var/lib/tpan",
		"dir:/var/lib/tpan/acme",
	}
	wantCommonTail = []string{
		"dir:/var/cache/tpan",
		"wpcli:/usr/local/bin/wp",
		"file:/etc/logrotate.d/tpan-sites",
	}
)

func TestPlanOrder(t *testing.T) {
	for _, tc := range []struct {
		web WebServer
		mid []string
	}{
		{Nginx, []string{
			"absent:/etc/nginx/sites-enabled/default",
			"file:/etc/nginx/sites-available/tpan-default.conf",
			"symlink:/etc/nginx/sites-enabled/tpan-default.conf",
			"service:nginx",
		}},
		{Apache, []string{
			"apache.modules:apache2",
			"absent:/etc/apache2/sites-enabled/000-default.conf",
			"file:/etc/apache2/sites-available/000-tpan-default.conf",
			"symlink:/etc/apache2/sites-enabled/000-tpan-default.conf",
			"service:apache2",
		}},
	} {
		t.Run(string(tc.web), func(t *testing.T) {
			s, _, _ := newTestStack(t)
			in, err := s.PrepareInstall(ctx, tc.web)
			if err != nil {
				t.Fatalf("PrepareInstall: %v", err)
			}
			if in.Plan.Kind != "stack.install" || in.Plan.Target != string(tc.web) {
				t.Errorf("Kind/Target = %q/%q", in.Plan.Kind, in.Plan.Target)
			}
			want := slices.Concat(wantCommonHead, tc.mid, wantCommonTail)
			got := stepIDs(in.Plan)
			pkgs, _ := Packages(StackConfig{Web: tc.web})
			want[1] = "apt.install:" + strings.Join(pkgs, ",")
			if !reflect.DeepEqual(got, want) {
				t.Errorf("thứ tự bước:\ncó    %q\nmuốn  %q", got, want)
			}
			if len(in.Plan.Verify) != 10 {
				t.Errorf("Verify có %d phép kiểm, muốn 10", len(in.Plan.Verify))
			}
		})
	}
}

func TestPrepareInstallRefusals(t *testing.T) {
	t.Run("web lạ", func(t *testing.T) {
		s, m, _ := newTestStack(t)
		_, err := s.PrepareInstall(ctx, "caddy")
		if refusedCode(t, err) != "web_invalid" {
			t.Error("muốn web_invalid")
		}
		if len(m.Ran()) != 0 {
			t.Errorf("từ chối mà vẫn chạy lệnh: %v", m.Ran())
		}
	})

	t.Run("lần đầu, cổng 80 bận", func(t *testing.T) {
		s, m, _ := newTestStack(t)
		m.ss = "LISTEN 0 511 0.0.0.0:80 0.0.0.0:* users:((\"caddy\",pid=9,fd=6))\n"
		_, err := s.PrepareInstall(ctx, Nginx)
		if refusedCode(t, err) != "precondition_failed" {
			t.Fatal("muốn precondition_failed")
		}
		var re *RefusedError
		errors.As(err, &re)
		if len(re.Checks) != 1 || re.Checks[0].Status != checks.Fail || !strings.HasPrefix(re.Checks[0].ID, "ports.free:") {
			t.Errorf("Checks = %+v; muốn đúng một ports.free Fail", re.Checks)
		}
		if len(re.Checks[0].ManualFix) == 0 {
			t.Error("điều kiện hỏng thiếu ManualFix")
		}
	})

	t.Run("lần đầu, đã có mysql-server", func(t *testing.T) {
		s, m, _ := newTestStack(t)
		m.pkgs["mysql-server"] = true
		_, err := s.PrepareInstall(ctx, Nginx)
		if refusedCode(t, err) != "precondition_failed" {
			t.Fatal("muốn precondition_failed")
		}
	})

	t.Run("không phải ubuntu 24.04", func(t *testing.T) {
		s, m, _ := newTestStack(t)
		m.SetFile("/etc/os-release", []byte("ID=debian\nVERSION_ID=\"12\"\n"), 0o644, system.Root)
		_, err := s.PrepareInstall(ctx, Nginx)
		if refusedCode(t, err) != "precondition_failed" {
			t.Fatal("muốn precondition_failed")
		}
	})

	t.Run("đã cài cùng web thì bỏ kiểm cổng và gói", func(t *testing.T) {
		s, m, st := newTestStack(t)
		mustSet(t, st, SettingWeb, "nginx")
		// Máy đã có nginx và mariadb do chính tpan cài: cổng 80 bận, gói có mặt.
		m.ss = "LISTEN 0 511 0.0.0.0:80 0.0.0.0:* users:((\"nginx\",pid=9,fd=6))\n"
		m.pkgs["nginx"], m.pkgs["mariadb-server"] = true, true
		in, err := s.PrepareInstall(ctx, Nginx)
		if err != nil {
			t.Fatalf("chạy lại cùng web phải qua: %v", err)
		}
		ids := make([]string, len(in.Preconditions))
		for i, r := range in.Preconditions {
			ids[i] = r.ID
		}
		if !reflect.DeepEqual(ids, []string{"os.release:ubuntu-24.04"}) {
			t.Errorf("điều kiện đầu khi chạy lại = %v; muốn chỉ os.release", ids)
		}
	})

	t.Run("đã cài web khác", func(t *testing.T) {
		s, m, st := newTestStack(t)
		mustSet(t, st, SettingWeb, "nginx")
		_, err := s.PrepareInstall(ctx, Apache)
		if refusedCode(t, err) != "stack_switch_required" {
			t.Error("muốn stack_switch_required")
		}
		var re *RefusedError
		errors.As(err, &re)
		if !strings.Contains(re.Message.Vi, "tpan stack switch") {
			t.Errorf("lời từ chối không chỉ sang tpan stack switch: %q", re.Message.Vi)
		}
		if len(m.Ran()) != 0 {
			t.Errorf("từ chối mà vẫn chạy lệnh: %v", m.Ran())
		}
	})

	t.Run("nguồn khác ubuntu", func(t *testing.T) {
		s, _, st := newTestStack(t)
		mustSet(t, st, SettingWebSource, "nginx.org")
		_, err := s.PrepareInstall(ctx, Nginx)
		if refusedCode(t, err) != "source_unsupported" {
			t.Error("muốn source_unsupported")
		}
		var re *RefusedError
		errors.As(err, &re)
		if !strings.Contains(re.Message.Vi, "chưa hỗ trợ ở bản này") {
			t.Errorf("lời từ chối = %q", re.Message.Vi)
		}
	})

	t.Run("stack.web trong settings có giá trị lạ", func(t *testing.T) {
		s, _, st := newTestStack(t)
		mustSet(t, st, SettingWeb, "caddy")
		if _, err := s.PrepareInstall(ctx, Nginx); err == nil {
			t.Error("muốn lỗi")
		}
	})
}

func TestPreviewChangesNothing(t *testing.T) {
	for _, web := range []WebServer{Nginx, Apache} {
		t.Run(string(web), func(t *testing.T) {
			s, m, st := newTestStack(t)
			before := m.Snapshot()
			in, err := s.PrepareInstall(ctx, web)
			if err != nil {
				t.Fatal(err)
			}
			pv, err := in.Preview(ctx)
			if err != nil {
				t.Fatalf("Preview: %v", err)
			}
			if len(pv.Steps) != len(in.Plan.Steps) {
				t.Errorf("Preview có %d bước, Plan có %d", len(pv.Steps), len(in.Plan.Steps))
			}
			for _, c := range m.Ran() {
				if !readOnly(c) {
					t.Errorf("xem trước chạy lệnh thay đổi máy: %s", c.String())
				}
			}
			if !reflect.DeepEqual(before, m.Snapshot()) {
				t.Error("xem trước làm đổi hệ thống file")
			}
			if all, _ := st.Settings(ctx); len(all) != 0 {
				t.Errorf("xem trước ghi settings: %v", all)
			}
			if jobs, _ := st.Jobs(ctx, 10); len(jobs) != 0 {
				t.Errorf("xem trước tạo job: %v", jobs)
			}
		})
	}
}

func TestPreviewWithEmptySettings(t *testing.T) {
	// --dry-run khi chưa có file DB: CLI đưa EmptySettings thay cho store.
	m := newSim(t)
	s := &Stack{Sys: m, Settings: EmptySettings{}}
	in, err := s.PrepareInstall(ctx, Nginx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := in.Preview(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := in.Execute(ctx, nil); err == nil {
		t.Error("Execute không có Journal phải lỗi")
	}
}

func TestSettingsWrittenBeforeExecute(t *testing.T) {
	s, m, st := newTestStack(t)
	var webAtUpdate, phpAtUpdate string
	m.onUpdate = func() int {
		webAtUpdate, _, _ = st.GetSetting(ctx, SettingWeb)
		phpAtUpdate, _, _ = st.GetSetting(ctx, SettingPHPVersion)
		return 100 // apt-get update hỏng: job dừng ngay bước đầu
	}
	in, err := s.PrepareInstall(ctx, Nginx)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := in.Execute(ctx, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if rep.Job.Status == engine.JobSucceeded {
		t.Fatal("job phải hỏng")
	}
	if webAtUpdate != "nginx" || phpAtUpdate != "" {
		t.Errorf("lúc apt-get update: stack.web=%q php_version=%q; muốn nginx và rỗng", webAtUpdate, phpAtUpdate)
	}
	for k, want := range map[string]string{SettingWebSource: "ubuntu", SettingDBSource: "ubuntu", SettingPHPSource: "ubuntu"} {
		if v, _, _ := st.GetSetting(ctx, k); v != want {
			t.Errorf("%s = %q, muốn %q", k, v, want)
		}
	}
	if _, ok, _ := st.GetSetting(ctx, SettingPHPVersion); ok {
		t.Error("php_version được ghi dù job hỏng")
	}
	// Job phải nằm trong store (Journal là store).
	if _, err := st.Job(ctx, rep.Job.ID); err != nil {
		t.Errorf("job %s không có trong store: %v", rep.Job.ID, err)
	}

	// Lần chạy lại sau lần hỏng: máy đã có gói do tpan cài dở, vẫn phải qua.
	m.onUpdate = nil
	m.pkgs["nginx"] = true
	m.ss = "LISTEN 0 511 0.0.0.0:80 0.0.0.0:*\n"
	if _, err := s.PrepareInstall(ctx, Nginx); err != nil {
		t.Errorf("chạy lại sau lần hỏng phải qua điều kiện đầu: %v", err)
	}
}

func TestFullInstall(t *testing.T) {
	for _, web := range []WebServer{Nginx, Apache} {
		t.Run(string(web), func(t *testing.T) {
			s, m, st := newTestStack(t)
			in, err := s.PrepareInstall(ctx, web)
			if err != nil {
				t.Fatal(err)
			}
			var kinds []engine.EventKind
			rep, err := in.Execute(ctx, func(ev engine.Event) { kinds = append(kinds, ev.Kind) })
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if rep.Job.Status != engine.JobSucceeded {
				for _, r := range rep.Checks {
					if r.Status != checks.Pass {
						t.Logf("kiểm chứng %s: %s %s", r.ID, r.Status, r.Summary.Vi)
					}
				}
				t.Fatalf("job %s: %s", rep.Job.Status, rep.Job.Error)
			}
			if len(kinds) == 0 || kinds[0] != engine.EventJobStart {
				t.Errorf("OnEvent không nhận sự kiện: %v", kinds)
			}
			for _, r := range rep.Checks {
				if r.Status != checks.Pass {
					t.Errorf("kiểm chứng %s: %s %s", r.ID, r.Status, r.Summary.Vi)
				}
			}
			if v, _, _ := st.GetSetting(ctx, SettingPHPVersion); v != "8.3" {
				t.Errorf("php_version = %q, muốn 8.3", v)
			}
			cnf, err := m.ReadFile("/etc/mysql/mariadb.conf.d/99-tpan.cnf")
			if err != nil || !strings.Contains(string(cnf), "bind-address = 127.0.0.1") {
				t.Errorf("99-tpan.cnf = %q, %v", cnf, err)
			}
			if fi, err := m.Stat("/var/lib/tpan"); err != nil || fi.Mode.Perm() != 0o755 {
				t.Errorf("/var/lib/tpan: %v %v; muốn 0755", fi.Mode, err)
			}

			// Status và health sau khi cài.
			stt, err := s.Status(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !stt.Installed || stt.Config.Web != web || len(stt.Services) != 3 {
				t.Errorf("Status = %+v", stt)
			}
			h, err := s.Health(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !h.Installed || len(h.Results) != 10 {
				t.Fatalf("Health: installed=%v, %d kết quả", h.Installed, len(h.Results))
			}
			for _, r := range h.Results {
				if r.Status != checks.Pass {
					t.Errorf("health %s: %s %s", r.ID, r.Status, r.Summary.Vi)
				}
			}
			if h.Results[1].ID != "service.active:php8.3-fpm" {
				t.Errorf("phép kiểm thứ hai = %s; muốn service.active:php8.3-fpm", h.Results[1].ID)
			}

			// Chạy lại: mọi bước bỏ qua, không lệnh thay đổi nào.
			in2, err := s.PrepareInstall(ctx, web)
			if err != nil {
				t.Fatalf("chạy lại: %v", err)
			}
			m.ResetRan()
			before := m.Snapshot()
			rep2, err := in2.Execute(ctx, nil)
			if err != nil || rep2.Job.Status != engine.JobSucceeded {
				t.Fatalf("chạy lại: %v %s %s", err, rep2.Job.Status, rep2.Job.Error)
			}
			for _, r := range rep2.Steps {
				if r.Outcome != "skipped" {
					t.Errorf("chạy lại: bước %s outcome %s", r.ID, r.Outcome)
				}
			}
			for _, c := range m.Ran() {
				if !readOnly(c) && c.Name != "curl" && c.Name != "nginx" && c.Name != "apachectl" && !strings.HasPrefix(c.Name, "php-fpm") {
					t.Errorf("chạy lại chạy lệnh thay đổi máy: %s", c.String())
				}
			}
			if !reflect.DeepEqual(before, m.Snapshot()) {
				t.Error("chạy lại làm đổi hệ thống file")
			}
		})
	}
}

func TestHealthStatusNotInstalled(t *testing.T) {
	s, m, st := newTestStack(t)
	h, err := s.Health(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if h.Installed || len(h.Results) != 0 {
		t.Errorf("Health khi chưa cài = %+v", h)
	}
	if len(m.Ran()) != 0 {
		t.Errorf("health khi chưa cài vẫn chạy lệnh: %v", m.Ran())
	}
	stt, err := s.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stt.Installed || len(stt.Services) != 0 {
		t.Errorf("Status khi chưa có gì = %+v", stt)
	}
	if stt.Config.WebSource != SourceUbuntu {
		t.Errorf("nguồn rỗng phải hiểu là ubuntu: %+v", stt.Config)
	}

	// Cài dở (có stack.web, chưa có php_version): vẫn là chưa cài, nhưng status
	// cho xem dịch vụ web và mariadb.
	mustSet(t, st, SettingWeb, "nginx")
	if h, _ := s.Health(ctx); h.Installed {
		t.Error("chưa có php_version mà Health coi là đã cài")
	}
	stt, _ = s.Status(ctx)
	if stt.Installed || len(stt.Services) != 2 {
		t.Errorf("Status khi cài dở = installed %v, %d dịch vụ; muốn false, 2", stt.Installed, len(stt.Services))
	}
}

// Bật ssl mà chưa có vhost 443 thì Apache phục vụ trang mặc định của Ubuntu ở
// cổng 443 (thấy trên máy ảo). M1 không được bật ssl; M5 bật cùng vhost 443.
func TestApacheNoSSLModuleInM1(t *testing.T) {
	if slices.Contains(apacheModules, "ssl") {
		t.Fatalf("apacheModules có ssl: %v", apacheModules)
	}
}
