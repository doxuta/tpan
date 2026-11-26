package core

import (
	"slices"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

// rmrf: thư mục bị xoá ngoài tpan (rm -rf). Máy giả không có RemoveAll: đổi
// tên cả cây sang chỗ khác cho cùng kết quả ở đường dẫn cũ.
func (se *siteEnv) rmrf(t *testing.T, p string) {
	t.Helper()
	if err := se.m.MkdirAll("/tmp", 0o777, system.Root); err != nil {
		t.Fatal(err)
	}
	if err := se.m.Rename(p, "/tmp/da-xoa-tay-"+strings.ReplaceAll(strings.Trim(p, "/"), "/", "-")); err != nil {
		t.Fatalf("xoá %s: %v", p, err)
	}
	if se.exists(p) {
		t.Fatalf("%s vẫn còn sau khi xoá tay", p)
	}
}

// Thư mục của site bị xoá ngoài tpan (rm -rf /var/www/<site>). Trước M34 bước
// chuyển thư mục dừng với "không còn gì để chuyển": site không vào thùng rác
// được, nên không xoá hẳn được, và chủ của nó không xoá được. Giờ: vào thùng
// rác (không có bước chuyển thư mục, tiêu đề nói rõ), khôi phục bị từ chối kèm
// đường đi, xoá hẳn chạy được và nói rõ không có bản tự backup.
func TestSiteMatThuMucVanXoaDuoc(t *testing.T) {
	const dom = "shop.example.com"
	for _, web := range []WebServer{Nginx, Apache} {
		t.Run(string(web), func(t *testing.T) {
			se := newSiteEnv(t, web)
			se.fixedNow(trashNow)
			se.add(t, "blog.example.com")
			se.add(t, dom)
			d := mustDomain(t, dom)
			l := site.LayoutOf(d)
			se.rmrf(t, l.Dir)

			r, err := se.s.PrepareRemove(ctx, d)
			if err != nil {
				t.Fatalf("PrepareRemove khi thư mục đã mất: %v", err)
			}
			ids := stepIDs(r.Plan)
			if slices.ContainsFunc(ids, func(id string) bool { return strings.HasPrefix(id, "dir.move:") }) {
				t.Errorf("còn bước chuyển thư mục dù không có gì để chuyển: %v", ids)
			}
			for _, want := range []string{"db.lock:" + dom, "user.lock:" + dom} {
				if !slices.Contains(ids, want) {
					t.Errorf("thiếu bước %s: %v", want, ids)
				}
			}
			if r.Plan.Kind != "site.rm" || !strings.Contains(r.Plan.Title.Vi, "đã bị xoá ngoài tpan") || !strings.Contains(r.Plan.Title.Vi, "không khôi phục lại được") || r.Plan.Title.En == "" {
				t.Errorf("tiêu đề phải nói rõ hậu quả: %q", r.Plan.Title.Vi)
			}
			if _, err := r.Preview(ctx); err != nil {
				t.Fatalf("Preview: %v", err)
			}
			rep, err := r.Execute(ctx, nil)
			if err != nil || rep.Job.Status != engine.JobSucceeded {
				t.Fatalf("đưa site mất thư mục vào thùng rác: %v %s (%s)", err, rep.Job.Status, rep.Job.Error)
			}
			if st := se.siteOf(t, dom); st.Status != site.Trashed {
				t.Fatalf("dòng sau khi vào thùng rác = %+v", st)
			}
			if _, link := vhostPaths(web, l); se.exists(link) {
				t.Error("link vhost còn sau khi vào thùng rác")
			}

			// Khôi phục: không có mã nguồn để đưa về.
			_, err = se.s.PrepareRestore(ctx, d)
			if refusedCode(t, err) != CodeSiteDirMissing || !strings.Contains(err.Error(), "tpan trash purge "+dom) {
				t.Errorf("khôi phục site mất thư mục: %v", err)
			}

			// Xoá hẳn: chạy được, không có bước backup, tiêu đề nói rõ.
			p, err := se.s.PreparePurge(ctx, d)
			if err != nil {
				t.Fatalf("PreparePurge: %v", err)
			}
			pids := stepIDs(p.Plan)
			if slices.ContainsFunc(pids, func(id string) bool { return strings.HasPrefix(id, "backup.create:") }) {
				t.Errorf("xoá hẳn site mất thư mục mà có bước backup: %v", pids)
			}
			if !strings.Contains(p.Plan.Title.Vi, "KHÔNG có bản tự backup") {
				t.Errorf("tiêu đề xoá hẳn phải nói không có bản tự backup: %q", p.Plan.Title.Vi)
			}
			prep, err := p.Execute(ctx, nil)
			if err != nil || prep.Job.Status != engine.JobSucceeded {
				t.Fatalf("xoá hẳn: %v %s (%s)", err, prep.Job.Status, prep.Job.Error)
			}
			if _, ok, _ := se.sites.Site(ctx, d); ok {
				t.Error("xoá hẳn xong mà Store còn site")
			}
			if _, found := se.m.users[l.User]; found || se.m.dbs[l.DB] || se.m.dbUsers[l.DB] {
				t.Error("còn user Linux hoặc DB của site đã xoá hẳn")
			}
			if _, ok, _ := se.sites.Site(ctx, mustDomain(t, "blog.example.com")); !ok {
				t.Error("xoá nhầm site khác")
			}
		})
	}
}

// Site đang tạm khoá mà thư mục đã mất: vẫn vào thùng rác được (mở khoá trước
// không làm nổi vì kiểm chứng đòi site trả 200). Thư mục còn thì luật cũ giữ
// nguyên: phải mở khoá trước.
func TestSiteMatThuMucDangTamKhoa(t *testing.T) {
	const dom = "shop.example.com"
	se := newSiteEnv(t, Nginx)
	se.add(t, dom)
	d := mustDomain(t, dom)
	st := se.siteOf(t, dom)
	st.Suspended = account.SuspendSelf
	if err := se.sites.SaveSite(ctx, st); err != nil {
		t.Fatal(err)
	}
	if _, err := se.s.PrepareRemove(ctx, d); refusedCode(t, err) != CodeSiteSuspended {
		t.Fatalf("site tạm khoá, thư mục còn: %v, muốn site_suspended", err)
	}
	se.rmrf(t, st.Layout().Dir)
	r, err := se.s.PrepareRemove(ctx, d)
	if err != nil {
		t.Fatalf("site tạm khoá, thư mục đã mất: %v", err)
	}
	if rep, err := r.Execute(ctx, nil); err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("chạy: %v %s (%s)", err, rep.Job.Status, rep.Job.Error)
	}
}

// Thư mục đang nằm trong thùng rác mà dòng chưa ghi trashed (lần "đưa vào thùng
// rác" trước chạy dở): không phải "đã mất", Plan vẫn có bước chuyển thư mục
// (bước đó tự thấy đã chuyển) và tiêu đề như thường.
func TestSiteThuMucDaOThungRacKhongPhaiMat(t *testing.T) {
	const dom = "shop.example.com"
	se := newSiteEnv(t, Nginx)
	se.add(t, dom)
	d := mustDomain(t, dom)
	l := site.LayoutOf(d)
	if err := se.m.MkdirAll(site.TrashRoot, 0o700, system.Root); err != nil {
		t.Fatal(err)
	}
	if err := se.m.Rename(l.Dir, site.TrashDir(l)); err != nil {
		t.Fatal(err)
	}
	r, err := se.s.PrepareRemove(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(stepIDs(r.Plan), "dir.move:"+l.Dir) || strings.Contains(r.Plan.Title.Vi, "xoá ngoài tpan") {
		t.Errorf("thư mục đã ở thùng rác: bước %v, tiêu đề %q", stepIDs(r.Plan), r.Plan.Title.Vi)
	}
}

// Site vào thùng rác bình thường, rồi thư mục trong thùng rác bị xoá tay: khôi
// phục từ chối kèm đường đi; xoá hẳn vẫn chạy.
func TestSiteTrongThungRacMatThuMuc(t *testing.T) {
	const dom = "shop.example.com"
	se := newSiteEnv(t, Nginx)
	se.add(t, dom)
	se.trash(t, dom)
	d := mustDomain(t, dom)
	l := site.LayoutOf(d)
	se.rmrf(t, site.TrashDir(l))
	if _, err := se.s.PrepareRestore(ctx, d); refusedCode(t, err) != CodeSiteDirMissing {
		t.Fatalf("khôi phục khi thùng rác mất thư mục: %v", err)
	}
	p, err := se.s.PreparePurge(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if rep, err := p.Execute(ctx, nil); err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("xoá hẳn: %v %s (%s)", err, rep.Job.Status, rep.Job.Error)
	}
}
