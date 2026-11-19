package steps

// Backup và khôi phục (mục 6.5). Luật chung ở m9_types.go.

import "github.com/doxuta/tpan/internal/site"

// BackupArchive tạo một bản backup đầy đủ của site (mục 6.5): một file
// <Dir>/<domain>-<YYYYMMDD-HHMMSS>.tar.gz (giờ UTC LÚC APPLY, không lúc dựng
// Plan) gồm:
//
//	manifest.json  {"domain","wp_version","php_version","created","db_sha256"}
//	db.sql         mariadb-dump --single-transaction --quick <db> (--result-file)
//	public/...     cây Src/public (tar, không -h)
//
// Src là thư mục site: l.Dir khi site active, site.TrashDir(l) khi site ở
// thùng rác. Dir là site.BackupDir(l) (root 0700; BackupRoot 0700;
// /var/lib/tpan giữ 0755).
//
// Apply: đo lại chỗ trống (MeasureBackup, SpaceOK) và báo lỗi nếu không đủ
// (điều kiện có thể đã đổi từ lúc dựng Plan); xuất DB và ghi manifest vào một
// thư mục tạm <Dir>/.tmp-<giờ>/ (0700); `wp core version` dưới user của site
// (lỗi thì WPVersion rỗng, không làm hỏng backup); tar ra <file>.part (0600)
// rồi rename thành tên thật; sha256sum; xoá thư mục tạm. Lỗi giữa chừng: gỡ
// .part và thư mục tạm. GNU tar thoát 1 khi một file đổi trong lúc đọc
// (WordPress đang chạy): chấp nhận, ghi cảnh báo vào output, không coi là lỗi.
// Sau Apply, các trường Path, Size, SHA256, WPVersion có giá trị.
// Check: chỉ true sau khi Apply của chính bước này thành công (backup luôn
// là bản mới). Undo: xoá file đã tạo. Info: Commands có lệnh xuất DB và tar
// với tên file dạng "<domain>-<giờ lúc chạy>.tar.gz" (chữ cố định, để bản xem
// trước dựng hai lần giống hệt nhau).
type BackupArchive struct {
	Layout     site.Layout
	Src        string
	Dir        string
	PHPVersion string

	// Kết quả, có sau Apply.
	Path      string
	Size      int64
	SHA256    string
	WPVersion string
}

// BackupExtract giải nén một bản backup đầy đủ vào Dir = site.RestoreDir(l)
// để chuẩn bị tráo vào site (mục 6.5, khôi phục). Apply:
//  1. sha256sum Archive phải bằng SHA256; khác thì lỗi "backup hỏng".
//  2. `tar -tzvf` (danh sách): chỉ manifest.json, db.sql, public/...; không
//     tuyệt đối, không "..", không thiết bị hay FIFO.
//  3. Dir chưa được có (còn sót từ lần trước: lỗi, nói rõ cách dọn); tạo Dir
//     root 0700 (RestoreRoot 0700); `tar -xzf --no-same-owner
//     --no-same-permissions -C Dir`.
//  4. manifest.json.domain phải bằng Layout.Domain.
//  5. wp-config.php: thay bản trong backup bằng bản đang chạy của site (đọc
//     bằng `cat` dưới user của site, ghi bằng WriteFileAtomic vào Dir, 0600):
//     mật khẩu DB có thể đã đổi sau lúc backup (mục 6.9), và DB import không
//     đổi mật khẩu user DB. Site không có wp-config.php thì giữ bản trong backup.
//  6. Chuẩn hoá trong Dir/public (Dir do root giữ nên không bị tráo):
//     `chown -R -h <user>:www-data`, thư mục 2750, file 640, wp-config.php 600.
//
// Check: Dir/public và Dir/db.sql đã có VÀ bước này đã Apply. Undo: gỡ Dir
// (rm -r --one-file-system, chỉ khi nằm dưới site.RestoreRoot).
type BackupExtract struct {
	Layout  site.Layout
	Archive string
	SHA256  string
	Dir     string
}

// DBImport nhập File (bản xuất của mariadb-dump) vào DB của site sao cho sau
// đó DB chỉ có đúng các bảng của File: xoá rồi tạo lại DB (utf8mb4) trước khi
// nhập; quyền của user DB nằm ở mysql.global_priv, không mất khi xoá DB.
// Lệnh: `mariadb --database=<db>` với câu `source <File>` (đường dẫn do tpan
// sinh, chỉ [A-Za-z0-9_./-]; kiểm trước). Undo: làm y như vậy với UndoFile
// (bản xuất ngay trước khi nhập). Check: chỉ true sau Apply của chính bước.
// File và UndoFile phải nằm dưới site.BackupRoot hoặc site.RestoreRoot.
type DBImport struct {
	Layout   site.Layout
	File     string
	UndoFile string
}
