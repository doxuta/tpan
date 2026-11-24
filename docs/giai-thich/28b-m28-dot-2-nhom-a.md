# M28 đợt 2 nhóm A: năm lỗ còn lại sau rà soát v0.2.0 (F4 kèm D5, H5, M1-DBCONN, P1, P2)

Đọc kèm: `28-m28-va-theo-ra-soat.md` (đợt 1: cái cửa `sitefs`, lỗ T1), `14-m14-co-lap-va-dinh-tuyen.md` (user Linux của từng site, quyền 2750, `disable_symlinks` của nginx), `13-m13-nhieu-nguoi-dung.md` (vai `admin` và `user`). Đặc tả: mục 6.27 của `docs/KE-HOACH.md`. Bản thiết kế đầy đủ và các phát hiện của thợ soát: `docs/thay-doi/M28-dot-2-thiet-ke.md`, `docs/thay-doi/M28-dot-2-soat-cuoi.md`. Quyết định và lý do: `docs/thay-doi/M28.md` mục 26 đến 36.

Cách đọc: phần "Nói trong một phút" là thứ bạn nói được khi bị hỏi đột xuất. Muốn trả lời sâu thì đọc mục 3. Muốn tự chạy lại thì đọc mục 2. Câu hỏi của người review ở mục 6, đáp án ở cuối file.

Quy ước (giữ như đợt 1, thêm K0):

- **K0**: người ngoài, chưa đăng nhập panel, chỉ gửi được request tới cổng của panel.
- **K1**: mã chạy dưới user Linux của một site (ví dụ PHP của site bị chiếm).
- **K2**: tài khoản panel vai `user`, chủ của site đó.
- **Neo**: thư mục `/var/www/<tên>`.
- Site thử trong log: `e.example.com` (chủ `khach1`, là K1 và K2), nạn nhân `b.example.com`, site của admin `a.example.com`.

Trùng tên cần nhớ: "D5" ở đây là tên của rà soát v0.2.0 cho lỗ "health báo đạt trong lúc F4 mở". Thợ soát cuối của đợt 2 cũng đánh số phát hiện D1 đến D13. Khi nói tới các số đó mình ghi "D2 của soát cuối".

## Nói trong một phút

Đợt 1 chặn việc panel (chạy root) bị lừa đọc file theo đường dẫn do site vẽ. Đợt 2 nhóm A vá năm lỗ khác trong cùng rà soát, mỗi lỗ một câu:

- **F4.** Một site đổi thư mục `public` của nó thành symlink trỏ sang thư mục của site khác, web server đi theo và trả file của site kia dưới tên miền của site này. Bản vá: nginx và Apache từ chối đi theo link nếu chủ của link khác chủ của đích. Thêm phép kiểm để `tpan health` không còn báo "đạt" trong lúc lỗ đang mở (D5).
- **H5.** Apache của Ubuntu bật sẵn trang `/server-status`. PHP của bất kỳ site nào gọi `127.0.0.1/server-status` là thấy URL đang chạy của mọi site, kể cả token đặt lại mật khẩu. Bản vá: tắt module `status`.
- **M1-DBCONN.** Một site mở hết 151 kết nối của MariaDB thì mọi site khác trả 500. Bản vá: user DB của mỗi site chỉ được 20 kết nối cùng lúc.
- **P1.** Luật "sai 5 lần thì khoá" lách được ba đường: bắn song song, xen một lần đăng nhập đúng của người khác, và (từ trong máy) đổi địa chỉ nguồn. Bản vá: đếm theo "khoá nguồn", chạy từng lượt, và đăng nhập đúng chỉ xoá phần lỗi của chính tên đó.
- **P2.** Người chưa đăng nhập gửi hàng chục request đăng nhập song song là panel xin hàng GiB RAM, vì mỗi lần kiểm mật khẩu (argon2id) xin 64 MiB. Bản vá: tối đa 2 lần kiểm cùng lúc, chờ quá 3 giây thì trả 429.

Máy đã cài trước đây không tự được vá lúc nâng cấp. `tpan health` báo ba dòng chưa đạt, và admin chạy `tpan security harden` (có bản xem trước) để vá F4, H5, DBCONN. P1 và P2 là mã của panel nên có hiệu lực ngay khi chạy bản mới.

## 1. Mốc này làm được gì

- Site đổi `public` thành symlink sang thư mục của site khác không còn đọc được file của site đó qua web: nginx trả 404, Apache trả 403. Symlink nằm bên trong `public` mà trỏ vào thứ của chính site vẫn chạy (200).
- `tpan health` có dòng `[lỗi]` khi `public` là symlink, và các dòng báo vhost cũ, `mod_status` còn bật, user DB chưa có trần.
- Trên Apache, `/server-status` không còn trả 200 cho PHP của site.
- User DB của site và của DB rời có trần 20 kết nối: site `e` mở thử 160, được 20, site `b` vẫn 200.
- Sai mật khẩu đăng nhập 5 lần từ cùng một nguồn là bị khoá (423), dù bắn song song, dù xen đăng nhập đúng của người khác, dù K1 đổi địa chỉ nguồn. Admin gõ sai 4 lần rồi gõ đúng thì bộ đếm của admin về 0. Lệnh mới `tpan admin unlock` gỡ khoá mà không đổi mật khẩu.
- 60 request đăng nhập song song từ người ngoài: RSS của tpan không tăng (168 MiB trước và sau), 55 câu trả 429.

Kết quả e2e trên bản vá `v0.2.2-92-gfc7d33a`, mỗi phần từ máy ảo sạch: **175 khẳng định đạt, 0 không đạt** (nginx 53, Apache 40, nâng từ v0.2.0 82), file `docs/bang-chung/M28-dot2.log`. Cùng bộ thử trên bản chưa vá `v0.2.2-4-g8657035`: 67 đạt, 25 không đạt (nginx 37 đạt 15 không, Apache 30 đạt 10 không), file `docs/bang-chung/M28-dot2-truoc-ban-va.log`. Các dòng không đạt ở bản chưa vá là điều mong muốn: nó chứng minh bộ thử bắt được lỗ.

## 2. Chạy thử

Mọi output dưới đây trích nguyên văn từ hai file log trong repo, kèm số dòng. Máy ảo trong log là `tpan-m28`. Phiên viết file này không dùng máy ảo, nên các lệnh "tự chạy" ghi ở mỗi mục là **chưa chạy trong phiên viết**: output dự kiến chỉ lấy từ log của e2e khi cùng thao tác. Nguồn:

- Bản chưa vá: `docs/bang-chung/M28-dot2-truoc-ban-va.log`, gọi tắt là "log chưa vá".
- Bản vá: `docs/bang-chung/M28-dot2.log`, gọi tắt là "log bản vá".
- Script của từng phép thử: `scripts/e2e-m28-dot2/` (`f4.sh`, `h5.sh`, `dbconn.sh`, `p1.sh`, `p2.sh`, `nang-cap.sh`).

Chạy lại cả bộ từ Mac (dùng máy ảo `tpan-m28`, mất vài phút mỗi phần, đừng chạy lúc có việc khác đang dùng máy ảo):

```bash
make e2e-m28-dot2 P="nginx apache nang-cap"
```

### 2.1 F4 và D5: `public` là symlink sang site khác

Log chưa vá, nginx (dòng 100 tới 108): `public` của `e` thành symlink tới thư mục ẩn của `b`, rồi GET:

```
  lrwxrwxrwx 1 tp_e_example_com_25cfc7 www-data 47 Nov 25 00:10 /var/www/e.example.com/public -> /var/www/b.example.com/public/.bimat-aa07dbd3ba
  GET e.example.com/config: HTTP 200, 20 byte
  => KHÔNG ĐẠT: D1: không có chuỗi đánh dấu của B trong thân
  => KHÔNG ĐẠT: D1: mã là 403 hay 404 (HTTP 200)
...
  số dòng health: 33; dòng [lỗi]: 1
  => KHÔNG ĐẠT: health có một dòng [lỗi] nhắc tới public là symlink
```

Hai dòng cuối là D5: health không có dòng nào nói về symlink này, tức admin chạy `tpan health` vẫn thấy "đạt" trong lúc file của `b` đang bị lộ. Chuỗi đánh dấu của B là nội dung file `config` của B, 20 byte.

Log bản vá, nginx (dòng 100 tới 109):

```
  GET e.example.com/config: HTTP 404, 162 byte
  => ĐẠT: D1: không có chuỗi đánh dấu của B trong thân
  => ĐẠT: D1: mã là 403 hay 404 (HTTP 404)

--- [00:27:52] F4.2 (D5). trong lúc lỗ mở: tpan health e.example.com
  số dòng health: 42; dòng [lỗi]: 2
  khớp: [lỗi] Thư mục public của site là thư mục thật của chính site: /var/www/e.example.com/public là symlink, không phải thư mục thật (đích ở bằng chứng)
  => ĐẠT: health có một dòng [lỗi] nhắc tới public là symlink
```

Apache (dòng 325 tới 327 của log bản vá): `GET e.example.com/config: HTTP 403, 278 byte`. Cùng phép thử trên Apache chưa vá cho `HTTP 200` (log chưa vá dòng 320 tới 324).

Ca hợp lệ, symlink nằm BÊN TRONG `public` và trỏ sang thư mục khác của chính `e` (dòng 117 tới 121 của log bản vá):

```
  lrwxrwxrwx 1 tp_e_example_com_25cfc7 www-data 41 Nov 25 00:27 /var/www/e.example.com/public/m28-hople -> /var/www/e.example.com/ban-moi-58c80e4be2
  GET e.example.com/m28-hople/ok.txt: HTTP 200, 23 byte
  => ĐẠT: ca hợp lệ: HTTP 200 (chủ link và chủ đích cùng là tp_e_example_com_25cfc7)
```

Tự chạy (máy ảo, quyền root; stack nginx, đã có `b.example.com` và `e.example.com` như bộ dựng máy của e2e). Chưa chạy trong phiên viết. Bản rút gọn của `f4.sh`:

```bash
UB=$(stat -c %U /var/www/b.example.com); UE=$(stat -c %U /var/www/e.example.com)
runuser -u "$UB" -- mkdir -p /var/www/b.example.com/public/.bimat
echo BI-MAT-B | runuser -u "$UB" -- tee /var/www/b.example.com/public/.bimat/config >/dev/null
runuser -u "$UB" -- chmod 640 /var/www/b.example.com/public/.bimat/config
runuser -u "$UE" -- mv /var/www/e.example.com/public /var/www/e.example.com/public.bak
runuser -u "$UE" -- ln -s /var/www/b.example.com/public/.bimat /var/www/e.example.com/public
curl -s -w '\nHTTP %{http_code}\n' --resolve e.example.com:80:127.0.0.1 http://e.example.com/config
tpan health e.example.com | grep -i 'symlink'
```

Sẽ thấy (theo log bản vá): `HTTP 404` và không có chữ `BI-MAT-B` trong thân (Apache: 403), và một dòng `[lỗi] Thư mục public của site là thư mục thật của chính site ...`. Trên v0.2.0 hay bản chưa vá thì thấy `BI-MAT-B` và `HTTP 200`. Dọn: `runuser -u "$UE" -- rm /var/www/e.example.com/public && runuser -u "$UE" -- mv /var/www/e.example.com/public.bak /var/www/e.example.com/public`.

### 2.2 H5: `/server-status` của Apache

Log chưa vá, Apache (dòng 352 tới 361):

```
  status (enabled by maintainer script)
  user tp_e_example_com_25cfc7 gọi 127.0.0.1/server-status: HTTP 200, 14325 byte
  => KHÔNG ĐẠT: /server-status: câu trả lời không chứa chuỗi đánh dấu của b.example.com
  => KHÔNG ĐẠT: /server-status: mã khác 200 (HTTP 200)
```

Log bản vá (dòng 357 tới 365):

```
  No module matches status (disabled by site administrator)
  user tp_e_example_com_25cfc7 gọi 127.0.0.1/server-status?auto: HTTP 403, 278 byte
  => ĐẠT: /server-status?auto: câu trả lời không chứa chuỗi đánh dấu của b.example.com
  => ĐẠT: /server-status?auto: mã khác 200 (HTTP 403)
```

Dòng đầu là kết quả của `a2query -m status` (script `h5.sh` dòng 27). Trước khi vá, trong trang 14325 byte có tên site `b.example.com`, tức PHP của `e` thấy được request đang chạy của `b`.

Tự chạy (máy ảo, root, stack Apache). Chưa chạy trong phiên viết:

```bash
a2query -m status
UE=$(stat -c %U /var/www/e.example.com)
runuser -u "$UE" -- curl -s -o /dev/null -w '%{http_code}\n' 'http://127.0.0.1/server-status?auto'
```

Sẽ thấy `No module matches status (disabled by site administrator)` rồi `403` trên máy đã vá; trên máy chưa vá là `status (enabled by maintainer script)` rồi `200`.

### 2.3 M1-DBCONN: trần kết nối DB

Log chưa vá, nginx (dòng 139 tới 152):

```
  kết nối của tp_e_example_com_25cfc7 đang mở (root nhìn): 151
  Threads_connected: Threads_connected 152
  mã HTTP của b.example.com: 500 500 500
  thành công: 151 trên 160; lỗi của các kết nối bị từ chối:
          5 ERROR 1040 (08004)
          4 ERROR 2002 (HY000)
  => KHÔNG ĐẠT: e không mở được quá 20 kết nối (151)
  => KHÔNG ĐẠT: b.example.com trả 200 cả ba lần trong lúc e mở kết nối (500 500 500)
```

Log bản vá, nginx (dòng 140 tới 152):

```
  kết nối của tp_e_example_com_25cfc7 đang mở (root nhìn): 20
  Threads_connected: Threads_connected 21
  mã HTTP của b.example.com: 200 200 200
  thành công: 20 trên 160; lỗi của các kết nối bị từ chối:
        140 ERROR 1226 (42000)
  => ĐẠT: e không mở được quá 20 kết nối (20)
  => ĐẠT: b.example.com trả 200 cả ba lần trong lúc e mở kết nối (200 200 200)
```

Lỗi 1040 là "Too many connections" (MariaDB hết 151 chỗ, ai cũng bị từ chối, kể cả `b`). Lỗi 1226 là "user đã vượt `max_user_connections`": chỉ `e` bị từ chối. Trần hiện ra trong cột `max_user_connections` của `mysql.user`: `0` trên v0.2.0 (log bản vá dòng 671), `20` sau harden (dòng 823).

Tự chạy (máy ảo, root). Tên user DB của site giống tên user Linux của site (log dòng 132: `tp_e_example_com_25cfc7` cho cả hai). Chưa chạy trong phiên viết:

```bash
DBU=$(stat -c %U /var/www/e.example.com)
mariadb -N -B -e "SELECT max_user_connections FROM mysql.user WHERE User = '$DBU' AND Host = 'localhost'"
tpan health e.example.com | grep -i 'trần'
```

Sẽ thấy `20` trên site tạo bằng bản vá (hay đã harden), `0` trên site tạo bằng bản cũ chưa harden, kèm dòng `[cảnh báo] User DB có trần số kết nối cùng lúc: User DB ... không có trần số kết nối`.

### 2.4 P1: khoá đăng nhập

Log chưa vá, nginx (dòng 168 tới 171): K1 (user của `e`) thử 20 lần sai mật khẩu admin, mỗi lần một nguồn `127.0.0.x`, cứ 4 lần sai xen 1 lần đăng nhập đúng của `khach1`:

```
  lần:mã = 1:401 2:401 3:401 4:401 (khach1 đúng:204) 5:401 6:401 7:401 8:401 (khach1 đúng:204) 9:401 ...
  số 401: 20; lần sai đầu tiên bị 423: 0 (0 là không lần nào)
  => KHÔNG ĐẠT: P1a: có 423, chậm nhất ở lần sai thứ 6 (đầu tiên ở lần 0)
  => KHÔNG ĐẠT: P1a: không quá 5 lần so mật khẩu sai (401 x20)
```

Log bản vá (dòng 168 tới 174):

```
  lần:mã = 1:401 2:401 3:401 4:401 (khach1 đúng:204) 5:423 6:423 7:423 8:423 (khach1 đúng:423) 9:423 ...
  số 401: 4; lần sai đầu tiên bị 423: 5 (0 là không lần nào)
  => ĐẠT: P1a: có 423, chậm nhất ở lần sai thứ 6 (đầu tiên ở lần 5)
  => ĐẠT: P1a: không quá 5 lần so mật khẩu sai (401 x4)
  sau đó, ĐÚNG mật khẩu admin, cùng user, nguồn mới: HTTP 423
  => ĐẠT: P1a: đang khoá thì mật khẩu admin đúng cũng 423
```

Chú ý: lần đăng nhập đúng của `khach1` sau lần sai thứ 4 (204) không làm bộ đếm của admin về 0; lần sai thứ 5 vẫn khoá. Và dù mỗi lần một nguồn `127.0.0.x`, mọi lần tính chung một khoá.

Bắn 20 lần sai song song, cùng nguồn (P1b). Log chưa vá (dòng 177 tới 183): 16 câu 401, 4 câu 423, `login.fail` trong nhật ký `audit` là 20, và không có dòng khoá nào ở khoá `uid:994`. Log bản vá (dòng 177 tới 185):

```
  mã       4 401
  mã      16 429
  => ĐẠT: P1b: số câu 401 không quá 5 (4), còn lại chỉ 423 hay 429
  sau loạt song song, các lần sai tuần tự: 423; tổng 401: 4
  => ĐẠT: P1b: khoá nằm ở khoá nguồn uid:994 trong login_failures (dòng: 1)
  dòng login.fail mới trong audit: 5
  => ĐẠT: P1b: không quá 5 lần so mật khẩu sai thật (login.fail trong audit: 5)
```

16 trên 20 request nhận 429 vì hàng chờ của một khoá nguồn chỉ có 4 chỗ. Vì thế chỉ 4 lần được so mật khẩu (401), chưa đủ 5 để khoá; các lần gửi tuần tự sau đó mới chạm 423. Con số đúng để khẳng định là số dòng `login.fail` trong `audit` (5), không phải số mã 401: đây là chỗ e2e từng xanh mà không chứng minh gì (mục 5.3).

Ca hợp lệ P1c (dòng 187 tới 196 của log bản vá): admin sai 4 lần rồi đúng (204), rồi sai 4 lần nữa vẫn 401 (bộ đếm đã về 0), lần sai thứ 5 là 423. Rồi:

```
  | Đã gỡ khoá đăng nhập của uid:993.
  => ĐẠT: P1c: sau tpan admin unlock, mật khẩu đúng vào được (HTTP 204)
```

Trên bản chưa vá, `tpan admin unlock` chưa có: lệnh in trợ giúp của `tpan admin` (log chưa vá dòng 193 tới 195) và mật khẩu đúng vẫn 423.

Tự chạy (máy ảo, root; panel nghe ở cổng 8443 với chứng chỉ tự ký nên có `-k`). Root nối tới `127.0.0.1` nên khoá nguồn là `uid:0`. Chưa chạy trong phiên viết:

```bash
for i in 1 2 3 4 5 6; do
  curl -sk -o /dev/null -w "lần $i: %{http_code}\n" -H 'Content-Type: application/json' \
    -d '{"name":"admin","password":"sai-roi"}' https://127.0.0.1:8443/api/v1/login
done
tpan admin unlock uid:0
```

Sẽ thấy, theo kiểu log P1c: `401` bốn lần, rồi `423` ở lần 5 và 6. Lệnh cuối gỡ khoá; thông báo của nó có dạng `Đã gỡ khoá đăng nhập của <khoá>.` như dòng 195 của log bản vá. Lưu ý: lúc đang khoá, mọi lần đăng nhập của root qua loopback (kể cả đúng mật khẩu) nhận 423 tới khi gỡ.

### 2.5 P2: trần argon2id

Log chưa vá, nginx (dòng 203 tới 218): 54 request đăng nhập song song, mỗi cái một địa chỉ riêng:

```
  một lần đăng nhập sai mất 0.150104s; N = 54
  mã      54 401
  thời gian: nhanh nhất 0.893692s, chậm nhất 8.583371s
  RSS đỉnh 2044 MiB, tăng 18 MiB so với trước; 429: 0; dòng OOM mới trong dmesg: 0
  => KHÔNG ĐẠT: có ít nhất một câu 429 (0 trên 54)
```

Đọc cho đúng: ở lần chạy này RSS ban đầu của tpan đã là 2026 MiB (dòng 207: `RSS trước: 2026 MiB`), tức tiến trình đã phình sau các phép thử P1 chạy ngay trước đó. Nên khẳng định "RSS tăng không quá 400 MiB" xanh (tăng 18) mà không chứng minh gì. Điều bản chưa vá làm sai đo được ở đây là: cả 54 request đều được so mật khẩu (0 câu 429), mất tới 8,6 giây. Con số "N = 20 thì RSS lên 1255 MiB" nằm ở báo cáo rà soát v0.2.0 (thiết kế mục 1), không nằm trong log này.

Log bản vá (dòng 203 tới 223):

```
  một lần đăng nhập sai mất 0.113833s; N = 60
  RSS trước: 168 MiB; NRestarts 0
  mã       3 401
  mã       2 423
  mã      55 429
  thời gian: nhanh nhất 0.015454s, chậm nhất 0.603559s
  RSS đỉnh 168 MiB, tăng 0 MiB so với trước; 429: 55; dòng OOM mới trong dmesg: 0
  => ĐẠT: có ít nhất một câu 429 (55 trên 60)
```

Số 429 gộp mọi chỗ trả `auth_busy`: hàng chờ của khoá nguồn, chỗ tra UID, và chỗ argon2; log không tách riêng. Cẩn thận: phép thử P2 trong log này (N = 60) phần lớn chưa tới chỗ argon2, vì mọi request chung khoá `uid:0` (3 câu 401, 2 câu 423). Phép thử đã được viết lại để mỗi request một khoá nguồn; số liệu mới và lý do ở mục 4 của `28c-m28-dot-2-nhom-b.md`. Phép thử này chưa chạm đường "kết nối loopback với bảng socket lớn" của D1 (mục 5.3).

Tự chạy: bộ đo đầy đủ là `scripts/e2e-m28-dot2/p2.sh` (gắn nhiều địa chỉ tạm vào `lo`), chạy qua `make e2e-m28-dot2 P=nginx` từ Mac. Cách nhìn nhanh trên máy ảo, chưa chạy trong phiên viết:

```bash
grep VmRSS /proc/$(systemctl show -p MainPID --value tpan)/status
```

Lệnh in RSS hiện tại của tpan (đơn vị kB). Chạy trước và sau một loạt đăng nhập sai song song để so.

### 2.6 Nâng cấp từ v0.2.0

Phần `nang-cap` của log bản vá (dòng 407 tới 864): cài v0.2.0 bằng `install.sh`, dựng hai site WordPress của admin và site `e`, đo lỗ (F4, H5, DBCONN đều lộ: dòng 633, 662, 679, 683), thay file chạy bằng bản của cây, rồi:

```
  số dòng [lỗi] hay [cảnh báo]: trước khi nâng 7, sau khi nâng 14
  | [cảnh báo] User DB có trần số kết nối cùng lúc: User DB tp_a_example_com_7653e4 không có trần số kết nối
  | [lỗi] Vhost không đi theo public khi nó là symlink sang chủ khác: Vhost chưa có bản vá F4: <VirtualHost *:80> (5) thiếu khối <Directory "/var/www/a.example.com"> với Options SymLinksIfOwnerMatch
  | [lỗi] mod_status của Apache đang tắt: mod_status của Apache đang bật
```

(trích dòng 722, 725, 728; ba site nên mỗi loại đầu có ba dòng). Rồi K1 mở quyền neo của site mình (`chmod o+x`, dòng 735: `/var/www/e.example.com 751`) và admin chạy harden:

```
  --- [00:34:10] N7. tpan security harden --yes
  |   [cảnh báo] Site b.example.com không chạm được vào file, log và socket của site e.example.com: tp_b_example_com_cd59d5 CHẠM ĐƯỢC vào site e.example.com ...
  |   [đạt] Vhost không đi theo public khi nó là symlink sang chủ khác: Vhost có dấu của bản vá F4: ...
  |   [đạt] User DB có trần số kết nối cùng lúc: User DB tp_e_example_com_25cfc7 có trần 20 kết nối (max_connections là 151)
  => ĐẠT: security harden --yes thoát 0 dù neo của e.example.com bị K1 mở o+x (soát cuối D2)
```

Harden thoát 0, hai site WordPress vẫn 200 và đăng nhập được (cookie `wordpress_logged_in_`, dòng 758 tới 766), rồi F4, H5, DBCONN chuyển sang chặn (dòng 770 tới 852). Dòng `[cảnh báo]` đầu là phép cô lập của site `e` bị K1 làm lỗi: nó chỉ được báo, không làm cả harden bị hoàn tác (mục 3.6).

## 3. Đi qua mã theo thứ tự chạy

Thứ tự: mỗi lỗ đi từ chỗ người dùng hay K1 chạm vào tới lúc máy đổi. Các lỗ độc lập nhau nên chia theo lỗ.

### 3.1 F4 chặng 1: dòng `disable_symlinks` của nginx

`internal/templates/files/site-nginx.conf.tmpl` dòng 19 (và bản SSL `site-nginx-ssl.conf.tmpl` dòng 54):

```
    disable_symlinks if_not_owner;
```

Hằng tương ứng ở `internal/templates/m14.go`:

```go
const NginxSymlinkGuard = "disable_symlinks if_not_owner from=$document_root;"   // bản M14, chỉ để nhận vhost cũ
const NginxSymlinkGuardV2 = "disable_symlinks if_not_owner;"
```

Cách nginx hiểu dòng này: `if_not_owner` là "chỉ đi theo symlink khi chủ của symlink cũng là chủ của thứ nó trỏ tới". `from=$document_root` bảo nginx bỏ qua mọi thành phần của đường dẫn từ `root` trở lên. Mà `public` chính là `root`, nên khi `public` là symlink thì nginx không xét nó và đi theo. Bỏ `from=` thì nginx xét mọi thành phần: `/`, `/var`, `/var/www`, neo (đều của root, không phải symlink, không bị gì) và `public` (symlink của user `e` trỏ sang đích của user `b`: khác chủ, từ chối). Chọn bỏ hẳn `from=` thay vì `from=<neo>` để dòng là một hằng chung cho mọi site, không phải dựng theo từng site.

### 3.2 F4 chặng 2: khối neo của Apache

`internal/templates/files/site-apache.conf.tmpl` dòng 30 tới 33 (có ở mọi `<VirtualHost>`, kể cả cổng 80 và 443 của bản SSL):

```
    <Directory "{{.Dir}}">
        Options SymLinksIfOwnerMatch
        AllowOverride None
    </Directory>
```

`{{.Dir}}` là neo. File `apache2.conf` của Ubuntu đặt `Options Indexes FollowSymLinks` cho cả `/var/www/`, nên với thành phần `public` Apache vẫn đi theo link. Khối trên đè lại cho riêng neo: `SymLinksIfOwnerMatch` là "đi theo symlink khi chủ link trùng chủ đích", cùng ý nghĩa với `if_not_owner` của nginx. Apache hết 403 khi khác chủ, 200 khi cùng chủ (đo ở nghiên cứu N1c của thiết kế). Khối này chỉ đổi `Options`, không cho thêm quyền truy cập nào. Bản vá này chặn ca tĩnh trên Apache; phần còn hở nằm ở mục 8.

### 3.3 F4 chặng 3: phép kiểm cho health (D5)

Ba phép kiểm mới trong `internal/checks/m28.go`. Phép thứ nhất là `SiteDocroot` (ID `site.docroot`, dòng 59):

```go
info, ev, missing, err := siteStat(sys, l.Public)
...
case info.Mode&fs.ModeSymlink != 0:
	// Đích chỉ vào bằng chứng, không vào Summary hay Cause (S14).
	linkEv := Evidence{Command: "readlink " + l.Public + " (tpan)"}
	if target, err := sys.Readlink(l.Public); err != nil {
		linkEv.ExitCode, linkEv.Output = -1, err.Error()
	} else {
		linkEv.Output = untrustedLine(target, docrootLinkMax)
	}
	return b.bad(Fail, ... "là symlink, không phải thư mục thật" ...)
```

`siteStat` dùng `sys.Stat` của cửa `sitefs` (đợt 1), không đi theo symlink, nên `public` là symlink thì phép thấy chính cái link và báo lỗi. Đích của link là chuỗi do K1 đặt, nên chỉ nằm trong phần bằng chứng, đã lọc ký tự điều khiển và cắt 200 ký tự (`untrustedLine`), và khi công cụ health của AI đọc nó thì được bọc `ai.Untrusted("symlink-target", ...)` (D9 của soát cuối).

Phép thứ hai là `DocrootGuard` (ID `web.docroot_guard`, dòng 123): đọc vhost của site và kiểm có dấu của bản vá. Với nginx, hàm `nginxGuardGaps` (dòng 205) đòi có đúng một dòng `NginxSymlinkGuardV2` và không có dòng `disable_symlinks` nào khác (dòng cũ có `from=`, hay `disable_symlinks off;` trong một `location`, đều mở lại lỗ). Với Apache, mọi `<VirtualHost>` phải có khối `<Directory "<neo>">` mà dòng `Options` cuối cùng là `Options SymLinksIfOwnerMatch`. Phép thứ ba là `PHPPool` (ID `php.pool`, dòng 339): pool PHP-FPM của site phải nghe đúng socket riêng, không phải cổng TCP (pool nghe TCP thì PHP của mọi site gửi request thẳng vào được), đúng user và nhóm. Cả `php.pool` lẫn `web.docroot_guard` chỉ ở health và (với `web.docroot_guard`) kiểm chứng của harden. Lý do ở mục 5.

Phép cũ `VhostHardened` (`internal/checks/m14.go` dòng 461) sửa lại để nhận cả dòng cũ lẫn dòng mới:

```go
if !has(templates.NginxSymlinkGuard) && !has(templates.NginxSymlinkGuardV2) {
```

### 3.4 H5 chặng 1: danh sách module tắt dùng chung

`internal/steps/m28_dot2.go` dòng 35:

```go
var ApacheModulesOff = []string{"mpm_prefork", "status"}
```

Một danh sách cho hai nơi: cài stack và chuyển web server sang Apache. Hai Plan đó vốn khởi động lại Apache vì đổi MPM, nên tắt thêm `status` không tốn gì. Máy cài mới thì `status` tắt ngay từ đầu.

### 3.5 H5 chặng 2: bước `ApacheModOff` trong harden

Máy cũ vá bằng bước `steps.ApacheModOff{Module: "status"}` (ID `apache.mod-off:status`). Thứ tự trong `Apply` (từ dòng 119):

1. `s.loaded(...)` chạy `apachectl -M` và tìm dòng có cột đầu đúng `status_module`. Cột đầu phải so đúng chuỗi, vì `proxy_status_module` cũng chứa chữ `status`.
2. `a2dismod -q status`, rồi `apachectl configtest`.
3. Hỏi lại `apachectl -M` (D11 của soát cuối): nếu `status_module` vẫn nạp thì trả `ErrModuleLoadedElsewhere` kèm lệnh tìm dòng `LoadModule` (`apachectl -t -D DUMP_INCLUDES`) và trả `mods-enabled` về như cũ.
4. `systemctl reload apache2` (`runReload`).

Hỏng ở bước nào thì `s.enable(...)` bật lại module trước khi trả lỗi. Hai lý do cho các chi tiết này: dùng `apachectl -M` thay `a2query -m` vì `a2query` chỉ thấy module bật qua `mods-enabled`; và `reload` thay vì `restart` để không cắt kết nối và lần tải lên đang dở của site nào.

Trong harden, bước này bọc bằng `hardenModOff` (`internal/core/harden_m14.go` dòng 584): gặp `ErrModuleLoadedElsewhere` thì coi như xong và hạ phép `web.mod_status` xuống cảnh báo, để một cấu hình lạ của admin không làm hoàn tác bản vá vhost và trần DB của cả máy. Phép kiểm `ApacheModStatus` (ID `web.mod_status:apache2`, `internal/checks/m28.go` dòng 454) báo lỗi khi `apachectl -M` còn `status_module`; nó không nằm trong bộ phép của `tpan update`, vì admin có thể cố ý bật lại `mod_status` để giám sát.

### 3.6 Harden không còn "tất cả hoặc không"

Bản thiết kế đầu làm kiểm chứng của harden gồm phép HTTP và phép cô lập của từng site. Một site bị chiếm cố ý làm hai phép đó Fail (ví dụ `chmod o+x` thư mục neo của chính nó, vì K1 làm chủ neo) thì cả job lỗi, engine hoàn tác mọi bước: vhost của mọi site về bản cũ, bật lại `status`, trả trần DB về 0. Sửa (D2 của soát cuối) bằng `checks.Soft` (`internal/checks/run.go` dòng 62): phép bọc `Soft` có lỗi thì thành cảnh báo, vẫn nằm trong kết quả của job. Chỉ các phép do chính các bước của Plan quyết định mới làm job lỗi và hoàn tác: bộ phép của stack, phép cấp máy của M14, quyền log, `VhostHardened`, `web.docroot_guard`, `web.mod_status`. Đây là lý do dòng `[cảnh báo] Site b.example.com không chạm được ...` ở mục 2.6 không làm harden thất bại.

Site đang trả lời sai TRƯỚC khi dựng Plan thì `hardenHealthy` (`internal/core/harden_m14.go` dòng 469) loại nó ra khỏi các phép HTTP, nhưng vẫn vá đủ mọi bước cho nó, và nêu tên nó trong bản xem trước (`Harden.Skipped`).

### 3.7 M1-DBCONN chặng 1: tạo user có trần

`internal/site/m28.go` dòng 14 và `internal/steps/site_db.go` dòng 90:

```go
const DBMaxUserConnections = 20

func dbCreateUserSQL(name, password string) string {
	return "CREATE USER '" + name + "'@'localhost' IDENTIFIED BY '" + password +
		"' WITH MAX_USER_CONNECTIONS " + strconv.Itoa(site.DBMaxUserConnections) + ";\n"
}
```

Câu này dùng chung cho user DB của site và của DB rời (M13), nên sửa một chỗ là đủ. Vì sao 20: pool của site có `pm.max_children = 5`, WordPress giữ tối đa một kết nối mỗi request PHP, cộng các lệnh `wp` của tpan chạy cùng lúc không quá 4: tối đa khoảng 9 kết nối hợp lệ, 20 là gấp đôi, kể cả lúc đổi bản PHP (hai pool chạy cùng lúc). Mật khẩu có mặt trong câu này nên engine không in nó ra bản xem trước.

### 3.8 M1-DBCONN chặng 2: user đã có, bước `DBUserLimit`

`internal/steps/m28_dot2.go`, `DBUserLimit.Apply` (từ dòng 281, trích rút gọn):

```go
exists, limit, err := DBUserLimitState(ctx, sys, s.User)
...
s.applied, s.changed, s.prev = true, false, limit
if !exists || limit != 0 {
	return nil
}
if err := dbRun(ctx, sys, dbLimitSQL(s.User, s.Max), "", "đặt trần kết nối cho user DB "+s.User); err != nil {
	return err
}
s.changed = true
if err := dbRun(ctx, sys, dbKillSQL(s.User), "", "cắt kết nối đang mở của user DB "+s.User); err != nil {
	slog.Warn("đã đặt trần kết nối nhưng chưa cắt được kết nối đang mở", "user", s.User, "err", err)
}
```

Đọc từng dòng:

- `DBUserLimitState` hỏi `SELECT COUNT(*), COALESCE(MAX(max_user_connections), 0) FROM mysql.user WHERE User='..' AND Host='localhost'`, không hỏi `SHOW CREATE USER`, vì câu đó in cả hash mật khẩu mà engine ghi mọi dòng output vào log của job.
- Trần đang khác 0 thì bỏ qua: admin đã cố ý nâng trần cho một site lớn thì giữ.
- Câu đặt trần là `ALTER USER '<u>'@'localhost' WITH MAX_USER_CONNECTIONS 20;`.
- Câu `KILL USER '<u>'@'localhost';` đi liền sau, vì MariaDB chỉ đếm kết nối mở SAU khi tài khoản có trần, và `ALTER USER` không cắt kết nối đang mở. Bản đầu thiếu câu này (D3 của soát cuối, mục 5.3). Vì nó cắt request đang chạy của site nên `Info()` đặt `Danger: true`, để bản xem trước nói rõ. `Undo` trả trần cũ nhưng không mở lại được kết nối đã cắt.

Phép kiểm `DBUserLimit` (ID `db.user_limit:<user>`, `internal/checks/m28.go` dòng 499) cảnh báo khi trần là 0, hoặc lớn hơn `@@GLOBAL.max_connections` (trần không có tác dụng; không sửa tự động vì tpan không đè trần admin đã chọn). Không đặt `max_user_connections` toàn máy trong `99-tpan.cnf`: trần toàn máy áp cho cả root của MariaDB, mà các lệnh của tpan chạy bằng root.

### 3.9 P1 chặng 1: khoá nguồn

`internal/server/auth_m28.go`, `sourceKey` (dòng 73):

```go
a = a.Unmap().WithZone("")
switch {
case a.IsLoopback() || selfConn(r, a):
	return loopbackKey(r)
case a.Is4():
	return a.String()
default:
	return netip.PrefixFrom(a, 64).Masked().String()
}
```

Mọi thứ liên quan tới đếm sai (cột `ip` của bảng `login_failures`, mutex, hàng chờ, bảng theo tên, giới hạn tốc độ) dùng CÙNG chuỗi này. IPv4 (kể cả dạng ánh xạ `::ffff:a.b.c.d`) giữ nguyên; IPv6 lấy dải /64 vì một máy thường có cả dải; còn kết nối từ chính máy (loopback, hoặc chính máy nối tới IP công khai của nó, hàm `selfConn`) thì dùng `loopbackKey`, kết quả có dạng `uid:<n>`. Lý do: trên Linux mọi địa chỉ `127.x.y.z` đều là của máy, và PHP của một site chọn được địa chỉ nguồn tuỳ ý trong cả dải (`bindto` của `stream_context_create`, hay `curl --interface`). Đếm theo địa chỉ thì mỗi địa chỉ có 5 lần đoán riêng, tức gần như không giới hạn. UID thì mỗi user Linux của site chỉ có một khoá, đổi địa chỉ thế nào cũng vậy. Admin vào qua `ssh -L` mang UID của phiên SSH, khác UID của site nào.

### 3.10 P1 chặng 2: tra UID mà không hết RAM

`loopbackKey` gọi `lookupLoopback`, hàm này đọc `/proc/net/tcp` và `/proc/net/tcp6` qua `system.System`, tìm dòng có local là địa chỉ:cổng nguồn của request và remote là địa chỉ:cổng panel nhận kết nối; cột `uid` của dòng đó là UID. Bản đầu đọc trọn hai file vào bộ nhớ cho mỗi request (D1 của soát cuối), trước hàng chờ và trước semaphore argon2, nên một site mở vài nghìn kết nối rồi bắn vài trăm request là đẩy heap của tpan lên hàng GiB, tức mở lại đúng hậu quả của P2. Bản cuối:

```go
procBufs = func() chan *bufio.Reader { ... procLookups bộ đệm cố định ... }()

func authConnContext(ctx context.Context, _ net.Conn) context.Context {
	return context.WithValue(ctx, authConnKey{}, &authConn{})
}
```

- Mỗi kết nối tra nhiều nhất một lần. `authConnContext` là `http.Server.ConnContext`: gắn một chỗ nhớ (`authConn`) vào kết nối, UID không đổi trong đời socket, nên các request (kể cả luồng HTTP/2) đi trên kết nối đó dùng lại kết quả.
- Cùng lúc tối đa `procLookups = 2` lần tra, mỗi lần lấy một bộ đệm cố định từ kênh `procBufs`. Lấy được bộ đệm là được tra; kênh này vừa là semaphore vừa là kho bộ đệm.
- Đọc theo dòng, so hai cột đầu với chuỗi hex dựng sẵn của 4-tuple, dừng ở dòng khớp, mỗi file đọc tối đa `procMaxBytes` (16 MiB).
- Hết chỗ sau `procWait = 200 ms` thì `sourceKey` trả `authLookupBusy` và `handleLogin` trả 429 ngay, không so mật khẩu và không đếm vào khoá nào. Không rơi về khoá chung `loopback`: kẻ làm đầy chỗ tra sẽ đẩy lần đoán của chính nó vào khoá chung, khoá cả admin đang vào qua `ssh -L`.
- Không tra được (không phải Linux, không thấy dòng) thì khoá chung `loopback`: an toàn hơn là cho qua.

### 3.11 P1 chặng 3: mỗi khoá một lượt, rồi mới so mật khẩu

`internal/server/auth.go`, `handleLogin` (dòng 387), đoạn đầu:

```go
ip, src := clientIP(r), sourceKey(r)
if src == authLookupBusy {
	authWriteBusy(w) // chưa biết khoá nguồn: không so, không đếm gì
	return
}
leave, ok := s.auth.gates.enter(r.Context(), src)
if !ok {
	authWriteBusy(w)
	return
}
defer leave()
if s.authLocked(w, r, src) {
	return
}
```

Rồi tới giới hạn tốc độ (`s.auth.rate.allow`), so mật khẩu (`authCheckPassword`), và lần sai thì `authFailed`. `authGates.enter` (`auth_m28.go` dòng 341) giữ một mutex cho mỗi khoá nguồn: các lần thử của cùng một khoá chạy lần lượt. Hai hệ quả:

- `authLocked` nằm trong đoạn giữ lượt, nên lần thử sau luôn thấy kết quả khoá của lần trước. Đây là chỗ đóng lỗi 1 (đua giữa kiểm và ghi): trước đây 20 request song song cùng đi qua kiểm "đã khoá chưa" trước khi lần sai thứ 5 kịp ghi khoá.
- Một khoá chỉ có tối đa `authMaxQueue = 4` lượt (đang chạy cộng đang chờ); lượt thứ 5 nhận 429 ngay, không xếp hàng vô hạn. Đây là lý do log bản vá có 16 câu 429 trong loạt 20 request song song.

`handlePasswordChange` (dòng 525) đi cùng đường vì nó cũng so mật khẩu.

### 3.12 P1 chặng 4: ghi lần sai, giữ khoá, trừ phần của đúng tên

`internal/store/auth.go`, `RecordLoginFailure` (dòng 481), nhánh quan trọng:

```go
if lu.Valid {
	if f.Until, err = parseTime(lu.String); err != nil { ... }
	if now.Before(f.Until) {
		f.Locked = true
		return f, nil
	}
}
if lu.Valid || now.Sub(f.FirstAt) > window {
	f.Failures, f.FirstAt, f.Until = 0, now.UTC(), time.Time{}
}
```

Lỗi 2: bản cũ, khoá còn hạn mà vẫn có một lần sai lọt vào (do lỗi 1), hàm thấy `locked_until` có giá trị, đếm lại từ 1 và ghi `locked_until = NULL`: chính lần sai làm mất khoá. Giờ khoá còn hạn thì trả về ngay, không đụng gì. Chỉ khi khoá đã hết hạn hoặc lần sai đầu đã quá cửa sổ 15 phút mới đếm lại từ 1.

Lỗi 3: bản cũ gọi `ClearLoginFailures(ip)` ở mọi lần đăng nhập đúng, xoá bộ đếm của cả nguồn. Giờ `handleLogin` dùng `ForgetLoginFailures(ctx, src, n, firstAt)` (dòng 540), chỉ trừ `n` lần, `n` lấy từ bảng `authNames` trong bộ nhớ: `(khoá nguồn, HMAC-SHA256 của tên) → (số lần sai, first_at của dòng DB lúc ghi)`. Tên đã gõ không bao giờ được lưu (người ta hay gõ nhầm mật khẩu vào ô tên), chỉ có HMAC với một khoá ngẫu nhiên sinh lúc tiến trình khởi động. Câu SQL chỉ trừ khi `first_at` còn trùng và dòng không mang khoá, và không trừ dưới 0. Nhờ vậy lần đăng nhập đúng của `khach1` chỉ trừ phần lỗi của chính `khach1` (0), lần sai của admin vẫn còn: đúng ca ở log bản vá (lần 4 sai rồi `khach1` đúng 204, lần 5 vẫn khoá).

Panel khởi động lại thì bảng theo tên mất, tổng trong DB vẫn còn: chỉ có thể khoá sớm hơn, không bao giờ muộn hơn.

### 3.13 P1 chặng 5: giới hạn tốc độ và lối ra

Lần SAI đã bị khoá sau 5 lần, nhưng lần ĐÚNG trước đây không bị đếm ở đâu: một tài khoản bất kỳ chạy vòng đăng nhập đúng từ nhiều nguồn là giữ đầy hai chỗ argon2 (D7 của soát cuối). `internal/server/auth_m28.go` dòng 481 tới 482:

```go
authRateSource = 30
authRateName   = 30
```

Mỗi khoá nguồn tối đa 30 lần so mật khẩu mỗi phút (đúng hay sai); mỗi tên tối đa 30 lần đăng nhập đúng mỗi phút, xét trước khi so. Quá thì 429 `auth_busy` kèm `Retry-After` tới hết phút, không tính là lần sai. Chỉ người biết mật khẩu mới làm đầy được phần của một tên, nên người ngoài không dùng nó để chặn admin.

Lối ra khi admin vô tình chung nguồn với người làm khoá (cùng NAT văn phòng, hay `tpan setup --private` vào qua `ssh -L` mà panel thấy cùng `127.0.0.1`): lệnh `tpan admin unlock [<khoá nguồn>]` (`cmd/tpan/admin.go`, `adminUnlockCmd`, cần root): xoá dòng `login_failures` của khoá đó, không đổi mật khẩu, không huỷ phiên, ghi nhật ký `admin.unlock`. Không đối số thì gỡ mọi khoá. Lời 423 (`authWriteLocked`) in sẵn khoá nguồn và lệnh này. Nhật ký `login.fail` ghi khoá nguồn vào cột `target` (D12 của soát cuối), nên với kết nối từ chính máy, admin đọc nhật ký thấy `uid:<n>` và tra ra site nào đang dò mật khẩu.

### 3.14 P2: semaphore argon2id

`internal/store/auth.go` từ dòng 44 (trích rút gọn):

```go
const argonSlots = 2

var (
	argonSem = make(chan struct{}, argonSlots)
	argonWait = 3 * time.Second
)

func argonRun(wait time.Duration, pw, salt []byte, ...) ([]byte, error) {
	if wait < 0 {
		argonSem <- struct{}{}
	} else {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case argonSem <- struct{}{}:
		case <-timer.C:
			return nil, ErrPasswordBusy
		}
	}
	defer func() { <-argonSem }()
	return argonKey(pw, salt, ...), nil
}
```

Mọi chỗ gọi argon2id đi qua `argonRun`; đây là gói duy nhất import `x/crypto/argon2`. Đọc theo thứ tự:

- `argonSem` là một kênh (channel) có 2 chỗ. Gửi một giá trị vào là "lấy chỗ", nhận ra là "trả chỗ". Kênh đầy thì gửi phải chờ: đó là semaphore.
- `select` chờ hai thứ, ai tới trước thì thắng: có chỗ, hoặc đồng hồ 3 giây kêu. Hết 3 giây thì trả `ErrPasswordBusy`.
- `wait < 0` là chờ không hạn: `HashPasswordWait`, dùng cho lệnh CLI (tiến trình riêng, không tranh chỗ với server) và hash giả lúc server khởi động.
- `defer func() { <-argonSem }()` trả chỗ khi hàm kết thúc, dù thành công hay không.

Mọi route gọi argon2 (đăng nhập, đổi mật khẩu, admin tạo user, đặt lại mật khẩu) đổi `ErrPasswordBusy` thành HTTP 429 `auth_busy` với `Retry-After: 3`. Lần bận không tính là lần sai: chưa so thì chưa lộ gì. Bộ nhớ sống của argon2 trong server không quá 2 × 64 MiB (tham số `m=65536` KiB, `t=3`, `p=4` ở `argonParams`).

## 4. Kiến thức nền

Mỗi khái niệm: ý chính, một chỗ trong mã, một lệnh để tự nhìn trên máy ảo. Các lệnh dưới đây chưa chạy trong phiên viết, trừ chỗ ghi khác; phần "sẽ thấy" dựa trên man page và trên log ở mục 2.

### 4.1 `disable_symlinks` của nginx và `SymLinksIfOwnerMatch` của Apache

Cả hai trả lời cùng một câu hỏi: web server có được đi theo symlink này không. Quy tắc "if owner": được, nếu chủ của symlink cũng là chủ của thứ nó trỏ tới. Site tự đặt symlink trỏ vào file của chính nó thì chủ giống nhau, chạy bình thường. Site `e` trỏ sang file của `b` thì chủ khác nhau, bị từ chối. Điểm cần nhớ là "xét những thành phần nào của đường dẫn": nginx có `from=` thì bỏ qua các thành phần từ `root` trở lên, Apache thì phụ thuộc `Options` của thư mục chứa thành phần đó.

Chỗ trong mã: mục 3.1 và 3.2.

Tự nhìn (máy ảo, nginx):

```bash
grep -rn disable_symlinks /etc/nginx/
```

Sẽ thấy một dòng `disable_symlinks if_not_owner;` cho mỗi vhost site (không có `from=` trên máy đã vá hay cài mới; máy nâng chưa harden vẫn còn dòng có `from=`). Trên Apache: `grep -n -B1 -A3 'SymLinksIfOwnerMatch' /etc/apache2/sites-available/<site>.conf`.

### 4.2 `mod_status` và `/server-status`

Module `status` của Apache có sẵn một trang `/server-status` liệt kê các request đang xử lý: máy nào gọi, URL đầy đủ kèm chuỗi truy vấn. Cấu hình mặc định của Ubuntu chỉ cho `Require local` (chỉ từ chính máy). Nghe an toàn, nhưng PHP của mọi site chạy trên chính máy này, nên "local" gồm cả K1. Mà URL hay chứa token đặt lại mật khẩu hay khoá API của khách.

Chỗ trong mã: `ApacheModulesOff`, `ApacheModOff` ở mục 3.4 và 3.5.

Tự nhìn (máy ảo, Apache): `apachectl -M | grep status` (đã chạy trong e2e theo `a2query -m status`, kết quả ở mục 2.2). Máy vá không in `status_module`; `proxy_status_module` có thể vẫn hiện, nó là module khác.

### 4.3 `max_connections` và `MAX_USER_CONNECTIONS` của MariaDB

MariaDB cho một số kết nối đồng thời tối đa cho cả máy: `max_connections`, mặc định 151, ở log là `Threads_connected 152` khi `e` đang giữ 151 kết nối (con số hơn một đơn vị so với 151, mình hiểu là kết nối của chính lệnh hỏi; log `Threads_connected 21` khi `e` giữ 20 cũng hơn một). Hết 151 thì mọi người (mọi site) bị lỗi 1040. `MAX_USER_CONNECTIONS` đặt trần riêng cho một tài khoản: vượt thì lỗi 1226, chỉ tài khoản đó bị từ chối. Trần chỉ có tác dụng với kết nối mở sau khi tài khoản có trần.

Chỗ trong mã: `dbCreateUserSQL` và `DBUserLimit.Apply` ở mục 3.7 và 3.8.

Tự nhìn (máy ảo, root): `mariadb -e "SELECT @@GLOBAL.max_connections"` (151 trên máy log), và câu `SELECT max_user_connections ...` ở mục 2.3.

### 4.4 argon2id và vì sao kiểm mật khẩu tốn RAM

argon2id là hàm băm mật khẩu cố ý tốn bộ nhớ và thời gian để kẻ đoán mật khẩu không thử nhanh được. Tham số của panel là `m=65536` KiB, nghĩa là mỗi lần băm hay kiểm xin 64 MiB, mất khoảng 0,11 đến 0,15 giây trên máy ảo (log: `một lần đăng nhập sai mất 0.113833s` và `0.150104s`). Điều đó tốt cho an toàn của mật khẩu và xấu khi có người gửi nhiều request đăng nhập một lúc: chưa cần đăng nhập đúng, mỗi request đã xin 64 MiB.

Chỗ trong mã: `argonRun` ở mục 3.14.

Tự nhìn: log ở mục 2.5; hoặc trong repo `go test -count=1 ./internal/store -run 'TestArgon' -v` (có `TestArgonKhongQuaHaiCho` kiểm không bao giờ có quá 2 lần chạy cùng lúc; chưa chạy trong phiên viết).

### 4.5 Semaphore bằng channel của Go

Trong Go, `make(chan struct{}, 2)` là một hàng đợi có sức chứa 2 mà phần tử không mang dữ liệu. Gửi `ch <- struct{}{}` khi đầy thì phải chờ; nhận `<-ch` thì giải phóng một chỗ. Dùng nó làm "2 chỗ ngồi": ai ngồi được thì chạy, hết chỗ thì chờ. `select` cho phép chờ "có chỗ hoặc hết giờ" trong một câu. `argonSem` (mục 3.14) và `procBufs` (mục 3.10) đều là cách dùng này.

Tự nhìn: `grep -n "argonSem\|procBufs" internal/store/auth.go internal/server/auth_m28.go` trong repo.

### 4.6 `/proc/net/tcp` và UID của đầu bên kia

Kernel Linux liệt kê mọi socket TCP đang có trong file `/proc/net/tcp` (IPv4) và `/proc/net/tcp6` (IPv6): mỗi dòng gồm địa chỉ:cổng bên mình, địa chỉ:cổng bên kia (viết hex), trạng thái, và cột `uid` là chủ tiến trình giữ socket. Khi một tiến trình trên máy nối tới panel, panel nhìn địa chỉ nguồn và cổng của kết nối rồi tìm dòng khớp: cột `uid` cho biết user Linux nào đang gọi. Kernel không cho hai socket có cùng bộ bốn (địa chỉ và cổng hai đầu), và UID không đổi trong đời socket, nên cách này không bị giả mạo bởi user thường.

Chỗ trong mã: `lookupLoopback`, `procUID` ở `internal/server/auth_m28.go`.

Tự nhìn (máy ảo): `head -3 /proc/net/tcp`. Sẽ thấy dòng tiêu đề (có cột `uid`) và các dòng socket, mỗi dòng một số UID ở cột `uid`. Địa chỉ viết hex nên khó đọc bằng mắt; đó là lý do mã dựng sẵn chuỗi hex của 4-tuple để so.

### 4.7 HTTP 423, 429 và `Retry-After`

423 Locked: "tài nguyên đang bị khoá". Panel dùng cho "khoá nguồn này đang bị khoá vì sai mật khẩu". 429 Too Many Requests: "bạn gửi quá nhiều, thử lại sau". Panel dùng mã `auth_busy` cho: hàng chờ của khoá đầy, hết chỗ tra UID, hết chỗ argon2, vượt giới hạn tốc độ. Khác biệt cần nhớ: 423 là hậu quả của lần sai thật; 429 là "chưa kiểm", không tính là lần sai và không dẫn tới khoá. Header `Retry-After` nói chờ bao nhiêu giây.

Chỗ trong mã: `authWriteLocked`, `authWriteBusy`, `authWriteRate` ở `internal/server`.

Tự nhìn: lệnh `curl` ở mục 2.4 với `-w "%{http_code}"`; thêm `-i` để thấy header `Retry-After`.

## 5. Cách khác đã cân nhắc, và chỗ đã làm sai

### 5.1 Cách khác

F4 nginx: dùng `from=<neo>` thay vì bỏ `from=`. Không chọn vì dòng sẽ phải dựng riêng cho từng site (thêm một giá trị vào template), còn bỏ hẳn `from=` cho một hằng chung. Cái giá: nginx xét thêm `/`, `/var`, `/var/www`, neo, nhưng chúng đều thuộc root nên không đổi gì với site hợp lệ (nghiên cứu N1d: đường ACME và các đường root đặt vẫn chạy).

F4, vá lúc nâng cấp: tự đổi vhost ngay khi `tpan self-update`. Không chọn vì đổi vhost thì phải reload web server, đổi user DB là đổi DB đang chạy và cắt request đang chạy; admin phải chọn lúc làm và xem trước được, như cách M14 đã làm. Cái giá: có một khoảng giữa lúc nâng và lúc `security harden` mà máy còn hở F4, H5, DBCONN; `tpan health` báo rõ trong lúc đó.

H5: giữ module và chặn bằng cấu hình. Không chọn vì tpan không dùng `/server-status` cho việc gì: không có lý do giữ. Ubuntu cũng không bật `mod_info`; tpan không đặt `stub_status` cho nginx hay `pm.status_path` cho pool nào.

DBCONN: đặt `max_user_connections` toàn máy trong `99-tpan.cnf`. Không chọn vì trần toàn máy áp cho cả root của MariaDB, mà mọi lệnh của tpan chạy bằng root: tpan sẽ tự bị trần. Một cách khác đã bỏ: đọc trần bằng `SHOW CREATE USER`, vì câu đó in hash mật khẩu mà engine ghi mọi dòng output vào log của job; nên đọc từ cột `max_user_connections` của `mysql.user`.

P1: khoá theo tên đích thay vì theo nguồn (sai 5 lần với tên `admin` thì khoá tên `admin`). Không chọn vì như vậy ai cũng khoá được admin: chỉ cần gửi 5 lần sai. Phần "chậm dần theo tên" chưa làm.

P1, tra UID: dùng netlink `sock_diag` (hỏi đúng một socket thay vì đọc cả bảng) thay vì đọc `/proc`. Soát cuối đề xuất. Chọn giữ `/proc` để test được qua `system.System` (và `FakeSystem` trên Mac), đổi lại phải làm đủ các trần của mục 3.10.

P2: nâng `argonSlots` hay giảm tham số argon2. Không chọn: giảm bộ nhớ làm yếu độ bền của hash mật khẩu cho mọi người dùng. Chọn trần 2 chỗ: một lần argon2id mất cỡ 0,1 giây nên panel vẫn nhận vài lần đăng nhập mỗi giây, đủ cho một panel quản trị.

### 5.2 Điều đã biết là còn thiếu

Site có `public` là symlink cùng chủ (kiểu thư mục `current` của công cụ triển khai) vẫn được web server phục vụ (200), nhưng `site.docroot` báo lỗi và nằm trong kiểm chứng của mọi Plan chạm site đó, nên các Plan đó bị hoàn tác (D13 của soát cuối). Chưa xử lý bằng mã, ghi là hành vi đã biết: đổi `public` lại thành thư mục thật.

### 5.3 Chỗ đã làm sai trong đợt này

Người review hay hỏi đúng những chỗ này. Mỗi cái: sai gì, phát hiện bằng cách nào.

1. **Đếm theo địa chỉ IP (S1, nặng, soát thiết kế).** Bản nháp đếm lần sai theo IP, kể cả với kết nối trong máy. Thợ soát chỉ ra: PHP của một site chọn được địa chỉ nguồn trong cả dải `127.0.0.0/8`, nên mỗi địa chỉ là một dòng đếm riêng, K1 dò mật khẩu admin gần như không giới hạn. Sửa: khoá là UID của tiến trình đầu bên kia. Bộ e2e P1a bắt điều này: mỗi lần một nguồn `127.0.0.x`, và bản chưa vá cho 20 lần 401, 0 lần 423 (mục 2.4).
2. **Harden "tất cả hoặc không" (S2 rồi D2 của soát cuối).** Lần trả lời soát thiết kế chỉ đo phép HTTP của từng site TRƯỚC Plan. Soát cuối (D2) chỉ ra là chưa đủ: K1 chạy `chmod o+x` trên neo của chính nó trong lúc site vẫn trả 200, phép cô lập của site đó Fail lúc kiểm chứng, cả bản vá của máy bị hoàn tác, và H5, DBCONN không có đường sửa tự động nào khác. Sửa bằng `checks.Soft` (mục 3.6) và cho `tpan health --fix` sửa `web.mod_status`, `db.user_limit`. e2e nâng cấp có ca N6b làm đúng việc này (`chmod o+x`, rồi root trả lại `2750`), và harden phải thoát 0 (log bản vá dòng 734 tới 753).
3. **Tra UID đọc trọn bảng socket (D1, nặng, soát cuối).** Đường mà chính bản vá P1 thêm vào mở lại hậu quả của P2: mỗi request đọc và tách cả `/proc/net/tcp`, trước mọi trần. Thợ soát đo trên Mac: bảng 10 002 dòng, 1000 request song song thì heap đỉnh 2 500 MiB. Sửa ở mục 3.10. Khuyết điểm còn lại của bộ e2e: P2 chỉ đi nguồn không phải loopback nên chưa có phép e2e nào cho đường này; chỉ có unit test `TestD1BangLonCapPhatNho`, `TestD1TranSoByte`, `TestD1HetChoKhongNho`, `TestD1TraMotLanMoiKetNoi` (mục 7 của bản thiết kế ghi: "tra UID trên bảng socket lớn (chỉ `TestD1*`)").
4. **Thiếu `KILL USER` (D3, soát cuối).** Bản đầu chỉ chạy `ALTER USER`. MariaDB chỉ đếm kết nối mở sau khi tài khoản có trần, nên K1 mở sẵn 140 kết nối trước harden vẫn giữ chúng, mở thêm cỡ 12 là cạn `max_connections`, trong lúc `db.user_limit` báo đạt. Sửa: `KILL USER` ngay sau `ALTER USER`, bước thành `Danger`. Ca "kết nối mở trước harden" chưa có trong e2e, chỉ có `TestDBUserLimitKillKetNoiCu` (hiểu biết về cách MariaDB đếm ở đây là của thợ soát, đọc từ mã nguồn MariaDB, chưa được tra lại trong phiên của soát cuối).
5. **`ApacheModOff` không hỏi lại sau `a2dismod` (D11).** Module nạp bằng dòng `LoadModule` ngoài `mods-enabled` thì `a2dismod` không làm gì mà vẫn thoát 0, bước báo xong, rồi `web.mod_status` lỗi và harden hoàn tác cả vhost lẫn trần DB. Sửa: hỏi lại `apachectl -M`, và `hardenModOff` hạ lỗi này xuống cảnh báo.
6. **Lần trả lời soát thiết kế nói "lùi bản không vỡ" (S7, rồi D5 của soát cuối).** Việc nhận cả dòng `disable_symlinks` cũ lẫn mới nằm ở bản MỚI; bản cũ (v0.2.1, v0.2.2) vẫn đòi đúng dòng `from=$document_root`. Hậu quả ghi ở mục 7. Bản đã phát hành không sửa được, nên xử lý bằng câu chữ.
7. **e2e xanh mà không chứng minh điều nó nói (D6, soát cuối).** Hai ví dụ. P1b bản đầu đếm "không quá 5 câu 401" và xanh ngay cả khi lần so mật khẩu thừa trả 423 qua nhánh "khoá còn hạn" của `RecordLoginFailure`; sửa bằng cách đếm dòng `login.fail` trong `audit` (log bản vá dòng 184 tới 185: 5) và dùng ĐÚNG mật khẩu ở lần thứ 6. Ví dụ hai là mục 2.5: khẳng định RSS của P2 xanh ở bản chưa vá vì tiến trình đã phình trước đó. Bài học: khẳng định phải đi cùng một phép chứng minh rằng bước cần thử đã chạy.
8. **Hai khẳng định đỏ ở lần chạy đầu trên bản vá, do kỳ vọng sai của script** (`docs/thay-doi/M28.md` mục 35). Lần đầu 172 đạt, 2 không đạt, cả hai ở P1b: hàng chờ của khoá nguồn trả 429 cho 16 trên 20 request song song nên chỉ 4 lần được so mật khẩu, chưa đủ 5 để khoá, và lần đăng nhập đúng sau đó vào được là đúng luật. Script giờ gửi tiếp tuần tự tới khi gặp 423 rồi mới thử mật khẩu đúng. Lần chạy đó giữ làm bằng chứng ở `docs/bang-chung/M28-dot2-lan1.log`.
9. **N6b mất bit setgid của neo.** Bản đầu của ca D2 chạy `chmod 2751` rồi `chmod 2750` dưới user của site; kernel bỏ bit setgid của neo vì user đó không thuộc nhóm `www-data`. Sửa: `chmod o+x` dưới user của site, và root trả lại `2750`.
10. **Hai chỗ bị bắt khi gộp `dev` có M17 (lồng theo site)** (`docs/thay-doi/M28.md` mục 34). Các test bất biến của M17 báo hai chỗ của đợt 2 chưa theo luật lồng: mod_status và trần kết nối phải đặt TRƯỚC bước khởi động lại lồng (bước này phải ở cuối harden); và sửa tự động `php.pool` ghi pool chung, nay từ chối site trong lồng.

### 5.4 Vì sao `web.docroot_guard` và `php.pool` tách khỏi `VhostHardened`

Phép `VhostHardened` (M14) nằm trong kiểm chứng của mọi Plan chạm site. Nếu nó đòi ngay dòng mới thì trên máy vừa nâng mà chưa harden, mọi Plan không tự ghi lại vhost (ví dụ khôi phục backup) sẽ bị hoàn tác (S5). Nên `VhostHardened` nhận cả hai dòng, và "có dấu của bản vá F4" thành phép riêng `web.docroot_guard` chỉ ở health và kiểm chứng của harden. Cùng lý do cho `php.pool`: gắn nó vào `checks.Isolated` (phép theo cặp site) thì một pool sửa tay chặn mọi thao tác trên mọi site khác, mà harden không sửa pool, và phép chạy theo bình phương số site (S8). Nên `php.pool` chỉ ở health, sửa bằng `tpan health --fix`.

## 6. Tự kiểm

Đáp án ở cuối file. Câu 3, 5 và 10 cần chạy lệnh trên máy ảo mới trả lời chắc được.

1. Vì sao bỏ `from=$document_root` khỏi dòng `disable_symlinks` thì chặn được trường hợp `public` là symlink? Dòng cũ sai ở chỗ nào?
2. Trên Apache, xoá khối `<Directory "{{.Dir}}">` khỏi vhost của một site thì chuyện gì xảy ra với F4 và với `tpan health`?
3. (Máy ảo) Trên máy của bạn, đổi `public` của một site thử thành symlink sang thư mục của site khác. Web server trả mã gì, và `tpan health` có dòng `[lỗi]` nào về chuyện đó?
4. Vì sao `ApacheModOff` hỏi `apachectl -M` chứ không dùng `a2query -m`, và vì sao `reload` chứ không `restart`?
5. (Máy ảo) Trần `max_user_connections` của user DB của một site tạo mới là bao nhiêu? Và của site tạo bằng bản cũ trước khi chạy `tpan security harden`?
6. Vì sao trần là 20 mà không phải 151, 5 hay 100? Vì sao không đặt trong `99-tpan.cnf`?
7. Admin gõ sai mật khẩu 4 lần, rồi `khach1` (cùng nguồn) đăng nhập đúng, rồi admin gõ sai thêm 1 lần. Kết quả? Bản cũ khác ở đâu?
8. Vì sao khoá nguồn của kết nối loopback là UID chứ không phải địa chỉ `127.x.y.z`?
9. Vì sao `argonSlots` là 2 và chờ tối đa 3 giây? Vì sao lệnh CLI dùng `HashPasswordWait` chứ không `HashPassword`? Vì sao 429 không được đếm là một lần sai?
10. (Máy ảo) Một K1 làm khoá nguồn `uid:<n>` của site mình, kéo theo khoá luôn người dùng chung nguồn. Admin gỡ khoá bằng lệnh nào mà không phải đổi mật khẩu? Chạy thử một vòng 6 lần sai rồi gỡ để thấy.

## 7. Nâng cấp từ bản cũ

Máy đã cài v0.2.0 hay v0.2.1, v0.2.2 rồi `tpan self-update` lên bản này.

**Vì sao không vá lúc nâng.** `self-update` không đổi cấu hình site nào (luật của M14 và M15). Vá F4 là ghi lại vhost và reload web server; vá H5 là tắt module Apache và reload; vá DBCONN là đổi user DB đang chạy và cắt kết nối đang mở (bước `Danger`). Đó là những việc admin phải chọn lúc làm và xem trước. Chỉ P1 và P2 có hiệu lực ngay, vì chúng là mã của panel, không đổi gì trên đĩa.

**`tpan health` báo gì sau khi nâng** (log bản vá dòng 719 tới 732, ca Apache): `[lỗi] Vhost không đi theo public khi nó là symlink sang chủ khác` kèm lý do `Vhost chưa có bản vá F4`; `[lỗi] mod_status của Apache đang tắt: mod_status của Apache đang bật`; `[cảnh báo] User DB có trần số kết nối cùng lúc: ... không có trần số kết nối`. Ba site nên mỗi loại vhost và DB có ba dòng. Cùng lúc, mọi Plan khác trên site cũ vẫn chạy được, vì `VhostHardened` nhận cả vhost cũ (mục 5.4). Nếu site nào đang có `public` là symlink hay pool PHP lệch thì còn thêm `site.docroot` hoặc `php.pool`.

**`tpan security harden`.** Có bản xem trước như mọi Plan, chạy `tpan security harden --yes` để bỏ qua hỏi. Một lệnh vá cả ba thứ cho cả máy: ghi lại vhost của mọi site theo template mới, tắt `status` (Apache), đặt trần 20 cho user DB của mọi site và DB rời đang chạy rồi cắt kết nối cũ. Không còn "tất cả hoặc không": site đang trả lời sai trước Plan vẫn được vá và được nêu tên trong bản xem trước. `tpan health --fix` sửa từng thứ riêng (`web.docroot_guard`, `web.mod_status`, `db.user_limit`, `php.pool`). Kết quả đo trên máy nâng từ v0.2.0 ở mục 2.6: harden thoát 0, hai site WordPress 200 và đăng nhập được.

**Cảnh báo lùi bản trên nginx.** Bản cũ (v0.2.2 trở về trước) chỉ nhận dòng `disable_symlinks if_not_owner from=$document_root;`. Nên nếu bạn đã chạy harden trên máy nginx rồi lùi về bản cũ: `VhostHardened` của bản cũ báo lỗi cho mọi site, mọi Plan trên site không tự ghi lại vhost (ví dụ khôi phục backup) bị hoàn tác; và `tpan health --fix` hay `tpan security harden` của bản cũ ghi lại vhost theo template cũ, tức là mở lại F4 cho mọi site. Đã lùi trên nginx thì đừng chạy harden hay `health --fix` của bản cũ. Apache không bị chuyện này (khối neo mới không làm phép kiểm cũ lỗi). Không có migration nên bản cũ vẫn mở được DB, và trần kết nối DB giữ nguyên khi lùi.

**Không có trong e2e.** Nâng cấp trên nginx, và F4 qua cổng 443, chỉ có golden test của template (`TestNginxSymlinkGuard`); mục 7 của bản thiết kế liệt kê đầy đủ phần chưa làm.

## 8. Còn hở

Chép nguyên từ mục 6.27 của `docs/KE-HOACH.md` (cách gây ra giữ ngoài repo tới khi vá, repo này công khai):

- Apache: một dạng đua với symlink ở `public` (nginx không bị).
- Kết nối MariaDB chưa đăng nhập không bị trần theo user tính tới.
- Một khách có nhiều DB rời cộng các trần lại vẫn vượt được `max_connections`.
- Kẻ chiếm site còn vài khoá nguồn phụ ngoài `uid:<n>`, nên được vài lần đoán hơn 5 mỗi 15 phút.
- Phần còn lại của D2: ngoài harden, một site bị chiếm vẫn làm được các Plan khác chạm tới nó bị hoàn tác (có từ M14; không mở lỗ đọc file, chỉ chặn thao tác của admin tới khi sửa site đó).
- Hồi quy cho site hợp lệ (D13 của soát cuối): site có `public` là symlink cùng chủ (kiểu thư mục `current` của công cụ triển khai) vẫn chạy nhưng mọi Plan trên site đó bị hoàn tác vì `site.docroot`; đổi `public` thành thư mục thật.
- Nhóm B của đợt 2: P4, P7 đã vá (xem `28c-m28-dot-2-nhom-b.md`); F1, P06, T3 và các phát hiện vừa còn lại chưa làm.

Nếu bị hỏi "bản này đã an toàn hoàn toàn chưa": chưa. Bốn gạch đầu là lỗ chưa vá. Gạch thứ nhất là lý do để khuyên nginx cho máy nhiều khách.

## Đáp án

1. `from=$document_root` bảo nginx bỏ qua mọi thành phần của đường dẫn từ thư mục `root` trở lên khi xét symlink. `public` chính là `root`, nên khi `public` là symlink sang thư mục của site khác, nginx không xét nó và đi theo. Bỏ `from=` thì nginx xét mọi thành phần, trong đó có `public`: chủ của link (user `e`) khác chủ của đích (user `b`) nên từ chối. Các thành phần `/`, `/var`, `/var/www` và neo thuộc root, không phải symlink, nên không bị ảnh hưởng. Bằng chứng: log bản vá dòng 100 tới 104 (nginx 404 thay cho 200 ở log chưa vá).

2. Ubuntu đặt `Options Indexes FollowSymLinks` cho `/var/www/` trong `apache2.conf`. Không có khối neo, thành phần `public` được xét theo `FollowSymLinks`, tức Apache đi theo link bất kể chủ: ca F4 mở lại (log chưa vá dòng 320 tới 324: HTTP 200). Về `tpan health`: phép `web.docroot_guard` đòi MỌI `<VirtualHost>` của file có khối `<Directory "<neo>">` mà dòng `Options` cuối cùng đúng là `Options SymLinksIfOwnerMatch`, nên xoá khối thì health báo `[lỗi] Vhost không đi theo public khi nó là symlink sang chủ khác` kèm câu `thiếu khối <Directory "/var/www/<site>"> với Options SymLinksIfOwnerMatch` (cùng câu như ở log bản vá dòng 725 cho vhost cũ). Cách sửa: `tpan health --fix` hay `tpan security harden`. Chưa chạy trong phiên viết: lấy từ mã `DocrootGuard` và log.

3. Chạy các lệnh ở mục 2.1. Sẽ thấy nginx trả 404 (log bản vá dòng 102), Apache trả 403 (dòng 325), thân không có nội dung file của site kia. `tpan health <domain>` có đúng một dòng `[lỗi] Thư mục public của site là thư mục thật của chính site: /var/www/<domain>/public là symlink, không phải thư mục thật (đích ở bằng chứng)` (dòng 108, và `số dòng [lỗi]` là 2 chứ không phải 1 như ở log chưa vá dòng 107). Trên máy chưa vá hay chưa harden: nginx thấy 200 và file của site kia.

4. `a2query -m` chỉ thấy module bật qua link trong `mods-enabled`. Module nạp bằng một dòng `LoadModule` đặt ở chỗ khác (cấu hình admin tự thêm) thì `a2query` không thấy, còn `apachectl -M` liệt kê module Apache thật sự nạp theo cấu hình trên đĩa (D11 của soát cuối). `reload` thay vì `restart` để không cắt kết nối và lần tải lên đang dở của site nào; vì `status` không đổi MPM nên reload là đủ. Hai Plan cài stack và chuyển sang Apache vốn khởi động lại Apache vì đổi MPM nên tắt thêm `status` ở đó không tốn gì.

5. Chạy lệnh ở mục 2.3. Site tạo bằng bản vá: `20` (log bản vá dòng 133, 372, 823). Site tạo bằng bản cũ chưa harden: `0` (dòng 671 của log bản vá, ca nâng từ v0.2.0), và `tpan health` báo `[cảnh báo] User DB có trần số kết nối cùng lúc: ... không có trần số kết nối`. Sau `tpan security harden` thì `20`.

6. 20 tính từ nhu cầu thật: pool của site có `pm.max_children = 5`, WordPress giữ tối đa một kết nối mỗi request PHP, cộng các lệnh `wp` của tpan (health, backup, cron) chạy cùng lúc không quá 4: khoảng 9 kết nối hợp lệ, 20 là gấp đôi, đủ cả lúc đổi bản PHP (hai pool chạy cùng lúc). 151 là chính `max_connections`, nên một trần 151 không chặn được gì. Trần nhỏ hơn 9 thì có nguy cơ làm lỗi chính site hợp lệ. Không đặt trong `99-tpan.cnf` vì trần toàn máy áp cả cho root của MariaDB, mà mọi lệnh của tpan chạy bằng root.

7. Lần sai thứ 5 của admin bị khoá (423): lần sai 4 của admin là 4 trong tổng, lần đúng của `khach1` chỉ trừ phần lỗi của chính `khach1` (là 0), nên tổng vẫn 4; lần sai tiếp theo là lần thứ 5 và khoá (ca E7 trong `TestE7XenDangNhapDungCuaTenKhac`; log bản vá dòng 168: `4:401 (khach1 đúng:204) 5:423`). Bản cũ gọi `ClearLoginFailures(ip)` ở lần đăng nhập đúng của bất kỳ ai, xoá bộ đếm của cả nguồn: sau mỗi 4 lần đoán admin xen một lần `khach1` đúng là không bao giờ bị khoá (log chưa vá dòng 168: 20 lần 401).

8. Trên Linux mọi địa chỉ `127.0.0.0/8` đều là của máy, và PHP của một site chọn được địa chỉ nguồn tuỳ ý trong cả dải. Đếm theo địa chỉ thì mỗi địa chỉ là một khoá riêng với 5 lần đoán riêng. Kernel ghi UID của chủ mỗi socket trong `/proc/net/tcp`, user thường không giả được, nên đếm theo UID thì mọi lần đoán từ PHP của một site chung một khoá (log bản vá dòng 182: khoá `uid:994`, một dòng). Admin vào qua `ssh -L` mang UID của phiên SSH, khác UID của site nên không chung khoá.

9. Một lần argon2id xin 64 MiB và mất cỡ 0,1 giây; 2 chỗ nghĩa là bộ nhớ sống cho argon2 không quá 128 MiB, vẫn nhận vài lần đăng nhập mỗi giây, đủ cho một panel quản trị. Chờ tối đa 3 giây, quá thì 429 kèm `Retry-After: 3`: không xếp hàng vô hạn. CLI dùng `HashPasswordWait` (chờ không hạn) vì nó là tiến trình riêng, không tranh chỗ với server. Hash giả cho tên lạ cũng dùng `HashPasswordWait` lúc khởi động (S11): nếu nó gặp `ErrPasswordBusy` và lỗi bị nhớ thì mọi tên lạ trả 500 ngay còn tên thật trả 401 sau argon2, tức lộ tên user nào có thật. 429 không tính là lần sai vì chưa so mật khẩu thì chưa có gì sai để đếm; đếm sẽ cho kẻ làm đầy chỗ tra khoá được người khác.

10. `sudo tpan admin unlock <khoá nguồn>` (khoá in trong lời 423, ví dụ `uid:993`); không đối số thì gỡ mọi khoá. Nó không đổi mật khẩu, không huỷ phiên, và ghi nhật ký `admin.unlock`. Khác `tpan admin passwd`, lệnh này đổi mật khẩu và huỷ mọi phiên. Chạy thử: vòng 6 lần `curl` ở mục 2.4 từ shell root (khoá `uid:0`), rồi `tpan admin unlock uid:0`; sẽ thấy `401` bốn lần, `423` từ lần 5, và một dòng báo đã gỡ khoá (dòng 195 của log bản vá là `Đã gỡ khoá đăng nhập của uid:993.`). Chưa chạy trong phiên viết với `uid:0`.

Nhóm B của đợt 2 (P4, P7, và sửa phép thử P2): xem `28c-m28-dot-2-nhom-b.md`.
