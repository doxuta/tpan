# M14: cô lập sâu giữa các site và định tuyến có bằng chứng

Nghiên cứu ngày 20/11/2026, trước khi viết mã. Nguồn gốc: hai yêu cầu mở rộng sau buổi training 20/11:

1. Vì sao nhiều website trên cùng một panel mà request không đi nhầm thư mục mã nguồn.
2. Cơ chế chống local attack (một site bị chiếm rồi lấn sang site khác trên cùng máy).

File này là đầu vào của mốc M14. Phiên M14 chép phần đặc tả (mục 2) vào `docs/KE-HOACH.md` thành mục 6.13 và hàng M14 ở mục 13, rồi mới viết mã.

**Ghi chú sau khi làm xong M14 (20/11/2026).** File này giữ nguyên như lúc nghiên cứu để đối chiếu; bản đã chốt là mục 6.13 của `docs/KE-HOACH.md`. Những chỗ bản chốt khác file này, kèm bằng chứng, nằm ở phần "Đã chốt khi làm M14" của mục đó. Tóm tắt: có thêm lỗ L9 (Apache, `Options FollowSymLinks`, `t9`), nên danh sách chỉ thị còn 55 tên và `Options` đi qua `AllowOverride Options=...` thay cho `AllowOverride None`; logrotate nhánh nginx gửi `HUP` thay cho `USR1` (`t8`); L8 có trên cả hai stack; phép thử là chín lỗ L1 đến L9 chứ không phải tám.

## 1. Đã thử gì, kết quả ra sao

Máy thử: máy ảo Multipass riêng, Ubuntu 24.04, TPAN v0.1.2 cài bằng `install.sh`, hai site `a.example.com` và `b.example.com` do `tpan site add` tạo. Phiên bản: nginx 1.24.0, Apache 2.4.58 (MPM event), PHP 8.3.6, systemd 255, nhân 6.8. "A" là site đã bị chiếm (kẻ tấn công chạy được PHP dưới user của A), "B" là nạn nhân. Script và kết quả gốc: `docs/bang-chung/M14-nghien-cuu/` (t1 đến t7).

### 1.1 Lỗ đã chứng minh

| # | Đường tấn công | Stack | Kết quả trên v0.1.2 | Cách chặn đã thử, kết quả |
|---|---|---|---|---|
| L1 | OPcache dùng chung giữa các pool (một tiến trình chủ PHP-FPM). PHP của A `include` đường dẫn `wp-config.php` của B, lúc file đang nằm trong cache. | cả hai | **Lấy được hằng `DB_PASSWORD` của B**, dù `open_basedir` của A còn nguyên và `test -r` (phép kiểm `Isolated` hiện nay) vẫn báo bị từ chối. Có mật khẩu DB là vào được DB của B. | `opcache.validate_permission=1`: không lấy được nữa (3 lần thử). WordPress hai site vẫn 200. `systemctl reload` là đủ để nhận. |
| L2 | `opcache_get_status()` gọi từ A. | cả hai | Liệt kê được 494 đường dẫn file của B. `opcache_reset()` cũng gọi được (xoá cache của mọi site). | `opcache.restrict_api=<đường dẫn không site nào có>`: bị chặn. |
| L3 | `.htaccess` của A thêm một dòng `SetEnv PHP_ADMIN_VALUE "open_basedir=/"`. | Apache | `open_basedir` của pool A thành `/`. PHP-FPM nhận `PHP_VALUE` và `PHP_ADMIN_VALUE` từ web server và cho đè cả `php_admin_value` của pool (PHP coi là đúng thiết kế, bug #79417). | Thêm vào vhost `ProxyFCGISetEnvIf "true" !PHP_VALUE` và `!PHP_ADMIN_VALUE`: `open_basedir` giữ nguyên. |
| L4 | `.htaccess` của A: `SetHandler` trỏ sang socket FPM của B, `ProxyFCGISetEnvIf` đặt `SCRIPT_FILENAME` là một file của B và `PHP_ADMIN_VALUE` có `auto_prepend_file`. | Apache | **Mã do A viết chạy dưới user của B**, đọc được `wp-config.php` của B. Tệ hơn: cấu hình bơm vào nằm lại trong tiến trình PHP của B, nên khách vào trang chủ của B cũng chạy mã của A cho tới khi tiến trình đó thoát. | `AllowOverride None` + `AllowOverrideList` (không có `SetHandler`, `AddHandler`, `Action`, `ProxyFCGISetEnvIf`) + hai dòng gỡ biến ở L3: `.htaccess` của A báo 500 (A tự hỏng), B không sao. Cờ `[H=…]` và `[E=…]` của `RewriteRule` vẫn dùng được nhưng chỉ nhận về `File not found` từ pool của B. |
| L5 | Symlink trong thư mục của A trỏ sang file của B, gọi qua nginx (nginx chạy dưới `www-data`, đọc được mọi site qua nhóm). | nginx | File `640` nhóm `www-data` của B bị trả về (200), kể cả **mã nguồn PHP** của B khi symlink mang đuôi `.txt`. `wp-config.php` (600) không lộ theo đường này. | `disable_symlinks if_not_owner from=$document_root;` trong vhost: 404. Symlink của A trỏ vào file của chính A vẫn 200; trang chủ, file tĩnh, trang 404 của WordPress vẫn đúng. |
| L6 | Đọc log của site khác. | cả hai | `access.log`, `error.log` là `root:root 644` trong thư mục `0755`: user A đọc được log của B (IP khách, URL kèm tham số). | Thư mục log `root:<nhóm của site> 0750`, file `0640`: A bị từ chối, B đọc được log của mình, nginx và PHP vẫn ghi, logrotate vẫn xoay. |
| L7 | PHP gọi lệnh shell (`shell_exec`). | cả hai | `open_basedir` không quản tiến trình con: đọc được file ngoài thư mục site, `ls /var/www` thấy tên mọi site, `ps` thấy 117 tiến trình của user khác kèm dòng lệnh. Quyền file Linux vẫn giữ (không đọc được file của B). | Drop-in systemd `ProtectProc=invisible` cho dịch vụ PHP-FPM: còn 0 tiến trình của user khác, hai site vẫn 200. |
| L8 | Ai đó thêm tay một vhost trùng `server_name` của site A. | nginx | `nginx -t` vẫn mã 0, chỉ cảnh báo `conflicting server name … ignored`; vhost đứng trước theo thứ tự tên file thắng, tên miền của A trả trang lạ. `tpan health` báo được triệu chứng ("trả 200 nhưng không phải WordPress") nhưng không chỉ ra nguyên nhân. | Chưa có; xem 2.7. |

### 1.2 Đã thử, không lộ (giữ nguyên, có test hồi quy)

- User A đọc `wp-config.php`, liệt kê thư mục, ghi vào `public` của B: bị từ chối cả ba.
- User A ghi vào socket FPM của B: không được.
- Symlink qua Apache: 403 nhờ `Options SymLinksIfOwnerMatch`.
- `RewriteRule` của A trỏ tới đường dẫn tuyệt đối của file B: 404, không lộ.
- `.user.ini` của A đặt `open_basedir=/` (stack nginx): không đổi được, vì pool dùng `php_admin_value`.
- Định tuyến theo `Host`: chữ hoa, dấu chấm cuối đều về đúng site; tên lạ, IP trần, không có `Host`: nginx đóng kết nối (444).

### 1.3 Ghi chú kỹ thuật rút ra

- `AllowOverrideList` chỉ nhận **tối đa 64 tên** trên một dòng; phần thừa bị bỏ lặng lẽ, và dòng sau thay dòng trước chứ không cộng dồn. Danh sách sinh tự động từ `apache2ctl -L` (129 tên) làm `RewriteEngine` rơi mất và WordPress báo 500. Phải dùng danh sách chọn tay ở 2.3 (56 tên, đã thử: WordPress chạy, các chỉ thị plugin hay ghi đều qua).
- Trên stack nginx không có file cấu hình web server nào do site ghi được, nên L3 và L4 không tồn tại. Đây là một lý do thật để giữ nginx làm stack mặc định.
- Phép kiểm `Isolated` hiện nay (`test -r` dưới user A) đạt trong khi L1 đang mở. Health phải kiểm cả cấu hình OPcache.

## 2. Đặc tả M14

Nguyên tắc: mỗi mục dưới đây đóng một dòng ở bảng 1.1, và e2e chạy lại đúng phép thử đó trên máy ảo: trước bản vá thì lộ, sau thì không.

### 2.1 OPcache (L1, L2)

- File mới do TPAN quản cho **mỗi bản PHP đang cài**: `/etc/php/<ver>/fpm/conf.d/99-tpan.ini`, nội dung `opcache.validate_permission=1` và `opcache.restrict_api=/usr/local/lib/tpan/opcache-api/` (đường dẫn không tồn tại, không site nào nằm dưới nó). Dòng đầu là chú thích `; Do tpan quản lý…` như file pool.
- Ghi lúc cài stack, lúc thêm một bản PHP (M12), và trong Plan "áp bản vá cô lập" (2.9). Sau khi ghi: `php-fpm<ver> -t` rồi reload dịch vụ.
- Health (bộ của stack, mỗi bản PHP một phép): file còn đúng (phát hiện lệch như các file khác) và `php-fpm<ver> -i` cho `opcache.validate_permission => On`. Sai là **Fail**, có sửa tự động.

### 2.2 nginx: symlink (L5)

- Thêm `disable_symlinks if_not_owner from=$document_root;` ở cấp `server` trong mọi template vhost nginx của site (cổng 80, cổng 443, và bản tạm khoá của M13 nếu có `root`). Chú thích trong template nói rõ lý do.
- Không đổi vhost mặc định.

### 2.3 Apache: `.htaccess` (L3, L4)

- Trong mọi template vhost Apache của site, cấp `VirtualHost`:
  ```
  ProxyFCGISetEnvIf "true" !PHP_VALUE
  ProxyFCGISetEnvIf "true" !PHP_ADMIN_VALUE
  ```
- Khối `<Directory "<public>">`: thay `AllowOverride All` theo cài đặt `htaccess` của site:
  - `an-toan` (mặc định): `AllowOverride None` và một dòng `AllowOverrideList` với đúng 56 tên sau (hằng số trong mã, có test đếm ≤ 64 và test "không chứa SetHandler, AddHandler, Action, RemoveHandler, ProxyFCGISetEnvIf, ProxyFCGIBackendType"):
    `<Files <FilesMatch <IfModule <If <Else <ElseIf <Limit <LimitExcept <RequireAll <RequireAny <RequireNone RewriteEngine RewriteRule RewriteCond RewriteBase RewriteOptions Require Order Allow Deny Satisfy AuthType AuthName AuthUserFile AuthGroupFile AuthBasicProvider Header RequestHeader ExpiresActive ExpiresByType ExpiresDefault FileETag AddType AddEncoding AddCharset AddDefaultCharset AddOutputFilterByType AddOutputFilter SetOutputFilter SetEnv SetEnvIf SetEnvIfNoCase UnsetEnv BrowserMatch BrowserMatchNoCase Options DirectoryIndex ErrorDocument Redirect RedirectMatch RedirectPermanent RedirectTemp LimitRequestBody ServerSignature AddLanguage DefaultLanguage`
  - `tat`: `AllowOverride None`, không có `AllowOverrideList`, permalink bằng `FallbackResource /index.php` trong vhost. TPAN không ghi `.htaccess` cho site ở chế độ này.
  - `day-du`: `AllowOverride All` như cũ. Chỉ admin đặt được. Health của site luôn có một **cảnh báo** nói rõ: site này ghi được cấu hình Apache và có thể chạy mã dưới user của site khác (L4).
- Cài đặt nằm ở bảng `sites` (cột mới), sửa bằng `tpan site set <domain> --htaccess an-toan|tat|day-du` và trên trang site (chỉ hiện khi stack là Apache). Đổi chế độ là một Plan có xem trước.
- Trước khi chuyển một site sang `an-toan`: quét mọi `.htaccess` dưới `public` (dưới user của site), liệt kê chỉ thị không nằm trong danh sách. Có thì Plan từ chối và in từng dòng (file, số dòng, chỉ thị), kèm hai lựa chọn: sửa `.htaccess`, hoặc để site ở `day-du` và nhận cảnh báo. Lý do: một chỉ thị ngoài danh sách làm cả site báo 500.
- Health của site (Apache, chế độ `an-toan`): quét như trên; có chỉ thị ngoài danh sách là **Fail** với đúng dòng gây lỗi (plugin vừa ghi thêm).

### 2.4 Log (L6)

- Thư mục `/var/log/tpan/sites/<domain>`: `root:<nhóm của site>`, `0750`. `access.log`, `error.log`: `0640`, nhóm của site. `php-error.log` giữ nguyên.
- Kiểm lại với `logrotate` thật trên máy ảo (đã thử: xoay được, file mới vẫn `0640`).
- Trình xem log của panel đọc bằng root nên không đổi.

### 2.5 /proc (L7)

- Drop-in `/etc/systemd/system/php<ver>-fpm.service.d/tpan.conf` với `ProtectProc=invisible`, cho mỗi bản PHP. `daemon-reload` rồi restart dịch vụ (đổi không gian mount cần restart, không reload được). Ghi trong Plan cài stack và Plan 2.9.
- Health: `systemctl show php<ver>-fpm -p ProtectProc` phải là `invisible`. Sai là **Warn** (không phải Fail: quyền file vẫn giữ), có sửa tự động.
- Không gắn lại `/proc` của cả máy.

### 2.6 Phép kiểm cô lập mở rộng

`checks.Isolated(from, to)` hiện chỉ có `test -r wp-config.php`. Thêm, vẫn dưới user của `from`, mỗi phép một dòng bằng chứng:

- `test -x <thư mục của to>` phải bị từ chối;
- `test -w <public của to>` phải bị từ chối;
- `test -r <access.log của to>` phải bị từ chối;
- `test -w <socket FPM của to>` phải bị từ chối.

Một phép lọt là **Fail**. Giữ luật cũ: mã 1 chỉ tin khi stderr rỗng, và file đích phải tồn tại thì "bị từ chối" mới có nghĩa.

Trên màn Bảo mật thêm khối "Cô lập giữa các site": ma trận site × site (đạt, lỗi, chưa rõ), bấm vào một ô thì thấy lệnh và output. Dưới ma trận là các phép kiểm cấp máy của 2.1, 2.5 và chế độ `.htaccess` của từng site Apache.

### 2.7 Định tuyến (yêu cầu 1, L8)

- Phép kiểm mới `web.server_name.unique` (bộ của stack): đọc `nginx -T` (hoặc `apache2ctl -S`), tìm mọi tên miền của site TPAN xuất hiện ở hơn một `server`/`VirtualHost` trên cùng cổng, hoặc trong file không do TPAN quản. Có là **Fail**, nêu tên hai file và dòng; sửa tay (TPAN không xoá file lạ).
- Phép kiểm mới `web.default.rejects`: gọi vào `127.0.0.1:80` với `Host: tpan-<ngẫu nhiên>.invalid`. Đạt khi nginx đóng kết nối không trả gì, hoặc Apache trả 403. Có site nào bật SSL thì thêm phép cho 443: bắt tay TLS với SNI lạ phải bị từ chối (nginx) hoặc request bị 403 (Apache).
- Lệnh `tpan route explain <domain>` và `GET /api/v1/sites/{domain}/route`: in chuỗi "tên miền → vhost (file, dòng `server_name`) → `root` → socket → pool → user → `open_basedir`", mỗi mắt xích kèm lệnh đã chạy và output (dùng lại các phép kiểm sẵn có: DNS, `HTTPSite`, pool, user). Trang site có nút "Request tới site này đi đường nào" mở đúng kết quả đó. Quyền: chủ của site hoặc admin (luật `owner:site` của M13).

### 2.8 `tpan isolation probe` (làm sau cùng, cắt được nếu thiếu thời gian)

Lệnh cho admin: `tpan isolation probe --from <site A> --to <site B>`. Đặt các file thử tên ngẫu nhiên vào một thư mục con tên ngẫu nhiên của A, chạy lại các phép thử L1, L2, L5, L7 (và L3, L4 nếu stack là Apache) theo kiểu không phá (chỉ in CÓ/KHÔNG và tên user, không in bí mật), xoá file thử, in bảng. Là một Plan có xem trước, ghi `audit`. Không chạy trong health định kỳ. Mục đích: demo cho người xem thấy từng đường tấn công bị chặn trên chính máy của họ.

### 2.9 Site đã có và nâng cấp từ bản cũ

- Sau khi cập nhật lên bản có M14, `tpan health` báo các site cũ lệch template (vhost, quyền log) và thiếu file 2.1, 2.5. Một Plan gộp "Áp bản vá cô lập" (CLI `tpan security harden`, nút trên màn Bảo mật) làm hết: ghi file ini và drop-in, ghi lại vhost của mọi site, đổi quyền log, quét `.htaccess` (site nào vướng thì giữ `day-du` và báo), `nginx -t`/`apache2ctl configtest`, reload, chạy lại phép kiểm. Hoàn tác được như mọi Plan.
- Site mới tạo sau M14 có sẵn mọi thứ.

### 2.10 Xong khi (hàng M14 ở mục 13)

- `make check` xanh.
- e2e bước 17 trên máy ảo sạch, **cả hai stack**, hai site: tám phép thử L1 đến L8 đều cho kết quả "bị chặn" (L8: health chỉ đúng file trùng), các phép ở 1.2 vẫn đạt, WordPress hai site 200, permalink chạy.
- e2e nâng cấp: cài v0.1.2, tạo hai site, cập nhật lên bản mới, chạy `tpan security harden`, rồi chạy lại bộ phép thử.
- `docs/giai-thich/14-m14-co-lap-va-dinh-tuyen.md`: lời thường, cấu hình thật, lệnh chạy lại được; có phần "vì sao nginx đứng trước" (event loop, đỡ khách chậm, TLS, cache, và điểm rút ra ở 1.3: không có `.htaccess`).
- README (bảng tính năng, CLI), mục 5 và 6.8 của `KE-HOACH.md` cập nhật.

## 3. Để sau M14 (ghi vào "Để sau" ở mục 13)

- **Lồng theo site (ứng viên M15).** Bản thử trong `t5`: mỗi site một dịch vụ PHP-FPM riêng do systemd quản, với `ProtectSystem=strict`, `TemporaryFileSystem=/var/www:ro` + `BindPaths=<thư mục site>`, `PrivateTmp`, `ProtectProc=invisible`, `MemoryMax`, `CPUQuota`, `TasksMax`. Kết quả: `ls /var/www` chỉ thấy site của mình, `/tmp` riêng, OPcache riêng (L1 hết ngay cả khi không bật `validate_permission`), script cấp phát 400 MB bị giết ở trần 200 MB còn site vẫn sống (cần `OOMPolicy=continue`; mặc định `stop` làm cả dịch vụ dừng), vòng lặp CPU chậm đi 2,5 lần ở `CPUQuota=50%`. Giá: khoảng 32 MB RAM cho mỗi site kể cả lúc không có khách. Hợp để gắn vào gói của M13 (trần RAM, CPU theo gói). Lệnh cho site (WP-CLI, cron) chạy trong cùng kiểu lồng bằng `systemd-run` cũng đã thử được.
- Tên phụ của site (`www.`), chuyển hướng: hiện `www.<domain>` rơi vào vhost mặc định.
- Stack thứ ba "nginx đứng trước Apache": đem `.htaccess` trở lại sau nginx, nên phải đi cùng 2.3.
