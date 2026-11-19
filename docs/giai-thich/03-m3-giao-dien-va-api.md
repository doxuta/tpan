# M3: panel web, API và đăng nhập

Đọc kèm: `01-m1-cai-stack.md` (engine, Step, Plan, Journal), `02-m2-site-wordpress.md` (core tạo site, phép kiểm của site), `00-go-can-ban.md` (mục 7 context, mục 8 goroutine và mutex, mục 10 embed), `docs/KE-HOACH.md` mục 3 (bảng công nghệ), 4.4 (bảng `sessions`, `audit`), 5 (bố trí trên máy chủ), 9 (CLI và API), 10 (giao diện), 11 (bảo mật của chính TPAN), `docs/GIAO-DIEN.md` mục 1 tới 7, và `docs/bang-chung/M3.log`.

## 1. Mốc này làm được gì

- `sudo tpan setup` biến một máy đã cài tpan thành panel: sinh chứng chỉ tự ký, mở DB, sinh mật khẩu quản trị (in một lần), ghi file dịch vụ systemd. Từ đó panel tự chạy ở `https://<IP>:8443` sau mỗi lần khởi động máy.
- Người quản trị đăng nhập bằng trình duyệt, xem Bảng điều khiển (bản đồ tuyến của từng site, số liệu máy, lịch sử job), tạo site có xem trước, xoá site có xem trước và hỏi lại, xem log của job chạy trực tiếp (API đẩy log qua SSE; đã thấy log hiện dần trên trình duyệt với panel thật của máy ảo, xem 2.13), nhận mật khẩu quản trị WordPress đúng một lần. Giao diện có tiếng Việt và tiếng Anh, sáng và tối.
- Sai mật khẩu 5 lần thì IP bị khoá 15 phút. Quên mật khẩu thì `sudo tpan admin passwd` trên máy chủ.
- Mọi thứ trình duyệt làm đều là một request tới API `/api/v1/...`, nên script cũng làm được y như vậy bằng `curl`.
- `tpan serve --demo` chạy toàn bộ panel trên một máy giả, để làm và xem giao diện trên Mac mà không đụng máy thật.

## 2. Chạy thử

Output dưới đây lấy nguyên văn từ `docs/bang-chung/M3.log`: lần chạy `scripts/e2e-m3.sh` ngày 19/11/2026, 16:50 tới 16:51, dòng tổng kết là `TỔNG KẾT: 0 khẳng định không đạt`. Bản tpan là `7de6977-dirty` (thấy ở trường `version` của `/api/v1/session`), tức build từ commit `7de6977` cộng phần sửa chưa commit lúc đó. Phần sửa đó chính là commit `06b4170` (đo CPU, mục 5): `git diff --stat 7de6977 0f4129e -- internal cmd web` chỉ ra đúng hai file `internal/server/api.go` và `internal/server/api_test.go`, đúng như commit đó. Commit `a9bfc79` (CLI cũng ghi audit) vào sau lần chạy này, nên chưa có trong log.

Ba điều cần biết khi đọc:

- Kịch bản in dòng lệnh bằng `$*`, nên dấu nháy bị mất (giống M2). Chỗ nào mình viết lại lệnh để bạn gõ được thì có nói; output giữ nguyên.
- Kịch bản tự che mật khẩu, cookie, IP: `<mật khẩu, đã che>`, `<đã che>`, `<vm>` (IP máy ảo), `<mac>` (IP của Mac), `<hostname>`, `<tmp>`. Đó là chữ của script.
- Kịch bản chạy trên Mac. Lệnh trong máy ảo đi qua `scripts/vm.sh root ...`; lệnh `curl` chạy trên Mac, gọi vào máy ảo qua mạng.

### 2.1 `tpan setup`

Máy ảo sạch, đã `tpan stack install --web nginx` (output giống M1). Chạy trên Mac:

```bash
scripts/vm.sh root tpan setup --yes
```

```
Bắt đầu job 71eb58fb3ed9dc32
- Tạo thư mục /etc/tpan ...
  xong
- Tạo thư mục /etc/tpan/tls ...
  xong
- Ghi chứng chỉ tự ký của panel ...
  xong
- Ghi khoá riêng của chứng chỉ (chỉ root đọc) ...
  xong
- Tạo thư mục /var/lib/tpan ...
  xong
- Ghi file dịch vụ systemd của tpan ...
  xong
- Chạy và bật tpan.service khi khởi động ...
  xong

Job 71eb58fb3ed9dc32: thành công

Mật khẩu quản trị (chỉ hiện MỘT lần, hãy lưu lại ngay):
    <mật khẩu, đã che>
Dấu vân tay SHA-256 của chứng chỉ (đối chiếu với trình duyệt khi nó cảnh báo chứng chỉ tự ký):
    A5:DC:89:BC:A8:5E:D8:B3:14:38:2E:C9:19:7C:78:11:6D:50:D4:93:4E:47:DA:3B:3E:63:CA:F1:C7:E8:83:A7
Mở: https://<IP của máy>:8443
```

Kịch bản đếm: `Mật khẩu quản trị panel trong output: 1 lần (26 ký tự base32)`. Setup là một job như `stack install`: có id, có từng bước, nằm trong Lịch sử.

Kết quả trên máy (`scripts/vm.sh root stat -c '%a %U:%G %n' ...`, mình thêm lại dấu nháy):

```
700 root:root /etc/tpan
700 root:root /etc/tpan/tls
600 root:root /etc/tpan/tls/cert.pem
600 root:root /etc/tpan/tls/key.pem
700 root:root /var/lib/tpan
600 root:root /var/lib/tpan/tpan.db
644 root:root /etc/systemd/system/tpan.service
```

File dịch vụ mà setup ghi (`scripts/vm.sh root cat /etc/systemd/system/tpan.service`):

```
# Do tpan quản lý. Sửa tay sẽ bị tpan health báo lệch.
[Unit]
Description=tpan panel
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/tpan serve --listen 0.0.0.0:8443 --tls-cert /etc/tpan/tls/cert.pem --tls-key /etc/tpan/tls/key.pem
Restart=on-failure
RestartSec=2s

[Install]
WantedBy=multi-user.target
```

`systemctl is-active tpan` in `active`, `is-enabled` in `enabled`, và `ss -tlnp` có dòng:

```
LISTEN 0      4096               *:8443            *:*    users:(("tpan",pid=13727,fd=8))
```

Đối chiếu fingerprint ba cách, cả ba ra cùng một chuỗi: setup in ra; `openssl x509 -fingerprint -sha256` đọc file trong máy ảo; `openssl s_client` từ Mac đọc chứng chỉ panel đang phục vụ:

```
sha256 Fingerprint=A5:DC:89:BC:A8:5E:D8:B3:14:38:2E:C9:19:7C:78:11:6D:50:D4:93:4E:47:DA:3B:3E:63:CA:F1:C7:E8:83:A7
```

Nội dung chứng chỉ (`openssl x509 -noout -subject -dates -ext subjectAltName,keyUsage,extendedKeyUsage`). Dòng SAN có thêm một địa chỉ IPv6 thật của máy ảo mà script không che; mình thay nó bằng `<IPv6 máy ảo>`:

```
subject=CN = tpan-test
notBefore=Nov 19 08:51:07 2026 GMT
notAfter=Nov 19 09:51:07 2036 GMT
X509v3 Key Usage: critical
    Digital Signature
X509v3 Extended Key Usage: 
    TLS Web Server Authentication
X509v3 Subject Alternative Name: 
    DNS:tpan-test, DNS:localhost, IP Address:127.0.0.1, IP Address:<vm>, IP Address:0:0:0:0:0:0:0:1, IP Address:<IPv6 máy ảo>
```

Setup chạy lúc 16:51:07 giờ Việt Nam, tức 09:51:07 GMT. `notBefore` lùi một giờ, `notAfter` là 10 năm sau: đúng như `newSelfSignedCert` (mục 3.1).

Không có chứng chỉ của panel thì `curl` từ chối, đúng như mong đợi:

```
curl: (60) SSL certificate problem: unable to get local issuer certificate
```

### 2.2 Chạy lại `tpan setup`

Cùng lệnh. Bớt dòng:

```
Bắt đầu job b82dc2e815e94517
- Tạo thư mục /etc/tpan ...
  đã đúng, bỏ qua
...
- Ghi chứng chỉ tự ký của panel ...
  đã đúng, bỏ qua
- Ghi khoá riêng của chứng chỉ (chỉ root đọc) ...
  đã đúng, bỏ qua
...
Job b82dc2e815e94517: thành công

Mật khẩu quản trị đã có từ trước, giữ nguyên. Quên thì chạy: sudo tpan admin passwd
Dấu vân tay SHA-256 của chứng chỉ (đối chiếu với trình duyệt khi nó cảnh báo chứng chỉ tự ký):
    A5:DC:89:BC:A8:5E:D8:B3:14:38:2E:C9:19:7C:78:11:6D:50:D4:93:4E:47:DA:3B:3E:63:CA:F1:C7:E8:83:A7
```

Bảy bước đều "đã đúng", không mật khẩu mới, cùng fingerprint. `sha256sum` của `cert.pem`, `key.pem`, `tpan.service` và PID của dịch vụ (`MainPID=13727`) giống hệt trước khi chạy lại.

Một chỗ phải nói thật: kịch bản còn định so hash mật khẩu quản trị trước và sau, nhưng câu Python của nó đọc khoá `admin_password_hash`, còn mã lưu ở khoá `admin.password_hash` (`store.AdminPasswordKey`). Cả hai lần đều ra `TypeError: 'NoneType' object is not subscriptable`, và phép so "giống nhau" chỉ so hai thông báo lỗi giống nhau. Nên log này **không** chứng minh hash không đổi. Điều đó chỉ có ở unit test (`TestSetupLanDauVaChayLai` trong `cmd/tpan/setup_test.go`) và ở dòng "Mật khẩu quản trị đã có từ trước" ở trên.

### 2.3 Trang đầu, header bảo mật, API khi chưa đăng nhập

Từ Mac, `curl --cacert cert.pem` (chứng chỉ chép từ máy ảo) tới `https://<vm>:8443/`:

```
HTTP/2 200 
cache-control: no-cache
content-security-policy: default-src 'self'; frame-ancestors 'none'
content-type: text/html; charset=utf-8
referrer-policy: no-referrer
x-content-type-options: nosniff
content-length: 799
```

Thân trang là `index.html` đã build, nằm sẵn trong file chạy:

```
    <script src="/theme.js"></script>
    <script type="module" crossorigin src="/assets/index-m5MYbe2-.js"></script>
```

`GET /sites/new` cũng trả đúng trang đó (`content-length: 799`): đây là đường dẫn của giao diện, không phải file thật, nên server trả `index.html` để react-router lo (hàm `spa`, mục 3.4).

Khi chưa đăng nhập:

```
{
  "logged_in": false,
  "demo": false
}
```

```
HTTP/2 401 
...
{
  "error": {
    "code": "unauthorized",
    "message": {
      "vi": "Chưa đăng nhập hoặc phiên đã hết hạn.",
      "en": "Not logged in, or the session has expired."
    }
  }
}
```

### 2.4 Đăng nhập, sai 5 lần, khoá IP

Đăng nhập đúng (`POST /api/v1/login`, thân `{"password": "..."}`):

```
HTTP/2 204 
content-security-policy: default-src 'self'; frame-ancestors 'none'
referrer-policy: no-referrer
set-cookie: __Host-tpan=<đã che>; Path=/; HttpOnly; Secure; SameSite=Strict
x-content-type-options: nosniff
```

Rồi gõ sai liên tiếp. Lần 1 tới 4 là `401` với mã `wrong_password`, mỗi lần mất khoảng 0,07 giây (`-> HTTP 401, 0.071720s`). Lần 5:

```
HTTP/2 423 
...
retry-after: 900
...
-> HTTP 423, 0.067074s
```

Gõ đúng mật khẩu trong lúc bị khoá vẫn bị từ chối, và trả lời nhanh hơn hẳn:

```
-> HTTP 423, 0.003041s
...
{
  "error": {
    "code": "login_locked",
    "message": {
      "vi": "Sai mật khẩu quá nhiều lần; địa chỉ IP này bị khoá thêm 15 phút. Quên mật khẩu thì chạy trên máy chủ: sudo tpan admin passwd",
      "en": "Too many wrong passwords; this IP address is locked for 15 more minutes. If you forgot the password, run on the server: sudo tpan admin passwd"
    }
  }
}
```

0,07 giây là thời gian argon2id băm thử mật khẩu; 0,003 giây là khi server từ chối trước cả khi băm (mục 3.6). `retry-after: 900` là 15 phút tính bằng giây.

### 2.5 `tpan admin passwd`

Chạy trên Mac:

```bash
scripts/vm.sh root tpan admin passwd --yes
```

```
Mật khẩu quản trị mới (chỉ hiện MỘT lần, hãy lưu lại ngay):
    <mật khẩu, đã che>
Mọi phiên đăng nhập đã bị huỷ và mọi IP bị khoá đã được gỡ.
```

Kịch bản kiểm ngay sau đó: phiên đăng nhập từ trước giờ trả `"logged_in": false`; mật khẩu setup cũ giờ là `401` (không còn `423`, vì khoá đã gỡ); mật khẩu mới cho `204` và cookie mới; `GET /api/v1/account/sessions` chỉ còn đúng một phiên.

`tpan admin passwd --dry-run`: chưa chạy trên máy ảo. Tự thử: `scripts/vm.sh root tpan admin passwd --dry-run`; theo mã (`adminPasswdCmd`), nó chỉ in câu "Sẽ: sinh mật khẩu quản trị mới ..." và không mở DB.

### 2.6 CSRF

Đã đăng nhập, gửi request có thay đổi nhưng giả như đến từ trang khác:

```
HTTP/2 403 
...
{
  "error": {
    "code": "csrf",
    "message": {
      "vi": "Yêu cầu đến từ một trang khác nên bị chặn (chống CSRF). Hãy thao tác từ chính giao diện tpan.",
      "en": "The request came from another site and was blocked (CSRF protection). Use the tpan interface itself."
    }
  }
}
```

Kịch bản thử bốn kiểu, đều `403 csrf`: `Sec-Fetch-Site: cross-site`; `Origin: https://evil.example` (không có `Sec-Fetch-Site`); `DELETE` cross-site; `POST /api/v1/logout` cross-site (trang khác không đá được bạn ra). Đối chứng: `Sec-Fetch-Site: same-origin` kèm `Origin` của chính panel thì `200`.

### 2.7 Tạo site qua API, như trình duyệt làm

Đây là đúng ba request mà màn Tạo site gửi. Xem trước (`POST /api/v1/sites?dry_run=1`, thân `{"domain":"a.example.com","title":"Site A","email":"admin@example.com"}`), kịch bản rút gọn JSON thành danh sách bước:

```
{
  "kind": "site.add",
  "target": "a.example.com",
  "title": "Tạo site WordPress a.example.com",
  "steps": [
    "site.user:tp_a_example_com_7653e4: Tạo user Linux tp_a_example_com_7653e4 cho site",
    "dir:/var/www/a.example.com: Tạo thư mục /var/www/a.example.com",
...
    "wp.install:a.example.com: Cài WordPress cho a.example.com"
  ]
}
```

Ngay sau đó, trên máy ảo chưa có gì:

```
html
id: ‘tp_a_example_com_7653e4’: no such user
```

Chạy thật (`POST /api/v1/sites`, cùng thân):

```
HTTP/2 202 
...
-> HTTP 202, 0.009035s
...
{
  "job_id": "deac0a92afe8c2fb"
}
```

Server trả lời sau 9 mili giây, khi job mới bắt đầu. Job chạy ở nền. Kịch bản mở luồng SSE (`GET /api/v1/jobs/deac0a92afe8c2fb/events`) bằng `curl -sN` và ghi giờ nhận từng dòng. Luồng thô trông như sau:

```
1795081870.988 id: 1
1795081870.988 event: job.start
1795081870.988 data: {"job_id":"deac0a92afe8c2fb","seq":1,"time":"2026-11-19T16:51:11.012005504+07:00","kind":"job.start"}
1795081870.988 
1795081870.988 id: 2
1795081870.988 event: step.start
1795081870.988 data: {"job_id":"deac0a92afe8c2fb","seq":2,"time":"2026-11-19T16:51:11.012910674+07:00","kind":"step.start","step_id":"site.user:tp_a_example_com_7653e4"}
```

Hai giây sau, `GET /api/v1/jobs/deac0a92afe8c2fb` cho thấy job vẫn chạy và chưa có mật khẩu:

```
    "status": "running",
    "started": "2026-11-19T09:51:11.010895671Z"
  },
  "events": 36,
  "has_secret": false
```

Đoạn cuối của luồng, theo thời điểm nhận tính từ sự kiện đầu (kịch bản cắt ngắn mỗi dòng):

```
+11.23s  #60  step.start  2026-11-19T16:51:22.250395442+07:00  wp.install:a.example.com  
...
+12.838s  #77  step.done  2026-11-19T16:51:23.855598342+07:00  wp.install:a.example.com  
+12.839s  #78  check  2026-11-19T16:51:23.856561498+07:00    [pass] web.vhost:a.example.com
+12.84s  #79  check  2026-11-19T16:51:23.857922092+07:00    [pass] php.socket:a.example.com
+14.687s  #80  check  2026-11-19T16:51:25.701837896+07:00    [pass] wp.checksums:a.example.com
+14.688s  #81  check  2026-11-19T16:51:25.703839973+07:00    [pass] wp.config:a.example.com
+14.741s  #82  check  2026-11-19T16:51:25.755752466+07:00    [pass] db.access:a.example.com
+14.991s  #83  check  2026-11-19T16:51:26.006459866+07:00    [pass] http.site:a.example.com
+14.994s  #84  job.done  2026-11-19T16:51:26.00922053+07:00    succeeded
```

Tóm tắt của kịch bản: 84 sự kiện, `"seq_contiguous": true`, `"span_seconds": 14.99400019645691`, `"received_2s_before_done": 79`. Tức 79 sự kiện tới tay người nghe ít nhất 2 giây trước khi job xong: log chạy trực tiếp, không phải đổ ra một lúc ở cuối.

GET job lần đầu sau khi xong có tài khoản quản trị; lần hai thì không:

```
    "status": "succeeded",
...
  "events": 84,
  "secret": {
    "url": "http://a.example.com/wp-admin/",
    "user": "admin",
    "password": "<mật khẩu, đã che>"
  }
```

```
{
  "job": "succeeded",
  "events": 84,
  "has_secret": false
}
```

Kịch bản đăng nhập thật vào `wp-login.php` bằng mật khẩu vừa nhận: `HTTP/1.1 302 Found` kèm cookie `wordpress_logged_in_`; mật khẩu sai thì `HTTP/1.1 200 OK`, không có cookie đó.

### 2.8 Khi CLI đang chạy job mà API cũng muốn chạy

Kịch bản chạy `tpan site add b.example.com` ở dòng lệnh (giữ khoá `/run/tpan/tpan.lock`), rồi gọi `POST /api/v1/sites` cho `c.example.com`:

```
HTTP/2 409 
...
-> HTTP 409, 2.049279s
...
{
  "error": {
    "code": "job_running",
    "message": {
      "vi": "Đang có một thao tác khác thay đổi máy. Chờ nó xong rồi thử lại (xem ở Lịch sử).",
      "en": "Another operation is changing the server. Wait for it to finish, then try again (see History)."
    }
  }
}
```

API chờ khoá đúng khoảng 2 giây (`apiLockWait`) rồi trả 409. Trong lúc đó xem trước (`?dry_run=1`) vẫn `200` và `GET /api/v1/overview` vẫn `200`: chỉ chạy thật mới cần khoá. Job CLI vẫn xong (`CLI site add b.example.com thoát 0`), và `c.example.com` không được tạo.

### 2.9 Bảng điều khiển: số liệu máy và bộ nhớ đệm phép kiểm

`GET /api/v1/machine` so với `/proc` đọc tay trong máy ảo: `cpus = nproc`, `mem_total = MemTotal × 1024`, `disk.total` bằng `df` của `/var/www`. Lúc rỗi `"cpu_percent": 0`; lúc kịch bản chạy `yes` trên mọi CPU:

```
CPU% lúc rỗi: 0; lúc chạy yes trên mọi CPU: 100
```

Sau khi khởi động lại tpan (bộ nhớ đệm rỗng), hai site:

```
Thời gian GET /overview: lần đầu (bộ nhớ đệm rỗng, 2 site) 2.338574 s; lần hai (đệm còn hạn) 0.356104 s
```

```
CPU% trong 3 lần GET /overview?refresh=1 (chạy lại mọi phép kiểm): 1.9607843137254901 0 0; /machine riêng: 0
```

Dòng cuối là bằng chứng cho lỗi đã sửa ở commit `06b4170` (mục 5). Phiên đăng nhập vẫn còn sau `systemctl restart tpan`, vì nó nằm trong DB.

### 2.10 Xoá site qua API

`DELETE /api/v1/sites/b.example.com?dry_run=1` trả 7 bước, bước nào cũng có nhãn `[nguy hiểm]`; `DELETE` thật trả `202` với `job_id`, luồng SSE tới `job.done`, sau đó `GET /api/v1/sites/b.example.com` là `404 site_not_found` và `a.example.com` vẫn `200`.

### 2.11 Chế độ không TLS: cookie tên `tpan`

Để thử nhánh không TLS trên máy ảo, kịch bản dựng tạm một bản `tpan serve --listen 127.0.0.1:8080` (HTTP, chỉ loopback) và một `socat` chuyển cổng 8081 vào đó. Đây là đồ nghề test, không phải cách triển khai:

```
HTTP/1.1 204 No Content
Content-Security-Policy: default-src 'self'; frame-ancestors 'none'
Referrer-Policy: no-referrer
Set-Cookie: tpan=<đã che>; Path=/; HttpOnly; SameSite=Strict
```

Không có `Secure`, tên không có `__Host-`. CSRF qua đường này vẫn chặn (`403`). Xong thì tắt cả hai, `tpan.service` vẫn nghe 8443.

### 2.12 Nhật ký `audit`

```
2026-11-19T09:51:07.735548157Z cli setup 0.0.0.0:8443 ok 71eb58fb3ed9dc32
2026-11-19T09:51:09.346515851Z <mac> login  ok 
2026-11-19T09:51:09.491494267Z <mac> login.fail  wrong_password 
...
2026-11-19T09:51:09.941543388Z <mac> login.fail  locked 
2026-11-19T09:51:10.169383928Z cli admin.passwd  ok 
...
2026-11-19T09:51:26.012096134Z <mac> site.add a.example.com ok deac0a92afe8c2fb
2026-11-19T09:51:40.554563399Z <mac> site.add c.example.com job_running 
2026-11-19T09:51:52.899124109Z <mac> site.rm b.example.com ok 5b59366f61f59293
2026-11-19T09:51:54.493400758Z 127.0.0.1 login  ok 
```

Không có dòng `cli site.add b.example.com`: log chạy trước commit `a9bfc79`. Mã bây giờ sẽ ghi dòng đó; chưa chạy lại trên máy ảo.

Kịch bản cũng tìm 6 mật khẩu (2 của panel, 2 của WordPress, 2 của DB) trong `tpan.db` cùng file WAL, log của `tpan.service` và luồng SSE: 0 dòng ở cả ba chỗ.

### 2.13 Trình duyệt thật

Phần này ghi tay ở cuối M3.log, không do kịch bản chạy. Kết quả có hai nửa, và bạn cần nói rõ cả hai khi trình bày.

Trên máy ảo, qua HTTPS: trình duyệt dựng sẵn mà agent dùng từ chối chứng chỉ tự ký (`ERR_CERT_AUTHORITY_INVALID`) và không có nút "vẫn tiếp tục". Qua đồ nghề HTTP ở 2.11, trình duyệt mở được trang đăng nhập của panel thật trên máy ảo, nhưng agent không được gõ mật khẩu vào một trang ở địa chỉ mạng riêng. Phiên chính làm tiếp bằng một bộ chuyển cổng trên Mac (`127.0.0.1:18443` tới cổng 8081 của máy ảo), nên trình duyệt vào panel thật của máy ảo qua địa chỉ loopback: đăng nhập, Tạo site `c.example.com`, Xem trước (13 bước), Chạy; đọc trang job ba lần trong lúc chạy thì nội dung đổi theo tiến độ (từ bước `wp rewrite structure` tới các phép kiểm chứng, rồi tới phép kiểm cô lập hai chiều), job xong trong 4,6 giây, tài khoản quản trị hiện một lần và mất khi tải lại, Bảng điều khiển có 2 tuyến đều 200, console không có lỗi. Bằng chứng: đoạn "Bước 4: tạo site từ trình duyệt trên máy ảo" ở cuối M3.log. Đây là đồ nghề test; người dùng thật vào bằng HTTPS và tự so fingerprint.

Trên Mac, chế độ `--demo`, cùng bản giao diện (cùng file `/assets/index-m5MYbe2-.js`):

```
$ <scratchpad>/tpan-mac serve --demo --listen 127.0.0.1:18080
2026/11/19 16:38:22 INFO tpan đang chạy listen=127.0.0.1:18080 tls=false demo=true version=7de6977-dirty
```

Đăng nhập sai báo "Sai mật khẩu. Gõ lại. Quên mật khẩu thì xem lệnh ở dưới."; đúng thì vào Bảng điều khiển. Ở `/sites/new`, nhập `c2.example.com` rồi "Xem trước":

```
     TÓM TẮT  SẼ CHẠY 13 bước · BỎ QUA 1 bước · FILE ĐỔI 2 file · ĐỐI TƯỢNG c2.example.com
     PHẠM VI ẢNH HƯỞNG  Dịch vụ sẽ reload: php8.3-fpm nginx
```

Bấm "Chạy 13 bước" thì sang trang job, có khối "TÀI KHOẢN QUẢN TRỊ WORDPRESS" và nút "Đã lưu, ẩn đi"; tải lại trang thì khối đó không còn. Log hiện dần thì **không** thấy được ở `--demo`: máy giả chạy xong job trong 5 ms, trang mở ra thì job đã dừng. Mở hộp thoại xoá site thì console có đúng một dòng CSP (lý do ở mục 4, "CSP"), trang vẫn khoá cuộn và hộp thoại vẫn dùng được.

### 2.14 Chưa chạy

- `make dev-api` và `make dev-web`: chưa chạy trong M3.log. Thứ đã chạy là một bản `go build` rồi `serve --demo --listen 127.0.0.1:18080` (2.13). Tự thử trên Mac: terminal một `make dev-api` (tức `go run ./cmd/tpan serve --demo --listen 127.0.0.1:8080`, in mật khẩu demo), terminal hai `make dev-web`, rồi mở `http://localhost:5173`. Vite chuyển mọi request `/api` sang cổng 8080 (`web/vite.config.ts`).
- `tpan setup --private`: chưa chạy trên máy ảo. Theo mã, file unit sẽ có `--listen 127.0.0.1:8443`. Tự thử: `scripts/vm.sh root tpan setup --private --dry-run`.
- Nối lại SSE bằng `Last-Event-ID`, và `204` khi không còn gì để gửi: chưa thử trên máy ảo, chỉ có unit test. Mình chạy trên Mac trong phiên viết tài liệu này (chỉ giữ các dòng `=== RUN`, `--- PASS`, `PASS`, `ok`; bỏ dòng log mà test in ra):

```
$ go test ./internal/server/ -run 'TestSSE|TestCSRF|TestCookieKhongTLS|TestAPIJobSecretOnce' -v
=== RUN   TestAPIJobSecretOnce
--- PASS: TestAPIJobSecretOnce (0.06s)
=== RUN   TestCookieKhongTLSKhongDungQuaTLS
--- PASS: TestCookieKhongTLSKhongDungQuaTLS (0.09s)
=== RUN   TestCSRF
--- PASS: TestCSRF (0.07s)
=== RUN   TestSSEFinishedJobReplay
--- PASS: TestSSEFinishedJobReplay (0.00s)
=== RUN   TestSSELive
--- PASS: TestSSELive (0.00s)
=== RUN   TestSSEClientGone
=== RUN   TestSSEClientGone/live
=== RUN   TestSSEClientGone/other
--- PASS: TestSSEClientGone (0.21s)
PASS
ok  	github.com/doxuta/tpan/internal/server	0.909s
```

  `TestSSEFinishedJobReplay` gửi `Last-Event-ID: 1` và đòi nhận đúng sự kiện 2, 3; gửi `?after=3` và đòi `204` thân rỗng.

## 3. Đi qua mã theo thứ tự chạy

Thứ tự: bạn gõ `tpan setup`, systemd chạy `tpan serve`, trình duyệt mở trang, đăng nhập, tạo site, nghe log, lấy mật khẩu. Sau đó là các phần đứng riêng: số liệu máy, chế độ demo, và phần còn lại của giao diện.

### 3.1 `cmd/tpan/setup.go`: setup là một Plan

`setupCmd` kiểm root (`requireRoot`), gọi `buildSetupPlan`, rồi đi đúng đường của mọi lệnh có thay đổi: `previewOrConfirm` (xem trước, hỏi lại, `--dry-run`), mở DB, `engine.Execute`. Tức setup không có đường riêng để ghi file: nó dùng lại `steps.Dir`, `steps.File`, `steps.Service` của M1, nên có Check trước Apply, có hoàn tác, có job trong Lịch sử, và chạy lại thì "đã đúng, bỏ qua" (2.2).

```go
	plan := engine.Plan{
		Kind:   "panel.setup",
		Target: listen,
		Title:  i18n.T("Khởi tạo panel tpan", "Set up the tpan panel"),
		Steps: []engine.Step{
			secretDir(setupConfDir),
			secretDir(setupTLSDir),
			&steps.File{
				Path: setupCertPath, Content: certPEM, Perm: 0o600, Owner: system.Root,
				...
			},
			&setupKeyFile{File: &steps.File{ ... }},
			secretDir(setupStateDir),
			&steps.File{
				Path: setupUnitPath, Content: setupUnitFile(listen), Perm: 0o644, Owner: system.Root,
				Reload: reload,
```

Bốn chỗ đáng chỉ ra:

- `setupCert` đọc chứng chỉ và khoá đang có; nếu cả hai đọc được và khớp nhau (`tls.X509KeyPair`) thì dùng lại đúng nội dung đó. Không làm vậy thì mỗi lần setup sinh chứng chỉ mới, bước ghi chứng chỉ không bao giờ "đã đúng", và trình duyệt cảnh báo lại sau mỗi lần chạy.
- `newSelfSignedCert` sinh khoá ECDSA P-256, `NotBefore` lùi một giờ (đồng hồ hai máy lệch chút vẫn nhận), `NotAfter` 10 năm, SAN gồm hostname (nếu khớp `dnsName`), `localhost` và mọi IP của máy trừ link-local. `certFingerprint` lấy sha256 của chứng chỉ dạng DER, in dạng `AB:CD:...` giống `openssl x509 -fingerprint -sha256`.
- `setupKeyFile` bọc `steps.File` và xoá `Files` khỏi `Info()`. Lý do: engine đưa nội dung mọi file trong `Info` vào bản xem trước, mà bản xem trước in ra màn hình và ra JSON. Khoá riêng không bao giờ được hiện.
- `setupAdminPassword` chỉ sinh mật khẩu khi DB chưa có hash, và chạy **sau** khi job thành công. Chạy lại setup không bao giờ đổi mật khẩu; muốn đổi thì `tpan admin passwd`.

`setupUnitFile` ghi file unit có `ExecStart=... serve --listen <listen> --tls-cert ... --tls-key ...`. Địa chỉ nghe nằm trong cờ ở đây, nên M3 chưa cần `/etc/tpan/config.toml` (mục 5).

### 3.2 `cmd/tpan/admin.go`: `adminPasswdCmd`

Sinh mật khẩu bằng `store.GeneratePassword` (là `rand.Text()`: 26 ký tự A-Z và 2-7, ít nhất 128 bit ngẫu nhiên), băm bằng `store.HashPassword`, rồi gọi `ResetAdminPassword` trong `internal/store/auth.go`: một transaction gồm ba câu, ghi hash mới, `DELETE FROM sessions`, `DELETE FROM login_failures`. Một transaction để không bao giờ có lúc mật khẩu đã đổi mà phiên của người cũ vẫn sống. Lỗi ghi `audit` chỉ in ra stderr rồi vẫn in mật khẩu: mật khẩu đã đổi rồi, không in là không ai vào được.

Lệnh này không đi qua engine vì nó không đổi gì trên máy ngoài DB của tpan.

### 3.3 `cmd/tpan/root.go`: `serveCmd` và `server.Run`

systemd chạy dòng `ExecStart`. `serveCmd` kiểm hai luật trước khi mở cổng:

```go
			if (certFile == "") != (keyFile == "") {
				return fmt.Errorf("--tls-cert và --tls-key phải đi cùng nhau")
			}
			// Không TLS thì mật khẩu đi trên mạng dạng chữ thường (mục 11): chỉ
			// cho nghe ở loopback (máy dev, --demo).
			if certFile == "" && !isLoopbackListen(listen) {
				return fmt.Errorf("không có --tls-cert/--tls-key thì chỉ được nghe 127.0.0.1 hoặc localhost (đang là %q)", listen)
			}
```

Rồi dựng `server.Deps`: máy thật (`e.sys`, DB ở `/var/lib/tpan/tpan.db`, hostname), hoặc với `--demo` thì máy giả từ `demo.New` (mục 3.12). `server.Run` trong `internal/server/server.go` tạo `http.Server` với `ReadHeaderTimeout: 10 * time.Second`, gọi `ListenAndServeTLS` khi có chứng chỉ, `ListenAndServe` khi không. Một goroutine chờ `ctx.Done()` (Ctrl+C hay `systemctl stop`) rồi `Shutdown`, cho request đang chạy tối đa 10 giây.

### 3.4 `internal/server/server.go`: `NewHandler`, bảng đường dẫn

```go
	mux.HandleFunc("GET /api/v1/health", handleHealth)
	mux.HandleFunc("POST /api/v1/login", s.handleLogin)
	mux.HandleFunc("POST /api/v1/logout", s.handleLogout)
	mux.HandleFunc("GET /api/v1/session", s.handleSession)
	auth := s.requireSession
	mux.Handle("POST /api/v1/account/password", auth(s.handlePasswordChange))
	...
	mux.Handle("POST /api/v1/sites", auth(s.handleSiteCreate))
	...
	mux.Handle("GET /api/v1/jobs/{id}/events", auth(s.handleJobEvents))
	mux.Handle("GET /api/v1/databases", auth(s.handleDatabases))
	mux.HandleFunc("/api/", handleAPINotFound)
	mux.Handle("/", spa(dist))
	// secure thêm header bảo mật và chặn CSRF cho mọi request (auth.go).
	return s.secure(mux)
```

- `http.ServeMux` của thư viện chuẩn nhận mẫu có phương thức và tham số (`"GET /api/v1/sites/{domain}"`); handler đọc tham số bằng `r.PathValue("domain")`. Không cần framework (mục 3 của KE-HOACH.md).
- Bốn đường đầu công khai; mọi đường còn lại bọc trong `requireSession`. Danh sách đầy đủ và ý nghĩa từng mã lỗi nằm ở chú thích đầu `internal/server/types.go`.
- `/api/` bắt mọi đường API gõ sai, trả JSON `404 not_found` thay vì rơi xuống `spa` và trả trang HTML.
- `spa(dist)`: đường dẫn trùng một file thật trong giao diện đã build (ví dụ `/assets/index-m5MYbe2-.js`) thì trả file; còn lại trả `index.html` với `Cache-Control: no-cache`, để react-router phía trình duyệt tự xử lý `/sites/new`. `dist` là `web.DistFS()`, tức thư mục `web/dist` đã nhúng vào file chạy (mục 4, "go:embed").
- `secure` bọc ngoài cùng, nên header bảo mật có ở mọi response, kể cả file tĩnh và 404 (2.3).

`NewHandler` nhận `dist` làm tham số thay vì tự gọi `web.DistFS()` để test đưa vào một thư mục giả (`TestSPA`, `TestSPAWithoutBuild`).

### 3.5 Trình duyệt mở trang: `index.html`, `theme.js`, `main.tsx`, `routes.tsx`, `Layout.tsx`

Thứ tự trình duyệt chạy:

1. `web/index.html` nạp `<script src="/theme.js">` trong `<head>`. File `web/public/theme.js` đọc khoá `tpan-theme` trong `localStorage`, không có thì theo `prefers-color-scheme`, rồi đặt `data-theme` trên `<html>`. Nó chạy **trước** khi trang vẽ, nên người dùng chế độ tối không thấy chớp trắng. Nó là file riêng, không viết thẳng trong HTML, vì CSP cấm script nội tuyến (mục 4).
2. `web/src/main.tsx` dựng router từ danh sách `screens` của `web/src/routes.tsx`, bọc các provider: `LangProvider` (ngôn ngữ), `TipsProvider` (công tắc Gợi ý), `QueryClientProvider` (TanStack Query). Màn có `bare: true` (Đăng nhập) vẽ không có khung; các màn khác là con của `Layout`. Mỗi màn tải lười (`lazy`), thành một file JS riêng.
3. `routes.tsx` là danh sách **duy nhất** các màn: đường dẫn, cách tải component, mục menu (nếu có), số đếm cạnh mục. Menu chính trong `Layout.tsx` dựng từ chính danh sách này, nên không thể có mục menu trỏ tới màn chưa có, hay màn có mà menu quên.
4. `Layout` gọi `useSession()` (tức `GET /api/v1/session`). Chưa đăng nhập thì `<Navigate to={loginHref(...)} replace />`: chuyển sang `/dang-nhap?next=<chỗ đang đứng>`, đăng nhập xong quay lại đúng chỗ. `safeNext` trong `client.ts` chỉ nhận `next` bắt đầu bằng `/` và không phải `//`, để trang đăng nhập không thành chỗ chuyển hướng ra trang ngoài.

Đã đăng nhập thì `Layout` vẽ thanh đầu trang (logo đổi theo `machineStatus`, VI/EN, Sáng/Tối, Gợi ý), menu, và `<Outlet />` là màn đang mở.

### 3.6 Đăng nhập: `client.ts` tới `handleLogin`

Mọi request của giao diện đi qua một hàm, `request` trong `web/src/api/client.ts`:

```ts
    res = await fetch(path, {
      method,
      credentials: "same-origin",
      headers: body === undefined ? { Accept: "application/json" } : { Accept: "application/json", "Content-Type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
```

`credentials: "same-origin"` để cookie phiên tự đi kèm. Lỗi nào cũng thành `ApiError` (mã HTTP, thân `ErrorBody`, `retryAfter` đọc từ header `Retry-After`). Gặp `401` mà mã không phải `wrong_password` thì tải lại hẳn trang đăng nhập, để bỏ mọi dữ liệu của phiên cũ đang nằm trong bộ nhớ.

Phía server, request đi qua `secure` trước (3.7), rồi tới `handleLogin` trong `internal/server/auth.go`:

```go
	ip := clientIP(r)
	if s.authLocked(w, r, ip) {
		return
	}
	ok, done := s.authCheckPassword(w, r, req.Password)
	if done {
		return
	}
	if !ok {
		s.authFailed(w, r, ip, "login.fail")
		return
	}
	if err := st.ClearLoginFailures(r.Context(), ip); err != nil {
```

- `authDecode` giới hạn thân request 4 KB (`authMaxBody`) trước khi đọc JSON: argon2 trên vài MB là cách làm chậm máy rẻ tiền.
- `clientIP` lấy IP của kết nối trực tiếp, **không** đọc `X-Forwarded-For`: tin header đó thì ai cũng tự đổi IP để né khoá.
- `authLocked` hỏi `LoginLockedUntil` **trước** khi băm. Đây là lý do 0,003 giây ở 2.4: IP đang khoá thì không tốn argon2.
- `authFailed` gọi `RecordLoginFailure`; lần sai thứ 5 trong 15 phút thì khoá và trả `423` kèm `Retry-After`, các lần trước trả `401 wrong_password`. Cả hai đều ghi `audit`.
- Đúng mật khẩu: xoá bộ đếm của IP, dọn các phiên đã hết hạn, sinh token 32 byte từ `crypto/rand` (mã hoá base64 URL), lưu `authTokenHash(token)` (sha256) vào bảng `sessions`, đặt cookie bằng `authSetCookie`, trả `204`.

```go
	http.SetCookie(w, &http.Cookie{
		Name:     authCookieName(r),
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		Secure:   r.TLS != nil,
		HttpOnly: true, // script trên trang không đọc được token
		SameSite: http.SameSiteStrictMode,
	})
```

`authCookieName(r)` là `__Host-tpan` khi request đến qua TLS, `tpan` khi không (2.4 và 2.11). Không đặt `Expires`: cookie mất khi đóng trình duyệt, còn phiên hết hạn do server quyết (12 giờ không dùng).

### 3.7 `secure`: header bảo mật và CSRF

`secure` nằm trong `auth.go`, `NewHandler` gọi nó ở dòng cuối:

```go
		hd.Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'")
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("Referrer-Policy", "no-referrer")
		// GET, HEAD, OPTIONS luôn qua; POST, DELETE từ origin khác thì bị chặn.
		if err := s.auth.cop.Check(r); err != nil {
			writeError(w, http.StatusForbidden, "csrf", i18n.T(
```

`cop` là `http.NewCrossOriginProtection()` của thư viện chuẩn Go (có từ Go 1.25; repo dùng Go 1.27). Tài liệu của đúng bản Go đang cài (`go doc net/http.CrossOriginProtection`, chạy trên Mac) ghi: request khác origin được nhận ra bằng header `Sec-Fetch-Site`, hoặc bằng cách so hostname trong `Origin` với `Host`; `GET`, `HEAD`, `OPTIONS` luôn được qua; request không có cả `Sec-Fetch-Site` lẫn `Origin` được coi là cùng origin hoặc không phải trình duyệt, và được qua. Request không mang cả hai header, như `curl` của kịch bản lúc đăng nhập ở 2.4, thì đi qua: đó không phải trình duyệt, nên không có chuyện trang khác lợi dụng cookie của nó.

`secure` không bọc `ResponseWriter` (không thay `w` bằng một kiểu khác), để SSE vẫn `Flush` được (3.9).

### 3.8 `requireSession` và `authCurrent`: mỗi request sau đó

`requireSession` gọi `authCurrent`: đọc cookie (bỏ qua giá trị rỗng hay dài hơn 128 ký tự), băm sha256, tìm trong bảng `sessions` bằng `SessionByToken`. Quá 12 giờ không dùng (`authIdle`) thì xoá phiên và coi như chưa đăng nhập. Còn sống thì cập nhật `last_seen_at`, nhưng chỉ khi lần ghi trước cũ hơn 1 phút (`authTouchEvery`): giao diện hỏi `/overview` mỗi 30 giây, ghi DB mỗi request là phí. Phiên tìm được gắn vào context của request (`authCtxKey`) để handler sau lấy ra bằng `authSessionOf`.

`handleSession` (công khai) trả `expires = LastSeen + 12 giờ`. Ở 2.5, phiên tạo lúc `09:51:10Z` có `"expires": "2026-11-19T21:51:10.434073503Z"`.

`handlePasswordChange` (màn Cài đặt) kiểm mật khẩu hiện tại qua đúng `authLocked`, `authCheckPassword`, `authFailed` như đăng nhập: phiên bị chiếm cũng không dùng form đổi mật khẩu để dò mật khẩu được. Đổi xong thì `ChangeAdminPassword` xoá mọi phiên trừ phiên hiện tại, trong một transaction.

### 3.9 Tạo site: `TaoSite`, `handleSiteCreate`, `jobStart`

Phía giao diện (`web/src/screens/tao-site/index.tsx`, component `TaoSite`): hai `useMutation`. "Xem trước" gửi `post<Preview>("/api/v1/sites?dry_run=1", body)` và cất `body` vào `sent`; màn vẽ bản xem trước bằng `PlanReview` (`web/src/screens/duyet-plan/index.tsx`). "Chạy" gửi **đúng** `sent`, không phải chữ đang nằm trong ô, rồi chuyển sang `/jobs/<job_id>`. Sửa bất kỳ ô nào là bỏ bản xem trước: không bao giờ chạy một thứ khác với thứ bạn vừa đọc. Duyệt Plan không có đường dẫn riêng trong `routes.tsx`; `PlanReview` được nhúng ở Tạo site và ở nút Xoá site.

Phía server, `handleSiteCreate` trong `internal/server/api_job.go`:

1. Đọc JSON với `DisallowUnknownFields` và giới hạn 64 KB; `validate.ParseDomain`, `validate.ParseEmail`. Domain sai là `400 domain_invalid` trước khi đụng tới core.
2. `?dry_run=1`: `PrepareAdd` rồi `Preview`, trả `200`. Không giữ chỗ, không lấy khoá (2.8: xem trước vẫn chạy khi CLI giữ khoá).
3. Chạy thật: `s.api.claim()` giành chỗ "đang có job" trong tiến trình này; có rồi thì `409 job_running` ngay. Rồi `PrepareAdd` ngay trong request, để lời từ chối của core (`site_exists_unmanaged`, `stack_not_installed`...) về thẳng người gọi dạng `409` kèm `checks`. Mã HTTP chọn ở `apiErrorStatus` trong `api.go`.
4. `jobStart` chạy `add.Execute` trong một goroutine và chờ sự kiện đầu tiên:

```go
	onEvent := func(ev engine.Event) {
		if run.feed == nil {
			run.id, run.feed = ev.JobID, sseNewFeed()
			a.mu.Lock()
			a.runs[ev.JobID] = run
			a.mu.Unlock()
			started <- ev.JobID
		}
		run.feed.publish(ev)
	}

	a.wg.Go(func() {
		res, err := fn(context.Background(), onEvent)
```

Sự kiện đầu của engine luôn là `job.start` và mang job id. Có id thì handler trả `202 {"job_id": ...}` (9 mili giây ở 2.7); job chạy tiếp ở nền.

Vì sao `context.Background()` mà không phải `r.Context()`: context của request bị huỷ khi trả lời xong hoặc khi người dùng đóng tab. Job dừng giữa chừng thì phải hoàn tác, mà hoàn tác bằng một context đã huỷ thì lỗi nốt. Nên job có context riêng, sống tới khi xong.

`jobLockSystem` bọc `System` để `Lock` chỉ chờ khoá toàn cục 2 giây (`apiLockWait`). Engine chờ khoá không giới hạn, đúng cho CLI; với API, chờ mãi nghĩa là request treo tới khi job của CLI xong. Đây là 2,049 giây ở 2.8.

Khi goroutine xong: cất mật khẩu quản trị vào `a.secrets` (nếu `res.WPInstalled`), bỏ bộ phát, bỏ đệm phép kiểm của domain, nhả `busy`, đóng `run.done`, ghi `audit`.

`handleSiteDelete` đi đúng đường này với `PrepareRemove`.

### 3.10 Log trực tiếp: `subscribeJobEvents` tới `handleJobEvents`

Trang job (`web/src/screens/lich-su/JobDetail.tsx`) dùng hook `useJob` trong `useJob.ts`: `GET /api/v1/jobs/{id}` một lần, rồi nếu job còn `running` thì `subscribeJobEvents(id, ..., after = seq cuối đã có)`. Hàm đó trong `client.ts` mở `new EventSource(...)` và nghe theo từng tên sự kiện (`job.start`, `command`, `output`, ...), vì server gửi `event: <kind>`. Nhận `job.done` thì tự `close()`. `useJob` gom sự kiện 100 ms một lần rồi mới vẽ (`flushMs`), vì `apt` in hàng nghìn dòng.

Phía server, `internal/server/sse.go`. Mỗi job đang chạy có một `sseFeed`: giữ mọi sự kiện trong bộ nhớ, và đánh thức người nghe bằng cách đóng kênh `wake` rồi thay kênh mới. Engine phát sự kiện không bao giờ phải chờ người nghe chậm: mỗi người nghe tự đọc phần mình chưa gửi bằng `since(after)`.

```go
func sseFromFeed(ctx context.Context, st *sseStream, f *sseFeed) {
	for {
		evs, done, wake := f.since(st.last)
		if err := st.send(evs); err != nil || st.sent || done {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-wake:
		}
	}
}
```

`sseStream.send` ghi mỗi sự kiện thành ba dòng `id:`, `event:`, `data:` cộng một dòng trống, rồi `Flush` một lần (`http.ResponseController`). `Flush` là chỗ đẩy byte ra mạng ngay, không chờ đệm đầy.

`handleJobEvents` chọn nguồn: job đang chạy trong tiến trình này thì đọc bộ phát; job khác (đã xong, hoặc đang chạy ở CLI) thì `sseFromStore` đọc bảng `job_events` mỗi 500 ms (`sseStorePoll`). `sseAfter` đọc chỗ nối lại từ header `Last-Event-ID` (trình duyệt tự gửi khi nối lại) hoặc `?after=N`. Job đã xong và không còn sự kiện nào sau chỗ đó thì trả `204`: `EventSource` tự nối lại cả khi server đóng luồng bình thường, và chỉ dừng hẳn khi gặp `204` (hay trang tự `close()`).

### 3.11 Mật khẩu WordPress đúng một lần: `handleJob`, `takeSecret`, `useJob`

```go
	if run := s.api.run(id); run != nil && run.feed.finished() {
		select {
		case <-run.done:
		case <-r.Context().Done():
			return
		}
	}
```

Đoạn này ở đầu `handleJob` (`api.go`). Giao diện GET lại job ngay khi thấy `job.done`; lúc đó goroutine của job có thể chưa kịp cất mật khẩu. Không chờ `run.done` thì GET đó thấy job "xong" mà không có mật khẩu. Một lần GET sau đó vẫn sẽ lấy được, nhưng giao diện không bao giờ GET lại (`useJob` đặt `staleTime: Infinity`, không đọc lại khi chuyển tab), nên mật khẩu nằm chờ trong bộ nhớ tới khi tpan khởi động lại mà người dùng không thấy. Có mật khẩu rồi thì `takeSecret` lấy mật khẩu ra khỏi map rồi xoá: lần GET sau không còn (2.7). Mật khẩu chỉ nằm trong bộ nhớ của tiến trình: không DB, không log; tpan khởi động lại thì mất.

Phía trình duyệt, `useJob.ts` tách `secret` ra khỏi kết quả **trước** khi TanStack Query cất vào cache, giữ nó trong một `Map` của module (không `localStorage`), và `staleTime: Infinity`, `refetchOnWindowFocus: false` để không có lần GET tự động nào "tiêu" mất mật khẩu. `SecretBox.tsx` vẽ khối có nút chép và chỉ biến mất khi bạn bấm "Đã lưu, ẩn đi".

### 3.12 Xuống core, engine, store

Từ `add.Execute` trở xuống là đường của M2: `core.SiteAdd.Execute` gọi `engine.New(s.Sys, engine.Options{Journal: s.Journal, OnEvent: onEvent}).Execute(...)`. `OnEvent` là móc duy nhất giữa engine và SSE: engine gọi nó cho mọi sự kiện, theo thứ tự `Seq`, ngay khi có (`internal/engine/types.go`). Cùng lúc engine ghi sự kiện vào `Journal`, tức `store.Store` (bảng `jobs`, `job_events`). Vì vậy log trực tiếp và log đọc lại sau này là cùng một dữ liệu. Job xong thì `SiteAdd.Execute` ghi bảng `sites`.

Server dựng `core.Sites` bằng `apiSites()` trong `api.go`, đưa `jobLockSystem` vào thay cho `System` thật.

### 3.13 Bảng điều khiển: `handleOverview`, `apiSiteHealth`, `internal/monitor`

`handleOverview` gom mọi thứ Bảng điều khiển cần trong một request: cấu hình stack, danh sách site, 10 job mới nhất, số liệu máy, phép kiểm stack, phép kiểm từng site.

```go
	ov.Machine = s.apiMachine(ctx)
	var stackErr error
	var wg sync.WaitGroup
	wg.Go(func() {
		h, err := s.apiStack().Health(ctx)
		ov.StackChecks, stackErr = apiNonNil(h.Results), err
	})
	for i, st := range sites {
		wg.Go(func() { ov.Sites[i] = s.apiSiteRow(ctx, st, refresh) })
	}
	wg.Wait()
```

Số liệu máy đo trước, một mình; phép kiểm chạy song song sau đó. Lý do ở mục 5.

`apiSiteHealth` (`api_checks.go`) giữ kết quả phép kiểm của mỗi site 60 giây (`apiChecksTTL`), vì `wp core verify-checksums` đọc lại toàn bộ mã nguồn WordPress và mất vài giây. `?refresh=1` chạy lại ngay. Job `site.add`, `site.rm` xong thì bỏ mục của domain đó. Phép kiểm stack thì không đệm, chạy mỗi lần: đó là lý do lần hai ở 2.9 vẫn mất 0,36 giây. M4 thay cả chỗ này bằng health chạy định kỳ.

`internal/monitor/monitor.go`, hàm `Read`: đọc `/proc/stat` hai lần cách nhau `CPUSample` (200 ms) để ra phần trăm CPU, rồi `/proc/meminfo`, `/proc/loadavg`, `/proc/uptime`, và `StatFS` của `/var/www`. Mọi thứ đọc qua `system.System`, không qua `os`, để `--demo` đưa vào `/proc` giả. Phần nào lỗi thì ghi vào `Errors`, các phần khác vẫn trả. `GET /api/v1/machine` mất 0,23 giây ở 2.9 chính vì 200 ms chờ này.

### 3.14 `internal/demo`: máy giả cho `--demo`

`demo.New` dựng một `FakeSystem` có sẵn stack nginx, PHP 8.3, MariaDB, `/proc` của máy 4 CPU 4 GB, và một `tpan.db` trong thư mục tạm. Ba site mẫu được tạo bằng **chính** `core.Sites` (hàm `seed`), nên lịch sử job và sự kiện là thật chứ không bịa: `blog.example.com` và `shop.example.net` đạt mọi chặng, `wiki.example.org` hỏng ở chặng PHP (`breakPool` xoá socket), cộng một job tạo `news.example.com` thất bại đã hoàn tác (`addRolledBack`). `pastJournal` lùi thời điểm ghi để lịch sử trông như đã tích luỹ nhiều ngày.

`demo.Hashes()` trả mã băm của tarball WordPress và WP-CLI giả. `serveCmd` đưa chúng vào `Deps.WPHash`, `Deps.WPCLISHA512`; thiếu thì tạo site trên giao diện demo hỏng ở bước kiểm checksum, vì core ghim mã băm của bản thật. Chế độ demo vẫn bắt đăng nhập (mật khẩu mới mỗi lần, in ra terminal) để giao diện đi đúng luồng như máy thật.

### 3.15 Phần còn lại của giao diện

- `web/src/i18n/`: mỗi khu một file (`khung.ts`, `tao-site.ts`, `lich-su.ts`...). Mỗi file viết bản `vi`, rồi `defineStrings(vi, { ... })` cho bản `en`. Chữ ký `defineStrings<T>(vi: T, en: NoInfer<T>)` bắt bản tiếng Anh đúng kiểu của bản tiếng Việt: thiếu hay thừa một khoá là `tsc` báo lỗi. Trong component: `const t = useStrings(strings)`. Câu song ngữ đến từ API (`{"vi": ..., "en": ...}`) thì dùng `tx(text, lang)`.
- `web/src/theme.ts`: phần sau khi trang đã vẽ. `setTheme` ghi hoặc xoá khoá `tpan-theme` (trùng tên với `theme.js`), đặt `data-theme`, báo cho component qua `useSyncExternalStore`. Chưa chọn gì thì đổi theo hệ điều hành ngay cả khi panel đang mở.
- `web/src/styles.css`: bảng biến màu của mục 3 GIAO-DIEN.md cho `[data-theme="light"]` và `[data-theme="dark"]`, rồi `@theme inline` đưa chúng vào Tailwind (`--color-*: initial` tắt bảng màu có sẵn, nên viết `bg-red-500` không ra class nào). `@font-face` trỏ tới file woff2 của gói `@fontsource`, chỉ phần Latin và tiếng Việt. Cuối file có luật `body[data-scroll-locked]` (mục 5, "Radix").
- `web/src/components/StatusMark.tsx`: component **duy nhất** vẽ bốn trạng thái (chấm tròn, hình thoi, ô vuông có ×, vòng rỗng), mỗi cái có `role="img"` và `aria-label` dạng "Pool PHP: lỗi". Không ai vẽ lại dấu trạng thái ở chỗ khác, nên đổi hình là đổi một chỗ.
- Các màn trong `web/src/screens/`: `bang-dieu-khien` (bản đồ tuyến `RouteMap.tsx`, `Machine.tsx`, `History.tsx`, cột bên `Side.tsx`), `site` (`SiteList`, `SiteDetail` với đường đi request của một site), `tao-site`, `lich-su` (`JobList`, `JobDetail`), `co-so-du-lieu` (chỉ xem), `cai-dat` (mật khẩu, phiên, ngôn ngữ, sáng tối), `dang-nhap`, và `duyet-plan` (`PlanReview`, nhúng vào màn khác).
- `web/src/api/queries.ts`: `useSession`, `useOverview` dùng chung một `queryKey`, nên menu đếm site và Bảng điều khiển cùng đọc một lần gọi `/overview`. `useOverview` đọc lại mỗi 30 giây, kể cả khi tab nằm nền, vì logo và biểu tượng tab báo tình trạng máy.

## 4. Kiến thức nền

### HTTP và JSON API

Trình duyệt và server nói với nhau bằng request (phương thức `GET`, `POST`, `DELETE`, đường dẫn, header, thân) và response (mã trạng thái, header, thân). API JSON nghĩa là thân là dữ liệu JSON thay vì trang HTML. Mã trạng thái mang nghĩa: `200` được, `202` đã nhận và đang làm ở nền, `204` được và không có thân, `400` đầu vào sai, `401` chưa đăng nhập, `403` bị chặn, `404` không có, `409` xung đột với trạng thái hiện tại, `423` bị khoá, `500` lỗi của server.

Trong mã: quy ước mã lỗi ở đầu `internal/server/types.go`; `apiErrorStatus` trong `api.go`; `writeError`, `writeJSON` trong `server.go`. Mọi lỗi có cùng dạng `{"error": {"code": ..., "message": {"vi": ..., "en": ...}}}`.

Tự xem: `scripts/vm.sh exec curl -sk https://127.0.0.1:8443/api/v1/health` (chưa chạy; trong M3.log lệnh này chạy từ Mac với `--pinnedpubkey` và trả `200`).

### Cookie và các thuộc tính `Secure`, `HttpOnly`, `SameSite`, `__Host-`

Cookie là một cặp tên và giá trị mà server bảo trình duyệt giữ (header `Set-Cookie`), rồi trình duyệt tự gửi lại ở mọi request sau tới cùng nơi. `Secure`: chỉ gửi qua HTTPS. `HttpOnly`: script trên trang không đọc được, nên một đoạn script lọt vào trang cũng không lấy được token. `SameSite=Strict`: trình duyệt không gửi cookie khi request bắt nguồn từ trang khác. Tiền tố `__Host-` bắt trình duyệt chỉ nhận cookie có `Secure`, `Path=/` và không có `Domain`, nên trang khác trên cùng tên miền cha không ghi đè được.

Trong mã: `authSetCookie`, `authCookieName`, hai hằng `authCookieTLS`, `authCookiePlain` trong `auth.go`.

Tự xem: dòng `set-cookie:` ở 2.4 và 2.11. Trong trình duyệt: công cụ dành cho nhà phát triển, tab Application, mục Cookies (chưa chạy).

### CSRF, `Sec-Fetch-Site` và `Origin`

CSRF (cross-site request forgery): bạn đang đăng nhập panel, rồi mở một trang lạ; trang đó gửi `POST https://<panel>/api/v1/sites` từ trình duyệt của bạn, và trình duyệt tự gắn cookie của bạn vào. Server thấy cookie hợp lệ nên làm theo. Chống bằng cách hỏi "request này bắt nguồn từ đâu". Trình duyệt hiện đại tự gửi `Sec-Fetch-Site` (`same-origin`, `same-site`, `cross-site`, `none`) và `Origin`, trang web không giả được hai header này. `SameSite=Strict` là lớp thứ hai.

Trong mã: `newAuthState` tạo `http.NewCrossOriginProtection()`; `secure` gọi `cop.Check(r)` cho mọi request. Theo chú thích trong `auth.go`, `GET`, `HEAD`, `OPTIONS` luôn qua. Vì vậy không handler `GET` nào được thay đổi máy.

Tự xem: các request 403 ở 2.6. Muốn tự thử thì lấy lệnh `curl` ở dòng có `Sec-Fetch-Site: cross-site` trong M3.log, thêm lại dấu nháy cho từng `-H`.

### CSP `default-src 'self'`

Content-Security-Policy là header bảo trình duyệt chỉ được nạp gì, từ đâu. `default-src 'self'` nghĩa là script, style, font, ảnh chỉ được tới từ chính panel, và **không** được viết thẳng trong HTML (`<script>...</script>`, `<style>...</style>`, `style="..."` qua JavaScript). Lợi ích: lỡ có lỗ hổng cho kẻ xấu chèn một đoạn `<script>` vào trang, trình duyệt cũng không chạy nó, và không có script nào gửi dữ liệu ra ngoài được. `frame-ancestors 'none'`: không trang nào nhúng panel vào `iframe` để lừa bạn bấm.

Hệ quả trong mã: script đặt sáng tối phải là file riêng `web/public/theme.js`; font tự chứa trong file chạy thay vì gọi Google Fonts; và thẻ `<style>` mà Radix Dialog chèn để khoá cuộn bị chặn, nên `styles.css` viết sẵn luật `body[data-scroll-locked]`.

Tự xem: header ở 2.3. Mở hộp thoại xoá site trong trình duyệt rồi xem console: một dòng `Applying inline style violates the following Content Security Policy directive` (2.13).

### TLS tự ký và fingerprint

TLS mã hoá đường truyền và chứng minh server là ai bằng chứng chỉ. Chứng chỉ thường do một tổ chức mà trình duyệt tin ký (ví dụ Let's Encrypt). Panel mới cài thường chưa có tên miền, nên `tpan setup` tự ký: đường truyền vẫn được mã hoá, nhưng trình duyệt không biết chứng chỉ của ai và cảnh báo. Cách biết đúng là panel của mình: so fingerprint (sha256 của chứng chỉ) setup in ra với fingerprint trình duyệt hiện trong cảnh báo. Khớp thì đúng; không khớp là có ai đứng giữa.

Trong mã: `newSelfSignedCert`, `certFingerprint`, `setupCert` trong `cmd/tpan/setup.go`; `ListenAndServeTLS` trong `server.Run`.

Tự xem: `scripts/vm.sh root openssl x509 -in /etc/tpan/tls/cert.pem -noout -fingerprint -sha256` (đã chạy, 2.1).

### Băm mật khẩu bằng argon2id, nhưng token chỉ cần sha256

Mật khẩu do người đặt hay nhớ được, nên ngắn và đoán được. Lỡ lộ DB, kẻ tấn công thử hàng tỉ mật khẩu với mỗi hash. argon2id là hàm băm cố tình chậm và tốn RAM (ở đây 64 MiB, 3 lượt, 4 luồng), nên mỗi lần thử tốn khoảng 0,07 giây như đo ở 2.4, thay vì vài phần triệu giây. Salt ngẫu nhiên 16 byte khiến hai lần băm cùng một mật khẩu ra hai chuỗi khác nhau. Token phiên thì khác: 32 byte ngẫu nhiên từ `crypto/rand`, không ai đoán nổi dù thử nhanh tới đâu, nên sha256 (nhanh) là đủ; mục đích chỉ là lộ DB thì không dựng lại được cookie.

Trong mã: `HashPassword`, `VerifyPassword` (đọc tham số từ chính chuỗi hash, so bằng `subtle.ConstantTimeCompare`, chặn hash có `m` quá 1 GiB) trong `internal/store/auth.go`; `authTokenHash` trong `internal/server/auth.go`.

Tự xem (chưa chạy): `scripts/vm.sh root python3 -c 'import sqlite3; c=sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True); print(c.execute("SELECT value FROM settings WHERE key = ?", ("admin.password_hash",)).fetchone()[0][:31])'`; theo mã (`HashPassword`), nó in `$argon2id$v=19$m=65536,t=3,p=4$`. Chú ý khoá là `admin.password_hash`, có dấu chấm.

### Server-Sent Events và WebSocket

SSE là một response HTTP không bao giờ kết thúc ngay: server giữ kết nối và ghi dần từng sự kiện dạng chữ (`id:`, `event:`, `data:`, dòng trống). Trình duyệt có sẵn `EventSource` để đọc, tự nối lại khi rớt mạng và gửi `Last-Event-ID` để server gửi tiếp từ chỗ đó. WebSocket cho hai chiều, nhưng phải nâng cấp kết nối sang giao thức khác và tự viết phần nối lại. Log của job chỉ đi một chiều, từ server ra trình duyệt.

Trong mã: `internal/server/sse.go`; `subscribeJobEvents` trong `client.ts`.

Tự xem (chưa chạy dạng này; M3.log chạy từ Mac): sau khi đăng nhập và có file cookie trong máy ảo, `scripts/vm.sh root curl -sN --cacert /etc/tpan/tls/cert.pem -b <file cookie> https://localhost:8443/api/v1/jobs/<id>/events`. Phải chạy bằng root vì `cert.pem` có quyền 600, chủ root. Job đã xong thì nhận hết rồi đóng; thêm `-H 'Last-Event-ID: 80'` thì chỉ nhận từ sự kiện 81.

### React, TanStack Query, react-router ở mức đủ hiểu

React: giao diện viết thành component, mỗi component là một hàm trả về "trang trông thế nào" từ dữ liệu hiện có; dữ liệu đổi thì React vẽ lại phần đổi. TanStack Query: lo việc gọi API, cất kết quả theo `queryKey`, nhiều component cùng cần một dữ liệu thì chỉ gọi một lần, tự gọi lại theo lịch. `useQuery` để đọc, `useMutation` để gửi thay đổi. react-router: đổi đường dẫn trên thanh địa chỉ mà không tải lại trang, nút Back đúng, đường dẫn có tham số (`/jobs/:id`). Repo dùng react-router 8 theo cách `createBrowserRouter` + `RouterProvider` (chú thích trong `main.tsx` gọi là "data mode"); mục 3 của KE-HOACH.md ghi "chế độ thư viện", tức dùng react-router như một thư viện trong ứng dụng Vite của mình, không dùng bộ khung dựng sẵn của nó.

Trong mã: `main.tsx`, `routes.tsx`, `api/queries.ts`, `screens/lich-su/useJob.ts`.

Tự xem: `make dev-api` và `make dev-web` rồi mở `http://localhost:5173` (chưa chạy, 2.14).

### Tailwind với biến CSS

Tailwind là bộ class viết sẵn (`border`, `px-[32px]`, `text-muted`), viết thẳng trong JSX thay vì đặt tên class rồi viết CSS riêng. Màu ở đây không viết mã màu trong class: `@theme inline` trong `styles.css` khai báo `--color-ink: var(--ink)`, nên class `text-ink` ra `color: var(--ink)`. Đổi `data-theme` trên `<html>` là biến `--ink` đổi giá trị, và mọi chỗ dùng `text-ink` đổi theo, không cần vẽ lại component.

Tự xem: trên trang panel, công cụ nhà phát triển, chọn thẻ `<html>`, đổi `data-theme` từ `light` sang `dark` (chưa chạy).

### `go:embed`: giao diện nằm trong file chạy

`//go:embed all:dist` trong `web/embed.go` bảo trình biên dịch Go chép cả thư mục `web/dist` (kết quả `vite build`) vào file chạy. `web.DistFS()` trả một hệ thống file chỉ đọc nằm trong bộ nhớ. Nhờ vậy cài tpan chỉ cần chép một file, và panel chạy được trong mạng kín. Tiền tố `all:` để nhúng cả `dist/.gitkeep`, giữ cho `go build` chạy được khi chưa build giao diện; khi đó `spa` trả `503` "Giao diện chưa được build. Chạy: make web". Xem thêm mục 10 của `00-go-can-ban.md`.

Tự xem: 2.3 (`index.html` trả về có thẻ trỏ tới `/assets/index-m5MYbe2-.js`, dù máy ảo không có thư mục `web/` nào). Kiểm thêm: `scripts/vm.sh exec ls -la /usr/local/bin` cho thấy panel chỉ là một file `tpan` (bên cạnh có `wp`, tức WP-CLI mà stack cài), không có thư mục giao diện nào (chưa chạy).

### systemd unit

systemd là chương trình khởi động và trông các dịch vụ trên Ubuntu. File unit nói chạy lệnh gì (`ExecStart`), chạy sau gì (`After=network-online.target`), chết thì làm gì (`Restart=on-failure`, `RestartSec=2s`), và bật lúc khởi động máy thì gắn vào đâu (`WantedBy=multi-user.target`). Sửa file unit xong phải `systemctl daemon-reload` để systemd đọc lại; tiến trình đang chạy thì vẫn chạy theo cờ cũ tới khi restart.

Trong mã: `setupUnitFile`; bước `steps.File` của file unit có `Reload: daemon-reload`; `steps.Service` chạy và bật dịch vụ; `printSetupResult` nhắc `systemctl restart tpan.service` khi file unit vừa đổi mà dịch vụ vốn đang chạy.

Tự xem: `scripts/vm.sh exec systemctl status tpan` (chưa chạy; `is-active` và `is-enabled` đã chạy ở 2.1). Thử tự hồi phục: `scripts/vm.sh root pkill -9 tpan`, đợi 3 giây, `scripts/vm.sh exec systemctl is-active tpan` (chưa chạy).

## 5. Cách khác đã cân nhắc, và chỗ đã làm sai

### WebSocket cho log trực tiếp

Ghi ở cột "Đã cân nhắc" của mục 3 KE-HOACH.md. Không chọn: log job chỉ đi một chiều; SSE là HTTP thường, đi qua được `secure`, `requireSession`, cookie như mọi request khác, và trình duyệt tự nối lại. Giá phải trả là phải tự lo chỗ `EventSource` nối lại mãi khi server đóng luồng, nên có nhánh trả `204` (3.10).

### Đếm đăng nhập sai trong bộ nhớ

Đếm trong một `map` của tiến trình là ít mã nhất. Không chọn, lý do ghi ngay trong `internal/store/migrations/003_auth.sql`: `tpan admin passwd` là một tiến trình khác, phải gỡ được khoá. Phiên đăng nhập cũng nằm trong DB vì cùng lý do, và vì thế phiên còn sau `systemctl restart tpan` (2.9). Ngược lại, mật khẩu WordPress chờ người lấy (`apiState.secrets`) cố ý chỉ nằm trong bộ nhớ: mất khi restart còn hơn nằm trong DB.

### Cookie `__Host-` cả khi không có TLS

Mục 11 lúc đầu chỉ ghi cookie `__Host-tpan`, `Secure`. Khi viết `make dev-api` và `--demo` (HTTP thường trên 127.0.0.1) thì cookie đó không đặt được: trang `http://` không đặt được cookie `Secure`, mà `__Host-` bắt buộc `Secure` (chú thích ở hằng `authCookiePlain`). Sửa: không TLS thì tên `tpan`, không `Secure`; và `serveCmd` từ chối nghe HTTP ở địa chỉ khác loopback. Đặc tả được sửa cho khớp ở commit `f8e1889`. `authCookieName` chọn theo từng request, nên cookie đặt qua `http` không được nhận qua `https` (`TestCookieKhongTLSKhongDungQuaTLS`).

### `/etc/tpan/config.toml` ngay ở M3

Mục 5 lúc đầu ghi địa chỉ nghe và chế độ nằm trong `config.toml`. Commit `d602b49` đổi: địa chỉ nghe và chế độ riêng tư là cờ của `tpan serve`, nằm trong file unit mà setup ghi. M3 khỏi phải đọc TOML (thêm một thư viện, thêm một file cấu hình phải kiểm lệch). `config.toml` để dành cho phần `[ai]` ở M8.

### Một cặp `vi.ts`, `en.ts`

Mục 10 lúc đầu ghi chuỗi giao diện nằm trong `web/src/i18n/vi.ts` và `en.ts`. Commit `d602b49` đổi sang mỗi khu một file, vì nhiều màn được làm song song: hai người cùng sửa một file `vi.ts` là xung đột liên tục. Kiểm thiếu khoá vẫn giữ được, nhờ `defineStrings`.

### Nới CSP cho Radix Dialog

Radix Dialog (qua thư viện `react-remove-scroll-bar`) chèn một thẻ `<style>` lúc mở hộp thoại để khoá cuộn trang. CSP chặn nó. Cách dễ là thêm `style-src 'unsafe-inline'` hoặc hash của thẻ đó vào CSP. Không chọn: `'unsafe-inline'` mở lại đúng thứ CSP đang chặn, còn hash thì đổi theo bề rộng thanh cuộn (M3.log ghi hai hash khác nhau ở hai lần mở). Thay vào đó `styles.css` tự viết luật `body[data-scroll-locked]` (thuộc tính mà thư viện vẫn gắn lên `<body>`). Cái giá: mỗi lần mở hộp thoại, console có một dòng lỗi CSP. Ghi vào mục 11 ở commit `f8e1889`; kiểm bằng trình duyệt ở 2.13: `"bodyOverflow": "hidden"` khi mở, `"visible"` khi đóng.

### Chỗ sai: đo CPU song song với phép kiểm (commit `06b4170`)

Bản đầu của `handleOverview` chạy ba thứ song song: số liệu máy, phép kiểm stack, phép kiểm site. Nghe hợp lý vì cả ba đều chậm. Sai ở chỗ CPU% là phần bận giữa hai lần đọc `/proc/stat` cách nhau 200 ms, mà trong 200 ms đó các phép kiểm (`wp core verify-checksums`, `nginx -t`, `curl`) đang chạy chính là tải. Bảng điều khiển luôn thấy CPU bận vì chính tpan: trên máy ảo 15 tới 30% khi máy rỗi, trong khi `/machine` gọi riêng ra 0% (chú thích trong commit). Phát hiện bằng cách so hai con số đó trên máy ảo. Sửa: đo máy trước, một mình, rồi mới chạy phép kiểm. Test `TestAPIOverviewCPUNotDuringChecks` và dòng `CPU% trong 3 lần GET /overview?refresh=1 ...: 1.9607843137254901 0 0` ở 2.9 giữ lỗi này không quay lại. Giá: `/overview` chậm thêm 200 ms.

### Chỗ sai: phép kiểm cú pháp PHP-FPM lặp ở từng site (commit `b94a707`)

Ở M2, `SiteChecks` có `PHPFPMConfig(phpVersion)` cho mỗi site, vì về đường đi request thì PHP-FPM là một chặng của site. Chỗ sai, theo chú thích mà commit thêm vào `internal/checks/site.go`: `php-fpm -t` kiểm cấu hình của cả máy, nên một lỗi cú pháp ở một file pool làm N site cùng đỏ, cùng một ID phép kiểm xuất hiện N lần, bản đồ vẽ một lỗi thành N sự cố, và health ở M4 (mở, đóng sự cố theo ID) sẽ va nhau. Sửa: bỏ khỏi phép kiểm của site, chỉ còn ở phép kiểm stack (`config.phpfpm:8.3` trong `stack_checks` ở 2.9). M3.log kiểm lại: `phép kiểm của site không còn config.phpfpm`. Commit không ghi lỗi được phát hiện bằng cách nào; nó nằm cùng đợt với các màn giao diện (cùng giờ commit với màn Bảng điều khiển). Còn sót: tham số `phpVersion` của `SiteChecks` không dùng nữa (`_ = phpVersion`), giữ cho M12.

### Chỗ sai: giao diện đá người dùng ra khi gõ sai mật khẩu hiện tại (commit `c70d8d6`)

Bản đầu của `request` trong `client.ts` coi mọi `401` là "phiên hết hạn" và chuyển sang trang đăng nhập. Nhưng `POST /api/v1/account/password` với mật khẩu hiện tại sai cũng trả `401`, mã `wrong_password`. Hậu quả nếu để nguyên: gõ nhầm một ký tự ở màn đổi mật khẩu là bị đá ra, mất cả form. Lỗi được sửa trước khi màn Cài đặt vào repo (commit `7de6977` sau đó), nên chưa ai gặp nó trên giao diện; commit không ghi phát hiện bằng cách nào. Sửa: bỏ qua `401` có mã `wrong_password`; cùng commit, `ApiError` giữ thêm `retryAfter` để màn đăng nhập nói được còn khoá bao lâu.

## 6. Tự kiểm

1. Mở panel bằng `http://<IP>:8443` (không có `s`) thì sao? Còn chạy `tpan serve --listen 0.0.0.0:8080` không có `--tls-cert` thì sao?
2. Vì sao lần gõ đúng mật khẩu trong lúc IP bị khoá trả lời trong 0,003 giây, còn lần gõ sai bình thường mất 0,07 giây? Nếu đổi thứ tự trong `handleLogin` (so mật khẩu trước, hỏi khoá sau) thì mất gì?
3. Kẻ xấu lấy được file `tpan.db`. Họ dùng được gì để vào panel, và vì sao?
4. Một trang lạ có nút "Nhận quà" gửi `POST https://<panel>:8443/api/v1/sites` từ trình duyệt của bạn trong lúc bạn đang đăng nhập. Kể các lớp chặn nó.
5. Vì sao `jobStart` chạy job bằng `context.Background()` mà không dùng `r.Context()`? Bạn đóng tab giữa lúc tạo site thì job ra sao?
6. Bạn mở trang job, rớt Wi-Fi 5 giây rồi có lại. Log có mất hay lặp dòng nào không? Job xong rồi mà trình duyệt vẫn cố nối lại thì server làm gì?
7. Vì sao mật khẩu WordPress chỉ có ở lần `GET /api/v1/jobs/{id}` đầu tiên? Vì sao `handleJob` phải chờ `run.done`?
8. Xoá dòng `<script src="/theme.js"></script>` khỏi `web/index.html` thì người dùng thấy gì? Chép nội dung `theme.js` vào thẳng `index.html` trong thẻ `<script>` thì sao?
9. (Cần máy ảo) Chứng minh `tpan.service` tự sống lại khi tiến trình chết, và nói dòng nào trong file unit làm việc đó.
10. (Cần máy ảo) Chứng minh fingerprint mà `tpan setup` in ra đúng là của chứng chỉ panel đang phục vụ, bằng lệnh không đi qua tpan.

## Đáp án

1. `http://` tới cổng 8443: server đang nói TLS, nên request HTTP thường không được hiểu; Go trả một câu báo lỗi kiểu "client gửi HTTP tới server HTTPS" chứ không trả trang (chưa chạy, tự thử bằng `curl -i http://<IP>:8443/`). Lệnh thứ hai: `serveCmd` từ chối ngay, lỗi `không có --tls-cert/--tls-key thì chỉ được nghe 127.0.0.1 hoặc localhost (đang là "0.0.0.0:8080")`, vì không TLS thì mật khẩu đi trên mạng dạng chữ thường. Test: `TestServeTuChoiHTTPNgoaiLoopback`.
2. `handleLogin` gọi `authLocked` trước `authCheckPassword`; IP đang khoá thì trả `423` mà không chạy argon2id (64 MiB, 3 lượt), nên chỉ tốn một câu SQL. Đổi thứ tự thì kẻ dò mật khẩu bị khoá rồi vẫn bắt máy chạy argon2id (64 MiB RAM mỗi lần) cho mỗi request gửi tới: khoá vẫn chặn được việc đăng nhập, nhưng không còn chặn được việc làm máy bận.
3. Không dùng được gì trực tiếp. Mật khẩu quản trị chỉ có dạng hash argon2id (phải dò từng mật khẩu, mỗi lần tốn thời gian và RAM; mật khẩu sinh bởi `rand.Text()` có ít nhất 128 bit ngẫu nhiên nên dò không nổi). Bảng `sessions` chỉ có sha256 của token; cookie cần token gốc, mà sha256 không đảo ngược được, và token 32 byte ngẫu nhiên thì không đoán được. Mật khẩu WordPress không bao giờ vào DB (kịch bản tìm 6 mật khẩu trong `tpan.db` và WAL: 0 dòng, 2.12). Tất nhiên, ai đọc được `tpan.db` thì thường đã có root, và root thì `tpan admin passwd` được.
4. Lớp một: `secure` gọi `CrossOriginProtection.Check`; trình duyệt gửi `Sec-Fetch-Site: cross-site` (hoặc `Origin` của trang lạ), nên trả `403 csrf` trước khi tới handler (2.6). Lớp hai: cookie có `SameSite=Strict`, nên trình duyệt không gắn cookie vào request bắt nguồn từ trang khác; kể cả lọt qua lớp một thì `requireSession` cũng trả `401`. Lớp ba: kể cả cả hai lớp trên hỏng, `POST /api/v1/sites` không có `?dry_run=1` chỉ tạo site với domain kẻ xấu chọn; không có endpoint nào nhận lệnh tuỳ ý.
5. `r.Context()` bị huỷ ngay khi handler trả `202`, và khi người dùng đóng tab. Job dùng context đó sẽ dừng giữa chừng, rồi hoàn tác cũng bằng context đã huỷ nên lỗi nốt: máy nằm ở trạng thái nửa vời. `context.Background()` không bao giờ bị huỷ, nên job chạy tới cùng dù không ai xem. Đóng tab chỉ làm luồng SSE của tab đó đóng (`sseFromFeed` thấy `ctx.Done()` và thoát); job vẫn chạy, xong thì vào Lịch sử, nhưng mật khẩu WordPress vẫn chờ trong bộ nhớ cho tới lần `GET` đầu tiên (hoặc tới khi tpan khởi động lại).
6. Không mất, không lặp. `EventSource` tự nối lại và gửi `Last-Event-ID` bằng `id` của sự kiện cuối nó nhận; `sseAfter` đọc header đó và chỉ gửi sự kiện có `Seq` lớn hơn. `useJob` còn bỏ thêm mọi sự kiện có `seq` đã thấy, phòng trường hợp nối lại phát trùng. Job đã xong và không còn gì sau chỗ đó thì `handleJobEvents` trả `204`, `EventSource` thôi nối lại. Mới kiểm bằng unit test (`TestSSEFinishedJobReplay`, 2.14), chưa rút mạng thật trên máy ảo.
7. `takeSecret` lấy mật khẩu ra khỏi `a.secrets` rồi `delete`: đúng một lần, để mật khẩu không nằm mãi trong bộ nhớ và không ai mở lại trang job sau đó mà đọc được. Chờ `run.done` vì giao diện `GET` lại ngay khi nhận `job.done`, mà lúc đó goroutine của job có thể chưa cất mật khẩu vào map; không chờ thì lần GET đó không có mật khẩu, và giao diện không có lý do gì để GET lần nữa.
8. Bỏ dòng đó: trang vẫn chạy, nhưng `data-theme` chỉ được đặt khi `theme.ts` nạp xong, tức sau khi trang đã vẽ một lần bằng màu sáng (biến ở `:root`); người dùng chế độ tối thấy chớp trắng mỗi lần mở trang. Viết thẳng trong `<script>`: CSP `default-src 'self'` chặn script nội tuyến, trình duyệt bỏ qua nó và báo lỗi ở console, kết quả giống hệt bỏ dòng.
9. `scripts/vm.sh exec systemctl show -p MainPID tpan` ghi lại PID, `scripts/vm.sh root kill -9 <PID>`, đợi khoảng 3 giây, rồi `scripts/vm.sh exec systemctl show -p MainPID tpan` thấy PID mới và `systemctl is-active tpan` là `active`. Dòng làm việc đó là `Restart=on-failure` (chết bất thường thì chạy lại) cùng `RestartSec=2s`. `kill -9` là chết bất thường; `systemctl stop` thì không tự chạy lại. Chưa chạy trên máy ảo.
10. Trong máy ảo: `scripts/vm.sh root openssl x509 -in /etc/tpan/tls/cert.pem -noout -fingerprint -sha256` đọc file. Từ Mac, đọc chứng chỉ mà cổng 8443 đang thật sự gửi: `echo | openssl s_client -connect <IP máy ảo>:8443 2>/dev/null | openssl x509 -noout -fingerprint -sha256`. Cả hai phải bằng dòng setup in ra. M3.log đã làm đúng ba phép so này, cùng ra `A5:DC:89:...:83:A7` (2.1). Thêm một đối chứng: `curl --pinnedpubkey` với sha256 sai của khoá công khai thì curl từ chối, mã 90.
