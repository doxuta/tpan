package core

// Bước khởi động lại lồng ở cuối php.install và security.harden (M17): lỗi
// khởi động lại không làm job của admin hỏng (mục 3.6, Plan chính sách).

import (
	"context"
	"slices"
	"testing"

	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
)

func TestCageRestartStepLoiKhongLamHongJob(t *testing.T) {
	ctx := context.Background()
	unit := "tpan-cage-a_example_com_7653e4-php.service"
	f := system.NewFake()
	f.Handle([]string{"systemctl", "is-active"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "active\n"}
	})
	// Mã của site lấp đầy số tiến trình của slice: dịch vụ không lên lại.
	f.Handle([]string{"systemctl", "restart"}, func(system.Cmd) system.Result {
		return system.Result{ExitCode: 1, Stderr: "Job for " + unit + " failed.\n"}
	})
	s := &cageRestartStep{ServiceRestart: &steps.ServiceRestart{Unit: unit, OnlyIfActive: true}}
	if done, err := s.Check(ctx, f); err != nil || done {
		t.Fatalf("dịch vụ đang chạy: Check phải false, có %v, %v", done, err)
	}
	if err := s.Apply(ctx, f); err != nil {
		t.Fatalf("khởi động lại hỏng không được làm bước hỏng: %v", err)
	}
	if !slices.ContainsFunc(f.Ran(), func(c system.Cmd) bool {
		return c.Name == "systemctl" && slices.Equal(c.Args, []string{"restart", unit})
	}) {
		t.Fatalf("phải thử khởi động lại %s; lệnh %v", unit, f.Ran())
	}
	if done, err := s.Check(ctx, f); err != nil || !done {
		t.Fatalf("sau Apply, Check phải true (engine không coi là bước không có tác dụng), có %v, %v", done, err)
	}
}
