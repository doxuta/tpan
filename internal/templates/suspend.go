package templates

// Template của site TẠM KHOÁ (M13, mục 6.12 của docs/KE-HOACH.md): vhost Nginx
// và Apache trả 403 kèm một trang tĩnh chung cho mọi đường dẫn, riêng thử
// thách Let's Encrypt (/.well-known/acme-challenge/) vẫn đọc từ webroot ACME
// chung như vhost thường, để chứng chỉ gia hạn được trong lúc site bị khoá.
//
// Cổng 80 của bản có SSL KHÔNG chuyển sang https như vhost SSL thường: nó trả
// thẳng trang tạm khoá. Lý do: chuyển hướng chỉ thêm một lượt đi về để rồi vẫn
// gặp 403; kiểm chứng gọi được cả cổng 80 lẫn 443 và đòi cùng một câu trả lời;
// và không có HSTS nào (vhost SSL của tpan cố ý không bật) bắt trình duyệt đi
// https. Thử thách ACME vẫn ở cổng 80 như mọi lúc.
//
// Vhost tạm khoá không trỏ tới thư mục site hay socket PHP nào: pool của site
// đã tắt, và DocumentRoot là thư mục của trang tạm khoá chứ không phải public
// (ở Apache, .htaccess của site có AllowOverride All có thể mở lại đường vào).

import (
	"fmt"

	"github.com/doxuta/tpan/internal/site"
)

// Trang tạm khoá dùng chung cho mọi site: một file do tpan ghi (root 0644, thư
// mục 0755), web server (www-data) đọc được. /var/lib/tpan giữ 0755 vì webroot
// ACME bên trong cũng phải đi qua được (mục 5).
const (
	SuspendedDir = "/var/lib/tpan/suspended"
	// SuspendedURI là đường dẫn nội bộ mà error_page / ErrorDocument chuyển
	// tới. Trùng tên file để Nginx dùng root, không cần alias.
	SuspendedURI  = "/tpan-suspended.html"
	SuspendedPage = SuspendedDir + SuspendedURI
	// SuspendedMarker nằm trong thân trang tạm khoá. Phép kiểm http.suspended
	// đòi thấy nó: 403 không kèm chuỗi này là 403 của thứ khác (site mặc định
	// của Apache, luật chặn file ẩn), không phải trang tạm khoá của tpan.
	SuspendedMarker = "tpan-suspended-page"
)

// suspendedData là dữ liệu của bốn vhost tạm khoá.
type suspendedData struct {
	Header   string
	Domain   string
	LogDir   string
	AcmeRoot string
	Dir      string
	URI      string
	Cert     string
	Key      string
	Marker   string
	// Tên phụ (M19): Aliases80 mọi tên phụ, Aliases443 chỉ tên có trong chứng chỉ.
	Aliases80  []string
	Aliases443 []string
}

// SuspendedVhost sinh vhost tạm khoá của site: web "nginx" hoặc "apache", ssl
// theo site.Site.SSL (thêm server/VirtualHost cổng 443 với chứng chỉ của site).
// Đây là bản thứ ba bên cạnh bản thường và bản có SSL (SiteVhost); core chọn
// giữa chúng ở đúng một chỗ (core.siteVhost).
//
// Từ M19 đây là SuspendedVhostNames (m19.go) với site.Names{}.
func SuspendedVhost(l site.Layout, web, acmeRoot string, ssl bool) ([]byte, error) {
	return SuspendedVhostNames(l, web, acmeRoot, ssl, site.Names{})
}

// SuspendedVhostNames như SuspendedVhost cộng tên phụ active của site: cổng 80
// liệt kê mọi tên phụ, cổng 443 chỉ tên có trong chứng chỉ (n.Covered). Không
// vẽ khối chuyển hay luật nào: mọi tên trả trang tạm khoá 403.
func SuspendedVhostNames(l site.Layout, web, acmeRoot string, ssl bool, n site.Names) ([]byte, error) {
	d, err := suspendedDataOf(l, acmeRoot, ssl)
	if err == nil {
		d.Aliases80, d.Aliases443, err = suspendedAliases(l, n, ssl)
	}
	if err != nil {
		return nil, fmt.Errorf("sinh vhost tạm khoá %s: %w", web, err)
	}
	var name string
	switch {
	case web == "nginx" && ssl:
		name = "site-nginx-suspended-ssl.conf.tmpl"
	case web == "nginx":
		name = "site-nginx-suspended.conf.tmpl"
	case web == "apache" && ssl:
		name = "site-apache-suspended-ssl.conf.tmpl"
	case web == "apache":
		name = "site-apache-suspended.conf.tmpl"
	default:
		return nil, fmt.Errorf("sinh vhost tạm khoá: %w: web %q phải là \"nginx\" hoặc \"apache\"", ErrParam, web)
	}
	return sslRender(name, d)
}

// SuspendedHTML sinh trang tạm khoá (SuspendedPage). Không có tham số: một
// trang cho mọi site, không nói tên site hay lý do khoá (người lạ không cần
// biết). Header nằm trong chú thích HTML ở dòng đầu: chú thích đứng trước
// <!doctype html> không đưa trình duyệt vào chế độ quirks (HTML Living
// Standard, chế độ chèn "initial").
func SuspendedHTML() []byte {
	out, err := sslRender("suspended-page.html.tmpl", suspendedData{Header: Header, Marker: SuspendedMarker})
	if err != nil {
		// Không có tham số: template hỏng là lỗi lập trình, test bắt được.
		panic(err)
	}
	return out
}

// suspendedDataOf kiểm Layout và acmeRoot như vhost thường (cùng các cửa kiểm
// của site.go, ssl.go), trừ bản PHP: vhost tạm khoá không trỏ tới pool nào.
func suspendedDataOf(l site.Layout, acmeRoot string, ssl bool) (suspendedData, error) {
	if l.Domain.IsZero() {
		return suspendedData{}, fmt.Errorf("%w: Layout zero (chưa có domain)", ErrParam)
	}
	if l != site.LayoutOf(l.Domain) {
		return suspendedData{}, fmt.Errorf("%w: Layout của %s không khớp site.LayoutOf", ErrParam, l.Domain)
	}
	if err := (DefaultSite{AcmeRoot: acmeRoot}).validate(); err != nil {
		return suspendedData{}, err
	}
	paths := []string{l.LogDir}
	if ssl {
		paths = append(paths, l.CertFullchain(), l.CertPrivkey())
	}
	for _, p := range paths {
		if err := siteCheckPath(p); err != nil {
			return suspendedData{}, err
		}
	}
	d := suspendedData{
		Header:   Header,
		Domain:   l.Domain.String(),
		LogDir:   l.LogDir,
		AcmeRoot: acmeRoot,
		Dir:      SuspendedDir,
		URI:      SuspendedURI,
	}
	if ssl {
		d.Cert, d.Key = l.CertFullchain(), l.CertPrivkey()
	}
	return d, nil
}
