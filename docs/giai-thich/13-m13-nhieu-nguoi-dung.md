# M13: nhiều người dùng, gói, tạm khoá, DB rời

Đọc kèm: `02-m2-site-wordpress.md` (user Linux `tp_<slug>`, pool, vhost), `03-m3-giao-dien-va-api.md` (API, phiên, cookie, CSRF), `04-m4-health.md` (phép kiểm, sự cố, `Unknown`), `09-m9-backup-va-cron.md` (cron của site), `10-m10-chuyen-web-server.md` (chuyển nginx ⇄ apache), `12-m12-phien-ban-va-cap-nhat.md` (`self-update`, `midCheck`), và `docs/KE-HOACH.md` mục 6.12 cùng phần "Đã chốt khi làm M13" ngay dưới nó (13 quyết định của đợt 0, bảng luật của từng route, chỗ khác đặc tả, việc còn hở). File `docs/thay-doi/M13.md` từng giữ phần đó đã được gộp vào KE-HOACH.md, nên mọi chỗ dưới đây trỏ về KE-HOACH.md.

## 1. Mốc này làm được gì

- Một máy có nhiều tài khoản panel: vai `admin` (thấy và làm mọi thứ như trước) và vai `user` (khách hosting, chỉ thấy site, DB, backup, job, sự cố của chính mình). Đăng nhập bằng tên và mật khẩu; để trống tên thì hiểu là `admin` như bản cũ.
- Gói hạn mức (`tpan package add goi_nho --max-sites 2 --max-dbs 1`): tạo site hay DB thứ ba vượt gói thì bị từ chối kèm lý do, máy không đổi gì.
- Tạm khoá một site (vhost trả 403 kèm trang "Site đang tạm khoá", pool PHP và cron tắt), một DB (`ACCOUNT LOCK` rồi cắt kết nối), hay cả một user (khoá mọi thứ của họ, huỷ phiên). Mở khoá user chỉ mở lại đúng những gì bị khoá vì user.
- DB rời (`tpan db add d1 --owner khach_a` ra DB `khach_a_d1`) có đổi charset, đổi mật khẩu, tạm khoá, thùng rác.
- Nâng một máy đang chạy v0.1.1 lên bản này giữ nguyên mật khẩu quản trị, phiên đang mở và site cũ (site cũ thuộc `admin`).

## 2. Chạy thử

Mọi output dưới đây lấy nguyên văn từ hai file:

- `docs/bang-chung/M13.log`: `scripts/e2e-m13.sh chinh` ngày 20/11/2026, 17:20 tới 17:28, từ máy sạch, dòng cuối `TỔNG KẾT: 293 khẳng định đạt, 0 khẳng định không đạt`.
- `docs/bang-chung/M13-nang-cap.log`: `scripts/e2e-m13.sh nang-cap`, 17:17 tới 17:20, dòng cuối `TỔNG KẾT: 64 khẳng định đạt, 0 khẳng định không đạt`.

Những điều cần biết khi đọc:

- Máy ảo là `tpan-test` (Multipass, Ubuntu 24.04, arm64). Mọi lệnh gõ trên Mac trong thư mục repo. `scripts/vm.sh exec X` chạy X trong máy ảo; `scripts/vm.sh root X` chạy bằng `sudo`.
- Bản tpan trong log là `v0.1.1-15-g13d35d8-dirty`. Chữ `dirty` nghĩa là lúc chạy có file chưa commit; `git status` ở đầu `M13-nang-cap.log` (dòng 261 tới 279) liệt kê chúng, trong đó có `internal/store/users.go`, `internal/server/api_m13.go`, `internal/server/sse.go`. Đó chính là hai bản sửa lỗi được commit sau (`ab5b734` và `a14caa1`, xem mục 5.6), nên log đã chạy cùng hai bản sửa đó.
- Kịch bản in lệnh bằng `$*` nên mất dấu nháy (`--full-name Khach A`). Dưới đây mình viết lại có nháy để dán vào zsh được; output giữ nguyên.
- Dòng `curl -X GET <panel>/api/v1/sites/b1.example.com   (phiên: jar.a)` là cách hàm `api()` của kịch bản (`scripts/e2e-m13.sh` dòng 466 tới 491) in một lần gọi API. Lệnh thật nó chạy có thêm `--cacert` (chứng chỉ của panel lấy từ máy ảo), `-b`/`-c` (file cookie của từng người: `jar.adm` là admin, `jar.a` là `khach_a`, `jar.b` là `khach_b`), header `Origin` và `Sec-Fetch-Site: same-origin` như trình duyệt gửi, và thân JSON ghi ra file quyền 0600 rồi gửi bằng `--data-binary @file` (mật khẩu không nằm trên dòng lệnh). Gõ tay thì có dạng `curl -sS --cacert cert.pem -b jar.a -c jar.a -H 'Content-Type: application/json' --data-binary @login.json https://<IP máy ảo>:8443/api/v1/login` (dạng tay này mình chưa chạy).
- Kịch bản che mật khẩu (`<bí mật, đã che>`), IP (`<vm>`, `<panel>`), thư mục tạm (`<tmp>`). Máy ảo không có tên miền thật: `s1.example.com` trỏ về `127.0.0.1` bằng `/etc/hosts`; các site khác được gọi bằng `curl --resolve`. Máy chủ ACME là pebble chạy trong máy ảo, không phải Let's Encrypt.

### 2.1 Tạo gói và user (M13.log dòng 303 tới 409)

Chạy trên Mac:

```bash
scripts/vm.sh root tpan package add goi_nho --max-sites 2 --max-dbs 1 --yes
scripts/vm.sh root tpan package add goi_lon --max-sites 5 --max-dbs 5 --yes
scripts/vm.sh root tpan package list
```

```
Đã thêm gói goi_nho: site 2, DB 1, disk ∞ MiB, backup ∞.
Đã thêm gói goi_lon: site 5, DB 5, disk ∞ MiB, backup ∞.
TÊN                    SITE     DB   DISK MiB   BACKUP   USER
default                   ∞      ∞          ∞        ∞      1
goi_lon                   5      5          ∞        ∞      0
goi_nho                   2      1          ∞        ∞      0
```

Gói `default` có sẵn từ migration, không giới hạn, đang có một user (`admin`). Thêm user (chạy trên Mac):

```bash
scripts/vm.sh root tpan user add khach_a --full-name "Khach A" --email a@example.com --package goi_nho --yes --json
scripts/vm.sh root tpan user list
```

```
{
  "user": {
    "user": {
      "name": "khach_a",
      "full_name": "Khach A",
      "email": "a@example.com",
      "role": "user",
      "package": "goi_nho",
      "status": "active",
      "created": "2026-11-20T10:21:16.639615171Z"
    },
    ...
    "usage": {
      "sites": 0,
      "dbs": 0,
      "disk_bytes": -1
    },
    "over_quota": false
  },
  "password": "<bí mật, đã che>"
}
```

```
TÊN                  VAI    GÓI            SITE    DB      DUNG LƯỢNG           TRẠNG THÁI
admin                admin  default        0/∞     0/∞     ?                    hoạt động
khach_a              user   goi_nho        0/2     0/1     ?                    hoạt động
khach_b              user   goi_lon        0/5     0/5     ?                    hoạt động
```

(`...` là mình cắt khối `limits`.) Không có cờ `--password`: tpan sinh mật khẩu và in đúng một lần, vì mật khẩu gõ trên dòng lệnh lọt vào `ps` và lịch sử shell. `disk_bytes: -1` và cột `?` là chưa đo lần nào; dung lượng đo sau một vòng health, mỗi giờ tối đa một lần.

Đăng nhập bằng tên rồi hỏi phiên (API, phiên của A):

```
$ curl -X POST <panel>/api/v1/login   (phiên: jar.a)
    thân gửi: {"name":"khach_a","password":"<bí mật, đã che>"}
-> HTTP 204

$ curl -X GET <panel>/api/v1/session   (phiên: jar.a)
-> HTTP 200
    thân nhận (tối đa 700 byte): {"logged_in":true,"demo":false,"hostname":"tpan-test","version":"v0.1.1-15-g13d35d8-dirty","expires":"2026-11-20T22:21:17.107114043Z","user":{"name":"khach_a","role":"user"}}
```

### 2.2 Site cho từng user (M13.log dòng 411 tới 752)

Admin tạo site cho A bằng dòng lệnh, A tự tạo site thứ hai qua API (chạy trên Mac):

```bash
scripts/vm.sh root tpan site add a1.example.com --email admin@example.com --yes --owner khach_a
```

Output giống `site add` của M2 (job thành công, mật khẩu WordPress in một lần). Lần gọi API của A:

```
$ curl -X POST <panel>/api/v1/sites   (phiên: jar.a)
    thân gửi: {"domain":"a2.example.com","email":"a@example.com"}
-> HTTP 202
    thân nhận (tối đa 700 byte): {"job_id":"91bd0dc1c054f05c"}
(chờ job 91bd0dc1c054f05c: 8 giây)
{"job":{"kind":"site.add","target":"a2.example.com","status":"succeeded","actor":"khach_a","owner":"khach_a","error":null},...}
```

`actor` là người bấm, `owner` là chủ của job; mục 3.10 nói hai trường này từ đâu ra. Sau khi B cũng có `b1.example.com` và `b2.example.com` (b2 đã vào thùng rác), admin xem danh sách:

```bash
scripts/vm.sh root tpan site list
```

```
DOMAIN                                   TRẠNG THÁI PHP   SSL  TẠO LÚC          CHỦ            TẠM KHOÁ
a1.example.com                           active   8.3   không 2026-11-20 17:21 khach_a        -
a2.example.com                           active   8.3   không 2026-11-20 17:21 khach_a        -
b1.example.com                           active   8.3   không 2026-11-20 17:21 khach_b        -
b2.example.com                           trashed  8.3   không 2026-11-20 17:22 khach_b        -
s1.example.com                           active   8.3   có   2026-11-20 17:21 admin          -
```

Mỗi site vẫn có user Linux `tp_<slug>` riêng như M2. Kiểm chứng của `site add b1.example.com` có 6 dòng cô lập, ví dụ `[đạt] Site b1.example.com không đọc được wp-config.php của site a1.example.com`: hai site của hai khách khác nhau cũng không đọc được file của nhau, y như hai site của cùng một khách.

DB rời (chạy trên Mac, M13.log dòng 781 tới 812):

```bash
scripts/vm.sh root tpan db add d1 --owner khach_a --note "e2e M13" --yes --json
```

```
Bắt đầu job fe6c3de531f4f600
- Tạo DB khach_a_d1 (utf8mb4_general_ci) và user DB cùng tên ...
  xong
{
  "report": {
    "job": {
      "id": "fe6c3de531f4f600",
      "kind": "dbs.add",
      "target": "khach_a_d1",
      ...
  "db": "khach_a_d1",
  "db_user": "khach_a_d1",
  "db_password": "<bí mật, đã che>"
}
```

### 2.3 A gọi vào đồ của B: 404 như không có (M13.log dòng 1010 tới 1151, 1333 tới 1417)

Phiên của A gọi site của B, rồi gọi một site không tồn tại:

```
$ curl -X GET <panel>/api/v1/sites/b1.example.com   (phiên: jar.a)
-> HTTP 404
    thân nhận (tối đa 700 byte): {"error":{"code":"site_not_found","message":{"vi":"không có site b1.example.com trong tpan; xem danh sách bằng: sudo tpan site list","en":"tpan has no site b1.example.com; list sites with: sudo tpan site list"}}}
  => ĐẠT: A: GET /api/v1/sites/b1.example.com -> 404 site_not_found

$ curl -X GET <panel>/api/v1/sites/khong-co.example.com   (phiên: jar.a)
-> HTTP 404
    thân nhận (tối đa 700 byte): {"error":{"code":"site_not_found","message":{"vi":"không có site khong-co.example.com trong tpan; xem danh sách bằng: sudo tpan site list","en":"tpan has no site khong-co.example.com; list sites with: sudo tpan site list"}}}
  => ĐẠT: 404 của site người khác giống hệt 404 của site không có (trừ tên domain)
```

Kịch bản thử 22 đường dẫn như vậy, không thêm `dry_run` (lọt là chạy thật): `DELETE /sites/b1...`, `POST /sites/b1.../ssl`, `/php`, `/backups`, `GET .../traffic`, `.../log`, khôi phục `b2` từ thùng rác, đổi mật khẩu, xuất, nhập, đổi charset DB của B, xem job và luồng SSE của job của B, xem sự cố số 1 (của B), khôi phục backup của B, sửa, đổi mật khẩu, xoá DB rời `khach_b_d1`, khôi phục và xoá hẳn `khach_b_d2` trong thùng rác. Tất cả ra 404 với mã `site_not_found`, `job_not_found`, `incident_not_found`, `backup_not_found` hay `db_not_found`. Sau đó kịch bản đếm lại job trong `tpan.db`: không có job nào do A chạy hay chạm vào đồ của B (dòng 1419 tới 1440).

Hai trường hợp khác cho ra mã khác, và lý do khác nhau:

```
$ curl -X POST <panel>/api/v1/sites   (phiên: jar.a)
    thân gửi: {"domain":"n1.example.com","email":"a@example.com","owner":"khach_b"}
-> HTTP 403
    thân nhận (tối đa 700 byte): {"error":{"code":"forbidden","message":{"vi":"Chỉ quản trị viên được chọn chủ khác mình.","en":"Only an administrator can choose another owner."}}}

$ curl -X GET <panel>/api/v1/users   (phiên: jar.a)
-> HTTP 403
    thân nhận (tối đa 700 byte): {"error":{"code":"forbidden","message":{"vi":"Chỉ quản trị viên được làm việc này.","en":"Only an administrator can do this."}}}
```

403 là "vai của bạn không được làm việc này": câu trả lời không phụ thuộc dữ liệu của ai, nên không lộ gì. 404 là "không có đồ nào của bạn ở đường dẫn này". A cũng nhận 403 khi tự khoá site của mình (`POST /sites/a1.example.com/suspend?dry_run=1`, dòng 1391): tạm khoá là việc của admin.

### 2.4 Vượt gói bị từ chối (M13.log dòng 1569 tới 1700)

Lúc này A dùng 2/2 site và 1/1 DB rời (`tpan user info khach_a --json` ra `"sites": 2, "dbs": 1`). Chạy trên Mac:

```bash
scripts/vm.sh root tpan site add a3.example.com --email admin@example.com --owner khach_a --yes
scripts/vm.sh root tpan db add d2 --owner khach_a --yes --json
```

```
Từ chối: user khach_a (gói goi_nho) đã dùng 2 trên 2 site (site trong thùng rác vẫn tính, và cả site mới trong cùng danh sách); xoá hẳn bớt site hoặc đổi sang gói lớn hơn
```

```
{
  "error": {
    "code": "quota_dbs",
    "message": {
      "vi": "user khach_a (gói goi_nho) đã dùng 1 trên 1 DB rời (DB trong thùng rác vẫn tính); xoá hẳn bớt DB hoặc đổi sang gói lớn hơn",
      ...
    }
  }
}
```

Cả hai thoát mã 1. Qua API, A nhận cùng lời đó với HTTP 409. Kịch bản chụp máy trước và sau (số user `tp_`, thư mục `/var/www`, danh sách DB, user DB, số pool) và đếm job: không đổi gì, không có job nào cho `a3.example.com` hay `khach_a_d2` (dòng 1671 tới 1698). Từ chối xảy ra lúc dựng Plan, trước khi engine chạy bước nào.

Sau đó admin nâng gói (`tpan package edit goi_nho --max-dbs 2 --yes`) và `db add d2` đạt. DB `khach_a_d2` này dùng ở 2.7.

### 2.5 Tạm khoá site: 403 rồi 200 (M13.log dòng 1745 tới 2763)

Xem trước rồi chạy (chạy trên Mac):

```bash
scripts/vm.sh root tpan site suspend s1.example.com --dry-run
scripts/vm.sh root tpan site suspend s1.example.com --yes
```

Bản xem trước (cắt bớt phần "Vì sao"):

```
Kế hoạch: Tạm khoá site s1.example.com
4 bước sẽ chạy, 1 bước đã đúng sẽ bỏ qua, 2 bước có thể gián đoạn dịch vụ.

 1. [sẽ chạy] Ghi trang tạm khoá /var/lib/tpan/suspended/tpan-suspended.html  (suspended.page:s1.example.com)
 2. [sẽ chạy] Ghi vhost nginx bản tạm khoá cho s1.example.com  (file:/etc/nginx/sites-available/tpan-s1.example.com.conf)
    $ nginx -t
    $ systemctl reload nginx
 3. [đã đúng, bỏ qua] Bật /etc/nginx/sites-enabled/tpan-s1.example.com.conf → /etc/nginx/sites-available/tpan-s1.example.com.conf  (symlink:...)
 4. [sẽ chạy] Tắt pool PHP-FPM tpan-s1_example_com_090c1b (đổi tên thành .suspended)  (rename:/etc/php/8.3/fpm/pool.d/tpan-s1_example_com_090c1b.conf)
    $ php-fpm8.3 -t
    $ systemctl reload php8.3-fpm
 5. [sẽ chạy] Tắt cron WordPress của s1.example.com (đổi tên thành .suspended)  (rename:/etc/cron.d/tpan-s1_example_com_090c1b)
```

Chạy thật:

```
Bắt đầu job 1466258d259ec0cd
- Ghi trang tạm khoá /var/lib/tpan/suspended/tpan-suspended.html ...
  xong
- Ghi vhost nginx bản tạm khoá cho s1.example.com ...
  xong
- Bật /etc/nginx/sites-enabled/tpan-s1.example.com.conf → /etc/nginx/sites-available/tpan-s1.example.com.conf ...
  đã đúng, bỏ qua
- Tắt pool PHP-FPM tpan-s1_example_com_090c1b (đổi tên thành .suspended) ...
  xong
- Tắt cron WordPress của s1.example.com (đổi tên thành .suspended) ...
  xong
  [đạt] Vhost của site có, do tpan quản lý và đang được bật
  [đạt] Cấu hình nginx hợp lệ
  [đạt] Gọi vào site đang tạm khoá trả 403 kèm trang tạm khoá

Job 1466258d259ec0cd: thành công
```

`s1.example.com` là site có SSL. Kịch bản gọi nó trên máy (`curl -sk --resolve s1.example.com:443:127.0.0.1 ...`, in mã HTTP, số lần gặp chuỗi `tpan-suspended-page` trong thân, và `<title>`):

```
http://s1.example.com/ curl=0 http=403 marker=1 title=<title>Site đang tạm khoá / This site is suspended</title>
http://s1.example.com/wp-login.php curl=0 http=403 marker=1 title=<title>Site đang tạm khoá / This site is suspended</title>
https://s1.example.com/ curl=0 http=403 marker=1 title=<title>Site đang tạm khoá / This site is suspended</title>
https://s1.example.com/wp-admin/ curl=0 http=403 marker=1 title=<title>Site đang tạm khoá / This site is suspended</title>
```

Đường thử thách ACME vẫn đi qua (một file thử đặt vào `/var/lib/tpan/acme/.well-known/acme-challenge/`):

```
e2e-m13-thu

HTTP 200
```

và `certbot renew --dry-run --cert-name s1.example.com` với pebble in `Congratulations, all simulated renewals succeeded`. File trên máy trước và sau khi khoá (một khối lệnh `ls` của kịch bản):

```
== pool
pool: tpan-s1_example_com_090c1b.conf.suspended
socket: không có
== cron
cron: tpan-s1_example_com_090c1b.suspended
== php-fpm8.3 -t
[20-Nov-2026 17:22:42] NOTICE: configuration file /etc/php/8.3/fpm/php-fpm.conf test is successful
php-fpm -t mã 0
php8.3-fpm: active
vhost /etc/nginx/sites-available/tpan-s1.example.com.conf: dấu trang tạm khoá 4 dòng, 443: 2 dòng
```

(Trước khi khoá: `pool: tpan-s1_example_com_090c1b.conf`, `socket: có`, `cron: tpan-s1_example_com_090c1b`.) Pool đổi tên nên socket biến mất; PHP-FPM vẫn chạy (pool `tpan-idle` của M12 giữ nó sống).

Health của site đang khoá (chạy trên Mac, `scripts/vm.sh root tpan health s1.example.com --json`, kịch bản tóm tắt):

```
số phép kiểm theo trạng thái: pass=3 unknown=3 warn=1
--- warn dns.a:s1.example.com (chặng dns): Bản ghi A của s1.example.com là 127.0.0.1, không địa chỉ nào nằm trên máy này
--- unknown [tạm khoá] suspended.php:s1.example.com (chặng php): Site đang tạm khoá; chặng này không được kiểm
--- unknown [tạm khoá] suspended.wordpress:s1.example.com (chặng wordpress): Site đang tạm khoá; chặng này không được kiểm
--- unknown [tạm khoá] suspended.db:s1.example.com (chặng db): Site đang tạm khoá; chặng này không được kiểm
sự cố vừa mở: 
sự cố vừa đóng: #2 php.socket:s1.example.com, #3 http.redirect:s1.example.com, #4 https.site:s1.example.com
```

Ba chặng xám, không chặng nào đỏ, không mở sự cố. Ba sự cố đóng là của các phép kiểm thường (php.socket, http.redirect, https.site), những phép chỉ hỏng sau khi site bị khoá; log không ghi lần chạy health nào đã mở chúng. Chúng đóng với lý do "phép kiểm không còn" vì site khoá dùng ID khác (mục 3.15). `a2.example.com` cũng được khoá (dòng 1930), còn `a1` và `b1` vẫn trả 200.

Kịch bản còn chuyển cả máy sang Apache rồi về nginx trong lúc hai site đang khoá (dòng 2076 tới 2654): trên Apache vẫn 403 kèm trang, ACME vẫn 200, `apachectl configtest` đạt, pool vẫn `.suspended`. Chuyển web server không lén mở khoá site nào.

Cron: `a2.example.com` khoá lúc 17:22:43, kịch bản chờ qua một lượt `*/5` rồi đọc nhật ký cron (dòng 2655 tới 2664):

```
2026-11-20T17:25:02+07:00 tpan-test CRON[25200]: (root) CMD (/usr/local/bin/tpan cron run a1.example.com >/dev/null 2>&1)
2026-11-20T17:25:02+07:00 tpan-test CRON[25202]: (root) CMD (/usr/local/bin/tpan cron run b1.example.com >/dev/null 2>&1)
lần chạy a2.example.com: 0
lần chạy a1.example.com: 1
```

Mở khoá (chạy trên Mac):

```bash
scripts/vm.sh root tpan site unsuspend s1.example.com --yes
```

```
Bắt đầu job ea823bf81362c2fd
- Bật lại pool PHP-FPM tpan-s1_example_com_090c1b ...
  xong
- Bật lại cron WordPress của s1.example.com ...
  xong
- Ghi vhost nginx có HTTPS cho s1.example.com ...
  xong
- Bật /etc/nginx/sites-enabled/tpan-s1.example.com.conf → /etc/nginx/sites-available/tpan-s1.example.com.conf ...
  đã đúng, bỏ qua
- Kiểm s1.example.com trả 200 sau khi mở khoá ...
  xong
  [đạt] Chứng chỉ SSL có, khớp tên miền và còn hạn
  ...
  [đạt] Gọi HTTPS vào site trả 200 và đúng là WordPress của site
  ...
Job ea823bf81362c2fd: thành công
```

```
https://s1.example.com/ curl=0 http=200 marker=0 title=<title>s1.example.com</title>
301 https://s1.example.com/
```

Dòng thứ hai là `http://s1.example.com/` chuyển 301 sang https như trước khi khoá.

### 2.6 Khoá DB: cắt kết nối, chặng DB xám (M13.log dòng 2766 tới 3108)

Kịch bản mở sẵn hai kết nối của user `khach_a_d1`, một qua socket, một qua TCP, mỗi cái chạy `SELECT SLEEP(120)`:

```
348	khach_a_d1	localhost	Query	SELECT SLEEP(120)
349	khach_a_d1	localhost:42106	Query	SELECT SLEEP(120)
```

Cột thứ ba là HOST: kết nối socket ghi `localhost`, kết nối TCP ghi `localhost:<cổng>`. Khoá DB rời (chạy trên Mac):

```bash
scripts/vm.sh root tpan db suspend khach_a_d1 --yes
```

```
Bắt đầu job 8db29a2c902d843f
- Khoá user DB khach_a_d1 ...
  xong
- Cắt các kết nối đang mở của user DB khach_a_d1 ...
  xong

Job 8db29a2c902d843f: thành công
```

Ngay sau đó: `PROCESSLIST` của `khach_a_d1` còn `0` dòng; cả hai lệnh `SLEEP(120)` thoát với `ERROR 2013 (HY000) at line 1: Lost connection to server during query`; đăng nhập lại bằng đúng mật khẩu ra `ERROR 4151 (HY000): Access denied, this account is locked`; bảng `mysql.global_priv` ghi `khach_a_d1	1` ở cột `account_locked`. Đổi mật khẩu khi đang khoá (`tpan db passwd khach_a_d1 --yes`) vẫn giữ khoá: mật khẩu mới cũng ra 4151.

Khoá DB của một site (đối số có dấu chấm là domain):

```bash
scripts/vm.sh root tpan db suspend s1.example.com --yes
scripts/vm.sh root tpan health s1.example.com --json
```

```
Bắt đầu job f78ea8271f2aef1b
- Khoá user DB tp_s1_example_com_090c1b ...
  xong
- Cắt các kết nối đang mở của user DB tp_s1_example_com_090c1b ...
  đã đúng, bỏ qua

Job f78ea8271f2aef1b: thành công
```

```
số phép kiểm theo trạng thái: pass=11 unknown=2 warn=1
--- warn dns.a:s1.example.com (chặng dns): Bản ghi A của s1.example.com là 127.0.0.1, không địa chỉ nào nằm trên máy này
--- unknown [tạm khoá] suspended.db:s1.example.com (chặng db): DB đang tạm khoá; chặng này không được kiểm
--- unknown [tạm khoá] suspended.e2e:s1.example.com (chặng e2e): DB đang tạm khoá; chặng này không được kiểm
sự cố vừa mở: 
sự cố vừa đóng: 
```

Chặng DB và chặng đầu cuối xám, không có sự cố nào (`GET /incidents` của admin trả `[]`). Chính site lúc đó trả `http=500 ... title=<title>Database Error</title>`: đúng điều giao diện cảnh báo trước khi khoá. `tpan db unsuspend s1.example.com --yes` chạy hai phép kiểm chứng (user DB đăng nhập được, HTTPS trả 200) rồi health về `pass=14 warn=1`.

### 2.7 Khoá user rồi mở: đúng như trước (M13.log dòng 3356 tới 4170)

Trạng thái trước khi khoá A: `a1.example.com` chạy thường, `a2.example.com` đã khoá riêng từ 2.5 (`"suspended": "self"`), `khach_a_d1` đã khoá riêng từ 2.6, `khach_a_d2` chạy thường. A đang đăng nhập (cookie `jar.a` lấy `GET /sites` được 200). Chạy trên Mac:

```bash
scripts/vm.sh root tpan user suspend khach_a --reason "e2e: chua thanh toan" --yes
```

```
Bắt đầu job 34c55c5821f4c168
- Ghi trang tạm khoá /var/lib/tpan/suspended/tpan-suspended.html ...
  đã đúng, bỏ qua
- Ghi vhost nginx bản tạm khoá cho a1.example.com ...
  xong
- Bật /etc/nginx/sites-enabled/tpan-a1.example.com.conf → /etc/nginx/sites-available/tpan-a1.example.com.conf ...
  đã đúng, bỏ qua
- Tắt pool PHP-FPM tpan-a1_example_com_4bc078 (đổi tên thành .suspended) ...
  xong
- Tắt cron WordPress của a1.example.com (đổi tên thành .suspended) ...
  xong
- Khoá user DB khach_a_d2 ...
  xong
- Cắt các kết nối đang mở của user DB khach_a_d2 ...
  đã đúng, bỏ qua
  [đạt] Gọi vào site đang tạm khoá trả 403 kèm trang tạm khoá

Job 34c55c5821f4c168: thành công
```

Job chỉ chạm `a1` và `khach_a_d2`: hai thứ đã khoá riêng thì bỏ qua. Sau đó cookie cũ của A ra `401 unauthorized`, đăng nhập lại đúng mật khẩu ra `403 user_suspended` (`Tài khoản khach_a đang bị tạm khoá. Liên hệ quản trị viên.`), cả hai site 403 kèm trang, cả hai DB rời 4151. `tpan site list --json` ghi `a1` là `"suspended": "user"`, `a2` vẫn `"suspended": "self"`.

Admin "xem như" A trong lúc A đang khoá (dòng 3870 tới 3917): `POST /users/khach_a/view-as` trả phiên `{"name":"khach_a","role":"user","viewed_by":"admin"}`; `GET /sites` chỉ có `a1`, `a2`; `GET /users` ra 403 (vai đang mang là `user`); `POST /sites/a1.example.com/backups` ra `403 read_only_view` (`Đang xem như khach_a: phiên này chỉ đọc...`); `POST /session/view-as/exit` đưa phiên về admin.

Mở khoá (chạy trên Mac):

```bash
scripts/vm.sh root tpan user unsuspend khach_a --yes
```

```
Bắt đầu job e3989c0df06bce78
- Bật lại pool PHP-FPM tpan-a1_example_com_4bc078 ...
  xong
- Bật lại cron WordPress của a1.example.com ...
  xong
- Ghi vhost nginx cho a1.example.com ...
  xong
- Bật /etc/nginx/sites-enabled/tpan-a1.example.com.conf → /etc/nginx/sites-available/tpan-a1.example.com.conf ...
  đã đúng, bỏ qua
- Kiểm a1.example.com trả 200 sau khi mở khoá ...
  xong
- Mở khoá user DB khach_a_d2 ...
  xong
  ...
Job e3989c0df06bce78: thành công
```

Kết quả, đúng như trước khi khoá user:

```
http://a1.example.com/ curl=0 http=200 marker=0 title=<title>a1.example.com</title>
  => ĐẠT: a1.example.com về 200
http://a2.example.com/ curl=0 http=403 marker=1 title=<title>Site đang tạm khoá / This site is suspended</title>
  => ĐẠT: a2.example.com (khoá riêng từ trước) VẪN 403 kèm trang
khach_a_d2@localhost
mã 0
  => ĐẠT: khach_a_d2 đăng nhập lại được
ERROR 4151 (HY000): Access denied, this account is locked
mã 1
  => ĐẠT: khach_a_d1 (khoá riêng từ trước) VẪN khoá: 4151
```

Không khoá được admin cuối cùng (dòng 4172 tới 4199): qua API admin tự khoá mình ra `409 last_admin` với câu `không tự khoá chính mình được...`; qua CLI `tpan user suspend admin --yes --json` ra `last_admin` với câu `admin là admin cuối cùng đang hoạt động...`.

### 2.8 Nâng từ v0.1.1, đăng nhập bằng admin cũ (M13-nang-cap.log)

Kịch bản cài gói v0.1.1 thật (đúng file đã phát hành, kiểm `SHA256SUMS`) bằng `install.sh` của chính nó, tạo site `up.example.com`, đăng nhập API chỉ bằng mật khẩu (`{"password":...}`, như bản cũ), giữ cookie lại. Dấu của hash mật khẩu quản trị lúc này, đọc từ bảng `settings` (dòng 245 tới 248):

```
hash mật khẩu (sha256 của hash): cced93cc9f114f51
migration: [1, 2, 3, 400, 600, 700, 701, 800, 900]
```

Nâng bằng lệnh `self-update` của chính bản v0.1.1, gói mới phục vụ từ một máy chủ HTTP tạm trong máy ảo (chạy trên Mac, dòng 426 tới 440):

```bash
scripts/vm.sh root tpan self-update --url http://127.0.0.1:18713/v0.1.1-15-g13d35d8-dirty --yes
```

```
Bắt đầu job 5655c63ba324f1e4
- Tải bản tpan mới vào /usr/local/bin/tpan.new ...
  xong
- Thay /usr/local/bin/tpan bằng /usr/local/bin/tpan.new, giữ bản cũ ở /usr/local/bin/tpan.old ...
  xong
- Khởi động lại tpan ...
  xong
  [đạt] File chạy /usr/local/bin/tpan chạy được

Job 5655c63ba324f1e4: thành công
Kiểm chứng:
  [đạt] File chạy /usr/local/bin/tpan chạy được: tpan v0.1.1-15-g13d35d8-dirty (commit 13d35d8, build 2026-11-20T10:17:33Z)
tpan đã đổi: v0.1.1 -> v0.1.1-15-g13d35d8-dirty
```

Sau khi nâng (dòng 470 tới 760):

```
$ curl -X GET <panel>/api/v1/session   (phiên: jar.old)
-> HTTP 200
    thân nhận (tối đa 700 byte): {"logged_in":true,...,"user":{"name":"admin","role":"admin"}}
  => ĐẠT: cookie cũ (tạo bởi v0.1.1): GET /session 200, logged_in, user admin vai admin

$ curl -X POST <panel>/api/v1/login   (phiên: jar.new)
    thân gửi: {"password":"<bí mật, đã che>"}
-> HTTP 204
  => ĐẠT: POST /login chỉ có mật khẩu (không tên): 204
```

```
DOMAIN                                   TRẠNG THÁI PHP   SSL  TẠO LÚC          CHỦ            TẠM KHOÁ
up.example.com                           active   8.3   không 2026-11-20 17:19 admin          -
```

```
hash mật khẩu (sha256 của hash): cced93cc9f114f51
settings còn admin.password_hash: 0
migration 1300: 1
phiên: [('admin', ''), ('admin', '')]
```

Cùng dấu `cced93cc9f114f51`: hash đã chuyển từ `settings` sang `users` nguyên vẹn, và khoá cũ trong `settings` đã bị xoá. `tpan admin passwd --yes` sau đó huỷ phiên cũ (401) và mật khẩu mới đăng nhập bằng `{"name":"admin",...}` được.

Lùi về v0.1.1 (dòng 766 tới 846). `tpan self-update --rollback --yes` báo thành công:

```
Job 61b64033471dfe07: thành công
Kiểm chứng:
  [đạt] File chạy /usr/local/bin/tpan chạy được: tpan v0.1.1 (commit b946767, build 2026-11-20T06:53:42Z)
tpan đã quay về bản trước: v0.1.1-15-g13d35d8-dirty -> v0.1.1
```

nhưng dịch vụ không lên:

```
Result=exit-code
NRestarts=2
ActiveState=activating
SubState=auto-restart
```

```
tpan: mở DB /var/lib/tpan/tpan.db: migration DB /var/lib/tpan/tpan.db: DB ở bản 1300, tpan này chỉ biết tới bản 900: hãy dùng tpan mới hơn
tpan.service: Main process exited, code=exited, status=1/FAILURE
tpan.service: Failed with result 'exit-code'.
tpan.service: Scheduled restart job, restart counter is at 1.
```

`tpan site list` của bản cũ in đúng câu đó và thoát 1; panel không trả lời (`curl: (7) Failed to connect`). Kiểm chứng của job rollback chỉ chạy `tpan version`, nên nó không thấy dịch vụ chết. Đường ra đã chạy thật (dòng 849 tới 955): chạy `install.sh` của gói mới, file chạy có sẵn trong gói:

```bash
scripts/vm.sh exec bash -c 'cd /home/ubuntu/rel/v0.1.1-15-g13d35d8-dirty && sudo ./install.sh'
```

`install.sh` đặt lại file chạy mới, `tpan setup` giữ mật khẩu cũ (`Mật khẩu quản trị đã có từ trước, giữ nguyên.`), dịch vụ `active`, đăng nhập được, `PRAGMA integrity_check` ra `ok`. Muốn thật sự quay về v0.1.1 thì phải chép lại bản sao `tpan.db` lấy từ trước khi nâng; hướng dẫn nâng cấp (`docs/HUONG-DAN-CHU-DU-AN.md`) bảo sao lưu trước.

### 2.9 Test phân quyền mọi route (mình chạy trên Mac, 20/11/2026)

```bash
go test ./internal/server -run 'TestPhanQuyenMoiRoute|TestMountRouteThieuLuat|TestXemNhuChiDoc' -count=1
```

```
ok  	<chủ repo>/tpan/internal/server	0.483s
```

(Đường dẫn module có tên tài khoản GitHub; mình che thành `<chủ repo>`.) Chạy riêng `TestPhanQuyenMoiRoute -v` ra 98 dòng `--- PASS` con, khớp 98 dòng `pattern:` trong `routes.go`, ví dụ:

```
--- PASS: TestPhanQuyenMoiRoute (0.02s)
    --- PASS: TestPhanQuyenMoiRoute/POST_/api/v1/sites (0.00s)
    --- PASS: TestPhanQuyenMoiRoute/GET_/api/v1/sites/{domain} (0.00s)
    --- PASS: TestPhanQuyenMoiRoute/DELETE_/api/v1/dbs/{name} (0.00s)
```

### 2.10 Chưa chạy

- Đo thời gian trả lời đăng nhập tên có thật và tên không có (mục 4.3). Lệnh để đo: `for n in admin khong_co; do curl -sk -o /dev/null -w "$n %{time_total}\n" -H 'Content-Type: application/json' --data-binary "{\"name\":\"$n\",\"password\":\"sai\"}" https://<IP máy ảo>:8443/api/v1/login; done` (chạy trên Mac; 5 lần sai thì IP bị khoá 15 phút).
- Giao diện (màn Người dùng, Gói, dải "xem như"): chưa xem bằng mắt trong mốc này.
- Dòng `include=` của PHP-FPM (mục 4.6): `scripts/vm.sh exec grep -n '^include' /etc/php/8.3/fpm/php-fpm.conf`.
- Nâng trên VPS thật: chủ dự án tự làm.

## 3. Đi qua mã theo thứ tự chạy

Thứ tự: tpan mở `tpan.db` và áp migration, người dùng đăng nhập, mỗi request đi qua guard của route, rồi tới các thao tác mới (tạo site có chủ, tạm khoá site, khoá DB, khoá user).

### 3.1 `internal/store/migrations/1300_users.sql`: bảng mới, cột mới có DEFAULT

`store.Open` áp mọi file `migrations/*.sql` chưa áp, tất cả trong một transaction (hàm `migrate`, `internal/store/store.go`). Bản 1300 thêm bảng `packages`, `users`, `databases` và cột mới cho bảng cũ. Đoạn chuyển tài khoản quản trị cũ:

```sql
INSERT INTO users (name, role, password_hash, created_at)
    SELECT 'admin', 'admin', value, updated_at FROM settings WHERE key = 'admin.password_hash';
DELETE FROM settings WHERE key = 'admin.password_hash';

ALTER TABLE sessions ADD COLUMN user_name TEXT NOT NULL DEFAULT 'admin';
ALTER TABLE sessions ADD COLUMN view_as TEXT NOT NULL DEFAULT '';

ALTER TABLE sites ADD COLUMN owner TEXT NOT NULL DEFAULT 'admin';
ALTER TABLE sites ADD COLUMN suspended TEXT NOT NULL DEFAULT '' CHECK (suspended IN ('', 'self', 'user'));
```

Vì sao viết vậy: `INSERT ... SELECT` chép đúng hash cũ, nên mật khẩu cũ vẫn đúng (2.8, cùng dấu `cced93cc...`). Máy chưa chạy `tpan setup` thì `settings` không có dòng đó và câu này không thêm gì. Mọi cột thêm vào bảng cũ có `DEFAULT`, nên site, job, phiên có từ v0.1.x tự thuộc `admin` mà không cần `UPDATE`; phiên đang mở có `user_name = 'admin'` nên cookie cũ dùng tiếp được. Tạm khoá là cột `suspended` riêng, không phải giá trị mới của `status` (mục 4.4 nói vì sao).

Ở cuối `migrate`, `PRAGMA user_version` được ghi bằng số migration lớn nhất mà bản tpan đó biết. Lúc mở DB, bản nào thấy `user_version` lớn hơn số nó biết thì dừng (`store.go`):

```go
if current > maxKnown {
	// DB do tpan mới hơn tạo ra; chạy tiếp có thể làm hỏng dữ liệu của nó.
	return fmt.Errorf("DB ở bản %d, tpan này chỉ biết tới bản %d: hãy dùng tpan mới hơn", current, maxKnown)
}
```

Đó là câu trong journal ở 2.8.

### 3.2 `internal/account/types.go` và `internal/validate/account.go`: kiểu và tên

`account` là gói lá chứa kiểu dùng chung cho store, core, server: `Role` (`admin`, `user`), `User`, `Package`, `Usage`, `Identity` (ai đang gọi, có `ViewedBy` khi admin đang xem như người khác), và `Suspension` với ba giá trị `NotSuspended` (`""`), `SuspendSelf` (`"self"`, admin khoá riêng thứ này) và `SuspendByUser` (`"user"`, bị khoá cùng lúc khoá user chủ của nó). `User` không có trường mật khẩu: hash chỉ đi qua hàm riêng của store, không bao giờ nằm trong kiểu được trả ra API.

Tên user đi vào câu SQL (tên DB rời `<user>_<tên>`) và vào đường dẫn file, nên kiểm ngay ở `validate.ParseUserName`:

```go
func ParseUserName(s string) (UserName, error) {
	if len(s) > maxUserNameLen || !userNameRe.MatchString(s) {
		return UserName{}, &Error{Code: CodeUserNameInvalid, ...}
	}
	if reservedUserNames[s] || strings.HasPrefix(s, "tp_") {
		return UserName{}, &Error{Code: CodeUserNameReserved, ...}
	}
	return UserName{s: s}, nil
}
```

(`...` là câu báo lỗi hai thứ tiếng, mình cắt.) `userNameRe` là `^[a-z][a-z0-9_]{2,31}$`. Cấm tiền tố `tp_` vì DB rời của user `tp_shop` tên `vn_3fa1c2` sẽ ra `tp_shop_vn_3fa1c2`, có thể trùng đúng DB `tp_<slug>` của một site. Tên user Linux có sẵn trên máy cũng bị chặn, nhưng ở core (`AddUser` đọc `getent passwd`), vì `validate` không chạy lệnh.

### 3.3 `internal/server/auth.go`: `handleLogin`

`POST /api/v1/login` là route `public`. Thứ tự trong `handleLogin`: IP đang bị khoá (sai 5 lần trong 15 phút) thì trả 423 ngay, không tốn argon2; rồi mới so mật khẩu; rồi mới xem user có bị khoá.

```go
name := req.Name
if name == "" {
	name = account.AdminName // thói quen và script cũ chỉ gửi mật khẩu (mục 6.12)
}
if len(name) > authMaxName {
	name = "\x00" // không có user nào tên này: đi đường hash giả
}
ok, done := s.authCheckPassword(w, r, name, req.Password)
if done {
	return
}
if !ok {
	s.authFailed(w, r, ip, "login.fail")
	return
}
```

`authFailed` đếm một lần sai cho IP (lần thứ 5 khoá IP) và ghi nhật ký, nhưng không ghi tên đã gõ: chú thích trong mã nói người ta hay gõ nhầm mật khẩu vào ô tên.

### 3.4 `authCheckPassword` và hash giả

`authCheckPassword` (cùng file) đọc hash của tên đó; không có user thì so với một hash giả:

```go
if !found {
	if hash, err = authDummyHash(); err != nil {
		authInternal(w, "dựng hash giả", err)
		return false, true
	}
}
ok, err = store.VerifyPassword(hash, pw)
...
return ok && found, false
```

`authDummyHash` là hash argon2id thật của một chuỗi ngẫu nhiên, tính một lần (`sync.Once`). Vì sao: tên có thật thì server tốn thời gian chạy argon2, tên không có mà trả lời ngay thì kẻ dò chỉ cần bấm giờ là biết tên nào tồn tại (mục 4.3). Phải là hash thật, vì `VerifyPassword` gặp hash hỏng (hay rỗng) thì trả lỗi ngay, request thành 500 thay vì 401, và mã khác nhau chính là lộ (câu 3 ở mục 6). Một ngoại lệ: tên `admin` mà chưa có hash (máy chưa `tpan setup`) thì trả 503 `not_set_up`; chuyện này không lộ gì vì máy nào cũng có user `admin`.

Chỉ sau khi đúng mật khẩu mới xem `u.Status`: user đang khoá nhận `403 user_suspended` (2.7). Người không biết mật khẩu thì chỉ thấy "Sai mật khẩu.", không biết được tài khoản có đang bị khoá không.

### 3.5 `internal/server/routes.go`: bảng route và `mount`

Mọi đường dẫn của server nằm trong một bảng (hàm `routes`, 98 dòng), mỗi dòng kèm luật:

```go
{pattern: "GET /api/v1/sites", rule: anyone, h: s.handleSites},
{pattern: "GET /api/v1/sites/{domain}", rule: ownerSite, h: s.handleSite},
{pattern: "GET /api/v1/jobs/{id}", rule: ownerJob, h: s.handleJob},
{pattern: "GET /api/v1/machine", rule: adminOnly, h: s.handleMachine},
```

Bốn loại luật: `public` (không cần đăng nhập), `anyone` (đã đăng nhập, dữ liệu tự lọc theo chủ), `ownerSite`/`ownerJob`/`ownerIncident`/`ownerBackup`/`ownerDB` (phải là chủ của đối tượng trong đường dẫn), `adminOnly`. `mount` đăng ký từng dòng qua `guard`; `guard` gặp luật zero (`ruleUnset`, tức là quên khai) thì `panic`, nên server không khởi động được với một route quên luật:

```go
switch rt.rule.kind {
case rulePublic:
	return rt.h
case ruleAny, ruleAdmin:
case ruleOwner:
	if rt.rule.obj < objSite || rt.rule.obj > objDB {
		panic("route " + rt.pattern + ": luật owner không có đối tượng")
	}
default:
	panic("route " + rt.pattern + " không khai luật (public, any, owner:X, admin); mặc định là cấm")
}
```

### 3.6 Thân của `guard`: phiên, chỉ đọc, context, luật

Với route không `public`, handler thật được bọc trong một hàm làm bốn việc theo thứ tự:

```go
ss, id, ok, err := s.authResolve(r)
...
if id.ReadOnly() && r.Method != http.MethodGet && r.Method != http.MethodHead && !rt.allowReadOnly {
	writeError(w, http.StatusForbidden, "read_only_view", ...)
	return
}
actor := id.Name
if id.ViewedBy != "" {
	actor = id.ViewedBy
}
ctx := context.WithValue(r.Context(), authCtxKey{}, ss)
ctx = context.WithValue(ctx, identityKey{}, id)
r = r.WithContext(engine.WithActor(ctx, actor))
```

1. `authResolve` (auth.go) đọc cookie, tìm phiên, rồi đọc lại dòng `users` của phiên đó. User không còn hay không `active` thì xoá phiên và trả như chưa đăng nhập: đó là lý do cookie của A ra 401 ngay sau khi A bị khoá (2.7), không phải chờ phiên hết hạn. Phiên "xem như" thì dựng danh tính của user được xem, kèm `ViewedBy` là admin thật.
2. Phiên "xem như" chỉ được GET, HEAD; mọi thứ khác ra `read_only_view`, trừ route thoát "xem như" (`allowReadOnly`).
3. Gắn phiên, danh tính và tên người thật đã thao tác vào context (mục 4.7). Khi admin đang xem như A, `actor` vẫn là `admin`.
4. Luật của route: `adminOnly` mà không phải admin thì 403 `forbidden`; `owner` thì admin qua, user thường phải qua `ownsPath`.

Danh sách (`GET /sites`, `/jobs`, `/databases`...) dùng `scopeOf(r)`: rỗng cho admin, tên mình cho user thường. Store lọc ngay trong SQL (`SitesOf`: `WHERE (? = '' OR owner = ?)`), nên dữ liệu của người khác không bao giờ ra tới tầng server. `scopeOf` panic nếu một user thường có tên rỗng: bộ lọc hiểu rỗng là "của mọi người", nên tên rỗng là lỗ lớn nhất có thể có.

### 3.7 `ownsPath`: không phải của bạn thì 404

```go
case objSite:
	d, perr := validate.ParseDomain(r.PathValue("domain"))
	...
	gone = siteNotFound(d)
	owner, found, err = st.SiteOwner(ctx, d.String())
...
// owner "" (sự cố của máy, stack, tpan) chỉ admin xem: name không bao giờ rỗng.
if !found || owner != name {
	apiFail(w, r, gone)
	return false
}
return true
```

"Không có" (`!found`) và "của người khác" (`owner != name`) đi cùng một nhánh và trả cùng lời `gone`. `siteNotFound` dùng đúng câu của core (`không có site ... trong tpan; xem danh sách bằng: sudo tpan site list`), nên một site của B và một site không tồn tại cho ra hai thân JSON chỉ khác nhau ở tên domain mà người gọi tự gõ (2.3). Sự cố của máy, stack, tpan có chủ rỗng, mà tên của user thường không bao giờ rỗng, nên chỉ admin xem được.

### 3.8 Tạo site có chủ: `siteCreateOwner`

`handleSiteCreate` (`internal/server/api_job.go`) gọi `siteCreateOwner` (`api_m13.go`) để quyết chủ của site mới:

```go
if asked != "" && asked != id.Name {
	...
	writeOwnerForbidden(w)
	return "", false
}
owner, found, err := s.deps.Store.SiteOwner(r.Context(), d.String())
...
if found && owner != id.Name {
	...
	apiFail(w, r, siteNotFound(d))
	return "", false
}
return id.Name, true
```

Admin thì chọn chủ tuỳ ý (để trống là chính mình). User thường xin chủ khác mình thì 403 (câu hỏi về vai). User thường gõ domain đã là site của người khác thì 404 như mọi đồ người khác.

### 3.9 Hạn mức: `newOwner`, `quotaCheck`

Tiếp theo `core.Sites.newOwner` (`internal/core/site.go`) kiểm chủ có thật, đang `active`, và còn chỗ trong gói: `checkQuota(..., QuotaSites, ...)`, tức `quotaCheck` trong `internal/core/users.go`:

```go
limit := func(code string, have, most int, vi, en string) error {
	if most == 0 || have < most {
		return nil
	}
	return refuse(code, i18n.T(
		fmt.Sprintf("%s đã dùng %d trên %d %s", who, have, most, vi),
		fmt.Sprintf("%s already uses %d of %d %s", whoEn, have, most, en)))
}
switch k {
case QuotaSites:
	return limit(CodeQuotaSites, use.Sites+extra, p.MaxSites, ...)
```

`most == 0` là không giới hạn. `extra` là số site cùng chủ đã nhận trong cùng một lô `apply` mà bảng `sites` chưa có. `refuse` tạo `RefusedError`, loại lỗi "từ chối kèm lý do" có từ M2: CLI in `Từ chối: ...`, API trả 409. Lời từ chối ra lúc dựng Plan, trước khi engine chạy bước nào, nên máy không đổi (2.4). DB rời đi qua cùng hàm với `QuotaDBs` (`DBs.PrepareAdd`, `internal/core/dbs.go`).

Vì sao chủ do server quyết rồi đưa xuống core (`SiteInput.Owner`), không để core tự xem vai: core không biết vai (quyết định 10). Core chỉ giữ những luật đúng với mọi người gọi (hạn mức, admin cuối cùng); ai được gọi gì là luật của route.

### 3.10 `engine.WithActor` và `store.CreateJob`: ai chạy, job của ai

`internal/engine/types.go` (chú thích trên `WithActor` nói nó dùng để "mọi job tạo bên dưới (qua core, không phải truyền tay qua từng hàm) ghi đúng người chạy"):

```go
func WithActor(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, actorKey{}, name)
}

// ActorFrom đọc tên đã gắn bằng WithActor; rỗng nếu không có.
func ActorFrom(ctx context.Context) string {
	s, _ := ctx.Value(actorKey{}).(string)
	return s
}
```

`guard` gắn (3.6), `engine.go` đọc lúc tạo job (`Actor: ActorFrom(ctx), Owner: OwnerFrom(ctx)`). Giữa hai chỗ đó là cả chục hàm của core không phải đổi chữ ký. Chủ của job thì `store.CreateJob` (`internal/store/store.go`) suy trong chính câu `INSERT`:

```sql
COALESCE(NULLIF(?, ''),
  (SELECT owner FROM sites WHERE domain = ?),
  (SELECT owner FROM databases WHERE name = ?),
  NULLIF(?, ''), 'admin')
```

Thứ tự: chủ người gọi đặt sẵn bằng `WithOwner` (dùng cho job tạo thứ chưa có dòng, như site mới), rồi chủ của site có domain trùng `target`, rồi chủ của DB rời trùng tên, rồi `actor`, cuối cùng `admin`. Nhờ vậy job `dbs.suspend` admin chạy bằng CLI trên `khach_a_d1` có `actor: -` nhưng `owner: khach_a` (M13.log dòng 2823), và A thấy job đó trong danh sách của mình.

### 3.11 Tạm khoá site: `PrepareSuspend`, `suspendSteps`

Lệnh `tpan site suspend s1.example.com` (`cmd/tpan/site.go`) gọi `core.Sites.PrepareSuspend` (`internal/core/suspend.go`). Hàm này từ chối site trong thùng rác hay đã khoá (`site_suspended`, M13.log dòng 2062), rồi lấy các bước từ `suspendSteps`:

```go
sus := st
if sus.Suspended == account.NotSuspended {
	sus.Suspended = account.SuspendSelf
}
vhost, err := siteVhost(sus, cfg)
...
pool, err := steps.PoolSuspend(l, sitePHP(st, cfg))
...
cron, err := steps.CronSuspend(l)
...
ss := append([]engine.Step{page}, vhost...)
return append(ss, pool, cron), nil
```

Thứ tự có lý do: vhost tạm khoá trước, pool sau. Nếu tắt pool trước, có vài trăm mili giây web server còn chuyển `.php` tới một socket đã mất và khách thấy 502 thay vì trang tạm khoá. Job thành công thì callback của `newPlanned` mới ghi `suspended = 'self'` vào bảng `sites` (`markSuspended`); job hỏng thì engine hoàn tác và bảng không đổi.

### 3.12 `siteVhost`: chỗ duy nhất chọn loại vhost

`siteVhost` (`internal/core/fix.go`) là chỗ duy nhất chọn loại vhost:

```go
func siteVhost(st site.Site, cfg StackConfig) ([]engine.Step, error) {
	web, php := string(cfg.Web), sitePHP(st, cfg)
	switch {
	case st.Suspended != account.NotSuspended:
		return steps.SiteVhostSuspended(st.Layout(), web, acmeRoot, st.SSL)
	case st.SSL:
		return steps.SiteVhostSSL(st.Layout(), web, php, acmeRoot)
	}
	return steps.SiteVhost(st.Layout(), web, php, acmeRoot)
}
```

Chuyển nginx ⇄ apache (`stack_switch.go`), sửa tự động của health (`fix.go`), xin SSL (`ssl.go`), đổi bản PHP (`php.go`), khôi phục từ thùng rác (`trash.go`) đều gọi hàm này (quyết định 8). Nếu một chỗ tự chọn `SiteVhost` thì chỗ đó sẽ lén ghi vhost thường cho site đang khoá, tức là mở khoá mà không ai bấm. Bằng chứng là phần chuyển sang Apache ở 2.5: site khoá vẫn 403 sau khi đổi web server.

### 3.13 Vhost tạm khoá: `site-nginx-suspended.conf.tmpl`

Bản nginx tạm khoá, `internal/templates/files/site-nginx-suspended.conf.tmpl`:

```nginx
location ^~ /.well-known/acme-challenge/ {
    root {{.AcmeRoot}};
}

error_page 403 {{.URI}};

location = {{.URI}} {
    internal;
    root {{.Dir}};
}

location / {
    return 403;
}
```

Mọi đường dẫn rơi vào `location /` và nhận 403. `error_page 403` không có dấu `=` thì giữ mã 403, chỉ thay thân bằng trang tạm khoá (`/var/lib/tpan/suspended/tpan-suspended.html`, chung cho mọi site). `internal` làm trang đó không gọi thẳng từ ngoài được. `^~` ở dòng ACME nghĩa là khớp tiền tố này thì nginx không xét location nào khác, nên thử thách Let's Encrypt vẫn đi qua và chứng chỉ gia hạn được trong lúc khoá. File không nhắc tới thư mục `public` hay socket PHP của site: site khoá không chạy mã nào của nó. Bản có SSL và hai bản Apache nằm cạnh (`site-nginx-suspended-ssl.conf.tmpl`, `site-apache-suspended.conf.tmpl`, `site-apache-suspended-ssl.conf.tmpl`).

### 3.14 Tắt pool và cron: `steps.Rename`, rồi mở khoá

Pool và cron tắt bằng đổi tên, Step `Rename` trong `internal/steps/suspend.go`. `PoolSuspend` dựng:

```go
conf := l.PoolConf(phpVersion)
return &Rename{From: conf, To: conf + SuspendedSuffix, Test: test, Reload: reload, Danger: true, ...
```

`Rename.Apply` đổi tên, chạy `php-fpm8.3 -t`, rồi `systemctl reload php8.3-fpm`; kiểm sai hay reload lỗi thì đổi tên ngược lại ngay. `Undo` cũng đổi ngược. `Check` đạt khi file `From` không còn: chạy lại lệnh khoá trên site đã khoá dở thì bước này "đã đúng, bỏ qua". Vì sao đổi tên mà không xoá: PHP-FPM chỉ nạp `*.conf`, cron bỏ qua tên có dấu chấm (mục 4.6), nên đổi tên là tắt mà vẫn giữ nguyên file, mở khoá chỉ cần đổi tên về. `CronSuspend` không có `Reload`: cron tự đọc lại `/etc/cron.d` khi thư mục đổi (M9).

Mở khoá (`PrepareUnsuspend`, `unsuspendSteps`) làm ngược lại theo thứ tự ngược: pool về trước và chờ socket, cron về, rồi vhost thường. Cuối cùng là một `midCheck` (M12) cần hai lần trả 200 liên tiếp, vì `systemctl reload apache2` trả về trước khi vhost mới có hiệu lực, và lần gọi đầu có thể còn gặp vhost tạm khoá. File `.suspended` bị mất (ai đó xoá tay) thì ghi lại pool từ template như lúc tạo site, không thì site mở khoá chỉ trả 502.

### 3.15 Health của site khoá: `checks.SuspendedStage`

`internal/checks/suspend.go`:

```go
func SuspendedStage(stage Stage, l site.Layout, db bool) Check {
	b := siteBase("suspended."+string(stage), stage, l, ...)
	sum := i18n.T("Site đang tạm khoá; chặng này không được kiểm", ...)
	if db {
		sum = i18n.T("DB đang tạm khoá; chặng này không được kiểm", ...)
	}
	return checkFunc(func(context.Context, system.System) Result {
		r := b.result(Unknown, sum)
		r.Suspended = true
		return r
	})
}
```

Health thay các chặng PHP, WordPress, DB của site khoá bằng kết quả này, và thay phép gọi HTTP thường bằng `HTTPSuspended` (đạt khi mọi lần gọi trả 403 và thân có chuỗi `tpan-suspended-page`). `Unknown` vốn không mở cũng không đóng sự cố (M4); cờ `Suspended` chỉ để giao diện đổi nhãn thành "tạm khoá" màu xám. ID riêng `suspended.<chặng>:<domain>` làm phép kiểm thường (`php.socket:...`) biến mất khỏi lần chạy, và health đóng sự cố đang mở của nó với lý do "không còn": đó là ba dòng "sự cố vừa đóng" ở 2.5. Khoá chỉ DB của site thì `db=true`, và chặng DB cùng chặng đầu cuối xám (2.6).

### 3.16 Khoá DB: `dbLockSteps`, `DBAccountLock`, `DBKillConnections`

`internal/core/dbs.go`:

```go
func dbLockSteps(user string) []engine.Step {
	return []engine.Step{&steps.DBAccountLock{Name: user}, &steps.DBKillConnections{Name: user}}
}
```

Dùng chung cho DB rời (`PrepareSuspend`), DB của site (`PrepareSiteDBSuspend`) và khoá user. `DBAccountLock` (`internal/steps/mariadb_m13.go`) chạy `ALTER USER '<tên>'@'localhost' ACCOUNT LOCK;` (câu dựng ở `dbLockSQL`, `internal/steps/fix.go`). Câu này chỉ chặn đăng nhập mới. Kết nối đã mở vẫn đọc ghi được, nên bước thứ hai cắt chúng:

```go
ids, err := sdbConns(ctx, sys, s.Name)
...
for _, id := range ids {
	err := dbRun(ctx, sys, sdbKillSQL(id), "", fmt.Sprintf("cắt kết nối %d của user DB %s", id, s.Name))
	if ee, ok := isExit(err); ok && strings.Contains(ee.Stderr, "1094") {
		continue // kết nối đã tự đóng
	}
	if err != nil {
		return err
	}
}
```

`sdbConns` chạy `SELECT ID FROM information_schema.PROCESSLIST WHERE USER='<tên>'` và đọc từng id bằng `strconv.ParseUint`: stdout không bao giờ được ghép thẳng vào câu `KILL`. Lọc theo `USER`, không theo `HOST`, vì kết nối TCP có HOST `localhost:<cổng>` (2.6), lọc `HOST='localhost'` sẽ sót nó. Mỗi `KILL CONNECTION <id>` một lần gọi `mariadb`, vì client dừng ở câu lỗi đầu tiên; lỗi 1094 (`Unknown thread id`) nghĩa là kết nối tự đóng giữa lúc đọc và lúc cắt, coi là xong. Sau đó bước chờ tới khi `PROCESSLIST` hết dòng, vì `KILL` chỉ đặt cờ cho luồng kia tự thoát.

Thứ tự khoá rồi mới cắt: cắt trước thì chương trình phía kia (WordPress, ứng dụng của khách) nối lại ngay được bằng user chưa khoá. Trạng thái khoá đọc bằng `JSON_VALUE(Priv, '$.account_locked')` trong `mysql.global_priv`, không bằng `SHOW CREATE USER`: câu đó in cả hash mật khẩu, mà engine ghi mọi output vào log của job.

### 3.17 Khoá user: `PrepareSuspendUser`, `PrepareUnsuspendUser`

`internal/core/users.go`. Khoá user từ chối khoá chính mình và admin cuối cùng, rồi duyệt đồ của user:

```go
for _, st := range sites {
	// Đã khoá riêng thì giữ nguyên nguồn "self": mở khoá user sau này
	// không được mở nó.
	if st.Status != site.Active || st.Suspended != account.NotSuspended {
		continue
	}
	steps, err := userSiteSuspend(a.Sites, ctx, st)
	...
	locked = append(locked, st)
}
```

DB rời làm tương tự với `userDBLock` (chính là `dbLockSteps`). Một job cho tất cả. Job xong thì đánh dấu từng thứ vừa khoá là `SuspendByUser`, ghi user `suspended` kèm lý do, và huỷ mọi phiên của user. Chú thích trong mã giải thích vì sao user và phiên vẫn được ghi khi một dòng ở trên lỗi: chặn đăng nhập quan trọng hơn.

Mở khoá user chọn ngược lại, đúng những thứ mang nguồn `user`:

```go
for _, st := range sites {
	if st.Status != site.Active || st.Suspended != account.SuspendByUser {
		continue
	}
```

Đó là toàn bộ lý do có cột nguồn khoá: ở 2.7, `a2` mang `self` nên không bị mở, `a1` mang `user` nên được mở. Nếu `suspended` chỉ là có/không thì mở khoá user sẽ mở luôn `a2` mà admin đã cố ý khoá trước đó.

### 3.18 `internal/server/authz_test.go`: test ba vai cho mọi route

`TestPhanQuyenMoiRoute` lấy chính bảng `routes()`, thay mọi handler bằng một handler giả trả mã 299 ("đã tới handler"), rồi với từng route gọi bằng bốn phiên: chưa đăng nhập, admin, A (chủ của dữ liệu mẫu), B (user khác):

```go
wantA, wantB := authzStatus, authzStatus
switch rt.rule.kind {
case ruleOwner:
	wantB = http.StatusNotFound
case ruleAdmin:
	wantA, wantB = http.StatusForbidden, http.StatusForbidden
}
if got := code(authzA); got != wantA {
	t.Errorf("A (chủ): mã %d, muốn %d", got, wantA)
}
if got := code(authzB); got != wantB {
	t.Errorf("B (user khác): mã %d, muốn %d", got, wantB)
}
```

Với luật owner, test còn so 404 của "đồ của A mà B gọi" với 404 của "đồ không có" (`sameNotFound`): cùng mã HTTP, cùng mã lỗi, cùng câu chữ sau khi thay giá trị trên đường dẫn bằng `X`. Route mới thêm vào bảng tự vào vòng duyệt; đường dẫn có placeholder lạ (`{abc}`) làm test dừng với `placeholder {abc} chưa có giá trị trong test`, buộc người thêm route phải nghĩ tới nó. `TestMountRouteThieuLuat` kiểm `mount` panic với route không luật; `TestXemNhuChiDoc` kiểm mọi route không phải GET bị `read_only_view` trong phiên xem như.

## 4. Kiến thức nền

### 4.1 Phân quyền mặc định là cấm (deny by default)

Hệ thống phân quyền có hai cách: liệt kê những gì bị cấm (còn lại được phép), hoặc liệt kê những gì được phép (còn lại bị cấm). Cách đầu hỏng âm thầm: quên một dòng là mở một lỗ. Cách sau hỏng ồn ào: quên một dòng thì chức năng không chạy và người viết thấy ngay. TPAN làm cách sau ở mức mạnh hơn: route không khai luật làm server không khởi động được. Trong mã: `guard` (`routes.go`, nhánh `default: panic(...)`) và `TestMountRouteThieuLuat`. Tự thấy: luật hiện hành nằm trong bảng; thử trên máy ảo bằng `scripts/vm.sh exec curl -sk https://127.0.0.1:8443/api/v1/users` (không cookie) phải ra 401 `unauthorized`. Lệnh này mình chưa chạy; log có trường hợp tương đương là cookie cũ của A ra 401 ở 2.7.

### 4.2 Vì sao 404, không phải 403, cho đồ của người khác

403 nghĩa là "có thứ đó, nhưng bạn không được đụng". Trả 403 cho `GET /sites/b1.example.com` là xác nhận với A rằng trên máy có site `b1.example.com`; A thử lần lượt tên là dò ra danh sách khách của nhà cung cấp hosting. 404 trả lời như thể không có gì, và vì "không có thật" cũng ra đúng 404 đó, A không phân biệt được. Còn 403 dùng cho câu hỏi về vai (`GET /users`, đặt chủ khác mình): câu trả lời không phụ thuộc dữ liệu của ai, nên nói thẳng là "không được" thì dễ hiểu hơn. Trong mã: `ownsPath` (`routes.go`), một nhánh cho `!found || owner != name`. Tự thấy: 2.3 (M13.log dòng 1012 tới 1021), hai thân JSON chỉ khác tên domain. Trên máy ảo, đăng nhập bằng user thường rồi gọi một domain bất kỳ không có, ví dụ `GET /api/v1/sites/khong-co.example.com`: phải ra đúng dạng 404 đó (dạng gõ tay của `curl` ở đầu mục 2; chưa chạy dạng tay).

### 4.3 Tấn công dò tên bằng thời gian trả lời

Kẻ tấn công gửi đăng nhập sai cho nhiều tên và đo thời gian từng lần. argon2id cố ý chậm (vài chục tới vài trăm mili giây). Nếu tên không có mà server trả lời ngay, còn tên có thì chờ argon2, chỉ cần bấm giờ là biết tên nào tồn tại, rồi dồn sức đoán mật khẩu cho đúng các tên đó. Đây là kiểu tấn công kênh phụ (side channel): không đọc được câu trả lời nhưng đọc được cách trả lời. Trong mã: `authDummyHash` và nhánh `!found` của `authCheckPassword` (`auth.go`). Tự thấy: lệnh đo ở 2.10, chưa chạy. Lưu ý: chống này giúp thời gian xấp xỉ nhau, không bằng nhau tới từng micro giây (còn một lần đọc DB khác nhau).

### 4.4 Migration SQLite, và vì sao không đổi được CHECK

Migration là file SQL đánh số, chạy đúng một lần theo thứ tự để đưa cấu trúc DB từ bản cũ lên bản mới; bảng `schema_migrations` nhớ file nào đã chạy. SQLite có `ALTER TABLE ... ADD COLUMN` nhưng không có lệnh sửa ràng buộc `CHECK` của cột đã có. Muốn cho `sites.status` nhận thêm `'suspended'` thì phải tạo bảng mới, chép dữ liệu, xoá bảng cũ, đổi tên, dựng lại index và khoá ngoại: nhiều bước, dễ sai trên DB người dùng đang chạy. Thêm một cột riêng có `DEFAULT` thì chỉ một câu. Trong mã: `1300_users.sql` (chú thích đầu file) và `migrate` (`store.go`). Tự thấy: `scripts/vm.sh root python3 -c 'import sqlite3; c=sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True); print(c.execute("SELECT sql FROM sqlite_master WHERE name=\"sites\"").fetchone()[0])'` in câu `CREATE TABLE sites` hiện tại, có cả các cột `ADD COLUMN` (chưa chạy; log có lệnh đọc `schema_migrations` tương tự ở 2.8).

### 4.5 `ACCOUNT LOCK` của MariaDB

Từ MariaDB 10.4, mỗi tài khoản có cờ khoá: `ALTER USER 'u'@'localhost' ACCOUNT LOCK` làm mọi lần đăng nhập mới bị từ chối với `ERROR 4151 (HY000): Access denied, this account is locked`, mà không đổi mật khẩu, quyền hay dữ liệu. `ACCOUNT UNLOCK` trả lại như cũ. Cờ nằm trong cột JSON `Priv` của bảng `mysql.global_priv`. Khoá không cắt kết nối đang mở, nên TPAN cắt bằng `KILL CONNECTION`. Đổi mật khẩu (`ALTER USER ... IDENTIFIED BY`) không gỡ cờ khoá (2.6). Trong mã: `DBAccountLock`, `DBKillConnections` (`mariadb_m13.go`), `dbLockSQL`, `dbLockStateSQL` (`internal/steps/fix.go`). Tự thấy (chạy trên Mac): `scripts/vm.sh root mariadb -N -B -e 'SELECT User, JSON_VALUE(Priv, "$.account_locked") FROM mysql.global_priv WHERE User = "khach_a_d1"'`. Log chạy câu này với nháy đơn quanh chuỗi SQL (2.6, ra `khach_a_d1	1`); mình đổi nháy để zsh không đụng tới `$`.

### 4.6 PHP-FPM chỉ nạp `*.conf`, cron.d bỏ qua tên có dấu chấm

File cấu hình chính của PHP-FPM trên Ubuntu có dòng `include=/etc/php/8.3/fpm/pool.d/*.conf` (theo gói của Ubuntu; chưa mở file này trên máy ảo trong M13): chỉ file đuôi `.conf` được nạp. Thêm `.suspended` vào cuối tên là pool không còn được nạp, và reload thì PHP-FPM gỡ pool cùng socket của nó. Cron của Debian, Ubuntu đọc `/etc/cron.d/` theo luật đặt tên của `run-parts`: chỉ nhận tên gồm chữ, số, `_`, `-`; tên có dấu chấm bị bỏ qua (vì thế file của gói như `foo.dpkg-old` không bao giờ chạy). Trong mã: `PoolSuspend`, `CronSuspend` (`internal/steps/suspend.go`). Tự thấy: socket biến mất và cron không chạy `a2` ở 2.5; dòng `include=` thì chưa chạy (lệnh ở 2.10).

### 4.7 `context.Context` mang giá trị: `WithActor`

Trong Go, `context.Context` là một giá trị đi theo một request từ đầu tới cuối: nó mang hạn chót, tín hiệu huỷ, và một ít giá trị gắn kèm. `context.WithValue(ctx, khoá, giá trị)` trả một context mới chứa thêm cặp đó; `ctx.Value(khoá)` đọc lại. Khoá là một kiểu riêng không xuất ra ngoài (`type actorKey struct{}`), để gói khác không vô tình ghi đè. Trong mã: `WithActor`, `ActorFrom` (`internal/engine/types.go`), gắn ở `guard`, đọc ở `engine.go` lúc tạo job. Tự thấy (chạy trên Mac, chưa chạy): `scripts/vm.sh root python3 -c 'import sqlite3; c=sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True); [print(*r) for r in c.execute("SELECT kind, target, actor, owner FROM jobs ORDER BY started_at DESC LIMIT 5")]'`. Job tạo qua API có `actor` là người đăng nhập (`"actor":"khach_a"` ở 2.2), job chạy bằng CLI có `actor` rỗng vì CLI không gắn (M13.log dòng 2823).

## 5. Cách khác đã cân nhắc, và chỗ đã làm sai

Các quyết định dưới đây là quyết định 1, 5, 9 và 3 trong phần "Đã chốt khi làm M13" của `docs/KE-HOACH.md`.

### 5.1 Tạm khoá là một giá trị mới của `status`

Ý đầu tiên là thêm `'suspended'` vào `sites.status`. Không chọn vì hai lẽ. Một: `status` có `CHECK (status IN ('active','trashed'))`, mà SQLite không đổi được CHECK nếu không dựng lại bảng (4.4). Hai: hai chuyện độc lập nhau. Site đang khoá vẫn là `active` theo nghĩa "không ở thùng rác"; mọi truy vấn có sẵn kiểu `WHERE status = 'active'` (health, backup theo lịch, danh sách) phải được rà lại hết nếu khoá trở thành một giá trị của `status`. Cột riêng thì mã cũ không đổi.

### 5.2 Khoá chỉ có hai trạng thái có/không

Đơn giản hơn là `suspended` kiểu có/không. Không chọn vì đặc tả đòi "mở khoá user chỉ mở lại đúng những gì bị khoá vì user". Với có/không, lúc mở khoá user không phân biệt được `a2` (admin khoá riêng từ trước) với `a1` (khoá cùng user). Nên cột mang nguồn: `''`, `'self'`, `'user'` (3.17, 2.7). Cũng vì thế khoá user bỏ qua thứ đã khoá riêng thay vì ghi đè nguồn của nó.

### 5.3 Trạng thái thứ năm "tạm khoá" trong health

Health có bốn trạng thái (`pass`, `warn`, `fail`, `unknown`). Thêm `suspended` thứ năm thì phải dạy lại phần mở, đóng sự cố, phần tóm tắt, và bốn hình trạng thái trên giao diện. Chọn `Unknown` cộng cờ `Result.Suspended`: `Unknown` vốn không mở cũng không đóng sự cố, đúng điều cần cho site khoá; giao diện chỉ đổi nhãn khi thấy cờ. Cái giá: ai đọc kết quả mà bỏ qua cờ sẽ thấy "chưa kiểm được" thay vì "tạm khoá". Đi kèm là chọn ID riêng `suspended.<chặng>` thay vì mượn ID cũ, để sự cố cũ đóng được (3.15).

### 5.4 Truyền chủ của job tay qua từng hàm

Cách thẳng là mọi hàm `Prepare...` của core nhận thêm tham số chủ và đưa xuống engine. Không chọn: hàng chục chỗ gọi, quên một chỗ là job đó thuộc admin và user không thấy job trên site của mình. Chọn suy ở store, một câu SQL (3.10): chủ của site hay DB trùng `target`, rồi người chạy, rồi admin. Chỉ job tạo thứ chưa có dòng mới cần `WithOwner`. Hệ quả cần biết: job có `target` là tên user (`user.suspend khach_a`) không khớp site hay DB nào, nên thuộc `admin` (M13.log dòng 3590: `target: khach_a | actor: - | owner: admin`).

### 5.5 Cho phép lùi về v0.1.1

Muốn lùi được thì bản v0.1.1 phải mở được `tpan.db` đã có bảng và cột của 1300. Nó không biết chúng, và v0.1.1 đã phát hành, không sửa được nữa. Thiết kế từ M1 là bản cũ từ chối DB mới hơn nó (`DB ở bản 1300, tpan này chỉ biết tới bản 900: hãy dùng tpan mới hơn`), vì chạy tiếp có thể ghi hỏng dữ liệu nó không hiểu. Đã chạy thật (2.8): `self-update --rollback` báo thành công nhưng dịch vụ khởi động lại liên tục. Đường ra là chạy `install.sh` của gói mới; muốn về v0.1.1 thật thì chép lại bản sao `tpan.db` từ trước khi nâng. Cách khác là viết "migration ngược" (file xoá cột, xoá bảng); không làm vì bản cũ không có mã để chạy nó, và mất dữ liệu user, gói không thể tránh.

### 5.6 Hai lỗi thật e2e trên máy ảo tìm ra

Cả hai đã sửa ở gốc trước lần chạy được ghi lại trong log (xem chữ `dirty` ở đầu mục 2).

(1) Khoá, đổi mật khẩu hay xoá một user làm admin đang "xem như" user đó bị đăng xuất. Sai ở đâu: hàm huỷ phiên của store xoá mọi phiên có `user_name = X` hoặc `view_as = X`, mà phiên có `view_as = X` là phiên của admin. Phát hiện bằng: bước "thứ tự ngược" của P6 (admin đang xem như `khach_b` thì `khach_b` bị khoá bằng CLI). Sửa (commit `ab5b734`): hàm `endSessionsOf` trong `internal/store/users.go` xoá phiên của X nhưng chỉ gỡ `view_as` khỏi phiên của admin. Bằng chứng sau khi sửa: M13.log dòng 4201 tới 4235, `GET /session` của admin sau khi khoá `khach_b` ra `{"name":"admin","role":"admin"}`.

(2) User thấy tên site của người khác qua phép kiểm cô lập. Phép kiểm `isolation:<x>:<y>` (M2) chạy cho mọi cặp site trên máy, bất kể chủ, nên kết quả health, phép kiểm của trang site và sự kiện của job `site.add` của A có tên, đường dẫn, user Linux của site B. Phát hiện bằng: e2e thấy ở `GET /overview`, `GET /sites/{site của A}`, `GET /jobs/{job site.add của A}`. Sửa (commit `a14caa1`): `foreignCheck` trong `internal/server/api_m13.go` cho biết một ID `isolation:` có nhắc tới site không phải của người gọi, và `keepChecks`, `keepEvents`, `keepIncidents` bỏ các kết quả đó trước khi trả cho user thường; cả luồng SSE (`sse.go`). Bằng chứng sau khi sửa: M13.log dòng 1275 tới 1300 (`A: GET /sites/a1.example.com ...: không có b1.example.com, s1.example.com; vẫn có isolation:a1.example.com:a2.example.com`).

Còn hở, ghi ở KE-HOACH.md: lời từ chối của API có thể kèm danh sách phép kiểm (`ErrorBody.Checks`) mà chưa lọc phép kiểm cô lập; dòng backup trả qua API chưa có trường `owner`.

## 6. Tự kiểm

1. Một bạn thêm route mới `GET /api/v1/sites/{domain}/stats` vào bảng nhưng quên ghi `rule`. Chuyện gì xảy ra, ở lúc nào? Nếu bạn ấy ghi `rule: anyone` thay vì `ownerSite` thì sao, và test nào bắt được?
2. Vì sao A gọi `GET /sites/b1.example.com` nhận 404, mà gọi `GET /users` lại nhận 403? Đổi cái đầu thành 403 thì mất gì?
3. Xoá nhánh `if !found { hash, err = authDummyHash() ... }` trong `authCheckPassword` thì đăng nhập vẫn đúng chứ? Vậy mất gì?
4. Trong `suspendSteps`, nếu đảo thứ tự cho bước tắt pool chạy trước bước ghi vhost tạm khoá thì khách vào site thấy gì trong khoảnh khắc giữa hai bước?
5. Có người sửa `stack_switch.go` để gọi thẳng `steps.SiteVhost` thay vì `siteVhost`. Sau `tpan stack switch --to apache`, site đang khoá ra sao? Phần nào của e2e sẽ đỏ?
6. Khoá user A khi `a2.example.com` của A đã khoá riêng từ trước. Mở khoá A thì `a2` thế nào, và mã nào quyết định điều đó?
7. Job `user.suspend` khoá user `khach_a` có `owner` là gì? Vì sao? A có thấy job đó không?
8. (Cần máy ảo) Chứng minh `a2.example.com` đang khoá mà không gọi HTTP: pool của nó và file cron của nó đang ở trạng thái nào?
9. (Cần máy ảo) Chứng minh user DB `khach_a_d1` đang bị khoá bằng hai cách: một cách hỏi MariaDB, một cách thử đăng nhập. Thêm: nếu `DBKillConnections` lọc `PROCESSLIST` bằng `USER = ... AND HOST = 'localhost'` thay vì chỉ `USER` thì khoá sót gì?
10. `apply?dry_run=1` của A với domain `b1.example.com` trả dòng `refused`, mã `site_not_found`. Đọc câu `reason` ở M13.log dòng 1158. A có biết được domain đó đã có người dùng không?

## Đáp án

1. `mount` gọi `guard` cho từng route khi server dựng handler; `rule` để trống là giá trị zero `ruleUnset`, rơi vào nhánh `default` và `panic("route ... không khai luật ...")`. `tpan serve` dừng ngay lúc khởi động, và `TestPhanQuyenMoiRoute` (dựng bảng thật) cũng panic. Nếu ghi `anyone`: server chạy, B gọi `/sites/a.example.com/stats` tới được handler. `TestPhanQuyenMoiRoute` không biết ý định của người viết nên không bắt được trường hợp này: với `anyone` nó chỉ muốn B tới handler, và B tới thật. Thứ bắt được là test riêng của handler đó, hoặc người review nhìn thấy `{domain}` đi cùng `anyone`. Đây là giới hạn của "mặc định là cấm": nó bắt được chuyện quên, không bắt được chuyện chọn sai.
2. `GET /sites/{domain}` là luật `ownerSite`: câu trả lời phụ thuộc site có tồn tại và thuộc ai. 404 cho cả "không có" lẫn "của người khác" (cùng nhánh trong `ownsPath`) nên A không phân biệt được. `GET /users` là `adminOnly`: câu trả lời chỉ phụ thuộc vai của A, giống nhau với mọi dữ liệu, nên 403 không lộ gì. Đổi cái đầu thành 403 thì A thử từng tên miền là biết tên nào có trên máy (403) và tên nào không (404), tức là dò được danh sách khách.
3. Không, và hỏng còn tệ hơn lộ thời gian. Bỏ nhánh đó thì với tên không có, `hash` là chuỗi rỗng. `store.VerifyPassword` (`internal/store/auth.go`) tách hash theo dấu `$` và đòi đúng 6 phần dạng `argon2id`; chuỗi rỗng không qua nên nó trả lỗi `hash mật khẩu không phải argon2id dạng PHC`. `authCheckPassword` gặp lỗi đó thì gọi `authInternal` và trả `500 internal`, không đi tới `authFailed`. Hệ quả: tên có thật mà sai mật khẩu ra 401, tên không có ra 500. Chỉ nhìn mã HTTP là biết tên nào tồn tại, không cần bấm giờ. Và vì `authFailed` không chạy, bộ đếm 5 lần sai không tăng, nên dò tên không bị khoá IP. Đó là lý do chú thích trên `authDummy` viết "Phải là hash thật". Suy theo mã, chưa chạy thử trên máy ảo.
4. Pool tắt thì socket biến mất, mà nginx còn dùng vhost thường và chuyển `.php` tới socket đó: khách thấy 502 Bad Gateway cho tới khi vhost tạm khoá được nạp. Chú thích đầu `internal/core/suspend.go` ghi đúng lý do này. Chưa chạy thử thứ tự đảo trên máy ảo.
5. Vhost thường được ghi đè lên vhost tạm khoá: site hết 403. Nhưng pool vẫn `.suspended` nên không có socket, và khách thấy 502 thay vì trang tạm khoá; bảng `sites` vẫn ghi `suspended`. Phần chuyển sang Apache của P4 sẽ đỏ ở dòng `http://s1.example.com/ -> 403 kèm trang tạm khoá` (M13.log dòng 2207). Phần "khách thấy 502" là suy luận theo mã, chưa chạy thử. Đó là lý do quyết định 8 bắt mọi chỗ ghi vhost đi qua `siteVhost`.
6. `a2` vẫn khoá. Khoá user bỏ qua thứ đã khoá (`st.Suspended != account.NotSuspended` thì `continue` trong `PrepareSuspendUser`), nên `a2` giữ nguồn `self`. Mở khoá user chỉ chọn `st.Suspended == account.SuspendByUser` trong `PrepareUnsuspendUser`, nên `a2` không bị chạm. Bằng chứng: 2.7, sau `user unsuspend` thì `a2` vẫn `http=403 marker=1`, `khach_a_d1` vẫn 4151.
7. `owner: admin` (M13.log dòng 3590). `target` là `khach_a`, một tên user, không trùng domain nào trong `sites` hay tên nào trong `databases`; `actor` rỗng vì chạy bằng CLI; nên `COALESCE` trong `CreateJob` rơi xuống `'admin'`. A không thấy job đó (danh sách job của A lọc theo `owner`). Còn job `dbs.suspend` trên `khach_a_d1` thì thuộc `khach_a`, vì tên DB nằm trong bảng `databases`.
8. Chạy trên Mac: `scripts/vm.sh root bash -c 'ls /etc/php/8.3/fpm/pool.d/ /etc/cron.d/ | grep a2_example; ls /run/php/ | grep a2_example'`. Mong đợi: `tpan-a2_example_com_8a907e.conf.suspended` trong `pool.d`, `tpan-a2_example_com_8a907e.suspended` trong `cron.d`, và không có socket `tpan-a2_example_com_8a907e-8.3.sock` trong `/run/php`. Log có cùng kết quả qua khối lệnh của kịch bản (M13.log dòng 1990 tới 1993: `pool: tpan-a2_example_com_8a907e.conf.suspended`, `socket: không có`, `cron: tpan-a2_example_com_8a907e.suspended`). Lệnh `ls` gộp như trên thì mình chưa chạy. Slug `8a907e` là của lần chạy đó; máy của bạn sẽ khác.
9. Hỏi MariaDB (chạy trên Mac): `scripts/vm.sh root mariadb -N -B -e 'SELECT User, JSON_VALUE(Priv, "$.account_locked") FROM mysql.global_priv WHERE User = "khach_a_d1"'`, mong đợi `khach_a_d1	1`. Thử đăng nhập: cần mật khẩu của user DB (in một lần lúc `db add` hay `db passwd`); kịch bản ghi nó vào file `/root/e2e-khach_a_d1.cnf` quyền 600 rồi chạy `mariadb --defaults-extra-file=/root/e2e-khach_a_d1.cnf -e 'SELECT CURRENT_USER()'`, ra `ERROR 4151 (HY000): Access denied, this account is locked` (2.6). Đặt mật khẩu trong file thay vì `-p<mật khẩu>` để nó không lọt vào `ps` và lịch sử shell. Phần thêm: lọc thêm `HOST = 'localhost'` thì sót mọi kết nối qua TCP. Kết nối qua socket có HOST `localhost`, kết nối TCP có HOST `localhost:<cổng>` (2.6: `localhost:42106`). Chương trình nối bằng `127.0.0.1` sẽ giữ được kết nối mở sau khi khoá và tiếp tục đọc ghi DB, tức là khoá không có tác dụng với nó.
10. Có. Mã lỗi là `site_not_found` nhưng câu là `không tạo được site b1.example.com từ tài khoản này: domain đã được dùng`. Và kể cả nếu câu chữ giấu đi, tạo site thì luôn lộ chuyện tên đã có: một domain chưa ai dùng sẽ được tạo (202), domain đã có thì không. Cùng chuyện đó xảy ra ở `POST /sites` với domain của B (404 thay vì 202). Không có cách nào cho tạo site mà giấu hoàn toàn chuyện một domain đã có trên máy; thứ 404 giấu được là nội dung của site đó (chủ, DB, job, log). KE-HOACH.md ghi một chuyện cùng loại cho tên DB rời (`db_exists`) và chấp nhận nó.
