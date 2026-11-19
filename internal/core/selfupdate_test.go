package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

const selfTestBase = "https://example.com/releases"

// newSelfEnv: máy có tpan bản v1.0.0 ở TpanBin, dịch vụ tpan đang chạy, máy
// chủ tải trả file của bản newVer. Lệnh `tpan version` in bản đang nằm ở
// TpanBin (đọc nội dung file giả), để kiểm chứng thấy đúng file sau khi thay.
func newSelfEnv(t *testing.T, newVer string) (*SelfUpdate, *system.FakeSystem) {
	t.Helper()
	f := system.NewFake()
	f.SetFile(TpanBin, []byte("tpan v1.0.0"), 0o755, system.Root)
	data := []byte("tpan " + newVer)
	sum := sha256.Sum256(data)
	f.Handle([]string{"curl", "-fsSL", "--max-time", "30"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: hex.EncodeToString(sum[:]) + "  tpan-linux-arm64\n"}
	})
	f.Handle([]string{"curl", "-fsSL", "--max-time", "300"}, func(c system.Cmd) system.Result {
		f.SetFile(c.Args[slices.Index(c.Args, "-o")+1], data, 0o644, system.Root)
		return system.Result{}
	})
	version := func(p string) func(system.Cmd) system.Result {
		return func(system.Cmd) system.Result {
			b, err := f.ReadFile(p)
			if err != nil {
				return system.Result{ExitCode: 127, Stderr: err.Error()}
			}
			return system.Result{Stdout: string(b) + " (commit abc, build x)\n"}
		}
	}
	f.Handle([]string{tpanNew, "version"}, version(tpanNew))
	f.Handle([]string{TpanBin, "version"}, version(TpanBin))
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "tpan.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return &SelfUpdate{Sys: f, Settings: mapSettings{}, Journal: st, Arch: "arm64", Version: "v1.0.0"}, f
}

func fileData(t *testing.T, f *system.FakeSystem, p string) string {
	t.Helper()
	b, err := f.ReadFile(p)
	if err != nil {
		return "<không có>"
	}
	return string(b)
}

func restarted(f *system.FakeSystem) bool {
	return slices.ContainsFunc(f.Ran(), func(c system.Cmd) bool { return c.String() == "systemctl restart tpan" })
}

func TestSelfUpdate(t *testing.T) {
	s, f := newSelfEnv(t, "v1.1.0")
	s.Settings = mapSettings{SettingSelfURL: selfTestBase}
	p, err := s.Prepare(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := stepIDs(p.Plan), []string{"self.download:" + tpanNew, "self.install:" + TpanBin, "service.restart:tpan"}; !slices.Equal(got, want) {
		t.Fatalf("bước = %v, muốn %v", got, want)
	}
	if p.Plan.Target != selfTestBase {
		t.Errorf("Target = %q (phải lấy gốc tải từ settings)", p.Plan.Target)
	}
	// Xem trước: chưa tải gì nên mọi bước đều "sẽ chạy".
	pv, err := p.Preview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, sp := range pv.Steps {
		if sp.Done {
			t.Errorf("xem trước: bước %s bị coi là đã xong", sp.Info.ID)
		}
	}
	f.Handle([]string{"systemctl", "is-active", "tpan"}, func(system.Cmd) system.Result { return system.Result{Stdout: "active\n"} })
	rep, err := p.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("job %s: %v %+v", rep.Job.Status, err, rep.Steps)
	}
	if got := fileData(t, f, TpanBin); got != "tpan v1.1.0" {
		t.Errorf("TpanBin = %q", got)
	}
	if got := fileData(t, f, tpanOld); got != "tpan v1.0.0" {
		t.Errorf(".old = %q", got)
	}
	if !restarted(f) {
		t.Error("không khởi động lại tpan")
	}
	if len(rep.Checks) != 1 || rep.Checks[0].ID != "self.version" || rep.Checks[0].Status != "pass" {
		t.Errorf("kiểm chứng = %+v", rep.Checks)
	}
}

// Bản tải về trùng bản đang chạy: không thay file, không khởi động lại.
func TestSelfUpdateSameVersion(t *testing.T) {
	s, f := newSelfEnv(t, "v1.0.0")
	f.Handle([]string{"systemctl", "is-active", "tpan"}, func(system.Cmd) system.Result { return system.Result{Stdout: "active\n"} })
	p, err := s.Prepare(ctx, selfTestBase)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := p.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("job %s: %v %+v", rep.Job.Status, err, rep.Steps)
	}
	for _, sr := range rep.Steps[1:] {
		if sr.Outcome != "skipped" {
			t.Errorf("bước %s: %s, muốn skipped", sr.ID, sr.Outcome)
		}
	}
	if _, err := f.Stat(tpanOld); err == nil {
		t.Error("trùng bản mà vẫn tạo .old")
	}
	if restarted(f) {
		t.Error("trùng bản mà vẫn khởi động lại tpan")
	}
}

// Kiểm chứng không đạt (file mới không chạy) thì engine trả file cũ về.
func TestSelfUpdateBrokenBinaryRollsBack(t *testing.T) {
	s, f := newSelfEnv(t, "v1.1.0")
	p, err := s.Prepare(ctx, selfTestBase)
	if err != nil {
		t.Fatal(err)
	}
	f.Handle([]string{TpanBin, "version"}, func(system.Cmd) system.Result {
		return system.Result{ExitCode: 126, Stderr: "cannot execute binary file: Exec format error"}
	})
	rep, err := p.Execute(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Job.Status == engine.JobSucceeded {
		t.Fatal("file mới hỏng mà job thành công")
	}
	if got := fileData(t, f, TpanBin); got != "tpan v1.0.0" {
		t.Errorf("không trả file cũ về: TpanBin = %q", got)
	}
	if len(rep.Checks) != 1 || len(rep.Checks[0].ManualFix) == 0 {
		t.Errorf("kiểm chứng hỏng thiếu cách sửa tay: %+v", rep.Checks)
	}
}

func TestSelfUpdateBadURL(t *testing.T) {
	for _, u := range []string{"http://example.com/x", "https://user:pw@example.com", "https://example.com/a b", "ftp://example.com", "https://example.com/x?a=$(id)"} {
		s, f := newSelfEnv(t, "v1.1.0")
		_, err := s.Prepare(ctx, u)
		if code := refusedCode(t, err); code != "self_url_invalid" {
			t.Errorf("%q: mã %q", u, code)
		}
		if len(f.Ran()) != 0 {
			t.Errorf("%q: gốc tải sai mà vẫn chạy lệnh %v", u, f.Ran())
		}
	}
	// Kiến trúc lạ cũng bị từ chối trước khi tải.
	s, _ := newSelfEnv(t, "v1.1.0")
	s.Arch = "386"
	if _, err := s.Prepare(ctx, selfTestBase); refusedCode(t, err) != "self_url_invalid" {
		t.Errorf("Arch 386: %v", err)
	}
}

func TestSelfRollback(t *testing.T) {
	s, f := newSelfEnv(t, "v1.1.0")
	_, err := s.PrepareRollback(ctx)
	if code := refusedCode(t, err); code != "self_no_old" {
		t.Errorf("không có .old: mã %q", code)
	}

	f.SetFile(tpanOld, []byte("tpan v0.9.0"), 0o755, system.Root)
	st, err := s.Status(ctx)
	if err != nil || !st.HasOld || st.URL != DefaultSelfURL {
		t.Errorf("Status = %+v, %v", st, err)
	}
	p, err := s.PrepareRollback(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if p.Plan.Kind != "self.rollback" {
		t.Errorf("Kind = %q", p.Plan.Kind)
	}
	f.Handle([]string{"systemctl", "is-active", "tpan"}, func(system.Cmd) system.Result { return system.Result{Stdout: "active\n"} })
	rep, err := p.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("job %s: %v %+v", rep.Job.Status, err, rep.Steps)
	}
	if got := fileData(t, f, TpanBin); got != "tpan v0.9.0" {
		t.Errorf("TpanBin sau quay về = %q", got)
	}
	if got := fileData(t, f, tpanNew); got != "tpan v1.0.0" {
		t.Errorf("bản vừa thay ra = %q, muốn nằm ở %s", got, tpanNew)
	}
	if _, err := f.Stat(tpanOld); err == nil {
		t.Error(".old vẫn còn sau khi quay về")
	}
	if !restarted(f) || !strings.Contains(rep.Checks[0].Summary.Vi, "v0.9.0") {
		t.Errorf("restart %v, kiểm chứng %+v", restarted(f), rep.Checks)
	}
}
