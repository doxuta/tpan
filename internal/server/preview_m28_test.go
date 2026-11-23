package server

// Test của M28 ở tầng API: lọc bản xem trước theo vai (B10), hai lớp độc lập
// (B11), mọi handler trả bản xem trước đi qua writePreview (B12), lỗi của cửa
// thành 409 (S12). Đặc tả: docs/thay-doi/M28-thiet-ke.md mục 3.7.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/sitefs"
	"github.com/doxuta/tpan/internal/system"
)

const (
	m28User     = "usera"
	m28Mine     = "a.example.com" // site của usera
	m28Other    = "b.example.com" // site của admin
	m28Machine  = "/etc/nginx/sites-available/tpan-default-ssl.conf"
	m28Shadow   = "root:$6$m28-bi-mat$abcdefghijklmnop:20000:0:99999:7:::\n"
	m28Htaccess = "# BEGIN WordPress\nRewriteEngine On\n# END WordPress\n"
)

// m28Step là một bước chỉ khai file (Info), không chạm máy.
type m28Step struct{ files []engine.FileChange }

func (s m28Step) Info() engine.StepInfo {
	return engine.StepInfo{ID: "m28.test:" + s.files[0].Path, Files: s.files}
}
func (m28Step) Check(context.Context, system.System) (bool, error) { return false, nil }
func (m28Step) Apply(context.Context, system.System) error         { return nil }
func (m28Step) Undo(context.Context, system.System) error          { return nil }

// m28Plan là apiPlan trên engine thật với System sys.
type m28Plan struct {
	sys  system.System
	plan engine.Plan
}

func (p m28Plan) Preview(ctx context.Context) (engine.Preview, error) {
	return engine.New(p.sys, engine.Options{}).Preview(ctx, p.plan)
}

func (p m28Plan) Execute(context.Context, func(engine.Event)) (engine.Report, error) {
	return engine.Report{}, errors.New("test không chạy thật")
}

// m28Env: store thật có usera chủ a.example.com, admin chủ b.example.com.
type m28Env struct {
	*apiEnv
	mine, other site.Layout
}

func newM28Env(t *testing.T) *m28Env {
	t.Helper()
	e := &m28Env{apiEnv: apiNewEnv(t)}
	ctx := context.Background()
	if err := e.st.CreateUser(ctx, account.User{Name: m28User, Role: account.RoleUser, Created: e.now}, "x"); err != nil {
		t.Fatal(err)
	}
	for dom, owner := range map[string]string{m28Mine: m28User, m28Other: account.AdminName} {
		d := apiDomain(t, dom)
		if err := e.st.SaveSite(ctx, site.Site{Domain: d, Status: site.Active, PHPVersion: "8.3", Created: e.now, Owner: owner}); err != nil {
			t.Fatal(err)
		}
	}
	e.mine, e.other = site.LayoutOf(apiDomain(t, m28Mine)), site.LayoutOf(apiDomain(t, m28Other))
	// Thư mục site đúng chủ như máy thật (neo và public của user site).
	siteOwner := system.Owner{User: e.mine.User, Group: site.WebGroup}
	e.f.SetDir(sitefs.Root, 0o755, system.Root)
	e.f.SetDir(e.mine.Dir, fs.ModeSetgid|0o750, siteOwner)
	e.f.SetDir(e.mine.Public, fs.ModeSetgid|0o750, siteOwner)
	e.f.SetFile("/etc/shadow", []byte(m28Shadow), 0o640, system.Owner{User: "root", Group: "shadow"})
	return e
}

func (e *m28Env) htaccess() string { return e.mine.Public + "/.htaccess" }

// preview chạy planJobKick?dry_run=1 với danh tính id trên System sys, trả mã
// và thân thô.
func (e *m28Env) preview(t *testing.T, id account.Identity, sys system.System, files ...engine.FileChange) (*httptest.ResponseRecorder, engine.Preview) {
	t.Helper()
	e.s.deps.Sys = sys
	var steps []engine.Step
	for _, f := range files {
		steps = append(steps, m28Step{files: []engine.FileChange{f}})
	}
	plan := m28Plan{sys: sys, plan: engine.Plan{Kind: "m28.test", Target: m28Mine, Steps: steps}}
	r := httptest.NewRequest("POST", "/api/v1/test?dry_run=1", nil)
	r = r.WithContext(context.WithValue(r.Context(), identityKey{}, id))
	rec := httptest.NewRecorder()
	e.s.planJobKick(rec, r, jobOpts{action: "m28.test", target: m28Mine}, func(context.Context) (apiPlan, error) { return plan, nil })
	var pv engine.Preview
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &pv); err != nil {
			t.Fatalf("thân không phải Preview: %v\n%s", err, rec.Body)
		}
	}
	return rec, pv
}

func userA() account.Identity { return account.Identity{Name: m28User, Role: account.RoleUser} }

// fileOf trả FileChange có Path p trong bản xem trước.
func fileOf(t *testing.T, pv engine.Preview, p string) engine.FileChange {
	t.Helper()
	for _, st := range pv.Steps {
		for _, f := range st.Info.Files {
			if f.Path == p {
				return f
			}
		}
	}
	t.Fatalf("bản xem trước không có file %s", p)
	return engine.FileChange{}
}

// noBytes đỏ khi thân trả về có secret, dạng thô hay base64 ([]byte của JSON).
func noBytes(t *testing.T, label string, body []byte, secret string) {
	t.Helper()
	for _, s := range []string{secret, base64.StdEncoding.EncodeToString([]byte(secret)), strings.TrimSpace(secret)} {
		if bytes.Contains(body, []byte(s)) {
			t.Errorf("%s: thân trả về chứa nội dung bí mật", label)
		}
	}
	// Một đoạn giữa (base64 lệch hàng vẫn lộ được một phần).
	if bytes.Contains(body, []byte("m28-bi-mat")) || bytes.Contains(body, []byte(base64.StdEncoding.EncodeToString([]byte("$m28-bi-mat$")))) {
		t.Errorf("%s: thân trả về chứa một phần nội dung bí mật", label)
	}
}

// resolveFake đi theo symlink ở tên cuối như kernel (tối đa 40 lần), trả
// đường dẫn thật và FileInfo của nó.
func resolveFake(f *system.FakeSystem, p string) (string, system.FileInfo, error) {
	for range 40 {
		fi, err := f.Stat(p)
		if err != nil {
			return "", system.FileInfo{}, err
		}
		if fi.Mode&fs.ModeSymlink == 0 {
			return p, fi, nil
		}
		t, err := f.Readlink(p)
		if err != nil {
			return "", system.FileInfo{}, err
		}
		if !path.IsAbs(t) {
			t = path.Join(path.Dir(p), t)
		}
		p = t
	}
	return "", system.FileInfo{}, syscall.ELOOP
}

// fakeDD trả lời `timeout -k 1 2 dd if=<p> iflag=...` chạy dưới AsUser, mô
// phỏng kernel: nofollow (khi honorNofollow) từ chối symlink ở tên cuối; quyền
// đọc chỉ có khi user làm chủ file (bit 0400) hoặc file có bit đọc cho người
// khác (0004). Không quyền thì mã 1, không byte nào.
func fakeDD(f *system.FakeSystem, honorNofollow bool) func(system.Cmd) system.Result {
	return func(c system.Cmd) system.Result {
		var p string
		nofollow := false
		for _, a := range c.Args {
			if v, ok := strings.CutPrefix(a, "if="); ok {
				p = v
			}
			if v, ok := strings.CutPrefix(a, "iflag="); ok && slices.Contains(strings.Split(v, ","), "nofollow") {
				nofollow = true
			}
		}
		if c.AsUser == "" || c.AsUser == "root" {
			return system.Result{ExitCode: 99, Stderr: "fakeDD: lệnh đọc lại phải chạy bằng user của site"}
		}
		if fi, err := f.Stat(p); err == nil && fi.Mode&fs.ModeSymlink != 0 && nofollow && honorNofollow {
			return system.Result{ExitCode: 1, Stderr: "dd: failed to open: Too many levels of symbolic links"}
		}
		real, fi, err := resolveFake(f, p)
		if err != nil {
			return system.Result{ExitCode: 1, Stderr: "dd: failed to open: " + err.Error()}
		}
		mine := fi.Owner.User == c.AsUser && fi.Mode&0o400 != 0
		if !mine && fi.Mode&0o004 == 0 {
			return system.Result{ExitCode: 1, Stderr: "dd: failed to open: Permission denied"}
		}
		data, err := f.ReadFile(real)
		if err != nil {
			return system.Result{ExitCode: 1, Stderr: "dd: " + err.Error()}
		}
		if int64(len(data)) > sitefs.MaxFile+1 {
			data = data[:sitefs.MaxFile+1]
		}
		return system.Result{Stdout: string(data)}
	}
}

var ddPrefix = []string{"timeout", "-k", "1", "2", "dd"}

// TestBanXemTruocLocTheoVai (B10): vai user chỉ thấy nội dung file của site
// mình; file trong thư mục site chỉ khi user Linux của site đọc lại ra đúng.
func TestBanXemTruocLocTheoVai(t *testing.T) {
	e := newM28Env(t)
	e.f.SetFile(m28Machine, []byte("server { listen 443 default_server; }\n"), 0o644, system.Root)
	e.f.SetFile(e.mine.NginxConf, []byte("server { server_name a.example.com; }\n"), 0o644, system.Root)
	e.f.SetFile(e.other.NginxConf, []byte("server { server_name b.example.com; }\n"), 0o644, system.Root)
	e.f.SetFile(e.htaccess(), []byte(m28Htaccess), 0o644, system.Owner{User: e.mine.User, Group: site.WebGroup})
	e.f.Handle(ddPrefix, fakeDD(e.f, true))
	files := []engine.FileChange{
		{Path: m28Machine, New: []byte("server { listen 443 default_server; ssl; }\n")},
		{Path: e.mine.NginxConf, New: []byte("server { server_name a.example.com; ssl; }\n")},
		{Path: e.other.NginxConf, New: []byte("server { server_name b.example.com; ssl; }\n")},
		{Path: e.htaccess(), New: []byte("# tpan\n")},
	}

	t.Run("admin", func(t *testing.T) {
		e.f.ResetRan()
		rec, pv := e.preview(t, account.Root, e.f, files...)
		if rec.Code != 200 {
			t.Fatalf("mã %d: %s", rec.Code, rec.Body)
		}
		for _, f := range files {
			got := fileOf(t, pv, f.Path)
			if got.Hidden != "" || len(got.Old) == 0 || len(got.New) == 0 {
				t.Errorf("admin: %s bị lọc: %+v", f.Path, got)
			}
		}
		if slices.ContainsFunc(e.f.Ran(), func(c system.Cmd) bool { return c.Name == "timeout" }) {
			t.Error("admin không được chạy lệnh đọc lại")
		}
	})

	t.Run("user", func(t *testing.T) {
		e.f.ResetRan()
		rec, pv := e.preview(t, userA(), e.f, files...)
		if rec.Code != 200 {
			t.Fatalf("mã %d: %s", rec.Code, rec.Body)
		}
		for _, p := range []string{m28Machine, e.other.NginxConf} {
			got := fileOf(t, pv, p)
			if got.Hidden != "foreign" || got.Old != nil || got.New != nil || got.OldLink != "" {
				t.Errorf("%s phải hidden foreign, không old/new: %+v", p, got)
			}
		}
		noBytes(t, "file của máy", rec.Body.Bytes(), "listen 443 default_server")
		noBytes(t, "vhost site khác", rec.Body.Bytes(), "server_name b.example.com")
		if got := fileOf(t, pv, e.mine.NginxConf); got.Hidden != "" || !strings.Contains(string(got.Old), "a.example.com") || len(got.New) == 0 {
			t.Errorf("vhost của chính site phải giữ nguyên: %+v", got)
		}
		if got := fileOf(t, pv, e.htaccess()); got.Hidden != "" || string(got.Old) != m28Htaccess || string(got.New) != "# tpan\n" {
			t.Errorf(".htaccess đọc lại khớp phải giữ: %+v", got)
		}
		// Lệnh đọc lại: đúng argv, dưới user của site, đúng một lần.
		var verify []system.Cmd
		for _, c := range e.f.Ran() {
			if c.Name == "timeout" {
				verify = append(verify, c)
			}
		}
		want := []string{"-k", "1", "2", "dd", "if=" + e.htaccess(), "iflag=nonblock,nofollow", fmt.Sprintf("bs=%d", sitefs.MaxFile+1), "count=1", "status=none"}
		if len(verify) != 1 || verify[0].AsUser != e.mine.User || !slices.Equal(verify[0].Args, want) {
			t.Errorf("lệnh đọc lại = %+v, muốn một lệnh dưới %s với %q", verify, e.mine.User, want)
		}
	})

	t.Run("user, đọc lại ra khác", func(t *testing.T) {
		e.f.Handle(ddPrefix, func(system.Cmd) system.Result { return system.Result{Stdout: "khác"} })
		defer e.f.Handle(ddPrefix, fakeDD(e.f, true))
		rec, pv := e.preview(t, userA(), e.f, files...)
		if rec.Code != 200 {
			t.Fatalf("mã %d: %s", rec.Code, rec.Body)
		}
		got := fileOf(t, pv, e.htaccess())
		if got.Hidden != "unverified" || got.Old != nil || string(got.New) != "# tpan\n" || !got.OldExists {
			t.Errorf(".htaccess đọc lại khác: muốn unverified, giữ new và old_exists: %+v", got)
		}
		noBytes(t, ".htaccess chưa xác minh", rec.Body.Bytes(), "RewriteEngine On")
	})

	t.Run("user, lệnh đọc lại lỗi", func(t *testing.T) {
		e.f.Handle(ddPrefix, func(system.Cmd) system.Result { return system.Result{ExitCode: 124, Stdout: m28Htaccess} })
		defer e.f.Handle(ddPrefix, fakeDD(e.f, true))
		_, pv := e.preview(t, userA(), e.f, files...)
		if got := fileOf(t, pv, e.htaccess()); got.Hidden != "unverified" || got.Old != nil {
			t.Errorf("lệnh đọc lại thoát khác 0 thì không tin stdout: %+v", got)
		}
	})

	t.Run("user, site đang tạo là đích", func(t *testing.T) {
		d := apiDomain(t, "c.example.com")
		l := site.LayoutOf(d)
		pv := engine.Preview{Steps: []engine.StepPreview{{Info: engine.StepInfo{Files: []engine.FileChange{
			{Path: l.NginxConf, New: []byte("server c")},
			{Path: m28Machine, New: []byte("x"), Old: []byte("y"), OldExists: true},
		}}}}}
		r := httptest.NewRequest("POST", "/", nil)
		r = r.WithContext(context.WithValue(r.Context(), identityKey{}, userA()))
		if err := e.s.filterPreview(r, &pv, d); err != nil {
			t.Fatal(err)
		}
		got := pv.Steps[0].Info.Files
		if got[0].Hidden != "" || string(got[0].New) != "server c" {
			t.Errorf("vhost của site đích phải giữ: %+v", got[0])
		}
		if got[1].Hidden != "foreign" || got[1].Old != nil || got[1].New != nil {
			t.Errorf("file của máy phải ẩn: %+v", got[1])
		}
	})
}

// m28BrokenRead là lớp đọc hỏng kiểu v0.2.0: Stat và ReadFile đi theo symlink
// ở tên cuối, nên engine thấy .htaccess là symlink như một file thường và đọc
// luôn đích.
type m28BrokenRead struct{ *system.FakeSystem }

func (b m28BrokenRead) Stat(p string) (system.FileInfo, error) {
	_, fi, err := resolveFake(b.FakeSystem, p)
	return fi, err
}

func (b m28BrokenRead) ReadFile(p string) ([]byte, error) {
	real, _, err := resolveFake(b.FakeSystem, p)
	if err != nil {
		return nil, err
	}
	return b.FakeSystem.ReadFile(real)
}

// TestBanXemTruocHaiLop (B11): hỏng một lớp vẫn không lộ.
func TestBanXemTruocHaiLop(t *testing.T) {
	e := newM28Env(t)
	e.f.SetSymlink("/etc/shadow", e.htaccess())
	file := engine.FileChange{Path: e.htaccess(), New: []byte("# tpan\n")}
	broken := m28BrokenRead{e.f}

	// Đối chứng: lớp đọc hỏng thật sự đưa /etc/shadow vào Old (admin không bị
	// lọc). Thiếu đối chứng này thì phần (a) xanh mà không chứng minh gì.
	e.f.Handle(ddPrefix, fakeDD(e.f, true))
	rec, pv := e.preview(t, account.Root, broken, file)
	if rec.Code != 200 || string(fileOf(t, pv, e.htaccess()).Old) != m28Shadow {
		t.Fatalf("đối chứng: lớp đọc hỏng phải đọc được /etc/shadow cho admin; mã %d, %+v", rec.Code, pv)
	}

	// (a) Lớp đọc hỏng, lớp API lọc: vai user không nhận byte nào. Hai biến
	// thể của phép đọc lại: kernel theo nofollow, và kernel bỏ qua nofollow
	// (chỉ còn quyền file chặn: /etc/shadow là root:shadow 0640).
	for _, honor := range []bool{true, false} {
		t.Run(fmt.Sprintf("a, nofollow=%v", honor), func(t *testing.T) {
			e.f.Handle(ddPrefix, fakeDD(e.f, honor))
			rec, pv := e.preview(t, userA(), broken, file)
			if rec.Code != 200 {
				t.Fatalf("mã %d: %s", rec.Code, rec.Body)
			}
			got := fileOf(t, pv, e.htaccess())
			if got.Old != nil || got.Hidden != "unverified" {
				t.Errorf("vai user nhận %+v, muốn hidden unverified không old", got)
			}
			noBytes(t, "lớp đọc hỏng", rec.Body.Bytes(), m28Shadow)
		})
	}

	// (b) Lớp API không lọc (admin), lớp đọc đúng (FakeSystem thật, engine
	// Stat không theo symlink): admin chỉ thấy đích của link.
	t.Run("b", func(t *testing.T) {
		e.f.Handle(ddPrefix, fakeDD(e.f, true))
		rec, pv := e.preview(t, account.Root, e.f, file)
		if rec.Code != 200 {
			t.Fatalf("mã %d: %s", rec.Code, rec.Body)
		}
		got := fileOf(t, pv, e.htaccess())
		if got.Old != nil || got.OldLink != "/etc/shadow" || !got.OldExists {
			t.Errorf("admin trên lớp đọc đúng: %+v, muốn old_link /etc/shadow, không old", got)
		}
		noBytes(t, "lớp API không lọc", rec.Body.Bytes(), m28Shadow)
	})
}

// TestLoiCuaThanh409 (S12): lỗi của cửa là 409 site_path_unsafe, trừ "không có".
func TestLoiCuaThanh409(t *testing.T) {
	const p = "/var/www/a.example.com/public/.htaccess"
	for _, c := range []struct {
		name   string
		err    error
		status int
	}{
		{"symlink", sitefs.ErrSymlink, 409},
		{"FIFO", sitefs.ErrNotRegular, 409},
		{"quá lớn", sitefs.ErrTooLarge, 409},
		{"chủ", sitefs.ErrOwner, 409},
		{"gốc", sitefs.ErrRootUnsafe, 409},
		{"ENOTDIR", syscall.ENOTDIR, 409},
		{"EACCES", syscall.EACCES, 409},
		{"ENOENT", syscall.ENOENT, 500},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := fmt.Errorf("xem trước bước site.htaccess: %w", &sitefs.Error{Op: "đọc", Path: p, Err: c.err})
			status, body := apiErrorStatus(err)
			if status != c.status {
				t.Fatalf("mã %d, muốn %d (%+v)", status, c.status, body)
			}
			if status != 409 {
				if body.Error.Code == "site_path_unsafe" {
					t.Error("ENOENT không được thành site_path_unsafe")
				}
				return
			}
			m := body.Error.Message
			if body.Error.Code != "site_path_unsafe" {
				t.Errorf("mã lỗi %q", body.Error.Code)
			}
			for _, s := range []string{m.Vi, m.En} {
				if !strings.Contains(s, p) || !strings.Contains(s, "ls -la "+p) {
					t.Errorf("câu phải nêu đường dẫn và cách xem: %q", s)
				}
			}
			if errors.Is(c.err, sitefs.ErrSymlink) && !strings.Contains(m.Vi, sitefs.ErrSymlink.Error()) {
				t.Errorf("lý do tiếng Việt phải lấy từ lỗi: %q", m.Vi)
			}
			if m.En == "" || m.En == m.Vi {
				t.Errorf("thiếu bản tiếng Anh: %q", m.En)
			}
		})
	}
	// Không phải lỗi của cửa: như cũ.
	if status, _ := apiErrorStatus(errors.New("lỗi khác")); status != 500 {
		t.Errorf("lỗi thường: %d", status)
	}
}

// previewLeaks trả tên các hàm (hay "<cấp gói>") gọi X.Preview( hoặc
// X.PreviewBatch( mà không gọi writePreview hay filterPreview, kể cả trong
// closure bên trong hàm.
func previewLeaks(f *ast.File) []string {
	var bad []string
	for _, d := range f.Decls {
		name := "<cấp gói>"
		if fd, ok := d.(*ast.FuncDecl); ok {
			name = fd.Name.Name
		}
		calls, filtered := false, false
		ast.Inspect(d, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			var fn string
			switch x := c.Fun.(type) {
			case *ast.SelectorExpr:
				fn = x.Sel.Name
			case *ast.Ident:
				fn = x.Name
			}
			switch fn {
			case "Preview", "PreviewBatch":
				calls = true
			case "writePreview", "filterPreview":
				filtered = true
			}
			return true
		})
		if calls && !filtered {
			bad = append(bad, name)
		}
	}
	return bad
}

// previewExempt: hàm gọi .Preview( mà không phải bản xem trước của engine.
var previewExempt = map[string]string{
	"handleAIPreview": "ai.Preview là bản xem trước ngữ cảnh gửi AI (không có FileChange), route /ai/* chỉ admin",
}

// TestMoiHandlerXemTruocQuaWritePreview (B12, S17).
func TestMoiHandlerXemTruocQuaWritePreview(t *testing.T) {
	// Bộ quét bắt được hàm xấu tổng hợp (cả trong closure), và không bắt hàm tốt.
	const src = `package x
func bad(w, r) { pv, _ := p.Preview(ctx); writeJSON(w, 200, pv) }
func badBatch() { go func() { e.PreviewBatch(ctx, b) }() }
func good(w, r) { pv, _ := p.Preview(ctx); s.writePreview(w, r, pv) }
func goodWrapped(w, r) { pv, _ := h.Preview(ctx); _ = s.filterPreview(r, &pv); writeJSON(w, 200, pv) }
func iface() { type x interface{ Preview() } }
`
	sf, err := parser.ParseFile(token.NewFileSet(), "x.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := previewLeaks(sf); !slices.Equal(got, []string{"bad", "badBatch"}) {
		t.Fatalf("bộ quét tự kiểm: %v, muốn [bad badBatch]", got)
	}

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, fn := range previewLeaks(f) {
			seen[fn] = true
			if _, ok := previewExempt[fn]; !ok {
				t.Errorf("%s: %s gọi .Preview(/.PreviewBatch( mà không qua writePreview hay filterPreview", name, fn)
			}
		}
	}
	for fn := range previewExempt {
		if !seen[fn] {
			t.Errorf("ngoại lệ %s không còn gọi .Preview(: xoá khỏi previewExempt", fn)
		}
	}

	// Bản xem trước đã lưu (Plan chờ duyệt) không đi qua .Preview( nên phép
	// quét không thấy: mọi route /plans phải chỉ cho admin.
	n := 0
	for _, rt := range (&Server{}).routes(authzDist) {
		if strings.Contains(rt.pattern, " /api/v1/plans") {
			n++
			if rt.rule != adminOnly {
				t.Errorf("%s: luật phải là admin (trả bản xem trước đã lưu, không qua bộ lọc)", rt.pattern)
			}
		}
	}
	if n == 0 {
		t.Error("không thấy route /api/v1/plans nào: bảng route đổi tên?")
	}
}

// TestVungSiteDocLaiBangUserSite (S9, B11): phép đọc lại với LocalSystem thật
// và user thật (nobody). Cần root trên Linux: chạy trong máy ảo bằng
// make m28-test-vm.
func TestVungSiteDocLaiBangUserSite(t *testing.T) {
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		t.Skip("cần root trên Linux (runuser, quyền file thật): chạy trong máy ảo")
	}
	ctx := context.Background()
	sys := system.NewLocal()
	nobody := system.Owner{User: "nobody", Group: "nogroup"}
	// t.TempDir là 0700 của root: mở cho nobody đi qua.
	tmp := t.TempDir()
	for _, d := range []string{path.Dir(tmp), tmp} {
		if err := sys.Chmod(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	dir := tmp + "/nobody"
	if err := sys.MkdirAll(dir, 0o755, nobody); err != nil {
		t.Fatal(err)
	}

	own := dir + "/own.txt"
	if err := sys.WriteFileAtomic(own, []byte("của nobody\n"), 0o600, nobody); err != nil {
		t.Fatal(err)
	}
	if !verifyAsSiteUser(ctx, sys, "nobody", own, []byte("của nobody\n")) {
		t.Error("file của chính nobody phải khớp")
	}
	if verifyAsSiteUser(ctx, sys, "nobody", own, []byte("khác\n")) {
		t.Error("nội dung khác phải không khớp")
	}

	// File thường của root 0600 trong thư mục của nobody: chỉ quyền kernel chặn.
	rootFile := dir + "/root.txt"
	if err := sys.WriteFileAtomic(rootFile, []byte("của root\n"), 0o600, system.Root); err != nil {
		t.Fatal(err)
	}
	if verifyAsSiteUser(ctx, sys, "nobody", rootFile, []byte("của root\n")) {
		t.Error("file 0600 của root: nobody không được đọc ra")
	}

	// Symlink tới /etc/shadow, Old dựng tay đúng bằng các byte của /etc/shadow
	// (giả lập lớp đọc của root đã đi theo link).
	shadow, err := sys.ReadFile("/etc/shadow")
	if err != nil {
		t.Fatal(err)
	}
	link := dir + "/.htaccess"
	if err := sys.Symlink("/etc/shadow", link); err != nil {
		t.Fatal(err)
	}
	if verifyAsSiteUser(ctx, sys, "nobody", link, shadow) {
		t.Error("symlink tới /etc/shadow phải không khớp")
	}

	// FIFO: trả lời trong hạn, không để lại tiến trình.
	fifo := dir + "/fifo"
	if _, err := sys.Run(ctx, system.Cmd{Name: "mkfifo", Args: []string{"-m", "0644", fifo}}); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if verifyAsSiteUser(ctx, sys, "nobody", fifo, []byte("x")) {
		t.Error("FIFO phải không khớp")
	}
	if d := time.Since(start); d > verifyTimeout {
		t.Errorf("FIFO mất %v, muốn dưới %v", d, verifyTimeout)
	}
	res, err := sys.Run(ctx, system.Cmd{Name: "pgrep", Args: []string{"-u", "nobody", "-x", "dd|timeout"}})
	if res.ExitCode != 1 {
		t.Errorf("còn tiến trình dd/timeout của nobody: mã %d, %q, %v", res.ExitCode, res.Stdout, err)
	}
}
