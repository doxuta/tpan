package sitefs

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"path"
	"sort"

	"golang.org/x/sys/unix"
)

// ReadFile đọc trọn một file thường của user site (C4). max ≤ 0 nghĩa là MaxFile.
func ReadFile(anchor, rel string, max int64) ([]byte, error) {
	if max <= 0 {
		max = MaxFile
	}
	p, err := walk(anchor, rel)
	if err != nil {
		return nil, fail("đọc", anchor, rel, err)
	}
	defer closeFD(p.fd)
	b, err := readLeaf(p, max)
	if err != nil {
		return nil, fail("đọc", anchor, rel, err)
	}
	return b, nil
}

func readLeaf(p parent, max int64) ([]byte, error) {
	// (a) Nhìn trước khi mở: không bao giờ mở thiết bị hay FIFO đã có sẵn.
	st, err := lstatAt(p.fd, p.name)
	if err != nil {
		return nil, err
	}
	if err := leafKind(&st, false); err != nil {
		return nil, err
	}
	// (b) O_NONBLOCK: FIFO bị tráo vào sau (a) không làm lần mở bị treo.
	fd, err := openLeaf(p.fd, p.name)
	if err != nil {
		return nil, err
	}
	defer closeFD(fd)
	// (c) Phép kiểm có giá trị: trên fd đã mở, giờ không ai tráo được nữa.
	if err := checkRead(fd, &p.anchor, max); err != nil {
		return nil, err
	}
	// (d) File có thể lớn thêm sau fstat.
	return readLimit(fd, max)
}

// checkRead: file thường, thuộc user site (bỏ qua khi neo thuộc root), không
// lớn hơn max.
func checkRead(fd int, anchor *unix.Stat_t, max int64) error {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return err
	}
	if fileType(&st) != unix.S_IFREG {
		return ErrNotRegular
	}
	if anchor.Uid != 0 && st.Uid != anchor.Uid {
		return ErrOwner
	}
	if st.Size > max {
		return ErrTooLarge
	}
	return nil
}

// readLimit đọc fd tới EOF nhưng không bao giờ quá max+1 byte.
func readLimit(fd int, max int64) ([]byte, error) {
	var out []byte
	buf := make([]byte, 32<<10)
	for {
		n, err := unix.Read(fd, buf)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return out, nil
		}
		out = append(out, buf[:n]...)
		if int64(len(out)) > max {
			return nil, ErrTooLarge
		}
	}
}

// Lstat là os.Lstat cho tên cuối; có symlink trong các thư mục cha thì là ErrSymlink.
func Lstat(anchor, rel string) (Info, error) {
	p, err := walk(anchor, rel)
	if err != nil {
		return Info{}, fail("stat", anchor, rel, err)
	}
	defer closeFD(p.fd)
	st, err := lstatAt(p.fd, p.name)
	if err != nil {
		return Info{}, fail("stat", anchor, rel, err)
	}
	return toInfo(&st), nil
}

// ReadDir liệt kê một thư mục thật (không phải symlink), đã sắp xếp, tối đa max tên.
func ReadDir(anchor, rel string, max int) ([]string, error) {
	if max <= 0 {
		max = MaxDirEntries
	}
	names, err := readDir(anchor, rel, max)
	if err != nil {
		return nil, fail("liệt kê", anchor, rel, err)
	}
	sort.Strings(names)
	return names, nil
}

func readDir(anchor, rel string, max int) ([]string, error) {
	p, err := walk(anchor, rel)
	if err != nil {
		return nil, err
	}
	fd, err := openDir(p.fd, p.name)
	closeFD(p.fd)
	if err != nil {
		return nil, err
	}
	defer closeFD(fd)
	var names []string
	buf := make([]byte, 32<<10)
	for {
		n, err := unix.ReadDirent(fd, buf)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if n <= 0 {
			return names, nil
		}
		// ParseDirent bỏ qua "." và "..".
		_, _, names = unix.ParseDirent(buf[:n], -1, names)
		if len(names) > max {
			return nil, ErrTooMany
		}
	}
}

// WriteFileAtomic ghi một file tạm cạnh đích rồi rename đè lên tên cuối (C6).
// rename thay thế bất cứ thứ gì đang ở đó, kể cả symlink; nó không bao giờ
// ghi xuyên qua symlink.
func WriteFileAtomic(anchor, rel string, data []byte, perm fs.FileMode, uid, gid int) error {
	p, err := walk(anchor, rel)
	if err != nil {
		return fail("ghi", anchor, rel, err)
	}
	defer closeFD(p.fd)
	if err := writeAt(p, data, perm, uid, gid); err != nil {
		return fail("ghi", anchor, rel, err)
	}
	return nil
}

func writeAt(p parent, data []byte, perm fs.FileMode, uid, gid int) (err error) {
	tmp, err := tempName(p.name)
	if err != nil {
		return err
	}
	fd, err := openat(p.fd, tmp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if fd >= 0 {
			closeFD(fd)
		}
		if err != nil {
			_ = unix.Unlinkat(p.fd, tmp, 0)
		}
	}()
	for b := data; len(b) > 0; {
		n, werr := unix.Write(fd, b)
		if errors.Is(werr, unix.EINTR) {
			continue
		}
		if werr != nil {
			return werr
		}
		b = b[n:]
	}
	// chown trước chmod: chown có thể xoá setuid, setgid.
	if err = unix.Fchown(fd, uid, gid); err != nil {
		return err
	}
	if err = unix.Fchmod(fd, toKernel(perm)); err != nil {
		return err
	}
	// fsync trước rename: mất điện ngay sau rename không bao giờ để lại file rỗng.
	if err = unix.Fsync(fd); err != nil {
		return err
	}
	err = unix.Close(fd)
	fd = -1
	if err != nil {
		return err
	}
	return unix.Renameat(p.fd, tmp, p.fd, p.name)
}

// tempName là ".<name>.tpan-<16 hex>", được cắt để không vượt NAME_MAX.
func tempName(name string) (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	if len(name) > 200 {
		name = name[:200]
	}
	return "." + name + ".tpan-" + hex.EncodeToString(b[:]), nil
}

// MkdirAll tạo các thư mục còn thiếu dưới neo (C7). Chỉ thư mục do lời gọi
// này tạo mới được đặt perm và chủ, làm trên fd của chính nó.
func MkdirAll(anchor, rel string, perm fs.FileMode, uid, gid int) error {
	if err := mkdirAll(anchor, rel, perm, uid, gid); err != nil {
		return fail("tạo thư mục", anchor, rel, err)
	}
	return nil
}

func mkdirAll(anchor, rel string, perm fs.FileMode, uid, gid int) error {
	parts, err := splitRel(anchor, rel)
	if err != nil {
		return err
	}
	fd, _, err := openAnchor(anchor)
	if err != nil {
		return err
	}
	defer func() { closeFD(fd) }()
	for _, c := range parts {
		next, err := mkdirOne(fd, c, perm, uid, gid)
		if err != nil {
			return err
		}
		closeFD(fd)
		fd = next
	}
	return nil
}

// mkdirOne mở thư mục c dưới dirfd, tạo nó khi chưa có. Dùng 0700 ở mkdirat
// để không ai vào được trước fchown, fchmod.
func mkdirOne(dirfd int, c string, perm fs.FileMode, uid, gid int) (int, error) {
	fd, err := openDir(dirfd, c)
	if err == nil || !errors.Is(err, unix.ENOENT) {
		return fd, err
	}
	if err := unix.Mkdirat(dirfd, c, 0o700); err != nil {
		if errors.Is(err, unix.EEXIST) {
			// Thua race: người khác đã tạo. Dùng luôn, không đụng vào.
			return openDir(dirfd, c)
		}
		return -1, err
	}
	// Mở lại để lấy fd: nếu thư mục mới bị tráo thành symlink ở giữa,
	// O_NOFOLLOW sẽ từ chối.
	if fd, err = openDir(dirfd, c); err != nil {
		return -1, err
	}
	if err := unix.Fchown(fd, uid, gid); err != nil {
		closeFD(fd)
		return -1, err
	}
	if err := unix.Fchmod(fd, toKernel(perm)); err != nil {
		closeFD(fd)
		return -1, err
	}
	return fd, nil
}

// Remove xoá một file, một symlink (không xoá đích của nó) hoặc một thư mục rỗng.
func Remove(anchor, rel string) error {
	p, err := walk(anchor, rel)
	if err != nil {
		return fail("xoá", anchor, rel, err)
	}
	defer closeFD(p.fd)
	st, err := lstatAt(p.fd, p.name)
	if err != nil {
		return fail("xoá", anchor, rel, err)
	}
	// unlink trên thư mục là EPERM ở macOS, EISDIR ở Linux: nên chọn cờ theo kiểu.
	flags := 0
	if fileType(&st) == unix.S_IFDIR {
		flags = unix.AT_REMOVEDIR
	}
	if err := unix.Unlinkat(p.fd, p.name, flags); err != nil {
		return fail("xoá", anchor, rel, err)
	}
	return nil
}

// Rename đổi tên giữa hai thư mục cha. Phía nằm ngoài vùng site có anchor ""
// và rel tuyệt đối (AT_FDCWD).
func Rename(oldAnchor, oldRel, newAnchor, newRel string) error {
	ofd, oname, err := renameSide(oldAnchor, oldRel)
	if err != nil {
		return fail("đổi tên", oldAnchor, oldRel, err)
	}
	if ofd != unix.AT_FDCWD {
		defer closeFD(ofd)
	}
	nfd, nname, err := renameSide(newAnchor, newRel)
	if err != nil {
		return fail("đổi tên", newAnchor, newRel, err)
	}
	if nfd != unix.AT_FDCWD {
		defer closeFD(nfd)
	}
	if err := unix.Renameat(ofd, oname, nfd, nname); err != nil {
		return fail("đổi tên", oldAnchor, oldRel, err)
	}
	return nil
}

func renameSide(anchor, rel string) (int, string, error) {
	if anchor == "" {
		if !path.IsAbs(rel) {
			return -1, "", ErrPath
		}
		return unix.AT_FDCWD, rel, nil
	}
	p, err := walk(anchor, rel)
	if err != nil {
		return -1, "", err
	}
	return p.fd, p.name, nil
}

// Symlink tạo link rel trỏ vào target (chỉ là một chuỗi, không bao giờ được
// mở), rồi giao chính cái link cho user site (S14): link của root sẽ qua được
// SymLinksIfOwnerMatch và disable_symlinks if_not_owner. Nếu tên bị tráo ở
// giữa, AT_SYMLINK_NOFOLLOW chỉ đổi chủ một thứ thuộc về site.
func Symlink(target, anchor, rel string) error {
	p, err := walk(anchor, rel)
	if err != nil {
		return fail("tạo symlink", anchor, rel, err)
	}
	defer closeFD(p.fd)
	if err := unix.Symlinkat(target, p.fd, p.name); err != nil {
		return fail("tạo symlink", anchor, rel, err)
	}
	if err := chownLink(p.fd, p.name, int(p.anchor.Uid), int(p.anchor.Gid)); err != nil {
		_ = unix.Unlinkat(p.fd, p.name, 0)
		return fail("tạo symlink", anchor, rel, err)
	}
	return nil
}

// Readlink đọc đích của symlink rel (readlinkat trên fd của thư mục cha).
func Readlink(anchor, rel string) (string, error) {
	p, err := walk(anchor, rel)
	if err != nil {
		return "", fail("đọc symlink", anchor, rel, err)
	}
	defer closeFD(p.fd)
	for n := 256; ; n *= 2 {
		buf := make([]byte, n)
		k, err := unix.Readlinkat(p.fd, p.name, buf)
		if err != nil {
			return "", fail("đọc symlink", anchor, rel, err)
		}
		if k < n {
			return string(buf[:k]), nil
		}
	}
}

// Chown đổi chủ qua fd đang mở (C5).
func Chown(anchor, rel string, uid, gid int) error {
	return onLeaf("đổi chủ", anchor, rel, func(fd int) error { return unix.Fchown(fd, uid, gid) })
}

// Chmod đổi mode qua fd đang mở (C5). Không bao giờ dùng fchmodat theo tên:
// trên Linux nó đi theo symlink bị tráo vào sau lúc kiểm (T8).
func Chmod(anchor, rel string, perm fs.FileMode) error {
	return onLeaf("đổi quyền", anchor, rel, func(fd int) error { return unix.Fchmod(fd, toKernel(perm)) })
}

// onLeaf mở tên cuối (một thư mục, hoặc một file thường chỉ có một tên)
// rồi chạy fn trên fd của nó.
func onLeaf(op, anchor, rel string, fn func(fd int) error) error {
	p, err := walk(anchor, rel)
	if err != nil {
		return fail(op, anchor, rel, err)
	}
	defer closeFD(p.fd)
	if err := leafFD(p, fn); err != nil {
		return fail(op, anchor, rel, err)
	}
	return nil
}

func leafFD(p parent, fn func(fd int) error) error {
	st, err := lstatAt(p.fd, p.name)
	if err != nil {
		return err
	}
	if err := leafKind(&st, true); err != nil {
		return err
	}
	fd, err := openLeaf(p.fd, p.name)
	if err != nil {
		return err
	}
	defer closeFD(fd)
	if err := unix.Fstat(fd, &st); err != nil {
		return err
	}
	if err := leafKind(&st, true); err != nil {
		return err
	}
	if fileType(&st) == unix.S_IFREG && st.Nlink != 1 {
		return ErrLinked
	}
	return fn(fd)
}

// StatFS cho dung lượng của filesystem chứa rel. Thư mục thì được mở
// (fstatfs trên fd của nó); thứ khác thì đo trên fd của thư mục cha, nên
// không bao giờ mở thiết bị hay FIFO.
func StatFS(anchor, rel string) (total, free uint64, err error) {
	p, err := walk(anchor, rel)
	if err != nil {
		return 0, 0, fail("statfs", anchor, rel, err)
	}
	defer closeFD(p.fd)
	total, free, err = statFS(p)
	if err != nil {
		return 0, 0, fail("statfs", anchor, rel, err)
	}
	return total, free, nil
}

func statFS(p parent) (uint64, uint64, error) {
	st, err := lstatAt(p.fd, p.name)
	if err != nil {
		return 0, 0, err
	}
	fd := p.fd
	switch fileType(&st) {
	case unix.S_IFLNK:
		return 0, 0, ErrSymlink
	case unix.S_IFDIR:
		if fd, err = openDir(p.fd, p.name); err != nil {
			return 0, 0, err
		}
		defer closeFD(fd)
	}
	var sf unix.Statfs_t
	if err := unix.Fstatfs(fd, &sf); err != nil {
		return 0, 0, err
	}
	// Bsize là int64 trên linux, uint32 trên darwin.
	bs := uint64(sf.Bsize)
	return sf.Blocks * bs, sf.Bavail * bs, nil
}
