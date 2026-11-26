package core

import (
	"context"
	"errors"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

// dbkStore là store thật (bảng backups, databases thật: chủ và charset của
// dòng backup được chép bằng đúng câu SQL chạy trên máy thật); chỉ SnapshotDB
// ghi vào máy giả, như bktStore.
type dbkStore struct {
	*store.Store
	sim *dbsSim
}

func (s dbkStore) SnapshotDB(_ context.Context, p string) error {
	s.sim.SetFile(p, []byte("sqlite giả"), 0o600, system.Root)
	return nil
}

// dbkEnv: DBs trên MariaDB giả (dbsSim) và store thật.
type dbkEnv struct {
	d   *DBs
	b   *Backups
	sim *dbsSim
	st  *store.Store
	acc *dbsMemAccounts
	now time.Time
}

func newDBkEnv(t *testing.T) *dbkEnv {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "tpan.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	sim := newDBsSim()
	sim.Handle([]string{"sha256sum", "--"}, func(c system.Cmd) system.Result {
		data, err := sim.ReadFile(c.Args[1])
		if err != nil {
			return system.Result{ExitCode: 1, Stderr: "sha256sum: " + err.Error()}
		}
		return system.Result{Stdout: bktSHA(data) + "  " + c.Args[1] + "\n"}
	})
	user := func(n string) account.User {
		return account.User{Name: n, Role: account.RoleUser, Status: account.Active, Package: account.DefaultPackage}
	}
	e := &dbkEnv{sim: sim, st: st, now: time.Date(2026, 11, 26, 5, 0, 0, 0, time.UTC)}
	e.acc = &dbsMemAccounts{users: map[string]account.User{
		"alice": user("alice"), "abc": user("abc"), "abc_de": user("abc_de"),
	}}
	now := func() time.Time { return e.now }
	e.b = &Backups{Sites: &Sites{Sys: sim, Settings: st, Store: st, Journal: dbsJournal{}, Now: now}, Store: dbkStore{Store: st, sim: sim}, Now: now}
	e.d = &DBs{Sys: sim, Settings: st, Store: st, Sites: st, Accounts: e.acc, Journal: dbsJournal{}, Now: now, Backups: e.b}
	return e
}

// tick đẩy đồng hồ một giây: tên file sao lưu có giờ tới giây.
func (e *dbkEnv) tick() { e.now = e.now.Add(time.Second) }

func (e *dbkEnv) add(t *testing.T, owner, short, data string) *DBAdd {
	t.Helper()
	a, err := e.d.PrepareAdd(ctx, DBInput{Owner: dbsUser(t, owner), Name: short, Charset: "utf8mb4", Collation: "utf8mb4_bin"})
	if err != nil {
		t.Fatal(err)
	}
	dbsExec(t)(a.Planned, nil)
	e.setData(a.DB.Name, data)
	return a
}

func (e *dbkEnv) setData(name, data string) {
	e.sim.mu.Lock()
	defer e.sim.mu.Unlock()
	d := e.sim.dbs[name]
	d.data = data
	e.sim.dbs[name] = d
}

func (e *dbkEnv) data(name string) string {
	e.sim.mu.Lock()
	defer e.sim.mu.Unlock()
	return e.sim.dbs[name].data
}

func (e *dbkEnv) rows(t *testing.T, name string) []Backup {
	t.Helper()
	rows, err := e.st.Backups(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// backup sao lưu bấm tay và trả dòng vừa ghi.
func (e *dbkEnv) backup(t *testing.T, name string) Backup {
	t.Helper()
	e.tick()
	dbsExec(t)(e.d.PrepareBackup(ctx, name, ReasonManual))
	return e.rows(t, name)[0]
}

func dbkCode(err error) string {
	var re *RefusedError
	if errors.As(err, &re) {
		return re.Code
	}
	if err == nil {
		return "<nil>"
	}
	return "lỗi thường: " + err.Error()
}

// Sao lưu DB rời: một dòng Kind sql mang tên DB, chủ và charset của DB; nhập
// lại đưa dữ liệu về đúng bản đó, giữ user DB và mật khẩu, và để lại bản
// before-import của dữ liệu vừa bị thay.
func TestDBBackupVaNhapLai(t *testing.T) {
	e := newDBkEnv(t)
	add := e.add(t, "alice", "shop", "v1")
	bk := e.backup(t, "alice_shop")
	if bk.Domain != "alice_shop" || bk.Kind != BackupSQL || bk.Reason != ReasonManual || !bk.DB || !bk.Local {
		t.Fatalf("dòng backup = %+v", bk)
	}
	if path.Dir(bk.Path) != site.DBBackupDir("alice_shop") || !strings.HasSuffix(bk.Path, ".sql") {
		t.Errorf("file = %s, muốn nằm trong %s", bk.Path, site.DBBackupDir("alice_shop"))
	}
	if fi, err := e.sim.Stat(bk.Path); err != nil || fi.Mode.Perm() != 0o600 {
		t.Errorf("file sao lưu: %+v, %v (muốn 0600)", fi, err)
	}
	if o, ok, err := e.st.BackupOwner(ctx, bk.ID); err != nil || !ok || o != "alice" {
		t.Errorf("chủ của dòng backup = %q, %v, %v; muốn alice (chủ của DB rời)", o, ok, err)
	}
	if cs, coll, err := e.st.BackupDBCharset(ctx, bk.ID); err != nil || cs != "utf8mb4" || coll != "utf8mb4_bin" {
		t.Errorf("charset nhớ trong dòng backup = %q, %q, %v", cs, coll, err)
	}

	e.setData("alice_shop", "v2")
	e.tick()
	p, err := e.d.PrepareImport(ctx, "alice_shop", bk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(p.Plan.Steps); n != 2 || !p.Plan.Steps[1].Info().Danger {
		t.Errorf("Plan nhập: %d bước; muốn xuất bản hiện tại rồi nhập (bước nhập là bước nguy hiểm)", n)
	}
	dbsExec(t)(p, nil)
	if got := e.data("alice_shop"); got != "v1" {
		t.Errorf("dữ liệu sau khi nhập = %q, muốn v1", got)
	}
	if u := e.sim.users["alice_shop"]; u.pw != add.Password.Reveal() {
		t.Errorf("mật khẩu user DB đổi sau khi nhập")
	}
	if d := e.sim.dbs["alice_shop"]; d.cs != "utf8mb4" || d.coll != "utf8mb4_bin" {
		t.Errorf("charset sau khi nhập = %+v", d)
	}
	rows := e.rows(t, "alice_shop")
	if len(rows) != 2 || rows[0].Reason != ReasonBeforeImport {
		t.Fatalf("các dòng sau khi nhập = %+v; muốn thêm một bản before-import", rows)
	}
	if raw, _ := e.sim.ReadFile(rows[0].Path); !strings.Contains(string(raw), "data:v2") {
		t.Errorf("bản before-import = %q, muốn là dữ liệu ngay trước lúc nhập (v2)", raw)
	}
}

func TestDBBackupTuChoi(t *testing.T) {
	e := newDBkEnv(t)
	e.add(t, "alice", "shop", "v1")
	e.add(t, "alice", "blog", "b1")
	bk := e.backup(t, "alice_shop")
	other := e.backup(t, "alice_blog")

	if _, err := e.d.PrepareBackup(ctx, "alice_none", ReasonManual); dbkCode(err) != CodeDBNotFound {
		t.Errorf("DB không có: %s", dbkCode(err))
	}
	if _, err := e.d.PrepareBackup(ctx, "alice_shop", ReasonBeforePurge); err == nil {
		t.Error("lý do before-purge không dùng cho sao lưu bấm tay")
	}
	for _, id := range []string{"", "zz", "deadbeef", other.ID} {
		if _, err := e.d.PrepareImport(ctx, "alice_shop", id); dbkCode(err) != "backup_not_found" {
			t.Errorf("nhập id %q: %s, muốn backup_not_found", id, dbkCode(err))
		}
	}
	// File bị sửa sau lúc sao lưu: sha256 không khớp.
	e.sim.SetFile(bk.Path, []byte("-- data:gia\n-- Dump completed on x\n"), 0o600, system.Root)
	if _, err := e.d.PrepareImport(ctx, "alice_shop", bk.ID); dbkCode(err) != "backup_corrupt" {
		t.Errorf("file bị sửa: %s, muốn backup_corrupt", dbkCode(err))
	}
	_ = e.sim.Remove(bk.Path)
	if _, err := e.d.PrepareImport(ctx, "alice_shop", bk.ID); dbkCode(err) != "backup_corrupt" {
		t.Errorf("file đã mất: %s, muốn backup_corrupt", dbkCode(err))
	}
	// DB tạm khoá: sao lưu được, nhập thì không.
	dbsExec(t)(e.d.PrepareSuspend(ctx, "alice_blog"))
	e.tick()
	dbsExec(t)(e.d.PrepareBackup(ctx, "alice_blog", ReasonManual))
	if _, err := e.d.PrepareImport(ctx, "alice_blog", other.ID); dbkCode(err) != CodeDBSuspended {
		t.Errorf("nhập vào DB tạm khoá: %s", dbkCode(err))
	}
	dbsExec(t)(e.d.PrepareUnsuspend(ctx, "alice_blog"))
	// DB trong thùng rác: không sao lưu, không nhập.
	dbsExec(t)(e.d.PrepareTrash(ctx, "alice_blog"))
	if _, err := e.d.PrepareBackup(ctx, "alice_blog", ReasonManual); dbkCode(err) != codeDBTrashed {
		t.Errorf("sao lưu DB trong thùng rác: %s", dbkCode(err))
	}
	if _, err := e.d.PrepareImport(ctx, "alice_blog", other.ID); dbkCode(err) != codeDBTrashed {
		t.Errorf("nhập vào DB trong thùng rác: %s", dbkCode(err))
	}
}

// Xoá hẳn DB rời: file xuất ở thùng rác thành một bản before-purge, và DB tạo
// lại được từ bản đó với đúng tên, chủ, charset cũ, mật khẩu mới.
func TestDBXoaHanGiuBanCuoiVaTaoLai(t *testing.T) {
	e := newDBkEnv(t)
	add := e.add(t, "alice", "shop", "v1")
	dbsExec(t)(e.d.PrepareTrash(ctx, "alice_shop"))
	e.tick()
	p, err := e.d.PreparePurge(ctx, "alice_shop")
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, s := range p.Plan.Steps {
		ids = append(ids, strings.SplitN(s.Info().ID, ":", 2)[0])
	}
	if strings.Join(ids, " ") != "mariadb.sdb-keep-dump mariadb.user-absent" {
		t.Errorf("các bước xoá hẳn = %v; muốn chuyển file trước (hoàn tác được), xoá user sau", ids)
	}
	dbsExec(t)(p, nil)
	if _, ok, _ := e.st.StandaloneDB(ctx, "alice_shop"); ok {
		t.Error("dòng DB rời còn sau khi xoá hẳn")
	}
	if _, ok := e.sim.users["alice_shop"]; ok {
		t.Error("user DB còn sau khi xoá hẳn")
	}
	if _, err := e.sim.Stat(account.DBTrashDump("alice_shop")); err == nil {
		t.Error("file xuất còn ở thùng rác")
	}
	rows := e.rows(t, "alice_shop")
	if len(rows) != 1 || rows[0].Reason != ReasonBeforePurge || !rows[0].DB || path.Dir(rows[0].Path) != site.DBBackupDir("alice_shop") {
		t.Fatalf("các dòng backup sau khi xoá hẳn = %+v", rows)
	}
	if o, _, _ := e.st.BackupOwner(ctx, rows[0].ID); o != "alice" {
		t.Errorf("chủ của bản before-purge = %q", o)
	}

	// Tạo lại.
	e.tick()
	re, err := e.d.PrepareRecreate(ctx, rows[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if re.Plan.Kind != "dbs.recreate" || re.Plan.Target != "alice_shop" || len(re.Plan.Steps) != 2 {
		t.Errorf("Plan tạo lại = %s %s, %d bước", re.Plan.Kind, re.Plan.Target, len(re.Plan.Steps))
	}
	dbsExec(t)(re.Planned, nil)
	db, ok, _ := e.st.StandaloneDB(ctx, "alice_shop")
	if !ok || db.Owner != "alice" || db.Status != account.DBActive || db.Charset != "utf8mb4" || db.Collation != "utf8mb4_bin" {
		t.Errorf("DB tạo lại = %+v (có=%v)", db, ok)
	}
	if got := e.data("alice_shop"); got != "v1" {
		t.Errorf("dữ liệu sau khi tạo lại = %q, muốn v1", got)
	}
	u := e.sim.users["alice_shop"]
	if u.pw == "" || u.pw != re.Password.Reveal() || u.pw == add.Password.Reveal() {
		t.Errorf("mật khẩu sau khi tạo lại phải là mật khẩu mới trả ở Password")
	}
	// Bản before-purge vẫn còn (người dùng tự xoá khi không cần), và giờ nhập lại được như bản thường.
	if len(e.rows(t, "alice_shop")) != 1 {
		t.Errorf("bản before-purge mất sau khi tạo lại")
	}
	if _, err := e.d.PrepareRecreate(ctx, rows[0].ID); dbkCode(err) != CodeDBExists {
		t.Errorf("tạo lại khi DB đang có: %s, muốn db_exists", dbkCode(err))
	}
	e.setData("alice_shop", "v3")
	e.tick()
	dbsExec(t)(e.d.PrepareImport(ctx, "alice_shop", rows[0].ID))
	if got := e.data("alice_shop"); got != "v1" {
		t.Errorf("nhập bản before-purge vào DB đã tạo lại = %q", got)
	}
}

func TestDBTaoLaiTuChoi(t *testing.T) {
	e := newDBkEnv(t)
	e.add(t, "alice", "shop", "v1")
	bk := e.backup(t, "alice_shop")
	for _, id := range []string{"", "zz", "deadbeef"} {
		if _, err := e.d.PrepareRecreate(ctx, id); dbkCode(err) != "backup_not_found" {
			t.Errorf("id %q: %s", id, dbkCode(err))
		}
	}
	// DB ở thùng rác: khôi phục từ thùng rác chứ không tạo lại.
	dbsExec(t)(e.d.PrepareTrash(ctx, "alice_shop"))
	if _, err := e.d.PrepareRecreate(ctx, bk.ID); dbkCode(err) != codeDBTrashed {
		t.Errorf("DB ở thùng rác: %s", dbkCode(err))
	}
	e.tick()
	dbsExec(t)(e.d.PreparePurge(ctx, "alice_shop"))
	// Chủ cũ bị khoá: như tạo DB mới, từ chối.
	u := e.acc.users["alice"]
	u.Status = account.Suspended
	e.acc.users["alice"] = u
	if _, err := e.d.PrepareRecreate(ctx, bk.ID); dbkCode(err) != CodeUserSuspended {
		t.Errorf("chủ bị khoá: %s", dbkCode(err))
	}
	u.Status = account.Active
	e.acc.users["alice"] = u
	// Một dòng backup của site (domain) không phải bản sao lưu của DB rời.
	siteRow, err := e.st.AddBackup(ctx, Backup{Domain: "a.example.com", Kind: BackupSQL, Reason: ReasonManual, Path: site.BackupRoot + "/a.example.com/x.sql", SHA256: "0"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.d.PrepareRecreate(ctx, siteRow.ID); dbkCode(err) != "backup_not_found" {
		t.Errorf("dòng của site: %s", dbkCode(err))
	}
	if siteRow.DB {
		t.Error("dòng của site mang cờ DB")
	}
}

// File xuất ở thùng rác không trọn vẹn (hoặc đã mất): xoá hẳn vẫn làm được như
// trước M33 và không sinh bản sao lưu giả.
func TestDBXoaHanKhiFileXuatHong(t *testing.T) {
	for name, prep := range map[string]func(e *dbkEnv){
		"file dở": func(e *dbkEnv) {
			e.sim.SetFile(account.DBTrashDump("alice_shop"), []byte("-- data:v1\n-- nửa"), 0o600, system.Root)
		},
		"file đã mất": func(e *dbkEnv) { _ = e.sim.Remove(account.DBTrashDump("alice_shop")) },
	} {
		e := newDBkEnv(t)
		e.add(t, "alice", "shop", "v1")
		dbsExec(t)(e.d.PrepareTrash(ctx, "alice_shop"))
		prep(e)
		dbsExec(t)(e.d.PreparePurge(ctx, "alice_shop"))
		if _, ok, _ := e.st.StandaloneDB(ctx, "alice_shop"); ok {
			t.Errorf("%s: dòng DB còn", name)
		}
		if rows := e.rows(t, "alice_shop"); len(rows) != 0 {
			t.Errorf("%s: sinh dòng backup %+v", name, rows)
		}
		if _, err := e.sim.Stat(account.DBTrashDump("alice_shop")); err == nil {
			t.Errorf("%s: file ở thùng rác còn", name)
		}
	}
}

// Dọn theo số bản giữ: giữ Keep bản mới nhất của từng DB, không đụng bản
// before-purge và không đụng bản của DB khác.
func TestDBBackupDonBanCu(t *testing.T) {
	e := newDBkEnv(t)
	e.add(t, "alice", "shop", "v1")
	e.add(t, "alice", "blog", "b1")
	blog := e.backup(t, "alice_blog")
	var all []Backup
	for i := 0; i < DefaultBackupKeep+2; i++ {
		all = append(all, e.backup(t, "alice_shop"))
	}
	rows := e.rows(t, "alice_shop")
	if len(rows) != DefaultBackupKeep {
		t.Fatalf("còn %d bản, muốn %d", len(rows), DefaultBackupKeep)
	}
	for i, old := range all[:2] {
		if _, err := e.sim.Stat(old.Path); err == nil {
			t.Errorf("file của bản cũ thứ %d còn trên máy", i)
		}
	}
	if _, err := e.sim.Stat(all[len(all)-1].Path); err != nil {
		t.Error("bản mới nhất bị dọn")
	}
	if _, err := e.sim.Stat(blog.Path); err != nil || len(e.rows(t, "alice_blog")) != 1 {
		t.Error("dọn lấn sang bản của DB khác")
	}
}

// Hai chủ khác nhau sinh cùng một tên DB ("abc_de" + "shop" và "abc" +
// "de_shop"): bản của chủ cũ không nhập được vào DB của chủ mới, và không bị
// dọn thay chủ mới.
func TestDBBackupHaiChuTrungTen(t *testing.T) {
	e := newDBkEnv(t)
	e.add(t, "abc_de", "shop", "cua-abc_de")
	old := e.backup(t, "abc_de_shop")
	dbsExec(t)(e.d.PrepareTrash(ctx, "abc_de_shop"))
	e.tick()
	dbsExec(t)(e.d.PreparePurge(ctx, "abc_de_shop"))
	a := e.add(t, "abc", "de_shop", "cua-abc")
	if a.DB.Name != "abc_de_shop" {
		t.Fatalf("tên DB = %s", a.DB.Name)
	}
	for _, r := range e.rows(t, "abc_de_shop") {
		if _, err := e.d.PrepareImport(ctx, "abc_de_shop", r.ID); dbkCode(err) != "backup_not_found" {
			t.Errorf("bản %s (%s) của chủ cũ: %s, muốn backup_not_found", r.ID, r.Reason, dbkCode(err))
		}
	}
	for i := 0; i < DefaultBackupKeep+1; i++ {
		e.backup(t, "abc_de_shop")
	}
	if _, ok, _ := e.st.Backup(ctx, old.ID); !ok {
		t.Error("bản bấm tay của chủ cũ bị dọn thay chủ mới")
	}
	mine := 0
	for _, r := range e.rows(t, "abc_de_shop") {
		if o, _, _ := e.st.BackupOwner(ctx, r.ID); o == "abc" {
			mine++
		}
	}
	if mine != DefaultBackupKeep {
		t.Errorf("chủ mới còn %d bản, muốn %d", mine, DefaultBackupKeep)
	}
	if got := e.data("abc_de_shop"); got != "cua-abc" {
		t.Errorf("dữ liệu của chủ mới = %q", got)
	}
}

// Lịch backup: CreateAll sao lưu cả các DB rời đang có (không lấy DB trong
// thùng rác), trước khi chép tpan.db.
func TestCreateAllCoDBRoi(t *testing.T) {
	e := newDBkEnv(t)
	e.add(t, "alice", "shop", "v1")
	e.add(t, "alice", "blog", "b1")
	dbsExec(t)(e.d.PrepareTrash(ctx, "alice_blog"))
	e.sim.SetDir(site.TPANDBBackupDir, 0o700, system.Root)
	e.b.DBs = e.d
	e.tick()
	res, err := e.b.CreateAll(ctx, ReasonSchedule, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Domain != "alice_shop" || res[0].Error != "" || res[0].Report.Job.Status != engine.JobSucceeded {
		t.Fatalf("kết quả = %+v", res)
	}
	rows := e.rows(t, "alice_shop")
	if len(rows) != 1 || rows[0].Reason != ReasonSchedule {
		t.Errorf("dòng backup theo lịch = %+v", rows)
	}
	if len(e.rows(t, "alice_blog")) != 0 {
		t.Error("DB trong thùng rác cũng bị sao lưu theo lịch")
	}
}

// Hai bản trong cùng một giây (bấm sao lưu rồi lịch backup chạy ngay sau): bản
// sau lấy tên file của giây kế tiếp còn trống, không bị từ chối. Gặp thật trên
// máy ảo 26/11/2026: lịch backup hằng ngày bỏ sót DB rời vừa được sao lưu tay,
// vì backup một site nhỏ xong trong cùng giây đó.
func TestDBBackupHaiBanCungGiay(t *testing.T) {
	e := newDBkEnv(t)
	e.add(t, "alice", "shop", "v1")
	e.tick()
	dbsExec(t)(e.d.PrepareBackup(ctx, "alice_shop", ReasonManual))
	// Không tick: lịch chạy trong cùng giây với bản bấm tay.
	res, err := e.d.BackupAll(ctx, ReasonSchedule, nil)
	if err != nil || len(res) != 1 || res[0].Error != "" || res[0].Report.Job.Status != engine.JobSucceeded {
		t.Fatalf("lịch backup trong cùng giây với bản bấm tay: %v %+v", err, res)
	}
	rows := e.rows(t, "alice_shop")
	if len(rows) != 2 {
		t.Fatalf("muốn 2 dòng (bấm tay, theo lịch), có %d: %+v", len(rows), rows)
	}
	reasons := map[BackupReason]string{}
	for _, r := range rows {
		reasons[r.Reason] = r.Path
		if _, err := e.sim.Stat(r.Path); err != nil {
			t.Errorf("file của bản %s không có: %v", r.Reason, err)
		}
		if !r.Created.Equal(e.now) {
			t.Errorf("lúc tạo của bản %s = %v, muốn giờ thật %v (không lấy từ tên file)", r.Reason, r.Created, e.now)
		}
	}
	stamp := func(d time.Duration) string { return e.now.Add(d).UTC().Format(dbStamp) }
	if !strings.Contains(reasons[ReasonManual], stamp(0)) || !strings.Contains(reasons[ReasonSchedule], stamp(time.Second)) {
		t.Errorf("tên file: bấm tay %q, theo lịch %q; muốn giây hiện tại rồi giây kế tiếp", reasons[ReasonManual], reasons[ReasonSchedule])
	}

	// Cả năm giây liên tiếp đều đã có file: lúc này mới từ chối, không ghi đè bản nào.
	e.tick()
	e.tick()
	for i := range dbsStampTries {
		p := site.DBBackupDir("alice_shop") + "/alice_shop-db-" + stamp(time.Duration(i)*time.Second) + ".sql"
		e.sim.SetFile(p, []byte("bản có sẵn "+stamp(time.Duration(i)*time.Second)), 0o600, system.Root)
	}
	if _, err := e.d.PrepareBackup(ctx, "alice_shop", ReasonManual); dbkCode(err) != "backup_exists" {
		t.Errorf("năm giây liền đều có file: %s, muốn backup_exists", dbkCode(err))
	}
	for i := range dbsStampTries {
		p := site.DBBackupDir("alice_shop") + "/alice_shop-db-" + stamp(time.Duration(i)*time.Second) + ".sql"
		if got, _ := e.sim.ReadFile(p); !strings.HasPrefix(string(got), "bản có sẵn") {
			t.Errorf("file có sẵn %s bị ghi đè", p)
		}
	}
}

// M34 nối với M33: DB rời có bản sao lưu bị xoá ngoài tpan. Sao lưu mới và nhập
// lại từ chối kèm đường đi; "xoá" gỡ dòng của DB mà giữ nguyên các bản sao lưu;
// rồi DB tạo lại được từ bản đó, đúng chủ, đúng bộ ký tự, đủ dữ liệu.
func TestDBRoiDaMatGoRoiTaoLaiTuBanSaoLuu(t *testing.T) {
	e := newDBkEnv(t)
	e.add(t, "alice", "shop", "v1")
	bk := e.backup(t, "alice_shop")
	e.sim.mu.Lock()
	delete(e.sim.dbs, "alice_shop")
	delete(e.sim.users, "alice_shop")
	e.sim.mu.Unlock()

	e.tick()
	if _, err := e.d.PrepareBackup(ctx, "alice_shop", ReasonManual); dbkCode(err) != CodeDBMissing {
		t.Errorf("sao lưu DB đã mất: %s, muốn db_missing", dbkCode(err))
	}
	if _, err := e.d.PrepareImport(ctx, "alice_shop", bk.ID); dbkCode(err) != CodeDBMissing {
		t.Errorf("nhập vào DB đã mất: %s, muốn db_missing", dbkCode(err))
	}
	// Lịch backup: DB đã mất báo lỗi ở dòng của nó, không làm hỏng cả lượt.
	res, err := e.d.BackupAll(ctx, ReasonSchedule, nil)
	if err != nil || len(res) != 1 || !strings.Contains(res[0].Error, "không còn trong MariaDB") {
		t.Errorf("lịch backup khi một DB đã mất: %v %+v", err, res)
	}
	// Đang có dòng thì chưa tạo lại được.
	if _, err := e.d.PrepareRecreate(ctx, bk.ID); dbkCode(err) != CodeDBExists {
		t.Errorf("tạo lại khi dòng của DB còn: %s, muốn db_exists", dbkCode(err))
	}

	p, err := e.d.PrepareTrash(ctx, "alice_shop")
	if err != nil || p.Plan.Kind != "dbs.forget" {
		t.Fatalf("xoá DB đã mất: %v", err)
	}
	dbsExec(t)(p, nil)
	if _, ok, _ := e.st.StandaloneDB(ctx, "alice_shop"); ok {
		t.Fatal("dòng của DB còn sau khi gỡ")
	}
	rows := e.rows(t, "alice_shop")
	if len(rows) != 1 || rows[0].ID != bk.ID {
		t.Fatalf("gỡ DB làm mất bản sao lưu: %+v", rows)
	}
	if _, err := e.sim.Stat(bk.Path); err != nil {
		t.Fatalf("file sao lưu mất sau khi gỡ: %v", err)
	}

	e.tick()
	re, err := e.d.PrepareRecreate(ctx, bk.ID)
	if err != nil {
		t.Fatalf("tạo lại từ bản sao lưu sau khi gỡ: %v", err)
	}
	dbsExec(t)(re.Planned, nil)
	db, ok, _ := e.st.StandaloneDB(ctx, "alice_shop")
	if !ok || db.Owner != "alice" || db.Collation != "utf8mb4_bin" || e.data("alice_shop") != "v1" {
		t.Errorf("DB tạo lại = %+v (có=%v), dữ liệu %q", db, ok, e.data("alice_shop"))
	}
	if u := e.sim.users["alice_shop"]; u.pw == "" || u.pw != re.Password.Reveal() {
		t.Error("DB tạo lại phải có user DB với mật khẩu mới trả một lần")
	}
}
