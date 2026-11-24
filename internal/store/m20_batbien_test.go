package store

// Bất biến của M20 ở tầng store (docs/thay-doi/M20-thiet-ke.md mục 4, 11):
// B2 (mã không dùng hai lần, kể cả khi đua), B3 (không lộ bí mật trong DB),
// B6 (nâng cấp không đăng xuất ai), B7 (xoá user xoá hết), 11.5, 11.8.

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/apitoken"
	"github.com/doxuta/tpan/internal/mfa"
)

// race chạy fn ở 20 goroutine cùng lúc (cùng chờ một cổng rồi chạy), nửa qua
// s1, nửa qua s2 (hai Store mở cùng một file: như `tpan serve` và một lệnh
// CLI chạy song song), và đòi đúng một lần trả true, không lần nào lỗi.
func race(t *testing.T, s1, s2 *Store, fn func(*Store) (bool, error)) {
	t.Helper()
	var (
		wins, errs atomic.Int32
		wg         sync.WaitGroup
		gate       = make(chan struct{})
	)
	for i := range 20 {
		s := s1
		if i%2 == 1 {
			s = s2
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-gate
			ok, err := fn(s)
			if err != nil {
				errs.Add(1)
				t.Errorf("goroutine %d: %v", i, err)
			}
			if ok {
				wins.Add(1)
			}
		}()
	}
	close(gate)
	wg.Wait()
	if wins.Load() != 1 || errs.Load() != 0 {
		t.Errorf("thắng %d lần, lỗi %d lần; muốn đúng 1 thắng, 0 lỗi", wins.Load(), errs.Load())
	}
}

// twoStores mở hai Store trên cùng một file DB có admin và "an".
func twoStores(t *testing.T) (*Store, *Store) {
	t.Helper()
	s1, path := openTemp(t)
	addUser(t, s1, "admin", account.RoleAdmin)
	addUser(t, s1, "an", account.RoleUser)
	s2, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s2.Close() })
	return s1, s2
}

func TestHaiRequestCungMaChiMotThang(t *testing.T) {
	ctx := context.Background()

	t.Run("UseTOTPStep", func(t *testing.T) {
		s1, s2 := twoStores(t)
		enableAn(t, s1)
		race(t, s1, s2, func(s *Store) (bool, error) { return s.UseTOTPStep(ctx, "an", 500) })
	})
	t.Run("UseRecoveryCode", func(t *testing.T) {
		s1, s2 := twoStores(t)
		enableAn(t, s1)
		race(t, s1, s2, func(s *Store) (bool, error) { return s.UseRecoveryCode(ctx, "an", "h2", t0) })
		if n, _ := s1.RecoveryLeft(ctx, "an"); n != 2 {
			t.Errorf("RecoveryLeft = %d, muốn 2", n)
		}
	})
	t.Run("ConsumeChallenge", func(t *testing.T) {
		s1, s2 := twoStores(t)
		addChallenge(t, s1, "ch", "an")
		race(t, s1, s2, func(s *Store) (bool, error) { return s.ConsumeChallenge(ctx, "ch") })
	})
	// RecordTOTPFailure đọc-tính-ghi trong một transaction BEGIN IMMEDIATE:
	// 20 lần sai song song (max 10) phải khoá đúng một lần, không mất lần đếm
	// nào, không lỗi "database is locked"; 10 lần sau rơi vào lúc đang khoá.
	t.Run("RecordTOTPFailure", func(t *testing.T) {
		s1, s2 := twoStores(t)
		enableAn(t, s1)
		race(t, s1, s2, func(s *Store) (bool, error) {
			locked, _, _, err := s.RecordTOTPFailure(ctx, "an", t0, 10, 15*time.Minute, 24*time.Hour)
			return locked, err
		})
		row, _, _ := s1.TOTP(ctx, "an")
		if row.Failures != 0 || row.LockCount != 1 || !row.LockedUntil.Equal(t0.Add(15*time.Minute)) {
			t.Errorf("sau 20 lần sai song song = %+v", row)
		}
	})
	t.Run("EnableTOTP", func(t *testing.T) {
		s1, s2 := twoStores(t)
		must(t, s1.SetPendingTOTP(ctx, "an", []byte("A"), t0))
		race(t, s1, s2, func(s *Store) (bool, error) {
			err := s.EnableTOTP(ctx, "an", []byte("A"), 7, []string{"h"}, "", t0)
			if errors.Is(err, ErrTOTPNotPending) {
				return false, nil
			}
			return err == nil, err
		})
		if n, _ := s1.RecoveryLeft(ctx, "an"); n != 1 {
			t.Errorf("RecoveryLeft = %d, muốn 1", n)
		}
	})
}

// Mục 11.5: người dùng so mã với bí mật A; trong lúc đó một lần setup khác
// (tab thứ hai, hay kẻ có phiên) ghi bí mật B. Enable(A) không được bật B, thứ
// người dùng chưa từng quét.
func TestDuaSetupVoiEnable(t *testing.T) {
	s := m20Store(t)
	ctx := context.Background()
	must(t, s.SetPendingTOTP(ctx, "an", []byte("A"), t0))
	row, _, _ := s.TOTP(ctx, "an") // handler đọc bí mật chờ để so mã
	must(t, s.SetPendingTOTP(ctx, "an", []byte("B"), t0.Add(time.Second)))
	if err := s.EnableTOTP(ctx, "an", row.SecretEnc, 9, []string{"h"}, "", t0); !errors.Is(err, ErrTOTPNotPending) {
		t.Fatalf("Enable bằng bí mật đã bị thay = %v, muốn ErrTOTPNotPending", err)
	}
	after, _, _ := s.TOTP(ctx, "an")
	if string(after.SecretEnc) != "B" || !after.Enabled.IsZero() {
		t.Fatalf("sau Enable hỏng = %+v; muốn vẫn chờ B", after)
	}
	if n, _ := s.RecoveryLeft(ctx, "an"); n != 0 {
		t.Errorf("Enable hỏng mà ghi %d mã dự phòng", n)
	}
	must(t, s.EnableTOTP(ctx, "an", []byte("B"), 9, []string{"h"}, "", t0))
}

// B7: xoá user thì token, hai bước, mã dự phòng, phiếu, phiên của họ mất trong
// cùng transaction; tạo lại user cùng tên không thừa hưởng gì.
func TestXoaUserXoaTokenVaHaiBuoc(t *testing.T) {
	s := m20Store(t)
	ctx := context.Background()
	enableAn(t, s)
	addChallenge(t, s, "ch", "an")
	addSession(t, s, "ss", "an", "")
	addToken(t, s, "tk", "an", t0, t0.Add(24*time.Hour))
	addToken(t, s, "tk-admin", "admin", t0, t0.Add(24*time.Hour))
	enableAdmin := func() {
		must(t, s.SetPendingTOTP(ctx, "admin", []byte("adm"), t0))
		must(t, s.EnableTOTP(ctx, "admin", []byte("adm"), 1, []string{"ha"}, "", t0))
	}
	enableAdmin()

	must(t, s.DeleteUser(ctx, "an"))
	for _, tbl := range []string{"user_totp", "user_recovery_codes", "login_challenges", "api_tokens", "sessions"} {
		var n int
		must(t, s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+tbl+" WHERE user_name = 'an'").Scan(&n))
		if n != 0 {
			t.Errorf("bảng %s còn %d dòng của an sau DeleteUser", tbl, n)
		}
	}
	// Của admin không bị đụng.
	if on, _ := s.TOTPEnabled(ctx, "admin"); !on {
		t.Error("hai bước của admin mất theo")
	}
	if _, err := s.TokenByHash(ctx, "sha-tk-admin"); err != nil {
		t.Errorf("token của admin mất theo: %v", err)
	}

	addUser(t, s, "an", account.RoleUser)
	if _, ok, _ := s.TOTP(ctx, "an"); ok {
		t.Error("user tạo lại thừa hưởng hai bước")
	}
	if n, _ := s.RecoveryLeft(ctx, "an"); n != 0 {
		t.Error("user tạo lại thừa hưởng mã dự phòng")
	}
	if ts, _ := s.Tokens(ctx, "an"); len(ts) != 0 {
		t.Errorf("user tạo lại thừa hưởng token %+v", ts)
	}
	if _, err := s.TokenByHash(ctx, "sha-tk"); !errors.Is(err, ErrNotFound) {
		t.Errorf("token cũ còn tra được: %v", err)
	}
	if hasChallenge(t, s, "ch") {
		t.Error("user tạo lại thừa hưởng phiếu")
	}
	if _, ok := hasSession(t, s, "ss"); ok {
		t.Error("user tạo lại thừa hưởng phiên")
	}
}

// Mục 11.5: admin đặt lại mật khẩu thì thu hồi mọi token của user (người cầm
// mật khẩu cũ có thể đã tạo token), xoá phiếu; tpan admin passwd còn gỡ khoá
// bước hai của admin.
func TestDatLaiMatKhauThuHoiToken(t *testing.T) {
	s := m20Store(t)
	ctx := context.Background()
	s.now = func() time.Time { return t0.Add(time.Hour) }
	exp := t0.Add(24 * time.Hour)

	// SetUserPassword (POST /users/{name}/password, tpan user passwd).
	addToken(t, s, "an1", "an", t0, exp)
	addToken(t, s, "an2", "an", t0, exp)
	addToken(t, s, "adm1", "admin", t0, exp)
	addChallenge(t, s, "ch-an", "an")
	addChallenge(t, s, "ch-adm", "admin")
	must(t, s.SetUserPassword(ctx, "an", "$moi"))
	if n, _ := s.ActiveTokens(ctx, "an", t0.Add(time.Hour)); n != 0 {
		t.Errorf("SetUserPassword còn %d token sống", n)
	}
	if tk, _ := s.TokenByHash(ctx, "sha-an1"); !tk.Revoked.Equal(t0.Add(time.Hour)) {
		t.Errorf("revoked_at = %v, muốn s.now()", tk.Revoked)
	}
	if n, _ := s.ActiveTokens(ctx, "admin", t0.Add(time.Hour)); n != 1 {
		t.Error("SetUserPassword(an) thu hồi nhầm token của admin")
	}
	if hasChallenge(t, s, "ch-an") || !hasChallenge(t, s, "ch-adm") {
		t.Error("SetUserPassword xoá phiếu sai")
	}
	// Không có user: không đổi gì.
	if err := s.SetUserPassword(ctx, "khong-co", "$x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetUserPassword user không có = %v", err)
	}

	// ResetAdminPassword (tpan admin passwd).
	must(t, s.SetPendingTOTP(ctx, "admin", []byte("adm"), t0))
	must(t, s.EnableTOTP(ctx, "admin", []byte("adm"), 1, nil, "", t0))
	for range 10 {
		_, _, _, err := s.RecordTOTPFailure(ctx, "admin", t0.Add(time.Hour), 10, 15*time.Minute, 24*time.Hour)
		must(t, err)
	}
	for range 3 {
		_, _, _, _ = s.RecordTOTPFailure(ctx, "admin", t0.Add(3*time.Hour), 10, 15*time.Minute, 24*time.Hour)
	}
	addToken(t, s, "an3", "an", t0, exp)
	addChallenge(t, s, "ch-an2", "an")
	must(t, s.ResetAdminPassword(ctx, "$admin-moi", t0.Add(2*time.Hour)))
	if n, _ := s.ActiveTokens(ctx, "admin", t0.Add(2*time.Hour)); n != 0 {
		t.Errorf("ResetAdminPassword còn %d token sống của admin", n)
	}
	if n, _ := s.ActiveTokens(ctx, "an", t0.Add(2*time.Hour)); n != 1 {
		t.Error("ResetAdminPassword thu hồi nhầm token của an")
	}
	if hasChallenge(t, s, "ch-adm") || !hasChallenge(t, s, "ch-an2") {
		t.Error("ResetAdminPassword xoá phiếu sai")
	}
	row, _, _ := s.TOTP(ctx, "admin")
	if row.Failures != 0 || !row.LockedUntil.IsZero() || row.LockCount != 0 || row.Enabled.IsZero() {
		t.Errorf("hai bước của admin sau ResetAdminPassword = %+v; muốn gỡ khoá, vẫn bật", row)
	}
}

// Mục 11.8: tự đổi mật khẩu xoá phiếu chờ (phiếu chứng minh mật khẩu cũ) mà
// không thu hồi token (mục 11.5); khoá user (DeleteSessionsOf) cũng xoá phiếu.
func TestDoiMatKhauXoaPhieu(t *testing.T) {
	s := m20Store(t)
	ctx := context.Background()
	exp := t0.Add(24 * time.Hour)
	addToken(t, s, "an1", "an", t0, exp)
	addToken(t, s, "adm1", "admin", t0, exp)
	addSession(t, s, "keep", "an", "")
	addChallenge(t, s, "ch-an", "an")
	addChallenge(t, s, "ch-adm", "admin")

	must(t, s.ChangeUserPassword(ctx, "an", "$moi", "keep", t0))
	if hasChallenge(t, s, "ch-an") || !hasChallenge(t, s, "ch-adm") {
		t.Error("ChangeUserPassword xoá phiếu sai")
	}
	if n, _ := s.ActiveTokens(ctx, "an", t0); n != 1 {
		t.Error("tự đổi mật khẩu không được thu hồi token")
	}
	must(t, s.ChangeAdminPassword(ctx, "$moi", "", t0))
	if hasChallenge(t, s, "ch-adm") {
		t.Error("ChangeAdminPassword không xoá phiếu")
	}
	if n, _ := s.ActiveTokens(ctx, "admin", t0); n != 1 {
		t.Error("ChangeAdminPassword không được thu hồi token")
	}

	addChallenge(t, s, "ch-an2", "an")
	must(t, s.DeleteSessionsOf(ctx, "an"))
	if hasChallenge(t, s, "ch-an2") {
		t.Error("DeleteSessionsOf (khoá user) không xoá phiếu")
	}
}

// B6: DB của bản 1400 (v0.2.0) có admin và phiên; mở bằng store mới thì phiên
// còn dùng được (Host rỗng), hai bước tắt, nhật ký cũ có token_id rỗng.
func TestNangCapM20GiuDangNhap(t *testing.T) {
	smallArgon(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tpan.db")
	const pass = "mat-khau-cua-v0.2.0"
	hash, err := HashPassword(pass)
	must(t, err)

	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	must(t, err)
	db.SetMaxOpenConns(1)
	if err := migrate(ctx, db, migrationsBelow(t, 1401)); err != nil {
		t.Fatalf("migrate bản 1400: %v", err)
	}
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO users (name, role, password_hash, created_at) VALUES ('admin', 'admin', ?, ?)", []any{hash, fmtTime(t0)}},
		{"INSERT INTO sessions (token_sha256, id, ip, created_at, last_seen_at, user_name) VALUES ('tok', 'sid', '192.0.2.1', ?, ?, 'admin')", []any{fmtTime(t0), fmtTime(t0)}},
		{"INSERT INTO audit (time, ip, action, target, result, actor) VALUES (?, '192.0.2.1', 'login', '', 'ok', 'admin')", []any{fmtTime(t0)}},
	} {
		if _, err := db.ExecContext(ctx, q.sql, q.args...); err != nil {
			t.Fatalf("dựng dữ liệu bản 1400 (%s): %v", q.sql, err)
		}
	}
	must(t, db.Close())

	s, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open sau khi nâng: %v", err)
	}
	defer func() { _ = s.Close() }()

	ss, err := s.SessionByToken(ctx, "tok")
	if err != nil || ss.UserName != "admin" || ss.Host != "" || ss.ID != "sid" {
		t.Fatalf("phiên cũ = %+v, %v; muốn còn, của admin, Host rỗng", ss, err)
	}
	must(t, s.TouchSession(ctx, "tok", t0.Add(time.Minute)))
	h, ok, err := s.PasswordHashOf(ctx, "admin")
	if err != nil || !ok {
		t.Fatalf("PasswordHashOf = %v, %v", ok, err)
	}
	if ok, err := VerifyPassword(h, pass); !ok || err != nil {
		t.Errorf("mật khẩu cũ không đăng nhập được: %v, %v", ok, err)
	}
	if on, err := s.TOTPEnabled(ctx, "admin"); on || err != nil {
		t.Errorf("hai bước sau nâng cấp = %v, %v; muốn tắt", on, err)
	}
	if ms, _ := s.MFAStatuses(ctx); len(ms) != 1 || ms[0].Enabled || ms[0].LockCount != 0 {
		t.Errorf("MFAStatuses = %+v", ms)
	}
	var tokenID string
	must(t, s.db.QueryRowContext(ctx, "SELECT token_id FROM audit WHERE action = 'login'").Scan(&tokenID))
	if tokenID != "" {
		t.Errorf("audit cũ token_id = %q", tokenID)
	}
	// Phiên mới sau nâng cấp mang host.
	must(t, s.CreateSession(ctx, Session{TokenSHA256: "moi", ID: "sid2", IP: "192.0.2.1", Created: t0, Host: "192.0.2.10"}))
	if ss, _ := s.SessionByToken(ctx, "moi"); ss.Host != "192.0.2.10" || ss.UserName != "admin" {
		t.Errorf("phiên mới = %+v", ss)
	}
}

// B3: sau một luồng thật (bí mật niêm phong bằng mfa, mã dự phòng và token
// sinh bằng mfa/apitoken), không bảng nào và không byte nào của file DB hay
// file -wal chứa token, mã dự phòng hay bí mật TOTP dạng rõ.
func TestKhongLoBiMatTrongDB(t *testing.T) {
	s, path := openTemp(t)
	ctx := context.Background()
	addUser(t, s, "admin", account.RoleAdmin)
	addUser(t, s, "an", account.RoleUser)

	key := mfa.NewKey()
	secret := mfa.NewSecret()
	sealed, err := mfa.Seal(key, "an", secret)
	must(t, err)
	codes := mfa.NewRecoveryCodes()
	hashes := make([]string, len(codes))
	for i, c := range codes {
		hashes[i] = mfa.HashRecovery(mfa.NormalizeRecovery(c))
	}
	token := apitoken.New()

	must(t, s.SetPendingTOTP(ctx, "an", sealed, t0))
	step := mfa.Step(t0)
	must(t, s.EnableTOTP(ctx, "an", sealed, step, hashes, "", t0))
	if ok, err := s.UseRecoveryCode(ctx, "an", mfa.HashRecovery(mfa.NormalizeRecovery(codes[0])), t0); !ok || err != nil {
		t.Fatalf("UseRecoveryCode = %v, %v", ok, err)
	}
	must(t, s.CreateToken(ctx, TokenRow{
		ID: "0123456789abcdef", User: "an", Name: "ci", Scopes: "sites:read",
		Hint: apitoken.Hint(token), Created: t0, Expires: t0.Add(24 * time.Hour),
	}, apitoken.Hash(token)))
	must(t, s.TouchToken(ctx, "0123456789abcdef", "192.0.2.1", t0))
	must(t, s.AddAudit(ctx, AuditEntry{Time: t0, IP: "192.0.2.1", Action: "token.create", Target: "ci", Result: "ok", Actor: "an", TokenID: "0123456789abcdef"}))
	addChallenge(t, s, "ch", "an")
	if _, err := s.TokenByHash(ctx, apitoken.Hash(token)); err != nil {
		t.Fatalf("token không tra được bằng sha256: %v", err)
	}

	b32 := mfa.EncodeSecret(secret)
	needles := map[string][]byte{
		"token":                 []byte(token),
		"phần ngẫu nhiên token": []byte(strings.TrimPrefix(token, apitoken.Prefix)),
		"bí mật base32":         []byte(b32),
		"bí mật base32 thường":  []byte(strings.ToLower(b32)),
		"bí mật thô":            secret,
		"khoá niêm phong":       key[:],
	}
	for i, c := range codes {
		needles["mã dự phòng "+strconv.Itoa(i)] = []byte(c)
		needles["mã dự phòng chuẩn hoá "+strconv.Itoa(i)] = []byte(mfa.NormalizeRecovery(c))
	}
	scan := func(where string, data []byte) {
		t.Helper()
		for name, n := range needles {
			if bytes.Contains(data, n) {
				t.Errorf("%s chứa %s dạng rõ", where, name)
			}
		}
	}

	// Mọi bảng, mọi cột, đọc như byte.
	var tables []string
	must(t, queryEach(ctx, s.db, "SELECT name FROM sqlite_master WHERE type = 'table'", func(r rowScanner) error {
		var n string
		if err := r.Scan(&n); err != nil {
			return err
		}
		tables = append(tables, n)
		return nil
	}))
	if len(tables) < 10 {
		t.Fatalf("chỉ thấy %d bảng", len(tables))
	}
	for _, tbl := range tables {
		rows, err := s.db.QueryContext(ctx, "SELECT * FROM "+tbl)
		must(t, err)
		cols, _ := rows.Columns()
		for rows.Next() {
			vals := make([]sql.RawBytes, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			must(t, rows.Scan(ptrs...))
			for i, v := range vals {
				scan("bảng "+tbl+" cột "+cols[i], v)
			}
		}
		must(t, rows.Close())
	}

	// File -wal phải đọc lúc store còn mở: Close gộp nó vào file chính rồi xoá.
	wal, err := os.ReadFile(path + "-wal")
	must(t, err)
	if len(wal) == 0 {
		t.Fatal("file -wal rỗng: phép quét không kiểm gì")
	}
	scan("file -wal", wal)
	main, err := os.ReadFile(path)
	must(t, err)
	scan("file DB (lúc mở)", main)
	must(t, s.Close())
	main, err = os.ReadFile(path)
	must(t, err)
	scan("file DB (sau khi đóng)", main)
	// Phép quét thật sự tìm được thứ có ở đó: sha256 của token phải nằm trong file.
	if !bytes.Contains(main, []byte(apitoken.Hash(token))) {
		t.Error("file DB không có sha256 của token: phép quét đọc nhầm chỗ")
	}
}
