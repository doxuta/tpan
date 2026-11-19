package steps

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/system"
)

// ================================================================= máy giả M10

// m10Handlers thêm vào máy giả những lệnh M10 cần mà newMachine chưa giả đúng:
//
//   - `apt-get install -y -o ... <gói>`: gói được cài, unit cùng tên luôn
//     enabled (đúng như đã kiểm ở M1: policy-rc.d không chặn enable), và chỉ
//     active khi lúc cài KHÔNG có policy-rc.d của tpan. Nhờ vậy test chứng minh
//     file thật sự nằm đó trong lúc apt chạy.
//   - systemctl start/stop/enable/disable với một hoặc nhiều unit, có hoặc không
//     --now (handler chung của newMachine chỉ đọc unit cuối, và "enable" luôn
//     bật cả active).
func m10Handlers(m *machine) {
	m.SetDir("/usr/sbin", 0o755, system.Root)
	ok := system.Result{}
	m.Handle([]string{"apt-get", "install", "-y", "-o"}, func(c system.Cmd) system.Result {
		data, err := m.ReadFile(policyRcPath)
		blocked := err == nil && strings.Contains(string(data), policyRcMarker) && strings.Contains(string(data), "exit 101")
		m.mu.Lock()
		defer m.mu.Unlock()
		for _, p := range c.Args[4:] { // bỏ "install -y -o DPkg::Lock::Timeout=300"
			m.pkgs[p] = true
			m.units[p] = unitState{Enabled: true, Active: !blocked}
		}
		return ok
	})
	for _, verb := range []string{"start", "stop", "enable", "disable"} {
		m.Handle([]string{"systemctl", verb}, func(c system.Cmd) system.Result {
			m.mu.Lock()
			defer m.mu.Unlock()
			now := slices.Contains(c.Args, "--now")
			for _, unit := range c.Args[1:] {
				if unit == "--now" {
					continue
				}
				u := m.units[unit]
				switch verb {
				case "start":
					u.Active = true
				case "stop":
					u.Active = false
				case "enable":
					u.Enabled, u.Active = true, u.Active || now
				case "disable":
					u.Enabled, u.Active = false, u.Active && !now
				}
				m.units[unit] = u
			}
			return ok
		})
	}
}

func newM10(t *testing.T) *m12Machine {
	t.Helper()
	m := newM12(t)
	m10Handlers(m.machine)
	return m
}

// lemp: máy đang chạy nginx, apache2 chưa cài.
func lemp(m *machine) {
	m.pkgs["nginx"] = true
	m.units["nginx"] = unitState{Active: true, Enabled: true}
	m.units["apache2"] = unitState{}
}

func ranCmd(m *m12Machine, cmd string) bool {
	return slices.Contains(ranStrings(m.Ran()), cmd)
}

// ========================================================== AptInstallNoStart

func TestAptInstallNoStartLifecycle(t *testing.T) {
	lifecycle{
		name:  "AptInstallNoStart",
		setup: func(m *machine) { m10Handlers(m); lemp(m) },
		step: func() engine.Step {
			return &AptInstallNoStart{Packages: []string{"apache2"}, Disable: []string{"apache2"}}
		},
		noUndo: true,
	}.run(t)
}

func TestAptInstallNoStart(t *testing.T) {
	ctx := context.Background()
	step := func() *AptInstallNoStart {
		return &AptInstallNoStart{Packages: []string{"apache2"}, Disable: []string{"apache2"}}
	}

	t.Run("cài xong: apache2 tắt, không bật khi khởi động, policy-rc.d đã gỡ", func(t *testing.T) {
		m := newM10(t)
		lemp(m.machine)
		before := m.Snapshot()
		if err := step().Apply(ctx, m); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if got := m.units["apache2"]; got.Active || got.Enabled {
			t.Errorf("apache2 = %+v, muốn tắt và không enabled", got)
		}
		if !reflect.DeepEqual(before, m.Snapshot()) {
			t.Error("hệ thống file khác trước Apply (policy-rc.d còn sót?)")
		}
		got := ranStrings(m.Ran())
		i := slices.Index(got, "apt-get install -y -o DPkg::Lock::Timeout=300 apache2")
		j := slices.Index(got, "systemctl disable --now apache2")
		if i < 0 || j < i {
			t.Errorf("thứ tự lệnh: %v", got)
		}
	})

	t.Run("apt-get lỗi: policy-rc.d vẫn được gỡ, lỗi có ngữ cảnh", func(t *testing.T) {
		m := newM10(t)
		lemp(m.machine)
		before := m.Snapshot()
		m.failCmd([]string{"apt-get", "install", "-y", "-o", "DPkg::Lock::Timeout=300"}, 100, "E: Unable to locate package apache2")
		err := step().Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "apache2") || !strings.Contains(err.Error(), "Unable to locate") {
			t.Fatalf("lỗi = %v", err)
		}
		if !reflect.DeepEqual(before, m.Snapshot()) {
			t.Error("policy-rc.d còn sót sau khi apt-get lỗi")
		}
	})

	t.Run("apt-get lỗi giữa chừng mà unit đã enabled: vẫn tắt", func(t *testing.T) {
		m := newM10(t)
		lemp(m.machine)
		m.Handle([]string{"apt-get", "install", "-y", "-o", "DPkg::Lock::Timeout=300"}, func(system.Cmd) system.Result {
			m.mu.Lock()
			defer m.mu.Unlock()
			m.units["apache2"] = unitState{Enabled: true} // postinst đã enable rồi mới lỗi
			return system.Result{ExitCode: 100, Stderr: "E: Sub-process /usr/bin/dpkg returned an error code (1)"}
		})
		if err := step().Apply(ctx, m); err == nil {
			t.Fatal("không lỗi")
		}
		if got := m.units["apache2"]; got.Enabled || got.Active {
			t.Errorf("apache2 = %+v, muốn tắt", got)
		}
	})

	t.Run("policy-rc.d có sẵn của người khác: trả lại y nguyên, cả khi apt-get lỗi", func(t *testing.T) {
		for _, aptFails := range []bool{false, true} {
			t.Run(fmt.Sprint("apt lỗi=", aptFails), func(t *testing.T) {
				m := newM10(t)
				lemp(m.machine)
				m.SetFile(policyRcPath, []byte("#!/bin/sh\n# của quản trị viên\nexit 0\n"), 0o700, system.Owner{User: "root", Group: "adm"})
				before := m.Snapshot()[policyRcPath]
				var during string
				m.Handle([]string{"apt-get", "install", "-y", "-o", "DPkg::Lock::Timeout=300"}, func(system.Cmd) system.Result {
					d, _ := m.ReadFile(policyRcPath)
					during = string(d)
					if aptFails {
						return system.Result{ExitCode: 100}
					}
					return system.Result{}
				})
				err := step().Apply(ctx, m)
				if (err != nil) != aptFails {
					t.Fatalf("Apply = %v", err)
				}
				if !strings.Contains(during, "exit 101") {
					t.Errorf("trong lúc apt chạy policy-rc.d = %q", during)
				}
				if after := m.Snapshot()[policyRcPath]; !reflect.DeepEqual(before, after) {
					t.Errorf("policy-rc.d không về như cũ:\ntrước %+v\nsau   %+v", before, after)
				}
			})
		}
	})

	t.Run("policy-rc.d của tpan sót từ lần trước: gỡ đi", func(t *testing.T) {
		m := newM10(t)
		lemp(m.machine)
		before := m.Snapshot()
		m.SetFile(policyRcPath, policyRcData, 0o755, system.Root)
		// Gói đã cài, unit đã tắt mà file sót vẫn còn: Check phải chưa đạt,
		// không thì bước bị bỏ qua và file không bao giờ được gỡ.
		installed := newM10(t)
		installed.pkgs["apache2"] = true
		installed.units["apache2"] = unitState{}
		installed.SetFile(policyRcPath, policyRcData, 0o755, system.Root)
		if done, err := step().Check(ctx, installed); err != nil || done {
			t.Errorf("Check khi còn policy-rc.d sót = %v, %v; muốn false, nil", done, err)
		}
		if err := step().Apply(ctx, m); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if !reflect.DeepEqual(before, m.Snapshot()) {
			t.Error("policy-rc.d sót của tpan chưa được gỡ")
		}
		if got := m.units["apache2"]; got.Active {
			t.Error("apache2 chạy: lúc cài policy-rc.d không có tác dụng")
		}
	})

	t.Run("ghi policy-rc.d lỗi thì không chạy apt-get", func(t *testing.T) {
		m := newM10(t)
		lemp(m.machine)
		m.Fail("write", policyRcPath, fmt.Errorf("đĩa đầy"))
		err := step().Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "policy-rc.d") {
			t.Fatalf("lỗi = %v", err)
		}
		if ranHas(m, "apt-get") {
			t.Error("vẫn chạy apt-get khi không chặn được dịch vụ tự khởi động")
		}
	})

	t.Run("disable lỗi thì lỗi có ngữ cảnh", func(t *testing.T) {
		m := newM10(t)
		lemp(m.machine)
		m.failCmd([]string{"systemctl", "disable", "--now"}, 1, "Failed to disable unit")
		err := step().Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "apache2") || !strings.Contains(err.Error(), "Failed to disable") {
			t.Fatalf("lỗi = %v", err)
		}
		if _, err := m.ReadFile(policyRcPath); err == nil {
			t.Error("policy-rc.d còn sót")
		}
	})

	t.Run("Check: gói đã cài nhưng unit còn enabled thì chưa xong", func(t *testing.T) {
		m := newM10(t)
		lemp(m.machine)
		m.pkgs["apache2"] = true
		m.units["apache2"] = unitState{Enabled: true}
		if done, err := step().Check(ctx, m); err != nil || done {
			t.Fatalf("Check = %v, %v", done, err)
		}
	})

	t.Run("Info", func(t *testing.T) {
		info := step().Info()
		if info.ID != "apt.install-nostart:apache2" || info.NoUndo.IsZero() || len(info.Files) != 0 || len(info.Commands) != 4 ||
			!slices.Contains(info.Commands, "systemctl disable --now apache2") {
			t.Errorf("Info = %+v", info)
		}
	})

	t.Run("tham số sai thì không chạy lệnh nào", func(t *testing.T) {
		bad := []*AptInstallNoStart{
			{Packages: nil, Disable: []string{"apache2"}},
			{Packages: []string{"apache2", "libapache2-mod-php8.3"}, Disable: []string{"apache2"}},
			{Packages: []string{"php8.3"}},
		}
		for _, attack := range []string{"", "-o", "a.com; rm -rf /", "$(id)", "../../etc/passwd", "apache2\nx", strings.Repeat("a", 300) + " b"} {
			bad = append(bad,
				&AptInstallNoStart{Packages: []string{attack}, Disable: []string{"apache2"}},
				&AptInstallNoStart{Packages: []string{"apache2"}, Disable: []string{attack}})
		}
		for _, s := range bad {
			t.Run(fmt.Sprintf("%q %q", s.Packages, s.Disable), func(t *testing.T) {
				m := newM10(t)
				before := m.Snapshot()
				if _, err := s.Check(ctx, m); err == nil {
					t.Error("Check không lỗi")
				}
				if err := s.Apply(ctx, m); err == nil {
					t.Error("Apply không lỗi")
				}
				if len(m.Ran()) != 0 || !reflect.DeepEqual(before, m.Snapshot()) {
					t.Errorf("đã đổi máy: %v", ranStrings(m.Ran()))
				}
			})
		}
	})
}

// ===================================================================== WebSwap

func TestWebSwapLifecycle(t *testing.T) {
	lifecycle{
		name: "WebSwap",
		setup: func(m *machine) {
			m10Handlers(m)
			lemp(m)
			m.pkgs["apache2"] = true
		},
		step: func() engine.Step { return &WebSwap{From: "nginx", To: "apache2"} },
	}.run(t)
}

func TestWebSwap(t *testing.T) {
	ctx := context.Background()
	setup := func(t *testing.T) *m12Machine {
		m := newM10(t)
		lemp(m.machine)
		return m
	}

	t.Run("dừng cũ trước rồi mới chạy mới", func(t *testing.T) {
		m := setup(t)
		if err := (&WebSwap{From: "nginx", To: "apache2"}).Apply(ctx, m); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		got := ranStrings(m.Ran())
		if i, j := slices.Index(got, "systemctl stop nginx"), slices.Index(got, "systemctl start apache2"); i < 0 || j < i {
			t.Errorf("thứ tự lệnh: %v", got)
		}
	})

	t.Run("start mới lỗi thì bật lại cũ", func(t *testing.T) {
		m := setup(t)
		m.failCmd([]string{"systemctl", "start", "apache2"}, 1, "Job for apache2.service failed")
		err := (&WebSwap{From: "nginx", To: "apache2"}).Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "apache2") || !strings.Contains(err.Error(), "Job for") {
			t.Fatalf("lỗi = %v", err)
		}
		if !m.units["nginx"].Active {
			t.Error("nginx không được bật lại")
		}
	})

	t.Run("start mới lỗi và bật lại cũ cũng lỗi thì gộp hai lỗi", func(t *testing.T) {
		m := setup(t)
		m.failCmd([]string{"systemctl", "start", "apache2"}, 1, "apache2 lỗi")
		m.failCmd([]string{"systemctl", "start", "nginx"}, 1, "nginx lỗi")
		err := (&WebSwap{From: "nginx", To: "apache2"}).Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "apache2 lỗi") || !strings.Contains(err.Error(), "nginx lỗi") {
			t.Fatalf("lỗi = %v", err)
		}
	})

	t.Run("stop cũ lỗi thì không chạy mới, bật lại cũ", func(t *testing.T) {
		m := setup(t)
		m.failCmd([]string{"systemctl", "stop", "nginx"}, 1, "Job for nginx.service canceled")
		err := (&WebSwap{From: "nginx", To: "apache2"}).Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "canceled") {
			t.Fatalf("lỗi = %v", err)
		}
		if ranCmd(m, "systemctl start apache2") || !ranCmd(m, "systemctl start nginx") {
			t.Errorf("lệnh: %v", ranStrings(m.Ran()))
		}
	})

	t.Run("hỏng giữa chừng lần trước (cũ đã dừng): chạy lại hội tụ", func(t *testing.T) {
		m := setup(t)
		m.units["nginx"] = unitState{Enabled: true}
		s := &WebSwap{From: "nginx", To: "apache2"}
		if done, err := s.Check(ctx, m); err != nil || done {
			t.Fatalf("Check = %v, %v", done, err)
		}
		if err := s.Apply(ctx, m); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if done, err := s.Check(ctx, m); err != nil || !done {
			t.Fatalf("Check sau = %v, %v", done, err)
		}
	})

	t.Run("Undo khi chưa Apply thì lỗi", func(t *testing.T) {
		if err := (&WebSwap{From: "nginx", To: "apache2"}).Undo(ctx, setup(t)); err == nil {
			t.Fatal("không lỗi")
		}
	})

	t.Run("Info", func(t *testing.T) {
		info := (&WebSwap{From: "nginx", To: "apache2"}).Info()
		if !info.Danger || info.ID != "web.swap:nginx:apache2" ||
			!slices.Equal(info.Commands, []string{"systemctl stop nginx", "systemctl start apache2"}) {
			t.Errorf("Info = %+v", info)
		}
	})

	t.Run("tham số sai thì không chạy lệnh nào", func(t *testing.T) {
		bad := []*WebSwap{{From: "nginx", To: "nginx"}}
		for _, attack := range []string{"", "-H", "nginx; rm -rf /", "$(id)", "../../etc/passwd", "nginx\nx"} {
			bad = append(bad, &WebSwap{From: attack, To: "apache2"}, &WebSwap{From: "nginx", To: attack})
		}
		for _, s := range bad {
			t.Run(fmt.Sprintf("%q→%q", s.From, s.To), func(t *testing.T) {
				m := newM10(t)
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
	})
}

// ============================================================== ServiceDisable

func TestServiceDisableLifecycle(t *testing.T) {
	lifecycle{
		name: "ServiceDisable",
		setup: func(m *machine) {
			m10Handlers(m)
			lemp(m)
			m.units["nginx"] = unitState{Enabled: true} // đã dừng ở WebSwap
		},
		step: func() engine.Step { return &ServiceDisable{Unit: "nginx"} },
	}.run(t)
}

func TestServiceDisable(t *testing.T) {
	ctx := context.Background()

	t.Run("không dừng dịch vụ đang chạy", func(t *testing.T) {
		m := newM10(t)
		lemp(m.machine)
		if err := (&ServiceDisable{Unit: "nginx"}).Apply(ctx, m); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if got := m.units["nginx"]; got.Enabled || !got.Active {
			t.Errorf("nginx = %+v", got)
		}
	})

	t.Run("vốn không enabled thì Undo không enable", func(t *testing.T) {
		m := newM10(t)
		lemp(m.machine)
		m.units["nginx"] = unitState{}
		s := &ServiceDisable{Unit: "nginx"}
		if err := s.Apply(ctx, m); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		m.ResetRan()
		if err := s.Undo(ctx, m); err != nil {
			t.Fatalf("Undo: %v", err)
		}
		if len(m.Ran()) != 0 {
			t.Errorf("Undo chạy: %v", ranStrings(m.Ran()))
		}
	})

	t.Run("disable lỗi thì lỗi có ngữ cảnh", func(t *testing.T) {
		m := newM10(t)
		lemp(m.machine)
		m.failCmd([]string{"systemctl", "disable"}, 1, "Access denied")
		err := (&ServiceDisable{Unit: "nginx"}).Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "nginx") || !strings.Contains(err.Error(), "Access denied") {
			t.Fatalf("lỗi = %v", err)
		}
	})

	t.Run("Undo khi chưa Apply thì lỗi", func(t *testing.T) {
		if err := (&ServiceDisable{Unit: "nginx"}).Undo(ctx, newM10(t)); err == nil {
			t.Fatal("không lỗi")
		}
	})

	t.Run("Info", func(t *testing.T) {
		info := (&ServiceDisable{Unit: "nginx"}).Info()
		if info.ID != "service.disable:nginx" || !slices.Equal(info.Commands, []string{"systemctl disable nginx"}) {
			t.Errorf("Info = %+v", info)
		}
	})

	t.Run("tham số sai thì không chạy lệnh nào", func(t *testing.T) {
		for _, unit := range []string{"", "-H", "nginx; rm -rf /", "$(id)", "../../etc/passwd", "nginx\nx"} {
			m := newM10(t)
			s := &ServiceDisable{Unit: unit}
			if _, err := s.Check(ctx, m); err == nil {
				t.Errorf("%q: Check không lỗi", unit)
			}
			if err := s.Apply(ctx, m); err == nil {
				t.Errorf("%q: Apply không lỗi", unit)
			}
			if len(m.Ran()) != 0 {
				t.Errorf("%q: đã chạy %v", unit, ranStrings(m.Ran()))
			}
		}
	})
}

// ================================================= ApacheModules NoRestart

func TestApacheModulesNoRestart(t *testing.T) {
	ctx := context.Background()
	step := func() *ApacheModules {
		return &ApacheModules{Enable: []string{"mpm_event", "proxy_fcgi"}, Disable: []string{"mpm_prefork"}, NoRestart: true}
	}
	setup := func(t *testing.T) *m12Machine {
		m := newM10(t)
		m.mods["mpm_prefork"] = true
		return m
	}

	t.Run("Apply và Undo không restart, vẫn configtest", func(t *testing.T) {
		m := setup(t)
		before := m.whole()
		s := step()
		if err := s.Apply(ctx, m); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if done, err := s.Check(ctx, m); err != nil || !done {
			t.Fatalf("Check sau = %v, %v", done, err)
		}
		applied := len(m.Ran())
		if err := s.Undo(ctx, m); err != nil {
			t.Fatalf("Undo: %v", err)
		}
		if !slices.Contains(ranStrings(m.Ran()[:applied]), "apachectl configtest") || !slices.Contains(ranStrings(m.Ran()[applied:]), "apachectl configtest") {
			t.Errorf("thiếu configtest ở Apply hoặc Undo: %v", ranStrings(m.Ran()))
		}
		if !reflect.DeepEqual(before, m.whole()) {
			t.Error("Undo không trả module về")
		}
		noRestart(t, m)
	})

	t.Run("configtest lỗi: trả module về, không restart", func(t *testing.T) {
		m := setup(t)
		before := m.whole()
		m.failCmd([]string{"apachectl", "configtest"}, 1, "AH00526: Syntax error")
		err := step().Apply(ctx, m)
		if err == nil || !strings.Contains(err.Error(), "AH00526") {
			t.Fatalf("lỗi = %v", err)
		}
		if !reflect.DeepEqual(before, m.whole()) {
			t.Error("module chưa được trả về")
		}
		noRestart(t, m)
	})

	t.Run("Info không có lệnh restart", func(t *testing.T) {
		for _, c := range step().Info().Commands {
			if strings.Contains(c, "restart") {
				t.Errorf("Commands có %q", c)
			}
		}
		if cmds := (&ApacheModules{Enable: []string{"ssl"}}).Info().Commands; !slices.Contains(cmds, "systemctl restart apache2") {
			t.Errorf("NoRestart=false mà thiếu restart: %v", cmds)
		}
	})
}

func noRestart(t *testing.T, m *m12Machine) {
	t.Helper()
	for _, c := range ranStrings(m.Ran()) {
		if strings.Contains(c, "restart") || strings.Contains(c, "reload") || strings.HasPrefix(c, "systemctl start") {
			t.Errorf("đã chạy %q", c)
		}
	}
}
