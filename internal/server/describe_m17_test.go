package server

// M17: mọi job của API chạy trên jobLockSystem; kiểu bọc này phải chuyển tiếp
// Describe của System gốc (cage.Sys), để sự kiện command và bằng chứng của
// site trong lồng in dòng systemd-run thật (mục 3.4 của
// docs/thay-doi/M17-thiet-ke.md).

import (
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/system"
)

type descSys struct{ *system.FakeSystem }

func (descSys) Describe(cmd system.Cmd) string { return "XEM " + cmd.String() }

func TestJobLockSystemChuyenTiepDescribe(t *testing.T) {
	var _ system.Describer = jobLockSystem{}
	j := jobLockSystem{System: descSys{system.NewFake()}}
	cmd := system.Cmd{Name: "wp", Args: []string{"core", "version"}, AsUser: "tp_a_example_com_7653e4"}
	if got := system.Describe(j, cmd); !strings.HasPrefix(got, "XEM ") {
		t.Fatalf("jobLockSystem phải trả Describe của System gốc, có %q", got)
	}
	// System gốc không có Describe: như trước M17, cmd.String().
	plain := jobLockSystem{System: system.NewFake()}
	if got := system.Describe(plain, cmd); got != cmd.String() {
		t.Fatalf("System gốc không có Describe: muốn %q, có %q", cmd.String(), got)
	}
}
