package core

// Hợp đồng M4: phần của core mà health và sửa tự động cần (mục 6.7 của
// docs/KE-HOACH.md). Thân hàm do thợ của phần core viết.
//
//   - Stack.Checks, Sites.Checks: trả các phép kiểm (chưa chạy) để gói health
//     chạy theo lịch, ghi kết quả, mở đóng sự cố. Cùng bộ phép kiểm với kiểm
//     chứng sau khi cài, tạo (stackChecks, siteChecks), để "lúc tạo đã đúng"
//     và "bây giờ còn đúng" là một định nghĩa.
//   - AutoFixName: tên cách sửa tự động cho một kết quả không đạt; health điền
//     vào Result.AutoFix trước khi ghi, giao diện chỉ hiện nút "Sửa tự động"
//     khi trường này khác rỗng.
//   - Fixes: dựng Plan sửa từ một kết quả, rồi xem trước hoặc chạy như mọi
//     thao tác khác (xem trước, xác nhận, kiểm chứng, hoàn tác).

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// Checks trả bộ phép kiểm của stack (stackChecks dựng từ settings).
// installed=false (chưa có stack.php_version) thì không trả phép kiểm nào.
func (s *Stack) Checks(ctx context.Context) (cs []checks.Check, installed bool, err error) {
	cfg, err := s.Config(ctx)
	if err != nil {
		return nil, false, err
	}
	if cfg.PHPVersion == "" {
		return nil, false, nil
	}
	_, sha := s.wpcli()
	return stackChecks(cfg.Web, func() string { return cfg.PHPVersion }, sha), true, nil
}

// Checks trả bộ phép kiểm của site d (siteChecks: đường đi request, rồi cô
// lập hai chiều với từng site active khác). Stack chưa cài: nil, không lỗi.
// Site không có: lỗi như find (RefusedError site_not_found).
func (s *Sites) Checks(ctx context.Context, d validate.Domain) ([]checks.Check, error) {
	st, err := s.find(ctx, d)
	if err != nil {
		return nil, err
	}
	cfg, err := (&Stack{Settings: s.Settings}).Config(ctx)
	if err != nil {
		return nil, err
	}
	if cfg.Web == "" || cfg.PHPVersion == "" {
		return nil, nil
	}
	php := st.PHPVersion
	if php == "" {
		php = cfg.PHPVersion
	}
	others, err := s.others(ctx, d)
	if err != nil {
		return nil, err
	}
	return append(siteEdgeChecks(st.Layout(), st.SSL), siteChecks(cfg.Web, php, st.Layout(), others, st.SSL)...), nil
}

// siteEdgeChecks là hai chặng đầu của đường đi request mà chỉ health xét: DNS,
// và TLS khi site chưa xin SSL (site có SSL thì TLSCert đã nằm trong
// siteChecks). Không đưa vào kiểm chứng sau thao tác: DNS do nhà cung cấp tên
// miền quản lý, hỏng ở đó không được làm hoàn tác một lần đổi web server.
//   - DNS luôn là Warn (checks.Soft): bản ghi nằm ở nhà cung cấp tên miền,
//     tpan không sửa được, nên không mở sự cố và không làm mọi lệnh khác thoát 1
//     (e2e M10: `stack switch` thành công vẫn thoát 1 vì DNS của site SSL).
//     Site SSL mà DNS hỏng lâu thì gia hạn hỏng và tls.cert báo lỗi đúng lúc.
//   - Site chưa có SSL: thiếu chứng chỉ là Warn: việc còn phải làm.
//
// DNS là phép kiểm nặng (gọi resolver, có thể chờ timeout): chạy mỗi giờ, khi
// gõ `tpan health` và khi bấm kiểm lại, không ở vòng 5 phút (mục 6.7).
func siteEdgeChecks(l site.Layout, ssl bool) []checks.Check {
	dns := checks.Heavy(checks.Soft(checks.DNSPointsHere(l)))
	if ssl {
		return []checks.Check{dns}
	}
	return []checks.Check{dns, checks.Soft(checks.TLSCert(l))}
}

// Tên các cách sửa tự động (giá trị của checks.Result.AutoFix). Đối tượng
// cần sửa đọc từ Result.ID ("<loại>:<đối tượng>").
const (
	// "service.active:<unit>" Fail: steps.Service (systemctl enable --now).
	FixServiceStart = "service.start"
	// "php.socket:<domain>" Fail: khởi động lại PHP-FPM của site để socket
	// được tạo lại (steps.PHPFPMRestart). Danger: mọi site gián đoạn vài giây.
	FixPHPRestart = "php.restart"
	// "drift:<path>" Fail: ghi lại file đó từ template (đúng Step
	// File của Plan đã ghi nó: SitePool, SiteVhost, SiteHtaccess, hay file của
	// stack), có kiểm cú pháp và reload như lúc tạo. Chỉ có khi core dựng lại
	// được nội dung của path (file của tpan setup như unit systemd thì không).
	FixFileRewrite = "file.rewrite"
	// "config.nginx:nginx" | "config.apache:apache2" Fail: ghi lại mọi file
	// web của tpan (site mặc định, vhost của mọi site active). Step File bỏ
	// qua file đã đúng nội dung, nên chỉ file bị sửa hỏng được ghi.
	FixWebRewrite = "web.rewrite"
	// "wp.config:<domain>" Fail: đưa wp-config.php về đúng quyền và chủ mà
	// checks.WPConfigPerm đòi.
	FixWPConfigPerm = "wp.config.perm"
	// "db.access:<domain>" Fail: mở khoá user DB của site (steps.DBUserUnlock).
	// Phép kiểm DBAccess Fail vì lý do khác (sai mật khẩu, mất DB) thì Plan
	// này không đổi gì và kiểm chứng vẫn Fail: job lỗi, người dùng sửa tay.
	FixDBUnlock = "db.unlock"
	// "tls.cert:<domain>" Fail (site đã có SSL mà chứng chỉ mất, hỏng, sắp
	// hết hạn): xin lại chứng chỉ bằng đúng Plan của `tpan ssl issue`, kể cả
	// điều kiện đầu (email, DNS). Site chưa từng xin SSL chỉ có Warn nên không
	// có nút này: xin chứng chỉ lần đầu là một quyết định, không phải sửa lỗi.
	FixSSLIssue = "ssl.issue"
)

// AutoFixName trả tên cách sửa tự động cho r, rỗng khi không có (Pass,
// Unknown, hoặc loại phép kiểm không có cách sửa tự động như disk.usage,
// http.site). Hàm thuần: không chạm máy, không đọc settings.
func AutoFixName(r checks.Result) string {
	kind, _, _ := strings.Cut(r.ID, ":")
	switch r.Status {
	case checks.Fail:
	case checks.Warn:
		// Từ đợt 2 lệch là Fail; vẫn nhận Warn để kết quả cũ trong DB còn sửa được.
		if kind == "drift" {
			return FixFileRewrite
		}
		return ""
	default: // Pass, Unknown: không có gì để sửa, hoặc chưa biết sửa gì.
		return ""
	}
	switch kind {
	case "service.active":
		return FixServiceStart
	case "php.socket":
		return FixPHPRestart
	case "drift":
		return FixFileRewrite
	case "config.nginx", "config.apache":
		return FixWebRewrite
	case "web.vhost":
		// Vhost của một site mất, bị thay, hay chưa bật (symlink ở
		// sites-enabled bị xoá): ghi lại vhost và symlink của riêng site đó
		// (Fixes.vhostRewrite), Verify là chính web.vhost.
		return FixWebRewrite
	case "wp.config":
		return FixWPConfigPerm
	case "db.access":
		return FixDBUnlock
	case "tls.cert":
		return FixSSLIssue
	}
	return ""
}

// Fixes gom những gì sửa tự động cần. Dựng mới cho mỗi lệnh, như Stack, Sites.
type Fixes struct {
	Sys      system.System
	Settings Settings
	Store    SiteStore
	// Journal nhận job khi Execute; nil chỉ hợp lệ khi xem trước.
	Journal engine.Journal
	// WPCLISHA512 như Stack (dùng khi dựng lại file của stack).
	WPCLISHA512 string
	// Files đọc sha256 tpan đang coi là chuẩn của một file (managed_files).
	// web.rewrite dùng nó để không ghi đè file người dùng đã nhận bản sửa tay
	// làm chuẩn. nil: không lọc, mọi file web khác template đều được ghi lại.
	Files FileRecords
}

// FileRecords là phần của store mà sửa tự động cần từ bảng managed_files.
// *store.Store thoả giao diện này.
type FileRecords interface {
	// FileSHA256 trả sha256 (hex) đang ghi cho path; ok=false khi không có dòng.
	FileSHA256(ctx context.Context, path string) (sha string, ok bool, err error)
}

// Fix là một lần sửa đã dựng Plan. Dùng cho đúng một lần Execute.
type Fix struct {
	// CheckID là Result.ID của phép kiểm được sửa; Name là cách sửa.
	CheckID string
	Name    string
	Plan    engine.Plan

	fixes *Fixes
	// ssl có khi Name là FixSSLIssue: Execute đi qua SSLIssue.Execute để job
	// thành công thì bảng sites ghi site.SSL = true như `tpan ssl issue`.
	ssl *SSLIssue
}

// Prepare dựng Plan sửa cho r theo r.AutoFix (gọi lại AutoFixName(r) nếu
// trường rỗng). Plan.Kind là "fix." + Name, Plan.Target là đối tượng (domain,
// unit, hay đường dẫn), Plan.Verify chứa lại đúng phép kiểm r (dựng lại từ ID)
// để job chỉ thành công khi phép kiểm đã đạt (Drift sau khi ghi lại phải Pass).
// Không có cách sửa tự động: *RefusedError code "no_auto_fix".
func (f *Fixes) Prepare(ctx context.Context, r checks.Result) (*Fix, error) {
	name := AutoFixName(r)
	// r.AutoFix khác tên suy từ ID và Status thì không tin: tên sửa phải khớp
	// đúng phép kiểm, không để người gọi chọn một cách sửa cho đối tượng khác.
	if name == "" || (r.AutoFix != "" && r.AutoFix != name) {
		return nil, noAutoFix(r.ID)
	}
	if name == FixSSLIssue {
		return f.sslIssue(ctx, r)
	}
	plan, err := f.plan(ctx, name, r)
	if err != nil {
		return nil, err
	}
	plan.Kind = "fix." + name
	return &Fix{CheckID: r.ID, Name: name, Plan: plan, fixes: f}, nil
}

// sslIssue dựng sửa "ssl.issue": dùng nguyên SSL.PrepareIssue (điều kiện đầu,
// Plan, kiểm chứng) để sửa tự động và `tpan ssl issue` là một việc. Lời từ
// chối của nó (thiếu email, DNS chưa trỏ về) đi thẳng ra người dùng.
func (f *Fixes) sslIssue(ctx context.Context, r checks.Result) (*Fix, error) {
	_, obj, _ := strings.Cut(r.ID, ":")
	d, err := validate.ParseDomain(obj)
	if err != nil {
		return nil, noAutoFix(r.ID)
	}
	issue, err := (&SSL{Sys: f.Sys, Settings: f.Settings, Store: f.Store, Journal: f.Journal}).PrepareIssue(ctx, d)
	if err != nil {
		return nil, err
	}
	issue.Plan.Kind = "fix." + FixSSLIssue
	return &Fix{CheckID: r.ID, Name: FixSSLIssue, Plan: issue.Plan, fixes: f, ssl: issue}, nil
}

// Preview xem trước Plan sửa (engine.Preview, có diff cho file).
func (x *Fix) Preview(ctx context.Context) (engine.Preview, error) {
	return engine.New(x.fixes.Sys, engine.Options{}).Preview(ctx, x.Plan)
}

// Execute chạy Plan sửa qua engine với Journal của Fixes. Engine ghi sha256
// mới của file đã ghi vào managed_files, nên phép kiểm lệch đạt lại.
func (x *Fix) Execute(ctx context.Context, onEvent func(engine.Event)) (engine.Report, error) {
	f := x.fixes
	if f.Journal == nil {
		return engine.Report{}, errors.New("sửa tự động: thiếu Journal, không ghi được lịch sử job")
	}
	if x.ssl != nil {
		x.ssl.Plan = x.Plan
		return x.ssl.Execute(ctx, onEvent)
	}
	rep, err := engine.New(f.Sys, engine.Options{Journal: f.Journal, OnEvent: onEvent}).Execute(ctx, x.Plan)
	if err != nil {
		return rep, fmt.Errorf("chạy %s cho %s: %w", x.Plan.Kind, x.CheckID, err)
	}
	return rep, nil
}
