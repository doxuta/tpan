package main

import (
	"encoding/base32"
	"testing"
	"time"
)

// TestHOTPVectorRFC4226 so hotp của bộ m20-that với vector ở phụ lục D của
// RFC 4226, và một mốc của RFC 6238 phụ lục B (SHA1, T=59 → 94287082, sáu số
// cuối 287082), để mã trình duyệt gõ vào panel không sai vì chính đồ nghề.
func TestHOTPVectorRFC4226(t *testing.T) {
	key := []byte("12345678901234567890")
	want := []string{"755224", "287082", "359152", "969429", "338314", "254676", "287922", "162583", "399871", "520489"}
	for i, w := range want {
		if got := hotp(key, uint64(i)); got != w {
			t.Errorf("hotp(counter %d) = %s, muốn %s", i, got, w)
		}
	}
	b32 := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(key)
	k, err := totpKey(b32)
	if err != nil {
		t.Fatal(err)
	}
	if got := hotp(k, totpStep(time.Unix(59, 0))); got != "287082" {
		t.Errorf("TOTP T=59 = %s, muốn 287082", got)
	}
	if c := wrongCode(serverClock{}, k); c == hotp(k, totpStep(time.Now())) {
		t.Errorf("wrongCode trả đúng mã hiện tại")
	}
}
