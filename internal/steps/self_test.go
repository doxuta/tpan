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
)

const (
	selfBin  = "/usr/local/bin/tpan"
	selfNew  = "/usr/local/bin/tpan.new"
	selfOld  = "/usr/local/bin/tpan.old"
	selfBase = "https://example.com/releases/v1.2.3"
)

var selfData = []byte("ELF giả của tpan v1.2.3\n")

// selfFake là FakeSystem với curl và `tpan.new version` giả. Không dùng
// newMachine: handler curl chung của nó tìm "-o" trong mọi lệnh curl, còn lệnh
// tải SHA256SUMS ở đây in ra stdout. sums là nội dung SHA256SUMS; data là file
// "tải" về.
type selfFake struct {
	*system.FakeSystem
	sums string
	data []byte
}

func newSelfFake(t *testing.T) *selfFake {
	t.Helper()
	f := &selfFake{FakeSystem: system.NewFake(), data: selfData}
	f.sums = sha256Hex(selfData) + "  tpan-linux-amd64\n" + strings.Repeat("0", 64) + "  tpan-linux-arm64\n"
	f.SetDir("/usr/local/bin", 0o755, system.Root)
	f.Handle([]string{"curl", "-fsSL", "--max-time", "30"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: f.sums}
	})
	f.Handle([]string{"curl", "-fsSL", "--max-time", "300"}, func(c system.Cmd) system.Result {
		i := slices.Index(c.Args, "-o")
		f.SetFile(c.Args[i+1], f.data, 0o644, system.Root)
		return system.Result{}
	})
	f.Handle([]string{selfNew, "version"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "tpan v1.2.3 (commit abc1234, build 2026-11-19)\n"}
	})
	return f
}

func dlStep(v *string) *SelfDownload {
	return &SelfDownload{Base: selfBase + "/", Arch: "amd64", Dest: selfNew, NewVersion: v}
}

// SelfDownload không dùng khuôn lifecycle: theo hợp đồng, Check của một bước
// mới luôn false (chưa tải SHA256SUMS lần này), nên "chạy lại thì bỏ qua"
// không áp dụng. Mỗi lần chạy self-update đều tải và kiểm lại.
func TestSelfDownload(t *testing.T) {
	ctx := context.Background()

	t.Run("Check, Apply, Check, Undo", func(t *testing.T) {
		f := newSelfFake(t)
		before := f.Snapshot()
		var v string
		s := dlStep(&v)
		if done, err := s.Check(ctx, f); err != nil || done {
			t.Fatalf("Check trước Apply = %v, %v", done, err)
		}
		if len(f.Ran()) != 0 {
			t.Fatalf("Check chạy lệnh: %q", ranStrings(f.Ran()))
		}
		if err := s.Apply(ctx, f); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if done, err := s.Check(ctx, f); err != nil || !done {
			t.Fatalf("Check sau Apply = %v, %v", done, err)
		}
		if v != "v1.2.3" {
			t.Errorf("NewVersion = %q", v)
		}
		fi, err := f.Stat(selfNew)
		if err != nil || fi.Mode.Perm() != 0o755 || fi.Owner != system.Root {
			t.Errorf("%s: %+v, %v", selfNew, fi, err)
		}
		want := []string{
			"curl -fsSL --max-time 30 --proto-redir =https " + selfBase + "/SHA256SUMS",
			"curl -fsSL --max-time 300 --proto-redir =https -o " + selfNew + " " + selfBase + "/tpan-linux-amd64",
			selfNew + " version",
		}
		if got := ranStrings(f.Ran()); !reflect.DeepEqual(got, want) {
			t.Errorf("lệnh = %q\nmuốn  %q", got, want)
		}
		if err := s.Undo(ctx, f); err != nil {
			t.Fatalf("Undo: %v", err)
		}
		if !reflect.DeepEqual(before, f.Snapshot()) {
			t.Error("sau Undo máy khác trước Apply")
		}
	})

	t.Run("NewVersion nil vẫn chạy", func(t *testing.T) {
		f := newSelfFake(t)
		if err := dlStep(nil).Apply(ctx, f); err != nil {
			t.Fatalf("Apply: %v", err)
		}
	})

	t.Run("file .new còn sót được thay, Undo trả lại", func(t *testing.T) {
		f := newSelfFake(t)
		f.SetFile(selfNew, []byte("dở dang"), 0o600, system.Root)
		before := f.Snapshot()
		s := dlStep(nil)
		if err := s.Apply(ctx, f); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if done, _ := s.Check(ctx, f); !done {
			t.Error("Check sau Apply false")
		}
		if err := s.Undo(ctx, f); err != nil {
			t.Fatalf("Undo: %v", err)
		}
		if !reflect.DeepEqual(before, f.Snapshot()) {
			t.Error("sau Undo máy khác trước Apply")
		}
	})

	t.Run("bước mới thì Check lại false", func(t *testing.T) {
		f := newSelfFake(t)
		if err := dlStep(nil).Apply(ctx, f); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if done, err := dlStep(nil).Check(ctx, f); err != nil || done {
			t.Errorf("Check của bước mới = %v, %v; muốn false", done, err)
		}
	})

	t.Run("file trên đĩa bị đổi sau Apply thì Check false", func(t *testing.T) {
		f := newSelfFake(t)
		s := dlStep(nil)
		if err := s.Apply(ctx, f); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		f.SetFile(selfNew, []byte("khác"), 0o755, system.Root)
		if done, _ := s.Check(ctx, f); done {
			t.Error("Check true với file đã bị đổi")
		}
	})

	t.Run("Undo khi chưa Apply", func(t *testing.T) {
		if err := dlStep(nil).Undo(ctx, newSelfFake(t)); !errors.Is(err, errNotApplied) {
			t.Errorf("Undo = %v", err)
		}
	})
}

func TestSelfDownloadErrors(t *testing.T) {
	ctx := context.Background()
	exit := func(code int, stderr string) func(system.Cmd) system.Result {
		return func(system.Cmd) system.Result { return system.Result{ExitCode: code, Stderr: stderr} }
	}
	cases := []struct {
		name    string
		setup   func(f *selfFake)
		context string
		exitErr bool
		noBin   bool // không được tải file chạy
	}{
		{
			name:    "sha256 sai",
			setup:   func(f *selfFake) { f.data = []byte("ELF bị sửa giữa đường") },
			context: "sha256",
		},
		{
			name:    "tải SHA256SUMS lỗi",
			setup:   func(f *selfFake) { f.Handle([]string{"curl", "-fsSL", "--max-time", "30"}, exit(22, "curl: (22) 404")) },
			context: "tải SHA256SUMS",
			exitErr: true,
			noBin:   true,
		},
		{
			name:    "SHA256SUMS thiếu dòng của kiến trúc",
			setup:   func(f *selfFake) { f.sums = sha256Hex(selfData) + "  tpan-linux-amd64.sig\n" },
			context: "không có dòng nào cho tpan-linux-amd64",
			noBin:   true,
		},
		{
			name: "tải file chạy lỗi",
			setup: func(f *selfFake) {
				f.Handle([]string{"curl", "-fsSL", "--max-time", "300"}, exit(28, "curl: (28) timeout"))
			},
			context: "tải tpan-linux-amd64",
			exitErr: true,
		},
		{
			name:    "bản mới không chạy được",
			setup:   func(f *selfFake) { f.Handle([]string{selfNew, "version"}, exit(126, "cannot execute binary file")) },
			context: "chạy thử bản vừa tải",
			exitErr: true,
		},
		{
			name: "bản mới in output lạ",
			setup: func(f *selfFake) {
				f.Handle([]string{selfNew, "version"}, func(system.Cmd) system.Result { return system.Result{Stdout: "hello\n"} })
			},
			context: "đọc bản của",
		},
		{
			name:    "chmod lỗi",
			setup:   func(f *selfFake) { f.Fail("chmod", selfNew, errors.New("chỉ đọc")) },
			context: "chỉ đọc",
		},
	}
	for _, tc := range cases {
		for _, leftover := range []bool{false, true} {
			name := tc.name
			if leftover {
				name += " (có .new cũ)"
			}
			t.Run(name, func(t *testing.T) {
				f := newSelfFake(t)
				if leftover {
					f.SetFile(selfNew, []byte("dở dang"), 0o600, system.Root)
				}
				tc.setup(f)
				before := f.Snapshot()
				var v string
				s := dlStep(&v)
				err := s.Apply(ctx, f)
				if tc.exitErr {
					wantExit(t, err, tc.context)
				} else if err == nil || !strings.Contains(err.Error(), tc.context) {
					t.Fatalf("Apply = %v, muốn lỗi chứa %q", err, tc.context)
				}
				if !reflect.DeepEqual(before, f.Snapshot()) {
					t.Error("Apply lỗi mà máy không về như cũ")
				}
				if v != "" {
					t.Errorf("NewVersion = %q dù Apply lỗi", v)
				}
				if done, _ := s.Check(ctx, f); done {
					t.Error("Check true sau Apply lỗi")
				}
				for _, c := range f.Ran() {
					if tc.noBin && slices.Contains(c.Args, "-o") {
						t.Errorf("vẫn tải file chạy: %s", c.String())
					}
				}
			})
		}
	}
}

func TestSelfDownloadBadParams(t *testing.T) {
	ctx := context.Background()
	cases := map[string]SelfDownload{
		"http lạ":            {Base: "http://example.com", Arch: "amd64", Dest: selfNew},
		"http giả loopback":  {Base: "http://127.0.0.1.evil.com", Arch: "amd64", Dest: selfNew},
		"http localhost giả": {Base: "http://localhost.example.com/", Arch: "amd64", Dest: selfNew},
		"ftp":                {Base: "ftp://example.com/pub", Arch: "amd64", Dest: selfNew},
		"file":               {Base: "file:///etc", Arch: "amd64", Dest: selfNew},
		"có dấu cách":        {Base: "https://example.com/a b", Arch: "amd64", Dest: selfNew},
		"xuống dòng":         {Base: "https://example.com/\n-o/etc/passwd", Arch: "amd64", Dest: selfNew},
		"không scheme":       {Base: "example.com/releases", Arch: "amd64", Dest: selfNew},
		"tuỳ chọn curl":      {Base: "-o/etc/passwd", Arch: "amd64", Dest: selfNew},
		"thiếu host":         {Base: "https:///releases", Arch: "amd64", Dest: selfNew},
		"user:pass":          {Base: "https://u:p@example.com", Arch: "amd64", Dest: selfNew},
		"query":              {Base: "https://example.com/?a=1", Arch: "amd64", Dest: selfNew},
		"fragment":           {Base: "https://example.com/#x", Arch: "amd64", Dest: selfNew},
		"Base rỗng":          {Base: "", Arch: "amd64", Dest: selfNew},
		"Arch lạ":            {Base: selfBase, Arch: "386", Dest: selfNew},
		"Arch chèn lệnh":     {Base: selfBase, Arch: "amd64; id", Dest: selfNew},
		"Dest tương đối":     {Base: selfBase, Arch: "amd64", Dest: "tpan.new"},
		"Dest có ..":         {Base: selfBase, Arch: "amd64", Dest: "/usr/local/bin/../../etc/passwd"},
	}
	for name, s := range cases {
		t.Run(name, func(t *testing.T) {
			f := newSelfFake(t)
			before := f.Snapshot()
			if _, err := s.Check(ctx, f); err == nil {
				t.Error("Check không lỗi")
			}
			if err := s.Apply(ctx, f); err == nil {
				t.Error("Apply không lỗi")
			}
			if len(f.Ran()) != 0 {
				t.Errorf("đã chạy lệnh: %q", ranStrings(f.Ran()))
			}
			if !reflect.DeepEqual(before, f.Snapshot()) {
				t.Error("máy bị đổi")
			}
		})
	}

	for _, base := range []string{
		"https://example.com", "https://example.com/releases/", "http://127.0.0.1:8080",
		"http://localhost/x", "http://[::1]:9000/", "HTTPS://example.com",
	} {
		if _, err := checkSelfBase(base); err != nil {
			t.Errorf("checkSelfBase(%q) = %v, muốn nhận", base, err)
		}
	}
}

func TestParseSums(t *testing.T) {
	sum := sha256Hex(selfData)
	ok := map[string]string{
		"hai dấu cách":        sum + "  tpan-linux-amd64\n",
		"một dấu cách":        sum + " tpan-linux-amd64\n",
		"chế độ nhị phân":     sum + " *tpan-linux-amd64\n",
		"chữ hoa, CRLF":       strings.ToUpper(sum) + "  tpan-linux-amd64\r\n",
		"nhiều dòng":          strings.Repeat("a", 64) + "  tpan-linux-arm64\n" + sum + "  tpan-linux-amd64",
		"trùng nhưng cùng số": sum + "  tpan-linux-amd64\n" + sum + " *tpan-linux-amd64\n",
	}
	for name, text := range ok {
		if got, err := parseSums(text, "tpan-linux-amd64"); err != nil || got != sum {
			t.Errorf("%s: %q, %v", name, got, err)
		}
	}
	bad := map[string]string{
		"tên gần giống":    sum + "  tpan-linux-amd64.sig\n",
		"tên có tiền tố":   sum + "  x/tpan-linux-amd64\n",
		"hex ngắn":         sum[:63] + "  tpan-linux-amd64\n",
		"ba dấu cách":      sum + "   tpan-linux-amd64\n",
		"hai số khác nhau": sum + "  tpan-linux-amd64\n" + strings.Repeat("b", 64) + "  tpan-linux-amd64\n",
		"rỗng":             "",
	}
	for name, text := range bad {
		if got, err := parseSums(text, "tpan-linux-amd64"); err == nil {
			t.Errorf("%s: nhận %q", name, got)
		}
	}
}

func TestSelfDownloadInfo(t *testing.T) {
	info := dlStep(nil).Info()
	if info.ID != "self.download:"+selfNew || len(info.Commands) != 3 || info.Why.Vi == "" || info.Why.En == "" {
		t.Errorf("Info = %+v", info)
	}
	if info.Danger || !info.NoUndo.IsZero() {
		t.Errorf("Danger %v, NoUndo %v", info.Danger, info.NoUndo)
	}
}

// ================================================================= SelfInstall

func selfInstallBase(m *machine) {
	m.SetFile(selfBin, []byte("bản đang chạy"), 0o755, system.Root)
	m.SetFile(selfNew, selfData, 0o755, system.Root)
}

func TestSelfInstallLifecycle(t *testing.T) {
	step := func() engine.Step { return &SelfInstall{Bin: selfBin, New: selfNew, Old: selfOld} }
	cases := []lifecycle{
		{name: "SelfInstall", setup: selfInstallBase, step: step},
		{
			// .old của lần cập nhật trước bị rename đè; Undo phải trả lại nó.
			name: "SelfInstall có .old cũ",
			setup: func(m *machine) {
				selfInstallBase(m)
				m.SetFile(selfOld, []byte("bản trước nữa"), 0o755, system.Root)
			},
			step: step,
		},
	}
	for _, lc := range cases {
		t.Run(lc.name, lc.run)
	}
}

func TestSelfInstall(t *testing.T) {
	ctx := context.Background()
	s := func() *SelfInstall { return &SelfInstall{Bin: selfBin, New: selfNew, Old: selfOld} }

	t.Run("bản mới vào Bin, bản cũ ở Old", func(t *testing.T) {
		m := newMachine(t)
		selfInstallBase(m)
		if err := s().Apply(ctx, m); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		snap := m.Snapshot()
		if snap[selfBin].Data != string(selfData) || snap[selfOld].Data != "bản đang chạy" {
			t.Errorf("Bin %q, Old %q", snap[selfBin].Data, snap[selfOld].Data)
		}
		if _, ok := snap[selfNew]; ok {
			t.Error("New vẫn còn")
		}
		if !s().Info().Danger {
			t.Error("thiếu Danger")
		}
	})

	t.Run("rename thứ hai lỗi thì Bin về chỗ cũ", func(t *testing.T) {
		m := newMachine(t)
		selfInstallBase(m)
		m.SetFile(selfOld, []byte("bản trước nữa"), 0o755, system.Root)
		m.Fail("rename", selfNew, errors.New("đĩa lỗi"))
		before := m.whole()
		if err := s().Apply(ctx, m); err == nil || !strings.Contains(err.Error(), "đĩa lỗi") {
			t.Fatalf("Apply = %v", err)
		}
		if !reflect.DeepEqual(before, m.whole()) {
			t.Error("Apply lỗi mà máy không về như cũ")
		}
	})

	t.Run("New không phải file thường thì từ chối", func(t *testing.T) {
		m := newMachine(t)
		m.SetFile(selfBin, []byte("bản đang chạy"), 0o755, system.Root)
		m.SetSymlink("/tmp/x", selfNew)
		before := m.whole()
		if err := s().Apply(ctx, m); err == nil {
			t.Fatal("Apply nhận symlink làm bản mới")
		}
		if !reflect.DeepEqual(before, m.whole()) {
			t.Error("máy bị đổi")
		}
	})

	t.Run("thiếu New thì lỗi", func(t *testing.T) {
		m := newMachine(t)
		m.SetFile(selfBin, []byte("bản đang chạy"), 0o755, system.Root)
		if err := s().Apply(ctx, m); err == nil || !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("Apply = %v", err)
		}
	})

	t.Run("tham số sai", func(t *testing.T) {
		bad := map[string]SelfInstall{
			"khác thư mục": {Bin: selfBin, New: "/tmp/tpan.new", Old: selfOld},
			"trùng":        {Bin: selfBin, New: selfBin, Old: selfOld},
			"tương đối":    {Bin: "tpan", New: "tpan.new", Old: "tpan.old"},
			"có ..":        {Bin: selfBin, New: "/usr/local/bin/../tpan.new", Old: selfOld},
			"rỗng":         {},
		}
		for name, b := range bad {
			m := newMachine(t)
			selfInstallBase(m)
			before := m.whole()
			if _, err := b.Check(ctx, m); err == nil {
				t.Errorf("%s: Check không lỗi", name)
			}
			if err := b.Apply(ctx, m); err == nil {
				t.Errorf("%s: Apply không lỗi", name)
			}
			if !reflect.DeepEqual(before, m.whole()) {
				t.Errorf("%s: máy bị đổi", name)
			}
		}
	})

	t.Run("Undo khi chưa Apply", func(t *testing.T) {
		if err := s().Undo(ctx, newMachine(t)); !errors.Is(err, errNotApplied) {
			t.Errorf("Undo = %v", err)
		}
	})
}

// ================================================================== SelfLatest

// latestFake: gốc tải trả SHA256SUMS (sums) và VERSION (version; versionCode
// khác 0 là curl lỗi, ví dụ 22 khi máy chủ trả 404).
func latestFake(sums, version string, versionCode int) *system.FakeSystem {
	f := system.NewFake()
	f.Handle([]string{"curl", "-fsSL", "--max-time", "15", "--proto-redir", "=https", selfBase + "/SHA256SUMS"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: sums}
	})
	f.Handle([]string{"curl", "-fsSL", "--max-time", "15", "--proto-redir", "=https", selfBase + "/VERSION"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: version, ExitCode: versionCode}
	})
	return f
}

func TestSelfLatest(t *testing.T) {
	ctx := context.Background()
	sumA, sumB := strings.Repeat("a", 64), strings.Repeat("b", 64)
	sums := sumA + "  tpan-linux-amd64\n" + sumB + "  tpan-linux-arm64\n"

	t.Run("đủ hai file", func(t *testing.T) {
		f := latestFake(sums, "v1.2.3\n", 0)
		sum, version, err := SelfLatest(ctx, f, selfBase+"/", "arm64")
		if err != nil || sum != sumB || version != "v1.2.3" {
			t.Fatalf("= %q, %q, %v", sum, version, err)
		}
		want := []string{
			"curl -fsSL --max-time 15 --proto-redir =https " + selfBase + "/SHA256SUMS",
			"curl -fsSL --max-time 15 --proto-redir =https " + selfBase + "/VERSION",
		}
		if got := ranStrings(f.Ran()); !reflect.DeepEqual(got, want) {
			t.Errorf("lệnh = %q\nmuốn  %q", got, want)
		}
		if len(f.Snapshot()) != 1 {
			t.Errorf("SelfLatest ghi vào máy: %v", f.Snapshot())
		}
	})

	t.Run("thiếu VERSION", func(t *testing.T) {
		f := latestFake(sums, "", 22)
		sum, version, err := SelfLatest(ctx, f, selfBase, "amd64")
		if err != nil || sum != sumA || version != "" {
			t.Errorf("= %q, %q, %v", sum, version, err)
		}
	})

	t.Run("VERSION lạ thì coi như không có", func(t *testing.T) {
		bad := []string{
			"",
			"<html>404</html>",
			"v1.2.3\nv1.2.4\n",
			"v1.2.3; rm -rf /",
			"$(id)",
			"-v1",
			"v1 2",
			"bản-mới",
			"v" + strings.Repeat("1", 64),
		}
		for _, v := range bad {
			sum, version, err := SelfLatest(ctx, latestFake(sums, v, 0), selfBase, "amd64")
			if err != nil || sum != sumA || version != "" {
				t.Errorf("%q: = %q, %q, %v", v, sum, version, err)
			}
		}
		// Khoảng trắng hai đầu được bỏ; chữ dài nhất còn nhận là 64 ký tự.
		for _, v := range []string{"  v0.3.0-1-gabc1234 \r\n", "1.0+build_5", strings.Repeat("9", 64)} {
			_, version, err := SelfLatest(ctx, latestFake(sums, v, 0), selfBase, "amd64")
			if err != nil || version != strings.TrimSpace(v) {
				t.Errorf("%q: = %q, %v", v, version, err)
			}
		}
	})

	t.Run("SHA256SUMS thiếu dòng hoặc không tải được", func(t *testing.T) {
		f := latestFake(sumA+"  tpan-linux-amd64\n", "v1.2.3\n", 0)
		if _, _, err := SelfLatest(ctx, f, selfBase, "arm64"); err == nil || !strings.Contains(err.Error(), "tpan-linux-arm64") {
			t.Errorf("thiếu dòng: %v", err)
		}
		// SHA256SUMS hỏng thì không hỏi tiếp VERSION.
		if n := len(f.Ran()); n != 1 {
			t.Errorf("thiếu dòng mà vẫn chạy %d lệnh: %q", n, ranStrings(f.Ran()))
		}
		f = latestFake(sums, "v1.2.3\n", 0)
		f.Handle([]string{"curl"}, func(system.Cmd) system.Result {
			return system.Result{ExitCode: 22, Stderr: "curl: (22) The requested URL returned error: 404\n"}
		})
		f.Handle([]string{"curl", "-fsSL", "--max-time", "15", "--proto-redir", "=https", selfBase + "/SHA256SUMS"}, func(system.Cmd) system.Result {
			return system.Result{ExitCode: 22, Stderr: "curl: (22) The requested URL returned error: 404\n"}
		})
		if _, _, err := SelfLatest(ctx, f, selfBase, "arm64"); err == nil || !strings.Contains(err.Error(), "SHA256SUMS") || !strings.Contains(err.Error(), "404") {
			t.Errorf("404: %v", err)
		}
	})

	t.Run("base hoặc arch sai thì không chạy lệnh nào", func(t *testing.T) {
		for _, base := range []string{"", "http://example.com/x", "https://user:pw@example.com", "https://example.com/a b", "https://example.com/x?a=$(id)", "ftp://example.com", "https://example.com/x\n--output /etc/passwd"} {
			f := latestFake(sums, "v1.2.3\n", 0)
			if _, _, err := SelfLatest(ctx, f, base, "amd64"); err == nil || len(f.Ran()) != 0 {
				t.Errorf("base %q: lỗi %v, lệnh %q", base, err, ranStrings(f.Ran()))
			}
		}
		for _, arch := range []string{"", "386", "amd64/../x", "arm64 "} {
			f := latestFake(sums, "v1.2.3\n", 0)
			if _, _, err := SelfLatest(ctx, f, selfBase, arch); err == nil || len(f.Ran()) != 0 {
				t.Errorf("arch %q: lỗi %v, lệnh %q", arch, err, ranStrings(f.Ran()))
			}
		}
	})
}
