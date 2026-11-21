package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/system"
)

var selfT0 = time.Date(2026, 11, 21, 8, 0, 0, 0, time.UTC)

// selfDB là tpan.db mà phép thử "file cũ có mở được DB không" tìm.
const selfDB = "/var/lib/tpan/tpan.db"

func sumOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// selfLatest dạy máy giả trả lời hai lệnh của "Kiểm bản mới": SHA256SUMS ghi
// sum cho arm64, VERSION trả version (rỗng là gốc tải không có file đó: curl
// thoát mã 22).
func selfLatest(f *system.FakeSystem, sum, version string) {
	f.Handle([]string{"curl", "-fsSL", "--max-time", "15"}, func(c system.Cmd) system.Result {
		switch url := c.Args[len(c.Args)-1]; {
		case strings.HasSuffix(url, "/SHA256SUMS"):
			return system.Result{Stdout: strings.Repeat("0", 64) + "  tpan-linux-amd64\n" + sum + "  tpan-linux-arm64\n"}
		case strings.HasSuffix(url, "/VERSION") && version != "":
			return system.Result{Stdout: version + "\n"}
		}
		return system.Result{ExitCode: 22, Stderr: "curl: (22) The requested URL returned error: 404\n"}
	})
}

func ranLines(f *system.FakeSystem) []string {
	var out []string
	for _, c := range f.Ran() {
		out = append(out, c.String())
	}
	return out
}

// ranOf trả các lệnh đã chạy có tên chương trình name.
func ranOf(f *system.FakeSystem, name string) []system.Cmd {
	var out []system.Cmd
	for _, c := range f.Ran() {
		if c.Name == name {
			out = append(out, c)
		}
	}
	return out
}

// lastRun đọc SelfRun đang nằm ở settings; chưa có thì test hỏng.
func lastRun(t *testing.T, s *SelfUpdate) SelfRun {
	t.Helper()
	run, ok, err := s.Last(ctx)
	if err != nil || !ok {
		t.Fatalf("Last = %+v, %v, %v", run, ok, err)
	}
	return run
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// ------------------------------------------------------------------- Check

func TestSelfCheck(t *testing.T) {
	installed := sumOf("tpan v1.0.0")
	other := sumOf("tpan v1.1.0")
	for _, tc := range []struct {
		name          string
		sum, version  string
		upToDate      bool
		wantLatest    string
		removeTpanBin bool
	}{
		{name: "trùng sha256, gốc tải không có VERSION", sum: installed, upToDate: true},
		{name: "trùng sha256, có VERSION", sum: installed, version: "v1.0.0", upToDate: true, wantLatest: "v1.0.0"},
		{name: "khác sha256 nhưng trùng tên bản", sum: other, version: "v1.0.0", upToDate: true, wantLatest: "v1.0.0"},
		{name: "có bản mới", sum: other, version: "v1.1.0", wantLatest: "v1.1.0"},
		{name: "có bản khác, không biết tên", sum: other},
		{name: "máy không có file chạy ở TpanBin", sum: installed, version: "v1.1.0", wantLatest: "v1.1.0", removeTpanBin: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, f := newSelfEnv(t, "v1.1.0")
			selfLatest(f, tc.sum, tc.version)
			if tc.removeTpanBin {
				if err := f.Remove(TpanBin); err != nil {
					t.Fatal(err)
				}
			}
			before := f.Snapshot()
			got, err := s.Check(ctx)
			if err != nil {
				t.Fatal(err)
			}
			want := SelfCheck{Current: "v1.0.0", Latest: tc.wantLatest, URL: DefaultSelfURL, Asset: "tpan-linux-arm64", SHA256: tc.sum, UpToDate: tc.upToDate}
			if got != want {
				t.Errorf("Check = %+v\nmuốn    %+v", got, want)
			}
			wantRan := []string{
				"curl -fsSL --max-time 15 --proto-redir =https " + DefaultSelfURL + "/SHA256SUMS",
				"curl -fsSL --max-time 15 --proto-redir =https " + DefaultSelfURL + "/VERSION",
			}
			if !slices.Equal(ranLines(f), wantRan) {
				t.Errorf("lệnh = %q\nmuốn  %q", ranLines(f), wantRan)
			}
			if !reflect.DeepEqual(before, f.Snapshot()) {
				t.Error("Check làm đổi máy")
			}
			if len(s.Settings.(mapSettings)) != 0 {
				t.Errorf("Check ghi settings: %v", s.Settings)
			}
		})
	}

	t.Run("gốc tải lấy từ settings", func(t *testing.T) {
		s, f := newSelfEnv(t, "v1.1.0")
		s.Settings = mapSettings{SettingSelfURL: selfTestBase + "/"}
		selfLatest(f, other, "v1.1.0")
		got, err := s.Check(ctx)
		if err != nil || got.URL != selfTestBase+"/" || got.UpToDate {
			t.Errorf("Check = %+v, %v", got, err)
		}
		if r := ranLines(f); len(r) != 2 || !strings.HasSuffix(r[0], " "+selfTestBase+"/SHA256SUMS") {
			t.Errorf("lệnh = %q", r)
		}
	})

	t.Run("không hỏi được gốc tải là lỗi thường, không phải lời từ chối", func(t *testing.T) {
		s, f := newSelfEnv(t, "v1.1.0")
		f.Handle([]string{"curl"}, func(system.Cmd) system.Result {
			return system.Result{ExitCode: 6, Stderr: "curl: (6) Could not resolve host: github.com\n"}
		})
		got, err := s.Check(ctx)
		var re *RefusedError
		if err == nil || errors.As(err, &re) || !strings.Contains(err.Error(), "Could not resolve host") {
			t.Fatalf("lỗi = %v", err)
		}
		if got.Current != "v1.0.0" || got.URL != DefaultSelfURL || got.UpToDate || got.SHA256 != "" {
			t.Errorf("Check khi lỗi = %+v", got)
		}
	})

	t.Run("gốc tải sai thì không chạy lệnh nào", func(t *testing.T) {
		s, f := newSelfEnv(t, "v1.1.0")
		s.Settings = mapSettings{SettingSelfURL: "http://example.com/x?a=$(id)"}
		if _, err := s.Check(ctx); err == nil || len(f.Ran()) != 0 {
			t.Errorf("lỗi %v, lệnh %q", err, ranLines(f))
		}
	})
}

// -------------------------------------------------------------------- Busy

func TestSelfBusy(t *testing.T) {
	// Output và mã thoát thật của `systemctl is-active` lấy từ
	// docs/bang-chung/M15-thu-co-che.log (phần 2 và 3).
	for _, tc := range []struct {
		out  string
		code int
		want bool
	}{
		{"active\n", 0, true},
		{"activating\n", 3, true},
		{"deactivating\n", 3, true},
		{"inactive\n", 4, false},
		{"failed\n", 3, false},
		{"unknown\n", 4, false},
		{"", 0, false},
		{"", 1, false},
	} {
		s, f := newSelfEnv(t, "v1.1.0")
		f.Handle([]string{"systemctl", "is-active"}, func(system.Cmd) system.Result {
			return system.Result{Stdout: tc.out, ExitCode: tc.code}
		})
		got, err := s.Busy(ctx)
		if err != nil || got != tc.want {
			t.Errorf("%q mã %d: Busy = %v, %v; muốn %v", tc.out, tc.code, got, err, tc.want)
		}
		if r := ranLines(f); !slices.Equal(r, []string{"systemctl is-active tpan-self-update.service"}) {
			t.Errorf("lệnh = %q", r)
		}
	}

	// Không chạy nổi systemctl (ở đây: ctx đã huỷ) là lỗi, không phải "rảnh".
	s, _ := newSelfEnv(t, "v1.1.0")
	dead, cancel := context.WithCancel(ctx)
	cancel()
	if busy, err := s.Busy(dead); err == nil || busy {
		t.Errorf("ctx đã huỷ: Busy = %v, %v", busy, err)
	}
}

// -------------------------------------------------------------------- Last

func TestSelfLast(t *testing.T) {
	s, _ := newSelfEnv(t, "v1.1.0")
	if run, ok, err := s.Last(ctx); err != nil || ok || run != (SelfRun{}) {
		t.Errorf("chưa có: %+v, %v, %v", run, ok, err)
	}
	for _, bad := range []string{"", "{", "[]", "null", "{}", `{"action":"update"}`} {
		s.Settings = mapSettings{SettingSelfLast: bad}
		if run, ok, err := s.Last(ctx); err != nil || ok {
			t.Errorf("%q: %+v, %v, %v", bad, run, ok, err)
		}
	}
	want := SelfRun{Action: SelfActionUpdate, Status: SelfOK, From: "v1.0.0", To: "v1.1.0", JobID: "abc", Actor: "admin", IP: "203.0.113.7", Started: selfT0, Finished: selfT0.Add(time.Second)}
	s.Settings = mapSettings{SettingSelfLast: mustJSON(t, want)}
	if run, ok, err := s.Last(ctx); err != nil || !ok || run != want {
		t.Errorf("Last = %+v, %v, %v", run, ok, err)
	}
	s.Settings = failSettings{}
	if _, ok, err := s.Last(ctx); err == nil || ok {
		t.Errorf("DB lỗi: ok %v, lỗi %v", ok, err)
	}
}

// ------------------------------------------------- PrepareRollback, preview

const oldDBError = "tpan: DB ở bản 1400, tpan này chỉ biết tới bản 900: hãy dùng tpan mới hơn"

// oldEnv: máy đã self-update một lần (có tpan.old bản v0.9.0). siteList là mã
// thoát của `tpan.old site list`.
func oldEnv(t *testing.T, siteListCode int, siteListErr string) (*SelfUpdate, *system.FakeSystem) {
	t.Helper()
	s, f := newSelfEnv(t, "v1.1.0")
	f.SetFile(tpanOld, []byte("tpan v0.9.0"), 0o755, system.Root)
	f.Handle([]string{tpanOld, "version"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "tpan v0.9.0 (commit abc, build x)\n"}
	})
	f.Handle([]string{tpanOld, "site", "list"}, func(system.Cmd) system.Result {
		return system.Result{ExitCode: siteListCode, Stderr: siteListErr}
	})
	return s, f
}

func TestSelfRollbackDBCheck(t *testing.T) {
	t.Run("file cũ không mở được tpan.db thì từ chối", func(t *testing.T) {
		s, f := oldEnv(t, 1, oldDBError+"\n")
		f.SetFile(selfDB, []byte("sqlite"), 0o600, system.Root)
		before := f.Snapshot()
		_, err := s.PrepareRollback(ctx)
		if code := refusedCode(t, err); code != CodeSelfRollbackDB {
			t.Fatalf("mã %q, lỗi %v", code, err)
		}
		var re *RefusedError
		errors.As(err, &re)
		for _, msg := range []string{re.Message.Vi, re.Message.En} {
			if !strings.Contains(msg, oldDBError) {
				t.Errorf("lời từ chối thiếu câu của file cũ: %q", msg)
			}
			if !strings.Contains(msg, "tpan.db") {
				t.Errorf("lời từ chối không nói đường lùi (bản sao tpan.db): %q", msg)
			}
		}
		if r := ranLines(f); !slices.Equal(r, []string{tpanOld + " site list"}) {
			t.Errorf("lệnh = %q", r)
		}
		if !reflect.DeepEqual(before, f.Snapshot()) {
			t.Error("bị từ chối mà máy đã đổi")
		}
		// Bản xem trước của nút "Quay về" từ chối y như vậy.
		if _, err := s.RollbackPreview(ctx); refusedCode(t, err) != CodeSelfRollbackDB {
			t.Errorf("RollbackPreview: %v", err)
		}
	})

	t.Run("output dài, nhiều dòng được cắt gọn thành một dòng", func(t *testing.T) {
		s, f := oldEnv(t, 2, "dòng một\ndòng hai\n"+strings.Repeat("x", 2000))
		f.SetFile(selfDB, []byte("sqlite"), 0o600, system.Root)
		_, err := s.PrepareRollback(ctx)
		var re *RefusedError
		if !errors.As(err, &re) || re.Code != CodeSelfRollbackDB {
			t.Fatalf("lỗi = %v", err)
		}
		if strings.Contains(re.Message.Vi, "\n") || !strings.Contains(re.Message.Vi, "dòng một dòng hai") {
			t.Errorf("chưa gọn thành một dòng: %q", re.Message.Vi)
		}
		if n := strings.Count(re.Message.Vi, "x"); n > 300 {
			t.Errorf("output chưa được cắt: còn %d chữ x", n)
		}
	})

	t.Run("file cũ không in gì thì vẫn có lý do", func(t *testing.T) {
		s, f := oldEnv(t, 126, "")
		f.SetFile(selfDB, []byte("sqlite"), 0o600, system.Root)
		_, err := s.PrepareRollback(ctx)
		var re *RefusedError
		if !errors.As(err, &re) || re.Code != CodeSelfRollbackDB || !strings.Contains(re.Message.Vi, "126") {
			t.Errorf("lỗi = %v", err)
		}
	})

	t.Run("file cũ mở được tpan.db thì dựng Plan", func(t *testing.T) {
		s, f := oldEnv(t, 0, "")
		f.SetFile(selfDB, []byte("sqlite"), 0o600, system.Root)
		p, err := s.PrepareRollback(ctx)
		if err != nil || p.Plan.Kind != "self.rollback" {
			t.Fatalf("PrepareRollback = %v, %v", p, err)
		}
		if r := ranLines(f); !slices.Equal(r, []string{tpanOld + " site list"}) {
			t.Errorf("lệnh = %q", r)
		}
		pv, err := s.RollbackPreview(ctx)
		if err != nil || pv != (SelfRollbackPreview{Current: "v1.0.0", Old: "v0.9.0"}) {
			t.Errorf("RollbackPreview = %+v, %v", pv, err)
		}
	})

	// Máy chưa có tpan.db: `site list` của file cũ sẽ tạo một DB rỗng, tức là
	// bản xem trước làm đổi máy. Không được chạy phép thử.
	t.Run("chưa có tpan.db thì không chạy file cũ", func(t *testing.T) {
		s, f := oldEnv(t, 1, oldDBError)
		if _, err := s.PrepareRollback(ctx); err != nil {
			t.Fatal(err)
		}
		if len(f.Ran()) != 0 {
			t.Errorf("chạy %q khi chưa có tpan.db", ranLines(f))
		}
	})

	t.Run("không có file cũ: self_no_old, không chạy gì", func(t *testing.T) {
		s, f := newSelfEnv(t, "v1.1.0")
		f.SetFile(selfDB, []byte("sqlite"), 0o600, system.Root)
		if _, err := s.RollbackPreview(ctx); refusedCode(t, err) != "self_no_old" || len(f.Ran()) != 0 {
			t.Errorf("lỗi %v, lệnh %q", err, ranLines(f))
		}
	})

	t.Run("ctx đã huỷ không bị báo thành lời từ chối", func(t *testing.T) {
		s, f := oldEnv(t, 0, "")
		f.SetFile(selfDB, []byte("sqlite"), 0o600, system.Root)
		dead, cancel := context.WithCancel(ctx)
		cancel()
		_, err := s.PrepareRollback(dead)
		var re *RefusedError
		if err == nil || errors.As(err, &re) {
			t.Errorf("lỗi = %v", err)
		}
	})
}

// ------------------------------------------------------------------- Start

// Chữ tấn công ở chỗ tên người bấm và IP: không được có chữ nào tới argv.
const (
	evilActor = `admin"; rm -rf / #$(id)`
	evilIP    = "203.0.113.7 --property=ExecStartPre=/bin/sh"
)

func TestSelfStart(t *testing.T) {
	wantArgs := map[string][]string{
		SelfActionUpdate:   {"--unit", "tpan-self-update", "--collect", "--description", "tpan: cap nhat tu panel", "/usr/local/bin/tpan", "internal", "self-update"},
		SelfActionRollback: {"--unit", "tpan-self-update", "--collect", "--description", "tpan: quay ve ban truoc tu panel", "/usr/local/bin/tpan", "internal", "self-update", "--rollback"},
	}
	for _, action := range []string{SelfActionUpdate, SelfActionRollback} {
		t.Run(action, func(t *testing.T) {
			s, f := oldEnv(t, 0, "")
			f.Handle([]string{"systemctl", "is-active"}, func(system.Cmd) system.Result {
				return system.Result{Stdout: "inactive\n", ExitCode: 4}
			})
			f.Handle([]string{"systemd-run"}, func(system.Cmd) system.Result {
				// Lúc systemd khởi đơn vị, dấu "đang chạy" đã phải nằm ở settings:
				// lệnh nội bộ đọc người bấm từ đó.
				if run := lastRun(t, s); run.Status != SelfRunning || run.Actor != evilActor {
					t.Errorf("settings lúc systemd-run chạy = %+v", run)
				}
				return system.Result{Stdout: "Running as unit: tpan-self-update.service\n"}
			})
			before := f.Snapshot()
			run, err := s.Start(ctx, action, evilActor, evilIP, selfT0)
			if err != nil {
				t.Fatal(err)
			}
			want := SelfRun{Action: action, Status: SelfRunning, From: "v1.0.0", Actor: evilActor, IP: evilIP, Started: selfT0}
			if run != want {
				t.Errorf("Start = %+v\nmuốn    %+v", run, want)
			}
			if got := lastRun(t, s); got != want {
				t.Errorf("settings = %+v\nmuốn      %+v", got, want)
			}
			cmds := ranOf(f, "systemd-run")
			if len(cmds) != 1 || !slices.Equal(cmds[0].Args, wantArgs[action]) {
				t.Fatalf("systemd-run = %q\nmuốn         %q", cmds, wantArgs[action])
			}
			if cmds[0].Stdin != nil || cmds[0].AsUser != "" {
				t.Errorf("systemd-run có stdin hoặc AsUser: %+v", cmds[0])
			}
			// Không lệnh nào (kể cả phép thử của rollback) mang chữ từ request.
			for _, c := range f.Ran() {
				for _, a := range append([]string{c.Name}, c.Args...) {
					if strings.Contains(a, "admin") || strings.Contains(a, "203.0.113") || strings.Contains(a, "rm -rf") {
						t.Errorf("argv có chữ từ request: %q", c.String())
					}
				}
			}
			if !reflect.DeepEqual(before, f.Snapshot()) {
				t.Error("Start làm đổi file trên máy")
			}
		})
	}

	t.Run("loại lượt chạy lạ", func(t *testing.T) {
		for _, action := range []string{"", "Update", "update --rollback", "rollback;id"} {
			s, f := oldEnv(t, 0, "")
			if _, err := s.Start(ctx, action, "admin", "203.0.113.7", selfT0); err == nil {
				t.Errorf("%q: không lỗi", action)
			}
			if len(f.Ran()) != 0 || len(s.Settings.(mapSettings)) != 0 {
				t.Errorf("%q: chạy %q, settings %v", action, ranLines(f), s.Settings)
			}
		}
	})

	t.Run("đơn vị tạm đang chạy: self_update_running", func(t *testing.T) {
		s, f := oldEnv(t, 0, "")
		f.Handle([]string{"systemctl", "is-active"}, func(system.Cmd) system.Result { return system.Result{Stdout: "active\n"} })
		_, err := s.Start(ctx, SelfActionUpdate, "admin", "203.0.113.7", selfT0)
		if code := refusedCode(t, err); code != CodeSelfBusy {
			t.Errorf("mã %q", code)
		}
		if len(ranOf(f, "systemd-run")) != 0 || len(s.Settings.(mapSettings)) != 0 {
			t.Errorf("đang chạy mà vẫn khởi: %q, settings %v", ranLines(f), s.Settings)
		}
	})

	t.Run("lời từ chối của Plan về thẳng người bấm, chưa ghi gì", func(t *testing.T) {
		// rollback: không có file cũ.
		s, f := newSelfEnv(t, "v1.1.0")
		_, err := s.Start(ctx, SelfActionRollback, "admin", "203.0.113.7", selfT0)
		if code := refusedCode(t, err); code != "self_no_old" {
			t.Errorf("mã %q", code)
		}
		if len(ranOf(f, "systemd-run")) != 0 || len(s.Settings.(mapSettings)) != 0 {
			t.Errorf("chạy %q, settings %v", ranLines(f), s.Settings)
		}
		// rollback: file cũ không mở được DB.
		s, f = oldEnv(t, 1, oldDBError)
		f.SetFile(selfDB, []byte("sqlite"), 0o600, system.Root)
		_, err = s.Start(ctx, SelfActionRollback, "admin", "203.0.113.7", selfT0)
		if code := refusedCode(t, err); code != CodeSelfRollbackDB {
			t.Errorf("mã %q", code)
		}
		if len(ranOf(f, "systemd-run")) != 0 || len(s.Settings.(mapSettings)) != 0 {
			t.Errorf("chạy %q, settings %v", ranLines(f), s.Settings)
		}
		// update: gốc tải trong settings sai. Không tải gì.
		s, f = newSelfEnv(t, "v1.1.0")
		bad := mapSettings{SettingSelfURL: "http://example.com/x"}
		s.Settings = bad
		_, err = s.Start(ctx, SelfActionUpdate, "admin", "203.0.113.7", selfT0)
		if code := refusedCode(t, err); code != "self_url_invalid" {
			t.Errorf("mã %q", code)
		}
		if len(ranOf(f, "systemd-run"))+len(ranOf(f, "curl")) != 0 || len(bad) != 1 {
			t.Errorf("chạy %q, settings %v", ranLines(f), bad)
		}
	})

	alreadyLoaded := system.Result{ExitCode: 1, Stderr: "Failed to start transient service unit: Unit tpan-self-update.service was already loaded or has a fragment file.\n"}

	t.Run("hai người bấm cùng lúc: systemd-run báo already loaded", func(t *testing.T) {
		s, f := newSelfEnv(t, "v1.1.0")
		// Người thứ nhất vừa ghi dấu "đang chạy" của họ.
		first := mustJSON(t, SelfRun{Action: SelfActionUpdate, Status: SelfRunning, From: "v1.0.0", Actor: "an", IP: "198.51.100.1", Started: selfT0})
		m := mapSettings{SettingSelfLast: first}
		s.Settings = m
		f.Handle([]string{"systemd-run"}, func(system.Cmd) system.Result { return alreadyLoaded })
		_, err := s.Start(ctx, SelfActionUpdate, "binh", "198.51.100.2", selfT0.Add(time.Second))
		if code := refusedCode(t, err); code != CodeSelfBusy {
			t.Errorf("mã %q", code)
		}
		if m[SettingSelfLast] != first {
			t.Errorf("settings không trả về như trước:\n%s\nmuốn\n%s", m[SettingSelfLast], first)
		}
	})

	t.Run("systemd-run lỗi khác: self_start_failed kèm stderr", func(t *testing.T) {
		s, f := newSelfEnv(t, "v1.1.0")
		prev := mustJSON(t, SelfRun{Action: SelfActionUpdate, Status: SelfOK, From: "v0.9.0", To: "v1.0.0", JobID: "abc", IP: "198.51.100.1", Started: selfT0, Finished: selfT0})
		m := mapSettings{SettingSelfLast: prev}
		s.Settings = m
		f.Handle([]string{"systemd-run"}, func(system.Cmd) system.Result {
			return system.Result{ExitCode: 1, Stderr: "Failed to start transient service unit: Cannot allocate memory\n"}
		})
		_, err := s.Start(ctx, SelfActionUpdate, "admin", "203.0.113.7", selfT0.Add(time.Hour))
		var re *RefusedError
		if !errors.As(err, &re) || re.Code != CodeSelfStart || !strings.Contains(re.Message.Vi, "Cannot allocate memory") || !strings.Contains(re.Message.En, "Cannot allocate memory") {
			t.Fatalf("lỗi = %v", err)
		}
		if m[SettingSelfLast] != prev {
			t.Errorf("settings không trả về như trước: %s", m[SettingSelfLast])
		}
	})

	t.Run("systemd-run lỗi khi trước đó chưa có lượt nào: ghi failed kèm lý do", func(t *testing.T) {
		s, f := newSelfEnv(t, "v1.1.0")
		f.Handle([]string{"systemd-run"}, func(system.Cmd) system.Result {
			return system.Result{ExitCode: 127, Stderr: "systemd-run: command not found\n"}
		})
		_, err := s.Start(ctx, SelfActionUpdate, "admin", "203.0.113.7", selfT0)
		if code := refusedCode(t, err); code != CodeSelfStart {
			t.Errorf("mã %q", code)
		}
		run := lastRun(t, s)
		if run.Status != SelfFailed || !strings.Contains(run.Error, "command not found") || run.Actor != "admin" || run.Finished.IsZero() {
			t.Errorf("settings = %+v", run)
		}
	})

	// Lượt đang chạy (của người kia) ghi xong kết quả đúng lúc lượt này bị
	// systemd từ chối: không được đè kết quả đó bằng giá trị cũ.
	t.Run("không đè kết quả vừa được ghi", func(t *testing.T) {
		s, f := newSelfEnv(t, "v1.1.0")
		m := mapSettings{SettingSelfLast: mustJSON(t, SelfRun{Action: SelfActionUpdate, Status: SelfRunning, From: "v1.0.0", Actor: "an", IP: "198.51.100.1", Started: selfT0})}
		s.Settings = m
		done := mustJSON(t, SelfRun{Action: SelfActionUpdate, Status: SelfOK, From: "v1.0.0", To: "v1.1.0", JobID: "abc", Actor: "an", IP: "198.51.100.1", Started: selfT0, Finished: selfT0.Add(time.Second)})
		f.Handle([]string{"systemd-run"}, func(system.Cmd) system.Result {
			m[SettingSelfLast] = done
			return alreadyLoaded
		})
		if _, err := s.Start(ctx, SelfActionUpdate, "binh", "198.51.100.2", selfT0); refusedCode(t, err) != CodeSelfBusy {
			t.Errorf("lỗi = %v", err)
		}
		if m[SettingSelfLast] != done {
			t.Errorf("kết quả của lượt kia bị đè: %s", m[SettingSelfLast])
		}
	})
}

// --------------------------------------------------------------------- Run

// clock trả selfT0 cộng thêm một giây mỗi lần gọi.
func clock() func() time.Time {
	n := 0
	return func() time.Time { n++; return selfT0.Add(time.Duration(n) * time.Second) }
}

// pressed ghi dấu "đang chạy" như Start đã ghi khi admin bấm nút.
func pressed(t *testing.T, s *SelfUpdate, action string) SelfRun {
	t.Helper()
	run := SelfRun{Action: action, Status: SelfRunning, From: "v1.0.0", Actor: "admin", IP: "203.0.113.7", Started: selfT0}
	if err := s.Settings.SetSetting(ctx, SettingSelfLast, mustJSON(t, run)); err != nil {
		t.Fatal(err)
	}
	return run
}

func TestSelfRun(t *testing.T) {
	active := func(f *system.FakeSystem) {
		f.Handle([]string{"systemctl", "is-active", "tpan"}, func(system.Cmd) system.Result { return system.Result{Stdout: "active\n"} })
	}

	t.Run("cập nhật thành công: ok kèm bản mới, người bấm lấy từ settings", func(t *testing.T) {
		s, f := newSelfEnv(t, "v1.1.0")
		active(f)
		s.Settings = mapSettings{SettingSelfURL: selfTestBase}
		pressed(t, s, SelfActionUpdate)
		var events int
		run, rep, err := s.Run(ctx, SelfActionUpdate, clock(), func(engine.Event) { events++ })
		if err != nil {
			t.Fatal(err)
		}
		if rep.Job.Status != engine.JobSucceeded || rep.Job.ID == "" || rep.Job.Actor != "admin" {
			t.Fatalf("job = %+v", rep.Job)
		}
		want := SelfRun{Action: SelfActionUpdate, Status: SelfOK, From: "v1.0.0", To: "v1.1.0", JobID: rep.Job.ID, Actor: "admin", IP: "203.0.113.7", Started: selfT0, Finished: run.Finished}
		if run != want || !run.Finished.After(selfT0) {
			t.Errorf("Run = %+v\nmuốn  %+v", run, want)
		}
		if got := lastRun(t, s); got != run {
			t.Errorf("settings = %+v\nmuốn      %+v", got, run)
		}
		if events == 0 {
			t.Error("onEvent không được gọi")
		}
		if fileData(t, f, TpanBin) != "tpan v1.1.0" || fileData(t, f, tpanOld) != "tpan v1.0.0" || !restarted(f) {
			t.Errorf("TpanBin %q, .old %q, restart %v", fileData(t, f, TpanBin), fileData(t, f, tpanOld), restarted(f))
		}
	})

	t.Run("bản tải về trùng bản đang chạy: same", func(t *testing.T) {
		s, f := newSelfEnv(t, "v1.0.0")
		active(f)
		pressed(t, s, SelfActionUpdate)
		run, rep, err := s.Run(ctx, SelfActionUpdate, clock(), nil)
		if err != nil || rep.Job.Status != engine.JobSucceeded {
			t.Fatalf("%v, job %+v", err, rep.Job)
		}
		if run.Status != SelfSame || run.To != "v1.0.0" || run.Error != "" || run.JobID != rep.Job.ID {
			t.Errorf("Run = %+v", run)
		}
		if got := lastRun(t, s); got != run {
			t.Errorf("settings = %+v", got)
		}
		if _, err := f.Stat(tpanOld); err == nil || restarted(f) {
			t.Error("trùng bản mà vẫn thay file hoặc khởi động lại")
		}
	})

	t.Run("sai sha256: failed kèm lý do, file chạy cũ còn nguyên", func(t *testing.T) {
		s, f := newSelfEnv(t, "v1.1.0")
		active(f)
		pressed(t, s, SelfActionUpdate)
		f.Handle([]string{"curl", "-fsSL", "--max-time", "300"}, func(c system.Cmd) system.Result {
			f.SetFile(c.Args[slices.Index(c.Args, "-o")+1], []byte("file đã bị sửa giữa đường"), 0o644, system.Root)
			return system.Result{}
		})
		run, rep, err := s.Run(ctx, SelfActionUpdate, clock(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if rep.Job.Status != engine.JobRolledBack {
			t.Errorf("job = %+v", rep.Job)
		}
		if run.Status != SelfFailed || !strings.Contains(run.Error, "sha256") || run.Error != rep.Job.Error || run.JobID != rep.Job.ID || run.To != "v1.0.0" || run.Actor != "admin" || run.Finished.IsZero() {
			t.Errorf("Run = %+v", run)
		}
		if got := lastRun(t, s); got != run {
			t.Errorf("settings = %+v", got)
		}
		if fileData(t, f, TpanBin) != "tpan v1.0.0" || restarted(f) {
			t.Errorf("TpanBin = %q, restart %v", fileData(t, f, TpanBin), restarted(f))
		}
		for _, p := range []string{tpanNew, tpanOld} {
			if _, err := f.Stat(p); err == nil {
				t.Errorf("còn sót %s", p)
			}
		}
	})

	t.Run("quay về bản trước", func(t *testing.T) {
		s, f := oldEnv(t, 0, "")
		active(f)
		f.SetFile(selfDB, []byte("sqlite"), 0o600, system.Root)
		pressed(t, s, SelfActionRollback)
		run, rep, err := s.Run(ctx, SelfActionRollback, clock(), nil)
		if err != nil || rep.Job.Status != engine.JobSucceeded || rep.Job.Kind != "self.rollback" {
			t.Fatalf("%v, job %+v", err, rep.Job)
		}
		if run.Status != SelfOK || run.From != "v1.0.0" || run.To != "v0.9.0" || run.Actor != "admin" || run.IP != "203.0.113.7" {
			t.Errorf("Run = %+v", run)
		}
		if got := lastRun(t, s); got != run {
			t.Errorf("settings = %+v", got)
		}
		if fileData(t, f, TpanBin) != "tpan v0.9.0" || !restarted(f) {
			t.Errorf("TpanBin = %q, restart %v", fileData(t, f, TpanBin), restarted(f))
		}
	})

	// Quay về mà bản cũ trùng tên với bản đang chạy vẫn là ok: "same" chỉ có
	// nghĩa với update (không thay file).
	t.Run("quay về bản cùng tên vẫn là ok", func(t *testing.T) {
		s, f := newSelfEnv(t, "v1.1.0")
		active(f)
		f.SetFile(tpanOld, []byte("tpan v1.0.0"), 0o755, system.Root)
		run, _, err := s.Run(ctx, SelfActionRollback, clock(), nil)
		if err != nil || run.Status != SelfOK || run.To != "v1.0.0" {
			t.Errorf("Run = %+v, %v", run, err)
		}
	})

	t.Run("bị từ chối trước khi thành job: failed, lời tiếng Việt, trả lời từ chối", func(t *testing.T) {
		s, f := newSelfEnv(t, "v1.1.0")
		pressed(t, s, SelfActionRollback)
		run, rep, err := s.Run(ctx, SelfActionRollback, clock(), nil)
		var re *RefusedError
		if !errors.As(err, &re) || re.Code != "self_no_old" {
			t.Fatalf("lỗi = %v", err)
		}
		if rep.Job.ID != "" || run.Status != SelfFailed || run.Error != re.Message.Vi || run.JobID != "" || run.To != "" || run.Actor != "admin" || run.Finished.IsZero() {
			t.Errorf("Run = %+v, job %+v", run, rep.Job)
		}
		if got := lastRun(t, s); got != run {
			t.Errorf("settings = %+v", got)
		}
		if len(f.Ran()) != 0 {
			t.Errorf("bị từ chối mà chạy %q", ranLines(f))
		}
	})

	t.Run("gõ tay, không qua panel: IP cli, không có người bấm", func(t *testing.T) {
		for name, prev := range map[string]string{
			"chưa có lượt nào":           "",
			"lượt trước đã xong":         mustJSON(t, SelfRun{Action: SelfActionUpdate, Status: SelfOK, From: "v0.9.0", To: "v1.0.0", Actor: "admin", IP: "203.0.113.7", Started: selfT0.Add(-time.Hour)}),
			"dấu đang chạy của loại kia": mustJSON(t, SelfRun{Action: SelfActionRollback, Status: SelfRunning, From: "v1.0.0", Actor: "admin", IP: "203.0.113.7", Started: selfT0.Add(-time.Hour)}),
			"settings hỏng":              "{",
		} {
			s, f := newSelfEnv(t, "v1.1.0")
			active(f)
			if prev != "" {
				s.Settings = mapSettings{SettingSelfLast: prev}
			}
			run, rep, err := s.Run(ctx, SelfActionUpdate, clock(), nil)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if run.Status != SelfOK || run.Actor != "" || run.IP != "cli" || run.From != "v1.0.0" || run.To != "v1.1.0" || !run.Started.Equal(selfT0.Add(time.Second)) || rep.Job.Actor != "" {
				t.Errorf("%s: Run = %+v", name, run)
			}
			if got := lastRun(t, s); got != run {
				t.Errorf("%s: settings = %+v", name, got)
			}
		}
	})

	t.Run("loại lượt chạy lạ", func(t *testing.T) {
		s, f := newSelfEnv(t, "v1.1.0")
		if _, _, err := s.Run(ctx, "upgrade", clock(), nil); err == nil || len(f.Ran()) != 0 || len(s.Settings.(mapSettings)) != 0 {
			t.Errorf("lỗi %v, lệnh %q, settings %v", err, ranLines(f), s.Settings)
		}
	})

	t.Run("không đọc được settings thì dừng, chưa đổi gì", func(t *testing.T) {
		s, f := newSelfEnv(t, "v1.1.0")
		s.Settings = failSettings{}
		run, _, err := s.Run(ctx, SelfActionUpdate, clock(), nil)
		if err == nil || run != (SelfRun{}) || len(f.Ran()) != 0 {
			t.Errorf("Run = %+v, %v, lệnh %q", run, err, ranLines(f))
		}
	})

	// Đơn vị tạm bị dừng giữa chừng (systemctl stop, máy tắt): ctx bị huỷ. Kết
	// quả vẫn phải được ghi, nếu không màn sẽ treo ở "đang chạy".
	t.Run("bị ngắt giữa chừng vẫn ghi được kết quả", func(t *testing.T) {
		s, f := newSelfEnv(t, "v1.1.0")
		active(f)
		m := ctxSettings{mapSettings{}}
		s.Settings = m
		pressed(t, s, SelfActionUpdate)
		cctx, cancel := context.WithCancel(ctx)
		defer cancel()
		run, rep, err := s.Run(cctx, SelfActionUpdate, clock(), func(ev engine.Event) {
			if ev.Kind == engine.EventStepStart {
				cancel()
			}
		})
		if err != nil {
			t.Fatal(err)
		}
		if run.Status != SelfFailed || run.Error == "" || run.To != "v1.0.0" || run.Actor != "admin" {
			t.Errorf("Run = %+v, job %+v", run, rep.Job)
		}
		if got := lastRun(t, s); got != run {
			t.Errorf("settings = %+v\nmuốn      %+v", got, run)
		}
		if fileData(t, f, TpanBin) != "tpan v1.0.0" {
			t.Errorf("TpanBin = %q", fileData(t, f, TpanBin))
		}
	})
}

// ctxSettings là mapSettings từ chối làm việc với ctx đã huỷ, như DB thật.
type ctxSettings struct{ m mapSettings }

func (c ctxSettings) GetSetting(ctx context.Context, k string) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	return c.m.GetSetting(ctx, k)
}

func (c ctxSettings) SetSetting(ctx context.Context, k, v string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.m.SetSetting(ctx, k, v)
}
