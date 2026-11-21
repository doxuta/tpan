package steps

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/s3"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

// ================================================================ máy giả M18

// s3Sys là FakeSystem cộng `sha256sum` tính thật trên nội dung giả, đủ cho
// mọi bước M18 (chúng chỉ chạy đúng lệnh đó, và tar ở PanelBundle).
func s3Sys(t *testing.T) *system.FakeSystem {
	t.Helper()
	f := system.NewFake()
	f.SetDir("/var/lib/tpan", 0o755, system.Root)
	f.Handle([]string{"sha256sum"}, func(c system.Cmd) system.Result {
		p := c.Args[len(c.Args)-1]
		data, err := f.ReadFile(p)
		if err != nil {
			return system.Result{ExitCode: 1, Stderr: err.Error()}
		}
		return system.Result{Stdout: sha256Hex(data) + "  " + p + "\n"}
	})
	return f
}

const (
	s3TestDir  = site.BackupRoot + "/example.com"
	s3TestFile = s3TestDir + "/example.com-20261121-100000.tar.gz"
	s3TestKey  = "sites/example.com/example.com-20261121-100000.tar.gz"
)

var s3TestData = []byte("noi dung ban sao luu gia, du dai de doi mot byte o giua file")

// s3TestPush đặt file cục bộ lên máy giả và trả bước đẩy nó.
func s3TestPush(f *system.FakeSystem, store ObjectStore) *S3Push {
	f.SetDir(site.BackupRoot, 0o700, system.Root)
	f.SetFile(s3TestFile, s3TestData, 0o600, system.Root)
	return &S3Push{
		Store: store, ID: "b1", Key: s3TestKey,
		Path: s3TestFile, Size: int64(len(s3TestData)), SHA256: sha256Hex(s3TestData),
	}
}

// s3Tamper là kho trả về dữ liệu khác thứ đã gửi: mỗi lần Get thì đổi một
// byte của object trước (như đĩa của kho hỏng sau lúc ghi).
type s3Tamper struct{ *s3.Mem }

func (t s3Tamper) Get(ctx context.Context, key string, w io.Writer) (int64, string, error) {
	t.Corrupt(key)
	return t.Mem.Get(ctx, key, w)
}

func s3NoPath(t *testing.T, f *system.FakeSystem, paths ...string) {
	t.Helper()
	snap := f.Snapshot()
	for _, p := range paths {
		if _, ok := snap[p]; ok {
			t.Errorf("%s vẫn còn", p)
		}
	}
}

// ==================================================================== S3Push

// B2: kho trả dữ liệu khác thứ đã gửi thì bước lỗi ErrRemoteMismatch, và
// object bị xoá (không có gì trên kho để ai đó tưởng là bản tốt).
func TestS3PushDocLaiMoiDanhDau(t *testing.T) {
	f, mem := s3Sys(t), s3.NewMem()
	s := s3TestPush(f, s3Tamper{mem})
	err := s.Apply(context.Background(), f)
	if !errors.Is(err, ErrRemoteMismatch) {
		t.Fatalf("lỗi = %v, muốn ErrRemoteMismatch", err)
	}
	if errors.Is(err, ErrBackupLocal) {
		t.Errorf("lỗi đọc lại không phải lỗi của bản cục bộ: %v", err)
	}
	if _, ok := mem.Objects()[s3TestKey]; ok {
		t.Error("object sai vẫn còn trên kho")
	}
	if ok, _ := s.Check(context.Background(), f); ok {
		t.Error("Check true sau Apply lỗi")
	}
}

// B9: lần tải dở của chính key (lần chạy trước bị giết) được dọn trước khi tải.
func TestS3PushDonTaiDo(t *testing.T) {
	f, mem := s3Sys(t), s3.NewMem()
	mem.AddUpload(s3TestKey, time.Now())
	mem.AddUpload("sites/other.com/x.tar.gz", time.Now())
	s := s3TestPush(f, mem)
	if err := s.Apply(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	for _, k := range mem.Uploads() {
		if k == s3TestKey {
			t.Errorf("còn lần tải dở của %s", k)
		}
	}
	if len(mem.Uploads()) != 1 {
		t.Errorf("lần tải dở của key khác bị đụng: %v", mem.Uploads())
	}

	t.Run("kho không hỗ trợ huỷ (501) thì bỏ qua", func(t *testing.T) {
		f, mem := s3Sys(t), s3.NewMem()
		mem.Fail("abort", "", &s3.Error{Op: "abort", Status: 501, Code: "NotImplemented"})
		if err := s3TestPush(f, mem).Apply(context.Background(), f); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("lỗi huỷ khác thì trả nguyên", func(t *testing.T) {
		f, mem := s3Sys(t), s3.NewMem()
		mem.Fail("abort", "", &s3.Error{Op: "abort", Status: 403, Code: "AccessDenied"})
		err := s3TestPush(f, mem).Apply(context.Background(), f)
		var se *s3.Error
		if !errors.As(err, &se) || se.Status != 403 {
			t.Fatalf("lỗi = %v", err)
		}
		if mem.Calls("put") != 0 {
			t.Error("vẫn tải lên sau lỗi huỷ")
		}
	})
}

func TestS3PushThuong(t *testing.T) {
	f, mem := s3Sys(t), s3.NewMem()
	s := s3TestPush(f, mem)
	ctx := context.Background()
	if info := s.Info(); info.ID != "s3.push:b1" || len(info.Files) != 0 || info.Title.Vi == "" || info.Why.En == "" {
		t.Errorf("Info = %+v", info)
	}
	if ok, err := s.Check(ctx, f); ok || err != nil {
		t.Fatalf("Check trước Apply = %v, %v", ok, err)
	}
	if err := s.Apply(ctx, f); err != nil {
		t.Fatal(err)
	}
	if got := mem.Objects()[s3TestKey]; !bytes.Equal(got, s3TestData) {
		t.Errorf("object = %q", got)
	}
	if ok, err := s.Check(ctx, f); !ok || err != nil {
		t.Errorf("Check sau Apply = %v, %v", ok, err)
	}
	if s.ETag == "" {
		t.Error("ETag rỗng sau Apply")
	}
	if mem.Calls("get") != 1 {
		t.Errorf("đọc lại %d lần, muốn 1", mem.Calls("get"))
	}
	ran := f.Ran()
	if len(ran) != 1 || ran[0].Name != "sha256sum" {
		t.Errorf("lệnh đã chạy = %v", ran)
	}
	if err := s.Undo(ctx, f); err != nil {
		t.Fatal(err)
	}
	if len(mem.Objects()) != 0 {
		t.Errorf("Undo không xoá object: %v", mem.Objects())
	}
}

func TestS3PushBanCucBoHong(t *testing.T) {
	cases := []struct {
		name string
		set  func(f *system.FakeSystem, s *S3Push)
	}{
		{"sai sha256", func(_ *system.FakeSystem, s *S3Push) { s.SHA256 = strings.Repeat("0", 64) }},
		{"sai cỡ", func(_ *system.FakeSystem, s *S3Push) { s.Size++ }},
		{"không có file", func(f *system.FakeSystem, _ *S3Push) { _ = f.Remove(s3TestFile) }},
		{"là symlink", func(f *system.FakeSystem, _ *S3Push) {
			_ = f.Remove(s3TestFile)
			f.SetFile(s3TestDir+"/that", s3TestData, 0o600, system.Root)
			f.SetSymlink(s3TestDir+"/that", s3TestFile)
		}},
		{"sha256sum lỗi", func(f *system.FakeSystem, _ *S3Push) {
			f.Handle([]string{"sha256sum"}, func(system.Cmd) system.Result { return system.Result{ExitCode: 1, Stderr: "I/O error"} })
		}},
		{"không mở được", func(f *system.FakeSystem, _ *S3Push) { f.Fail("open", s3TestFile, fs.ErrPermission) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, mem := s3Sys(t), s3.NewMem()
			s := s3TestPush(f, mem)
			c.set(f, s)
			err := s.Apply(context.Background(), f)
			if !errors.Is(err, ErrBackupLocal) {
				t.Fatalf("lỗi = %v, muốn ErrBackupLocal", err)
			}
			if n := mem.Calls("put"); n != 0 {
				t.Errorf("đã Put %d lần", n)
			}
			if len(mem.Objects()) != 0 {
				t.Errorf("kho có object: %v", mem.Objects())
			}
		})
	}
}

func TestS3PushKiemThamSo(t *testing.T) {
	cases := []struct {
		name string
		set  func(s *S3Push)
	}{
		{"không có kho", func(s *S3Push) { s.Store = nil }},
		{"không có id", func(s *S3Push) { s.ID = "" }},
		{"file ngoài thư mục backup", func(s *S3Push) { s.Path = "/etc/shadow" }},
		{"đường dẫn có ..", func(s *S3Push) { s.Path = s3TestDir + "/../../../etc/shadow" }},
		{"key có ..", func(s *S3Push) { s.Key = "sites/../x" }},
		{"sha256 sai dạng", func(s *S3Push) { s.SHA256 = "abc" }},
		{"cỡ âm", func(s *S3Push) { s.Size = -1 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, mem := s3Sys(t), s3.NewMem()
			s := s3TestPush(f, mem)
			c.set(s)
			if err := s.Apply(context.Background(), f); err == nil {
				t.Fatal("Apply không lỗi")
			}
			if mem.Calls("put") != 0 || len(f.Ran()) != 0 {
				t.Error("đã chạm kho hay chạy lệnh trước khi kiểm tham số")
			}
		})
	}
}

// Lỗi của kho đi ra nguyên vẹn để core phân loại bằng s3.Classify.
func TestS3PushKhoLoi503(t *testing.T) {
	f, mem := s3Sys(t), s3.NewMem()
	mem.Fail("put", "", &s3.Error{Op: "put", Status: 503, Code: "SlowDown"})
	err := s3TestPush(f, mem).Apply(context.Background(), f)
	var se *s3.Error
	if !errors.As(err, &se) || se.Status != 503 {
		t.Fatalf("lỗi = %v, muốn *s3.Error 503", err)
	}
	if errors.Is(err, ErrBackupLocal) || errors.Is(err, ErrRemoteMismatch) {
		t.Errorf("lỗi của đích bị gắn nhầm là lỗi của bản: %v", err)
	}
	if !s3.IsTargetError(err) {
		t.Errorf("Classify = %s", s3.Classify(err))
	}
}

// Đọc lại lỗi (mạng đứt sau Put): object chưa được kiểm thì không để lại.
func TestS3PushDocLaiLoiThiXoa(t *testing.T) {
	f, mem := s3Sys(t), s3.NewMem()
	mem.Fail("get", "", &s3.Error{Op: "get", Status: 500, Code: "InternalError"})
	err := s3TestPush(f, mem).Apply(context.Background(), f)
	var se *s3.Error
	if !errors.As(err, &se) || se.Op != "get" {
		t.Fatalf("lỗi = %v", err)
	}
	if len(mem.Objects()) != 0 {
		t.Error("object chưa đọc lại được vẫn còn")
	}
}

// ==================================================================== S3Pull

func s3TestPull(f *system.FakeSystem, mem *s3.Mem) *S3Pull {
	mem.SetObject(s3TestKey, s3TestData)
	return &S3Pull{
		Store: mem, ID: "b1", Key: s3TestKey, Dest: s3TestFile, Dir: s3TestDir,
		Size: int64(len(s3TestData)), SHA256: sha256Hex(s3TestData),
	}
}

// B4: object hỏng trên kho thì không còn gì trên máy, kể cả .part.
func TestS3PullSaiSha256KhongDeLaiFile(t *testing.T) {
	f, mem := s3Sys(t), s3.NewMem()
	s := s3TestPull(f, mem)
	mem.Corrupt(s3TestKey)
	err := s.Apply(context.Background(), f)
	if err == nil {
		t.Fatal("Apply không lỗi với object hỏng")
	}
	if !strings.Contains(err.Error(), s.SHA256) {
		t.Errorf("lỗi không nói sha256 mong đợi: %v", err)
	}
	s3NoPath(t, f, s3TestFile, s3TestFile+".part")
}

func TestS3PullThuong(t *testing.T) {
	f, mem := s3Sys(t), s3.NewMem()
	s := s3TestPull(f, mem)
	ctx := context.Background()
	if info := s.Info(); info.ID != "s3.pull:b1" || len(info.Files) != 0 {
		t.Errorf("Info = %+v", info)
	}
	if ok, err := s.Check(ctx, f); ok || err != nil {
		t.Fatalf("Check trước Apply = %v, %v", ok, err)
	}
	if err := s.Apply(ctx, f); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.Check(ctx, f); !ok || err != nil {
		t.Fatalf("Check sau Apply = %v, %v", ok, err)
	}
	e := f.Snapshot()[s3TestFile]
	if e.Data != string(s3TestData) || e.Mode != 0o600 || e.Owner != system.Root {
		t.Errorf("Dest = %+v", e)
	}
	if d := f.Snapshot()[s3TestDir]; d.Mode != fs.ModeDir|0o700 {
		t.Errorf("Dir mode = %v", d.Mode)
	}
	s3NoPath(t, f, s3TestFile+".part")
	if err := s.Undo(ctx, f); err != nil {
		t.Fatal(err)
	}
	s3NoPath(t, f, s3TestFile)
}

// Check theo trạng thái: Dest đã đúng thì bước được bỏ qua, không gọi kho.
func TestS3PullDestDungThiBoQua(t *testing.T) {
	f, mem := s3Sys(t), s3.NewMem()
	s := s3TestPull(f, mem)
	f.SetFile(s3TestFile, s3TestData, 0o600, system.Root)
	if ok, err := s.Check(context.Background(), f); !ok || err != nil {
		t.Fatalf("Check = %v, %v", ok, err)
	}
	if mem.Calls("get") != 0 {
		t.Error("Check gọi kho")
	}
}

// S8: Dest sai (bản tải hỏng lần trước) và .part còn sót thì Apply thay.
func TestS3PullDestSaiThiThay(t *testing.T) {
	f, mem := s3Sys(t), s3.NewMem()
	s := s3TestPull(f, mem)
	bad := bytes.Clone(s3TestData)
	bad[3] ^= 0xff
	f.SetFile(s3TestFile, bad, 0o600, system.Root)
	f.SetFile(s3TestFile+".part", []byte("do"), 0o600, system.Root)
	ctx := context.Background()
	if ok, _ := s.Check(ctx, f); ok {
		t.Fatal("Check true với Dest sai")
	}
	if err := s.Apply(ctx, f); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.Check(ctx, f); !ok || err != nil {
		t.Fatalf("Check sau Apply = %v, %v", ok, err)
	}
	s3NoPath(t, f, s3TestFile+".part")
}

func TestS3PullDayDia(t *testing.T) {
	f, mem := s3Sys(t), s3.NewMem()
	s := s3TestPull(f, mem)
	f.SetDisk("/", system.DiskUsage{Total: 1000, Free: 10})
	if err := s.Apply(context.Background(), f); err == nil || !strings.Contains(err.Error(), "không đủ chỗ trống") {
		t.Fatalf("lỗi = %v", err)
	}
	if mem.Calls("get") != 0 {
		t.Error("vẫn tải khi đĩa đầy")
	}
}

func TestS3PullKiemThamSo(t *testing.T) {
	cases := []struct {
		name string
		set  func(s *S3Pull)
	}{
		{"không có kho", func(s *S3Pull) { s.Store = nil }},
		{"Dir rỗng", func(s *S3Pull) { s.Dir = "" }},
		{"Dir ngoài thư mục backup", func(s *S3Pull) { s.Dir, s.Dest = "/etc", "/etc/passwd" }},
		{"Dest không ngay dưới Dir", func(s *S3Pull) { s.Dest = s3TestDir + "/a/b.tar.gz" }},
		{"Dest có ..", func(s *S3Pull) { s.Dest = s3TestDir + "/../other.com/x.tar.gz" }},
		{"Dest là Dir", func(s *S3Pull) { s.Dest = s3TestDir }},
		{"cỡ 0", func(s *S3Pull) { s.Size = 0 }},
		{"sha256 sai dạng", func(s *S3Pull) { s.SHA256 = strings.Repeat("G", 64) }},
		{"key sai", func(s *S3Pull) { s.Key = "/abs" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, mem := s3Sys(t), s3.NewMem()
			s := s3TestPull(f, mem)
			c.set(s)
			if _, err := s.Check(context.Background(), f); err == nil {
				t.Error("Check không lỗi")
			}
			if err := s.Apply(context.Background(), f); err == nil {
				t.Error("Apply không lỗi")
			}
			if mem.Calls("get") != 0 {
				t.Error("đã gọi kho")
			}
		})
	}
}

// =================================================================== S3Probe

func TestS3ProbeThuong(t *testing.T) {
	f, mem := s3Sys(t), s3.NewMem()
	s := &S3Probe{Store: mem, Label: "s3.example.com/bucket/tpan/may"}
	ctx := context.Background()
	info := s.Info()
	if info.ID != "s3.probe:s3.example.com/bucket/tpan/may" || info.NoUndo.IsZero() {
		t.Errorf("Info = %+v", info)
	}
	if ok, err := s.Check(ctx, f); ok || err != nil {
		t.Fatalf("Check trước Apply = %v, %v", ok, err)
	}
	if err := s.Apply(ctx, f); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.Check(ctx, f); !ok || err != nil {
		t.Errorf("Check sau Apply = %v, %v", ok, err)
	}
	if len(mem.Objects()) != 0 {
		t.Errorf("object thử còn trên kho: %v", mem.Objects())
	}
	for op, n := range map[string]int{"put": 1, "get": 1, "head": 2, "delete": 1} {
		if mem.Calls(op) != n {
			t.Errorf("%s gọi %d lần, muốn %d", op, mem.Calls(op), n)
		}
	}
	if err := s.Undo(ctx, f); err != nil {
		t.Errorf("Undo = %v", err)
	}
}

// Hỏng giữa chừng thì vẫn cố xoá object thử, rồi báo lỗi của kho.
func TestS3ProbeGetLoiVanXoa(t *testing.T) {
	f, mem := s3Sys(t), s3.NewMem()
	mem.Fail("get", "", &s3.Error{Op: "get", Status: 403, Code: "AccessDenied"})
	err := (&S3Probe{Store: mem, Label: "x"}).Apply(context.Background(), f)
	var se *s3.Error
	if !errors.As(err, &se) || se.Status != 403 {
		t.Fatalf("lỗi = %v", err)
	}
	if mem.Calls("delete") != 1 {
		t.Errorf("delete gọi %d lần", mem.Calls("delete"))
	}
	if len(mem.Objects()) != 0 {
		t.Errorf("object thử còn trên kho: %v", mem.Objects())
	}
}

// Kho đọc lại khác thứ đã ghi thì lỗi, và object thử vẫn bị xoá.
func TestS3ProbeDocLaiKhac(t *testing.T) {
	f, mem := s3Sys(t), s3.NewMem()
	err := (&S3Probe{Store: s3Tamper{mem}, Label: "x"}).Apply(context.Background(), f)
	if err == nil {
		t.Fatal("Apply không lỗi")
	}
	if len(mem.Objects()) != 0 {
		t.Errorf("object thử còn trên kho: %v", mem.Objects())
	}
}

// ============================================================= Bước bí mật

// B3: nội dung file bí mật không bao giờ vào bản xem trước.
func TestBuocBiMatKhongKhaiFiles(t *testing.T) {
	const oldMark = "OLD-SECRET-7f3a9c"
	const newMark = "NEW-SECRET-2b8e41"
	sf := &SecretFile{Path: S3KeysFile, Content: system.NewSecret(newMark)}
	sa := &SecretAbsent{Path: BackupKeyFile}
	for _, s := range []engine.Step{sf, sa} {
		if info := s.Info(); len(info.Files) != 0 {
			t.Errorf("%s khai Files: %+v", info.ID, info.Files)
		}
	}
	if id := sf.Info().ID; id != "secret.file:"+S3KeysFile {
		t.Errorf("ID = %s", id)
	}
	if id := sa.Info().ID; id != "secret.absent:"+BackupKeyFile {
		t.Errorf("ID = %s", id)
	}

	f := s3Sys(t)
	f.SetDir("/etc/tpan", 0o700, system.Root)
	f.SetFile(BackupKeyFile, []byte(oldMark), 0o600, system.Root)
	f.SetFile(S3KeysFile, []byte(oldMark), 0o600, system.Root)
	preview := func(steps ...engine.Step) string {
		t.Helper()
		pv, err := engine.New(f, engine.Options{}).Preview(context.Background(), engine.Plan{Kind: "t", Steps: steps})
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(pv)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	leaked := func(js string) bool {
		for _, m := range []string{oldMark, newMark} {
			if strings.Contains(js, m) || strings.Contains(js, base64.StdEncoding.EncodeToString([]byte(m))) {
				return true
			}
		}
		return false
	}
	if js := preview(sa, sf); leaked(js) {
		t.Errorf("bản xem trước lộ bí mật: %s", js)
	}
	// Đối chứng: một Absent khai Files (như trước M18) thì phép quét trên bắt
	// được nội dung cũ. Không có ca này thì phép quét có thể qua oan.
	if js := preview(&Absent{Path: BackupKeyFile, secret: true}); !leaked(js) {
		t.Error("phép quét không bắt được nội dung cũ trong old")
	}

	ctx := context.Background()
	for _, s := range []engine.Step{
		&File{Path: S3KeysFile, Content: []byte("x"), Perm: 0o600},
		&Absent{Path: BackupKeyFile},
	} {
		if err := s.Apply(ctx, f); err == nil {
			t.Errorf("%s: Apply không từ chối file bí mật", s.Info().ID)
		}
		if _, err := s.Check(ctx, f); err == nil {
			t.Errorf("%s: Check không từ chối file bí mật", s.Info().ID)
		}
	}
	if f.Snapshot()[BackupKeyFile].Data != oldMark {
		t.Error("file bí mật bị đổi bởi bước thường")
	}
}

func TestSecretFileGhiVaHoanTac(t *testing.T) {
	ctx := context.Background()
	f := s3Sys(t)
	f.SetDir("/etc/tpan", 0o700, system.Root)
	f.SetFile(S3KeysFile, []byte("cu"), 0o644, system.Root)
	s := &SecretFile{Path: S3KeysFile, Content: system.NewSecret(`{"access_key":"a"}`)}
	if ok, err := s.Check(ctx, f); ok || err != nil {
		t.Fatalf("Check trước = %v, %v", ok, err)
	}
	if err := s.Apply(ctx, f); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.Check(ctx, f); !ok || err != nil {
		t.Fatalf("Check sau = %v, %v", ok, err)
	}
	if e := f.Snapshot()[S3KeysFile]; e.Data != `{"access_key":"a"}` || e.Mode != 0o600 || e.Owner != system.Root {
		t.Errorf("file = %+v", e)
	}
	if err := s.Undo(ctx, f); err != nil {
		t.Fatal(err)
	}
	if e := f.Snapshot()[S3KeysFile]; e.Data != "cu" || e.Mode != 0o644 {
		t.Errorf("Undo không trả nội dung cũ: %+v", e)
	}

	t.Run("file mới thì Undo xoá", func(t *testing.T) {
		f := s3Sys(t)
		f.SetDir("/etc/tpan", 0o700, system.Root)
		s := &SecretFile{Path: S3CAFile, Content: system.NewSecret("pem")}
		if err := s.Apply(ctx, f); err != nil {
			t.Fatal(err)
		}
		if err := s.Undo(ctx, f); err != nil {
			t.Fatal(err)
		}
		s3NoPath(t, f, S3CAFile)
	})
	t.Run("đường dẫn không bí mật bị từ chối", func(t *testing.T) {
		f := s3Sys(t)
		s := &SecretFile{Path: "/etc/tpan/khac.json", Content: system.NewSecret("x")}
		if err := s.Apply(ctx, f); err == nil {
			t.Error("Apply không lỗi")
		}
		if _, err := s.Check(ctx, f); err == nil {
			t.Error("Check không lỗi")
		}
	})
	t.Run("Undo khi chưa Apply", func(t *testing.T) {
		if err := (&SecretFile{Path: S3CAFile}).Undo(ctx, s3Sys(t)); !errors.Is(err, errNotApplied) {
			t.Errorf("lỗi = %v", err)
		}
	})
}

func TestSecretAbsentHoanTac(t *testing.T) {
	ctx := context.Background()
	f := s3Sys(t)
	f.SetDir("/etc/tpan", 0o700, system.Root)
	f.SetFile(BackupKeyFile, []byte("tpan-key-1-abc"), 0o600, system.Root)
	s := &SecretAbsent{Path: BackupKeyFile}
	if ok, err := s.Check(ctx, f); ok || err != nil {
		t.Fatalf("Check trước = %v, %v", ok, err)
	}
	if err := s.Apply(ctx, f); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.Check(ctx, f); !ok || err != nil {
		t.Fatalf("Check sau = %v, %v", ok, err)
	}
	if err := s.Undo(ctx, f); err != nil {
		t.Fatal(err)
	}
	if e := f.Snapshot()[BackupKeyFile]; e.Data != "tpan-key-1-abc" || e.Mode != 0o600 {
		t.Errorf("Undo không trả lại file: %+v", e)
	}
	if err := (&SecretAbsent{Path: "/etc/passwd"}).Apply(ctx, f); err == nil {
		t.Error("SecretAbsent nhận đường dẫn không bí mật")
	}
}
