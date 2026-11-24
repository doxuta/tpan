package core

import (
	"context"
	"errors"
	"io/fs"
	"reflect"
	"slices"
	"testing"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

const (
	panelDom      = "panel.example.com"
	panelRenewal  = "/etc/letsencrypt/renewal/" + PanelLineage + ".conf"
	panelChain    = PanelCertDir + "/fullchain.pem"
	panelTestACME = "https://127.0.0.1:14000/dir"
)

// fakeSessions ghi các host bị xoá phiên.
type fakeSessions struct{ hosts []string }

func (f *fakeSessions) DeleteSessionsByHost(_ context.Context, host string) error {
	f.hosts = append(f.hosts, host)
	return nil
}

// panelEnv là sslEnv cộng certbot của lineage tpan-panel (certonly ghi vào
// live/tpan-panel, delete xoá nó) và một Panel đã đủ điều kiện đầu: stack
// nginx, có email, ssl.server là máy chủ ACME thử (không staging).
type panelEnv struct {
	*sslEnv
	p        *Panel
	sess     *fakeSessions
	deleted  int
	certonly int
}

func newPanelEnv(t *testing.T) *panelEnv {
	t.Helper()
	pe := &panelEnv{sslEnv: newSSLEnv(t, Nginx), sess: &fakeSessions{}}
	pe.setEmail(t)
	mustSet(t, pe.st, SettingSSLServer, panelTestACME)
	pe.p = &Panel{Sys: pe.m, Settings: pe.st, Store: pe.sites, Sessions: pe.sess, Journal: pe.st}
	m := pe.m
	// Đăng ký sau handler của sslEnv, cùng độ dài prefix: handler này thắng.
	m.Handle([]string{"certbot", "certonly"}, func(c system.Cmd) system.Result {
		if argAfter(c.Args, "--cert-name") != PanelLineage {
			t.Errorf("certbot không xin vào lineage %s: %v", PanelLineage, c.Args)
		}
		m.SetFile(panelChain, selfSigned(t, argAfter(c.Args, "-d")), 0o644, system.Root)
		m.SetFile(panelRenewal, []byte("server = "+argAfter(c.Args, "--server")+"\n"), 0o644, system.Root)
		pe.certonly++
		return system.Result{}
	})
	m.Handle([]string{"certbot", "delete"}, func(system.Cmd) system.Result {
		m.removeUnder(PanelCertDir, true)
		_ = m.Remove(panelRenewal)
		pe.deleted++
		return system.Result{}
	})
	return pe
}

func (pe *panelEnv) prepare(t *testing.T, dom string) *Planned {
	t.Helper()
	pl, err := pe.p.PrepareDomain(ctx, mustDomain(t, dom))
	if err != nil {
		t.Fatalf("PrepareDomain %s: %v", dom, err)
	}
	return pl
}

func (pe *panelEnv) run(t *testing.T, pl *Planned) engine.Report {
	t.Helper()
	// Kiểm chứng gọi openssl, certbot thật; TestPanelPlanOrder xem danh sách.
	pl.Plan.Verify = nil
	rep, err := pl.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("%v %s %s; bước %+v", err, rep.Job.Status, rep.Job.Error, rep.Steps)
	}
	return rep
}

func (pe *panelEnv) setting(t *testing.T) string {
	t.Helper()
	v, _, err := pe.st.GetSetting(ctx, SettingPanelDomain)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestPanelPrepareRefusals(t *testing.T) {
	d := func(t *testing.T) validate.Domain { return mustDomain(t, panelDom) }
	cases := []struct {
		name  string
		setup func(t *testing.T, pe *panelEnv)
		dom   func(t *testing.T) validate.Domain
		code  string
	}{
		{"domain rỗng", nil, func(*testing.T) validate.Domain { return validate.Domain{} }, validate.CodeDomainInvalid},
		{"trùng site trong thùng rác", func(t *testing.T, pe *panelEnv) {
			_ = pe.sites.SaveSite(ctx, site.Site{Domain: d(t), Status: site.Trashed, PHPVersion: "8.3"})
		}, d, "panel_domain_is_site"},
		{"stack chưa cài", func(t *testing.T, pe *panelEnv) {
			mustSet(t, pe.st, SettingWeb, "")
		}, d, "stack_not_installed"},
		{"chưa có email", func(t *testing.T, pe *panelEnv) {
			mustSet(t, pe.st, SettingSSLEmail, "")
		}, d, "acme_email_missing"},
		{"staging mặc định", func(t *testing.T, pe *panelEnv) {
			mustSet(t, pe.st, SettingSSLServer, "")
		}, d, "panel_staging"},
		{"chế độ riêng tư", func(t *testing.T, pe *panelEnv) {
			pe.m.SetFile(panelUnitPath, []byte("[Service]\nExecStart=/usr/local/bin/tpan serve --listen 127.0.0.1:8443 --tls-cert x --tls-key y\n"), 0o644, system.Root)
		}, d, "panel_private"},
		{"không có bản ghi A", func(t *testing.T, pe *panelEnv) { pe.a4 = "" }, d, "precondition_failed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pe := newPanelEnv(t)
			if c.setup != nil {
				c.setup(t, pe)
			}
			_, err := pe.p.PrepareDomain(ctx, c.dom(t))
			if got := refusedCode(t, err); got != c.code {
				t.Errorf("mã = %q (%v), muốn %q", got, err, c.code)
			}
		})
	}

	// Staging tắt (không có ssl.server) thì qua: Let's Encrypt thật.
	pe := newPanelEnv(t)
	mustSet(t, pe.st, SettingSSLServer, "")
	mustSet(t, pe.st, SettingSSLStaging, "0")
	if _, err := pe.p.PrepareDomain(ctx, d(t)); err != nil {
		t.Errorf("staging tắt mà bị từ chối: %v", err)
	}
}

func TestPanelListen(t *testing.T) {
	for _, c := range []struct {
		unit    string // "" là không có file
		port    string
		private bool
		bad     bool
	}{
		{"", "8443", false, false},
		{"ExecStart=/usr/local/bin/tpan serve --listen 0.0.0.0:8443 --tls-cert a\n", "8443", false, false},
		{"ExecStart=/usr/local/bin/tpan serve --listen 0.0.0.0:9443\n", "9443", false, false},
		{"ExecStart=/usr/local/bin/tpan serve --listen=127.0.0.1:8443\n", "8443", true, false},
		{"ExecStart=/usr/local/bin/tpan serve\n", "8443", false, false},
		{"ExecStart=/usr/local/bin/tpan serve --listen rac\n", "", false, true},
	} {
		m := system.NewFake()
		if c.unit != "" {
			m.SetFile(panelUnitPath, []byte("[Service]\n"+c.unit), 0o644, system.Root)
		}
		port, private, err := panelListen(m)
		if (err != nil) != c.bad || port != c.port || private != c.private {
			t.Errorf("%q: %q %v %v", c.unit, port, private, err)
		}
	}
}

func TestPanelPlanOrder(t *testing.T) {
	pe := newPanelEnv(t)
	pl := pe.prepare(t, panelDom)
	want := []string{"panel.namefree:" + panelDom, "dir:" + acmeRoot + "/.well-known/acme-challenge",
		"ssl.probe:" + panelDom, "ssl.cert:" + panelDom, "setting:" + SettingPanelDomain}
	if got := stepIDs(pl.Plan); !reflect.DeepEqual(got, want) {
		t.Errorf("thứ tự bước:\n got %v\nwant %v", got, want)
	}
	if pl.Plan.Kind != "panel.domain" || pl.Plan.Target != panelDom {
		t.Errorf("Plan = %s/%s", pl.Plan.Kind, pl.Plan.Target)
	}
	var ids []string
	// systemctl is-active tpan không được giả: PanelServes ra Unknown ngay, không chờ.
	for _, r := range checks.RunAll(ctx, pe.m, pl.Plan.Verify) {
		ids = append(ids, r.ID)
	}
	wantV := []string{"tpan.panelcert:" + panelDom, "service.active:certbot.timer", "tls.renew:" + PanelLineage, "tpan.panelserves:" + panelDom}
	if !reflect.DeepEqual(ids, wantV) {
		t.Errorf("kiểm chứng = %v, muốn %v", ids, wantV)
	}

	// Lineage đang mang tên khác: bước xoá nằm SAU bước thử, trước bước xin.
	pe.m.SetFile(panelChain, selfSigned(t, "old.example.com"), 0o644, system.Root)
	pe.m.SetFile(panelRenewal, []byte("server = "+panelTestACME+"\n"), 0o644, system.Root)
	got := stepIDs(pe.prepare(t, panelDom).Plan)
	want = slices.Insert(slices.Clone(want), 3, "ssl.delete:"+PanelLineage)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("đổi tên:\n got %v\nwant %v", got, want)
	}
	// Lineage đã đúng tên: không có bước xoá.
	pe.m.SetFile(panelChain, selfSigned(t, panelDom), 0o644, system.Root)
	if slices.Contains(stepIDs(pe.prepare(t, panelDom).Plan), "ssl.delete:"+PanelLineage) {
		t.Error("lineage đúng tên mà vẫn xoá")
	}
}

func TestPanelExecuteRerunAndRename(t *testing.T) {
	pe := newPanelEnv(t)
	pe.run(t, pe.prepare(t, panelDom))
	if v := pe.setting(t); v != panelDom {
		t.Errorf("setting = %q", v)
	}
	if pe.certonly != 1 || pe.deleted != 0 || len(pe.sess.hosts) != 0 {
		t.Errorf("certonly %d, delete %d, xoá phiên %v", pe.certonly, pe.deleted, pe.sess.hosts)
	}
	if d, ok, err := pe.p.Domain(ctx); err != nil || !ok || d.String() != panelDom {
		t.Errorf("Domain = %v %v %v", d, ok, err)
	}

	// Chạy lại cùng tên: mọi bước được bỏ qua, không lệnh nào đổi máy.
	n := len(pe.m.Ran())
	rep := pe.run(t, pe.prepare(t, panelDom))
	for _, s := range rep.Steps {
		if s.Outcome != "skipped" {
			t.Errorf("chạy lại mà bước %s: %s", s.ID, s.Outcome)
		}
	}
	for _, c := range pe.m.Ran()[n:] {
		if c.Name == "certbot" || c.Name == "curl" {
			t.Errorf("chạy lại gọi %s", c.String())
		}
	}

	// Đổi tên: xoá lineage cũ, xin mới, phiên của tên cũ bị xoá.
	const next = "admin.example.com"
	pe.run(t, pe.prepare(t, next))
	if v := pe.setting(t); v != next {
		t.Errorf("setting = %q", v)
	}
	if pe.deleted != 1 || pe.certonly != 2 || !reflect.DeepEqual(pe.sess.hosts, []string{panelDom}) {
		t.Errorf("certonly %d, delete %d, xoá phiên %v", pe.certonly, pe.deleted, pe.sess.hosts)
	}
}

func TestPanelSettingUndoOnVerifyFail(t *testing.T) {
	pe := newPanelEnv(t)
	pl := pe.prepare(t, panelDom)
	// Kiểm chứng của job lỗi (chứng chỉ không có file): setting về như cũ.
	pl.Plan.Verify = []checks.Check{checks.PanelACMECert(mustDomain(t, "khac.example.com"), nil)}
	rep, err := pl.Execute(ctx, nil)
	if err != nil || rep.Job.Status == engine.JobSucceeded {
		t.Fatalf("muốn job hỏng: %v %s", err, rep.Job.Status)
	}
	if v := pe.setting(t); v != "" {
		t.Errorf("job hỏng mà setting = %q", v)
	}
	if len(pe.sess.hosts) != 0 {
		t.Errorf("job hỏng mà xoá phiên %v", pe.sess.hosts)
	}
}

func TestPanelNameFreeUnderLock(t *testing.T) {
	pe := newPanelEnv(t)
	d := mustDomain(t, panelDom)
	step := &panelNameFree{store: pe.sites, d: d}
	if ok, err := step.Check(ctx, pe.m); !ok || err != nil {
		t.Fatalf("máy sạch: %v %v", ok, err)
	}
	l := site.LayoutOf(d)
	for _, p := range []string{l.Dir, l.ApacheConf} {
		m := system.NewFake()
		m.SetDir("/var/www", 0o755, system.Root)
		if p == l.Dir {
			m.SetDir(p, 0o755, system.Root)
		} else {
			m.SetFile(p, []byte("x"), 0o644, system.Root)
		}
		ok, err := step.Check(ctx, m)
		if ok || refusedCode(t, err) != "panel_domain_is_site" {
			t.Errorf("%s có mà Check = %v %v", p, ok, err)
		}
	}

	// Site được tạo chen vào giữa điều kiện đầu và job: job dừng ở bước đầu.
	pl := pe.prepare(t, panelDom)
	pe.m.SetDir(l.Dir, 0o755, system.Root)
	rep, err := pl.Execute(ctx, nil)
	if err != nil || rep.Job.Status == engine.JobSucceeded || pe.certonly != 0 || pe.setting(t) != "" {
		t.Errorf("job = %v %s, certonly %d, setting %q", err, rep.Job.Status, pe.certonly, pe.setting(t))
	}
}

func TestPanelClear(t *testing.T) {
	pe := newPanelEnv(t)
	if _, err := pe.p.PrepareClear(ctx); refusedCode(t, err) != "panel_domain_not_set" {
		t.Errorf("chưa đặt: %v", err)
	}
	pe.run(t, pe.prepare(t, panelDom))
	pl, err := pe.p.PrepareClear(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Setting trước (panel thôi phục vụ chứng chỉ ACME ngay), rồi xoá lineage.
	if got, want := stepIDs(pl.Plan), []string{"setting:" + SettingPanelDomain, "ssl.delete:" + PanelLineage}; !reflect.DeepEqual(got, want) {
		t.Errorf("bước = %v, muốn %v", got, want)
	}
	if pl.Plan.Kind != "panel.domain.clear" || pl.Plan.Target != panelDom {
		t.Errorf("Plan = %s/%s", pl.Plan.Kind, pl.Plan.Target)
	}
	pe.run(t, pl)
	if _, ok, _ := pe.p.Domain(ctx); ok || pe.deleted != 1 || !reflect.DeepEqual(pe.sess.hosts, []string{panelDom}) {
		t.Errorf("còn tên %v, delete %d, xoá phiên %v", ok, pe.deleted, pe.sess.hosts)
	}
	if _, err := pe.m.Stat(PanelCertDir); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("lineage còn: %v", err)
	}
}

func TestRefusePanelDomain(t *testing.T) {
	pe := newPanelEnv(t)
	d := mustDomain(t, panelDom)
	if err := RefusePanelDomain(ctx, pe.st, d); err != nil {
		t.Errorf("chưa đặt tên: %v", err)
	}
	mustSet(t, pe.st, SettingPanelDomain, panelDom)
	if refusedCode(t, RefusePanelDomain(ctx, pe.st, d)) != "domain_is_panel" {
		t.Error("muốn domain_is_panel")
	}
	if err := RefusePanelDomain(ctx, pe.st, mustDomain(t, "shop.example.com")); err != nil {
		t.Errorf("tên khác: %v", err)
	}
	mustSet(t, pe.st, SettingPanelDomain, "không phải tên miền")
	if err := RefusePanelDomain(ctx, pe.st, d); err == nil {
		t.Error("setting hỏng mà không lỗi")
	}
}

func TestSiteAddDuoiKhoaTuChoiTenPanel(t *testing.T) {
	se := newSiteEnv(t, Nginx)
	mustSet(t, se.st, SettingPanelDomain, panelDom)
	if _, err := se.s.PrepareAdd(ctx, se.input(t, panelDom)); refusedCode(t, err) != "domain_is_panel" {
		t.Errorf("điều kiện đầu: %v", err)
	}

	// Điều kiện đầu qua lúc tên chưa đặt; tên panel được đặt trước khi job
	// lấy khoá: bước đầu (dưới khoá) dừng job, máy và bảng sites không đổi.
	mustSet(t, se.st, SettingPanelDomain, "")
	a, err := se.s.PrepareAdd(ctx, se.input(t, panelDom))
	if err != nil {
		t.Fatal(err)
	}
	mustSet(t, se.st, SettingPanelDomain, panelDom)
	before := se.m.Snapshot()
	res, err := a.Execute(ctx, nil)
	if err != nil || res.Report.Job.Status == engine.JobSucceeded {
		t.Fatalf("muốn job hỏng: %v %s", err, res.Report.Job.Status)
	}
	if !reflect.DeepEqual(before, se.m.Snapshot()) || se.sites.count() != 0 {
		t.Error("job bị từ chối mà máy hoặc bảng sites đổi")
	}
}
