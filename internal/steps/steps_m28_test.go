package steps

import (
	"context"
	"errors"
	"io/fs"
	"strings"
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
	for _, skip := range []error{fs.ErrNotExist, sitefs.ErrSymlink} {
		f, root := setup()
		f.Fail("chmod", root+"/a", skip)
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
