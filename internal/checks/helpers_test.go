package checks

import (
	"context"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

// Output mẫu trong các test viết theo tài liệu của từng lệnh (man systemctl,
// ss, curl, dpkg-query, nginx, apachectl, php-fpm, mariadb) cho Ubuntu 24.04.
// Chưa lấy từ máy ảo thật: phần này nằm trong mục "cần kiểm trên máy ảo".

// reply đăng ký câu trả lời cho các lệnh bắt đầu bằng prefix.
func reply(f *system.FakeSystem, prefix []string, code int, stdout, stderr string) {
	f.Handle(prefix, func(system.Cmd) system.Result {
		return system.Result{ExitCode: code, Stdout: stdout, Stderr: stderr}
	})
}

// cancelledCtx là ctx đã bị huỷ: FakeSystem.Run trả lỗi (không phải ExitError),
// tức là "lệnh không chạy được". Đây là cách duy nhất để giả lập tình huống đó.
func cancelledCtx() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func bg() context.Context { return context.Background() }

func assertText(t *testing.T, name string, x i18n.Text) {
	t.Helper()
	if strings.TrimSpace(x.Vi) == "" || strings.TrimSpace(x.En) == "" {
		t.Errorf("%s thiếu một ngôn ngữ: %+v", name, x)
	}
}

// assertShape kiểm các điều bất biến của mọi Result.
func assertShape(t *testing.T, r Result, id string, stage Stage, want Status) {
	t.Helper()
	if r.ID != id {
		t.Errorf("ID = %q, muốn %q", r.ID, id)
	}
	if r.Stage != stage {
		t.Errorf("Stage = %q, muốn %q", r.Stage, stage)
	}
	if r.Status != want {
		t.Fatalf("Status = %q, muốn %q; summary: %q", r.Status, want, r.Summary.Vi)
	}
	assertText(t, "Title", r.Title)
	assertText(t, "Summary", r.Summary)
	switch want {
	case Pass:
		if !r.Cause.IsZero() || len(r.ManualFix) != 0 || r.AutoFix != "" {
			t.Errorf("Pass không được có Cause/ManualFix/AutoFix: %+v", r)
		}
	case Warn, Fail:
		assertText(t, "Cause", r.Cause)
		if len(r.ManualFix) == 0 {
			t.Fatalf("%s mà không có ManualFix", want)
		}
		for _, l := range r.ManualFix {
			if strings.TrimSpace(l) == "" {
				t.Errorf("ManualFix có dòng rỗng: %q", r.ManualFix)
			}
		}
	case Unknown:
		if r.Summary.Vi == "" || !strings.Contains(r.Summary.Vi, "Chưa kiểm được") {
			t.Errorf("Unknown phải nói lý do trong Summary: %q", r.Summary.Vi)
		}
	}
}

// assertEvidenceIsRan kiểm Evidence ghi đúng các lệnh đã chạy, đúng thứ tự.
func assertEvidenceIsRan(t *testing.T, r Result, f *system.FakeSystem) {
	t.Helper()
	ran := f.Ran()
	if len(r.Evidence) != len(ran) {
		t.Fatalf("Evidence có %d mục, nhưng đã chạy %d lệnh: %+v / %v", len(r.Evidence), len(ran), r.Evidence, ran)
	}
	for i := range ran {
		if r.Evidence[i].Command != ran[i].String() {
			t.Errorf("Evidence[%d].Command = %q, lệnh thật là %q", i, r.Evidence[i].Command, ran[i].String())
		}
	}
}

func lastFix(r Result) string {
	if len(r.ManualFix) == 0 {
		return ""
	}
	return r.ManualFix[len(r.ManualFix)-1]
}

// assertNoCmd kiểm không có lệnh nào chạy (ví dụ khi đầu vào sai).
func assertNoCmd(t *testing.T, f *system.FakeSystem) {
	t.Helper()
	if ran := f.Ran(); len(ran) != 0 {
		t.Errorf("không được chạy lệnh nào, nhưng đã chạy %v", ran)
	}
}
