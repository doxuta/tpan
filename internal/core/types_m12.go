package core

// Hợp đồng M12 (đợt 0): phiên bản và cập nhật (mục 6.11 của docs/KE-HOACH.md).
// Kiểu và tên khoá ở đây là thứ server, CLI và giao diện dựa vào; thân các ca
// sử dụng nằm ở php.go, stack_sources.go, updates.go, selfupdate.go (thợ đợt 2).
//
// Ghi chú đã kiểm trên máy ảo trước khi viết hợp đồng (docs/thay-doi/M12.md):
//   - Kho PHP của Ondřej Surý: https://packages.sury.org/php/ có dist noble.
//     Thêm kho mà không ghim thì gói meta php-fpm của kho đó trỏ sang bản PHP
//     mới nhất (8.4, 8.5...) và php8.3-* của Ubuntu bị thay bằng bản của kho:
//     bản lớn tự nhảy. Ghim cả kho ở 100 thì cài php8.4 lỗi vì php8.4-common cần
//     php-common (>= 2:95~). Cách đúng: ghim kho ở 100, riêng php-common và
//     php<bản>* của các bản cài từ kho này ở 500.
//   - PHP-FPM không chạy khi không có pool nào ("No pool defined"), nên gỡ pool
//     www của gói phải kèm một pool tpan-idle (ondemand, socket chỉ root) để
//     bản PHP chưa có site nào vẫn khởi động được.

import (
	"context"
	"fmt"
	"time"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/system"
)

// Nguồn gói khác Ubuntu (mục 6.11). Giá trị lưu trong settings.
const (
	// SourceNginxStable, SourceNginxMainline: kho của nginx.org (stable hoặc
	// mainline). Gói nginx của nginx.org chạy dưới user nginx và chỉ đọc
	// conf.d/*.conf, nên Plan cài thêm bước thích nghi (conf.d/tpan.conf đọc
	// sites-enabled, gỡ conf.d/default.conf, cho user nginx vào nhóm www-data).
	SourceNginxStable   Source = "nginx.org"
	SourceNginxMainline Source = "nginx.org-mainline"
	// SourceMariaDB: kho của MariaDB, dòng ở SettingDBLine.
	SourceMariaDB Source = "mariadb.org"
	// SourceSury: kho PHP của Ondřej Surý (packages.sury.org). Chỉ là nguồn của
	// từng bản PHP, không phải lựa chọn lúc cài stack: stack luôn cài PHP của
	// Ubuntu, kho này chỉ được thêm khi cài một bản Ubuntu không có.
	SourceSury Source = "sury"
)

// Khoá settings mới của M12.
const (
	// SettingDBLine là dòng MariaDB khi stack.db_source = mariadb.org, ví dụ "11.8".
	SettingDBLine = "stack.db_line"
	// SettingSelfURL là gốc tải bản phát hành cho `tpan self-update`; rỗng thì
	// DefaultSelfURL. Ở gốc phải có tpan-linux-<arch> và SHA256SUMS.
	SettingSelfURL = "update.self_url"
)

// DefaultSelfURL là nơi M11 đặt bản phát hành.
const DefaultSelfURL = "https://github.com/doxuta/tpan/releases/latest/download"

// TpanBin là đường dẫn file chạy của tpan trên máy chủ (mục 5).
const TpanBin = "/usr/local/bin/tpan"

// InstallOptions là lựa chọn nguồn gói lúc cài stack lần đầu (mục 6.11). Zero
// value là cài toàn bộ từ kho của Ubuntu, đúng như M1. Chạy lại trên máy đã
// cài thì phải trùng cấu hình đã ghi; khác thì từ chối source_change_unsupported.
type InstallOptions struct {
	WebSource Source // "", SourceUbuntu, SourceNginxStable, SourceNginxMainline (chỉ khi web = nginx)
	DBSource  Source // "", SourceUbuntu, SourceMariaDB
	DBLine    string // bắt buộc khi DBSource = SourceMariaDB, dạng "11.8"
}

// ---- PHP theo từng site ----

// PHPSupport là hạn hỗ trợ của một bản PHP, hỏi php.net lúc chạy
// (https://www.php.net/releases/states.php). Ngày dạng YYYY-MM-DD.
type PHPSupport struct {
	State         string `json:"state"` // stable | security | eol
	ActiveUntil   string `json:"active_until,omitempty"`
	SecurityUntil string `json:"security_until,omitempty"`
}

// PHPVersionRow là một bản PHP trên màn Stack và trong `tpan php list`.
type PHPVersionRow struct {
	Version   string `json:"version"` // "8.3"
	Installed bool   `json:"installed"`
	// Default: bản mặc định của stack (settings stack.php_version), site mới dùng bản này.
	Default bool `json:"default"`
	// Sites là domain các site (mọi trạng thái) đang dùng bản này, sắp theo chữ.
	Sites []string `json:"sites"`
	// Source: SourceUbuntu nếu kho Ubuntu có bản này, không thì SourceSury.
	Source Source `json:"source"`
	// Support là ghi chú hạn hỗ trợ; nil khi không hỏi được php.net.
	Support *PHPSupport `json:"support,omitempty"`
}

// PHPList là danh sách bản PHP: mọi bản đã cài cộng mọi bản các kho đang cấu
// hình có gói php<bản>-fpm (apt-cache), sắp tăng dần theo số. Không ghi cứng
// bản nào trong mã (mục 6.11).
type PHPList struct {
	Versions []PHPVersionRow `json:"versions"`
	// SuryEnabled: kho của Ondřej Surý đã được thêm. Chưa thêm thì danh sách chỉ
	// có bản của Ubuntu; cài một bản khác (tpan php install 8.4) sẽ thêm kho.
	SuryEnabled bool `json:"sury_enabled"`
	// WPRecommended là bản PHP WordPress.org khuyên dùng
	// (https://api.wordpress.org/core/serve-happy/1.0/); rỗng khi không hỏi được.
	WPRecommended string `json:"wp_recommended,omitempty"`
	// NotesOK: hỏi được cả hai nguồn ghi chú. false thì danh sách vẫn đủ, chỉ thiếu ghi chú.
	NotesOK bool `json:"notes_ok"`
}

// PHP gom những gì ca sử dụng PHP cần. Dựng mới cho mỗi lệnh.
type PHP struct {
	Sys      system.System
	Settings Settings
	Sites    SiteStore
	// Journal nhận job khi Execute; nil chỉ hợp lệ khi xem trước.
	Journal engine.Journal
}

// ---- Cập nhật ----

// PendingPackage là một gói đang có bản mới, đọc từ `apt-get -s dist-upgrade`
// (dòng "Inst <tên> [<cũ>] (<mới> <kho>, <kho> [<arch>])").
type PendingPackage struct {
	Name string `json:"name"`
	From string `json:"from"`
	To   string `json:"to"`
	// Security: một trong các kho của bản mới có đuôi "-security".
	Security bool `json:"security"`
	// Origins là các kho trong ngoặc, ví dụ "Ubuntu:24.04/noble-updates".
	Origins []string `json:"origins"`
}

// MariaDBLine là một dòng LTS của MariaDB cao hơn dòng đang chạy, hỏi
// https://downloads.mariadb.org/rest-api/mariadb/ lúc chạy (chỉ lấy
// release_support_type "Long Term Support", release_status "Stable").
type MariaDBLine struct {
	Line string `json:"line"` // "11.8"
	EOL  string `json:"eol"`  // YYYY-MM-DD
}

// MariaDBStatus là phần MariaDB của màn Cập nhật.
type MariaDBStatus struct {
	Source  Source `json:"source"`
	Line    string `json:"line"`    // dòng đang chạy, "10.11"
	Version string `json:"version"` // bản đầy đủ của gói mariadb-server
	// Upgrades là các dòng LTS nâng lên được; rỗng khi không hỏi được (không
	// mạng) hoặc đã ở dòng mới nhất.
	Upgrades []MariaDBLine `json:"upgrades"`
}

// SelfStatus là phần `tpan self-update` của màn Cập nhật (chỉ hiện lệnh, không
// có nút: tự thay file chạy của panel đang phục vụ trang là cắt chính job đó).
type SelfStatus struct {
	Version string `json:"version"` // buildinfo của bản đang chạy
	URL     string `json:"url"`     // gốc tải (settings update.self_url hoặc DefaultSelfURL)
	// HasOld: có TpanBin + ".old" để `tpan self-update --rollback`.
	HasOld bool `json:"has_old"`
}

// UpdateStatus là nội dung màn Cập nhật và `tpan update --check`.
type UpdateStatus struct {
	// Packages là gói của stack có bản mới (web server, PHP, MariaDB, certbot,
	// curl và thư viện cùng nguồn), sắp theo tên.
	Packages []PendingPackage `json:"packages"`
	// Others, OthersSecurity: số gói khác của hệ điều hành đang chờ (bản vá bảo
	// mật của chúng do unattended-upgrades tự cài).
	Others         int `json:"others"`
	OthersSecurity int `json:"others_security"`
	// ListsUpdated là lần cuối danh sách gói được làm mới (mtime của
	// /var/lib/apt/periodic/update-success-stamp hoặc /var/lib/apt/lists); zero
	// khi không biết.
	ListsUpdated time.Time `json:"lists_updated,omitzero"`
	// Auto là kết quả checks.UnattendedUpgrades (bản vá bảo mật tự cài hằng ngày).
	Auto checks.Result `json:"auto"`
	// AutoOrigins là các kho ngoài (nginx.org, MariaDB, sury) đang có trong
	// danh sách tự vá của tpan.
	AutoOrigins []string      `json:"auto_origins"`
	MariaDB     MariaDBStatus `json:"mariadb"`
	Self        SelfStatus    `json:"self"`
}

// Updates gom những gì màn Cập nhật cần. Dựng mới cho mỗi lệnh.
type Updates struct {
	Sys      system.System
	Settings Settings
	Sites    SiteStore
	Journal  engine.Journal
	// Version là buildinfo.Version của bản đang chạy.
	Version string
}

// SelfUpdate là `tpan self-update` (chỉ CLI).
type SelfUpdate struct {
	Sys      system.System
	Settings Settings
	Journal  engine.Journal
	// Arch là GOARCH của máy (amd64, arm64): chọn file tpan-linux-<arch>.
	Arch string
	// Version là buildinfo.Version của bản đang chạy.
	Version string
}

// ---- Plan đã dựng, dùng chung cho mọi ca sử dụng của M12 ----

// Planned (khác Prepared của M7, vốn dựng từ PlanRequest) là một Plan đã qua điều kiện đầu, sẵn sàng xem trước hoặc chạy đúng
// một lần (Step giữ trạng thái để hoàn tác). after chạy khi job thành công, để
// ghi settings hay bảng sites (ví dụ bản PHP mới của site); after lỗi thì
// Execute trả lỗi kèm Report của job đã xong.
type Planned struct {
	Plan engine.Plan

	sys     system.System
	journal engine.Journal
	after   func(ctx context.Context, rep engine.Report) error
}

// newPlanned dựng Planned. journal nil chỉ dùng được cho Preview.
func newPlanned(sys system.System, journal engine.Journal, plan engine.Plan, after func(context.Context, engine.Report) error) *Planned {
	return &Planned{Plan: plan, sys: sys, journal: journal, after: after}
}

// Preview là bản xem trước của Plan. Chỉ đọc máy.
func (p *Planned) Preview(ctx context.Context) (engine.Preview, error) {
	return engine.New(p.sys, engine.Options{}).Preview(ctx, p.Plan)
}

// Execute chạy Plan qua engine (lấy khoá, ghi job). error chỉ khác nil khi
// không chạy được hoặc after lỗi; job hỏng thì xem Report.Job.Status.
func (p *Planned) Execute(ctx context.Context, onEvent func(engine.Event)) (engine.Report, error) {
	if p.journal == nil {
		return engine.Report{}, fmt.Errorf("%s: thiếu Journal, không ghi được lịch sử job", p.Plan.Kind)
	}
	rep, err := engine.New(p.sys, engine.Options{Journal: p.journal, OnEvent: onEvent}).Execute(ctx, p.Plan)
	if err != nil {
		return rep, fmt.Errorf("chạy %s: %w", p.Plan.Kind, err)
	}
	if rep.Job.Status != engine.JobSucceeded || p.after == nil {
		return rep, nil
	}
	if err := p.after(ctx, rep); err != nil {
		return rep, fmt.Errorf("job %s xong nhưng chưa ghi được kết quả: %w", rep.Job.ID, err)
	}
	return rep, nil
}
