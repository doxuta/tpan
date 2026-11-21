package engine

// M17: lệnh của site trong lồng chạy qua systemd-run (cage.Sys). Engine bọc
// System trong lúc Execute (recSys); kiểu bọc đó phải chuyển tiếp Describe, nếu
// không thì cả sự kiện command lẫn dòng "sửa tay" mà Step dựng bằng
// system.Describe đều in dạng runuser, tức là chỉ người vận hành chạy lệnh của
// site ở ngoài lồng (mục 3.4 của docs/thay-doi/M17-thiet-ke.md).

import (
	"context"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

// descSys là System giả có Describe riêng, như cage.Sys.
type descSys struct{ *system.FakeSystem }

func (descSys) Describe(cmd system.Cmd) string { return "XEM " + cmd.String() }

// describeStep dựng dòng "sửa tay" bằng system.Describe ngay trong Apply.
type describeStep struct {
	got  string
	done bool
}

func (s *describeStep) Info() StepInfo {
	return StepInfo{ID: "describe", Title: i18n.T("thử Describe", "try Describe")}
}
func (s *describeStep) Check(context.Context, system.System) (bool, error) { return s.done, nil }
func (s *describeStep) Undo(context.Context, system.System) error          { return nil }
func (s *describeStep) Apply(ctx context.Context, sys system.System) error {
	cmd := system.Cmd{Name: "wp", Args: []string{"core", "version"}, AsUser: "tp_a_example_com_7653e4"}
	s.got = system.Describe(sys, cmd)
	if _, err := sys.Run(ctx, cmd); err != nil {
		return err
	}
	s.done = true
	return nil
}

func TestRecSysChuyenTiepDescribe(t *testing.T) {
	fs := system.NewFake()
	fs.Handle([]string{"wp"}, func(system.Cmd) system.Result { return system.Result{} })
	j := newJournal()
	st := &describeStep{}
	rep, err := New(descSys{fs}, Options{Journal: j}).Execute(context.Background(),
		Plan{Kind: "test.describe", Target: "a.example.com", Title: i18n.T("thử", "try"), Steps: []Step{st}})
	if err != nil {
		t.Fatalf("Execute: %v (report %+v)", err, rep)
	}
	if !strings.HasPrefix(st.got, "XEM ") {
		t.Fatalf("Step gọi system.Describe trong Execute phải nhận Describe của System gốc, có %q", st.got)
	}
	var cmdEvents []string
	for _, ev := range j.events {
		if ev.Kind == EventCommand {
			cmdEvents = append(cmdEvents, ev.Message)
		}
	}
	if len(cmdEvents) != 1 || !strings.HasPrefix(cmdEvents[0], "XEM ") {
		t.Fatalf("sự kiện command phải là Describe của System gốc, có %q", cmdEvents)
	}
}
