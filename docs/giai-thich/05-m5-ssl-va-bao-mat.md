# M5: SSL (staging), trang bảo mật, ufw, fail2ban

Đọc kèm: `01-m1-cai-stack.md` (engine, Step, `File`, Check rồi Apply rồi Check), `02-m2-site-wordpress.md` (site, vhost, `site rm`), `docs/KE-HOACH.md` mục 3 (dòng "Chứng chỉ"), 6.3 (SSL), 6.4 (xoá hẳn), 6.7 (chặng DNS và TLS), 6.8 (bảo mật máy), 11, và `docs/thay-doi/M5.md` (chỗ khác đặc tả, kết quả kiểm, việc trên VPS).

## 1. Mốc này làm được gì

- `sudo tpan ssl issue shop.example.com` xin chứng chỉ Let's Encrypt cho một site đã có, ghi lại vhost để site chạy ở `https://`, cổng 80 chuyển 301 sang https, và đặt URL của WordPress thành `https://...`. Mặc định dùng máy chủ staging của Let's Encrypt (chứng chỉ thử, không tốn hạn mức).
- Lỗi biết trước bị chặn trước khi gọi Let's Encrypt: chưa có email, tên miền chưa có bản ghi A, hoặc đường `http://<domain>/.well-known/acme-challenge/` không về đúng máy này.
- Chứng chỉ tự gia hạn (`certbot.timer`), và sau mỗi lần gia hạn certbot gọi hook của tpan để reload web server. Kiểm chứng có chạy `certbot renew --dry-run`.
- `tpan security status` cho xem ufw, fail2ban, SSH, swap, gói bảo mật chờ cập nhật, cổng panel. `tpan security ufw` bật tường lửa sau khi đã mở đúng cổng SSH thật; `tpan security fail2ban` cài fail2ban và bật jail sshd.
- Chạy lại mọi lệnh trên: máy đúng rồi thì không đổi gì. `tpan site rm` của site có SSL xoá luôn chứng chỉ.

## 2. Chạy thử

Mọi output dưới đây lấy nguyên văn từ `docs/bang-chung/M5.log`: lần chạy `scripts/e2e-m5.sh all` ngày 19/11/2026, 18:43 tới 18:47, dòng cuối là `TỔNG KẾT: 0 khẳng định không đạt`. Máy ảo của mốc này tên `tpan-m5` (ghi trong file `.tpan-vm`), Multipass, Ubuntu 24.04, arm64. Lượt Nginx chạy trước, lượt Apache sau, mỗi lượt từ máy sạch.

Những điều cần biết khi đọc log:

- Mọi lệnh ở đây gõ trên Mac, trong thư mục repo. `scripts/vm.sh exec X` chạy X trong máy ảo; `scripts/vm.sh root X` chạy X trong máy ảo bằng `sudo`. Riêng các lệnh `curl https://<vm>:8443/...` và `curl http://<vm>:9090/` chạy thẳng trên Mac, để thử đường từ ngoài vào máy ảo.
- Kịch bản in lệnh bằng `$*` nên mất dấu nháy, và các lệnh `bash -c ... _ a b` khó đọc. Lệnh dưới đây mình viết lại có nháy đúng (và thay `$1`, `$2` bằng giá trị thật) để bạn dán vào zsh được; output giữ nguyên.
- Kịch bản che IP máy ảo (`<vm>`, `<vm-v6>`), mạng Multipass (`<mạng máy ảo>`), IP của Mac (`<mac>`), thư mục tạm (`<tmp>`). Đó là chữ của script, không phải của tpan.
- Máy ảo không có tên miền thật, nên không xin được chứng chỉ Let's Encrypt thật. Kịch bản chạy `pebble` (máy chủ ACME thử do chính Let's Encrypt viết) ngay trong máy ảo, và trỏ tên thử về `127.0.0.1` bằng `/etc/hosts`. certbot, nginx, apache và thử thách HTTP-01 là thật; chỉ máy chủ cấp chứng chỉ là đồ thử. Mục 2.1 nói rõ phần đồ nghề này.

### 2.1 Chuẩn bị, và đồ nghề pebble (chỉ có trong máy ảo)

Chạy trên Mac:

```bash
make vm-reset
make vm-deploy
scripts/vm.sh root tpan stack install --web nginx --yes
scripts/vm.sh root tpan site add s1.example.com --email admin@example.com --yes
scripts/vm.sh root tpan site add s2.example.test --email admin@example.com --yes
```

Output của cài stack và tạo site giống M1, M2 (M5.log dòng 90 tới 400). `s1.example.com` là site sẽ có SSL; kịch bản thêm dòng `127.0.0.1 s1.example.com` vào `/etc/hosts` của máy ảo. `s2.example.test` giữ http; đuôi `.test` là đuôi dành riêng cho thử nghiệm, không bao giờ có bản ghi DNS thật.

Kế đó kịch bản dựng pebble (M5.log dòng 172 tới 254): cài gói `pebble` của Ubuntu, tự tạo một CA thử và chứng chỉ cho `localhost` bằng `openssl` (gói của Ubuntu không kèm chứng chỉ cho API của pebble), đưa CA đó vào kho tin cậy của hệ thống bằng `update-ca-certificates`, rồi chạy pebble nghe `127.0.0.1:14000`. Hai lệnh kiểm:

```bash
scripts/vm.sh exec curl -sS https://localhost:14000/dir
scripts/vm.sh exec python3 -c 'import certifi, requests; print("certifi.where():", certifi.where()); print("requests", requests.__version__)'
```

```
{
   "keyChange": "https://localhost:14000/rollover-account-key",
   "meta": {
      "externalAccountRequired": false,
      "termsOfService": "data:text/plain,Do%20what%20thou%20wilt"
   },
   "newAccount": "https://localhost:14000/sign-me-up",
   "newNonce": "https://localhost:14000/nonce-plz",
   "newOrder": "https://localhost:14000/order-plz",
   "revokeCert": "https://localhost:14000/revoke-cert"
}
```

```
certifi.where(): /etc/ssl/certs/ca-certificates.crt
requests 2.31.0
```

Lệnh đầu không có `-k` mà vẫn chạy: CA thử đã nằm trong kho hệ thống. Lệnh hai cho thấy vì sao certbot (viết bằng Python) cũng tin pebble: thư viện `certifi` của Ubuntu trỏ về đúng kho đó. Nhờ vậy tpan không phải truyền biến môi trường nào cho certbot. Trên VPS thật không có phần nào trong số này.

### 2.2 Cấu hình SSL: email, staging, máy chủ ACME

Chạy trên Mac:

```bash
scripts/vm.sh root tpan ssl settings
```

```
Email:     (chưa có; đặt bằng: sudo tpan ssl settings --email <email>)
Staging:   bật (chứng chỉ thử, trình duyệt không tin)
ACME:      https://acme-staging-v02.api.letsencrypt.org/directory
```

Chưa ai đặt gì mà staging đã bật: đó là mặc định (mục 3.2). Xin chứng chỉ khi chưa có email thì bị từ chối:

```bash
scripts/vm.sh root tpan ssl issue s1.example.com --yes
```

```
Từ chối: chưa có email cho Let's Encrypt (dùng để báo chứng chỉ sắp hết hạn); đặt bằng: sudo tpan ssl settings --email <email>
```

Mã thoát 1, và số job trong `tpan.db` vẫn là 3 (không có job mới): lời từ chối xảy ra trước khi dựng Plan. Bản `--json` có `"code": "acme_email_missing"`.

Đặt email và trỏ tpan sang pebble bằng cờ ẩn `--server` (cờ này chỉ để test, không hiện trong `--help`):

```bash
scripts/vm.sh root tpan ssl settings --email admin@example.com --server https://localhost:14000/dir
scripts/vm.sh root tpan ssl settings --server http://localhost:14000/dir
```

```
Email:     admin@example.com
Staging:   bật (chứng chỉ thử, trình duyệt không tin)
ACME:      https://localhost:14000/dir
```

```
Từ chối: địa chỉ máy chủ ACME "http://localhost:14000/dir" không hợp lệ: chỉ nhận https://<máy>[:cổng][/đường-dẫn], không khoảng trắng, không dấu ?
```

Khi `ssl.server` có giá trị thì nó thắng staging: dòng `ACME:` là pebble dù `Staging:` vẫn ghi "bật". `tpan ssl settings --json` sau lần từ chối vẫn là `"server": "https://localhost:14000/dir"`: lần ghi sai không để lại gì.

### 2.3 Tên miền không có bản ghi A: từ chối trước khi gọi ACME

Chạy trên Mac:

```bash
scripts/vm.sh root tpan ssl issue s2.example.test --yes
```

```
Từ chối: tên miền s2.example.test chưa sẵn sàng để xin chứng chỉ
  [lỗi] Bản ghi DNS của tên miền trỏ về máy này: s2.example.test chưa có bản ghi A
      Nguyên nhân: Tên miền chưa trỏ về đâu cả, hoặc bản ghi chưa lan tới máy này. Vào nơi quản lý DNS của tên miền, tạo bản ghi A trỏ về địa chỉ IP của máy, rồi chờ vài phút.
      Sửa tay:
        ip -o addr show scope global
        getent ahostsv4 s2.example.test
```

Bản `--json` có `"code": "precondition_failed"` kèm bằng chứng: `getent ahostsv4 s2.example.test` thoát mã 2 (không có bản ghi). Kịch bản kiểm thêm: vẫn 3 job, không có thư mục `/etc/letsencrypt/renewal` hay `live`, và log của pebble không nhắc tới `s2.example.test` lần nào. Tức là certbot chưa từng được gọi.

### 2.4 Tên miền trỏ về máy khác: bước thử HTTP hỏng, hoàn tác

Kịch bản trỏ `s2.example.test` về `192.0.2.1` (một địa chỉ dành cho tài liệu, không có máy nào) bằng `/etc/hosts`, rồi chạy trên Mac:

```bash
scripts/vm.sh root tpan ssl issue s2.example.test --yes --verbose
```

```
[cảnh báo] Bản ghi DNS của tên miền trỏ về máy này: Bản ghi A của s2.example.test là 192.0.2.1, không địa chỉ nào nằm trên máy này
    Nguyên nhân: Tên miền trỏ về máy khác, hoặc máy này đứng sau NAT (IP public không hiện trên card mạng, thường gặp ở VPS đám mây). Nếu là NAT thì vẫn xin chứng chỉ được; bước thử HTTP sẽ cho câu trả lời thật.
    Sửa tay:
      ip -o addr show scope global
      getent ahostsv4 s2.example.test
Bắt đầu job 45054e38577a4ee0
- Tạo thư mục /var/lib/tpan/acme/.well-known/acme-challenge ...
  xong
- Thử đường HTTP mà Let's Encrypt sẽ kiểm cho s2.example.test ...
    $ curl -sS -m 10 http://s2.example.test/.well-known/acme-challenge/nwwlxodwfoa7gkwdr7p7dpjbr4
    | curl: (28) Connection timed out after 10062 milliseconds
    $ getent ahosts s2.example.test
    | 192.0.2.1       STREAM s2.example.test
    | 192.0.2.1       DGRAM  
    | 192.0.2.1       RAW    
  LỖI: bước ssl.probe:s2.example.test: thực hiện: thử http://s2.example.test/.well-known/acme-challenge/ không tới được máy này (Let's Encrypt cũng sẽ thất bại): curl -sS -m 10 http://s2.example.test/.well-known/acme-challenge/nwwlxodwfoa7gkwdr7p7dpjbr4: mã thoát 28: curl: (28) Connection timed out after 10062 milliseconds; s2.example.test đang trỏ về 192.0.2.1 theo DNS của máy; lệnh đã chạy: curl -sS -m 10 http://s2.example.test/.well-known/acme-challenge/nwwlxodwfoa7gkwdr7p7dpjbr4
- Hoàn tác: Tạo thư mục /var/lib/tpan/acme/.well-known/acme-challenge ...
  đã hoàn tác

Job 45054e38577a4ee0: hỏng, đã hoàn tác sạch
```

Ba điều đáng chú ý:

- DNS lần này chỉ cảnh báo, không chặn: có bản ghi A, chỉ là không trỏ về địa chỉ nào của máy. Máy sau NAT cũng trông y như vậy, nên tpan để bước thử HTTP quyết.
- Bước thử HTTP hỏng, nên không có lệnh `certbot` nào chạy. Bảng `job_events` của job này chỉ có `curl` và `getent` (M5.log dòng 617 tới 622). Không tốn lượt nào của Let's Encrypt.
- Lỗi nói rõ domain đang trỏ về đâu theo DNS của máy (`getent ahosts`). Thư mục vừa tạo được hoàn tác. `sha256sum` của vhost `s2.example.test` trước và sau giống nhau, và `curl` vào `http://s2.example.test/` vẫn trả `200`.

### 2.5 Xin chứng chỉ cho s1.example.com

Chạy trên Mac:

```bash
scripts/vm.sh root tpan ssl issue s1.example.com --yes --verbose
```

Bớt vài đoạn:

```
[cảnh báo] Bản ghi DNS của tên miền trỏ về máy này: Bản ghi A của s1.example.com là 127.0.0.1, không địa chỉ nào nằm trên máy này
... (bớt dòng)
Bắt đầu job 53d81363b761db0a
- Tạo thư mục /var/lib/tpan/acme/.well-known/acme-challenge ...
  xong
- Thử đường HTTP mà Let's Encrypt sẽ kiểm cho s1.example.com ...
    $ curl -sS -m 10 http://s1.example.com/.well-known/acme-challenge/monrpgysovddxs5dxnyghrvcq2
    | O4XWL2TCFXBGTHDFZFFRIJIUWZ
  xong
- Xin chứng chỉ Let's Encrypt cho s1.example.com ...
    $ certbot certonly --webroot -w /var/lib/tpan/acme -d s1.example.com --cert-name s1.example.com --non-interactive --agree-tos -m admin@example.com --server https://localhost:14000/dir
    | Saving debug log to /var/log/letsencrypt/letsencrypt.log
    | Account registered.
    | Requesting a certificate for s1.example.com
    | 
    | Successfully received certificate.
    | Certificate is saved at: /etc/letsencrypt/live/s1.example.com/fullchain.pem
    | Key is saved at:         /etc/letsencrypt/live/s1.example.com/privkey.pem
    | This certificate expires on 2031-11-19.
... (bớt dòng)
  xong
- Ghi site mặc định cổng 443 của Nginx ...
    $ nginx -t
    | nginx: the configuration file /etc/nginx/nginx.conf syntax is ok
    | nginx: configuration file /etc/nginx/nginx.conf test is successful
    $ systemctl reload nginx
  xong
- Bật /etc/nginx/sites-enabled/tpan-default-ssl.conf → /etc/nginx/sites-available/tpan-default-ssl.conf ...
... (bớt dòng)
  xong
- Ghi vhost nginx có HTTPS cho s1.example.com ...
... (bớt dòng)
  xong
- Bật /etc/nginx/sites-enabled/tpan-s1.example.com.conf → /etc/nginx/sites-available/tpan-s1.example.com.conf ...
  đã đúng, bỏ qua
- Đặt địa chỉ WordPress của s1.example.com là https://s1.example.com ...
    $ runuser -u tp_s1_example_com_090c1b -- wp option get home --skip-plugins --skip-themes --path=/var/www/s1.example.com/public
    | http://s1.example.com
    $ runuser -u tp_s1_example_com_090c1b -- wp option get home --skip-plugins --skip-themes --path=/var/www/s1.example.com/public
    | http://s1.example.com
    $ runuser -u tp_s1_example_com_090c1b -- wp option get siteurl --skip-plugins --skip-themes --path=/var/www/s1.example.com/public
    | http://s1.example.com
    $ runuser -u tp_s1_example_com_090c1b -- wp option update home https://s1.example.com --skip-plugins --skip-themes --quiet --path=/var/www/s1.example.com/public
    $ runuser -u tp_s1_example_com_090c1b -- wp option update siteurl https://s1.example.com --skip-plugins --skip-themes --quiet --path=/var/www/s1.example.com/public
    $ runuser -u tp_s1_example_com_090c1b -- wp option get home --skip-plugins --skip-themes --path=/var/www/s1.example.com/public
    | https://s1.example.com
    $ runuser -u tp_s1_example_com_090c1b -- wp option get siteurl --skip-plugins --skip-themes --path=/var/www/s1.example.com/public
    | https://s1.example.com
  xong
- Ghi hook gia hạn chứng chỉ ...
  xong
... (bớt dòng)
Job 53d81363b761db0a: thành công
Kiểm chứng:
  [đạt] Chứng chỉ SSL có, khớp tên miền và còn hạn: Chứng chỉ khớp s1.example.com, còn 1825 ngày, do Pebble Intermediate CA 7d0ce0 cấp
  [đạt] Vhost của site có, do tpan quản lý và đang được bật: Vhost của site có, đúng dòng đầu của tpan, và đã được bật
  [đạt] Socket của pool PHP có, đúng chủ và đúng quyền: Socket của pool có, chủ www-data:www-data, quyền 0660
  [đạt] Mã nguồn WordPress nguyên vẹn (khớp checksum): Mã nguồn WordPress khớp checksum
  [đạt] wp-config.php có quyền 0600 và đúng chủ: wp-config.php quyền 0600, chủ tp_s1_example_com_090c1b
  [đạt] User DB của site đăng nhập được, chỉ thấy DB của site: User DB đăng nhập được và chỉ thấy DB tp_s1_example_com_090c1b
  [đạt] Cổng 80 chuyển hướng 301 sang https: Cổng 80 chuyển hướng 301 tới https://s1.example.com/
  [đạt] Gọi HTTPS vào site trả 200 và đúng là WordPress của site: Site trả 200 qua https, đúng là WordPress của s1.example.com
  [đạt] Dịch vụ certbot.timer đang chạy và tự bật cùng máy: certbot.timer đang chạy và tự bật khi khởi động máy
  [đạt] Gia hạn thử chứng chỉ (certbot renew --dry-run) thành công: certbot gia hạn thử s1.example.com thành công
```

Cả lệnh mất 8 giây (18:45:12 tới 18:45:20). Những chỗ người review hay hỏi:

- DNS cảnh báo vì `127.0.0.1` là địa chỉ loopback, không phải địa chỉ `scope global` mà `ip -o addr show scope global` liệt kê. Đây là chuyện riêng của máy ảo; trên VPS, bản ghi A trỏ đúng thì dòng này là `[đạt]`.
- Bước thử in ra `O4XWL2TCFXBGTHDFZFFRIJIUWZ`: đó là nội dung ngẫu nhiên tpan vừa ghi vào file thử, gọi lại qua HTTP thấy đúng chuỗi đó. File thử bị xoá ngay sau đó.
- Bước bật symlink của vhost site "đã đúng, bỏ qua": symlink đã có từ lúc tạo site; chỉ nội dung file vhost đổi.
- `wp option get home` xuất hiện ba lần trước khi update: lần một là `Check` (thấy `home` sai thì dừng luôn, không đọc `siteurl`), lần hai và ba là `Apply` đọc lại cả hai giá trị cũ để nhớ cho hoàn tác (mục 3.8).
- "còn 1825 ngày" là vì pebble cấp chứng chỉ 5 năm. Chứng chỉ Let's Encrypt thật ngắn hơn nhiều; đừng lấy con số này để trình bày.

### 2.6 Kiểm độc lập (không qua tpan)

Chạy trên Mac. Pebble thật sự gọi vào cổng 80 của máy, và request đi đúng vào nginx của site:

```bash
scripts/vm.sh root bash -c 'grep acme-challenge /var/log/tpan/sites/s1.example.com/access.log | tail -4'
```

```
127.0.0.1 - - [19/Nov/2026:18:45:13 +0700] "GET /.well-known/acme-challenge/Fn96zZA_vAbAsb2CxkYjAlLoo8zcevANrc10Rs7o-x4 HTTP/1.1" 200 87 "-" "LetsEncrypt-Pebble-VA (linux; arm64)"
127.0.0.1 - - [19/Nov/2026:18:45:18 +0700] "GET /.well-known/acme-challenge/oz0krRpr0zWJvHAFZ2MpJejo0ADJzuJjui7ifFMB5aQ HTTP/1.1" 200 87 "-" "LetsEncrypt-Pebble-VA (linux; arm64)"
... (bớt dòng)
```

Request lúc 18:45:13 là của `certbot certonly`; lúc 18:45:18 là của `certbot renew --dry-run` trong bước kiểm chứng.

Chứng chỉ, chuyển hướng giữ nguyên đường dẫn và query, và đường thử thách không bị chuyển hướng:

```bash
scripts/vm.sh root openssl x509 -in /etc/letsencrypt/live/s1.example.com/cert.pem -noout -subject -issuer -dates -ext subjectAltName
scripts/vm.sh exec curl -sS -m 20 --resolve s1.example.com:80:127.0.0.1 -o /dev/null -D - 'http://s1.example.com/duong/dan?a=1&b=2'
```

```
subject=CN = s1.example.com
issuer=CN = Pebble Intermediate CA 7d0ce0
notBefore=Nov 19 11:45:14 2026 GMT
notAfter=Nov 19 11:45:13 2031 GMT
X509v3 Subject Alternative Name: 
    DNS:s1.example.com
```

```
HTTP/1.1 301 Moved Permanently
Server: nginx/1.24.0 (Ubuntu)
... (bớt dòng)
Location: https://s1.example.com/duong/dan?a=1&b=2
```

Kịch bản ghi một file `e2e-thu` vào `/var/lib/tpan/acme/.well-known/acme-challenge/`, gọi `http://s1.example.com/.well-known/acme-challenge/e2e-thu`: nhận `HTTP/1.1 200 OK` với đúng nội dung `noi-dung-thu-e2e`; gọi một tên không có file thì `file không có: 404`. Tức là cổng 80 vẫn phục vụ thử thách (cho lần gia hạn sau), không chuyển nó sang https.

HTTPS trả đúng WordPress, link trong trang đã là https. Kịch bản gọi `curl -sS -k --resolve s1.example.com:443:127.0.0.1 https://s1.example.com/` rồi tự đếm trong thân trang; ba dòng dưới là phần đếm của nó:

```
HTTP 200
<title>s1.example.com</title>
wp-content: 4 dòng; nhắc https://s1.example.com: 29 dòng; nhắc http://s1.example.com: 0 dòng
```

Cổng 443 với một tên không có vhost, với IP, và với `s2.example.test` (site chưa có SSL). Kịch bản chạy một vòng lặp `bash` qua ba địa chỉ; hai lệnh dưới là bản gọn để bạn tự gõ, còn output là của vòng lặp (dòng `==` và `curl mã` do kịch bản in):

```bash
scripts/vm.sh exec curl -sS -k -m 10 --resolve la.example.net:443:127.0.0.1 -o /dev/null -w '%{http_code}\n' https://la.example.net/
scripts/vm.sh exec curl -sS -k -m 10 -o /dev/null -w '%{http_code}\n' https://127.0.0.1/
```

```
== https://la.example.net/
curl: (35) OpenSSL/3.0.13: error:0A000458:SSL routines::tlsv1 unrecognized name
HTTP 000
curl mã 35
== https://127.0.0.1/
curl: (35) OpenSSL/3.0.13: error:0A000458:SSL routines::tlsv1 unrecognized name
HTTP 000
curl mã 35
== https://s2.example.test/
curl: (35) OpenSSL/3.0.13: error:0A000458:SSL routines::tlsv1 unrecognized name
HTTP 000
curl mã 35
```

Nginx từ chối ngay lúc bắt tay TLS (mã thoát 35 của curl), không đưa ra chứng chỉ nào, không lộ trang nào. Đó là việc của site mặc định 443 (mục 3.7).

Hook gia hạn có đúng quyền và nội dung, `certbot.timer` đang chạy:

```bash
scripts/vm.sh root bash -c 'stat -c "%a %U:%G %n" /etc/letsencrypt/renewal-hooks/deploy/tpan; cat /etc/letsencrypt/renewal-hooks/deploy/tpan'
scripts/vm.sh exec systemctl is-active certbot.timer
```

```
755 root:root /etc/letsencrypt/renewal-hooks/deploy/tpan
#!/bin/sh
# Do tpan quản lý. Sửa tay sẽ bị tpan health báo lệch.
# certbot chạy mọi file trong thư mục renewal-hooks/deploy sau mỗi lần gia hạn
# thành công. Web server chỉ đọc chứng chỉ lúc khởi động, nên không báo nó thì
# nó vẫn dùng chứng chỉ cũ cho tới khi hết hạn. tpan reload đúng web server đang dùng.
exec /usr/local/bin/tpan internal reload-web
```

```
active
```

### 2.7 Hook gia hạn chạy thật

`certbot renew --dry-run` cố ý không chạy hook, nên kịch bản ép một lần gia hạn thật với pebble rồi xem chứng chỉ đang phục vụ có đổi không. Chạy trên Mac:

```bash
scripts/vm.sh root bash -c 'echo | openssl s_client -connect 127.0.0.1:443 -servername s1.example.com 2>/dev/null | openssl x509 -noout -serial'
scripts/vm.sh root certbot renew --force-renewal --no-random-sleep-on-renew --cert-name s1.example.com --server https://localhost:14000/dir --non-interactive
scripts/vm.sh root bash -c 'grep -E "Running deploy-hook command|Dry run: skipping deploy hook" /var/log/letsencrypt/letsencrypt.log | tail -3'
scripts/vm.sh root bash -c 'echo | openssl s_client -connect 127.0.0.1:443 -servername s1.example.com 2>/dev/null | openssl x509 -noout -serial; openssl x509 -in /etc/letsencrypt/live/s1.example.com/cert.pem -noout -serial'
```

```
serial=397AF279660793F7
```

```
Congratulations, all renewals succeeded: 
  /etc/letsencrypt/live/s1.example.com/fullchain.pem (success)
```

```
2026-11-19 18:45:20,986:INFO:certbot._internal.hooks:Dry run: skipping deploy hook command: /etc/letsencrypt/renewal-hooks/deploy/tpan
2026-11-19 18:45:26,913:INFO:certbot.compat.misc:Running deploy-hook command: /etc/letsencrypt/renewal-hooks/deploy/tpan
```

```
serial=15B2B83C51550933
serial=15B2B83C51550933
```

Dòng 18:45:20 là bước kiểm chứng `--dry-run` của `tpan ssl issue`: certbot bỏ qua hook. Dòng 18:45:26 là lần gia hạn ép: certbot chạy hook. `journalctl -u nginx` có `Reloaded nginx.service` lúc 18:45:26, và serial chứng chỉ đang phục vụ ở cổng 443 đổi từ `397AF279660793F7` sang `15B2B83C51550933`, khớp file trong `live/`. Thiếu hook thì nginx vẫn đưa ra serial cũ.

### 2.8 Chạy lại: không đổi gì

Chạy trên Mac:

```bash
scripts/vm.sh root tpan ssl issue s1.example.com --yes
```

```
Bắt đầu job 5e932c7e7a0c0ffd
- Tạo thư mục /var/lib/tpan/acme/.well-known/acme-challenge ...
  đã đúng, bỏ qua
- Thử đường HTTP mà Let's Encrypt sẽ kiểm cho s1.example.com ...
  đã đúng, bỏ qua
- Xin chứng chỉ Let's Encrypt cho s1.example.com ...
  đã đúng, bỏ qua
- Ghi site mặc định cổng 443 của Nginx ...
  đã đúng, bỏ qua
- Bật /etc/nginx/sites-enabled/tpan-default-ssl.conf → /etc/nginx/sites-available/tpan-default-ssl.conf ...
  đã đúng, bỏ qua
- Ghi vhost nginx có HTTPS cho s1.example.com ...
  đã đúng, bỏ qua
- Bật /etc/nginx/sites-enabled/tpan-s1.example.com.conf → /etc/nginx/sites-available/tpan-s1.example.com.conf ...
  đã đúng, bỏ qua
- Đặt địa chỉ WordPress của s1.example.com là https://s1.example.com ...
  đã đúng, bỏ qua
- Ghi hook gia hạn chứng chỉ ...
  đã đúng, bỏ qua
... (bớt dòng)
Job 5e932c7e7a0c0ffd: thành công
```

Bước thử HTTP cũng được bỏ qua, vì nó hỏi bước xin chứng chỉ "chứng chỉ đã đúng chưa" (mục 3.5). Kịch bản so `sha256sum` của vhost, site mặc định 443, hook, file renewal, danh sách `archive/`, serial đang phục vụ và PID của nginx trước và sau: giống hệt. Các lần `certbot renew --dry-run` của tpan đều có cờ mới:

```bash
scripts/vm.sh root bash -c 'grep -hE "Arguments: \[.--dry-run|random delay" /var/log/letsencrypt/letsencrypt.log* | cut -c1-200'
```

```
2026-11-19 18:45:18,826:DEBUG:certbot._internal.main:Arguments: ['--dry-run', '--no-random-sleep-on-renew', '--cert-name', 's1.example.com', '--non-interactive', '--server', 'https://localhost:14000/d
2026-11-19 18:45:31,314:DEBUG:certbot._internal.main:Arguments: ['--dry-run', '--no-random-sleep-on-renew', '--cert-name', 's1.example.com', '--non-interactive', '--server', 'https://localhost:14000/d
```

Không có dòng `random delay` nào (mục 5, chỗ sai). Lần chạy lại mất 5 giây (18:45:28 tới 18:45:33).

Chạy lại `site add` cho site đã có SSL, đây là chỗ dễ hỏng nhất (vhost 443 bị ghi đè thành bản cổng 80). Chạy trên Mac:

```bash
scripts/vm.sh root tpan site add s1.example.com --email admin@example.com --yes
```

```
Bắt đầu job 18fd872cb9363309
... (bớt dòng)
- Ghi vhost nginx có HTTPS cho s1.example.com ...
  đã đúng, bỏ qua
... (bớt dòng)
Job 18fd872cb9363309: thành công
Kiểm chứng:
  [đạt] Chứng chỉ SSL có, khớp tên miền và còn hạn: Chứng chỉ khớp s1.example.com, còn 1825 ngày, do Pebble Intermediate CA 7d0ce0 cấp
... (bớt dòng)
  [đạt] Cổng 80 chuyển hướng 301 sang https: Cổng 80 chuyển hướng 301 tới https://s1.example.com/
  [đạt] Gọi HTTPS vào site trả 200 và đúng là WordPress của site: Site trả 200 qua https, đúng là WordPress của s1.example.com
... (bớt dòng)
```

Tên bước là "Ghi vhost nginx có HTTPS", không phải "Ghi vhost nginx", và bộ kiểm chứng là bộ của site có SSL. `grep` dòng `listen ... 443` trong vhost vẫn ra `2`.

### 2.9 Xoá hẳn site có SSL

Chạy trên Mac:

```bash
scripts/vm.sh root tpan site rm s1.example.com --yes
scripts/vm.sh root bash -c 'ls /etc/letsencrypt/renewal /etc/letsencrypt/live /etc/letsencrypt/archive 2>&1; certbot certificates 2>&1 | tail -4'
```

```
Bắt đầu job 667f7f1f716a745b
- Tắt vhost của s1.example.com ...
  xong
... (bớt dòng)
- Xoá user Linux tp_s1_example_com_090c1b ...
  xong
- Xoá chứng chỉ của s1.example.com ...
  xong

Job 667f7f1f716a745b: thành công
```

```
/etc/letsencrypt/archive:

/etc/letsencrypt/live:
README

/etc/letsencrypt/renewal:

- - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - -
No certificates found.
```

Lệnh trong job là `certbot delete --cert-name s1.example.com --non-interactive`, và nó là bước cuối cùng (mục 3.11 nói vì sao).

### 2.10 Apache khác gì

Lượt Apache chạy đúng các bước trên (M5.log dòng 1876 tới 3245). Hai chỗ khác. Thứ nhất, Plan SSL có thêm hai bước trước site mặc định:

```
- Cài gói: ssl-cert ...
    $ dpkg-query --show '--showformat=${Package}\t${Status}\n' ssl-cert
    | ssl-cert	install ok installed
  đã đúng, bỏ qua
- Module Apache: bật ssl; tắt  ...
    $ a2query -m ssl
    | No module matches ssl
... (bớt dòng)
    $ a2enmod -q ssl
    | Enabling module socache_shmcb.
    | Enabling module ssl.
    $ apachectl configtest
... (bớt dòng)
    $ systemctl restart apache2
    $ a2query -m ssl
    | ssl (enabled by site administrator)
  xong
```

Chữ "tắt " cụt ở cuối tiêu đề là do tiêu đề của bước `ApacheModules` (trong `internal/steps/apache.go`) luôn nối cả danh sách bật lẫn danh sách tắt, mà ở đây danh sách tắt rỗng. Đã sửa sau lần chạy này (hàm `apacheModulesTitle`); bản mới in "Module Apache: bật ssl". Bật module là `restart` chứ không `reload`: Apache phải khởi động lại để nạp module mới. Theo mã, lần xin SSL đầu tiên trên Apache khởi động lại web server; thời gian gián đoạn chưa đo.

Thứ hai, cổng 443 với tên lạ:

```
== https://la.example.net/
HTTP 403
curl mã 0
0
```

Apache không từ chối được lúc bắt tay như Nginx, nên nó đưa ra chứng chỉ tự ký "snakeoil" rồi trả 403. Kịch bản so fingerprint: chứng chỉ Apache đưa cho `la.example.net` trùng với `/etc/ssl/certs/ssl-cert-snakeoil.pem` (CN là tên máy, `tpan-m5`), không phải chứng chỉ của `s1.example.com`.

### 2.11 Trang bảo mật, fail2ban, ufw (chỉ lượt Nginx)

Trạng thái trên máy ảo sạch. Chạy trên Mac: `scripts/vm.sh root tpan security status --json`. Kịch bản rút gọn mỗi dòng thành id, trạng thái, tóm tắt:

```
{"id":"sec.ufw","status":"warn","summary":"ufw đang tắt"}
{"id":"sec.fail2ban","status":"warn","summary":"fail2ban chưa cài"}
{"id":"sec.ssh.root","status":"pass","summary":"PermitRootLogin là without-password: root không vào bằng mật khẩu được"}
{"id":"sec.ssh.password","status":"pass","summary":"PasswordAuthentication là no"}
{"id":"sec.swap","status":"warn","summary":"Máy không có swap"}
{"id":"sec.updates","status":"warn","summary":"45 gói bảo mật đang chờ cập nhật"}
{"id":"sec.panel","status":"warn","summary":"Panel đang mở ra ngoài (nghe ở *, cổng 8443)"}
```

fail2ban, chạy trên Mac:

```bash
scripts/vm.sh root tpan security fail2ban --yes
scripts/vm.sh root fail2ban-client status sshd
```

```
Bắt đầu job c91b7a2c1c980056
- Cài gói: fail2ban ...
  xong
- Bật jail sshd của fail2ban ...
  xong
- Chạy và bật fail2ban khi khởi động ...
  đã đúng, bỏ qua
  [đạt] fail2ban đang bảo vệ SSH

Job c91b7a2c1c980056: thành công
Kiểm chứng:
  [đạt] fail2ban đang bảo vệ SSH: jail sshd đang chạy, 0 IP đang bị chặn
```

```
Status for the jail: sshd
|- Filter
|  |- Currently failed:	0
|  |- Total failed:	0
|  `- Journal matches:	_SYSTEMD_UNIT=sshd.service + _COMM=sshd
`- Actions
   |- Currently banned:	0
   |- Total banned:	0
   `- Banned IP list:	
```

Bước "Chạy và bật fail2ban" đã đúng vì gói fail2ban tự chạy dịch vụ ngay khi cài. Chạy lại lệnh: cả ba bước "đã đúng, bỏ qua".

ufw. Trước hết đối chiếu cổng SSH thật rồi xem trước. Chạy trên Mac:

```bash
scripts/vm.sh root bash -c 'sshd -T 2>/dev/null | grep -i "^port "'
scripts/vm.sh root tpan security ufw --dry-run
```

```
port 22
```

```
Kế hoạch: Bật tường lửa ufw (mở 22/tcp, 80/tcp, 443/tcp, 8443/tcp)
2 bước sẽ chạy, 1 bước đã đúng sẽ bỏ qua, 1 bước có thể gián đoạn dịch vụ.

 1. [đã đúng, bỏ qua] Cài gói: ufw  (apt.install:ufw)
 2. [sẽ chạy] Mở cổng trên tường lửa: 22/tcp 80/tcp 443/tcp 8443/tcp  (ufw.allow:22/tcp,80/tcp,443/tcp,8443/tcp)
    Vì sao: Bật tường lửa là chặn mọi cổng chưa được mở. Thiếu luật cho cổng SSH thì mất đường vào máy; thiếu 80, 443 thì site không ai vào được; thiếu cổng panel thì không mở được tpan.
    $ ufw allow 22/tcp
    $ ufw allow 80/tcp
    $ ufw allow 443/tcp
    $ ufw allow 8443/tcp
 3. [sẽ chạy] Bật tường lửa ufw  (ufw.enable)
    NGUY HIỂM: bước này có thể làm gián đoạn dịch vụ.
    Vì sao: Tường lửa tắt thì mọi dịch vụ đang nghe trên máy (MariaDB, cổng thử của ứng dụng khác) đều mở ra internet. Bật lên thì chỉ các cổng đã mở ở bước trước còn vào được. tpan mở cổng SSH trước khi bật để không mất đường vào máy.
    $ ufw --force enable
```

Có 8443 vì lúc này panel đã được `tpan setup` bật và nghe ở `*:8443`. Để có đối chứng, kịch bản chạy một máy chủ thử ở cổng 9090 trong máy ảo; từ Mac, `curl http://<vm>:9090/` trả `200`. Rồi bật thật, chạy trên Mac:

```bash
scripts/vm.sh root tpan security ufw --yes
scripts/vm.sh root ufw status verbose
```

```
Bắt đầu job 63390a23837215b5
- Cài gói: ufw ...
  đã đúng, bỏ qua
- Mở cổng trên tường lửa: 22/tcp 80/tcp 443/tcp 8443/tcp ...
  xong
- Bật tường lửa ufw ...
  xong
  [đạt] Tường lửa ufw đang bật và cho cổng SSH

Job 63390a23837215b5: thành công
Kiểm chứng:
  [đạt] Tường lửa ufw đang bật và cho cổng SSH: ufw đang bật, cho vào: 22/tcp; 80/tcp; 443/tcp; 8443/tcp
```

```
Status: active
Logging: on (low)
Default: deny (incoming), allow (outgoing), disabled (routed)
New profiles: skip

To                         Action      From
--                         ------      ----
22/tcp                     ALLOW IN    Anywhere                  
80/tcp                     ALLOW IN    Anywhere                  
443/tcp                    ALLOW IN    Anywhere                  
8443/tcp                   ALLOW IN    Anywhere                  
22/tcp (v6)                ALLOW IN    Anywhere (v6)             
80/tcp (v6)                ALLOW IN    Anywhere (v6)             
443/tcp (v6)               ALLOW IN    Anywhere (v6)             
8443/tcp (v6)              ALLOW IN    Anywhere (v6)             
```

Sau khi bật: `scripts/vm.sh exec echo ...` (đi qua SSH) vẫn vào được máy ảo; từ Mac, cổng 9090 hết giờ (`curl: (28) Connection timed out after 5004 milliseconds`), còn `https://<vm>:8443/api/v1/health` và cổng 80 của `s2.example.test` vẫn trả `200`. Chạy lại `tpan security ufw --yes`: cả ba bước "đã đúng, bỏ qua".

API trả cùng dữ liệu cho giao diện: `GET /api/v1/ssl` trả cấu hình và danh sách site kèm phép kiểm chứng chỉ (M5.log dòng 1283 tới 1320), `GET /api/v1/security` gọi từ Mac qua ufw trả 7 dòng của trang bảo mật, lúc này `sec.ufw` và `sec.fail2ban` đã là `pass` (dòng 1823 tới 1875).

### 2.12 Chưa chạy

- `tpan ssl issue <domain> --dry-run` (bản xem trước của Plan SSL): chưa chạy trên máy ảo, chỉ có unit test `TestSSLIssueDryRun` (`cmd/tpan/ssl_test.go`). Thử: `scripts/vm.sh root tpan ssl issue s1.example.com --dry-run`.
- `tpan site add <domain> --email E --ssl` (tạo site rồi xin SSL luôn): chưa chạy trên máy ảo, chỉ có unit test `TestSiteAddSSLFlag` và `TestSiteAddSSLUsesSiteEmail`.
- Đổi từ staging sang thật (`tpan ssl settings --staging=false` rồi `tpan ssl issue` cho site đã có chứng chỉ staging, nhánh `--force-renewal` của `CertbotCert.Apply`): chưa chạy, chỉ có unit test `TestCertbotCertArgs`. Mục 3.6 mô tả theo mã.
- Let's Encrypt staging thật với một tên miền thật, và `certbot renew --dry-run` với Let's Encrypt: chưa chạy, chủ dự án chạy trên VPS theo mục "Việc chủ dự án tự làm trên VPS" của `docs/thay-doi/M5.md`. Đây là tiêu chí đóng M5 ở mục 13, nên M5 chưa đóng được tới khi có output đó.
- Một lần đăng nhập SSH sai để thấy bộ đếm `Currently failed` của fail2ban tăng: chưa chạy (đăng nhập sai từ trong máy bị bỏ qua vì `127.0.0.1` nằm trong `ignoreip`).
- ufw và fail2ban trên máy Apache: chưa chạy; phần bảo mật chỉ chạy ở lượt Nginx. Mã của chúng không phụ thuộc web server.

## 3. Đi qua mã theo thứ tự chạy

Hai luồng: `tpan ssl issue` (3.1 tới 3.11), và `tpan security ufw|fail2ban|status` (3.12 tới 3.14).

### 3.1 `cmd/tpan/ssl.go`: `sslIssueCmd`, `runSSLIssue`

`sslIssueCmd` kiểm domain (`parseDomainArg`), kiểm email nếu có `--email`, đòi root khi không phải `--dry-run`, mở DB (`openSSL`), rồi:

```go
if !em.IsZero() {
	if f.dryRun {
		s.Settings = emailOverlay{Settings: s.Settings, email: em.String()}
	} else if _, err := setSSL(ctx, e, s, core.SSLSettingsInput{Email: &em}); err != nil {
		return fail(e, f.asJSON, err)
	}
}
rep, ran, err := runSSLIssue(ctx, e, &f, s, d)
if !ran {
	return fail(e, f.asJSON, err)
}
return finishReport(e, f.asJSON, rep, err)
```

`--dry-run` không được ghi gì, kể cả email. Nhưng bản xem trước phải đúng với email người dùng vừa gõ. `emailOverlay` giải hai yêu cầu đó: nó bọc bảng settings thật, trả email của cờ khi được hỏi `ssl.email`, và từ chối mọi lần ghi. Đây là cách Go "ghi đè một phương thức" của một interface: struct nhúng `core.Settings` nên có sẵn mọi phương thức, rồi tự định nghĩa lại `GetSetting` và `SetSetting`.

`runSSLIssue` là phần chung với `site add --ssl` (hàm `siteAddSSL`): gọi `PrepareIssue`, in cảnh báo điều kiện đầu (`printPreconditionWarnings`), xem trước hoặc hỏi lại (`previewOrConfirm`), chạy `Execute`, ghi nhật ký `audit` với hành động `site.ssl`.

### 3.2 `internal/core/ssl.go`: `Config` và `ServerURL`

Cấu hình SSL nằm trong bảng `settings` với ba khoá: `ssl.email`, `ssl.staging`, `ssl.server`. Mục 6.3 viết `settings.acme_staging`; tên thật là `ssl.staging` (ghi ở `docs/thay-doi/M5.md`).

```go
// Chưa ghi thì BẬT: lúc phát triển luôn dùng staging (mục 6.3), nên muốn
// xin chứng chỉ thật phải tắt có chủ ý.
switch {
case !ok || staging == "1":
	cfg.Staging = true
case staging == "0":
default:
	return SSLConfig{}, fmt.Errorf("setting %s có giá trị lạ %q (chỉ nhận 1 hoặc 0)", SettingSSLStaging, staging)
}
```

`ServerURL` chọn địa chỉ đưa cho certbot: `ssl.server` nếu có, không thì `steps.LetsEncryptStaging` hoặc `steps.LetsEncryptProd`. Mục 6.3 viết "thêm `--staging`"; mã luôn truyền `--server <địa chỉ>` thay vì `--staging`. Hệ quả nhìn thấy được: một cờ đó chọn được cả ba trường hợp (thật, staging, pebble), và file renewal của certbot ghi lại đúng địa chỉ đó ở dòng `server = ...`, là dòng `CertbotCert.Check` so khi chạy lại (mục 3.6). Chỗ khác đặc tả này ghi trong `docs/thay-doi/M5.md` để gộp vào KE-HOACH.md khi ghép.

`SetConfig` kiểm hết các trường rồi mới ghi, để một trường sai không để lại cấu hình ghi dở. `ssl.server` phải khớp `sslServerRe` (chỉ `https://`), nên lần thử `--server http://...` ở mục 2.2 bị từ chối.

### 3.3 `PrepareIssue`: điều kiện đầu, chỉ đọc

Theo thứ tự: site có trong bảng `sites` và đang `active` (dùng lại `sites.find` để lời từ chối giống `tpan site`), stack đã cài, có email (không thì `acme_email_missing`), `ssl.server` hợp lệ, rồi DNS:

```go
l := st.Layout()
// DNS không có bản ghi A thì Let's Encrypt chắc chắn hỏng: dừng ở đây, không
// tốn lượt. Warn (máy sau NAT, AAAA lạ) không chặn: bước thử HTTP quyết.
results := checks.RunAll(ctx, s.Sys, []checks.Check{checks.DNSPointsHere(l)})
var failed []checks.Result
for _, r := range results {
	if r.Status == checks.Fail {
		failed = append(failed, r)
	}
}
if len(failed) > 0 {
	e := refuse("precondition_failed", i18n.T(
```

Chỉ `Fail` chặn. `checks.DNSPointsHere` (trong `internal/checks/ssl.go`) chạy `getent ahostsv4`, `getent ahostsv6` và `ip -o addr show scope global`: không có bản ghi A là `Fail`; có A nhưng không địa chỉ nào nằm trên card mạng là `Warn`; có AAAA trỏ đi chỗ khác cũng `Warn`, vì Let's Encrypt ưu tiên IPv6 khi có AAAA. Đó là ba kết quả bạn thấy ở mục 2.3, 2.4, 2.5.

Kết quả của mọi điều kiện đầu (kể cả `Warn`) được giữ trong `SSLIssue.Preconditions` để CLI in ra trước khi chạy.

### 3.4 `sslPlan`: thứ tự bước

```go
// Bước thử và bước xin dùng CHUNG một CertbotCert: bước thử hỏi nó "chứng
// chỉ đã đúng chưa" để bỏ qua khi chạy lại.
cert := &steps.CertbotCert{Layout: l, Webroot: acmeRoot, Email: email, Server: cfg.ServerURL()}
ss := []engine.Step{
	&steps.Dir{Path: acmeRoot + "/.well-known/acme-challenge", Perm: 0o755, Owner: system.Root},
	&steps.ACMEProbe{Layout: l, Webroot: acmeRoot, Cert: cert},
	cert,
}
```

Sau đó là `steps.DefaultSSL(web)` (site mặc định 443), `steps.SiteVhostSSL(...)` (vhost có 443), `steps.WPSiteURL` với `https://<domain>`, và một `steps.File` ghi hook gia hạn vào `RenewHookPath` (`/etc/letsencrypt/renewal-hooks/deploy/tpan`, quyền 0755). Kiểm chứng: `checks.SiteChecksSSL`, `checks.Service("certbot.timer", ...)`, `checks.CertRenewDryRun`. `acmeRoot` là `/var/lib/tpan/acme`, hằng số trong `internal/core/stack.go`, cùng thư mục vhost cổng 80 của mọi site đã phục vụ từ M1, M2.

Thứ tự có lý do. Thử HTTP trước khi xin, để lỗi không tốn lượt. Xin chứng chỉ trước khi ghi vhost 443, vì vhost 443 trỏ tới file chứng chỉ: ghi vhost trước thì `nginx -t` báo không có file và bước lỗi. Site mặc định 443 trước vhost của site, để ngay khi cổng 443 mở đã có chỗ hứng tên lạ. Đặt URL WordPress sau vhost, vì lúc đó https đã chạy.

### 3.5 `internal/steps/ssl.go`: `ACMEProbe`

`Apply` ghi một file tên ngẫu nhiên, nội dung ngẫu nhiên vào thư mục thử thách, gọi lại nó qua đúng đường mà Let's Encrypt sẽ đi, rồi so:

```go
name := strings.ToLower(rand.Text())
want := rand.Text()
file := dir + "/" + name
if err := sys.WriteFileAtomic(file, []byte(want), 0o644, system.Root); err != nil {
	return fmt.Errorf("ghi file thử %s: %w", file, err)
}
defer func() {
	if rerr := sys.Remove(file); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
		err = errors.Join(err, fmt.Errorf("xoá file thử %s: %w", file, rerr))
	}
}()

cmd := s.curl(name)
res, cerr := sys.Run(ctx, cmd)
got := strings.TrimSpace(res.Stdout)
if cerr == nil && got == want {
```

- Ngẫu nhiên vì câu trả lời khớp chỉ có thể đến từ đúng file này trên đúng máy này. Một trang 404 hay trang của máy khác không thể tình cờ trùng.
- `curl` không có `--resolve`: nó phân giải tên qua DNS của máy, như Let's Encrypt sẽ làm. Các phép kiểm khác (`HTTPRedirect`, `HTTPSSite`) thì có `--resolve ...:127.0.0.1`, vì chúng hỏi "web server có phục vụ đúng không", không hỏi "DNS có đúng không".
- `defer` xoá file trong mọi trường hợp, kể cả khi curl lỗi. Nên bước này không cần hoàn tác (`Undo` trống, `NoUndo` nói rõ).
- Không khớp thì lỗi kèm `sslResolve`: chạy `getent ahosts <domain>` để nói tên đang trỏ về đâu (dòng "đang trỏ về 192.0.2.1" ở mục 2.4).

`Check` trả `true` khi `Apply` của chính nó đã đạt (`passed`), hoặc khi `s.Cert.Check` đạt. `s.Cert` là cùng con trỏ với bước xin chứng chỉ đứng ngay sau, nên lần chạy lại, chứng chỉ đã đúng thì bước thử cũng bỏ qua (mục 2.8). `validate` còn kiểm `Cert` cùng `Layout` và `Webroot` với bước thử, để không bỏ qua bước thử chỉ vì chứng chỉ của site khác đã có.

### 3.6 `CertbotCert`: xin chứng chỉ

```go
func (s *CertbotCert) cmd(force bool) system.Cmd {
	d := s.Layout.Domain.String()
	args := []string{"certonly", "--webroot", "-w", s.Webroot, "-d", d, "--cert-name", d,
		"--non-interactive", "--agree-tos", "-m", s.Email.String(), "--server", s.Server}
	if force {
		args = append(args, "--force-renewal")
	}
	return system.Cmd{Name: "certbot", Args: args}
}
```

- `certonly --webroot`: certbot chỉ đặt file thử thách vào `-w` rồi lấy chứng chỉ, không đụng config của nginx hay apache. Config do tpan sinh từ template (mục 3 của KE-HOACH.md, dòng "Chứng chỉ").
- `--cert-name <domain>`: bộ file của chứng chỉ (certbot gọi là lineage) luôn mang đúng tên domain, không bao giờ thành `<domain>-0001`. Nhờ vậy `internal/site/ssl.go` suy được mọi đường dẫn từ domain: `CertDir`, `CertFullchain`, `CertPrivkey`, `CertRenewal`.
- Chạy bằng argv qua `system.Cmd`, không qua shell. Email đã qua `validate`, nhưng `validate()` của bước còn chặn email bắt đầu bằng `-` (certbot sẽ đọc nhầm thành tuỳ chọn) và địa chỉ máy chủ không đúng dạng `https://...`.

`Check` đạt khi có `fullchain.pem` và dòng `server = ...` trong file renewal bằng đúng `s.Server`. `Apply` đọc file renewal: đã có chứng chỉ nhưng của máy chủ khác (staging sang thật) thì thêm `--force-renewal`, vì certbot thấy chứng chỉ cũ chưa tới hạn sẽ không xin lại. Nhánh này chưa chạy trên máy ảo (mục 2.12).

`Undo` không làm gì. Lý do ở mục 5.

### 3.7 Site mặc định 443 và vhost có HTTPS

`steps.DefaultSSL(web)` trả các bước ghi file `tpan-default-ssl.conf` (Nginx) hoặc `000-tpan-default-ssl.conf` (Apache) rồi bật symlink. Với Apache thêm hai bước đứng trước: `AptInstall{ssl-cert}` (gói tạo chứng chỉ tự ký snakeoil) và `ApacheModules{Enable: ssl}`, vì `SSLEngine` là lệnh của mod_ssl, chưa bật module thì `apachectl configtest` báo sai cú pháp.

Template của Nginx, `internal/templates/files/nginx-default-ssl.conf.tmpl`:

```nginx
server {
    listen 443 ssl default_server;
    listen [::]:443 ssl default_server;

    server_name _;

    ssl_reject_handshake on;
}
```

(Bỏ các dòng chú thích.) Apache không có lệnh tương đương `ssl_reject_handshake`, nên `apache-default-ssl.conf.tmpl` dùng chứng chỉ snakeoil và `Require all denied` cho mọi đường dẫn. Tên `000-` để Apache nạp nó trước mọi vhost 443 của site và coi nó là mặc định.

`steps.SiteVhostSSL` ghi đè file vhost của site bằng nội dung từ `templates.SiteVhost(l, web, acmeRoot, true)`. `File` của M1 giữ bản cũ, nên khi một bước sau lỗi, hoàn tác trả lại vhost cổng 80. Phần cổng 80 của `site-nginx-ssl.conf.tmpl`:

```nginx
    location ^~ /.well-known/acme-challenge/ {
        root {{.AcmeRoot}};
    }

    location / {
        return 301 https://{{.Domain}}$request_uri;
    }
```

(Bỏ chú thích.) `^~` nghĩa là khớp tiền tố này thì Nginx không xét location nào khác, nên đường thử thách được phục vụ thẳng từ webroot, không bị 301: gia hạn vẫn đi qua cổng 80.

Khối `server` cổng 443 chép đủ mọi luật bảo mật của bản cổng 80 (chặn file ẩn, chặn PHP trong uploads, `try_files`, `fastcgi_pass` tới pool riêng), cộng `ssl_certificate` và `ssl_certificate_key`. Bản Apache (`site-apache-ssl.conf.tmpl`) dùng `RewriteCond %{REQUEST_URI} !^/\.well-known/acme-challenge/` rồi `RewriteRule ... [R=301,L]`; mục 5 nói vì sao không dùng `Redirect`.

`templates.SiteVhost` là chỗ duy nhất chọn giữa bốn template (nginx hoặc apache, có hoặc không SSL). Tạo site, chạy lại, xin SSL, và sau này phát hiện lệch ở M4 đều phải gọi qua nó.

### 3.8 `WPSiteURL`: tránh vòng lặp chuyển hướng

WordPress lưu địa chỉ của site trong hai tuỳ chọn `home` và `siteurl`, và tự chuyển khách về địa chỉ đó. Site tạo ở M2 có `http://...`. Web server giờ chuyển http sang https; WordPress lại chuyển https về http; trình duyệt chạy qua lại tới khi báo "quá nhiều lần chuyển hướng". Bước này đặt cả hai thành `https://<domain>` bằng `wp option update`, chạy dưới user của site (`wpRun`).

- `Check` đọc lần lượt `home`, `siteurl`, gặp giá trị khác thì trả `false` ngay.
- `Apply` đọc lại cả hai, nhớ giá trị cũ của những tuỳ chọn cần đổi (`old`), rồi update. Nếu update thứ hai lỗi, nó tự trả lại cái thứ nhất (`restore`), vì engine không gọi `Undo` cho bước có `Apply` lỗi.
- `Undo` đặt lại đúng những giá trị đã đổi, theo thứ tự ngược.
- `sslRestorable` từ chối đổi nếu giá trị cũ bắt đầu bằng `-` hoặc có ký tự điều khiển: giá trị đó không đi qua argv an toàn khi hoàn tác được.

### 3.9 Hook gia hạn và `tpan internal reload-web`

Template `renew-hook.tmpl` sinh ra file bạn đã thấy ở mục 2.6. Dòng đầu bắt buộc là `#!/bin/sh` nên dòng `# Do tpan quản lý...` nằm ở dòng hai. `templates.RenewHook` kiểm đường dẫn `tpanBin` bằng cùng luật như mọi đường dẫn đi vào config (`siteCheckPath`), nên không có cách nào chèn thêm lệnh vào dòng `exec`.

Hook gọi `tpan internal reload-web` (`cmd/tpan/internal.go`, lệnh ẩn). Lệnh này không mở DB nếu chưa có file DB, rồi gọi `core.ReloadWeb`:

```go
test := system.Cmd{Name: "nginx", Args: []string{"-t"}}
switch cfg.Web {
case "":
	return nil
case Apache:
	test = system.Cmd{Name: "apachectl", Args: []string{"configtest"}}
}
// Config sai thì reload sẽ làm web server từ chối nạp; giữ nguyên bản đang
// chạy (vẫn phục vụ được) và báo lỗi để certbot ghi vào log của nó.
if _, err := sys.Run(ctx, test); err != nil {
	return fmt.Errorf("kiểm config %s trước khi reload: %w", cfg.Web, err)
}
reload := system.Cmd{Name: "systemctl", Args: []string{"reload", webUnit(cfg.Web)}}
```

Mục 6.3 bước 4 muốn hook "reload đúng web server đang dùng". Gọi tpan thay vì gọi thẳng `systemctl reload nginx` thì file hook giống nhau cho Nginx và Apache: tpan đọc `stack.web` lúc chạy. Khi có chuyển LEMP sang LAMP (M10), hook không phải ghi lại. Kiểm config trước khi reload để một config hỏng không làm web server từ chối nạp ngay giữa đêm gia hạn.

### 3.10 Kiểm chứng: `SiteChecksSSL` và `CertRenewDryRun`

`checks.SiteChecksSSL` (trong `internal/checks/ssl.go`) là bộ phép kiểm của site có SSL, theo đường đi của request: `TLSCert`, `VhostEnabled`, `PoolSocket`, `WPChecksums`, `WPConfigPerm`, `DBAccess`, `HTTPRedirect`, `HTTPSSite`. So với bộ của M2, `HTTPSite` (gọi http, chờ 200) được thay bằng hai phép: cổng 80 phải trả 301 về `https://<domain>/`, và cổng 443 phải trả 200 đúng WordPress.

- `TLSCert` đọc `fullchain.pem` bằng Go (`crypto/x509`), không gọi `openssl`. Nó kiểm tên (`VerifyHostname`), hết hạn, chưa tới ngày hiệu lực (đồng hồ máy chậm), và cảnh báo khi còn dưới 14 ngày (`sslWarnDays`): certbot gia hạn khi còn 30 ngày, nên còn dưới 14 nghĩa là gia hạn tự động đã hỏng ít nhất hai tuần. Tên CA có chữ "(STAGING)" thì tóm tắt thêm "chứng chỉ thử".
- `HTTPSSite` dùng `curl -k` vì chứng chỉ staging không được tin; việc tin hay không đã có `TLSCert` lo.

`CertRenewDryRun`:

```go
args := []string{"renew", "--dry-run", "--no-random-sleep-on-renew", "--cert-name", dom, "--non-interactive"}
if server != "" {
	args = append(args, "--server", server)
}
```

`--dry-run` làm cả quy trình gia hạn (kể cả thử thách HTTP-01) với máy chủ thử của Let's Encrypt mà không ghi chứng chỉ mới. Đây là cách duy nhất biết trước gia hạn tự động có chạy được không. `--server` chỉ thêm khi dùng máy chủ tuỳ chỉnh (pebble). Không có `--server` thì `--dry-run` luôn đi tới máy chủ staging của Let's Encrypt, kể cả khi chứng chỉ là bản thật, chứ không đọc file renewal (thấy trên máy ảo: một lần chạy thiếu `--server` đã gọi ra staging thật và bị từ chối vì tên `example.com`). Nhờ vậy gia hạn thử không tốn hạn mức của chứng chỉ thật. Phép này nặng (gọi ra internet), nên chỉ nằm ở kiểm chứng của Plan SSL, không ở health định kỳ.

### 3.11 `Execute`, và `internal/core/site.go` nhớ SSL

Job thành công thì `SSLIssue.Execute` ghi `site.SSL = true` vào bảng `sites`. Ghi lỗi thì thông báo bảo chạy lại `tpan ssl issue`, vì thiếu cờ này là lần chạy lại `site add` sẽ ghi vhost cổng 80 đè lên vhost 443.

Cờ đó được đọc ở ba chỗ trong `internal/core/site.go`:

- `PrepareAdd` (chạy lại `site add`): `rec.SSL` đúng thì URL cài WordPress là `https://`, và `addPlan` dùng `steps.SiteVhostSSL` thay cho `steps.SiteVhost`. Kiểm chứng (`siteChecks`) dùng `checks.SiteChecksSSL`. Đó là mục 2.8.
- `Health`: cũng qua `siteChecks` với `st.SSL`.
- `PrepareRemove` (xoá hẳn):

```go
if st.SSL {
	// Xoá chứng chỉ ở CUỐI (mục 6.4): bước nào trước đó lỗi thì engine hoàn
	// tác trả lại vhost 443, mà vhost đó trỏ tới chứng chỉ; chứng chỉ đã mất
	// thì kiểm config lúc hoàn tác lỗi theo. Để cuối thì lỗi ở đây chỉ còn
	// sót chứng chỉ, chạy lại `site rm` dọn nốt.
	ss = append(ss, &steps.CertbotDelete{Layout: l})
}
```

`CertbotDelete` chạy `certbot delete --cert-name <domain> --non-interactive`, đánh dấu `Danger`, không hoàn tác được.

### 3.12 `tpan security ufw`: `PrepareUFW`, `UFWAllow`, `UFWEnable`

`cmd/tpan/security.go` dựng hai lệnh `ufw` và `fail2ban` bằng cùng một hàm `securityPlanCmd`; chúng chỉ khác nhau ở hàm dựng Plan (`(*core.Security).PrepareUFW` hay `PrepareFail2ban`). Lệnh đòi root cả khi `--dry-run`, vì dựng Plan ufw phải chạy `sshd -T`.

`core.Security.PrepareUFW` (trong `internal/core/security.go`):

```go
cfg, _, err := checks.SSHDConfig(ctx, s.Sys)
if err != nil {
	return nil, sshPortUnknown(err.Error())
}
ports := cfg["port"]
if len(ports) == 0 {
	return nil, sshPortUnknown("sshd -T không in dòng port")
}
```

Mỗi cổng trong `ports` thành luật `<cổng>/tcp`, rồi thêm `80/tcp`, `443/tcp`. Cổng panel `8443/tcp` chỉ thêm khi `checks.PanelListen` thấy panel nghe ở địa chỉ khác loopback; panel ở chế độ riêng tư (chỉ `127.0.0.1`) thì không mở cổng của nó. Mục 6.8 viết "mở 22, 80, 443 và cổng panel"; mã mở cổng SSH thật thay vì 22 cố định.

`checks.SSHDConfig` chạy `sshd -T`, lệnh in cấu hình hiệu lực của sshd (đã gộp `sshd_config`, `sshd_config.d`, và điền mặc định). Không đọc được thì từ chối `ssh_port_unknown`, không đoán 22.

Plan có ba bước theo thứ tự: `AptInstall{ufw}`, `UFWAllow{Rules}`, `UFWEnable`. Luật cho phép phải có trước khi bật.

`UFWAllow.missing` đọc `ufw show added`, lệnh liệt kê luật đã thêm và đọc được cả khi ufw đang tắt (khác `ufw status`). `Apply` chỉ nhớ những luật mà ufw trả lời "Rule added":

```go
// Chỉ nhớ luật ufw báo "Rule added". Luật đã có ("Skipping adding
// existing rule", "Rule updated") không phải do bước này thêm: Undo mà
// gỡ nó thì có thể gỡ mất luật SSH của người quản trị và khoá máy.
if strings.Contains(res.Stdout, "Rule added") {
	added = append(added, r)
}
```

`UFWEnable` chạy `ufw --force enable` (`--force` để ufw không hỏi y/n), là bước `Danger`; `Check` đọc dòng đầu của `ufw status` có phải `Status: active`. `Undo` là `ufw disable`.

Kiểm chứng là `checks.UFW` (trong `internal/checks/security.go`): đọc `ufw status verbose` bằng `parseUFW`, chạy lại `sshd -T`, và trả `Fail` nếu ufw đang bật mà thiếu luật cho bất kỳ cổng SSH nào sshd đang nghe. Phiên SSH đang mở vẫn sống, nhưng đăng nhập mới sẽ bị chặn.

### 3.13 `tpan security fail2ban`: `PrepareFail2ban`

Ba bước: `AptInstall{fail2ban}`, `File` ghi `/etc/fail2ban/jail.d/tpan-sshd.conf` (hằng `Fail2banJail`) từ `templates.Fail2banSSHD`, có `Test` là `fail2ban-client -t` và `Reload` là `fail2ban-client reload`, rồi `Service{fail2ban}`. Template:

```ini
[sshd]
enabled = true
backend = systemd
```

`backend = systemd` vì Ubuntu 24.04 ghi log của sshd vào journal của systemd. Kiểm chứng `checks.Fail2banSSHD` chạy `fail2ban-client status sshd`: mã thoát 0 là đạt, và tóm tắt lấy số IP đang bị chặn từ dòng `Currently banned:`.

### 3.14 `tpan security status`

`core.Security.Status` chạy `checks.SecurityChecks(PanelPort)`: `UFW`, `Fail2banSSHD`, `SSHRootLogin`, `SSHPasswordAuth`, `Swap`, `SecurityUpdates`, `PanelExposed`. Hai dòng SSH chỉ cảnh báo: `PermitRootLogin yes` hoặc `PasswordAuthentication yes` là `Warn`, kèm các lệnh để người vận hành tự sửa (`secSSHDFix`): tìm dòng cấu hình, `sshd -t`, `systemctl reload ssh`, `sshd -T`. Không có Plan nào sửa SSH (mục 5). `SecurityUpdates` chạy `apt-get -s` (mô phỏng, không cài) và đếm dòng `Inst` thuộc kho `-security`.

## 4. Kiến thức nền

### HTTPS, TLS và chứng chỉ

HTTPS là HTTP chạy bên trong một kết nối TLS: dữ liệu được mã hoá, nên mật khẩu đăng nhập WordPress không đi qua mạng ở dạng đọc được. Lúc bắt tay TLS, máy chủ đưa ra một chứng chỉ: tài liệu nói "khoá công khai này thuộc về tên miền này", được một CA (tổ chức cấp chứng chỉ) ký. Trình duyệt tin chứng chỉ khi CA nằm trong danh sách nó tin, tên khớp, và còn hạn. File `fullchain.pem` chứa chứng chỉ của site cộng chứng chỉ của CA trung gian; `privkey.pem` là khoá riêng, chỉ root đọc. Trong mã: `checks.TLSCert`. Tự xem: `scripts/vm.sh root openssl x509 -in /etc/letsencrypt/live/s1.example.com/cert.pem -noout -subject -issuer -dates` (mục 2.6).

### Let's Encrypt, ACME và thử thách HTTP-01

Let's Encrypt là CA miễn phí, cấp chứng chỉ tự động qua giao thức ACME. Để chứng minh bạn giữ tên miền, nó đưa một chuỗi bí mật, certbot đặt chuỗi đó vào file `/.well-known/acme-challenge/<token>`, rồi máy chủ của Let's Encrypt gọi `http://<domain>/.well-known/acme-challenge/<token>` qua cổng 80. Gọi được và đúng nội dung thì cấp. Đó là thử thách HTTP-01. Chế độ webroot nghĩa là certbot chỉ ghi file vào một thư mục (`-w /var/lib/tpan/acme`), còn web server tự phục vụ thư mục đó. Trong mã: `CertbotCert.cmd`, khối `location ^~ /.well-known/acme-challenge/` của vhost. Tự xem: `scripts/vm.sh root grep acme-challenge /var/log/tpan/sites/s1.example.com/access.log` (user agent `LetsEncrypt-Pebble-VA` ở máy ảo).

### Staging và hạn mức

Let's Encrypt giới hạn số chứng chỉ: mục 6.3 ghi 5 chứng chỉ cho cùng một bộ tên trong 7 ngày, 50 cho mỗi tên miền đăng ký trong 7 ngày (chung cho mọi người dùng tên miền đó). Máy chủ staging hạn mức rộng hơn nhiều, cấp chứng chỉ do một CA thử ký (trình duyệt không tin, tên CA có chữ "(STAGING)"). Lúc phát triển luôn dùng staging; chỉ tắt cho buổi demo. Trong mã: `SSLConfig.ServerURL`, `steps.LetsEncryptStaging`. Tự xem: `scripts/vm.sh root tpan ssl settings` (dòng `ACME:`).

### SNI và site mặc định cổng 443

Nhiều site dùng chung một IP và một cổng 443. Trình duyệt gửi tên miền ngay trong lời chào TLS (SNI, Server Name Indication) để máy chủ chọn đúng chứng chỉ. Tên không khớp vhost nào thì web server dùng vhost mặc định; không khai báo thì Nginx lấy vhost 443 đầu tiên, tức là ai quét IP cũng thấy chứng chỉ (và tên) của một site thật. `ssl_reject_handshake on` (Nginx 1.19.4 trở lên) cho Nginx từ chối ngay lúc bắt tay. Apache không có lệnh đó nên dùng chứng chỉ tự ký "snakeoil" của gói `ssl-cert` và trả 403. Trong mã: `nginx-default-ssl.conf.tmpl`, `apache-default-ssl.conf.tmpl`. Tự xem: `scripts/vm.sh exec curl -sS -k https://127.0.0.1/` (Nginx: mã 35; Apache: 403).

### Chuyển hướng 301

301 là "đã chuyển hẳn": trình duyệt nhớ, công cụ tìm kiếm chuyển thứ hạng sang địa chỉ mới. 302, 307 là tạm. `HTTPRedirect` coi 302 hay 307 là lỗi. Vòng lặp chuyển hướng xảy ra khi hai bên chuyển ngược nhau: web server http sang https, WordPress https về http. Trong mã: `steps.WPSiteURL`. Tự xem: `scripts/vm.sh exec curl -sS --resolve s1.example.com:80:127.0.0.1 -o /dev/null -D - http://s1.example.com/` (dòng `Location:`).

### Gia hạn: `certbot.timer`, file renewal, hook

Gói `certbot` của Ubuntu cài sẵn một bộ hẹn giờ systemd, `certbot.timer`, chạy `certbot renew` hai lần mỗi ngày; certbot chỉ gia hạn chứng chỉ còn dưới 30 ngày. Mỗi chứng chỉ có một file `/etc/letsencrypt/renewal/<domain>.conf` ghi máy chủ ACME, webroot, email, để lần gia hạn dùng đúng cách đã xin. Sau mỗi lần gia hạn thành công, certbot chạy mọi file trong `/etc/letsencrypt/renewal-hooks/deploy/`. Web server chỉ đọc chứng chỉ lúc khởi động hoặc reload, nên thiếu hook thì file trên đĩa mới mà trình duyệt vẫn thấy chứng chỉ cũ. Trong mã: `templates.RenewHook`, `core.ReloadWeb`. Tự xem: `scripts/vm.sh exec systemctl list-timers certbot.timer` (chưa chạy trong M5.log), và `scripts/vm.sh root cat /etc/letsencrypt/renewal/s1.example.com.conf` (chưa chạy) để thấy dòng `server =`.

### ufw và chính sách mặc định

ufw là lớp vỏ dễ dùng của tường lửa trong nhân Linux. Khi bật, chính sách mặc định của Ubuntu là `deny (incoming)`: mọi kết nối vào bị chặn, trừ cổng đã có luật `allow`. Bật mà chưa có luật cho cổng SSH thì phiên đang mở còn sống, nhưng không SSH vào được nữa; trên VPS, lúc đó chỉ còn console web của nhà cung cấp. `ufw show added` đọc luật cả khi ufw tắt. Trong mã: `steps.UFWAllow`, `steps.UFWEnable`, `checks.parseUFW`. Tự xem: `scripts/vm.sh root ufw status verbose` (mục 2.11).

### `sshd -T`

`sshd -T` in cấu hình hiệu lực của sshd, mỗi dòng một khoá viết thường, sau khi đã gộp mọi file `Include` và điền giá trị mặc định. Đọc `sshd_config` bằng tay sẽ sai ngay khi có một file trong `sshd_config.d` đổi cổng. Cần root vì sshd phải đọc khoá host. Trên Ubuntu 24.04, cổng 22 do `ssh.socket` của systemd giữ, nên `ss -tlnp` thấy cả `systemd` và `sshd` ở cổng 22. Trong mã: `checks.SSHDConfig`, dùng ở cả `PrepareUFW` và hai dòng SSH của trang bảo mật. Tự xem: `scripts/vm.sh root bash -c 'sshd -T | grep -E "^(port|permitrootlogin|passwordauthentication) "'` (chưa chạy đúng dạng này; kịch bản chạy `grep -i "^port "` và thấy `port 22`).

### fail2ban, jail, journal

fail2ban đọc log, đếm số lần đăng nhập hỏng theo IP, và khi vượt ngưỡng thì thêm luật tường lửa chặn IP đó một thời gian. Một jail là một cặp "đọc log nào, chặn thế nào"; jail `sshd` có sẵn trong gói, tpan chỉ bật nó. `backend = systemd` cho fail2ban đọc journal thay vì file `/var/log/auth.log`. Dòng `Journal matches: _SYSTEMD_UNIT=sshd.service + _COMM=sshd` nghĩa là lấy dòng của unit `sshd.service` hoặc của tiến trình tên `sshd`; dấu `+` trong journalctl là "hoặc". Trên 24.04 unit tên `ssh.service`, nên jail đọc được log nhờ vế `_COMM=sshd` (theo `docs/thay-doi/M5.md`). Trong mã: `fail2ban-sshd.tmpl`, `checks.Fail2banSSHD`. Tự xem: `scripts/vm.sh root fail2ban-client status sshd`.

### Go: hai bước dùng chung một con trỏ

Trong `sslPlan`, `cert := &steps.CertbotCert{...}` tạo một giá trị và lấy địa chỉ của nó. Biến `cert` được đặt vào trường `Cert` của `ACMEProbe` và cũng được thêm vào danh sách bước. Hai chỗ trỏ tới cùng một struct, nên `ACMEProbe.Check` hỏi `s.Cert.Check` là hỏi đúng bước xin chứng chỉ sẽ chạy ngay sau. Nếu `sslPlan` tạo hai `&steps.CertbotCert{...}` riêng, ai đó sửa `Server` ở một cái mà quên cái kia thì bước thử sẽ hỏi về một chứng chỉ khác với chứng chỉ sắp xin. `TestSSLPlanOrder` trong `internal/core/ssl_test.go` kiểm đúng điều này: `probe.Cert != cert` là lỗi.

### Go: `defer` để dọn dẹp chắc chắn

`defer f()` chạy `f` khi hàm bao quanh trả về, dù trả về bình thường hay giữa chừng vì lỗi. `ACMEProbe.Apply` dùng nó để xoá file thử, và dùng `errors.Join` để gộp lỗi xoá file (nếu có) vào lỗi chính chứ không làm mất lỗi nào. Vì hàm khai báo giá trị trả về có tên (`(err error)`), hàm trong `defer` sửa được `err` trước khi nó tới người gọi.

## 5. Cách khác đã cân nhắc, và chỗ đã làm sai

### `certbot --nginx` hoặc thư viện ACME nhúng trong Go

Bảng công nghệ ở mục 3 của KE-HOACH.md ghi hai cách đã loại. `certbot --nginx` (và `--apache`) tự sửa config của web server: khi đó config có hai tác giả, tpan và certbot, và lần ghi vhost sau của tpan sẽ xoá phần certbot đã thêm (hoặc phép kiểm lệch của M4 báo lệch mãi). Thư viện ACME nhúng (lego) bớt một gói cài nhưng khó quan sát: không có `certbot certificates`, không có `letsencrypt.log`, không có `certbot.timer` sẵn. `certonly --webroot` để tpan giữ một nguồn config duy nhất và dùng cùng một cách cho Nginx và Apache.

### Thử đường HTTP là điều kiện đầu

Mục 6.3 xếp "thử HTTP" chung với "kiểm DNS" ở bước 1, tức là kiểm trước. Nhưng điều kiện đầu và bản xem trước chỉ được đọc máy (`--dry-run` không để lại gì), mà bước thử phải ghi một file vào webroot. Nên M5 làm nó thành một Step trong Plan: có hiện trong xem trước, có trong job, có hoàn tác (thư mục ở mục 2.4). Kiểm DNS chỉ đọc nên vẫn là điều kiện đầu. Ghi ở `docs/thay-doi/M5.md`.

### Hoàn tác xoá chứng chỉ

Theo quy ước, bước nào tạo gì thì hoàn tác xoá nó. Với chứng chỉ thì không: nếu một bước sau (vhost, URL WordPress) lỗi, hoàn tác xoá chứng chỉ, người dùng sửa rồi chạy lại là tốn thêm một lượt trong hạn mức 5 chứng chỉ cùng bộ tên mỗi 7 ngày. Vài lần thử là hết lượt cả tuần. Chứng chỉ nằm trên đĩa không làm gì khi chưa vhost nào trỏ tới nó, và lần chạy lại bỏ qua bước xin. Chứng chỉ chỉ bị xoá khi xoá hẳn site. Ghi ở `docs/thay-doi/M5.md` và `NoUndo` của `CertbotCert.Info`.

### Ba cách viết vhost SSL ngắn hơn mà không dùng

Cả ba đều ghi lý do ngay trong chú thích của template.

- `Redirect permanent / https://...` trong Apache. Trong `site-apache-ssl.conf.tmpl`: mod_alias xử lý mọi `Redirect` trước mọi `Alias`, nên `Redirect /` nuốt luôn đường thử thách, và lần gia hạn sau thất bại (lần xin đầu thì không, nên lỗi lộ ra sau vài tuần). `RewriteCond` loại trừ `/.well-known/acme-challenge/` rồi mới `RewriteRule` thì thử thách đi tiếp tới `Alias`. Lượt Apache kiểm đúng điều này: file thử ở cổng 80 trả 200, không trả 301 (M5.log dòng 2775).
- `return 301 https://$host$request_uri` trong Nginx, gọn và dùng chung được. Nhưng `$host` lấy từ header `Host` của request, kẻ gửi `Host` lạ sẽ khiến máy chủ chuyển trình duyệt tới tên do họ chọn. tpan sinh vhost riêng cho từng site nên chèn thẳng tên domain.
- Bật HSTS (header bảo trình duyệt "từ nay chỉ vào bằng https"). Cố ý không bật: với chứng chỉ staging trình duyệt không tin, HSTS sẽ khoá người dùng ngoài site, không cho bấm "tiếp tục".

### ufw: đoán cổng 22, và tự sửa SSH

Đoán 22 khi `sshd -T` không chạy được thì đơn giản hơn. Nhưng máy đổi cổng SSH (rất hay gặp trên VPS) sẽ bị khoá ngoài ngay khi bật tường lửa. `sshPortUnknown` từ chối thay vì đoán. Cũng vì lý do mất đường vào máy, mục 6.8 quyết định SSH chỉ cảnh báo: tự sửa `PermitRootLogin` hay `PasswordAuthentication` mà người dùng chưa có khoá SSH là tự khoá mình ngoài máy, và tpan không biết chắc người dùng có khoá hay không.

### Chỗ sai: `certbot renew --dry-run` chờ ngẫu nhiên tới 8 phút (commit `25c7bd5`)

Bản đầu của `CertRenewDryRun` chạy `certbot renew --dry-run --cert-name ...` không có cờ nào khác. Unit test đạt, vì unit test không chạy certbot thật. Lần chạy thử đầu trên máy ảo (trước khi có `M5.log` hiện tại), một lần `tpan ssl issue`, và cả lần chạy lại vốn bỏ qua mọi bước, mất từ 1 tới 4 phút; lần có ghi lại ở lượt Apache chạy từ 18:32:12 tới 18:36:02. Đọc `/var/log/letsencrypt/letsencrypt.log` trong máy ảo thấy dòng `Non-interactive renewal: random delay of 221.5... seconds`: khi chạy không có terminal, certbot tự chờ ngẫu nhiên tới 8 phút trước khi gia hạn, kể cả `--dry-run`, để bộ hẹn giờ trên mọi máy không gọi Let's Encrypt cùng một lúc. Trong lúc đó job giữ khoá toàn cục, mọi thao tác khác của tpan phải chờ. Đoạn log gốc được chép ở mục "Kết quả kiểm cho mục 16" của `docs/thay-doi/M5.md`, không nằm trong `M5.log`.

Sửa: thêm `--no-random-sleep-on-renew` vào lệnh của tpan. Bộ hẹn giờ `certbot.timer` vẫn chờ ngẫu nhiên như Let's Encrypt muốn; chỉ lần kiểm do người bấm là không chờ. Bằng chứng sau khi sửa là lệnh `grep` ở mục 2.8: mọi dòng `Arguments:` có cờ mới, không có dòng `random delay`, và lần xin chứng chỉ mất 8 giây. Bài học: lệnh chạy qua `system.System` không có terminal, và có chương trình đổi cách cư xử khi không có terminal; chỉ chạy thật mới thấy.

## 6. Tự kiểm

1. `tpan ssl issue` cho `s1.example.com` (trỏ về 127.0.0.1) chỉ in cảnh báo DNS rồi chạy tiếp, còn `s2.example.test` (không có bản ghi nào) bị từ chối ngay. Vì sao hai trường hợp khác nhau, và vì sao không chặn luôn trường hợp "A không trỏ về máy này"?
2. Xoá khối `location ^~ /.well-known/acme-challenge/` ở `server` cổng 80 của vhost SSL thì hỏng gì, và lúc nào mới thấy hỏng?
3. Vì sao `CertbotCert.Undo` không xoá chứng chỉ? Khi nào chứng chỉ mới bị xoá, và vì sao bước đó đứng cuối Plan xoá site?
4. Bỏ bước `WPSiteURL` khỏi `sslPlan` thì người mở `https://<domain>/` thấy gì? Phép kiểm nào sẽ bắt được?
5. Bỏ hook gia hạn thì sao? Vì sao bước kiểm chứng `certbot renew --dry-run` đạt không chứng minh được hook chạy?
6. Vì sao `tpan security ufw` không mở cứng cổng 22, và vì sao nó cần root ngay cả với `--dry-run`?
7. `UFWAllow.Apply` chỉ nhớ luật khi ufw trả lời "Rule added". Nếu nó nhớ mọi luật trong danh sách thì có thể xảy ra chuyện gì khi hoàn tác?
8. Bạn đổi `tpan ssl settings --staging=false` rồi chạy `tpan ssl issue` cho một site đã có chứng chỉ staging. certbot nhận lệnh gì khác lần đầu, và vì sao cần điều đó?
9. (Cần máy ảo) Chứng minh rằng máy chủ ACME thật sự gọi vào nginx của máy để kiểm thử thách, chứ không phải tpan tự giả kết quả.
10. (Cần máy ảo) Trong máy ảo đã xin SSL, chạy `curl -k https://127.0.0.1/` ở lượt Nginx và lượt Apache. Kết quả khác nhau thế nào, và vì sao?

## Đáp án

1. Điều kiện đầu chỉ chặn `Fail`. `DNSPointsHere` trả `Fail` khi không có bản ghi A: Let's Encrypt chắc chắn không tới được máy, chạy tiếp chỉ tốn lượt. Có A nhưng không địa chỉ nào nằm trên card mạng thì trả `Warn`, vì nhiều VPS đám mây đứng sau NAT: IP public không hiện trong `ip -o addr show scope global` dù DNS đúng. Chặn trường hợp này là chặn nhầm những máy đó. Câu trả lời thật đến từ bước `ACMEProbe` ngay sau: nó gọi qua đúng đường DNS, và nếu không về được máy này thì lỗi trước khi gọi certbot (mục 2.4). Với `s1.example.com` trên máy ảo, `127.0.0.1` là loopback nên không có trong danh sách `scope global`, vì vậy cũng ra `Warn`.
2. Mọi request cổng 80 rơi vào `location /` và bị 301 sang https. Lần xin đầu không hỏng, vì lúc đó vhost vẫn là bản cổng 80 của M2. Hỏng ở lần gia hạn: certbot `--webroot` đặt file thử thách, Let's Encrypt gọi qua http, nhận 301. Bước kiểm chứng `certbot renew --dry-run` sẽ bắt được ngay lúc xin (nó làm đúng quy trình gia hạn), và health sau này sẽ báo `TLSCert` cảnh báo khi còn dưới 14 ngày. Kịch bản e2e kiểm đúng điều này: file thử ở cổng 80 trả 200, không trả 301 (mục 2.6). Chưa chạy thử xoá khối này trên máy ảo; đây là suy luận theo template.
3. Xoá thì mỗi lần thử lại sau một lỗi ở bước sau tốn một lượt trong hạn mức 5 chứng chỉ cùng bộ tên mỗi 7 ngày. Giữ lại thì chứng chỉ không làm gì khi chưa vhost nào dùng nó, và lần chạy lại bỏ qua bước xin (`CertbotCert.Check` thấy fullchain và đúng máy chủ). Chứng chỉ bị xoá khi xoá hẳn site, bằng `CertbotDelete`. Bước đó đứng cuối vì nếu một bước trước đó lỗi, engine hoàn tác các bước đã chạy, kể cả trả lại vhost 443; vhost đó trỏ tới chứng chỉ, nên nếu chứng chỉ đã mất thì `nginx -t` lúc hoàn tác cũng lỗi theo. Để cuối thì lỗi ở `certbot delete` chỉ sót lại chứng chỉ, chạy lại `site rm` dọn nốt.
4. WordPress vẫn có `home` và `siteurl` là `http://...`. Theo phần "Vì sao" của bước (`WPSiteURL.Info`): WordPress chuyển khách về địa chỉ đó, tức https về http, còn web server chuyển http về https, nên trình duyệt bị chuyển qua lại và báo quá nhiều lần chuyển hướng. Kể cả trang nào không bị chuyển thì link và tài nguyên trong trang vẫn là `http://`. Phép kiểm sẽ bắt: `HTTPSSite` có hẳn một nhánh cho mã `301`, `302`, `303`, `307`, `308` qua https, trả `Fail` với nguyên nhân "vòng lặp chuyển hướng" và lệnh sửa tay `wp option update home|siteurl https://<domain>`. Job hỏng ở kiểm chứng thì engine hoàn tác cả Plan. Kịch bản e2e còn đếm số lần trang nhắc `http://s1.example.com` (phải là 0, mục 2.6). Chưa chạy thử bỏ bước này trên máy ảo; đây là suy luận theo mã.
5. Chứng chỉ trên đĩa được gia hạn, nhưng nginx hay apache vẫn giữ chứng chỉ cũ trong bộ nhớ tới lần reload hoặc khởi động lại tiếp theo. Nếu không có lần nào trước ngày hết hạn, trình duyệt thấy chứng chỉ hết hạn dù `certbot certificates` báo còn hạn. `--dry-run` cố ý không chạy hook: letsencrypt.log ghi `Dry run: skipping deploy hook command` (mục 2.7). Muốn chứng minh hook phải gia hạn thật (`certbot renew --force-renewal`) rồi so serial đang phục vụ ở cổng 443 trước và sau, như kịch bản đã làm: `397AF279660793F7` thành `15B2B83C51550933`.
6. Cổng SSH có thể không phải 22: nhiều VPS đổi cổng, và cấu hình có thể nằm trong một file của `sshd_config.d`. Mở 22 rồi bật ufw trên máy SSH ở cổng khác là khoá chính mình ngoài máy. tpan hỏi chính sshd bằng `sshd -T` (cấu hình hiệu lực), đọc được thì mở đúng các cổng đó, không đọc được thì từ chối `ssh_port_unknown`. `sshd -T` cần root vì sshd phải đọc khoá host; dựng Plan (kể cả để xem trước) cần biết cổng, nên `securityPlanCmd` đòi root trước cả khi xem cờ `--dry-run`.
7. Giả sử người quản trị đã tự mở `22/tcp` từ trước. `ufw allow 22/tcp` lần này trả "Skipping adding existing rule". Nếu bước nhớ `22/tcp` là do mình thêm, thì khi bước sau (`UFWEnable`) hay kiểm chứng lỗi, `Undo` chạy `ufw delete allow 22/tcp` và gỡ mất luật SSH của người quản trị. Nếu ufw vẫn đang bật từ trước, đó là khoá máy. Test cho đúng trường hợp này: `TestUFWAllowSkippedRuleNotUndone` trong `internal/steps/ufw_test.go`.
8. Theo mã, chưa chạy trên máy ảo: `ServerURL` giờ là `steps.LetsEncryptProd`. `CertbotCert.Check` đọc dòng `server =` trong `/etc/letsencrypt/renewal/<domain>.conf`, thấy là địa chỉ staging, khác nên trả `false`. `ACMEProbe.Check` hỏi cùng `Check` đó nên bước thử cũng chạy lại. `Apply` thấy đã có lineage nhưng khác máy chủ nên thêm `--force-renewal` vào `certbot certonly ... --server https://acme-v02.api.letsencrypt.org/directory`. Thiếu cờ đó thì certbot thấy chứng chỉ staging còn hạn và không xin lại, site cứ mang chứng chỉ trình duyệt không tin. Test: `TestCertbotCertArgs`.
9. Xem access log của site, nơi chỉ nginx ghi: `scripts/vm.sh root bash -c 'grep acme-challenge /var/log/tpan/sites/s1.example.com/access.log | tail -4'`. Phải thấy các dòng `GET /.well-known/acme-challenge/<token> ... 200` với user agent `LetsEncrypt-Pebble-VA (linux; arm64)` (mục 2.6), vào đúng giờ chạy `certbot certonly`. Đối chiếu phía máy chủ ACME: `scripts/vm.sh root bash -c 'journalctl -u tpan-e2e-pebble --no-pager | grep s1.example.com | tail -8'` có `Attempting to validate w/ HTTP: http://s1.example.com:80/.well-known/acme-challenge/...` cùng token. Token đó không phải tên file ngẫu nhiên của bước thử tpan (`monrpgysovddxs5dxnyghrvcq2`), nên đó là request của pebble chứ không phải của tpan. Trên VPS, dòng tương ứng sẽ có user agent của Let's Encrypt (chưa chạy).
10. Lệnh: `scripts/vm.sh exec curl -sS -k -m 10 -o /dev/null -w '%{http_code}\n' https://127.0.0.1/`. Nginx: `curl: (35) ... tlsv1 unrecognized name`, mã thoát 35, `HTTP 000`: site mặc định 443 có `ssl_reject_handshake on`, từ chối ngay lúc bắt tay vì không có tên nào khớp (gọi bằng IP thì không có SNI). Apache: `HTTP 403`, mã thoát 0: Apache không có cách từ chối lúc bắt tay, nên `000-tpan-default-ssl.conf` đưa chứng chỉ snakeoil (CN là tên máy) rồi `Require all denied` trả 403. Cả hai không lộ chứng chỉ hay trang của `s1.example.com` (mục 2.6 và 2.10). Bỏ `-k` thì Apache cũng lỗi ở curl, nhưng vì chứng chỉ snakeoil không được tin, không phải vì máy chủ từ chối (chưa chạy).
