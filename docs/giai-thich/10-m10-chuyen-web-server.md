# M10: chuyển web server tại chỗ (LEMP ⇄ LAMP)

Đọc kèm: `01-m1-cai-stack.md` (engine, Step, Check rồi Apply rồi Check, hoàn tác ngược), `02-m2-site-wordpress.md` (pool PHP-FPM, socket, vhost), `04-m4-health.md` (phép kiểm lệch, `managed_files`), `05-m5-ssl-va-bao-mat.md` (site mặc định cổng 443), `06-m6-hang-loat-va-thung-rac.md` (thùng rác, khôi phục), `12-m12-phien-ban-va-cap-nhat.md` (`midCheck`, nguồn nginx.org, socket có số bản PHP), `docs/KE-HOACH.md` mục 6.6 và dòng `policy-rc.d` ở mục 16, và `docs/thay-doi/M10.md` (bảng chia việc, chỗ khác đặc tả, kết quả kiểm).

## 1. Mốc này làm được gì

- `sudo tpan stack switch --to apache` đổi một máy đang chạy nginx sang Apache, tại chỗ, với mọi site đang có. `--to nginx` đổi ngược lại. Site không phải tạo lại, DB, mã nguồn và pool PHP-FPM không bị đụng tới.
- Mỗi site đi theo đúng cấu hình của nó: site có SSL được vhost cổng 443, site chạy PHP 8.4 vẫn trỏ socket 8.4, site dựng trên nginx được thêm `.htaccess` để permalink chạy trên Apache.
- Trong lúc đổi, mỗi site chỉ không trả lời khoảng 0,1 đến 0,2 giây (đo ở 2.10). Site nào không chạy được trên web server mới thì job tự hoàn tác: web server cũ chạy lại với đúng file cũ.
- `--dry-run` cho xem trước mọi bước. Máy có site hỏng sẵn thì bị từ chối trước khi đổi gì.
- Khôi phục một site từ thùng rác sau khi đã đổi sang Apache cũng tự ghi `.htaccess` nếu thiếu.

## 2. Chạy thử

Mọi output dưới đây lấy nguyên văn từ `docs/bang-chung/M10.log`: lần chạy `scripts/e2e-m10.sh all` ngày 20/11/2026, 00:54 tới 01:04, dòng cuối là `TỔNG KẾT: 279 khẳng định đạt, 0 khẳng định không đạt`. Máy ảo của mốc này tên `tpan-m10` (ghi trong file `.tpan-vm`), Multipass, Ubuntu 24.04, arm64. Log chia thành phần A, B, E, C, D, E, F, G (phần E, khởi động lại máy, chạy hai lần).

Những điều cần biết khi đọc log:

- Mọi lệnh gõ trên Mac, trong thư mục repo. `scripts/vm.sh exec X` chạy X trong máy ảo; `scripts/vm.sh root X` chạy X trong máy ảo bằng `sudo`.
- Kịch bản in lệnh bằng `$*` nên mất dấu nháy, và nhiều lệnh `bash -c` truyền tham số kiểu `_ apache2 nginx`. Lệnh dưới đây mình viết lại có nháy đúng, tên thay thẳng vào, để bạn dán vào zsh được; output giữ nguyên.
- Giờ đầu mỗi dòng log là giờ Mac (+07). Bảng sự kiện job đọc từ `tpan.db` ghi giờ UTC: `00:57:04` trong log là `17:57:04` trong bảng sự kiện.
- Máy ảo không có tên miền thật. Kịch bản gọi site bằng `curl --resolve <tên>:80:127.0.0.1`.
- Chứng chỉ của `a.example.com` do pebble cấp (máy chủ ACME thử chạy trong máy ảo, chỉ là đồ nghề của e2e).

### 2.1 Máy ban đầu: nginx và 4 site (phần A)

Phần A dựng máy từ snapshot sạch: `stack install --web nginx`, 4 site, SSL cho `a`, PHP 8.4 cho `c`, permalink `/%postname%/` cho `b` và `d`, rồi đưa `d` vào thùng rác (M10.log dòng 3 tới 1112). Kết quả (chạy trên Mac):

```bash
scripts/vm.sh root tpan site list
```

```
DOMAIN                                   TRẠNG THÁI PHP   SSL  TẠO LÚC
a.example.com                            active   8.3   có   2026-11-20 00:55
b.example.com                            active   8.3   không 2026-11-20 00:55
c.example.com                            active   8.4   không 2026-11-20 00:55
d.example.com                            trashed  8.3   không 2026-11-20 00:55
```

Bốn site này phủ bốn trường hợp mà Plan đổi web server phải lo: site SSL, site PHP khác bản mặc định, site có permalink, site trong thùng rác.

### 2.2 Xem trước: `--dry-run` trên máy chưa có apache2 (phần B)

Chạy trên Mac:

```bash
scripts/vm.sh root tpan stack switch --to apache --dry-run
```

Output dài 38 bước (M10.log dòng 1133 tới 1288). Đầu và vài bước tiêu biểu:

```
Kế hoạch: Chuyển LEMP (Nginx) sang LAMP (Apache)
36 bước sẽ chạy, 2 bước đã đúng sẽ bỏ qua, 1 bước có thể gián đoạn dịch vụ.

 1. [sẽ chạy] Làm mới danh sách gói (apt-get update)  (apt.update:lists)
 2. [sẽ chạy] Cài gói nhưng chưa cho chạy: apache2  (apt.install-nostart:apache2)
    ...
    $ write /usr/sbin/policy-rc.d (exit 101)
    $ apt-get install -y -o DPkg::Lock::Timeout=300 apache2
    $ restore /usr/sbin/policy-rc.d (remove it if it was not there)
    $ systemctl disable --now apache2
 3. [sẽ chạy] Module Apache: bật mpm_event proxy_fcgi setenvif rewrite headers http2; tắt mpm_prefork  (apache.modules:apache2)
    ...
    $ a2dismod -q mpm_prefork
    $ a2enmod -q mpm_event proxy_fcgi setenvif rewrite headers http2
    $ apachectl configtest
    (chưa kiểm được lúc xem trước: hỏi a2query module mpm_prefork: chạy a2query -m mpm_prefork: exec: "a2query": executable file not found in $PATH)
```

Để ý bước 3 không có dòng `systemctl restart apache2`: đó là `ApacheModules.NoRestart` (3.5). Dòng "chưa kiểm được lúc xem trước" là đúng: apache2 chưa cài thì chưa có `a2query` để hỏi, nhưng bản xem trước vẫn đủ các lệnh sẽ chạy. Các bước 11 tới 20 là vhost, link và `.htaccess` của ba site active, rồi logrotate. Sau đó:

```
21. [sẽ chạy] Đổi web server: dừng nginx, chạy apache2  (web.swap:nginx:apache2)
    NGUY HIỂM: bước này có thể làm gián đoạn dịch vụ.
    ...
    $ systemctl stop nginx
    $ systemctl start apache2
22. [sẽ chạy] Kiểm a.example.com trả 200 qua apache2  (stack.switch.check:a.example.com)
    Vì sao: nginx chỉ bị tắt hẳn khi mọi site đã chạy được trên apache2. Site không trả 200 thì job dừng ở đây và hoàn tác: apache2 dừng, nginx chạy lại với đúng các file cũ.
...
25. [sẽ chạy] Chạy và bật apache2 khi khởi động  (service:apache2)
    $ systemctl enable --now apache2
26. [sẽ chạy] Không bật nginx khi khởi động  (service.disable:nginx)
    $ systemctl disable nginx
27. [sẽ chạy] Gỡ /etc/nginx/sites-enabled/tpan-a.example.com.conf của nginx  (absent:/etc/nginx/sites-enabled/tpan-a.example.com.conf)
...
30. [đã đúng, bỏ qua] Gỡ /etc/nginx/sites-enabled/tpan-d.example.com.conf của nginx  (absent:/etc/nginx/sites-enabled/tpan-d.example.com.conf)
...
38. [sẽ chạy] Gỡ /etc/nginx/sites-available/tpan-default-ssl.conf của nginx  (absent:/etc/nginx/sites-available/tpan-default-ssl.conf)
```

Bước 30 "đã đúng" vì `d.example.com` đang trong thùng rác: link của nó đã bị gỡ lúc `site rm`, chỉ còn file ở `sites-available` (bước 36 gỡ nó). Kịch bản chụp trạng thái máy trước và sau lệnh này (dịch vụ, gói, cây `/etc/nginx` `/etc/apache2` `/etc/logrotate.d` kèm sha256, số job) và so: `=> ĐẠT: dry-run không đổi gì`.

### 2.3 Chạy thật trong lúc curl gọi liên tục (phần B)

Trước khi đổi, kịch bản bật bốn vòng lặp curl (a-http, a-https, b-http, c-http) chạy nền trong máy ảo; cách chúng chạy ở 2.10. Rồi (chạy trên Mac):

```bash
scripts/vm.sh root tpan stack switch --to apache --yes --verbose
```

`--verbose` in từng lệnh và output của nó. Bước cài gói (M10.log dòng 1339 tới 1469, cắt bớt):

```
- Cài gói nhưng chưa cho chạy: apache2 ...
    $ dpkg-query --show '--showformat=${Package}\t${Status}\n' apache2
    | dpkg-query: no packages found matching apache2
    $ apt-get install -y -o DPkg::Lock::Timeout=300 apache2
    ...
    | Enabling site 000-default.
    | invoke-rc.d: policy-rc.d denied execution of start.
    | Created symlink /etc/systemd/system/multi-user.target.wants/apache2.service -> /usr/lib/systemd/system/apache2.service.
    | /usr/sbin/policy-rc.d returned 101, not running 'start apache2.service'
    ...
    $ systemctl is-enabled apache2
    | enabled
    $ systemctl is-active apache2
    | inactive
    $ systemctl disable --now apache2
    | Synchronizing state of apache2.service with SysV service script with /usr/lib/systemd/systemd-sysv-install.
    | Executing: /usr/lib/systemd/systemd-sysv-install disable apache2
    | Removed "/etc/systemd/system/multi-user.target.wants/apache2.service".
    ...
    $ systemctl is-enabled apache2
    | disabled
    $ systemctl is-active apache2
    | inactive
  xong
```

Ba dòng giữa là điều cả mốc này xoay quanh. `policy-rc.d denied execution of start`: gói muốn chạy apache2 và bị chặn. Ngay dòng sau, `Created symlink .../multi-user.target.wants/apache2.service`: gói vẫn bật apache2 khi khởi động máy, vì `policy-rc.d` không chặn việc đó. Sau `apt-get`, apache2 là `inactive` (không chạy) nhưng `enabled` (sẽ chạy khi khởi động lại). Lệnh `systemctl disable --now apache2` gỡ đúng cái link đó. Khái niệm ở mục 4.

Bước đổi web server (dòng 1700 tới 1711):

```
- Đổi web server: dừng nginx, chạy apache2 ...
    $ systemctl is-active nginx
    | active
    $ systemctl is-active nginx
    | active
    $ systemctl stop nginx
    $ systemctl start apache2
    $ systemctl is-active nginx
    | inactive
    $ systemctl is-active apache2
    | active
  xong
```

Hai lần `is-active nginx` đầu: một của `Check` (bước đã đúng chưa), một của `Apply` (nhớ nginx có đang chạy không, để biết có nên bật lại khi lỗi). Hai lần cuối là `Check` sau `Apply`. Cuối job (dòng 3534 tới 3546):

```
Job 4d8947adca0384d9: thành công
Kiểm chứng:
  [đạt] Dịch vụ apache2 đang chạy và tự bật cùng máy: apache2 đang chạy và tự bật khi khởi động máy
  [đạt] Cấu hình Apache hợp lệ: Cấu hình Apache hợp lệ
  [đạt] Site mặc định của Apache trả 403 cho request lạ: Apache trả 403 cho request lạ như mong đợi
  [đạt] Vhost của site có, do tpan quản lý và đang được bật: Vhost của site có, đúng dòng đầu của tpan, và đã được bật
  [đạt] Gọi HTTPS vào site trả 200 và đúng là WordPress của site: Site trả 200 qua https, đúng là WordPress của a.example.com
  ...
Kiểm lại toàn bộ health sau job:
  Stack và mọi site vẫn đạt.
```

Bảng sự kiện của job (đọc từ `tpan.db`, giờ UTC, dòng 3642 tới 3729, cắt bớt) cho thấy thời gian từng bước:

```
  17:56:54.063 step.start apt.install-nostart:apache2 
  17:57:03.459 step.done apt.install-nostart:apache2 
  ...
  17:57:04.370 step.start file:/etc/logrotate.d/tpan-sites 
  17:57:04.465 step.done file:/etc/logrotate.d/tpan-sites 
  17:57:04.466 step.start web.swap:nginx:apache2 
  17:57:04.562 step.done web.swap:nginx:apache2 
  17:57:04.563 step.start stack.switch.check:a.example.com 
  17:57:06.134 step.done stack.switch.check:a.example.com 
  ...
  17:57:09.250 step.start service:apache2 
  17:57:09.740 step.done service:apache2 
  17:57:09.741 step.start service.disable:nginx 
  17:57:10.210 step.done service.disable:nginx 
  ...
  17:57:10.430 job.done  succeeded
```

Cả job mất khoảng 18,5 giây: hơn nửa là `apt-get update` và `apt-get install` (khoảng 11,6 giây), khoảng 4,7 giây là ba bước kiểm site; bước đổi web server mất 96 ms. Kịch bản còn đọc journal của systemd để chắc apache2 không chạy lần nào trước bước đổi (dòng 3783 tới 3788):

```
số lần Started apache2.service trong journal: 1
  Started lúc +84 ms so với step.start của web.swap
Started trước web.swap: 0
  => ĐẠT: apache2 không chạy lần nào trước bước web.swap
  => ĐẠT: apache2 Started đúng một lần (ở web.swap)
```

Một chỗ lạ trong output `--verbose`: bước bật module cơ bản (bước 3 của bản xem trước) được in là `- Module Apache: bật ssl ...` (dòng 1470), dù lệnh nó chạy là `a2enmod -q proxy_fcgi rewrite headers http2`. Đó là tên sai, không phải bước sai. Nguyên nhân: bước module cơ bản và bước bật `ssl` (của M5) có cùng ID `apache.modules:apache2`. `newProgress` trong `cmd/tpan/print.go` dựng bảng tên bước theo ID (`pr.titles[info.ID] = info.Title`), nên bước sau ghi đè tên của bước trước. Bản xem trước in đúng vì nó đọc tên từ từng bước. Engine chạy theo vị trí của bước chứ không theo ID, nên chuyện này chỉ ảnh hưởng chữ in ra và bảng `job_events`. Xem mục 5.6.

### 2.4 Sau khi đổi: dịch vụ, cổng, file của nginx, phép kiểm lệch (phần B)

Chạy trên Mac:

```bash
scripts/vm.sh exec bash -c 'for u in apache2 nginx; do echo "$u $(systemctl is-active $u) $(systemctl is-enabled $u 2>&1)"; done; ls -la /usr/sbin/policy-rc.d 2>&1'
```

```
apache2 active enabled
nginx inactive disabled
ls: cannot access '/usr/sbin/policy-rc.d': No such file or directory
```

`policy-rc.d` không còn: tpan đặt nó trong lúc `apt-get` rồi gỡ ngay. Cổng (`scripts/vm.sh root ss -tlnp`, cắt bớt):

```
LISTEN 0      511                *:80               *:*    users:(("apache2",pid=33331,fd=4),("apache2",pid=33330,fd=4),("apache2",pid=33323,fd=4))
LISTEN 0      511                *:443              *:*    users:(("apache2",pid=33331,fd=6),("apache2",pid=33330,fd=6),("apache2",pid=33323,fd=6))
```

File tpan của nginx đã gỡ hết, file của Apache có đủ:

```bash
scripts/vm.sh root find /etc/nginx -name '*tpan*'
scripts/vm.sh root find /etc/apache2 -name '*tpan*'
```

Lệnh đầu không in gì. Lệnh sau:

```
/etc/apache2/sites-enabled/000-tpan-default-ssl.conf
/etc/apache2/sites-enabled/000-tpan-default.conf
/etc/apache2/sites-enabled/tpan-c.example.com.conf
/etc/apache2/sites-enabled/tpan-b.example.com.conf
/etc/apache2/sites-enabled/tpan-a.example.com.conf
/etc/apache2/sites-available/000-tpan-default-ssl.conf
/etc/apache2/sites-available/000-tpan-default.conf
/etc/apache2/sites-available/tpan-c.example.com.conf
/etc/apache2/sites-available/tpan-b.example.com.conf
/etc/apache2/sites-available/tpan-a.example.com.conf
```

So bảng `managed_files` với đĩa bằng một đoạn Python đọc `tpan.db` (dòng 4036 tới 4088, cuối output):

```
số dòng: 17 ; lệch hoặc thiếu file: 0
dòng còn trỏ vào /etc/nginx: 0
file tpan của /etc/apache2 trên đĩa: 6 ; không có dòng: 0
  => ĐẠT: mọi dòng managed_files khớp sha256 trên đĩa
  => ĐẠT: không dòng nào còn trỏ vào /etc/nginx
  => ĐẠT: mọi file tpan của apache trên đĩa (và logrotate) đều có dòng
```

"6 file" là 5 file thật trong `sites-available` cộng `/etc/logrotate.d/tpan-sites`; 5 cái trong `sites-enabled` là symlink, không có dòng. 17 dòng gồm cả file của PHP, MariaDB, kho sury và `.htaccess` của ba site. Rồi `tpan health` đạt hết và `tpan health --fix --dry-run` không có gì để sửa (dòng 4157, 4248). Phép kiểm lệch còn làm việc: kịch bản thêm một dòng vào vhost Apache của `b.example.com`, và health báo đúng một lệch của đúng file đó (dòng 4620, 4621):

```
  => ĐẠT: drift:/etc/apache2/sites-available/tpan-b.example.com.conf Fail
  => ĐẠT: chỉ phép kiểm lệch của đúng file đó Fail
```

`nginx -t` vẫn chạy được khi nginx tắt và Apache giữ cổng (dòng 4355):

```
nginx: the configuration file /etc/nginx/nginx.conf syntax is ok
nginx: configuration file /etc/nginx/nginx.conf test is successful
```

Mọi site, gồm permalink của `b` (dòng 3800 tới 3810): `a-http 301 https://a.example.com/`, `a-https 200`, `b-http 200`, `c-http 200`, `b-permalink 200`. Vhost Apache của `c` trỏ socket 8.4, của `b` trỏ 8.3 (dòng 3819 tới 3823):

```
/etc/apache2/sites-available/tpan-c.example.com.conf:59:        SetHandler "proxy:unix:/run/php/tpan-c_example_com_6e5b26-8.4.sock|fcgi://localhost"
/etc/apache2/sites-available/tpan-b.example.com.conf:59:        SetHandler "proxy:unix:/run/php/tpan-b_example_com_cd59d5-8.3.sock|fcgi://localhost"
```

### 2.5 Khôi phục site từ thùng rác trên Apache (phần B)

`d.example.com` vào thùng rác lúc máy còn chạy nginx, nên thư mục của nó không có `.htaccess`. Chạy trên Mac:

```bash
scripts/vm.sh root tpan site restore d.example.com --yes
```

```
Bắt đầu job 4e396446eacd76d8
- Chuyển thư mục của d.example.com ra khỏi thùng rác ...
  xong
- Ghi /var/www/d.example.com/public/.htaccess ...
  xong
- Mở khoá user Linux tp_d_example_com_316ffc của d.example.com ...
  xong
...
Job 4e396446eacd76d8: thành công
```

Sau đó `d-http: 200` và `d-permalink: 200` (dòng 4444, 4445). Bước thứ hai là phần M10 thêm vào `trash.go` (3.12).

### 2.6 Chuyển ngược về nginx (phần C)

```bash
scripts/vm.sh root tpan stack switch --to nginx --dry-run
```

```
Kế hoạch: Chuyển LAMP (Apache) sang LEMP (Nginx)
32 bước sẽ chạy, 3 bước đã đúng sẽ bỏ qua, 1 bước có thể gián đoạn dịch vụ.
```

Chạy thật bằng `scripts/vm.sh root tpan stack switch --to nginx --yes --verbose`, với năm vòng lặp curl (thêm `d`). Job thành công, mọi site 200, nginx `active enabled`, apache2 `inactive disabled`, không còn file tpan nào trong `/etc/apache2` (dòng 5500 tới 8958). Chạy lại lần nữa thì bị từ chối mà không tạo job (dòng 9011 tới 9024):

```bash
scripts/vm.sh root tpan stack switch --to nginx --yes --json
```

```
{
  "error": {
    "code": "stack_same_web",
    "message": {
      "vi": "stack đang chạy LEMP (Nginx); không có gì để chuyển",
      "en": "the stack already runs LEMP (Nginx); nothing to switch"
    }
  }
}
```

Lúc máy chạy Apache, kịch bản cài lại gói `nginx-common` mà không đặt `policy-rc.d`, giống `unattended-upgrades` vá gói nginx trên máy đã chuyển sang Apache (dòng 5138 tới 5163):

```
gói chứa nginx.service: nginx-common 1.24.0-2ubuntu7.20
trước: nginx inactive disabled; apache2 active
ls: cannot access '/usr/sbin/policy-rc.d': No such file or directory
apt-get mã 0
Setting up nginx-common (1.24.0-2ubuntu7.20) ...
nginx.service is a disabled or a static unit not running, not starting it.
...
sau khi cài lại (không policy-rc.d): nginx inactive disabled; apache2 active
```

Unit đã `disabled` thì script của gói tự để yên. Đây là lý do thứ hai bước `ServiceDisable` quan trọng: không chỉ cho lần khởi động lại, mà cho mọi lần vá gói về sau.

### 2.7 Từ chối và hoàn tác (phần D, máy đang chạy nginx)

D0. Xoá socket của pool `b.example.com` (kịch bản lỗi 02 của M4) rồi đổi web server (dòng 9161 tới 9224, cắt bớt):

```bash
scripts/faults/02-pool-socket-rm.sh break b.example.com
scripts/vm.sh root tpan stack switch --to apache --yes
```

```
Từ chối: có site đang không trả lời đúng trên web server hiện tại; sửa nó trước (xem tpan health), không thì bước kiểm sau khi đổi chắc chắn lỗi và cả job hoàn tác
  [lỗi] Gọi HTTP vào site trả 200 và đúng là WordPress của site: Site trả 502: web server không nói chuyện được với PHP
      Nguyên nhân: Pool PHP-FPM của site không chạy, socket sai chủ hay quyền, hoặc pool quá tải (504).
```

Mã thoát 1, không tạo job (`=> ĐẠT: không tạo job (số job: 14)`).

D1. Lỗi sớm: kịch bản đặt một file hỏng `/etc/apache2/conf-enabled/zz-hong.conf` để `apachectl configtest` lỗi, rồi đổi sang Apache (dòng 9316 tới 9378, cắt bớt):

```
- Ghi /etc/apache2/sites-available/000-tpan-default.conf ...
    $ apachectl configtest
    | AH00526: Syntax error on line 1 of /etc/apache2/conf-enabled/zz-hong.conf:
    | Invalid command 'DongSaiCuPhapE2E', perhaps misspelled or defined by a module not included in the server configuration
  LỖI: bước file:/etc/apache2/sites-available/000-tpan-default.conf: ...

Job 8cf5ae6458086a6f: hỏng, đã hoàn tác sạch
```

Lỗi xảy ra trước bước đổi, nên nginx không hề dừng: mọi site vẫn 200, cây `/etc/nginx` giống hệt trước (so sha256), không file tpan nào sót trong `/etc/apache2` (dòng 9405 tới 9445).

D2. Lỗi sau khi đổi: thêm một dòng sai cú pháp vào `.htaccess` của `b.example.com`. nginx không đọc `.htaccess` nên `b` vẫn 200 trên nginx (qua được điều kiện đầu); trên Apache thì 500. Job chạy tới bước kiểm `b`, thử 5 lần, lỗi, rồi hoàn tác ngược (dòng 10571 tới 10736, cắt bớt):

```
  LỖI: bước stack.switch.check:b.example.com: thực hiện: http.site:b.example.com (fail) sau 5 lần thử: Site trả 500: PHP hoặc WordPress báo lỗi; ...
- Hoàn tác: Kiểm a.example.com trả 200 qua apache2 ...
  không có gì để hoàn tác: Bước này chỉ gọi thử, không đổi gì trên máy.
- Hoàn tác: Đổi web server: dừng nginx, chạy apache2 ...
    $ systemctl stop apache2
    $ systemctl start nginx
  đã hoàn tác
- Hoàn tác: Ghi /etc/logrotate.d/tpan-sites ...
...
- Hoàn tác: Ghi /etc/apache2/sites-available/000-tpan-default.conf ...
  đã hoàn tác

Job 81082997c54f6122: hỏng, đã hoàn tác sạch
```

Sau đó (dòng 10957 tới 10973):

```
nginx active enabled
apache2 inactive disabled
ls: cannot access '/usr/sbin/policy-rc.d': No such file or directory
  => ĐẠT: nginx active+enabled, apache2 inactive+disabled, không policy-rc.d
  ...
  => ĐẠT: cây /etc/nginx giống hệt trước
  ...
  => ĐẠT: không file tpan nào sót trong /etc/apache2
```

### 2.8 Khởi động lại máy (phần E, hai lần)

Lần đầu khi máy đang chạy Apache (dòng 5213 tới 5342):

```bash
multipass restart tpan-m10
scripts/vm.sh exec bash -c 'uptime -s; for u in apache2 nginx; do echo "$u $(systemctl is-active $u) $(systemctl is-enabled $u 2>&1)"; done'
```

```
2026-11-20 00:58:39
apache2 active enabled
nginx inactive disabled
```

`ss -tlnp` chỉ có `apache2` ở cổng 80 và 443; mọi site và permalink 200; `tpan health` đạt. Lần thứ hai khi máy đang chạy nginx (dòng 11203 tới 11334) cho kết quả ngược lại, cũng đạt.

### 2.9 nginx của nginx.org, và máy cài LAMP trước (phần F, G)

Phần F: máy sạch, `stack install --web nginx --web-source nginx.org`, một site, sang Apache rồi về. Lúc về, Plan không cài gì (gói và kho nginx.org vẫn trên máy), chỉ ghi lại phần thích nghi của nginx.org (dòng 12768 tới 12800, cắt bớt):

```
- Cài gói nhưng chưa cho chạy: nginx ...
    $ dpkg-query --show '--showformat=${Package}\t${Status}\n' nginx
    | nginx	install ok installed
    $ systemctl is-enabled nginx
    | disabled
    $ systemctl is-active nginx
    | inactive
  đã đúng, bỏ qua
...
- Cho nginx của nginx.org đọc sites-enabled ...
    $ nginx -t
    ...
  xong
...
- Thêm user nginx vào nhóm www-data ...
    $ id -nG nginx
    | nginx www-data
  đã đúng, bỏ qua
```

Sau đó `stack status --json` ghi `"web_source": "nginx.org"` (lúc ở Apache là `"ubuntu"`), và (dòng 13678 tới 13693):

```
nginx	1.30.7-1~noble
nginx    nginx: worker process
nginx    nginx: worker process
nginx www-data
tpan.conf
  => ĐẠT: worker nginx chạy dưới user nginx
  => ĐẠT: id -nG nginx có www-data
  => ĐẠT: conf.d có tpan.conf, không default.conf
```

Phần G: máy sạch cài LAMP trước rồi chuyển sang nginx của Ubuntu (cài mới gói nginx dưới `policy-rc.d`). Bản xem trước: `15 bước sẽ chạy, 3 bước đã đúng sẽ bỏ qua, 1 bước có thể gián đoạn dịch vụ.` (dòng 14415). Job thành công, health đạt (dòng 14500 tới 15875).

### 2.10 Thời gian gián đoạn đo được

Mục 6.6 bắt buộc đo khoảng từ lúc web server cũ dừng tới lúc web server mới nhận request. Con số dưới đây là khối `ĐO GIÁN ĐOẠN (tới lúc này)` cuối cùng của M10.log (dòng 15888 tới 15914), gom mọi lượt đổi.

**Cách đo.** Trước mỗi lần đổi, kịch bản chạy một vòng lặp cho mỗi site, nền, trong máy ảo (lệnh viết lại từ dòng 1306 của log, chạy trên Mac):

```bash
scripts/vm.sh root systemd-run --unit=tpan-e2e-curl-B-a-http bash -c '
  f=$1; u=$2; shift 2; : >"$f"
  while :; do
    a=$EPOCHREALTIME
    o=$(curl -s -o /dev/null --connect-timeout 1 -m 3 -H "Connection: close" -w "%{http_code} %{time_total}" "$@" "$u" 2>/dev/null)
    r=$?
    echo "$a $r $o" >>"$f"
    sleep 0.02
  done' _ /root/tpan-e2e-curl-B-a-http.log http://a.example.com/ -k --resolve a.example.com:80:127.0.0.1
```

Mỗi dòng của file ghi: lúc bắt đầu request (giây, có phần lẻ), mã thoát của curl, mã HTTP, thời gian request. `Connection: close` để mỗi request mở một kết nối mới, nghỉ 20 ms giữa hai request. Request "đạt" là curl thoát 0 với mã mong đợi (301 cho http của site SSL, 200 cho phần còn lại). Mốc 0 là sự kiện `step.start` của bước `web.swap` trong bảng `job_events`; vòng lặp và tpan chạy trên cùng một máy nên cùng đồng hồ. Sau job, một đoạn Python đọc file và `tpan.db` rồi in các con số (mã ở dòng 3553 tới 3608 của log).

"Gián đoạn dài nhất" tính quanh mỗi chuỗi request không đạt liền nhau: từ lúc request đạt ngay trước chuỗi kết thúc, tới lúc request đạt ngay sau chuỗi kết thúc. Nó gồm cả một lần nghỉ 20 ms và thời gian của request đạt phía sau, nên là cận trên của khoảng site thật sự không trả lời.

**Con số.** Mã không đạt gặp được: `curl 7` (không kết nối được: không ai nghe cổng đó) và một lần `curl 52` (kết nối được nhưng không nhận được gì).

| Lượt | Chiều | Bước `web.swap` | Site | Request | Không đạt | Gián đoạn dài nhất |
|---|---|---|---|---|---|---|
| B | nginx → Apache | 96 ms | a-http | 1153 | 2 (curl 7) | 76 ms, từ +10 tới +85 ms |
| | | | a-https | 446 | 2 (curl 7) | 128 ms, từ +23 tới +151 ms |
| | | | b-http | 458 | 2 (curl 7) | 116 ms, từ +11 tới +127 ms |
| | | | c-http | 457 | 1 (curl 7) | 109 ms, từ +40 tới +149 ms |
| C | Apache → nginx | 100 ms | a-http | 857 | 3 (curl 7) | 113 ms, từ -3 tới +110 ms |
| | | | a-https | 304 | 1 (curl 7) | 130 ms, từ +49 tới +179 ms |
| | | | b-http | 315 | 2 (curl 7) | 165 ms, từ +10 tới +175 ms |
| | | | c-http | 314 | 2 (curl 7) | 165 ms, từ +20 tới +185 ms |
| | | | d-http | 311 | 1 (curl 7) | 136 ms, từ +41 tới +177 ms |
| F1 | nginx.org → Apache | 67 ms | a-http | 383 | 1 (curl 52) | 127 ms, từ -33 tới +94 ms |
| F2 | Apache → nginx.org | 89 ms | a-http | 175 | 1 (curl 7) | 76 ms, từ +43 tới +119 ms |
| G | Apache → nginx (Ubuntu) | 94 ms | a-http | 331 | 2 (curl 7) | 111 ms, từ +16 tới +126 ms |

Lượt D2 (đổi rồi hoàn tác vì `b` lỗi trên Apache): bước `web.swap` 98 ms; hoàn tác `web.swap` 148 ms, bắt đầu ở +5838 ms. Bốn site lành gián đoạn lần thứ hai lúc quay về nginx: a-http 185 ms (từ +5824 ms), a-https 227 ms (từ +5870 ms), c-http 218 ms, d-http 204 ms. Riêng `b-http`: 216 request không đạt (`curl 7` 7 lần, `HTTP 500` 209 lần), gián đoạn 6099 ms, từ +4 tới +6103 ms.

**Đọc con số.**

- Mỗi site mất 1 đến 3 request, khoảng không trả lời dài nhất 76 đến 165 ms. Với khách thật: một lần tải trang bị "không kết nối được", tải lại là được. Đây cũng là câu trong `docs/thay-doi/M10.md`: "Đổi web server làm mọi site ngừng trả lời khoảng 0,1 đến 0,2 giây."
- Bản thân bước `web.swap` mất 67 đến 100 ms. Đó là thời gian của `systemctl stop nginx` cộng `systemctl start apache2` (cộng hai lệnh `is-active`). Journal ở 2.3 cho thấy apache2 `Started` ở +84 ms. Gián đoạn đo được dài hơn bước một chút vì cách đo là cận trên (trên).
- Độ phân giải của phép đo bằng nhịp của vòng lặp. Log in "trung vị" khoảng giữa hai lần đạt: 27 đến 29 ms cho a-http (chỉ là một redirect 301, không chạy PHP), 54 đến 76 ms cho trang WordPress. Gián đoạn 128 ms ở a-https nghĩa là khoảng hai nhịp.
- Đừng nhầm với cột "khoảng dài nhất giữa hai lần nhận 200 cả lượt" trong log. Cột đó có thể nằm xa bước đổi: lượt C, a-https là 289 ms ở +5558 ms, lúc đó mọi request vẫn 200, chỉ là chậm (request chậm nhất 252 ms bắt đầu ở +5596 ms, trong lúc bước kiểm từng site và health đang gọi WordPress). Đó là site chậm, không phải site chết.
- `curl 7` là "Failed to connect": trong khoảng giữa `stop` và `start`, không tiến trình nào nghe cổng 80 và 443, nên nhân Linux từ chối kết nối ngay. Lần `curl 52` ở F1 bắt đầu ở -9 ms (dòng 12593: `a-http: không đạt đầu tiên bắt đầu -9 ms, cuối cùng kết thúc +40 ms`), tức là trước mốc 0; còn "-33" trong bảng là lúc request đạt ngay trước nó kết thúc. Nhiều khả năng request đó đã kết nối tới nginx rồi nginx dừng giữa chừng, đóng kết nối mà không trả lời. Đây là suy luận theo mã lỗi; log không có thêm bằng chứng.
- D2 cho thấy cái giá của hoàn tác: site lành gián đoạn thêm một lần, 185 đến 227 ms. Site hỏng trả 500 khoảng 6 giây, vì bước kiểm giữa chừng thử `midCheckTries = 5` lần, cách nhau `midCheckWait = 1` giây (`internal/core/php.go`), rồi mới hoàn tác.

Chưa đo: khách giữ kết nối keep-alive (trình duyệt thật giữ kết nối và gửi nhiều request trên đó; `systemctl stop` cắt các kết nối đó); khách gọi từ ngoài máy qua mạng thật; máy amd64; site có nhiều khách cùng lúc.

### 2.11 Chưa chạy

- API `POST /api/v1/stack/switch` và khối "Đổi web server" ở màn Stack chưa chạy trên máy ảo; e2e chỉ đi qua CLI. API có unit test (`internal/server/api_m10_test.go`), giao diện chỉ qua kiểm kiểu và build. Muốn xem: `make dev-api` và `make dev-web`. Máy demo có một site hỏng sẵn nên bấm đổi ngay sẽ bị từ chối `precondition_failed` (`docs/thay-doi/M10.md`).
- Đường "job xong mà không ghi được settings, chạy lại" (3.10) chỉ có unit test `TestSwitchRerunAfterSettingsFailure` trong `internal/core/stack_switch_test.go`; M10.log không có lần chạy nào như vậy. Unit test đã chạy trên Mac lúc viết file này (20/11/2026), PASS:

```bash
go test ./internal/core -run 'TestSwitchRerunAfterSettingsFailure|TestSwitchPlanContent' -v
```

```
=== RUN   TestSwitchPlanContent
--- PASS: TestSwitchPlanContent (0.02s)
=== RUN   TestSwitchRerunAfterSettingsFailure
--- PASS: TestSwitchRerunAfterSettingsFailure (0.03s)
PASS
ok  	github.com/doxuta/tpan/internal/core	0.666s
```

- Lỗi ở bước 5 hoặc 6 (sau khi mọi site đã đạt trên web server mới) rồi hoàn tác: chưa chạy, cả trên máy ảo lẫn trong unit test (3.11).
- Script của gói apache2 và `ssl-cert` còn bật `apache-htcacheclean.service` và `ssl-cert.service` khi khởi động (dòng `Created symlink` ở 1401 và 1440 của log), và chúng ở lại đó cả sau hoàn tác hay sau khi chuyển về nginx; `AptInstallNoStart` chỉ tắt `apache2`. Hai unit này không nghe cổng nào. Chưa kiểm trạng thái của chúng: `scripts/vm.sh exec systemctl is-enabled apache-htcacheclean ssl-cert`.
- Bản amd64 chỉ build, chưa chạy.
- Site có chứng chỉ Let's Encrypt thật (máy ảo dùng pebble).

## 3. Đi qua mã theo thứ tự chạy

Đường đi của lệnh `tpan stack switch --to apache`: `cmd/tpan/stack_switch.go` dựng `core.Switch` và gọi `Prepare`, `Prepare` (`internal/core/stack_switch.go`) đọc máy và dựng Plan, `runPlanned` (`cmd/tpan/planned.go`, có từ M12) in bản xem trước, hỏi lại, chạy engine, rồi hàm `after` ghi settings. Các Step mới nằm ở `internal/steps/switch_m10.go`.

### 3.1 `cmd/tpan/stack_switch.go`: `stackSwitchCmd`

```go
			w, err := core.ParseWeb(to)
			...
			if err := requireRoot(e, cmd); err != nil {
			...
			sw := &core.Switch{Sys: e.sys, Settings: core.EmptySettings{}, Sites: core.EmptySiteStore{}}
			...
			sw.Settings, sw.Sites = st, st
			if !f.dryRun {
				sw.Journal = st
			}
			p, err := sw.Prepare(ctx, w)
			...
			rep, err := runPlanned(ctx, e, f, sw.Journal, "stack.switch", string(w), p)
			...
			return healthAfterJob(ctx, e, st, f.asJSON, err)
```

File này mỏng: đọc cờ `--to`, đòi root, mở DB, giao việc cho core. `Journal` chỉ gắn khi chạy thật; xem trước thì không có gì được ghi. Dòng cuối chạy lại toàn bộ health sau job (như `tpan update` của M12): phép kiểm của web server cũ không còn chạy nên sự cố của chúng đóng, và site nào không đạt trên web server mới thì mở sự cố. Đó là câu "Kiểm lại toàn bộ health sau job" ở 2.3.

Có một nhánh riêng cho `--dry-run` khi chưa có DB (`core.EmptySettings{}`): không tạo DB chỉ để xem trước, core sẽ từ chối `stack_not_installed`.

### 3.2 `Prepare`: điều kiện đầu

```go
	old, err := (&Sites{Settings: s.Settings}).stack(ctx)
	...
	if old.Web == to {
		return nil, refuse("stack_same_web", ...)
	}
	...
	for _, st := range all {
		if st.Status == site.Active {
			active = append(active, st)
		}
		anySSL = anySSL || st.SSL
	}
	if err := s.sitesAnswer(ctx, active); err != nil {
		return nil, err
	}
```

Ba lần từ chối, đều trước khi dựng Plan nên không có job nào: stack chưa cài (`stack()` trả `stack_not_installed`), đã chạy đúng web server muốn đổi sang (`stack_same_web`, 2.6), và có site active đang không trả lời đúng (`sitesAnswer`, `precondition_failed`, 2.7 D0).

Để ý vòng lặp tách hai danh sách. `active` chỉ có site đang chạy: chỉ chúng được ghi vhost và được kiểm. `anySSL` xét mọi site, kể cả trong thùng rác: một site SSL trong thùng rác, khôi phục về sau trên web server mới, sẽ cần mod `ssl` và site mặc định 443.

`sitesAnswer` chạy `siteHTTP` (HTTPS khi site có SSL, HTTP khi không) cho mọi site active. Vì sao: nếu một site hỏng sẵn, bước kiểm sau khi đổi chắc chắn lỗi, cả job hoàn tác, và mọi site chịu hai lần gián đoạn vô ích. Nói trước rẻ hơn.

### 3.3 `Prepare`: nguồn gói của web server mới (`source`)

```go
func (s *Switch) source(ctx context.Context, to WebServer) (Source, error) {
	if to == Apache {
		return SourceUbuntu, nil
	}
	on, err := repoEnabled(s.Sys, RepoNginx)
	if err != nil || !on {
		return SourceUbuntu, err
	}
	...
	for _, src := range []Source{SourceNginxStable, SourceNginxMainline} {
		r, err := repoNginx(codename, src == SourceNginxMainline)
		...
		if ok, err := r.Check(ctx, s.Sys); ok { // trong mã: hai dòng, có xử lý err
			return src, nil
		}
```

Apache luôn lấy của Ubuntu. Với nginx thì đọc từ máy, không đọc từ settings. Lý do: đổi sang Apache ghi `stack.web_source = ubuntu` (nguồn nginx.org chỉ có nghĩa với nginx), nhưng kho nginx.org và gói nginx của nó vẫn nằm trên máy, ghim ưu tiên 900. Nếu tin settings, lúc về tpan sẽ nghĩ nginx là của Ubuntu, bỏ bước thích nghi `conf.d/tpan.conf`, và nginx của nginx.org sẽ không đọc `sites-enabled`. `r.Check` là chính `Check` của bước `AptRepo` (M12): kho có trên máy đúng như tpan ghi thì đạt. Bằng chứng ở 2.9: về nginx thì `web_source` lại là `nginx.org`.

### 3.4 Bước 1: `AptInstallNoStart` (`internal/steps/switch_m10.go`)

Trong `Prepare`:

```go
	disable := []string{unit}
	running, err := (&steps.Service{Unit: unit}).Check(ctx, s.Sys)
	...
	if running {
		disable = nil
	}
	ss := []engine.Step{
		&steps.AptUpdate{Packages: []string{unit}},
		&steps.AptInstallNoStart{Packages: []string{unit}, Disable: disable},
	}
```

`unit` là `webUnit(to)`: `apache2` hoặc `nginx`, vừa là tên gói vừa là tên dịch vụ. Nhánh `running` dành cho một ca hiếm: lần trước job đã đổi xong mà không ghi được settings (3.10), nên web server mới đang chạy và đang phục vụ mọi site. Lúc đó `disable --now` là tự tắt web server đang sống, nên bỏ `Disable`.

Trong `AptInstallNoStart`, `Apply` gọi `install` rồi mới tắt unit:

```go
func (s *AptInstallNoStart) install(ctx context.Context, sys system.System) (err error) {
	prev, err := capture(sys, policyRcPath)
	...
	defer func() {
		if rerr := prev.restore(sys); rerr != nil {
			err = errors.Join(err, fmt.Errorf("trả %s về như trước: %w", policyRcPath, rerr))
		}
	}()
	if err := sys.WriteFileAtomic(policyRcPath, policyRcData, 0o755, system.Root); err != nil {
		return fmt.Errorf("ghi %s để chặn dịch vụ tự khởi động: %w", policyRcPath, err)
	}
	if res, err := sys.Run(ctx, s.cmd()); err != nil {
		return withStdout(fmt.Errorf("cài gói %s: %w", strings.Join(s.Packages, " "), err), res)
	}
	return nil
```

Bốn chỗ đáng nói:

- `policyRcData` là `#!/bin/sh`, một dòng đánh dấu của tpan, `exit 101`. Mã 101 nghĩa là "không được khởi động" (mục 4).
- `capture` nhớ file cũ nếu có (của người khác) để `restore` trả y nguyên; nếu file sót là của tpan (có dòng đánh dấu, do lần trước chết giữa `apt-get`) thì coi như không có, để lần này gỡ nó.
- `restore` nằm trong `defer`, đặt trước khi ghi. Nó chạy ngay khi `install` trả về, kể cả khi `apt-get` lỗi hay job bị huỷ. Để sót file này là mọi lần `apt-get` về sau trên máy (kể cả `unattended-upgrades`) không khởi động lại được dịch vụ nào. `err` là giá trị trả về có tên, nên `defer` gộp được lỗi của nó vào (xem `00-go-can-ban.md` mục 6).
- Tách `install` thành hàm riêng là để `defer` chạy trước bước tắt unit, không phải ở cuối `Apply`.

Rồi `Apply` tắt unit, kể cả khi cài lỗi:

```go
	err := s.install(ctx, sys)
	on, qerr := s.unitsOn(ctx, sys)
	...
	if len(on) == 0 {
		return err
	}
	if res, derr := sys.Run(ctx, s.disableCmd(on)); derr != nil {
		return errors.Join(err, withStdout(fmt.Errorf("tắt %s sau khi cài: %w", strings.Join(on, " "), derr), res))
	}
	return err
```

`disableCmd` là `systemctl disable --now <unit>`. Vì sao phải có bước này dù đã có `policy-rc.d`: `policy-rc.d` chỉ chặn được việc khởi động; script của gói vẫn `enable` dịch vụ (dòng `Created symlink` ở 2.3). Điều này đã kiểm từ M1 (mục 16 của KE-HOACH.md: "`policy-rc.d` trả 101 chặn được việc khởi động nhưng **không** chặn `enable`"). Để nguyên thì nếu máy khởi động lại giữa job (hoặc job hoàn tác rồi máy khởi động lại vài tuần sau), cả hai web server cùng lên và giành cổng 80. Tắt cả khi cài lỗi vì script của gói có thể đã `enable` rồi mới lỗi ở bước sau. `unitsOn` chỉ trả unit đang bật hoặc đang chạy, để cài lỗi vì gói không có thì không thêm một lỗi "Unit file does not exist" làm rối.

`Check` của bước này đạt khi: không có `policy-rc.d` sót của tpan, gói đã cài, và unit không `enabled` cũng không `active`. Ở 2.9 (về nginx.org) bước này "đã đúng, bỏ qua" vì gói vẫn đó và nginx đang tắt.

`Undo` không làm gì. Lý do ở 5.3.

### 3.5 Bước 2: cấu hình của web server mới, không start, không reload (`newConfig`)

```go
func (s *Switch) newConfig(cfg StackConfig, active []site.Site, anySSL bool) ([]engine.Step, error) {
	ss, err := webSteps(cfg, false)
	... // anySSL: thêm steps.DefaultSSL(string(cfg.Web))
	for _, st := range active {
		v, err := siteVhost(st, cfg)
		... // Apache: thêm htaccessIfMissing(s.Sys, l, l.Public) nếu khác nil
	lr, err := logrotateStep(cfg.Web)
	...
	for _, st := range ss {
		if m, ok := st.(*steps.ApacheModules); ok {
			m.NoRestart = true
		}
	}
	return steps.WithoutReload(ss), nil
```

Điều xuyên suốt của bước này: web server mới đang tắt, web server cũ đang giữ cổng. Mọi lệnh `reload`, `restart`, `start` web server mới lúc này đều sai: `reload` một dịch vụ đang tắt là lỗi; `restart` hay `start` thì nó sẽ cố nghe cổng 80 đang bị web server cũ giữ (mục 4).

- `webSteps(cfg, false)` (`internal/core/stack.go`) là phần web server của `stack install`, tách thành hàm dùng chung ở M10 để hai nơi không lệch nhau. Tham số `running` là `false`: với nginx.org thì không thêm bước bật dịch vụ và không khởi động lại nginx sau khi thêm user `nginx` vào nhóm `www-data` (nhóm có hiệu lực khi nginx khởi động ở bước đổi); với Apache thì `ApacheModules` có `NoRestart: !running`.
- `steps.DefaultSSL` là site mặc định 443 của M5, khi có site SSL ở bất kỳ trạng thái nào.
- `siteVhost(st, cfg)` (`internal/core/fix.go`) dựng vhost của từng site theo template của web server mới: bản 443 khi site có SSL, socket theo bản PHP của site (`sitePHP`). Cùng một hàm với `site add` và sửa tự động của health, nên vhost sau khi đổi giống hệt vhost của một site tạo mới trên Apache.
- `htaccessIfMissing` chỉ khi đổi sang Apache (3.12).
- `logrotateStep`: cách xoay log và báo web server mở lại file log khác nhau giữa nginx và Apache.
- Vòng lặp cuối đặt `NoRestart` cho mọi `ApacheModules`, kể cả cái nằm trong `DefaultSSL` (bật mod `ssl`), vì M5 viết bước đó cho máy đang chạy Apache nên có restart.
- `steps.WithoutReload` (`internal/steps/batch.go`) xoá trường `Reload` của mọi `File`, `Symlink`, `Absent`. Trường `Test` (`nginx -t`, `apachectl configtest`) giữ nguyên: kiểm cú pháp chạy được khi dịch vụ tắt (2.4, và mục 16 đã kiểm).

`ApacheModules.NoRestart` (`internal/steps/apache.go`) là trường duy nhất M10 thêm vào một Step cũ:

```go
	// configtest lỗi thì Apache vẫn đang chạy bằng config cũ trong bộ nhớ: chỉ
	// cần trả module về, không restart.
	if res, err := sys.Run(ctx, apacheConfigtest); err != nil {
		return fail(withStdout(fmt.Errorf("kiểm cấu hình Apache sau khi đổi module: %w", err), res), false)
	}
	if !s.NoRestart {
		if res, err := sys.Run(ctx, apacheRestart); err != nil {
			return fail(withStdout(fmt.Errorf("khởi động lại apache2 sau khi đổi module: %w", err), res), true)
		}
	}
```

`revert` (dùng khi lỗi và khi `Undo`) cũng dừng trước lệnh restart khi `NoRestart`. `Info` bỏ lệnh restart khỏi danh sách lệnh và bỏ cờ `Danger` (không restart thì không cắt kết nối nào): vì thế bản xem trước ở 2.2 nói "1 bước có thể gián đoạn dịch vụ", chỉ `web.swap`.

### 3.6 Bước 3: `WebSwap`

```go
	fromActive, err := unitActive(ctx, sys, s.From)
	...
	if res, err := sys.Run(ctx, systemctl("stop", s.From)); err != nil {
		err = withStdout(fmt.Errorf("dừng %s: %w", s.From, err), res)
		if fromActive {
			return errors.Join(err, s.start(ctx, sys, s.From))
		}
		return err
	}
	if res, err := sys.Run(ctx, systemctl("start", s.To)); err != nil {
		err = withStdout(fmt.Errorf("khởi động %s: %w", s.To, err), res)
		return errors.Join(err, s.start(ctx, sys, s.From))
	}
	s.applied = true
	return nil
```

Hai lệnh, đúng thứ tự: dừng cũ rồi mới chạy mới, vì cổng 80 chỉ một tiến trình giữ được (mục 4). Khoảng giữa hai lệnh là gián đoạn đo ở 2.10.

Nếu `start` web server mới lỗi, chính bước này bật lại web server cũ ngay. Không chờ engine hoàn tác được, vì engine không gọi `Undo` của bước có `Apply` lỗi (luật chung của gói `steps`, ghi ở đầu `common.go`). `Undo` của bước (chạy khi một bước sau lỗi, như D2) làm ngược lại: `systemctl stop <mới>`, `systemctl start <cũ>`.

`Check` đạt khi cũ đã dừng VÀ mới đang chạy. Lần trước hỏng giữa chừng (cũ đã dừng, mới chưa chạy) thì `Check` sai và `Apply` chạy lại; đã đổi xong thì không dừng rồi chạy lại vô ích.

Bước này không `enable` hay `disable` gì. Việc đó để sau bước kiểm từng site, để lúc còn có thể hoàn tác thì trạng thái "bật khi khởi động" vẫn là của web server cũ.

### 3.7 Bước 4: kiểm từng site (`midCheck`)

```go
	ss = append(ss, &steps.WebSwap{From: from, To: unit})
	for _, st := range active {
		dom := st.Domain.String()
		ss = append(ss, &midCheck{
			id:    "stack.switch.check:" + dom,
			check: siteHTTP(st),
			need:  2,
			...
```

`midCheck` (`internal/core/php.go`, có từ M12) là một phép kiểm đóng vai một bước của Plan: thử tối đa `midCheckTries = 5` lần, cách nhau `midCheckWait = 1` giây, cần `need: 2` lần đạt liên tiếp. Không đạt thì bước lỗi và engine hoàn tác mọi bước trước theo thứ tự ngược. Hai lần đạt vì lần gọi đầu ngay sau `start` chưa chắc đã là cấu hình cuối cùng. Ở 2.3 mỗi bước kiểm mất khoảng 1,5 giây (`17:57:04.563` tới `17:57:06.134`): hai lần gọi cách nhau một giây.

### 3.8 Bước 5: bật cái mới, tắt bật-khi-khởi-động của cái cũ

```go
	ss = append(ss, &steps.Service{Unit: unit}, &steps.ServiceDisable{Unit: from})
```

`steps.Service` (M1) chạy `systemctl enable --now apache2`: lúc này apache2 đã chạy nên chỉ còn phần `enable`. `Undo` của nó chỉ đảo phần đã đổi: trước bước, apache2 đang chạy nhưng chưa bật khi khởi động, nên `Undo` chỉ `disable`, không dừng.

`ServiceDisable` (mới):

```go
func (s *ServiceDisable) Apply(ctx context.Context, sys system.System) error {
	...
	enabled, err := unitEnabled(ctx, sys, s.Unit)
	...
	if res, err := sys.Run(ctx, systemctl("disable", s.Unit)); err != nil {
	...
	s.wasEnabled, s.applied = enabled, true
	return nil
}

func (s *ServiceDisable) Undo(ctx context.Context, sys system.System) error {
	... // !s.wasEnabled thì không làm gì; còn lại chạy systemctl("enable", s.Unit)
```

`disable` không có `--now`: nginx đã dừng ở bước 3, và nếu vì lý do gì nó còn chạy thì không phải việc của bước này. `unitEnabled` hỏi `systemctl is-enabled`: thoát 0 là bật, mọi mã khác 0 (1: disabled hoặc masked, 4: không có unit) là "không". `Undo` chỉ bật lại nếu trước đó nó đang bật.

### 3.9 Bước 6: gỡ file của web server cũ (`oldWebFiles`)

```go
	for _, st := range append(append(vh, ss...), ssl...) {
		switch x := st.(type) {
		case *steps.Symlink:
			links = append(links, x.Link)
		case *steps.File:
			paths = append(paths, x.Path)
		}
	}
	return append(links, paths...), nil
```

`oldWebFiles(old, all)` không chép đường dẫn nào. Nó gọi lại đúng các hàm đã ghi file cho web server cũ: `siteVhost` cho mọi site trong bảng `sites` (mọi trạng thái, kể cả thùng rác), `webSteps(old, false)` (site mặc định 80, và `conf.d/tpan.conf` với nginx.org), `steps.DefaultSSL` (site mặc định 443). Rồi nhặt đường dẫn của mọi `Symlink` và `File` trong các bước đó. Link trước, file sau. Mỗi đường dẫn thành một `steps.Absent` không `Test`, không `Reload` (web server cũ đã dừng).

Vì sao gỡ: phép kiểm lệch của M4 so sha256 của mọi dòng trong `managed_files` với file trên đĩa. Để lại vhost nginx thì health vẫn theo dõi file của một web server không chạy. Tệ hơn ở lần chuyển ngược: bước ghi file sẽ thấy file "đã đúng", bị bỏ qua, nên engine không ghi lại dòng của nó. `Absent` khai với engine là nó xoá file, nên engine bỏ dòng đó khỏi `managed_files`. `Absent` giữ nội dung cũ, nên `Undo` trả file về được. Bằng chứng: 2.4 (`dòng còn trỏ vào /etc/nginx: 0`).

Gói cũ không gỡ, chỉ dừng và tắt: gỡ gói có thể kéo gói khác theo, và giữ gói thì lần chuyển ngược nhanh (2.9: không cài gì).

### 3.10 Kiểm chứng và `after`

```go
	verify := []checks.Check{checks.Service(unit, checks.StageWeb), config, checks.HTTPDefault(string(to))}
	for _, st := range active {
		verify = append(verify, checks.VhostEnabled(string(to), st.Layout()), siteHTTP(st))
	}
```

Kiểm chứng chỉ gồm phần web: dịch vụ, cú pháp, site mặc định, vhost bật, HTTP từng site. Không chạy cả bộ phép kiểm của site (checksum WordPress, DB...): một site hỏng ở chỗ không liên quan web server không được kéo hoàn tác một lần đổi đúng. Health đầy đủ chạy ngay sau job (3.1).

`after` chạy khi job thành công:

```go
	after := func(ctx context.Context, rep engine.Report) error {
		for _, kv := range [][2]string{{SettingWeb, string(to)}, {SettingWebSource, string(src)}} {
			if err := s.Settings.SetSetting(ctx, kv[0], kv[1]); err != nil {
				return fmt.Errorf("máy đã chạy %s nhưng chưa ghi được setting %s (%w); chạy lại `sudo tpan stack switch --to %s`: ...", unit, kv[0], err, to)
			}
		}
		for _, f := range newFiles {
			sum := sha256.Sum256(f.Content)
			if err := s.Journal.RecordFile(ctx, f.Path, hex.EncodeToString(sum[:]), rep.Job.ID); err != nil {
			...
		for _, p := range gone {
			if err := s.Journal.ForgetFile(ctx, p); err != nil {
```

Ba việc:

1. Ghi `stack.web` và `stack.web_source`. Ghi không được thì câu lỗi nói rõ máy đang chạy gì và lệnh chạy lại. Chạy lại thì mọi bước đã đúng được bỏ qua (nhờ nhánh `running` ở 3.4 và `Check` của từng bước), chỉ còn ghi settings. Vì thế `Prepare` không được từ chối khi máy đã chạy web server mới mà settings còn ghi cũ: nó chỉ so `old.Web == to` theo settings.
2. Ghi sha256 của mọi `File` của web server mới (`newFiles := files(conf)` ở bước 2) vào `managed_files`, kể cả file đã đúng sẵn nên bước bị bỏ qua. Engine chỉ ghi dòng cho bước đã `Apply`; thiếu dòng này thì một file đúng sẵn sẽ không bao giờ được kiểm lệch. Đây là ý "phép kiểm lệch nhận các file mới làm chuẩn".
3. Bỏ dòng của mọi file cũ (`gone`), kể cả khi bước gỡ bị bỏ qua vì file vốn không còn (engine không bỏ dòng của bước bị bỏ qua).

`after` là một closure: hàm viết ngay trong `Prepare`, dùng được `to`, `src`, `newFiles`, `gone` của `Prepare` (xem `00-go-can-ban.md` mục 9).

### 3.11 Hoàn tác chạy ngược thế nào

Engine (M1) chạy các bước theo thứ tự; một bước lỗi thì gọi `Undo` của mọi bước đã chạy trước nó, ngược thứ tự. Bước "đã đúng, bỏ qua" không có gì để hoàn tác. Với Plan này:

| Lỗi ở | Hoàn tác gồm | Kết quả |
|---|---|---|
| Bước 1 hoặc 2 (D1) | trả các file đã ghi của web server mới về như trước, trả module Apache (không restart) | web server cũ chưa từng dừng; gói mới còn, tắt, không bật khi khởi động |
| Bước 4, kiểm một site (D2) | `midCheck` không làm gì; `WebSwap.Undo`: dừng mới, chạy cũ; rồi trả file của bước 2 | web server cũ chạy lại với đúng file cũ; site lành gián đoạn thêm 185 đến 227 ms |
| Bước 5 hoặc 6 | `Absent.Undo` trả file cũ; `ServiceDisable.Undo` bật lại cũ khi khởi động; `Service.Undo` tắt bật-khi-khởi-động của mới; `WebSwap.Undo`; trả file bước 2 | như trên; chưa chạy, cả trên máy ảo lẫn trong unit test (`TestSwitchRollback` chỉ làm hỏng bước kiểm HTTP); đây là suy luận theo `Undo` của từng bước |

Thứ tự ngược là quan trọng: file của web server cũ được trả về trước khi `WebSwap.Undo` khởi động nó, nên nó khởi động với đúng file cũ. `AptInstallNoStart.Undo` không làm gì, nên sau mọi lần hoàn tác web server mới vẫn ở trạng thái tắt và không bật khi khởi động (2.7 D2: `apache2 inactive disabled`).

### 3.12 `htaccessIfMissing`, và khôi phục site trên Apache (`internal/core/trash.go`)

```go
func htaccessIfMissing(sys system.System, l site.Layout, public string) (engine.Step, error) {
	_, err := sys.Stat(public + "/.htaccess")
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return steps.SiteHtaccess(l)
	case err != nil:
		return nil, fmt.Errorf("kiểm .htaccess của %s: %w", l.Domain, err)
	}
	return nil, nil
}
```

Site dựng trên nginx không có `.htaccess` (nginx không đọc file này). Trên Apache, thiếu nó thì trang chủ vẫn 200 nhưng mọi permalink `/%postname%/` là 404. Có rồi thì để nguyên: WordPress tự ghi lại đoạn của nó, ghi đè là mất luật mà plugin đã thêm. `steps.SiteHtaccess` là bước `File` với nội dung `templates.WPHtaccess()`, quyền 0640, chủ là user của site, nhóm `www-data`.

Tham số `public` là chỗ thư mục public đang nằm lúc dựng Plan, còn bước luôn ghi vào `l.Public`. Hai chỗ gọi:

- `newConfig` (3.5): site active, `public` là `l.Public`.
- `PrepareRestore` trong `trash.go`, khi stack đang là Apache:

```go
	if cfg.Web == Apache {
		ht, err := htaccessIfMissing(s.Sys, l, site.TrashDir(l)+"/public")
		...
		if ht != nil {
			ss = append(ss[:1], append([]engine.Step{ht}, ss[1:]...)...)
		}
	}
```

Lúc dựng Plan khôi phục, thư mục của site còn nằm trong thùng rác, nên phải nhìn ở `site.TrashDir(l)+"/public"`. Bước được chèn vào vị trí thứ hai, ngay sau bước chuyển thư mục về (2.5). Lý do phải có đoạn này: `stack switch` chỉ ghi `.htaccess` cho site active, nên site vào thùng rác lúc còn nginx sẽ về trên Apache mà không có nó (mục 5.5).

## 4. Kiến thức nền

### `policy-rc.d` và `invoke-rc.d`

Gói Debian và Ubuntu có các script chạy khi cài (`postinst`). Script đó không gọi thẳng `systemctl start` mà qua `invoke-rc.d` hoặc `deb-systemd-invoke`. Hai công cụ này hỏi `/usr/sbin/policy-rc.d` trước: file đó thoát 101 nghĩa là "không được chạy hành động này". Không có file thì mọi hành động được phép. Mô tả nằm ở `/usr/share/doc/init-system-helpers/README.policy-rc.d` (ghi trong chú thích của `policyRcPath`). Trong mã: `policyRcPath`, `policyRcData`, `AptInstallNoStart.install`. Tự xem: dòng `policy-rc.d returned 101, not running 'start apache2.service'` ở 2.3, và `scripts/vm.sh exec ls -la /usr/sbin/policy-rc.d` (sau job thì không còn).

### `systemctl enable` khác `systemctl start`

`start` chạy dịch vụ ngay bây giờ. `enable` không chạy gì, chỉ tạo một symlink trong `/etc/systemd/system/multi-user.target.wants/` để systemd chạy dịch vụ ở lần khởi động máy sau. `enable --now` làm cả hai; `disable --now` gỡ link và dừng. `is-active` trả lời câu "đang chạy không", `is-enabled` trả lời câu "khởi động máy có chạy không". `policy-rc.d` chỉ chặn được `start`, nên sau khi cài, apache2 là `inactive` mà `enabled`. Trong mã: `AptInstallNoStart.disableCmd`, `ServiceDisable`, `unitEnabled`. Tự xem: `scripts/vm.sh exec bash -c 'systemctl is-active apache2 nginx; systemctl is-enabled apache2 nginx; ls /etc/systemd/system/multi-user.target.wants/ | grep -E "nginx|apache2"'`.

### Cổng 80 chỉ một tiến trình giữ

Một chương trình muốn nhận kết nối ở cổng 80 phải `bind` vào cổng đó. Một cổng TCP trên một địa chỉ chỉ một socket nghe giữ được (nginx hay Apache có nhiều tiến trình con, nhưng chúng dùng chung socket của tiến trình cha). Chương trình thứ hai `bind` sẽ lỗi (thường là `Address already in use`) và dịch vụ không lên; M10.log không có lần nào thử điều này, vì tpan không bao giờ để hai web server cùng chạy. Khi không ai giữ cổng, nhân Linux trả lời ngay "connection refused": đó là `curl 7` ở 2.10. Vì thế phải dừng cũ rồi mới chạy mới, và khoảng giữa là gián đoạn không tránh được khi chỉ có một máy. Trong mã: `WebSwap.Apply`. Tự xem: `scripts/vm.sh root ss -tlnp` (2.4: cột cuối cho biết tiến trình nào giữ `*:80`).

### PHP-FPM tách khỏi web server

PHP không chạy trong nginx hay Apache mà trong PHP-FPM, mỗi site một pool với socket riêng ở `/run/php/`. Web server chỉ nhận request rồi chuyển file `.php` sang socket đó: nginx bằng `fastcgi_pass unix:...sock;`, Apache bằng `SetHandler "proxy:unix:...sock|fcgi://localhost"` (cần mod `proxy_fcgi`, nằm trong danh sách `apacheModules`). Vì thế đổi web server chỉ là viết vhost khác trỏ vào cùng socket; pool, DB, mã nguồn không đổi. Đó là câu đầu của mục 6.6. Trong mã: `siteVhost` dựng vhost với socket từ `sitePHP`; `templates/files/site-apache.conf.tmpl`. Tự xem: `scripts/vm.sh exec grep -n sock /etc/apache2/sites-available/tpan-c.example.com.conf` (2.4: socket `-8.4.sock`), rồi `scripts/vm.sh exec ls -la /run/php` để thấy socket đó vẫn là của pool cũ.

### `.htaccess` và `AllowOverride`

`.htaccess` là file cấu hình Apache đặt ngay trong thư mục web. Apache đọc nó ở mỗi request, nếu thư mục cho phép bằng `AllowOverride`. Vhost Apache của tpan đặt `AllowOverride All` cho thư mục public của site (WordPress ghi luật permalink vào đó), và `AllowOverride None` cho `wp-content/uploads` (một `.htaccess` tải lên đó không mở lại được việc chạy PHP). nginx không có khái niệm này: luật permalink nằm thẳng trong vhost (`try_files $uri $uri/ /index.php?$args;`). Nên site dựng trên nginx không có `.htaccess`, và một dòng sai trong `.htaccess` chỉ làm hỏng site trên Apache (đó là cách D2 làm một site "chỉ hỏng trên Apache"). Health không kiểm lệch `.htaccess` (`internal/health/collect.go` bỏ qua tên file này) vì WordPress tự ghi lại nó; dòng của nó vẫn có trong `managed_files`. Trong mã: `htaccessIfMissing`, `steps.SiteHtaccess`, `internal/templates/files/site-apache.conf.tmpl`. Tự xem: `scripts/vm.sh exec grep -n AllowOverride /etc/apache2/sites-available/tpan-b.example.com.conf` (chưa chạy trong M10.log; bản mẫu ở `internal/templates/testdata/site-apache.golden` dòng 21, 32, 40).

### Go: type switch và ép kiểu interface

Plan là một danh sách `[]engine.Step`, kiểu interface. Muốn biết một phần tử cụ thể là kiểu gì thì dùng type switch, như trong `oldWebFiles`:

```go
		switch x := st.(type) {
		case *steps.Symlink:
			links = append(links, x.Link)
		case *steps.File:
			paths = append(paths, x.Path)
		}
```

Trong nhánh `case *steps.Symlink`, `x` có kiểu `*steps.Symlink` nên đọc được `x.Link`. Bước khác (`Dir`, `ApacheModules`...) không khớp nhánh nào thì bỏ qua. Dạng một nhánh là `m, ok := st.(*steps.ApacheModules)` trong `newConfig`: `ok` là `true` khi `st` đúng kiểu đó. Nhờ vậy core sửa được trường của bước (`m.NoRestart = true`) mà không cần interface có phương thức "đừng restart". Tự xem (trên Mac, không cần máy ảo): `go test ./internal/core -run TestSwitchPlanContent -v` (test này duyệt Plan bằng type switch để kiểm từng `ApacheModules` có `NoRestart` và không bước ghi file nào còn `Reload`; đã chạy, PASS, output ở 2.11).

## 5. Cách khác đã cân nhắc, và chỗ đã làm sai

### 5.1 Lọc bước từ `installPlan` thay vì danh sách cho phép

Plan cài stack (`installPlan`) đã có mọi bước web server. Cách dễ nhất là lấy Plan đó rồi lọc bỏ các bước khởi động. Không chọn, theo chú thích đầu `internal/core/stack_switch.go`: `installPlan` có `Service` (`enable --now`), `ApacheModules` có restart, `groupRestart` của nginx.org khởi động lại nginx. Lọc theo kiểu là danh sách cấm: mốc sau thêm một bước có restart vào `installPlan` thì bộ lọc không biết, và Plan đổi web server âm thầm khởi động web server mới lúc web server cũ còn giữ cổng. Cách chọn là tách phần web thành `webSteps(cfg, running)` dùng chung, với `running=false` không bao giờ thêm bước khởi động, và `newConfig` dựng tường minh từng phần. Test `TestSwitchNoStartBeforeSwap` canh luật này: trước `web.swap`, không bước nào được khai hay chạy một lệnh khớp `systemctl (start|restart|reload|enable)( --now)? (nginx|apache2)` (`webUnitRE` trong `internal/core/stack_switch_test.go`).

### 5.2 Để lại file của web server cũ

Để lại thì chuyển ngược nhanh hơn một chút và Plan ngắn hơn (bản xem trước ở 2.2 có 12 bước gỡ). Không chọn, lý do ở 3.9 và `docs/thay-doi/M10.md`: health sẽ kiểm lệch file của một web server không chạy, và lần chuyển ngược thì bước ghi file thấy "đã đúng", không được ghi nhận lại vào `managed_files`. Gói cũ thì giữ (chỉ dừng và tắt), vì gỡ gói có thể kéo gói khác và không cần cho việc gì.

### 5.3 `Undo` của bước cài gói bật lại web server mới

Một `Undo` "đối xứng" sẽ trả web server mới về trạng thái script của gói muốn: `enabled`. Không chọn, theo chú thích hợp đồng ở `internal/steps/m10_types.go` và câu `NoUndo` của `AptInstallNoStart`: bật lại nó là đem lại đúng chuyện hai web server tranh cổng 80 ở lần khởi động máy sau. Gói cũng không gỡ khi hoàn tác (như `AptInstall` của M1). Sau hoàn tác, web server mới nằm trên máy, tắt, không bật khi khởi động: vô hại, và lần đổi sau nhanh hơn.

### 5.4 Kiểm cả bộ phép kiểm của site trong kiểm chứng

Các Plan khác của tpan kiểm chứng bằng cả bộ `siteChecks`. Không chọn ở đây (3.10): một site hỏng ở chỗ không liên quan web server (DB, mã nguồn) sẽ làm một lần đổi đúng bị hoàn tác, mọi site gián đoạn thêm lần nữa. Cùng lập luận với `update.apply` của M12. Phần còn lại do health đầy đủ sau job lo, và điều kiện đầu (`sitesAnswer`) đã chặn site hỏng sẵn ở chặng HTTP.

### 5.5 Chỗ đã làm sai: khôi phục site trên Apache thiếu `.htaccess`

Bản đầu của Plan chỉ ghi `.htaccess` cho site active. Lần chạy e2e trước M10.log hiện tại (ghi trong `docs/thay-doi/M10.md`, "thấy ở e2e, phần B"): `d.example.com` vào thùng rác lúc còn nginx, khôi phục sau khi đã sang Apache thì trang chủ 200 mà permalink 404. Lỗi nằm ở Plan (thiếu một bước), nên unit test bắt được, chỉ là chưa ai viết test cho ca "site vào thùng rác trên nginx, khôi phục trên Apache"; e2e có ca đó nên lộ ra. Sửa ở commit `6888d06`: tách `htaccessIfMissing` dùng chung, `PrepareRestore` gọi nó khi stack là Apache (3.12); `git show --stat 6888d06` cho thấy `trash_test.go` thêm 34 dòng test cho đúng ca này. M10.log là lần chạy sau khi sửa: 2.5 có bước `Ghi /var/www/d.example.com/public/.htaccess` và `d-permalink: 200`.

Còn một ca chưa phủ: đổi sang Apache, rồi về nginx, rồi lại sang Apache; site active lúc đó đã có `.htaccess` từ lần đầu nên được để nguyên. Đúng ý muốn.

### 5.6 Hai bước cùng ID `apache.modules:apache2`

Không phải cách khác mà là chỗ còn sót, thấy khi đọc M10.log để viết file này: bước module cơ bản (`webSteps`) và bước bật `ssl` (`steps.DefaultSSL`) cùng ID, nên output `--verbose` in sai tên bước đầu (2.3) và bảng `job_events` không phân biệt được hai bước. Máy vẫn đúng: engine chạy và hoàn tác theo vị trí của bước, không theo ID, nên cả hai bước chạy và kiểm đúng. Chỉ bảng tên trong `cmd/tpan/print.go` (và `plan.go`) bị ghi đè, và người đọc log sẽ bối rối. Chưa sửa ở M10.

Một chỗ nhỏ cùng loại: `managed_files` giữ dòng của `.htaccess` sau khi chuyển về nginx (phần C, dòng 8651 tới 8655 của log: `số dòng: 20`, có đủ bốn `.htaccess`), vì `oldWebFiles` chỉ gỡ file của web server. Không hại gì: file vẫn đúng như tpan ghi, và health bỏ qua `.htaccess` khi kiểm lệch (mục 4).

## 6. Tự kiểm

1. Vì sao không cài apache2 bằng `apt-get install` thường rồi `systemctl stop apache2` ngay sau đó?
2. `policy-rc.d` đã chặn apache2 tự chạy. Vậy bước `systemctl disable --now apache2` để làm gì? Bỏ nó đi thì hỏng lúc nào?
3. Ở bước 2, xoá dòng `return steps.WithoutReload(ss), nil` thay bằng `return ss, nil` thì chuyện gì xảy ra khi chạy?
4. Vì sao `source` đọc kho nginx.org từ máy mà không đọc `stack.web_source` trong settings?
5. Sau khi đổi sang Apache, vì sao tpan gỡ hết vhost nginx của mình mà không giữ gói nginx lại? Nếu không gỡ file thì lần chuyển ngược hỏng ở đâu?
6. Site `b` có `.htaccess` hỏng (D2). Kể theo thứ tự những gì máy làm từ lúc `systemctl start apache2` tới lúc job báo "hỏng, đã hoàn tác sạch". Vì sao `b` trả 500 khoảng 6 giây?
7. Người review hỏi "đổi web server thì site chết bao lâu?". Bạn trả lời bằng con số nào, đo thế nào, và con số đó chưa nói được điều gì?
8. Site trong thùng rác được đối xử khác site active ở những chỗ nào trong `Prepare`, và vì sao?
9. (Cần máy ảo) Sau khi đổi sang Apache, chứng minh rằng khởi động lại máy sẽ không có nginx tranh cổng 80, mà không cần khởi động lại.
10. (Cần máy ảo) Chứng minh rằng phép kiểm lệch của M4 đang theo dõi vhost Apache mới và không còn theo dõi vhost nginx cũ.

## Đáp án

1. Vì script của gói đã cố khởi động apache2 ngay trong `apt-get`, lúc nginx đang giữ cổng 80: apache2 không nghe được cổng đó và dịch vụ lỗi (suy luận, chưa chạy thử trên máy ảo; M1 chỉ thấy apache2 tự chạy và nghe cổng 80 trên máy chưa có nginx, mục 16). Thêm nữa, gói đã `enable` apache2, nên `stop` thôi chưa đủ. `AptInstallNoStart` không bao giờ cho nó khởi động: journal ở 2.3 ghi đúng một lần `Started apache2`, ở +84 ms sau khi bước `web.swap` bắt đầu.
2. `policy-rc.d` chặn `start` nhưng không chặn `enable`: output ở 2.3 có `policy-rc.d denied execution of start` rồi ngay sau là `Created symlink .../multi-user.target.wants/apache2.service`. Bỏ `disable --now` thì apache2 `inactive` mà `enabled`. Hỏng khi máy khởi động lại trong lúc job chạy, hoặc sau một lần hoàn tác (máy đang chạy nginx, apache2 nằm đó với link bật): lần khởi động sau cả hai cùng lên, một trong hai không nghe được cổng 80, và site có thể chết. Có thể hỏng cả ở lần vá gói về sau: 2.6 cho thấy unit `disabled` thì script của gói để yên (`not starting it`); unit `enabled` thì script có thể khởi động nó (suy luận, chưa chạy thử).
3. Các bước `File`, `Symlink`, `Absent` giữ trường `Reload`, tức là chạy `systemctl reload apache2` sau mỗi file. apache2 đang tắt nên `reload` lỗi, bước đầu tiên ghi file đã lỗi và cả job hoàn tác trước khi đổi gì. Với nginx cũng vậy. Kiểm cú pháp thì không ảnh hưởng: `WithoutReload` chỉ xoá `Reload`, giữ `Test`. Đây là suy luận theo mã (`steps/batch.go`, chú thích của `webSteps`), chưa chạy thử trên máy ảo.
4. Vì đổi sang Apache đã ghi `stack.web_source = ubuntu`, mà kho nginx.org và gói nginx của nó vẫn trên máy (ghim 900). Tin settings thì lúc về tpan dựng Plan cho nginx của Ubuntu, bỏ `conf.d/tpan.conf` và bước thêm user `nginx` vào `www-data`: nginx của nginx.org không đọc `sites-enabled`, mọi site rơi vào trang mặc định của nó. `source` hỏi chính `Check` của bước `AptRepo` (M12) xem kho stable hay mainline đang có. Bằng chứng ở 2.9: về nginx thì `web_source` là `nginx.org`, có `conf.d/tpan.conf`, worker chạy dưới user `nginx` thuộc nhóm `www-data`.
5. Gói giữ lại vì gỡ gói có thể kéo gói khác và giữ thì lần về nhanh (2.9: không cài gì). File thì gỡ vì `managed_files`. Nếu để lại: health vẫn kiểm lệch vhost nginx dù nginx không chạy. Lần chuyển ngược: bước ghi vhost nginx thấy file "đã đúng", bỏ qua, nên engine không ghi lại dòng của nó (3.9). Gỡ thì mọi file nginx được ghi lại từ template ở lần về, và `/etc/nginx` lúc ở Apache không còn file tpan nào (2.4: `find /etc/nginx -name '*tpan*'` không in gì).
6. `start apache2` xong, bước `web.swap` xong (98 ms). Bước kiểm `a` đạt hai lần liên tiếp. Bước kiểm `b` gọi `http://b.example.com/`, nhận 500 vì Apache đọc `.htaccess` có dòng `DongSaiCuPhapE2E on`; thử 5 lần, cách nhau 1 giây (`midCheckTries`, `midCheckWait` ở `internal/core/php.go`), rồi lỗi. Engine hoàn tác ngược: `midCheck` của `a` không làm gì; `WebSwap.Undo` chạy `systemctl stop apache2` rồi `systemctl start nginx` (148 ms, ở +5838 ms); trả `/etc/logrotate.d/tpan-sites` về bản nginx; gỡ link và vhost Apache của từng site, site mặc định 443 rồi 80. Bước cài gói và bước module không có gì để hoàn tác (lần này chúng "đã đúng, bỏ qua"). Kết quả: nginx `active enabled`, apache2 `inactive disabled`, cây `/etc/nginx` giống hệt trước (2.7). `b` trả 500 khoảng 6 giây: 5 lần thử mất hơn 4 giây, cộng thời gian bước kiểm `a` và hoàn tác (2.10: 209 lần `HTTP 500`, gián đoạn 6099 ms).
7. Mỗi site không trả lời 76 đến 165 ms, mất 1 đến 3 request, ở cả hai chiều, http và https, và cả nguồn nginx.org; bước `web.swap` mất 67 đến 100 ms (2.10). Đo bằng vòng lặp curl trong máy ảo, mỗi request một kết nối mới, nghỉ 20 ms, mốc 0 là `step.start` của `web.swap` trong `job_events`; con số là cận trên. Chưa nói được: khách giữ kết nối keep-alive bị cắt khi `stop`, khách ở ngoài máy, máy amd64, máy có tải thật. Và nếu job hoàn tác thì site lành gián đoạn thêm 185 đến 227 ms, site hỏng trả lỗi khoảng 6 giây.
8. Ba chỗ. `sitesAnswer` và bước kiểm giữa chừng chỉ cho site active (site trong thùng rác không phục vụ ai). `newConfig` chỉ ghi vhost và `.htaccess` cho site active, nhưng `anySSL` xét cả site trong thùng rác: khôi phục một site SSL về sau cần mod `ssl` và site mặc định 443 trên web server mới. `oldWebFiles` gỡ file cũ của mọi site, kể cả thùng rác: vhost của chúng vẫn nằm trong `sites-available` (2.2, bước 36). Khi khôi phục, `PrepareRestore` dựng vhost theo web server đang chạy, và trên Apache thì thêm `.htaccess` nếu thiếu (3.12).
9. Hỏi systemd chứ không khởi động lại: `scripts/vm.sh exec bash -c 'systemctl is-enabled apache2 nginx; ls /etc/systemd/system/multi-user.target.wants/ | grep -E "nginx|apache2"'`. Mong đợi `enabled` cho apache2, `disabled` cho nginx, và chỉ `apache2.service` trong thư mục `wants`. Đó đúng là thứ systemd đọc khi khởi động máy. Phần E của M10.log đã khởi động lại thật hai lần để xác nhận (2.8). Lệnh `ls .../multi-user.target.wants/` này chưa chạy trong M10.log; dòng `Removed "/etc/systemd/system/multi-user.target.wants/nginx.service"` ở bước `service.disable:nginx` (2.3) là bằng chứng gián tiếp.
10. Hai cách. Đọc thẳng bảng: `scripts/vm.sh root python3 -c 'import sqlite3; [print(r[0]) for r in sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True).execute("SELECT path FROM managed_files ORDER BY path")]'` (lệnh này chưa chạy đúng như vậy; kịch bản e2e dùng một đoạn Python dài hơn ở 2.4, ra `dòng còn trỏ vào /etc/nginx: 0`). Hoặc thử nghiệm như e2e: thêm một dòng vào `/etc/apache2/sites-available/tpan-b.example.com.conf` rồi chạy `scripts/vm.sh root tpan health`: chỉ `drift:/etc/apache2/sites-available/tpan-b.example.com.conf` lỗi (2.4), và output `tpan health --json` không có dòng `drift:` nào của `/etc/nginx` (2.8 liệt kê các dòng `drift:` sau khi khởi động lại). Sửa lại bằng `scripts/vm.sh root tpan health --fix --yes`.
