package server

// Bảng route có luật (M13, mục 6.12 của docs/KE-HOACH.md; bảng đầy đủ ở
// docs/KE-HOACH.md mục 6.12 ("Đã chốt khi làm M13"), mục "Luật của từng route").
//
// Mặc định là cấm: mọi đường dẫn của server đi qua đúng một hàm mount, và mount
// panic khi gặp route không khai luật, nên server không khởi động được với một
// route quên luật (test authz_test.go cũng đỏ). Luật được kiểm ở guard, một chỗ
// duy nhất, trước khi tới handler; handler không tự nhớ phải kiểm quyền.
//
// Thứ tự trong guard:
//  0. (M20) Request có header Authorization chỉ xác thực bằng token
//     (token_m20.go, guardToken): nhóm token của route, phạm vi, chính sách
//     hai bước, rồi tới bước 3 với vai hiện tại của chủ token.
//  1. Đọc phiên, dựng danh tính (account.Identity); không có thì 401.
//     (M20) Chính sách bắt buộc hai bước: user thật chưa bật thì 403
//     mfa_enroll_required, trừ route có allowEnroll.
//  2. Phiên "xem như" chỉ đọc: mọi request không phải GET, HEAD bị 403
//     read_only_view, trừ route có allowReadOnly (thoát "xem như").
//  3. Luật của route: any qua; admin thì user thường 403 forbidden; owner:X
//     thì admin qua, user thường phải là chủ của X, không thì 404 như X không
//     tồn tại (cùng mã cho "không có" và "của người khác", để không dò được).

import (
	"context"
	"io/fs"
	"net/http"
	"regexp"
	"strconv"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/apitoken"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/validate"
)

// ruleKind là loại luật của một route. Giá trị zero (ruleUnset) là "quên khai
// luật": mount panic.
type ruleKind int

const (
	ruleUnset ruleKind = iota
	rulePublic
	ruleAny
	ruleOwner
	ruleAdmin
)

// ownerObj là loại đối tượng của luật owner:X.
type ownerObj int

const (
	objSite     ownerObj = iota + 1 // {domain}
	objJob                          // {id}
	objIncident                     // {id}
	objBackup                       // {id}
	objDB                           // {name}: DB rời
)

// rule là luật của một route.
type rule struct {
	kind ruleKind
	obj  ownerObj // chỉ có nghĩa với ruleOwner
}

var (
	public        = rule{kind: rulePublic}
	anyone        = rule{kind: ruleAny}
	adminOnly     = rule{kind: ruleAdmin}
	ownerSite     = rule{kind: ruleOwner, obj: objSite}
	ownerJob      = rule{kind: ruleOwner, obj: objJob}
	ownerIncident = rule{kind: ruleOwner, obj: objIncident}
	ownerBackup   = rule{kind: ruleOwner, obj: objBackup}
	ownerDB       = rule{kind: ruleOwner, obj: objDB}
)

// route là một dòng của bảng route.
type route struct {
	pattern string
	rule    rule
	h       http.HandlerFunc
	// allowReadOnly: chạy được cả trong phiên "xem như" khi không phải GET.
	// Chỉ thoát "xem như" cần (đăng xuất là public, không qua guard).
	allowReadOnly bool
	// tok (M20) là nhóm token của route: request bằng token cần phạm vi đọc
	// (GET, HEAD) hoặc ghi nhóm này. Rỗng là chỉ phiên: quên khai là đóng.
	tok apitoken.Group
	// allowEnroll (M20): gọi được khi chính sách bắt bật hai bước mà chưa bật
	// (trang bật hai bước và lối thoát "xem như").
	allowEnroll bool
}

// routes là bảng route của server: thêm đường dẫn mới ở đây, kèm luật. Thứ tự
// không quan trọng (ServeMux chọn mẫu cụ thể nhất).
func (s *Server) routes(dist fs.FS) []route {
	return []route{
		// Công khai.
		{pattern: "GET /api/v1/health", rule: public, h: handleHealth},
		{pattern: "POST /api/v1/login", rule: public, h: s.handleLogin},
		{pattern: "POST /api/v1/logout", rule: public, h: s.handleLogout},
		{pattern: "GET /api/v1/session", rule: public, h: s.handleSession},
		{pattern: "POST /api/v1/login/2fa", rule: public, h: s.handleLogin2FA},

		// Tài khoản của chính mình (auth.go).
		{pattern: "POST /api/v1/account/password", rule: anyone, h: s.handlePasswordChange},
		{pattern: "GET /api/v1/account/sessions", rule: anyone, h: s.handleSessions},
		{pattern: "DELETE /api/v1/account/sessions/{id}", rule: anyone, h: s.handleSessionDelete},

		// M3: tổng quan, site, job, DB (api.go, api_job.go, sse.go).
		{pattern: "GET /api/v1/overview", rule: anyone, tok: apitoken.GroupHealth, h: s.handleOverview},
		{pattern: "GET /api/v1/machine", rule: adminOnly, tok: apitoken.GroupSystem, h: s.handleMachine},
		{pattern: "GET /api/v1/machine/ips", rule: adminOnly, tok: apitoken.GroupSystem, h: s.handleMachineIPs},
		{pattern: "GET /api/v1/stack", rule: adminOnly, tok: apitoken.GroupSystem, h: s.handleStack},
		{pattern: "GET /api/v1/checks", rule: anyone, tok: apitoken.GroupHealth, h: s.handleChecks},
		{pattern: "GET /api/v1/sites", rule: anyone, tok: apitoken.GroupSites, h: s.handleSites},
		{pattern: "GET /api/v1/site-check", rule: anyone, tok: apitoken.GroupSites, h: s.handleSiteCheck},
		{pattern: "POST /api/v1/sites", rule: anyone, tok: apitoken.GroupSites, h: s.handleSiteCreate},
		{pattern: "GET /api/v1/sites/{domain}", rule: ownerSite, tok: apitoken.GroupSites, h: s.handleSite},
		{pattern: "DELETE /api/v1/sites/{domain}", rule: ownerSite, tok: apitoken.GroupSites, h: s.handleSiteDelete},
		{pattern: "POST /api/v1/sites/{domain}/owner", rule: adminOnly, tok: apitoken.GroupSites, h: s.handleSiteOwner},
		{pattern: "GET /api/v1/jobs", rule: anyone, tok: apitoken.GroupJobs, h: s.handleJobs},
		{pattern: "GET /api/v1/jobs/{id}", rule: ownerJob, tok: apitoken.GroupJobs, h: s.handleJob},
		{pattern: "GET /api/v1/jobs/{id}/events", rule: ownerJob, tok: apitoken.GroupJobs, h: s.handleJobEvents},
		{pattern: "GET /api/v1/databases", rule: anyone, tok: apitoken.GroupDatabases, h: s.handleDatabases},

		// M5: SSL và trang bảo mật (types_m5.go).
		{pattern: "GET /api/v1/ssl", rule: anyone, tok: apitoken.GroupSSL, h: s.handleSSL},
		{pattern: "POST /api/v1/ssl/settings", rule: adminOnly, tok: apitoken.GroupSSL, h: s.handleSSLSettings},
		{pattern: "POST /api/v1/sites/{domain}/ssl", rule: ownerSite, tok: apitoken.GroupSites, h: s.handleSiteSSL},
		{pattern: "GET /api/v1/security", rule: adminOnly, tok: apitoken.GroupSystem, h: s.handleSecurity},
		{pattern: "POST /api/v1/security/ufw", rule: adminOnly, tok: apitoken.GroupSystem, h: s.handleSecurityUFW},
		{pattern: "POST /api/v1/security/fail2ban", rule: adminOnly, tok: apitoken.GroupSystem, h: s.handleSecurityFail2ban},

		// M4: health (api_health.go). Sửa tự động, nhận bản sửa tay, chạy lại
		// health chỉ admin: thân chứa id phép kiểm, không phải đường dẫn
		// (quyết định 12 ở docs/KE-HOACH.md mục 6.12 ("Đã chốt khi làm M13")).
		{pattern: "GET /api/v1/incidents", rule: anyone, tok: apitoken.GroupHealth, h: s.handleIncidents},
		{pattern: "GET /api/v1/incidents/{id}", rule: ownerIncident, tok: apitoken.GroupHealth, h: s.handleIncident},
		{pattern: "POST /api/v1/fixes", rule: adminOnly, tok: apitoken.GroupHealth, h: s.handleFix},
		{pattern: "POST /api/v1/files/accept", rule: adminOnly, tok: apitoken.GroupHealth, h: s.handleAcceptFile},
		{pattern: "POST /api/v1/health/run", rule: adminOnly, tok: apitoken.GroupHealth, h: s.handleHealthRun},
		{pattern: "GET /api/v1/sites/{domain}/traffic", rule: ownerSite, tok: apitoken.GroupSites, h: s.handleSiteTraffic},
		{pattern: "GET /api/v1/sites/{domain}/log", rule: ownerSite, tok: apitoken.GroupSites, h: s.handleSiteLog},

		// M7: Plan chờ duyệt và cổng agent: chỉ admin (quyết định 11).
		{pattern: "GET /api/v1/plans", rule: adminOnly, h: s.handlePlans},
		{pattern: "GET /api/v1/plans/{id}", rule: adminOnly, h: s.handlePlan},
		{pattern: "POST /api/v1/plans/{id}/approve", rule: adminOnly, h: s.handlePlanApprove},
		{pattern: "POST /api/v1/plans/{id}/reject", rule: adminOnly, h: s.handlePlanReject},
		{pattern: "GET /api/v1/agent", rule: adminOnly, h: s.handleAgent},

		// M6: thùng rác và hàng loạt (types_m6.go, api_trash.go).
		{pattern: "POST /api/v1/sites/{domain}/restore", rule: ownerSite, tok: apitoken.GroupSites, h: s.handleSiteRestore},
		{pattern: "GET /api/v1/trash", rule: anyone, tok: apitoken.GroupSites, h: s.handleTrash},
		{pattern: "POST /api/v1/trash/settings", rule: adminOnly, tok: apitoken.GroupSystem, h: s.handleTrashSettings},
		{pattern: "DELETE /api/v1/trash/{domain}", rule: ownerSite, tok: apitoken.GroupSites, h: s.handleTrashPurge},
		{pattern: "POST /api/v1/apply", rule: anyone, tok: apitoken.GroupSites, h: s.handleApply},
		{pattern: "GET /api/v1/export", rule: anyone, tok: apitoken.GroupSites, h: s.handleExport},

		// M8: AI trong panel: chỉ admin.
		{pattern: "GET /api/v1/ai/brain", rule: adminOnly, h: s.handleAIBrain},
		{pattern: "POST /api/v1/ai/preview", rule: adminOnly, h: s.handleAIPreview},
		{pattern: "POST /api/v1/ai/ask", rule: adminOnly, h: s.handleAIAsk},
		{pattern: "GET /api/v1/ai/sessions", rule: adminOnly, h: s.handleAISessions},
		{pattern: "GET /api/v1/ai/sessions/{id}", rule: adminOnly, h: s.handleAISession},
		{pattern: "GET /api/v1/ai/sessions/{id}/turns/{seq}/events", rule: adminOnly, h: s.handleAITurnEvents},
		{pattern: "POST /api/v1/ai/views", rule: adminOnly, h: s.handleAIView},

		// M12: phiên bản và cập nhật (types_m12.go, api_m12.go).
		{pattern: "GET /api/v1/php", rule: anyone, tok: apitoken.GroupSites, h: s.handlePHP},
		{pattern: "POST /api/v1/php/install", rule: adminOnly, tok: apitoken.GroupSystem, h: s.handlePHPInstall},
		{pattern: "POST /api/v1/php/remove", rule: adminOnly, tok: apitoken.GroupSystem, h: s.handlePHPRemove},
		{pattern: "POST /api/v1/sites/{domain}/php", rule: ownerSite, tok: apitoken.GroupSites, h: s.handleSitePHP},
		{pattern: "GET /api/v1/updates", rule: adminOnly, tok: apitoken.GroupSystem, h: s.handleUpdates},
		{pattern: "POST /api/v1/updates", rule: adminOnly, tok: apitoken.GroupSystem, h: s.handleUpdateApply},
		{pattern: "POST /api/v1/updates/auto", rule: adminOnly, tok: apitoken.GroupSystem, h: s.handleUpdateAuto},
		{pattern: "POST /api/v1/mariadb/upgrade", rule: adminOnly, tok: apitoken.GroupSystem, h: s.handleMariaDBUpgrade},

		// M10: chuyển LEMP ⇄ LAMP.
		{pattern: "POST /api/v1/stack/switch", rule: adminOnly, tok: apitoken.GroupSystem, h: s.handleStackSwitch},

		// M9: backup, cron, thao tác DB của site (types_m9.go, api_m9.go).
		{pattern: "GET /api/v1/backups", rule: anyone, tok: apitoken.GroupBackups, h: s.handleBackups},
		{pattern: "POST /api/v1/sites/{domain}/backups", rule: ownerSite, tok: apitoken.GroupBackups, h: s.handleSiteBackup},
		{pattern: "POST /api/v1/backups/{id}/restore", rule: ownerBackup, tok: apitoken.GroupBackups, h: s.handleBackupRestore},
		{pattern: "POST /api/v1/backups/settings", rule: adminOnly, tok: apitoken.GroupBackups, h: s.handleBackupSettings},
		{pattern: "GET /api/v1/cron", rule: anyone, tok: apitoken.GroupCron, h: s.handleCron},
		{pattern: "POST /api/v1/databases/{domain}/password", rule: ownerSite, tok: apitoken.GroupDatabases, h: s.handleDBPassword},
		{pattern: "POST /api/v1/databases/{domain}/dump", rule: ownerSite, tok: apitoken.GroupDatabases, h: s.handleDBDump},
		{pattern: "POST /api/v1/databases/{domain}/import", rule: ownerSite, tok: apitoken.GroupDatabases, h: s.handleDBImport},

		// M13: user, gói, tạm khoá, DB rời, xem như (types_m13.go, api_m13.go).
		{pattern: "GET /api/v1/users", rule: adminOnly, h: s.handleUsers},
		{pattern: "POST /api/v1/users", rule: adminOnly, h: s.handleUserCreate},
		{pattern: "GET /api/v1/users/{name}", rule: adminOnly, h: s.handleUser},
		{pattern: "POST /api/v1/users/{name}", rule: adminOnly, h: s.handleUserEdit},
		{pattern: "DELETE /api/v1/users/{name}", rule: adminOnly, h: s.handleUserDelete},
		{pattern: "POST /api/v1/users/{name}/password", rule: adminOnly, h: s.handleUserPassword},
		{pattern: "POST /api/v1/users/{name}/suspend", rule: adminOnly, h: s.handleUserSuspend},
		{pattern: "POST /api/v1/users/{name}/unsuspend", rule: adminOnly, h: s.handleUserUnsuspend},
		{pattern: "POST /api/v1/users/{name}/trash-all", rule: adminOnly, h: s.handleUserTrashAll},
		{pattern: "POST /api/v1/users/{name}/view-as", rule: adminOnly, h: s.handleViewAs},
		{pattern: "POST /api/v1/session/view-as/exit", rule: anyone, h: s.handleViewAsExit, allowReadOnly: true, allowEnroll: true},
		{pattern: "GET /api/v1/packages", rule: adminOnly, h: s.handlePackages},
		{pattern: "POST /api/v1/packages", rule: adminOnly, h: s.handlePackageCreate},
		{pattern: "POST /api/v1/packages/{name}", rule: adminOnly, h: s.handlePackageEdit},
		{pattern: "DELETE /api/v1/packages/{name}", rule: adminOnly, h: s.handlePackageDelete},
		{pattern: "POST /api/v1/sites/{domain}/suspend", rule: adminOnly, tok: apitoken.GroupSites, h: s.handleSiteSuspend},
		{pattern: "POST /api/v1/sites/{domain}/unsuspend", rule: adminOnly, tok: apitoken.GroupSites, h: s.handleSiteUnsuspend},
		{pattern: "POST /api/v1/databases/{domain}/suspend", rule: adminOnly, tok: apitoken.GroupDatabases, h: s.handleSiteDBSuspend},
		{pattern: "POST /api/v1/databases/{domain}/unsuspend", rule: adminOnly, tok: apitoken.GroupDatabases, h: s.handleSiteDBUnsuspend},
		{pattern: "POST /api/v1/databases/{domain}/charset", rule: ownerSite, tok: apitoken.GroupDatabases, h: s.handleSiteDBCharset},
		{pattern: "GET /api/v1/collations", rule: anyone, tok: apitoken.GroupDatabases, h: s.handleCollations},
		{pattern: "POST /api/v1/dbs", rule: anyone, tok: apitoken.GroupDatabases, h: s.handleDBCreate},
		{pattern: "POST /api/v1/dbs/{name}", rule: ownerDB, tok: apitoken.GroupDatabases, h: s.handleDBEdit},
		{pattern: "POST /api/v1/dbs/{name}/password", rule: ownerDB, tok: apitoken.GroupDatabases, h: s.handleStandaloneDBPassword},
		{pattern: "POST /api/v1/dbs/{name}/suspend", rule: adminOnly, tok: apitoken.GroupDatabases, h: s.handleDBSuspend},
		{pattern: "POST /api/v1/dbs/{name}/unsuspend", rule: adminOnly, tok: apitoken.GroupDatabases, h: s.handleDBUnsuspend},
		{pattern: "DELETE /api/v1/dbs/{name}", rule: ownerDB, tok: apitoken.GroupDatabases, h: s.handleDBTrash},
		{pattern: "POST /api/v1/dbs/{name}/restore", rule: ownerDB, tok: apitoken.GroupDatabases, h: s.handleDBRestore},
		{pattern: "DELETE /api/v1/trash/dbs/{name}", rule: ownerDB, tok: apitoken.GroupDatabases, h: s.handleDBPurge},

		// M14: cô lập sâu và định tuyến (types_m14.go, api_m14.go). /htaccess
		// là owner:site, riêng mode "day-du" handler đòi admin.
		{pattern: "GET /api/v1/sites/{domain}/route", rule: ownerSite, tok: apitoken.GroupSites, h: s.handleSiteRoute},
		{pattern: "POST /api/v1/sites/{domain}/htaccess", rule: ownerSite, tok: apitoken.GroupSites, h: s.handleSiteHtaccess},
		{pattern: "GET /api/v1/security/isolation", rule: adminOnly, tok: apitoken.GroupSystem, h: s.handleIsolation},
		{pattern: "POST /api/v1/security/harden", rule: adminOnly, tok: apitoken.GroupSystem, h: s.handleSecurityHarden},

		// M17: lồng theo site, số liệu, thử tấn công (types_m17.go, api_m17.go).
		// Mọi thao tác đổi lồng hay trần là admin; user thường chỉ xem số liệu
		// của site mình (bất biến 7 của docs/thay-doi/M17-thiet-ke.md).
		{pattern: "GET /api/v1/cage", rule: adminOnly, tok: apitoken.GroupSystem, h: s.handleCage},
		{pattern: "POST /api/v1/sites/{domain}/cage", rule: adminOnly, tok: apitoken.GroupSystem, h: s.handleSiteCage},
		{pattern: "GET /api/v1/usage", rule: anyone, tok: apitoken.GroupHealth, h: s.handleUsage},
		{pattern: "GET /api/v1/sites/{domain}/usage", rule: ownerSite, tok: apitoken.GroupHealth, h: s.handleSiteUsage},
		{pattern: "GET /api/v1/usage/suggestions", rule: adminOnly, tok: apitoken.GroupHealth, h: s.handleUsageSuggestions},
		{pattern: "GET /api/v1/security/probe", rule: adminOnly, tok: apitoken.GroupSystem, h: s.handleProbe},
		{pattern: "POST /api/v1/security/probe", rule: adminOnly, tok: apitoken.GroupSystem, h: s.handleProbeRun},
		{pattern: "POST /api/v1/security/probe/cleanup", rule: adminOnly, tok: apitoken.GroupSystem, h: s.handleProbeCleanup},
		// M15: nút cập nhật TPAN trong panel (types_m15.go, api_m15.go). Chỉ
		// admin: lượt chạy thay file chạy của panel bằng quyền root.
		{pattern: "GET /api/v1/self", rule: adminOnly, tok: apitoken.GroupSystem, h: s.handleSelf},
		{pattern: "POST /api/v1/self/update", rule: adminOnly, tok: apitoken.GroupSystem, h: s.handleSelfUpdate},
		{pattern: "POST /api/v1/self/rollback", rule: adminOnly, tok: apitoken.GroupSystem, h: s.handleSelfRollback},

		// M18: kho S3 và cloud-init (types_m18.go, api_m18.go). Mọi route là
		// admin (B5): đích quyết định bản sao lưu của MỌI site đi đâu, job đẩy,
		// tải mang endpoint và câu lỗi của kho, user-data mang cấu hình của cả
		// máy mới.
		//
		// Soát cuối M20 (F2): đích sao lưu và cloud-init là chỉ phiên (nhóm
		// token rỗng). Token backups:write đổi được đích là chuyển bản sao lưu
		// (không mã hoá) của mọi site sang kho của người khác; user-data mang
		// cấu hình máy. Đẩy, tải, xem kho vẫn theo nhóm backups.
		{pattern: "GET /api/v1/backups/target", rule: adminOnly, h: s.handleBackupTarget},
		{pattern: "POST /api/v1/backups/target", rule: adminOnly, h: s.handleBackupTargetSet},
		{pattern: "DELETE /api/v1/backups/target", rule: adminOnly, h: s.handleBackupTargetRemove},
		{pattern: "POST /api/v1/backups/target/test", rule: adminOnly, h: s.handleBackupTargetTest},
		{pattern: "POST /api/v1/backups/{id}/push", rule: adminOnly, tok: apitoken.GroupBackups, h: s.handleBackupPush},
		{pattern: "POST /api/v1/backups/{id}/pull", rule: adminOnly, tok: apitoken.GroupBackups, h: s.handleBackupPull},
		{pattern: "GET /api/v1/backups/remote", rule: adminOnly, tok: apitoken.GroupBackups, h: s.handleBackupRemote},
		{pattern: "POST /api/v1/cloud-init/render", rule: adminOnly, h: s.handleCloudInitRender},

		// M19: tên phụ của site và màn "Tên miền" (types_m19.go, api_m19.go).
		// Thao tác trên tên là của chủ site; /domains tự lọc theo phạm vi.
		{pattern: "GET /api/v1/sites/{domain}/names", rule: ownerSite, tok: apitoken.GroupSites, h: s.handleSiteNames},
		{pattern: "POST /api/v1/sites/{domain}/names", rule: ownerSite, tok: apitoken.GroupSites, h: s.handleAliasAdd},
		{pattern: "POST /api/v1/sites/{domain}/names/{name}/mode", rule: ownerSite, tok: apitoken.GroupSites, h: s.handleAliasMode},
		{pattern: "DELETE /api/v1/sites/{domain}/names/{name}", rule: ownerSite, tok: apitoken.GroupSites, h: s.handleAliasRemove},
		{pattern: "GET /api/v1/sites/{domain}/redirects", rule: ownerSite, tok: apitoken.GroupSites, h: s.handleRedirectList},
		{pattern: "POST /api/v1/sites/{domain}/redirects", rule: ownerSite, tok: apitoken.GroupSites, h: s.handleRedirectAdd},
		{pattern: "DELETE /api/v1/sites/{domain}/redirects/{id}", rule: ownerSite, tok: apitoken.GroupSites, h: s.handleRedirectRemove},
		{pattern: "GET /api/v1/domains", rule: anyone, tok: apitoken.GroupSites, h: s.handleDomains},
		// M20: hai bước, token API, chính sách, tên miền của panel (types_m20.go,
		// auth_m20.go, api_m20.go, token_m20.go). Mọi route quản lý hai bước,
		// token, chính sách là chỉ phiên (B5).
		{pattern: "GET /api/v1/account/2fa", rule: anyone, h: s.handleMFAState, allowEnroll: true},
		{pattern: "POST /api/v1/account/2fa/setup", rule: anyone, h: s.handleMFASetup, allowEnroll: true},
		{pattern: "POST /api/v1/account/2fa/enable", rule: anyone, h: s.handleMFAEnable, allowEnroll: true},
		{pattern: "POST /api/v1/account/2fa/disable", rule: anyone, h: s.handleMFADisable},
		{pattern: "POST /api/v1/account/2fa/recovery", rule: anyone, h: s.handleMFARecovery},
		{pattern: "GET /api/v1/account/tokens", rule: anyone, h: s.handleMyTokens},
		{pattern: "POST /api/v1/account/tokens", rule: anyone, h: s.handleTokenCreate},
		{pattern: "DELETE /api/v1/account/tokens/{id}", rule: anyone, h: s.handleMyTokenRevoke},
		{pattern: "GET /api/v1/settings/auth", rule: adminOnly, h: s.handleAuthSettings},
		{pattern: "POST /api/v1/settings/auth", rule: adminOnly, h: s.handleAuthSettingsSet},
		{pattern: "GET /api/v1/tokens", rule: adminOnly, h: s.handleTokens},
		{pattern: "DELETE /api/v1/tokens/{id}", rule: adminOnly, h: s.handleTokenRevoke},
		{pattern: "GET /api/v1/panel", rule: adminOnly, tok: apitoken.GroupSystem, h: s.handlePanel},
		{pattern: "POST /api/v1/panel/domain", rule: adminOnly, tok: apitoken.GroupSystem, h: s.handlePanelDomain},

		// Còn lại: đường dẫn API lạ là 404 JSON; mọi thứ khác là giao diện web
		// (đã nhúng). Cũng nằm trong bảng để test duyệt đủ mọi mẫu của mux.
		{pattern: "/api/", rule: public, h: handleAPINotFound},
		{pattern: "/", rule: public, h: spa(dist).ServeHTTP},
	}
}

// mount đăng ký mọi route vào mux, mỗi route bọc guard theo luật của nó.
// Route không khai luật thì panic: server không khởi động được (mục 6.12).
func (s *Server) mount(mux *http.ServeMux, rs []route) {
	for _, rt := range rs {
		mux.Handle(rt.pattern, s.guard(rt))
	}
}

// guard bọc handler của rt bằng luật của nó (thứ tự ở đầu file).
func (s *Server) guard(rt route) http.Handler {
	switch rt.rule.kind {
	case rulePublic:
		return rt.h
	case ruleAny, ruleAdmin:
	case ruleOwner:
		if rt.rule.obj < objSite || rt.rule.obj > objDB {
			panic("route " + rt.pattern + ": luật owner không có đối tượng")
		}
	default:
		panic("route " + rt.pattern + " không khai luật (public, any, owner:X, admin); mặc định là cấm")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.refreshShared(r.Context())
		if hasAuthorization(r) {
			s.guardToken(w, r, rt)
			return
		}
		ss, id, ok, err := s.authResolve(r)
		if err != nil {
			authInternal(w, "đọc phiên", err)
			return
		}
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", i18n.T(
				"Chưa đăng nhập hoặc phiên đã hết hạn.", "Not logged in, or the session has expired."))
			return
		}
		if !rt.allowEnroll {
			// User THẬT của phiên (đang "xem như" thì là admin): chính sách áp
			// cho người cầm phiên, không cho người được xem.
			if req, err := s.enrollRequired(r.Context(), ss.UserName, realRole(id)); err != nil {
				authInternal(w, "đọc chính sách hai bước", err)
				return
			} else if req {
				writeEnrollRequired(w)
				return
			}
		}
		if id.ReadOnly() && r.Method != http.MethodGet && r.Method != http.MethodHead && !rt.allowReadOnly {
			writeError(w, http.StatusForbidden, "read_only_view", i18n.T(
				"Đang xem như "+id.Name+": phiên này chỉ đọc. Thoát chế độ xem như rồi mới đổi được.",
				"Viewing as "+id.Name+": this session is read-only. Leave view-as mode to make changes."))
			return
		}
		// Người thật đã thao tác (admin khi đang "xem như"): job và nhật ký ghi tên này.
		actor := id.Name
		if id.ViewedBy != "" {
			actor = id.ViewedBy
		}
		ctx := context.WithValue(r.Context(), authCtxKey{}, ss)
		ctx = context.WithValue(ctx, identityKey{}, id)
		s.serveRule(w, r.WithContext(engine.WithActor(ctx, actor)), rt, id)
	})
}

// serveRule áp luật vai của route cho danh tính id (phiên hay chủ token) rồi
// gọi handler.
func (s *Server) serveRule(w http.ResponseWriter, r *http.Request, rt route, id account.Identity) {
	switch rt.rule.kind {
	case ruleAdmin:
		if !id.IsAdmin() {
			writeError(w, http.StatusForbidden, "forbidden", i18n.T(
				"Chỉ quản trị viên được làm việc này.", "Only an administrator can do this."))
			return
		}
	case ruleOwner:
		if !id.IsAdmin() && !s.ownsPath(w, r, rt.rule.obj, id.Name) {
			return
		}
	}
	rt.h(w, r)
}

// identityKey là khoá context giữ account.Identity của request đã qua guard.
type identityKey struct{}

// who trả danh tính mà guard đã gắn. Handler của route không public luôn có
// nó; thiếu là handler bị gọi ngoài guard (lỗi lập trình), nên panic chứ không
// đoán: đoán "admin" là mở cửa, đoán "user rỗng" thì bộ lọc rỗng ra mọi thứ.
func who(r *http.Request) account.Identity {
	id, ok := r.Context().Value(identityKey{}).(account.Identity)
	if !ok {
		panic("server: handler cần danh tính bị gọi ngoài guard")
	}
	return id
}

// scopeOf là chủ dùng để lọc danh sách: "" (mọi thứ) cho admin, tên của mình
// cho user thường. Mọi bộ lọc của store hiểu "" là "của mọi người", nên tên
// rỗng của một user thường là lỗ lớn: chặn ở đây.
func scopeOf(r *http.Request) string {
	id := who(r)
	if id.IsAdmin() {
		return ""
	}
	if id.Name == "" {
		panic("server: user thường không có tên")
	}
	return id.Name
}

// ---------------------------------------------------------------- owner:X

// dbNamePathRe là dạng tên DB rời (như core): tên lạ không đi tới store.
var dbNamePathRe = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)

// ownsPath kiểm user name có là chủ của đối tượng obj ở đường dẫn không; không
// thì đã trả 404 như đối tượng không tồn tại và trả false. "Không có" và "của
// người khác" ra đúng cùng một câu trả lời.
func (s *Server) ownsPath(w http.ResponseWriter, r *http.Request, obj ownerObj, name string) bool {
	ctx := r.Context()
	st := s.deps.Store
	var (
		owner string
		found bool
		err   error
		gone  error // lời 404 của đối tượng này
	)
	switch obj {
	case objSite:
		d, perr := validate.ParseDomain(r.PathValue("domain"))
		if perr != nil {
			// Domain sai dạng: 400 như handler, không phụ thuộc dữ liệu nên không lộ gì.
			apiFail(w, r, perr)
			return false
		}
		gone = siteNotFound(d)
		owner, found, err = st.SiteOwner(ctx, d.String())
	case objJob:
		gone = errJobNotFound
		owner, found, err = st.JobOwner(ctx, r.PathValue("id"))
	case objIncident:
		gone = errIncidentNotFound
		if id, perr := strconv.ParseInt(r.PathValue("id"), 10, 64); perr == nil && id > 0 {
			owner, found, err = st.IncidentOwner(ctx, id)
		}
	case objBackup:
		id := r.PathValue("id")
		if !backupIDRe.MatchString(id) {
			id = "?"
		}
		gone = backupNotFound(id)
		if id != "?" {
			owner, found, err = st.BackupOwner(ctx, id)
		}
	case objDB:
		n := r.PathValue("name")
		if !dbNamePathRe.MatchString(n) {
			n = "?"
		}
		gone = dbNotFound(n)
		if n != "?" {
			owner, found, err = st.DBOwner(ctx, n)
		}
	}
	if err != nil {
		apiFail(w, r, err)
		return false
	}
	// owner "" (sự cố của máy, stack, tpan) chỉ admin xem: name không bao giờ rỗng.
	if !found || owner != name {
		apiFail(w, r, gone)
		return false
	}
	return true
}

// Lời 404 của guard. Không in lại giá trị lấy từ đường dẫn khi chưa kiểm dạng.
var (
	errJobNotFound = &core.RefusedError{Code: "job_not_found", Message: i18n.T(
		"Không có job này.", "No such job.")}
	errIncidentNotFound = &core.RefusedError{Code: "incident_not_found", Message: i18n.T(
		"Không có sự cố này.", "No such incident.")}
)

// siteNotFound là lời "không có site" của guard, cùng câu với core (Sites.find).
func siteNotFound(d validate.Domain) error {
	return &core.RefusedError{Code: "site_not_found", Message: i18n.T(
		"không có site "+d.String()+" trong tpan; xem danh sách bằng: sudo tpan site list",
		"tpan has no site "+d.String()+"; list sites with: sudo tpan site list")}
}

// dbNotFound là lời "không có DB rời", cùng câu với core (dbsNotFound).
func dbNotFound(name string) error {
	return &core.RefusedError{Code: core.CodeDBNotFound, Message: i18n.T(
		"không có DB rời "+name+" trong tpan; xem danh sách bằng: sudo tpan db list",
		"tpan has no standalone database "+name+"; list them with: sudo tpan db list")}
}
