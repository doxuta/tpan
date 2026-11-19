package checks

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

// stubCheck là phép kiểm giả: trả về Result cố định và đếm số lần được gọi.
type stubCheck struct {
	id    string
	calls *[]string
}

func (s stubCheck) Run(context.Context, system.System) Result {
	*s.calls = append(*s.calls, s.id)
	return Result{ID: s.id, Status: Pass}
}

func TestRunAllGiuThuTu(t *testing.T) {
	var calls []string
	cs := []Check{
		stubCheck{"c:1", &calls}, stubCheck{"c:2", &calls}, stubCheck{"c:3", &calls},
	}
	got := RunAll(bg(), system.NewFake(), cs)
	if len(got) != 3 {
		t.Fatalf("có %d kết quả, muốn 3", len(got))
	}
	for i, id := range []string{"c:1", "c:2", "c:3"} {
		if got[i].ID != id || calls[i] != id {
			t.Errorf("vị trí %d: kết quả %q, gọi %q, muốn %q", i, got[i].ID, calls[i], id)
		}
	}
}

func TestRunAllRongVaNil(t *testing.T) {
	if got := RunAll(bg(), system.NewFake(), nil); len(got) != 0 {
		t.Errorf("danh sách rỗng phải ra rỗng, được %v", got)
	}
	got := RunAll(bg(), system.NewFake(), []Check{nil})
	if len(got) != 1 || got[0].Status != Unknown {
		t.Fatalf("phép kiểm nil phải ra Unknown, được %+v", got)
	}
	assertText(t, "Summary", got[0].Summary)
}

func TestLazyDungLucRun(t *testing.T) {
	version := ""
	built := 0
	c := Lazy(func() Check {
		built++
		if version == "" {
			return nil
		}
		return PHPFPMConfig(version)
	})
	if built != 0 {
		t.Fatalf("Lazy không được gọi fn lúc dựng, đã gọi %d lần", built)
	}

	fake := system.NewFake()
	// Lần 1: chưa biết bản PHP -> fn trả nil -> Unknown.
	r := c.Run(bg(), fake)
	assertShape(t, r, "lazy:unresolved", StageTPAN, Unknown)
	assertNoCmd(t, fake)

	// Lần 2: sau khi "cài php-fpm" đã biết bản -> chạy phép kiểm thật.
	version = "8.3"
	reply(fake, []string{"php-fpm8.3"}, 0, "", "[19-Nov-2026 10:00:00] NOTICE: configuration file /etc/php/8.3/fpm/php-fpm.conf test is successful\n")
	r = c.Run(bg(), fake)
	assertShape(t, r, "config.phpfpm:8.3", StagePHP, Pass)
	if built != 2 {
		t.Errorf("fn phải được gọi ở mỗi lần Run, đã gọi %d lần", built)
	}
}

func TestLazyFnNil(t *testing.T) {
	r := Lazy(nil).Run(bg(), system.NewFake())
	assertShape(t, r, "lazy:unresolved", StageTPAN, Unknown)
}

func TestClip(t *testing.T) {
	short := "abc"
	if got := clip(short); got != short {
		t.Errorf("chuỗi ngắn phải giữ nguyên, được %q", got)
	}

	long := strings.Repeat("dòng đầu bị cắt\n", 1000) + "KẾT LUẬN CUỐI"
	got := clip(long)
	if !strings.HasSuffix(got, "KẾT LUẬN CUỐI") {
		t.Errorf("phải giữ phần cuối, được ...%q", got[max(0, len(got)-30):])
	}
	if !strings.Contains(got, "đã cắt") {
		t.Errorf("phải ghi rõ đã cắt: %q", got[:min(80, len(got))])
	}
	if !utf8.ValidString(got) {
		t.Errorf("cắt giữa một ký tự nhiều byte: kết quả không phải UTF-8 hợp lệ")
	}
	if len(got) > maxEvidenceBytes+100 {
		t.Errorf("sau khi cắt còn %d byte, quá lớn", len(got))
	}
}

func TestExecCmdPhanBietKhongChayDuocVaMaThoat(t *testing.T) {
	fake := system.NewFake()
	reply(fake, []string{"systemctl", "is-active"}, 3, "inactive\n", "")

	res, ev, err := execCmd(bg(), fake, system.Cmd{Name: "systemctl", Args: []string{"is-active", "nginx"}})
	if err != nil {
		t.Fatalf("mã thoát 3 là câu trả lời bình thường, không phải lỗi: %v", err)
	}
	if res.ExitCode != 3 || ev.ExitCode != 3 || ev.Output != "inactive" {
		t.Errorf("res=%+v ev=%+v", res, ev)
	}
	if ev.Command != "systemctl is-active nginx" {
		t.Errorf("Command = %q", ev.Command)
	}

	_, ev, err = execCmd(cancelledCtx(), fake, system.Cmd{Name: "systemctl", Args: []string{"is-active", "nginx"}})
	if err == nil {
		t.Fatal("ctx đã huỷ phải trả lỗi 'không chạy được'")
	}
	if ev.ExitCode != -1 || ev.Output == "" {
		t.Errorf("bằng chứng của lệnh không chạy được: %+v", ev)
	}
}

func TestJoinOutputStdoutRoiStderr(t *testing.T) {
	got := joinOutput(system.Result{Stdout: "ra", Stderr: "loi\n"})
	if got != "ra\nloi" {
		t.Errorf("joinOutput = %q", got)
	}
}

func TestUnknownCoLyDoHaiNgonNgu(t *testing.T) {
	b := base{id: "x:y", stage: StageMachine, title: i18n.T("a", "b")}
	r := b.unknown(i18n.T("lý do", "reason"))
	if r.Summary.Vi != "Chưa kiểm được: lý do" || r.Summary.En != "Could not check: reason" {
		t.Errorf("Summary = %+v", r.Summary)
	}
}
