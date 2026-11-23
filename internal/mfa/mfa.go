// Package mfa là phần thuần của đăng nhập hai bước (M20, docs/thay-doi/M20-thiet-ke.md
// mục 2.1, 2.2, 3.1): TOTP theo RFC 6238 (HOTP của RFC 4226 với bộ đếm là bước
// thời gian), ma trận QR của URI otpauth://, mã dự phòng dùng một lần, niêm
// phong bí mật TOTP bằng AES-256-GCM. Gói lá: chỉ thư viện chuẩn và rsc.io/qr,
// không đụng máy, không đụng DB.
package mfa

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // HMAC-SHA1 là thuật toán của RFC 4226/6238; va chạm SHA-1 không làm yếu HMAC.
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	// Chỉ gói gốc: rsc.io/qr/libqrencode dùng cgo, build CGO_ENABLED=0 sẽ hỏng.
	"rsc.io/qr"
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

// b32Upper là bảng chữ mà app xác thực đòi cho bí mật trong QR (chữ hoa, không '=').
var b32Upper = base32.StdEncoding.WithPadding(base32.NoPadding)

// b32Lower cho mã dự phòng: chữ thường dễ đọc to và gõ lại hơn.
var b32Lower = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// NewSecret trả SecretBytes byte từ crypto/rand.
func NewSecret() []byte {
	b := make([]byte, SecretBytes)
	_, _ = rand.Read(b) // từ Go 1.24 Read không bao giờ trả lỗi
	return b
}

// EncodeSecret: base32 RFC 4648, chữ hoa, không đệm (32 ký tự cho 20 byte).
func EncodeSecret(b []byte) string { return b32Upper.EncodeToString(b) }

// HOTP là mã của bộ đếm counter (RFC 4226 mục 5.3: HMAC-SHA1, cắt động,
// chia dư 10^digits, giữ số 0 đứng đầu).
func HOTP(secret []byte, counter uint64, digits int) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	mac := hmac.New(sha1.New, secret)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	// Cắt động: 4 bit cuối chọn chỗ lấy 4 byte; bỏ bit dấu để mọi nền tính
	// ra cùng một số không âm (RFC 4226 mục 5.3).
	off := sum[len(sum)-1] & 0x0f
	bin := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	mod := uint32(1)
	for range digits {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", digits, bin%mod)
}

// Step là bước thời gian của t: floor(unix(t) / Period); t trước 1970 là 0.
func Step(t time.Time) uint64 {
	u := t.Unix()
	if u < 0 {
		return 0
	}
	return uint64(u) / Period
}

// IsCode: s đúng Digits chữ số ASCII.
func IsCode(s string) bool {
	if len(s) != Digits {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// Match thử các bước Step(now)-Skew .. Step(now)+Skew, so bằng
// subtle.ConstantTimeCompare và không dừng sớm; trả bước khớp lớn nhất.
// code không đúng dạng (IsCode sai) thì ok=false.
//
// Không dừng sớm để thời gian trả lời không cho biết mã khớp ở bước nào. Trả
// bước lớn nhất để UseTOTPStep ghi last_step cao nhất có thể: mã của bước nhỏ
// hơn (trùng mã ngẫu nhiên giữa hai bước) không bao giờ được dùng lại sau đó.
func Match(secret []byte, code string, now time.Time) (step uint64, ok bool) {
	if !IsCode(code) {
		return 0, false
	}
	cur := Step(now)
	for d := -Skew; d <= Skew; d++ {
		if d < 0 && cur < uint64(-d) {
			continue // không có bước âm; uint64 trừ quá sẽ quay về 2^64-1
		}
		s := cur + uint64(d) // d âm: phép cộng không dấu quay vòng đúng bằng cur-|d|
		if subtle.ConstantTimeCompare([]byte(HOTP(secret, s, Digits)), []byte(code)) == 1 {
			step, ok = s, true
		}
	}
	return step, ok
}

// URI theo Key URI Format: otpauth://totp/<issuer>:<account>?secret=<B32>&issuer=<issuer>&algorithm=SHA1&digits=6&period=30.
//
// ':' trong issuer hay account cũng được mã hoá (url.PathEscape để nguyên nó):
// tên máy là địa chỉ IPv6 thì app sẽ tách sai chỗ issuer:account. Dấu cách
// thành %20 thay vì '+': vài app hiện '+' nguyên văn trong tên issuer.
func URI(issuer, account string, secret []byte) string {
	esc := func(s string) string { return strings.ReplaceAll(url.PathEscape(s), ":", "%3A") }
	v := url.Values{}
	v.Set("secret", EncodeSecret(secret))
	v.Set("issuer", issuer)
	v.Set("algorithm", "SHA1")
	v.Set("digits", fmt.Sprint(Digits))
	v.Set("period", fmt.Sprint(Period))
	// QueryEscape viết '+' thật thành %2B, nên mọi '+' còn lại là dấu cách.
	q := strings.ReplaceAll(v.Encode(), "+", "%20")
	return "otpauth://totp/" + esc(issuer) + ":" + esc(account) + "?" + q
}

// QR là ma trận QR (rsc.io/qr, mức M) của text: mỗi chuỗi một hàng, '1' là
// module tối, không kèm vùng trắng.
func QR(text string) ([]string, error) {
	c, err := qr.Encode(text, qr.M)
	if err != nil {
		return nil, fmt.Errorf("vẽ QR: %w", err)
	}
	rows := make([]string, c.Size)
	var b strings.Builder
	for y := range c.Size {
		b.Reset()
		for x := range c.Size {
			if c.Black(x, y) {
				b.WriteByte('1')
			} else {
				b.WriteByte('0')
			}
		}
		rows[y] = b.String()
	}
	return rows, nil
}

// NewRecoveryCodes trả RecoveryCount mã khác nhau dạng "xxxx-xxxx-xxxx-xxxx"
// (16 ký tự a-z2-7, 80 bit mỗi mã).
func NewRecoveryCodes() []string {
	seen := map[string]bool{}
	out := make([]string, 0, RecoveryCount)
	for len(out) < RecoveryCount {
		var raw [10]byte
		_, _ = rand.Read(raw[:])
		s := b32Lower.EncodeToString(raw[:])
		// Trùng 80 bit gần như không thể, nhưng hai mã trùng sẽ vỡ khoá chính
		// (user_name, code_sha256) lúc lưu; kiểm thì rẻ hơn.
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s[0:4]+"-"+s[4:8]+"-"+s[8:12]+"-"+s[12:16])
	}
	return out
}

// NormalizeRecovery bỏ khoảng trắng và '-', đổi chữ thường; không đúng 16 ký
// tự a-z2-7 thì trả "".
//
// Chỉ đổi chữ hoa ASCII: strings.ToLower đổi cả ký tự lạ (dấu Kelvin U+212A
// thành 'k'), làm nhiều chuỗi khác nhau thành cùng một mã.
func NormalizeRecovery(s string) string {
	out := make([]byte, 0, 16)
	for i := range len(s) {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '-':
			continue
		case c >= 'A' && c <= 'Z':
			c += 'a' - 'A'
		}
		if (c < 'a' || c > 'z') && (c < '2' || c > '7') {
			return ""
		}
		out = append(out, c)
	}
	if len(out) != 16 {
		return ""
	}
	return string(out)
}

// HashRecovery là sha256 (hex) của mã đã chuẩn hoá.
func HashRecovery(normalized string) string {
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

// Key là khoá AES-256 niêm phong bí mật TOTP (file /etc/tpan/secret.key).
type Key [32]byte

// KeyFile là chỗ của khoá trên máy chủ (0600 root, trong /etc/tpan 0700).
const KeyFile = "/etc/tpan/secret.key"

// ParseKey nhận nội dung file khoá: đúng 32 byte.
func ParseKey(b []byte) (Key, error) {
	var k Key
	if len(b) != len(k) {
		return Key{}, fmt.Errorf("file khoá dài %d byte, muốn đúng %d", len(b), len(k))
	}
	copy(k[:], b)
	return k, nil
}

// NewKey sinh khoá mới từ crypto/rand.
func NewKey() Key {
	var k Key
	_, _ = rand.Read(k[:])
	return k
}

// aad gắn bản mã với tên user: chép bản mã của A sang dòng của B thì Open lỗi.
func aad(user string) []byte { return []byte("tpan-totp-v1:" + user) }

// gcm dùng NewGCMWithRandomNonce: nó tự sinh nonce 12 byte và đặt trước bản
// mã (đúng dạng nonce || bản mã || tag 16 byte của mục 3.1), nên không có chỗ
// nào để người viết lỡ dùng lại nonce.
func gcm(k Key) (cipher.AEAD, error) {
	block, err := aes.NewCipher(k[:])
	if err != nil {
		return nil, fmt.Errorf("khoá AES: %w", err)
	}
	a, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return nil, fmt.Errorf("AES-GCM: %w", err)
	}
	return a, nil
}

// Seal niêm phong plain cho user: nonce 12 byte ngẫu nhiên || AES-256-GCM,
// AAD "tpan-totp-v1:"+user (chép bản mã sang user khác thì Open lỗi).
func Seal(k Key, user string, plain []byte) ([]byte, error) {
	a, err := gcm(k)
	if err != nil {
		return nil, err
	}
	return a.Seal(nil, nil, plain, aad(user)), nil
}

// errSealed không nói lý do cụ thể (sai khoá, sai user hay bị sửa): GCM cũng
// không phân biệt được, và người đọc log không cần hơn thế.
var errSealed = errors.New("không mở được bí mật hai bước (sai khoá, sai user hoặc dữ liệu hỏng)")

// Open mở bản đã niêm phong; sai khoá, sai user, bị sửa thì lỗi.
func Open(k Key, user string, sealed []byte) ([]byte, error) {
	a, err := gcm(k)
	if err != nil {
		return nil, err
	}
	// Ngắn hơn nonce + tag (28 byte) thì không thể là bản niêm phong nào.
	if len(sealed) < a.Overhead() {
		return nil, errSealed
	}
	plain, err := a.Open(nil, nil, sealed, aad(user))
	if err != nil {
		return nil, errSealed
	}
	return plain, nil
}
