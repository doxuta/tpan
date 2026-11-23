package server

// Test của M28 đợt 2 cho khoá đăng nhập (P1: E6, E7, E8, S1, S9, S10) và lời
// 429 khi argon2 bận (P2, S11). Store SQLite thật, đồng hồ giả; hàm so mật
// khẩu được thay bằng bản đếm (móc s.auth.verify) để biết đúng số lần so.

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

const khach1Password = "mat-khau-khach1"

// hookEnv là authEnv có user admin và khach1, và hàm so mật khẩu giả: hash là
// chuỗi "$admin", "$khach1" (không phải argon2), so bằng bảng pwOf.
type hookEnv struct {
	*authEnv
	s     *Server
	sys   *system.FakeSystem
	calls atomic.Int32 // số lần so mật khẩu (kể cả tên lạ, so với hash giả)
	// block khác nil thì mỗi lần so chờ nó đóng (test hàng chờ đầy).
	block chan struct{}
	// dbPath để đọc bảng audit bằng kết nối riêng (store không có hàm đọc).
	dbPath string
}

func newHookEnv(t *testing.T) *hookEnv {
	t.Helper()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "tpan.db")
	st, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.SetAdminPasswordHash(ctx, "$admin"); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateUser(ctx, account.User{Name: "khach1", Role: account.RoleUser}, "$khach1"); err != nil {
		t.Fatal(err)
	}
	he := &hookEnv{authEnv: &authEnv{t: t, st: st, now: authT0, tls: true}, sys: system.NewFake(), dbPath: dbPath}
	he.s = newServer(Deps{Store: st, Sys: he.sys, Hostname: "may-thu", Now: func() time.Time { return he.now }})
	pwOf := map[string]string{"$admin": authTestPassword, "$khach1": khach1Password}
	he.s.auth.verify = func(hash, pw string) (bool, error) {
		he.calls.Add(1)
		if he.block != nil {
			<-he.block
		}
		// Giữ lượt một chút để các request song song kịp chồng lên nhau.
		time.Sleep(2 * time.Millisecond)
		want, ok := pwOf[hash]
		return ok && want == pw, nil
	}
	he.h = he.s.handler(fstest.MapFS{"index.html": {Data: []byte("<html>tpan</html>")}})
	return he
}

func (e *authEnv) loginAs(name, pw, ip string) *httptest.ResponseRecorder {
	e.t.Helper()
	b, _ := json.Marshal(LoginRequest{Name: name, Password: pw})
	return e.req(http.MethodPost, "/api/v1/login", string(b), ip, nil)
}

// expect gửi một lần đăng nhập và đòi đúng mã.
func (e *authEnv) expect(name, pw, ip string, want int) {
	e.t.Helper()
	if rec := e.loginAs(name, pw, ip); rec.Code != want {
		e.t.Fatalf("đăng nhập %q từ %s: mã %d, muốn %d (%s)", name, ip, rec.Code, want, rec.Body)
	}
}

// E6: một nguồn không bao giờ được so mật khẩu sai quá 5 lần trước khi bị 423,
// dù bắn song song. Lần sau, dùng ĐÚNG mật khẩu admin, vẫn 423.
func TestE6SongSongKhongQuaNamLanSo(t *testing.T) {
	he := newHookEnv(t)
	const ip = "198.51.100.7"
	var mu sync.Mutex
	codes := map[int]int{}
	for burst := range 6 {
		var wg sync.WaitGroup
		for i := range 8 {
			wg.Go(func() {
				rec := he.loginAs("admin", fmt.Sprintf("doan-%d-%d", burst, i), ip)
				mu.Lock()
				codes[rec.Code]++
				mu.Unlock()
			})
		}
		wg.Wait()
	}
	if n := he.calls.Load(); n != authMaxFailures {
		t.Fatalf("đã so mật khẩu %d lần, muốn đúng %d (mã: %v)", n, authMaxFailures, codes)
	}
	for c := range codes {
		if c != http.StatusUnauthorized && c != http.StatusLocked && c != http.StatusTooManyRequests {
			t.Fatalf("mã lạ %d (mã: %v)", c, codes)
		}
	}
	if codes[http.StatusUnauthorized] != authMaxFailures-1 || codes[http.StatusLocked] == 0 {
		t.Fatalf("muốn 4 lần 401 và có 423: %v", codes)
	}
	he.expect("admin", authTestPassword, ip, http.StatusLocked)
	if n := he.calls.Load(); n != authMaxFailures {
		t.Fatalf("nguồn đang khoá mà vẫn so mật khẩu: %d lần", n)
	}

	// Cùng khoá nguồn: IPv4 ánh xạ trong IPv6, và hai địa chỉ của một dải /64.
	he.expect("admin", authTestPassword, "[::ffff:198.51.100.7]", http.StatusLocked)
	for range authMaxFailures - 1 {
		he.expect("admin", "sai", "[2001:db8:1:2::1]", http.StatusUnauthorized)
	}
	he.expect("admin", "sai", "[2001:db8:1:2::1]", http.StatusLocked)
	he.expect("admin", authTestPassword, "[2001:db8:1:2:ffff::9]", http.StatusLocked)
	// Nguồn khác không bị ảnh hưởng.
	he.expect("admin", authTestPassword, "198.51.100.8", http.StatusNoContent)
	he.expect("admin", authTestPassword, "[2001:db8:1:3::1]", http.StatusNoContent)
}

// E7: đăng nhập đúng chỉ xoá lần sai của chính tên đó. 4 lần sai admin, 1 lần
// đúng khach1, 1 lần sai admin thì khoá.
func TestE7XenDangNhapDungCuaTenKhac(t *testing.T) {
	he := newHookEnv(t)
	const ip = "198.51.100.7"
	for range 4 {
		he.expect("admin", "doan", ip, http.StatusUnauthorized)
	}
	he.expect("khach1", khach1Password, ip, http.StatusNoContent)
	he.expect("admin", "doan", ip, http.StatusLocked)
}

// E8: admin gõ sai 4 lần rồi gõ đúng thì vào được, và bộ đếm của admin về 0.
// Thêm (S10): nguồn đã bị người khác làm khoá thì admin cũng 423, gỡ bằng
// ClearLoginFailures (việc của tpan admin unlock) là vào được.
func TestE8SaiBonLanRoiDung(t *testing.T) {
	he := newHookEnv(t)
	const ip = "198.51.100.7"
	for range 4 {
		he.expect("", "go-nham", ip, http.StatusUnauthorized) // tên rỗng là admin
	}
	he.expect("admin", authTestPassword, ip, http.StatusNoContent)
	for range 4 {
		he.expect("admin", "go-nham", ip, http.StatusUnauthorized)
	}
	he.expect("admin", "go-nham", ip, http.StatusLocked)

	const other = "203.0.113.5"
	for range 5 {
		he.loginAs("khach1", "doan", other)
	}
	he.expect("admin", authTestPassword, other, http.StatusLocked)
	if found, err := he.st.ClearLoginFailures(context.Background(), other); err != nil || !found {
		t.Fatalf("gỡ khoá %s: %v %v", other, found, err)
	}
	he.expect("admin", authTestPassword, other, http.StatusNoContent)
}

// S9: phần theo tên chỉ trừ vào đúng cửa sổ đã đếm. Lần sai của cửa sổ trước
// không được "gửi" sang trừ lần đoán admin của cửa sổ sau.
func TestS9HaiCuaSo(t *testing.T) {
	he := newHookEnv(t)
	const ip = "198.51.100.7"
	for range 4 {
		he.expect("khach1", "go-nham", ip, http.StatusUnauthorized)
	}
	he.now = he.now.Add(authFailWindow + time.Minute) // DB đếm lại từ 1 ở lần sai kế
	for range 4 {
		he.expect("admin", "doan", ip, http.StatusUnauthorized)
	}
	he.expect("khach1", khach1Password, ip, http.StatusNoContent)
	he.expect("admin", "doan", ip, http.StatusLocked)

	// Khoá hết hạn: phần theo tên của cửa sổ cũ bỏ hết; lần sai mới đếm từ 1.
	he.now = he.now.Add(authLockFor + time.Minute)
	for range 4 {
		he.expect("admin", "doan", ip, http.StatusUnauthorized)
	}
	he.expect("admin", "doan", ip, http.StatusLocked)
}

// Bảng theo tên: không giữ dòng quá cửa sổ, không vượt authNamesMax.
func TestAuthNamesDon(t *testing.T) {
	old := authNamesMax
	authNamesMax = 50
	t.Cleanup(func() { authNamesMax = old })
	tb := newAuthNames()
	f := store.LoginFailure{Failures: 1, FirstAt: authT0}
	tb.failed("a", "x", f, authT0)
	tb.failed("b", "y", f, authT0)
	// DB của "b" đếm lại từ 1 (first_at mới): dòng cũ của "b" bỏ, của "a" giữ.
	reset := store.LoginFailure{Failures: 1, FirstAt: authT0.Add(time.Minute)}
	tb.failed("b", "z", reset, reset.FirstAt)
	if _, ok := tb.m[tb.id("b", "y")]; ok {
		t.Fatal("first_at của nguồn đổi mà dòng cũ của nguồn đó còn")
	}
	if _, ok := tb.m[tb.id("a", "x")]; !ok {
		t.Fatal("first_at của nguồn khác đổi không được xoá dòng của a")
	}
	tb.dropSource("b")
	if len(tb.m) != 1 {
		t.Fatalf("dropSource b: còn %d dòng, muốn 1", len(tb.m))
	}
	later := store.LoginFailure{Failures: 1, FirstAt: authT0.Add(authFailWindow + time.Second)}
	tb.failed("c", "z", later, later.FirstAt)
	if len(tb.m) != 1 {
		t.Fatalf("dòng quá cửa sổ phải bị dọn khi ghi: còn %d dòng", len(tb.m))
	}
	for i := range authNamesMax + 5 {
		tb.failed(fmt.Sprintf("src-%d", i), "x", later, later.FirstAt.Add(time.Duration(i)))
	}
	if len(tb.m) != authNamesMax {
		t.Fatalf("bảng có %d dòng, trần %d", len(tb.m), authNamesMax)
	}
	if _, ok := tb.m[tb.id("c", "z")]; ok {
		t.Fatal("đầy thì phải bỏ dòng ghi lâu nhất")
	}
	if n, _, ok := tb.take("src-9", "x", later.FirstAt); !ok || n != 1 {
		t.Fatalf("take: %d %v", n, ok)
	}
	if _, _, ok := tb.take("src-9", "x", later.FirstAt); ok {
		t.Fatal("take hai lần")
	}
}

// S11 và P2: argon2 bận thì tên thật, tên lạ, tên quá dài đều ra cùng 429
// auth_busy (Retry-After: 3), và không tính là lần sai.
func TestS11BanCungMa(t *testing.T) {
	he := newHookEnv(t)
	he.s.auth.verify = func(string, string) (bool, error) { return false, fmt.Errorf("bọc: %w", store.ErrPasswordBusy) }
	const ip = "198.51.100.7"
	for range 3 {
		for _, name := range []string{"admin", "khach1", "khong-co-ai", strings.Repeat("x", authMaxName+1)} {
			rec := he.loginAs(name, "doan", ip)
			if rec.Code != http.StatusTooManyRequests || errCode(t, rec) != "auth_busy" || rec.Header().Get("Retry-After") != "3" {
				t.Fatalf("tên %.10q khi argon2 bận: mã %d %s", name, rec.Code, rec.Body)
			}
		}
	}
	if _, l, _ := he.st.LoginLockedUntil(context.Background(), ip, he.now); l {
		t.Fatal("429 không được tính là lần sai")
	}
}

// Hàng chờ của một nguồn đầy (authMaxQueue lượt) thì lượt kế nhận 429 ngay;
// xong thì bảng hàng chờ không còn dòng nào.
func TestHangChoDay(t *testing.T) {
	he := newHookEnv(t)
	he.block = make(chan struct{})
	const ip = "198.51.100.7"
	var wg sync.WaitGroup
	for range authMaxQueue {
		wg.Go(func() { he.loginAs("admin", "doan", ip) })
	}
	queued := func() int {
		he.s.auth.gates.mu.Lock()
		defer he.s.auth.gates.mu.Unlock()
		if g := he.s.auth.gates.m[ip]; g != nil {
			return g.n
		}
		return 0
	}
	for deadline := time.Now().Add(5 * time.Second); queued() != authMaxQueue; {
		if time.Now().After(deadline) {
			close(he.block)
			t.Fatalf("hàng chờ có %d lượt, muốn %d", queued(), authMaxQueue)
		}
		time.Sleep(time.Millisecond)
	}
	rec := he.loginAs("admin", authTestPassword, ip)
	close(he.block)
	wg.Wait()
	if rec.Code != http.StatusTooManyRequests || errCode(t, rec) != "auth_busy" {
		t.Fatalf("lượt thứ %d: mã %d %s, muốn 429", authMaxQueue+1, rec.Code, rec.Body)
	}
	he.s.auth.gates.mu.Lock()
	left := len(he.s.auth.gates.m)
	he.s.auth.gates.mu.Unlock()
	if left != 0 {
		t.Fatalf("bảng hàng chờ còn %d dòng", left)
	}
	// Một nguồn đầy không chặn nguồn khác.
	he.expect("admin", authTestPassword, "198.51.100.8", http.StatusNoContent)
}

// P2: đổi mật khẩu khi argon2 bận lúc băm mật khẩu mới: 429, không đổi gì.
func TestDoiMatKhauBan(t *testing.T) {
	he := newHookEnv(t)
	rec := he.loginAs("admin", authTestPassword, "192.0.2.1")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("đăng nhập: %d", rec.Code)
	}
	c := rec.Result().Cookies()[0]
	he.s.auth.hash = func(string) (string, error) { return "", store.ErrPasswordBusy }
	raw, _ := json.Marshal(PasswordChange{Current: authTestPassword, New: "mat-khau-moi-rat-dai"})
	rec = he.req(http.MethodPost, "/api/v1/account/password", string(raw), "192.0.2.1", c)
	if rec.Code != http.StatusTooManyRequests || errCode(t, rec) != "auth_busy" || rec.Header().Get("Retry-After") != "3" {
		t.Fatalf("đổi mật khẩu khi bận: mã %d %s", rec.Code, rec.Body)
	}
	if h, _, _ := he.st.AdminPasswordHash(context.Background()); h != "$admin" {
		t.Fatalf("hash đã đổi: %q", h)
	}
}

// P2: route tạo user, đặt lại mật khẩu (core.Accounts bọc lỗi băm bằng %w)
// đổi ErrPasswordBusy thành 429 auth_busy ở auditDone.
func TestAuditDoneBan(t *testing.T) {
	he := newHookEnv(t)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/users", nil)
	if he.s.auditDone(rec, r, "user.add", "x", fmt.Errorf("băm mật khẩu: %w", store.ErrPasswordBusy)) {
		t.Fatal("auditDone với lỗi bận phải trả false")
	}
	if rec.Code != http.StatusTooManyRequests || errCode(t, rec) != "auth_busy" || rec.Header().Get("Retry-After") != "3" {
		t.Fatalf("mã %d %s", rec.Code, rec.Body)
	}
}

// ---------------------------------------------------------------- loopback

// procLine dựng một dòng /proc/net/tcp cho socket IPv4 local→remote của uid,
// theo cách kernel little endian in: bốn byte của địa chỉ viết ngược.
func procLine(sl int, local, remote string, uid, inode int, state string) string {
	enc := func(ap string) string {
		h, p, _ := net.SplitHostPort(ap)
		ip := net.ParseIP(h).To4()
		var port int
		_, _ = fmt.Sscan(p, &port)
		return fmt.Sprintf("%02X%02X%02X%02X:%04X", ip[3], ip[2], ip[1], ip[0], port)
	}
	return fmt.Sprintf("  %2d: %s %s %s 00000000:00000000 00:00000000 00000000 %5d        0 %d 1 0000000000000000 20 4 30 10 -1",
		sl, enc(local), enc(remote), state, uid, inode)
}

const procHeader = "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode"

// S1: PHP của một site đổi địa chỉ nguồn trong 127/8 vẫn chung một khoá UID;
// admin vào qua ssh -L (UID khác) không bị khoá theo.
func TestLoopbackTheoUID(t *testing.T) {
	he := newHookEnv(t)
	const panel = "127.0.0.1:8443"
	lines := []string{procHeader, procLine(0, "0.0.0.0:8443", "0.0.0.0:0", 0, 100, "0A")}
	for i := 1; i <= 6; i++ {
		client := fmt.Sprintf("127.0.1.%d:%d", i, 51000+i)
		lines = append(lines,
			procLine(i, client, panel, 1001, 2000+i, "01"),
			procLine(10+i, panel, client, 0, 3000+i, "01")) // socket của chính panel
	}
	lines = append(lines, procLine(20, "127.0.0.1:40000", panel, 0, 4000, "01"))
	he.sys.SetFile("/proc/net/tcp", []byte(strings.Join(lines, "\n")+"\n"), 0o444, system.Owner{})

	loop := func(remote, pw string) int {
		b, _ := json.Marshal(LoginRequest{Name: "admin", Password: pw})
		r := httptest.NewRequest(http.MethodPost, "/api/v1/login", strings.NewReader(string(b)))
		r.RemoteAddr = remote
		local, _ := net.ResolveTCPAddr("tcp", panel)
		r = r.WithContext(context.WithValue(r.Context(), http.LocalAddrContextKey, local))
		r.TLS = &tls.ConnectionState{}
		rec := httptest.NewRecorder()
		he.h.ServeHTTP(rec, r)
		return rec.Code
	}
	for i := 1; i <= 5; i++ {
		want := http.StatusUnauthorized
		if i == 5 {
			want = http.StatusLocked
		}
		if c := loop(fmt.Sprintf("127.0.1.%d:%d", i, 51000+i), "doan"); c != want {
			t.Fatalf("lần đoán %d từ địa chỉ 127.0.1.%d: mã %d, muốn %d", i, i, c, want)
		}
	}
	if c := loop("127.0.1.6:51006", authTestPassword); c != http.StatusLocked {
		t.Fatalf("địa chỉ 127.x mới của cùng UID: mã %d, muốn 423", c)
	}
	if c := loop("127.0.0.1:40000", authTestPassword); c != http.StatusNoContent {
		t.Fatalf("admin qua ssh -L (uid 0): mã %d, muốn 204", c)
	}
	if _, l, _ := he.st.LoginLockedUntil(context.Background(), "uid:1001", he.now); !l {
		t.Fatal("khoá phải nằm ở uid:1001")
	}
}

// sourceKey: bảng chuẩn hoá, và cách đọc /proc/net/tcp, tcp6 (dòng viết tay,
// không dựng bằng cùng công thức với mã).
func TestSourceKey(t *testing.T) {
	sys := system.NewFake()
	tcp := strings.Join([]string{procHeader,
		"   0: 00000000:20FB 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 100 1 0000000000000000 100 0 0 10 0",
		// 127.0.1.5:51234 -> 127.0.0.1:8443, uid 1001
		"   1: 0501007F:C822 0100007F:20FB 01 00000000:00000000 00:00000000 00000000  1001        0 4242 1 0000000000000000 20 4 30 10 -1",
		// chiều ngược lại (socket của panel), uid 0
		"   2: 0100007F:20FB 0501007F:C822 01 00000000:00000000 00:00000000 00000000     0        0 4243 1 0000000000000000 20 4 30 10 -1",
		// 127.0.0.9:40002 -> panel, TIME_WAIT: uid 0, inode 0
		"   3: 0900007F:9C42 0100007F:20FB 06 00000000:00000000 03:00001000 00000000     0        0 0 3 0000000000000000",
		// 127.0.0.8:40003 -> panel: hai dòng khác UID (kernel không sinh ra; dừng ở dòng đầu)
		"   4: 0800007F:9C43 0100007F:20FB 01 00000000:00000000 00:00000000 00000000  1001        0 5000 1 0000000000000000 20 4 30 10 -1",
		"   5: 0800007F:9C43 0100007F:20FB 01 00000000:00000000 00:00000000 00000000  1002        0 5001 1 0000000000000000 20 4 30 10 -1",
		// 203.0.113.9:40004 -> 203.0.113.9:8443 (IP công khai của chính máy), uid 1004; chữ hex thường
		"   6: 097100cb:9c44 097100cb:20fb 01 00000000:00000000 00:00000000 00000000  1004        0 5002 1 0000000000000000 20 4 30 10 -1",
	}, "\n")
	tcp6 := strings.Join([]string{procHeader,
		// [::1]:40001 -> [::1]:8443, uid 1002
		"   0: 00000000000000000000000001000000:9C41 00000000000000000000000001000000:20FB 01 00000000:00000000 00:00000000 00000000  1002        0 6000 1 0000000000000000 20 4 30 10 -1",
		// ::ffff:127.0.1.7:50000 -> ::ffff:127.0.0.1:8443, uid 1003
		"   1: 0000000000000000FFFF00000701007F:C350 0000000000000000FFFF00000100007F:20FB 01 00000000:00000000 00:00000000 00000000  1003        0 6001 1 0000000000000000 20 4 30 10 -1",
	}, "\n")
	sys.SetFile("/proc/net/tcp", []byte(tcp), 0o444, system.Owner{})
	sys.SetFile("/proc/net/tcp6", []byte(tcp6), 0o444, system.Owner{})

	v4 := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8443}
	v6 := &net.TCPAddr{IP: net.ParseIP("::1"), Port: 8443}
	other := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 9999}
	cases := []struct {
		remote string
		local  net.Addr
		noSys  bool
		want   string
	}{
		{remote: "203.0.113.5:1", want: "203.0.113.5"},
		{remote: "[::ffff:203.0.113.5]:1", want: "203.0.113.5"},
		{remote: "[2001:db8:1:2:aaaa::1]:1", want: "2001:db8:1:2::/64"},
		{remote: "[fe80::1%eth0]:1", want: "fe80::/64"},
		{remote: "127.0.1.5:51234", local: v4, want: "uid:1001"},
		{remote: "127.0.1.5:51234", local: v4, noSys: true, want: authLoopback},
		{remote: "127.0.1.5:51234", want: authLoopback},                                                                  // không biết cổng panel
		{remote: "127.0.1.5:51234", local: other, want: authLoopback},                                                    // remote phải là cổng panel
		{remote: "127.0.0.9:40002", local: v4, want: authLoopback},                                                       // TIME_WAIT, inode 0
		{remote: "127.0.0.8:40003", local: v4, want: "uid:1001"},                                                         // hai UID: dòng hợp lệ đầu tiên
		{remote: "203.0.113.9:40004", local: &net.TCPAddr{IP: net.ParseIP("203.0.113.9"), Port: 8443}, want: "uid:1004"}, // nối tới IP của chính máy (D8)
		{remote: "203.0.113.9:40005", local: &net.TCPAddr{IP: net.ParseIP("203.0.113.9"), Port: 8443}, want: authLoopback},
		{remote: "203.0.113.8:40004", local: &net.TCPAddr{IP: net.ParseIP("203.0.113.9"), Port: 8443}, want: "203.0.113.8"},
		{remote: "127.0.0.7:1", local: v4, want: authLoopback}, // không thấy dòng
		{remote: "[::1]:40001", local: v6, want: "uid:1002"},
		{remote: "127.0.1.7:50000", local: v4, want: "uid:1003"}, // client AF_INET6 ánh xạ
		{remote: "[::ffff:127.0.1.7]:50000", local: v4, want: "uid:1003"},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = c.remote
		ctx := r.Context()
		if !c.noSys {
			ctx = context.WithValue(ctx, authSysKey{}, system.System(sys))
		}
		if c.local != nil {
			ctx = context.WithValue(ctx, http.LocalAddrContextKey, c.local)
		}
		if got := sourceKey(r.WithContext(ctx)); got != c.want {
			t.Errorf("sourceKey(%s, local %v, noSys %v) = %q, muốn %q", c.remote, c.local, c.noSys, got, c.want)
		}
	}
}

// Máy không có /proc (không phải Linux): mọi loopback chung khoá "loopback".
func TestSourceKeyKhongProc(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "127.0.0.1:5"
	ctx := context.WithValue(r.Context(), authSysKey{}, system.System(system.NewFake()))
	ctx = context.WithValue(ctx, http.LocalAddrContextKey, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8443})
	if got := sourceKey(r.WithContext(ctx)); got != authLoopback {
		t.Fatalf("không có /proc: %q", got)
	}
}
