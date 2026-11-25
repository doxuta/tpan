package core

// Mục "Địa chỉ IP" ở Cài đặt (admin): máy có những IP nào, panel nghe ở đâu,
// bản ghi DNS của từng site đang trỏ về IP nào. CHỈ ĐỌC: không đổi cấu hình
// mạng (netplan), không đổi DNS (bản ghi nằm ở nơi quản lý tên miền).
//
// Nguồn: `ip -o addr show scope global` (như phép kiểm dns.a) và
// `getent ahostsv4|ahostsv6 <domain>` cho từng site. Mỗi lần tra DNS có hạn
// giờ riêng để một tên miền chậm không kéo cả trang.

import (
	"context"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/system"
)

// IPAddr là một địa chỉ trên card mạng của máy.
type IPAddr struct {
	Iface  string `json:"iface"`
	IP     string `json:"ip"`
	Prefix int    `json:"prefix"`
	// Family: "ipv4" hoặc "ipv6".
	Family string `json:"family"`
	// Public: không phải dải nội bộ (10/8, 172.16/12, 192.168/16, 100.64/10,
	// fc00::/7). VPS sau NAT thường chỉ có IP nội bộ trên card mạng.
	Public bool `json:"public"`
}

// SiteDNS là bản ghi DNS của một site nhìn từ máy này.
type SiteDNS struct {
	Domain string   `json:"domain"`
	A      []string `json:"a"`
	AAAA   []string `json:"aaaa"`
	// Here: có ít nhất một bản ghi A nằm trên card mạng của máy. false mà A
	// không rỗng: trỏ máy khác, hoặc máy đứng sau NAT.
	Here bool `json:"here"`
	// Error: tra DNS không được (hết giờ, getent lỗi lạ). Rỗng là tra được.
	Error string `json:"error,omitempty"`
}

// MachineIPs là thân của GET /api/v1/machine/ips.
type MachineIPs struct {
	// Hostname là tên máy (hostname); OS là PRETTY_NAME của /etc/os-release
	// (rỗng nếu không đọc được).
	Hostname string   `json:"hostname"`
	OS       string   `json:"os,omitempty"`
	Addrs    []IPAddr `json:"addrs"`
	// PanelPort, PanelPrivate đọc từ file dịch vụ của panel: private là chỉ
	// nghe 127.0.0.1 (vào bằng ssh -L), không mở được bằng IP từ ngoài.
	PanelPort    string    `json:"panel_port"`
	PanelPrivate bool      `json:"panel_private"`
	Sites        []SiteDNS `json:"sites"`
}

// ipLookupTimeout là hạn giờ của một lần getent.
const ipLookupTimeout = 4 * time.Second

var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// ReadMachineIPs gom IP của máy, chế độ nghe của panel và DNS của domains.
func ReadMachineIPs(ctx context.Context, sys system.System, hostname string, domains []string) (MachineIPs, error) {
	out := MachineIPs{Hostname: hostname, Addrs: []IPAddr{}, Sites: []SiteDNS{}}
	if b, err := sys.ReadFile("/etc/os-release"); err == nil {
		out.OS = osPrettyName(string(b))
	}
	res, err := sys.Run(ctx, system.Cmd{Name: "ip", Args: []string{"-o", "addr", "show", "scope", "global"}})
	if err != nil {
		return out, err
	}
	out.Addrs = parseIPAddrs(res.Stdout)
	port, private, err := panelListen(sys)
	if err != nil {
		return out, err
	}
	out.PanelPort, out.PanelPrivate = port, private

	mine := map[string]bool{}
	for _, a := range out.Addrs {
		mine[a.IP] = true
	}
	for _, d := range domains {
		sd := SiteDNS{Domain: d, A: []string{}, AAAA: []string{}}
		var errs []string
		for _, db := range []string{"ahostsv4", "ahostsv6"} {
			ips, err := getentIPs(ctx, sys, db, d)
			if err != nil {
				errs = append(errs, err.Error())
				continue
			}
			if db == "ahostsv4" {
				sd.A = ips
			} else {
				sd.AAAA = ips
			}
		}
		sd.Here = slices.ContainsFunc(sd.A, func(ip string) bool { return mine[ip] })
		sd.Error = strings.Join(errs, "; ")
		out.Sites = append(out.Sites, sd)
	}
	return out, nil
}

// parseIPAddrs đọc output của `ip -o addr show scope global`, ví dụ:
//
//	2: eth0    inet 203.0.113.74/24 brd 203.0.113.74 scope global eth0\       valid_lft forever ...
//	2: eth0    inet6 2001:db8::5/64 scope global \       valid_lft forever ...
func parseIPAddrs(s string) []IPAddr {
	out := []IPAddr{}
	for ln := range strings.Lines(s) {
		f := strings.Fields(ln)
		if len(f) < 4 || (f[2] != "inet" && f[2] != "inet6") {
			continue
		}
		p, err := netip.ParsePrefix(f[3])
		if err != nil {
			continue
		}
		a := p.Addr().Unmap()
		fam := "ipv4"
		if f[2] == "inet6" {
			fam = "ipv6"
		}
		out = append(out, IPAddr{
			Iface: strings.TrimSuffix(f[1], ":"), IP: a.String(), Prefix: p.Bits(), Family: fam,
			Public: !a.IsPrivate() && !a.IsLoopback() && !a.IsLinkLocalUnicast() && !cgnat.Contains(a),
		})
	}
	return out
}

// getentIPs trả các IP (không trùng, giữ thứ tự) mà getent db trả cho dom.
// Mã 2 của getent là "không có bản ghi": danh sách rỗng, không phải lỗi.
func getentIPs(ctx context.Context, sys system.System, db, dom string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, ipLookupTimeout)
	defer cancel()
	res, err := sys.Run(ctx, system.Cmd{Name: "getent", Args: []string{db, dom}})
	if err != nil && res.ExitCode != 2 {
		return []string{}, err
	}
	ips := []string{}
	for ln := range strings.Lines(res.Stdout) {
		f := strings.Fields(ln)
		if len(f) == 0 {
			continue
		}
		a, err := netip.ParseAddr(f[0])
		if err != nil {
			continue
		}
		// ahostsv6 trả cả IPv4 dạng ::ffff:a.b.c.d khi tên chỉ có bản ghi A:
		// đó không phải bản ghi AAAA.
		if db == "ahostsv6" && a.Is4In6() {
			continue
		}
		if s := a.Unmap().String(); !slices.Contains(ips, s) {
			ips = append(ips, s)
		}
	}
	return ips, nil
}

// osPrettyName lấy PRETTY_NAME trong nội dung /etc/os-release.
func osPrettyName(s string) string {
	for ln := range strings.Lines(s) {
		if v, ok := strings.CutPrefix(strings.TrimSpace(ln), "PRETTY_NAME="); ok {
			return strings.Trim(v, `"'`)
		}
	}
	return ""
}
