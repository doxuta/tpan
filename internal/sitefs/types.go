// Package sitefs là cửa duy nhất để tpan (chạy bằng root) chạm vào "vùng
// site": mọi đường dẫn nằm hẳn bên dưới /var/www/<tên>/. Thư mục
// /var/www/<tên> (gọi là "neo") do user Linux của site làm chủ, nên user đó
// tạo, đổi tên, xoá, tráo được mọi thứ bên trong, kể cả biến một file hay một
// thư mục thành symlink, bất kỳ lúc nào. Root đọc, ghi, đổi quyền theo đường
// dẫn trong đó là đi theo đường do người khác vẽ (lỗ T1 của rà soát v0.2.0).
//
// Đặc tả: docs/thay-doi/M28-thiet-ke.md, mục 3.1 (vùng site) và 3.2 (luật C1
// đến C10). Tóm tắt luật:
//
//   - Gốc (Root, hay SiteRoot của LocalSystem) phải thuộc chính tiến trình
//     đang chạy và nhóm, người khác không ghi được; kiểm ở mỗi lời gọi bằng
//     fstat trên fd của gốc, sai thì ErrRootUnsafe. Neo mở bằng
//     openat(fd của gốc, tên neo, O_DIRECTORY|O_NOFOLLOW).
//   - Không đi theo symlink ở bất kỳ thành phần nào, kể cả neo: openat
//     (O_NOFOLLOW) từng thành phần.
//   - Tên cuối chỉ được thao tác qua fd đã mở (fstat, fchmod, fchown, read)
//     hoặc lời gọi *at trên fd của thư mục cha. Không hàm nào đưa lại một
//     đường dẫn đầy đủ cho os.*.
//   - Đọc: chỉ file thường, chủ là user của site (uid của neo; neo thuộc root
//     thì bỏ luật này), cỡ ≤ trần. Kiểm trên chính fd đã mở. Không bao giờ
//     mở thiết bị, FIFO, socket; mọi open có O_NONBLOCK nên không treo.
//   - Đổi chủ, đổi quyền: chỉ thư mục, hoặc file thường có đúng một tên, và
//     chỉ qua fd đã mở (không bao giờ fchmodat, fchownat theo tên: fchmodat
//     của Linux đi theo symlink).
//   - Symlink root tạo trong vùng được đổi chủ về uid, gid của neo
//     (fchownat AT_SYMLINK_NOFOLLOW).
//   - Bit quyền đổi hai chiều như gói os: fs.ModeSetgid ↔ 02000, setuid ↔
//     04000, sticky ↔ 01000 (syscallMode); Info.Mode dựng từ Stat_t như
//     fillFileStatFromSys (loại file, ba bit đặc biệt).
//   - Lỗi không bao giờ chứa nội dung file hay đích của symlink.
//
// Gói lá: chỉ import thư viện chuẩn và golang.org/x/sys/unix (các lời gọi *at
// có trên cả Linux lẫn macOS; unit test của repo chạy trên Mac). Không import
// gói nào của tpan. system.LocalSystem gọi các hàm dưới đây cho mọi đường dẫn
// mà Split nhận; system.FakeSystem dùng chung Split, Error và các lỗi Err*.
//
// Gói phải có (viết ở các file khác trong gói). anchor là neo (đường dẫn
// tuyệt đối, ví dụ "/var/www/a.example.com"), rel là phần còn lại (ví dụ
// "public/.htaccess", không bắt đầu bằng "/"). Đường dẫn không có: lỗi thoả
// errors.Is(err, fs.ErrNotExist). Lời từ chối: *Error bọc một Err* dưới đây.
// perm nhận cả fs.ModeSetgid, fs.ModeSetuid, fs.ModeSticky và đổi sang bit
// của kernel như os.Chmod làm (thư mục site là 2750).
//
//	// ReadFile đọc cả file (luật C4). max ≤ 0 nghĩa là MaxFile.
//	func ReadFile(anchor, rel string, max int64) ([]byte, error)
//	// Lstat như os.Lstat cho tên cuối; thư mục cha là symlink thì ErrSymlink.
//	func Lstat(anchor, rel string) (Info, error)
//	// ReadDir trả tên các mục đã sắp xếp, tối đa max (≤ 0: MaxDirEntries),
//	// quá thì ErrTooMany. Tên cuối cũng phải là thư mục thật (không symlink).
//	func ReadDir(anchor, rel string, max int) ([]string, error)
//	// WriteFileAtomic: file tạm O_CREAT|O_EXCL|O_NOFOLLOW cạnh đích, ghi,
//	// fchown, fchmod, fsync, renameat đè lên tên cuối (luật C6).
//	func WriteFileAtomic(anchor, rel string, data []byte, perm fs.FileMode, uid, gid int) error
//	// MkdirAll tạo các thư mục còn thiếu bên dưới neo; chỉ thư mục mới tạo
//	// nhận perm và chủ (luật C7). Neo phải có sẵn.
//	func MkdirAll(anchor, rel string, perm fs.FileMode, uid, gid int) error
//	// Remove xoá một file, một symlink (không xoá đích) hoặc một thư mục rỗng.
//	func Remove(anchor, rel string) error
//	// Rename đổi tên. Bên nào nằm ngoài vùng site thì anchor của bên đó là ""
//	// và rel của nó là đường dẫn tuyệt đối (dùng AT_FDCWD).
//	func Rename(oldAnchor, oldRel, newAnchor, newRel string) error
//	// Symlink tạo link rel trỏ tới target (target chỉ là chuỗi, không mở).
//	func Symlink(target, anchor, rel string) error
//	// Readlink đọc đích của symlink rel (readlinkat trên fd thư mục cha).
//	func Readlink(anchor, rel string) (string, error)
//	// Chown, Chmod: luật C5 (fd đã mở, thư mục hoặc file thường một tên).
//	func Chown(anchor, rel string, uid, gid int) error
//	func Chmod(anchor, rel string, perm fs.FileMode) error
//	// StatFS: dung lượng phân vùng chứa rel (fstatfs trên fd đã mở).
//	// free là phần user thường dùng được (Bavail), như system.DiskUsage.
//	func StatFS(anchor, rel string) (total, free uint64, err error)
package sitefs

import (
	"errors"
	"io/fs"
)

// Root là thư mục cha của mọi neo. Chỉ thư mục này: đó là nơi duy nhất user
// của site làm chủ thư mục (thư mục log là root:<nhóm site> 0750, thùng rác
// và chỗ giải nén khôi phục là 0700 của root).
const Root = "/var/www"

// Trần của cửa (mục 3.8 của bản thiết kế).
const (
	// MaxFile là trần cỡ một file tpan đọc trong vùng site. .htaccess và
	// wp-config.php thật chỉ vài KB.
	MaxFile int64 = 1 << 20
	// MaxDirEntries là trần số mục ReadDir trả.
	MaxDirEntries = 100_000
	// MaxDepth là trần số thành phần của rel.
	MaxDepth = 64
)

// Các lời từ chối của cửa. Luôn được bọc trong *Error kèm thao tác và đường
// dẫn; kiểm bằng errors.Is.
var (
	ErrPath       = errors.New("đường dẫn trong thư mục site không hợp lệ (rỗng, có \".\", \"..\", byte 0, hoặc quá sâu)")
	ErrSymlink    = errors.New("là symlink: tpan không đi theo symlink trong thư mục site")
	ErrNotRegular = errors.New("không phải file thường: tpan không mở thư mục, thiết bị, FIFO hay socket ở chỗ cần file thường")
	ErrTooLarge   = errors.New("lớn hơn trần tpan đọc trong thư mục site")
	ErrOwner      = errors.New("không thuộc user của site: tpan chỉ đọc file do chính site làm chủ")
	ErrLinked     = errors.New("có hơn một tên (hardlink): tpan không đổi chủ hay quyền qua hardlink")
	ErrTooMany    = errors.New("thư mục có quá nhiều mục")
	// ErrRootUnsafe: thư mục gốc của các site (/var/www) không thuộc tiến
	// trình đang chạy (root trên máy chủ) hoặc nhóm, người khác ghi được.
	// Khi đó một tiến trình khác đổi tên được neo của site này thành site
	// kia, nên cửa từ chối mọi thao tác.
	ErrRootUnsafe = errors.New("thư mục gốc của các site không thuộc root hoặc người khác ghi được (sửa: chown root:root /var/www && chmod 755 /var/www)")
)

// Error là lời từ chối (hoặc lỗi hệ thống) của cửa. Message nêu thao tác, đường
// dẫn và lý do; không bao giờ chứa nội dung file hay đích của symlink.
type Error struct {
	// Op là thao tác, tiếng Việt ngắn: "đọc", "stat", "ghi", "tạo thư mục",
	// "xoá", "đổi tên", "tạo symlink", "đọc symlink", "đổi chủ", "đổi quyền",
	// "liệt kê", "statfs".
	Op string
	// Path là đường dẫn đầy đủ người gọi đưa (neo + "/" + rel).
	Path string
	// Err là một Err* ở trên, hoặc lỗi của kernel (unix.Errno: ENOENT thoả
	// fs.ErrNotExist qua errors.Is).
	Err error
}

func (e *Error) Error() string { return e.Op + " " + e.Path + ": " + e.Err.Error() }

func (e *Error) Unwrap() error { return e.Err }

// Refused cho biết err là lời từ chối của cửa (một Err* ở trên), không phải
// "không có" hay lỗi hệ thống thường. Server dùng để trả 409 site_path_unsafe.
func Refused(err error) bool {
	for _, e := range []error{ErrPath, ErrSymlink, ErrNotRegular, ErrTooLarge, ErrOwner, ErrLinked, ErrTooMany, ErrRootUnsafe} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

// Info là kết quả Lstat. Mode gồm bit loại như os.FileMode (fs.ModeDir,
// fs.ModeSymlink, fs.ModeNamedPipe...) và bit setgid, setuid, sticky.
type Info struct {
	Mode  fs.FileMode
	Size  int64
	UID   uint32
	GID   uint32
	Nlink uint64
}
