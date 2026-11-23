package steps

// Test của hai Step M28 đợt 2 (m28_dot2.go): ApacheModOff và DBUserLimit, theo
// bốn ca của skill tpan-step (máy sạch rồi Apply thì Check đạt; chạy lại không
// đổi gì; Apply rồi Undo về như cũ; lệnh lỗi có ngữ cảnh) cộng tên tấn công.

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/system"
)

var m28BadNames = []string{"", "status; rm -rf /", "$(id)", "../../etc/passwd", "-q", "st\natus", "Status", strings.Repeat("a", 300), "trạng"}

// ---------------------------------------------------------------- ApacheModOff

// modFake: Apache giả có tập module đang bật trên đĩa. `apachectl -M` in các
// module đó (thêm core_module, proxy_status_module để thử so đúng cột).
type modFake struct {
	*system.FakeSystem
	mu   sync.Mutex
	on   map[string]bool
	fail map[string]bool // "a2dismod", "a2enmod", "configtest", "reload", "-M"
	// elsewhere: module nạp bằng LoadModule ngoài mods-enabled (a2dismod không gỡ được).
	elsewhere []string
}

func newModFake(on ...string) *modFake {
	f := &modFake{FakeSystem: system.NewFake(), on: map[string]bool{}, fail: map[string]bool{}}
	for _, m := range on {
		f.on[m] = true
	}
	bad := func(what string) (system.Result, bool) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.fail[what] {
			return system.Result{ExitCode: 1, Stderr: what + " hỏng\n"}, true
		}
		return system.Result{}, false
	}
	f.Handle([]string{"apachectl", "-M"}, func(system.Cmd) system.Result {
		if r, ok := bad("-M"); ok {
			return r
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		out := "Loaded Modules:\n core_module (static)\n proxy_status_module (shared)\n"
		for _, m := range f.elsewhere {
			if !f.on[m] {
				out += " " + m + "_module (shared)\n"
			}
		}
		for _, m := range slices.Sorted(maps.Keys(f.on)) {
			out += " " + m + "_module (shared)\n"
		}
		return system.Result{Stdout: out}
	})
	for _, name := range []string{"a2dismod", "a2enmod"} {
		f.Handle([]string{name, "-q"}, func(c system.Cmd) system.Result {
			if r, ok := bad(name); ok {
				return r
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			for _, m := range c.Args[1:] {
				if name == "a2enmod" {
					f.on[m] = true
				} else {
					delete(f.on, m)
				}
			}
			return system.Result{}
		})
	}
	f.Handle([]string{"apachectl", "configtest"}, func(system.Cmd) system.Result {
		r, _ := bad("configtest")
		return r
	})
	f.Handle([]string{"systemctl", "reload", "apache2"}, func(system.Cmd) system.Result {
		r, _ := bad("reload")
		return r
	})
	return f
}

func (f *modFake) isOn(m string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.on[m]
}

func (f *modFake) setFail(what string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail[what] = true
}

func (f *modFake) ran() []string {
	var out []string
	for _, c := range f.Ran() {
		out = append(out, c.String())
	}
	return out
}

func TestApacheModOffLifecycle(t *testing.T) {
	ctx := context.Background()
	f := newModFake("status", "rewrite")
	s := &ApacheModOff{Module: "status"}
	if ok, err := s.Check(ctx, f); err != nil || ok {
		t.Fatalf("máy có status: Check = %v %v, muốn false", ok, err)
	}
	if rep := runOne(t, f, s); rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("job %s: %s", rep.Job.Status, rep.Job.Error)
	}
	if f.isOn("status") || !f.isOn("rewrite") {
		t.Errorf("sau Apply: status %v, rewrite %v; muốn chỉ tắt status", f.isOn("status"), f.isOn("rewrite"))
	}
	ran := strings.Join(f.ran(), "\n")
	for _, want := range []string{"a2dismod -q status", "apachectl configtest", "systemctl reload apache2"} {
		if !strings.Contains(ran, want) {
			t.Errorf("thiếu lệnh %q:\n%s", want, ran)
		}
	}
	if strings.Contains(ran, "restart") {
		t.Errorf("tắt status không được restart Apache:\n%s", ran)
	}

	// Chạy lại: chỉ hỏi apachectl -M.
	f.ResetRan()
	if rep := runOne(t, f, &ApacheModOff{Module: "status"}); rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("chạy lại: %s", rep.Job.Error)
	}
	for _, c := range f.ran() {
		if c != "apachectl -M" {
			t.Errorf("chạy lại mà vẫn chạy %q", c)
		}
	}

	// Apply rồi Undo: status bật lại, có kiểm cú pháp và reload.
	g := newModFake("status")
	u := &ApacheModOff{Module: "status"}
	if err := u.Apply(ctx, g); err != nil {
		t.Fatal(err)
	}
	g.ResetRan()
	if err := u.Undo(ctx, g); err != nil {
		t.Fatal(err)
	}
	if !g.isOn("status") {
		t.Error("Undo không bật lại status")
	}
	if got := g.ran(); !slices.Equal(got, []string{"a2enmod -q status", "apachectl configtest", "systemctl reload apache2"}) {
		t.Errorf("Undo chạy %q", got)
	}
	// Apply không tắt gì thì Undo không làm gì.
	h := newModFake()
	n := &ApacheModOff{Module: "status"}
	if err := n.Apply(ctx, h); err != nil {
		t.Fatal(err)
	}
	h.ResetRan()
	if err := n.Undo(ctx, h); err != nil || len(h.Ran()) != 0 {
		t.Errorf("Undo khi Apply không đổi gì: %v, chạy %q", err, h.ran())
	}
	if err := (&ApacheModOff{Module: "status"}).Undo(ctx, h); !errors.Is(err, errNotApplied) {
		t.Errorf("Undo khi chưa Apply: %v", err)
	}
}

// "status" không được khớp proxy_status_module.
func TestApacheModOffExactName(t *testing.T) {
	f := newModFake()
	ok, err := (&ApacheModOff{Module: "status"}).Check(context.Background(), f)
	if err != nil || !ok {
		t.Errorf("chỉ có proxy_status_module: Check = %v %v, muốn true", ok, err)
	}
}

// Lệnh lỗi: lỗi có ngữ cảnh, module được bật lại, và Apache được nạp lại khi
// lần reload hỏng có thể đã nạp cấu hình mới.
func TestApacheModOffErrors(t *testing.T) {
	for _, tc := range []struct {
		fail, want   string
		reloadsAfter int
	}{
		{"-M", "hỏi apachectl các module đang nạp", 0},
		{"a2dismod", "tắt module status", 0},
		{"configtest", "kiểm cấu hình Apache sau khi tắt module status", 0},
		{"reload", "nạp lại dịch vụ sau khi tắt module status", 2},
	} {
		t.Run(tc.fail, func(t *testing.T) {
			f := newModFake("status")
			f.setFail(tc.fail)
			err := (&ApacheModOff{Module: "status"}).Apply(context.Background(), f)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("lỗi = %v, muốn chứa %q", err, tc.want)
			}
			if tc.fail != "-M" && !f.isOn("status") {
				t.Error("Apply lỗi mà status vẫn tắt (engine không Undo bước có Apply lỗi)")
			}
			n := 0
			for _, c := range f.ran() {
				if c == "systemctl reload apache2" {
					n++
				}
			}
			if n != tc.reloadsAfter {
				t.Errorf("reload %d lần, muốn %d", n, tc.reloadsAfter)
			}
		})
	}
}

// D11: status nạp bằng LoadModule ngoài mods-enabled: a2dismod không làm gì.
// Apply hỏi lại apachectl -M, trả ErrModuleLoadedElsewhere kèm chỉ dẫn, trả
// link mods-enabled về như cũ, và không reload.
func TestApacheModOffNapNgoaiModsEnabled(t *testing.T) {
	f := newModFake("status")
	f.elsewhere = []string{"status"}
	err := (&ApacheModOff{Module: "status"}).Apply(context.Background(), f)
	if !errors.Is(err, ErrModuleLoadedElsewhere) || !strings.Contains(err.Error(), "DUMP_INCLUDES") {
		t.Fatalf("lỗi = %v, muốn ErrModuleLoadedElsewhere kèm chỉ dẫn", err)
	}
	if !f.isOn("status") {
		t.Error("link mods-enabled không được trả về")
	}
	if slices.Contains(f.ran(), "systemctl reload apache2") {
		t.Errorf("không được reload khi module vẫn nạp: %q", f.ran())
	}
}

func TestApacheModOffBadNames(t *testing.T) {
	ctx := context.Background()
	for _, name := range m28BadNames {
		f := newModFake("status")
		s := &ApacheModOff{Module: name}
		if _, err := s.Check(ctx, f); err == nil {
			t.Errorf("%q: Check không lỗi", name)
		}
		if err := s.Apply(ctx, f); err == nil {
			t.Errorf("%q: Apply không lỗi", name)
		}
		if len(f.Ran()) != 0 {
			t.Errorf("%q: tên sai mà vẫn chạy %q", name, f.ran())
		}
	}
}

func TestApacheModOffInfo(t *testing.T) {
	info := (&ApacheModOff{Module: "status"}).Info()
	if info.ID != "apache.mod-off:status" || info.Danger || info.Title.Vi == "" || info.Why.En == "" {
		t.Errorf("Info = %+v", info)
	}
	if !slices.Contains(info.Commands, "systemctl reload apache2") || slices.ContainsFunc(info.Commands, func(c string) bool { return strings.Contains(c, "restart") }) {
		t.Errorf("Commands = %q", info.Commands)
	}
}

// ApacheModulesOff là danh sách tắt dùng chung của H5.
func TestApacheModulesOff(t *testing.T) {
	if !slices.Equal(ApacheModulesOff, []string{"mpm_prefork", "status"}) {
		t.Errorf("ApacheModulesOff = %q", ApacheModulesOff)
	}
}

// ----------------------------------------------------------------- DBUserLimit

var (
	limFakeState = regexp.MustCompile(`^SELECT COUNT\(\*\), COALESCE\(MAX\(max_user_connections\), 0\) FROM mysql\.user WHERE User='([a-z0-9_]+)' AND Host='localhost';$`)
	limFakeAlter = regexp.MustCompile(`^ALTER USER '([a-z0-9_]+)'@'localhost' WITH MAX_USER_CONNECTIONS ([0-9]+);$`)
	limFakeKill  = regexp.MustCompile(`^KILL USER '([a-z0-9_]+)'@'localhost';$`)
)

// limFake: MariaDB giả chỉ biết trần kết nối của từng user.
type limFake struct {
	*system.FakeSystem
	mu    sync.Mutex
	users map[string]int
	fail  bool
	// killed: các user đã nhận KILL USER; killFail làm câu KILL lỗi.
	killed   []string
	killFail bool
}

func newLimFake(users map[string]int) *limFake {
	f := &limFake{FakeSystem: system.NewFake(), users: users}
	f.Handle([]string{"mariadb", "-N", "-B"}, func(c system.Cmd) system.Result {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.fail {
			return system.Result{ExitCode: 1, Stderr: "ERROR 2002 (HY000): Can't connect to local server through socket\n"}
		}
		st := strings.TrimSpace(string(c.Stdin))
		if m := limFakeState.FindStringSubmatch(st); m != nil {
			n, ok := f.users[m[1]]
			return system.Result{Stdout: fmt.Sprintf("%d\t%d\n", b2i(ok), n)}
		}
		if m := limFakeKill.FindStringSubmatch(st); m != nil {
			f.killed = append(f.killed, m[1])
			if f.killFail {
				return system.Result{ExitCode: 1, Stderr: "ERROR 1095 (HY000): You are not owner of thread\n"}
			}
			return system.Result{}
		}
		if m := limFakeAlter.FindStringSubmatch(st); m != nil {
			if _, ok := f.users[m[1]]; !ok {
				return system.Result{ExitCode: 1, Stderr: "ERROR 1396 (HY000): Operation ALTER USER failed\n"}
			}
			f.users[m[1]], _ = strconv.Atoi(m[2])
			return system.Result{}
		}
		return system.Result{ExitCode: 1, Stderr: "limFake: câu SQL lạ: " + st}
	})
	return f
}

func (f *limFake) limit(u string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.users[u]
}

const limUser = "tp_shop_example_com_3fa1c2"

func TestDBUserLimitLifecycle(t *testing.T) {
	ctx := context.Background()
	f := newLimFake(map[string]int{limUser: 0})
	s := &DBUserLimit{User: limUser, Max: 20}
	if ok, err := s.Check(ctx, f); err != nil || ok {
		t.Fatalf("user không trần: Check = %v %v, muốn false", ok, err)
	}
	if rep := runOne(t, f, s); rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("job %s: %s", rep.Job.Status, rep.Job.Error)
	}
	if f.limit(limUser) != 20 {
		t.Errorf("trần sau Apply = %d, muốn 20", f.limit(limUser))
	}

	// Chạy lại: chỉ câu SELECT.
	f.ResetRan()
	runOne(t, f, &DBUserLimit{User: limUser, Max: 20})
	for _, c := range f.Ran() {
		if !strings.HasPrefix(string(c.Stdin), "SELECT ") {
			t.Errorf("chạy lại mà vẫn chạy %q", c.Stdin)
		}
	}

	// Undo trả về 0.
	if err := s.Undo(ctx, f); err != nil || f.limit(limUser) != 0 {
		t.Errorf("Undo: %v, trần %d; muốn 0", err, f.limit(limUser))
	}
	if err := (&DBUserLimit{User: limUser, Max: 20}).Undo(ctx, f); !errors.Is(err, errNotApplied) {
		t.Errorf("Undo khi chưa Apply: %v", err)
	}
}

// Trần admin đã đặt (khác 0, kể cả lớn hơn Max) thì giữ; user không có thì đạt
// và không ALTER gì (core nêu tên user đó trong bản xem trước).
func TestDBUserLimitKeepsAndSkips(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name  string
		users map[string]int
	}{
		{"trần admin đã nâng", map[string]int{limUser: 50}},
		{"user không có", map[string]int{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newLimFake(tc.users)
			s := &DBUserLimit{User: limUser, Max: 20}
			if ok, err := s.Check(ctx, f); err != nil || !ok {
				t.Errorf("Check = %v %v, muốn true", ok, err)
			}
			if err := s.Apply(ctx, f); err != nil {
				t.Fatal(err)
			}
			if err := s.Undo(ctx, f); err != nil {
				t.Fatal(err)
			}
			for _, c := range f.Ran() {
				if strings.HasPrefix(string(c.Stdin), "ALTER") {
					t.Errorf("chạy %q, muốn không đổi gì", c.Stdin)
				}
			}
			if got := f.limit(limUser); got != tc.users[limUser] {
				t.Errorf("trần = %d, muốn giữ %d", got, tc.users[limUser])
			}
		})
	}
}

func TestDBUserLimitErrors(t *testing.T) {
	ctx := context.Background()
	f := newLimFake(map[string]int{limUser: 0})
	f.fail = true
	s := &DBUserLimit{User: limUser, Max: 20}
	if _, err := s.Check(ctx, f); err == nil || !strings.Contains(err.Error(), "đọc trần kết nối của user DB "+limUser) {
		t.Errorf("Check lỗi = %v", err)
	}
	if err := s.Apply(ctx, f); err == nil || !strings.Contains(err.Error(), limUser) {
		t.Errorf("Apply lỗi = %v", err)
	}
	// Kết quả lạ của mariadb.
	g := system.NewFake()
	g.Handle([]string{"mariadb"}, func(system.Cmd) system.Result { return system.Result{Stdout: "1\tabc\n"} })
	if _, _, err := DBUserLimitState(ctx, g, limUser); err == nil {
		t.Error("kết quả không phải số mà không lỗi")
	}
}

func TestDBUserLimitBadParams(t *testing.T) {
	ctx := context.Background()
	for _, s := range append(func() []*DBUserLimit {
		var out []*DBUserLimit
		for _, n := range m28BadNames {
			out = append(out, &DBUserLimit{User: n, Max: 20})
		}
		return out
	}(), &DBUserLimit{User: limUser, Max: 0}, &DBUserLimit{User: limUser, Max: -1}) {
		f := newLimFake(map[string]int{limUser: 0})
		if _, err := s.Check(ctx, f); err == nil {
			t.Errorf("%q/%d: Check không lỗi", s.User, s.Max)
		}
		if err := s.Apply(ctx, f); err == nil {
			t.Errorf("%q/%d: Apply không lỗi", s.User, s.Max)
		}
		if len(f.Ran()) != 0 {
			t.Errorf("%q/%d: tham số sai mà vẫn chạy lệnh", s.User, s.Max)
		}
	}
}

// D3: sau ALTER USER, cắt kết nối đang mở của user (kết nối mở trước khi có
// trần không bị đếm). KILL lỗi không làm hỏng bước: trần vẫn đặt. Trần đã có
// (Apply không ALTER) thì không KILL.
func TestDBUserLimitKillKetNoiCu(t *testing.T) {
	ctx := context.Background()
	for _, killFail := range []bool{false, true} {
		f := newLimFake(map[string]int{limUser: 0})
		f.killFail = killFail
		s := &DBUserLimit{User: limUser, Max: 20}
		if err := s.Apply(ctx, f); err != nil {
			t.Fatalf("killFail %v: Apply lỗi %v", killFail, err)
		}
		if f.limit(limUser) != 20 || !slices.Equal(f.killed, []string{limUser}) {
			t.Errorf("killFail %v: trần %d, KILL %q", killFail, f.limit(limUser), f.killed)
		}
		if ok, err := s.Check(ctx, f); err != nil || !ok {
			t.Errorf("killFail %v: Check sau Apply = %v %v", killFail, ok, err)
		}
		var order []string
		for _, c := range f.Ran() {
			order = append(order, strings.Fields(string(c.Stdin))[0])
		}
		// Câu SELECT cuối là của Check sau Apply.
		if !slices.Equal(order, []string{"SELECT", "ALTER", "KILL", "SELECT"}) {
			t.Errorf("thứ tự câu SQL %q, muốn SELECT, ALTER, KILL (rồi SELECT của Check)", order)
		}
	}
	g := newLimFake(map[string]int{limUser: 50})
	if err := (&DBUserLimit{User: limUser, Max: 20}).Apply(ctx, g); err != nil || len(g.killed) != 0 {
		t.Errorf("trần đã có: %v, KILL %q", err, g.killed)
	}
	if all := strings.Join((&DBUserLimit{User: limUser, Max: 20}).Info().Commands, "\n"); !strings.Contains(all, "KILL USER '"+limUser+"'@'localhost';") {
		t.Errorf("Commands thiếu KILL USER:\n%s", all)
	}
}

// Câu SQL của bản xem trước không có mật khẩu, chỉ tên user và số.
func TestDBUserLimitInfo(t *testing.T) {
	info := (&DBUserLimit{User: limUser, Max: 20}).Info()
	if info.ID != "mariadb.user-limit:"+limUser || !info.Danger || info.Title.Vi == "" || info.Why.En == "" {
		t.Errorf("Info = %+v", info)
	}
	all := strings.Join(info.Commands, "\n")
	if !strings.Contains(all, "ALTER USER '"+limUser+"'@'localhost' WITH MAX_USER_CONNECTIONS 20;") {
		t.Errorf("Commands thiếu câu ALTER:\n%s", all)
	}
	if strings.Contains(all, "IDENTIFIED") || strings.Contains(all, "PASSWORD") {
		t.Errorf("Commands có phần mật khẩu:\n%s", all)
	}
}
