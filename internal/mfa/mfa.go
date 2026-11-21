// Package mfa là phần thuần của đăng nhập hai bước (M20, docs/thay-doi/M20-thiet-ke.md
// mục 2.1, 2.2, 3.1): TOTP theo RFC 6238 (HOTP của RFC 4226 với bộ đếm là bước
// thời gian), ma trận QR của URI otpauth://, mã dự phòng dùng một lần, niêm
// phong bí mật TOTP bằng AES-256-GCM. Gói lá: chỉ thư viện chuẩn và rsc.io/qr,
// không đụng máy, không đụng DB.
//
// HỢP ĐỒNG ĐỢT 0: chữ ký dưới đây là thứ store, server, cmd dựa vào; thân hàm
// do thợ của đợt 1 viết (cùng test).
package mfa

import (
	"errors"
	"time"
)

// Tham số TOTP: đúng mặc định mà mọi app xác thực phổ biến nhận (mục 2.1).
const (
	Digits      = 6
	Period      = 30 // giây mỗi bước
	Skew        = 1  // chấp nhận bước hiện tại ±1
	SecretBytes = 20 // 160 bit, khuyến nghị R6 của RFC 4226
	// RecoveryCount là số mã dự phòng mỗi lần tạo.
	RecoveryCount = 10
)

var errTodo = errors.New("mfa: chưa viết (hợp đồng đợt 0)")

// NewSecret trả SecretBytes byte từ crypto/rand.
func NewSecret() []byte { panic(errTodo) }

// EncodeSecret: base32 RFC 4648, chữ hoa, không đệm (32 ký tự cho 20 byte).
func EncodeSecret(b []byte) string { panic(errTodo) }

// HOTP là mã của bộ đếm counter (RFC 4226 mục 5.3: HMAC-SHA1, cắt động,
// chia dư 10^digits, giữ số 0 đứng đầu).
func HOTP(secret []byte, counter uint64, digits int) string { panic(errTodo) }

// Step là bước thời gian của t: floor(unix(t) / Period); t trước 1970 là 0.
func Step(t time.Time) uint64 { panic(errTodo) }

// IsCode: s đúng Digits chữ số ASCII.
func IsCode(s string) bool { panic(errTodo) }

// Match thử các bước Step(now)-Skew .. Step(now)+Skew, so bằng
// subtle.ConstantTimeCompare và không dừng sớm; trả bước khớp lớn nhất.
// code không đúng dạng (IsCode sai) thì ok=false.
func Match(secret []byte, code string, now time.Time) (step uint64, ok bool) { panic(errTodo) }

// URI theo Key URI Format: otpauth://totp/<issuer>:<account>?secret=<B32>&issuer=<issuer>&algorithm=SHA1&digits=6&period=30.
func URI(issuer, account string, secret []byte) string { panic(errTodo) }

// QR là ma trận QR (rsc.io/qr, mức M) của text: mỗi chuỗi một hàng, '1' là
// module tối, không kèm vùng trắng.
func QR(text string) ([]string, error) { panic(errTodo) }

// NewRecoveryCodes trả RecoveryCount mã khác nhau dạng "xxxx-xxxx-xxxx-xxxx"
// (16 ký tự a-z2-7, 80 bit mỗi mã).
func NewRecoveryCodes() []string { panic(errTodo) }

// NormalizeRecovery bỏ khoảng trắng và '-', đổi chữ thường; không đúng 16 ký
// tự a-z2-7 thì trả "".
func NormalizeRecovery(s string) string { panic(errTodo) }

// HashRecovery là sha256 (hex) của mã đã chuẩn hoá.
func HashRecovery(normalized string) string { panic(errTodo) }

// Key là khoá AES-256 niêm phong bí mật TOTP (file /etc/tpan/secret.key).
type Key [32]byte

// KeyFile là chỗ của khoá trên máy chủ (0600 root, trong /etc/tpan 0700).
const KeyFile = "/etc/tpan/secret.key"

// ParseKey nhận nội dung file khoá: đúng 32 byte.
func ParseKey(b []byte) (Key, error) { panic(errTodo) }

// NewKey sinh khoá mới từ crypto/rand.
func NewKey() Key { panic(errTodo) }

// Seal niêm phong plain cho user: nonce 12 byte ngẫu nhiên || AES-256-GCM,
// AAD "tpan-totp-v1:"+user (chép bản mã sang user khác thì Open lỗi).
func Seal(k Key, user string, plain []byte) ([]byte, error) { panic(errTodo) }

// Open mở bản đã niêm phong; sai khoá, sai user, bị sửa thì lỗi.
func Open(k Key, user string, sealed []byte) ([]byte, error) { panic(errTodo) }
