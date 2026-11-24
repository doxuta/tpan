package templates

// Hợp đồng M5 (đợt 0): template của SSL (mục 6.3 của docs/KE-HOACH.md) và của
// trang bảo mật (mục 6.8). Thân hàm do thợ đợt 1 viết; chữ ký và tài liệu dưới
// đây là thứ steps và core dựa vào, không đổi.

import (
	"bytes"
	"fmt"

	"github.com/doxuta/tpan/internal/site"
)

// sslData là dữ liệu đưa vào các template SSL: dữ liệu site (có thể trống với
// hai site mặc định, chỉ có Header) cộng đường dẫn chứng chỉ.
type sslData struct {
	siteData
	Cert string
	Key  string
}

// hookData là dữ liệu của hook gia hạn.
type hookData struct {
	Header  string
	TpanBin string
}

func sslRender(name string, d any) ([]byte, error) {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, d); err != nil {
		return nil, fmt.Errorf("render template %s: %w", name, err)
	}
	return buf.Bytes(), nil
}

// sslSiteData kiểm Layout và acmeRoot như vhost cổng 80 (cùng một cửa kiểm), rồi
// thêm đường dẫn chứng chỉ. Đường dẫn chứng chỉ suy từ domain đã kiểm, nhưng vẫn
// qua danh sách ký tự cho phép như mọi đường dẫn khác đi vào config.
func sslSiteData(l site.Layout, php, acmeRoot string) (sslData, error) {
	d, err := siteVhostData(l, php, acmeRoot)
	if err != nil {
		return sslData{}, err
	}
	for _, p := range []string{l.CertFullchain(), l.CertPrivkey()} {
		if err := siteCheckPath(p); err != nil {
			return sslData{}, err
		}
	}
	return sslData{siteData: d, Cert: l.CertFullchain(), Key: l.CertPrivkey()}, nil
}

// Hai cặp file chứng chỉ tự ký có sẵn trên Ubuntu khi cài gói ssl-cert. Chỉ
// site mặc định 443 của Apache dùng (Apache bắt buộc vhost 443 có chứng chỉ);
// trình duyệt không bao giờ tin nó, và cũng không cần: site mặc định chỉ để
// từ chối request lạ.
const (
	SnakeoilCert = "/etc/ssl/certs/ssl-cert-snakeoil.pem"
	SnakeoilKey  = "/etc/ssl/private/ssl-cert-snakeoil.key"
)

// NginxSiteSSL sinh vhost Nginx của site đã có chứng chỉ:
//   - server cổng 80: giữ nguyên `location ^~ /.well-known/acme-challenge/`
//     (gia hạn vẫn đi qua cổng 80) rồi `return 301 https://<domain>$request_uri`
//     cho mọi đường dẫn khác;
//   - server cổng 443 (`listen 443 ssl;` và `[::]:443`): mọi luật của bản cổng
//     80 hiện có (root, log, upload 64 MB, chặn file ẩn, chặn PHP trong uploads,
//     try_files, fastcgi_pass), cộng ssl_certificate l.CertFullchain(),
//     ssl_certificate_key l.CertPrivkey().
//
// Không có default_server ở đây: request lạ vào 443 rơi vào tpan-default-ssl.
func NginxSiteSSL(l site.Layout, php, acmeRoot string) ([]byte, error) {
	d, err := sslSiteData(l, php, acmeRoot)
	if err != nil {
		return nil, fmt.Errorf("sinh vhost nginx SSL: %w", err)
	}
	return sslRender("site-nginx-ssl.conf.tmpl", d)
}

// ApacheSiteSSL sinh vhost Apache của site đã có chứng chỉ:
//   - <VirtualHost *:80>: Alias của acme-challenge giữ nguyên, mọi đường dẫn khác
//     chuyển 301 sang https://<domain>/ BẰNG CÁCH KHÔNG nuốt acme-challenge
//     (RedirectMatch có loại trừ, hoặc RewriteRule có RewriteCond; mod rewrite đã
//     bật từ M1). Kiểm trên máy ảo: curl http://<domain>/.well-known/acme-challenge/x
//     phải ra file (hoặc 404), không ra 301;
//   - <VirtualHost *:443>: mọi luật của bản cổng 80 hiện có, cộng SSLEngine on,
//     SSLCertificateFile l.CertFullchain(), SSLCertificateKeyFile l.CertPrivkey().
//
// Cần mod ssl đã bật (Plan SSL bật trước khi ghi file này). mode như ApacheSite.
func ApacheSiteSSL(l site.Layout, php, acmeRoot string, mode site.HtaccessMode) ([]byte, error) {
	d, err := sslSiteData(l, php, acmeRoot)
	if err == nil {
		d.siteData, err = withHtaccess(d.siteData, mode)
	}
	if err != nil {
		return nil, fmt.Errorf("sinh vhost apache SSL: %w", err)
	}
	return sslRender("site-apache-ssl.conf.tmpl", d)
}

// SiteVhost là chỗ DUY NHẤT chọn template vhost của một site: web "nginx" hoặc
// "apache", ssl theo site.Site.SSL. Tạo site, chạy lại, xin SSL và phát hiện
// lệch (M4) đều gọi hàm này, để không có chỗ nào ghi đè vhost 443 bằng bản cổng 80.
// php là bản PHP site đang dùng (site.Site.PHPVersion): vhost trỏ vào socket của bản đó.
// mode là chế độ .htaccess (site.Site.Htaccess), chỉ vhost Apache dùng; với
// nginx nó bị bỏ qua vì nginx không đọc .htaccess.
//
// Từ M19 đây là SiteVhostNames (m19.go) với site.Names{}: site không tên phụ.
func SiteVhost(l site.Layout, web, php, acmeRoot string, ssl bool, mode site.HtaccessMode) ([]byte, error) {
	return SiteVhostNames(l, web, php, acmeRoot, ssl, mode, site.Names{})
}

// NginxDefaultSSL sinh /etc/nginx/sites-available/tpan-default-ssl.conf: server
// `listen 443 ssl default_server;` (và [::]:443) với `ssl_reject_handshake on;`
// (Nginx ≥ 1.19.4): từ chối bắt tay TLS với mọi tên không có vhost, nên không
// cần chứng chỉ nào và không lộ tên site nào qua chứng chỉ mặc định.
func NginxDefaultSSL() ([]byte, error) {
	return sslRender("nginx-default-ssl.conf.tmpl", sslData{siteData: siteData{Header: Header}})
}

// ApacheDefaultSSL sinh /etc/apache2/sites-available/000-tpan-default-ssl.conf:
// <VirtualHost *:443> ServerName tpan-default.invalid, SSLEngine on, chứng chỉ
// SnakeoilCert/SnakeoilKey, chặn hết (Require all denied, trả 403). Tên 000-
// để Apache nạp nó trước mọi vhost 443 của site và coi là mặc định (giống
// 000-tpan-default.conf của M1).
func ApacheDefaultSSL() ([]byte, error) {
	return sslRender("apache-default-ssl.conf.tmpl", sslData{
		siteData: siteData{Header: Header},
		Cert:     SnakeoilCert,
		Key:      SnakeoilKey,
	})
}

// RenewHook sinh /etc/letsencrypt/renewal-hooks/deploy/tpan (0755). certbot chạy
// mọi file trong thư mục này sau mỗi lần gia hạn thành công. Nội dung:
//
//	#!/bin/sh
//	<Header>
//	exec <tpanBin> internal reload-web
//
// Dòng đầu bắt buộc là shebang, nên Header nằm ở dòng 2. tpanBin là đường dẫn
// tuyệt đối (thường /usr/local/bin/tpan), qua cùng luật kiểm đường dẫn của
// AcmeRoot.
func RenewHook(tpanBin string) ([]byte, error) {
	// Cùng luật với AcmeRoot: đường dẫn tuyệt đối, mỗi đoạn chỉ gồm ký tự an
	// toàn. Nhờ vậy chèn thẳng vào dòng exec của shell script không cần nháy,
	// và không có cách nào thêm lệnh thứ hai vào dòng đó.
	if err := siteCheckPath(tpanBin); err != nil {
		return nil, fmt.Errorf("sinh hook gia hạn: %w", err)
	}
	return sslRender("renew-hook.tmpl", hookData{Header: Header, TpanBin: tpanBin})
}

// Fail2banSSHD sinh /etc/fail2ban/jail.d/tpan-sshd.conf (mục 6.8): bật jail
// sshd với `backend = systemd` (Ubuntu 24.04 ghi log sshd vào journal; có máy
// không còn /var/log/auth.log). Header là dòng chú thích đầu (fail2ban đọc '#'
// là chú thích).
func Fail2banSSHD() ([]byte, error) {
	return sslRender("fail2ban-sshd.tmpl", sslData{siteData: siteData{Header: Header}})
}
