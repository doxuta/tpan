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
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
)

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

// Lý do đóng sự cố (cột close_reason).
const (
	closePass = "pass"
	closeGone = "gone"
)

// querier là phần chung của *sql.DB và *sql.Tx, để các hàm đọc sự cố dùng được
// cả trong transaction lẫn ngoài. Store chỉ có MỘT kết nối (SetMaxOpenConns(1)),
// nên trong transaction mà đọc qua s.db là treo: s.db chờ kết nối mà tx đang giữ.
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// inTx chạy fn trong một transaction: fn lỗi thì hoàn tác hết, để không bao giờ
// có kết quả đã ghi mà sự cố tương ứng chưa mở (hay ngược lại).
func (s *Store) inTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("mở transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // không làm gì nếu đã Commit
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// Cột data, open_data, close_data giữ JSON của cả checks.Result (bằng chứng,
// cách sửa) như job_events giữ engine.Event: thêm trường cho Result không cần
// migration mới.
func marshalResult(r checks.Result) (string, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return "", fmt.Errorf("mã hoá kết quả %s: %w", r.ID, err)
	}
	return string(b), nil
}

func unmarshalResult(data string) (checks.Result, error) {
	var r checks.Result
	if err := json.Unmarshal([]byte(data), &r); err != nil {
		return checks.Result{}, fmt.Errorf("giải mã kết quả phép kiểm: %w", err)
	}
	return r, nil
}

// RecordResults ghi các kết quả của đối tượng target (upsert theo Result.ID)
// rồi mở, đóng sự cố theo luật ở đầu file. Không xoá kết quả nào khác của
// target: lần chạy nhẹ chỉ có một phần các phép kiểm (PruneResults lo việc xoá).
func (s *Store) RecordResults(ctx context.Context, target string, rs []checks.Result, now time.Time) (Transition, error) {
	var tr Transition
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		for _, r := range rs {
			data, err := marshalResult(r)
			if err != nil {
				return err
			}
			// changed_at chỉ đổi khi status đổi: đó là "lỗi từ lúc nào" mà màn
			// hình hiện, không phải lần kiểm gần nhất (checked_at).
			_, err = tx.ExecContext(ctx,
				`INSERT INTO check_results (id, target, stage, status, data, checked_at, changed_at)
				 VALUES (?, ?, ?, ?, ?, ?, ?)
				 ON CONFLICT (id) DO UPDATE SET
				   target = excluded.target, stage = excluded.stage, status = excluded.status,
				   data = excluded.data, checked_at = excluded.checked_at,
				   changed_at = CASE WHEN check_results.status = excluded.status
				                     THEN check_results.changed_at ELSE excluded.changed_at END`,
				r.ID, target, string(r.Stage), string(r.Status), data, fmtTime(now), fmtTime(now))
			if err != nil {
				return fmt.Errorf("ghi kết quả %s: %w", r.ID, err)
			}

			var openID int64
			err = tx.QueryRowContext(ctx,
				"SELECT id FROM incidents WHERE check_id = ? AND closed_at IS NULL", r.ID).Scan(&openID)
			open := err == nil
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("tìm sự cố đang mở của %s: %w", r.ID, err)
			}

			switch {
			case r.Status == checks.Fail && !open:
				res, err := tx.ExecContext(ctx,
					`INSERT INTO incidents (check_id, target, stage, title_vi, title_en, opened_at, open_data)
					 VALUES (?, ?, ?, ?, ?, ?, ?)`,
					r.ID, target, string(r.Stage), r.Title.Vi, r.Title.En, fmtTime(now), data)
				if err != nil {
					return fmt.Errorf("mở sự cố cho %s: %w", r.ID, err)
				}
				id, err := res.LastInsertId()
				if err != nil {
					return fmt.Errorf("mở sự cố cho %s: %w", r.ID, err)
				}
				inc, err := getIncident(ctx, tx, id)
				if err != nil {
					return err
				}
				tr.Opened = append(tr.Opened, inc)
			case r.Status == checks.Pass && open:
				inc, err := closeIncident(ctx, tx, openID, closePass, data, now)
				if err != nil {
					return err
				}
				tr.Closed = append(tr.Closed, inc)
			}
			// Warn, Unknown: không mở, không đóng ("chưa kiểm được" không phải
			// bằng chứng đã hết lỗi). Fail khi đã có sự cố mở: giữ sự cố cũ.
		}
		return nil
	})
	if err != nil {
		return Transition{}, fmt.Errorf("ghi kết quả phép kiểm của %q: %w", target, err)
	}
	return tr, nil
}

// closeIncident đóng sự cố id rồi đọc lại. closeData rỗng (đóng vì "gone") lưu NULL.
func closeIncident(ctx context.Context, q querier, id int64, reason, closeData string, now time.Time) (Incident, error) {
	var cd any
	if closeData != "" {
		cd = closeData
	}
	if _, err := q.ExecContext(ctx,
		"UPDATE incidents SET closed_at = ?, close_data = ?, close_reason = ? WHERE id = ?",
		fmtTime(now), cd, reason, id); err != nil {
		return Incident{}, fmt.Errorf("đóng sự cố %d: %w", id, err)
	}
	return getIncident(ctx, q, id)
}

// PruneResults xoá mọi kết quả có id không nằm trong keep (phép kiểm không
// còn tồn tại: site đã xoá, file thôi được quản lý) và đóng sự cố đang mở của
// chúng với lý do "gone". Chỉ gọi sau một lần chạy ĐẦY ĐỦ.
func (s *Store) PruneResults(ctx context.Context, keep []string, now time.Time) (Transition, error) {
	keepSet := make(map[string]bool, len(keep))
	for _, id := range keep {
		keepSet[id] = true
	}
	var tr Transition
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		// Đóng theo bảng incidents, không theo check_results: sự cố mở mà dòng
		// kết quả đã mất (dữ liệu cũ, hoặc xoá tay) cũng phải đóng, không thì nó
		// mở mãi trên màn Sự cố.
		type openInc struct {
			id      int64
			checkID string
		}
		var open []openInc
		if err := queryEach(ctx, tx, "SELECT id, check_id FROM incidents WHERE closed_at IS NULL ORDER BY id",
			func(r rowScanner) error {
				var o openInc
				if err := r.Scan(&o.id, &o.checkID); err != nil {
					return err
				}
				open = append(open, o)
				return nil
			}); err != nil {
			return fmt.Errorf("đọc sự cố đang mở: %w", err)
		}
		for _, o := range open {
			if keepSet[o.checkID] {
				continue
			}
			inc, err := closeIncident(ctx, tx, o.id, closeGone, "", now)
			if err != nil {
				return err
			}
			tr.Closed = append(tr.Closed, inc)
		}

		var gone []string
		if err := queryEach(ctx, tx, "SELECT id FROM check_results", func(r rowScanner) error {
			var id string
			if err := r.Scan(&id); err != nil {
				return err
			}
			if !keepSet[id] {
				gone = append(gone, id)
			}
			return nil
		}); err != nil {
			return fmt.Errorf("đọc kết quả phép kiểm: %w", err)
		}
		for _, id := range gone {
			if _, err := tx.ExecContext(ctx, "DELETE FROM check_results WHERE id = ?", id); err != nil {
				return fmt.Errorf("xoá kết quả %s: %w", id, err)
			}
		}
		return nil
	})
	if err != nil {
		return Transition{}, fmt.Errorf("dọn kết quả phép kiểm: %w", err)
	}
	return tr, nil
}

// queryEach chạy query rồi gọi fn cho từng dòng, và đóng rows trước khi trả
// về: với một kết nối, rows còn mở thì câu lệnh sau sẽ chờ mãi.
func queryEach(ctx context.Context, q querier, query string, fn func(rowScanner) error, args ...any) error {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		if err := fn(rows); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return rows.Close()
}

const resultColumns = "target, data, checked_at, changed_at"

func scanStoredResult(r rowScanner) (StoredResult, error) {
	var sr StoredResult
	var data, checked, changed string
	if err := r.Scan(&sr.Target, &data, &checked, &changed); err != nil {
		return StoredResult{}, err
	}
	var err error
	if sr.Result, err = unmarshalResult(data); err != nil {
		return StoredResult{}, err
	}
	if sr.Checked, err = parseTime(checked); err != nil {
		return StoredResult{}, err
	}
	if sr.Changed, err = parseTime(changed); err != nil {
		return StoredResult{}, err
	}
	return sr, nil
}

// CheckResults trả mọi kết quả, xếp theo target rồi id.
func (s *Store) CheckResults(ctx context.Context) ([]StoredResult, error) {
	out := []StoredResult{}
	err := queryEach(ctx, s.db, "SELECT "+resultColumns+" FROM check_results ORDER BY target, id",
		func(r rowScanner) error {
			sr, err := scanStoredResult(r)
			if err != nil {
				return err
			}
			out = append(out, sr)
			return nil
		})
	if err != nil {
		return nil, fmt.Errorf("đọc kết quả phép kiểm: %w", err)
	}
	return out, nil
}

// CheckResult trả kết quả của một phép kiểm; ok=false khi không có.
func (s *Store) CheckResult(ctx context.Context, id string) (r StoredResult, ok bool, err error) {
	r, err = scanStoredResult(s.db.QueryRowContext(ctx,
		"SELECT "+resultColumns+" FROM check_results WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return StoredResult{}, false, nil
	}
	if err != nil {
		return StoredResult{}, false, fmt.Errorf("đọc kết quả %s: %w", id, err)
	}
	return r, true, nil
}

const incidentColumns = "id, check_id, target, stage, title_vi, title_en, opened_at, open_data, closed_at, close_data, close_reason"

// scanIncident đọc một dòng incidents; Jobs để rỗng, loadJobs điền sau.
func scanIncident(r rowScanner) (Incident, error) {
	var (
		inc                     Incident
		stage, opened, openData string
		closed, closeData       sql.NullString
	)
	if err := r.Scan(&inc.ID, &inc.CheckID, &inc.Target, &stage, &inc.Title.Vi, &inc.Title.En,
		&opened, &openData, &closed, &closeData, &inc.CloseReason); err != nil {
		return Incident{}, err
	}
	inc.Stage = checks.Stage(stage)
	inc.Jobs = []string{}
	var err error
	if inc.Opened, err = parseTime(opened); err != nil {
		return Incident{}, err
	}
	if inc.OpenResult, err = unmarshalResult(openData); err != nil {
		return Incident{}, err
	}
	if closed.Valid {
		if inc.Closed, err = parseTime(closed.String); err != nil {
			return Incident{}, err
		}
	}
	if closeData.Valid {
		cr, err := unmarshalResult(closeData.String)
		if err != nil {
			return Incident{}, err
		}
		inc.CloseResult = &cr
	}
	return inc, nil
}

// loadJobs điền Jobs cho các sự cố bằng một câu truy vấn, cũ trước (added_at,
// rồi rowid cho hai job gắn cùng lúc). Gọi SAU khi rows của sự cố đã đóng.
func loadJobs(ctx context.Context, q querier, incs []Incident) error {
	idx := make(map[int64]int, len(incs))
	for i, inc := range incs {
		idx[inc.ID] = i
	}
	// Chia lô: SQLite giới hạn số tham số của một câu lệnh (32766), mà sự cố
	// không bao giờ bị dọn, nên "không giới hạn" của Incidents có ngày vượt nó.
	const batch = 500
	for start := 0; start < len(incs); start += batch {
		part := incs[start:min(start+batch, len(incs))]
		args := make([]any, len(part))
		for i, inc := range part {
			args[i] = inc.ID
		}
		// Chỉ ghép dấu ?, giá trị vẫn đi qua tham số. Thứ tự job trong một sự
		// cố không phụ thuộc lô vì mỗi sự cố nằm trọn trong một lô.
		query := "SELECT incident_id, job_id FROM incident_jobs WHERE incident_id IN (?" +
			strings.Repeat(", ?", len(part)-1) + ") ORDER BY added_at, rowid"
		err := queryEach(ctx, q, query, func(r rowScanner) error {
			var id int64
			var job string
			if err := r.Scan(&id, &job); err != nil {
				return err
			}
			if i, ok := idx[id]; ok {
				incs[i].Jobs = append(incs[i].Jobs, job)
			}
			return nil
		}, args...)
		if err != nil {
			return err
		}
	}
	return nil
}

// getIncident đọc một sự cố kèm Jobs; không có thì lỗi thoả ErrNotFound.
func getIncident(ctx context.Context, q querier, id int64) (Incident, error) {
	inc, err := scanIncident(q.QueryRowContext(ctx, "SELECT "+incidentColumns+" FROM incidents WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return Incident{}, fmt.Errorf("sự cố %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return Incident{}, fmt.Errorf("đọc sự cố %d: %w", id, err)
	}
	one := []Incident{inc}
	if err := loadJobs(ctx, q, one); err != nil {
		return Incident{}, fmt.Errorf("đọc job của sự cố %d: %w", id, err)
	}
	return one[0], nil
}

// Incidents trả sự cố mới nhất trước (theo opened_at, rồi id). openOnly chỉ
// lấy sự cố đang mở. limit <= 0 là không giới hạn.
func (s *Store) Incidents(ctx context.Context, openOnly bool, limit int) ([]Incident, error) {
	if limit <= 0 {
		limit = -1 // SQLite: LIMIT âm là không giới hạn
	}
	query := "SELECT " + incidentColumns + " FROM incidents"
	if openOnly {
		query += " WHERE closed_at IS NULL"
	}
	query += " ORDER BY opened_at DESC, id DESC LIMIT ?"
	out := []Incident{}
	err := queryEach(ctx, s.db, query, func(r rowScanner) error {
		inc, err := scanIncident(r)
		if err != nil {
			return err
		}
		out = append(out, inc)
		return nil
	}, limit)
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách sự cố: %w", err)
	}
	if err := loadJobs(ctx, s.db, out); err != nil {
		return nil, fmt.Errorf("đọc job của sự cố: %w", err)
	}
	return out, nil
}

// Incident trả một sự cố kèm Jobs; không có thì lỗi thoả errors.Is(err, ErrNotFound).
func (s *Store) Incident(ctx context.Context, id int64) (Incident, error) {
	return getIncident(ctx, s.db, id)
}

// LinkIncidentJob gắn job vào sự cố ĐANG MỞ của checkID. Không có sự cố đang
// mở thì không làm gì và không lỗi (phép kiểm có thể vừa đạt lại).
func (s *Store) LinkIncidentJob(ctx context.Context, checkID, jobID string, now time.Time) error {
	// Một câu lệnh: SELECT không ra dòng nào thì không chèn gì. OR IGNORE: gắn
	// lại cùng job (chạy lại sửa) không lỗi và giữ added_at của lần đầu.
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO incident_jobs (incident_id, job_id, added_at)
		 SELECT id, ?, ? FROM incidents WHERE check_id = ? AND closed_at IS NULL`,
		jobID, fmtTime(now), checkID)
	if err != nil {
		return fmt.Errorf("gắn job %s vào sự cố của %s: %w", jobID, checkID, err)
	}
	return nil
}

// IntegrityCheck chạy PRAGMA integrity_check và trả nguyên văn kết quả
// (một SQLite lành trả "ok").
func (s *Store) IntegrityCheck(ctx context.Context) (string, error) {
	// DB hỏng trả nhiều dòng, mỗi dòng một chỗ hỏng: nối lại để giữ hết bằng chứng.
	var lines []string
	err := queryEach(ctx, s.db, "PRAGMA integrity_check", func(r rowScanner) error {
		var l string
		if err := r.Scan(&l); err != nil {
			return err
		}
		lines = append(lines, l)
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("kiểm toàn vẹn DB: %w", err)
	}
	return strings.Join(lines, "\n"), nil
}

// StuckJobs trả các job còn status "running" mà bắt đầu trước before (tiến
// trình chạy nó đã chết giữa chừng, hoặc treo).
func (s *Store) StuckJobs(ctx context.Context, before time.Time) ([]engine.Job, error) {
	// started_at có độ rộng cố định (timeLayout) nên so chuỗi là so thời gian.
	out := []engine.Job{}
	err := queryEach(ctx, s.db,
		"SELECT "+jobColumns+" FROM jobs WHERE status = ? AND started_at < ? ORDER BY started_at, rowid",
		func(r rowScanner) error {
			j, err := scanJob(r)
			if err != nil {
				return err
			}
			out = append(out, j)
			return nil
		}, string(engine.JobRunning), fmtTime(before))
	if err != nil {
		return nil, fmt.Errorf("đọc job treo: %w", err)
	}
	return out, nil
}

// CheckResultOf là CheckResult chỉ lấy phần checks.Result (M8): core.Planner
// dựng Plan sửa một phát hiện của health từ kết quả đã lưu, mà core không
// import store nên không gọi tên được StoredResult.
func (s *Store) CheckResultOf(ctx context.Context, id string) (checks.Result, bool, error) {
	r, ok, err := s.CheckResult(ctx, id)
	return r.Result, ok, err
}
