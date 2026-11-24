package main

// Test CLI của M14 (mục 6.13): route explain, security harden, site set
// --htaccess, dòng chế độ .htaccess của site info, lời nhắc sau self-update.
// Các test cần một máy có site thật chạy trên máy demo (internal/demo): vhost,
// pool và `nginx -T` ở đó do chính core ghi và máy giả trả lời.

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/demo"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/htaccess"
	"github.com/doxuta/tpan/internal/server"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

const (
	m14Blog = "blog.example.com" // có SSL
	m14Shop = "shop.example.net"
	m14Wiki = "wiki.example.org" // hỏng socket cho tới khi PHP-FPM khởi động lại
	m14Lab  = "lab.example.org"  // đang tạm khoá
)

// demoCLI là CLI chạy trên máy demo: máy giả giữ trong bộ nhớ, DB của nó nằm
// trong thư mục tạm và được CLI mở lại ở mỗi lệnh như trên máy thật.
func demoCLI(t *testing.T) *cli {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	dir := t.TempDir()
	sys, st, err := demo.New(ctx, dir)
	if err != nil {
		t.Fatalf("dựng máy demo: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	sys.ResetRan() // lệnh của lúc dựng dữ liệu mẫu không phải lệnh của CLI
	_, wpcliSHA := demo.Hashes()
	return &cli{t: t, fake: sys, root: true, db: filepath.Join(dir, "tpan.db"), wpcliSHA512: wpcliSHA}
}

// healWiki khởi động lại PHP-FPM của máy demo: socket của wiki có lại, để Plan
// có kiểm chứng trên mọi site (harden, chuyển web server) không vướng site hỏng sẵn.
func (c *cli) healWiki() {
	c.t.Helper()
	if _, err := c.fake.Run(context.Background(), system.Cmd{Name: "systemctl", Args: []string{"restart", "php8.3-fpm"}}); err != nil {
		c.t.Fatal(err)
	}
}

func (c *cli) setHtaccess(dom string, mode site.HtaccessMode) {
	c.t.Helper()
	c.withStore(func(ctx context.Context, st *store.Store) {
		s, ok, err := st.Site(ctx, domainOf(c.t, dom))
		if err != nil || !ok {
			c.t.Fatalf("site %s: %v %v", dom, ok, err)
		}
		s.Htaccess = mode
		if err := st.SaveSite(ctx, s); err != nil {
			c.t.Fatal(err)
		}
	})
}

func (c *cli) htaccessOf(dom string) (mode site.HtaccessMode) {
	c.t.Helper()
	c.withStore(func(ctx context.Context, st *store.Store) {
		s, _, _ := st.Site(ctx, domainOf(c.t, dom))
		mode = s.Htaccess
	})
	return mode
}

// badHtaccess đặt vào site một .htaccess có chỉ thị ngoài danh sách ở dòng 3.
func (c *cli) badHtaccess(dom string) string {
	l := site.LayoutOf(domainOf(c.t, dom))
	p := l.Public + "/.htaccess"
	c.fake.SetFile(p, []byte("# BEGIN WordPress\nRewriteEngine On\nSetHandler application/x-httpd-php\n# END WordPress\n"), 0o644, system.Owner{User: l.User, Group: l.User})
	return p
}

// ---------------------------------------------------------------- đầu vào

func TestM14ArgsRejectedBeforeAnything(t *testing.T) {
	for _, args := range [][]string{
		{"route", "explain", "a.com; rm -rf /"},
		{"route", "explain", "$(id)"},
		{"route", "explain", "../../etc/passwd"},
		{"site", "set", "a..b", "--htaccess", "tat"},
		{"site", "set", "a.example.com", "--htaccess", "all"},
		{"site", "set", "a.example.com", "--htaccess", "tat; id"},
		{"site", "set", "a.example.com"},
	} {
		c := newCLI(t)
		code, out, _ := c.run(append(args, "--json")...)
		if code != 1 || len(c.fake.Ran()) != 0 || c.dbExists() {
			t.Errorf("%q: mã %d, lệnh %v, DB %v", args, code, c.fake.Ran(), c.dbExists())
		}
		want := "domain_invalid"
		if args[0] == "site" && args[2] == "a.example.com" {
			want = "htaccess_mode_invalid"
		}
		if got := refusalCode(t, out); got != want {
			t.Errorf("%q: mã từ chối %s; muốn %s", args, got, want)
		}
		if strings.Contains(out, "rm -rf") || strings.Contains(out, "; id") {
			t.Errorf("%q: lời từ chối in lại chuỗi lạ: %s", args, out)
		}
	}
	// Thiếu tham số: cobra báo, không chạy gì.
	c := newCLI(t)
	if code, _, _ := c.run("route", "explain"); code == 0 || len(c.fake.Ran()) != 0 {
		t.Errorf("route explain thiếu domain: mã %d", code)
	}
}

func TestM14NeedsRoot(t *testing.T) {
	for _, args := range [][]string{
		{"route", "explain", "a.example.com"},
		{"security", "harden", "--dry-run"},
		{"security", "harden", "--yes"},
		{"site", "set", "a.example.com", "--htaccess", "tat", "--dry-run"},
	} {
		c := newCLI(t)
		c.root = false
		code, _, errs := c.run(args...)
		if code != 1 || !strings.Contains(errs, "sudo") || len(c.fake.Ran()) != 0 || c.dbExists() {
			t.Errorf("%q: mã %d, stderr %q, lệnh %v", args, code, errs, c.fake.Ran())
		}
	}
}

// Máy chưa có gì: lệnh đọc và --dry-run từ chối rõ ràng, không tạo DB.
func TestM14EmptyMachine(t *testing.T) {
	for args, want := range map[string]string{
		"route explain a.example.com --json":                     "name_unknown", // M19: tên không ai có
		"security harden --dry-run --json":                       "stack_not_installed",
		"site set a.example.com --htaccess tat --dry-run --json": "site_not_found",
	} {
		c := newCLI(t)
		code, out, _ := c.run(strings.Fields(args)...)
		if code != 1 || refusalCode(t, out) != want || c.dbExists() {
			t.Errorf("%s: mã %d, %s, DB %v", args, code, out, c.dbExists())
		}
	}
}

// ---------------------------------------------------------------- route explain

func TestRouteExplainCLI(t *testing.T) {
	c := demoCLI(t)
	l := site.LayoutOf(domainOf(t, m14Shop))
	before := len(c.fake.Snapshot())

	code, out, errs := c.run("route", "explain", m14Shop)
	if code != 0 {
		t.Fatalf("mã %d\n%s%s", code, out, errs)
	}
	var hopLines []string
	for _, ln := range strings.Split(out, "\n") {
		if strings.HasPrefix(ln, "[") {
			hopLines = append(hopLines, ln)
		}
	}
	if len(hopLines) != 8 {
		t.Fatalf("số dòng chặng = %d:\n%s", len(hopLines), out)
	}
	for _, want := range []string{
		"[đạt] Vhost nhận tên miền: " + l.NginxLink + ":",
		"[đạt] Thư mục gốc của vhost: " + l.Public,
		"[đạt] Socket mà vhost chuyển .php tới: " + l.SocketFor("8.3"),
		"[đạt] User Linux mà pool chạy: " + l.User,
		"[đạt] Gọi thật vào site trên chính máy: http://" + m14Shop + "/",
		"Không chặng nào lỗi",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("thiếu %q:\n%s", want, out)
		}
	}
	// Mọi chặng đạt thì không in bằng chứng; --evidence thì in lệnh và output.
	if strings.Contains(out, "$ nginx -T") {
		t.Errorf("không --evidence mà in bằng chứng của chặng đạt:\n%s", out)
	}
	code, out, _ = c.run("route", "explain", m14Shop, "--evidence")
	for _, want := range []string{"$ nginx -T", "$ read " + l.NginxLink + " (tpan)", "root " + l.Public + ";", "user = " + l.User, "$ curl "} {
		if code != 0 || !strings.Contains(out, want) {
			t.Errorf("--evidence thiếu %q (mã %d):\n%s", want, code, out)
		}
	}

	// --json: đúng kiểu của API; --lang en: chữ tiếng Anh.
	code, out, _ = c.run("route", "explain", m14Blog, "--json")
	var tr core.RouteTrace
	if err := json.Unmarshal([]byte(out), &tr); err != nil || code != 0 || tr.Domain != m14Blog || len(tr.Hops) != 8 || tr.Hops[7].Value != "https://"+m14Blog+"/" {
		t.Errorf("--json: mã %d, %v, %+v", code, err, tr)
	}
	if code, out, _ := c.run("route", "explain", m14Shop, "--lang", "en"); code != 0 || !strings.Contains(out, "[pass] Vhost that receives the domain: ") || !strings.Contains(out, "No hop failed") {
		t.Errorf("--lang en: mã %d\n%s", code, out)
	}

	// Site tạm khoá: ba chặng. Site hỏng socket: mã 1, chặng gọi thật đỏ kèm bằng chứng.
	code, out, _ = c.run("route", "explain", m14Lab)
	if code != 0 || strings.Count(out, "\n[") != 3 || !strings.Contains(out, "tạm khoá") {
		t.Errorf("site tạm khoá: mã %d\n%s", code, out)
	}
	code, out, _ = c.run("route", "explain", m14Wiki)
	if code != 1 || !strings.Contains(out, "[lỗi] Gọi thật vào site trên chính máy") || !strings.Contains(out, "$ curl ") || !strings.Contains(out, "Có 1 chặng lỗi") {
		t.Errorf("site hỏng socket: mã %d\n%s", code, out)
	}
	// --json vẫn thoát 1 khi có chặng lỗi, để script dùng được mã thoát.
	if code, out, _ := c.run("route", "explain", m14Wiki, "--json"); code != 1 || !json.Valid([]byte(out)) {
		t.Errorf("--json site hỏng: mã %d\n%s", code, out)
	}

	// Lỗ L8: vhost lạ đứng trước nhận tên miền. Chặng vhost đỏ, nêu file lạ,
	// và in bằng chứng dù không có --evidence.
	const stray = "/etc/nginx/sites-enabled/aaa-la.conf"
	c.fake.SetFile(stray, []byte("server {\n    listen 80;\n    server_name "+m14Shop+";\n    root /srv/la;\n}\n"), 0o644, system.Root)
	code, out, _ = c.run("route", "explain", m14Shop)
	for _, want := range []string{"[lỗi] Vhost nhận tên miền: " + stray + ":3", "$ nginx -T", "[lỗi] Thư mục gốc của vhost: /srv/la", "[chưa rõ] Pool PHP-FPM nghe socket đó: -"} {
		if code != 1 || !strings.Contains(out, want) {
			t.Errorf("vhost lạ thiếu %q (mã %d):\n%s", want, code, out)
		}
	}
	if err := c.fake.Remove(stray); err != nil {
		t.Fatal(err)
	}

	// Lệnh chỉ đọc: không đổi file nào, không ghi nhật ký, không tạo job.
	if n := len(c.fake.Snapshot()); n != before {
		t.Errorf("route explain đổi hệ thống file: %d thành %d mục", before, n)
	}
	for _, cmd := range c.fake.Ran() {
		if !slices.Contains([]string{"getent", "ip", "nginx", "curl", "systemctl"}, cmd.Name) {
			t.Errorf("route explain chạy lệnh ngoài danh sách đọc: %s", cmd.String())
		}
	}
	if c.audits("route.explain") != 0 {
		t.Error("route explain ghi nhật ký")
	}
}

// ---------------------------------------------------------------- security harden

func TestPrintHardenModes(t *testing.T) {
	kept := []core.HtaccessKept{{Domain: "b.example.com", Findings: []htaccess.Finding{
		{File: "/var/www/b.example.com/public/.htaccess", Line: 12, Directive: "SetHandler"},
		{File: "/var/www/b.example.com/public/wp-admin/.htaccess", Line: 3, Directive: "Options FollowSymLinks"},
	}}}
	render := func(lang string, switched []string, kept []core.HtaccessKept) string {
		var out bytes.Buffer
		printHardenModes(&env{stdout: &out, lang: lang}, switched, kept)
		return out.String()
	}
	out := render("vi", []string{"a.example.com", "c.example.com"}, kept)
	for _, want := range []string{
		"Chuyển sang an-toan (2 site", "    a.example.com\n", "    c.example.com\n",
		"Giữ day-du (1 site)", "    b.example.com\n",
		"      /var/www/b.example.com/public/.htaccess:12: SetHandler\n",
		"      /var/www/b.example.com/public/wp-admin/.htaccess:3: Options FollowSymLinks\n",
		"sudo tpan site set <domain> --htaccess an-toan",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("thiếu %q:\n%s", want, out)
		}
	}
	if out := render("en", nil, kept); !strings.Contains(out, "Staying on day-du (1 site(s))") || strings.Contains(out, "Switching") {
		t.Errorf("tiếng Anh, chỉ có site giữ:\n%s", out)
	}
	if out := render("vi", []string{"a.example.com"}, nil); strings.Contains(out, "Giữ day-du") {
		t.Errorf("không site nào giữ mà vẫn in mục giữ:\n%s", out)
	}
	// Không site nào đổi chế độ (máy đã áp bản vá): không in gì thêm.
	if out := render("vi", nil, nil); out != "" {
		t.Errorf("không có gì mà vẫn in: %q", out)
	}
}

func TestSecurityHardenCLI(t *testing.T) {
	c := demoCLI(t)
	c.healWiki()
	// Như máy nâng từ bản trước M14: hai site còn ở day-du. shop sạch nên sẽ
	// chuyển; wiki có chỉ thị ngoài danh sách nên giữ.
	c.setHtaccess(m14Shop, site.HtaccessFull)
	c.setHtaccess(m14Wiki, site.HtaccessFull)
	bad := c.badHtaccess(m14Wiki)
	before := len(c.fake.Snapshot())

	code, out, errs := c.run("security", "harden", "--dry-run")
	for _, want := range []string{"Kế hoạch:", "Chuyển sang an-toan (1 site", "    " + m14Shop + "\n", "Giữ day-du (1 site)", "    " + m14Wiki + "\n", "      " + bad + ":3: SetHandler\n"} {
		if code != 0 || !strings.Contains(out, want) {
			t.Fatalf("--dry-run thiếu %q (mã %d):\n%s%s", want, code, out, errs)
		}
	}
	if len(c.fake.Snapshot()) != before || c.audits("security.harden") != 0 || c.htaccessOf(m14Shop) != site.HtaccessFull {
		t.Error("--dry-run đổi máy, ghi nhật ký hoặc đổi chế độ của site")
	}

	// --dry-run --json: cùng dạng với API (Preview cộng switched, kept).
	code, out, _ = c.run("security", "harden", "--dry-run", "--json")
	var pv server.HardenPreview
	if err := json.Unmarshal([]byte(out), &pv); err != nil || code != 0 {
		t.Fatalf("--json: mã %d, %v\n%s", code, err, out)
	}
	if pv.Kind != "security.harden" || !slices.Equal(pv.Switched, []string{m14Shop}) || len(pv.Kept) != 1 ||
		pv.Kept[0].Domain != m14Wiki || len(pv.Kept[0].Findings) != 1 || pv.Kept[0].Findings[0].Line != 3 {
		t.Errorf("HardenPreview = %s, switched %v, kept %+v", pv.Kind, pv.Switched, pv.Kept)
	}

	// Không --yes, không terminal: không chạy. Có terminal: hai danh sách in
	// TRƯỚC câu hỏi, trả lời "n" thì không đổi gì.
	if code, _, errs := c.run("security", "harden"); code != 1 || !strings.Contains(errs, "--yes") {
		t.Errorf("không xác nhận: mã %d, %q", code, errs)
	}
	c.terminal, c.stdin = true, "n\n"
	code, out, _ = c.run("security", "harden")
	ask, list := strings.Index(out, "[y/N]"), strings.Index(out, bad+":3: SetHandler")
	if code != 1 || ask < 0 || list < 0 || list > ask || c.audits("security.harden") != 0 {
		t.Errorf("trả lời n: mã %d, danh sách ở %d, câu hỏi ở %d\n%s", code, list, ask, out)
	}
	c.terminal, c.stdin = false, ""

	// Chạy thật với --yes: site giữ day-du vẫn được nói ra, job được ghi nhật ký.
	code, out, errs = c.run("security", "harden", "--yes")
	if !strings.Contains(out, bad+":3: SetHandler") || c.audits("security.harden") != 1 {
		t.Fatalf("--yes: mã %d, nhật ký %d\n%s%s", code, c.audits("security.harden"), out, errs)
	}
	if code != 0 || c.htaccessOf(m14Shop) != site.HtaccessSafe || c.htaccessOf(m14Wiki) != site.HtaccessFull {
		t.Errorf("sau harden: mã %d, shop %q, wiki %q\n%s%s", code, c.htaccessOf(m14Shop), c.htaccessOf(m14Wiki), out, errs)
	}
}

// ---------------------------------------------------------------- site set --htaccess

func TestSiteSetHtaccessCLI(t *testing.T) {
	c := demoCLI(t)
	// Trên nginx: cài đặt chưa có tác dụng nên core từ chối; site info không in dòng chế độ.
	if code, out, _ := c.run("site", "set", m14Shop, "--htaccess", "tat", "--dry-run", "--json"); code != 1 || refusalCode(t, out) != core.CodeHtaccessNotApache {
		t.Errorf("nginx: mã %d, %s", code, out)
	}
	if _, out, _ := c.run("site", "info", m14Shop); strings.Contains(out, ".htaccess:") {
		t.Errorf("site info trên nginx in chế độ .htaccess:\n%s", out)
	}

	// Chuyển máy demo sang Apache bằng chính core.Switch (như `tpan stack switch`).
	c.healWiki()
	c.withStore(func(ctx context.Context, st *store.Store) {
		p, err := (&core.Switch{Sys: c.fake, Settings: st, Sites: st, Journal: st}).Prepare(ctx, core.Apache)
		if err != nil {
			t.Fatalf("chuẩn bị chuyển sang Apache: %v", err)
		}
		if rep, err := p.Execute(ctx, nil); err != nil || rep.Job.Status != engine.JobSucceeded {
			t.Fatalf("chuyển sang Apache: %v, %s %s", err, rep.Job.Status, rep.Job.Error)
		}
	})
	l := site.LayoutOf(domainOf(t, m14Shop))
	vhost := func() string {
		b, err := c.fake.ReadFile(l.ApacheConf)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if _, out, _ := c.run("site", "info", m14Shop); !strings.Contains(out, "Chế độ .htaccess: an-toan") {
		t.Errorf("site info trên Apache thiếu chế độ .htaccess:\n%s", out)
	}

	// Xem trước: không đổi vhost, không đổi bảng sites, không nhật ký.
	old := vhost()
	code, out, errs := c.run("site", "set", m14Shop, "--htaccess", "tat", "--dry-run")
	if code != 0 || !strings.Contains(out, "Kế hoạch:") || vhost() != old || c.htaccessOf(m14Shop) != site.HtaccessSafe || c.audits("site.htaccess") != 0 {
		t.Fatalf("--dry-run: mã %d\n%s%s", code, out, errs)
	}
	if code, out, _ := c.run("site", "set", m14Shop, "--htaccess", "an-toan", "--dry-run", "--json"); code != 1 || refusalCode(t, out) != core.CodeHtaccessSame {
		t.Errorf("đặt lại đúng chế độ đang có: mã %d, %s", code, out)
	}
	// day-du: CLI là admin nên được đặt; bản xem trước kèm lời cảnh báo.
	if code, out, _ := c.run("site", "set", m14Shop, "--htaccess", "day-du", "--dry-run"); code != 0 || !strings.Contains(out, "chạy mã dưới user của site khác") {
		t.Errorf("day-du --dry-run: mã %d\n%s", code, out)
	}
	if code, _, errs := c.run("site", "set", m14Shop, "--htaccess", "tat"); code != 1 || !strings.Contains(errs, "--yes") {
		t.Errorf("không xác nhận: mã %d, %q", code, errs)
	}

	// Chạy thật sang "tat": vhost thôi đọc .htaccess, bảng sites và site info đổi theo.
	code, out, errs = c.run("site", "set", m14Shop, "--htaccess", "tat", "--yes")
	if code != 0 || c.htaccessOf(m14Shop) != site.HtaccessOff || c.audits("site.htaccess") != 1 {
		t.Fatalf("sang tat: mã %d, chế độ %q\n%s%s", code, c.htaccessOf(m14Shop), out, errs)
	}
	if v := vhost(); !strings.Contains(v, "FallbackResource /index.php") || strings.Contains(v, "AllowOverrideList") {
		t.Errorf("vhost ở chế độ tat:\n%s", v)
	}
	if _, out, _ := c.run("site", "info", m14Shop); !strings.Contains(out, "Chế độ .htaccess: tat") {
		t.Errorf("site info sau khi đổi:\n%s", out)
	}

	// Về an-toan khi .htaccess có chỉ thị ngoài danh sách: từ chối, in đúng dòng vướng.
	bad := c.badHtaccess(m14Shop)
	code, out, errs = c.run("site", "set", m14Shop, "--htaccess", "an-toan", "--yes")
	if code != 1 || !strings.Contains(errs, bad+":3: SetHandler") || c.htaccessOf(m14Shop) != site.HtaccessOff {
		t.Errorf("an-toan với .htaccess vướng: mã %d\n%s%s", code, out, errs)
	}
	if code, out, _ := c.run("site", "set", m14Shop, "--htaccess", "an-toan", "--yes", "--json"); code != 1 || refusalCode(t, out) != core.CodeHtaccessBlocked {
		t.Errorf("--json: mã %d, %s", code, out)
	}
	// Sửa .htaccess rồi thì chuyển được, và health của site không có lỗi mới.
	c.fake.SetFile(bad, []byte("# BEGIN WordPress\nRewriteEngine On\n# END WordPress\n"), 0o644, system.Owner{User: l.User, Group: l.User})
	code, out, errs = c.run("site", "set", m14Shop, "--htaccess", "an-toan", "--yes")
	if code != 0 || c.htaccessOf(m14Shop) != site.HtaccessSafe || !strings.Contains(vhost(), "AllowOverrideList") {
		t.Fatalf("về an-toan: mã %d\n%s%s", code, out, errs)
	}
	code, out, _ = c.run("site", "info", m14Shop, "--json")
	var info core.SiteInfo
	if err := json.Unmarshal([]byte(out), &info); err != nil || code != 0 {
		t.Fatalf("site info --json: mã %d, %v", code, err)
	}
	for _, r := range info.Checks {
		if r.Status == checks.Fail {
			t.Errorf("sau khi đổi chế độ, phép kiểm %s lỗi: %s", r.ID, r.Summary.Vi)
		}
	}
	// route explain trên Apache: đọc apache2ctl -S và vhost Apache.
	code, out, _ = c.run("route", "explain", m14Shop, "--evidence")
	if code != 0 || !strings.Contains(out, "[đạt] Vhost nhận tên miền: "+l.ApacheLink+":") || !strings.Contains(out, "$ apache2ctl -S") {
		t.Errorf("route explain trên Apache: mã %d\n%s", code, out)
	}
}

// ---------------------------------------------------------------- self-update

// Sau khi tpan đổi sang bản mới thì nhắc chạy security harden; không đổi bản
// (đã mới nhất) hay quay về bản trước thì không nhắc.
func TestSelfUpdateRemindsHarden(t *testing.T) {
	const remind = "sudo tpan security harden"
	c := newCLI(t)
	c.selfRelease("v9.9.9")
	code, out, errs := c.run("self-update", "--yes", "--url", "https://example.com/rel")
	if code != 0 || !strings.Contains(out, remind) {
		t.Fatalf("cập nhật: mã %d\n%s%s", code, out, errs)
	}
	// --json: stdout chỉ có JSON, lời nhắc ra stderr.
	c = newCLI(t)
	c.selfRelease("v9.9.9")
	code, out, errs = c.run("self-update", "--yes", "--json", "--url", "https://example.com/rel")
	if code != 0 || !json.Valid([]byte(out)) || strings.Contains(out, remind) || !strings.Contains(errs, remind) {
		t.Errorf("--json: mã %d\nstdout %s\nstderr %s", code, out, errs)
	}
	if code, out, _ := c.run("self-update", "--rollback", "--yes"); code != 0 || strings.Contains(out, remind) {
		t.Errorf("quay về bản trước mà nhắc harden: mã %d\n%s", code, out)
	}
	c = newCLI(t)
	c.selfRelease("")
	if _, out, _ := c.run("self-update", "--dry-run", "--url", "https://example.com/rel"); strings.Contains(out, remind) {
		t.Errorf("--dry-run mà nhắc harden:\n%s", out)
	}
}
