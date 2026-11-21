package system

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// B7 (M28-thiet-ke.md, mục 4): ngoài package system và sitefs, không mã nào
// đụng file trực tiếp qua os, io/ioutil, filepath.Walk*, syscall hay
// x/sys/unix. Mọi thao tác file đi qua system.System, nơi đưa đường dẫn site
// qua cổng. Gọi trực tiếp chính là cách T1 đã xảy ra
// (engine.Preview đọc .htaccess bằng os.ReadFile).

// osForbidden liệt kê, theo từng import path, các hàm không được dùng.
// nil nghĩa là mọi hàm của package đó.
var osForbidden = map[string]map[string]bool{
	"os": set("ReadFile", "Open", "OpenFile", "Create", "CreateTemp", "MkdirTemp", "WriteFile",
		"Stat", "Lstat", "ReadDir", "Chown", "Lchown", "Chmod", "Chtimes", "Remove", "RemoveAll",
		"Rename", "Symlink", "Readlink", "Link", "Mkdir", "MkdirAll", "Truncate", "OpenRoot", "DirFS"),
	"syscall": set("Open", "Openat", "Stat", "Lstat", "Chown", "Lchown", "Chmod", "Fchmodat", "Fchownat",
		"Unlink", "Unlinkat", "Rename", "Renameat", "Mkdir", "Mkdirat", "Symlink", "Readlink", "Link",
		"Creat", "Truncate"),
	"golang.org/x/sys/unix": nil,
	"path/filepath":         set("Walk", "WalkDir", "Glob", "EvalSymlinks"),
}

// osAllowed: các file vẫn được đụng file system trực tiếp, mỗi file một lý
// do. Không file nào nhận đường dẫn nằm trong vùng site.
var osAllowed = map[string]string{
	"internal/store/store.go":   "mở tpan.db (SQLite cần đường dẫn thật, thư mục /var/lib/tpan của root)",
	"internal/store/backup.go":  "sao lưu tpan.db cạnh chính nó",
	"internal/ai/bridge/hub.go": "socket nội bộ của cầu AI dưới /run/tpan",
	"internal/demo/demo.go":     "thư mục tạm của chế độ --demo",
	"cmd/tpan/main.go":          "kiểm tpan.db đã có chưa trước khi mở store",
	"cmd/tpan/apply.go":         "đọc sites.yaml người gõ lệnh chỉ ra",
}

func set(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// osSkipDirs không thuộc binary tpan, hoặc là hai package duy nhất được
// phép đụng file. tmp-phien là thư mục tạm của phiên làm việc (nằm trong
// .git/info/exclude, không vào git): chương trình thử để ở đó không phải mã
// của tpan.
var osSkipDirs = []string{"internal/system", "internal/sitefs", "scripts", "docs", "agent-dev", "tmp-phien", "web/node_modules"}

// osHits phân tích một file Go và trả về "line: pkg.Func" cho mỗi chỗ dùng bị cấm.
func osHits(src []byte) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, 0)
	if err != nil {
		return nil, err
	}
	var hits []string
	alias := map[string]string{} // tên cục bộ -> import path
	for _, imp := range f.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		if p == "io/ioutil" {
			hits = append(hits, strconv.Itoa(fset.Position(imp.Pos()).Line)+": import io/ioutil")
			continue
		}
		if _, ok := osForbidden[p]; !ok {
			continue
		}
		name := path.Base(p)
		if imp.Name != nil {
			name = imp.Name.Name
		}
		alias[name] = p
	}
	ast.Inspect(f, func(n ast.Node) bool {
		var sel *ast.SelectorExpr
		call := false
		switch x := n.(type) {
		case *ast.CallExpr:
			sel, _ = x.Fun.(*ast.SelectorExpr)
			call = true
		case *ast.SelectorExpr:
			sel = x
		}
		if sel == nil {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		p, ok := alias[id.Name]
		if !ok {
			return true
		}
		names := osForbidden[p]
		// unix: mọi lời gọi; các package khác: những tên trong danh sách, được gọi hoặc truyền như một giá trị.
		if (names == nil && call) || names[sel.Sel.Name] {
			hits = append(hits, strconv.Itoa(fset.Position(sel.Pos()).Line)+": "+id.Name+"."+sel.Sel.Name)
		}
		return true
	})
	sort.Strings(hits)
	return dedup(hits), nil
}

func dedup(s []string) []string {
	var out []string
	for i, v := range s {
		if i == 0 || v != s[i-1] {
			out = append(out, v)
		}
	}
	return out
}

func TestKhongGoiOSTrucTiep(t *testing.T) {
	root := repoRoot(t)
	seen := map[string]bool{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(strings.TrimPrefix(p, root+string(filepath.Separator)))
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", ".claude":
				return filepath.SkipDir
			}
			for _, s := range osSkipDirs {
				if rel == s {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		hits, err := osHits(src)
		if err != nil {
			return nil // go vet đã báo các file không phân tích được
		}
		if len(hits) == 0 {
			return nil
		}
		if _, ok := osAllowed[rel]; ok {
			seen[rel] = true
			return nil
		}
		for _, h := range hits {
			t.Errorf("%s:%s: chạm file trực tiếp; đi qua system.System (B7, mục 4 của M28-thiet-ke.md)", rel, h)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Ngoại lệ nào không còn cần thì bị gỡ, để danh sách luôn trung thực.
	for f := range osAllowed {
		if !seen[f] {
			t.Errorf("%s có trong osAllowed nhưng không còn gọi hàm file nào: bỏ khỏi danh sách", f)
		}
	}
}

// TestMauQuetOS kiểm chính bộ quét, để test ở trên không xanh chỉ vì bộ quét
// bị mù.
func TestMauQuetOS(t *testing.T) {
	bad := `package x
import (
	"os"
	sys "syscall"
	"path/filepath"
	"golang.org/x/sys/unix"
	"io/ioutil"
)
func f() {
	os.ReadFile("/var/www/a/public/.htaccess")
	_ = os.Chmod
	sys.Unlinkat(0, "x", 0)
	filepath.WalkDir(".", nil)
	unix.Fchmodat(0, "x", 0, 0)
	ioutil.ReadAll(nil)
}`
	hits, err := osHits([]byte(bad))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"10: os.ReadFile", "11: os.Chmod", "12: sys.Unlinkat", "13: filepath.WalkDir", "14: unix.Fchmodat", "7: import io/ioutil"}
	sort.Strings(want)
	if strings.Join(hits, "|") != strings.Join(want, "|") {
		t.Errorf("bắt được %q, muốn %q", hits, want)
	}
	good := `package x
import (
	"os"
	"path/filepath"
	"golang.org/x/sys/unix"
)
func f() {
	_, _ = os.Stdin.Stat()
	_ = os.Getenv("HOME")
	_ = os.Geteuid()
	_ = filepath.Join("a", "b")
	_ = unix.O_RDONLY
}`
	hits, err = osHits([]byte(good))
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Errorf("bắt nhầm: %q", hits)
	}
}
