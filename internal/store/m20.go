package store

// Truy vấn của M20 (docs/thay-doi/M20-thiet-ke.md mục 3.2, 3.3): hai bước
// (user_totp, user_recovery_codes), phiếu chờ bước hai (login_challenges),
// token API (api_tokens). Bảng ở migrations/2000_hai_buoc_token.sql.
//
// Không hàm nào ở đây nhận hay trả bí mật dạng rõ: bí mật TOTP đến đã niêm
// phong (mfa.Seal), mã dự phòng và token đến đã băm (sha256 hex).
//
// HỢP ĐỒNG ĐỢT 0: chữ ký dưới đây là thứ server, cmd, health dựa vào; thân
// hàm do thợ của đợt 1 viết (cùng migration và test).

import (
	"context"
	"errors"
	"time"

	"github.com/doxuta/tpan/internal/account"
)

var errM20Todo = errors.New("store: M20 chưa viết (hợp đồng đợt 0)")

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
}

// TOTP đọc dòng hai bước của user; ok=false khi chưa có.
func (s *Store) TOTP(ctx context.Context, user string) (TOTPRow, bool, error) {
	return TOTPRow{}, false, errM20Todo
}

// TOTPEnabled: user đã bật (enabled_at khác NULL).
func (s *Store) TOTPEnabled(ctx context.Context, user string) (bool, error) {
	return false, errM20Todo
}

// SetPendingTOTP ghi (hoặc ghi đè) bí mật chờ xác nhận. Đã bật → ErrTOTPEnabled.
func (s *Store) SetPendingTOTP(ctx context.Context, user string, secretEnc []byte, now time.Time) error {
	return errM20Todo
}

// EnableTOTP trong MỘT transaction: chỉ khi đang chờ (không thì
// ErrTOTPNotPending); enabled_at=now, last_step=step, failures=0,
// locked_until=NULL; thay toàn bộ mã dự phòng bằng recoveryHashes; xoá mọi
// phiên của user (kể cả phiên admin đang "xem như" họ) trừ phiên keepSession
// (sha256 của token phiên); xoá mọi phiếu chờ của user.
func (s *Store) EnableTOTP(ctx context.Context, user string, step uint64, recoveryHashes []string, keepSession string, now time.Time) error {
	return errM20Todo
}

// DisableTOTP xoá dòng hai bước, mã dự phòng, phiếu chờ của user (một
// transaction). Không có dòng → ErrNotFound.
func (s *Store) DisableTOTP(ctx context.Context, user string) error { return errM20Todo }

// ResetTOTP là việc của `tpan user 2fa reset`: như DisableTOTP và xoá thêm
// mọi phiên của user (một transaction). Không có dòng hai bước vẫn xoá phiên
// và trả ErrNotFound để CLI nói "chưa bật".
func (s *Store) ResetTOTP(ctx context.Context, user string) error { return errM20Todo }

// UseTOTPStep nhận mã của bước step nếu step lớn hơn last_step (một câu
// UPDATE có điều kiện): true khi nhận (đặt last_step=step, failures=0,
// locked_until=NULL), false khi mã đã dùng hoặc thuộc bước cũ hơn.
func (s *Store) UseTOTPStep(ctx context.Context, user string, step uint64) (bool, error) {
	return false, errM20Todo
}

// UseRecoveryCode đánh dấu đã dùng mã có sha256 codeSHA256 nếu nó chưa dùng
// (UPDATE có điều kiện): true khi nhận; khi nhận cũng đặt failures=0,
// locked_until=NULL ở user_totp.
func (s *Store) UseRecoveryCode(ctx context.Context, user, codeSHA256 string, now time.Time) (bool, error) {
	return false, errM20Todo
}

// ReplaceRecoveryCodes thay toàn bộ mã dự phòng của user (một transaction).
func (s *Store) ReplaceRecoveryCodes(ctx context.Context, user string, hashes []string) error {
	return errM20Todo
}

// RecoveryLeft đếm mã dự phòng chưa dùng.
func (s *Store) RecoveryLeft(ctx context.Context, user string) (int, error) { return 0, errM20Todo }

// RecordTOTPFailure cộng một lần sai ở bước hai cho user. Lần sai thứ max
// (liên tiếp, tính từ lần đúng gần nhất) thì đặt locked_until=now+lock, đếm
// lại từ 0, và trả locked=true.
func (s *Store) RecordTOTPFailure(ctx context.Context, user string, now time.Time, max int, lock time.Duration) (locked bool, until time.Time, err error) {
	return false, time.Time{}, errM20Todo
}

// MFAStatus là tình trạng hai bước của một user (tpan user 2fa status, health).
type MFAStatus struct {
	User         string
	Role         account.Role
	Status       account.Status
	Enabled      bool
	EnabledAt    time.Time
	RecoveryLeft int
}

// MFAStatuses trả mọi user (có hay không có hai bước), sắp theo tên.
func (s *Store) MFAStatuses(ctx context.Context) ([]MFAStatus, error) { return nil, errM20Todo }

// Challenge là một phiếu chờ bước hai.
type Challenge struct {
	TokenSHA256 string
	User        string
	IP          string
	Created     time.Time
	Expires     time.Time
	Attempts    int
}

// CreateChallenge ghi một phiếu mới.
func (s *Store) CreateChallenge(ctx context.Context, c Challenge) error { return errM20Todo }

// ChallengeByToken tìm phiếu theo sha256 của token; không có → ErrNotFound.
// Không tự kiểm hết hạn: luật nằm ở server.
func (s *Store) ChallengeByToken(ctx context.Context, tokenSHA256 string) (Challenge, error) {
	return Challenge{}, errM20Todo
}

// BumpChallenge cộng một lần nhập sai cho phiếu, trả số lần sau khi cộng.
// Không có phiếu → ErrNotFound.
func (s *Store) BumpChallenge(ctx context.Context, tokenSHA256 string) (int, error) {
	return 0, errM20Todo
}

// ConsumeChallenge xoá phiếu; true khi đúng một dòng bị xoá (hai request đua
// nhau thì chỉ một bên nhận true).
func (s *Store) ConsumeChallenge(ctx context.Context, tokenSHA256 string) (bool, error) {
	return false, errM20Todo
}

// DeleteChallengesOf xoá mọi phiếu của user (đổi mật khẩu, khoá, reset).
func (s *Store) DeleteChallengesOf(ctx context.Context, user string) error { return errM20Todo }

// DeleteExpiredChallenges dọn phiếu đã hết hạn trước now.
func (s *Store) DeleteExpiredChallenges(ctx context.Context, now time.Time) error { return errM20Todo }

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

// CreateToken ghi token mới với sha256 của nó.
func (s *Store) CreateToken(ctx context.Context, t TokenRow, tokenSHA256 string) error {
	return errM20Todo
}

// TokenByHash tìm token theo sha256; không có → ErrNotFound. Không tự kiểm
// hạn hay thu hồi: luật nằm ở server.
func (s *Store) TokenByHash(ctx context.Context, tokenSHA256 string) (TokenRow, error) {
	return TokenRow{}, errM20Todo
}

// Tokens trả token của user (user "" là của mọi user), mới tạo trước.
func (s *Store) Tokens(ctx context.Context, user string) ([]TokenRow, error) { return nil, errM20Todo }

// RevokeToken thu hồi token id. user khác rỗng thì chỉ thu hồi khi token là
// của user đó. Không có, không phải của user, hay đã thu hồi → ErrNotFound
// (một lỗi cho cả ba, để không dò được id của người khác).
func (s *Store) RevokeToken(ctx context.Context, id, user string, now time.Time) error {
	return errM20Todo
}

// TouchToken ghi lần dùng gần nhất (giờ, IP).
func (s *Store) TouchToken(ctx context.Context, id, ip string, now time.Time) error {
	return errM20Todo
}
