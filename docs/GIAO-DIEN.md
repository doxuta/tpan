# Giao diện TPAN: bản "Tuyến"

Chốt ngày 19/11/2026 sau khi so ba bản mẫu. Tài liệu này là đặc tả hình thức cho thư mục `web/`. Nó đi cùng mục 10 của `docs/KE-HOACH.md` (mục đó nói giao diện có những màn nào và vì sao; tài liệu này nói chúng trông ra sao).

Hình chuẩn để so là các trang mẫu tĩnh, mở thẳng bằng trình duyệt:

- `docs/mau-giao-dien/bang-dieu-khien-sang.html`: Bảng điều khiển (trang chủ), chế độ sáng
- `docs/mau-giao-dien/bang-dieu-khien-toi.html`: Bảng điều khiển, chế độ tối
- `docs/mau-giao-dien/ngan-ai-sang.html`: Bảng điều khiển khi đang mở ngăn AI

Logo nằm ở `docs/logo/` (mục 11).

Các trang mẫu tải font từ Google Fonts cho tiện xem. Bản thật không làm vậy (mục 4).

## 1. Ý chính

Bảng điều khiển, tức trang chủ, vẽ như sơ đồ tuyến tàu điện. Mỗi site là một tuyến chạy ngang. Mỗi chặng của đường đi request là một ga: DNS, TLS, web server, pool PHP, WordPress, DB. Các tuyến xếp chồng nên ga cùng loại thẳng cột với nhau.

- Tuyến liền nét tới ga cuối cùng còn đạt, đứt nét từ ga hỏng trở đi. Nhìn là biết request đi được tới đâu.
- Một ga hỏng trên một tuyến: lỗi của riêng site đó. Cả một cột ga hỏng: lỗi của tầng dùng chung.
- Mọi thứ không phải trạng thái đều đen trắng: chữ, nút, khung, đường kẻ. Màu chỉ xuất hiện ở dấu trạng thái, nên chỗ nào có màu thì chỗ đó đang nói về tình trạng của máy.
- Góc vuông, nét thẳng, không bóng đổ, không chuyển màu. Giao diện trông như một bản vẽ kỹ thuật, không như một trang quảng cáo.

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
- Xanh lá, vàng, đỏ không dùng cho việc gì khác: không tô nút, không tô tiêu đề, không trang trí.
- Bấm vào một dấu trạng thái là mở bằng chứng của phép kiểm đó.

## 3. Màu

Khai báo một lần thành biến CSS; mọi thành phần chỉ dùng biến, không viết mã màu trực tiếp.

```css
:root,
[data-theme="light"] {
  color-scheme: light;
  --ink: #16181A;          /* chữ chính, viền nút, nét tuyến liền */
  --paper: #ECEDEA;        /* nền trang */
  --surface: #F6F7F4;      /* nền thẻ, nút phụ, ô nhập */
  --muted: #5A6066;        /* chữ phụ, nhãn cột */
  --hair: #CDD0D2;         /* kẻ mảnh giữa các hàng */
  --card-border: #16181A;  /* viền thẻ */
  --on-ink: #FFFFFF;       /* chữ trên nền --ink (nút chính) */
  --pre: #E1E4E1;          /* nền khối bằng chứng */
  --track: #D2D5D7;        /* rãnh của thanh số liệu */
  --line: #16181A;         /* nét tuyến liền */
  --line-dash: #7E848A;    /* nét tuyến đứt */
  --link: #0B4FB8;
  --link-hover: #083A87;

  --ok: #168A3A;           /* chỉ cho hình, không cho chữ */
  --ok-text: #12752F;      /* khi buộc phải viết chữ màu xanh */
  --warn: #E0A100;
  --warn-border: #16181A;
  --fail: #C8320A;
  --on-fail: #FFFFFF;
  --fail-text: #B42A06;    /* chữ đỏ trên nền sáng */
  --unknown: #7E848A;
}

[data-theme="dark"] {
  color-scheme: dark;
  --ink: #E9EBED;
  --paper: #15171A;
  --surface: #1E2124;
  --muted: #A3A9AF;
  --hair: #34393E;
  --card-border: #6B727A;
  --on-ink: #15171A;
  --pre: #101214;
  --track: #30353A;
  --line: #B8BEC4;
  --line-dash: #6B727A;
  --link: #8FB8FF;
  --link-hover: #B9D2FF;

  --ok: #45C97A;
  --ok-text: #45C97A;
  --warn: #F2C14E;
  --warn-border: #15171A;
  --fail: #FF6B4A;
  --on-fail: #190B07;
  --fail-text: #FF8A70;
  --unknown: #8A9096;
}
```

Tương phản đã đo (công thức WCAG 2.1; chữ cần từ 4,5:1, hình cần từ 3:1):

| Cặp | Sáng | Tối |
|---|---|---|
| `--ink` trên `--paper` | 15,1 | 15,0 |
| `--muted` trên `--paper` / `--surface` / `--pre` | 5,4 / 5,9 / 5,0 | 7,6 / 6,8 |
| `--link` trên `--paper` / `--surface` | 6,3 / 6,9 | 9,0 / 8,1 |
| `--fail-text` trên `--paper` / `--surface` | 5,5 / 6,0 | 7,8 / 7,0 |
| `--on-fail` trên `--fail` | 5,4 | 6,8 |
| `--ok-text` trên `--paper` / `--surface` | 4,9 / 5,4 | 8,5 / 7,6 |
| `--ok` (hình) trên `--paper` | 3,8 | 8,5 |
| `--unknown`, `--line-dash` (hình) trên `--paper` | 3,2 | 5,6 và 3,7 |

Bản sáng được làm dịu ngày 19/11/2026 theo ý chủ dự án ("giao diện sáng hơi chói"): nền trang từ `#F6F7F5` xuống `#ECEDEA`, nền thẻ từ trắng tinh `#FFFFFF` xuống `#F6F7F4`, các màu đi kèm (`--hair`, `--pre`, `--track`) đậm theo, và xám "chưa kiểm được" đậm thành `#7E848A` để vòng rỗng vẫn đạt 3:1 trên nền mới. Màu trạng thái và bản tối không đổi.

`--ok` ở chế độ sáng chỉ đạt 3,8:1 trên nền trang, đủ cho hình nhưng không đủ cho chữ. Vì vậy chữ màu xanh dùng `--ok-text`. Thêm màu mới thì đo lại và ghi vào bảng này.

Đưa các biến trên vào Tailwind theo tài liệu của đúng phiên bản Tailwind đang cài (phần khai báo theme bằng biến CSS). Không dùng bảng màu có sẵn của Tailwind và không viết mã màu trong tên class.

## 4. Chữ

| Dùng cho | Font | Cỡ, độ đậm |
|---|---|---|
| Chữ giao diện | IBM Plex Sans | 14 px, dòng 1,45 |
| Tiêu đề màn (`h1`) | IBM Plex Sans | 30 px, 600, giãn chữ −0,015em |
| Tiêu đề khối (`h2`) | IBM Plex Sans | 16 px, 600 |
| Tiêu đề thẻ sự cố (`h3`) | IBM Plex Sans | 17 px, 600 |
| Số liệu lớn | IBM Plex Sans | 24 px, 500, chữ số đều cột (`tabular-nums`) |
| Dữ liệu kỹ thuật: domain, đường dẫn, lệnh, mã HTTP, mã job | IBM Plex Mono | 12 đến 13 px |
| Nhãn cột, nhãn khối (SITE, HTTP, BẰNG CHỨNG) | IBM Plex Mono | 11 px, chữ hoa, giãn chữ 0,08em, màu `--muted` |
| Tên TPAN trên thanh đầu trang | IBM Plex Mono | 22 px, 600, chữ hoa, giãn chữ 0,04em |

- Font nằm trong repo dưới dạng file `woff2` và được nhúng vào file chạy; giao diện không gọi ra mạng. Lấy font qua gói npm của đúng hai họ font này, đọc tài liệu của gói để chỉ nhúng phần ký tự Latin và tiếng Việt, ba độ đậm 400, 500, 600.
- Sau khi nhúng, mở giao diện và nhìn các chuỗi có dấu ("Đường đi request", "chưa kiểm được", "Sửa tự động") ở cả hai font. Thiếu dấu ở font nào thì báo chủ dự án; không tự đổi sang font khác.
- Chữ mono dùng cho thứ người ta sẽ chép hoặc so từng ký tự. Câu văn thường không dùng mono.

## 5. Sáng và tối

- Thuộc tính `data-theme` trên thẻ `<html>` nhận `light` hoặc `dark`. Mọi màu đi theo biến ở mục 3, nên đổi thuộc tính là đổi cả giao diện.
- Lần đầu mở: theo cài đặt của hệ điều hành (`prefers-color-scheme`). Sau khi người dùng bấm nút Sáng/Tối trên thanh đầu trang: nhớ lựa chọn trong `localStorage` dưới khoá `tpan-theme`. Trang Cài đặt có thêm lựa chọn "Theo hệ điều hành" để xoá khoá đó.
- Thuộc tính phải được đặt **trước** khi trang vẽ lần đầu, nếu không người dùng chế độ tối sẽ thấy màn hình chớp trắng. Panel dùng `Content-Security-Policy: default-src 'self'` nên không được viết script ngay trong HTML. Cách làm: một file script nhỏ riêng (`web/public/theme.js`), nạp bằng thẻ `<script src>` thường trong `<head>`, chỉ làm đúng việc đọc `localStorage` rồi đặt `data-theme`.
- Mỗi màn hình làm xong phải được xem ở cả hai chế độ. Không có thành phần nào chỉ đúng ở một chế độ.

## 6. Thành phần

Tất cả đều góc vuông, viền 1 px, không bóng đổ.

| Thành phần | Hình thức |
|---|---|
| Nút phụ | cao 40 px, đệm ngang 14 px, viền `--ink`, nền `--surface`, chữ `--ink` 14 px độ đậm 500 |
| Nút chính | như nút phụ nhưng nền `--ink`, chữ `--on-ink`. Mỗi khối chỉ có một nút chính |
| Nút đoạn (VI/EN, Sáng/Tối) | các ô cao 40 px dính liền nhau, chữ mono 12 px; ô đang chọn nền `--ink` chữ `--on-ink`, có `aria-pressed` |
| Nhãn (LEMP, nginx 1.24) | chữ mono 12 px, viền `--ink`, đệm 2 px 8 px, không nền |
| Thẻ | viền `--card-border`, nền `--surface`, đệm 18 px. Thẻ sự cố có thêm vạch trên dày 4 px màu `--fail` |
| Khối bằng chứng | nền `--pre`, chữ mono 12 px, dòng 1,55, tự xuống dòng; dòng lệnh (bắt đầu bằng `$`) đậm 600 |
| Khối lệnh để người tự gõ | nền `--pre`. Đầu khối một dòng chữ `--muted` nói chạy ở máy nào, quyền gì, thư mục nào. Mỗi lệnh một hàng: `$ ` nhạt (không chép được), lệnh mono 12 px, cuối hàng nút biểu tượng chép (đổi sang ✓ khoảng 2 giây). Góc khối có nút phụ nhỏ "Chép hết" khi có từ hai lệnh |
| Hàng tuyến | cao 44 px; cột tên site rộng 180 px (mono, là liên kết tới trang site); sáu ga chia đều; cột mã HTTP rộng 76 px. Mã khác 200 có nền `--fail`, chữ `--on-fail`. Dưới hàng lỗi có một dòng chữ `--fail-text` nói hỏng ở đâu, từ lúc nào |
| Thanh số liệu | nhãn mono 11 px, số 24 px, một dòng phụ, rồi thanh cao 6 px: nền `--track`, phần đầy `--ink`, vạch ngưỡng 80% màu `--muted` |
| Hàng lịch sử | mã job, giờ (mono, `--muted`), việc và đối tượng, kết quả (`--muted`), liên kết "bằng chứng" và "hoàn tác"; kẻ trên `--hair` |
| Ô lệnh `⌘K` | một nút trông như ô nhập: biểu tượng kính lúp, dòng gợi ý màu `--muted`, phím tắt trong khung nhỏ |
| Ngăn AI | một thẻ rộng chừng 400 px ở cột bên, giữ nguyên khi chuyển mục. Từ trên xuống: đầu ngăn (tên, tình trạng bộ não, nút đóng); "Ngữ cảnh gửi kèm" gồm các nhãn bỏ được và dòng "Bạn vừa đi qua"; cuộc trò chuyện; ô hỏi tiếp. Hình chuẩn: `ngan-ai-sang.html` |
| Thanh đầu trang | logo và tên TPAN (bấm là về Bảng điều khiển), tên máy, các nhãn stack, rồi dồn phải: ô lệnh, VI/EN, Sáng/Tối, công tắc "Gợi ý". Kẻ dưới dày 2 px màu `--ink` |
| Menu chính | cột bên trái rộng 192 px, kẻ phải `--hair`. Mỗi mục cao 40 px, chữ 14 px. Mục đang mở có vạch trái 3 px màu `--ink`, nền `--surface`, chữ đậm 600 và `aria-current="page"`. Số đếm nằm sát phải, chữ mono 12 px màu `--muted`; riêng "Sự cố" khi có sự cố đang mở thì số nằm trên nền `--fail`, chữ `--on-fail`. Tên nhóm (MÁY, AI, TÀI KHOẢN) là nhãn mono 11 px |

Các trạng thái của thành phần bấm được:

- Rê chuột: nút phụ đổi nền sang `--pre`; liên kết đổi sang `--link-hover`.
- Đang chọn bằng bàn phím: viền ngoài 2 px màu `--link`, cách 2 px. Không bao giờ tắt viền này.
- Vô hiệu: chữ `--muted`, viền `--hair`, con trỏ không đổi thành bàn tay.
- Ở màn hẹp dưới 640 px, nút cao 44 px để bấm được bằng ngón tay.
- Chữ dữ liệu bấm-là-chép (domain, đường dẫn, mã job, sha) không phải nút: hàng bảng giữ gọn, nhưng ở màn hẹp vùng bấm tối thiểu 24 × 24 px (WCAG 2.2 bậc AA, tiêu chí 2.5.8). Mức 44 px là bậc AAA, chỉ áp cho nút.

Cuộc trò chuyện trong ngăn AI:

- Trình bày như một bản ghi, không dùng bong bóng chat. Mỗi đoạn mở bằng một nhãn mono: "BẠN", "AI ĐÃ ĐỌC", "AI".
- Mỗi lần AI đọc dữ liệu là một dòng: nhãn "ĐỌC", tên thứ được đọc, và liên kết "dữ liệu đã gửi" mở ra đúng bản đã che.
- Plan được đề xuất là một khung viền `--ink` có nhãn "AI ĐỀ XUẤT" và nút chính "Mở bản xem trước".
- "Đi tới" là các nút phụ dẫn sang mục khác; bấm thì chuyển mục, ngăn AI vẫn mở.
- Mỗi nhãn ngữ cảnh có nút × riêng để bỏ nó khỏi câu hỏi sắp gửi.
- Chấm ở đầu ngăn là tình trạng bộ não: chấm xanh khi đã nối, vòng rỗng kèm cách nối khi chưa.
- Cuối ngăn luôn có dòng: "AI chỉ đọc và đề xuất. Máy chỉ đổi khi bạn duyệt."

Khối lệnh để người tự gõ (`CommandBlock`, dùng cho cách sửa tay `manual_fix`) khác khối bằng chứng: bằng chứng là việc tpan đã chạy, còn khối lệnh là việc người vận hành phải làm, nên:

- Đầu khối luôn nói ba điều: chạy trên máy nào (tên máy lấy từ phiên, kèm `ssh <user>@<máy>`), quyền gì (root, các lệnh đã có `sudo`), thư mục nào (mọi đường dẫn tuyệt đối nên thư mục nào cũng được). Ở chế độ demo, dòng đó nói đây là máy giả, lệnh chỉ để xem.
- Mỗi lệnh có nút chép riêng, nhãn đọc màn hình là "Chép lệnh" kèm lệnh. Bấm xong biểu tượng đổi sang ✓ rồi trở lại, và có vùng `aria-live` báo "Đã chép".
- Chữ được chép là đúng lệnh, không có `$ `. Dấu `$ ` chỉ để nhìn và không bôi đen được. Dữ liệu `manual_fix` từ API không có `sudo`; `CommandBlock` thêm vào khi hiện và khi chép (trừ lệnh đã có `sudo` hoặc dòng chú thích bắt đầu bằng `#`).
- Lệnh tpan tự chạy (ví dụ "Lệnh sẽ chạy" ở màn duyệt Plan) vẫn dùng khối bằng chứng, không dùng khối này.

Biểu tượng: vẽ bằng SVG nét, khung 24, nét 2, đầu nét tròn. Chỉ dùng khi chữ không đủ (kính lúp, dấu ×, dấu ✓). Không dùng emoji.

## 7. Bố cục

Khung chung của mọi màn:

- Khung trang rộng tối đa 1440 px, đệm ngang 32 px. Trên cùng là thanh đầu trang. Bên dưới là một hàng hai phần cách nhau 32 px: menu chính (192 px) và nội dung.
- Hàng đó dùng flex có xuống dòng (menu `flex: 1 1 192px`, nội dung `flex: 999 1 560px`). Màn hẹp thì menu nằm trên, nội dung nằm dưới; menu không bao giờ dính cố định.
- Mỗi màn mở đầu bằng tên màn (`h1`) và một dòng tóm tắt. Thứ tự và tên các mục của menu ghi ở mục 10 của KE-HOACH.md.

Bảng điều khiển (trang chủ):

- Nội dung chia hai cột bằng flex có xuống dòng: cột chính (`flex: 999 1 560px`) và cột bên (`flex: 1 1 372px`), cách nhau 40 px. Màn hẹp thì cột bên tự rơi xuống dưới.
- Cột chính, từ trên xuống: tên màn "Bảng điều khiển" và dòng tóm tắt; khối "Đường đi request của từng site" kèm chú giải bốn trạng thái; bản đồ tuyến; "Máy" (CPU, RAM, đĩa, swap); "Lịch sử máy".
- Cột bên, từ trên xuống: "Sự cố đang mở"; "Cảnh báo"; "Chờ bạn duyệt". Khối nào không có gì thì ghi một dòng "Không có", không ẩn đi.
- Bản đồ tuyến có bề rộng tối thiểu 700 px và nằm trong một khung cuộn ngang riêng; cả trang không bao giờ cuộn ngang.
- Thẻ sự cố luôn có ba nút theo thứ tự: "Sửa tự động · xem trước N bước" (nút chính), "Sửa tay", "Hỏi AI".
- Khi ngăn AI mở, nó nằm trên cùng của cột bên; ba khối kia xếp xuống dưới nó. Chuyển sang màn khác thì ngăn vẫn ở cột bên của màn đó.

Các màn khác dùng lại đúng các thành phần ở mục 6. Trang Site vẽ một tuyến duy nhất, ga to hơn và có tên ga bên dưới. Màn duyệt Plan trình bày như một pull request: tóm tắt, phạm vi ảnh hưởng, file thay đổi dạng diff, lệnh sẽ chạy. Dòng thêm và dòng bớt trong diff có dấu `+` và `−` ở đầu dòng, không chỉ khác màu. Các màn danh sách (Site, Cơ sở dữ liệu, Cron, Backup, Thùng rác) là bảng kẻ mảnh như "Lịch sử máy": không thẻ, không màu nền xen kẽ.

## 8. Giọng chữ

Chữ trên giao diện viết như một người đang nói với một người. Bản tiếng Anh theo cùng tinh thần.

- Dùng từ thường ngày. Từ nào nghe như dịch máy hoặc như văn bản hành chính thì đổi.
- Nói thẳng việc sắp xảy ra, kèm con số của nó: "Sửa tự động · xem trước 3 bước", không viết "Thực hiện khắc phục".
- Nút là động từ của đúng việc đó: "Tạo 3 site", "Gửi", "Mở bản xem trước". Không dùng "OK", "Xác nhận", "Tiếp tục" khi nói rõ hơn được.
- Lỗi nói ba điều theo thứ tự: chuyện gì xảy ra, ở đâu, làm gì tiếp. Không đổ lỗi cho người dùng.
- Không "Vui lòng", không chấm than, không viết hoa cả câu. Nhãn cột là ngoại lệ duy nhất được viết hoa.
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
| Sửa tay, Sửa tự động | Fix by hand, Automatic fix | Nhãn mono: SỬA TAY / FIX BY HAND. |
| Phép kiểm | Check | Dấu trạng thái: đạt, cảnh báo, lỗi, chưa kiểm được / pass, warning, failed, not checked. |
| Bằng chứng | Evidence | |
| Bản xem trước | Preview | Lỗi dựng bản xem trước: "Chưa dựng được bản xem trước ..." / "Could not build the preview ...". |
| Điều kiện chưa đạt | Conditions not met | |
| Chặng (ga trên bản đồ) | Stage (station) | |
| Bộ não, Ngăn AI, Phiên AI | Brain, AI drawer, AI session | |
| Cấu hình | Configuration | "Cài đặt" / "Settings" chỉ là tên màn Cài đặt. |
| Việc (tên cột) | Action | Cột nút bấm của bảng. Không dùng "Thao tác". |
| Cỡ | Size | Dung lượng file hoặc cơ sở dữ liệu. |
| Màn | screen | "màn Backup" / "the Backups screen". Tên màn viết đúng như tên trong menu. "Trang" chỉ dùng cho trang của một site hoặc trang lỗi. |
| Đăng xuất | Log out | |
| Bấm | Press | Nút và liên kết. Không dùng "click" trong chữ giao diện. |
| Giờ giấc | `3 s`, `2 min 3 s`, `1 h 5 min`, `2 d` | Tiếng Anh viết số, cách một dấu cách, rồi đơn vị. |

Giữ nguyên tiếng Anh ở cả hai bản: site, request, pool, socket, reload, log, vhost, job, domain, stack, backup, cron, DNS, TLS, SSL, PHP, MariaDB, MCP, agent, prompt.

## 9. Không làm

- Không nền chuyển màu, không bóng đổ, không kính mờ, không bo góc thẻ.
- Không tô màu để trang trí; không thêm màu nhấn thứ năm.
- Không dùng thẻ có vạch màu bên trái, không lưới ô số liệu có biểu tượng, không hình minh hoạ.
- Không hiệu ứng chuyển động trang trí. Chuyển trạng thái khi rê chuột dài không quá 120 ms; tôn trọng `prefers-reduced-motion`.
- Không khẳng định trạng thái mà không mở ra được bằng chứng.

## 10. Kiểm trước khi coi một màn là xong

1. Đặt cạnh trang mẫu, ở cả sáng và tối: khoảng cách, cỡ chữ, độ dày nét khớp nhau.
2. Chỉ dùng bàn phím đi hết màn; lúc nào cũng thấy mình đang ở đâu.
3. Thu cửa sổ còn 390 px: không cuộn ngang cả trang, nút vẫn bấm được.
4. Đổi sang tiếng Anh: không chuỗi nào bị cắt hay tràn.
5. Tắt màu của màn hình (chế độ xám của hệ điều hành): bốn trạng thái vẫn phân biệt được.

## 11. Tên và logo

- Tên sản phẩm viết hoa: **TPAN**. Trên giao diện nó được dựng bằng IBM Plex Mono đậm 600, giãn chữ 0,04em. Tên lệnh và đường dẫn vẫn viết thường (`tpan`).
- Dấu logo là một tuyến hình chữ T có ba ga, vẽ trên khung 32 × 32: nét ngang và nét dọc dày 4; hai ga ở hai đầu nét ngang là vòng rỗng (bán kính 3,5, viền 2, ruột cùng màu với nền đặt logo); ga cuối ở chân nét dọc là một hình đặc.
- **Ga cuối là dấu trạng thái của cả máy**, dùng đúng các hình ở mục 2: chấm tròn xanh khi mọi phép kiểm đạt, hình thoi vàng khi có cảnh báo, ô vuông đỏ khi có sự cố đang mở. Logo trên thanh đầu trang và biểu tượng trên tab trình duyệt cùng đổi theo, nên chưa mở panel đã biết máy có chuyện.
- Tài liệu, README và bản in dùng bản một màu: ga cuối là chấm tròn cùng màu với nét.
- File gốc nằm ở `docs/logo/`: `tpan.svg` (một màu), `tpan-dat.svg`, `tpan-canh-bao.svg`, `tpan-loi.svg`. Trong `web/`, logo là một component nhận trạng thái. Biểu tượng tab là các file SVG tĩnh trong `web/public/`, đổi bằng cách đổi `href` của thẻ `<link rel="icon">`; không dùng địa chỉ `data:` vì CSP của panel chặn.
- Không đặt logo lên ảnh, không đổi màu nét, không bo góc, không thêm bóng. Cỡ nhỏ nhất là 16 px.
