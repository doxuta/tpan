package steps

import (
	"context"
	"errors"
	"io/fs"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
)

const (
	hardenConfD = "/etc/php/8.3/fpm/conf.d"
	hardenINI   = hardenConfD + "/99-tpan.ini"
	dropInDir   = "/etc/systemd/system/php8.3-fpm.service.d"
	dropIn      = dropInDir + "/tpan.conf"
)

func iniStep(v string) *PHPHardeningINI { return &PHPHardeningINI{Version: &v} }
func procStep(v string) *PHPProtectProc { return &PHPProtectProc{Version: &v} }

// hardenFresh là máy ngay sau `apt-get install php8.3-fpm`: có thư mục conf.d
// của gói, có thư mục unit của systemd, dịch vụ đang chạy, chưa có file nào
// của M14.
func hardenFresh(m *machine) {
	m.SetDir(hardenConfD, 0o755, system.Root)
	m.SetDir("/etc/systemd/system", 0o755, system.Root)
	m.units["php8.3-fpm"] = unitState{Active: true, Enabled: true}
}

func hardenStopped(m *machine) {
	hardenFresh(m)
	m.units["php8.3-fpm"] = unitState{}
}

func countRan(m *machine, cmd string) int {
	n := 0
	for _, c := range ranStrings(m.Ran()) {
		if c == cmd {
			n++
		}
	}
	return n
}

// ============================================================ PHPHardeningINI

func TestPHPHardeningINILifecycle(t *testing.T) {
	cases := []lifecycle{
		{name: "máy mới cài", setup: hardenFresh, step: func() engine.Step { return iniStep("8.3") }},
		{name: "dịch vụ đang tắt", setup: hardenStopped, step: func() engine.Step { return iniStep("8.3") }},
		{
			// File cũ (bản tpan trước, hoặc ai đó sửa tay): Undo phải trả đúng
			// nội dung và quyền cũ chứ không xoá.
			name: "thay file cũ",
			setup: func(m *machine) {
				hardenFresh(m)
				m.SetFile(hardenINI, []byte("; bản cũ\nopcache.validate_permission=0\n"), 0o600, system.Root)
			},
			step: func() engine.Step { return iniStep("8.3") },
		},
		{
			name: "đúng nội dung nhưng sai quyền",
			setup: func(m *machine) {
				hardenFresh(m)
				m.SetFile(hardenINI, templates.PHPHardeningINI(), 0o600, system.Root)
			},
			step: func() engine.Step { return iniStep("8.3") },
		},
	}
	for _, lc := range cases {
		t.Run(lc.name, lc.run)
	}
}

func TestPHPHardeningINIApply(t *testing.T) {
	ctx := context.Background()

	t.Run("ghi file, kiểm cú pháp, rồi reload", func(t *testing.T) {
		m := newMachine(t)
		hardenFresh(m)
		if err := iniStep("8.3").Apply(ctx, m); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		e, ok := m.Snapshot()[hardenINI]
		if !ok || e.Mode != 0o644 || e.Owner != system.Root || e.Data != string(templates.PHPHardeningINI()) {
			t.Errorf("99-tpan.ini = %+v, %v", e, ok)
		}
		// Hai dòng đóng lỗ L1 và L2 phải thật sự có trong thứ được ghi.
		for _, line := range []string{"opcache.validate_permission=1", "opcache.restrict_api=" + templates.OPcacheAPIDir} {
			if !strings.Contains(e.Data, line) {
				t.Errorf("file thiếu dòng %q", line)
			}
		}
		got := ranStrings(m.Ran())
		want := []string{"php-fpm8.3 -t", "systemctl is-active php8.3-fpm", "systemctl reload php8.3-fpm"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("lệnh = %q, muốn %q", got, want)
		}
	})

	t.Run("dịch vụ tắt thì không reload", func(t *testing.T) {
		m := newMachine(t)
		hardenStopped(m)
		if err := iniStep("8.3").Apply(ctx, m); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		got := ranStrings(m.Ran())
		want := []string{"php-fpm8.3 -t", "systemctl is-active php8.3-fpm"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("lệnh = %q, muốn %q", got, want)
		}
	})

	t.Run("php-fpm -t lỗi thì trả file cũ, không reload", func(t *testing.T) {
		for name, setup := range map[string]func(*machine){
			"trước đó chưa có file": hardenFresh,
			"trước đó có file cũ": func(m *machine) {
				hardenFresh(m)
				m.SetFile(hardenINI, []byte("; bản cũ\n"), 0o600, system.Root)
			},
		} {
			t.Run(name, func(t *testing.T) {
				m := newMachine(t)
				setup(m)
				m.failCmd([]string{"php-fpm8.3", "-t"}, 78, "ERROR: failed to load configuration file")
				before := m.whole()
				err := iniStep("8.3").Apply(ctx, m)
				wantExit(t, err, "kiểm cấu hình sau khi ghi "+hardenINI)
				if !reflect.DeepEqual(before, m.whole()) {
					t.Error("Apply lỗi mà máy không về như cũ")
				}
				if got := ranStrings(m.Ran()); !reflect.DeepEqual(got, []string{"php-fpm8.3 -t"}) {
					t.Errorf("lệnh = %q, muốn chỉ lệnh kiểm", got)
				}
			})
		}
	})

	t.Run("reload lỗi thì trả file cũ và reload lại", func(t *testing.T) {
		m := newMachine(t)
		hardenFresh(m)
		m.failCmd([]string{"systemctl", "reload", "php8.3-fpm"}, 1, "Job for php8.3-fpm.service failed")
		before := m.whole()
		err := iniStep("8.3").Apply(ctx, m)
		wantExit(t, err, "nạp lại dịch vụ")
		if !reflect.DeepEqual(before, m.whole()) {
			t.Error("Apply lỗi mà máy không về như cũ")
		}
		if n := countRan(m, "systemctl reload php8.3-fpm"); n != 2 {
			t.Errorf("reload %d lần, muốn 2 (lần đổi và lần trả về)", n)
		}
	})

	t.Run("ghi lỗi thì báo có ngữ cảnh, không chạy lệnh", func(t *testing.T) {
		m := newMachine(t)
		hardenFresh(m)
		m.Fail("write", hardenINI, errors.New("đĩa đầy"))
		before := m.whole()
		err := iniStep("8.3").Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "đĩa đầy") || !strings.Contains(err.Error(), hardenINI) {
			t.Fatalf("Apply = %v, muốn lỗi ghi kèm đường dẫn", err)
		}
		if len(m.Ran()) != 0 || !reflect.DeepEqual(before, m.whole()) {
			t.Errorf("máy đổi hoặc đã chạy lệnh: %q", ranStrings(m.Ran()))
		}
	})

	t.Run("Undo kiểm cú pháp rồi reload", func(t *testing.T) {
		m := newMachine(t)
		hardenFresh(m)
		s := iniStep("8.3")
		if err := s.Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
		m.ResetRan()
		if err := s.Undo(ctx, m); err != nil {
			t.Fatalf("Undo: %v", err)
		}
		got := ranStrings(m.Ran())
		want := []string{"php-fpm8.3 -t", "systemctl is-active php8.3-fpm", "systemctl reload php8.3-fpm"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("lệnh của Undo = %q, muốn %q", got, want)
		}
	})
}

// ============================================================= PHPProtectProc

func TestPHPProtectProcLifecycle(t *testing.T) {
	cases := []lifecycle{
		{name: "máy mới cài", setup: hardenFresh, step: func() engine.Step { return procStep("8.3") }},
		{name: "dịch vụ đang tắt", setup: hardenStopped, step: func() engine.Step { return procStep("8.3") }},
		{
			// Thư mục drop-in đã có (người vận hành đặt override.conf riêng) và
			// tpan.conf là bản cũ: Undo trả đúng bản cũ, không xoá thư mục.
			name: "thay drop-in cũ trong thư mục có sẵn",
			setup: func(m *machine) {
				hardenFresh(m)
				m.SetFile(dropInDir+"/override.conf", []byte("[Service]\nLimitNOFILE=65535\n"), 0o644, system.Root)
				m.SetFile(dropIn, []byte("[Service]\nProtectProc=default\n"), 0o644, system.Root)
				m.daemonReload()
			},
			step: func() engine.Step { return procStep("8.3") },
		},
	}
	for _, lc := range cases {
		t.Run(lc.name, lc.run)
	}
}

func TestPHPProtectProcApply(t *testing.T) {
	ctx := context.Background()
	const (
		daemonReload = "systemctl daemon-reload"
		isActive     = "systemctl is-active php8.3-fpm"
		test         = "php-fpm8.3 -t"
		restart      = "systemctl restart php8.3-fpm"
	)

	t.Run("ghi drop-in, daemon-reload, kiểm cú pháp, rồi restart", func(t *testing.T) {
		m := newMachine(t)
		hardenFresh(m)
		if err := procStep("8.3").Apply(ctx, m); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		snap := m.Snapshot()
		if e := snap[dropInDir]; e.Mode != fs.ModeDir|0o755 || e.Owner != system.Root {
			t.Errorf("thư mục drop-in = %+v", e)
		}
		e, ok := snap[dropIn]
		if !ok || e.Mode != 0o644 || e.Owner != system.Root || e.Data != string(templates.PHPDropIn()) {
			t.Errorf("tpan.conf = %+v, %v", e, ok)
		}
		if !strings.Contains(e.Data, "[Service]\nProtectProc=invisible\n") {
			t.Errorf("drop-in thiếu ProtectProc=invisible:\n%s", e.Data)
		}
		got := ranStrings(m.Ran())
		want := []string{daemonReload, isActive, test, restart}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("lệnh = %q, muốn %q", got, want)
		}
	})

	t.Run("dịch vụ tắt thì chỉ daemon-reload, không restart", func(t *testing.T) {
		m := newMachine(t)
		hardenStopped(m)
		if err := procStep("8.3").Apply(ctx, m); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		got := ranStrings(m.Ran())
		if want := []string{daemonReload, isActive}; !reflect.DeepEqual(got, want) {
			t.Errorf("lệnh = %q, muốn %q", got, want)
		}
	})

	t.Run("daemon-reload lỗi thì gỡ file và thư mục vừa tạo", func(t *testing.T) {
		m := newMachine(t)
		hardenFresh(m)
		m.failCmd([]string{"systemctl", "daemon-reload"}, 1, "Failed to reload daemon")
		before := m.whole()
		err := procStep("8.3").Apply(ctx, m)
		wantExit(t, err, "nạp lại cấu hình systemd sau khi ghi "+dropIn)
		if !reflect.DeepEqual(before, m.whole()) {
			t.Error("Apply lỗi mà máy không về như cũ")
		}
		if slices.Contains(ranStrings(m.Ran()), restart) {
			t.Error("restart sau khi daemon-reload lỗi")
		}
	})

	t.Run("php-fpm -t lỗi thì không restart, trả drop-in cũ", func(t *testing.T) {
		m := newMachine(t)
		hardenFresh(m)
		m.SetFile(dropIn, []byte("[Service]\nProtectProc=default\n"), 0o644, system.Root)
		m.daemonReload()
		m.failCmd([]string{"php-fpm8.3", "-t"}, 78, "ERROR: No pool defined")
		before := m.whole()
		err := procStep("8.3").Apply(ctx, m)
		wantExit(t, err, "kiểm cấu hình sau khi ghi "+dropIn)
		if !reflect.DeepEqual(before, m.whole()) {
			t.Error("Apply lỗi mà máy không về như cũ")
		}
		// Restart một PHP-FPM có config hỏng là tắt mọi site của bản này.
		if slices.Contains(ranStrings(m.Ran()), restart) {
			t.Error("restart dù php-fpm -t lỗi")
		}
		if n := countRan(m, daemonReload); n != 2 {
			t.Errorf("daemon-reload %d lần, muốn 2 (lần đổi và lần trả về)", n)
		}
	})

	t.Run("restart lỗi thì trả máy về và restart lại bằng cấu hình cũ", func(t *testing.T) {
		m := newMachine(t)
		hardenFresh(m)
		m.failCmd([]string{"systemctl", "restart", "php8.3-fpm"}, 1, "Job for php8.3-fpm.service failed")
		before := m.whole()
		err := procStep("8.3").Apply(ctx, m)
		wantExit(t, err, "khởi động lại php8.3-fpm sau khi ghi "+dropIn)
		if !reflect.DeepEqual(before, m.whole()) {
			t.Error("Apply lỗi mà máy không về như cũ")
		}
		if n := countRan(m, daemonReload); n != 2 {
			t.Errorf("daemon-reload %d lần, muốn 2", n)
		}
		if n := countRan(m, restart); n != 2 {
			t.Errorf("restart %d lần, muốn 2 (lần đổi và lần trả về)", n)
		}
	})

	t.Run("ghi lỗi thì gỡ thư mục vừa tạo, không chạy lệnh", func(t *testing.T) {
		m := newMachine(t)
		hardenFresh(m)
		m.Fail("write", dropIn, errors.New("đĩa đầy"))
		before := m.whole()
		err := procStep("8.3").Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "đĩa đầy") || !strings.Contains(err.Error(), dropIn) {
			t.Fatalf("Apply = %v, muốn lỗi ghi kèm đường dẫn", err)
		}
		if len(m.Ran()) != 0 || !reflect.DeepEqual(before, m.whole()) {
			t.Errorf("máy đổi hoặc đã chạy lệnh: %q", ranStrings(m.Ran()))
		}
	})

	t.Run("chỗ thư mục drop-in là một file thì từ chối", func(t *testing.T) {
		m := newMachine(t)
		hardenFresh(m)
		m.SetFile(dropInDir, []byte("lạ"), 0o644, system.Root)
		before := m.whole()
		if _, err := procStep("8.3").Check(ctx, m); err == nil {
			t.Error("Check không lỗi")
		}
		if err := procStep("8.3").Apply(ctx, m); err == nil {
			t.Error("Apply không lỗi")
		}
		if len(m.Ran()) != 0 || !reflect.DeepEqual(before, m.whole()) {
			t.Errorf("máy đổi hoặc đã chạy lệnh: %q", ranStrings(m.Ran()))
		}
	})

	t.Run("Undo gỡ drop-in, daemon-reload, kiểm rồi restart", func(t *testing.T) {
		m := newMachine(t)
		hardenFresh(m)
		s := procStep("8.3")
		if err := s.Apply(ctx, m); err != nil {
			t.Fatal(err)
		}
		m.ResetRan()
		if err := s.Undo(ctx, m); err != nil {
			t.Fatalf("Undo: %v", err)
		}
		got := ranStrings(m.Ran())
		if want := []string{daemonReload, isActive, test, restart}; !reflect.DeepEqual(got, want) {
			t.Errorf("lệnh của Undo = %q, muốn %q", got, want)
		}
		if _, ok := m.Snapshot()[dropInDir]; ok {
			t.Error("thư mục drop-in do bước tạo vẫn còn sau Undo")
		}
	})
}

func TestPHPProtectProcCheck(t *testing.T) {
	ctx := context.Background()
	show := "systemctl show php8.3-fpm -p ProtectProc --value"

	t.Run("file đúng nhưng systemd chưa nhận thì chưa xong", func(t *testing.T) {
		m := newMachine(t)
		hardenFresh(m)
		// File đặt tay, chưa daemon-reload: máy giả trả "default".
		m.SetFile(dropIn, templates.PHPDropIn(), 0o644, system.Root)
		s := procStep("8.3")
		if done, err := s.Check(ctx, m); err != nil || done {
			t.Fatalf("Check = %v, %v; muốn false, nil", done, err)
		}
		if got := ranStrings(m.Ran()); !reflect.DeepEqual(got, []string{"systemctl is-active php8.3-fpm", show}) {
			t.Errorf("lệnh của Check = %q", got)
		}
		m.ResetRan()
		if err := s.Apply(ctx, m); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if !slices.Contains(ranStrings(m.Ran()), "systemctl restart php8.3-fpm") {
			t.Error("Apply không restart dịch vụ đang chạy")
		}
		if done, err := s.Check(ctx, m); err != nil || !done {
			t.Fatalf("Check sau Apply = %v, %v; muốn true, nil", done, err)
		}
		// Undo không được xoá file có từ trước khi bước chạy.
		if err := s.Undo(ctx, m); err != nil {
			t.Fatalf("Undo: %v", err)
		}
		if e, ok := m.Snapshot()[dropIn]; !ok || e.Data != string(templates.PHPDropIn()) {
			t.Errorf("Undo làm mất drop-in có từ trước: %+v, %v", e, ok)
		}
	})

	t.Run("dịch vụ tắt thì chỉ xét file, không hỏi ProtectProc", func(t *testing.T) {
		m := newMachine(t)
		hardenStopped(m)
		m.SetFile(dropIn, templates.PHPDropIn(), 0o644, system.Root)
		if done, err := procStep("8.3").Check(ctx, m); err != nil || !done {
			t.Fatalf("Check = %v, %v; muốn true, nil", done, err)
		}
		if slices.Contains(ranStrings(m.Ran()), show) {
			t.Error("hỏi ProtectProc của dịch vụ đang tắt")
		}
	})

	t.Run("chưa có file thì không chạy lệnh nào", func(t *testing.T) {
		m := newMachine(t)
		hardenFresh(m)
		if done, err := procStep("8.3").Check(ctx, m); err != nil || done {
			t.Fatalf("Check = %v, %v; muốn false, nil", done, err)
		}
		if len(m.Ran()) != 0 {
			t.Errorf("đã chạy lệnh: %q", ranStrings(m.Ran()))
		}
	})

	t.Run("systemctl show lỗi thì báo có ngữ cảnh", func(t *testing.T) {
		m := newMachine(t)
		hardenFresh(m)
		m.SetFile(dropIn, templates.PHPDropIn(), 0o644, system.Root)
		m.failCmd([]string{"systemctl", "show"}, 1, "Failed to connect to bus")
		_, err := procStep("8.3").Check(ctx, m)
		wantExit(t, err, "hỏi systemd ProtectProc của php8.3-fpm")
	})
}

// ================================================================ dùng chung

func TestPHPHardeningBadParams(t *testing.T) {
	ctx := context.Background()
	str := func(s string) *string { return &s }
	cases := map[string]*string{
		"Version nil":   nil,
		"Version rỗng":  str(""),
		"chèn lệnh":     str("8.3; rm -rf /"),
		"thoát thư mục": str("../8.3"),
		"thay lệnh":     str("$(id)"),
		"xuống dòng":    str("8.3\n"),
		"tuỳ chọn":      str("-t"),
		"dài 300 ký tự": str(strings.Repeat("8", 300)),
		"ba phần":       str("8.3.1"),
	}
	for name, v := range cases {
		for _, s := range []engine.Step{&PHPHardeningINI{Version: v}, &PHPProtectProc{Version: v}} {
			t.Run(name+"/"+reflect.TypeOf(s).Elem().Name(), func(t *testing.T) {
				m := newMachine(t)
				hardenFresh(m)
				before := m.whole()
				if _, err := s.Check(ctx, m); err == nil {
					t.Error("Check không lỗi")
				}
				if err := s.Apply(ctx, m); err == nil {
					t.Error("Apply không lỗi")
				}
				if len(m.Ran()) != 0 {
					t.Errorf("đã chạy lệnh: %q", ranStrings(m.Ran()))
				}
				if !reflect.DeepEqual(before, m.whole()) {
					t.Error("máy bị đổi")
				}
				// Info của bước có tham số sai không được khai file theo đường
				// dẫn ghép từ tham số đó.
				if info := s.Info(); len(info.Files) != 0 {
					t.Errorf("Info khai Files với bản sai: %+v", info.Files)
				}
			})
		}
		if v != nil {
			if ss, err := PHPHardening(*v); err == nil {
				t.Errorf("PHPHardening(%q) = %d bước, muốn lỗi", *v, len(ss))
			}
		}
	}
}

func TestPHPHardening(t *testing.T) {
	ss, err := PHPHardening("8.3")
	if err != nil {
		t.Fatal(err)
	}
	if len(ss) != 2 {
		t.Fatalf("%d bước, muốn 2", len(ss))
	}
	ini, ok1 := ss[0].(*PHPHardeningINI)
	proc, ok2 := ss[1].(*PHPProtectProc)
	if !ok1 || !ok2 || *ini.Version != "8.3" || *proc.Version != "8.3" {
		t.Fatalf("thứ tự hoặc bản sai: %T, %T", ss[0], ss[1])
	}

	// Cả hai bước chạy liền nhau trên máy mới cài, rồi chạy lại thì bỏ qua hết.
	m := newMachine(t)
	hardenFresh(m)
	if rep := execute(t, m, ss...); rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("lần đầu: %s %s", rep.Job.Status, rep.Job.Error)
	}
	ss, _ = PHPHardening("8.3")
	m.ResetRan()
	for _, r := range execute(t, m, ss...).Steps {
		if r.Outcome != "skipped" {
			t.Errorf("lần hai %s: %s %s", r.ID, r.Outcome, r.Error)
		}
	}
	for _, c := range m.Ran() {
		if !readOnly(c) {
			t.Errorf("lần hai chạy lệnh thay đổi máy: %s", c.String())
		}
	}
}

func TestPHPHardeningInfo(t *testing.T) {
	// Lúc xem trước Plan cài stack, DetectPHP chưa chạy: Info không khai file
	// theo đường dẫn chưa biết bản.
	for _, s := range []engine.Step{&PHPHardeningINI{Version: new(string)}, &PHPProtectProc{Version: new(string)}} {
		info := s.Info()
		if !strings.Contains(info.Title.Vi, "php<bản>-fpm") || len(info.Files) != 0 {
			t.Errorf("%T chưa biết bản: Title %q, Files %v", s, info.Title.Vi, info.Files)
		}
	}

	ini := iniStep("8.3").Info()
	if len(ini.Files) != 1 || ini.Files[0].Path != hardenINI || string(ini.Files[0].New) != string(templates.PHPHardeningINI()) {
		t.Errorf("Files của ini = %+v", ini.Files)
	}
	if ini.Danger {
		t.Error("ghi file ini chỉ reload, không phải Danger")
	}
	proc := procStep("8.3").Info()
	if len(proc.Files) != 1 || proc.Files[0].Path != dropIn || string(proc.Files[0].New) != string(templates.PHPDropIn()) {
		t.Errorf("Files của drop-in = %+v", proc.Files)
	}
	// Restart ngắt request PHP đang chạy: phải hỏi lại và phải nói ra.
	if !proc.Danger || !strings.Contains(proc.Why.Vi, "khởi động lại") || !strings.Contains(proc.Why.En, "restart") {
		t.Errorf("drop-in: Danger %v, Why %q", proc.Danger, proc.Why.Vi)
	}
	if !slices.Contains(proc.Commands, "systemctl restart php8.3-fpm") || !slices.Contains(proc.Commands, "systemctl daemon-reload") {
		t.Errorf("Commands của drop-in = %q", proc.Commands)
	}
	if ini.ID == proc.ID {
		t.Errorf("hai bước trùng ID %q", ini.ID)
	}
	for _, info := range []engine.StepInfo{ini, proc} {
		if info.ID == "" || !strings.HasSuffix(info.ID, ":8.3") || info.Title.Vi == "" || info.Title.En == "" ||
			info.Why.Vi == "" || info.Why.En == "" || len(info.Commands) == 0 {
			t.Errorf("Info thiếu: %+v", info)
		}
	}

	for _, s := range []engine.Step{iniStep("8.3"), procStep("8.3")} {
		if err := s.Undo(context.Background(), newMachine(t)); !errors.Is(err, errNotApplied) {
			t.Errorf("%T: Undo khi chưa Apply = %v", s, err)
		}
	}
}
