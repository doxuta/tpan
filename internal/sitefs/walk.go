package sitefs

import (
	"errors"
	"os"
	"path"
	"strings"

	"golang.org/x/sys/unix"
)

// Open flags. O_NONBLOCK everywhere (C9): a FIFO swapped in at any component
// must not make root wait. O_NOFOLLOW everywhere except on the site root
// itself (its components belong to root).
const (
	dirFlags  = unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_NONBLOCK | unix.O_CLOEXEC
	leafFlags = unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_NONBLOCK | unix.O_CLOEXEC
	rootFlags = unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC
)

// parent is the result of walking every component of rel except the last:
// an open fd on the directory holding the last component, which every
// operation then uses with *at calls or by opening the last name (C3).
type parent struct {
	fd     int
	name   string
	anchor unix.Stat_t // fstat of the anchor fd: uid, gid of the site user
}

// fail wraps err in *Error. Path never carries file content (B14): only what
// the caller passed in.
func fail(op, anchor, rel string, err error) error {
	p := rel
	if anchor != "" {
		p = anchor + "/" + rel
	}
	return &Error{Op: op, Path: p, Err: err}
}

// splitRel checks anchor and rel without touching the kernel (C2).
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

// openAnchor checks the site root and opens the anchor (C1). The root must
// belong to this process and not be writable by group or others: otherwise
// another user could rename one site's anchor to another's name (S15).
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

// walk opens the anchor and every middle component of rel, one openat
// (O_NOFOLLOW) at a time. The caller closes p.fd.
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

// openat retries on EINTR.
func openat(dirfd int, name string, flags int, mode uint32) (int, error) {
	for {
		fd, err := unix.Openat(dirfd, name, flags, mode)
		if !errors.Is(err, unix.EINTR) {
			return fd, err
		}
	}
}

// openDir opens a directory under dirfd without following a symlink.
func openDir(dirfd int, name string) (int, error) {
	fd, err := openat(dirfd, name, dirFlags, 0)
	if err != nil {
		return -1, classify(dirfd, name, err)
	}
	return fd, nil
}

// openLeaf opens the last name (O_NOFOLLOW|O_NONBLOCK). Callers have already
// checked its type with lstatAt; this open is the race-safe one, and the
// fstat on the returned fd is the check that counts.
func openLeaf(dirfd int, name string) (int, error) {
	fd, err := openat(dirfd, name, leafFlags, 0)
	if err != nil {
		return -1, classify(dirfd, name, err)
	}
	return fd, nil
}

// classify: O_NOFOLLOW on a symlink gives ELOOP, and O_DIRECTORY|O_NOFOLLOW
// gives ENOTDIR on Linux 6.8. Look at the name only to report "is a symlink";
// anything else keeps the kernel error.
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

// leafKind rejects a symlink, and anything other than a regular file (or a
// directory when dirOK).
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
