package templates

import "github.com/doxuta/tpan/internal/site"

// Từ M14 hai hàm sinh vhost Apache nhận thêm chế độ .htaccess. Các bảng test có
// từ trước dùng chúng như giá trị hàm ba tham số (cùng dạng với NginxSite); hai
// hàm bọc này giữ dạng đó với chế độ mặc định của site mới.
func apacheSite(l site.Layout, php, acmeRoot string) ([]byte, error) {
	return ApacheSite(l, php, acmeRoot, site.HtaccessSafe)
}

func apacheSiteSSL(l site.Layout, php, acmeRoot string) ([]byte, error) {
	return ApacheSiteSSL(l, php, acmeRoot, site.HtaccessSafe)
}
