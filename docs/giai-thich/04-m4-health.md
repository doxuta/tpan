# M4: health đầy đủ, sự cố, sửa tự động, log trực tiếp

Đọc kèm: `01-m1-cai-stack.md` (engine, Step, Plan, Journal, `managed_files`), `02-m2-site-wordpress.md` (phép kiểm theo đường đi request, `site.Layout`), `03-m3-giao-dien-va-api.md` (API, SSE, khoá job, `jobStart`), `00-go-can-ban.md` (mục 7 context, mục 8 goroutine, mutex, channel), `docs/KE-HOACH.md` mục 6.7 (health), 12 (bước 9 và 11), 13 (hàng M4), `docs/thay-doi/M4.md` (quyết định và chỗ khác đặc tả), và bằng chứng `docs/bang-chung/M4.log`, `docs/bang-chung/M4-htaccess.log`.

## 1. Mốc này làm được gì

- tpan tự kiểm máy theo lịch: phép kiểm nhẹ 5 phút một lần, đầy đủ mỗi giờ, và ngay sau mỗi job. Gõ tay thì `sudo tpan health [<domain>] [--json]`. Kết quả nằm trong DB, Bảng điều khiển đọc từ đó chứ không chạy lại mỗi lần mở trang.
- Phép kiểm chuyển sang lỗi thì mở một sự cố, đạt lại thì đóng. Sự cố giữ bằng chứng lúc mở, lúc đóng và các job sửa ở giữa. Màn Sự cố cho ba cách: sửa tự động (qua xem trước và duyệt), sửa tay (lệnh để tự gõ), hỏi AI (chưa có ở mốc này, chỉ có ghi chú).
- Phát hiện lệch: file cấu hình tpan đã ghi mà bị ai sửa thì health báo đúng file đó, `tpan health --fix --dry-run` in diff, và người dùng chọn ghi lại bản của tpan hoặc nhận bản sửa tay làm chuẩn mới.
- Tự kiểm chính tpan: DB toàn vẹn, chỗ trống ở `/var/lib/tpan`, chứng chỉ panel, quyền file, job treo, bộ hẹn giờ còn sống.
- Trang site có log trực tiếp (`access`, `error`, `php`) và số request, số lỗi 5xx trong một giờ. `scripts/faults/` có 6 kịch bản gây lỗi có chủ ý để thử chính hệ thống health trên máy ảo.

## 2. Chạy thử

Output dưới đây lấy nguyên văn từ `docs/bang-chung/M4.log`: lần chạy `scripts/e2e-m4.sh` ngày 19/11/2026, 18:38 tới 18:45, trên máy ảo `tpan-m4`, dòng cuối là `TỔNG KẾT: 0 khẳng định không đạt`. File chạy trong máy ảo build từ commit `f569de3` (journal ghi `version=f569de3`). Commit `9dbec02` sau nó chỉ thêm log và tài liệu. Commit `8e90056` sửa ba thứ sau khi đọc lại log: dòng "sửa tay" của phép kiểm `php.socket` đổi từ `systemctl reload` sang `systemctl restart` (output ở 2.5 bên dưới còn là chữ cũ, xem câu tự kiểm 1), bỏ một lần lọc thừa trong `Runner.Run`, và sửa chú thích. Mã đó chạy lại e2e từ máy sạch, log ở `docs/bang-chung/M4-sau-sua.log`, cũng xanh.

Bốn điều cần biết khi đọc:

- Mọi lệnh chạy trên Mac. Lệnh trong máy ảo đi qua `scripts/vm.sh root ...` hoặc `scripts/vm.sh exec ...`; lệnh `curl` gọi panel chạy trên Mac, qua mạng vào máy ảo.
- Kịch bản in dòng lệnh bằng `$*`, nên dấu nháy bị mất (giống M2, M3). Kịch bản tự che: `<vm>` (IP máy ảo), `<mac>` (IP của Mac), `<tmp>` (thư mục tạm), `<mật khẩu, đã che>`.
- Các khối ghi `tpan health --json` **không phải** JSON nguyên văn. Kịch bản đọc JSON đó bằng `jq` rồi in lại thành dòng `PASS|FAIL  chặng  đối tượng  id  tóm tắt`, thêm khối "Không đạt, kèm bằng chứng" và hai dòng "sự cố vừa mở/đóng". Bạn tự gõ `tpan health --json` sẽ thấy JSON, không thấy các dòng này. Danh sách sự cố (`["#1 MỞ [web] ..."]`) và chi tiết sự cố cũng được `jq` rút gọn.
- Chỗ mình bớt dòng thì ghi `...` hoặc "bớt dòng". Không dòng output nào bị viết lại.

### 2.1 Máy mới khoẻ, bộ hẹn giờ đã chạy

Máy ảo sạch, `tpan stack install --web nginx`, `tpan setup`, tạo `a.example.com` và `b.example.com` (output giống M1, M2, M3). Rồi `scripts/vm.sh root systemctl restart tpan`, chờ 5 giây, đọc `settings` bằng python3 trong máy ảo (máy ảo không có lệnh `sqlite3`):

```
health.scheduler_beat 2026-11-19T11:40:02Z
health.last_run 2026-11-19T11:40:06Z
```

Journal của `tpan.service` (bớt các dòng `pam_unix` của `runuser`):

```
2026-11-19T18:39:42+07:00 tpan-m4 tpan[13728]: 2026/11/19 18:39:42 INFO tpan đang chạy listen=0.0.0.0:8443 tls=true demo=false version=f569de3
2026-11-19T18:39:42+07:00 tpan-m4 tpan[13728]: 2026/11/19 18:39:42 INFO health: đang có job, bỏ vòng này full=true domain=""
...
2026-11-19T18:40:02+07:00 tpan-m4 tpan[14549]: 2026/11/19 18:40:02 INFO tpan đang chạy listen=0.0.0.0:8443 tls=true demo=false version=f569de3
```

Dòng "đang có job, bỏ vòng này" lúc 18:39:42 là vòng đầu tiên khi `tpan setup` vừa bật dịch vụ: setup còn giữ khoá, nên bộ hẹn giờ bỏ vòng (mục 3.1). Sau `restart` thì vòng đầu chạy được và ghi `health.last_run`.

`scripts/vm.sh root tpan health --json` (kịch bản in lại, bớt dòng):

```
started 2026-11-19T18:40:08.157356804+07:00  finished 2026-11-19T18:40:12.821868508+07:00  installed true
  PASS  php	(máy, stack)	service.active:php8.3-fpm	php8.3-fpm đang chạy và tự bật khi khởi động máy
  PASS  machine	(máy, stack)	memory:ram	RAM còn dùng được 86.6% (3.3 GiB trên 3.8 GiB); máy không có swap
  PASS  web	(máy, stack)	drift:/etc/nginx/sites-available/tpan-default.conf	File /etc/nginx/sites-available/tpan-default.conf đúng như tpan đã ghi
  PASS  php	a.example.com	php.socket:a.example.com	Socket của pool có, chủ www-data:www-data, quyền 0660
  PASS  wordpress	a.example.com	wp.checksums:a.example.com	Mã nguồn WordPress khớp checksum
  PASS  web	a.example.com	drift:/etc/nginx/sites-available/tpan-a.example.com.conf	File /etc/nginx/sites-available/tpan-a.example.com.conf đúng như tpan đã ghi
  ...
  PASS  tpan	tpan	tpan.db:integrity	PRAGMA integrity_check trả ok
  PASS  tpan	tpan	disk.free:/var/lib/tpan	Ổ đĩa chứa /var/lib/tpan còn trống 15.7 GiB
  PASS  tpan	tpan	tpan.cert:/etc/tpan/tls/cert.pem	Chứng chỉ của panel còn 3652 ngày
  PASS  tpan	tpan	tpan.perm:/var/lib/tpan/tpan.db	/var/lib/tpan/tpan.db: quyền 0600, chủ root:root đúng
  PASS  tpan	tpan	tpan.jobs:stuck	Không có job nào chạy quá lâu
  PASS  tpan	tpan	tpan.scheduler:health	Bộ hẹn giờ health vẫn chạy
Không đạt, kèm bằng chứng:
(mọi phép kiểm đạt)
```

Cả lần chạy có 42 phép kiểm, chia bốn nhóm: máy và stack, từng site, và `tpan`. Các file tpan quản lý (bảng `managed_files`, kịch bản đọc bằng python3):

```
/etc/logrotate.d/tpan-sites 2e760d317df14588
/etc/mysql/mariadb.conf.d/99-tpan.cnf 2e760d317df14588
/etc/nginx/sites-available/tpan-a.example.com.conf 9b6f74a476af153d
/etc/nginx/sites-available/tpan-b.example.com.conf 8ecedf3c7a256f0e
/etc/nginx/sites-available/tpan-default.conf 2e760d317df14588
/etc/php/8.3/fpm/pool.d/tpan-a_example_com_7653e4.conf 9b6f74a476af153d
/etc/php/8.3/fpm/pool.d/tpan-b_example_com_cd59d5.conf 8ecedf3c7a256f0e
/etc/systemd/system/tpan.service ea83f7ab269e3603
/etc/tpan/tls/cert.pem ea83f7ab269e3603
```

Cột thứ hai là id của job đã ghi file. Mỗi dòng ở đây thành một phép kiểm `drift:<path>`.

Thời gian (`scripts/vm.sh root bash -c 'time ...'`):

```
[2026-11-19 18:40:13] $ scripts/vm.sh root bash -c time tpan health --json >/dev/null

real	0m3.654s
...
[2026-11-19 18:40:16] $ scripts/vm.sh root bash -c cd / && time runuser -u tp_a_example_com_7653e4 -- wp core verify-checksums --path=/var/www/a.example.com/public
Success: WordPress installation verifies against checksums.

real	0m1.670s
...
[2026-11-19 18:40:18] $ scripts/vm.sh root bash -c time tpan health a.example.com --json >/dev/null

real	0m1.773s
```

Bớt các dòng `user` và `sys`. Vòng đầy đủ với 2 site có hai lần `wp core verify-checksums`, mỗi lần khoảng 1,7 giây đo riêng, trong khi cả vòng chỉ 3,7 giây. Nếu trong vòng health chúng cũng tốn như lần đo riêng thì `wp.checksums` chiếm phần lớn thời gian (mình suy ra, chưa đo từng phép kiểm trong một vòng). Đó là lý do nó là phép kiểm "nặng", chỉ chạy mỗi giờ (mục 3.3).

### 2.2 Bước 9: sửa tay một vhost, health báo lệch đúng file đó

Thêm một dòng chú thích hợp lệ vào cuối vhost của site a (Mac):

```bash
scripts/vm.sh root sed -i '$a # e2e-m4: sua tay boi nguoi van hanh' /etc/nginx/sites-available/tpan-a.example.com.conf
```

Mình thêm lại dấu nháy để bạn gõ được. `nginx -t` vẫn đạt (chú thích không làm hỏng cú pháp). Rồi `scripts/vm.sh root tpan health --json` (kịch bản in lại, bớt các dòng PASS):

```
  FAIL  web	a.example.com	drift:/etc/nginx/sites-available/tpan-a.example.com.conf	File /etc/nginx/sites-available/tpan-a.example.com.conf khác bản tpan đã ghi (có thể bị sửa tay)  [sửa tự động: file.rewrite]
Không đạt, kèm bằng chứng:
--- fail drift:/etc/nginx/sites-available/tpan-a.example.com.conf (chặng web)
    File /etc/nginx/sites-available/tpan-a.example.com.conf khác bản tpan đã ghi (có thể bị sửa tay)
    nguyên nhân: Có người hoặc chương trình khác sửa file này. Máy có thể vẫn chạy đúng, nhưng lần tới tpan dựng lại cấu hình sẽ ghi đè bản sửa tay.
    sửa tay: tpan health --fix --dry-run ; sha256sum /etc/nginx/sites-available/tpan-a.example.com.conf
    $ sha256sum /etc/nginx/sites-available/tpan-a.example.com.conf (tpan)  (mã 0)
      | 242c7695ebd6c5425587ac61f156d52df92f5969b661ca8f63b7105bbab6b72d  /etc/nginx/sites-available/tpan-a.example.com.conf
      | mong đợi / expected: a7abe2d4240304755cc582efd59e97e2136d5396fb46812a86ebcb15c77f823a
sự cố vừa mở: #1 drift:/etc/nginx/sites-available/tpan-a.example.com.conf
[2026-11-19 18:40:24] (mã thoát 1)
```

`a7abe2d4...` chính là sha256 kịch bản đo được trước khi sửa. Fail duy nhất là lệch đó; mã thoát 1. Bản chữ (`tpan health` không `--json`) nhắc lại ở cuối:

```
File bị sửa ngoài tpan (lệch):
  /etc/nginx/sites-available/tpan-a.example.com.conf
Xem cách sửa: sudo tpan health --fix --dry-run
```

API thấy cùng sự cố đó (`GET /api/v1/incidents/1`, kịch bản rút gọn bằng `jq`, bớt dòng):

```
  "incident": {
    "id": 1,
    "check_id": "drift:/etc/nginx/sites-available/tpan-a.example.com.conf",
    "target": "a.example.com",
    "stage": "web",
    "opened": "2026-11-19T11:40:22.906109355Z",
    "closed": null
  },
  "current": {
    "status": "fail",
    "auto_fix": "file.rewrite",
    ...
  },
  "jobs": []
```

### 2.3 Xem diff, ghi lại bản của tpan

`scripts/vm.sh root tpan health --fix --dry-run` (bớt phần kết quả phép kiểm ở đầu):

```
Sửa drift:/etc/nginx/sites-available/tpan-a.example.com.conf (file.rewrite)
Kế hoạch: Ghi lại /etc/nginx/sites-available/tpan-a.example.com.conf từ bản của tpan
1 bước sẽ chạy, 0 bước đã đúng sẽ bỏ qua, 0 bước có thể gián đoạn dịch vụ.

 1. [sẽ chạy] Ghi vhost nginx cho a.example.com  (file:/etc/nginx/sites-available/tpan-a.example.com.conf)
    Vì sao: Vhost nói cho nginx biết request tới a.example.com lấy file ở đâu và chuyển .php cho pool nào. Thiếu nó thì request rơi vào site mặc định của tpan (444 hoặc 403).
    $ nginx -t
    $ systemctl reload nginx
    sửa /etc/nginx/sites-available/tpan-a.example.com.conf

    --- /etc/nginx/sites-available/tpan-a.example.com.conf (đang có)
    +++ /etc/nginx/sites-available/tpan-a.example.com.conf (tpan sẽ ghi)
              fastcgi_pass unix:/run/php/tpan-a_example_com_7653e4.sock;
          }
      }
    - # e2e-m4: sua tay boi nguoi van hanh
[2026-11-19 18:40:33] (mã thoát 0)
```

Sau lệnh này dòng sửa tay vẫn còn (`grep -c` ra `1`): `--dry-run` không đổi gì. Rồi `scripts/vm.sh root tpan health --fix --yes` (bớt dòng):

```
Bắt đầu job b4ea6ed47c231008
- Ghi vhost nginx cho a.example.com ...
  xong
  [đạt] File /etc/nginx/sites-available/tpan-a.example.com.conf còn đúng như tpan đã ghi

Job b4ea6ed47c231008: thành công
...
Kiểm lại sau khi sửa:
...
Sự cố vừa đóng:
  #1 [web] File /etc/nginx/sites-available/tpan-a.example.com.conf còn đúng như tpan đã ghi (drift:/etc/nginx/sites-available/tpan-a.example.com.conf) lý do: pass
[2026-11-19 18:40:40] (mã thoát 0)
```

`grep -c` sau đó ra `0`: dòng sửa tay đã bị ghi đè. Site vẫn trả `200`.

### 2.4 Nhận bản sửa tay làm chuẩn

Sửa tay lần nữa (dòng `# e2e-m4: sua tay lan hai, giu lai`), health mở sự cố #2. Kịch bản gọi `POST /api/v1/files/accept` ba lần. Lần đối chứng với `/etc/passwd`:

```
-> HTTP 400, 0.002679s
...
    "code": "path_invalid",
    "message": {
      "vi": "File này không do tpan quản lý.",
```

Lần đối chứng gửi `Sec-Fetch-Site: cross-site`: `-> HTTP 403` (lớp CSRF của M3 chặn). Lần thật với đúng file: `-> HTTP 204`. Sau đó `managed_files` của file này:

```
/etc/nginx/sites-available/tpan-a.example.com.conf e79fe07a8b629138b471f1bb02e2f748c08e5405a78e470d6743d6b6cc1f658c manual
```

`sha256sum` của file trong máy ảo cũng ra `e79fe07a...`, `job_id` là `manual`. Lần `tpan health` tiếp theo: "(mọi phép kiểm đạt)", sự cố #2 `đóng(pass)`, dòng sửa tay vẫn nằm trong file. Bảng `audit`:

```
2026-11-19T11:40:49.013934190Z file.accept /etc/nginx/sites-available/tpan-a.example.com.conf ok
2026-11-19T11:40:37.041906323Z fix.file.rewrite drift:/etc/nginx/sites-available/tpan-a.example.com.conf ok
```

### 2.5 Bước 11: sáu kịch bản lỗi

Mỗi kịch bản: health trước khi phá (không Fail nào), `scripts/faults/<kịch bản>.sh break <domain>`, health báo lỗi, `--fix --dry-run`, `--fix --yes`, health lại (không Fail nào), mọi sự cố liên quan đã `đóng(pass)`.

#### 01: dừng PHP-FPM

`systemctl is-active php8.3-fpm` ra `inactive`, site a trả `502`. Health:

```
  FAIL  php	(máy, stack)	service.active:php8.3-fpm	php8.3-fpm không chạy (trạng thái: inactive)  [sửa tự động: service.start]
  FAIL  php	a.example.com	php.socket:a.example.com	Không có socket /run/php/tpan-a_example_com_7653e4.sock  [sửa tự động: php.restart]
  FAIL  e2e	a.example.com	http.site:a.example.com	Site trả 502: web server không nói chuyện được với PHP
  FAIL  php	b.example.com	php.socket:b.example.com	Không có socket /run/php/tpan-b_example_com_cd59d5.sock  [sửa tự động: php.restart]
  FAIL  e2e	b.example.com	http.site:b.example.com	Site trả 502: web server không nói chuyện được với PHP
...
sự cố vừa mở: #3 service.active:php8.3-fpm, #4 php.socket:a.example.com, #5 http.site:a.example.com, #6 php.socket:b.example.com, #7 http.site:b.example.com
```

Mọi Fail ngoài chặng `e2e` đều ở chặng `php`: đúng "một chặng" theo cách hiểu ở `docs/thay-doi/M4.md` (chặng đầu cuối gọi HTTP thật nên lỗi theo mọi chặng trước nó). `--fix --yes` sửa theo thứ tự (bớt dòng):

```
Bắt đầu job c57af3177568eebf
- Chạy và bật php8.3-fpm khi khởi động ...
  xong
...
Sửa php.socket:a.example.com (php.restart)
Kế hoạch: Khởi động lại PHP-FPM 8.3 để tạo lại socket của a.example.com
0 bước sẽ chạy, 1 bước đã đúng sẽ bỏ qua, 0 bước có thể gián đoạn dịch vụ.
...
Sự cố vừa đóng:
  #3 [php] Dịch vụ php8.3-fpm đang chạy và tự bật cùng máy (service.active:php8.3-fpm) lý do: pass
  #4 [php] Socket của pool PHP có, đúng chủ và đúng quyền (php.socket:a.example.com) lý do: pass
  ...
```

Bật lại dịch vụ là socket có lại, nên hai lần `php.restart` sau đó "đã đúng, bỏ qua" và PHP-FPM không bị khởi động lại thêm lần nào. Lưu ý: ở `--fix --dry-run` trước đó, cả ba bước hiện "sẽ chạy", vì xem trước không thật sự bật dịch vụ.

#### 02: xoá socket của pool

Chỉ `php.socket:a.example.com` và `http.site:a.example.com` Fail. Sửa tự động `php.restart`:

```
Bắt đầu job 85e46bfca3f59099
- Khởi động lại php8.3-fpm để tạo lại /run/php/tpan-a_example_com_7653e4.sock ...
  xong
  [đạt] Socket của pool PHP có, đúng chủ và đúng quyền
```

Ngay sau đó kịch bản đo câu hỏi của mục 16: xoá socket rồi `reload` có tạo lại không:

```
[2026-11-19 18:41:54] $ scripts/vm.sh root systemctl reload php8.3-fpm
[2026-11-19 18:41:54] (mã thoát 0)

[2026-11-19 18:41:56] $ scripts/vm.sh root ls -l /run/php/tpan-a_example_com_7653e4.sock
ls: cannot access '/run/php/tpan-a_example_com_7653e4.sock': No such file or directory
[2026-11-19 18:41:56] (mã thoát 2)
...
[2026-11-19 18:41:56] $ scripts/vm.sh root systemctl restart php8.3-fpm
[2026-11-19 18:41:57] (mã thoát 0)

[2026-11-19 18:41:58] $ scripts/vm.sh root ls -l /run/php/tpan-a_example_com_7653e4.sock
srw-rw---- 1 www-data www-data 0 Nov 19 18:41 /run/php/tpan-a_example_com_7653e4.sock
```

Reload không tạo lại, restart có. Đó là lý do Step sửa là restart (mục 4, "reload và restart").

#### 03: sửa hỏng vhost của site b

Kịch bản thêm dòng `tpan_fault_invalid on;` vào vhost, không reload. `nginx -t` báo `unknown directive "tpan_fault_invalid"`, site b vẫn `200` vì nginx đang chạy bản đã nạp từ trước. Health:

```
  FAIL  web	(máy, stack)	config.nginx:nginx	Cấu hình nginx có lỗi: 2026/11/19 18:42:05 [emerg] 20205#20205: unknown directive "tpan_fault_invalid" in /etc/nginx/sites-enabled/tpan-b.example.com.conf:53  [sửa tự động: web.rewrite]
  FAIL  web	b.example.com	drift:/etc/nginx/sites-available/tpan-b.example.com.conf	File /etc/nginx/sites-available/tpan-b.example.com.conf khác bản tpan đã ghi (có thể bị sửa tay)  [sửa tự động: file.rewrite]
```

Kế hoạch của `web.rewrite` (từ `--fix --dry-run`):

```
Sửa config.nginx:nginx (web.rewrite)
Kế hoạch: Ghi lại các file cấu hình nginx của tpan
1 bước sẽ chạy, 4 bước đã đúng sẽ bỏ qua, 0 bước có thể gián đoạn dịch vụ.

 1. [đã đúng, bỏ qua] Ghi /etc/nginx/sites-available/tpan-default.conf  (file:/etc/nginx/sites-available/tpan-default.conf)
 2. [đã đúng, bỏ qua] Bật /etc/nginx/sites-enabled/tpan-default.conf → /etc/nginx/sites-available/tpan-default.conf  (symlink:/etc/nginx/sites-enabled/tpan-default.conf)
 3. [đã đúng, bỏ qua] Bật /etc/nginx/sites-enabled/tpan-a.example.com.conf → /etc/nginx/sites-available/tpan-a.example.com.conf  (symlink:/etc/nginx/sites-enabled/tpan-a.example.com.conf)
 4. [sẽ chạy] Ghi vhost nginx cho b.example.com  (file:/etc/nginx/sites-available/tpan-b.example.com.conf)
 ...
    - tpan_fault_invalid on;
```

Để ý: không có bước "Ghi vhost nginx cho a.example.com". File của site a đang là bản sửa tay đã nhận ở 2.4, nên `dropAccepted` bỏ bước đó (mục 3.7). Sau khi sửa, kịch bản kiểm: `bản sửa tay đã nhận của a.example.com còn nguyên sau web.rewrite (chỉ file hỏng được ghi lại)`.

#### 04: `chmod 644 wp-config.php` của site a

`stat` ra `644 tp_a_example_com_7653e4:www-data`. Health: `FAIL wordpress a.example.com wp.config:a.example.com wp-config.php sai: quyền là 0644, phải là 0600 [sửa tự động: wp.config.perm]`. Step sửa chạy chmod dưới user của site:

```
    $ runuser -u tp_a_example_com_7653e4 -- chmod 0600 -- /var/www/a.example.com/public/wp-config.php
Bắt đầu job 625ec5ad222837f0
```

Sau đó `stat` ra `600 tp_a_example_com_7653e4:www-data`.

#### 05: khoá user DB của site a

Đọc trạng thái khoá ngay sau khi phá:

```
$ scripts/vm.sh root mariadb -N -B -e SELECT User, Host, JSON_EXTRACT(Priv, '$.account_locked'), JSON_VALUE(Priv, '$.account_locked') FROM mysql.global_priv WHERE User = 'tp_a_example_com_7653e4'
tp_a_example_com_7653e4	localhost	true	1
```

Site trả `500` với trang "Error establishing a database connection". Health:

```
  FAIL  db	a.example.com	db.access:a.example.com	User DB của site không đăng nhập được hoặc không thấy DB: Error: Failed to get current SQL modes. Reason: ERROR 4151 (HY000): Access denied, this account is locked  [sửa tự động: db.unlock]
  FAIL  e2e	a.example.com	http.site:a.example.com	Site trả 500: PHP hoặc WordPress báo lỗi
```

Sửa tự động:

```
    $ mariadb -N -B  (stdin: ALTER USER 'tp_a_example_com_7653e4'@'localhost' ACCOUNT UNLOCK;)
Bắt đầu job dec4cd2127298b2b
- Mở khoá user DB tp_a_example_com_7653e4 của site a.example.com ...
  xong
```

Đọc lại: `tp_a_example_com_7653e4	localhost	false	0`.

#### 06: đầy đĩa

Kịch bản tạo file giả 15 GB:

```
Đã tạo /var/tmp/tpan-fault-fill (15051919033 byte).
Filesystem      Size  Used Avail Use% Mounted on
/dev/sda1        19G   17G  1.7G  91% /
```

Health: Fail duy nhất là `disk.usage:/` ở chặng `machine` (`Ổ đĩa chứa / đã dùng 91.0%, gần đầy`), chặng `tpan` không Fail (vẫn còn 1,7 GB, trên ngưỡng 200 MiB của `disk.free:/var/lib/tpan`). `--fix --dry-run` in `Không có phép kiểm nào sửa tự động được.` Đây là chỗ cố ý khác mục 6.7 (mục 5). `restore` xoá file giả, health tiếp theo in `sự cố vừa đóng: #15 disk.usage:/ (pass)`.

Cuối nửa nginx: 15 sự cố, cả 15 `đóng(pass)`; log của `tpan.service` có 43 dòng, `level=ERROR: 0; "database is locked": 0`.

#### Apache

Máy ảo reset, `stack install --web apache`, setup, một site. Kịch bản 03 trên Apache: `apache2ctl configtest` báo `AH00526: Syntax error on line 62`, health mở `#1 config.apache:apache2, #2 drift:/etc/apache2/sites-available/tpan-a.example.com.conf`, sửa tự động xong thì cả hai `đóng(pass)`.

### 2.6 Số request và log trực tiếp

`GET /api/v1/sites/a.example.com/traffic` sau các kịch bản trên:

```
{
  "requests": 70,
  "errors_5xx": 15,
  "since": "2026-11-19T17:43:29.011748622+07:00"
}
```

Log không tách được 15 lỗi 5xx đó ra từng request; nhiều khả năng là các lần site trả 502 và 500 trong kịch bản 01, 02, 05 (mình suy ra, chưa đếm lại).

Log trực tiếp: kịch bản mở `GET /api/v1/sites/a.example.com/log?file=access` bằng `curl -sN -m 8` ở nền, gọi site 3 lần, rồi xem tiến trình trong máy ảo:

```
[2026-11-19 18:43:29] $ scripts/vm.sh exec pgrep -a tail
[2026-11-19 18:43:29] (mã thoát 1)
...
[2026-11-19 18:43:31] $ scripts/vm.sh exec pgrep -a tail
23170 tail -n 100 -F /var/log/tpan/sites/a.example.com/access.log
```

Phần cuối luồng SSE:

```
event: line
data: {"line":"<mac> - - [19/Nov/2026:18:43:31 +0700] \"GET /?e2e-m4-1795088608-sse-1 HTTP/1.1\" 200 69723 \"-\" \"curl/8.7.1\""}
event: line
data: {"line":"<mac> - - [19/Nov/2026:18:43:32 +0700] \"GET /?e2e-m4-1795088608-sse-2 HTTP/1.1\" 200 69723 \"-\" \"curl/8.7.1\""}
event: line
data: {"line":"<mac> - - [19/Nov/2026:18:43:32 +0700] \"GET /?e2e-m4-1795088608-sse-3 HTTP/1.1\" 200 69723 \"-\" \"curl/8.7.1\""}
Tổng: 73 dòng "event: line"
```

Mình suy ra 73 là 70 dòng cũ (file chưa đủ 100 dòng, khớp với `"requests": 70` đo ngay trước đó) cộng 3 request mới; log không in số dòng của file để đối chiếu. Hai giây sau khi `curl` đóng, `pgrep -a tail` lại ra mã 1: không còn `tail` nào.

File log chưa có thì sao. Trước hết là `tail` một mình, không và có `-F`:

```
[2026-11-19 18:43:39] $ scripts/vm.sh exec tail -n 1 /var/log/tpan/khong-co-file-nay.log
tail: cannot open '/var/log/tpan/khong-co-file-nay.log' for reading: No such file or directory
[2026-11-19 18:43:39] (mã thoát 1)

[2026-11-19 18:43:39] $ scripts/vm.sh exec timeout 3 tail -n 1 -F /var/log/tpan/khong-co-file-nay.log
tail: cannot open '/var/log/tpan/khong-co-file-nay.log' for reading: No such file or directory
[2026-11-19 18:43:42] (mã thoát 124)
```

Không `-F` thì thoát ngay, mã 1. Có `-F` thì ngồi chờ tới khi `timeout` giết sau 3 giây (mã 124 là mã của `timeout`). Rồi kịch bản tạm đổi tên `php-error.log` của site a và mở luồng `?file=php`:

```
event: line
data: {"line":"tail: cannot open '/var/log/tpan/sites/a.example.com/php-error.log' for reading: No such file or directory"}
```

Không có sự kiện `end`: `tail -F` ngồi chờ file xuất hiện. Đây là chỗ đã chấp nhận (ghi ở `docs/thay-doi/M4.md`).

### 2.7 Bộ hẹn giờ bỏ vòng khi CLI đang giữ khoá

Kịch bản chạy `tpan site add c.example.com` ở nền, chờ tới khi khoá toàn cục bị giữ, rồi `systemctl restart tpan`. Journal:

```
2026-11-19T18:43:50+07:00 tpan-m4 tpan[23864]: 2026/11/19 18:43:50 INFO tpan đang chạy listen=0.0.0.0:8443 tls=true demo=false version=f569de3
2026-11-19T18:43:50+07:00 tpan-m4 tpan[23864]: 2026/11/19 18:43:50 INFO health: đang có job, bỏ vòng này full=true domain=""
```

Job tạo site vẫn `thành công`. Bộ hẹn giờ không chen vào giữa.

### 2.8 Chưa chạy

- Màn Sự cố và log trực tiếp trên trình duyệt thật: e2e của M4 chỉ gọi API bằng `curl`. Để xem: `make dev-web` với `make dev-api`, hoặc mở panel của máy ảo và vào `/su-co`.
- Vòng nhẹ 5 phút và vòng đầy đủ mỗi giờ chạy đúng giờ: e2e chỉ thấy vòng đầu sau restart (2.1). Unit test của `Scheduler` dùng nhịp vài mili giây. Để tự xem: để panel chạy 10 phút rồi đọc `health.scheduler_beat` như ở 2.1.
- Thử lại sau 30 giây (`BusyRetry`) trên máy ảo: e2e không chờ đủ 30 giây sau 2.7 rồi đọc lại `health.last_run`.
- Sau `tpan site rm`, sự cố của site đã xoá đóng với lý do `gone`: e2e xoá `c.example.com` trước khi nó có sự cố nào. Hiện chỉ có unit test (`TestPruneResults`, `TestRunPruneKhiSiteMat`).

## 3. Đi qua mã theo thứ tự chạy

Thứ tự: một vòng health bắt đầu từ CLI hoặc bộ hẹn giờ, Runner gom phép kiểm, chạy, ghi kết quả và mở đóng sự cố, rồi (nếu có `--fix` hoặc người bấm nút) dựng Plan sửa, engine chạy, `managed_files` cập nhật. Sau đó là các phần đứng riêng: số request, log trực tiếp, kịch bản lỗi, giao diện.

### 3.1 Hai cửa vào: `cmd/tpan/health.go` và `health.Scheduler`

`healthCmd` kiểm cờ (`--dry-run`, `--yes` chỉ đi cùng `--fix`), kiểm root, mở DB, rồi dựng `health.Runner` và gọi `healthRun.run`. Dòng chính của nó là `h.runner.Run(ctx, health.Options{Domain: h.domain, Full: true})`, rồi `printHealthReport` khi không có `--json`.

CLI luôn chạy đầy đủ (`Full: true`): người gõ tay muốn câu trả lời đầy đủ. CLI cũng ghi kết quả và mở đóng sự cố như bộ hẹn giờ, nên e2e không phải chờ 5 phút. `healthExit` trả mã 1 khi có Fail hoặc stack chưa cài.

Cửa thứ hai: `server.Run` trong `internal/server/server.go` dựng `health.Scheduler{Runner: s.healthRunner(), Busy: s.healthBusy}` rồi chạy `s.sched.Run(sctx)` trong một goroutine. Khi `tpan serve` dừng, nó huỷ `sctx` rồi chờ vòng đang dở xong (`<-done`) trước khi đóng DB.

`Scheduler.Run` trong `internal/health/health.go` là một vòng `select` chờ năm thứ:

```go
		select {
		case <-ctx.Done():
			return
		case <-retry:
			retry = nil
			if !s.round(ctx, Options{Full: true}) {
				retry = time.After(busyRetry)
			}
		case <-fullT.C:
			...
		case <-lightT.C:
			...
		case <-kick:
			...
		}
```

Trước vòng `for` nó đã chạy một vòng đầy đủ ngay. `lightT` là ticker 5 phút, `fullT` là ticker 1 giờ. `retry` là kênh của `time.After(30 * time.Second)`, chỉ khác `nil` khi vòng đầy đủ vừa bị bỏ vì đang có job (mục 5, "chỗ sai: không thử lại"). Kênh `nil` trong `select` không bao giờ sẵn sàng, nên `retry = nil` là cách tắt nhánh đó.

`kick` nhận tín hiệu từ `Scheduler.Kick(domain)`. Server gọi `s.kick(o.kick)` ở cuối mỗi job (`internal/server/api_job.go`), sau khi nhả chỗ "đang có job". `Kick` gộp yêu cầu: hai domain khác nhau đang chờ thì đổi thành `""` (kiểm mọi thứ). Kênh có đệm 1, nên gọi `Kick` 10 lần liền chỉ ra một vòng.

`Scheduler.round` làm hai việc theo thứ tự cố ý:

1. Ghi `health.scheduler_beat` trước. Phép kiểm `tpan.scheduler:health` hỏi "bộ hẹn giờ còn sống không"; một job dài làm bỏ nhiều vòng liền mà không ghi nhịp thì nó cảnh báo sai là `tpan serve` đã chết.
2. Hỏi `Busy()`. Đang có job thì ghi `health: đang có job, bỏ vòng này` và trả `false`.

`Server.healthBusy` trong `internal/server/api_health.go` trả `true` khi tiến trình này đang chạy job (`a.busy`), hoặc khi không lấy được khoá toàn cục `/run/tpan/tpan.lock` trong 50 ms (tiến trình CLI đang giữ). Lấy được thì nhả ngay: health không giữ khoá trong lúc kiểm (mục 5).

### 3.2 `Runner.Run` và `Runner.collect`: gom phép kiểm

`Runner.Run` gọi `r.collect(ctx, o)` để lấy danh sách `planned{target, checks}`, chưa chạy gì. `collect` trong `internal/health/collect.go` gom theo đối tượng:

- `""` (máy và stack): `core.Stack.Checks` (bộ phép kiểm stack của M1, cùng định nghĩa với kiểm chứng sau khi cài) cộng `checks.Memory()`.
- Mỗi site `active`: `core.Sites.Checks`, tức `siteChecks` của M2 cộng phép kiểm cô lập với từng site khác.
- Mỗi dòng `managed_files`: `checks.Drift(f.Path, f.SHA256, checks.DriftStage(f.Path))`, gắn vào site sở hữu file (`fileOwner`) hoặc vào `""`. Bỏ `.htaccess`:

```go
	for _, f := range files {
		// .htaccess: tpan ghi bản đầu, nhưng WordPress tự ghi lại đoạn giữa
		// "# BEGIN WordPress" và "# END WordPress" mỗi khi lưu permalink, nên so
		// sha256 cả file là báo lệch giả (thấy trên máy ảo Apache, e2e M4).
		...
		if path.Base(f.Path) == ".htaccess" {
			continue
		}
		add(fileOwner(f.Path, all), checks.Drift(f.Path, f.SHA256, checks.DriftStage(f.Path)))
	}
```

- `store.TargetTPAN` (`"tpan"`): `Runner.selfChecks`, trừ khi `NoSelf` (chế độ `--demo`, nơi không có chứng chỉ hay `/etc/tpan`).

`Options.Domain` khác rỗng thì `collect` chỉ trả phép kiểm của site đó, sau khi đưa domain qua `validate.ParseDomain` (domain đến từ CLI hoặc API là dữ liệu từ ngoài).

`fileOwner` so đường dẫn với `site.Layout` của từng site, kể cả site đang ở thùng rác. Nhờ vậy lệch vhost của site a hiện trên ga "web" của site a trên bản đồ, như ở 2.2 (`target: a.example.com`). `DriftStage` chọn chặng theo thư mục: `/etc/nginx/`, `/etc/apache2/` là `web`; `/etc/php/` là `php`; `/var/www/` là `wordpress`; `/etc/tpan/` và `/etc/systemd/system/tpan*` là `tpan`; còn lại là `machine`. 2.1 có đủ các trường hợp đó.

### 3.3 Chạy: `checks.IsHeavy`, `checks.RunAll`, `core.AutoFixName`

Trong `Runner.Run`, với từng nhóm:

```go
		cs := g.checks
		if !o.Full {
			cs = nil
			for _, c := range g.checks {
				if !checks.IsHeavy(c) {
					cs = append(cs, c)
				}
			}
		}
		for _, res := range checks.RunAll(ctx, r.Sys, cs) {
			if seen[res.ID] || (!o.Full && isHeavy(res.ID)) {
				continue
			}
			seen[res.ID] = true
```

`checks.Heavy` (trong `internal/checks/run.go`) bọc một phép kiểm vào kiểu `heavyCheck`; `IsHeavy` nhận ra kiểu đó trước khi chạy. Ở M4 chỉ `WPChecksums` được bọc, trong `SiteChecks` của `internal/checks/site.go`. Lần nhẹ bỏ hẳn nó, không tốn 1,7 giây mỗi site mỗi 5 phút. Dòng `isHeavy(res.ID)` (so tiền tố `wp.checksums:` của ID) là lọc lần hai sau khi chạy; với mã hiện tại nó không bỏ thêm gì (mục 5).

`checks.RunAll` chạy tuần tự. Chú thích nói vì sao: các phép kiểm dùng chung một máy, song song chỉ thêm tải lên máy đang có chuyện.

`seen` giữ mỗi ID một lần: phép kiểm cô lập `isolation:a.example.com:b.example.com` nằm trong bộ của cả site a lẫn site b. Ghi cả hai thì cột `target` của dòng đó đổi qua lại mỗi lần chạy.

Kết quả không đạt được điền `res.AutoFix = core.AutoFixName(res)`. `AutoFixName` trong `internal/core/health.go` là hàm thuần: đọc phần trước dấu `:` của ID và trạng thái, trả tên cách sửa. Bảng:

| ID bắt đầu bằng | Tên cách sửa |
|---|---|
| `service.active` | `service.start` |
| `php.socket` | `php.restart` |
| `drift` | `file.rewrite` |
| `config.nginx`, `config.apache` | `web.rewrite` |
| `wp.config` | `wp.config.perm` |
| `db.access` | `db.unlock` |

Loại khác (`disk.usage`, `http.site`...) trả rỗng: giao diện không hiện nút "Sửa tự động".

### 3.4 Các phép kiểm mới: `internal/checks/health.go`

Gói `checks` vẫn chỉ import `system` và `i18n`. Thứ cần DB (toàn vẹn, job treo, nhịp bộ hẹn giờ) được đưa vào bằng hàm, `Runner.selfChecks` nối chúng với store. Nhờ vậy gói `checks` test được với `FakeSystem` mà không cần SQLite.

`Drift` đọc file qua `sys.ReadFile`, tính sha256 bằng Go và so với giá trị trong `managed_files`:

```go
		sum := sha256.Sum256(data)
		got := hex.EncodeToString(sum[:])
		ev.Output = got + "  " + p + "\nmong đợi / expected: " + want
		if got == want {
			return b.pass(i18n.T("File "+p+" đúng như tpan đã ghi", "File "+p+" is as tpan wrote it"), ev)
		}
		return b.bad(Fail,
			i18n.T("File "+p+" khác bản tpan đã ghi (có thể bị sửa tay)", ...),
			...
```

Evidence ghi lệnh là `sha256sum <path> (tpan)`: chữ `(tpan)` nói thật rằng tpan tự tính bằng Go chứ không chạy `sha256sum`. Dòng cuối của cách sửa tay mới là lệnh thật để bạn tự kiểm lại. File không còn cũng là Fail. Mã mong đợi không đúng dạng 64 ký tự hex thì `Unknown` ("chưa kiểm được"), không báo lệch giả.

Các phép kiểm khác cùng file, mỗi cái một câu:

- `Memory` đọc `/proc/meminfo`, dùng `MemAvailable` (không `MemFree`, vì `MemFree` không tính page cache mà kernel trả lại được). Dưới 10% cảnh báo, dưới 5% lỗi, swap dùng quá 50% cảnh báo.
- `FreeSpace` (`disk.free:/var/lib/tpan`): chỗ trống tuyệt đối dưới 200 MiB là lỗi. Dùng số tuyệt đối chứ không phần trăm để kịch bản 06 (đĩa 91%) chỉ làm chặng `machine` lỗi, như 2.5 cho thấy.
- `PanelCert` đọc chứng chỉ PEM bằng `crypto/x509`, dưới 14 ngày cảnh báo, hết hạn lỗi.
- `FileMode` (`tpan.perm:<path>`): quyền và chủ của `tpan.db` (0600), `key.pem` (0600), `/etc/tpan` (0700). Symlink là lỗi.
- `TPANDatabase` chạy `PRAGMA integrity_check` qua hàm `Store.IntegrityCheck`; `ok` là đạt.
- `TPANJobs` lấy job còn `running` quá 1 giờ (`Store.StuckJobs`).
- `TPANScheduler` đọc `health.scheduler_beat`; quá 15 phút (ba lần chạy nhẹ bị lỡ) hoặc chưa bao giờ có là cảnh báo.

### 3.5 Ghi kết quả, mở và đóng sự cố: `Runner.record`, `Store.RecordResults`

`Runner.record` gọi `Store.RecordResults(ctx, target, results, now)` cho từng nhóm. Lần chạy cho một site thì giữ `target` cũ của dòng đã có (lại là chuyện phép kiểm cô lập).

`RecordResults` trong `internal/store/health.go` làm mọi việc trong một transaction (`s.inTx`): ghi kết quả và mở đóng sự cố cùng thành công hoặc cùng huỷ. Với từng kết quả, nó upsert vào `check_results`, tìm sự cố đang mở của ID đó, rồi:

```go
			switch {
			case r.Status == checks.Fail && !open:
				res, err := tx.ExecContext(ctx,
					`INSERT INTO incidents (check_id, target, stage, title_vi, title_en, opened_at, open_data)
					 VALUES (?, ?, ?, ?, ?, ?, ?)`,
					r.ID, target, string(r.Stage), r.Title.Vi, r.Title.En, fmtTime(now), data)
				...
				tr.Opened = append(tr.Opened, inc)
			case r.Status == checks.Pass && open:
				inc, err := closeIncident(ctx, tx, openID, closePass, data, now)
				...
				tr.Closed = append(tr.Closed, inc)
			}
```

Luật: Fail mà chưa có sự cố mở thì mở; Pass mà đang có thì đóng với lý do `pass`. `Warn` và `Unknown` không mở, không đóng: "chưa kiểm được" không phải bằng chứng là đã hết lỗi. Fail khi đã có sự cố mở thì giữ sự cố cũ. 2.2 và 2.3 cho thấy cả hai nhánh: `sự cố vừa mở: #1 ...` rồi `Sự cố vừa đóng: #1 ... lý do: pass`.

Cột `data` giữ JSON của cả `checks.Result` (bằng chứng, nguyên nhân, cách sửa), như `job_events` giữ `engine.Event` ở M3. Thêm trường cho `Result` không cần migration mới. `changed_at` chỉ đổi khi trạng thái đổi (`CASE WHEN check_results.status = excluded.status ...`): đó là "lỗi từ lúc nào" mà màn hình hiện.

Bảng nằm ở `internal/store/migrations/400_health.sql`: `check_results`, `incidents`, `incident_jobs`. Dòng quan trọng nhất của file:

```sql
CREATE UNIQUE INDEX incidents_open ON incidents (check_id) WHERE closed_at IS NULL;
```

Mỗi phép kiểm có nhiều nhất một sự cố đang mở, chặn ngay ở DB (mục 4).

Hai chi tiết của store đáng biết. `querier` là interface chung của `*sql.DB` và `*sql.Tx`, để `getIncident`, `closeIncident` chạy được cả trong transaction: store chỉ có một kết nối (`SetMaxOpenConns(1)`), nên trong transaction mà đọc qua `s.db` là treo, vì `s.db` chờ kết nối mà transaction đang giữ. Vì cùng lý do, `queryEach` luôn đóng `rows` trước khi trả về.

Chỉ lần chạy đầy đủ với `Domain` rỗng mới gọi `Store.PruneResults`: xoá kết quả của phép kiểm không còn (site đã xoá, file thôi được quản lý) và đóng sự cố của chúng với lý do `gone`. Lần nhẹ thiếu `wp.checksums`, lần một site thiếu mọi thứ khác; dọn lúc đó là xoá nhầm. Rồi nó ghi `health.last_run`.

### 3.6 Dựng Plan sửa: `fixable`, `Fixes.Prepare`, `Fixes.plan`

Có `--fix` thì `healthRun.fix` lấy `fixable(before)`: các kết quả không đạt có `AutoFix`, theo thứ tự của Runner (máy và stack trước site). Thứ tự đó có ích ở kịch bản 01: bật PHP-FPM trước, nên hai lần sửa socket sau thấy "đã đúng, bỏ qua" (2.5). Không có `--yes` và không có terminal để hỏi thì từ chối với mã `confirm_required` trước khi dựng Plan nào.

Mỗi kết quả đi vào `core.Fixes.Prepare` trong `internal/core/health.go`:

```go
func (f *Fixes) Prepare(ctx context.Context, r checks.Result) (*Fix, error) {
	name := AutoFixName(r)
	// r.AutoFix khác tên suy từ ID và Status thì không tin: tên sửa phải khớp
	// đúng phép kiểm, không để người gọi chọn một cách sửa cho đối tượng khác.
	if name == "" || (r.AutoFix != "" && r.AutoFix != name) {
		return nil, noAutoFix(r.ID)
	}
	plan, err := f.plan(ctx, name, r)
	if err != nil {
		return nil, err
	}
	plan.Kind = "fix." + name
	return &Fix{CheckID: r.ID, Name: name, Plan: plan, fixes: f}, nil
}
```

`Fixes.plan` trong `internal/core/fix.go` là một `switch` theo tên. Mỗi nhánh dựng Plan từ Step có sẵn, và `Verify` là đúng phép kiểm đã báo lỗi, dựng lại từ ID. Job sửa chỉ thành công khi phép kiểm đó đạt lại. Đối tượng trong ID (domain, unit, đường dẫn) không bao giờ đi thẳng vào Step:

- `php.restart`, `wp.config.perm`, `db.unlock`: domain qua `Fixes.activeSite`, phải đúng dạng chuẩn và là site `active` trong store.
- `service.start`: `Fixes.serviceStart` chỉ nhận unit thuộc stack (web server, `mariadb`, `php<v>-fpm` của stack và của từng site). `systemctl enable --now <unit bất kỳ>` bằng root là bật được thứ không phải của tpan.
- `file.rewrite`: `Fixes.managedFile` dựng lại các Step `File` của mọi site và của stack (gọi lại `installPlan` của M1, không chép danh sách), rồi tìm Step có `Path` bằng đúng đường dẫn (so `==`). Đường dẫn lạ, kể cả có `..`, không bao giờ khớp. File của `tpan setup` (unit systemd, chứng chỉ) không dựng lại được nên không có sửa tự động.
- `web.rewrite`: chặng sau.

### 3.7 `Fixes.webRewrite`, `Fixes.dropAccepted` và ba Step mới

`Fixes.webRewrite` lấy mọi Step `File` và `Symlink` dưới `/etc/nginx/` (hoặc `/etc/apache2/`) của stack, cộng vhost của mọi site. Step `File` bỏ qua file đã đúng nội dung, nên chỉ file hỏng được ghi, và mỗi lần ghi vẫn qua `nginx -t` trước khi reload (Step của M1). Rồi `Fixes.dropAccepted` bỏ các file người dùng đã nhận làm chuẩn:

```go
	for _, s := range ss {
		if x, ok := s.(*steps.File); ok {
			want, rec, err := f.Files.FileSHA256(ctx, x.Path)
			...
			if rec {
				if cur, err := f.Sys.ReadFile(x.Path); err == nil && !bytes.Equal(cur, x.Content) {
					sum := sha256.Sum256(cur)
					if strings.EqualFold(hex.EncodeToString(sum[:]), want) {
						continue
					}
				}
			}
		}
		out = append(out, s)
	}
```

Tức: file đang có khác template, nhưng sha256 của nó khớp đúng dòng trong `managed_files`, thì đó là bản sửa tay đã nhận; bỏ bước ghi nó. 2.5 (kịch bản 03) cho thấy kết quả. Vì sao có hàm này: mục 5, "chỗ sai: web.rewrite ghi đè bản đã nhận".

Ba Step mới nằm ở `internal/steps/fix.go`:

- `PHPFPMRestart`: Check là socket có và đúng là socket; Apply là `systemctl restart php<v>-fpm`; `Danger: true` (mọi site dùng bản PHP này mất PHP vài giây); không hoàn tác được và `NoUndo` nói vì sao. Socket chỉ được nhận nếu nằm ngay trong `/run/php/` và có đuôi `.sock`.
- `DBUserUnlock`: đọc trạng thái khoá từ `mysql.global_priv` (không `SHOW CREATE USER`, mục 5), `ALTER USER ... ACCOUNT UNLOCK` qua stdin của `mariadb`. Undo khoá lại chỉ khi chính Apply đã mở. User không có thì Apply từ chối: tạo lại user cần mật khẩu trong `wp-config.php`, mà tpan không đọc nó ra.
- `WPConfigPerm`: chủ sai thì root đổi bằng `sys.Chown` (không đi theo symlink), rồi `chmod 0600` chạy dưới user của site (`runuser -u <user> -- chmod`, thấy ở 2.5). Lỡ file bị tráo thành symlink giữa chừng thì user đó không chmod được file của người khác. File mất thì từ chối chứ không tạo file rỗng: `wp-config.php` rỗng làm WordPress hiện trang cài đặt, ai vào trước chiếm site.

### 3.8 Engine chạy, `managed_files` cập nhật, sự cố gắn job

`Fix.Execute` gọi `engine.New(...).Execute(ctx, x.Plan)` y như mọi thao tác khác của M1 tới M3: Check trước Apply, hoàn tác khi lỗi, kiểm chứng cuối, job vào Lịch sử. Phần M4 dựa vào nằm cuối `Execute` trong `internal/engine/engine.go`, có từ M1:

```go
	} else if j != nil {
		// Chỉ ghi file của bước đã Apply: bước bỏ qua không do tpan ghi lần này.
		for _, i := range applied {
			for _, f := range infos[i].Files {
				var err error
				if f.Delete {
					err = j.ForgetFile(r.jctx, f.Path)
				} else {
					sum := sha256.Sum256(f.New)
					err = j.RecordFile(r.jctx, f.Path, hex.EncodeToString(sum[:]), id)
				}
```

`Store.RecordFile` upsert sha256 mới vào `managed_files`. Nên sau `file.rewrite`, lần kiểm lại thấy file khớp, sự cố đóng. Nhánh `else` này chỉ chạy khi job không bị hoàn tác.

Nhận bản sửa tay đi đường khác, không qua engine (không đổi gì trên máy): `Server.handleAcceptFile` chỉ nhận đường dẫn có trong `managed_files` (`/etc/passwd` bị `400 path_invalid` ở 2.4), giành chỗ "đang có job" và khoá toàn cục (job có thể đang ghi đúng file này), rồi `Server.acceptFile` đọc file, tính sha256, gọi `RecordFile(ctx, path, sum, "manual")`. Ghi `audit`, rồi `s.kick("")`.

Gắn job vào sự cố: CLI gọi `Store.LinkIncidentJob` sau khi job xong; API gọi nó ngay khi job bắt đầu (`onStart` trong `handleFix`), để màn Sự cố thấy job sửa trong lúc nó chạy. `LinkIncidentJob` là một câu `INSERT OR IGNORE ... SELECT id ... WHERE check_id = ? AND closed_at IS NULL`: không có sự cố đang mở thì không chèn gì và không lỗi.

Cuối cùng `healthRun.fix` chạy lại `h.run(ctx)` ("Kiểm lại sau khi sửa"). Chính lần chạy này đóng sự cố, không phải job sửa.

### 3.9 API của health: `internal/server/api_health.go`

- `handleOverview` đọc `check_results` và sự cố đang mở, không chạy phép kiểm. `?refresh=1` (nút "Kiểm lại") chạy `Runner.Run` đầy đủ trước. Số liệu máy vẫn đo trước, một mình (bài học CPU% của M3), rồi mới đếm request của các site song song.
- `handleFix` đọc kết quả trong store theo `check_id`, không tin kết quả người gọi gửi lên. `?dry_run=1` trả bản xem trước; không có thì giành chỗ job và `jobStart` như tạo site ở M3, trả `202`.
- `handleIncidents`, `handleIncident`: danh sách (`state=open|all`, `limit` 1 tới 500) và một sự cố kèm kết quả hiện tại và các job.
- `handleHealthRun`: chạy ngay, đồng bộ, trả `Report`.
- `apiFixes` bọc `Sys` bằng `jobLockSystem` của M3: chờ khoá toàn cục có hạn rồi trả `409`.

### 3.10 Số request và 5xx: `internal/monitor/traffic.go`

`SiteTraffic` chạy `tail -n 50000 <access.log>` qua `sys.Run` (argv, không shell) rồi đếm từng dòng có thời điểm trong một giờ gần nhất. 50.000 dòng cố định để một site đông khách không làm tpan đọc hết file hàng GB. Đọc đủ 50.000 dòng mà dòng cũ nhất vẫn nằm trong khoảng đếm thì `Partial: true`: số thật có thể lớn hơn.

`parseCombined` lấy thời điểm trong cặp `[]` và mã trạng thái ngay sau chuỗi request trong nháy kép. Chuỗi request do khách gửi, có thể chứa nháy kép đã thoát (`\"` của Apache, `\x22` của Nginx), nên nó tìm nháy đóng bằng cách bỏ qua ký tự sau dấu `\`. Dòng không đọc được thì bỏ, không làm hỏng cả lần đếm. File chưa có (site mới) là 0 request, không lỗi: `tail` in `No such file` (cố định vì `LocalSystem` đặt `LC_ALL=C`).

### 3.11 Log trực tiếp: `handleSiteLog` tới `LiveLog`

`apiLogPath` chỉ nhận ba tên `access`, `error`, `php` và lấy đường dẫn từ `site.Layout`. Người gọi không bao giờ đưa được đường dẫn vào lệnh `tail`. `handleSiteLog` đặt header SSE, rồi:

```go
	ctx, cancel := context.WithTimeout(r.Context(), LogStreamMax)
	defer cancel()
	_, err := s.deps.Sys.Run(ctx, system.Cmd{
		Name: "tail",
		Args: []string{"-n", "100", "-F", p},
		OnOutput: func(line string) {
			// Ghi lỗi nghĩa là client đã ngắt: dừng tail luôn.
			if send("line", struct {
				Line string `json:"line"`
			}{line}) != nil {
				cancel()
			}
		},
	})
```

`OnOutput` (trường của `system.Cmd` có từ hợp đồng M1) được `LocalSystem` gọi cho từng dòng ngay khi có, tuần tự, nên chỉ một goroutine ghi vào response. `LocalSystem.Run` dùng `exec.CommandContext`: context bị huỷ (trình duyệt đóng tab, `r.Context()` huỷ theo) thì tiến trình `tail` bị giết. 2.6 thấy đúng điều đó bằng `pgrep`. `LocalSystem.Run` cũng giữ toàn bộ output trong bộ nhớ tới khi lệnh xong, nên một luồng sống tối đa `LogStreamMax` (10 phút) rồi đóng lặng lẽ; trình duyệt tự nối lại. `tail` tự thoát (lỗi thật, hoặc máy giả của `--demo`) thì gửi sự kiện `end` để giao diện thôi nối lại.

Phía trình duyệt, `subscribeSiteLog` trong `web/src/api/client.ts` mở `EventSource`, nghe sự kiện `line` và `end`. `web/src/screens/site/LiveLog.tsx` gom dòng tới dồn dập rồi vẽ 150 ms một lần, giữ 500 dòng cuối, cắt dòng dài quá 2000 ký tự, và đưa dòng vào DOM như chữ thường (không `dangerouslySetInnerHTML`): dòng log chứa URL và User-Agent do khách lạ gửi. Mỗi lần nối lại, server gửi lại 100 dòng cuối, nên `onOpen` xoá dòng cũ trong bộ đệm trước để không bị lặp.

### 3.12 `scripts/faults/` và `scripts/e2e-m4.sh`

Mỗi kịch bản nhận `break` hoặc `restore` và (trừ 06) một domain. Phần chung ở `scripts/faults/lib.sh`: đọc layout của site bằng `tpan site info --json` và bản PHP bằng `tpan stack status --json`, đọc JSON bằng `python3 -I` trên Mac (không cài `jq` trong máy ảo). Mọi lệnh trong máy ảo đi qua `scripts/vm.sh root <argv>`, không ghép chuỗi thành lệnh shell, và giá trị đọc từ tpan được kiểm bằng regex trước khi dùng vì lệnh chạy bằng root. Ví dụ 02 chỉ `rm -f` đường dẫn khớp `^/run/php/tpan-[a-z0-9_-]+\.sock$`; 05 chỉ nhận tên DB khớp `^tp_[a-z0-9_]+$` vì tên đó nằm trong câu SQL. Bảng kịch bản, chặng và cách sửa mong đợi nằm ở `scripts/faults/README.md`.

`scripts/e2e-m4.sh` chạy toàn bộ 2.1 tới 2.7 từ máy sạch, mỗi khẳng định in `=> ĐẠT` hoặc `=> KHÔNG ĐẠT`, và cuối cùng tìm xem log có lọt mật khẩu, IP hay thư mục nhà nào không (`Số bí mật đã gặp: 10; số dòng của log chứa một trong số đó: 0`).

### 3.13 Màn Sự cố

`web/src/screens/su-co/`: `IncidentList.tsx` (gọi `?state=all` một lần, sự cố đang mở trước, đã đóng sau), `IncidentDetail.tsx` (từ trên xuống: trạng thái, nguyên nhân, ba cách sửa khi còn mở, bằng chứng lúc mở, kết quả hiện tại hoặc bằng chứng lúc đóng, các job sửa), `FixPanel.tsx` (ba cách sửa). Mình chỉ đọc chú thích đầu của các file này, chưa mở màn trên trình duyệt (2.8). Sửa tự động gọi `POST /api/v1/fix?dry_run=1`, hiện bản xem trước bằng `PlanReview` của màn Duyệt Plan, rồi mới chạy và chuyển sang trang job. Phép kiểm `drift:` có thêm nút nhận bản sửa tay. "Hỏi AI" chỉ là một ghi chú, không có nút giả.

## 4. Kiến thức nền

### systemd: reload và restart, và vì sao socket không có lại

`systemctl reload <dịch vụ>` bảo tiến trình đang chạy đọc lại cấu hình mà không dừng hẳn; `restart` thì dừng rồi chạy lại từ đầu. Với PHP-FPM, reload giữ tiến trình chính và các socket nó đang nghe, nên request không bị rớt. Socket của pool là một file đặc biệt ở `/run/php/`; `rm` chỉ xoá tên file, còn PHP-FPM vẫn giữ socket đã mở. Sau reload, PHP-FPM dùng lại socket nó đang giữ chứ không tạo lại tên file đã mất, nên web server không tìm thấy socket và trả 502. Câu "dùng lại socket đang giữ" là giải thích của mình về cơ chế, chưa kiểm trực tiếp; điều đã đo là kết quả ở 2.5: reload không tạo lại, restart có.

Trong mã: `PHPFPMRestart.restartCmd` trong `internal/steps/fix.go`, và chữ "Nạp lại (reload) không tạo lại socket đã bị xoá" trong `Info().Why`.

Tự xem: đã chạy ở 2.5. Muốn thấy PHP-FPM còn giữ socket sau khi tên file mất (chưa chạy): `scripts/vm.sh root rm -f /run/php/tpan-a_example_com_7653e4.sock`, rồi `scripts/vm.sh root ss -xlp`, tìm dòng có `tpan-a`; xong nhớ `scripts/vm.sh root systemctl restart php8.3-fpm`.

### sha256 để phát hiện lệch

sha256 biến nội dung bất kỳ thành 64 ký tự hex. Đổi một byte là ra chuỗi khác hẳn, và không ai tạo được hai nội dung khác nhau có cùng sha256. Nên chỉ cần lưu 64 ký tự lúc ghi file, không cần lưu cả bản cũ, là biết sau này file có bị đổi không. Cái giá: biết là khác nhưng không biết khác chỗ nào. Muốn xem diff thì phải dựng lại nội dung tpan sẽ ghi, đó là việc `tpan health --fix --dry-run` làm (`printFileDiffs`).

Trong mã: `checks.Drift`; engine ghi sha256 vào `managed_files` sau mỗi job (3.8); `Fixes.dropAccepted` so sha256 để nhận ra bản đã nhận.

Tự xem: `scripts/vm.sh root sha256sum /etc/nginx/sites-available/tpan-a.example.com.conf` (đã chạy ở 2.2 và 2.4), so với cột `sha256` trong `managed_files`.

### SQLite: transaction, WAL, `SQLITE_BUSY` và `_txlock=immediate`

Transaction gom nhiều câu lệnh thành một: cùng ghi hoặc cùng không. WAL (write-ahead log) cho phép đọc trong lúc tiến trình khác đang ghi, nhưng mỗi lúc chỉ một người ghi. Transaction mặc định của SQLite là "deferred": bắt đầu như người đọc, tới câu ghi đầu tiên mới xin quyền ghi. Theo chú thích trong `store.go` và trang tài liệu SQLite nó dẫn, nếu tiến trình khác đã ghi xen vào giữa lúc đó thì SQLite trả `SQLITE_BUSY` ngay, không chờ `busy_timeout`. `_txlock=immediate` bảo driver mở mọi transaction bằng `BEGIN IMMEDIATE`: xin quyền ghi ngay từ đầu, và việc chờ ở đây thì `busy_timeout(5000)` có tác dụng.

Trong mã: chuỗi DSN trong `store.Open` (`internal/store/store.go`); `Store.inTx` trong `internal/store/health.go`.

Tự xem: đã chạy, gián tiếp: log dịch vụ ở cuối nửa nginx có `"database is locked": 0`. Xem cấu hình WAL (chưa chạy): `scripts/vm.sh root ls -l /var/lib/tpan/` thấy `tpan.db-wal`, `tpan.db-shm` bên cạnh `tpan.db`.

### Partial unique index: "một sự cố mở mỗi phép kiểm"

Unique index bắt một cột không có hai dòng trùng giá trị. Partial index chỉ áp cho các dòng thoả `WHERE`. `incidents_open` nói: trong các dòng `closed_at IS NULL` (sự cố đang mở), `check_id` không trùng. Sự cố đã đóng thì bao nhiêu cũng được, như #1 và #2 cùng `check_id` ở 2.4. `RecordResults` tự tìm trước rồi mới chèn, và người ghi đã được xếp hàng nhờ `_txlock=immediate`, nên bình thường index không bao giờ phải ra tay; nó là lưới cuối ở DB, phòng một chỗ ghi khác về sau quên luật.

Trong mã: `internal/store/migrations/400_health.sql`.

Tự xem (chưa chạy): sao DB ra `/tmp` bằng backup API của SQLite rồi thử chèn sự cố mở thứ hai cho cùng một phép kiểm, trên bản sao:

```bash
scripts/vm.sh root python3 -c 'import sqlite3; s=sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True); d=sqlite3.connect("/tmp/thu.db"); s.backup(d); q="INSERT INTO incidents (check_id,target,stage,title_vi,title_en,opened_at,open_data) VALUES (?,?,?,?,?,?,?)"; a=("x","","web","t","t","2026","{}"); d.execute(q,a); d.execute(q,a)'
```

Câu `execute` thứ hai phải lỗi `UNIQUE constraint failed`. Không thử trên file thật vì `tpan serve` đang dùng nó. Không `cp tpan.db` vì DB ở chế độ WAL: phần mới ghi có thể còn nằm trong `tpan.db-wal`, bản chép chỉ một file có thể thiếu cả bảng của M4; `backup` đọc đúng như một kết nối SQLite thấy.

### SSE và `tail -F`

SSE (đã gặp ở M3) là một response HTTP không kết thúc ngay; server ghi dần từng sự kiện `event:` và `data:`. `tail -n 100 -F <file>` in 100 dòng cuối rồi theo dõi file, in dòng mới khi có. `-F` khác `-f` ở chỗ theo tên file: logrotate đổi tên `access.log` và tạo file mới thì `-F` mở file mới, còn `-f` cứ theo file cũ đã đổi tên. `-F` cũng chờ khi file chưa có (2.6: không `-F` thì mã 1, có `-F` thì ngồi chờ tới khi `timeout` giết, mã 124). Đó là lý do luồng log của một file chưa có không bao giờ gửi `end`.

Trong mã: `handleSiteLog`, `subscribeSiteLog`, `LiveLog`.

Tự xem: `pgrep -a tail` trước, trong và sau khi mở luồng (đã chạy ở 2.6).

### Ticker, goroutine, `select` và context trong Go

Goroutine là một hàm chạy song song, rất nhẹ. `time.NewTicker(d)` cho một kênh nhận một giá trị mỗi `d`; `time.After(d)` cho một kênh nhận đúng một lần. `select` chờ nhiều kênh cùng lúc và chạy nhánh nào sẵn sàng trước. Context mang tín hiệu huỷ: `ctx.Done()` đóng khi bị huỷ, nên một nhánh `case <-ctx.Done(): return` là cách dừng vòng lặp. Xem thêm mục 7 và 8 của `00-go-can-ban.md`.

Trong mã: `Scheduler.Run` (3.1). Goroutine chạy nó ở `server.Run`. Context có hạn 10 phút ở `handleSiteLog` (`context.WithTimeout`).

Tự xem: `go test ./internal/health/ -run Scheduler -v` trên Mac chạy ba test `TestSchedulerChayDayDuRoiKick`, `TestSchedulerNhipNheVaBan`, `TestSchedulerThuLaiKhiVongDauBan` với nhịp rút ngắn (chưa chạy trong phiên viết tài liệu này).

### MariaDB `ACCOUNT LOCK` và `mysql.global_priv`

Từ MariaDB 10.4, một user có thể bị khoá bằng `ALTER USER '<user>'@'localhost' ACCOUNT LOCK`: mật khẩu vẫn đúng nhưng mọi lần đăng nhập mới bị từ chối với `ERROR 4151`. Thông tin của user nằm trong bảng `mysql.global_priv`, cột `Priv` là JSON; khoá `account_locked` cho biết đang khoá hay không. Trên bản 10.11 trong máy ảo, `JSON_EXTRACT` của nó ra `true`/`false` còn `JSON_VALUE` ra `1`/`0` (2.5).

Trong mã: `dbLockStateSQL`, `dbUnlockSQL`, `DBUserUnlock.lockState` trong `internal/steps/fix.go`.

Tự xem: câu `SELECT User, Host, JSON_EXTRACT(...), JSON_VALUE(...)` ở 2.5 (đã chạy).

### Định dạng log "combined"

Nginx và Apache đều ghi access log theo dạng "combined": IP khách, hai cột `-`, thời điểm trong `[]`, dòng request trong nháy kép, mã trạng thái, số byte, Referer và User-Agent trong nháy kép. Ví dụ từ 2.6:

```
<mac> - - [19/Nov/2026:18:43:31 +0700] "GET /?e2e-m4-1795088608-sse-1 HTTP/1.1" 200 69723 "-" "curl/8.7.1"
```

Mã 5xx là lỗi phía máy chủ (502: web server không nói chuyện được với PHP; 500: PHP hoặc WordPress lỗi).

Trong mã: `combinedTime` và `parseCombined` trong `internal/monitor/traffic.go`.

Tự xem (chưa chạy): `scripts/vm.sh root tail -n 3 /var/log/tpan/sites/a.example.com/access.log`.

### Khoá toàn cục bằng `flock`

`flock` là khoá trên một file mà kernel giữ hộ: tiến trình nào giữ thì tiến trình khác thử lấy sẽ phải chờ, và tiến trình chết thì khoá tự nhả. Mọi tiến trình tpan (CLI và `tpan serve`) dùng chung `/run/tpan/tpan.lock` để không hai job cùng đổi máy (M1). Health không lấy khoá để kiểm; nó chỉ thử lấy trong 50 ms để biết có ai đang giữ không.

Trong mã: `LocalSystem.Lock` trong `internal/system/local.go`; `Server.healthBusy`.

Tự xem: 2.7 (đã chạy, gián tiếp qua journal).

## 5. Cách khác đã cân nhắc, và chỗ đã làm sai

### Lệch là cảnh báo (Warn), sau đổi sang lỗi (Fail)

Lúc lập hợp đồng (commit `f3d6b3c`), lệch định là cảnh báo: máy có thể vẫn chạy đúng với bản sửa tay. Đổi ở commit `68930c6`, lý do ghi trong `docs/thay-doi/M4.md`: sự cố chỉ mở khi phép kiểm chuyển sang lỗi, mà lựa chọn "ghi lại bản của tpan hay nhận bản sửa tay" nằm ở màn Sự cố. Để cảnh báo thì người dùng không bao giờ tới được lựa chọn đó. Thêm nữa, một file cấu hình mà tiến trình root đọc bị sửa ngoài tpan cũng đáng là sự cố. Dấu vết còn lại của bản cũ: `AutoFixName` vẫn nhận `Warn` cho `drift` để kết quả cũ trong DB còn sửa được.

### Reload thay vì restart để tạo lại socket

Reload êm hơn (không rớt request), và lúc đầu cách sửa tay của phép kiểm socket gợi ý reload. Không chọn: đo trên máy ảo (2.5) thấy reload không tạo lại socket đã bị xoá, nên cả sửa tự động lẫn dòng sửa tay đều dùng restart. Cái giá: restart làm mọi site dùng bản PHP đó mất PHP vài giây, nên Step có `Danger: true` và giao diện hỏi lại.

### Đọc trạng thái khoá bằng `SHOW CREATE USER`

Đó là câu quen thuộc nhất để xem một user có `ACCOUNT LOCK` không. Không chọn: nó in cả hash mật khẩu (`IDENTIFIED BY PASSWORD '*...'`), mà engine ghi mọi dòng output vào log của job, log đó hiện trên giao diện và nằm trong DB. Câu đọc `mysql.global_priv` chỉ trả hai số.

### Giữ khoá toàn cục trong lúc health kiểm

Giữ khoá thì chắc chắn không kiểm trúng một site đang dựng dở. Không chọn: một vòng đầy đủ mất khoảng 3,7 giây, người bấm "tạo site" đúng lúc đó sẽ bị `409`. Thay vào đó `Scheduler` chỉ hỏi "có ai đang giữ không" rồi bỏ vòng nếu có, và mọi job xong thì `Kick` một vòng mới.

### Chạy phép kiểm nặng rồi bỏ kết quả, so với `checks.Heavy`

Hợp đồng ban đầu (`f3d6b3c`) viết: lần chạy nhẹ bỏ các phép kiểm có ID bắt đầu bằng `wp.checksums:`. Một `Check` chỉ biết ID sau khi chạy, nên theo cách đó vẫn phải chạy `wp core verify-checksums` rồi mới bỏ: tốn đúng thứ cần tránh. Thợ viết phép kiểm thêm `checks.Heavy`, một lớp bọc nhận ra được trước khi chạy (`4c26e34`), và Runner lọc bằng `checks.IsHeavy` (`2694d3f`). Bộ lọc theo ID (`isHeavy(res.ID)`) vẫn còn ngay sau đó, và chú thích của `Runner.Run` vẫn tả cách cũ.

### `.htaccess` trong phép kiểm lệch

tpan ghi `.htaccess` lúc tạo site Apache, nên nó có trong `managed_files`. `docs/bang-chung/M4-htaccess.log` chứng minh lệch giả: lưu permalink (`wp rewrite structure "/%postname%/" --hard`) làm WordPress ghi lại đoạn giữa `# BEGIN WordPress` và `# END WordPress`, sha256 đổi từ `323969ea...` sang `1dd027ac...`, và health báo `fail wordpress drift:/var/www/a.example.com/public/.htaccess`. Đó là việc bình thường của WordPress. Phần F của e2e không bắt được vì `wp rewrite flush --hard` không ghi `.htaccess` khi thiếu cấu hình `apache_modules` (log in `Warning: Regenerating a .htaccess file requires special configuration.`). Chọn bỏ `.htaccess` khỏi phép kiểm lệch (`397cadc`). Cách khác đã cân nhắc: chỉ so phần ngoài đoạn BEGIN/END; để sau, ghi trong chú thích `ponytail:` ở `collect.go`.

### Đĩa đầy có sửa tự động

Mục 6.7 nói mọi kịch bản lỗi phải có sửa tự động. Với đĩa đầy thì không: tpan chạy bằng root, tự xoá file nó không tạo ra là quá nguy hiểm vì không biết file nào là dữ liệu của người dùng. Chỉ có sửa tay (tìm file lớn, `apt-get clean`, dọn journal).

### Chỗ sai: `JSON_VALUE` trả `1` chứ không phải `true` (commit `b7620b2`)

Bản đầu của `dbLockStateSQL` so `JSON_VALUE(Priv, '$.account_locked') = 'true'`. Trên MariaDB 10.11 giá trị đó là chuỗi `1`, nên user đang bị khoá vẫn đọc ra "không khoá", Check của `DBUserUnlock` báo đã đúng và sửa tự động bỏ qua đúng bước cần chạy. Unit test không bắt được vì `FakeSystem` trả cái mã mình đoán. Phát hiện bằng cách đo trên máy ảo (câu `SELECT` ở 2.5 in cả `JSON_EXTRACT` lẫn `JSON_VALUE`). Sửa: nhận cả `'1'` lẫn `'true'`, thêm `TestDBLockStateSQLAcceptsOne`.

### Chỗ sai: `web.rewrite` ghi đè bản đã nhận (commit `04856ce`)

Bản đầu của `webRewrite` ghi lại mọi file web khác template. Kịch bản e2e: nhận bản sửa tay của site a (2.4) rồi phá vhost site b (kịch bản 03). `web.rewrite` ghi đè luôn file của site a, tức xoá đúng lựa chọn người dùng vừa làm; và theo chú thích của `dropAccepted`, bước đó còn chạy `nginx -t` trong lúc file hỏng thật của site b chưa được ghi lại, nên cả job lỗi và hoàn tác. Phát hiện ở e2e M4. Sửa: `dropAccepted`. Hệ quả ghi ở `docs/thay-doi/M4.md`: người dùng đã nhận một file hỏng làm chuẩn thì sửa tự động để nguyên nó, job lỗi ở kiểm chứng, phải sửa tay.

### Chỗ sai: `tpan serve` chết vì `SQLITE_BUSY` lúc setup (commit `f371f6c`)

`tpan setup` bật `tpan.service` trong lúc chính nó vẫn đang ghi sự kiện job vào DB. `tpan serve` khởi động, chạy migration trong transaction deferred, gặp `database is locked` ngay, chết; setup thấy dịch vụ không lên và hoàn tác cả job. Phát hiện ở e2e M4 trên máy ảo. Sửa: `_txlock=immediate` cho mọi transaction. Kiểm lại: e2e M3 chạy lại trên máy ảo M4 vẫn xanh (146 khẳng định đạt, theo `docs/thay-doi/M4.md`).

### Chỗ sai: không thử lại vòng bị bỏ (commit `397cadc`)

Vòng đầu tiên khi `tpan setup` bật dịch vụ luôn bị bỏ vì setup còn giữ khoá (2.1). Không thử lại thì panel trống kết quả tới 5 phút, và `wp.checksums` trống tới 1 giờ. Thấy ở e2e M4. Sửa: `BusyRetry` 30 giây.

### Chỗ sai: `--fix --dry-run` không in diff (commit `38f9ab1`)

Phép kiểm lệch bảo người dùng chạy `tpan health --fix --dry-run` "để xem diff", nhưng bản đầu chỉ in "sửa <file>". Sửa: `printFileDiffs` và `lineDiff` trong `cmd/tpan/health.go`. `lineDiff` cố ý đơn giản: bỏ phần đầu và phần cuối giống nhau rồi in cả khúc giữa (chú thích `ponytail:` nói khi nào cần thay bằng thuật toán diff đầy đủ).

## 6. Tự kiểm

1. Socket của pool bị xoá. Bạn gõ `systemctl reload php8.3-fpm` như dòng "sửa tay" của bản đã chạy trong M4.log. Site có hết 502 không? Chứng minh trên máy ảo.
2. Vì sao lệch là Fail chứ không Warn, khi máy có thể vẫn chạy đúng với bản sửa tay?
3. Hai vòng health liền nhau cùng thấy `php.socket:a.example.com` Fail. Có hai sự cố không? Kể hai chỗ chặn, một trong mã Go và một trong DB.
4. Bạn đã nhận bản sửa tay vhost của site a làm chuẩn, rồi vhost site b bị hỏng. Bấm "Sửa tự động" cho `config.nginx:nginx`. File của site a có bị ghi đè không? Hàm nào quyết định?
5. Một script gửi `POST /api/v1/fix` với `check_id` là `service.active:ssh`. Chuyện gì xảy ra? Còn `check_id` là `drift:/etc/passwd`?
6. Bạn đóng tab đang xem log trực tiếp. Tiến trình `tail` trên máy còn không? Chỉ ra dòng mã làm việc đó, và lệnh để kiểm trên máy ảo.
7. Vì sao `DBUserUnlock` không dùng `SHOW CREATE USER`? Và vì sao câu SQL so `IN ('1', 'true')` mà không so `= 'true'`?
8. `tpan serve` khởi động đúng lúc `tpan setup` còn giữ khoá. Vòng health đầu tiên ra sao, và khi nào panel có kết quả?
9. Vì sao kịch bản đầy đĩa không có sửa tự động, khi mục 6.7 nói mọi kịch bản phải có?
10. Một site nhận 80.000 request trong một giờ. Số request tpan báo là bao nhiêu, và giao diện biết số đó thiếu bằng cách nào?

## Đáp án

1. Không. 2.5 đã đo trên máy ảo: sau `scripts/vm.sh root rm -f /run/php/tpan-a_example_com_7653e4.sock` và `scripts/vm.sh root systemctl reload php8.3-fpm`, `ls -l` vẫn ra `No such file or directory`; chỉ `systemctl restart php8.3-fpm` mới làm socket có lại (`srw-rw---- 1 www-data www-data`). Vì vậy sửa tự động `php.restart` dùng restart. Dòng sửa tay của bản đã chạy trong M4.log còn gợi ý reload (thấy trong output ở 2.5: `systemctl reload 'php*-fpm.service'`); đọc lại log thì thấy mâu thuẫn đó, nên commit `8e90056` đổi dòng sửa tay sang `systemctl restart`. Restart cũng đủ cho trường hợp còn lại mà dòng đó nhắm tới (file pool vừa được ghi lại bằng `tpan site add`), chỉ kém reload ở chỗ làm các site dùng chung bản PHP gián đoạn vài giây.
2. Sự cố chỉ mở khi phép kiểm chuyển sang Fail (luật của `RecordResults`). Lựa chọn "ghi lại bản của tpan" hay "nhận bản sửa tay làm chuẩn" nằm trên màn Sự cố. Lệch là Warn thì không có sự cố, người dùng không tới được lựa chọn đó. Thêm nữa: file đó do tiến trình root đọc (nginx, PHP-FPM, MariaDB), bị sửa ngoài tpan thì đáng để người xem. Commit `68930c6`.
3. Không. Trong Go: `RecordResults` tìm `SELECT id FROM incidents WHERE check_id = ? AND closed_at IS NULL` trước, có rồi thì nhánh `Fail && !open` không chạy. Trong DB: `CREATE UNIQUE INDEX incidents_open ON incidents (check_id) WHERE closed_at IS NULL` từ chối dòng mở thứ hai. Giữa hai vòng chạy song song (CLI và bộ hẹn giờ) thì `_txlock=immediate` xếp hàng hai transaction, nên cái sau thấy sự cố cái trước vừa mở. 2.5 có khẳng định `không check_id nào có hai sự cố mở` sau mỗi kịch bản.
4. Không. `Fixes.dropAccepted` bỏ Step ghi file nào mà nội dung đang có khác template nhưng sha256 khớp dòng trong `managed_files`. Bản đã nhận có `job_id` `manual` và sha256 của chính nội dung đang có, nên bị bỏ khỏi Plan. 2.5 thấy kế hoạch không có bước "Ghi vhost nginx cho a.example.com", và dòng sửa tay còn nguyên sau đó.
5. `handleFix` đọc kết quả trong store theo `check_id`. Không có kết quả nào tên `service.active:ssh` (stack không kiểm `ssh`), nên trả `404 check_not_found`. Kể cả có, `Fixes.serviceStart` chỉ nhận unit thuộc stack, unit lạ thì `no_auto_fix`. Với `drift:/etc/passwd`: không có trong `managed_files` nên cũng không có kết quả, `404`; và `Fixes.managedFile` chỉ khớp đường dẫn bằng đúng đường dẫn của một Step `File` tpan tự dựng lại được.
6. Không còn. `handleSiteLog` chạy `tail` với `ctx` dẫn từ `r.Context()`; trình duyệt đóng kết nối thì context huỷ, `LocalSystem.Run` dùng `exec.CommandContext` nên giết `tail`. Ghi vào response lỗi thì `OnOutput` gọi `cancel()`, cũng dẫn tới cùng chỗ. Trên máy ảo: `scripts/vm.sh exec pgrep -a tail` trong lúc mở luồng thấy `tail -n 100 -F /var/log/tpan/sites/a.example.com/access.log`, đóng rồi chạy lại thì mã 1, không còn gì (đã chạy ở 2.6).
7. `SHOW CREATE USER` in cả hash mật khẩu, mà engine ghi mọi dòng output vào log của job (hiện trên giao diện, nằm trong DB). Đọc `mysql.global_priv` chỉ ra hai số. `IN ('1', 'true')` vì MariaDB 10.11 trả `1` cho `JSON_VALUE` của JSON `true` (đo ở 2.5); bản đầu so `= 'true'` nên user đang khoá vẫn đọc ra "không khoá" và sửa tự động bỏ qua bước mở khoá (commit `b7620b2`).
8. `Scheduler.round` ghi nhịp, thấy `healthBusy()` trả `true` (không lấy được `/run/tpan/tpan.lock` trong 50 ms), ghi `health: đang có job, bỏ vòng này` và trả `false`. `Scheduler.Run` đặt `retry = time.After(30 giây)`; nếu khi đó khoá đã nhả thì vòng đầy đủ chạy. Journal ở 2.1 có đúng dòng đó lúc 18:39:42. Không có `BusyRetry` thì phải chờ nhịp nhẹ 5 phút, và `wp.checksums` tới 1 giờ (commit `397cadc`). Việc thử lại sau 30 giây chưa được e2e đo trên máy ảo (2.8).
9. Sửa tự động chạy bằng root. Để giải phóng đĩa phải xoá file, mà tpan không biết file lớn nào là rác và file nào là dữ liệu của người dùng (backup, upload, DB). Xoá nhầm là mất dữ liệu không lấy lại được. Nên chỉ có sửa tay; kịch bản 06 tự dọn file giả của nó ở bước `restore` (`docs/thay-doi/M4.md`).
10. `SiteTraffic` chỉ đọc 50.000 dòng cuối (`trafficTailLines`), nên đếm được tối đa 50.000. Dòng cũ nhất trong 50.000 dòng đó vẫn nằm trong một giờ gần nhất, nên `Partial` là `true`. Bản đồ tuyến ở Bảng điều khiển (`RouteMap.tsx`) đọc `traffic.partial` và ghi số đó là "ít nhất". Mới kiểm bằng unit test (`TestSiteTrafficPartial`), chưa thử với log thật lớn như vậy trên máy ảo.
