package server

// Khoá đăng nhập đúng nghĩa (M28 đợt 2, P1) và lời 429 khi argon2 bận (P2).
// Thiết kế: agent-dev/M28-dot-2-ban-nhap/M28-dot-2-thiet-ke.md mục 3.4, 3.5,
// cùng các chỗ đã sửa theo S1, S9, S10, S11 (tra-loi-soat.md).
//
// Ba thứ trong bộ nhớ, cùng dùng một "khoá nguồn" (sourceKey) với dòng
// login_failures trong DB:
//
//   - authGates: các lần thử của cùng một khoá nguồn chạy lần lượt, nên lần
//     sau luôn thấy kết quả của lần trước (hết lỗi "bắn song song lọt qua
//     trước khi kịp khoá"); quá authMaxQueue lượt thì 429 ngay.
//   - authNames: phần lần sai của từng tên trong khoá nguồn, để đăng nhập đúng
//     của tên N chỉ trừ phần của N (hết lỗi "khách xen một lần đăng nhập đúng
//     của mình là xoá đếm đoán mật khẩu admin").
//   - authSysKey: System của server gắn vào context (secure, auth.go) để
//     sourceKey tra được UID của kết nối loopback.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

const (
	// authMaxQueue: số lần thử của một khoá nguồn được có mặt cùng lúc, tính cả
	// lượt đang chạy. Lượt thứ authMaxQueue+1 nhận 429 ngay, không xếp hàng vô hạn.
	authMaxQueue = 4
	// authLoopback là khoá chung cho kết nối loopback không tra được UID: gộp
	// làm một an toàn hơn là cho mỗi địa chỉ 127.x một khoá riêng (S1).
	authLoopback = "loopback"
	// authBusyAfter là số giây trong Retry-After của 429 auth_busy.
	authBusyAfter = "3"
)

// authNamesMax chặn bảng theo tên: đầy thì bỏ dòng ghi lâu nhất. Mỗi lần ghi
// duyệt cả bảng (dọn dòng cũ, tìm dòng lâu nhất): tối đa 10 000 dòng, mà lần
// ghi bị argon2 giới hạn cỡ chục lần mỗi giây, nên rẻ. Biến để test hạ xuống.
var authNamesMax = 10000

// authSysKey là khoá context giữ system.System của server (gắn ở secure).
type authSysKey struct{}

// sourceKey là khoá nguồn đã chuẩn hoá của request: thứ mà bộ đếm lần sai (cột
// ip của login_failures), mutex, hàng chờ và bảng theo tên cùng dùng.
//
//   - IPv4 (kể cả IPv4 ánh xạ ::ffff:a.b.c.d): chính địa chỉ đó.
//   - IPv6: dải /64 ("2001:db8:1:2::/64"), vì một máy thường có cả dải.
//   - Loopback (127.0.0.0/8, ::1): "uid:<n>" của tiến trình đầu bên kia. Trên
//     Linux mọi địa chỉ 127.x đều là của máy, PHP của một site chọn được địa
//     chỉ nguồn tuỳ ý trong cả dải; tính theo địa chỉ thì mỗi địa chỉ có 5 lần
//     đoán riêng (S1). Tính theo UID thì mỗi user Linux của site chỉ có một
//     khoá dù đổi địa chỉ nguồn thế nào, và admin vào qua `ssh -L` mang UID
//     của phiên SSH, khác UID của site. Không tra được thì "loopback" (một
//     khoá chung).
func sourceKey(r *http.Request) string {
	host := clientIP(r)
	a, err := netip.ParseAddr(host)
	if err != nil {
		// Không phải địa chỉ IP (socket unix, test): dùng nguyên chuỗi.
		return host
	}
	a = a.Unmap().WithZone("")
	switch {
	case a.IsLoopback():
		return loopbackKey(r)
	case a.Is4():
		return a.String()
	default:
		return netip.PrefixFrom(a, 64).Masked().String()
	}
}

// loopbackKey tra UID của đầu bên kia của kết nối loopback trong
// /proc/net/tcp và /proc/net/tcp6: dòng có local là địa chỉ:cổng nguồn của
// request và remote là địa chỉ:cổng mà panel nhận kết nối.
func loopbackKey(r *http.Request) string {
	sys, _ := r.Context().Value(authSysKey{}).(system.System)
	local, _ := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	if sys == nil || local == nil {
		return authLoopback
	}
	peer, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return authLoopback
	}
	srv, err := netip.ParseAddrPort(local.String())
	if err != nil {
		return authLoopback
	}
	uid, ok := procUID(sys, unmapAP(peer), unmapAP(srv))
	if !ok {
		return authLoopback
	}
	return "uid:" + uid
}

func unmapAP(ap netip.AddrPort) netip.AddrPort {
	return netip.AddrPortFrom(ap.Addr().Unmap().WithZone(""), ap.Port())
}

// procUID tìm cột uid của socket có local = peer, remote = srv. Bỏ dòng LISTEN
// (0A) và dòng inode 0: socket TIME_WAIT hay socket mồ côi (tiến trình đã đóng)
// in uid 0, nên kẻ gửi request rồi đóng ngay sẽ mạo nhận được UID của root.
// Thấy hai dòng khớp mà khác UID (không nên xảy ra) thì coi như không tra được.
func procUID(sys system.System, peer, srv netip.AddrPort) (string, bool) {
	uid := ""
	for _, p := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		b, err := sys.ReadFile(p)
		if err != nil {
			continue // không phải Linux, hay máy tắt IPv6
		}
		for line := range strings.SplitSeq(string(b), "\n") {
			// sl local rem st tx:rx tr:when retrnsmt uid timeout inode ...
			f := strings.Fields(line)
			if len(f) < 10 || f[3] == "0A" || f[9] == "0" {
				continue
			}
			l, ok1 := procAddr(f[1])
			rem, ok2 := procAddr(f[2])
			if !ok1 || !ok2 || l != peer || rem != srv {
				continue
			}
			if _, err := strconv.ParseUint(f[7], 10, 32); err != nil {
				return "", false
			}
			if uid != "" && uid != f[7] {
				return "", false
			}
			uid = f[7]
		}
	}
	return uid, uid != ""
}

// procAddr đọc một ô "ADDR:PORT" của /proc/net/tcp{,6}. Kernel in địa chỉ thành
// từng word 32 bit bằng %08X của giá trị đọc theo byte order của máy (little
// endian trên amd64, arm64): 127.0.0.1 là "0100007F". Cổng là %04X thường.
func procAddr(s string) (netip.AddrPort, bool) {
	h, p, ok := strings.Cut(s, ":")
	port, err := strconv.ParseUint(p, 16, 16)
	if !ok || err != nil {
		return netip.AddrPort{}, false
	}
	raw, err := hex.DecodeString(h)
	if err != nil || (len(raw) != 4 && len(raw) != 16) {
		return netip.AddrPort{}, false
	}
	var b [16]byte
	for i := 0; i < len(raw); i += 4 {
		binary.NativeEndian.PutUint32(b[i:], binary.BigEndian.Uint32(raw[i:]))
	}
	var a netip.Addr
	if len(raw) == 4 {
		a = netip.AddrFrom4([4]byte(b[:4]))
	} else {
		a = netip.AddrFrom16(b).Unmap()
	}
	return netip.AddrPortFrom(a, uint16(port)), true
}

// ---------------------------------------------------------------- hàng chờ

// authGates là một mutex cho mỗi khoá nguồn, kèm số lượt đang có mặt.
type authGates struct {
	mu sync.Mutex
	m  map[string]*authGate
}

type authGate struct {
	turn chan struct{} // một chỗ: ai gửi được vào là tới lượt
	n    int           // lượt đang chạy cộng lượt đang chờ
}

// enter chờ tới lượt của khoá nguồn key. ok=false khi key đã có authMaxQueue
// lượt, hay ctx bị huỷ lúc chờ (người gửi đã bỏ đi). ok=true thì người gọi
// phải gọi leave đúng một lần.
func (g *authGates) enter(ctx context.Context, key string) (leave func(), ok bool) {
	g.mu.Lock()
	if g.m == nil {
		g.m = map[string]*authGate{}
	}
	gt := g.m[key]
	if gt == nil {
		gt = &authGate{turn: make(chan struct{}, 1)}
		g.m[key] = gt
	}
	if gt.n >= authMaxQueue {
		g.mu.Unlock()
		return nil, false
	}
	gt.n++
	g.mu.Unlock()
	out := func() {
		g.mu.Lock()
		if gt.n--; gt.n == 0 {
			delete(g.m, key) // không ai dùng: bảng không phình theo số nguồn
		}
		g.mu.Unlock()
	}
	select {
	case gt.turn <- struct{}{}:
		return func() { <-gt.turn; out() }, true
	case <-ctx.Done():
		out()
		return nil, false
	}
}

// ---------------------------------------------------------------- bảng theo tên

// authNames nhớ, cho mỗi cặp (khoá nguồn, tên), số lần sai đã cộng vào dòng
// DB của khoá nguồn và first_at của dòng DB lúc cộng. Tên không bao giờ được
// lưu: chỉ HMAC-SHA256 của nó với một khoá ngẫu nhiên sinh lúc tiến trình khởi
// động (người ta hay gõ nhầm mật khẩu vào ô tên). Panel khởi động lại thì bảng
// mất, tổng trong DB vẫn còn: chỉ có thể khoá sớm hơn, không bao giờ muộn hơn.
type authNames struct {
	mu  sync.Mutex
	key []byte
	m   map[authNameKey]authNameRow
}

type authNameKey struct {
	src  string
	name [sha256.Size]byte
}

type authNameRow struct {
	firstAt time.Time // first_at của dòng DB khi cộng
	at      time.Time // lần ghi gần nhất, để bỏ dòng cũ nhất khi đầy
	n       int
}

func newAuthNames() *authNames {
	return &authNames{key: authRandom(32), m: map[authNameKey]authNameRow{}}
}

// id băm tên đã chuẩn hoá (rỗng là admin, quá dài là "\x00": handleLogin làm
// trước khi gọi).
func (t *authNames) id(src, name string) authNameKey {
	h := hmac.New(sha256.New, t.key)
	h.Write([]byte(name))
	k := authNameKey{src: src}
	h.Sum(k.name[:0])
	return k
}

// failed ghi một lần sai của (src, name) sau khi DB đã ghi thành f. Cùng lúc
// dọn: dòng của src có first_at khác f.FirstAt (DB đã đếm lại từ 1, kể cả vì
// khoá cũ hết hạn) và dòng đã quá cửa sổ. Đầy thì bỏ dòng ghi lâu nhất.
func (t *authNames) failed(src, name string, f store.LoginFailure, now time.Time) {
	k := t.id(src, name)
	t.mu.Lock()
	defer t.mu.Unlock()
	var oldest authNameKey
	var oldestAt time.Time
	for kk, row := range t.m {
		if (kk.src == src && !row.firstAt.Equal(f.FirstAt)) || now.Sub(row.firstAt) > authFailWindow {
			delete(t.m, kk)
			continue
		}
		if kk != k && (oldestAt.IsZero() || row.at.Before(oldestAt)) {
			oldest, oldestAt = kk, row.at
		}
	}
	row := t.m[k]
	row.firstAt, row.at = f.FirstAt, now
	row.n++
	t.m[k] = row
	if len(t.m) > authNamesMax {
		delete(t.m, oldest)
	}
}

// take lấy ra (và xoá) phần của (src, name); ok=false khi không có hay đã quá
// cửa sổ.
func (t *authNames) take(src, name string, now time.Time) (n int, firstAt time.Time, ok bool) {
	k := t.id(src, name)
	t.mu.Lock()
	defer t.mu.Unlock()
	row, found := t.m[k]
	delete(t.m, k)
	if !found || now.Sub(row.firstAt) > authFailWindow {
		return 0, time.Time{}, false
	}
	return row.n, row.firstAt, true
}

// dropSource xoá mọi dòng của src (khoá của src đã hết hạn).
func (t *authNames) dropSource(src string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for kk := range t.m {
		if kk.src == src {
			delete(t.m, kk)
		}
	}
}

// authWriteBusy: 429 auth_busy (P2 và hàng chờ của một khoá nguồn đầy).
func authWriteBusy(w http.ResponseWriter) {
	w.Header().Set("Retry-After", authBusyAfter)
	writeJSON(w, http.StatusTooManyRequests, authBusyBody())
}

func authBusyBody() ErrorBody {
	return ErrorBody{Error: ErrorDetail{Code: "auth_busy", Message: i18n.T(
		"Đang có quá nhiều lần kiểm mật khẩu cùng lúc; thử lại sau vài giây.",
		"Too many password checks are running at once; try again in a few seconds.")}}
}
