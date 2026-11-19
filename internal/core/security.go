package core

// Hợp đồng M5 (đợt 0): ca sử dụng của trang bảo mật (mục 6.8 của
// docs/KE-HOACH.md). Thân hàm do thợ đợt 2 viết (thay mọi chỗ trả errM5).
//
// Plan "security.ufw":
//  1. AptInstall{ufw} (máy bản "minimized" có thể chưa có).
//  2. UFWAllow: mọi cổng SSH thật (checks.SSHDConfig, khoá "port"; không đọc
//     được thì TỪ CHỐI ssh_port_unknown, không đoán 22), 80/tcp, 443/tcp, và
//     PanelPort/tcp chỉ khi panel nghe ra ngoài loopback (checks.PanelListen).
//  3. UFWEnable (Danger).
//  Kiểm chứng: checks.UFW().
//
// Plan "security.fail2ban":
//  1. AptInstall{fail2ban}.
//  2. File /etc/fail2ban/jail.d/tpan-sshd.conf (templates.Fail2banSSHD, 0644,
//     Test `fail2ban-client -t`).
//  3. Service{fail2ban} (đang chạy và bật khi khởi động); file jail đổi thì
//     reload: `fail2ban-client reload`.
//  Kiểm chứng: checks.Fail2banSSHD().

import (
	"context"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/system"
)

// PanelPort là cổng HTTPS của panel (tpan setup ghi vào unit systemd).
const PanelPort = 8443

// Fail2banJail là file jail sshd do tpan quản lý.
const Fail2banJail = "/etc/fail2ban/jail.d/tpan-sshd.conf"

// Security gom những gì trang bảo mật cần. Dựng mới cho mỗi lệnh.
type Security struct {
	Sys system.System
	// Journal nhận job và sự kiện khi Execute; nil chỉ hợp lệ khi xem trước.
	Journal engine.Journal
}

// SecurityStatus là nội dung trang bảo mật: checks.SecurityChecks(PanelPort).
type SecurityStatus struct {
	Results []checks.Result `json:"results"`
}

// Status chạy các phép kiểm của trang bảo mật. Chỉ đọc.
func (s *Security) Status(ctx context.Context) (SecurityStatus, error) {
	return SecurityStatus{}, errM5
}

// SecurityPlan là một Plan của trang bảo mật đã dựng xong, dùng cho đúng một
// lần Execute.
type SecurityPlan struct {
	Plan engine.Plan

	sec *Security
}

// PrepareUFW dựng Plan "security.ufw". Chỉ đọc máy.
func (s *Security) PrepareUFW(ctx context.Context) (*SecurityPlan, error) { return nil, errM5 }

// PrepareFail2ban dựng Plan "security.fail2ban". Chỉ đọc máy.
func (s *Security) PrepareFail2ban(ctx context.Context) (*SecurityPlan, error) { return nil, errM5 }

// Preview là bản xem trước. Chỉ đọc máy.
func (p *SecurityPlan) Preview(ctx context.Context) (engine.Preview, error) {
	_ = p.sec
	return engine.Preview{}, errM5
}

// Execute chạy Plan. error chỉ khác nil khi không chạy được; job hỏng thì xem
// Report.Job.Status.
func (p *SecurityPlan) Execute(ctx context.Context, onEvent func(engine.Event)) (engine.Report, error) {
	return engine.Report{}, errM5
}
