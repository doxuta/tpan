package cloudinit

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
	"golang.org/x/crypto/argon2"

	"github.com/doxuta/tpan/internal/system"
)

var (
	sumA = strings.Repeat("a", 64)
	sumB = strings.Repeat("b", 64)
)

func params() Params {
	return Params{
		Version:  "v1.18.0",
		Base:     "https://example.com/tpan/releases/download/v1.18.0",
		SumAMD64: sumA,
		SumARM64: sumB,
		Config:   Config{Version: 1, Web: "nginx", AdminHash: "$argon2id$v=19$m=65536,t=1,p=4$c2FsdA$aGFzaA"},
	}
}

// argonHash dựng bản băm PHC argon2id như store.HashPassword, để test không
// kéo cả gói store (sqlite) vào.
func argonHash(t *testing.T, pw string) string {
	t.Helper()
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		t.Fatal(err)
	}
	h := argon2.IDKey([]byte(pw), salt, 1, 64*1024, 4, 32)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=65536,t=1,p=4$%s$%s", argon2.Version, enc.EncodeToString(salt), enc.EncodeToString(h))
}

type parsedFile struct {
	Path, Permissions, Encoding string
	Raw                         string // content như trong YAML
	Data                        []byte // content đã giải base64/gzip
}

// parse đọc lại user-data như cloud-init: đúng một document, chỉ hai khoá
// write_files và runcmd, rồi giải nội dung từng file.
func parse(t *testing.T, ud []byte) (files []parsedFile, runcmd any) {
	t.Helper()
	if !bytes.HasPrefix(ud, []byte("#cloud-config\n# ")) {
		t.Fatalf("dòng đầu không phải #cloud-config rồi chú thích:\n%s", ud)
	}
	dec := yaml.NewDecoder(bytes.NewReader(ud))
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		t.Fatal(err)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatalf("user-data có hơn một document: %v", err)
	}
	keys := []string{}
	for k := range doc {
		keys = append(keys, k)
	}
	if len(doc) != 2 || doc["write_files"] == nil || doc["runcmd"] == nil {
		t.Fatalf("khoá cấp trên lạ: %v", keys)
	}
	list, ok := doc["write_files"].([]any)
	if !ok {
		t.Fatalf("write_files không phải danh sách: %T", doc["write_files"])
	}
	for _, it := range list {
		m := it.(map[string]any)
		f := parsedFile{}
		for k, v := range m {
			s, ok := v.(string)
			if !ok {
				t.Fatalf("write_files.%s không phải chuỗi: %#v", k, v)
			}
			switch k {
			case "path":
				f.Path = s
			case "permissions":
				f.Permissions = s
			case "encoding":
				f.Encoding = s
			case "content":
				f.Raw = s
			default:
				t.Fatalf("write_files có khoá lạ %q", k)
			}
		}
		switch f.Encoding {
		case "":
			f.Data = []byte(f.Raw)
		case "b64":
			b, err := base64.StdEncoding.DecodeString(f.Raw)
			if err != nil {
				t.Fatal(err)
			}
			f.Data = b
		case "gz+b64":
			b, err := base64.StdEncoding.DecodeString(f.Raw)
			if err != nil {
				t.Fatal(err)
			}
			zr, err := gzip.NewReader(bytes.NewReader(b))
			if err != nil {
				t.Fatal(err)
			}
			if f.Data, err = io.ReadAll(zr); err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatalf("encoding lạ %q", f.Encoding)
		}
		files = append(files, f)
	}
	return files, doc["runcmd"]
}

func paths(files []parsedFile) []string {
	var out []string
	for _, f := range files {
		out = append(out, f.Path)
	}
	return out
}

func checkShape(t *testing.T, files []parsedFile, runcmd any, withSites bool) {
	t.Helper()
	if want := []any{[]any{ScriptPath}}; !reflect.DeepEqual(runcmd, want) {
		t.Fatalf("runcmd = %#v, muốn %#v", runcmd, want)
	}
	want := []string{ConfigPath, ScriptPath}
	perms := []string{"0600", "0700"}
	if withSites {
		want = []string{ConfigPath, SitesPath, ScriptPath}
		perms = []string{"0600", "0600", "0700"}
	}
	if got := paths(files); !reflect.DeepEqual(got, want) {
		t.Fatalf("đường dẫn write_files = %v, muốn %v", got, want)
	}
	for i, f := range files {
		if f.Permissions != perms[i] {
			t.Fatalf("%s: permissions %q, muốn %q", f.Path, f.Permissions, perms[i])
		}
	}
}

func TestRenderCaThuong(t *testing.T) {
	p := params()
	ud, err := Render(p)
	if err != nil {
		t.Fatal(err)
	}
	files, runcmd := parse(t, ud)
	checkShape(t, files, runcmd, false)
	if files[0].Encoding != "b64" || files[1].Encoding != "" {
		t.Fatalf("encoding sai: %q %q", files[0].Encoding, files[1].Encoding)
	}
	if string(files[1].Data) != Script(p) {
		t.Fatal("nội dung script trong user-data khác Script(p)")
	}
	if !bytes.Contains(ud, []byte("content: |")) {
		t.Fatalf("script không ở dạng khối literal:\n%s", ud)
	}
	got, err := ParseConfig(files[0].Data)
	if err != nil || got != p.Config {
		t.Fatalf("config đọc lại = %+v, %v", got, err)
	}
	// Tất định: cùng đầu vào cùng output (dễ so, dễ kiểm lại).
	ud2, _ := Render(p)
	if !bytes.Equal(ud, ud2) {
		t.Fatal("hai lần Render cùng đầu vào cho hai output khác nhau")
	}
}

// B6: user-data không chứa bí mật dạng rõ, kể cả sau khi giải base64 và gzip.
func TestKhongCoBiMatDangRo(t *testing.T) {
	const marker = "MatKhauDanhDau-7f3a9c"
	p := params()
	p.Config.AdminHash = argonHash(t, marker)
	p.Config.Private = true
	p.Config.SitesFile = SitesPath
	p.SitesYAML = []byte("sites:\n  - domain: example.com\n    title: Trang thử\n")
	ud, err := Render(p)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ud, []byte(marker)) {
		t.Fatal("user-data thô chứa mật khẩu đánh dấu")
	}
	files, runcmd := parse(t, ud)
	checkShape(t, files, runcmd, true)
	for _, f := range files {
		if bytes.Contains(f.Data, []byte(marker)) || strings.Contains(f.Raw, marker) {
			t.Fatalf("%s chứa mật khẩu đánh dấu", f.Path)
		}
	}
	got, err := ParseConfig(files[0].Data)
	if err != nil || got != p.Config {
		t.Fatalf("config đọc lại = %+v, %v; muốn %+v", got, err, p.Config)
	}
	if !bytes.Equal(files[1].Data, p.SitesYAML) {
		t.Fatal("sites.yaml giải ra khác đầu vào")
	}
}

// B12: giá trị người dùng không thể đổi hình dạng của user-data.
func TestChenNoiDung(t *testing.T) {
	for _, base := range []string{
		"https://example.com/a\nruncmd: [x]",
		"https://example.com/a'; id; '",
		"https://example.com/$(id)",
		"https://example.com/a b",
		"https://example.com/a`id`",
		"https://example.com/a;id",
		"https://example.com/../x",
		"https://example.com/x/",
		"ftp://example.com/x",
		"https://",
		"http:///x",
		"example.com/x",
		"",
	} {
		p := params()
		p.Base = base
		var ie *InvalidError
		if _, err := Render(p); !errors.As(err, &ie) {
			t.Errorf("Base %q: muốn *InvalidError, được %v", base, err)
		} else if ie.Msg.Vi == "" || ie.Msg.En == "" {
			t.Errorf("Base %q: lỗi thiếu câu song ngữ", base)
		}
		// Gọi thẳng Script với Base bẩn cũng không được mang nó vào shell.
		if s := Script(p); base != "" && strings.Contains(s, base) {
			t.Errorf("Script mang Base bẩn %q vào script", base)
		}
	}

	for _, v := range []string{"v1'; rm -rf /", "a\nb", "$(id)", "", strings.Repeat("x", 65)} {
		p := params()
		p.Version = v
		ud, err := Render(p)
		if err != nil {
			t.Fatalf("Version %q: %v", v, err)
		}
		files, _ := parse(t, ud)
		script := string(files[len(files)-1].Data)
		if !strings.Contains(script, "cho tpan unknown.") {
			t.Errorf("Version %q không thành unknown", v)
		}
		if v != "" && strings.Contains(script, v) {
			t.Errorf("Version %q lọt vào script", v)
		}
	}

	for _, s := range []string{"A" + strings.Repeat("a", 63), strings.Repeat("a", 63), "x'; id; '" + strings.Repeat("a", 55)} {
		p := params()
		p.SumARM64 = s
		if _, err := Render(p); err == nil {
			t.Errorf("sha256 %q được nhận", s)
		}
	}

	evil := []byte("runcmd:\n  - [ rm, -rf, / ]\nwrite_files:\n  - path: /etc/shadow\n    content: 'x'\n" +
		"title: \"nháy kép\" 'nháy đơn' $(id) `id`\n---\nruncmd: [[sh]]\n\x00\xff\n")
	for _, sites := range [][]byte{evil, {}} {
		p := params()
		p.Config.SitesFile = SitesPath
		p.SitesYAML = sites
		ud, err := Render(p)
		if err != nil {
			t.Fatal(err)
		}
		files, runcmd := parse(t, ud)
		checkShape(t, files, runcmd, true)
		if !bytes.Equal(files[1].Data, sites) {
			t.Fatal("sites.yaml giải ra khác từng byte")
		}
		if bytes.Contains(ud, []byte("rm, -rf")) {
			t.Fatal("nội dung sites.yaml nằm dạng rõ trong user-data")
		}
	}
}

// B15: khối kiểm file trạng thái đứng trước mọi lệnh tải và thay file chạy.
func TestScriptKhongThayFileChaySauLanDau(t *testing.T) {
	s := Script(params())
	// Script ghi cứng đường dẫn; đổi BinPath hay StatePath mà quên khuôn thì đỏ.
	if !strings.Contains(s, "\nbin="+BinPath+"\n") {
		t.Fatalf("script không đặt bin=%s", BinPath)
	}
	guard := "if [ -e " + StatePath + " ] && [ -x \"$bin\" ]; then\n  exec \"$bin\" cloud-init run\nfi\n"
	g := strings.Index(s, guard)
	if g < 0 {
		t.Fatalf("không thấy khối chặn đầu:\n%s", s)
	}
	for _, re := range []*regexp.Regexp{regexp.MustCompile(`\bcurl\b`), regexp.MustCompile(`\bmv\b`), regexp.MustCompile(`\bchmod\b`)} {
		locs := re.FindAllStringIndex(s, -1)
		if len(locs) == 0 {
			t.Fatalf("script không có %s", re)
		}
		for _, l := range locs {
			if l[0] < g {
				t.Fatalf("%s ở vị trí %d, trước khối chặn đầu (%d)", re, l[0], g)
			}
		}
	}
	if !strings.HasPrefix(s, "#!/bin/sh\n") || !strings.Contains(s, "\nset -eu\n") {
		t.Fatal("script thiếu shebang hoặc set -eu")
	}
	if strings.Index(s, "set -eu") > g {
		t.Fatal("set -eu phải đứng trước khối chặn đầu")
	}
	if !strings.HasSuffix(s, "\nexec \"$bin\" cloud-init run\n") {
		t.Fatal("script không kết thúc bằng exec tpan cloud-init run")
	}
	for _, want := range []string{"sum='" + sumA + "'", "sum='" + sumB + "'", "'" + params().Base + "/tpan-linux-'\"$arch\"", "dpkg --print-architecture"} {
		if !strings.Contains(s, want) {
			t.Fatalf("script thiếu %q", want)
		}
	}
}

// Kiểm cú pháp script bằng sh -n của máy (nếu có). Đi qua system.LocalSystem
// (argv, script qua stdin) vì ngoài gói system không được import os/exec.
func TestScriptCuPhap(t *testing.T) {
	res, err := system.NewLocal().Run(context.Background(), system.Cmd{Name: "sh", Args: []string{"-n"}, Stdin: []byte(Script(params()))})
	if err != nil {
		var ee *system.ExitError
		if errors.As(err, &ee) {
			t.Fatalf("sh -n báo lỗi cú pháp: %v %s", err, res.Stderr)
		}
		t.Skipf("không chạy được sh: %v", err)
	}
}

func TestQuaTran(t *testing.T) {
	p := params()
	p.Config.SitesFile = SitesPath
	big := make([]byte, 40000)
	if _, err := rand.Read(big); err != nil { // ngẫu nhiên: gzip không nén được
		t.Fatal(err)
	}
	p.SitesYAML = big
	var tl *TooLargeError
	_, err := Render(p)
	if !errors.As(err, &tl) || tl.Max != DefaultMaxBytes || tl.Size <= DefaultMaxBytes {
		t.Fatalf("muốn TooLargeError với trần mặc định, được %v", err)
	}
	p.MaxBytes = 1 << 20
	ud, err := Render(p)
	if err != nil {
		t.Fatalf("trần 1 MiB vẫn lỗi: %v", err)
	}
	p.MaxBytes = len(ud) - 1
	if _, err := Render(p); !errors.As(err, &tl) || tl.Size != len(ud) || tl.Max != len(ud)-1 {
		t.Fatalf("muốn TooLargeError{%d,%d}, được %v", len(ud), len(ud)-1, err)
	}
}

func TestRenderKiemConfig(t *testing.T) {
	for name, mut := range map[string]func(*Params){
		"web lạ":                  func(p *Params) { p.Config.Web = "caddy" },
		"version 2":               func(p *Params) { p.Config.Version = 2 },
		"hash bcrypt":             func(p *Params) { p.Config.AdminHash = "$2y$10$abc" },
		"sites_file thiếu file":   func(p *Params) { p.Config.SitesFile = SitesPath },
		"file thiếu sites_file":   func(p *Params) { p.SitesYAML = []byte("sites: []\n") },
		"sites_file đường dẫn lạ": func(p *Params) { p.Config.SitesFile = "/etc/passwd"; p.SitesYAML = []byte("x") },
	} {
		p := params()
		mut(&p)
		var ie *InvalidError
		if _, err := Render(p); !errors.As(err, &ie) {
			t.Errorf("%s: muốn *InvalidError, được %v", name, err)
		}
	}
}

func TestParseSums(t *testing.T) {
	ok := "# chú thích\n" + sumA + "  tpan-linux-amd64\r\n" + sumB + " *tpan-linux-arm64\n" +
		strings.Repeat("c", 64) + "  tpan-darwin-arm64\n" + sumA + "  tpan-linux-amd64\n"
	a, b, err := ParseSums([]byte(ok))
	if err != nil || a != sumA || b != sumB {
		t.Fatalf("ParseSums = %q %q %v", a, b, err)
	}
	for name, text := range map[string]string{
		"thiếu arm64":     sumA + "  tpan-linux-amd64\n",
		"thiếu amd64":     sumB + "  tpan-linux-arm64\n",
		"hai dòng khác":   sumA + "  tpan-linux-amd64\n" + sumB + "  tpan-linux-arm64\n" + sumB + "  tpan-linux-amd64\n",
		"hex hoa":         strings.ToUpper(sumA) + "  tpan-linux-amd64\n" + sumB + "  tpan-linux-arm64\n",
		"một dấu cách":    sumA + " tpan-linux-amd64\n" + sumB + "  tpan-linux-arm64\n",
		"tên có đuôi lạ":  sumA + "  tpan-linux-amd64.sig\n" + sumB + "  tpan-linux-arm64\n",
		"rỗng":            "",
		"hex 63 ký tự":    sumA[:63] + "  tpan-linux-amd64\n" + sumB + "  tpan-linux-arm64\n",
		"đường dẫn ./tên": sumA + "  ./tpan-linux-amd64\n" + sumB + "  tpan-linux-arm64\n",
	} {
		var ie *InvalidError
		if _, _, err := ParseSums([]byte(text)); !errors.As(err, &ie) {
			t.Errorf("%s: muốn *InvalidError, được %v", name, err)
		}
	}
}

func TestParseConfig(t *testing.T) {
	good := `{"version":1,"web":"apache","private":true,"admin_hash":"$argon2id$v=19$x","sites_file":"/etc/tpan/cloud/sites.yaml"}`
	c, err := ParseConfig([]byte(good))
	if err != nil || c != (Config{Version: 1, Web: "apache", Private: true, AdminHash: "$argon2id$v=19$x", SitesFile: SitesPath}) {
		t.Fatalf("ParseConfig = %+v %v", c, err)
	}
	if _, err := ParseConfig([]byte(`{"version":1,"web":"nginx","private":false,"admin_hash":""}`)); err != nil {
		t.Fatalf("hash rỗng, không sites_file: %v", err)
	}
	for name, s := range map[string]string{
		"trường lạ":      `{"version":1,"web":"nginx","admin_hash":"","password":"x"}`,
		"version 2":      `{"version":2,"web":"nginx","admin_hash":""}`,
		"thiếu version":  `{"web":"nginx","admin_hash":""}`,
		"web lạ":         `{"version":1,"web":"caddy","admin_hash":""}`,
		"hash bcrypt":    `{"version":1,"web":"nginx","admin_hash":"$2y$10$abc"}`,
		"hash argon2i":   `{"version":1,"web":"nginx","admin_hash":"$argon2i$v=19$x"}`,
		"sites_file lạ":  `{"version":1,"web":"nginx","admin_hash":"","sites_file":"/etc/passwd"}`,
		"rác phía sau":   `{"version":1,"web":"nginx","admin_hash":""} {}`,
		"không phải obj": `[1]`,
		"rỗng":           ``,
	} {
		var ie *InvalidError
		if _, err := ParseConfig([]byte(s)); !errors.As(err, &ie) {
			t.Errorf("%s: muốn *InvalidError, được %v", name, err)
		}
	}
}

// In user-data mẫu để người đọc xem bằng mắt: go test -v -run TestDump ./internal/cloudinit/
func TestDump(t *testing.T) {
	p := params()
	p.Config.SitesFile = SitesPath
	p.SitesYAML = []byte("sites:\n  - domain: example.com\n")
	ud, err := Render(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d byte:\n%s", len(ud), ud)
}
