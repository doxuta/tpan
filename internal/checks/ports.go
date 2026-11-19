package checks

import (
	"context"
	"net/netip"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

// listener là một dòng của `ss -tln`: một địa chỉ đang nghe.
type listener struct {
	addr netip.Addr // không hợp lệ (IsValid false) khi là "*" hoặc không đọc được
	port int
	proc string // tên tiến trình, chỉ có khi chạy `ss` với -p và đủ quyền
}

// loopback cho biết địa chỉ chỉ nhận kết nối từ chính máy này. Địa chỉ không
// đọc được (ví dụ "*") coi là KHÔNG loopback: phép kiểm bảo mật phải lỗi về phía an toàn.
func (l listener) loopback() bool { return l.addr.IsValid() && l.addr.IsLoopback() }

var procRE = regexp.MustCompile(`\(\("([^"]+)"`)

// parseSS đọc output của `ss -tlnH` (có hoặc không có -p). Mỗi dòng dạng:
//
//	LISTEN 0 80  127.0.0.1:3306  0.0.0.0:*
//	LISTEN 0 511 0.0.0.0:80      0.0.0.0:*   users:(("nginx",pid=812,fd=6))
//	LISTEN 0 128 [::]:22         [::]:*
//	LISTEN 0 4096 127.0.0.53%lo:53 0.0.0.0:*
//	LISTEN 0 511 *:80            *:*
//
// Cột 4 là địa chỉ:cổng. Tách ở dấu ":" CUỐI CÙNG (IPv6 có nhiều dấu ":"), bỏ
// ngoặc vuông và đuôi "%giao-diện", rồi để net/netip đọc.
func parseSS(out string) []listener {
	var ls []listener
	for _, ln := range strings.Split(out, "\n") {
		f := strings.Fields(ln)
		if len(f) < 4 || f[0] != "LISTEN" {
			continue
		}
		i := strings.LastIndex(f[3], ":")
		if i < 0 {
			continue
		}
		port, err := strconv.Atoi(f[3][i+1:])
		if err != nil {
			continue
		}
		host := strings.Trim(f[3][:i], "[]")
		host, _, _ = strings.Cut(host, "%")
		l := listener{port: port}
		if a, err := netip.ParseAddr(host); err == nil {
			l.addr = a.Unmap() // ::ffff:127.0.0.1 thành 127.0.0.1
		}
		if m := procRE.FindStringSubmatch(ln); m != nil {
			l.proc = m[1]
		}
		ls = append(ls, l)
	}
	return ls
}

// PortLocalOnly kiểm cổng TCP chỉ nghe ở địa chỉ loopback (127.0.0.0/8 hoặc ::1).
// Dùng cho 3306 của MariaDB: mở ra ngoài là ai trên mạng cũng thử mật khẩu được.
//
//   - Chỉ nghe loopback: Pass.
//   - Có nghe ở địa chỉ khác (kể cả 0.0.0.0, [::], *): Fail.
//   - Không ai nghe: Fail (dịch vụ đang chết, mà "không mở ra ngoài" thì đúng
//     chỉ vì nó không chạy; báo xanh ở đây là che lỗi).
//
// Số cổng được so như số nguyên, không so chuỗi con: 33060 không phải 3306.
func PortLocalOnly(port int, stage Stage) Check {
	p := strconv.Itoa(port)
	b := base{id: "port.localonly:" + p, stage: stage,
		title: i18n.T("Cổng "+p+" chỉ nghe ở 127.0.0.1", "Port "+p+" listens on 127.0.0.1 only")}
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		res, ev, err := execCmd(ctx, sys, system.Cmd{Name: "ss", Args: []string{"-tlnH"}})
		if err != nil {
			return b.cantRun(err, ev)
		}
		if res.ExitCode != 0 {
			return b.unknown(i18n.T("ss báo lỗi (mã "+strconv.Itoa(res.ExitCode)+")", "ss failed (exit "+strconv.Itoa(res.ExitCode)+")"), ev)
		}
		var open, local []string
		for _, l := range parseSS(res.Stdout) {
			if l.port != port {
				continue
			}
			if l.loopback() {
				local = append(local, l.addr.String())
			} else {
				open = append(open, addrText(l))
			}
		}
		recheck := line("ss", "-tln")
		switch {
		case len(open) > 0:
			fix := []string{line("ss", "-tlnp")}
			if port == 3306 {
				fix = append([]string{line("grep", "-rn", "bind-address", "/etc/mysql/")}, fix...)
			}
			return b.bad(Fail,
				i18n.T("Cổng "+p+" đang mở ra ngoài (nghe ở "+strings.Join(open, ", ")+")", "Port "+p+" is open to the outside (listening on "+strings.Join(open, ", ")+")"),
				i18n.T("Dịch vụ nghe ở mọi địa chỉ nên ai trên mạng cũng nối vào được. Cần đặt nó chỉ nghe ở 127.0.0.1 (với MariaDB: dòng bind-address = 127.0.0.1 trong /etc/mysql), rồi khởi động lại dịch vụ.",
					"The service listens on every address, so anyone on the network can connect. It must listen on 127.0.0.1 only (for MariaDB: bind-address = 127.0.0.1 under /etc/mysql), then restart the service."),
				append(fix, recheck), ev)
		case len(local) == 0:
			return b.bad(Fail,
				i18n.T("Không có gì đang nghe ở cổng "+p, "Nothing is listening on port "+p),
				i18n.T("Dịch vụ dùng cổng này chưa chạy hoặc đang nghe ở cổng khác.", "The service that uses this port is not running, or listens on another port."),
				[]string{line("ss", "-tlnp"), recheck}, ev)
		}
		return b.pass(i18n.T("Cổng "+p+" chỉ nghe ở "+strings.Join(local, ", "), "Port "+p+" listens only on "+strings.Join(local, ", ")), ev)
	})
}

// addrText viết địa chỉ nghe cho người đọc; "*" khi không đọc được địa chỉ.
func addrText(l listener) string {
	if l.addr.IsValid() {
		return l.addr.String()
	}
	return "*"
}

// PortsFree kiểm các cổng TCP đang trống, dùng TRƯỚC khi cài stack (mục 6.1:
// cổng 80 và 443 phải trống). Có ai nghe thì Fail và nêu tên tiến trình nếu ss
// cho biết.
//
// Dùng `ss -tlnpH` thay vì `ss -tlnH`: chỉ có -p mới in cột tên tiến trình, mà
// thông báo "cổng 80 đang bị nginx giữ" hữu ích hơn nhiều so với "cổng 80 bận".
// tpan chạy bằng root nên -p thấy được mọi tiến trình.
func PortsFree(ports ...int) Check {
	names := make([]string, len(ports))
	for i, p := range ports {
		names[i] = strconv.Itoa(p)
	}
	list := strings.Join(names, ",")
	b := base{id: "ports.free:" + list, stage: StageMachine,
		title: i18n.T("Cổng "+list+" đang trống", "Port "+list+" is free")}
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		res, ev, err := execCmd(ctx, sys, system.Cmd{Name: "ss", Args: []string{"-tlnpH"}})
		if err != nil {
			return b.cantRun(err, ev)
		}
		if res.ExitCode != 0 {
			return b.unknown(i18n.T("ss báo lỗi (mã "+strconv.Itoa(res.ExitCode)+")", "ss failed (exit "+strconv.Itoa(res.ExitCode)+")"), ev)
		}
		busy := map[int]string{} // cổng -> tên tiến trình ("" nếu không biết)
		for _, l := range parseSS(res.Stdout) {
			if !containsInt(ports, l.port) {
				continue
			}
			if busy[l.port] == "" {
				busy[l.port] = l.proc
			}
		}
		if len(busy) == 0 {
			return b.pass(i18n.T("Cổng "+list+" đang trống", "Port "+list+" is free"), ev)
		}
		used := make([]int, 0, len(busy))
		for p := range busy {
			used = append(used, p)
		}
		sort.Ints(used)
		var vi, en []string
		for _, p := range used {
			if busy[p] != "" {
				vi = append(vi, strconv.Itoa(p)+" (đang bị "+busy[p]+" dùng)")
				en = append(en, strconv.Itoa(p)+" (used by "+busy[p]+")")
			} else {
				vi = append(vi, strconv.Itoa(p)+" (đang có tiến trình dùng)")
				en = append(en, strconv.Itoa(p)+" (in use)")
			}
		}
		return b.bad(Fail,
			i18n.T("Cổng đã bị chiếm: "+strings.Join(vi, ", "), "Ports already in use: "+strings.Join(en, ", ")),
			i18n.T("Máy đã có web server hoặc dịch vụ khác giữ cổng. TPAN chỉ cài trên máy sạch để không đè lên thứ đang chạy: dừng dịch vụ đó hoặc dùng máy khác.",
				"Another web server or service already holds the port. TPAN only installs on a clean machine so it never overwrites something running: stop that service or use another machine."),
			[]string{line("ss", "-tlnp")}, ev)
	})
}

func containsInt(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
