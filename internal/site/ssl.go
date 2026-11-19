package site

// Đường dẫn chứng chỉ Let's Encrypt của một site (mục 6.3 của docs/KE-HOACH.md).
// certbot luôn được gọi với --cert-name <domain>, nên "lineage" (bộ file của một
// chứng chỉ qua các lần gia hạn) mang đúng tên domain, không bao giờ thành
// <domain>-0001. Nhờ vậy mọi đường dẫn suy được từ domain như Layout.

// CertDir là /etc/letsencrypt/live/<domain>: symlink tới bản chứng chỉ mới nhất.
func (l Layout) CertDir() string { return "/etc/letsencrypt/live/" + l.Domain.String() }

// CertFullchain là chứng chỉ của site kèm chứng chỉ trung gian; vhost dùng file này.
func (l Layout) CertFullchain() string { return l.CertDir() + "/fullchain.pem" }

// CertPrivkey là khoá riêng của chứng chỉ (0600, chỉ root đọc; web server đọc
// lúc khởi động bằng quyền root rồi mới hạ quyền).
func (l Layout) CertPrivkey() string { return l.CertDir() + "/privkey.pem" }

// CertRenewal là file cấu hình gia hạn của certbot cho site: ghi địa chỉ máy
// chủ ACME đã cấp (Let's Encrypt thật hay staging), webroot, email.
func (l Layout) CertRenewal() string {
	return "/etc/letsencrypt/renewal/" + l.Domain.String() + ".conf"
}
