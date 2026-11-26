package core

// DB rời, sửa charset, tạm khoá DB của M13 (mục 6.12 của docs/KE-HOACH.md).
//
// Tên DB rời là "<owner>_<tên>" (validate.StandaloneDBName), cũng là tên user
// DB @'localhost'. Mọi tên đi vào SQL đều được kiểm lại bằng databaseNameRe ở
// đây và bằng dbNameRe trong từng Step (internal/steps/mariadb_m13.go).
//
// Thứ tự đưa DB rời vào thùng rác: khoá user, cắt kết nối, xuất .sql, rồi mới
// DROP. Khoá trước để không ai ghi gì giữa lúc xuất và lúc xoá (mariadb-dump
// chạy bằng root, không bị khoá user ảnh hưởng). Trong thùng rác user DB còn,
// đang khoá, giữ mật khẩu cũ (quyết định 13 ở docs/KE-HOACH.md mục 6.12 ("Đã chốt khi làm M13")).
//
// Tên hàm phụ trong file này có tiền tố dbs: thợ users.go và suspend.go viết
// song song trong cùng gói.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// Mã từ chối riêng của file này (ngoài các Code* chung ở types_m13.go).
const (
	codeDBTrashed     = "db_trashed"      // thao tác không làm được trên DB rời trong thùng rác
	codeDBNotTrashed  = "db_not_trashed"  // khôi phục, xoá hẳn DB rời không ở thùng rác
	codeDBDumpMissing = "db_dump_missing" // file xuất trong thùng rác đã mất
	codeNoteInvalid   = "note_invalid"
)

// dbsDefaultCharset: mục 6.12, "Charset, Collation rỗng là utf8mb4 và collation mặc định của nó".
const dbsDefaultCharset = "utf8mb4"

// dbsNoteMax: ghi chú hiện trong bảng của giao diện; không cần dài hơn.
const dbsNoteMax = 500

func (d *DBs) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func dbsNotFound(name string) *RefusedError {
	if !databaseNameRe.MatchString(name) {
		name = "?" // tên lạ do người gõ: không in lại ký tự điều khiển
	}
	return refuse(CodeDBNotFound, i18n.T(
		"không có DB rời "+name+" trong tpan; xem danh sách bằng: sudo tpan db list",
		"tpan has no standalone database "+name+"; list them with: sudo tpan db list"))
}

// dbsFind đọc DB rời trong bảng databases. Tên sai dạng là "không có" (không
// có DB nào mang tên đó được), không bao giờ tới MariaDB.
func (d *DBs) dbsFind(ctx context.Context, name string) (account.Database, error) {
	if d.Store == nil || !databaseNameRe.MatchString(name) {
		return account.Database{}, dbsNotFound(name)
	}
	db, ok, err := d.Store.StandaloneDB(ctx, name)
	if err != nil {
		return account.Database{}, fmt.Errorf("đọc DB rời %s: %w", name, err)
	}
	if !ok {
		return account.Database{}, dbsNotFound(name)
	}
	return db, nil
}

// dbsActive: DB rời có và không ở thùng rác.
func (d *DBs) dbsActive(ctx context.Context, name string) (account.Database, error) {
	db, err := d.dbsFind(ctx, name)
	if err != nil {
		return db, err
	}
	if db.Status == account.DBTrashed {
		return db, refuse(codeDBTrashed, i18n.T(
			"DB rời "+name+" đang ở thùng rác; khôi phục trước bằng: sudo tpan db restore "+name,
			"standalone database "+name+" is in the trash; restore it first with: sudo tpan db restore "+name))
	}
	return db, nil
}

func dbsSuspendedErr(what string) *RefusedError {
	return refuse(CodeDBSuspended, i18n.T(
		"DB "+what+" đang tạm khoá; mở khoá trước rồi làm lại",
		"database "+what+" is suspended; unsuspend it first and try again"))
}

func dbsNotSuspendedErr(what string) *RefusedError {
	return refuse(CodeDBNotSuspended, i18n.T("DB "+what+" không bị tạm khoá", "database "+what+" is not suspended"))
}

// dbsOwnerSuspended từ chối khi chủ đang bị khoá: mở khoá hay khôi phục DB
// của user bị khoá là mở khoá lén (cùng lý do với quyết định 7 ở docs/KE-HOACH.md mục 6.12).
func (d *DBs) dbsOwnerSuspended(ctx context.Context, owner string) error {
	if d.Accounts == nil {
		return nil
	}
	u, ok, err := d.Accounts.User(ctx, owner)
	if err != nil {
		return fmt.Errorf("đọc user %s: %w", owner, err)
	}
	if ok && u.Status == account.Suspended {
		return refuse(CodeUserSuspended, i18n.T(
			"user "+owner+" đang bị tạm khoá; mở khoá user trước",
			"user "+owner+" is suspended; unsuspend the user first"))
	}
	return nil
}

func checkDBNote(note string) error {
	var vi, en string
	switch {
	case len(note) > dbsNoteMax:
		vi, en = fmt.Sprintf("ghi chú dài quá %d byte", dbsNoteMax), fmt.Sprintf("the note is longer than %d bytes", dbsNoteMax)
	case !utf8.ValidString(note):
		vi, en = "ghi chú không phải UTF-8 hợp lệ", "the note is not valid UTF-8"
	case strings.ContainsFunc(note, func(r rune) bool { return r < 0x20 || (r >= 0x7f && r < 0xa0) }):
		vi, en = "ghi chú có ký tự điều khiển (xuống dòng, tab...)", "the note contains control characters (newline, tab...)"
	default:
		return nil
	}
	return refuse(codeNoteInvalid, i18n.T(vi, en))
}

// ---------------------------------------------------------------- collation

// dbsCollationsSQL: các cặp charset, collation biên dịch sẵn. Từ MariaDB 10.10
// có collation không gắn charset (uca1400_*): bỏ dòng CHARACTER_SET_NAME NULL.
// https://mariadb.com/kb/en/information-schema-collations-table/ (IS_DEFAULT là
// "Yes" hoặc rỗng).
const dbsCollationsSQL = "SELECT CHARACTER_SET_NAME, COLLATION_NAME, IS_DEFAULT FROM information_schema.COLLATIONS " +
	"WHERE CHARACTER_SET_NAME IS NOT NULL ORDER BY CHARACTER_SET_NAME, COLLATION_NAME;\n"

// Collations là các cặp charset, collation MariaDB đang có.
func (d *DBs) Collations(ctx context.Context) ([]Collation, error) {
	res, err := d.Sys.Run(ctx, system.Cmd{Name: "mariadb", Args: []string{"-N", "-B"}, Stdin: []byte(dbsCollationsSQL)})
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách collation của MariaDB: %w", err)
	}
	return parseCollations(res.Stdout), nil
}

// parseCollations đọc output -N -B. Dòng lạ, NULL, hay tên ngoài [a-z0-9_]
// thì bỏ: tên ở đây sau đó đi thẳng vào câu SQL.
func parseCollations(out string) []Collation {
	list := []Collation{}
	for line := range strings.Lines(out) {
		f := strings.Split(strings.TrimRight(line, "\r\n"), "\t")
		if len(f) != 3 || !databaseNameRe.MatchString(f[0]) || !databaseNameRe.MatchString(f[1]) {
			continue
		}
		list = append(list, Collation{Charset: f[0], Collation: f[1], Default: f[2] == "Yes"})
	}
	return list
}

// dbsResolve điền phần rỗng (charset rỗng: utf8mb4, hoặc charset của
// collation đã cho; collation rỗng: mặc định của charset) và đòi cặp có trong
// information_schema.COLLATIONS.
func (d *DBs) dbsResolve(ctx context.Context, charset, collation string) (string, string, error) {
	list, err := d.Collations(ctx)
	if err != nil {
		return "", "", err
	}
	if charset == "" && collation == "" {
		charset = dbsDefaultCharset
	}
	for _, c := range list {
		switch {
		case charset == "" && c.Collation == collation,
			collation == "" && c.Charset == charset && c.Default,
			c.Charset == charset && c.Collation == collation:
			return c.Charset, c.Collation, nil
		}
	}
	show := func(s string) string {
		if !databaseNameRe.MatchString(s) {
			return "?"
		}
		return s
	}
	return "", "", refuse(CodeCollationInvalid, i18n.T(
		"MariaDB không có cặp charset "+show(charset)+" / collation "+show(collation)+"; xem các cặp có sẵn ở màn Cơ sở dữ liệu hoặc GET /api/v1/collations",
		"MariaDB has no charset "+show(charset)+" / collation "+show(collation)+" pair; see the available pairs on the Databases screen or GET /api/v1/collations"))
}

// ---------------------------------------------------------------- tạo

// PrepareAdd dựng Plan tạo DB rời "<owner>_<tên>" cùng user DB trùng tên,
// mật khẩu 32 ký tự ngẫu nhiên. Kiểm hạn mức (QuotaDBs), tên trùng (kể cả DB
// đã có trong MariaDB mà không có trong bảng), cặp charset và collation.
func (d *DBs) PrepareAdd(ctx context.Context, in DBInput) (*DBAdd, error) {
	name, err := validate.StandaloneDBName(in.Owner, in.Name)
	if err != nil {
		return nil, err
	}
	owner := in.Owner.String()
	if err := checkDBNote(in.Note); err != nil {
		return nil, err
	}
	if d.Accounts != nil {
		u, _, err := ownerUser(ctx, d.Accounts, owner)
		if err != nil {
			return nil, err
		}
		if u.Status != account.Active {
			return nil, refuse(CodeUserSuspended, i18n.T("user "+owner+" đang bị tạm khoá", "user "+owner+" is suspended"))
		}
		if err := checkQuota(ctx, d.Accounts, owner, QuotaDBs, validate.Domain{}); err != nil {
			return nil, err
		}
	}
	exists := refuse(CodeDBExists, i18n.T("DB "+name+" đã có", "database "+name+" already exists"))
	// P7 của rà soát v0.2.0: tên đầy đủ của DB rời không được trùng tên một
	// user (kể cả user đang khoá): job trên user có Target là tên user, trùng
	// tên DB thì hai thứ của hai người mang cùng một tên.
	if d.Accounts != nil {
		if _, ok, err := d.Accounts.User(ctx, name); err != nil {
			return nil, fmt.Errorf("đọc user %s: %w", name, err)
		} else if ok {
			return nil, refuse(CodeDBExists, i18n.T(
				"đã có user tên "+name+"; tên DB rời không được trùng tên user, chọn tên khác",
				"a user named "+name+" already exists; a standalone database name cannot match a user name, choose another"))
		}
	}
	if d.Store != nil {
		if _, ok, err := d.Store.StandaloneDB(ctx, name); err != nil {
			return nil, fmt.Errorf("đọc DB rời %s: %w", name, err)
		} else if ok {
			return nil, exists
		}
	}
	if d.Sites != nil {
		all, err := d.Sites.Sites(ctx)
		if err != nil {
			return nil, fmt.Errorf("đọc danh sách site: %w", err)
		}
		for _, st := range all {
			if st.Layout().DB == name {
				return nil, exists
			}
		}
	}
	dbOK, userOK, err := steps.DBNameState(ctx, d.Sys, name)
	if err != nil {
		return nil, err
	}
	if dbOK || userOK {
		return nil, refuse(CodeDBExists, i18n.T(
			"MariaDB đã có DB hoặc user DB tên "+name+" (không do tpan tạo); chọn tên khác hoặc xoá nó bằng tay",
			"MariaDB already has a database or database user named "+name+" (not created by tpan); choose another name or remove it by hand"))
	}
	cs, coll, err := d.dbsResolve(ctx, in.Charset, in.Collation)
	if err != nil {
		return nil, err
	}
	pw, picked, err := pickDBPassword(in.Password, name)
	if err != nil {
		return nil, err
	}
	db := account.Database{
		Name: name, Owner: owner, Charset: cs, Collation: coll, Note: in.Note,
		Status: account.DBActive, Created: d.now().UTC(),
	}
	p := newPlanned(d.Sys, d.Journal, engine.Plan{
		Kind:   "dbs.add",
		Target: name,
		Title:  i18n.T("Tạo DB rời "+name+" cho "+owner, "Create standalone database "+name+" for "+owner),
		Steps:  []engine.Step{&steps.StandaloneDB{Name: name, Charset: cs, Collation: coll, Password: pw}},
	}, func(ctx context.Context, rep engine.Report) error {
		db.Created = d.now().UTC()
		return d.dbsSave(ctx, db)
	})
	p.owner = owner // dòng DB rời chỉ có sau job: store chưa suy được chủ từ Target
	return &DBAdd{Planned: p, DB: db, Password: secretUnlessChosen(pw, picked), Chosen: picked}, nil
}

func (d *DBs) dbsSave(ctx context.Context, db account.Database) error {
	if d.Store == nil {
		return fmt.Errorf("ghi DB rời %s: chưa có DB của tpan", db.Name)
	}
	if err := d.Store.SaveStandaloneDB(ctx, db); err != nil {
		return fmt.Errorf("ghi DB rời %s vào bảng databases: %w", db.Name, err)
	}
	return nil
}

// dbsUpdate đọc lại dòng (có thể đã đổi từ lúc dựng Plan) rồi sửa bằng fn.
func (d *DBs) dbsUpdate(ctx context.Context, name string, fn func(*account.Database)) error {
	db, err := d.dbsFind(ctx, name)
	if err != nil {
		return err
	}
	fn(&db)
	return d.dbsSave(ctx, db)
}

// ---------------------------------------------------------------- sửa

// PrepareCharset đổi charset và collation của DB rời (ALTER DATABASE).
func (d *DBs) PrepareCharset(ctx context.Context, name, charset, collation string) (*Planned, error) {
	db, err := d.dbsActive(ctx, name)
	if err != nil {
		return nil, err
	}
	if err := d.dbsPresent(ctx, db.Name); err != nil {
		return nil, err
	}
	cs, coll, err := d.dbsResolve(ctx, charset, collation)
	if err != nil {
		return nil, err
	}
	return newPlanned(d.Sys, d.Journal, engine.Plan{
		Kind:   "dbs.charset",
		Target: db.Name,
		Title:  i18n.T("Đổi charset của DB rời "+db.Name+" thành "+coll, "Change the charset of standalone database "+db.Name+" to "+coll),
		Steps:  []engine.Step{&steps.DBCharset{Name: db.Name, Charset: cs, Collation: coll}},
	}, func(ctx context.Context, _ engine.Report) error {
		return d.dbsUpdate(ctx, db.Name, func(x *account.Database) { x.Charset, x.Collation = cs, coll })
	}), nil
}

// SetNote ghi chú cho DB rời; không đụng máy nên không có Plan.
func (d *DBs) SetNote(ctx context.Context, name, note string) error {
	if err := checkDBNote(note); err != nil {
		return err
	}
	if _, err := d.dbsFind(ctx, name); err != nil {
		return err
	}
	return d.dbsUpdate(ctx, name, func(x *account.Database) { x.Note = note })
}

// PreparePassword đổi mật khẩu user DB của DB rời (chỉ trong MariaDB). Được
// làm cả khi DB đang khoá (khoá giữ nguyên); không làm được khi ở thùng rác.
//
// M34: user DB đã bị xoá ngoài tpan mà DB còn thì đây là đường tạo lại user
// (mật khẩu mới, khoá nếu DB đang tạm khoá). DB cũng mất thì từ chối, chỉ đường gỡ.
func (d *DBs) PreparePassword(ctx context.Context, name string) (*DBPassword, error) {
	return d.PreparePasswordWith(ctx, name, "")
}

// PreparePasswordWith là PreparePassword với mật khẩu do người dùng đặt (M36);
// chosen rỗng thì tpan sinh. Mật khẩu tự đặt không được trả lại.
func (d *DBs) PreparePasswordWith(ctx context.Context, name, chosen string) (*DBPassword, error) {
	db, err := d.dbsActive(ctx, name)
	if err != nil {
		return nil, err
	}
	dbOK, userOK, known := d.dbsState(ctx, db.Name)
	if known && !dbOK {
		return nil, dbsMissingErr(db.Name)
	}
	pw, picked, err := pickDBPassword(chosen, db.Name)
	if err != nil {
		return nil, err
	}
	plan := engine.Plan{
		Kind:   "dbs.passwd",
		Target: db.Name,
		Title:  i18n.T("Đổi mật khẩu user DB "+db.Name, "Change the password of database user "+db.Name),
		Steps:  []engine.Step{&steps.DBUserPassword{Name: db.Name, Password: pw}},
	}
	if known && !userOK {
		plan.Title = i18n.T(
			"Tạo lại user DB "+db.Name+" với mật khẩu mới (user đã bị xoá ngoài tpan; dữ liệu trong DB giữ nguyên)",
			"Recreate the database user "+db.Name+" with a new password (the user was dropped outside tpan; the data in the database is kept)")
		plan.Steps = []engine.Step{&steps.DBUserRecreate{Name: db.Name, Password: pw, Locked: db.Suspended != account.NotSuspended}}
	}
	return &DBPassword{Planned: newPlanned(d.Sys, d.Journal, plan, nil), Name: db.Name, Password: secretUnlessChosen(pw, picked), Chosen: picked}, nil
}

// ---------------------------------------------------------------- tạm khoá

// dbLockSteps là các bước khoá user DB user@localhost (ACCOUNT LOCK, KILL kết
// nối đang mở), dùng chung cho DB rời, DB của site và khoá user.
func dbLockSteps(user string) []engine.Step {
	return []engine.Step{&steps.DBAccountLock{Name: user}, &steps.DBKillConnections{Name: user}}
}

// dbUnlockSteps là các bước mở khoá user DB.
func dbUnlockSteps(user string) []engine.Step {
	return []engine.Step{&steps.DBAccountUnlock{Name: user}}
}

// PrepareSuspend khoá DB rời: ACCOUNT LOCK rồi KILL các kết nối đang mở của user DB.
func (d *DBs) PrepareSuspend(ctx context.Context, name string) (*Planned, error) {
	db, err := d.dbsActive(ctx, name)
	if err != nil {
		return nil, err
	}
	// Đang khoá vì user thì vẫn cho: chuyển nguồn khoá sang "self", để mở khoá
	// user sau này không mở DB mà admin muốn giữ khoá.
	if db.Suspended == account.SuspendSelf {
		return nil, dbsSuspendedErr(db.Name)
	}
	// M34: user DB không còn thì bước khoá coi là "đã đúng" và job báo thành
	// công dù không khoá được gì; sau đó DB mang cờ tạm khoá mà không mở lại
	// được. Từ chối ngay, chỉ đường theo thứ còn lại.
	if dbOK, userOK, known := d.dbsState(ctx, db.Name); known && !dbOK {
		return nil, dbsMissingErr(db.Name)
	} else if known && !userOK {
		return nil, dbsUserMissingErr(db.Name, i18n.T("khoá", "locked"))
	}
	return newPlanned(d.Sys, d.Journal, engine.Plan{
		Kind:   "dbs.suspend",
		Target: db.Name,
		Title: i18n.T(
			"Tạm khoá DB rời "+db.Name+": ứng dụng đang dùng nó mất kết nối và báo lỗi kết nối DB cho tới khi mở khoá",
			"Suspend standalone database "+db.Name+": applications using it lose their connection and get database connection errors until it is unsuspended"),
		Steps: dbLockSteps(db.Name),
	}, func(ctx context.Context, _ engine.Report) error {
		now := d.now().UTC()
		return d.dbsUpdate(ctx, db.Name, func(x *account.Database) { x.Suspended, x.SuspendedAt = account.SuspendSelf, now })
	}), nil
}

// PrepareUnsuspend mở khoá DB rời (ACCOUNT UNLOCK).
func (d *DBs) PrepareUnsuspend(ctx context.Context, name string) (*Planned, error) {
	db, err := d.dbsFind(ctx, name)
	if err != nil {
		return nil, err
	}
	// DB trong thùng rác giữ user DB khoá (quyết định 13); mở ở đây là mở lén.
	if db.Status == account.DBTrashed {
		return nil, refuse(codeDBTrashed, i18n.T(
			"DB rời "+db.Name+" đang ở thùng rác; user DB của nó giữ khoá cho tới khi khôi phục",
			"standalone database "+db.Name+" is in the trash; its user stays locked until it is restored"))
	}
	if db.Suspended == account.NotSuspended {
		return nil, dbsNotSuspendedErr(db.Name)
	}
	if db.Suspended == account.SuspendByUser {
		if err := d.dbsOwnerSuspended(ctx, db.Owner); err != nil {
			return nil, err
		}
	}
	// M34: không có user DB thì không có gì để mở khoá; trước đây job chạy rồi
	// hỏng ở bước mở khoá mà không nói phải làm gì tiếp.
	if dbOK, userOK, known := d.dbsState(ctx, db.Name); known && !dbOK {
		return nil, dbsMissingErr(db.Name)
	} else if known && !userOK {
		return nil, dbsUserMissingErr(db.Name, i18n.T("mở khoá", "unlocked"))
	}
	return newPlanned(d.Sys, d.Journal, engine.Plan{
		Kind:   "dbs.unsuspend",
		Target: db.Name,
		Title:  i18n.T("Mở khoá DB rời "+db.Name, "Unsuspend standalone database "+db.Name),
		Steps:  dbUnlockSteps(db.Name),
	}, func(ctx context.Context, _ engine.Report) error {
		return d.dbsUpdate(ctx, db.Name, func(x *account.Database) { x.Suspended, x.SuspendedAt = account.NotSuspended, time.Time{} })
	}), nil
}

// ---------------------------------------------------------------- thùng rác

func (d *DBs) dbsTrashDays(ctx context.Context) (int, error) {
	if d.Settings == nil {
		return DefaultTrashDays, nil
	}
	return (&Sites{Settings: d.Settings}).TrashDays(ctx)
}

// PrepareTrash xuất DB rời ra account.DBTrashDump(name), DROP DATABASE, khoá
// user DB; status trashed, purge_at theo trash.days. Từ chối DB đang khoá.
//
// M34: DB không còn trong MariaDB thì không có gì để xuất hay khôi phục, nên
// "xoá" là gỡ hẳn dòng của nó (Plan dbs.forget, xem prepareForget). Làm được cả
// khi DB đang tạm khoá: lý do từ chối DB đang khoá là khôi phục sẽ mở khoá lén,
// mà ở đây không có gì để khôi phục.
func (d *DBs) PrepareTrash(ctx context.Context, name string) (*Planned, error) {
	db, err := d.dbsActive(ctx, name)
	if err != nil {
		return nil, err
	}
	if dbOK, userOK, known := d.dbsState(ctx, db.Name); known && !dbOK {
		return d.prepareForget(db, userOK), nil
	}
	// Quyết định 7 ở docs/KE-HOACH.md mục 6.12: khôi phục mở khoá user DB, tức là mở khoá lén.
	if db.Suspended != account.NotSuspended {
		return nil, dbsSuspendedErr(db.Name)
	}
	days, err := d.dbsTrashDays(ctx)
	if err != nil {
		return nil, err
	}
	st := append(dbLockSteps(db.Name), &steps.DBTrashDump{Name: db.Name}, &steps.DBDrop{Name: db.Name})
	return newPlanned(d.Sys, d.Journal, engine.Plan{
		Kind:   "dbs.trash",
		Target: db.Name,
		Title: i18n.T(
			fmt.Sprintf("Đưa DB rời %s vào thùng rác (giữ %d ngày rồi xoá hẳn)", db.Name, days),
			fmt.Sprintf("Move standalone database %s to the trash (kept %d days, then purged)", db.Name, days)),
		Steps: st,
	}, func(ctx context.Context, _ engine.Report) error {
		now := d.now().UTC()
		return d.dbsUpdate(ctx, db.Name, func(x *account.Database) {
			x.Status, x.TrashedAt, x.PurgeAt = account.DBTrashed, now, now.AddDate(0, 0, days)
		})
	}), nil
}

// dbsTrashed: DB rời có và đang ở thùng rác.
func (d *DBs) dbsTrashed(ctx context.Context, name string) (account.Database, error) {
	db, err := d.dbsFind(ctx, name)
	if err != nil {
		return db, err
	}
	if db.Status != account.DBTrashed {
		return db, refuse(codeDBNotTrashed, i18n.T(
			"DB rời "+name+" không ở thùng rác; đưa vào thùng rác trước bằng: sudo tpan db rm "+name,
			"standalone database "+name+" is not in the trash; move it there first with: sudo tpan db rm "+name))
	}
	return db, nil
}

// PrepareRestore tạo lại DB, nhập bản xuất trong thùng rác, mở khoá user DB.
func (d *DBs) PrepareRestore(ctx context.Context, name string) (*Planned, error) {
	db, err := d.dbsTrashed(ctx, name)
	if err != nil {
		return nil, err
	}
	if err := d.dbsOwnerSuspended(ctx, db.Owner); err != nil {
		return nil, err
	}
	dump := account.DBTrashDump(db.Name)
	ok, err := pathExists(d.Sys, dump)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, refuse(codeDBDumpMissing, i18n.T(
			"file xuất "+dump+" của DB rời "+db.Name+" không còn; không khôi phục được, chỉ xoá hẳn được",
			"the dump "+dump+" of standalone database "+db.Name+" is gone; it cannot be restored, only purged"))
	}
	dbOK, userOK, err := steps.DBNameState(ctx, d.Sys, db.Name)
	if err != nil {
		return nil, err
	}
	if dbOK {
		return nil, refuse(CodeDBExists, i18n.T(
			"MariaDB đã có DB "+db.Name+" (tạo bằng tay sau khi xoá); tpan không nhập đè",
			"MariaDB already has a database "+db.Name+" (created by hand after deletion); tpan does not import over it"))
	}
	// M34: user DB (giữ khoá trong lúc ở thùng rác) đã bị xoá ngoài tpan thì
	// bước mở khoá cuối Plan sẽ hỏng và cả job bị hoàn tác. Đường còn lại là
	// xoá hẳn (bản xuất cuối thành một bản sao lưu) rồi tạo lại DB từ bản đó.
	if !userOK {
		return nil, refuse(CodeDBUserMissing, i18n.T(
			"user DB "+db.Name+" không còn trong MariaDB (đã bị xoá ngoài tpan) nên DB này không khôi phục lại như cũ được. Xoá hẳn nó (tpan giữ bản xuất cuối làm bản sao lưu), rồi tạo lại DB từ bản đó ở màn Backup: DB tạo lại có user và mật khẩu mới, dữ liệu như lúc vào thùng rác.",
			"database user "+db.Name+" is no longer in MariaDB (dropped outside tpan), so this database cannot be restored as it was. Purge it (tpan keeps the last dump as a backup), then recreate the database from that backup on the Backup screen: it gets a new user and password, with the data as it was when trashed."))
	}
	st := []engine.Step{&steps.DBRestore{Name: db.Name, Charset: db.Charset, Collation: db.Collation}}
	st = append(st, dbUnlockSteps(db.Name)...)
	// D10 của soát cuối đợt 2: DB rời vào thùng rác trước khi máy có bản vá
	// M1-DBCONN thì user của nó (giữ lại trong thùng rác) không có trần, và
	// harden không đi qua DB trong thùng rác. Đặt trần lúc khôi phục, như
	// khôi phục site (S16). Trần đã có thì bước được bỏ qua.
	st = append(st, &steps.DBUserLimit{User: db.Name, Max: site.DBMaxUserConnections})
	st = append(st, &steps.DBTrashDumpAbsent{Name: db.Name, Restored: true})
	return newPlanned(d.Sys, d.Journal, engine.Plan{
		Kind:   "dbs.restore",
		Target: db.Name,
		Title:  i18n.T("Khôi phục DB rời "+db.Name+" từ thùng rác", "Restore standalone database "+db.Name+" from the trash"),
		Steps:  st,
	}, func(ctx context.Context, _ engine.Report) error {
		return d.dbsUpdate(ctx, db.Name, func(x *account.Database) {
			x.Status, x.TrashedAt, x.PurgeAt = account.DBActive, time.Time{}, time.Time{}
		})
	}), nil
}

// PreparePurge xoá hẳn DB rời trong thùng rác: DROP USER, xoá dòng.
//
// M33: file xuất ở thùng rác là bản dữ liệu cuối cùng. Có bảng backups
// (d.Backups) và file còn thì nó được chuyển sang thư mục sao lưu và ghi thành
// một bản before-purge (như bản tự backup trước khi xoá hẳn site): DB tạo lại
// được từ màn Backup cho tới khi người dùng tự xoá bản đó. Dòng backup ghi
// TRƯỚC khi xoá dòng của DB, vì store chép chủ và charset từ dòng DB sang.
// Không có bảng backups, hoặc file đã mất hay không trọn vẹn, thì xoá hẳn như
// trước M33.
func (d *DBs) PreparePurge(ctx context.Context, name string) (*Planned, error) {
	db, err := d.dbsTrashed(ctx, name)
	if err != nil {
		return nil, err
	}
	keep := ""
	if _, berr := d.dbsBackups(); berr == nil {
		// Chỉ giữ bản xuất trọn vẹn: file dở không khôi phục được gì, và giữ
		// nó thì người dùng tưởng mình còn một bản sao lưu.
		whole, err := steps.StandaloneTrashDumpComplete(ctx, d.Sys, db.Name)
		if err != nil {
			return nil, err
		}
		if whole {
			if keep, err = d.dbsDumpPath(db.Name, "-before-purge"); err != nil {
				return nil, err
			}
		}
	}
	plan := engine.Plan{
		Kind:   "dbs.purge",
		Target: db.Name,
		Title:  i18n.T("Xoá hẳn DB rời "+db.Name+" (không lấy lại được)", "Purge standalone database "+db.Name+" (cannot be undone)"),
		Steps:  []engine.Step{&steps.DBUserAbsent{Name: db.Name}, &steps.DBTrashDumpAbsent{Name: db.Name}},
	}
	if keep != "" {
		plan.Title = i18n.T(
			"Xoá hẳn DB rời "+db.Name+" (giữ bản xuất cuối làm bản sao lưu)",
			"Purge standalone database "+db.Name+" (the last dump is kept as a backup)")
		// Chuyển file trước (hoàn tác được), xoá user sau (không hoàn tác được).
		plan.Steps = []engine.Step{&steps.SDBKeepDump{Name: db.Name, To: keep}, &steps.DBUserAbsent{Name: db.Name}}
	}
	return newPlanned(d.Sys, d.Journal, plan, func(ctx context.Context, rep engine.Report) error {
		var errs []error
		if keep != "" {
			if _, err := d.dbsRecord(ctx, db.Name, keep, ReasonBeforePurge, rep.Job.ID); err != nil {
				errs = append(errs, fmt.Errorf("ghi bản sao lưu cuối của DB rời %s (file %s vẫn còn): %w", db.Name, keep, err))
			}
		}
		if err := d.Store.DeleteStandaloneDB(ctx, db.Name); err != nil {
			errs = append(errs, fmt.Errorf("xoá dòng DB rời %s: %w", db.Name, err))
		}
		return errors.Join(errs...)
	}), nil
}

// PurgeExpired xoá hẳn mọi DB rời trong thùng rác đã quá purge_at; trả tên đã
// xoá. Một DB lỗi không chặn các DB sau; lỗi gom lại trả cùng danh sách. DB
// không có hạn (purge_at rỗng) không bao giờ tự bị xoá, như site (M6).
func (d *DBs) PurgeExpired(ctx context.Context, now time.Time, onEvent func(engine.Event)) ([]string, error) {
	if d.Store == nil {
		return []string{}, nil
	}
	all, err := d.Store.StandaloneDBs(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách DB rời: %w", err)
	}
	done := []string{}
	var errs []error
	for _, db := range all {
		if db.Status != account.DBTrashed || db.PurgeAt.IsZero() || db.PurgeAt.After(now) {
			continue
		}
		p, err := d.PreparePurge(ctx, db.Name)
		if err == nil {
			var rep engine.Report
			rep, err = p.Execute(ctx, onEvent)
			if err == nil && rep.Job.Status != engine.JobSucceeded {
				err = fmt.Errorf("job %s: %s", rep.Job.ID, rep.Job.Error)
			}
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("xoá hẳn DB rời %s: %w", db.Name, err))
			continue
		}
		done = append(done, db.Name)
	}
	return done, errors.Join(errs...)
}

// ---------------------------------------------------------------- DB của site

// dbsSite đọc site đang active (DB của site trong thùng rác đã bị khoá theo site).
func (d *DBs) dbsSite(ctx context.Context, dom validate.Domain) (site.Site, error) {
	if d.Sites == nil {
		return site.Site{}, errors.New("thiếu bảng sites")
	}
	st, err := (&Sites{Sys: d.Sys, Store: d.Sites}).find(ctx, dom)
	if err != nil {
		return st, err
	}
	if st.Status != site.Active {
		return st, refuse("site_trashed", i18n.T(
			"site "+dom.String()+" đang ở thùng rác; khôi phục trước bằng: sudo tpan site restore "+dom.String(),
			"site "+dom.String()+" is in the trash; restore it first with: sudo tpan site restore "+dom.String()))
	}
	return st, nil
}

// dbsSetSiteDB đọc lại site rồi ghi DBSuspended (các trường khác có thể đã
// đổi từ lúc dựng Plan).
func (d *DBs) dbsSetSiteDB(ctx context.Context, dom validate.Domain, s account.Suspension) error {
	st, ok, err := d.Sites.Site(ctx, dom)
	if err != nil {
		return fmt.Errorf("đọc site %s: %w", dom, err)
	}
	if !ok {
		return fmt.Errorf("site %s không còn trong bảng sites", dom)
	}
	st.DBSuspended = s
	if err := d.Sites.SaveSite(ctx, st); err != nil {
		return fmt.Errorf("ghi trạng thái khoá DB của site %s: %w", dom, err)
	}
	return nil
}

// PrepareSiteDBSuspend khoá user DB của site (DBSuspended = SuspendSelf).
func (d *DBs) PrepareSiteDBSuspend(ctx context.Context, dom validate.Domain) (*Planned, error) {
	st, err := d.dbsSite(ctx, dom)
	if err != nil {
		return nil, err
	}
	if st.DBSuspended == account.SuspendSelf {
		return nil, dbsSuspendedErr("của site " + dom.String())
	}
	l := st.Layout()
	// M34: user DB không còn thì bước khoá coi là "đã đúng" và job báo thành
	// công dù không khoá được gì (như DB rời ở PrepareSuspend).
	if err := siteDBUserPresent(ctx, d.Sys, l, i18n.T("khoá", "lock")); err != nil {
		return nil, err
	}
	return newPlanned(d.Sys, d.Journal, engine.Plan{
		Kind:   "db.suspend",
		Target: dom.String(),
		Title: i18n.T(
			"Tạm khoá DB của site "+dom.String()+": site sẽ báo lỗi kết nối DB (\"Error establishing a database connection\") cho tới khi mở khoá",
			"Suspend the database of "+dom.String()+": the site will show a database connection error (\"Error establishing a database connection\") until it is unsuspended"),
		Steps: dbLockSteps(l.DB),
	}, func(ctx context.Context, _ engine.Report) error {
		return d.dbsSetSiteDB(ctx, dom, account.SuspendSelf)
	}), nil
}

// PrepareSiteDBUnsuspend mở khoá user DB của site. Site không tạm khoá thì
// kiểm chứng như sau đổi mật khẩu DB: đăng nhập DB được và site trả 200.
func (d *DBs) PrepareSiteDBUnsuspend(ctx context.Context, dom validate.Domain) (*Planned, error) {
	st, err := d.dbsSite(ctx, dom)
	if err != nil {
		return nil, err
	}
	switch st.DBSuspended {
	case account.NotSuspended:
		return nil, dbsNotSuspendedErr("của site " + dom.String())
	case account.SuspendByUser:
		if err := d.dbsOwnerSuspended(ctx, st.Owner); err != nil {
			return nil, err
		}
	}
	// M34: không có user DB thì không có gì để mở khoá. DB đã mất (user còn)
	// thì vẫn mở khoá được, chỉ bỏ kiểm chứng "site chạy lại": site chưa thể
	// chạy cho tới khi DB được khôi phục, mà khôi phục lại cần user đã mở khoá.
	dbOK, userOK, known := siteDBState(ctx, d.Sys, st.Layout())
	if known && !userOK {
		return nil, siteDBUserGoneErr(st.Layout(), i18n.T("mở khoá", "unlock"))
	}
	var verify []checks.Check
	if dbGone := known && !dbOK; st.Suspended == account.NotSuspended && !dbGone {
		verify = dbVerify(st)
	}
	return newPlanned(d.Sys, d.Journal, engine.Plan{
		Kind:   "db.unsuspend",
		Target: dom.String(),
		Title:  i18n.T("Mở khoá DB của site "+dom.String(), "Unsuspend the database of "+dom.String()),
		Steps:  dbUnlockSteps(st.Layout().DB),
		Verify: verify,
	}, func(ctx context.Context, _ engine.Report) error {
		return d.dbsSetSiteDB(ctx, dom, account.NotSuspended)
	}), nil
}

// PrepareSiteDBCharset đổi charset và collation của DB của site.
func (d *DBs) PrepareSiteDBCharset(ctx context.Context, dom validate.Domain, charset, collation string) (*Planned, error) {
	st, err := d.dbsSite(ctx, dom)
	if err != nil {
		return nil, err
	}
	cs, coll, err := d.dbsResolve(ctx, charset, collation)
	if err != nil {
		return nil, err
	}
	// M34: DB đã bị xoá ngoài tpan thì không có gì để đổi charset.
	if err := siteDBPresent(ctx, d.Sys, st.Layout()); err != nil {
		return nil, err
	}
	return newPlanned(d.Sys, d.Journal, engine.Plan{
		Kind:   "db.charset",
		Target: dom.String(),
		Title:  i18n.T("Đổi charset DB của site "+dom.String()+" thành "+coll, "Change the database charset of "+dom.String()+" to "+coll),
		Steps:  []engine.Step{&steps.DBCharset{Name: st.Layout().DB, Charset: cs, Collation: coll}},
	}, nil), nil
}
