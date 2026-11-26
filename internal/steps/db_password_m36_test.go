package steps

import (
	"testing"

	"github.com/doxuta/tpan/internal/system"
)

// M36: bước đổi mật khẩu, tạo lại user và tạo DB rời nhận mật khẩu tự đặt có
// dấu; không bao giờ nhận ký tự làm vỡ câu SQL ('...' gửi qua stdin).
func TestBuocNhanMatKhauTuDat(t *testing.T) {
	for pw, ok := range map[string]bool{
		"Ab3-kho/bi:mat@#":         true,
		"Ab3x9.y8~z7^w6%v":         true,
		"aB3dE5gH7jK9mN1pQ3sT5vX7": true, // mật khẩu tpan sinh (24+ chữ số)
		"co'nhay-1234567":          false,
		`co\gach-1234567`:          false,
		"co khoang 123456":         false,
		"co`backtick-1234":         false,
		"ngan-1A":                  false,
		"co;cham-phay-123":         false,
	} {
		steps := []interface{ validate() error }{
			&DBUserPassword{Name: "alice_shop", Password: system.NewSecret(pw)},
			&DBUserRecreate{Name: "alice_shop", Password: system.NewSecret(pw)},
			&StandaloneDB{Name: "alice_shop", Charset: "utf8mb4", Collation: "utf8mb4_unicode_ci", Password: system.NewSecret(pw)},
		}
		for _, s := range steps {
			if err := s.validate(); (err == nil) != ok {
				t.Errorf("%T %q: lỗi %v, muốn nhận=%v", s, pw, err, ok)
			}
		}
	}
}
