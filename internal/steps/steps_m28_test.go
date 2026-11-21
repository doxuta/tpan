package steps

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"syscall"
	"testing"

	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/sitefs"
	"github.com/doxuta/tpan/internal/system"
)

// Test của M28 (mục 3.6 của docs/thay-doi/M28-thiet-ke.md): đặt quyền thư mục
// sau khi giải nén WordPress không chạy lệnh root nào trên thư mục site, và
// không bao giờ đổi quyền thứ nằm ngoài site.

// WPFiles không còn chạy lệnh nào bằng root: tar và chmod file chạy dưới user
// của site, chmod thư mục đi qua sys.Chmod (cửa sitefs trên máy thật).
func TestWPFilesKhongChayLenhRoot(t *testing.T) {
	m := wpNewSim(t)
	if err := (&WPFiles{Layout: m.l, Archive: wpArchivePath()}).Apply(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	for _, c := range m.Ran() {
		if c.AsUser == "" {
			t.Errorf("lệnh %s chạy bằng root trên thư mục site", c.String())
		}
	}
	for _, line := range (&WPFiles{Layout: m.l, Archive: wpArchivePath()}).Info().Commands {
		if strings.HasPrefix(line, "find ") {
			t.Errorf("Info còn lệnh find chạy bằng root: %s", line)
		}
	}
}

// chmodDirs bỏ qua symlink (kể cả symlink tới thư mục ngoài site), đặt đúng
// 2750 cho thư mục thật, không đổi quyền chính root.
func TestWPFilesDatQuyenQuaCua(t *testing.T) {
	f := system.NewFake()
	owner := system.Owner{User: "tp_a_1", Group: site.WebGroup}
	root := "/var/www/a.example.com/public"
	f.SetDir("/var/www/a.example.com", fs.ModeSetgid|0o750, owner)
	f.SetDir(root, fs.ModeSetgid|0o750, owner)
	f.SetDir(root+"/wp-includes/js", 0o755, owner)
	f.SetDir(root+"/wp-admin", 0o755, owner)
	f.SetFile(root+"/index.php", []byte("<?php\n"), 0o644, owner)
	f.SetDir("/srv/nan-nhan", 0o755, system.Root)
	f.SetDir("/srv/nan-nhan/con", 0o755, system.Root)
	f.SetSymlink("/srv/nan-nhan", root+"/wp-includes/lien")

	if err := chmodDirs(context.Background(), f, root, wpDirMode); err != nil {
		t.Fatal(err)
	}
	snap := f.Snapshot()
	want := fs.ModeDir | fs.ModeSetgid | 0o750
	for _, p := range []string{root + "/wp-includes", root + "/wp-includes/js", root + "/wp-admin"} {
		if snap[p].Mode != want {
			t.Errorf("%s: %v, muốn %v", p, snap[p].Mode, want)
		}
	}
	for _, p := range []string{"/srv/nan-nhan", "/srv/nan-nhan/con"} {
		if snap[p].Mode != fs.ModeDir|0o755 {
			t.Errorf("thư mục ngoài site %s bị đổi quyền: %v", p, snap[p].Mode)
		}
	}
	if snap[root+"/index.php"].Mode != 0o644 {
		t.Errorf("file bị chmodDirs đụng: %v", snap[root+"/index.php"].Mode)
	}
}

// Mục biến mất hay bị tráo thành symlink giữa lúc liệt kê và lúc chmod thì bỏ
// qua; lỗi khác làm hỏng bước.
func TestChmodDirsLoi(t *testing.T) {
	setup := func() (*system.FakeSystem, string) {
		f := system.NewFake()
		owner := system.Owner{User: "tp_a_1", Group: site.WebGroup}
		root := "/var/www/a.example.com/public"
		f.SetDir("/var/www/a.example.com", fs.ModeSetgid|0o750, owner)
		f.SetDir(root+"/a", 0o755, owner)
		f.SetDir(root+"/b", 0o755, owner)
		return f, root
	}
	// ELOOP là đúng lỗi e2e A7 bắt được trên máy ảo (vòng 4, nginx).
	for _, skip := range []error{fs.ErrNotExist, sitefs.ErrSymlink, syscall.ELOOP, syscall.ENOTDIR} {
		f, root := setup()
		f.Fail("chmod", root+"/a", &sitefs.Error{Op: "đổi quyền", Path: root + "/a", Err: skip})
		if err := chmodDirs(context.Background(), f, root, wpDirMode); err != nil {
			t.Errorf("chmod lỗi %v phải được bỏ qua: %v", skip, err)
		}
		if f.Snapshot()[root+"/b"].Mode != fs.ModeDir|fs.ModeSetgid|0o750 {
			t.Errorf("bỏ qua a thì vẫn phải làm b")
		}
	}
	f, root := setup()
	f.Fail("chmod", root+"/a", fs.ErrPermission)
	if err := chmodDirs(context.Background(), f, root, wpDirMode); !errors.Is(err, fs.ErrPermission) {
		t.Errorf("lỗi khác phải làm hỏng bước: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := chmodDirs(ctx, f, root, wpDirMode); !errors.Is(err, context.Canceled) {
		t.Errorf("ctx đã huỷ: err = %v", err)
	}
}

// Thư mục con bị tráo giữa lúc chmod nó và lúc liệt kê bên trong (ReadDir trả
// ErrSymlink, ENOENT, ENOTDIR, ELOOP): bỏ qua, không làm hỏng bước (e2e M28 A7
// bắt được trên máy ảo). Thư mục gốc không liệt kê được thì vẫn là lỗi, và lỗi
// khác ở thư mục con (EACCES) cũng vậy.
type m28ReadDirSys struct {
	*system.FakeSystem
	fail map[string]error
}

func (s *m28ReadDirSys) ReadDir(p string) ([]string, error) {
	if err := s.fail[p]; err != nil {
		return nil, err
	}
	return s.FakeSystem.ReadDir(p)
}

func TestChmodDirsThuMucConBiTrao(t *testing.T) {
	owner := system.Owner{User: "tp_a_1", Group: site.WebGroup}
	root := "/var/www/a.example.com/public"
	for _, e := range []error{sitefs.ErrSymlink, fs.ErrNotExist, syscall.ENOTDIR, syscall.ELOOP} {
		f := system.NewFake()
		f.SetDir("/var/www/a.example.com", fs.ModeSetgid|0o750, owner)
		f.SetDir(root+"/a/b", 0o755, owner)
		f.SetDir(root+"/c", 0o755, owner)
		s := &m28ReadDirSys{FakeSystem: f, fail: map[string]error{root + "/a": &sitefs.Error{Op: "liệt kê", Path: root + "/a", Err: e}}}
		if err := chmodDirs(context.Background(), s, root, wpDirMode); err != nil {
			t.Errorf("ReadDir thư mục con lỗi %v phải được bỏ qua: %v", e, err)
		}
		if f.Snapshot()[root+"/c"].Mode != fs.ModeDir|fs.ModeSetgid|0o750 {
			t.Errorf("bỏ qua a thì vẫn phải làm c")
		}
	}
	f := system.NewFake()
	f.SetDir(root, fs.ModeSetgid|0o750, owner)
	s := &m28ReadDirSys{FakeSystem: f, fail: map[string]error{root: &sitefs.Error{Op: "liệt kê", Path: root, Err: sitefs.ErrSymlink}}}
	if err := chmodDirs(context.Background(), s, root, wpDirMode); !errors.Is(err, sitefs.ErrSymlink) {
		t.Errorf("thư mục gốc không liệt kê được phải là lỗi: %v", err)
	}
	f = system.NewFake()
	f.SetDir("/var/www/a.example.com", fs.ModeSetgid|0o750, owner)
	f.SetDir(root+"/a", 0o755, owner)
	s = &m28ReadDirSys{FakeSystem: f, fail: map[string]error{root + "/a": &sitefs.Error{Op: "liệt kê", Path: root + "/a", Err: fs.ErrPermission}}}
	if err := chmodDirs(context.Background(), s, root, wpDirMode); !errors.Is(err, fs.ErrPermission) {
		t.Errorf("lỗi khác ở thư mục con phải làm hỏng bước: %v", err)
	}
}

// backupPublic: public là symlink hay không phải thư mục thì trả lỗi của cửa
// (*sitefs.Error), để API ra 409 site_path_unsafe chứ không 500 (soát cuối
// M28, C14: máy ảo ra 500 internal cho backup và khôi phục dry-run).
func TestBackupPublicLoiCuaCua(t *testing.T) {
	owner := system.Owner{User: "tp_e_1", Group: site.WebGroup}
	src := "/var/www/e.example.com"
	for _, tc := range []struct {
		name string
		set  func(f *system.FakeSystem)
		want error
	}{
		{"symlink", func(f *system.FakeSystem) { f.SetSymlink("/var/www/a.example.com/public", src+"/public") }, sitefs.ErrSymlink},
		{"file", func(f *system.FakeSystem) { f.SetFile(src+"/public", []byte("x"), 0o640, owner) }, syscall.ENOTDIR},
	} {
		f := system.NewFake()
		f.SetDir(src, fs.ModeSetgid|0o750, owner)
		f.SetDir("/var/www/a.example.com/public", fs.ModeSetgid|0o750, system.Owner{User: "tp_a_1", Group: site.WebGroup})
		tc.set(f)
		_, err := backupPublic(f, src)
		var se *sitefs.Error
		if !errors.As(err, &se) || !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, muốn *sitefs.Error bọc %v", tc.name, err, tc.want)
		}
	}
	f := system.NewFake()
	f.SetDir(src+"/public", fs.ModeSetgid|0o750, owner)
	if p, err := backupPublic(f, src); err != nil || p != src+"/public" {
		t.Errorf("public là thư mục thật: %q, %v", p, err)
	}
}

// File.Check không đọc file khác cỡ: file trong thư mục site do site ghi nên
// có thể lớn tuỳ ý.
type m28CountSys struct {
	*system.FakeSystem
	reads int
}

func (s *m28CountSys) ReadFile(p string) ([]byte, error) {
	s.reads++
	return s.FakeSystem.ReadFile(p)
}

func TestFileCheckKhongDocKhiKhacCo(t *testing.T) {
	s := &m28CountSys{FakeSystem: system.NewFake()}
	owner := system.Owner{User: "tp_a_1", Group: site.WebGroup}
	p := "/var/www/a.example.com/public/.htaccess"
	s.SetDir("/var/www/a.example.com/public", fs.ModeSetgid|0o750, owner)
	s.SetFile(p, []byte(strings.Repeat("x", 4096)), 0o640, owner)
	st := &File{Path: p, Content: []byte("# BEGIN WordPress\n"), Perm: 0o640, Owner: owner}
	done, err := st.Check(context.Background(), s)
	if err != nil || done {
		t.Fatalf("Check = %v, %v", done, err)
	}
	if s.reads != 0 {
		t.Errorf("Check đọc file khác cỡ %d lần", s.reads)
	}
	// Cùng cỡ, cùng nội dung: vẫn đọc để so, và đạt.
	s.SetFile(p, st.Content, 0o640, owner)
	if done, err := st.Check(context.Background(), s); err != nil || !done {
		t.Fatalf("Check cùng nội dung = %v, %v", done, err)
	}
}
