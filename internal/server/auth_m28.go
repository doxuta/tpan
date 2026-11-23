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
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
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
	case a.IsLoopback() || selfConn(r, a):
		return loopbackKey(r)
	case a.Is4():
		return a.String()
	default:
		return netip.PrefixFrom(a, 64).Masked().String()
	}
}

// selfConn: địa chỉ nguồn trùng địa chỉ mà panel nhận kết nối, tức là tiến
// trình trên chính máy nối tới IP công khai (hay IP riêng) của máy (D8 của soát
// cuối đợt 2). Tính theo địa chỉ thì PHP của site có thêm một khoá ngoài
// uid:<n>; tra UID như loopback thì không.
//
// ponytail: chỉ bắt ca nguồn bằng đích (kernel chọn nguồn như vậy khi đích là
// địa chỉ của máy). Tiến trình tự bind nguồn là một địa chỉ khác của máy vẫn
// được khoá theo địa chỉ đó; muốn bắt hết thì so với net.InterfaceAddrs().
func selfConn(r *http.Request, a netip.Addr) bool {
	local, _ := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	if local == nil {
		return false
	}
	srv, err := netip.ParseAddrPort(local.String())
	return err == nil && srv.Addr().Unmap().WithZone("") == a
}

// Tra UID qua /proc (D1 của soát cuối đợt 2). Trước đây mỗi request đọc trọn
// hai bảng socket của máy vào bộ nhớ rồi tách từng dòng: bảng to (máy đông
// khách, hay một site cố tình mở vài nghìn kết nối) nhân với vài trăm request
// cùng lúc là hàng GiB, trước cả hàng chờ và semaphore argon2. Giờ:
//
//   - mỗi kết nối tra nhiều nhất một lần (authConn gắn ở http.Server.ConnContext);
//   - cùng lúc chỉ procLookups lần tra, mỗi lần một bộ đệm cố định lấy từ
//     procBufs; hết chỗ thì chờ tối đa procWait, vẫn hết thì không đọc gì và
//     sourceKey trả authLookupBusy: người gọi trả 429, không đếm lần sai nào.
//     KHÔNG rơi về khoá chung "loopback": kẻ làm đầy chỗ tra sẽ đẩy chính lần
//     đoán của nó vào khoá chung, khoá nó lại, rồi admin vào qua `ssh -L` đúng
//     lúc đó (cũng hết chỗ tra) nhận 423 suốt 15 phút. 429 thì thử lại được.
//   - đọc theo dòng, so hai cột đầu với chuỗi hex dựng sẵn của 4-tuple (không
//     tách dòng không khớp), dừng ở dòng khớp, mỗi file đọc tối đa procMaxBytes.
const (
	procLookups = 2
	// procWait: chờ chỗ tra tối đa chừng này. Một lần tra trên bảng vài MiB
	// mất cỡ chục mili giây.
	procWait = 200 * time.Millisecond
	// authLookupBusy là "khoá nguồn" khi hết chỗ tra UID: không phải khoá thật,
	// handleLogin và handlePasswordChange trả 429 auth_busy ngay.
	authLookupBusy = "busy:lookup"
	// procMaxBytes: cỡ 100 000 socket (dòng tcp6 khoảng 150 byte). Socket nằm
	// sau trần thì tra hụt: khoá chung "loopback", an toàn hơn là cho qua.
	procMaxBytes = 16 << 20
	// procLineMax: một dòng /proc/net/tcp6 khoảng 150 byte; dòng dài hơn bộ đệm
	// (không có trên kernel nào) thì bỏ qua.
	procLineMax = 1024
)

// procBufs vừa là semaphore của lần tra vừa giữ bộ đệm: lấy được một bộ đệm
// là được tra. Không cấp phát bộ đệm mới cho mỗi lần tra.
var procBufs = func() chan *bufio.Reader {
	c := make(chan *bufio.Reader, procLookups)
	for range procLookups {
		c <- bufio.NewReaderSize(nil, procLineMax)
	}
	return c
}()

// authConnKey là khoá context giữ *authConn của một kết nối.
type authConnKey struct{}

// authConn nhớ khoá nguồn loopback của một kết nối: UID của đầu bên kia không
// đổi trong đời socket, nên tra một lần là đủ cho mọi request (kể cả các luồng
// HTTP/2) đi trên kết nối đó.
type authConn struct {
	mu  sync.Mutex
	key string
}

// authConnContext là http.Server.ConnContext của Run: gắn chỗ nhớ, chưa tra
// gì (đa số kết nối không bao giờ gọi tới đăng nhập).
func authConnContext(ctx context.Context, _ net.Conn) context.Context {
	return context.WithValue(ctx, authConnKey{}, &authConn{})
}

// loopbackKey trả khoá nguồn của kết nối loopback (hay kết nối từ chính máy):
// "uid:<n>", "loopback" khi không tra được (không phải Linux, không thấy dòng),
// authLookupBusy khi hết chỗ tra. Có chỗ nhớ của kết nối thì
// tra một lần; kết quả "bận" (hết chỗ tra) không nhớ, request sau tra lại.
func loopbackKey(r *http.Request) string {
	m, _ := r.Context().Value(authConnKey{}).(*authConn)
	if m == nil {
		k, _ := lookupLoopback(r)
		return k
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.key != "" {
		return m.key
	}
	k, sure := lookupLoopback(r)
	if sure {
		m.key = k
	}
	return k
}

// lookupLoopback tra UID của đầu bên kia trong /proc/net/tcp và /proc/net/tcp6:
// dòng có local là địa chỉ:cổng nguồn của request, remote là địa chỉ:cổng mà
// panel nhận kết nối. sure=false khi hết chỗ tra (kết quả chỉ tạm thời).
func lookupLoopback(r *http.Request) (key string, sure bool) {
	sys, _ := r.Context().Value(authSysKey{}).(system.System)
	local, _ := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	if sys == nil || local == nil {
		return authLoopback, true
	}
	peer, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return authLoopback, true
	}
	srv, err := netip.ParseAddrPort(local.String())
	if err != nil {
		return authLoopback, true
	}
	var br *bufio.Reader
	select {
	case br = <-procBufs:
	default:
		t := time.NewTimer(procWait)
		defer t.Stop()
		select {
		case br = <-procBufs:
		case <-t.C:
			return authLookupBusy, false
		case <-r.Context().Done():
			return authLookupBusy, false
		}
	}
	defer func() { procBufs <- br }()
	uid, ok := procUID(sys, br, unmapAP(peer), unmapAP(srv))
	if !ok {
		return authLoopback, true
	}
	return "uid:" + uid, true
}

func unmapAP(ap netip.AddrPort) netip.AddrPort {
	return netip.AddrPortFrom(ap.Addr().Unmap().WithZone(""), ap.Port())
}

// procUID tìm cột uid của socket có local = peer, remote = srv, đọc bằng bộ
// đệm br. Bỏ dòng LISTEN (0A) và dòng inode 0: socket TIME_WAIT hay socket mồ
// côi (tiến trình đã đóng) in uid 0, nên kẻ gửi request rồi đóng ngay sẽ mạo
// nhận được UID của root. Dừng ở dòng hợp lệ đầu tiên: kernel không cho hai
// socket đang sống cùng một 4-tuple.
func procUID(sys system.System, br *bufio.Reader, peer, srv netip.AddrPort) (string, bool) {
	files := []struct {
		path string
		pat  []byte
	}{
		{"/proc/net/tcp", procPattern(peer, srv, false)},
		// tcp6 có cả socket IPv4 của listener hai họ, in dạng ::ffff:a.b.c.d.
		{"/proc/net/tcp6", procPattern(peer, srv, true)},
	}
	for _, f := range files {
		if f.pat == nil {
			continue
		}
		rc, _, err := sys.OpenRead(f.path)
		if err != nil {
			continue // không phải Linux, hay máy tắt IPv6
		}
		uid, ok := procScan(br, io.LimitReader(rc, procMaxBytes), f.pat)
		_ = rc.Close()
		if ok {
			return uid, true
		}
	}
	return "", false
}

// procScan đọc từng dòng của một bảng socket, trả uid của dòng hợp lệ đầu tiên
// mà hai cột local, rem khớp pat. Dòng không khớp chỉ được so, không tách.
func procScan(br *bufio.Reader, rd io.Reader, pat []byte) (string, bool) {
	br.Reset(rd)
	for {
		line, err := br.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			// Dòng quá dài: bỏ phần còn lại của nó.
			for errors.Is(err, bufio.ErrBufferFull) {
				_, err = br.ReadSlice('\n')
			}
			if err != nil {
				return "", false
			}
			continue
		}
		// "  sl: local rem st tx:rx tr:when retrnsmt uid timeout inode ..."
		if i := bytes.IndexByte(line, ':'); i >= 0 {
			rest := bytes.TrimLeft(line[i+1:], " ")
			if len(rest) > len(pat) && bytes.EqualFold(rest[:len(pat)], pat) {
				f := strings.Fields(string(rest))
				// f: local rem st tx:rx tr:when retrnsmt uid timeout inode
				if len(f) >= 9 && f[2] != "0A" && f[8] != "0" {
					if _, perr := strconv.ParseUint(f[6], 10, 32); perr == nil {
						return f[6], true
					}
				}
			}
		}
		if err != nil {
			return "", false // hết file, hay chạm trần procMaxBytes
		}
	}
}

// procPattern dựng "LOCAL:PORT REM:PORT " đúng như kernel in một dòng của
// /proc/net/tcp (v6=false, chỉ khi cả hai đầu là IPv4) hay /proc/net/tcp6
// (IPv4 thành ::ffff:a.b.c.d). Kernel in địa chỉ thành từng word 32 bit bằng
// %08X của giá trị đọc theo byte order của máy (little endian trên amd64,
// arm64): 127.0.0.1 là "0100007F". Cổng là %04X thường. nil: không có dòng nào
// trong file này khớp được.
func procPattern(peer, srv netip.AddrPort, v6 bool) []byte {
	if !v6 && (!peer.Addr().Is4() || !srv.Addr().Is4()) {
		return nil
	}
	var out []byte
	for i, ap := range []netip.AddrPort{peer, srv} {
		var raw []byte
		if v6 {
			b := ap.Addr().As16()
			raw = b[:]
		} else {
			b := ap.Addr().As4()
			raw = b[:]
		}
		for j := 0; j < len(raw); j += 4 {
			out = fmt.Appendf(out, "%08X", binary.NativeEndian.Uint32(raw[j:]))
		}
		out = fmt.Appendf(out, ":%04X", ap.Port())
		if i == 0 {
			out = append(out, ' ')
		}
	}
	return append(out, ' ')
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

// ---------------------------------------------------------------- giới hạn tốc độ

// Giới hạn tốc độ của lần so mật khẩu (D6 của soát cuối đợt 2). Lần SAI đã bị
// khoá sau 5 lần; lần ĐÚNG thì trước đây không bị đếm ở đâu, nên một tài khoản
// bất kỳ chạy vòng đăng nhập đúng từ vài chục dải /64 là giữ đầy hai chỗ
// argon2, và mọi lần đăng nhập khác (kể cả admin) chờ 3 giây hay nhận 429.
//
//   - authRateSource: mỗi khoá nguồn tối đa chừng này lần so mật khẩu (đúng hay
//     sai) mỗi phút.
//   - authRateName: mỗi tên tối đa chừng này lần đăng nhập ĐÚNG mỗi phút, xét
//     trước khi so. Chỉ người biết mật khẩu mới làm đầy được phần của một tên,
//     nên người ngoài không dùng nó để chặn admin; và đây không phải khoá tài
//     khoản, chỉ là 429 tới hết phút.
//
// Quá thì 429 auth_busy, không tính là lần sai.
const (
	// 30: đủ cho e2e và người dùng thật đăng nhập lại nhiều lần; vẫn chặn được
	// một khoá nguồn giữ đầy semaphore argon2 (2 chỗ, mỗi lần vài trăm ms).
	authRateSource = 30
	authRateName   = 30
	authRateWindow = time.Minute
	// authRateMax chặn bảng: đầy thì khoá mới không được đếm (cho qua). Lấp đầy
	// bảng cần chừng này khoá nguồn khác nhau trong một phút, mà khi đó mỗi khoá
	// mới vốn đã có phần riêng của nó; hai chỗ argon2 vẫn chặn phần còn lại.
	authRateMax = 100000
)

// authRate đếm theo cửa sổ cố định authRateWindow cho mỗi khoá.
type authRate struct {
	mu    sync.Mutex
	m     map[string]authRateRow
	swept time.Time
}

type authRateRow struct {
	start time.Time
	n     int
}

// get trả số đếm còn trong cửa sổ của key và lúc cửa sổ đó hết. Gọi khi giữ mu.
func (t *authRate) get(key string, now time.Time) (int, time.Time) {
	row, ok := t.m[key]
	if !ok || now.Sub(row.start) >= authRateWindow {
		return 0, now.Add(authRateWindow)
	}
	return row.n, row.start.Add(authRateWindow)
}

// inc cộng một cho key. Gọi khi giữ mu.
func (t *authRate) inc(key string, now time.Time) {
	if t.m == nil {
		t.m = map[string]authRateRow{}
	}
	if now.Sub(t.swept) >= authRateWindow {
		for k, row := range t.m {
			if now.Sub(row.start) >= authRateWindow {
				delete(t.m, k)
			}
		}
		t.swept = now
	}
	row, ok := t.m[key]
	if !ok || now.Sub(row.start) >= authRateWindow {
		if !ok && len(t.m) >= authRateMax {
			return
		}
		row = authRateRow{start: now}
	}
	row.n++
	t.m[key] = row
}

// allow xét trước một lần so mật khẩu của khoá nguồn src cho tên name (rỗng:
// không xét phần theo tên, như đổi mật khẩu). Không được thì trả lúc thử lại
// được. Chỉ xét, không đếm: lần so có chạy thật hay không (argon2 bận) chỉ
// biết sau, và người gọi đang giữ lượt của src nên xét rồi đếm không bị chen.
func (t *authRate) allow(src, name string, now time.Time) (time.Time, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if n, until := t.get("src:"+src, now); n >= authRateSource {
		return until, false
	}
	if name != "" {
		if n, until := t.get("ok:"+name, now); n >= authRateName {
			return until, false
		}
	}
	return time.Time{}, true
}

// compared đếm một lần so mật khẩu đã chạy của khoá nguồn src.
func (t *authRate) compared(src string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.inc("src:"+src, now)
}

// succeeded đếm một lần đăng nhập đúng của name.
func (t *authRate) succeeded(name string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.inc("ok:"+name, now)
}

// authWriteRate: 429 auth_busy khi chạm giới hạn tốc độ. Cùng mã với lúc
// argon2 bận để giao diện xử lý một kiểu; lời và Retry-After nói rõ phải chờ.
func authWriteRate(w http.ResponseWriter, left time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(int(left.Seconds())+1))
	writeJSON(w, http.StatusTooManyRequests, ErrorBody{Error: ErrorDetail{Code: "auth_busy", Message: i18n.T(
		"Quá nhiều lần đăng nhập trong một phút từ nơi gửi này hay cho tài khoản này; thử lại sau "+strconv.Itoa(int(left.Seconds())+1)+" giây.",
		"Too many logins in one minute from this source or for this account; try again in "+strconv.Itoa(int(left.Seconds())+1)+" seconds.")}})
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
