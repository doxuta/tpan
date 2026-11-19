package core

// Hợp đồng M5 (đợt 0): ca sử dụng SSL (mục 6.3 của docs/KE-HOACH.md). Chữ ký,
// kiểu và hằng dưới đây là thứ server và cmd/tpan dựa vào; thân hàm do thợ
// đợt 2 viết (thay mọi chỗ trả errM5).
//
// Plan "site.ssl" cho site <d>, đúng thứ tự:
//  1. Điều kiện đầu (chỉ đọc, trong PrepareIssue): site có trong bảng sites và
//     active, stack đã cài, có email (ssl.email), checks.DNSPointsHere không Fail.
//  2. Dir <acmeRoot>/.well-known/acme-challenge (0755, root).
//  3. steps.ACMEProbe (thử đường HTTP; bỏ qua khi chứng chỉ đã đúng).
//  4. steps.CertbotCert (Server theo SSLConfig.ServerURL).
//  5. steps.DefaultSSL(web): site mặc định 443 (và mod ssl của Apache).
//  6. steps.SiteVhostSSL: vhost có 443, cổng 80 chuyển hướng.
//  7. steps.WPSiteURL https://<d>.
//  8. File hook gia hạn /etc/letsencrypt/renewal-hooks/deploy/tpan (0755,
//     templates.RenewHook("/usr/local/bin/tpan")).
//  9. Kiểm chứng: checks.SiteChecksSSL, Service certbot.timer,
//     checks.CertRenewDryRun(l, server tuỳ chỉnh nếu có).
// Job thành công thì ghi site.SSL = true vào bảng sites.

import (
	"context"
	"errors"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// errM5 đánh dấu thân hàm chưa viết. Thợ đợt 2 xoá khi viết xong.
var errM5 = errors.New("M5: chưa viết")

// Khoá của SSL trong bảng settings (mục 4.4).
const (
	// SettingSSLEmail: email đăng ký với Let's Encrypt (thư báo sắp hết hạn).
	SettingSSLEmail = "ssl.email"
	// SettingSSLStaging: "1" bật, "0" tắt; chưa ghi thì hiểu là BẬT (mục 6.3).
	SettingSSLStaging = "ssl.staging"
	// SettingSSLServer: ẩn, chỉ cho test. Địa chỉ thư mục ACME (https://...)
	// thay cho Let's Encrypt; có giá trị thì thắng staging.
	SettingSSLServer = "ssl.server"
)

// RenewHookPath là hook certbot chạy sau mỗi lần gia hạn thành công.
const RenewHookPath = "/etc/letsencrypt/renewal-hooks/deploy/tpan"

// SSLConfig là cấu hình SSL đọc từ settings.
type SSLConfig struct {
	Email   string `json:"email"`
	Staging bool   `json:"staging"`
	// Server là ssl.server (rỗng khi dùng Let's Encrypt). Không hiện trên giao diện.
	Server string `json:"server,omitempty"`
}

// ServerURL là địa chỉ ACME đưa cho certbot: Server nếu có, không thì
// steps.LetsEncryptStaging hoặc steps.LetsEncryptProd theo Staging.
func (c SSLConfig) ServerURL() string { return "" }

// SSLSiteRow là một dòng của màn SSL: một site active và phép kiểm chứng chỉ
// của nó (checks.TLSCert; site chưa có SSL thì Cert là nil).
type SSLSiteRow struct {
	Domain validate.Domain `json:"domain"`
	SSL    bool            `json:"ssl"`
	Cert   *checks.Result  `json:"cert,omitempty"`
}

// SSLSettingsInput là thay đổi cấu hình; trường nil thì giữ nguyên.
type SSLSettingsInput struct {
	Email   *validate.Email
	Staging *bool
	// Server: chỉ CLI (cờ ẩn) đặt; chuỗi rỗng là xoá. Phải là https://...
	Server *string
}

// SSL gom những gì ca sử dụng SSL cần. Dựng mới cho mỗi lệnh.
type SSL struct {
	Sys      system.System
	Settings Settings
	Store    SiteStore
	// Journal nhận job và sự kiện khi Execute; nil chỉ hợp lệ khi xem trước.
	Journal engine.Journal
}

// Config đọc cấu hình SSL.
func (s *SSL) Config(ctx context.Context) (SSLConfig, error) { return SSLConfig{}, errM5 }

// SetConfig ghi thay đổi cấu hình. Server sai dạng thì từ chối ssl_server_invalid.
func (s *SSL) SetConfig(ctx context.Context, in SSLSettingsInput) (SSLConfig, error) {
	return SSLConfig{}, errM5
}

// List trả mọi site active, sắp theo domain, kèm phép kiểm chứng chỉ của site có SSL.
func (s *SSL) List(ctx context.Context) ([]SSLSiteRow, error) { return nil, errM5 }

// SSLIssue là một lần xin (hoặc chạy lại) chứng chỉ đã qua điều kiện đầu. Dùng
// cho đúng một lần Execute.
type SSLIssue struct {
	Site   site.Site
	Layout site.Layout
	Config SSLConfig
	// Preconditions là kết quả mọi điều kiện đầu (kể cả Pass, Warn) để hiện ra.
	Preconditions []checks.Result
	Plan          engine.Plan
}

// PrepareIssue làm phần chỉ đọc của "xin SSL": kiểm điều kiện đầu, dựng Plan.
// Từ chối thì trả *RefusedError: site_not_found, site_trashed,
// stack_not_installed, acme_email_missing, precondition_failed (kèm Checks).
func (s *SSL) PrepareIssue(ctx context.Context, d validate.Domain) (*SSLIssue, error) {
	return nil, errM5
}

// Preview là bản xem trước. Chỉ đọc máy.
func (i *SSLIssue) Preview(ctx context.Context) (engine.Preview, error) {
	return engine.Preview{}, errM5
}

// Execute chạy Plan; job thành công thì ghi site.SSL = true. error chỉ khác nil
// khi không chạy được hoặc không ghi được bảng sites; job hỏng thì xem
// Report.Job.Status.
func (i *SSLIssue) Execute(ctx context.Context, onEvent func(engine.Event)) (engine.Report, error) {
	return engine.Report{}, errM5
}

// ReloadWeb là việc của `tpan internal reload-web` (hook gia hạn gọi): đọc
// stack.web, kiểm config (nginx -t hoặc apachectl configtest), đúng thì
// `systemctl reload` web server đó. Stack chưa cài thì không làm gì.
func ReloadWeb(ctx context.Context, sys system.System, settings Settings) error { return errM5 }
