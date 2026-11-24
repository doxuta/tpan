package core

// M28 đợt 2, B1: tên dùng để vào panel là tên dành riêng.
//
// Panel chạy ở https://<tên>:8443 với cookie phiên __Host-tpan. Cookie
// host-only nhưng trình duyệt không tách cookie theo cổng: một site mang đúng
// <tên> (cổng 80/443) nhận được cookie phiên của admin khi admin mở trang đó.
// Nên không ai (mọi vai, mọi chủ, cả CLI) tạo được site mới mang đúng tên dùng
// để vào panel. Hai nguồn tên:
//
//	(a) các tên DNS trong chứng chỉ của panel (tpan setup ghi hostname vào SAN);
//	(b) các Host đã thấy ở lần đăng nhập đúng của admin (RecordPanelHost).
//
// ponytail: chưa có phép kiểm health báo site ĐÃ CÓ mang tên của panel (máy
// nâng cấp lên có thể đã có). Thêm khi làm health của M28 hoặc mốc sau.
// ponytail: panel chạy bằng --tls-cert ở đường dẫn khác thì (a) không thấy
// chứng chỉ đó; khi ấy chỉ còn (b) che (tên được ghi từ lần admin đăng nhập
// đầu tiên).

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"slices"
	"strings"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/validate"
)

const (
	// SettingPanelHosts là khoá settings giữ danh sách (JSON, mới nhất trước)
	// các tên đã dùng để vào panel ở lần đăng nhập đúng của admin.
	SettingPanelHosts = "panel.hosts"
	// panelHostsMax là trần của danh sách: chừng này tên gần nhất là đủ.
	panelHostsMax = 20
	// panelCertPath là chứng chỉ do tpan setup sinh. Cùng giá trị với
	// health.PanelCertPath và setupCertPath (cmd/tpan/setup.go); không import
	// health vì health import core.
	panelCertPath = "/etc/tpan/tls/cert.pem"
)

// SettingsRW là settings đọc và ghi được (*store.Store thoả).
type SettingsRW interface {
	Settings
	SetSetting(ctx context.Context, key, value string) error
}

// PanelHostName chuẩn hoá giá trị Host của một request thành tên miền: bỏ
// cổng, hạ chữ thường, phải qua validate.ParseDomain. IP (kể cả [::1]) và tên
// không hợp lệ thì ok=false.
func PanelHostName(host string) (validate.Domain, bool) {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if net.ParseIP(host) != nil {
		return validate.Domain{}, false
	}
	d, err := validate.ParseDomain(host)
	if err != nil {
		return validate.Domain{}, false
	}
	return d, true
}

// RecordPanelHost ghi tên trong Host vào danh sách tên dành riêng của panel
// (đưa lên đầu, bỏ trùng, giữ panelHostsMax tên). Host là IP hay tên không hợp
// lệ thì không ghi gì. Người gọi (đăng nhập đúng của admin) chỉ ghi nhật ký
// khi lỗi, không làm hỏng lần đăng nhập.
//
// ponytail: đọc rồi ghi, không khoá: hai admin đăng nhập cùng lúc bằng hai tên
// khác nhau có thể mất một tên; lần đăng nhập sau ghi lại.
func RecordPanelHost(ctx context.Context, st SettingsRW, host string) error {
	d, ok := PanelHostName(host)
	if !ok {
		return nil
	}
	names, err := panelHosts(ctx, st)
	if err != nil {
		return err
	}
	if len(names) > 0 && names[0] == d.String() {
		return nil // thường gặp: không ghi lại DB mỗi lần đăng nhập
	}
	names = slices.DeleteFunc(names, func(n string) bool { return n == d.String() })
	names = append([]string{d.String()}, names...)
	if len(names) > panelHostsMax {
		names = names[:panelHostsMax]
	}
	b, err := json.Marshal(names)
	if err != nil {
		return err
	}
	return st.SetSetting(ctx, SettingPanelHosts, string(b))
}

// panelHosts đọc danh sách tên đã ghi. Giá trị hỏng thì coi như rỗng (lần ghi
// sau viết lại), không chặn việc tạo site.
func panelHosts(ctx context.Context, st Settings) ([]string, error) {
	v, ok, err := st.GetSetting(ctx, SettingPanelHosts)
	if err != nil || !ok {
		return nil, err
	}
	var names []string
	if json.Unmarshal([]byte(v), &names) != nil {
		return nil, nil
	}
	return names, nil
}

// panelCertNames trả các tên DNS trong chứng chỉ của panel, hạ chữ thường.
// Không đọc hay không parse được (máy demo, máy giả, chưa setup) thì rỗng.
func (s *Sites) panelCertNames() []string {
	b, err := s.Sys.ReadFile(panelCertPath)
	if err != nil {
		return nil
	}
	blk, _ := pem.Decode(b)
	if blk == nil {
		return nil
	}
	c, err := x509.ParseCertificate(blk.Bytes)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(c.DNSNames))
	for _, n := range c.DNSNames {
		out = append(out, strings.ToLower(n))
	}
	return out
}

// checkPanelReserved từ chối domain trùng đúng một tên dùng để vào panel.
// Chỉ gọi cho site mới (chạy lại site đã có thì không chặn).
func (s *Sites) checkPanelReserved(ctx context.Context, d validate.Domain) error {
	names, err := panelHosts(ctx, s.Settings)
	if err != nil {
		return fmt.Errorf("đọc tên dành riêng của panel: %w", err)
	}
	if !slices.Contains(names, d.String()) && !slices.Contains(s.panelCertNames(), d.String()) {
		return nil
	}
	return refuse(CodeDomainReserved, i18n.T(
		"tên "+d.String()+" là địa chỉ dùng để vào panel; site mang tên này nhận được cookie phiên đăng nhập của panel. Vào panel bằng một tên khác hoặc bằng IP nếu thật sự muốn site ở tên này",
		"the name "+d.String()+" is the address used to reach the panel; a site with this name would receive the panel's login session cookie. Reach the panel through another name or by IP if you really want a site at this name"))
}
