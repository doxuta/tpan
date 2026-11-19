package system

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Mục 11 của docs/KE-HOACH.md: mọi lệnh chạy bằng argv qua system.System,
// không bao giờ qua shell. Test này đọc mọi file .go của repo để giữ luật đó.
//
// Các mẫu dưới đây viết bằng lớp ký tự ([h], [c]...) để chính file này không
// tự khớp với mẫu của nó.
var shellPatterns = []*regexp.Regexp{
	// Tên shell trong nháy kép (sh, bash, dash, zsh, có thể kèm /bin/) rồi tới
	// cờ -c trong nháy kép, giữa hai chuỗi không có chuỗi nào khác (được xuống
	// dòng). Bắt cả argv viết liền lẫn Cmd{Name: <shell>, Args: []string{<-c>, ...}}
	// mà gofmt tách ra nhiều dòng.
	regexp.MustCompile(`"(?:/usr)?(?:/bin/)?(?:s[h]|bas[h]|das[h]|zs[h])"[^"]{0,80}"-[c]"`),
	// exec.Command hay exec.CommandContext có tên shell trong tham số.
	regexp.MustCompile(`exec\.Comman[d](?:Context)?\([^)]*"(?:/usr)?(?:/bin/)?(?:s[h]|bas[h]|das[h]|zs[h])"`),
}

func TestNoShell(t *testing.T) {
	root := repoRoot(t)
	allowedExec := filepath.Join(root, "internal", "system") + string(filepath.Separator)

	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, re := range shellPatterns {
			if loc := re.FindIndex(src); loc != nil {
				line := 1 + strings.Count(string(src[:loc[0]]), "\n")
				t.Errorf("%s:%d: gọi shell %q; chạy lệnh bằng argv qua system.System (mục 11)", rel, line, src[loc[0]:loc[1]])
			}
		}
		// scripts/ là đồ nghề test chạy trên máy dev (ví dụ client MCP thử của
		// e2e M7 mở `ssh` làm server), không nằm trong file chạy tpan; vẫn phải
		// qua mẫu cấm shell ở trên, chỉ được import os/exec.
		if strings.HasPrefix(p, allowedExec) || strings.HasPrefix(rel, "scripts"+string(filepath.Separator)) {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), p, src, parser.ImportsOnly)
		if err != nil {
			// File không parse được thì go vet sẽ báo; ở đây chỉ lo luật import.
			return nil
		}
		for _, imp := range f.Imports {
			if path, _ := strconv.Unquote(imp.Path.Value); path == "os/exec" {
				t.Errorf("%s: import os/exec ngoài internal/system; dùng system.System.Run", rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestShellPatterns kiểm chính các mẫu, để test trên không "xanh" chỉ vì mẫu sai.
func TestShellPatterns(t *testing.T) {
	// SHELL và BASH được thay lúc chạy, để file này không tự khớp khi TestNoShell quét.
	r := strings.NewReplacer("SHELL", "sh", "BASH", "bash")
	bad := []string{
		r.Replace(`Cmd{Name: "SHELL", Args: []string{"-c", x}}`),
		r.Replace(`[]string{"BASH", "-c", "ls"}`),
		r.Replace(`exec.Command("/bin/SHELL", "-c", s)`),
		r.Replace(`exec.CommandContext(ctx, "BASH", script)`),
		r.Replace("system.Cmd{\n\tName: \"SHELL\",\n\tArgs: []string{\"-c\", s},\n}"),
		r.Replace("[]string{\n\t\"BASH\",\n\t\"-c\",\n\ts,\n}"),
	}
	good := []string{
		`Cmd{Name: "nginx", Args: []string{"-t"}}`,
		`// cấm sh -c trong mã`,
		`Cmd{Name: "ssh", Args: []string{"-c", "aes"}}`,
	}
	match := func(s string) bool {
		for _, re := range shellPatterns {
			if re.MatchString(s) {
				return true
			}
		}
		return false
	}
	for _, s := range bad {
		if !match(s) {
			t.Errorf("mẫu bỏ sót: %s", s)
		}
	}
	for _, s := range good {
		if match(s) {
			t.Errorf("mẫu bắt nhầm: %s", s)
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("không tìm thấy go.mod")
		}
		dir = parent
	}
}
