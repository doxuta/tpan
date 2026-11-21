# Giao diện TPAN: bản 2

Chốt ngày 21/11/2026 (mốc M16), thay bản "Tuyến" ngày 19/11/2026 về màu, chữ, hình khối và khung trang. Tài liệu này là đặc tả hình thức cho thư mục `web/`. Nó đi cùng mục 10 của `docs/KE-HOACH.md` (mục đó nói giao diện có những màn nào và vì sao; tài liệu này nói chúng trông ra sao).

Vì sao có bản 2: chủ dự án dùng bản 1 trên máy thật và thấy khó nhìn. Đo ra thì chữ phải đọc phần lớn là mono 11 đến 13 px, màn Bảo mật dài 6 màn hình, mỗi màn có vài chục ô viền đen, tên site dài xuống hai ba dòng. Số đo trước và sau nằm ở `docs/thay-doi/M16.md` (sau khi ghép thì ở mục 10 của `docs/KE-HOACH.md`).

Bản 2 **giữ** những thứ làm nên TPAN: logo tuyến chữ T có ga cuối đổi theo tình trạng máy (mục 11); bốn dấu trạng thái, cả hình lẫn màu (mục 2); bản đồ tuyến đường đi request; mọi trạng thái mở ra được bằng chứng; thao tác đi qua xem trước rồi mới chạy; giọng chữ (mục 8); hai ngôn ngữ; sáng và tối (mục 5); font IBM Plex nhúng sẵn; CSP `default-src 'self'`; dùng được bằng bàn phím.

Hình chuẩn để so là ba trang mẫu tĩnh, mở thẳng bằng trình duyệt:

- `docs/mau-giao-dien/v2/nguoi-dung.html`: màn Người dùng
- `docs/mau-giao-dien/v2/co-so-du-lieu.html`: màn Cơ sở dữ liệu
- `docs/mau-giao-dien/v2/cong-cu.html`: trang Công cụ

Ba trang đó tải font từ Google Fonts cho tiện xem; bản thật không làm vậy (mục 4). Trang Công cụ mẫu có các ô viền đứt mang nhãn "đề xuất": đó là tính năng TPAN chưa có, **bản thật không vẽ chúng**. Bố cục có tham khảo cách bày của các panel thương mại (trang công cụ chia nhóm, cột thông tin tài khoản và mức dùng, ô tìm ở đầu trang) nhưng không chép hình ảnh, biểu tượng hay màu của sản phẩm nào.

Ba trang mẫu của bản 1 (`docs/mau-giao-dien/bang-dieu-khien-*.html`, `ngan-ai-sang.html`) giữ lại để đối chiếu, không còn là hình chuẩn. Logo nằm ở `docs/logo/` (mục 11).

## 1. Ý chính

- **Bảng điều khiển vẫn là sơ đồ tuyến tàu điện.** Mỗi site là một tuyến chạy ngang. Mỗi chặng của đường đi request là một ga: DNS, TLS, web server, pool PHP, WordPress, DB. Các tuyến xếp chồng nên ga cùng loại thẳng cột với nhau. Tuyến liền nét tới ga cuối cùng còn đạt, đứt nét từ ga hỏng trở đi. Một ga hỏng trên một tuyến là lỗi của riêng site đó; cả một cột ga hỏng là lỗi của tầng dùng chung.
- **Ba màu trạng thái chỉ nói về trạng thái.** Xanh lá, vàng, đỏ không tô nút, không tô tiêu đề, không trang trí: chỗ nào có ba màu đó thì chỗ đó đang nói về tình trạng của máy.
- **Một màu nhấn cho hành động và điều hướng.** Nút chính, liên kết, mục menu đang mở dùng xanh dương `--accent`. Nhìn màu là biết thứ nào bấm được và mình đang ở đâu.
- **Nội dung nằm trong thẻ trắng trên nền xám nhạt.** Viền nhạt, góc bo. Khung không được nặng hơn nội dung: không còn ô nào viền màu chữ.
- **Chữ để đọc là sans 14 px.** Mono chỉ dành cho thứ người ta chép hoặc so từng ký tự. Không chữ nào dưới 12 px.
- **Trang ngắn.** Thứ ít xem nằm sau một tab hoặc một khối mở ra; bằng chứng đóng sẵn, trừ dòng đang lỗi.
- **Thiết kế theo dữ liệu thật.** Tên miền 36 ký tự trở lên là chuyện thường: cột co giãn, tên nằm một dòng, dài quá thì cắt bằng dấu ba chấm và có `title`.

## 2. Bốn trạng thái

Mỗi trạng thái có một hình riêng và một màu riêng. Hình khác nhau để người mù màu và bản in đen trắng vẫn đọc được; màu để người nhìn bình thường nhận ra nhanh hơn.

| Trạng thái | Hình | Kích thước | Sáng | Tối | Nét tuyến đi qua ga |
|---|---|---|---|---|---|
| đạt | chấm tròn đặc | 12 px | `#168A3A` | `#45C97A` | liền |
| cảnh báo | hình thoi (ô vuông xoay 45°) có viền | 13 px, viền 2 px | nền `#E0A100`, viền `#16181A` | nền `#F2C14E`, viền `#15171A` | liền |
| lỗi | ô vuông đặc, bên trong có dấu × | 20 px | nền `#C8320A`, dấu `#FFFFFF` | nền `#FF6B4A`, dấu `#190B07` | liền tới ga, đứt từ ga trở đi |
| chưa kiểm được | vòng tròn rỗng | 10 px, viền 2 px | `#7E848A` | `#8A9096` | đứt |

Quy tắc:

- Dấu trạng thái nào cũng có nhãn cho trình đọc màn hình, dạng "Pool PHP: lỗi".
- Chú giải bốn hình nằm ngay trên bản đồ tuyến.
- Xanh lá, vàng, đỏ không dùng cho việc gì khác: không tô nút, không tô tiêu đề, không trang trí. Ngoại lệ duy nhất là chữ của nút nguy hiểm (mục 6).
- Bấm vào một dấu trạng thái là mở bằng chứng của phép kiểm đó.
- Dấu đặt trong một dòng chữ (nút "N sự cố" trên thanh đầu trang, dòng trạng thái trong bảng) dùng cỡ nhỏ: cùng hình, thu còn khoảng 70%.
- Dấu thông tin không phải dấu trạng thái. Hiện chỉ có một: ô vuông đặc 6 px màu chữ (`--ink`) cạnh số bản ở dòng phiên bản cuối menu, nghĩa là "có bản TPAN mới" (M15). Nó không nói gì về tình trạng của máy nên không mang hình hay màu nào của bốn trạng thái, và có chữ cho trình đọc màn hình ("có bản mới").

## 3. Màu

Khai báo một lần thành biến CSS trong `web/src/styles.css`; mọi thành phần chỉ dùng biến, không viết mã màu trực tiếp.

| Biến | Sáng | Tối | Dùng cho |
|---|---|---|---|
| `--ink` | `#171A1F` | `#E9EBED` | chữ chính; phần đầy của thanh mức dùng |
| `--ink-soft` | `#2B3138` | `#CDD2D7` | chữ mục menu chưa mở, chữ trong nhãn nhỏ |
| `--muted` | `#556070` | `#A3A9AF` | chữ phụ, nhãn, đầu bảng |
| `--paper` | `#EEF1F4` | `#111316` | nền trang; nền nhãn nhỏ |
| `--surface` | `#FFFFFF` | `#1A1D21` | thẻ, thanh đầu trang, menu |
| `--soft` | `#F6F7F9` | `#20242A` | đầu bảng, chân bảng, ô tìm, hàng đang rê |
| `--hair` | `#DDE1E6` | `#2A2F36` | viền thẻ, kẻ hàng |
| `--line` | `#C5CBD3` | `#3B424B` | viền nút phụ, viền ô tìm |
| `--field` | `#7A8592` | `#737C87` | viền ô nhập, ô chọn |
| `--accent` | `#0B4FB8` | `#8FB8FF` | nút chính, mục menu đang mở, liên kết, viền focus |
| `--accent-hover` | `#083A87` | `#B9D2FF` | nút chính và liên kết lúc rê |
| `--on-accent` | `#FFFFFF` | `#0B1220` | chữ trên nền nhấn |
| `--accent-tint` | `#E7EDF8` | `#2D3645` | nền mục đang mở, ô biểu tượng |
| `--on-ink` | `#FFFFFF` | `#111316` | chữ trên nền `--ink` |
| `--pre` | `#E1E4E1` | `#101214` | nền khối bằng chứng |
| `--track` | `#D2D5D7` | `#30353A` | rãnh thanh số liệu |
| `--route` | `#171A1F` | `#B8BEC4` | nét tuyến liền |
| `--route-dash` | `#7E848A` | `#6B727A` | nét tuyến đứt |
| `--ok` / `--ok-text` | `#168A3A` / `#12752F` | `#45C97A` / `#45C97A` | hình đạt / chữ màu xanh khi buộc phải viết |
| `--warn` / `--warn-border` | `#E0A100` / `#16181A` | `#F2C14E` / `#15171A` | hình cảnh báo và viền của nó |
| `--fail` / `--on-fail` / `--fail-text` | `#C8320A` / `#FFFFFF` / `#B42A06` | `#FF6B4A` / `#190B07` / `#FF8A70` | hình lỗi, dấu × trên nó, chữ đỏ |
| `--unknown` | `#7E848A` | `#8A9096` | hình chưa kiểm được |

Thêm hai giá trị không phải màu đặc: `--scrim` (lớp phủ sau hộp thoại) và `--shadow-pop` (bóng của lớp nổi).

`--accent-tint` là màu nhấn pha 10% (sáng) và 16% (tối) trên `--surface`. Nó được viết thành mã hex cố định thay vì `color-mix()` lúc chạy, để đo được tương phản; test `TestAccentTint` kiểm lại phép pha.

Hai biến ngoài bảng của đặc tả gốc, có lý do: `--ink-soft` (trang mẫu dùng hai màu xám trung gian cho mục menu và nhãn; bản thật gộp thành một) và `--field` (viền `--line` chỉ đạt 1,63:1 trên nền thẻ, không đủ cho ranh giới của một ô nhập, WCAG 2.1 mục 1.4.11).

Tương phản đã đo (công thức WCAG 2.1; chữ cần từ 4,5:1, hình và viền ô nhập cần từ 3:1). Test `TestTuongPhan` ở `web/tuong_phan_test.go` đọc thẳng `styles.css` và chạy trong `make check`, nên đổi một màu làm rớt ngưỡng là test đỏ:

| Cặp | Sáng | Tối |
|---|---|---|
| `--ink` trên `--paper` / `--surface` / `--soft` / `--pre` / `--accent-tint` | 15,4 / 17,4 / 16,3 / 13,6 / 14,8 | 15,6 / 14,2 / 13,0 / 15,7 / 10,2 |
| `--ink-soft` trên `--surface` / `--paper` / `--soft` | 13,1 / 11,6 / 12,3 | 11,1 / 12,2 / 10,2 |
| `--muted` trên `--paper` / `--surface` / `--soft` / `--pre` | 5,6 / 6,4 / 6,0 / 5,0 | 7,8 / 7,1 / 6,6 / 7,9 |
| `--accent` trên `--surface` / `--paper` / `--soft` / `--accent-tint` | 7,4 / 6,6 / 6,9 / 6,3 | 8,4 / 9,3 / 7,8 / 6,1 |
| `--accent-hover` trên `--surface` / `--paper` / `--soft` | 10,7 / 9,4 / 10,0 | 11,1 / 12,2 / 10,2 |
| `--on-accent` trên `--accent` / `--accent-hover` | 7,4 / 10,7 | 9,3 / 12,2 |
| `--fail-text` trên `--paper` / `--surface` / `--soft` / `--pre` | 5,7 / 6,4 / 6,0 / 5,0 | 8,1 / 7,4 / 6,8 / 8,2 |
| `--ok-text` trên `--paper` / `--surface` / `--soft` | 5,1 / 5,8 / 5,4 | 8,8 / 8,0 / 7,3 |
| `--on-fail` trên `--fail` | 5,4 | 6,8 |
| `--ok` (hình) trên `--paper` / `--surface` / `--soft` | 3,9 / 4,4 / 4,1 | 8,8 / 8,0 / 7,3 |
| `--fail` (hình) trên `--paper` / `--surface` / `--soft` | 4,7 / 5,4 / 5,0 | 6,6 / 6,0 / 5,5 |
| `--unknown` (hình) trên `--paper` / `--surface` / `--soft` | 3,3 / 3,8 / 3,5 | 5,8 / 5,2 / 4,8 |
| `--warn-border` trên `--warn` | 7,9 | 10,7 |
| `--route` / `--route-dash` trên `--surface` | 17,4 / 3,8 | 9,0 / 3,5 |
| `--field` (viền ô nhập) trên `--surface` / `--soft` / `--paper` | 3,8 / 3,5 / 3,3 | 4,0 / 3,7 / 4,4 |
| `--ink` (phần đầy) trên `--track` | 11,8 | 10,4 |

Không có ngưỡng, chỉ để biết: `--line` trên `--surface` 1,6 (viền nút phụ: chữ trên nút đã nói đó là nút); `--hair` trên `--surface` 1,3 (viền thẻ, trang trí); `--warn` trên `--surface` 2,3 ở chế độ sáng (vì vậy hình thoi luôn có viền đậm).

`--ok` ở chế độ sáng chỉ đạt 3,9 đến 4,4:1, đủ cho hình nhưng không đủ cho chữ. Vì vậy chữ màu xanh dùng `--ok-text`. Thêm màu mới thì thêm cặp vào test và ghi vào bảng này.

Tailwind nhận các biến qua `@theme inline` (tài liệu của đúng phiên bản Tailwind đang cài). Bảng màu có sẵn của Tailwind bị tắt; không viết mã màu trong tên class.

## 4. Chữ

| Dùng cho | Font | Cỡ, độ đậm |
|---|---|---|
| Chữ giao diện | IBM Plex Sans | 14 px, dòng 1,45 |
| Tiêu đề màn (`h1`) | IBM Plex Sans | 24 px, 600, dòng 1,25, giãn chữ −0,01em |
| Tiêu đề khối (`h2`), tiêu đề hộp thoại | IBM Plex Sans | 16 px, 600 |
| Số liệu lớn | IBM Plex Sans | 24 px, 500, chữ số đều cột (`tabular-nums`) |
| Nhãn, đầu bảng, tên nhóm menu, chữ trong nhãn nhỏ | IBM Plex Sans | 12 px, 500, **viết thường** |
| Chữ phụ | IBM Plex Sans | 13 px; dòng phụ trong ô bảng và ô công cụ 12,5 px |
| Thứ người ta chép hoặc so từng ký tự | IBM Plex Mono | 13 px |
| Khối bằng chứng, khối lệnh, log | IBM Plex Mono | 12,5 px, dòng 1,55 |
| Tên TPAN trên thanh đầu trang | IBM Plex Mono | 18 px, 600, chữ hoa, giãn chữ 0,04em |

- **Cỡ chữ được phép:** 12, 12,5, 13, 14, 16, 18, 24 px. Không chữ nào dưới 12 px.
- **Mono chỉ cho:** đường dẫn, lệnh, tên file, giá trị cấu hình, tên DB và user hệ thống, mã job, sha, số phiên bản, tên máy. **Tên miền dùng sans.** Tên user TPAN, tên gói, bộ ký tự cũng dùng sans.
- Không còn nhãn mono chữ hoa giãn chữ. Nhãn viết như một từ thường: "Bằng chứng", "Sửa tay", "Máy chủ".
- Font nằm trong repo dưới dạng file `woff2` và được nhúng vào file chạy; giao diện không gọi ra mạng. Lấy font qua gói npm của đúng hai họ font này, chỉ nhúng phần ký tự Latin và tiếng Việt, ba độ đậm 400, 500, 600.
- Sau khi đổi cách nhúng, mở giao diện và nhìn các chuỗi có dấu ("Đường đi request", "chưa kiểm được", "Sửa tự động") ở cả hai font. Thiếu dấu ở font nào thì báo chủ dự án; không tự đổi sang font khác.

## 5. Sáng và tối

- Thuộc tính `data-theme` trên thẻ `<html>` nhận `light` hoặc `dark`. Mọi màu đi theo biến ở mục 3, nên đổi thuộc tính là đổi cả giao diện.
- Lần đầu mở: theo cài đặt của hệ điều hành (`prefers-color-scheme`). Người dùng chọn Sáng, Tối hoặc "Theo hệ điều hành" ở **menu tài khoản** trên thanh đầu trang, hoặc ở thẻ "Giao diện" của màn Cài đặt (cùng một trạng thái). Sáng và Tối được nhớ trong `localStorage` dưới khoá `tpan-theme`; "Theo hệ điều hành" xoá khoá đó.
- Thuộc tính phải được đặt **trước** khi trang vẽ lần đầu, nếu không người dùng chế độ tối sẽ thấy màn hình chớp trắng. Panel dùng `Content-Security-Policy: default-src 'self'` nên không được viết script ngay trong HTML. Cách làm: một file script nhỏ riêng (`web/public/theme.js`), nạp bằng thẻ `<script src>` thường trong `<head>`, chỉ làm đúng việc đọc `localStorage` rồi đặt `data-theme`.
- Mỗi màn hình làm xong phải được xem ở cả hai chế độ. Không có thành phần nào chỉ đúng ở một chế độ.

## 6. Thành phần

Thành phần nằm ở `web/src/components/`. Màn chỉ ghép thành phần và viết class bố cục (flex, grid, khoảng cách, bề rộng); **không viết kiểu dáng riêng trong màn**. Thiếu một thành phần thì thêm vào đây trước, rồi mới dùng.

Hình khối chung: thẻ bo 10 px, viền `--hair`, nền `--surface`, đệm 16 px. Ô điều khiển (nút, ô nhập, ô chọn) bo 8 px, cao 40 px; dưới 640 px cao 44 px để bấm được bằng ngón tay. Bóng đổ chỉ cho lớp nổi (menu thả, hộp thoại, thanh lệnh, ngăn AI, thẻ theo dõi job).

| Thành phần | Hình thức |
|---|---|
| Nút chính (`Button variant="primary"`) | nền `--accent`, chữ `--on-accent` 14 px độ đậm 500; rê thì nền `--accent-hover`. Mỗi màn một nút chính, nằm bên phải đầu màn; khối xem trước có nút chính của riêng nó |
| Nút phụ (`secondary`) | viền `--line`, nền `--surface`, chữ `--ink`; rê thì nền `--soft` |
| Nút chữ (`text`) | không viền, chữ `--accent`; rê thì nền `--accent-tint`. Dùng trong hàng bảng và chỗ cần nhẹ |
| Nút nguy hiểm (`danger`) | như nút phụ, chữ `--fail-text`. **Chỉ** cho việc không hoàn tác được (xoá hẳn, xoá user) |
| Nút biểu tượng (`IconButton`) | ô vuông 40 px không viền, biểu tượng `--muted`; luôn có nhãn đọc được |
| Thẻ (`Card`) | có thể có tiêu đề (`h2`), số đếm cạnh tiêu đề, hành động bên phải tiêu đề, chân thẻ nền `--soft` |
| Đầu màn (`ScreenHeader`) | đường dẫn (13 px, `--muted`), `h1`, một dòng tóm tắt (tối đa 72 ký tự), hành động chính bên phải, hàng nhãn nhỏ bên dưới khi cần |
| Bảng (`TableList`) | tự vẽ thẻ của nó (không bọc thêm `Card`), có thể có tiêu đề và hành động ở đầu thẻ; đầu bảng nền `--soft`, chữ 12/500 `--muted`; hàng cao 52 đến 56 px, kẻ `--hair`; số canh phải và đều cột; chân bảng nền `--soft`; khung cuộn ngang riêng |
| Chữ mono chỉ để đọc (`Code`) | trong dòng: mono 13 px; `block`: khối nền `--pre`, bo 8 px, 12,5 px dòng 1,55. Thứ người ta bấm để chép thì dùng `MonoText` |
| Cột Việc (`RowActions`) | tối đa hai nút chữ, phần còn lại vào menu ba chấm. Nút ba chấm có nhãn đọc được, dạng "Việc khác với X: a, b, c" |
| Menu thả (`Menu`) | Radix DropdownMenu, luôn `modal={false}` (bản modal chèn một thẻ `<style>` và CSP chặn); khung bo 8 px, viền `--hair`, bóng; mục cao 40 px, mục đang tới nền `--soft` |
| Tab (`Tabs`) | Radix Tabs; hàng tab kẻ dưới `--hair`; tab đang mở chữ `--accent` đậm 600 và gạch dưới 2 px. Phím mũi tên đổi tab. Tab đang mở ghi vào địa chỉ (`?tab=`) bằng `replace` |
| Nhãn nhỏ (`Tag`) | bo tròn, nền `--paper` (trên thẻ) hoặc `--surface` (trên nền trang), không viền, chữ 12 px `--ink-soft` |
| Ô mức dùng (`UsageMeter`) | "5 / ∞": số đã dùng đậm, "/ trần" màu `--muted`; có trần thì thêm thanh cao 4 px (rãnh `--track`, đầy `--ink`) |
| Thanh số liệu (`MetricBar`) | nhãn 12/500, số 24 px, một dòng phụ, thanh cao 6 px có vạch ngưỡng 80% |
| Khối mở ra (`Disclosure`) | một nút cả hàng có mũi tên và `aria-expanded`; nội dung chỉ dựng khi mở. Dùng cho bằng chứng |
| Khối bằng chứng (`EvidenceBlock`) | nền `--pre`, bo 8 px, mono 12,5 px dòng 1,55, tự xuống dòng; dòng lệnh (bắt đầu bằng `$`) đậm 600; bấm là chép toàn bộ. Output dài: chỉ vẽ 20 dòng đầu và nút "Xem hết N dòng" |
| Khối lệnh để người tự gõ (`CommandBlock`) | nền `--pre`. Đầu khối một dòng chữ `--muted` nói chạy ở máy nào, quyền gì, thư mục nào. Mỗi lệnh một hàng: `$ ` nhạt (không chép được), lệnh mono, cuối hàng nút chép (đổi sang ✓ khoảng 2 giây). Có nút "Chép hết" khi có từ hai lệnh |
| Ô nhập (`Field`, `Input`, `Select`, `Textarea`) | nhãn 14/500 nằm trên ô; ô viền `--field`, nền `--surface`; lỗi có dấu lỗi và `aria-invalid` |
| Ô chọn (`Checkbox`) | 16 px, màu nhấn; ô bị khoá có lời giải thích trong nhãn đọc và `title`, không để trống |
| Nút đoạn (`SegmentButton`) | nhóm bo 8 px viền `--line`; ô đang chọn nền `--accent-tint`, chữ `--accent`, có `aria-pressed` |
| Hộp thoại (`Dialog`) | bo 10 px, bóng, lớp phủ `--scrim`; nút làm việc là động từ của đúng việc đó |
| Trạng thái rỗng (`EmptyState`) | một câu `--muted`, kèm một liên kết hoặc nút tới việc tiếp theo |
| Hàng tuyến | cao 52 px; cột tên site co giãn 220 đến 320 px, tên **một dòng**, sans 14/500, dài thì cắt bằng dấu ba chấm và có `title`; sáu ga chia đều; cột mã HTTP rộng 76 px. Mã khác 200 có nền `--fail`, chữ `--on-fail`. Dưới hàng lỗi có một dòng chữ `--fail-text` nói hỏng ở đâu |
| Ô tìm `⌘K` | một nút trông như ô nhập (nền `--soft`, viền `--line`): kính lúp, dòng gợi ý `--muted`, phím tắt trong khung nhỏ. Mở thanh lệnh |
| Ngăn AI | khung chat nổi cỡ cố định 400 × 640 px (thấp hơn khi cửa sổ thấp) ở góc dưới bên phải, bo 10 px, có bóng. Mở bằng nút "Hỏi AI" trên thanh đầu trang; **đóng thì không còn gì nổi ở góc**. Giữ nguyên khi chuyển mục. Màn hẹp: phủ toàn màn |

Các trạng thái của thành phần bấm được:

- Rê chuột: theo bảng trên; liên kết đổi sang `--accent-hover`.
- Đang chọn bằng bàn phím: viền ngoài 2 px màu `--accent`, cách 2 px. Không bao giờ tắt viền này.
- Vô hiệu: chữ `--muted`, viền `--hair`, nền `--soft`, con trỏ không đổi thành bàn tay.
- Chữ dữ liệu bấm-là-chép (đường dẫn, mã job, sha) không phải nút: hàng bảng giữ gọn, nhưng ở màn hẹp vùng bấm tối thiểu 24 × 24 px (WCAG 2.2 bậc AA, tiêu chí 2.5.8).
- Liên kết nằm trong câu văn có gạch chân: màu nhấn so với chữ thường chỉ 2,35:1, không đủ để phân biệt bằng màu (WCAG 1.4.1). Liên kết đứng riêng (ô bảng, menu, nút) không gạch chân, gạch chân khi rê.

Cuộc trò chuyện trong ngăn AI:

- Câu của bạn là khung nền `--pre` lệch phải; phần của AI lệch trái, có nhãn "AI". Các lần AI đọc dữ liệu gập thành một dòng "Đã đọc N mục dữ liệu trên máy", bấm để mở.
- Mỗi lần AI đọc dữ liệu là một dòng: nhãn "Đọc", tên thứ được đọc, và liên kết "dữ liệu đã gửi" mở ra đúng bản đã che.
- Plan được đề xuất là một thẻ có nhãn "AI đề xuất" và nút chính "Mở bản xem trước".
- "Đi tới" là các nút phụ dẫn sang mục khác; bấm thì chuyển mục, ngăn AI vẫn mở.
- Mỗi nhãn ngữ cảnh có nút × riêng để bỏ nó khỏi câu hỏi sắp gửi.
- Chấm ở đầu ngăn là tình trạng bộ não: chấm xanh khi đã nối, vòng rỗng kèm cách nối khi chưa.
- Cuối ngăn luôn có dòng: "AI chỉ đọc và đề xuất. Máy chỉ đổi khi bạn duyệt."

Khối lệnh để người tự gõ (`CommandBlock`, dùng cho cách sửa tay `manual_fix`) khác khối bằng chứng: bằng chứng là việc tpan đã chạy, còn khối lệnh là việc người vận hành phải làm, nên:

- Đầu khối luôn nói ba điều: chạy trên máy nào (tên máy lấy từ phiên, kèm `ssh <user>@<máy>`), quyền gì (root, các lệnh đã có `sudo`), thư mục nào (mọi đường dẫn tuyệt đối nên thư mục nào cũng được). Ở chế độ demo, dòng đó nói đây là máy giả, lệnh chỉ để xem.
- Mỗi lệnh có nút chép riêng, nhãn đọc màn hình là "Chép lệnh" kèm lệnh. Bấm xong biểu tượng đổi sang ✓ rồi trở lại, và có vùng `aria-live` báo "Đã chép".
- Chữ được chép là đúng lệnh, không có `$ `. Dấu `$ ` chỉ để nhìn và không bôi đen được. Dữ liệu `manual_fix` từ API không có `sudo`; `CommandBlock` thêm vào khi hiện và khi chép (trừ lệnh đã có `sudo` hoặc dòng chú thích bắt đầu bằng `#`).
- Lệnh tpan tự chạy (ví dụ "Lệnh sẽ chạy" ở màn duyệt Plan) vẫn dùng khối bằng chứng, không dùng khối này.

Biểu tượng (`Icon`): vẽ riêng cho TPAN bằng SVG nét, khung 24, nét 1,75, đầu nét tròn. Mỗi mục menu và mỗi ô công cụ có một biểu tượng; trong nút thì chỉ dùng khi giúp nhận ra nhanh hơn (dấu cộng ở "Thêm user"). Biểu tượng luôn đi kèm chữ hoặc nhãn đọc được. Không dùng emoji.

## 7. Bố cục

Khung chung của mọi màn (trừ Đăng nhập):

- **Thanh đầu trang** trải hết bề ngang, dính trên cùng, nền `--surface`, kẻ dưới `--hair`. Từ trái: logo và tên TPAN (bấm là về trang chủ), tên máy (mono 13 px, `--muted`), ô tìm `⌘K` (tối đa 440 px). Dồn phải: nút sự cố (chỉ khi có sự cố đang mở: dấu lỗi và "N sự cố"), một nút "Hỏi AI" (chỉ admin), menu tài khoản.
- **Menu tài khoản**: nút gồm hình tròn có chữ cái đầu của tên, tên, vai. Trong menu: tên và vai; Ngôn ngữ (Tiếng Việt, English); Giao diện (Sáng, Tối, Theo hệ điều hành); Gợi ý; Đăng xuất.
- **Menu trái** rộng 232 px, nền `--surface`, kẻ phải `--hair`, dính và cuộn riêng. Mỗi mục cao 40 px, bo 8 px, có biểu tượng 18 px. Mục đang mở: nền `--accent-tint`, chữ và biểu tượng `--accent`, đậm 600, `aria-current="page"`. Số đếm nằm sát phải, 12 px `--muted`; riêng "Sự cố" khi có sự cố đang mở thì số nằm trên nền `--fail`, chữ `--on-fail`. Cuối menu là dòng phiên bản TPAN (không còn chân trang).
- Nhóm và thứ tự của menu: (không tên) Bảng điều khiển, Công cụ, Site, Cơ sở dữ liệu, SSL, Backup, Cron, Thùng rác; **Máy chủ**: Sự cố, Lịch sử, Stack, Cập nhật, Bảo mật; **AI**: Phiên AI; **Tài khoản**: Người dùng, Gói, Cài đặt.
- **Nội dung**: đệm 24 px trên, 32 px hai bên, 40 px dưới; các khối cách nhau 20 px. Bề rộng tối đa: bảng 1200 px, form 560 px, đoạn văn 72 ký tự. Mỗi màn mở bằng đầu màn.
- **Màn hẹp (dưới 1024 px)**: menu trái ẩn; thanh đầu trang có nút "Menu", bấm thì menu mở ra thành một khối ngay dưới thanh đầu trang và tự đóng khi chọn một mục. Dưới 640 px: ô tìm thu thành nút biểu tượng, nội dung đệm ngang 16 px. Cả trang không bao giờ cuộn ngang; bảng và bản đồ tuyến cuộn trong khung riêng.
- **Trang chủ theo vai**: admin vào `/` thấy Bảng điều khiển; user thường vào `/` được đưa tới Công cụ (`/tools`), và Bảng điều khiển của họ nằm ở `/dashboard`.
- Bấm chạy một thao tác thì ở lại màn đang dùng: thẻ theo dõi job ở góc dưới bên trái (lệch phải menu) báo đang chạy, xong hay lỗi, có liên kết "Xem chi tiết" tới trang job; job xong thì dữ liệu trên màn tự tải lại. Riêng Tạo site vẫn chuyển sang trang job vì mật khẩu quản trị WordPress chỉ hiện một lần ở đó. Xoá một site từ trang của site đó thì về danh sách Site.
- Bảng Site và Thùng rác có ô chọn từng hàng và ô chọn tất cả; chọn rồi thì hiện dải "Đã chọn N" với việc hàng loạt. Hộp hỏi lại liệt kê đúng các mục đã chọn; các mục chạy lần lượt, mỗi mục một job.

Từng màn:

- **Bảng điều khiển**: hai cột có xuống dòng. Cột chính: thẻ bản đồ tuyến (chú giải bốn trạng thái ở đầu thẻ; bấm ga mở bằng chứng ngay trong thẻ), thẻ Máy (CPU, RAM, đĩa, swap), thẻ TPAN tự kiểm, thẻ Lịch sử máy. Cột bên: Sự cố đang mở (thẻ gọn: tiêu đề tối đa hai dòng, lệnh cho Claude Code đóng sẵn), Cảnh báo (một thẻ, mỗi cảnh báo một dòng mở ra được), Chờ bạn duyệt. Nhãn stack (LEMP, nginx, PHP 8.3) nằm dưới tóm tắt của màn. Khối nào không có gì thì ghi một dòng, không ẩn đi.
- **Công cụ** (`/tools`): mọi việc tài khoản làm được, xếp nhóm; mỗi ô có biểu tượng, tên, một dòng mô tả, và là liên kết tới màn đó. Chỉ có ô cho thứ đã tồn tại; user thường không thấy ô của màn admin. Cột bên: Tài khoản (user, vai, gói, máy, stack, bản TPAN), Đang dùng (site, DB rời, dung lượng so với gói), Cần xem (sự cố đang mở).
- **Trang một site**: đầu màn (tên miền, nhãn web server, PHP, chủ), rồi các tab: Tổng quan (đường đi request, request một giờ qua, sự cố của site), Phép kiểm, Log, PHP, Backup, Cài đặt site. Bấm một ga thì sang tab Phép kiểm, mở đúng chặng đó. Phép kiểm cô lập gộp thành một dòng "Cô lập với N site khác: đạt (X phép thử)"; mở ra mới thấy từng cặp; có cặp lỗi thì tự mở đúng cặp đó.
- **Bảo mật**: hai thẻ, "Kiểm tra bảo mật máy" và "Cô lập giữa các site". Bằng chứng đóng sẵn trừ dòng lỗi; output dài chỉ hiện 20 dòng đầu.
- **Cơ sở dữ liệu, Người dùng**: như trang mẫu. Cột của màn Người dùng tên là "DB rời". Số cạnh mục "Cơ sở dữ liệu" trong menu đếm DB đang chạy (của site cộng DB rời).
- **Duyệt Plan** trình bày như một pull request: tóm tắt, phạm vi ảnh hưởng, file thay đổi dạng diff, lệnh sẽ chạy. Dòng thêm và dòng bớt trong diff có dấu `+` và `−` ở đầu dòng, không chỉ khác màu.
- **Cài đặt**: mỗi mục một thẻ (Mật khẩu, Phiên đang đăng nhập, Phiên bản và cập nhật, Giao diện, AI và bộ não, Agent và MCP).
- Các màn danh sách khác (Site, SSL, Backup, Cron, Thùng rác, Sự cố, Lịch sử, Gói, Phiên AI) là một bảng trong thẻ, kèm thẻ cài đặt bên dưới khi màn có cài đặt.

## 8. Giọng chữ

Chữ trên giao diện viết như một người đang nói với một người. Bản tiếng Anh theo cùng tinh thần.

- Dùng từ thường ngày. Từ nào nghe như dịch máy hoặc như văn bản hành chính thì đổi.
- Nói thẳng việc sắp xảy ra, kèm con số của nó: "Sửa tự động · xem trước 3 bước", không viết "Thực hiện khắc phục".
- Nút là động từ của đúng việc đó: "Tạo 3 site", "Gửi", "Mở bản xem trước". Không dùng "OK", "Xác nhận", "Tiếp tục" khi nói rõ hơn được.
- Lỗi nói ba điều theo thứ tự: chuyện gì xảy ra, ở đâu, làm gì tiếp. Không đổ lỗi cho người dùng.
- Không "Vui lòng", không chấm than, không viết hoa cả câu, kể cả nhãn cột và nhãn khối (bản 1 cho nhãn viết hoa; bản 2 thì không).
- Thuật ngữ mà người vận hành vẫn gọi bằng tiếng Anh thì giữ nguyên: site, request, pool, socket, reload, log.

| Không viết | Viết |
|---|---|
| Chế độ học | Gợi ý |
| Thực thi thao tác | Chạy |
| Khởi tạo site thành công | Đã tạo xong site |
| Vui lòng xác nhận để tiếp tục | Chạy 11 bước này? |
| Đã xảy ra lỗi không xác định | Bước 4 hỏng: nginx không nhận file cấu hình. Xem output bên dưới. |
| Không có dữ liệu | Chưa có site nào. Tạo site đầu tiên |
| Tiến hành khôi phục | Khôi phục |
| BẰNG CHỨNG | Bằng chứng |

### Thuật ngữ đã thống nhất

Mọi màn dùng đúng một từ cho một khái niệm. Thêm chuỗi mới thì tra bảng này trước; gặp khái niệm chưa có thì thêm một dòng vào đây cùng lúc.

| Tiếng Việt | English | Ghi chú |
|---|---|---|
| TPAN | TPAN | Tên sản phẩm, luôn viết hoa. `tpan` chữ thường chỉ cho tên lệnh, đường dẫn và tên dịch vụ (`tpan.db`, "log của tpan"). |
| Plan | plan | Tiếng Việt viết hoa như tên riêng; tiếng Anh chữ thường trong câu, và "Plan #3" khi kèm số. |
| Plan chờ duyệt | Pending plans | Ba trạng thái: chờ duyệt, đã chạy, đã từ chối / pending, applied, rejected. |
| Sự cố | Incident | Đang mở / đã đóng: open / closed. |
| Thùng rác, Xoá hẳn | Trash, Delete permanently | |
| Hoàn tác | Roll back (danh từ: rollback) | Job hỏng thì TPAN tự gỡ các bước đã chạy. "Không hoàn tác được" / "cannot be undone" chỉ dùng cho việc không lấy lại được (xoá hẳn). |
| Sửa tay, Sửa tự động | Fix by hand, Automatic fix | |
| Phép kiểm | Check | Dấu trạng thái: đạt, cảnh báo, lỗi, chưa kiểm được / pass, warning, failed, not checked. |
| Bằng chứng | Evidence | |
| Bản xem trước | Preview | Lỗi dựng bản xem trước: "Chưa dựng được bản xem trước ..." / "Could not build the preview ...". |
| Điều kiện chưa đạt | Conditions not met | |
| Chặng (ga trên bản đồ) | Stage (station) | |
| Bộ não, Ngăn AI, Phiên AI | Brain, AI drawer, AI session | |
| Cấu hình | Configuration | "Cài đặt" / "Settings" chỉ là tên màn Cài đặt và tab "Cài đặt site". |
| Việc (tên cột) | Action | Cột nút bấm của bảng. Không dùng "Thao tác". |
| Việc khác | More actions | Nhãn của nút ba chấm, luôn kèm tên đối tượng và các việc bên trong. |
| Cỡ | Size | Dung lượng file hoặc cơ sở dữ liệu. |
| Màn | screen | "màn Backup" / "the Backups screen". Tên màn viết đúng như tên trong menu. "Trang" chỉ dùng cho trang của một site, trang Công cụ hoặc trang lỗi. |
| Công cụ | Tools | Trang liệt kê mọi việc tài khoản làm được (M16). |
| Máy chủ | Server | Tên nhóm menu (bản 1: "Máy"). |
| Tổng quan | Overview | Tab đầu của trang một site. |
| DB rời | Standalone database | DB không gắn với site nào. Tên cột ở màn Người dùng. |
| Trang chủ | Home | Bảng điều khiển với admin, Công cụ với user thường. |
| Đăng xuất | Log out | |
| Cô lập giữa các site | Isolation between sites | Thẻ trên màn Bảo mật (M14): ma trận site × site, ô là dấu trạng thái, bấm ô thì thấy bằng chứng. |
| Cô lập với N site khác | Isolated from N other sites | Dòng gộp ở tab Phép kiểm của trang một site (M16). |
| Áp bản vá cô lập | Apply isolation hardening | Nút và tên Plan của `tpan security harden`. |
| Chế độ .htaccess | .htaccess mode | Ba giá trị `an-toan`, `tat`, `day-du` giữ nguyên ở cả hai bản, chữ mono. |
| Đường đi của request | Request path | Nút "Request tới site này đi đường nào" / "Which way a request to this site goes"; mỗi mắt xích là một chặng. |
| Phiên bản và cập nhật | Version and updates | Thẻ ở màn Cài đặt và màn Cập nhật (M15): bản đang chạy, nguồn tải, lượt gần nhất. |
| Kiểm bản mới | Check for a new version | Nút xem trước của việc cập nhật TPAN: chỉ hỏi nguồn tải, chưa tải và chưa thay gì. |
| Quay về bản trước | Go back to the previous version | Nút và hộp hỏi lại của `tpan self-update --rollback`. Không dùng "hạ cấp" hay "rollback" trong chữ giao diện. |
| Bấm | Press | Nút và liên kết. Không dùng "click" trong chữ giao diện. |
| Giờ giấc | `3 s`, `2 min 3 s`, `1 h 5 min`, `2 d` | Tiếng Anh viết số, cách một dấu cách, rồi đơn vị. |

Giữ nguyên tiếng Anh ở cả hai bản: site, request, pool, socket, reload, log, vhost, job, domain, stack, backup, cron, DNS, TLS, SSL, PHP, MariaDB, MCP, agent, prompt.

## 9. Không làm

- Không tô màu để trang trí; xanh lá, vàng, đỏ chỉ cho trạng thái; không thêm màu nhấn thứ hai.
- Không viền màu chữ quanh ô, nút, thẻ. Không viền dày hơn 1 px, trừ gạch dưới của tab đang mở, viền focus và viền của hình thoi cảnh báo.
- Không nền chuyển màu, không kính mờ, không hình minh hoạ. Bóng đổ chỉ cho lớp nổi.
- Không chữ dưới 12 px. Không nhãn viết hoa. Không mono cho câu văn hay tên miền.
- Không viết mã màu, cỡ chữ lạ hay kiểu dáng riêng trong một màn.
- Không hiệu ứng chuyển động trang trí. Chuyển trạng thái khi rê chuột dài không quá 120 ms; tôn trọng `prefers-reduced-motion`.
- Không khẳng định trạng thái mà không mở ra được bằng chứng.
- Không gọi ra mạng từ giao diện: không font, ảnh, script từ máy khác.

## 10. Kiểm trước khi coi một màn là xong

Phần máy kiểm được thì để máy kiểm:

1. `make check`: gồm `go test ./web/` (luật kiểu dáng: cỡ chữ, viền, mã màu, mono đúng chỗ, không gọi ra mạng, menu không modal, nhãn không viết hoa, bảng tương phản) và `make test-web` (logic thuần của giao diện). Khi đổi riêng một màn: `make kieu-dang DIR=src/screens/<màn>/,src/i18n/<màn>.ts`.
2. `make do-giao-dien NGUONG=1`: đo từng màn trên máy demo bằng trình duyệt thật. Ngưỡng cứng ở 1440 × 900: không chữ nào dưới 12 px; không ô nào viền màu chữ; danh sách Site có chữ mono không quá 15%; tab Tổng quan của trang một site không quá 1,5 màn hình và chữ mono không quá 30%; Cơ sở dữ liệu 5 hàng vừa một màn hình; Bảo mật lúc mới mở không quá 2 màn hình; tên site 36 ký tự trên bản đồ tuyến nằm một dòng; không màn nào cuộn ngang cả trang.
3. `make anh-m16`: chụp mọi màn ở 1440 × 900 (sáng, tối) và các màn chính ở 390 × 844; trên mọi màn: không lỗi console, không cuộn ngang ở 1440 và 390 px, không chữ dưới 12 px, không ô viền màu chữ, đi hết bằng phím Tab, mở bản tiếng Anh và tìm chữ bị tràn.

Phần phải nhìn bằng mắt:

4. Đặt ảnh của màn cạnh trang mẫu (`make anh-mau` chụp ba trang mẫu): khoảng cách, cỡ chữ, độ nặng của viền khớp nhau, ở cả sáng và tối.
5. Tắt màu của màn hình (chế độ xám của hệ điều hành): bốn trạng thái vẫn phân biệt được.

## 11. Tên và logo

- Tên sản phẩm viết hoa: **TPAN**. Trên giao diện nó được dựng bằng IBM Plex Mono đậm 600, giãn chữ 0,04em. Tên lệnh và đường dẫn vẫn viết thường (`tpan`).
- Dấu logo là một tuyến hình chữ T có ba ga, vẽ trên khung 32 × 32: nét ngang và nét dọc dày 4; hai ga ở hai đầu nét ngang là vòng rỗng (bán kính 3,5, viền 2, ruột cùng màu với nền đặt logo); ga cuối ở chân nét dọc là một hình đặc.
- **Ga cuối là dấu trạng thái của cả máy**, dùng đúng các hình ở mục 2: chấm tròn xanh khi mọi phép kiểm đạt, hình thoi vàng khi có cảnh báo, ô vuông đỏ khi có sự cố đang mở. Logo trên thanh đầu trang và biểu tượng trên tab trình duyệt cùng đổi theo, nên chưa mở panel đã biết máy có chuyện.
- Tài liệu, README và bản in dùng bản một màu: ga cuối là chấm tròn cùng màu với nét.
- File gốc nằm ở `docs/logo/`: `tpan.svg` (một màu), `tpan-dat.svg`, `tpan-canh-bao.svg`, `tpan-loi.svg`. Trong `web/`, logo là một component nhận trạng thái. Biểu tượng tab là các file SVG tĩnh trong `web/public/`, đổi bằng cách đổi `href` của thẻ `<link rel="icon">`; không dùng địa chỉ `data:` vì CSP của panel chặn.
- Không đặt logo lên ảnh, không đổi màu nét, không bo góc, không thêm bóng. Cỡ nhỏ nhất là 16 px.
