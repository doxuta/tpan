package store

// Truy vấn của M20 (docs/thay-doi/M20-thiet-ke.md mục 3.2, 3.3, 11): hai bước
// (user_totp, user_recovery_codes), phiếu chờ bước hai (login_challenges),
// token API (api_tokens). Bảng ở migrations/2000_hai_buoc_token.sql.
//
// Không hàm nào ở đây nhận hay trả bí mật dạng rõ: bí mật TOTP đến đã niêm
// phong (mfa.Seal), mã dự phòng và token đến đã băm (sha256 hex).
//
// Mọi chỗ "kiểm rồi ghi" mà hai request có thể đua nhau (dùng mã, dùng phiếu,
// bật hai bước, thu hồi token) là MỘT câu UPDATE/DELETE có điều kiện, và
// RowsAffected quyết định ai thắng: không có khe nào giữa lúc đọc và lúc ghi.
//
// Mọi thời điểm đi qua fmtTime (độ rộng cố định) trước khi so trong SQL: so
// chuỗi khi đó đúng bằng so thời gian (store.go, timeLayout).

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/doxuta/tpan/internal/account"
)

// ErrTOTPEnabled: user đã bật hai bước (SetPendingTOTP không ghi đè bí mật đang dùng).
var ErrTOTPEnabled = errors.New("user đã bật đăng nhập hai bước")

// ErrTOTPNotPending: chưa có bí mật chờ xác nhận (EnableTOTP).
var ErrTOTPNotPending = errors.New("chưa có bí mật hai bước chờ xác nhận")

// TOTPRow là một dòng của user_totp.
type TOTPRow struct {
	User      string
	SecretEnc []byte // nonce || AES-256-GCM (mfa.Seal)
	Created   time.Time
	// Enabled zero: đang chờ xác nhận (chưa bật).
	Enabled     time.Time
	LastStep    uint64
	Failures    int
	LockedUntil time.Time
	// LockCount: số lần bị khoá liên tiếp từ lần đúng gần nhất (khoá leo thang).
	LockCount int
	// WrongSinceOK: số mã sai kể từ lần đăng nhập đúng gần nhất (không reset
	// khi khoá; TakeWrongSinceOK đọc rồi đặt về 0).
	WrongSinceOK int
}

// parseOptTime đọc cột thời gian có thể NULL (NULL là thời điểm zero).
func parseOptTime(ns sql.NullString) (time.Time, error) {
	if !ns.Valid {
		return time.Time{}, nil
	}
	return parseTime(ns.String)
}

// TOTP đọc dòng hai bước của user; ok=false khi chưa có.
func (s *Store) TOTP(ctx context.Context, user string) (TOTPRow, bool, error) {
	var (
		r                 TOTPRow
		created           string
		enabled, lockedAt sql.NullString
		step              int64
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT user_name, secret_enc, created_at, enabled_at, last_step, failures, locked_until, lock_count, wrong_since_ok
		 FROM user_totp WHERE user_name = ?`, user).
		Scan(&r.User, &r.SecretEnc, &created, &enabled, &step, &r.Failures, &lockedAt, &r.LockCount, &r.WrongSinceOK)
	if errors.Is(err, sql.ErrNoRows) {
		return TOTPRow{}, false, nil
	}
	if err != nil {
		return TOTPRow{}, false, fmt.Errorf("đọc hai bước của %s: %w", user, err)
	}
	r.LastStep = uint64(step) // last_step chỉ nhận số do mfa.Step tính (>= 0)
	if r.Created, err = parseTime(created); err != nil {
		return TOTPRow{}, false, fmt.Errorf("hai bước của %s: %w", user, err)
	}
	if r.Enabled, err = parseOptTime(enabled); err != nil {
		return TOTPRow{}, false, fmt.Errorf("hai bước của %s: %w", user, err)
	}
	if r.LockedUntil, err = parseOptTime(lockedAt); err != nil {
		return TOTPRow{}, false, fmt.Errorf("hai bước của %s: %w", user, err)
	}
	return r, true, nil
}

// TOTPEnabled: user đã bật (enabled_at khác NULL).
func (s *Store) TOTPEnabled(ctx context.Context, user string) (bool, error) {
	var on bool
	err := s.db.QueryRowContext(ctx,
		"SELECT enabled_at IS NOT NULL FROM user_totp WHERE user_name = ?", user).Scan(&on)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("đọc hai bước của %s: %w", user, err)
	}
	return on, nil
}

// SetPendingTOTP ghi (hoặc ghi đè) bí mật chờ xác nhận. Đã bật → ErrTOTPEnabled.
func (s *Store) SetPendingTOTP(ctx context.Context, user string, secretEnc []byte, now time.Time) error {
	if len(secretEnc) == 0 {
		return fmt.Errorf("ghi bí mật hai bước của %s: bí mật rỗng", user)
	}
	// Một câu: điều kiện "chưa bật" nằm ở WHERE của nhánh DO UPDATE, nên không
	// có khe để một lần bật chen vào giữa lúc kiểm và lúc ghi đè. Dòng đã bật
	// thì câu không đổi gì (RowsAffected = 0).
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO user_totp (user_name, secret_enc, created_at) VALUES (?, ?, ?)
		 ON CONFLICT (user_name) DO UPDATE SET secret_enc = excluded.secret_enc, created_at = excluded.created_at
		 WHERE user_totp.enabled_at IS NULL`,
		user, secretEnc, fmtTime(now))
	if err != nil {
		return fmt.Errorf("ghi bí mật hai bước của %s: %w", user, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("ghi bí mật hai bước của %s: %w", user, err)
	}
	if n == 0 {
		return fmt.Errorf("ghi bí mật hai bước của %s: %w", user, ErrTOTPEnabled)
	}
	return nil
}

// EnableTOTP trong MỘT transaction: chỉ khi đang chờ VÀ bí mật chờ đúng là
// secretEnc (bản đã dùng để so mã; một lần setup chen vào giữa thì không
// khớp → ErrTOTPNotPending); enabled_at=now, last_step=step, failures=0,
// locked_until=NULL, lock_count=0; thay toàn bộ mã dự phòng bằng
// recoveryHashes; xoá mọi phiên của user trừ phiên keepSession (sha256 của
// token phiên; phiên admin đang "xem như" họ giữ nguyên); xoá mọi phiếu chờ
// của user.
func (s *Store) EnableTOTP(ctx context.Context, user string, secretEnc []byte, step uint64, recoveryHashes []string, keepSession string, now time.Time) error {
	if len(secretEnc) == 0 {
		return fmt.Errorf("bật hai bước cho %s: %w", user, ErrTOTPNotPending)
	}
	return s.inTx(ctx, func(tx *sql.Tx) error {
		// Câu đầu tiên quyết định: so cả secret_enc nên nếu một lần setup khác
		// đã ghi đè bí mật chờ sau khi người dùng so mã, câu không khớp dòng nào
		// và không bật nhầm bí mật mà người dùng chưa từng quét (mục 11.5).
		res, err := tx.ExecContext(ctx,
			`UPDATE user_totp SET enabled_at = ?, last_step = ?, failures = 0, locked_until = NULL, lock_count = 0,
			   wrong_since_ok = 0
			 WHERE user_name = ? AND enabled_at IS NULL AND secret_enc = ?`,
			fmtTime(now), int64(step), user, secretEnc)
		if err != nil {
			return fmt.Errorf("bật hai bước cho %s: %w", user, err)
		}
		if n, err := res.RowsAffected(); err != nil {
			return fmt.Errorf("bật hai bước cho %s: %w", user, err)
		} else if n == 0 {
			return fmt.Errorf("bật hai bước cho %s: %w", user, ErrTOTPNotPending)
		}
		if err := replaceRecovery(ctx, tx, user, recoveryHashes); err != nil {
			return fmt.Errorf("bật hai bước cho %s: %w", user, err)
		}
		// Đăng xuất mọi nơi khác: nếu người bật không phải chủ thật (có mật khẩu
		// lộ), chủ thật bị đăng xuất và thấy ngay (mục 11.9). Phiên admin đang
		// "xem như" user là của admin, không đụng tới (không dùng endSessionsOf).
		if _, err := tx.ExecContext(ctx,
			"DELETE FROM sessions WHERE user_name = ? AND token_sha256 <> ?", user, keepSession); err != nil {
			return fmt.Errorf("bật hai bước cho %s: huỷ phiên khác: %w", user, err)
		}
		if err := deleteChallenges(ctx, tx, user); err != nil {
			return fmt.Errorf("bật hai bước cho %s: %w", user, err)
		}
		return nil
	})
}

// dropTOTP xoá phiếu chờ, mã dự phòng, dòng hai bước (và phiên, khi sessions)
// của user; found=false khi không có dòng hai bước.
func dropTOTP(ctx context.Context, q querier, user string, sessions bool) (found bool, err error) {
	if sessions {
		if err := endSessionsOf(ctx, q, user); err != nil {
			return false, err
		}
	} else if err := deleteChallenges(ctx, q, user); err != nil {
		return false, err
	}
	if _, err := q.ExecContext(ctx, "DELETE FROM user_recovery_codes WHERE user_name = ?", user); err != nil {
		return false, fmt.Errorf("xoá mã dự phòng: %w", err)
	}
	res, err := q.ExecContext(ctx, "DELETE FROM user_totp WHERE user_name = ?", user)
	if err != nil {
		return false, fmt.Errorf("xoá hai bước: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("xoá hai bước: %w", err)
	}
	return n > 0, nil
}

// DisableTOTP xoá dòng hai bước, mã dự phòng, phiếu chờ của user (một
// transaction). Không có dòng → ErrNotFound.
func (s *Store) DisableTOTP(ctx context.Context, user string) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		found, err := dropTOTP(ctx, tx, user, false)
		if err != nil {
			return fmt.Errorf("tắt hai bước của %s: %w", user, err)
		}
		if !found {
			// Hoàn tác: không có gì để tắt thì không đổi gì.
			return fmt.Errorf("hai bước của %s: %w", user, ErrNotFound)
		}
		return nil
	})
}

// ResetTOTP là việc của `tpan user 2fa reset`: như DisableTOTP và xoá thêm
// mọi phiên của user (một transaction). Không có dòng hai bước vẫn xoá phiên
// và trả ErrNotFound để CLI nói "chưa bật".
func (s *Store) ResetTOTP(ctx context.Context, user string) error {
	var found bool
	// Closure trả nil cả khi không có dòng, để inTx commit việc xoá phiên;
	// ErrNotFound trả sau khi commit.
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var err error
		found, err = dropTOTP(ctx, tx, user, true)
		return err
	})
	if err != nil {
		return fmt.Errorf("đặt lại hai bước của %s: %w", user, err)
	}
	if !found {
		return fmt.Errorf("hai bước của %s: %w", user, ErrNotFound)
	}
	return nil
}

// UseTOTPStep nhận mã của bước step nếu step lớn hơn last_step (một câu
// UPDATE có điều kiện): true khi nhận (đặt last_step=step, failures=0,
// locked_until=NULL, lock_count=0), false khi mã đã dùng hoặc thuộc bước cũ hơn.
func (s *Store) UseTOTPStep(ctx context.Context, user string, step uint64) (bool, error) {
	// lock_count = 0 theo mục 11.1 (đè mục 3.3): đúng mã thì hết khoá leo thang
	// và hết cảnh báo tpan.2fa:attempts.
	res, err := s.db.ExecContext(ctx,
		`UPDATE user_totp SET last_step = ?, failures = 0, locked_until = NULL, lock_count = 0
		 WHERE user_name = ? AND enabled_at IS NOT NULL AND last_step < ?`,
		int64(step), user, int64(step))
	if err != nil {
		return false, fmt.Errorf("nhận mã hai bước của %s: %w", user, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("nhận mã hai bước của %s: %w", user, err)
	}
	return n == 1, nil
}

// UseRecoveryCode đánh dấu đã dùng mã có sha256 codeSHA256 nếu nó chưa dùng
// (UPDATE có điều kiện): true khi nhận; khi nhận cũng đặt failures=0,
// locked_until=NULL, lock_count=0 ở user_totp.
func (s *Store) UseRecoveryCode(ctx context.Context, user, codeSHA256 string, now time.Time) (bool, error) {
	var ok bool
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE user_recovery_codes SET used_at = ?
			 WHERE user_name = ? AND code_sha256 = ? AND used_at IS NULL`,
			fmtTime(now), user, codeSHA256)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil || n == 0 {
			return err
		}
		ok = true
		// Mã dự phòng đúng gỡ khoá user (mục 11.1): người chỉ có mật khẩu không
		// nhốt được chủ tài khoản mãi.
		_, err = tx.ExecContext(ctx,
			"UPDATE user_totp SET failures = 0, locked_until = NULL, lock_count = 0 WHERE user_name = ?", user)
		return err
	})
	if err != nil {
		return false, fmt.Errorf("dùng mã dự phòng của %s: %w", user, err)
	}
	return ok, nil
}

// replaceRecovery thay mã dự phòng trên q (transaction của người gọi): store
// chỉ có một kết nối, nên trong transaction mà gọi qua s.db là treo.
func replaceRecovery(ctx context.Context, q querier, user string, hashes []string) error {
	if _, err := q.ExecContext(ctx, "DELETE FROM user_recovery_codes WHERE user_name = ?", user); err != nil {
		return fmt.Errorf("xoá mã dự phòng cũ: %w", err)
	}
	for _, h := range hashes {
		if _, err := q.ExecContext(ctx,
			"INSERT INTO user_recovery_codes (user_name, code_sha256) VALUES (?, ?)", user, h); err != nil {
			return fmt.Errorf("ghi mã dự phòng: %w", err)
		}
	}
	return nil
}

// ReplaceRecoveryCodes thay toàn bộ mã dự phòng của user (một transaction).
func (s *Store) ReplaceRecoveryCodes(ctx context.Context, user string, hashes []string) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if err := replaceRecovery(ctx, tx, user, hashes); err != nil {
			return fmt.Errorf("thay mã dự phòng của %s: %w", user, err)
		}
		return nil
	})
}

// RecoveryLeft đếm mã dự phòng chưa dùng.
func (s *Store) RecoveryLeft(ctx context.Context, user string) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM user_recovery_codes WHERE user_name = ? AND used_at IS NULL", user).Scan(&n); err != nil {
		return 0, fmt.Errorf("đếm mã dự phòng của %s: %w", user, err)
	}
	return n, nil
}

// lockFor là thời gian khoá lần thứ n liên tiếp: base×2^(n−1), trần cap.
// Nhân đôi từng bước và dừng ở trần, để n lớn không tràn time.Duration.
func lockFor(n int, base, cap time.Duration) time.Duration {
	d := base
	for i := 1; i < n && d < cap; i++ {
		d *= 2
	}
	return min(d, cap)
}

// RecordTOTPFailure cộng một lần sai ở bước hai cho user (người gọi đã giữ
// khoá theo user trong tiến trình, mục 11.1 của thiết kế). Lần sai thứ max
// liên tiếp thì lock_count++, locked_until = now + base×2^(lock_count−1)
// (trần cap), failures = 0, và trả locked=true cùng until, lockCount. Lần sai
// ghi lúc đang khoá KHÔNG gỡ khoá và không đếm lại từ đầu.
//
// Cụ thể (để server viết theo):
//   - locked=true CHỈ khi chính lần gọi này vừa khoá (mỗi lần khoá một dòng
//     audit login.2fa_locked).
//   - Đang khoá (locked_until > now): không đổi failures, khoá, lock_count;
//     trả locked=false cùng until và lockCount của khoá đang có (until khác
//     zero và sau now nghĩa là vẫn đang khoá).
//   - Chưa khoá và chưa tới max: trả false, zero, lockCount hiện có.
//   - Mọi lần sai (kể cả lúc đang khoá) cộng wrong_since_ok.
//   - User chưa bật hai bước (không có dòng, hoặc còn đang chờ) → ErrNotFound.
func (s *Store) RecordTOTPFailure(ctx context.Context, user string, now time.Time, max int, base, cap time.Duration) (locked bool, until time.Time, lockCount int, err error) {
	// Đọc, tính, ghi trong một transaction: DSN có _txlock=immediate nên BEGIN
	// đã giữ quyền ghi (cả với tiến trình khác), không ai ghi xen vào được giữa
	// câu đọc và câu ghi. Không gộp được thành một câu UPDATE: thời điểm hết
	// khoá phụ thuộc lock_count mới và phải định dạng bằng fmtTime ở Go.
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		var (
			failures int
			lu       sql.NullString
		)
		err := tx.QueryRowContext(ctx,
			"SELECT failures, locked_until, lock_count FROM user_totp WHERE user_name = ? AND enabled_at IS NOT NULL",
			user).Scan(&failures, &lu, &lockCount)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("hai bước của %s: %w", user, ErrNotFound)
		}
		if err != nil {
			return err
		}
		cur, err := parseOptTime(lu)
		if err != nil {
			return err
		}
		if now.Before(cur) {
			// Đang khoá: lần sai này (chỉ có thể là mã dự phòng sai, mã 6 số bị
			// chặn trước Match) không gỡ khoá, không đặt lại bộ đếm; chỉ được
			// kể lại cho chủ tài khoản ở lần đăng nhập đúng sau (wrong_since_ok).
			until = cur
			_, err = tx.ExecContext(ctx,
				"UPDATE user_totp SET wrong_since_ok = wrong_since_ok + 1 WHERE user_name = ?", user)
			return err
		}
		failures++
		if failures < max {
			_, err = tx.ExecContext(ctx,
				"UPDATE user_totp SET failures = ?, wrong_since_ok = wrong_since_ok + 1 WHERE user_name = ?", failures, user)
			return err
		}
		lockCount++
		locked, until = true, now.Add(lockFor(lockCount, base, cap))
		_, err = tx.ExecContext(ctx,
			`UPDATE user_totp SET failures = 0, locked_until = ?, lock_count = ?, wrong_since_ok = wrong_since_ok + 1
			 WHERE user_name = ?`,
			fmtTime(until), lockCount, user)
		return err
	})
	if err != nil {
		return false, time.Time{}, 0, fmt.Errorf("ghi mã hai bước sai của %s: %w", user, err)
	}
	return locked, until, lockCount, nil
}

// TakeWrongSinceOK trả số mã sai kể từ lần đăng nhập đúng gần nhất rồi đặt nó
// về 0, trong một transaction: server gọi sau khi bước hai đúng để báo "có N
// lần nhập sai mã từ lần trước", và hai lần đăng nhập đúng đua nhau thì chỉ
// một bên thấy N (bên kia thấy 0), không báo trùng. Không có dòng → 0.
//
// UseTOTPStep, UseRecoveryCode cố ý không đặt lại cột này: chúng chạy trước,
// đặt lại ở đó thì hàm này luôn đọc được 0.
func (s *Store) TakeWrongSinceOK(ctx context.Context, user string) (int, error) {
	var n int
	// SQLite RETURNING trả giá trị SAU khi ghi, nên không lấy được số cũ bằng
	// một câu; BEGIN IMMEDIATE (DSN) giữ quyền ghi từ câu đọc tới câu ghi.
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, "SELECT wrong_since_ok FROM user_totp WHERE user_name = ?", user).Scan(&n)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && n == 0) {
			return nil
		}
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE user_totp SET wrong_since_ok = 0 WHERE user_name = ?", user)
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("đọc số mã sai của %s: %w", user, err)
	}
	return n, nil
}

// MFAStatus là tình trạng hai bước của một user (tpan user 2fa status, health).
type MFAStatus struct {
	User         string
	Role         account.Role
	Status       account.Status
	Enabled      bool
	EnabledAt    time.Time
	RecoveryLeft int
	// LockCount > 0: có người nhập sai mã nhiều lần cho user này từ lần đúng
	// gần nhất (phép kiểm tpan.2fa:attempts).
	LockCount int
	// WrongSinceOK: số mã sai kể từ lần đăng nhập đúng gần nhất.
	WrongSinceOK int
}

// MFAStatuses trả mọi user (có hay không có hai bước), sắp theo tên.
func (s *Store) MFAStatuses(ctx context.Context) ([]MFAStatus, error) {
	out := []MFAStatus{}
	err := queryEach(ctx, s.db,
		`SELECT u.name, u.role, u.status, t.enabled_at, COALESCE(t.lock_count, 0), COALESCE(t.wrong_since_ok, 0),
		        (SELECT COUNT(*) FROM user_recovery_codes r WHERE r.user_name = u.name AND r.used_at IS NULL)
		 FROM users u LEFT JOIN user_totp t ON t.user_name = u.name
		 ORDER BY u.name`,
		func(r rowScanner) error {
			var (
				m            MFAStatus
				role, status string
				enabled      sql.NullString
			)
			if err := r.Scan(&m.User, &role, &status, &enabled, &m.LockCount, &m.WrongSinceOK, &m.RecoveryLeft); err != nil {
				return err
			}
			m.Role, m.Status = account.Role(role), account.Status(status)
			t, err := parseOptTime(enabled)
			if err != nil {
				return fmt.Errorf("user %s: %w", m.User, err)
			}
			m.Enabled, m.EnabledAt = enabled.Valid, t
			out = append(out, m)
			return nil
		})
	if err != nil {
		return nil, fmt.Errorf("đọc tình trạng hai bước: %w", err)
	}
	return out, nil
}

// Challenge là một phiếu chờ bước hai.
type Challenge struct {
	TokenSHA256 string
	User        string
	IP          string
	// Host: tên máy lúc tạo phiếu (Host bỏ cổng, chữ thường).
	Host     string
	Created  time.Time
	Expires  time.Time
	Attempts int
}

// CreateChallenge ghi một phiếu mới.
func (s *Store) CreateChallenge(ctx context.Context, c Challenge) error {
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO login_challenges (token_sha256, user_name, ip, host, created_at, expires_at, attempts)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		c.TokenSHA256, c.User, c.IP, c.Host, fmtTime(c.Created), fmtTime(c.Expires), c.Attempts); err != nil {
		return fmt.Errorf("tạo phiếu bước hai cho %s: %w", c.User, err)
	}
	return nil
}

// ChallengeByToken tìm phiếu theo sha256 của token; không có → ErrNotFound.
// Không tự kiểm hết hạn: luật nằm ở server.
func (s *Store) ChallengeByToken(ctx context.Context, tokenSHA256 string) (Challenge, error) {
	var (
		c                Challenge
		created, expires string
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT token_sha256, user_name, ip, host, created_at, expires_at, attempts
		 FROM login_challenges WHERE token_sha256 = ?`, tokenSHA256).
		Scan(&c.TokenSHA256, &c.User, &c.IP, &c.Host, &created, &expires, &c.Attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return Challenge{}, fmt.Errorf("phiếu bước hai: %w", ErrNotFound)
	}
	if err != nil {
		return Challenge{}, fmt.Errorf("đọc phiếu bước hai: %w", err)
	}
	if c.Created, err = parseTime(created); err != nil {
		return Challenge{}, fmt.Errorf("phiếu bước hai: %w", err)
	}
	if c.Expires, err = parseTime(expires); err != nil {
		return Challenge{}, fmt.Errorf("phiếu bước hai: %w", err)
	}
	return c, nil
}

// BumpChallenge cộng một lần nhập sai cho phiếu, trả số lần sau khi cộng.
// Không có phiếu → ErrNotFound.
func (s *Store) BumpChallenge(ctx context.Context, tokenSHA256 string) (int, error) {
	// RETURNING: cộng và đọc số mới trong cùng một câu.
	var n int
	err := s.db.QueryRowContext(ctx,
		"UPDATE login_challenges SET attempts = attempts + 1 WHERE token_sha256 = ? RETURNING attempts",
		tokenSHA256).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("phiếu bước hai: %w", ErrNotFound)
	}
	if err != nil {
		return 0, fmt.Errorf("đếm lần sai của phiếu: %w", err)
	}
	return n, nil
}

// ConsumeChallenge xoá phiếu; true khi đúng một dòng bị xoá (hai request đua
// nhau thì chỉ một bên nhận true).
func (s *Store) ConsumeChallenge(ctx context.Context, tokenSHA256 string) (bool, error) {
	res, err := s.db.ExecContext(ctx, "DELETE FROM login_challenges WHERE token_sha256 = ?", tokenSHA256)
	if err != nil {
		return false, fmt.Errorf("dùng phiếu bước hai: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("dùng phiếu bước hai: %w", err)
	}
	return n == 1, nil
}

// deleteChallenges xoá mọi phiếu của user trên q (để gọi trong transaction
// của đổi mật khẩu, khoá, xoá user: phiếu chứng minh mật khẩu cũ).
func deleteChallenges(ctx context.Context, q querier, user string) error {
	if _, err := q.ExecContext(ctx, "DELETE FROM login_challenges WHERE user_name = ?", user); err != nil {
		return fmt.Errorf("xoá phiếu bước hai: %w", err)
	}
	return nil
}

// DeleteChallengesOf xoá mọi phiếu của user (đổi mật khẩu, khoá, reset).
func (s *Store) DeleteChallengesOf(ctx context.Context, user string) error {
	if err := deleteChallenges(ctx, s.db, user); err != nil {
		return fmt.Errorf("phiếu của %s: %w", user, err)
	}
	return nil
}

// DeleteExpiredChallenges dọn phiếu đã hết hạn trước now.
func (s *Store) DeleteExpiredChallenges(ctx context.Context, now time.Time) error {
	if _, err := s.db.ExecContext(ctx, "DELETE FROM login_challenges WHERE expires_at < ?", fmtTime(now)); err != nil {
		return fmt.Errorf("dọn phiếu bước hai hết hạn: %w", err)
	}
	return nil
}

// DeleteSessionsByHost xoá mọi phiên có host đúng bằng host (đổi hoặc xoá tên
// panel: cookie đặt dưới tên cũ chết trước khi tên đó có thể thành tên site).
// host rỗng thì không làm gì (không xoá phiên của bản cũ).
func (s *Store) DeleteSessionsByHost(ctx context.Context, host string) error {
	if host == "" {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE host = ?", host); err != nil {
		return fmt.Errorf("xoá phiên của tên %s: %w", host, err)
	}
	return nil
}

// revokeTokens thu hồi token còn sống (chưa thu hồi, chưa hết hạn) của user
// trên q, để đặt lại mật khẩu thu hồi trong cùng transaction (mục 11.5).
func revokeTokens(ctx context.Context, q querier, user string, now time.Time) (int, error) {
	at := fmtTime(now)
	res, err := q.ExecContext(ctx,
		"UPDATE api_tokens SET revoked_at = ? WHERE user_name = ? AND revoked_at IS NULL AND expires_at > ?",
		at, user, at)
	if err != nil {
		return 0, fmt.Errorf("thu hồi token: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("thu hồi token: %w", err)
	}
	return int(n), nil
}

// RevokeTokensOf thu hồi mọi token còn sống của user, trả số token vừa thu hồi.
// Token đã hết hạn không tính (nó đã chết), nên số trả về bằng ActiveTokens ngay trước đó.
func (s *Store) RevokeTokensOf(ctx context.Context, user string, now time.Time) (int, error) {
	n, err := revokeTokens(ctx, s.db, user, now)
	if err != nil {
		return 0, fmt.Errorf("token của %s: %w", user, err)
	}
	return n, nil
}

// ActiveTokens đếm token chưa thu hồi và chưa hết hạn của user lúc now.
func (s *Store) ActiveTokens(ctx context.Context, user string, now time.Time) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM api_tokens WHERE user_name = ? AND revoked_at IS NULL AND expires_at > ?",
		user, fmtTime(now)).Scan(&n); err != nil {
		return 0, fmt.Errorf("đếm token của %s: %w", user, err)
	}
	return n, nil
}

// TokenRow là một dòng của api_tokens (không có token thật).
type TokenRow struct {
	ID       string
	User     string
	Name     string
	Scopes   string // apitoken.Scopes.String()
	Hint     string
	Created  time.Time
	Expires  time.Time
	Revoked  time.Time // zero: chưa thu hồi
	LastUsed time.Time // zero: chưa dùng
	LastIP   string
}

const tokenColumns = "id, user_name, name, scopes, hint, created_at, expires_at, revoked_at, last_used_at, last_used_ip"

func scanToken(r rowScanner) (TokenRow, error) {
	var (
		t                TokenRow
		created, expires string
		revoked, used    sql.NullString
	)
	if err := r.Scan(&t.ID, &t.User, &t.Name, &t.Scopes, &t.Hint, &created, &expires, &revoked, &used, &t.LastIP); err != nil {
		return TokenRow{}, err
	}
	var err error
	if t.Created, err = parseTime(created); err != nil {
		return TokenRow{}, fmt.Errorf("token %s: %w", t.ID, err)
	}
	if t.Expires, err = parseTime(expires); err != nil {
		return TokenRow{}, fmt.Errorf("token %s: %w", t.ID, err)
	}
	if t.Revoked, err = parseOptTime(revoked); err != nil {
		return TokenRow{}, fmt.Errorf("token %s: %w", t.ID, err)
	}
	if t.LastUsed, err = parseOptTime(used); err != nil {
		return TokenRow{}, fmt.Errorf("token %s: %w", t.ID, err)
	}
	return t, nil
}

// CreateToken ghi token mới với sha256 của nó.
func (s *Store) CreateToken(ctx context.Context, t TokenRow, tokenSHA256 string) error {
	if t.ID == "" || tokenSHA256 == "" {
		return fmt.Errorf("tạo token %q: thiếu id hoặc sha256", t.ID)
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO api_tokens (id, token_sha256, user_name, name, scopes, hint, created_at, expires_at, revoked_at, last_used_at, last_used_ip)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, tokenSHA256, t.User, t.Name, t.Scopes, t.Hint, fmtTime(t.Created), fmtTime(t.Expires),
		nullTime(t.Revoked), nullTime(t.LastUsed), t.LastIP); err != nil {
		return fmt.Errorf("tạo token %s: %w", t.ID, err)
	}
	return nil
}

// TokenByHash tìm token theo sha256; không có → ErrNotFound. Không tự kiểm
// hạn hay thu hồi: luật nằm ở server.
func (s *Store) TokenByHash(ctx context.Context, tokenSHA256 string) (TokenRow, error) {
	t, err := scanToken(s.db.QueryRowContext(ctx,
		"SELECT "+tokenColumns+" FROM api_tokens WHERE token_sha256 = ?", tokenSHA256))
	if errors.Is(err, sql.ErrNoRows) {
		return TokenRow{}, fmt.Errorf("token: %w", ErrNotFound)
	}
	if err != nil {
		return TokenRow{}, fmt.Errorf("đọc token: %w", err)
	}
	return t, nil
}

// Tokens trả token của user (user "" là của mọi user), mới tạo trước.
func (s *Store) Tokens(ctx context.Context, user string) ([]TokenRow, error) {
	out := []TokenRow{}
	err := queryEach(ctx, s.db,
		"SELECT "+tokenColumns+" FROM api_tokens WHERE (? = '' OR user_name = ?) ORDER BY created_at DESC, rowid DESC",
		func(r rowScanner) error {
			t, err := scanToken(r)
			if err != nil {
				return err
			}
			out = append(out, t)
			return nil
		}, user, user)
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách token: %w", err)
	}
	return out, nil
}

// RevokeToken thu hồi token id. user khác rỗng thì chỉ thu hồi khi token là
// của user đó. Không có, không phải của user, hay đã thu hồi → ErrNotFound
// (một lỗi cho cả ba, để không dò được id của người khác).
func (s *Store) RevokeToken(ctx context.Context, id, user string, now time.Time) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE api_tokens SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL AND (? = '' OR user_name = ?)",
		fmtTime(now), id, user, user)
	if err != nil {
		return fmt.Errorf("thu hồi token %s: %w", id, err)
	}
	return needRow(res, "token "+id, "thu hồi token "+id)
}

// TouchToken ghi lần dùng gần nhất (giờ, IP).
func (s *Store) TouchToken(ctx context.Context, id, ip string, now time.Time) error {
	if _, err := s.db.ExecContext(ctx,
		"UPDATE api_tokens SET last_used_at = ?, last_used_ip = ? WHERE id = ?", fmtTime(now), ip, id); err != nil {
		return fmt.Errorf("ghi lần dùng token %s: %w", id, err)
	}
	return nil
}
