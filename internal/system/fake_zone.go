package system

import (
	"io/fs"
	"path"
	"sort"
	"strings"
	"syscall"

	"github.com/doxuta/tpan/internal/sitefs"
)

// The fake follows the same gate rules as LocalSystem inside the site zone
// (M28-thiet-ke.md, 3.3), with the zone root fixed at sitefs.Root: no symlink
// is followed at the anchor or any component below it; ReadFile only reads a
// regular file owned by the anchor's user (skipped when the anchor belongs to
// root), within sitefs.MaxFile; Chown, Chmod only touch a directory or a
// regular file. Errors are *sitefs.Error, like the real gate. Outside the zone
// nothing changes. All functions here are called with f.mu held.

// SetSite builds dir and dir/public owned by user:www-data, mode 2750, like
// a real site, so the owner rule of ReadFile is on (S16). A parent made
// implicitly by SetFile belongs to root, which turns the rule off.
func (f *FakeSystem) SetSite(dir, user string) {
	owner := Owner{User: user, Group: "www-data"}
	f.SetDir(dir, fs.ModeSetgid|0o750, owner)
	f.SetDir(path.Join(dir, "public"), fs.ModeSetgid|0o750, owner)
}

func zoneErr(op, p string, err error) error { return &sitefs.Error{Op: op, Path: p, Err: err} }

// zoneWalk resolves anchor/rel without following any symlink. It returns the
// full path, the anchor entry and the entry at the last name (nil when
// missing). Middle components must exist and be real directories.
func (f *FakeSystem) zoneWalk(op, anchor, rel string) (string, *FakeEntry, *FakeEntry, error) {
	full := anchor + "/" + rel
	if r, ok := f.entries[sitefs.Root]; ok && (r.Owner.User != "root" || r.Mode&0o022 != 0) {
		return full, nil, nil, zoneErr(op, full, sitefs.ErrRootUnsafe)
	}
	parts := strings.Split(rel, "/")
	if len(parts) > sitefs.MaxDepth || strings.IndexByte(rel, 0) >= 0 {
		return full, nil, nil, zoneErr(op, full, sitefs.ErrPath)
	}
	anc, err := f.zoneDir(op, full, anchor)
	if err != nil {
		return full, nil, nil, err
	}
	cur := anchor
	for _, c := range parts[:len(parts)-1] {
		cur = cur + "/" + c
		if _, err := f.zoneDir(op, full, cur); err != nil {
			return full, nil, nil, err
		}
	}
	return full, anc, f.entries[full], nil
}

// zoneDir: the entry at p must be a directory that is not a symlink.
func (f *FakeSystem) zoneDir(op, full, p string) (*FakeEntry, error) {
	e, ok := f.entries[p]
	switch {
	case !ok:
		return nil, zoneErr(op, full, syscall.ENOENT)
	case e.Mode&fs.ModeSymlink != 0:
		return nil, zoneErr(op, full, sitefs.ErrSymlink)
	case !e.Mode.IsDir():
		return nil, zoneErr(op, full, syscall.ENOTDIR)
	}
	return e, nil
}

// zoneLeaf is the last-name check of the gate: missing, symlink, or neither
// a regular file nor (when dirOK) a directory.
func zoneLeaf(op, full string, e *FakeEntry, dirOK bool) error {
	switch {
	case e == nil:
		return zoneErr(op, full, syscall.ENOENT)
	case e.Mode&fs.ModeSymlink != 0:
		return zoneErr(op, full, sitefs.ErrSymlink)
	case e.Mode.Type() == 0, dirOK && e.Mode.IsDir():
		return nil
	}
	return zoneErr(op, full, sitefs.ErrNotRegular)
}

func (f *FakeSystem) zoneReadFile(a, r string) ([]byte, error) {
	full, anc, e, err := f.zoneWalk("đọc", a, r)
	if err != nil {
		return nil, err
	}
	if err := zoneLeaf("đọc", full, e, false); err != nil {
		return nil, err
	}
	if anc.Owner.User != "root" && e.Owner.User != anc.Owner.User {
		return nil, zoneErr("đọc", full, sitefs.ErrOwner)
	}
	if int64(len(e.Data)) > sitefs.MaxFile {
		return nil, zoneErr("đọc", full, sitefs.ErrTooLarge)
	}
	return []byte(e.Data), nil
}

func (f *FakeSystem) zoneWrite(a, r string, data []byte, perm fs.FileMode, owner Owner) error {
	full, _, e, err := f.zoneWalk("ghi", a, r)
	if err != nil {
		return err
	}
	// rename replaces a file or a symlink, never a directory.
	if e != nil && e.Mode.IsDir() {
		return zoneErr("ghi", full, syscall.EISDIR)
	}
	f.entries[full] = &FakeEntry{Mode: keepMode(perm), Owner: normOwner(owner), Data: string(data)}
	return nil
}

// zoneMkdirAll: the anchor and its parents belong to root and are created
// like outside the zone when missing; below the anchor no symlink is followed.
func (f *FakeSystem) zoneMkdirAll(a, r string, perm fs.FileMode, owner Owner) error {
	full := a + "/" + r
	var missing []string
	if _, ok := f.entries[a]; !ok {
		for d := a; d != "/"; d = path.Dir(d) {
			if _, ok := f.entries[d]; !ok {
				missing = append([]string{d}, missing...)
			}
		}
	} else if _, err := f.zoneDir("tạo thư mục", full, a); err != nil {
		return err
	}
	cur := a
	for _, c := range strings.Split(r, "/") {
		cur = cur + "/" + c
		e, ok := f.entries[cur]
		switch {
		case !ok:
			missing = append(missing, cur)
		case e.Mode&fs.ModeSymlink != 0:
			return zoneErr("tạo thư mục", full, sitefs.ErrSymlink)
		case !e.Mode.IsDir():
			return zoneErr("tạo thư mục", full, syscall.ENOTDIR)
		}
	}
	for _, m := range missing {
		if err := f.failed("mkdir", m); err != nil {
			return err
		}
	}
	for _, m := range missing {
		f.entries[m] = &FakeEntry{Mode: fs.ModeDir | keepMode(perm), Owner: normOwner(owner)}
	}
	return nil
}

func (f *FakeSystem) zoneRemove(a, r string) error {
	full, _, e, err := f.zoneWalk("xoá", a, r)
	if err != nil {
		return err
	}
	if e == nil {
		return zoneErr("xoá", full, syscall.ENOENT)
	}
	if e.Mode.IsDir() && f.hasChildren(full) {
		return zoneErr("xoá", full, syscall.ENOTEMPTY)
	}
	delete(f.entries, full)
	return nil
}

// renameSrc and renameDst resolve one side of Rename: through the gate rules
// inside the zone, the old way (following parent symlinks) outside.
func (f *FakeSystem) renameSrc(p string) (string, *FakeEntry, error) {
	if a, r, ok := sitefs.Split(sitefs.Root, p); ok {
		full, _, e, err := f.zoneWalk("đổi tên", a, r)
		if err == nil && e == nil {
			err = zoneErr("đổi tên", full, syscall.ENOENT)
		}
		return full, e, err
	}
	return f.lookup("rename", p, false)
}

func (f *FakeSystem) renameDst(p string) (string, error) {
	if a, r, ok := sitefs.Split(sitefs.Root, p); ok {
		full, _, _, err := f.zoneWalk("đổi tên", a, r)
		return full, err
	}
	dir, err := f.dirOf("rename", p)
	if err != nil {
		return "", err
	}
	return path.Join(dir, path.Base(p)), nil
}

// zoneSymlink gives the new link to the anchor's owner, like the real gate (S14).
func (f *FakeSystem) zoneSymlink(target, a, r string) error {
	full, anc, e, err := f.zoneWalk("tạo symlink", a, r)
	if err != nil {
		return err
	}
	if e != nil {
		return zoneErr("tạo symlink", full, syscall.EEXIST)
	}
	f.entries[full] = &FakeEntry{Mode: fs.ModeSymlink | 0o777, Owner: anc.Owner, Target: target}
	return nil
}

func (f *FakeSystem) zoneReadlink(a, r string) (string, error) {
	full, _, e, err := f.zoneWalk("đọc symlink", a, r)
	switch {
	case err != nil:
		return "", err
	case e == nil:
		return "", zoneErr("đọc symlink", full, syscall.ENOENT)
	case e.Mode&fs.ModeSymlink == 0:
		return "", zoneErr("đọc symlink", full, syscall.EINVAL)
	}
	return e.Target, nil
}

func (f *FakeSystem) zoneStat(a, r string) (FileInfo, error) {
	full, _, e, err := f.zoneWalk("stat", a, r)
	if err != nil {
		return FileInfo{}, err
	}
	if e == nil {
		return FileInfo{}, zoneErr("stat", full, syscall.ENOENT)
	}
	return statOf(e), nil
}

func (f *FakeSystem) zoneReadDir(a, r string) ([]string, error) {
	full, _, e, err := f.zoneWalk("liệt kê", a, r)
	if err != nil {
		return nil, err
	}
	if err := zoneLeaf("liệt kê", full, e, true); err != nil {
		return nil, err
	}
	if !e.Mode.IsDir() {
		return nil, zoneErr("liệt kê", full, syscall.ENOTDIR)
	}
	names := f.children(full)
	if len(names) > sitefs.MaxDirEntries {
		return nil, zoneErr("liệt kê", full, sitefs.ErrTooMany)
	}
	return names, nil
}

// zoneEntry is the target of Chown, Chmod: a directory or a regular file.
func (f *FakeSystem) zoneEntry(op, a, r string) (*FakeEntry, error) {
	full, _, e, err := f.zoneWalk(op, a, r)
	if err != nil {
		return nil, err
	}
	if err := zoneLeaf(op, full, e, true); err != nil {
		return nil, err
	}
	return e, nil
}

func (f *FakeSystem) zoneStatFS(a, r string) (string, error) {
	full, _, e, err := f.zoneWalk("statfs", a, r)
	switch {
	case err != nil:
		return "", err
	case e == nil:
		return "", zoneErr("statfs", full, syscall.ENOENT)
	case e.Mode&fs.ModeSymlink != 0:
		return "", zoneErr("statfs", full, sitefs.ErrSymlink)
	}
	return full, nil
}

// children lists the names directly under dir, sorted.
func (f *FakeSystem) children(dir string) []string {
	var names []string
	for q := range f.entries {
		if q != "/" && path.Dir(q) == dir {
			names = append(names, path.Base(q))
		}
	}
	sort.Strings(names)
	return names
}

func statOf(e *FakeEntry) FileInfo {
	size := int64(len(e.Data))
	if e.Mode&fs.ModeSymlink != 0 {
		size = int64(len(e.Target)) // like lstat: a symlink's size is its target's length
	}
	return FileInfo{Mode: e.Mode, Size: size, Owner: e.Owner}
}
