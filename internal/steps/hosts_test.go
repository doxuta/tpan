package steps

import (
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/system"
)

const (
	hostsName = "telemetry.sury.org"
	hostsLine = "0.0.0.0 telemetry.sury.org # tpan: chặn telemetry của kho PHP Sury"
	// hostsBase giống /etc/hosts do cloud-init sinh trên Ubuntu 24.04, cộng
	// một dòng người dùng tự chặn đúng tên đó (không có dấu của tpan).
	hostsBase = "# Your system has configured 'manage_etc_hosts' as True.\n" +
		"127.0.1.1 vps vps\n127.0.0.1 localhost\n\n" +
		"::1 localhost ip6-localhost ip6-loopback\n" +
		"0.0.0.0 telemetry.sury.org\n"
)

var hostsOwner = system.Owner{User: "root", Group: "adm"}

func hostsSim(content string) *system.FakeSystem {
	m := system.NewFake()
	m.SetDir("/etc", 0o755, system.Root)
	m.SetFile(hostsPath, []byte(content), 0o640, hostsOwner)
	return m
}

func hostsRead(t *testing.T, m *system.FakeSystem) string {
	t.Helper()
	b, err := m.ReadFile(hostsPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mustCheck(t *testing.T, s *HostsBlock, m *system.FakeSystem, want bool) {
	t.Helper()
	done, err := s.Check(t.Context(), m)
	if err != nil {
		t.Fatal(err)
	}
	if done != want {
		t.Fatalf("Check = %v; muốn %v\n%s", done, want, hostsRead(t, m))
	}
}

func TestHostsBlockLifecycle(t *testing.T) {
	m := hostsSim(hostsBase)
	before := m.Snapshot()
	s := &HostsBlock{Name: hostsName}

	mustCheck(t, s, m, false)
	if err := s.Apply(t.Context(), m); err != nil {
		t.Fatal(err)
	}
	mustCheck(t, s, m, true)
	if got, want := hostsRead(t, m), hostsBase+hostsLine+"\n"; got != want {
		t.Errorf("sau Apply:\n%s\nmuốn:\n%s", got, want)
	}
	fi, _ := m.Stat(hostsPath)
	if fi.Mode.Perm() != 0o640 || fi.Owner != hostsOwner {
		t.Errorf("quyền/chủ đổi: %v %v", fi.Mode.Perm(), fi.Owner)
	}
	if len(m.Ran()) != 0 {
		t.Errorf("bước không được chạy lệnh nào: %v", m.Ran())
	}

	// Chạy lại: Check true nên engine bỏ qua; Apply lần nữa cũng không đổi gì.
	after := m.Snapshot()
	if err := (&HostsBlock{Name: hostsName}).Apply(t.Context(), m); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, m.Snapshot()) {
		t.Errorf("Apply lần hai đổi file:\n%s", hostsRead(t, m))
	}

	if err := s.Undo(t.Context(), m); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, m.Snapshot()) {
		t.Errorf("Undo không trả về như cũ:\n%s", hostsRead(t, m))
	}
}

func TestHostsUnblockLifecycle(t *testing.T) {
	content := hostsBase + hostsLine + "\n" + "10.0.0.5 db.example.com\n"
	m := hostsSim(content)
	before := m.Snapshot()
	s := &HostsBlock{Name: hostsName, Remove: true}

	mustCheck(t, s, m, false)
	if err := s.Apply(t.Context(), m); err != nil {
		t.Fatal(err)
	}
	mustCheck(t, s, m, true)
	// Dòng tự chặn của người dùng (không có dấu tpan) và dòng sau giữ nguyên.
	if got, want := hostsRead(t, m), hostsBase+"10.0.0.5 db.example.com\n"; got != want {
		t.Errorf("sau gỡ:\n%s\nmuốn:\n%s", got, want)
	}
	if err := s.Undo(t.Context(), m); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, m.Snapshot()) {
		t.Errorf("Undo không trả về như cũ:\n%s", hostsRead(t, m))
	}

	// Không có dòng nào của tpan: đã đạt, kể cả khi người dùng tự chặn tên đó.
	mustCheck(t, &HostsBlock{Name: hostsName, Remove: true}, hostsSim(hostsBase), true)
}

func TestHostsBlockEdgeCases(t *testing.T) {
	t.Run("file không có xuống dòng cuối", func(t *testing.T) {
		m := hostsSim("127.0.0.1 localhost")
		if err := (&HostsBlock{Name: hostsName}).Apply(t.Context(), m); err != nil {
			t.Fatal(err)
		}
		if got, want := hostsRead(t, m), "127.0.0.1 localhost\n"+hostsLine+"\n"; got != want {
			t.Errorf("got %q; muốn %q", got, want)
		}
	})

	t.Run("dòng của tpan bị lặp: còn đúng một", func(t *testing.T) {
		m := hostsSim(hostsLine + "\n127.0.0.1 localhost\n" + hostsLine + "\r\n")
		s := &HostsBlock{Name: hostsName}
		mustCheck(t, s, m, false)
		if err := s.Apply(t.Context(), m); err != nil {
			t.Fatal(err)
		}
		if got := hostsRead(t, m); strings.Count(got, hostsLine) != 1 || !strings.Contains(got, "127.0.0.1 localhost\n") {
			t.Errorf("sau Apply:\n%s", got)
		}
		mustCheck(t, s, m, true)
	})

	t.Run("chưa có /etc/hosts: tạo 0644 root, Undo xoá", func(t *testing.T) {
		m := system.NewFake()
		m.SetDir("/etc", 0o755, system.Root)
		before := m.Snapshot()
		s := &HostsBlock{Name: hostsName}
		mustCheckErrFree(t, &HostsBlock{Name: hostsName, Remove: true}, m, true)
		if err := s.Apply(t.Context(), m); err != nil {
			t.Fatal(err)
		}
		fi, err := m.Stat(hostsPath)
		if err != nil || fi.Mode.Perm() != 0o644 || fi.Owner != system.Root {
			t.Errorf("file mới: %+v %v", fi, err)
		}
		if err := s.Undo(t.Context(), m); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, m.Snapshot()) {
			t.Error("Undo không xoá file đã tạo")
		}
	})

	t.Run("symlink: từ chối, không ghi đè", func(t *testing.T) {
		m := system.NewFake()
		m.SetFile("/run/hosts", []byte("127.0.0.1 localhost\n"), 0o644, system.Root)
		m.SetSymlink("/run/hosts", hostsPath)
		before := m.Snapshot()
		s := &HostsBlock{Name: hostsName}
		if _, err := s.Check(t.Context(), m); err == nil {
			t.Error("Check phải lỗi với symlink")
		}
		if err := s.Apply(t.Context(), m); err == nil {
			t.Error("Apply phải lỗi với symlink")
		}
		if !reflect.DeepEqual(before, m.Snapshot()) {
			t.Error("Apply đổi máy")
		}
	})

	t.Run("lỗi ghi có ngữ cảnh", func(t *testing.T) {
		m := hostsSim(hostsBase)
		m.Fail("write", hostsPath, fs.ErrPermission)
		err := (&HostsBlock{Name: hostsName}).Apply(t.Context(), m)
		if !errors.Is(err, fs.ErrPermission) || !strings.Contains(err.Error(), hostsPath) {
			t.Errorf("lỗi = %v", err)
		}
	})

	t.Run("Undo khi chưa Apply", func(t *testing.T) {
		if err := (&HostsBlock{Name: hostsName}).Undo(t.Context(), hostsSim(hostsBase)); !errors.Is(err, errNotApplied) {
			t.Errorf("lỗi = %v", err)
		}
	})
}

func mustCheckErrFree(t *testing.T, s *HostsBlock, m *system.FakeSystem, want bool) {
	t.Helper()
	done, err := s.Check(t.Context(), m)
	if err != nil || done != want {
		t.Fatalf("Check = %v, %v; muốn %v", done, err, want)
	}
}

func TestHostsBlockBadNames(t *testing.T) {
	bad := []string{
		"", "localhost", "a.com; rm -rf /", "$(id).example.com", "../../etc/passwd",
		"a.com\n1.2.3.4 bank.example.com", "a.com # x", "a.com\tb.com", "a .com",
		"Telemetry.Sury.Org", "tên-miền.việt", "-a.example.com", "a-.example.com",
		"a..example.com", ".example.com", "example.com.", "*.example.com",
		strings.Repeat("a", 64) + ".com", strings.Repeat(strings.Repeat("a", 60)+".", 5) + "com",
	}
	for _, name := range bad {
		for _, remove := range []bool{false, true} {
			m := hostsSim(hostsBase)
			before := m.Snapshot()
			s := &HostsBlock{Name: name, Remove: remove}
			if _, err := s.Check(t.Context(), m); err == nil {
				t.Errorf("Check(%q) phải lỗi", name)
			}
			if err := s.Apply(t.Context(), m); err == nil {
				t.Errorf("Apply(%q) phải lỗi", name)
			}
			if !reflect.DeepEqual(before, m.Snapshot()) {
				t.Errorf("tên %q làm đổi /etc/hosts", name)
			}
		}
	}
	for _, good := range []string{hostsName, "a.b", "x-1.example.com", strings.Repeat("a", 63) + ".com"} {
		if _, err := (&HostsBlock{Name: good}).Check(t.Context(), hostsSim(hostsBase)); err != nil {
			t.Errorf("tên %q bị từ chối: %v", good, err)
		}
	}
}

func TestHostsBlockInfo(t *testing.T) {
	for _, s := range []*HostsBlock{{Name: hostsName}, {Name: hostsName, Remove: true}} {
		info := s.Info()
		// Không khai Files: engine sẽ ghi sha256 của cả /etc/hosts vào
		// managed_files và health coi mọi sửa tay của người khác là lệch.
		if len(info.Files) != 0 || len(info.Commands) != 0 || info.Danger || !info.NoUndo.IsZero() {
			t.Errorf("%s: %+v", info.ID, info)
		}
		if info.Title.IsZero() || info.Why.IsZero() || !strings.Contains(info.Why.Vi, hostsPath) {
			t.Errorf("%s: thiếu tiêu đề/lý do: %+v", info.ID, info)
		}
	}
	if a, b := (&HostsBlock{Name: hostsName}).Info().ID, (&HostsBlock{Name: hostsName, Remove: true}).Info().ID; a == b {
		t.Errorf("hai chiều trùng ID %s", a)
	}
}

// tmplBase giống phần cuối của /etc/cloud/templates/hosts.debian.tmpl trên
// Ubuntu 24.04 (cloud-init sinh /etc/hosts từ file này mỗi lần khởi động).
const tmplBase = "## template:jinja\n{#\nThis file (/etc/cloud/templates/hosts.debian.tmpl) is only utilized\n#}\n" +
	"127.0.1.1 {{fqdn}} {{hostname}}\n127.0.0.1 localhost\n\n::1 localhost ip6-localhost ip6-loopback\n"

func readFile(t *testing.T, m *system.FakeSystem, p string) string {
	t.Helper()
	b, err := m.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestHostsBlockCloudInitTemplate(t *testing.T) {
	m := hostsSim(hostsBase)
	m.SetFile(hostsTemplate, []byte(tmplBase), 0o644, system.Root)
	before := m.Snapshot()

	s := &HostsBlock{Name: hostsName}
	mustCheck(t, s, m, false)
	if err := s.Apply(t.Context(), m); err != nil {
		t.Fatal(err)
	}
	mustCheck(t, s, m, true)
	if got := readFile(t, m, hostsTemplate); got != tmplBase+hostsLine+"\n" {
		t.Errorf("file mẫu sau Apply:\n%s", got)
	}
	if got := hostsRead(t, m); got != hostsBase+hostsLine+"\n" {
		t.Errorf("/etc/hosts sau Apply:\n%s", got)
	}
	if err := s.Undo(t.Context(), m); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, m.Snapshot()) {
		t.Error("Undo không trả cả hai file về như cũ")
	}

	// Chỉ một trong hai file thiếu dòng (cloud-init vừa sinh lại /etc/hosts):
	// Check false, Apply chỉ ghi file thiếu.
	if err := s.Apply(t.Context(), m); err != nil {
		t.Fatal(err)
	}
	m.SetFile(hostsPath, []byte(hostsBase), 0o640, hostsOwner)
	fixed := &HostsBlock{Name: hostsName}
	mustCheck(t, fixed, m, false)
	if err := fixed.Apply(t.Context(), m); err != nil {
		t.Fatal(err)
	}
	mustCheck(t, fixed, m, true)
	if len(fixed.prev) != 1 || fixed.prev[0].path != hostsPath {
		t.Errorf("Apply ghi cả file đã đúng: %+v", fixed.prev)
	}

	// Gỡ: dòng đi khỏi cả hai file, các dòng khác giữ nguyên.
	rm := &HostsBlock{Name: hostsName, Remove: true}
	mustCheck(t, rm, m, false)
	if err := rm.Apply(t.Context(), m); err != nil {
		t.Fatal(err)
	}
	mustCheck(t, rm, m, true)
	if hostsRead(t, m) != hostsBase || readFile(t, m, hostsTemplate) != tmplBase {
		t.Errorf("sau gỡ:\n%s\n---\n%s", hostsRead(t, m), readFile(t, m, hostsTemplate))
	}
}

func TestHostsBlockTemplateWriteFailRestoresHosts(t *testing.T) {
	m := hostsSim(hostsBase)
	m.SetFile(hostsTemplate, []byte(tmplBase), 0o644, system.Root)
	m.Fail("write", hostsTemplate, fs.ErrPermission)
	// Lỗi ở file thứ hai thì /etc/hosts vừa ghi phải được trả về; Fail làm
	// mọi lần ghi file mẫu lỗi nhưng không chặn ghi /etc/hosts.
	before := m.Snapshot()
	err := (&HostsBlock{Name: hostsName}).Apply(t.Context(), m)
	if !errors.Is(err, fs.ErrPermission) || !strings.Contains(err.Error(), hostsTemplate) {
		t.Errorf("lỗi = %v", err)
	}
	if !reflect.DeepEqual(before, m.Snapshot()) {
		t.Errorf("Apply lỗi mà /etc/hosts không về như cũ:\n%s", hostsRead(t, m))
	}
}

func TestHostsBlockTemplateSymlinkRefused(t *testing.T) {
	m := hostsSim(hostsBase)
	m.SetFile("/usr/share/hosts.tmpl", []byte(tmplBase), 0o644, system.Root)
	m.SetSymlink("/usr/share/hosts.tmpl", hostsTemplate)
	before := m.Snapshot()
	if err := (&HostsBlock{Name: hostsName}).Apply(t.Context(), m); err == nil {
		t.Error("file mẫu là symlink mà Apply không lỗi")
	}
	if !reflect.DeepEqual(before, m.Snapshot()) {
		t.Error("Apply đổi máy")
	}
}
