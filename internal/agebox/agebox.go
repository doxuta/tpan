package agebox

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"filippo.io/age"

	"github.com/doxuta/tpan/internal/system"
)

var keyRe = regexp.MustCompile(`^tpan-key-1-[a-z2-7]{52}$`)

// b32 là base32 RFC 4648 không đệm: 32 byte ra đúng 52 ký tự, toàn chữ và số
// nên khoá chép tay, dán vào terminal hay đặt trong YAML đều không cần nháy.
var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// errBadKey cố ý không nhắc gì tới giá trị khoá: lỗi có thể đi vào log job.
var errBadKey = errors.New("agebox: khoá sao lưu sai dạng (cần tpan-key-1- và 52 ký tự a-z, 2-7)")

// GenerateKey sinh khoá mới từ 32 byte crypto/rand (256 bit).
func GenerateKey() (system.Secret, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return system.Secret{}, fmt.Errorf("agebox: đọc số ngẫu nhiên: %w", err)
	}
	return system.NewSecret(KeyPrefix + strings.ToLower(b32.EncodeToString(b[:]))), nil
}

// CheckKey kiểm dạng khoá. Lỗi không chép lại khoá.
func CheckKey(key system.Secret) error {
	if !keyRe.MatchString(key.Reveal()) {
		return errBadKey
	}
	return nil
}

// Fingerprint là 8 hex đầu của sha256 chuỗi khoá: đủ để người quản trị so hai
// bản chép có cùng khoá không, mà không lộ gì về khoá.
func Fingerprint(key system.Secret) string {
	sum := sha256.Sum256([]byte(key.Reveal()))
	return hex.EncodeToString(sum[:4])
}

// Encrypt mở luồng mã hoá vào dst. Kiểm dạng khoá trước, vì age nhận mọi chuỗi
// làm mật khẩu: một khoá rỗng hay cắt cụt vẫn "mã hoá được" rồi không ai mở nổi.
func Encrypt(dst io.Writer, key system.Secret) (io.WriteCloser, error) {
	if err := CheckKey(key); err != nil {
		return nil, err
	}
	r, err := age.NewScryptRecipient(key.Reveal())
	if err != nil {
		return nil, fmt.Errorf("agebox: tạo người nhận: %w", err)
	}
	r.SetWorkFactor(WorkFactor)
	w, err := age.Encrypt(dst, r)
	if err != nil {
		return nil, fmt.Errorf("agebox: mở luồng mã hoá: %w", err)
	}
	return w, nil
}

// Decrypt mở luồng giải mã từ src. SetMaxWorkFactor để file do kẻ ghi được vào
// bucket đặt hệ số công rất cao bị từ chối ngay ở header, trước khi scrypt ăn
// hết RAM. Bản rõ chỉ đáng tin khi đã đọc tới io.EOF không lỗi (xem types.go).
func Decrypt(src io.Reader, key system.Secret) (io.Reader, error) {
	if err := CheckKey(key); err != nil {
		return nil, err
	}
	id, err := age.NewScryptIdentity(key.Reveal())
	if err != nil {
		return nil, fmt.Errorf("agebox: tạo danh tính: %w", err)
	}
	id.SetMaxWorkFactor(WorkFactor)
	r, err := age.Decrypt(src, id)
	if err != nil {
		return nil, fmt.Errorf("agebox: giải mã: %w", err)
	}
	return r, nil
}
