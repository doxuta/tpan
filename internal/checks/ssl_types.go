package checks

// Hợp đồng M5: phép kiểm của SSL (chặng DNS và TLS, mục 6.3 và 6.7 của
// docs/KE-HOACH.md) và các dòng của trang bảo mật (mục 6.8).
//
// File này chỉ có chú thích. Thợ đợt 1 viết hai file mới: ssl.go (tiền tố hàm
// không xuất "ssl") và security.go (tiền tố "sec"). Không sửa file của M1, M2.
// Không đạt thì luôn có Cause và ManualFix (mục 6.7). ID ổn định như dưới.
//
// ------------------------------------------------------------------- ssl.go
//
//	// DNSPointsHere (StageDNS, "dns.a:<domain>"): `getent ahostsv4 <domain>` và
//	// `getent ahostsv6 <domain>` so với địa chỉ của máy (`ip -o addr show scope
//	// global`). Không có bản ghi A nào: Fail. Có A mà không địa chỉ nào là của
//	// máy: Warn (máy có thể đứng sau NAT; bước thử HTTP của Plan sẽ quyết). Có
//	// AAAA mà không trỏ về máy: Warn (Let's Encrypt ưu tiên IPv6 khi có AAAA).
//	func DNSPointsHere(l site.Layout) Check
//
//	// TLSCert (StageTLS, "tls.cert:<domain>"): đọc l.CertFullchain() bằng
//	// sys.ReadFile, đọc chứng chỉ đầu tiên bằng crypto/x509. Không có file:
//	// Fail. Không khớp domain (VerifyHostname): Fail. Hết hạn: Fail. Còn dưới
//	// 14 ngày: Warn. Còn lại Pass; Summary nói còn bao nhiêu ngày và tổ chức
//	// cấp (Issuer.CommonName; chứng chỉ staging có chữ "(STAGING)").
//	func TLSCert(l site.Layout) Check
//
//	// HTTPRedirect (StageWeb, "http.redirect:<domain>"): curl tới
//	// http://<domain>/ với --resolve <domain>:80:127.0.0.1, không theo chuyển
//	// hướng: mã 301 và Location bắt đầu bằng https://<domain>/.
//	func HTTPRedirect(l site.Layout) Check
//
//	// HTTPSSite (StageEndToEnd, "https.site:<domain>"): như HTTPSite (site.go)
//	// nhưng https và cổng 443: --resolve <domain>:443:127.0.0.1, -k (chứng chỉ
//	// staging không được tin, TLSCert đã kiểm chứng chỉ riêng). 200, có dấu
//	// hiệu WordPress VÀ có chính domain.
//	func HTTPSSite(l site.Layout) Check
//
//	// CertRenewDryRun (StageTLS, "tls.renew:<domain>"):
//	//   certbot renew --dry-run --no-random-sleep-on-renew --cert-name <domain> --non-interactive [--server server]
//	// Mã 0: Pass. server rỗng thì không thêm --server. Nặng (gọi máy chủ ACME):
//	// chỉ dùng ở kiểm chứng của Plan SSL và trong e2e, không ở health định kỳ.
//	func CertRenewDryRun(l site.Layout, server string) Check
//
//	// SiteChecksSSL là SiteChecks cho site đã có chứng chỉ, đúng thứ tự chặng:
//	// TLSCert, VhostEnabled, PoolSocket, WPChecksums, WPConfigPerm, DBAccess,
//	// HTTPRedirect, HTTPSSite. (HTTPSite bị thay: cổng 80 giờ trả 301.)
//	func SiteChecksSSL(web, phpVersion string, l site.Layout) []Check
//
// -------------------------------------------------------------- security.go
//
// Mọi phép kiểm dưới đây có Stage là StageMachine và chỉ đọc. Trang bảo mật chủ
// yếu cảnh báo: Fail chỉ dùng khi đang có chỗ nguy hiểm thật.
//
//	// SSHDConfig chạy `sshd -T` (in cấu hình hiệu lực, đã gộp sshd_config.d)
//	// và trả từng khoá viết thường kèm mọi giá trị (port có thể nhiều dòng).
//	// Không phải Check: core dùng nó để biết cổng SSH thật khi bật ufw.
//	func SSHDConfig(ctx context.Context, sys system.System) (map[string][]string, Evidence, error)
//
//	// PanelListen trả các địa chỉ đang nghe cổng port (`ss -tlnH sport = :<port>`).
//	// exposed = có ít nhất một địa chỉ không phải loopback.
//	func PanelListen(ctx context.Context, sys system.System, port int) (addrs []string, exposed bool, ev Evidence, err error)
//
//	// UFW ("sec.ufw"): `ufw status verbose`. Tắt: Warn. Bật mà không cho cổng
//	// SSH nào của SSHDConfig: Fail. Bật và có SSH: Pass, Summary liệt kê cổng mở.
//	// Không có lệnh ufw: Warn.
//	func UFW() Check
//
//	// Fail2banSSHD ("sec.fail2ban"): `fail2ban-client status sshd`. Mã 0: Pass
//	// (Summary có số IP đang bị chặn). Không có lệnh: Warn "chưa cài". Lỗi
//	// khác: Warn kèm output.
//	func Fail2banSSHD() Check
//
//	// SSHRootLogin ("sec.ssh.root"): permitrootlogin của SSHDConfig. "yes":
//	// Warn. "prohibit-password", "without-password", "forced-commands-only",
//	// "no": Pass. Chỉ cảnh báo, ManualFix là lệnh để người tự sửa (mục 6.8).
//	func SSHRootLogin() Check
//
//	// SSHPasswordAuth ("sec.ssh.password"): passwordauthentication "yes": Warn.
//	func SSHPasswordAuth() Check
//
//	// Swap ("sec.swap"): đọc /proc/swaps. Không có dòng nào ngoài tiêu đề: Warn.
//	func Swap() Check
//
//	// SecurityUpdates ("sec.updates"): `apt-get -s -o Debug::NoLocking=1
//	// dist-upgrade`, đếm dòng "Inst " có "-security". Lớn hơn 0: Warn, Summary
//	// có số gói. Chỉ mô phỏng, không cài gì.
//	func SecurityUpdates() Check
//
//	// PanelExposed ("sec.panel"): PanelListen(port). Không nghe: Unknown. Chỉ
//	// loopback: Pass (chế độ riêng tư). Có địa chỉ ngoài loopback: Warn, kèm
//	// cách chuyển sang chế độ riêng tư (`tpan setup --private`) và lệnh SSH tunnel.
//	func PanelExposed(port int) Check
//
//	// SecurityChecks trả theo đúng thứ tự trên trang: UFW, Fail2banSSHD,
//	// SSHRootLogin, SSHPasswordAuth, Swap, SecurityUpdates, PanelExposed(port).
//	func SecurityChecks(panelPort int) []Check
