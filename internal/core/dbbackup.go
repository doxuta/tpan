package core

// M33: sao lưu DB rời (docs/thay-doi/M33.md).
//
// Trước M33 DB rời không nằm trong bản sao lưu nào: bản backup của site chỉ có
// mã nguồn và DB của chính site, và xoá hẳn DB rời khỏi thùng rác là xoá luôn
// file xuất cuối cùng. Người dùng backup site, xoá hẳn một DB rời, rồi khôi
// phục bản backup của site thì không thấy DB rời quay lại (báo ngày 26/11/2026).
//
// Bản sao lưu của DB rời là một dòng của bảng backups như bản xuất DB của site
// (Kind sql), chỉ khác cột domain mang TÊN DB. Tên DB rời luôn có "_", domain
// không bao giờ có, nên hai loại không lẫn nhau (site.IsDBBackupName). Nhờ ở
// chung bảng, các thao tác đã có dùng lại được nguyên: xem ở màn Backup, tải
// về, xoá, dọn theo số bản giữ, lọc theo chủ.
//
// Bốn ca sử dụng:
//   - PrepareBackup: xuất DB ra một bản sao lưu (bấm tay hoặc theo lịch).
//   - PrepareImport: nhập một bản sao lưu vào DB đang có (có đường lùi).
//   - PreparePurge (dbs.go): xoá hẳn thì giữ file xuất cuối làm bản sao lưu.
//   - PrepareRecreate: tạo lại DB đã xoá hẳn từ một bản sao lưu.
//
// ponytail: bản sao lưu của DB rời chỉ nằm trên máy. Kho S3 (M18) chỉ nhận bản
// đầy đủ của site (pushBlock), bản xuất DB của site cũng không lên kho.

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/validate"
)

// backupDBCharsetReader là phần tuỳ chọn của store: charset, collation mà dòng
// backup của DB rời nhớ (migration 3300). *store.Store có sẵn.
type backupDBCharsetReader interface {
	BackupDBCharset(ctx context.Context, id string) (charset, collation string, err error)
}

// dbsBackups trả Backups để ghi và dọn dòng. Thiếu là lỗi lập trình hoặc máy
// chưa có tpan.db; khi đó cũng không thể có DB rời nào.
func (d *DBs) dbsBackups() (*Backups, error) {
	if d.Backups == nil || d.Backups.Store == nil || d.Backups.Sites == nil {
		return nil, errors.New("sao lưu DB rời: chưa có bảng backups của tpan")
	}
	return d.Backups, nil
}

// dbsDumpPath là file sao lưu mới của DB rời name. suffix phân biệt bản tự
// xuất (trước khi nhập, trước khi xoá hẳn) với bản người dùng bấm, để hai bản
// trong cùng một giây không trùng tên. File đã có thì từ chối: cột path của
// bảng backups là UNIQUE và MariaDBDump coi file trọn vẹn sẵn có là đã xong.
func (d *DBs) dbsDumpPath(name, suffix string) (string, error) {
	if !databaseNameRe.MatchString(name) {
		return "", dbsNotFound(name)
	}
	p := site.DBBackupDir(name) + "/" + name + "-db-" + d.now().UTC().Format(dbStamp) + suffix + ".sql"
	exists, err := pathExists(d.Sys, p)
	if err != nil {
		return "", err
	}
	if exists {
		return "", refuse("backup_exists", i18n.T(
			p+" đã có (vừa sao lưu trong cùng giây này); chờ một giây rồi làm lại",
			p+" already exists (a backup was taken this same second); wait a second and try again"))
	}
	return p, nil
}

// dbsSpace là chặn đầy đĩa của mục 6.5 cho một bản xuất DB rời, lúc dựng Plan.
// MariaDBDump đo lại lúc Apply.
func (d *DBs) dbsSpace(ctx context.Context, name string) error {
	need, err := steps.MeasureStandaloneDB(ctx, d.Sys, name)
	if err != nil {
		return err
	}
	du, err := dbDiskNear(d.Sys, site.DBBackupDir(name))
	if err != nil {
		return err
	}
	if steps.SpaceOK(du.Free, du.Total, need) {
		return nil
	}
	n, f, t := dbHumanBytes(need), dbHumanBytes(du.Free), dbHumanBytes(du.Total)
	return refuse(CodeDiskSpaceLow, i18n.T(
		"không đủ chỗ trống để sao lưu DB "+name+": cần khoảng "+n+", đĩa còn trống "+f+" trên tổng "+t+". Luật: chỗ trống phải ít nhất gấp 1,2 lần cỡ bản xuất và sau khi ghi vẫn còn 10% đĩa. Xoá bớt bản sao lưu cũ ở màn Backup, hoặc xem: df -h "+site.BackupRoot,
		"not enough free space to back up database "+name+": needs about "+n+", the disk has "+f+" free of "+t+". Rule: free space must be at least 1.2 times the dump and 10% of the disk must stay free afterwards. Delete old backups on the Backup screen, or check: df -h "+site.BackupRoot))
}

// dbsRecord ghi file sao lưu p vừa có vào bảng backups (sha256 và cỡ đọc lại
// từ chính file), rồi dọn bản cũ của DB đó như dọn bản xuất DB của site: giữ
// Keep bản mới nhất, không bao giờ xoá bản trước khi xoá hẳn, bản vừa ghi và
// các id trong protect.
func (d *DBs) dbsRecord(ctx context.Context, name, p string, reason BackupReason, jobID string, protect ...string) (Backup, error) {
	b, err := d.dbsBackups()
	if err != nil {
		return Backup{}, err
	}
	sum, err := dbFileSHA256(ctx, d.Sys, p)
	if err != nil {
		return Backup{}, err
	}
	fi, err := d.Sys.Stat(p)
	if err != nil {
		return Backup{}, fmt.Errorf("đọc cỡ %s: %w", p, err)
	}
	added, err := b.Store.AddBackup(ctx, Backup{
		Domain: name, Kind: BackupSQL, Reason: reason,
		Path: p, Size: fi.Size, SHA256: sum, JobID: jobID, Created: d.now(),
	})
	if err != nil {
		return Backup{}, fmt.Errorf("ghi bản sao lưu %s vào bảng backups (file vẫn còn trên máy): %w", p, err)
	}
	return added, b.prune(ctx, name, append(protect, added.ID)...)
}

// PrepareBackup dựng Plan "dbs.backup": xuất DB rời name ra một bản sao lưu.
// DB đang tạm khoá vẫn sao lưu được (xuất bằng root); DB trong thùng rác thì
// không (DB đã bị xoá, file ở thùng rác là bản duy nhất).
func (d *DBs) PrepareBackup(ctx context.Context, name string, reason BackupReason) (*Planned, error) {
	switch reason {
	case ReasonManual, ReasonSchedule:
	default:
		return nil, fmt.Errorf("sao lưu DB rời %s: lý do %q không dùng cho dbs.backup", name, reason)
	}
	db, err := d.dbsActive(ctx, name)
	if err != nil {
		return nil, err
	}
	if _, err := d.dbsBackups(); err != nil {
		return nil, err
	}
	// Như backup của site: hạn mức dung lượng của gói chỉ chặn bản bấm tay.
	// Bản theo lịch được dọn theo số bản giữ.
	if reason == ReasonManual && d.Accounts != nil {
		if err := checkQuota(ctx, d.Accounts, db.Owner, QuotaBackups, validate.Domain{}); err != nil {
			return nil, err
		}
	}
	if err := d.dbsSpace(ctx, db.Name); err != nil {
		return nil, err
	}
	p, err := d.dbsDumpPath(db.Name, "")
	if err != nil {
		return nil, err
	}
	return newPlanned(d.Sys, d.Journal, engine.Plan{
		Kind:   "dbs.backup",
		Target: db.Name,
		Title:  i18n.T("Sao lưu DB rời "+db.Name+" ra file .sql", "Back up standalone database "+db.Name+" to a .sql file"),
		Steps:  []engine.Step{&steps.MariaDBDump{DB: db.Name, Path: p}},
	}, func(ctx context.Context, rep engine.Report) error {
		_, err := d.dbsRecord(ctx, db.Name, p, reason, rep.Job.ID)
		return err
	}), nil
}

// dbsBackupNotFound: id không trỏ tới một bản sao lưu của DB rời name. id lạ
// không được in lại (do người gõ, có thể mang ký tự điều khiển).
func dbsBackupNotFound(name, id string) *RefusedError {
	if !backupIDRe.MatchString(id) {
		id = "?"
	}
	if !databaseNameRe.MatchString(name) {
		name = "?"
	}
	return refuse("backup_not_found", i18n.T(
		"không có bản sao lưu "+id+" của DB rời "+name+"; xem các bản đang có ở màn Backup",
		"there is no backup "+id+" of standalone database "+name+"; see the existing ones on the Backup screen"))
}

// dbsBackupRow đọc dòng backup id và kiểm nó là bản sao lưu dùng được của DB
// rời name: đúng DB, Kind sql, còn trên máy, nằm đúng thư mục sao lưu của DB
// đó, là file thường và sha256 khớp lúc ghi. Dòng do tpan ghi luôn đạt; khác
// thế là dòng hay file bị sửa tay.
func (d *DBs) dbsBackupRow(ctx context.Context, name, id string) (Backup, error) {
	b, err := d.dbsBackups()
	if err != nil {
		return Backup{}, err
	}
	if !backupIDRe.MatchString(id) || !databaseNameRe.MatchString(name) {
		return Backup{}, dbsBackupNotFound(name, id)
	}
	bk, ok, err := b.Store.Backup(ctx, id)
	if err != nil {
		return Backup{}, err
	}
	if !ok || bk.Domain != name || bk.Kind != BackupSQL || !site.IsDBBackupName(bk.Domain) {
		return Backup{}, dbsBackupNotFound(name, id)
	}
	corrupt := func(vi, en string) *RefusedError {
		return refuse("backup_corrupt", i18n.T(
			"bản sao lưu "+id+" ("+bk.Path+") "+vi+"; không dùng được",
			"backup "+id+" ("+bk.Path+") "+en+"; it cannot be used"))
	}
	if !bk.Local {
		return Backup{}, corrupt("không còn trên máy", "is no longer on this machine")
	}
	if path.Dir(bk.Path) != site.DBBackupDir(name) {
		return Backup{}, corrupt("không nằm trong "+site.DBBackupDir(name), "is not inside "+site.DBBackupDir(name))
	}
	fi, err := d.Sys.Stat(bk.Path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Backup{}, corrupt("không còn trên đĩa", "is no longer on disk")
	case err != nil:
		return Backup{}, fmt.Errorf("kiểm %s: %w", bk.Path, err)
	case !fi.Mode.IsRegular():
		return Backup{}, corrupt("không phải file thường", "is not a regular file")
	}
	sum, err := dbFileSHA256(ctx, d.Sys, bk.Path)
	if err != nil {
		return Backup{}, err
	}
	if sum != bk.SHA256 {
		return Backup{}, corrupt("có sha256 khác lúc sao lưu (file đã bị sửa hoặc hỏng)", "has a different sha256 than when it was taken (the file was changed or damaged)")
	}
	return bk, nil
}

// dbsBackupOwner trả chủ ghi trong dòng backup; store không đọc được (store
// giả của test) thì ok=false.
func (d *DBs) dbsBackupOwner(ctx context.Context, id string) (owner string, ok bool, err error) {
	b, err := d.dbsBackups()
	if err != nil {
		return "", false, err
	}
	or, can := b.Store.(backupOwnerReader)
	if !can {
		return "", false, nil
	}
	return or.BackupOwner(ctx, id)
}

// PrepareImport dựng Plan "dbs.import": đưa DB rời name về đúng nội dung của
// bản sao lưu id. Trước khi nhập, DB hiện tại được xuất ra một bản
// before-import (đường lùi, cũng là thứ Undo nhập lại). User DB và mật khẩu
// giữ nguyên.
func (d *DBs) PrepareImport(ctx context.Context, name, id string) (*Planned, error) {
	db, err := d.dbsActive(ctx, name)
	if err != nil {
		return nil, err
	}
	// Nhập xong ứng dụng phải dùng lại được DB; DB hay chủ đang khoá thì không.
	if db.Suspended != account.NotSuspended {
		return nil, dbsSuspendedErr(db.Name)
	}
	if err := d.dbsOwnerSuspended(ctx, db.Owner); err != nil {
		return nil, err
	}
	bk, err := d.dbsBackupRow(ctx, db.Name, id)
	if err != nil {
		return nil, err
	}
	// Hai chủ khác nhau có thể sinh cùng một tên DB ("a" + "b_c" và "a_b" +
	// "c"): bản sao lưu của chủ cũ không được nhập vào DB của chủ mới.
	if o, ok, err := d.dbsBackupOwner(ctx, bk.ID); err != nil {
		return nil, err
	} else if ok && o != db.Owner {
		return nil, dbsBackupNotFound(db.Name, id)
	}
	if err := d.dbsSpace(ctx, db.Name); err != nil {
		return nil, err
	}
	undo, err := d.dbsDumpPath(db.Name, "-before-import")
	if err != nil {
		return nil, err
	}
	return newPlanned(d.Sys, d.Journal, engine.Plan{
		Kind:   "dbs.import",
		Target: db.Name,
		Title:  i18n.T("Nhập bản sao lưu "+bk.ID+" vào DB rời "+db.Name, "Import backup "+bk.ID+" into standalone database "+db.Name),
		Steps: []engine.Step{
			&steps.MariaDBDump{DB: db.Name, Path: undo},
			&steps.SDBImport{Name: db.Name, Charset: db.Charset, Collation: db.Collation, File: bk.Path, UndoFile: undo},
		},
	}, func(ctx context.Context, rep engine.Report) error {
		// Bản vừa nhập cũng được giữ: nó là trạng thái hiện tại của DB.
		_, err := d.dbsRecord(ctx, db.Name, undo, ReasonBeforeImport, rep.Job.ID, bk.ID)
		return err
	}), nil
}

// PrepareRecreate dựng Plan "dbs.recreate": DB rời đã bị xoá hẳn (không còn
// dòng trong bảng databases) được tạo lại đúng tên cũ, charset cũ, cho chủ cũ,
// rồi nhập bản sao lưu id. User DB cũ đã bị xoá cùng DB nên DB có mật khẩu
// mới (trả ở DBAdd.Password, hiện một lần). Mọi phép kiểm của tạo DB (chủ còn
// và không bị khoá, hạn mức số DB, tên chưa ai dùng) chạy lại như tạo mới.
func (d *DBs) PrepareRecreate(ctx context.Context, id string) (*DBAdd, error) {
	b, err := d.dbsBackups()
	if err != nil {
		return nil, err
	}
	if !backupIDRe.MatchString(id) {
		return nil, dbsBackupNotFound("?", id)
	}
	row, ok, err := b.Store.Backup(ctx, id)
	if err != nil {
		return nil, err
	}
	if !ok || !site.IsDBBackupName(row.Domain) {
		return nil, dbsBackupNotFound("?", id)
	}
	name := row.Domain
	if d.Store != nil {
		if cur, ok, err := d.Store.StandaloneDB(ctx, name); err != nil {
			return nil, fmt.Errorf("đọc DB rời %s: %w", name, err)
		} else if ok && cur.Status == account.DBTrashed {
			return nil, refuse(codeDBTrashed, i18n.T(
				"DB rời "+name+" đang ở thùng rác; khôi phục nó từ thùng rác trước, rồi mới nhập bản sao lưu",
				"standalone database "+name+" is in the trash; restore it from the trash first, then import the backup"))
		} else if ok {
			return nil, refuse(CodeDBExists, i18n.T(
				"DB rời "+name+" đang có; dùng Nhập bản sao lưu thay vì tạo lại",
				"standalone database "+name+" exists; import the backup instead of recreating it"))
		}
	}
	bk, err := d.dbsBackupRow(ctx, name, id)
	if err != nil {
		return nil, err
	}
	owner, ok, err := d.dbsBackupOwner(ctx, bk.ID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("tạo lại DB rời %s: không đọc được chủ của bản sao lưu %s", name, id)
	}
	on, err := validate.ParseUserName(owner)
	short, cut := strings.CutPrefix(name, owner+"_")
	if err != nil || !cut || short == "" {
		// Dòng do tpan ghi luôn có chủ là phần đầu của tên DB.
		return nil, refuse("backup_corrupt", i18n.T(
			"bản sao lưu "+id+" ghi chủ là "+owner+", không khớp tên DB "+name+"; không tạo lại được",
			"backup "+id+" records owner "+owner+", which does not match database name "+name+"; cannot recreate it"))
	}
	var cs, coll string
	if cr, can := b.Store.(backupDBCharsetReader); can {
		if cs, coll, err = cr.BackupDBCharset(ctx, bk.ID); err != nil {
			return nil, err
		}
	}
	add, err := d.PrepareAdd(ctx, DBInput{Owner: on, Name: short, Charset: cs, Collation: coll})
	if err != nil {
		return nil, err
	}
	if add.DB.Name != name {
		return nil, fmt.Errorf("tạo lại DB rời: tên dựng lại %q khác tên trong bản sao lưu %q", add.DB.Name, name)
	}
	add.Plan.Kind = "dbs.recreate"
	add.Plan.Title = i18n.T(
		"Tạo lại DB rời "+name+" cho "+owner+" từ bản sao lưu "+bk.ID,
		"Recreate standalone database "+name+" for "+owner+" from backup "+bk.ID)
	add.Plan.Steps = append(add.Plan.Steps, &steps.SDBLoad{Name: name, File: bk.Path})
	return add, nil
}

// BackupAll sao lưu lần lượt mọi DB rời đang có (không ở thùng rác), mỗi DB
// một job; DB lỗi không chặn DB sau. Dùng cho lịch backup hằng ngày, sau
// Backups.CreateAll.
func (d *DBs) BackupAll(ctx context.Context, reason BackupReason, onEvent func(engine.Event)) ([]BackupResult, error) {
	if reason != ReasonManual && reason != ReasonSchedule {
		return nil, fmt.Errorf("sao lưu mọi DB rời: lý do %q không hợp lệ (manual hoặc schedule)", reason)
	}
	out := []BackupResult{}
	if d.Store == nil {
		return out, nil
	}
	all, err := d.Store.StandaloneDBs(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách DB rời: %w", err)
	}
	for _, db := range all {
		if db.Status != account.DBActive {
			continue
		}
		r := BackupResult{Domain: db.Name}
		p, err := d.PrepareBackup(ctx, db.Name, reason)
		if err == nil {
			r.Report, err = p.Execute(ctx, onEvent)
		}
		if err != nil {
			r.Error = err.Error()
		}
		out = append(out, r)
	}
	return out, nil
}
