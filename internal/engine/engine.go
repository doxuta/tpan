package engine

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/sitefs"
	"github.com/doxuta/tpan/internal/system"
)

// Mặc định của Options.VerifyRetry và VerifyEvery trên máy thật.
const (
	verifyRetryDefault = 6 * time.Second
	verifyEveryDefault = 500 * time.Millisecond
)

// undoTimeout là hạn giờ hoàn tác một bước. Hoàn tác vẫn phải chạy khi ctx của
// job đã bị huỷ (người dùng bấm dừng), nên nó dùng một ctx riêng; hạn giờ giữ
// cho một lệnh treo không giữ khoá toàn cục mãi.
const undoTimeout = 5 * time.Minute

// Các giá trị của StepResult.Outcome.
const (
	outcomeSkipped    = "skipped"
	outcomeApplied    = "applied"
	outcomeFailed     = "failed"
	outcomeUndone     = "undone"
	outcomeUndoFailed = "undo_failed"
	outcomeNotRun     = "not_run"
)

// Engine chạy Plan trên một System. Không giữ trạng thái giữa các lần chạy nên
// dùng chung được từ nhiều goroutine; khoá toàn cục nằm ở System.
type Engine struct {
	sys  system.System
	opts Options
}

// New dựng engine chạy trên sys.
func New(sys system.System, opts Options) *Engine {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Engine{sys: sys, opts: opts}
}

// Preview gọi Info và Check cho từng bước, không Apply, không khoá, không ghi
// Journal. Mỗi FileChange được điền nội dung hiện có trên máy để vẽ diff.
func (e *Engine) Preview(ctx context.Context, p Plan) (Preview, error) {
	pv := Preview{Kind: p.Kind, Target: p.Target, Title: p.Title, Steps: make([]StepPreview, 0, len(p.Steps))}
	for _, s := range p.Steps {
		if err := ctx.Err(); err != nil {
			return Preview{}, fmt.Errorf("xem trước %s: %w", p.Kind, err)
		}
		info := s.Info()
		// Chép slice trước khi ghi Old: Info có thể trả về slice của chính Step,
		// ghi vào đó sẽ làm bẩn Step cho lần Execute sau.
		info.Files = append([]FileChange(nil), info.Files...)
		for i := range info.Files {
			if err := e.fillOld(&info.Files[i]); err != nil {
				// Không đọc được (thiếu quyền, file lạ trong thư mục site...) thì
				// không vẽ được diff đúng; báo lỗi thay vì giả vờ file chưa có.
				return Preview{}, fmt.Errorf("xem trước bước %s: %w", info.ID, err)
			}
		}
		sp := StepPreview{Info: info}
		if done, err := s.Check(ctx, e.sys); err != nil {
			sp.CheckError = err.Error()
		} else {
			sp.Done = done
		}
		pv.Steps = append(pv.Steps, sp)
	}
	return pv, nil
}

// fillOld điền nội dung hiện có của f.Path để vẽ diff.
//
// Đường dẫn trong thư mục của một site (sitefs.InZone) do user của site kiểm
// soát: họ biến được file đó thành symlink tới bất kỳ đâu, kể cả wp-config.php
// của site khác hay /etc/shadow (lỗ T1 của rà soát v0.2.0, M28). Vì vậy ở đó
// engine hỏi trước bằng Stat (không theo symlink, đi qua cửa sitefs của
// System) và chỉ đọc khi là file thường; symlink thì chỉ ghi đích vào OldLink,
// không đọc đích: lúc chạy thật bước File thay symlink bằng file thường, nên
// bản xem trước nói đúng điều sẽ xảy ra. Thứ khác (thư mục, FIFO, socket) là
// lỗi. ReadFile của System kiểm lại trên fd đã mở, nên file bị tráo giữa Stat
// và ReadFile cũng chỉ ra lỗi, không ra nội dung.
func (e *Engine) fillOld(f *FileChange) error {
	if sitefs.InZone(f.Path) {
		fi, err := e.sys.Stat(f.Path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			f.Old, f.OldExists = nil, false
			return nil
		case err != nil:
			return fmt.Errorf("kiểm %s: %w", f.Path, err)
		case fi.Mode&fs.ModeSymlink != 0:
			target, err := e.sys.Readlink(f.Path)
			if err != nil {
				return fmt.Errorf("đọc symlink %s: %w", f.Path, err)
			}
			f.Old, f.OldLink, f.OldExists = nil, target, true
			return nil
		case !fi.Mode.IsRegular():
			return &sitefs.Error{Op: "xem trước", Path: f.Path, Err: sitefs.ErrNotRegular}
		}
	}
	old, err := e.sys.ReadFile(f.Path)
	switch {
	case err == nil:
		f.Old, f.OldExists = old, true
	case errors.Is(err, fs.ErrNotExist):
		f.Old, f.OldExists = nil, false
	default:
		return fmt.Errorf("đọc %s: %w", f.Path, err)
	}
	return nil
}

// Execute chạy Plan như mô tả ở types.go.
//
// Quy tắc outcome khi có lỗi: bước gây lỗi giữ "failed" (kể cả khi nó đã được
// hoàn tác vì Apply đã thành công nhưng Check lại vẫn sai), trừ khi hoàn tác nó
// lỗi thì thành "undo_failed". Các bước "applied" khác thành "undone" hoặc
// "undo_failed". Một Undo lỗi không làm dừng việc hoàn tác các bước trước nó.
func (e *Engine) Execute(ctx context.Context, p Plan) (Report, error) {
	unlock, err := e.sys.Lock(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("lấy khoá toàn cục: %w", err)
	}
	// defer để khoá được nhả cả khi có panic lọt ra ngoài.
	defer unlock()

	id, err := newJobID()
	if err != nil {
		return Report{}, err
	}
	r := &run{
		e: e,
		// Journal ghi bằng ctx không bị huỷ: job bị dừng giữa chừng vẫn phải
		// ghi được sự kiện hoàn tác và trạng thái cuối.
		jctx: context.WithoutCancel(ctx),
		job: Job{
			ID: id, Kind: p.Kind, Target: p.Target, Title: p.Title,
			Status: JobRunning, Started: e.opts.Now(), Actor: ActorFrom(ctx), Owner: OwnerFrom(ctx),
		},
	}
	j := e.opts.Journal
	if j != nil {
		if err := j.CreateJob(r.jctx, r.job); err != nil {
			return Report{}, fmt.Errorf("tạo job %s: %w", id, err)
		}
	}
	r.emit(EventJobStart, "", "")

	// Gọi Info một lần cho mỗi bước: dùng cho Report và cho RecordFile, để hai
	// chỗ thấy cùng một danh sách file.
	// Info panic thì chưa bước nào chạy: job kết thúc như một lần hoàn tác rỗng
	// thay vì làm sập tiến trình và để job kẹt ở "running".
	infos := make([]StepInfo, len(p.Steps))
	rep := Report{Steps: make([]StepResult, len(p.Steps))}
	var infoErr error
	for i, s := range p.Steps {
		if err := safe(func() error { infos[i] = s.Info(); return nil }); err != nil && infoErr == nil {
			infoErr = fmt.Errorf("lấy mô tả bước thứ %d: %w", i+1, err)
		}
		rep.Steps[i] = StepResult{ID: infos[i].ID, Outcome: outcomeNotRun}
	}

	var applied []int // chỉ số các bước đã Apply thành công, theo thứ tự chạy
	failErr := func() error {
		if infoErr != nil {
			return infoErr
		}
		for i, s := range p.Steps {
			sid := infos[i].ID
			r.emit(EventStepStart, sid, "")
			skipped, didApply, err := runStep(ctx, s, recSys{System: e.sys, r: r, stepID: sid})
			if didApply {
				applied = append(applied, i)
			}
			switch {
			case err != nil:
				err = fmt.Errorf("bước %s: %w", sid, err)
				rep.Steps[i].Outcome, rep.Steps[i].Error = outcomeFailed, err.Error()
				r.emit(EventStepFail, sid, err.Error())
				return err
			case skipped:
				rep.Steps[i].Outcome = outcomeSkipped
				r.emit(EventStepSkip, sid, "")
			default:
				rep.Steps[i].Outcome = outcomeApplied
				r.emit(EventStepDone, sid, "")
			}
			// Journal hỏng thì dừng: không để máy bị đổi mà không có nhật ký.
			if err := r.journalErr(); err != nil {
				return err
			}
		}
		return r.verify(ctx, p.Verify, &rep)
	}()

	status := JobSucceeded
	if failErr != nil {
		status = JobRolledBack
		for k := len(applied) - 1; k >= 0; k-- {
			i := applied[k]
			sid := infos[i].ID
			r.emit(EventUndoStart, sid, "")
			if err := undoStep(ctx, p.Steps[i], recSys{System: e.sys, r: r, stepID: sid}); err != nil {
				status = JobUndoFailed
				err = fmt.Errorf("hoàn tác bước %s: %w", sid, err)
				sr := &rep.Steps[i]
				if sr.Error != "" {
					sr.Error += "; "
				}
				sr.Outcome, sr.Error = outcomeUndoFailed, sr.Error+err.Error()
				r.emit(EventUndoFail, sid, err.Error())
				continue
			}
			if rep.Steps[i].Outcome == outcomeApplied {
				rep.Steps[i].Outcome = outcomeUndone
			}
			r.emit(EventUndoDone, sid, "")
		}
	} else if j != nil {
		// Chỉ ghi file của bước đã Apply: bước bỏ qua không do tpan ghi lần này.
		for _, i := range applied {
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
	if failErr != nil {
		r.job.Error = failErr.Error()
	}
	r.emit(EventJobDone, "", string(status))
	if j != nil {
		if err := j.FinishJob(r.jctx, r.job); err != nil {
			r.setJournalErr(fmt.Errorf("kết thúc job %s: %w", id, err))
		}
	}
	rep.Job = r.job
	if err := r.journalErr(); err != nil {
		return rep, fmt.Errorf("journal của job %s: %w", id, err)
	}
	return rep, nil
}

// runStep chạy một bước: Check, rồi Apply và Check lại nếu cần. applied cho
// biết Apply đã trả nil, kể cả khi Check sau đó vẫn sai: bước như vậy đã đổi
// máy nên phải được hoàn tác. Apply lỗi hoặc panic thì applied là false, vì
// Undo chỉ dành cho bước đã Apply thành công (types.go).
func runStep(ctx context.Context, s Step, sys system.System) (skipped, applied bool, err error) {
	if err := ctx.Err(); err != nil {
		return false, false, fmt.Errorf("job bị huỷ trước khi chạy: %w", err)
	}
	var done bool
	check := func() (err error) { done, err = s.Check(ctx, sys); return err }
	if err := safe(check); err != nil {
		return false, false, fmt.Errorf("kiểm trước khi chạy: %w", err)
	}
	if done {
		return true, false, nil
	}
	if err := safe(func() error { return s.Apply(ctx, sys) }); err != nil {
		return false, false, fmt.Errorf("thực hiện: %w", err)
	}
	if err := safe(check); err != nil {
		return false, true, fmt.Errorf("kiểm sau khi chạy: %w", err)
	}
	if !done {
		return false, true, errors.New("đã chạy xong nhưng kiểm lại vẫn chưa đạt")
	}
	return false, true, nil
}

// undoStep hoàn tác bằng ctx không bị huỷ nhưng có hạn giờ (xem undoTimeout).
func undoStep(ctx context.Context, s Step, sys system.System) error {
	uctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), undoTimeout)
	defer cancel()
	return safe(func() error { return s.Undo(uctx, sys) })
}

// safe biến panic trong mã của Step thành lỗi thường, để engine vẫn hoàn tác
// được các bước trước thay vì để tiến trình chết với máy dở dang.
func safe(fn func() error) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic: %v", p)
		}
	}()
	return fn()
}

// newJobID sinh 16 ký tự hex ngẫu nhiên.
func newJobID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("sinh id cho job: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// run giữ trạng thái của một lần Execute.
type run struct {
	e    *Engine
	jctx context.Context
	job  Job

	// mu bao seq, việc ghi Journal và gọi OnEvent: output có thể tới từ hai
	// goroutine (stdout và stderr), mà Seq phải liên tục và đúng thứ tự ở cả
	// hai nơi nhận.
	mu   sync.Mutex
	seq  int
	jerr error // lỗi Journal đầu tiên
}

func (r *run) emit(kind EventKind, stepID, msg string) {
	r.add(Event{Kind: kind, StepID: stepID, Message: msg})
}

func (r *run) add(ev Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	ev.JobID, ev.Seq, ev.Time = r.job.ID, r.seq, r.e.opts.Now()
	if j := r.e.opts.Journal; j != nil {
		if err := j.AddEvent(r.jctx, ev); err != nil && r.jerr == nil {
			r.jerr = fmt.Errorf("ghi sự kiện %d (%s): %w", ev.Seq, ev.Kind, err)
		}
	}
	// OnEvent vẫn được gọi khi Journal lỗi, để CLI còn in được tiến độ.
	if r.e.opts.OnEvent != nil {
		r.e.opts.OnEvent(ev)
	}
}

func (r *run) setJournalErr(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.jerr == nil {
		r.jerr = err
	}
}

func (r *run) journalErr() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.jerr
}

// verify chạy mọi phép kiểm chứng (kể cả sau một Fail, để có đủ bằng chứng) và
// trả lỗi nếu có Fail. Phép kiểm dùng sys gốc: lệnh của nó đã nằm trong
// Evidence, không cần thành sự kiện command riêng.
func (r *run) verify(ctx context.Context, list []checks.Check, rep *Report) error {
	var failed error
	budget, every := r.e.verifyWindow()
	for _, c := range list {
		res, used := r.verifyOne(ctx, c, budget, every)
		budget -= used
		rep.Checks = append(rep.Checks, res)
		r.add(Event{Kind: EventCheck, Check: &res})
		if res.Status == checks.Fail && failed == nil {
			failed = fmt.Errorf("kiểm chứng %s không đạt: %s", res.ID, res.Summary.Vi)
		}
	}
	if failed != nil {
		return failed
	}
	// Bị huỷ giữa lúc kiểm thì phép kiểm thường trả Unknown, không làm job lỗi;
	// nhưng người dùng đã muốn dừng nên vẫn hoàn tác.
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("job bị huỷ lúc kiểm chứng: %w", err)
	}
	return r.journalErr()
}

// verifyWindow trả thời gian thử lại của một lượt kiểm chứng (dùng chung cho
// mọi phép kiểm của lượt) và nhịp thử (Options).
func (e *Engine) verifyWindow() (window, every time.Duration) {
	window, every = e.opts.VerifyRetry, e.opts.VerifyEvery
	if window == 0 && !testing.Testing() {
		window = verifyRetryDefault
	}
	if every <= 0 {
		every = verifyEveryDefault
	}
	return max(window, 0), every
}

// verifyOne chạy một phép kiểm chứng. Trượt (Fail) thì chạy lại, cách nhau
// every, cho tới khi hết Fail hoặc dùng hết budget; chỉ kết quả cuối được ghi
// và phát sự kiện. Trả thêm thời gian đã dùng để thử lại (0 khi đạt ngay).
//
// Vì sao thử lại: phép kiểm chứng là thứ quyết định hoàn tác cả job, mà một lần
// trượt có thể chỉ là máy đang chuyển trạng thái. Đo trên máy ảo 26/11/2026:
// web server bị tiến trình khác khởi động lại (unattended-upgrades và
// needrestart) thì dừng vài chục mili giây; phép gọi thử site rơi đúng lúc đó
// nhận "không kết nối được" và job đã chạy đúng bị hoàn tác oan (2 trên 6 job
// khi nginx bị khởi động lại mỗi 0,9 giây). Cùng lý do với dbRetry của core
// (site trả 500 trong lúc opcache còn giữ file cũ), nay áp cho mọi phép kiểm
// chứng.
//
// budget là của cả lượt kiểm chứng, không phải của riêng một phép kiểm (verify
// trừ dần): job hỏng thật thường trượt nhiều phép kiểm cùng lúc (web server
// chết thì mọi phép gọi thử đều trượt), mỗi phép chờ riêng thì hoàn tác chậm
// thêm số phép kiểm nhân với thời gian chờ. Dùng chung thì phép trượt thật đầu
// tiên dùng hết budget, các phép sau chạy một lần; job hỏng thật chỉ hoàn tác
// chậm hơn đúng một lần budget.
//
// Phép kiểm chỉ đạt ở lần thử lại thì câu kết quả của nó nói thêm lần đầu trượt
// vì gì và đạt sau bao lâu: người đọc job phải thấy được.
func (r *run) verifyOne(ctx context.Context, c checks.Check, budget, every time.Duration) (checks.Result, time.Duration) {
	res := runCheck(ctx, c, r.e.sys)
	if res.Status != checks.Fail || budget <= 0 {
		return res, 0
	}
	first, tries, start := res, 1, time.Now()
	for res.Status == checks.Fail && time.Since(start) < budget {
		select {
		case <-ctx.Done():
			return res, time.Since(start)
		case <-time.After(every):
		}
		res = runCheck(ctx, c, r.e.sys)
		tries++
	}
	used := time.Since(start)
	if res.Status != checks.Fail {
		after := used.Round(100 * time.Millisecond)
		res.Summary = i18n.T(
			fmt.Sprintf("%s (lần đầu chưa đạt: %s; đạt ở lần thử %d, sau %s)", res.Summary.Vi, first.Summary.Vi, tries, after),
			fmt.Sprintf("%s (the first try failed: %s; passed on try %d, after %s)", res.Summary.En, first.Summary.En, tries, after))
	}
	return res, used
}

// runCheck coi một phép kiểm panic là Unknown, như mọi trường hợp "không kiểm được".
func runCheck(ctx context.Context, c checks.Check, sys system.System) (res checks.Result) {
	defer func() {
		if p := recover(); p != nil {
			res = checks.Result{Status: checks.Unknown}
			res.Summary.Vi = fmt.Sprintf("phép kiểm bị panic: %v", p)
			res.Summary.En = fmt.Sprintf("check panicked: %v", p)
		}
	}()
	return c.Run(ctx, sys)
}

// recSys bọc System trong lúc Execute: mỗi lệnh Run thành sự kiện command, mỗi
// dòng output thành sự kiện output gắn với bước đang chạy. Các hàm khác đi
// thẳng tới System gốc.
type recSys struct {
	system.System
	r      *run
	stepID string
}

func (s recSys) Run(ctx context.Context, cmd system.Cmd) (system.Result, error) {
	// Ghi lệnh thật sự chạy (M17: lệnh của site trong lồng là systemd-run).
	// Describe, như Cmd.String(), không in Stdin, nên mật khẩu đưa qua stdin
	// không lọt vào log.
	s.r.emit(EventCommand, s.stepID, system.Describe(s.System, cmd))
	// Step đã đặt OnOutput riêng thì vẫn gọi nó, sau khi ghi sự kiện.
	own := cmd.OnOutput
	secrets := stdinLines(cmd.Stdin)
	cmd.OnOutput = func(line string) {
		s.r.emit(EventOutput, s.stepID, maskLines(line, secrets))
		if own != nil {
			own(line)
		}
	}
	return s.System.Run(ctx, cmd)
}

// Describe chuyển tiếp tới System gốc (M17): kiểu nhúng interface chỉ mang các
// hàm của system.System, nên thiếu dòng này thì Step dựng dòng "sửa tay" bằng
// system.Describe trong lúc Execute nhận dạng `runuser` cho site trong lồng.
func (s recSys) Describe(cmd system.Cmd) string { return system.Describe(s.System, cmd) }

// stdinLines trả các dòng khác rỗng của stdin. Stdin là chỗ đưa mật khẩu vào
// lệnh; vài chương trình in lại thứ đã đọc (WP-CLI với --prompt in lại cả câu
// lệnh kèm giá trị vừa hỏi), nên output phải được che trước khi vào log job.
//
// Ngoài cả dòng, mỗi chuỗi chữ-số dài từ 20 ký tự trong stdin cũng được che
// riêng: mật khẩu tpan sinh là 32 ký tự [A-Za-z0-9], và khi stdin là cả câu SQL
// (CREATE USER ... IDENTIFIED BY '...') thì lỗi của MariaDB chỉ chép lại một
// đoạn câu (near '...'), không bao giờ khớp cả dòng.
func stdinLines(stdin []byte) []string {
	var out []string
	for _, ln := range strings.Split(string(stdin), "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			out = append(out, ln)
			out = append(out, longToken.FindAllString(ln, -1)...)
		}
	}
	return out
}

// longToken là một chuỗi chữ-số đủ dài để trông như mật khẩu. Tên DB, tên
// user của tpan có dấu "_" nên bị cắt thành đoạn ngắn, không bị che nhầm.
var longToken = regexp.MustCompile(`[A-Za-z0-9]{20,}`)

// maskLines thay mọi dòng stdin xuất hiện trong line bằng "••••".
func maskLines(line string, secrets []string) string {
	for _, sec := range secrets {
		line = strings.ReplaceAll(line, sec, "••••")
	}
	return line
}
