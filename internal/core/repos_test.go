package core

import (
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/system"
)

func TestOSCodename(t *testing.T) {
	m := system.NewFake()
	m.SetFile("/etc/os-release", []byte("NAME=\"Ubuntu\"\nVERSION_ID=\"24.04\"\nVERSION_CODENAME=noble\n"), 0o644, system.Root)
	if c, err := osCodename(m); err != nil || c != "noble" {
		t.Fatalf("osCodename = %q, %v", c, err)
	}
	m.SetFile("/etc/os-release", []byte("VERSION_CODENAME=\"no ble; rm\"\n"), 0o644, system.Root)
	if _, err := osCodename(m); err == nil {
		t.Fatal("tên mã lạ phải lỗi")
	}
}

// Ghim của kho sury phải giữ gói Ubuntu (cả kho 100) và chỉ mở đúng các bản
// PHP lấy từ kho (đã kiểm trên máy ảo, docs/thay-doi/M12.md).
func TestRepoSuryPin(t *testing.T) {
	r, err := repoSury("noble", []string{"8.4", "8.2"})
	if err != nil {
		t.Fatal(err)
	}
	pin := string(r.Pin)
	for _, want := range []string{"Pin: origin packages.sury.org\nPin-Priority: 100", "Package: php-common php8.2* php8.4*\n"} {
		if !strings.Contains(pin, want) {
			t.Errorf("pin thiếu %q:\n%s", want, pin)
		}
	}
	if !strings.Contains(string(r.Sources), "Signed-By: /etc/apt/keyrings/tpan-sury.gpg") {
		t.Errorf("sources thiếu Signed-By:\n%s", r.Sources)
	}
	if _, err := repoSury("noble", []string{"8.4; rm"}); err == nil {
		t.Error("bản PHP lạ phải lỗi")
	}
}

func TestRepoMariaDBLine(t *testing.T) {
	r, err := repoMariaDB("noble", "11.8")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(r.Sources), "https://dlm.mariadb.com/repo/mariadb-server/11.8/repo/ubuntu") || r.Pin != nil {
		t.Errorf("kho MariaDB sai: %s", r.Sources)
	}
	for _, bad := range []string{"", "11", "11.8/../x", "latest"} {
		if _, err := repoMariaDB("noble", bad); err == nil {
			t.Errorf("dòng %q phải bị từ chối", bad)
		}
	}
	if _, err := repoNginx("noble", true); err != nil {
		t.Fatal(err)
	}
}

func TestRepoEnabled(t *testing.T) {
	m := system.NewFake()
	if ok, err := repoEnabled(m, RepoSury); ok || err != nil {
		t.Fatalf("chưa có file mà báo %v, %v", ok, err)
	}
	m.SetFile(RepoSourcesPath(RepoSury), []byte("x"), 0o644, system.Root)
	if ok, err := repoEnabled(m, RepoSury); !ok || err != nil {
		t.Fatalf("có file mà báo %v, %v", ok, err)
	}
}
