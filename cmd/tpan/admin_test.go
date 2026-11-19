package main

import (
	"context"
	"database/sql"
	"encoding/json"
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
	_, _, _ = st.RecordLoginFailure(ctx, "192.0.2.1", now, 1, time.Hour, time.Hour)
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
