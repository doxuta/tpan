# M16: giao diện bản 2 và trang Công cụ

Đọc kèm: `03-m3-giao-dien-va-api.md` (giao diện được nhúng vào file chạy `tpan`, cookie phiên, `make dev-api`), `13-m13-nhieu-nguoi-dung.md` (vai admin và user, luật route, phiên "xem như"), `15-m15-nut-cap-nhat.md` (khối cập nhật, kịch bản bấm thật, dòng phiên bản), `docs/GIAO-DIEN.md` (quy ước bản 2), `docs/thay-doi/M16-thiet-ke.md` (bản thiết kế viết trước khi code) và `docs/thay-doi/M16.md` (26 quyết định tự đưa ra, bảng số đo).

Mốc này sinh ra từ một nhận xét của chủ dự án sau khi dùng bản thật: giao diện khó nhìn. Bản thiết kế liệt kê bảy nguyên nhân đã đo được: chữ mono 11 đến 13 px, trang dài một cột (màn Bảo mật 6,1 màn hình), lặp chữ, nút nặng, khung nặng hơn nội dung (mỗi màn 16 đến 58 ô viền màu chữ), tên site 36 ký tự bị xuống 2 đến 3 dòng, và vài con số lệch nhau. M16 không thêm tính năng. Nó vẽ lại mọi màn bằng một bộ thành phần mới, thêm trang Công cụ, và thêm đúng một trường vào API.

Người review sẽ hỏi quanh tám câu dưới đây. Bảng này là mục lục của file:

| Câu người review hỏi | Trả lời một dòng | Đọc ở |
|---|---|---|
| Mốc này đổi hành vi hay API nào không? | Không đổi thao tác nào. API chỉ thêm `db_count` ở `/api/v1/overview`. | 1, 3.6 |
| Số ở mục "Cơ sở dữ liệu" của menu đếm gì? | DB của site đang chạy cộng DB rời đang chạy, trong phạm vi người xem. Site và DB trong thùng rác không tính. | 3.6, 5 |
| User thường mở `/` thì thấy gì, và có phải đó là bảo mật không? | Họ được đưa sang `/tools`. Đó chỉ là chuyện hiển thị, quyền thật do route ở server giữ. | 3.4, 4.6 |
| Làm sao biết giao diện "dễ nhìn hơn" bằng số, không bằng cảm giác? | Bảng đo trước và sau trên cùng dữ liệu, kèm ngưỡng cứng làm lệnh `make` thoát mã khác 0 khi vi phạm. | 2.3 |
| Màu mới có đủ tương phản không, ai kiểm? | Test Go đọc thẳng `styles.css` và tính tỷ lệ WCAG cho từng cặp màu ở cả sáng và tối. | 3.8, 4.2 |
| Vì sao menu thả luôn `modal={false}`? | Bản modal của Radix chèn thẻ `<style>`, mà CSP của panel là `default-src 'self'` nên trình duyệt chặn và báo lỗi. | 3.7, 4.4 |
| Làm sao người sau không viết lại kiểu bản 1? | `TestKieuDang` quét toàn bộ `web/src` và đỏ khi gặp cỡ chữ, màu viết thẳng hay class cấm. | 3.9, 4.8 |
| Nâng từ v0.2.0 lên có bị đăng xuất không? | Không: e2e phần E giữ nguyên cookie qua lần nâng, tải lại trang là thấy giao diện mới. | 2.2 |

Hai từ dùng suốt file. **Bản 1** là giao diện của v0.2.0 (kiểu "Tuyến" ban đầu), **bản 2** là giao diện của mốc này. Trong log e2e, **bản A** là bản của nhánh `moc/M16` (tên `v0.2.0-61-g365ea2c`), **bản B** là cùng mã với A nhưng khác tên bản, dùng cho phần F.

## 1. Mốc này làm được gì

- Mọi màn có cùng khung: thanh đầu trang dính ở trên, menu trái có biểu tượng và số đếm, nội dung là các thẻ. Chữ nền là sans 14 px; chữ mono chỉ còn cho thứ người ta chép hoặc so từng ký tự (đường dẫn, lệnh, mã job). Không chữ nào dưới 12 px.
- Màn Bảng điều khiển ngắn lại từ 3,0 xuống 1,8 màn hình, trang một site từ 4,3 xuống 1,0 (chia sáu tab), màn Bảo mật từ 6,1 xuống 1,9. Mỗi hàng của bảng có tối đa hai nút chữ, các việc còn lại nằm trong menu ba chấm.
- Trang Công cụ (`/tools`) liệt kê mọi việc làm được với tài khoản đang đăng nhập, xếp theo nhóm. Đây là trang chủ của user thường. Admin vẫn vào Bảng điều khiển ở `/`.
- Ô tìm `⌘K` tìm được cả site ("Site: <tên miền>"). Số ở mục "Cơ sở dữ liệu" của menu đếm đúng số DB đang chạy.
- Sáng và tối, tiếng Việt và tiếng Anh, bàn phím và CSP giữ nguyên như bản 1. Bản nâng từ v0.2.0 giữ cookie đăng nhập.

## 2. Chạy thử

Nguồn của mọi output dưới đây:

- `docs/bang-chung/M16.log`: e2e bước 19, `make e2e-m16`, chạy trên máy ảo `tpan-m16` ngày 21/11/2026 từ 11:48:59 tới 11:55:25, trên commit `365ea2c`. Dòng cuối: `TỔNG KẾT: 118 khẳng định đạt, 0 khẳng định không đạt`.
- `docs/bang-chung/M16-anh.log` (bộ ảnh, commit `2f1992c`, 11:40) và `docs/bang-chung/M16-do.log` (số đo có ngưỡng, 11:45).
- `docs/thay-doi/M16.md` mục 2 ("Soát cuối") và mục 3: bảng số đo trước và sau, kết quả bộ ảnh.
- `docs/bang-chung/M16/`: 94 ảnh chụp (sáng, tối, 390 px, các lớp nổi). Mình chưa mở từng ảnh để nhận xét về hình thức; nhận xét về ảnh trong file này lấy từ `M16.md` và `M16-anh.log`.
- Lệnh kiểm mình vừa chạy trên Mac trong phiên viết file này (mục 2.5).

Những điều cần biết khi đọc:

- Mình là thợ viết, không được dùng máy ảo và không được mở trình duyệt. Trừ mục 2.5, không lệnh nào dưới đây do mình vừa chạy. Chỗ nào chưa có output thì mình ghi "chưa chạy" kèm lệnh để bạn tự kiểm.
- Máy ảo trong log tên `tpan-m16`, tên đọc từ file `.tpan-vm` của worktree. Ở repo chính không có file đó thì `scripts/vm.sh` dùng `tpan-test`. `scripts/vm.sh exec X` chạy X trong máy ảo, `scripts/vm.sh root X` chạy bằng quyền root.
- `<panel>` là địa chỉ panel của máy ảo, `<vm>` là IP máy ảo, `<mac>` là IP của Mac. Log đã che chúng. `<bí mật, đã che>` là mật khẩu.
- Lần chạy trước (10:57, commit `4767288`) cũng 119 đạt nhưng tên bản có đuôi `-dirty`; `make e2e-m16` giờ ghi log ra `tmp-phien/` (ngoài git) rồi mới chép vào `docs/bang-chung/M16.log` để chuyện đó không lặp lại.
- Điểm khác các e2e trước: phần lớn khẳng định do trình duyệt làm. Một Chrome không giao diện chạy trên Mac mở panel thật của máy ảo, đăng nhập qua form, mở từng màn, bấm menu. Mỗi lần mở một màn, kịch bản kiểm: có tiêu đề `h1`, 0 lỗi console (lỗi JS, request hỏng như 403, vi phạm CSP), cả trang không cuộn ngang (`scripts/e2e-m16.sh` đầu file, `scripts/anh-readme/m16_that.go`).

### 2.1 Tự xem bằng máy demo trên Mac

Máy demo là `tpan serve --demo`: panel thật chạy trên một máy giả, dữ liệu mẫu `*.example.*`. Lệnh đọc từ `Makefile` (`dev-api`, `dev-web`) và `web/vite.config.ts` (Vite chuyển mọi request `/api` sang cổng 18080). Chạy trên Mac, trong thư mục repo, hai terminal:

```bash
make dev-api     # terminal 1: go run ./cmd/tpan serve --demo --listen 127.0.0.1:18080
make dev-web     # terminal 2: giao diện ở http://localhost:5173, tự nạp lại khi sửa mã
```

Terminal 1 in mật khẩu admin và mật khẩu của hai user mẫu `khach_a`, `khach_b` (mỗi lần chạy đặt mật khẩu mới: `internal/demo/m13.go`, chú thích đầu file). Chưa chạy: mình không mở trình duyệt, nên chưa nhìn bằng mắt. Những thứ nên mở:

1. Đăng nhập `admin`, mở `/tools`, `/users`, `/databases`. So với ảnh trong `docs/bang-chung/M16/`: `cong-cu-sang.png`, `nguoi-dung-sang.png`, `co-so-du-lieu-sang.png`.
2. Đổi sáng tối: bấm tên tài khoản ở góc phải trên, chọn nhóm "Giao diện" (Sáng, Tối, Theo hệ điều hành). Log e2e dòng 543 liệt kê đúng các mục của menu này: `Tiếng Việt | English | Sáng | Tối | Theo hệ điều hành | Gợi ý | Đăng xuất`.
3. Đăng xuất, đăng nhập `khach_a`: bạn sẽ rơi vào `/tools`, menu không có Stack, Bảo mật, Người dùng. Gõ tay `/users` thì thấy trang "Không có quyền vào màn này".
4. Mở DevTools của Chrome, bật chế độ thiết bị (Ctrl+Shift+M trên Windows, Cmd+Shift+M trên Mac), đặt chiều rộng 390: menu trái thành nút "Menu" ở thanh đầu trang.
5. Bấm `⌘K` (hoặc `Ctrl+K`), gõ vài chữ của tên một site: có dòng "Site: <tên miền>".

### 2.2 e2e bước 19 trên máy ảo

Chạy trên Mac, trong thư mục repo. Lệnh đầu của kịch bản là `make vm-reset`, đưa máy ảo về snapshot sạch, mất mọi thứ đang có trong đó. Chỉ chạy với máy ảo test.

```bash
make e2e-m16     # chạy scripts/e2e-m16.sh, ghi log ra tmp-phien/ rồi chép vào docs/bang-chung/M16.log
```

Lần chạy trong log mất khoảng sáu phút rưỡi. Kịch bản gồm bảy phần, mỗi phần in một dòng tổng. Mình ghi số dòng của `M16.log`:

| Phần | Việc | Kết quả | Dòng |
|---|---|---|---|
| 0 | đóng gói bản A và bản B, dựng thư mục đồ nghề (gói v0.2.0 đã có sẵn trên Mac, không tải lại) | 5 đạt | 265 |
| A | máy sạch, cài bản A bằng `install.sh --web nginx` của chính gói | 13 đạt | 510 |
| B | admin đăng nhập qua form, mở mọi màn admin | 31 đạt | 549 |
| C | tạo site tên 36 ký tự từ form Tạo site | 10 đạt | 577 |
| D | user thường: đăng nhập, mở các màn của mình | 25 đạt | 714 |
| E | máy sạch, cài v0.2.0, đăng nhập giao diện cũ, tự nâng lên bản A | 23 đạt | 1137 |
| F | hồi quy M15: bấm thật nút cập nhật rồi quay về | 3 đạt | 1169 |
| cuối | health, site, tắt đồ nghề, rồi dò xem log có lộ bí mật không | 3 đạt cộng 5 phép dò lộ | 1257 tới 1269 |

Cộng lại: 5 + 13 + 31 + 10 + 25 + 23 + 3 + 3 + 5 = 118. Lần chạy 10:57 có 6 ở phần 0 vì còn tải gói v0.2.0, nên cộng ra 119.

**Phần B: admin mở mọi màn** (`M16.log` dòng 513 tới 549; mình cắt các dòng giữa, mỗi dòng giữa có đúng khuôn như hai dòng đầu):

```
[2026-11-21 11:51:05] $ anh-readme -bo m16-that -buoc admin   (Chrome không giao diện trên Mac, panel <panel>)
  => ĐẠT: admin đăng nhập qua form: tới / và thấy Bảng điều khiển
  => ĐẠT: mở /: tiêu đề «Bảng điều khiển», 0 lỗi console, cuộn ngang: false
  => ĐẠT: mở /tools: tiêu đề «Công cụ», 0 lỗi console, cuộn ngang: false
  ...
  => ĐẠT: mở /sites/a.example.com?tab=cai-dat: tiêu đề «a.example.com», 0 lỗi console, cuộn ngang: false
  ...
  => ĐẠT: menu tài khoản: mở được, Escape đóng được, 0 lỗi console; các mục: Tiếng Việt | English | Sáng | Tối | Theo hệ điều hành | Gợi ý | Đăng xuất
  => ĐẠT: menu ba chấm của hàng đầu ở màn Cơ sở dữ liệu: mở được, Escape đóng được, 0 lỗi console; các mục: Nhập từ bản đã xuất | Đổi charset | Tạm khoá
  => ĐẠT: số ở mục Cơ sở dữ liệu của menu (1) bằng số DB đang chạy của /api/v1/databases (1)
  => ĐẠT: dòng phiên bản cuối menu ghi v0.2.0-61-g365ea2c, không còn chân trang
[2026-11-21 11:52:00] (mã thoát 0)

==== Phần B: 31 đạt, 0 không đạt
```

Đọc ý:

- Mỗi dòng "mở ..." là một lần trình duyệt thật mở đường dẫn đó và kiểm ba thứ. `?tab=cai-dat` là một trong sáu tab của trang một site; mục 3.7 nói tab nằm ở địa chỉ thế nào.
- "0 lỗi console" gồm cả vi phạm CSP. Nếu menu thả dùng bản modal của Radix thì dòng "menu tài khoản: mở được ..." sẽ báo lỗi (mục 4.4).
- Dòng số DB là đối chiếu giữa thứ người dùng thấy (số trên menu) và thứ API trả. Máy ảo ở thời điểm đó chỉ có site `a.example.com` nên cả hai là 1.

**Phần C: tạo site từ giao diện** (dòng 552 tới 579, rút gọn):

```
  => ĐẠT: form Tạo site (hoc-truc-tuyen.lop-hoc-a.example.net): bản xem trước hiện ra, nút chạy ghi «Chạy 17 bước»
  => ĐẠT: job tạo site dad4fe4a594aeeb2 kết thúc: succeeded
  ...
  => ĐẠT: tên hoc-truc-tuyen.lop-hoc-a.example.net (36 ký tự) trên bản đồ tuyến: một dòng
[2026-11-21 11:52:15] $ scripts/vm.sh exec curl -s -o /dev/null -m 20 -w %{http_code}\n --resolve hoc-truc-tuyen.lop-hoc-a.example.net:80:127.0.0.1 http://hoc-truc-tuyen.lop-hoc-a.example.net/
200
```

Tên 36 ký tự là con số của đặc tả: ở bản 1 tên như vậy xuống 2 đến 3 dòng trên bản đồ tuyến. Dòng cuối là bằng chứng việc tạo từ giao diện chạy tới tận cùng (site trả 200 trong máy ảo), không chỉ "form hiện đúng".

**Phần D: user thường** (dòng 580 tới 716; mình bỏ output của `tpan user add` và `tpan site add`, đã có ở các mốc trước):

```
  => ĐẠT: user thường khach_m16 đăng nhập qua form: tới /tools (trang chủ là Công cụ)
  => ĐẠT: menu và trang Công cụ của user thường không có mục admin (lọt: [])
  => ĐẠT: mở /tools: tiêu đề «Công cụ», 0 lỗi console, cuộn ngang: false
  => ĐẠT: mở /dashboard: tiêu đề «Bảng điều khiển», 0 lỗi console, cuộn ngang: false
  ...
  => ĐẠT: user thường gõ /users: thấy «Không có quyền vào màn này», 0 lỗi console (không gọi API admin)
  => ĐẠT: user thường mở / thì được đưa tới /tools (đang ở /tools)
```

Hai điều cần thấy ở đây. Một: user thường mở mọi màn của mình mà không có request nào bị 403 (kịch bản tính 403 là lỗi console). Hai: gõ `/users` thì giao diện không gọi API admin, nên không có 403 nào để đếm; trang "không có quyền" tự vẽ ở phía giao diện (mục 3.1).

**Phần E: nâng từ v0.2.0** (dòng 717 tới 1139; mình cắt output cài đặt):

```
  => ĐẠT: tpan version là v0.2.0
  => ĐẠT: trình duyệt đã đăng nhập bằng giao diện v0.2.0 và đang chờ
  => ĐẠT: tpan self-update của v0.2.0 lên bản A: thoát 0, job thành công
  => ĐẠT: in v0.2.0 -> v0.2.0-61-g365ea2c
  => ĐẠT: giao diện bản cũ chưa có mục Công cụ
  => ĐẠT: sau khi nâng: cùng cookie, /api/v1/session vẫn logged_in, bản v0.2.0 -> v0.2.0-61-g365ea2c
  => ĐẠT: tải lại trang: thấy giao diện bản 2 (menu có Công cụ) bằng phiên cũ, 0 lỗi console
  => ĐẠT: đăng xuất rồi đăng nhập lại bằng mật khẩu cũ trên bản mới
```

Máy được dựng lại sạch, cài đúng gói v0.2.0 đã phát hành, rồi nâng bằng `tpan self-update` của chính v0.2.0 (mốc M12 và M15 đã giải thích lệnh này). "Cùng cookie" nghĩa là không đăng nhập lại giữa chừng: M16 không đổi cookie, bảng người dùng hay mật khẩu. Đây là điều bản thiết kế (mục 6) cam kết, và dòng này là bằng chứng.

**Phần F: hồi quy M15 trên giao diện mới** (dòng 1140 tới 1171):

```
bản đang chạy lúc đầu: v0.2.0-61-g365ea2c
từ lúc bấm tới lúc khối báo "Đã lên": 3.1 giây
bản đang chạy sau khi cập nhật: v0.2.0-61-g365ea2c-e2e-b (phiên đăng nhập cũ còn dùng được: /api/v1/session trả lời có version)
dòng phiên bản sau khi lên: "TPAN v0.2.0-61-g365ea2c-e2e-b"
từ lúc bấm tới lúc khối báo "Đã quay về": 3.2 giây
bản đang chạy sau khi quay về: v0.2.0-61-g365ea2c
  => ĐẠT: kịch bản bấm thật của M15 chạy hết, thoát 0
```

Kịch bản này là thứ duy nhất của các mốc trước chạy qua mã giao diện, nên là phép hồi quy đáng giá nhất: nó tìm nút bằng chữ ("Kiểm bản mới", "Cập nhật lên", "Quay về bản trước") và đọc dòng phiên bản ở cuối menu. M16 phải giữ các chữ đó (bảng "móc cho kiểm thử" ở mục 3.11 của bản thiết kế) và sửa hai nhãn mà bản 1 viết hoa ("SHA256 SẼ KIỂM", "LƯỢT GẦN NHẤT") thành chữ thường trong cả script lẫn giao diện.

**Phần cuối: log có lộ bí mật không** (dòng 1260 tới 1269):

```
Số bí mật đã gặp: 10; số dòng của log chứa một trong số đó: 0
  => ĐẠT: log không chứa mật khẩu nào
  => ĐẠT: log không chứa IP máy ảo
  => ĐẠT: log không chứa mạng Multipass
  => ĐẠT: log không chứa IP của Mac
  => ĐẠT: log không chứa thư mục nhà hay tên user Mac
```

### 2.3 Số đo trước và sau

Lệnh (chạy trên Mac, máy demo, không cần máy ảo):

```bash
make do-giao-dien NGUONG=1     # đo từng màn ở 1440 x 900, sáng, tiếng Việt; vi phạm ngưỡng cứng thì thoát mã khác 0
```

Lệnh này mình không tự chạy (nó mở Chrome). Phiên chính chạy lại lúc 11:45 ngày 21/11 trên bản cuối, log `docs/bang-chung/M16-do.log`, cùng các con số dưới đây; dòng cuối của log là `NGƯỠNG CỨNG: đạt hết.` Bảng lấy từ `M16.md` mục 3 và `M16-do.log`, so với bản 1 đo ở đợt 0 trên cùng dữ liệu demo có ba site tên dài (`docs/bang-chung/M16-nghien-cuu/t1-do-ban-1-demo.ket-qua.txt`). Mỗi ô ghi "trước / sau".

| Màn | Trang dài (màn hình) | Chữ mono | Ô viền màu chữ | Cỡ chữ nhỏ nhất |
|---|---|---|---|---|
| Bảng điều khiển | 3,0 / 1,8 | 26% / 4% | 43 / 0 | 11 / 12 px |
| Công cụ (màn mới) | không có / 1,6 | không có / 2% | không có / 0 | không có / 12 px |
| Site (danh sách) | 1,1 / 1,0 | 48% / 5% | 27 / 0 | 11 / 12 px |
| Trang một site, tab Tổng quan | 4,3 / 1,0 | 68% / 2% | 26 / 0 | 11 / 12 px |
| Cơ sở dữ liệu | 2,2 / 1,2 | 33% / 12% | 55 / 0 | 11 / 12 px |
| Bảo mật | 6,1 / 1,9 | 40% / 1% | 28 / 0 | 11 / 12 px |
| Cài đặt | 2,9 / 2,9 | 33% / 17% | 29 / 0 | 11 / 12 px |
| Người dùng | 1,1 / 1,0 | 22% / 2% | 27 / 0 | 11 / 12 px |

Cách đọc bảng: "ô viền màu chữ" là số phần tử có viền cùng màu chữ chính (nguyên nhân số 5 "khung nặng hơn nội dung"); mọi cột do `scripts/anh-readme/do-giao-dien.js` đo trên trình duyệt. Màn Cài đặt dài bằng bản 1 (2,9 màn hình); bản thiết kế không đặt ngưỡng dài cho màn này.

Cột "chữ từ 13 px trở xuống" (không có trong bảng trên) tăng ở Bảo mật (46% lên 70%) và Người dùng (25% lên 37%). `M16.md` giải thích: bản 2 dùng chữ phụ 13 px và dòng phụ 12,5 px, còn nhãn mono 11 px chữ hoa của bản 1 thành 12 px viết thường. Ngưỡng của mốc là không chữ nào dưới 12 px, không phải giảm tỷ lệ chữ nhỏ.

Ngưỡng cứng của bản thiết kế (mục 7) và kết quả theo `M16.md`: **đạt hết**. Trang một site đo trên site 44 ký tự là 1,0 màn hình, mono 2% (ngưỡng 1,5 và 30%); trang một site đang hỏng (`wiki.example.org`) 1,1 màn hình, mono 2%; đáy hàng thứ năm của bảng Cơ sở dữ liệu nằm trong 900 px; Bảo mật lúc mới mở 1,9 màn hình (ngưỡng 2); không màn nào cuộn ngang.

Hai lần đo trước đó không đạt và đã sửa. Mục 5 kể lại.

### 2.4 Bộ ảnh, bàn phím, tiếng Anh

Lệnh (Mac, máy demo, cần Chrome cài sẵn):

```bash
make anh-m16     # chụp mọi màn vào docs/bang-chung/M16, đồng thời kiểm console, cuộn ngang, cỡ chữ, bàn phím, tiếng Anh
```

Kết quả lần chạy cuối (11:40, commit `2f1992c`, log `docs/bang-chung/M16-anh.log`; dòng cuối: `Đã chụp 94 ảnh vào docs/bang-chung/M16. Không đạt: 0`): 94 ảnh. Mọi màn của `routes.tsx` ở 1440 x 900 sáng và tối (cả sáu tab của trang một site, site đang hỏng, Công cụ, trang "không có quyền", Bảng điều khiển và Cơ sở dữ liệu của user thường, trang 404, Đăng nhập), sáu màn chính thêm ở 390 x 844, và sáu lớp nổi (menu tài khoản, menu ba chấm, thanh lệnh, ngăn AI, bản xem trước Plan của Tạo site, thẻ theo dõi job). Trên mọi ảnh: 0 lỗi console, không cuộn ngang cả trang, không chữ dưới 12 px, không ô viền màu chữ. Bàn phím: đi hết bằng Tab trên 33 màn. Tiếng Anh: 33 màn ở 1440 và 390 px, 66 lượt đạt. Lần chạy cuối có thêm một phép kiểm của soát cuối (`M16-anh.log` dòng 141, 142):

```
  bàn phím menu ba chấm: Enter mở, tới «Nhập từ bản đã xuất», mũi tên xuống tới «Đổi charset», Escape đóng, focus về nút, 0 lỗi console
  bàn phím hộp hỏi lại: mở «Tạm khoá» từ menu ba chấm, Escape đóng, focus về nút ba chấm
```

Dòng thứ hai là phép kiểm cho lỗi V6 ở mục 5.2: trước khi sửa, đóng hộp hỏi lại làm focus rơi về `<body>`.

Lần chạy đầu (10:58) báo 7 điều không đạt, cùng một chỗ. Mình đọc được kết thúc của lần chạy đó trong `tmp-phien/anh-m16.log` (file này nằm ngoài git, có thể đã bị ghi đè):

```
Đã chụp 94 ảnh vào docs/bang-chung/M16. Không đạt: 7
  - site-tong-quan: Tab không tới 1 trên 11 thứ bấm được: div «Các phần của site»
  - site-hong: Tab không tới 1 trên 13 thứ bấm được: div «Các phần của site»
  ...
```

Đó là lỗi của phép kiểm, không phải của giao diện: khung `role="tablist"` của Radix có `tabindex="0"` chỉ để chuyển focus ngay sang tab đang mở, nên không bao giờ tự giữ focus. Commit `9f6df5a` sửa `tabWalk` trong `scripts/anh-readme/m16_phim.go` để bỏ khung đó. Sau khi sửa, bộ ảnh được chạy lại ở lần cuối nói trên: 0 không đạt. Tự kiểm: `make anh-m16`, rồi đọc dòng `Không đạt:` ở cuối (mình chưa tự chạy lệnh này).

### 2.5 Các lệnh kiểm mình vừa chạy trên Mac

Chạy trong phiên viết file này, trên Mac, từ thư mục repo (`make -C` và `go -C` là cách chạy mà không cần `cd`). Trong output, mình thay phần đầu đường dẫn gói Go (tên module, có chứa tên tài khoản GitHub) bằng `<module>`; phần còn lại giữ nguyên.

Test logic thuần của giao diện (`node --test`). Output đã cắt phần đầu, còn dòng tổng:

```
$ make test-web
...
1..16
# tests 16
# suites 0
# pass 16
# fail 0
# cancelled 0
# skipped 0
# todo 0
# duration_ms 95.929333
```

Test Go của gói `web` (tương phản, màu pha, kiểu dáng, nhãn):

```
$ go test ./web/ -run 'TestTuongPhan|TestAccentTint|TestHaiCheDoCungBien|TestKieuDang|TestNhanKhongVietHoa' -count=1 -v
=== RUN   TestKieuDang
--- PASS: TestKieuDang (0.15s)
=== RUN   TestNhanKhongVietHoa
--- PASS: TestNhanKhongVietHoa (0.00s)
=== RUN   TestHaiCheDoCungBien
--- PASS: TestHaiCheDoCungBien (0.00s)
=== RUN   TestTuongPhan
--- PASS: TestTuongPhan (0.00s)
=== RUN   TestAccentTint
--- PASS: TestAccentTint (0.00s)
PASS
ok  	<module>/web	0.463s
```

Test của `db_count` (bất biến B1, B2 và hàm đếm):

```
$ go test ./internal/server/ -run 'TestOverviewDBCount|TestActiveDBCount' -count=1 -v
=== RUN   TestOverviewDBCountMatchesDatabases
--- PASS: TestOverviewDBCountMatchesDatabases (0.01s)
=== RUN   TestOverviewDBCountScoped
--- PASS: TestOverviewDBCountScoped (0.01s)
=== RUN   TestActiveDBCount
--- PASS: TestActiveDBCount (0.00s)
PASS
ok  	<module>/internal/server	0.454s
```

Luật kiểu dáng cho riêng một thư mục (lệnh của thợ đổi màn):

```
$ make kieu-dang DIR=src/screens/cong-cu/
KIEU_DANG_THU="src/screens/cong-cu/" go test ./web/ -run 'TestKieuDang|TestNhanKhongVietHoa' -count=1
ok  	<module>/web	0.085s
```

Và `make check` đầy đủ (chạy sau khi phiên chính sửa các phát hiện của soát cuối, thoát mã 0). Output dài hơn 170 dòng, mình chỉ giữ các dòng mỗi bước báo kết quả; các dòng `...` là phần cắt (68 gói Go báo `ok`, không dòng `FAIL` nào):

```
$ make check
go vet ./...
golangci-lint run
0 issues.
go test ./...
ok  	<module>/cmd/tpan	12.817s
...
# pass 16
# fail 0
...
cd web && pnpm typecheck
$ tsc --noEmit
bash scripts/install_test.sh
...
29 đạt, 0 hỏng
```

Bốn bước của `make check` theo `Makefile`: `lint` (gồm `go vet` và `golangci-lint`), `test` (Go), `test-web` (`node --test`), rồi `pnpm typecheck` và `scripts/install_test.sh`. `tsc --noEmit` không in gì nghĩa là không có lỗi kiểu.

### 2.6 Những điều chưa chạy

| Việc | Lệnh để kiểm |
|---|---|
| Nhìn bằng mắt các màn trên máy demo | mục 2.1 |
| Tự chạy `make do-giao-dien NGUONG=1` và `make anh-m16` (có log của phiên chính, mình không mở Chrome) | trên Mac |
| Giao diện trên điện thoại thật hoặc trình duyệt khác Chrome | chưa có kịch bản nào; e2e và bộ ảnh chỉ dùng Chrome |
| Trình đọc màn hình (VoiceOver, NVDA) | chưa có kịch bản; phép kiểm bàn phím chỉ đi Tab và kiểm focus nhìn thấy |

## 3. Đi qua mã theo thứ tự chạy

Thứ tự theo đường đi của một lần mở trang: trình duyệt nhận `index.html`, nạp `main.tsx`, router chọn màn, khung trang vẽ thanh đầu và menu, màn gọi API và vẽ, rồi các lớp bảo vệ nằm ngoài đường chạy (test). Mình giải thích React ở chỗ nó xuất hiện lần đầu, vì bạn chưa quen.

Ba từ React dùng suốt mục này. **Component** là một hàm trả về phần giao diện (viết bằng cú pháp giống HTML gọi là JSX); tên viết hoa chữ đầu như `Layout`, `Home`. **Hook** là hàm bắt đầu bằng `use`, dùng trong component để lấy dữ liệu hay giữ trạng thái: `useState`, `useLocation`, và các hook tự viết như `useOverview`. **TanStack Query** là thư viện giữ bộ nhớ đệm cho dữ liệu lấy từ API: nhiều component gọi `useOverview` cùng lúc thì chỉ có một request, và dữ liệu được hỏi lại định kỳ.

### 3.1 `web/src/main.tsx`: dựng router từ danh sách màn

Trình duyệt mở `https://<panel>/tools`. Server Go trả `index.html` và các file JS đã nhúng sẵn vào file chạy (mốc M3). `main.tsx` là điểm vào: nạp CSS, dựng router, bọc các provider (ngôn ngữ, Gợi ý, TanStack Query). Router đọc từ danh sách `screens` của `routes.tsx` (chặng 3.2) và chuyển mỗi dòng thành một route:

```tsx
function route(s: Loaded): RouteObject {
  const load = s.admin
    ? async () => {
        const Screen = await s.load();
        return function AdminScreen() {
          return (
            <AdminOnly>
              <Screen />
            </AdminOnly>
          );
        };
      }
    : s.load;
  return { path: s.path, lazy: { Component: load }, HydrateFallback: Blank };
}
```

(Mình bỏ các dòng chú thích.) `lazy` nghĩa là file JS của màn chỉ được tải khi người dùng vào màn đó; trong log build ở đầu `M16.log` mỗi màn là một file `dist/assets/...js` riêng. Màn có cờ `admin` thì bị bọc thêm `AdminOnly`: user thường gõ thẳng đường dẫn sẽ thấy trang "không có quyền" thay vì một màn toàn lỗi 403. Chú thích trong mã nói thẳng đây chỉ là phần giao diện, "server vẫn là nơi chặn thật". Mục 4.6 nói lại điều này.

Toàn bộ cây route có hai nhánh: các màn `bare` (trang Đăng nhập, không khung) và nhánh còn lại nằm dưới `Layout`, kèm route `*` cho trang 404 (`NotFound`).

### 3.2 `web/src/routes.tsx`: một bảng duy nhất cho mọi màn

Mỗi dòng của bảng là một màn: đường dẫn, hàm tải component, và mục menu nếu có. Menu trái, trang Công cụ và thanh lệnh đều dựng từ bảng này, nên thêm một màn là thêm một dòng ở đúng một chỗ.

```tsx
const table = [
  {
    path: "/",
    load: () => import("./screens/trang-chu").then((m) => m.Home),
    menu: { label: "dashboard", group: "chinh", icon: "home", userPath: "/dashboard" },
  },
  // ... mỗi màn một dòng
] as const satisfies readonly ScreenEntry[];

/** Mọi đường dẫn có trong bảng trên. */
export type ScreenPath = (typeof table)[number]["path"];

export function menuPath(entry: ScreenEntry, admin: boolean): string {
  return admin ? entry.path : (entry.menu?.userPath ?? entry.path);
}
```

Ba chỗ cần hiểu:

- `as const` giữ nguyên từng chuỗi đường dẫn ("/tools" chứ không phải `string` chung chung), nhờ đó `ScreenPath` là hợp của đúng các đường dẫn có thật. `satisfies ScreenEntry` vẫn bắt lỗi nếu một dòng sai khuôn. Mục 4.7 giải thích.
- `userPath` là đường dẫn của mục menu khi người xem là user thường. Mục "Bảng điều khiển" có `path: "/"` nhưng `userPath: "/dashboard"`, vì với user thường `/` được dành làm trang chủ (chặng 3.4).
- `menuPath` là hàm duy nhất quyết định mục menu dẫn tới đâu. Menu trái, trang Công cụ và thanh lệnh đều gọi nó. Bản thiết kế ghi đây là một phát hiện của lần soát (S6): nếu thanh lệnh đi đường riêng thì user thường mở "Bảng điều khiển" từ thanh lệnh sẽ dẫn sai.

Số đếm của mục menu cũng khai ở đây. Mục Cơ sở dữ liệu: `count: (o) => o.db_count`, và chú thích ngay trên đó nói server cũ không gửi `db_count` thì menu không hiện số.

### 3.3 `web/src/Layout.tsx`: khung trang, menu, thanh đầu

`Layout` chạy với mọi màn (trừ Đăng nhập). Nó làm ba việc theo thứ tự:

```tsx
const session = useSession();
const loggedIn = session.data?.logged_in === true;
const overview = useOverview(loggedIn);
const who = useWho();
...
if (!loggedIn) return <Navigate to={loginHref(location.pathname + location.search)} replace />;
```

1. `useSession` hỏi `GET /api/v1/session`: đã đăng nhập chưa, người nào, vai gì, tên máy, tên bản. Chưa đăng nhập thì `Navigate` đưa sang trang Đăng nhập, nhớ đường dẫn đang mở trong `?next=`.
2. `useOverview` hỏi `GET /api/v1/overview` (site, sự cố, `db_count`). Trong `web/src/api/queries.ts`, hook này có `refetchInterval: 30_000`: cứ 30 giây hỏi lại, cả khi tab nằm nền, để logo và số trên menu luôn đúng.
3. `useWho` rút từ phiên ra `{ name, isAdmin }`. Nếu server cũ không gửi `user` trong phiên thì coi là admin (như bản 1); đó là chuyện hiển thị, không cấp quyền gì.

`Shell` rồi vẽ ba thứ: `Header` (thanh đầu), `SideMenu` (menu trái) và `<Outlet />` (chỗ màn hiện ra). Thanh đầu có `sticky top-0` nên dính khi cuộn, và cao đúng 61 px từ 1024 px trở lên; con số này là biến `--header-h: 61px` trong `styles.css` để menu trái biết dính ở đâu.

Mục menu là `NavLink` của react-router:

```tsx
const to = menuPath(s, admin);
const n = overview && s.menu.count ? s.menu.count(overview) : undefined;
return (
  <NavLink
    key={s.menu.label}
    to={to}
    end={to === "/" || to === "/dashboard"}
    onClick={onNavigate}
    className={({ isActive }) =>
      "flex min-h-[44px] items-center gap-[10px] rounded-ctl px-[10px] no-underline sm:min-h-[40px] " +
      (isActive ? "bg-accent-tint font-semibold text-accent hover:text-accent" : "text-ink-soft hover:bg-soft hover:text-ink-soft")
    }
  >
```

`NavLink` tự đặt `aria-current="page"` lên mục đang mở (chú thích trong mã nhắc điều này); trình đọc màn hình đọc được "trang hiện tại", và test kiểm bàn phím cũng dựa vào nó. `end` buộc khớp chính xác: không có `end` thì mục "/" sáng ở mọi trang vì mọi đường dẫn đều bắt đầu bằng "/".

Danh sách mục được lọc ngay trên đầu `SideMenu`: `screens.filter(... (admin || !s.admin))`. User thường không thấy mục của màn admin.

Màn hẹp (dưới 1024 px): menu ẩn, thanh đầu có nút "Menu" với `aria-expanded` và `aria-controls="menu-chinh"`. Bấm thì menu hiện thành một khối dưới thanh đầu. Bản thiết kế chọn cách này thay vì ngăn trượt (mục 5).

### 3.4 `web/src/screens/trang-chu/index.tsx`: trang chủ theo vai

Cả file chỉ có hai component:

```tsx
export function Home() {
  const { isAdmin } = useWho();
  return isAdmin ? <Dashboard /> : <Navigate to="/tools" replace />;
}

export function DashboardAlias() {
  const { isAdmin } = useWho();
  return isAdmin ? <Navigate to="/" replace /> : <Dashboard />;
}
```

`/` là Bảng điều khiển với admin và chuyển sang `/tools` với user thường. `/dashboard` ngược lại. `replace` thay mục hiện tại trong lịch sử trình duyệt, nên nút Back không quay lại một địa chỉ vừa bị chuyển đi. Phiên "xem như" (mốc M13) mang vai của user được xem, nên admin đang xem như `khach_a` mở `/` sẽ tới `/tools`. Bản thiết kế (mục 3.6) giải thích vì sao không chỉ đổi đích sau đăng nhập: mở thẳng địa chỉ panel vẫn ra Bảng điều khiển, trái đặc tả "user thường: trang chủ là Công cụ".

### 3.5 `web/src/screens/cong-cu/`: trang Công cụ

Hai file. `tools.ts` là bảng ô; `index.tsx` là màn.

```ts
export type Tool = {
  path: ScreenPath;
  icon: IconName;
  key: keyof S["tiles"];
  count?: "sites" | "dbs" | "incidents";
  userPath?: ScreenPath;
};
...
export function visibleGroups(groups: ToolGroup[], admin: boolean, isAdminPath: (p: string) => boolean): ToolGroup[] {
  return groups.map((g) => ({ ...g, tools: g.tools.filter((t) => admin || !isAdminPath(t.path)) })).filter((g) => g.tools.length > 0);
}
```

`path` có kiểu `ScreenPath`, nên viết `/site` thay vì `/sites` là `tsc` báo lỗi lúc biên dịch (bất biến B9). Cờ admin không chép sang `tools.ts`: `isAdminPath` đọc thẳng `routes.tsx` lúc vẽ, nên không có hai bản để lệch. `visibleGroups` bỏ ô trỏ tới màn admin khi người xem không phải admin, rồi bỏ nhóm rỗng. File này chỉ `import type` để `node --test` chạy được (bản thiết kế T5).

Hai ô không có màn riêng, và `tools.ts` ghi ngay trong mã: "request và lỗi 5xx" trỏ tới Bảng điều khiển (`path: "/"`, `userPath: "/dashboard"`), "PHP theo site" trỏ tới `/sites`. Đây là quyết định Q11 của `M16.md`: trang Công cụ chỉ có ô cho thứ đã tồn tại.

Màn gọi hai nguồn dữ liệu đã nằm sẵn trong bộ nhớ đệm của khung trang: phiên và tổng quan.

```tsx
const session = useSession().data;
const overview = useOverview().data;
const users = useUsers(admin);
const own = admin ? users.data?.find((r) => r.user.name === who.name) : undefined;
```

Riêng admin thì tổng quan không có quota (gói và mức dùng), nên màn lấy hàng của chính admin trong `GET /api/v1/users`; route đó chỉ admin gọi được, nên `useUsers(admin)` không chạy với user thường. Số trên ô tính bằng `countOf`:

```tsx
function countOf(tool: Tool, o: Overview | undefined): number | undefined {
  if (!o || !tool.count) return undefined;
  if (tool.count === "sites") return activeSites(o);
  if (tool.count === "dbs") return o.db_count;
  return o.incidents.length || undefined;
}
```

Ô "Site" dùng `activeSites`, đúng hàm mà mục menu Site dùng (khai ở `routes.tsx`); ô "Cơ sở dữ liệu" đọc `db_count`. Nhờ chung nguồn, số ở menu, ở ô và ở chân bảng của màn Cơ sở dữ liệu cùng một định nghĩa. Sự cố chỉ hiện số khi lớn hơn 0 (`|| undefined`).

Thẻ "Đang dùng" dùng `plan.usage.sites` và `plan.usage.dbs`. Hai số này đếm theo cách gói tính, tức **cả hàng trong thùng rác**, khác `db_count` (chỉ đang chạy). Màn có hai dòng chú thích (`t.usageHow`, `t.unlimitedNote`) để không ai đặt hai số cạnh nhau mà tưởng cùng luật. Chỗ này là phát hiện S2 của lần soát thiết kế (mục 5).

### 3.6 `internal/server/api_health.go`: trường `db_count`

Đây là chỗ duy nhất mốc đổi API. `handleOverview` đã đọc danh sách site trong phạm vi người gọi (`scope`). M16 thêm:

```go
dbs, err := s.deps.Store.StandaloneDBs(ctx, scope)
if err != nil {
	apiFail(w, r, err)
	return
}
ov.DBCount = activeDBCount(sites, dbs)
```

```go
func activeDBCount(sites []site.Site, dbs []account.Database) int {
	n := 0
	for _, st := range sites {
		if st.Status == site.Active {
			n++
		}
	}
	for _, d := range dbs {
		if d.Status == account.DBActive {
			n++
		}
	}
	return n
}
```

Mỗi site đang chạy có đúng một DB, cộng với các DB rời đang chạy. Site trong thùng rác không tính (DB của nó đi theo site); DB rời trong thùng rác không tính (nó đã bị `DROP`). `scope` là cùng thứ mà mọi phần khác của tổng quan dùng: admin thấy cả máy, user thường chỉ thấy của họ, phiên "xem như" cho phạm vi của người được xem. Hàm không nhận tham số nào từ người gọi, nên user thường không có đường nào đọc số của người khác (đe doạ Đ1 của bản thiết kế).

Mã đếm từ bảng `sites` và `databases` của chính TPAN, không hỏi MariaDB, vì tổng quan được hỏi lại mỗi 30 giây (chặng 3.3). Phía TypeScript: `db_count?: number` ở `web/src/api/types.ts`, có dấu `?` vì server cũ không gửi.

Ba test (đã chạy ở mục 2.5): `TestOverviewDBCountMatchesDatabases` so `db_count` với số hàng `status = "active"` mà `GET /api/v1/databases` trả cho đúng người gọi, dữ liệu có cả site và DB rời trong thùng rác; con số mong đợi viết tay (admin 3, `usera` 2, `userb` 0) để test không chỉ so hai đường cùng sai. `TestOverviewDBCountScoped` kiểm phiên "xem như". `TestActiveDBCount` là bảng ca biên.

### 3.7 Menu thả và tab: `components/Menu.tsx`, `components/Tabs.tsx`

Mọi menu thả (menu tài khoản, nút ba chấm của hàng bảng) đi qua một file:

```tsx
export function Menu({ trigger, label, align = "end", children }: MenuProps) {
  return (
    <DM.Root modal={false}>
      <DM.Trigger asChild>{trigger}</DM.Trigger>
      <DM.Portal>
        <DM.Content aria-label={label} align={align} sideOffset={4} collisionPadding={8} className="...">
          {children}
        </DM.Content>
      </DM.Portal>
    </DM.Root>
  );
}
```

`DM` là `@radix-ui/react-dropdown-menu`. Thư viện này lo phần khó của menu: mở bằng Enter, Space hay mũi tên xuống, đi bằng mũi tên, Escape đóng và trả focus về nút, bỏ qua mục bị khoá (file `Menu.tsx` ghi đúng các điều này; bản thử T2 đã thử bằng phím thật). `modal={false}` là điểm cốt lõi: chế độ mặc định chèn một thẻ `<style>` khoá cuộn, CSP `default-src 'self'` của panel chặn nó (mục 4.4). Bất biến B8: `TestKieuDang` kiểm file nào ngoài `Menu.tsx` import thư viện này là lỗi, và `Menu.tsx` phải có `modal={false}`.

Trang một site chia sáu tab bằng `components/Tabs.tsx` (bọc Radix Tabs). Tab đang mở nằm trong địa chỉ:

```tsx
const raw = params.get("tab");
const tab: TabId = visible.find((v) => v === raw) ?? "tong-quan";
const setTab = (v: TabId) =>
  setParams(
    (prev) => {
      const next = new URLSearchParams(prev);
      if (v === "tong-quan") next.delete("tab");
      else next.set("tab", v);
      return next;
    },
    { replace: true },
  );
```

(Đây là `web/src/screens/site/index.tsx`, mình đã gộp cho gọn.) Giá trị lạ hay tab đang ẩn (tab Log và PHP ẩn khi site ở thùng rác hay chưa có stack) thì coi như Tổng quan. `replace: true` để nút Back của trình duyệt về màn trước chứ không đi qua từng tab. Lưu ý trong đầu file: nội dung của tab không mở bị gỡ khỏi trang, nên thứ cần sống qua lần đổi tab (ga đang chọn, phép kiểm đang mở, bản xem trước xoá) phải ở component cha `SiteView`.

### 3.8 Màu: `web/src/styles.css` và `web/tuong_phan_test.go`

Mọi màu khai một lần ở `styles.css`, hai khối: sáng (`:root, [data-theme="light"]`) và tối (`[data-theme="dark"]`). Ví dụ cho ô nhập:

```css
/* khối sáng, dòng 134-135 */
--line: #C5CBD3;
--field: #7A8592;
/* khối tối, dòng 172-173 */
--line: #3B424B;
--field: #737C87;
```

Rồi `@theme inline` nối từng biến thành màu mà Tailwind biết (`--color-field: var(--field)`), để viết `border-field` trong component. `--color-*: initial` ở đầu khối tắt bảng màu có sẵn của Tailwind: viết `bg-red-500` sẽ không ra gì. Đổi `data-theme` trên `<html>` (file `web/public/theme.js` đặt trước khi vẽ) là đổi cả giao diện.

`tuong_phan_test.go` đọc thẳng `styles.css`, không có bản sao màu nào trong test:

```go
var bangCapMau = []capMau{
	// Chữ >= 4,5.
	{"ink", []string{"paper", "surface", "soft", "pre", "accent-tint"}, nguongChu},
	{"muted", []string{"paper", "surface", "soft", "pre", "accent-tint"}, nguongChu},
	{"accent", []string{"surface", "paper", "soft", "accent-tint"}, nguongChu},
	...
	// Hình >= 3.
	{"field", []string{"surface", "soft", "paper"}, nguongHinh},
	...
}
```

Mỗi dòng: màu đứng trước phải đạt ngưỡng trên từng màu nền sau nó, ở cả sáng và tối. Tên biến gõ sai hay biến bị xoá thì test đỏ vì "thiếu biến", không lặng lẽ bỏ qua. Tỷ lệ tính bằng công thức WCAG 2.1 (hàm `doChoi`, `tyLeTuongPhan`), chỉ dùng thư viện chuẩn.

`TestAccentTint` kiểm màu `--accent-tint` (nền mục menu đang mở) đúng bằng `--accent` pha 10% trên `--surface` ở sáng, 16% ở tối:

```go
kenh := func(x, y int) int { return (x*p + y*(100-p) + 50) / 100 }
return fmt.Sprintf("#%02X%02X%02X", kenh(ar, sr), kenh(ag, sg), kenh(ab, sb)), nil
```

Vì sao hex viết sẵn mà không để trình duyệt tự pha bằng `color-mix()`: mục 5. Test sinh ra để sửa lại sai lệch khi ai đó chỉnh `--accent` mà quên `--accent-tint`: nó báo đúng mã cần điền.

### 3.9 `web/kieu_dang_test.go`: giữ luật bằng test

Bản 2 có một loạt luật mà trình biên dịch TypeScript không biết: không cỡ chữ ngoài {12; 12,5; 13; 14; 16; 18; 24} px, không mã màu viết thẳng trong `.tsx`, không `uppercase`, không `shadow-` ngoài `shadow-pop`, `font-mono` chỉ ở vài file được phép, không `dangerouslySetInnerHTML` hay chuỗi `http://`. Test đọc từng file `.ts`, `.tsx` trong `web/src`, bỏ chú thích, rồi so mẫu. Ví dụ luật 3 (màu):

```go
{3, regexp.MustCompile(`(?:^|[^&])(#(?:[0-9a-fA-F]{6}|[0-9a-fA-F]{3}))(?:[^-\w]|$)`), nil,
	"màu viết thẳng: màu chỉ khai ở styles.css, dùng class (bg-accent, text-muted...) hoặc var(--tên)"},
```

Có hai điều tinh tế trong mẫu này: `#phien-ban` (neo trang) và `#add-site` không bị nhận nhầm là màu, vì sau chữ số hex phải hết từ và không phải dấu `-`; còn `&#123;` là thực thể HTML nên đứng sau `&` thì bỏ qua.

Trong lúc đổi, giao diện bản 1 và bản 2 cùng nằm trong repo, nên test có danh sách "chưa đổi":

```go
// chuaDoi: tiền tố đường dẫn (tính từ web/) của mã chưa đổi sang bản 2.
var chuaDoi = []string{}
```

Danh sách này chỉ được ngắn đi: thư mục nằm trong danh sách thì không bị kiểm luật 1 đến 6; nhưng nếu thư mục trong danh sách đã sạch thì test lỗi "đã đổi xong ...: gỡ khỏi danh sách chuaDoi", buộc người nhận phần gỡ nó ra. Từ commit `4767288` danh sách rỗng, test quét toàn bộ `web/src`. Mục 4.8 giải thích cơ chế này (người ta hay gọi là "ratchet", cái cờ-lê chỉ vặn một chiều).

Thợ đổi một màn kiểm riêng thư mục của mình bằng `make kieu-dang DIR=src/screens/cong-cu/`: lệnh này đặt biến môi trường `KIEU_DANG_THU` rồi chạy đúng hai test, và vì có biến đó nên test bỏ qua danh sách "chưa đổi" và kiểm đủ luật.

### 3.10 `web/src/screens/site/isolation.ts`: logic thuần, test bằng Node

Phép kiểm cô lập (mốc M14) có id `isolation:<từ>:<tới>`: site có N site khác thì có 2N phép. Bản 1 in 2N dòng. Bản 2 gộp theo site kia:

```ts
export function isolationEnds(id: string): [from: string, to: string] | null {
  if (!id.startsWith(prefix)) return null;
  const parts = id.slice(prefix.length).split(":");
  return parts.length === 2 ? [parts[0], parts[1]] : null;
}
```

`groupIsolation(checks, domain)` dùng nó để lập từng cặp (chiều ra, chiều vào), tính trạng thái xấu nhất theo thứ tự `pass < unknown < warn < fail`, sắp cặp có phép lỗi lên trước, và trả `openIds` là các phép `fail` để giao diện mở sẵn. Nó so tên miền **bằng nhau** sau khi tách bằng `:`, không dùng `startsWith`: nếu dùng thì `a.com` nhận nhầm phép của `a.com.vn` (phát hiện S13, có ca test riêng).

File chỉ `import type` và là `.ts` (không phải `.tsx`) vì `node --test` chạy thẳng file TypeScript, Node tự bỏ phần kiểu, nhưng không nhận `.tsx` và không nạp được file kéo theo `i18n/index.tsx`. Đó là lý do chuỗi hiển thị nằm ngoài file này. `isolation.test.ts` có 14 ca `test(...)` (đếm trong file; thông điệp commit `94a2065` cũng ghi 14); hai ca còn lại trong tổng 16 của `make test-web` ở mục 2.5 nằm ở `co-so-du-lieu/footer.test.ts` (test chân bảng đếm DB). Có thêm ca "một cặp lỗi" mà máy demo không tạo được, nên mới cần test thay vì chỉ xem ảnh.

### 3.11 `scripts/anh-readme/m16*.go` và `scripts/e2e-m16.sh`: đồ nghề kiểm bằng trình duyệt thật

Giao diện không có test nào mở màn trong trình duyệt, nên mốc viết đồ nghề riêng. Tất cả chạy qua `make`:

| Lệnh | Việc | Chạy ở |
|---|---|---|
| `make do-giao-dien` | đo từng màn bằng `do-giao-dien.js` (trang dài, cỡ chữ, tỷ lệ chữ mono, ô viền màu chữ); `NGUONG=1` thì vi phạm ngưỡng cứng là lỗi | Mac, máy demo |
| `make anh-m16` | chụp mọi màn, kiểm console, cuộn ngang, cỡ chữ, bàn phím, tiếng Anh | Mac, máy demo |
| `make anh-xem CHI=cong-cu,nguoi-dung` | chụp nhanh vài màn vào `tmp-phien/anh` (ngoài git) để soát giữa các đợt | Mac, máy demo |
| `make anh-mau` | chụp ba trang mẫu tĩnh ở `docs/mau-giao-dien/v2` để đặt cạnh ảnh bản thật | Mac |
| `make e2e-m16` | bước 19 (mục 2.2) | Mac, máy ảo |

Các script dùng chung một lõi: Chrome không giao diện điều khiển bằng Chrome DevTools Protocol (CDP, giao thức mà DevTools của Chrome cũng dùng). Script giữ lại sự kiện lỗi của Chrome (`Runtime.exceptionThrown`, `console.error`, lỗi mức `Log`, gồm vi phạm CSP và request hỏng), gõ phím và bấm chuột thật, đổi cỡ cửa sổ.

Phép đi Tab (`tabWalk` trong `m16_phim.go`) làm hai việc: mọi thứ bấm được trong `<main>` đều phải được Tab tới, và phần tử đang giữ focus phải nhìn thấy được, không bị thanh đầu trang dính che ở điểm giữa. Để biết "bị che", nó hỏi trình duyệt phần tử nào nằm ở toạ độ đó:

```js
const top = document.elementFromPoint(cx, cy);
const covered = !top || !(top === e || e.contains(top) || top.contains(e));
```

Đây là chỗ mà `scroll-padding-top: 72px` ở `styles.css` có tác dụng: không có nó, Tab tới một ô ở gần mép trên thì trình duyệt cuộn vừa đủ nhìn thấy, và thanh đầu dính che mất ô đó.

Kịch bản `e2e-m16.sh` thì gọi ba thứ cùng lúc: `scripts/vm.sh` (máy ảo), `make release` (đóng gói bản A, B), và `go run ./scripts/anh-readme -bo m16-that` (trình duyệt). Mật khẩu panel đi qua biến môi trường do script đặt, không vào dòng lệnh, không vào log (phần cuối của `M16.log` dò lại điều đó).

## 4. Kiến thức nền

### 4.1 Biến CSS và `@theme inline` của Tailwind 4

Biến CSS (`--ink`, `--accent`) là một tên gắn với một giá trị, dùng lại bằng `var(--ink)`. Tailwind 4 tạo class (`bg-accent`, `text-muted`) từ các biến khai trong khối `@theme`. Chữ `inline` bảo Tailwind giữ nguyên `var(--accent)` trong class sinh ra thay vì chép giá trị, nên khi `data-theme` đổi, biến đổi và class đổi theo, không cần class riêng cho tối. Vì khác nhau giữa sáng và tối nên `--shadow-pop` không đặt thẳng trong `@theme` được: Tailwind sẽ nối một biến vào chính nó (quyết định Q26), nên có `@utility shadow-pop { box-shadow: var(--shadow-pop); }`.

Trong mã: `web/src/styles.css`, khối `@theme inline`. Tự nhìn: mở DevTools của Chrome, chọn thẻ `<html>`, tab Styles; đổi `data-theme` từ `light` sang `dark` bằng cách sửa thuộc tính, các biến đổi giá trị ngay. Hoặc chạy `grep -n -- '--field' web/src/styles.css` trên Mac thấy hai giá trị.

### 4.2 Tỷ lệ tương phản WCAG, và vì sao viền ô nhập có biến riêng

WCAG (hướng dẫn truy cập nội dung web) 2.1 đo độ khác nhau giữa hai màu bằng tỷ lệ từ 1:1 (cùng màu) tới 21:1 (đen trên trắng). Chữ thường cần ít nhất 4,5:1 (mục 1.4.3), thành phần giao diện và hình cần 3:1 (mục 1.4.11). Vì sao viền ô nhập cần 3:1: viền là thứ duy nhất cho biết ô nằm ở đâu. `--line` trên nền thẻ chỉ có 1,63:1 (bản thử T4), nên không dùng được; biến mới `--field` đạt 3,75 ở sáng, 4,00 ở tối. Nút phụ có chữ nên vẫn dùng `--line`: chữ đã nói đó là nút.

Trong mã: `web/tuong_phan_test.go` (`bangCapMau`), `web/src/styles.css` (`--field`). Tự nhìn: `go test ./web/ -run TestTuongPhan -v`; hoặc xem `docs/bang-chung/M16-nghien-cuu/t4-tuong-phan.ket-qua.txt`, mỗi dòng ghi tỷ lệ và "đạt". Trong Chrome DevTools, chọn một ô nhập, tab Styles, bấm ô vuông màu cạnh `border-color`: bộ chọn màu hiện "Contrast ratio".

### 4.3 `position: sticky` và `scroll-padding-top`

`position: sticky` làm phần tử trôi theo trang cho tới khi chạm một mép (ở đây là mép trên), rồi dính lại. Thanh đầu và menu trái dùng nó. Phần tử dính che mất thứ nằm sau nó, nên khi trình duyệt cuộn tới một neo (`#phien-ban`) hay tới ô đang nhận Tab, ô đó có thể nằm khuất dưới thanh đầu. `scroll-padding-top` ở `<html>` bảo trình duyệt chừa khoảng đó ở mép trên khi cuộn: 72 px, lớn hơn chiều cao 61 px của thanh.

Trong mã: `styles.css`, `html { scroll-padding-top: 72px }`; `Layout.tsx`, `Header` (`sticky top-0`), `SideMenu` (`lg:sticky lg:top-[var(--header-h)]`). Tự nhìn: mở `/settings#phien-ban` trên máy demo, khối "Phiên bản và cập nhật" nằm dưới thanh đầu, không bị che. Xoá dòng `scroll-padding-top` trong DevTools rồi tải lại: chưa chạy, mình không dự đoán kết quả.

### 4.4 CSP và Radix `modal={false}`

CSP (Content-Security-Policy) là header mà server gửi để bảo trình duyệt chỉ nạp thứ gì từ đâu. Panel gửi `default-src 'self'; frame-ancestors 'none'` (`internal/server/auth.go`, hàm `secure`): script, style, font, ảnh chỉ từ chính panel; không trang nào nhúng panel vào iframe. Không có `style-src` riêng nên `default-src` áp cho style, và thẻ `<style>` chèn bằng JS bị chặn. Radix DropdownMenu mặc định ở chế độ modal: nó khoá cuộn trang bằng một thẻ `<style>` (thư viện `react-remove-scroll`), nên mỗi lần mở menu trình duyệt báo một vi phạm CSP. Đặt `modal={false}` thì Radix bỏ bước đó. Bản thử T2 đo cả hai trường hợp trên panel demo: không modal là 0 lỗi console, modal là một lỗi `style-src-elem`.

Trong mã: `components/Menu.tsx` (`<DM.Root modal={false}>`). Tự nhìn trên máy ảo (chưa chạy):

```bash
scripts/vm.sh exec curl -skI https://127.0.0.1:8443/     # máy ảo: in header, tìm dòng content-security-policy
```

Kết quả T2 trên demo (`t2-radix-csp.ket-qua.txt`): `header CSP của trang: default-src 'self'; frame-ancestors 'none'`.

### 4.5 `aria-expanded`, `aria-current`, và phím Tab

Thuộc tính ARIA cho trình đọc màn hình biết trạng thái mà mắt thấy bằng hình. `aria-expanded="true"` trên nút nghĩa là khối nó điều khiển đang mở (nút Menu ở màn hẹp, nút "Hỏi AI", dòng mở ra được). `aria-current="page"` đánh dấu mục menu của trang hiện tại. Phím Tab đi qua các phần tử bấm được theo thứ tự trong trang; viền focus (`outline: 2px solid var(--accent)`) cho biết đang ở đâu, và bản thiết kế quy định không bao giờ tắt nó. Tab của Radix là ngoại lệ có chủ đích: chỉ một tab nhận Tab, các tab còn lại đi bằng mũi tên trái phải (mẫu ARIA cho hàng tab).

Trong mã: `Layout.tsx` (nút Menu, `NavLink`), `components/Disclosure.tsx`. Tự nhìn: trong DevTools, tab Elements, chọn mục menu đang mở, thấy `aria-current="page"`; bấm nút Menu ở 390 px, thấy `aria-expanded` đổi `false` sang `true`.

### 4.6 Route theo vai: hiển thị khác với quyền

Ẩn mục menu, chuyển hướng `/` sang `/tools`, bọc màn bằng `AdminOnly` đều chỉ là giao diện; ai cũng có thể gọi API bằng `curl`. Quyền thật nằm ở luật của từng route Go (mốc M13, `internal/server/routes.go`): route chỉ admin trả 403 cho user thường. M16 không thêm route nào và không đổi luật nào (bất biến B16, kiểm bằng `authz_test.go` có sẵn). Hệ quả thực tế ở trang Công cụ: màn của user thường không gọi API admin (`useUsers(admin)` chỉ chạy với admin), nên e2e đếm được 0 request 403.

Trong mã: `main.tsx` (`AdminOnly`), `routes.tsx` (`isAdminPath`), `screens/cong-cu/index.tsx`. Tự nhìn: mục 2.2 phần D, hoặc đọc `13-m13-nhieu-nguoi-dung.md`, nơi có output 403 thật của user thường gọi route admin.

### 4.7 `as const satisfies` và kiểu `ScreenPath`

Trong TypeScript, `as const` bảo trình biên dịch coi mảng là hằng và giữ nguyên chuỗi `"/tools"` làm kiểu riêng thay vì `string`. `satisfies ScreenEntry` kiểm mảng đúng khuôn mà không làm mất kiểu hẹp ấy. Hợp lại: `(typeof table)[number]["path"]` là hợp của mọi đường dẫn có thật, và đặt làm kiểu của `Tool.path`. Sai chính tả đường dẫn là lỗi biên dịch, không phải một ô trỏ vào trang 404 mà không ai thấy.

Trong mã: `routes.tsx` (cuối file), `cong-cu/tools.ts`. Tự nhìn (chưa chạy): sửa `"/sites"` thành `"/site"` ở một dòng của `tools.ts`, rồi `pnpm typecheck` trong thư mục `web`; sau đó sửa lại.

### 4.8 Danh sách "chưa đổi" chỉ được ngắn đi (ratchet)

Khi đổi một giao diện lớn theo nhiều đợt, luật kiểu dáng mới sẽ đỏ ngay vì phần chưa đổi vi phạm đầy rẫy. Giải pháp thường: tạm tắt test. Giải pháp ở đây: test có danh sách thư mục "chưa đổi", bỏ qua chúng; nhưng khi một thư mục trong danh sách đã sạch thì test **đỏ**, buộc người nhận phần gỡ nó ra. Danh sách vì thế chỉ co lại, không bao giờ phình ra, và khi rỗng thì luật áp toàn bộ. Cơ chế này sinh ra từ phát hiện nặng duy nhất của lần soát thiết kế (S1, mục 5).

Trong mã: `web/kieu_dang_test.go`, `chuaDoi`, `quetKieuDang` (dòng "đã đổi xong ... gỡ khỏi danh sách"). Tự nhìn: `make kieu-dang DIR=src/screens/cong-cu/` (đã chạy ở mục 2.5, ra `ok`). Để thấy test đỏ: thêm `text-[11px]` vào một file trong `cong-cu/`, chạy lại lệnh đó, rồi bỏ dòng đó đi (chưa chạy).

### 4.9 Radix Tabs: nội dung bị gỡ khi rời tab

Theo bản thử T2, Radix Tabs chỉ dựng ruột của tab đang mở; khối của tab không mở không có phần tử con nào trong trang. Thẻ vỏ `role="tabpanel"` thì vẫn còn: kịch bản thử in "3 khối tabpanel" cho ba tab. Ruột bị gỡ có hệ quả cho cách viết: mọi thứ cần sống qua lần đổi tab phải nằm ở component cha, vì component con bị huỷ và dựng lại. Đó là lý do trang một site giữ state ở `SiteView`. Mặt tốt: bằng chứng của 50 site không bị dựng cùng lúc nếu chúng nằm ở tab chưa mở.

Trong mã: `components/Tabs.tsx`, `screens/site/index.tsx` (chú thích đầu file). Tự nhìn: trong DevTools, tab Elements, trên trang một site tìm `role="tabpanel"`; đổi tab và quan sát ruột đổi.

## 5. Cách khác đã cân nhắc, chỗ đã sai, và giới hạn

### 5.1 Cách khác

Bảng "Các cách đã cân nhắc" của bản thiết kế (mục 2) và các quyết định Q của `M16.md`:

| Việc | Chọn | Không chọn, vì sao |
|---|---|---|
| Menu thả, tab | Radix `react-dropdown-menu` và `react-tabs` (đã thử trên panel demo, bản thử T2) | Tự viết: phải tự lo vai ARIA, focus, phím mũi tên, đóng khi bấm ra ngoài. `<select>` làm menu việc: không đặt được nhãn đọc cho từng hàng |
| Khối mở ra (bằng chứng, dòng cảnh báo) | Nút `aria-expanded`, nội dung chỉ dựng khi mở (`Disclosure`) | `<details>`: nội dung luôn nằm trong DOM, nên bằng chứng 98 dòng của 50 site vẫn bị dựng |
| Menu ở màn hẹp (Q7) | Khối mở ra dưới thanh đầu bằng một nút | Ngăn trượt bằng Radix Dialog: mỗi lần mở sinh một dòng vi phạm CSP ở console (T2) |
| Màu nền mục đang mở, `--accent-tint` (Q3) | Mã hex cố định, có `TestAccentTint` | `color-mix()` lúc chạy: trình duyệt trả `color(srgb ...)` thay vì `rgb(...)`, không đo được tương phản (T2) |
| Trang chủ của user thường (Q6) | `/` chuyển sang `/tools`; Bảng điều khiển của họ ở `/dashboard` | Chỉ đổi đích sau đăng nhập: mở thẳng địa chỉ panel vẫn ra Bảng điều khiển, trái đặc tả |
| Kiểm kiểu dáng | Test Go quét `web/src` | Chỉ soát bằng mắt: 22 nghìn dòng, tám thợ |
| Đếm DB cho menu (Q5) | Trường `db_count` ở API tổng quan | Gọi thêm `/api/v1/databases` ở khung trang: câu SQL đo dung lượng chạy ở mỗi lần mở trang |
| Test logic TypeScript (Q14) | `node --test` chạy thẳng `.ts`, không thêm thư viện | Thêm một trình chạy test: repo chưa có, và bản thử T5 cho thấy Node 22 đủ dùng |
| Viền ô nhập (Q1) | Biến riêng `--field` đậm hơn `--line` | Dùng `--line` của đặc tả: chỉ 1,63:1, ô nhập không nhìn ra được |

### 5.2 Chỗ agent đã làm sai, và phát hiện bằng cách nào

- **Danh sách "chưa đổi" lúc thiết kế (S1, nặng).** Bản thiết kế đầu viết ba điều không thể cùng đúng: luật kiểu dáng quét toàn bộ `web/src`, bật từ đợt 1, và `make check` phải xanh mỗi đợt. Lúc đó hơn 30 file còn dùng `text-[11px]`, `border-ink`, `tracking-[0.08em]`. Lần soát bằng một thợ Opus effort max (1 điểm nặng, 9 vừa, 18 nhẹ) tìm ra bằng cách đối chiếu bản thiết kế với mã. Cách sửa là cơ chế ratchet ở mục 4.8 (Q20).
- **Hai số cạnh nhau, hai luật đếm (S2).** Bản thiết kế viết `usage.dbs` "đúng là số DB rời". Thật ra hai số `usage.sites`, `usage.dbs` đếm cả hàng trong thùng rác (`internal/store/users.go`, hai `COUNT(*)` không lọc `status`). Trang Công cụ sẽ đặt chúng cạnh số "đang chạy". Người soát tìm ra bằng cách đọc câu SQL. Cách sửa: ghi định nghĩa trên màn, thêm dòng chú thích dưới thẻ "Đang dùng" và ở chân bảng Người dùng ("Site và DB rời tính như gói tính: cả thứ trong thùng rác.").
- **`id` của tab (S5).** Bản thiết kế ban đầu định đặt `id="tab-<giá trị>"` cho nút tab làm móc kiểm thử. Radix 1.2.1 rải thuộc tính người dùng đè lên `id` nó tự sinh, nhưng khối nội dung vẫn trỏ `aria-labelledby` tới id tự sinh, nên mất nhãn đọc. Người soát đọc `dist/index.mjs` của thư viện (dòng 124-125 và 174). Cách sửa: không đè `id`, móc là `data-tab="<giá trị>"`.
- **Thanh lệnh dẫn sai cho user thường (S6, Q22).** Trang chủ theo vai đổi nghĩa mục "Bảng điều khiển" từ đợt 1. Để phần logic của thanh lệnh tới đợt 3 thì thanh lệnh của user thường dẫn sai suốt đợt 2. Cách sửa: phiên chính sửa phần logic (`menuPath`, bỏ dòng "/dashboard") ngay ở đợt 1; thợ đợt 3 chỉ đổi kiểu dáng.
- **Bảng của hai màn mẫu tràn ngang lần chụp đầu.** Ở Người dùng và Cơ sở dữ liệu, bảng vượt thẻ vì hai cột số và vì thanh mức dùng 96 px (`M16.md`, mục 3). Phát hiện khi chụp cạnh trang mẫu. Sửa: thanh mức mặc định 64 px; DB không đọc được cỡ hay số bảng thì ô ghi một dấu gạch ngắn, đủ lời nằm trong `title`.
- **Màn Bảo mật dài 1836 px sau đợt 2.** Ngưỡng là 1800 px (2 màn hình). Phát hiện bằng `make do-giao-dien NGUONG=1`. Nguyên nhân: nút "Bật tường lửa", "Cài fail2ban" và lời dẫn nằm ở hai hàng. Sửa: gộp một hàng trong `CheckRow`; lúc mở màn là 1,9 màn hình.
- **Nút "Hỏi AI" nổi làm phép đo đỏ tới đợt 3.** Phép đo "ô viền màu chữ" đỏ ở mọi màn admin vì đúng nút này (viền chữ chính, nằm ở góc dưới phải). Bản thiết kế đã quyết bỏ nút nổi ở đợt 3 (T8); trong lúc đó `M16.md` ghi rõ chỗ đỏ này là có chủ đích. Sau đợt 3, "ô viền màu chữ" là 0 ở mọi màn.
- **Thứ tự nút trong hộp hỏi lại suýt đổi (Q25).** Thứ tự nút trong hộp suýt bị đổi sang "Huỷ" trước. Thợ W3 phát hiện kịch bản bấm thật của M15 và M16 bấm nút **đầu tiên** của hộp; đổi chỗ thì hai kịch bản bấm nhầm "Huỷ". Giữ như bản 1: nút làm việc trước. Điểm đáng nhớ: một chi tiết hình thức nhỏ phá được một kịch bản kiểm nằm ở nơi khác.
- **Sửa file bằng `python3` và heredoc trong shell**, trái luật 7 của `AGENTS.md` (ghi file bằng công cụ ghi file, vì một số công cụ điều khiển máy chặn lệnh khi gặp tên lệnh nằm trong danh sách cấm của chúng). Cả phiên chính lẫn vài thợ đã làm vậy; các thợ tự khai trong báo cáo của họ. Nội dung sửa đúng, cách làm sai luật. Thông tin này lấy từ lời giao việc của phiên chính; các file nguồn mình đọc không có log chi tiết nên mình không ghi thợ nào, lúc nào.
- **Soát cuối (thợ soát opus, đọc toàn bộ diff so với bản 1): 0 nặng, 6 vừa, 11 nhẹ, đã sửa hết** (bảng V1 tới V6, N1 tới N11 ở `M16.md` mục 2 "Soát cuối"). Những lỗi đáng kể: (V1) bản xem trước Plan chỉ mở sẵn lệnh khi Plan có tối đa 3 bước, mà Plan tạo site 17 đến 19 bước nên người duyệt không thấy lệnh nào; (V2) bấm một ga ở Bảng điều khiển không còn mở bằng chứng cả chặng như bản 1; (V6) đóng hộp hỏi lại thì focus rơi về `<body>`, sửa bằng `components/focusReturn.ts` (nhớ phần tử cuối cùng có focus nằm ngoài menu và hộp thoại, trả focus về đó khi đóng); (N8) test kiểu dáng tách token ở dấu `:` nên lọt `text-[length:11px]`, nay tách giữ nguyên giá trị trong `[...]` và `(...)`, có thêm 11 ca test bảng. Điểm chung của cả nhóm: các lỗi này không làm test nào đỏ, chỉ lộ ra khi đối chiếu từng chức năng với bản 1.
- **Phép kiểm bàn phím báo sai (mục 2.4).** Lần chạy đầu của `make anh-m16` báo 7 màn "Tab không tới 1 phần tử": phần tử đó là khung `role="tablist"` mà Radix đặt `tabindex="0"` chỉ để chuyển focus. Lỗi của phép kiểm; sửa ở `tabWalk`.
- **Hiểu lầm khi thử Radix (T2).** Lần chạy đầu của kịch bản thử đọc trạng thái ngay sau phím mũi tên và tưởng phím không chạy. Radix dời focus bằng `setTimeout`, nên script phải chờ một nhịp rồi mới đọc.
- **`tsc` báo lỗi với file test của Node (T5).** Đặt `*.test.ts` vào `web/src/` thì `tsc --noEmit` báo ba lỗi (không biết `node:test`, `node:assert/strict`; đường dẫn import có đuôi `.ts`). Sửa bằng `"exclude": ["src/**/*.test.ts"]` ở `web/tsconfig.json`: file test không qua `tsc`, Node vẫn chạy.
- **Dữ liệu demo khác đặc tả (mục 2 của `M16.md`).** Bản thiết kế ghi site `tin-tuc` tạo cách đây 1 ngày; test có sẵn đòi mọi site mẫu tạo cách đây ít nhất 24 giờ, nên là 30 giờ. Ba site mới còn làm đổi số ô của một test M14 (viết cứng 12): test giờ tính n x (n - 1) từ danh sách site.

### 5.3 Giới hạn

- Bộ ảnh và phép đo chạy bằng Chrome trên máy demo. Ảnh chỉ chụp ở demo (dữ liệu `example.*`); panel máy ảo chỉ được e2e mở, không chụp vào repo, vì ảnh có thể chứa bí mật.
- Phép kiểm bàn phím chỉ đi Tab và kiểm focus nhìn thấy, không dùng trình đọc màn hình.
- Dấu trang tạo trên bản 2 (`/tools`, `/dashboard`, `?tab=`) rơi vào trang 404 nếu quay về v0.2.0 bằng nút "Quay về bản trước". Không mất dữ liệu, chỉ mất một trang (bản thiết kế mục 6, S19).
- Tab đang mở trước lúc nâng có thể bấm sang một màn chưa tải và hỏi một file JS đã đổi tên (tên chứa mã băm). Tải lại trang là đủ, vì `index.html` gửi với `Cache-Control: no-cache`. Hành vi này có từ các bản trước (khối cập nhật M15 có nút "Tải lại trang"); M16 không làm nó tệ hơn.
- Trang Công cụ chỉ có ô cho thứ đã tồn tại; các ô "đề xuất" của trang mẫu không vào bản thật (Q11).

## 6. Tự kiểm

1. User thường `khach_a` gõ `/users` vào thanh địa chỉ. Họ thấy gì, và thứ nào thật sự chặn họ gọi `GET /api/v1/users` bằng `curl`?
2. Bạn sửa `--accent-tint` ở khối sáng của `styles.css` thành `#E7EDFF`, không đổi gì khác. Test nào đỏ, và test đó muốn nói gì?
3. Người review hỏi: "Vì sao viền ô nhập dùng `--field` mà viền nút phụ dùng `--line`, hai màu gần giống nhau?" Trả lời bằng con số.
4. User `khach_a` có 1 site đang chạy, 1 site trong thùng rác, 1 DB rời đang chạy, 1 DB rời trong thùng rác. Số ở mục "Cơ sở dữ liệu" trên menu của họ là bao nhiêu? Màn Người dùng của admin ghi gì ở hai cột "Site" và "DB rời" cho họ? Vì sao hai chỗ không cùng một luật đếm?
5. Bạn thêm một ô vào `tools.ts` với `path: "/site"`. Lỗi hiện ở đâu, ở lệnh nào, trước khi code chạy hay sau?
6. Bạn viết `text-[11px]` vào một file trong `web/src/screens/backup/`. Lệnh nào trên Mac báo lỗi, và vì sao danh sách `chuaDoi` không cứu bạn?
7. (Máy ảo.) Sau khi tạo vài site và DB trên máy ảo, làm sao kiểm bằng lệnh rằng số ở mục "Cơ sở dữ liệu" của menu đúng? Ghi hai lệnh và cách đọc kết quả.
8. (Máy ảo.) Chạy lệnh nào để xem header CSP mà panel gửi? Giá trị đó giải thích điều gì về menu tài khoản?
9. Admin bấm "Xem như" `khach_a` rồi mở `/`. Họ thấy trang nào, và vì sao không phải Bảng điều khiển?
10. Trong `e2e-m16` phần E, vì sao kịch bản cài v0.2.0 và nâng bằng `tpan self-update` của chính v0.2.0, thay vì chỉ cài bản A trên máy sạch?

## Đáp án

1. Họ thấy trang "Không có quyền vào màn này" (`M16.log` dòng 710). Trang đó do `AdminOnly` vẽ ở phía giao diện (`main.tsx`, `Layout.tsx`); giao diện không gọi API admin nên không có 403 nào ở đây. Thứ thật sự chặn `curl` là luật route ở server (`internal/server/routes.go`, mốc M13): route danh sách user chỉ cho admin, user thường nhận 403. M16 không đổi luật nào; `authz_test.go` có sẵn kiểm điều này (B16).
2. `TestAccentTint` đỏ. Nó tính `--accent` (`#0B4FB8`) pha 10% trên `--surface` (`#FFFFFF`) và đòi kết quả bằng `--accent-tint`; sáng là `#E7EDF8`, không phải `#E7EDFF`. Test đó chốt điều: màu nền mục đang mở chỉ có một nguồn sự thật là `--accent`, ghi tay lệch đi thì phải báo lỗi và in mã đúng cần điền. `TestTuongPhan` có thể vẫn xanh (đổi một mã gần như không đổi tương phản); mình chưa chạy thử nên không khẳng định.
3. Viền ô nhập là thứ duy nhất cho biết ô nằm ở đâu, nên cần 3:1 (WCAG 1.4.11). `--line` (`#C5CBD3`) trên `--surface` chỉ 1,63:1, không đạt; `--field` (`#7A8592`) đạt 3,75 ở sáng, và ở tối `#737C87` đạt 4,00. Viền nút phụ không cần: chữ trên nút đã nói đó là nút. (Số lấy từ T4 trong `M16-thiet-ke.md`.)
4. Menu ghi 2: 1 site đang chạy cộng 1 DB rời đang chạy, hai thứ trong thùng rác không tính (`activeDBCount`; đúng bằng ca của `TestOverviewDBCountMatchesDatabases`). Màn Người dùng ghi Site 2 và DB rời 2, vì `usage.sites`, `usage.dbs` đếm hết các hàng của người đó, cả trong thùng rác (`internal/store/users.go`, hai `COUNT(*)` không lọc `status`), đúng cách gói dùng để chặn tạo thêm. Hai nơi trả lời hai câu hỏi khác nhau: "đang có gì chạy" và "đang chiếm bao nhiêu hạn mức". Màn có câu chú thích để người xem không nhầm.
5. `ScreenPath` là hợp các đường dẫn của `routes.tsx` nên `"/site"` không thuộc kiểu. Lỗi hiện khi `tsc` chạy: `pnpm typecheck` trong thư mục `web` (và `make check`, vì `check` có `cd web && pnpm typecheck`), tức trước khi code được build hay chạy.
6. `go test ./web/` (có trong `make check`) hoặc riêng `make kieu-dang DIR=src/screens/backup/` báo `luật 1` (cỡ chữ không thuộc {12; 12,5; 13; 14; 16; 18; 24}). Danh sách `chuaDoi` không cứu vì từ commit `4767288` nó rỗng: không còn thư mục nào được miễn. Viết `text-[length:11px]` hay `text-[11px]/5` cũng không lọt: sau soát cuối (commit `2f1992c`, lỗi N8) test không còn tách token ở dấu `:` bên trong ngoặc vuông, và bắt thêm cỡ chữ tính bằng rem.
7. Hai lệnh trên Mac: `scripts/vm.sh root tpan site list` và `scripts/vm.sh root tpan db list`. Đếm số site có trạng thái `active` (cột TRẠNG THÁI), cộng số DB rời có trạng thái đang chạy; tổng phải bằng số ở menu. Mình chưa chạy hai lệnh này trên máy ảo (cột của `tpan db list` mình chưa kiểm). Mục 2.2 phần B đã làm điều này tự động: dòng "số ở mục Cơ sở dữ liệu của menu (1) bằng số DB đang chạy của /api/v1/databases (1)".
8. `scripts/vm.sh exec curl -skI https://127.0.0.1:8443/`, tìm dòng `content-security-policy`. Mã nguồn đặt giá trị `default-src 'self'; frame-ancestors 'none'` (`internal/server/auth.go`). Vì không có `style-src` riêng, thẻ `<style>` chèn bằng JS bị chặn; menu bản modal của Radix chèn đúng thẻ đó nên phải đặt `modal={false}`. (Lệnh chưa chạy trên máy ảo; giá trị trên demo có trong `t2-radix-csp.ket-qua.txt`.)
9. Họ thấy `/tools`. Phiên "xem như" mang danh tính và vai của user được xem (mốc M13), nên `useWho().isAdmin` là `false`, và `Home` chuyển sang `/tools` (chặng 3.4).
10. Vì người dùng thật nâng từ bản cũ chứ không cài mới. Phần E kiểm các điều chỉ xảy ra khi nâng: cùng cookie vẫn đăng nhập (`/api/v1/session` vẫn `logged_in`), tải lại trang thì ra giao diện bản 2, đăng nhập lại bằng mật khẩu cũ được, site vẫn trả 200, `tpan health` không có lỗi mới. Cài mới trên máy sạch (phần A) không chứng minh được điều nào trong số đó.
