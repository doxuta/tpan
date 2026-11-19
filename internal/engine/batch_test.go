package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

// ---- Step giả chạy song song được -----------------------------------------
//
// fakeStep của engine_test.go ghi nhật ký vào một slice dùng chung không khoá;
// các unit chạy song song sẽ đua nhau trong chính bản giả. bStep ghi vào
// syncLog có khoá. Trạng thái của từng bStep chỉ do một goroutine chạm tới
// (bước của unit nào chạy trong goroutine của unit đó).

type syncLog struct {
	mu    sync.Mutex
	lines []string
}

func (l *syncLog) add(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, s)
}

// only trả các dòng có tiền tố prefix (ví dụ "apply:"), nối bằng dấu cách.
func (l *syncLog) only(prefix string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for _, s := range l.lines {
		if strings.HasPrefix(s, prefix) {
			out = append(out, s)
		}
	}
	return strings.Join(out, " ")
}

func (l *syncLog) count(s string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, x := range l.lines {
		if x == s {
			n++
		}
	}
	return n
}

// index trả vị trí đầu tiên của s, -1 nếu không có.
func (l *syncLog) index(s string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i, x := range l.lines {
		if x == s {
			return i
		}
	}
	return -1
}

type bStep struct {
	id    string
	log   *syncLog
	files []FileChange
	done  bool
	// reload: như bước reload của pha B, Check chỉ true ngay sau Apply, nên
	// lần chạy nào cũng Apply. Không có cờ này thì lần chạy lại Commit sẽ bỏ
	// qua bước và test "Commit chạy hai lần" đúng nhờ may.
	reload, fresh bool
	applyErr      error
	failOnApply   int // > 0: lỗi ở lần Apply thứ n
	applies       int
	undoErr       error
	onApply       func(ctx context.Context)
	cmd           bool // Apply chạy một lệnh qua sys
}

func (s *bStep) Info() StepInfo {
	return StepInfo{ID: s.id, Title: i18n.T("bước "+s.id, "step "+s.id), Files: s.files}
}

func (s *bStep) Check(context.Context, system.System) (bool, error) {
	s.log.add("check:" + s.id)
	if s.reload {
		d := s.fresh
		s.fresh = false
		return d, nil
	}
	return s.done, nil
}

func (s *bStep) Apply(ctx context.Context, sys system.System) error {
	s.log.add("apply:" + s.id)
	s.applies++
	if s.onApply != nil {
		s.onApply(ctx)
	}
	if s.cmd {
		if _, err := sys.Run(ctx, system.Cmd{Name: "echo", Args: []string{s.id}}); err != nil {
			return err
		}
	}
	if s.applyErr != nil {
		return s.applyErr
	}
	if s.failOnApply == s.applies {
		return fmt.Errorf("lần %d hỏng", s.applies)
	}
	s.done, s.fresh = true, true
	return nil
}

func (s *bStep) Undo(context.Context, system.System) error {
	s.log.add("undo:" + s.id)
	if s.undoErr != nil {
		return s.undoErr
	}
	s.done = false
	return nil
}

// ---- Dựng Batch mẫu ---------------------------------------------------------

// bt dựng một Batch mẫu: Pre [pre]; mỗi unit T có Prepare [prep1:T prep2:T],
// Finish [fin:T]; Commit [reload:web reload:php] (cả hai là bước reload).
type bt struct {
	h     *harness
	log   *syncLog
	steps map[string]*bStep
	units []string
	// verify[T]: phép kiểm của unit T.
	verify map[string][]checks.Check
}

func newBT(targets ...string) *bt {
	t := &bt{h: newHarness(), log: &syncLog{}, steps: map[string]*bStep{}, units: targets, verify: map[string][]checks.Check{}}
	t.step("pre")
	for _, u := range targets {
		t.step("prep1:" + u)
		t.step("prep2:" + u)
		t.step("fin:" + u)
	}
	t.step("reload:web").reload = true
	t.step("reload:php").reload = true
	return t
}

func (t *bt) step(id string) *bStep {
	s := &bStep{id: id, log: t.log}
	t.steps[id] = s
	return s
}

func (t *bt) batch(parallel int) Batch {
	b := Batch{
		Kind: "site.batch", Target: "lô thử", Title: i18n.T("thử lô", "test batch"),
		Pre:      []Step{t.steps["pre"]},
		Commit:   []Step{t.steps["reload:web"], t.steps["reload:php"]},
		Parallel: parallel,
	}
	for _, u := range t.units {
		b.Units = append(b.Units, Unit{
			Target:  u,
			Prepare: []Step{t.steps["prep1:"+u], t.steps["prep2:"+u]},
			Finish:  []Step{t.steps["fin:"+u]},
			Verify:  t.verify[u],
		})
	}
	return b
}

func (t *bt) run(tt *testing.T, parallel int) BatchReport {
	tt.Helper()
	r, err := t.h.eng.ExecuteBatch(context.Background(), t.batch(parallel))
	if err != nil {
		tt.Fatalf("ExecuteBatch: %v", err)
	}
	return r
}

// units tóm tắt bảng kết quả: "a=succeeded b=rolled_back/prepare/prep2:b".
func units(r BatchReport) string {
	var parts []string
	for _, u := range r.Units {
		p := u.Target + "=" + string(u.Status)
		if u.Phase != "" {
			p += "/" + u.Phase
		}
		if u.StepID != "" {
			p += "/" + u.StepID
		}
		parts = append(parts, p)
	}
	return strings.Join(parts, " ")
}

// outcome trả Outcome của bước id trong Report.
func outcome(r BatchReport, id string) string {
	for _, s := range r.Steps {
		if s.ID == id {
			return s.Outcome
		}
	}
	return "?"
}

// ---- Test -------------------------------------------------------------------

func TestBatchAllSucceedCommitOnceInOrder(t *testing.T) {
	b := newBT("a", "b")
	b.steps["pre"].files = []FileChange{{Path: "/cache/wp.zip", New: []byte("zip")}}
	b.steps["prep1:a"].files = []FileChange{{Path: "/etc/a", New: []byte("A")}}
	b.steps["fin:b"].files = []FileChange{{Path: "/etc/b", Delete: true}}
	r := b.run(t, 1)

	wantEq(t, "status", r.Job.Status, JobSucceeded)
	wantEq(t, "job.error", r.Job.Error, "")
	wantEq(t, "units", units(r), "a=succeeded b=succeeded")
	// Report.Steps theo thứ tự Pre, Prepare từng unit, Commit, Finish từng unit.
	wantEq(t, "steps", outcomes(r.Report),
		"pre=applied prep1:a=applied prep2:a=applied prep1:b=applied prep2:b=applied "+
			"reload:web=applied reload:php=applied fin:a=applied fin:b=applied")
	// Parallel = 1: chạy đúng thứ tự; Commit đúng một lần, sau mọi Prepare.
	wantEq(t, "apply", b.log.only("apply:"),
		"apply:pre apply:prep1:a apply:prep2:a apply:prep1:b apply:prep2:b "+
			"apply:reload:web apply:reload:php apply:fin:a apply:fin:b")
	wantEq(t, "undo", b.log.only("undo:"), "")

	if len(b.h.j.files) != 2 || b.h.j.files["/cache/wp.zip"] == "" || b.h.j.files["/etc/a"] == "" {
		t.Errorf("RecordFile = %v", b.h.j.files)
	}
	wantEq(t, "ForgetFile", strings.Join(b.h.j.forgot, ","), "/etc/b")
	wantEq(t, "locks", b.h.sys.locks, 1)
	wantEq(t, "unlocks", b.h.sys.unlocks, 1)
	if len(b.h.j.finished) != 1 || b.h.j.finished[0].Status != JobSucceeded {
		t.Errorf("FinishJob = %+v", b.h.j.finished)
	}
}

func TestBatchPrepareFailUndoesOnlyThatUnit(t *testing.T) {
	b := newBT("a", "b", "c")
	b.steps["prep2:b"].applyErr = errors.New("tạo DB hỏng")
	b.steps["prep1:b"].files = []FileChange{{Path: "/etc/b", New: []byte("B")}}
	b.steps["prep1:a"].files = []FileChange{{Path: "/etc/a", New: []byte("A")}}
	r := b.run(t, 1)

	wantEq(t, "status", r.Job.Status, JobSucceeded)
	wantEq(t, "units", units(r), "a=succeeded b=rolled_back/prepare/prep2:b c=succeeded")
	if !strings.Contains(r.Units[1].Error, "tạo DB hỏng") {
		t.Errorf("lỗi của b = %q", r.Units[1].Error)
	}
	if !strings.Contains(r.Job.Error, "1/3") || !strings.Contains(r.Job.Error, "b (prepare)") {
		t.Errorf("Job.Error = %q", r.Job.Error)
	}
	wantEq(t, "undo", b.log.only("undo:"), "undo:prep1:b")
	wantEq(t, "prep1:b", outcome(r, "prep1:b"), outcomeUndone)
	wantEq(t, "prep2:b", outcome(r, "prep2:b"), outcomeFailed)
	wantEq(t, "fin:b", outcome(r, "fin:b"), outcomeNotRun)
	wantEq(t, "fin:c", outcome(r, "fin:c"), outcomeApplied)
	wantEq(t, "commit chạy một lần", b.log.count("apply:reload:web"), 1)
	// Commit chạy sau khi b đã được hoàn tác ở pha A.
	if b.log.index("apply:reload:web") < b.log.index("undo:prep1:b") {
		t.Error("Commit chạy trước khi b được hoàn tác")
	}
	// File của unit lỗi không được ghi vào Journal.
	if _, ok := b.h.j.files["/etc/b"]; ok || b.h.j.files["/etc/a"] == "" {
		t.Errorf("RecordFile = %v", b.h.j.files)
	}
}

func TestBatchCommitFailUndoesEverything(t *testing.T) {
	b := newBT("a", "b")
	b.steps["reload:php"].applyErr = errors.New("php-fpm -t lỗi")
	b.steps["prep1:a"].files = []FileChange{{Path: "/etc/a", New: []byte("A")}}
	r := b.run(t, 1)

	wantEq(t, "status", r.Job.Status, JobRolledBack)
	wantEq(t, "units", units(r), "a=rolled_back/commit b=rolled_back/commit")
	// Ngược đúng thứ tự chạy: Commit, unit b, unit a, rồi Pre.
	wantEq(t, "undo", b.log.only("undo:"),
		"undo:reload:web undo:prep2:b undo:prep1:b undo:prep2:a undo:prep1:a undo:pre")
	wantEq(t, "fin:a", outcome(r, "fin:a"), outcomeNotRun)
	wantEq(t, "reload:php", outcome(r, "reload:php"), outcomeFailed)
	wantEq(t, "pre", outcome(r, "pre"), outcomeUndone)
	// Lỗi chung nói một lần, không lặp theo từng unit.
	if strings.Count(r.Job.Error, "php-fpm -t lỗi") != 1 || strings.Contains(r.Job.Error, "2/2") {
		t.Errorf("Job.Error = %q", r.Job.Error)
	}
	if !strings.Contains(r.Units[0].Error, "php-fpm -t lỗi") {
		t.Errorf("lỗi của a = %q", r.Units[0].Error)
	}
	if len(b.h.j.files)+len(b.h.j.forgot) != 0 {
		t.Errorf("job lỗi mà vẫn ghi file: %v %v", b.h.j.files, b.h.j.forgot)
	}
}

func TestBatchFinishOrVerifyFailRecommits(t *testing.T) {
	cases := []struct {
		name  string
		setup func(b *bt)
		want  string
	}{
		{"verify Fail", func(b *bt) {
			b.verify["b"] = []checks.Check{fakeCheck{id: "http:b", st: checks.Fail}}
		}, "a=succeeded b=rolled_back/verify"},
		{"Finish lỗi", func(b *bt) {
			b.steps["fin:b"].applyErr = errors.New("wp core install hỏng")
		}, "a=succeeded b=rolled_back/finish/fin:b"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := newBT("a", "b")
			b.verify["a"] = []checks.Check{fakeCheck{id: "http:a", st: checks.Pass}}
			b.steps["prep1:a"].files = []FileChange{{Path: "/etc/a", New: []byte("A")}}
			b.steps["prep1:b"].files = []FileChange{{Path: "/etc/b", New: []byte("B")}}
			c.setup(b)
			r := b.run(t, 1)

			wantEq(t, "status", r.Job.Status, JobSucceeded)
			wantEq(t, "units", units(r), c.want)
			// Finish rồi Prepare của b, ngược; a không bị đụng.
			undo := b.log.only("undo:")
			if c.name == "verify Fail" {
				wantEq(t, "undo", undo, "undo:fin:b undo:prep2:b undo:prep1:b")
			} else {
				wantEq(t, "undo", undo, "undo:prep2:b undo:prep1:b")
			}
			// Commit chạy lần hai, sau khi b đã được gỡ.
			wantEq(t, "reload:web", b.log.count("apply:reload:web"), 2)
			wantEq(t, "reload:php", b.log.count("apply:reload:php"), 2)
			if b.log.only("apply:reload:web") == "" ||
				lastIndex(b.log, "apply:reload:web") < b.log.index("undo:prep1:b") {
				t.Error("Commit không chạy lại sau khi hoàn tác b")
			}
			wantEq(t, "reload:web outcome", outcome(r, "reload:web"), outcomeApplied)
			if !reflect.DeepEqual(mapKeys(b.h.j.files), []string{"/etc/a"}) {
				t.Errorf("RecordFile = %v", b.h.j.files)
			}
		})
	}
}

func TestBatchVerifyChecksInReport(t *testing.T) {
	b := newBT("a", "b")
	b.verify["a"] = []checks.Check{fakeCheck{id: "http:a", st: checks.Pass}}
	b.verify["b"] = []checks.Check{fakeCheck{id: "http:b", st: checks.Fail}, fakeCheck{id: "php:b", st: checks.Warn}}
	r := b.run(t, 2)

	var ids []string
	for _, c := range r.Checks {
		ids = append(ids, c.ID)
	}
	wantEq(t, "checks", strings.Join(ids, " "), "http:a http:b php:b")
	wantEq(t, "checks của b", len(r.Units[1].Checks), 2)
	n := 0
	for _, ev := range b.h.events {
		if ev.Kind == EventCheck {
			n++
		}
	}
	wantEq(t, "sự kiện check", n, 3)
}

func TestBatchAllUnitsFail(t *testing.T) {
	t.Run("ở pha A", func(t *testing.T) {
		b := newBT("a", "b")
		b.steps["prep2:a"].applyErr = errors.New("hỏng a")
		b.steps["prep2:b"].applyErr = errors.New("hỏng b")
		r := b.run(t, 2)

		wantEq(t, "status", r.Job.Status, JobRolledBack)
		wantEq(t, "units", units(r), "a=rolled_back/prepare/prep2:a b=rolled_back/prepare/prep2:b")
		wantEq(t, "pre", outcome(r, "pre"), outcomeUndone)
		wantEq(t, "commit không chạy", b.log.count("apply:reload:web"), 0)
		wantEq(t, "reload:web", outcome(r, "reload:web"), outcomeNotRun)
		if !strings.Contains(r.Job.Error, "2/2") {
			t.Errorf("Job.Error = %q", r.Job.Error)
		}
	})
	t.Run("ở pha C", func(t *testing.T) {
		b := newBT("a", "b")
		b.steps["pre"].files = []FileChange{{Path: "/cache/wp.zip", New: []byte("zip")}}
		b.verify["a"] = []checks.Check{fakeCheck{id: "http:a", st: checks.Fail}}
		b.verify["b"] = []checks.Check{fakeCheck{id: "http:b", st: checks.Fail}}
		r := b.run(t, 2)

		wantEq(t, "status", r.Job.Status, JobRolledBack)
		wantEq(t, "units", units(r), "a=rolled_back/verify b=rolled_back/verify")
		wantEq(t, "commit chạy lại", b.log.count("apply:reload:web"), 2)
		wantEq(t, "pre", outcome(r, "pre"), outcomeUndone)
		// Pre được trả lại sau khi Commit đã chạy lại.
		if b.log.index("undo:pre") < lastIndex(b.log, "apply:reload:php") {
			t.Error("Pre bị hoàn tác trước khi chạy lại Commit")
		}
		if len(b.h.j.files)+len(b.h.j.forgot) != 0 {
			t.Errorf("job lỗi mà vẫn ghi file: %v", b.h.j.files)
		}
	})
}

func TestBatchPreFailNoUnitRuns(t *testing.T) {
	b := newBT("a", "b")
	b.steps["pre"].applyErr = errors.New("tải WordPress hỏng")
	r := b.run(t, 2)

	wantEq(t, "status", r.Job.Status, JobRolledBack)
	wantEq(t, "units", units(r), "a=not_run b=not_run")
	wantEq(t, "apply", b.log.only("apply:"), "apply:pre")
	if !strings.Contains(r.Job.Error, "tải WordPress hỏng") {
		t.Errorf("Job.Error = %q", r.Job.Error)
	}
	n := 0
	for _, ev := range b.h.events {
		if ev.Kind == EventUnitDone && strings.Contains(ev.Message, `"not_run"`) {
			n++
		}
	}
	wantEq(t, "unit.done not_run", n, 2)
}

func TestBatchUndoFailMarksUnitAndJob(t *testing.T) {
	b := newBT("a", "b")
	b.steps["prep2:b"].applyErr = errors.New("hỏng")
	b.steps["prep1:b"].undoErr = errors.New("không xoá được user")
	r := b.run(t, 1)

	wantEq(t, "status", r.Job.Status, JobUndoFailed)
	wantEq(t, "units", units(r), "a=succeeded b=undo_failed/prepare/prep2:b")
	wantEq(t, "prep1:b", outcome(r, "prep1:b"), outcomeUndoFailed)
	if !strings.Contains(r.Units[1].Error, "không xoá được user") {
		t.Errorf("lỗi của b = %q", r.Units[1].Error)
	}
}

func TestBatchRecommitFailIsUndoFailed(t *testing.T) {
	b := newBT("a", "b")
	b.verify["b"] = []checks.Check{fakeCheck{id: "http:b", st: checks.Fail}}
	b.steps["reload:php"].failOnApply = 2
	r := b.run(t, 1)

	wantEq(t, "status", r.Job.Status, JobUndoFailed)
	wantEq(t, "units", units(r), "a=succeeded b=rolled_back/verify")
	if !strings.Contains(r.Job.Error, "chạy lại") || !strings.Contains(r.Job.Error, "lần 2 hỏng") {
		t.Errorf("Job.Error = %q", r.Job.Error)
	}
}

// Người dùng dừng job giữa pha C: unit đang chạy bị hoàn tác, và Commit vẫn
// phải chạy lại được (bằng ctx không bị huỷ) để web server thôi nạp file đã gỡ.
func TestBatchCancelDuringFinishStillRecommits(t *testing.T) {
	b := newBT("a", "b")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.steps["fin:b"].onApply = func(context.Context) { cancel() }
	r, err := b.h.eng.ExecuteBatch(ctx, b.batch(1))
	if err != nil {
		t.Fatal(err)
	}
	wantEq(t, "units", units(r), "a=succeeded b=rolled_back/verify")
	wantEq(t, "commit chạy lại", b.log.count("apply:reload:php"), 2)
	wantEq(t, "status", r.Job.Status, JobSucceeded)
}

func TestBatchParallelLimit(t *testing.T) {
	for _, c := range []struct{ parallel, want int }{{2, 2}, {3, 3}, {0, 1}, {1, 1}} {
		t.Run(fmt.Sprint(c.parallel), func(t *testing.T) {
			targets := []string{"a", "b", "c", "d", "e"}
			b := newBT(targets...)
			var cur, peak atomic.Int32
			// Mỗi Apply chờ tới khi đủ c.want bước cùng chạy (hoặc hết giờ),
			// để engine có cơ hội vượt giới hạn nếu nó sai.
			gate := func(context.Context) {
				n := cur.Add(1)
				for {
					p := peak.Load()
					if n <= p || peak.CompareAndSwap(p, n) {
						break
					}
				}
				deadline := time.Now().Add(200 * time.Millisecond)
				for cur.Load() < int32(c.want) && time.Now().Before(deadline) {
					time.Sleep(time.Millisecond)
				}
				time.Sleep(5 * time.Millisecond)
				cur.Add(-1)
			}
			for _, u := range targets {
				b.steps["prep1:"+u].onApply = gate
				b.steps["fin:"+u].onApply = gate
			}
			r := b.run(t, c.parallel)

			wantEq(t, "status", r.Job.Status, JobSucceeded)
			wantEq(t, "số bước chạy cùng lúc tối đa", int(peak.Load()), c.want)
			if c.want == 1 {
				wantEq(t, "thứ tự", b.log.only("apply:prep1:"),
					"apply:prep1:a apply:prep1:b apply:prep1:c apply:prep1:d apply:prep1:e")
			}
			// Commit không bao giờ chạy cùng bước nào khác: nó chạy sau mọi
			// Prepare và trước mọi Finish.
			web := b.log.index("apply:reload:web")
			for _, u := range targets {
				if b.log.index("apply:prep2:"+u) > web || b.log.index("apply:fin:"+u) < web {
					t.Errorf("unit %s chạy chồng lên Commit", u)
				}
			}
		})
	}
}

func TestBatchUnitDoneEvents(t *testing.T) {
	b := newBT("a", "b", "c")
	b.steps["prep2:b"].applyErr = errors.New("hỏng b")
	b.verify["c"] = []checks.Check{fakeCheck{id: "http:c", st: checks.Fail}}
	b.verify["a"] = []checks.Check{fakeCheck{id: "http:a", st: checks.Pass}}
	b.steps["prep1:a"].cmd = true
	r := b.run(t, 3)

	got := map[string]UnitResult{}
	jobDone := -1
	for i, ev := range b.h.events {
		wantEq(t, "seq", ev.Seq, i+1)
		switch ev.Kind {
		case EventUnitDone:
			if _, dup := got[ev.StepID]; dup {
				t.Errorf("unit.done hai lần cho %s", ev.StepID)
			}
			if strings.Contains(ev.Message, `"checks"`) {
				t.Errorf("unit.done mang Checks: %s", ev.Message)
			}
			var ur UnitResult
			if err := json.Unmarshal([]byte(ev.Message), &ur); err != nil {
				t.Fatalf("unit.done không phải JSON: %v", err)
			}
			got[ev.StepID] = ur
			if jobDone >= 0 {
				t.Error("unit.done sau job.done")
			}
		case EventJobDone:
			jobDone = i
		}
	}
	if len(got) != 3 {
		t.Fatalf("có %d sự kiện unit.done, muốn 3", len(got))
	}
	for _, want := range r.Units {
		want.Checks = nil
		if !reflect.DeepEqual(got[want.Target], want) {
			t.Errorf("unit.done %s = %+v, muốn %+v", want.Target, got[want.Target], want)
		}
	}
	// Journal nhận cùng chuỗi sự kiện với OnEvent.
	wantEq(t, "journal", len(b.h.j.events), len(b.h.events))
	// Lệnh của bước trong unit vẫn thành sự kiện command gắn StepID.
	found := false
	for _, ev := range b.h.events {
		found = found || (ev.Kind == EventCommand && ev.StepID == "prep1:a")
	}
	if !found {
		t.Error("thiếu sự kiện command của prep1:a")
	}
}

func TestBatchRejectsBadBatch(t *testing.T) {
	cases := []struct {
		name string
		edit func(b *Batch, t *bt)
		want string
	}{
		{"trùng StepID giữa hai unit", func(b *Batch, t *bt) {
			b.Units[1].Finish = append(b.Units[1].Finish, t.steps["fin:a"])
		}, `"fin:a" xuất hiện hai lần`},
		{"trùng StepID giữa Pre và Commit", func(b *Batch, t *bt) {
			b.Commit = append(b.Commit, t.steps["pre"])
		}, `"pre" xuất hiện hai lần`},
		{"trùng Target", func(b *Batch, _ *bt) {
			b.Units[1].Target = "a"
		}, `"a" xuất hiện hai lần`},
		{"Target rỗng", func(b *Batch, _ *bt) {
			b.Units[0].Target = ""
		}, "không có tên"},
		{"không có unit", func(b *Batch, _ *bt) {
			b.Units = nil
		}, "không có đối tượng"},
		{"Info panic", func(b *Batch, _ *bt) {
			b.Pre = append(b.Pre, &infoPanicStep{})
		}, "panic"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := newBT("a", "b")
			batch := b.batch(1)
			c.edit(&batch, b)
			_, err := b.h.eng.ExecuteBatch(context.Background(), batch)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, muốn chứa %q", err, c.want)
			}
			// Từ chối trước khi lấy khoá: không khoá, không job, không sự kiện.
			wantEq(t, "locks", b.h.sys.locks, 0)
			if len(b.h.j.created)+len(b.h.j.events)+len(b.h.events) != 0 {
				t.Error("Batch sai mà vẫn tạo job hoặc phát sự kiện")
			}
			wantEq(t, "log", b.log.only(""), "")
		})
	}
}

func TestBatchLockError(t *testing.T) {
	b := newBT("a")
	b.h.sys.lockErr = errors.New("job khác đang chạy")
	_, err := b.h.eng.ExecuteBatch(context.Background(), b.batch(1))
	if err == nil || !strings.Contains(err.Error(), "job khác đang chạy") {
		t.Errorf("err = %v", err)
	}
	wantEq(t, "log", b.log.only(""), "")
}

func TestPreviewBatch(t *testing.T) {
	b := newBT("a", "b")
	b.h.sys.files["/etc/a"] = []byte("cũ")
	b.steps["prep1:a"].files = []FileChange{{Path: "/etc/a", New: []byte("mới")}}
	b.steps["reload:web"].fresh = true // Check trả true một lần
	pv, err := b.h.eng.PreviewBatch(context.Background(), b.batch(2))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, s := range pv.Steps {
		ids = append(ids, s.Info.ID)
	}
	wantEq(t, "thứ tự", strings.Join(ids, " "),
		"pre prep1:a prep2:a prep1:b prep2:b reload:web reload:php fin:a fin:b")
	wantEq(t, "kind", pv.Kind, "site.batch")
	if f := pv.Steps[1].Info.Files[0]; string(f.Old) != "cũ" || !f.OldExists {
		t.Errorf("Old = %q %v", f.Old, f.OldExists)
	}
	if !pv.Steps[5].Done || pv.Steps[6].Done {
		t.Errorf("Done = %v %v", pv.Steps[5].Done, pv.Steps[6].Done)
	}
	wantEq(t, "apply", b.log.only("apply:"), "")
	wantEq(t, "locks", b.h.sys.locks, 0)
	if len(b.h.j.created)+len(b.h.events) != 0 {
		t.Error("PreviewBatch ghi Journal hoặc phát sự kiện")
	}
}

// ---- tiện ích riêng của test hàng loạt ----

func lastIndex(l *syncLog, s string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := len(l.lines) - 1; i >= 0; i-- {
		if l.lines[i] == s {
			return i
		}
	}
	return -1
}

func mapKeys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
