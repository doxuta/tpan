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

// ErrPasswordBusy: đã đủ argonSlots lần argon2id đang chạy và chờ quá
// argonWait mà chưa tới lượt. Server trả 429 auth_busy (M28 đợt 2, P2).
var ErrPasswordBusy = errors.New("store: đang có quá nhiều lần kiểm mật khẩu cùng lúc")

// argonSlots là số lần argon2id được chạy cùng lúc trong một tiến trình (P2).
// Mỗi lần xin 64 MiB, nên bộ nhớ sống cho argon2 không quá 2 × 64 MiB; trước
// đây người chưa đăng nhập bắn 20 request song song là panel xin hơn 1 GiB.
// Hai chỗ đủ cho vài lần đăng nhập mỗi giây của một panel quản trị.
const argonSlots = 2

var (
	// argonSem là semaphore argonSlots chỗ: gửi vào là lấy chỗ, nhận ra là trả.
	argonSem = make(chan struct{}, argonSlots)
	// argonWait là thời gian chờ chỗ tối đa của HashPassword, VerifyPassword.
	// Biến (không phải hằng) để test đặt ngắn lại.
	argonWait = 3 * time.Second
	// argonKey là argon2.IDKey; test thay để đếm số lần chạy cùng lúc (E9).
	argonKey = argon2.IDKey
)

// argonRun chạy argon2id khi có chỗ trong argonSem. wait < 0 là chờ không hạn.
func argonRun(wait time.Duration, pw, salt []byte, t, mem uint32, threads uint8, keyLen uint32) ([]byte, error) {
	if wait < 0 {
		argonSem <- struct{}{}
	} else {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case argonSem <- struct{}{}:
		case <-timer.C:
			return nil, ErrPasswordBusy
		}
	}
	defer func() { <-argonSem }()
	return argonKey(pw, salt, t, mem, threads, keyLen), nil
}

// HashPassword băm pw bằng argon2id với salt ngẫu nhiên 16 byte, trả chuỗi
// dạng PHC: $argon2id$v=19$m=65536,t=3,p=4$<salt>$<hash> (base64 không đệm).
// Salt ngẫu nhiên khiến hai lần băm cùng một mật khẩu cho hai chuỗi khác nhau.
// Chờ chỗ argon2 tối đa argonWait, quá thì ErrPasswordBusy (dùng trong server).
func HashPassword(pw string) (string, error) { return hashPassword(pw, argonWait) }

// HashPasswordWait là HashPassword chờ chỗ không hạn: cho lệnh CLI (tiến trình
// riêng, không tranh chỗ với ai) và hash giả lúc server khởi động. Không bao
// giờ trả ErrPasswordBusy.
func HashPasswordWait(pw string) (string, error) { return hashPassword(pw, -1) }

func hashPassword(pw string, wait time.Duration) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("sinh salt: %w", err)
	}
	p := argonParams
	key, err := argonRun(wait, []byte(pw), salt, p.time, p.memory, p.threads, p.keyLen)
	if err != nil {
		return "", err
	}
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
// Chờ chỗ argon2 tối đa argonWait, quá thì ErrPasswordBusy; hash hỏng bị từ
// chối trước khi xin chỗ.
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
	got, err := argonRun(argonWait, []byte(pw), salt, t, mem, threads, uint32(len(want)))
	if err != nil {
		return false, err
	}
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
//
// M20 (mục 11.5, 11.8): cùng transaction đó xoá phiếu chờ bước hai của admin
// (phiếu chứng minh mật khẩu cũ), thu hồi mọi token còn sống của admin (người
// cầm mật khẩu cũ có thể đã tạo token), và gỡ khoá bước hai của admin (chủ máy
// gõ lệnh này khi bị khoá ngoài; không gỡ thì vẫn kẹt sau khi đổi mật khẩu).
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
		{"DELETE FROM login_challenges WHERE user_name = ?", []any{adminName}},
		{"UPDATE user_totp SET failures = 0, locked_until = NULL, lock_count = 0 WHERE user_name = ?", []any{adminName}},
	}
	for _, st := range stmts {
		if _, err := tx.ExecContext(ctx, st.q, st.args...); err != nil {
			return fmt.Errorf("đặt lại mật khẩu: %w", err)
		}
	}
	if _, err := revokeTokens(ctx, tx, adminName, now); err != nil {
		return fmt.Errorf("đặt lại mật khẩu: %w", err)
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
	// UserName là user TPAN sở hữu phiên (M13); rỗng khi tạo thì ghi "admin".
	UserName string
	// ViewAs là user mà admin đang "xem như" (phiên chỉ đọc); rỗng là bình thường.
	ViewAs string
	// Host là tên máy đã tạo phiên (M20, mục 11.2): request mang cookie mà Host
	// khác là cookie bị chép đi nơi khác. Rỗng là phiên của bản cũ.
	Host string
}

func scanSession(r rowScanner) (Session, error) {
	var (
		ss              Session
		created, lastAt string
	)
	if err := r.Scan(&ss.TokenSHA256, &ss.ID, &ss.IP, &created, &lastAt, &ss.UserName, &ss.ViewAs, &ss.Host); err != nil {
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

const sessionColumns = "token_sha256, id, ip, created_at, last_seen_at, user_name, view_as, host"

// CreateSession ghi một phiên mới (Created = LastSeen = thời điểm của ss.Created).
// UserName rỗng thì là "admin".
func (s *Store) CreateSession(ctx context.Context, ss Session) error {
	// Phiên của bản v0.1.x và mã gọi chưa biết tới user đều là của admin.
	user := ss.UserName
	if user == "" {
		user = adminName
	}
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO sessions ("+sessionColumns+") VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		ss.TokenSHA256, ss.ID, ss.IP, fmtTime(ss.Created), fmtTime(ss.Created), user, ss.ViewAs, ss.Host)
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

// Sessions trả mọi phiên của mọi user, phiên dùng gần nhất trước.
func (s *Store) Sessions(ctx context.Context) ([]Session, error) {
	return s.sessionsWhere(ctx, "đọc danh sách phiên", "")
}

// SessionsOf chỉ trả phiên của user name (màn Tài khoản không được lộ phiên
// của người khác). Phiên admin đang "xem như" name không tính: nó là của admin.
func (s *Store) SessionsOf(ctx context.Context, name string) ([]Session, error) {
	return s.sessionsWhere(ctx, "đọc phiên của "+name, name)
}

func (s *Store) sessionsWhere(ctx context.Context, what, user string) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+sessionColumns+" FROM sessions WHERE (? = '' OR user_name = ?) ORDER BY last_seen_at DESC, id",
		user, user)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	defer func() { _ = rows.Close() }()
	out := []Session{}
	for rows.Next() {
		ss, err := scanSession(rows)
		if err != nil {
			return nil, fmt.Errorf("%s: đọc dòng: %w", what, err)
		}
		out = append(out, ss)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
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

// DeleteSessionOf xoá phiên theo mã ngắn, CHỈ khi phiên đó thuộc user name:
// user không được đăng xuất phiên của người khác dù đoán được id. Không có
// hoặc không thuộc user thì ErrNotFound (cùng một lỗi để không lộ id có tồn tại).
func (s *Store) DeleteSessionOf(ctx context.Context, name, id string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE id = ? AND user_name = ?", id, name)
	if err != nil {
		return fmt.Errorf("xoá phiên %s của %s: %w", id, name, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("xoá phiên %s của %s: %w", id, name, err)
	} else if n == 0 {
		return fmt.Errorf("phiên %s của %s: %w", id, name, ErrNotFound)
	}
	return nil
}

// SetViewAs đặt (hoặc gỡ, khi viewAs rỗng) user mà phiên đang "xem như".
// Không có phiên thì ErrNotFound.
func (s *Store) SetViewAs(ctx context.Context, tokenSHA256, viewAs string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE sessions SET view_as = ? WHERE token_sha256 = ?", viewAs, tokenSHA256)
	if err != nil {
		return fmt.Errorf("đặt xem-như cho phiên: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("đặt xem-như cho phiên: %w", err)
	} else if n == 0 {
		return fmt.Errorf("phiên: %w", ErrNotFound)
	}
	return nil
}

// PasswordHashOf trả hash mật khẩu của user name (đăng nhập theo tên);
// ok=false khi không có user đó.
func (s *Store) PasswordHashOf(ctx context.Context, name string) (hash string, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, "SELECT password_hash FROM users WHERE name = ?", name).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("đọc hash mật khẩu của %s: %w", name, err)
	}
	return hash, true, nil
}

// ChangeAdminPassword là ChangeUserPassword cho user "admin". Khác một điểm giữ
// từ trước M13: máy chưa có dòng admin thì tạo luôn (như SetAdminPasswordHash)
// thay vì báo không tìm thấy.
func (s *Store) ChangeAdminPassword(ctx context.Context, hash, keepTokenSHA256 string, now time.Time) error {
	return s.changePassword(ctx, adminName, hash, keepTokenSHA256, now, true)
}

// ChangeUserPassword ghi hash mới cho user name và huỷ mọi phiên của user đó
// (kể cả phiên admin đang "xem như" họ) trừ phiên keepTokenSHA256 (người vừa
// đổi mật khẩu), trong một transaction. Không có user thì ErrNotFound.
func (s *Store) ChangeUserPassword(ctx context.Context, name, hash, keepTokenSHA256 string, now time.Time) error {
	return s.changePassword(ctx, name, hash, keepTokenSHA256, now, false)
}

func (s *Store) changePassword(ctx context.Context, name, hash, keep string, now time.Time, upsertAdmin bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("đổi mật khẩu của %s: %w", name, err)
	}
	defer func() { _ = tx.Rollback() }()
	if upsertAdmin {
		_, err = tx.ExecContext(ctx, upsertAdminSQL, name, hash, fmtTime(now))
	} else {
		var res sql.Result
		if res, err = tx.ExecContext(ctx, "UPDATE users SET password_hash = ? WHERE name = ?", hash, name); err == nil {
			if n, rerr := res.RowsAffected(); rerr != nil {
				err = rerr
			} else if n == 0 {
				return fmt.Errorf("đổi mật khẩu: user %s: %w", name, ErrNotFound)
			}
		}
	}
	if err != nil {
		return fmt.Errorf("đổi mật khẩu của %s: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx,
		"DELETE FROM sessions WHERE (user_name = ? OR view_as = ?) AND token_sha256 <> ?", name, name, keep); err != nil {
		return fmt.Errorf("đổi mật khẩu của %s: huỷ phiên khác: %w", name, err)
	}
	// Phiếu chờ bước hai chứng minh mật khẩu cũ (mục 11.8). Tự đổi mật khẩu
	// KHÔNG thu hồi token (mục 11.5): server nhắc số token còn sống.
	if err := deleteChallenges(ctx, tx, name); err != nil {
		return fmt.Errorf("đổi mật khẩu của %s: %w", name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("đổi mật khẩu của %s: %w", name, err)
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
//
// Cột ip của login_failures giữ "khoá nguồn" mà server đã chuẩn hoá (M28 đợt
// 2: địa chỉ IPv4, dải /64 của IPv6, hay uid:<n> cho kết nối loopback; xem
// sourceKey ở internal/server). Tên cột giữ nguyên để không cần migration.

// LoginFailure là dòng login_failures của một khoá nguồn sau một lần ghi.
type LoginFailure struct {
	Failures int
	// FirstAt là lần sai đầu của cửa sổ hiện tại. Server nhớ nó cạnh phần của
	// từng tên để chỉ trừ vào đúng cửa sổ đó (ForgetLoginFailures).
	FirstAt time.Time
	Locked  bool
	Until   time.Time // chỉ có nghĩa khi Locked
}

// LoginLockedUntil trả thời điểm hết khoá của ip nếu ip đang bị khoá lúc now.
// Khoá đã hết hạn thì until vẫn là thời điểm đó (khác zero) và locked=false.
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
// window (tính từ lần sai đầu) thì khoá ip tới now+lock và trả Locked=true.
// Khoá còn hạn thì giữ nguyên, không ghi gì (trước M28 đợt 2, một lần sai lọt
// vào lúc đang khoá làm đếm lại từ 1 và mất khoá). Khoá đã hết, hoặc lần sai
// đầu đã quá window, thì đếm lại từ 1: người gõ nhầm vài lần mỗi ngày không
// bị khoá dồn.
func (s *Store) RecordLoginFailure(ctx context.Context, ip string, now time.Time, maxFailures int, window, lock time.Duration) (LoginFailure, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return LoginFailure{}, fmt.Errorf("đếm đăng nhập sai: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var (
		f       LoginFailure
		firstAt string
		lu      sql.NullString
	)
	err = tx.QueryRowContext(ctx, "SELECT failures, first_at, locked_until FROM login_failures WHERE ip = ?", ip).
		Scan(&f.Failures, &firstAt, &lu)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		f.Failures, f.FirstAt = 0, now.UTC()
	case err != nil:
		return LoginFailure{}, fmt.Errorf("đọc đếm đăng nhập sai của %s: %w", ip, err)
	default:
		if f.FirstAt, err = parseTime(firstAt); err != nil {
			return LoginFailure{}, err
		}
		if lu.Valid {
			if f.Until, err = parseTime(lu.String); err != nil {
				return LoginFailure{}, err
			}
			if now.Before(f.Until) {
				f.Locked = true
				return f, nil
			}
		}
		if lu.Valid || now.Sub(f.FirstAt) > window {
			f.Failures, f.FirstAt, f.Until = 0, now.UTC(), time.Time{}
		}
	}
	f.Failures++
	var lockVal any
	if f.Failures >= maxFailures {
		f.Locked, f.Until = true, now.Add(lock)
		lockVal = fmtTime(f.Until)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO login_failures (ip, failures, first_at, locked_until) VALUES (?, ?, ?, ?)
		 ON CONFLICT (ip) DO UPDATE SET failures = excluded.failures, first_at = excluded.first_at,
		   locked_until = excluded.locked_until`,
		ip, f.Failures, fmtTime(f.FirstAt), lockVal); err != nil {
		return LoginFailure{}, fmt.Errorf("ghi đếm đăng nhập sai của %s: %w", ip, err)
	}
	if err := tx.Commit(); err != nil {
		return LoginFailure{}, fmt.Errorf("ghi đếm đăng nhập sai của %s: %w", ip, err)
	}
	return f, nil
}

// ForgetLoginFailures trừ n lần sai khỏi dòng của ip (đăng nhập đúng của một
// tên chỉ xoá phần của chính tên đó, M28 đợt 2). Chỉ trừ khi first_at của dòng
// còn đúng là firstAt (vẫn cửa sổ mà server đã đếm cho tên đó) và dòng không
// mang khoá; không trừ dưới 0; về 0 thì xoá dòng.
func (s *Store) ForgetLoginFailures(ctx context.Context, ip string, n int, firstAt time.Time) error {
	const where = " WHERE ip = ? AND first_at = ? AND locked_until IS NULL"
	fa := fmtTime(firstAt)
	if _, err := s.db.ExecContext(ctx, "DELETE FROM login_failures"+where+" AND failures <= ?", ip, fa, n); err != nil {
		return fmt.Errorf("trừ đếm đăng nhập sai của %s: %w", ip, err)
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE login_failures SET failures = failures - ?"+where, n, ip, fa); err != nil {
		return fmt.Errorf("trừ đếm đăng nhập sai của %s: %w", ip, err)
	}
	return nil
}

// ClearLoginFailures xoá dòng của một khoá nguồn (`tpan admin unlock <khoá>`);
// found=false khi không có dòng đó.
func (s *Store) ClearLoginFailures(ctx context.Context, ip string) (found bool, err error) {
	res, err := s.db.ExecContext(ctx, "DELETE FROM login_failures WHERE ip = ?", ip)
	if err != nil {
		return false, fmt.Errorf("gỡ khoá đăng nhập của %s: %w", ip, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("gỡ khoá đăng nhập của %s: %w", ip, err)
	}
	return n > 0, nil
}

// ClearAllLoginFailures xoá mọi dòng (`tpan admin unlock` không đối số), trả
// số dòng đã xoá. Tách khỏi ClearLoginFailures để không có đường nào đưa khoá
// rỗng vào mà xoá hết.
func (s *Store) ClearAllLoginFailures(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, "DELETE FROM login_failures")
	if err != nil {
		return 0, fmt.Errorf("gỡ mọi khoá đăng nhập: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("gỡ mọi khoá đăng nhập: %w", err)
	}
	return n, nil
}
