package steps

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/s3"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

const panelTestDB = "SQLite format 3\x00 ban chup tpan.db"

// panelSys là s3Sys cộng `tar --create` giả: file tar giả là JSON của
// map[tên]nội dung các file được nén, đủ để test mở ra đọc manifest.
func panelSys(t *testing.T) *system.FakeSystem {
	t.Helper()
	f := s3Sys(t)
	f.Handle([]string{"tar", "--create"}, func(c system.Cmd) system.Result {
		var file, dir string
		files := map[string]string{}
		for _, a := range c.Args[1:] {
			switch {
			case strings.HasPrefix(a, "--file="):
				file = strings.TrimPrefix(a, "--file=")
			case strings.HasPrefix(a, "--directory="):
				dir = strings.TrimPrefix(a, "--directory=")
			case strings.HasPrefix(a, "--"):
			default:
				data, err := f.ReadFile(dir + "/" + a)
				if err != nil {
					return system.Result{ExitCode: 2, Stderr: "tar: " + err.Error()}
				}
				files[a] = string(data)
			}
		}
		data, _ := json.Marshal(files)
		f.SetFile(file, data, 0o644, system.Root)
		return system.Result{}
	})
	return f
}

// panelEnc là luồng mã hoá giả: "AGE|" ở đầu, đuôi "|END" chỉ ghi lúc Close
// (như age ghi khúc cuối lúc Close), để test bắt được lỗi quên hay sai thứ
// tự Close.
type panelEnc struct{ w io.Writer }

func (e panelEnc) Write(b []byte) (int, error) { return e.w.Write(b) }
func (e panelEnc) Close() error {
	_, err := io.WriteString(e.w, "|END")
	return err
}

func panelTestEncrypt(dst io.Writer) (io.WriteCloser, error) {
	if _, err := io.WriteString(dst, "AGE|"); err != nil {
		return nil, err
	}
	return panelEnc{dst}, nil
}

var panelTestNow = time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)

func newPanelBundle(f *system.FakeSystem) *PanelBundle {
	return &PanelBundle{
		Dir: site.TPANDBBackupDir,
		Snapshot: func(_ context.Context, p string) error {
			f.SetFile(p, []byte(panelTestDB), 0o644, system.Root)
			return nil
		},
		Migrations: func(context.Context) ([]int, error) { return []int{1, 2, 1800}, nil },
		Encrypt:    panelTestEncrypt,
		Hostname:   "may1",
		Version:    "0.3.0",
		now:        func() time.Time { return panelTestNow },
	}
}

// panelNoTmp: không còn thư mục tạm, .part hay gói nào trong Dir.
func panelNoLeft(t *testing.T, f *system.FakeSystem, alsoFinal bool) {
	t.Helper()
	for p := range f.Snapshot() {
		if strings.Contains(p, "/.tmp-") || strings.HasSuffix(p, ".part") {
			t.Errorf("còn sót %s", p)
		}
		if alsoFinal && strings.HasSuffix(p, ".tar.gz.age") {
			t.Errorf("còn gói %s sau lỗi", p)
		}
	}
}

// panelOpen mở gói giả: bỏ "AGE|" và "|END", đọc JSON của tar giả.
func panelOpen(t *testing.T, data string) map[string]string {
	t.Helper()
	body, ok := strings.CutPrefix(data, "AGE|")
	if !ok {
		t.Fatalf("gói không bắt đầu bằng phần đầu của luồng mã: %q", data)
	}
	body, ok = strings.CutSuffix(body, "|END")
	if !ok {
		t.Fatalf("gói thiếu khúc cuối (luồng mã chưa Close trước file): %q", data)
	}
	var files map[string]string
	if err := json.Unmarshal([]byte(body), &files); err != nil {
		t.Fatal(err)
	}
	return files
}

func TestPanelBundleThuong(t *testing.T) {
	ctx := context.Background()
	f := panelSys(t)
	s := newPanelBundle(f)
	info := s.Info()
	if info.ID != "panel.bundle" || len(info.Files) != 0 || !strings.Contains(strings.Join(info.Commands, "\n"), PanelNameLater) {
		t.Errorf("Info = %+v", info)
	}
	if ok, err := s.Check(ctx, f); ok || err != nil {
		t.Fatalf("Check trước = %v, %v", ok, err)
	}
	if err := s.Apply(ctx, f); err != nil {
		t.Fatal(err)
	}
	wantName := "tpan-20261121-100000.tar.gz.age"
	if s.Name != wantName || s.Path != site.TPANDBBackupDir+"/"+wantName {
		t.Errorf("Name, Path = %s, %s", s.Name, s.Path)
	}
	e := f.Snapshot()[s.Path]
	if e.Mode != 0o600 || e.Owner != system.Root {
		t.Errorf("gói = %v %v", e.Mode, e.Owner)
	}
	if s.Size != int64(len(e.Data)) || s.SHA256 != sha256Hex([]byte(e.Data)) {
		t.Errorf("Size, SHA256 = %d, %s", s.Size, s.SHA256)
	}
	files := panelOpen(t, e.Data)
	if files["tpan.db"] != panelTestDB {
		t.Errorf("tpan.db trong gói = %q", files["tpan.db"])
	}
	var man map[string]any
	if err := json.Unmarshal([]byte(files["manifest.json"]), &man); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"format": 1.0, "created": "2026-11-21T10:00:00Z", "hostname": "may1", "tpan_version": "0.3.0",
		"migrations": []any{1.0, 2.0, 1800.0}, "db_sha256": sha256Hex([]byte(panelTestDB)),
	}
	if g, w := mustJSON(t, man), mustJSON(t, want); g != w {
		t.Errorf("manifest = %s\nmuốn %s", g, w)
	}
	panelNoLeft(t, f, false)
	if ok, err := s.Check(ctx, f); !ok || err != nil {
		t.Errorf("Check sau = %v, %v", ok, err)
	}
	if info2 := s.Info(); mustJSON(t, info2) != mustJSON(t, info) {
		t.Error("Info đổi sau Apply")
	}
	if err := s.Undo(ctx, f); err != nil {
		t.Fatal(err)
	}
	s3NoPath(t, f, s.Path)
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestPanelBundleMigrationsRong(t *testing.T) {
	f := panelSys(t)
	s := newPanelBundle(f)
	s.Migrations = func(context.Context) ([]int, error) { return nil, nil }
	if err := s.Apply(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	files := panelOpen(t, f.Snapshot()[s.Path].Data)
	if !strings.Contains(files["manifest.json"], `"migrations": []`) {
		t.Errorf("manifest = %s", files["manifest.json"])
	}
}

// Thư mục tạm (bản rõ của tpan.db) luôn bị xoá, lỗi ở bước nào cũng vậy.
func TestPanelBundleLuonXoaThuMucTam(t *testing.T) {
	boom := errors.New("boom")
	cases := []struct {
		name string
		set  func(f *system.FakeSystem, s *PanelBundle)
	}{
		{"Snapshot lỗi giữa chừng", func(f *system.FakeSystem, s *PanelBundle) {
			s.Snapshot = func(_ context.Context, p string) error {
				f.SetFile(p, []byte("nua"), 0o644, system.Root)
				f.SetFile(p+"-journal", []byte("j"), 0o644, system.Root)
				return boom
			}
		}},
		{"Migrations lỗi", func(_ *system.FakeSystem, s *PanelBundle) {
			s.Migrations = func(context.Context) ([]int, error) { return nil, boom }
		}},
		{"tar lỗi", func(f *system.FakeSystem, _ *PanelBundle) {
			f.Handle([]string{"tar", "--create"}, func(system.Cmd) system.Result {
				return system.Result{ExitCode: 1, Stderr: "tar: file changed as we read it"}
			})
		}},
		{"Encrypt lỗi", func(_ *system.FakeSystem, s *PanelBundle) {
			s.Encrypt = func(io.Writer) (io.WriteCloser, error) { return nil, boom }
		}},
		{"Close của luồng mã lỗi", func(_ *system.FakeSystem, s *PanelBundle) {
			s.Encrypt = func(dst io.Writer) (io.WriteCloser, error) { return failClose{dst}, nil }
		}},
		{"đổi tên lỗi", func(f *system.FakeSystem, _ *PanelBundle) {
			f.Fail("rename", site.TPANDBBackupDir+"/tpan-20261121-100000.tar.gz.age", boom)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := panelSys(t)
			s := newPanelBundle(f)
			c.set(f, s)
			if err := s.Apply(context.Background(), f); err == nil {
				t.Fatal("Apply không lỗi")
			}
			panelNoLeft(t, f, true)
			if s.Path != "" || s.Name != "" {
				t.Errorf("kết quả đặt dù lỗi: %s %s", s.Path, s.Name)
			}
			if ok, _ := s.Check(context.Background(), f); ok {
				t.Error("Check true sau lỗi")
			}
		})
	}
}

type failClose struct{ w io.Writer }

func (e failClose) Write(b []byte) (int, error) { return e.w.Write(b) }
func (failClose) Close() error                  { return errors.New("không ghi được khúc cuối") }

func TestPanelBundleKiemThamSo(t *testing.T) {
	cases := []struct {
		name string
		set  func(s *PanelBundle)
	}{
		{"Dir khác", func(s *PanelBundle) { s.Dir = "/tmp" }},
		{"thiếu Snapshot", func(s *PanelBundle) { s.Snapshot = nil }},
		{"thiếu Migrations", func(s *PanelBundle) { s.Migrations = nil }},
		{"thiếu Encrypt", func(s *PanelBundle) { s.Encrypt = nil }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := panelSys(t)
			s := newPanelBundle(f)
			c.set(s)
			if err := s.Apply(context.Background(), f); err == nil {
				t.Error("Apply không lỗi")
			}
			if len(f.Ran()) != 0 {
				t.Error("đã chạy lệnh")
			}
		})
	}
}

// Plan backup.panel thật: PanelBundle rồi S3Push đọc kết quả của nó lúc
// Apply, chạy qua engine (Check của S3Push lúc gói chưa có không được lỗi).
func TestS3PushTuPanelBundle(t *testing.T) {
	f, mem := panelSys(t), s3.NewMem()
	pb := newPanelBundle(f)
	push := &S3Push{Store: mem, ID: "panel", From: pb}
	before := mustJSON(t, push.Info())
	if !strings.Contains(push.Info().Title.Vi, "panel/"+PanelNameLater) || push.Info().ID != "s3.push:panel" {
		t.Errorf("Info = %s", before)
	}
	var notes []string
	rep, err := engine.New(f, engine.Options{OnEvent: func(e engine.Event) {
		if e.Kind == engine.EventOutput && e.StepID == "s3.push:panel" {
			notes = append(notes, e.Message)
		}
	}}).Execute(context.Background(), engine.Plan{Kind: "backup.panel", Target: "panel", Steps: []engine.Step{pb, push}})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("job = %s: %s", rep.Job.Status, rep.Job.Error)
	}
	key := "panel/" + pb.Name
	if push.Key != key || push.Path != pb.Path || push.SHA256 != pb.SHA256 || push.Size != pb.Size {
		t.Errorf("push = %+v", push)
	}
	if got := mem.Objects()[key]; string(got) != f.Snapshot()[pb.Path].Data {
		t.Errorf("object %s = %q", key, got)
	}
	if mustJSON(t, push.Info()) != before {
		t.Error("Info của S3Push đổi sau Apply")
	}
	if len(notes) == 0 || !strings.Contains(notes[len(notes)-1], pb.SHA256) {
		t.Errorf("dòng tiến độ = %q", notes)
	}
}
