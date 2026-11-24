package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/apitoken"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/server"
	"github.com/doxuta/tpan/internal/store"
)

// seedM20 tạo admin (có hash), khach_a (vai user) và khach_b (vai user, tạm
// khoá) trong DB của CLI.
func (c *cli) seedM20() {
	c.t.Helper()
	c.seedUser("khach_a", "co_ban", account.Package{MaxSites: 2})
	c.withStore(func(ctx context.Context, st *store.Store) {
		if err := st.SetAdminPasswordHash(ctx, "$cu"); err != nil {
			c.t.Fatal(err)
		}
		u, _, _ := st.User(ctx, "khach_a")
		u.Name, u.Status, u.SuspendedAt = "khach_b", account.Suspended, time.Now()
		if err := st.CreateUser(ctx, u, "$cu"); err != nil {
			c.t.Fatal(err)
		}
	})
}

// enable2FA bật hai bước cho user với hai mã dự phòng (bí mật là chuỗi giả:
// CLI không bao giờ mở nó).
func (c *cli) enable2FA(user string) {
	c.t.Helper()
	c.withStore(func(ctx context.Context, st *store.Store) {
		now := time.Now().Add(-time.Hour)
		if err := st.SetPendingTOTP(ctx, user, []byte("niem-phong"), now); err != nil {
			c.t.Fatal(err)
		}
		if err := st.EnableTOTP(ctx, user, []byte("niem-phong"), 0, []string{"aa", "bb"}, "", now); err != nil {
			c.t.Fatal(err)
		}
	})
}

// addToken ghi thẳng một token (trả id); revoked, expired chọn trạng thái.
func (c *cli) addToken(user, name, scopes string, revoked, expired bool) string {
	c.t.Helper()
	var id string
	c.withStore(func(ctx context.Context, st *store.Store) {
		tok := apitoken.New()
		now := time.Now()
		row := store.TokenRow{ID: newTokenID(), User: user, Name: name, Scopes: scopes, Hint: apitoken.Hint(tok),
			Created: now.Add(-48 * time.Hour), Expires: now.Add(24 * time.Hour)}
		if expired {
			row.Expires = now.Add(-time.Hour)
		}
		if revoked {
			row.Revoked = now.Add(-time.Minute)
		}
		if err := st.CreateToken(ctx, row, apitoken.Hash(tok)); err != nil {
			c.t.Fatal(err)
		}
		id = row.ID
	})
	return id
}

// m20Commands là mọi lệnh M20 có nhận một giá trị người dùng gõ ở dạng arg.
func m20Commands(bad string) [][]string {
	return [][]string{
		{"user", "2fa", "status", bad},
		{"user", "2fa", "reset", bad, "--yes"},
		{"user", "2fa", "reset", bad, "--dry-run"},
		{"user", "2fa", "policy", bad},
		{"token", "create", bad, "--name", "n", "--scope", "sites:read", "--days", "1"},
		{"token", "list", "--user", bad},
		{"token", "revoke", bad, "--yes"},
		{"panel", "domain", "set", bad, "--dry-run"},
		{"panel", "domain", "set", bad, "--yes"},
	}
}

func TestM20ArgsRejectedBeforeAnything(t *testing.T) {
	for _, bad := range []string{"a;rm -rf /", "$(id)", "../../etc/passwd", "a\nb", strings.Repeat("a", 300), "Khách"} {
		for _, args := range m20Commands(bad) {
			c := newCLI(t)
			code, out, errs := c.run(args...)
			if code != 1 || !strings.Contains(errs, "Từ chối:") || out != "" {
				t.Errorf("%q: mã %d, stdout %q, stderr %q", args, code, out, errs)
			}
			if len(c.fake.Ran()) != 0 || c.dbExists() {
				t.Errorf("%q: chạy lệnh %v hoặc tạo DB", args, c.fake.Ran())
			}
		}
	}
	// Cờ sai giá trị cũng bị chặn trước khi chạm DB.
	for _, args := range [][]string{
		{"token", "create", "khach_a", "--name", "n", "--scope", "sites:read; id", "--days", "1"},
		{"token", "create", "khach_a", "--name", "n", "--scope", "sites:execute", "--days", "1"},
		{"token", "create", "khach_a", "--name", "n", "--days", "1"},
		{"panel", "domain", "set"},
		{"panel", "domain", "set", "a.example.com", "--clear"},
	} {
		c := newCLI(t)
		code, _, errs := c.run(args...)
		if code != 1 || !strings.Contains(errs, "Từ chối:") || c.dbExists() || len(c.fake.Ran()) != 0 {
			t.Errorf("%q: mã %d, stderr %q, DB %v", args, code, errs, c.dbExists())
		}
	}
}

// Không root: mọi lệnh đọc hay ghi tpan.db từ chối trước khi mở DB.
func TestM20CanRoot(t *testing.T) {
	for _, args := range [][]string{
		{"user", "2fa", "status"},
		{"user", "2fa", "reset", "khach_a", "--yes"},
		{"user", "2fa", "reset", "khach_a", "--dry-run"},
		{"user", "2fa", "policy"},
		{"user", "2fa", "policy", "off"},
		{"token", "create", "khach_a", "--name", "n", "--scope", "sites:read", "--days", "1"},
		{"token", "list"},
		{"token", "revoke", "0123456789abcdef", "--yes"},
		{"panel", "domain", "show"},
		{"panel", "domain", "set", "panel.example.com", "--yes"},
		{"panel", "domain", "set", "--clear", "--yes"},
	} {
		c := newCLI(t)
		c.root = false
		code, out, _ := c.run(append(args, "--json")...)
		if code != 1 || refusalCode(t, out) != "root_required" || c.dbExists() {
			t.Errorf("%q: mã %d, %s", args, code, out)
		}
	}
}

// Chưa có tpan.db: từ chối setup_required và không tạo file.
func TestM20ChuaCoDB(t *testing.T) {
	for _, args := range [][]string{
		{"user", "2fa", "status"},
		{"user", "2fa", "reset", "khach_a", "--yes"},
		{"user", "2fa", "policy", "all"},
		{"token", "create", "khach_a", "--name", "n", "--scope", "sites:read", "--days", "1"},
		{"token", "list"},
		{"token", "revoke", "0123456789abcdef", "--yes"},
	} {
		c := newCLI(t)
		code, out, _ := c.run(append(args, "--json")...)
		if code != 1 || refusalCode(t, out) != "setup_required" || c.dbExists() {
			t.Errorf("%q: mã %d, %s", args, code, out)
		}
	}
}

func TestUser2FAStatus(t *testing.T) {
	c := newCLI(t)
	c.seedM20()
	c.enable2FA("khach_a")
	c.withStore(func(ctx context.Context, st *store.Store) {
		if ok, err := st.UseRecoveryCode(ctx, "khach_a", "aa", time.Now()); err != nil || !ok {
			t.Fatal(ok, err)
		}
	})
	c.seed(map[string]string{settingRequire2FA: "admin"})

	code, out, errs := c.run("user", "2fa", "status", "--json")
	var rows []mfaRow
	if err := json.Unmarshal([]byte(out), &rows); err != nil || code != 0 {
		t.Fatalf("mã %d, %v\n%s%s", code, err, out, errs)
	}
	by := map[string]mfaRow{}
	for _, r := range rows {
		by[r.User] = r
	}
	if a := by["khach_a"]; !a.Enabled || a.RecoveryLeft != 1 || a.Required || a.EnabledAt.IsZero() {
		t.Errorf("khach_a: %+v", a)
	}
	if ad := by["admin"]; ad.Enabled || !ad.Required || ad.Role != account.RoleAdmin {
		t.Errorf("admin: %+v", ad)
	}
	if b := by["khach_b"]; b.Enabled || b.Required {
		t.Errorf("khach_b: %+v", b)
	}

	// Một user; chính sách all bắt mọi user.
	c.seed(map[string]string{settingRequire2FA: "all"})
	code, out, _ = c.run("user", "2fa", "status", "khach_b", "--json")
	rows = nil
	if err := json.Unmarshal([]byte(out), &rows); err != nil || code != 0 || len(rows) != 1 || rows[0].User != "khach_b" || !rows[0].Required {
		t.Errorf("status khach_b: mã %d, %v, %s", code, err, out)
	}
	code, out, _ = c.run("user", "2fa", "status")
	if code != 0 || !strings.Contains(out, "khach_a") || !strings.Contains(out, "bắt buộc") || !strings.Contains(out, "all") {
		t.Errorf("bảng: mã %d\n%s", code, out)
	}
	if code, out, _ = c.run("user", "2fa", "status", "khong_co", "--json"); code != 1 || refusalCode(t, out) != "user_not_found" {
		t.Errorf("user lạ: mã %d, %s", code, out)
	}
	if n := c.audits("user.2fa_reset"); n != 0 {
		t.Errorf("status ghi audit: %d", n)
	}
}

func TestUser2FAReset(t *testing.T) {
	c := newCLI(t)
	c.seedM20()
	c.enable2FA("khach_a")
	c.withStore(func(ctx context.Context, st *store.Store) {
		if err := st.CreateSession(ctx, store.Session{TokenSHA256: "s1", ID: "s1", IP: "192.0.2.1", Created: time.Now(), UserName: "khach_a"}); err != nil {
			t.Fatal(err)
		}
	})
	live := c.addToken("khach_a", "Triển khai", "sites:write", false, false)
	c.addToken("khach_a", "Cũ", "sites:read", true, false)

	// --dry-run và không --yes: không đổi gì.
	if code, out, _ := c.run("user", "2fa", "reset", "khach_a", "--dry-run"); code != 0 || !strings.Contains(out, "khach_a") || !strings.Contains(out, "--dry-run") {
		t.Errorf("--dry-run: mã %d, %s", code, out)
	}
	if code, out, _ := c.run("user", "2fa", "reset", "khach_a", "--json"); code != 1 || refusalCode(t, out) != "confirm_required" {
		t.Errorf("không --yes: mã %d, %s", code, out)
	}
	c.withStore(func(ctx context.Context, st *store.Store) {
		if on, _ := st.TOTPEnabled(ctx, "khach_a"); !on {
			t.Error("chưa chạy thật mà hai bước đã gỡ")
		}
	})
	if n := c.audits("user.2fa_reset"); n != 0 {
		t.Errorf("audit trước khi chạy thật = %d", n)
	}
	if code, out, _ := c.run("user", "2fa", "reset", "khong_co", "--yes", "--json"); code != 1 || refusalCode(t, out) != "user_not_found" {
		t.Errorf("user lạ: mã %d, %s", code, out)
	}

	code, out, errs := c.run("user", "2fa", "reset", "khach_a", "--yes")
	if code != 0 || !strings.Contains(out, "Đã gỡ hai bước") || !strings.Contains(out, "sudo tpan token revoke "+live) {
		t.Fatalf("mã %d\n%s%s", code, out, errs)
	}
	if strings.Contains(out, "Cũ") {
		t.Errorf("in cả token đã thu hồi:\n%s", out)
	}
	c.withStore(func(ctx context.Context, st *store.Store) {
		if on, _ := st.TOTPEnabled(ctx, "khach_a"); on {
			t.Error("hai bước vẫn bật")
		}
		if n, _ := st.RecoveryLeft(ctx, "khach_a"); n != 0 {
			t.Errorf("còn %d mã dự phòng", n)
		}
		if list, _ := st.Sessions(ctx); len(list) != 0 {
			t.Errorf("còn phiên: %+v", list)
		}
		if n, _ := st.ActiveTokens(ctx, "khach_a", time.Now()); n != 1 {
			t.Errorf("token phải giữ nguyên, còn %d", n)
		}
	})
	if n := c.audits("user.2fa_reset"); n != 1 {
		t.Errorf("audit user.2fa_reset = %d", n)
	}

	// Chưa bật: nói rõ, vẫn thành công (đã đăng xuất mọi phiên), có audit.
	code, out, _ = c.run("user", "2fa", "reset", "khach_a", "--yes", "--json")
	var res struct {
		User         string            `json:"user"`
		WasEnabled   bool              `json:"was_enabled"`
		ActiveTokens []server.TokenRow `json:"active_tokens"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil || code != 0 || res.WasEnabled || len(res.ActiveTokens) != 1 || res.ActiveTokens[0].ID != live {
		t.Errorf("lần hai: mã %d, %v, %s", code, err, out)
	}
	if code, out, _ = c.run("user", "2fa", "reset", "khach_a", "--yes"); code != 0 || !strings.Contains(out, "chưa bật") {
		t.Errorf("lần hai (chữ): mã %d\n%s", code, out)
	}

	// admin (tên dành riêng, ParseUserName từ chối) vẫn gỡ được: đó là lối
	// thoát của admin bị khoá ngoài.
	c.enable2FA("admin")
	if code, out, _ = c.run("user", "2fa", "reset", "admin", "--yes"); code != 0 || !strings.Contains(out, "Đã gỡ hai bước") {
		t.Errorf("reset admin: mã %d\n%s", code, out)
	}
}

func TestUser2FAPolicy(t *testing.T) {
	c := newCLI(t)
	c.seedM20()

	if code, out, _ := c.run("user", "2fa", "policy", "--json"); code != 0 || !strings.Contains(out, `"off"`) {
		t.Errorf("mặc định: mã %d, %s", code, out)
	}
	if code, out, _ := c.run("user", "2fa", "policy", "mọi người", "--json"); code != 1 || refusalCode(t, out) != "policy_invalid" {
		t.Errorf("giá trị lạ: mã %d, %s", code, out)
	}
	if code, out, _ := c.run("user", "2fa", "policy", "admin", "--dry-run"); code != 0 || !strings.Contains(out, "admin") {
		t.Errorf("--dry-run: mã %d, %s", code, out)
	}
	if v, ok := c.settingOK(settingRequire2FA); ok {
		t.Errorf("--dry-run ghi %q", v)
	}
	// Không cần --yes và không cần terminal: là lối thoát khi bị khoá ngoài.
	code, out, errs := c.run("user", "2fa", "policy", "admin")
	if code != 0 || !strings.Contains(out, "admin") {
		t.Fatalf("đặt admin: mã %d\n%s%s", code, out, errs)
	}
	if v, _ := c.settingOK(settingRequire2FA); v != "admin" {
		t.Errorf("setting = %q", v)
	}
	if n := c.audits("auth.require_2fa"); n != 1 {
		t.Errorf("audit auth.require_2fa = %d", n)
	}
	if code, out, _ = c.run("user", "2fa", "policy", "--json"); code != 0 || !strings.Contains(out, `"admin"`) {
		t.Errorf("đọc lại: mã %d, %s", code, out)
	}
	if code, out, _ = c.run("user", "2fa", "policy", "off", "--json"); code != 0 || !strings.Contains(out, `"off"`) {
		t.Errorf("đặt off: mã %d, %s", code, out)
	}
	if v, _ := c.settingOK(settingRequire2FA); v != "off" {
		t.Errorf("setting = %q", v)
	}
}

func TestTokenCreate(t *testing.T) {
	c := newCLI(t)
	c.seedM20()
	create := func(args ...string) (int, string, string) {
		return c.run(append([]string{"token", "create"}, args...)...)
	}

	// --dry-run: kiểm đủ luật mà không ghi gì.
	if code, out, _ := create("khach_a", "--name", "CI", "--scope", "sites:write", "--days", "30", "--dry-run"); code != 0 || !strings.Contains(out, "sites:write") {
		t.Errorf("--dry-run: mã %d\n%s", code, out)
	}
	c.withStore(func(ctx context.Context, st *store.Store) {
		if ts, _ := st.Tokens(ctx, ""); len(ts) != 0 {
			t.Errorf("--dry-run đã tạo token: %+v", ts)
		}
	})

	code, out, errs := create("khach_a", "--name", "CI", "--scope", "sites:write,jobs:read", "--days", "30", "--json")
	var got server.TokenCreated
	if err := json.Unmarshal([]byte(out), &got); err != nil || code != 0 {
		t.Fatalf("mã %d, %v\n%s%s", code, err, out, errs)
	}
	if !apitoken.WellFormed(got.Token) || got.Row.User != "khach_a" || got.Row.Status != "active" || got.Row.Hint != apitoken.Hint(got.Token) ||
		strings.Join(got.Row.Scopes, ",") != "sites:write,jobs:read" {
		t.Errorf("kết quả: %+v", got)
	}
	if d := got.Row.Expires.Sub(got.Row.Created); d < 29*24*time.Hour || d > 31*24*time.Hour {
		t.Errorf("hạn %v, muốn 30 ngày", d)
	}
	// DB giữ sha256, không giữ token; token này không xuất hiện ở đâu khác.
	c.withStore(func(ctx context.Context, st *store.Store) {
		row, err := st.TokenByHash(ctx, apitoken.Hash(got.Token))
		if err != nil || row.ID != got.Row.ID || row.User != "khach_a" {
			t.Errorf("TokenByHash: %+v, %v", row, err)
		}
	})
	if n := c.audits("token.create"); n != 1 {
		t.Errorf("audit token.create = %d", n)
	}
	if strings.Contains(errs, got.Token) {
		t.Error("token lọt vào stderr")
	}

	// Dạng chữ: in token một lần, kèm lời nhắc.
	code, out, _ = create("khach_a", "--name", "Máy B", "--scope", "sites:read", "--days", "7")
	if code != 0 || !strings.Contains(out, "MỘT lần") || !strings.Contains(out, apitoken.Prefix) {
		t.Errorf("dạng chữ: mã %d\n%s", code, out)
	}

	// Danh sách không bao giờ có token.
	if code, out, _ = c.run("token", "list", "--json"); code != 0 || strings.Contains(out, got.Token) || !strings.Contains(out, got.Row.Hint) {
		t.Errorf("token list lộ token hoặc thiếu hint: mã %d\n%s", code, out)
	}

	// Từ chối: không tạo gì thêm.
	for _, tc := range []struct {
		args []string
		code string
	}{
		{[]string{"khong_co", "--name", "n", "--scope", "sites:read", "--days", "1"}, "user_not_found"},
		{[]string{"khach_b", "--name", "n", "--scope", "sites:read", "--days", "1"}, "user_suspended"},
		{[]string{"khach_a", "--name", "n", "--scope", "system:read", "--days", "1"}, "token_scope_invalid"},
		{[]string{"khach_a", "--name", "n", "--scope", "sites:read", "--days", "0"}, "token_days_invalid"},
		{[]string{"khach_a", "--name", "n", "--scope", "sites:read", "--days", "91"}, "token_days_invalid"},
		{[]string{"khach_a", "--name", "", "--scope", "sites:read", "--days", "1"}, "token_name_invalid"},
		{[]string{"khach_a", "--name", "a\nb", "--scope", "sites:read", "--days", "1"}, "token_name_invalid"},
		{[]string{"khach_a", "--name", "n", "--scope", "sites:x", "--days", "1"}, "token_scope_invalid"},
	} {
		code, out, _ := create(append(tc.args, "--json")...)
		if code != 1 || refusalCode(t, out) != tc.code {
			t.Errorf("%q: mã %d, %s; muốn %s", tc.args, code, out, tc.code)
		}
	}
	// admin được chọn nhóm system; trần ngày theo setting.
	if code, out, _ := create("admin", "--name", "n", "--scope", "system:read", "--days", "5", "--json"); code != 0 {
		t.Errorf("admin system:read: mã %d, %s", code, out)
	}
	c.seed(map[string]string{settingTokenMaxDays: "10"})
	if code, out, _ := create("admin", "--name", "n", "--scope", "sites:read", "--days", "11", "--json"); code != 1 || refusalCode(t, out) != "token_days_invalid" {
		t.Errorf("quá trần 10 ngày: mã %d, %s", code, out)
	}
	if code, out, _ := create("admin", "--name", "n", "--scope", "sites:read", "--days", "10", "--json"); code != 0 {
		t.Errorf("đúng trần 10 ngày: mã %d, %s", code, out)
	}
	c.withStore(func(ctx context.Context, st *store.Store) {
		if ts, _ := st.Tokens(ctx, ""); len(ts) != 4 {
			t.Errorf("còn %d token, muốn 4 (hai của khach_a, hai của admin)", len(ts))
		}
	})
}

func TestTokenList(t *testing.T) {
	c := newCLI(t)
	c.seedM20()
	if code, out, _ := c.run("token", "list"); code != 0 || !strings.Contains(out, "Chưa có token") {
		t.Errorf("rỗng: mã %d\n%s", code, out)
	}
	active := c.addToken("khach_a", "Đang dùng", "sites:read", false, false)
	expired := c.addToken("khach_a", "Hết hạn", "jobs:read", false, true)
	revoked := c.addToken("admin", "Thu hồi", "sites:read,jobs:read", true, true)

	code, out, _ := c.run("token", "list", "--json")
	var rows []server.TokenRow
	if err := json.Unmarshal([]byte(out), &rows); err != nil || code != 0 || len(rows) != 3 {
		t.Fatalf("mã %d, %v\n%s", code, err, out)
	}
	status := map[string]string{}
	for _, r := range rows {
		status[r.ID] = r.Status
	}
	// Thu hồi thắng hết hạn.
	if status[active] != "active" || status[expired] != "expired" || status[revoked] != "revoked" {
		t.Errorf("trạng thái: %v", status)
	}
	code, out, _ = c.run("token", "list", "--user", "khach_a", "--json")
	rows = nil
	if err := json.Unmarshal([]byte(out), &rows); err != nil || code != 0 || len(rows) != 2 {
		t.Errorf("--user khach_a: mã %d, %v, %d dòng", code, err, len(rows))
	}
	code, out, _ = c.run("token", "list")
	if code != 0 || !strings.Contains(out, "đang dùng") || !strings.Contains(out, "đã thu hồi") || !strings.Contains(out, "hết hạn") {
		t.Errorf("bảng: mã %d\n%s", code, out)
	}
}

func TestTokenRevoke(t *testing.T) {
	c := newCLI(t)
	c.seedM20()
	id := c.addToken("khach_a", "CI", "sites:read", false, false)

	if code, out, _ := c.run("token", "revoke", id, "--dry-run"); code != 0 || !strings.Contains(out, id) {
		t.Errorf("--dry-run: mã %d\n%s", code, out)
	}
	if code, out, _ := c.run("token", "revoke", id, "--json"); code != 1 || refusalCode(t, out) != "confirm_required" {
		t.Errorf("không --yes: mã %d, %s", code, out)
	}
	c.withStore(func(ctx context.Context, st *store.Store) {
		if n, _ := st.ActiveTokens(ctx, "khach_a", time.Now()); n != 1 {
			t.Errorf("token đã bị thu hồi trước khi chạy thật")
		}
	})
	if code, out, _ := c.run("token", "revoke", "0123456789abcdef", "--yes", "--json"); code != 1 || refusalCode(t, out) != "token_not_found" {
		t.Errorf("id lạ: mã %d, %s", code, out)
	}
	if code, out, errs := c.run("token", "revoke", id, "--yes"); code != 0 || !strings.Contains(out, "Đã thu hồi token "+id) {
		t.Fatalf("mã %d\n%s%s", code, out, errs)
	}
	c.withStore(func(ctx context.Context, st *store.Store) {
		if n, _ := st.ActiveTokens(ctx, "khach_a", time.Now()); n != 0 {
			t.Errorf("còn %d token sống", n)
		}
	})
	if n := c.audits("token.revoke"); n != 1 {
		t.Errorf("audit token.revoke = %d", n)
	}
	// Thu hồi lần hai: từ chối, không ghi audit thêm.
	if code, out, _ := c.run("token", "revoke", id, "--yes", "--json"); code != 1 || refusalCode(t, out) != "token_not_found" {
		t.Errorf("lần hai: mã %d, %s", code, out)
	}
	if n := c.audits("token.revoke"); n != 1 {
		t.Errorf("audit sau lần hai = %d", n)
	}
}

// Đặt lại mật khẩu thu hồi token (store làm); CLI báo số token đã thu hồi.
func TestPasswdBaoSoTokenThuHoi(t *testing.T) {
	c := newCLI(t)
	c.seedM20()
	a1 := c.addToken("admin", "Giám sát", "sites:read", false, false)
	c.addToken("admin", "Cũ", "sites:read", true, false)
	k1 := c.addToken("khach_a", "CI", "sites:write", false, false)
	k2 := c.addToken("khach_a", "CI 2", "sites:write", false, false)

	code, out, errs := c.run("admin", "passwd", "--yes")
	if code != 0 || !strings.Contains(out, "Đã thu hồi 1 token API của admin") {
		t.Fatalf("admin passwd: mã %d\n%s%s", code, out, errs)
	}
	// --json giữ nguyên hình: chỉ có mật khẩu.
	code, out, _ = c.run("admin", "passwd", "--yes", "--json")
	var res map[string]string
	if err := json.Unmarshal([]byte(out), &res); err != nil || code != 0 || len(res) != 1 {
		t.Errorf("admin passwd --json: mã %d, %v, %s", code, err, out)
	}
	c.withStore(func(ctx context.Context, st *store.Store) {
		if n, _ := st.ActiveTokens(ctx, "admin", time.Now()); n != 0 {
			t.Errorf("admin còn %d token sống (%s)", n, a1)
		}
		if n, _ := st.ActiveTokens(ctx, "khach_a", time.Now()); n != 2 {
			t.Errorf("token của khach_a bị đụng tới: còn %d", n)
		}
	})

	// --dry-run không thu hồi và không báo.
	if code, out, _ = c.run("user", "passwd", "khach_a", "--dry-run"); code != 0 || strings.Contains(out, "thu hồi") {
		t.Errorf("user passwd --dry-run: mã %d\n%s", code, out)
	}
	c.withStore(func(ctx context.Context, st *store.Store) {
		if n, _ := st.ActiveTokens(ctx, "khach_a", time.Now()); n != 2 {
			t.Errorf("--dry-run thu hồi token: còn %d", n)
		}
	})
	code, out, errs = c.run("user", "passwd", "khach_a", "--yes")
	if code != 0 || !strings.Contains(out, "Đã thu hồi 2 token API") {
		t.Fatalf("user passwd: mã %d\n%s%s", code, out, errs)
	}
	c.withStore(func(ctx context.Context, st *store.Store) {
		if n, _ := st.ActiveTokens(ctx, "khach_a", time.Now()); n != 0 {
			t.Errorf("khach_a còn %d token sống (%s, %s)", n, k1, k2)
		}
	})
	// Không còn token: không có dòng thu hồi.
	if code, out, _ = c.run("user", "passwd", "khach_a", "--yes"); code != 0 || strings.Contains(out, "thu hồi") {
		t.Errorf("user passwd lần hai: mã %d\n%s", code, out)
	}
}

const panelTestDomain = "panel.example.com"

func TestPanelDomainShow(t *testing.T) {
	c := newCLI(t)
	// Chưa có DB: không tạo, nói chưa đặt.
	code, out, _ := c.run("panel", "domain", "show")
	if code != 0 || !strings.Contains(out, "chưa có tên miền") || c.dbExists() {
		t.Errorf("chưa có DB: mã %d\n%s", code, out)
	}
	c.seed(map[string]string{core.SettingPanelDomain: panelTestDomain})
	code, out, _ = c.run("panel", "domain", "show", "--json")
	var st server.PanelState
	if err := json.Unmarshal([]byte(out), &st); err != nil || code != 0 || st.Domain != panelTestDomain || st.Cert == nil {
		t.Fatalf("--json: mã %d, %v\n%s", code, err, out)
	}
	// Máy giả không có chứng chỉ: phép kiểm Fail kèm cách sửa.
	if st.Cert.Status != "fail" {
		t.Errorf("cert = %s, muốn fail (không có file)", st.Cert.Status)
	}
	code, out, _ = c.run("panel", "domain", "show")
	if code != 0 || !strings.Contains(out, panelTestDomain) || !strings.Contains(out, "[lỗi]") {
		t.Errorf("dạng chữ: mã %d\n%s", code, out)
	}
}

func TestPanelDomainSetRefusals(t *testing.T) {
	c := newCLI(t)
	// Chưa có DB, --dry-run: không cần root, không tạo DB; chưa cài stack.
	c.root = false
	code, out, _ := c.run("panel", "domain", "set", panelTestDomain, "--dry-run", "--json")
	if code != 1 || refusalCode(t, out) != "stack_not_installed" || c.dbExists() {
		t.Errorf("chưa có stack: mã %d, %s", code, out)
	}
	if code, out, _ = c.run("panel", "domain", "set", "--clear", "--dry-run", "--json"); code != 1 || refusalCode(t, out) != "panel_domain_not_set" || c.dbExists() {
		t.Errorf("--clear khi chưa đặt: mã %d, %s", code, out)
	}
	c.root = true

	// Site đã dùng tên đó.
	c.seed(stackNginx, panelTestDomain)
	if code, out, _ = c.run("panel", "domain", "set", panelTestDomain, "--dry-run", "--json"); code != 1 || refusalCode(t, out) != "panel_domain_is_site" {
		t.Errorf("tên của site: mã %d, %s", code, out)
	}
	// Staging mặc định bị từ chối (chứng chỉ thử không dùng cho panel).
	c.seed(map[string]string{core.SettingSSLEmail: "admin@example.com"})
	c.dnsHere()
	if code, out, _ = c.run("panel", "domain", "set", "panel2.example.com", "--dry-run", "--json"); code != 1 || refusalCode(t, out) != "panel_staging" {
		t.Errorf("staging: mã %d, %s", code, out)
	}
}

func TestPanelDomainSetDryRun(t *testing.T) {
	c := newCLI(t)
	c.seed(map[string]string{core.SettingSSLEmail: "admin@example.com", core.SettingSSLStaging: "0"})
	c.seed(stackNginx)
	c.dnsHere()
	before := c.fake.Snapshot()
	code, out, errs := c.run("panel", "domain", "set", panelTestDomain, "--dry-run")
	if code != 0 || !strings.Contains(out, "Kế hoạch:") || !strings.Contains(out, "--cert-name tpan-panel") || !strings.Contains(out, panelTestDomain) {
		t.Fatalf("mã %d\n%s%s", code, out, errs)
	}
	if v, ok := c.settingOK(core.SettingPanelDomain); ok && v != "" {
		t.Errorf("--dry-run ghi panel.domain = %q", v)
	}
	if len(c.fake.Snapshot()) != len(before) {
		t.Error("--dry-run đổi hệ thống file")
	}
	for _, m := range c.mutating() {
		if !strings.HasPrefix(m, "getent ") && !strings.HasPrefix(m, "ip ") && !strings.Contains(m, "wp option get") {
			t.Errorf("--dry-run chạy lệnh thay đổi máy: %s", m)
		}
	}
	// Không --yes, không terminal: hỏi lại được thì mới chạy.
	if code, out, _ = c.run("panel", "domain", "set", panelTestDomain, "--json"); code != 1 || refusalCode(t, out) != "confirm_required" {
		t.Errorf("không --yes: mã %d, %s", code, out)
	}
}

// --clear chạy thật: bỏ setting, ghi audit, và xoá phiên đăng nhập mang tên cũ
// (core.Panel cần Sessions; nil thì job báo lỗi sau khi đã đổi tên).
func TestPanelDomainClear(t *testing.T) {
	c := newCLI(t)
	c.seedM20()
	c.seed(map[string]string{core.SettingPanelDomain: panelTestDomain})
	c.withStore(func(ctx context.Context, st *store.Store) {
		now := time.Now()
		for _, s := range []store.Session{
			{TokenSHA256: "h1", ID: "h1", IP: "192.0.2.1", Created: now, UserName: "admin", Host: panelTestDomain},
			{TokenSHA256: "h2", ID: "h2", IP: "192.0.2.1", Created: now, UserName: "admin", Host: "198.51.100.7"},
		} {
			if err := st.CreateSession(ctx, s); err != nil {
				t.Fatal(err)
			}
		}
	})

	if code, out, _ := c.run("panel", "domain", "set", "--clear", "--dry-run"); code != 0 || !strings.Contains(out, "Kế hoạch:") {
		t.Errorf("--dry-run: mã %d\n%s", code, out)
	}
	if v, _ := c.settingOK(core.SettingPanelDomain); v != panelTestDomain {
		t.Fatalf("--dry-run đổi setting: %q", v)
	}
	code, out, errs := c.run("panel", "domain", "set", "--clear", "--yes")
	if code != 0 {
		t.Fatalf("mã %d\n%s%s", code, out, errs)
	}
	if v, _ := c.settingOK(core.SettingPanelDomain); v != "" {
		t.Errorf("panel.domain = %q sau --clear", v)
	}
	c.withStore(func(ctx context.Context, st *store.Store) {
		list, _ := st.Sessions(ctx)
		if len(list) != 1 || list[0].Host != "198.51.100.7" {
			t.Errorf("phiên sau --clear: %+v", list)
		}
	})
	if n := c.audits("panel.domain.clear"); n != 1 {
		t.Errorf("audit panel.domain.clear = %d", n)
	}
	if code, out, _ = c.run("panel", "domain", "show"); code != 0 || !strings.Contains(out, "chưa có tên miền") {
		t.Errorf("show sau --clear: mã %d\n%s", code, out)
	}
}
