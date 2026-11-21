package core

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/doxuta/tpan/internal/cloudinit"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

var (
	ciSumA = strings.Repeat("a", 64)
	ciSumB = strings.Repeat("b", 64)
)

// ciFakeHash là bản băm giả có tiền tố argon2id (config.json đòi), nhanh hơn
// argon2 thật nhiều lần; test vẫn kiểm được "bản băm này của mật khẩu nào".
func ciFakeHash(pw string) (string, error) {
	s := sha256.Sum256([]byte(pw))
	return "$argon2id$v=19$m=1,t=1,p=1$" + hex.EncodeToString(s[:]), nil
}

func newCloudInit() (*CloudInit, *system.FakeSystem) {
	f := system.NewFake()
	return &CloudInit{Sys: f, Version: "v0.3.0", Hash: ciFakeHash}, f
}

func ciSites(t *testing.T, text string) *SitesFile {
	t.Helper()
	sf, err := ParseSitesFile(strings.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}
	return &sf
}

// ciWantRefused trả câu của lời từ chối (không trả *RefusedError: nó là error,
// errcheck đòi kiểm mọi chỗ gọi).
func ciWantRefused(t *testing.T, err error, code string) i18n.Text {
	t.Helper()
	var re *RefusedError
	if !errors.As(err, &re) || re.Code != code {
		t.Fatalf("muốn lời từ chối %s, nhận %v", code, err)
	}
	return re.Message
}

// ciFiles đọc lại user-data như cloud-init: trả nội dung đã giải mã của từng
// file theo đường dẫn, và runcmd.
func ciFiles(t *testing.T, ud []byte) (map[string][]byte, [][]string) {
	t.Helper()
	var doc struct {
		WriteFiles []struct {
			Path, Encoding, Content string
		} `yaml:"write_files"`
		Runcmd [][]string `yaml:"runcmd"`
	}
	if err := yaml.Unmarshal(ud, &doc); err != nil {
		t.Fatalf("đọc lại user-data: %v", err)
	}
	out := map[string][]byte{}
	for _, f := range doc.WriteFiles {
		b := []byte(f.Content)
		switch f.Encoding {
		case "b64", "gz+b64":
			var err error
			if b, err = base64.StdEncoding.DecodeString(f.Content); err != nil {
				t.Fatalf("%s: base64: %v", f.Path, err)
			}
			if f.Encoding == "gz+b64" {
				zr, err := gzip.NewReader(bytes.NewReader(b))
				if err != nil {
					t.Fatalf("%s: gzip: %v", f.Path, err)
				}
				if b, err = io.ReadAll(zr); err != nil {
					t.Fatalf("%s: gzip: %v", f.Path, err)
				}
			}
		}
		out[f.Path] = b
	}
	return out, doc.Runcmd
}

// S12 và mô hình đe doạ: base http:// không có sha256 sẵn thì không bao giờ tự
// tải SHA256SUMS; API (AllowFetch false) không tải gì kể cả với https.
func TestRenderHttpCanSums(t *testing.T) {
	cases := []struct {
		name string
		in   CloudInitInput
		want string
	}{
		{"http không --sums", CloudInitInput{Web: Nginx, Base: "http://example.com/tpan", AllowFetch: true}, "--sums"},
		{"API với https", CloudInitInput{Web: Nginx, Base: "https://example.com/tpan"}, "sha256_amd64"},
		{"API chỉ một sha256", CloudInitInput{Web: Nginx, Base: "https://example.com/tpan", SumAMD64: ciSumA}, "sha256_arm64"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, f := newCloudInit()
			_, err := c.Render(context.Background(), tc.in)
			re := ciWantRefused(t, err, CodeCloudInitInvalid)
			if !strings.Contains(re.Vi, tc.want) {
				t.Errorf("câu từ chối phải nói %q: %s", tc.want, re.Vi)
			}
			if ran := f.Ran(); len(ran) != 0 {
				t.Errorf("không được chạy lệnh nào, đã chạy %v", ran)
			}
		})
	}
	// Có --sums thì base http:// dùng được.
	c, f := newCloudInit()
	sums := []byte(ciSumA + "  tpan-linux-amd64\n" + ciSumB + "  tpan-linux-arm64\n")
	if _, err := c.Render(context.Background(), CloudInitInput{Web: Nginx, Base: "http://example.com/tpan", Sums: sums, AllowFetch: true}); err != nil {
		t.Fatal(err)
	}
	if len(f.Ran()) != 0 {
		t.Errorf("có --sums thì không tải gì: %v", f.Ran())
	}
}

// B6 ở tầng core: user-data (kể cả sau khi giải base64 và gzip) không chứa mật
// khẩu trả về, và bản băm trong config.json kiểm được bằng đúng mật khẩu đó.
func TestRenderKhongCoMatKhau(t *testing.T) {
	c, _ := newCloudInit()
	c.Hash = store.HashPassword
	res, err := c.Render(context.Background(), CloudInitInput{
		Web: Apache, Private: true, Base: "https://example.com/tpan", SumAMD64: ciSumA, SumARM64: ciSumB,
		Sites: ciSites(t, "defaults:\n  admin_email: admin@example.com\nsites:\n  - domain: a.example.com\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	pw := res.AdminPassword.Reveal()
	if len(pw) != sitePasswordLen {
		t.Fatalf("mật khẩu phải %d ký tự, có %d", sitePasswordLen, len(pw))
	}
	if res.Bytes != len(res.UserData) {
		t.Errorf("Bytes = %d, cỡ thật %d", res.Bytes, len(res.UserData))
	}
	if bytes.Contains(res.UserData, []byte(pw)) {
		t.Fatal("user-data chứa mật khẩu dạng rõ")
	}
	files, runcmd := ciFiles(t, res.UserData)
	for p, b := range files {
		if bytes.Contains(b, []byte(pw)) {
			t.Fatalf("%s (đã giải mã) chứa mật khẩu dạng rõ", p)
		}
	}
	if !slices.EqualFunc(runcmd, [][]string{{cloudinit.ScriptPath}}, slices.Equal) {
		t.Errorf("runcmd = %v", runcmd)
	}
	cfg, err := cloudinit.ParseConfig(files[cloudinit.ConfigPath])
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Web != "apache" || !cfg.Private || cfg.SitesFile != cloudinit.SitesPath {
		t.Errorf("config = %+v", cfg)
	}
	ok, err := store.VerifyPassword(cfg.AdminHash, pw)
	if err != nil || !ok {
		t.Fatalf("bản băm không khớp mật khẩu trả về: ok=%v err=%v", ok, err)
	}
	// JSON của kết quả không lộ mật khẩu (Secret) và không có user-data.
	j, _ := json.Marshal(res)
	if bytes.Contains(j, []byte(pw)) {
		t.Errorf("JSON của CloudInitResult lộ mật khẩu: %s", j)
	}
}

func TestRenderSitesTuChoi(t *testing.T) {
	cases := []struct {
		name, yaml, code, want string
	}{
		{"thiếu email", "sites:\n  - domain: a.example.com\n", CodeCloudInitInvalid, "admin_email"},
		{"owner ở dòng", "defaults:\n  admin_email: a@example.com\nsites:\n  - domain: a.example.com\n    owner: bob\n", CodeCloudInitInvalid, "owner"},
		{"owner ở defaults", "defaults:\n  admin_email: a@example.com\n  owner: bob\nsites:\n  - domain: a.example.com\n", CodeCloudInitInvalid, "owner"},
		{"ssl ở dòng", "defaults:\n  admin_email: a@example.com\nsites:\n  - domain: a.example.com\n    ssl: true\n", CodeCloudInitInvalid, "ssl"},
		{"ssl ở defaults", "defaults:\n  admin_email: a@example.com\n  ssl: true\nsites:\n  - domain: a.example.com\n", CodeCloudInitInvalid, "ssl"},
		// Lời từ chối của Resolve trả nguyên mã gốc.
		{"domain sai", "defaults:\n  admin_email: a@example.com\nsites:\n  - domain: 'a.com; rm -rf /'\n", "domain_invalid", "dòng 1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, f := newCloudInit()
			_, err := c.Render(context.Background(), CloudInitInput{
				Web: Nginx, Base: "https://example.com/tpan", SumAMD64: ciSumA, SumARM64: ciSumB, Sites: ciSites(t, tc.yaml),
			})
			re := ciWantRefused(t, err, tc.code)
			if !strings.Contains(re.Vi, tc.want) {
				t.Errorf("câu từ chối phải nói %q: %s", tc.want, re.Vi)
			}
			if len(f.Ran()) != 0 {
				t.Errorf("không được chạy lệnh nào: %v", f.Ran())
			}
		})
	}
	// owner: admin thì nhận.
	c, _ := newCloudInit()
	if _, err := c.Render(context.Background(), CloudInitInput{
		Web: Nginx, Base: "https://example.com/tpan", SumAMD64: ciSumA, SumARM64: ciSumB,
		Sites: ciSites(t, "defaults:\n  admin_email: a@example.com\n  owner: admin\nsites:\n  - domain: a.example.com\n    ssl: false\n"),
	}); err != nil {
		t.Fatal(err)
	}
}

// B12: sites.yaml được ghi lại từ cấu trúc đã kiểm; chú thích, defaults, ssl,
// owner của người dùng không tới máy mới, email của defaults vào từng dòng.
func TestRenderGhiLaiSitesYAML(t *testing.T) {
	c, _ := newCloudInit()
	src := `# chú thích của người dùng: $(id)
defaults:
  admin_email: Chu@Example.com   # email chung
  owner: admin
sites:
  - domain: A.Example.com
    title: "Blog của tôi"
    ssl: false
  - domain: b.example.com
    admin_email: b@example.com
`
	res, err := c.Render(context.Background(), CloudInitInput{
		Web: Nginx, Base: "https://example.com/tpan", SumAMD64: ciSumA, SumARM64: ciSumB, Sites: ciSites(t, src),
	})
	if err != nil {
		t.Fatal(err)
	}
	files, _ := ciFiles(t, res.UserData)
	got := string(files[cloudinit.SitesPath])
	want := `sites:
  - domain: a.example.com
    title: Blog của tôi
    admin_email: chu@example.com
  - domain: b.example.com
    admin_email: b@example.com
`
	// Email đã qua validate.ParseEmail; nếu nó không hạ chữ thường thì dòng
	// mong đợi theo đúng giá trị đã chuẩn hoá.
	sf := ciSites(t, src)
	list, _ := sf.Resolve()
	want = strings.Replace(want, "chu@example.com", list[0].Input.Email.String(), 1)
	if got != want {
		t.Errorf("sites.yaml ghi lại:\n%s\nmuốn:\n%s", got, want)
	}
	// Máy mới đọc lại được và ra đúng danh sách.
	back := ciSites(t, got)
	if l2, err := back.Resolve(); err != nil || len(l2) != 2 {
		t.Fatalf("đọc lại sites.yaml: %v %v", l2, err)
	}
}

func TestRenderBaseMacDinh(t *testing.T) {
	c, _ := newCloudInit()
	in := CloudInitInput{Web: Nginx, SumAMD64: ciSumA, SumARM64: ciSumB}
	res, err := c.Render(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	files, _ := ciFiles(t, res.UserData)
	if !strings.Contains(string(files[cloudinit.ScriptPath]), "'https://github.com/doxuta/tpan/releases/download/v0.3.0/tpan-linux-'") {
		t.Errorf("script không có base mặc định theo Version:\n%s", files[cloudinit.ScriptPath])
	}

	// Version lạ: không đoán được nơi phát hành, phải có --base.
	c.Version = "bản tự build"
	re := ciWantRefused(t, func() error { _, err := c.Render(context.Background(), in); return err }(), CodeCloudInitInvalid)
	if !strings.Contains(re.Vi, "--base") {
		t.Errorf("câu từ chối phải chỉ sang --base: %s", re.Vi)
	}
	in.Base = "https://example.com/tpan"
	if _, err := c.Render(context.Background(), in); err != nil {
		t.Fatalf("có --base thì Version lạ vẫn sinh được: %v", err)
	}

	// Base sai luật ký tự: cloudinit_invalid, không chạy lệnh nào.
	c2, f := newCloudInit()
	_, err = c2.Render(context.Background(), CloudInitInput{Web: Nginx, Base: "https://example.com/'$(id)'", AllowFetch: true})
	ciWantRefused(t, err, CodeCloudInitInvalid)
	if len(f.Ran()) != 0 {
		t.Errorf("base sai không được tới curl: %v", f.Ran())
	}
}

func TestRenderWebSai(t *testing.T) {
	c, _ := newCloudInit()
	_, err := c.Render(context.Background(), CloudInitInput{Web: "caddy", SumAMD64: ciSumA, SumARM64: ciSumB})
	ciWantRefused(t, err, CodeCloudInitInvalid)
}

func TestRenderQuaTran(t *testing.T) {
	c, _ := newCloudInit()
	in := CloudInitInput{Web: Nginx, Base: "https://example.com/tpan", SumAMD64: ciSumA, SumARM64: ciSumB, MaxBytes: 100}
	_, err := c.Render(context.Background(), in)
	re := ciWantRefused(t, err, CodeCloudInitTooLarge)
	for _, s := range []string{"AWS EC2 16 KB", "GCP 256 KB", "--max-bytes", "100 byte"} {
		if !strings.Contains(re.Vi, s) {
			t.Errorf("câu từ chối thiếu %q: %s", s, re.Vi)
		}
	}
	in.MaxBytes = -1
	_, err = c.Render(context.Background(), in)
	ciWantRefused(t, err, CodeCloudInitInvalid)
}

// CLI không có --sums với base https://: tải <base>/SHA256SUMS bằng đúng argv
// đã chốt (chỉ https, cả khi bị chuyển hướng; hạn 60 giây).
func TestRenderTaiSHA256SUMS(t *testing.T) {
	c, f := newCloudInit()
	f.Handle([]string{"curl"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: ciSumA + "  tpan-linux-amd64\n" + ciSumB + " *tpan-linux-arm64\n" + ciSumA + "  khac.txt\n"}
	})
	res, err := c.Render(context.Background(), CloudInitInput{Web: Nginx, Base: "https://example.com/tpan", AllowFetch: true})
	if err != nil {
		t.Fatal(err)
	}
	ran := f.Ran()
	want := []string{"-fsSL", "--proto", "=https", "--proto-redir", "=https", "--max-time", "60", "https://example.com/tpan/SHA256SUMS"}
	if len(ran) != 1 || ran[0].Name != "curl" || !slices.Equal(ran[0].Args, want) {
		t.Fatalf("lệnh đã chạy = %v, muốn curl %v", ran, want)
	}
	files, _ := ciFiles(t, res.UserData)
	script := string(files[cloudinit.ScriptPath])
	if !strings.Contains(script, "amd64) sum='"+ciSumA+"'") || !strings.Contains(script, "arm64) sum='"+ciSumB+"'") {
		t.Errorf("script không ghim đúng sha256:\n%s", script)
	}

	// curl lỗi: từ chối, chỉ sang --sums.
	c2, f2 := newCloudInit()
	f2.Handle([]string{"curl"}, func(system.Cmd) system.Result {
		return system.Result{ExitCode: 22, Stderr: "curl: (22) The requested URL returned error: 404\n"}
	})
	_, err = c2.Render(context.Background(), CloudInitInput{Web: Nginx, AllowFetch: true})
	re := ciWantRefused(t, err, CodeCloudInitInvalid)
	if !strings.Contains(re.Vi, "--sums") {
		t.Errorf("câu từ chối phải chỉ sang --sums: %s", re.Vi)
	}

	// Tải về mà thiếu dòng arm64: lời từ chối của ParseSums.
	c3, f3 := newCloudInit()
	f3.Handle([]string{"curl"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: ciSumA + "  tpan-linux-amd64\n"}
	})
	_, err = c3.Render(context.Background(), CloudInitInput{Web: Nginx, AllowFetch: true})
	ciWantRefused(t, err, CodeCloudInitInvalid)
}
