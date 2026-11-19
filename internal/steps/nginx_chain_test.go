package steps

import (
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/system"
)

// nginxChain là chuỗi bước Nginx của mục 6.1 bước 5: gỡ site default của
// Ubuntu, ghi tpan-default.conf, bật nó bằng symlink. Mỗi bước kiểm bằng
// `nginx -t` rồi `systemctl reload nginx`.
func nginxChain() []engine.Step {
	return []engine.Step{
		&Absent{Path: enDefault, Test: nginxTest, Reload: nginxReload},
		&File{Path: tpanConf, Content: []byte("# Do tpan quản lý.\nserver { listen 80 default_server; return 444; }\n"),
			Perm: 0o644, Owner: system.Root, Test: nginxTest, Reload: nginxReload},
		&Symlink{Target: tpanConf, Link: tpanLink, Test: nginxTest, Reload: nginxReload},
	}
}

func TestNginxChain(t *testing.T) {
	m := newMachine(t)
	nginxBase(m)
	rep := execute(t, m, nginxChain()...)
	if rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("job %s: %s", rep.Job.Status, rep.Job.Error)
	}
	if fi, err := m.Stat(enDefault); err == nil {
		t.Errorf("site default còn: %v", fi)
	}
	if got, err := m.Readlink(tpanLink); err != nil || got != tpanConf {
		t.Errorf("link = %q, %v", got, err)
	}

	// Chạy lại: mọi bước bỏ qua, không lệnh nào (kể cả nginx -t, reload) chạy.
	m.ResetRan()
	rep = execute(t, m, nginxChain()...)
	for _, s := range rep.Steps {
		if s.Outcome != "skipped" {
			t.Errorf("chạy lại: %s = %s", s.ID, s.Outcome)
		}
	}
	if len(m.Ran()) != 0 {
		t.Errorf("chạy lại vẫn chạy lệnh: %v", ranStrings(m.Ran()))
	}
}

// Reload lần thứ ba (lúc bật symlink) lỗi: Symlink tự gỡ link của nó, rồi
// engine hoàn tác File và Absent theo thứ tự ngược. Máy giả phải về đúng như cũ.
func TestNginxChainRollback(t *testing.T) {
	m := newMachine(t)
	nginxBase(m)
	before := m.whole()

	var mu sync.Mutex
	reloads := 0
	m.Handle([]string{"systemctl", "reload", "nginx"}, func(system.Cmd) system.Result {
		mu.Lock()
		defer mu.Unlock()
		if reloads++; reloads == 3 {
			return system.Result{ExitCode: 1, Stderr: "Job for nginx.service failed because the control process exited with error code."}
		}
		return system.Result{}
	})

	rep := execute(t, m, nginxChain()...)
	if rep.Job.Status != engine.JobRolledBack {
		t.Fatalf("job %s, muốn rolled_back: %s", rep.Job.Status, rep.Job.Error)
	}
	if !strings.Contains(rep.Job.Error, "symlink:"+tpanLink) || !strings.Contains(rep.Job.Error, "nạp lại dịch vụ") {
		t.Errorf("lỗi job thiếu ngữ cảnh: %s", rep.Job.Error)
	}
	outcomes := []string{rep.Steps[0].Outcome, rep.Steps[1].Outcome, rep.Steps[2].Outcome}
	if want := []string{"undone", "undone", "failed"}; !reflect.DeepEqual(outcomes, want) {
		t.Errorf("outcome %v, muốn %v", outcomes, want)
	}
	if !reflect.DeepEqual(before, m.whole()) {
		t.Errorf("sau hoàn tác máy khác trước:\ntrước %v\nsau   %v", before, m.whole())
	}

	// Thứ tự lệnh: ba bước tiến, symlink lỗi và nạp lại config cũ, rồi hoàn
	// tác File (nginx -t, reload) và Absent (nginx -t, reload).
	want := []string{
		"nginx -t", "systemctl reload nginx", // Absent
		"nginx -t", "systemctl reload nginx", // File
		"nginx -t", "systemctl reload nginx", "systemctl reload nginx", // Symlink lỗi, nạp lại
		"nginx -t", "systemctl reload nginx", // hoàn tác File
		"nginx -t", "systemctl reload nginx", // hoàn tác Absent
	}
	if got := ranStrings(m.Ran()); !reflect.DeepEqual(got, want) {
		t.Errorf("lệnh đã chạy:\n%s\nmuốn:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
