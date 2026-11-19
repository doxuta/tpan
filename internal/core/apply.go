package core

// Hợp đồng M6, đợt 0: thao tác hàng loạt (mục 7 của docs/KE-HOACH.md):
// `tpan apply -f sites.yaml [--prune]`, `tpan export`, và "dán danh sách" trên
// màn Tạo site. Kiểu và chữ ký ở đây; thân viết ở đợt 1 (file sites.yaml) và
// đợt 2 (Apply).
//
// Apply đưa máy về đúng danh sách: site thiếu thì tạo, site đã có (active) thì
// bỏ qua (không chạy lại Plan), site ngoài danh sách thì vào thùng rác khi
// Prune. Cả tạo lẫn đưa vào thùng rác nằm trong MỘT engine.Batch (một job):
//
//	Pre:    WPArchive (tải WordPress một lần cho cả lô), nếu có site mới
//	Unit tạo site, pha A: user, thư mục, DB, pool, WPFiles, WPConfig,
//	        .htaccess (Apache), vhost: steps.WithoutReload; useradd và các bước
//	        ghi file có Test bọc steps.Serial (một mutex cho cả lô)
//	Unit đưa vào thùng rác, pha A: các bước của PrepareRemove, WithoutReload
//	Commit: steps.PHPReload cho mỗi bản PHP trong lô, rồi steps.WebReload
//	Unit tạo site, pha C: WPInstall (bọc như wpInstallStep để biết có cài thật
//	        không), Verify: siteChecks với các site active đã có trong store
//	Parallel: 3
//
// Pha D (SSL, lần lượt từng site, có kiểm trước) chạy SAU job hàng loạt, mỗi
// site có ssl một job SSL riêng (dùng lại ca sử dụng SSL của M5), chỉ cho site
// vừa tạo thành công. Cô lập giữa các site cùng lô kiểm sau job (checks.RunAll
// trên mọi cặp site mới thành công) và nằm trong ApplyResult.Isolation, không
// hoàn tác gì: lúc pha C chạy song song, site anh em có thể đang dở.
//
// Job hàng loạt thành công: mỗi unit tạo site succeeded được SaveSite (active),
// mỗi unit thùng rác succeeded được đánh dấu trashed như PrepareRemove.

import (
	"context"
	"errors"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

// ---------------------------------------------------------------- apply

var errApplyTODO = errors.New("core: thao tác hàng loạt chưa viết (M6 đợt 2)")

// ApplyAction là việc Apply sẽ làm với một domain.
type ApplyAction string

const (
	ApplyCreate  ApplyAction = "create"  // chưa có: tạo
	ApplySkip    ApplyAction = "skip"    // đã có, active: bỏ qua
	ApplyTrash   ApplyAction = "trash"   // ngoài file và Prune: vào thùng rác
	ApplyRefused ApplyAction = "refused" // không làm được (đang ở thùng rác, máy đã có thứ của domain, thiếu email...)
)

// ApplyItem là một dòng của bản kế hoạch hàng loạt.
type ApplyItem struct {
	Domain string      `json:"domain"`
	Action ApplyAction `json:"action"`
	SSL    bool        `json:"ssl,omitempty"`
	// Code và Reason chỉ có khi Refused (Code như RefusedError.Code).
	Code   string    `json:"code,omitempty"`
	Reason i18n.Text `json:"reason,omitzero"`
}

// ApplyInput là đầu vào của Apply.
type ApplyInput struct {
	Sites []ApplySite
	Prune bool
}

// Apply là một lần áp danh sách đã qua điều kiện đầu. Dùng cho đúng một lần
// Execute (các Step giữ trạng thái để hoàn tác).
type Apply struct {
	Items []ApplyItem
	// Batch là job hàng loạt; Units rỗng khi không có gì để tạo hay xoá.
	Batch engine.Batch

	sites *Sites
}

// PrepareApply làm phần chỉ đọc: stack đã cài, phân loại từng domain (Create,
// Skip, Trash, Refused), sinh mật khẩu, dựng Batch. Từ chối cả lô (lỗi
// *RefusedError) chỉ khi không dựng được gì: stack chưa cài, đầu vào rỗng.
// Dòng nào không làm được thì thành ApplyRefused, các dòng khác vẫn đi.
func (s *Sites) PrepareApply(ctx context.Context, in ApplyInput) (*Apply, error) {
	return nil, errApplyTODO
}

// Empty: không có gì để tạo hay xoá (mọi dòng Skip hoặc Refused). Execute khi
// Empty thì không tạo job nào.
func (a *Apply) Empty() bool { return len(a.Batch.Units) == 0 }

// ApplyPreview là bản xem trước của Apply.
type ApplyPreview struct {
	Items []ApplyItem `json:"items"`
	// Preview là bản xem trước phẳng của Batch (engine.PreviewBatch); nil khi Empty.
	Preview *engine.Preview `json:"preview,omitempty"`
}

// Preview chỉ đọc máy.
func (a *Apply) Preview(ctx context.Context) (ApplyPreview, error) {
	_ = a.sites
	return ApplyPreview{}, errApplyTODO
}

// ApplySiteResult là kết quả của một site được tạo trong lô.
type ApplySiteResult struct {
	Domain      string            `json:"domain"`
	Status      engine.UnitStatus `json:"status"`
	WPInstalled bool              `json:"wp_installed"`
	AdminUser   string            `json:"admin_user,omitempty"`
	// AdminPassword chỉ có khi WPInstalled; JSON luôn "••••" (system.Secret),
	// người gọi tự Reveal đúng một lần, như SiteAddResult.
	AdminPassword system.Secret `json:"admin_password"`
}

// ApplyResult là kết quả của Apply.Execute.
type ApplyResult struct {
	Items []ApplyItem `json:"items"`
	// Report là kết quả job hàng loạt; nil khi Empty.
	Report *engine.BatchReport `json:"report,omitempty"`
	// Created: một dòng cho mỗi unit tạo site, theo thứ tự Items.
	Created []ApplySiteResult `json:"created"`
	// Isolation: phép kiểm cô lập giữa các site mới cùng lô (sau job).
	Isolation []checks.Result `json:"isolation,omitempty"`
	// SSL: job SSL của pha D, theo thứ tự (site lỗi SSL không làm hỏng site).
	SSL []engine.Report `json:"ssl,omitempty"`
}

// Execute chạy job hàng loạt (engine.ExecuteBatch), ghi bảng sites cho từng
// unit thành công, kiểm cô lập trong lô, rồi pha D. onEvent nhận sự kiện của
// MỌI job chạy ở đây (job hàng loạt trước, rồi từng job SSL; mỗi job JobID
// riêng). error chỉ khác nil khi không chạy được hoặc không ghi được bảng
// sites; kể cả khi đó ApplyResult vẫn mang mật khẩu quản trị đã có.
func (a *Apply) Execute(ctx context.Context, onEvent func(engine.Event)) (ApplyResult, error) {
	return ApplyResult{}, errApplyTODO
}
