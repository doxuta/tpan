# M19: tên phụ của site, chuyển hướng, chứng chỉ phủ đủ các tên (thiết kế đợt 0)

Bản thiết kế đợt 0 của mốc M19 (mục 6.17 của `docs/KE-HOACH.md`, migration 1900–1999, e2e bước 22). Khung do điều phối chốt ở `agent-dev/TU-CHAY-M19.md`; file này kiểm lại khung bằng bản thử thật trên máy ảo `tpan-m19` (ngày 25/11/2026) và chốt tới mức thợ viết mã không phải đoán. Bản thử và output nằm ở `docs/bang-chung/M19-nghien-cuu/`.

Quy ước: "A", "B" là hai user thường; `example.com`, `example.net`, `example.org` là tên thử; "tên" là một tên miền đầy đủ (FQDN) đã qua `validate.ParseDomain`; "tên miền gốc" là eTLD+1 theo Public Suffix List.

---

## 1. Vấn đề và phạm vi

Một site thật hầu như luôn có nhiều tên: `www.` và tên trần, tên cũ chuyển về tên mới, tên miền thứ hai trỏ cùng nội dung. Tới v0.2.x mỗi site của TPAN có đúng một tên (`sites.domain`), và muốn chuyển hướng thì phải sửa vhost bằng tay (TPAN coi là lệch và báo).

**Làm trong M19:**

1. **Tên phụ** (alias) của site, mỗi tên một kiểu: `serve` (phục vụ cùng nội dung) hoặc `redirect` (301 về tên chính, giữ đường dẫn và query). Site mới mặc định có một tên phụ `www.` kiểu `redirect` (chọn được `serve` hoặc không có). Site có từ trước giữ nguyên.
2. **Quyền sở hữu tên**: một tên miền gốc (eTLD+1) chỉ thuộc một chủ trên máy. Áp cho cả tên chính lúc tạo site (đây cũng là lỗ P4 của rà soát) và tên phụ. Viết ở **một** chỗ dùng chung (mục 3.2), M28 dùng lại.
3. **Một tên chỉ thuộc một site** trên toàn máy, kể cả tên chính của site khác: khoá chính ở DB, kiểm và giữ chỗ trong một transaction.
4. **Chuyển hướng theo đường dẫn**: tối đa 50 luật mỗi site, nguồn là đường dẫn khớp đúng, đích là đường dẫn hoặc URL `http(s)://`, mã 301 hoặc 302, giữ query hay không. Không có regex do người dùng viết.
5. **Chứng chỉ SAN** cho tên chính và mọi tên phụ đã trỏ đúng về máy; tên chưa đạt không làm hỏng chứng chỉ đang có.
6. **Định tuyến**: `tpan route explain <host>` nhận tên phụ; phép kiểm health cho tên phụ (chỉ cảnh báo); `web.server_name.unique` tính cả tên phụ.
7. Mọi thay đổi là Plan có xem trước, hoàn tác khi kiểm chứng hỏng. CLI, API (vào bảng phân quyền một chỗ), MCP chỉ đọc, giao diện bản 2.

**Không làm (và vì sao):**

- Tên đại diện `*.example.com`: cần xác minh DNS-01 (TPAN không quản DNS, mục 1 của KE-HOACH) và làm luật sở hữu phức tạp hơn nhiều.
- Regex, chuyển hướng có điều kiện (theo Host, cookie, ngôn ngữ), mã 307/308: regex do người dùng viết mở đường ReDoS và chèn cấu hình; ba thứ còn lại chưa ai cần.
- Đường dẫn nguồn có ký tự ngoài `[A-Za-z0-9._~/-]` (chữ có dấu, `%`): nginx so `location =` với đường dẫn đã giải mã (bản thử P06), Apache coi `%N` trong đích là tham chiếu ngược. Bản sau nếu cần thì thêm cách mã hoá riêng cho từng web server.
- Chuyển chủ một tên miền gốc giữa hai user, "giải phóng" tên của site đang ở thùng rác: không có thao tác; admin xoá tên phụ trên site đang giữ rồi thêm lại.
- Tên phụ dạng IP, cổng khác 80/443.

---

## 2. Nghiên cứu (25/11/2026, máy ảo `tpan-m19`: Ubuntu 24.04.5 arm64, nginx 1.24.0, Apache 2.4.58, certbot 2.9.0-1, pebble 2.4.0+ds1-2ubuntu0.24.04.3; TPAN v0.2.2-96-gf6a68b9 cài bằng `tpan stack install --web nginx`, `tpan site add example.com`, rồi `tpan stack switch --to apache`)

### 2.1 nginx: `server_name` nhiều tên, khối chuyển tên phụ, `location =` cho luật đường dẫn (t1)

Script `t1-nginx.sh`, vhost thử `t1-nginx-vhost.conf` (vhost thật của site cộng phần M19), output `t1-nginx.out`. `nginx -t` mã 0. Lần chạy đầu thấy `000` ở ba dòng đầu vì đo ngay sau `systemctl reload` (worker cũ còn phục vụ cấu hình cũ một lúc); script chờ 3 giây rồi đo lại, output lưu là lần hai. **Hệ quả cho mã: bước kiểm chứng sau reload phải thử lại vài lần trong vài giây**, như các bước kiểm HTTP sẵn có.

| Đo | Kết quả | Hệ quả cho thiết kế |
|---|---|---|
| N01–N02 tên chính, tên phụ `serve` (punycode) | 200, trang WordPress | `server_name` nhiều tên là đủ cho `serve` |
| N03–N04 tên phụ `redirect`, `/a?b=1` | 301 `http://example.com/a?b=1` | khối riêng `return 301 <scheme>://<tên chính>$request_uri` giữ đường dẫn và query |
| N05 thử thách ACME trên tên phụ `redirect` | 200 nội dung file thử | `location ^~ /.well-known/acme-challenge/` đứng trong khối chuyển, xin được chứng chỉ cho tên này |
| P01, P05, P06 `/old-page`, `//old-page`, `/old%2Dpage` | 301 `/new-page` | `location =` so với đường dẫn đã giải mã và gộp gạch chéo |
| P02 `/old-page?x=1` | 301 `http://example.com/new-page` (mất query) | giữ query phải ghi `$is_args$args` |
| P03, P04 `/old-page/`, `/OLD-PAGE` | 404 của WordPress | khớp đúng, phân biệt hoa thường và gạch cuối: nói rõ trên giao diện |
| P07–P09 giữ/bỏ query, 302 | đúng như ghi | |
| P10 luật đường dẫn qua tên phụ `serve` | 301 `http://xn--vitnam-jk8b.example.net/new-page` | đích tương đối được nginx đổi thành tuyệt đối theo tên đang gọi (đúng tên trong `server_name`) |
| P11 luật đường dẫn qua tên phụ `redirect` | 301 về tên chính, chưa áp luật | hai bước chuyển; chấp nhận |
| S01, S03, S04 PHP trong uploads, `/.env` | 403 (cả qua tên phụ) | luật chặn sẵn có không đổi |
| S02 luật `=` cho đúng `/wp-content/uploads/evil2.php` | 301, PHP không chạy | `location =` thắng mọi location regex; vô hại (chỉ trả 301), nhưng vẫn cấm nguồn có đoạn bắt đầu bằng `.` để thử thách ACME và file ẩn không bao giờ bị luật người dùng che |
| H01–H05 `$host` trong khối chuyển https | luôn là một tên trong `server_name` (chữ thường, bỏ chấm cuối và cổng); URI tuyệt đối mang tên lạ thì rơi vào vhost mặc định | dùng được `$host`, nhưng thiết kế **không dùng** (mục 3.4): mỗi tên một đích cố định, khỏi phải tin chứng minh này về sau |

### 2.2 Apache: `ServerAlias`, `RewriteCond =`, thứ tự so với `.htaccess` của M14 (t2)

Script `t2-apache.sh`, vhost thử `t2-apache-vhost.conf`, output `t2-apache.out`. `apachectl -t`: `Syntax OK` (cảnh báo AH00558 có từ trước, không liên quan). `.htaccess` của site (chế độ `an-toan`) được thêm `Redirect 302 /old-page /from-htaccess` và `Redirect 302 /ht-only /from-htaccess`.

| Đo | Kết quả | Hệ quả |
|---|---|---|
| `apache2ctl -S` | `namevhost example.com` + dòng `alias xn--vitnam-jk8b.example.net`; vhost thứ hai `namevhost www.example.com` | `checks.apacheDecls` và `core.apacheDumpDecls` đã đọc dòng `alias` (M14), không phải sửa bộ đọc |
| N01–N06 | như nginx | |
| P01–P11 | như nginx, kể cả `%2D`, `//` | `RewriteCond %{REQUEST_URI} =<nguồn>` so chuỗi (không regex) trên đường dẫn đã giải mã |
| P02 `[QSD]` | bỏ query | bỏ query: cờ `QSD`; giữ: `QSA` |
| S06 cùng nguồn `/old-page` ở vhost và `.htaccess` | 301 `/new-page` của vhost | luật ở vhost chạy trước `.htaccess` (giai đoạn dịch tên so với giai đoạn fixup) |
| S05 `/ht-only` chỉ có ở `.htaccess` | 302 của `.htaccess` | `.htaccess` của site vẫn chạy như cũ |
| P03 `/old-page/` | 302 `/from-htaccess/` của `.htaccess` | `Redirect` của mod_alias khớp **tiền tố**: lý do vhost không dùng `Redirect` |
| R07 Unicode thô | 400 | |
| R08, R09, R11–R13 Host lạ, IP, dài, tên con chưa khai, URI tuyệt đối lạ | 403 của vhost mặc định | |

### 2.3 Hoa thường, chấm cuối, punycode, Host lạ: bộ định tuyến R01–R14 (bộ dựng lại)

Bộ R01–R14 của rà soát mục 3 không có trong repo (bản chép bỏ khi repo thành công khai). Đây là **bộ dựng lại** cho tên phụ, cùng tên R01–R14, đo ở t1 (nginx) và t2 (Apache). e2e bước 22 chạy lại đúng bộ này trên cả hai stack.

| # | Request | nginx | Apache | Kết quả mong đợi |
|---|---|---|---|---|
| R01 | `Host: WWW.EXAMPLE.COM` | 301 về tên chính | 301 | như chữ thường |
| R02 | `Host: XN--VITNAM-JK8B.EXAMPLE.NET` | 200 | 200 | như chữ thường |
| R03 | `Host: www.example.com.` | 301 | 301 | như không có chấm |
| R04 | `Host: xn--vitnam-jk8b.example.net.` | 200 | 200 | như không có chấm |
| R05 | `Host: www.example.com:80` | 301 | 301 | cổng bị bỏ qua |
| R06 | `Host: www.example.com:8080` | 301 | 301 | cổng bị bỏ qua |
| R07 | `Host: việtnam.example.net` (UTF-8 thô) | đóng kết nối | 400 | không tới site |
| R08 | `Host: evil.example.org` | đóng kết nối | 403 | vhost mặc định |
| R09 | `Host: 127.0.0.1` | đóng kết nối | 403 | vhost mặc định |
| R10 | `Host:` rỗng | 400 | 400 | |
| R11 | Host 262 ký tự | đóng kết nối | 403 | vhost mặc định |
| R12 | `Host: x.www.example.com` (tên con chưa khai) | đóng kết nối | 403 | không có tên đại diện |
| R13 | dòng yêu cầu `GET http://evil.example.org/`, `Host: example.com` | đóng kết nối | 403 | tên ở dòng yêu cầu thắng Host |
| R14 | dòng yêu cầu `GET http://www.example.com/x`, `Host: evil.example.org` | 301 về tên chính | 301 | như R13: chọn theo dòng yêu cầu |

Ở phía nhập liệu, `ParseDomain` của tên chính được dùng nguyên cho tên phụ (t4): `WWW.Example.COM` thành `www.example.com`; `việtnam.example.net` thành `xn--vitnam-jk8b.example.net`; `www.example.com.` bị từ chối (cùng luật với tên chính); IP, tên rỗng, tên quá 253 ký tự bị từ chối như cũ. **Kết luận: tên phụ đi đúng đường của tên chính, không cần luật nhập riêng.**

### 2.4 Chứng chỉ nhiều tên với certbot và pebble (t3)

TPAN xin chứng chỉ bằng `certbot certonly --webroot -w /var/lib/tpan/acme -d <tên> --cert-name <tên> --non-interactive --agree-tos -m <email> --server <url>` (`internal/steps/ssl.go`, `CertbotCert.cmd`). Script `t3-pebble.sh` gọi đúng lệnh đó với nhiều `-d`, output `t3-pebble.out`:

| Ca | Lệnh | Kết quả |
|---|---|---|
| C1 | `-d example.com` | mã 0, SAN `example.com` |
| C2 | thêm `-d www.example.com`, cùng `--cert-name` | mã 0, "Renewing an existing certificate", SAN hai tên. **Không cần `--expand`** khi đã có `--cert-name` |
| C3 | thêm `-d nodns.example.invalid` (không phân giải được) | **mã 1**, "Some challenges have failed", CA trả `Type: connection ... no such host`. **Chứng chỉ cũ giữ nguyên** (cùng serial `33D187EB...`) |
| C3b | thêm `-d alias.example.net` (trỏ về máy) | mã 0, SAN ba tên |
| C4 | bớt về hai tên | mã 0, SAN hai tên (xin lại khi bớt tên chạy được) |
| C4b | chạy lại đúng bộ tên | mã 0, "Certificate not yet due for renewal; no action taken" |
| C5 | 101 tên | mã 0: **pebble không giữ trần**, nên TPAN phải tự giữ |

Hệ quả: một tên không đạt làm **cả đơn** hỏng (không có chuyện "xin được phần đạt"); vì vậy TPAN tự thử đường HTTP của **từng tên** trước (như `ACMEProbe` đang làm cho tên chính) và chỉ đưa tên đạt vào lệnh certbot. Đơn hỏng không đụng chứng chỉ đang chạy. Lưu ý gia hạn: `certbot renew` xin lại đúng bộ tên đang có trong chứng chỉ, nên một tên phụ bị gỡ hoặc đổi DNS mà vẫn nằm trong chứng chỉ sẽ làm **lần gia hạn của cả site hỏng**. Thiết kế xin lại khi gỡ tên và cảnh báo trước (mục 3.6, 3.8).

**Bố cục cổng 443 của mục 3.4, Apache (t5)**: script `t5-apache-443.sh`, vhost thử `t5-apache-443.conf` (rút gọn, không PHP; chứng chỉ tự ký có SAN `example.com`, `www.example.com`, `xn--vitnam-jk8b.example.net`; `uncovered.example.net` cố ý không có), output `t5-apache-443.out`. `apachectl -t` `Syntax OK`; `apache2ctl -S` thấy đúng ba vhost 443 và hai vhost chuyển cổng 80. Kết quả: tên chính và tên phụ `serve` có phủ qua https 200 (T01, T02); tên phụ `redirect` có phủ qua https 301 `https://example.com/x?y=1` (T03); tên chưa phủ qua https rơi vào vhost mặc định 443, 403 (T04); cổng 80: tên `serve` có phủ 301 `https://<chính tên đó>/a?b=1` (T05), tên chưa phủ và tên `redirect` 301 `https://example.com/a` (T06, T07); thử thách ACME 200 trên cả ba khối cổng 80 (T08–T10).

**Chưa thử trên máy ảo** (hết giờ đợt 0): cùng bố cục 443 cho **nginx** (khối `server` 443 chỉ chuyển tên phụ, khối 80 riêng cho từng tên `serve` có phủ). Chỉ thị giống t1 (`listen 443 ssl`, `ssl_certificate`, `return 301`), chỉ khác bố cục. Thợ 1b chạy trên máy ảo (`nginx -t`, `curl -k --resolve` cho đủ T01–T10 như t5, tên chưa phủ phải rơi vào `tpan-default-ssl` do Plan SSL ghi) và lưu `docs/bang-chung/M19-nghien-cuu/t5-nginx-443.out` trước khi đợt 1 đóng.

**Trần của Let's Encrypt** (tra ngày 25/11/2026): trang Rate Limits (cập nhật 05/08/2026) ghi "A single certificate can include up to 100 identifiers ... depending on the certificate profile"; trang Profiles (cập nhật 04/10/2026): `classic` (mặc định) 100 tên, `tlsserver` và `shortlived` 25 tên. Các giới hạn liên quan: 50 chứng chỉ mỗi tên miền đăng ký mỗi 7 ngày; 5 chứng chỉ cho cùng một bộ tên mỗi 7 ngày; 5 lần xác minh hỏng mỗi tên mỗi tài khoản mỗi giờ; 300 đơn mới mỗi tài khoản mỗi 3 giờ. **Chốt: trần 25 tên mỗi site (tên chính + 24 tên phụ)**, vừa mọi profile, kể cả khi Let's Encrypt đổi profile mặc định.

### 2.5 Public Suffix List (t4)

`go.mod` có `golang.org/x/net v0.63.0`; gói `golang.org/x/net/publicsuffix` nhúng bảng sinh từ `public_suffix_list.dat` **git revision d6c92f1bbb74 (2026-02-06T07:36:33Z)** (hằng `version` trong `table.go`), ba file dữ liệu `data/nodes`, `data/children`, `data/text` tổng 88 743 byte. Không thêm dependency. **Cập nhật**: bảng chỉ đổi khi nâng `golang.org/x/net` (phiên chính sửa `go.mod`); bản PSL in ra được bằng `go run` (t4 in dòng `version`). PSL đổi chậm và chủ yếu thêm hậu tố mới; hậu tố mới chưa có trong bảng chỉ làm TPAN coi vài tên là "cùng tên miền gốc" (chặt hơn, không lỏng hơn) cho tới lần nâng sau.

Output `t4-psl.out` (`go run ./docs/bang-chung/M19-nghien-cuu/_t4psl`):

- `www.example.com`, `a.b.example.com` → `example.com`; `shop.example.co.uk` → `example.co.uk`; `www.example.com.vn` → `example.com.vn`; `www.tênmiền.vn` → `xn--tnmin-hsa0954c.vn`.
- Phần private: `alice.github.io` và `bob.github.io` là **hai** tên miền gốc khác nhau (`github.io` là hậu tố private, icann=false). Dùng cả phần private là đúng nghĩa "ai kiểm soát tên này": hai người thuê hai tên dưới một hậu tố private là hai chủ khác nhau. Chỉ dùng phần ICANN sẽ gộp họ vào `github.io` và chặn nhầm người thứ hai; lỗi theo hướng chặt, nhưng sai. **Chốt: dùng `EffectiveTLDPlusOne` (cả hai phần).**
- TLD lạ hoặc dành riêng (`example.test`, `example.invalid`, `example.cromulent`): rơi vào luật mặc định `*`, tên miền gốc là hai nhãn cuối. Đúng cho máy ảo.
- `co.uk`, `com.vn`, `github.io` (chính là hậu tố công cộng): `EffectiveTLDPlusOne` báo lỗi. **Chốt: từ chối làm tên chính hay tên phụ** (mã `domain_public_suffix`), vì ai giữ được `com.vn` thì về lý "sở hữu" mọi tên dưới nó.

---

## 3. Thiết kế

### 3.1 Dữ liệu: migration `internal/store/migrations/1900_site_names.sql`

```sql
-- Bản 1900: tên của site (tên chính, tên phụ) và luật chuyển hướng (M19,
-- docs/thay-doi/M19-thiet-ke.md mục 3.1). Khoảng 1900-1999 là của M19.

CREATE TABLE site_names (
    -- Đã qua validate.ParseDomain: chữ thường, punycode. PRIMARY KEY nên một tên
    -- chỉ thuộc một site trên toàn máy, kể cả tên chính của site khác (B2).
    name       TEXT PRIMARY KEY,
    -- Tên chính của site giữ tên này (sites.domain). Không khoá ngoại: tên chính
    -- của site đang tạo được giữ chỗ (pending) trước khi có dòng trong sites.
    site       TEXT NOT NULL,
    -- Chủ của site lúc giữ chỗ (TPAN không có thao tác đổi chủ site).
    owner      TEXT NOT NULL,
    kind       TEXT NOT NULL CHECK (kind IN ('primary', 'alias')),
    -- serve: phục vụ cùng nội dung; redirect: 301 về tên chính.
    mode       TEXT NOT NULL DEFAULT 'serve' CHECK (mode IN ('serve', 'redirect')),
    -- pending: đã giữ chỗ, job của nó đang chạy; vhost chỉ vẽ tên active,
    -- trừ đúng job đang thêm tên đó.
    state      TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('pending', 'active')),
    -- 1: tên có trong SAN của chứng chỉ đang dùng (ghi sau mỗi job site.ssl
    -- thành công, đọc từ cert.pem). Vhost vẽ cổng 443 theo cột này, nên vhost
    -- vẫn chỉ là hàm của DB; health ssl.names so cột với file chứng chỉ.
    in_cert    INTEGER NOT NULL DEFAULT 0 CHECK (in_cert IN (0, 1)),
    created_at TEXT NOT NULL,
    CHECK (kind = 'alias' OR (name = site AND mode = 'serve'))
) STRICT;
CREATE INDEX site_names_site ON site_names (site);

-- Mỗi site đang có (cả trong thùng rác) giữ đúng tên chính của nó, không thêm
-- tên phụ nào (B5). Site có SSL: tên chính đang có trong chứng chỉ.
INSERT INTO site_names (name, site, owner, kind, mode, state, in_cert, created_at)
    SELECT domain, domain, owner, 'primary', 'serve', 'active', ssl, created_at FROM sites;

CREATE TABLE site_redirects (
    id         INTEGER PRIMARY KEY,
    site       TEXT NOT NULL REFERENCES sites (domain) ON DELETE CASCADE,
    -- Đã qua validate.ParseRedirectSource / ParseRedirectTarget.
    source     TEXT NOT NULL,
    target     TEXT NOT NULL,
    code       INTEGER NOT NULL CHECK (code IN (301, 302)),
    keep_query INTEGER NOT NULL DEFAULT 0 CHECK (keep_query IN (0, 1)),
    state      TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('pending', 'active')),
    created_at TEXT NOT NULL,
    UNIQUE (site, source)
) STRICT;
```

Không có cột tên miền gốc: nó tính được từ `name` bằng PSL trong Go, và tính lại mỗi lần kiểm (vài trăm dòng, không đáng kể) thì nâng `golang.org/x/net` không bao giờ để lại cột cũ sai. Không có ràng buộc DB "một tên miền gốc một chủ": dữ liệu cũ có thể đã vi phạm (P4 chưa vá ở v0.2.0), migration không được hỏng vì nó; luật áp cho mọi lần giữ chỗ mới (mục 3.2) và health cảnh báo chỗ cũ (`names.root.shared`, mục 3.8).

DB mở với `_txlock=immediate` và `foreign_keys(1)` (`store.go`): transaction ghi lấy khoá ngay ở `BEGIN`, nên kiểm rồi chèn trong một transaction là tuần tự trên toàn máy.

### 3.2 Một chỗ kiểm sở hữu (dùng chung với M28)

**Luật** (một câu, nằm ở `site.CheckClaims`): một tên `n` được giữ cho site `S` của chủ `O` khi và chỉ khi (1) `n` không phải hậu tố công cộng và không phải tên dành cho phép thử (`site.IsProbeDomain`); (2) chưa có dòng nào mang tên `n`; (3) không có dòng nào (bất kể `pending` hay `active`, bất kể site nào) có tên miền gốc trùng tên miền gốc của `n` mà `owner` khác `O`. **Chạy lại tên chính**: claim `kind=primary` mà đã có dòng `primary` của chính `S` (pending hay active: `tpan site add` trên site đã có, `tpan apply` lần hai, chạy lại sau khi tiến trình chết giữa job) là **no-op**: không xét (1), (2), (3), không ghi gì. Nhờ vậy site cũ nằm trên một tên miền gốc dùng chung với chủ khác (dữ liệu trước M19, mục 6) vẫn chạy lại được. **Tên phụ không có ngoại lệ**: đã có dòng mang tên đó, kể cả của chính `S`, là từ chối (`alias_exists` khi cùng site); nên mười request song song thêm cùng một tên phụ cho cùng một site chỉ một cái qua, và `server_name` không bao giờ có tên lặp. Dòng pending mồ côi của tên phụ gỡ bằng `tpan site alias rm` (nhận cả dòng pending). Luật áp cho **chủ của site**, không cho người bấm: admin thao tác hộ trên site của B cũng không gắn được tên thuộc tên miền gốc của A vào site của B. Không có cờ vượt luật trong bản này.

**File và chữ ký:**

`internal/validate/psl.go` (thuần):

```go
// CodeDomainPublicSuffix: tên là chính một hậu tố công cộng (co.uk, com.vn, github.io).
const CodeDomainPublicSuffix = "domain_public_suffix"

// RegisteredDomain trả tên miền gốc (eTLD+1) của d theo Public Suffix List nhúng
// trong golang.org/x/net/publicsuffix, cả phần ICANN lẫn phần private.
func RegisteredDomain(d Domain) (Domain, error)
```

`internal/site/names.go` (thuần, không DB):

```go
type NameKind string // "primary" | "alias"
type NameMode string // "serve" | "redirect"
type NameState string // "pending" | "active"

const (
	MaxNames     = 25 // tên chính + 24 tên phụ: vừa profile nhỏ nhất của Let's Encrypt
	MaxRedirects = 50
)

// Name là một dòng của bảng site_names.
type Name struct {
	Name    validate.Domain
	Site    validate.Domain
	Owner   string
	Kind    NameKind
	Mode    NameMode
	State   NameState
	Created time.Time
}

// Claim là yêu cầu giữ một tên cho một site.
type Claim struct {
	Name  validate.Domain
	Site  validate.Domain
	Owner string
	Kind  NameKind
	Mode  NameMode
}

// NameError là lời từ chối của CheckClaims. Code: "domain_public_suffix",
// "domain_reserved", "alias_exists" (đã là tên phụ của chính site này), "name_taken" (tên thuộc site của chủ khác), "name_taken_self"
// (thuộc site khác của cùng chủ; Other là site đó), "name_root_foreign" (tên miền
// gốc có tên của chủ khác). Message không chép tên site hay chủ của người khác.
type NameError struct {
	Code  string
	Name  validate.Domain
	Other validate.Domain // chỉ điền khi name_taken_self
}

// CheckClaims áp luật mục 3.2 cho mọi claim trên ảnh chụp existing (mọi dòng của
// site_names). Lỗi ở claim đầu tiên vi phạm. Không đụng DB: store gọi nó bên
// trong transaction, core gọi nó cho bản xem trước.
func CheckClaims(claims []Claim, existing []Name) error

// Names là các tên phụ active và luật chuyển hướng active của một site, theo thứ
// tự (created_at, name) / (created_at, id): thứ tự cố định để vhost sinh ra không
// đổi giữa hai lần vẽ.
type Names struct {
	Serve    []validate.Domain
	Redirect []validate.Domain
	Covered  []validate.Domain // tên có in_cert = 1 (cả tên chính); chỉ bản SSL dùng
	Rules    []Redirect
}

type Redirect struct {
	ID        int64
	Source    validate.RedirectSource
	Target    validate.RedirectTarget
	Code      int // 301 | 302
	KeepQuery bool
}
```

`site.Names` có thêm `Covered []validate.Domain` (tên, kể cả tên chính, có `in_cert = 1`). `site.Site` thêm trường `Names Names \`json:"names"\``. `store.Site`, `Sites`, `SitesOf` điền nó (một truy vấn phụ mỗi bảng), chỉ dòng `active`. Nhờ vậy `core.siteVhost(st, cfg)` **giữ nguyên chữ ký**: nó đọc `st.Names`; mọi chỗ đang dựng vhost từ một `site.Site` đọc từ store tự có tên phụ, luật và bộ tên trong chứng chỉ, không phải đọc file chứng chỉ lúc dựng Plan.

**Ảnh chụp để kiểm** (`ClaimNames`, `CheckNames`): mọi dòng `site_names` **hợp với** một dòng `primary` suy từ mỗi dòng `sites` chưa có dòng của mình (`domain, domain, owner, 'primary', 'active'`). Vậy một đường nào đó tạo dòng `sites` mà quên `site_names` (demo, khôi phục, mã sau này) vẫn không để tên đó bị người khác giữ (B2). Thêm một lớp ở chỗ ghi: `store.SaveSite` chạy trong một transaction: upsert `sites` như cũ, rồi `INSERT INTO site_names (...) VALUES (<domain>, <domain>, <owner>, 'primary', 'serve', 'active', <ssl>, <created_at>) ON CONFLICT (name) DO NOTHING`, rồi đọc lại dòng của `<domain>`: `site` khác `<domain>` (tên đang là tên phụ của site khác) thì trả `ErrNameTaken` và hoàn tác cả transaction.

`internal/store/names.go`:

```go
// ClaimNames giữ chỗ (state pending) mọi tên trong cs trong MỘT transaction:
// đọc mọi dòng site_names, site.CheckClaims, rồi INSERT (hoặc giữ nguyên dòng
// pending của chính site đó). Một tên lỗi thì không giữ tên nào; lỗi là *site.NameError.
func (s *Store) ClaimNames(ctx context.Context, cs []site.Claim) error
// CheckNames: cùng luật, chỉ đọc (Prepare và ?dry_run=1).
func (s *Store) CheckNames(ctx context.Context, cs []site.Claim) error
// ActivateNames chuyển các dòng pending của site sang active.
func (s *Store) ActivateNames(ctx context.Context, siteD validate.Domain, names []validate.Domain) error
// ReleaseNames xoá các dòng PENDING của site (job hỏng). Không bao giờ xoá dòng active.
func (s *Store) ReleaseNames(ctx context.Context, siteD validate.Domain, names []validate.Domain) error
// DeleteAlias xoá một tên phụ active (sau job gỡ thành công). Tên chính: lỗi.
func (s *Store) DeleteAlias(ctx context.Context, siteD, name validate.Domain) error
func (s *Store) SetAliasMode(ctx context.Context, siteD, name validate.Domain, m site.NameMode) error
// NamesOf: mọi dòng của một site (cả pending), cho danh sách và API.
func (s *Store) NamesOf(ctx context.Context, siteD validate.Domain) ([]site.Name, error)
// NameSite tra một tên về dòng của nó (route explain, API ?host=).
func (s *Store) NameSite(ctx context.Context, name validate.Domain) (site.Name, bool, error)

// SetInCert ghi cột in_cert của mọi tên của site: 1 cho tên có trong names, 0 cho tên còn lại.
func (s *Store) SetInCert(ctx context.Context, siteD validate.Domain, names []validate.Domain) error

// Luật chuyển hướng (đợt 2, file riêng internal/store/redirects.go): cùng khuôn pending/active.
func (s *Store) AddRedirectPending(ctx context.Context, siteD validate.Domain, r site.Redirect) (int64, error) // đếm < MaxRedirects và UNIQUE trong cùng transaction
func (s *Store) ActivateRedirect(ctx context.Context, id int64) error
func (s *Store) ReleaseRedirect(ctx context.Context, id int64) error // chỉ xoá pending
func (s *Store) DeleteRedirect(ctx context.Context, siteD validate.Domain, id int64) error
func (s *Store) RedirectsOf(ctx context.Context, siteD validate.Domain) ([]site.Redirect, error)
```

`store.DeleteSite` (xoá hẳn) xoá luôn các dòng `site_names` của site trong cùng transaction (`site_redirects` đi theo `ON DELETE CASCADE`). `store.SaveSite` không đụng `site_names`.

**M28 dùng lại:** phần P4 (tạo site lấn tên miền gốc người khác) chính là `ClaimNames` với `Kind: primary`, đã gọi trong `core.PrepareAdd`/`Execute` (mục 3.5). M28 không viết hàm thứ hai.

**Không lộ thông tin:** user thường bị từ chối vì `name_taken` hoặc `name_root_foreign` đều nhận **cùng một** mã API `name_unavailable` và một câu chung ("tên này không dùng được trên máy này: đã thuộc một site khác, hoặc tên miền gốc của nó thuộc người dùng khác"), không có tên site hay tên chủ. `name_taken_self` (site khác của chính họ) nói rõ site đó. Admin nhận mã chi tiết.

### 3.3 Kiểm đầu vào luật chuyển hướng: `internal/validate/redirect.go`

```go
const CodeRedirectInvalid = "redirect_invalid"

// RedirectSource là đường dẫn nguồn đã kiểm. Chỉ ParseRedirectSource tạo được.
type RedirectSource struct{ s string }
func ParseRedirectSource(s string) (RedirectSource, error)

// RedirectTarget là đích đã kiểm: đường dẫn trên cùng site, hoặc URL http(s).
type RedirectTarget struct{ s string }
func ParseRedirectTarget(s string) (RedirectTarget, error)
func (t RedirectTarget) HasQuery() bool
func (t RedirectTarget) IsAbsolute() bool
// String, IsZero, MarshalText, UnmarshalText như Domain.
```

Luật (danh sách cho phép, không danh sách cấm; không `TrimSpace`, có khoảng trắng là từ chối):

- **Đoạn** (segment): `[A-Za-z0-9._~-]{1,}`, không bắt đầu bằng `.` (chặn `.`, `..`, `/.well-known/...`, `/.env`).
- **Nguồn**: `/` hoặc `/` + các đoạn nối bằng `/`, được phép có một `/` cuối; không `//`; dài tối đa 255 byte. Không có query, không `%`, không chữ ngoài ASCII.
- **Đích dạng đường dẫn**: như nguồn, cộng tuỳ chọn `?` + query `[A-Za-z0-9._~=&-]{1,255}`; không bắt đầu bằng `//` (URL tương đối giao thức).
- **Đích dạng URL**: `http://` hoặc `https://` (chữ thường) + host qua `ParseDomain` (lưu dạng đã chuẩn hoá; IP bị từ chối) + tuỳ chọn `:cổng` (1–65535, không số 0 đứng đầu) + tuỳ chọn đường dẫn như trên (có thể rỗng) + tuỳ chọn query như trên. Không `@`, `#`, `%`, `$`, `\`, `"`, `'`, `;`, `{`, `}`, khoảng trắng, ký tự điều khiển. Dài tối đa 1024 byte.
- Từ chối ở core (cần biết site): đích dạng đường dẫn bằng nguồn (`redirect_loop`); đích URL có host là một tên của chính site và đường dẫn bằng nguồn (`redirect_loop`); `keep_query` khi đích đã có query (`redirect_query_conflict`); nguồn đã có luật (`redirect_exists`); quá 50 luật (`redirect_limit`); mã khác 301/302 (`redirect_invalid`).

Mọi ký tự có nghĩa trong cú pháp nginx (`; { } $ # " ' \` khoảng trắng, xuống dòng) và Apache (`$N`, `%N`, `%{`, `\`, khoảng trắng, xuống dòng) nằm ngoài danh sách cho phép, nên giá trị đã kiểm chèn thẳng vào template mà không cần trích dẫn. Template còn một lớp chặn cuối (mục 3.4).

### 3.4 Vhost sinh ra (nguyên văn)

Template nhận thêm dữ liệu tên và luật; **site không có tên phụ và không có luật thì output giống hệt từng byte bản v0.2.x** (B5, test vàng). Hàm mới trong `internal/templates/m19.go`; hàm cũ giữ chữ ký và gọi hàm mới với `site.Names{}`:

```go
// SiteVhostNames như SiteVhost (templates/ssl.go) cộng tên phụ, luật chuyển hướng
// và bộ tên trong chứng chỉ (n.Covered, chỉ dùng khi ssl).
func SiteVhostNames(l site.Layout, web, php, acmeRoot string, ssl bool, mode site.HtaccessMode, n site.Names) ([]byte, error)
// Bản trong lồng (M17, templates/cage.go) và bản tạm khoá (templates/suspend.go):
func SiteVhostCagedNames(l site.Layout, web, acmeRoot string, ssl bool, mode site.HtaccessMode, n site.Names) ([]byte, error)
func SuspendedVhostNames(l site.Layout, web, acmeRoot string, ssl bool, n site.Names) ([]byte, error)
```

Ở `internal/steps`, bốn hàm dựng bước vhost thêm tham số cuối `n site.Names`: `SiteVhost` (`site_web.go`), `SiteVhostSSL` (`ssl.go:536`), `SiteVhostCaged` (`cage.go:1508`), `SiteVhostSuspended` (`suspend.go:169`). Nơi gọi truyền `st.Names` (đọc từ store); nơi chỉ có `Layout` mà không có `Site` (ví dụ `core/probe.go` cho site thử của M17) truyền `site.Names{}`.

Chia tên (hàm `splitNames` trong `m19.go`, có test bảng):

| | Không SSL | Có SSL |
|---|---|---|
| Khối nội dung (cổng 80 hoặc 443) | tên chính + `Serve` | tên chính + (`Serve` ∩ `Covered`) |
| Khối chuyển tên phụ, cổng 80, đích `<scheme>://<tên chính>` | `Redirect`, đích `http://` | `Redirect` ∪ (`Serve` \ `Covered`), đích `https://` |
| Mỗi tên `Serve` ∩ `Covered`, cổng 80 | – | một khối chuyển riêng, đích `https://<chính tên đó>` |
| Khối chuyển tên phụ, cổng 443 | – | `Redirect` ∩ `Covered`, đích `https://<tên chính>` |

Không dùng `$host` hay `%{HTTP_HOST}`: mọi đích là tên cố định đã kiểm. Tên phụ chưa có trong chứng chỉ không bao giờ nằm trên cổng 443 (trình duyệt sẽ báo lỗi chứng chỉ); ở cổng 80 nó chuyển về `https://<tên chính>`, chứng chỉ của tên chính hợp lệ.

Lớp chặn cuối trong template (như `siteCheckPath`): mọi tên phải khớp `^[a-z0-9.-]{1,253}$`, mọi nguồn và đích phải khớp `^[A-Za-z0-9._~/:?=&-]{1,1024}$`; không khớp là `ErrParam` (lỗi lập trình, không bao giờ do người dùng).

**nginx, khối nội dung** (`site-nginx.conf.tmpl` và khối 443 của `site-nginx-ssl.conf.tmpl`, cùng hai bản lồng): dòng `server_name` thành

```
    server_name {{.Domain}}{{range .ContentAliases}} {{.}}{{end}};
```

và ngay sau khối `location ^~ /.well-known/acme-challenge/` (bản 443: ngay trước "Các location regex được xét theo thứ tự") gọi `{{- template "site-nginx-redirects.tmpl" .}}`. Output với hai luật:

```
    # Chuyển hướng theo đường dẫn (M19, tpan site redirect). Mỗi luật là một
    # location khớp đúng (=): nginx xét nó trước mọi location khác, so với đường
    # dẫn đã giải mã, không gồm query. Nguồn không bao giờ bắt đầu bằng "/." nên
    # không che được thử thách ACME hay luật chặn file ẩn.
    location = /old-page {
        return 301 /new-page;
    }
    location = /keepq {
        return 301 https://example.net/target$is_args$args;
    }
```

**nginx, khối chuyển tên phụ** (template con `site-nginx-name-redirect.tmpl`, đặt sau khối `server` cuối cùng của file, mỗi lần gọi một khối):

```
# Tên phụ chuyển về https://example.com (M19, tpan site alias). Thử thách Let's
# Encrypt vẫn phục vụ thẳng, để xin được chứng chỉ có cả các tên này.
server {
    listen 80;
    listen [::]:80;
    server_name www.example.com old.example.net;

    access_log /var/log/tpan/sites/example.com/access.log;
    error_log /var/log/tpan/sites/example.com/error.log;

    location ^~ /.well-known/acme-challenge/ {
        root /var/lib/tpan/acme;
    }

    location / {
        return 301 https://example.com$request_uri;
    }
}
```

**nginx, khối chuyển tên phụ cổng 443** (`site-nginx-name-redirect-ssl.tmpl`):

```
server {
    listen 443 ssl;
    listen [::]:443 ssl;
    server_name www.example.com;

    ssl_certificate /etc/letsencrypt/live/example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/example.com/privkey.pem;

    access_log /var/log/tpan/sites/example.com/access.log;
    error_log /var/log/tpan/sites/example.com/error.log;

    location / {
        return 301 https://example.com$request_uri;
    }
}
```

**Apache, khối nội dung**: sau `ServerName {{.Domain}}` thêm `{{- if .ContentAliases}}` + dòng `    ServerAlias {{join .ContentAliases " "}}`; ngay sau dòng `LimitRequestBody 67108864` gọi `{{- template "site-apache-redirects.tmpl" .}}`:

```
    # Chuyển hướng theo đường dẫn (M19, tpan site redirect). So chuỗi (=), không
    # regex; chạy trước .htaccess của site. QSD bỏ query, QSA giữ query.
    RewriteEngine On
    RewriteCond %{REQUEST_URI} =/old-page
    RewriteRule ^ /new-page [R=301,L,QSD]
    RewriteCond %{REQUEST_URI} =/keepq
    RewriteRule ^ https://example.net/target [R=301,L,QSA]
```

**Apache, vhost chuyển tên phụ** (`site-apache-name-redirect.tmpl`, cùng khuôn với khối cổng 80 của bản SSL đã soát ở M5/M28):

```
# Tên phụ chuyển về https://example.com (M19, tpan site alias).
<VirtualHost *:80>
    ServerName www.example.com
    ServerAlias old.example.net
    DocumentRoot "/var/www/example.com/public"

    ErrorLog "/var/log/tpan/sites/example.com/error.log"
    CustomLog "/var/log/tpan/sites/example.com/access.log" combined

    Alias /.well-known/acme-challenge/ "/var/lib/tpan/acme/.well-known/acme-challenge/"
    <Directory "/var/lib/tpan/acme/.well-known/acme-challenge">
        Options None
        AllowOverride None
        Require all granted
    </Directory>

    <Directory "/var/www/example.com">
        Options SymLinksIfOwnerMatch
        AllowOverride None
    </Directory>

    RewriteEngine On
    RewriteCond %{REQUEST_URI} !^/\.well-known/acme-challenge/
    RewriteRule ^/(.*)$ https://example.com/$1 [R=301,L]
</VirtualHost>
```

Bản 443 (`site-apache-name-redirect-ssl.tmpl`): `<VirtualHost *:443>`, `ServerName`/`ServerAlias` như trên, `DocumentRoot` và khối `<Directory "{{.Dir}}">` như trên, `SSLEngine on`, `SSLCertificateFile`, `SSLCertificateKeyFile` như vhost 443 của site, hai dòng log, rồi `RewriteEngine On` và `RewriteRule ^/(.*)$ https://example.com/$1 [R=301,L]` (không cần loại trừ thử thách: thử thách đi cổng 80).

**Bản tạm khoá**: `server_name`/`ServerName`+`ServerAlias` của bản tạm khoá liệt kê tên chính + mọi tên phụ active (cổng 443: chỉ tên có trong chứng chỉ); không vẽ khối chuyển hay luật nào: mọi tên trả trang tạm khoá 403.

### 3.5 Các Plan

Chung: Prepare chỉ đọc (từ chối sớm, dùng `store.CheckNames`); `?dry_run=1` chỉ gọi Prepare + Preview nên **không bao giờ giữ chỗ**. `Execute` giữ chỗ (pending) **trước** khi chạy engine, chạy job, rồi `Activate…` khi job thành công hoặc `Release…` khi job hỏng hay không chạy được. Tiến trình chết giữa chừng để lại dòng pending: chạy lại đúng lệnh trên đúng site là nhận lại (luật (2) mục 3.2); hoặc `tpan site alias rm` gỡ nó. Vhost của mọi job vẽ từ `st.Names` (chỉ active) cộng đúng thay đổi của job đó, nên job khác chạy chen không vẽ tên đang pending.

Mọi Plan dưới đây có kind riêng, đích là tên chính, chủ của job là chủ site; kiểm chứng sau reload thử lại tối đa 5 lần cách 1 giây (mục 2.1).

Kiểu và chữ ký ở `internal/core/types_m19.go` (phiên chính ghi trước đợt 1, thân trả lỗi "chưa làm", để thợ 1c và 1d cùng biên dịch được):

```go
// NamesOp là một thay đổi tên phụ hay luật chuyển hướng đã qua điều kiện đầu.
type NamesOp struct{ /* trường không xuất: sites, site, plan, claim, redirect id */ }

func (o *NamesOp) Preview(ctx context.Context) (engine.Preview, error)
// Execute: giữ chỗ (nếu cần), chạy job, activate hoặc release. error chỉ khác nil
// khi không chạy được hay không ghi được DB; job hỏng thì xem Report.Job.Status.
func (o *NamesOp) Execute(ctx context.Context, onEvent func(engine.Event)) (NamesResult, error)

type NamesResult struct {
	Report    engine.Report `json:"report"`
	NeedsCert bool          `json:"needs_cert"` // site SSL và bộ tên muốn có khác bộ tên trong chứng chỉ
	Notes     []i18n.Text   `json:"notes,omitempty"`
}

type RedirectInput struct {
	Source, Target string
	Code           int
	KeepQuery      bool
}

// NameView là một dòng của GET .../names và tpan site alias list.
type NameView struct {
	Name   string         `json:"name"`
	Kind   site.NameKind  `json:"kind"`
	Mode   site.NameMode  `json:"mode"`
	State  site.NameState `json:"state"`
	DNS    string         `json:"dns"` // "ok" | "warn" | "missing" (getent lúc gọi)
	InCert bool           `json:"in_cert"`
}

func (s *Sites) PrepareAliasAdd(ctx context.Context, siteD, name validate.Domain, m site.NameMode) (*NamesOp, error)
func (s *Sites) PrepareAliasMode(ctx context.Context, siteD, name validate.Domain, m site.NameMode) (*NamesOp, error)
func (s *Sites) PrepareAliasRemove(ctx context.Context, siteD, name validate.Domain) (*NamesOp, error)
func (s *Sites) PrepareRedirectAdd(ctx context.Context, siteD validate.Domain, in RedirectInput) (*NamesOp, error)
func (s *Sites) PrepareRedirectRemove(ctx context.Context, siteD validate.Domain, id int64) (*NamesOp, error)
func (s *Sites) NameList(ctx context.Context, siteD validate.Domain) ([]NameView, error)
func (s *Sites) RedirectList(ctx context.Context, siteD validate.Domain) ([]site.Redirect, error)
func (s *Sites) RouteExplainHost(ctx context.Context, host validate.Domain) (RouteTrace, error)
```

**Giao diện store của core.** `core.Sites.Store` là interface `SiteStore` (`internal/core/types.go:59`) mà nhiều bản giả trong test cài; không thêm hàm vào đó. Thay vào đó `types_m19.go` khai interface riêng:

```go
// NameStore là phần store giữ tên và luật (mục 3.2). *store.Store cài nó;
// cmd/tpan/main.go có dòng kiểm lúc biên dịch: var _ core.NameStore = (*store.Store)(nil).
type NameStore interface {
	ClaimNames(ctx context.Context, cs []site.Claim) error
	CheckNames(ctx context.Context, cs []site.Claim) error
	ActivateNames(ctx context.Context, siteD validate.Domain, names []validate.Domain) error
	ReleaseNames(ctx context.Context, siteD validate.Domain, names []validate.Domain) error
	DeleteAlias(ctx context.Context, siteD, name validate.Domain) error
	SetAliasMode(ctx context.Context, siteD, name validate.Domain, m site.NameMode) error
	SetInCert(ctx context.Context, siteD validate.Domain, names []validate.Domain) error
	NamesOf(ctx context.Context, siteD validate.Domain) ([]site.Name, error)
	NameSite(ctx context.Context, name validate.Domain) (site.Name, bool, error)
}
```

Core lấy nó bằng `func (s *Sites) nameStore() (NameStore, bool)` (ép kiểu `s.Store.(NameStore)`). Store giả không cài nó (test cũ): `site add` kiểm `site.CheckClaims` trên ảnh chụp dựng từ `s.Store.Sites()` và không ghi gì; mọi lệnh tên phụ và luật từ chối "store không giữ được tên". Bản chạy thật luôn có `*store.Store`, và dòng kiểm lúc biên dịch giữ điều đó. Đợt 2 thêm `RedirectStore` cùng khuôn (`AddRedirectPending`, `ActivateRedirect`, `ReleaseRedirect`, `DeleteRedirect`, `RedirectsOf`).

Lời từ chối dùng `refuse(code, i18n.T(...))` như các Plan khác; core dịch `*site.NameError` ra mã ở mục 3.7 (user thường: `name_unavailable`).

**`site.alias.add`** (`core.(*Sites).PrepareAliasAdd`): điều kiện đầu: site có, `active`, không tạm khoá (`site_suspended`), tên khác tên chính (`alias_primary`), số tên < 25 (`alias_limit`), `CheckNames`. Bước: (1) các bước vhost của `siteVhost(st', cfg)` với `st'.Names` có thêm tên mới (chưa có trong chứng chỉ, nên ở site SSL nó chỉ nằm ở cổng 80) (bước `File`: ghi, `nginx -t`/`apachectl -t`, reload; hoàn tác ghi lại nội dung cũ rồi reload); (2) `steps.NameProbe{Layout, Host, Mode, SSL, Covered}`: `curl --resolve <host>:<cổng>:127.0.0.1` tới `http(s)://<host>/`: `serve` → 200 và dấu hiệu WordPress như `HTTPSite`; `redirect` (hoặc tên chưa có trong chứng chỉ ở site SSL, gọi cổng 80) → 301 và `Location` đúng bằng `<scheme>://<tên chính>/`. Hỏng: engine hoàn tác (1). Sau job thành công và site có SSL: kết quả mang `NeedsCert: true`, CLI/API chạy tiếp job SSL (mục 3.6). Job SSL hỏng **không** gỡ tên phụ.

**`site.alias.mode`**: như trên, không giữ chỗ; xong thì `SetAliasMode`.

**`site.alias.rm`**: điều kiện: tên là tên phụ của site (tên chính: `alias_primary`). Bước: vhost không có tên đó; kiểm chứng tên chính vẫn đạt (`HTTPSite`/`HTTPSSite`) và tên vừa gỡ rơi vào vhost mặc định (`steps.NameGone`: nginx đóng kết nối, Apache 403). Xong thì `DeleteAlias`. Tên đó đang có trong chứng chỉ → `NeedsCert: true` để xin lại không có nó (lý do ở mục 2.4: gia hạn xin lại đúng bộ tên cũ).

**`site.redirect.add`**: điều kiện: luật của mục 3.3, site active, không tạm khoá. Execute: `AddRedirectPending` (đếm và UNIQUE trong transaction) → job: vhost có thêm luật; `steps.RedirectProbe{Layout, SSL, Source, Code, Location}`: gọi `<scheme>://<tên chính><nguồn>`, mong mã đúng và `Location` đúng (đích đường dẫn thành `<scheme>://<tên chính><đích>`, như P01) → `ActivateRedirect` hoặc `ReleaseRedirect`.

**`site.redirect.rm`**: vhost không có luật; kiểm chứng tên chính đạt; xong thì `DeleteRedirect`.

**`site.add`** (sửa `core.PrepareAdd`/`SiteAdd.Execute` trong `internal/core/site.go`): `SiteInput` thêm `WWW site.NameMode` hoặc `"none"` (mặc định `redirect`). Tên phụ mặc định chỉ có khi tên chính là tên miền gốc (`example.com` → `www.example.com`) hoặc là `www.` + tên miền gốc (`www.example.com` → `example.com`, đây là cách chọn "ngược lại"); tên con sâu hơn (`blog.example.com`) không có. Prepare: `CheckNames` cho tên chính (kind primary) và tên `www`. Tên `www` đã thuộc site khác của cùng chủ thì bỏ qua (ghi chú trong kết quả), không từ chối. Execute: `ClaimNames` cả hai → job (vhost có tên `www`, kiểm chứng thêm `NameProbe` cho nó) → thành công: `SaveSite` rồi `ActivateNames`; hỏng: `ReleaseNames`. **Chạy lại** `site add` trên site đã có: không giữ chỗ gì mới (dòng `primary` của chính site đã có, luật (2) mục 3.2) và không thêm tên phụ nào (B5); Plan dùng `st.Names` đang có. `tpan apply` (`internal/core/apply.go`) đi đúng đường này: giữ chỗ tên chính của mọi site mới trong lô trước khi chạy lô (site đã có: không giữ gì), nhả tên của site hỏng; `apply` không thêm tên `www` mặc định (file khai báo nói đủ trạng thái muốn có).

**Xoá, thùng rác, khôi phục, đổi stack, lồng, tạm khoá, sửa health "ghi lại vhost", PHP, áp bản vá cô lập**: không đổi luồng. `core.siteVhost(st, cfg)` giữ chữ ký và truyền `st.Names` xuống bốn hàm của `steps` (mục 3.4). Các file đang gọi thẳng hàm dựng vhost của `steps` hay `templates` (đếm bằng `grep -rn "siteVhost(\|SiteVhostSSL(\|SiteVhostCaged(\|SiteVhostSuspended(\|steps\.SiteVhost(" internal`): `internal/core/{cage,fix,harden_m14,health,php,probe,site,ssl,stack_switch,suspend,trash}.go`, `internal/demo/ssl.go`, `internal/steps/{site_web,ssl,suspend,cage}.go`; mỗi chỗ có `site.Site` thì truyền `st.Names`, không có thì `site.Names{}`. Site trong thùng rác vẫn giữ tên (không ai lấy được tên của site đang chờ khôi phục); xoá hẳn thì nhả.

### 3.6 Chứng chỉ SAN (sửa `internal/steps/ssl.go`, `internal/core/ssl.go`)

Đơn của job `site.ssl` (kind có sẵn ở `internal/core/ssl.go`; cùng `core.(*SSL).PrepareIssue`, `tpan ssl issue <domain>`, `POST /api/v1/sites/{domain}/ssl`):

1. Tên muốn có: tên chính + mọi tên phụ active (cả `redirect`: `https://www…` cũng phải mở được). Trần 25 đã giữ ở bước thêm tên.
2. Điều kiện đầu DNS như cũ cho tên chính (`dns_not_here` vẫn chặn); tên phụ chỉ ghi nhận.
3. `steps.ACMEProbeNames{Layout, Names, Result *ProbeResult}`: thử đường HTTP của **từng** tên như `ACMEProbe` (file ngẫu nhiên trong webroot, `curl` qua chính tên đó). Tên chính hỏng: bước hỏng (như hôm nay). Tên phụ hỏng: ghi vào `Result.Failed` kèm lý do, bước vẫn đạt.
4. `steps.CertbotCert` thêm `Names func() []validate.Domain` (đọc `Result.Passed`, tên chính đứng đầu; nil thì chỉ tên chính như cũ): lệnh `certbot certonly --webroot -w <webroot> --cert-name <tên chính> -d <tên chính> -d <tên phụ>... --non-interactive --agree-tos -m <email> --server <url>` (không cần `--expand`, bản thử C2). `Check`: có `fullchain.pem`, đúng máy chủ ACME **và** SAN của `cert.pem` đúng bằng bộ tên **đã qua bước thử** (`Result.Passed`, so tập, không so thứ tự) → bỏ qua. So với bộ tên muốn có thì một tên phụ chưa trỏ DNS làm bước này "lệch" mãi. Bản xem trước chưa biết tên nào sẽ qua (bước thử chạy lúc áp dụng), nên Preview ghi argv với mọi tên muốn có và một dòng "tên nào không qua bước thử sẽ bị bỏ khỏi lệnh". Undo không xoá chứng chỉ (như cũ).
5. Sau job thành công: core đọc SAN của `cert.pem` mới (`crypto/x509`), ghi cột `in_cert` của mọi tên của site (`store.SetInCert(ctx, siteD, names []validate.Domain)`), và nếu bộ `Covered` đổi thì chạy tiếp một job `site.vhost` (vhost vẽ lại từ DB + `NameProbe` cho từng tên mới được phủ). Job này hỏng thì vhost về như cũ, chứng chỉ mới vẫn hợp lệ cho các tên cũ (cột `in_cert` đã đúng với file; health `site.alias.http` cảnh báo tên chưa phục vụ trên 443).
6. Kết quả trả `names: [{name, in_cert, reason}]` cho giao diện ("chưa đạt: không phân giải được tên", "chưa đạt: gọi http://… không về máy này").

Tên phụ chưa trỏ DNS: bước 3 loại nó ra, certbot không thấy nó, chứng chỉ cũ không đụng tới (B4). certbot vẫn hỏng vì lý do khác (CAA, hạn mức): bước 4 hỏng, job dừng trước khi đụng vhost, chứng chỉ cũ giữ nguyên (bản thử C3).

**Hạn mức tài khoản**: TPAN dùng một tài khoản ACME cho mọi site, và "300 đơn mỗi 3 giờ" là của tài khoản. Để một user bật tắt tên phụ liên tục không làm cạn hạn mức của mọi người: job `site.ssl` **tự chạy tiếp sau một thay đổi tên phụ** (`NeedsCert`) chỉ chạy khi site có dưới **5 job `site.ssl` trong 24 giờ** (đếm trong bảng `jobs` theo `kind` và `target`, cả thành công lẫn hỏng; hàm mới `store.CountJobs(ctx, kind, target string, since time.Time) (int, error)` trong `internal/store/jobs_m18.go`). Quá thì không chạy, kết quả mang ghi chú `ssl_rate_limited` (nói giờ thử lại); tên phụ vẫn thêm hay gỡ được, health `ssl.names` cảnh báo. `tpan ssl issue` và `POST .../ssl` gọi tay **không** bị giới hạn này (giữ hành vi M5; e2e M5 gọi `ssl issue` 14 lần trên hai tên).

### 3.7 API (`internal/server/api_m19.go`, `types_m19.go`, dòng mới trong `routes.go`)

Mọi POST/DELETE dùng khuôn `planJob` sẵn có (`?dry_run=1` trả bản xem trước 200, không thì 202 kèm job). Luật `ownerSite`: admin qua; user thường phải là chủ site, không thì 404 như site không có; phiên "xem như" bị 403 `read_only_view` ở mọi request ghi.

| Route | Luật | Thân | Trả |
|---|---|---|---|
| `GET /api/v1/sites/{domain}/names` | owner:site | | `[{name, kind, mode, state, dns: "ok"\|"warn"\|"missing", in_cert: bool}]` (DNS và chứng chỉ đọc lúc gọi) |
| `POST /api/v1/sites/{domain}/names[?dry_run=1]` | owner:site | `{"name": "www.example.com", "mode": "redirect"}` | job `site.alias.add` (+ job SSL nếu cần) |
| `POST /api/v1/sites/{domain}/names/{name}/mode[?dry_run=1]` | owner:site | `{"mode": "serve"}` | job `site.alias.mode` |
| `DELETE /api/v1/sites/{domain}/names/{name}[?dry_run=1]` | owner:site | | job `site.alias.rm` (+ job SSL) |
| `GET /api/v1/sites/{domain}/redirects` | owner:site | | `[{id, source, target, code, keep_query, state}]` |
| `POST /api/v1/sites/{domain}/redirects[?dry_run=1]` | owner:site | `{"source": "/old", "target": "/new", "code": 301, "keep_query": false}` | job `site.redirect.add` |
| `DELETE /api/v1/sites/{domain}/redirects/{id}[?dry_run=1]` | owner:site | | job `site.redirect.rm` |
| `GET /api/v1/domains` | any | | site trong phạm vi người xem: `[{site, names: [...], redirects: <số luật>}]` (màn "Tên miền") |
| `GET /api/v1/sites/{domain}/route?host=<tên>` | owner:site (đã có) | | thêm `host`: tên phải thuộc `{domain}`, không thì 404 `name_unknown` |
| `POST /api/v1/sites` (đã có) | | thêm `"www": "redirect"\|"serve"\|"none"` | |

Mã lỗi: `domain_invalid` 400, `domain_public_suffix` 400, `domain_reserved` 400, `name_unavailable` 409 (user thường; admin thấy `name_taken`/`name_root_foreign`), `name_taken_self` 409, `alias_exists` 409, `alias_limit` 409, `alias_primary` 400, `alias_not_found` 404, `site_suspended` 409 (mã có sẵn), `redirect_invalid` 400 (kèm `field`: `source`\|`target`\|`code`), `redirect_exists` 409, `redirect_limit` 409, `redirect_loop` 400, `redirect_query_conflict` 400, `redirect_not_found` 404, `name_unknown` 404. `ssl_rate_limited` không phải mã lỗi HTTP: là ghi chú trong `notes` của kết quả khi job SSL tự chạy tiếp bị bỏ (mục 3.6). Bảng test ba vai (`authz_test.go`) thêm mọi dòng trên: admin, chủ, user khác (404), phiên xem như (403 với POST/DELETE).

### 3.8 CLI, health, route explain, MCP

**CLI** (`cmd/tpan/site_m19.go`; `--dry-run`, `--yes`, `--json` như các lệnh Plan khác):

```
tpan site alias add <domain> <name> [--mode serve|redirect]   (mặc định redirect)
tpan site alias set <domain> <name> --mode serve|redirect
tpan site alias rm <domain> <name>
tpan site alias list <domain> [--json]
tpan site redirect add <domain> <source> <target> [--code 301|302] [--keep-query]
tpan site redirect rm <domain> <id>
tpan site redirect list <domain> [--json]
tpan site add <domain> ... [--www redirect|serve|none]          (mặc định redirect)
tpan route explain <host>                                        (nhận cả tên phụ)
```

**Health** (`internal/checks/m19.go`, nối vào `core.siteChecks`):

| Mã | Mức tối đa | Đạt khi | Sửa |
|---|---|---|---|
| `site.alias.dns:<tên>` | **cảnh báo** (bọc `checks.Soft`, không bao giờ đỏ) | bản ghi A/AAAA của tên phụ trỏ về máy (cùng thân với `DNSPointsHere`, tách hàm `dnsPointsHere(b, dom)`) | lời hướng dẫn tạo bản ghi A |
| `site.alias.http:<tên>` | **cảnh báo** | `curl --resolve` tên phụ về 127.0.0.1 cho đúng kết quả như `NameProbe` | `tpan site alias set …` (vẽ lại vhost) |
| `ssl.names:<tên chính>` (site SSL) | **cảnh báo** | mọi tên muốn có mà DNS trỏ về máy đều có trong SAN, và mọi tên trong SAN vẫn là tên của site và trỏ về máy (không thì lần gia hạn sẽ hỏng) | `tpan ssl issue <domain>`; không sửa tự động (tốn hạn mức) |
| `names.root.shared` (bộ của stack) | **cảnh báo** | không tên miền gốc nào có tên của hai chủ (dữ liệu từ trước M19) | gỡ tay; nêu tên miền gốc và các site |
| `web.server_name.unique` (đã có) | lỗi (như cũ) | như M14, nhưng tập tên của mỗi site là tên chính + tên phụ active | như cũ |

`checks.ServerNameUnique(web string, sites []site.Layout)` đổi thành `ServerNameUnique(web string, sites []checks.NamedSite)` với `type NamedSite struct { Layout site.Layout; Names []validate.Domain }`.

**Route explain** (`internal/core/route_m19.go`, sửa nhỏ `route_m14.go`): `func (s *Sites) RouteExplainHost(ctx context.Context, host validate.Domain) (RouteTrace, error)`: `NameSite(host)`; không có: từ chối `name_unknown` ("không site nào có tên này; request với Host này rơi vào vhost mặc định và bị từ chối"). Là tên chính: `RouteExplain` như cũ. Là tên phụ: chặng DNS cho `host`; chặng vhost tìm `host` (đổi `routeVhostHop(ctx, sys, web, l, port)` thành nhận thêm `name string`), cổng 443 khi tên có trong chứng chỉ, không thì 80; tên `redirect`: chặng mới `HopRedirect = "redirect"` (gọi thật, thấy 301 và `Location`) rồi dừng, kèm câu "đi tiếp: tpan route explain <tên chính>"; tên `serve`: đủ chuỗi như tên chính, chặng HTTP gọi bằng `host`. `RouteTrace` thêm `Host string \`json:"host"\``, `Kind string \`json:"kind"\``, `Mode string \`json:"mode,omitempty"\``. `cmd/tpan/route.go` gọi `RouteExplainHost`.

**MCP chỉ đọc** (`internal/tools/catalog.go`, `read.go`): `list_site_names {domain}` và `list_site_redirects {domain}`, `KindRead`, cùng phạm vi quyền với `site_health`. Không có `propose_*` cho M19.

### 3.9 Giao diện bản 2

- Trang một site: thêm hai tab `ten-mien` ("Tên miền") và `chuyen-huong` ("Chuyển hướng") vào danh sách tab của `web/src/screens/site/index.tsx`. Tab "Tên miền": bảng tên chính + tên phụ (cột: tên, kiểu phục vụ/chuyển, DNS, chứng chỉ, thao tác), ô thêm tên phụ (tên + chọn kiểu), đổi kiểu, gỡ; mỗi thao tác qua `PreviewedOp` (xem trước → xác nhận → `JobTracker`). Lời từ chối `name_unavailable` hiện nguyên câu chung. Tab "Chuyển hướng": bảng luật, ô thêm (nguồn, đích, 301/302, giữ query), gỡ; dòng chú ý "khớp đúng đường dẫn, phân biệt hoa thường và dấu / cuối".
- Trang Công cụ: hai ô `domains` ("Tên miền phụ") và `redirects` ("Chuyển hướng") trong nhóm `website`, cả hai trỏ màn mới `/domains`: bảng các site trong phạm vi người xem, số tên phụ, số luật, mỗi dòng dẫn tới `/sites/<domain>?tab=ten-mien` hoặc `?tab=chuyen-huong`. Kiểu `Tool` thêm trường tuỳ chọn `tab?: string`.
- Ảnh sáng, tối, 390 px vào `docs/bang-chung/M19/`.

---

## 4. Bất biến (mỗi câu một test có tên)

| # | Bất biến | Test |
|---|---|---|
| B1 | User thường không bao giờ gắn được một tên thuộc tên miền gốc của người khác vào site của mình (tên phụ, tên chính, kể cả admin làm hộ) | `site.TestCheckClaimsForeignRoot`, `store.TestClaimNamesForeignRoot`, `core.TestAliasAddForeignRootRefused`, `core.TestSiteAddForeignRootRefused`; e2e E20–E23 |
| B2 | Một tên không bao giờ thuộc hai site (kể cả tên chính của site khác), kể cả khi giữ chỗ đua | `store.TestSiteNamesPrimaryKey`, `store.TestClaimNamesRace` (20 goroutine, hai chủ, cùng tên và cùng tên miền gốc: đúng một chủ thắng); e2e E24 |
| B3 | Chuỗi người dùng không bao giờ vào cấu hình web server ở dạng thô: output chỉ có chỉ thị của mẫu | `validate.FuzzParseRedirect` (chuỗi được nhận chỉ gồm ký tự cho phép), `templates.FuzzVhostRedirects` (mọi cặp nguồn/đích được nhận: số dòng, số `;`, `{`, `}` của output đúng bằng mẫu cộng đúng số dòng luật, mỗi dòng luật khớp regex mẫu), `templates.TestVhostNamesGuard` (lớp chặn cuối); e2e E30–E33 (`nginx -t`/`apachectl -t` với 50 luật ngẫu nhiên) |
| B4 | Thêm tên phụ chưa trỏ DNS không làm site hay chứng chỉ đang có ngừng chạy | `steps.TestCertbotNamesSkipFailedProbe`, `core.TestSSLIssueAliasNoDNSKeepsCert`, `templates.TestSSLUncoveredAliasNotOn443`; e2e E40–E42 |
| B5 | Nâng từ v0.2.0: site cũ không đổi hành vi, không có tên phụ nào tự sinh | `templates.TestVhostZeroNamesGolden` (output với `site.Names{}` bằng từng byte file vàng sinh từ mã v0.2.2 trước khi sửa, cho mọi biến thể: thường, SSL, lồng, tạm khoá × nginx, Apache), `store.TestMigration1900OnlyPrimaries`, `core.TestSiteAddRerunAddsNoAlias`; e2e E60–E62 |
| B6 | Vhost chỉ vẽ tên và luật `active`, cộng đúng thay đổi của job đang chạy | `core.TestVhostSkipsPendingNames` |
| B7 | Không tên nào là hậu tố công cộng | `validate.TestRegisteredDomainPublicSuffix`, `core.TestAliasAddPublicSuffixRefused` |
| B8 | Thử thách ACME của mọi tên luôn tới được webroot chung, không luật người dùng nào che được | `validate.TestRedirectSourceRejectsDotSegment`, `templates.TestNameRedirectBlockServesACME`; e2e E43 |
| B9 | Gỡ tên phụ đang nằm trong chứng chỉ thì chứng chỉ được xin lại không có nó (gia hạn sau đó không hỏng vì tên đã đi) | `core.TestAliasRmNeedsCert`; e2e E44 |
| B10 | Mọi route mới có luật, ba vai đúng (admin qua, user khác 404, xem như 403 khi ghi) | `server.TestAuthzTable` (thêm dòng) |
| B11 | Đích chuyển hướng không bao giờ lấy từ request (không `$host`, `%{HTTP_HOST}`) | `templates.TestNoHostVariables` (grep output mọi biến thể) |

---

## 5. Mô hình đe doạ

**Kẻ tấn công**: (a) user thường B muốn chiếm tên hay lưu lượng của A; (b) site của B đã bị chiếm (chạy được PHP dưới user của B, ghi được file trong thư mục site, kể cả `.htaccess`); (c) người ngoài gửi request với Host tuỳ ý; (d) user thường muốn làm hỏng chứng chỉ hay hạn mức của người khác.

| Đường | Chặn bằng | Test |
|---|---|---|
| B thêm `www.<tên của A>` hay `shop.<tên của A>` làm tên phụ, hoặc tạo site tên đó | luật tên miền gốc (3.2) trong transaction | B1, E20–E23 |
| B và A cùng giành một tên miền gốc mới, gửi song song | `ClaimNames` trong transaction IMMEDIATE, khoá chính `name` | B2, E24 |
| B giữ trước tên miền gốc của A trước khi A kịp tạo site (chiếm chỗ) | không chặn được bằng luật "ai tới trước"; admin thấy qua `tpan site alias list`/màn "Tên miền" và gỡ được trên site của B; ghi vào rủi ro mở (mục 8) | – |
| B dò xem tên nào đang ở máy qua lời từ chối | một mã `name_unavailable` và một câu cho mọi kiểu trùng của người khác | `server.TestNameUnavailableNoLeak` |
| B gắn hậu tố công cộng (`com.vn`) để "sở hữu" mọi tên dưới nó | từ chối `domain_public_suffix` | B7 |
| Chèn cấu hình qua tên (xuống dòng, `;`, `{`) | tên chỉ qua `ParseDomain` (ký tự `[a-z0-9.-]`), lớp chặn cuối trong template | B3, `TestVhostNamesGuard` |
| Chèn cấu hình qua luật chuyển hướng (xuống dòng, `;`, `}`, khoảng trắng, `$`, `%1`, `javascript:`, `//evil`) | danh sách ký tự cho phép, kiểu có trường ẩn, lớp chặn cuối, fuzz | B3, E30–E33 |
| ReDoS | không có regex do người dùng viết; Apache so `=`, nginx `location =` | – |
| Luật người dùng che thử thách ACME hay luật chặn file ẩn/PHP uploads | nguồn không có đoạn bắt đầu bằng `.`; `location =` cho đúng `/wp-content/uploads/x.php` chỉ trả 301 (bản thử S02, PHP không chạy) | B8 |
| Host lạ, hoa thường, chấm cuối, cổng, IP, URI tuyệt đối (c) | `server_name`/`ServerName` khớp đúng, không tên đại diện; vhost mặc định từ chối | R01–R14 (mục 2.3), e2e E50 |
| Đích chuyển lấy từ Host do kẻ gửi request đặt (mở chuyển hướng) | đích luôn là tên cố định trong file | B11 |
| B thêm tên phụ không trỏ DNS hay tên của người khác để làm hỏng chứng chỉ của site khác | chứng chỉ là của từng site (`--cert-name` = tên chính của site); tên phụ hỏng bị loại trước certbot; đơn hỏng không đụng chứng chỉ cũ | B4 |
| B bật tắt tên phụ liên tục để cạn hạn mức ACME của tài khoản chung | job `site.ssl` tự chạy sau thay đổi tên phụ chỉ chạy khi site có dưới 5 job `site.ssl` trong 24 giờ; trần 24 tên phụ. `POST .../ssl` gọi tay không bị giới hạn (như M5, rủi ro có từ trước, ghi ở mục 8) | `core.TestSSLIssueRateLimit` |
| B gỡ tên phụ rồi để tên đó trỏ đi chỗ khác, mong lần gia hạn của site A hỏng | không áp dụng: tên phụ của B nằm trong chứng chỉ của site B; với site của chính mình thì gỡ là xin lại (B9) và `ssl.names` cảnh báo | B9 |

### 5.1 Panel chạy bằng root đụng vào thứ do site kiểm soát

M19 không thêm thao tác root nào đọc hay ghi trong thư mục site. Từng chỗ chạm:

- **Ghi vhost** (`/etc/nginx/sites-available/tpan-<tên>.conf`, `/etc/apache2/...`): thư mục của root, tên file chỉ từ tên chính (`site.LayoutOf`), không từ tên phụ. Không tạo file mới cho tên phụ (mọi khối nằm trong file vhost của site), nên `Layout.Owns` (bản xem trước M28) không đổi.
- **Thử thách ACME** (`ACMEProbeNames`): ghi file ngẫu nhiên vào `/var/lib/tpan/acme/.well-known/acme-challenge/` (của root, site không ghi được) rồi gọi qua từng tên; vhost phục vụ thử thách từ thư mục đó bằng `location ^~` (nginx) hoặc `Alias` + `AllowOverride None` (Apache), nên `.htaccess` hay file trong `public` của site không chặn, không giả được thử thách cho tên của site khác.
- **Đọc chứng chỉ** (sau job `site.ssl`, để ghi `in_cert`; health `ssl.names`): `/etc/letsencrypt/live/<tên chính>/cert.pem`, của root; `ReadFile` đi theo symlink sang `archive/` như `CertbotCert.Check` hôm nay; site không ghi được `/etc/letsencrypt`.
- **Kiểm chứng bằng `curl`** (`NameProbe`, `RedirectProbe`, health): nội dung trả về do PHP của site sinh, tức do site kiểm soát. Chỉ dùng để so mã, `Location` và dấu hiệu WordPress; không ghi ra file, không đưa vào lệnh nào; thân trả về đọc tối đa 64 KiB (`curl --max-filesize` không áp cho chunked nên đặt `-r 0-65535` và cắt khi đọc), dòng `Location` cắt 2 KiB và bỏ ký tự điều khiển trước khi vào log hay giao diện. Site bị chiếm làm phép kiểm chứng của chính nó qua hay hỏng: chỉ ảnh hưởng chính site đó.
- **`.htaccess` của site** (M14): vhost chạy luật chuyển hướng trước `.htaccess` (bản thử S06), `.htaccess` không đổi được đích của luật vhost hay của khối tên phụ (khối tên phụ không có `<Directory>` mở `AllowOverride`). Danh sách chỉ thị M14 không đổi.
- **Đua thời điểm**: mọi thay đổi DB đi qua transaction; mọi job đi qua khoá của engine (`sys.Lock`); khoảng giữa giữ chỗ và job do trạng thái `pending` che (B6).
- **Tên file lạ**: không có; tên phụ không bao giờ thành tên file hay đường dẫn.

---

## 6. Nâng cấp và lùi

- **Từ v0.2.0/v0.2.x lên**: migration 1900 tạo hai bảng, chép tên chính của mọi site (kể cả trong thùng rác) thành dòng `primary`. Không sinh tên phụ, không ghi lại vhost nào. Vhost cũ chỉ được ghi lại khi có thao tác khác (sửa health, đổi stack…), và khi đó output với `site.Names{}` giống từng byte bản cũ (B5), nên hành vi không đổi. Dữ liệu cũ có thể đã có hai chủ trên một tên miền gốc (P4 chưa vá): migration không hỏng vì nó; `names.root.shared` cảnh báo; mọi lần giữ chỗ mới tôn trọng luật, nên với tên miền gốc đó **không chủ nào thêm được tên mới** cho tới khi admin gỡ bớt (chặt, không lỏng).
- **Lùi về bản trước M19**: bản cũ từ chối mở `tpan.db` có migration nó không biết ("DB đã áp migration 1900 mà tpan này không có"), đúng thiết kế từ M1; `tpan self-update --rollback` của M15 đã kiểm trước bằng `tpan.old site list` và từ chối. Muốn lùi thật: chép lại bản sao `tpan.db` từ trước khi nâng (hướng dẫn nâng cấp hiện có). Vhost có tên phụ do M19 ghi vẫn chạy với nginx/Apache sau khi lùi binary, nhưng bản cũ coi chúng là lệch: hướng dẫn ghi rõ gỡ hết tên phụ và luật trước khi lùi.
- **Hành vi cố ý đổi**: từ M19, hai chủ không còn tạo được site (hay tên phụ) dưới cùng một tên miền gốc. e2e cũ đang làm việc đó phải đổi tên thử, không đổi khẳng định: `scripts/e2e-m13.sh` (site của `khach_a` sang `*.example.net`, của `khach_b` và `N1` sang `*.example.org`, site của admin giữ `*.example.com`), `scripts/e2e-m14.sh` (`C`, của user A, sang `c.example.net`). `e2e-m16.sh` đã dùng ba tên miền gốc khác nhau cho admin và user nên không đổi. Tên `www` mặc định chỉ thêm cho tên miền gốc hoặc `www.` + tên miền gốc; mọi e2e cũ dùng tên con (`s1.example.com`, `a.example.com`...) nên không có tên phụ mới, không có phép kiểm mới, vhost không đổi. Ghi vào KE-HOACH mục 6.17 lúc đóng mốc.
- **Chứng chỉ**: lineage giữ tên `--cert-name <tên chính>`, đường dẫn `live/<tên chính>/` không đổi, hook gia hạn không đổi. Chứng chỉ cũ một tên vẫn dùng tới khi có tên phụ được thêm.

---

## 7. Kế hoạch kiểm thử

### 7.1 Unit (mỗi gói một file `*_m19_test.go` hoặc theo tên ở mục 4)

- `validate`: `TestRegisteredDomain` (bảng t4), `TestRegisteredDomainPublicSuffix`, `TestParseRedirectSource`/`Target` (bảng: hợp lệ; xuống dòng `\n`, `\r`, `;`, `}`, `{`, khoảng trắng đầu/giữa/cuối, `$`, `%2F`, `\`, `"`, `#frag`, `javascript:alert(1)`, `//evil.example.org`, `http://user@example.net`, `HTTP://` hoa, cổng 0 và 65536, IP, `/.well-known/x`, `/a/../b`, `/a//b`, quá dài), `FuzzParseRedirect`.
- `site`: `TestCheckClaims*` (bảng: tên tự do, tên của site khác cùng chủ, của chủ khác, tên miền gốc của chủ khác, pending của chính site, hậu tố công cộng, tên dành cho phép thử), `TestSplitNames`.
- `store`: `TestMigration1900OnlyPrimaries` (DB tạo bằng các migration tới 1800 có ba site, một trong thùng rác), `TestSiteNamesPrimaryKey`, `TestClaimNamesForeignRoot`, `TestClaimNamesRace`, `TestReleaseKeepsActive`, `TestDeleteSiteDropsNames`, `TestRedirectLimitAndUnique`.
- `templates`: `TestVhostZeroNamesGolden` (testdata vàng tạo bằng mã hiện tại TRƯỚC khi sửa template: thợ chạy một lần, commit file vàng cùng test), `TestVhostNamesNginx`/`Apache` (so nguyên văn với các khối ở mục 3.4), `TestSSLUncoveredAliasNotOn443`, `TestNameRedirectBlockServesACME`, `TestNoHostVariables`, `TestVhostNamesGuard`, `FuzzVhostRedirects`.
- `steps`: `TestCertbotNamesArgs` (argv có đủ `-d`, tên chính đầu), `TestCertbotCheckSAN`, `TestCertbotNamesSkipFailedProbe`, `TestNameProbe`, `TestRedirectProbe` (bằng `system.Fake`).
- `core`: `TestAliasAdd*`, `TestAliasRm*`, `TestSiteAddDefaultWWW` (gốc → www redirect; `www.` → gốc; tên con → không), `TestSiteAddRerunAddsNoAlias`, `TestVhostSkipsPendingNames`, `TestSSLIssueAliasNoDNSKeepsCert`, `TestSSLIssueRateLimit`, `TestRouteExplainAlias`.
- `checks`: `TestAliasDNSNeverFail`, `TestCertNamesWarn`, `TestServerNameUniqueAliases`.
- `server`: `TestAuthzTable` (dòng mới), `TestNameUnavailableNoLeak`, `TestRedirectInvalidField`.

### 7.2 e2e bước 22: `scripts/e2e-m19.sh` (theo khuôn `scripts/e2e-m14.sh` và cách dựng pebble của `scripts/e2e-m10.sh`)

Hai phần, A nginx và B Apache (máy ảo về sạch giữa hai phần), cùng các khẳng định; log `docs/bang-chung/M19.log`. `/etc/hosts` trong máy ảo trỏ các tên thử về 127.0.0.1; `nodns.example.invalid` cố ý không có. Hai user thường A và B (M13), mỗi người một phiên API.

| # | Khẳng định | Mong đợi |
|---|---|---|
| E01 | `tpan site add example.com` (A) | job thành công; `site alias list` có `www.example.com redirect active` |
| E02 | `curl -H 'Host: www.example.com' http://127.0.0.1/a?b=1` | 301, `Location: http://example.com/a?b=1` |
| E03 | `curl -H 'Host: example.com' http://127.0.0.1/` | 200, trang WordPress |
| E04 | `tpan site alias set example.com www.example.com --mode serve` | cả hai Host 200 |
| E05 | `tpan site add www.example.org --www redirect` (A) | tên phụ `example.org` chuyển về `www.example.org` |
| E06 | `tpan site add blog.example.com` (A) | không có tên phụ nào |
| E10 | thêm tên phụ `example.net` cho `example.com` **bằng giao diện** (bấm thật, ảnh) | job thành công; `example.net` 200 |
| E11 | SSL bằng pebble cho `example.com` | `openssl x509 -ext subjectAltName` có `example.com`, `www.example.com`, `example.net` |
| E12 | `https://www.example.com/` (khi `redirect`) và `https://example.net/` (`serve`) qua `curl --resolve` | 301 về `https://example.com/`; 200 |
| E20 | B: `POST /sites/{site của B}/names {"name": "www.example.com"}` | 409 `name_unavailable`, không có tên site hay chủ trong thân |
| E21 | B: thêm `shop.example.com`, `example.com.` , `EXAMPLE.COM`, `xn--…` của tên của A | 409 hoặc 400, không có dòng mới trong `site_names` |
| E22 | B: `tpan site add shop.example.com --owner B` (admin làm hộ) | từ chối `name_root_foreign` |
| E23 | giao diện của B: thêm `www.example.com` | thấy câu từ chối chung |
| E24 | A và B gửi song song 10 + 10 request thêm `www.example.edu` (tên miền gốc mới) cho site của mình | đúng một 202, còn lại 409 (`name_unavailable` cho người thua, `alias_exists` cho request cùng site); `site_names` có đúng một dòng; `server_name` không có tên lặp |
| E30 | `tpan site redirect add example.com /old-page /new-page` | 301 đúng đích; `?x=1` bị bỏ |
| E31 | 9 đầu vào xấu: `$'/a\nb'`, `/a;b`, `/a}`, `'/a b'`, `javascript:alert(1)`, `//evil.example.org`, `/a$x`, `/%2e%2e`, `/.well-known/x` | mỗi cái `redirect_invalid` (CLI mã khác 0, API 400 kèm `field`), vhost không đổi (sha256 trước = sau) |
| E32 | thêm 49 luật ngẫu nhiên hợp lệ (script sinh từ danh sách ký tự cho phép) | `nginx -t`/`apachectl -t` mã 0; luật thứ 51 `redirect_limit` |
| E33 | luật `keep_query` và 302 | `Location` đúng như bản thử P07–P09 |
| E40 | thêm tên phụ `nodns.example.invalid` cho site SSL | tên phụ thêm được; job SSL: `in_cert=false` kèm lý do; chứng chỉ cũ còn (serial như trước); `https://example.com/` 200 |
| E41 | tab "Tên miền" hiện `nodns.example.invalid` "chưa đạt" | ảnh |
| E42 | `curl --resolve nodns.example.invalid:443:127.0.0.1 https://…` | không vào vhost của site (bắt tay bị vhost mặc định từ chối) |
| E43 | thử thách ACME qua mọi tên (file thử) | 200 trên tên chính, tên phụ `serve`, `redirect`, tên chưa có trong chứng chỉ |
| E44 | gỡ `example.net` | chứng chỉ mới không còn `example.net`; `Host: example.net` rơi vào vhost mặc định |
| E50 | bộ R01–R14 (mục 2.3) với tên phụ, hai stack | đúng bảng |
| E51 | `tpan route explain www.example.com` (redirect) và `example.net` (serve) | chặng vhost trỏ đúng file và dòng; chặng `redirect` hoặc đủ chuỗi tới HTTP; thoát 0 |
| E52 | `tpan route explain evil.example.org` | `name_unknown`, thoát 1 |
| E53 | `tpan health` | `site.alias.dns:nodns.example.invalid` là **cảnh báo**, không lỗi; `ssl.names` cảnh báo |
| E60 | máy cài v0.2.0 (gói phát hành), hai site, một có SSL; nâng lên bản M19 | `site_names` chỉ có hai dòng primary; sha256 của hai vhost không đổi; `tpan health` không có lỗi mới |
| E61 | sau nâng, `tpan health --fix` ghi lại vhost | sha256 vẫn bằng trước (output vàng) |
| E62 | sau nâng, `site add` chạy lại trên site cũ | không có tên phụ mới |
| E70 | hồi quy: `scripts/e2e-m3.sh`, `e2e-m5.sh`, `e2e-m13.sh`, `e2e-m14.sh`, `e2e-m16.sh` | xanh |
| E71 | `e2e-m13.sh`, `e2e-m14.sh` đã đổi tên thử theo mục 6 (diff chỉ ở các dòng gán tên) | xanh; thêm một khẳng định trong `e2e-m13.sh`: `khach_b` tạo `b9.example.net` (tên miền gốc của `khach_a`) bị từ chối |

`make check` xanh. Soát cuối bằng thợ `opus` effort max chưa đọc mã mốc: tìm đường chiếm tên người khác, đường chèn cấu hình, đường làm hỏng chứng chỉ site khác.

---

## 8. Bảng chia việc

Luật: mỗi đợt tối đa 4 thợ, mỗi thợ một vùng file, không hai thợ chung file trong một đợt; NHẠY CẢM → `opus` effort high, còn lại `sonnet`. Chỉ phiên chính sửa `go.mod`, `go.sum`, `package.json`, `docs/KE-HOACH.md` (M19 không cần sửa `go.mod`). Đợt xếp theo ưu tiên điều phối: hết giờ ở đợt nào thì các đợt trước vẫn đứng một mình (xanh, an toàn). Sau mỗi đợt: phiên chính `make check`, commit, push.

| Đợt | Thợ | Loại | File được sửa | Việc |
|---|---|---|---|---|
| 0b (trước đợt 1, ~10 phút) | phiên chính | – | `internal/site/names.go` (chỉ kiểu, hằng, `CheckClaims` trả "chưa làm"); `internal/validate/redirect.go` (hai kiểu, `String`, `IsZero`, `MarshalText`, `UnmarshalText`, `Parse*` trả lỗi "chưa làm"); `internal/core/types_m19.go` (kiểu, chữ ký và `NameStore` của mục 3.5, thân trả lỗi "chưa làm"); dòng `var _ core.NameStore = (*store.Store)(nil)` để sau, khi 1a xong | để bốn thợ đợt 1 biên dịch song song; `make check`, commit |
| 1 (lõi tên + sở hữu + một tên một site) | 1a | NHẠY CẢM, opus high | `internal/validate/psl.go`, `psl_test.go`; `internal/site/names.go` (thân `CheckClaims`), `names_test.go`, `types.go` (trường `Names`); `internal/store/migrations/1900_site_names.sql`; `internal/store/names.go`, `names_test.go`; `internal/store/sites.go` (điền `Names` trừ `Rules`, `SaveSite` trong transaction, `DeleteSite`); `internal/store/upgrade_test.go`; `cmd/tpan/main.go` (một dòng kiểm `NameStore`) | 3.1 (migration tạo cả hai bảng), 3.2, `SetInCert`; chưa có hàm store của luật (đợt 2) |
| 1 | 1b | NHẠY CẢM, opus high | `internal/templates/m19.go`, `m19_test.go`, `testdata/m19-*`; `files/site-nginx*.tmpl`, `files/site-apache*.tmpl` (mọi biến thể, gồm bản tạm khoá), `files/site-*-name-redirect*.tmpl` (mới), `site.go`, `ssl.go`, `suspend.go`, `cage.go` (chỉ thêm hàm `…Names`, hàm cũ gọi hàm mới với `site.Names{}`); `docs/bang-chung/M19-nghien-cuu/t5-nginx-443.*` | 3.4 phần tên (`Rules` rỗng); test vàng B5 viết **trước** khi sửa template; chạy bố cục 443 của nginx trên máy ảo (mục 2.4, "Chưa thử") |
| 1 | 1c | NHẠY CẢM, opus high | `internal/core/names_m19.go`, `names_m19_test.go` (thân các hàm ở `types_m19.go`, được sửa file đó); `internal/core/{site,apply,cage,fix,harden_m14,health,php,probe,ssl,stack_switch,suspend,trash}.go` (chỉ chỗ gọi hàm dựng vhost, `site add --www`, giữ chỗ trong `site.go` và `apply.go`); `internal/demo/ssl.go`; `internal/steps/m19.go` (`NameProbe`, `NameGone`), `internal/steps/{site_web,ssl,suspend,cage}.go` (tham số `n site.Names`) | 3.5 phần tên |
| 1 | 1d | NHẠY CẢM, opus high | `internal/server/api_m19.go`, `types_m19.go`, `routes.go`, `authz_test.go`, `api_m19_test.go`; `cmd/tpan/site_m19.go`, `cmd/tpan/site.go` (cờ `--www`) | 3.7 và CLI cho tên phụ (route `names`, `domains`) |
| 2 (chuyển hướng) | 2a | NHẠY CẢM, opus high | `internal/validate/redirect.go`, `redirect_test.go`; `internal/templates/m19.go`, `m19_test.go`, `files/site-*-redirects.tmpl` | 3.3, phần luật của 3.4, fuzz B3 |
| 2 | 2b | NHẠY CẢM, opus high | `internal/core/redirects_m19.go`, `redirects_m19_test.go`, `internal/core/types_m19.go` (`RedirectStore`, bỏ thân "chưa làm" của hai hàm redirect); `internal/steps/m19.go` (`RedirectProbe`); `internal/store/redirects.go`, `redirects_test.go`, `internal/store/sites.go` (điền `Names.Rules`) | Plan `site.redirect.add/rm` |
| 2 | 2c | sonnet | `internal/server/api_m19.go` (route `redirects`), `authz_test.go`; `cmd/tpan/site_m19.go` (lệnh `redirect`) | API và CLI luật |
| 3 (chứng chỉ SAN) | 3a | NHẠY CẢM, opus high | `internal/steps/ssl.go`, `ssl_test.go`, `steps/m10_types.go` nếu cần | `ACMEProbeNames`, `CertbotCert` nhiều tên, `Check` so SAN |
| 3 | 3b | NHẠY CẢM, opus high | `internal/core/ssl.go`, `ssl_test.go`, `internal/core/names_m19.go` (`NeedsCert`, job vẽ lại sau SSL, chạy tiếp job SSL có hạn mức), `internal/store/jobs_m18.go` (`CountJobs`) | 3.6, ghi `in_cert` sau job, hạn mức 5 job/24 giờ cho job tự chạy |
| 4 (định tuyến + health + MCP) | 4a | sonnet | `internal/core/route_m19.go`, `route_m14.go`, `types_m14.go` (trường mới), `cmd/tpan/route.go`, `internal/server/api_m14.go` (`?host=`) | route explain cho tên phụ |
| 4 | 4b | sonnet | `internal/checks/m19.go`, `m19_test.go`, `checks/m14.go` (`ServerNameUnique`), `checks/ssl.go` (tách `dnsPointsHere`), `internal/core/health.go`, `internal/core/site.go` (hàm `siteChecks`, dòng 320) | 3.8 health |
| 4 | 4c | sonnet | `internal/tools/catalog.go`, `read.go`, `tools_test.go` | MCP chỉ đọc |
| 4 | 4d | sonnet | `scripts/e2e-m19.sh`; `scripts/e2e-m13.sh`, `scripts/e2e-m14.sh` (chỉ các dòng gán tên thử, mục 6, và khẳng định E71) | e2e bước 22 theo 7.2 (E10, E23, E41 đánh dấu "giao diện") |
| 5 (giao diện) | 5a | sonnet (`tho-giao-dien`) | `web/src/screens/site/Names.tsx`, `Redirects.tsx`, `index.tsx` (danh sách tab), `web/src/api/m19.ts`, `m19-types.ts`, `web/src/i18n/ten-mien.ts` | hai tab của trang site |
| 5 | 5b | sonnet (`tho-giao-dien`) | `web/src/screens/ten-mien/index.tsx`, `web/src/screens/cong-cu/tools.ts`, `web/src/i18n/cong-cu.ts`, `web/src/routes.tsx` | màn `/domains`, hai ô Công cụ |
| 6 | máy ảo | `tho-may-ao` | `docs/bang-chung/M19.log`, `docs/bang-chung/M19/` | chạy e2e hai stack, ảnh |
| 6 | soát cuối | opus effort max | cuối file này | như "Xong khi" |

**Phụ thuộc trong một đợt**: thợ viết theo chữ ký đã chốt ở mục 3 của thợ cùng đợt (ví dụ 2b gọi `validate.ParseRedirectSource` của 2a); test phụ thuộc chỉ xanh sau khi ghép. Phiên chính ghép theo thứ tự cột "Thợ" (a → b → c → d), chạy `make check` sau mỗi lần ghép.

**Ước lượng và phần cắt khi thiếu giờ** (cả mốc ~2,5 giờ viết mã với 4 thợ): đợt 1 ~45 phút, đợt 2 ~30, đợt 3 ~30, đợt 4 ~25, đợt 5 ~30. Thiếu giờ thì cắt theo thứ tự: (1) màn `/domains` (hai ô Công cụ trỏ `/sites`); (2) MCP; (3) `names.root.shared`; (4) lệnh `alias set` (gỡ rồi thêm lại thay được); (5) hạn mức 5 job SSL/24 giờ (khi cắt phải ghi rủi ro vào KE-HOACH). Không cắt: B1–B5, B8, B11.

### Rủi ro còn mở

- **Chiếm chỗ tên miền gốc** ("ai tới trước"): B có thể giữ một tên miền gốc trước khi chủ thật của nó tạo site. Luật không phân biệt được; admin gỡ tay. Cách chặt hơn (xác minh quyền tên miền bằng bản ghi TXT) cần DNS, để mốc sau.
- **Dòng pending mồ côi** khi tiến trình chết giữa job: tự nhận lại khi chạy lại đúng lệnh; chưa có dọn tự động.
- **Tên đã có trong chứng chỉ mà DNS đổi đi** (không do gỡ trong TPAN): lần gia hạn của cả site hỏng; `ssl.names` cảnh báo trước, sửa bằng `tpan ssl issue` (xin lại chỉ với tên đạt).
- **`tpan ssl issue` và `POST .../ssl` gọi tay không có hạn mức** (giữ hành vi M5): một user vẫn tự gọi lặp được để tốn hạn mức đơn của tài khoản ACME chung. Rủi ro có từ trước M19; M19 chỉ chặn đường tự chạy sau thay đổi tên phụ.
- Bố cục cổng 443 của nginx chưa chạy trên máy ảo (mục 2.4); thợ 1b chạy trước khi đóng đợt 1.
- Bản thử pebble chạy với `PEBBLE_AUTHZREUSE=0`; Let's Encrypt thật dùng lại xác minh còn hạn, nên xin lại khi thêm tên chỉ xác minh tên mới. Không đổi thiết kế.
