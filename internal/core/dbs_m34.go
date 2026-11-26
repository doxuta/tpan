package core

// M34: DB rời có phần bị xoá ngoài tpan (docs/thay-doi/M34.md).
//
// Thử trên máy ảo 26/11/2026 với v0.2.14: một DB rời bị DROP DATABASE bằng tay
// thì không đưa vào thùng rác được (bước xuất dữ liệu lỗi), nên không xoá hẳn
// được, và tài khoản chủ của nó cũng không xoá được. Mất cả user DB thì "tạm
// khoá" vẫn báo thành công, sau đó không mở khoá được, mà đang khoá thì không
// vào thùng rác được: ngõ cụt. Chỉ mất user DB (dữ liệu còn) thì không có cách
// nào tạo lại user.
//
// Cách xử lý, cùng một phép đo (steps.DBNameState) lúc dựng Plan:
//
//	DB mất (user còn hay mất)   "Xoá" thành "gỡ khỏi tpan" (dbs.forget): không
//	                            có gì để đưa vào thùng rác; làm được cả khi đang
//	                            tạm khoá. Các thao tác khác từ chối, chỉ đường.
//	DB còn, user mất            "Đổi mật khẩu" tạo lại user (khoá nếu DB đang
//	                            tạm khoá). Tạm khoá, mở khoá từ chối, chỉ đường.
//	Không hỏi được MariaDB      Giữ đường cũ: Plan chạy và tự báo lỗi của MariaDB.
//
// DB đi kèm một site (tên site.Layout.DB) theo cùng luật, khác ở đường cứu:
//
//	DB mất, user còn            "Khôi phục" site từ một bản backup, hoặc "Nhập"
//	                            một bản xuất DB, tạo lại DB từ file (không có gì
//	                            để tự backup hay tự xuất trước; hoàn tác là xoá
//	                            DB vừa tạo). Các thao tác khác từ chối, chỉ đường.
//	User mất (DB còn hay mất)   "Đổi mật khẩu DB" tạo lại user: site WordPress
//	                            với mật khẩu đang ghi trong wp-config.php, site
//	                            PHP trống với mật khẩu mới hiện một lần. Khoá
//	                            ngay nếu DB của site đang tạm khoá. Khôi phục,
//	                            nhập, tạm khoá, mở khoá từ chối, chỉ sang đó.
//	Cả hai mất                  Đổi mật khẩu DB (có lại user) rồi khôi phục
//	                            hoặc nhập (có lại DB).

import (
	"context"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
)

// Mã từ chối của M34 (409 ở API).
const (
	// CodeDBMissing: DB có trong bảng của tpan nhưng không còn trong MariaDB.
	CodeDBMissing = "db_missing"
	// CodeDBUserMissing: DB còn nhưng user DB cùng tên không còn trong MariaDB.
	CodeDBUserMissing = "db_user_missing"
	// CodeSiteDirMissing: thư mục của site (trong thùng rác) đã bị xoá ngoài tpan.
	CodeSiteDirMissing = "site_dir_missing"
)

// dbsState đọc DB rời name và user DB của nó có trong MariaDB không. known là
// false khi không hỏi được MariaDB (đang dừng, không có Sys): người gọi khi đó
// giữ đường cũ, không được coi là "đã mất".
func (d *DBs) dbsState(ctx context.Context, name string) (dbOK, userOK, known bool) {
	if d.Sys == nil {
		return false, false, false
	}
	dbOK, userOK, err := steps.DBNameState(ctx, d.Sys, name)
	if err != nil {
		return false, false, false
	}
	return dbOK, userOK, true
}

// dbsMissingErr là lời từ chối chung cho mọi thao tác cần DB còn trong MariaDB.
func dbsMissingErr(name string) *RefusedError {
	return refuse(CodeDBMissing, i18n.T(
		"DB rời "+name+" có trong tpan nhưng không còn trong MariaDB (đã bị xoá ngoài tpan). Gỡ nó khỏi tpan bằng: sudo tpan db rm "+name+" (trên giao diện: nút Xoá). Còn bản sao lưu thì sau khi gỡ, tạo lại DB từ bản đó ở màn Backup.",
		"standalone database "+name+" is known to tpan but is no longer in MariaDB (it was dropped outside tpan). Remove it from tpan with: sudo tpan db rm "+name+" (in the web panel: the Delete button). If it has a backup, recreate the database from it on the Backup screen afterwards."))
}

// dbsUserMissingErr: user DB đã mất, DB còn. what là việc đang bị từ chối
// ("khoá", "mở khoá"), để lời báo nói đúng điều người dùng vừa bấm.
func dbsUserMissingErr(name string, what i18n.Text) *RefusedError {
	return refuse(CodeDBUserMissing, i18n.T(
		"user DB "+name+" không còn trong MariaDB (đã bị xoá ngoài tpan) nên không "+what.Vi+" được. Tạo lại user bằng: sudo tpan db passwd "+name+" (trên giao diện: Đổi mật khẩu); dữ liệu trong DB không bị đụng tới.",
		"database user "+name+" is no longer in MariaDB (it was dropped outside tpan), so it cannot be "+what.En+". Recreate the user with: sudo tpan db passwd "+name+" (in the web panel: Change password); the data in the database is not touched."))
}

// dbsPresent từ chối khi biết chắc DB đã mất khỏi MariaDB. Dùng ở đầu các thao
// tác cần DB (đổi charset, sao lưu, nhập, xem dữ liệu) để người dùng nhận một
// lời chỉ đường thay vì lỗi "Unknown database" giữa một job.
func (d *DBs) dbsPresent(ctx context.Context, name string) error {
	if dbOK, _, known := d.dbsState(ctx, name); known && !dbOK {
		return dbsMissingErr(name)
	}
	return nil
}

// prepareForget dựng Plan "dbs.forget": gỡ dòng của một DB rời đã mất khỏi
// MariaDB. Xoá user DB nếu còn (DB đã mất thì user không còn việc gì), xoá file
// xuất sót trong thùng rác nếu có, rồi xoá dòng trong bảng databases. Không
// đụng bảng backups: bản sao lưu của DB (M33) ở lại, DB tạo lại được từ đó.
func (d *DBs) prepareForget(db account.Database, userOK bool) *Planned {
	title := i18n.T(
		"Gỡ DB rời "+db.Name+" khỏi tpan: DB này không còn trong MariaDB (đã bị xoá ngoài tpan) nên không có gì để đưa vào thùng rác",
		"Remove standalone database "+db.Name+" from tpan: it is no longer in MariaDB (dropped outside tpan), so there is nothing to move to the trash")
	if userOK {
		title = i18n.T(
			"Gỡ DB rời "+db.Name+" khỏi tpan và xoá user DB còn sót: DB này không còn trong MariaDB (đã bị xoá ngoài tpan) nên không có gì để đưa vào thùng rác",
			"Remove standalone database "+db.Name+" from tpan and drop its leftover database user: the database is no longer in MariaDB (dropped outside tpan), so there is nothing to move to the trash")
	}
	return newPlanned(d.Sys, d.Journal, engine.Plan{
		Kind:   "dbs.forget",
		Target: db.Name,
		Title:  title,
		Steps:  []engine.Step{&steps.DBUserAbsent{Name: db.Name}, &steps.DBTrashDumpAbsent{Name: db.Name}},
	}, func(ctx context.Context, _ engine.Report) error {
		return d.Store.DeleteStandaloneDB(ctx, db.Name)
	})
}

// DBGoneRefusal cho màn quản lý dữ liệu (M32) một lời từ chối khi DB name
// không còn trong MariaDB; nil khi DB còn hoặc khi không hỏi được MariaDB.
// domain khác rỗng: DB đi kèm site đó; rỗng: DB rời.
//
// Trước M34 màn này mở một DB đã mất như một DB không có bảng nào: người dùng
// tưởng dữ liệu trống chứ không biết DB đã bị xoá.
func DBGoneRefusal(ctx context.Context, sys system.System, name, domain string) *RefusedError {
	if sys == nil || !databaseNameRe.MatchString(name) {
		return nil
	}
	dbOK, _, err := steps.DBNameState(ctx, sys, name)
	if err != nil || dbOK {
		return nil
	}
	if domain == "" {
		return dbsMissingErr(name)
	}
	return siteDBGoneErr(name, domain)
}

// ---------------------------------------------------------------- DB của site

// siteDBState là dbsState cho DB đi kèm một site. known là false khi không
// hỏi được MariaDB: người gọi giữ đường cũ, không coi là "đã mất".
func siteDBState(ctx context.Context, sys system.System, l site.Layout) (dbOK, userOK, known bool) {
	if sys == nil {
		return false, false, false
	}
	dbOK, userOK, err := steps.SiteDBState(ctx, sys, l)
	if err != nil {
		return false, false, false
	}
	return dbOK, userOK, true
}

// siteDBGoneErr: DB của site không còn trong MariaDB. Hai đường có lại dữ
// liệu đều tạo lại DB từ file (Backups.PrepareRestore, PrepareDBImport).
func siteDBGoneErr(name, domain string) *RefusedError {
	return refuse(CodeDBMissing, i18n.T(
		"DB "+name+" của site "+domain+" không còn trong MariaDB (đã bị xoá ngoài tpan). Khôi phục site từ một bản backup ở màn Backup (sudo tpan backup restore "+domain+"), hoặc nhập một bản xuất DB đã có (sudo tpan db import "+domain+" --id ID): cả hai tạo lại DB từ file. Không có bản nào thì chạy lại tạo site (sudo tpan site add "+domain+") để dựng lại một DB trống.",
		"database "+name+" of site "+domain+" is no longer in MariaDB (it was dropped outside tpan). Restore the site from a backup on the Backup screen (sudo tpan backup restore "+domain+"), or import an existing database dump (sudo tpan db import "+domain+" --id ID): both recreate the database from the file. With neither, run the site creation again (sudo tpan site add "+domain+") to rebuild an empty database."))
}

// siteDBPresent từ chối khi biết chắc DB của site đã mất khỏi MariaDB.
func siteDBPresent(ctx context.Context, sys system.System, l site.Layout) error {
	if dbOK, _, known := siteDBState(ctx, sys, l); known && !dbOK {
		return siteDBGoneErr(l.DB, l.Domain.String())
	}
	return nil
}

// siteDBUserGoneErr: user DB của site đã mất. what là việc đang bị từ chối
// ("khoá", "khôi phục site từ backup"...). Đường cứu duy nhất là đổi mật khẩu
// DB của site (recreateSiteDBUser ở dbops.go), làm được cả khi DB cũng mất và
// cả khi site hay DB của site đang tạm khoá.
func siteDBUserGoneErr(l site.Layout, what i18n.Text) *RefusedError {
	d := l.Domain.String()
	return refuse(CodeDBUserMissing, i18n.T(
		"user DB "+l.DB+" của site "+d+" không còn trong MariaDB (đã bị xoá ngoài tpan) nên không "+what.Vi+" được. Tạo lại user trước bằng: sudo tpan db passwd "+d+" (trên giao diện: Đổi mật khẩu DB của site), rồi làm lại việc này.",
		"database user "+l.DB+" of site "+d+" is no longer in MariaDB (it was dropped outside tpan), so the "+what.En+" cannot go ahead. Recreate the user first with: sudo tpan db passwd "+d+" (in the web panel: Change the site's database password), then do this again."))
}

// siteDBUserPresent từ chối khi biết chắc user DB của site đã mất.
func siteDBUserPresent(ctx context.Context, sys system.System, l site.Layout, what i18n.Text) error {
	if _, userOK, known := siteDBState(ctx, sys, l); known && !userOK {
		return siteDBUserGoneErr(l, what)
	}
	return nil
}
