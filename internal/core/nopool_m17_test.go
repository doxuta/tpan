package core

// Test tĩnh của M17 (mục 3.6 của docs/thay-doi/M17-thiet-ke.md): chỉ vài hàm
// được nhắc tới pool chung của PHP-FPM. Mọi Plan dựng nơi phục vụ PHP hay chọn
// phép kiểm của một site đi qua chúng, nên không đường nào lén đưa một site
// trong lồng về pool chung, hay kiểm site trong lồng bằng phép kiểm của pool
// chung (bất biến 3 và 7). Hàm mới cần một trong các tên dưới thì gọi qua hàm
// chọn; nếu thật sự phải gọi thẳng, thêm hàm đó vào danh sách kèm lý do.
//
// Đọc cây cú pháp (go/parser), không quét chữ: tên trong chú thích không tính.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestKhongGoiPoolChungNgoaiHamChon(t *testing.T) {
	// Tên được canh → các hàm được gọi nó (method ghi kèm kiểu nhận).
	allow := map[string][]string{
		// Chỗ chọn duy nhất giữa pool chung và lồng.
		"steps.SitePool": {"sitePHPSteps"},
		// Tạm khoá và mở khoá site ngoài lồng (đổi tên file pool); site trong
		// lồng đi nhánh CageSocket và sitePHPSteps trong cùng hai hàm này.
		"steps.PoolSuspend":   {"Sites.suspendSteps"},
		"steps.PoolUnsuspend": {"Sites.unsuspendSteps"},
		// Chỗ chọn bộ phép kiểm theo đường request (siteChecks gọi nó).
		"checks.SiteChecks":    {"siteRequestChecks"},
		"checks.SiteChecksSSL": {"siteRequestChecks"},
		// Ba nhánh chỉ dành cho site ngoài lồng: nhánh DB đang khoá của
		// siteStateChecks, sửa tự động php.restart (site trong lồng bị từ chối
		// no_auto_fix), phép kiểm giữa chừng của php.use (site trong lồng có
		// Plan riêng).
		"checks.PoolSocket": {"siteStateChecks", "Fixes.plan", "PHP.PrepareUse"},
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	files := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("đọc %s: %v", name, err)
		}
		files++
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			fn := funcName(fd)
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkg, ok := sel.X.(*ast.Ident)
				if !ok {
					return true
				}
				key := pkg.Name + "." + sel.Sel.Name
				if allowed, watched := allow[key]; watched && !slices.Contains(allowed, fn) {
					t.Errorf("%s: %s gọi %s; chỉ được gọi trong %v (đi qua hàm chọn để site trong lồng không bị đưa về pool chung)",
						fset.Position(sel.Pos()), fn, key, allowed)
				}
				return true
			})
		}
	}
	if files < 20 {
		t.Fatalf("chỉ đọc được %d file của gói core: test đang chạy sai thư mục?", files)
	}
}

// funcName là tên hàm, kèm kiểu nhận với method ("Fixes.plan").
func funcName(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return fd.Name.Name
	}
	typ := fd.Recv.List[0].Type
	if star, ok := typ.(*ast.StarExpr); ok {
		typ = star.X
	}
	if id, ok := typ.(*ast.Ident); ok {
		return id.Name + "." + fd.Name.Name
	}
	return fd.Name.Name
}
