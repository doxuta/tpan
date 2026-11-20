# M14: cô lập sâu giữa các site và định tuyến có bằng chứng

Đọc kèm: `02-m2-site-wordpress.md` (user Linux `tp_<slug>`, pool, vhost, quyền 2750), `04-m4-health.md` (phép kiểm, sửa tự động, phép kiểm lệch), `10-m10-chuyen-web-server.md` (hai stack nginx và Apache), `13-m13-nhieu-nguoi-dung.md` (`siteVhost`, vhost tạm khoá), `docs/KE-HOACH.md` mục 5 và 6.13, `docs/thay-doi/M14-nghien-cuu.md` (đã thử gì trước khi viết mã) và phần "Đã chốt khi làm M14" ở mục 6.13 của `docs/KE-HOACH.md` (quyết định, chỗ khác đặc tả, kết quả e2e).

Mốc này sinh ra từ hai câu người review hỏi sau buổi training 20/11/2026. File sắp theo đúng hai câu đó:

1. Vì sao nhiều website trên cùng một panel mà request không đi nhầm thư mục mã nguồn? Trả lời ở mục 3, phần A (3.1 tới 3.8).
2. Cơ chế chống local attack là gì? "Local attack" ở đây: một site bị chiếm (kẻ tấn công chạy được PHP dưới user của site đó) rồi lấn sang site khác trên cùng máy. Trả lời ở mục 3, phần B (3.9 tới 3.15).

Quy ước dùng suốt file, giống tài liệu nguồn: **A** là site đã bị chiếm, **B** là nạn nhân. Trong log, A là `a.example.com` (user `tp_a_example_com_7653e4`), B là `b.example.com` (user `tp_b_example_com_cd59d5`).

## 1. Mốc này làm được gì

- Gõ `tpan route explain a.example.com` là thấy request tới tên miền đó đi đường nào trên máy: vhost nào nhận, thư mục gốc nào, socket nào, pool PHP nào, chạy dưới user nào, PHP được mở thư mục nào. Tám chặng, mỗi chặng kèm lệnh đã chạy và output. Có chặng lỗi thì lệnh thoát mã 1.
- Site tạo bằng bản này đã chặn sẵn chín đường một site bị chiếm lấn sang site khác (L1 tới L9: OPcache dùng chung, `.htaccess`, symlink, log, `/proc`, vhost trùng tên). `tpan health` có phép kiểm riêng cho từng thứ.
- Trên Apache, mỗi site có một chế độ `.htaccess`: `an-toan` (mặc định), `tat`, `day-du`. Đổi bằng `tpan site set <domain> --htaccess ...`; TPAN quét `.htaccess` trước và từ chối kèm đúng file, số dòng, chỉ thị nếu việc đổi sẽ làm site báo 500.
- Máy nâng từ bản cũ chạy một lệnh `tpan security harden` là có đủ mọi thứ trên, trong một Plan có xem trước và hoàn tác được.
- Health chỉ ra được file vhost ai đó thêm tay trùng tên miền của site (`web.server_name.unique`) và kiểm vhost mặc định còn từ chối tên lạ (`web.default.rejects`). Màn Bảo mật có ma trận cô lập site × site.

## 2. Chạy thử

Mọi output dưới đây trích nguyên văn từ ba nguồn, mình ghi tên file và số dòng ở từng mục:

- `docs/bang-chung/M14.log`: `scripts/e2e-m14.sh chinh` ngày 20/11/2026, 22:35 tới 22:45, từ máy sạch, cả hai stack. Dòng cuối: `TỔNG KẾT: 270 khẳng định đạt, 0 khẳng định không đạt`.
- `docs/bang-chung/M14-nang-cap.log`: `scripts/e2e-m14.sh nang-cap`, 22:45 tới 22:50, cài v0.1.2 rồi nâng. Dòng cuối: `TỔNG KẾT: 155 khẳng định đạt, 0 khẳng định không đạt`.
- `docs/bang-chung/M14-nghien-cuu/`: chín script `t1` tới `t9` (và `symlink-lab`) cùng file `.ket-qua.txt` của từng cái, chạy trước khi viết mã.

Những điều cần biết khi đọc:

- Mình là thợ viết, không được dùng máy ảo trong phiên viết file này. Không lệnh nào dưới đây do mình vừa chạy; output là của hai lần chạy e2e nói trên. Chỗ nào log không có thì mình ghi "chưa chạy".
- Máy ảo trong log là `tpan-m14` (Multipass, Ubuntu 24.04), tên nằm trong file `.tpan-vm` của worktree mốc này. Ở repo chính không có file đó thì `scripts/vm.sh` dùng `tpan-test`. Lệnh gõ y hệt: `scripts/vm.sh exec X` chạy X trong máy ảo, `scripts/vm.sh root X` chạy bằng quyền root. Mọi lệnh gõ trên Mac, trong thư mục repo.
- Bản tpan trong log là `v0.1.2-48-g20e3a22-dirty`. `dirty` nghĩa là lúc chạy có file chưa commit; `git status` ở `M14.log` dòng 113 tới 127 liệt kê chúng, trong đó có `internal/checks/site.go` và `scripts/vm.sh`. Đó là các bản sửa của đợt chạy thật, commit ngay sau (mục 5.9).
- Kịch bản in lệnh bằng `$*` nên trong log mất dấu nháy. Ở các khối lệnh dưới đây mình viết lại có nháy để dán vào zsh được; output giữ nguyên. Chỗ nào mình cắt output thì có dấu `...` và một câu nói rõ đã cắt gì.
- Kịch bản che IP (`<vm>`, `<vm-v6>`) và mật khẩu. Máy ảo không có tên miền thật, nên chặng DNS trong mọi output là `[cảnh báo]`; các lần gọi thử dùng `curl --resolve a.example.com:80:127.0.0.1`.
- Bộ phép thử tấn công `scripts/e2e-m14/tan-cong.sh` **chỉ chạy trên máy ảo test**, bằng root. Không chạy trên VPS hay máy có khách: nó đặt file thử vào thư mục của hai site, sửa `.htaccess` của site A, đặt một vhost lạ trùng tên rồi reload web server, và khi L4 lộ thì restart PHP-FPM. Nó tự dọn ở cuối, nhưng trong lúc chạy site A có lúc báo 500.

### 2.1 Chạy lại cả bộ, và riêng bộ phép thử tấn công

Chạy trên Mac (lệnh đầu đưa máy ảo về snapshot sạch, mất mọi thứ đang có trong máy ảo):

```bash
scripts/e2e-m14.sh chinh      # nginx, Apache, tính năng: khoảng 10 phút trong log
scripts/e2e-m14.sh nang-cap   # cần dist/release/v0.1.2, đầu script ghi cách tải
```

Khi máy ảo đã có stack và hai site, chạy riêng bộ phép thử (trên Mac):

```bash
scripts/vm.sh root bash -s -- a.example.com b.example.com < scripts/e2e-m14/tan-cong.sh
```

Tham số một là site bị chiếm, tham số hai là nạn nhân. Output trên site tạo bằng bản M14, stack nginx (`M14.log` dòng 432 tới 456):

```
## Môi trường: stack nginx | A=a.example.com user=tp_a_example_com_7653e4 php=8.3 | B=b.example.com user=tp_b_example_com_cd59d5 php=8.3 | hậu tố 6a8fe8801ed6
KQ L1  CHAN PHP của A include wp-config.php của B: không lấy được (3 lần thử)
KQ L2  CHAN opcache_get_status từ A: bị chặn
KQ L3  BO   stack nginx không có .htaccess
KQ L4  BO   stack nginx không có .htaccess
KQ L9  BO   stack nginx không có .htaccess
KQ L5  CHAN symlink của A sang file của B: HTTP 404; sang mã nguồn PHP của B: HTTP 404
KQ N5  DAT  symlink của A trỏ vào file của chính A: HTTP 200
KQ N6  DAT  .user.ini của A đặt open_basedir=/: không đổi được
KQ L6  CHAN user A không đọc được access.log của B (drwxr-x--- root:tp_b_example_com_cd59d5)
KQ N7  DAT  user B đọc được access.log của chính mình
KQ N8  DAT  web server vẫn ghi access.log của B (11 -> 12 dòng)
KQ L7  CHAN PHP của A gọi ps: thấy 0 tiến trình của user khác
KQ L8  CHAN vhost lạ trùng tên a.example.com (tên miền đang trả: 'DAY LA TRANG LA '): tpan health chỉ ra file aaa-trung-6a8fe8801ed6.conf (4 dòng)
KQ N1  DAT  user A không đọc được wp-config.php của B
KQ N2  DAT  user A không liệt kê được thư mục của B
KQ N3  DAT  user A không ghi được vào public của B
KQ N4  DAT  user A không ghi được vào socket FPM của B
KQ N11 DAT  Host viết hoa về đúng site A
KQ N12 DAT  Host có dấu chấm cuối về đúng site B
KQ N13 DAT  tên lạ: nginx đóng kết nối
KQ N14 DAT  trang chủ WordPress của A và B: HTTP 200 / 200
KQ N15 DAT  permalink của A: /hello-world/ HTTP 200, trang không có HTTP 404 do WordPress trả
KQ N16 DAT  file tĩnh của A: HTTP 200
TONG lo=0 chan=6 hong=0
```

Cách đọc: dòng `L<n>` là một đường tấn công, `LO` là còn mở, `CHAN` là đã chặn, `BO` là không thuộc stack này. Dòng `N<n>` là phép "đã thử từ trước, không lộ" và phép "site vẫn chạy bình thường": `DAT` hoặc `HONG`. Một bản vá chặn được lỗ mà làm hỏng WordPress thì dòng `N` sẽ đỏ.

Cùng script trên máy cài bằng v0.1.2, stack nginx, trước khi nâng (`M14-nang-cap.log` dòng 271 tới 294, mình chỉ trích các dòng `L`):

```
KQ L1  LO   PHP của A include wp-config.php của B: lấy được hằng DB_PASSWORD
KQ L2  LO   opcache_get_status từ A: GOI_DUOC thay 532 file cua B
KQ L5  LO   symlink của A sang file 640 của B: HTTP 200; sang mã nguồn PHP của B dưới tên .txt: HTTP 200
KQ L6  LO   user A đọc được access.log của B (drwxr-xr-x root:root )
KQ L7  LO   PHP của A gọi ps: thấy 118 tiến trình của user khác
KQ L8  LO   vhost lạ trùng tên a.example.com (tên miền đang trả: 'DAY LA TRANG LA '): tpan health không chỉ ra file trùng
TONG lo=6 chan=0 hong=0
```

Stack Apache, v0.1.2 (`M14-nang-cap.log` dòng 1773 tới 1798):

```
KQ L1  LO   PHP của A include wp-config.php của B: lấy được hằng DB_PASSWORD
KQ L2  LO   opcache_get_status từ A: GOI_DUOC thay 532 file cua B
KQ L3  LO   .htaccess SetEnv PHP_ADMIN_VALUE: user=tp_a_example_com_7653e4|open_basedir=/|ngoai=CO
KQ L4  LO   mã do A viết chạy dưới user của B: MA_CUA_A chay duoi user=tp_b_example_com_cd59d5|doc wp-config cua B=COFile not found.
KQ L9  LO   symlink sang file 640 của B sau khi .htaccess đổi Options: [Options FollowSymLinks: HTTP 200] [Options +FollowSymLinks: HTTP 403] [Options All: HTTP 200]
KQ L6  LO   user A đọc được access.log của B (drwxr-xr-x root:root )
KQ L7  LO   PHP của A gọi ps: thấy 118 tiến trình của user khác
KQ L8  LO   vhost lạ trùng tên a.example.com (tên miền đang trả: 'DAY LA TRANG LA '): tpan health không chỉ ra file trùng
TONG lo=8 chan=0 hong=0
```

Stack Apache, site tạo bằng bản M14 (`M14.log` dòng 2697 tới 2722):

```
KQ L1  CHAN PHP của A include wp-config.php của B: không lấy được (3 lần thử)
KQ L2  CHAN opcache_get_status từ A: bị chặn
KQ L3  CHAN .htaccess SetEnv PHP_ADMIN_VALUE: open_basedir giữ nguyên
KQ L4  CHAN SetHandler + ProxyFCGISetEnvIf sang socket của B: HTTP 500, mã của A không chạy dưới user B (cả lối RewriteRule [H=])
KQ N5  DAT  symlink của A sang file của B qua Apache: HTTP 403 (SymLinksIfOwnerMatch)
KQ L9  CHAN symlink sang file 640 của B sau khi .htaccess đổi Options: [Options FollowSymLinks: HTTP 500] [Options +FollowSymLinks: HTTP 500] [Options All: HTTP 500]
KQ N10 DAT  .htaccess có 'Options -Indexes' (plugin hay ghi): trang chủ A vẫn HTTP 200
KQ L6  CHAN user A không đọc được access.log của B (drwxr-x--- root:tp_b_example_com_cd59d5)
KQ L7  CHAN PHP của A gọi ps: thấy 0 tiến trình của user khác
KQ L8  CHAN vhost lạ trùng tên a.example.com (tên miền đang trả: 'DAY LA TRANG LA '): tpan health chỉ ra file aaa-trung-73eee72ae451.conf (4 dòng)
TONG lo=0 chan=8 hong=0
```

Gộp lại thành một bảng (cột "sau" là site mới của bản M14 hoặc site cũ đã qua `tpan security harden`; "bỏ" là lỗ không thuộc stack đó):

| | nginx, v0.1.2 | nginx, sau | Apache, v0.1.2 | Apache, sau, A ở `an-toan` | Apache, sau, A ở `day-du` |
|---|---|---|---|---|---|
| L1 | lộ | chặn | lộ | chặn | chặn |
| L2 | lộ | chặn | lộ | chặn | chặn |
| L3 | bỏ | bỏ | lộ | chặn | chặn |
| L4 | bỏ | bỏ | lộ | chặn | **lộ** |
| L5 | lộ | chặn | bỏ | bỏ | bỏ |
| L6 | lộ | chặn | lộ | chặn | chặn |
| L7 | lộ | chặn | lộ | chặn | chặn |
| L8 | lộ | chặn | lộ | chặn | chặn |
| L9 | bỏ | bỏ | lộ | chặn | **lộ** |

Cột cuối là giới hạn còn lại, mục 2.6 và 5.10 nói kỹ.

### 2.2 Site mới có sẵn gì (M14.log dòng 211 tới 236 và 414 tới 426)

Ngay sau `tpan stack install --web nginx --yes`, kịch bản đọc hai file cấp máy và hỏi PHP giá trị đang dùng (chạy trên Mac):

```bash
scripts/vm.sh root bash -c 'cat /etc/php/8.3/fpm/conf.d/99-tpan.ini; echo ---; cat /etc/systemd/system/php8.3-fpm.service.d/tpan.conf; echo ---; systemctl show -p ProtectProc php8.3-fpm; php-fpm8.3 -i 2>/dev/null | grep -E "^opcache\.(validate_permission|restrict_api)"'
```

```
; # Do tpan quản lý. Sửa tay sẽ bị tpan health báo lệch.
;
; Cô lập giữa các site trên cùng một bản PHP-FPM (docs/KE-HOACH.md mục 6.13).
; Mọi pool của một bản PHP dùng chung một bộ nhớ OPcache.

; Trước khi dùng một file đã cache, kiểm user của pool có quyền đọc file đó.
; Thiếu dòng này, PHP của site này include được file PHP đã cache của site khác.
opcache.validate_permission=1

; Chỉ script nằm dưới đường dẫn này mới gọi được các hàm opcache_*. Đường dẫn
; không tồn tại, nên không site nào xem được danh sách file hay xoá cache chung.
opcache.restrict_api=/usr/local/lib/tpan/opcache-api/
---
# Do tpan quản lý. Sửa tay sẽ bị tpan health báo lệch.
#
# Cô lập giữa các site (docs/KE-HOACH.md mục 6.13): tiến trình PHP của một site
# không thấy tiến trình của user khác trong /proc.
[Service]
ProtectProc=invisible
---
ProtectProc=invisible
opcache.restrict_api => /usr/local/lib/tpan/opcache-api/ => /usr/local/lib/tpan/opcache-api/
opcache.validate_permission => On => On
```

File đầu đóng L1 và L2, file sau đóng L7. Sau khi tạo hai site (chạy trên Mac):

```bash
scripts/vm.sh root bash -c 'grep -n disable_symlinks /etc/nginx/sites-available/tpan-*.conf; stat -c "%U:%G %a %n" /var/log/tpan/sites/* /var/log/tpan/sites/*/*.log'
```

```
/etc/nginx/sites-available/tpan-a.example.com.conf:17:    disable_symlinks if_not_owner from=$document_root;
/etc/nginx/sites-available/tpan-b.example.com.conf:17:    disable_symlinks if_not_owner from=$document_root;
root:tp_a_example_com_7653e4 750 /var/log/tpan/sites/a.example.com
root:tp_b_example_com_cd59d5 750 /var/log/tpan/sites/b.example.com
root:tp_a_example_com_7653e4 640 /var/log/tpan/sites/a.example.com/access.log
root:tp_a_example_com_7653e4 640 /var/log/tpan/sites/a.example.com/error.log
tp_a_example_com_7653e4:tp_a_example_com_7653e4 640 /var/log/tpan/sites/a.example.com/php-error.log
root:tp_b_example_com_cd59d5 640 /var/log/tpan/sites/b.example.com/access.log
root:tp_b_example_com_cd59d5 640 /var/log/tpan/sites/b.example.com/error.log
tp_b_example_com_cd59d5:tp_b_example_com_cd59d5 640 /var/log/tpan/sites/b.example.com/php-error.log
```

Dòng 17 của vhost đóng L5, quyền log đóng L6. Phép kiểm cô lập của health giờ có năm phép thử, mỗi phép một dòng bằng chứng (`M14.log` dòng 591 tới 596):

```
Bị từ chối như mong đợi: tp_a_example_com_7653e4 không chạm được vào file, log và socket của b.example.com (5 phép thử)
    $ runuser -u tp_a_example_com_7653e4 -- test -r /var/www/b.example.com/public/wp-config.php  (mã thoát 1)
    $ runuser -u tp_a_example_com_7653e4 -- test -x /var/www/b.example.com  (mã thoát 1)
    $ runuser -u tp_a_example_com_7653e4 -- test -w /var/www/b.example.com/public  (mã thoát 1)
    $ runuser -u tp_a_example_com_7653e4 -- test -r /var/log/tpan/sites/b.example.com/access.log  (mã thoát 1)
    $ runuser -u tp_a_example_com_7653e4 -- test -w /run/php/tpan-b_example_com_cd59d5-8.3.sock  (mã thoát 1)
```

### 2.3 Request đi đường nào: `tpan route explain` (M14.log dòng 991 tới 1049)

Chạy trên Mac:

```bash
scripts/vm.sh root tpan route explain a.example.com --evidence
```

```
Request tới a.example.com (nginx)

[cảnh báo] Tên miền trỏ về máy này: -
    a.example.com chưa có bản ghi A
    $ getent ahostsv4 a.example.com  (mã thoát 2)
    $ getent ahostsv6 a.example.com  (mã thoát 2)
    ...
[đạt] Vhost nhận tên miền: /etc/nginx/sites-enabled/tpan-a.example.com.conf:8
    Tên miền a.example.com (cổng 80) do vhost của chính site nhận: /etc/nginx/sites-enabled/tpan-a.example.com.conf:8
    $ nginx -T  (mã thoát 0)
      /etc/nginx/sites-enabled/tpan-a.example.com.conf:8: a.example.com (port 80)
[đạt] Thư mục gốc của vhost: /var/www/a.example.com/public
    Vhost phục vụ file từ đúng thư mục của site
    $ read /etc/nginx/sites-enabled/tpan-a.example.com.conf (tpan)  (mã thoát 0)
      10: root /var/www/a.example.com/public;
[đạt] Socket mà vhost chuyển .php tới: /run/php/tpan-a_example_com_7653e4-8.4.sock
    Vhost chuyển .php tới đúng socket của site
    $ read /etc/nginx/sites-enabled/tpan-a.example.com.conf (tpan)  (mã thoát 0)
      61: fastcgi_pass unix:/run/php/tpan-a_example_com_7653e4-8.4.sock;
[đạt] Pool PHP-FPM nghe socket đó: /etc/php/8.4/fpm/pool.d/tpan-a_example_com_7653e4.conf [tpan-a_example_com_7653e4]
    Socket do đúng pool của site nghe
    $ grep -n 'listen = /run/php/tpan-a_example_com_7653e4-8.4.sock' /etc/php/*/fpm/pool.d/*.conf (tpan)  (mã thoát 0)
      /etc/php/8.4/fpm/pool.d/tpan-a_example_com_7653e4.conf:5: [tpan-a_example_com_7653e4]
      /etc/php/8.4/fpm/pool.d/tpan-a_example_com_7653e4.conf:14: listen = /run/php/tpan-a_example_com_7653e4-8.4.sock
[đạt] User Linux mà pool chạy: tp_a_example_com_7653e4
    Pool chạy dưới đúng user riêng của site
    $ read /etc/php/8.4/fpm/pool.d/tpan-a_example_com_7653e4.conf (tpan)  (mã thoát 0)
      9: user = tp_a_example_com_7653e4
[đạt] Thư mục PHP của pool được mở (open_basedir): /var/www/a.example.com/
    PHP của site chỉ mở được file trong thư mục của chính site
    $ read /etc/php/8.4/fpm/pool.d/tpan-a_example_com_7653e4.conf (tpan)  (mã thoát 0)
      29: php_admin_value[open_basedir] = /var/www/a.example.com/
[đạt] Gọi thật vào site trên chính máy: http://a.example.com/
    Site trả 200, đúng là WordPress của a.example.com
    $ curl -sS --max-time 15 --resolve a.example.com:80:127.0.0.1 -w 'TPAN_HTTP_CODE=%{http_code}' http://a.example.com/  (mã thoát 0)
      HTTP 200, 69638 byte
      ...

Không chặng nào lỗi: request tới a.example.com đi đúng vhost, thư mục, pool và user của chính site.
```

(Hai chỗ `...` là mình cắt hai dòng `ip -o addr` đã che IP và phần HTML của trang chủ.) Socket mang đuôi `-8.4` vì ngay trước đó kịch bản đã cài PHP 8.4 và chuyển site A sang; site B vẫn ở 8.3. Dòng `(tpan)` sau một lệnh nghĩa là TPAN tự đọc file bằng Go rồi in lệnh tương đương để bạn gõ lại được, không phải nó gọi `grep`.

Không có `--evidence` thì chặng đạt chỉ in hai dòng đầu, chặng không đạt vẫn in đủ lệnh và output (`M14.log` dòng 960 tới 984). Trên Apache, cùng lệnh, chặng vhost đọc từ `apache2ctl -S` và ra `/etc/apache2/sites-enabled/tpan-a.example.com.conf:5`, chặng thư mục gốc là dòng `7: DocumentRoot "/var/www/a.example.com/public"`, chặng socket là dòng `80: SetHandler "proxy:unix:/run/php/tpan-a_example_com_7653e4-8.3.sock|fcgi://localhost"` (`M14.log` dòng 3746 tới 3776).

### 2.4 Ai đó thêm tay một vhost trùng tên (L8) (M14.log dòng 1237 tới 1335 và 1500 tới 1515)

Kịch bản đặt file `/etc/nginx/sites-enabled/aaa-e2e-la.conf` khai `server_name a.example.com; root /srv/e2e-trang-la;` rồi kiểm cú pháp và reload:

```
2026/11/20 22:39:11 [warn] 28338#28338: conflicting server name "a.example.com" on 0.0.0.0:80, ignored
nginx: the configuration file /etc/nginx/nginx.conf syntax is ok
nginx: configuration file /etc/nginx/nginx.conf test is successful
nginx -t mã 0
file lạ: /etc/nginx/sites-enabled/aaa-e2e-la.conf
tên miền đang trả: DAY LA TRANG LA 
```

`nginx -t` vẫn mã 0, chỉ có một dòng `[warn]`. Tên miền của A giờ trả trang lạ. Chạy trên Mac:

```bash
scripts/vm.sh root tpan route explain a.example.com
```

```
[lỗi] Vhost nhận tên miền: /etc/nginx/sites-enabled/aaa-e2e-la.conf:3
    Tên miền a.example.com (cổng 80) đang do file lạ /etc/nginx/sites-enabled/aaa-e2e-la.conf:3 nhận, không phải vhost của site (/etc/nginx/sites-enabled/tpan-a.example.com.conf). Web server dùng vhost được nạp trước, nên các chặng dưới là của file lạ đó
    $ nginx -T  (mã thoát 0)
      /etc/nginx/sites-enabled/aaa-e2e-la.conf:3: a.example.com (port 80)
      /etc/nginx/sites-enabled/tpan-a.example.com.conf:8: a.example.com (port 80)
      2026/11/20 22:39:13 [warn] 28420#28420: conflicting server name "a.example.com" on 0.0.0.0:80, ignored
[lỗi] Thư mục gốc của vhost: /srv/e2e-trang-la
    Vhost phục vụ file từ /srv/e2e-trang-la, không phải thư mục của site (/var/www/a.example.com/public)
    $ read /etc/nginx/sites-enabled/aaa-e2e-la.conf (tpan)  (mã thoát 0)
      4: root /srv/e2e-trang-la;
[lỗi] Socket mà vhost chuyển .php tới: -
    Vhost ở /etc/nginx/sites-enabled/aaa-e2e-la.conf:3 không chuyển .php tới socket nào: PHP của site không chạy qua vhost này
...
[lỗi] Gọi thật vào site trên chính máy: http://a.example.com/
    Trả 200 nhưng trang không phải WordPress
      HTTP 200, 16 byte
      DAY LA TRANG LA

Có 4 chặng lỗi: request tới a.example.com không đi đúng đường của site. Xem các chặng [lỗi] ở trên.
```

Lệnh thoát mã 1. `tpan health --json` cùng lúc (kịch bản in gọn mỗi phép kiểm một dòng):

```
--- fail web.server_name.unique (chặng web): Tên miền a.example.com được khai ở hai nơi: /etc/nginx/sites-enabled/tpan-a.example.com.conf:8 (cổng 80) và /etc/nginx/sites-enabled/aaa-e2e-la.conf:3 (cổng 80)
--- fail http.site:a.example.com (chặng e2e): Trả 200 nhưng trang không phải WordPress
```

Trước M14 chỉ có dòng thứ hai: biết triệu chứng, không biết nguyên nhân. Giờ dòng đầu nêu đúng hai file và số dòng. TPAN không xoá file nó không ghi; gỡ file lạ là việc của bạn. Gỡ xong, `route explain` thoát 0 lại (`M14.log` dòng 1567 tới 1594). Trên Apache kết quả cùng dạng: `apache2ctl -S` liệt kê `aaa-e2e-la.conf:1` đứng trước `tpan-a.example.com.conf:5` và tên miền trả trang lạ (`M14.log` dòng 4014 tới 4062).

### 2.5 Tên lạ gõ vào máy (M14.log dòng 2044 tới 2076 và 5572 tới 5598)

Chạy trên Mac, stack nginx, khi đã có một site bật SSL:

```bash
scripts/vm.sh exec bash -c 'curl -sk -m 10 --resolve "tpan-e2e-la.invalid:443:127.0.0.1" -o /dev/null -w "http=%{http_code}\n" "https://tpan-e2e-la.invalid/"; echo "curl mã $?"; curl -s -m 10 -o /dev/null -w "http=%{http_code}\n" -H "Host: tpan-e2e-la.invalid" http://127.0.0.1/; echo "curl mã $?"'
```

```
http=000
curl mã 35
http=000
curl mã 52
```

Mã 52 của curl là "máy chủ đóng kết nối, không trả gì" (nginx trả 444 ở cổng 80). Mã 35 là bắt tay TLS không xong (cổng 443). Bằng chứng mà phép kiểm `web.default.rejects` của health tự ghi:

```
Tên miền lạ bị từ chối ở cổng 80 và cổng 443
    $ curl -sS --max-time 5 -o /dev/null -w '%{http_code}' -H 'Host: tpan-6f505e9e.invalid' http://127.0.0.1/  (mã thoát 52)
      000
curl: (52) Empty reply from server
    $ curl -sS -k --max-time 5 -o /dev/null -w '%{http_code}' --resolve tpan-6f505e9e.invalid:443:127.0.0.1 https://tpan-6f505e9e.invalid/  (mã thoát 35)
      000
curl: (35) OpenSSL/3.0.13: error:0A000458:SSL routines::tlsv1 unrecognized name
```

Cùng lệnh trên stack Apache cho `http=403`, `curl mã 0` ở cả hai cổng.

### 2.6 Ba chế độ `.htaccess` trên Apache (M14.log dòng 2642 tới 2643 và 3086 tới 3711)

Site mới trên Apache ở chế độ `an-toan`. Hai dòng quyết định của vhost:

```
        AllowOverride Options=Indexes,MultiViews,SymLinksIfOwnerMatch
        AllowOverrideList <Files <FilesMatch <IfModule <If <Else <ElseIf <Limit <LimitExcept <RequireAll <RequireAny <RequireNone RewriteEngine RewriteRule RewriteCond RewriteBase RewriteOptions Require Order Allow Deny Satisfy AuthType AuthName AuthUserFile AuthGroupFile AuthBasicProvider Header RequestHeader ExpiresActive ExpiresByType ExpiresDefault FileETag AddType AddEncoding AddCharset AddDefaultCharset AddOutputFilterByType AddOutputFilter SetOutputFilter SetEnv SetEnvIf SetEnvIfNoCase UnsetEnv BrowserMatch BrowserMatchNoCase DirectoryIndex ErrorDocument Redirect RedirectMatch RedirectPermanent RedirectTemp LimitRequestBody ServerSignature AddLanguage DefaultLanguage
```

Sang chế độ `tat` (chạy trên Mac; thêm `--dry-run` để chỉ xem trước):

```bash
scripts/vm.sh root tpan site set a.example.com --htaccess tat --yes
scripts/vm.sh root grep -nE 'AllowOverride|FallbackResource' /etc/apache2/sites-available/tpan-a.example.com.conf
```

```
Job d5f0b82c6141a81c: thành công
Kiểm chứng:
  [đạt] Vhost của site có đủ các dòng chặn cô lập: Vhost Apache gỡ PHP_VALUE, PHP_ADMIN_VALUE và khối AllowOverride khớp chế độ .htaccess "tat"
  [đạt] Cấu hình Apache hợp lệ: Cấu hình Apache hợp lệ
  [đạt] .htaccess của site chỉ dùng chỉ thị được phép: Apache không đọc .htaccess của site này (chế độ "tat")
  [đạt] Gọi HTTP vào site trả 200 và đúng là WordPress của site: Site trả 200, đúng là WordPress của a.example.com
```

```
21:        AllowOverride None
33:        # chuyển cho index.php) do dòng FallbackResource làm, thay cho các
35:        AllowOverride None
36:        FallbackResource /index.php
41:    # của plugin) cũng không thành mã chạy được. AllowOverride None để một
44:        AllowOverride None
```

Ở `tat`, kịch bản thêm một dòng `SetHandler ...` vào `.htaccess` của A: site vẫn 200 vì Apache không đọc file đó nữa. Rồi nó đòi về `an-toan` trong lúc dòng đó còn:

```
Từ chối: không chuyển a.example.com sang chế độ an-toan được: .htaccess của site có 1 chỗ nằm ngoài danh sách chỉ thị cho phép, và Apache sẽ trả 500 cho mọi request dưới thư mục chứa file đó:
/var/www/a.example.com/public/.htaccess:1: SetHandler
Hai lối ra: (1) sửa hoặc xoá các dòng trên rồi chạy lại; (2) để site ở chế độ day-du (sudo tpan site set a.example.com --htaccess day-du) và nhận cảnh báo đi kèm trong health.
```

Mã thoát 1, bảng `sites` vẫn ghi `tat`, máy không đổi gì. Gỡ dòng đó rồi chạy lại thì job thành công.

Sang `day-du`, bản xem trước in thêm một câu nói rõ rủi ro, và health của site có cảnh báo thường trực:

```
Chế độ day-du: site này ghi được cấu hình Apache qua .htaccess và, nếu bị chiếm, có thể chạy mã dưới user của site khác. health sẽ luôn cảnh báo cho tới khi chuyển về an-toan.
```

```
[cảnh báo] .htaccess của site chỉ dùng chỉ thị được phép: Site ở chế độ .htaccess "day-du": nó ghi được cấu hình Apache (AllowOverride All)
    Nguyên nhân: Với AllowOverride All, file .htaccess do site ghi được dùng mọi chỉ thị của Apache. Nếu site bị chiếm, kẻ tấn công có thể gửi request sang socket PHP-FPM của site khác kèm cấu hình PHP do nó chọn, tức là chạy mã dưới user của site khác, và bật FollowSymLinks để đọc file của site khác qua symlink. Chế độ "an-toan" chỉ cho các chỉ thị WordPress và plugin phổ biến cần; tpan quét .htaccess trước khi chuyển và từ chối nếu có chỉ thị sẽ làm site báo 500.
    Sửa tay (gõ trên máy này, thư mục nào cũng được):
      sudo tpan site set a.example.com --htaccess an-toan
      sudo tpan health a.example.com
```

Cảnh báo đó nói thật. Bộ phép thử chạy lúc A ở `day-du` (`M14.log` dòng 3394 tới 3419):

```
KQ L3  CHAN .htaccess SetEnv PHP_ADMIN_VALUE: open_basedir giữ nguyên
KQ L4  LO   mã do A viết chạy dưới user của B: MA_CUA_A chay duoi user=tp_b_example_com_cd59d5|doc wp-config cua B=COFile not found.
KQ L9  LO   symlink sang file 640 của B sau khi .htaccess đổi Options: [Options FollowSymLinks: HTTP 200] [Options +FollowSymLinks: HTTP 403] [Options All: HTTP 200]
TONG lo=2 chan=6 hong=0
```

Cuối cùng, tình huống hay gặp nhất ngoài đời: site ở `an-toan`, một plugin ghi thêm dòng lạ. Kịch bản thêm `php_value memory_limit 256M` vào cuối `.htaccess` của A (`M14.log` dòng 3584 tới 3660):

```
trang-chu=500
permalink=500
khong-co=500 dau-wordpress=0
wp-admin=500 -> 
tinh=500
```

```
[lỗi] .htaccess của site chỉ dùng chỉ thị được phép: 1 chỉ thị ngoài danh sách cho phép trong .htaccess; đầu tiên: /var/www/a.example.com/public/.htaccess:17: php_value
    Nguyên nhân: Site ở chế độ .htaccess "an-toan": vhost chỉ cho một danh sách chỉ thị cố định. Gặp một chỉ thị ngoài danh sách (hoặc một .htaccess không đọc được), Apache trả 500 cho mọi request dưới thư mục chứa file đó; thường là một plugin vừa ghi thêm. Có hai lối ra: sửa hoặc xoá dòng đó (hai lệnh đầu), hoặc nếu site thật sự cần chỉ thị này thì chuyển site sang chế độ "day-du" (lệnh thứ ba) và nhận cảnh báo đi kèm.
    Sửa tay (gõ trên máy này, thư mục nào cũng được):
      sudo sed -n 17p /var/www/a.example.com/public/.htaccess
      sudo nano +17 /var/www/a.example.com/public/.htaccess
      sudo tpan site set a.example.com --htaccess day-du
      sudo tpan health a.example.com
```

Cả site A báo 500 (đó là giá của chế độ chặt), site B không sao, và health nêu đúng file, dòng 17, chỉ thị `php_value`. Gỡ dòng đó thì site 200 lại ngay, không cần reload.

### 2.7 Nâng một máy cũ (M14-nang-cap.log)

Kịch bản cài file chạy v0.1.2 đã phát hành, cài stack, tạo hai site, rồi thay file chạy bằng bản mới (`make vm-deploy`). Lưu ý: nó thay file chạy trực tiếp, không đi qua `tpan self-update`; đường `self-update` rồi `harden` chưa có trong log của M14 (mục 2.9).

**Health ngay sau khi thay file chạy** (stack nginx, dòng 411 tới 591). Chạy trên Mac: `scripts/vm.sh root tpan health`. Phần máy và stack:

```
[lỗi] OPcache của PHP-FPM 8.3 kiểm quyền file và khoá API: OPcache của PHP-FPM 8.3 chưa được siết: không có file /etc/php/8.3/fpm/conf.d/99-tpan.ini; opcache.validate_permission đang là Off (cần On); opcache.restrict_api đang là "no value" (cần /usr/local/lib/tpan/opcache-api/)
    ...
    Sửa tay (gõ trên máy này, thư mục nào cũng được):
      sudo tpan security harden
      sudo cat /etc/php/8.3/fpm/conf.d/99-tpan.ini
      sudo php-fpm8.3 -i | grep -E '^opcache\.(validate_permission|restrict_api) '
[cảnh báo] PHP-FPM 8.3 không thấy tiến trình của user khác: Dịch vụ php8.3-fpm có ProtectProc=default, cần invisible
    ...
      sudo tpan security harden
      sudo systemctl show php8.3-fpm -p ProtectProc --value
```

Bản in gọn của `tpan health --json` cho cả máy (dòng 577 tới 591; mình bỏ các dòng DNS, chứng chỉ và nhóm tpan không liên quan, và cắt đuôi ba dòng dài bằng `...`):

```
số phép kiểm theo trạng thái: fail=7 pass=39 unknown=3 warn=7
--- fail php.opcache:8.3 (chặng php): OPcache của PHP-FPM 8.3 chưa được siết: ...
--- warn php.protectproc:8.3 (chặng php): Dịch vụ php8.3-fpm có ProtectProc=default, cần invisible
--- fail log.perm:a.example.com (chặng web): Quyền log của site sai: /var/log/tpan/sites/a.example.com thuộc root:root, phải là root:tp_a_example_com_7653e4; /var/log/tpan/sites/a.example.com có quyền 0755, phải là 0750; ...
--- fail web.hardened:a.example.com (chặng web): Vhost của site chưa đủ dòng chặn: thiếu dòng "disable_symlinks if_not_owner from=$document_root;"
--- fail isolation:a.example.com:b.example.com (chặng wordpress): tp_a_example_com_7653e4 CHẠM ĐƯỢC vào site b.example.com (đọc được access.log): hai site không còn cô lập
--- fail isolation:b.example.com:a.example.com (chặng wordpress): tp_b_example_com_cd59d5 CHẠM ĐƯỢC vào site a.example.com (đọc được access.log): hai site không còn cô lập
--- fail log.perm:b.example.com (chặng web): Quyền log của site sai: ...
--- fail web.hardened:b.example.com (chặng web): Vhost của site chưa đủ dòng chặn: thiếu dòng "disable_symlinks if_not_owner from=$document_root;"
```

Bảy lỗi, thuộc bốn loại: `php.opcache`, `log.perm`, `web.hardened`, và `isolation` (vì phép cô lập giờ thử cả log). Thêm một cảnh báo `php.protectproc`. Phần "Sửa tay" của cả bốn loại đều có `sudo tpan security harden`. Bảng `sites` ghi site cũ ở chế độ `day-du` (dòng 626): migration 1400 nói đúng thực tế trên đĩa.

**Thao tác trên site cũ bị hoàn tác** (dòng 631 tới 775). Kịch bản chạy lại `tpan site add a.example.com` trước khi harden:

```
- Tạo thư mục /var/log/tpan/sites/a.example.com ...
  xong
- Tạo /var/log/tpan/sites/a.example.com/access.log với đúng quyền và chủ ...
  xong
...
- Ghi vhost nginx cho a.example.com ...
  xong
...
  [đạt] Log của site không đọc được bởi site khác
  [đạt] Vhost của site có đủ các dòng chặn cô lập
  [lỗi] Site a.example.com không chạm được vào file, log và socket của site b.example.com
  [đạt] Site b.example.com không chạm được vào file, log và socket của site a.example.com
- Hoàn tác: Ghi vhost nginx cho a.example.com ...
  đã hoàn tác
- Hoàn tác: Tạo /var/log/tpan/sites/a.example.com/error.log với đúng quyền và chủ ...
  đã hoàn tác
...
Job cf76772eafeae9df: hỏng, đã hoàn tác sạch
Lỗi: kiểm chứng isolation:a.example.com:b.example.com không đạt: tp_a_example_com_7653e4 CHẠM ĐƯỢC vào site b.example.com (đọc được access.log): hai site không còn cô lập
```

Job sửa được phần của A (log, vhost) nhưng kiểm chứng còn thử A đọc log của B, mà log của B vẫn `0755`. Kiểm chứng lỗi thì engine hoàn tác. So ảnh chụp trước và sau: `diff` không ra dòng nào, site A vẫn 200. Lời nhắn đi kèm chỉ sang `sudo tpan security harden`. Mục 3.15 giải thích vì sao chủ ý làm vậy.

**Xem trước rồi áp** (dòng 934 tới 1113). Chạy trên Mac:

```bash
scripts/vm.sh root tpan security harden --dry-run
scripts/vm.sh root tpan security harden --yes
```

Bản xem trước, mình giữ tên các bước và bỏ phần "Vì sao" của từng bước:

```
Kế hoạch: Áp bản vá cô lập
12 bước sẽ chạy, 4 bước đã đúng sẽ bỏ qua, 1 bước có thể gián đoạn dịch vụ.

 1. [sẽ chạy] Bật kiểm quyền file và khoá API của OPcache cho php8.3-fpm  (php.harden.opcache:8.3)
    $ php-fpm8.3 -t
    $ systemctl reload php8.3-fpm
    tạo /etc/php/8.3/fpm/conf.d/99-tpan.ini
 2. [sẽ chạy] Ẩn tiến trình của user khác với PHP của php8.3-fpm  (php.harden.proc:8.3)
    NGUY HIỂM: bước này có thể làm gián đoạn dịch vụ.
    $ systemctl daemon-reload
    $ php-fpm8.3 -t
    $ systemctl restart php8.3-fpm
    tạo /etc/systemd/system/php8.3-fpm.service.d/tpan.conf
 3. [sẽ chạy] Ghi /etc/logrotate.d/tpan-sites  (file:/etc/logrotate.d/tpan-sites)
 4. [sẽ chạy] Tạo thư mục /var/log/tpan/sites/a.example.com  (dir:/var/log/tpan/sites/a.example.com)
 5. [sẽ chạy] Tạo /var/log/tpan/sites/a.example.com/access.log với đúng quyền và chủ  (...)
 6. [sẽ chạy] Tạo /var/log/tpan/sites/a.example.com/error.log với đúng quyền và chủ  (...)
 7. [đã đúng, bỏ qua] Tạo /var/log/tpan/sites/a.example.com/php-error.log với đúng quyền và chủ  (...)
 8. tới 11. như 4 tới 7, cho b.example.com
12. [sẽ chạy] Ghi vhost nginx cho a.example.com  (file:/etc/nginx/sites-available/tpan-a.example.com.conf)
13. [đã đúng, bỏ qua] Bật /etc/nginx/sites-enabled/tpan-a.example.com.conf → ...
14. [sẽ chạy] Ghi vhost nginx cho b.example.com  (...)
15. [đã đúng, bỏ qua] Bật /etc/nginx/sites-enabled/tpan-b.example.com.conf → ...
16. [sẽ chạy] Kiểm cấu hình rồi nạp lại nginx một lần cho mọi vhost vừa ghi  (service.reload:nginx)
    $ nginx -t
    $ systemctl reload nginx
Chế độ .htaccess:
  Chuyển sang an-toan (2 site, .htaccess không có chỉ thị nào ngoài danh sách cho phép):
    a.example.com
    b.example.com
```

(Dòng "8. tới 11." là mình gộp bốn bước giống hệt của site B; các chỗ `(...)` và `→ ...` là mã bước và đường dẫn mình cắt cho ngắn.) Bước 2 là bước duy nhất gắn nhãn nguy hiểm: restart PHP-FPM ngắt request PHP đang chạy trong khoảng một giây. `--dry-run` không đổi gì trên máy (`diff` trước và sau rỗng, dòng 1012 tới 1014).

Chạy thật: mười hai bước chạy, bốn bước bỏ qua, rồi kiểm chứng 23 dòng đều `[đạt]`, `Job 961c1618f5b53113: thành công`. Kịch bản ghi nhận thời gian: 2 giây với hai site. Xem trước lần nữa (dòng 1111 tới 1132):

```
Kế hoạch: Áp bản vá cô lập
0 bước sẽ chạy, 16 bước đã đúng sẽ bỏ qua, 0 bước có thể gián đoạn dịch vụ.
```

Sau đó health hết `[lỗi]` và bộ phép thử cho `TONG lo=0 chan=6 hong=0` (dòng 1367 tới 1390).

**Site "vướng" trên Apache** (dòng 1804 tới 1837 và 2505 tới 3027). Trước khi nâng, kịch bản thêm dòng `Options +FollowSymLinks` vào đầu `.htaccess` của `b.example.com`, kiểu dòng mà plugin và bài hướng dẫn cũ hay ghi. Harden in ra trước khi chạy:

```
Chế độ .htaccess:
  Chuyển sang an-toan (1 site, .htaccess không có chỉ thị nào ngoài danh sách cho phép):
    a.example.com
  Giữ day-du (1 site) vì .htaccess có chỉ thị ngoài danh sách cho phép; ở an-toan các dòng này làm site báo 500:
    b.example.com
      /var/www/b.example.com/public/.htaccess:1: Options FollowSymLinks
  Site giữ day-du vẫn ghi được cấu hình Apache và health tiếp tục cảnh báo. Sửa các dòng trên rồi chạy: sudo tpan site set <domain> --htaccess an-toan
```

Job vẫn thành công cho cả máy. Sau job: `a.example.com` ở `an-toan`, `b.example.com` ở `day-du` (bảng `sites`, dòng 2812 tới 2820); health không còn `[lỗi]`; trong các phép kiểm của M14 chỉ còn một cảnh báo, `web.htaccess:b.example.com` (dòng 2946 tới 2962). Bộ phép thử chạy hai chiều:

```
A=a.example.com (an-toan), B=b.example.com:   TONG lo=0 chan=8 hong=0
A=b.example.com (day-du),  B=a.example.com:   TONG lo=2 chan=6 hong=0
```

(Hai dòng trên là mình tóm từ dòng 2991 và 3023; hai lỗ còn lộ ở chiều thứ hai là L4 và L9, dòng 3001 và 3004.) Nghĩa là site vướng mà bị chiếm thì vẫn chạy được mã dưới user của site kia. Muốn đóng: bỏ dòng `Options +FollowSymLinks` rồi `sudo tpan site set b.example.com --htaccess an-toan`.

**Harden hỏng giữa chừng thì máy về như trước** (dòng 775 tới 932). Kịch bản đặt một vhost sai cú pháp ngoài TPAN (`zzz-e2e-hong.conf`) rồi chạy harden. Bước ghi vhost đầu tiên lỗi ở `nginx -t`, engine hoàn tác chín bước đã chạy, kể cả file ini và drop-in:

```
Job 52fbb446a99b3689: hỏng, đã hoàn tác sạch
Lỗi: bước file:/etc/nginx/sites-available/tpan-a.example.com.conf: thực hiện: kiểm cấu hình sau khi ghi /etc/nginx/sites-available/tpan-a.example.com.conf: nginx -t: mã thoát 1: 2026/11/20 22:47:13 [emerg] 15905#15905: unknown directive "chi_thi_khong_co" in /etc/nginx/sites-enabled/zzz-e2e-hong.conf:4
```

`diff` ảnh chụp trước và sau rỗng, hai site vẫn 200.

### 2.8 Log vẫn xoay đúng (M14.log dòng 719 tới 751)

Quyền log chặt hơn kéo theo một việc: sau khi logrotate đổi tên file, web server phải mở lại được file mới. Kịch bản gọi hai request, ép xoay bằng `logrotate -f /etc/logrotate.d/tpan-sites`, gọi ba request nữa:

```
trước khi xoay: access.log có 28 dòng, 2 dòng mang dấu truoc
logrotate mã 0
drwxr-x--- 2 root                    tp_a_example_com_7653e4 4096 Nov 20 22:38 .
-rw-r----- 1 root                    tp_a_example_com_7653e4  300 Nov 20 22:38 access.log
-rw-r----- 1 root                    tp_a_example_com_7653e4 2895 Nov 20 22:38 access.log.1
...
sau khi xoay: access.log có 3 dòng mang dấu sau
access.log.1 có 0 dòng mang dấu sau, 2 dòng mang dấu truoc
```

Ba request mới nằm ở `access.log`, không cái nào rơi vào `access.log.1`. Mục 5.4 kể lần làm sai ở đúng chỗ này.

### 2.9 Chưa chạy, hoặc không có trong log

- Mình không chạy lại lệnh nào trong phiên viết file này (thợ viết không dùng máy ảo).
- `tpan isolation probe`: chưa viết mã. `scripts/e2e-m14/tan-cong.sh` làm đúng việc đó cho máy ảo test.
- Giao diện mới (ma trận cô lập trên màn Bảo mật, nút "Request tới site này đi đường nào", khối chế độ `.htaccess` trên trang site): chưa ai xem bằng mắt. Xem bằng `make dev-api` và `make dev-web`. API phía sau thì đã chạy: `GET /api/v1/security/isolation` trả 200 cho admin và 403 cho user thường (`M14.log` dòng 4803 tới 4806 và 4992 tới 4995).
- Nâng bằng `tpan self-update` rồi `tpan security harden`: chưa có trong log M14 (kịch bản thay file chạy bằng `make vm-deploy`).
- Đường `tpan health --fix` (không qua harden) trên máy nâng từ v0.1.2: phần "Đã chốt khi làm M14" ở mục 6.13 của `docs/KE-HOACH.md` ghi đã chạy trên máy ảo và "6 lỗ lộ thành 0", nhưng hai file log của mốc không có lần chạy đó. Mình không trích được.
- Danh sách `AllowOverrideList` sinh tự động 129 tên làm rơi `RewriteEngine`: có trong `M14-nghien-cuu.md` mục 1.3 và chú thích của `internal/htaccess/htaccess.go`, không có file kết quả riêng trong `docs/bang-chung/M14-nghien-cuu/`.
- Không lệnh nào của mốc này chạy trên VPS.

## 3. Đi qua mã theo thứ tự chạy

Mục này có ba phần. Phần A đi theo một request (câu hỏi 1). Phần B đi theo kẻ tấn công (câu hỏi 2). Phần C đi theo lệnh `tpan security harden` trên máy cũ.

### Phần A. Vì sao request không đi nhầm thư mục mã nguồn

### 3.1 Câu trả lời để nói bằng lời

Bạn có thể nói như sau, rồi dùng các mục 3.2 tới 3.8 để trả lời câu hỏi đào sâu:

> Mọi site dùng chung một IP và một cổng. Thứ phân biệt chúng là tên miền mà trình duyệt gửi trong header `Host`. Web server so header đó với `server_name` để chọn đúng một vhost; tên không khớp vhost nào thì rơi vào vhost mặc định và bị từ chối. Mỗi vhost do TPAN sinh từ một template, chỉ biết một thư mục gốc và một socket PHP, cả hai suy từ cùng tên miền đó. Socket do pool PHP riêng của site nghe, pool chạy dưới user Linux riêng và bị khoá `open_basedir` vào thư mục của site. Nên kể cả khi một mắt xích cấu hình sai, quyền Linux và `open_basedir` vẫn không cho PHP của site này mở file của site kia. Và không cần tin lời: `tpan route explain` lần cả chuỗi từ cấu hình đang chạy, `tpan health` báo khi có vhost lạ trùng tên.

Cả chuỗi, mỗi mắt xích gắn với một dòng template:

```
trình duyệt gõ a.example.com
  -> DNS trả IP của máy                                        chặng 1 của route explain
  -> request tới cổng 80, mang header "Host: a.example.com"
  -> nginx so Host với server_name, chọn một khối server       site-nginx.conf.tmpl dòng 8
       không khớp khối nào: vhost mặc định, trả 444            nginx-default.conf.tmpl dòng 10, 26
  -> root của khối đó: /var/www/a.example.com/public           site-nginx.conf.tmpl dòng 10
  -> đường dẫn kết thúc bằng .php: gửi qua fastcgi_pass        site-nginx.conf.tmpl dòng 61
  -> socket /run/php/tpan-<slug>-<bản PHP>.sock                site-pool.conf.tmpl dòng 14
  -> pool của site, chạy dưới user tp_<slug>                   site-pool.conf.tmpl dòng 9
  -> PHP chỉ mở được file dưới /var/www/a.example.com/         site-pool.conf.tmpl dòng 29
```

Các số dòng 8, 10, 61 và 14, 9, 29 cũng chính là số dòng mà `tpan route explain --evidence` in ra ở mục 2.3, vì file trên máy sinh từ đúng các template đó (các file nằm trong `internal/templates/files/`).

### 3.2 Mắt xích 1: tên miền, header `Host`, `server_name`

DNS chỉ đưa request tới IP của máy. Tới đó mọi site như nhau: cùng IP, cùng cổng 80. Trình duyệt ghi tên miền người dùng gõ vào header `Host` của request. Với HTTPS, tên đó còn nằm trong lúc bắt tay TLS (gọi là SNI), để máy chủ biết đưa chứng chỉ nào.

`site-nginx.conf.tmpl`, dòng 4 tới 8:

```nginx
server {
    # Không có default_server: request không khớp tên nào rơi vào tpan-default.
    listen 80;
    listen [::]:80;
    server_name {{.Domain}};
```

`{{.Domain}}` là chỗ Go điền tên miền của site. Mỗi site một file `/etc/nginx/sites-available/tpan-<domain>.conf`, trong đó `server_name` đúng một tên. nginx so `Host` với `server_name` của mọi khối `server` nghe cổng đó và chọn khối khớp. Bằng chứng ở chặng 2 của mục 2.3: `tpan-a.example.com.conf:8: a.example.com (port 80)`.

Viết hoa hay thêm dấu chấm cuối vẫn về đúng site (`KQ N11`, `KQ N12` ở mục 2.1). `www.a.example.com` thì không: TPAN chưa có tên phụ, nên tên đó rơi vào vhost mặc định (`t6-dinh-tuyen.ket-qua.txt`: `Host: www.a.example.com -> HTTP 000, không ai`). Đây là việc để sau.

Vhost mặc định, `nginx-default.conf.tmpl` (mình bỏ chú thích):

```nginx
server {
    listen 80 default_server;
    listen [::]:80 default_server;
    server_name _;
    location /.well-known/acme-challenge/ {
        root {{.AcmeRoot}};
    }
    location / {
        return 444;
    }
}
```

`default_server` nghĩa là: không `server_name` nào khớp thì khối này nhận. Không có nó, nginx lấy khối `server` đầu tiên theo thứ tự tên file làm mặc định, và ai gõ thẳng IP hay trỏ một tên miền lạ về máy sẽ thấy nội dung của site đó. `444` là mã riêng của nginx: đóng kết nối, không trả gì. Ngoại lệ duy nhất là đường dẫn thử thách của Let's Encrypt. Ở cổng 443, `nginx-default-ssl.conf.tmpl` dùng `ssl_reject_handshake on;`: từ chối ngay lúc bắt tay, nên không lộ chứng chỉ (và tên) của site nào.

### 3.3 Mắt xích 2: `root`

`site-nginx.conf.tmpl`, dòng 10:

```nginx
    root {{.Public}};
```

`.Public` là `/var/www/<domain>/public`, lấy từ `site.Layout` (kiểu dữ liệu gom mọi đường dẫn của một site, có từ M2). Vhost đã chọn chỉ có một `root`, nên file tĩnh và file `.php` đều được tìm dưới thư mục này. Bằng chứng ở chặng 3: `10: root /var/www/a.example.com/public;`.

Với file tĩnh (ảnh, CSS), nginx tự đọc file. nginx chạy dưới user `www-data`, và mọi thư mục site có nhóm `www-data` với quyền `2750` (M2). Hãy nhớ chi tiết này: một user đọc được mọi site chính là gốc của lỗ L5 ở phần B.

### 3.4 Mắt xích 3: file `.php` đi qua socket riêng

`site-nginx.conf.tmpl`, dòng 52 tới 62 (mình bỏ chú thích):

```nginx
    location ~ \.php$ {
        try_files $uri =404;
        include fastcgi_params;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
        fastcgi_pass unix:{{.Socket}};
    }
```

nginx không chạy PHP. Nó chuyển request cho PHP-FPM qua một Unix socket (một "ống" nằm trên đĩa dưới dạng file, hai tiến trình cùng máy nói chuyện qua đó). Ba dòng đáng nói:

- `try_files $uri =404`: chỉ chuyển cho PHP file `.php` có thật dưới `root`.
- `SCRIPT_FILENAME`: nginx nói cho PHP biết chạy file nào. Giá trị là `$document_root` (chính là `root` ở trên) cộng đường dẫn trong URL, nên luôn nằm dưới thư mục của site này.
- `fastcgi_pass unix:{{.Socket}}`: socket của site, tên `/run/php/tpan-<slug>-<bản PHP>.sock`. `slug` suy từ tên miền (tối đa 16 ký tự đầu, ký tự lạ đổi thành `_`, cộng 6 ký tự hex của sha256 tên miền; mục 5 của KE-HOACH.md), nên hai site không trùng socket.

Bằng chứng ở chặng 4: `61: fastcgi_pass unix:/run/php/tpan-a_example_com_7653e4-8.4.sock;`.

### 3.5 Mắt xích 4 và 5: pool, user, `open_basedir`

`site-pool.conf.tmpl`, các dòng 5, 9, 10, 14 tới 17 và 29:

```ini
[{{.Pool}}]
user = {{.User}}
group = {{.User}}
listen = {{.Socket}}
listen.owner = www-data
listen.group = www-data
listen.mode = 0660
php_admin_value[open_basedir] = {{.Dir}}/
```

- `listen`: pool này, và chỉ pool này, nghe socket đó. Socket thuộc `www-data` với quyền `0660`: chỉ web server ghi được vào, user của site khác thì không (`KQ N4` ở mục 2.1).
- `user`: mã PHP của site chạy dưới `tp_<slug>`. User này không thuộc nhóm `www-data`, nên nhân Linux từ chối khi nó mở file của site khác (thư mục `2750`, `wp-config.php` `600`).
- `open_basedir`: PHP chỉ mở được file dưới thư mục của site. Đặt bằng `php_admin_value` nên file `.user.ini` của site không đè được (`KQ N6`). Dấu `/` ở cuối để `/var/www/a.example.com` không khớp luôn `/var/www/a.example.com.vn`.

Bằng chứng ở chặng 5, 6, 7 của mục 2.3. Điểm cần nói với người review: sau khi chọn đúng vhost, còn hai hàng rào không phụ thuộc cấu hình web server. Muốn "đi nhầm" phải lọt cả quyền Linux lẫn `open_basedir`.

### 3.6 Apache: cùng chuỗi, khác chữ

| Mắt xích | nginx | Apache (`site-apache.conf.tmpl`) | Bằng chứng Apache (M14.log dòng 3746 tới 3776) |
|---|---|---|---|
| chọn vhost theo tên | `server_name` (dòng 8) | `ServerName {{.Domain}}` (dòng 6) | `tpan-a.example.com.conf:5` (dòng mở `<VirtualHost>`) |
| thư mục gốc | `root` (dòng 10) | `DocumentRoot "{{.Public}}"` (dòng 7) | `7: DocumentRoot "/var/www/a.example.com/public"` |
| chuyển `.php` | `fastcgi_pass unix:...` (dòng 61) | `SetHandler "proxy:unix:{{.Socket}}\|fcgi://localhost"` (dòng 66) | `80: SetHandler "proxy:unix:/run/php/tpan-a_example_com_7653e4-8.3.sock\|fcgi://localhost"` |
| tên lạ | `default_server`, trả 444 | vhost đầu tiên theo tên file, `000-tpan-default.conf`, `Require all denied`: trả 403 | `KQ N13 DAT tên lạ: Apache trả 403` |

Pool, user, `open_basedir` giống hệt, vì cả hai stack dùng chung PHP-FPM. Số dòng `SetHandler` trên máy (80) khác template (66) vì khối theo chế độ `.htaccess` chèn thêm dòng ở giữa.

Khác biệt lớn nhất không nằm trong bảng: Apache đọc thêm file `.htaccess` trong thư mục của site, tức là site tự ghi được một phần cấu hình web server. Ba trong chín lỗ ở phần B (L3, L4, L9) sinh ra từ đó.

### 3.7 `tpan route explain`: từ lệnh tới tám chặng

**`cmd/tpan/route.go`, hàm `routeExplainCmd`.** Kiểm tên miền, đòi quyền root (vì `nginx -T` đọc cả file chỉ root đọc được, và file pool nằm trong thư mục của root), mở DB kiểu chỉ xem (chưa có DB thì không tạo), gọi `s.RouteExplain(ctx, d)`, in bằng `printRoute`. Cuối cùng `routeFails(tr) > 0` thì thoát mã 1, giống `tpan health`. Cảnh báo (DNS) và "chưa rõ" không tính là lỗi.

**`internal/core/route_m14.go`, hàm `Sites.RouteExplain`.** Phần lõi:

```go
	dns := checks.Soft(checks.DNSPointsHere(l)).Run(ctx, s.Sys)
	tr.Hops = append(tr.Hops, routeFromCheck(HopDNS, routeDNSValue(dns), dns))

	port := "80"
	if st.SSL {
		port = "443" // site có SSL: cổng 80 chỉ chuyển hướng, site chạy ở 443
	}
	vh, vhop := routeVhostHop(ctx, s.Sys, cfg.Web, l, port)
	// ... (site tạm khoá dừng ở đây)
	rootHop, sockHop, socket := routeTargets(s.Sys, cfg.Web, vh, l, port, l.SocketFor(php))
	tr.Hops = append(tr.Hops, rootHop, sockHop)
	tr.Hops = append(tr.Hops, routePoolHops(s.Sys, socket, l, php)...)
```

Ý quan trọng nhất của cả file: **mỗi chặng lấy đầu vào từ điều chặng trước tìm thấy trên máy, không từ điều TPAN nghĩ nó đã ghi.**

- `routeVhostHop` chạy `nginx -T` (in toàn bộ cấu hình nginx đang nạp, trước mỗi file có dòng `# configuration file <đường dẫn>:`) hoặc `apache2ctl -S`. Nó lấy chỗ khai **đầu tiên** của tên miền trên cổng đang xét, đúng thứ tự web server nạp, rồi mới so file đó với vhost của site. Là file lạ thì chặng `[lỗi]` và nêu file.
- `routeTargets` đọc chính file vừa tìm được (không phải file của TPAN) để lấy `root` và `fastcgi_pass`, rồi so với `l.Public` và socket mong đợi.
- `routePoolHops` duyệt mọi `/etc/php/*/fpm/pool.d/*.conf`, tìm pool nào có `listen` bằng đúng socket đó, rồi đọc `user` và `php_admin_value[open_basedir]` của chính pool ấy.
- Chặng cuối gọi thật vào site bằng `curl --resolve` (dùng lại phép kiểm `checks.HTTPSite` của M2).

Chặng nào không có đầu vào (chặng trước không ra gì) thì `routeBlocked` trả `[chưa rõ] Chưa lần được`, như ba chặng pool, user, `open_basedir` ở mục 2.4.

Vì sao viết vậy: nếu chỉ kiểm "file của TPAN còn đúng không" thì ở tình huống L8 mọi thứ vẫn xanh, vì file của TPAN đúng thật, chỉ là có file khác đứng trước nó. Lần từ cấu hình đang chạy thì file lạ hiện ra ngay ở chặng vhost.

Cùng kết quả này có ở API `GET /api/v1/sites/{domain}/route` (luật `ownerSite` trong `internal/server/routes.go`: chủ của site hoặc admin) và nút "Request tới site này đi đường nào" trên trang site (`web/src/screens/site/RouteExplain.tsx`, chưa ai xem bằng mắt).

### 3.8 Vhost trùng tên (L8) và vhost mặc định: hai phép kiểm định tuyến

Khi ai đó thêm tay một vhost trùng `server_name` của site: nginx nạp các file trong `sites-enabled` theo thứ tự tên; khối khai tên đó trước thì thắng, khối sau bị bỏ qua kèm đúng một dòng `[warn] conflicting server name ... ignored`. `nginx -t` vẫn thoát mã 0. Nếu file lạ có tên xếp trước `tpan-...` (như `aaa-e2e-la.conf`), tên miền của site trả trang lạ. Apache cũng chọn vhost được nạp trước (mục 2.4).

**`internal/checks/m14.go`, hàm `ServerNameUnique(web, sites)`.** Chạy `nginx -T` hoặc `apache2ctl -S`, tách ra mọi bộ (tên, cổng, file, số dòng, khối) bằng `nginxDecls` hoặc `apacheDecls`, rồi với từng site gọi `findNameConflict`:

```go
	for i, d := range mine {
		if containsStr(ownFiles, d.file) {
			continue
		}
		c := nameConflict{bad: d, foreign: true}
		// ... tìm chỗ khai còn lại để nêu trong lời báo
		return c, true
	}
	for i, d := range mine {
		for j := range mine[:i] {
			if mine[j].port == d.port && mine[j].block != d.block {
				return nameConflict{bad: d, other: &mine[j]}, true
			}
		}
	}
```

`mine` là mọi chỗ khai tên miền của một site. Vòng lặp đầu bắt trường hợp tên nằm trong một file không phải vhost của site. Vòng lặp sau bắt hai khối khác nhau cùng khai tên trên một cổng. Có là `[lỗi]`, lời báo nêu hai file và số dòng. Lệnh "sửa tay" gợi ý: `grep -nE 'listen|server_name' <hai file>`, `ls -l <file lạ>`, `mv <file lạ> /root/`, `nginx -t`, `systemctl reload nginx`. Gợi ý chuyển file ra ngoài chứ không đổi tên tại chỗ, vì `sites-enabled` nạp mọi tên file. Phép kiểm này không có sửa tự động: TPAN không xoá file nó không ghi.

Ba chỗ làm gọn có đánh dấu `ponytail:` trong mã: chỉ so cổng, không so địa chỉ nghe; không lần theo `include` bên trong khối `server`; tên wildcard và regex không tính là trùng. Vhost của TPAN không dùng cả ba.

**Hàm `DefaultRejects(web, ssl)`.** Sinh một tên ngẫu nhiên `tpan-<8 ký tự hex>.invalid` (`.invalid` là đuôi dành riêng, không bao giờ là tên miền thật), gọi vào `127.0.0.1` với tên đó. Đạt khi nginx đóng kết nối (curl mã 52) hoặc Apache trả 403. Có site bật SSL thì thử thêm cổng 443: nginx phải từ chối bắt tay (curl mã 35), Apache phải 403. Không đạt nghĩa là có một vhost đang hứng mọi tên miền.

Hai phép này chỉ health chạy (`Stack.isolationChecks` trong `internal/core/health.go`), vì chúng cần danh sách mọi site trên máy.

### Phần B. Chống local attack

### 3.9 Câu trả lời để nói bằng lời, và lớp nền có từ M2

> Nền là mỗi site một user Linux: file của site thuộc user đó, PHP của site chạy dưới user đó, nên nhân Linux tự từ chối khi PHP của site A mở file của site B. M14 đi tìm những đường vòng qua nền ấy và thấy chín đường, đều qua những thứ các site vẫn dùng chung: bộ nhớ cache mã của PHP, web server chạy dưới một user đọc được mọi site, file `.htaccess` do site tự ghi, thư mục log, bảng tiến trình, bảng tên miền. Mỗi đường được đóng bằng một hai dòng cấu hình. Mỗi dòng có một phép kiểm trong health canh giữ. Và mỗi đường có một phép thử tấn công chạy lại trên máy ảo: trước bản vá thì lộ, sau thì chặn.

| Riêng từng site (từ M2) | Vẫn dùng chung, nên M14 phải vá |
|---|---|
| user Linux `tp_<slug>`; thư mục `2750`, file `640`, `wp-config.php` `600` | tiến trình chủ PHP-FPM và vùng nhớ OPcache của nó (L1, L2) |
| pool PHP-FPM, socket `0660` của `www-data` | web server chạy dưới `www-data`, đọc được mọi site qua nhóm (L5, L9) |
| DB và user DB chỉ có quyền trên DB đó | cấu hình web server; trên Apache site tự ghi được một phần qua `.htaccess` (L3, L4, L9) |
| `open_basedir`, thư mục `tmp/` riêng | thư mục log (L6), `/proc` (L7), bảng tên miền của web server (L8) |

Một câu trong nghiên cứu đáng nhớ: phép kiểm cô lập cũ (`test -r wp-config.php` dưới user A) vẫn **đạt** trong lúc L1 đang mở. Quyền file đúng chưa có nghĩa là hai site đã cô lập.

### 3.10 Chín lỗ, từng cái một

Mỗi lỗ theo cùng một mẫu: kẻ tấn công làm gì, vì sao trước đây lọt, dòng cấu hình nào chặn, bằng chứng trước và sau. "nang-cap" là `M14-nang-cap.log`.

**L1. OPcache dùng chung (cả hai stack).**

- Làm gì: PHP của A chạy `include "/var/www/b.example.com/public/wp-config.php";` ngay sau khi B có khách, rồi đọc hằng `DB_PASSWORD`. Có mật khẩu DB là vào được DB của B.
- Vì sao lọt: một bản PHP-FPM là một dịch vụ, có một tiến trình chủ. Tiến trình chủ tạo vùng nhớ chung của OPcache rồi mới sinh tiến trình con cho từng pool. Con nào cũng dùng chung vùng nhớ đó, dù nó chạy dưới user của pool mình. OPcache giữ mã đã biên dịch của mọi file PHP, tra theo đường dẫn. Khi PHP của A `include` một đường dẫn đang có trong cache, OPcache đưa luôn mã đã biên dịch từ bộ nhớ, không mở file trên đĩa. Cả hai hàng rào kia đều đứng ở bước mở file: nhân Linux kiểm quyền `600` lúc mở file, PHP kiểm `open_basedir` lúc nó mở file. Không ai mở file thì không ai kiểm. Vì vậy `open_basedir` của A còn nguyên và `test -r` dưới user A vẫn bị từ chối, mà mật khẩu vẫn lộ. `t2-opcache.ket-qua.txt` ghi đúng cảnh đó: `open_basedir=/var/www/a.example.com/ -> include wp-config.php cua B: LAY DUOC hang DB_PASSWORD`.
- Vì sao phải "ngay sau khi B có khách": file phải đang nằm trong cache. `t2` in `opcache.revalidate_freq => 2`; mình hiểu là trong 2 giây đó OPcache không nhìn lại file trên đĩa. Phần này mình suy từ tên tham số và cách script thử viết (gọi trang chủ của B rồi gọi ngay file của A, ba lần), mốc này không đo riêng.
- Dòng chặn: `opcache.validate_permission=1` trong `/etc/php/<bản>/fpm/conf.d/99-tpan.ini`. Trước khi dùng một file đã cache, OPcache hỏi user đang chạy có quyền đọc file đó không.
- Bằng chứng: trước `KQ L1 LO ... lấy được hằng DB_PASSWORD` (nang-cap dòng 271 và 1773); sau `KQ L1 CHAN ... không lấy được (3 lần thử)` (`M14.log` dòng 433 và 2697).

**L2. Hàm quản lý của OPcache (cả hai stack).**

- Làm gì: gọi `opcache_get_status(true)` từ A để lấy danh sách mọi file đang cache; gọi `opcache_reset()` để xoá cache của mọi site.
- Vì sao lọt: cùng gốc với L1. Một vùng nhớ cho mọi pool, và các hàm `opcache_*` không phân biệt pool nào đang hỏi.
- Dòng chặn: `opcache.restrict_api=/usr/local/lib/tpan/opcache-api/` trong cùng file ini. Chỉ script nằm dưới đường dẫn này mới gọi được các hàm đó. Đường dẫn cố ý không tồn tại (hằng `OPcacheAPIDir` trong `internal/templates/m14.go`), nên không site nào gọi được.
- Bằng chứng: trước `GOI_DUOC thay 532 file cua B` (nang-cap dòng 272); sau `opcache_get_status từ A: bị chặn` (`M14.log` dòng 434). Bộ phép thử chỉ gọi `opcache_get_status`; `opcache_reset` sau bản vá thì chưa có phép thử riêng (chưa chạy).

**L3. `.htaccess` tự nới `open_basedir` (Apache).**

- Làm gì: A thêm vào `.htaccess` của mình một dòng `SetEnv PHP_ADMIN_VALUE "open_basedir=/"`.
- Vì sao lọt: Apache gửi request cho PHP-FPM kèm một danh sách biến. PHP-FPM coi hai biến `PHP_VALUE` và `PHP_ADMIN_VALUE` là cài đặt PHP cho request đó, và cho chúng đè cả `php_admin_value` của pool (nghiên cứu ghi: PHP coi đây là đúng thiết kế, bug #79417). `SetEnv` trong `.htaccess` đặt được biến, nên site tự gỡ được hàng rào của chính nó. `t1-apache-co-lap.ket-qua.txt` mục 6: trước `open_basedir=/var/www/a.example.com/ | doc /etc/hostname: KHONG`, sau một dòng `SetEnv` thành `open_basedir=/ | doc /etc/hostname: CO`.
- Dòng chặn: `site-apache.conf.tmpl` dòng 59 và 60, ở cấp `VirtualHost`:

  ```apache
  ProxyFCGISetEnvIf "true" !PHP_VALUE
  ProxyFCGISetEnvIf "true" !PHP_ADMIN_VALUE
  ```

  Dấu `!` là gỡ biến. Hai biến bị gỡ ngay trước khi request sang PHP-FPM.
- Bằng chứng: trước `open_basedir=/|ngoai=CO` (nang-cap dòng 1775); sau `open_basedir giữ nguyên` (`M14.log` dòng 2699).
- Trên nginx lỗ này không có: site không có cách nào bảo nginx gửi thêm biến cho PHP.

**L4. `.htaccess` gửi request sang pool PHP của B (Apache).**

- Làm gì: A viết vào `.htaccess` (rút gọn từ `scripts/e2e-m14/tan-cong.sh` dòng 113 tới 117):

  ```apache
  <Files "qua.php">
      SetHandler "proxy:unix:<socket của B>|fcgi://localhost"
      ProxyFCGISetEnvIf "true" SCRIPT_FILENAME "/var/www/b.example.com/public/index.php"
      ProxyFCGISetEnvIf "true" PHP_ADMIN_VALUE "open_basedir=/ \n auto_prepend_file=/tmp/ma-cua-a.php"
  </Files>
  ```

- Vì sao lọt: user A không ghi được vào socket của B (`0660`, `www-data`). Nhưng Apache thì ghi được, và `.htaccess` là chỗ A ra lệnh cho Apache. `SetHandler` bảo Apache gửi request sang socket của B. `SCRIPT_FILENAME` trỏ vào một file có thật của B để pool của B chịu chạy. `auto_prepend_file` bảo PHP của B nạp trước một file do A viết, đặt ở `/tmp`. Kết quả: mã của A chạy dưới user của B và đọc được `wp-config.php` của B. `t1` mục 7b còn thấy cấu hình bơm vào nằm lại trong tiến trình PHP của B: khách vào trang chủ của B cũng chạy mã của A, cho tới khi tiến trình đó thoát.
- Dòng chặn: chế độ `an-toan`. Dòng `AllowOverrideList` của vhost không có `SetHandler`, `AddHandler`, `RemoveHandler`, `Action`, `ProxyFCGISetEnvIf`, `ProxyFCGIBackendType`. `.htaccess` dùng chỉ thị ngoài danh sách thì Apache trả 500 cho site đó: A tự hỏng, B không sao.
- Lối vòng: `RewriteRule` có cờ `[H=...]` cũng đổi được nơi nhận request, mà `RewriteRule` phải nằm trong danh sách vì WordPress cần. Bộ phép thử thử cả lối này: mã của A không chạy dưới user B. Nghiên cứu ghi lối này chỉ nhận về `File not found` từ pool của B.
- Bằng chứng: trước `MA_CUA_A chay duoi user=tp_b_example_com_cd59d5|doc wp-config cua B=CO` (nang-cap dòng 1776); sau `HTTP 500, mã của A không chạy dưới user B (cả lối RewriteRule [H=])` (`M14.log` dòng 2700), và `KQ N9 DAT trang chủ của B không dính mã của A`.
- Ở `day-du`, hai dòng gỡ biến của L3 vẫn có trong vhost mà L4 vẫn lộ (mục 2.6). `.htaccess` của A dùng chính `ProxyFCGISetEnvIf` để đặt lại biến. Vì vậy L4 chỉ đóng khi `.htaccess` không được dùng chỉ thị đó.

**L5. Symlink qua nginx.**

- Làm gì: dưới user A, `ln -s /var/www/b.example.com/public/<file> /var/www/a.example.com/public/lay.txt`, rồi gọi `http://a.example.com/lay.txt`.
- Vì sao lọt: tạo symlink không cần quyền đọc file đích. Người mở file là nginx, chạy dưới `www-data`, mà `www-data` là nhóm của thư mục mọi site. File `640` của B vì thế được trả về dưới tên miền của A. Mã nguồn PHP của B cũng lộ nếu symlink mang đuôi `.txt`: nginx coi là file tĩnh, trả nguyên văn. `wp-config.php` (`600`) không lộ theo đường này, vì `www-data` không đọc được nó: `t4-nginx-co-lap.ket-qua.txt` đo `HTTP 403`.
- Dòng chặn: `site-nginx.conf.tmpl` dòng 17 (và khối 443 của `site-nginx-ssl.conf.tmpl`):

  ```nginx
  disable_symlinks if_not_owner from=$document_root;
  ```

  nginx chỉ đi theo symlink khi symlink và file đích cùng một chủ. `from=$document_root` giới hạn việc kiểm vào phần đường dẫn nằm dưới thư mục gốc của site.
- Bằng chứng: trước `HTTP 200` cho cả hai file (nang-cap dòng 276); sau `HTTP 404; ... HTTP 404` (`M14.log` dòng 438), qua cổng 443 cũng `http=404 lộ=0` (dòng 2035). Symlink của A trỏ vào file của chính A vẫn `HTTP 200` (`KQ N5`).
- Vì sao ra 404 mà không phải 403: `t4` cho thấy thân trả về là trang HTML của WordPress. nginx coi symlink đó như không có, `try_files` ở dòng 49 của template chuyển tiếp cho `/index.php`, WordPress trả 404. Trong lab không có WordPress (`symlink-lab.ket-qua.txt`), cùng dòng cấu hình cho `HTTP 403` và error log của nginx ghi `openat() ... failed (40: Too many levels of symbolic links)`.

**L6. Đọc log của site khác (cả hai stack).**

- Làm gì: dưới user A, đọc `/var/log/tpan/sites/b.example.com/access.log`.
- Vì sao lọt: trước M14 thư mục log là `root:root 0755` và web server tự tạo `access.log`, `error.log` với quyền `0644`. Ai trên máy cũng đọc được, trong đó có IP của khách và URL kèm tham số.
- Chặn: thư mục `root:<nhóm của site> 0750`, hai file `root:<nhóm của site> 0640`, tạo sẵn lúc tạo site (`steps.SiteLogs`, mục 3.12).
- Bằng chứng: trước `user A đọc được access.log của B (drwxr-xr-x root:root )` (nang-cap dòng 279 và 1783); sau `user A không đọc được ... (drwxr-x--- root:tp_b_example_com_cd59d5)` (`M14.log` dòng 441), và B vẫn đọc được log của chính mình (`KQ N7`), web server vẫn ghi (`KQ N8`).

**L7. Nhìn thấy tiến trình của user khác (cả hai stack).**

- Làm gì: PHP của A gọi `shell_exec("ps ...")`.
- Vì sao lọt: `open_basedir` là hàng rào bên trong PHP. Một lệnh shell do PHP gọi là tiến trình khác, không còn bị nó quản. `ps` đọc `/proc`, và mặc định ai cũng xem được tiến trình cùng dòng lệnh của mọi user (tên site, đường dẫn, đôi khi cả tham số nhạy cảm).
- Dòng chặn: drop-in systemd `/etc/systemd/system/php<bản>-fpm.service.d/tpan.conf` với `ProtectProc=invisible`. Dịch vụ PHP-FPM được gắn một `/proc` riêng, trong đó một tiến trình chỉ thấy tiến trình của chính user mình.
- Bằng chứng: trước `thấy 118 tiến trình của user khác` (nang-cap dòng 282); sau `thấy 0 tiến trình của user khác` (`M14.log` dòng 444).
- Giới hạn: chỉ chặn nhìn tiến trình. Lệnh shell vẫn đọc được file ngoài thư mục site mà quyền Linux cho phép (`t4` mục 5: `doc /etc/hostname bang shell_exec: CO`, `ls /var/www: a.example.com b.example.com html`). File của B thì vẫn không đọc được, vì quyền Linux giữ. Đó là lý do health coi thiếu dòng này là cảnh báo, không phải lỗi.

**L8. Vhost trùng tên miền.**

- Ai làm: không phải site A (ghi vào `/etc/nginx/sites-enabled` cần root). Là một người quản trị khác, hoặc một công cụ khác trên máy.
- Vì sao trước đây lọt: `nginx -t` vẫn mã 0, health chỉ báo triệu chứng "trả 200 nhưng không phải WordPress".
- Chặn: ở đây là phát hiện, bằng phép kiểm `web.server_name.unique` và chặng vhost của `tpan route explain` (mục 2.4, 3.8). Gỡ file là việc của người vận hành.
- Bằng chứng: trước `tpan health không chỉ ra file trùng` (nang-cap dòng 283 và 1787); sau `tpan health chỉ ra file aaa-trung-....conf (4 dòng)` (`M14.log` dòng 445 và 2711).

**L9. `.htaccess` đổi `Options` rồi dùng symlink (Apache).**

- Làm gì: A ghi `Options FollowSymLinks` (không có dấu `+`) hoặc `Options All` vào `.htaccess`, rồi đặt symlink như L5.
- Vì sao lọt: vhost đã có `Options SymLinksIfOwnerMatch` (dòng 30 của template), tương đương `disable_symlinks if_not_owner` bên nginx, nên bình thường symlink sang file của B bị 403. Nhưng `Options` viết không có dấu `+` hay `-` là thay cả bộ tuỳ chọn, bỏ mất `SymLinksIfOwnerMatch`. Với `AllowOverride All`, hoặc với một `AllowOverrideList` có tên `Options`, `.htaccess` được phép viết dòng đó.
- Dòng chặn: bỏ `Options` khỏi `AllowOverrideList` (còn 55 tên) và mở riêng bằng `AllowOverride Options=Indexes,MultiViews,SymLinksIfOwnerMatch`. `.htaccess` vẫn viết được `Options -Indexes`; viết `Options FollowSymLinks` thì Apache báo 500.
- Bằng chứng: trước `[Options FollowSymLinks: HTTP 200] [Options +FollowSymLinks: HTTP 403] [Options All: HTTP 200]` (nang-cap dòng 1779); sau cả ba `HTTP 500` (`M14.log` dòng 2703), và `Options -Indexes` thì trang chủ A vẫn `HTTP 200` (`KQ N10`).
- Lỗ này không có trong nghiên cứu ban đầu; mục 5.2 kể nó được tìm ra thế nào.

### 3.11 Hai file cấp máy: `internal/templates/m14.go` và `internal/steps/harden_m14.go`

**`internal/templates/m14.go`.** Nội dung hai file là hằng trong mã, không phải file template, vì chúng không có tham số nào: không có gì để điền sai. `PHPHardeningINIPath("8.3")` trả `/etc/php/8.3/fpm/conf.d/99-tpan.ini`. Tên bắt đầu bằng `99-` để PHP đọc nó sau `10-opcache.ini` của gói Ubuntu, nên giá trị của TPAN thắng. `PHPDropInPath("8.3")` trả `/etc/systemd/system/php8.3-fpm.service.d/tpan.conf`. Mỗi bản PHP đang cài có một cặp file riêng.

**`internal/steps/harden_m14.go`, `PHPHardeningINI.Apply`** (một Step là một bước của Plan, có `Check`, `Apply`, `Undo`; xem `00-go-can-ban.md` và mục 4.2 của KE-HOACH.md):

```go
	if err := sys.WriteFileAtomic(p, templates.PHPHardeningINI(), hardenMode, system.Root); err != nil {
		return fmt.Errorf("ghi %s: %w", p, err)
	}
	what := "ghi " + p
	if err := runTest(ctx, sys, test, what); err != nil {
		// Dịch vụ chưa nạp gì: chỉ cần trả file.
		return errors.Join(err, prev.restore(sys))
	}
	if err := phpReloadIfActive(ctx, sys, v, reload, what); err != nil {
		// Reload hỏng: trả file rồi reload lần nữa ...
```

Ghi file, chạy `php-fpm<bản> -t` để kiểm cú pháp, rồi reload dịch vụ nếu nó đang chạy. Sai ở đâu thì trả file cũ về. Reload là đủ để PHP-FPM đọc lại file ini (`t7-log-va-reload.ket-qua.txt`: `sau reload: validate_permission=1`).

**`PHPProtectProc.Apply`.** Tạo thư mục drop-in nếu chưa có, ghi file, `systemctl daemon-reload` (để systemd đọc file mới), `php-fpm<bản> -t`, rồi `systemctl restart`. Hai chỗ có lý do:

- Phải restart, reload không đủ: `/proc` riêng của dịch vụ được dựng lúc dịch vụ khởi động. Restart ngắt request PHP đang chạy, nên bước này mang cờ `Danger: true` và bản xem trước in "NGUY HIỂM".
- Kiểm cú pháp trước khi restart: một PHP-FPM đang chạy với cấu hình đã hỏng trên đĩa (ai đó sửa tay một file pool) vẫn phục vụ bình thường cho tới lúc bị restart. Restart nó là tắt mọi site của bản PHP đó. Kiểm sai thì bước lỗi và dịch vụ không bị đụng tới.

**`PHPHardening(version)`** trả hai bước theo thứ tự ini rồi drop-in. Thứ tự có chủ ý: bước ini chỉ reload, bước drop-in restart, và lần restart đó nạp luôn file ini.

Hai bước này được gọi từ bốn nơi: Plan cài stack (`internal/core/stack.go`), Plan thêm một bản PHP (`internal/core/php.go`), sửa tự động `php.harden` (`internal/core/fix.go`) và Plan harden. Bằng chứng: hai dòng `- Bật kiểm quyền file và khoá API của OPcache ...` và `- Ẩn tiến trình của user khác ...` trong output của `stack install` (`M14.log` dòng 150 tới 153), và PHP 8.4 cài thêm cũng có đủ hai file (dòng 812 tới 833).

### 3.12 Log: `SiteLogs` trong `internal/steps/site_user.go`, và `logrotate-sites.tmpl`

```go
func SiteLogs(l site.Layout) []engine.Step {
	grp := system.Owner{User: "root", Group: l.User}
	own := system.Owner{User: l.User, Group: l.User}
	// ...
	return []engine.Step{
		&Dir{Path: l.LogDir, Perm: 0o750, Owner: grp},
		&OwnedFile{Path: l.AccessLog, Perm: 0o640, Owner: grp, Why: webWhy},
		&OwnedFile{Path: l.ErrorLog, Perm: 0o640, Owner: grp, Why: webWhy},
		&OwnedFile{Path: l.PHPErrorLog, Perm: 0o640, Owner: own},
	}
}
```

Bốn quyết định trong mấy dòng này:

- Nhóm là `l.User`: lúc tạo user của site, Linux tạo kèm một nhóm cùng tên. Site đọc được log của chính nó; site khác không vào được thư mục.
- Chủ là `root`, không phải user của site. logrotate chạy bằng root, đổi tên và tạo file trong thư mục này. Nếu user của site ghi được vào thư mục, nó có thể đặt symlink để lừa root ghi đè một file khác. (Thêm một hệ quả của luật quyền Unix, tài liệu nguồn không ghi: chủ thư mục xoá và đổi tên được file trong đó, tức là site bị chiếm xoá được dấu vết của chính nó.)
- Hai file được tạo sẵn, rỗng, trước khi vhost được ghi. Để web server tự tạo thì file ra `root:root 0644`.
- `OwnedFile` chỉ sửa quyền và chủ, không đụng nội dung, nên chạy trên site đang có log thì không mất dòng nào. `php-error.log` vẫn thuộc user của site, vì PHP (chạy dưới user đó) phải ghi được.

Web server vẫn ghi được vào file `0640` của root vì tiến trình chính của nó chạy bằng root: tiến trình chính mở file log, các worker (chạy dưới `www-data`) thừa kế file đã mở. Điều đó kéo theo một thay đổi trong `logrotate-sites.tmpl`, nhánh nginx (dòng 43):

```
        [ ! -f /run/nginx.pid ] || kill -HUP "$(cat /run/nginx.pid)"
```

Trước M14 dòng này gửi `USR1`. Mục 4.8 và 5.4 giải thích vì sao phải đổi sang `HUP`.

### 3.13 `.htaccess`: gói `internal/htaccess`, template con, `PrepareHtaccess`

`.htaccess` là một file cấu hình của Apache nằm ngay trong thư mục mã nguồn, do chính site ghi (WordPress ghi luật permalink vào đó, plugin cache và bảo mật ghi thêm). Mỗi request, Apache đọc các `.htaccess` trên đường đi tới file và áp các chỉ thị trong đó. Nói cách khác: với `AllowOverride All`, ai ghi được vào thư mục site thì sửa được cấu hình web server cho site đó. M14 thêm một cài đặt cho mỗi site: chế độ `.htaccess`, lưu ở cột `sites.htaccess`.

| Chế độ | Dòng trong vhost | `.htaccess` của site làm được gì | Lỗ còn mở | Health (`web.htaccess`) |
|---|---|---|---|---|
| `an-toan` (mặc định cho site mới) | `AllowOverride Options=Indexes,MultiViews,SymLinksIfOwnerMatch` và một dòng `AllowOverrideList` 55 tên | rewrite, phân quyền, header, cache, kiểu file, chuyển hướng; `Options` chỉ với ba tuỳ chọn | không | lỗi khi có chỉ thị ngoài danh sách, nêu file và dòng |
| `tat` | `AllowOverride None` và `FallbackResource /index.php` | không gì: Apache không đọc file đó; permalink do vhost lo | không | đạt |
| `day-du` | `AllowOverride All`, như trước M14 | mọi chỉ thị | L4, L9 | luôn cảnh báo |

**`internal/htaccess/htaccess.go`.** Gói này không gọi gói nào khác của TPAN và không chạm máy. Nó giữ `Allowed` (55 tên chỉ thị), `AllowedOptions` (ba tuỳ chọn), và hàm `Scan`. Ba nơi dùng chung đúng các hằng này: template (sinh dòng `AllowOverrideList` qua `templates.withHtaccess`), phép kiểm `web.hardened` (so dòng trong vhost), và phép quét. Nhờ vậy ba nơi không lệch nhau được.

Vì sao danh sách là 55 tên chọn tay: Apache chỉ nhận tối đa 64 tên trên một dòng `AllowOverrideList`. Phần thừa bị bỏ lặng lẽ, không cảnh báo, và dòng `AllowOverrideList` thứ hai thay dòng thứ nhất chứ không cộng dồn (mục 5.1). Hằng `MaxNames = 64` và hai test trong `htaccess_test.go` giữ điều này: `TestAllowedVuaMotDong` (đúng 55 tên, không quá 64, không lặp) và `TestAllowedKhongCoChiThiCam` (không có sáu chỉ thị của L4, không có `Options`; `AllowedOptions` không có `FollowSymLinks`, `ExecCGI`, `Includes`, `All`).

Vì sao `Options` đi đường riêng: `AllowOverrideList` cho phép theo tên chỉ thị, không nhìn tham số. Cho tên `Options` vào là cho luôn `Options FollowSymLinks` (L9). Còn bỏ hẳn `Options` thì `Options -Indexes`, dòng mà plugin hay ghi, làm site báo 500 (`t9` mục 3). Apache có đúng một cú pháp giới hạn theo từng tuỳ chọn: `AllowOverride Options=<danh sách>`. Vì vậy vhost có hai dòng: `AllowOverride` lo riêng `Options`, `AllowOverrideList` lo mọi chỉ thị còn lại.

Phần của `Scan` xử một dòng:

```go
		if strings.EqualFold(name, "Options") {
			for _, arg := range fields[1:] {
				opt := strings.TrimLeft(arg, "+-")
				if !strings.EqualFold(opt, "None") && !containsFold(AllowedOptions, opt) {
					out = append(out, Finding{File: file, Line: start, Directive: name + " " + opt})
				}
			}
			continue
		}
		if !containsFold(Allowed, name) {
			out = append(out, Finding{File: file, Line: start, Directive: name})
		}
```

`name` là từ đầu tiên của dòng (tên chỉ thị), so không phân biệt hoa thường. `Scan` là lời báo trước, không phải hàng rào: hàng rào là Apache, nó tự từ chối chỉ thị ngoài danh sách. `Scan` chỉ nói trước dòng nào sẽ làm site báo 500.

**`internal/templates/files/site-apache-override.tmpl`.** Một template con cho cả vhost cổng 80 và 443, ba nhánh theo `.Htaccess`. Nhánh cuối (`else`) là `an-toan`: nếu một giá trị lạ lọt tới template, nó rơi vào bản chặt chứ không vào `AllowOverride All`.

**`internal/checks/m14.go`, `ScanHtaccess`.** Tìm mọi `.htaccess` dưới `public` bằng `find`, đọc từng file bằng `head -c 262144`, cả hai chạy **dưới user của site** (`AsUser`, tức `runuser -u <user> --`), không bằng root. Lý do: user của site đặt được một symlink tên `.htaccess` trỏ đi bất cứ đâu, mà root thì đọc được mọi thứ. Bỏ qua `wp-content/uploads` vì vhost đặt `AllowOverride None` ở đó. Giới hạn 200 file, 256 KiB mỗi file, 10 giây mỗi lần đọc (symlink trỏ vào một FIFO sẽ làm `head` chờ mãi và treo cả vòng health).

**`internal/core/harden_m14.go`, `Sites.PrepareHtaccess`.** Đây là `tpan site set <domain> --htaccess ...` (lệnh ở `cmd/tpan/site.go`, hàm `siteSetCmd`). Thứ tự:

1. Từ chối sớm: chế độ không hợp lệ; site không active hoặc đang tạm khoá; stack không phải Apache (`htaccess_not_apache`: trên nginx cài đặt này chưa có tác dụng); đã ở đúng chế độ đó (`htaccess_same`).
2. Sang `an-toan` thì quét trước khi dựng Plan. Có chỉ thị ngoài danh sách: từ chối `htaccess_blocked`, in từng dòng `<file>:<dòng>: <chỉ thị>` (tối đa 50 dòng) và hai lối ra. Chú thích trong mã: "Nói trước rẻ hơn hoàn tác."
3. Plan: ghi lại vhost theo chế độ mới qua `siteVhost` (chỗ duy nhất chọn loại vhost, có từ M13), rồi một bước `midCheck` gọi thử site cho tới khi hai lần liên tiếp trả 200. Apache nạp vhost mới trong nền sau khi lệnh reload đã trả về; site không trả 200 thì job dừng và hoàn tác về chế độ cũ.
4. Job thành công mới ghi chế độ mới vào bảng `sites` (`saveHtaccess`).

`day-du` chỉ admin đặt được: luật đó nằm ở server (API), CLI chạy bằng root nên coi là admin.

### 3.14 Các phép kiểm: `internal/checks/m14.go` và `Isolated` trong `internal/checks/site.go`

| ID trong health | Hàm | Hỏi máy điều gì | Sai là | Sửa tự động |
|---|---|---|---|---|
| `php.opcache:<bản>` | `OPcacheHardened` | file ini đúng nội dung, **và** `php-fpm<bản> -i` in `validate_permission` là `On`, `restrict_api` đúng đường dẫn | lỗi | `php.harden` |
| `php.protectproc:<bản>` | `PHPProtectProc`, `ProcHidden` | `systemctl show ... -p ProtectProc` là `invisible`, **và** `/proc/<MainPID>/mountinfo` có `hidepid=invisible` | cảnh báo | `php.harden` |
| `log.perm:<domain>` | `LogPerms` | thư mục log `root:<nhóm> 0750`, hai file `0640` | lỗi | `log.perm` |
| `web.hardened:<domain>` | `VhostHardened` | vhost có đủ dòng chặn theo stack và theo chế độ `.htaccess` | lỗi | `web.rewrite` (ghi lại vhost của riêng site đó) |
| `web.htaccess:<domain>` | `HtaccessDirectives` | `an-toan`: quét; `day-du`: luôn cảnh báo; `tat`: đạt | lỗi hoặc cảnh báo | không |
| `web.server_name.unique` | `ServerNameUnique` | mỗi tên miền chỉ ở vhost của site đó (3.8) | lỗi | không |
| `web.default.rejects` | `DefaultRejects` | tên lạ bị từ chối (3.8) | lỗi | không |
| `isolation:<A>:<B>` | `Isolated` | năm phép thử dưới user của A | lỗi | không |

Mấy chỗ đáng giải thích:

- `OPcacheHardened` cần hai bằng chứng vì file đúng chưa chắc có hiệu lực: một file ini khác đọc sau (tên xếp sau `99-tpan.ini`) có thể đặt lại giá trị. `php-fpm<bản> -i` in giá trị sau khi PHP gộp mọi file ini.
- `PHPProtectProc` hỏi systemd chứ không đọc file drop-in, vì systemd trả kết quả sau khi gộp mọi drop-in. Rồi `ProcHidden` hỏi thêm chính tiến trình đang chạy; mục 5.5 kể vì sao.
- `VhostHardened` tồn tại vì phép kiểm lệch (M4) không làm được việc này. Lệch so file với bản TPAN đã ghi, nên một vhost do bản TPAN cũ ghi vẫn "không lệch" dù thiếu dòng chặn. `VhostHardened` so từng dòng không phải chú thích với chính các hằng mà template dùng (`templates.NginxSymlinkGuard`, `templates.ApacheFCGIGuards`, `htaccess.List()`).
- `Isolated` trước M14 chỉ có một phép thử `test -r wp-config.php`. Giờ có năm (output ở mục 2.2). Lõi của nó:

```go
	probeCmd := func(p isoProbe) system.Cmd {
		return system.Cmd{Name: "test", Args: []string{p.flag, p.target}, AsUser: from.User}
	}
	// ...
		switch {
		case res.ExitCode == 0:
			leaked = append(leaked, p)
		case res.ExitCode != 1 || strings.TrimSpace(ev.Output) != "":
			if !unclear {
				unclear, unclearCode = true, res.ExitCode
			}
		}
```

Mã 0 là lọt. Mã 1 chỉ được tin khi stderr rỗng, vì `runuser` cũng thoát mã 1 khi user không tồn tại. Và đích phải có thật (TPAN `stat` trước): `test` cũng trả 1 khi không có gì để thử, lúc đó "bị từ chối" không chứng minh được gì. Socket của site đích được tìm bằng cách đọc `/run/php`, vì tên socket mang số bản PHP.

`Isolated` chỉ đo quyền. L1 không phải thứ `test -r` nhìn thấy, nên OPcache phải có phép kiểm riêng.

### 3.15 Ghép vào health và sửa tự động: `internal/core/site.go`, `health.go`, `fix.go`

**`siteHardeningChecks` trong `internal/core/site.go`:**

```go
func siteHardeningChecks(web WebServer, l site.Layout, mode site.HtaccessMode) []checks.Check {
	cs := []checks.Check{
		checks.LogPerms(l),
		checks.VhostHardened(string(web), l, mode),
	}
	if web == Apache {
		cs = append(cs, checks.HtaccessDirectives(l, mode))
	}
	return cs
}
```

Hàm `siteChecks` ngay trên nó nối kết quả này vào bộ phép kiểm của site, cùng `Isolated` hai chiều với từng site còn lại. Bộ đó dùng cho **hai việc**: health, và kiểm chứng sau mọi Plan chạm vào site. Nguyên tắc có từ M4: "lúc tạo đã đúng" và "bây giờ còn đúng" là một định nghĩa.

Hệ quả cho máy nâng từ bản cũ, cũng là câu người review dễ hỏi: vì sao thao tác trên site cũ bị hoàn tác? Vì kiểm chứng của Plan đó giờ có `log.perm`, `web.hardened` và phép thử log của `Isolated`. Site cũ chưa qua harden thì các phép này lỗi; kiểm chứng lỗi thì engine hoàn tác (mục 2.7). Đây là chủ ý: thà từ chối một thao tác còn hơn báo "thành công" trên một cặp site chưa cô lập. Đổi lại, lời nhắn của từng phép kiểm chỉ thẳng sang `sudo tpan security harden`, và đó là việc đầu tiên phải làm sau khi cập nhật.

**`Stack.isolationChecks` trong `internal/core/health.go`** thêm các phép cấp máy mà chỉ health chạy: `php.opcache` và `php.protectproc` của các bản PHP ngoài bản mặc định (bản mặc định đã nằm trong kiểm chứng của cài stack), `web.server_name.unique`, `web.default.rejects`.

**`AutoFixName` trong `health.go`** nối phép kiểm với cách sửa:

```go
	case "php.opcache":
		return FixPHPHarden
	case "log.perm":
		return FixLogPerms
	case "web.hardened":
		return FixWebRewrite
```

Thêm một ngoại lệ ở nhánh cảnh báo: `php.protectproc` là cảnh báo duy nhất có sửa tự động (đặc tả yêu cầu).

**`Fixes.plan` trong `internal/core/fix.go`** dựng Plan cho từng tên:

- `FixPHPHarden`: `steps.PHPHardening(<bản>)`, kiểm chứng bằng cả `OPcacheHardened` và `PHPProtectProc`. Số bản PHP lấy từ ID của phép kiểm và được kiểm dạng trước khi đi vào đường dẫn.
- `FixLogPerms`: ghi lại file logrotate rồi `steps.SiteLogs`. File logrotate đi cùng vì chỉ đặt quyền log mà để lệnh `USR1` cũ thì sau lần xoay đầu tiên log mới rơi vào `access.log.1`.
- `FixWebRewrite` cho `web.hardened`: `vhostRewrite` ghi lại vhost của riêng site đó, theo chế độ `.htaccess` đang lưu. Có một ngoại lệ so với M4, kể ở mục 5.9.

Trên màn Bảo mật, ma trận cô lập đọc kết quả health đã lưu trong bảng `check_results` (API `GET /api/v1/security/isolation`, chỉ admin), không chạy lại phép kiểm mỗi lần mở màn.

### Phần C. Nâng máy cũ

### 3.16 `tpan security harden`: `cmd/tpan/security.go` tới `core.Security.PrepareHarden`

**`cmd/tpan/security.go`, hàm `securityHardenCmd`.** Đòi root cả khi `--dry-run`, vì dựng Plan phải quét `.htaccess` dưới user của từng site. Gọi `sec.PrepareHarden(ctx)`, in bản xem trước, rồi in thêm hai danh sách bằng `printHardenModes`: site nào chuyển sang `an-toan`, site nào giữ `day-du` kèm từng dòng vướng. Có `--yes` vẫn in hai danh sách đó, vì site giữ `day-du` là việc còn lại cho người vận hành.

**`internal/core/harden_m14.go`, hàm `PrepareHarden`.** Năm nhóm bước, đúng thứ tự này:

1. Với mỗi bản PHP đang cài: `steps.PHPHardening` (file ini rồi drop-in, mục 3.11).
2. File logrotate của stack. Đứng trước nhóm 3: khi thư mục log đã là `0750` thì lần xoay log kế tiếp phải dùng file mới này.
3. Với mỗi site active, kể cả site tạm khoá: `steps.SiteLogs`.
4. Quyết định chế độ `.htaccess`. Nhóm này chỉ đọc, và chạy ngay lúc dựng Plan:

```go
		found, _, err := checks.ScanHtaccess(ctx, s.Sys, l)
		// ...
		if err != nil {
			found = []htaccess.Finding{{File: l.Public, Directive: hardenUnscanned + " " + cleanCtl(err.Error())}}
		}
		if len(found) > 0 {
			h.Kept = append(h.Kept, HtaccessKept{Domain: st.Domain.String(), Findings: found})
			continue
		}
		active[i].Htaccess = site.HtaccessSafe
		h.Switched = append(h.Switched, st.Domain.String())
```

   Site đang `day-du` mà `.htaccess` sạch: chuyển sang `an-toan`. Có chỉ thị ngoài danh sách, hoặc không quét được: giữ `day-du` và ghi vào `Kept`. Một site vướng không chặn bản vá của cả máy. Trên stack nginx vẫn quét và chuyển, để máy sau này đổi sang Apache đã sẵn chế độ `an-toan`.
5. Với mỗi site active: ghi lại vhost qua `siteVhost` theo chế độ vừa quyết, không reload từng cái; cuối cùng một bước kiểm cú pháp và reload web server đúng một lần.

Kiểm chứng: bộ của stack cộng các phép cấp máy; với từng site là `LogPerms`, `VhostHardened`, gọi HTTP, và `Isolated` từng cặp. Không chạy cả bộ `siteChecks`: một site hỏng ở chặng không liên quan (DB, mã nguồn) không được kéo hoàn tác bản vá của cả máy. Job thành công mới ghi chế độ mới của các site trong `Switched` vào bảng `sites`.

**Hai kiểu bước riêng `hardenWrite` và `hardenReload`.** Chúng giải hai bài toán:

- Chạy lại không được reload thừa. `hardenReload.Check` đạt khi không bước ghi vhost nào còn việc, nên lần chạy lại in "0 bước sẽ chạy".
- Hoàn tác phải đúng thứ tự. Engine hoàn tác ngược thứ tự chạy: bước reload trước, rồi mới tới các bước ghi vhost. Nếu reload ngay ở `Undo` của bước reload thì web server nạp lại đúng cấu hình mới, vì file cũ chưa về. Nên việc nạp lại nằm ở `hardenWrite.Undo` của bước ghi vhost được hoàn tác sau cùng (khi bộ đếm `written` về 0).

Bằng chứng cho cả hai ở mục 2.7: chạy lại 0 bước, và lần harden lỗi có chủ ý thì `diff` trước sau rỗng.

### 3.17 Hai kịch bản: `scripts/e2e-m14/tan-cong.sh` và `scripts/e2e-m14.sh`

`tan-cong.sh` dựng lại từ các script nghiên cứu `t1` tới `t9`. Nó tự nhận ra stack (`systemctl is-active --quiet apache2`), tìm user và socket của hai site, đặt file thử mang hậu tố ngẫu nhiên, chạy từng phép dưới user của A (`as_a(){ runuser -u "$UA" -- "$@"; }`) hoặc qua HTTP, và in mỗi phép một dòng `KQ`. Nó không in mật khẩu hay nội dung file của B: mã thử chỉ in tên user và `CO`/`KHONG`. `.htaccess` của A được trả về sau mỗi phép, và `trap don EXIT` xoá mọi file thử kể cả khi script dừng giữa chừng.

`e2e-m14.sh` có bốn phần (đầu file mô tả): A là nginx từ máy sạch, B là Apache từ máy sạch, C là tính năng trên máy của B (ba chế độ `.htaccess`, `route explain`, API), D là nâng từ v0.1.2 trên từng stack. Mỗi khẳng định in `=> ĐẠT` hoặc `=> KHÔNG ĐẠT`. Cuối lần chạy nó tự tìm trong chính file log xem có mật khẩu, cookie hay IP nào lọt không.

## 4. Kiến thức nền

Các lệnh "tự nhìn" dưới đây gõ trên Mac, trong thư mục repo, khi máy ảo đã có stack và hai site. Mình không chạy chúng trong phiên viết file này. Lệnh nào log đã có output thì mình ghi chỗ; lệnh nào chưa thì ghi "chưa chạy".

### 4.1 Virtual host, header `Host`, vhost mặc định

Một web server phục vụ nhiều tên miền trên cùng một IP và cổng bằng cách chia cấu hình thành nhiều "virtual host" (vhost), mỗi vhost khai tên miền của nó. Request nào cũng mang header `Host`; web server so header đó với tên các vhost để chọn một. Không khớp vhost nào thì dùng vhost mặc định. Nếu không tự đặt vhost mặc định, web server lấy vhost đầu tiên làm mặc định, và đó là cách nội dung một site lộ ra dưới tên miền lạ.

- Trong mã: `internal/templates/files/site-nginx.conf.tmpl` dòng 8 (`server_name`), `nginx-default.conf.tmpl` (`default_server`, `return 444`).
- Tự nhìn:

```bash
scripts/vm.sh exec curl -s -o /dev/null -w '%{http_code}\n' -H 'Host: a.example.com' http://127.0.0.1/
scripts/vm.sh exec curl -s -o /dev/null -w '%{http_code}\n' -H 'Host: khong-co.example.com' http://127.0.0.1/
```

  Đúng hai lệnh này chưa chạy. Phép đo cùng kiểu có ở `docs/bang-chung/M14-nghien-cuu/t6-dinh-tuyen.ket-qua.txt` mục 1 (tên của site: 200; tên lạ: 000, tức nginx đóng kết nối) và ở mục 2.5.

### 4.2 PHP-FPM: tiến trình chủ, pool, và vùng nhớ chung của OPcache

PHP-FPM là dịch vụ chạy mã PHP. Mỗi bản PHP (8.3, 8.4) là một dịch vụ riêng, có một tiến trình chủ chạy bằng root; tiến trình chủ đọc mọi file pool rồi sinh tiến trình con cho từng pool, mỗi con chạy dưới user của pool đó. OPcache là bộ nhớ đệm mã PHP đã biên dịch, để không phải đọc và biên dịch lại file `.php` ở mỗi request. Vùng nhớ của nó do tiến trình chủ tạo, nên mọi pool của cùng một bản PHP dùng chung. "Mỗi site một pool" vì thế tách được user, không tách được cache: đó là gốc của L1 và L2.

- Trong mã: `internal/templates/m14.go`, hàm `PHPHardeningINI`; `internal/checks/m14.go`, hàm `OPcacheHardened`.
- Tự nhìn:

```bash
scripts/vm.sh root bash -c "php-fpm8.3 -i | grep -E '^opcache\.(validate_permission|restrict_api) '"
scripts/vm.sh exec ps -o user,pid,ppid,cmd -C php-fpm8.3
```

  Output của lệnh đầu có ở mục 2.2 (`opcache.validate_permission => On => On`). Lệnh sau chưa chạy; nó để bạn thấy một tiến trình chủ của root và các tiến trình con mang user của từng site (site vừa có khách mới có con, vì pool dùng `pm = ondemand`).

### 4.3 `open_basedir` là hàng rào của PHP, không phải của nhân Linux

`open_basedir` là một cài đặt của PHP: trước khi PHP mở một file, nó tự kiểm đường dẫn có nằm dưới thư mục cho phép không. Thứ gì không đi qua bước mở file của PHP thì không bị kiểm: mã lấy thẳng từ OPcache (L1), lệnh shell do PHP gọi (L7). Và cài đặt này đổi được nếu web server gửi biến `PHP_ADMIN_VALUE` (L3). Vì vậy nó là lớp thứ hai; lớp thứ nhất là quyền file của Linux, do nhân kiểm với mọi tiến trình.

- Trong mã: `site-pool.conf.tmpl` dòng 29; chặng `open_basedir` trong `routePoolHops` (`internal/core/route_m14.go`).
- Tự nhìn: `scripts/vm.sh root grep -n open_basedir /etc/php/8.3/fpm/pool.d/tpan-a_example_com_7653e4.conf`. Chưa chạy; dòng đó có trong output của `route explain --evidence` ở mục 2.3 (với bản PHP 8.4).

### 4.4 `.htaccess`, `AllowOverride`, `AllowOverrideList`

`.htaccess` là file cấu hình Apache đặt trong thư mục web, Apache đọc lại ở mỗi request. Hai chỉ thị trong vhost quyết định file đó được viết gì: `AllowOverride` cho phép theo nhóm (`None`, `All`, hoặc từng nhóm như `Options=...`), còn `AllowOverrideList` cho phép theo tên từng chỉ thị. Gặp chỉ thị không được phép, Apache trả 500 cho mọi request dưới thư mục chứa file đó. nginx không có khái niệm tương đương: mọi cấu hình nằm trong `/etc/nginx`, chỉ root ghi được.

- Trong mã: `site-apache-override.tmpl`; `internal/htaccess/htaccess.go` (`Allowed`, `AllowedOptions`).
- Tự nhìn (stack Apache): `scripts/vm.sh root grep -nE 'AllowOverride|FallbackResource' /etc/apache2/sites-available/tpan-a.example.com.conf`. Output cho chế độ `tat` có ở mục 2.6.

### 4.5 Symlink, và ai là người mở file

Symlink là một file chứa đường dẫn tới file khác. Tạo symlink không cần quyền gì với file đích. Quyền chỉ được kiểm khi một tiến trình mở file qua symlink, và kiểm theo user của tiến trình đó. Nên "user A không đọc được file của B" chưa đủ, nếu trên máy có một tiến trình đọc được cả hai và chịu mở file theo lời A. Web server chính là tiến trình đó.

- Trong mã: `site-nginx.conf.tmpl` dòng 17 (`disable_symlinks if_not_owner`); `site-apache.conf.tmpl` dòng 30 (`Options SymLinksIfOwnerMatch`).
- Tự nhìn (stack nginx):

```bash
scripts/vm.sh root runuser -u tp_a_example_com_7653e4 -- ln -s /var/www/b.example.com/public/index.php /var/www/a.example.com/public/thu.txt
scripts/vm.sh exec curl -s -o /dev/null -w '%{http_code}\n' --resolve a.example.com:80:127.0.0.1 http://a.example.com/thu.txt
scripts/vm.sh root rm /var/www/a.example.com/public/thu.txt
```

  Đúng ba lệnh này chưa chạy. Bộ phép thử làm cùng việc ở dòng `KQ L5` (mục 2.1): 404 sau M14, 200 trước M14.

### 4.6 `/proc`, `hidepid`, `ProtectProc` và drop-in của systemd

`/proc` là một hệ file ảo do nhân Linux dựng: mỗi tiến trình một thư mục `/proc/<pid>`, trong đó có dòng lệnh đã gõ. Lệnh `ps` đọc từ đây. Tuỳ chọn gắn `hidepid=invisible` làm một tiến trình chỉ thấy thư mục của các tiến trình cùng user. systemd có chỉ thị `ProtectProc=invisible`: gắn cho riêng một dịch vụ một `/proc` như thế, không đụng `/proc` của cả máy. Drop-in là một file `.conf` đặt trong thư mục `<tên dịch vụ>.service.d/` để thêm cài đặt cho một dịch vụ mà không sửa file của gói; sau khi ghi phải `systemctl daemon-reload`.

- Trong mã: `internal/templates/m14.go`, hàm `PHPDropIn`; `internal/checks/m14.go`, hàm `ProcHidden`.
- Tự nhìn:

```bash
scripts/vm.sh root systemctl show -p ProtectProc php8.3-fpm
scripts/vm.sh root bash -c 'grep " /proc " /proc/$(systemctl show php8.3-fpm -p MainPID --value)/mountinfo'
```

  Lệnh đầu có output ở mục 2.2 (`ProtectProc=invisible`). Lệnh sau chưa chạy; đó là dòng mà `ProcHidden` đọc, và nó tìm chuỗi `hidepid=invisible` trong đó.

### 4.7 Quyền `0750` với chủ `root` và nhóm của site

Mỗi file và thư mục có ba nhóm quyền: cho chủ, cho nhóm, cho người khác. `0750` là: chủ đọc, ghi, đi qua; nhóm đọc và đi qua; người khác không gì cả. Với thư mục, quyền "đi qua" (`x`) quyết định có tới được file bên trong không. Thư mục log `root:<nhóm của site> 0750` nghĩa là: root ghi, site đọc được log của mình, site khác không qua được cửa. Hệ quả phụ: worker của nginx (`www-data`) cũng không qua được, dẫn tới mục 4.8.

- Trong mã: `SiteLogs` trong `internal/steps/site_user.go`; `LogPerms` trong `internal/checks/m14.go`.
- Tự nhìn: `scripts/vm.sh root stat -c "%U:%G %a %n" /var/log/tpan/sites/a.example.com /var/log/tpan/sites/a.example.com/access.log`. Output cùng dạng có ở mục 2.2.

### 4.8 Tín hiệu `HUP` và `USR1` của nginx

Tín hiệu là cách một tiến trình báo cho tiến trình khác một việc ngắn gọn (`kill -HUP <pid>`). nginx có một tiến trình chính chạy bằng root và các worker chạy dưới `www-data`. Nhận `USR1`, từng worker tự mở lại file log theo đường dẫn. Nhận `HUP`, tiến trình chính nạp lại cấu hình, mở file log bằng quyền root, rồi sinh worker mới thừa kế các file đã mở; worker cũ làm nốt request đang chạy rồi thoát. Với thư mục log `0750`, worker không tự mở được file, nên sau khi logrotate đổi tên file phải gửi `HUP`. Cái giá ghi trong chú thích của template: `HUP` cũng là nạp lại cấu hình, nên nếu cấu hình nginx đang sai cú pháp thì log chưa được mở lại cho tới lần reload đúng.

- Trong mã: `internal/templates/files/logrotate-sites.tmpl` dòng 30 tới 43.
- Tự nhìn: `scripts/vm.sh root grep -n 'kill -' /etc/logrotate.d/tpan-sites` (chưa chạy). Phép thử xoay log thật ở mục 2.8.

### 4.9 Vì sao nginx đứng trước

Trước hết, hiện trạng cho đúng: TPAN có **hai stack, chọn một**. Hoặc nginx với PHP-FPM (mặc định), hoặc Apache với PHP-FPM. Chưa có stack "nginx đứng trước Apache"; `docs/thay-doi/M14-nghien-cuu.md` mục 3 xếp nó vào việc để sau. Trong TPAN hôm nay, "nginx đứng trước" nghĩa là: nginx là thứ đứng mặt tiền, nhận mọi kết nối từ Internet và đứng trước PHP-FPM, và nó là stack mặc định.

Bốn lý do quen thuộc. Đây là kiến thức chung về web server; mốc này không đo cái nào trong bốn cái:

1. **Mô hình xử lý kết nối.** nginx chạy vài tiến trình worker, mỗi worker là một vòng lặp sự kiện (event loop): nó hỏi nhân Linux "kết nối nào vừa có dữ liệu", xử một chút cho từng kết nối, không đứng chờ kết nối nào. Một worker giữ được hàng nghìn kết nối, mỗi kết nối tốn rất ít bộ nhớ. Apache kiểu cũ (chế độ prefork, worker) dành hẳn một tiến trình hoặc một luồng cho mỗi kết nối tới khi xong. Nói cho công bằng: Apache của TPAN chạy chế độ event (chú thích đầu `site-apache.conf.tmpl`), ở đó kết nối đang rảnh không còn chiếm luồng, nên khoảng cách nhỏ hơn các bài viết cũ mô tả.
2. **Đỡ khách chậm.** Khách mạng yếu gửi request từng ít một, hoặc nhận trả lời rất chậm. nginx nhận đủ request rồi mới chuyển cho PHP, và nhận hết trả lời của PHP vào bộ đệm rồi tự trả dần cho khách. Tiến trình PHP là thứ đắt (mỗi site tối đa `pm.max_children = 5`), được nhả ngay thay vì bị một khách chậm giữ.
3. **Kết thúc TLS.** Bắt tay TLS và chứng chỉ được xử ở một chỗ là web server mặt tiền; phía sau nó nói chuyện với PHP-FPM qua socket nội bộ. Trong TPAN đó là khối cổng 443 của `site-nginx-ssl.conf.tmpl` và vhost mặc định có `ssl_reject_handshake on`.
4. **File tĩnh và cache.** nginx trả ảnh, CSS thẳng từ đĩa, không đụng tới PHP (`location /` với `try_files`). Nó còn có thể giữ lại trả lời của PHP để trả cho request sau (`fastcgi_cache`). Nói thật: template của TPAN hiện chưa bật cache nào của nginx.

Lý do thứ năm là điểm M14 rút ra, và có bằng chứng: **trên stack nginx không có file cấu hình web server nào do site ghi được.** nginx không đọc `.htaccess`. Vì vậy L3, L4, L9 không tồn tại ở đó: ba dòng `KQ L3 BO`, `KQ L4 BO`, `KQ L9 BO` ở mục 2.1. Apache có `.htaccess`, và như mục 6.13 của KE-HOACH.md viết: mọi thứ cho `.htaccess` làm được là thứ một site bị chiếm làm được. Trên Apache phải thêm cả một cơ chế (ba chế độ, danh sách 55 tên, phép quét) để về được mức mà nginx có sẵn.

Nếu sau này đặt nginx trước Apache (chưa thiết kế, chưa thử):

- `.htaccess` quay lại phía sau nginx, nên ba chế độ `.htaccess` của M14 phải áp cho vhost Apache phía sau. Ý này có trong nghiên cứu ("phải đi cùng 2.3").
- Các ý sau là mình suy ra, tài liệu nguồn không ghi: Apache chỉ nghe `127.0.0.1`, để không ai đi vòng qua nginx; IP thật của khách phải được chuyển cho Apache và Apache chỉ tin đúng nginx, vì log và fail2ban dựa vào IP; `route explain` và `web.server_name.unique` phải lần qua hai tầng chọn vhost thay vì một; file tĩnh do tầng nào trả thì tầng đó phải có dòng chặn symlink.

- Trong mã: đoạn cuối mục 6.13 của `docs/KE-HOACH.md`; `scripts/e2e-m14/tan-cong.sh` dòng 152 (ba dòng `BO`).
- Tự nhìn: `scripts/vm.sh exec ps -o user,pid,ppid,cmd -C nginx` (chưa chạy), để thấy một tiến trình chính của root và các worker của `www-data`.

## 5. Cách khác đã cân nhắc, và chỗ đã làm sai

### 5.1 Sinh danh sách `AllowOverrideList` tự động

Ý đầu tiên là lấy mọi chỉ thị Apache biết (`apache2ctl -L`) rồi bỏ đi các tên nguy hiểm. Danh sách ra 129 tên. Apache chỉ nhận 64 tên trên một dòng, bỏ phần thừa không báo gì; `RewriteEngine` nằm trong phần bị bỏ, và WordPress báo 500. Viết thành hai dòng cũng không được: dòng sau thay dòng trước. Nên danh sách là 55 tên chọn tay, có test đếm. Nguồn: `M14-nghien-cuu.md` mục 1.3 và chú thích của `MaxNames`; không có file kết quả riêng cho lần thử 129 tên.

### 5.2 Để `Options` trong danh sách (và lỗ L9)

Đặc tả của nghiên cứu là `AllowOverride None` cộng danh sách 56 tên, trong đó có `Options`. Nghiên cứu cũng xếp "symlink qua Apache: 403 nhờ `SymLinksIfOwnerMatch`" vào nhóm "đã thử, không lộ". Lúc lập hợp đồng, phiên chính thử thêm dạng `Options` không có dấu `+` và thấy lộ. `t9-apache-options-symlink.ket-qua.txt` đo bốn cấu hình:

| Cấu hình vhost | `Options FollowSymLinks` rồi lấy file của B | `Options -Indexes` (plugin hay ghi) |
|---|---|---|
| 0. `AllowOverride All` (trước M14) | HTTP 200, lộ | trang chủ 200 |
| 1. `AllowOverride None` + danh sách có `Options` (đặc tả cũ) | HTTP 200, lộ | trang chủ 200 |
| 2. `AllowOverride Options=Indexes,MultiViews,SymLinksIfOwnerMatch` + danh sách không có `Options` | HTTP 500 | trang chủ 200 |
| 3. `AllowOverride None` + danh sách không có `Options` | HTTP 500 | trang chủ 500 |

Cấu hình 1 không chặn được. Cấu hình 3 chặn được nhưng làm hỏng site của người dùng lương thiện. Chỉ cấu hình 2 đạt cả hai, và đó là bản đang dùng. Cái giá: `.htaccess` có `Options +FollowSymLinks` (một số plugin và bài hướng dẫn cũ ghi dòng này) bị coi là vướng; site đó phải bỏ dòng ấy hoặc ở `day-du`.

Nếu người review hỏi "sao biết đã thử đủ": chưa chắc đủ. L9 lọt qua vòng nghiên cứu vì nghiên cứu chỉ thử symlink với vhost nguyên trạng (`t1` mục 5: 403), không thử cho `.htaccess` đổi `Options` trước.

### 5.3 Dùng phép kiểm lệch để báo vhost cũ

Nghiên cứu viết: sau khi cập nhật, health "báo các site cũ lệch template". Thực tế phép kiểm lệch (M4) so file với mã băm sha256 của bản TPAN đã ghi, nên vhost do bản cũ ghi vẫn "không lệch". Phải thêm một phép kiểm riêng, `web.hardened`, đọc nội dung vhost và tìm đúng các dòng chặn.

### 5.4 logrotate gửi `USR1` (chỗ nghiên cứu kết luận sai)

Script `t7` đổi quyền thư mục log rồi kết luận "logrotate vẫn xoay". Nó không đếm xem sau khi xoay, request mới được ghi vào đâu. Thử lại bằng `t8-log-usr1-hay-hup.ket-qua.txt`:

```
## 1. logrotate -f voi postrotate USR1 (ban hien tai)
access.log: 0 dong | access.log.1: 6 dong
x1..x3 nam o dau: access.log=0 access.log.1=3
2026/11/20 17:11:46 [emerg] 13753#13753: open() "/var/log/tpan/sites/b.example.com/access.log" failed (13: Permission denied)

## 2. gui HUP (reload) roi goi tiep
y1,y2 nam o dau: access.log=2 access.log.1=0
```

Với `USR1`, ba request mới rơi cả vào `access.log.1`: worker không qua được thư mục `0750` để mở file mới, nên vẫn ghi vào file cũ đã đổi tên. Với `HUP` thì ghi đúng `access.log`. Từ đó phép thử xoay log trong e2e (mục 2.8) đếm riêng số dòng mang dấu "trước" và "sau".

### 5.5 Chỉ tin `systemctl show -p ProtectProc`

Đặc tả chỉ yêu cầu phép kiểm `php.protectproc` hỏi systemd. Đo trên máy ảo thì thấy: ngay sau `daemon-reload`, `systemctl show` đã in `invisible` dù dịch vụ chưa restart, tức là tiến trình đang chạy vẫn dùng `/proc` cũ. Chiều ngược lại cũng vậy: gỡ drop-in rồi `daemon-reload` thì nó in `default` trong khi tiến trình vẫn có `hidepid=invisible`. systemd nói về cấu hình nó đã nạp, không nói về tiến trình đang chạy. Nên có hàm `ProcHidden` đọc `/proc/<MainPID>/mountinfo` của chính tiến trình đó; systemd nói `invisible` mà tiến trình chưa có thì là cảnh báo kèm lệnh restart. Nguồn: phần "Đã chốt khi làm M14" ở mục 6.13 của `docs/KE-HOACH.md`, đợt 1, mục 3; lần đo đó không có file log riêng trong `docs/bang-chung/`.

### 5.6 Lồng từng site bằng systemd (để sau)

`t5-long-theo-site.ket-qua.txt` thử cho mỗi site một dịch vụ PHP-FPM riêng, bọc bằng các chỉ thị của systemd (`ProtectSystem=strict`, chỉ gắn thư mục của site vào, `/tmp` riêng, trần RAM và CPU). Kết quả tốt hơn hẳn bản vá của M14:

```
 A (trong lồng):
   ls /var/www: a.example.com
   so tien trinh cua user khac nhin thay: 0
 B (FPM chung, như TPAN hiện nay):
   ls /var/www: a.example.com b.example.com html
   so tien trinh cua user khac nhin thay: 119

## 2. OPcache: A include wp-config.php của B (không bật validate_permission)
  include wp-config.php cua B: khong lay duoc
```

Site trong lồng không thấy cả tên site khác, có OPcache riêng (L1 hết từ gốc), và script cố cấp phát 400 MB bị giết ở trần 200 MB trong khi site vẫn sống. Vì sao chưa chọn: mỗi site thêm một tiến trình chủ PHP-FPM, `t5` đo khoảng 32 MB RAM kể cả lúc không có khách (`31.7 MB  php-fpm: master process`). Điều đó đi ngược `pm = ondemand` (site không có khách thì không tốn RAM). Nó được ghi là ứng viên cho mốc sau, gắn với gói hạn mức của M13 (trần RAM, CPU theo gói). Một chi tiết đáng nhớ: lần thử đầu, script vượt trần RAM làm cả dịch vụ dừng; phải đặt `OOMPolicy=continue` (file `t5-long-theo-site.ket-qua-lan1-OOMPolicy-mac-dinh.txt`).

### 5.7 Gắn lại `/proc` của cả máy với `hidepid`

Cách này ẩn tiến trình với mọi user trên máy, không riêng PHP. Đặc tả chỉ ghi quyết định ("Không gắn lại `/proc` của cả máy"), không ghi lý do. Lý do theo mình hiểu: đổi `/proc` của cả máy ảnh hưởng mọi dịch vụ và mọi người đăng nhập vào máy, vượt phạm vi của một panel quản lý web; còn `ProtectProc` chỉ đụng đúng dịch vụ PHP-FPM và gỡ được bằng cách xoá một file. Nếu người review hỏi sâu, hãy nói rõ đây là phần chưa có ghi chép.

### 5.8 Kiểm chứng của harden chạy cả bộ phép kiểm của từng site

Bị loại vì một site hỏng ở chặng không liên quan (DB, mã nguồn bị sửa) sẽ kéo hoàn tác bản vá của cả máy. Harden chỉ kiểm phần nó đụng tới; health đầy đủ chạy sau job và mở sự cố cho site hỏng. Tương tự, harden không quét được `.htaccess` của một site thì giữ site đó ở `day-du` chứ không dừng cả job.

### 5.9 Chỗ đã làm sai trong mốc này, và phát hiện bằng cách nào

- **Sửa tự động `web.hardened` bỏ qua đúng file cần sửa.** Từ M4, `web.rewrite` bỏ qua file "khớp mã băm đã ghi nhưng khác template", vì coi đó là bản sửa tay người dùng đã nhận (`dropAccepted`). Vhost do bản TPAN cũ ghi có đúng hình dạng đó. Lần thử nâng từ v0.1.2 trên máy ảo, job sửa bỏ bước ghi vhost rồi hỏng ở kiểm chứng. Sửa: với `web.hardened` thì luôn ghi lại (`vhostRewrite` trong `fix.go`). Hệ quả phải nói thật: một vhost bạn từng sửa tay và đã bấm nhận, nếu thiếu dòng chặn, sẽ bị ghi đè khi bấm sửa `web.hardened`. Bản xem trước hiện diff.
- **Lời nhắn chỉ sai lệnh.** Khi `Isolated` thấy đọc được log của site kia, lời nhắn ghi `tpan health --fix`. Trên máy vừa nâng, lệnh đúng là `tpan security harden`. Phát hiện ở e2e nâng cấp; sửa ở commit `9b09c9c`. Output ở mục 2.7 là sau khi sửa.
- **L6 "đã chặn sẵn" trên Apache.** Lần chạy tay đầu thấy Apache không lộ L6, trái bảng nghiên cứu. Nguyên nhân: máy ảo lần đó đã qua một lần chuyển web server. e2e nâng cấp từ máy sạch cho thấy v0.1.2 cài Apache từ đầu cũng lộ (nang-cap dòng 1783). Bài học: phép thử "trước" phải chạy từ máy sạch.
- **`steps.Service` không xoá trạng thái `failed`.** Lỗi có từ trước M14, lộ ra khi chạy lại e2e nâng cấp của M13: dịch vụ vừa chết liên tục thì systemd chặn khởi động, `enable --now` vẫn thoát 0. Giờ có `systemctl reset-failed <unit>` trước.
- **`multipass exec` treo giữa `tpan health`.** Gặp hai lần, output về Mac bị cắt. `scripts/vm.sh` có thêm chế độ `TPAN_VM_CAPTURE=1` (ghi output vào file trong máy ảo rồi lấy về). Đây là cách tránh; nguyên nhân treo chưa tìm ra.

### 5.10 Giới hạn còn lại, nói thẳng

- **Site ở `day-du` còn mở L4 và L9.** Một site như thế mà bị chiếm thì chạy được mã dưới user của site khác trên cùng máy (bằng chứng ở mục 2.6 và 2.7). TPAN chỉ cảnh báo, không tự ép sang `an-toan`, vì ép là làm site đó báo 500. Nút "Áp bản vá cô lập" trên màn Bảo mật nổi mãi khi còn site `day-du`: có chủ ý.
- **PHP gọi lệnh shell vẫn đọc được mọi file mà quyền Linux cho phép,** ngoài thư mục site: file hệ thống ai cũng đọc được, tên của mọi site qua `ls /var/www`. L7 chỉ chặn nhìn tiến trình. Muốn đóng phải lồng theo site (mục 5.6).
- **Ở `an-toan`, một dòng lạ do plugin ghi làm cả site báo 500** cho tới khi gỡ dòng đó. Health nêu đúng file và dòng ở `web.htaccess`, nhưng phép `http.site` bên cạnh vẫn ghi nguyên nhân chung "PHP hoặc WordPress báo lỗi" (mục 2.6).
- **`tpan isolation probe` chưa làm.** Chưa có lệnh của panel để chạy lại các phép thử trên máy của khách; chỉ có script cho máy ảo test.
- **Giao diện mới chưa ai xem bằng mắt.**
- **`www.<domain>` rơi vào vhost mặc định.** TPAN chưa có tên phụ.
- **`web.server_name.unique` có ba chỗ làm gọn** (mục 3.8): chỉ so cổng, không lần theo `include` trong khối `server`, bỏ qua tên wildcard và regex.
- **User thường gọi `/route` cho site của mình trên một máy cấu hình lệch** thấy đường dẫn file và tên pool lạ trong kết quả. Chưa che.
- **Hai file cấp máy không phải lúc nào cũng nằm trong phép kiểm lệch.** Dòng đầu của `99-tpan.ini` và của drop-in ghi "Sửa tay sẽ bị tpan health báo lệch". Trên máy đã chạy harden, health có dòng `File /etc/php/8.3/fpm/conf.d/99-tpan.ini còn đúng như tpan đã ghi` và dòng tương tự cho drop-in (nang-cap dòng 1258, 1259). Trên máy cài mới bằng `stack install`, phần "Máy và stack" của health không có hai dòng đó (`M14.log` dòng 486 tới 490). File ini vẫn được canh bởi `php.opcache` (so nội dung và giá trị PHP đang dùng); drop-in thì chỉ được canh qua giá trị `ProtectProc`. Mình chỉ thấy điều này trong log, chưa lần ra nguyên nhân trong mã; ghi lại để phiên chính xem.
- Mấy chỗ nhỏ đã ghi nhận: lời từ chối in `Options FollowSymLinks` không kèm dấu `+` của dòng gốc; cảnh báo rủi ro của `day-du` hiện ở bản xem trước, không hiện lại khi chạy `--yes`; `opcache_reset` sau bản vá chưa có phép thử riêng.
- **Chưa có stack "nginx đứng trước Apache".**

## 6. Tự kiểm

Câu 3, 8 và 10 phải chạy lệnh trên máy ảo mới trả lời chắc được.

1. `a.example.com` và `b.example.com` nằm trên cùng một IP, cùng cổng 80. nginx dựa vào đâu để biết một request thuộc site nào? File `.php` của A do tiến trình nào chạy, dưới user nào? Chỉ ra dòng template cho từng ý.
2. Vhost mặc định để làm gì? Nếu nó biến mất thì khách gõ thẳng IP của máy thấy gì, và phép kiểm nào của health báo?
3. (Máy ảo) Đặt một file `/etc/nginx/sites-enabled/aaa-thu.conf` khai `server_name a.example.com;` với một `root` khác, rồi `nginx -t` và reload. `nginx -t` có báo lỗi không? Lệnh nào của TPAN cho biết file nào đang nhận request? TPAN có tự gỡ file đó không?
4. Tắt `opcache.validate_permission` thì chuyện gì xảy ra? Vì sao `open_basedir` và quyền `600` của `wp-config.php` đều đúng mà không cứu được? Health nói gì?
5. Vì sao thư mục log của site thuộc `root` mà không thuộc user của site? Quyết định đó buộc phải đổi gì ở logrotate?
6. Site A đặt một symlink trong `public` của nó trỏ vào `wp-config.php` của B rồi gọi qua nginx. nginx trả gì, trước và sau M14, và vì sao?
7. Vì sao `AllowOverrideList` là 55 tên chọn tay chứ không sinh tự động? Vì sao `Options` không nằm trong danh sách mà `.htaccess` vẫn viết được `Options -Indexes`?
8. (Máy ảo) Máy vừa nâng từ bản trước M14. Bạn chạy lại `tpan site add` cho một site cũ và job báo "hỏng, đã hoàn tác sạch". Vì sao? Gõ lệnh gì để xem trước bản vá sẽ làm gì, và nó làm theo thứ tự nào?
9. Trên stack nginx, bộ phép thử in `BO` cho L3, L4, L9. Vì sao ba lỗ đó không có ở nginx? Trên Apache, một site ở `day-du` còn mở lỗ nào, và vì sao ở chế độ đó L3 vẫn "chặn" mà L4 lại "lộ"?
10. (Máy ảo) `ProtectProc=invisible` chặn được gì và không chặn được gì? Vì sao health chỉ coi thiếu nó là cảnh báo? Lệnh nào cho biết tiến trình PHP-FPM đang chạy đã thật sự có `/proc` riêng, và vì sao không tin riêng `systemctl show`?

## Đáp án

**1.** Dựa vào header `Host` của request, so với `server_name` của từng khối `server` (`site-nginx.conf.tmpl` dòng 8). Khối khớp chỉ có một `root` (dòng 10) và chuyển file `.php` qua `fastcgi_pass` tới socket riêng của site (dòng 61). Pool nghe socket đó (`site-pool.conf.tmpl` dòng 14) là tiến trình PHP-FPM chạy dưới user `tp_<slug>` của site (dòng 9), bị khoá `open_basedir` vào thư mục của site (dòng 29). Kiểm trên máy: `tpan route explain a.example.com --evidence` in đúng các dòng đó (mục 2.3).

**2.** Nó hứng mọi request mang tên không khớp site nào (gõ thẳng IP, hoặc một tên miền lạ trỏ về máy) và từ chối: nginx trả 444 tức đóng kết nối, ở cổng 443 thì từ chối bắt tay TLS; Apache trả 403 (mục 2.5). Thiếu nó, web server lấy vhost đầu tiên theo thứ tự tên file làm mặc định, nên khách gõ IP sẽ thấy nội dung của site đó (chú thích đầu `nginx-default.conf.tmpl`; e2e của M14 không có phép thử xoá file này, chưa chạy). Health có hai phép báo: "Site mặc định của nginx đóng kết nối request lạ" (có từ M1, gọi bằng IP) và `web.default.rejects` (M14, gọi bằng một tên `tpan-<ngẫu nhiên>.invalid`).

**3.** `nginx -t` vẫn thoát mã 0, chỉ in một dòng `[warn] conflicting server name "a.example.com" on 0.0.0.0:80, ignored`. `tpan route explain a.example.com` cho chặng vhost `[lỗi]`, nêu file lạ và số dòng, thoát mã 1; `tpan health` cho `web.server_name.unique` lỗi, nêu cả hai file. TPAN không gỡ: nó không xoá file nó không ghi. Lệnh sửa tay nó gợi ý là chuyển file ra khỏi thư mục cấu hình (`mv <file> /root/`), `nginx -t`, `systemctl reload nginx`. Output thật của tình huống này ở mục 2.4. Thử xong nhớ gỡ file và reload.

**4.** L1 mở lại: PHP của một site `include` được file PHP đang nằm trong cache của site khác, ví dụ `wp-config.php`, và lấy được `DB_PASSWORD`. Đó đúng là trạng thái của v0.1.2 (`KQ L1 LO`, mục 2.1). `open_basedir` và quyền `600` không cứu vì cả hai chỉ được kiểm ở bước mở file, mà OPcache đưa mã đã biên dịch từ vùng nhớ chung, không mở file. Vùng nhớ đó chung vì mọi pool của một bản PHP là con của cùng một tiến trình chủ PHP-FPM. Health: `php.opcache:<bản>` thành `[lỗi]`, vì nội dung file ini khác bản TPAN ghi và `php-fpm<bản> -i` in `Off`; có sửa tự động `php.harden`, và lời nhắn chỉ sang `sudo tpan security harden`. Kịch bản "sửa thành 0 rồi reload" chưa chạy trong e2e; trạng thái tương đương (không có file, giá trị `Off`) là máy vừa nâng ở mục 2.7.

**5.** Vì logrotate chạy bằng root và phải đổi tên, tạo file trong thư mục đó. Nếu user của site ghi được vào thư mục, nó có thể đặt symlink để lừa root ghi đè file khác. Nhóm của thư mục là nhóm của site, quyền `0750`, nên site vẫn đọc được log của mình còn site khác thì không (L6). Hệ quả: worker của nginx chạy dưới `www-data` cũng không qua được thư mục `0750`, nên không tự mở lại file log được khi nhận `USR1`. logrotate vì vậy gửi `HUP`: tiến trình chính (root) mở file rồi sinh worker mới thừa kế. Với `USR1`, log mới rơi vào `access.log.1` (`t8`, mục 5.4).

**6.** Trước M14: 403. Không phải nhờ chặn symlink, mà vì `wp-config.php` có quyền `600` và nginx (`www-data`) không đọc được nó (`t4` mục 2). File `640` của B thì lộ theo đúng cách này (HTTP 200). Sau M14: 404 (`t4`, sau khi thêm dòng chặn: `wp-config.php của B (600): HTTP 404`). Lý do: `disable_symlinks if_not_owner` ở dòng 17 của vhost. Symlink do A sở hữu, file đích do B sở hữu, khác chủ nên nginx không đi theo; `try_files` chuyển tiếp cho `/index.php` và WordPress trả 404. Symlink của A trỏ vào file của chính A vẫn 200. Trên Apache, `Options SymLinksIfOwnerMatch` cho 403, miễn là site không ở `day-du` (L9).

**7.** Apache chỉ nhận tối đa 64 tên trên một dòng `AllowOverrideList`, bỏ phần thừa không báo gì, và dòng thứ hai thay dòng thứ nhất. Danh sách sinh tự động có 129 tên làm rơi `RewriteEngine`, WordPress báo 500. Nên danh sách là hằng `htaccess.Allowed`, 55 tên, có test đếm và test "không chứa tên cấm". `Options` không nằm trong đó vì danh sách cho phép theo tên, không nhìn tham số: cho `Options` là cho luôn `Options FollowSymLinks` (L9). `Options` được mở bằng dòng riêng `AllowOverride Options=Indexes,MultiViews,SymLinksIfOwnerMatch`, cú pháp duy nhất của Apache giới hạn theo từng tuỳ chọn. `-Indexes` nằm trong ba tuỳ chọn đó nên chạy; `FollowSymLinks` thì Apache báo 500.

**8.** Vì kiểm chứng của mọi Plan chạm vào site dùng chung bộ phép kiểm với health, và từ M14 bộ đó có `log.perm`, `web.hardened` và phép cô lập năm phép thử (có thử đọc log). Job sửa được phần của chính site đó, nhưng log của site bên cạnh còn `0755`, nên `isolation:<site này>:<site kia>` lỗi và engine hoàn tác (mục 2.7). Xem trước: `scripts/vm.sh root tpan security harden --dry-run` (trên máy thật là `sudo tpan security harden --dry-run`). Thứ tự: với từng bản PHP, ghi file ini OPcache (reload) rồi drop-in `ProtectProc` (`daemon-reload`, kiểm cú pháp, restart); ghi file logrotate; đặt quyền log của từng site; ghi lại vhost của từng site theo chế độ `.htaccess` đã quyết lúc dựng Plan (sạch thì `an-toan`, vướng thì giữ `day-du`); kiểm cú pháp và reload web server một lần; chạy kiểm chứng; lưu chế độ mới vào bảng `sites`.

**9.** nginx không đọc `.htaccess`, và không có file cấu hình web server nào khác do site ghi được, nên site không có cách ra lệnh cho nginx. Trên Apache, site ở `day-du` còn L4 và L9 (mục 2.6). L3 vẫn "chặn" vì phép thử L3 đo đúng một đường, `SetEnv PHP_ADMIN_VALUE`, và hai dòng `ProxyFCGISetEnvIf "true" !...` có trong vhost ở mọi chế độ nên gỡ được biến đó. L4 "lộ" vì ở `day-du` `.htaccess` được dùng `SetHandler` để đổi socket và dùng chính `ProxyFCGISetEnvIf` để đặt lại biến.

**10.** Chặn: PHP của một site (và lệnh shell nó gọi) nhìn thấy tiến trình cùng dòng lệnh của user khác; phép thử đếm từ 118 xuống 0. Không chặn: lệnh shell vẫn đọc được file ngoài thư mục site mà quyền Linux cho phép, và vẫn `ls /var/www` ra tên mọi site. Chỉ là cảnh báo vì quyền file vẫn giữ: site này không đọc được file của site kia; thứ lộ ra là danh sách tiến trình. Lệnh: `scripts/vm.sh root bash -c 'grep " /proc " /proc/$(systemctl show php8.3-fpm -p MainPID --value)/mountinfo'` rồi tìm `hidepid=invisible` trong dòng in ra (lệnh này chưa có output trong log); hoặc chạy bộ phép thử và đọc dòng `KQ L7`. Không tin riêng `systemctl show` vì nó in `invisible` ngay sau `daemon-reload`, cả khi dịch vụ chưa restart và tiến trình đang chạy vẫn dùng `/proc` cũ (mục 5.5).

