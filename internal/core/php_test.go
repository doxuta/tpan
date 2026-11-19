package core

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
)

// phpEnv là siteEnv (stack nginx, PHP 8.3 mặc định) cộng ca sử dụng PHP và
// lệnh apt-cache giả: search trả avail, madison trả from[bản] ("ubuntu", "sury"
// hoặc cả hai, cách nhau dấu phẩy).
type phpEnv struct {
	*siteEnv
	p     *PHP
	avail []string
	from  map[string]string
}

func newPHPEnv(t *testing.T) *phpEnv {
	t.Helper()
	se := newSiteEnv(t, Nginx)
	pe := &phpEnv{
		siteEnv: se,
		p:       &PHP{Sys: se.m, Settings: se.st, Sites: se.sites, Journal: se.st},
		avail:   []string{"8.3"},
		from:    map[string]string{"8.3": "ubuntu"},
	}
	pe.installPHP("8.3")
	se.m.SetFile("/etc/os-release", []byte(osReleaseNoble), 0o644, system.Root)
	se.m.Handle([]string{"apt-cache", "search"}, func(system.Cmd) system.Result {
		var b strings.Builder
		for _, v := range pe.avail {
			b.WriteString("php" + v + "-fpm - server-side, HTML-embedded scripting language (FPM-CGI binary)\n")
		}
		return system.Result{Stdout: b.String()}
	})
	se.m.Handle([]string{"apt-cache", "madison"}, func(c system.Cmd) system.Result {
		var b strings.Builder
		for _, pkg := range c.Args[1:] {
			v, _ := phpFromPkg(pkg)
			for _, src := range strings.Split(pe.from[v], ",") {
				switch src {
				case "ubuntu": // mirror của nhà cung cấp, không phải *.ubuntu.com
					b.WriteString(" " + pkg + " | " + v + ".6-0ubuntu0.24.04.13 | https://mirror.example.com/ubuntu noble-updates/universe amd64 Packages\n")
				case "sury":
					b.WriteString(" " + pkg + " | " + v + ".1-1+ubuntu24.04.1+deb.sury.org+1 | https://packages.sury.org/php noble/main amd64 Packages\n")
				}
			}
		}
		return system.Result{Stdout: b.String()}
	})
	midCheckWait = 0
	t.Cleanup(func() { midCheckWait = time.Second })
	return pe
}

// installPHP giả lập gói php<v>-fpm đã cài: thư mục pool, file chạy, và
// reload tạo hoặc gỡ socket theo các file pool. Thay cả handler 8.3 của
// siteSim: handler đó gỡ mọi socket không thuộc 8.3, kể cả socket của 8.4.
func (pe *phpEnv) installPHP(v string) {
	m := pe.m
	m.SetDir("/etc/php/"+v+"/fpm/pool.d", 0o755, system.Root)
	m.SetFile("/usr/sbin/php-fpm"+v, []byte("ELF"), 0o755, system.Root)
	m.Handle([]string{"systemctl", "reload", "php" + v + "-fpm"}, func(system.Cmd) system.Result {
		names, _ := m.ReadDir("/etc/php/" + v + "/fpm/pool.d")
		want := map[string]bool{}
		for _, n := range names {
			if pool, ok := strings.CutSuffix(n, ".conf"); ok {
				want["/run/php/"+pool+"-"+v+".sock"] = true
			}
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		for s := range m.sockets {
			if strings.HasSuffix(s, "-"+v+".sock") && !want[s] {
				_ = m.Remove(s)
				delete(m.sockets, s)
			}
		}
		for s := range want {
			m.SetSocket(s, 0o660, system.Owner{User: "www-data", Group: "www-data"})
			m.sockets[s] = true
		}
		return system.Result{}
	})
}

func TestPHPVersionRefusals(t *testing.T) {
	for _, bad := range []string{"", "8", "8.3.1", "8.3; rm -rf /", "$(id)", "../../etc/passwd", "8.3\n8.4", "php8.3", strings.Repeat("8", 300) + ".3", "８.３"} {
		pe := newPHPEnv(t)
		pe.m.ResetRan()
		d := mustDomain(t, "shop.example.com")
		for name, call := range map[string]func() error{
			"install": func() error { _, err := pe.p.PrepareInstall(ctx, bad); return err },
			"use":     func() error { _, err := pe.p.PrepareUse(ctx, d, bad); return err },
			"remove":  func() error { _, err := pe.p.PrepareRemove(ctx, bad); return err },
		} {
			if code := refusedCode(t, call()); code != "php_version_invalid" {
				t.Errorf("%s %q: mã %s", name, bad, code)
			}
		}
		if ran := pe.m.Ran(); len(ran) != 0 {
			t.Errorf("%q: từ chối mà vẫn chạy lệnh %v", bad, ran)
		}
	}
}

func TestPHPList(t *testing.T) {
	pe := newPHPEnv(t)
	pe.avail = []string{"8.3", "8.4", "8.10", "7.4"}
	pe.from = map[string]string{"8.3": "ubuntu,sury", "8.4": "sury", "8.10": "sury", "7.4": "sury"}
	pe.m.SetFile(RepoSourcesPath(RepoSury), []byte("Types: deb\n"), 0o644, system.Root)
	pe.installPHP("8.4")
	// 8.2: gói đã gỡ (apt-get remove giữ /etc/php/8.2/fpm) thì không còn là đã cài.
	pe.m.SetDir("/etc/php/8.2/fpm/pool.d", 0o755, system.Root)
	for _, s := range []site.Site{
		{Domain: mustDomain(t, "b.example.com"), Status: site.Active, PHPVersion: "8.4"},
		{Domain: mustDomain(t, "a.example.com"), Status: site.Trashed, PHPVersion: "8.4"},
		{Domain: mustDomain(t, "old.example.com"), Status: site.Active}, // site của M2: bản của stack
	} {
		if err := pe.sites.SaveSite(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	curlOK := true
	pe.m.Handle([]string{"curl", "-fsS"}, func(c system.Cmd) system.Result {
		if !curlOK {
			return system.Result{ExitCode: 6, Stderr: "curl: (6) Could not resolve host\n"}
		}
		switch c.Args[len(c.Args)-1] {
		case phpStatesURL:
			return system.Result{Stdout: `{"8":{"8.3":{"state":"security","active_support_end":"2025-12-31T00:00:00+00:00","security_support_end":"2027-12-31T00:00:00+00:00"},"8.4":{"state":"stable","active_support_end":"2026-12-31T00:00:00+00:00","security_support_end":"2028-12-31T00:00:00+00:00"}}}`}
		case wpServeHappy + "8.3":
			return system.Result{Stdout: `{"recommended_version":"8.3","minimum_version":"7.4","is_supported":true}`}
		}
		return system.Result{ExitCode: 22, Stderr: "curl: (22) 404\n"}
	})

	list, err := pe.p.List(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	var vs []string
	for _, r := range list.Versions {
		vs = append(vs, r.Version)
	}
	if !reflect.DeepEqual(vs, []string{"7.4", "8.3", "8.4", "8.10"}) {
		t.Fatalf("thứ tự bản = %v", vs)
	}
	row := func(v string) PHPVersionRow { return list.Versions[slices.Index(vs, v)] }
	if r := row("8.3"); !r.Installed || !r.Default || r.Source != SourceUbuntu || !reflect.DeepEqual(r.Sites, []string{"old.example.com"}) {
		t.Errorf("8.3 = %+v", r)
	}
	if r := row("8.4"); !r.Installed || r.Default || r.Source != SourceSury || !reflect.DeepEqual(r.Sites, []string{"a.example.com", "b.example.com"}) {
		t.Errorf("8.4 = %+v", r)
	}
	if r := row("8.10"); r.Installed || r.Sites == nil || len(r.Sites) != 0 {
		t.Errorf("8.10 = %+v (Sites phải là [] chứ không nil, để JSON là [])", r)
	}
	if s := row("8.3").Support; s == nil || *s != (PHPSupport{State: "security", ActiveUntil: "2025-12-31", SecurityUntil: "2027-12-31"}) {
		t.Errorf("hạn hỗ trợ 8.3 = %+v", s)
	}
	if s := row("7.4").Support; s == nil || s.State != "eol" {
		t.Errorf("7.4 cũ hơn mọi nhánh php.net còn liệt kê, muốn eol: %+v", s)
	}
	if row("8.10").Support != nil {
		t.Error("8.10 không có trong php.net (bản mới hơn) mà có hạn hỗ trợ")
	}
	if !list.SuryEnabled || list.WPRecommended != "8.3" || !list.NotesOK {
		t.Errorf("SuryEnabled=%v WPRecommended=%q NotesOK=%v", list.SuryEnabled, list.WPRecommended, list.NotesOK)
	}

	// Không có mạng: danh sách vẫn đủ, chỉ thiếu ghi chú, không lỗi.
	curlOK = false
	list2, err := pe.p.List(ctx, true)
	if err != nil {
		t.Fatalf("không mạng mà List lỗi: %v", err)
	}
	if len(list2.Versions) != 4 || list2.NotesOK || list2.WPRecommended != "" || list2.Versions[1].Support != nil {
		t.Errorf("không mạng: %+v", list2)
	}
	// notes=false thì không gọi mạng.
	pe.m.ResetRan()
	if _, err := pe.p.List(ctx, false); err != nil {
		t.Fatal(err)
	}
	for _, c := range pe.m.Ran() {
		if c.Name == "curl" {
			t.Errorf("notes=false mà vẫn gọi %s", c.String())
		}
	}
}

func TestPHPInstallPlan(t *testing.T) {
	pkgs := func(v string) string { return "apt.install:" + strings.Join(phpPackages(v), ",") }
	verify := func(t *testing.T, p engine.Plan, v string) {
		t.Helper()
		var ids []string
		for _, c := range p.Verify {
			ids = append(ids, c.Run(ctx, system.NewFake()).ID)
		}
		want := []string{"service.active:php" + v + "-fpm", "config.phpfpm:" + v, "php.www_pool:" + v}
		if !reflect.DeepEqual(ids, want) {
			t.Errorf("Verify = %v; muốn %v", ids, want)
		}
	}

	t.Run("bản của Ubuntu: không thêm kho", func(t *testing.T) {
		pe := newPHPEnv(t)
		pl, err := pe.p.PrepareInstall(ctx, "8.3")
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"apt.update:lists", pkgs("8.3"), "php.idle:8.3", "service:php8.3-fpm"}
		if got := stepIDs(pl.Plan); !reflect.DeepEqual(got, want) {
			t.Errorf("bước = %q; muốn %q", got, want)
		}
		if pl.Plan.Kind != "php.install" || pl.Plan.Target != "8.3" {
			t.Errorf("Kind/Target = %s/%s", pl.Plan.Kind, pl.Plan.Target)
		}
		verify(t, pl.Plan, "8.3")
	})

	t.Run("Ubuntu không có, chưa có kho sury: thêm kho", func(t *testing.T) {
		pe := newPHPEnv(t)
		pl, err := pe.p.PrepareInstall(ctx, "8.4")
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"apt.repo:sury", pkgs("8.4"), "php.idle:8.4", "service:php8.4-fpm"}
		if got := stepIDs(pl.Plan); !reflect.DeepEqual(got, want) {
			t.Errorf("bước = %q; muốn %q", got, want)
		}
		r := repoOf(t, pl.Plan, RepoSury)
		if !r.Info().Danger || !strings.Contains(string(r.Pin), "Package: php-common php8.4*\n") || strings.Contains(string(r.Pin), "php8.3") {
			t.Errorf("ghim:\n%s", r.Pin)
		}
		verify(t, pl.Plan, "8.4")
	})

	t.Run("kho sury đã có: ghim đủ mọi bản đang cài từ kho", func(t *testing.T) {
		pe := newPHPEnv(t)
		pe.m.SetFile(RepoSourcesPath(RepoSury), []byte("Types: deb\n"), 0o644, system.Root)
		pe.avail = []string{"8.3", "8.4", "8.5", "8.6"}
		pe.from = map[string]string{"8.3": "ubuntu,sury", "8.4": "sury", "8.5": "sury", "8.6": "sury"}
		pe.installPHP("8.4")
		pe.installPHP("8.6")
		pl, err := pe.p.PrepareInstall(ctx, "8.5")
		if err != nil {
			t.Fatal(err)
		}
		r := repoOf(t, pl.Plan, RepoSury)
		if !strings.Contains(string(r.Pin), "Package: php-common php8.4* php8.5* php8.6*\n") || strings.Contains(string(r.Pin), "php8.3") {
			t.Errorf("ghim thiếu bản cũ hoặc ghim nhầm bản Ubuntu:\n%s", r.Pin)
		}
		// Bản của Ubuntu khi kho sury đã có: vẫn không đụng kho.
		pl2, err := pe.p.PrepareInstall(ctx, "8.3")
		if err != nil {
			t.Fatal(err)
		}
		if slices.ContainsFunc(pl2.Plan.Steps, func(s engine.Step) bool { _, ok := s.(*steps.AptRepo); return ok }) {
			t.Errorf("cài bản Ubuntu mà đụng kho sury: %q", stepIDs(pl2.Plan))
		}
	})

	t.Run("từ chối", func(t *testing.T) {
		pe := newPHPEnv(t)
		pe.m.SetFile(RepoSourcesPath(RepoSury), []byte("Types: deb\n"), 0o644, system.Root)
		if _, err := pe.p.PrepareInstall(ctx, "9.9"); refusedCode(t, err) != "php_version_unavailable" {
			t.Error("kho sury đã có mà không có bản: muốn php_version_unavailable")
		}

		pe2 := newPHPEnv(t)
		pe2.m.SetFile("/etc/os-release", []byte("ID=ubuntu\nVERSION_ID=\"24.04\"\n"), 0o644, system.Root)
		if _, err := pe2.p.PrepareInstall(ctx, "8.4"); refusedCode(t, err) != "precondition_failed" {
			t.Error("cần thêm kho mà không biết tên mã: muốn precondition_failed")
		}

		pe3 := &PHP{Sys: newSiteSim(t), Settings: EmptySettings{}, Sites: EmptySiteStore{}}
		if _, err := pe3.PrepareInstall(ctx, "8.4"); refusedCode(t, err) != "stack_not_installed" {
			t.Error("muốn stack_not_installed")
		}
	})

	t.Run("xem trước không đổi gì", func(t *testing.T) {
		pe := newPHPEnv(t)
		pl, err := pe.p.PrepareInstall(ctx, "8.4")
		if err != nil {
			t.Fatal(err)
		}
		before := pe.m.Snapshot()
		if _, err := pl.Preview(ctx); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, pe.m.Snapshot()) {
			t.Error("xem trước đổi hệ thống file")
		}
	})
}

const useDom = "shop.example.com"

// newUseEnv: site shop.example.com đã tạo trên PHP 8.3, PHP 8.4 đã cài.
func newUseEnv(t *testing.T) *phpEnv {
	t.Helper()
	pe := newPHPEnv(t)
	pe.add(t, useDom)
	pe.installPHP("8.4")
	return pe
}

func TestPHPUsePlanOrder(t *testing.T) {
	pe := newUseEnv(t)
	l := mustDomain(t, useDom)
	lay := site.LayoutOf(l)
	pl, err := pe.p.PrepareUse(ctx, l, "8.4")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"file:" + lay.PoolConf("8.4"),
		"file:" + lay.NginxConf,
		"symlink:" + lay.NginxLink,
		"php.use.check:" + useDom,
		"absent:" + lay.PoolConf("8.3"),
	}
	if got := stepIDs(pl.Plan); !reflect.DeepEqual(got, want) {
		t.Errorf("bước:\ncó    %q\nmuốn  %q", got, want)
	}
	if pl.Plan.Kind != "php.use" || pl.Plan.Target != useDom {
		t.Errorf("Kind/Target = %s/%s", pl.Plan.Kind, pl.Plan.Target)
	}
	// Vhost mới trỏ socket của 8.4.
	vh := pl.Plan.Steps[1].(*steps.File)
	if !strings.Contains(string(vh.Content), lay.SocketFor("8.4")) {
		t.Errorf("vhost mới không trỏ %s", lay.SocketFor("8.4"))
	}
}

func TestPHPUseExecute(t *testing.T) {
	pe := newUseEnv(t)
	d := mustDomain(t, useDom)
	lay := site.LayoutOf(d)
	pl, err := pe.p.PrepareUse(ctx, d, "8.4")
	if err != nil {
		t.Fatal(err)
	}
	rep, err := pl.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("Execute: %v %s %s%s", err, rep.Job.Status, rep.Job.Error, failedChecks(rep.Checks))
	}
	if st, _, _ := pe.sites.Site(ctx, d); st.PHPVersion != "8.4" {
		t.Errorf("bảng sites: PHPVersion = %q", st.PHPVersion)
	}
	if _, err := pe.m.Stat(lay.PoolConf("8.3")); err == nil {
		t.Error("pool ở 8.3 còn")
	}
	if _, err := pe.m.Stat(lay.SocketFor("8.4")); err != nil {
		t.Errorf("socket 8.4: %v", err)
	}
	if r := checkByID(rep.Checks, "php.socket:"+useDom); r == nil {
		t.Errorf("kiểm chứng thiếu socket của pool; có %+v", rep.Checks)
	}
	vh, _ := pe.m.ReadFile(lay.NginxConf)
	if !strings.Contains(string(vh), lay.SocketFor("8.4")) {
		t.Error("vhost trên máy không trỏ socket 8.4")
	}
	// Đã ở 8.4: php_same_version.
	if _, err := pe.p.PrepareUse(ctx, d, "8.4"); refusedCode(t, err) != "php_same_version" {
		t.Error("muốn php_same_version")
	}
}

// Site không trả 200 qua socket mới: job hoàn tác, vhost về socket cũ, pool
// mới bị gỡ, pool cũ còn, bảng sites không đổi.
func TestPHPUseRollsBackOnMidCheck(t *testing.T) {
	pe := newUseEnv(t)
	d := mustDomain(t, useDom)
	lay := site.LayoutOf(d)
	vhostBefore, _ := pe.m.ReadFile(lay.NginxConf)
	stBefore, _, _ := pe.sites.Site(ctx, d)
	calls := 0
	pe.m.Handle([]string{"curl", "-sS"}, func(c system.Cmd) system.Result {
		calls++
		return system.Result{Stdout: "<html>502 Bad Gateway</html>TPAN_HTTP_CODE=502"}
	})

	pl, err := pe.p.PrepareUse(ctx, d, "8.4")
	if err != nil {
		t.Fatal(err)
	}
	rep, err := pl.Execute(ctx, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if rep.Job.Status != engine.JobRolledBack {
		t.Fatalf("job %s; muốn rolled_back", rep.Job.Status)
	}
	if calls != midCheckTries {
		t.Errorf("phép kiểm giữa chừng chạy %d lần; muốn %d", calls, midCheckTries)
	}
	outcomes := map[string]string{}
	for _, s := range rep.Steps {
		outcomes[s.ID] = s.Outcome
	}
	if outcomes["php.use.check:"+useDom] != "failed" || outcomes["absent:"+lay.PoolConf("8.3")] != "not_run" {
		t.Errorf("outcome: %v", outcomes)
	}
	if vh, _ := pe.m.ReadFile(lay.NginxConf); string(vh) != string(vhostBefore) {
		t.Error("vhost không về như cũ (socket 8.3)")
	}
	if _, err := pe.m.Stat(lay.PoolConf("8.4")); err == nil {
		t.Error("pool mới ở 8.4 còn sau khi hoàn tác")
	}
	if _, err := pe.m.Stat(lay.PoolConf("8.3")); err != nil {
		t.Errorf("pool cũ ở 8.3 mất: %v", err)
	}
	if st, _, _ := pe.sites.Site(ctx, d); !reflect.DeepEqual(st, stBefore) {
		t.Errorf("bảng sites đổi: %+v → %+v", stBefore, st)
	}
	if !strings.Contains(rep.Job.Error, "502") {
		t.Errorf("lỗi job không nói site trả gì: %s", rep.Job.Error)
	}
}

func TestPHPUseRefusals(t *testing.T) {
	pe := newUseEnv(t)
	if _, err := pe.p.PrepareUse(ctx, mustDomain(t, "none.example.com"), "8.4"); refusedCode(t, err) != "site_not_found" {
		t.Error("muốn site_not_found")
	}
	if _, err := pe.p.PrepareUse(ctx, mustDomain(t, useDom), "8.3"); refusedCode(t, err) != "php_same_version" {
		t.Error("muốn php_same_version")
	}
	if _, err := pe.p.PrepareUse(ctx, mustDomain(t, useDom), "8.5"); refusedCode(t, err) != "php_not_installed" {
		t.Error("muốn php_not_installed")
	}
	// Thư mục còn mà file chạy không còn (đã gỡ gói): vẫn là chưa cài.
	pe.m.SetDir("/etc/php/8.2/fpm/pool.d", 0o755, system.Root)
	if _, err := pe.p.PrepareUse(ctx, mustDomain(t, useDom), "8.2"); refusedCode(t, err) != "php_not_installed" {
		t.Error("gói đã gỡ: muốn php_not_installed")
	}
	trashed := site.Site{Domain: mustDomain(t, "gone.example.com"), Status: site.Trashed, PHPVersion: "8.3"}
	if err := pe.sites.SaveSite(ctx, trashed); err != nil {
		t.Fatal(err)
	}
	if _, err := pe.p.PrepareUse(ctx, trashed.Domain, "8.4"); refusedCode(t, err) != "site_not_active" {
		t.Error("muốn site_not_active")
	}
}

func TestPHPRemove(t *testing.T) {
	pe := newUseEnv(t)
	pkgs := "php8.4-cli\tinstalled\nphp8.4-fpm\tinstalled\nphp8.4-xml\tconfig-files\n"
	pe.m.Handle([]string{"dpkg-query", "-W"}, func(c system.Cmd) system.Result {
		if c.Args[len(c.Args)-1] != "php8.4-*" || pkgs == "" {
			return system.Result{ExitCode: 1, Stderr: "dpkg-query: no packages found matching " + c.Args[len(c.Args)-1] + "\n"}
		}
		return system.Result{Stdout: pkgs}
	})

	if _, err := pe.p.PrepareRemove(ctx, "8.3"); refusedCode(t, err) != "php_default" {
		t.Error("muốn php_default")
	}
	d := mustDomain(t, useDom)
	st, _, _ := pe.sites.Site(ctx, d)
	st.PHPVersion = "8.4"
	if err := pe.sites.SaveSite(ctx, st); err != nil {
		t.Fatal(err)
	}
	_, err := pe.p.PrepareRemove(ctx, "8.4")
	if refusedCode(t, err) != "php_in_use" || !strings.Contains(err.Error(), useDom) {
		t.Errorf("muốn php_in_use nêu tên site: %v", err)
	}
	st.PHPVersion = "8.3"
	if err := pe.sites.SaveSite(ctx, st); err != nil {
		t.Fatal(err)
	}

	pl, err := pe.p.PrepareRemove(ctx, "8.4")
	if err != nil {
		t.Fatal(err)
	}
	if got := stepIDs(pl.Plan); !reflect.DeepEqual(got, []string{"apt.remove:php8.4-cli,php8.4-fpm"}) {
		t.Errorf("bước = %q (gói chỉ còn config-files không được gỡ lại)", got)
	}
	if info := pl.Plan.Steps[0].Info(); !info.Danger || info.NoUndo.IsZero() {
		t.Errorf("gỡ gói phải Danger và có NoUndo: %+v", info)
	}

	pkgs = ""
	if _, err := pe.p.PrepareRemove(ctx, "8.4"); refusedCode(t, err) != "php_not_installed" {
		t.Error("không gói nào: muốn php_not_installed")
	}
}

func TestMidCheckUnknownIsNotPass(t *testing.T) {
	midCheckWait = 0
	t.Cleanup(func() { midCheckWait = time.Second })
	for _, tc := range []struct {
		status checks.Status
		ok     bool
	}{{checks.Pass, true}, {checks.Warn, true}, {checks.Unknown, false}, {checks.Fail, false}} {
		s := &midCheck{id: "x", check: statusCheck(tc.status)}
		err := s.Apply(ctx, system.NewFake())
		if (err == nil) != tc.ok {
			t.Errorf("%s: err = %v", tc.status, err)
		}
		if done, _ := s.Check(ctx, nil); done != tc.ok {
			t.Errorf("%s: Check sau Apply = %v", tc.status, done)
		}
	}
}

type statusCheck checks.Status

func (s statusCheck) Run(context.Context, system.System) checks.Result {
	return checks.Result{ID: "test", Status: checks.Status(s)}
}
