package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

// ---- System giả tối giản -------------------------------------------------
//
// Chỉ cài các hàm engine gọi (Run, ReadFile, Lock). Interface nhúng là nil nên
// lỡ engine gọi hàm khác thì panic; vì engine bắt panic của Step, mọi test đều
// so outcome và lỗi chính xác để chuyện đó không lọt qua im lặng.

type fakeSys struct {
	system.System

	mu      sync.Mutex
	files   map[string][]byte
	readErr map[string]error
	ran     []string
	output  []string // các dòng mỗi lệnh Run in ra qua OnOutput
	// concurrent: in output từ hai goroutine cùng lúc, như stdout và stderr
	// của LocalSystem.
	concurrent bool
	lockErr    error
	locks      int
	unlocks    int
}

func newFakeSys() *fakeSys {
	return &fakeSys{files: map[string][]byte{}, readErr: map[string]error{}}
}

func (f *fakeSys) Run(_ context.Context, cmd system.Cmd) (system.Result, error) {
	f.mu.Lock()
	f.ran = append(f.ran, cmd.String())
	out, conc := f.output, f.concurrent
	f.mu.Unlock()
	if cmd.OnOutput != nil {
		if conc {
			var wg sync.WaitGroup
			for g := range 2 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for _, l := range out {
						cmd.OnOutput(fmt.Sprintf("%d:%s", g, l))
					}
				}()
			}
			wg.Wait()
		} else {
			for _, l := range out {
				cmd.OnOutput(l)
			}
		}
	}
	return system.Result{Stdout: strings.Join(out, "\n")}, nil
}

func (f *fakeSys) ReadFile(path string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.readErr[path]; err != nil {
		return nil, err
	}
	d, ok := f.files[path]
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: path, Err: fs.ErrNotExist}
	}
	return append([]byte(nil), d...), nil
}

func (f *fakeSys) Lock(context.Context) (func(), error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.lockErr != nil {
		return nil, f.lockErr
	}
	f.locks++
	return func() {
		f.mu.Lock()
		f.unlocks++
		f.mu.Unlock()
	}, nil
}

// ---- Step giả --------------------------------------------------------------

type fakeStep struct {
	id    string
	log   *[]string // nhật ký gọi hàm, dùng chung cho cả Plan
	done  bool      // trạng thái "máy" của bước
	files []FileChange

	checkErr, applyErr, undoErr error
	noEffect                    bool // Apply không đổi gì: Check sau Apply vẫn false
	panicApply                  bool
	runCmd                      bool // Apply chạy một lệnh qua sys
	onApply, onUndo             func(ctx context.Context)
}

func (s *fakeStep) Info() StepInfo {
	return StepInfo{ID: s.id, Title: i18n.T("bước "+s.id, "step "+s.id), Files: s.files}
}

func (s *fakeStep) Check(context.Context, system.System) (bool, error) {
	*s.log = append(*s.log, "check:"+s.id)
	return s.done, s.checkErr
}

func (s *fakeStep) Apply(ctx context.Context, sys system.System) error {
	*s.log = append(*s.log, "apply:"+s.id)
	if s.panicApply {
		panic("hỏng giữa chừng")
	}
	if s.onApply != nil {
		s.onApply(ctx)
	}
	if s.runCmd {
		if _, err := sys.Run(ctx, system.Cmd{Name: "echo", Args: []string{s.id}}); err != nil {
			return err
		}
	}
	if s.applyErr != nil {
		return s.applyErr
	}
	if !s.noEffect {
		s.done = true
	}
	return nil
}

func (s *fakeStep) Undo(ctx context.Context, _ system.System) error {
	*s.log = append(*s.log, "undo:"+s.id)
	if s.onUndo != nil {
		s.onUndo(ctx)
	}
	if s.undoErr != nil {
		return s.undoErr
	}
	s.done = false
	return nil
}

// ---- Journal giả -----------------------------------------------------------

type memJournal struct {
	mu        sync.Mutex
	created   []Job
	finished  []Job
	events    []Event
	files     map[string]string // path -> sha256 hex
	fileJob   map[string]string // path -> job id
	forgot    []string
	createErr error
	failAfter int // >0: AddEvent lỗi từ sự kiện thứ failAfter trở đi
}

func newJournal() *memJournal {
	return &memJournal{files: map[string]string{}, fileJob: map[string]string{}}
}

// alive trả lỗi nếu engine ghi Journal bằng một ctx đã bị huỷ.
func alive(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("journal nhận ctx đã huỷ: %w", err)
	}
	return nil
}

func (j *memJournal) CreateJob(ctx context.Context, job Job) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.createErr != nil {
		return j.createErr
	}
	j.created = append(j.created, job)
	return alive(ctx)
}

func (j *memJournal) AddEvent(ctx context.Context, ev Event) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.failAfter > 0 && ev.Seq >= j.failAfter {
		return errors.New("đĩa đầy")
	}
	j.events = append(j.events, ev)
	return alive(ctx)
}

func (j *memJournal) FinishJob(ctx context.Context, job Job) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.finished = append(j.finished, job)
	return alive(ctx)
}

func (j *memJournal) RecordFile(ctx context.Context, path, sum, jobID string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.files[path], j.fileJob[path] = sum, jobID
	return alive(ctx)
}

func (j *memJournal) ForgetFile(ctx context.Context, path string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.forgot = append(j.forgot, path)
	return alive(ctx)
}

// ---- Phép kiểm giả ---------------------------------------------------------

type fakeCheck struct {
	id string
	st checks.Status
	fn func()
}

func (c fakeCheck) Run(context.Context, system.System) checks.Result {
	if c.fn != nil {
		c.fn()
	}
	return checks.Result{ID: c.id, Status: c.st, Summary: i18n.T("kết quả "+c.id, "result "+c.id)}
}

// ---- Tiện ích --------------------------------------------------------------

var fixedNow = time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)

type harness struct {
	sys    *fakeSys
	j      *memJournal
	eng    *Engine
	log    []string
	events []Event // từ OnEvent
}

func newHarness() *harness {
	h := &harness{sys: newFakeSys(), j: newJournal()}
	var mu sync.Mutex
	h.eng = New(h.sys, Options{
		Journal: h.j,
		OnEvent: func(ev Event) {
			mu.Lock()
			h.events = append(h.events, ev)
			mu.Unlock()
		},
		Now: func() time.Time { return fixedNow },
	})
	return h
}

func (h *harness) step(id string) *fakeStep { return &fakeStep{id: id, log: &h.log} }

func plan(steps ...Step) Plan {
	return Plan{Kind: "test.run", Target: "example.com", Title: i18n.T("thử", "test"), Steps: steps}
}

func outcomes(r Report) string {
	var parts []string
	for _, s := range r.Steps {
		parts = append(parts, s.ID+"="+s.Outcome)
	}
	return strings.Join(parts, " ")
}

func kinds(evs []Event) string {
	var parts []string
	for _, e := range evs {
		p := string(e.Kind)
		if e.StepID != "" {
			p += ":" + e.StepID
		}
		parts = append(parts, p)
	}
	return strings.Join(parts, " ")
}

func mustExecute(t *testing.T, h *harness, p Plan) Report {
	t.Helper()
	r, err := h.eng.Execute(context.Background(), p)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return r
}

func wantEq[T comparable](t *testing.T, what string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s:\n got  %v\n want %v", what, got, want)
	}
}

// ---- Test ------------------------------------------------------------------

func TestSkipsStepAlreadyDone(t *testing.T) {
	h := newHarness()
	a, b := h.step("a"), h.step("b")
	b.done = true
	r := mustExecute(t, h, plan(a, b))

	wantEq(t, "status", r.Job.Status, JobSucceeded)
	wantEq(t, "outcomes", outcomes(r), "a=applied b=skipped")
	wantEq(t, "log", strings.Join(h.log, " "), "check:a apply:a check:a check:b")
}

func TestCheckStillFalseAfterApplyFailsAndUndoes(t *testing.T) {
	h := newHarness()
	a, b, c := h.step("a"), h.step("b"), h.step("c")
	b.noEffect = true
	r := mustExecute(t, h, plan(a, b, c))

	wantEq(t, "status", r.Job.Status, JobRolledBack)
	// b đã Apply thành công (máy có thể đã đổi) nên cũng được hoàn tác,
	// nhưng vẫn giữ outcome "failed" để biết bước nào hỏng.
	wantEq(t, "outcomes", outcomes(r), "a=undone b=failed c=not_run")
	wantEq(t, "log", strings.Join(h.log, " "),
		"check:a apply:a check:a check:b apply:b check:b undo:b undo:a")
	if !strings.Contains(r.Job.Error, "bước b") || !strings.Contains(r.Job.Error, "kiểm lại vẫn chưa đạt") {
		t.Errorf("Job.Error = %q", r.Job.Error)
	}
	wantEq(t, "b.Error", r.Steps[1].Error, r.Job.Error)
}

func TestUndoReverseOrderOnlyApplied(t *testing.T) {
	h := newHarness()
	a, b, c, d, e := h.step("a"), h.step("b"), h.step("c"), h.step("d"), h.step("e")
	b.done = true
	d.applyErr = errors.New("ghi vhost lỗi")
	r := mustExecute(t, h, plan(a, b, c, d, e))

	wantEq(t, "status", r.Job.Status, JobRolledBack)
	wantEq(t, "outcomes", outcomes(r), "a=undone b=skipped c=undone d=failed e=not_run")
	// d Apply lỗi: không phải "Apply thành công" nên không Undo. b bỏ qua: không Undo.
	wantEq(t, "log", strings.Join(h.log, " "),
		"check:a apply:a check:a check:b check:c apply:c check:c check:d apply:d undo:c undo:a")
	wantEq(t, "Job.Error", r.Job.Error, "bước d: thực hiện: ghi vhost lỗi")
	if a.done || c.done {
		t.Error("a hoặc c chưa được hoàn tác")
	}
	if len(h.j.finished) != 1 || h.j.finished[0].Status != JobRolledBack {
		t.Errorf("FinishJob = %+v", h.j.finished)
	}
}

func TestUndoErrorIsReported(t *testing.T) {
	h := newHarness()
	a, b, c := h.step("a"), h.step("b"), h.step("c")
	a.undoErr = errors.New("không xoá được user")
	c.applyErr = errors.New("hỏng")
	r := mustExecute(t, h, plan(a, b, c))

	wantEq(t, "status", r.Job.Status, JobUndoFailed)
	// Undo của a lỗi không làm dừng việc hoàn tác b.
	wantEq(t, "outcomes", outcomes(r), "a=undo_failed b=undone c=failed")
	wantEq(t, "a.Error", r.Steps[0].Error, "hoàn tác bước a: không xoá được user")
	wantEq(t, "Job.Error là lỗi đầu tiên", r.Job.Error, "bước c: thực hiện: hỏng")

	var found bool
	for _, ev := range h.events {
		if ev.Kind == EventUndoFail && ev.StepID == "a" && strings.Contains(ev.Message, "không xoá được user") {
			found = true
		}
	}
	if !found {
		t.Errorf("thiếu sự kiện undo.fail cho a: %s", kinds(h.events))
	}
	wantEq(t, "job.done", h.events[len(h.events)-1].Message, string(JobUndoFailed))
}

func TestFailingStepUndoErrorKeepsBothErrors(t *testing.T) {
	h := newHarness()
	a := h.step("a")
	a.noEffect = true
	a.undoErr = errors.New("kẹt")
	r := mustExecute(t, h, plan(a))

	wantEq(t, "status", r.Job.Status, JobUndoFailed)
	wantEq(t, "outcomes", outcomes(r), "a=undo_failed")
	wantEq(t, "a.Error", r.Steps[0].Error,
		"bước a: đã chạy xong nhưng kiểm lại vẫn chưa đạt; hoàn tác bước a: kẹt")
}

func TestRerunFinishedPlanSkipsAll(t *testing.T) {
	h := newHarness()
	p := plan(h.step("a"), h.step("b"))
	first := mustExecute(t, h, p)
	wantEq(t, "lần 1", outcomes(first), "a=applied b=applied")

	h.log = nil
	second := mustExecute(t, h, p)
	wantEq(t, "status", second.Job.Status, JobSucceeded)
	wantEq(t, "lần 2", outcomes(second), "a=skipped b=skipped")
	wantEq(t, "log", strings.Join(h.log, " "), "check:a check:b")
	if first.Job.ID == second.Job.ID {
		t.Error("hai job trùng ID")
	}
}

func TestPreview(t *testing.T) {
	h := newHarness()
	h.sys.files["/etc/x"] = []byte("cũ")
	a, b, c := h.step("a"), h.step("b"), h.step("c")
	a.files = []FileChange{{Path: "/etc/x", New: []byte("mới")}, {Path: "/etc/y", New: []byte("y")}}
	b.done = true
	c.checkErr = errors.New("chưa có nginx")

	pv, err := h.eng.Preview(context.Background(), plan(a, b, c))
	if err != nil {
		t.Fatal(err)
	}
	wantEq(t, "kind", pv.Kind, "test.run")
	wantEq(t, "target", pv.Target, "example.com")
	if len(pv.Steps) != 3 {
		t.Fatalf("có %d bước", len(pv.Steps))
	}
	fx, fy := pv.Steps[0].Info.Files[0], pv.Steps[0].Info.Files[1]
	if string(fx.Old) != "cũ" || !fx.OldExists {
		t.Errorf("/etc/x: Old=%q OldExists=%v", fx.Old, fx.OldExists)
	}
	if fy.Old != nil || fy.OldExists {
		t.Errorf("/etc/y: Old=%q OldExists=%v", fy.Old, fy.OldExists)
	}
	if pv.Steps[0].Done || !pv.Steps[1].Done || pv.Steps[2].Done {
		t.Errorf("Done = %v %v %v", pv.Steps[0].Done, pv.Steps[1].Done, pv.Steps[2].Done)
	}
	wantEq(t, "CheckError", pv.Steps[2].CheckError, "chưa có nginx")

	// Không Apply, không khoá, không ghi Journal, không phát sự kiện.
	wantEq(t, "log", strings.Join(h.log, " "), "check:a check:b check:c")
	wantEq(t, "locks", h.sys.locks, 0)
	if len(h.j.created)+len(h.j.events) != 0 || len(h.events) != 0 {
		t.Error("Preview đã ghi Journal hoặc phát sự kiện")
	}
	// Không ghi Old vào slice của chính Step.
	if a.files[0].Old != nil || a.files[0].OldExists {
		t.Error("Preview sửa slice Files của Step")
	}
}

func TestPreviewReadErrorIsReturned(t *testing.T) {
	h := newHarness()
	h.sys.readErr["/etc/x"] = fs.ErrPermission
	a := h.step("a")
	a.files = []FileChange{{Path: "/etc/x", New: []byte("x")}}
	_, err := h.eng.Preview(context.Background(), plan(a))
	if !errors.Is(err, fs.ErrPermission) || !strings.Contains(err.Error(), "/etc/x") {
		t.Errorf("err = %v", err)
	}
}

func TestVerify(t *testing.T) {
	cases := []struct {
		st     checks.Status
		status JobStatus
		out    string
	}{
		{checks.Pass, JobSucceeded, "a=applied"},
		{checks.Warn, JobSucceeded, "a=applied"},
		{checks.Unknown, JobSucceeded, "a=applied"},
		{checks.Fail, JobRolledBack, "a=undone"},
	}
	for _, tc := range cases {
		t.Run(string(tc.st), func(t *testing.T) {
			h := newHarness()
			a := h.step("a")
			p := plan(a)
			p.Verify = []checks.Check{fakeCheck{id: "k1", st: checks.Pass}, fakeCheck{id: "k2", st: tc.st}}
			r := mustExecute(t, h, p)

			wantEq(t, "status", r.Job.Status, tc.status)
			wantEq(t, "outcomes", outcomes(r), tc.out)
			if len(r.Checks) != 2 || r.Checks[1].Status != tc.st {
				t.Errorf("Checks = %+v", r.Checks)
			}
			var n int
			for _, ev := range h.events {
				if ev.Kind == EventCheck {
					n++
					if ev.Check == nil {
						t.Error("sự kiện check thiếu Check")
					}
				}
			}
			wantEq(t, "số sự kiện check", n, 2)
			if tc.st == checks.Fail {
				wantEq(t, "Job.Error", r.Job.Error, "kiểm chứng k2 không đạt: kết quả k2")
			}
		})
	}
}

func TestEventsOrderAndSeq(t *testing.T) {
	h := newHarness()
	h.sys.output = []string{"dòng 1", "dòng 2"}
	a, b := h.step("a"), h.step("b")
	a.runCmd = true
	b.done = true
	p := plan(a, b)
	p.Verify = []checks.Check{fakeCheck{id: "k", st: checks.Pass}}
	r := mustExecute(t, h, p)

	wantEq(t, "kinds", kinds(h.events),
		"job.start step.start:a command:a output:a output:a step.done:a step.start:b step.skip:b check job.done")
	if !reflect.DeepEqual(h.events, h.j.events) {
		t.Error("OnEvent và Journal nhận sự kiện khác nhau")
	}
	for i, ev := range h.events {
		wantEq(t, "seq", ev.Seq, i+1)
		wantEq(t, "job id", ev.JobID, r.Job.ID)
		wantEq(t, "time", ev.Time, fixedNow)
	}
	wantEq(t, "command", h.events[2].Message, "echo a")
	wantEq(t, "output", h.events[3].Message, "dòng 1")
	wantEq(t, "job.done", h.events[len(h.events)-1].Message, string(JobSucceeded))

	if !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(r.Job.ID) {
		t.Errorf("Job.ID = %q", r.Job.ID)
	}
	if len(h.j.created) != 1 || h.j.created[0].Status != JobRunning || h.j.created[0].ID != r.Job.ID {
		t.Errorf("CreateJob = %+v", h.j.created)
	}
	wantEq(t, "kind", r.Job.Kind, "test.run")
	wantEq(t, "started", r.Job.Started, fixedNow)
	wantEq(t, "finished", r.Job.Finished, fixedNow)
}

func TestStepOwnOnOutputStillCalled(t *testing.T) {
	h := newHarness()
	h.sys.output = []string{"x"}
	var got []string
	s := &cmdStep{id: "a", onOutput: func(l string) { got = append(got, l) }}
	r := mustExecute(t, h, plan(s))
	wantEq(t, "status", r.Job.Status, JobSucceeded)
	wantEq(t, "OnOutput của Step", strings.Join(got, ","), "x")
	wantEq(t, "kinds", kinds(h.events), "job.start step.start:a command:a output:a step.done:a job.done")
}

// Lệnh in lại thứ đã đọc từ stdin (WP-CLI với --prompt): log của job phải che
// đi, còn OnOutput riêng của Step vẫn thấy nguyên văn.
func TestOutputMasksStdinLines(t *testing.T) {
	h := newHarness()
	h.sys.output = []string{"wp core install --admin_password=Secr3tPassw0rdXYZ --quiet"}
	var got []string
	s := &cmdStep{id: "a", stdin: []byte("Secr3tPassw0rdXYZ\n"), onOutput: func(l string) { got = append(got, l) }}
	mustExecute(t, h, plan(s))
	for _, ev := range h.events {
		if strings.Contains(ev.Message, "Secr3tPassw0rdXYZ") {
			t.Errorf("sự kiện %s lộ mật khẩu: %q", ev.Kind, ev.Message)
		}
		if ev.Kind == EventOutput {
			wantEq(t, "output đã che", ev.Message, "wp core install --admin_password=•••• --quiet")
		}
	}
	wantEq(t, "OnOutput của Step", strings.Join(got, ","), h.sys.output[0])
}

// cmdStep chạy một lệnh có OnOutput riêng.
type cmdStep struct {
	id       string
	onOutput func(string)
	stdin    []byte
	done     bool
}

func (s *cmdStep) Info() StepInfo                                     { return StepInfo{ID: s.id} }
func (s *cmdStep) Check(context.Context, system.System) (bool, error) { return s.done, nil }
func (s *cmdStep) Undo(context.Context, system.System) error          { return nil }
func (s *cmdStep) Apply(ctx context.Context, sys system.System) error {
	_, err := sys.Run(ctx, system.Cmd{Name: "true", Stdin: s.stdin, OnOutput: s.onOutput})
	s.done = err == nil
	return err
}

func TestConcurrentOutputKeepsSeq(t *testing.T) {
	h := newHarness()
	h.sys.concurrent = true
	for i := range 50 {
		h.sys.output = append(h.sys.output, fmt.Sprint(i))
	}
	a := h.step("a")
	a.runCmd = true
	mustExecute(t, h, plan(a))

	if !reflect.DeepEqual(h.events, h.j.events) {
		t.Fatal("OnEvent và Journal lệch thứ tự")
	}
	var n int
	for i, ev := range h.events {
		wantEq(t, "seq", ev.Seq, i+1)
		if ev.Kind == EventOutput {
			n++
		}
	}
	wantEq(t, "số dòng output", n, 100)
}

func TestRecordFileOnlyOnSuccess(t *testing.T) {
	sum := func(s string) string { x := sha256.Sum256([]byte(s)); return hex.EncodeToString(x[:]) }

	t.Run("thành công", func(t *testing.T) {
		h := newHarness()
		a, b, c := h.step("a"), h.step("b"), h.step("c")
		a.files = []FileChange{{Path: "/etc/a", New: []byte("AAA")}}
		b.done = true
		b.files = []FileChange{{Path: "/etc/b", New: []byte("BBB")}}
		c.files = []FileChange{{Path: "/etc/c", Delete: true}}
		r := mustExecute(t, h, plan(a, b, c))

		wantEq(t, "status", r.Job.Status, JobSucceeded)
		if !reflect.DeepEqual(h.j.files, map[string]string{"/etc/a": sum("AAA")}) {
			t.Errorf("RecordFile = %v (bước bỏ qua không được ghi)", h.j.files)
		}
		wantEq(t, "job của file", h.j.fileJob["/etc/a"], r.Job.ID)
		wantEq(t, "ForgetFile", strings.Join(h.j.forgot, ","), "/etc/c")
	})

	t.Run("lỗi", func(t *testing.T) {
		h := newHarness()
		a, b := h.step("a"), h.step("b")
		a.files = []FileChange{{Path: "/etc/a", New: []byte("AAA")}}
		b.files = []FileChange{{Path: "/etc/b", Delete: true}}
		b.applyErr = errors.New("hỏng")
		mustExecute(t, h, plan(a, b))
		if len(h.j.files)+len(h.j.forgot) != 0 {
			t.Errorf("job lỗi mà vẫn ghi file: %v %v", h.j.files, h.j.forgot)
		}
	})
}

func TestContextCancelledMidRun(t *testing.T) {
	h := newHarness()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, b := h.step("a"), h.step("b")
	a.onApply = func(context.Context) { cancel() }
	var undoCtxOK bool
	a.onUndo = func(uctx context.Context) {
		_, hasDeadline := uctx.Deadline()
		undoCtxOK = uctx.Err() == nil && hasDeadline
	}

	r, err := h.eng.Execute(ctx, plan(a, b))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	wantEq(t, "status", r.Job.Status, JobRolledBack)
	wantEq(t, "outcomes", outcomes(r), "a=undone b=failed")
	if !strings.Contains(r.Job.Error, "bước b") || !strings.Contains(r.Job.Error, context.Canceled.Error()) {
		t.Errorf("Job.Error = %q", r.Job.Error)
	}
	if !undoCtxOK {
		t.Error("Undo nhận ctx đã huỷ hoặc không có hạn giờ")
	}
	// Journal vẫn ghi được tới cuối (memJournal báo lỗi nếu nhận ctx đã huỷ).
	wantEq(t, "FinishJob", len(h.j.finished), 1)
	wantEq(t, "unlock", h.sys.unlocks, 1)
}

func TestContextCancelledDuringVerify(t *testing.T) {
	h := newHarness()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := h.step("a")
	p := plan(a)
	p.Verify = []checks.Check{fakeCheck{id: "k", st: checks.Unknown, fn: cancel}}
	r, err := h.eng.Execute(ctx, p)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	wantEq(t, "status", r.Job.Status, JobRolledBack)
	wantEq(t, "outcomes", outcomes(r), "a=undone")
}

func TestStepPanicIsRecoveredAndUndone(t *testing.T) {
	h := newHarness()
	a, b := h.step("a"), h.step("b")
	b.panicApply = true
	r := mustExecute(t, h, plan(a, b))

	wantEq(t, "status", r.Job.Status, JobRolledBack)
	wantEq(t, "outcomes", outcomes(r), "a=undone b=failed")
	wantEq(t, "Job.Error", r.Job.Error, "bước b: thực hiện: panic: hỏng giữa chừng")
	wantEq(t, "log", strings.Join(h.log, " "), "check:a apply:a check:a check:b apply:b undo:a")
	wantEq(t, "unlock", h.sys.unlocks, 1)
}

func TestUndoPanicContinuesUndo(t *testing.T) {
	h := newHarness()
	a, b, c := h.step("a"), h.step("b"), h.step("c")
	b.onUndo = func(context.Context) { panic("undo hỏng") }
	c.applyErr = errors.New("hỏng")
	r := mustExecute(t, h, plan(a, b, c))

	wantEq(t, "status", r.Job.Status, JobUndoFailed)
	wantEq(t, "outcomes", outcomes(r), "a=undone b=undo_failed c=failed")
	wantEq(t, "b.Error", r.Steps[1].Error, "hoàn tác bước b: panic: undo hỏng")
	wantEq(t, "unlock", h.sys.unlocks, 1)
}

// infoPanicStep panic ngay ở Info.
type infoPanicStep struct{ fakeStep }

func (s *infoPanicStep) Info() StepInfo { panic("info hỏng") }

func TestInfoPanicDoesNotCrash(t *testing.T) {
	h := newHarness()
	a := h.step("a")
	r := mustExecute(t, h, plan(a, &infoPanicStep{fakeStep{id: "x", log: &h.log}}))

	wantEq(t, "status", r.Job.Status, JobRolledBack)
	wantEq(t, "outcomes", outcomes(r), "a=not_run =not_run")
	wantEq(t, "Job.Error", r.Job.Error, "lấy mô tả bước thứ 2: panic: info hỏng")
	wantEq(t, "log", strings.Join(h.log, " "), "")
	wantEq(t, "FinishJob", len(h.j.finished), 1)
	wantEq(t, "unlock", h.sys.unlocks, 1)
}

func TestLock(t *testing.T) {
	t.Run("giữ và nhả đúng một lần", func(t *testing.T) {
		h := newHarness()
		mustExecute(t, h, plan(h.step("a")))
		wantEq(t, "locks", h.sys.locks, 1)
		wantEq(t, "unlocks", h.sys.unlocks, 1)
	})
	t.Run("không lấy được khoá", func(t *testing.T) {
		h := newHarness()
		h.sys.lockErr = errors.New("đang có job khác")
		_, err := h.eng.Execute(context.Background(), plan(h.step("a")))
		if err == nil || !strings.Contains(err.Error(), "đang có job khác") {
			t.Fatalf("err = %v", err)
		}
		if len(h.log)+len(h.j.created)+len(h.events) != 0 {
			t.Error("không có khoá mà vẫn chạy")
		}
	})
}

func TestCreateJobError(t *testing.T) {
	h := newHarness()
	h.j.createErr = errors.New("db khoá")
	_, err := h.eng.Execute(context.Background(), plan(h.step("a")))
	if err == nil || !strings.Contains(err.Error(), "db khoá") {
		t.Fatalf("err = %v", err)
	}
	if len(h.log)+len(h.events) != 0 {
		t.Error("CreateJob lỗi mà vẫn chạy bước")
	}
	wantEq(t, "unlock", h.sys.unlocks, 1)
}

func TestJournalErrorMidRunUndoes(t *testing.T) {
	h := newHarness()
	h.j.failAfter = 3 // job.start, step.start:a ghi được; step.done:a lỗi
	a, b := h.step("a"), h.step("b")
	r, err := h.eng.Execute(context.Background(), plan(a, b))
	if err == nil || !strings.Contains(err.Error(), "đĩa đầy") {
		t.Fatalf("err = %v", err)
	}
	wantEq(t, "status", r.Job.Status, JobRolledBack)
	wantEq(t, "outcomes", outcomes(r), "a=undone b=not_run")
	// OnEvent vẫn nhận đủ, để CLI còn in được tiến độ.
	wantEq(t, "kinds", kinds(h.events), "job.start step.start:a step.done:a undo.start:a undo.done:a job.done")
}

func TestNoJournalNoOnEvent(t *testing.T) {
	sys := newFakeSys()
	var log []string
	a := &fakeStep{id: "a", log: &log, files: []FileChange{{Path: "/etc/a", New: []byte("x")}}}
	r, err := New(sys, Options{}).Execute(context.Background(), plan(a))
	if err != nil {
		t.Fatal(err)
	}
	wantEq(t, "status", r.Job.Status, JobSucceeded)
	if r.Job.Started.IsZero() {
		t.Error("Now mặc định chưa được đặt")
	}
}
