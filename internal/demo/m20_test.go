package demo

import (
	"context"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/mfa"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

// seedLikeServe đi đúng thứ tự của `tpan serve --demo` (root.go): New, mật khẩu
// admin, mật khẩu từng user mẫu (thu hồi token), rồi SeedM20 (R8).
func seedLikeServe(t *testing.T) (context.Context, *system.FakeSystem, *store.Store, string) {
	t.Helper()
	ctx, sys, st := newDemo(t)
	if err := st.SetAdminPasswordHash(ctx, "$demo"); err != nil {
		t.Fatal(err)
	}
	for _, u := range Users() {
		if err := st.SetUserPassword(ctx, u, "$demo"); err != nil {
			t.Fatal(err)
		}
	}
	secret, err := SeedM20(ctx, st, sys)
	if err != nil {
		t.Fatalf("SeedM20: %v", err)
	}
	return ctx, sys, st, secret
}

// R8: token mẫu còn sống sau vòng đặt mật khẩu của root.go.
func TestM20TokenSauVongMatKhau(t *testing.T) {
	ctx, _, st, _ := seedLikeServe(t)
	now := time.Now()

	toks, err := st.Tokens(ctx, account.AdminName)
	if err != nil || len(toks) != 2 {
		t.Fatalf("token của admin: %d, %v", len(toks), err)
	}
	var active, revoked int
	for _, tk := range toks {
		switch {
		case !tk.Revoked.IsZero():
			revoked++
		case tk.Expires.After(now):
			active++
			if tk.Scopes != "sites:read,jobs:read" || tk.LastIP != "192.0.2.10" || tk.LastUsed.IsZero() {
				t.Errorf("token đang dùng: %+v", tk)
			}
		}
	}
	if active != 1 || revoked != 1 {
		t.Errorf("admin: %d đang dùng, %d thu hồi; muốn 1 và 1", active, revoked)
	}
	if n, err := st.ActiveTokens(ctx, userA, now); err != nil || n != 1 {
		t.Errorf("token của khach_a còn sống = %d, %v; muốn 1", n, err)
	}
	if n, err := st.ActiveTokens(ctx, userB, now); err != nil || n != 0 {
		t.Errorf("khach_b có %d token, %v", n, err)
	}
	ks, _ := st.Tokens(ctx, userA)
	if len(ks) != 1 || ks[0].Scopes != "sites:write" {
		t.Errorf("token của khach_a: %+v", ks)
	}
}

// khach_a bật hai bước: bí mật in ra mở được bằng khoá trong máy giả và sinh
// mã được nhận; 10 mã dự phòng, còn 9.
func TestM20HaiBuocKhachA(t *testing.T) {
	ctx, sys, st, secretB32 := seedLikeServe(t)

	row, ok, err := st.TOTP(ctx, userA)
	if err != nil || !ok || row.Enabled.IsZero() {
		t.Fatalf("TOTP khach_a: %+v ok=%v err=%v", row, ok, err)
	}
	raw, err := sys.ReadFile(SecretKeyPath())
	if err != nil {
		t.Fatalf("đọc khoá demo: %v", err)
	}
	key, err := mfa.ParseKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := mfa.Open(key, userA, row.SecretEnc)
	if err != nil {
		t.Fatalf("mở bí mật: %v", err)
	}
	if got := mfa.EncodeSecret(secret); got != secretB32 {
		t.Errorf("bí mật in ra %q khác bí mật đã niêm phong %q", secretB32, got)
	}
	// Mã đang hiện trong app dùng được ngay (last_step của demo là 0).
	now := time.Now()
	step, ok := mfa.Match(secret, mfa.HOTP(secret, mfa.Step(now), mfa.Digits), now)
	if !ok {
		t.Fatal("mã của bí mật demo không khớp")
	}
	if used, err := st.UseTOTPStep(ctx, userA, step); err != nil || !used {
		t.Errorf("UseTOTPStep = %v, %v; muốn nhận mã", used, err)
	}
	if n, err := st.RecoveryLeft(ctx, userA); err != nil || n != mfa.RecoveryCount-1 {
		t.Errorf("mã dự phòng còn %d, %v; muốn %d", n, err, mfa.RecoveryCount-1)
	}
	if on, err := st.TOTPEnabled(ctx, userB); err != nil || on {
		t.Errorf("khach_b bật hai bước = %v, %v", on, err)
	}
	// Phép kiểm file khoá của health đạt: có người bật hai bước và file đúng.
	anyOn := func(context.Context) (bool, error) { return true, nil }
	if r := checks.SecretKey(mfa.KeyFile, anyOn).Run(ctx, sys); r.Status != checks.Pass {
		t.Errorf("tpan.secretkey = %s: %s", r.Status, r.Summary.Vi)
	}
}

// panel.domain và chứng chỉ giả: còn khoảng 60 ngày, đúng tên, không phải staging.
func TestM20TenMienPanel(t *testing.T) {
	ctx, sys, st, _ := seedLikeServe(t)

	d, ok, err := (&core.Panel{Settings: st}).Domain(ctx)
	if err != nil || !ok || d.String() != "panel.example.com" {
		t.Fatalf("Domain = %q ok=%v err=%v", d, ok, err)
	}
	r := checks.PanelACMECert(d, time.Now).Run(ctx, sys)
	if r.Status != checks.Pass {
		t.Fatalf("tpan.panelcert = %s: %s", r.Status, r.Summary.Vi)
	}
	if _, err := sys.ReadFile(core.PanelCertDir + "/privkey.pem"); err != nil {
		t.Errorf("thiếu khoá của chứng chỉ panel: %v", err)
	}
	if _, err := sys.ReadFile("/etc/letsencrypt/renewal/tpan-panel.conf"); err != nil {
		t.Errorf("thiếu file gia hạn của lineage: %v", err)
	}
}
