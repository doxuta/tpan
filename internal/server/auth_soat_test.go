package server

// Test của các chỗ sửa theo soát cuối M28 đợt 2: tra UID loopback có trần và
// nhớ theo kết nối (D1), địa chỉ của chính máy (D8), giới hạn tốc độ của lần
// so mật khẩu (D6/D7), nhật ký ghi khoá nguồn (D12).

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/doxuta/tpan/internal/system"
)

// bigProcTable dựng /proc/net/tcp có n dòng socket không liên quan, rồi dòng
// của client 127.0.1.9:50999 -> 127.0.0.1:8443 (uid 1009) ở CUỐI bảng.
func bigProcTable(n int) []byte {
	var b strings.Builder
	b.WriteString(procHeader + "\n")
	for i := range n {
		// 10.0.x.y:port -> 10.1.0.1:443, uid 33
		fmt.Fprintf(&b, "%6d: %02X%02X000A:%04X 0100010A:01BB 01 00000000:00000000 00:00000000 00000000    33        0 %d 1 0000000000000000 20 4 30 10 -1\n",
			i, i%250, (i/250)%250, 1024+i%60000, 100000+i)
	}
	b.WriteString(procLine(n, "127.0.1.9:50999", "127.0.0.1:8443", 1009, 777, "01") + "\n")
	return []byte(b.String())
}

func loopReq(sys system.System, remote string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/login", nil)
	r.RemoteAddr = remote
	ctx := context.WithValue(r.Context(), authSysKey{}, sys)
	ctx = context.WithValue(ctx, http.LocalAddrContextKey, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8443})
	return r.WithContext(ctx)
}

// D1: bảng 40 000 dòng (khoảng 5 MiB), 600 request song song. Trước đây mỗi
// request đọc và tách cả bảng (đo được 22 MiB mỗi request, heap đỉnh 3,6
// GiB). Giờ cấp phát mỗi request phải nhỏ, không phụ thuộc cỡ bảng.
func TestD1BangLonCapPhatNho(t *testing.T) {
	sys := system.NewFake()
	sys.SetFile("/proc/net/tcp", bigProcTable(40000), 0o444, system.Owner{})
	const want = "uid:1009"

	// Một request: tìm đúng dòng ở cuối bảng, cấp phát vài chục lần.
	if got := sourceKey(loopReq(sys, "127.0.1.9:50999")); got != want {
		t.Fatalf("sourceKey = %q, muốn %q", got, want)
	}
	r := loopReq(sys, "127.0.1.9:50999")
	if n := testing.AllocsPerRun(5, func() { _ = sourceKey(r) }); n > 60 {
		t.Errorf("một lần tra cấp phát %.0f lần, muốn không quá 60", n)
	}

	// 600 request song song: lần tra nào chờ quá procWait thì ra authLookupBusy
	// (429, không đọc); tổng byte cấp phát chia cho số request phải nhỏ.
	const reqs = 600
	rs := make([]*http.Request, reqs)
	for i := range rs {
		rs[i] = loopReq(sys, "127.0.1.9:50999")
	}
	var found, busy atomic.Int32
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, r := range rs {
		wg.Go(func() {
			<-start
			switch sourceKey(r) {
			case want:
				found.Add(1)
			case authLookupBusy:
				busy.Add(1)
			}
		})
	}
	close(start)
	wg.Wait()
	runtime.ReadMemStats(&after)
	per := (after.TotalAlloc - before.TotalAlloc) / reqs
	t.Logf("600 request: %d tra được, %d hết chỗ tra; cấp phát %d byte mỗi request", found.Load(), busy.Load(), per)
	if found.Load()+busy.Load() != reqs || found.Load() == 0 {
		t.Errorf("kết quả lạ: %d tra được, %d hết chỗ tra", found.Load(), busy.Load())
	}
	// Mỗi goroutine tự nó đã tốn vài KiB ngăn xếp (không tính ở đây) và
	// httptest không có trong vòng đo; 64 KiB là trần rộng, bản cũ là 22 MiB.
	if per > 64<<10 {
		t.Errorf("cấp phát %d byte mỗi request, muốn dưới 64 KiB", per)
	}
}

// Trần số byte đọc: socket nằm sau procMaxBytes thì tra hụt (khoá chung).
func TestD1TranSoByte(t *testing.T) {
	br := bufio.NewReaderSize(nil, procLineMax)
	tbl := bigProcTable(20)
	pat := procPattern(netip4("127.0.1.9", 50999), netip4("127.0.0.1", 8443), false)
	if _, ok := procScan(br, strings.NewReader(string(tbl)), pat); !ok {
		t.Fatal("không thấy dòng khi chưa chạm trần")
	}
	if _, ok := procScan(br, io.LimitReader(strings.NewReader(string(tbl)), int64(len(tbl)-200)), pat); ok {
		t.Fatal("dòng nằm sau trần mà vẫn thấy")
	}
	// Dòng dài hơn bộ đệm bị bỏ, dòng sau vẫn đọc được.
	long := strings.Repeat("x", procLineMax*3) + "\n" + string(tbl)
	if uid, ok := procScan(br, strings.NewReader(long), pat); !ok || uid != "1009" {
		t.Fatalf("sau dòng quá dài: %q %v", uid, ok)
	}
}

// Hết chỗ tra (sau procWait): trả authLookupBusy mà không đọc, không nhớ kết
// quả tạm đó cho kết nối; lần sau có chỗ thì tra thật. Đăng nhập lúc đó nhận
// 429 auth_busy, không đếm lần sai vào khoá chung "loopback".
func TestD1HetChoKhongNho(t *testing.T) {
	sys := system.NewFake()
	sys.SetFile("/proc/net/tcp", bigProcTable(3), 0o444, system.Owner{})
	m := &authConn{}
	r := loopReq(sys, "127.0.1.9:50999")
	r = r.WithContext(context.WithValue(r.Context(), authConnKey{}, m))
	var held []*bufio.Reader
	for range procLookups {
		held = append(held, <-procBufs)
	}
	got := sourceKey(r)
	he := newHookEnv(t)
	b, _ := json.Marshal(LoginRequest{Name: "admin", Password: "doan"})
	lr := loopReq(he.sys, "127.0.1.9:50999")
	lr.Body = io.NopCloser(strings.NewReader(string(b)))
	rec := httptest.NewRecorder()
	he.h.ServeHTTP(rec, lr)
	for _, b := range held {
		procBufs <- b
	}
	if got != authLookupBusy || m.key != "" {
		t.Fatalf("hết chỗ tra: %q, nhớ %q; muốn %q và không nhớ", got, m.key, authLookupBusy)
	}
	if rec.Code != http.StatusTooManyRequests || errCode(t, rec) != "auth_busy" || he.calls.Load() != 0 {
		t.Fatalf("đăng nhập lúc hết chỗ tra: %d %s, so %d lần", rec.Code, rec.Body, he.calls.Load())
	}
	for _, k := range []string{authLoopback, authLookupBusy} {
		if _, l, _ := he.st.LoginLockedUntil(context.Background(), k, he.now); l {
			t.Fatalf("khoá %s bị đếm", k)
		}
	}
	if got := sourceKey(r); got != "uid:1009" || m.key != "uid:1009" {
		t.Fatalf("có chỗ tra: %q, nhớ %q", got, m.key)
	}
}

// countSys đếm số lần mở file qua OpenRead.
type countSys struct {
	system.System
	opens atomic.Int32
}

func (c *countSys) OpenRead(p string) (io.ReadCloser, int64, error) {
	c.opens.Add(1)
	return c.System.OpenRead(p)
}

// D1: http.Server có ConnContext của Run thì nhiều request trên một kết nối
// keep-alive chỉ tra /proc một lần (hai file).
func TestD1TraMotLanMoiKetNoi(t *testing.T) {
	sys := &countSys{System: system.NewFake()}
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(context.WithValue(r.Context(), authSysKey{}, system.System(sys)))
		_, _ = io.WriteString(w, sourceKey(r))
	}))
	ts.Config.ConnContext = authConnContext
	ts.Start()
	defer ts.Close()
	c := ts.Client()
	for i := range 5 {
		res, err := c.Get(ts.URL)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if string(b) != authLoopback { // FakeSystem không có /proc
			t.Fatalf("request %d: %q", i, b)
		}
	}
	if n := sys.opens.Load(); n != 2 {
		t.Fatalf("5 request trên một kết nối mở /proc %d lần, muốn 2 (tcp, tcp6)", n)
	}
}

func netip4(ip string, port int) netip.AddrPort {
	return netip.MustParseAddrPort(net.JoinHostPort(ip, fmt.Sprint(port)))
}

// D6 (D7 ở bản cuối của file soát): một tài khoản đăng nhập ĐÚNG liên tục từ
// 40 dải /64 khác nhau. Sau authRateName lần trong một phút thì 429, không
// so mật khẩu nữa; admin từ nơi khác vẫn vào. Hết phút thì được lại.
func TestD6DangNhapDungCoGioiHan(t *testing.T) {
	he := newHookEnv(t)
	codes := map[int]int{}
	for i := range 40 {
		codes[he.loginAs("khach1", khach1Password, fmt.Sprintf("[2001:db8:%x::1]", i)).Code]++
	}
	if codes[http.StatusNoContent] != authRateName || codes[http.StatusTooManyRequests] != 40-authRateName {
		t.Fatalf("mã: %v; muốn %d lần 204 rồi 429", codes, authRateName)
	}
	if n := he.calls.Load(); n != authRateName {
		t.Fatalf("đã so mật khẩu %d lần, muốn %d", n, authRateName)
	}
	rec := he.loginAs("khach1", khach1Password, "[2001:db8:ff::1]")
	if rec.Code != http.StatusTooManyRequests || errCode(t, rec) != "auth_busy" || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("lần quá giới hạn: %d %s", rec.Code, rec.Body)
	}
	he.expect("admin", authTestPassword, "198.51.100.8", http.StatusNoContent)
	he.now = he.now.Add(authRateWindow)
	he.expect("khach1", khach1Password, "[2001:db8:ff::1]", http.StatusNoContent)
}

// D6: mỗi khoá nguồn tối đa authRateSource lần so mật khẩu mỗi phút, đúng hay
// sai; quá thì 429 và không tính là lần sai.
func TestD6GioiHanTheoNguon(t *testing.T) {
	he := newHookEnv(t)
	const ip = "198.51.100.7"
	for range authRateSource {
		he.expect("khach1", khach1Password, ip, http.StatusNoContent)
	}
	before := he.calls.Load()
	he.expect("khach1", "sai", ip, http.StatusTooManyRequests)
	if he.calls.Load() != before {
		t.Fatal("quá giới hạn mà vẫn so mật khẩu")
	}
	if _, l, _ := he.st.LoginLockedUntil(context.Background(), ip, he.now); l {
		t.Fatal("429 không được tính là lần sai")
	}
	he.expect("admin", authTestPassword, "198.51.100.8", http.StatusNoContent)
}

// Bảng giới hạn tốc độ không giữ dòng quá cửa sổ và không vượt authRateMax.
func TestAuthRateDon(t *testing.T) {
	var tb authRate
	tb.compared("a", authT0)
	tb.compared("b", authT0)
	tb.compared("c", authT0.Add(authRateWindow))
	if len(tb.m) != 1 {
		t.Fatalf("bảng còn %d dòng, muốn 1", len(tb.m))
	}
	if _, ok := tb.allow("a", "", authT0.Add(authRateWindow)); !ok {
		t.Fatal("hết cửa sổ mà vẫn chặn")
	}
}

// D12: dòng login.fail của kết nối loopback ghi khoá nguồn uid:<n> ở target.
func TestD12NhatKyGhiKhoaNguon(t *testing.T) {
	he := newHookEnv(t)
	he.sys.SetFile("/proc/net/tcp", bigProcTable(3), 0o444, system.Owner{})
	b, _ := json.Marshal(LoginRequest{Name: "admin", Password: "doan"})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/login", strings.NewReader(string(b)))
	r.RemoteAddr = "127.0.1.9:50999"
	r = r.WithContext(context.WithValue(r.Context(), http.LocalAddrContextKey, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8443}))
	rec := httptest.NewRecorder()
	he.h.ServeHTTP(rec, r)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("mã %d %s", rec.Code, rec.Body)
	}
	db, err := sql.Open("sqlite", he.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var ip, target, result string
	if err := db.QueryRow("SELECT ip, target, result FROM audit WHERE action = 'login.fail'").Scan(&ip, &target, &result); err != nil {
		t.Fatal(err)
	}
	if ip != "127.0.1.9" || target != "uid:1009" || result != "wrong_password" {
		t.Fatalf("audit: ip %q target %q result %q", ip, target, result)
	}
}
