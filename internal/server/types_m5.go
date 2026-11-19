package server

// Hợp đồng API của M5 (SSL, mục 6.3; trang bảo mật, mục 6.8). Tách khỏi
// types.go để các mốc chạy song song không sửa chung một file. Quy ước lỗi,
// dry_run, JobStarted giống M3 (types.go).
//
//	  GET    /api/v1/ssl                          → SSLOverview
//	  POST   /api/v1/ssl/settings                 SSLSettings → core.SSLConfig
//	  POST   /api/v1/sites/{domain}/ssl[?dry_run=1] → Preview | JobStarted
//	  GET    /api/v1/security                     → core.SecurityStatus
//	  POST   /api/v1/security/ufw[?dry_run=1]     → Preview | JobStarted
//	  POST   /api/v1/security/fail2ban[?dry_run=1] → Preview | JobStarted
//
// Lời từ chối của core.SSL (acme_email_missing, precondition_failed...) là 409
// kèm checks như mọi lời từ chối khác; ssl_server_invalid và email_invalid là 400.
// Mọi POST ghi vào audit: ssl.settings, site.ssl, security.ufw, security.fail2ban.

import "github.com/doxuta/tpan/internal/core"

// SSLOverview là nội dung màn SSL.
type SSLOverview struct {
	Config core.SSLConfig    `json:"config"`
	Sites  []core.SSLSiteRow `json:"sites"`
}

// SSLSettings là thân của POST /api/v1/ssl/settings. Trường vắng mặt (null)
// thì giữ nguyên. Không có trường server: địa chỉ ACME tuỳ chỉnh chỉ đặt ở CLI.
type SSLSettings struct {
	Email   *string `json:"email"`
	Staging *bool   `json:"staging"`
}
