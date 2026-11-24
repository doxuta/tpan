# M28 đợt 2 nhóm A: soát cuối (F4 kèm D5, H5, M1-DBCONN, P1, P2)

Ngày 23/11/2026. Thợ soát cuối, chưa đọc mã đợt 2 trước phiên này. Đối tượng: nhánh `moc/M28` ở `c249404` (bốn commit `a13745b`, `5cdd5b6`, `617eb97`, `c249404` trên `8657035`), đối chiếu với bản thiết kế `docs/thay-doi/M28-dot-2-thiet-ke.md` (bản nháp lúc đó, cùng lần trả lời soát thiết kế).

Cách làm: chỉ đọc mã, script và tài liệu; không dùng máy ảo, không sửa mã. Trên Mac có chạy unit test của `internal/server` (đạt), và một chương trình thử dựng handler thật bằng `server.NewHandler` với store SQLite thật và argon2 thật, chỉ dùng API xuất ra của `server`, `store`, `system`, `account`, để đo hai thứ: chi phí tra UID của kết nối loopback (bảng `/proc/net/tcp` giả qua `system.FakeSystem`, cùng mã đọc và tách dòng của bản vá lúc đó), và vòng đăng nhập đúng từ nhiều nguồn. Chương trình thử và log của nó không vào repo.

Chỗ ghi "cần máy ảo" là suy từ mã và từ cách chương trình ngoài (kernel Linux, MariaDB, Apache) chạy, chưa đo; chỗ dựa vào hiểu biết mã nguồn chương trình ngoài mà chưa tra lại trong phiên thì ghi rõ. Số dòng trong bằng chứng là số dòng ở `c249404`; mã đã đổi sau đó.

Trong lúc soát, phiên chính đã đọc bản nháp của file này và thêm commit `e223277` (chỉ sửa script e2e); D6 ghi trạng thái e2e ở commit đó.

Repo này công khai. Phát hiện đã vá giữ đầy đủ; phát hiện chưa vá chỉ còn loại lỗi và ảnh hưởng, chi tiết giữ ngoài repo tới khi vá (cùng cách làm với soát cuối đợt 1, mục 24 của `docs/thay-doi/M28.md`).

**Trùng tên.** D1 đến D13 ở đây là phát hiện của soát cuối này. "D5" trong bản thiết kế là tên khác (lỗ "health báo đạt trong lúc F4 mở" của rà soát v0.2.0); ở đây gọi nó là "D5 của bản thiết kế".

Từ dùng: K0 (người ngoài chưa đăng nhập), K1 (mã PHP của site A bị chiếm, chạy dưới user Linux của A; pool không có `disable_functions` nên chạy được `proc_open`, python3), K2 (tài khoản panel vai `user`), neo (`/var/www/<tên>`, do user của site làm chủ).

## Tóm tắt

Tổng: **2 nặng, 4 vừa, 7 nhẹ**.

| # | Mức | Việc | Trạng thái |
|---|---|---|---|
| D1 | nặng | tra UID của kết nối loopback đọc trọn bảng socket cho mỗi request: hết RAM | đã sửa ở `021735d` |
| D2 | nặng | khách vẫn làm `security harden` hoàn tác bản vá của cả máy | đã sửa ở `010ad3d` |
| D3 | vừa | trần kết nối không đếm kết nối mở trước `ALTER USER` | đã sửa ở `010ad3d` |
| D4 | vừa | trần theo user không chặn mọi cách chiếm kết nối MariaDB | chưa vá; e2e ghi kết quả đo, câu chữ trong tài liệu đã sửa |
| D5 | vừa | lùi bản trên nginx sau harden không sạch | xử lý bằng câu chữ (mục 6 của bản thiết kế, mục 6.27 của KE-HOACH) |
| D6 | vừa | bộ e2e thiếu các phép mà soát thiết kế hứa | bù một phần ở `e223277`, `2cb8f5a`; phần còn thiếu ghi ở mục 7 của bản thiết kế |
| D7 | nhẹ | đăng nhập đúng không bị giới hạn ở đâu | đã sửa ở `021735d` (giới hạn tốc độ) |
| D8 | nhẹ | K1 còn khoá nguồn ngoài `uid:<n>` | sửa một phần ở `021735d`; phần còn lại chưa vá |
| D9 | nhẹ | đích symlink vào ngữ cảnh AI không bọc `untrusted` | đã sửa ở `010ad3d` |
| D10 | nhẹ | DB rời khôi phục từ thùng rác không có trần, health không báo | đã sửa ở `010ad3d` |
| D11 | nhẹ | `ApacheModOff` không hỏi lại sau `a2dismod` | đã sửa ở `010ad3d` |
| D12 | nhẹ | nhật ký không ghi khoá nguồn | đã sửa ở `021735d` |
| D13 | nhẹ | site có `public` là symlink cùng chủ: Plan trên site bị hoàn tác | chưa xử lý; ghi là hành vi đã biết |

---

### D1 (nặng): Tra UID của kết nối loopback đọc và tách TOÀN BỘ `/proc/net/tcp`, `/proc/net/tcp6` cho mỗi request, trước hàng chờ và trước semaphore argon2, không trần: K1 chưa cần đăng nhập đẩy heap của tpan lên hàng GiB, tức mở lại đúng hậu quả của P2 (panel hết RAM) qua đường mà chính bản vá P1 thêm vào.

**Đường đi.**

1. K1 làm bảng socket của máy lớn lên: python3 dưới user của A nâng `RLIMIT_NOFILE` lên mức hard, mở một listener ở 127.0.0.2 và vài nghìn kết nối tới nó (mỗi kết nối là hai dòng trong `/proc/net/tcp`). Máy đông khách vốn đã có bảng lớn.
2. K1 bắn khoảng 1000 `POST /api/v1/login` (thân JSON nhỏ, mật khẩu sai) cùng lúc tới `https://127.0.0.1:8443` (HTTP/2 cho 250 luồng mỗi kết nối).
3. Mỗi request đi `sourceKey` → `loopbackKey` → `procUID`: `os.ReadFile` cả hai file (không trần), chép sang `string`, rồi `strings.Fields` từng dòng. Việc này xảy ra TRƯỚC `gates.enter`: hàng chờ đầy chỉ trả 429 sau khi đã đọc xong bảng; semaphore argon2 không dính gì tới nó.

Đo trên Mac (cùng mã tách dòng, `FakeSystem` thay `/proc`):

| Bảng `/proc/net/tcp` | Request song song | Cấp phát mỗi request | Heap đỉnh | Mã trả về |
|---|---|---|---|---|
| 202 dòng (mốc) | 200 | 1,4 MiB (chủ yếu 4 lần argon2) | 88 MiB | 4×401, 196×429 |
| 10 002 dòng, 1,3 MiB | 200 | 6,7 MiB | 462 MiB | 4×401, 196×429 |
| 10 002 dòng, 1,3 MiB | 1000 | 5,6 MiB | 2 500 MiB | 4×401, 996×429 |
| 40 002 dòng, 5,1 MiB | 600 | 22,0 MiB | 3 607 MiB | 4×401, 266×423, 330×429 |

Trên VPS 2 đến 4 GiB, tpan bị kernel giết (hoặc kernel giết MariaDB, PHP-FPM: mọi site cùng chết); systemd khởi động lại tpan và K1 lặp lại được. Kernel còn phải sinh lại bảng cho mỗi lần đọc, nên CPU của cả máy cũng bị đốt. Đường này mở cho mọi tiến trình trên máy, không cần mật khẩu nào.

**Bằng chứng.** `internal/server/auth.go` (`sourceKey(r)` trước `s.auth.gates.enter`); `internal/server/auth_m28.go` (`loopbackKey`, `procUID`: `sys.ReadFile`, `string(b)`, `strings.Fields` mọi dòng); `internal/system/local.go` (`os.ReadFile` không trần cho đường ngoài vùng site); `internal/server/server.go` (`http.Server` không có trần số kết nối hay request).

**Đề xuất sửa.** Tra đúng một socket thay vì đọc cả bảng (netlink `sock_diag`), hoặc nếu giữ `/proc`: đọc theo dòng với bộ đệm cố định, so hai cột đầu với chuỗi hex dựng sẵn của 4-tuple, dừng ở dòng khớp, có trần số byte đọc; thêm một semaphore nhỏ cho số lần tra cùng lúc (đầy thì 429 ngay, không đọc); chỉ tra một lần cho mỗi kết nối (`http.Server.ConnContext`).

**Đã sửa (`021735d`).** Giữ `/proc` (để test được qua `system.System`) và làm đủ các ý trên: mỗi kết nối tra nhiều nhất một lần (`authConnContext` gắn ở `ConnContext`); tối đa 2 lần tra cùng lúc, mỗi lần dùng một bộ đệm cố định lấy từ `procBufs`; đọc qua `sys.OpenRead` theo dòng, so hai cột với mẫu dựng sẵn (`procPattern`), không tách dòng không khớp, dừng ở dòng khớp, có trần số byte đọc; hết chỗ tra sau 200 ms thì 429 `auth_busy`: thử lại được, không so và không đếm vào khoá nào. Test: `TestD1BangLonCapPhatNho`, `TestD1TranSoByte`, `TestD1HetChoKhongNho`, `TestD1TraMotLanMoiKetNoi`.

---

### D2 (nặng): S2 chưa đóng: K1 vẫn làm `tpan security harden` thất bại và hoàn tác cho cả máy, nên trên máy nâng cấp F4 (mọi site), H5 và DBCONN không bao giờ được vá; H5 và DBCONN không có đường sửa tự động nào khác.

**Đường đi.**

1. Cách không cần canh giờ: K1 chạy `chmod o+x` trên neo của chính mình (K1 làm chủ neo nên đổi được). Site A vẫn trả 200, nên phép đo trước Plan coi A là lành và kiểm chứng có `Isolated(B, A)`. Phép thử "user của B có đi qua được neo của A không" thoát 0, kết quả lỗi. `Isolated` không được đo trước Plan, và harden không có bước nào đặt lại quyền của neo. Job lỗi, engine hoàn tác mọi bước: vhost của mọi site về bản cũ (F4 mở lại trên nginx cho cả máy), `ApacheModOff.Undo` bật lại `status` (H5), `DBUserLimit.Undo` trả trần về 0 (DBCONN). Admin sửa quyền bằng tay rồi chạy lại thì một vòng lặp của K1 đổi lại trong một giây. Tạm khoá A không giúp: `Isolated` vẫn xét site tạm khoá, và tạm khoá không dừng tiến trình nào của user A.
2. Cách không đổi quyền: A trả 200 lúc đo trước Plan và 500 lúc kiểm chứng. K1 biết lúc nào: file vhost đọc được bằng `proc_open('cat ...')`, thấy dòng mới là trả 500. Hoặc không cần biết gì: một tiến trình nền liên tục mở kết nối DB mới bằng user của A; ngay khi trần 20 có hiệu lực, 20 chỗ thuộc K1 và WordPress của A báo lỗi DB (500) ở mọi lần thử lại của phép HTTP.

Hệ quả: E10 sai trên mọi máy có một khách xấu. F4 còn đường riêng (`tpan health --fix` cho `web.docroot_guard`), nhưng `web.mod_status` và `db.user_limit` không có cách sửa tự động nào, Cause của hai phép chỉ trỏ về harden.

**Bằng chứng.** `internal/core/harden_m14.go` (tập site lành chỉ từ phép HTTP đo trước; `Isolated` cho mọi cặp lành; không có bước đặt lại quyền thư mục site); `internal/checks/site.go` (phép đi qua neo chạy dưới user của site kia); `internal/core/health.go` (`AutoFixName` không có `web.mod_status`, `db.user_limit`); `internal/steps/m28_dot2.go` (hai `Undo`); `internal/core/core_m28_dot2_test.go` (chỉ thử site hỏng TRƯỚC Plan).

**Đề xuất sửa.** Kiểm chứng của harden chỉ gồm phép do chính các bước của Plan quyết định; phép HTTP và `Isolated` của từng site thành báo cáo sau job, không làm job lỗi. Thêm cách sửa tự động cho `web.mod_status` và `db.user_limit`. e2e nâng cấp thêm ca ở ý 1.

**Đã sửa (`010ad3d`).** Trong kiểm chứng của harden, phép HTTP và `Isolated` của mọi site bọc `checks.Soft` (lỗi thành cảnh báo, vẫn nằm trong kết quả của job và nêu đúng site); chỉ bộ phép của stack, phép cấp máy, quyền log, `VhostHardened`, `web.docroot_guard`, `web.mod_status` làm job lỗi. `tpan health --fix` có cách sửa `apache.mod-off` (bước `ApacheModOff`, kiểm chứng chỉ `web.mod_status`) và `db.user-limit` (bước `DBUserLimit`, kiểm chứng chỉ `db.user_limit`). Test: `TestD2HardenKhachXauKhongHoanTac`. e2e: ca N6b ở phần nâng cấp (`e223277`, sửa ở `2cb8f5a`: `chmod o+x` dưới user của site, root trả lại `2750` để giữ bit setgid của neo), harden phải thoát 0 và các phép N8 đến N11 phải đạt.

---

### D3 (vừa): Trần kết nối không đếm các kết nối đã mở trước `ALTER USER`: K1 mở sẵn hơn 130 kết nối trước lúc admin chạy harden thì sau harden vẫn mở thêm được kết nối mới tới khi chạm `max_connections`, mọi site khác trả 500, trong khi `db.user_limit` báo đạt.

**Đường đi.** Máy vừa nâng (trần 0). K1 mở 140 kết nối bằng user DB của A và giữ (kết nối rảnh sống tới `wait_timeout`, mặc định 8 giờ; K1 gửi ping là giữ mãi). 140 < 151 nên các site khác vẫn chạy, không ai để ý. Admin chạy harden, user của A có trần 20. MariaDB chỉ đếm kết nối của một tài khoản có giới hạn từ lúc đăng nhập (đối tượng đếm được tạo trong lúc xác thực, chỉ khi tài khoản đã có giới hạn; theo hiểu biết mã nguồn MariaDB, chưa tra lại trong phiên), nên 140 kết nối cũ không bị đếm, và `ALTER USER` không cắt chúng. K1 mở thêm khoảng 12 kết nối mới (bộ đếm của trần bắt đầu từ 0 nên được nhận) là cạn chỗ: mọi site khác nhận lỗi 1040 và trả 500.

**Bằng chứng.** `internal/steps/m28_dot2.go` (Apply chỉ chạy `ALTER USER`, không đụng kết nối đang mở); `internal/checks/m28.go` (`DBUserLimit` chỉ đọc cột `max_user_connections` và `max_connections`); `scripts/e2e-m28-dot2/dbconn.sh` (luôn mở kết nối sau khi đã có trần, và dọn hết kết nối giữa các pha).

**Đề xuất sửa.** Sau `ALTER USER`, cắt các kết nối đang mở của user đó (`KILL USER`); WordPress mở kết nối mới mỗi request nên chỉ vài request đang chạy bị lỗi một lần.

**Đã sửa (`010ad3d`).** `DBUserLimit.Apply` chạy `KILL USER '<u>'@'localhost'` ngay sau `ALTER USER`; lỗi ở lệnh KILL chỉ ghi cảnh báo (trần đã đặt). Bước thành Danger vì cắt request đang chạy của site; lời `Why` nói rõ Undo không mở lại được kết nối đã cắt. Test: `TestDBUserLimitKillKetNoiCu`. Ca "mở trước harden" chưa có trong e2e.

---

### D4 (vừa): chưa vá.

Kết nối MariaDB chưa đăng nhập không bị trần theo user tính tới, nên trần 20 không phải hàng rào trọn vẹn cho DBCONN. Xử lý trong đợt này: e2e đo và chỉ ghi kết quả (không khẳng định), tài liệu (mục 3.3 và 5 của bản thiết kế, mục 6.27 của KE-HOACH) ghi đúng phạm vi của trần và ghi còn hở. Chi tiết giữ ngoài repo tới khi vá.

---

### D5 (vừa): Lần trả lời soát thiết kế (S7) nói "lùi bản không vỡ", nhưng việc nhận cả dòng cũ lẫn dòng mới nằm ở bản MỚI; bản cũ (v0.2.1, v0.2.2) vẫn đòi đúng dòng `from=$document_root`, nên lùi bản trên nginx sau harden làm mọi Plan trên site bị hoàn tác, và lần ghi vhost đầu tiên của bản cũ mở lại F4.

**Đường đi.** Máy nginx đã nâng và harden (vhost có `disable_symlinks if_not_owner;`). Admin lùi về v0.2.2 (hay v0.2.1). `VhostHardened` của bản cũ không thấy `disable_symlinks if_not_owner from=$document_root;`, báo lỗi cho mọi site; phép này nằm trong kiểm chứng của mọi Plan chạm site, nên mọi Plan trên site không tự ghi vhost (khôi phục backup...) bị hoàn tác. `tpan health --fix` (cho `web.hardened`) hay `tpan security harden` của bản cũ ghi lại vhost theo template cũ: F4 mở lại cho mọi site.

**Bằng chứng.** `git show v0.2.2:internal/checks/m14.go` (so nguyên dòng cũ); `git show v0.2.2:internal/templates/files/site-nginx.conf.tmpl` (dòng cũ). Apache không bị (khối neo mới không làm phép kiểm cũ lỗi).

**Đề xuất sửa.** Sửa mục 6 của bản thiết kế cho đúng; ghi rõ "đã lùi thì đừng chạy harden hay `health --fix` của bản cũ trên nginx", và đích lùi là bản nào.

**Xử lý.** Bằng câu chữ: mục 6 của bản thiết kế và mục 6.27 của KE-HOACH ghi đúng hệ quả trên, đích lùi là "v0.2.2 trở về trước", kèm lời khuyên. Không đổi mã: bản cũ đã phát hành không sửa được.

---

### D6 (vừa): Bộ e2e tới `c249404` thiếu đúng những phép mà lần trả lời soát thiết kế hứa (những phép bắt được D2, D3, D4) và có khẳng định xanh mà không chứng minh điều nó nói.

**Đã bù ở `e223277` và `2cb8f5a`.** N6 khẳng định theo ID phép kiểm (`web.docroot_guard`, `web.mod_status`, `db.user_limit` qua `tpan health --json`); ca D2 (N6b: K1 cho người khác đi qua neo của site mình trước harden; `2cb8f5a` đổi sang `chmod o+x` dưới user của site và để root trả lại `2750`, vì bản đầu chạy `chmod 2751` rồi `chmod 2750` dưới user của site, kernel bỏ bit setgid của neo do user đó không thuộc nhóm `www-data`); P1b đếm `login.fail` trong `audit` (trước đó một bản đặt `authLocked` ngoài lượt vẫn ra 4 lần 401 rồi 423, vì các lần so thừa trả 423 qua nhánh "khoá còn hạn" của `RecordLoginFailure`, nên "không quá 5 lần 401" xanh mà không chứng minh "không quá 5 lần so") và đọc khoá `uid:<n>` trong `login_failures`; phép đo của D4 (chỉ ghi kết quả).

**Còn thiếu lúc soát** (trạng thái hiện tại ở mục 7 của bản thiết kế):

1. Ca "site hỏng TRƯỚC harden" (S2): chỉ có unit test `TestHardenSiteHongVanQua`.
2. Nâng cấp chỉ trên Apache, trong khi chỗ đổi hằng của nginx (`NginxSymlinkGuard` sang `NginxSymlinkGuardV2`, `VhostHardened` nhận cả hai, `DocrootGuard`) chưa chạy thật trên máy nâng cấp nào.
3. Phép đo ghi kết quả cho phần còn hở của Apache (S3) chưa có.
4. Ca D3 (kết nối mở trước harden).
5. F4 chỉ qua cổng 80: `server` 443 của nginx và `<VirtualHost *:443>` của Apache chỉ có golden test.
6. Ca `a2enmod status` rồi health báo `web.mod_status`; ca `apt-get install --reinstall apache2`.
7. P2 chỉ đi nguồn không phải loopback, nên D1 không có phép e2e nào. Khẳng định "có ít nhất một 429" phụ thuộc số nhân: N lấy theo thời gian một lần argon2 đo một mình; trên máy ảo từ 8 nhân, hai lần argon2 chạy song song đủ nhanh để không có 429. Với 2 nhân (mặc định của máy ảo) thì đủ dư.
8. Với mã ở `c249404` thì N7 đỏ đúng chỗ (D2), harden hoàn tác, nên N8 đến N11 đỏ dây chuyền. Sau khi D2 được sửa, N7 phải thoát 0 thì N8 đến N11 mới đo được bản vá; kết quả ở log của bản vá (`docs/bang-chung/M28-dot2.log`).

**Đề xuất sửa.** Theo thứ tự đáng làm: nâng cấp trên nginx; ca D3 vào `dbconn.sh`; ca site hỏng trước harden; F4 qua 443; P2 thêm phép loopback với bảng socket lớn; N của P2 tính theo thời gian của hai lần argon2 chạy song song.

---

### D7 (nhẹ): Đăng nhập ĐÚNG không bị đếm hay giới hạn ở đâu: một tài khoản bất kỳ (K2) chạy vòng đăng nhập đúng từ nhiều dải /64, không bao giờ bị khoá, giữ semaphore argon2 đầy, nên mọi lần đăng nhập khác (kể cả admin) chờ khoảng 3 giây và một phần nhận 429; mỗi vòng còn để lại một dòng `sessions` và một dòng `audit`.

**Đường đi.** Hàng chờ theo khoá nguồn cho mỗi khoá đúng một lần argon2 cùng lúc, và khoá chỉ đến từ lần SAI. K2 mở C vòng song song, mỗi vòng một dải /64 riêng, lặp đăng nhập đúng của chính mình. Semaphore 2 chỗ với hàng chờ chờ 3 giây bão hoà khi C lớn hơn khoảng 3 giây nhân thông lượng argon2. Đo trên Mac (12 nhân, argon2 khoảng 35 đến 48 ms): C=8 thì admin vào trong 0,2 giây; C=160 và C=300 thì mọi lần đăng nhập của admin mất 2,9 đến 3,1 giây (vẫn 204); C=1000 thì 85% request của K2 nhận 429, admin nhận 429 một lần trên năm, các lần còn lại mất 3,0 giây, và 889 dòng `sessions` được tạo trong 15 giây. Trên máy ảo 2 nhân (argon2 khoảng 0,3 giây) cỡ 10 vòng đã bão hoà.

**Bằng chứng.** `internal/server/auth.go` (nhánh đúng: không đếm, không giới hạn, luôn `CreateSession`); `internal/store/auth.go` (`argonRun`: một kênh 2 chỗ, chờ 3 giây; không có trần số phiên mỗi user).

**Đề xuất sửa.** Giới hạn tốc độ cho đăng nhập đúng theo tên và trần số phiên mỗi user. Mục 5 của bản thiết kế khuyên `--private` hoặc tường lửa cho cổng 8443 trên máy nhiều khách.

**Đã sửa (`021735d`).** Hai giới hạn theo cửa sổ một phút (`internal/server/auth_m28.go`): `authRateSource = 30` lần so mật khẩu (đúng hay sai) mỗi khoá nguồn, `authRateName = 30` lần đăng nhập đúng mỗi tên, xét trước khi so; quá thì 429 `auth_busy` với `Retry-After` tới hết phút, không tính là lần sai. Giới hạn theo tên chặn đúng vòng ở trên, vì mọi vòng của K2 dùng chung một tên, và chỉ người biết mật khẩu mới làm đầy được phần của một tên. Trần số phiên mỗi user chưa làm; giới hạn theo tên làm số dòng `sessions` mới tối đa 30 mỗi phút mỗi user. Test: `TestD6DangNhapDungCoGioiHan`, `TestD6GioiHanTheoNguon`, `TestAuthRateDon` (tên test và chú thích trong mã gọi nhầm là D6). Từ chối phục vụ màn đăng nhập nói chung (kẻ có nhiều địa chỉ làm người đăng nhập thật phải chờ) vẫn là giới hạn đã biết, ghi ở mục 5 của bản thiết kế.

---

### D8 (nhẹ): K1 vẫn có khoá nguồn ngoài `uid:<n>`, nên được thêm vài lần 5 lần đoán mỗi 15 phút.

**Phần đã sửa.** Bản vá lúc soát chỉ tra UID khi địa chỉ nguồn là loopback (`127.0.0.0/8`, `::1`), nên PHP của một site nối tới IP công khai hay IP riêng của chính máy có thêm một khoá ngoài `uid:<n>`. Ở `021735d`, kết nối từ chính máy tới IP của máy cũng được tra UID như loopback (`selfConn`). Test: `TestSourceKey`, `TestLoopbackTheoUID`.

**Phần còn lại: chưa vá.** Kẻ chiếm site còn vài khoá nguồn phụ ngoài `uid:<n>`, nên được vài lần đoán hơn 5 mỗi 15 phút. Chi tiết giữ ngoài repo tới khi vá.

---

### D9 (nhẹ): Đích symlink trong bằng chứng của `site.docroot` (chuỗi K1 tự đặt, tới 200 ký tự) đi vào ngữ cảnh AI qua công cụ health của site mà không được bọc `ai.Untrusted` và không bật cờ untrusted của phiên.

**Đường đi.** K1 đặt `public` thành symlink có đích là một câu lệnh cho AI (đích không cần tồn tại). Admin hỏi AI vì sao site A hỏng; công cụ health đưa nguyên chuỗi đó (đã lọc ký tự điều khiển, cắt 200 ký tự) vào kết quả. Chỉ `http.site:*` được bọc, nên phiên không bị đánh dấu untrusted, và Plan do AI đề xuất sau đó không mang cảnh báo "phiên đã đọc nội dung không tin được" trên màn duyệt.

**Bằng chứng.** `internal/checks/m28.go` (bằng chứng `readlink`); `internal/tools/read.go` (`siteHealth` chỉ bọc `http.site:`); `internal/tools/plan.go`. Đường xem job đã bọc mọi output phép kiểm, đường health thì chưa.

**Đề xuất sửa.** Trong `siteHealth`, bọc bằng chứng của `site.docroot` và đặt `c.untrusted = true`.

**Đã sửa (`010ad3d`).** `siteHealth` bọc bằng chứng `readlink` của `site.docroot:*` bằng `ai.Untrusted("symlink-target", ...)` và đánh dấu phiên. Test: `TestSiteHealthDichSymlinkUntrusted`.

---

### D10 (nhẹ): DB rời khôi phục từ thùng rác không được đặt trần, và health không có `db.user_limit` cho DB rời: DB rời tạo trước bản vá, nằm trong thùng rác lúc harden, khôi phục ra là không có trần mà không phép kiểm nào báo.

**Đường đi.** Harden chỉ đi qua DB rời đang active; Plan khôi phục DB rời giữ user cũ (trần 0) và không có bước `DBUserLimit` (lần trả lời soát thiết kế, S16, chỉ thêm cho site). Health chỉ có `db.user_limit` cho user DB của site.

**Bằng chứng.** `internal/core/dbs.go` (`PrepareRestore` của DB rời); `internal/core/harden_m14.go` (chỉ `account.DBActive`); `internal/core/health.go`.

**Đề xuất sửa.** Thêm `DBUserLimit` vào Plan khôi phục DB rời; thêm `db.user_limit` cho DB rời active vào health.

**Đã sửa (`010ad3d`).** Plan `dbs.restore` có `DBUserLimit` sau bước mở khoá user (trần đã có thì bước được bỏ qua); health thêm `db.user_limit` cho mỗi DB rời active, gắn vào máy (`internal/health/collect.go`). Test: `TestRunDBRoiCoTranKetNoi`.

---

### D11 (nhẹ): `ApacheModOff.Apply` không hỏi lại sau `a2dismod`: `status_module` nạp bằng một dòng `LoadModule` ngoài `mods-enabled` thì `a2dismod` không làm gì, bước báo xong, rồi `web.mod_status` trong kiểm chứng lỗi và cả harden hoàn tác (cùng hệ quả D2, lần này do cấu hình của admin).

**Đường đi.** Admin cần trang trạng thái cho công cụ giám sát nên đặt `LoadModule status_module ...` trong một file của `conf-enabled/` (không qua `a2enmod`). Sau khi nâng bản, admin chạy harden để vá F4 và DBCONN: `apachectl -M` thấy `status_module`, bước chạy `a2dismod -q status` (không có link nào để gỡ, thoát 0), kiểm cú pháp, reload, đánh dấu đã tắt. Kiểm chứng: `web.mod_status` vẫn thấy module, lỗi, engine hoàn tác cả vhost lẫn trần DB của mọi site. Lời báo chỉ nói "mod_status đang bật, chạy harden", tức đúng lệnh vừa hỏng.

**Bằng chứng.** `internal/steps/m28_dot2.go` (sau reload chỉ đặt `s.disabled = true`, không hỏi lại); `internal/core/harden_m14.go` (kiểm chứng gồm phép cấp máy, trong đó có `ApacheModStatus`).

**Đề xuất sửa.** Sau `a2dismod` hỏi lại `apachectl -M`; còn nạp thì bước trả lỗi riêng, nói module được nạp ngoài `mods-enabled` (tìm bằng `apachectl -t -D DUMP_INCLUDES`). Cùng hướng D2: `web.mod_status` không nên làm hoàn tác các phần khác của harden.

**Đã sửa (`010ad3d`).** `ApacheModOff.Apply` hỏi lại `apachectl -M` sau `configtest`, trước reload; còn nạp thì trả `steps.ErrModuleLoadedElsewhere` (kèm cách tìm dòng `LoadModule`) và trả `mods-enabled` về như cũ. Trong harden, bước bọc `hardenModOff` nhận lỗi đó, coi bước là xong và hạ `web.mod_status` trong kết quả của job xuống cảnh báo, nên vhost và trần DB vẫn được giữ. `tpan health --fix` dùng bước gốc nên báo lỗi rõ. Cause của `web.mod_status` nói thêm ca này. Test: `TestApacheModOffNapNgoaiModsEnabled`, `TestD11ModStatusNapNgoaiModsEnabled`.

---

### D12 (nhẹ): Nhật ký không ghi khoá nguồn: lần đoán của K1 ghi IP `127.0.x.y`, không ghi `uid:<n>`, nên admin đọc nhật ký không biết site nào đang dò mật khẩu (chỉ lời 423 mới có khoá).

**Đường đi.** K1 đoán mật khẩu admin từ 127.0.1.1, 127.0.1.2... Bản vá đếm đúng theo `uid:<n>` và khoá sau lần thứ năm, nhưng dòng `login.fail` trong `audit` chỉ có địa chỉ 127.0.x.y, cột `target` rỗng. Lời 423 có khoá nhưng chỉ người bị khoá (K1) nhận.

**Bằng chứng.** `internal/server/server.go` (`audit` dùng `clientIP`); `internal/server/auth.go`.

**Đề xuất sửa.** Ghi khoá nguồn vào dòng `login.fail`, `account.password`.

**Đã sửa (`021735d`).** `authFailed` ghi khoá nguồn vào cột `target` của dòng nhật ký (lần sai và lần khoá, cả khi đăng nhập lẫn đổi mật khẩu); không bao giờ ghi tên đã gõ. Test: `TestD12NhatKyGhiKhoaNguon`.

---

### D13 (nhẹ): Hồi quy cho site hợp lệ: site có `public` là symlink CÙNG CHỦ (kiểu thư mục `current` của công cụ triển khai) vẫn được web server phục vụ 200 sau bản vá, nhưng mọi Plan trên site đó bị hoàn tác vì `site.docroot` nằm trong kiểm chứng của mọi Plan chạm site.

**Đường đi.** Khách để `public` → `/var/www/<A>/releases/<bản>` (chủ là user của A). nginx và Apache vẫn trả 200 (cùng chủ). Sau khi nâng bản, `site.docroot` lỗi; phép này nằm trong kiểm chứng của chạy lại `site add`, đổi bản PHP, khôi phục thùng rác, khôi phục backup, mở khoá site và mở khoá khách hàng. Mọi Plan đó bị hoàn tác; site đang tạm khoá thì không mở khoá được. Tạm khoá vẫn chạy. Lần trả lời soát thiết kế (S14) quyết "không hỗ trợ", nhưng chưa nói hệ quả này cho máy nâng cấp.

**Bằng chứng.** `internal/core/site.go` (`siteHardeningChecks` có `checks.SiteDocroot`) và các kiểm chứng dùng `siteChecks` ở `site.go`, `php.go`, `trash.go`, `backup.go`, `suspend.go`, `users.go`.

**Đề xuất sửa.** Tách hai ca trong `site.docroot`: đích thuộc chủ khác thì lỗi, đích cùng chủ thì cảnh báo; hoặc chỉ giữ ở health và Plan ghi vào `public`. Ghi hệ quả cho máy nâng cấp.

**Xử lý.** Chưa đổi mã. Ghi là hành vi đã biết, không phải lỗ (mục 6 của bản thiết kế, mục 6.27 của KE-HOACH): đổi `public` lại thành thư mục thật.

---

## Đã thử, không phá được

**P1, khoá nguồn và hàng chờ.**

- Giả UID của user khác hay của root: cột `uid` của `/proc/net/tcp` là chủ của socket, gán lúc tạo; K1 không lấy được socket do tiến trình của user khác tạo. Không thấy tiến trình root nào gửi POST tới panel thay K1 (health, backup, certbot chỉ gọi site hay kho; chuyển hướng của site chỉ đổi được GET).
- Hai dòng cùng 4-tuple khác UID để làm tra hụt cho người khác: kernel không cho hai socket cùng 4-tuple; socket IPv6 ánh xạ dùng chung bảng với IPv4.
- Đua giữa lúc tra UID và lúc xử lý: UID không đổi trong đời socket; dùng lại 4-tuple khi phía panel còn giữ kết nối thì SYN mới không thành.
- IPv4 ánh xạ (`::ffff:a.b.c.d`), zone, `::1`, client AF_INET6 nối tới 127.0.0.1, listener `0.0.0.0` của Go là socket hai họ: chuẩn hoá và so cột đúng (đọc mã, `TestSourceKey`).
- Bắn song song cùng khoá: `authLocked` nằm trong lượt; unit test E6 đếm đúng 5 lần so; nhánh "khoá còn hạn thì giữ" của `RecordLoginFailure` đúng.
- Bảng theo tên: trừ chéo cửa sổ (S9) bị chặn bởi `first_at` ở cả bảng nhớ lẫn câu SQL; không trừ quá tổng; `first_at` lưu đủ 9 chữ số lẻ nên không lệch khi so; tên khác chữ hoa thường chỉ làm khoá sớm hơn; đổi mật khẩu ghi lần sai theo tên của phiên nên không trừ được lần đoán admin; lấp đầy bảng 10 000 dòng để đẩy dòng của admin ra chỉ làm khoá sớm hơn; `tpan admin unlock` không xoá bảng nhớ nhưng các dòng cũ bị dọn ở lần sai kế hoặc không khớp `first_at`.
- `ErrPasswordBusy` không tính là lần sai: chưa so nên không lộ gì; tên lạ và tên thật đi cùng một đường qua semaphore (S11 đạt).
- Hàng chờ 4 lượt: K1 không chạm được khoá của admin (khác UID hay khác địa chỉ); chỉ người dùng chung nguồn mới làm đầy được, đã ghi ở S10.

**P2.** Chỉ `internal/store/auth.go` gọi argon2, mọi lời gọi qua `argonRun`; CLI dùng `HashPasswordWait` trong tiến trình riêng; hash giả tính trước khi nghe cổng; bộ nhớ sống của argon2 không quá 2 × 64 MiB (đường tra UID của D1 đã sửa riêng).

**F4, nginx.** Bỏ `from=` có ở cả bản thường lẫn khối 443 của bản SSL; khối cổng 80 của bản SSL chỉ có ACME (`/var/lib/tpan/acme`, của root) và chuyển hướng 301; vhost tạm khoá không trỏ vào `public` (`/var/lib/tpan/suspended`); vhost mặc định chỉ phục vụ `AcmeRoot`. nginx mở từng thành phần và so chủ trên thứ vừa mở (theo hiểu biết mã nguồn nginx), nên các dạng đua với symlink, chuỗi symlink, symlink tầng sâu đều không qua; `try_files`, `index`, `X-Accel-Redirect` (URI chứa `..` bị từ chối) đều đi qua phép này; site có `public` là symlink sang site khác nhận 404. Hard link sang file của site khác bị `protected_hardlinks` chặn. FUSE mount của K1 cần `user_allow_other` (mặc định tắt) thì `www-data` mới vào được. Rủi ro hồi quy thấp: chỉ thêm các thành phần `/`, `/var`, `/var/www`, neo, `public` vào phép so chủ; symlink của root trỏ sang thư mục của root (ví dụ `/var/www` chuyển sang ổ khác) vẫn đạt.

**F4, Apache (ca tĩnh).** Khối neo có ở cả `<VirtualHost *:80>` lẫn `*:443` của bản SSL; `Options SymLinksIfOwnerMatch` tuyệt đối nên không còn `FollowSymLinks`; `.htaccess` ở neo không được đọc; `.htaccess` trong `public` (kể cả chế độ `day-du`) chỉ đọc sau khi thành phần `public` đã được xét. `DocrootGuard` đòi khối trong mọi `<VirtualHost>` và lấy dòng `Options` cuối của khối. Phần còn hở trên Apache (S3) đã chốt ở bản thiết kế.

**Phép kiểm mới (D5 của bản thiết kế).** `site.docroot` dùng `Stat` không đi theo symlink và `Readlink` qua cửa; không nằm trong `Isolated` hay kiểm chứng của harden; `php.pool` chỉ ở health, file pool của root nên K1 không làm nó lỗi được; `VhostHardened` mới nhận cả dòng cũ nên Plan trên site cũ không bị hoàn tác trước harden (S5).

**H5.** Không còn trang trạng thái nào khác (`mod_info`, `stub_status`, `pm.status_path` đều không có); `status` nằm trong danh sách tắt dùng chung cho cài stack và chuyển sang Apache; trong mã chỉ `Undo` bật lại `status`; K1 không nạp module được (cần cấu hình của root); K1 không đọc được URL của site khác qua `/proc` (ProtectProc của PHP-FPM) hay log (0640 root:nhóm site). Nâng gói `apache2` không bật lại module admin đã tắt (theo hiểu biết cách đóng gói của Debian, chưa đo).

**DBCONN.** Mọi `CREATE USER` đi qua `dbCreateUserSQL` (site và DB rời); `ALTER USER ... IDENTIFIED BY` (đổi mật khẩu DB) không đụng trần; user của site không tự nâng trần được (cần quyền `CREATE USER`); không có user ẩn danh (`steps.MariaDBSecure`); `DB_HOST=localhost` đi qua socket; nối qua 127.0.0.1 thì hoặc khớp chính tài khoản `'<user>'@'localhost'` (cùng trần) hoặc bị từ chối, không có tài khoản thứ hai không trần; harden giữ trần khác 0 admin đã đặt; khôi phục site từ thùng rác có `DBUserLimit` (S16).

**Harden bỏ qua site hỏng.** Site hỏng sẵn vẫn được ghi vhost và đặt trần (`TestHardenSiteHongVanQua`), nên K1 không né được bản vá trên site của mình bằng cách làm site hỏng; làm site khác "hỏng" trước harden chỉ làm kiểm chứng bớt phép, không làm site đó thiếu bản vá. Đường chặn harden còn lại lúc soát là D2, đã sửa.

## Trả lời của phiên chính

- **Sửa trong đợt 2, kèm test:** `021735d` (D1: tra UID một lần mỗi kết nối, đọc theo dòng có trần, tối đa 2 lần tra cùng lúc; D7: giới hạn 30 lần so mỗi phút mỗi khoá nguồn và 30 lần đăng nhập đúng mỗi phút mỗi tên; D8 một phần: nối tới IP của chính máy cũng tra UID; D12: nhật ký ghi khoá nguồn), `010ad3d` (D2: phép HTTP và `Isolated` trong kiểm chứng của harden chỉ cảnh báo, `tpan health --fix` sửa `web.mod_status` và `db.user_limit`; D3: `KILL USER`, bước là Danger; D9; D10; D11).
- **e2e (D6):** `e223277`, `2cb8f5a`. Phần còn thiếu ghi ở mục 7 của bản thiết kế.
- **Câu chữ:** D4 (phạm vi của trần kết nối, còn hở), D5 (lùi bản trên nginx), D13 (hành vi đã biết) ở bản thiết kế và mục 6.27 của KE-HOACH.
- **Chưa vá:** D4, phần còn lại của D8. Chi tiết giữ ngoài repo tới khi vá.
