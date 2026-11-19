package steps

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"path"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// Mọi tên trong file này mang tiền tố "wp" (hoặc chứa "WP" với tên test) vì
// nhiều file test cùng gói steps được viết song song.

const (
	wpTestDBPass    = "DbPassAbc123DbPassAbc123xyz0"
	wpTestAdminPass = "AdminPassXyz789AdminPassXyz789"
)

var wpTestTarball = []byte("tarball WordPress giả\n")

func wpTestSum(data []byte) string {
	sum := sha1.Sum(data)
	return hex.EncodeToString(sum[:])
}

func wpTestLayout(t *testing.T) site.Layout {
	t.Helper()
	d, err := validate.ParseDomain("example.com")
	if err != nil {
		t.Fatal(err)
	}
	return site.LayoutOf(d)
}

func wpTestEmail(t *testing.T) validate.Email {
	t.Helper()
	e, err := validate.ParseEmail("admin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// wpSim là FakeSystem cộng các handler giả lập curl, tar, find, chmod và wp có
// trạng thái. Không có chúng thì mọi lệnh trả mã 0 với output rỗng và test
// "máy sạch" đạt mà không kiểm gì.
type wpSim struct {
	*system.FakeSystem
	l site.Layout

	mu        sync.Mutex
	installed bool
	permalink string
	adminPass string // mật khẩu quản trị wp core install đọc được từ stdin
	tarball   []byte // nội dung curl "tải" về
}

func wpNewSim(t *testing.T) *wpSim {
	t.Helper()
	l := wpTestLayout(t)
	m := &wpSim{FakeSystem: system.NewFake(), l: l, tarball: wpTestTarball}
	siteOwner := system.Owner{User: l.User, Group: site.WebGroup}
	m.SetDir(wpCacheDir, 0o755, system.Root)
	m.SetDir(l.Dir, fs.ModeSetgid|0o750, siteOwner)
	m.SetDir(l.Public, fs.ModeSetgid|0o750, siteOwner)

	fail := func(code int, msg string) system.Result { return system.Result{ExitCode: code, Stderr: msg} }

	m.Handle([]string{"curl"}, func(c system.Cmd) system.Result {
		// curl của root để file 0600: Apply phải tự đặt 0644.
		m.SetFile(c.Args[2], m.tarball, 0o600, system.Root)
		return system.Result{}
	})
	// tar dưới user của site: file mang chủ user, nhóm www-data (setgid của
	// public), quyền theo archive (0644, 0755) như GNU tar khi không phải root.
	m.Handle([]string{"tar"}, func(c system.Cmd) system.Result {
		if c.AsUser != l.User {
			return fail(2, "tar phải chạy dưới user của site")
		}
		pub := c.Args[3]
		m.SetDir(pub+"/wp-includes", 0o755, siteOwner)
		m.SetDir(pub+"/wp-admin", 0o755, siteOwner)
		m.SetFile(pub+"/index.php", []byte("<?php\n"), 0o644, siteOwner)
		m.SetFile(pub+"/wp-includes/version.php", []byte("<?php $wp_version = '7.1.5';\n"), 0o644, siteOwner)
		return system.Result{}
	})
	m.Handle([]string{"find"}, func(c system.Cmd) system.Result {
		root := c.Args[0]
		if !slices.Equal(c.Args[1:3], []string{"-mindepth", "1"}) {
			return fail(1, "thiếu -mindepth 1")
		}
		var under []string
		for p := range m.Snapshot() {
			if strings.HasPrefix(p, root+"/") {
				under = append(under, p)
			}
		}
		// Sâu trước: xoá file trước thư mục chứa nó.
		sort.Slice(under, func(i, j int) bool { return len(under[i]) > len(under[j]) })
		if slices.Contains(c.Args, "-delete") {
			for _, p := range under {
				if err := m.Remove(p); err != nil {
					return fail(1, err.Error())
				}
			}
			return system.Result{}
		}
		kind, mode := c.Args[4], wpParseMode(c.Args[7])
		snap := m.Snapshot()
		for _, p := range under {
			isDir := snap[p].Mode.IsDir()
			if (kind == "d") == isDir && snap[p].Mode&fs.ModeSymlink == 0 {
				if err := m.Chmod(p, mode); err != nil {
					return fail(1, err.Error())
				}
			}
		}
		return system.Result{}
	})
	m.Handle([]string{"chmod"}, func(c system.Cmd) system.Result {
		if err := m.Chmod(c.Args[2], wpParseMode(c.Args[0])); err != nil {
			return fail(1, err.Error())
		}
		return system.Result{}
	})
	m.Handle([]string{"wp"}, func(c system.Cmd) system.Result {
		if c.AsUser != l.User || !slices.Contains(c.Args, "--path="+l.Public) || slices.Contains(c.Args, "--allow-root") {
			return fail(1, "Error: chạy wp sai user hoặc thiếu --path")
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		sub := strings.Join(c.Args[:2], " ")
		cfg := l.WPConfig
		switch sub {
		case "config create":
			if _, err := m.Stat(cfg); err == nil {
				return fail(1, "Error: The 'wp-config.php' file already exists.")
			}
			pass := strings.TrimSuffix(string(c.Stdin), "\n")
			m.SetFile(cfg, []byte("define( 'DB_PASSWORD', '"+pass+"' );\n"), 0o644, siteOwner)
		case "config set":
			old, err := m.ReadFile(cfg)
			if err != nil {
				return fail(1, "Error: 'wp-config.php' not found.")
			}
			m.SetFile(cfg, append(old, "define( 'DISALLOW_FILE_EDIT', true );\n"...), 0o644, siteOwner)
		case "core is-installed":
			if !m.installed {
				return fail(1, "")
			}
		case "core install":
			m.installed = true
			m.adminPass = strings.TrimSuffix(string(c.Stdin), "\n")
		case "option get":
			return system.Result{Stdout: m.permalink + "\n"}
		case "rewrite structure":
			m.permalink = c.Args[2]
		default:
			return fail(1, "Error: lệnh wp lạ "+sub)
		}
		return system.Result{}
	})
	return m
}

func wpParseMode(s string) fs.FileMode {
	n, err := strconv.ParseUint(s, 8, 32)
	if err != nil {
		panic(err)
	}
	m := fs.FileMode(n & 0o777)
	if n&0o2000 != 0 {
		m |= fs.ModeSetgid
	}
	return m
}

func (m *wpSim) whole() any {
	m.mu.Lock()
	defer m.mu.Unlock()
	return []any{m.Snapshot(), m.installed, m.permalink}
}

func (m *wpSim) failCmd(prefix []string, code int, stderr string) {
	m.Handle(prefix, func(system.Cmd) system.Result { return system.Result{ExitCode: code, Stderr: stderr} })
}

// wpReadOnly: lệnh chỉ hỏi trạng thái, được phép chạy khi engine Check bước đã xong.
func wpReadOnly(c system.Cmd) bool {
	if c.Name != "wp" || len(c.Args) < 2 {
		return false
	}
	sub := c.Args[0] + " " + c.Args[1]
	return sub == "core is-installed" || sub == "option get"
}

func wpExecute(t *testing.T, m *wpSim, s engine.Step) engine.Report {
	t.Helper()
	rep, err := engine.New(m.FakeSystem, engine.Options{}).Execute(context.Background(),
		engine.Plan{Kind: "test", Target: "test", Steps: []engine.Step{s}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return rep
}

// wpLifecycle là bốn trường hợp skill tpan-step đòi (trường hợp lệnh lỗi nằm ở
// các test riêng của từng bước).
type wpLifecycle struct {
	setup  func(m *wpSim)
	step   func(m *wpSim) engine.Step
	noUndo bool
}

func (lc wpLifecycle) run(t *testing.T) {
	ctx := context.Background()
	prep := func(t *testing.T) *wpSim {
		m := wpNewSim(t)
		if lc.setup != nil {
			lc.setup(m)
		}
		return m
	}

	t.Run("máy sạch rồi Apply thì Check true", func(t *testing.T) {
		m := prep(t)
		s := lc.step(m)
		if done, err := s.Check(ctx, m); err != nil || done {
			t.Fatalf("Check trước Apply = %v, %v; muốn false, nil", done, err)
		}
		if err := s.Apply(ctx, m); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if done, err := s.Check(ctx, m); err != nil || !done {
			t.Fatalf("Check sau Apply = %v, %v; muốn true, nil", done, err)
		}
	})

	t.Run("chạy lại thì không lệnh thay đổi nào chạy", func(t *testing.T) {
		m := prep(t)
		if rep := wpExecute(t, m, lc.step(m)); rep.Job.Status != engine.JobSucceeded {
			t.Fatalf("lần đầu: %s %s", rep.Job.Status, rep.Job.Error)
		}
		before := m.whole()
		m.ResetRan()
		rep := wpExecute(t, m, lc.step(m))
		if got := rep.Steps[0].Outcome; got != "skipped" {
			t.Fatalf("lần hai: outcome %q, muốn skipped (%s)", got, rep.Steps[0].Error)
		}
		for _, c := range m.Ran() {
			if !wpReadOnly(c) {
				t.Errorf("lần hai chạy lệnh thay đổi máy: %s", c.String())
			}
		}
		if !reflect.DeepEqual(before, m.whole()) {
			t.Error("lần hai làm đổi máy")
		}
	})

	t.Run("Apply rồi Undo", func(t *testing.T) {
		m := prep(t)
		s := lc.step(m)
		if lc.noUndo {
			if s.Info().NoUndo.IsZero() {
				t.Error("Undo không làm gì nhưng Info().NoUndo trống")
			}
			if err := s.Apply(ctx, m); err != nil {
				t.Fatalf("Apply: %v", err)
			}
			after := m.whole()
			if err := s.Undo(ctx, m); err != nil {
				t.Fatalf("Undo: %v", err)
			}
			if !reflect.DeepEqual(after, m.whole()) {
				t.Error("Undo của bước NoUndo lại đổi máy")
			}
			return
		}
		before := m.whole()
		if err := s.Apply(ctx, m); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if reflect.DeepEqual(before, m.whole()) {
			t.Fatal("Apply không đổi gì, test không kiểm được Undo")
		}
		if err := s.Undo(ctx, m); err != nil {
			t.Fatalf("Undo: %v", err)
		}
		if !reflect.DeepEqual(before, m.whole()) {
			t.Errorf("máy không về như cũ\ntrước: %v\nsau:   %v", before, m.whole())
		}
	})
}

// ---------------------------------------------------------------- WPArchive

func TestWPArchiveLifecycle(t *testing.T) {
	wpLifecycle{
		step: func(*wpSim) engine.Step {
			return &WPArchive{Version: "7.1.5", Hash: strings.ToUpper(wpTestSum(wpTestTarball))}
		},
		noUndo: true,
	}.run(t)

	m := wpNewSim(t)
	s := &WPArchive{Version: "7.1.5", Hash: wpTestSum(wpTestTarball)}
	if err := s.Apply(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	fi, err := m.Stat(s.Path())
	if err != nil || fi.Mode.Perm() != 0o644 || normOwner(fi.Owner) != system.Root {
		t.Errorf("file tải về: %+v %v; muốn 0644 root", fi, err)
	}
	if s.Path() != "/var/cache/tpan/wordpress-7.1.5.tar.gz" {
		t.Errorf("Path() = %s", s.Path())
	}
	want := "curl -fsSL -o /var/cache/tpan/wordpress-7.1.5.tar.gz https://wordpress.org/wordpress-7.1.5.tar.gz"
	if got := ranStrings(m.Ran()); !slices.Equal(got, []string{want}) {
		t.Errorf("lệnh = %v", got)
	}
}

func TestWPArchiveErrors(t *testing.T) {
	ctx := context.Background()
	t.Run("tải lỗi", func(t *testing.T) {
		m := wpNewSim(t)
		before := m.whole()
		m.failCmd([]string{"curl"}, 22, "curl: (22) The requested URL returned error: 404")
		wantExit(t, (&WPArchive{Version: "9.9", Hash: wpTestSum(wpTestTarball)}).Apply(ctx, m), "tải WordPress 9.9")
		if !reflect.DeepEqual(before, m.whole()) {
			t.Error("máy không về như cũ")
		}
	})
	t.Run("SHA-1 không khớp thì bỏ file tải về", func(t *testing.T) {
		m := wpNewSim(t)
		before := m.whole()
		err := (&WPArchive{Version: DefaultWordPressVersion, Hash: DefaultWordPressSHA1}).Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "SHA-1 không khớp") {
			t.Fatalf("Apply = %v", err)
		}
		if !reflect.DeepEqual(before, m.whole()) {
			t.Error("file tải về chưa bị xoá")
		}
	})
	t.Run("file cũ hỏng thì tải lại, hỏng nữa thì trả file cũ", func(t *testing.T) {
		m := wpNewSim(t)
		s := &WPArchive{Version: "7.1.5", Hash: wpTestSum(wpTestTarball)}
		m.SetFile(s.Path(), []byte("cắt dở"), 0o644, system.Root)
		m.tarball = []byte("vẫn hỏng")
		before := m.whole()
		if err := s.Apply(ctx, m); err == nil {
			t.Fatal("Apply không lỗi")
		}
		if !reflect.DeepEqual(before, m.whole()) {
			t.Error("file cũ không được trả lại")
		}
	})
	t.Run("dùng lại file đã tải, chỉ sửa quyền", func(t *testing.T) {
		m := wpNewSim(t)
		s := &WPArchive{Version: "7.1.5", Hash: wpTestSum(wpTestTarball)}
		m.SetFile(s.Path(), wpTestTarball, 0o600, system.Root)
		if err := s.Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
		if len(m.Ran()) != 0 {
			t.Errorf("vẫn tải lại: %v", ranStrings(m.Ran()))
		}
		if done, err := s.Check(ctx, m); err != nil || !done {
			t.Errorf("Check = %v, %v", done, err)
		}
	})
	t.Run("chmod lỗi thì báo có ngữ cảnh và trả file về", func(t *testing.T) {
		m := wpNewSim(t)
		s := &WPArchive{Version: "7.1.5", Hash: wpTestSum(wpTestTarball)}
		before := m.whole()
		m.Fail("chmod", s.Path(), errors.New("hết quyền"))
		err := s.Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "đổi quyền "+s.Path()) {
			t.Fatalf("Apply = %v", err)
		}
		if !reflect.DeepEqual(before, m.whole()) {
			t.Error("máy không về như cũ")
		}
	})
}

func TestWPDefaultConstants(t *testing.T) {
	s := &WPArchive{Version: DefaultWordPressVersion, Hash: DefaultWordPressSHA1}
	if err := s.validate(); err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"7.1", "7.1.5", "10.0.12"} {
		if err := (&WPArchive{Version: v, Hash: DefaultWordPressSHA1}).validate(); err != nil {
			t.Errorf("bản %s bị từ chối: %v", v, err)
		}
	}
}

// ------------------------------------------------------------------ WPFiles

func wpArchivePath() string { return (&WPArchive{Version: "7.1.5"}).Path() }

func TestWPFilesLifecycle(t *testing.T) {
	wpLifecycle{
		step: func(m *wpSim) engine.Step { return &WPFiles{Layout: m.l, Archive: wpArchivePath()} },
	}.run(t)

	// Quyền sau khi giải nén: thư mục 2750 (còn setgid), file 0640, l.Public
	// không bị đụng. Chỉ chmod thư mục chạy bằng root.
	m := wpNewSim(t)
	l := m.l
	if err := (&WPFiles{Layout: l, Archive: wpArchivePath()}).Apply(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	snap := m.Snapshot()
	for p, e := range snap {
		if !strings.HasPrefix(p, l.Public+"/") {
			continue
		}
		want := fs.FileMode(0o640)
		if e.Mode.IsDir() {
			want = fs.ModeDir | fs.ModeSetgid | 0o750
		}
		if e.Mode != want {
			t.Errorf("%s: %v, muốn %v", p, e.Mode, want)
		}
	}
	if e := snap[l.Public]; e.Mode != fs.ModeDir|fs.ModeSetgid|0o750 {
		t.Errorf("l.Public bị đổi: %v", e.Mode)
	}
	for _, c := range m.Ran() {
		root := c.Name == "find" && slices.Contains(c.Args, "d")
		if (c.AsUser == "") != root {
			t.Errorf("lệnh %s chạy sai user", c.String())
		}
	}
}

func TestWPFilesErrors(t *testing.T) {
	ctx := context.Background()
	t.Run("tar lỗi thì dọn phần đã giải nén", func(t *testing.T) {
		m := wpNewSim(t)
		before := m.whole()
		m.Handle([]string{"tar"}, func(c system.Cmd) system.Result {
			m.SetFile(m.l.Public+"/index.php", nil, 0o644, system.Owner{User: m.l.User, Group: site.WebGroup})
			return system.Result{ExitCode: 2, Stderr: "tar: Unexpected EOF in archive"}
		})
		wantExit(t, (&WPFiles{Layout: m.l, Archive: wpArchivePath()}).Apply(ctx, m), "giải nén WordPress vào "+m.l.Public)
		if !reflect.DeepEqual(before, m.whole()) {
			t.Error("phần giải nén dở chưa được dọn")
		}
	})
	t.Run("chmod lỗi thì dọn", func(t *testing.T) {
		m := wpNewSim(t)
		before := m.whole()
		m.failCmd([]string{"find", m.l.Public, "-mindepth", "1", "-type", "f"}, 1, "find: lỗi")
		wantExit(t, (&WPFiles{Layout: m.l, Archive: wpArchivePath()}).Apply(ctx, m), "đặt quyền 0640")
		if !reflect.DeepEqual(before, m.whole()) {
			t.Error("chưa dọn sau lỗi")
		}
	})
	t.Run("public đã có file thì không đụng", func(t *testing.T) {
		m := wpNewSim(t)
		m.SetFile(m.l.Public+"/index.html", []byte("của người dùng"), 0o644, system.Root)
		before := m.whole()
		err := (&WPFiles{Layout: m.l, Archive: wpArchivePath()}).Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "không ghi đè") {
			t.Fatalf("Apply = %v", err)
		}
		if len(m.Ran()) != 0 || !reflect.DeepEqual(before, m.whole()) {
			t.Errorf("vẫn chạy lệnh: %v", ranStrings(m.Ran()))
		}
	})
	t.Run("Undo lỗi có ngữ cảnh", func(t *testing.T) {
		m := wpNewSim(t)
		s := &WPFiles{Layout: m.l, Archive: wpArchivePath()}
		if err := s.Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
		m.failCmd([]string{"find", m.l.Public, "-mindepth", "1", "-delete"}, 1, "find: cannot delete")
		wantExit(t, s.Undo(ctx, m), "xoá mã nguồn trong "+m.l.Public)
	})
}

// ----------------------------------------------------------------- WPConfig

func TestWPConfigLifecycle(t *testing.T) {
	wpLifecycle{
		step: func(m *wpSim) engine.Step {
			return &WPConfig{Layout: m.l, DBPassword: system.NewSecret(wpTestDBPass)}
		},
	}.run(t)

	ctx := context.Background()
	m := wpNewSim(t)
	l := m.l
	if err := (&WPConfig{Layout: l, DBPassword: system.NewSecret(wpTestDBPass)}).Apply(ctx, m); err != nil {
		t.Fatal(err)
	}
	data, _ := m.ReadFile(l.WPConfig)
	if !strings.Contains(string(data), wpTestDBPass) || !strings.Contains(string(data), "DISALLOW_FILE_EDIT") {
		t.Errorf("wp-config.php = %q", data)
	}
	ran := m.Ran()
	if len(ran) != 3 {
		t.Fatalf("lệnh = %v", ranStrings(ran))
	}
	create := ran[0]
	if string(create.Stdin) != wpTestDBPass+"\n" {
		t.Errorf("stdin của config create = %q", create.Stdin)
	}
	for _, a := range []string{"--prompt=dbpass", "--quiet", "--dbname=" + l.DB, "--dbuser=" + l.DB, "--dbcharset=utf8mb4"} {
		if !slices.Contains(create.Args, a) {
			t.Errorf("config create thiếu %s: %s", a, create.String())
		}
	}
	if got := ranStrings(ran[1:]); !slices.Equal(got, []string{
		"runuser -u " + l.User + " -- wp config set DISALLOW_FILE_EDIT true --raw --type=constant --quiet --path=" + l.Public,
		"runuser -u " + l.User + " -- chmod 0600 -- " + l.WPConfig,
	}) {
		t.Errorf("lệnh = %v", got)
	}
}

func TestWPConfigExisting(t *testing.T) {
	ctx := context.Background()
	t.Run("file của user site sai quyền thì chỉ sửa quyền, Undo trả quyền cũ", func(t *testing.T) {
		m := wpNewSim(t)
		owner := system.Owner{User: m.l.User, Group: site.WebGroup}
		m.SetFile(m.l.WPConfig, []byte("mật khẩu cũ"), 0o644, owner)
		before := m.whole()
		s := &WPConfig{Layout: m.l, DBPassword: system.NewSecret(wpTestDBPass)}
		if err := s.Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
		if got := ranStrings(m.Ran()); len(got) != 1 || !strings.Contains(got[0], "chmod 0600") {
			t.Errorf("lệnh = %v; muốn chỉ chmod", got)
		}
		if done, err := s.Check(ctx, m); err != nil || !done {
			t.Errorf("Check = %v, %v", done, err)
		}
		if err := s.Undo(ctx, m); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, m.whole()) {
			t.Error("Undo không trả lại như cũ (hoặc đã xoá file có sẵn)")
		}
	})
	t.Run("file của root thì từ chối", func(t *testing.T) {
		m := wpNewSim(t)
		m.SetFile(m.l.WPConfig, []byte("x"), 0o644, system.Root)
		err := (&WPConfig{Layout: m.l, DBPassword: system.NewSecret(wpTestDBPass)}).Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "không ghi đè") || len(m.Ran()) != 0 {
			t.Errorf("Apply = %v, lệnh %v", err, ranStrings(m.Ran()))
		}
	})
	t.Run("symlink thì từ chối", func(t *testing.T) {
		m := wpNewSim(t)
		m.SetSymlink("/etc/shadow", m.l.WPConfig)
		err := (&WPConfig{Layout: m.l, DBPassword: system.NewSecret(wpTestDBPass)}).Apply(ctx, m)
		if err == nil || len(m.Ran()) != 0 {
			t.Errorf("Apply = %v, lệnh %v", err, ranStrings(m.Ran()))
		}
	})
}

func TestWPConfigErrors(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, ctx string
		prefix    []string
	}{
		{"config create lỗi", "tạo ", []string{"wp", "config", "create"}},
		{"config set lỗi", "thêm DISALLOW_FILE_EDIT", []string{"wp", "config", "set"}},
		{"chmod lỗi", "đặt quyền 600", []string{"chmod"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := wpNewSim(t)
			before := m.whole()
			// Giả lập WP-CLI in lại mật khẩu ra stderr và stdout.
			m.Handle(tc.prefix, func(c system.Cmd) system.Result {
				return system.Result{ExitCode: 1, Stdout: "wp config create --dbpass=" + wpTestDBPass, Stderr: "Error: " + wpTestDBPass}
			})
			// Lỗi ở bước sau thì file đã có: dựng sẵn để xem Apply có xoá không.
			if tc.prefix[0] != "wp" || tc.prefix[2] != "create" {
				m.Handle([]string{"wp", "config", "create"}, func(c system.Cmd) system.Result {
					m.SetFile(m.l.WPConfig, []byte("x"), 0o644, system.Owner{User: m.l.User, Group: site.WebGroup})
					return system.Result{}
				})
			}
			err := (&WPConfig{Layout: m.l, DBPassword: system.NewSecret(wpTestDBPass)}).Apply(ctx, m)
			if err == nil || !strings.Contains(err.Error(), tc.ctx) {
				t.Fatalf("Apply = %v; muốn lỗi có %q", err, tc.ctx)
			}
			if strings.Contains(err.Error(), wpTestDBPass) {
				t.Errorf("mật khẩu DB lọt vào lỗi: %v", err)
			}
			if !reflect.DeepEqual(before, m.whole()) {
				t.Error("file dở chưa bị xoá")
			}
		})
	}
}

// ---------------------------------------------------------------- WPInstall

func wpInstallStep(t *testing.T, l site.Layout) *WPInstall {
	return &WPInstall{
		Layout:        l,
		URL:           "http://" + l.Domain.String(),
		Title:         "Cửa hàng 'của tôi' $(id)",
		AdminUser:     "admin_1",
		AdminEmail:    wpTestEmail(t),
		AdminPassword: system.NewSecret(wpTestAdminPass),
	}
}

func TestWPInstallLifecycle(t *testing.T) {
	wpLifecycle{
		step:   func(m *wpSim) engine.Step { return wpInstallStep(t, m.l) },
		noUndo: true,
	}.run(t)

	ctx := context.Background()
	m := wpNewSim(t)
	l := m.l
	if err := wpInstallStep(t, l).Apply(ctx, m); err != nil {
		t.Fatal(err)
	}
	if m.adminPass != wpTestAdminPass || m.permalink != "/%postname%/" {
		t.Errorf("mật khẩu nhận được %q, permalink %q", m.adminPass, m.permalink)
	}
	var install system.Cmd
	for _, c := range m.Ran() {
		if len(c.Args) > 1 && c.Args[1] == "install" {
			install = c
		}
	}
	for _, a := range []string{"--prompt=admin_password", "--quiet", "--skip-email", "--title=Cửa hàng 'của tôi' $(id)", "--admin_user=admin_1", "--admin_email=admin@example.com", "--url=http://example.com"} {
		if !slices.Contains(install.Args, a) {
			t.Errorf("core install thiếu %s: %s", a, install.String())
		}
	}
	if string(install.Stdin) != wpTestAdminPass+"\n" {
		t.Errorf("stdin = %q", install.Stdin)
	}
}

func TestWPInstallPartial(t *testing.T) {
	// Đã cài mà permalink sai (lần trước hỏng ở bước đặt permalink): chỉ đặt lại permalink.
	m := wpNewSim(t)
	m.installed = true
	if err := wpInstallStep(t, m.l).Apply(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	for _, c := range m.Ran() {
		if c.Args[1] == "install" {
			t.Errorf("cài lại WordPress đã cài: %s", c.String())
		}
	}
	if m.permalink != wpPermalink {
		t.Errorf("permalink = %q", m.permalink)
	}
}

func TestWPInstallErrors(t *testing.T) {
	ctx := context.Background()
	t.Run("core install lỗi, mật khẩu bị che", func(t *testing.T) {
		m := wpNewSim(t)
		m.failCmd([]string{"wp", "core", "install"}, 1, "Error: Installation failed ("+wpTestAdminPass+").")
		err := wpInstallStep(t, m.l).Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "cài WordPress cho example.com") {
			t.Fatalf("Apply = %v", err)
		}
		if strings.Contains(err.Error(), wpTestAdminPass) {
			t.Errorf("mật khẩu quản trị lọt vào lỗi: %v", err)
		}
	})
	t.Run("is-installed lỗi PHP là lỗi thật", func(t *testing.T) {
		m := wpNewSim(t)
		m.failCmd([]string{"wp", "core", "is-installed"}, 255, "PHP Fatal error")
		_, err := wpInstallStep(t, m.l).Check(ctx, m)
		wantExit(t, err, "kiểm WordPress đã cài chưa")
	})
	t.Run("rewrite lỗi", func(t *testing.T) {
		m := wpNewSim(t)
		m.failCmd([]string{"wp", "rewrite"}, 1, "Error: x")
		wantExit(t, wpInstallStep(t, m.l).Apply(ctx, m), "đặt permalink")
	})
}

// ------------------------------------------------- tham số xấu và bí mật

func TestWPBadParams(t *testing.T) {
	ctx := context.Background()
	l := wpTestLayout(t)
	email := wpTestEmail(t)
	pass := system.NewSecret(wpTestAdminPass)
	inst := func(f func(s *WPInstall)) engine.Step {
		s := wpInstallStep(t, l)
		f(s)
		return s
	}
	badLayout := l
	badLayout.Public = "/var/www/example.com/../../etc"
	badUser := l
	badUser.User = "root; rm -rf /"

	cases := map[string]engine.Step{
		"archive: bản có lệnh":         &WPArchive{Version: "7.1.5; rm -rf /", Hash: DefaultWordPressSHA1},
		"archive: bản đi ngược":        &WPArchive{Version: "../../etc/passwd", Hash: DefaultWordPressSHA1},
		"archive: hash ngắn":           &WPArchive{Version: "7.1.5", Hash: "abc"},
		"archive: hash rỗng":           &WPArchive{Version: "7.1.5"},
		"files: Layout zero":           &WPFiles{Archive: wpArchivePath()},
		"files: archive ngoài cache":   &WPFiles{Layout: l, Archive: "/etc/passwd"},
		"files: archive đi ngược":      &WPFiles{Layout: l, Archive: wpCacheDir + "/wordpress-../../etc/x.tar.gz"},
		"files: public đi ngược":       &WPFiles{Layout: badLayout, Archive: wpArchivePath()},
		"files: user lạ":               &WPFiles{Layout: badUser, Archive: wpArchivePath()},
		"config: Layout zero":          &WPConfig{DBPassword: pass},
		"config: mật khẩu rỗng":        &WPConfig{Layout: l},
		"config: mật khẩu xuống dòng":  &WPConfig{Layout: l, DBPassword: system.NewSecret("abc\n--dbhost=evil")},
		"config: mật khẩu có khoảng":   &WPConfig{Layout: l, DBPassword: system.NewSecret("a b")},
		"config: mật khẩu quá dài":     &WPConfig{Layout: l, DBPassword: system.NewSecret(strings.Repeat("a", 1024))},
		"install: Layout zero":         &WPInstall{URL: "http://example.com", Title: "x", AdminUser: "admin", AdminEmail: email, AdminPassword: pass},
		"install: URL lạ":              inst(func(s *WPInstall) { s.URL = "http://evil.example" }),
		"install: URL có đường dẫn":    inst(func(s *WPInstall) { s.URL = "http://example.com/../x" }),
		"install: tiêu đề rỗng":        inst(func(s *WPInstall) { s.Title = "" }),
		"install: tiêu đề xuống dòng":  inst(func(s *WPInstall) { s.Title = "a\nb" }),
		"install: tiêu đề 300 byte":    inst(func(s *WPInstall) { s.Title = strings.Repeat("a", 300) }),
		"install: tiêu đề C1":          inst(func(s *WPInstall) { s.Title = "a\u0085b" }),
		"install: tiêu đề không UTF-8": inst(func(s *WPInstall) { s.Title = "a\xffb" }),
		"install: user có lệnh":        inst(func(s *WPInstall) { s.AdminUser = "a; rm -rf /" }),
		"install: user $(id)":          inst(func(s *WPInstall) { s.AdminUser = "$(id)" }),
		"install: user đi ngược":       inst(func(s *WPInstall) { s.AdminUser = "../../etc/passwd" }),
		"install: user là cờ":          inst(func(s *WPInstall) { s.AdminUser = "--allow-root" }),
		"install: user ngắn":           inst(func(s *WPInstall) { s.AdminUser = "ab" }),
		"install: user hoa":            inst(func(s *WPInstall) { s.AdminUser = "Admin" }),
		"install: user có dấu":         inst(func(s *WPInstall) { s.AdminUser = "quảntrị" }),
		"install: user dài":            inst(func(s *WPInstall) { s.AdminUser = strings.Repeat("a", 61) }),
		"install: email rỗng":          inst(func(s *WPInstall) { s.AdminEmail = validate.Email{} }),
		"install: mật khẩu rỗng":       inst(func(s *WPInstall) { s.AdminPassword = system.Secret{} }),
		"install: mật khẩu xuống dòng": inst(func(s *WPInstall) { s.AdminPassword = system.NewSecret("x\ny") }),
	}
	for name, s := range cases {
		t.Run("WP "+name, func(t *testing.T) {
			m := wpNewSim(t)
			if _, err := s.Check(ctx, m); err == nil {
				t.Error("Check không lỗi")
			}
			if err := s.Apply(ctx, m); err == nil {
				t.Error("Apply không lỗi")
			}
			if len(m.Ran()) != 0 {
				t.Errorf("vẫn chạy lệnh: %v", ranStrings(m.Ran()))
			}
		})
	}
}

func TestWPUndoWithoutApply(t *testing.T) {
	ctx := context.Background()
	l := wpTestLayout(t)
	for _, s := range []engine.Step{
		&WPFiles{Layout: l, Archive: wpArchivePath()},
		&WPConfig{Layout: l, DBPassword: system.NewSecret(wpTestDBPass)},
	} {
		m := wpNewSim(t)
		if err := s.Undo(ctx, m); !errors.Is(err, errNotApplied) || len(m.Ran()) != 0 {
			t.Errorf("%s: Undo = %v, lệnh %v", s.Info().ID, err, ranStrings(m.Ran()))
		}
	}
}

// Mật khẩu không bao giờ nằm trong Info (thứ hiện ra giao diện và --dry-run),
// trong Cmd.String() của lệnh đã chạy, hay trong argv.
func TestWPSecretsNotLeaked(t *testing.T) {
	ctx := context.Background()
	m := wpNewSim(t)
	l := m.l
	cfg := &WPConfig{Layout: l, DBPassword: system.NewSecret(wpTestDBPass)}
	inst := wpInstallStep(t, l)
	for _, s := range []engine.Step{cfg, inst} {
		info, err := json.Marshal(s.Info())
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(info), wpTestDBPass) || strings.Contains(string(info), wpTestAdminPass) {
			t.Errorf("%s: mật khẩu nằm trong Info: %s", s.Info().ID, info)
		}
		if err := s.Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	if len(cfg.Info().Files) != 0 {
		t.Error("wp-config.php nằm trong Info().Files: Preview sẽ đọc mật khẩu DB ra giao diện")
	}
	for _, c := range m.Ran() {
		all := c.String() + strings.Join(c.Args, " ")
		if strings.Contains(all, wpTestDBPass) || strings.Contains(all, wpTestAdminPass) {
			t.Errorf("mật khẩu nằm trên dòng lệnh: %s", c.String())
		}
		if c.Name == "wp" && c.AsUser != l.User {
			t.Errorf("wp chạy bằng root: %s", c.String())
		}
	}
}

func TestWPInfo(t *testing.T) {
	l := wpTestLayout(t)
	for _, s := range []engine.Step{
		&WPArchive{Version: DefaultWordPressVersion, Hash: DefaultWordPressSHA1},
		&WPFiles{Layout: l, Archive: wpArchivePath()},
		&WPConfig{Layout: l, DBPassword: system.NewSecret(wpTestDBPass)},
		wpInstallStep(t, l),
	} {
		info := s.Info()
		if info.ID == "" || info.Title.Vi == "" || info.Title.En == "" || info.Why.Vi == "" || info.Why.En == "" || len(info.Commands) == 0 {
			t.Errorf("Info thiếu: %+v", info)
		}
		if strings.Contains(info.ID, " ") || path.Base(info.ID) == "" {
			t.Errorf("ID lạ: %q", info.ID)
		}
	}
}
