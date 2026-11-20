package checks

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// TestI18nEnglishNotVietnamese scans every non-test Go file under internal/ for
// i18n.T(vi, en) calls and i18n.Text{...} literals. It fails when the English
// side is an empty literal, or when any string literal inside the English
// expression holds a Latin letter outside ASCII (Vietnamese diacritics leaking
// into the English UI). The same letter check runs on literals assigned to an
// Output field (Evidence.Output, cron run output), which is a plain string
// shown in both languages. It also fails when a NoUndo reason starts with
// "Không hoàn tác"/"Not undone": the web label already says it.
func TestI18nEnglishNotVietnamese(t *testing.T) {
	root := filepath.Join("..")
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CallExpr:
				// i18n.T("", "") is a deliberate zero value, filled in later.
				if isSel(x.Fun, "i18n", "T") && len(x.Args) == 2 && !isEmptyLit(x.Args[0]) {
					checkEn(t, fset, x.Args[1])
				}
			case *ast.AssignStmt:
				// Evidence.Output (and similar run output) is a plain string shown
				// as-is in both languages, so it must not hold Vietnamese.
				if len(x.Lhs) == len(x.Rhs) {
					for i, l := range x.Lhs {
						if s, ok := l.(*ast.SelectorExpr); ok && s.Sel.Name == "Output" {
							checkLatin(t, fset, x.Rhs[i], "Output")
						}
					}
				}
			case *ast.CompositeLit:
				if isSel(x.Type, "i18n", "Text") {
					for _, e := range x.Elts {
						if kv, ok := e.(*ast.KeyValueExpr); ok && isIdent(kv.Key, "En") {
							checkEn(t, fset, kv.Value)
						}
					}
				}
			case *ast.KeyValueExpr:
				switch {
				case isIdent(x.Key, "NoUndo"):
					checkNoUndo(t, fset, x.Value)
				case isIdent(x.Key, "Output"):
					checkLatin(t, fset, x.Value, "Output")
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func checkEn(t *testing.T, fset *token.FileSet, e ast.Expr) {
	t.Helper()
	if isEmptyLit(e) {
		t.Errorf("%s: English text is empty", fset.Position(e.Pos()))
	}
	checkLatin(t, fset, e, "English text")
}

// properNames may keep their own letters in English text.
var properNames = strings.NewReplacer("Ondřej Surý", "")

// checkLatin reports string literals inside e that hold a Latin letter outside
// ASCII, which in this code base means Vietnamese.
func checkLatin(t *testing.T, fset *token.FileSet, e ast.Expr, what string) {
	t.Helper()
	ast.Inspect(e, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		s, _ := strconv.Unquote(lit.Value)
		for _, r := range properNames.Replace(s) {
			if r > unicode.MaxASCII && unicode.Is(unicode.Latin, r) {
				t.Errorf("%s: %s has non-English letter %q: %s", fset.Position(lit.Pos()), what, r, s)
				break
			}
		}
		return true
	})
}

func checkNoUndo(t *testing.T, fset *token.FileSet, e ast.Expr) {
	t.Helper()
	call, ok := e.(*ast.CallExpr)
	if !ok || !isSel(call.Fun, "i18n", "T") {
		return
	}
	for _, a := range call.Args {
		lit, ok := a.(*ast.BasicLit)
		if !ok {
			continue
		}
		s, _ := strconv.Unquote(lit.Value)
		if strings.HasPrefix(s, "Không hoàn tác") || strings.HasPrefix(s, "Not undone") {
			t.Errorf("%s: NoUndo repeats the UI label: %s", fset.Position(lit.Pos()), s)
		}
	}
}

func isEmptyLit(e ast.Expr) bool {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	s, _ := strconv.Unquote(lit.Value)
	return s == ""
}

func isSel(e ast.Expr, pkg, name string) bool {
	s, ok := e.(*ast.SelectorExpr)
	return ok && isIdent(s.X, pkg) && s.Sel.Name == name
}

func isIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}
