package steps

// Hợp đồng các Step của M18 (mục 3.2, 3.5, 3.6, 3.8, 3.16 của
// docs/thay-doi/M18-thiet-ke.md). File này (đợt 0) giữ giao diện kho, lỗi có
// tên, đường dẫn bí mật và các struct bước với trường; thân các phương thức
// (Info, Check, Apply, Undo) do thợ đợt 1 viết ở s3.go (S3Probe, S3Push,
// S3Pull, SecretFile, SecretAbsent) và panel.go (PanelBundle), kèm dòng
// `var _ engine.Step = (*X)(nil)` cho từng bước.
//
// Luật chung của các bước M18:
//   - Không bước nào đưa bí mật vào Info (Title, Why, Commands, Files), vào lỗi
//     hay vào engine.Note. Khoá truy cập S3 chỉ nằm trong ObjectStore đã dựng.
//   - Bước nói chuyện với kho không chạy lệnh nào, nên ghi tiến độ bằng
//     engine.Note (mỗi 10% hoặc 30 giây, và một dòng kết quả).
//   - File lớn đọc ghi bằng sys.OpenRead, sys.CreateFile (theo dòng), không
//     bằng ReadFile, WriteFileAtomic.
//   - sha256 của file cục bộ tính bằng lệnh `sha256sum` (sha256Of ở
//     backup.go), để có bằng chứng trong log job.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/s3"
	"github.com/doxuta/tpan/internal/system"
)

// ObjectStore là phần của kho S3 mà các bước dùng. *s3.Client (thợ đợt 1) và
// *s3.Mem (kho giả cho test) thoả giao diện này. Key và prefix là tương đối
// với tiền tố của đích.
type ObjectStore interface {
	Check(ctx context.Context) error
	Put(ctx context.Context, key string, r io.Reader, size int64, progress func(done int64)) (sha256Hex string, err error)
	Get(ctx context.Context, key string, w io.Writer) (n int64, sha256Hex string, err error)
	Head(ctx context.Context, key string) (s3.Object, error)
	Delete(ctx context.Context, key string) error
	List(ctx context.Context, prefix string, each func(s3.Object) error) error
	AbortUploads(ctx context.Context, prefix string, olderThan time.Duration) (int, error)
}

var _ ObjectStore = (*s3.Mem)(nil)

// Lỗi có tên của S3Push. core dùng errors.Is để quyết có tăng remote_tries
// không (mục 3.5): đây là lỗi "của riêng bản", không phải lỗi của đích.
var (
	// ErrBackupLocal: file cục bộ của bản không dùng được (không có, không
	// phải file thường, sai cỡ, sai sha256, đổi trong lúc tải lên).
	ErrBackupLocal = errors.New("bản sao lưu trên máy không dùng được")
	// ErrRemoteMismatch: đọc lại object sau khi tải lên mà sha256 hay cỡ khác
	// thứ đã gửi.
	ErrRemoteMismatch = errors.New("kho trả về dữ liệu khác thứ đã gửi")
)

// Ba file bí mật của M18 (mục 3.2): root:root 0600 trong /etc/tpan (0700).
const (
	S3KeysFile    = "/etc/tpan/s3.json"
	S3CAFile      = "/etc/tpan/s3-ca.pem"
	BackupKeyFile = "/etc/tpan/backup.key"
)

// IsSecretPath: p là một trong ba file bí mật. steps.File và steps.Absent từ
// chối các đường dẫn này (secretGuard); chỉ SecretFile, SecretAbsent được chạm.
func IsSecretPath(p string) bool {
	switch p {
	case S3KeysFile, S3CAFile, BackupKeyFile:
		return true
	}
	return false
}

// secretGuard là chốt chặn của File, Absent: file bí mật mà không phải bước bí
// mật thì từ chối.
func secretGuard(p string, allowed bool) error {
	if IsSecretPath(p) && !allowed {
		return fmt.Errorf("%s là file bí mật; chỉ ghi hay gỡ bằng SecretFile, SecretAbsent", p)
	}
	return nil
}

// SecretFile ghi một file bí mật (root:root 0600). Thân: một *File nội bộ với
// secret=true (cùng cách ghi nguyên tử, hoàn tác trả nội dung cũ hoặc xoá),
// nhưng Info().Files luôn rỗng: nội dung không vào bản xem trước, sha256
// không vào managed_files. ID bước: "secret.file:<Path>". Check so nội dung
// như File (đọc file chỉ để so, không in).
type SecretFile struct {
	Path    string // phải là một đường dẫn của IsSecretPath
	Content system.Secret
	Title   i18n.Text
	Why     i18n.Text

	file *File
}

// SecretAbsent gỡ một file bí mật. Thân: một *Absent nội bộ với secret=true,
// Info().Files luôn rỗng. ID bước: "secret.absent:<Path>". Hoàn tác trả lại
// nội dung cũ.
type SecretAbsent struct {
	Path  string // phải là một đường dẫn của IsSecretPath
	Title i18n.Text
	Why   i18n.Text

	absent *Absent
}

// S3Probe thử thật một đích (mục 3.3 bước 2): ghi object nhỏ ngẫu nhiên
// "probe/<16 hex>", Head, đọc lại và so từng byte, xoá, Head lần nữa phải là
// s3.ErrNotFound. Hỏng ở đâu thì cố xoá object rồi báo lỗi. Check chỉ đúng sau
// Apply của chính bước; Undo không làm gì (NoUndo có lý do: bước tự dọn).
// ID bước: "s3.probe:<Label>".
type S3Probe struct {
	Store ObjectStore
	// Label là "<host>/<bucket>/<tiền tố>" để hiển thị; không có khoá.
	Label string

	applied bool
}

// S3Push tải một file cục bộ lên kho rồi đọc lại (mục 3.5, sáu việc theo thứ
// tự: Stat; sha256sum; AbortUploads(Key, 0) bỏ qua 501; OpenRead + Put, so
// sha256 đã gửi; Get vào io.Discard so sha256 và cỡ; engine.Note). Sai ở việc
// 1, 2, 4: lỗi bọc ErrBackupLocal (việc 4 thì xoá object trước). Sai ở việc 5:
// xoá object, lỗi bọc ErrRemoteMismatch. Lỗi của kho: trả nguyên (*s3.Error)
// để core phân loại. Check chỉ đúng sau Apply của chính bước. Undo: xoá object.
// ID bước: "s3.push:<ID>".
type S3Push struct {
	Store ObjectStore
	// ID là id bản sao lưu (hoặc "panel" cho gói panel): chỉ dùng cho ID bước.
	ID  string
	Key string
	// Path, Size, SHA256 của file cục bộ (cột path, size, sha256 của dòng backups).
	Path   string
	Size   int64
	SHA256 string
	// From khác nil: Path, Size, SHA256 lấy từ kết quả của bước PanelBundle
	// này lúc Apply (gói panel chỉ có tên lúc chạy). Key khi đó là
	// "panel/" + From.Name. Info dùng chữ giữ chỗ PanelNameLater.
	From *PanelBundle

	// Kết quả sau Apply.
	ETag string

	applied bool
}

// S3Pull tải một object về đúng đường dẫn cục bộ của bản sao lưu (mục 3.6):
// Dir là site.BackupDir của domain, Dest nằm ngay dưới Dir. Check THEO TRẠNG
// THÁI: Dest là file thường, cỡ Size, sha256sum bằng SHA256 thì đúng. Apply:
// chặn đầy đĩa (need = Size), tạo Dir (0700 root), xoá Dest+".part" còn sót,
// Dest có mà sai thì xoá, CreateFile(Dest+".part", 0600, root), Get vào đó,
// so số byte và sha256 (sai thì xoá .part, lỗi nói rõ hai sha256), Rename
// sang Dest. Undo: xoá Dest khi chính Apply đã tạo nó. Chỉ chạy dưới khoá
// lượt đẩy (core lo). ID bước: "s3.pull:<ID>".
type S3Pull struct {
	Store  ObjectStore
	ID     string // id bản sao lưu, cho ID bước
	Key    string
	Dest   string
	Dir    string
	Size   int64
	SHA256 string

	created bool
	applied bool
}

// PanelNameLater là chữ giữ chỗ tên gói panel trong Info (tên chỉ biết lúc
// Apply; Info phải giống hệt nhau giữa hai lần dựng Plan, như backupLater).
const PanelNameLater = "tpan-<YYYYMMDD-HHMMSS>.tar.gz.age"

// PanelBundle tạo gói panel đã mã hoá (mục 3.8): thư mục tạm ".tmp-<giờ>"
// (0700) trong Dir; Snapshot(tmp/tpan.db); manifest.json {"format":1,
// "created","hostname","tpan_version","migrations","db_sha256"}; `tar
// --create --gzip` hai file ra tmp/panel.tar.gz; mã hoá theo dòng (OpenRead
// → Encrypt → CreateFile) ra Dir/<tên>.part rồi Rename sang Dir/<tên>; tính
// sha256 file mã; LUÔN xoá thư mục tạm (bản rõ của tpan.db), kể cả khi lỗi.
// Tên: "tpan-" + giờ UTC lúc Apply dạng 20060102-150405 + ".tar.gz.age".
// Check chỉ đúng sau Apply. Undo: xoá file mã. ID bước: "panel.bundle".
type PanelBundle struct {
	Dir string // site.TPANDBBackupDir
	// Snapshot chép tpan.db đang mở ra path (store.SnapshotDB).
	Snapshot func(ctx context.Context, path string) error
	// Migrations trả danh sách migration đã áp (store.AppliedMigrations).
	Migrations func(ctx context.Context) ([]int, error)
	// Encrypt mở luồng mã hoá vào dst (core bọc agebox.Encrypt với khoá đọc
	// từ BackupKeyFile lúc chạy): steps không đọc khoá.
	Encrypt  func(dst io.Writer) (io.WriteCloser, error)
	Hostname string
	Version  string

	// Kết quả sau Apply.
	Path   string
	Name   string
	Size   int64
	SHA256 string

	now     func() time.Time // nil: time.Now; test đặt giờ cố định
	applied bool
}
