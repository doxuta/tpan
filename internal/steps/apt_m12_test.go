package steps

import (
	"context"
	"fmt"
	"io/fs"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

// ================================================================= máy giả M12

// Vân tay giả: 40 ký tự hex. fprSub là vân tay của khoá con (subkey) đi cùng
// fprA, để kiểm Step chỉ tin vân tay của khoá chính.
const (
	fprA   = "15058500A0235D97F5D10063B188E2B695BD4743"
	fprB   = "8540A6F18833A80E9C1653A42FD21310B49F6B46"
	fprSub = "AAAA8500A0235D97F5D10063B188E2B695BD4743"
	tmpDir = "/tmp/tpan-apt-key.Ab3dE6gH"
)

// fakeKey là "file khoá" của gpg giả: một dòng liệt kê vân tay khoá chính.
// gpg giả đọc lại đúng dạng này; nội dung khác thì nó báo không phải khoá.
func fakeKey(fprs ...string) []byte { return []byte("KEY:" + strings.Join(fprs, ",") + "\n") }

func keyFprs(data []byte) ([]string, bool) {
	s, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "KEY:")
	if !ok {
		return nil, false
	}
	if s == "" {
		return nil, true
	}
	return strings.Split(s, ","), true
}

// argAfter trả tham số đứng sau flag trong argv, "" nếu không có.
func argAfter(args []string, flag string) string {
	if i := slices.Index(args, flag); i >= 0 && i+1 < len(args) {
		return args[i+1]
	}
	return ""
}

// m12Machine là newMachine cộng các lệnh M12: mktemp, rm, gpg, gpgconf, curl
// tải khoá, apt-get -s/--only-upgrade/remove, id, usermod, gpasswd,
// mariadb-dump, tail, mariadb-upgrade.
type m12Machine struct {
	*machine
	key       []byte            // nội dung curl "tải" về
	avail     map[string]string // bản mới nhất trên kho
	inst      map[string]string // bản đang cài (AptUpgrade)
	groups    map[string][]string
	dbVersion string // SELECT VERSION()
	dump      string // nội dung mariadb-dump ghi ra
}

func newM12(t *testing.T) *m12Machine {
	t.Helper()
	m := &m12Machine{
		machine:   newMachine(t),
		key:       fakeKey(fprA),
		avail:     map[string]string{},
		inst:      map[string]string{},
		groups:    map[string][]string{},
		dbVersion: "11.4.4-MariaDB-ubu2404",
		dump:      "-- MariaDB dump\nINSERT INTO t VALUES (1);\n-- Dump completed on 2026-11-19 10:00:00\n",
	}
	m.SetDir("/tmp", 0o777, system.Root)
	ok := system.Result{}
	exit := func(code int, stdout, stderr string) system.Result {
		return system.Result{ExitCode: code, Stdout: stdout, Stderr: stderr}
	}

	m.Handle([]string{"mktemp"}, func(system.Cmd) system.Result {
		m.SetDir(tmpDir, 0o700, system.Root)
		return exit(0, tmpDir+"\n", "")
	})
	m.Handle([]string{"rm"}, func(c system.Cmd) system.Result {
		dir := c.Args[len(c.Args)-1]
		var paths []string
		for p := range m.Snapshot() {
			if p == dir || strings.HasPrefix(p, dir+"/") {
				paths = append(paths, p)
			}
		}
		slices.Sort(paths)
		for _, p := range slices.Backward(paths) {
			if err := m.Remove(p); err != nil {
				return exit(1, "", err.Error())
			}
		}
		return ok
	})
	m.Handle([]string{"gpgconf"}, func(system.Cmd) system.Result { return ok })
	m.Handle([]string{"curl"}, func(c system.Cmd) system.Result {
		m.mu.Lock()
		data := slices.Clone(m.key)
		m.mu.Unlock()
		m.SetFile(argAfter(c.Args, "-o"), data, 0o644, system.Root)
		return ok
	})
	m.Handle([]string{"gpg"}, func(c system.Cmd) system.Result {
		a := c.Args
		home := argAfter(a, "--homedir")
		if home == "" {
			return exit(2, "", "gpg: không có --homedir")
		}
		if _, err := m.Stat(home); err != nil {
			return exit(2, "", "gpg: Fatal: "+home+": directory does not exist!")
		}
		switch {
		case slices.Contains(a, "--show-keys"):
			data, err := m.ReadFile(a[len(a)-1])
			if err != nil {
				return exit(2, "", err.Error())
			}
			fprs, ok := keyFprs(data)
			if !ok {
				return exit(2, "", "gpg: no valid OpenPGP data found.")
			}
			var out strings.Builder
			for _, f := range fprs {
				// Mỗi khoá chính có một khoá con: dòng fpr sau "sub" không được tính.
				fmt.Fprintf(&out, "pub:-:255:22:%s:1:::-:::scSC:::::ed25519:::0:\nfpr:::::::::%s:\nuid:-::::1::X::Test <t@example.com>::::::::::0:\nsub:-:255:18:X:1::::::e:::::cv25519::\nfpr:::::::::%s:\n", f[24:], f, fprSub)
			}
			return exit(0, out.String(), "")
		case slices.Contains(a, "--import"):
			data, err := m.ReadFile(a[len(a)-1])
			if err != nil {
				return exit(2, "", err.Error())
			}
			if _, ok := keyFprs(data); !ok {
				return exit(2, "", "gpg: no valid OpenPGP data found.")
			}
			m.SetFile(argAfter(a, "--keyring"), data, 0o644, system.Root)
			return ok
		case slices.Contains(a, "--export"):
			data, err := m.ReadFile(argAfter(a, "--keyring"))
			if err != nil {
				return exit(2, "", err.Error())
			}
			have, _ := keyFprs(data)
			want := a[slices.Index(a, "--export")+1:]
			var got []string
			for _, f := range have {
				// Như gpg thật: vân tay khoá con cũng khớp và xuất cả khoá chính.
				if slices.Contains(want, f) || slices.Contains(want, fprSub) {
					got = append(got, f)
				}
			}
			if len(got) == 0 {
				return exit(0, "", "gpg: WARNING: nothing exported\n")
			}
			m.SetFile(argAfter(a, "--output"), fakeKey(got...), 0o644, system.Root)
			return ok
		}
		return exit(2, "", "gpg giả: lệnh lạ")
	})

	// apt-get -s install --only-upgrade ... và apt-get install -y --only-upgrade ...
	pkgArgs := func(args []string) []string {
		var out []string
		for i := 0; i < len(args); i++ {
			switch {
			case args[i] == "-o":
				i++
			case strings.HasPrefix(args[i], "-"):
			default:
				out = append(out, args[i])
			}
		}
		return out
	}
	m.Handle([]string{"apt-get", "-s", "install", "--only-upgrade"}, func(c system.Cmd) system.Result {
		m.mu.Lock()
		defer m.mu.Unlock()
		var out strings.Builder
		// Gói phụ thuộc được nâng kèm: dòng Inst của nó không tính.
		out.WriteString("Inst php-common [2:93ubuntu2] (2:93ubuntu3 Ubuntu:24.04/noble-updates [amd64])\n")
		for _, p := range pkgArgs(c.Args[3:]) {
			if v, ok := m.inst[p]; ok && v != m.avail[p] {
				fmt.Fprintf(&out, "Inst %s [%s] (%s Ubuntu:24.04/noble-security [amd64])\nConf %s (%s Ubuntu:24.04/noble-security [amd64])\n", p, v, m.avail[p], p, m.avail[p])
			}
		}
		return exit(0, out.String(), "")
	})
	m.Handle([]string{"apt-get", "install", "-y", "--only-upgrade"}, func(c system.Cmd) system.Result {
		m.mu.Lock()
		defer m.mu.Unlock()
		for _, p := range pkgArgs(c.Args[3:]) {
			if _, ok := m.inst[p]; ok {
				m.inst[p] = m.avail[p]
			}
		}
		return ok
	})
	m.Handle([]string{"apt-get", "remove", "-y"}, func(c system.Cmd) system.Result {
		m.mu.Lock()
		defer m.mu.Unlock()
		for _, p := range pkgArgs(c.Args[2:]) {
			delete(m.pkgs, p)
		}
		return ok
	})

	// systemctl start của newMachine không đổi trạng thái; ServiceStop.Undo cần nó.
	m.Handle([]string{"systemctl", "start"}, func(c system.Cmd) system.Result {
		m.mu.Lock()
		defer m.mu.Unlock()
		u := m.units[c.Args[1]]
		u.Active = true
		m.units[c.Args[1]] = u
		return ok
	})
	m.Handle([]string{"id", "-nG"}, func(c system.Cmd) system.Result {
		m.mu.Lock()
		defer m.mu.Unlock()
		g, ok := m.groups[c.Args[len(c.Args)-1]]
		if !ok {
			return exit(1, "", "id: '"+c.Args[len(c.Args)-1]+"': no such user\n")
		}
		return exit(0, strings.Join(g, " ")+"\n", "")
	})
	m.Handle([]string{"usermod", "-aG"}, func(c system.Cmd) system.Result {
		m.mu.Lock()
		defer m.mu.Unlock()
		group, user := c.Args[1], c.Args[2]
		if _, ok := m.groups[user]; !ok {
			return exit(6, "", "usermod: user '"+user+"' does not exist\n")
		}
		m.groups[user] = append(m.groups[user], group)
		return ok
	})
	m.Handle([]string{"gpasswd", "-d"}, func(c system.Cmd) system.Result {
		m.mu.Lock()
		defer m.mu.Unlock()
		user, group := c.Args[1], c.Args[2]
		m.groups[user] = slices.DeleteFunc(m.groups[user], func(g string) bool { return g == group })
		return ok
	})

	m.Handle([]string{"mariadb-dump"}, func(c system.Cmd) system.Result {
		var file string
		for _, a := range c.Args {
			if v, ok := strings.CutPrefix(a, "--result-file="); ok {
				file = v
			}
		}
		// Như fopen("w"): file đã có giữ quyền và chủ, chỉ thay nội dung.
		perm, owner := fs.FileMode(0o644), system.Root
		if fi, err := m.Stat(file); err == nil {
			perm, owner = fi.Mode.Perm(), fi.Owner
		}
		m.mu.Lock()
		data := m.dump
		m.mu.Unlock()
		m.SetFile(file, []byte(data), perm, owner)
		return ok
	})
	m.Handle([]string{"tail", "-n", "1"}, func(c system.Cmd) system.Result {
		data, err := m.ReadFile(c.Args[len(c.Args)-1])
		if err != nil {
			return exit(1, "", err.Error())
		}
		lines := strings.SplitAfter(strings.TrimSuffix(string(data), "\n"), "\n")
		return exit(0, lines[len(lines)-1]+"\n", "")
	})
	m.Handle([]string{"mariadb", "-N", "-B", "-e"}, func(system.Cmd) system.Result {
		m.mu.Lock()
		defer m.mu.Unlock()
		return exit(0, m.dbVersion+"\n", "")
	})
	m.Handle([]string{"mariadb-upgrade"}, func(system.Cmd) system.Result {
		m.mu.Lock()
		v := m.dbVersion
		m.mu.Unlock()
		ver, _, _ := strings.Cut(v, "-")
		m.SetFile(mariadbUpgradeInfo, []byte(ver+"-MariaDB"), 0o644, system.Owner{User: "mysql", Group: "mysql"})
		return ok
	})
	return m
}

// m12State là trạng thái ngoài hệ thống file giả, cho các phép so trước và sau.
func (m *m12Machine) m12State() any {
	whole := m.whole() // whole tự khoá m.mu
	m.mu.Lock()
	defer m.mu.Unlock()
	g := map[string][]string{}
	for k, v := range m.groups {
		g[k] = slices.Clone(v)
	}
	return []any{whole, g}
}

func ranHas(m *m12Machine, sub string) bool {
	for _, s := range ranStrings(m.Ran()) {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// noTmpLeft: thư mục tạm của gpg phải được dọn sau mọi lần chạy.
func noTmpLeft(t *testing.T, m *m12Machine) {
	t.Helper()
	for p := range m.Snapshot() {
		if strings.HasPrefix(p, "/tmp/") {
			t.Errorf("còn sót %s", p)
		}
	}
}

// ====================================================================== AptRepo

const testSources = "Types: deb\nURIs: https://packages.sury.org/php/\nSuites: noble\nComponents: main\nSigned-By: /etc/apt/keyrings/tpan-sury.gpg\n"

func repoStep() *AptRepo {
	return &AptRepo{
		Name:         "sury",
		Title:        i18n.T("kho PHP của Ondřej Surý (packages.sury.org)", "Ondřej Surý's PHP repository (packages.sury.org)"),
		KeyURL:       "https://packages.sury.org/php/apt.gpg",
		Fingerprints: []string{fprA},
		Sources:      []byte(testSources),
		Pin:          []byte("Package: *\nPin: origin packages.sury.org\nPin-Priority: 100\n"),
	}
}

const (
	surySources = "/etc/apt/sources.list.d/tpan-sury.sources"
	suryPin     = "/etc/apt/preferences.d/tpan-sury"
	suryKey     = "/etc/apt/keyrings/tpan-sury.gpg"
)

// aptBase là /etc/apt của Ubuntu 24.04: có sẵn ba thư mục.
func aptBase(m *m12Machine) {
	for _, d := range []string{"/etc/apt/keyrings", "/etc/apt/sources.list.d", "/etc/apt/preferences.d"} {
		m.SetDir(d, 0o755, system.Root)
	}
}

func TestAptKeyPath(t *testing.T) {
	if got := AptKeyPath("sury"); got != suryKey {
		t.Fatalf("AptKeyPath = %q", got)
	}
}

func TestAptRepo(t *testing.T) {
	ctx := context.Background()

	t.Run("máy sạch rồi Apply thì Check true, tạm được dọn", func(t *testing.T) {
		m := newM12(t)
		aptBase(m)
		s := repoStep()
		if done, err := s.Check(ctx, m); err != nil || done {
			t.Fatalf("Check trước = %v, %v", done, err)
		}
		if err := s.Apply(ctx, m); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		snap := m.Snapshot()
		if e := snap[suryKey]; e.Data != string(fakeKey(fprA)) || e.Mode != 0o644 {
			t.Errorf("khoá = %+v", e)
		}
		if e := snap[surySources]; e.Data != testSources || e.Mode != 0o644 {
			t.Errorf("sources = %+v", e)
		}
		if snap[suryPin].Data != string(repoStep().Pin) {
			t.Errorf("pin = %q", snap[suryPin].Data)
		}
		if m.updates != 1 {
			t.Errorf("apt-get update chạy %d lần", m.updates)
		}
		if !ranHas(m, "curl -fsSL --proto =https --max-time 60 -o "+tmpDir+"/") || !ranHas(m, "gpgconf --homedir "+tmpDir+" --kill all") {
			t.Errorf("thiếu curl hoặc gpgconf: %v", ranStrings(m.Ran()))
		}
		for _, c := range m.Ran() {
			if c.Name == "apt-get" && !slices.Contains(c.Args, "DPkg::Lock::Timeout=300") {
				t.Errorf("apt-get thiếu DPkg::Lock::Timeout: %s", c.String())
			}
		}
		noTmpLeft(t, m)
		if done, err := s.Check(ctx, m); err != nil || !done {
			t.Fatalf("Check sau = %v, %v", done, err)
		}
		noTmpLeft(t, m)
	})

	t.Run("chạy lại thì bỏ qua, không tải không update", func(t *testing.T) {
		m := newM12(t)
		aptBase(m)
		if rep := execute(t, m.machine, repoStep()); rep.Job.Status != engine.JobSucceeded {
			t.Fatalf("lần đầu: %s %s", rep.Job.Status, rep.Job.Error)
		}
		before := m.Snapshot()
		m.ResetRan()
		rep := execute(t, m.machine, repoStep())
		if rep.Steps[0].Outcome != "skipped" {
			t.Fatalf("lần hai: %q %s", rep.Steps[0].Outcome, rep.Steps[0].Error)
		}
		for _, c := range m.Ran() {
			if c.Name == "curl" || c.Name == "apt-get" || slices.Contains(c.Args, "--import") {
				t.Errorf("lần hai chạy %s", c.String())
			}
		}
		if !reflect.DeepEqual(before, m.Snapshot()) {
			t.Error("lần hai làm đổi máy")
		}
	})

	t.Run("Apply rồi Undo trả ba file và thư mục về như cũ", func(t *testing.T) {
		m := newM12(t)
		// Không có keyrings: Apply tạo, Undo gỡ. Có sẵn pin cũ: Undo ghi lại.
		m.SetDir("/etc/apt/sources.list.d", 0o755, system.Root)
		m.SetFile(suryPin, []byte("pin cũ\n"), 0o600, system.Root)
		before := m.Snapshot()
		s := repoStep()
		if err := s.Apply(ctx, m); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if reflect.DeepEqual(before, m.Snapshot()) {
			t.Fatal("Apply không đổi gì")
		}
		n := m.updates
		if err := s.Undo(ctx, m); err != nil {
			t.Fatalf("Undo: %v", err)
		}
		if !reflect.DeepEqual(before, m.Snapshot()) {
			t.Errorf("sau Undo khác trước:\n%v\n%v", before, m.Snapshot())
		}
		if m.updates != n+1 {
			t.Error("Undo không chạy apt-get update")
		}
	})

	t.Run("Pin nil thì gỡ pin cũ, Undo trả lại", func(t *testing.T) {
		m := newM12(t)
		aptBase(m)
		m.SetFile(suryPin, []byte("pin cũ\n"), 0o644, system.Root)
		before := m.Snapshot()
		s := repoStep()
		s.Pin = nil
		if err := s.Apply(ctx, m); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if _, ok := m.Snapshot()[suryPin]; ok {
			t.Error("pin cũ còn")
		}
		if done, err := s.Check(ctx, m); err != nil || !done {
			t.Fatalf("Check sau = %v, %v", done, err)
		}
		if err := s.Undo(ctx, m); err != nil {
			t.Fatalf("Undo: %v", err)
		}
		if !reflect.DeepEqual(before, m.Snapshot()) {
			t.Error("sau Undo khác trước")
		}
	})

	t.Run("apt-get update lỗi thì ba file về như cũ", func(t *testing.T) {
		m := newM12(t)
		aptBase(m)
		m.SetFile(surySources, []byte("sources cũ\n"), 0o644, system.Root)
		before := m.Snapshot()
		m.failCmd([]string{"apt-get", "update"}, 100, "E: The repository is not signed.")
		err := repoStep().Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "sury") || !strings.Contains(err.Error(), "not signed") {
			t.Fatalf("lỗi = %v", err)
		}
		if !reflect.DeepEqual(before, m.Snapshot()) {
			t.Errorf("máy khác trước:\n%v\n%v", before, m.Snapshot())
		}
	})

	t.Run("ghi sources lỗi thì khoá được trả về", func(t *testing.T) {
		m := newM12(t)
		aptBase(m)
		before := m.Snapshot()
		m.Fail("write", surySources, fs.ErrPermission)
		if err := repoStep().Apply(ctx, m); err == nil {
			t.Fatal("Apply không lỗi")
		}
		if !reflect.DeepEqual(before, m.Snapshot()) {
			t.Errorf("máy khác trước:\n%v\n%v", before, m.Snapshot())
		}
		if ranHas(m, "apt-get update") {
			t.Error("vẫn chạy apt-get update")
		}
	})

	keyCases := []struct {
		name string
		key  []byte
		want []string // vân tay phải có trong khoá ghi ra; nil: không ghi gì
	}{
		{"khoá không có vân tay nào trong danh sách", fakeKey(fprB), nil},
		{"vân tay khớp khoá con, không phải khoá chính", fakeKey(fprB), nil},
		{"không phải khoá", []byte("<html>404</html>"), nil},
		{"khoá có thêm khoá lạ: chỉ xuất khoá đã khai", fakeKey(fprB, fprA), []string{fprA}},
	}
	for i, tc := range keyCases {
		t.Run(tc.name, func(t *testing.T) {
			m := newM12(t)
			aptBase(m)
			m.key = tc.key
			s := repoStep()
			if i == 1 {
				s.Fingerprints = []string{fprSub}
			}
			before := m.Snapshot()
			err := s.Apply(ctx, m)
			noTmpLeft(t, m)
			if tc.want == nil {
				if err == nil {
					t.Fatal("Apply không lỗi")
				}
				if !reflect.DeepEqual(before, m.Snapshot()) {
					t.Errorf("có file bị ghi:\n%v\n%v", before, m.Snapshot())
				}
				if ranHas(m, "apt-get") {
					t.Error("vẫn chạy apt-get")
				}
				return
			}
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if got, _ := keyFprs([]byte(m.Snapshot()[suryKey].Data)); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("khoá ghi ra có %v", got)
			}
		})
	}

	t.Run("vân tay viết thường có dấu cách được chuẩn hoá", func(t *testing.T) {
		m := newM12(t)
		aptBase(m)
		s := repoStep()
		s.Fingerprints = []string{"1505 8500 a023 5d97 f5d1  0063 b188 e2b6 95bd 4743"}
		if err := s.Apply(ctx, m); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if !ranHas(m, "--export "+fprA) {
			t.Errorf("không export vân tay chuẩn hoá: %v", ranStrings(m.Ran()))
		}
		if done, err := s.Check(ctx, m); err != nil || !done {
			t.Fatalf("Check sau = %v, %v", done, err)
		}
	})

	t.Run("khoá trên máy bị thay bằng khoá khác thì Check false", func(t *testing.T) {
		m := newM12(t)
		aptBase(m)
		s := repoStep()
		if err := s.Apply(ctx, m); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		m.SetFile(suryKey, fakeKey(fprB), 0o644, system.Root)
		if done, err := repoStep().Check(ctx, m); err != nil || done {
			t.Fatalf("Check = %v, %v", done, err)
		}
	})

	t.Run("job bị huỷ giữa lúc tải thì thư mục tạm vẫn được dọn", func(t *testing.T) {
		m := newM12(t)
		aptBase(m)
		cctx, cancel := context.WithCancel(ctx)
		defer cancel()
		m.Handle([]string{"curl"}, func(c system.Cmd) system.Result {
			m.SetFile(argAfter(c.Args, "-o"), m.key, 0o644, system.Root)
			cancel()
			return system.Result{}
		})
		before := m.Snapshot()
		if err := repoStep().Apply(cctx, m); err == nil {
			t.Fatal("Apply không lỗi")
		}
		noTmpLeft(t, m)
		if !reflect.DeepEqual(before, m.Snapshot()) {
			t.Error("máy khác trước")
		}
	})

	t.Run("Undo khi chưa Apply thì lỗi", func(t *testing.T) {
		if err := repoStep().Undo(ctx, newM12(t)); err == nil {
			t.Fatal("không lỗi")
		}
	})
}

func TestAptRepoBadParams(t *testing.T) {
	ctx := context.Background()
	cases := map[string]func(s *AptRepo){
		"tên trống":           func(s *AptRepo) { s.Name = "" },
		"tên có ../":          func(s *AptRepo) { s.Name = "../../etc/passwd" },
		"tên bắt đầu bằng -":  func(s *AptRepo) { s.Name = "-o" },
		"tên có chấm phẩy":    func(s *AptRepo) { s.Name = "a.com; rm -rf /" },
		"tên có $(id)":        func(s *AptRepo) { s.Name = "$(id)" },
		"tên có xuống dòng":   func(s *AptRepo) { s.Name = "sury\nx" },
		"tên dài 300":         func(s *AptRepo) { s.Name = strings.Repeat("a", 300) },
		"URL http":            func(s *AptRepo) { s.KeyURL = "http://packages.sury.org/php/apt.gpg" },
		"URL file":            func(s *AptRepo) { s.KeyURL = "file:///etc/passwd" },
		"URL có user":         func(s *AptRepo) { s.KeyURL = "https://u:p@example.com/k.gpg" },
		"URL có xuống dòng":   func(s *AptRepo) { s.KeyURL = "https://example.com/k.gpg\n-o /etc/passwd" },
		"URL có dấu cách":     func(s *AptRepo) { s.KeyURL = "https://example.com/k.gpg -o x" },
		"URL không host":      func(s *AptRepo) { s.KeyURL = "https:///k.gpg" },
		"URL là tuỳ chọn":     func(s *AptRepo) { s.KeyURL = "-K/etc/passwd" },
		"không có vân tay":    func(s *AptRepo) { s.Fingerprints = nil },
		"vân tay ngắn":        func(s *AptRepo) { s.Fingerprints = []string{"B188E2B695BD4743"} },
		"vân tay không hex":   func(s *AptRepo) { s.Fingerprints = []string{strings.Repeat("Z", 40)} },
		"vân tay là tuỳ chọn": func(s *AptRepo) { s.Fingerprints = []string{"--import"} },
		"sources trống":       func(s *AptRepo) { s.Sources = nil },
		"sources thiếu Signed-By": func(s *AptRepo) {
			s.Sources = []byte("Types: deb\nURIs: https://packages.sury.org/php/\nSuites: noble\nComponents: main\n")
		},
	}
	for name, mod := range cases {
		t.Run(name, func(t *testing.T) {
			m := newM12(t)
			aptBase(m)
			s := repoStep()
			mod(s)
			if _, err := s.Check(ctx, m); err == nil {
				t.Error("Check không lỗi")
			}
			if err := s.Apply(ctx, m); err == nil {
				t.Error("Apply không lỗi")
			}
			if len(m.Ran()) != 0 {
				t.Errorf("đã chạy lệnh: %v", ranStrings(m.Ran()))
			}
		})
	}
}

func TestAptRepoInfo(t *testing.T) {
	info := repoStep().Info()
	if info.ID != "apt.repo:sury" || !info.Danger || !info.NoUndo.IsZero() {
		t.Errorf("Info = %+v", info)
	}
	if !strings.Contains(info.Title.Vi, "packages.sury.org") || info.Why.Vi == "" || info.Why.En == "" || info.Title.En == "" {
		t.Errorf("Title/Why = %+v %+v", info.Title, info.Why)
	}
	cmds := strings.Join(info.Commands, "\n")
	for _, want := range []string{"curl -fsSL --proto =https --max-time 60", repoStep().KeyURL, "--export " + fprA, "apt-get update"} {
		if !strings.Contains(cmds, want) {
			t.Errorf("Commands thiếu %q:\n%s", want, cmds)
		}
	}
	var paths []string
	for _, f := range info.Files {
		paths = append(paths, f.Path)
	}
	if !slices.Equal(paths, []string{surySources, suryPin}) {
		t.Errorf("Files = %v", paths)
	}
	s := repoStep()
	s.Pin = nil
	if f := s.Info().Files; len(f) != 2 || !f[1].Delete {
		t.Errorf("Pin nil: Files = %+v", f)
	}
}

// =================================================================== AptUpgrade

func TestAptUpgrade(t *testing.T) {
	ctx := context.Background()
	setup := func() *m12Machine {
		m := newM12(t)
		m.inst["nginx"], m.avail["nginx"] = "1.24.0-2ubuntu7.4", "1.24.0-2ubuntu7.5"
		m.inst["php8.3-fpm"], m.avail["php8.3-fpm"] = "8.3.6-0ubuntu0.24.04.5", "8.3.6-0ubuntu0.24.04.5"
		return m
	}
	step := func() *AptUpgrade { return &AptUpgrade{Packages: []string{"nginx", "php8.3-fpm"}} }

	t.Run("có bản mới rồi Apply thì Check true", func(t *testing.T) {
		m := setup()
		s := step()
		if done, err := s.Check(ctx, m); err != nil || done {
			t.Fatalf("Check trước = %v, %v", done, err)
		}
		if err := s.Apply(ctx, m); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if m.inst["nginx"] != m.avail["nginx"] {
			t.Error("chưa nâng nginx")
		}
		want := "apt-get install -y --only-upgrade -o DPkg::Lock::Timeout=300 -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold nginx php8.3-fpm"
		if !ranHas(m, want) {
			t.Errorf("thiếu %q: %v", want, ranStrings(m.Ran()))
		}
		if done, err := s.Check(ctx, m); err != nil || !done {
			t.Fatalf("Check sau = %v, %v", done, err)
		}
	})

	t.Run("đã mới nhất thì bỏ qua (Inst của gói phụ thuộc không tính)", func(t *testing.T) {
		m := setup()
		m.avail["nginx"] = m.inst["nginx"]
		rep := execute(t, m.machine, step())
		if rep.Steps[0].Outcome != "skipped" {
			t.Fatalf("outcome %q", rep.Steps[0].Outcome)
		}
		if ranHas(m, "install -y") {
			t.Error("vẫn chạy apt-get install")
		}
	})

	t.Run("dòng Inst có đuôi kiến trúc", func(t *testing.T) {
		m := newM12(t)
		m.Handle([]string{"apt-get", "-s", "install", "--only-upgrade"}, func(system.Cmd) system.Result {
			return system.Result{Stdout: "Inst nginx:arm64 [1] (2 Ubuntu:24.04/noble-updates [arm64])\n"}
		})
		if done, err := (&AptUpgrade{Packages: []string{"nginx"}}).Check(ctx, m); err != nil || done {
			t.Fatalf("Check = %v, %v", done, err)
		}
	})

	t.Run("lệnh lỗi thì lỗi có ngữ cảnh", func(t *testing.T) {
		m := setup()
		m.failCmd([]string{"apt-get", "install", "-y", "--only-upgrade"}, 100, "E: Could not get lock")
		err := step().Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "nginx") || !strings.Contains(err.Error(), "Could not get lock") {
			t.Fatalf("lỗi = %v", err)
		}
		m2 := setup()
		m2.failCmd([]string{"apt-get", "-s", "install", "--only-upgrade"}, 100, "E: broken")
		if _, err := step().Check(ctx, m2); err == nil {
			t.Fatal("Check không lỗi")
		}
	})

	t.Run("NoUndo và Danger", func(t *testing.T) {
		info := step().Info()
		if info.NoUndo.IsZero() || !info.Danger || info.ID != "apt.upgrade:nginx,php8.3-fpm" {
			t.Errorf("Info = %+v", info)
		}
	})
}

// badPkgLists là các danh sách gói phải bị từ chối trước khi chạy lệnh.
var badPkgLists = map[string][]string{
	"trống":        nil,
	"tuỳ chọn":     {"-oAPT::Get::AllowUnauthenticated=1"},
	"chấm phẩy":    {"nginx; rm -rf /"},
	"$(id)":        {"$(id)"},
	"đường dẫn":    {"../../etc/passwd"},
	"xuống dòng":   {"nginx\nphp"},
	"chữ hoa":      {"Nginx"},
	"tên miền dấu": {"nginx-việt"},
	"chuỗi trống":  {""},
}

func TestAptUpgradeRemoveBadParams(t *testing.T) {
	ctx := context.Background()
	for name, pkgs := range badPkgLists {
		for _, s := range []engine.Step{&AptUpgrade{Packages: pkgs}, &AptRemove{Packages: pkgs}} {
			t.Run(fmt.Sprintf("%T %s", s, name), func(t *testing.T) {
				m := newM12(t)
				if _, err := s.Check(ctx, m); err == nil {
					t.Error("Check không lỗi")
				}
				if err := s.Apply(ctx, m); err == nil {
					t.Error("Apply không lỗi")
				}
				if len(m.Ran()) != 0 {
					t.Errorf("đã chạy: %v", ranStrings(m.Ran()))
				}
			})
		}
	}
}

// ==================================================================== AptRemove

func TestAptRemove(t *testing.T) {
	ctx := context.Background()
	step := func() *AptRemove { return &AptRemove{Packages: []string{"php8.2-fpm", "php8.2-cli"}} }

	t.Run("đang cài rồi Apply thì Check true", func(t *testing.T) {
		m := newM12(t)
		m.pkgs["php8.2-fpm"], m.pkgs["php8.2-cli"], m.pkgs["nginx"] = true, true, true
		s := step()
		if done, err := s.Check(ctx, m); err != nil || done {
			t.Fatalf("Check trước = %v, %v", done, err)
		}
		if err := s.Apply(ctx, m); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if !ranHas(m, "apt-get remove -y -o DPkg::Lock::Timeout=300 php8.2-fpm php8.2-cli") {
			t.Errorf("lệnh: %v", ranStrings(m.Ran()))
		}
		if !m.pkgs["nginx"] {
			t.Error("gỡ nhầm nginx")
		}
		if done, err := s.Check(ctx, m); err != nil || !done {
			t.Fatalf("Check sau = %v, %v", done, err)
		}
	})

	t.Run("một gói còn cài thì Check false", func(t *testing.T) {
		m := newM12(t)
		m.pkgs["php8.2-cli"] = true
		if done, err := step().Check(ctx, m); err != nil || done {
			t.Fatalf("Check = %v, %v", done, err)
		}
	})

	t.Run("không gói nào cài thì bỏ qua", func(t *testing.T) {
		m := newM12(t)
		rep := execute(t, m.machine, step())
		if rep.Steps[0].Outcome != "skipped" || ranHas(m, "remove") {
			t.Fatalf("outcome %q, ran %v", rep.Steps[0].Outcome, ranStrings(m.Ran()))
		}
	})

	t.Run("lệnh lỗi thì lỗi có ngữ cảnh", func(t *testing.T) {
		m := newM12(t)
		m.pkgs["php8.2-fpm"] = true
		m.failCmd([]string{"apt-get", "remove", "-y"}, 100, "E: dpkg was interrupted")
		err := step().Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "php8.2-fpm") || !strings.Contains(err.Error(), "dpkg was interrupted") {
			t.Fatalf("lỗi = %v", err)
		}
	})

	t.Run("mod_php được phép gỡ", func(t *testing.T) {
		m := newM12(t)
		m.pkgs["libapache2-mod-php8.3"] = true
		if err := (&AptRemove{Packages: []string{"libapache2-mod-php8.3"}}).Apply(ctx, m); err != nil {
			t.Fatalf("Apply: %v", err)
		}
	})

	t.Run("NoUndo và Danger", func(t *testing.T) {
		info := step().Info()
		if info.NoUndo.IsZero() || !info.Danger || info.ID != "apt.remove:php8.2-fpm,php8.2-cli" {
			t.Errorf("Info = %+v", info)
		}
	})
}

// =============================================================== ServiceRestart

func TestServiceRestart(t *testing.T) {
	ctx := context.Background()
	test := &system.Cmd{Name: "nginx", Args: []string{"-t"}}

	t.Run("Check false tới khi Apply xong, Test chạy trước restart", func(t *testing.T) {
		m := newM12(t)
		m.units["nginx"] = unitState{Active: true, Enabled: true}
		s := &ServiceRestart{Unit: "nginx", Test: test}
		if done, err := s.Check(ctx, m); err != nil || done {
			t.Fatalf("Check trước = %v, %v", done, err)
		}
		if err := s.Apply(ctx, m); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		got := ranStrings(m.Ran())
		if i, j := slices.Index(got, "nginx -t"), slices.Index(got, "systemctl restart nginx"); i < 0 || j < i {
			t.Errorf("thứ tự lệnh: %v", got)
		}
		if done, err := s.Check(ctx, m); err != nil || !done {
			t.Fatalf("Check sau = %v, %v", done, err)
		}
	})

	t.Run("Test lỗi thì không restart", func(t *testing.T) {
		m := newM12(t)
		m.failCmd([]string{"nginx", "-t"}, 1, "nginx: [emerg] unknown directive")
		err := (&ServiceRestart{Unit: "nginx", Test: test}).Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "unknown directive") {
			t.Fatalf("lỗi = %v", err)
		}
		if ranHas(m, "restart") {
			t.Error("vẫn restart")
		}
	})

	t.Run("restart lỗi thì lỗi có ngữ cảnh", func(t *testing.T) {
		m := newM12(t)
		m.failCmd([]string{"systemctl", "restart"}, 1, "Job for nginx.service failed")
		err := (&ServiceRestart{Unit: "nginx"}).Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "nginx") || !strings.Contains(err.Error(), "Job for") {
			t.Fatalf("lỗi = %v", err)
		}
	})

	t.Run("OnlyIfActive: dịch vụ đang tắt thì bỏ qua", func(t *testing.T) {
		m := newM12(t)
		m.units["php8.4-fpm"] = unitState{}
		rep := execute(t, m.machine, &ServiceRestart{Unit: "php8.4-fpm", OnlyIfActive: true})
		if rep.Steps[0].Outcome != "skipped" || ranHas(m, "restart") {
			t.Fatalf("outcome %q, ran %v", rep.Steps[0].Outcome, ranStrings(m.Ran()))
		}
	})

	t.Run("OnlyIfActive: đang chạy thì restart", func(t *testing.T) {
		m := newM12(t)
		m.units["php8.4-fpm"] = unitState{Active: true}
		rep := execute(t, m.machine, &ServiceRestart{Unit: "php8.4-fpm", OnlyIfActive: true})
		if rep.Job.Status != engine.JobSucceeded || !ranHas(m, "systemctl restart php8.4-fpm") {
			t.Fatalf("%s %s, ran %v", rep.Job.Status, rep.Job.Error, ranStrings(m.Ran()))
		}
	})

	t.Run("Info", func(t *testing.T) {
		info := (&ServiceRestart{Unit: "nginx", Test: test}).Info()
		if !info.Danger || info.NoUndo.IsZero() || info.ID != "service.restart:nginx" ||
			!slices.Equal(info.Commands, []string{"nginx -t", "systemctl restart nginx"}) {
			t.Errorf("Info = %+v", info)
		}
	})
}

// ================================================================== ServiceStop

func TestServiceStop(t *testing.T) {
	ctx := context.Background()

	t.Run("đang chạy: Apply dừng, Undo bật lại", func(t *testing.T) {
		m := newM12(t)
		m.units["mariadb"] = unitState{Active: true, Enabled: true}
		before := m.m12State()
		s := &ServiceStop{Unit: "mariadb"}
		if done, err := s.Check(ctx, m); err != nil || done {
			t.Fatalf("Check trước = %v, %v", done, err)
		}
		if err := s.Apply(ctx, m); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if done, err := s.Check(ctx, m); err != nil || !done {
			t.Fatalf("Check sau = %v, %v", done, err)
		}
		if err := s.Undo(ctx, m); err != nil {
			t.Fatalf("Undo: %v", err)
		}
		if !ranHas(m, "systemctl start mariadb") || !reflect.DeepEqual(before, m.m12State()) {
			t.Errorf("Undo không bật lại: %v", ranStrings(m.Ran()))
		}
	})

	t.Run("đã dừng thì bỏ qua", func(t *testing.T) {
		m := newM12(t)
		m.units["mariadb"] = unitState{Enabled: true}
		rep := execute(t, m.machine, &ServiceStop{Unit: "mariadb"})
		if rep.Steps[0].Outcome != "skipped" || ranHas(m, "stop") {
			t.Fatalf("outcome %q", rep.Steps[0].Outcome)
		}
	})

	t.Run("stop lỗi thì bật lại và báo lỗi", func(t *testing.T) {
		m := newM12(t)
		m.units["mariadb"] = unitState{Active: true}
		m.failCmd([]string{"systemctl", "stop"}, 1, "Job for mariadb.service canceled")
		err := (&ServiceStop{Unit: "mariadb"}).Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "mariadb") || !strings.Contains(err.Error(), "canceled") {
			t.Fatalf("lỗi = %v", err)
		}
		if !ranHas(m, "systemctl start mariadb") {
			t.Error("không thử bật lại")
		}
	})

	t.Run("Undo khi chưa Apply thì lỗi", func(t *testing.T) {
		if err := (&ServiceStop{Unit: "mariadb"}).Undo(ctx, newM12(t)); err == nil {
			t.Fatal("không lỗi")
		}
	})

	t.Run("Info", func(t *testing.T) {
		info := (&ServiceStop{Unit: "mariadb"}).Info()
		if !info.Danger || info.ID != "service.stop:mariadb" || !slices.Equal(info.Commands, []string{"systemctl stop mariadb"}) {
			t.Errorf("Info = %+v", info)
		}
	})
}

func TestServiceBadUnit(t *testing.T) {
	ctx := context.Background()
	for _, unit := range []string{"", "-H", "nginx; rm -rf /", "$(id)", "../../etc/passwd", "nginx\nx", "nginx service"} {
		for _, s := range []engine.Step{&ServiceRestart{Unit: unit}, &ServiceStop{Unit: unit}} {
			t.Run(fmt.Sprintf("%T %q", s, unit), func(t *testing.T) {
				m := newM12(t)
				if _, err := s.Check(ctx, m); err == nil {
					t.Error("Check không lỗi")
				}
				if err := s.Apply(ctx, m); err == nil {
					t.Error("Apply không lỗi")
				}
				if len(m.Ran()) != 0 {
					t.Errorf("đã chạy: %v", ranStrings(m.Ran()))
				}
			})
		}
	}
}

// ================================================================== UserInGroup

func TestUserInGroup(t *testing.T) {
	ctx := context.Background()
	step := func() *UserInGroup { return &UserInGroup{User: "nginx", Group: "www-data"} }

	t.Run("chưa trong nhóm: Apply thêm, Undo bỏ ra", func(t *testing.T) {
		m := newM12(t)
		m.groups["nginx"] = []string{"nginx"}
		before := m.m12State()
		s := step()
		if done, err := s.Check(ctx, m); err != nil || done {
			t.Fatalf("Check trước = %v, %v", done, err)
		}
		if err := s.Apply(ctx, m); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if !ranHas(m, "usermod -aG www-data nginx") {
			t.Errorf("lệnh: %v", ranStrings(m.Ran()))
		}
		if done, err := s.Check(ctx, m); err != nil || !done {
			t.Fatalf("Check sau = %v, %v", done, err)
		}
		if err := s.Undo(ctx, m); err != nil {
			t.Fatalf("Undo: %v", err)
		}
		if !ranHas(m, "gpasswd -d nginx www-data") || !reflect.DeepEqual(before, m.m12State()) {
			t.Error("Undo không bỏ user ra khỏi nhóm")
		}
	})

	t.Run("đã trong nhóm thì bỏ qua", func(t *testing.T) {
		m := newM12(t)
		m.groups["nginx"] = []string{"nginx", "www-data"}
		rep := execute(t, m.machine, step())
		if rep.Steps[0].Outcome != "skipped" || ranHas(m, "usermod") {
			t.Fatalf("outcome %q", rep.Steps[0].Outcome)
		}
	})

	t.Run("nhóm tên gần giống không tính", func(t *testing.T) {
		m := newM12(t)
		m.groups["nginx"] = []string{"nginx", "www-data2"}
		if done, err := step().Check(ctx, m); err != nil || done {
			t.Fatalf("Check = %v, %v", done, err)
		}
	})

	t.Run("user chưa có: Check false, Apply lỗi có ngữ cảnh", func(t *testing.T) {
		m := newM12(t)
		if done, err := step().Check(ctx, m); err != nil || done {
			t.Fatalf("Check = %v, %v", done, err)
		}
		err := step().Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "nginx") || !strings.Contains(err.Error(), "does not exist") {
			t.Fatalf("lỗi = %v", err)
		}
	})

	t.Run("Undo khi chưa Apply thì lỗi", func(t *testing.T) {
		if err := step().Undo(ctx, newM12(t)); err == nil {
			t.Fatal("không lỗi")
		}
	})

	t.Run("Info", func(t *testing.T) {
		info := step().Info()
		if info.ID != "user.group:nginx:www-data" || info.Why.Vi == "" || len(info.Commands) != 1 {
			t.Errorf("Info = %+v", info)
		}
	})

	bad := []string{"", "-r", "root;id", "$(id)", "a b", "../../etc/passwd", "nginx\nroot", "Nginx", strings.Repeat("a", 300), "việt"}
	for _, b := range bad {
		for _, s := range []*UserInGroup{{User: b, Group: "www-data"}, {User: "nginx", Group: b}} {
			t.Run(fmt.Sprintf("tham số xấu %q/%q", s.User, s.Group), func(t *testing.T) {
				m := newM12(t)
				if _, err := s.Check(ctx, m); err == nil {
					t.Error("Check không lỗi")
				}
				if err := s.Apply(ctx, m); err == nil {
					t.Error("Apply không lỗi")
				}
				if len(m.Ran()) != 0 {
					t.Errorf("đã chạy: %v", ranStrings(m.Ran()))
				}
			})
		}
	}
}

// ================================================================= MariaDBDump

const dumpPath = "/var/lib/tpan/backups/mariadb-20261119-100000.sql"

func TestMariaDBDump(t *testing.T) {
	ctx := context.Background()
	step := func() *MariaDBDump { return &MariaDBDump{Path: dumpPath} }

	t.Run("máy sạch: tạo thư mục 0700, file 0600, Check true", func(t *testing.T) {
		m := newM12(t)
		m.SetDir("/var/lib", 0o755, system.Root)
		s := step()
		if done, err := s.Check(ctx, m); err != nil || done {
			t.Fatalf("Check trước = %v, %v", done, err)
		}
		if err := s.Apply(ctx, m); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		snap := m.Snapshot()
		if e := snap["/var/lib/tpan"]; e.Mode != fs.ModeDir|0o755 {
			t.Errorf("/var/lib/tpan = %v (acme bên trong phải đi qua được)", e.Mode)
		}
		if e := snap["/var/lib/tpan/backups"]; e.Mode != fs.ModeDir|0o700 || e.Owner != system.Root {
			t.Errorf("backups = %+v", e)
		}
		if e := snap[dumpPath]; e.Mode != 0o600 || e.Owner != system.Root || e.Data != m.dump {
			t.Errorf("file xuất = %+v", e)
		}
		want := "mariadb-dump --all-databases --single-transaction --routines --events --triggers --result-file=" + dumpPath
		if !ranHas(m, want) {
			t.Errorf("thiếu %q: %v", want, ranStrings(m.Ran()))
		}
		if done, err := s.Check(ctx, m); err != nil || !done {
			t.Fatalf("Check sau = %v, %v", done, err)
		}
	})

	t.Run("chạy lại thì không xuất lại", func(t *testing.T) {
		m := newM12(t)
		if rep := execute(t, m.machine, step()); rep.Job.Status != engine.JobSucceeded {
			t.Fatalf("lần đầu: %s", rep.Job.Error)
		}
		m.ResetRan()
		rep := execute(t, m.machine, step())
		if rep.Steps[0].Outcome != "skipped" || ranHas(m, "mariadb-dump") {
			t.Fatalf("outcome %q", rep.Steps[0].Outcome)
		}
	})

	t.Run("file xuất dở thì lỗi và gỡ file", func(t *testing.T) {
		m := newM12(t)
		m.dump = "-- MariaDB dump\nINSERT INTO t VALUES (1"
		err := step().Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), dumpPath) {
			t.Fatalf("lỗi = %v", err)
		}
		if _, ok := m.Snapshot()[dumpPath]; ok {
			t.Error("file dở còn lại")
		}
	})

	t.Run("mariadb-dump lỗi thì lỗi có ngữ cảnh và gỡ file", func(t *testing.T) {
		m := newM12(t)
		m.failCmd([]string{"mariadb-dump"}, 2, "mariadb-dump: Got error: 2002")
		err := step().Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "2002") {
			t.Fatalf("lỗi = %v", err)
		}
		if _, ok := m.Snapshot()[dumpPath]; ok {
			t.Error("file rỗng còn lại")
		}
	})

	t.Run("file có sẵn nhưng dở thì Check false", func(t *testing.T) {
		m := newM12(t)
		m.SetFile(dumpPath, []byte("-- MariaDB dump\n"), 0o600, system.Root)
		if done, err := step().Check(ctx, m); err != nil || done {
			t.Fatalf("Check = %v, %v", done, err)
		}
	})

	t.Run("Info", func(t *testing.T) {
		info := step().Info()
		if info.Danger || info.NoUndo.IsZero() || info.ID != "mariadb.dump:"+dumpPath {
			t.Errorf("Info = %+v", info)
		}
	})

	for _, p := range []string{"", "backup.sql", "/etc/x.sql", "/var/lib/tpan/backups", "/var/lib/tpan/backups/", "/var/lib/tpan/backups/../../../etc/shadow", "/var/lib/tpan/backupsX/a.sql", "/var/lib/tpan/backups/a\n.sql"} {
		t.Run(fmt.Sprintf("đường dẫn xấu %q", p), func(t *testing.T) {
			m := newM12(t)
			s := &MariaDBDump{Path: p}
			if _, err := s.Check(ctx, m); err == nil {
				t.Error("Check không lỗi")
			}
			if err := s.Apply(ctx, m); err == nil {
				t.Error("Apply không lỗi")
			}
			if len(m.Ran()) != 0 {
				t.Errorf("đã chạy: %v", ranStrings(m.Ran()))
			}
		})
	}
}

// =========================================================== MariaDBUpgradeRun

func TestMariaDBUpgradeRun(t *testing.T) {
	ctx := context.Background()

	t.Run("chưa nâng rồi Apply thì Check true", func(t *testing.T) {
		m := newM12(t)
		m.SetFile(mariadbUpgradeInfo, []byte("10.11.8-MariaDB"), 0o644, system.Root)
		s := &MariaDBUpgradeRun{}
		if done, err := s.Check(ctx, m); err != nil || done {
			t.Fatalf("Check trước = %v, %v", done, err)
		}
		if err := s.Apply(ctx, m); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if done, err := s.Check(ctx, m); err != nil || !done {
			t.Fatalf("Check sau = %v, %v", done, err)
		}
	})

	t.Run("chưa có file info thì Check false", func(t *testing.T) {
		if done, err := (&MariaDBUpgradeRun{}).Check(ctx, newM12(t)); err != nil || done {
			t.Fatalf("Check = %v, %v", done, err)
		}
	})

	t.Run("11.4.1 không khớp 11.4.10", func(t *testing.T) {
		m := newM12(t)
		m.dbVersion = "11.4.1-MariaDB"
		m.SetFile(mariadbUpgradeInfo, []byte("11.4.10-MariaDB"), 0o644, system.Root)
		if done, err := (&MariaDBUpgradeRun{}).Check(ctx, m); err != nil || done {
			t.Fatalf("Check = %v, %v", done, err)
		}
	})

	t.Run("đã nâng thì bỏ qua", func(t *testing.T) {
		m := newM12(t)
		m.SetFile(mariadbUpgradeInfo, []byte("11.4.4-MariaDB"), 0o644, system.Root)
		rep := execute(t, m.machine, &MariaDBUpgradeRun{})
		if rep.Steps[0].Outcome != "skipped" || ranHas(m, "mariadb-upgrade") {
			t.Fatalf("outcome %q", rep.Steps[0].Outcome)
		}
	})

	t.Run("lệnh lỗi thì lỗi có ngữ cảnh", func(t *testing.T) {
		m := newM12(t)
		m.failCmd([]string{"mariadb-upgrade"}, 1, "mariadb-upgrade: Got error: 1045")
		if err := (&MariaDBUpgradeRun{}).Apply(ctx, m); err == nil || !strings.Contains(err.Error(), "1045") {
			t.Fatalf("lỗi = %v", err)
		}
		m2 := newM12(t)
		m2.failCmd([]string{"mariadb", "-N", "-B", "-e"}, 1, "ERROR 2002 (HY000)")
		m2.SetFile(mariadbUpgradeInfo, []byte("11.4.4-MariaDB"), 0o644, system.Root)
		if _, err := (&MariaDBUpgradeRun{}).Check(ctx, m2); err == nil || !strings.Contains(err.Error(), "2002") {
			t.Fatalf("Check lỗi = %v", err)
		}
	})

	t.Run("Info", func(t *testing.T) {
		info := (&MariaDBUpgradeRun{}).Info()
		if info.NoUndo.IsZero() || info.ID != "mariadb.upgrade:localhost" || !slices.Contains(info.Commands, "mariadb-upgrade") {
			t.Errorf("Info = %+v", info)
		}
	})
}
