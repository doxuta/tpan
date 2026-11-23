package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/sitefs"
	"github.com/doxuta/tpan/internal/system"
)

// Test của M28 (mục 3.5 và bất biến B8, B9, B13 của docs/thay-doi/M28-thiet-ke.md):
// bản xem trước không bao giờ đọc qua symlink trong thư mục site.

const (
	m28Anchor = "/var/www/a.example.com"
	m28Public = m28Anchor + "/public"
	m28HT     = m28Public + "/.htaccess"
	m28User   = "tp_a_example_com_1"
)

// m28Secret là nội dung "bí mật" nằm ngoài site: không byte nào được lọt vào
// bản xem trước.
var m28Secret = []byte("root:$6$BI-MAT-KHONG-DUOC-LOT$xyz:19000:0:99999:7:::\n")

// m28Sys dựng một site trên máy giả: neo và public thuộc user của site
// (2750), /etc/shadow thuộc root.
func m28Sys() *system.FakeSystem {
	f := system.NewFake()
	owner := system.Owner{User: m28User, Group: "www-data"}
	f.SetDir(m28Anchor, fs.ModeSetgid|0o750, owner)
	f.SetDir(m28Public, fs.ModeSetgid|0o750, owner)
	f.SetFile("/etc/shadow", m28Secret, 0o640, system.Owner{User: "root", Group: "shadow"})
	return f
}

// m28Step là một bước khai đúng một file trong thư mục site.
type m28Step struct{ path string }

func (s m28Step) Info() StepInfo {
	return StepInfo{ID: "file:" + s.path, Title: i18n.T("ghi", "write"),
		Files: []FileChange{{Path: s.path, New: []byte("# BEGIN WordPress\n")}}}
}
func (m28Step) Check(context.Context, system.System) (bool, error) { return false, nil }
func (m28Step) Apply(context.Context, system.System) error         { return nil }
func (m28Step) Undo(context.Context, system.System) error          { return nil }

func m28Preview(t *testing.T, sys system.System, path string) (Preview, error) {
	t.Helper()
	p := Plan{Kind: "site.add", Target: "a.example.com", Title: i18n.T("x", "x"), Steps: []Step{m28Step{path: path}}}
	return New(sys, Options{}).Preview(context.Background(), p)
}

// m28NoSecret: chuỗi JSON của bản xem trước (và lỗi, nếu có) không chứa byte
// nào của bí mật, cả dạng thô lẫn dạng base64.
func m28NoSecret(t *testing.T, pv Preview, err error) {
	t.Helper()
	raw, _ := json.Marshal(pv)
	if err != nil {
		raw = append(raw, err.Error()...)
	}
	for _, needle := range [][]byte{m28Secret[:24], []byte("BI-MAT-KHONG-DUOC-LOT")} {
		if bytes.Contains(raw, needle) {
			t.Fatalf("bản xem trước chứa bí mật: %s", raw)
		}
	}
	enc, _ := json.Marshal(m28Secret[:24]) // base64 của 24 byte đầu
	if bytes.Contains(raw, bytes.Trim(enc, `"`)[:16]) {
		t.Fatalf("bản xem trước chứa bí mật dạng base64: %s", raw)
	}
}

func TestXemTruocKhongDocQuaSymlink(t *testing.T) {
	f := m28Sys()
	f.SetSymlink("/etc/shadow", m28HT)
	pv, err := m28Preview(t, f, m28HT)
	m28NoSecret(t, pv, err)
	if err != nil {
		t.Fatalf("symlink ở chính tên file phải cho bản xem trước (old_link), không lỗi: %v", err)
	}
	fc := pv.Steps[0].Info.Files[0]
	if fc.Old != nil || !fc.OldExists || fc.OldLink != "/etc/shadow" {
		t.Errorf("FileChange = old %q, old_exists %v, old_link %q", fc.Old, fc.OldExists, fc.OldLink)
	}
}

func TestXemTruocSymlinkHienDich(t *testing.T) {
	f := m28Sys()
	f.SetSymlink("/dev/zero", m28HT)
	pv, err := m28Preview(t, f, m28HT)
	if err != nil {
		t.Fatal(err)
	}
	if got := pv.Steps[0].Info.Files[0].OldLink; got != "/dev/zero" {
		t.Errorf("OldLink = %q, muốn /dev/zero", got)
	}
}

func TestXemTruocFileThuongGiuDiff(t *testing.T) {
	f := m28Sys()
	cur := []byte("# BEGIN WordPress\nRewriteEngine On\n# END WordPress\n")
	f.SetFile(m28HT, cur, 0o640, system.Owner{User: m28User, Group: "www-data"})
	pv, err := m28Preview(t, f, m28HT)
	if err != nil {
		t.Fatal(err)
	}
	fc := pv.Steps[0].Info.Files[0]
	if !bytes.Equal(fc.Old, cur) || !fc.OldExists || fc.OldLink != "" {
		t.Errorf("FileChange = old %q, old_exists %v, old_link %q", fc.Old, fc.OldExists, fc.OldLink)
	}
	// Chưa có file: như v0.2.0.
	f2 := m28Sys()
	pv, err = m28Preview(t, f2, m28HT)
	if err != nil {
		t.Fatal(err)
	}
	if fc := pv.Steps[0].Info.Files[0]; fc.Old != nil || fc.OldExists {
		t.Errorf("file chưa có: old %q, old_exists %v", fc.Old, fc.OldExists)
	}
}

func TestXemTruocChaLaSymlink(t *testing.T) {
	f := m28Sys()
	// public thật đổi tên đi, public thành symlink tới thư mục chỉ root đọc.
	f.SetDir("/srv/nan-nhan", 0o700, system.Root)
	f.SetFile("/srv/nan-nhan/.htaccess", m28Secret, 0o600, system.Root)
	if err := f.Remove(m28Public); err != nil {
		t.Fatal(err)
	}
	f.SetSymlink("/srv/nan-nhan", m28Public)
	pv, err := m28Preview(t, f, m28HT)
	m28NoSecret(t, pv, err)
	if !errors.Is(err, sitefs.ErrSymlink) {
		t.Fatalf("err = %v, muốn ErrSymlink", err)
	}
}

func TestXemTruocLoaiKhac(t *testing.T) {
	f := m28Sys()
	f.SetSocket(m28HT, 0o660, system.Owner{User: m28User, Group: "www-data"})
	_, err := m28Preview(t, f, m28HT)
	if !errors.Is(err, sitefs.ErrNotRegular) {
		t.Fatalf("err = %v, muốn ErrNotRegular", err)
	}
	var se *sitefs.Error
	if !errors.As(err, &se) || se.Path != m28HT {
		t.Errorf("err phải là *sitefs.Error nêu %s: %v", m28HT, err)
	}
}

// Ngoài thư mục site, Preview đọc như v0.2.0 (file của tpan dưới /etc).
func TestXemTruocNgoaiVungNhuCu(t *testing.T) {
	f := m28Sys()
	f.SetFile("/etc/nginx/sites-available/tpan-a.conf", []byte("cũ"), 0o644, system.Root)
	pv, err := m28Preview(t, f, "/etc/nginx/sites-available/tpan-a.conf")
	if err != nil {
		t.Fatal(err)
	}
	if fc := pv.Steps[0].Info.Files[0]; string(fc.Old) != "cũ" || fc.OldLink != "" {
		t.Errorf("FileChange = old %q, old_link %q", fc.Old, fc.OldLink)
	}
}

// B13: sự kiện và báo cáo của job không mang nội dung file (Old, New).
func TestSuKienKhongChuaNoiDungFile(t *testing.T) {
	h := newHarness()
	const marker = "NOI-DUNG-FILE-KHONG-VAO-SU-KIEN"
	h.sys.files["/etc/x"] = []byte("cũ " + marker)
	a := h.step("a")
	a.files = []FileChange{{Path: "/etc/x", New: []byte("mới " + marker)}}
	rep := mustExecute(t, h, plan(a))
	if rep.Job.Status != JobSucceeded {
		t.Fatalf("status = %s", rep.Job.Status)
	}
	raw, _ := json.Marshal(struct {
		E []Event
		R Report
	}{h.events, rep})
	for _, ev := range h.j.events {
		raw = append(raw, ev.Message...)
	}
	if strings.Contains(string(raw), marker) {
		t.Fatalf("sự kiện hoặc báo cáo chứa nội dung file: %s", raw)
	}
}
