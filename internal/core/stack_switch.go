package core

// Chuyển LEMP ⇄ LAMP tại chỗ (mục 6.6 của docs/KE-HOACH.md). Hợp đồng M10 đợt 0;
// thân của Prepare do thợ core đợt 2 viết.
//
// Prepare(ctx, to) chỉ đọc máy và DB. Thứ tự:
//
// Điều kiện đầu (từ chối bằng *RefusedError, không Plan nào được dựng):
//   - to qua ParseWeb (web_invalid).
//   - Stack đã cài (stack.php_version có giá trị), không thì stack_not_installed.
//   - to khác stack.web, không thì stack_same_web.
//   - Mọi site active đang trả lời đúng trên web server CŨ: checks.HTTPSite,
//     hoặc checks.HTTPSSite khi site.SSL. Site nào Fail thì từ chối
//     precondition_failed, Checks là các kết quả Fail (một site hỏng sẵn thì
//     bước kiểm giữa chừng chắc chắn lỗi và cả job hoàn tác; nói trước rẻ hơn).
//
// Nguồn của web server mới: Apache luôn là SourceUbuntu. Nginx: đọc từ máy,
// không từ settings (đổi sang apache đã ghi ubuntu, mà gói nginx của nginx.org
// và kho của nó, ghim 900, vẫn nằm trên máy): kho nginx.org (repoNginx của
// repos.go, stable hay mainline) đã có trên máy (Check của AptRepo đạt) thì
// nguồn là kho đó, không thì SourceUbuntu.
//
// Plan "stack.switch", Target là to, Title "Chuyển LEMP (Nginx) sang LAMP
// (Apache)" (stackName). Các bước, đúng thứ tự:
//
//  1. steps.AptUpdate và steps.AptInstallNoStart{Packages: [webUnit(to)],
//     Disable: [webUnit(to)]} (mục 6.6 bước 1, mục 16).
//  2. Cấu hình của web server mới, KHÔNG reload, KHÔNG restart, KHÔNG start
//     (web server mới đang tắt; web server cũ vẫn giữ cổng). Dựng từ danh sách
//     cho phép tường minh, không lọc từ installPlan (installPlan có Service
//     enable --now, ApacheModules restart, groupRestart). Tách phần web của
//     installPlan thành một hàm dùng chung nếu cần, để hai nơi không lệch nhau.
//     Mọi *steps.File, *Symlink, *Absent qua steps.WithoutReload (giữ Test:
//     `nginx -t`, `apachectl configtest` chạy được khi dịch vụ tắt); mọi
//     *steps.ApacheModules đặt NoRestart. Thứ tự trong bước này:
//     a. nginx.org: Dir sites-available, sites-enabled; File conf.d/tpan.conf;
//        Absent conf.d/default.conf; steps.UserInGroup{nginx, www-data} (KHÔNG
//        groupRestart: nhóm có hiệu lực khi nginx khởi động ở WebSwap).
//        Apache: ApacheModules{Enable: apacheModules, Disable: mpm_prefork}.
//     b. Gỡ site mặc định của distro, ghi site mặc định cổng 80 của tpan
//        (file + symlink), như installPlan.
//     c. Có site nào trong bảng sites (MỌI trạng thái, kể cả thùng rác: khôi
//        phục một site SSL về sau cần mod ssl và site mặc định 443) có SSL thì
//        steps.DefaultSSL(to) (M5), trước vhost của các site.
//     d. Mỗi site active: siteVhost(st, cfgMới) (fix.go: bản 443 khi site.SSL,
//        socket theo bản PHP của từng site qua sitePHP). Apache: thêm
//        steps.SiteHtaccess khi <public>/.htaccess CHƯA có (đọc lúc Prepare);
//        có rồi thì để nguyên (WordPress tự ghi lại đoạn của nó). Site dựng
//        trên nginx không có .htaccess: thiếu nó thì trang chủ vẫn 200 nhưng
//        mọi permalink /%postname%/ là 404 trên Apache.
//     e. File /etc/logrotate.d/tpan-sites theo web server mới
//        (templates.LogrotateSites), Test `logrotate -d`.
//  3. steps.WebSwap{From: webUnit(cũ), To: webUnit(to)} (mục 6.6 bước 3).
//  4. midCheck (php.go) cho mỗi site active: HTTPSite hoặc HTTPSSite, thử lại
//     như php.use (mục 6.6 bước 4). Không đạt thì job dừng, engine hoàn tác
//     ngược: web server cũ chạy lại với đúng file cũ (mục 6.6 bước 5).
//  5. steps.Service{Unit: webUnit(to)} (enable --now; Undo của nó disable lại),
//     rồi steps.ServiceDisable{Unit: webUnit(cũ)} (mục 6.6 bước 5).
//  6. Gỡ file tpan của web server CŨ (Absent, không Test, không Reload: dịch vụ
//     cũ đã dừng): link và file vhost của mọi site trong bảng sites (mọi trạng
//     thái), site mặc định 80 và 443 (file và link), và với nginx.org
//     conf.d/tpan.conf. Absent khai FileChange Delete nên engine bỏ chúng khỏi
//     managed_files: health không còn kiểm lệch file của web server không chạy,
//     và lần chuyển ngược lại ghi lại chúng từ template (được ghi nhận lại).
//
// Verify: Service(webUnit(to)), NginxConfig/ApacheConfig, HTTPDefault(to), và
// mỗi site active VhostEnabled(to, l) cùng HTTPSite/HTTPSSite. Không chạy cả bộ
// siteChecks: một site hỏng ở chặng không liên quan tới web server (DB, mã
// nguồn) không được kéo hoàn tác một lần chuyển đúng; health đầy đủ chạy ngay
// sau job (CLI và API) và mở sự cố cho nó.
//
// Sau khi job thành công (after của Planned):
//   - Ghi stack.web = to và stack.web_source = nguồn mới (ubuntu với apache).
//     Không ghi được thì báo lỗi nói rõ máy đang chạy web server nào và lệnh
//     chạy lại (`sudo tpan stack switch --to <to>`: các bước đã đúng được bỏ
//     qua, chỉ còn ghi settings). Vì vậy Prepare không được từ chối khi máy đã
//     chạy web server mới mà settings còn ghi cũ.
//   - Ghi sha256 của MỌI *steps.File của web server mới ở bước 2 vào
//     managed_files (Journal.RecordFile), kể cả bước được bỏ qua vì file đã
//     đúng: engine chỉ ghi file của bước đã Apply, nên một file đúng sẵn mà
//     không có dòng sẽ không bao giờ được kiểm lệch. Đây là "phép kiểm lệch
//     nhận các file mới làm chuẩn".
//
// Mã từ chối mới: stack_not_installed, stack_same_web (cộng web_invalid,
// precondition_failed đã có). Câu từ chối stack_switch_required của
// stack_sources.go bỏ đuôi "(chưa có ở bản này)".

import (
	"context"
	"errors"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/system"
)

// Switch gom những gì Plan "stack.switch" cần. Dựng mới cho mỗi lệnh.
type Switch struct {
	Sys      system.System
	Settings Settings
	Sites    SiteStore
	// Journal nhận job khi Execute và các dòng managed_files sau khi xong;
	// nil chỉ hợp lệ khi xem trước.
	Journal engine.Journal
}

// Prepare dựng Plan "stack.switch" chuyển web server sang to (chú thích đầu
// file). Chỉ đọc máy. Bị từ chối thì trả *RefusedError.
func (s *Switch) Prepare(ctx context.Context, to WebServer) (*Planned, error) {
	if _, err := ParseWeb(string(to)); err != nil {
		return nil, err
	}
	return nil, errors.New("stack switch: chưa viết (M10 đợt 2)")
}
