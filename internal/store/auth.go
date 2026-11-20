package store

// Đăng nhập của panel (mục 11 của docs/KE-HOACH.md): hash mật khẩu quản trị,
// phiên đăng nhập, đếm lần đăng nhập sai. Bảng ở migrations/003_auth.sql.
//
// Hàm băm nằm ở store (không ở server) vì cả `tpan setup`, `tpan admin passwd`
// (cmd/tpan) lẫn server đều cần, và server, cmd đều đã import store.

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

// AdminPasswordKey là khoá trong bảng settings từng giữ hash mật khẩu quản
// trị (trước M13). Migration 1300 chuyển nó sang user "admin" của bảng users
// rồi xoá khoá này; hằng số còn lại để test và tài liệu nhắc tới.
const AdminPasswordKey = "admin.password_hash"

// argonParams là tham số argon2id cho hash MỚI. Hash cũ tự mang tham số của nó
// trong chuỗi PHC, nên đổi ở đây không làm hỏng mật khẩu đã lưu.
//
// 64 MiB, 3 lượt, 4 luồng: phương án thứ hai của RFC 9106 mục 4, dành cho máy
// không có 2 GiB RAM để dành cho một lần đăng nhập. Test đặt nhỏ lại cho nhanh.
var argonParams = struct {
	memory  uint32 // KiB
	time    uint32
	threads uint8
	keyLen  uint32
}{memory: 64 * 1024, time: 3, threads: 4, keyLen: 32}

// HashPassword băm pw bằng argon2id với salt ngẫu nhiên 16 byte, trả chuỗi
// dạng PHC: $argon2id$v=19$m=65536,t=3,p=4$<salt>$<hash> (base64 không đệm).
// Salt ngẫu nhiên khiến hai lần băm cùng một mật khẩu cho hai chuỗi khác nhau.
func HashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("sinh salt: %w", err)
	}
	p := argonParams
	key := argon2.IDKey([]byte(pw), salt, p.time, p.memory, p.threads, p.keyLen)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.memory, p.time, p.threads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// maxArgonMemory chặn hash hỏng (hay bị sửa tay trong DB) bắt máy cấp phát
// hàng chục GiB khi kiểm một lần đăng nhập.
const maxArgonMemory = 1024 * 1024 // 1 GiB, tính bằng KiB

// VerifyPassword so pw với hash dạng PHC. Tham số đọc từ chính chuỗi hash.
// So bằng subtle.ConstantTimeCompare để thời gian trả lời không lộ mấy byte đầu
// đã khớp. Hash hỏng thì trả lỗi (không phải false) để người gọi ghi log.
func VerifyPassword(hash, pw string) (bool, error) {
	parts := strings.Split(hash, "$")
	// "", "argon2id", "v=19", "m=..,t=..,p=..", salt, key
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return false, errors.New("hash mật khẩu không phải argon2id dạng PHC")
	}
	var v int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &v); err != nil || v != argon2.Version {
		return false, fmt.Errorf("hash mật khẩu: phiên bản argon2 %q không hỗ trợ", parts[2])
	}
	var (
		mem, t  uint32
		threads uint8
	)
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &t, &threads); err != nil {
		return false, fmt.Errorf("hash mật khẩu: tham số %q: %w", parts[3], err)
	}
	if mem == 0 || mem > maxArgonMemory || t == 0 || t > 100 || threads == 0 {
		return false, fmt.Errorf("hash mật khẩu: tham số ngoài khoảng cho phép %q", parts[3])
	}
	b64 := base64.RawStdEncoding
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return false, fmt.Errorf("hash mật khẩu: salt: %w", err)
	}
	want, err := b64.DecodeString(parts[5])
	if err != nil || len(want) == 0 || len(want) > 1024 {
		return false, errors.New("hash mật khẩu: phần hash hỏng")
	}
	got := argon2.IDKey([]byte(pw), salt, t, mem, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// GeneratePassword sinh mật khẩu quản trị: 26 ký tự A-Z và 2-7 (base32 của
// crypto/rand, ít nhất 128 bit ngẫu nhiên). Chỉ chữ và số nên đọc, gõ lại
// không nhầm dấu câu.
func GeneratePassword() string { return rand.Text() }

// AdminPasswordHash trả hash mật khẩu của user "admin" (M13: bảng users);
// ok=false khi chưa có (chưa chạy tpan setup).
func (s *Store) AdminPasswordHash(ctx context.Context) (hash string, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, "SELECT password_hash FROM users WHERE name = ?", adminName).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("đọc hash mật khẩu quản trị: %w", err)
	}
	return hash, true, nil
}

// adminName trùng account.AdminName; chép lại để store không phải import
// account chỉ vì một hằng số.
const adminName = "admin"

// upsertAdminSQL tạo user "admin" vai admin (máy mới, tpan setup) hoặc chỉ đổi
// hash (đã có). Không đổi vai hay trạng thái của dòng đã có.
const upsertAdminSQL = `INSERT INTO users (name, role, password_hash, created_at) VALUES (?, 'admin', ?, ?)
	ON CONFLICT (name) DO UPDATE SET password_hash = excluded.password_hash`

// SetAdminPasswordHash ghi hash mật khẩu của user "admin", không đụng tới phiên.
func (s *Store) SetAdminPasswordHash(ctx context.Context, hash string) error {
	if _, err := s.db.ExecContext(ctx, upsertAdminSQL, adminName, hash, fmtTime(time.Now())); err != nil {
		return fmt.Errorf("ghi hash mật khẩu quản trị: %w", err)
	}
	return nil
}

// ResetAdminPassword đặt hash mới, xoá mọi phiên và gỡ mọi khoá đăng nhập
// trong MỘT transaction (`tpan admin passwd`): không bao giờ có lúc mật khẩu
// đã đổi mà phiên của người cũ vẫn còn sống.
func (s *Store) ResetAdminPassword(ctx context.Context, hash string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("đặt lại mật khẩu: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	stmts := []struct {
		q    string
		args []any
	}{
		{upsertAdminSQL, []any{adminName, hash, fmtTime(now)}},
		// Chỉ phiên của admin (kể cả phiên admin đang "xem như" ai đó): mật
		// khẩu của user khác không đổi nên phiên của họ không có lý do bị huỷ.
		{"DELETE FROM sessions WHERE user_name = ?", []any{adminName}},
		{"DELETE FROM login_failures", nil},
	}
	for _, st := range stmts {
		if _, err := tx.ExecContext(ctx, st.q, st.args...); err != nil {
			return fmt.Errorf("đặt lại mật khẩu: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("đặt lại mật khẩu: %w", err)
	}
	return nil
}

// ---- Phiên ----

// Session là một dòng của bảng sessions. TokenSHA256 là sha256 (hex) của token
// trong cookie; token thật không bao giờ tới DB.
type Session struct {
	TokenSHA256 string
	ID          string
	IP          string
	Created     time.Time
	LastSeen    time.Time
}

func scanSession(r rowScanner) (Session, error) {
	var (
		ss              Session
		created, lastAt string
	)
	if err := r.Scan(&ss.TokenSHA256, &ss.ID, &ss.IP, &created, &lastAt); err != nil {
		return Session{}, err
	}
	var err error
	if ss.Created, err = parseTime(created); err != nil {
		return Session{}, err
	}
	if ss.LastSeen, err = parseTime(lastAt); err != nil {
		return Session{}, err
	}
	return ss, nil
}

const sessionColumns = "token_sha256, id, ip, created_at, last_seen_at"

// CreateSession ghi một phiên mới (Created = LastSeen = thời điểm của ss.Created).
func (s *Store) CreateSession(ctx context.Context, ss Session) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO sessions ("+sessionColumns+") VALUES (?, ?, ?, ?, ?)",
		ss.TokenSHA256, ss.ID, ss.IP, fmtTime(ss.Created), fmtTime(ss.Created))
	if err != nil {
		return fmt.Errorf("tạo phiên %s: %w", ss.ID, err)
	}
	return nil
}

// SessionByToken tìm phiên theo sha256 của token; không có thì ErrNotFound.
// Không tự kiểm hết hạn: luật 12 giờ nằm ở server.
func (s *Store) SessionByToken(ctx context.Context, tokenSHA256 string) (Session, error) {
	ss, err := scanSession(s.db.QueryRowContext(ctx,
		"SELECT "+sessionColumns+" FROM sessions WHERE token_sha256 = ?", tokenSHA256))
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, fmt.Errorf("phiên: %w", ErrNotFound)
	}
	if err != nil {
		return Session{}, fmt.Errorf("đọc phiên: %w", err)
	}
	return ss, nil
}

// TouchSession ghi lần dùng gần nhất của phiên.
func (s *Store) TouchSession(ctx context.Context, tokenSHA256 string, now time.Time) error {
	if _, err := s.db.ExecContext(ctx,
		"UPDATE sessions SET last_seen_at = ? WHERE token_sha256 = ?", fmtTime(now), tokenSHA256); err != nil {
		return fmt.Errorf("cập nhật phiên: %w", err)
	}
	return nil
}

// Sessions trả mọi phiên, phiên dùng gần nhất trước.
func (s *Store) Sessions(ctx context.Context) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+sessionColumns+" FROM sessions ORDER BY last_seen_at DESC, id")
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách phiên: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []Session{}
	for rows.Next() {
		ss, err := scanSession(rows)
		if err != nil {
			return nil, fmt.Errorf("đọc dòng phiên: %w", err)
		}
		out = append(out, ss)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("đọc danh sách phiên: %w", err)
	}
	return out, nil
}

// DeleteSessionByToken xoá phiên (đăng xuất, hết hạn). Không có thì thôi.
func (s *Store) DeleteSessionByToken(ctx context.Context, tokenSHA256 string) error {
	if _, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE token_sha256 = ?", tokenSHA256); err != nil {
		return fmt.Errorf("xoá phiên: %w", err)
	}
	return nil
}

// DeleteSessionByID xoá phiên theo mã ngắn (màn Tài khoản); không có thì ErrNotFound.
func (s *Store) DeleteSessionByID(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("xoá phiên %s: %w", id, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("xoá phiên %s: %w", id, err)
	} else if n == 0 {
		return fmt.Errorf("phiên %s: %w", id, ErrNotFound)
	}
	return nil
}

// ChangeAdminPassword ghi hash mới và xoá mọi phiên trừ phiên keepTokenSHA256
// (người vừa đổi mật khẩu), trong một transaction.
func (s *Store) ChangeAdminPassword(ctx context.Context, hash, keepTokenSHA256 string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("đổi mật khẩu: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, upsertAdminSQL, adminName, hash, fmtTime(now)); err != nil {
		return fmt.Errorf("đổi mật khẩu: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_name = ? AND token_sha256 <> ?", adminName, keepTokenSHA256); err != nil {
		return fmt.Errorf("đổi mật khẩu: huỷ phiên khác: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("đổi mật khẩu: %w", err)
	}
	return nil
}

// DeleteSessionsIdleBefore xoá các phiên không dùng từ trước thời điểm before
// (đã hết hạn), để bảng không phình mãi.
func (s *Store) DeleteSessionsIdleBefore(ctx context.Context, before time.Time) error {
	if _, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE last_seen_at < ?", fmtTime(before)); err != nil {
		return fmt.Errorf("dọn phiên hết hạn: %w", err)
	}
	return nil
}

// ---- Đếm lần đăng nhập sai ----

// LoginLockedUntil trả thời điểm hết khoá của ip nếu ip đang bị khoá lúc now.
func (s *Store) LoginLockedUntil(ctx context.Context, ip string, now time.Time) (until time.Time, locked bool, err error) {
	var lu sql.NullString
	err = s.db.QueryRowContext(ctx, "SELECT locked_until FROM login_failures WHERE ip = ?", ip).Scan(&lu)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !lu.Valid) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("đọc khoá đăng nhập của %s: %w", ip, err)
	}
	if until, err = parseTime(lu.String); err != nil {
		return time.Time{}, false, err
	}
	return until, now.Before(until), nil
}

// RecordLoginFailure cộng một lần sai cho ip. Lần sai thứ max trong cửa sổ
// window (tính từ lần sai đầu) thì khoá ip tới now+lock và trả locked=true.
// Lần sai đầu đã quá window, hoặc khoá cũ đã hết, thì đếm lại từ 1: người gõ
// nhầm vài lần mỗi ngày không bị khoá dồn.
func (s *Store) RecordLoginFailure(ctx context.Context, ip string, now time.Time, maxFailures int, window, lock time.Duration) (locked bool, until time.Time, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, time.Time{}, fmt.Errorf("đếm đăng nhập sai: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var (
		failures int
		firstAt  string
		lu       sql.NullString
	)
	err = tx.QueryRowContext(ctx, "SELECT failures, first_at, locked_until FROM login_failures WHERE ip = ?", ip).
		Scan(&failures, &firstAt, &lu)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		failures, firstAt = 0, fmtTime(now)
	case err != nil:
		return false, time.Time{}, fmt.Errorf("đọc đếm đăng nhập sai của %s: %w", ip, err)
	default:
		first, perr := parseTime(firstAt)
		if perr != nil {
			return false, time.Time{}, perr
		}
		if lu.Valid || now.Sub(first) > window {
			failures, firstAt = 0, fmtTime(now)
		}
	}
	failures++
	var lockVal any
	if failures >= maxFailures {
		locked, until = true, now.Add(lock)
		lockVal = fmtTime(until)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO login_failures (ip, failures, first_at, locked_until) VALUES (?, ?, ?, ?)
		 ON CONFLICT (ip) DO UPDATE SET failures = excluded.failures, first_at = excluded.first_at,
		   locked_until = excluded.locked_until`,
		ip, failures, firstAt, lockVal); err != nil {
		return false, time.Time{}, fmt.Errorf("ghi đếm đăng nhập sai của %s: %w", ip, err)
	}
	if err := tx.Commit(); err != nil {
		return false, time.Time{}, fmt.Errorf("ghi đếm đăng nhập sai của %s: %w", ip, err)
	}
	return locked, until, nil
}

// ClearLoginFailures xoá bộ đếm của ip (đăng nhập đúng).
func (s *Store) ClearLoginFailures(ctx context.Context, ip string) error {
	if _, err := s.db.ExecContext(ctx, "DELETE FROM login_failures WHERE ip = ?", ip); err != nil {
		return fmt.Errorf("xoá đếm đăng nhập sai của %s: %w", ip, err)
	}
	return nil
}
