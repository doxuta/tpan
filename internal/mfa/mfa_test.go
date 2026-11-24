package mfa

import (
	"bytes"
	"encoding/base32"
	"net/url"
	"strings"
	"testing"
	"time"
)

// rfcSecret là bí mật thử của RFC 4226 phụ lục D và RFC 6238 phụ lục B (SHA-1).
var rfcSecret = []byte("12345678901234567890")

func TestHOTPVectorRFC4226(t *testing.T) {
	want := []string{"755224", "287082", "359152", "969429", "338314", "254676", "287922", "162583", "399871", "520489"}
	for i, w := range want {
		if got := HOTP(rfcSecret, uint64(i), 6); got != w {
			t.Errorf("HOTP(counter=%d) = %s, muốn %s", i, got, w)
		}
	}
}

func TestTOTPVectorRFC6238(t *testing.T) {
	cases := []struct {
		unix int64
		want string
	}{
		{59, "94287082"},
		{1111111109, "07081804"},
		{1111111111, "14050471"},
		{1234567890, "89005924"},
		{2000000000, "69279037"},
		{20000000000, "65353130"},
	}
	for _, c := range cases {
		if got := HOTP(rfcSecret, Step(time.Unix(c.unix, 0)), 8); got != c.want {
			t.Errorf("TOTP(T=%d) = %s, muốn %s", c.unix, got, c.want)
		}
	}
}

func TestStep(t *testing.T) {
	if s := Step(time.Unix(-100, 0)); s != 0 {
		t.Errorf("Step trước 1970 = %d, muốn 0", s)
	}
	if s := Step(time.Unix(59, 0)); s != 1 {
		t.Errorf("Step(59) = %d, muốn 1", s)
	}
	if s := Step(time.Unix(60, 0)); s != 2 {
		t.Errorf("Step(60) = %d, muốn 2", s)
	}
}

func TestIsCode(t *testing.T) {
	for _, s := range []string{"000000", "123456", "999999"} {
		if !IsCode(s) {
			t.Errorf("IsCode(%q) = false", s)
		}
	}
	// "١٢٣٤٥٦" là chữ số Ả Rập: unicode.IsDigit nhận, mã TOTP thì không.
	for _, s := range []string{"", "12345", "1234567", "12345a", " 12345", "١٢٣٤٥٦", "12 456", "-12345"} {
		if IsCode(s) {
			t.Errorf("IsCode(%q) = true", s)
		}
	}
}

func TestMatchCuaSo(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	cur := Step(now)
	for _, d := range []int64{-2, -1, 0, 1, 2} {
		code := HOTP(rfcSecret, uint64(int64(cur)+d), Digits)
		step, ok := Match(rfcSecret, code, now)
		inWindow := d >= -1 && d <= 1
		if ok != inWindow {
			t.Errorf("bước %+d: ok = %v, muốn %v", d, ok, inWindow)
			continue
		}
		if ok && step != uint64(int64(cur)+d) {
			t.Errorf("bước %+d: step = %d, muốn %d", d, step, int64(cur)+d)
		}
	}
	if _, ok := Match(rfcSecret, "12345", now); ok {
		t.Error("mã 5 số không được khớp")
	}
	if _, ok := Match(rfcSecret, "abcdef", now); ok {
		t.Error("mã có chữ không được khớp")
	}
}

func TestMatchBuocKhong(t *testing.T) {
	// Step(now) = 0: không có bước -1, không được tràn số thành bước 2^64-1.
	now := time.Unix(10, 0)
	if _, ok := Match(rfcSecret, HOTP(rfcSecret, 0, Digits), now); !ok {
		t.Error("mã bước 0 phải khớp")
	}
	if _, ok := Match(rfcSecret, HOTP(rfcSecret, ^uint64(0), Digits), now); ok {
		t.Error("bước 2^64-1 không được thử khi Step(now)=0")
	}
}

func TestMatchTraBuocLonNhat(t *testing.T) {
	// Tìm một bí mật mà hai bước liền nhau ra cùng mã (xác suất 1e-6 mỗi cặp)
	// thì tốn; thay vào đó kiểm hành vi bằng mã của bước +1: phải trả đúng +1.
	now := time.Unix(1_700_000_000, 0)
	step, ok := Match(rfcSecret, HOTP(rfcSecret, Step(now)+1, Digits), now)
	if !ok || step != Step(now)+1 {
		t.Errorf("Match = %d, %v; muốn %d, true", step, ok, Step(now)+1)
	}
}

// TestTOTPLechDongHo đo tỉ lệ máy chủ (đồng hồ lệch d giây) nhận mã mà app sinh
// đúng giờ, trên 3000 mốc liên tiếp. Bí mật và mốc bắt đầu cố định để kết quả
// không đổi giữa các lần chạy (mã của bước bên cạnh trùng ngẫu nhiên thì vẫn
// trùng y như vậy ở lần sau).
func TestTOTPLechDongHo(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	const n = 3000
	t.Logf("%8s  %8s", "lệch (s)", "nhận")
	for _, d := range []int{-90, -60, -45, -30, -15, 0, 15, 30, 45, 60, 90} {
		ok := 0
		for i := range n {
			app := start.Add(time.Duration(i) * time.Second)
			code := HOTP(rfcSecret, Step(app), Digits)
			if _, m := Match(rfcSecret, code, app.Add(time.Duration(d)*time.Second)); m {
				ok++
			}
		}
		rate := float64(ok) * 100 / n
		t.Logf("%8d  %7.1f%%", d, rate)
		switch {
		case d > -30 && d < 30 && ok != n:
			t.Errorf("lệch %d giây: nhận %.1f%%, muốn 100%%", d, rate)
		case (d >= 60 || d <= -60) && ok != 0:
			t.Errorf("lệch %d giây: nhận %.1f%%, muốn 0%%", d, rate)
		}
	}
}

func TestSecretVaEncode(t *testing.T) {
	a, b := NewSecret(), NewSecret()
	if len(a) != SecretBytes || bytes.Equal(a, b) {
		t.Fatalf("NewSecret: len %d, hai lần trùng nhau %v", len(a), bytes.Equal(a, b))
	}
	enc := EncodeSecret(a)
	if len(enc) != 32 || strings.ToUpper(enc) != enc || strings.Contains(enc, "=") {
		t.Errorf("EncodeSecret = %q, muốn 32 ký tự hoa không đệm", enc)
	}
	back, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(enc)
	if err != nil || !bytes.Equal(back, a) {
		t.Errorf("giải ngược EncodeSecret: %v", err)
	}
}

func TestURI(t *testing.T) {
	secret := rfcSecret
	raw := URI("TPAN example.com", "an@example.com", secret)
	if !strings.HasPrefix(raw, "otpauth://totp/TPAN%20example.com:an@example.com?") {
		t.Fatalf("URI = %q", raw)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "otpauth" || u.Host != "totp" || u.Path != "/TPAN example.com:an@example.com" {
		t.Errorf("scheme %q host %q path %q", u.Scheme, u.Host, u.Path)
	}
	q := u.Query()
	want := map[string]string{"secret": EncodeSecret(secret), "issuer": "TPAN example.com", "algorithm": "SHA1", "digits": "6", "period": "30"}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("tham số %s = %q, muốn %q", k, q.Get(k), v)
		}
	}
	if len(q) != len(want) {
		t.Errorf("thừa tham số: %v", q)
	}
	// Dấu cách là %20 (không phải '+'): vài app đọc '+' thành chữ '+'.
	if strings.Contains(raw, "+") || !strings.Contains(raw, "issuer=TPAN%20example.com") {
		t.Errorf("URI mã hoá dấu cách sai: %q", raw)
	}
	// ':' trong nhãn (tên máy là IPv6) không được làm lệch chỗ tách issuer:account.
	if raw := URI("a:b", "c/d+e", secret); !strings.HasPrefix(raw, "otpauth://totp/a%3Ab:c%2Fd+e?") || !strings.Contains(raw, "issuer=a%3Ab&") {
		t.Errorf("URI có ký tự đặc biệt = %q", raw)
	}
}

func TestQR(t *testing.T) {
	m, err := QR(URI("TPAN", "admin", rfcSecret))
	if err != nil {
		t.Fatal(err)
	}
	n := len(m)
	if n < 21 || (n-17)%4 != 0 || (n-17)/4 > 40 {
		t.Fatalf("cạnh %d không phải 17+4×version", n)
	}
	for y, row := range m {
		if len(row) != n || strings.Trim(row, "01") != "" {
			t.Fatalf("hàng %d: dài %d, muốn %d ký tự '0'/'1'", y, len(row), n)
		}
	}
	dark := func(x, y int) bool { return m[y][x] == '1' }
	// Ô định vị: viền 7×7 tối, vòng trong sáng, lõi 3×3 tối; ở ba góc trên trái,
	// trên phải, dưới trái (không kèm vùng trắng nên chạm đúng mép).
	for _, c := range [][2]int{{0, 0}, {n - 7, 0}, {0, n - 7}} {
		for dy := range 7 {
			for dx := range 7 {
				ring := max(abs(dx-3), abs(dy-3))
				want := ring != 2
				if dark(c[0]+dx, c[1]+dy) != want {
					t.Fatalf("ô định vị ở (%d,%d): module (%d,%d) tối=%v, muốn %v", c[0], c[1], dx, dy, !want, want)
				}
			}
		}
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func TestMaDuPhong(t *testing.T) {
	codes := NewRecoveryCodes()
	if len(codes) != RecoveryCount {
		t.Fatalf("%d mã, muốn %d", len(codes), RecoveryCount)
	}
	seen := map[string]bool{}
	for _, c := range codes {
		if len(c) != 19 || c[4] != '-' || c[9] != '-' || c[14] != '-' {
			t.Errorf("mã %q sai dạng xxxx-xxxx-xxxx-xxxx", c)
		}
		n := NormalizeRecovery(c)
		if len(n) != 16 || strings.Trim(n, "abcdefghijklmnopqrstuvwxyz234567") != "" {
			t.Errorf("NormalizeRecovery(%q) = %q", c, n)
		}
		if seen[n] {
			t.Errorf("mã trùng %q", c)
		}
		seen[n] = true
	}
}

func TestNormalizeRecovery(t *testing.T) {
	const want = "abcd2345efgh6723"
	for _, in := range []string{"abcd-2345-efgh-6723", "ABCD 2345 EFGH 6723", " abcd\t2345-efgh 6723\n", want} {
		if got := NormalizeRecovery(in); got != want {
			t.Errorf("NormalizeRecovery(%q) = %q, muốn %q", in, got, want)
		}
	}
	bad := []string{
		"", "abcd-2345-efgh-672", "abcd-2345-efgh-67234",
		"abcd-2345-efgh-6721", // '1' không thuộc base32
		"abcd-2345-efgh-672é",
		"abcd-2345-efgh-672K", // dấu Kelvin: strings.ToLower đổi thành 'k' ASCII
		"abcd_2345_efgh_6723",
	}
	for _, in := range bad {
		if got := NormalizeRecovery(in); got != "" {
			t.Errorf("NormalizeRecovery(%q) = %q, muốn rỗng", in, got)
		}
	}
}

func TestHashRecovery(t *testing.T) {
	// sha256("abc"), để chắc là hex chữ thường của sha256 trần.
	if got := HashRecovery("abc"); got != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Errorf("HashRecovery = %s", got)
	}
}

func TestNiemPhong(t *testing.T) {
	k := NewKey()
	plain := NewSecret()
	sealed, err := Seal(k, "an", plain)
	if err != nil {
		t.Fatal(err)
	}
	if len(sealed) != 12+len(plain)+16 || bytes.Contains(sealed, plain) {
		t.Fatalf("bản niêm phong dài %d hoặc chứa bí mật rõ", len(sealed))
	}
	got, err := Open(k, "an", sealed)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("Open = %x, %v", got, err)
	}
	again, _ := Seal(k, "an", plain)
	if bytes.Equal(again, sealed) {
		t.Error("hai lần Seal cùng nonce")
	}
	if _, err := Open(k, "binh", sealed); err == nil {
		t.Error("mở bằng user khác phải lỗi")
	}
	for i := range sealed {
		bad := bytes.Clone(sealed)
		bad[i] ^= 1
		if _, err := Open(k, "an", bad); err == nil {
			t.Fatalf("sửa byte %d mà vẫn mở được", i)
		}
	}
	if _, err := Open(NewKey(), "an", sealed); err == nil {
		t.Error("khoá khác phải lỗi")
	}
	for _, n := range []int{0, 11, 12, 27} {
		if _, err := Open(k, "an", sealed[:n]); err == nil {
			t.Errorf("bản dài %d byte phải lỗi", n)
		}
	}
}

func TestKey(t *testing.T) {
	if a, b := NewKey(), NewKey(); a == b || a == (Key{}) {
		t.Error("NewKey không ngẫu nhiên")
	}
	raw := bytes.Repeat([]byte{7}, 32)
	k, err := ParseKey(raw)
	if err != nil || !bytes.Equal(k[:], raw) {
		t.Errorf("ParseKey 32 byte: %v", err)
	}
	for _, n := range []int{0, 31, 33, 64} {
		if _, err := ParseKey(make([]byte, n)); err == nil {
			t.Errorf("ParseKey %d byte phải lỗi", n)
		}
	}
}
