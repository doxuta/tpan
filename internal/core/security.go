package core

// Ca sử dụng của trang bảo mật (mục 6.8 của docs/KE-HOACH.md). Hợp đồng M5
// (đợt 0).
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
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
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
	// Settings và Sites chỉ PrepareHarden (M14, types_m14.go) cần: stack đang
	// dùng web server nào, và danh sách site để ghi lại vhost, quyền log.
	Settings Settings
	Sites    SiteStore
	// WPCLISHA512 rỗng thì kiểm chứng của PrepareHarden so WP-CLI với mã đã
	// ghim (như Stack). Test và chế độ demo đặt sha512 của file giả.
	WPCLISHA512 string
}

// SecurityStatus là nội dung trang bảo mật: checks.SecurityChecks(PanelPort).
type SecurityStatus struct {
	Results []checks.Result `json:"results"`
}

// Status chạy các phép kiểm của trang bảo mật. Chỉ đọc.
func (s *Security) Status(ctx context.Context) (SecurityStatus, error) {
	return SecurityStatus{Results: checks.RunAll(ctx, s.Sys, checks.SecurityChecks(PanelPort))}, nil
}

// SecurityPlan là một Plan của trang bảo mật đã dựng xong, dùng cho đúng một
// lần Execute.
type SecurityPlan struct {
	Plan engine.Plan

	sec *Security
}

// sshPortUnknown là lời từ chối khi không biết chắc cổng SSH. Bật tường lửa mà
// đoán sai cổng là khoá chính mình ở ngoài máy, nên không bao giờ đoán 22.
func sshPortUnknown(why string) *RefusedError {
	return refuse("ssh_port_unknown", i18n.T(
		"không biết chắc sshd đang nghe cổng nào ("+why+"), nên không bật ufw: bật sai là mất đường SSH vào máy. Xem bằng: sudo sshd -T | grep -i '^port'",
		"cannot tell which port sshd listens on ("+why+"), so ufw is not enabled: a wrong guess locks you out of SSH. Check with: sudo sshd -T | grep -i '^port'"))
}

// PrepareUFW dựng Plan "security.ufw". Chỉ đọc máy.
func (s *Security) PrepareUFW(ctx context.Context) (*SecurityPlan, error) {
	cfg, _, err := checks.SSHDConfig(ctx, s.Sys)
	if err != nil {
		return nil, sshPortUnknown(err.Error())
	}
	ports := cfg["port"]
	if len(ports) == 0 {
		return nil, sshPortUnknown("sshd -T không in dòng port")
	}
	var rules []string
	add := func(port int) {
		if r := strconv.Itoa(port) + "/tcp"; !slices.Contains(rules, r) {
			rules = append(rules, r)
		}
	}
	for _, p := range ports {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n < 1 || n > 65535 {
			return nil, sshPortUnknown(fmt.Sprintf("sshd -T in cổng lạ %q", p))
		}
		add(n)
	}
	add(80)
	add(443)
	// Lỗi khi đọc cổng panel thì dừng: không biết thì có thể đóng mất đường vào
	// panel của chính người đang bấm nút.
	_, exposed, _, err := checks.PanelListen(ctx, s.Sys, PanelPort)
	if err != nil {
		return nil, fmt.Errorf("xem panel có nghe ra ngoài không (cổng %d): %w", PanelPort, err)
	}
	if exposed {
		add(PanelPort)
	}

	return &SecurityPlan{sec: s, Plan: engine.Plan{
		Kind:   "security.ufw",
		Target: "ufw",
		Title: i18n.T("Bật tường lửa ufw (mở "+strings.Join(rules, ", ")+")",
			"Enable the ufw firewall (open "+strings.Join(rules, ", ")+")"),
		Steps: []engine.Step{
			&steps.AptInstall{Packages: []string{"ufw"}},
			// Luật cho phép phải có TRƯỚC khi bật, để lúc bật thì đường SSH đã mở.
			&steps.UFWAllow{Rules: rules},
			&steps.UFWEnable{},
		},
		Verify: []checks.Check{checks.UFW()},
	}}, nil
}

// PrepareFail2ban dựng Plan "security.fail2ban". Chỉ đọc máy.
func (s *Security) PrepareFail2ban(_ context.Context) (*SecurityPlan, error) {
	jail, err := templates.Fail2banSSHD()
	if err != nil {
		return nil, fmt.Errorf("dựng Plan fail2ban: %w", err)
	}
	return &SecurityPlan{sec: s, Plan: engine.Plan{
		Kind:   "security.fail2ban",
		Target: "fail2ban",
		Title:  i18n.T("Cài và bật fail2ban cho SSH", "Install and enable fail2ban for SSH"),
		Steps: []engine.Step{
			&steps.AptInstall{Packages: []string{"fail2ban"}},
			&steps.File{
				Path: Fail2banJail, Content: jail, Perm: 0o644, Owner: system.Root,
				Test:   &system.Cmd{Name: "fail2ban-client", Args: []string{"-t"}},
				Reload: &system.Cmd{Name: "fail2ban-client", Args: []string{"reload"}},
				Title:  i18n.T("Bật jail sshd của fail2ban", "Enable fail2ban's sshd jail"),
				Why: i18n.T(
					"Máy có SSH mở ra internet bị dò mật khẩu liên tục. Jail này cho fail2ban đọc log của sshd (từ journal của systemd) và chặn IP thử sai nhiều lần.",
					"A machine with SSH open to the internet gets constant password guessing. This jail makes fail2ban read sshd's log (from the systemd journal) and block IPs that fail too often."),
			},
			&steps.Service{Unit: "fail2ban"},
		},
		Verify: []checks.Check{checks.Fail2banSSHD()},
	}}, nil
}

// Preview là bản xem trước. Chỉ đọc máy.
func (p *SecurityPlan) Preview(ctx context.Context) (engine.Preview, error) {
	return engine.New(p.sec.Sys, engine.Options{}).Preview(ctx, p.Plan)
}

// Execute chạy Plan. error chỉ khác nil khi không chạy được; job hỏng thì xem
// Report.Job.Status.
func (p *SecurityPlan) Execute(ctx context.Context, onEvent func(engine.Event)) (engine.Report, error) {
	if p.sec.Journal == nil {
		return engine.Report{}, errors.New(p.Plan.Kind + ": thiếu Journal, không ghi được lịch sử job")
	}
	rep, err := engine.New(p.sec.Sys, engine.Options{Journal: p.sec.Journal, OnEvent: onEvent}).Execute(ctx, p.Plan)
	if err != nil {
		return rep, fmt.Errorf("chạy %s: %w", p.Plan.Kind, err)
	}
	return rep, nil
}
