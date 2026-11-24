# M20: đăng nhập hai bước, token API có phạm vi, chứng chỉ panel được tin

Đọc kèm: `13-m13-nhieu-nguoi-dung.md` (user, vai `admin` và `user`, khoá "sai 5 lần"), `28-m28-va-theo-ra-soat.md` (M28 sửa `authFailed` mà M20 gọi vào), `docs/thay-doi/M20-thiet-ke.md` (bản thiết kế đầy đủ; mục 11 đè các mục trước nó), `docs/thay-doi/M20.md` (chỗ thực tế khác thiết kế).

Cách đọc file này: "Nói trong một phút" là thứ bạn nói được khi bị hỏi đột xuất. Muốn trả lời sâu thì đọc mục 3. Muốn tự chạy lại thì đọc mục 7. Các câu người review hay hỏi nằm ở mục 8, đáp án ở cuối file.

Quy ước: **phiếu** là "phiếu chờ bước hai", thứ panel đưa cho người vừa gõ đúng mật khẩu mà chưa nhập mã. **App xác thực** là ứng dụng trên điện thoại hiện mã 6 số đổi mỗi 30 giây (Google Authenticator, Aegis, 1Password...). Khi ví dụ cần tên: `khach_a` là user demo đã bật hai bước, `admin` là quản trị viên.

**Trạng thái trung thực của mốc này (cập nhật 25/11/2026).** Mã và test đơn vị đã xong và xanh trên Mac. Bộ ảnh giao diện đã chụp (39 ảnh, 0 không đạt). **E2E bước 23 đã chạy trên máy ảo `tpan-m20`: 222 khẳng định đạt, 0 không đạt** (`docs/bang-chung/M20.log`, bản `v0.2.2-109-g82eeb72`, cây sạch, mỗi lần từ máy sạch). Hồi quy năm kịch bản cũng xanh (mục 2.6). Còn thiếu: phần "thử phá trên máy ảo" của soát cuối, đang chạy lúc viết, xem `docs/thay-doi/M20.md`; và vài chỗ nhỏ mà log không có (mục 6). Chỗ nào log không có thì file này vẫn ghi "chưa chạy" kèm lệnh.

## Nói trong một phút

Trước M20, panel TPAN chạy bằng root và chỉ cần một mật khẩu là có root cả máy. Mọi script, CI hay AI muốn gọi panel phải dùng chung mật khẩu của người. Chứng chỉ HTTPS của panel là tự ký, nên trình duyệt báo đỏ và người dùng quen tay bấm "vẫn tiếp tục".

M20 sửa ba việc.

Một, đăng nhập hai bước. Gõ đúng mật khẩu chỉ đổi lấy một phiếu sống 5 phút, gắn với IP và tên máy của bạn. Phiếu không phải phiên: không route nào nhận nó, trừ đúng hai route "nhập mã" và "đăng xuất". Phải nhập thêm mã 6 số từ app (hoặc một mã dự phòng dùng một lần) thì mới có phiên thật. Mã đã dùng không dùng lại được. Dò mã bị chặn bằng ba lớp: mỗi phiếu 5 lần, mỗi user 10 lần rồi khoá 15 phút (lần sau gấp đôi), mỗi IP theo luật cũ.

Hai, token API cho máy dùng. Token có tiền tố `tpan_`, có phạm vi theo nhóm (ví dụ `sites:read`), có hạn bắt buộc, thu hồi được. Máy chủ chỉ lưu bản băm sha256. Quyền thật của token là phạm vi giao với quyền hiện tại của chủ token, nên hạ vai hay khoá chủ là token mất quyền ngay. Những việc về mật khẩu, hai bước, quản lý user, duyệt Plan của AI thì token không bao giờ làm được: chỉ phiên đăng nhập.

Ba, panel có thể đặt một tên miền riêng, xin chứng chỉ Let's Encrypt thật bằng certbot, và chọn chứng chỉ theo tên bạn gõ (SNI). Vào bằng IP vẫn là chứng chỉ tự ký như cũ. Gia hạn xong panel tự nạp, không restart.

Điều mình muốn bạn nhớ: thiết kế đầu của mốc này có ba lỗ nặng, và một thợ soát chưa đọc mã đã tìm ra cả ba trước khi ai viết dòng mã nào. Một là đoán mã song song vượt mọi trần chống dò. Hai là site và panel cùng tên máy làm lộ cookie phiên admin cho chủ site. Ba là token "chỉ đọc" lấy được mật khẩu WordPress qua `GET /jobs/{id}`. Mục 5.4 kể từng cái.

## 1. Mốc này làm được gì

- Mỗi user bật được đăng nhập hai bước bằng TOTP (mã 6 số đổi mỗi 30 giây) ở trang Tài khoản: quét QR, nhập mã và mật khẩu, nhận 10 mã dự phòng một lần. Admin đặt được chính sách "bắt buộc hai bước" cho admin hoặc cho mọi người.
- Mất điện thoại thì dùng mã dự phòng. Hết mã thì người có root trên máy gõ `tpan user 2fa reset <tên>`. Không có đường gỡ hai bước qua web khi không có mã.
- Tạo token API ở trang Tài khoản hoặc bằng `tpan token create`, gọi bằng `Authorization: Bearer tpan_...`.
- Admin đặt tên miền cho panel (`tpan panel domain set <tên>` hoặc thẻ ở màn Cài đặt); panel có chứng chỉ tin được và tự gia hạn.
- Nâng từ v0.2.0: không ai bị đăng xuất, hai bước mặc định tắt, đăng nhập cũ vẫn trả 204. Chỉ thêm bảng mới và một cột có giá trị mặc định (mục 6 của thiết kế).

## 2. Chạy thử

Mọi output dưới đây là output thật. Nguồn:

- Lệnh mình vừa chạy trên Mac trong phiên viết (23/11/2026, nhánh `moc/M20`): mục 2.1, 2.2, 2.3.
- `docs/bang-chung/M20-nghien-cuu/thu-vm.log`: thử nghiệm đợt 0 trên máy ảo `tpan-m20` (Ubuntu 24.04), mục 2.4.
- `docs/bang-chung/M20-anh.log` và 39 ảnh trong `docs/bang-chung/M20/`: mục 2.5.
- `docs/bang-chung/M20.log`: e2e bước 23 trên máy ảo `tpan-m20` (25/11/2026), 222 đạt, 0 không đạt. Trích ở mục 2.6 và rải trong mục 3, 4, 7, 8 kèm số dòng hoặc tên phần. Log đã che mật khẩu, bí mật, token; phần cuối của log tự quét xem có lọt không.
- `docs/bang-chung/M20-hoi-quy-m6.log`, `-m13.log`, `-m13-nang-cap.log`, `-m15.log`, `-m16.log`: hồi quy, mục 2.6.
- **Chưa có**: `docs/bang-chung/M20-thu-pha.log` (thử phá trên máy ảo của soát cuối). File này chưa tồn tại lúc viết; đang chạy, xem `docs/thay-doi/M20.md`.

### 2.1 Bảng lệch đồng hồ (Mac)

```bash
go test ./internal/mfa -run TestTOTPLechDongHo -v
```

```
=== RUN   TestTOTPLechDongHo
    mfa_test.go:120: lệch (s)      nhận
    mfa_test.go:131:      -90      0.0%
    mfa_test.go:131:      -60      0.0%
    mfa_test.go:131:      -45     50.0%
    mfa_test.go:131:      -30    100.0%
    mfa_test.go:131:      -15    100.0%
    mfa_test.go:131:        0    100.0%
    mfa_test.go:131:       15    100.0%
    mfa_test.go:131:       30    100.0%
    mfa_test.go:131:       45     50.0%
    mfa_test.go:131:       60      0.0%
    mfa_test.go:131:       90      0.0%
--- PASS: TestTOTPLechDongHo (0.05s)
PASS
```

Đọc bảng này thế nào: test cho "app" sinh mã đúng giờ ở 3000 mốc liên tiếp, rồi hỏi máy chủ có đồng hồ lệch `d` giây có nhận không. Lệch tới 30 giây thì nhận cả 3000/3000. Lệch 45 giây thì nhận khoảng một nửa (còn tuỳ mốc nằm ở đâu trong bước 30 giây). **Lệch từ 60 giây trở lên thì 0%, luôn luôn.** Lý do ở mục 3.5.

### 2.2 Các bất biến của M20, chạy trên Mac

```bash
go test ./internal/server -run 'TestDangNhapHaiBuoc|TestPhieuChoKhongVaoRouteNao|TestMaTOTPKhongDungLai|TestBuocHaiSongSongKhongVuotTran|TestKhoaLeoThang|TestDoMaKhongResetBoDem|TestPhienLechHostBiXoa|TestHostLaSite421|TestChungChiTheoSNI|TestTokenTheoVaiHienTai|TestNhomTokenCuaRoute|TestTokenChiDocKhongLayMatKhauJob|TestDuaSetupVoiEnable|TestBatBuocChiVaoTrangBat' -v
```

Lọc các dòng kết quả:

```
--- PASS: TestDangNhapHaiBuoc (0.11s)
--- PASS: TestPhieuChoKhongVaoRouteNao (0.05s)
--- PASS: TestMaTOTPKhongDungLai (0.05s)
--- PASS: TestBuocHaiSongSongKhongVuotTran (0.18s)
--- PASS: TestKhoaLeoThang (0.06s)
--- PASS: TestDoMaKhongResetBoDem (0.11s)
--- PASS: TestPhienLechHostBiXoa (0.04s)
--- PASS: TestHostLaSite421 (0.04s)
--- PASS: TestChungChiTheoSNI (0.04s)
--- PASS: TestTokenTheoVaiHienTai (0.04s)
--- PASS: TestDuaSetupVoiEnable (0.11s)
--- PASS: TestBatBuocChiVaoTrangBat (0.05s)
--- PASS: TestNhomTokenCuaRoute (0.00s)
--- PASS: TestTokenChiDocKhongLayMatKhauJob (0.05s)
ok  	.../internal/server	1.427s
```

Ba gói `internal/mfa`, `internal/apitoken`, `internal/store` cũng `ok` ở lần chạy `-count=1` cùng phiên. Mỗi test ứng với một bất biến B1 đến B10 của thiết kế (bảng ở mục 4 của `M20-thiet-ke.md`). Ví dụ `TestPhieuChoKhongVaoRouteNao` là B1, `TestNhomTokenCuaRoute` là B10.

### 2.3 Chạy `tpan serve --demo` và đi hết luồng (Mac)

Chế độ demo dựng một panel giả trên Mac (dữ liệu giả, không đụng máy thật). Nó có sẵn `khach_a` đã bật hai bước, và in mật khẩu cùng bí mật hai bước của `khach_a` lúc khởi động (mình để ở biến `ADMIN`, `KP`, `SEC` bên dưới, không chép giá trị vào đây). Máy demo chạy HTTP không TLS nên tên cookie là `tpan` và `tpan-2fa`; trên máy thật có TLS thì là `__Host-tpan` và `__Host-tpan-2fa`.

Bước một, mật khẩu đúng của user có hai bước:

```bash
curl -s -i -c cj.txt -X POST $B/login -d "{\"name\":\"khach_a\",\"password\":\"$KP\"}"
```

```
HTTP/1.1 200 OK
Set-Cookie: tpan-2fa=<phiếu>; Path=/; Max-Age=300; HttpOnly; SameSite=Strict
{"mfa_required":true}
```

Có phiếu mà đi vào route khác thì không được, nhưng `GET /session` biết đang ở bước hai:

```
GET /sites   -> {"error":{"code":"unauthorized",...}} [401]
GET /session -> {"logged_in":false,"demo":true,"mfa_pending":true}
```

Mã sai rồi mã đúng:

```
POST /login/2fa {"code":"000000"} -> mfa_code_wrong [401]
POST /login/2fa {"code":"<mã app>"} -> HTTP/1.1 200 OK
Set-Cookie: tpan=<phiên>; Path=/; HttpOnly; SameSite=Strict
Set-Cookie: tpan-2fa=; Path=/; Max-Age=0; HttpOnly; SameSite=Strict
```

Mã đúng đổi phiếu lấy phiên mới (cookie `tpan`) và xoá cookie phiếu (`Max-Age=0`). Rồi đăng nhập bước một lại và nhập đúng mã vừa dùng:

```
mfa_code_used: "Mã này vừa được dùng rồi. Chờ app hiện mã mới (30 giây) rồi nhập lại." [401]
```

Token: admin (chưa bật hai bước, nên đăng nhập một chặng) tạo token chỉ đọc nhóm `sites`:

```
POST /account/tokens {"name":"thu","scopes":["sites:read"],"days":7,"password":"<mật khẩu>"}
{"token":"<tpan_...>","row":{"id":"0ef11efc6360b798","user":"admin","name":"thu","scopes":["sites:read"],"hint":"tpan_eelsjg…",...,"status":"active"}}
```

Dùng token (mình che giá trị `tpan_...`):

```
GET  /sites         + token                 -> 200
POST /sites         + token                 -> 403 token_scope       "Token thiếu phạm vi sites:write."
GET  /account/tokens + token                -> 403 token_forbidden   "Route này chỉ dùng được khi đăng nhập trên giao diện..."
GET  /machine       + token                 -> 403 token_scope       "Token thiếu phạm vi system:read."
GET  /sites         + token sai một ký tự   -> 401 token_malformed   "Token sai dạng (thiếu hay thừa ký tự khi chép?)."
```

### 2.4 Máy ảo, đợt thử nghiên cứu (thu-vm.log)

Đây là thử của đợt 0 để chọn thiết kế, không phải e2e của mốc. Máy `tpan-m20`, Ubuntu 24.04.

Đồng hồ (dòng 30 đến 40): `systemd-timesyncd` chạy, `chrony` không cài, và ngay sau khi về snapshot:

```
NTP=yes
NTPSynchronized=no
active
inactive
```

Nghĩa là máy ảo mới dựng có thể lệch giờ vài giây đầu. Đó là lý do có phép kiểm `tpan.clock` (mục 3.5).

HTTP-01 đi qua vhost mặc định (dòng 262 đến 290 và quanh dòng 270): tên panel `panel.m20.test` không thuộc site nào nên rơi vào vhost mặc định của tpan. File thử thách trả 200 đúng nội dung, còn trang chủ thì nginx đóng kết nối:

```
noi-dung-nginx-4705  <- HTTP 200
curl: (52) Empty reply from server
```

Gia hạn bằng `certbot renew --cert-name tpan-panel --force-renewal` đổi serial chứng chỉ (dòng 362 và 507) và hook deploy nhận đường dẫn lineage:

```
serial=60978CC6536D21C4
serial=25B3C03ACFF60665
hook thu: RENEWED_LINEAGE=/etc/letsencrypt/live/tpan-panel RENEWED_DOMAINS=panel.m20.test
```

CSP của panel (dòng 390) là `default-src 'self'; frame-ancestors 'none'`: ảnh `data:` bị chặn, nên QR được vẽ bằng phần tử SVG chứ không phải ảnh.

### 2.5 Ảnh giao diện (M20-anh.log)

Dòng cuối của `docs/bang-chung/M20-anh.log`: `Đã chụp 39 ảnh vào docs/bang-chung/M20. Không đạt: 0`. Các màn: đăng nhập chặng hai, mã sai, bật hai bước bắt buộc, Tài khoản (bật có QR, mã dự phòng, token, hộp tạo token), Cài đặt, Công cụ; sáng, tối, 390 px; kiểm bàn phím và tiếng Anh. Ví dụ ảnh để mở xem: `docs/bang-chung/M20/dang-nhap-buoc-hai-sang.png`, `tai-khoan-bat-sang.png`, `tai-khoan-token-moi-sang.png`.

### 2.6 E2E trên máy ảo và hồi quy (25/11/2026)

Cách chạy: `make e2e-m20` từ Mac, script `scripts/e2e-m20.sh`, máy ảo `tpan-m20` (Ubuntu 24.04, arm64) dựng lại từ máy sạch. Script cài v0.2.0, nâng lên bản mới bằng chính `tpan self-update` của v0.2.0, rồi chạy tiếp các phần. Dòng cuối của `M20.log`:

```
######## [2026-11-25 00:26:45] TỔNG KẾT: 222 khẳng định đạt, 0 khẳng định không đạt
```

Số đạt từng phần, lấy từ các dòng `==== Phần ...` của log:

| Phần | Nội dung (mã T trong thiết kế) | Đạt | Dòng log |
|---|---|---|---|
| 0 | đóng gói bản mới, build đồ nghề | 6 | 190 |
| U | nâng từ v0.2.0 (T80 đến T85) | 28 | 700 |
| A | đăng nhập khi chưa ai bật hai bước, site (T1, T2) | 21 | 1671 |
| B | chính sách khi chưa bật, QR, `oathtool`, lệch đồng hồ (T21, T4, T3) | 25 | 1816 |
| C | mã sai, mã cũ, mã đã dùng, mã dự phòng, cookie phiếu (T10 đến T15) | 18 | 1951 |
| D | trình duyệt thật bật hai bước rồi đăng nhập lại (T5) | 14 | 2002 |
| E | chính sách bắt buộc (T20) | 15 | 2051 |
| F | lối thoát bằng CLI (T30) | 8 | 2109 |
| G | token API, bảng mọi route (T40 đến T42) | 30 | 2704 |
| H | chứng chỉ panel với pebble, gia hạn, 421 (T50) | 26 | 2974 |
| I | khoá IP, phiếu quá 5 lần sai (T16, T17) | 10 | 3045 |
| J | quét bí mật (T60 đến T66) | 14 | 3156 |
| CUỐI | health, site | 2 | 3271 |

Cộng 13 dòng này ra 217, còn dòng tổng kết ghi 222. Năm khẳng định còn lại nằm sau "Phần CUỐI" (dòng 3276 đến 3280): script tự quét chính file log của nó và khẳng định log không chứa mật khẩu, bí mật, token, cookie, IP máy ảo, mạng Multipass, IP của Mac hay thư mục nhà. Năm dòng đó không thuộc phần nào nên không có dòng `====` riêng. Tổng: 222 đạt, 0 không đạt.

Lần chạy trước đó là 220 đạt, 2 không đạt; hai chỗ đỏ là điều kiện sai của chính script (biểu thức `jq` dùng `.qr` bên trong `all()`; dòng tóm tắt MCP tự chứa chữ `isError`), dữ liệu thật đúng. Sửa script rồi chạy lại từ máy sạch (nguồn: `docs/thay-doi/M20.md`, mục "E2E trên máy ảo").

Hồi quy: mỗi kịch bản chạy từ máy sạch trên bản M20 đã gộp `dev`. Dòng "TỔNG KẾT" của từng log:

| Kịch bản | Đạt | Không đạt | Log |
|---|---|---|---|
| M6 | 167 | 0 | `M20-hoi-quy-m6.log` |
| M13 | 293 | 0 | `M20-hoi-quy-m13.log` |
| M13 nâng cấp | 69 | 0 | `M20-hoi-quy-m13-nang-cap.log` |
| M15 | 200 | 0 | `M20-hoi-quy-m15.log` |
| M16 | 118 | 0 | `M20-hoi-quy-m16.log` |

Ở lượt đầu có ba chỗ đỏ, không chỗ nào do mã M20 (`docs/thay-doi/M20.md`): worktree thiếu gói phát hành `v0.1.2` mà e2e M15 cần; script bấm thật của M15 chờ chữ "Đã lên" trong khi `dev` đã đổi thành "Đã cập nhật lên"; e2e M15 khẳng định "Kiểm bản mới" trả `latest` rỗng, nhưng từ v0.2.1 bản phát hành có file `VERSION` nên `latest` là tên bản. Hai script được sửa cho đúng hành vi hiện tại. Ghi chú vì sao lại có hồi quy: M20 đụng đường đăng nhập, cookie và chứng chỉ, nên mọi kịch bản đăng nhập bằng cookie hay bấm giao diện của các mốc trước đều phải chạy lại.

Vài đoạn log đáng chép, để bạn thấy dữ liệu thật:

Lệch đồng hồ ở bước hai (log dòng 1755 đến 1818, tiêu đề "T3"). Mã của giờ máy lệch 90 giây bị từ chối cả hai chiều, lệch 15 giây thì được nhận:

```
=> ĐẠT: T3: mã của giờ máy −90 giây (bước 59851364): 401 mfa_code_wrong
=> ĐẠT: T3: mã của giờ máy +90 giây (bước 59851370): 401 mfa_code_wrong
=> ĐẠT: T3: mã của giờ máy −15 giây (bước 59851368): được nhận
=> ĐẠT: T3: mã của giờ máy +15 giây (bước 59851369): 204 (không còn lần sai nào để báo)
```

Điểm cần chú ý: máy ảo chạy mã `oathtool` (một công cụ độc lập, không phải mã của TPAN) để tính mã, nên đây là đối chiếu chéo với bên thứ ba. Log chỉ thử ±15 và ±90, không thử 45 giây; bảng 45 giây ở mục 2.1 vẫn chỉ có từ test trên Mac.

Khoá IP và phiếu (phần I, dòng 2977 đến 3045):

```
mã sai lần 1: HTTP 401 mfa_code_wrong
...
mã sai lần 4: HTTP 401 mfa_code_wrong
mã sai lần 5: HTTP 423 login_locked (Retry-After 900)
đăng nhập lại bằng ĐÚNG mật khẩu từ IP đang khoá: HTTP 423 login_locked (Retry-After 900)
```

Sau lần sai thứ 5, phiếu bị xoá khỏi `tpan.db` (`SELECT COUNT(*) FROM login_challenges WHERE user_name = ?` ra `0`), và từ Mac (IP khác) cùng user vẫn qua bước một (`200 mfa_required`). Nghĩa là khoá IP không khoá đường chính của user.

Quét bí mật (phần J, dòng 3048 đến 3156). Phép quét có đối chứng: trước hết quét các câu trả lời lần đầu (nơi bí mật được phép hiện) và phải thấy chúng, để chắc phép quét không mù: `totp=4 ... ma=20 ... token=1 ... tong=25`. Rồi quét `tpan.db` và `-wal`, `journalctl`, `/var/log`, sự kiện job qua API, mọi câu trả lời API đã lưu, output công cụ MCP, bản sao `tpan.db` của backup, và chính file log e2e. Cả tám chỗ ra `tong=0`. Danh sách quét gồm 4 bí mật TOTP, 40 mã dự phòng, 8 token.

## 3. Đi qua mã theo thứ tự chạy

Ta đi theo một lần đăng nhập của `khach_a` (có hai bước), rồi một lần gọi API bằng token, rồi tới chứng chỉ panel. Số dòng là của commit `a3efdc7`.

### 3.1 Chặng 1: `handleLogin` rẽ nhánh (internal/server/auth.go)

`handleLogin` (dòng 350) là hàm có từ M3. M20 chỉ thêm đúng một nhánh, và chỗ đặt nhánh là điều quan trọng.

```go
	if !found || u.Status != account.Active { ... return }
	// M20 (điểm ghép 3.4.1): user đã bật hai bước thì sang bước hai, KHÔNG xoá
	// bộ đếm sai của IP.
	if s.loginChallenge(w, r, name, ip, host) {
		return
	}
	if err := st.ClearLoginFailures(r.Context(), ip); err != nil {
```

Vì sao nhánh nằm **trước** `ClearLoginFailures`: bộ đếm "sai mật khẩu" theo IP là thứ chặn kẻ dò. Nếu mật khẩu đúng ở bước một xoá bộ đếm, kẻ có mật khẩu chỉ cần đăng nhập lại giữa các lần đoán mã để bộ đếm không bao giờ đầy. Với user có hai bước, chỉ bước hai đúng mới xoá (`handleLogin2FA`, mục 3.3). Test: `TestDoMaKhongResetBoDem`.

Hàm này cũng bắt đầu bằng `loginHost` (auth_m20.go dòng 76): request không có header `Host` thì trả 400 `host_missing` trước khi tốn argon2, vì phiên cần ghi tên máy (mục 3.9).

### 3.2 Chặng 2: `loginChallenge` tạo phiếu (internal/server/auth_m20.go dòng 218)

```go
	on, err := st.TOTPEnabled(r.Context(), name)
	if !on { return false }
	...
	token := base64.RawURLEncoding.EncodeToString(authRandom(32))
	st.CreateChallenge(r.Context(), store.Challenge{
		TokenSHA256: authTokenHash(token), User: name, IP: ip, Host: host,
		Created: now, Expires: now.Add(mfaChallengeTTL),   // 5 phút
	})
	mfaSetCookie(w, r, token, int(mfaChallengeTTL/time.Second))
	writeJSON(w, http.StatusOK, LoginResult{MFARequired: true})
```

Ba chi tiết. Token của phiếu là 32 byte ngẫu nhiên; DB chỉ giữ sha256 của nó (`TokenSHA256`), giống phiên. Phiếu ghi IP và tên máy lúc tạo. Cookie phiếu là `HttpOnly`, `SameSite=Strict`, và có tiền tố `__Host-` khi có TLS (`mfaCookieName`, dòng 175).

**Vì sao phiếu là bảng riêng `login_challenges`, không phải cột "đang chờ" trong `sessions`.** Mọi chỗ đọc phiên đều qua `authResolve` (auth.go dòng 113) rồi `authCurrent`, và hai hàm này chỉ đọc bảng `sessions` với cookie `tpan`. Phiếu nằm ở bảng khác với cookie khác, nên **không có đường mã nào biến phiếu thành phiên**, theo cấu trúc chứ không theo trí nhớ của người viết mã. Nếu là một cột, mỗi chỗ đọc phiên phải nhớ kiểm cột đó, quên một chỗ là có lỗ vượt bước hai. Test `TestPhieuChoKhongVaoRouteNao` duyệt mọi route của `routes()` chỉ mang cookie phiếu và đòi 401 ở mọi route không công khai.

### 3.3 Chặng 3: `handleLogin2FA` (internal/server/auth_m20.go dòng 269)

Thứ tự trong hàm: IP đang khoá thì từ chối ngay (`authLocked`); phiếu phải hợp lệ (`mfaChallenge`: có cookie, có trong DB, chưa hết hạn, **đúng IP, đúng tên máy**); user còn `active`; rồi mới kiểm mã.

```go
	method, ok := s.checkSecondFactor(w, r, ch.User, req.Code, sfPurpose{
		action: "login.2fa", challenge: ch.TokenSHA256, status: http.StatusUnauthorized,
	})
	if !ok { return }
	won, err := st.ConsumeChallenge(r.Context(), ch.TokenSHA256)   // DELETE, true khi đúng 1 dòng
	if !won { challengeInvalid(w, r); return }
	st.ClearLoginFailures(r.Context(), ip)                         // chỉ tới đây mới xoá đếm IP
	s.startSession(w, r, ch.User, ip, ch.Host)                     // phiên MỚI, token MỚI
```

`ConsumeChallenge` là một câu `DELETE` và chỉ một bên thắng nếu hai request cùng phiếu cùng lúc: phiên chỉ được tạo cho bên xoá được phiếu. `startSession` luôn sinh token phiên mới thay vì "nâng cấp" phiếu, để chống cố định phiên (kẻ biết phiếu không biết được phiên).

Nếu mã đúng mà kể từ lần đăng nhập đúng trước có người nhập sai, câu trả lời là 200 `{"wrong_codes": n}` thay vì 204, để giao diện báo "có N lần nhập sai mã, nếu không phải bạn thì đổi mật khẩu" (`TakeWrongSinceOK`).

### 3.4 Chặng 4: `checkSecondFactor` và khoá theo user (auth_m20.go dòng 377 đến 486)

Đây là hàm trung tâm. Cùng hàm này phục vụ bước hai đăng nhập, tắt hai bước, tạo lại mã dự phòng, tạo token, đổi mật khẩu. Nó có một ý tưởng: **mọi lần kiểm mã của một user nối đuôi nhau**.

```go
type mfaLocks struct {
	mu sync.Mutex
	m  map[string]chan struct{}   // mỗi user một kênh dung lượng 1
}
func (l *mfaLocks) lock(ctx context.Context, user string) (func(), bool) {
	...
	select {
	case ch <- struct{}{}:   return func() { <-ch }, true
	case <-ctx.Done():       return nil, false
	}
}
```

Kênh dung lượng 1 dùng như ổ khoá: gửi vào được là giữ khoá, đọc ra là nhả. Khác `sync.Mutex` ở chỗ chờ có thể bị huỷ theo `ctx` (client ngắt thì thôi chờ, trả 503 `mfa_busy`). Trong khoá, hàm làm theo thứ tự: đọc trạng thái khoá user **sau khi đã giữ khoá**; nếu đang khoá mà mã có dạng 6 số thì trả 423 `mfa_locked` **không chạy `Match`**; nếu không khoá thì mở bí mật, `Match`, rồi `UseTOTPStep`; sai thì ghi lần sai vào cả ba bộ đếm; cuối cùng mới nhả khoá.

Vì sao phải làm vậy: bản thiết kế đầu kiểm "đã khoá chưa" rồi mới so mã rồi mới ghi lần sai. Hàng nghìn request song song đều qua bước "chưa khoá" trước khi bộ đếm kịp tới 10. Khoá trong tiến trình biến "kiểm rồi ghi" thành một khối. Chỉ `tpan serve` kiểm mã (CLI không bao giờ), nên khoá trong một tiến trình là đủ. Test `TestBuocHaiSongSongKhongVuotTran`: 400 phiếu của một user, mỗi phiếu bắn 5 mã sai song song (2000 request); đòi `Match` chạy không quá `mfaUserMax` (10) lần và user bị khoá.

Mã dự phòng (80 bit, không dò được) vẫn được kiểm khi user đang bị khoá, và đúng thì gỡ khoá. Nếu không, kẻ có mật khẩu cứ gõ sai là nhốt được chủ tài khoản mãi.

Mã sai thì `mfaWrong` (dòng 508) ghi ba bộ đếm: phiếu (`BumpChallenge`: 5 lần thì xoá phiếu), user (`RecordTOTPFailure`: 10 lần thì khoá 15 phút, lần khoá thứ n liên tiếp dài `15 phút × 2^(n-1)`, trần 24 giờ), và IP qua `authFailed`.

### 3.5 Chặng 5: `mfa.Match` và `UseTOTPStep` (internal/mfa/mfa.go, internal/store/m20.go)

`Match` là chỗ trả lời "mã này có đúng không", và cũng là chỗ cửa sổ ±1 bước nằm:

```go
func Match(secret []byte, code string, now time.Time) (step uint64, ok bool) {
	if !IsCode(code) { return 0, false }
	cur := Step(now)                       // floor(unix / 30)
	for d := -Skew; d <= Skew; d++ {       // Skew = 1: thử bước trước, nay, sau
		...
		s := cur + uint64(d)
		if subtle.ConstantTimeCompare([]byte(HOTP(secret, s, Digits)), []byte(code)) == 1 {
			step, ok = s, true             // không dừng sớm: thử đủ 3 bước
		}
	}
	return step, ok
}
```

Từng dòng và vì sao:

- `Step(now)` là số giây từ 1970 chia 30, lấy phần nguyên. App và máy chủ cùng công thức, nên cùng giờ thì ra cùng bước, cùng mã.
- Máy chủ chấp nhận bước hiện tại, trước và sau (`Skew = 1`). Lệch đồng hồ `d` giây là lệch `d/30` bước. Lệch đúng 30 giây thì bước máy chủ hơn app đúng 1: vẫn nằm trong ±1, nhận 100% (bảng ở 2.1). Lệch 45 giây thì tuỳ app đang ở đầu hay cuối bước của nó mà lệch 1 hay 2 bước: nhận khoảng một nửa. **Lệch từ 60 giây trở lên thì luôn lệch ít nhất 2 bước**, nên không bao giờ nhận. Con số 60 không phải chọn, nó suy ra từ `Skew = 1` và bước 30 giây.
- `ConstantTimeCompare` và không `break`: thời gian trả lời không cho kẻ nghe biết mã khớp ở bước nào.
- Trả bước **lớn nhất** khớp, để `last_step` ghi cao nhất có thể. Hai bước liền kề có thể trùng mã (1 trên 10^6); ghi bước cao thì mã của bước thấp không dùng lại được.
- Dòng `d < 0 && cur < uint64(-d)` chặn tràn: `uint64` trừ quá 0 sẽ quay về 2^64-1. Đây là chỗ khó đọc nhất của file; xem mục 4.2.

Tại sao 6 số, 30 giây, SHA-1: đây là mặc định mà mọi app xác thực phổ biến nhận. SHA-256 thì một số app bỏ qua tham số `algorithm` trong mã QR và tính bằng SHA-1, ra mã sai mà không báo. HMAC-SHA1 không bị yếu đi bởi va chạm của SHA-1 (lý do dòng `//nolint:gosec` ở đầu `mfa.go`). Bí mật 20 byte (160 bit) đúng khuyến nghị của RFC 4226. Hàm `HOTP` là RFC 4226 mục 5.3: HMAC-SHA1 của bộ đếm 8 byte, lấy 4 bit cuối làm "con trỏ", đọc 4 byte tại đó, bỏ bit dấu, chia dư 10^6, đệm số 0 đứng đầu. Kiểm bằng vector thử của RFC 6238 phụ lục B (test `TestTOTPVectorRFC6238`), và mình đã chạy lại bằng một chương trình Go tạm (chỉ gọi `mfa.Step` và `mfa.HOTP`, không nằm trong repo):

```
bước 1 mã 8 số 94287082 mã 6 số 287082
```

Đúng với RFC: tại giây 59, bí mật `12345678901234567890`, mã 8 số là `94287082`.

Dùng lại mã: `Match` chỉ nói "đúng". Chống dùng lại là việc của store:

```go
`UPDATE user_totp SET last_step = ?, failures = 0, locked_until = NULL, lock_count = 0
 WHERE user_name = ? AND enabled_at IS NOT NULL AND last_step < ?`
```

`n == 1` dòng bị đổi nghĩa là nhận; `0` nghĩa là bước này (hoặc bước mới hơn) đã dùng rồi, server trả `mfa_code_used`. Điều kiện `last_step < ?` nằm trong chính câu `UPDATE`, nên hai request cùng mã đua nhau thì SQLite chỉ cho một bên đổi được dòng (test `TestHaiRequestCungMaChiMotThang` ở `internal/store/m20_batbien_test.go`, 20 goroutine). Mã `mfa_code_used` không tính là đoán sai: gõ lại cùng mã trong 30 giây là chuyện thường.

### 3.6 Chặng 6: bật hai bước và niêm phong bí mật (internal/server/api_m20.go, internal/mfa/mfa.go)

Bật hai bước có hai lần gọi: `handleMFASetup` (dòng 178) rồi `handleMFAEnable` (dòng 237).

```go
	secret := mfa.NewSecret()                       // 20 byte crypto/rand
	sealed, err := mfa.Seal(key, user, secret)      // AES-256-GCM, khoá ở /etc/tpan/secret.key
	st.SetPendingTOTP(ctx, user, sealed, s.now())   // dòng user_totp, enabled_at còn NULL
	uri := mfa.URI("TPAN", user+"@"+host, secret)   // otpauth://totp/TPAN:... để app quét
	qr, _ := mfa.QR(uri)                            // ma trận '0'/'1', trình duyệt vẽ SVG
```

`Seal` (mfa.go dòng 256) niêm phong bí mật bằng AES-256-GCM. Hai chi tiết đáng nhớ:

- `cipher.NewGCMWithRandomNonce` tự sinh nonce 12 byte ngẫu nhiên và đặt trước bản mã. Người viết mã không có chỗ nào để lỡ dùng lại nonce, thứ phá hỏng GCM. Mình đã đo: bí mật 20 byte thành 48 byte đã niêm phong (12 nonce + 20 bản mã + 16 thẻ xác thực).
- AAD (dữ liệu đi kèm được xác thực) là `"tpan-totp-v1:" + tên user`. Chép bản mã của user A sang dòng của user B thì `Open` lỗi. Mình đã thử: `Open` với user khác trả `không mở được bí mật hai bước (sai khoá, sai user hoặc dữ liệu hỏng)`.

Khoá nằm ở `/etc/tpan/secret.key` (32 byte, 0600, root), **ngoài** `tpan.db`. Lộ file DB hay bản sao lưu của nó thì không lộ bí mật. Khoá được tạo lúc `setup` đầu tiên (`mfaKey(ctx, true)`, auth_m20.go dòng 617) bằng `sys.WriteFileAtomic`, **không** qua `steps.File` hay engine: engine ghi mọi file vào bảng `managed_files`, và công cụ MCP `managed_file` trả nguyên nội dung các file đó cho AI. Khoá đi qua đường ấy thì ra thẳng nhà cung cấp AI. Có dòng `user_totp` mà file khoá mất thì không sinh khoá mới (bí mật cũ vẫn không mở được; khoá mới chỉ che lỗi) mà trả `mfa_secret_unavailable`.

`handleMFAEnable` đòi **mật khẩu trước**, rồi mới đọc bí mật chờ, so mã, và gọi:

```go
err = st.EnableTOTP(ctx, user, row.SecretEnc, step, hashes, cur.TokenSHA256, now)
// UPDATE user_totp SET enabled_at=? ... WHERE user_name=? AND enabled_at IS NULL AND secret_enc=?
```

`secret_enc = ?` trong `WHERE` là bản sửa của lỗ S5 (mục 5.4): bật đúng bí mật đã so mã chứ không bật "dòng đang chờ lúc đó". `EnableTOTP` cũng xoá mọi phiên khác của user trong cùng transaction, nên nếu người bật không phải chủ thật (có mật khẩu bị lộ) thì chủ thật bị đăng xuất và thấy ngay.

Mã dự phòng: `NewRecoveryCodes` sinh 10 mã `xxxx-xxxx-xxxx-xxxx` (16 ký tự `a-z2-7`, 80 bit), hiện **một lần**. DB chỉ giữ `sha256` của mã đã chuẩn hoá (`HashRecovery`). Dùng sha256 trần mà không argon2 vì mã 80 bit ngẫu nhiên thì dò ngược 2^80 là không làm được; argon2 cho 10 mã mỗi lần thử còn là đường làm máy hết RAM.

### 3.7 Chặng 7: guard và token (internal/server/routes.go, token_m20.go)

Mọi route đi qua `guard` (routes.go dòng 282). Dòng đầu của nhánh chạy là điểm rẽ của M20:

```go
		if hasAuthorization(r) {
			s.guardToken(w, r, rt)
			return
		}
```

Request có header `Authorization` (kể cả sai dạng) **chỉ** được xác thực bằng token, bỏ qua cookie. `guardToken` (token_m20.go dòng 113) đi theo thứ tự:

1. `tokenAuth`: thiếu `Bearer ` hay trùng header thì `token_invalid`; `apitoken.WellFormed` sai thì `token_malformed` (không chạm DB); `TokenByHash` không có thì `token_invalid`; đã thu hồi thì `token_revoked`; quá hạn thì `token_expired`; chủ token không còn hoặc không `active` thì `token_invalid`.
2. Route có nhóm token không? `rt.tok == ""` nghĩa là **chỉ phiên**, trả 403 `token_forbidden`.
3. Phạm vi: `GET` và `HEAD` cần quyền đọc, còn lại cần quyền ghi; ghi bao gồm đọc cùng nhóm. Thiếu thì 403 `token_scope`.
4. Chính sách bắt buộc hai bước: chủ token thuộc diện bắt buộc mà chưa bật thì 403 `mfa_enroll_required`.
5. `serveRule` với danh tính `account.Identity{Name, Role}` dựng từ **dòng `users` đọc ở request này**, tức vai hiện tại.

Bước 5 là "quyền = phạm vi giao quyền hiện tại": token `system:write` của một người vừa bị hạ từ admin xuống user vẫn bị `ruleAdmin` chặn (test `TestTokenTheoVaiHienTai`). Token không lưu vai.

Token có dạng `tpan_` + 32 ký tự base32 (20 byte ngẫu nhiên) + 7 ký tự CRC32 của 32 ký tự giữa, tổng 44 (`internal/apitoken/apitoken.go`). Tiền tố để công cụ quét bí mật (gitleaks, GitHub secret scanning) nhận ra token bị dán nhầm lên mạng. CRC bắt lỗi gõ sai một ký tự mà không phải hỏi DB. Mình đã sinh một token thử và đổi một ký tự: `WellFormed` trả `true` rồi `false`. DB lưu `sha256` (`api_tokens.token_sha256`) cộng `hint` (`tpan_` và 6 ký tự đầu) để nhận ra token trong danh sách; token thật chỉ hiện một lần lúc tạo, với `Cache-Control: no-store`.

**Bảng nhóm của từng route** là trường `tok` trong `routes()`. Ví dụ `{pattern: "GET /api/v1/sites", rule: anyone, tok: apitoken.GroupSites, ...}`. **Giá trị rỗng là chỉ phiên**: quên khai thì đóng, không mở. Hàm xuất `RouteTable()` (token_m20.go dòng 164) trả bảng này cho test và script e2e; `TestNhomTokenCuaRoute` so bảng thật với bảng mong đợi viết tay (B10).

**Vì sao một số route chỉ dùng được bằng phiên:**

- Mọi `/account/...` (mật khẩu, phiên, hai bước, token): đặc tả cấm token quản lý token, mật khẩu, hai bước. Nếu token tạo được token thì một token lộ ra thành token sống mãi.
- `/users...` và `/packages...`: `/users` đặt lại được mật khẩu và đổi vai người khác. Một token admin lộ có thể tạo admin mới hoặc đặt mật khẩu admin khác, tức biến "một token sống 7 ngày" thành "một tài khoản admin sống vĩnh viễn".
- `/plans...` và `/ai/...`: Plan do AI đề xuất phải có **người** duyệt (KE-HOACH mục 8.1). Token là thứ máy dùng; cho nó duyệt thì một agent tự duyệt Plan của chính nó.
- `/settings/auth` và `/tokens` (danh sách và thu hồi của admin).

Tạo token (`handleTokenCreate`, dòng 245) kiểm đầu vào (tên, phạm vi, số ngày so trần `auth.token_max_days`, mặc định 90, tối đa 365) **trước** khi đòi mật khẩu và mã. Nếu kiểm sau thì người dùng mất một mã TOTP (phải chờ 30 giây mã mới) chỉ vì gõ sai số ngày. Đòi mật khẩu (và mã nếu đã bật hai bước) vì token là thông tin đăng nhập mới sống lâu hơn phiên: phiên bị chiếm không được tự in token cho mình.

### 3.8 Chặng 8: phiên gắn tên máy và 421 (auth_m20.go dòng 93, host_m20.go)

Cookie không tách theo cổng (RFC 6265 mục 8.5). Cookie phiên panel đặt bởi `https://X:8443` cũng được trình duyệt gửi tới `https://X/` ở cổng 443, nơi PHP của chủ site `X` đọc được. Nên:

- Phiên và phiếu ghi `host` (tên máy, bỏ cổng, chữ thường) lúc tạo (`sessions.host`, `login_challenges.host`). `sessionHost` xoá phiên nếu request đến từ tên máy khác (và ghi nhật ký `session.host_mismatch`). Phiên của bản cũ có `host` rỗng thì được gắn tên máy ở lần dùng đầu, để nâng cấp không đăng xuất ai.
- `hostIsSite` (host_m20.go dòng 55) trong `secure` (auth.go): tên máy của request trùng domain của **bất kỳ** site nào (mọi trạng thái, của ai cũng vậy) thì trả 421 `host_is_site`, xoá phiên và phiếu nếu có, và in sẵn cách vào bằng IP hoặc tên miền của panel. Request có header `Authorization` được miễn vì không dùng cookie. Địa chỉ IP luôn được vào, nên không ai bị khoá ngoài.
- Cấm đặt `panel.domain` trùng tên một site, và cấm tạo site trùng `panel.domain` (hai chiều). Đổi hoặc xoá `panel.domain` thì xoá mọi phiên mang tên cũ.

### 3.9 Chặng 9: chứng chỉ panel (internal/core/panel_m20.go, internal/server/tls_m20.go)

Phần xin chứng chỉ là một Plan như các mốc trước. `Panel.PrepareDomain` (dòng 137) kiểm điều kiện đầu (chỉ đọc): tên không trùng site, stack đã cài, có email ACME, **không phải staging** (`panel_staging`), panel không ở chế độ riêng tư (`panel_private`), DNS trỏ về máy. Rồi dựng các bước:

```go
	cert := &steps.CertbotCert{Layout: l, Webroot: acmeRoot, Email: email, Server: cfg.ServerURL(), CertName: PanelLineage}
	ss := []engine.Step{
		&panelNameFree{store: p.Store, d: d},                                   // kiểm lại trùng tên dưới khoá toàn cục
		&steps.Dir{Path: acmeRoot + "/.well-known/acme-challenge", ...},
		&steps.ACMEProbe{Layout: l, Webroot: acmeRoot, Cert: cert},            // thử đường HTTP trước khi đụng gì
	}
	if stale { ss = append(ss, &steps.CertbotDelete{CertName: PanelLineage}) } // chỉ khi lineage đang mang bộ tên khác
	ss = append(ss, cert, set)                                                 // xin chứng chỉ, rồi ghi setting panel.domain
```

Output thật của Plan này trên máy ảo (`M20.log` dòng 2750 đến 2772, chạy `tpan panel domain set panel.m20.test --yes`; máy chủ ACME là pebble, một CA thử chạy cục bộ, không phải Let's Encrypt thật). Mỗi dòng "- ..." là một bước ở trên, theo đúng thứ tự:

```
- Kiểm panel.m20.test không thuộc site nào ...   (panelNameFree)
- Tạo thư mục /var/lib/tpan/acme/.well-known/acme-challenge ...   (Dir)
- Thử đường HTTP mà Let's Encrypt sẽ kiểm cho panel.m20.test ...   (ACMEProbe)
- Xin chứng chỉ Let's Encrypt cho panel ở panel.m20.test ...   (CertbotCert)
- Ghi tên miền của panel: panel.m20.test ...   (setting panel.domain)
```

Chú thích trong ngoặc là mình thêm để nối với mã; log không có phần này. Bước kiểm chứng sau đó báo `Panel phục vụ chứng chỉ Let's Encrypt cho panel.m20.test`. Từ chối trùng tên có bằng chứng ở T50 (dòng 2873 đến 2900): `409 panel_domain_is_site` khi đặt tên panel trùng site `a.example.com`, và `domain_is_panel` khi tạo site tên `panel.m20.test` bằng cả CLI (`--dry-run`, thoát 1) lẫn API.

- **HTTP-01 qua vhost mặc định.** Cổng 80 do web server của site giữ. Tên panel không thuộc site nào nên rơi vào vhost mặc định của tpan, vhost này phục vụ `/.well-known/acme-challenge/` từ webroot chung `/var/lib/tpan/acme` (đã đo ở 2.4). Không sửa cấu hình web server, không đụng vhost của site nào.
- **Lineage cố định `tpan-panel`** (`CertName`). Nếu panel dùng `--cert-name <tên miền>` như site thì hai bên chung thư mục `/etc/letsencrypt/live/<tên>`, và xoá hẳn site (`certbot delete`) xoá luôn chứng chỉ panel. Tên `tpan-panel` không có dấu chấm nên không bao giờ trùng một tên miền hợp lệ.
- **Bước `ACMEProbe` đứng trước `CertbotDelete`**: tên mới sai DNS thì dừng ở bước thử, panel không mất chứng chỉ cũ.
- Điều kiện đầu và `panelSetting` (bước ghi `panel.domain` với `Undo` trả giá trị cũ) nằm dưới khoá toàn cục của engine, nên "đặt tên panel" và "tạo site" cùng một tên đua nhau chỉ một bên thắng.

Phần phục vụ là `panelCerts.GetCertificate` (tls_m20.go dòng 153). Go gọi hàm này mỗi lần có bắt tay TLS mới, kèm tên máy mà trình duyệt gửi (SNI):

```go
	sni := strings.TrimSuffix(strings.ToLower(hello.ServerName), ".")
	if sni != "" {
		if d := p.domain(); d != "" && d == sni {          // setting panel.domain, đệm 2 giây
			c := p.acme.load()                             // stat 2 file, đổi thì nạp lại
			if c != nil && now.After(c.Leaf.NotBefore) && now.Before(c.Leaf.NotAfter) && c.Leaf.VerifyHostname(sni) == nil {
				return c, nil
			}
		}
	}
	return p.selfSigned()                                  // mọi trường hợp còn lại: tự ký như v0.2.0
```

Các quyết định ở đây:

- **Chọn theo setting, không theo file.** Bỏ tên hay job hỏng (Undo trả setting cũ) thì chứng chỉ còn trên đĩa không được phục vụ. Panel không đặt tên thì phục vụ đúng chứng chỉ tự ký như v0.2.0 với mọi SNI (B9; test `TestChungChiTheoSNI`).
- **Gia hạn không restart.** `certSlot.load` so (mtime, cỡ) của hai file mỗi lần bắt tay; certbot đổi file lúc gia hạn thì lần bắt tay kế tiếp nạp bản mới. Nạp hỏng thì giữ bản đang chạy, nhớ dấu của lần hỏng và ghi log một lần (không nạp lại cho tới khi file đổi). Khẳng định "serial mới được phục vụ mà `MainPID` của `tpan.service` không đổi" đã có bằng chứng trên máy ảo (T50, `M20.log` dòng 2843 đến 2870, với pebble làm máy chủ ACME thử): `MainPID` là 1680 trước khi đặt tên panel (dòng 2746) và vẫn 1680 sau `certbot renew --cert-name tpan-panel --force-renewal`; serial phục vụ đổi từ `0BEE040B1F06CAD4` sang `0C2365875FC4564D`, trùng serial của lineage `tpan-panel` trên đĩa (`serial mới của lineage khác serial cũ và được panel phục vụ`; `MainPID của tpan.service không đổi (1680)`).
- Hai file đọc bằng `os.Stat`, không qua `system.System`, nên `internal/system/noos_test.go` có một ngoại lệ ghi rõ cho đúng file này (M20.md).
- **Không nhận staging**: chứng chỉ staging của Let's Encrypt không được trình duyệt tin, nên người dùng vẫn phải bấm "vẫn tiếp tục", đúng điều M20 muốn bỏ. Cài mặc định có `ssl.staging` bật, nên `PrepareDomain` từ chối và chỉ lệnh tắt staging; `ssl.server` có giá trị (máy chủ ACME thử của e2e) thì cho qua.
- **Tên panel bị công khai.** Mọi tên được cấp chứng chỉ đều xuất hiện trong nhật ký Certificate Transparency, ai cũng đọc được. Giao diện nói rõ điều này, và `--private` bị từ chối vì tên miền không dùng được qua `ssh -L`.

Chứng chỉ tự ký mới sinh (lúc `tpan setup`) có hạn tối đa 825 ngày thay vì 10 năm. Chứng chỉ đang có giữ nguyên, để nâng cấp không bắt ai chấp nhận lại cảnh báo.

### 3.10 Chặng 10: bắt buộc hai bước và lối thoát

`enrollRequired` (api_m20.go dòng 56) trả true khi chính sách `auth.require_2fa` (`off`, `admin`, `all`) phủ vai của người này mà họ chưa bật. Guard kiểm **mỗi request**, không chụp lúc đăng nhập, nên bật chính sách là áp ngay cho phiên đang mở. Người bị bắt buộc chỉ gọi được các route có cờ `allowEnroll` (xem trạng thái, `setup`, `enable`, thoát "xem như", cùng các route công khai), còn lại nhận 403 `mfa_enroll_required` và giao diện đưa sang `/bat-hai-buoc`. Token của họ cũng bị chặn như phiên.

Admin không tự nhốt mình: bật chính sách `admin` hay `all` khi chính admin chưa bật hai bước thì 409 `mfa_enroll_self_first`. Lối thoát cuối cùng là root trên máy: `tpan user 2fa policy off` và `tpan user 2fa reset <tên>` (CLI chạy bằng root nên không bị chính sách, và có nhật ký).

## 4. Kiến thức nền

### 4.1 HMAC và TOTP

HMAC là "băm có khoá": cùng dữ liệu, khác khoá cho dấu vân tay khác, và không có khoá thì không tạo lại được. TOTP lấy dấu vân tay của `số bước thời gian` bằng bí mật chung, rồi cắt thành 6 số. App và máy chủ không nói chuyện với nhau lúc đăng nhập; họ chỉ cùng biết bí mật và cùng đọc đồng hồ. Trong mã: `HOTP` ở `internal/mfa/mfa.go`. Tự thấy: lệnh 1 ở mục 7 chạy 3000 lần `HOTP` và `Match` và in bảng.

### 4.2 `uint64` quay vòng

Trong Go, `uint64` không có số âm: `3 - 5` ra `18446744073709551614`, không phải `-2`. Trong `Match`, bước "hiện tại trừ 1" khi giờ là giây 0 sẽ thành số khổng lồ. Mã chặn bằng `if d < 0 && cur < uint64(-d) { continue }`. Với giờ thật `cur` cỡ 58 triệu nên không bao giờ chạm, nhưng test `TestMatchBuocKhong` dùng giờ gần 1970 (`Step(now) = 0`) để chắc chỗ này không tràn.

### 4.3 AES-GCM, nonce và AAD

AES-GCM vừa mã hoá vừa kiểm "dữ liệu có bị sửa không" (thẻ xác thực 16 byte). Nonce là số dùng một lần cho mỗi lần mã hoá; dùng lại nonce với cùng khoá là phá vỡ bảo mật, nên mã dùng `NewGCMWithRandomNonce`. AAD là dữ liệu không mã hoá nhưng được ràng vào thẻ: ở đây là tên user. Trong mã: `Seal`, `Open` ở mfa.go. Tự thấy trên Mac: lệnh 1 ở mục 7 không in cái này; chạy `go test ./internal/mfa -run TestNiemPhong -v`. Mình chưa chạy riêng lệnh đó; kết quả `ok` của cả gói ở mục 2.2.

### 4.4 Băm và mã hoá khác nhau thế nào

Mật khẩu, token, mã dự phòng: máy chủ chỉ cần biết "đúng hay sai", nên lưu băm một chiều (sha256). Bí mật TOTP: máy chủ phải tính ra mã 6 số mỗi lần, cần chính bí mật, nên phải mã hoá hai chiều (AES-GCM). Sha256 trần chấp nhận được với token và mã dự phòng vì chúng có 160 và 80 bit ngẫu nhiên; mật khẩu người đặt thì yếu nên dùng argon2 (đã có từ M3). Tự thấy trên Mac:

```bash
printf 'abcd-efgh' | shasum -a 256 | cut -c1-16
```

Mình chạy ra `5fbda72a949e18fe` (chuỗi khác cho ra chuỗi khác hoàn toàn, và không suy ngược được).

### 4.5 So sánh có điều kiện trong một câu SQL (compare-and-swap)

Thay vì "đọc, kiểm, ghi" (hai request chen vào giữa được), ta viết `UPDATE ... WHERE <điều kiện cũ còn đúng>` rồi xem số dòng bị đổi. `1` là thắng, `0` là thua. SQLite xếp các câu ghi nối đuôi nhau (DSN `_txlock=immediate`, một kết nối), nên không có kẽ hở. Dùng ở bốn chỗ: `UseTOTPStep` (`last_step < ?`), `UseRecoveryCode` (`used_at IS NULL`), `EnableTOTP` (`enabled_at IS NULL AND secret_enc = ?`), `ConsumeChallenge` (xoá phiếu). Trong mã: `internal/store/m20.go`.

### 4.6 Cookie host-only, `__Host-`, `SameSite`

Cookie "host-only" chỉ được gửi lại đúng tên máy đã đặt nó, **nhưng không phân biệt cổng**. Tiền tố `__Host-` bắt buộc `Secure`, `Path=/` và không có `Domain`, nên không tên con nào đặt đè được. `SameSite=Strict` chặn gửi cookie khi bấm từ trang khác. Trong mã: `authSetCookie` (auth.go dòng 203), `mfaSetCookie` (auth_m20.go dòng 183). Tự thấy trong trình duyệt ở panel thật có TLS: công cụ nhà phát triển, tab Application, Cookies: thấy `__Host-tpan`. Chưa chạy trên máy ảo.

### 4.7 Bearer token và vì sao không có CSRF

CSRF là trang lạ bắt trình duyệt của bạn gửi request kèm cookie của bạn. Trình duyệt **không tự gửi** header `Authorization`, nên request bằng token không bị CSRF. Công cụ dòng lệnh không gửi `Origin`, `Sec-Fetch-Site`, và `http.CrossOriginProtection` cho qua đúng những request như vậy. Trong mã: `hasAuthorization`, `tokenAuth`. Tự thấy: mục 2.3, dùng `curl -H "Authorization: Bearer ..."`.

### 4.8 SNI và `GetCertificate`

Trước khi TLS mã hoá, trình duyệt nói tên máy nó muốn vào (SNI). Một cổng, một địa chỉ IP vẫn phục vụ được nhiều chứng chỉ theo tên đó. Vào bằng IP thì không có SNI, nên rơi về chứng chỉ tự ký. Trong mã: `GetCertificate` ở tls_m20.go. Tự thấy trên máy ảo: `openssl s_client -connect 127.0.0.1:8443 -servername panel.m20.test </dev/null 2>/dev/null | openssl x509 -noout -issuer -subject`, rồi đổi `-servername` sang tên khác (hoặc `-noservername`) để thấy nhà phát hành đổi. Lệnh y nguyên này mình chưa chạy; e2e đã chạy cùng cách đó qua `scripts/e2e-m20/chung-chi.sh` (`openssl s_client` rồi `openssl x509`) và log ghi ba trường hợp (`M20.log`, phần H):

```
dòng 2740  (chưa đặt tên, SNI panel.m20.test): tu-ky=co    issuer=CN=tpan-m20
dòng 2804  (đã đặt tên, SNI panel.m20.test):   tu-ky=khong issuer=CN=Pebble Intermediate CA 0fa61e
dòng 2812  (vào bằng IP, không SNI):           tu-ky=co    issuer=CN=tpan-m20
```

Ba dòng này mình rút gọn từ các dòng `phuc-vu: ...` của log (bỏ serial và danh sách SAN). Đúng B9: có SNI khớp tên panel thì chứng chỉ do CA ký; vào bằng IP thì vẫn là chứng chỉ tự ký cũ.

### 4.9 ACME HTTP-01, certbot lineage, Certificate Transparency

ACME là giao thức xin chứng chỉ tự động. HTTP-01: CA bảo bạn đặt một file ở `http://<tên>/.well-known/acme-challenge/...` rồi CA tải về để chứng minh bạn giữ tên đó. `certbot` làm việc này; một "lineage" là một thư mục trong `/etc/letsencrypt/live/<tên>` kèm cấu hình gia hạn trong `/etc/letsencrypt/renewal/<tên>.conf`. Certificate Transparency là sổ công khai ghi mọi chứng chỉ được cấp, nên tên panel công khai. Trong mã: `steps.CertbotCert`, `PanelLineage = "tpan-panel"`. Tự thấy trên máy ảo: `sudo certbot certificates`. Lệnh này mình chưa chạy. Log e2e có thứ gần với nó: dòng `lineage-tpan-panel: serial=0BEE040B1F06CAD4 san=DNS:panel.m20.test` (dòng 2806) đọc thẳng từ `/etc/letsencrypt/live/tpan-panel/fullchain.pem`, và sau `tpan panel domain set --clear --yes` thì `lineage tpan-panel đã bị xoá (certbot delete)` (phần H). `sudo ls /etc/letsencrypt/live/` cũng cho thấy thư mục `tpan-panel` (chưa chạy).

### 4.10 Kênh Go làm ổ khoá

`make(chan struct{}, 1)` là một hộp chứa tối đa một phần tử. Gửi vào được thì giữ khoá; hộp đầy thì phải chờ. Khác `sync.Mutex`, `select` cho phép chờ **hoặc** bỏ khi `ctx.Done()` (request bị ngắt). Trong mã: `mfaLocks.lock`. Xem thêm khái niệm `context` và `goroutine` ở `docs/giai-thich/00-go-can-ban.md`.

## 5. Cách khác đã cân nhắc, và chỗ đã làm sai

### 5.1 Dùng thư viện OTP thay vì tự viết TOTP

`github.com/pquerna/otp` v1.5.0 (kéo thêm `github.com/boombuler/barcode`) làm được. Không chọn: phần TOTP chỉ vài chục dòng thư viện chuẩn (`crypto/hmac`, `crypto/sha1`, `encoding/binary`) và có vector thử chính thức trong RFC; thêm hai mô-đun cho chừng đó trái quy ước "không thêm thư viện khi thư viện chuẩn làm được" (KE-HOACH mục 14). Thư viện mới duy nhất của M20 là `rsc.io/qr` v0.2.0 (BSD-3, không phụ thuộc gì), vì Go chuẩn không có QR và tự viết bộ mã hoá QR (Reed-Solomon, mặt nạ) mà không có bộ giải mã để thử là rủi ro. Chỉ import gói gốc: gói con `libqrencode` dùng cgo, `CGO_ENABLED=0` sẽ hỏng.

### 5.2 Phiếu là một cột trong `sessions`

Đã giải thích ở 3.2. Một cột thì mọi chỗ đọc phiên phải nhớ kiểm; bảng riêng thì `authResolve` không thể đọc nhầm. Chọn bảng riêng.

### 5.3 Chép chứng chỉ ACME đè chứng chỉ tự ký

Cách này đơn giản (panel đọc một file), nhưng đổi file mà `managed_files` và phép kiểm quyền đang theo dõi, mất chứng chỉ tự ký để vào bằng IP, và vẫn phải restart panel khi gia hạn (cắt job đang chạy). Chọn `GetCertificate` theo SNI: không đổi file nào, vào bằng IP vẫn chạy, gia hạn không restart.

### 5.4 Chỗ đã làm sai trong thiết kế mốc này

Bản thiết kế đợt 0 do phiên chính viết. Một thợ soát (model opus, effort max) đọc **thiết kế và mã khung** (chưa có thân hàm nào) ngày 21/11/2026 và tìm ra 3 điểm NẶNG, 6 VỪA, 4 NHẸ (mục 10 của `M20-thiet-ke.md`). Phiên chính nhận cả 13 điểm và đè lên mục 3 đến 8 ở mục 11. Một thợ soát lượt hai (23/11/2026) không còn điểm nặng. Mọi con số xác suất trong mục 10 là ước lượng trên giấy, chưa đo. Ba điểm nặng:

**S1. Đoán mã song song vượt mọi trần chống dò.** Thiết kế đầu kiểm "user có đang khoá không", rồi so mã, rồi mới ghi lần sai. Kẻ có mật khẩu gửi `POST /login` liên tục để có vài trăm đến vài nghìn phiếu, rồi bắn 5 mã cho mỗi phiếu cùng lúc: mọi request qua bước "chưa khoá" trước khi bộ đếm kịp tới 10. Vài nghìn lần đoán một đợt, mỗi lần trúng cỡ 3 phần triệu (3 mã đúng trên 10^6). Câu "tốn argon2 cho kẻ tấn công" trong thiết kế cũng sai: argon2 chạy ở máy chủ, kẻ tấn công chỉ tốn một request. Sửa: khoá trong tiến trình theo user bao trọn kiểm, so, ghi (mục 3.4); test `TestBuocHaiSongSongKhongVuotTran`.

**S2. Site và panel cùng tên máy làm lộ cookie phiên admin cho chủ site.** Thiết kế đầu chỉ xét hại ở lineage chứng chỉ. Hại thật là cookie: cookie không tách theo cổng, nên PHP của chủ site tên X đọc được cookie phiên panel đặt bởi `X:8443`, rồi phát lại. Các đường để hai tên cùng tồn tại: đổi tên panel (tên cũ hết được giữ chỗ), đua giữa CLI và job tạo site, job hỏng ở bước kiểm chứng sau khi certbot đã cấp. Sửa: phiên gắn tên máy, 421 cho mọi tên site, xoá phiên khi đổi tên panel, kiểm trùng tên dưới khoá toàn cục, `GetCertificate` chọn theo setting (mục 3.8 và 3.9). Bản sửa đầu còn miễn 421 cho site của admin; lượt soát hai bỏ miễn đó (mục 11.12 R2): mọi tên site, của bất kỳ ai, đều 421.

**S3. Token chỉ đọc lấy được mật khẩu hiện một lần.** `GET /jobs/{id}` được xếp nhóm `jobs:read` như "chỉ đọc", nhưng khi job đã xong nó lấy rồi xoá mật khẩu hiện một lần của job (mật khẩu admin WordPress, mật khẩu DB). Danh tính dựng từ token không có `ViewedBy`, nên mã coi như người thật mở job. Token chỉ đọc lộ ra thì kẻ giữ nó gọi `GET /jobs?limit=5` rồi `GET /jobs/{id}` mỗi giây, lấy mật khẩu của mọi site mới. Sửa: `heldSecret` ghi id token đã khởi job (`starterToken`); request bằng token chỉ lấy mật khẩu khi id trùng, không thì không thấy và không làm mất chúng. Test `TestTokenChiDocKhongLayMatKhauJob`, `TestJobGhiTokenKhoi`.

Sáu điểm VỪA đáng nhớ:

- **S5, bật nhầm bí mật.** `EnableTOTP` bật "dòng đang chờ lúc đó", không phải "đúng bí mật vừa so mã". Kẻ có cookie phiên gọi `setup` đúng lúc nạn nhân bấm Xác nhận thì nạn nhân bật bí mật của kẻ kia. Sửa: thêm `secret_enc = ?` vào `WHERE` (mục 3.6). Test `TestDuaSetupVoiEnable`.
- **S6, khoá AES đi qua engine.** Ghi file mặc định của TPAN là `steps.File`, vào `managed_files`, rồi công cụ MCP `managed_file` trả nội dung cho AI. Sửa: khoá được tạo trực tiếp bằng `sys.WriteFileAtomic` trong `tpan serve` (mục 3.6).
- **S7, khoá cố định và không báo ai.** 10 lần mỗi 15 phút là khoảng 960 lần đoán mỗi ngày. Sửa: khoá leo thang (gấp đôi mỗi lần, trần 24 giờ), một dòng nhật ký `login.2fa_locked` mỗi lần khoá, và phép kiểm `tpan.2fa:attempts` cảnh báo "mật khẩu của <user> có thể đã lộ".
- **S8, token sống qua mọi bước đuổi kẻ lạ.** Tạo token chỉ đòi mật khẩu; đổi mật khẩu chỉ huỷ phiên. Sửa: user đã bật hai bước thì tạo token đòi thêm mã; admin đặt lại mật khẩu thì thu hồi mọi token của user đó.
- **S9, `CertbotDelete` không hiểu `CertName`.** Chỉ thêm trường `CertName` cho `CertbotCert` thôi thì bước xoá lineage luôn lỗi hoặc luôn bị bỏ qua (Check tìm `live/<tên miền>` thay vì `live/tpan-panel`). Sửa: `CertbotDelete` có `CertName` thì dùng `--cert-name <CertName>` và stat đúng thư mục lineage; test `TestCertbotDeleteTheoCertName`.
- **S4, staging.** Cài mặc định có `ssl.staging` bật, panel nhận chứng chỉ "(STAGING)", trình duyệt vẫn cảnh báo mà mọi phép kiểm báo đạt. Sửa: `PrepareDomain` từ chối `panel_staging`.

Chỗ khác nhận ra khi viết mã (M20.md): `RecordTOTPFailure` không thể là một câu `UPDATE` vì thời điểm hết khoá phụ thuộc `lock_count` mới, nên nó là một transaction đọc-tính-ghi (cần `_txlock=immediate`); `authFailed` luôn ghi `wrong_password` nên server gọi nó với thao tác `<action>.ip` và tự ghi thêm dòng `login.2fa`/`wrong_code`.

**Soát cuối trên mã đã viết (23/11/2026).** Một thợ opus chưa đọc mã mốc này soát mã thật và viết test khai thác (chạy trên Mac): 1 NẶNG, 1 VỪA, 3 NHẸ, đều đã sửa và có test (mục "Soát cuối (đọc mã)" của `docs/thay-doi/M20.md`).

- **F1 (nặng): cửa sau bằng một phiên bị chiếm.** Mọi thao tác của M20 đã đòi mật khẩu, nhưng route cũ của M13 `POST /api/v1/users` thì không: kẻ chỉ có cookie phiên admin tạo được một user admin mới (biết mật khẩu, không có hai bước), rồi từ user đó in token 365 ngày. `tpan admin passwd` đuổi được phiên bị chiếm nhưng không đuổi được cửa sau. Bài học: bảo vệ một cửa (token) mà quên cửa bên cạnh (tạo tài khoản) là chưa bảo vệ. Sửa: tạo user, nâng một người lên admin, đặt lại mật khẩu người khác, nới chính sách đăng nhập đều đòi mật khẩu (và mã hai bước nếu đã bật) của admin đang làm.
- **F2 (vừa):** token `backups:write` đổi được đích sao lưu ngoài máy (M18) sang kho của kẻ khác, tức rút được dữ liệu mọi site. Sửa: đổi đích và cloud-init chỉ dùng được bằng phiên.
- **F3, F4, F5 (nhẹ):** token `health:read` chạy được lượt health đầy đủ qua `?refresh=1`; trần 5 lần mỗi phiếu và mỗi IP còn "kiểm trước, ghi sau" (trần 10 lần theo user vẫn đúng); trang 421 in tên miền panel cho người lạ.

**Phát hiện bằng cách nào:** đọc thiết kế rồi đọc mã khung và đi theo từng kịch bản tấn công trên giấy, chưa chạy gì. Mỗi bản sửa có test đơn vị (xanh trên Mac). Sau đó e2e trên máy ảo (25/11/2026, `M20.log`, 222 đạt, 0 không đạt) xác nhận một số bản sửa bằng máy thật, nhưng không phải tất cả:

- **S2 (cookie lộ cho chủ site): có bằng chứng máy ảo.** Phần H, dòng 2901 đến 2931: Host là tên của site `a.example.com` ở đường `/api/v1/session` trả `421 host_is_site` (JSON); Host `khach.example.org` (site của user thường, đường giao diện) trả 421 và trang HTML; cùng Host mà có `Authorization` (token) thì được miễn, 200; Host là `panel.m20.test` thì 200. Chặn đặt tên panel trùng site: `409 panel_domain_is_site`; tạo site tên panel: `domain_is_panel` cả CLI lẫn API (dòng 2873 đến 2900). Chưa thấy trong log: cookie thật bị xoá khi đổi tên panel, và cuộc đua CLI với job tạo site (chỉ có test đơn vị).
- **S3 và F2 (token lấy quá quyền): chỉ có gián tiếp.** T40 (dòng 2482 đến 2653) dựng bảng từ `RouteTable()` rồi thử mọi route với từng kiểu token: `T40: mọi route × {hết hạn 401 token_expired, thu hồi 401 token_revoked, chỉ phiên 403 token_forbidden, thiếu phạm vi 403 token_scope, chỉ đọc trên route ghi 403 token_scope}, đủ phạm vi không 401/403` và `không có dòng SAI`, trên máy thật với 110 route API không công khai trở lên. Việc "token chỉ đọc không lấy được mật khẩu job" không có phép thử riêng trong log; phần J chỉ cho thấy 9 job được đọc qua API và 11 lời gọi MCP bằng một token đủ phạm vi, và không bí mật nào lọt ra ngoài các câu trả lời lần đầu.
- **S1 (đoán mã song song), F1 đến F5 (soát cuối): chưa có bằng chứng máy ảo.** Log e2e không bắn mã song song và không thử cửa sau của F1. Bằng chứng vẫn là test đơn vị trên Mac (`TestBuocHaiSongSongKhongVuotTran`, `TestPhieuSongSongKhongVuotTran5`, `TestTaoUserCanXacNhan` và các test khác). Phần "thử phá trên máy ảo" của soát cuối đang chạy lúc viết; `docs/bang-chung/M20-thu-pha.log` chưa có, xem `docs/thay-doi/M20.md` khi nó xong.
- **Bản sửa nâng cấp: có bằng chứng.** Phần U (T80 đến T85): cookie của v0.2.0 vẫn là phiên sau khi nâng (`T81: ... 200`), đăng nhập cũ vẫn 204 không có chặng hai (`T82`), hai bước mặc định tắt, `/etc/tpan/secret.key` chưa tồn tại (`T85`), health có cảnh báo vàng `tpan.2fa:admins`.

Ba chỗ đỏ của hồi quy lượt đầu (mục 2.6) cũng là "agent làm sai" nhưng ở đồ nghề, không ở mã M20: thiếu gói `v0.1.2` trong worktree, hai kịch bản cũ (M15) khẳng định theo chữ và hành vi đã đổi ở `dev`. Phát hiện bằng cách chạy hồi quy trên máy ảo và đọc chỗ đỏ. Bài học cho người review: kịch bản e2e cũ là mã, và mã cũ hỏng khi `dev` đổi chữ trên giao diện.

## 6. Chưa có trong tay

Từ 25/11/2026 phần lớn đã có (mục 2.6): e2e bước 23 đạt 222, không đạt 0, và năm hồi quy xanh. Những thứ dưới đây **vẫn không** có bằng chứng từ máy ảo:

- Soát cuối kiểu "thử phá trên máy ảo" bởi một thợ chưa đọc mã mốc này (mục 8, đợt 4 của thiết kế). Đang chạy lúc viết; `docs/bang-chung/M20-thu-pha.log` chưa có. Xem `docs/thay-doi/M20.md`.
- Đoán mã song song (S1) và cửa sau F1 đến F5 trên máy thật: log e2e không bắn mã song song, không tạo admin bằng cookie bị chiếm. Chỉ có test đơn vị trên Mac.
- Bảng lệch đồng hồ ở 45 giây trên máy ảo: e2e chỉ thử ±15 và ±90 giây (T3). Con số 50% chỉ từ test Mac.
- Tên cookie `__Host-tpan` và `__Host-tpan-2fa` trên panel có TLS: log không in tên cookie. Mở trình duyệt xem mới chắc (mục 4.6).
- Độ dài 48 byte của `user_totp.secret_enc` (lệnh 6 mục 7): log không có truy vấn này. Có bằng chứng gián tiếp: `tpan.db`, `-wal` và bản sao backup không chứa bí mật TOTP nào của bốn user (T60 và T66, tổng bằng 0 sau phép quét đối chứng thấy 4 bí mật ở câu trả lời lần đầu).
- Gia hạn chứng chỉ bằng timer thật của certbot (không ép): e2e dùng `--force-renewal` và `renew --dry-run` do bước kiểm chứng gọi, với pebble. Chưa thử với Let's Encrypt thật, và chưa thử trên VPS.
- Demo `--demo` chưa chạy được "đặt tên miền panel" thật (certbot giả chưa hiểu lineage `tpan-panel`), chỉ hiện bản xem trước và trạng thái có sẵn. (Trên máy ảo thì đã chạy thật với pebble, `M20.log` phần H.)

Đã có từ e2e, để bạn khỏi tưởng còn thiếu: `oathtool` đối chiếu mã (T3, dòng 1740 `mã oathtool tính trong máy ảo (bước 59851367) được máy chủ nhận khi bật: 200`), `zbarimg` đọc QR (T4, dòng 1730 `zbarimg đọc PNG ra đúng URI otpauth:// mà setup trả`), trình duyệt thật (phần D), mã sai, bước -2, mã dùng lại, mã dự phòng (T10 đến T14), khoá IP 423 (T17), chính sách bắt buộc (T20, T21), `tpan user 2fa reset` (T30), bảng route × token (T40 đến T42), chứng chỉ panel với pebble và `MainPID` không đổi (T50), quét bí mật (T60 đến T66), nâng từ v0.2.0 thật bằng `tpan self-update` của v0.2.0 (T80 đến T85).

## 7. Tự thử lại

Tối đa 6 lệnh. Lệnh 1 đến 4 chạy ở **Mac**, trong thư mục repo `tpan-M20`, và mình đã chạy thật trong phiên viết (mục 2). Lệnh 5 và 6 chạy ở **máy ảo**; máy ảo là tên trong `.tpan-vm` (hôm viết là `tpan-m20`) và phải có bản mới (`make vm-deploy`) cùng stack, một site, và đã có người bật hai bước. Lệnh 5 đã có output thật từ e2e (mục dưới); lệnh 6 mình chưa chạy.

1. **Mac**: bảng lệch đồng hồ.

   ```bash
   go test ./internal/mfa -run TestTOTPLechDongHo -v
   ```

   Sẽ thấy bảng ở mục 2.1: 100% tới lệch 30 giây, khoảng 50% ở 45 giây, 0% từ 60 giây.

2. **Mac**: vector của RFC 6238 và bốn gói của M20.

   ```bash
   go test ./internal/mfa ./internal/apitoken ./internal/store ./internal/server -count=1
   ```

   Sẽ thấy bốn dòng `ok`. Muốn xem riêng vector của RFC: thêm `-run TestTOTPVectorRFC6238 -v` cho gói `./internal/mfa`.

3. **Mac**: các bất biến chạy từng cái, ví dụ B1 (phiếu không vào được route nào) và đoán mã song song.

   ```bash
   go test ./internal/server -run 'TestPhieuChoKhongVaoRouteNao|TestBuocHaiSongSongKhongVuotTran|TestNhomTokenCuaRoute' -v
   ```

   Sẽ thấy ba dòng `--- PASS`.

4. **Mac**: chạy panel demo ở một cửa sổ terminal rồi đi hết luồng hai bước ở cửa sổ khác.

   ```bash
   go run ./cmd/tpan serve --demo --listen 127.0.0.1:18200
   ```

   Nó in mật khẩu của admin, của `khach_a`, và bí mật hai bước của `khach_a`. Ở cửa sổ khác, đăng nhập `khach_a` bằng `curl -s -i -c cj.txt -X POST http://127.0.0.1:18200/api/v1/login -d '{"name":"khach_a","password":"<mật khẩu in ra>"}'`. Sẽ thấy `200` với `{"mfa_required":true}` và cookie `tpan-2fa` (như mục 2.3). Tính mã cần một app xác thực hoặc `oathtool` (mình chưa chạy `oathtool` trong phiên này; mình đã tính mã bằng `internal/mfa` trong một chương trình Go tạm).

5. **Máy ảo, quyền root** (từ Mac: `scripts/vm.sh root bash`): xem khoá niêm phong. **Đã chạy bởi e2e** (`M20.log` dòng 1749 đến 1752, sau khi admin bật hai bước lần đầu).

   ```bash
   stat -c '%a %U %s %F %n' /etc/tpan/secret.key
   ```

   Output thật trong log:

   ```
   600 root 32 regular file /etc/tpan/secret.key
   ```

   Nghĩa là file thường, 32 byte, quyền 0600, chủ root. Trước khi có ai bật hai bước, file chưa tồn tại vì nó chỉ được tạo ở lần `setup` đầu tiên. Log cũng có phía "trước": sau khi nâng từ v0.2.0, T85 ở dòng 570 đến 577 chạy `ls -la /etc/tpan/` và in `không có /etc/tpan/secret.key`. Phép kiểm sức khoẻ cũng báo: `pass tpan.secretkey: File khoá /etc/tpan/secret.key đúng: file thường 32 byte, 0600, root` (dòng 1997).

6. **Máy ảo, quyền root**: xem bí mật trong DB là dữ liệu nhị phân, không phải chuỗi đọc được. **Chưa chạy** (log e2e không có truy vấn này).

   ```bash
   python3 -c "import sqlite3; c=sqlite3.connect('file:/var/lib/tpan/tpan.db?mode=ro', uri=True); print([(r[0], len(r[1]), r[1][:6]) for r in c.execute('SELECT user_name, secret_enc, 1 FROM user_totp')])"
   ```

   Sẽ thấy tên user, độ dài 48, và vài byte đầu dạng `b'...'` lộn xộn (nonce ngẫu nhiên), không có chuỗi base32 32 ký tự của bí mật. (Cách gọi `python3` với `sqlite3` theo đúng mẫu `scripts/e2e-m11.sh` dòng 267.) Độ 48 byte lấy từ phép đo trên Mac (mục 3.6), chưa đo trên máy ảo. Bằng chứng gián tiếp đã có: phép quét T60 (`tpan.db` và `-wal`) ra `totp=0 ... tong=0` trong khi phép quét đối chứng thấy bí mật ở câu trả lời lần đầu (mục 2.6).

Dọn dẹp: `make vm-reset` đưa máy ảo về sạch. Lệnh 4 để lại hai file `cj.txt` trong thư mục hiện tại; xoá tay.

## 8. Câu hay bị hỏi

Đáp án ở cuối file. Câu 2, 9 và 10 cần chạy lệnh trên máy ảo mới trả lời chắc được.

1. Đồng hồ điện thoại lệch 45 giây thì có đăng nhập được không? Lệch 2 phút thì sao? Vì sao con số 60 giây?
2. Kẻ lấy được file `tpan.db` (hoặc bản sao lưu) thì có lấy được bí mật hai bước không? Làm sao tự kiểm?
3. Kẻ có mật khẩu admin mở vài trăm phiếu rồi bắn mã song song thì sao?
4. Vì sao phiếu chờ là bảng riêng, không thêm cột vào `sessions`?
5. Mất điện thoại và hết mã dự phòng thì làm gì? Có gỡ hai bước qua web được không?
6. Token chỉ đọc có lấy được mật khẩu WordPress qua `GET /jobs/{id}` không?
7. Hạ admin xuống user thì token `system:write` của họ còn làm được gì?
8. Vì sao `/users`, `/plans`, `/ai` chỉ dùng được bằng phiên, không dùng được bằng token?
9. Đặt tên miền cho panel trùng với tên một site thì sao? Vào panel bằng tên của site thì sao?
10. Gia hạn chứng chỉ panel có làm panel restart không? Làm sao tự kiểm?

## Đáp án

1. Lệch 45 giây thì có lúc được, có lúc không: bảng ở mục 2.1 cho 50% trên 3000 mốc. Lệch 30 giây trở xuống thì luôn được. Lệch 2 phút thì không bao giờ. Máy chủ chấp nhận bước hiện tại và một bước mỗi bên (`Skew = 1`), mỗi bước 30 giây. Lệch từ 60 giây thì luôn lệch ít nhất 2 bước, nằm ngoài cửa sổ. Con số 60 suy ra từ hai hằng đó, không phải chọn riêng. Cách chữa: bật đồng bộ giờ trên điện thoại. Phía máy chủ có phép kiểm `tpan.clock` (chạy `timedatectl show -p NTPSynchronized --value`, cảnh báo khi `no` và có user đã bật hai bước).

2. Không. Trong DB, `user_totp.secret_enc` là bản đã niêm phong bằng AES-256-GCM, còn khoá nằm ở `/etc/tpan/secret.key`, một file riêng 0600 root, ngoài `tpan.db` và ngoài bản sao lưu của nó. Phải lấy được cả hai. AAD gắn tên user, nên chép bản mã của user này sang dòng của user khác cũng không mở được. Tự kiểm trên máy ảo: lệnh 5 mục 7 cho thấy khoá ở file riêng (`600 root 32 regular file /etc/tpan/secret.key`, đã có trong `M20.log` dòng 1750); lệnh 6 cho thấy cột là 48 byte nhị phân (lệnh 6 chưa chạy). Phép quét T60 đến T66 của e2e (phần J) tìm bí mật TOTP của bốn user trong `tpan.db`, `-wal`, nhật ký, job, MCP, bản sao backup và ra 0 chỗ, sau khi phép quét đối chứng thấy 4 bí mật trong câu trả lời lần đầu. Mất file khoá thì mã dự phòng vẫn dùng được (chỉ cần sha256) và root dùng `tpan user 2fa reset`; panel không sinh khoá mới đè lên.

3. Không vượt được trần. Mọi lần kiểm mã của một user đi qua một ổ khoá trong tiến trình (`mfaLocks`) và ghi lần sai ngay trong ổ khoá đó, nên 10 lần sai là khoá, dù có bao nhiêu phiếu song song. Test `TestBuocHaiSongSongKhongVuotTran` dựng đúng cảnh này (400 phiếu, 2000 request) và đòi `Match` chạy không quá 10 lần. Sau khoá lần đầu, mã 6 số bị từ chối mà không chạy `Match`; khoá lần sau gấp đôi (15 phút, 30 phút, ... trần 24 giờ); mỗi lần khoá ghi một dòng `login.2fa_locked` và phép kiểm `tpan.2fa:attempts` cảnh báo "mật khẩu có thể đã lộ". Thiết kế đầu thì không chặn được cảnh này (S1, mục 5.4), và đó là điểm nặng nhất của bản soát.

4. Vì `authResolve` (chỗ duy nhất dựng danh tính cho guard) chỉ đọc bảng `sessions` và cookie `tpan`. Phiếu ở bảng `login_challenges` với cookie `tpan-2fa`, nên không có đường mã nào biến phiếu thành phiên. Nếu là một cột, mỗi chỗ đọc phiên phải nhớ kiểm cột đó; quên một chỗ là có lỗ vượt bước hai. Test B1: `TestPhieuChoKhongVaoRouteNao` đi qua mọi route của `routes()`. Phiếu còn gắn IP, tên máy, sống 5 phút, tối đa 5 lần nhập.

5. Dùng mã dự phòng (mỗi mã một lần, đăng nhập bằng mã dự phòng cho biết còn bao nhiêu). Hết mã thì người có root trên máy gõ `sudo tpan user 2fa reset <tên>` (gỡ hai bước, mã dự phòng, phiếu và đăng xuất mọi phiên; có nhật ký `user.2fa_reset`, ip `cli`). Qua web thì **không** gỡ được khi không có mã: đặc tả cấm, và `disable` đòi mật khẩu cộng một mã. Chính sách bắt buộc cũng có lối thoát bằng CLI: `tpan user 2fa policy off`.

6. Không, sau bản sửa S3. Trước đó có: `GET /jobs/{id}` lấy rồi xoá mật khẩu hiện một lần khi danh tính không "xem như", mà danh tính từ token thì không có `ViewedBy`. Nay request bằng token chỉ lấy được khi id token trùng id token đã khởi job (`starterToken`); token khác, nhất là token chỉ đọc, không thấy `secret`, `secrets`, `db_secret` và cũng không làm mất chúng. Request bằng phiên cũng không lấy mật khẩu của job do token khởi. Test: `TestTokenChiDocKhongLayMatKhauJob`, `TestJobGhiTokenKhoi`.

7. Không gì cả ở route admin. Danh tính của request bằng token được dựng từ dòng `users` đọc ở chính request đó (`account.Identity{Name, Role}`), không lưu vai lúc tạo. Quyền thật là phạm vi giao quyền hiện tại, nên `ruleAdmin` trả 403 dù token có `system:write`. Khoá user thì token trả 401 `token_invalid`, xoá user thì token mất cùng lúc (`ON DELETE CASCADE`, B7). Test `TestTokenTheoVaiHienTai`, `TestXoaUserXoaTokenVaHaiBuoc`.

8. `/users` đặt lại được mật khẩu và đổi vai người khác: một token admin lộ có thể tạo admin mới, biến token sống 7 ngày thành tài khoản sống mãi. `/plans` và `/ai`: Plan do AI đề xuất phải có người duyệt; token là thứ máy dùng, cho nó duyệt là agent tự duyệt Plan của mình. Mọi `/account/...` là mật khẩu, hai bước, token: đặc tả cấm. Cách bảo đảm: trường `tok` của route rỗng nghĩa là chỉ phiên, mặc định đóng. `TestNhomTokenCuaRoute` so bảng thật với bảng viết tay, nên route mới mà quên khai nhóm thì test đỏ chứ không âm thầm mở cho token.

9. Đặt tên panel trùng tên một site thì `panel_domain_is_site` (bước `panelNameFree` và điều kiện đầu), và tạo site trùng `panel.domain` thì `domain_is_panel`. Lý do không chỉ là lineage: cookie phiên không tách theo cổng, nên PHP của site cùng tên đọc được cookie phiên panel. Vào panel bằng tên của một site thì nhận 421 `host_is_site` với bất kỳ site nào và bất kỳ chủ nào, kèm cách vào bằng IP hoặc tên miền của panel. Tự kiểm trên máy ảo, với site `e.example.com` (e2e đã chạy cùng cách với `a.example.com`, `M20.log` dòng 2901 đến 2905: `https://a.example.com:8443/api/v1/session -> HTTP 421 ... error.code=host_is_site`; lệnh dưới đây là lệnh gọn hơn của mình, chưa chạy): `curl -sk -o /dev/null -w '%{http_code}\n' --resolve e.example.com:8443:127.0.0.1 https://e.example.com:8443/` phải ra `421`. Cùng phần H: đặt tên panel trùng site thì `409 panel_domain_is_site`, tạo site tên panel thì `domain_is_panel`; Host của một site mà có `Authorization` thì 200 (miễn 421). Test trên Mac: `TestHostLaSite421`.

10. Không. `GetCertificate` đọc `mtime` và cỡ của hai file chứng chỉ mỗi lần bắt tay và nạp lại khi chúng đổi, nên certbot gia hạn xong là bắt tay kế tiếp có chứng chỉ mới, và `tpan.service` không phải khởi động lại (không cắt job đang chạy). Tự kiểm trên máy ảo (đây là khẳng định T50 của e2e, đã chạy, `M20.log` dòng 2843 đến 2870): `systemctl show -p MainPID --value tpan`; `openssl s_client -connect 127.0.0.1:8443 -servername <tên panel> </dev/null 2>/dev/null | openssl x509 -noout -serial`; `sudo certbot renew --cert-name tpan-panel --force-renewal`; chạy lại hai lệnh đầu. Sẽ thấy serial đổi và `MainPID` giữ nguyên. Output thật của e2e: `MainPID` 1680 trước và sau; serial phục vụ đổi từ `0BEE040B1F06CAD4` sang `0C2365875FC4564D`, bằng serial của lineage `tpan-panel` trên đĩa; log kết luận `T50: MainPID của tpan.service không đổi (1680)`. Chạy lại lệnh của bạn sẽ ra PID và serial khác (máy dựng lại), nhưng quan hệ giống nhau. Phần certbot đổi serial cũng đã thấy ở `thu-vm.log` (mục 2.4); test `TestChungChiTheoSNI` chạy trên Mac. Lưu ý: e2e dùng pebble và `--force-renewal`, chưa phải Let's Encrypt thật và chưa phải timer tự gia hạn.
