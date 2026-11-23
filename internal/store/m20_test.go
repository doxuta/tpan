package store

// Test cho từng hàm của m20.go. Bất biến của thiết kế (B2, B3, B6, B7, mục 11)
// nằm ở m20_batbien_test.go.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/account"
)

// m20Store mở store có admin và user thường "an".
func m20Store(t *testing.T) *Store {
	t.Helper()
	s, _ := openTemp(t)
	addUser(t, s, "admin", account.RoleAdmin)
	addUser(t, s, "an", account.RoleUser)
	return s
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// enableAn bật hai bước cho "an" với bí mật niêm phong giả và ba mã dự phòng.
func enableAn(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	must(t, s.SetPendingTOTP(ctx, "an", []byte("sealed-an"), t0))
	must(t, s.EnableTOTP(ctx, "an", []byte("sealed-an"), 100, []string{"h1", "h2", "h3"}, "", t0))
}

func addSession(t *testing.T, s *Store, tok, user, viewAs string) {
	t.Helper()
	must(t, s.CreateSession(context.Background(), Session{TokenSHA256: tok, ID: "id-" + tok, IP: "192.0.2.1", Created: t0, UserName: user, ViewAs: viewAs}))
}

func addChallenge(t *testing.T, s *Store, tok, user string) {
	t.Helper()
	must(t, s.CreateChallenge(context.Background(), Challenge{
		TokenSHA256: tok, User: user, IP: "192.0.2.1", Host: "panel.example.com",
		Created: t0, Expires: t0.Add(5 * time.Minute),
	}))
}

func addToken(t *testing.T, s *Store, id, user string, created, expires time.Time) {
	t.Helper()
	must(t, s.CreateToken(context.Background(), TokenRow{
		ID: id, User: user, Name: "ci " + id, Scopes: "sites:read", Hint: "tpan_abcdef…",
		Created: created, Expires: expires,
	}, "sha-"+id))
}

func hasSession(t *testing.T, s *Store, tok string) (Session, bool) {
	t.Helper()
	ss, err := s.SessionByToken(context.Background(), tok)
	if errors.Is(err, ErrNotFound) {
		return Session{}, false
	}
	must(t, err)
	return ss, true
}

func hasChallenge(t *testing.T, s *Store, tok string) bool {
	t.Helper()
	_, err := s.ChallengeByToken(context.Background(), tok)
	if errors.Is(err, ErrNotFound) {
		return false
	}
	must(t, err)
	return true
}

func TestHaiBuocChoRoiBat(t *testing.T) {
	s := m20Store(t)
	ctx := context.Background()

	if _, ok, err := s.TOTP(ctx, "an"); ok || err != nil {
		t.Fatalf("chưa có dòng: ok %v, err %v", ok, err)
	}
	if on, err := s.TOTPEnabled(ctx, "an"); on || err != nil {
		t.Fatalf("TOTPEnabled khi chưa có = %v, %v", on, err)
	}
	if err := s.SetPendingTOTP(ctx, "an", nil, t0); err == nil {
		t.Error("bí mật rỗng phải lỗi")
	}
	if err := s.SetPendingTOTP(ctx, "khong-co", []byte("x"), t0); err == nil {
		t.Error("user không có phải lỗi (khoá ngoại)")
	}
	must(t, s.SetPendingTOTP(ctx, "an", []byte("A"), t0))
	// Đang chờ thì ghi đè được (quét lại mã QR).
	must(t, s.SetPendingTOTP(ctx, "an", []byte("B"), t0.Add(time.Minute)))
	row, ok, err := s.TOTP(ctx, "an")
	if err != nil || !ok || string(row.SecretEnc) != "B" || !row.Enabled.IsZero() || !row.Created.Equal(t0.Add(time.Minute)) {
		t.Fatalf("dòng chờ = %+v, %v, %v", row, ok, err)
	}
	if on, _ := s.TOTPEnabled(ctx, "an"); on {
		t.Error("đang chờ mà TOTPEnabled = true")
	}

	// Phiên: keep (người bật), other (máy khác của an), admin đang xem như an.
	addSession(t, s, "keep", "an", "")
	addSession(t, s, "other", "an", "")
	addSession(t, s, "adm", "admin", "an")
	addChallenge(t, s, "ch-an", "an")
	addChallenge(t, s, "ch-admin", "admin")

	if err := s.EnableTOTP(ctx, "an", []byte("A"), 5, nil, "keep", t0); !errors.Is(err, ErrTOTPNotPending) {
		t.Fatalf("Enable với bí mật cũ = %v, muốn ErrTOTPNotPending", err)
	}
	if err := s.EnableTOTP(ctx, "an", nil, 5, nil, "keep", t0); !errors.Is(err, ErrTOTPNotPending) {
		t.Fatalf("Enable với bí mật rỗng = %v", err)
	}
	if _, ok := hasSession(t, s, "other"); !ok {
		t.Fatal("Enable hỏng mà vẫn xoá phiên")
	}
	now := t0.Add(2 * time.Minute)
	must(t, s.EnableTOTP(ctx, "an", []byte("B"), 12345, []string{"h1", "h2"}, "keep", now))

	row, _, _ = s.TOTP(ctx, "an")
	if !row.Enabled.Equal(now) || row.LastStep != 12345 || row.Failures != 0 || !row.LockedUntil.IsZero() || row.LockCount != 0 {
		t.Errorf("sau Enable = %+v", row)
	}
	if on, _ := s.TOTPEnabled(ctx, "an"); !on {
		t.Error("sau Enable TOTPEnabled = false")
	}
	if n, _ := s.RecoveryLeft(ctx, "an"); n != 2 {
		t.Errorf("RecoveryLeft = %d, muốn 2", n)
	}
	if _, ok := hasSession(t, s, "keep"); !ok {
		t.Error("phiên của người bật bị xoá")
	}
	if _, ok := hasSession(t, s, "other"); ok {
		t.Error("phiên khác của an còn sống sau khi bật")
	}
	if ss, ok := hasSession(t, s, "adm"); !ok || ss.ViewAs != "an" {
		t.Errorf("phiên admin xem như an = %+v, %v; muốn giữ nguyên", ss, ok)
	}
	if hasChallenge(t, s, "ch-an") {
		t.Error("phiếu của an còn sau khi bật")
	}
	if !hasChallenge(t, s, "ch-admin") {
		t.Error("phiếu của admin bị xoá nhầm")
	}

	// Đã bật: không ghi đè bí mật, không bật lại.
	if err := s.SetPendingTOTP(ctx, "an", []byte("C"), now); !errors.Is(err, ErrTOTPEnabled) {
		t.Errorf("SetPendingTOTP khi đã bật = %v", err)
	}
	if row2, _, _ := s.TOTP(ctx, "an"); string(row2.SecretEnc) != "B" || !row2.Created.Equal(row.Created) {
		t.Errorf("bí mật đang dùng bị đổi: %+v", row2)
	}
	if err := s.EnableTOTP(ctx, "an", []byte("B"), 1, nil, "", now); !errors.Is(err, ErrTOTPNotPending) {
		t.Errorf("Enable lần hai = %v", err)
	}
}

func TestTatVaDatLaiHaiBuoc(t *testing.T) {
	s := m20Store(t)
	ctx := context.Background()

	if err := s.DisableTOTP(ctx, "an"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Disable khi chưa bật = %v", err)
	}
	enableAn(t, s)
	addSession(t, s, "s1", "an", "")
	addChallenge(t, s, "c1", "an")
	must(t, s.DisableTOTP(ctx, "an"))
	if _, ok, _ := s.TOTP(ctx, "an"); ok {
		t.Error("còn dòng hai bước sau Disable")
	}
	if n, _ := s.RecoveryLeft(ctx, "an"); n != 0 {
		t.Errorf("còn %d mã dự phòng sau Disable", n)
	}
	if hasChallenge(t, s, "c1") {
		t.Error("còn phiếu sau Disable")
	}
	if _, ok := hasSession(t, s, "s1"); !ok {
		t.Error("Disable không được xoá phiên (người tự tắt đang dùng nó)")
	}

	// Reset khi chưa bật: vẫn xoá phiên, vẫn trả ErrNotFound.
	if err := s.ResetTOTP(ctx, "an"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Reset khi chưa bật = %v", err)
	}
	if _, ok := hasSession(t, s, "s1"); ok {
		t.Error("Reset khi chưa bật phải vẫn xoá phiên")
	}

	enableAn(t, s)
	addSession(t, s, "s2", "an", "")
	addSession(t, s, "adm", "admin", "an")
	addChallenge(t, s, "c2", "an")
	must(t, s.ResetTOTP(ctx, "an"))
	if _, ok, _ := s.TOTP(ctx, "an"); ok {
		t.Error("còn dòng hai bước sau Reset")
	}
	if _, ok := hasSession(t, s, "s2"); ok {
		t.Error("còn phiên sau Reset")
	}
	if ss, ok := hasSession(t, s, "adm"); !ok || ss.ViewAs != "" {
		t.Errorf("phiên admin xem như an sau Reset = %+v, %v; muốn còn, thoát xem như", ss, ok)
	}
	if hasChallenge(t, s, "c2") || func() bool { n, _ := s.RecoveryLeft(ctx, "an"); return n != 0 }() {
		t.Error("còn phiếu hoặc mã dự phòng sau Reset")
	}
}

func TestMaTOTPKhongDungLai(t *testing.T) {
	s := m20Store(t)
	ctx := context.Background()

	// Đang chờ (chưa bật) thì không nhận mã nào.
	must(t, s.SetPendingTOTP(ctx, "an", []byte("A"), t0))
	if ok, err := s.UseTOTPStep(ctx, "an", 200); ok || err != nil {
		t.Fatalf("UseTOTPStep khi chưa bật = %v, %v", ok, err)
	}
	must(t, s.EnableTOTP(ctx, "an", []byte("A"), 100, nil, "", t0))
	for _, c := range []struct {
		step uint64
		want bool
	}{{100, false}, {99, false}, {101, true}, {101, false}, {100, false}, {103, true}, {102, false}} {
		if ok, err := s.UseTOTPStep(ctx, "an", c.step); ok != c.want || err != nil {
			t.Errorf("UseTOTPStep(%d) = %v, %v; muốn %v", c.step, ok, err, c.want)
		}
	}
	if row, _, _ := s.TOTP(ctx, "an"); row.LastStep != 103 {
		t.Errorf("last_step = %d, muốn 103", row.LastStep)
	}
	if ok, _ := s.UseTOTPStep(ctx, "khong-co", 999); ok {
		t.Error("user không có mà nhận mã")
	}
}

func TestMaDuPhongMotLan(t *testing.T) {
	s := m20Store(t)
	ctx := context.Background()
	enableAn(t, s)
	must(t, s.ReplaceRecoveryCodes(ctx, "an", []string{"x1", "x2"}))
	if n, _ := s.RecoveryLeft(ctx, "an"); n != 2 {
		t.Fatalf("RecoveryLeft sau Replace = %d", n)
	}
	if ok, _ := s.UseRecoveryCode(ctx, "an", "h1", t0); ok {
		t.Error("mã cũ (trước Replace) vẫn dùng được")
	}
	if ok, _ := s.UseRecoveryCode(ctx, "admin", "x1", t0); ok {
		t.Error("mã của an dùng được cho admin")
	}

	// Đang bị khoá: mã dự phòng đúng gỡ khoá và xoá đếm leo thang.
	for range 10 {
		_, _, _, err := s.RecordTOTPFailure(ctx, "an", t0, 10, 15*time.Minute, 24*time.Hour)
		must(t, err)
	}
	if row, _, _ := s.TOTP(ctx, "an"); row.LockCount != 1 || row.LockedUntil.IsZero() {
		t.Fatalf("chưa khoá được: %+v", row)
	}
	if ok, err := s.UseRecoveryCode(ctx, "an", "x1", t0); !ok || err != nil {
		t.Fatalf("UseRecoveryCode lần đầu = %v, %v", ok, err)
	}
	if row, _, _ := s.TOTP(ctx, "an"); row.LockCount != 0 || !row.LockedUntil.IsZero() || row.Failures != 0 {
		t.Errorf("mã dự phòng đúng không gỡ khoá: %+v", row)
	}
	if ok, err := s.UseRecoveryCode(ctx, "an", "x1", t0); ok || err != nil {
		t.Errorf("UseRecoveryCode lần hai = %v, %v; muốn false", ok, err)
	}
	if n, _ := s.RecoveryLeft(ctx, "an"); n != 1 {
		t.Errorf("RecoveryLeft = %d, muốn 1", n)
	}
}

func TestKhoaLeoThang(t *testing.T) {
	s := m20Store(t)
	ctx := context.Background()
	const max = 10
	base, cap := 15*time.Minute, 24*time.Hour

	if _, _, _, err := s.RecordTOTPFailure(ctx, "an", t0, max, base, cap); !errors.Is(err, ErrNotFound) {
		t.Fatalf("chưa có hai bước = %v, muốn ErrNotFound", err)
	}
	must(t, s.SetPendingTOTP(ctx, "an", []byte("A"), t0))
	if _, _, _, err := s.RecordTOTPFailure(ctx, "an", t0, max, base, cap); !errors.Is(err, ErrNotFound) {
		t.Fatalf("đang chờ (mã sai ở enable không đếm) = %v, muốn ErrNotFound", err)
	}
	must(t, s.EnableTOTP(ctx, "an", []byte("A"), 1, nil, "", t0))

	now := t0
	for i, want := range []time.Duration{15 * time.Minute, 30 * time.Minute, 60 * time.Minute} {
		n := i + 1
		for j := 1; j < max; j++ {
			locked, until, lc, err := s.RecordTOTPFailure(ctx, "an", now, max, base, cap)
			if err != nil || locked || !until.IsZero() || lc != n-1 {
				t.Fatalf("lần khoá %d, sai %d: %v %v %d %v", n, j, locked, until, lc, err)
			}
		}
		locked, until, lc, err := s.RecordTOTPFailure(ctx, "an", now, max, base, cap)
		if err != nil || !locked || !until.Equal(now.Add(want)) || lc != n {
			t.Fatalf("lần khoá %d = %v, %v, %d, %v; muốn khoá tới +%v", n, locked, until, lc, err, want)
		}
		row, _, _ := s.TOTP(ctx, "an")
		if row.Failures != 0 || !row.LockedUntil.Equal(until) || row.LockCount != n {
			t.Fatalf("dòng sau khoá %d = %+v", n, row)
		}

		// Sai lúc đang khoá: không gỡ khoá, không đếm, không khoá lại.
		mid := now.Add(want / 2)
		for range 3 * max {
			l2, u2, lc2, err := s.RecordTOTPFailure(ctx, "an", mid, max, base, cap)
			if err != nil || l2 || !u2.Equal(until) || lc2 != n {
				t.Fatalf("sai lúc đang khoá = %v, %v, %d, %v", l2, u2, lc2, err)
			}
		}
		if again, _, _ := s.TOTP(ctx, "an"); again.Failures != row.Failures || !again.LockedUntil.Equal(row.LockedUntil) || again.LockCount != row.LockCount {
			t.Fatalf("sai lúc đang khoá làm đổi dòng: %+v -> %+v", row, again)
		}
		if ms, _ := s.MFAStatuses(ctx); ms[1].User != "an" || ms[1].LockCount != n {
			t.Errorf("MFAStatuses = %+v", ms)
		}
		now = until // hết khoá đúng lúc này: lần sai sau được đếm
	}

	// Đúng mã thì hết leo thang và hết cảnh báo.
	if ok, _ := s.UseTOTPStep(ctx, "an", 2); !ok {
		t.Fatal("UseTOTPStep")
	}
	if row, _, _ := s.TOTP(ctx, "an"); row.LockCount != 0 || !row.LockedUntil.IsZero() || row.Failures != 0 {
		t.Errorf("sau mã đúng = %+v", row)
	}
	for range max {
		_, _, _, _ = s.RecordTOTPFailure(ctx, "an", now, max, base, cap)
	}
	if row, _, _ := s.TOTP(ctx, "an"); row.LockCount != 1 || !row.LockedUntil.Equal(now.Add(base)) {
		t.Errorf("leo thang không đếm lại từ đầu sau mã đúng: %+v", row)
	}

	// Trần.
	for _, c := range []struct {
		n    int
		want time.Duration
	}{{1, 15 * time.Minute}, {2, 30 * time.Minute}, {4, 2 * time.Hour}, {7, 16 * time.Hour}, {8, 24 * time.Hour}, {1000, 24 * time.Hour}} {
		if got := lockFor(c.n, base, cap); got != c.want {
			t.Errorf("lockFor(%d) = %v, muốn %v", c.n, got, c.want)
		}
	}
	// Trần nhỏ qua đường thật: lần khoá thứ hai đã chạm trần.
	enableFresh := func() {
		must(t, s.DisableTOTP(ctx, "an"))
		must(t, s.SetPendingTOTP(ctx, "an", []byte("A"), t0))
		must(t, s.EnableTOTP(ctx, "an", []byte("A"), 1, nil, "", t0))
	}
	enableFresh()
	now = t0
	var until time.Time
	for range 2 {
		for range max {
			_, until, _, _ = s.RecordTOTPFailure(ctx, "an", now, max, base, 20*time.Minute)
		}
		now = until
	}
	// Lần một 15 phút (t0 → t0+15), lần hai 30 phút bị cắt còn 20 (→ t0+35).
	if row, _, _ := s.TOTP(ctx, "an"); row.LockCount != 2 || !row.LockedUntil.Equal(t0.Add(35*time.Minute)) {
		t.Errorf("trần 20 phút: %+v", row)
	}
}

func TestTinhTrangHaiBuoc(t *testing.T) {
	s := m20Store(t)
	ctx := context.Background()
	addUser(t, s, "binh", account.RoleUser)
	enableAn(t, s)
	must(t, s.SetPendingTOTP(ctx, "binh", []byte("B"), t0))
	if _, err := s.UseRecoveryCode(ctx, "an", "h2", t0); err != nil {
		t.Fatal(err)
	}
	ms, err := s.MFAStatuses(ctx)
	if err != nil || len(ms) != 3 {
		t.Fatalf("MFAStatuses = %+v, %v", ms, err)
	}
	want := []MFAStatus{
		{User: "admin", Role: account.RoleAdmin, Status: account.Active},
		{User: "an", Role: account.RoleUser, Status: account.Active, Enabled: true, EnabledAt: t0, RecoveryLeft: 2},
		{User: "binh", Role: account.RoleUser, Status: account.Active}, // đang chờ = chưa bật
	}
	for i := range want {
		if ms[i] != want[i] {
			t.Errorf("MFAStatuses[%d] = %+v, muốn %+v", i, ms[i], want[i])
		}
	}
}

func TestPhieuBuocHai(t *testing.T) {
	s := m20Store(t)
	ctx := context.Background()
	c := Challenge{TokenSHA256: "c1", User: "an", IP: "192.0.2.7", Host: "panel.example.com", Created: t0, Expires: t0.Add(5 * time.Minute)}
	must(t, s.CreateChallenge(ctx, c))
	if err := s.CreateChallenge(ctx, Challenge{TokenSHA256: "c9", User: "khong-co", Created: t0, Expires: t0}); err == nil {
		t.Error("phiếu cho user không có phải lỗi")
	}
	got, err := s.ChallengeByToken(ctx, "c1")
	if err != nil || got != c {
		t.Fatalf("ChallengeByToken = %+v, %v; muốn %+v", got, err, c)
	}
	for want := 1; want <= 3; want++ {
		if n, err := s.BumpChallenge(ctx, "c1"); n != want || err != nil {
			t.Fatalf("BumpChallenge = %d, %v; muốn %d", n, err, want)
		}
	}
	if got, _ := s.ChallengeByToken(ctx, "c1"); got.Attempts != 3 {
		t.Errorf("Attempts = %d", got.Attempts)
	}
	if ok, err := s.ConsumeChallenge(ctx, "c1"); !ok || err != nil {
		t.Fatalf("ConsumeChallenge = %v, %v", ok, err)
	}
	if ok, err := s.ConsumeChallenge(ctx, "c1"); ok || err != nil {
		t.Errorf("ConsumeChallenge lần hai = %v, %v", ok, err)
	}
	if _, err := s.BumpChallenge(ctx, "c1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("BumpChallenge phiếu đã dùng = %v", err)
	}
	if _, err := s.ChallengeByToken(ctx, "c1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("ChallengeByToken phiếu đã dùng = %v", err)
	}

	// Dọn theo hạn và theo user.
	must(t, s.CreateChallenge(ctx, Challenge{TokenSHA256: "old", User: "an", Created: t0, Expires: t0.Add(time.Minute)}))
	must(t, s.CreateChallenge(ctx, Challenge{TokenSHA256: "new", User: "an", Created: t0, Expires: t0.Add(time.Hour)}))
	must(t, s.CreateChallenge(ctx, Challenge{TokenSHA256: "adm", User: "admin", Created: t0, Expires: t0.Add(time.Hour)}))
	must(t, s.DeleteExpiredChallenges(ctx, t0.Add(2*time.Minute)))
	if hasChallenge(t, s, "old") || !hasChallenge(t, s, "new") {
		t.Error("DeleteExpiredChallenges xoá sai")
	}
	must(t, s.DeleteChallengesOf(ctx, "an"))
	if hasChallenge(t, s, "new") || !hasChallenge(t, s, "adm") {
		t.Error("DeleteChallengesOf xoá sai")
	}
}

func TestPhienCoHost(t *testing.T) {
	s := m20Store(t)
	ctx := context.Background()
	must(t, s.CreateSession(ctx, Session{TokenSHA256: "a", ID: "ia", IP: "192.0.2.1", Created: t0, UserName: "an", Host: "panel.example.com"}))
	must(t, s.CreateSession(ctx, Session{TokenSHA256: "b", ID: "ib", IP: "192.0.2.1", Created: t0, UserName: "an", Host: "192.0.2.10"}))
	must(t, s.CreateSession(ctx, Session{TokenSHA256: "c", ID: "ic", IP: "192.0.2.1", Created: t0, UserName: "an"}))
	if ss, _ := hasSession(t, s, "a"); ss.Host != "panel.example.com" {
		t.Errorf("Host = %q", ss.Host)
	}
	all, _ := s.SessionsOf(ctx, "an")
	if len(all) != 3 {
		t.Fatalf("SessionsOf = %+v", all)
	}
	must(t, s.DeleteSessionsByHost(ctx, ""))
	if all, _ := s.Sessions(ctx); len(all) != 3 {
		t.Error("host rỗng không được xoá gì (phiên bản cũ)")
	}
	must(t, s.DeleteSessionsByHost(ctx, "panel.example.com"))
	_, a := hasSession(t, s, "a")
	_, b := hasSession(t, s, "b")
	_, c := hasSession(t, s, "c")
	if a || !b || !c {
		t.Errorf("sau DeleteSessionsByHost: a %v b %v c %v; muốn chỉ a mất", a, b, c)
	}
}

func TestTokenTaoDocThuHoi(t *testing.T) {
	s := m20Store(t)
	ctx := context.Background()
	exp := t0.Add(90 * 24 * time.Hour)
	addToken(t, s, "t1", "an", t0, exp)
	addToken(t, s, "t2", "an", t0.Add(time.Minute), exp)
	addToken(t, s, "t3", "admin", t0.Add(2*time.Minute), exp)
	addToken(t, s, "t4", "an", t0.Add(3*time.Minute), t0.Add(time.Hour)) // hết hạn sớm
	if err := s.CreateToken(ctx, TokenRow{ID: "t1", User: "an", Created: t0, Expires: exp}, "sha-khac"); err == nil {
		t.Error("trùng id phải lỗi")
	}
	if err := s.CreateToken(ctx, TokenRow{ID: "t9", User: "an", Created: t0, Expires: exp}, "sha-t1"); err == nil {
		t.Error("trùng sha256 phải lỗi")
	}
	if err := s.CreateToken(ctx, TokenRow{ID: "t9", User: "khong-co", Created: t0, Expires: exp}, "sha-t9"); err == nil {
		t.Error("token cho user không có phải lỗi")
	}

	got, err := s.TokenByHash(ctx, "sha-t2")
	want := TokenRow{ID: "t2", User: "an", Name: "ci t2", Scopes: "sites:read", Hint: "tpan_abcdef…", Created: t0.Add(time.Minute), Expires: exp}
	if err != nil || got != want {
		t.Fatalf("TokenByHash = %+v, %v; muốn %+v", got, err, want)
	}
	if _, err := s.TokenByHash(ctx, "khong-co"); !errors.Is(err, ErrNotFound) {
		t.Errorf("TokenByHash không có = %v", err)
	}
	ids := func(user string) []string {
		ts, err := s.Tokens(ctx, user)
		must(t, err)
		out := []string{}
		for _, t := range ts {
			out = append(out, t.ID)
		}
		return out
	}
	if g := ids("an"); len(g) != 3 || g[0] != "t4" || g[2] != "t1" {
		t.Errorf("Tokens(an) = %v, muốn mới trước", g)
	}
	if g := ids(""); len(g) != 4 || g[0] != "t4" || g[1] != "t3" {
		t.Errorf("Tokens(\"\") = %v", g)
	}
	if g := ids("binh"); g == nil || len(g) != 0 {
		t.Errorf("Tokens(binh) = %#v, muốn rỗng không nil", g)
	}

	now := t0.Add(2 * time.Hour) // t4 đã hết hạn
	if n, _ := s.ActiveTokens(ctx, "an", now); n != 2 {
		t.Errorf("ActiveTokens(an) = %d, muốn 2", n)
	}
	must(t, s.TouchToken(ctx, "t1", "192.0.2.9", now))
	if got, _ := s.TokenByHash(ctx, "sha-t1"); !got.LastUsed.Equal(now) || got.LastIP != "192.0.2.9" {
		t.Errorf("sau TouchToken = %+v", got)
	}

	// Thu hồi: người khác, không có, đã thu hồi đều là cùng một ErrNotFound.
	if err := s.RevokeToken(ctx, "t1", "admin", now); !errors.Is(err, ErrNotFound) {
		t.Errorf("thu hồi token của người khác = %v", err)
	}
	if err := s.RevokeToken(ctx, "khong-co", "an", now); !errors.Is(err, ErrNotFound) {
		t.Errorf("thu hồi id không có = %v", err)
	}
	must(t, s.RevokeToken(ctx, "t1", "an", now))
	if err := s.RevokeToken(ctx, "t1", "an", now); !errors.Is(err, ErrNotFound) {
		t.Errorf("thu hồi lần hai = %v", err)
	}
	if got, _ := s.TokenByHash(ctx, "sha-t1"); !got.Revoked.Equal(now) {
		t.Errorf("Revoked = %v", got.Revoked)
	}
	must(t, s.RevokeToken(ctx, "t3", "", now)) // admin thu hồi bất kỳ
	if n, _ := s.ActiveTokens(ctx, "an", now); n != 1 {
		t.Errorf("ActiveTokens(an) sau thu hồi = %d", n)
	}
	n, err := s.RevokeTokensOf(ctx, "an", now)
	if err != nil || n != 1 {
		t.Errorf("RevokeTokensOf = %d, %v; muốn 1 (t2; t1 đã thu hồi, t4 hết hạn)", n, err)
	}
	if n, _ := s.ActiveTokens(ctx, "an", now); n != 0 {
		t.Errorf("ActiveTokens sau RevokeTokensOf = %d", n)
	}
	if got, _ := s.TokenByHash(ctx, "sha-t1"); !got.Revoked.Equal(now) {
		t.Error("RevokeTokensOf ghi đè revoked_at cũ")
	}
}

// wrong_since_ok đếm mọi lần sai (cả lúc đang khoá), không mất khi khoá hay khi
// mã đúng, và chỉ về 0 khi server lấy nó ra sau bước hai đúng.
func TestSoMaSaiTuLanDungTruoc(t *testing.T) {
	s := m20Store(t)
	ctx := context.Background()
	if n, err := s.TakeWrongSinceOK(ctx, "an"); n != 0 || err != nil {
		t.Fatalf("chưa có hai bước = %d, %v", n, err)
	}
	enableAn(t, s)
	const max = 10
	// 12 lần sai: lần 10 khoá, lần 11, 12 rơi vào lúc đang khoá.
	for range 12 {
		_, _, _, err := s.RecordTOTPFailure(ctx, "an", t0, max, 15*time.Minute, 24*time.Hour)
		must(t, err)
	}
	if row, _, _ := s.TOTP(ctx, "an"); row.WrongSinceOK != 12 || row.Failures != 0 || row.LockCount != 1 {
		t.Fatalf("sau 12 lần sai = %+v; muốn WrongSinceOK 12", row)
	}
	if ms, _ := s.MFAStatuses(ctx); ms[1].User != "an" || ms[1].WrongSinceOK != 12 {
		t.Errorf("MFAStatuses = %+v", ms)
	}
	// Mã dự phòng đúng gỡ khoá nhưng không xoá số đếm (để báo cho chủ tài khoản).
	if ok, _ := s.UseRecoveryCode(ctx, "an", "h1", t0); !ok {
		t.Fatal("UseRecoveryCode")
	}
	_, _, _, err := s.RecordTOTPFailure(ctx, "an", t0, max, 15*time.Minute, 24*time.Hour)
	must(t, err)
	if ok, _ := s.UseTOTPStep(ctx, "an", 200); !ok {
		t.Fatal("UseTOTPStep")
	}
	if row, _, _ := s.TOTP(ctx, "an"); row.WrongSinceOK != 13 || row.LockCount != 0 {
		t.Fatalf("mã đúng không được xoá WrongSinceOK: %+v", row)
	}
	if n, err := s.TakeWrongSinceOK(ctx, "an"); n != 13 || err != nil {
		t.Errorf("TakeWrongSinceOK = %d, %v; muốn 13", n, err)
	}
	if n, err := s.TakeWrongSinceOK(ctx, "an"); n != 0 || err != nil {
		t.Errorf("TakeWrongSinceOK lần hai = %d, %v; muốn 0", n, err)
	}
	// Bật lại từ đầu thì số đếm của lần bật trước không còn.
	_, _, _, _ = s.RecordTOTPFailure(ctx, "an", t0, max, 15*time.Minute, 24*time.Hour)
	must(t, s.DisableTOTP(ctx, "an"))
	enableAn(t, s)
	if row, _, _ := s.TOTP(ctx, "an"); row.WrongSinceOK != 0 {
		t.Errorf("bật lại mà WrongSinceOK = %d", row.WrongSinceOK)
	}
}

func TestDemAuditTuLuc(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	add := func(at time.Time, action, target, result string) {
		must(t, s.AddAudit(ctx, AuditEntry{Time: at, IP: "192.0.2.1", Action: action, Target: target, Result: result}))
	}
	add(t0.Add(-time.Hour), "login.2fa_locked", "an", "locked") // trước since
	add(t0, "login.2fa_locked", "an", "locked")                 // đúng since: tính
	add(t0.Add(time.Minute), "login.2fa_locked", "an", "locked")
	add(t0.Add(time.Minute), "login.2fa_locked", "binh", "locked")
	add(t0.Add(time.Minute), "login.2fa", "an", "wrong_code")   // khác action
	add(t0.Add(time.Minute), "login.2fa_locked", "an", "other") // khác result
	got, err := s.CountAuditSince(ctx, "login.2fa_locked", "locked", t0)
	if err != nil || len(got) != 2 || got["an"] != 2 || got["binh"] != 1 {
		t.Errorf("CountAuditSince = %v, %v; muốn an 2, binh 1", got, err)
	}
	got, err = s.CountAuditSince(ctx, "login.2fa_locked", "locked", t0.Add(time.Hour))
	if err != nil || got == nil || len(got) != 0 {
		t.Errorf("không có dòng = %#v, %v; muốn map rỗng không nil", got, err)
	}
}

func TestAuditGhiTokenID(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	must(t, s.AddAudit(ctx, AuditEntry{Time: t0, IP: "192.0.2.1", Action: "site.add", Result: "ok", Actor: "an", TokenID: "0123456789abcdef"}))
	must(t, s.AddAudit(ctx, AuditEntry{Time: t0, IP: "cli", Action: "site.add", Result: "ok"}))
	var got []string
	must(t, queryEach(ctx, s.db, "SELECT token_id FROM audit ORDER BY id", func(r rowScanner) error {
		var id string
		got = append(got, id)
		return r.Scan(&got[len(got)-1])
	}))
	if len(got) != 2 || got[0] != "0123456789abcdef" || got[1] != "" {
		t.Errorf("token_id = %q", got)
	}
}
