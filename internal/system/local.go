package system

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/doxuta/tpan/internal/sitefs"
)

// fixedPATH là PATH duy nhất lệnh của tpan thấy. Cố định để kết quả không phụ
// thuộc vào shell của người gõ lệnh (PATH của root qua sudo, cron, systemd khác nhau).
const fixedPATH = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

// DefaultLockPath là file khoá toàn cục giữa các tiến trình tpan (mục 4.2).
// /run là tmpfs: khoá tự mất khi khởi động lại máy, không bao giờ bị "kẹt".
const DefaultLockPath = "/run/tpan/tpan.lock"

// stderrTail là số dòng cuối của stderr giữ trong ExitError: đủ để thấy lý do
// lỗi, không kéo cả trang log của apt-get vào thông báo.
const stderrTail = 5

// LocalSystem chạy lệnh thật và ghi file thật. Dùng trên máy chủ.
type LocalSystem struct {
	// LockPath là file dùng cho Lock. NewLocal đặt DefaultLockPath; test đổi
	// sang một file trong t.TempDir() vì máy dev không có /run/tpan.
	LockPath string
	// SiteRoot là gốc của vùng site (M28, mục 3.3 của M28-thiet-ke.md): mọi
	// thao tác file có đường dẫn mà sitefs.Split(SiteRoot, p) nhận đi qua cửa
	// sitefs, và Run từ chối lệnh root nhận đường dẫn trong đó (mục 3.4).
	// NewLocal đặt sitefs.Root; test đặt một thư mục tạm. Rỗng: không có vùng.
	SiteRoot string
}

// NewLocal tạo LocalSystem với khoá ở DefaultLockPath và vùng site ở sitefs.Root.
func NewLocal() *LocalSystem {
	return &LocalSystem{LockPath: DefaultLockPath, SiteRoot: sitefs.Root}
}

// Run chạy cmd bằng argv, không qua shell. Xem System.Run.
func (s *LocalSystem) Run(ctx context.Context, cmd Cmd) (Result, error) {
	if err := checkRootZone(s.SiteRoot, cmd); err != nil {
		return Result{ExitCode: -1}, err
	}
	c, err := command(ctx, cmd)
	if err != nil {
		return Result{ExitCode: -1}, err
	}
	// Mỗi lệnh một nhóm tiến trình riêng, và huỷ thì giết cả nhóm (S1):
	// runuser fork ra lệnh thật rồi đứng chờ; Go chỉ giết runuser thì lệnh
	// con ở lại, giữ ống stdout và nằm mãi trong cgroup của tpan.
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error {
		err := syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	if len(cmd.Stdin) > 0 {
		c.Stdin = bytes.NewReader(cmd.Stdin)
	}
	// Một mutex chung cho hai luồng: OnOutput không bao giờ bị gọi song song,
	// nên người viết OnOutput không phải tự khoá.
	var mu sync.Mutex
	stdout := &lineWriter{mu: &mu, fn: cmd.OnOutput}
	stderr := &lineWriter{mu: &mu, fn: cmd.OnOutput}
	c.Stdout, c.Stderr = stdout, stderr
	// Lệnh bị huỷ mà để lại tiến trình con giữ pipe (ví dụ dịch vụ tự fork)
	// thì Wait không chờ quá thời gian này.
	c.WaitDelay = 5 * time.Second

	runErr := c.Run()
	stdout.flush()
	stderr.flush()
	res := Result{Stdout: stdout.buf.String(), Stderr: stderr.buf.String()}
	if runErr == nil {
		return res, nil
	}
	res.ExitCode = -1
	// Kiểm ctx trước: tiến trình bị giết vì ctx cũng trả *exec.ExitError,
	// nhưng hợp đồng coi đó là "không chạy xong", không phải mã thoát.
	if ctxErr := ctx.Err(); ctxErr != nil {
		return res, fmt.Errorf("chạy %s: %w", cmd.String(), ctxErr)
	}
	var ee *exec.ExitError
	if errors.As(runErr, &ee) {
		res.ExitCode = ee.ExitCode()
		return res, &ExitError{Cmd: cmd.String(), Code: res.ExitCode, Stderr: lastLines(res.Stderr, stderrTail)}
	}
	return res, fmt.Errorf("chạy %s: %w", cmd.String(), runErr)
}

// command dựng exec.Cmd cho cmd: argv (bọc runuser nếu AsUser), môi trường cố
// định cộng cmd.Env, thư mục làm việc.
func command(ctx context.Context, cmd Cmd) (*exec.Cmd, error) {
	name, args := runArgv(cmd)
	env := mergeEnv(fixedEnv(), cmd.Env)
	bin, err := lookPath(name, envValue(env, "PATH"))
	if err != nil {
		return nil, fmt.Errorf("chạy %s: %w", cmd.String(), err)
	}
	c := exec.CommandContext(ctx, bin, args...)
	c.Args[0] = name // để ps hiện tên ngắn, như khi gõ tay
	c.Env = env
	c.Dir = cmd.Dir
	if c.Dir == "" {
		c.Dir = "/"
	}
	return c, nil
}

// mergeEnv trả base cộng extra; biến trùng tên thì bản trong extra thắng.
func mergeEnv(base, extra []string) []string {
	if len(extra) == 0 {
		return base
	}
	out := make([]string, 0, len(base)+len(extra))
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if envValue(extra, k) == "" {
			out = append(out, kv)
		}
	}
	return append(out, extra...)
}

// envValue là giá trị của biến key trong env (rỗng khi không có).
func envValue(env []string, key string) string {
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && k == key {
			return v
		}
	}
	return ""
}

// Pipe là một chương trình đang chạy với stdin và stdout nối ống, cho kết nối
// sống lâu hai chiều (`tpan brain` giữ `ssh <máy> tpan brain-link`). stderr
// đi thẳng ra stderr của tpan để người vận hành thấy lỗi của ssh.
type Pipe struct {
	Stdin  io.WriteCloser
	Stdout io.ReadCloser
	cmd    *exec.Cmd
}

// Wait chờ chương trình thoát. Mã thoát khác 0 là *ExitError.
func (p *Pipe) Wait() error {
	err := p.cmd.Wait()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return &ExitError{Cmd: strings.Join(p.cmd.Args, " "), Code: ee.ExitCode()}
	}
	return err
}

// StartPipe chạy cmd (argv, không shell, như Run; bỏ qua Stdin và OnOutput)
// và trả ống stdin, stdout của nó. ctx bị huỷ thì chương trình bị giết. Chỉ
// dùng ở máy của người vận hành; máy chủ không cần lệnh nào sống lâu kiểu này.
func StartPipe(ctx context.Context, cmd Cmd) (*Pipe, error) {
	c, err := command(ctx, cmd)
	if err != nil {
		return nil, err
	}
	c.Stderr = os.Stderr
	c.WaitDelay = 5 * time.Second
	in, err := c.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("chạy %s: %w", cmd.String(), err)
	}
	out, err := c.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("chạy %s: %w", cmd.String(), err)
	}
	if err := c.Start(); err != nil {
		return nil, fmt.Errorf("chạy %s: %w", cmd.String(), err)
	}
	return &Pipe{Stdin: in, Stdout: out, cmd: c}, nil
}

// runArgv trả về chương trình và tham số thật sẽ chạy: với AsUser thì bọc
// trong `runuser -u <user> --`. Dấu "--" chặn việc tên lệnh bị hiểu là cờ của runuser.
func runArgv(cmd Cmd) (string, []string) {
	if cmd.AsUser == "" {
		return cmd.Name, cmd.Args
	}
	return "runuser", append([]string{"-u", cmd.AsUser, "--", cmd.Name}, cmd.Args...)
}

// lookPath tìm chương trình trong path (PATH của môi trường lệnh con, mặc định
// là fixedPATH). Không dùng exec.LookPath(name) vì nó đọc PATH của tiến trình
// tpan, không phải PATH của lệnh con.
func lookPath(name, path string) (string, error) {
	if strings.Contains(name, "/") {
		return exec.LookPath(name)
	}
	for _, dir := range filepath.SplitList(path) {
		if p, err := exec.LookPath(filepath.Join(dir, name)); err == nil {
			return p, nil
		}
	}
	return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
}

// fixedEnv là toàn bộ môi trường của lệnh con. LC_ALL=C để output là tiếng
// Anh ổn định (phép kiểm đọc output); DEBIAN_FRONTEND=noninteractive để
// apt-get không bao giờ dừng lại hỏi.
func fixedEnv() []string {
	env := []string{"PATH=" + fixedPATH, "LC_ALL=C", "DEBIAN_FRONTEND=noninteractive"}
	// Chạy bằng root (trên máy chủ) thì thêm HOME: nhiều công cụ (composer,
	// wp-cli, git) lỗi hoặc ghi cache bừa khi thiếu HOME. Lệnh chạy qua runuser
	// tự được runuser đặt lại HOME của user đích.
	if os.Geteuid() == 0 {
		env = append(env, "HOME=/root")
	}
	return env
}

// lineWriter gom output và gọi fn cho từng dòng trọn vẹn ngay khi có.
type lineWriter struct {
	mu   *sync.Mutex
	fn   func(string)
	buf  bytes.Buffer
	part []byte // phần dòng chưa gặp "\n"
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf.Write(p)
	if w.fn == nil {
		return len(p), nil
	}
	w.part = append(w.part, p...)
	for {
		i := bytes.IndexByte(w.part, '\n')
		if i < 0 {
			break
		}
		w.fn(string(w.part[:i]))
		w.part = w.part[i+1:]
	}
	return len(p), nil
}

// flush đẩy nốt dòng cuối không có "\n".
func (w *lineWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fn != nil && len(w.part) > 0 {
		w.fn(string(w.part))
	}
	w.part = nil
}

// WriteFileAtomic ghi ra file tạm cùng thư mục, fsync, đặt quyền và chủ, rồi
// rename. rename trong cùng phân vùng là nguyên tử: người đọc chỉ thấy file cũ
// hoặc file mới, không bao giờ thấy file ghi dở.
func (s *LocalSystem) WriteFileAtomic(path string, data []byte, perm fs.FileMode, owner Owner) (err error) {
	uid, gid, err := lookupOwner(owner)
	if err != nil {
		return fmt.Errorf("ghi %s: %w", path, err)
	}
	if a, r, ok := sitefs.Split(s.SiteRoot, path); ok {
		return sitefs.WriteFileAtomic(a, r, data, perm, uid, gid)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tpan-*")
	if err != nil {
		return fmt.Errorf("ghi %s: %w", path, err)
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		return fmt.Errorf("ghi %s: %w", path, err)
	}
	// fsync trước rename: mất điện ngay sau rename vẫn không ra file rỗng.
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("fsync %s: %w", path, err)
	}
	// Chmod tường minh vì CreateTemp tạo 0600 và umask có thể cắt perm.
	if err = tmp.Chmod(perm); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	if err = tmp.Chown(uid, gid); err != nil {
		return fmt.Errorf("chown %s: %w", path, err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("ghi %s: %w", path, err)
	}
	if err = os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("ghi %s: %w", path, err)
	}
	return nil
}

// OpenRead mở file bằng O_NOFOLLOW (thành phần cuối là symlink thì open lỗi
// ELOOP) rồi fstat trên chính fd đã mở: không có khoảng hở giữa lúc kiểm và
// lúc đọc để ai tráo file.
func (s *LocalSystem) OpenRead(path string) (io.ReadCloser, int64, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, 0, fmt.Errorf("mở %s để đọc: là symlink, không đi theo: %w", path, err)
		}
		return nil, 0, fmt.Errorf("mở %s để đọc: %w", path, err)
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, 0, fmt.Errorf("stat %s: %w", path, err)
	}
	if !fi.Mode().IsRegular() {
		_ = f.Close()
		return nil, 0, fmt.Errorf("mở %s để đọc: không phải file thường (%v)", path, fi.Mode().Type())
	}
	return f, fi.Size(), nil
}

// CreateFile tạo file mới bằng O_CREAT|O_EXCL|O_NOFOLLOW (đã có, kể cả là
// symlink, thì lỗi EEXIST), đặt quyền và chủ trên fd trước byte đầu tiên.
func (s *LocalSystem) CreateFile(path string, perm fs.FileMode, owner Owner) (io.WriteCloser, error) {
	uid, gid, err := lookupOwner(owner)
	if err != nil {
		return nil, fmt.Errorf("tạo %s: %w", path, err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("tạo %s: %w", path, err)
	}
	// Chmod tường minh vì umask cắt quyền lúc tạo.
	if err := f.Chmod(perm); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("chmod %s: %w", path, err)
	}
	if err := f.Chown(uid, gid); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("chown %s: %w", path, err)
	}
	return &syncFile{f: f}, nil
}

// syncFile fsync rồi mới đóng: tải về xong mà mất điện thì không ra file rỗng
// mang tên thật (người gọi đổi tên sau Close).
type syncFile struct{ f *os.File }

func (w *syncFile) Write(p []byte) (int, error) { return w.f.Write(p) }

func (w *syncFile) Close() error {
	serr := w.f.Sync()
	cerr := w.f.Close()
	if serr != nil {
		return fmt.Errorf("fsync %s: %w", w.f.Name(), serr)
	}
	if cerr != nil {
		return fmt.Errorf("đóng %s: %w", w.f.Name(), cerr)
	}
	return nil
}

func (s *LocalSystem) ReadFile(path string) ([]byte, error) {
	if a, r, ok := sitefs.Split(s.SiteRoot, path); ok {
		return sitefs.ReadFile(a, r, sitefs.MaxFile)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("đọc %s: %w", path, err)
	}
	return b, nil
}

// MkdirAll tạo các thư mục còn thiếu từ trên xuống; chỉ thư mục mới tạo nhận
// perm và owner.
func (s *LocalSystem) MkdirAll(path string, perm fs.FileMode, owner Owner) error {
	uid, gid, err := lookupOwner(owner)
	if err != nil {
		return fmt.Errorf("tạo thư mục %s: %w", path, err)
	}
	if a, r, ok := sitefs.Split(s.SiteRoot, path); ok {
		// Neo và mọi thứ phía trên nó thuộc root: nếu thiếu thì tạo theo cách
		// cũ, phần còn lại đi qua cổng.
		if err := mkdirAll(a, perm, uid, gid); err != nil {
			return err
		}
		return sitefs.MkdirAll(a, r, perm, uid, gid)
	}
	return mkdirAll(path, perm, uid, gid)
}

// mkdirAll là MkdirAll cho phần nằm ngoài vùng site.
func mkdirAll(path string, perm fs.FileMode, uid, gid int) error {
	var missing []string
	for p := filepath.Clean(path); ; p = filepath.Dir(p) {
		fi, err := os.Stat(p)
		if err == nil {
			if !fi.IsDir() {
				return fmt.Errorf("tạo thư mục %s: %s không phải thư mục: %w", path, p, syscall.ENOTDIR)
			}
			break
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("tạo thư mục %s: %w", path, err)
		}
		missing = append(missing, p)
		if p == filepath.Dir(p) {
			break
		}
	}
	for i := len(missing) - 1; i >= 0; i-- {
		d := missing[i]
		if err := os.Mkdir(d, perm); err != nil {
			return fmt.Errorf("tạo thư mục %s: %w", d, err)
		}
		// Mkdir bị umask cắt quyền, nên đặt lại perm tường minh.
		if err := os.Chmod(d, perm); err != nil {
			return fmt.Errorf("chmod %s: %w", d, err)
		}
		if err := os.Lchown(d, uid, gid); err != nil {
			return fmt.Errorf("chown %s: %w", d, err)
		}
	}
	return nil
}

func (s *LocalSystem) Remove(path string) error {
	if a, r, ok := sitefs.Split(s.SiteRoot, path); ok {
		return sitefs.Remove(a, r)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("xoá %s: %w", path, err)
	}
	return nil
}

// Rename: hễ một phía nằm trong vùng thì cả hai phía đi qua cổng; phía nằm
// ngoài đi dưới dạng đường dẫn tuyệt đối (AT_FDCWD).
func (s *LocalSystem) Rename(oldPath, newPath string) error {
	oa, or, oin := sitefs.Split(s.SiteRoot, oldPath)
	na, nr, nin := sitefs.Split(s.SiteRoot, newPath)
	if oin || nin {
		if !oin {
			oa, or = "", oldPath
		}
		if !nin {
			na, nr = "", newPath
		}
		return sitefs.Rename(oa, or, na, nr)
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("đổi tên %s: %w", oldPath, err)
	}
	return nil
}

func (s *LocalSystem) Symlink(target, link string) error {
	if a, r, ok := sitefs.Split(s.SiteRoot, link); ok {
		return sitefs.Symlink(target, a, r)
	}
	if err := os.Symlink(target, link); err != nil {
		return fmt.Errorf("tạo symlink %s: %w", link, err)
	}
	return nil
}

func (s *LocalSystem) Readlink(path string) (string, error) {
	if a, r, ok := sitefs.Split(s.SiteRoot, path); ok {
		return sitefs.Readlink(a, r)
	}
	t, err := os.Readlink(path)
	if err != nil {
		return "", fmt.Errorf("đọc symlink %s: %w", path, err)
	}
	return t, nil
}

// Stat dùng Lstat: không đi theo symlink.
func (s *LocalSystem) Stat(path string) (FileInfo, error) {
	if a, r, ok := sitefs.Split(s.SiteRoot, path); ok {
		in, err := sitefs.Lstat(a, r)
		if err != nil {
			return FileInfo{}, err
		}
		return FileInfo{Mode: in.Mode, Size: in.Size, Owner: ownerName(in.UID, in.GID)}, nil
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return FileInfo{}, fmt.Errorf("stat %s: %w", path, err)
	}
	info := FileInfo{Mode: fi.Mode(), Size: fi.Size()}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		info.Owner = ownerName(st.Uid, st.Gid)
	}
	return info, nil
}

// ReadDir trả tên đã sắp xếp (os.ReadDir sắp theo tên).
func (s *LocalSystem) ReadDir(path string) ([]string, error) {
	if a, r, ok := sitefs.Split(s.SiteRoot, path); ok {
		return sitefs.ReadDir(a, r, sitefs.MaxDirEntries)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("đọc thư mục %s: %w", path, err)
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return names, nil
}

func (s *LocalSystem) Chown(path string, owner Owner) error {
	uid, gid, err := lookupOwner(owner)
	if err != nil {
		return fmt.Errorf("chown %s: %w", path, err)
	}
	if a, r, ok := sitefs.Split(s.SiteRoot, path); ok {
		return sitefs.Chown(a, r, uid, gid)
	}
	if err := os.Lchown(path, uid, gid); err != nil {
		return fmt.Errorf("chown %s: %w", path, err)
	}
	return nil
}

func (s *LocalSystem) Chmod(path string, perm fs.FileMode) error {
	if a, r, ok := sitefs.Split(s.SiteRoot, path); ok {
		return sitefs.Chmod(a, r, perm)
	}
	if err := os.Chmod(path, perm); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	return nil
}

// StatFS đọc statfs. Free là Bavail (phần user thường dùng được, như cột Avail
// của df), không phải Bfree (gồm cả phần dành riêng cho root).
func (s *LocalSystem) StatFS(path string) (DiskUsage, error) {
	if a, r, ok := sitefs.Split(s.SiteRoot, path); ok {
		total, free, err := sitefs.StatFS(a, r)
		return DiskUsage{Total: total, Free: free}, err
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return DiskUsage{}, fmt.Errorf("statfs %s: %w", path, &fs.PathError{Op: "statfs", Path: path, Err: err})
	}
	// Bsize là int64 trên linux, uint32 trên darwin: đổi về uint64 cho cả hai.
	bs := uint64(st.Bsize)
	return DiskUsage{Total: st.Blocks * bs, Free: st.Bavail * bs}, nil
}

// Lock lấy flock độc quyền trên LockPath. Thử không chờ (LOCK_NB) lặp lại có
// nghỉ, vì flock chờ thật thì không huỷ được bằng ctx.
func (s *LocalSystem) Lock(ctx context.Context) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(s.LockPath), 0o700); err != nil {
		return nil, fmt.Errorf("tạo thư mục khoá: %w", err)
	}
	f, err := os.OpenFile(s.LockPath, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("mở file khoá: %w", err)
	}
	fd := int(f.Fd())
	for {
		err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			_ = f.Close()
			return nil, fmt.Errorf("flock %s: %w", s.LockPath, err)
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, fmt.Errorf("chờ khoá tpan %s: %w", s.LockPath, ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			// Đóng file là nhả flock; gọi LOCK_UN trước cho rõ ý.
			_ = syscall.Flock(fd, syscall.LOCK_UN)
			_ = f.Close()
		})
	}, nil
}

// lookupOwner đổi Owner sang uid, gid. Trường rỗng nghĩa là root.
func lookupOwner(o Owner) (int, int, error) {
	o = normOwner(o)
	u, err := user.Lookup(o.User)
	if err != nil {
		return 0, 0, fmt.Errorf("tìm user %s: %w", o.User, err)
	}
	g, err := user.LookupGroup(o.Group)
	if err != nil {
		return 0, 0, fmt.Errorf("tìm nhóm %s: %w", o.Group, err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return 0, 0, fmt.Errorf("uid của %s: %w", o.User, err)
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return 0, 0, fmt.Errorf("gid của %s: %w", o.Group, err)
	}
	return uid, gid, nil
}

// ownerName đổi uid, gid ra tên. Không tra được (user đã xoá) thì để số,
// để Stat vẫn trả kết quả thay vì lỗi.
func ownerName(uid, gid uint32) Owner {
	o := Owner{User: strconv.FormatUint(uint64(uid), 10), Group: strconv.FormatUint(uint64(gid), 10)}
	if u, err := user.LookupId(o.User); err == nil {
		o.User = u.Username
	}
	if g, err := user.LookupGroupId(o.Group); err == nil {
		o.Group = g.Name
	}
	return o
}

// splitLines tách output thành dòng, bỏ dòng rỗng do "\n" cuối cùng tạo ra.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// lastLines trả về n dòng cuối khác rỗng của s.
func lastLines(s string, n int) string {
	lines := splitLines(strings.TrimRight(s, "\n"))
	var keep []string
	for i := len(lines) - 1; i >= 0 && len(keep) < n; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			keep = append([]string{lines[i]}, keep...)
		}
	}
	return strings.Join(keep, "\n")
}
