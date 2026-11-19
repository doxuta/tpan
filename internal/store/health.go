package store

// Hợp đồng M4: kết quả phép kiểm và sự cố (mục 4.4 và 6.7 của docs/KE-HOACH.md;
// bảng ở migrations/400_health.sql). Gói health (internal/health) gọi các hàm
// này sau mỗi lần chạy phép kiểm; server đọc chúng cho Bảng điều khiển và màn
// Sự cố.
//
// Luật mở và đóng sự cố (RecordResults làm trong MỘT transaction):
//   - Kết quả Fail và phép kiểm đó chưa có sự cố đang mở: mở sự cố mới, giữ
//     Result lúc mở. Lần đầu tiên thấy phép kiểm mà nó đã Fail cũng mở.
//   - Kết quả Pass và có sự cố đang mở: đóng, giữ Result lúc đóng, lý do "pass".
//   - Warn và Unknown không mở cũng không đóng (đang mở thì vẫn mở): "chưa
//     kiểm được" không phải bằng chứng là đã hết lỗi.
//   - check_results: upsert theo id; changed_at chỉ đổi khi status đổi.

import (
	"context"
	"errors"
	"time"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
)

// errNotWritten: thân hàm chưa viết (đợt 0 chỉ có chữ ký). Thợ của phần
// store thay bằng mã thật.
var errNotWritten = errors.New("store: chưa viết")

// TargetTPAN là đối tượng của các phép kiểm chính tpan. "" là máy và stack;
// còn lại là domain của site.
const TargetTPAN = "tpan"

// StoredResult là một dòng check_results.
type StoredResult struct {
	checks.Result
	Target  string    `json:"target"`
	Checked time.Time `json:"checked"`
	// Changed là lần gần nhất Status đổi giá trị ("lỗi từ lúc nào").
	Changed time.Time `json:"changed"`
}

// Incident là một sự cố.
type Incident struct {
	ID      int64        `json:"id"`
	CheckID string       `json:"check_id"`
	Target  string       `json:"target"`
	Stage   checks.Stage `json:"stage"`
	Title   i18n.Text    `json:"title"`
	Opened  time.Time    `json:"opened"`
	// Closed zero khi sự cố còn mở.
	Closed time.Time `json:"closed,omitzero"`
	// CloseReason: "pass" (phép kiểm đạt lại) hoặc "gone" (đối tượng không
	// còn được kiểm, ví dụ site đã xoá); rỗng khi còn mở.
	CloseReason string `json:"close_reason,omitempty"`
	// OpenResult là bằng chứng lúc mở; CloseResult lúc đóng (nil khi còn mở
	// hoặc đóng vì "gone").
	OpenResult  checks.Result  `json:"open_result"`
	CloseResult *checks.Result `json:"close_result,omitempty"`
	// Jobs là id các job sửa đã chạy cho sự cố, cũ trước.
	Jobs []string `json:"jobs"`
}

// Transition là các sự cố vừa mở và vừa đóng sau một lần ghi kết quả.
type Transition struct {
	Opened []Incident `json:"opened"`
	Closed []Incident `json:"closed"`
}

// RecordResults ghi các kết quả của đối tượng target (upsert theo Result.ID)
// rồi mở, đóng sự cố theo luật ở đầu file. Không xoá kết quả nào khác của
// target: lần chạy nhẹ chỉ có một phần các phép kiểm (PruneResults lo việc xoá).
func (s *Store) RecordResults(ctx context.Context, target string, rs []checks.Result, now time.Time) (Transition, error) {
	return Transition{}, errNotWritten
}

// PruneResults xoá mọi kết quả có id không nằm trong keep (phép kiểm không
// còn tồn tại: site đã xoá, file thôi được quản lý) và đóng sự cố đang mở của
// chúng với lý do "gone". Chỉ gọi sau một lần chạy ĐẦY ĐỦ.
func (s *Store) PruneResults(ctx context.Context, keep []string, now time.Time) (Transition, error) {
	return Transition{}, errNotWritten
}

// CheckResults trả mọi kết quả, xếp theo target rồi id.
func (s *Store) CheckResults(ctx context.Context) ([]StoredResult, error) {
	return nil, errNotWritten
}

// CheckResult trả kết quả của một phép kiểm; ok=false khi không có.
func (s *Store) CheckResult(ctx context.Context, id string) (r StoredResult, ok bool, err error) {
	return StoredResult{}, false, errNotWritten
}

// Incidents trả sự cố mới nhất trước (theo opened_at, rồi id). openOnly chỉ
// lấy sự cố đang mở. limit <= 0 là không giới hạn.
func (s *Store) Incidents(ctx context.Context, openOnly bool, limit int) ([]Incident, error) {
	return nil, errNotWritten
}

// Incident trả một sự cố kèm Jobs; không có thì lỗi thoả errors.Is(err, ErrNotFound).
func (s *Store) Incident(ctx context.Context, id int64) (Incident, error) {
	return Incident{}, errNotWritten
}

// LinkIncidentJob gắn job vào sự cố ĐANG MỞ của checkID. Không có sự cố đang
// mở thì không làm gì và không lỗi (phép kiểm có thể vừa đạt lại).
func (s *Store) LinkIncidentJob(ctx context.Context, checkID, jobID string, now time.Time) error {
	return errNotWritten
}

// IntegrityCheck chạy PRAGMA integrity_check và trả nguyên văn kết quả
// (một SQLite lành trả "ok").
func (s *Store) IntegrityCheck(ctx context.Context) (string, error) {
	return "", errNotWritten
}

// StuckJobs trả các job còn status "running" mà bắt đầu trước before (tiến
// trình chạy nó đã chết giữa chừng, hoặc treo).
func (s *Store) StuckJobs(ctx context.Context, before time.Time) ([]engine.Job, error) {
	return nil, errNotWritten
}
