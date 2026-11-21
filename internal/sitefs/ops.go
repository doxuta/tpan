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

// ReadFile reads a whole regular file of the site user (C4). max ≤ 0 is MaxFile.
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
	// (a) Look before opening: never open a device or FIFO already there.
	st, err := lstatAt(p.fd, p.name)
	if err != nil {
		return nil, err
	}
	if err := leafKind(&st, false); err != nil {
		return nil, err
	}
	// (b) O_NONBLOCK: a FIFO swapped in after (a) does not block the open.
	fd, err := openLeaf(p.fd, p.name)
	if err != nil {
		return nil, err
	}
	defer closeFD(fd)
	// (c) The check that counts: on the open fd, nothing can swap it now.
	if err := checkRead(fd, &p.anchor, max); err != nil {
		return nil, err
	}
	// (d) The file may grow after fstat.
	return readLimit(fd, max)
}

// checkRead: regular file, owned by the site user (skipped when the anchor
// belongs to root), not larger than max.
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

// readLimit reads fd to EOF but never more than max+1 bytes.
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

// Lstat is os.Lstat for the last name; a symlink among the parents is ErrSymlink.
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

// ReadDir lists a real directory (not a symlink), sorted, at most max names.
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
		// ParseDirent skips "." and "..".
		_, _, names = unix.ParseDirent(buf[:n], -1, names)
		if len(names) > max {
			return nil, ErrTooMany
		}
	}
}

// WriteFileAtomic writes a temp file next to the target and renames it over
// the last name (C6). rename replaces whatever is there, a symlink included;
// it never writes through it.
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
	// chown before chmod: chown may clear setuid, setgid.
	if err = unix.Fchown(fd, uid, gid); err != nil {
		return err
	}
	if err = unix.Fchmod(fd, toKernel(perm)); err != nil {
		return err
	}
	// fsync before rename: a power cut right after rename never gives an empty file.
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

// tempName is ".<name>.tpan-<16 hex>", cut so it stays within NAME_MAX.
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

// MkdirAll creates the missing directories below the anchor (C7). Only a
// directory this call created gets perm and owner, set on its own fd.
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

// mkdirOne opens directory c under dirfd, creating it when missing. 0700 at
// mkdirat so nobody gets in before fchown, fchmod.
func mkdirOne(dirfd int, c string, perm fs.FileMode, uid, gid int) (int, error) {
	fd, err := openDir(dirfd, c)
	if err == nil || !errors.Is(err, unix.ENOENT) {
		return fd, err
	}
	if err := unix.Mkdirat(dirfd, c, 0o700); err != nil {
		if errors.Is(err, unix.EEXIST) {
			// Lost a race: someone else created it. Use it, do not touch it.
			return openDir(dirfd, c)
		}
		return -1, err
	}
	// Reopen by fd: if the new directory was swapped for a symlink in
	// between, O_NOFOLLOW refuses it.
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

// Remove removes a file, a symlink (not its target) or an empty directory.
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
	// unlink on a directory is EPERM on macOS, EISDIR on Linux: pick the flag.
	flags := 0
	if fileType(&st) == unix.S_IFDIR {
		flags = unix.AT_REMOVEDIR
	}
	if err := unix.Unlinkat(p.fd, p.name, flags); err != nil {
		return fail("xoá", anchor, rel, err)
	}
	return nil
}

// Rename renames between two parents. A side outside the site zone has
// anchor "" and an absolute rel (AT_FDCWD).
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

// Symlink creates link rel pointing at target (only a string, never opened),
// then gives the link itself to the site user (S14): a root-owned link would
// pass SymLinksIfOwnerMatch and disable_symlinks if_not_owner. If the name is
// swapped in between, AT_SYMLINK_NOFOLLOW only re-owns a thing of the site.
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

// Readlink reads the target of symlink rel (readlinkat on the parent fd).
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

// Chown changes owner through the open fd (C5).
func Chown(anchor, rel string, uid, gid int) error {
	return onLeaf("đổi chủ", anchor, rel, func(fd int) error { return unix.Fchown(fd, uid, gid) })
}

// Chmod changes mode through the open fd (C5). Never fchmodat by name: on
// Linux it follows a symlink swapped in after the check (T8).
func Chmod(anchor, rel string, perm fs.FileMode) error {
	return onLeaf("đổi quyền", anchor, rel, func(fd int) error { return unix.Fchmod(fd, toKernel(perm)) })
}

// onLeaf opens the last name (a directory, or a regular file with one name)
// and runs fn on its fd.
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

// StatFS gives the size of the filesystem holding rel. A directory is opened
// (fstatfs on its fd); anything else is measured on its parent's fd, so no
// device or FIFO is ever opened.
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
	// Bsize is int64 on linux, uint32 on darwin.
	bs := uint64(sf.Bsize)
	return sf.Blocks * bs, sf.Bavail * bs, nil
}
