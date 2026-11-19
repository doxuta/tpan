# M1: cài stack LEMP hoặc LAMP bằng `tpan stack install`

Đọc kèm: `00-go-can-ban.md` (những phần Go dùng trong file này), `docs/KE-HOACH.md` mục 4 (kiến trúc), 6.1 (luồng cài stack), 11 (bảo mật), `docs/bang-chung/M1.log` (log thật trên máy ảo).

## 1. Mốc này làm được gì

- Trên một máy Ubuntu 24.04 sạch, `sudo tpan stack install --web nginx` (hoặc `--web apache`) cài web server, PHP-FPM, MariaDB, certbot, WP-CLI, rồi siết và kiểm chứng chúng.
- `--dry-run` in trước từng bước sẽ làm, lý do, lệnh sẽ chạy, file sẽ ghi, mà không đổi gì trên máy.
- Chạy lại lệnh cài trên máy đã cài thì không bước nào chạy và máy không đổi gì. Bước lỗi giữa chừng thì các bước trước được hoàn tác theo thứ tự ngược.
- `tpan stack status` cho biết cấu hình và ba dịch vụ; `tpan health` chạy lại đúng 10 phép kiểm dùng lúc cài, mã thoát 1 khi có lỗi.
- Site mặc định hứng mọi request lạ: Nginx đóng kết nối (mã 444), Apache trả 403. Chỉ thư mục thử thách Let's Encrypt được mở.

## 2. Chạy thử

Mọi output dưới đây lấy nguyên văn từ `docs/bang-chung/M1.log`, PHẦN 7 (lần 5, ngày 19/11/2026, 13:23 đến 13:25), là lần chạy cuối cùng đạt trọn: 104 khẳng định đạt, 0 không đạt. Máy ảo `tpan-test` là Multipass, Ubuntu 24.04, arm64.

Log in dòng lệnh đã mất dấu nháy (script `scripts/e2e-m1.sh` in `$*`). Lệnh dưới đây mình viết lại với dấu nháy đúng để bạn dán vào zsh được; output thì giữ nguyên. `<ip-may-ao>` là IP của máy ảo đã được che.

Toàn bộ kịch bản tự động: chạy trên Mac, `scripts/e2e-m1.sh all > /tmp/m1.log`. Từng bước tay như sau.

### 2.1 Đưa máy ảo về sạch và chép tpan vào

Chạy trên Mac, trong thư mục repo:

```bash
make vm-reset      # về snapshot "clean"
make vm-deploy     # build tpan cho Linux rồi chép vào máy ảo
scripts/vm.sh exec tpan version
```

```
tpan 7e04926-dirty (commit 7e04926, build 2026-11-19T06:23:33Z)
```

`scripts/vm.sh exec` chạy một lệnh trong máy ảo bằng user thường, `scripts/vm.sh root` chạy bằng root. Lệnh gõ ở Mac nhưng chạy trong máy ảo.

### 2.2 Xem trước, không đổi gì

Chạy trên Mac (lệnh chạy trong máy ảo bằng root):

```bash
scripts/vm.sh root tpan stack install --web nginx --dry-run
```

Output (mình bớt các bước ở giữa, đánh dấu bằng dòng "bớt dòng"):

```
Kế hoạch: Cài stack LEMP (Nginx)
14 bước sẽ chạy, 1 bước đã đúng sẽ bỏ qua, 2 bước có thể gián đoạn dịch vụ.

 1. [sẽ chạy] Làm mới danh sách gói (apt-get update)  (apt.update:lists)
    Vì sao: apt chỉ cài được bản gói mà nó biết. Máy mới tạo thường có danh sách gói cũ hoặc trống, nên bỏ bước này thì bước cài gói báo không tìm thấy gói hoặc cài bản cũ còn lỗ hổng.
    $ apt-get update
    Ghi chú hoàn tác: apt-get update chỉ làm mới danh sách gói, không cài hay gỡ gì, nên không có gì để hoàn tác.
... (bớt dòng)
 5. [sẽ chạy] Siết MariaDB: root qua unix_socket, bỏ user ẩn danh và DB test  (mariadb.secure:localhost)
    NGUY HIỂM: bước này có thể làm gián đoạn dịch vụ.
    Vì sao: User ẩn danh cho bất kỳ ai trên máy vào MariaDB không cần mật khẩu; DB test thì ai cũng ghi được. root dùng unix_socket nghĩa là chỉ root của máy vào được root của DB, không có mật khẩu nào để đoán hay để lộ.
    $ mariadb -N -B
    Ghi chú hoàn tác: Không hoàn tác: không có lý do gì để tạo lại user ẩn danh hay DB test, cả hai đều là lỗ hổng.
    (chưa kiểm được lúc xem trước: đọc trạng thái bảo mật của MariaDB: chạy mariadb -N -B: exec: "mariadb": executable file not found in $PATH)
... (bớt dòng)
 9. [đã đúng, bỏ qua] Gỡ /etc/nginx/sites-enabled/default  (absent:/etc/nginx/sites-enabled/default)
10. [sẽ chạy] Ghi /etc/nginx/sites-available/tpan-default.conf  (file:/etc/nginx/sites-available/tpan-default.conf)
    Vì sao: Dịch vụ đọc cấu hình từ file này. Thiếu nó hoặc sai nội dung thì dịch vụ chạy theo cấu hình khác với cấu hình tpan đã kiểm. tpan kiểm cú pháp trước khi nạp, sai thì trả file cũ về.
    $ nginx -t
    $ systemctl reload nginx
    tạo /etc/nginx/sites-available/tpan-default.conf
... (bớt dòng)
15. [sẽ chạy] Ghi /etc/logrotate.d/tpan-sites  (file:/etc/logrotate.d/tpan-sites)
    Vì sao: Dịch vụ đọc cấu hình từ file này. Thiếu nó hoặc sai nội dung thì dịch vụ chạy theo cấu hình khác với cấu hình tpan đã kiểm. tpan kiểm cú pháp trước khi nạp, sai thì trả file cũ về.
    $ logrotate -d /etc/logrotate.d/tpan-sites
    tạo /etc/logrotate.d/tpan-sites
```

Hai chỗ lạ đáng để ý, người review hay hỏi:

- Bước 9 báo "đã đúng, bỏ qua" vì lúc xem trước nginx chưa được cài nên file `sites-enabled/default` chưa có. Lúc chạy thật, bước 2 cài nginx tạo ra file đó, nên bước 9 lại chạy ("xong" ở mục 2.3). Xem trước nhìn máy ở hiện tại, không đoán trước được tác dụng của các bước phía trên.
- Bước 5 "chưa kiểm được lúc xem trước" vì chương trình `mariadb` chưa có. Engine coi đó là bước sẽ chạy, không phải lỗi (trường `CheckError` của `engine.StepPreview`).

Xem trước không để lại gì. Kịch bản kiểm ngay sau đó (chạy trên Mac):

```bash
scripts/vm.sh exec dpkg -l nginx
```

```
dpkg-query: no packages found matching nginx
```

và các thư mục `/var/lib/tpan`, `/var/lib/tpan/tpan.db`, `/etc/tpan`, `/var/cache/tpan`, `/run/tpan` đều "không có".

### 2.3 Cài thật

Chạy trên Mac:

```bash
scripts/vm.sh root tpan stack install --web nginx --yes
```

`--yes` để không hỏi "Chạy? [y/N]" (script không có ai trả lời). Output, đủ:

```
Bắt đầu job 1041ee1474639236
- Làm mới danh sách gói (apt-get update) ...
  xong
- Cài gói: mariadb-server php-fpm php-cli php-mysql php-curl php-gd php-mbstring php-xml php-zip php-intl certbot curl nginx ...
  xong
- Chạy và bật mariadb khi khởi động ...
  đã đúng, bỏ qua
- Ghi /etc/mysql/mariadb.conf.d/99-tpan.cnf ...
  xong
- Siết MariaDB: root qua unix_socket, bỏ user ẩn danh và DB test ...
  đã đúng, bỏ qua
- Dò bản PHP-FPM đã cài ...
  đã đúng, bỏ qua
- Tạo thư mục /var/lib/tpan ...
  xong
- Tạo thư mục /var/lib/tpan/acme ...
  xong
- Gỡ /etc/nginx/sites-enabled/default ...
  xong
- Ghi /etc/nginx/sites-available/tpan-default.conf ...
  xong
- Bật /etc/nginx/sites-enabled/tpan-default.conf → /etc/nginx/sites-available/tpan-default.conf ...
  xong
- Chạy và bật nginx khi khởi động ...
  đã đúng, bỏ qua
- Tạo thư mục /var/cache/tpan ...
  xong
- Cài WP-CLI 2.12.0 vào /usr/local/bin/wp ...
  xong
- Ghi /etc/logrotate.d/tpan-sites ...
  xong
  [đạt] Dịch vụ nginx đang chạy và tự bật cùng máy
  [đạt] Dịch vụ php8.3-fpm đang chạy và tự bật cùng máy
  [đạt] Dịch vụ mariadb đang chạy và tự bật cùng máy
  [đạt] Cấu hình nginx hợp lệ
  [đạt] Cấu hình PHP-FPM 8.3 hợp lệ
  [đạt] Cổng 3306 chỉ nghe ở 127.0.0.1
  [đạt] Site mặc định của nginx đóng kết nối request lạ
  [đạt] MariaDB an toàn: root dùng unix_socket, không user ẩn danh, không DB test
  [đạt] WP-CLI có sẵn, đúng quyền và đúng mã sha512
  [đạt] Ổ đĩa chứa / còn chỗ trống

Job 1041ee1474639236: thành công
Kiểm chứng:
  [đạt] Dịch vụ nginx đang chạy và tự bật cùng máy: nginx đang chạy và tự bật khi khởi động máy
  [đạt] Dịch vụ php8.3-fpm đang chạy và tự bật cùng máy: php8.3-fpm đang chạy và tự bật khi khởi động máy
  [đạt] Dịch vụ mariadb đang chạy và tự bật cùng máy: mariadb đang chạy và tự bật khi khởi động máy
  [đạt] Cấu hình nginx hợp lệ: Cấu hình nginx hợp lệ
  [đạt] Cấu hình PHP-FPM 8.3 hợp lệ: Cấu hình PHP-FPM 8.3 hợp lệ
  [đạt] Cổng 3306 chỉ nghe ở 127.0.0.1: Cổng 3306 chỉ nghe ở 127.0.0.1
  [đạt] Site mặc định của nginx đóng kết nối request lạ: nginx đóng kết nối request lạ như mong đợi (mã 444)
  [đạt] MariaDB an toàn: root dùng unix_socket, không user ẩn danh, không DB test: MariaDB an toàn: root dùng unix_socket, không user ẩn danh, không DB test
  [đạt] WP-CLI có sẵn, đúng quyền và đúng mã sha512: WP-CLI có sẵn, quyền 0755, sha512 khớp
  [đạt] Ổ đĩa chứa / còn chỗ trống: Ổ đĩa chứa / đã dùng 13.1%
```

Ba bước "đã đúng, bỏ qua" ngay lần đầu, và vì sao:

- `service:mariadb`: gói `mariadb-server` của Ubuntu tự bật và chạy dịch vụ khi cài. `Service.Check` thấy `is-active` và `is-enabled` đều đạt.
- `mariadb.secure:localhost`: MariaDB 10.11.14 của Ubuntu 24.04 mới cài đã đạt cả ba điều (đã kiểm ở mục 16 của KE-HOACH.md). Bước vẫn giữ để bắt máy không sạch.
- `php.detect:/etc/php`: `DetectPHP.Check` tự dò thấy `/etc/php/8.3/fpm` (do bước cài gói vừa tạo), ghi bản `8.3` và trả "đã đúng". Bước này không bao giờ cần `Apply` trên máy đã có PHP-FPM.

### 2.4 Kiểm độc lập với tpan

Đây là lệnh của chính Linux, không qua tpan, để không phải tin lời tpan. Chạy trên Mac:

```bash
scripts/vm.sh root ss -tlnp
scripts/vm.sh exec curl -sS -m 5 -o /dev/null -w '%{http_code}\n' http://127.0.0.1/
scripts/vm.sh exec curl -sS -m 5 -o /dev/null -w '%{http_code}\n' http://127.0.0.1:443/
```

```
State  Recv-Q Send-Q Local Address:Port Peer Address:PortProcess
LISTEN 0      80         127.0.0.1:3306      0.0.0.0:*    users:(("mariadbd",pid=13543,fd=22))
LISTEN 0      4096   127.0.0.53%lo:53        0.0.0.0:*    users:(("systemd-resolve",pid=477,fd=15))
LISTEN 0      4096      127.0.0.54:53        0.0.0.0:*    users:(("systemd-resolve",pid=477,fd=17))
LISTEN 0      511          0.0.0.0:80        0.0.0.0:*    users:(("nginx",pid=13630,fd=9),("nginx",pid=13629,fd=9),("nginx",pid=10078,fd=9))
LISTEN 0      4096         0.0.0.0:22        0.0.0.0:*    users:(("sshd",pid=810,fd=3),("systemd",pid=1,fd=133))
LISTEN 0      511             [::]:80           [::]:*    users:(("nginx",pid=13630,fd=10),("nginx",pid=13629,fd=10),("nginx",pid=10078,fd=10))
LISTEN 0      4096            [::]:22           [::]:*    users:(("sshd",pid=810,fd=4),("systemd",pid=1,fd=134))
```

```
curl: (52) Empty reply from server
000
```

```
curl: (7) Failed to connect to 127.0.0.1 port 443 after 0 ms: Couldn't connect to server
000
```

Đọc: 3306 chỉ ở `127.0.0.1`; nginx nghe cổng 80 cả IPv4 và IPv6; không ai nghe 443; request lạ vào cổng 80 bị đóng kết nối không trả gì (curl mã thoát 52, mã HTTP `000`). Từ Mac gọi `curl http://<ip-may-ao>/` cũng ra `curl: (52) Empty reply from server`.

MariaDB, chạy trên Mac:

```bash
scripts/vm.sh root mariadb -e 'SHOW GRANTS FOR root@localhost'
```

```
Grants for root@localhost
GRANT ALL PRIVILEGES ON *.* TO `root`@`localhost` IDENTIFIED VIA mysql_native_password USING 'invalid' OR unix_socket WITH GRANT OPTION
GRANT PROXY ON ''@'%' TO 'root'@'localhost' WITH GRANT OPTION
```

Quyền thư mục tpan tạo, chạy trên Mac:

```bash
scripts/vm.sh root stat -c '%a %U:%G %n' /var/lib/tpan /var/lib/tpan/acme /var/lib/tpan/tpan.db
```

```
755 root:root /var/lib/tpan
755 root:root /var/lib/tpan/acme
600 root:root /var/lib/tpan/tpan.db
```

Site mặc định vẫn phục vụ thư mục thử thách Let's Encrypt cho mọi Host: kịch bản đặt một file thử vào `/var/lib/tpan/acme/.well-known/acme-challenge/tpan-e2e`, rồi gọi với `-H 'Host: la.example'` và nhận `tpan-e2e-acme-ok` kèm mã `200`.

### 2.5 Chạy lại: không đổi gì

Kịch bản chụp trước md5 các file config, symlink, quyền thư mục và PID dịch vụ, chạy lại, chụp lần nữa rồi `diff`. Chạy trên Mac:

```bash
scripts/vm.sh root tpan stack install --web nginx --yes --verbose
```

`--verbose` in mọi lệnh tpan chạy (dòng `$`) và output của chúng (dòng `|`). Phần đầu và phần cuối:

```
Bắt đầu job 84b2cd9a0877e672
- Làm mới danh sách gói (apt-get update) ...
    $ dpkg-query --show '--showformat=${Package}\t${Status}\n' mariadb-server php-fpm php-cli php-mysql php-curl php-gd php-mbstring php-xml php-zip php-intl certbot curl nginx
... (bớt dòng)
  đã đúng, bỏ qua
... (bớt dòng)
- Siết MariaDB: root qua unix_socket, bỏ user ẩn danh và DB test ...
    $ mariadb -N -B
    | 1	0	0
  đã đúng, bỏ qua
... (bớt dòng)
Job 84b2cd9a0877e672: thành công
```

Kịch bản đếm và gom các lệnh tpan đã chạy trong lần hai:

```
Số bước: 15, số bước bỏ qua: 15
  => ĐẠT: mọi bước đều bỏ qua

Các lệnh tpan đã chạy trong lần hai (dòng "$"), gom và đếm:
   2 dpkg-query --show '--showformat=${Package}\t${Status}\n' mariadb-server php-fpm php-cli php-mysql php-curl php-gd php-mbstring php-xml php-zip php-intl certbot curl nginx
   1 mariadb -N -B
   1 systemctl is-active mariadb
   1 systemctl is-active nginx
   1 systemctl is-enabled mariadb
   1 systemctl is-enabled nginx
  => ĐẠT: chỉ có lệnh đọc (dpkg-query, systemctl is-*, mariadb -N -B đọc trạng thái, a2query -m)
```

Và `diff` ảnh chụp trước, sau ra rỗng, mã thoát 0: `=> ĐẠT: máy không đổi (md5, symlink, quyền, PID)`. PID không đổi nghĩa là không dịch vụ nào bị restart.

### 2.6 status, health, và từ chối đổi web server

Chạy trên Mac:

```bash
scripts/vm.sh root tpan stack status
scripts/vm.sh root tpan stack install --web apache --yes
```

```
Web server:  nginx (nguồn ubuntu)
PHP:         8.3 (nguồn ubuntu)
MariaDB:     nguồn ubuntu
Đã cài:      rồi
Dịch vụ:
  [đạt] Dịch vụ nginx đang chạy và tự bật cùng máy: nginx đang chạy và tự bật khi khởi động máy
  [đạt] Dịch vụ php8.3-fpm đang chạy và tự bật cùng máy: php8.3-fpm đang chạy và tự bật khi khởi động máy
  [đạt] Dịch vụ mariadb đang chạy và tự bật cùng máy: mariadb đang chạy và tự bật khi khởi động máy
```

```
Từ chối: máy này đã được tpan cài với nginx; muốn đổi sang apache thì dùng `tpan stack switch --to apache` (chưa có ở bản này)
```

Lệnh thứ hai thoát mã 1, `dpkg -l apache2` sau đó vẫn báo không có gói. `scripts/vm.sh root tpan health` in 10 dòng `[đạt]` giống phần "Kiểm chứng" ở 2.3 và dòng cuối `10 đạt`, mã thoát 0.

### 2.7 Apache

Kịch bản đưa máy ảo về sạch rồi làm lại các bước trên với `--web apache`. Những chỗ khác Nginx, chạy trên Mac:

```bash
scripts/vm.sh exec curl -sS -m 5 -o /dev/null -w '%{http_code}\n' http://127.0.0.1/
scripts/vm.sh root apachectl -S
scripts/vm.sh root ss -tlnH
```

```
403
```

```
VirtualHost configuration:
*:80                   tpan-default.invalid (/etc/apache2/sites-enabled/000-tpan-default.conf:7)
```

```
LISTEN 0      4096   127.0.0.53%lo:53   0.0.0.0:*
LISTEN 0      80         127.0.0.1:3306 0.0.0.0:*
LISTEN 0      4096         0.0.0.0:22   0.0.0.0:*
LISTEN 0      4096      127.0.0.54:53   0.0.0.0:*
LISTEN 0      511                *:80         *:*
LISTEN 0      4096            [::]:22      [::]:*
```

`apachectl -M` liệt kê `mpm_event_module`, `proxy_fcgi_module`, `http2_module`, không có `mpm_prefork`, không có module PHP, không có `ssl_module`. Không ai nghe 443. Lần cài Apache có 16 bước (thêm bước module), chạy lại thì 16 bước đều bỏ qua.

### 2.8 Những lần hỏng trong log, và hoàn tác chạy thật

`M1.log` giữ cả các lần hỏng, không xoá. Lần 1 (PHẦN 2) và lần 3 (PHẦN 5) hỏng ở bước cài gói: apt trong máy ảo tải gói nginx `1.24.0-2ubuntu7.20` và nhận 404 từ kho `ports.ubuntu.com`. PHẦN 4 của log gọi thẳng vào từng máy đứng sau tên `ports.ubuntu.com`: ba máy trả 404, ba máy trả 200. Kho đang đồng bộ dở, không phải lỗi của tpan; lần nào apt trúng máy thiếu file thì hỏng.

Đoạn cuối của lần 1 (M1.log, dòng 1123 đến 1126):

```
- Hoàn tác: Làm mới danh sách gói (apt-get update) ...
  đã hoàn tác

Job a69a70ac3e2a0791: hỏng, đã hoàn tác sạch
```

Nói đúng mức về đoạn này. Nó chứng minh đường hoàn tác của engine chạy trên máy thật: bước lỗi làm job dừng, engine gọi Undo ngược cho các bước đã Apply, job kết thúc ở trạng thái `rolled_back` và CLI in "hỏng, đã hoàn tác sạch". Nhưng bước duy nhất đã Apply là `apt-get update`, mà `AptUpdate.Undo` không làm gì. `AptInstall` lỗi ngay trong Apply nên engine không gọi Undo cho nó. Máy vẫn sạch (kịch bản thấy `mariadb`, `nginx` đều `not-found`) vì apt tải hết gói trước rồi mới bung, nên chết lúc tải thì chưa gói nào được cài. Việc trả lại một file thật (vhost, symlink, module Apache) khi bước sau lỗi mới được kiểm bằng unit test trên `FakeSystem` (`TestUndoReverseOrderOnlyApplied` trong `internal/engine/engine_test.go`, các test trong `internal/steps/`), chưa có trên máy ảo. E2e bước 6 của mục 12 (cho bước ghi vhost lỗi có chủ ý) thuộc M2.

## 3. Đi qua mã theo thứ tự chạy

Theo đúng đường đi của `tpan stack install --web nginx --yes`, từ lúc gõ lệnh tới lúc máy đổi.

### 3.1 `cmd/tpan/main.go`: `main` và `run`

`main` tạo `ctx` bị huỷ khi nhận Ctrl+C hoặc SIGTERM, gọi `run`, rồi thoát bằng mã `run` trả về. `run` tách khỏi `main` để test gọi được mà không thoát tiến trình.

```go
func run(ctx context.Context, args []string, e env) int {
	root := newRoot(&e)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	var ec *exitCode
	switch {
	case err == nil:
		return 0
	case errors.As(err, &ec):
		// Lệnh đã tự in lý do.
		return ec.code
	default:
		// Lỗi cờ, lỗi đọc DB...: in một dòng, không stack trace.
		printf(e.stderr, "tpan: %v\n", err)
		return 1
```

`env` (trong `cmd/tpan/root.go`) gom mọi thứ CLI lấy từ bên ngoài: `sys`, `isRoot`, `isTerminal`, `openStore`, stdin, stdout. `realEnv` gắn chúng với máy thật (`system.NewLocal()`); test trong `cmd/tpan/main_test.go` thay bằng `FakeSystem` và bộ đệm. Cây lệnh dựng bằng thư viện cobra trong `newRoot`; cờ chung là `--lang` (vi hoặc en) và `--db` (ẩn, mặc định `/var/lib/tpan/tpan.db`).

### 3.2 `cmd/tpan/stack.go`: `stackInstallCmd`

Hàm `RunE` của lệnh làm theo thứ tự: `core.ParseWeb(web)` (chỉ nhận `nginx` hoặc `apache`), `requireRoot` (từ chối sớm khi không chạy bằng root), mở DB, `PrepareInstall`, xem trước hoặc hỏi xác nhận, `Execute`, in báo cáo.

Chỗ đáng nói là `--dry-run` không được để lại gì, kể cả file DB:

```go
			// --dry-run không được để lại gì: chưa có file DB thì không tạo, chỉ
			// đọc như một bảng settings rỗng.
			s := &core.Stack{Sys: e.sys, Settings: core.EmptySettings{}}
			if !dryRun || e.fileExists(e.dbPath) {
				st, err := openStore(ctx, e)
				if err != nil {
					return err
				}
				defer closeStore(st)
				s.Settings = st
				if !dryRun {
					s.Journal = st
				}
			}
```

`store.Open` tạo file nếu chưa có, nên khi xem trước trên máy sạch, CLI dùng `core.EmptySettings{}` thay cho DB. Đó là lý do mục 2.2 thấy "không có /var/lib/tpan/tpan.db" sau `--dry-run`.

Không có `--yes` thì CLI in bản xem trước rồi hỏi `Chạy? [y/N]`; không có terminal (chạy trong script, qua pipe) hoặc đang `--json` thì từ chối với mã `confirm_required`, vì không có ai trả lời. Với `--json`, stdout chỉ chứa JSON của báo cáo, còn tiến độ ra stderr.

### 3.3 `internal/core/stack.go`: `PrepareInstall`, điều kiện đầu

`PrepareInstall` là phần chỉ đọc: đọc cấu hình từ settings (`Stack.Config`), kiểm điều kiện đầu, dựng Plan. Không ghi gì, không lấy khoá.

```go
	firstInstall := cfg.Web == ""
	if !firstInstall && cfg.Web != web {
		return nil, refuse("stack_switch_required", i18n.T(
			fmt.Sprintf("máy này đã được tpan cài với %s; muốn đổi sang %s thì dùng `tpan stack switch --to %s` (chưa có ở bản này)", cfg.Web, web, web),
			fmt.Sprintf("tpan already installed this machine with %s; to change to %s use `tpan stack switch --to %s` (not in this version yet)", cfg.Web, web, web)))
	}
	cfg.Web = web
	pkgs, err := Packages(cfg)
	if err != nil {
		return nil, err
	}
```

Điều kiện đầu là các phép kiểm của `internal/checks`, cùng loại với phép kiểm chứng sau khi cài: `checks.OSRelease("ubuntu", "24.04")` luôn chạy; `checks.PortsFree(80, 443)` và `checks.PackagesAbsent("nginx", "apache2", "mariadb-server", "mysql-server")` chỉ chạy ở lần cài đầu. Có phép kiểm nào `Fail` thì trả `*RefusedError` mã `precondition_failed`, kèm các kết quả lỗi để CLI in nguyên nhân và lệnh sửa tay.

Vì sao lần chạy lại bỏ kiểm cổng và gói: lúc đó máy đã có gói và cổng 80 bận, do chính tpan cài, kể cả sau một lần hỏng giữa chừng. Dấu hiệu "máy này thuộc tpan" là khoá `stack.web` trong settings (xem 3.5).

`Packages(cfg)` là chỗ duy nhất biết tên gói của stack. Không có gói `php`, `php8.3` hay `libapache2-mod-php*` (lý do ở mục 4, "MPM event và mod_php"). Nguồn gói khác `ubuntu` bị từ chối với mã `source_unsupported`; M12 sẽ thêm nguồn khác bằng cách sửa đúng hàm này.

### 3.4 `internal/core/stack.go`: `installPlan` dựng Plan

Plan (`engine.Plan`) là danh sách Step theo thứ tự, cộng danh sách phép kiểm chứng. `installPlan` dựng đúng thứ tự mục 6.1 của KE-HOACH.md: `AptUpdate`, `AptInstall`, `Service{Unit: "mariadb"}`, `File` cho `99-tpan.cnf`, `MariaDBSecure`, `DetectPHP`, hai `Dir`, phần web server, `Service` cho web server, `Dir` cho cache, `WPCLI`, `File` cho logrotate.

Phần riêng của Apache:

```go
		avail := "/etc/apache2/sites-available/000-tpan-default.conf"
		ss = append(ss,
			&steps.ApacheModules{
				// Chưa bật ssl ở M1: bật nó là Apache nghe cổng 443 mà chưa có vhost
				// nào hứng, nên trang mặc định của Ubuntu lộ ra ở 443 (thấy trên máy
				// ảo, docs/bang-chung/M1.log). M5 bật ssl cùng một vhost 443 mặc định.
				Enable:  apacheModules,
				Disable: []string{"mpm_prefork"},
			},
			&steps.Absent{Path: "/etc/apache2/sites-enabled/000-default.conf", Test: test, Reload: reloadWeb},
			&steps.File{Path: avail, Content: conf, Perm: 0o644, Owner: system.Root, Test: test, Reload: reloadWeb},
			&steps.Symlink{Target: avail, Link: "/etc/apache2/sites-enabled/000-tpan-default.conf", Test: test, Reload: reloadWeb},
		)
```

Ba ý:

- Step chỉ nhận tham số (đường dẫn, nội dung, lệnh kiểm, lệnh reload); nó không biết đang cài stack hay tạo site. Cùng một `steps.File` sẽ ghi vhost của từng site ở M2.
- Nội dung config sinh từ template (`templates.ApacheDefault`, `templates.NginxDefault`, `templates.LogrotateSites` trong `internal/templates/templates.go`). Template nhúng trong file chạy và có golden test.
- Plan được dựng trước khi apt cài `php-fpm`, nên chưa biết bản PHP. `php := new(string)` được đưa cho `steps.DetectPHP{Result: php}` và cho phép kiểm qua `func() string { return *php }`; khi bước dò chạy, các phép kiểm sau đọc được giá trị mới.

`99-tpan.cnf` của MariaDB có `Danger: true`: đổi `bind-address` không reload được, phải `systemctl restart mariadb`, mọi kết nối đang mở bị cắt.

### 3.5 `internal/core/stack.go`: `Install.Execute`, settings trước rồi mới chạy

```go
	for _, kv := range [][2]string{
		{SettingWeb, string(in.Config.Web)},
		{SettingWebSource, string(in.Config.WebSource)},
		{SettingDBSource, string(in.Config.DBSource)},
		{SettingPHPSource, string(in.Config.PHPSource)},
	} {
		if err := s.Settings.SetSetting(ctx, kv[0], kv[1]); err != nil {
			return engine.Report{}, fmt.Errorf("ghi setting %s trước khi cài: %w", kv[0], err)
		}
	}
	rep, err := engine.New(s.Sys, engine.Options{Journal: s.Journal, OnEvent: onEvent}).Execute(ctx, in.Plan)
```

`stack.web` và ba nguồn gói được ghi ngay trước khi chạy Plan. `stack.php_version` chỉ được ghi sau khi job thành công. Lý do của cách chia này ở mục 5. Cũng vì vậy "stack đã cài" trong `Stack.Health` và `Stack.Status` nghĩa là `cfg.PHPVersion != ""`.

### 3.6 `internal/engine/engine.go`: `Preview`

Với mỗi bước: gọi `Info()` để lấy mô tả, đọc nội dung hiện có của mọi file bước sẽ ghi (để giao diện vẽ diff ở M3), rồi gọi `Check`. Không gọi `Apply`, không lấy khoá, không ghi DB. `Check` lỗi thì ghi vào `CheckError` và coi bước là sẽ chạy. `printPreview` trong `cmd/tpan/print.go` in kết quả thành mục 2.2.

### 3.7 `internal/engine/engine.go`: `Execute` lấy khoá rồi chạy từng bước

Trình tự: lấy khoá toàn cục (`e.sys.Lock`), sinh id job 16 ký tự hex, `Journal.CreateJob`, rồi chạy từng bước bằng `runStep`:

```go
func runStep(ctx context.Context, s Step, sys system.System) (skipped, applied bool, err error) {
	if err := ctx.Err(); err != nil {
		return false, false, fmt.Errorf("job bị huỷ trước khi chạy: %w", err)
	}
	var done bool
	check := func() (err error) { done, err = s.Check(ctx, sys); return err }
	if err := safe(check); err != nil {
		return false, false, fmt.Errorf("kiểm trước khi chạy: %w", err)
	}
	if done {
		return true, false, nil
	}
	if err := safe(func() error { return s.Apply(ctx, sys) }); err != nil {
		return false, false, fmt.Errorf("thực hiện: %w", err)
	}
```

Phần còn lại của hàm: gọi `Check` lần nữa; vẫn chưa đạt thì lỗi "đã chạy xong nhưng kiểm lại vẫn chưa đạt", và bước đó được đánh dấu đã Apply để còn được hoàn tác. Đây là cách engine không tin lời Apply: Apply trả nil chưa đủ, máy phải thật sự ở trạng thái mong muốn.

Chú ý dòng `Apply` lỗi trả `applied = false`. Engine chỉ gọi Undo cho bước đã Apply thành công, nên bước có Apply lỗi phải tự dọn trước khi báo lỗi. Đó là luật chung ghi ở đầu `internal/steps/common.go`, và là lý do có hàm `verifyOrRestore` (3.11).

### 3.8 `internal/engine/engine.go`: hoàn tác ngược và kiểm chứng

Khi có bước lỗi, `Execute` đi ngược danh sách bước đã Apply:

```go
		for k := len(applied) - 1; k >= 0; k-- {
			i := applied[k]
			sid := infos[i].ID
			r.emit(EventUndoStart, sid, "")
			if err := undoStep(ctx, p.Steps[i], recSys{System: e.sys, r: r, stepID: sid}); err != nil {
				status = JobUndoFailed
				err = fmt.Errorf("hoàn tác bước %s: %w", sid, err)
```

Undo lỗi không làm dừng việc hoàn tác các bước trước nó; job kết thúc ở `undo_failed` và CLI in "hỏng, và hoàn tác cũng lỗi: máy có thể đang dở dang". Undo chạy bằng ctx riêng không bị huỷ, hạn 5 phút (`undoTimeout`), vì người dùng bấm dừng thì vẫn phải hoàn tác.

Mọi bước xong thì `r.verify` chạy cả 10 phép kiểm chứng (kể cả sau một phép kiểm lỗi, để có đủ bằng chứng). Có phép kiểm `Fail` thì cũng coi là lỗi và hoàn tác như trên; `Warn` và `Unknown` không làm job lỗi. Job thành công thì engine ghi sha256 của mọi file đã ghi vào `Journal.RecordFile`, để health ở M4 phát hiện file bị sửa tay.

Mỗi Step và mỗi phép kiểm đều chạy trong `safe`, nên một panic trong mã Step thành lỗi thường và các bước trước vẫn được hoàn tác.

### 3.9 Sự kiện: `recSys`, `run.add`, và CLI in tiến độ

Trong lúc Execute, Step không nhận `System` gốc mà nhận `recSys`: một lớp bọc chỉ viết lại `Run`.

```go
func (s recSys) Run(ctx context.Context, cmd system.Cmd) (system.Result, error) {
	// Cmd.String() không in Stdin, nên mật khẩu đưa qua stdin không lọt vào log.
	s.r.emit(EventCommand, s.stepID, cmd.String())
	// Step đã đặt OnOutput riêng thì vẫn gọi nó, sau khi ghi sự kiện.
	own := cmd.OnOutput
	cmd.OnOutput = func(line string) {
		s.r.emit(EventOutput, s.stepID, line)
		if own != nil {
			own(line)
		}
	}
	return s.System.Run(ctx, cmd)
}
```

Mỗi lệnh thành sự kiện `command`, mỗi dòng output thành sự kiện `output`. `run.add` đánh số `Seq` tăng dần dưới một mutex, ghi vào `Journal.AddEvent` (bảng `job_events`), rồi gọi `Options.OnEvent`. Step không cần biết tới job hay DB.

Phía CLI, `progress.onEvent` trong `cmd/tpan/print.go` in một dòng cho mỗi bước bắt đầu, bỏ qua, xong, lỗi, hoàn tác. Output lệnh chỉ in khi `--verbose`; nếu không thì giữ 40 dòng cuối (`maxKeptLines`) của mỗi bước và chỉ in khi bước đó lỗi. Đó là lý do lần cài trơn tru ở mục 2.3 không đổ hàng nghìn dòng apt ra màn hình, còn lần hỏng ở mục 2.8 in ra đúng các dòng lỗi của apt.

### 3.10 `internal/steps/apt.go`: `AptInstall`

`Check` hỏi dpkg gói nào chưa cài (`missingPackages`, dùng `dpkg-query --show`), `Apply` chạy `apt-get install -y <gói...>`. Trước cả hai, `validate` chặn tên gói lạ và gói kéo theo mod_php:

```go
var modPHP = regexp.MustCompile(`^(php[0-9.]*|libapache2-mod-php.*)$`)

func (s *AptInstall) validate() error {
	if err := checkNames("gói", debName, s.Packages); err != nil {
		return err
	}
	for _, p := range s.Packages {
		if modPHP.MatchString(p) {
			return fmt.Errorf("không cài gói %q: nó kéo theo mod_php và buộc Apache về MPM prefork (mục 3 của KE-HOACH.md); dùng php-fpm", p)
		}
	}
	return nil
}
```

`debName` yêu cầu ký tự đầu là chữ hoặc số: argv không qua shell nên không chèn lệnh được, nhưng một "tên gói" như `-o...` vẫn bị apt-get đọc thành tuỳ chọn. `Undo` không làm gì và `Info` ghi lý do vào `NoUndo`: gỡ gói có thể kéo theo gỡ gói khác.

### 3.11 `internal/steps/files.go`: `File` có Test rồi Reload

`Apply` chụp trạng thái cũ của đường dẫn (`capture`: chưa có, symlink, hay file thường kèm nội dung, quyền, chủ), ghi file mới bằng `WriteFileAtomic`, rồi gọi `verifyOrRestore` trong `internal/steps/common.go`:

```go
func verifyOrRestore(ctx context.Context, sys system.System, prev saved, test, reload *system.Cmd, what string) error {
	if err := runTest(ctx, sys, test, what); err != nil {
		if rerr := prev.restore(sys); rerr != nil {
			return errors.Join(err, rerr)
		}
		return err
	}
	if err := runReload(ctx, sys, reload, what); err != nil {
		if rerr := prev.restore(sys); rerr != nil {
			return errors.Join(err, rerr)
		}
		if rerr := runReload(ctx, sys, reload, "trả lại "+prev.path); rerr != nil {
			return errors.Join(err, rerr)
		}
		return err
```

`nginx -t` sai thì trả file cũ về ngay, dịch vụ chưa nạp gì nên không bao giờ thấy config hỏng. Reload sai thì trả file cũ rồi reload thêm lần nữa để dịch vụ chạy lại bằng config cũ. `Symlink` và `Absent` dùng đúng hàm này. `Check` của `File` so cả nội dung, quyền và chủ; lệch một thứ là chưa đạt.

### 3.12 `internal/steps/apache.go`: `ApacheModules`, và hai Step khác

`Check` hỏi `a2query -m <mod>` cho từng module. `Apply` tắt trước rồi mới bật, vì `a2enmod mpm_event` từ chối khi `mpm_prefork` còn bật (hai MPM xung đột); sau đó `apachectl configtest`, rồi `systemctl restart apache2` (đổi MPM không reload được, nên bước này `Danger`). Bước nhớ đúng các module nó đã đổi (`s.enabled`, `s.disabled`), để `Undo` chỉ đảo những module đó, không đụng module vốn có.

Hai Step khác cần biết khi trả lời câu hỏi. `MariaDBSecure` (`internal/steps/mariadb.go`) đưa câu SQL vào `mariadb -N -B` qua `Cmd.Stdin`, không đặt trên dòng lệnh, vì `ps` thấy dòng lệnh của mọi tiến trình. `WPCLI` (`internal/steps/wpcli.go`) tải file phar bằng `curl` vào `/var/cache/tpan`, so sha512 bằng Go với hằng `DefaultWPCLISHA512`, khớp mới ghi vào `/usr/local/bin/wp` quyền 0755.

### 3.13 `internal/system`: argv, không shell, ghi file nguyên tử

`LocalSystem.Run` trong `internal/system/local.go`:

```go
func (s *LocalSystem) Run(ctx context.Context, cmd Cmd) (Result, error) {
	name, args := runArgv(cmd)
	bin, err := lookPath(name)
	if err != nil {
		return Result{ExitCode: -1}, fmt.Errorf("chạy %s: %w", cmd.String(), err)
	}

	c := exec.CommandContext(ctx, bin, args...)
	c.Args[0] = name // để ps hiện tên ngắn, như khi gõ tay
	c.Env = fixedEnv()
	c.Dir = "/"
	if len(cmd.Stdin) > 0 {
		c.Stdin = bytes.NewReader(cmd.Stdin)
	}
```

`system.Cmd` có `Name` và `Args []string`, mỗi phần tử là đúng một tham số. Không có trường nào nhận cả một câu lệnh shell. `exec.CommandContext` gọi thẳng chương trình (hàm hệ thống `execve`), không qua `/bin/sh`, nên dấu `;`, `|`, `$(...)` trong một tham số chỉ là chữ.

Môi trường của lệnh con cố định (`fixedEnv`): `PATH` chuẩn, `LC_ALL=C` để output là tiếng Anh ổn định (phép kiểm đọc output), `DEBIAN_FRONTEND=noninteractive` để apt không dừng lại hỏi, cộng `HOME=/root` khi chạy bằng root. Mã thoát khác 0 thành `*ExitError` kèm 5 dòng cuối của stderr.

`Cmd.String()` chỉ để hiển thị và làm bằng chứng; tham số lạ được đặt trong nháy đơn cho dễ đọc, và chuỗi này không bao giờ được đem đi chạy. Nó không in `Stdin`.

`WriteFileAtomic` ghi ra file tạm `.<tên>.tpan-*` trong cùng thư mục, `fsync`, `chmod`, `chown`, đóng, rồi `rename` đè lên file đích. Ai đọc file (nginx đang reload chẳng hạn) chỉ thấy file cũ hoặc file mới, không bao giờ thấy file ghi dở. Lỗi giữa chừng thì `defer` xoá file tạm.

`Lock` dùng `flock` trên `/run/tpan/tpan.lock` (mục 4, "flock").

`Secret` trong `internal/system/types.go`: một struct bọc giá trị bí mật, mọi cách in thường gặp (`fmt`, `slog`, JSON) đều ra `••••`; lấy giá trị thật phải gọi `Reveal`, và chỉ để đưa vào `Cmd.Stdin` hoặc file quyền 600. M1 chưa có mật khẩu nào nên chưa dùng tới; M2 dùng cho mật khẩu DB của site.

`FakeSystem` trong `internal/system/fake.go` làm mọi việc trên trong bộ nhớ, để unit test chạy trên Mac.

### 3.14 `internal/checks`: kiểm chứng

`stackChecks` trong `internal/core/stack.go` là bộ 10 phép kiểm, dùng chung cho kiểm chứng sau khi cài và cho `tpan health`:

```go
	return []checks.Check{
		checks.Service(webUnit(web), checks.StageWeb),
		lazy(func(v string) checks.Check { return checks.Service("php"+v+"-fpm", checks.StagePHP) }),
		checks.Service("mariadb", checks.StageDB),
		config,
		lazy(checks.PHPFPMConfig),
		checks.PortLocalOnly(3306, checks.StageDB),
		checks.HTTPDefault(string(web)),
		checks.MariaDBSecure(),
		checks.WPCLI(wpSHA),
		checks.Disk("/"),
	}
```

"Lúc cài đã đúng" và "bây giờ còn đúng" vì vậy cùng một định nghĩa. Mỗi phép kiểm là một `checks.Check` có method `Run` chỉ đọc, không trả lỗi: không kiểm được thì kết quả là `Unknown` kèm lý do. Kết quả (`checks.Result`) có `Evidence`: lệnh đã chạy, mã thoát, output thật, để giao diện hiện ra khi bấm vào. Khi không đạt thì có thêm `Cause` và `ManualFix` (các lệnh để người vận hành tự gõ, dòng cuối luôn là lệnh kiểm lại).

`checks.Lazy` dựng phép kiểm thật ở lúc chạy, vì lúc dựng Plan chưa biết bản PHP để biết tên dịch vụ `php8.3-fpm`.

`checks.HTTPDefault` (`internal/checks/http.go`) gọi thật `curl -sS --max-time 5 -o /dev/null -w %{http_code} http://127.0.0.1/`. Với nginx, đạt khi curl thoát mã 52 (máy chủ đóng kết nối không trả gì); với Apache, đạt khi mã HTTP là 403. Không dùng `-f` vì 403 sẽ thành mã thoát 22, lẫn với lỗi khác. Đây là kiểm bằng con đường độc lập: file vhost có thể đúng cú pháp mà site hứng request lạ vẫn là site khác.

`checks.PortLocalOnly(3306, ...)` đọc `ss -tlnH`, tách địa chỉ bằng `net/netip`, và lỗi về phía an toàn: địa chỉ `*` không đọc được thì coi là mở ra ngoài; không ai nghe 3306 cũng là `Fail`, vì "không mở ra ngoài" chỉ đúng do MariaDB đang chết.

### 3.15 `internal/store`: SQLite

`store.Open` (`internal/store/store.go`) tạo thư mục quyền 0700 và file DB quyền 0600 trước khi SQLite mở nó, rồi mở với các pragma:

```go
	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("mở DB %s: %w", path, err)
	}
	// ponytail: một kết nối là đủ cho tải của một panel và loại hẳn lỗi khoá
	// giữa các kết nối. Nâng lên khi đo thấy đọc bị chặn.
	db.SetMaxOpenConns(1)

	if err := migrate(ctx, db, migrationFS); err != nil {
```

`migrate` đọc các file `migrations/NNN_ten.sql` (nhúng bằng `//go:embed`), so với `PRAGMA user_version`, áp những bản chưa áp trong một transaction. Bản 1 (`internal/store/migrations/001_init.sql`) tạo bốn bảng: `settings`, `jobs`, `job_events`, `managed_files`.

`*store.Store` thoả cả `core.Settings` (`GetSetting`, `SetSetting`) lẫn `engine.Journal` (`CreateJob`, `AddEvent`, `FinishJob`, `RecordFile`, `ForgetFile`). Engine không import store; core nối hai bên bằng cách đặt `Journal: st`.

### 3.16 Kết thúc

Quay lại `Install.Execute`: job thành công thì ghi `stack.php_version` bằng giá trị bước dò đã ghi vào `*in.php`. CLI gọi `printReport` in "Job ...: thành công" và phần "Kiểm chứng". Job không thành công thì lệnh trả `&exitCode{1}` để `run` thoát mã 1 mà không in thêm dòng lỗi (báo cáo đã in rồi). `defer closeStore(st)` đóng DB; `defer unlock()` trong engine đã nhả khoá từ trước.

## 4. Kiến thức nền

Mỗi khái niệm: nó là gì, chỗ trong mã dùng nó, và một lệnh để tự nhìn thấy trên máy ảo. Các lệnh gõ trên Mac qua `scripts/vm.sh`; chưa ghi output thì nghĩa là mình chưa chạy lệnh đó, bạn tự chạy.

### apt và dpkg

dpkg là tầng dưới: cài một file `.deb` và giữ cơ sở dữ liệu "gói nào đã cài". apt là tầng trên: biết kho gói ở đâu, tự kéo các gói phụ thuộc, rồi gọi dpkg. `apt-get update` chỉ tải lại danh sách gói của kho, không cài gì.

Trong mã: `AptUpdate`, `AptInstall`, `missingPackages` trong `internal/steps/apt.go`. tpan hỏi dpkg chứ không hỏi apt, vì dpkg trả lời chính xác "đã cài xong" bằng trạng thái `install ok installed`.

Tự xem: `scripts/vm.sh exec dpkg-query --show '--showformat=${Package}\t${Status}\n' nginx`. Output thật (mục 2.5) có dòng `nginx	install ok installed`.

### systemd

systemd khởi động và trông các dịch vụ. Hai câu hỏi khác nhau: `systemctl is-active` (đang chạy không) và `systemctl is-enabled` (có tự bật khi máy khởi động không). Câu trả lời "không" là mã thoát khác 0 (is-active trả 3 khi dừng), không phải lỗi.

Trong mã: `steps.Service` dùng `systemctl enable --now <unit>`; `checks.Service` coi "chạy mà không bật lúc khởi động" là `Warn`, vì khởi động lại máy là web sập.

Tự xem: `scripts/vm.sh exec systemctl is-enabled nginx` (output thật: `enabled`), và `scripts/vm.sh exec systemctl status php8.3-fpm`.

### Nginx `default_server` và mã 444

Nginx chọn server theo Host của request; không khớp `server_name` nào thì dùng server có `default_server` trên cổng đó. Không khai báo thì nginx lấy server đầu tiên, và người gõ IP sẽ thấy một site thật. 444 không phải mã HTTP: nó bảo nginx đóng kết nối ngay, không gửi gì, nên bot quét IP không biết máy chạy gì.

Trong mã: `internal/templates/files/nginx-default.conf.tmpl` (`listen 80 default_server;`, `listen [::]:80 default_server;`, `return 444;`). `checks.HTTPDefault` chờ curl thoát mã 52.

Tự xem: `scripts/vm.sh exec curl -sS -m 5 -o /dev/null -w '%{http_code}\n' http://127.0.0.1/` ra `curl: (52) Empty reply from server` và `000` (mục 2.4).

### Apache: vhost đầu tiên là site mặc định

Apache không có `default_server`. Với một cổng, VirtualHost được nạp đầu tiên là site mặc định. Debian và Ubuntu nạp `sites-enabled/*` theo thứ tự tên file. Nếu đặt tên `tpan-default.conf`, site `tpan-abc.com.conf` của M2 sẽ xếp trước (`a` đứng trước `d`) và hứng mọi request lạ. Vì vậy file tên `000-tpan-default.conf`: số 0 đứng trước mọi chữ cái. Đây là ngoại lệ duy nhất của luật tiền tố `tpan-`.

Trong mã: biến `avail` ở nhánh Apache của `installPlan` (`internal/core/stack.go`); chú thích đầu `internal/templates/files/apache-default.conf.tmpl`.

Tự xem: `scripts/vm.sh root apachectl -S`. Output thật (mục 2.7): `*:80 tpan-default.invalid (/etc/apache2/sites-enabled/000-tpan-default.conf:7)`.

### MPM event và vì sao cấm mod_php

MPM là cách Apache chia việc cho tiến trình. `prefork`: mỗi kết nối chiếm một tiến trình, tốn RAM. `event`: vài tiến trình nhiều luồng phục vụ nhiều kết nối. mod_php chạy PHP ngay trong tiến trình Apache, không an toàn với nhiều luồng, nên buộc Apache về prefork; PHP lại chạy dưới user `www-data` chung cho mọi site, phá cách cô lập từng site của M2. TPAN chạy PHP bằng PHP-FPM và cho Apache chuyển request sang qua `proxy_fcgi`.

Bẫy cụ thể: gói `php8.3` của Ubuntu 24.04 ghi `libapache2-mod-php8.3` là lựa chọn đầu trong Depends, nên `apt-get install php8.3` trên máy chưa có php-fpm sẽ cài mod_php. `AptInstall.validate` từ chối các tên này.

Trong mã: `modPHP` trong `internal/steps/apt.go`; `ApacheModules{Disable: []string{"mpm_prefork"}}` trong `installPlan`.

Tự xem: `scripts/vm.sh root apachectl -M` (output thật ở lần 5 có `mpm_event_module (shared)` và không có `php`); `scripts/vm.sh exec apt-cache depends php8.3` để thấy dòng Depends (chưa chạy).

### PHP-FPM

PHP-FPM là một dịch vụ riêng giữ một nhóm (pool) tiến trình PHP, nhận request từ web server qua giao thức FastCGI, thường qua Unix socket. Tên dịch vụ, thư mục config và socket đều chứa số phiên bản (`php8.3-fpm`, `/etc/php/8.3/fpm/pool.d`), nên tpan phải dò bản đang cài. Ở M2 mỗi site có pool riêng dưới user riêng.

Trong mã: `steps.DetectPHP` và `detectPHP` trong `internal/steps/php.go` (đọc `/etc/php/*/fpm`, chọn bản cao nhất, so theo số: 8.10 lớn hơn 8.9); `checks.PHPFPMConfig` trong `internal/checks/config.go`.

Tự xem: `scripts/vm.sh exec ls /etc/php/` và `scripts/vm.sh exec ls -l /run/php/` (chưa chạy).

### MariaDB: `unix_socket` và `bind-address`

`unix_socket` là cách đăng nhập: MariaDB hỏi hệ điều hành "ai đang nối vào qua socket này", và user Linux `root` thì được vào user DB `root`. Không có mật khẩu nào để đoán hay để lộ. Từ 10.4, thông tin đăng nhập nằm trong cột JSON `Priv` của bảng `mysql.global_priv`. `bind-address = 127.0.0.1` cho MariaDB chỉ nghe trong máy, cổng 3306 không mở ra mạng.

Trong mã: `mariadbStateSQL` trong `internal/steps/mariadb.go`; file `99-tpan.cnf` trong `installPlan` (tên `99-` để được đọc sau `50-server.cnf` của Ubuntu và thắng nếu khác nhau).

Tự xem: `scripts/vm.sh root mariadb -e 'SHOW GRANTS FOR root@localhost'` (output thật có `IDENTIFIED VIA mysql_native_password USING 'invalid' OR unix_socket`). Thử đăng nhập bằng user thường: `scripts/vm.sh exec mariadb -u root` sẽ bị từ chối (chưa chạy).

### logrotate

logrotate chạy mỗi ngày, đổi tên file log cũ (`access.log` thành `access.log.1`), nén bản cũ hơn, xoá bản quá hạn, rồi chạy `postrotate` để web server mở file log mới. Không có nó, log ăn hết đĩa. `logrotate -d` chỉ đọc config và in sẽ làm gì, không xoay thật, nên dùng làm lệnh kiểm cú pháp.

Trong mã: `internal/templates/files/logrotate-sites.tmpl`; bước `File` cho `/etc/logrotate.d/tpan-sites` có `Test: logrotate -d ...`. Log PHP dùng `copytruncate` vì PHP-FPM không được báo mở lại file.

Tự xem: `scripts/vm.sh root logrotate -d /etc/logrotate.d/tpan-sites` (output thật trong PHẦN 7 của M1.log có dòng `not running postrotate script, since no logs were rotated`, vì chưa có site nào nên chưa có log).

### flock

`flock` là khoá trên một file, do nhân Linux giữ. Hai tiến trình cùng xin khoá độc quyền trên một file thì một bên phải chờ. Tiến trình chết thì nhân tự nhả khoá. Cần nó vì CLI và `tpan serve` là hai tiến trình khác nhau, mutex trong Go không chặn được nhau. File khoá ở `/run/tpan/`, mà `/run` là tmpfs (nằm trong RAM), nên khởi động lại máy là sạch.

Trong mã: `LocalSystem.Lock` trong `internal/system/local.go`, dùng `LOCK_EX|LOCK_NB` lặp lại mỗi 100 ms để còn huỷ được bằng ctx.

Tự xem: trong lúc một lần `tpan stack install` đang chạy, mở terminal khác và chạy `scripts/vm.sh root tpan stack install --web nginx --yes`; lần thứ hai phải chờ (chưa chạy). Xem ai đang giữ khoá: `scripts/vm.sh root cat /proc/locks` (chưa chạy).

### Ghi file nguyên tử bằng rename

`rename` trong cùng một phân vùng là nguyên tử: tên file trỏ sang nội dung mới trong một thao tác, không có lúc nào nửa cũ nửa mới. Ghi thẳng vào file đang có (mở, cắt về 0, ghi) thì có khoảnh khắc file rỗng hoặc dở; nginx reload đúng lúc đó sẽ đọc phải config hỏng. File tạm phải nằm cùng thư mục để chắc cùng phân vùng.

Trong mã: `LocalSystem.WriteFileAtomic`; `replaceSymlink` trong `internal/steps/common.go` làm điều tương tự cho symlink (tạo link tạm tên bắt đầu bằng dấu chấm rồi rename đè).

Tự xem: `scripts/vm.sh root ls -la /etc/nginx/sites-available/` sau khi cài, không còn file `.tpan-*` nào sót lại (chưa chạy).

### SQLite

SQLite là cơ sở dữ liệu nằm trong một file, không cần dịch vụ chạy nền. Panel không phụ thuộc MariaDB của các site: MariaDB chết thì panel vẫn chạy để báo lỗi. WAL là chế độ ghi nhật ký cho phép đọc trong lúc đang ghi. `PRAGMA user_version` là một số nguyên SQLite để dành cho ứng dụng; tpan dùng nó làm số bản migration.

Trong mã: `store.Open` và `migrate` trong `internal/store/store.go`; thư viện `modernc.org/sqlite` viết thuần Go nên build chéo được với `CGO_ENABLED=0`.

Tự xem: đọc bằng tpan, `scripts/vm.sh root tpan stack status`. Đọc thẳng bằng `sqlite3`: chương trình này có thể chưa có trên máy ảo (chưa kiểm); nếu thiếu thì `scripts/vm.sh root apt-get install -y sqlite3`, rồi `scripts/vm.sh root sqlite3 /var/lib/tpan/tpan.db 'SELECT key, value FROM settings'` (chưa chạy). Cài thêm gói là máy không còn sạch, nên xem xong thì `make vm-reset`.

## 5. Cách khác đã cân nhắc, và chỗ đã làm sai

### Chạy lệnh bằng `sh -c "..."`

Cách nhanh nhất để chạy `apt-get install -y nginx` là ghép thành một chuỗi rồi đưa cho shell. Không chọn vì shell hiểu `;`, `|`, `&&`, `$(...)`: một giá trị lạ lọt vào chuỗi (tên miền như `a.com; rm -rf /` ở M2) là thành lệnh thứ hai chạy bằng root. Đó là gốc của phần lớn lỗ hổng chèn lệnh ở các panel năm 2026 (mục 11 của KE-HOACH.md). Với argv, tham số chỉ là dữ liệu.

Luật này được giữ bằng test, không chỉ bằng lời hứa: `internal/system/noshell_test.go` đọc mọi file `.go` của repo, báo lỗi nếu thấy tên shell đi kèm `-c`, hoặc import `os/exec` ngoài `internal/system`.

Hai chỗ có shell mà người review có thể chỉ ra, và vì sao không trái luật:

- `postrotate` trong `logrotate-sites.tmpl` là đoạn shell. Nó do logrotate chạy, nội dung cố định trong template, không có giá trị nào từ người dùng.
- `scripts/e2e-m1.sh` và các lệnh kiểm trong log dùng `bash -c`. Đó là kịch bản test chạy từ Mac, không phải mã của tpan.

### Bật `ssl` của Apache ngay ở M1

Bản đầu làm đúng như KE-HOACH.md lúc đó: bật `mpm_event proxy_fcgi setenvif rewrite ssl headers http2`. Lần 4 của e2e (PHẦN 6 của M1.log) đạt trọn 99 khẳng định, nhưng phần ghi lại cổng 443 cho thấy:

```
LISTEN 0      511                *:443             *:*    users:(("apache2",pid=14748,fd=6),("apache2",pid=14747,fd=6),("apache2",pid=14342,fd=6))
```

```
<title>Apache2 Ubuntu Default Page: It works</title>
```

Bật `ssl` là `ports.conf` của Ubuntu cho Apache nghe 443 (tự xem: `scripts/vm.sh exec cat /etc/apache2/ports.conf`, chưa chạy). Chưa có vhost 443 nào của tpan, nên request vào 443 rơi về cấu hình chung, `DocumentRoot "/var/www/html"`, tức trang chào của Ubuntu, phục vụ bằng HTTP thường. Đúng thứ site mặc định của tpan sinh ra để chặn.

Phát hiện được vì kịch bản có ghi lại cổng 443 dù chưa khẳng định gì về nó. Sửa ở commit `7e04926`: bỏ `ssl` khỏi `apacheModules` trong `internal/core/stack.go`, sửa mục 6.1 của KE-HOACH.md. M5 sẽ bật `ssl` cùng một vhost 443 mặc định. Lần 5 thêm ba khẳng định: `apachectl -M` không có `ssl_module`, `ss -tlnH` không có 443, `curl http://127.0.0.1:443/` thoát mã 7.

Còn sót: chú thích đầu `ApacheModules` và câu "Vì sao" của nó trong `internal/steps/apache.go` vẫn nhắc ssl ("thiếu ssl thì không có HTTPS"); câu này hiện ra trong bản xem trước của lần 5.

### Chỉ thị `su root root` trong logrotate

Theo tài liệu của logrotate, nó từ chối xoay log trong một thư mục mà user khác root ghi được, và gợi ý thêm chỉ thị `su <user> <nhóm>` (chưa tái hiện trên máy ảo, vì M1 chưa có thư mục log của site). Cách vá nhanh là thêm `su root root`. Không chọn: lời từ chối đó có lý do. Nếu thư mục log thuộc user của site, user đó (tức PHP của site, nếu site bị chiếm) có thể đặt sẵn một symlink đúng tên file mà logrotate sắp tạo hay đổi tên, trỏ ra một file hệ thống; logrotate chạy bằng root sẽ ghi vào file đó. `su root root` chỉ làm câu cảnh báo im đi, logrotate vẫn chạy bằng root trong thư mục người khác kiểm soát.

Cách tpan chọn: thư mục `/var/log/tpan/sites/<domain>/` thuộc `root:root`, quyền 0755; riêng file `php-error.log` thuộc user của site để PHP ghi được. Khi đó logrotate không cần `su`, và user của site không tạo được file nào trong thư mục. Quyết định này có từ trước commit template đầu tiên (`fd9d963`), ghi ở chú thích đầu `internal/templates/files/logrotate-sites.tmpl` và mục 5 của KE-HOACH.md; bản nháp có `su root root` không vào git nên không có diff để xem. Thư mục log của site do M2 tạo, nên phần này chưa chạy trên máy ảo; M2 phải kiểm bằng `logrotate -d` khi đã có site.

### Ghi `stack.web` trước khi chạy, `stack.php_version` sau khi xong

Cân nhắc hai cách đơn giản hơn:

- Ghi hết settings sau khi job thành công. Hỏng giữa chừng thì máy đã có gói do chính tpan cài dở (ví dụ có `mariadb-server`, cổng 80 bận vì nginx), mà settings trống, nên lần chạy lại bị điều kiện đầu "máy phải sạch" từ chối. Người dùng kẹt: không chạy lại được, phải tự gỡ tay.
- Ghi hết settings trước khi chạy. Hỏng giữa chừng thì `stack.php_version` đã có, `tpan health` và `stack status` báo "đã cài" cho một stack dở.

Nên chia đôi. `stack.web` ghi trước, như lời "máy này thuộc tpan với web server X": lần sau cùng web server thì bỏ kiểm cổng và gói, khác web server thì từ chối và chỉ sang `tpan stack switch`. `stack.php_version` ghi sau, và "đã cài" nghĩa là khoá này có giá trị. Mã ở `Install.Execute` (3.5), test ở `internal/core/stack_test.go` (trường hợp "đã cài cùng web thì bỏ kiểm cổng và gói").

### Gỡ gói khi hoàn tác

Cân nhắc cho `AptInstall.Undo` chạy `apt-get remove`. Không chọn: gỡ một gói có thể kéo theo gỡ gói khác đang dùng chung thư viện, và máy không sạch từ trước thì có thể gỡ mất thứ người dùng đang dùng. Gói thừa không làm hỏng gì. Bản xem trước ghi rõ điều này ở dòng "Ghi chú hoàn tác".

## 6. Tự kiểm

1. Đổi tên file site mặc định của Apache thành `tpan-default.conf` thì hỏng gì, và hỏng từ mốc nào?
2. Vì sao `checks.HTTPDefault` với nginx coi curl thoát mã 52 là đạt? Bỏ dòng `listen [::]:80 default_server;` trong template thì sao?
3. Một lần cài hỏng ở bước ghi `tpan-default.conf` vì `nginx -t` lỗi. Chạy lại lệnh cài thì điều kiện đầu có từ chối vì cổng 80 bận không? Vì sao?
4. Ở mục 2.8, CLI in "hỏng, đã hoàn tác sạch". Vậy những gì đã thật sự được trả lại trên máy?
5. Vì sao bản xem trước nói bước "Gỡ /etc/nginx/sites-enabled/default" sẽ bỏ qua, mà lúc chạy thật nó lại chạy?
6. Vì sao câu SQL của `MariaDBSecure` đi qua `Cmd.Stdin` mà không qua `mariadb -e '...'`?
7. (Cần máy ảo) Sau khi cài nginx, sửa tay `/etc/nginx/sites-available/tpan-default.conf` (đổi `444` thành `200`), rồi chạy lại `tpan stack install --web nginx --yes`. tpan làm gì?
8. (Cần máy ảo) Chứng minh MariaDB không nhận kết nối từ ngoài máy ảo, bằng hai lệnh độc lập với tpan.
9. Hai lệnh `tpan stack install` chạy cùng lúc trên một máy thì sao?

## Đáp án

1. Apache coi vhost nạp đầu tiên (theo tên file trong `sites-enabled`) là site mặc định. Ở M1 chỉ có một vhost nên chưa thấy hỏng. Từ M2, file `tpan-abc.com.conf` xếp trước `tpan-default.conf`, nên site `abc.com` hứng mọi request lạ: ai gõ IP máy hay trỏ một domain lạ về máy đều thấy site đó. Cũng vì thế test `TestPlanOrder` trong `internal/core/stack_test.go` khoá cứng ID `file:/etc/apache2/sites-available/000-tpan-default.conf`.
2. `return 444` bảo nginx đóng kết nối không gửi gì, nên không có dòng trạng thái HTTP nào; curl báo `Empty reply from server`, mã thoát 52, và `-w %{http_code}` in `000`. Bỏ dòng `[::]:80 default_server` thì request tới địa chỉ IPv6 của máy không có server mặc định; từ M2, chúng rơi vào site đầu tiên nghe IPv6. `checks.HTTPDefault` chỉ gọi `127.0.0.1` nên không bắt được lỗi này; tự kiểm bằng `curl -g 'http://[::1]/'` trong máy ảo (chưa chạy).
3. Không. `stack.web = nginx` đã được ghi trước khi chạy (`Install.Execute`), nên lần chạy lại `firstInstall` là false và `PrepareInstall` bỏ `PortsFree` và `PackagesAbsent`, chỉ còn kiểm Ubuntu 24.04. Lần hỏng trước đã hoàn tác các bước có Undo (thư mục, gỡ site distro...), nên lần này chúng chạy lại; gói đã cài thì không được hoàn tác, nên `AptInstall` được bỏ qua. Còn `File` lỗi ở `nginx -t` thì chính `verifyOrRestore` đã trả file cũ về trước khi báo lỗi, nên nginx chưa bao giờ nạp config hỏng. Lỗi `nginx -t` vẫn còn đó thì lần chạy lại hỏng ở đúng bước ấy, cho tới khi sửa nguyên nhân.
4. Chỉ một bước được Apply: `apt-get update`, và Undo của nó không làm gì. `AptInstall` lỗi trong Apply nên engine không gọi Undo cho nó (chỉ bước Apply thành công mới được hoàn tác, xem `runStep`). Máy sạch vì apt tải gói trước rồi mới bung, mà nó chết lúc tải (404). Kịch bản kiểm lại thấy `mariadb`, `nginx`, `php-fpm` đều `not-found`. Đoạn log chứng minh đường hoàn tác của engine chạy và job ở trạng thái `rolled_back`; việc trả lại file thật thì chỉ có ở unit test.
5. Xem trước gọi `Check` trên máy hiện tại. Lúc đó nginx chưa cài, file `sites-enabled/default` chưa có, nên `Absent.Check` trả "đã đúng". Lúc chạy thật, bước 2 cài nginx, gói tạo ra file đó, nên tới bước 9 `Check` trả "chưa đúng" và bước chạy. Xem trước không mô phỏng tác dụng của các bước phía trên.
6. Mọi user trên máy xem được dòng lệnh của mọi tiến trình bằng `ps` hoặc `/proc/<pid>/cmdline`. Ở M1 câu SQL không có bí mật, nhưng M2 sẽ đưa mật khẩu DB của site qua cùng đường này. Thêm nữa, `recSys.Run` ghi `Cmd.String()` vào log job, và `Cmd.String()` không in `Stdin`; nên những gì đi qua stdin không vào bảng `job_events`.
7. Theo mã: `File.Check` so nội dung, khác nên chưa đạt; `Apply` ghi lại nội dung của template, chạy `nginx -t`, rồi `systemctl reload nginx`. Job thành công, file về đúng bản tpan, và bước kiểm chứng `HTTPDefault` lại đạt. Đó là ý "sửa chỗ lệch chính là chạy lại Plan" ở mục 4.2 của KE-HOACH.md. Chưa chạy trên máy ảo. Tự kiểm: `scripts/vm.sh root sed -i 's/return 444/return 200/' /etc/nginx/sites-available/tpan-default.conf`, `scripts/vm.sh root systemctl reload nginx`, `scripts/vm.sh exec curl -sS -o /dev/null -w '%{http_code}\n' http://127.0.0.1/` (phải ra 200), rồi `scripts/vm.sh root tpan stack install --web nginx --yes` và curl lại (phải ra `curl: (52)`).
8. Lệnh thứ nhất, trong máy ảo: `scripts/vm.sh root ss -tlnp`. Output thật (mục 2.4) chỉ có `127.0.0.1:3306`, không có `0.0.0.0:3306` hay `[::]:3306`. Lệnh thứ hai, từ Mac: `nc -vz <ip-may-ao> 3306` phải báo không kết nối được (chưa chạy). Nguồn của điều này là `bind-address = 127.0.0.1` trong `/etc/mysql/mariadb.conf.d/99-tpan.cnf`: `scripts/vm.sh root cat /etc/mysql/mariadb.conf.d/99-tpan.cnf`.
9. `engine.Execute` gọi `sys.Lock` trước mọi việc. `LocalSystem.Lock` lấy `flock` trên `/run/tpan/tpan.lock`; lệnh thứ hai thử lại mỗi 100 ms cho tới khi lệnh thứ nhất xong và nhả khoá, rồi mới tạo job. Khi tới lượt, mọi bước của nó được bỏ qua vì máy đã đúng. Bấm Ctrl+C lúc đang chờ thì nó thoát với lỗi "chờ khoá tpan". Unit test: `TestLock` trong `internal/engine/engine_test.go`. Chưa chạy hai lệnh song song trên máy ảo.
