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

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

var errNotWritten = errors.New("core: chưa viết")

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
	return siteChecks(cfg.Web, php, st.Layout(), others), nil
}

// Tên các cách sửa tự động (giá trị của checks.Result.AutoFix). Đối tượng
// cần sửa đọc từ Result.ID ("<loại>:<đối tượng>").
const (
	// "service.active:<unit>" Fail: steps.Service (systemctl enable --now).
	FixServiceStart = "service.start"
	// "php.socket:<domain>" Fail: khởi động lại PHP-FPM của site để socket
	// được tạo lại (steps.PHPFPMRestart). Danger: mọi site gián đoạn vài giây.
	FixPHPRestart = "php.restart"
	// "drift:<path>" Warn hoặc Fail: ghi lại file đó từ template (đúng Step
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
)

// AutoFixName trả tên cách sửa tự động cho r, rỗng khi không có (Pass,
// Unknown, hoặc loại phép kiểm không có cách sửa tự động như disk.usage,
// http.site). Hàm thuần: không chạm máy, không đọc settings.
func AutoFixName(r checks.Result) string {
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
}

// Fix là một lần sửa đã dựng Plan. Dùng cho đúng một lần Execute.
type Fix struct {
	// CheckID là Result.ID của phép kiểm được sửa; Name là cách sửa.
	CheckID string
	Name    string
	Plan    engine.Plan

	fixes *Fixes
}

// Prepare dựng Plan sửa cho r theo r.AutoFix (gọi lại AutoFixName(r) nếu
// trường rỗng). Plan.Kind là "fix." + Name, Plan.Target là đối tượng (domain,
// unit, hay đường dẫn), Plan.Verify chứa lại đúng phép kiểm r (dựng lại từ ID)
// để job chỉ thành công khi phép kiểm đã đạt (Warn của Drift không tính là lỗi
// nhưng Drift sau khi ghi lại phải Pass).
// Không có cách sửa tự động: *RefusedError code "no_auto_fix".
func (f *Fixes) Prepare(ctx context.Context, r checks.Result) (*Fix, error) {
	return nil, errNotWritten
}

// Preview xem trước Plan sửa (engine.Preview, có diff cho file).
func (x *Fix) Preview(ctx context.Context) (engine.Preview, error) {
	_ = x.fixes
	return engine.Preview{}, errNotWritten
}

// Execute chạy Plan sửa qua engine với Journal của Fixes. Engine ghi sha256
// mới của file đã ghi vào managed_files, nên phép kiểm lệch đạt lại.
func (x *Fix) Execute(ctx context.Context, onEvent func(engine.Event)) (engine.Report, error) {
	return engine.Report{}, errNotWritten
}
