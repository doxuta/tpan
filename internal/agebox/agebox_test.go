package agebox

import (
	"bytes"
	"crypto/rand"
	"io"
	"strings"
	"testing"

	"filippo.io/age"

	"github.com/doxuta/tpan/internal/system"
)

func newKey(t *testing.T) system.Secret {
	t.Helper()
	k, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func randBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

func encrypt(t *testing.T, key system.Secret, plain []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := Encrypt(&buf, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(plain); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// noKey khẳng định lỗi không chép lại khoá (lỗi có thể vào log job).
func noKey(t *testing.T, err error, key system.Secret) {
	t.Helper()
	if strings.Contains(err.Error(), key.Reveal()) {
		t.Fatalf("lỗi chép lại khoá: %v", err)
	}
}

func TestKhoaDungDang(t *testing.T) {
	a, b := newKey(t), newKey(t)
	for _, k := range []system.Secret{a, b} {
		if err := CheckKey(k); err != nil {
			t.Fatalf("khoá vừa sinh không qua CheckKey: %v", err)
		}
		if len(k.Reveal()) != len(KeyPrefix)+52 {
			t.Fatalf("độ dài khoá %d", len(k.Reveal()))
		}
	}
	if a.Reveal() == b.Reveal() {
		t.Fatal("hai khoá sinh liền nhau trùng nhau")
	}
	if len(Fingerprint(a)) != 8 {
		t.Fatalf("fingerprint sai độ dài: %q", Fingerprint(a))
	}
	if Fingerprint(a) == Fingerprint(b) {
		t.Fatal("hai khoá khác nhau cùng fingerprint")
	}
	// Giá trị tính độc lập bằng `shasum -a 256`. Đổi cách tính fingerprint là đổi thứ người quản trị đã ghi lại.
	fixed := system.NewSecret(KeyPrefix + strings.Repeat("a", 52))
	if got := Fingerprint(fixed); got != "6496ef46" {
		t.Fatalf("fingerprint của khoá cố định = %q", got)
	}
}

func TestCheckKeyTuChoi(t *testing.T) {
	good := strings.Repeat("a", 52)
	for _, s := range []string{
		"",
		KeyPrefix,
		KeyPrefix + good[:51],
		KeyPrefix + good + "a",
		KeyPrefix + strings.ToUpper(good),
		KeyPrefix + good[:51] + "1", // 1 không thuộc bảng base32
		KeyPrefix + good[:51] + "=",
		"tpan-key-2-" + good,
		KeyPrefix + good + "\n",
		" " + KeyPrefix + good,
	} {
		k := system.NewSecret(s)
		err := CheckKey(k)
		if err == nil {
			t.Errorf("CheckKey nhận %q", s)
			continue
		}
		if len(s) > len(KeyPrefix) {
			noKey(t, err, k)
		}
	}
	if _, err := Encrypt(io.Discard, system.NewSecret("hunter2")); err == nil {
		t.Error("Encrypt nhận khoá sai dạng")
	}
	if _, err := Decrypt(strings.NewReader(""), system.NewSecret("hunter2")); err == nil {
		t.Error("Decrypt nhận khoá sai dạng")
	}
}

func TestVongMaHoaGiaiMa(t *testing.T) {
	key := newKey(t)
	plain := randBytes(t, 5<<20)
	ct := encrypt(t, key, plain)
	if !bytes.HasPrefix(ct, []byte("age-encryption.org/v1\n-> scrypt ")) {
		t.Fatalf("không phải khuôn age scrypt: %q", ct[:40])
	}
	r, err := Decrypt(bytes.NewReader(ct), key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatal("bản giải mã khác bản rõ")
	}
}

func TestSuaMotByteThiLoi(t *testing.T) {
	key := newKey(t)
	ct := encrypt(t, key, randBytes(t, 300<<10)) // năm khối 64 KiB
	ct[len(ct)/2] ^= 0x01                        // giữa phần thân, xa header
	r, err := Decrypt(bytes.NewReader(ct), key)
	if err != nil {
		t.Fatalf("header còn nguyên mà Decrypt lỗi: %v", err)
	}
	_, err = io.ReadAll(r)
	if err == nil {
		t.Fatal("bản mã bị sửa vẫn đọc tới EOF không lỗi")
	}
	noKey(t, err, key)
}

func TestSaiKhoaThiLoi(t *testing.T) {
	key, other := newKey(t), newKey(t)
	ct := encrypt(t, key, []byte("tpan.db"))
	_, err := Decrypt(bytes.NewReader(ct), other)
	if err == nil {
		t.Fatal("khoá khác vẫn giải mã được")
	}
	noKey(t, err, key)
	noKey(t, err, other)
}

func TestHeSoCongCaoBiTuChoi(t *testing.T) {
	key := newKey(t)
	rc, err := age.NewScryptRecipient(key.Reveal())
	if err != nil {
		t.Fatal(err)
	}
	rc.SetWorkFactor(18) // scrypt N=2^18: khoảng 256 MiB lúc mã hoá
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, rc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("gói giả")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	// Khoá đúng mà vẫn phải bị từ chối, và từ chối vì hệ số công (không phải sai
	// khoá), ngay ở Decrypt trước khi chạy scrypt.
	_, err = Decrypt(bytes.NewReader(buf.Bytes()), key)
	if err == nil {
		t.Fatal("file hệ số công 18 vẫn được giải mã")
	}
	if !strings.Contains(err.Error(), "work factor") {
		t.Fatalf("lỗi không phải vì hệ số công: %v", err)
	}
	noKey(t, err, key)
}

func TestCatCutThiLoi(t *testing.T) {
	key := newKey(t)
	plain := randBytes(t, 300<<10)
	ct := encrypt(t, key, plain)
	ct = ct[:len(ct)-1000] // mất khối cuối
	r, err := Decrypt(bytes.NewReader(ct), key)
	if err != nil {
		t.Fatalf("header còn nguyên mà Decrypt lỗi: %v", err)
	}
	got, err := io.ReadAll(r)
	if err == nil {
		t.Fatal("bản mã bị cắt cụt vẫn đọc tới EOF không lỗi")
	}
	noKey(t, err, key)
	// Đúng lời cảnh báo ở types.go: luồng đã nhả phần đầu hợp lệ trước khi lỗi,
	// nên người gọi không được dùng bản rõ trước khi gặp io.EOF.
	if len(got) == 0 || !bytes.Equal(got, plain[:len(got)]) {
		t.Fatalf("mong có phần đầu hợp lệ trước lỗi, được %d byte", len(got))
	}
}
