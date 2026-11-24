package demo

// Phần M20 của máy giả (mục 3.11 và 11.12 R8 của docs/thay-doi/M20-thiet-ke.md):
// hai bước, token API, tên miền và chứng chỉ của panel xem được ở chế độ --demo.
//
//   - khach_a đã bật hai bước: bí mật niêm phong bằng khoá demo, 10 mã dự phòng
//     (một mã đã dùng);
//   - admin có hai token (một đang dùng, dùng lần cuối từ 192.0.2.10; một đã
//     thu hồi), khach_a có một token sites:write;
//   - panel.domain = panel.example.com, kèm chứng chỉ giả (tự ký, còn 60 ngày)
//     ở lineage tpan-panel của máy giả.
//
// Khác các phần seed khác: SeedM20 KHÔNG nằm trong New. `tpan serve --demo` đặt
// lại mật khẩu các user mẫu sau New, mà SetUserPassword thu hồi token của user
// (mục 11.5), nên token mẫu chỉ có nghĩa nếu tạo sau vòng đó. root.go gọi
// SeedM20 ngay sau vòng đặt mật khẩu.

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/apitoken"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/mfa"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

const (
	// panelDomain là tên miền demo của panel (tên dành riêng cho ví dụ, RFC 2606).
	panelDomain = "panel.example.com"
	// panelCertAge: chứng chỉ cấp cách đây 30 ngày trong 90 ngày, còn 60 ngày.
	panelCertAge = 30 * 24 * time.Hour
	// demoTokenIP là IP của lần dùng cuối của token đang chạy (TEST-NET-1, RFC 5737).
	demoTokenIP = "192.0.2.10"
)

// SecretKeyPath là đường dẫn file khoá niêm phong bí mật hai bước của demo.
// File nằm TRONG máy giả (SeedM20 ghi bằng sys.SetFile, 0600 root, đúng đường
// dẫn như máy thật), không phải file thật trên máy dev: server đọc nó qua
// Deps.Sys như trên máy thật, nên không cần thêm đường khoá riêng cho demo.
func SecretKeyPath() string { return mfa.KeyFile }

// SeedM20 ghi dữ liệu M20 của demo và trả bí mật hai bước của khach_a (base32
// chữ hoa) để `tpan serve --demo` in ra: ai muốn xem màn bước hai thì nạp nó
// vào app xác thực. Gọi SAU khi đã đặt mật khẩu cho admin và các user mẫu.
func SeedM20(ctx context.Context, st *store.Store, sys *system.FakeSystem) (khachASecret string, err error) {
	now := time.Now()

	// Khoá niêm phong, như server tạo ở lần bật đầu tiên (mục 11.6).
	key := mfa.NewKey()
	sys.SetDir("/etc/tpan", 0o700, system.Root)
	sys.SetFile(mfa.KeyFile, key[:], 0o600, system.Root)

	secret := mfa.NewSecret()
	sealed, err := mfa.Seal(key, userA, secret)
	if err != nil {
		return "", fmt.Errorf("niêm phong bí mật hai bước của %s: %w", userA, err)
	}
	enabledAt := now.Add(-3 * 24 * time.Hour)
	if err := st.SetPendingTOTP(ctx, userA, sealed, enabledAt); err != nil {
		return "", fmt.Errorf("ghi bí mật chờ của %s: %w", userA, err)
	}
	codes := mfa.NewRecoveryCodes()
	hashes := make([]string, len(codes))
	for i, c := range codes {
		hashes[i] = mfa.HashRecovery(mfa.NormalizeRecovery(c))
	}
	// Bước 0 (không phải bước hiện tại): mã đang hiện trong app vẫn dùng được
	// ngay, không bị coi là đã dùng.
	if err := st.EnableTOTP(ctx, userA, sealed, 0, hashes, "", enabledAt); err != nil {
		return "", fmt.Errorf("bật hai bước cho %s: %w", userA, err)
	}
	if ok, err := st.UseRecoveryCode(ctx, userA, hashes[0], now.Add(-24*time.Hour)); err != nil || !ok {
		return "", fmt.Errorf("đánh dấu một mã dự phòng của %s đã dùng: %v (nhận=%v)", userA, err, ok)
	}

	if err := seedTokens(ctx, st, now); err != nil {
		return "", err
	}
	if err := seedPanelDomain(ctx, st, sys, now); err != nil {
		return "", err
	}
	return mfa.EncodeSecret(secret), nil
}

// seedTokens tạo ba token mẫu. Token thật sinh ngẫu nhiên rồi bỏ: demo chỉ cần
// danh sách (hint, phạm vi, hạn, lần dùng cuối), không ai gọi API bằng chúng.
func seedTokens(ctx context.Context, st *store.Store, now time.Time) error {
	for _, t := range []struct {
		user, name string
		scopes     []string
		created    time.Duration // cách đây
		days       int
		used       time.Duration // lần dùng cuối cách đây; 0 là chưa dùng
		revoked    bool
	}{
		{account.AdminName, "Giám sát", []string{"sites:read", "jobs:read"}, 12 * 24 * time.Hour, 90, 2 * time.Hour, false},
		{account.AdminName, "Sao lưu cũ", []string{"backups:write"}, 40 * 24 * time.Hour, 90, 20 * 24 * time.Hour, true},
		{userA, "Triển khai site", []string{"sites:write"}, 5 * 24 * time.Hour, 30, 0, false},
	} {
		sc, err := apitoken.ParseScopes(t.scopes)
		if err != nil {
			return fmt.Errorf("phạm vi token mẫu %q: %w", t.name, err)
		}
		var id [8]byte
		_, _ = rand.Read(id[:])
		tok := apitoken.New()
		created := now.Add(-t.created)
		row := store.TokenRow{ID: hex.EncodeToString(id[:]), User: t.user, Name: t.name, Scopes: sc.String(), Hint: apitoken.Hint(tok),
			Created: created, Expires: created.AddDate(0, 0, t.days)}
		if t.revoked {
			row.Revoked = now.Add(-18 * 24 * time.Hour)
		}
		if t.used > 0 {
			row.LastUsed, row.LastIP = now.Add(-t.used), demoTokenIP
		}
		if err := st.CreateToken(ctx, row, apitoken.Hash(tok)); err != nil {
			return fmt.Errorf("tạo token mẫu %q: %w", t.name, err)
		}
	}
	return nil
}

// seedPanelDomain đặt panel.domain và ghi bộ file certbot của lineage
// tpan-panel (chứng chỉ, khoá, file gia hạn) như sau một Plan `panel.domain`
// đã chạy. Bên ký mang tên giống Let's Encrypt (không phải staging), để phép
// kiểm tpan.panelcert đạt thay vì cảnh báo chứng chỉ thử.
func seedPanelDomain(ctx context.Context, st *store.Store, sys *system.FakeSystem, now time.Time) error {
	if err := st.SetSetting(ctx, core.SettingPanelDomain, panelDomain); err != nil {
		return fmt.Errorf("ghi setting %s: %w", core.SettingPanelDomain, err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("sinh khoá cho chứng chỉ giả của panel: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return fmt.Errorf("sinh số serial cho chứng chỉ giả của panel: %w", err)
	}
	notBefore := now.Add(-panelCertAge)
	leaf := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: panelDomain},
		DNSNames:     []string{panelDomain},
		NotBefore:    notBefore,
		NotAfter:     notBefore.Add(certLife),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	// parent chỉ để đặt tên bên ký; chứng chỉ ký bằng chính khoá của nó.
	parent := &x509.Certificate{Subject: pkix.Name{Country: []string{"US"}, Organization: []string{"Let's Encrypt"}, CommonName: "E6"}}
	der, err := x509.CreateCertificate(rand.Reader, leaf, parent, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("tạo chứng chỉ giả của panel: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return fmt.Errorf("mã hoá khoá của chứng chỉ giả của panel: %w", err)
	}
	live := core.PanelCertDir
	sys.SetDir(live, 0o755, system.Root)
	sys.SetFile(live+"/fullchain.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644, system.Root)
	sys.SetFile(live+"/privkey.pem", pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600, system.Root)
	renewal := "# renew_before_expiry = 30 days\nversion = 2.9.0\narchive_dir = /etc/letsencrypt/archive/" + core.PanelLineage +
		"\nfullchain = " + live + "/fullchain.pem\nprivkey = " + live + "/privkey.pem\n\n[renewalparams]\nauthenticator = webroot\nwebroot_path = " +
		acmeRoot + ",\nserver = https://acme-v02.api.letsencrypt.org/directory\nkey_type = ecdsa\n[[webroot_map]]\n" + panelDomain + " = " + acmeRoot + "\n"
	sys.SetFile("/etc/letsencrypt/renewal/"+core.PanelLineage+".conf", []byte(renewal), 0o644, system.Root)
	return nil
}
