package core

import (
	"errors"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
)

// M34, phần DB đi kèm một site (docs/thay-doi/M34.md). Ba máy giả:
//   - dbEnv (dbops_test.go) cho đổi mật khẩu, xuất, nhập: câu "DB và user DB có
//     không" trả theo de.state;
//   - bktEnv (backup_test.go) cho khôi phục từ bản backup đầy đủ: MariaDB giả có
//     trạng thái của siteSim;
//   - dbsEnv (dbs_test.go) cho tạm khoá, mở khoá, đổi charset.

// gone đặt câu trả lời của MariaDB giả: DB của site có không, user DB có không.
func (de *dbEnv) gone(db, user bool) {
	b := func(missing bool) string {
		if missing {
			return "0"
		}
		return "1"
	}
	de.mu.Lock()
	defer de.mu.Unlock()
	de.state = b(db) + "\t" + b(user) + "\n"
}

// mutate sửa dòng của site dbDom trong bảng sites.
func (de *dbEnv) mutate(t *testing.T, fn func(*site.Site)) {
	t.Helper()
	st := de.siteOf(t, dbDom)
	fn(&st)
	if err := de.sites.SaveSite(ctx, st); err != nil {
		t.Fatal(err)
	}
}

var m34CreateUser = regexp.MustCompile(`CREATE USER '([a-z0-9_]+)'@'localhost' IDENTIFIED BY '([A-Za-z0-9]+)'`)

// User DB của một site WordPress đã bị xoá ngoài tpan: "Đổi mật khẩu DB" tạo
// lại user với đúng mật khẩu trong wp-config.php. Không file nào bị sửa, mật
// khẩu không đổi, và site được kiểm là chạy lại.
func TestSiteDBUserMatThiDoiMatKhauLaTaoLai(t *testing.T) {
	de := newDBEnv(t, false)
	d := mustDomain(t, dbDom)
	de.gone(false, true)

	sp, err := de.b.PrepareSiteDBPassword(ctx, d)
	if err != nil {
		t.Fatalf("PrepareSiteDBPassword: %v", err)
	}
	p := sp.Planned
	if p.Plan.Kind != "db.passwd" || p.Plan.Target != dbDom {
		t.Errorf("Plan = %s/%s", p.Plan.Kind, p.Plan.Target)
	}
	if got, want := stepIDs(p.Plan), []string{"mariadb.site-user-restore:" + dbDom}; !reflect.DeepEqual(got, want) {
		t.Errorf("bước = %v, muốn %v", got, want)
	}
	if sp.Password.Reveal() != "" {
		t.Error("site WordPress: không có mật khẩu mới nào để hiện, mà Plan trả một mật khẩu")
	}
	if !strings.Contains(p.Plan.Title.Vi, "wp-config.php") || !strings.Contains(p.Plan.Title.Vi, "mật khẩu không đổi") {
		t.Errorf("tiêu đề không nói mật khẩu lấy từ wp-config.php và không đổi: %s", p.Plan.Title.Vi)
	}
	if len(p.Plan.Verify) != 2 {
		t.Errorf("kiểm chứng có %d phép kiểm, muốn 2 (DB đăng nhập được, site trả 200)", len(p.Plan.Verify))
	}

	de.m.ResetRan()
	if _, err := p.Preview(ctx); err != nil {
		t.Fatal(err)
	}
	if bad := dbMutating(de.m.Ran()); len(bad) > 0 {
		t.Errorf("xem trước chạy lệnh thay đổi máy: %v", bad)
	}

	rep, err := p.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("job: %v %s (%s)%s", err, rep.Job.Status, rep.Job.Error, failedChecks(rep.Checks))
	}
	if len(de.sqls) != 1 {
		t.Fatalf("các lô SQL đổi trạng thái = %q, muốn đúng một lô", de.sqls)
	}
	g := m34CreateUser.FindStringSubmatch(de.sqls[0])
	if g == nil || g[1] != de.l.DB || g[2] != dbOldPassword {
		t.Errorf("câu tạo user = %q; muốn user %s với mật khẩu đang ghi trong wp-config.php", de.sqls[0], de.l.DB)
	}
	if !strings.Contains(de.sqls[0], "GRANT ALL PRIVILEGES ON `"+strings.ReplaceAll(de.l.DB, "_", `\_`)+"`.*") {
		t.Errorf("không cấp quyền trên đúng DB của site: %q", de.sqls[0])
	}
	if strings.Contains(de.sqls[0], "ACCOUNT LOCK") {
		t.Errorf("DB không tạm khoá mà user tạo lại bị khoá: %q", de.sqls[0])
	}
	if len(de.alters) != 0 || len(de.wpSets) != 0 {
		t.Errorf("tạo lại user mà còn đổi mật khẩu (ALTER %d lần) hoặc ghi wp-config.php (%d lần)", len(de.alters), len(de.wpSets))
	}
	for _, c := range de.m.Ran() {
		if strings.Contains(c.String(), dbOldPassword) {
			t.Errorf("mật khẩu trên dòng lệnh: %s", c.String())
		}
	}

	// Bản không trả mật khẩu (công cụ AI) đi cùng đường.
	de.gone(false, true)
	if pl, err := de.b.PrepareDBPassword(ctx, d); err != nil || !reflect.DeepEqual(stepIDs(pl.Plan), []string{"mariadb.site-user-restore:" + dbDom}) {
		t.Errorf("PrepareDBPassword khi user mất: %v, %v", pl, err)
	}

	// User đã có lại: bấm đổi mật khẩu lần nữa là đổi mật khẩu thật.
	de.gone(false, false)
	sp, err = de.b.PrepareSiteDBPassword(ctx, d)
	if err != nil || !reflect.DeepEqual(stepIDs(sp.Plan), []string{"mariadb.db-password:" + dbDom}) {
		t.Errorf("user đã có lại: %v, %v; muốn bước đổi mật khẩu thường", sp, err)
	}
}

// Site hay DB của site đang tạm khoá vẫn phải tạo lại được user (mở khoá cũng
// cần user đó), nhưng tạo lại không được là một cách mở khoá lén.
func TestSiteDBUserMatKhiDangTamKhoaHoacMatCaDB(t *testing.T) {
	d := mustDomain(t, dbDom)
	lockOf := func(p *Planned) bool { return p.Plan.Steps[0].(*steps.SiteDBUserRestore).Locked }

	// Site tạm khoá (PHP tắt, DB không khoá): tạo lại được, không khoá user,
	// không gắn kiểm chứng "site trả 200" (site đang trả trang tạm khoá).
	de := newDBEnv(t, false)
	de.mutate(t, func(s *site.Site) { s.Suspended = account.SuspendSelf })
	de.gone(false, true)
	sp, err := de.b.PrepareSiteDBPassword(ctx, d)
	if err != nil {
		t.Fatalf("site tạm khoá: bị từ chối: %v", err)
	}
	if lockOf(sp.Planned) || len(sp.Plan.Verify) != 0 {
		t.Errorf("site tạm khoá: khoá user = %v, %d phép kiểm chứng; muốn không khoá, không kiểm chứng", lockOf(sp.Planned), len(sp.Plan.Verify))
	}

	// DB của site tạm khoá: user tạo ra ở trạng thái khoá, tiêu đề nói rõ.
	de = newDBEnv(t, false)
	de.mutate(t, func(s *site.Site) { s.DBSuspended = account.SuspendSelf })
	de.gone(false, true)
	sp, err = de.b.PrepareSiteDBPassword(ctx, d)
	if err != nil {
		t.Fatalf("DB tạm khoá: bị từ chối: %v", err)
	}
	if !lockOf(sp.Planned) || len(sp.Plan.Verify) != 0 || !strings.Contains(sp.Plan.Title.Vi, "trạng thái khoá") {
		t.Errorf("DB tạm khoá: khoá user = %v, %d phép kiểm chứng, tiêu đề %q", lockOf(sp.Planned), len(sp.Plan.Verify), sp.Plan.Title.Vi)
	}
	if rep, err := sp.Execute(ctx, nil); err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("DB tạm khoá: job: %v %s (%s)", err, rep.Job.Status, rep.Job.Error)
	}
	if len(de.sqls) != 1 || !strings.Contains(de.sqls[0], "ACCOUNT LOCK") {
		t.Errorf("DB tạm khoá mà user tạo lại không bị khoá ngay trong cùng lô: %q", de.sqls)
	}

	// Mất cả DB lẫn user: vẫn tạo lại được user (đường duy nhất để sau đó khôi
	// phục), không kiểm chứng "site chạy lại", tiêu đề chỉ việc tiếp theo.
	de = newDBEnv(t, false)
	de.gone(true, true)
	sp, err = de.b.PrepareSiteDBPassword(ctx, d)
	if err != nil {
		t.Fatalf("mất cả DB lẫn user: bị từ chối: %v", err)
	}
	if len(sp.Plan.Verify) != 0 || !strings.Contains(sp.Plan.Title.Vi, "DB của site cũng không còn") || !strings.Contains(sp.Plan.Title.Vi, "khôi phục site từ một bản backup") {
		t.Errorf("mất cả hai: %d phép kiểm chứng, tiêu đề %q", len(sp.Plan.Verify), sp.Plan.Title.Vi)
	}
	if rep, err := sp.Execute(ctx, nil); err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("mất cả hai: job: %v %s (%s)", err, rep.Job.Status, rep.Job.Error)
	}
}

// Site PHP trống: tpan không biết mật khẩu trong mã của site, nên user tạo lại
// có mật khẩu mới, trả cho người gọi để hiện đúng một lần.
func TestSiteDBUserMatSitePHPTrong(t *testing.T) {
	d := mustDomain(t, dbDom)
	de := newDBEnv(t, false)
	de.mutate(t, func(s *site.Site) { s.App = site.AppPHP })
	de.gone(false, true)

	sp, err := de.b.PrepareSiteDBPassword(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := stepIDs(sp.Plan), []string{"mariadb.user-recreate:" + de.l.DB}; !reflect.DeepEqual(got, want) {
		t.Fatalf("bước = %v, muốn %v", got, want)
	}
	pw := sp.Password.Reveal()
	if len(pw) != 32 || pw == dbOldPassword || sp.Name != de.l.DB {
		t.Errorf("mật khẩu mới dài %d, tên %q; muốn 32 ký tự cho user %s", len(pw), sp.Name, de.l.DB)
	}
	st := sp.Plan.Steps[0].(*steps.DBUserRecreate)
	if st.NoDB || st.Locked || len(sp.Plan.Verify) != 1 {
		t.Errorf("bước = %+v, %d phép kiểm chứng; muốn DB phải còn, không khoá, kiểm DB và user còn đó", st, len(sp.Plan.Verify))
	}
	if !strings.Contains(sp.Plan.Title.Vi, "tự sửa mật khẩu trong cấu hình của mã nguồn") {
		t.Errorf("tiêu đề không nhắc sửa mật khẩu trong mã nguồn: %s", sp.Plan.Title.Vi)
	}

	// Mất cả DB: user vẫn tạo được (NoDB), không kiểm chứng.
	de.gone(true, true)
	sp, err = de.b.PrepareSiteDBPassword(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if st := sp.Plan.Steps[0].(*steps.DBUserRecreate); !st.NoDB || len(sp.Plan.Verify) != 0 {
		t.Errorf("mất cả DB: NoDB = %v, %d phép kiểm chứng", st.NoDB, len(sp.Plan.Verify))
	}
	rep, err := sp.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("job: %v %s (%s)", err, rep.Job.Status, rep.Job.Error)
	}
	g := m34CreateUser.FindStringSubmatch(strings.Join(de.sqls, "\n"))
	if g == nil || g[1] != de.l.DB || g[2] != sp.Password.Reveal() {
		t.Errorf("câu tạo user = %q; muốn user %s với mật khẩu Plan trả ra", de.sqls, de.l.DB)
	}
}

// DB của site đã bị xoá ngoài tpan (user còn): đổi mật khẩu, xuất, backup bấm
// tay từ chối kèm đường đi; "Nhập" một bản xuất tạo lại DB từ file.
func TestSiteDBMatMaUserCon(t *testing.T) {
	de := newDBEnv(t, false)
	d := mustDomain(t, dbDom)
	bk := de.dump(t) // bản xuất có từ trước khi DB bị xoá
	de.gone(true, false)

	missing := func(what string, err error) {
		t.Helper()
		if refusedCode(t, err) != CodeDBMissing {
			t.Errorf("%s: %v; muốn từ chối %s", what, err, CodeDBMissing)
			return
		}
		msg := err.(*RefusedError).Message.Vi
		if !strings.Contains(msg, "tpan backup restore "+dbDom) || !strings.Contains(msg, "tpan db import "+dbDom) {
			t.Errorf("%s: lời từ chối không chỉ đường lấy lại DB: %s", what, msg)
		}
	}
	_, err := de.b.PrepareSiteDBPassword(ctx, d)
	missing("đổi mật khẩu", err)
	_, err = de.b.PrepareDBDump(ctx, d)
	missing("xuất DB", err)
	_, err = de.b.PrepareCreate(ctx, d, ReasonManual)
	missing("backup bấm tay", err)
	// Backup theo lịch không bị chặn ở lúc dựng Plan: lượt của các site khác
	// không được phụ thuộc vào một site hỏng.
	// (Máy giả này không có lệnh du, nên Plan dừng ở bước đo chỗ trống: điều
	// cần thấy là nó đi QUA chỗ chặn của M34.)
	var ref *RefusedError
	if _, err := de.b.PrepareCreate(ctx, d, ReasonSchedule); errors.As(err, &ref) && ref.Code == CodeDBMissing {
		t.Errorf("backup theo lịch bị chặn bởi %s lúc dựng Plan: %v", CodeDBMissing, err)
	}

	p, err := de.b.PrepareDBImport(ctx, d, bk.ID)
	if err != nil {
		t.Fatalf("nhập vào DB đã mất: %v", err)
	}
	if got, want := stepIDs(p.Plan), []string{"db.import:" + dbDom}; !reflect.DeepEqual(got, want) {
		t.Fatalf("bước = %v, muốn %v (không có bước xuất DB trước)", got, want)
	}
	imp := p.Plan.Steps[0].(*steps.DBImport)
	if !imp.Fresh || imp.UndoFile != "" || imp.File != bk.Path {
		t.Errorf("bước nhập = %+v", imp)
	}
	if !strings.Contains(p.Plan.Title.Vi, "không có bản tự xuất trước khi nhập") {
		t.Errorf("tiêu đề không nói lần này không có bản tự xuất: %s", p.Plan.Title.Vi)
	}
	rep, err := p.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("job: %v %s (%s)%s", err, rep.Job.Status, rep.Job.Error, failedChecks(rep.Checks))
	}
	if !reflect.DeepEqual(de.imports, []string{bk.Path}) {
		t.Errorf("các lần nhập = %v, muốn đúng %s", de.imports, bk.Path)
	}
	for _, c := range de.m.Ran() {
		if c.Name == "mariadb-dump" && !strings.Contains(c.String(), bk.Path) {
			t.Errorf("DB không có mà vẫn chạy mariadb-dump: %s", c.String())
		}
	}
	// Không có bản "trước khi nhập" nào được ghi: không có gì để xuất.
	rows, _ := de.st.Backups(ctx, dbDom)
	if len(rows) != 1 || rows[0].ID != bk.ID {
		t.Errorf("bảng backups sau khi nhập = %+v, muốn chỉ bản xuất ban đầu", rows)
	}
}

// User DB của site đã mất: nhập bản xuất từ chối, chỉ sang đổi mật khẩu DB.
func TestSiteDBUserMatThiNhapTuChoi(t *testing.T) {
	de := newDBEnv(t, false)
	d := mustDomain(t, dbDom)
	bk := de.dump(t)
	for _, dbGone := range []bool{false, true} {
		de.gone(dbGone, true)
		_, err := de.b.PrepareDBImport(ctx, d, bk.ID)
		if refusedCode(t, err) != CodeDBUserMissing || !strings.Contains(err.(*RefusedError).Message.Vi, "tpan db passwd "+dbDom) {
			t.Errorf("DB mất=%v: %v; muốn %s kèm lệnh tạo lại user", dbGone, err, CodeDBUserMissing)
		}
	}
	if len(de.imports) != 0 {
		t.Errorf("bị từ chối mà vẫn nhập: %v", de.imports)
	}
}

// ---------------------------------------------------------------- khôi phục

// dropSiteDB xoá DB (và user DB) của site thẳng trong MariaDB giả.
func (be *bktEnv) dropSiteDB(l site.Layout, db, user bool) {
	be.m.mu.Lock()
	if db {
		delete(be.m.dbs, l.DB)
	}
	if user {
		delete(be.m.dbUsers, l.DB)
	}
	be.m.mu.Unlock()
	if db {
		be.mu.Lock()
		delete(be.body, l.DB)
		be.mu.Unlock()
	}
}

func (be *bktEnv) hasDB(l site.Layout) bool {
	be.m.mu.Lock()
	defer be.m.mu.Unlock()
	return be.m.dbs[l.DB]
}

// DB của site đã bị xoá ngoài tpan: khôi phục từ một bản backup đầy đủ tạo lại
// DB từ bản đó. Không có bước tự backup và không có bước xuất DB trước (cả hai
// cần một DB để xuất), và tiêu đề nói rõ điều đó. Trước M34 job hỏng ngay ở
// bước tự backup, nên đường cứu duy nhất của site không dùng được.
func TestBackupRestoreKhiDBCuaSiteDaMat(t *testing.T) {
	const dom = "shop.example.com"
	be := newBktEnv(t, dom, "blog.example.com")
	d := mustDomain(t, dom)
	l := site.LayoutOf(d)
	rd := site.RestoreDir(l)
	src := be.seedFull(t, dom, "20261101-000000", "từ backup", "db từ backup", ReasonManual)
	be.dropSiteDB(l, true, false)

	p, err := be.b.PrepareRestore(ctx, d, src.ID)
	if err != nil {
		t.Fatalf("PrepareRestore khi DB đã mất: %v", err)
	}
	want := []string{"backup.extract:" + dom, "dir.move:" + l.Public, "dir.move:" + rd + "/public", "db.import:" + dom}
	if got := stepIDs(p.Plan); !reflect.DeepEqual(got, want) {
		t.Fatalf("thứ tự bước:\n got %v\nwant %v", got, want)
	}
	if imp := p.Plan.Steps[3].(*steps.DBImport); !imp.Fresh || imp.UndoFile != "" {
		t.Errorf("bước nhập = %+v, muốn Fresh và không có file hoàn tác", imp)
	}
	if !strings.Contains(p.Plan.Title.Vi, "KHÔNG có bản tự backup") || !strings.Contains(p.Plan.Title.Vi, "không còn trong MariaDB") {
		t.Errorf("tiêu đề không nói lần này không có bản tự backup: %s", p.Plan.Title.Vi)
	}
	if len(p.Plan.Verify) == 0 {
		t.Error("khôi phục mà không có kiểm chứng")
	}

	be.m.ResetRan()
	rep, err := p.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("chạy: %v %s (%s)%s", err, rep.Job.Status, rep.Job.Error, failedChecks(rep.Checks))
	}
	if got := be.read(t, l.Public+"/index.php"); got != "từ backup" {
		t.Errorf("index.php = %q, muốn nội dung của bản backup", got)
	}
	if !be.hasDB(l) || be.dbBody(l) != "db từ backup" {
		t.Errorf("sau khôi phục: DB có = %v, nội dung %q; muốn có lại với nội dung của bản backup", be.hasDB(l), be.dbBody(l))
	}
	for _, c := range be.m.Ran() {
		if c.Name == "mariadb-dump" {
			t.Errorf("DB không có mà vẫn chạy mariadb-dump: %s", c.String())
		}
	}
	if be.exists(rd) {
		t.Errorf("%s còn sau khi job thành công", rd)
	}
	// Không có dòng "trước khi khôi phục": không có bản tự backup nào được tạo.
	list, _ := be.b.List(ctx, dom)
	if len(list) != 1 || list[0].ID != src.ID {
		t.Errorf("bảng backups = %+v, muốn chỉ bản nguồn", list)
	}

	// Lần sau DB đã có lại: Plan về đường thường (có tự backup, có xuất trước).
	p, err = be.b.PrepareRestore(ctx, d, src.ID)
	if err != nil || !reflect.DeepEqual(stepIDs(p.Plan), restoreStepIDs(dom, l)) {
		t.Errorf("DB đã có lại: bước = %v, %v", stepIDs(p.Plan), err)
	}
}

// Nhập lỗi giữa chừng khi DB vốn đã mất: hoàn tác về đúng trạng thái trước
// (mã nguồn cũ ở chỗ cũ, không có DB), không để lại một DB nhập dở.
func TestBackupRestoreKhiDBDaMatMaNhapLoi(t *testing.T) {
	const dom = "shop.example.com"
	be := newBktEnv(t, dom)
	d := mustDomain(t, dom)
	l := site.LayoutOf(d)
	src := be.seedFull(t, dom, "20261101-000000", "từ backup", "db từ backup", ReasonManual)
	be.dropSiteDB(l, true, false)
	be.importFail = "/db.sql"

	p, err := be.b.PrepareRestore(ctx, d, src.ID)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := p.Execute(ctx, nil)
	if err != nil || rep.Job.Status == engine.JobSucceeded {
		t.Fatalf("chạy: %v %s, muốn job lỗi", err, rep.Job.Status)
	}
	if got := be.read(t, l.Public+"/index.php"); got != "live "+dom {
		t.Errorf("index.php = %q sau hoàn tác", got)
	}
	if be.hasDB(l) {
		t.Error("sau hoàn tác còn một DB nhập dở (trước job DB không có)")
	}
	if be.exists(site.RestoreDir(l)) {
		t.Error("thư mục khôi phục còn sau hoàn tác")
	}
	if list, _ := be.b.List(ctx, dom); len(list) != 1 || list[0].ID != src.ID {
		t.Errorf("bảng backups = %+v, muốn chỉ bản nguồn", list)
	}
}

// User DB của site đã mất: khôi phục từ chối trước khi đụng gì, chỉ sang đổi
// mật khẩu DB (kiểm chứng sau khôi phục chắc chắn hỏng khi thiếu user).
func TestBackupRestoreKhiUserDBDaMat(t *testing.T) {
	const dom = "shop.example.com"
	be := newBktEnv(t, dom)
	d := mustDomain(t, dom)
	l := site.LayoutOf(d)
	src := be.seedFull(t, dom, "20261101-000000", "từ backup", "db từ backup", ReasonManual)
	for _, dbGone := range []bool{false, true} {
		be.dropSiteDB(l, dbGone, true)
		be.m.ResetRan()
		_, err := be.b.PrepareRestore(ctx, d, src.ID)
		if refusedCode(t, err) != CodeDBUserMissing || !strings.Contains(err.(*RefusedError).Message.Vi, "tpan db passwd "+dom) {
			t.Errorf("DB mất=%v: %v; muốn %s kèm lệnh tạo lại user", dbGone, err, CodeDBUserMissing)
		}
		for _, c := range mutatingRan(be.m.Ran()) {
			t.Errorf("DB mất=%v: bị từ chối mà chạy lệnh thay đổi máy: %s", dbGone, c)
		}
	}
	if got := be.read(t, l.Public+"/index.php"); got != "live "+dom {
		t.Errorf("index.php = %q: bị từ chối mà mã nguồn đã đổi", got)
	}
}

// ---------------------------------------------------------------- khoá, charset

// Tạm khoá, mở khoá, đổi charset DB của một site khi DB hay user DB đã mất.
// Trước M34 "tạm khoá" báo thành công dù không khoá được gì (bước khoá coi
// user không có là "đã đúng"), rồi site mang cờ khoá DB mà không mở lại được.
func TestSiteDBKhoaMoKhoaCharsetKhiMat(t *testing.T) {
	e := newDBsEnv(t)
	userGone := func(what string, err error, dom string) {
		t.Helper()
		if dbsCode(err) != CodeDBUserMissing || !strings.Contains(err.(*RefusedError).Message.Vi, "tpan db passwd "+dom) {
			t.Errorf("%s: %v; muốn %s kèm lệnh tạo lại user", what, err, CodeDBUserMissing)
		}
	}

	// User mất, DB còn.
	st := e.site(t, "shop.example.com", nil)
	e.dropOutside(st.Layout().DB, false, true)
	_, err := e.d.PrepareSiteDBSuspend(ctx, st.Domain)
	userGone("tạm khoá", err, "shop.example.com")
	if got, _, _ := e.sites.Site(ctx, st.Domain); got.DBSuspended != account.NotSuspended {
		t.Errorf("bị từ chối mà site mang cờ khoá DB: %q", got.DBSuspended)
	}

	// Đang khoá DB thì user bị xoá: mở khoá từ chối, chỉ sang tạo lại user.
	st2 := e.site(t, "blog.example.com", func(s *site.Site) { s.DBSuspended = account.SuspendSelf })
	e.dropOutside(st2.Layout().DB, false, true)
	_, err = e.d.PrepareSiteDBUnsuspend(ctx, st2.Domain)
	userGone("mở khoá", err, "blog.example.com")

	// DB mất, user còn (đang khoá): mở khoá vẫn được, bỏ kiểm chứng "site chạy
	// lại" (site chưa thể chạy cho tới khi DB được khôi phục, mà khôi phục lại
	// cần user đã mở khoá). Đổi charset thì từ chối: không có gì để đổi.
	st3 := e.site(t, "news.example.com", func(s *site.Site) { s.DBSuspended = account.SuspendSelf })
	e.sim.mu.Lock()
	u := e.sim.users[st3.Layout().DB]
	u.locked = true
	e.sim.users[st3.Layout().DB] = u
	e.sim.mu.Unlock()
	e.dropOutside(st3.Layout().DB, true, false)
	p, err := e.d.PrepareSiteDBUnsuspend(ctx, st3.Domain)
	if err != nil || len(p.Plan.Verify) != 0 {
		t.Fatalf("mở khoá khi DB đã mất: %v, %v; muốn Plan không kiểm chứng", p, err)
	}
	dbsExec(t)(p, nil)
	if e.sim.users[st3.Layout().DB].locked {
		t.Error("user DB chưa được mở khoá")
	}
	if _, err := e.d.PrepareSiteDBCharset(ctx, st3.Domain, "utf8mb4", "utf8mb4_bin"); dbsCode(err) != CodeDBMissing {
		t.Errorf("đổi charset khi DB đã mất: %v; muốn %s", err, CodeDBMissing)
	}

	// Không hỏi được MariaDB: không kết luận "đã mất", giữ đường cũ.
	st4 := e.site(t, "ok.example.com", nil)
	e.d.Sys = dbsDown{e.sim}
	if p, err := e.d.PrepareSiteDBSuspend(ctx, st4.Domain); err != nil || p.Plan.Kind != "db.suspend" {
		t.Errorf("MariaDB không trả lời: tạm khoá = %v, %v; muốn Plan thường", p, err)
	}
}

// Lời từ chối của hai mã phải đọc được ở cả hai thứ tiếng.
func TestSiteDBLoiTuChoiDocDuoc(t *testing.T) {
	l := site.LayoutOf(mustDomain(t, "shop.example.com"))
	for _, e := range []*RefusedError{
		siteDBGoneErr(l.DB, "shop.example.com"),
		siteDBUserGoneErr(l, i18n.T("khoá", "lock")),
	} {
		for _, s := range []string{e.Message.Vi, e.Message.En} {
			if !strings.Contains(s, "shop.example.com") || !strings.Contains(s, l.DB) || !strings.Contains(s, "sudo tpan ") {
				t.Errorf("%s: lời từ chối thiếu tên site, tên DB hoặc lệnh: %s", e.Code, s)
			}
		}
	}
}

// ---------------------------------------------------------------- sửa tự động

// Nút "Sửa" của db.access: trước M34 nó chỉ biết mở khoá user DB, nên với user
// đã bị xoá hay DB đã mất thì job chạy rồi hỏng mà không nói phải làm gì. Giờ
// user mất thì tạo lại ngay (site WordPress), DB mất thì từ chối kèm đường đi;
// và lời của chính phép kiểm chỉ đúng lệnh cho từng trường hợp.
func TestFixDBAccessKhiUserDBHoacDBDaMat(t *testing.T) {
	e := newFixEnv(t, Nginx, fixDom)
	l := site.LayoutOf(mustDomain(t, fixDom))
	e.m.SetFile(l.WPConfig, []byte("<?php\n"+
		"define( 'DB_NAME', '"+l.DB+"' );\n"+
		"define( 'DB_USER', '"+l.DB+"' );\n"+
		"define( 'DB_PASSWORD', '"+dbOldPassword+"' );\n"),
		0o600, system.Owner{User: l.User, Group: l.User})
	// wp db query theo trạng thái của MariaDB giả: thiếu user là 1045, thiếu DB là 1049.
	e.m.Handle([]string{"wp", "db", "query"}, func(c system.Cmd) system.Result {
		e.m.mu.Lock()
		defer e.m.mu.Unlock()
		switch {
		case !e.m.dbUsers[l.DB]:
			return system.Result{ExitCode: 1, Stderr: "ERROR 1045 (28000): Access denied for user '" + l.DB + "'@'localhost' (using password: YES)\n"}
		case !e.m.dbs[l.DB]:
			return system.Result{ExitCode: 1, Stderr: "ERROR 1049 (42000): Unknown database '" + l.DB + "'\n"}
		}
		return system.Result{Stdout: "information_schema\n" + l.DB + "\n"}
	})
	drop := func(db, user bool) {
		e.m.mu.Lock()
		defer e.m.mu.Unlock()
		if db {
			delete(e.m.dbs, l.DB)
		}
		if user {
			delete(e.m.dbUsers, l.DB)
		}
	}
	fixes := func(r checks.Result) string { return strings.Join(r.ManualFix, "\n") }

	// User DB bị xoá: phép kiểm chỉ sang `tpan db passwd`, nút Sửa tạo lại user.
	drop(false, true)
	r := e.recheck(t, fixDom, "db.access:"+fixDom)
	if r.Status != checks.Fail || !strings.Contains(fixes(r), "tpan db passwd "+fixDom) || strings.Contains(fixes(r), "tpan site add") {
		t.Fatalf("user DB mất: %s, cách sửa tay:\n%s", r.Status, fixes(r))
	}
	x := e.fix(t, r)
	if got, want := stepIDsOf(x), []string{"mariadb.site-user-restore:" + fixDom}; !reflect.DeepEqual(got, want) {
		t.Errorf("bước của nút Sửa = %v, muốn %v", got, want)
	}
	if r := e.recheck(t, fixDom, "db.access:"+fixDom); r.Status != checks.Pass {
		t.Errorf("sau khi sửa: %s %s", r.Status, r.Summary.Vi)
	}

	// DB bị xoá: phép kiểm chỉ sang khôi phục từ backup; nút Sửa từ chối kèm đường đi.
	drop(true, false)
	r = e.recheck(t, fixDom, "db.access:"+fixDom)
	if r.Status != checks.Fail || !strings.Contains(fixes(r), "tpan backup restore "+fixDom) || !strings.Contains(r.Cause.Vi, "không còn trong MariaDB") {
		t.Fatalf("DB mất: %s, nguyên nhân %q, cách sửa tay:\n%s", r.Status, r.Cause.Vi, fixes(r))
	}
	if _, err := e.f.Prepare(ctx, r); refusedCode(t, err) != CodeDBMissing {
		t.Errorf("nút Sửa khi DB đã mất: %v; muốn %s", err, CodeDBMissing)
	}
}
