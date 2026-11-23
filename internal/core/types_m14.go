package core

// Hợp đồng M14 (đợt 0): cô lập sâu giữa các site và định tuyến có bằng chứng
// (mục 6.13 của docs/KE-HOACH.md). Kiểu và chữ ký ở đây là thứ server, CLI và
// giao diện dựa vào; thân hàm nằm ở harden_m14.go và route_m14.go.

import (
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/htaccess"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
)

// Tên cách sửa tự động mới (AutoFixName, Fixes.plan).
const (
	// "php.opcache:<bản>" Fail và "php.protectproc:<bản>" Warn: ghi lại file
	// ini và drop-in của bản PHP đó (steps.PHPHardening), Verify là hai phép
	// kiểm của bản đó. Là ngoại lệ của luật "Warn không có sửa tự động".
	FixPHPHarden = "php.harden"
	// "log.perm:<domain>" Fail: đặt lại quyền thư mục và file log của site
	// (steps.SiteLogs), Verify là checks.LogPerms.
	FixLogPerms = "log.perm"
	// "web.hardened:<domain>" Fail dùng FixWebRewrite có sẵn: ghi lại vhost của
	// riêng site đó từ template hiện tại, theo chế độ .htaccess đang lưu của
	// site (Fixes.vhostRewrite); Verify thêm checks.VhostHardened.
)

// Mã từ chối mới.
const (
	// CodeHtaccessBlocked: không chuyển được site sang chế độ an-toan vì
	// .htaccess của nó có chỉ thị ngoài danh sách (Message liệt kê từng dòng).
	CodeHtaccessBlocked = "htaccess_blocked"
	// CodeHtaccessNotApache: đổi chế độ .htaccess khi stack không phải Apache.
	CodeHtaccessNotApache = "htaccess_not_apache"
	// CodeHtaccessSame: site đã ở đúng chế độ được yêu cầu.
	CodeHtaccessSame = "htaccess_same"
)

// ---------------------------------------------------------------- .htaccess

// PrepareHtaccess dựng Plan "site.htaccess": đổi chế độ .htaccess của site d
// sang mode (`tpan site set <domain> --htaccess ...`). Chỉ đọc máy.
//
// Điều kiện đầu: site có, active, không tạm khoá (CodeSiteSuspended); stack là
// Apache (CodeHtaccessNotApache); mode khác chế độ hiện tại (CodeHtaccessSame).
// Chuyển sang an-toan: checks.ScanHtaccess trước; có chỉ thị ngoài danh sách
// thì từ chối CodeHtaccessBlocked, Message in từng dòng "<file>:<dòng>:
// <chỉ thị>" và hai lối ra (sửa .htaccess, hoặc giữ day-du và nhận cảnh báo).
//
// Plan: ghi lại vhost của site theo mode (siteVhost với bản ghi Site đã đổi
// Htaccess; có kiểm cú pháp và reload). Sang "tat" không xoá .htaccess đang có
// (Apache thôi đọc nó). Rời "tat" mà <public>/.htaccess chưa có thì ghi bản
// chuẩn của WordPress (htaccessIfMissing). Verify: VhostHardened, phép kiểm cú
// pháp Apache, HtaccessDirectives (trừ khi nó chắc chắn Warn ở day-du), và
// HTTPSite (bản SSL khi site có SSL). Job thành công thì lưu Htaccess mới vào
// bảng sites. Ai được đặt day-du (chỉ admin) do server quyết, core không biết vai.
//
//	func (s *Sites) PrepareHtaccess(ctx context.Context, d validate.Domain, mode site.HtaccessMode) (*Planned, error)   // thân ở harden_m14.go

// ------------------------------------------------------------------- harden

// HtaccessKept là một site Apache mà Plan "Áp bản vá cô lập" giữ ở chế độ
// day-du vì .htaccess của nó có chỉ thị ngoài danh sách.
type HtaccessKept struct {
	Domain   string             `json:"domain"`
	Findings []htaccess.Finding `json:"findings"`
}

// Harden là Plan "Áp bản vá cô lập" đã dựng (mục 6.13, "Site đã có và nâng
// cấp từ bản cũ"), dùng cho đúng một lần Execute.
type Harden struct {
	*Planned
	// Switched là các site sẽ chuyển từ day-du sang an-toan trong job này.
	Switched []string `json:"switched"`
	// Kept là các site giữ day-du; CLI và giao diện in danh sách này trước khi
	// người dùng xác nhận, và sau job site đó vẫn có cảnh báo web.htaccess.
	Kept []HtaccessKept `json:"kept"`
	// Skipped (M28 đợt 2, S2 và S16): thứ Plan không kiểm hay không đổi, kèm
	// lời giải thích; CLI và giao diện in trước khi người dùng xác nhận. Không
	// bao giờ null trong JSON.
	Skipped []HardenSkipped `json:"skipped"`
}

// Kind của HardenSkipped.
const (
	// Site đang trả lời sai TRƯỚC Plan: vẫn được vá, nhưng phép HTTP và cô lập
	// của nó không nằm trong báo cáo sau job.
	HardenSkipSiteHTTP = "site_http"
	// User DB của site hay DB rời không có trong MariaDB: không có bước đặt trần.
	HardenSkipDBUser = "db_user"
)

// HardenSkipped là một mục Plan "Áp bản vá cô lập" bỏ qua.
type HardenSkipped struct {
	// Target là domain (site_http) hay tên user DB (db_user).
	Target  string    `json:"target"`
	Kind    string    `json:"kind"`
	Message i18n.Text `json:"message"`
	// Result là kết quả phép HTTP đo trước Plan (chỉ site_http).
	Result *checks.Result `json:"result,omitempty"`
}

// PrepareHarden dựng Plan "security.harden" (Target "isolation"). Chỉ đọc máy.
// Cần Security.Settings và Security.Sites. Stack chưa cài: từ chối như các ca
// sử dụng khác (Sites.stack).
//
// Các bước, theo thứ tự:
//  1. Với mỗi bản PHP đang cài (phpInstalled): steps.PHPHardening (file ini
//     OPcache, drop-in ProtectProc).
//  2. File logrotate của stack (logrotateStep): bản nginx đổi sang HUP.
//  3. Với mỗi site active: steps.SiteLogs (quyền log).
//  4. Với mỗi site active không tạm khoá: quyết định chế độ .htaccess. Site
//     đang day-du: checks.ScanHtaccess; sạch thì chuyển sang an-toan
//     (Switched), có chỉ thị ngoài danh sách thì giữ (Kept). Trên stack nginx
//     vẫn quét và chuyển (cột htaccess dùng khi máy chuyển sang Apache). Site
//     đang an-toan hay tat: giữ nguyên.
//  5. Với mỗi site active: ghi lại vhost (siteVhost với chế độ đã quyết; site
//     tạm khoá ghi bản tạm khoá như cũ). Reload gom lại một lần ở cuối như job
//     hàng loạt (steps.WithoutReload rồi một bước kiểm cú pháp và reload).
//  6. M28 đợt 2 (H5): stack Apache thêm steps.ApacheModOff{status} (tự
//     reload, đặt sau bước reload chung), bọc bằng hardenModOff: module nạp
//     ngoài mods-enabled (D11) thì bước không làm hoàn tác cả Plan.
//  7. M28 đợt 2 (M1-DBCONN): steps.DBUserLimit cho user DB của mọi site
//     active và mọi DB rời active (Security.DBs, hoặc Sites khi nó cũng là
//     DBStore). User không có thì không có bước, nêu trong Skipped.
//
// Verify: Stack.Checks (stackChecks cộng OPcacheHardened, PHPProtectProc cho
// mọi bản PHP, ServerNameUnique, DefaultRejects, và web.mod_status khi
// Apache; thành cảnh báo khi module nạp ngoài mods-enabled); với mỗi site:
// LogPerms, VhostHardened và DocrootGuard (site không tạm khoá); DBUserLimit
// (chỉ cảnh báo) cho mỗi user có bước. Báo cáo, không làm job lỗi (D2 của soát
// cuối đợt 2, checks.Soft): HTTPSite (hoặc HTTPSuspended) và cô lập hai chiều
// cho những site trả lời đúng TRƯỚC Plan (site hỏng sẵn nêu trong Skipped);
// thứ khách điều khiển được thì không được làm hoàn tác bản vá cả máy. Job thành công thì lưu
// Htaccess mới của các site trong Switched. Chạy lại khi mọi thứ đã đúng thì
// mọi bước được bỏ qua.
//
//	func (s *Security) PrepareHarden(ctx context.Context) (*Harden, error)   // thân ở harden_m14.go

// ------------------------------------------------------------ route explain

// Chặng của RouteTrace, theo đúng thứ tự in ra.
const (
	HopDNS         = "dns"          // tên miền trỏ về máy này không
	HopVhost       = "vhost"        // vhost nào nhận tên miền: file và dòng server_name / ServerName
	HopRoot        = "root"         // document root của vhost đó
	HopSocket      = "socket"       // socket mà vhost chuyển .php tới
	HopPool        = "pool"         // pool PHP-FPM đang nghe socket đó: file và tên pool
	HopUser        = "user"         // user Linux mà pool chạy
	HopOpenBasedir = "open_basedir" // thư mục PHP của pool được mở
	HopHTTP        = "http"         // gọi thật vào site trên chính máy
)

// RouteHop là một mắt xích của đường đi request.
type RouteHop struct {
	Key   string    `json:"key"`
	Title i18n.Text `json:"title"`
	// Value là điều tìm được ở chặng này, chữ mono trên giao diện: ví dụ
	// "/etc/nginx/sites-enabled/tpan-a.example.com.conf:8", "/var/www/a.example.com/public",
	// "tp_a_example_com_7653e4". Rỗng khi không tìm được.
	Value string `json:"value"`
	// Status: Pass khi chặng khớp với thứ tpan mong đợi (site.Layout), Fail
	// khi lệch (vhost lạ nhận tên miền, root trỏ chỗ khác, pool chạy user
	// khác), Warn cho DNS chưa trỏ về máy, Unknown khi không đọc được hoặc
	// chặng trước đã hỏng nên không có gì để lần tiếp.
	Status checks.Status `json:"status"`
	// Summary là một câu nói kết quả của chặng.
	Summary i18n.Text `json:"summary"`
	// Evidence là lệnh đã chạy và output thật (mục 10, ý 4).
	Evidence []checks.Evidence `json:"evidence"`
}

// RouteTrace là câu trả lời cho "request tới tên miền này đi đường nào".
type RouteTrace struct {
	Domain string     `json:"domain"`
	Web    WebServer  `json:"web"`
	Hops   []RouteHop `json:"hops"`
}

// RouteExplain lần đường đi của một request tới site d trên chính máy này,
// mỗi chặng kèm lệnh đã chạy và output (`tpan route explain <domain>`,
// GET /api/v1/sites/{domain}/route). Chỉ đọc; không đổi gì trên máy.
//
// Lần từ cấu hình ĐANG CHẠY chứ không từ điều tpan nghĩ nó đã ghi: vhost lấy
// từ `nginx -T` hoặc `apache2ctl -S` (vhost đầu tiên mang tên miền trên cổng
// 80, hoặc 443 khi site có SSL), root và socket đọc từ chính vhost đó, pool
// là file trong /etc/php/*/fpm/pool.d có dòng listen bằng socket đó, user và
// open_basedir đọc từ file pool. Mỗi chặng so với site.Layout để ra Status.
// Chặng dns dùng checks.DNSPointsHere, chặng http dùng checks.HTTPSite (bản
// SSL, hoặc HTTPSuspended, theo trạng thái site). Site tạm khoá: dừng sau
// chặng vhost với một câu nói site đang tạm khoá, rồi chặng http.
// Site không có hoặc ở thùng rác: lỗi như find. Stack chưa cài: từ chối.
//
//	func (s *Sites) RouteExplain(ctx context.Context, d validate.Domain) (RouteTrace, error)   // thân ở route_m14.go

// ------------------------------------------------------- ma trận cô lập

// IsolationCell là một ô của ma trận cô lập: kết quả gần nhất của phép kiểm
// "isolation:<from>:<to>". Result nil khi health chưa chạy phép đó.
type IsolationCell struct {
	From   string         `json:"from"`
	To     string         `json:"to"`
	Result *checks.Result `json:"result,omitempty"`
}

// HtaccessRow là chế độ .htaccess của một site kèm kết quả gần nhất của phép
// kiểm "web.htaccess:<domain>" (nil khi stack không phải Apache hoặc chưa chạy).
type HtaccessRow struct {
	Domain string            `json:"domain"`
	Mode   site.HtaccessMode `json:"mode"`
	Result *checks.Result    `json:"result,omitempty"`
}

// Isolation là khối "Cô lập giữa các site" của màn Bảo mật. Server dựng từ
// kết quả health đã lưu (bảng check_results), không chạy lại phép kiểm.
type Isolation struct {
	// Web là web server của stack; Apache thì giao diện hiện cột chế độ .htaccess.
	Web WebServer `json:"web"`
	// Sites là tên miền các site active, xếp theo tên: hàng và cột của ma trận.
	Sites []string `json:"sites"`
	// Cells có một ô cho mỗi cặp (from, to) khác nhau.
	Cells []IsolationCell `json:"cells"`
	// Machine là kết quả gần nhất của các phép kiểm cấp máy của M14:
	// php.opcache:*, php.protectproc:*, web.server_name.unique, web.default.rejects.
	Machine []checks.Result `json:"machine"`
	// PerSite là kết quả gần nhất của log.perm:* và web.hardened:* của từng site.
	PerSite []checks.Result `json:"per_site"`
	// Htaccess có một dòng cho mỗi site active.
	Htaccess []HtaccessRow `json:"htaccess"`
	// NeedsHarden true khi có phép kiểm nào ở trên không đạt mà Plan "Áp bản
	// vá cô lập" sửa được: giao diện hiện nút.
	NeedsHarden bool `json:"needs_harden"`
}
