# M28, đợt 2 nhóm A: bản thiết kế (F4 kèm D5, H5, M1-DBCONN, P1, P2)

Phiên chính của mốc M28 viết bản nháp ngày 21/11/2026, sau khi đợt 1 (T1) đã vào `dev` và trước khi viết mã. Thợ soát thiết kế đọc bản nháp cùng ngày (mục 9). Phiên chính trả lời từng phát hiện ngày 23/11, rồi thợ soát cuối đọc mã (`docs/thay-doi/M28-dot-2-soat-cuoi.md`) và mã được sửa theo đó. File này là **bản cuối**: thân bài (mục 1 đến 8) đã gộp các quyết định của lần trả lời soát thiết kế và các chỗ sửa sau soát cuối, nên nó mô tả đúng mã đã làm. Mục 9 giữ lại từng phát hiện của soát thiết kế, mỗi cái một đoạn: thợ soát nói gì, phiên chính chốt gì. Cuối file là các chỗ mã khác bản thiết kế.

Repo này công khai. Với những chỗ còn hở (chưa vá), file này chỉ nói loại lỗi và ảnh hưởng, không nói cách gây ra; chi tiết giữ ngoài repo tới khi vá. Các lỗ đã vá thì giữ đủ chi tiết, vì bản vá và bộ thử đi cùng một lần đẩy.

**Từ dùng.** Giữ như bản thiết kế đợt 1 (`M28-thiet-ke.md`): **neo** là `/var/www/<tên>`, **K1** là mã chạy dưới user Linux của site A (PHP của một site bị chiếm), **K2** là tài khoản panel vai `user`. Thêm **K0**: người ngoài chưa đăng nhập, chỉ gửi được request tới cổng của panel. **Khoá nguồn** là thứ panel dùng để đếm lần đăng nhập sai (mục 3.4).

**Trùng tên.** "D5" trong file này là tên của rà soát v0.2.0 cho lỗ "health báo đạt trong lúc F4 mở". Soát cuối của đợt 2 cũng đánh số phát hiện D1 đến D13; khi file này nhắc tới chúng thì ghi rõ "D2 của soát cuối". Nhãn như `KQ D1 LO` ở mục 1 là tên phép thử của rà soát v0.2.0, không liên quan tới cả hai.

## 1. Vấn đề và phạm vi

| # | Lỗ | Ai | Đã xác nhận (máy ảo của phiên rà soát v0.2.0) | Hậu quả |
|---|---|---|---|---|
| F4 | `public` của site A thành symlink sang thư mục của site B: web server phục vụ thư mục đó dưới tên miền của A | K1 | nginx: `KQ D1 LO`, `KQ D2 LO` (file 640 của B, kể cả file ngoài docroot của B và file trong thư mục ẩn); mã PHP của B không lộ (`D3 CHAN`) | đọc dump SQL, `debug.log`, backup, `.git/config` của site khác |
| D5 | `tpan health` và `checks.Isolated` báo "đạt" trong lúc F4 mở | | `KQ D5 KHONG-DAT` | lỗ âm thầm, admin không biết |
| H5 | Apache bật `mod_status` sẵn, `/server-status` là `Require local`: PHP của A gọi `127.0.0.1/server-status` thấy request của mọi site | K1 | `H5 LO` | lộ URL kèm tham số (token đặt lại mật khẩu, khoá API) của khách mọi site |
| M1-DBCONN | user DB của site không có `MAX_USER_CONNECTIONS`: A mở hết 151 kết nối, mọi site khác trả 500 | K1 | `KQ M1-DBCONN LO ... site B loi 3/3 lan (ma: 500 500 500)` | một site làm sập mọi site |
| P1 | khoá "sai 5 lần thì khoá IP" lách được | K2 (hoặc K0 có một tài khoản thường) | `KQ VM-LOCK-1 LO 20 lần đoán mật khẩu admin từ một IP, không lần nào bị khoá` | dò mật khẩu admin không giới hạn |
| P2 | chưa đăng nhập vẫn ép panel xin hàng GiB RAM (argon2id 64 MiB mỗi lần, không trần số lần chạy cùng lúc) | K0 | `KQ VM-ARGON LO N=20: RSS của tpan lên 1255 MiB` | panel bị kernel giết vì hết RAM |

P1 là ba lỗi cùng một chỗ (`internal/store/auth.go`, `internal/server/auth.go` của bản cũ):

1. Request song song cùng đi qua `authLocked` trước khi lần sai thứ 5 kịp ghi khoá, nên bắn song song thì được so mật khẩu nhiều hơn 5 lần.
2. Đã khoá rồi mà còn một lần sai được ghi (do lỗi 1), thì `RecordLoginFailure` thấy `locked_until` có giá trị, đếm lại từ 1 và ghi `locked_until = NULL`: chính lần sai làm mất khoá.
3. Đăng nhập đúng của *bất kỳ* ai từ IP đó xoá bộ đếm của cả IP (`ClearLoginFailures(ip)`): khách hosting xen một lần đăng nhập đúng của chính mình sau mỗi 4 lần đoán mật khẩu admin là không bao giờ bị khoá.

Ngoài nhóm A (làm sau, hoặc ở mốc khác): nhóm B của đợt 2, các phát hiện của đợt 1 còn để sau (C1 đến C4, C9, C10, C11, C13), F2, P3, P8, P10, M1-KET, M1-DRYRUN, M1-MANAGED, TLS và cấu hình theo cỡ máy.

## 2. Nghiên cứu

Đo trên máy ảo `tpan-m28` trước khi viết mã. Mỗi phép đo là một script trong `scripts/e2e-m28-dot2/`, chạy trên bản chưa vá đợt 2 (phải đỏ đúng chỗ) và trên bản sửa tay trong máy ảo (để chắc cách sửa đúng trước khi viết thành mã). Các phép đo này sau đó thành bộ e2e của đợt 2 (mục 7).

| # | Đo gì | Kỳ vọng |
|---|---|---|
| N1a | nginx: A đổi `public` thành symlink sang `/var/www/B/public` và sang một thư mục ngoài docroot của B (file 640 của B) | bản chưa vá: lộ; sửa tay `disable_symlinks if_not_owner;` (bỏ `from=`) rồi reload: 403, không byte nào của B |
| N1b | ca hợp lệ: symlink BÊN TRONG `public` trỏ sang một thư mục khác của chính A | sau khi sửa vẫn 200 (chủ link và chủ đích cùng là user của A) |
| N1c | Apache: như N1a, N1b | bản chưa vá: lộ (khối `<Directory /var/www/>` của Ubuntu có `FollowSymLinks`); thêm khối neo `Options SymLinksIfOwnerMatch`: 403 khi khác chủ, 200 khi cùng chủ |
| N1d | `disable_symlinks` không có `from=` có làm hỏng đường ACME (`AcmeRoot`) hay đường nào có symlink do root đặt không | không: thành phần thuộc root trỏ tới thứ thuộc root là cùng chủ |
| N2 | Apache: user của A gọi `127.0.0.1/server-status` | bản chưa vá: thấy request của site khác; sau khi tắt `status` và reload: không 200, không có chuỗi đánh dấu của site khác |
| N3 | A mở 160 kết nối bằng tài khoản DB của mình; trong lúc đó `curl` site B | bản chưa vá: B 500; sau `ALTER USER '<A>'@'localhost' WITH MAX_USER_CONNECTIONS 20`: kết nối thứ 21 của A nhận lỗi 1226 (`max_user_connections`), B 200; root của MariaDB không bị trần này |
| N4 | P1: 20 lần sai mật khẩu admin xen đăng nhập đúng của `khach1`; và 20 lần sai bắn song song | bản chưa vá: không khoá (lỗi 3), và song song qua được nhiều hơn 5 lần so mật khẩu (lỗi 1) |
| N4b | P1 từ trong máy: dưới user của site e, các lần sai mật khẩu admin, mỗi lần một địa chỉ nguồn 127.0.x.y (S1) | bản chưa vá: không khoá; bản vá: mọi lần tính chung một khoá `uid:<n>`, khoá sau lần thứ 5 |
| N5 | P2: N request đăng nhập song song (mật khẩu sai, tên lạ); lấy mẫu RSS của tpan mỗi 0,1 giây; đo thời gian một lần argon2id trên máy ảo | bản chưa vá: RSS tăng khoảng N × 64 MiB; bản vá: tối đa 2 lần argon2id cùng lúc |

Ngoài bảng trên, e2e có thêm một phép đo chỉ ghi kết quả (không khẳng định) cho chỗ còn hở về MariaDB ở mục 5.

## 3. Thiết kế

### 3.1 F4: web server không đi theo chính `public` sang chủ khác

**nginx.** Hai template site (`site-nginx.conf.tmpl` và bản SSL) đổi `disable_symlinks if_not_owner from=$document_root;` thành `disable_symlinks if_not_owner;` (hằng `templates.NginxSymlinkGuardV2`). Có `from=$document_root` thì nginx không xét các thành phần từ `root` trở lên, mà `public` chính là `root`, nên `public` là symlink thì lọt. Không có `from=` thì nginx xét *mọi* thành phần của đường dẫn: thành phần nào là symlink thì chủ link phải trùng chủ đích. `/`, `/var`, `/var/www` và neo không phải symlink nên không bị gì; `public` là symlink của user A trỏ sang thứ của user B thì nginx từ chối. Symlink do chủ site đặt trỏ vào thứ của chính site vẫn chạy. Chọn bỏ `from=` thay vì `from=<neo>` để dòng là một hằng chung cho mọi site.

**Apache.** Mọi `<VirtualHost>` của vhost site (bản thường, và cả khối cổng 80 lẫn 443 của bản SSL) có thêm một khối ngay trước khối `public`:

```
<Directory "{{.Dir}}">
    Options SymLinksIfOwnerMatch
    AllowOverride None
</Directory>
```

Khối này thay `Options Indexes FollowSymLinks` mà `<Directory /var/www/>` của Ubuntu đặt cho neo, nên Apache xét thành phần `public` theo `SymLinksIfOwnerMatch` (N1c xác nhận). Bản vá này chặn ca tĩnh trên Apache; mục 5 ghi phần còn hở.

**Phép kiểm (D5).**

- `site.docroot` (mới, mỗi site, `checks.SiteDocroot`): `public` phải là thư mục thật thuộc user của site. `sys.Stat` của cửa `sitefs` (đợt 1) không đi theo symlink, nên `public` là symlink thì phép thấy chính cái link và báo **lỗi**. `public` là symlink là ca **không hỗ trợ** (S14): sau bản vá, web server từ chối phục vụ khi đích thuộc chủ khác; trên vhost cũ thì đây là lỗ đang mở; backup cũng từ chối site như vậy. Symlink BÊN TRONG `public` trỏ vào thứ của chính site thì vẫn hợp lệ, phép này không xét. Không có cách sửa tự động vì tpan không biết chủ site định làm gì. Đích của link là chuỗi do user của site đặt, nên chỉ vào bằng chứng (không vào tóm tắt hay nguyên nhân), đã lọc ký tự điều khiển và cắt còn 200 ký tự; khi công cụ health của AI đọc nó thì chuỗi được bọc `ai.Untrusted("symlink-target", ...)` và phiên bị đánh dấu đã đọc nội dung không tin được (D9 của soát cuối). Phép này nằm ở health và ở kiểm chứng của mọi Plan chạm tới chính site đó (`siteHardeningChecks`), không nằm trong `Isolated` hay kiểm chứng của harden.
- `web.docroot_guard` (mới, mỗi site, `checks.DocrootGuard`): vhost có dấu của bản vá. nginx: có dòng `disable_symlinks if_not_owner;` và không có dòng `disable_symlinks` nào khác (dòng cũ có `from=`, hay `disable_symlinks off;` trong một `location`, đều mở lại lỗ). Apache: MỌI `<VirtualHost>` của file có khối `<Directory "<neo>">` mà dòng `Options` cuối cùng đúng là `Options SymLinksIfOwnerMatch`. Phép này chỉ ở health và kiểm chứng của harden. Cách sửa: `tpan security harden`, hoặc `tpan health --fix` (ghi lại vhost của riêng site đó).
- `checks.VhostHardened` (M14) nhận cả dòng nginx cũ lẫn dòng mới. Phép này nằm trong kiểm chứng của mọi Plan chạm site; nếu nó đòi dòng mới thì trên máy vừa nâng cấp, mọi Plan không tự ghi lại vhost (khôi phục backup là ca rõ nhất) sẽ bị hoàn tác (S5). Tách "có dấu của bản vá F4" thành phép riêng `web.docroot_guard` là để health vẫn báo vhost cũ mà không làm hỏng các Plan khác.
- `php.pool` (mới, mỗi site, `checks.PHPPool`): đọc file pool `tpan-<slug>.conf` của site ở mọi bản PHP đang cài, xét mọi khối `[...]` (một file có thể khai hai pool): `listen` phải là đúng socket của site cho bản PHP đó (không phải `host:port`: pool nghe TCP thì PHP của mọi site gửi được request thẳng vào PHP của site này), `user`, `group` là user của site, `listen.owner`, `listen.group` là `www-data`, `listen.mode` là `0660`, không có `listen.acl_users`, `listen.acl_groups`. File pool nằm ngoài vùng site, do root ghi, nên K1 không làm phép này đổi kết quả được. Chỉ ở health (S8): một pool bị sửa tay không được chặn thao tác trên site khác. Cách sửa: `tpan health --fix` ghi lại pool theo template.

### 3.2 H5: tắt `mod_status` của Apache

- Danh sách module tắt dùng chung `steps.ApacheModulesOff = []string{"mpm_prefork", "status"}` cho cài stack và chuyển web server sang Apache (một chỗ, hai nơi dùng không lệch nhau). Hai Plan đó vốn khởi động lại Apache vì đổi MPM, nên tắt thêm `status` không tốn gì.
- `security harden` trên Apache chỉ tắt `status`, không áp lại phần bật của danh sách stack (admin có thể đã cố ý đổi các module đó), bằng bước mới `steps.ApacheModOff{Module: "status"}` (ID `apache.mod-off:status`). Bước này: hỏi `apachectl -M` (danh sách module đã nạp; khác `a2query -m`, lệnh này thấy cả module nạp bằng một dòng `LoadModule` đặt ngoài `mods-enabled`), `a2dismod -q status`, `apachectl configtest`, hỏi lại `apachectl -M`, rồi `systemctl reload apache2`. Reload, không restart, nên không cắt kết nối và lần tải lên đang dở của site nào. Hỏng ở bước nào thì bật lại module trước khi trả lỗi. Bước đặt SAU bước reload chung của các vhost trong harden, để lần reload của nó không nạp những vhost mà bước reload chung chưa biết.
- Module vẫn nạp sau `a2dismod` (dòng `LoadModule` ở chỗ khác) thì bước trả lỗi `steps.ErrModuleLoadedElsewhere` kèm cách tìm dòng đó (`apachectl -t -D DUMP_INCLUDES`), không báo xong một việc chưa xong. Trong harden, lỗi này không được làm hoàn tác cả bản vá (vhost, trần DB của mọi site): bước bọc `hardenModOff` coi như xong và `web.mod_status` trong kết quả của job thành cảnh báo kèm cách sửa tay (D11 của soát cuối).
- Phép kiểm mới `web.mod_status:apache2` (`checks.ApacheModStatus`, chỉ khi stack là Apache): `apachectl -M` có `status_module` là **lỗi**. Nằm ở health và kiểm chứng của harden, KHÔNG ở bộ phép của `tpan update`: admin cố ý bật lại `mod_status` để giám sát thì vẫn cập nhật gói được, chỉ health báo. Cách sửa: `tpan security harden`, hoặc `tpan health --fix` (cách sửa `apache.mod-off`: đúng bước `ApacheModOff`, kiểm chứng chỉ có `web.mod_status`).

tpan không dùng `/server-status` cho việc gì. Ubuntu không bật `mod_info`; tpan không đặt `stub_status` cho nginx hay `pm.status_path` cho pool nào.

### 3.3 M1-DBCONN: trần kết nối cho user DB

Hằng `site.DBMaxUserConnections = 20` (gói `internal/site`, vì `checks` không được import `steps`), có lý do bằng số: pool của site có `pm.max_children = 5` (`site-pool.conf.tmpl`), WordPress giữ tối đa một kết nối mỗi request PHP, cộng các lệnh `wp` của tpan (health, backup, cron của WordPress) chạy cùng lúc không quá 4: tối đa khoảng 9 kết nối hợp lệ, đặt 20 cho gấp đôi, kể cả lúc đổi bản PHP (hai pool chạy cùng lúc). Một chỗ cho ba nơi dùng (câu tạo user, Step, phép kiểm) để chúng không lệch nhau.

- **User mới.** `CREATE USER '<u>'@'localhost' IDENTIFIED BY '<pw>' WITH MAX_USER_CONNECTIONS 20;`. Cả user DB của site lẫn user của DB rời (M13) đều tạo bằng hàm dùng chung `dbCreateUserSQL` (`internal/steps/site_db.go`), nên sửa một chỗ là đủ.
- **User đang có: Step `steps.DBUserLimit{User, Max}`** (ID `mariadb.user-limit:<user>`). Đọc trần từ cột `max_user_connections` của `mysql.user` bằng client `mariadb` của root, KHÔNG bằng `SHOW CREATE USER` (câu đó in cả hash mật khẩu, mà engine ghi mọi dòng output vào log của job). Câu SQL dựng từ tên đã qua `dbNameRe` như mọi câu khác của `site_db.go`. Check đạt khi trần khác 0 (giữ trần admin đã cố ý nâng cho một site lớn) hoặc user không có. Apply đọc số cũ (đọc ở Apply chứ không ở Check, vì Check còn chạy lúc xem trước), chạy `ALTER USER '<u>'@'localhost' WITH MAX_USER_CONNECTIONS 20;`, rồi `KILL USER '<u>'@'localhost';`. Lệnh KILL là vì MariaDB chỉ đếm kết nối mở SAU khi tài khoản có trần, và `ALTER USER` không cắt kết nối đang mở: không cắt thì một site mở sẵn hơn 130 kết nối trước harden vẫn mở thêm được 20 sau harden, vượt `max_connections` (D3 của soát cuối). WordPress mở kết nối mới mỗi request, nên chỉ vài request đang chạy của site lỗi một lần. Vì cắt request đang chạy nên bước là **Danger**. Undo trả lại trần cũ nhưng không mở lại được kết nối đã cắt. Câu `ALTER USER` không chứa mật khẩu, nên in được ở bản xem trước.
- **Ai chạy bước này.** `security harden`, cho user DB của mọi site active (kể cả site hay DB đang tạm khoá: user khoá vẫn đổi trần được) và mọi DB rời active; khôi phục site từ thùng rác (S16); khôi phục DB rời từ thùng rác (D10 của soát cuối); `tpan health --fix` cho `db.user_limit` (cách sửa `db.user-limit`, kiểm chứng chỉ có `db.user_limit`). Harden đọc trạng thái từng user lúc dựng Plan: user không có, hay đọc lỗi, thì không có bước cho user đó, tên nó nêu trong bản xem trước (S2, S16), để một user lệch không làm hỏng cả Plan.
- **Phép kiểm `db.user_limit:<user>`** (`checks.DBUserLimit`): đọc trần và `@@GLOBAL.max_connections`. Trần là 0 thì **cảnh báo** (có sửa tự động); trần lớn hơn `max_connections` thì cảnh báo (trần không có tác dụng; không sửa tự động vì tpan không đè trần admin đã chọn); user không có thì cảnh báo và trỏ sang `db.access`. Có ở health cho user DB của mỗi site và của mỗi DB rời active (`internal/health/collect.go`), và trong kết quả của job harden.

Không đặt `max_user_connections` toàn máy trong `99-tpan.cnf`: trần toàn máy áp cho cả root của MariaDB, mà các lệnh của tpan chạy bằng root.

Trần theo user chặn được ca một site (plugin hỏng, script mở kết nối không đóng, hay mã của site bị chiếm mở kết nối bằng tài khoản DB của nó) giữ hết kết nối của máy. Nó không phải hàng rào trọn vẹn; hai chỗ còn hở ghi ở mục 5.

### 3.4 P1: khoá đăng nhập đúng nghĩa

1. **Khoá nguồn** (`sourceKey(r)`, `internal/server/auth_m28.go`). Bộ đếm lần sai trong DB (cột `ip` của `login_failures`), mutex, hàng chờ, bảng theo tên và giới hạn tốc độ đều dùng CÙNG một khoá đã chuẩn hoá:
   - kết nối từ ngoài máy: địa chỉ IPv4 (kể cả IPv4 ánh xạ `::ffff:a.b.c.d`), hay dải /64 của địa chỉ IPv6 (một máy thường có cả dải);
   - kết nối loopback (`127.0.0.0/8`, `::1`), và kết nối từ chính máy tới IP công khai hay IP riêng của máy (D8 của soát cuối): `uid:<n>`, UID của tiến trình đầu bên kia. Lý do (S1): trên Linux mọi địa chỉ 127.x.y.z đều là của máy, PHP của một site chọn được địa chỉ nguồn tuỳ ý trong cả dải, nên đếm theo địa chỉ thì mỗi địa chỉ có 5 lần đoán riêng. Đếm theo UID thì mọi lần đoán từ PHP của một site chung một khoá, còn admin vào qua `ssh -L` mang UID của phiên SSH, khác UID của site;
   - không tra được UID (không phải Linux, không thấy dòng): khoá chung `loopback`, an toàn hơn là cho qua.
2. **Tra UID có trần** (D1 của soát cuối). UID đọc từ cột `uid` của `/proc/net/tcp` và `/proc/net/tcp6` qua `system.System` (dòng có local là địa chỉ:cổng nguồn của request, remote là địa chỉ:cổng mà panel nhận kết nối). Bản đầu đọc trọn hai bảng vào bộ nhớ cho mỗi request, trước hàng chờ và semaphore argon2, nên một site mở vài nghìn kết nối rồi bắn vài trăm request là đẩy heap của tpan lên hàng GiB. Bản cuối: mỗi kết nối tra nhiều nhất một lần (`http.Server.ConnContext` gắn chỗ nhớ; UID không đổi trong đời socket); cùng lúc tối đa 2 lần tra, mỗi lần một bộ đệm cố định; đọc theo dòng, so hai cột đầu với chuỗi hex dựng sẵn của 4-tuple (không tách dòng không khớp), dừng ở dòng khớp, có trần số byte đọc. Hết chỗ tra sau 200 ms thì trả 429 `auth_busy` ngay: 429 thì thử lại được, không so và không đếm vào khoá nào.
3. **Mỗi khoá nguồn một lượt.** Trong `handleLogin` và `handlePasswordChange` (hai chỗ duy nhất so mật khẩu), các lần thử của cùng một khoá nguồn chạy lần lượt (`authGates`: một mutex theo khoá nguồn trong bộ nhớ, đếm số lượt có mặt). `authLocked` nằm trong đoạn giữ lượt, nên lần thử sau luôn thấy kết quả của lần trước: lỗi 1 hết mà không cần đổi transaction. Một khoá đã có 4 lượt (đang chạy cộng đang chờ) thì lượt thứ 5 nhận 429 ngay, không xếp hàng vô hạn. Không còn ai dùng thì dòng của khoá bị xoá khỏi bảng, nên bảng không phình theo số nguồn.
4. **Khoá còn hạn thì giữ.** `RecordLoginFailure`: `locked_until` còn hạn thì không đếm lại, không xoá khoá; chỉ khi khoá đã hết hạn hoặc lần sai đầu đã quá cửa sổ 15 phút mới đếm lại từ 1 (lỗi 2). Đang khoá thì cả mật khẩu đúng cũng nhận 423.
5. **Đăng nhập đúng chỉ trừ phần của chính tên đó** (lỗi 3). DB giữ như cũ một dòng mỗi khoá nguồn (tổng số lần sai trong cửa sổ, khoá). Trong bộ nhớ có bảng `authNames`: `(khoá nguồn, HMAC-SHA256 của tên) → (số lần sai, first_at của dòng DB lúc ghi)`. Khoá HMAC sinh ngẫu nhiên lúc tiến trình khởi động; tên đã gõ không bao giờ được lưu (người ta hay gõ nhầm mật khẩu vào ô tên). Tên được chuẩn hoá TRƯỚC khi băm (rỗng là `admin`, dài quá 64 là `\x00`). Đăng nhập đúng của tên N lấy phần của (khoá nguồn, N) và trừ đúng số đó khỏi dòng DB (`ForgetLoginFailures`), chỉ khi `first_at` còn trùng và không trừ quá tổng; lần sai của tên khác từ cùng nguồn vẫn còn. Theo S9: DB đếm lại từ 1 hay khoá hết hạn thì các dòng của khoá nguồn đó bị xoá; dòng quá cửa sổ được dọn mỗi lần ghi; bảng tối đa 10 000 dòng (đầy thì bỏ dòng ghi lâu nhất). Panel khởi động lại thì bảng mất, tổng trong DB vẫn còn: chỉ có thể khoá sớm hơn, không bao giờ khoá muộn hơn.
6. **Giới hạn tốc độ** (D7 của soát cuối). Lần SAI đã bị khoá sau 5 lần, nhưng lần ĐÚNG trước đây không bị đếm ở đâu, nên một tài khoản bất kỳ chạy vòng đăng nhập đúng từ nhiều nguồn là giữ đầy hai chỗ argon2 (mục 3.5). Thêm hai giới hạn theo cửa sổ một phút: mỗi khoá nguồn tối đa `authRateSource = 30` lần so mật khẩu (đúng hay sai), và mỗi tên tối đa `authRateName = 30` lần đăng nhập đúng (xét trước khi so). Chỉ người biết mật khẩu mới làm đầy được phần của một tên, nên người ngoài không dùng nó để chặn admin; và đây không phải khoá tài khoản, chỉ là 429 `auth_busy` tới hết phút, không tính là lần sai.
7. **Nhật ký ghi khoá nguồn** (D12 của soát cuối). Dòng `login.fail` và lần sai lúc đổi mật khẩu có cột `target` là khoá nguồn, nên với kết nối từ chính máy admin đọc được `uid:<n>` và tra ra site nào đang dò mật khẩu (cột `ip` chỉ có 127.0.x.y). Không bao giờ ghi tên đã gõ.
8. **`tpan admin unlock [<khoá nguồn>]`** (CLI, cần root; S10): xoá dòng `login_failures` của khoá đó (không đối số thì xoá mọi khoá), không đổi mật khẩu, không huỷ phiên, ghi nhật ký `admin.unlock`. Đây là lối ra khi admin dùng chung nguồn với người đã làm khoá (cùng NAT văn phòng). Lời 423 in khoá nguồn và nhắc lệnh này.

Không thêm migration: bảng `login_failures` giữ nguyên cấu trúc, nên lùi bản vẫn mở được DB.

### 3.5 P2: trần số lần argon2id chạy cùng lúc

`store.HashPassword` và `store.VerifyPassword` (mọi chỗ gọi argon2id đều qua `argonRun` trong `internal/store/auth.go`, gói duy nhất import `x/crypto/argon2`) lấy một chỗ trong semaphore `argonSlots = 2` trước khi chạy, chờ tối đa 3 giây; quá hạn thì trả `store.ErrPasswordBusy`. Mọi route gọi argon2 (đăng nhập, đổi mật khẩu, admin tạo user, đặt lại mật khẩu) đổi lỗi đó thành HTTP 429 `auth_busy` kèm `Retry-After: 3`; lần bận không tính là lần sai (chưa so thì chưa lộ gì). CLI dùng `store.HashPasswordWait` (chờ không hạn): CLI là tiến trình riêng nên không tranh chỗ với server. Trần bộ nhớ sống của argon2 trong server là 2 × 64 MiB. Hai chỗ vì một lần argon2id với `time=3, threads=4` mất cỡ trăm mili giây (đo ở N5): panel vẫn nhận vài lần đăng nhập mỗi giây, đủ cho một panel quản trị.

Hash giả cho tên lạ (để thời gian trả lời không lộ tên nào có thật) được tính lúc server khởi động, trước khi nghe cổng, bằng `HashPasswordWait` nên không bao giờ gặp `ErrPasswordBusy`, và lỗi không được nhớ (S11). Thứ tự trong `handleLogin`: tra khoá nguồn, chờ lượt, khoá nguồn đang khoá thì từ chối ngay (không tốn argon2), xét giới hạn tốc độ, rồi mới so mật khẩu (tên lạ cũng tốn argon2 như tên thật), rồi mới xem user có bị tạm khoá.

### 3.6 Harden không còn "tất cả hoặc không"

Máy nâng cấp chỉ nhận bản vá F4, H5, DBCONN qua `tpan security harden` (mục 6), nên harden không được để một site làm hỏng cả job.

- **Trước Plan** (S2): phép HTTP của từng site active được đo lúc dựng Plan (`hardenHealthy`). Site đang trả lời sai vẫn được vá đủ mọi bước, nhưng nêu tên trong bản xem trước (`Harden.Skipped`, CLI in kèm lý do) với lời "sửa hoặc tạm khoá site này", và phép HTTP, cô lập của nó không chạy sau job. User DB không có hay đọc lỗi cũng nêu ở đó (mục 3.3).
- **Kiểm chứng** (D2 của soát cuối): chỉ những phép do chính các bước của Plan quyết định mới làm job lỗi và hoàn tác: bộ phép của stack và các phép cấp máy của M14, quyền log, `VhostHardened`, `web.docroot_guard`, `web.mod_status`. Phép HTTP và `Isolated` của từng site đo thứ mà mã của khách điều khiển được (site trả 500 đúng lúc kiểm, chủ site đổi quyền thư mục neo của mình), nên trong harden chúng chạy như một báo cáo (`checks.Soft`: lỗi thành cảnh báo), nằm trong kết quả của job và nêu đúng site có vấn đề; health chạy sau job mở sự cố cho site đó như thường. `db.user_limit` cũng chỉ là cảnh báo, có mặt để kết quả của job ghi trần của từng user.
- **Thứ tự các nhóm bước** (sau khi gộp M17): ghi vhost của mọi site, một bước reload chung, tắt `mod_status`, trần kết nối DB, rồi khởi động lại dịch vụ của mọi lồng (M17) luôn ở cuối.

## 4. Bất biến

| # | Bất biến | Test |
|---|---|---|
| E1 | nginx không bao giờ trả một file mà đường tới nó đi qua symlink sang chủ khác, kể cả khi symlink là chính `public`. Apache: ca tĩnh như vậy bị từ chối (phần còn hở ở mục 5) | e2e F4 (nginx, Apache); golden test của template (`TestNginxSymlinkGuard`) |
| E2 | Symlink BÊN TRONG `public` trỏ vào thứ của chính site vẫn chạy | e2e F4.4 |
| E3 | Vhost viết trước bản vá bị `web.docroot_guard` báo, `security harden` và `tpan health --fix` viết lại nó; `VhostHardened` vẫn nhận vhost cũ nên Plan khác trên site cũ không bị hoàn tác | `TestDocrootGuard*`, `TestFixDocrootGuard`; e2e nâng cấp N6, N8 |
| E4 | Sau cài stack, chuyển sang Apache, hay harden trên Apache: `mod_status` tắt | `TestH5InstallApacheTatStatus`, `TestH5SwitchApacheTatStatus`, `TestApacheModOff*`; e2e H5 |
| E5 | Mọi user DB do tpan tạo hay đã qua harden, khôi phục thùng rác có `MAX_USER_CONNECTIONS` khác 0; kết nối mở trước đó bị cắt | `TestDBUserLimit*`, `TestRestoreDatTranKetNoi`, `TestRunDBRoiCoTranKetNoi`; e2e DBCONN (kết nối thứ 21 của A bị từ chối, B 200) |
| E6 | Một khoá nguồn không bao giờ được so mật khẩu sai quá 5 lần trong cửa sổ trước khi bị 423, dù gửi song song, xen đăng nhập đúng của tên khác, hay (từ trong máy) đổi địa chỉ nguồn | `TestE6SongSongKhongQuaNamLanSo` (bắn song song vào `handleLogin`, store SQLite thật, đồng hồ giả, móc đếm số lần so), `TestLoopbackTheoUID`; e2e P1a, P1b (đếm `login.fail` trong `audit`) |
| E7 | Đăng nhập đúng chỉ xoá lần sai của chính tên đó: 4 lần sai admin, 1 lần đúng `khach1`, 1 lần sai admin thì khoá | `TestE7XenDangNhapDungCuaTenKhac`, `TestS9HaiCuaSo`; e2e P1a |
| E8 | Admin gõ sai 4 lần rồi gõ đúng thì vào được, bộ đếm của admin về 0; khoá do người khác gây ra gỡ được bằng `tpan admin unlock` mà không đổi mật khẩu | `TestE8SaiBonLanRoiDung`; e2e P1c |
| E9 | Trong tiến trình không bao giờ có quá 2 lần argon2id chạy cùng lúc; tra UID của kết nối loopback có trần bộ nhớ | unit test có móc đếm, `TestD1*`; e2e P2 (RSS) |
| E10 | Sau bản vá, các lỗ ở bảng mục 1 đóng như mục 3 mô tả (trừ phần còn hở ghi ở mục 5), trên máy cài mới lẫn máy nâng từ v0.2.0 rồi chạy `security harden` | e2e chính và e2e nâng cấp |
| E11 | Một khách không làm được `security harden` hoàn tác bản vá của cả máy (site hỏng trước Plan, hay làm phép HTTP, cô lập của site mình lỗi lúc kiểm chứng) | `TestHardenSiteHongVanQua`, `TestD2HardenKhachXauKhongHoanTac`, `TestD11ModStatusNapNgoaiModsEnabled`; e2e nâng cấp N6b |

## 5. Mô hình đe doạ và còn hở

- F4 phía PHP đã bị chặn từ trước (pool của A chạy bằng user A, `open_basedir`, quyền 2750 của neo B); bản vá đóng phía web server.
- P1: kẻ có nhiều địa chỉ ngoài máy vẫn có 5 lần thử mỗi khoá nguồn mỗi 15 phút. Mật khẩu admin do `tpan setup` tạo dài 26 ký tự nên không dò được bằng tốc độ đó; mật khẩu do người đặt cho vai `user` thì yếu hơn. Chưa làm "chậm dần theo tên đích" (không khoá theo tên, vì như vậy ai cũng khoá được admin).
- Khoá theo nguồn khoá luôn người dùng chung nguồn với kẻ gây khoá (cùng NAT văn phòng, cùng dải /64). Lối ra là `tpan admin unlock`; admin vào qua `ssh -L` không chung khoá với site nào vì khoá là UID.
- P2: kẻ ngoài vẫn làm người đăng nhập thật phải chờ hay nhận 429 khi bắn liên tục (từ chối phục vụ màn đăng nhập), nhưng không còn làm panel hết RAM. Trên máy nhiều khách nên dùng `tpan setup --private` hoặc tường lửa cho cổng 8443.
- H5: admin tự bật lại `mod_status` thì health báo; tpan không tự tắt lại ngoài `security harden` và `tpan health --fix`.

**Còn hở, chưa sửa** (cách gây ra giữ ngoài repo tới khi vá):

- Apache: một dạng đua với symlink ở `public` (nginx không bị). Máy nhiều khách nên dùng nginx.
- Kết nối MariaDB chưa đăng nhập không bị trần theo user tính tới.
- Một khách có nhiều DB rời cộng các trần lại vẫn vượt được `max_connections`.
- Kẻ chiếm site còn vài khoá nguồn phụ ngoài `uid:<n>`, nên được vài lần đoán hơn 5 mỗi 15 phút.

## 6. Nâng cấp và lùi

- `tpan self-update` không đổi cấu hình site nào (giữ luật của M14 và M15). Sau khi nâng, health báo trên máy cũ: `web.docroot_guard` (vhost thiếu dấu của bản vá F4), `web.mod_status` (Apache), `db.user_limit` (user DB không có trần), và `site.docroot`, `php.pool` nếu site nào đang có vấn đề đó. `tpan security harden` (Plan có bản xem trước, có từ M14) sửa ba thứ đầu cho cả máy: viết lại vhost của mọi site theo template mới, tắt `status`, đặt trần 20 và cắt kết nối cũ. `tpan health --fix` sửa từng thứ riêng lẻ. Vì sao qua Plan mà không tự áp lúc nâng: đổi vhost thì phải reload web server, đổi user DB là đổi DB đang chạy và cắt request đang chạy; admin chọn lúc làm và xem trước được, như M14.
- Từ lúc nâng tới lúc harden, các Plan khác trên site cũ vẫn chạy được, vì `VhostHardened` nhận cả vhost cũ (S5).
- P1, P2 có hiệu lực ngay khi chạy bản mới (mã của panel, không đổi gì trên đĩa).
- **Site có `public` là symlink cùng chủ** (kiểu thư mục `current` của công cụ triển khai): web server vẫn phục vụ 200 sau bản vá, nhưng `site.docroot` báo lỗi và nằm trong kiểm chứng của mọi Plan chạm site đó (chạy lại `site add`, đổi bản PHP, khôi phục thùng rác, khôi phục backup, mở khoá site hay mở khoá khách hàng), nên các Plan đó bị hoàn tác; trên máy nâng cấp, trước bản vá chúng vẫn qua. Đây là hành vi đã biết (D13 của soát cuối), không phải lỗ: đổi `public` lại thành thư mục thật.
- **Lùi bản.** Không có migration nên bản cũ mở được DB, và trần kết nối DB giữ nguyên. Apache: khối neo mới không làm phép kiểm cũ lỗi. **nginx sau khi đã harden thì không lùi sạch** (D5 của soát cuối): bản cũ (v0.2.2 trở về trước) chỉ nhận dòng `disable_symlinks if_not_owner from=$document_root;`, nên `VhostHardened` của bản cũ báo lỗi cho mọi site và mọi Plan trên site không tự ghi vhost bị hoàn tác; còn `tpan health --fix` hay `tpan security harden` của bản cũ ghi lại vhost theo template cũ, tức là mở lại F4 cho mọi site. Đã lùi trên nginx thì đừng chạy harden hay `health --fix` của bản cũ.

## 7. Kế hoạch kiểm thử

Bộ e2e `scripts/e2e-m28-dot2.sh` (gọi qua `make e2e-m28-dot2 P="nginx apache nang-cap"`), chạy trên máy ảo `tpan-m28`, mỗi phần từ máy sạch (reset có kiểm máy thật sự sạch):

- `nginx`, `apache`: F4.0 đến F4.5 (symlink sang thư mục ẩn của B, sang thư mục ngoài docroot của B, health thấy lỗ, ca hợp lệ là symlink bên trong `public`), H5 (chỉ Apache: không 200, không chuỗi đánh dấu), DBCONN.0 đến DBCONN.4 (kết nối thứ 21 của e bị từ chối, b trả 200 trong lúc đó, dọn sạch sau phép thử) cộng một phép đo chỉ ghi kết quả, P1a (K1 dưới user của site e, mỗi lần một nguồn 127.0.0.x, xen đăng nhập đúng của `khach1`; khoá `uid:<n>` trong `login_failures`), P1b (20 lần sai song song: lần thứ 6 dùng ĐÚNG mật khẩu vẫn 423, đếm `login.fail` trong `audit`), P1c (admin sai 4 lần rồi đúng; `tpan admin unlock`), P1d (tpan còn sống), P2 (N chọn theo thời gian đo một lần argon2, lấy mẫu RSS).
- `nang-cap` (Apache): cài v0.2.0 bằng `install.sh` của gói phát hành, hai site WordPress của admin; N3 khẳng định F4, H5, DBCONN lộ trên bản cũ; nâng lên bản của cây; N6 khẳng định health có `web.docroot_guard`, `web.mod_status`, `db.user_limit` theo ID phép kiểm (`tpan health --json`); N6b: K1 cho người khác đi qua neo của site mình (`chmod o+x` dưới user của site) trước harden, rồi root trả lại `2750`; N7 harden thoát 0; N8 đến N11: health hết các dòng đó, hai site 200 và đăng nhập WordPress được (`wp-login.php` trả cookie `wordpress_logged_in_`), F4, H5, DBCONN chuyển sang chặn.

Log: bản chưa vá đợt 2 ở `docs/bang-chung/M28-dot2-truoc-ban-va.log` (bản `v0.2.2-4-g8657035`: 67 khẳng định đạt, 25 không đạt, các khẳng định không đạt đúng ở F4, H5, DBCONN, P1, P2); bản vá ở `docs/bang-chung/M28-dot2.log`. Kết quả trên bản vá (`v0.2.2-92-gfc7d33a`): 175 khẳng định đạt, 0 không đạt (nginx 53, Apache 40, nâng từ v0.2.0 82); chi tiết ở mục 6.27 của `KE-HOACH.md`.

Unit test: `internal/server/auth_m28_test.go`, `auth_soat_test.go` (E6 đến E8, S9, S11, hàng chờ, khoá nguồn, D1, giới hạn tốc độ, D12), `internal/steps/m28_dot2_test.go`, `internal/checks/m28_test.go`, `internal/core/core_m28_dot2_test.go`.

Chưa có trong e2e (chỉ unit test hoặc chưa làm): nâng cấp trên nginx; phép đo ghi kết quả cho phần còn hở trên Apache; F4 qua cổng 443 (chỉ golden test của template SSL); ca site hỏng TRƯỚC harden (chỉ `TestHardenSiteHongVanQua`); `a2enmod status` lại rồi health báo; `apt-get install --reinstall apache2` giữ module tắt; ca "kết nối mở trước harden" của D3 (chỉ `TestDBUserLimitKillKetNoiCu`); tra UID trên bảng socket lớn (chỉ `TestD1*`).

## 8. Bảng chia việc

| Phần | Vùng file | Ai làm |
|---|---|---|
| Nghiên cứu N1 đến N5 trên máy ảo; hợp đồng tên (hằng, ID phép kiểm, `ErrPasswordBusy`, mã `auth_busy`) | `scripts/e2e-m28-dot2/`, file `types` | phiên chính |
| Soát thiết kế | mục 9 của file này | thợ soát |
| F4, D5, H5: template, dấu nhận biết, phép kiểm `site.docroot`, `web.docroot_guard`, `php.pool`, `web.mod_status`, `db.user_limit` | `internal/templates/`, `internal/checks/` | `tho-loi` |
| H5, DBCONN: danh sách module dùng chung, `CREATE USER ... WITH`, Step `ApacheModOff`, `DBUserLimit`, Plan cài stack, chuyển web server, `security harden`; nối phép kiểm mới vào health | `internal/steps/`, `internal/core/` | `tho-loi` |
| P1, P2: khoá nguồn, hàng chờ, khoá còn hạn, bảng theo tên, semaphore argon2id, 429, `tpan admin unlock` | `internal/server/auth.go`, `auth_m28.go`, `internal/store/auth.go`, `cmd/tpan/admin.go` | `tho-loi` |
| e2e đợt 2 (chính, nâng cấp) | `scripts/e2e-m28-dot2.sh`, `scripts/e2e-m28-dot2/`, `Makefile` | thợ test |
| Soát cuối (thợ chưa đọc mã đợt 2): đọc mã và thử phá | `docs/thay-doi/M28-dot-2-soat-cuoi.md` | thợ soát |

## 9. Soát thiết kế

Thợ soát thiết kế, ngày 21/11/2026, đọc bản nháp của file này (bản trước khi gộp), báo cáo rà soát v0.2.0 và mã; không chạy gì trên máy ảo. Tổng: **4 nặng, 9 vừa, 3 nhẹ**. Phiên chính trả lời ngày 23/11; mọi chỗ chốt dưới đây đã nằm trong mục 1 đến 8.

**S1 (nặng): P1 và P2 đếm theo địa chỉ nguồn, mà PHP của bất kỳ site nào chọn được địa chỉ nguồn trong cả dải 127.0.0.0/8 khi gọi panel.** Panel nghe ở cổng mà site trên cùng máy gọi tới được; PHP của site A chọn địa chỉ nguồn bằng tuỳ chọn `bindto` của `stream_context_create` hay `curl --interface` (pool không có `disable_functions`). Mỗi địa chỉ là một dòng `login_failures`, một mutex, một hàng chờ riêng, nên K1 dò mật khẩu gần như không giới hạn (tốc độ chỉ còn bị argon2 chặn) và giữ kín được hai chỗ argon2. Chốt: khoá nguồn của kết nối loopback là `uid:<n>` của tiến trình đầu bên kia, tra trong `/proc/net/tcp`, `/proc/net/tcp6`; không tra được thì khoá chung `loopback`; thêm `tpan admin unlock`; không làm "chậm dần theo tên đích" (ghi ở mục 5). E6 viết lại thành "một khoá nguồn".

**S2 (nặng): `security harden` là tất cả hoặc không, và kiểm chứng của nó có phép HTTP của từng site.** Một site trả sai (kể cả chính site đang mở F4, vì sau bản vá nó trả 403; hay chỉ một plugin lỗi) làm job lỗi, engine hoàn tác cả bản vá của cả máy, mà harden là đường sửa duy nhất cho máy nâng cấp. Chốt: đo phép HTTP trước Plan, site đang hỏng vẫn được vá nhưng nêu tên trong bản xem trước và không vào kiểm chứng; user DB không có thì bỏ qua kèm cảnh báo. Soát cuối (D2) thấy chốt này chưa đủ vì khách vẫn làm phép của site mình lỗi đúng lúc kiểm chứng; bản cuối hạ mọi phép HTTP và cô lập của từng site trong harden xuống cảnh báo (mục 3.6).

**S3 (nặng): trên Apache, `SymLinksIfOwnerMatch` không chặn được mọi dạng của F4.** Còn một dạng đua với symlink ở `public`, nên bản vá chỉ chặn ca tĩnh trên Apache. Chốt: không sửa thêm trong đợt này; E1 chỉ hứa cho nginx; ghi còn hở và khuyên nginx cho máy nhiều khách (mục 5).

**S4 (nặng): trần theo user của MariaDB không phải hàng rào trọn vẹn.** Kết nối MariaDB chưa đăng nhập không bị trần theo user tính tới. Chốt: đo trên máy ảo (e2e ghi kết quả, không khẳng định), sửa câu chữ của mục 3.3 và E10 cho đúng, ghi còn hở (mục 5).

**S5 (vừa): từ lúc nâng tới lúc harden, `VhostHardened` báo thiếu dấu mới ở mọi site cũ, mà phép này nằm trong kiểm chứng của mọi Plan chạm site.** Plan không tự ghi lại vhost (khôi phục backup) sẽ bị hoàn tác trên site cũ, và khách vai `user` không chạy được harden để thoát. Chốt: `VhostHardened` nhận cả dòng cũ lẫn dòng mới; dấu của bản vá F4 là phép riêng `web.docroot_guard`, chỉ ở health và kiểm chứng của harden.

**S6 (vừa): dấu nhận biết Apache theo kiểu "một dòng có ở đâu đó trong file" không phân biệt được vhost cũ với vhost mới**, vì `Options SymLinksIfOwnerMatch` và `AllowOverride None` đã có sẵn trong vhost cũ (ở khối `public`, khối `uploads`): health sẽ im trên máy Apache nâng cấp. Chốt: `web.docroot_guard` cho Apache đòi khối `<Directory "<neo>">` có `Options SymLinksIfOwnerMatch` trong MỌI `<VirtualHost>` của file, kể cả khối cổng 80 của bản SSL (vì `<Directory /var/www/>` của Ubuntu là `Require all granted`, không thể trông vào luật chuyển hướng); e2e nâng cấp khẳng định theo ID phép kiểm.

**S7 (vừa): lùi bản không sạch như bản nháp viết.** Bản cũ đòi đúng dòng nginx cũ, nên sau harden thì mọi site báo lỗi, Plan bị hoàn tác, và harden của bản cũ mở lại F4. Chốt lúc trả lời soát thiết kế chưa đúng (cho rằng nhận cả hai dòng ở bản mới là đủ); soát cuối (D5) chỉ ra, và mục 6 nay ghi đúng hệ quả cùng lời khuyên.

**S8 (vừa): đưa `listen`, `user`, `group` của pool vào `checks.Isolated` (phép theo cặp, nằm trong kiểm chứng của mọi Plan chạm site và của harden) thì một pool sửa tay chặn mọi thao tác trên mọi site khác**, mà harden không sửa pool; phép còn chạy theo bình phương số site. Chốt: phép riêng `php.pool` theo site, mọi bản PHP đang cài, mọi khối của file, xét thêm `listen.owner`, `listen.group`, `listen.mode`, ACL; chỉ ở health; sửa bằng `tpan health --fix`.

**S9 (vừa): bảng theo tên trong bộ nhớ chưa có luật sống chết.** Không xoá khi DB đếm lại từ 1, không chặn dưới 0, nên lần sai "gửi" từ cửa sổ trước trừ được lần đoán admin của cửa sổ sau (gấp đôi số lần đoán), và bảng lớn mãi. Thợ soát cũng nhắc: mutex, hàng chờ, bảng và dòng DB phải cùng một khoá đã chuẩn hoá (IPv4 ánh xạ giữ IPv4, nếu không lấy /64 của nó gộp mọi người dùng IPv4 làm một), và `authLocked` phải nằm trong đoạn giữ mutex (kiểm ngoài thì 4 lượt đang chờ đã qua kiểm, tới 9 lần so). Chốt: mỗi dòng nhớ `first_at`, chỉ trừ khi còn trùng, không trừ quá tổng, dọn khi DB đếm lại hay khoá hết hạn, trần 10 000 dòng, băm tên sau chuẩn hoá, `authLocked` trong lượt; test bằng đồng hồ giả cho ca hai cửa sổ.

**S10 (vừa): khoá theo nguồn khoá luôn admin khi admin dùng chung nguồn với kẻ khác** (`tpan setup --private` vào bằng `ssh -L` thì panel thấy 127.0.0.1, chung với mọi site trên máy; NAT văn phòng cũng vậy), mà lối thoát duy nhất là `tpan admin passwd` (đổi luôn mật khẩu và huỷ phiên). Chốt: khoá theo UID của S1 tách admin khỏi site; thêm `tpan admin unlock [<khoá nguồn>]`, lời 423 nhắc lệnh này.

**S11 (vừa): hash giả cho tên lạ tính lười trong `sync.Once`; khi đi qua semaphore, lần đầu gặp `ErrPasswordBusy` bị nhớ tới lúc khởi động lại**: mọi tên lạ trả 500 ngay, tên thật trả 401 sau argon2, tức là lộ tên user. Chốt: tính lúc server khởi động trước khi nghe cổng, không qua semaphore, không nhớ lỗi; `ErrPasswordBusy` thành 429 ở mọi route gọi argon2; CLI chờ không hạn.

**S12 (vừa): trần theo từng user DB không trần được một khách.** Một khách có nhiều DB vẫn cộng các trần lại vượt `max_connections`. Chốt: giữ trần 20 cho cả user DB rời, ghi còn hở (mục 5); phép cấp máy cảnh báo khi tổng trần của một chủ vượt `max_connections` để sau.

**S13 (vừa): kế hoạch kiểm thử thiếu khẳng định cho các lỗ nặng, có khẳng định xanh mà không chứng minh điều nó nói, và một khẳng định sẽ đỏ trên bản đúng.** Thiếu: N4b (S1), harden khi có site hỏng (S2), phép đo cho S3, S4, phép kiểm pool, `web.mod_status` sau khi bật lại, `db.user_limit` trên máy nâng cấp, F4 qua HTTPS, nâng cấp trên nginx. Xanh mà không chứng minh: bước ghi lại health không khẳng định gì; unit test E6 chỉ gọi store trong khi mutex nằm ở server; P1b đếm câu 401 trong khi lần sai thứ 5 trả 423 sau khi đã so mật khẩu. Đỏ trên bản đúng: P2 đòi ít nhất một 429 với N=20, mà 20 lần đăng nhập chỉ mất 1,2 đến 1,7 giây, dưới hạn chờ 3 giây. Chốt: E6 bắn song song vào `handleLogin` với móc đếm số lần so; P1b dùng ĐÚNG mật khẩu ở lần thứ 6 và đếm `login.fail` trong `audit`; N của P2 chọn theo thời gian đo; thêm N4b, N6 theo ID phép kiểm, phép đo ghi kết quả cho S3, S4; F4 qua HTTPS và nâng cấp trên nginx "nếu kịp". Đã làm: E6, P1b, N của P2, N4b (P1a), N6, phép đo cho S4. Chưa làm: phép đo cho S3, F4 qua HTTPS, nâng cấp trên nginx (mục 7).

**S14 (nhẹ): bản nháp coi `public` là symlink sang thư mục của chính site là ca hợp lệ, trong khi `site.docroot` báo ca đó là lỗi và backup đã từ chối site như vậy**; đích của link (chuỗi do site viết) vào bằng chứng mà công cụ AI chỉ bọc `untrusted` cho `http.site:*`. Chốt: `public` là symlink là không hỗ trợ; ca hợp lệ là symlink bên trong `public`; đích cắt 200 ký tự, lọc ký tự điều khiển; `site.docroot` ở health và kiểm chứng của Plan trên chính site đó. Phần bọc `untrusted` được làm sau soát cuối (D9).

**S15 (nhẹ): H5 còn vài chỗ chưa đúng.** Bước `ApacheModules` luôn restart Apache (cắt kết nối và upload đang dở của mọi site) trong khi tắt `status` chỉ cần reload; harden dùng nguyên danh sách stack thì áp lại cả phần bật mà admin có thể đã cố ý đổi; `a2query -m` chỉ nhìn link trong `mods-enabled` nên hụt module nạp bằng `LoadModule` ở chỗ khác; đặt `web.mod_status` vào bộ phép của `tpan update` thì admin bật lại `mod_status` để giám sát sẽ không cập nhật gói được; N2 kỳ vọng 404 nhưng thực tế là 403 của vhost mặc định. Chốt: bước `ApacheModOff` reload, harden chỉ tắt `status`, phép dùng `apachectl -M`, chỉ ở health và kiểm chứng của harden, N2 khẳng định "không 200, không chuỗi đánh dấu".

**S16 (nhẹ): `DBUserLimit` còn vài chỗ chưa chốt.** Check "bằng 20" thì harden hạ trần admin đã cố ý nâng; số cũ cho Undo đọc ở Check, mà Check còn chạy lúc xem trước; `WHERE User = ?` trong khi client `mariadb` không có tham số; site đang trong thùng rác lúc harden không nhận trần, khôi phục ra thì không có trần. Chốt: đạt khi khác 0; `db.user_limit` cảnh báo khi 0 hay lớn hơn `max_connections`; số cũ đọc ở Apply; user không có thì bỏ qua kèm cảnh báo; câu SQL dựng từ tên đã qua `dbNameRe`; khôi phục site từ thùng rác có `DBUserLimit` (khôi phục DB rời thêm sau soát cuối, D10).

### Đã kiểm, không thấy sai (soát thiết kế)

- nginx: bỏ `from=` đóng F4 cho nginx ở gốc, kể cả chuỗi symlink và symlink ở tầng sâu hơn; `try_files`, `index` cũng đi qua kiểm này; `/`, `/var`, `/var/www`, neo không phải symlink; đường ACME toàn thành phần của root và site không ghi được webroot ACME; vhost tạm khoá không trỏ vào `public` (`root` là `/var/lib/tpan/suspended`); file `.php` đi sang pool của A nên mã PHP của B không lộ.
- Apache, ca tĩnh: Options xét cho thành phần `public` là của khối neo, nên khối `<Directory "{{.Dir}}">` có tác dụng như mục 3.1 (N1c xác nhận). `.htaccess` ở neo không được đọc; chế độ `an-toan` không cho `Options +FollowSymLinks`; `Alias` duy nhất là của ACME, trỏ vào cây của root.
- Không còn trang trạng thái nào khác: Ubuntu không bật `mod_info`; tpan không đặt `stub_status` cho nginx hay `pm.status_path` cho pool nào; không mã nào của tpan dùng `/server-status`.
- DB: `CREATE USER` chỉ có trong `dbCreateUserSQL`, dùng cho cả site lẫn DB rời; `ALTER USER ... IDENTIFIED BY` của đổi mật khẩu không đụng trần; `DB_HOST=localhost` đi qua socket; không đặt trần toàn máy là đúng; 20 đủ cho `pm.max_children = 5`, kể cả lúc đổi bản PHP.
- P1: sửa lỗi 2 đúng; chỗ so mật khẩu chỉ có đăng nhập và đổi mật khẩu; không cần migration; HMAC với khoá sinh lúc khởi động không giữ tên đã gõ; panel khởi động lại chỉ làm khoá sớm hơn, không muộn hơn.
- Nâng cấp từ v0.2.0 hay từ bản đợt 1: hai bản ghi vhost giống nhau, nên hai đường nâng là một.
- P2: chỉ `internal/store/auth.go` import `x/crypto/argon2`, nên semaphore trong hai hàm đó phủ mọi lời gọi; CLI chạy tiến trình riêng nên không tranh chỗ với server.
- Harden ghi lại vhost theo nội dung, nên khi harden chạy qua được thì vhost Apache cũ cũng được ghi lại dù dấu nhận biết có sai (S6).

## Chỗ mã khác bản thiết kế

Mã thắng bản thiết kế. Những chỗ dưới đây là chỗ mã đã làm khác (hoặc chi tiết hơn) bản nháp và lần trả lời soát thiết kế; thân bài ở trên đã viết theo mã.

1. Hằng trần kết nối là `site.DBMaxUserConnections` (gói `internal/site`), không phải `steps.DBUserMaxConnections` như bản nháp.
2. H5 trong harden dùng bước mới `steps.ApacheModOff` (reload, hỏi lại `apachectl -M`, lỗi `ErrModuleLoadedElsewhere`), không phải `ApacheModules` với `NoRestart` và `a2query` như bản nháp.
3. ID phép kiểm có hậu tố: `web.mod_status:apache2`, `db.user_limit:<user>`, các phép theo site là `<loại>:<tên miền>` (`site.docroot:example.com`). `web.mod_status` nằm trong `isolationChecks` của core (phép cấp máy của health), nên cũng có trong kiểm chứng của harden.
4. `DBUserLimit`: Apply chạy thêm `KILL USER` sau `ALTER USER`, và bước là Danger (D3 của soát cuối). Bản nháp và lần trả lời soát thiết kế không có.
5. Kiểm chứng của harden: phép HTTP và `Isolated` của MỌI site (không chỉ site hỏng trước Plan) là `checks.Soft` (D2 của soát cuối). `Isolated` không sinh từ site đang chạy trong lồng (luật của M17, có sau khi gộp `dev`).
6. `web.mod_status` và `db.user_limit` có cách sửa riêng qua `tpan health --fix` (`apache.mod-off`, `db.user-limit`); lần trả lời soát thiết kế chỉ cho harden sửa hai thứ này (D2 của soát cuối).
7. `php.pool` từ chối cả `listen.acl_groups`, không chỉ `listen.acl_users` (PHP-FPM bỏ qua `listen.owner`, `listen.group` khi có ACL).
8. `DBUserLimit` coi user không có là đạt (không có gì để đặt); harden đọc trạng thái lúc dựng Plan và không đưa bước cho user không có hay đọc lỗi.
9. Tra UID: đọc qua `sys.OpenRead` theo dòng có trần, một lần mỗi kết nối, tối đa 2 lần tra cùng lúc, hết chỗ thì 429 (D1 của soát cuối); lần trả lời soát thiết kế viết "đọc qua `system.System.ReadFile`".
10. Kết nối từ chính máy tới IP của máy cũng tra UID như loopback (`selfConn`, D8 của soát cuối).
11. Giới hạn tốc độ 30 lần so mỗi phút mỗi khoá nguồn và 30 lần đăng nhập đúng mỗi phút mỗi tên (D7 của soát cuối); 429 của giới hạn này có `Retry-After` tính theo lúc hết cửa sổ, không cố định 3 giây.
12. Nhật ký ghi khoá nguồn ở cột `target` (D12 của soát cuối).
13. Đích symlink của `site.docroot` được bọc `ai.Untrusted` khi công cụ health của AI đọc (D9 của soát cuối); lần trả lời soát thiết kế chỉ cắt và lọc.
14. Khôi phục DB rời từ thùng rác có `DBUserLimit`, và health có `db.user_limit` cho mỗi DB rời active (`internal/health/collect.go`) (D10 của soát cuối).
15. `tpan admin unlock` không đối số thì gỡ mọi khoá (`ClearAllLoginFailures`).
16. Hai template nginx (thường và SSL) mang dòng mới; bản tạm khoá của nginx không có dòng `disable_symlinks` (nó không trỏ vào `public`), khác câu "ba template" của bản nháp.
