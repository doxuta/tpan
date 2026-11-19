package core

// Ca sử dụng PHP theo từng site (mục 6.11 của docs/KE-HOACH.md). Hợp đồng M12
// đợt 0: chữ ký và tài liệu dưới đây cố định; thân hàm do thợ đợt 2 viết.
//
// Plan "php.install" (tpan php install <bản>):
//  1. Bản không có trong kho Ubuntu (apt-cache không thấy php<bản>-fpm khi chưa
//     có kho sury) thì: steps.AptRepo của kho sury (Danger, hiện rõ kho và khoá
//     sẽ thêm), ghim: cả kho 100, php-common và php<bản>* của mọi bản cài từ
//     kho này (kể cả bản đang cài) 500. Bản Ubuntu có thì không thêm kho.
//  2. AptInstall php<bản>-fpm và đúng bộ extension ở mục 3 (php<bản>-cli,
//     -mysql, -curl, -gd, -mbstring, -xml, -zip, -intl).
//  3. steps.PHPIdlePool cho bản đó (gỡ pool www của gói, thêm tpan-idle).
//  4. Service php<bản>-fpm.
//  Kiểm chứng: dịch vụ php<bản>-fpm active, `php-fpm<bản> -t`,
//  checks.PHPPoolWWWAbsent(bản). Từ chối php_version_invalid (không dạng 8.3),
//  php_version_unavailable (kho Ubuntu và kho sury đều không có).
//
// Plan "php.use" (tpan php use <domain> <bản>), đổi không gián đoạn (6.11 ý 3):
//  1. Ghi pool của site vào bản mới (steps.SitePool(l, mới): socket mang số bản
//     mới, file /etc/php/<mới>/fpm/pool.d/tpan-<slug>.conf), kiểm, reload bản mới.
//  2. Ghi lại vhost trỏ socket mới (siteVhost với site đã đổi PHPVersion),
//     kiểm config, reload web server.
//  3. Kiểm chứng giữa chừng: site trả 200 qua socket mới (một Step chạy
//     checks.HTTPSite hoặc HTTPSSite; Fail thì Step lỗi và các bước trước hoàn
//     tác: vhost về socket cũ, pool mới bị gỡ).
//  4. Cuối cùng mới gỡ pool ở bản cũ (Absent, reload bản cũ).
//  Kiểm chứng: siteChecks của site với bản mới. Thành công thì after ghi
//  site.PHPVersion mới vào bảng sites. Từ chối: site_not_found, site_not_active,
//  php_not_installed (bản mới chưa cài: chỉ sang `tpan php install`),
//  php_same_version (đã ở bản đó; không phải lỗi với CLI, in "không đổi gì").
//
// Plan "php.remove" (tpan php remove <bản>): chỉ khi không site nào dùng và
// không phải bản mặc định (php_in_use, php_default). AptRemove php<bản>-* đã
// cài (Danger, NoUndo).

import (
	"context"

	"github.com/doxuta/tpan/internal/validate"
)

// List trả danh sách bản PHP (đã cài và có trong kho). notes: hỏi php.net và
// WordPress.org (curl qua Sys, timeout ngắn, lỗi thì bỏ ghi chú, không lỗi).
func (p *PHP) List(ctx context.Context, notes bool) (PHPList, error) {
	return PHPList{}, errM12Todo
}

// PrepareInstall dựng Plan "php.install". Chỉ đọc máy.
func (p *PHP) PrepareInstall(ctx context.Context, version string) (*Planned, error) {
	return nil, errM12Todo
}

// PrepareUse dựng Plan "php.use". Chỉ đọc máy.
func (p *PHP) PrepareUse(ctx context.Context, d validate.Domain, version string) (*Planned, error) {
	return nil, errM12Todo
}

// PrepareRemove dựng Plan "php.remove". Chỉ đọc máy.
func (p *PHP) PrepareRemove(ctx context.Context, version string) (*Planned, error) {
	return nil, errM12Todo
}
