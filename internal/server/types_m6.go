package server

// Hợp đồng API của M6 (thùng rác, mục 6.4; hàng loạt, mục 7). Tách khỏi
// types.go để các mốc chạy song song không sửa chung một file. Quy ước lỗi,
// dry_run, JobStarted giống M3 (types.go).
//
//	  DELETE /api/v1/sites/{domain}[?dry_run=1]          → Preview | JobStarted   (M6: đưa vào thùng rác, không còn xoá hẳn)
//	  POST   /api/v1/sites/{domain}/restore[?dry_run=1]  → Preview | JobStarted
//	  GET    /api/v1/trash                               → TrashOverview
//	  POST   /api/v1/trash/settings                      TrashSettings → TrashOverview
//	  DELETE /api/v1/trash/{domain}[?dry_run=1]          → Preview | JobStarted   (xoá hẳn một site trong thùng rác)
//	  POST   /api/v1/apply[?dry_run=1]                   ApplyRequest → core.ApplyPreview | JobStarted
//	  GET    /api/v1/export                              → text/yaml (core.SitesFile.Marshal), tên file sites.yaml
//
// Lời từ chối của core (site_not_trashed, site_trashed, nothing_to_do...) là
// 409 kèm checks như mọi lời từ chối khác; sites_file_invalid, domain_invalid,
// email_invalid, title_invalid, trash_days_invalid là 400. POST /apply khi
// core.Apply.Empty() (mọi dòng đã có hoặc bị từ chối) trả 409 nothing_to_do,
// không tạo job. Mọi thao tác có thay đổi ghi vào audit: site.rm,
// site.restore, site.purge, trash.settings, apply.
//
// Job của POST /apply là job hàng loạt (engine.ExecuteBatch): sự kiện
// unit.done mang kết quả từng site. Job SSL của pha D chạy sau, mỗi site một
// job riêng; SSE của job hàng loạt KHÔNG đẩy sự kiện của chúng (chúng có
// trang job riêng ở Lịch sử). Mật khẩu quản trị của các site vừa tạo nằm ở
// JobDetail.Secrets của job hàng loạt, hiện một lần như Secret của site.add.
//
// Bộ hẹn giờ dọn thùng rác (mục 6.4): trong `tpan serve`, mỗi giờ một lần,
// gọi core.Sites.PurgeExpired khi không có job nào (giành apiState.claim như
// một job của API, không được thì bỏ vòng), giống health bỏ vòng khi đang có job.

import (
	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/site"
)

// TrashOverview là nội dung màn Thùng rác.
type TrashOverview struct {
	// Days là số ngày giữ đang đặt (core.Sites.TrashDays).
	Days  int         `json:"days"`
	Sites []site.Site `json:"sites"`
	// DBs (M13): DB rời đang ở thùng rác (Status trashed), lọc theo chủ.
	DBs []account.Database `json:"dbs"`
}

// TrashSettings là thân của POST /api/v1/trash/settings.
type TrashSettings struct {
	Days int `json:"days"`
}

// ApplyRequest là thân của POST /api/v1/apply: cùng trường với sites.yaml
// (core.SitesFile nhúng, JSON phẳng: defaults, sites) cộng prune. Màn Tạo site
// gửi danh sách đã dán ở đây.
type ApplyRequest struct {
	core.SitesFile
	Prune bool `json:"prune,omitempty"`
}
