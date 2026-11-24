package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/store"
)

func TestAdminPasswd(t *testing.T) {
	c := newCLI(t)
	ctx := context.Background()
	now := time.Now()

	// Trạng thái trước: có hash cũ, một phiên, một IP đang bị khoá.
	st, err := store.Open(ctx, c.db)
	if err != nil {
		t.Fatal(err)
	}
	_ = st.SetAdminPasswordHash(ctx, "$cu")
	_ = st.CreateSession(ctx, store.Session{TokenSHA256: "aa", ID: "a", IP: "192.0.2.1", Created: now})
	_, _ = st.RecordLoginFailure(ctx, "192.0.2.1", now, 1, time.Hour, time.Hour)
	_ = st.Close()

	// Không terminal, không --yes: từ chối, không đổi gì.
	if code, _, _ := c.run("admin", "passwd"); code != 1 {
		t.Fatalf("không --yes: mã %d, muốn 1", code)
	}
	// --dry-run: không đổi gì.
	if code, _, _ := c.run("admin", "passwd", "--dry-run"); code != 0 {
		t.Fatal("--dry-run lỗi")
	}
	st, _ = store.Open(ctx, c.db)
	if h, _, _ := st.AdminPasswordHash(ctx); h != "$cu" {
		t.Fatalf("chưa chạy thật mà hash đã đổi: %q", h)
	}
	_ = st.Close()

	code, out, errb := c.run("admin", "passwd", "--yes", "--json")
	if code != 0 {
		t.Fatalf("mã %d\n%s\n%s", code, out, errb)
	}
	var res map[string]string
	if err := json.Unmarshal([]byte(out), &res); err != nil || len(res["admin_password"]) < 20 {
		t.Fatalf("output: %s", out)
	}

	st, _ = store.Open(ctx, c.db)
	defer func() { _ = st.Close() }()
	h, _, _ := st.AdminPasswordHash(ctx)
	if ok, err := store.VerifyPassword(h, res["admin_password"]); !ok || err != nil {
		t.Fatal("mật khẩu in ra không khớp hash trong DB")
	}
	if list, _ := st.Sessions(ctx); len(list) != 0 {
		t.Fatalf("phải huỷ mọi phiên: %+v", list)
	}
	if _, locked, _ := st.LoginLockedUntil(ctx, "192.0.2.1", now); locked {
		t.Fatal("phải gỡ khoá IP")
	}

	// Nhật ký ghi IP "cli" (đọc thẳng bảng audit: store chưa có hàm đọc).
	db, err := sql.Open("sqlite", c.db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var n int
	if err := db.QueryRow("SELECT count(*) FROM audit WHERE action = 'admin.passwd' AND ip = 'cli' AND result = 'ok'").Scan(&n); err != nil || n != 1 {
		t.Fatalf("audit admin.passwd: %d dòng, %v", n, err)
	}
}

func TestAdminPasswdCanRoot(t *testing.T) {
	c := newCLI(t)
	c.root = false
	if code, _, _ := c.run("admin", "passwd", "--yes"); code != 1 || c.dbExists() {
		t.Fatal("không root phải bị từ chối trước khi mở DB")
	}
}

// tpan admin unlock (M28 đợt 2, S10): gỡ khoá của một nguồn hay mọi nguồn,
// không đổi mật khẩu, không huỷ phiên.
func TestAdminUnlock(t *testing.T) {
	c := newCLI(t)
	ctx := context.Background()
	now := time.Now()
	st, err := store.Open(ctx, c.db)
	if err != nil {
		t.Fatal(err)
	}
	_ = st.SetAdminPasswordHash(ctx, "$cu")
	_ = st.CreateSession(ctx, store.Session{TokenSHA256: "aa", ID: "a", IP: "192.0.2.1", Created: now})
	for _, k := range []string{"uid:1001", "203.0.113.5", "2001:db8:1:2::/64"} {
		_, _ = st.RecordLoginFailure(ctx, k, now, 1, time.Hour, time.Hour)
	}
	_ = st.Close()
	locked := func(k string) bool {
		t.Helper()
		st, _ := store.Open(ctx, c.db)
		defer func() { _ = st.Close() }()
		_, l, _ := st.LoginLockedUntil(ctx, k, now)
		return l
	}

	if code, out, errb := c.run("admin", "unlock", "uid:1001"); code != 0 || !strings.Contains(out, "uid:1001") {
		t.Fatalf("unlock uid:1001: mã %d\n%s\n%s", code, out, errb)
	}
	if locked("uid:1001") || !locked("203.0.113.5") {
		t.Fatal("chỉ được gỡ đúng khoá nguồn đã nêu")
	}
	// Nguồn không bị khoá: vẫn thành công, nói rõ không có gì để gỡ.
	if code, out, _ := c.run("admin", "unlock", "198.51.100.1", "--json"); code != 0 || !strings.Contains(out, `"unlocked": 0`) {
		t.Fatalf("nguồn không khoá: mã %d %s", code, out)
	}
	// Chuỗi lạ bị từ chối trước khi mở DB, không vào nhật ký.
	for _, bad := range []string{"a b", "x\ny", strings.Repeat("a", 65), "Đ"} {
		if code, _, _ := c.run("admin", "unlock", bad); code != 1 {
			t.Fatalf("khoá nguồn %q phải bị từ chối", bad)
		}
	}
	code, out, _ := c.run("admin", "unlock", "--json")
	if code != 0 || !strings.Contains(out, `"unlocked": 2`) {
		t.Fatalf("unlock hết: mã %d %s", code, out)
	}
	if locked("203.0.113.5") || locked("2001:db8:1:2::/64") {
		t.Fatal("unlock không đối số phải gỡ mọi khoá")
	}

	st, _ = store.Open(ctx, c.db)
	defer func() { _ = st.Close() }()
	if h, _, _ := st.AdminPasswordHash(ctx); h != "$cu" {
		t.Fatalf("unlock không được đổi mật khẩu: %q", h)
	}
	if list, _ := st.Sessions(ctx); len(list) != 1 {
		t.Fatalf("unlock không được huỷ phiên: %+v", list)
	}
	db, err := sql.Open("sqlite", c.db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var n int
	if err := db.QueryRow("SELECT count(*) FROM audit WHERE action = 'admin.unlock' AND ip = 'cli'").Scan(&n); err != nil || n != 3 {
		t.Fatalf("audit admin.unlock: %d dòng, %v; muốn 3", n, err)
	}
}

func TestAdminUnlockCanRoot(t *testing.T) {
	c := newCLI(t)
	c.root = false
	if code, _, _ := c.run("admin", "unlock"); code != 1 || c.dbExists() {
		t.Fatal("không root phải bị từ chối trước khi mở DB")
	}
}
