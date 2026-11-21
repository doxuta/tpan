package system

import (
	"io/fs"
	"path"
	"sort"
	"strings"
	"syscall"

	"github.com/doxuta/tpan/internal/sitefs"
)

// Bản giả theo cùng luật cổng như LocalSystem bên trong vùng site
// (M28-thiet-ke.md, 3.3), với thư mục gốc vùng cố định ở sitefs.Root: không
// đi theo symlink ở neo hay ở bất kỳ thành phần nào bên dưới; ReadFile chỉ
// đọc file thường thuộc user của neo (bỏ qua khi neo thuộc root), trong
// giới hạn sitefs.MaxFile; Chown, Chmod chỉ đụng thư mục hoặc file thường.
// Lỗi là *sitefs.Error, như cổng thật. Ngoài vùng thì không đổi gì. Mọi hàm
// ở đây được gọi khi đang giữ f.mu.

// SetSite dựng dir và dir/public thuộc user:www-data, mode 2750, giống một
// site thật, để luật chủ của ReadFile bật (S16). Thư mục cha do SetFile tạo
// ngầm thì thuộc root, và làm luật đó tắt.
func (f *FakeSystem) SetSite(dir, user string) {
	owner := Owner{User: user, Group: "www-data"}
	f.SetDir(dir, fs.ModeSetgid|0o750, owner)
	f.SetDir(path.Join(dir, "public"), fs.ModeSetgid|0o750, owner)
}

func zoneErr(op, p string, err error) error { return &sitefs.Error{Op: op, Path: p, Err: err} }

// zoneWalk phân giải anchor/rel mà không đi theo symlink nào. Nó trả về
// đường dẫn đầy đủ, mục của neo và mục ở tên cuối (nil khi không có). Các
// thành phần giữa phải tồn tại và là thư mục thật.
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

// zoneDir: mục ở p phải là thư mục và không phải symlink.
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

// zoneLeaf là phép kiểm tên cuối của cổng: không có, là symlink, hoặc không
// phải file thường cũng không phải (khi dirOK) thư mục.
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
	// rename thay thế một file hoặc một symlink, không bao giờ thay thư mục.
	if e != nil && e.Mode.IsDir() {
		return zoneErr("ghi", full, syscall.EISDIR)
	}
	f.entries[full] = &FakeEntry{Mode: keepMode(perm), Owner: normOwner(owner), Data: string(data)}
	return nil
}

// zoneMkdirAll: neo và các thư mục cha của nó thuộc root và được tạo như
// ngoài vùng khi chưa có; bên dưới neo không đi theo symlink nào.
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

// renameSrc và renameDst phân giải một phía của Rename: theo luật cổng bên
// trong vùng, theo cách cũ (đi theo symlink của thư mục cha) bên ngoài.
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

// zoneSymlink giao link mới cho chủ của neo, như cổng thật (S14).
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

// zoneEntry là đích của Chown, Chmod: một thư mục hoặc một file thường.
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

// children liệt kê các tên nằm ngay dưới dir, đã sắp xếp.
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
		size = int64(len(e.Target)) // như lstat: kích thước của symlink là độ dài đích của nó
	}
	return FileInfo{Mode: e.Mode, Size: size, Owner: e.Owner}
}
