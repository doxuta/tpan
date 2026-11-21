package core

import (
	"errors"
	"io/fs"
	"testing"

	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/sitefs"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// Soát cuối M28, C7: khôi phục từ thùng rác trên Apache hỏi public/.htaccess
// của cây trong thùng rác (ngoài vùng site, không qua cửa sitefs). public bị
// một tiến trình còn sống của site đổi thành symlink thì phải từ chối, không
// stat theo symlink.
func TestHtaccessIfMissingKhongDiTheoSymlink(t *testing.T) {
	d, err := validate.ParseDomain("a.example.com")
	if err != nil {
		t.Fatal(err)
	}
	l := site.LayoutOf(d)
	trash := site.TrashDir(l)

	f := system.NewFake()
	f.SetDir("/srv/bi-mat", 0o700, system.Root)
	f.SetFile("/srv/bi-mat/.htaccess", []byte("x"), 0o600, system.Root)
	f.SetDir(trash, 0o2750, system.Owner{User: l.User, Group: site.WebGroup})
	f.SetSymlink("/srv/bi-mat", trash+"/public")
	if _, err := htaccessIfMissing(f, l, trash+"/public"); !errors.Is(err, sitefs.ErrSymlink) {
		t.Fatalf("public là symlink: err = %v, muốn ErrSymlink", err)
	}

	// public là thư mục thật: chưa có .htaccess thì có bước ghi, có rồi thì không.
	f2 := system.NewFake()
	f2.SetDir(trash+"/public", fs.ModeSetgid|0o750, system.Owner{User: l.User, Group: site.WebGroup})
	st, err := htaccessIfMissing(f2, l, trash+"/public")
	if err != nil || st == nil {
		t.Fatalf("chưa có .htaccess: bước = %v, err = %v", st, err)
	}
	f2.SetFile(trash+"/public/.htaccess", []byte("# BEGIN WordPress\n"), 0o640, system.Owner{User: l.User, Group: site.WebGroup})
	if st, err := htaccessIfMissing(f2, l, trash+"/public"); err != nil || st != nil {
		t.Fatalf("đã có .htaccess: bước = %v, err = %v", st, err)
	}
}
