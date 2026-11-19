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
//     hoàn tác lỗi ở bất kỳ đâu. Job.Error tóm tắt các unit lỗi.
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
	"errors"

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
	UnitNotRun     UnitStatus = "not_run"     // job dừng trước khi tới unit (Pre hoặc Commit lỗi)
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

// errBatchTODO: thân hàm viết ở đợt 1 của M6.
var errBatchTODO = errors.New("engine: job hàng loạt chưa viết (M6 đợt 1)")

// PreviewBatch là bản xem trước của Batch dưới dạng một Preview phẳng theo
// đúng thứ tự của BatchReport.Steps (Pre, Prepare từng unit, Commit, Finish
// từng unit). Chỉ đọc máy, không khoá, không Journal; giống Preview.
func (e *Engine) PreviewBatch(ctx context.Context, b Batch) (Preview, error) {
	return Preview{}, errBatchTODO
}

// ExecuteBatch chạy Batch như mô tả ở đầu file. error chỉ khác nil khi không
// chạy được (khoá, Journal, Batch sai như trùng StepID hay trùng Target); unit
// lỗi thì error là nil, xem BatchReport.Units và Job.Status.
func (e *Engine) ExecuteBatch(ctx context.Context, b Batch) (BatchReport, error) {
	return BatchReport{}, errBatchTODO
}
