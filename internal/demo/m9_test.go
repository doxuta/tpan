package demo

import (
	"context"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

// m9Backups dựng ca sử dụng backup và cron trên máy demo như server dựng.
func m9Backups(sys system.System, st *store.Store) (*core.Backups, *core.Cron) {
	b := &core.Backups{Sites: NewSites(sys, st), Store: st}
	return b, &core.Cron{Sites: b.Sites, Runs: st, Backups: b}
}

// Màn Backup và màn Cron có dữ liệu ngay khi mở demo.
func TestM9Seed(t *testing.T) {
	ctx, sys, st := newDemo(t)
	b, c := m9Backups(sys, st)

	set, err := b.Settings(ctx)
	if err != nil || set != (core.BackupSettings{Keep: core.DefaultBackupKeep, At: demoBackupAt}) {
		t.Fatalf("Settings = %+v, %v", set, err)
	}
	all, err := b.List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	count := map[string]int{}
	purged := false
	for i, r := range all {
		count[r.Domain+" "+string(r.Kind)]++
		if i > 0 && r.Created.After(all[i-1].Created) {
			t.Errorf("không sắp mới nhất trước: %v sau %v", r.Created, all[i-1].Created)
		}
		if r.Domain == sitePurged && r.Reason == core.ReasonBeforePurge {
			purged = true
		}
		// File có trên máy giả và sha256 khớp dòng (kiểm của khôi phục, nhập DB).
		res, err := sys.Run(ctx, system.Cmd{Name: "sha256sum", Args: []string{"--", r.Path}})
		if err != nil || !strings.HasPrefix(res.Stdout, r.SHA256+" ") {
			t.Errorf("sha256 của %s = %q, %v; dòng ghi %s", r.Path, res.Stdout, err, r.SHA256)
		}
	}
	if count[siteBlog+" full"] != 3 || count[siteBlog+" sql"] != 1 || count[siteShop+" full"] != 1 || count[siteShop+" sql"] != 1 || !purged {
		t.Errorf("backup mẫu = %v, có bản trước xoá hẳn của %s: %v", count, sitePurged, purged)
	}

	ov, err := c.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ov.BackupFile != site.BackupCronFile {
		t.Errorf("BackupFile = %q", ov.BackupFile)
	}
	by := map[string]core.CronEntry{}
	for _, e := range ov.Sites {
		by[e.Domain] = e
	}
	for _, d := range []string{siteBlog, siteShop, siteWiki} {
		if e := by[d]; !e.Enabled || e.Last == nil {
			t.Errorf("cron của %s = %+v", d, e)
		}
	}
	if w := by[siteWiki].Last; w == nil || w.Status != core.CronFail || !strings.Contains(w.Output, "PHP Fatal error") {
		t.Errorf("lần chạy lỗi của %s = %+v", siteWiki, w)
	}
	if o, ok := by[siteOld]; !ok || o.Enabled || o.Last != nil {
		t.Errorf("site trong thùng rác = %+v (có trong danh sách: %v)", o, ok)
	}
}

// Mọi nút "xem trước" của M9 chạy được trên máy giả.
func TestM9DryRunsOnDemo(t *testing.T) {
	ctx, sys, st := newDemo(t)
	b, _ := m9Backups(sys, st)
	blog, shop, old := domain(t, siteBlog), domain(t, siteShop), domain(t, siteOld)
	sqlID := ""
	rows, _ := b.List(ctx, siteBlog)
	for _, r := range rows {
		if r.Kind == core.BackupSQL {
			sqlID = r.ID
		}
	}
	for _, c := range []struct {
		name    string
		kind    string
		prepare func() (*core.Planned, error)
	}{
		{"backup blog", "backup.create", func() (*core.Planned, error) { return b.PrepareCreate(ctx, blog, core.ReasonManual) }},
		{"backup site trong thùng rác", "backup.create", func() (*core.Planned, error) { return b.PrepareCreate(ctx, old, core.ReasonManual) }},
		{"khôi phục blog", "backup.restore", func() (*core.Planned, error) { return b.PrepareRestore(ctx, blog, "") }},
		{"cài đặt", "backup.settings", func() (*core.Planned, error) { return b.PrepareSettings(ctx, 5, "03:00") }},
		{"đổi mật khẩu", "db.passwd", func() (*core.Planned, error) { return b.PrepareDBPassword(ctx, shop) }},
		{"xuất DB", "db.dump", func() (*core.Planned, error) { return b.PrepareDBDump(ctx, shop) }},
		{"nhập DB", "db.import", func() (*core.Planned, error) { return b.PrepareDBImport(ctx, blog, sqlID) }},
	} {
		p, err := c.prepare()
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		pv, err := p.Preview(ctx)
		if err != nil || pv.Kind != c.kind || len(pv.Steps) == 0 {
			t.Errorf("%s: Preview = %+v, %v", c.name, pv, err)
		}
	}
}

// m9Run chạy một Plan và đòi job thành công.
func m9Run(t *testing.T, ctx context.Context, name string, p *core.Planned, err error) engine.Report {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	rep, err := p.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("%s: %v, job %s: %s %+v", name, err, rep.Job.Status, rep.Job.Error, rep.Steps)
	}
	return rep
}

// Bấm thật trên giao diện demo: khôi phục, backup, đổi mật khẩu, xuất rồi
// nhập DB đều xong và site vẫn đạt. Mỗi kịch bản một máy demo: hai bản
// backup của cùng site trong cùng một giây trùng tên file.
func TestM9RunOnDemo(t *testing.T) {
	t.Run("khôi phục", func(t *testing.T) {
		ctx, sys, st := newDemo(t)
		b, _ := m9Backups(sys, st)
		d := domain(t, siteBlog)
		p, err := b.PrepareRestore(ctx, d, "")
		m9Run(t, ctx, "khôi phục", p, err)
		h, err := b.Sites.Health(ctx, d)
		if err != nil {
			t.Fatal(err)
		}
		if bad := notPass(h.Results); len(bad) > 0 {
			t.Errorf("site vừa khôi phục phải đạt mọi phép kiểm, có: %v", bad)
		}
		rows, _ := b.List(ctx, siteBlog)
		if len(rows) == 0 || rows[0].Reason != core.ReasonBeforeRestore {
			t.Errorf("thiếu bản tự backup trước khi khôi phục: %+v", rows)
		}
		l, _ := layoutOf(siteBlog)
		if _, err := sys.Stat(site.RestoreDir(l)); err == nil {
			t.Error("thư mục khôi phục còn sót")
		}
	})
	t.Run("backup", func(t *testing.T) {
		ctx, sys, st := newDemo(t)
		b, _ := m9Backups(sys, st)
		p, err := b.PrepareCreate(ctx, domain(t, siteShop), core.ReasonManual)
		m9Run(t, ctx, "backup", p, err)
		rows, _ := b.List(ctx, siteShop)
		if len(rows) == 0 || rows[0].Kind != core.BackupFull || rows[0].WPVersion != demoWPVersion {
			t.Errorf("bản vừa tạo = %+v", rows)
		}
	})
	t.Run("đổi mật khẩu", func(t *testing.T) {
		ctx, sys, st := newDemo(t)
		b, _ := m9Backups(sys, st)
		l, _ := layoutOf(siteShop)
		before, _ := sys.ReadFile(l.WPConfig)
		p, err := b.PrepareDBPassword(ctx, l.Domain)
		m9Run(t, ctx, "đổi mật khẩu", p, err)
		after, _ := sys.ReadFile(l.WPConfig)
		if string(before) == string(after) || !wpDefinePW.Match(after) {
			t.Errorf("wp-config.php không đổi mật khẩu:\n%s", after)
		}
	})
	t.Run("xuất rồi nhập", func(t *testing.T) {
		ctx, sys, st := newDemo(t)
		b, _ := m9Backups(sys, st)
		d := domain(t, siteShop)
		p, err := b.PrepareDBDump(ctx, d)
		m9Run(t, ctx, "xuất", p, err)
		rows, _ := b.List(ctx, siteShop)
		if len(rows) == 0 || rows[0].Kind != core.BackupSQL {
			t.Fatalf("bản xuất vừa tạo = %+v", rows)
		}
		// Nhập bản cũ có sẵn (bản vừa xuất cùng giây với bản tự xuất trước khi nhập).
		p, err = b.PrepareDBImport(ctx, d, rows[len(rows)-1].ID)
		m9Run(t, ctx, "nhập", p, err)
	})
}
