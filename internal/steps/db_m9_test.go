package steps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// Mọi tên trong file này mang tiền tố "m9db" (hoặc "DBPassword", "WPConstant"
// với tên test) vì nhiều file test cùng gói steps được viết song song.

const (
	m9dbOldPass = "OldPw0123456789abcdefABCDEFghijk" // 32 ký tự, dạng tpan sinh
	m9dbNewPass = "NewPwZYXWVUTSRQponmlk9876543210x" // 32 ký tự
)

// m9dbSim là FakeSystem cộng MariaDB giả (chỉ ALTER USER) và wp giả sửa
// wp-config.php theo từng dòng define, để Undo so được cả nội dung file.
type m9dbSim struct {
	*system.FakeSystem
	l site.Layout

	mu    sync.Mutex
	users map[string]string // user DB → mật khẩu
	// failAlter: ALTER USER với mật khẩu này thì lỗi; stderr chép lại câu SQL
	// (như lỗi cú pháp của MariaDB) để test thấy mật khẩu có bị lọc không.
	failAlter map[string]bool
	// failWP: lệnh wp có hai tham số đầu (vd "config set") thì lỗi; stderr
	// chép lại stdin để test thấy mật khẩu có bị lọc không.
	failWP map[string]int
}

var (
	m9dbAlterRe  = regexp.MustCompile(`^ALTER USER '([a-z0-9_]+)'@'localhost' IDENTIFIED BY '([A-Za-z0-9]+)';$`)
	m9dbDefineRe = regexp.MustCompile(`^define\( '([A-Z_]+)', (.*) \);$`)
)

func m9dbLayout(t *testing.T) site.Layout {
	t.Helper()
	d, err := validate.ParseDomain("m9a.example.com")
	if err != nil {
		t.Fatal(err)
	}
	return site.LayoutOf(d)
}

func m9dbConfig(lines ...string) []byte {
	return []byte("<?php\n" + strings.Join(lines, "\n") + "\n/* That's all, stop editing! */\n")
}

// m9dbSiteConfig: wp-config.php như `wp config create --dbname=db --dbuser=db` ghi.
func m9dbSiteConfig(db string, lines ...string) []byte {
	return m9dbConfig(append([]string{"define( 'DB_NAME', '" + db + "' );", "define( 'DB_USER', '" + db + "' );"}, lines...)...)
}

func m9dbNewSim(t *testing.T) *m9dbSim {
	t.Helper()
	l := m9dbLayout(t)
	m := &m9dbSim{
		FakeSystem: system.NewFake(), l: l,
		users:     map[string]string{l.DB: m9dbOldPass},
		failAlter: map[string]bool{},
		failWP:    map[string]int{},
	}
	owner := system.Owner{User: l.User, Group: site.WebGroup}
	m.SetDir(l.Public, 0o2750, owner)
	m.SetFile(l.WPConfig, m9dbSiteConfig(l.DB, "define( 'DB_PASSWORD', '"+m9dbOldPass+"' );"), 0o600, owner)

	fail := func(code int, msg string) system.Result { return system.Result{ExitCode: code, Stderr: msg} }
	m.Handle([]string{"mariadb", "-N", "-B"}, func(c system.Cmd) system.Result {
		m.mu.Lock()
		defer m.mu.Unlock()
		st := strings.TrimSpace(string(c.Stdin))
		a := m9dbAlterRe.FindStringSubmatch(st)
		if a == nil {
			return fail(1, "m9dbSim: câu SQL lạ: "+st)
		}
		if m.failAlter[a[2]] {
			return fail(1, "ERROR 1064 (42000) at line 1: syntax error near '"+st+"'")
		}
		if _, ok := m.users[a[1]]; !ok {
			return fail(1, "ERROR 1396 (HY000): Operation ALTER USER failed for '"+a[1]+"'@'localhost'")
		}
		m.users[a[1]] = a[2]
		return system.Result{}
	})
	m.Handle([]string{"wp"}, func(c system.Cmd) system.Result {
		if c.AsUser != l.User || !slices.Contains(c.Args, "--path="+l.Public) || slices.Contains(c.Args, "--allow-root") {
			return fail(1, "Error: chạy wp sai user hoặc thiếu --path")
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		sub := strings.Join(c.Args[:2], " ")
		if code := m.failWP[sub]; code != 0 {
			return fail(code, "PHP Fatal error: stdin was "+string(c.Stdin))
		}
		data, err := m.ReadFile(l.WPConfig)
		if err != nil {
			return fail(1, "Error: 'wp-config.php' not found.")
		}
		name := c.Args[2]
		lines := strings.Split(string(data), "\n")
		at := slices.IndexFunc(lines, func(s string) bool {
			d := m9dbDefineRe.FindStringSubmatch(s)
			return d != nil && d[1] == name
		})
		notDefined := fail(1, "Error: The constant '"+name+"' is not defined in the 'wp-config.php' file.")
		switch sub {
		case "config get":
			if at < 0 {
				return notDefined
			}
			v := m9dbDefineRe.FindStringSubmatch(lines[at])[2]
			if !slices.Contains(c.Args, "--format=json") {
				return fail(1, "m9dbSim: Check phải dùng --format=json")
			}
			if strings.HasPrefix(v, "'") {
				b, _ := json.Marshal(strings.Trim(v, "'"))
				v = string(b)
			}
			return system.Result{Stdout: v + "\n"}
		case "config set":
			var value string
			switch {
			case slices.Contains(c.Args, "--prompt"):
				// WP-CLI 2.12.0: --prompt hỏi <value> trước; hết stdin thì dừng hỏi.
				in := string(c.Stdin)
				if strings.Count(in, "\n") != 1 || !strings.HasSuffix(in, "\n") {
					return fail(1, "m9dbSim: stdin phải đúng một dòng")
				}
				value = strings.TrimSuffix(in, "\n")
			case slices.ContainsFunc(c.Args, func(a string) bool { return strings.HasPrefix(a, "--prompt=") }):
				// Đã thấy trên máy ảo: --prompt=value báo usage, thoát 1.
				return fail(1, "usage: wp config set <name> <value> [--add] [--raw] ...")
			default:
				value = c.Args[3]
			}
			if !slices.Contains(c.Args, "--raw") {
				value = "'" + value + "'"
			}
			line := "define( '" + name + "', " + value + " );"
			if at >= 0 {
				lines[at] = line
			} else {
				end := slices.IndexFunc(lines, func(s string) bool { return strings.HasPrefix(s, "/* That's all") })
				lines = slices.Insert(lines, end, line)
			}
		case "config delete":
			if at < 0 {
				return notDefined
			}
			lines = slices.Delete(lines, at, at+1)
		default:
			return fail(1, "Error: lệnh wp lạ "+sub)
		}
		fi, _ := m.Stat(l.WPConfig)
		m.SetFile(l.WPConfig, []byte(strings.Join(lines, "\n")), fi.Mode.Perm(), fi.Owner)
		return system.Result{}
	})
	return m
}

func (m *m9dbSim) whole() any {
	m.mu.Lock()
	defer m.mu.Unlock()
	return []any{m.Snapshot(), maps.Clone(m.users)}
}

func (m *m9dbSim) set(fn func(m *m9dbSim)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fn(m)
}

func (m *m9dbSim) pass() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.users[m.l.DB]
}

func (m *m9dbSim) config(t *testing.T) string {
	t.Helper()
	b, err := m.ReadFile(m.l.WPConfig)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func m9dbExecute(t *testing.T, m *m9dbSim, s engine.Step) engine.Report {
	t.Helper()
	rep, err := engine.New(m.FakeSystem, engine.Options{}).Execute(context.Background(),
		engine.Plan{Kind: "test", Target: "test", Steps: []engine.Step{s}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return rep
}

func m9dbPassStep(m *m9dbSim) *DBPassword {
	return &DBPassword{Layout: m.l, Password: system.NewSecret(m9dbNewPass)}
}

// m9dbNoSecret: không lệnh nào mang mật khẩu trên argv hay Cmd.String(); lỗi
// cũng không.
func m9dbNoSecret(t *testing.T, m *m9dbSim, err error) {
	t.Helper()
	for _, c := range m.Ran() {
		for _, pw := range []string{m9dbOldPass, m9dbNewPass} {
			if strings.Contains(c.String(), pw) || slices.ContainsFunc(c.Args, func(a string) bool { return strings.Contains(a, pw) }) {
				t.Errorf("mật khẩu nằm trên dòng lệnh: %s", c.String())
			}
		}
	}
	if err != nil {
		for _, pw := range []string{m9dbOldPass, m9dbNewPass} {
			if strings.Contains(err.Error(), pw) {
				t.Errorf("mật khẩu lọt vào lỗi: %v", err)
			}
		}
	}
}

// ---------------------------------------------------------------- DBPassword

func TestDBPasswordApply(t *testing.T) {
	ctx := context.Background()
	m := m9dbNewSim(t)
	s := m9dbPassStep(m)
	if done, err := s.Check(ctx, m); err != nil || done {
		t.Fatalf("Check trước Apply = %v, %v; muốn false, nil", done, err)
	}
	if err := s.Apply(ctx, m); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if done, err := s.Check(ctx, m); err != nil || !done {
		t.Fatalf("Check sau Apply = %v, %v; muốn true, nil", done, err)
	}
	if got := m.pass(); got != m9dbNewPass {
		t.Errorf("mật khẩu trong MariaDB chưa đổi")
	}
	if cfg := m.config(t); !strings.Contains(cfg, "define( 'DB_PASSWORD', '"+m9dbNewPass+"' );") {
		t.Errorf("wp-config.php chưa có mật khẩu mới:\n%s", cfg)
	}

	// Đúng hai lệnh, theo thứ tự; mật khẩu cũ được đọc bằng ReadFile, không có
	// lệnh nào đọc wp-config.php (output của Run vào log job).
	ran := m.Ran()
	if len(ran) != 2 {
		t.Fatalf("chạy %d lệnh, muốn 2: %v", len(ran), ran)
	}
	if ran[0].Name != "mariadb" || string(ran[0].Stdin) != dbAlterUserSQL(m.l.DB, m9dbNewPass) {
		t.Errorf("lệnh 1 = %s (stdin %d byte), muốn ALTER USER qua stdin", ran[0].String(), len(ran[0].Stdin))
	}
	wantArgs := []string{"config", "set", "DB_PASSWORD", "--prompt", "--type=constant", "--quiet", "--path=" + m.l.Public}
	if ran[1].Name != "wp" || ran[1].AsUser != m.l.User || !slices.Equal(ran[1].Args, wantArgs) {
		t.Errorf("lệnh 2 = %s, muốn runuser -u %s -- wp %v", ran[1].String(), m.l.User, wantArgs)
	}
	if string(ran[1].Stdin) != m9dbNewPass+"\n" {
		t.Error("stdin của wp config set phải đúng một dòng: mật khẩu mới")
	}
	m9dbNoSecret(t, m, nil)
}

func TestDBPasswordEngine(t *testing.T) {
	m := m9dbNewSim(t)
	s := m9dbPassStep(m)
	if rep := m9dbExecute(t, m, s); rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("lần đầu: %s %s", rep.Job.Status, rep.Job.Error)
	}
	// Chạy lại đúng bước đã Apply: Check true, không lệnh nào chạy.
	before := m.whole()
	m.ResetRan()
	rep := m9dbExecute(t, m, s)
	if got := rep.Steps[0].Outcome; got != "skipped" {
		t.Fatalf("lần hai: outcome %q, muốn skipped (%s)", got, rep.Steps[0].Error)
	}
	if len(m.Ran()) != 0 {
		t.Errorf("lần hai chạy lệnh: %v", m.Ran())
	}
	if !reflect.DeepEqual(before, m.whole()) {
		t.Error("lần hai làm đổi máy")
	}
}

func TestDBPasswordUndo(t *testing.T) {
	ctx := context.Background()
	m := m9dbNewSim(t)
	s := m9dbPassStep(m)
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
	m9dbNoSecret(t, m, nil)
}

func TestDBPasswordUndoWithoutApply(t *testing.T) {
	m := m9dbNewSim(t)
	if err := m9dbPassStep(m).Undo(context.Background(), m); !errors.Is(err, errNotApplied) {
		t.Errorf("Undo khi chưa Apply = %v, muốn errNotApplied", err)
	}
	if len(m.Ran()) != 0 {
		t.Errorf("Undo khi chưa Apply chạy lệnh: %v", m.Ran())
	}
}

func TestDBPasswordCommandErrors(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name    string
		setup   func(m *m9dbSim)
		want    []string // các đoạn phải có trong lỗi
		wantRan int
	}{
		{
			name:    "ALTER USER lỗi thì không ghi wp-config.php",
			setup:   func(m *m9dbSim) { m.failAlter[m9dbNewPass] = true },
			want:    []string{"đổi mật khẩu user DB"},
			wantRan: 1,
		},
		{
			name:    "ghi wp-config.php lỗi thì đặt lại mật khẩu cũ",
			setup:   func(m *m9dbSim) { m.failWP["config set"] = 255 },
			want:    []string{"ghi mật khẩu DB mới vào"},
			wantRan: 3,
		},
		{
			name: "ghi lỗi và đặt lại cũng lỗi thì báo cả hai",
			setup: func(m *m9dbSim) {
				m.failWP["config set"] = 255
				m.failAlter[m9dbOldPass] = true
			},
			want:    []string{"ghi mật khẩu DB mới vào", "đặt lại mật khẩu cũ"},
			wantRan: 3,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := m9dbNewSim(t)
			cfgBefore := m.config(t)
			m.set(tc.setup)
			s := m9dbPassStep(m)
			err := s.Apply(ctx, m)
			if err == nil {
				t.Fatal("Apply không lỗi")
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("lỗi %q thiếu ngữ cảnh %q", err, w)
				}
			}
			if len(m.Ran()) != tc.wantRan {
				t.Errorf("chạy %d lệnh, muốn %d", len(m.Ran()), tc.wantRan)
			}
			if m.config(t) != cfgBefore {
				t.Error("wp-config.php bị đổi dù Apply lỗi")
			}
			if tc.name != "ghi lỗi và đặt lại cũng lỗi thì báo cả hai" && m.pass() != m9dbOldPass {
				t.Error("MariaDB không còn mật khẩu cũ sau khi Apply lỗi")
			}
			if done, _ := s.Check(ctx, m); done {
				t.Error("Check true sau khi Apply lỗi")
			}
			m9dbNoSecret(t, m, err)
		})
	}
}

func TestDBPasswordUndoWriteFails(t *testing.T) {
	ctx := context.Background()
	m := m9dbNewSim(t)
	s := m9dbPassStep(m)
	if err := s.Apply(ctx, m); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	m.set(func(m *m9dbSim) { m.failWP["config set"] = 255 })
	err := s.Undo(ctx, m)
	if err == nil || !strings.Contains(err.Error(), "ghi lại mật khẩu DB cũ") {
		t.Fatalf("Undo = %v, muốn lỗi ghi lại mật khẩu cũ", err)
	}
	// wp-config.php vẫn mang mật khẩu mới: MariaDB phải về mật khẩu mới cho khớp.
	if m.pass() != m9dbNewPass {
		t.Error("MariaDB lệch với wp-config.php sau khi Undo lỗi")
	}
	m9dbNoSecret(t, m, err)
}

// Mật khẩu cũ không đọc được an toàn thì từ chối trước mọi lệnh.
func TestDBPasswordRefuseOld(t *testing.T) {
	ctx := context.Background()
	define := func(v string) string { return "define( 'DB_PASSWORD', '" + v + "' );" }
	own := func(m *m9dbSim) system.Owner { return system.Owner{User: m.l.User, Group: site.WebGroup} }
	put := func(m *m9dbSim, data []byte) { m.SetFile(m.l.WPConfig, data, 0o600, own(m)) }
	otherDB := site.LayoutOf(func() validate.Domain { d, _ := validate.ParseDomain("b.example.com"); return d }()).DB
	cases := []struct {
		name  string
		setup func(m *m9dbSim)
		want  string // lý do phải có trong lỗi
	}{
		{"không có wp-config.php", func(m *m9dbSim) { _ = m.Remove(m.l.WPConfig) }, "kiểm"},
		{"wp-config.php là symlink", func(m *m9dbSim) {
			_ = m.Remove(m.l.WPConfig)
			m.SetFile("/var/www/b.example.com/public/wp-config.php", m9dbSiteConfig(otherDB, define(m9dbOldPass)), 0o600, system.Owner{User: otherDB, Group: site.WebGroup})
			m.SetSymlink("/var/www/b.example.com/public/wp-config.php", m.l.WPConfig)
		}, "không phải file thường"},
		{"wp-config.php của user khác", func(m *m9dbSim) {
			m.SetFile(m.l.WPConfig, m9dbSiteConfig(m.l.DB, define(m9dbOldPass)), 0o600, system.Root)
		}, "không phải file thường"},
		// Tráo file giữa Stat và ReadFile: byte đọc được là wp-config.php của site khác.
		{"file mang DB_USER của site khác", func(m *m9dbSim) { put(m, m9dbSiteConfig(otherDB, define(m9dbOldPass))) }, "DB_NAME và DB_USER"},
		{"DB_USER khác DB_NAME", func(m *m9dbSim) {
			put(m, m9dbConfig("define( 'DB_NAME', '"+m.l.DB+"' );", "define( 'DB_USER', '"+otherDB+"' );", define(m9dbOldPass)))
		}, "DB_NAME và DB_USER"},
		{"không có dòng DB_USER", func(m *m9dbSim) {
			put(m, m9dbConfig("define( 'DB_NAME', '"+m.l.DB+"' );", define(m9dbOldPass)))
		}, "0 dòng define DB_USER"},
		{"không có dòng DB_PASSWORD", func(m *m9dbSim) { put(m, m9dbSiteConfig(m.l.DB)) }, "0 dòng define DB_PASSWORD"},
		{"hai dòng DB_PASSWORD", func(m *m9dbSim) {
			put(m, m9dbSiteConfig(m.l.DB, "// "+define("Abc0123456789abcdefABCDEFghijklm"), define(m9dbOldPass)))
		}, "2 dòng define DB_PASSWORD"},
		{"mật khẩu không do tpan sinh (ngắn)", func(m *m9dbSim) { put(m, m9dbSiteConfig(m.l.DB, define("hunter2"))) }, "không phải loại tpan tạo"},
		{"mật khẩu không do tpan sinh (ký tự lạ)", func(m *m9dbSim) {
			put(m, m9dbSiteConfig(m.l.DB, define("OldPw0123456789abcdefABCDEFghij;")))
		}, "không phải loại tpan tạo"},
		{"mật khẩu lấy từ biến môi trường", func(m *m9dbSim) {
			put(m, m9dbSiteConfig(m.l.DB, "define( 'DB_PASSWORD', getenv('DB_PASSWORD') );"))
		}, "0 dòng define DB_PASSWORD"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := m9dbNewSim(t)
			tc.setup(m)
			before := m.whole()
			err := m9dbPassStep(m).Apply(ctx, m)
			if err == nil {
				t.Fatal("Apply không từ chối")
			}
			if !strings.Contains(err.Error(), m.l.WPConfig) || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("lỗi %q thiếu tên file hoặc lý do %q", err, tc.want)
			}
			if len(m.Ran()) != 0 {
				t.Errorf("chạy lệnh dù đã từ chối: %v", m.Ran())
			}
			if !reflect.DeepEqual(before, m.whole()) {
				t.Error("máy bị đổi dù đã từ chối")
			}
			m9dbNoSecret(t, m, err)
			if strings.Contains(err.Error(), "hunter2") || strings.Contains(err.Error(), "Abc0123456789") {
				t.Errorf("giá trị đọc từ wp-config.php lọt vào lỗi: %v", err)
			}
		})
	}
}

func TestDBPasswordBadParams(t *testing.T) {
	ctx := context.Background()
	m := m9dbNewSim(t)
	good := m.l
	long := strings.Repeat("a", 300)
	cases := []struct {
		name string
		l    site.Layout
		pw   string
	}{
		{"layout rỗng", site.Layout{}, m9dbNewPass},
		{"tên DB không khớp domain", func() site.Layout { l := good; l.DB = "tp_other"; return l }(), m9dbNewPass},
		{"tên DB có dấu nháy", func() site.Layout { l := good; l.DB = "tp_x';DROP"; return l }(), m9dbNewPass},
		{"wp-config.php ngoài public", func() site.Layout { l := good; l.WPConfig = "/etc/passwd"; return l }(), m9dbNewPass},
		{"public có ..", func() site.Layout { l := good; l.Public = "/var/www/../../etc"; return l }(), m9dbNewPass},
		{"mật khẩu trống", good, ""},
		{"mật khẩu ngắn", good, "Abc123"},
		{"mật khẩu có dấu nháy", good, "Abcdefghijklmnopqrstuvwx'; DROP USER x; --"},
		{"mật khẩu có $(id)", good, "Abcdefghijklmnopqrstuvwx$(id)"},
		{"mật khẩu có ;", good, "a.com; rm -rf /Abcdefghijklmnopqrstuvwx"},
		{"mật khẩu có xuống dòng", good, "Abcdefghijklmnopqrstuvwx\nsecond"},
		{"mật khẩu có ../", good, "../../etc/passwdAbcdefghijklmnopq"},
		{"mật khẩu dài 300 ký tự có dấu", good, long + "é"},
		{"mật khẩu dài quá giới hạn WP-CLI", good, strings.Repeat("A", wpSecretMax+1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m.ResetRan()
			s := &DBPassword{Layout: tc.l, Password: system.NewSecret(tc.pw)}
			if _, err := s.Check(ctx, m); err == nil {
				t.Error("Check không lỗi")
			}
			if err := s.Apply(ctx, m); err == nil {
				t.Error("Apply không lỗi")
			} else if tc.pw != "" && strings.Contains(err.Error(), tc.pw) {
				t.Errorf("mật khẩu lọt vào lỗi: %v", err)
			}
			if len(m.Ran()) != 0 {
				t.Errorf("chạy lệnh với tham số sai: %v", m.Ran())
			}
		})
	}
}

func TestDBPasswordInfo(t *testing.T) {
	m := m9dbNewSim(t)
	s := m9dbPassStep(m)
	info := s.Info()
	if info.ID != "mariadb.db-password:m9a.example.com" || !info.Danger || info.Title.IsZero() || info.Why.IsZero() {
		t.Errorf("Info thiếu: %+v", info)
	}
	if len(info.Commands) != 2 || len(info.Files) != 0 {
		t.Errorf("Info: %d lệnh, %d file; muốn 2 lệnh, 0 file (file sẽ lộ mật khẩu)", len(info.Commands), len(info.Files))
	}
	if err := s.Apply(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	// Sau Apply bước giữ cả mật khẩu cũ: in bước, in Info đều không lộ.
	dump := fmt.Sprintf("%v %+v %#v %v", s, s, s, s.Info())
	for _, pw := range []string{m9dbOldPass, m9dbNewPass} {
		if strings.Contains(dump, pw) {
			t.Errorf("mật khẩu lọt khi in bước: %s", dump)
		}
	}
}

// ---------------------------------------------------------------- WPConstant

func m9dbConstStep(m *m9dbSim) *WPConstant {
	return &WPConstant{Layout: m.l, Name: "DISABLE_WP_CRON", Value: "true"}
}

// m9dbReadOnly: chỉ `wp config get` được chạy khi engine bỏ qua bước đã xong.
func m9dbReadOnly(c system.Cmd) bool {
	return c.Name == "wp" && len(c.Args) >= 2 && c.Args[0] == "config" && c.Args[1] == "get"
}

func TestWPConstantLifecycle(t *testing.T) {
	ctx := context.Background()
	define := func(v string) string { return "define( 'DISABLE_WP_CRON', " + v + " );" }
	owner := func(m *m9dbSim) system.Owner { return system.Owner{User: m.l.User, Group: site.WebGroup} }
	cases := []struct {
		name  string
		setup func(m *m9dbSim)
	}{
		{"chưa có hằng", nil},
		{"đang là false", func(m *m9dbSim) {
			m.SetFile(m.l.WPConfig, m9dbConfig(define("false"), "define( 'X', 1 );"), 0o600, owner(m))
		}},
		{"đang là số", func(m *m9dbSim) { m.SetFile(m.l.WPConfig, m9dbConfig(define("0")), 0o600, owner(m)) }},
		{"đang là chuỗi", func(m *m9dbSim) { m.SetFile(m.l.WPConfig, m9dbConfig(define(`'a"b$c'`)), 0o600, owner(m)) }},
	}
	for _, tc := range cases {
		prep := func(t *testing.T) *m9dbSim {
			m := m9dbNewSim(t)
			if tc.setup != nil {
				tc.setup(m)
			}
			return m
		}
		t.Run(tc.name+": máy sạch rồi Apply thì Check true", func(t *testing.T) {
			m := prep(t)
			s := m9dbConstStep(m)
			if done, err := s.Check(ctx, m); err != nil || done {
				t.Fatalf("Check trước Apply = %v, %v; muốn false, nil", done, err)
			}
			if err := s.Apply(ctx, m); err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if done, err := s.Check(ctx, m); err != nil || !done {
				t.Fatalf("Check sau Apply = %v, %v; muốn true, nil", done, err)
			}
			if !strings.Contains(m.config(t), define("true")) {
				t.Errorf("wp-config.php chưa có hằng:\n%s", m.config(t))
			}
		})
		t.Run(tc.name+": chạy lại thì chỉ có lệnh đọc", func(t *testing.T) {
			m := prep(t)
			if rep := m9dbExecute(t, m, m9dbConstStep(m)); rep.Job.Status != engine.JobSucceeded {
				t.Fatalf("lần đầu: %s %s", rep.Job.Status, rep.Job.Error)
			}
			before := m.whole()
			m.ResetRan()
			rep := m9dbExecute(t, m, m9dbConstStep(m))
			if got := rep.Steps[0].Outcome; got != "skipped" {
				t.Fatalf("lần hai: outcome %q, muốn skipped (%s)", got, rep.Steps[0].Error)
			}
			for _, c := range m.Ran() {
				if !m9dbReadOnly(c) {
					t.Errorf("lần hai chạy lệnh thay đổi máy: %s", c.String())
				}
			}
			if !reflect.DeepEqual(before, m.whole()) {
				t.Error("lần hai làm đổi máy")
			}
		})
		t.Run(tc.name+": Apply rồi Undo", func(t *testing.T) {
			m := prep(t)
			s := m9dbConstStep(m)
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
}

// Xem trước Plan tạo site: wp-config.php chưa có thì Check false, không lỗi.
func TestWPConstantNoConfig(t *testing.T) {
	ctx := context.Background()
	m := m9dbNewSim(t)
	_ = m.Remove(m.l.WPConfig)
	s := m9dbConstStep(m)
	if done, err := s.Check(ctx, m); err != nil || done {
		t.Fatalf("Check khi chưa có wp-config.php = %v, %v; muốn false, nil", done, err)
	}
	if len(m.Ran()) != 0 {
		t.Errorf("Check chạy lệnh khi chưa có wp-config.php: %v", m.Ran())
	}
	if err := s.Apply(ctx, m); err == nil || !strings.Contains(err.Error(), "DISABLE_WP_CRON") {
		t.Errorf("Apply khi chưa có wp-config.php = %v, muốn lỗi có ngữ cảnh", err)
	}
}

func TestWPConstantCommandErrors(t *testing.T) {
	ctx := context.Background()
	t.Run("đọc hằng lỗi thật (PHP lỗi) thì Check lỗi", func(t *testing.T) {
		m := m9dbNewSim(t)
		m.set(func(m *m9dbSim) { m.failWP["config get"] = 255 })
		_, err := m9dbConstStep(m).Check(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "đọc hằng DISABLE_WP_CRON") {
			t.Errorf("Check = %v, muốn lỗi có ngữ cảnh", err)
		}
	})
	t.Run("ghi hằng lỗi thì Apply lỗi", func(t *testing.T) {
		m := m9dbNewSim(t)
		m.set(func(m *m9dbSim) { m.failWP["config set"] = 1 })
		before := m.whole()
		err := m9dbConstStep(m).Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "đặt DISABLE_WP_CRON = true") {
			t.Errorf("Apply = %v, muốn lỗi có ngữ cảnh", err)
		}
		if !reflect.DeepEqual(before, m.whole()) {
			t.Error("máy bị đổi dù Apply lỗi")
		}
	})
	t.Run("xoá hằng lỗi thì Undo lỗi", func(t *testing.T) {
		m := m9dbNewSim(t)
		s := m9dbConstStep(m)
		if err := s.Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
		m.set(func(m *m9dbSim) { m.failWP["config delete"] = 1 })
		if err := s.Undo(ctx, m); err == nil || !strings.Contains(err.Error(), "xoá hằng DISABLE_WP_CRON") {
			t.Errorf("Undo = %v, muốn lỗi có ngữ cảnh", err)
		}
	})
	t.Run("giá trị cũ không ghi lại được thì từ chối trước khi ghi", func(t *testing.T) {
		for _, v := range []string{`'-rf'`, `array( 1 )`} {
			m := m9dbNewSim(t)
			m.SetFile(m.l.WPConfig, m9dbConfig("define( 'DISABLE_WP_CRON', "+v+" );"), 0o600, system.Owner{User: m.l.User, Group: site.WebGroup})
			// Fake in mảng như PHP in json_encode: không phải chuỗi, bool hay số.
			before := m.whole()
			err := m9dbConstStep(m).Apply(ctx, m)
			if err == nil || !strings.Contains(err.Error(), "sửa tay") {
				t.Errorf("%s: Apply = %v, muốn từ chối", v, err)
			}
			if !reflect.DeepEqual(before, m.whole()) {
				t.Errorf("%s: máy bị đổi dù đã từ chối", v)
			}
		}
	})
}

func TestWPConstantUndoWithoutApply(t *testing.T) {
	m := m9dbNewSim(t)
	if err := m9dbConstStep(m).Undo(context.Background(), m); !errors.Is(err, errNotApplied) {
		t.Errorf("Undo khi chưa Apply = %v, muốn errNotApplied", err)
	}
}

func TestWPConstantBadParams(t *testing.T) {
	ctx := context.Background()
	m := m9dbNewSim(t)
	cases := []struct {
		name, constName, value string
		l                      site.Layout
	}{
		{"layout rỗng", "DISABLE_WP_CRON", "true", site.Layout{}},
		{"tên chữ thường", "disable_wp_cron", "true", m.l},
		{"tên trống", "", "true", m.l},
		{"tên có ;", "A;B", "true", m.l},
		{"tên có $(id)", "$(id)", "true", m.l},
		{"tên bắt đầu bằng -", "--allow-root", "true", m.l},
		{"tên dài 300", strings.Repeat("A", 300), "true", m.l},
		{"giá trị trống", "DISABLE_WP_CRON", "", m.l},
		{"giá trị là mã PHP", "DISABLE_WP_CRON", "true; system('id')", m.l},
		{"giá trị $(id)", "DISABLE_WP_CRON", "$(id)", m.l},
		{"giá trị chuỗi", "DISABLE_WP_CRON", "'x'", m.l},
		{"giá trị có xuống dòng", "DISABLE_WP_CRON", "true\nfalse", m.l},
		{"giá trị a.com; rm -rf /", "DISABLE_WP_CRON", "a.com; rm -rf /", m.l},
		{"giá trị ../../etc/passwd", "DISABLE_WP_CRON", "../../etc/passwd", m.l},
		{"giá trị số thực", "DISABLE_WP_CRON", "1.5", m.l},
		{"giá trị có dấu", "DISABLE_WP_CRON", "đúng", m.l},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m.ResetRan()
			s := &WPConstant{Layout: tc.l, Name: tc.constName, Value: tc.value}
			if _, err := s.Check(ctx, m); err == nil {
				t.Error("Check không lỗi")
			}
			if err := s.Apply(ctx, m); err == nil {
				t.Error("Apply không lỗi")
			}
			if len(m.Ran()) != 0 {
				t.Errorf("chạy lệnh với tham số sai: %v", m.Ran())
			}
		})
	}
}

func TestWPConstantInfo(t *testing.T) {
	m := m9dbNewSim(t)
	info := m9dbConstStep(m).Info()
	if info.ID != "wp.constant:m9a.example.com:DISABLE_WP_CRON" || info.Title.IsZero() || info.Why.IsZero() || len(info.Commands) != 1 {
		t.Errorf("Info thiếu: %+v", info)
	}
	want := "runuser -u " + m.l.User + " -- wp config set DISABLE_WP_CRON true --raw --type=constant --quiet --path=" + m.l.Public
	if info.Commands[0] != want {
		t.Errorf("lệnh = %q\nmuốn   %q", info.Commands[0], want)
	}
	other := (&WPConstant{Layout: m.l, Name: "WP_DEBUG", Value: "false"}).Info()
	if other.Why == info.Why {
		t.Error("hằng khác DISABLE_WP_CRON dùng lý do của cron")
	}
}
