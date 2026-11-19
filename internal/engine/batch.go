package engine

// Job hàng loạt (mục 7 của docs/KE-HOACH.md): một job, một khoá, nhiều đối
// tượng (site). Hợp đồng M6, đợt 0: kiểu và chữ ký; thân viết ở đợt 1.
//
// Vì sao không dùng Execute: Execute chạy một danh sách bước phẳng và một bước
// lỗi thì hoàn tác cả job. Job hàng loạt cần hai thứ khác:
//
//   - Site nào lỗi thì hoàn tác riêng site đó, các site khác đi tiếp.
//   - Kiểm config và reload web server, PHP-FPM đúng MỘT lần cho cả lô (pha B),
//     thay vì mỗi file một lần như Plan tạo một site.
//
// File này mới hoàn toàn và chỉ dùng lại runStep, undoStep, run, recSys của
// engine.go (không sửa engine.go: M8 và M12 chạy cùng lượt).
//
// Thứ tự chạy của ExecuteBatch:
//
//  1. Lấy khoá toàn cục (sys.Lock), tạo job (Kind, Target, Title của Batch),
//     phát job.start. Giống Execute.
//  2. Pre: chạy lần lượt, như Execute. Bước lỗi thì hoàn tác các bước Pre đã
//     Apply theo thứ tự ngược và job kết thúc rolled_back (chưa unit nào chạy).
//  3. Pha A: Prepare của mọi Unit, tối đa Parallel unit cùng lúc; trong một
//     unit các bước chạy lần lượt. Bước của một unit lỗi thì hoàn tác các bước
//     đã Apply của riêng unit đó (ngược), unit thành "rolled_back" (hoặc
//     "undo_failed"), các unit khác đi tiếp.
//  4. Không còn unit nào sống sau pha A: hoàn tác Pre, job rolled_back.
//  5. Pha B: Commit chạy lần lượt (thường là: kiểm config rồi reload, mỗi dịch
//     vụ một lần). Bước Commit lỗi thì không biết lỗi do site nào: hoàn tác
//     MỌI unit còn sống (ngược), rồi Pre, job rolled_back.
//  6. Pha C: Finish của mọi unit còn sống, tối đa Parallel cùng lúc, rồi Verify
//     của chính unit đó (checks.RunAll trên sys gốc, phát sự kiện check như
//     Execute). Bước lỗi hoặc có phép kiểm Fail: hoàn tác cả Finish lẫn
//     Prepare của unit đó (ngược), unit thành rolled_back.
//  7. Có unit bị hoàn tác ở pha C (file của nó đã được web server nạp ở pha B):
//     chạy lại Commit một lần nữa (Check → Apply như thường) để web server và
//     PHP-FPM thôi phục vụ cấu hình đã gỡ. Lỗi ở lần này ghi vào Job.Error,
//     job thành undo_failed.
//  8. Job: succeeded nếu ít nhất một unit succeeded và không có undo_failed;
//     rolled_back nếu mọi unit đều không thành; undo_failed nếu có một lần
//     hoàn tác lỗi ở bất kỳ đâu. Job.Error tóm tắt các unit lỗi, KỂ CẢ khi job
//     succeeded (khác Execute): người đọc xem Job.Status, không xem Error rỗng.
//  9. Ghi sha256 của file đã Apply (RecordFile, ForgetFile) chỉ cho Pre, Commit
//     và các unit succeeded. Phát job.done, FinishJob. Giống Execute.
//
// Sự kiện: mọi bước phát step.start/skip/done/fail và undo.* với StepID của
// chính nó như Execute. StepID của các unit phải khác nhau (ID của Step site
// đã kèm domain); Batch chạy hai bước cùng ID trong hai unit là lỗi lập trình,
// ExecuteBatch từ chối trước khi lấy khoá. Hai unit chạy song song nên sự kiện
// của chúng xen kẽ; Seq vẫn liên tục vì run.add có khoá.
//
// Song song chỉ an toàn khi các Step của các unit không giẫm lên nhau. Engine
// không biết điều đó; người dựng Batch (core) tự bọc các bước phải chạy một
// mình (useradd khoá /etc/passwd; ghi vhost rồi `nginx -t` kiểm cả config) bằng
// một Step có mutex. Engine chỉ hứa: Pre và Commit không bao giờ chạy cùng lúc
// với bước nào khác.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/i18n"
)

// EventUnitDone là sự kiện ExecuteBatch phát khi một unit có kết quả cuối
// (sau pha C, hoặc khi unit bị hoàn tác ở pha nào đó, hoặc not_run khi job dừng
// sớm). StepID là Unit.Target; Message là JSON của UnitResult với Checks bỏ
// trống (kết quả phép kiểm đã có sự kiện check riêng). Nhờ sự kiện này bảng
// kết quả từng site nằm trong job_events, trang job đọc lại được về sau.
const EventUnitDone EventKind = "unit.done"

// Batch là một job hàng loạt.
type Batch struct {
	Kind   string    `json:"kind"`
	Target string    `json:"target"`
	Title  i18n.Text `json:"title"`
	// Pre chạy một lần trước pha A (ví dụ tải WordPress vào cache).
	Pre []Step `json:"-"`
	// Units: mỗi đối tượng (site) một Unit, chạy theo đúng thứ tự trong slice
	// khi Parallel = 1.
	Units []Unit `json:"-"`
	// Commit là pha B: chạy một lần cho cả lô sau pha A.
	Commit []Step `json:"-"`
	// Parallel là số unit tối đa chạy cùng lúc ở pha A và C. ≤ 0 thì là 1.
	// Mục 7: tối đa 3.
	Parallel int `json:"-"`
}

// Unit là phần việc của một đối tượng trong Batch.
type Unit struct {
	// Target là đối tượng, ví dụ domain. Duy nhất trong Batch.
	Target string `json:"target"`
	// Prepare là pha A, Finish là pha C của unit này.
	Prepare []Step `json:"-"`
	Finish  []Step `json:"-"`
	// Verify chạy sau Finish; có Fail thì unit bị hoàn tác.
	Verify []checks.Check `json:"-"`
}

// UnitStatus là kết quả của một unit.
type UnitStatus string

const (
	UnitSucceeded  UnitStatus = "succeeded"
	UnitRolledBack UnitStatus = "rolled_back" // lỗi, đã hoàn tác sạch phần của unit
	UnitUndoFailed UnitStatus = "undo_failed" // lỗi, hoàn tác cũng lỗi
	UnitNotRun     UnitStatus = "not_run"     // job dừng trước khi tới unit (Pre lỗi); Commit lỗi thì unit là rolled_back, Phase "commit"
)

// UnitResult là một dòng trong bảng kết quả của job hàng loạt (mục 7: "Kết
// quả là bảng từng site: xong, lỗi ở bước nào, vì sao").
type UnitResult struct {
	Target string     `json:"target"`
	Status UnitStatus `json:"status"`
	// Phase là pha có lỗi đầu tiên: "prepare", "commit", "finish", "verify";
	// rỗng khi succeeded.
	Phase string `json:"phase,omitempty"`
	// StepID là bước lỗi đầu tiên của unit (rỗng khi lỗi ở Verify hay Commit).
	StepID string `json:"step_id,omitempty"`
	Error  string `json:"error,omitempty"`
	// Checks là kết quả Verify của unit (cũng nằm trong BatchReport.Checks).
	Checks []checks.Result `json:"checks,omitempty"`
}

// BatchReport là kết quả của ExecuteBatch. Report.Steps có mọi bước theo thứ
// tự Pre, Prepare của từng unit, Commit, Finish của từng unit (thứ tự trong
// Batch, không phải thứ tự chạy thật), nên trang job của M3 đọc được như cũ.
type BatchReport struct {
	Report
	Units []UnitResult `json:"units"`
}

// PreviewBatch là bản xem trước của Batch dưới dạng một Preview phẳng theo
// đúng thứ tự của BatchReport.Steps (Pre, Prepare từng unit, Commit, Finish
// từng unit). Chỉ đọc máy, không khoá, không Journal; giống Preview.
func (e *Engine) PreviewBatch(ctx context.Context, b Batch) (Preview, error) {
	// Preview đã làm đúng việc cần (Info, đọc file cũ để vẽ diff, Check) trên
	// một danh sách phẳng; chỉ cần trải Batch ra theo thứ tự của Report.Steps.
	return e.Preview(ctx, Plan{Kind: b.Kind, Target: b.Target, Title: b.Title, Steps: layoutBatch(b).steps})
}

// ExecuteBatch chạy Batch như mô tả ở đầu file. error chỉ khác nil khi không
// chạy được (khoá, Journal, Batch sai như trùng StepID hay trùng Target); unit
// lỗi thì error là nil, xem BatchReport.Units và Job.Status.
func (e *Engine) ExecuteBatch(ctx context.Context, b Batch) (BatchReport, error) {
	l := layoutBatch(b)
	// Kiểm trước khi lấy khoá: Batch sai là lỗi lập trình, không đáng giữ khoá
	// toàn cục hay để lại một job rỗng trong lịch sử.
	infos, err := checkBatch(b, l.steps)
	if err != nil {
		return BatchReport{}, fmt.Errorf("job hàng loạt %s: %w", b.Kind, err)
	}

	unlock, err := e.sys.Lock(ctx)
	if err != nil {
		return BatchReport{}, fmt.Errorf("lấy khoá toàn cục: %w", err)
	}
	defer unlock()

	id, err := newJobID()
	if err != nil {
		return BatchReport{}, err
	}
	r := &run{
		e: e,
		// Như Execute: job bị dừng giữa chừng vẫn phải ghi được hoàn tác.
		jctx: context.WithoutCancel(ctx),
		job: Job{
			ID: id, Kind: b.Kind, Target: b.Target, Title: b.Title,
			Status: JobRunning, Started: e.opts.Now(),
		},
	}
	j := e.opts.Journal
	if j != nil {
		if err := j.CreateJob(r.jctx, r.job); err != nil {
			return BatchReport{}, fmt.Errorf("tạo job %s: %w", id, err)
		}
	}
	r.emit(EventJobStart, "", "")

	br := &batchRun{run: r, ctx: ctx, b: b, l: l, infos: infos, applied: make([][]int, len(b.Units))}
	br.rep.Steps = make([]StepResult, len(l.steps))
	for i := range br.rep.Steps {
		br.rep.Steps[i] = StepResult{ID: infos[i].ID, Outcome: outcomeNotRun}
	}
	br.rep.Units = make([]UnitResult, len(b.Units))
	for k, u := range b.Units {
		br.rep.Units[k] = UnitResult{Target: u.Target, Status: UnitNotRun}
	}
	stopErr := br.exec()

	// Kết quả của job. Checks gom theo thứ tự unit (không theo thứ tự chạy) để
	// báo cáo giống nhau giữa các lần chạy.
	var failed []string
	succeeded := 0
	for _, ur := range br.rep.Units {
		br.rep.Checks = append(br.rep.Checks, ur.Checks...)
		switch ur.Status {
		case UnitSucceeded:
			succeeded++
		case UnitRolledBack, UnitUndoFailed:
			failed = append(failed, fmt.Sprintf("%s (%s): %s", ur.Target, ur.Phase, ur.Error))
		}
	}
	status := JobRolledBack
	if succeeded > 0 {
		status = JobSucceeded
	}
	undoFailed := br.recommitErr != nil
	for _, sr := range br.rep.Steps {
		undoFailed = undoFailed || sr.Outcome == outcomeUndoFailed
	}
	if undoFailed {
		status = JobUndoFailed
	}
	var msgs []string
	switch {
	case stopErr != nil:
		// Pre hay Commit lỗi: mọi unit chung một lỗi, nói một lần là đủ.
		msgs = append(msgs, stopErr.Error())
	case len(failed) > 0:
		msgs = append(msgs, fmt.Sprintf("%d/%d đối tượng lỗi: %s", len(failed), len(b.Units), strings.Join(failed, "; ")))
	}
	if br.recommitErr != nil {
		msgs = append(msgs, br.recommitErr.Error())
	}

	// Ghi sha256 chỉ khi còn ít nhất một unit thành: khi không còn unit nào
	// thì Pre cũng đã được hoàn tác, không file nào của job còn trên máy.
	if succeeded > 0 && j != nil {
		keep := make([]bool, len(l.steps))
		mark := func(idx []int) {
			for _, i := range idx {
				keep[i] = true
			}
		}
		mark(br.preApplied)
		mark(br.commitApplied)
		for k, ur := range br.rep.Units {
			if ur.Status == UnitSucceeded {
				mark(br.applied[k])
			}
		}
		for i, ok := range keep {
			if !ok {
				continue
			}
			for _, f := range infos[i].Files {
				var err error
				if f.Delete {
					err = j.ForgetFile(r.jctx, f.Path)
				} else {
					sum := sha256.Sum256(f.New)
					err = j.RecordFile(r.jctx, f.Path, hex.EncodeToString(sum[:]), id)
				}
				if err != nil {
					r.setJournalErr(fmt.Errorf("ghi sha256 của %s: %w", f.Path, err))
				}
			}
		}
	}

	r.job.Status, r.job.Finished = status, e.opts.Now()
	r.job.Error = strings.Join(msgs, "; ")
	r.emit(EventJobDone, "", string(status))
	if j != nil {
		if err := j.FinishJob(r.jctx, r.job); err != nil {
			r.setJournalErr(fmt.Errorf("kết thúc job %s: %w", id, err))
		}
	}
	br.rep.Job = r.job
	if err := r.journalErr(); err != nil {
		return br.rep, fmt.Errorf("journal của job %s: %w", id, err)
	}
	return br.rep, nil
}

// span là khoảng [lo, hi) trong danh sách bước phẳng của một Batch.
type span struct{ lo, hi int }

// batchLayout trải mọi bước của Batch thành một danh sách phẳng theo thứ tự
// của BatchReport.Steps. Nhờ vậy mỗi bước có đúng một ô trong Report.Steps và
// trong infos, và goroutine của unit nào chỉ ghi vào ô của unit đó (không cần
// khoá cho Report).
type batchLayout struct {
	steps     []Step
	pre       span
	commit    span
	prep, fin []span // theo chỉ số unit
}

func layoutBatch(b Batch) batchLayout {
	var l batchLayout
	add := func(ss []Step) span {
		lo := len(l.steps)
		l.steps = append(l.steps, ss...)
		return span{lo, len(l.steps)}
	}
	l.pre = add(b.Pre)
	for _, u := range b.Units {
		l.prep = append(l.prep, add(u.Prepare))
	}
	l.commit = add(b.Commit)
	for _, u := range b.Units {
		l.fin = append(l.fin, add(u.Finish))
	}
	return l
}

// checkBatch kiểm Batch trước khi chạy và trả Info của mọi bước theo thứ tự
// phẳng. Trùng StepID thì sự kiện và Report.Steps không còn chỉ ra được bước
// nào của site nào; trùng Target thì bảng kết quả có hai dòng cùng tên. Cả
// hai là lỗi lập trình của người dựng Batch nên bị từ chối hẳn. Batch không
// có unit nào cũng bị từ chối: core không tạo job khi không có gì để làm.
func checkBatch(b Batch, steps []Step) ([]StepInfo, error) {
	if len(b.Units) == 0 {
		return nil, errors.New("không có đối tượng nào")
	}
	targets := make(map[string]bool, len(b.Units))
	for i, u := range b.Units {
		switch {
		case u.Target == "":
			return nil, fmt.Errorf("đối tượng thứ %d không có tên", i+1)
		case targets[u.Target]:
			return nil, fmt.Errorf("đối tượng %q xuất hiện hai lần", u.Target)
		}
		targets[u.Target] = true
	}
	infos := make([]StepInfo, len(steps))
	ids := make(map[string]bool, len(steps))
	for i, s := range steps {
		if err := safe(func() error { infos[i] = s.Info(); return nil }); err != nil {
			return nil, fmt.Errorf("lấy mô tả bước thứ %d: %w", i+1, err)
		}
		id := infos[i].ID
		if ids[id] {
			return nil, fmt.Errorf("bước %q xuất hiện hai lần", id)
		}
		ids[id] = true
	}
	return infos, nil
}

// batchRun giữ trạng thái của một lần ExecuteBatch, chồng lên run của
// Execute (Seq, Journal, OnEvent dùng chung).
type batchRun struct {
	*run
	ctx   context.Context
	b     Batch
	l     batchLayout
	infos []StepInfo
	rep   BatchReport
	// applied[k] là các bước đã Apply của unit k (Prepare rồi Finish) theo
	// thứ tự chạy; chỉ goroutine của unit k ghi vào.
	applied       [][]int
	preApplied    []int
	commitApplied []int // cả lần chạy lại ở bước 7
	recommitErr   error
}

// exec chạy Pre và ba pha. Trả lỗi làm cả lô dừng (Pre hoặc Commit lỗi); nil
// khi lô chạy hết, kể cả khi có unit lỗi (xem rep.Units).
func (br *batchRun) exec() error {
	if _, err := br.runSteps(br.ctx, br.l.pre, &br.preApplied); err != nil {
		_ = br.undo(br.preApplied) // lỗi hoàn tác đã nằm ở Report.Steps
		for k := range br.b.Units {
			br.unitDone(k) // not_run
		}
		return fmt.Errorf("phần chung trước lô: %w", err)
	}

	// Pha A.
	all := make([]int, len(br.b.Units))
	for k := range all {
		all[k] = k
	}
	br.parallel(all, func(k int) {
		if sid, err := br.runSteps(br.ctx, br.l.prep[k], &br.applied[k]); err != nil {
			br.unitFail(k, "prepare", sid, err)
		}
	})
	live := br.alive()
	if len(live) == 0 {
		_ = br.undo(br.preApplied)
		return nil
	}

	// Pha B. Lỗi ở đây không chỉ ra được site nào gây ra (nginx -t kiểm cả
	// config), nên mọi unit còn sống bị hoàn tác. Hoàn tác đúng thứ tự ngược
	// của lúc chạy: Commit, các unit (ngược), rồi Pre.
	if _, err := br.runSteps(br.ctx, br.l.commit, &br.commitApplied); err != nil {
		err = fmt.Errorf("phần chung của lô: %w", err)
		_ = br.undo(br.commitApplied)
		for i := len(live) - 1; i >= 0; i-- {
			br.unitFail(live[i], "commit", "", err)
		}
		_ = br.undo(br.preApplied)
		return err
	}

	// Pha C.
	br.parallel(live, func(k int) {
		sid, err := br.runSteps(br.ctx, br.l.fin[k], &br.applied[k])
		phase := "finish"
		if err == nil {
			// verify của Execute: phát sự kiện check, coi Fail, huỷ, Journal
			// hỏng là lỗi. Report tạm riêng cho mỗi unit để không ghi chung.
			var tmp Report
			phase, err = "verify", br.verify(br.ctx, br.b.Units[k].Verify, &tmp)
			br.rep.Units[k].Checks = tmp.Checks
		}
		if err != nil {
			br.unitFail(k, phase, sid, err)
			return
		}
		br.rep.Units[k].Status = UnitSucceeded
		br.unitDone(k)
	})

	succeeded, undoneInC := 0, false
	for _, k := range live {
		if br.rep.Units[k].Status == UnitSucceeded {
			succeeded++
		} else {
			undoneInC = true
		}
	}
	if undoneInC {
		// File của unit vừa gỡ đã được web server và PHP-FPM nạp ở pha B: chạy
		// lại Commit để chúng thôi phục vụ cấu hình đó. Đây là một phần của
		// việc dọn dẹp nên chạy cả khi job bị huỷ, như undoStep.
		uctx, cancel := context.WithTimeout(context.WithoutCancel(br.ctx), undoTimeout)
		var again []int
		if _, err := br.runSteps(uctx, br.l.commit, &again); err != nil {
			br.recommitErr = fmt.Errorf("chạy lại phần chung sau khi hoàn tác: %w", err)
		}
		cancel()
		br.commitApplied = append(br.commitApplied, again...)
	}
	if succeeded == 0 {
		// Không còn site nào dùng Pre: trả nó lại để rolled_back nghĩa là sạch.
		_ = br.undo(br.preApplied)
	}
	return nil
}

// alive trả các unit chưa có kết quả (chưa lỗi, chưa xong).
func (br *batchRun) alive() []int {
	var out []int
	for k, ur := range br.rep.Units {
		if ur.Status == UnitNotRun {
			out = append(out, k)
		}
	}
	return out
}

// parallel chạy fn cho từng unit, tối đa Parallel cùng lúc, rồi chờ hết.
func (br *batchRun) parallel(units []int, fn func(k int)) {
	sem := make(chan struct{}, max(br.b.Parallel, 1))
	var wg sync.WaitGroup
	for _, k := range units {
		// Giữ chỗ trước khi tạo goroutine: Parallel = 1 thì các unit chạy đúng
		// thứ tự trong slice.
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			fn(k)
		})
	}
	wg.Wait()
}

// runSteps chạy lần lượt các bước trong sp như vòng lặp của Execute, thêm
// chỉ số bước đã Apply vào applied. Trả StepID và lỗi của bước lỗi đầu tiên
// (StepID rỗng khi lỗi là Journal hỏng).
//
// Commit có thể chạy hai lần (bước 7): lần sau bỏ qua thì giữ "applied" của
// lần trước, vì thay đổi đó vẫn còn trên máy.
func (br *batchRun) runSteps(ctx context.Context, sp span, applied *[]int) (string, error) {
	for i := sp.lo; i < sp.hi; i++ {
		sid := br.infos[i].ID
		br.emit(EventStepStart, sid, "")
		skipped, didApply, err := runStep(ctx, br.l.steps[i], recSys{System: br.e.sys, r: br.run, stepID: sid})
		if didApply {
			*applied = append(*applied, i)
		}
		sr := &br.rep.Steps[i]
		switch {
		case err != nil:
			err = fmt.Errorf("bước %s: %w", sid, err)
			sr.Outcome, sr.Error = outcomeFailed, err.Error()
			br.emit(EventStepFail, sid, err.Error())
			return sid, err
		case skipped:
			if sr.Outcome != outcomeApplied {
				sr.Outcome = outcomeSkipped
			}
			br.emit(EventStepSkip, sid, "")
		default:
			sr.Outcome = outcomeApplied
			br.emit(EventStepDone, sid, "")
		}
		// Journal hỏng thì dừng, như Execute.
		if err := br.journalErr(); err != nil {
			return "", err
		}
	}
	return "", nil
}

// undo hoàn tác các bước trong applied theo thứ tự ngược, với cùng quy tắc
// outcome như Execute: bước gây lỗi giữ "failed", bước "applied" thành
// "undone"; hoàn tác lỗi thì "undo_failed" và vẫn hoàn tác tiếp các bước
// trước. Trả lỗi hoàn tác đầu tiên.
func (br *batchRun) undo(applied []int) error {
	var first error
	for k := len(applied) - 1; k >= 0; k-- {
		i := applied[k]
		sid := br.infos[i].ID
		br.emit(EventUndoStart, sid, "")
		if err := undoStep(br.ctx, br.l.steps[i], recSys{System: br.e.sys, r: br.run, stepID: sid}); err != nil {
			err = fmt.Errorf("hoàn tác bước %s: %w", sid, err)
			sr := &br.rep.Steps[i]
			if sr.Error != "" {
				sr.Error += "; "
			}
			sr.Outcome, sr.Error = outcomeUndoFailed, sr.Error+err.Error()
			br.emit(EventUndoFail, sid, err.Error())
			if first == nil {
				first = err
			}
			continue
		}
		if br.rep.Steps[i].Outcome == outcomeApplied {
			br.rep.Steps[i].Outcome = outcomeUndone
		}
		br.emit(EventUndoDone, sid, "")
	}
	return first
}

// unitFail ghi lỗi của unit k, hoàn tác mọi bước nó đã Apply (Finish rồi
// Prepare, ngược), rồi phát unit.done.
func (br *batchRun) unitFail(k int, phase, stepID string, err error) {
	ur := &br.rep.Units[k]
	ur.Status, ur.Phase, ur.StepID, ur.Error = UnitRolledBack, phase, stepID, err.Error()
	if uerr := br.undo(br.applied[k]); uerr != nil {
		ur.Status = UnitUndoFailed
		ur.Error += "; " + uerr.Error()
	}
	br.unitDone(k)
}

// unitDone phát unit.done cho unit k với kết quả hiện tại, bỏ Checks (đã có
// sự kiện check riêng).
func (br *batchRun) unitDone(k int) {
	ur := br.rep.Units[k]
	ur.Checks = nil
	// UnitResult chỉ có chuỗi: Marshal không lỗi được.
	msg, _ := json.Marshal(ur)
	br.emit(EventUnitDone, ur.Target, string(msg))
}
