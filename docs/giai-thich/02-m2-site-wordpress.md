# M2: tạo site WordPress bằng `tpan site add`

Đọc kèm: `01-m1-cai-stack.md` (engine, Step, `File`, `verifyOrRestore`, `recSys` đã giải thích ở đó), `00-go-can-ban.md`, `docs/KE-HOACH.md` mục 5 (bố trí và cô lập), 6.2 (luồng tạo site), 6.4 đoạn "Ở M2" (xoá), 6.7 (health), 11 (bảo mật), 16 (các dòng ghi "M2"), và `docs/bang-chung/M2.log` (log thật trên máy ảo).

## 1. Mốc này làm được gì

- Trên máy đã cài stack ở M1, `sudo tpan site add shop.example.com --email admin@example.com` tạo trọn một site WordPress: user Linux riêng, thư mục với đúng quyền, DB và user DB riêng, pool PHP-FPM riêng, vhost Nginx hoặc Apache, rồi cài WordPress và in mật khẩu quản trị đúng một lần.
- Các site cô lập với nhau: PHP của site A không đọc được file của site B, user DB của A không thấy DB của B.
- Chạy lại `site add` cho domain đã có: máy đúng rồi thì không đổi gì, lệch chỗ nào thì sửa chỗ đó. Một bước lỗi giữa chừng thì mọi bước trước được hoàn tác ngược, không sót user, DB hay thư mục.
- `tpan site list`, `tpan site info <domain>`, `tpan health <domain>` (7 phép kiểm theo đường đi của request, cộng 2 phép kiểm cô lập cho mỗi site khác). `tpan site rm <domain>` xoá hẳn site; M2 chưa có thùng rác.

## 2. Chạy thử

Mọi output dưới đây lấy nguyên văn từ `docs/bang-chung/M2.log`: lần chạy ngày 19/11/2026, 14:59 tới 15:03, bản tpan build từ commit `86134d0`, dòng cuối của log là `TỔNG KẾT: 0 khẳng định không đạt`. Máy ảo `tpan-test` là Multipass, Ubuntu 24.04, arm64.

Ba điều cần biết khi đọc log:

- Kịch bản `scripts/e2e-m2.sh` in dòng lệnh bằng `$*`, nên dấu nháy bị mất. Lệnh dưới đây mình viết lại với dấu nháy đúng để bạn dán vào zsh được; output giữ nguyên.
- Kịch bản tự che mật khẩu quản trị, mã băm mật khẩu và IP máy ảo trước khi in (hàm `mask` trong script). Những chỗ `<mật khẩu, đã che>`, `<mã băm, đã che>`, `<IP máy ảo>` là chữ của script, không phải của tpan. Lúc bạn tự chạy, tpan in mật khẩu thật.
- Toàn bộ kịch bản chạy trên Mac: `scripts/e2e-m2.sh all > /tmp/m2.log` (nửa Nginx rồi nửa Apache, mỗi nửa bắt đầu từ máy sạch). Chỉ nửa Nginx: `scripts/e2e-m2.sh nginx`.

### 2.1 Chuẩn bị

Chạy trên Mac, trong thư mục repo:

```bash
make vm-reset
make vm-deploy
scripts/vm.sh root tpan stack install --web nginx --yes
```

Output của lần cài stack giống mục 2.3 của file M1, nên mình không chép lại (M2.log dòng 58 tới 115).

### 2.2 Tạo site đầu tiên

Chạy trên Mac:

```bash
scripts/vm.sh root tpan site add a.example.com --email admin@example.com --yes --verbose
```

`--email` là bắt buộc (`MarkFlagRequired` trong `cmd/tpan/site.go`). `--verbose` in mọi lệnh tpan chạy (dòng `$`) và output của chúng (dòng `|`). Output, mình bớt vài đoạn:

```
Bắt đầu job a4e0427b1ccae163
- Tạo user Linux tp_a_example_com_7653e4 cho site ...
    $ getent passwd tp_a_example_com_7653e4
    $ getent passwd tp_a_example_com_7653e4
    $ useradd --system --user-group --home-dir /var/www/a.example.com --no-create-home --shell /usr/sbin/nologin tp_a_example_com_7653e4
    $ getent passwd tp_a_example_com_7653e4
    | tp_a_example_com_7653e4:x:999:988::/var/www/a.example.com:/usr/sbin/nologin
  xong
- Tạo thư mục /var/www/a.example.com ...
  xong
... (bớt dòng)
- Tạo DB tp_a_example_com_7653e4 và user DB riêng của site a.example.com ...
    $ mariadb -N -B
    | 0	0
    $ mariadb -N -B
    | 0	0
    $ mariadb -N -B
    $ mariadb -N -B
    $ mariadb -N -B
    $ mariadb -N -B
    | 1	1
  xong
- Ghi pool PHP-FPM tpan-a_example_com_7653e4 cho a.example.com ...
    $ php-fpm8.3 -t
    | [19-Nov-2026 15:00:53] NOTICE: configuration file /etc/php/8.3/fpm/php-fpm.conf test is successful
    $ systemctl reload php8.3-fpm
  xong
- Tải WordPress 7.1.5 vào /var/cache/tpan ...
    $ curl -fsSL -o /var/cache/tpan/wordpress-7.1.5.tar.gz https://wordpress.org/wordpress-7.1.5.tar.gz
  xong
- Giải nén WordPress vào /var/www/a.example.com/public ...
    $ runuser -u tp_a_example_com_7653e4 -- tar -xzf /var/cache/tpan/wordpress-7.1.5.tar.gz -C /var/www/a.example.com/public --strip-components=1
    $ find /var/www/a.example.com/public -mindepth 1 -type d -exec chmod 2750 '{}' +
    $ runuser -u tp_a_example_com_7653e4 -- find /var/www/a.example.com/public -mindepth 1 -type f -exec chmod 0640 '{}' +
  xong
- Tạo /var/www/a.example.com/public/wp-config.php ...
    $ runuser -u tp_a_example_com_7653e4 -- wp config create --dbname=tp_a_example_com_7653e4 --dbuser=tp_a_example_com_7653e4 --dbhost=localhost --dbcharset=utf8mb4 --prompt=dbpass --quiet --path=/var/www/a.example.com/public
    $ runuser -u tp_a_example_com_7653e4 -- wp config set DISALLOW_FILE_EDIT true --raw --type=constant --quiet --path=/var/www/a.example.com/public
    $ runuser -u tp_a_example_com_7653e4 -- chmod 0600 -- /var/www/a.example.com/public/wp-config.php
  xong
- Ghi vhost nginx cho a.example.com ...
    $ nginx -t
    | nginx: the configuration file /etc/nginx/nginx.conf syntax is ok
    | nginx: configuration file /etc/nginx/nginx.conf test is successful
    $ systemctl reload nginx
  xong
... (bớt dòng)
- Cài WordPress cho a.example.com ...
    $ runuser -u tp_a_example_com_7653e4 -- wp core is-installed --skip-plugins --skip-themes --path=/var/www/a.example.com/public
    | PHP Warning:  Undefined array key "HTTP_HOST" in /var/www/a.example.com/public/wp-includes/functions.php on line 6473
... (bớt dòng)
    $ runuser -u tp_a_example_com_7653e4 -- wp core install --url=http://a.example.com --title=a.example.com --admin_user=admin --admin_email=admin@example.com --prompt=admin_password --skip-email --quiet --path=/var/www/a.example.com/public
    $ runuser -u tp_a_example_com_7653e4 -- wp option get permalink_structure --skip-plugins --skip-themes --path=/var/www/a.example.com/public
    | 
    $ runuser -u tp_a_example_com_7653e4 -- wp rewrite structure /%postname%/ --quiet --path=/var/www/a.example.com/public
... (bớt dòng)
  xong
... (bớt dòng)
Job a4e0427b1ccae163: thành công
Kiểm chứng:
  [đạt] Vhost của site có, do tpan quản lý và đang được bật: Vhost của site có, đúng dòng đầu của tpan, và đã được bật
  [đạt] Cấu hình PHP-FPM 8.3 hợp lệ: Cấu hình PHP-FPM 8.3 hợp lệ
  [đạt] Socket của pool PHP có, đúng chủ và đúng quyền: Socket của pool có, chủ www-data:www-data, quyền 0660
  [đạt] Mã nguồn WordPress nguyên vẹn (khớp checksum): Mã nguồn WordPress khớp checksum
  [đạt] wp-config.php có quyền 0600 và đúng chủ: wp-config.php quyền 0600, chủ tp_a_example_com_7653e4
  [đạt] User DB của site đăng nhập được, chỉ thấy DB của site: User DB đăng nhập được và chỉ thấy DB tp_a_example_com_7653e4
  [đạt] Gọi HTTP vào site trả 200 và đúng là WordPress của site: Site trả 200, đúng là WordPress của a.example.com

Tài khoản quản trị WordPress:
  Đăng nhập:     http://a.example.com/wp-admin/
  Tên:           admin
  Mật khẩu:      <mật khẩu, đã che>
Lưu lại ngay: tpan không giữ mật khẩu này và sẽ không in lại.
```

Kịch bản đếm ngay sau lệnh:

```
Mật khẩu quản trị trong output: 1 lần; mật khẩu DB của site trong output: 0 lần
```

Những chỗ người review hay hỏi:

- `getent` chạy hai lần trước `useradd`, và một lần sau. Lần một là `Check` của bước, lần hai là `Apply` hỏi lại trước khi tạo, lần ba là `Check` lại sau `Apply`. Engine luôn chạy Check, Apply, Check (xem `runStep`, mục 3.7 của file M1).
- Bước DB có 6 lệnh `mariadb`: Check đọc trạng thái (`0	0`: chưa có DB, chưa có user DB), Apply đọc lại, rồi ba câu tạo DB, tạo user, cấp quyền chạy riêng từng lệnh, cuối cùng Check lại thấy `1	1`. Câu SQL đi qua stdin nên dòng `$` không cho thấy nó.
- Không có dòng nào chứa mật khẩu DB hay mật khẩu quản trị, dù `wp config create` và `wp core install` nhận chúng. Chúng đi qua stdin (`--prompt=dbpass`, `--prompt=admin_password`).
- Cảnh báo `Undefined array key "HTTP_HOST"` là của WordPress 7.1.5 khi chạy từ dòng lệnh, không ảnh hưởng gì (đã ghi ở mục 16 của KE-HOACH.md).
- Kịch bản còn đăng nhập thật vào `wp-login.php` bằng mật khẩu tpan in ra: nhận `HTTP/1.1 302 Found` kèm cookie `wordpress_logged_in_`; với mật khẩu sai thì `HTTP/1.1 200 OK`, không có cookie đó (M2.log dòng 1253 tới 1293). Đây là bằng chứng WP-CLI nhận đúng mật khẩu qua stdin.

Kịch bản tạo thêm `b.example.com` và `c.example.com` bằng đúng lệnh trên. Mỗi site gọi bằng `curl` đều trả 200, có tên của mình và không nhắc tên site khác:

```
HTTP 200
<title>a.example.com</title>
thân nhắc a.example.com: 30 dòng
thân nhắc b.example.com: 0 dòng
thân nhắc c.example.com: 0 dòng
```

### 2.3 Xem: list, info, health

Chạy trên Mac:

```bash
scripts/vm.sh root tpan site list
scripts/vm.sh root tpan site info a.example.com
scripts/vm.sh root tpan health a.example.com
```

```
DOMAIN                                   TRẠNG THÁI PHP   SSL  TẠO LÚC
a.example.com                            active   8.3   không 2026-11-19 15:01
b.example.com                            active   8.3   không 2026-11-19 15:01
c.example.com                            active   8.3   không 2026-11-19 15:01
```

```
Domain:          a.example.com
Trạng thái:      active
PHP:             8.3
Tạo lúc:         2026-11-19 15:01
User Linux:      tp_a_example_com_7653e4
DB, user DB:     tp_a_example_com_7653e4
Thư mục:         /var/www/a.example.com
Document root:   /var/www/a.example.com/public
Log:             /var/log/tpan/sites/a.example.com
Pool PHP-FPM:    tpan-a_example_com_7653e4 (/run/php/tpan-a_example_com_7653e4.sock)
Vhost:           /etc/nginx/sites-available/tpan-a.example.com.conf
```

(Phần "Kiểm tra:" bên dưới của `site info` giống hệt output của `health`.)

```
[đạt] Vhost của site có, do tpan quản lý và đang được bật: Vhost của site có, đúng dòng đầu của tpan, và đã được bật
[đạt] Cấu hình PHP-FPM 8.3 hợp lệ: Cấu hình PHP-FPM 8.3 hợp lệ
[đạt] Socket của pool PHP có, đúng chủ và đúng quyền: Socket của pool có, chủ www-data:www-data, quyền 0660
[đạt] Mã nguồn WordPress nguyên vẹn (khớp checksum): Mã nguồn WordPress khớp checksum
[đạt] wp-config.php có quyền 0600 và đúng chủ: wp-config.php quyền 0600, chủ tp_a_example_com_7653e4
[đạt] User DB của site đăng nhập được, chỉ thấy DB của site: User DB đăng nhập được và chỉ thấy DB tp_a_example_com_7653e4
[đạt] Gọi HTTP vào site trả 200 và đúng là WordPress của site: Site trả 200, đúng là WordPress của a.example.com
[đạt] Site a.example.com không đọc được wp-config.php của site b.example.com: Bị từ chối như mong đợi: tp_a_example_com_7653e4 không đọc được wp-config.php của b.example.com
[đạt] Site b.example.com không đọc được wp-config.php của site a.example.com: Bị từ chối như mong đợi: tp_b_example_com_cd59d5 không đọc được wp-config.php của a.example.com
[đạt] Site a.example.com không đọc được wp-config.php của site c.example.com: Bị từ chối như mong đợi: tp_a_example_com_7653e4 không đọc được wp-config.php của c.example.com
[đạt] Site c.example.com không đọc được wp-config.php của site a.example.com: Bị từ chối như mong đợi: tp_c_example_com_6e5b26 không đọc được wp-config.php của a.example.com

11 đạt
```

Vì sao lúc tạo `a.example.com` chỉ có 7 phép kiểm, còn bây giờ 11: phép kiểm cô lập chỉ được thêm cho các site đã nằm trong bảng `sites` (hàm `others` trong `internal/core/site.go`). Lúc tạo `a`, chưa có site nào khác. Bây giờ có `b` và `c`, mỗi site thêm 2 phép kiểm (hai chiều). Sau khi xoá `c` ở mục 2.9, `health a.example.com` còn `9 đạt`.

`site info a.example.com --json` in thêm bảng `layout`: mọi tên và đường dẫn của site, suy ra từ domain (M2.log dòng 235 tới 418).

### 2.4 Xem trước: `--dry-run`

Chạy trên Mac:

```bash
scripts/vm.sh root tpan site add d.example.com --email admin@example.com --dry-run
```

Bớt dòng:

```
Kế hoạch: Tạo site WordPress d.example.com
13 bước sẽ chạy, 1 bước đã đúng sẽ bỏ qua, 0 bước có thể gián đoạn dịch vụ.

 1. [sẽ chạy] Tạo user Linux tp_d_example_com_316ffc cho site  (site.user:tp_d_example_com_316ffc)
    Vì sao: PHP của site chạy dưới user riêng này. Bỏ bước này thì PHP của mọi site chạy chung một user, và một site bị chiếm là đọc được wp-config.php (mật khẩu DB) của mọi site khác trên máy.
    $ useradd --system --user-group --home-dir /var/www/d.example.com --no-create-home --shell /usr/sbin/nologin tp_d_example_com_316ffc
... (bớt dòng)
 7. [sẽ chạy] Tạo DB tp_d_example_com_316ffc và user DB riêng của site d.example.com  (mariadb.site-db:d.example.com)
    Vì sao: WordPress cần một DB để lưu bài viết, tài khoản và cài đặt; thiếu nó thì không cài được site. User DB riêng chỉ có quyền trên đúng DB này, nên lỡ site bị chiếm thì kẻ tấn công không đọc được DB của site khác.
    $ mariadb -N -B  (stdin: CREATE DATABASE `tp_d_example_com_316ffc` CHARACTER SET utf8mb4;)
    $ mariadb -N -B  (stdin: CREATE USER 'tp_d_example_com_316ffc'@'localhost' IDENTIFIED BY '••••';)
    $ mariadb -N -B  (stdin: GRANT ALL PRIVILEGES ON `tp\_d\_example\_com\_316ffc`.* TO 'tp_d_example_com_316ffc'@'localhost';)
... (bớt dòng)
 9. [đã đúng, bỏ qua] Tải WordPress 7.1.5 vào /var/cache/tpan  (wp.archive:7.1.5)
... (bớt dòng)
14. [sẽ chạy] Cài WordPress cho d.example.com  (wp.install:d.example.com)
    Vì sao: Bước này tạo bảng của WordPress và tài khoản quản trị. Thiếu nó thì ai mở site trước sẽ tự đặt tài khoản quản trị và chiếm site. Permalink /%postname%/ cho đường dẫn bài viết dễ đọc thay vì ?p=123.
    $ runuser -u tp_d_example_com_316ffc -- wp core install --url=http://d.example.com --title=d.example.com --admin_user=admin --admin_email=admin@example.com --prompt=admin_password --skip-email --quiet --path=/var/www/d.example.com/public
    $ runuser -u tp_d_example_com_316ffc -- wp rewrite structure /%postname%/ --quiet --path=/var/www/d.example.com/public
    Ghi chú hoàn tác: Bảng của WordPress nằm trong DB của site; hoàn tác bước tạo DB thì chúng mất theo.
```

Ba điều đáng chỉ ra: mật khẩu DB trong bản xem trước là `••••`; câu GRANT có `\_` thay cho `_` (lý do ở mục 4, "GRANT và dấu gạch dưới"); bước tải WordPress "đã đúng" vì file đã nằm trong cache từ lần tạo `a`. Toàn bộ Plan Nginx có 14 bước, Apache có 15 (thêm `.htaccess`).

### 2.5 Cô lập (e2e bước 3)

Quyền thật trên máy. Chạy trên Mac:

```bash
scripts/vm.sh root stat -c '%a %U:%G %n' /var/www/a.example.com /var/www/a.example.com/public /var/www/a.example.com/tmp /var/www/a.example.com/public/wp-config.php /var/www/a.example.com/public/index.php /var/log/tpan/sites/a.example.com /var/log/tpan/sites/a.example.com/php-error.log /run/php/tpan-a_example_com_7653e4.sock
```

```
2750 tp_a_example_com_7653e4:www-data /var/www/a.example.com
2750 tp_a_example_com_7653e4:www-data /var/www/a.example.com/public
700 tp_a_example_com_7653e4:tp_a_example_com_7653e4 /var/www/a.example.com/tmp
600 tp_a_example_com_7653e4:www-data /var/www/a.example.com/public/wp-config.php
640 tp_a_example_com_7653e4:www-data /var/www/a.example.com/public/index.php
755 root:root /var/log/tpan/sites/a.example.com
640 tp_a_example_com_7653e4:tp_a_example_com_7653e4 /var/log/tpan/sites/a.example.com/php-error.log
660 www-data:www-data /run/php/tpan-a_example_com_7653e4.sock
```

Thử đọc chéo dưới user của site `a`, chạy trên Mac:

```bash
scripts/vm.sh root runuser -u tp_a_example_com_7653e4 -- test -r /var/www/a.example.com/public/wp-config.php
scripts/vm.sh root runuser -u www-data -- test -r /var/www/a.example.com/public/wp-config.php
scripts/vm.sh root runuser -u tp_a_example_com_7653e4 -- cat /var/www/b.example.com/public/wp-config.php
scripts/vm.sh root runuser -u tp_a_example_com_7653e4 -- ls /var/www/b.example.com/public
```

Lệnh một thoát mã 0 (đọc được file của chính mình), lệnh hai thoát mã 1 (web server không đọc được `wp-config.php`). Hai lệnh sau:

```
cat: /var/www/b.example.com/public/wp-config.php: Permission denied
```

```
ls: cannot access '/var/www/b.example.com/public': Permission denied
```

Kịch bản thử đủ 6 cặp (a đọc b, a đọc c, b đọc a...), cặp nào cũng bị từ chối.

Đọc theo quyền (giải thích kỹ ở mục 4):

- `tp_a_...` không phải chủ `/var/www/b.example.com`, không thuộc nhóm `www-data`, nên rơi vào phần "người khác" của `2750`, tức `0`: không được đi vào thư mục (thiếu bit `x`). Bị chặn ngay ở thư mục ngoài cùng, chưa tới được `public/` hay `wp-config.php`. Đó là lý do `ls` báo `cannot access`, không phải `cannot open directory`.
- Lớp thứ hai: `wp-config.php` là `600`. Kể cả `www-data`, đi qua được thư mục nhờ nhóm, cũng không đọc được nó.

PHP của mỗi site chạy dưới đúng user của site. Kịch bản gọi mỗi site một lần cho pool ondemand sinh worker, rồi `ps -eo user:32,cmd | grep '[p]hp-fpm'`:

```
root                             php-fpm: master process (/etc/php/8.3/fpm/php-fpm.conf)
www-data                         php-fpm: pool www
www-data                         php-fpm: pool www
tp_c_example_com_6e5b26          php-fpm: pool tpan-c_example_com_6e5b26
tp_a_example_com_7653e4          php-fpm: pool tpan-a_example_com_7653e4
tp_b_example_com_cd59d5          php-fpm: pool tpan-b_example_com_cd59d5
```

Pool `www` là pool mặc định của gói `php-fpm`, không vhost nào dùng; gỡ nó để sau (mục 16 của KE-HOACH.md).

Cô lập DB. Kịch bản đọc mật khẩu DB của site `a` bằng `wp config get DB_PASSWORD` dưới user của site (không in ra), đưa cho `mariadb` qua biến `MYSQL_PWD`, rồi chạy `SHOW DATABASES`, đếm bảng `wp_options`, và thử `USE` DB của site `b`:

```
SHOW DATABASES dưới user DB tp_a_example_com_7653e4:
information_schema
tp_a_example_com_7653e4
đếm wp_options trong DB của chính mình:
127
USE tp_b_example_com_cd59d5 dưới user DB tp_a_example_com_7653e4:
ERROR 1044 (42000) at line 1: Access denied for user 'tp_a_example_com_7653e4'@'localhost' to database 'tp_b_example_com_cd59d5'
mã thoát USE: 1
```

Quyền của user DB, chạy trên Mac:

```bash
scripts/vm.sh root mariadb -r -e "SHOW GRANTS FOR 'tp_a_example_com_7653e4'@'localhost'"
```

```
Grants for tp_a_example_com_7653e4@localhost
GRANT USAGE ON *.* TO `tp_a_example_com_7653e4`@`localhost` IDENTIFIED BY PASSWORD '<mã băm, đã che>'
GRANT ALL PRIVILEGES ON `tp\_a\_example\_com\_7653e4`.* TO `tp_a_example_com_7653e4`@`localhost`
```

`USAGE ON *.*` nghĩa là "được đăng nhập, không có quyền gì trên toàn máy". Quyền thật chỉ có ở dòng hai, trên đúng một DB. Cần `-r` để client in `\_` nguyên văn (mục 16 của KE-HOACH.md).

### 2.6 Chạy lại: không đổi gì (e2e bước 4)

Kịch bản chụp trước sha256 của cấu hình và `wp-config.php`, quyền, user, DB, PID dịch vụ, bảng `sites`; chạy lại `site add` cho cả ba site; chụp lần nữa rồi `diff`. Chạy trên Mac:

```bash
scripts/vm.sh root tpan site add a.example.com --email admin@example.com --yes --verbose
```

Kịch bản đếm:

```
Số bước: 14, số bước bỏ qua: 14
```

```
Các lệnh tpan đã chạy khi chạy lại a.example.com (dòng "$"), gom và đếm:
   1 getent passwd tp_a_example_com_7653e4
   1 mariadb -N -B
   1 runuser -u tp_a_example_com_7653e4 -- wp core is-installed --skip-plugins --skip-themes --path=/var/www/a.example.com/public
   1 runuser -u tp_a_example_com_7653e4 -- wp option get permalink_structure --skip-plugins --skip-themes --path=/var/www/a.example.com/public
```

```
Mật khẩu quản trị trong output: 0 lần; mật khẩu DB của site trong output: 0 lần
```

Cả bốn lệnh chỉ đọc. Các bước thư mục, file, symlink không chạy lệnh nào vì `Check` của chúng dùng `Stat` và `ReadFile` của Go. Lần chạy lại không in mật khẩu quản trị, vì WordPress không được cài lại (mục 3.6, `wpInstallStep`). `diff` hai ảnh chụp ra rỗng, mã thoát 0: `=> ĐẠT: máy không đổi (sha256 cấu hình và wp-config, quyền, user, DB, PID, bảng sites)`.

### 2.7 Domain có lệnh chèn (e2e bước 5)

Chạy trên Mac:

```bash
scripts/vm.sh root tpan site add 'a.com; rm -rf /' --email admin@example.com --yes
scripts/vm.sh root tpan site add 'a.com; rm -rf /' --email admin@example.com --yes --json
```

```
Từ chối: tên miền chỉ được chứa chữ, số, dấu gạch ngang và dấu chấm
```

```
{
  "error": {
    "code": "domain_invalid",
    "message": {
      "vi": "tên miền chỉ được chứa chữ, số, dấu gạch ngang và dấu chấm",
      "en": "the domain may only contain letters, digits, hyphens and dots"
    }
  }
}
```

Cả hai thoát mã 1. Ảnh chụp `tpan.db` và WAL, `/var/www`, user, nhóm, DB trước và sau giống nhau. Từ chối xảy ra ở `parseDomainArg`, dòng đầu tiên của `RunE`, trước khi hỏi root hay mở DB (mục 3.1).

Một chi tiết của chính kịch bản: phần đầu log (trước khi cài stack) kiểm `scripts/vm.sh exec printf '[%s]\n' 'a.com; echo INJECTED'` ra `[a.com; echo INJECTED]`, tức `multipass exec` chuyển nguyên một tham số. Không có bước đó thì bước 5 có thể "đạt" chỉ vì shell của Mac đã tách chuỗi trước khi tới tpan.

### 2.8 Hoàn tác khi bước vhost lỗi (e2e bước 6)

Cách gây lỗi: tạo sẵn một thư mục đúng ở chỗ file vhost của site `d` sẽ được ghi. Bước `File` không ghi đè thư mục nên lỗi. Chạy trên Mac:

```bash
scripts/vm.sh root mkdir /etc/nginx/sites-available/tpan-d.example.com.conf
scripts/vm.sh root tpan site add d.example.com --email admin@example.com --yes --verbose
```

Bớt dòng ở phần chạy tới:

```
Bắt đầu job b8cb1f72e71f677a
- Tạo user Linux tp_d_example_com_316ffc cho site ...
... (bớt dòng)
  xong
... (bớt dòng)
- Tải WordPress 7.1.5 vào /var/cache/tpan ...
  đã đúng, bỏ qua
... (bớt dòng)
- Ghi vhost nginx cho d.example.com ...
  LỖI: bước file:/etc/nginx/sites-available/tpan-d.example.com.conf: thực hiện: ghi /etc/nginx/sites-available/tpan-d.example.com.conf: /etc/nginx/sites-available/tpan-d.example.com.conf là thư mục, bước này chỉ làm việc với file hoặc symlink
- Hoàn tác: Tạo /var/www/d.example.com/public/wp-config.php ...
  đã hoàn tác
- Hoàn tác: Giải nén WordPress vào /var/www/d.example.com/public ...
    $ runuser -u tp_d_example_com_316ffc -- find /var/www/d.example.com/public -mindepth 1 -delete
  đã hoàn tác
- Hoàn tác: Ghi pool PHP-FPM tpan-d_example_com_316ffc cho d.example.com ...
    $ php-fpm8.3 -t
    | [19-Nov-2026 15:01:50] NOTICE: configuration file /etc/php/8.3/fpm/php-fpm.conf test is successful
    $ systemctl reload php8.3-fpm
  đã hoàn tác
- Hoàn tác: Tạo DB tp_d_example_com_316ffc và user DB riêng của site d.example.com ...
    $ mariadb -N -B
  đã hoàn tác
- Hoàn tác: Tạo /var/log/tpan/sites/d.example.com/php-error.log với đúng quyền và chủ ...
  đã hoàn tác
- Hoàn tác: Tạo thư mục /var/log/tpan/sites/d.example.com ...
  đã hoàn tác
- Hoàn tác: Tạo thư mục /var/www/d.example.com/tmp ...
  đã hoàn tác
- Hoàn tác: Tạo thư mục /var/www/d.example.com/public ...
  đã hoàn tác
- Hoàn tác: Tạo thư mục /var/www/d.example.com ...
  đã hoàn tác
- Hoàn tác: Tạo user Linux tp_d_example_com_316ffc cho site ...
    $ userdel tp_d_example_com_316ffc
  đã hoàn tác

Job b8cb1f72e71f677a: hỏng, đã hoàn tác sạch
Lỗi: bước file:/etc/nginx/sites-available/tpan-d.example.com.conf: thực hiện: ghi /etc/nginx/sites-available/tpan-d.example.com.conf: /etc/nginx/sites-available/tpan-d.example.com.conf là thư mục, bước này chỉ làm việc với file hoặc symlink
```

Lệnh thoát mã 1. Đọc từng chỗ:

1. Bước gây lỗi là bước thứ 12 (ghi vhost). Nó lỗi ngay trong `capture` của `internal/steps/common.go` (chụp trạng thái cũ trước khi ghi), trước khi ghi gì. `Apply` lỗi thì `runStep` trả `applied = false`, nên bước này không nằm trong danh sách cần hoàn tác, và thư mục giả không bị đụng tới.
2. Engine đi ngược danh sách bước đã Apply thành công: 11, 10, 8, 7, 6, 5, 4, 3, 2, 1. Bước 9 (tải WordPress) không có trong danh sách vì nó "đã đúng, bỏ qua", không Apply gì.
3. `WPConfig.Undo` xoá `wp-config.php` bằng `sys.Remove` của Go, nên không có dòng `$`.
4. `WPFiles.Undo` xoá mọi thứ bên trong `public/` bằng `find -mindepth 1 -delete`, chạy dưới user của site. Phải xong bước này thì `public/` mới rỗng: `Dir.Undo` sau đó chỉ xoá thư mục rỗng (`removeDirs` trong `internal/steps/files.go`).
5. Pool là một `File` của M1: Undo xoá file pool vì bước đã tạo nó, kiểm `php-fpm8.3 -t`, rồi reload.
6. `SiteDB.Undo` gửi một đoạn SQL qua một lệnh `mariadb`: `DROP USER IF EXISTS` trước, `DROP DATABASE IF EXISTS` sau (`dropCreated` trong `internal/steps/site_db.go`).
7. Log, thư mục log, `tmp`, `public`, thư mục site lần lượt bị xoá, con trước cha sau. Cuối cùng `userdel` (`SiteUser.Undo` chỉ chạy vì chính bước này đã tạo user).

Kịch bản kiểm lại bằng lệnh của chính Linux, không qua tpan:

```
không có user Linux tp_d_example_com_316ffc (getent mã 2)
không có nhóm tp_d_example_com_316ffc
không có DB tp_d_example_com_316ffc
không có user DB tp_d_example_com_316ffc
không có /var/www/d.example.com
không có /var/log/tpan/sites/d.example.com
không có /etc/php/8.3/fpm/pool.d/tpan-d_example_com_316ffc.conf
không có /run/php/tpan-d_example_com_316ffc.sock
không có /etc/nginx/sites-enabled/tpan-d.example.com.conf
thư mục giả /etc/nginx/sites-available/tpan-d.example.com.conf (do e2e tạo)
không có dòng d.example.com trong bảng sites
```

Sau đó kịch bản `rmdir` thư mục giả, `nginx -t` vẫn đạt, và `--dry-run` cho `d.example.com` không bị từ chối là "site lạ": không còn sót gì để `unmanaged` (mục 3.3) bắt được. Bản xem trước ở mục 2.4 chính là lần chạy này.

Đúng mức về đoạn này: nó chứng minh hoàn tác khi một bước lỗi trong `Apply`. Cùng tình huống đó có unit test với `FakeSystem` (`TestSiteAddFailureRollsBackAndSkipsStore` trong `internal/core/site_test.go`, trường hợp "vhost bị chặn" và "tar lỗi"). Hoàn tác khi một phép kiểm chứng `Fail` đi cùng đường trong `Execute`, nhưng mới được kiểm bằng `TestVerify` trong `internal/engine/engine_test.go` với một bước giả; chưa có test nào cho Plan tạo site với kiểm chứng `Fail`, và chưa chạy trên máy ảo.

### 2.9 Xoá hẳn một site

Kịch bản gọi site `c` một lần cho có worker PHP, in worker đó bằng `ps`, rồi xoá trong lúc worker còn chạy. Phần xoá, chạy trên Mac:

```bash
scripts/vm.sh root tpan site rm c.example.com --yes --verbose
```

Hai dòng đầu của output dưới đây là của `curl` (mã HTTP) và `ps` (worker của pool `c`), chạy ngay trước `site rm` trong cùng một lệnh của kịch bản:

```
c: 200
tp_c_example_com_6e5b26            18797 php-fpm: pool tpan-c_example_com_6e5b26
Bắt đầu job 32543d23534b5c62
- Tắt vhost của c.example.com ...
    $ nginx -t
    | nginx: the configuration file /etc/nginx/nginx.conf syntax is ok
    | nginx: configuration file /etc/nginx/nginx.conf test is successful
    $ systemctl reload nginx
  xong
... (bớt dòng)
- Gỡ pool PHP-FPM tpan-c_example_com_6e5b26 ...
    $ php-fpm8.3 -t
    | [19-Nov-2026 15:01:52] NOTICE: configuration file /etc/php/8.3/fpm/php-fpm.conf test is successful
    $ systemctl reload php8.3-fpm
  xong
- Xoá user DB và DB tp_c_example_com_6e5b26 của site c.example.com ...
    $ mariadb -N -B
    | 1	1
    $ mariadb -N -B
    $ mariadb -N -B
    | 0	0
  xong
- Xoá hẳn /var/www/c.example.com và mọi thứ bên trong ...
    $ rm -r --one-file-system -- /var/www/c.example.com
  xong
- Xoá hẳn /var/log/tpan/sites/c.example.com và mọi thứ bên trong ...
    $ rm -r --one-file-system -- /var/log/tpan/sites/c.example.com
  xong
- Xoá user Linux tp_c_example_com_6e5b26 ...
... (bớt dòng)
    $ userdel tp_c_example_com_6e5b26
    $ getent passwd tp_c_example_com_6e5b26
  xong

Job 32543d23534b5c62: thành công
```

Không có phần "Kiểm chứng": Plan xoá cố ý không có phép kiểm chứng (mục 3.9). `userdel` chạy được dù lúc bắt đầu có worker: reload PHP-FPM sau khi gỡ pool làm worker của pool đó thoát. Mục 16 của KE-HOACH.md ghi rõ mới thử một lần; một request dài hơn có thể làm `userdel` thoát mã 8.

Sau đó: kịch bản thấy "không có" cho mọi thứ của `c` (như danh sách ở 2.8); gọi `c.example.com` ra `curl: (52) Empty reply from server` (rơi vào site mặc định, Nginx đóng kết nối); `a` và `b` vẫn 200; `site list` chỉ còn `a` và `b`; `tpan health` của stack in `10 đạt`.

### 2.10 Apache

Kịch bản đưa máy ảo về sạch, cài stack với `--web apache`, tạo `a` và `b`, chạy lại bước 2 tới 4. Mọi khẳng định đều đạt. Chỗ khác Nginx, chạy trên Mac:

```bash
scripts/vm.sh root ls -l /var/www/a.example.com/public/.htaccess
scripts/vm.sh root apachectl -S
```

```
-rw-r----- 1 tp_a_example_com_7653e4 www-data 696 Nov 19 15:03 /var/www/a.example.com/public/.htaccess
```

```
VirtualHost configuration:
*:80                   is a NameVirtualHost
         default server tpan-default.invalid (/etc/apache2/sites-enabled/000-tpan-default.conf:7)
         port 80 namevhost tpan-default.invalid (/etc/apache2/sites-enabled/000-tpan-default.conf:7)
         port 80 namevhost a.example.com (/etc/apache2/sites-enabled/tpan-a.example.com.conf:5)
         port 80 namevhost b.example.com (/etc/apache2/sites-enabled/tpan-b.example.com.conf:5)
```

`default server` vẫn là `000-tpan-default.conf`, dù `tpan-a.example.com.conf` đã có. Đây là điều M1 đặt tên file `000-` để giữ. Lần chạy lại của Apache: `Số bước: 15, số bước bỏ qua: 15`.

Cuối log, kịch bản tìm 10 mật khẩu đã gặp trong cả lần chạy (5 site Nginx và Apache, mỗi site một mật khẩu DB và một mật khẩu quản trị) trong `tpan.db`, file WAL, bảng `job_events` và chính file log: 0 dòng ở mọi nơi.

### 2.11 Chưa chạy trên máy ảo

Những việc dưới đây có trong mã và unit test nhưng không có trong `M2.log`:

- `tpan site add --json` và trường `admin_password` (lần `--json` duy nhất trong log là lời từ chối ở 2.7). Tự chạy: `scripts/vm.sh root tpan site add e.example.com --email admin@example.com --yes --json`.
- `tpan site rm --dry-run`, và `site rm` không có `--yes` (câu hỏi `Chạy? [y/N]`).
- Các lời từ chối `stack_not_installed`, `title_invalid`, `site_trashed`, `site_exists_unmanaged`, `site_drift`. Chúng được kiểm bằng `TestSiteAddRefusals` trong `internal/core/site_test.go`, với `FakeSystem`.
- `tpan site list --json`, `tpan health <domain> --json`.

## 3. Đi qua mã theo thứ tự chạy

Theo đường đi của `tpan site add a.example.com --email admin@example.com --yes`. Những gì đã có ở M1 (`main`, `run`, `env`, khoá, `recSys`, `File`, `verifyOrRestore`) mình chỉ nhắc tên; đọc lại file M1 mục 3.

### 3.1 `cmd/tpan/site.go`: `siteAddCmd`

`siteCmd` gắn bốn lệnh con: `siteAddCmd`, `siteListCmd`, `siteInfoCmd`, `siteRmCmd`. `RunE` của `siteAddCmd` làm theo thứ tự:

1. `parseDomainArg(args[0])`: gọi `validate.ParseDomain`. Sai thì `asRefusal` đổi `*validate.Error` thành `*core.RefusedError` để `fail` in giống mọi lời từ chối khác. Đây là chỗ chặn domain ở mục 2.7, trước mọi việc khác.
2. `validate.ParseEmail(email)`.
3. `requireRoot`, trừ khi `--dry-run`.
4. `openSites`: dựng `core.Sites`. Khi `--dry-run` mà chưa có file DB thì dùng `core.EmptySettings{}` và `core.EmptySiteStore{}`, không tạo file (cùng lý do với `stack install` ở M1).
5. `s.PrepareAdd(...)`: phần chỉ đọc, dựng Plan (3.3).
6. `f.previewOrConfirm`: in bản xem trước khi `--dry-run` rồi dừng; không có `--yes` thì in xem trước và hỏi `Chạy? [y/N]`; không có terminal hoặc đang `--json` thì từ chối mã `confirm_required`. `planRun` gom cờ và phần này để `site rm` dùng lại.
7. `add.Execute(...)`, rồi `printReport` và `printAdminPassword` (hoặc JSON).

Mật khẩu quản trị chỉ được lấy ra dạng chữ ở hai chỗ, đều trong `cmd/tpan`:

```go
func siteAddJSONOf(res core.SiteAddResult) siteAddJSON {
	out := siteAddJSON{Report: res.Report, Site: res.Site, WPInstalled: res.WPInstalled}
	if res.WPInstalled {
		out.AdminUser, out.AdminPassword = res.AdminUser, res.AdminPassword.Reveal()
	}
	return out
}
```

và `printAdminPassword` trong `cmd/tpan/print.go`. Trong `core`, mật khẩu là `system.Secret`: JSON của nó luôn là `••••`. Quyết định "chữ thật trong `--json`, đúng một lần" ghi ở mục 11 của KE-HOACH.md: script tạo site không có đường nào khác để lấy mật khẩu.

### 3.2 `internal/site/types.go`: `Layout`, mọi tên suy từ domain

`site.LayoutOf(d)` nhận một `validate.Domain` đã kiểm và trả `Layout`: user Linux, tên DB, thư mục site, `public`, `tmp`, `wp-config.php`, thư mục log, `php-error.log`, tên pool, socket, file và symlink vhost của cả hai web server. File pool phụ thuộc bản PHP nên là method riêng, `Layout.PoolConf(version)`.

Gốc của mọi tên là slug, `validate.SlugOf` trong `internal/validate/validate.go`: tối đa 16 ký tự đầu của domain (ký tự ngoài `a-z0-9` thành `_`), cộng `_`, cộng 6 ký tự hex đầu của sha256(domain). `a.example.com` thành `a_example_com_7653e4`, user là `tp_a_example_com_7653e4` (`Slug.LinuxUser`), DB cũng vậy (`Slug.DBName`), pool là `tpan-a_example_com_7653e4`. Phần hash để `a-b.com` và `a.b.com` (cùng ra `a_b_com`) vẫn khác nhau.

Vì sao gom vào một gói: steps, checks, templates, store, core đều cần tên, mà nếu mỗi nơi tự ghép `"tp_" + slug` thì một ngày nào đó hai nơi sẽ lệch nhau. `internal/site` là gói lá, chỉ import `validate`, nên cả `checks` (không được import `core`) lẫn `steps` dùng được. Domain zero cho `Layout` zero, mọi trường rỗng: lỡ quên kiểm domain thì Step từ chối vì đường dẫn rỗng, thay vì làm việc trên `/var/www/`.

Kịch bản e2e tự tính slug bằng bash (hàm `slug_of` trong `scripts/e2e-m2.sh`) và so với `site info --json`: `=> ĐẠT: slug của a.example.com đúng quy tắc mục 5`. Hai cách tính độc lập ra cùng kết quả.

### 3.3 `internal/core/site.go`: `PrepareAdd`

`PrepareAdd` chỉ đọc máy và DB, không ghi gì. Các lời từ chối theo đúng thứ tự trong hàm (mã ở cột trái là `Code` của `RefusedError`):

| Mã | Khi nào |
|---|---|
| `domain_invalid`, `email_invalid` | thiếu domain hay email (CLI đã kiểm, đây là lớp chặn thứ hai cho API ở M3) |
| `title_invalid` | `checkSiteTitle`: tiêu đề rỗng, dài quá 200 byte, không phải UTF-8, hoặc có ký tự điều khiển |
| `stack_not_installed` | `Sites.stack`: chưa có `stack.web` hoặc `stack.php_version` |
| `site_trashed` | domain có trong bảng `sites` với trạng thái `trashed` (để dành cho M6) |
| `site_drift` | chạy lại, mà user DB và `wp-config.php` không cùng có hay cùng không (`Sites.drift`) |
| `site_exists_unmanaged` | domain chưa có trong bảng `sites` mà máy đã có thư mục site, user Linux, DB hay user DB của nó (`Sites.unmanaged`) |

Hai nhánh chính, theo việc domain đã có trong bảng `sites` chưa:

- Đã có (`found`): đây là lần chạy lại. Giữ bản ghi cũ (ngày tạo, bản PHP), rồi `drift`. Lý do có `drift`: mỗi lần chạy sinh mật khẩu DB mới. Nếu user DB còn mà `wp-config.php` mất, bước `WPConfig` sẽ ghi mật khẩu mới mà user DB không biết; ngược lại, `SiteDB` sẽ tạo user với mật khẩu mà `wp-config.php` không biết. Cả hai đều ra site hỏng, nên từ chối và chỉ cách sửa tay.
- Chưa có: `unmanaged` hỏi máy (`pathExists`, `linuxUserExists` bằng `getent passwd`, `steps.SiteDBState`). Có bất cứ thứ gì thì từ chối, vì đó là dữ liệu tpan không tạo; nhận làm của mình rồi hoàn tác là xoá mất dữ liệu người khác. File vhost và pool không được xét: tpan tự ghi lại được, và `File` giữ bản cũ để hoàn tác.

Sau đó sinh hai mật khẩu bằng `sitePassword`, lấy danh sách site khác (`others`), dựng Plan bằng `addPlan`, gắn `plan.Verify = siteChecks(...)`.

```go
	const limit = 256 - 256%len(sitePasswordChars)
	out := make([]byte, 0, sitePasswordLen)
	buf := make([]byte, sitePasswordLen*2)
	for len(out) < sitePasswordLen {
		if _, err := rand.Read(buf); err != nil {
			return system.Secret{}, fmt.Errorf("sinh mật khẩu: %w", err)
		}
		for _, b := range buf {
			if int(b) >= limit {
				continue
			}
			out = append(out, sitePasswordChars[int(b)%len(sitePasswordChars)])
```

32 ký tự, chỉ chữ và số (62 ký tự), lấy từ `crypto/rand`. `limit` là 248: byte từ 248 tới 255 bị bỏ, vì 248 = 4 × 62 nên mỗi ký tự có đúng 4 giá trị byte dẫn tới nó. Nếu lấy thẳng `b % 62` cho mọi byte thì 8 ký tự đầu bảng có 5 cơ hội, các ký tự khác chỉ 4. Chỉ chữ và số để mật khẩu đặt được trong `'...'` của SQL và đi qua stdin của WP-CLI mà không phải thoát ký tự nào.

### 3.4 `addPlan`: thứ tự bước

```go
	// Bước 2, 3, 4: user Linux, thư mục và log, DB.
	ss := []engine.Step{&steps.SiteUser{Layout: l}}
	ss = append(ss, steps.SiteDirs(l)...)
	ss = append(ss, &steps.SiteDB{Layout: l, Password: dbPw})

	// Bước 5: pool PHP-FPM.
	pool, err := steps.SitePool(l, php)
	if err != nil {
		return engine.Plan{}, wrap(err)
	}
	// Bước 6, 7: WordPress từ cache, rồi wp-config.php.
	archive := &steps.WPArchive{Version: wpVersion, Hash: wpHash}
	ss = append(ss, pool, archive,
		&steps.WPFiles{Layout: l, Archive: archive.Path()},
		&steps.WPConfig{Layout: l, DBPassword: dbPw},
	)
```

Phần còn lại: với Apache thêm `steps.SiteHtaccess` (phải có trước khi vhost được bật), rồi `steps.SiteVhost` (file vhost và symlink), cuối cùng bước cài WordPress. Số bước "Bước 2" tới "Bước 9" trong chú thích là số bước ở mục 6.2 của KE-HOACH.md.

Vì sao thứ tự này:

- User trước thư mục: thư mục phải có chủ là user đó.
- DB trước `wp-config.php`: cùng một `dbPw` đi vào cả hai. Đây là chỗ duy nhất mật khẩu DB được sinh ra; nó không được lưu ở đâu khác.
- Pool trước vhost: vhost trỏ vào socket của pool. Bật vhost khi chưa có pool là khách thấy 502.
- Cài WordPress sau vhost: `wp core install` ghi URL của site vào DB, nên lúc đó site nên trả lời được rồi.

Bước cài WordPress không phải `steps.WPInstall` trần mà được bọc trong `wpInstallStep` (cùng file `internal/core/site.go`). Lý do ở 3.6.

### 3.5 Các Step của site

Mọi Step ở đây kiểm lại `Layout` trước khi chạy lệnh nào (`userCheckLayout`, `dbCheckLayout`, `webCheckLayout`, `wpCheckLayout`): Layout zero hay tên lạ thì `Check` và `Apply` đều lỗi. Đây là lớp chặn cuối, kể cả khi ai đó dựng `Layout` bằng tay.

`internal/steps/site_user.go`

- `SiteUser`: `useradd --system --user-group --home-dir <Dir> --no-create-home --shell /usr/sbin/nologin <User>`. `--user-group` tạo nhóm riêng cùng tên làm nhóm chính, nên user không thuộc `www-data`. `--no-create-home` vì thư mục site do `SiteDirs` tạo với quyền 2750, không để `useradd` tạo theo mặc định của nó. Không có cờ mật khẩu: tài khoản không có mật khẩu, cộng shell `nologin` là không đăng nhập được. User cùng tên đã có mà home hay shell khác thì `userOurs` báo lỗi: không phải user của tpan, không sửa, không xoá. `Undo` chỉ chạy `userdel` khi chính bước này đã tạo (`created`).
- `SiteDirs`: trả 5 bước theo mục 5 của KE-HOACH.md, dùng lại `Dir` của M1 cộng một `OwnedFile`. Thư mục site và `public` là `fs.ModeSetgid | 0o750` (tức 2750), chủ `<User>:www-data`; `tmp` là 0700; thư mục log là 0755 `root:root`; `php-error.log` là 0640 thuộc user của site.
- `OwnedFile`: đảm bảo file có, đúng quyền và chủ, không quan tâm nội dung. Không dùng `File` của M1 vì `File` so nội dung, sẽ ghi đè log mỗi lần chạy lại. `Info` của nó cố ý không khai `Files`: engine ghi sha256 của file đã ghi vào nhật ký để M4 phát hiện file bị sửa tay, mà log thì đổi liên tục. Ở đó cũng không phải file thường (ví dụ một symlink) thì từ chối: `Chmod` đi theo symlink, root chmod qua một symlink do user của site đặt có thể đổi quyền của `/etc/shadow`.
- `RemoveTree` (dùng khi xoá): `rm -r --one-file-system -- <Path>`, với `Path` phải nằm hẳn dưới `Under` (`/var/www` hoặc `/var/log/tpan/sites`).
- `SiteUserAbsent` (dùng khi xoá): `userdel` không có `-r` (thư mục do `RemoveTree` xoá, có kiểm đường dẫn) và không có `-f` (không xoá user đang có tiến trình). Mã 8 của `userdel` được dịch thành lời nhắc "chờ vài giây rồi chạy lại".

`internal/steps/site_db.go`

- `SiteDB.Check`: `SiteDBState` chạy một câu SELECT (`dbStateSQL`) trả hai số: DB có không, user DB có không. So bằng `=`, không dùng `LIKE`, vì trong `LIKE` dấu `_` là ký tự đại diện.
- `SiteDB.Apply` chạy ba lệnh `mariadb` riêng, mỗi cờ chỉ bật khi lệnh của nó xong:

```go
	if !dbOK {
		if err := dbRun(ctx, sys, dbCreateDBSQL(name), "", "tạo DB "+name); err != nil {
			return err
		}
		s.createdDB = true
	}
	if !userOK {
		if err := dbRun(ctx, sys, dbCreateUserSQL(name, pw), pw, "tạo user DB "+name); err != nil {
			return s.rollback(ctx, sys, err)
		}
		s.createdUser = true
		if err := dbRun(ctx, sys, dbGrantSQL(name), "", "cấp quyền trên DB "+name+" cho user "+name); err != nil {
			return s.rollback(ctx, sys, err)
		}
	}
```

  Lệnh sau lỗi thì `rollback` xoá đúng phần vừa tạo rồi mới trả lỗi, vì engine không gọi `Undo` cho bước có `Apply` lỗi (luật ở đầu `internal/steps/common.go`, xem file M1 mục 3.7).
- Câu tạo không có `IF NOT EXISTS`, cố ý: nếu DB xuất hiện giữa lúc đọc trạng thái và lúc tạo, câu lệnh phải lỗi. Không thì bước tưởng mình đã tạo và `Undo` sẽ xoá mất thứ không phải của nó. Câu xoá thì có `IF EXISTS`, để chạy lại sau một lần hỏng vẫn được.
- `dbGrantSQL` đổi `_` thành `\_` trong tên DB (mục 4).
- Mật khẩu chỉ đi trong `Cmd.Stdin`. `Info` dùng `dbMask` thay chỗ mật khẩu. `dbRedact` lọc mật khẩu khỏi lỗi trả về, vì lỗi cú pháp của MariaDB chép lại đoạn SQL quanh chỗ sai. `SiteDB.validate` còn chặn thêm: mật khẩu phải khớp `^[A-Za-z0-9]{24,}$` trước khi được đặt vào chuỗi SQL.
- `SiteDBAbsent` (dùng khi xoá): `DROP USER IF EXISTS` rồi `DROP DATABASE IF EXISTS`.

`internal/steps/site_web.go`

Không có kiểu Step mới. Mỗi hàm dựng `File`, `Symlink`, `Absent` của M1 với nội dung từ `internal/templates` và đúng lệnh kiểm, lệnh reload. Nhờ vậy việc giữ file cũ, kiểm cú pháp, trả file cũ khi kiểm sai và `Undo` đã có sẵn và đã có test từ M1.

- `SitePool`: `File` ở `l.PoolConf(v)`, quyền 0644 root, `Test` là `php-fpm8.3 -t`, `Reload` là `systemctl reload php8.3-fpm`. Bản PHP phải khớp `phpVersion` (dạng `8.3`) trước khi được ghép vào tên chương trình (`webPHPCmds`).
- `SiteVhost`: `File` cho vhost ở `sites-available`, rồi `Symlink` ở `sites-enabled`. Cả hai có `Test` (`nginx -t` hoặc `apachectl configtest`) và `Reload`.
- `SiteHtaccess` (chỉ Apache): `.htaccess` chuẩn của WordPress, 0640, `<User>:www-data`. Apache đọc `.htaccess` ở mỗi request nên không cần reload.
- `SiteWebAbsent` (dùng khi xoá): gỡ symlink vhost, gỡ file vhost, rồi gỡ pool. Vhost gỡ trước để không lúc nào web server trỏ vào socket của một pool đã mất.

Template ở `internal/templates/files/site-pool.conf.tmpl`, `site-nginx.conf.tmpl`, `site-apache.conf.tmpl`, `site-htaccess.tmpl`. Dòng đầu của file pool là `"; " + Header` (`sitePoolComment` trong `internal/templates/site.go`), lý do ở mục 5.

`internal/steps/site_wp.go`

- `WPArchive`: tải `wordpress-7.1.5.tar.gz` vào `/var/cache/tpan` bằng `curl`, so SHA-1 bằng Go với hằng `DefaultWordPressSHA1`. Không khớp thì trả cache về như cũ (`prev.restore`) và báo lỗi: file hỏng không bao giờ được giải nén vào site. Đã có trong cache và SHA-1 vẫn khớp thì chỉ sửa quyền. `Undo` không xoá cache. SHA-1 vì wordpress.org chỉ công bố `.md5` và `.sha1` cho tarball; các đường `.sha256` trên wordpress.org trả 200 nhưng nội dung là chính tarball (chú thích trên hằng `DefaultWordPressVersion`). Tải qua HTTPS nên SHA-1 là lớp kiểm thứ hai. Cách này giữ bản WordPress cố định (không tự nâng lên bản mới nhất lúc tạo site), và site thứ hai trở đi không tải lại (mục 2.4, bước 9 "đã đúng").
- `WPFiles`: chỉ giải nén vào `public/` rỗng (`ReadDir` trước), vì `Undo` xoá mọi thứ bên trong `public/`; có sẵn file lạ thì `Undo` sẽ xoá mất. `tar` chạy dưới user của site, nên mọi file thuộc user đó ngay từ đầu. Đặt quyền bằng hai lệnh `find`: thư mục 2750 chạy bằng root, file 0640 chạy dưới user của site. Lý do tách đôi ở mục 4 ("setgid").
- `WPConfig`:

```go
	create = wpRun(l, []byte(s.DBPassword.Reveal()+"\n"),
		"config", "create", "--dbname="+l.DB, "--dbuser="+l.DB, "--dbhost=localhost",
		"--dbcharset=utf8mb4", "--prompt=dbpass", "--quiet")
	// Tắt trình sửa theme và plugin trong trang quản trị: lấy được tài khoản
	// quản trị thì cũng không sửa được mã PHP từ trình duyệt.
	set = wpRun(l, nil, "config", "set", "DISALLOW_FILE_EDIT", "true", "--raw", "--type=constant", "--quiet")
```

  `wpRun` đặt `AsUser: l.User` và thêm `--path=`. Không bao giờ có `--allow-root`: root không có việc gì phải nạp mã PHP của site. File đã có thì không tạo lại (`config create --force` sẽ ghi một mật khẩu user DB không biết), chỉ sửa quyền về 600. `chmod` cũng chạy dưới user của site (`wpChmod`).
- `WPInstall`: `Check` là "đã cài VÀ permalink đúng" (`wp core is-installed`, `wp option get permalink_structure`). `Apply` chạy `wp core install` với `--prompt=admin_password --skip-email --quiet` khi chưa cài, rồi `wp rewrite structure /%postname%/` khi permalink lệch. `Undo` không làm gì: bảng của WordPress nằm trong DB, bước `SiteDB` hoàn tác thì mất theo.

### 3.6 `wpInstallStep`: khi nào mới in mật khẩu quản trị

Engine báo một bước "applied" khi `Apply` chạy. Nhưng `WPInstall.Apply` có thể chỉ đặt lại permalink trên một WordPress đã cài sẵn; lúc đó mật khẩu quản trị mới chưa bao giờ được dùng, in nó ra là in một mật khẩu sai. `wpInstallStep` trong `internal/core/site.go` bọc bước gốc, hỏi `wp core is-installed` ngay trước khi giao cho bước gốc, và chỉ đặt `ran = true` khi lúc đó WordPress chưa cài (mã thoát 1). `SiteAdd.Execute` chỉ trả `AdminPassword` khi `install.ran`. Unit test: `TestSiteAddPermalinkOnlyIsNotInstall`.

Đây là lý do log ở mục 2.2 có ba lần `wp core is-installed` trước `wp core install`: `Check` của bước, `wpInstallStep.Apply`, rồi `WPInstall.Apply` tự hỏi lại.

### 3.7 Engine: xem trước, hoàn tác ngược, che stdin

`internal/engine/engine.go` không đổi cách chạy so với M1: `Preview` gọi `Info` và `Check`, không `Apply`; `Execute` lấy khoá, chạy từng bước bằng `runStep`, có lỗi thì đi ngược danh sách bước đã `Apply` thành công và gọi `undoStep` (mục 2.8 là ví dụ thật), xong thì chạy `verify`.

Thứ M2 thêm vào là che dòng stdin trong log job (commit `a4ee4b0`):

```go
func (s recSys) Run(ctx context.Context, cmd system.Cmd) (system.Result, error) {
	// Cmd.String() không in Stdin, nên mật khẩu đưa qua stdin không lọt vào log.
	s.r.emit(EventCommand, s.stepID, cmd.String())
	// Step đã đặt OnOutput riêng thì vẫn gọi nó, sau khi ghi sự kiện.
	own := cmd.OnOutput
	secrets := stdinLines(cmd.Stdin)
	cmd.OnOutput = func(line string) {
		s.r.emit(EventOutput, s.stepID, maskLines(line, secrets))
		if own != nil {
			own(line)
		}
	}
	return s.System.Run(ctx, cmd)
}
```

`stdinLines` tách stdin thành các dòng khác rỗng; `maskLines` thay mỗi dòng đó, nếu nó xuất hiện trong một dòng output, bằng `••••`. Cần vì WP-CLI, sau khi hỏi qua `--prompt`, in lại cả câu lệnh kèm giá trị vừa đọc (mục 16 của KE-HOACH.md). `OnOutput` nhận cả stdout lẫn stderr (`internal/system/local.go`), nên mọi dòng đi vào bảng `job_events` đều qua đây.

Che mật khẩu thực ra có ba lớp: `--quiet` để WP-CLI không in lại; engine che dòng stdin trong sự kiện output; và ở Step, `wpHide` (site_wp.go) cùng `dbRedact` (site_db.go) lọc mật khẩu khỏi lỗi trả về. E2e đếm được 0 mật khẩu trong `tpan.db` và trong log (mục 2.10). Lớp thứ hai lúc đầu chỉ che nguyên dòng stdin. Với WP-CLI, dòng stdin chính là mật khẩu nên khớp. Với `SiteDB`, dòng stdin là cả câu `CREATE USER ... IDENTIFIED BY '...'`; nếu MariaDB in ra chỉ một đoạn của câu đó (`near '...'`) thì cả dòng không khớp. Vì vậy `stdinLines` còn tách thêm mọi chuỗi chữ-số dài từ 20 ký tự trong stdin (biến `longToken`) để che riêng: mật khẩu tpan sinh là 32 ký tự chữ-số, còn tên DB, tên user có dấu `_` nên bị cắt ngắn và không bị che nhầm. Test: `TestOutputMasksPasswordInsideSQL` trong `internal/engine/engine_test.go`.

### 3.8 `internal/checks/site.go`: kiểm chứng

`SiteChecks(web, phpVersion, l)` trả 7 phép kiểm theo đường đi của request (mục 6.7 của KE-HOACH.md): `VhostEnabled`, `PHPFPMConfig`, `PoolSocket`, `WPChecksums`, `WPConfigPerm`, `DBAccess`, `HTTPSite`. `siteChecks` trong `internal/core/site.go` thêm `checks.Isolated(l, o)` và `checks.Isolated(o, l)` cho mỗi site `o` còn lại. Cùng một bộ dùng cho kiểm chứng sau khi tạo, cho `tpan health <domain>` (`Sites.Health`) và cho phần "Kiểm tra" của `site info`.

Ba phép kiểm đáng nói:

- `Isolated(from, to)`: dưới user của `from`, chạy `test -r <wp-config.php của to>`. Đạt khi mã 1 và stderr rỗng. Hai bẫy đã được tính: `runuser` cũng thoát mã 1 khi user không tồn tại (nhưng có dòng lỗi ở stderr), và `test -r` cũng trả 1 khi file không có. Nên phép kiểm `Stat` file của site kia trước; không có file thì kết quả là `Unknown`, không phải `Pass`, vì "không đọc được thứ không tồn tại" không chứng minh gì.
- `DBAccess`: chạy `wp db query "SHOW DATABASES"` dưới user của site. WP-CLI tự đọc mật khẩu trong `wp-config.php`, nên mật khẩu không đi qua tpan. Đạt khi thấy DB của site và không thấy DB nào khác ngoài `information_schema`.
- `HTTPSite`: `curl --resolve <domain>:80:127.0.0.1 http://<domain>/`, nên không phụ thuộc DNS. Đạt khi mã 200, thân có dấu hiệu WordPress, và có chính domain đứng riêng như một tên máy (biểu thức chính quy `domRE` chặn trường hợp trang của `myshop.example.com` chứa chuỗi `shop.example.com`). Curl thoát mã 52 nghĩa là request rơi vào site mặc định của Nginx: lỗi, kèm gợi ý kiểm vhost.

Mỗi kết quả không đạt có `Cause` và `ManualFix` (lệnh để người vận hành tự gõ), giống M1.

### 3.9 Kết thúc: ghi bảng `sites`, và luồng xoá

`SiteAdd.Execute` chạy engine. Job không thành công thì không ghi gì vào bảng `sites`. Job thành công thì đặt `Created` (giữ ngày cũ khi chạy lại) rồi `Store.SaveSite`. Ghi lỗi thì vẫn trả kết quả kèm mật khẩu quản trị, vì mất nó là mất tài khoản quản trị (`TestSiteAddSaveFailsStillReturnsPassword`).

`internal/store/sites.go` cài `core.SiteStore`: `Site`, `Sites`, `SaveSite`, `DeleteSite`. Bảng tạo ở `internal/store/migrations/002_sites.sql`, không có cột mật khẩu nào; `slug` có `UNIQUE` để hai domain lỡ trùng slug bị chặn ngay ở DB. `SaveSite` là `INSERT ... ON CONFLICT (domain) DO UPDATE`, và luôn tự tính `slug` từ domain, bỏ qua trường `Slug` người gọi đưa vào. `siteScan` đọc lại cũng không tin DB: domain đi lại qua `validate.ParseDomain`, slug suy lại từ domain.

Luồng xoá, `Sites.PrepareRemove`: `find` (không có trong bảng thì `site_not_found`), rồi Plan gồm `SiteWebAbsent` (3 bước), `SiteDBAbsent`, hai `RemoveTree`, `SiteUserAbsent`. Gỡ vhost và pool trước vì chúng hoàn tác được; DB, file, user sau vì không hoàn tác được. Plan này không có `Verify`: nếu một phép kiểm chứng `Fail`, engine sẽ hoàn tác, tức trả lại vhost và pool cho một site đã mất DB và file, tệ hơn để yên. `SiteRemove.Execute` chỉ xoá dòng trong bảng `sites` khi job thành công; hỏng giữa chừng thì giữ dòng để `site rm` chạy lại làm nốt.

## 4. Kiến thức nền

Mỗi khái niệm: nó là gì, chỗ trong mã dùng nó, và một lệnh để tự nhìn thấy trên máy ảo. Lệnh gõ trên Mac qua `scripts/vm.sh`. Lệnh nào ghi "chưa chạy" là mình chưa chạy, bạn tự chạy sau khi tạo site.

### User và nhóm Linux

Mỗi tiến trình chạy dưới một user (UID) và một nhóm chính (GID), cộng các nhóm phụ. Quyền đọc ghi file được quyết định bởi ba thứ đó. `useradd --system` tạo user trong dải UID hệ thống (dưới 1000), `--user-group` tạo nhóm riêng cùng tên. Shell `/usr/sbin/nologin` làm mọi cách đăng nhập bị từ chối.

Trong mã: `SiteUser.useradd` trong `internal/steps/site_user.go`; `userLookup` đọc 7 trường của dòng `passwd`.

Tự xem: dòng `getent passwd tp_a_example_com_7653e4` ở mục 2.2 ra `tp_a_example_com_7653e4:x:999:988::/var/www/a.example.com:/usr/sbin/nologin` (UID 999, GID 988, home, shell). Xem nhóm của user: `scripts/vm.sh exec id tp_a_example_com_7653e4`, phải không có `www-data` (chưa chạy).

### Quyền 2750, 640, 600 và bit setgid

Quyền ghi bằng số bát phân, mỗi chữ số cho một đối tượng: chủ, nhóm, người khác. 4 là đọc, 2 là ghi, 1 là `x`. Với thư mục, `x` là "được đi vào", thiếu nó thì không mở được bất cứ thứ gì bên trong, kể cả biết tên. Chữ số thứ tư đứng đầu là bit đặc biệt: 2 là setgid. Trên thư mục, setgid làm file mới tạo bên trong mang nhóm của thư mục, thay vì nhóm chính của người tạo.

Vì vậy `2750 tp_a:www-data` nghĩa là: chủ đủ quyền; `www-data` (web server) đọc và đi vào được; người khác không làm gì được; file mới sinh ra (ảnh tải lên) mang nhóm `www-data` để web server phục vụ được. File thường 640: chủ đọc ghi, nhóm đọc. `wp-config.php` 600: chỉ chủ.

Trong mã: `SiteDirs` (`fs.ModeSetgid | 0o750`); `WPFiles.cmds`. Một bẫy của Linux nằm ở đây: khi người chạy `chmod` không phải root và không thuộc nhóm của file, kernel tự xoá bit setgid. User của site cố ý không thuộc `www-data`, nên `chmod 2750` cho thư mục phải chạy bằng root. File 0640 thì chạy dưới user của site, vì không cần setgid và không cần root.

Tự xem: output `stat` ở mục 2.5. Dòng `600 tp_a_example_com_7653e4:www-data .../wp-config.php` là setgid đang làm việc: WP-CLI chạy dưới `tp_a`, có nhóm chính `tp_a`, nhưng file nó tạo lại mang nhóm `www-data`. Dù vậy `www-data` vẫn không đọc được vì phần quyền của nhóm là 0.

### `runuser`

`runuser -u <user> -- <lệnh>` chạy một lệnh dưới user khác. Chỉ root dùng được, không hỏi mật khẩu, không cần user đích có shell đăng nhập. `--` để tên lệnh không bị hiểu là cờ của `runuser`. Đổi `HOME`, `USER`, `LOGNAME` sang user đích (đã kiểm ở M1, mục 16).

Trong mã: trường `AsUser` của `system.Cmd`; `runArgv` trong `internal/system/local.go` bọc lệnh thành `runuser -u <user> -- <tên> <tham số...>`. `LocalSystem.Run` đặt thư mục làm việc là `/` để user của site đọc được thư mục hiện tại.

Tự xem: mọi dòng `$ runuser -u tp_a_example_com_7653e4 -- ...` ở mục 2.2; và `scripts/vm.sh root runuser -u tp_a_example_com_7653e4 -- id` (chưa chạy).

### Pool PHP-FPM và Unix socket

PHP-FPM có một tiến trình master chạy bằng root và nhiều pool; mỗi pool là một nhóm worker chạy dưới user riêng, nghe ở một địa chỉ riêng. Unix socket là một file đặc biệt dùng làm địa chỉ nói chuyện giữa hai tiến trình trong cùng máy, không qua mạng. Ai được nói chuyện với pool do quyền của file socket quyết định: `0660 www-data:www-data` nghĩa là chỉ web server (và root) kết nối được.

Trong mã: `internal/templates/files/site-pool.conf.tmpl` (`user`, `group`, `listen`, `listen.owner`, `listen.mode = 0660`, `pm = ondemand`); vhost Nginx có `fastcgi_pass unix:<socket>`, Apache có `SetHandler "proxy:unix:<socket>|fcgi://localhost"`; phép kiểm `PoolSocket`.

Tự xem: `ps` ở mục 2.5 (mỗi pool một user); dòng `660 www-data:www-data /run/php/tpan-a_example_com_7653e4.sock` ở `stat`. Xem socket: `scripts/vm.sh exec ls -l /run/php/` (chưa chạy).

### Vhost Nginx, Apache và `default_server`

Một vhost là một khối cấu hình cho một tên miền: lấy file ở đâu, chuyển `.php` cho ai, log ghi vào đâu. Web server chọn vhost theo header `Host` của request. Vhost của site không có `default_server` (Nginx) và không đứng đầu thứ tự nạp (Apache), nên request mang tên lạ vẫn rơi vào site mặc định của M1.

Trong mã: `site-nginx.conf.tmpl` (chú thích "Không có default_server"; `try_files $uri $uri/ /index.php?$args` cho permalink; chặn file ẩn và PHP trong `wp-content/uploads`); `site-apache.conf.tmpl`; `SiteVhost`.

Tự xem: `apachectl -S` ở mục 2.10; `curl` tới `c.example.com` sau khi xoá ra `curl: (52)` ở mục 2.9. Với Nginx: `scripts/vm.sh root nginx -T` in toàn bộ cấu hình đang nạp (chưa chạy).

### MariaDB: `user@localhost` và GRANT

Một tài khoản MariaDB là cặp tên và máy: `'tp_a'@'localhost'` chỉ đăng nhập được từ chính máy này. Tài khoản tạo ra không có quyền gì (`USAGE`) cho tới khi được `GRANT`. ``GRANT ALL PRIVILEGES ON `db`.*`` cấp mọi quyền trên mọi bảng của đúng một DB.

Trong mã: `dbCreateUserSQL`, `dbGrantSQL` trong `internal/steps/site_db.go`; phép kiểm `DBAccess`.

Tự xem: `SHOW GRANTS` và thử `USE` chéo ở mục 2.5.

### GRANT và dấu gạch dưới

Trong tên DB của câu GRANT, `_` và `%` là ký tự đại diện, giống trong `LIKE`: `_` khớp đúng một ký tự bất kỳ. Không thoát thì `` `tp_shop_vn_3fa1c2`.* `` cũng khớp một DB tên `tpXshop...` nếu có. Thoát thành `\_` thì quyền chỉ áp cho đúng tên đó. Trang GRANT của MariaDB nói có ký tự đại diện mà không nói cách thoát; cách thoát lấy từ tài liệu MySQL và đã kiểm trên máy ảo (mục 16 của KE-HOACH.md).

Trong mã: ``strings.ReplaceAll(name, "_", `\_`)`` trong `dbGrantSQL`. Ngược lại, `dbStateSQL` so tên bằng `=` để `_` không phải ký tự đại diện.

Tự xem: dòng `` GRANT ALL PRIVILEGES ON `tp\_a\_example\_com\_7653e4`.* `` ở mục 2.5.

### WP-CLI và `--prompt`

WP-CLI là chương trình dòng lệnh để làm mọi việc của WordPress (tạo `wp-config.php`, cài, đổi cài đặt) mà không qua trình duyệt. `--prompt=dbpass` bảo WP-CLI không lấy tham số `dbpass` từ dòng lệnh mà hỏi; khi stdin không phải terminal, nó đọc một dòng từ stdin làm câu trả lời. Sau khi hỏi, WP-CLI in lại cả câu lệnh kèm giá trị vừa đọc, trừ khi có `--quiet`.

Trong mã: `WPConfig.cmds`, `WPInstall.cmds` trong `internal/steps/site_wp.go` (stdin là `mật khẩu + "\n"`); `wpCheckSecret` từ chối mật khẩu có ký tự điều khiển (xuống dòng sẽ thành câu trả lời thứ hai) hay dài quá 1023 byte (WP-CLI cắt mà không báo).

Tự xem: lần đăng nhập 302 ở mục 2.2 là bằng chứng mật khẩu đi qua stdin đúng. Đọc lại tên DB mà không lộ mật khẩu: `scripts/vm.sh root runuser -u tp_a_example_com_7653e4 -- wp config get DB_NAME --path=/var/www/a.example.com/public` (chưa chạy).

### Vì sao mật khẩu không được nằm trên dòng lệnh

Mọi user trên máy đọc được dòng lệnh của mọi tiến trình qua `ps` hoặc `/proc/<pid>/cmdline`. Một lệnh `wp config create --dbpass=abc` chạy vài giây là đủ để PHP của một site khác (đang bị chiếm) đọc được mật khẩu. Thêm nữa, engine ghi `Cmd.String()` vào bảng `job_events`; `Cmd.String()` không in `Stdin`.

Trong mã: `Cmd.Stdin` ở `SiteDB`, `WPConfig`, `WPInstall`; `recSys.Run` (3.7).

Tự xem: `scripts/vm.sh exec ps -eo user,args` cho thấy cả dòng lệnh của tiến trình root, chạy dưới user thường (chưa chạy).

### `open_basedir`

`open_basedir` là cài đặt của PHP: hàm mở file của PHP (`fopen`, `include`, `file_get_contents`) từ chối mọi đường dẫn ngoài danh sách cho phép. Nó là lớp thứ hai sau quyền Linux, không thay được quyền Linux: mã PHP gọi chương trình ngoài (nếu được phép) thì không bị nó chặn. Dấu `/` ở cuối là bắt buộc: `/var/www/a.com` không có `/` còn khớp cả `/var/www/a.com.evil`.

Trong mã: `php_admin_value[open_basedir] = {{.Dir}}/` trong `site-pool.conf.tmpl`. `php_admin_value` (không phải `php_value`) để mã của site không đổi lại được bằng `ini_set`. `error_log` nằm ngoài `open_basedir` mà PHP vẫn ghi được, vì file đó do PHP-FPM mở chứ không qua kiểm `open_basedir` (mục 16, M2.log dòng 2187 tới 2197).

Tự xem: `scripts/vm.sh root grep open_basedir /etc/php/8.3/fpm/pool.d/tpan-a_example_com_7653e4.conf` (chưa chạy).

### Idempotent

Idempotent nghĩa là chạy một lần hay mười lần thì máy cũng ở cùng một trạng thái. tpan làm được vậy nhờ mỗi Step có `Check` hỏi "máy đã đúng chưa" trước `Apply`; đúng rồi thì bỏ qua. Vì vậy chạy lại `site add` cũng là cách sửa chỗ lệch.

Trong mã: `runStep` trong `internal/engine/engine.go`; mỗi `Check` trong các file `site_*.go`. Chỗ không idempotent theo nghĩa thông thường là mật khẩu: mỗi lần chạy sinh mật khẩu mới, nên `drift` từ chối thay vì ghi một nửa (3.3).

Tự xem: mục 2.6. Hoặc tự làm lệch rồi chạy lại: `scripts/vm.sh root chmod 644 /var/www/a.example.com/public/wp-config.php`, rồi `scripts/vm.sh root tpan site add a.example.com --email admin@example.com --yes`. Theo mã, chỉ bước `wp.config` chạy và đặt lại 600 (chưa chạy).

## 5. Cách khác đã cân nhắc, và chỗ đã làm sai

### Mọi site dùng chung một pool PHP-FPM

Gói `php-fpm` có sẵn pool `www`, chạy dưới `www-data`, nghe ở `/run/php/php8.3-fpm.sock`. Cho mọi vhost trỏ vào đó là ít việc nhất: không cần user riêng, không cần file pool. Không chọn: PHP của mọi site chạy chung một user, nên một plugin lỗi ở site A đọc được `wp-config.php` (mật khẩu DB) của mọi site. Đó cũng chính là user web server, nên `wp-config.php` không thể vừa 600 vừa đọc được. Lý do này ghi trong `Why` của `SitePool`. Giá phải trả: mỗi site một file pool và một nhóm worker; `pm = ondemand` giữ cho site không có khách không tốn RAM (đo trên máy ảo khoảng 50 MB mỗi worker, mục 16).

### Mật khẩu trên dòng lệnh

`wp config create --dbpass=...` và `wp core install --admin_password=...` là cách tài liệu WP-CLI hay dùng nhất. Không chọn vì lý do ở mục 4 (`ps`, log job). Chọn `--prompt` và stdin.

Chỗ phát hiện ra khi làm: WP-CLI 2.12.0, sau khi hỏi qua `--prompt`, in lại cả câu lệnh kèm mật khẩu vừa đọc (mục 16 của KE-HOACH.md). Tức là chuyển sang stdin chưa đủ, mật khẩu vẫn quay lại output và đi vào `job_events`. Sửa bằng hai lớp: `--quiet` trên mọi lệnh `wp` có `--prompt`, và engine che các dòng stdin trong output (commit `a4ee4b0`, `stdinLines`, `maskLines`). E2e kiểm bằng cách tìm từng mật khẩu thật trong `tpan.db` và log.

### Xoá site vào thùng rác ngay ở M2

Mục 6.4 của KE-HOACH.md mô tả xoá là đưa vào thùng rác: khoá user DB, khoá user Linux, chuyển thư mục vào `/var/lib/tpan/trash/`, hết hạn mới xoá hẳn. Không làm ở M2: cần thêm luồng khôi phục, bộ hẹn giờ dọn thùng rác, trạng thái `trashed` trong mọi chỗ đọc bảng `sites`. M2 chỉ cần xoá được để e2e dọn được site và để thử đường xoá, nên `site rm` xoá hẳn, có hỏi lại và `--dry-run`. M6 đổi `site rm` thành đưa vào thùng rác và dùng lại Plan này cho `trash purge`. Mã đã chừa chỗ: `site.Trashed`, cột `trashed_at`, lời từ chối `site_trashed`.

Một quyết định đi kèm: Plan xoá không có bước kiểm chứng. Đặc tả lúc đầu không nói gì về việc này; khi viết core (commit `e9282d0`), `PrepareRemove` được viết không có `Verify`, và lý do được ghi thêm vào mục 6.4 của KE-HOACH.md ở commit `86134d0`. Lý do: kiểm chứng `Fail` làm engine hoàn tác, mà hoàn tác Plan xoá chỉ trả lại được vhost và pool (các bước Absent), không trả lại được DB và file. Kết quả sẽ là một vhost bật cho một site đã mất dữ liệu.

### Dòng `#` ở đầu file pool

Luật chung ở mục 5 của KE-HOACH.md lúc đầu: mọi file tpan sinh ra có dòng đầu `# Do tpan quản lý. Sửa tay sẽ bị tpan health báo lệch.` File pool PHP-FPM lại theo cú pháp INI của PHP, nơi `#` không còn là chú thích từ PHP 7.0. Chỗ sai nằm ở đặc tả, không ở mã: template pool dùng `; ` cộng dòng trên ngay từ commit đầu tiên của nó (`aee0650`, hằng `sitePoolComment` trong `internal/templates/site.go`), rồi KE-HOACH.md được sửa cho khớp ở commit `0572e36`. Phần "phép kiểm so bằng có chứa" trong KE-HOACH.md chưa có mã: ở M2 chỉ `VhostEnabled` đọc dòng đầu (của file vhost, so bằng `templates.Header`); chưa phép kiểm nào đọc dòng đầu file pool. Việc phát hiện file bị sửa tay thuộc M4.

Trên máy ảo mới kiểm chiều đúng: `php-fpm8.3 -t` nhận file có dòng `; # Do tpan quản lý...` (M2.log dòng 1653 tới 1657). Bản dùng `#` lỗi ra sao thì chưa chạy trên máy ảo. Tự thử: thêm một dòng `# thu` vào đầu một file pool rồi `scripts/vm.sh root php-fpm8.3 -t`, xong thì `scripts/vm.sh root tpan site add a.example.com --email admin@example.com --yes` để tpan ghi lại file.

### `FakeSystem` làm mất bit setgid

Trước M2, `Dir` chỉ nhận 9 bit quyền thường (`checkPerm` từ chối setgid) và `FakeSystem` cắt mọi mode về `perm.Perm()`, tức bỏ setgid. Hậu quả: không đặt được thư mục 2750, và nếu có đặt thì unit test không phân biệt được 2750 với 0750. Sửa ở commit `cef2ee3`, trước khi viết các Step của site: thêm `checkDirPerm` và `dirMode` trong `internal/steps/common.go`, và `keepMode` trong `internal/system/fake.go` giữ setuid, setgid, sticky như hệ thống file thật. Bài học: `FakeSystem` phải giống máy thật ở đúng những chỗ phép kiểm dựa vào.

## 6. Tự kiểm

1. Site A bị chiếm. Kẻ tấn công chạy được lệnh dưới user `tp_a_...`. Kể ra từng lớp chặn nó đọc `wp-config.php` của site B, theo thứ tự request đi qua hệ thống file.
2. Vì sao `wp-config.php` của site có nhóm `www-data` dù WP-CLI chạy dưới `tp_a_...`? Như vậy web server có đọc được mật khẩu DB không?
3. Bỏ bit setgid (thư mục 0750 thay vì 2750) thì hỏng gì, và hỏng lúc nào?
4. Bỏ `\_` trong câu GRANT thì user DB của site có thêm quyền gì? Vì sao `dbStateSQL` không cần thoát `_`?
5. Ở mục 2.8, vì sao thư mục giả `/etc/nginx/sites-available/tpan-d.example.com.conf` không bị xoá khi hoàn tác? Vì sao bước tải WordPress không được hoàn tác?
6. Ai đó xoá tay `wp-config.php` của site `a`, rồi chạy `tpan site add a.example.com --email admin@example.com --yes`. tpan làm gì, và vì sao không tạo lại file?
7. Vì sao chạy lại `site add` cho một site đã có không in mật khẩu quản trị, dù Plan có bước cài WordPress?
8. Vì sao `tpan site rm` không có phần "Kiểm chứng" như `site add`?
9. (Cần máy ảo) Chứng minh PHP của site `a` thật sự chạy dưới user của site `a`, bằng hai lệnh không qua tpan.
10. (Cần máy ảo) Đổi quyền `/var/www/b.example.com` thành 2755, rồi chạy `tpan health a.example.com`. Phép kiểm nào đổi kết quả, và vì sao chỉ đổi quyền thư mục là chưa đủ để `tp_a_...` đọc được `wp-config.php` của `b`?

## Đáp án

1. Lớp một: `/var/www/b.example.com` là `2750 tp_b:www-data`. `tp_a` không phải chủ, không thuộc nhóm `www-data` (nhóm chính của nó là nhóm riêng `tp_a`, do `useradd --user-group`), nên dùng quyền "người khác" là 0: không đi vào được thư mục. `ls` báo `cannot access`, `cat` báo `Permission denied` (mục 2.5). Lớp hai: `public/` cũng 2750. Lớp ba: `wp-config.php` là 600. Lớp bốn, chỉ cho mã PHP: `open_basedir` của pool `a` là `/var/www/a.example.com/`, nên `file_get_contents` tới thư mục site `b` bị PHP từ chối trước cả khi hỏi quyền Linux. Ở tầng DB: user DB của `a` chỉ có quyền trên DB của `a` (`ERROR 1044` khi `USE` DB của `b`).
2. Thư mục `public/` có bit setgid, nên file mới tạo trong đó mang nhóm của thư mục (`www-data`), không phải nhóm chính của người tạo. Web server không đọc được: quyền 600 cho nhóm là 0. Kịch bản kiểm `runuser -u www-data -- test -r .../wp-config.php` thoát mã 1 (mục 2.5).
3. File và thư mục mà PHP tạo sau này (ảnh tải lên qua wp-admin, thư mục `uploads/<năm>/<tháng>`, file cache của plugin) sẽ mang nhóm chính của user site, `tp_a`, thay vì `www-data`. Web server (`www-data`) không thuộc nhóm `tp_a`, nên chỉ còn quyền "người khác" để đọc chúng. Hoặc phần đó là 0 và web server không phục vụ được ảnh vừa tải lên, hoặc phải mở quyền cho mọi người, tức phá cô lập. File của bản cài đầu không bị, vì `WPFiles` đã đặt quyền; lỗi chỉ hiện ra khi site bắt đầu được dùng. Đây là suy luận theo quyền, chưa chạy trên máy ảo. Cũng là lý do `FakeSystem` phải giữ setgid (mục 5), không thì unit test không thấy khác biệt.
4. `_` trong tên DB của GRANT khớp một ký tự bất kỳ, nên `` `tp_a_example_com_7653e4`.* `` còn khớp các DB như `tpXaYexample...`. Nếu có một DB như vậy (của site khác hoặc ai đó tạo ra để lợi dụng), user DB của site `a` có toàn quyền trên nó. `dbStateSQL` so tên bằng `=` chứ không bằng `LIKE`, và `=` không có ký tự đại diện.
5. Bước ghi vhost lỗi trong `Apply` (`capture` báo "là thư mục" trước khi ghi). `runStep` trả `applied = false`, nên bước đó không nằm trong danh sách hoàn tác, và `File` cũng không được ghi gì để phải dọn. tpan chỉ hoàn tác thứ chính nó đã làm. Bước tải WordPress "đã đúng, bỏ qua" (file có trong cache từ site `a`), tức không `Apply`, nên không có gì để hoàn tác; kể cả khi có tải, `WPArchive.Undo` cũng không xoá cache (NoUndo: dùng lại cho site sau).
6. `PrepareAdd` thấy domain đã có trong bảng `sites`, nên gọi `drift`. User DB còn mà `wp-config.php` không có: từ chối với mã `site_drift`, kèm hướng dẫn lấy lại `wp-config.php` từ backup hoặc xoá hẳn rồi tạo lại. Không tạo lại vì lần chạy này sinh mật khẩu DB mới, mà user DB đang dùng mật khẩu cũ tpan không giữ ở đâu; ghi mật khẩu mới vào `wp-config.php` là site báo "Error establishing a database connection". Đường này mới có ở unit test (`TestSiteAddRefusals`, trường hợp "lệch"), chưa chạy trên máy ảo.
7. `wpInstallStep` hỏi `wp core is-installed` ngay trước khi giao cho `WPInstall`, và chỉ đặt `ran = true` khi WordPress chưa cài. Lần chạy lại, `Check` của bước đã đạt nên bước bị bỏ qua hoàn toàn; kể cả khi chỉ permalink lệch và bước chạy, `ran` vẫn là false. `SiteAdd.Execute` chỉ trả mật khẩu khi `install.ran`, và mật khẩu mới chưa bao giờ được dùng nên in ra là in một mật khẩu sai. Log: `Mật khẩu quản trị trong output: 0 lần` ở mục 2.6.
8. Kiểm chứng `Fail` làm engine hoàn tác các bước đã chạy. Trong Plan xoá, chỉ ba bước Absent (vhost, pool) hoàn tác được; DB, file, user thì đã mất. Hoàn tác lúc đó là bật lại vhost và pool cho một site không còn DB và mã nguồn, tệ hơn để yên. Nên `PrepareRemove` không đặt `Verify`, và e2e tự kiểm sau khi xoá bằng lệnh của Linux (mục 2.9).
9. Lệnh một: gọi site cho pool ondemand sinh worker, `scripts/vm.sh exec curl -s -o /dev/null --resolve a.example.com:80:127.0.0.1 http://a.example.com/`, rồi `scripts/vm.sh exec ps -eo user:32,cmd`. Phải thấy dòng `tp_a_example_com_7653e4 php-fpm: pool tpan-a_example_com_7653e4` (output thật ở mục 2.5). Lệnh hai: xem cấu hình mà master đọc, `scripts/vm.sh root grep -E '^(user|group|listen)' /etc/php/8.3/fpm/pool.d/tpan-a_example_com_7653e4.conf` (chưa chạy). Cách thứ ba kịch bản đã làm: một file PHP gọi `error_log`, và dòng log xuất hiện trong `php-error.log` thuộc `tp_a` (mục 16, M2.log dòng 2187 tới 2197).
10. Theo mã, chưa chạy: `scripts/vm.sh root chmod 2755 /var/www/b.example.com`, rồi `scripts/vm.sh root tpan health a.example.com`. Phép kiểm `Isolated` (a đọc b) vẫn đạt, vì `public/` vẫn 2750 và `wp-config.php` vẫn 600: mở thư mục ngoài chỉ gỡ lớp một ở câu 1. Phải mở thêm `public/` (2755) và `wp-config.php` (644) thì `test -r` mới thoát 0; lúc đó `Isolated` thành `Fail`, kèm lời khuyên `chmod 2750` và `chmod 600`, và health thoát mã 1. Sửa lại bằng `scripts/vm.sh root tpan site add b.example.com --email admin@example.com --yes`: `Dir.Check` và `WPConfig.Check` so quyền, thấy lệch thì đặt lại 2750 và 600.
