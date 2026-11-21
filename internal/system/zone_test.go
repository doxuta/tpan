package system

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/sitefs"
)

// zoneFixture là cùng một cây, tính từ thư mục gốc site, trên cả hai system:
// neo a có public bình thường; neo b có public là symlink trỏ tới public của a;
// neo e chính nó là một symlink.
var zoneFixture = []struct{ kind, rel, data string }{
	{"dir", "a", ""},
	{"dir", "a/public", ""},
	{"file", "a/public/.htaccess", "Options -Indexes\n"},
	{"file", "a/public/wp-config.php", "<?php\n"},
	{"symlink", "a/public/l", "../../out/secret"},
	{"dir", "a/public/d", ""},
	{"file", "a/public/d/f", "f"},
	{"dir", "b", ""},
	{"symlink", "b/public", "../a/public"},
	{"symlink", "e", "a"},
}

// zoneSide là một System với fixture dựng dưới thư mục gốc site của nó, và
// một thư mục nằm ngoài vùng.
type zoneSide struct {
	name      string
	sys       System
	root, out string
}

func (z zoneSide) path(p string) string {
	if rest, ok := strings.CutPrefix(p, "@out"); ok {
		return z.out + rest
	}
	return z.root + "/" + p
}

func localSide(t *testing.T) zoneSide {
	t.Helper()
	base := t.TempDir()
	z := zoneSide{name: "local", root: filepath.Join(base, "www"), out: filepath.Join(base, "out")}
	z.sys = &LocalSystem{LockPath: filepath.Join(base, "lock"), SiteRoot: z.root}
	for _, d := range []string{z.root, z.out} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
		// macOS cho thư mục mới group của thư mục cha (wheel dưới /tmp);
		// dùng group của ta để setgid và chown chạy được mà không cần root.
		if err := os.Chown(d, os.Geteuid(), os.Getegid()); err != nil {
			t.Fatal(err)
		}
	}
	for _, e := range zoneFixture {
		p := filepath.Join(z.root, e.rel)
		var err error
		switch e.kind {
		case "dir":
			err = os.Mkdir(p, 0o750)
		case "file":
			err = os.WriteFile(p, []byte(e.data), 0o640)
		case "symlink":
			err = os.Symlink(e.data, p)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	return z
}

func fakeSide(t *testing.T) zoneSide {
	t.Helper()
	me := currentOwner(t)
	f := NewFake()
	f.SetDir("/var/www", 0o755, Root)
	f.SetDir("/srv/out", 0o755, Root)
	for _, e := range zoneFixture {
		p := "/var/www/" + e.rel
		switch e.kind {
		case "dir":
			f.SetDir(p, 0o750, me)
		case "file":
			f.SetFile(p, []byte(e.data), 0o640, me)
		case "symlink":
			f.SetSymlink(e.data, p)
		}
	}
	return zoneSide{name: "fake", sys: f, root: "/var/www", out: "/srv/out"}
}

// zoneRow là một thao tác trên một đường dẫn. want là lỗi mà cả hai system
// phải trả (nil: cả hai thành công); bản tóm tắt kết quả của hai bên cũng
// phải khớp nhau.
type zoneRow struct {
	name, op, p, arg string
	want             error
}

var zoneRows = []zoneRow{
	{"read/file", "ReadFile", "a/public/.htaccess", "", nil},
	{"read/symlink-cuoi", "ReadFile", "a/public/l", "", sitefs.ErrSymlink},
	{"read/cha-symlink", "ReadFile", "b/public/.htaccess", "", sitefs.ErrSymlink}, // T1, T1b
	{"read/neo-symlink", "ReadFile", "e/public/.htaccess", "", sitefs.ErrSymlink},
	{"read/thu-muc", "ReadFile", "a/public/d", "", sitefs.ErrNotRegular},
	{"read/khong-co", "ReadFile", "a/public/nope", "", fs.ErrNotExist},
	{"read/cha-la-file", "ReadFile", "a/public/.htaccess/x", "", syscall.ENOTDIR},

	{"write/file", "WriteFileAtomic", "a/public/new", "", nil},
	{"write/de-symlink", "WriteFileAtomic", "a/public/l", "", nil},
	{"write/cha-symlink", "WriteFileAtomic", "b/public/.htaccess", "", sitefs.ErrSymlink}, // T1c
	{"write/thu-muc", "WriteFileAtomic", "a/public/d", "", syscall.EISDIR},
	{"write/khong-co-cha", "WriteFileAtomic", "a/public/nope/x", "", fs.ErrNotExist},

	{"mkdir/moi", "MkdirAll", "a/public/n/m", "", nil},
	{"mkdir/co-san", "MkdirAll", "a/public/d", "", nil},
	{"mkdir/neo-chua-co", "MkdirAll", "c/public", "", nil},
	{"mkdir/cha-symlink", "MkdirAll", "b/public/x", "", sitefs.ErrSymlink},
	{"mkdir/qua-symlink", "MkdirAll", "a/public/l/x", "", sitefs.ErrSymlink},
	{"mkdir/qua-file", "MkdirAll", "a/public/.htaccess/x", "", syscall.ENOTDIR},

	{"remove/file", "Remove", "a/public/.htaccess", "", nil},
	{"remove/symlink", "Remove", "a/public/l", "", nil},
	{"remove/cha-symlink", "Remove", "b/public/wp-config.php", "", sitefs.ErrSymlink}, // T2
	{"remove/khong-rong", "Remove", "a/public/d", "", syscall.ENOTEMPTY},
	{"remove/khong-co", "Remove", "a/public/nope", "", fs.ErrNotExist},

	{"rename/trong", "Rename", "a/public/.htaccess", "a/public/h2", nil},
	{"rename/public", "Rename", "a/public", "a/public2", nil}, // T11
	{"rename/ra-ngoai", "Rename", "a/public/.htaccess", "@out/h", nil},
	{"rename/tu-ngoai", "Rename", "@out", "a/public/out", nil},
	{"rename/cha-symlink-nguon", "Rename", "b/public/.htaccess", "a/public/x", sitefs.ErrSymlink},
	{"rename/cha-symlink-dich", "Rename", "a/public/.htaccess", "b/public/x", sitefs.ErrSymlink},
	{"rename/khong-co", "Rename", "a/public/nope", "a/public/x", fs.ErrNotExist},

	{"symlink/moi", "Symlink", "a/public/s", "/etc/passwd", nil},
	{"symlink/da-co", "Symlink", "a/public/l", "x", fs.ErrExist},
	{"symlink/cha-symlink", "Symlink", "b/public/s", "x", sitefs.ErrSymlink},

	{"readlink/link", "Readlink", "a/public/l", "", nil},
	{"readlink/file", "Readlink", "a/public/.htaccess", "", syscall.EINVAL},
	{"readlink/cha-symlink", "Readlink", "b/public/l", "", sitefs.ErrSymlink},

	{"stat/file", "Stat", "a/public/.htaccess", "", nil},
	{"stat/symlink-cuoi", "Stat", "a/public/l", "", nil},
	{"stat/public-symlink", "Stat", "b/public", "", nil},                                    // T21: lstat của tên cuối
	{"stat/cha-symlink", "Stat", "b/public/.htaccess", "", sitefs.ErrSymlink},               // T17, T20
	{"stat/wp-config-cha-symlink", "Stat", "b/public/wp-config.php", "", sitefs.ErrSymlink}, // T19
	{"stat/neo-symlink", "Stat", "e/public", "", sitefs.ErrSymlink},                         // T21, S11
	{"stat/khong-co", "Stat", "a/public/nope", "", fs.ErrNotExist},

	{"readdir/public", "ReadDir", "a/public", "", nil},
	{"readdir/public-symlink", "ReadDir", "b/public", "", sitefs.ErrSymlink}, // T18
	{"readdir/file", "ReadDir", "a/public/.htaccess", "", syscall.ENOTDIR},

	{"chown/file", "Chown", "a/public/.htaccess", "", nil}, // T3
	{"chown/thu-muc", "Chown", "a/public/d", "", nil},
	{"chown/symlink", "Chown", "a/public/l", "", sitefs.ErrSymlink},
	{"chown/cha-symlink", "Chown", "b/public/wp-config.php", "", sitefs.ErrSymlink},

	{"chmod/thu-muc-setgid", "Chmod", "a/public/d", "", nil}, // T8b, B16
	{"chmod/file", "Chmod", "a/public/.htaccess", "", nil},
	{"chmod/symlink", "Chmod", "a/public/l", "", sitefs.ErrSymlink},
	{"chmod/cha-symlink", "Chmod", "b/public/d", "", sitefs.ErrSymlink},

	{"statfs/public", "StatFS", "a/public", "", nil},
	{"statfs/symlink", "StatFS", "a/public/l", "", sitefs.ErrSymlink},
	{"statfs/cha-symlink", "StatFS", "b/public/d", "", sitefs.ErrSymlink},
}

// runZoneRow chạy một dòng và tóm tắt kết quả theo cách phải giống nhau
// trên cả hai system.
func runZoneRow(t *testing.T, z zoneSide, r zoneRow) (string, error) {
	t.Helper()
	me := currentOwner(t)
	p := z.path(r.p)
	s := z.sys
	switch r.op {
	case "ReadFile":
		b, err := s.ReadFile(p)
		return string(b), err
	case "WriteFileAtomic":
		if err := s.WriteFileAtomic(p, []byte("moi"), 0o640, me); err != nil {
			return "", err
		}
		b, err := s.ReadFile(p)
		return string(b), err
	case "MkdirAll":
		if err := s.MkdirAll(p, fs.ModeSetgid|0o750, me); err != nil {
			return "", err
		}
		fi, err := s.Stat(p)
		return fi.Mode.Type().String(), err
	case "Remove":
		if err := s.Remove(p); err != nil {
			return "", err
		}
		_, err := s.Stat(p)
		return fmt.Sprint(errors.Is(err, fs.ErrNotExist)), nil
	case "Rename":
		if err := s.Rename(p, z.path(r.arg)); err != nil {
			return "", err
		}
		fi, err := s.Stat(z.path(r.arg))
		return fi.Mode.Type().String(), err
	case "Symlink":
		if err := s.Symlink(r.arg, p); err != nil {
			return "", err
		}
		// B18: link thuộc chủ của neo.
		link, err := s.Stat(p)
		if err != nil {
			return "", err
		}
		anchor, err := s.Stat(z.path("a"))
		return fmt.Sprint(link.Owner.User == anchor.Owner.User), err
	case "Readlink":
		return s.Readlink(p)
	case "Stat":
		fi, err := s.Stat(p)
		if fi.Mode&fs.ModeSymlink != 0 {
			// Các bit của chính symlink là 0777 trên Linux, 0755 trên macOS.
			return fi.Mode.Type().String(), err
		}
		return fi.Mode.String(), err
	case "ReadDir":
		names, err := s.ReadDir(p)
		return strings.Join(names, ","), err
	case "Chown":
		if err := s.Chown(p, me); err != nil {
			return "", err
		}
		fi, err := s.Stat(p)
		return fmt.Sprint(fi.Owner == me), err
	case "Chmod":
		if err := s.Chmod(p, fs.ModeSetgid|0o750); err != nil {
			return "", err
		}
		fi, err := s.Stat(p)
		return fi.Mode.String(), err
	case "StatFS":
		du, err := s.StatFS(p)
		return fmt.Sprint(du.Total > 0), err
	}
	t.Fatalf("thao tác lạ %s", r.op)
	return "", nil
}

func TestVungSiteCaHaiBan(t *testing.T) {
	for _, r := range zoneRows {
		t.Run(r.name, func(t *testing.T) {
			var got [2]string
			for i, z := range []zoneSide{localSide(t), fakeSide(t)} {
				res, err := runZoneRow(t, z, r)
				got[i] = res
				switch {
				case r.want == nil && err != nil:
					t.Errorf("%s: lỗi %v", z.name, err)
				case r.want != nil && !errors.Is(err, r.want):
					t.Errorf("%s: lỗi %v, muốn %v", z.name, err, r.want)
				}
				var se *sitefs.Error
				if err != nil && !errors.As(err, &se) {
					t.Errorf("%s: lỗi %T không phải *sitefs.Error", z.name, err)
				}
			}
			if r.want == nil && got[0] != got[1] {
				t.Errorf("kết quả khác nhau: local %q, fake %q", got[0], got[1])
			}
		})
	}
}

// TestMoiHamCoLuatVungSite (B5): mọi hàm của System có tham số đường dẫn đều
// có dòng trong TestVungSiteCaHaiBan. Hàm mới mà không có dòng thì test đỏ.
func TestMoiHamCoLuatVungSite(t *testing.T) {
	covered := map[string]bool{}
	for _, r := range zoneRows {
		covered[r.op] = true
	}
	typ := reflect.TypeFor[System]()
	for i := range typ.NumMethod() {
		m := typ.Method(i)
		hasPath := false
		for j := range m.Type.NumIn() {
			hasPath = hasPath || m.Type.In(j).Kind() == reflect.String
		}
		if m.Name == "Lock" || !hasPath {
			continue
		}
		if !covered[m.Name] {
			t.Errorf("System.%s nhận đường dẫn mà bảng của TestVungSiteCaHaiBan không có dòng nào", m.Name)
		}
	}
	if len(covered) != 12 {
		t.Errorf("bảng phủ %d hàm, muốn 12", len(covered))
	}
}

// ---- 3.4: lệnh chạy bằng root ----

func TestLenhRootKhongNhanDuongDanSite(t *testing.T) {
	rows := []struct {
		name string
		cmd  Cmd
		ok   bool
	}{
		{"rm-root", Cmd{Name: "rm", Args: []string{"-r", "--", "/var/www/a/public"}}, false},
		{"tar-create", Cmd{Name: "tar", Args: []string{"--create", "--directory=/var/www/a", "public"}}, true},
		{"tar-backup-that", Cmd{Name: "tar", Args: []string{
			"--create", "--gzip", "--file=/var/lib/tpan/backups/a/x.tar.gz", "--one-file-system",
			"--directory=/var/lib/tpan/tmp", "manifest.json", "db.sql",
			"--directory=/var/www/a", "public",
		}}, true},
		{"tar-extract", Cmd{Name: "tar", Args: []string{"--extract", "-C", "/var/www/a/public"}}, false},
		{"tar-extract-viet-tat", Cmd{Name: "tar", Args: []string{"--create", "--ext", "--directory=/var/www/a", "public"}}, false},
		{"tar-x-gop", Cmd{Name: "tar", Args: []string{"--create", "-xzf", "a.tgz", "--directory=/var/www/a", "public"}}, false},
		{"tar-h", Cmd{Name: "tar", Args: []string{"--create", "-h", "--directory=/var/www/a", "public"}}, false},
		{"tar-dereference", Cmd{Name: "tar", Args: []string{"--create", "--dereference", "--directory=/var/www/a", "public"}}, false},
		{"tar-khong-create", Cmd{Name: "tar", Args: []string{"--list", "/var/www/a/public"}}, false},
		{"tar-sau-hon-mot", Cmd{Name: "tar", Args: []string{"--create", "--directory=/var/www/a/public", "wp-content"}}, false},
		{"tar-sau-tuong-doi", Cmd{Name: "tar", Args: []string{"--create", "--directory=/var/www/a", "public/wp-content"}}, false},
		{"tar-C-tach", Cmd{Name: "tar", Args: []string{"--create", "-C", "/var/www/a", "public"}}, true},
		{"tar-C-vao-public", Cmd{Name: "tar", Args: []string{"--create", "-C", "/var/www/a/public", "."}}, false},
		{"tar-directory-tach", Cmd{Name: "tar", Args: []string{"--create", "--directory", "/var/www/a/public", "wp-content"}}, false},
		// Dạng tách giá trị: tpan không dùng, danh sách trắng không nhận (C5).
		{"tar-directory-tach-neo", Cmd{Name: "tar", Args: []string{"--create", "--directory", "/var/www/a", "public"}}, false},
		// Soát cuối M28, C5: cờ nhận FILE (chương trình mở theo đường dẫn, đi
		// theo symlink ở tên cuối, ghi đè, hay chạy file đó) không bao giờ
		// được nhận trên đường dẫn site, kể cả <neo>/<một tên>.
		{"c5-tar-file", Cmd{Name: "tar", Args: []string{"--create", "--file=/var/www/a/x", "--directory=/var/www/a", "public"}}, false},
		{"c5-tar-f", Cmd{Name: "tar", Args: []string{"--create", "-f", "/var/www/a/x", "--directory=/var/www/a", "public"}}, false},
		{"c5-tar-T", Cmd{Name: "tar", Args: []string{"--create", "--file=/tmp/x.tar", "-T", "/var/www/a/x"}}, false},
		{"c5-tar-files-from", Cmd{Name: "tar", Args: []string{"--create", "--file=/tmp/x.tar", "--files-from=/var/www/a/x"}}, false},
		{"c5-tar-exclude-from", Cmd{Name: "tar", Args: []string{"--create", "--exclude-from=/var/www/a/x", "--directory=/var/www/a", "public"}}, false},
		{"c5-tar-index-file", Cmd{Name: "tar", Args: []string{"--create", "--index-file=/var/www/a/x", "--directory=/var/www/a", "public"}}, false},
		{"c5-tar-volno-file", Cmd{Name: "tar", Args: []string{"--create", "--volno-file=/var/www/a/x", "--directory=/var/www/a", "public"}}, false},
		{"c5-tar-listed-incremental", Cmd{Name: "tar", Args: []string{"--create", "--listed-incremental=/var/www/a/x", "--directory=/var/www/a", "public"}}, false},
		{"c5-tar-I", Cmd{Name: "tar", Args: []string{"--create", "-I", "/var/www/a/x", "--directory=/var/www/a", "public"}}, false},
		{"c5-tar-compress-program", Cmd{Name: "tar", Args: []string{"--create", "--use-compress-program=/var/www/a/x", "--directory=/var/www/a", "public"}}, false},
		{"c5-tar-F", Cmd{Name: "tar", Args: []string{"--create", "-F", "/var/www/a/x", "--directory=/var/www/a", "public"}}, false},
		{"c5-du-files0-from", Cmd{Name: "du", Args: []string{"-sb", "--files0-from=/var/www/a/x"}}, false},
		{"c5-du-X", Cmd{Name: "du", Args: []string{"-sb", "-X", "/var/www/a/x", "--", "/var/www/a"}}, false},
		{"c5-du-L", Cmd{Name: "du", Args: []string{"-L", "-sb", "--", "/var/www/a/public"}}, false},
		{"c5-useradd-P", Cmd{Name: "useradd", Args: []string{"-M", "-P", "/var/www/a/x", "tp_a_1"}}, false},
		{"c5-useradd-shell", Cmd{Name: "useradd", Args: []string{"--system", "--home-dir", "/var/www/a", "--no-create-home", "--shell", "/var/www/a/x", "tp_a_1"}}, false},
		{"useradd-that", Cmd{Name: "useradd", Args: []string{"--system", "--user-group", "--home-dir", "/var/www/a", "--no-create-home", "--shell", "/usr/sbin/nologin", "tp_a_1"}}, true},
		// Soát cuối M28, C12: --file= tương đối vẫn trỏ được vào vùng site
		// (tar ghép nó với thư mục làm việc) mà không chứa chuỗi "/var/www/".
		{"c12-tar-file-tuong-doi", Cmd{Name: "tar", Args: []string{"--create", "--gzip", "--file=var/www/a/out.tar", "--directory=/var/www/a", "public"}}, false},
		{"c12-tar-file-dir-neo", Cmd{Name: "tar", Args: []string{"--create", "--file=out.tar", "--directory=/var/www/a", "public"}, Dir: "/var/www/a"}, false},
		{"c12-tar-file-dir-var", Cmd{Name: "tar", Args: []string{"--create", "--file=www/a/out.tar", "--directory=/var/www/a", "public"}, Dir: "/var"}, false},
		{"c12-tar-file-cha", Cmd{Name: "tar", Args: []string{"--create", "--file=../a/out.tar", "--directory=/var/www/b", "public"}, Dir: "/var/www/b"}, false},
		{"c12-tar-file-chua-sach", Cmd{Name: "tar", Args: []string{"--create", "--file=/tmp/../var/www/a/out.tar", "--directory=/var/www/a", "public"}}, false},
		{"c12-tar-file-goc-vung", Cmd{Name: "tar", Args: []string{"--create", "--file=/var/www", "--directory=/var/www/a", "public"}}, false},
		{"c12-useradd-home-sau-neo", Cmd{Name: "useradd", Args: []string{"--system", "--user-group", "--home-dir", "/var/www/a/public", "--no-create-home", "--shell", "/usr/sbin/nologin", "tp_a_1"}}, false},
		{"c12-useradd-home-tuong-doi", Cmd{Name: "useradd", Args: []string{"--system", "--user-group", "--home-dir", "var/www/a", "--no-create-home", "--shell", "/usr/sbin/nologin", "tp_a_1"}}, false},
		{"tar-dir-viet-tat", Cmd{Name: "tar", Args: []string{"--create", "--dir=/var/www/a/public", "wp-content"}}, false},
		{"tar-zC-gop", Cmd{Name: "tar", Args: []string{"--create", "-zC", "/var/www/a/public", "wp-content"}}, false},
		{"tar-kieu-cu", Cmd{Name: "tar", Args: []string{"h", "--create", "--directory=/var/www/a", "public"}}, false},
		{"tar-C-dinh", Cmd{Name: "tar", Args: []string{"--create", "-C/var/www/a", "public"}}, false},
		{"tar-cham-cham", Cmd{Name: "tar", Args: []string{"--create", "/var/www/a/x/../public"}}, false},
		{"ten-chuong-trinh", Cmd{Name: "/var/www/a/public/x"}, false},
		{"tuong-doi", Cmd{Name: "cat", Args: []string{"var/www/a/public/x"}}, false},
		{"tuong-doi-dir", Cmd{Name: "cat", Args: []string{"a/public/x"}, Dir: "/var/www"}, false},
		{"dir-trong-vung", Cmd{Name: "ls", Dir: "/var/www/a/public"}, false},
		{"directory-tuong-doi", Cmd{Name: "tar", Args: []string{"--create", "--directory=var/www/a/public", "."}}, false},
		{"mariadb-source", Cmd{Name: "mariadb", Args: []string{"-e", "source /var/www/a/x.sql"}}, false},
		{"env", Cmd{Name: "wp", Env: []string{"WP_CLI_CONFIG_PATH=/var/www/a/public/wp-cli.yml"}}, false},
		{"du-neo", Cmd{Name: "du", Args: []string{"-sb", "--one-file-system", "--", "/var/www/a"}}, true},
		{"du-public", Cmd{Name: "du", Args: []string{"-sb", "--one-file-system", "--", "/var/www/a/public"}}, true},
		{"du-sau", Cmd{Name: "du", Args: []string{"-sb", "--", "/var/www/a/public/wp-content"}}, false},
		{"useradd", Cmd{Name: "useradd", Args: []string{"--system", "--user-group", "--home-dir", "/var/www/a", "--no-create-home", "tp_a_1"}}, true},
		{"useradd-tao-home", Cmd{Name: "useradd", Args: []string{"--home-dir", "/var/www/a", "tp_a_1"}}, false},
		{"find-chmod", Cmd{Name: "find", Args: []string{"/var/www/a/public", "-type", "d", "-exec", "chmod", "2750", "{}", "+"}}, false},
		{"as-site-user", Cmd{Name: "mariadb", Args: []string{"-e", "source /var/www/a/x.sql"}, AsUser: "tp_a_1"}, true},
		{"as-site-user-rm", Cmd{Name: "rm", Args: []string{"-r", "--", "/var/www/a/public/x"}, AsUser: "tp_a_1"}, true},
		{"as-www-data", Cmd{Name: "mariadb", Args: []string{"-e", "source /var/www/a/x.sql"}, AsUser: "www-data"}, false},
		{"as-root", Cmd{Name: "rm", Args: []string{"/var/www/a/public/x"}, AsUser: "root"}, false},
		{"ngoai-vung", Cmd{Name: "rm", Args: []string{"-r", "--", "/var/lib/tpan/trash/a_1"}}, true},
		{"chinh-goc", Cmd{Name: "ls", Args: []string{"/var/www"}}, true},
		{"goc-giong-ten", Cmd{Name: "ls", Args: []string{"/var/wwwx/a/b"}}, true},
		{"lenh-thuong", Cmd{Name: "systemctl", Args: []string{"reload", "nginx"}}, true},
	}
	// Phép kiểm thuần với thư mục gốc vùng thật, và FakeSystem.Run từ đầu đến cuối.
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			err := checkRootZone(sitefs.Root, r.cmd)
			if r.ok != (err == nil) {
				t.Errorf("checkRootZone = %v, muốn được chạy: %v", err, r.ok)
			}
			if err != nil && !errors.Is(err, ErrRootZoneCommand) {
				t.Errorf("lỗi %v không phải ErrRootZoneCommand", err)
			}
			f := NewFake()
			_, ferr := f.Run(context.Background(), r.cmd)
			if errors.Is(ferr, ErrRootZoneCommand) == r.ok {
				t.Errorf("FakeSystem.Run = %v, muốn được chạy: %v", ferr, r.ok)
			}
			if !r.ok && len(f.Ran()) != 0 {
				t.Errorf("lệnh bị từ chối vẫn được ghi là đã chạy")
			}
		})
	}
	// LocalSystem.Run từ chối trước khi khởi chạy bất cứ thứ gì. Ở đây chỉ chạy
	// các dòng bị từ chối: một lệnh tar được phép trên máy Mac sẽ chạy thật.
	s := &LocalSystem{LockPath: filepath.Join(t.TempDir(), "lock"), SiteRoot: sitefs.Root}
	for _, r := range rows {
		if r.ok {
			continue
		}
		if _, err := s.Run(context.Background(), r.cmd); !errors.Is(err, ErrRootZoneCommand) {
			t.Errorf("LocalSystem.Run %s = %v, muốn ErrRootZoneCommand", r.name, err)
		}
	}
	// SiteRoot rỗng: không có vùng, không kiểm (các test tự dựng LocalSystem bằng tay).
	if err := checkRootZone("", Cmd{Name: "rm", Args: []string{"/var/www/a/public"}}); err != nil {
		t.Errorf("SiteRoot rỗng vẫn xét: %v", err)
	}
}

// TestLenhRootHuyKhongDeLaiTienTrinh (B15): runuser fork lệnh thật rồi chờ;
// huỷ phải giết cả process group, không chỉ runuser.
func TestLenhRootHuyKhongDeLaiTienTrinh(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("cần root: chạy trong máy ảo bằng make m28-test-vm")
	}
	if _, err := exec.LookPath("runuser"); err != nil {
		t.Skip("không có runuser")
	}
	s := NewLocal()
	s.SiteRoot = t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	start := time.Now()
	_, err := s.Run(ctx, Cmd{Name: "sleep", Args: []string{"30"}, AsUser: "nobody"})
	if err == nil {
		t.Fatal("lệnh bị huỷ mà không lỗi")
	}
	if d := time.Since(start); d > 7*time.Second {
		t.Errorf("Run trả về sau %v", d)
	}
	time.Sleep(200 * time.Millisecond)
	out, _ := exec.Command("pgrep", "-u", "nobody", "sleep").Output()
	if len(strings.TrimSpace(string(out))) != 0 {
		t.Errorf("còn tiến trình sleep của nobody: %s", out)
	}
}

// TestVungSiteDirSetgid (B16): trên kernel thật, một thư mục site đặt thành
// fs.ModeSetgid|0o750 qua LocalSystem ra 2750, và Stat đọc lại đúng Mode đó
// (nên steps.Dir.Check qua được ở lần thứ hai).
func TestVungSiteDirSetgid(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("cần root: chạy trong máy ảo bằng make m28-test-vm")
	}
	z := localSide(t)
	owner := Owner{User: "nobody", Group: "nogroup"}
	if _, _, err := lookupOwner(owner); err != nil {
		owner = Owner{User: "nobody", Group: "nobody"}
	}
	p := z.path("a/public/site")
	want := fs.ModeDir | fs.ModeSetgid | 0o750
	if err := z.sys.MkdirAll(p, want.Perm()|fs.ModeSetgid, owner); err != nil {
		t.Fatal(err)
	}
	if err := z.sys.Chown(p, owner); err != nil {
		t.Fatal(err)
	}
	if err := z.sys.Chmod(p, fs.ModeSetgid|0o750); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("stat", "-c", "%a", p).Output()
	if err != nil || strings.TrimSpace(string(out)) != "2750" {
		t.Errorf("stat -c %%a = %q, %v; muốn 2750", out, err)
	}
	fi, err := z.sys.Stat(p)
	if err != nil || fi.Mode != want {
		t.Errorf("Stat = %v, %v; muốn %v", fi.Mode, err, want)
	}
}

// TestVungSiteFakeLuatChu: các luật bản giả áp dụng mà trên máy thật cần
// root (chủ, giới hạn, file đặc biệt, thư mục gốc không an toàn), với SetSite.
func TestVungSiteFakeLuatChu(t *testing.T) {
	f := NewFake()
	f.SetDir("/var/www", 0o755, Root)
	f.SetSite("/var/www/a", "tp_a")
	site := Owner{User: "tp_a", Group: "www-data"}
	if fi, err := f.Stat("/var/www/a/public"); err != nil || fi.Mode != fs.ModeDir|fs.ModeSetgid|0o750 || fi.Owner != site {
		t.Fatalf("SetSite: %+v, %v", fi, err)
	}
	f.SetFile("/var/www/a/public/mine", []byte("ok"), 0o640, site)
	f.SetFile("/var/www/a/public/root", []byte("x"), 0o640, Root)
	f.SetFile("/var/www/a/public/big", make([]byte, sitefs.MaxFile+1), 0o640, site)
	f.SetSocket("/var/www/a/public/sock", 0o660, site)
	if b, err := f.ReadFile("/var/www/a/public/mine"); err != nil || string(b) != "ok" {
		t.Errorf("file của site: %q, %v", b, err)
	}
	checks := []struct {
		name string
		err  error
		want error
	}{
		{"file của root", second(f.ReadFile("/var/www/a/public/root")), sitefs.ErrOwner},
		{"quá trần", second(f.ReadFile("/var/www/a/public/big")), sitefs.ErrTooLarge},
		{"socket", second(f.ReadFile("/var/www/a/public/sock")), sitefs.ErrNotRegular},
		{"chmod socket", f.Chmod("/var/www/a/public/sock", 0o600), sitefs.ErrNotRegular},
		{"chown socket", f.Chown("/var/www/a/public/sock", site), sitefs.ErrNotRegular},
	}
	for _, c := range checks {
		if !errors.Is(c.err, c.want) {
			t.Errorf("%s: %v, muốn %v", c.name, c.err, c.want)
		}
	}
	// Neo do SetFile tạo ngầm thì thuộc root: luật chủ tắt.
	f.SetFile("/var/www/b/public/x", []byte("x"), 0o640, Owner{User: "khac"})
	if _, err := f.ReadFile("/var/www/b/public/x"); err != nil {
		t.Errorf("neo của root bỏ luật chủ: %v", err)
	}
	// Quá nhiều mục.
	for i := range sitefs.MaxDirEntries + 1 {
		f.SetFile("/var/www/a/public/many/"+strconv.Itoa(i), nil, 0o640, site)
	}
	if _, err := f.ReadDir("/var/www/a/public/many"); !errors.Is(err, sitefs.ErrTooMany) {
		t.Errorf("ReadDir quá trần: %v", err)
	}
	// Thư mục gốc vùng không an toàn.
	f.SetDir("/var/www", 0o777, Root)
	if _, err := f.Stat("/var/www/a/public"); !errors.Is(err, sitefs.ErrRootUnsafe) {
		t.Errorf("gốc 0777: %v", err)
	}
}

func second[T any](_ T, err error) error { return err }
