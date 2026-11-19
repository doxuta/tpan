# M12: phiên bản và cập nhật

Đọc kèm: `01-m1-cai-stack.md` (engine, Step, Check rồi Apply rồi Check, Plan cài stack), `02-m2-site-wordpress.md` (pool PHP-FPM, socket, vhost, slug), `04-m4-health.md` (health, sự cố, sửa tự động), `docs/KE-HOACH.md` mục 3 (gói hệ điều hành), 5 (bố trí trên máy chủ), 6.1 (cài stack), 6.11 (phiên bản và cập nhật), 9 (CLI và API), 12 (bước 14, 15 của e2e), và `docs/thay-doi/M12.md` (bảng chia việc, chỗ khác đặc tả, kết quả kiểm, việc còn mở).

## 1. Mốc này làm được gì

- Một máy chạy nhiều bản PHP cùng lúc, mỗi site chọn bản riêng: `sudo tpan php install 8.4` cài bản mới (thêm kho sury khi Ubuntu không có bản đó), `sudo tpan php use shop.example.com 8.4` chuyển một site sang bản đó mà không request nào lỗi, `sudo tpan php remove 8.4` gỡ bản không còn site dùng. `tpan php list` cho xem bản nào có, hạn hỗ trợ của php.net và bản WordPress.org khuyên dùng.
- Pool `www` thừa của gói `php-fpm` (2 tiến trình chạy không cho ai) bị gỡ, thay bằng pool giữ chỗ `tpan-idle` không có tiến trình con nào.
- Lúc cài stack, chọn được nginx của nginx.org và một dòng MariaDB của kho MariaDB, thay cho gói của Ubuntu.
- `sudo tpan update --check` cho xem gói nào của stack có bản mới, gói nào là bản vá bảo mật; `sudo tpan update` nâng chúng, khởi động lại đúng dịch vụ, rồi chạy lại toàn bộ health. `sudo tpan update auto` bật bản vá bảo mật tự cài hằng ngày, gồm cả kho ngoài. `sudo tpan mariadb upgrade --to 11.8` nâng MariaDB lên dòng mới (một chiều, có file xuất làm đường lùi). `sudo tpan self-update` thay chính file chạy của tpan, có `--rollback`.
- Giao diện có màn Stack (các bản PHP, cài, gỡ) và màn Cập nhật (gói chờ nâng, bản vá tự động, nâng MariaDB, lệnh self-update), cộng khối đổi bản PHP ở trang một site.

## 2. Chạy thử

Mọi output dưới đây lấy nguyên văn từ `docs/bang-chung/M12.log`: lần chạy `scripts/e2e-m12.sh all` ngày 19/11/2026, 22:45 tới 22:51, dòng cuối là `TỔNG KẾT: 174 khẳng định đạt, 0 khẳng định không đạt`. Máy ảo của mốc này tên `tpan-m12` (ghi trong file `.tpan-vm`), Multipass, Ubuntu 24.04, arm64. Log chia thành phần A, B, C, G, D, E, F; mục 2.1 tới 2.11 đi theo đúng thứ tự đó.

Những điều cần biết khi đọc log:

- Mọi lệnh ở đây gõ trên Mac, trong thư mục repo. `scripts/vm.sh exec X` chạy X trong máy ảo; `scripts/vm.sh root X` chạy X trong máy ảo bằng `sudo`. Phần D chạy trên một bản sao của máy ảo, nên lệnh có thêm `env TPAN_VM=tpan-m12-clone` ở đầu: biến đó bảo `scripts/vm.sh` nói chuyện với bản sao thay vì máy chính.
- Kịch bản in lệnh bằng `$*` nên mất dấu nháy. Lệnh dưới đây mình viết lại có nháy đúng để bạn dán vào zsh được; output giữ nguyên.
- Kịch bản che IP máy ảo (`<vm>`), IP bản sao (`<bản sao>`), IP của Mac (`<mac>`), mật khẩu (`<mật khẩu, đã che>`). Riêng file này che thêm tên tài khoản GitHub trong địa chỉ tải bản phát hành: log ghi tên thật, ở đây viết `<chủ repo>`.
- Giờ trong log là giờ máy Mac (+07). Bảng sự kiện của job đọc từ `tpan.db` ghi giờ UTC, nên `22:46:36` trong log là `15:46:36` trong bảng sự kiện.
- Máy ảo không có tên miền thật. Kịch bản trỏ `a.example.com`, `b.example.com` về `127.0.0.1` bằng `/etc/hosts`.

### 2.1 Cài stack: pool www thay bằng tpan-idle (phần A)

Chạy trên Mac:

```bash
make vm-reset
make vm-deploy
scripts/vm.sh root tpan stack install --web nginx --yes
```

Output của cài stack giống M1, chỉ thêm một bước và một dòng kiểm chứng (M12.log dòng 103 tới 162):

```
- Dò bản PHP-FPM đã cài ...
  đã đúng, bỏ qua
- Thay pool www của php<bản>-fpm bằng pool tpan-idle ...
  xong
...
  [đạt] PHP-FPM 8.3 không còn pool www thừa: Không có pool www; pool giữ chỗ tpan-idle có mặt
```

Tên bước còn chữ `<bản>` vì lúc job bắt đầu, bước "Dò bản PHP-FPM" chưa chạy nên tpan chưa biết bản PHP (mục 3.7 nói vì sao). Xem kết quả trên máy (chạy trên Mac):

```bash
scripts/vm.sh exec ls -la /etc/php/8.3/fpm/pool.d
scripts/vm.sh exec bash -c 'ps -eo user,cmd | grep [p]hp-fpm'
scripts/vm.sh exec ls -la /run/php
```

```
total 12
drwxr-xr-x 2 root root 4096 Nov 19 22:45 .
drwxr-xr-x 4 root root 4096 Nov 19 22:45 ..
-rw-r--r-- 1 root root 1060 Nov 19 22:45 tpan-idle.conf
```

```
root     php-fpm: master process (/etc/php/8.3/fpm/php-fpm.conf)
```

```
total 4
drwxr-xr-x  2 www-data www-data 100 Nov 19 22:45 .
drwxr-xr-x 32 root     root     980 Nov 19 22:45 ..
lrwxrwxrwx  1 root     root      30 Nov 19 22:45 php-fpm.sock -> /etc/alternatives/php-fpm.sock
-rw-r--r--  1 root     root       5 Nov 19 22:45 php8.3-fpm.pid
srw-------  1 root     root       0 Nov 19 22:45 tpan-idle-8.3.sock
```

Không còn `www.conf`, không còn tiến trình `php-fpm: pool www`, chỉ còn tiến trình master. Socket của pool giữ chỗ là `srw-------  root root`: chỉ root mở được. Ở M2, chỗ này có 2 tiến trình `pool www` dưới `www-data`.

Chạy lại `stack install --web nginx --yes` thì mọi bước ra `đã đúng, bỏ qua`, kể cả `Thay pool www của php8.3-fpm bằng pool tpan-idle` (lần này tên bước có số bản, vì `stack.php_version` đã ghi). Job mất 0,1 giây (M12.log dòng 253 tới 335).

### 2.2 Cài PHP 8.4 (phần B)

Trước hết hai site, `a.example.com` và `b.example.com`, tạo như M2. Rồi xem danh sách bản PHP (chạy trên Mac):

```bash
scripts/vm.sh root tpan php list
```

```
BẢN     ĐÃ CÀI  MẶC ĐỊNH  SITE  NGUỒN   HỖ TRỢ
8.3     có      có        2     ubuntu  chỉ còn vá bảo mật tới 2027-12-31

WordPress.org khuyên dùng PHP 8.3
Danh sách chỉ có bản của Ubuntu. Cài một bản khác (sudo tpan php install <bản>) sẽ thêm kho PHP của Ondřej Surý (packages.sury.org).
```

Kho Ubuntu 24.04 chỉ có `php8.3-fpm` (lệnh `apt-cache search --names-only '^php[0-9]+\.[0-9]+-fpm$'` in đúng một dòng, M12.log dòng 513, 514). Xem trước khi cài 8.4:

```bash
scripts/vm.sh root tpan php install 8.4 --dry-run
```

```
Kế hoạch: Cài PHP 8.4
4 bước sẽ chạy, 0 bước đã đúng sẽ bỏ qua, 1 bước có thể gián đoạn dịch vụ.

 1. [sẽ chạy] Thêm kho PHP của Ondřej Surý (packages.sury.org)  (apt.repo:sury)
    NGUY HIỂM: bước này có thể làm gián đoạn dịch vụ.
    Vì sao: Đây là nguồn gói ngoài Ubuntu: từ giờ apt cài và cập nhật gói từ kho này, ký bằng khoá có vân tay 15058500A0235D97F5D10063B188E2B695BD4743. Không có bước này thì không cài được bản bạn chọn (Ubuntu không có). File ghim giữ cho các gói khác vẫn lấy từ Ubuntu.
    $ curl -fsSL --proto =https --max-time 60 -o /tmp/tpan-apt-key.XXXXXXXX/key https://packages.sury.org/php/apt.gpg
    ...
    tạo /etc/apt/sources.list.d/tpan-sury.sources
    tạo /etc/apt/preferences.d/tpan-sury
 2. [sẽ chạy] Cài gói: php8.4-fpm php8.4-cli php8.4-mysql php8.4-curl php8.4-gd php8.4-mbstring php8.4-xml php8.4-zip php8.4-intl  (apt.install:...)
 3. [sẽ chạy] Thay pool www của php8.4-fpm bằng pool tpan-idle  (php.idle:8.4)
    ...
    tạo /etc/php/8.4/fpm/pool.d/tpan-idle.conf
    xoá /etc/php/8.4/fpm/pool.d/www.conf
 4. [sẽ chạy] Chạy và bật php8.4-fpm khi khởi động  (service:php8.4-fpm)
```

(Bản đầy đủ ở M12.log dòng 520 tới 546; chỗ `...` là mình cắt.) Xem trước không ghi file nào: `ls /etc/apt/sources.list.d/` sau đó vẫn chỉ có `ubuntu.sources`. Chạy thật:

```bash
scripts/vm.sh root tpan php install 8.4 --yes
```

```
Bắt đầu job 3b6774e310718358
- Thêm kho PHP của Ondřej Surý (packages.sury.org) ...
  xong
- Cài gói: php8.4-fpm php8.4-cli php8.4-mysql php8.4-curl php8.4-gd php8.4-mbstring php8.4-xml php8.4-zip php8.4-intl ...
  xong
- Thay pool www của php8.4-fpm bằng pool tpan-idle ...
  xong
- Chạy và bật php8.4-fpm khi khởi động ...
  đã đúng, bỏ qua
  [đạt] Dịch vụ php8.4-fpm đang chạy và tự bật cùng máy
  [đạt] Cấu hình PHP-FPM 8.4 hợp lệ
  [đạt] PHP-FPM 8.4 không còn pool www thừa

Job 3b6774e310718358: thành công
```

Job mất 20,5 giây. Ba file tpan vừa ghi cho kho (chạy trên Mac):

```bash
scripts/vm.sh exec cat /etc/apt/sources.list.d/tpan-sury.sources
scripts/vm.sh exec gpg --show-keys --with-colons /etc/apt/keyrings/tpan-sury.gpg
scripts/vm.sh exec cat /etc/apt/preferences.d/tpan-sury
```

```
# Do tpan quản lý. Sửa tay sẽ bị tpan health báo lệch.
# Kho apt ngoài, dạng deb822. Signed-By trỏ vào đúng một file khoá do tpan tạo
# (chỉ chứa các vân tay đã chốt): kho này chỉ được tin bằng khoá đó.
Types: deb
URIs: https://packages.sury.org/php/
Suites: noble
Components: main
Signed-By: /etc/apt/keyrings/tpan-sury.gpg
```

```
pub:-:3072:1:B188E2B695BD4743:1552904095:1833274803::-:::scESC::::::23::0:
fpr:::::::::15058500A0235D97F5D10063B188E2B695BD4743:
uid:-::::1763385756::5A6880F9B1075434C4E8CF3B0E5CC319E0A6B8E8::DEB.SURY.ORG Automatic Signing Key <deb@sury.org>::::::::::0:
sub:-:3072:1:B214EAC28059B8AC:1552904095:1833274801:::::e::::::23:
fpr:::::::::45BEA3E529112086C622F8A4B214EAC28059B8AC:
```

```
# Do tpan quản lý. Sửa tay sẽ bị tpan health báo lệch.
Explanation: tpan ghim kho ngoài để apt không tự thay các gói Ubuntu đang chạy bằng bản của kho đó; chỉ các gói nêu rõ dưới đây mới lấy từ kho ngoài.
Package: *
Pin: origin packages.sury.org
Pin-Priority: 100

Package: php-common php8.4*
Pin: origin packages.sury.org
Pin-Priority: 500
```

(Lệnh `gpg` còn in ba dòng `gpg: ... created` vì user `ubuntu` chưa có thư mục `.gnupg`; mình bỏ ba dòng đó.) Khoá có một khoá chính (`pub`, vân tay `15058500...4743`) và một khoá con (`sub`). Hiệu quả của file ghim:

```bash
scripts/vm.sh exec apt-cache policy php-fpm php8.3-fpm
```

```
php-fpm:
  Installed: 2:8.3+93ubuntu2
  Candidate: 2:8.3+93ubuntu2
  Version table:
     2:8.4+101+debsuryorg24.04.1+php+1 100
        100 https://packages.sury.org/php noble/main arm64 Packages
 *** 2:8.3+93ubuntu2 500
        500 http://ports.ubuntu.com/ubuntu-ports noble/universe arm64 Packages
        100 /var/lib/dpkg/status
php8.3-fpm:
  Installed: 8.3.6-0ubuntu0.24.04.13
  Candidate: 8.3.6-0ubuntu0.24.04.13
  Version table:
     8.3.36-1+0~20261021.88+ubuntu24.04~1.gbp250f7c 100
        100 https://packages.sury.org/php noble/main arm64 Packages
 *** 8.3.6-0ubuntu0.24.04.13 500
        500 http://ports.ubuntu.com/ubuntu-ports noble-updates/universe arm64 Packages
        500 http://ports.ubuntu.com/ubuntu-ports noble-security/universe arm64 Packages
        100 /var/lib/dpkg/status
     8.3.6-0maysync1 500
        500 http://ports.ubuntu.com/ubuntu-ports noble/universe arm64 Packages
```

Kho sury có `php-fpm` 8.4 và `php8.3-fpm` 8.3.36, đều mới hơn bản Ubuntu, nhưng chỉ có độ ưu tiên 100, nên `Candidate` (bản apt sẽ cài khi nâng cấp) vẫn là bản Ubuntu. `php -v` vẫn in `PHP 8.3.6`. Sau khi cài, `ps` thấy hai master, không có pool `www` nào:

```
root     php-fpm: master process (/etc/php/8.3/fpm/php-fpm.conf)
root     php-fpm: master process (/etc/php/8.4/fpm/php-fpm.conf)
```

Và `dpkg-query -W php8.4-fpm php-common php8.3-fpm`:

```
php-common	2:101+debsuryorg24.04.1+php+1
php8.3-fpm	8.3.6-0ubuntu0.24.04.13
php8.4-fpm	8.4.27-1+0~20261021.57+ubuntu24.04~1.gbp7636b9
```

`php-common` giờ là bản của kho sury: đó là gói duy nhất ngoài `php8.4*` được phép lấy từ kho này (mục 3.4 nói vì sao).

### 2.3 PHP 8.4 của kho sury khởi động chậm 2 giây

Kịch bản đo thời gian `php-fpm<bản> -t` (kiểm cấu hình) của hai bản, và đếm số lần nó gọi tới `telemetry.sury.org` bằng `strace` (chạy trên Mac):

```bash
scripts/vm.sh root bash -c 'for v in 8.3 8.4; do s=$(date +%s%N); php-fpm$v -t >/dev/null 2>&1; e=$(date +%s%N); echo "php-fpm$v -t: $(( (e - s) / 1000000 )) ms"; done'
scripts/vm.sh root bash -c 'strace -f -e trace=sendmmsg,sendto,connect -s 64 php-fpm8.4 -t 2>&1 | grep -c "telemetry"; strace -f -e trace=sendmmsg,sendto,connect -s 64 php-fpm8.3 -t 2>&1 | grep -c "telemetry"'
```

```
php-fpm8.3 -t: 14 ms
php-fpm8.4 -t: 2112 ms
```

```
1
0
```

Bản 8.4 của kho sury gửi một truy vấn DNS tới `telemetry.sury.org` mỗi lần khởi động hoặc chạy `-t`, và chờ khoảng 2 giây. Changelog của gói (`zcat /usr/share/doc/php8.4-common/changelog.Debian.gz`) ghi "The secure DNS telemetry is disabled at the compile-time", nhưng thực tế vẫn còn. Đây là việc chủ dự án phải quyết (mục "Còn mở" ở `docs/thay-doi/M12.md`). Với M12 nó có một hệ quả thật: lần nạp lại PHP-FPM 8.4 chậm, và chính độ chậm này làm lộ lỗi ở mục 5.4.

### 2.4 Bước 14: đổi bản PHP của site trong lúc curl gọi liên tục

Trước khi đổi, cả hai pool của site chạy dưới master 8.3, và socket mang số bản:

```
srw-rw----  1 www-data www-data   0 Nov 19 22:45 tpan-a_example_com_7653e4-8.3.sock
srw-rw----  1 www-data www-data   0 Nov 19 22:45 tpan-b_example_com_cd59d5-8.3.sock
srw-------  1 root     root       0 Nov 19 22:45 tpan-idle-8.3.sock
srw-------  1 root     root       0 Nov 19 22:46 tpan-idle-8.4.sock
```

Kịch bản bật một vòng lặp chạy nền bằng `systemd-run` (M12.log dòng 776): gọi `curl` vào `http://a.example.com/` liên tục, không nghỉ, mỗi lần ghi mã thoát của curl và mã HTTP vào `/root/tpan-e2e-curl-84.log`. Trong lúc đó (chạy trên Mac):

```bash
scripts/vm.sh root tpan php use a.example.com 8.4 --yes
```

```
Bắt đầu job d94ac8aac9c9d372
- Ghi pool PHP-FPM tpan-a_example_com_7653e4 cho a.example.com ...
  xong
- Chờ socket /run/php/tpan-a_example_com_7653e4-8.4.sock của PHP 8.4 ...
  xong
- Ghi vhost nginx cho a.example.com ...
  xong
- Bật /etc/nginx/sites-enabled/tpan-a.example.com.conf → /etc/nginx/sites-available/tpan-a.example.com.conf ...
  đã đúng, bỏ qua
- Kiểm a.example.com trả 200 qua PHP 8.4 ...
  xong
- Gỡ pool tpan-a_example_com_7653e4 ở PHP 8.3 ...
  xong
  [đạt] Vhost của site có, do tpan quản lý và đang được bật
  [đạt] Socket của pool PHP có, đúng chủ và đúng quyền
  [đạt] Mã nguồn WordPress nguyên vẹn (khớp checksum)
  [đạt] wp-config.php có quyền 0600 và đúng chủ
  [đạt] User DB của site đăng nhập được, chỉ thấy DB của site
  [đạt] Gọi HTTP vào site trả 200 và đúng là WordPress của site
  [đạt] Site a.example.com không đọc được wp-config.php của site b.example.com
  [đạt] Site b.example.com không đọc được wp-config.php của site a.example.com

Job d94ac8aac9c9d372: thành công
```

Dừng vòng lặp rồi đếm:

```
tổng request: 435
request lỗi (curl thoát khác 0 hoặc mã khác 200): 0
từ 1795103194.367533931 tới 1795103208.636075483 = 14.27 giây
```

435 request trong 14 giây, không request nào lỗi, kể cả lỗi kết nối. Bảng sự kiện của job (đọc từ `tpan.db`, giờ UTC) cho thấy từng bước tốn bao lâu:

```
  15:46:36.478 step.start file:/etc/php/8.4/fpm/pool.d/tpan-a_example_com_7653e4.conf
  15:46:36.480 command file:/etc/php/8.4/fpm/pool.d/tpan-a_example_com_7653e4.conf php-fpm8.4 -t
  15:46:38.554 command file:/etc/php/8.4/fpm/pool.d/tpan-a_example_com_7653e4.conf systemctl reload php8.4-fpm
  15:46:38.573 step.done file:/etc/php/8.4/fpm/pool.d/tpan-a_example_com_7653e4.conf
  15:46:38.575 step.start php.use.socket:a.example.com
  15:46:41.579 step.done php.use.socket:a.example.com
  15:46:41.583 step.start file:/etc/nginx/sites-available/tpan-a.example.com.conf
  15:46:41.585 command file:/etc/nginx/sites-available/tpan-a.example.com.conf nginx -t
  15:46:41.591 command file:/etc/nginx/sites-available/tpan-a.example.com.conf systemctl reload nginx
  15:46:41.604 step.done file:/etc/nginx/sites-available/tpan-a.example.com.conf
  ...
  15:46:41.610 command php.use.check:a.example.com curl -sS --max-time 15 --resolve a.example.com:80:127.0.0.1 -w 'TPAN_HTTP_CODE=%{http_code}' http://a.example.com/
  15:46:42.892 command php.use.check:a.example.com curl -sS --max-time 15 --resolve a.example.com:80:127.0.0.1 -w 'TPAN_HTTP_CODE=%{http_code}' http://a.example.com/
  15:46:43.162 step.done php.use.check:a.example.com
  15:46:43.163 step.start absent:/etc/php/8.3/fpm/pool.d/tpan-a_example_com_7653e4.conf
```

Ba chỗ đáng nhìn: `php-fpm8.4 -t` mất 2 giây (đúng con số ở 2.3); `systemctl reload php8.4-fpm` trả về sau 19 ms nhưng bước chờ socket phải đợi thêm 3 giây; và kiểm HTTP gọi hai lần (cần hai lần đạt liên tiếp). Sau khi đổi:

```
pool tpan-a_example_com_7653e4 user tp_a_example_com_7653e4 master /etc/php/8.4/fpm/php-fpm.conf
pool tpan-b_example_com_cd59d5 user tp_b_example_com_cd59d5 master /etc/php/8.3/fpm/php-fpm.conf
```

```bash
scripts/vm.sh exec grep -n fastcgi_pass /etc/nginx/sites-available/tpan-a.example.com.conf /etc/nginx/sites-available/tpan-b.example.com.conf
```

```
/etc/nginx/sites-available/tpan-a.example.com.conf:54:        fastcgi_pass unix:/run/php/tpan-a_example_com_7653e4-8.4.sock;
/etc/nginx/sites-available/tpan-b.example.com.conf:54:        fastcgi_pass unix:/run/php/tpan-b_example_com_cd59d5-8.3.sock;
```

Site 1 chạy dưới master 8.4, site 2 vẫn ở 8.3. File pool của site 1 chỉ còn ở `/etc/php/8.4/fpm/pool.d/`. `tpan site info a.example.com --json` có `"php_version": "8.4"` và trường mới `"socket": "/run/php/tpan-a_example_com_7653e4-8.4.sock"`.

Đổi về 8.3 cũng trong lúc curl gọi liên tục: 397 request trong 11,66 giây, 0 lỗi; bước chờ socket chỉ đợi 1 giây (8.3 của Ubuntu không chờ DNS). Gọi đổi về 8.3 lần nữa khi đã ở 8.3:

```
site a.example.com đã chạy PHP 8.3; không đổi gì
```

mã thoát 0, không tạo job.

### 2.5 Gỡ PHP 8.4

Gỡ khi site 1 còn ở 8.4 (trước lần đổi về 8.3) thì bị từ chối:

```bash
scripts/vm.sh root tpan php remove 8.4 --yes
```

```
Từ chối: PHP 8.4 còn site dùng: a.example.com. Chuyển các site đó sang bản khác trước (sudo tpan php use <domain> <bản>)
```

Mã thoát 1; bản `--json` có `"code": "php_in_use"`. Sau khi site 1 về 8.3, chạy lại thì job gỡ 12 gói `php8.4-*` trong 2,3 giây. `dpkg-query` cho thấy các gói đó ở trạng thái `config-files` (gỡ chương trình, giữ file cấu hình), không còn master 8.4, nhưng `tpan-sury.sources` vẫn còn. `tpan php list` lúc này có thêm mọi bản kho sury cung cấp (M12.log dòng 1431 tới 1446):

```
BẢN     ĐÃ CÀI  MẶC ĐỊNH  SITE  NGUỒN   HỖ TRỢ
5.6     -       -         0     sury    hết hỗ trợ
...
8.2     -       -         0     sury    chỉ còn vá bảo mật tới 2026-12-31
8.3     có      có        2     ubuntu  chỉ còn vá bảo mật tới 2027-12-31
8.4     -       -         0     sury    hỗ trợ đủ tới 2026-12-31, vá bảo mật tới 2028-12-31
8.5     -       -         0     sury    hỗ trợ đủ tới 2027-12-31, vá bảo mật tới 2029-12-31
8.6     -       -         0     sury    hỗ trợ đủ tới 2028-12-31, vá bảo mật tới 2030-12-31
```

Danh sách này không có trong mã: nó là kết quả `apt-cache search` cộng hạn hỗ trợ hỏi php.net lúc chạy. Health sau đó đạt hết cho máy và hai site.

### 2.6 Bước 15: có gói của stack chờ nâng, `tpan update` (phần C)

Máy ảo vừa cài nên không có gói nào của stack chờ nâng. Để có, kịch bản dùng một đồ nghề chỉ có trong máy ảo: hạ cả bộ 12 gói `php8.3-*` về bản `8.3.6-0maysync1` của kho release (bản cũ nhất Ubuntu 24.04 có). Sau khi hạ, lệnh `scripts/vm.sh exec bash -c 'dpkg-query -W php8.3-fpm php8.3-common; ls /etc/php/8.3/fpm/pool.d; dpkg-query -W -f "\${Conffiles}\n" php8.3-fpm; systemctl is-active php8.3-fpm'` in (M12.log dòng 1674):

```
php8.3-common	8.3.6-0maysync1
php8.3-fpm	8.3.6-0maysync1
tpan-a_example_com_7653e4.conf
tpan-b_example_com_cd59d5.conf
tpan-idle.conf
 /etc/apache2/conf-available/php8.3-fpm.conf c763554beba3b432ebf19c71e69ada46
 /etc/init.d/php8.3-fpm 41b98fe30a0cb49481818a0c297d9bcb
 /etc/logrotate.d/php8.3-fpm d2accdb1b12c3ac02239c433153fdb79
 /etc/php/8.3/fpm/php-fpm.conf 696fcb228ad0e29f57cdd2082eb41fac
 /etc/php/8.3/fpm/pool.d/www.conf 6204554fd51b0d45f12e2f2b5b62e377
active
```

Dòng cuối của danh sách `Conffiles` là `www.conf`: gói vẫn coi nó là file cấu hình của mình, dù file đã bị tpan xoá. Câu hỏi của mục 16 là: lần nâng gói tới, apt có đem `www.conf` về, hay hỏi "giữ hay thay" và treo không. Câu trả lời ở dưới.

**Khoá danh sách gói.** Trước đó, kịch bản tái hiện một lỗi chỉ gặp trên máy vừa khởi động (mục 5.4): một tiến trình python giữ khoá `/var/lib/apt/lists/lock` 12 giây, đúng như `apt-daily` giữ khi nó chạy `apt-get update`. Đối chứng (chạy trên Mac):

```bash
scripts/vm.sh root apt-get update -o DPkg::Lock::Timeout=300
```

```
Reading package lists...
E: Could not get lock /var/lib/apt/lists/lock. It is held by process 47151 (python3)
E: Unable to lock directory /var/lib/apt/lists/
```

Mã thoát 100, ngay lập tức, dù đã bảo apt chờ 300 giây. Giữ khoá lần nữa rồi chạy `sudo tpan update --check --json`: lệnh chạy 16 giây (chờ khoá rồi chạy tiếp), thoát 0. Bản chữ:

```bash
scripts/vm.sh root tpan update --check
```

```
Gói của stack có bản mới (12)
  php8.3-cli       8.3.6-0maysync1 -> 8.3.6-0ubuntu0.24.04.13  [bảo mật]
  php8.3-common    8.3.6-0maysync1 -> 8.3.6-0ubuntu0.24.04.13  [bảo mật]
  php8.3-curl      8.3.6-0maysync1 -> 8.3.6-0ubuntu0.24.04.13  [bảo mật]
  php8.3-fpm       8.3.6-0maysync1 -> 8.3.6-0ubuntu0.24.04.13  [bảo mật]
  php8.3-gd        8.3.6-0maysync1 -> 8.3.6-0ubuntu0.24.04.13  [bảo mật]
  php8.3-intl      8.3.6-0maysync1 -> 8.3.6-0ubuntu0.24.04.13  [bảo mật]
  php8.3-mbstring  8.3.6-0maysync1 -> 8.3.6-0ubuntu0.24.04.13  [bảo mật]
  php8.3-mysql     8.3.6-0maysync1 -> 8.3.6-0ubuntu0.24.04.13  [bảo mật]
  php8.3-opcache   8.3.6-0maysync1 -> 8.3.6-0ubuntu0.24.04.13  [bảo mật]
  php8.3-readline  8.3.6-0maysync1 -> 8.3.6-0ubuntu0.24.04.13  [bảo mật]
  php8.3-xml       8.3.6-0maysync1 -> 8.3.6-0ubuntu0.24.04.13  [bảo mật]
  php8.3-zip       8.3.6-0maysync1 -> 8.3.6-0ubuntu0.24.04.13  [bảo mật]
Cập nhật: sudo tpan update
Gói khác của hệ điều hành đang chờ: 55 (bảo mật 33; bản vá bảo mật do unattended-upgrades tự cài)

[đạt] Bản vá bảo mật tự cài hằng ngày: Bản vá tự cài hằng ngày đang bật (APT::Periodic::Unattended-Upgrade "1")
  Kho ngoài được tự vá: deb.sury.org

MariaDB: 1:10.11.14-0ubuntu0.24.04.1 (dòng 10.11, nguồn ubuntu)
  Nâng lên được dòng LTS 11.4 (hỗ trợ tới 2029-05-29): sudo tpan mariadb upgrade --to 11.4
  Nâng lên được dòng LTS 11.8 (hỗ trợ tới 2030-02-13): sudo tpan mariadb upgrade --to 11.8
  Nâng lên được dòng LTS 12.3 (hỗ trợ tới 2029-06-12): sudo tpan mariadb upgrade --to 12.3

tpan d7a899e-dirty (gốc tải https://github.com/<chủ repo>/tpan/releases/latest/download)
  Cập nhật: sudo tpan self-update
```

Mỗi gói được đánh dấu `[bảo mật]` vì bản mới nằm cả ở `noble-updates` lẫn `noble-security`. Dòng "Kho ngoài được tự vá: deb.sury.org" in ra ở đây là chưa đúng: lúc này file `52tpan-unattended-upgrades` chưa có (2.7 mới tạo nó). Mục 3.8 giải thích vì sao.

**unattended-upgrades với `www.conf` đã xoá.** Kịch bản chạy thử bản vá tự động (chỉ mô phỏng, `--dry-run`):

```bash
scripts/vm.sh root timeout 900 unattended-upgrade --dry-run -d
```

Đoạn về gói `php8.3-fpm` (M12.log dòng 2556 tới 2570, giữa hàng nghìn dòng khác):

```
check_conffile_prompt(/var/cache/apt/archives/php8.3-fpm_8.3.6-0ubuntu0.24.04.13_arm64.deb)
found pkg: php8.3-fpm
...
conffile line: /etc/php/8.3/fpm/pool.d/www.conf 6204554fd51b0d45f12e2f2b5b62e377
conffile /etc/php/8.3/fpm/pool.d/www.conf in missing on the system
current md5: 
pkg_md5sum: 6204554fd51b0d45f12e2f2b5b62e377
```

Và cuối lần chạy: `All upgrades installed`. unattended-upgrades bỏ qua gói nào có thể bật câu hỏi conffile; ở đây nó thấy `www.conf` không có trên máy, không coi đó là câu hỏi, và vẫn nâng các gói php8.3.

**Nâng thật.** Xem trước rồi chạy:

```bash
scripts/vm.sh root tpan update --dry-run
```

```
Kế hoạch: Cập nhật 12 gói của stack
3 bước sẽ chạy, 0 bước đã đúng sẽ bỏ qua, 2 bước có thể gián đoạn dịch vụ.

 1. [sẽ chạy] Làm mới danh sách gói (apt-get update)  (apt.update:lists)
    ...
    $ apt-get update -o DPkg::Lock::Timeout=300
 2. [sẽ chạy] Nâng gói: php8.3-cli php8.3-common php8.3-curl php8.3-fpm php8.3-gd php8.3-intl php8.3-mbstring php8.3-mysql php8.3-opcache php8.3-readline php8.3-xml php8.3-zip  (apt.upgrade:...)
    NGUY HIỂM: bước này có thể làm gián đoạn dịch vụ.
    Vì sao: Bản mới vá lỗi và lỗ hổng. Không nâng thì máy tiếp tục chạy bản có lỗi đã biết. File cấu hình đang dùng được giữ nguyên.
    $ apt-get install -y --only-upgrade -o DPkg::Lock::Timeout=300 -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold php8.3-cli ... php8.3-zip
    Ghi chú hoàn tác: Không hoàn tác: apt không hạ gói về bản cũ một cách an toàn (bản cũ có thể không còn trên kho, gói khác có thể đã cần bản mới).
 3. [sẽ chạy] Khởi động lại php8.3-fpm  (service.restart:php8.3-fpm)
    NGUY HIỂM: bước này có thể làm gián đoạn dịch vụ.
    Vì sao: Dịch vụ đang chạy vẫn dùng bản cũ trong bộ nhớ cho tới khi được khởi động lại; không làm bước này thì bản vá chưa có tác dụng. Cấu hình được kiểm trước, sai thì không khởi động lại.
    $ php-fpm8.3 -t
    $ systemctl restart php8.3-fpm
```

```bash
scripts/vm.sh root tpan update --yes --verbose
```

Output `--verbose` dài (M12.log dòng 3838 tới 4000): in từng lệnh và output của apt. Phần cuối:

```
Kiểm lại toàn bộ health sau khi đổi gói:
  Stack và mọi site vẫn đạt.
```

Job mất 10,1 giây. Kiểm sau đó, cùng kiểu lệnh như trên cộng `ps` và `systemctl is-active php8.3-fpm nginx mariadb` (M12.log dòng 4046):

```
php8.3-common	8.3.6-0ubuntu0.24.04.13
php8.3-fpm	8.3.6-0ubuntu0.24.04.13
tpan-a_example_com_7653e4.conf
tpan-b_example_com_cd59d5.conf
tpan-idle.conf
root     php-fpm: master process (/etc/php/8.3/fpm/php-fpm.conf)
tp_a_ex+ php-fpm: pool tpan-a_example_com_7653e4
tp_b_ex+ php-fpm: pool tpan-b_example_com_cd59d5
active
active
active
```

Gói đã lên bản mới, `www.conf` không trở lại, không có tiến trình pool `www`, không có câu hỏi conffile nào. `tpan update --check` lúc này in `Không có gói nào của stack đang chờ cập nhật.`, và `tpan health` đạt hết cho máy và hai site.

### 2.7 Bật bản vá tự động (phần C)

```bash
scripts/vm.sh root tpan update auto --yes
scripts/vm.sh exec cat /etc/apt/apt.conf.d/52tpan-unattended-upgrades
scripts/vm.sh exec bash -c 'apt-config dump | grep -i -A3 Origins-Pattern'
```

```
Bắt đầu job 13fb602531c39730
- Cài gói: unattended-upgrades ...
  đã đúng, bỏ qua
- Bật tự vá trong /etc/apt/apt.conf.d/52tpan-unattended-upgrades ...
  xong
  [đạt] Bản vá bảo mật tự cài hằng ngày

Job 13fb602531c39730: thành công
```

```
// # Do tpan quản lý. Sửa tay sẽ bị tpan health báo lệch.
//
// Bản vá bảo mật tự cài mỗi ngày (unattended-upgrades). File này đọc sau
// 20auto-upgrades và 50unattended-upgrades của Ubuntu nên giá trị ở đây thắng.
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";

// Danh sách trong apt.conf được cộng dồn: các origin dưới đây thêm vào danh
// sách của Ubuntu (kho -security vẫn được vá), không thay nó.
Unattended-Upgrade::Origins-Pattern {
    "origin=deb.sury.org";
};
```

```
Unattended-Upgrade::Origins-Pattern "";
Unattended-Upgrade::Origins-Pattern:: "origin=deb.sury.org";
CommandLine "";
CommandLine::AsString "apt-config dump";
```

Kho sury có trong danh sách vì file `tpan-sury.sources` còn sau khi gỡ 8.4. Chạy lại `update auto --yes`: cả hai bước `đã đúng, bỏ qua`.

### 2.8 Hồi quy kịch bản lỗi 02 với socket mang số bản (phần G)

```bash
scripts/faults/02-pool-socket-rm.sh break b.example.com
scripts/vm.sh root tpan health b.example.com --fix --yes
```

Kịch bản lỗi xoá `/run/php/tpan-b_example_com_cd59d5-8.3.sock`. Health báo hai lỗi, `php.socket` (`Không có socket /run/php/tpan-b_example_com_cd59d5-8.3.sock`) và `http.site` (`Site trả 502`), sửa tự động khởi động lại `php8.3-fpm`, rồi cả 8 phép kiểm đạt lại (M12.log dòng 4249 tới 4356). Tức là phép kiểm và sửa tự động của M4 đã theo đường dẫn socket mới.

### 2.9 Nâng MariaDB 10.11 lên 11.8 trên bản sao (phần D)

Nâng MariaDB không hoàn tác được, nên kịch bản làm trên một bản sao của máy ảo, rồi xoá bản sao. Chạy trên Mac:

```bash
multipass stop tpan-m12
multipass clone tpan-m12 --name tpan-m12-clone
multipass start tpan-m12
multipass start tpan-m12-clone
```

Dữ liệu trước khi nâng, đọc cho từng site (số bài viết, `siteurl`, và `CHECKSUM TABLE wp_posts`, một số tính từ toàn bộ nội dung bảng):

```
posts=1
siteurl=http://a.example.com
tp_a_example_com_7653e4.wp_posts	2141919730
```

```
posts=1
siteurl=http://b.example.com
tp_b_example_com_cd59d5.wp_posts	2256471371
```

Xem trước:

```bash
env TPAN_VM=tpan-m12-clone scripts/vm.sh root tpan mariadb upgrade --to 11.8 --dry-run
```

```
Kế hoạch: Nâng MariaDB từ dòng 10.11 lên 11.8. Một chiều, không tự hoàn tác: đường lùi là cài lại dòng 10.11 rồi nhập lại /var/lib/tpan/backups/mariadb-upgrade-20261119-224930.sql
6 bước sẽ chạy, 1 bước đã đúng sẽ bỏ qua, 3 bước có thể gián đoạn dịch vụ.

 1. [sẽ chạy] Xuất toàn bộ MariaDB ra /var/lib/tpan/backups/mariadb-upgrade-20261119-224930.sql  (mariadb.dump:...)
    ...
 2. [sẽ chạy] Dừng mariadb  (service.stop:mariadb)
    ...
 3. [sẽ chạy] Thêm kho của MariaDB (dòng 11.8)  (apt.repo:mariadb)
    ...
    tạo /etc/apt/sources.list.d/tpan-mariadb.sources
    xoá /etc/apt/preferences.d/tpan-mariadb
    Ghi chú hoàn tác: Không gỡ kho khi hoàn tác: nếu gói đã kịp nâng thì máy cần kho này để nhận bản vá của dòng 11.8; gỡ thì apt không còn biết gói đang chạy từ đâu ra.
 4. [sẽ chạy] Nâng gói: galera-4 mariadb-client mariadb-client-core mariadb-common mariadb-plugin-provider-bzip2 mariadb-plugin-provider-lz4 mariadb-plugin-provider-lzma mariadb-plugin-provider-lzo mariadb-plugin-provider-snappy mariadb-server mariadb-server-core  (apt.upgrade:...)
    ...
 5. [sẽ chạy] Ghi nguồn MariaDB mới vào settings (mariadb.org, dòng 11.8)  (settings:stack.db_source=mariadb.org:stack.db_line=11.8)
    Vì sao: tpan đọc settings để biết máy đang dùng kho nào; ghi ngay sau khi nâng gói để dù bước sau lỗi, tpan vẫn nói đúng dòng đang chạy.
    Ghi chú hoàn tác: Không hoàn tác: bước này chỉ chạy sau khi gói đã nâng, mà gói đã nâng thì máy đang chạy dòng mới; trả settings về dòng cũ là để tpan nói sai về máy.
 6. [đã đúng, bỏ qua] Chạy và bật mariadb khi khởi động  (service:mariadb)
 7. [sẽ chạy] Chạy mariadb-upgrade  (mariadb.upgrade:localhost)
    ...
```

(Bản đầy đủ ở M12.log dòng 4442 tới 4490.) Hai điều trông lạ trong bản xem trước:

- Bước 6 ghi "đã đúng, bỏ qua" vì lúc xem trước MariaDB đang chạy. Lúc chạy thật, sau bước 2 thì nó sẽ được hỏi lại.
- Bước 3 ghi "xoá /etc/apt/preferences.d/tpan-mariadb" dù file đó không có. Kho MariaDB không ghim, nên `AptRepo` coi "file ghim phải không có" là một thay đổi và in ra. Đây chỉ là chữ, đã ghi ở mục "Còn mở" của `docs/thay-doi/M12.md`.

Chạy thật, với một đầu dò nền gọi `mariadb -N -e "SELECT 1"` mỗi 0,2 giây để đo thời gian dừng:

```bash
env TPAN_VM=tpan-m12-clone scripts/vm.sh root tpan mariadb upgrade --to 11.8 --yes --verbose
```

Output dài (M12.log dòng 4500 tới 4826). Khung chính:

```
Bắt đầu job c2706134891f5876
- Xuất toàn bộ MariaDB ra /var/lib/tpan/backups/mariadb-upgrade-20261119-224932.sql ...
    $ mariadb-dump --all-databases --single-transaction --routines --events --triggers --result-file=/var/lib/tpan/backups/mariadb-upgrade-20261119-224932.sql
    $ tail -n 1 -- /var/lib/tpan/backups/mariadb-upgrade-20261119-224932.sql
    | -- Dump completed on 2026-11-19 22:49:32
...
  xong
- Dừng mariadb ...
  xong
- Thêm kho của MariaDB (dòng 11.8) ...
  xong
- Nâng gói: galera-4 mariadb-client ... mariadb-server mariadb-server-core ...
  xong
- Ghi nguồn MariaDB mới vào settings (mariadb.org, dòng 11.8) ...
  xong
- Chạy và bật mariadb khi khởi động ...
  đã đúng, bỏ qua
- Chạy mariadb-upgrade ...
  đã đúng, bỏ qua
  [đạt] Dịch vụ mariadb đang chạy và tự bật cùng máy
  [đạt] MariaDB an toàn: root dùng unix_socket, không user ẩn danh, không DB test

Job c2706134891f5876: thành công
```

Tên file xuất ở bản xem trước là `...-224930.sql`, ở lần chạy thật là `...-224932.sql`: tên lấy theo giờ lúc dựng Plan, và mỗi lệnh dựng Plan một lần.

Bước "Chạy và bật mariadb" bị bỏ qua vì gói `mariadb-server` tự khởi động dịch vụ khi cài xong. Bước `mariadb-upgrade` bị bỏ qua vì gói cũng tự chạy nó. Bằng chứng:

```bash
env TPAN_VM=tpan-m12-clone scripts/vm.sh root bash -c 'cat /var/lib/mysql/mariadb_upgrade_info; echo; systemctl cat mariadb | grep -E "^Exec"; journalctl -u mariadb --no-pager -o cat | grep -iE "upgrade|debian-start|Phase" | tail -15'
```

```
11.8.10-MariaDB

ExecStart=/bin/sh -c "set -f; [ ! -e /usr/bin/galera_recovery ] && VAR= || \
ExecStartPost=!/etc/mysql/debian-start
2026-11-19 22:49:56 0 [Note] InnoDB: Upgraded the change buffer: 0 tablespaces, 0 pages
```

`ExecStartPost=!/etc/mysql/debian-start` là lệnh systemd chạy sau mỗi lần MariaDB khởi động; script đó gọi `mariadb-upgrade`, và file `mariadb_upgrade_info` đã ghi `11.8.10-MariaDB`. `MariaDBUpgradeRun.Check` so số này với bản đang chạy và thấy khớp.

Thời gian dừng, theo đầu dò:

```
số lần dò: 164 ; số lần không vào được: 106
lần lỗi đầu: 1795103372.756094801 ; lần lỗi cuối: 1795103397.751312379
thời gian dừng (lần đạt cuối trước khi dừng -> lần đạt đầu sau khi chạy lại): 25.6 giây
```

Sau khi nâng:

```
mariadb from 11.8.10-MariaDB, client 15.2 for debian-linux-gnu (aarch64) using  EditLine wrapper
```

```
posts=1
siteurl=http://a.example.com
tp_a_example_com_7653e4.wp_posts	2141919730
```

```
posts=1
siteurl=http://b.example.com
tp_b_example_com_cd59d5.wp_posts	2256471371
```

Cùng số bài, cùng `siteurl`, cùng checksum với trước khi nâng. Hai site trả 200, health trên bản sao đạt hết, `tpan stack status --json` có `"db_source": "mariadb.org"`. Thêm vài kiểm khác (M12.log dòng 5096 tới 5135):

```
--bind-address=127.0.0.1
--bind-address=127.0.0.1
LISTEN 0      80         127.0.0.1:3306      0.0.0.0:*    users:(("mariadbd",pid=3698,fd=52))
```

```
root	localhost	mysql_native_password	[{}, {"plugin": "unix_socket"}]
```

```
drwx------ 2 root root    4096 Nov 19 22:49 .
drwxr-xr-x 4 root root    4096 Nov 19 22:50 ..
-rw------- 1 root root 2712329 Nov 19 22:49 mariadb-upgrade-20261119-224932.sql
/var/lib/tpan/backups/mariadb-upgrade-20261119-224932.sql
-- Dump completed on 2026-11-19 22:49:32
```

`99-tpan.cnf` của tpan vẫn được đọc (chỉ nghe 127.0.0.1), root vẫn dùng `unix_socket`, file xuất 0600 trong thư mục 0700, dòng cuối là `-- Dump completed`. Trong lúc nâng, apt có dòng `Installing new version of config file /etc/mysql/mariadb.conf.d/50-server.cnf ...` (M12.log dòng 4739) dù có `--force-confold`; câu hỏi 5 ở mục 6 giải thích vì sao điều đó không sao. Kịch bản xoá bản sao sau khi xong.

### 2.10 self-update và quay về (phần E)

Chưa có bản phát hành thật (M11), nên kịch bản build hai bản `m12-a` và `m12-b`, cài `m12-a` và chạy `tpan setup` để có `tpan.service`, rồi phục vụ `m12-b` bằng `python3 -m http.server 18081 --bind 127.0.0.1 --directory /srv` ngay trong máy ảo. Thư mục `/srv/tpan-rel` có `tpan-linux-arm64` và `SHA256SUMS` đúng; `/srv/tpan-rel-bad` có cùng file chạy nhưng `SHA256SUMS` toàn số 0.

SHA256SUMS sai (chạy trên Mac):

```bash
scripts/vm.sh root tpan self-update --url http://127.0.0.1:18081/tpan-rel-bad --yes
```

```
Bắt đầu job 146a1c79ee17436b
- Tải bản tpan mới vào /usr/local/bin/tpan.new ...
  LỖI: bước self.download:/usr/local/bin/tpan.new: thực hiện: tpan-linux-arm64 tải về có sha256 ed764cc334c27ddd28cf559583a57922f2ad88e0be4b9cd480917ec22cdaeca8, SHA256SUMS ghi 0000000000000000000000000000000000000000000000000000000000000000; đã bỏ file /usr/local/bin/tpan.new
    | 0000000000000000000000000000000000000000000000000000000000000000  tpan-linux-arm64

Job 146a1c79ee17436b: hỏng, đã hoàn tác sạch
```

Mã thoát 1; `sha256sum /usr/local/bin/tpan` trước và sau đều là `0d3d9cc9...3282`; không còn `tpan.new`. Bản đúng:

```bash
scripts/vm.sh root tpan self-update --url http://127.0.0.1:18081/tpan-rel --yes
scripts/vm.sh exec bash -c 'tpan version; ls -la /usr/local/bin/ | grep tpan; /usr/local/bin/tpan.old version'
scripts/vm.sh root bash -c 'p=$(systemctl show -p MainPID --value tpan); readlink /proc/$p/exe; /proc/$p/exe version'
```

```
Bắt đầu job b6503f8a70b332ca
- Tải bản tpan mới vào /usr/local/bin/tpan.new ...
  xong
- Thay /usr/local/bin/tpan bằng /usr/local/bin/tpan.new, giữ bản cũ ở /usr/local/bin/tpan.old ...
  xong
- Khởi động lại tpan ...
  xong
  [đạt] File chạy /usr/local/bin/tpan chạy được

Job b6503f8a70b332ca: thành công
Kiểm chứng:
  [đạt] File chạy /usr/local/bin/tpan chạy được: tpan m12-b (commit d7a899e, build 2026-11-19T15:45:03Z)
tpan đã đổi: m12-a -> m12-b
```

```
tpan m12-b (commit d7a899e, build 2026-11-19T15:45:03Z)
-rwxr-xr-x  1 root root 19267744 Nov 19 22:50 tpan
-rwxr-xr-x  1 root root 19267744 Nov 19 22:50 tpan.old
tpan m12-a (commit d7a899e, build 2026-11-19T15:45:03Z)
```

```
/usr/local/bin/tpan
tpan m12-b (commit d7a899e, build 2026-11-19T15:45:03Z)
```

Lệnh thứ ba hỏi chính tiến trình của `tpan.service` (qua `/proc/<pid>/exe`) đang chạy bản nào: `m12-b`, và PID đổi từ 1503 sang 2162. Quay về:

```bash
scripts/vm.sh root tpan self-update --rollback --yes
```

```
Bắt đầu job c753e6710594cf4b
- Thay /usr/local/bin/tpan bằng /usr/local/bin/tpan.old, giữ bản cũ ở /usr/local/bin/tpan.new ...
  xong
- Khởi động lại tpan ...
  xong
  [đạt] File chạy /usr/local/bin/tpan chạy được

Job c753e6710594cf4b: thành công
Kiểm chứng:
  [đạt] File chạy /usr/local/bin/tpan chạy được: tpan m12-a (commit d7a899e, build 2026-11-19T15:45:03Z)
tpan đã quay về bản trước: m12-b -> m12-a
```

`sha256sum /usr/local/bin/tpan` lại là `0d3d9cc9...3282`: giống hệt file ban đầu.

### 2.11 nginx.org và MariaDB 11.8 ngay lúc cài stack (phần F)

Từ máy sạch (chạy trên Mac):

```bash
make vm-reset
make vm-deploy
scripts/vm.sh root tpan stack install --web nginx --web-source nginx.org --db-source mariadb.org --db-line 11.8 --yes
```

Plan có 23 bước (bản xem trước ở M12.log dòng 5738 tới 5832). So với Plan của M1, nó có thêm hai bước thêm kho ở đầu và năm bước thích nghi bố cục của gói nginx.org:

```
- Thêm kho nginx.org (stable) ...
  xong
- Thêm kho của MariaDB (dòng 11.8) ...
  xong
...
- Chạy và bật nginx khi khởi động ...
  xong
- Tạo thư mục /etc/nginx/sites-available ...
  xong
- Tạo thư mục /etc/nginx/sites-enabled ...
  xong
- Cho nginx của nginx.org đọc sites-enabled ...
  xong
- Gỡ /etc/nginx/conf.d/default.conf ...
  xong
- Thêm user nginx vào nhóm www-data, khởi động lại nginx ...
  xong
```

Kết quả:

```
nginx version: nginx/1.30.7
mariadb-server	1:11.8.10+maria~ubu2404
nginx	1.30.7-1~noble
mariadb from 11.8.10-MariaDB, client 15.2 for debian-linux-gnu (aarch64) using  EditLine wrapper
uid=999(nginx) gid=988(nginx) groups=988(nginx),33(www-data)
/etc/nginx/conf.d:
tpan.conf

/etc/php/8.3/fpm/pool.d:
tpan-idle.conf
root     php-fpm: master process (/etc/php/8.3/fpm/php-fpm.conf)
nginx    nginx: worker process
nginx    nginx: worker process
```

Worker của nginx chạy dưới user `nginx` (gói của Ubuntu dùng `www-data`), và `nginx` đã thuộc nhóm `www-data`. Site `c.example.com` tạo sau đó trả 200, health đạt hết. Chạy lại cùng lệnh cài: mọi bước `đã đúng, bỏ qua`, không khởi động lại nginx.

### 2.12 Chưa chạy

Những việc dưới đây có mã nhưng không có trong `M12.log`:

- **API và hai màn giao diện.** `M12.log` không có request nào tới `/api/v1/php`, `/api/v1/updates`, `/api/v1/mariadb/upgrade` hay `/api/v1/sites/{domain}/php`. Màn Stack, màn Cập nhật và khối PHP ở trang site chưa được mở trên máy ảo. Kiểm: `scripts/vm.sh root tpan serve --demo` rồi mở trình duyệt, hoặc gọi `curl -sk https://<vm>:8443/api/v1/updates` với cookie đăng nhập (chưa chạy).
- **unattended-upgrades chạy thật.** Mới có `unattended-upgrade --dry-run -d`. Lần chạy thật hằng đêm chưa được quan sát. Kiểm: `scripts/vm.sh root unattended-upgrade -d` rồi `scripts/vm.sh exec cat /var/log/unattended-upgrades/unattended-upgrades.log` (chưa chạy).
- **`MariaDBUpgradeRun.Apply`.** Bước này bị bỏ qua vì gói đã tự chạy `mariadb-upgrade`. Nhánh tpan tự gọi `mariadb-upgrade` chỉ có unit test.
- **Apache.** Không lượt nào của M12 dùng Apache: đổi bản PHP, `update`, nâng MariaDB đều chạy với nginx.
- **Đổi postrotate của logrotate sang `/run/nginx.pid`.** Không lệnh nào trong log chạy `logrotate -f`. Kiểm: `scripts/vm.sh root logrotate -f /etc/logrotate.d/tpan-sites` rồi xem nginx còn ghi vào `access.log` mới không (chưa chạy).
- **amd64 và VPS.** Máy ảo là arm64. Các lệnh cho VPS ở cuối `docs/thay-doi/M12.md` do chủ dự án tự gõ.

## 3. Đi qua mã theo thứ tự chạy

Mọi chuyện bắt đầu từ một đổi nhỏ ở `internal/site/types.go` (3.1). Sau đó là sáu luồng: `php` (3.3 tới 3.6), cài stack có nguồn (3.7), `update --check` và `update` (3.8, 3.9), `update auto` (3.10), `mariadb upgrade` (3.11), `self-update` (3.12). Cuối cùng là API và giao diện (3.13).

### 3.1 `internal/site/types.go`: `SocketFor`

```go
// Có số bản trong tên vì đổi bản PHP không gián đoạn (mục 6.11) cần hai pool
// của cùng site chạy song song trong lúc chuyển, mỗi bản PHP-FPM một socket.
// Cùng một đường dẫn thì PHP-FPM bản cũ, khi gỡ pool, xoá mất socket mà bản
// mới vừa tạo.
func (l Layout) SocketFor(version string) string {
	return "/run/php/" + l.Pool + "-" + version + ".sock"
}
```

Trước M12, `Layout` có trường `Socket` cố định `/run/php/tpan-<slug>.sock`. Trong lúc đổi bản, pool của site có mặt ở cả hai master (8.3 và 8.4) vài giây. Nếu cùng tên socket, master 8.4 tạo socket mới đè lên đường dẫn đó, rồi lúc gỡ pool ở 8.3, master 8.3 xoá "socket của nó", mà đó lại là file master 8.4 vừa tạo. Site mất socket: 502. Có số bản trong tên thì mỗi master giữ file riêng.

Vì socket giờ phụ thuộc bản PHP, mọi chỗ dùng socket đều phải biết bản: template vhost nhận `.Socket`, `steps.SitePool(l, phpVersion)` nhận bản, `checks.PoolSocket(l, php)` nhận bản, `tpan site info --json` bỏ `layout.socket` và thêm trường `socket` ở cấp trên. Site tạo trước M12 vẫn chạy bằng file trên đĩa, nhưng `php.socket` sẽ báo lỗi cho tới khi chạy lại `sudo tpan site add <domain>` (ghi ở `docs/thay-doi/M12.md`).

### 3.2 `cmd/tpan/planned.go`: `runPlanned`, và `core.Planned`

Mọi lệnh mới của M12 (php, update, mariadb, self-update) đều dựng một `core.Planned` rồi giao cho `runPlanned`. `Planned` (ở `internal/core/types_m12.go`) là một Plan đã qua điều kiện đầu, cộng một hàm `after` chạy khi job thành công:

```go
func (p *Planned) Execute(ctx context.Context, onEvent func(engine.Event)) (engine.Report, error) {
	if p.journal == nil {
		return engine.Report{}, fmt.Errorf("%s: thiếu Journal, không ghi được lịch sử job", p.Plan.Kind)
	}
	rep, err := engine.New(p.sys, engine.Options{Journal: p.journal, OnEvent: onEvent}).Execute(ctx, p.Plan)
	...
	if rep.Job.Status != engine.JobSucceeded || p.after == nil {
		return rep, nil
	}
	if err := p.after(ctx, rep); err != nil {
		return rep, fmt.Errorf("job %s xong nhưng chưa ghi được kết quả: %w", rep.Job.ID, err)
	}
	return rep, nil
}
```

`runPlanned` làm đúng nhịp của `tpan stack install`: `--dry-run` thì in bản xem trước; không `--yes` thì in xem trước rồi hỏi; rồi chạy, in tiến độ, ghi audit, in báo cáo. Phiên chính viết file này trước đợt 2 để hai thợ viết core không phải chép cùng một đoạn vào hai nơi (`docs/thay-doi/M12.md`, "Thay đổi so với bảng lập lúc đầu").

### 3.3 `tpan php list`: `PHP.List`

`cmd/tpan/php.go` gọi `core.PHP.List(ctx, notes)` ở `internal/core/php.go`. Hàm này ghép năm nguồn:

- `phpInstalled`: đọc `/etc/php/`, một bản là "đã cài" khi có cả `/etc/php/<v>/fpm` lẫn `/usr/sbin/php-fpm<v>`. Chỉ thư mục thì chưa đủ: `php remove` giữ file cấu hình nên thư mục còn.
- `phpAvailable`: `apt-cache search --names-only '^php[0-9]+\.[0-9]+-fpm$'`, mỗi gói `php<bản>-fpm` là một bản. Không có bản nào ghi cứng trong mã (mục 6.11).
- `phpSources`: `apt-cache madison php<bản>-fpm ...`, mỗi dòng cho biết bản đó đến từ kho nào.
- `repoEnabled(p.Sys, RepoSury)`: có file `tpan-sury.sources` không.
- `sitesByVersion`: đọc bảng `sites`, gom domain theo bản PHP.

`phpSources` có một quyết định đáng chú ý:

```go
		from := out[v]
		from.found = true
		if !strings.Contains(f[2], "packages.sury.org") {
			from.ubuntu = true
		}
		out[v] = from
```

Mọi dòng không phải `packages.sury.org` đều tính là Ubuntu, không chỉ `*.ubuntu.com`. Chú thích của hàm nói lý do: VPS thường dùng mirror của nhà cung cấp; kiểm theo tên `ubuntu.com` thì trên VPS mọi bản đều thành "sury", và `php install 8.3` sẽ đi thêm kho sury.

Cuối cùng `notes` gọi `curl --max-time 5` tới `https://www.php.net/releases/states.php` và `https://api.wordpress.org/core/serve-happy/1.0/?php_version=<bản mặc định>`. Lỗi thì bỏ ghi chú, không lỗi cả lệnh: danh sách vẫn đủ khi máy không ra được mạng. php.net chỉ liệt kê nhánh còn hỗ trợ, nên bản cũ hơn nhánh cũ nhất trong đó được ghi `eol` ("hết hỗ trợ" ở 2.5).

### 3.4 `tpan php install`: `PrepareInstall`, `repoSury`, `AptRepo`, `PHPIdlePool`

`PrepareInstall` kiểm bản dạng `số.số` (`checkPHPVersion`, vì chuỗi này đi vào tên gói, tên dịch vụ, đường dẫn), rồi chọn:

- Ubuntu có bản này (`src[version].ubuntu`): chỉ `AptUpdate`, không thêm kho nào.
- Không: thêm bước `repoSury(...)`. Kho sury đã thêm mà vẫn không có bản này thì từ chối `php_version_unavailable`.

Bước kho do `repoSury` ở `internal/core/repos.go` dựng. Đây là file duy nhất ghi địa chỉ kho và vân tay khoá:

```go
	pkgs := []string{"php-common"}
	for _, v := range slices.Sorted(slices.Values(phpVersions)) {
		if !phpVersionRE.MatchString(v) {
			return nil, fmt.Errorf("bản PHP %q không hợp lệ (dạng 8.3)", v)
		}
		pkgs = append(pkgs, "php"+v+"*")
	}
	pin, err := templates.AptPin([]templates.AptPinRule{
		{Packages: []string{"*"}, Origin: "packages.sury.org", Priority: 100},
		{Packages: pkgs, Origin: "packages.sury.org", Priority: 500},
	})
```

Hai luật ghim, đã thử trên máy ảo trước khi viết (`docs/thay-doi/M12.md`, "Ghim kho sury"):

- Không ghim: `php-fpm` (gói meta, trỏ tới "bản PHP mặc định") nhảy lên 8.4, và `php8.3-*` của Ubuntu bị thay bằng 8.3.36 của kho sury. Bản lớn tự nhảy, điều mục 6.11 cấm.
- Ghim cả kho ở 100: cài `php8.4-fpm` lỗi `php8.4-common : Depends: php-common (>= 2:95~) but 2:93ubuntu2 is to be installed`. Gói 8.4 cần một `php-common` mới hơn bản Ubuntu có.
- Nên: cả kho 100, riêng `php-common` và `php<bản>*` của các bản cài từ kho này 500 (bằng mức của kho Ubuntu, và bản của sury cao hơn nên thắng).

`PrepareInstall` truyền vào `repoSury` mọi bản đang cài từ kho sury cộng bản mới, không chỉ bản mới. Chú thích giải thích: ghi lại file ghim mà thiếu một bản cũ thì bản đó tụt về 100, và lần nâng sau apt đổi nó sang gói Ubuntu (hoặc gỡ vì phụ thuộc `php-common`).

`steps.AptRepo` ở `internal/steps/apt_m12.go` thêm kho bằng ba file: khoá ở `/etc/apt/keyrings/tpan-<tên>.gpg`, sources dạng deb822 ở `/etc/apt/sources.list.d/tpan-<tên>.sources`, ghim ở `/etc/apt/preferences.d/tpan-<tên>`, rồi `apt-get update`. Phần quan trọng là cách lấy khoá, `fetchKey`:

```go
		if res, err := sys.Run(ctx, s.curl(home)); err != nil {
			return withStdout(fmt.Errorf("tải khoá của kho %s từ %s: %w", s.Name, s.KeyURL, err), res)
		}
		if res, err := sys.Run(ctx, gpgKeyring(home, "--import", home+"/key")); err != nil {
			return withStdout(fmt.Errorf("đọc khoá của kho %s (file tải về không phải khoá OpenPGP?): %w", s.Name, err), res)
		}
		if res, err := sys.Run(ctx, s.exportCmd(home)); err != nil {
			return withStdout(fmt.Errorf("tách khoá của kho %s: %w", s.Name, err), res)
		}
		...
		got, err := primaryFprs(ctx, sys, home, out)
		...
		if !trusted(got, s.fprs()) {
			return noMatch
		}
```

Tải khoá vào một thư mục tạm (`withGPGHome`, luôn dọn bằng `defer`, kể cả tắt `gpg-agent`), nhập vào một keyring tạm, chỉ xuất ra những khoá có vân tay ghi sẵn trong `repos.go`, rồi đọc lại vân tay của thứ sắp ghi bằng `gpg --show-keys --with-colons`. Bước kiểm lại thứ hai có lý do riêng: `gpg --export <vân tay>` cũng khớp vân tay của khoá con, rồi xuất nguyên khoá chính của nó. `primaryFprs` chỉ đọc dòng `fpr` đứng ngay sau `pub`, bỏ dòng `fpr` sau `sub`. Mọi việc này xong rồi mới đụng tới `/etc`: khoá lạ thì máy không đổi gì.

`AptRepo.Check` so khoá bằng vân tay chứ không so byte, vì cùng một khoá tải ở hai lần có thể khác byte (máy chủ của kho thêm chữ ký mới vào khoá).

Sau bước cài gói là `steps.PHPIdlePool` (`internal/steps/php_m12.go`):

```go
	if err := sys.WriteFileAtomic(idle, conf, phpIdleMode, system.Root); err != nil {
		return restore(fmt.Errorf("ghi %s: %w", idle, err))
	}
	if prevWWW.exists {
		if err := sys.Remove(www); err != nil {
			return restore(fmt.Errorf("gỡ %s: %w", www, err))
		}
	}
	what := "thay pool www của PHP " + v + " bằng tpan-idle"
	if err := runTest(ctx, sys, test, what); err != nil {
		// Dịch vụ chưa nạp gì: chỉ cần trả file.
		return restore(err)
	}
```

Ghi `tpan-idle.conf` trước, gỡ `www.conf` sau, để không lúc nào `pool.d` trống. Kiểm `php-fpm<v> -t` trước khi reload; sai thì trả cả hai file. Nội dung pool giữ chỗ ở `internal/templates/files/php-idle-pool.conf.tmpl` (bản đã sinh ở 2.1): `pm = ondemand` với `pm.max_children = 1` nghĩa là không ai gọi thì không có tiến trình con; socket `root:root 0600` nghĩa là web server (chạy dưới `www-data`) không mở được nó.

Kiểm chứng của `php.install`: dịch vụ chạy, `php-fpm<bản> -t`, và `checks.PHPPoolWWWAbsent(bản)` (`internal/checks/m12.go`), phép kiểm chỉ liệt kê thư mục `pool.d`: còn `www.conf` là `Warn`, thiếu `tpan-idle.conf` cũng là `Warn`.

### 3.5 `tpan php use`: `PrepareUse` và `midCheck`

`cmd/tpan/php.go`, hàm `phpUseCmd`, kiểm domain rồi gọi `runPHPPlan`, hàm này coi lời từ chối `php_same_version` là "không đổi gì" với mã thoát 0 (output cuối 2.4). `PrepareUse` ở `internal/core/php.go` từ chối khi site không `active` hoặc bản mới chưa cài (`php_not_installed`), rồi dựng Plan theo thứ tự này:

```go
	ss := []engine.Step{pool, &midCheck{
		id:    "php.use.socket:" + dom,
		check: checks.PoolSocket(l, version),
		tries: socketTries,
		...
	}}
	ss = append(ss, vhost...)
	ss = append(ss,
		&midCheck{id: "php.use.check:" + dom, check: http, need: 2, ...},
		&steps.Absent{Path: l.PoolConf(old), ...},
```

1. `pool`: `steps.SitePool(l, version)` ghi file pool vào `/etc/php/<mới>/fpm/pool.d/`, kiểm `php-fpm<mới> -t`, `systemctl reload php<mới>-fpm`.
2. `midCheck` chờ socket: chạy `checks.PoolSocket(l, version)` tối đa `socketTries = 20` lần, cách nhau `midCheckWait = 1` giây.
3. `vhost`: `siteVhost` với site đã đổi `PHPVersion`, nên `fastcgi_pass` trỏ socket mới; kiểm `nginx -t`, reload.
4. `midCheck` gọi HTTP: phải đạt hai lần liên tiếp (`need: 2`).
5. `steps.Absent` gỡ pool ở bản cũ, kiểm, reload bản cũ.

Kiểm chứng cuối là toàn bộ phép kiểm của site với bản mới. Hàm `after` ghi bản mới vào bảng `sites`: chỉ khi job thành công.

`midCheck` là một phép kiểm đóng vai một bước của Plan:

```go
func (s *midCheck) Apply(ctx context.Context, sys system.System) error {
	var r checks.Result
	tries, need, streak := cmp.Or(s.tries, midCheckTries), max(s.need, 1), 0
	for i := range tries {
		if i > 0 {
			... // chờ midCheckWait, hoặc dừng nếu job bị huỷ
		}
		if r = s.check.Run(ctx, sys); r.Status != checks.Pass && r.Status != checks.Warn {
			streak = 0
			continue
		}
		if streak++; streak >= need {
```

Không đạt thì bước lỗi, và engine hoàn tác các bước trước theo thứ tự ngược: vhost về socket cũ, pool mới bị gỡ. Site vẫn chạy bản cũ. `Check` của `midCheck` trả `s.passed` (false cho tới khi chính nó chạy đạt), nên bản xem trước luôn ghi "sẽ chạy". `Undo` không làm gì, vì phép kiểm không đổi gì trên máy.

Vì sao cần hai bước kiểm giữa chừng thì xem mục 5.4: bản đầu không có chúng, và máy ảo cho thấy 597 request 502.

### 3.6 `tpan php remove`: `PrepareRemove`

Từ chối nếu là bản mặc định (`php_default`) hoặc còn site dùng (`php_in_use`). `sitesByVersion` đếm cả site đang trong thùng rác: khôi phục lại thì site đó vẫn cần bản PHP của nó. Plan chỉ có một bước `steps.AptRemove` với các gói `php<v>-*` đang cài, dùng `apt-get remove` (không `purge`): file cấu hình ở lại, nên cài lại thì cấu hình cũ trở lại. Hệ quả phụ: `/etc/php/<v>/fpm/pool.d/tpan-idle.conf` cũng ở lại, và health vẫn theo dõi nó (dòng `drift:/etc/php/8.4/fpm/pool.d/tpan-idle.conf` ở 2.5). Việc này nằm trong "Còn mở".

### 3.7 `tpan stack install --web-source --db-source --db-line`: `PrepareInstallWith`

`cmd/tpan/stack.go` có ba cờ mới, đưa vào `core.InstallOptions`. `PrepareInstallWith` ở `internal/core/stack_sources.go`:

```go
	cfg.Web = web
	if cfg, err = withSources(cfg, opts, firstInstall); err != nil {
		return nil, err
	}
	...
	repos, err := stackRepos(s.Sys, cfg)
	if err != nil {
		return nil, err
	}
	...
	// Kho ngoài phải có trước apt-get update và apt-get install: gói nginx,
	// mariadb-server mới được lấy từ kho đã chọn.
	plan.Steps = append(repos, plan.Steps...)
```

- `withSources`: lần đầu thì lấy lựa chọn của người dùng. Chạy lại thì cờ trống là giữ như đã ghi, còn cờ khác đã ghi thì từ chối `source_change_unsupported`. Đổi nguồn của một stack đang chạy (gỡ nginx của Ubuntu, cài nginx.org) là một Plan khác hẳn, không phải "chạy lại".
- `stackRepos`: dựng `repoNginx` (ghim 900 cho origin `nginx.org`, như hướng dẫn của nginx.org, để gói của kho này thắng gói Ubuntu) và `repoMariaDB` (không ghim: bản của kho MariaDB luôn cao hơn bản Ubuntu). Tên mã `noble` đọc từ `VERSION_CODENAME` trong `/etc/os-release` (`osCodename`), không ghi cứng, để lên Ubuntu 26.04 không phải sửa mã.

Gói nginx của nginx.org có bố cục khác gói Ubuntu: chỉ đọc `/etc/nginx/conf.d/*.conf`, không có `sites-available`, `sites-enabled`, không có `fastcgi.conf`, và chạy dưới user `nginx`. `nginxOrgSteps` thích nghi:

- tạo hai thư mục `sites-available`, `sites-enabled`;
- ghi `/etc/nginx/conf.d/tpan.conf` (`templates.NginxOrgInclude`) có dòng `include /etc/nginx/sites-enabled/*;` và `server_names_hash_bucket_size 64;`;
- gỡ `/etc/nginx/conf.d/default.conf`;
- `groupRestart`: thêm `nginx` vào nhóm `www-data`, rồi khởi động lại nginx.

Thư mục site là `2750` thuộc nhóm `www-data` (mục 5); worker dưới user `nginx` mà không trong nhóm này thì trả 403 cho mọi file tĩnh. Thành viên nhóm chỉ có hiệu lực với tiến trình mới, nên phải khởi động lại. `groupRestart` gộp hai việc vào một bước để chỉ khởi động lại khi thật sự vừa thêm user:

```go
func (s *groupRestart) Apply(ctx context.Context, sys system.System) error {
	if err := s.add.Apply(ctx, sys); err != nil {
		return err
	}
	if err := s.restart().Apply(ctx, sys); err != nil {
		// Engine không Undo bước có Apply lỗi: tự bỏ user ra khỏi nhóm.
		return errors.Join(err, s.add.Undo(ctx, sys))
	}
	return nil
}
```

Nếu dùng `steps.ServiceRestart` đứng riêng thì `Check` của nó luôn sai cho tới khi chính nó chạy, nên mỗi lần chạy lại `stack install` sẽ khởi động lại nginx: gián đoạn mà không đổi gì. Lần chạy lại ở 2.11 đúng là không khởi động lại.

Gói nginx.org không có `fastcgi.conf`, nên template vhost (`internal/templates/files/site-nginx.conf.tmpl`) đổi sang `include fastcgi_params;` cộng một dòng `fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;`. Với gói Ubuntu thì hai cách cho cùng kết quả: `fastcgi.conf` của Ubuntu chính là `fastcgi_params` cộng dòng đó.

Với mọi nguồn, `installPlan` ở `internal/core/stack.go` đặt `steps.PHPIdlePool{Version: php, ...}` ngay sau `steps.DetectPHP`. `php` là một con trỏ `*string` dùng chung: `DetectPHP` ghi bản vào đó lúc chạy, `PHPIdlePool` đọc nó lúc chạy. Lúc job bắt đầu, con trỏ còn trỏ chuỗi rỗng, nên tên bước in `php<bản>` (2.1, 2.11). Chú thích `ponytail:` ở `stack.go` ghi hệ quả thứ hai: engine đọc `Info()` một lần lúc bắt đầu, nên trên máy mới `tpan-idle.conf` không vào danh sách file tpan quản lý; chạy lại `stack install` thì vào.

### 3.8 `tpan update --check`: `Updates.Status`, `parseAptInst`, `RunAptUpdate`

`cmd/tpan/update.go`, `updateCmd`, với `--check` gọi `core.Updates.Status(ctx, true)` ở `internal/core/updates.go`. `refresh = true` nghĩa là lấy khoá toàn cục của tpan rồi chạy `apt-get update` qua `steps.RunAptUpdate`. Sau đó là `apt-get -s -o Debug::NoLocking=1 dist-upgrade`: `-s` là mô phỏng, apt chỉ in việc sẽ làm. Mỗi gói sẽ nâng là một dòng `Inst`, ví dụ (M12.log dòng 3864):

```
Inst php8.3-fpm [8.3.6-0maysync1] (8.3.6-0ubuntu0.24.04.13 Ubuntu:24.04/noble-updates, Ubuntu:24.04/noble-security [arm64]) []
```

`parseAptInst` cắt dòng đó: tên, bản cũ trong `[...]`, bản mới là chữ đầu trong `(...)`, phần còn lại là danh sách kho. Gói là bản vá bảo mật khi một trong các kho có đuôi `-security`:

```go
		p := PendingPackage{Name: name, From: from, To: to, Origins: []string{}}
		for o := range strings.SplitSeq(kho, ",") {
			if o = strings.TrimSpace(o); o != "" {
				p.Origins = append(p.Origins, o)
				p.Security = p.Security || strings.HasSuffix(o, "-security")
			}
		}
```

`stackPackage` tách gói "của stack" (tên bắt đầu bằng `nginx`, `apache2`, `mariadb-`, `galera-`, `php`, `certbot`, `python3-certbot`, `curl`, `libcurl`) khỏi phần còn lại. Gói khác của hệ điều hành chỉ được đếm ("55, bảo mật 33"): bản vá bảo mật của chúng do unattended-upgrades lo.

`steps.RunAptUpdate` ở `internal/steps/apt.go`:

```go
func RunAptUpdate(ctx context.Context, sys system.System, cmd system.Cmd) (system.Result, error) {
	for i := 1; ; i++ {
		res, err := sys.Run(ctx, cmd)
		if err == nil || i >= listsLockTries || !strings.Contains(err.Error()+res.Stderr+res.Stdout, "/var/lib/apt/lists/lock") {
			return res, err
		}
		select {
		case <-ctx.Done():
			return res, errors.Join(err, ctx.Err())
		case <-time.After(listsLockWait):
		}
	}
}
```

Chạy lại tối đa 60 lần, mỗi lần cách 5 giây (5 phút), nhưng chỉ khi lỗi nhắc đúng `/var/lib/apt/lists/lock`. Lỗi khác (mạng, kho hỏng) trả về ngay. Mục 5.4 kể vì sao có hàm này.

Phần MariaDB của màn: `mariadbVersion` hỏi `dpkg-query` bản của `mariadb-server`, `dbLineOfVersion` cắt ra dòng ("1:10.11.14-0ubuntu0.24.04.1" ra "10.11"), `mariadbUpgrades` hỏi `https://downloads.mariadb.org/rest-api/mariadb/` và chỉ giữ dòng `Long Term Support` + `Stable` cao hơn dòng đang chạy. Không mạng thì danh sách trống.

Dòng "Kho ngoài được tự vá" lấy từ `externalOrigins`, hàm này chỉ xem kho nào đã thêm (có file `tpan-<tên>.sources`), không đọc file `52tpan-unattended-upgrades`. Vì vậy ở 2.6 nó in `deb.sury.org` trước khi `update auto` chạy. Câu đó đúng nghĩa là "sẽ được tự vá khi bạn bật", chưa phải "đang được tự vá".

### 3.9 `tpan update`: `PrepareApply`, `restartsFor`, `verifyAll`, health sau job

`PrepareApply` đọc lại gói chờ, giữ gói của stack đã cài (có bản cũ), không có gói nào thì từ chối `nothing_to_update`. Plan:

```go
		Steps:  append([]engine.Step{&aptRefresh{}, &steps.AptUpgrade{Packages: pkgs}}, restartsFor(pkgs)...),
		Verify: verify,
```

- `aptRefresh` luôn chạy `apt-get update`. `steps.AptUpdate` của M1 không dùng được: nó bỏ qua khi mọi gói đã cài, mà gói cần nâng thì luôn đã cài.
- `steps.AptUpgrade` chạy `apt-get install -y --only-upgrade ... -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold <gói>`. `--only-upgrade` để không cài thêm gói chưa có; hai tuỳ chọn `force-conf...` để dpkg không bao giờ hỏi "giữ hay thay file cấu hình" (mục 4). `Check` của nó chạy bản `-s` và tìm dòng `Inst` của đúng các gói đó; không thấy là đã đúng. Không có `Undo`: apt không hạ gói về an toàn.
- `restartsFor`: từ tên gói suy ra dịch vụ cần khởi động lại, theo thứ tự web server, PHP-FPM từng bản, MariaDB. Mỗi `ServiceRestart` có `Test` (`nginx -t`, `apachectl configtest`, `php-fpm<v> -t`), sai thì không khởi động lại, và `OnlyIfActive: true`: sau khi chuyển stack, web server cũ đang tắt; bật nó lên là tranh cổng 80 với web server đang dùng. Ở 2.6 chỉ có gói `php8.3-*` nên chỉ có `php8.3-fpm` được khởi động lại.

`verifyAll` chỉ có phép kiểm của stack và `php-fpm<v> -t` của mọi bản đã cài, không có phép kiểm từng site:

```go
// verifyAll là kiểm chứng sau khi đổi gói: phép kiểm của stack và `php-fpm<bản>
// -t` của mọi bản PHP khác bản mặc định. Không có phép kiểm của từng site: một
// site hỏng từ trước không được làm job "lỗi" (gói đã nâng không hạ về được);
// CLI và API chạy lại toàn bộ health ngay sau job, site nào đang đạt mà giờ
// không đạt thì mở sự cố (mục 6.11). Stack chưa cài thì không có gì để kiểm.
```

Phần "chạy lại toàn bộ health" nằm ở `healthAfterJob` trong `cmd/tpan/update.go` (in "Kiểm lại toàn bộ health sau khi đổi gói" ở 2.6) và ở `healthAfter` trong `internal/server/api_m12.go` cho API. Cả hai chạy health kể cả khi job hỏng: gói đã nâng thì máy đã đổi, phải biết site nào hỏng.

### 3.10 `tpan update auto`: `PrepareAuto`

Hai bước: `AptInstall unattended-upgrades` (có sẵn trên ảnh Ubuntu, nên bỏ qua), và một `steps.File` ghi `/etc/apt/apt.conf.d/52tpan-unattended-upgrades` từ `templates.UnattendedUpgrades(origins)`, với `Test: apt-config dump`: file sai cú pháp thì `apt-config` lỗi và `File` trả nội dung cũ về.

Số `52` để file được đọc sau `20auto-upgrades` và `50unattended-upgrades` của Ubuntu: với giá trị đơn (`"1"`), file đọc sau thắng. Với danh sách (`Origins-Pattern { ... }`), apt cộng dồn chứ không thay: dòng `Unattended-Upgrade::Origins-Pattern:: "origin=deb.sury.org";` ở 2.7 là một phần tử thêm vào danh sách của Ubuntu. Origin là giá trị dòng `Origin:` trong file Release của kho, ghi ở `repos.go`: `deb.sury.org`, `nginx`, `MariaDB`. Template tự thêm `origin=` trước mỗi giá trị.

Kiểm chứng là `checks.UnattendedUpgrades` (`internal/checks/m12.go`): hỏi `apt-config dump APT::Periodic::Unattended-Upgrade` (giá trị hiệu lực sau khi gộp mọi file) và `dpkg-query` xem gói đã cài chưa. Giá trị "1" mà gói chưa cài thì không có gì chạy cả, nên phải hỏi cả hai.

### 3.11 `tpan mariadb upgrade --to 11.8`: `PrepareMariaDB` và ba Step một chiều

`cmd/tpan/mariadb.go` gọi `core.Updates.PrepareMariaDB(ctx, line)`. Từ chối nếu dòng sai dạng (`mariadb_line_invalid`), chưa cài MariaDB, hoặc dòng không cao hơn dòng đang chạy (`mariadb_not_newer`: chỉ nâng, không hạ). Plan:

```go
		Steps: []engine.Step{
			&steps.MariaDBDump{Path: dump},
			&steps.ServiceStop{Unit: "mariadb"},
			&keepOnUndo{Step: repo, why: i18n.T(
				...)},
			&afterStep{Step: &steps.AptUpgrade{Packages: pkgs}, dep: repo},
			&settingsStep{s: u.Settings, kv: [][2]string{{SettingDBSource, string(SourceMariaDB)}, {SettingDBLine, line}},
				...},
			&steps.Service{Unit: "mariadb"},
			&steps.MariaDBUpgradeRun{},
		},
		Verify: []checks.Check{checks.Service("mariadb", checks.StageDB), checks.MariaDBSecure()},
```

- `steps.MariaDBDump` (`internal/steps/apt_m12.go`): tạo file rỗng `0600` trước (để không lúc nào file chứa dữ liệu mà quyền rộng hơn), rồi `mariadb-dump --all-databases --single-transaction --routines --events --triggers --result-file=...`. Kiểm file xong bằng `tail -n 1`: dòng cuối phải bắt đầu bằng `-- Dump completed`. Đọc dòng cuối bằng `tail` chứ không đọc cả file, vì file có thể vài GB. Lỗi giữa chừng thì xoá file dở: để lại thì người đọc tưởng đó là bản xuất dùng được.
- `keepOnUndo` (`internal/core/oneway.go`) bọc bước thêm kho: `Undo` không làm gì.
- `afterStep` bọc bước nâng gói: chỉ coi là "đã đúng" khi bước thêm kho cũng đã đúng.
- `settingsStep` ghi `stack.db_source = mariadb.org` và `stack.db_line = 11.8` như một bước của Plan, ngay sau khi nâng gói, không đợi job thành công.

Ba Step bọc này có chung một lý do, ghi ở đầu `oneway.go`: Plan một chiều có một điểm không quay lại (gói đã nâng thì dữ liệu đã nâng). Bước nào sau điểm đó lỗi thì engine hoàn tác các bước trước. Nếu hoàn tác gỡ kho MariaDB, máy chạy 11.8 mà apt không còn biết gói đến từ đâu, không nhận bản vá. Nếu settings chỉ ghi ở `after` (khi job thành công), một lỗi ở `MariaDBUpgradeRun` để settings nói "ubuntu" trong khi máy chạy 11.8. Vì vậy kho ở lại, và settings ghi ngay khi điều đó trở thành sự thật.

Kiểm chứng chỉ có dịch vụ MariaDB và `MariaDBSecure`; phép kiểm từng site do health đầy đủ chạy sau job (`healthAfterJob`), cùng lý do như 3.9.

`MariaDBUpgradeRun.Check` so phần số của bản trong `/var/lib/mysql/mariadb_upgrade_info` với `SELECT VERSION()`, so bằng nhau chứ không so tiền tố ("11.4.10" bắt đầu bằng "11.4.1" mà không phải cùng bản). Trên bản sao, gói đã tự chạy `mariadb-upgrade` nên bước này bỏ qua đúng (2.9).

### 3.12 `tpan self-update`: `SelfUpdate.Prepare`, `SelfDownload`, `SelfInstall`

`cmd/tpan/selfupdate.go` dựng `core.SelfUpdate` với `Arch: runtime.GOARCH` (amd64 hoặc arm64, kiến trúc mà chính file tpan đang chạy được build cho). `Prepare` ở `internal/core/selfupdate.go`:

```go
	got := new(string)
	dl := &steps.SelfDownload{Base: baseURL, Arch: s.Arch, Dest: tpanNew, NewVersion: got}
	...
		Steps: []engine.Step{
			dl,
			&sameVersionSkip{Step: &steps.SelfInstall{Bin: TpanBin, New: tpanNew, Old: tpanOld}, got: got, running: s.Version},
			&sameVersionSkip{Step: &steps.ServiceRestart{Unit: "tpan", OnlyIfActive: true}, got: got, running: s.Version},
		},
		Verify: []checks.Check{tpanRuns{}},
```

- `steps.SelfDownload` (`internal/steps/self.go`): tải `SHA256SUMS` trước (lỗi thì chưa đổi gì), tìm dòng của `tpan-linux-<arch>`, tải file vào `/usr/local/bin/tpan.new`, tính sha256 trong Go (không gọi `sha256sum`), so, sai thì xoá file (2.10). Rồi chạy `tpan.new version` để đọc bản mới và ghi vào `*got`. Gốc tải phải là `https://`; `http://` chỉ nhận khi máy là loopback, cho e2e.
- `sameVersionSkip`: bản vừa tải trùng bản đang chạy thì hai bước sau "đã đúng", không thay file, không khởi động lại. Con trỏ `got` được bước 1 ghi và hai bước sau đọc, cùng kiểu với `php` ở 3.7.
- `steps.SelfInstall`: hai lần `rename` trong cùng thư mục, `tpan` thành `tpan.old`, `tpan.new` thành `tpan`. Lỗi ở lần thứ hai thì đổi lần thứ nhất ngược lại ngay.
- `ServiceRestart{Unit: "tpan", OnlyIfActive: true}`: khởi động lại panel nếu nó đang chạy.
- `tpanRuns`: kiểm chứng chạy `/usr/local/bin/tpan version`, dòng đầu phải bắt đầu bằng `tpan `. Không đạt thì engine hoàn tác: `SelfInstall.Undo` đổi ngược hai file.

`PrepareRollback` dùng lại chính `SelfInstall` với vai đổi chỗ: `{Bin: tpan, New: tpan.old, Old: tpan.new}`. Bản đang chạy thành `tpan.new`, bản cũ về `tpan` (dòng "giữ bản cũ ở /usr/local/bin/tpan.new" ở 2.10).

Không có nút self-update trên panel, và không có API. Lý do ghi ở `cmd/tpan/selfupdate.go`, `web/src/screens/cap-nhat/Self.tsx` và "Chỗ khác đặc tả": job chạy trong chính tiến trình `tpan serve`. Bước khởi động lại `tpan.service` giết tiến trình đó giữa job, trước khi kịp chạy kiểm chứng hay ghi kết quả, và nếu bản mới hỏng thì không còn ai hoàn tác (suy luận theo mã; không ai thử chạy self-update từ panel). Chạy từ CLI thì tiến trình `tpan self-update` là tiến trình riêng, sống qua lần khởi động lại của dịch vụ. Màn Cập nhật hiện lệnh để tự gõ bằng `CommandBlock`.

### 3.13 API và giao diện

`internal/server/api_m12.go` thêm các endpoint ghi ở `internal/server/types_m12.go`: `GET /api/v1/php`, `POST /api/v1/php/install|remove`, `POST /api/v1/sites/{domain}/php`, `GET /api/v1/updates`, `POST /api/v1/updates`, `POST /api/v1/updates/auto`, `POST /api/v1/mariadb/upgrade`. Mọi POST đi qua `planJob` như M5: `?dry_run=1` trả bản xem trước, không thì chạy job nền và trả 202. Hai điểm riêng:

- `GET /api/v1/updates` gọi `Status(ctx, false)`: không chạy `apt-get update`, vì việc đó cần khoá toàn cục và có thể mất cả phút. Danh sách gói do `apt-daily` của Ubuntu làm mới; muốn ngay thì `sudo tpan update --check`.
- `GET /api/v1/php` đệm phần ghi chú (php.net, WordPress.org) 1 giờ (`phpNotes`), vì mỗi lần hỏi có thể chờ tới 5 giây mỗi nguồn. Danh sách bản thì đọc mới mỗi lần.

`healthAfter` bọc Plan đổi gói để chạy health đầy đủ ngay sau job, trong goroutine của job.

Màn Stack (`web/src/screens/stack/`) có hai khối tự đọc API riêng: `Config.tsx` (cấu hình, dịch vụ) và `Php.tsx` (bảng bản PHP, nút "Cài" cho bản chưa cài, nút "Gỡ" cho bản đã cài mà không site nào dùng và không phải mặc định). Màn Cập nhật (`web/src/screens/cap-nhat/`) có bốn khối: `Packages.tsx`, `Auto.tsx`, `Mariadb.tsx`, `Self.tsx`. Ba khối đầu cùng một nhịp: bấm, gọi `?dry_run=1`, hiện `PlanReview`, chạy thật, chuyển sang `/jobs/:id`. Khối MariaDB đặt câu cảnh báo "một chiều" ngay trước các nút và nối vào từng nút bằng `aria-describedby`, để người dùng trình đọc màn hình nghe cảnh báo cùng tên nút. Trang một site có thêm `web/src/screens/site/PhpBlock.tsx` để đổi bản PHP. Phần này chưa chạy trên máy ảo (2.12).

## 4. Kiến thức nền

### Pool PHP-FPM, master và "No pool defined"

PHP-FPM chạy một tiến trình master (dưới root) và các tiến trình con theo từng pool. Mỗi pool là một file trong `/etc/php/<bản>/fpm/pool.d/`, có user riêng và socket riêng. Master đọc mọi file trong đó lúc khởi động; không có file nào thì nó từ chối chạy: `php-fpm8.3 -t` báo `ERROR: No pool defined. at least one pool section must be specified in config file`, mã 78 (đã kiểm trước hợp đồng, ghi ở `docs/thay-doi/M12.md`; không có trong `M12.log`). Vì thế không gỡ `www.conf` không mà phải đặt `tpan-idle.conf` thay chỗ. Trong mã: `steps.PHPIdlePool`. Tự xem: `scripts/vm.sh exec bash -c 'ls /etc/php/*/fpm/pool.d/; ps -eo user,cmd | grep [p]hp-fpm'` (2.4, 2.5).

### Reload PHP-FPM là bất đồng bộ

`systemctl reload php8.4-fpm` gửi một tín hiệu cho master rồi trả về ngay; master nạp lại cấu hình sau đó, và socket của pool mới chỉ có khi nó nạp xong. Với 8.4 của kho sury, việc nạp chờ một truy vấn DNS 2 giây. Lệnh trả về thành công không có nghĩa việc đã xong. Trong mã: chú thích của `midCheckTries` và bước `php.use.socket` trong `PrepareUse`. Tự xem: bảng sự kiện job ở 2.4 (`reload` lúc `38.554`, socket có lúc `41.579`). Lệnh `scripts/vm.sh exec systemctl cat php8.3-fpm` để xem dòng `ExecReload` thì chưa chạy.

### Unix socket và quyền của nó

Unix socket là một file đặc biệt (chữ `s` đầu dòng `ls -la`) mà hai tiến trình trên cùng máy dùng để nói chuyện, thay cho cổng TCP. Ai mở được socket do quyền file quyết định: socket của site là `www-data:www-data 0660` (nginx chạy dưới `www-data` mở được), socket của `tpan-idle` là `root:root 0600` (chỉ root). Trong mã: `site.Layout.SocketFor`, `listen.mode` trong `php-idle-pool.conf.tmpl`. Tự xem: `scripts/vm.sh exec ls -la /run/php` (2.1, 2.4).

### Kho apt, deb822 và `Signed-By`

apt cài gói từ các kho ghi trong `/etc/apt/sources.list.d/`. Dạng deb822 là dạng "khoá: giá trị" nhiều dòng (`Types`, `URIs`, `Suites`, `Components`). `Signed-By` nói kho này chỉ được tin khi file Release được ký bằng khoá trong đúng file đó. Không có `Signed-By` (hay khoá nằm ở `trusted.gpg.d`) thì mọi khoá trên máy ký được cho mọi kho: khoá của kho sury ký được cả gói giả mạo `nginx`. Trong mã: `templates.AptSources`, `steps.AptKeyPath`. Tự xem: `scripts/vm.sh exec cat /etc/apt/sources.list.d/tpan-sury.sources` (2.2).

### Vân tay khoá, khoá chính và khoá con

Một khoá OpenPGP gồm một khoá chính và có thể vài khoá con. Vân tay là một chuỗi 40 ký tự hex tính từ khoá, đổi một bit là đổi vân tay. tpan ghi sẵn vân tay khoá chính của từng kho trong `internal/core/repos.go`; khoá tải về chỉ được ghi vào máy khi khoá chính khớp. Kẻ chiếm được đường tải khoá (hay chính trang web của kho) cũng không đưa được khoá của họ vào. Trong mã: `primaryFprs`, `trusted`. Tự xem: `scripts/vm.sh exec gpg --show-keys --with-colons /etc/apt/keyrings/tpan-sury.gpg`: dòng `fpr` sau `pub` là khoá chính, sau `sub` là khoá con (2.2).

### Ghim apt và `Candidate`

Mỗi bản của một gói có một độ ưu tiên. Kho thường là 500, bản đang cài 100. apt chọn bản có ưu tiên cao nhất; cùng ưu tiên thì chọn bản số cao hơn. Bản được chọn là `Candidate`. File ghim trong `/etc/apt/preferences.d/` đổi ưu tiên theo gói và theo kho. Kho sury ghim 100 thì bản của nó, dù số cao hơn, thua bản Ubuntu ở 500. Trong mã: `templates.AptPin`, `repoSury`, `repoNginx`. Tự xem: `scripts/vm.sh exec apt-cache policy php-fpm php8.3-fpm` (2.2).

### `apt-get -s` và dòng `Inst`

`-s` (simulate) cho apt tính mọi việc sẽ làm mà không làm, in mỗi gói sẽ cài thành một dòng `Inst <tên> [<bản cũ>] (<bản mới> <kho>, <kho> [<kiến trúc>])`. Không cần root, không đổi gì, nên dùng được cho cả màn Cập nhật lẫn `Check` của bước nâng gói. Trong mã: `aptSimulateCmd`, `parseAptInst`, `AptUpgrade.Check`. Tự xem: output `--verbose` ở 2.6 (M12.log dòng 3861 tới 3872).

### Conffile và `--force-confold`

Conffile là file cấu hình mà gói khai là của mình (`dpkg-query -W -f '${Conffiles}\n' php8.3-fpm`). Khi nâng gói mà người quản trị đã sửa file đó, dpkg hỏi "giữ bản của bạn hay lấy bản mới". Không có terminal thì câu hỏi đó treo apt. `--force-confdef` bảo dpkg dùng lựa chọn mặc định nếu có, `--force-confold` bảo giữ bản đang có. File chưa bị sửa thì dpkg cứ thay bằng bản mới, không hỏi. File đã bị xoá thì dpkg coi là người quản trị cố ý xoá, không đem về. Trong mã: `AptUpgrade.cmd`. Tự xem: `scripts/vm.sh exec bash -c 'dpkg-query -W -f "\${Conffiles}\n" php8.3-fpm'` (2.6).

### unattended-upgrades và danh sách cộng dồn của apt.conf

`unattended-upgrades` là chương trình của Ubuntu chạy mỗi ngày, cài bản vá từ các kho nằm trong danh sách cho phép. Hai khoá `APT::Periodic::Update-Package-Lists` và `APT::Periodic::Unattended-Upgrade` bằng `"1"` là bật nó (bộ hẹn giờ systemd gọi nó thì chưa xem trên máy ảo trong M12: `scripts/vm.sh exec systemctl list-timers 'apt-daily*'`, chưa chạy). Ubuntu cho phép sẵn kho `-security`. Cấu hình nằm trong `/etc/apt/apt.conf.d/`; giá trị đơn thì file đọc sau thắng, còn danh sách thì cộng dồn. Trong mã: `templates.UnattendedUpgrades`, `checks.UnattendedUpgrades`. Tự xem: `scripts/vm.sh exec bash -c 'apt-config dump | grep -i -A3 Origins-Pattern'` (2.7).

### `rename` và file đang chạy

Trên Linux, tên file chỉ là một mục trong thư mục trỏ tới một inode (dữ liệu thật). Tiến trình đang chạy giữ inode của file chạy, không giữ tên. `rename` trong cùng phân vùng là nguyên tử: chỉ đổi mục trong thư mục, không lúc nào tên đó trỏ vào file dở. Nên đổi `tpan.new` thành `tpan` trong lúc `tpan serve` đang chạy không làm hỏng tiến trình đó; nó chỉ dùng bản mới khi khởi động lại. Trong mã: `steps.SelfInstall`. Tự xem: `scripts/vm.sh root bash -c 'p=$(systemctl show -p MainPID --value tpan); readlink /proc/$p/exe; /proc/$p/exe version'` (2.10).

### Go: nhúng struct để thay một phương thức

`keepOnUndo` trong `internal/core/oneway.go`:

```go
type keepOnUndo struct {
	engine.Step
	why i18n.Text
}

func (k *keepOnUndo) Undo(context.Context, system.System) error { return nil }
```

Trường `engine.Step` không có tên: đó là nhúng (embedding). `keepOnUndo` tự có mọi phương thức của bước bên trong (`Check`, `Apply`), nên nó cũng là một `engine.Step`. Rồi nó tự định nghĩa `Undo` (và `Info`), và hai cái đó thắng cái của bước bên trong. `afterStep` (thay `Check`) và `sameVersionSkip` (thay `Check`) cũng viết theo cách này. Nhờ vậy `steps.AptRepo` không phải có cờ "đừng hoàn tác"; chỉ Plan nâng MariaDB cần hành vi đó, nên nó nằm ở core.

## 5. Cách khác đã cân nhắc, và chỗ đã làm sai

### 5.1 Một socket chung cho mọi bản PHP

Giữ `/run/php/tpan-<slug>.sock` như mục 5 thì không phải đổi đường dẫn của site cũ. Nhưng đổi bản không gián đoạn cần hai pool chạy cùng lúc, và hai master không chia nhau được một file socket (3.1). Cái giá đã chấp nhận: site tạo trước M12 cần chạy lại `sudo tpan site add <domain>` một lần, có một thoáng 502 giữa lúc reload PHP-FPM và lúc reload web server.

### 5.2 Ghim cả kho sury ở một mức

Hai cách đơn giản hơn đã thử trên máy ảo trước khi viết hợp đồng, cả hai đều sai (3.4): không ghim thì bản lớn tự nhảy (`php-fpm` lên 8.4, `php8.3-*` đổi sang bản của kho sury); ghim cả kho ở 100 thì không cài được 8.4 vì thiếu `php-common` mới. Cách chọn là ghim theo gói, và liệt kê lại mọi bản đã cài từ kho sury mỗi lần ghi file ghim. Một lựa chọn khác vẫn còn mở: bỏ hẳn kho sury vì chuyện `telemetry.sury.org` (2.3). Chủ dự án quyết.

### 5.3 Kiểm từng site trong kiểm chứng của `update` và `mariadb upgrade`

Plan khác của tpan đều kiểm site trong kiểm chứng, và kiểm chứng hỏng thì hoàn tác. Ở đây hoàn tác không gỡ được gói đã nâng, mà một site hỏng từ trước sẽ làm job "lỗi" rồi kéo hoàn tác những bước còn hoàn tác được, để lại máy ở trạng thái lưng chừng. Nên kiểm chứng chỉ có stack và MariaDB; site do health đầy đủ chạy ngay sau job lo, và mở sự cố cho site đang đạt mà sau đó không đạt (3.9).

### 5.4 Bốn lỗi chỉ thấy trên máy ảo

Unit test dùng `FakeSystem`, nên không có độ trễ của PHP-FPM, không có `apt-daily`, không có apt thật. Cả bốn lỗi dưới đây lộ ra khi chạy trên máy ảo, và hai commit sửa chúng đều thêm test cho đúng trường hợp đó (`git show --stat 6c7cf18`: `php_test.go` thêm 93 dòng, `updates_test.go` thêm 54 dòng; `git show --stat 2bc4ad5`: file mới `apt_lock_test.go`). Tức là bộ test trước đó chưa phủ các trường hợp này.

**5.4 a. `php use` trỏ vhost vào socket chưa có (commit `6c7cf18`).** Bản đầu của `PrepareUse` làm theo mục 6.11 ý 3 nhưng bỏ bước "kiểm socket": ghi pool, reload PHP-FPM, ghi vhost, reload nginx, kiểm HTTP một lần, gỡ pool cũ. Lần chạy e2e đầu (trước `M12.log` hiện tại, nên con số chỉ có ở `docs/thay-doi/M12.md`): vòng lặp curl ghi 597 request 502, rồi job hoàn tác. Nguyên nhân, theo `docs/thay-doi/M12.md`: `systemctl reload php8.4-fpm` trả về ngay, nhưng PHP-FPM 8.4 cần khoảng 2 giây mới có socket mới (khớp với truy vấn DNS đo ở 2.3), nên nginx đã trỏ sang một socket chưa có. Sửa hai chỗ: thêm `midCheck` chờ socket (tối đa 20 giây) trước khi ghi vhost; và kiểm HTTP phải đạt hai lần liên tiếp, vì `systemctl reload nginx` cũng trả về trước khi worker mới nhận việc, nên lần gọi đầu có thể do worker cũ (còn trỏ socket cũ, vẫn chạy) trả lời: một lần đạt có thể là đạt giả. Bằng chứng sau khi sửa ở 2.4: 0 lỗi, chờ 3 giây khi lên 8.4 và 1 giây khi về 8.3. Bài học: lệnh trả về thành công không có nghĩa dịch vụ đã xong việc.

**5.4 b. `apt-get update` không chờ khoá danh sách gói (commit `2bc4ad5`).** Mọi lệnh `apt-get` của M12 có `-o DPkg::Lock::Timeout=300`, để chờ khi `apt-daily` đang giữ khoá. Trên máy ảo vừa khởi động, `apt-daily` đang chạy `apt-get update`, và lệnh của tpan lỗi ngay: `Could not get lock /var/lib/apt/lists/lock`. Hoá ra apt 2.8.3 chỉ áp tuỳ chọn đó cho khoá của dpkg, không áp cho khoá của danh sách gói `/var/lib/apt/lists/lock`. Sửa bằng `steps.RunAptUpdate` (3.8). Bằng chứng ở 2.6: đối chứng thoát 100 ngay, `tpan update --check` chờ 16 giây rồi chạy được. Trên VPS mới, đây là lỗi người dùng gặp ngay ở lệnh đầu tiên.

**5.4 c. Bản xem trước nâng MariaDB in bước nâng gói là "đã đúng" (commit `6c7cf18`).** Lúc xem trước, kho MariaDB chưa được thêm (xem trước không ghi gì), nên `apt-get -s install --only-upgrade mariadb-server` không thấy bản nào mới hơn, và `AptUpgrade.Check` trả "đã đúng, bỏ qua". Người đọc bản xem trước sẽ tưởng nâng MariaDB không nâng gói nào. Thấy được khi đọc output `tpan mariadb upgrade --dry-run` trên máy ảo (chú thích của `afterStep` ghi lại); sau khi sửa, kịch bản e2e có khẳng định "bước nâng gói hiện là sẽ chạy" để lỗi không quay lại. Sửa bằng `afterStep` (3.11): bước nâng gói chỉ "đã đúng" khi bước thêm kho cũng đã đúng. `M12.log` chỉ có output sau khi sửa (2.9, bước 4 ghi "sẽ chạy"); trạng thái trước khi sửa được ghi trong chú thích của `afterStep`.

**5.4 d. Bản PHP đã gỡ vẫn vào kiểm chứng (commit `6c7cf18`).** Sau `php remove 8.4`, kiểm chứng của `update.apply` và `mariadb.upgrade` có dòng `[chưa rõ] Cấu hình PHP-FPM 8.4`: `verifyAll` lấy danh sách bản từ thư mục `/etc/php/<bản>/fpm`, mà `apt-get remove` giữ file cấu hình nên thư mục còn, trong khi `php-fpm8.4` đã mất. Sửa: dùng `phpInstalled`, đòi cả thư mục lẫn file chạy. Bằng chứng ở 2.6: khẳng định "kiểm chứng không có dòng 'chưa rõ' (bản 8.4 đã gỡ không bị kiểm)" đạt.

## 6. Tự kiểm

1. Vì sao socket của pool có số bản PHP trong tên? Nếu giữ `/run/php/tpan-<slug>.sock` cho mọi bản thì hỏng ở bước nào của `php use`?
2. Xoá bước "Chờ socket" khỏi `PrepareUse` thì chuyện gì xảy ra với site đổi sang 8.4 của kho sury? Và vì sao bước kiểm HTTP cần hai lần đạt liên tiếp, một lần chưa đủ?
3. Vì sao tpan không xoá `www.conf` rồi thôi? Pool `tpan-idle` có phải một cửa mới để chạy PHP không?
4. Ghim cả kho sury ở 100 có đủ không? Còn ghim cả kho ở 500?
5. `tpan update` chạy với `--force-confold`, vậy sao log nâng MariaDB vẫn có `Installing new version of config file /etc/mysql/mariadb.conf.d/50-server.cnf`? Và vì sao `www.conf` không quay lại sau khi nâng `php8.3-fpm`?
6. Nâng MariaDB lỗi ở bước `mariadb-upgrade`. Máy đang ở trạng thái nào: kho, gói, settings, file xuất? Bạn lùi về 10.11 bằng cách nào?
7. Vì sao màn Cập nhật không có nút "Cập nhật tpan"?
8. Ở 2.6, `tpan update --check` in "Kho ngoài được tự vá: deb.sury.org" trước khi chạy `update auto`. Câu đó có đúng không?
9. (Cần máy ảo) Sau `tpan php install 8.4`, chứng minh rằng apt sẽ không tự đổi `php8.3-fpm` đang chạy sang bản của kho sury ở lần nâng cấp tới.
10. (Cần máy ảo) Sau `tpan self-update`, file `/usr/local/bin/tpan` đã là bản mới. Làm sao chứng minh panel đang chạy cũng là bản mới, chứ không chỉ file trên đĩa?

## Đáp án

1. Đổi bản không gián đoạn cần pool của site có mặt ở cả master cũ lẫn master mới trong vài giây (mục 6.11 ý 3). Hai master không chia nhau được một file socket: master mới tạo socket đè lên đường dẫn đó, rồi ở bước cuối (`steps.Absent` gỡ pool ở bản cũ, reload bản cũ), master cũ xoá socket của pool nó vừa bỏ, mà file ở đường dẫn đó là của master mới. Site mất socket, nginx trả 502, và kiểm chứng sau đó báo `php.socket` lỗi. Có số bản thì mỗi master giữ file riêng: `tpan-a_example_com_7653e4-8.3.sock` và `-8.4.sock` (2.4). Chưa chạy thử bản không có số bản trên máy ảo; đây là suy luận theo chú thích của `SocketFor` và `docs/thay-doi/M12.md`.
2. Đã thấy thật ở lần chạy đầu (mục 5.4 a): `systemctl reload php8.4-fpm` trả về ngay, PHP-FPM 8.4 mất khoảng 2 giây mới có socket, nginx được trỏ sang socket chưa có, mọi request trong khoảng đó trả 502 (597 request), rồi bước kiểm HTTP hỏng và job hoàn tác. Hai lần đạt liên tiếp vì `systemctl reload nginx` cũng trả về trước khi worker mới nhận việc: worker cũ vẫn trả lời qua socket cũ (vẫn còn, vì pool cũ chưa bị gỡ), nên một lần 200 có thể đến từ bản cũ. Lần đạt thứ hai, cách một giây, mới cho thấy worker mới đã chạy qua socket mới. Bảng sự kiện ở 2.4 cho thấy hai lệnh `curl` ở `41.610` và `42.892`.
3. PHP-FPM từ chối chạy khi không có pool nào (`No pool defined`, mã 78). Máy chưa có site, hoặc bản PHP vừa cài chưa có site nào, thì `www.conf` là pool duy nhất; xoá nó là dịch vụ không khởi động được. `tpan-idle` không phải cửa vào: socket thuộc `root:root` quyền `0600`, nginx chạy dưới `www-data` (hay `nginx`) không mở được, và `pm = ondemand` nghĩa là không có tiến trình con nào chạy sẵn. Tự xem: `scripts/vm.sh exec ls -la /run/php` (dòng `srw------- root root ... tpan-idle-8.3.sock`) và `scripts/vm.sh exec bash -c 'ps -eo user,cmd | grep [p]hp-fpm'` (chỉ có master) ở 2.1.
4. Ghim cả kho ở 100 thì không cài được 8.4: `php8.4-common` cần `php-common (>= 2:95~)`, mà ở 100 apt chọn `php-common` 2:93 của Ubuntu (lỗi ghi ở `docs/thay-doi/M12.md`). Ghim cả kho ở 500 thì kho sury ngang hàng Ubuntu, bản số cao hơn thắng: `php-fpm` nhảy lên 8.4 và `php8.3-fpm` đổi sang 8.3.36 của kho sury ở lần nâng cấp tới, đúng điều mục 6.11 cấm ("bản lớn không bao giờ tự nhảy"). Trường hợp không ghim cho đúng kết quả đó, đã kiểm trước hợp đồng; ghim 500 thì chưa thử trên máy ảo, nhưng 500 bằng mức mặc định nên apt cư xử như không ghim. Cách dùng: cả kho 100, riêng `php-common` và `php8.4*` 500.
5. `--force-confold` chỉ có tác dụng với conffile người quản trị đã sửa: nó bảo dpkg giữ bản đang có thay vì hỏi. `50-server.cnf` chưa ai sửa, nên dpkg thay bằng bản mới mà không hỏi. tpan không sửa file của gói; cấu hình của tpan nằm ở `99-tpan.cnf`, file của riêng tpan, và file đó vẫn được đọc sau khi nâng (`--bind-address=127.0.0.1` ở 2.9). `www.conf` không quay lại vì dpkg coi conffile bị xoá là người quản trị cố ý xoá: `unattended-upgrade --dry-run -d` ghi `conffile /etc/php/8.3/fpm/pool.d/www.conf in missing on the system`, và sau `tpan update --yes` thư mục `pool.d` vẫn không có `www.conf` (2.6).
6. Bước `mariadb-upgrade` đứng sau bước nâng gói, nên: gói đã là 11.8 (không hoàn tác, `AptUpgrade` không có `Undo`); kho MariaDB 11.8 vẫn còn (`keepOnUndo`); settings đã ghi `stack.db_source = mariadb.org`, `stack.db_line = 11.8` (`settingsStep`, không hoàn tác); dịch vụ MariaDB được bật lại (`steps.Service`); file xuất vẫn nằm ở `/var/lib/tpan/backups/mariadb-upgrade-<thời điểm>.sql` (`MariaDBDump` không hoàn tác). Engine hoàn tác ngược các bước trước, nhưng các bước kể trên đều không có gì để hoàn tác; `ServiceStop.Undo` bật lại MariaDB nếu trước bước dừng nó đang chạy (`wasActive`). Lùi về 10.11: không có lệnh tự động. Tiêu đề Plan nói rõ đường lùi: cài lại dòng 10.11 rồi nhập lại file xuất. Trên thực tế là gỡ kho MariaDB, hạ gói về bản Ubuntu (dữ liệu trong `/var/lib/mysql` đã nâng, 10.11 có thể không đọc được nên phải dọn), rồi `mariadb < file.sql`. Chưa chạy trên máy ảo.
7. Job chạy bên trong tiến trình `tpan serve`. Bước khởi động lại `tpan.service` giết chính tiến trình đó giữa job: không có kiểm chứng, không ghi được kết quả, và nếu bản mới hỏng thì không còn ai hoàn tác (chưa chạy; suy luận theo mã). Chạy bằng CLI thì `tpan self-update` là tiến trình riêng, sống qua lần khởi động lại, chạy được `tpanRuns` và hoàn tác nếu cần. Màn Cập nhật hiện ba lệnh để tự gõ (`--dry-run`, chạy thật, `--rollback` khi có `tpan.old`). Đây là chỗ khác mục 9, ghi ở "Chỗ khác đặc tả" của `docs/thay-doi/M12.md`.
8. Chưa đúng. `externalOrigins` chỉ xem kho nào đã thêm (có file `tpan-<tên>.sources`), không đọc `/etc/apt/apt.conf.d/52tpan-unattended-upgrades`. Lúc đó file `52tpan-...` chưa có: lần `update auto --yes` ngay sau in "Bật tự vá trong ... xong" và bản xem trước ghi "tạo /etc/apt/apt.conf.d/52tpan-unattended-upgrades" (2.7). Nghĩa đúng của dòng đó là "kho ngoài sẽ được thêm vào danh sách tự vá khi bật". Để biết thật sự, đọc `apt-config dump | grep -i -A3 Origins-Pattern`. Đây là chỗ mã nên sửa (chữ hoặc cách đọc), chưa sửa trong M12.
9. `scripts/vm.sh exec apt-cache policy php-fpm php8.3-fpm`. Dòng `Candidate` là bản apt sẽ cài ở lần nâng cấp tới. Ở 2.2: `php8.3-fpm` có `Candidate: 8.3.6-0ubuntu0.24.04.13` (bản Ubuntu, ưu tiên 500), dù kho sury có `8.3.36-1+0~...` số cao hơn nhưng ở ưu tiên 100. `php-fpm` cũng giữ `2:8.3+93ubuntu2`, không nhảy lên `2:8.4+101...`. Kiểm thêm: `scripts/vm.sh exec cat /etc/apt/preferences.d/tpan-sury` để thấy hai luật. Để thử thật thì `scripts/vm.sh root apt-get -s upgrade` và tìm dòng `Inst php8.3` (chưa chạy trong M12.log).
10. Hỏi chính tiến trình của dịch vụ: `scripts/vm.sh root bash -c 'p=$(systemctl show -p MainPID --value tpan); readlink /proc/$p/exe; /proc/$p/exe version'`. `/proc/<pid>/exe` trỏ tới inode tiến trình đang chạy, không phải tên file trên đĩa. Ở 2.10 nó in `tpan m12-b`, và `systemctl show -p MainPID` cho PID đổi từ 1503 sang 2162 (dịch vụ đã khởi động lại). Nếu bỏ bước khởi động lại, `tpan version` (đọc file trên đĩa) in `m12-b` mà `/proc/<pid>/exe version` vẫn in `m12-a`: sau khi `rename`, tiến trình cũ vẫn giữ inode cũ, và `readlink` khi đó sẽ trỏ sang `/usr/local/bin/tpan.old` (tên mới của inode cũ). Phần "nếu bỏ bước" chưa chạy; đây là suy luận.
