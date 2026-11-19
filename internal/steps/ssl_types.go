package steps

// Hợp đồng M5: các Step của SSL (mục 6.3 của docs/KE-HOACH.md) và của trang
// bảo mật (mục 6.8).
//
// File này chỉ có chú thích. Thợ đợt 1 viết hai file mới: ssl.go (tiền tố hàm
// và biến không xuất "ssl") và ufw.go (tiền tố "ufw"). Mỗi file tự có
// `var _ engine.Step = (*X)(nil)` cho Step của mình, không sửa common.go hay
// file của M1, M2. Core (đợt 2) ghép Plan dựa trên đúng tên và trường xuất dưới
// đây. Mọi đường dẫn chứng chỉ lấy từ site.Layout (internal/site/ssl.go).
//
// ------------------------------------------------------------------- ssl.go
//
//	// ACME directory của Let's Encrypt. Core chọn một trong hai (hoặc
//	// settings ssl.server) rồi đưa vào CertbotCert.Server.
//	const (
//		LetsEncryptProd    = "https://acme-v02.api.letsencrypt.org/directory"
//		LetsEncryptStaging = "https://acme-staging-v02.api.letsencrypt.org/directory"
//	)
//
//	// CertbotCert xin chứng chỉ cho một site bằng webroot:
//	//   certbot certonly --webroot -w Webroot -d <domain> --cert-name <domain>
//	//     --non-interactive --agree-tos -m Email --server Server
//	// Thêm --force-renewal khi đã có lineage nhưng `server = ` trong
//	// l.CertRenewal() khác Server (đổi từ staging sang thật).
//	// Check: có l.CertFullchain() VÀ l.CertRenewal() có dòng `server = Server`.
//	// Undo: không làm gì (giữ chứng chỉ để lần chạy lại không tốn lượt của
//	// Let's Encrypt; xem docs/thay-doi/M5.md). Info.Why nói rõ điều đó.
//	// Server phải là https://...; Email đã qua validate. Sai thì Check, Apply lỗi.
//	type CertbotCert struct {
//		Layout  site.Layout
//		Webroot string // /var/lib/tpan/acme
//		Email   validate.Email
//		Server  string
//	}
//
//	// ACMEProbe là bước thử đường HTTP trước khi gọi Let's Encrypt (6.3 bước 1,
//	// ý 2): ghi một file ngẫu nhiên vào Webroot/.well-known/acme-challenge/,
//	// gọi lại `curl -sS -m 10 http://<domain>/.well-known/acme-challenge/<file>`
//	// KHÔNG có --resolve (đi đúng đường DNS như Let's Encrypt sẽ đi), so nội
//	// dung, rồi luôn xoá file (kể cả khi lỗi). Không khớp thì lỗi, nói rõ:
//	// domain đang trỏ về đâu theo DNS của máy, curl trả gì.
//	// Check: Cert.Check (chứng chỉ đã đúng thì không cần thử, giữ idempotent),
//	// hoặc true sau khi Apply của chính bước này đã thành công.
//	// Undo: không làm gì (Apply không để lại gì).
//	type ACMEProbe struct {
//		Layout  site.Layout
//		Webroot string
//		Cert    *CertbotCert
//	}
//
//	// CertbotDelete xoá chứng chỉ khi xoá hẳn site (6.4):
//	//   certbot delete --cert-name <domain> --non-interactive
//	// Check: không còn l.CertRenewal() và l.CertDir(). Danger, NoUndo.
//	type CertbotDelete struct{ Layout site.Layout }
//
//	// WPSiteURL đặt hai tuỳ chọn home và siteurl của WordPress về URL, dưới
//	// user của site: `wp option get|update home|siteurl ... --path=l.Public`.
//	// URL chỉ nhận "http://<domain>" hoặc "https://<domain>" (đúng domain của
//	// Layout). Check: cả hai bằng URL. Undo: trả giá trị cũ của từng tuỳ chọn
//	// mà Apply đã đổi.
//	type WPSiteURL struct {
//		Layout site.Layout
//		URL    string
//	}
//
//	// SiteVhostSSL giống SiteVhost (site_web.go) nhưng nội dung là
//	// templates.SiteVhost(l, web, phpVersion, acmeRoot, true): ghi file vhost (Test, Reload
//	// như SiteVhost) rồi Symlink bật nó. Ghi đè vhost cổng 80 đang có; File của
//	// M1 giữ bản cũ nên Undo trả lại được.
//	func SiteVhostSSL(l site.Layout, web, phpVersion, acmeRoot string) ([]engine.Step, error)
//
//	// DefaultSSL trả các bước của site mặc định cổng 443 cho web:
//	//   nginx:  File tpan-default-ssl.conf (templates.NginxDefaultSSL) + Symlink,
//	//           Test nginx -t, Reload systemctl reload nginx;
//	//   apache: AptInstall{ssl-cert}, ApacheModules{Enable: ssl},
//	//           File 000-tpan-default-ssl.conf (templates.ApacheDefaultSSL) +
//	//           Symlink, Test apachectl configtest, Reload systemctl reload apache2.
//	// Đường dẫn: /etc/nginx/sites-{available,enabled}/tpan-default-ssl.conf,
//	// /etc/apache2/sites-{available,enabled}/000-tpan-default-ssl.conf.
//	func DefaultSSL(web string) ([]engine.Step, error)
//
// ------------------------------------------------------------------- ufw.go
//
//	// UFWAllow đảm bảo ufw có các luật cho phép, ví dụ "22/tcp", "443/tcp".
//	// Rule chỉ nhận dạng <cổng 1-65535>/tcp. Check: `ufw show added` có đủ dòng
//	// "ufw allow <rule>" (lệnh này đọc được cả khi ufw đang tắt). Apply: `ufw
//	// allow <rule>` cho từng luật còn thiếu. Undo: `ufw delete allow <rule>`
//	// cho đúng những luật Apply đã thêm.
//	type UFWAllow struct{ Rules []string }
//
//	// UFWEnable bật tường lửa: `ufw --force enable` (--force: không hỏi y/n).
//	// Check: dòng đầu của `ufw status` là "Status: active". Undo: `ufw disable`.
//	// Danger (bật sai luật là mất đường SSH vào máy): core luôn đặt UFWAllow
//	// có cổng SSH thật đứng trước.
//	type UFWEnable struct{}
