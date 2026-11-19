package system

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"sort"
	"strings"
	"sync"
	"syscall"
)

// FakeSystem giữ hệ thống file và danh sách lệnh trong bộ nhớ. Dùng cho unit
// test và chế độ --demo. An toàn khi dùng từ nhiều goroutine.
//
// Hệ thống file giả cố bắt chước đúng những điểm Step dựa vào: ReadFile đi theo
// symlink còn Stat thì không, ghi file cần thư mục cha có sẵn, xoá thư mục
// không rỗng thì lỗi. Nhờ vậy một Step đúng trên fake thì cũng đúng trên máy thật.
type FakeSystem struct {
	mu       sync.Mutex
	entries  map[string]*FakeEntry // khoá là đường dẫn đã path.Clean
	handlers []fakeHandler
	ran      []Cmd
	fails    map[string]error // khoá là op + "\x00" + path
	disks    map[string]DiskUsage
	lock     chan struct{} // kênh dung lượng 1: giữ khoá = gửi được vào kênh
}

type fakeHandler struct {
	prefix []string
	fn     func(cmd Cmd) Result
}

// defaultDisk là dung lượng StatFS trả khi test chưa gọi SetDisk:
// 100 GiB, trống 60 GiB, đủ để mọi phép kiểm dung lượng đều đạt.
var defaultDisk = DiskUsage{Total: 100 << 30, Free: 60 << 30}

// NewFake tạo một FakeSystem chỉ có thư mục gốc "/".
func NewFake() *FakeSystem {
	return &FakeSystem{
		entries: map[string]*FakeEntry{"/": {Mode: fs.ModeDir | 0o755, Owner: Root}},
		fails:   map[string]error{},
		disks:   map[string]DiskUsage{},
		lock:    make(chan struct{}, 1),
	}
}

// ---- Hàm cho test ----

// SetFile đặt sẵn một file thường, tự tạo các thư mục cha (chủ root).
func (f *FakeSystem) SetFile(p string, data []byte, perm fs.FileMode, owner Owner) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p = clean(p)
	f.mkParents(p)
	f.entries[p] = &FakeEntry{Mode: keepMode(perm), Owner: normOwner(owner), Data: string(data)}
}

// SetDir đặt sẵn một thư mục, tự tạo các thư mục cha.
func (f *FakeSystem) SetDir(p string, perm fs.FileMode, owner Owner) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p = clean(p)
	f.mkParents(p)
	f.entries[p] = &FakeEntry{Mode: fs.ModeDir | keepMode(perm), Owner: normOwner(owner)}
}

// SetSymlink đặt sẵn một symlink, tự tạo các thư mục cha.
func (f *FakeSystem) SetSymlink(target, link string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	link = clean(link)
	f.mkParents(link)
	f.entries[link] = &FakeEntry{Mode: fs.ModeSymlink | 0o777, Owner: Root, Target: target}
}

// SetDisk đặt dung lượng StatFS trả về cho mọi path nằm dưới path này. Nhiều
// mục khớp thì mục dài nhất thắng, như điểm gắn (mount point) thật.
func (f *FakeSystem) SetDisk(p string, du DiskUsage) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.disks[clean(p)] = du
}

// Handle đăng ký cách trả lời cho các lệnh có argv bắt đầu bằng prefix.
// Hai handler cùng độ dài prefix cùng khớp thì handler đăng ký sau thắng,
// để test ghi đè được handler chung.
func (f *FakeSystem) Handle(prefix []string, fn func(cmd Cmd) Result) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers = append(f.handlers, fakeHandler{prefix: slices.Clone(prefix), fn: fn})
}

// Fail làm cho thao tác op trên đúng đường dẫn path trả về err.
// op là một trong: "write", "remove", "rename", "mkdir", "symlink", "chown", "chmod".
func (f *FakeSystem) Fail(op, p string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fails[op+"\x00"+clean(p)] = err
}

// Ran trả về các lệnh đã gọi qua Run, theo thứ tự.
func (f *FakeSystem) Ran() []Cmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.ran)
}

// ResetRan xoá danh sách lệnh đã gọi.
func (f *FakeSystem) ResetRan() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ran = nil
}

// Snapshot chụp toàn bộ hệ thống file giả, kể cả "/".
func (f *FakeSystem) Snapshot() map[string]FakeEntry {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[string]FakeEntry, len(f.entries))
	for p, e := range f.entries {
		out[p] = *e
	}
	return out
}

// ---- System ----

// Run ghi lại lệnh rồi trả lời bằng handler khớp prefix dài nhất.
func (f *FakeSystem) Run(ctx context.Context, cmd Cmd) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{ExitCode: -1}, fmt.Errorf("chạy %s: %w", cmd.String(), err)
	}
	argv := append([]string{cmd.Name}, cmd.Args...)

	f.mu.Lock()
	rec := cmd
	rec.Args = slices.Clone(cmd.Args)
	rec.Stdin = slices.Clone(cmd.Stdin)
	f.ran = append(f.ran, rec)
	var fn func(Cmd) Result
	best := 0
	for _, h := range f.handlers {
		if len(h.prefix) >= best && len(h.prefix) <= len(argv) && slices.Equal(h.prefix, argv[:len(h.prefix)]) {
			fn, best = h.fn, len(h.prefix)
		}
	}
	f.mu.Unlock()

	// Gọi handler và OnOutput khi đã nhả khoá: handler được phép gọi SetFile,
	// SetDir... trên chính fake này.
	var res Result
	if fn != nil {
		res = fn(cmd)
	}
	if cmd.OnOutput != nil {
		for _, s := range []string{res.Stdout, res.Stderr} {
			for _, line := range splitLines(s) {
				cmd.OnOutput(line)
			}
		}
	}
	if res.ExitCode != 0 {
		return res, &ExitError{Cmd: cmd.String(), Code: res.ExitCode, Stderr: lastLines(res.Stderr, stderrTail)}
	}
	return res, nil
}

func (f *FakeSystem) ReadFile(p string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, e, err := f.lookup("open", p, true)
	if err != nil {
		return nil, err
	}
	if e.Mode.IsDir() {
		return nil, &fs.PathError{Op: "read", Path: r, Err: syscall.EISDIR}
	}
	return []byte(e.Data), nil
}

func (f *FakeSystem) WriteFileAtomic(p string, data []byte, perm fs.FileMode, owner Owner) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p = clean(p)
	if err := f.failed("write", p); err != nil {
		return err
	}
	// Như bản thật: thư mục cha đi theo symlink, còn tên cuối thì không
	// (rename thay chính symlink chứ không ghi vào đích của nó).
	dir, err := f.dirOf("open", p)
	if err != nil {
		return err
	}
	target := path.Join(dir, path.Base(p))
	if e, ok := f.entries[target]; ok && e.Mode.IsDir() {
		return &fs.PathError{Op: "rename", Path: p, Err: syscall.EISDIR}
	}
	f.entries[target] = &FakeEntry{Mode: keepMode(perm), Owner: normOwner(owner), Data: string(data)}
	return nil
}

func (f *FakeSystem) MkdirAll(p string, perm fs.FileMode, owner Owner) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p = clean(p)
	// Tìm các thư mục còn thiếu trước, kiểm Fail cho cả loạt, rồi mới tạo:
	// lỗi giữa chừng thì hệ thống file giả không bị đổi.
	var missing []string
	cur := "/"
	for _, name := range splitPath(p) {
		next := path.Join(cur, name)
		r, e, err := f.lookup("mkdir", next, true)
		switch {
		case err == nil && !e.Mode.IsDir():
			return &fs.PathError{Op: "mkdir", Path: next, Err: syscall.ENOTDIR}
		case err == nil:
			cur = r
		case isNotExist(err):
			missing = append(missing, next)
			cur = next
		default:
			return err
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

func (f *FakeSystem) Remove(p string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p = clean(p)
	if err := f.failed("remove", p); err != nil {
		return err
	}
	r, e, err := f.lookup("remove", p, false)
	if err != nil {
		return err
	}
	if e.Mode.IsDir() && (r == "/" || f.hasChildren(r)) {
		return &fs.PathError{Op: "remove", Path: p, Err: syscall.ENOTEMPTY}
	}
	delete(f.entries, r)
	return nil
}

func (f *FakeSystem) Rename(oldPath, newPath string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	oldPath, newPath = clean(oldPath), clean(newPath)
	for _, p := range []string{oldPath, newPath} {
		if err := f.failed("rename", p); err != nil {
			return err
		}
	}
	src, e, err := f.lookup("rename", oldPath, false)
	if err != nil {
		return err
	}
	dir, err := f.dirOf("rename", newPath)
	if err != nil {
		return err
	}
	dst := path.Join(dir, path.Base(newPath))
	if dst == src {
		return nil
	}
	if e.Mode.IsDir() && strings.HasPrefix(dst, src+"/") {
		return &fs.PathError{Op: "rename", Path: newPath, Err: syscall.EINVAL}
	}
	if old, ok := f.entries[dst]; ok {
		switch {
		case old.Mode.IsDir() && !e.Mode.IsDir():
			return &fs.PathError{Op: "rename", Path: newPath, Err: syscall.EISDIR}
		case !old.Mode.IsDir() && e.Mode.IsDir():
			return &fs.PathError{Op: "rename", Path: newPath, Err: syscall.ENOTDIR}
		case old.Mode.IsDir() && f.hasChildren(dst):
			return &fs.PathError{Op: "rename", Path: newPath, Err: syscall.ENOTEMPTY}
		}
	}
	// Chuyển cả các mục con của thư mục. Gom trước rồi mới sửa map, vì thêm
	// khoá vào map đang duyệt thì Go không hứa có duyệt tới khoá mới hay không.
	var children []string
	for p := range f.entries {
		if strings.HasPrefix(p, src+"/") {
			children = append(children, p)
		}
	}
	for _, p := range children {
		f.entries[dst+strings.TrimPrefix(p, src)] = f.entries[p]
		delete(f.entries, p)
	}
	delete(f.entries, src)
	f.entries[dst] = e
	return nil
}

func (f *FakeSystem) Symlink(target, link string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	link = clean(link)
	if err := f.failed("symlink", link); err != nil {
		return err
	}
	dir, err := f.dirOf("symlink", link)
	if err != nil {
		return err
	}
	p := path.Join(dir, path.Base(link))
	if _, ok := f.entries[p]; ok {
		return &fs.PathError{Op: "symlink", Path: link, Err: syscall.EEXIST}
	}
	f.entries[p] = &FakeEntry{Mode: fs.ModeSymlink | 0o777, Owner: Root, Target: target}
	return nil
}

func (f *FakeSystem) Readlink(p string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, e, err := f.lookup("readlink", p, false)
	if err != nil {
		return "", err
	}
	if e.Mode&fs.ModeSymlink == 0 {
		return "", &fs.PathError{Op: "readlink", Path: p, Err: syscall.EINVAL}
	}
	return e.Target, nil
}

func (f *FakeSystem) Stat(p string) (FileInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, e, err := f.lookup("lstat", p, false)
	if err != nil {
		return FileInfo{}, err
	}
	size := int64(len(e.Data))
	if e.Mode&fs.ModeSymlink != 0 {
		size = int64(len(e.Target)) // như lstat: kích thước symlink là độ dài đích
	}
	return FileInfo{Mode: e.Mode, Size: size, Owner: e.Owner}, nil
}

func (f *FakeSystem) ReadDir(p string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, e, err := f.lookup("open", p, true)
	if err != nil {
		return nil, err
	}
	if !e.Mode.IsDir() {
		return nil, &fs.PathError{Op: "readdirent", Path: p, Err: syscall.ENOTDIR}
	}
	var names []string
	for q := range f.entries {
		if q != "/" && path.Dir(q) == r {
			names = append(names, path.Base(q))
		}
	}
	sort.Strings(names)
	return names, nil
}

func (f *FakeSystem) Chown(p string, owner Owner) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p = clean(p)
	if err := f.failed("chown", p); err != nil {
		return err
	}
	_, e, err := f.lookup("lchown", p, false)
	if err != nil {
		return err
	}
	e.Owner = normOwner(owner)
	return nil
}

func (f *FakeSystem) Chmod(p string, perm fs.FileMode) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p = clean(p)
	if err := f.failed("chmod", p); err != nil {
		return err
	}
	_, e, err := f.lookup("chmod", p, true) // os.Chmod đi theo symlink
	if err != nil {
		return err
	}
	e.Mode = e.Mode.Type() | keepMode(perm)
	return nil
}

func (f *FakeSystem) StatFS(p string) (DiskUsage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, _, err := f.lookup("statfs", p, true)
	if err != nil {
		return DiskUsage{}, err
	}
	du, best := defaultDisk, -1
	for mount, d := range f.disks {
		if (r == mount || mount == "/" || strings.HasPrefix(r, mount+"/")) && len(mount) > best {
			du, best = d, len(mount)
		}
	}
	return du, nil
}

func (f *FakeSystem) Lock(ctx context.Context) (func(), error) {
	select {
	case f.lock <- struct{}{}:
	case <-ctx.Done():
		return nil, fmt.Errorf("chờ khoá tpan: %w", ctx.Err())
	}
	var once sync.Once
	return func() { once.Do(func() { <-f.lock }) }, nil
}

// ---- Bên trong ----

// maxSymlinks giống giới hạn của Linux: quá số lần này thì coi là vòng lặp.
const maxSymlinks = 40

// lookup tìm mục ở p, đi theo symlink ở các thư mục giữa, và ở tên cuối nếu
// followLast. Trả về đường dẫn thật đã giải và mục tìm được.
// Gọi khi đang giữ f.mu.
func (f *FakeSystem) lookup(op, p string, followLast bool) (string, *FakeEntry, error) {
	parts := splitPath(clean(p))
	cur, e := "/", f.entries["/"]
	for i, hops := 0, 0; i < len(parts); i++ {
		next := path.Join(cur, parts[i])
		ne, ok := f.entries[next]
		if !ok {
			return "", nil, &fs.PathError{Op: op, Path: p, Err: fs.ErrNotExist}
		}
		last := i == len(parts)-1
		if ne.Mode&fs.ModeSymlink != 0 && (!last || followLast) {
			if hops++; hops > maxSymlinks {
				return "", nil, &fs.PathError{Op: op, Path: p, Err: syscall.ELOOP}
			}
			// Thay link bằng đích của nó rồi đi lại từ gốc.
			t := ne.Target
			if !path.IsAbs(t) {
				t = path.Join(cur, t)
			}
			parts = append(splitPath(clean(t)), parts[i+1:]...)
			cur, e, i = "/", f.entries["/"], -1
			continue
		}
		if !last && !ne.Mode.IsDir() {
			return "", nil, &fs.PathError{Op: op, Path: p, Err: syscall.ENOTDIR}
		}
		cur, e = next, ne
	}
	return cur, e, nil
}

// dirOf trả về đường dẫn thật của thư mục cha của p; thư mục cha phải có sẵn.
func (f *FakeSystem) dirOf(op, p string) (string, error) {
	dir, e, err := f.lookup(op, path.Dir(p), true)
	if err != nil {
		return "", &fs.PathError{Op: op, Path: p, Err: unwrapPathErr(err)}
	}
	if !e.Mode.IsDir() {
		return "", &fs.PathError{Op: op, Path: p, Err: syscall.ENOTDIR}
	}
	return dir, nil
}

// mkParents tạo các thư mục cha còn thiếu của p (0755, root). Dùng cho SetFile...
func (f *FakeSystem) mkParents(p string) {
	for d := path.Dir(p); d != "/"; d = path.Dir(d) {
		if _, ok := f.entries[d]; !ok {
			f.entries[d] = &FakeEntry{Mode: fs.ModeDir | 0o755, Owner: Root}
		}
	}
}

func (f *FakeSystem) hasChildren(dir string) bool {
	for q := range f.entries {
		if q != "/" && path.Dir(q) == dir {
			return true
		}
	}
	return false
}

func (f *FakeSystem) failed(op, p string) error {
	if err, ok := f.fails[op+"\x00"+p]; ok {
		return &fs.PathError{Op: op, Path: p, Err: err}
	}
	return nil
}

func clean(p string) string { return path.Clean("/" + p) }

func splitPath(p string) []string {
	if p == "/" {
		return nil
	}
	return strings.Split(strings.TrimPrefix(p, "/"), "/")
}

// normOwner lưu Owner rỗng thành root:root, đúng như LocalSystem hiểu nó, để
// Snapshot trước và sau Undo so được với nhau.
func normOwner(o Owner) Owner {
	if o.User == "" {
		o.User = "root"
	}
	if o.Group == "" {
		o.Group = "root"
	}
	return o
}

func isNotExist(err error) bool { return errors.Is(err, fs.ErrNotExist) }

func unwrapPathErr(err error) error {
	if pe := (*fs.PathError)(nil); errors.As(err, &pe) {
		return pe.Err
	}
	return err
}

// keepMode giữ 9 bit quyền cùng setuid, setgid, sticky, như hệ thống file thật.
// Thư mục site dùng setgid (2750, mục 5) để file mới sinh ra mang nhóm www-data;
// bỏ bit này thì test không phân biệt được 2750 với 0750.
func keepMode(perm fs.FileMode) fs.FileMode {
	return perm & (fs.ModePerm | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky)
}
