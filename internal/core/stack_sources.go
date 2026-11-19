package core

// Nguồn gói lúc cài stack và pool www thừa của PHP-FPM (mục 6.11, mục 16 dòng
// "Còn mở" ở M2). Hợp đồng M12 đợt 0; thân do thợ đợt 2 viết.
//
// PrepareInstallWith là PrepareInstall (stack.go) cộng lựa chọn nguồn:
//   - Packages(cfg) nhận thêm SourceNginxStable, SourceNginxMainline (chỉ với
//     web nginx) và SourceMariaDB; nguồn khác vẫn source_unsupported.
//   - Trước bước cài gói: steps.AptRepo của nginx.org (ghim 900 cho origin
//     nginx.org để gói của kho này thắng gói Ubuntu) và/hoặc của MariaDB (dòng
//     DBLine), mỗi cái Danger, xem trước hiện kho và vân tay khoá.
//   - nginx.org: sau cài gói, thích nghi bố cục: Dir sites-available,
//     sites-enabled; File /etc/nginx/conf.d/tpan.conf (templates.NginxOrgInclude);
//     Absent /etc/nginx/conf.d/default.conf; steps.UserInGroup{nginx, www-data}
//     rồi restart nginx (thành viên nhóm chỉ có hiệu lực với tiến trình mới).
//   - Mọi nguồn: sau DetectPHP thêm steps.PHPIdlePool{Version: php} (gỡ pool
//     www, thêm tpan-idle), và checks.PHPPoolWWWAbsent trong kiểm chứng.
//   - Chạy lại: InstallOptions phải trùng stack.*_source và stack.db_line đã ghi
//     (zero value = giữ như đã ghi); khác thì từ chối source_change_unsupported.
//   - stack.php_version đã có giá trị thì giữ nguyên (không lấy bản cao nhất
//     DetectPHP thấy: máy đã cài thêm 8.4 thì chạy lại stack install không được
//     đổi bản mặc định).
// PrepareInstall(ctx, web) giữ nguyên chữ ký, gọi PrepareInstallWith với zero
// InstallOptions.

import "context"

// PrepareInstallWith dựng Plan "stack.install" với nguồn gói đã chọn. Chỉ đọc máy.
func (s *Stack) PrepareInstallWith(ctx context.Context, web WebServer, opts InstallOptions) (*Install, error) {
	return nil, errM12Todo
}
