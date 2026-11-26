package steps

import (
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
)

// Máy mới (chưa có file): ghi conf.d/tpan-names.conf; hoàn lại thì máy như cũ.
// Chạy lần hai thì không đổi gì.
func TestEnsureNginxNames(t *testing.T) {
	m := system.NewFake()
	m.SetDir("/etc/nginx", 0o755, system.Root)
	before := m.Snapshot()
	restore, err := EnsureNginxNames(m)
	if err != nil || restore == nil {
		t.Fatalf("máy mới: restore=%v err=%v, muốn có hàm hoàn lại", restore != nil, err)
	}
	got, err := m.ReadFile(NginxNamesConf)
	if err != nil || string(got) != string(templates.NginxNames()) {
		t.Fatalf("nội dung %s = %q, %v", NginxNamesConf, got, err)
	}
	if !strings.Contains(string(got), "\nserver_names_hash_bucket_size 512;\n") {
		t.Errorf("thiếu trần 512:\n%s", got)
	}
	if again, err := EnsureNginxNames(m); err != nil || again != nil {
		t.Errorf("lần hai phải không đổi gì: restore=%v err=%v", again != nil, err)
	}
	if len(m.Ran()) != 0 {
		t.Errorf("hàm này không được chạy lệnh: %v", m.Ran())
	}
	if err := restore(); err != nil {
		t.Fatal(err)
	}
	if after := m.Snapshot(); len(after) != len(before) {
		t.Errorf("hoàn lại mà máy khác trước: %d mục, trước %d", len(after), len(before))
	}
}

// Máy dùng gói nginx.org cài bằng bản cũ: conf.d/tpan.conf còn dòng 64. Phải gỡ
// dòng đó (hai file cùng đặt một chỉ thị thì nginx báo "is duplicate"), giữ
// nguyên phần còn lại; hoàn lại thì dòng cũ về.
func TestEnsureNginxNamesGoDongCuCuaNginxOrg(t *testing.T) {
	m := system.NewFake()
	old := "# Do tpan quản lý.\ninclude /etc/nginx/sites-enabled/*;\n\n# chú thích\nserver_names_hash_bucket_size 64;\n"
	m.SetFile(nginxOrgInclude, []byte(old), 0o644, system.Root)
	restore, err := EnsureNginxNames(m)
	if err != nil || restore == nil {
		t.Fatalf("restore=%v err=%v", restore != nil, err)
	}
	org, _ := m.ReadFile(nginxOrgInclude)
	if strings.Contains(string(org), "server_names_hash_bucket_size 64;") || !strings.Contains(string(org), "include /etc/nginx/sites-enabled/*;\n") {
		t.Errorf("tpan.conf sau khi gỡ dòng cũ:\n%s", org)
	}
	if _, err := m.ReadFile(NginxNamesConf); err != nil {
		t.Errorf("chưa ghi %s: %v", NginxNamesConf, err)
	}
	if err := restore(); err != nil {
		t.Fatal(err)
	}
	if org, _ := m.ReadFile(nginxOrgInclude); string(org) != old {
		t.Errorf("hoàn lại mà tpan.conf khác trước:\n%s", org)
	}
	if _, err := m.ReadFile(NginxNamesConf); err == nil {
		t.Errorf("hoàn lại mà %s vẫn còn", NginxNamesConf)
	}
}

// Người quản trị đã tự đặt chỉ thị trong nginx.conf: không chen vào. Dòng đang
// là chú thích (mặc định của Ubuntu) thì không tính.
func TestEnsureNginxNamesNhuongNguoiQuanTri(t *testing.T) {
	m := system.NewFake()
	m.SetFile(nginxMainConf, []byte("http {\n\tserver_names_hash_bucket_size 128;\n}\n"), 0o644, system.Root)
	if restore, err := EnsureNginxNames(m); err != nil || restore != nil {
		t.Errorf("đã tự đặt: restore=%v err=%v, muốn không đổi gì", restore != nil, err)
	}
	if _, err := m.ReadFile(NginxNamesConf); err == nil {
		t.Errorf("không được ghi %s khi nginx.conf đã đặt chỉ thị", NginxNamesConf)
	}
	m2 := system.NewFake()
	m2.SetFile(nginxMainConf, []byte("http {\n\t# server_names_hash_bucket_size 64;\n}\n"), 0o644, system.Root)
	if restore, err := EnsureNginxNames(m2); err != nil || restore == nil {
		t.Errorf("dòng chú thích không phải là đã đặt: restore=%v err=%v", restore != nil, err)
	}
}
