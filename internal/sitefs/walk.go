package sitefs

import (
	"errors"
	"os"
	"path"
	"strings"

	"golang.org/x/sys/unix"
)

// Cờ mở. O_NONBLOCK ở mọi nơi (C9): FIFO bị tráo vào ở bất kỳ thành phần
// nào cũng không được làm root phải chờ. O_NOFOLLOW ở mọi nơi trừ chính thư
// mục gốc của site (các thành phần của nó thuộc về root).
const (
	dirFlags  = unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_NONBLOCK | unix.O_CLOEXEC
	leafFlags = unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_NONBLOCK | unix.O_CLOEXEC
	rootFlags = unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC
)

// parent là kết quả khi đi qua mọi thành phần của rel trừ thành phần cuối:
// một fd đang mở trên thư mục chứa thành phần cuối, rồi mọi thao tác dùng nó
// với các lời gọi *at hoặc bằng cách mở tên cuối (C3).
type parent struct {
	fd     int
	name   string
	anchor unix.Stat_t // fstat của fd neo: uid, gid của user site
}

// fail bọc err vào *Error. Path không bao giờ chứa nội dung file (B14): chỉ
// có những gì bên gọi đã đưa vào.
func fail(op, anchor, rel string, err error) error {
	p := rel
	if anchor != "" {
		p = anchor + "/" + rel
	}
	return &Error{Op: op, Path: p, Err: err}
}

// splitRel kiểm anchor và rel mà không đụng tới kernel (C2).
func splitRel(anchor, rel string) ([]string, error) {
	if !path.IsAbs(anchor) || path.Clean(anchor) != anchor || strings.IndexByte(anchor, 0) >= 0 {
		return nil, ErrPath
	}
	if b := path.Base(anchor); b == "/" || b == "." || b == ".." {
		return nil, ErrPath
	}
	if rel == "" {
		return nil, ErrPath
	}
	parts := strings.Split(rel, "/")
	if len(parts) > MaxDepth {
		return nil, ErrPath
	}
	for _, c := range parts {
		if c == "" || c == "." || c == ".." || strings.IndexByte(c, 0) >= 0 {
			return nil, ErrPath
		}
	}
	return parts, nil
}

// closeFD đóng một fd chỉ dùng để đi đường hay đọc. Lỗi khi đóng không đổi
// được kết quả của thao tác đã xong, nên bỏ qua có chủ ý. File tạm của
// WriteFileAtomic thì không đóng bằng hàm này: ở đó lỗi khi đóng là lỗi ghi.
func closeFD(fd int) { _ = unix.Close(fd) }

// openAnchor kiểm thư mục gốc của site rồi mở neo (C1). Thư mục gốc phải
// thuộc về tiến trình này và nhóm hay người khác không được ghi: nếu không,
// một user khác có thể đổi tên neo của site này thành tên của site kia (S15).
func openAnchor(anchor string) (int, unix.Stat_t, error) {
	var st unix.Stat_t
	rfd, err := openat(unix.AT_FDCWD, path.Dir(anchor), rootFlags, 0)
	if err != nil {
		return -1, st, err
	}
	defer closeFD(rfd)
	if err := unix.Fstat(rfd, &st); err != nil {
		return -1, st, err
	}
	if int(st.Uid) != os.Geteuid() || uint32(st.Mode)&0o022 != 0 {
		return -1, st, ErrRootUnsafe
	}
	fd, err := openDir(rfd, path.Base(anchor))
	if err != nil {
		return -1, st, err
	}
	if err := unix.Fstat(fd, &st); err != nil {
		closeFD(fd)
		return -1, st, err
	}
	return fd, st, nil
}

// walk mở neo và mọi thành phần giữa của rel, mỗi lần một openat
// (O_NOFOLLOW). Bên gọi đóng p.fd.
func walk(anchor, rel string) (parent, error) {
	parts, err := splitRel(anchor, rel)
	if err != nil {
		return parent{}, err
	}
	fd, ast, err := openAnchor(anchor)
	if err != nil {
		return parent{}, err
	}
	for _, c := range parts[:len(parts)-1] {
		next, err := openDir(fd, c)
		closeFD(fd)
		if err != nil {
			return parent{}, err
		}
		fd = next
	}
	return parent{fd: fd, name: parts[len(parts)-1], anchor: ast}, nil
}

// openat thử lại khi gặp EINTR.
func openat(dirfd int, name string, flags int, mode uint32) (int, error) {
	for {
		fd, err := unix.Openat(dirfd, name, flags, mode)
		if !errors.Is(err, unix.EINTR) {
			return fd, err
		}
	}
}

// openDir mở một thư mục dưới dirfd mà không đi theo symlink.
func openDir(dirfd int, name string) (int, error) {
	fd, err := openat(dirfd, name, dirFlags, 0)
	if err != nil {
		return -1, classify(dirfd, name, err)
	}
	return fd, nil
}

// openLeaf mở tên cuối (O_NOFOLLOW|O_NONBLOCK). Bên gọi đã kiểm kiểu của nó
// bằng lstatAt; lần mở này mới là lần an toàn trước race, và fstat trên fd
// trả về mới là phép kiểm có giá trị.
func openLeaf(dirfd int, name string) (int, error) {
	fd, err := openat(dirfd, name, leafFlags, 0)
	if err != nil {
		return -1, classify(dirfd, name, err)
	}
	return fd, nil
}

// classify: O_NOFOLLOW trên symlink cho ELOOP, còn O_DIRECTORY|O_NOFOLLOW
// cho ENOTDIR trên Linux 6.8. Chỉ nhìn tên để báo "là symlink"; mọi trường
// hợp khác giữ nguyên lỗi của kernel.
func classify(dirfd int, name string, err error) error {
	if !errors.Is(err, unix.ELOOP) && !errors.Is(err, unix.ENOTDIR) {
		return err
	}
	if st, e := lstatAt(dirfd, name); e == nil && fileType(&st) == unix.S_IFLNK {
		return ErrSymlink
	}
	return err
}

func lstatAt(dirfd int, name string) (unix.Stat_t, error) {
	var st unix.Stat_t
	err := unix.Fstatat(dirfd, name, &st, unix.AT_SYMLINK_NOFOLLOW)
	return st, err
}

// leafKind từ chối symlink và mọi thứ không phải file thường (hoặc thư mục
// khi dirOK).
func leafKind(st *unix.Stat_t, dirOK bool) error {
	switch fileType(st) {
	case unix.S_IFREG:
		return nil
	case unix.S_IFLNK:
		return ErrSymlink
	case unix.S_IFDIR:
		if dirOK {
			return nil
		}
	}
	return ErrNotRegular
}
