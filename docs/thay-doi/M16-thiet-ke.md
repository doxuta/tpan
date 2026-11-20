# M16: bản thiết kế (giao diện bản 2, trang Công cụ)

Viết ngày 21/11/2026, trước khi viết mã, theo khuôn tám phần của `agent-dev/LO-TRINH.md`. Đặc tả gốc là `agent-dev/TU-CHAY-M16.md`; hình chuẩn là ba trang tĩnh ở `docs/mau-giao-dien/v2/` (Người dùng, Công cụ, Cơ sở dữ liệu). File này nói **làm thế nào**, tới mức thợ không phải đoán. Cuối mốc, phần còn giá trị lâu dài được gộp vào `docs/GIAO-DIEN.md` (bản 2) và mục 10 của `docs/KE-HOACH.md`.

Mã nền hiện tại để đối chiếu: nhánh `moc/M16` tại commit `4269cc1` (bản 1, "Tuyến"). Giao diện nằm ở `web/src` (React 19, TypeScript 7, Tailwind 4, react-router 8, TanStack Query 5, Radix Dialog 1.3).

## 1. Vấn đề và phạm vi

### Vấn đề

Chủ dự án dùng bản thật và thấy khó nhìn. Số đo trên máy thật và nguyên nhân nằm ở đầu `agent-dev/TU-CHAY-M16.md`; phiên này đo lại được đúng các hiện tượng đó trên máy demo và trên máy ảo (mục 2, T1 và T3). Bảy nguyên nhân, đều là hệ quả của đặc tả bản 1:

1. Chữ phải đọc là mono 11 đến 13 px.
2. Trang dài một cột (Bảo mật 6,1 màn hình, trang một site 3,8 đến 4,3).
3. Lặp chữ: phép kiểm cô lập in hai dòng cho mỗi site khác.
4. Nút nặng: mỗi hàng của màn Cơ sở dữ liệu có 5 nút viền đen.
5. Khung nặng hơn nội dung: mỗi màn 16 đến 58 ô viền màu chữ.
6. Thiết kế theo dữ liệu mẫu: tên site 36 ký tự xuống 2 đến 3 dòng.
7. Số lệch: menu ghi số site ở mục "Cơ sở dữ liệu"; cột "DB" của màn Người dùng chỉ đếm DB rời; nút "Hỏi AI" có hai nơi; ô chọn trống ở hàng của chính mình.

### Làm

- Bảng màu, chữ, hình khối, thành phần và khung trang bản 2 (mục 3.1 đến 3.5).
- Đổi mọi màn sang thành phần bản 2; ba màn có đổi bố cục: Bảng điều khiển, trang một site (chia tab), Bảo mật (chia khối, bằng chứng đóng sẵn).
- Trang **Công cụ** (`/tools`, mới). User thường: trang chủ là Công cụ.
- Một trường mới ở API tổng quan: `db_count` (mục 3.8). Đây là thay đổi API duy nhất.
- Dữ liệu demo thêm ba site tên dài (mục 3.9).
- Đồ nghề: script đo, bộ ảnh M16, e2e bước 19 (mục 3.10).
- Tài liệu: `docs/GIAO-DIEN.md` bản 2, mục 10 của KE-HOACH, file giải thích, ảnh README.

### Không làm

- Không đổi API hay hành vi của thao tác nào (trừ `db_count`). Không thêm bảng, migration, setting, lệnh CLI, công cụ MCP, phép kiểm health, Plan hay Step. Khoảng migration 1600 đến 1699 để trống.
- Không thêm tính năng. Trang Công cụ chỉ có ô cho thứ **đã tồn tại**: các ô "đề xuất" viền đứt trong trang mẫu không vào bản thật.
- Không đổi logo, bốn dấu trạng thái (hình và màu), bản đồ tuyến, giọng chữ, hai ngôn ngữ, sáng và tối, font IBM Plex nhúng sẵn, CSP `default-src 'self'`, cách dùng bàn phím.
- Không gọi ra mạng từ giao diện. Không chép hình ảnh, biểu tượng hay màu của sản phẩm nào: biểu tượng vẽ riêng.
- Không thay Radix Dialog (hộp thoại hiện có giữ nguyên hành vi, kể cả một dòng CSP ở console lúc mở hộp thoại, đã ghi ở mục 11 của KE-HOACH).

Một chỗ nằm sát ranh giới "không thêm tính năng", ghi rõ để người duyệt biết: ô tìm `⌘K` của trang mẫu ghi "Tìm màn, site hoặc gõ lệnh". Thanh lệnh bản 1 chưa tìm được site. Bản 2 thêm các dòng "Site: <tên miền>" vào thanh lệnh (đọc từ dữ liệu tổng quan đã có trong bộ nhớ, không gọi API mới), để chữ trên ô tìm nói đúng. Lý do: máy 50 site thì gõ tên để tới một site là việc người ta chờ ở một ô tìm.

## 2. Nghiên cứu

Mọi thứ thử được đều đã thử; script và output ở `docs/bang-chung/M16-nghien-cuu/`. Ngày thử: 21/11/2026. Bản phần mềm: Chrome trên Mac (bản cài sẵn, chạy không giao diện qua Chrome DevTools Protocol), Node 22.23.0, Go 1.27.4, `@radix-ui/react-dropdown-menu` 2.2.1, `@radix-ui/react-tabs` 1.2.1, `@radix-ui/react-menu` 2.2.1, máy ảo `tpan-m16` Ubuntu 24.04.

### T1. Số đo gốc trên máy demo (bản 1, có ba site tên dài)

Script đo của chủ dự án (`agent-dev/M16-do-giao-dien.js`) được đưa thành `scripts/anh-readme/do-giao-dien.js` và chạy bằng Chrome của script chụp (`-bo m16-do`). Các trường cũ giữ nguyên cách tính; thêm cỡ chữ nhỏ nhất, số dòng của tên site, đáy hàng thứ năm của bảng. Ba site tên dài được tạo qua API của máy demo ngay trước khi đo (bản 1 chưa có sẵn chúng). Cửa sổ 1440 × 900, sáng, tiếng Việt. Output: `t1-do-ban-1-demo.ket-qua.txt`.

| Màn | Trang dài | Chữ từ 13 px trở xuống | Chữ mono | Chỗ bấm được | Ô viền màu chữ | Cỡ chữ nhỏ nhất | Tên site dài nhất |
|---|---|---|---|---|---|---|---|
| Bảng điều khiển | 3,0 màn hình | 61% | 26% | 60 | 43 | 11 px | 2 dòng |
| Site (danh sách) | 1,1 | 48% | 48% | 7 | 27 | 11 px | 1 dòng |
| Trang một site | 4,3 | 71% | 68% | 48 | 26 | 11 px | |
| Cơ sở dữ liệu | 2,2 | 68% | 33% | 51 | 55 | 11 px | 3 dòng |
| Bảo mật | 6,1 | 46% | 40% | 94 | 28 | 11 px | |
| Cài đặt | 2,9 | 39% | 33% | 23 | 29 | 11 px | |
| Người dùng | 1,1 | 25% | 22% | 16 | 27 | 11 px | |

### T2. Radix dưới CSP thật của panel

Trang tạm (`t2-radix-csp.trang-thu.tsx.txt`) được build vào máy demo (server gửi đúng header `Content-Security-Policy: default-src 'self'; frame-ancestors 'none'`), Chrome gõ phím thật qua DevTools. Output: `t2-radix-csp.ket-qua.txt`.

| Thử | Kết quả |
|---|---|
| `DropdownMenu` với `modal={false}`: Enter mở, mũi tên xuống đổi mục (bỏ qua mục bị khoá), Enter chọn, Escape đóng, focus về nút; mở bằng chuột | chạy đúng; **0 lỗi console, 0 vi phạm CSP**; `body` không bị gắn `data-scroll-locked`. Vị trí menu do Radix đặt bằng `style` qua CSSOM (CSP không chặn) |
| `DropdownMenu` mặc định (modal) | mở được, nhưng trình duyệt báo **một vi phạm CSP** (`style-src-elem`, thẻ `<style>` của `react-remove-scroll`), y như Radix Dialog hiện nay |
| `Tabs`: mũi tên phải, End | đổi tab đúng, `aria-selected` đúng; khối của tab không mở **không có con trong DOM** (nội dung bị gỡ khi rời tab); 0 lỗi console |
| `color-mix()` qua Tailwind | trình duyệt trả `color(srgb …)`, không phải `rgb()`: khó so và khó đo tương phản |
| `<details>` | mở bằng chuột được, 0 lỗi |

Rút ra: (a) mọi menu thả của bản 2 dùng `modal={false}`, gói trong một thành phần duy nhất để không ai quên; (b) màu pha (`--accent-tint`) viết thành mã hex cố định, có test kiểm lại phép pha; (c) nội dung một tab bị gỡ khi rời tab, nên thứ cần sống qua lần đổi tab (ga đang chọn) phải nằm ở component cha.

Ghi thêm về đồ nghề: Radix dời focus bằng `setTimeout` sau phím mũi tên. Script phải chờ một nhịp sau mỗi phím rồi mới đọc trạng thái (lần chạy đầu đọc ngay nên tưởng mũi tên không chạy).

### T3. Số đo gốc trên máy ảo (bản 1, panel thật)

`tpan-m16` từ snapshot sạch: `make vm-deploy`, `tpan setup --yes`, `tpan stack install --web nginx --yes`, năm site (hai tên ngắn, ba tên dài). Chrome trên Mac mở `https://<máy ảo>:8443` (chứng chỉ tự ký, `--ignore-certificate-errors`), đăng nhập, đo. Output: `t3-do-ban-1-may-ao.ket-qua.txt` (không có IP, không có mật khẩu).

| Màn | Trang dài | Chữ từ 13 px trở xuống | Chữ mono | Chỗ bấm được | Ô viền màu chữ |
|---|---|---|---|---|---|
| Bảng điều khiển | 3,6 màn hình | 68% | 14% | 56 | 58 |
| Site (danh sách) | 1,1 | 48% | 48% | 5 | 23 |
| Trang một site | 3,8 | 29% | 20% | 44 | 25 |
| Cơ sở dữ liệu | 1,4 | 66% | 31% | 31 | 38 |
| Bảo mật | 6,1 | 63% | 57% | 71 | 27 |
| Cài đặt | 3,6 | 45% | 29% | 29 | 31 |
| Người dùng | 1,1 | 30% | 31% | 4 | 16 |

Hai điều rút ra ngoài con số: (a) đường chạy "Chrome của script chụp → panel thật trên máy ảo → bắt lỗi console" dùng được cho e2e bước 19; (b) Bảng điều khiển trên máy thật dài hơn demo vì mỗi site chưa có DNS và SSL sinh hai thẻ cảnh báo (năm site là mười thẻ): khối "Cảnh báo" phải là danh sách gọn, không phải mỗi cảnh báo một thẻ.

### T4. Tương phản của bảng màu bản 2

`t4-tuong-phan.go` tính theo công thức WCAG 2.1 cho 53 cặp ở mỗi chế độ. Output: `t4-tuong-phan.ket-qua.txt`. Với giá trị "điểm xuất phát" của chế độ tối trong đặc tả: **0 cặp chữ hay hình không đạt** ở cả hai chế độ, nên không phải chỉnh. Con số thấp nhất: `--unknown` (hình) trên `--paper` sáng 3,33; `--route-dash` trên `--surface` tối 3,47; `--muted` trên `--pre` sáng 4,98.

Một điều đặc tả chưa tính: `--line` (`#C5CBD3`) trên `--surface` chỉ 1,63:1. Viền nút phụ thì không sao (chữ trên nút nói đó là nút). Nhưng viền **ô nhập** là thứ duy nhất cho biết ô nằm ở đâu, và WCAG 2.1 mục 1.4.11 đòi 3:1. Vì vậy thêm biến `--field` cho viền ô nhập, ô chọn (sáng `#7A8592` đạt 3,75 trên `--surface`; tối `#737C87` đạt 4,00).

### T5. Test logic TypeScript bằng Node

Repo chưa có trình chạy test cho mã giao diện (chỉ `tsc --noEmit`). Logic gộp phép kiểm cô lập (mục 3.7) cần test có ca "một cặp lỗi", mà máy demo không có cặp lỗi nào. Thử: `node --test` của Node 22.23 chạy thẳng file `.ts` (Node tự bỏ phần kiểu), miễn là file được test chỉ `import type` và file test ghi đuôi `.ts` trong đường dẫn import. Output: `t5-node-test.ket-qua.txt` (1 test đạt, 65 ms). Không cần thêm thư viện nào. CI dùng Node 22.

### Các cách đã cân nhắc

| Việc | Chọn | Không chọn, vì sao |
|---|---|---|
| Menu thả, tab | Radix `react-dropdown-menu`, `react-tabs` (đã thử ở T2) | Tự viết: phải tự lo vai ARIA, focus, phím mũi tên, đóng khi bấm ngoài. `<select>` làm menu việc: không đặt được nhãn đọc cho từng hàng |
| Khối mở ra | Nút có `aria-expanded` và nội dung chỉ dựng khi mở (kiểu đang dùng ở `CheckItem`) | `<details>`: nội dung luôn nằm trong DOM, 98 dòng bằng chứng của 50 site vẫn bị dựng |
| Menu ở màn hẹp | Khối mở ra ngay dưới thanh đầu trang bằng một nút | Ngăn trượt bằng Radix Dialog: thêm một dòng vi phạm CSP mỗi lần mở (T2) |
| `--accent-tint` | Mã hex cố định, có test kiểm phép pha | `color-mix()` lúc chạy: T2 |
| Trang chủ của user thường | `/` chuyển sang `/tools`; Bảng điều khiển của họ ở `/dashboard` (mục 3.6) | Chỉ đổi đích sau đăng nhập: mở thẳng địa chỉ panel vẫn ra Bảng điều khiển, trái với "trang chủ là Công cụ" |
| Kiểm kiểu dáng | Test Go quét `web/src` (cấm cỡ chữ dưới 12, viền màu chữ, mã màu viết thẳng…) | Chỉ soát bằng mắt: 22 nghìn dòng, tám thợ |
| Đếm DB cho menu | Trường `db_count` ở API tổng quan | Gọi thêm `/api/v1/databases` ở khung trang: câu SQL đo dung lượng chạy ở mỗi lần mở trang |

Nguồn đã tra (21/11/2026): mã nguồn đã cài của `@radix-ui/react-menu` 2.2.1 (`dist/index.mjs`: `modal` mặc định `true`; `ScrollLockWrapper = disableOutsideScroll ? RemoveScroll : React.Fragment`), file kiểu của `react-dropdown-menu` 2.2.1 và `react-tabs` 1.2.1 trong `web/node_modules`; WCAG 2.1 mục 1.4.3, 1.4.11, 1.4.1; tài liệu Tailwind 4 về `@theme` (đã dùng ở bản 1).

## 3. Thiết kế

Mốc này không có mô hình dữ liệu, migration, máy trạng thái, Plan, lệnh CLI hay phép kiểm health mới. Phần thiết kế là: token, thành phần, khung trang, đường dẫn, từng màn, một trường API, dữ liệu demo, đồ nghề kiểm.

### 3.1 Màu (biến CSS trong `web/src/styles.css`)

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
| `--field` | `#7A8592` | `#737C87` | viền ô nhập, ô chọn (thêm, xem T4) |
| `--accent` | `#0B4FB8` | `#8FB8FF` | nút chính, mục menu đang mở, liên kết, viền focus |
| `--accent-hover` | `#083A87` | `#B9D2FF` | nút chính và liên kết lúc rê |
| `--on-accent` | `#FFFFFF` | `#0B1220` | chữ trên nền nhấn |
| `--accent-tint` | `#E7EDF8` | `#2D3645` | nền mục đang mở, ô biểu tượng (nhấn 10% trên `--surface` ở sáng, 16% ở tối) |
| `--on-ink` | `#FFFFFF` | `#111316` | chữ trên nền `--ink` (dải "đang xem như", dòng log đang chọn) |
| `--pre` | `#E1E4E1` | `#101214` | nền khối bằng chứng (như bản 1) |
| `--track` | `#D2D5D7` | `#30353A` | rãnh thanh số liệu (như bản 1) |
| `--route` | `#171A1F` | `#B8BEC4` | nét tuyến liền (bản 1 gọi là `--line`) |
| `--route-dash` | `#7E848A` | `#6B727A` | nét tuyến đứt (bản 1 gọi là `--line-dash`) |
| `--scrim` | `rgb(23 26 31 / 0.45)` | `rgb(0 0 0 / 0.6)` | lớp phủ sau hộp thoại |
| `--shadow-pop` | `0 8px 24px rgb(23 26 31 / 0.14)` | `0 8px 24px rgb(0 0 0 / 0.5)` | bóng của lớp nổi |
| `--ok`, `--ok-text`, `--warn`, `--warn-border`, `--fail`, `--on-fail`, `--fail-text`, `--unknown` | như bản 1 | như bản 1 | bốn dấu trạng thái |

Bỏ: `--card-border`, `--link`, `--link-hover`, `--line` (nghĩa cũ), `--line-dash`. Tên `--line` đổi nghĩa (bản 1: nét tuyến; bản 2: viền nút phụ), nên mọi class `border-line`, `border-line-dash` của hai file bản đồ tuyến phải đổi thành `border-route`, `border-route-dash` **trong cùng commit đổi token**; test kiểu dáng (mục 4, B6) cấm `border-line-dash` để không sót.

Hai biến không có trong bảng của đặc tả, thêm vì trang mẫu dùng hoặc vì đo ra: `--ink-soft` (trang mẫu dùng `#2B3138` cho mục menu và `#3C4652` cho chữ nhãn; bản thật gộp thành một biến) và `--field` (T4).

Xanh lá, vàng, đỏ vẫn chỉ dành cho trạng thái. Ngoại lệ duy nhất, có trong đặc tả: nút **nguy hiểm** dùng chữ `--fail-text`, chỉ cho việc không hoàn tác được.

Tailwind nhận các biến qua `@theme inline` như bản 1: `--color-ink-soft`, `--color-soft`, `--color-field`, `--color-accent`, `--color-accent-hover`, `--color-on-accent`, `--color-accent-tint`, `--color-route`, `--color-route-dash`, `--color-scrim`; `--radius-card: 10px`, `--radius-ctl: 8px`, `--radius-item: 6px`; `--shadow-pop`. Tên class: `bg-accent`, `text-on-accent`, `border-field`, `rounded-card`, `rounded-ctl`, `rounded-item`, `rounded-full`, `shadow-pop`.

### 3.2 Chữ

| Dùng cho | Font | Cỡ, độ đậm |
|---|---|---|
| Chữ nền | Sans | 14 px, dòng 1,45 |
| Tiêu đề màn `h1` | Sans | 24 px, 600, dòng 1,25, giãn −0,01em |
| Tiêu đề khối `h2`, tiêu đề hộp thoại | Sans | 16 px, 600 |
| Số liệu lớn | Sans | 24 px, 500, `tabular-nums` |
| Nhãn, đầu bảng, tên nhóm menu, chữ trong nhãn nhỏ | Sans | 12 px, 500, **viết thường** |
| Chữ phụ | Sans | 13 px; dòng phụ trong ô bảng và ô công cụ 12,5 px |
| Thứ người ta chép hoặc so từng ký tự | Mono | 13 px |
| Khối bằng chứng, khối lệnh, log | Mono | 12,5 px, dòng 1,55 |
| Tên TPAN trên thanh đầu trang | Mono | 18 px, 600, giãn 0,04em |

- Cỡ chữ được phép trong `web/src`: 12, 12,5, 13, 14, 16, 18, 24 px. **Không chữ nào dưới 12 px** (kể cả phím tắt `⌘K`, trang mẫu vẽ 11 px; bản thật 12 px).
- Mono chỉ cho: đường dẫn, lệnh, tên file, giá trị cấu hình, tên DB và user hệ thống, mã job, sha, số phiên bản, tên máy. **Tên miền dùng sans.** Tên user TPAN, tên gói, bộ ký tự dùng sans (như trang mẫu).
- Class `font-mono` chỉ được xuất hiện trong `web/src/components/`, `Layout.tsx` và hai file có khối log riêng (`screens/site/LiveLog.tsx`, `ai/AnswerText.tsx`); các màn dùng `MonoText` hoặc `Code`. Test B6 giữ luật này.
- Không còn nhãn mono chữ hoa giãn chữ. Chuỗi nhãn trong `i18n/*.ts` đang viết hoa sẵn ("BẰNG CHỨNG", "SỬA TAY", "MÁY") đổi thành viết thường ("Bằng chứng", "Sửa tay", "Máy chủ").

### 3.3 Hình khối

- Thẻ: bo 10 px, viền `--hair`, nền `--surface`, đệm 16 px. Không còn vạch đỏ trên thẻ sự cố: dấu lỗi (ô vuông đỏ có ×) đã nói trạng thái.
- Ô điều khiển (nút, ô nhập, ô chọn, ô tìm): bo 8 px, cao 40 px; dưới 640 px cao 44 px.
- Nhãn nhỏ: bo tròn, nền `--paper` (đặt trên thẻ) hoặc `--surface` (đặt trên nền trang), không viền, chữ 12 px `--ink-soft`.
- Bóng đổ chỉ cho lớp nổi: menu thả, hộp thoại, thanh lệnh, ngăn AI, thẻ theo dõi job.
- Viền focus: `outline: 2px solid var(--accent); outline-offset: 2px`. Không bao giờ tắt.
- Liên kết: màu `--accent`. Liên kết nằm trong câu văn có gạch chân (màu nhấn so với chữ thường chỉ 2,35:1, không đủ để phân biệt bằng màu; WCAG 1.4.1). Liên kết đứng riêng (ô bảng, menu, nút) không gạch chân, gạch chân khi rê.

### 3.4 Thành phần (`web/src/components/`, chỉ phiên chính sửa)

Giữ tên file và tên export cũ ở mọi chỗ làm được, để các màn đổi ít dòng nhất. Chữ ký dưới đây là hợp đồng cho thợ.

| Thành phần | Chữ ký (rút gọn) | Hình thức, hành vi |
|---|---|---|
| `Button`, `buttonClass` | `variant: "primary" \| "secondary" \| "text" \| "danger"`; `size?: "md" \| "sm"`; `icon?: IconName` | chính: nền `--accent`, chữ `--on-accent`, rê `--accent-hover`. Phụ: viền `--line`, nền `--surface`, rê `--soft`. Chữ: không viền, chữ `--accent`, rê nền `--accent-tint`. Nguy hiểm: như nút phụ, chữ `--fail-text`. Vô hiệu: chữ `--muted`, viền `--hair`, nền `--soft`. `sm` cao 32 px (dưới 640 px vẫn 44) |
| `IconButton` | `icon: IconName; label: string` (bắt buộc) + thuộc tính nút | ô vuông 40 px không viền, biểu tượng `--muted`, rê nền `--soft`; `label` thành `aria-label` và `title` |
| `Icon` | `name: IconName; size?: 16 \| 18 \| 20` | SVG nét, khung 24, nét 1,75, đầu nét tròn, `aria-hidden`. Bộ tên ở mục 3.5 |
| `Card` | `title?; titleId?; count?; actions?; footer?; flush?; as?: "section" \| "div"; children` | thẻ; có `title` thì vẽ `h2` 16/600 và `actions` bên phải; `flush` bỏ đệm thân (cho bảng); `footer` là dải nền `--soft` 13 px `--muted` |
| `ScreenHeader` | `title; summary?; crumbs?: {label; to?}[]; actions?; meta?` | đầu màn: đường dẫn 13 px `--muted`, `h1`, tóm tắt tối đa 72 ký tự, `actions` nằm bên phải (xuống dòng ở màn hẹp), `meta` là hàng nhãn nhỏ dưới tóm tắt |
| `TableList` | như bản 1, thêm `footer?`, `minWidth?: number`; `Column` thêm `align?: "right"`, `nowrap?: boolean` (mặc định `true`) | bảng **nằm trong thẻ**: đầu bảng nền `--soft` chữ 12/500 `--muted`; ô đệm 8 × 12 px, canh giữa theo chiều dọc; hàng cao 56 px khi có nút; kẻ hàng `--hair`; khung cuộn ngang riêng; không có hàng thì vẽ `EmptyState` trong thẻ |
| `RowActions` | `primary: RowAction[]` (tối đa 2); `more?: MenuAction[]`; `moreLabel: string` | cột Việc: nút chữ, rồi nút ba chấm mở menu. `moreLabel` đọc được, dạng "Việc khác với X: a, b, c". `RowAction = {label; onClick; disabled?; busyLabel?; ariaLabel}`; `MenuAction = {label; onSelect; disabled?; danger?; hint?}` |
| `Menu` và các phần | `Menu({trigger; label; align?; children})`, `MenuItem`, `MenuLink`, `MenuLabel`, `MenuSeparator`, `MenuRadioGroup`, `MenuRadioItem`, `MenuCheckboxItem` | bọc Radix DropdownMenu, **luôn `modal={false}`** (T2); khung bo 8 px, viền `--hair`, nền `--surface`, `shadow-pop`; mục cao 40 px (dưới 640 px 44), bo 6 px, mục đang tới nền `--soft` |
| `Tabs`, `TabPanel` | `Tabs({label; value; onValueChange; tabs: {value; label; count?}[]; children})`, `TabPanel({value; children})` | bọc Radix Tabs; hàng tab kẻ dưới `--hair`, cuộn ngang riêng khi hẹp; tab đang mở chữ `--accent` 600 và gạch dưới 2 px `--accent`. Nội dung tab bị gỡ khi rời tab (T2) |
| `Tag` | `children; title?; tone?: "paper" \| "surface"` | nhãn nhỏ (mục 3.3) |
| `StatusMark` | như bản 1, thêm `size: "sm"` (14 px, cho dòng chữ) | bốn hình giữ nguyên; ruột vòng "chưa kiểm được" đổi từ `--paper` sang `--surface` |
| `StatusLine` | `status; children; suspended?` | dấu trạng thái rồi chữ, trên một dòng |
| `UsageMeter` | `used: string \| null; limit: string \| null; percent?: number \| null; hint?` | "5 / ∞" (phần "/ ∞" màu `--muted`); `limit` khác `null` và có `percent` thì thêm thanh cao 4 px (rãnh `--track`, đầy `--ink`); `used` là `null` thì ghi "chưa đo" |
| `MetricBar` | như bản 1 | nhãn 12/500 sans, số 24/500, dòng phụ 13 px, thanh 6 px |
| `Disclosure` | `summary; children; open?; defaultOpen?; onOpenChange?; id?` | nút cả hàng có `aria-expanded`, mũi tên xoay; nội dung chỉ dựng khi mở |
| `EvidenceBlock` | như bản 1, thêm `clamp?: number` | nền `--pre`, bo 8 px, mono 12,5/1,55; có `clamp` và dài hơn thì chỉ vẽ chừng đó dòng đầu và nút "Xem hết N dòng" / "Thu gọn"; bấm vào khối vẫn chép **toàn bộ** |
| `CommandBlock`, `ClaudeCommand` | như bản 1 | đổi kiểu dáng; `ClaudeCommand` nằm trong `Disclosure` đóng sẵn |
| `Field`, `Input`, `Select`, `Textarea`, `inputClass` | `Field({label; htmlFor; hint?; error?; children})`; `Input`, `Select`, `Textarea` nhận thuộc tính gốc và `mono?: boolean` | nhãn 14/500 nằm trên; ô cao 40/44, viền `--field`, nền `--surface`, bo 8 px; vô hiệu: nền `--soft`, viền `--hair`; lỗi: `aria-invalid`, câu lỗi có dấu lỗi |
| `Checkbox` | thuộc tính gốc + `label: string`; `locked?: string` | ô 16 px, `accent-color: var(--accent)`; `locked` là lời giải thích: ô bị khoá, lời đó vào nhãn đọc và `title` |
| `SegmentButton` | như bản 1 | nhóm bo 8 px viền `--line`; ô đang chọn nền `--accent-tint`, chữ `--accent`, `aria-pressed` |
| `Dialog` | như bản 1, thêm `danger?: boolean` | Radix Dialog như cũ; khung bo 10 px, `shadow-pop`, lớp phủ `--scrim`; `danger` thì nút làm việc là nút nguy hiểm |
| `EmptyState` | `children; action?` | một câu `--muted` và tuỳ chọn một liên kết hoặc nút |
| `Notice` | `kind: "pass" \| "fail" \| "warn"; children` | dấu trạng thái rồi chữ (`screens/cai-dat/Note.tsx` xuất lại từ đây) |
| `Avatar` | `name: string; size?: 28 \| 32` | hình tròn nền `--accent-tint`, chữ cái đầu viết hoa màu `--accent`, `aria-hidden` |
| `Logo`, `MonoText`, `DomainText`, `JobTracker`, `Bulk`, `PreviewedOp`, `CharsetPicker`, `DbSecretBox`, `SuspendTag` | chữ ký như bản 1 | đổi kiểu dáng. `Logo`: ruột hai ga trên cùng màu `--surface`. `JobTracker`: thẻ nổi có `shadow-pop`, lệch phải menu (`left: 248px` từ 1024 px trở lên). `Bulk`: dải "Đã chọn N" nền `--accent-tint` |

Ba file dùng chung nằm trong thư mục màn, cũng do phiên chính sửa: `screens/duyet-plan/*` (`PlanReview`), `screens/site/ErrorNote.tsx`, `screens/site/CheckItem.tsx`.

Chuỗi của thành phần nằm ở `i18n/khung.ts`.

### 3.5 Khung trang (`Layout.tsx`)

```
[dải "đang xem như"]                       chỉ khi admin đang xem như một user
┌ thanh đầu trang: dính trên cùng, nền --surface, kẻ dưới --hair, đệm 10 × 24 px ──────┐
│ [nút Menu*]  logo TPAN   tên máy   [ô tìm ⌘K, tối đa 440 px]      [N sự cố] [Hỏi AI] [tài khoản ▾] │
└──────────────────────────────────────────────────────────────────────────────────────┘
┌ menu 232 px ─────────┐┌ nội dung: đệm 24 × 32 × 40 px, các khối cách nhau 20 px ──────┐
│ dính, cuộn riêng      ││ rộng tối đa 1200 px                                           │
│ …                     ││ <màn>                                                         │
│ TPAN v0.3.0 (cuối)    ││                                                               │
└───────────────────────┘└───────────────────────────────────────────────────────────────┘
* chỉ dưới 1024 px
```

- **Thanh đầu trang.** Logo và chữ TPAN là liên kết về trang chủ (`/`). Tên máy: mono 13 px `--muted`; ở demo tên máy là "demo", kèm lời "chế độ demo" trong `title` và chữ đọc màn hình. Ô tìm là một nút mở thanh lệnh (nền `--soft`, viền `--line`, phím tắt trong khung nhỏ 12 px). Nút sự cố chỉ hiện khi `overview.incidents.length > 0`: dấu lỗi cỡ `sm` và "N sự cố", là liên kết tới `/incidents`. "Hỏi AI" chỉ admin thấy, là nút phụ, `aria-expanded` theo ngăn AI. **Không còn nút "Hỏi AI" nổi ở góc** khi ngăn đóng. Nhãn stack rời khỏi thanh này.
- **Menu tài khoản** (`Menu`, canh phải): nút gồm `Avatar`, tên, vai (12 px `--muted`), mũi tên; `aria-label` "<tên>, <vai>: mở menu tài khoản". Trong menu, từ trên xuống: nhãn tên và vai; nhóm chọn một "Ngôn ngữ" (Tiếng Việt, English); nhóm chọn một "Giao diện" (Sáng, Tối, Theo hệ điều hành); mục đánh dấu "Gợi ý"; "Đăng xuất". Đăng xuất hỏng thì một dòng `role="alert"` hiện ngay dưới thanh đầu trang. Dưới 640 px nút chỉ còn `Avatar`.
- **Menu trái.** Rộng 232 px, nền `--surface`, kẻ phải `--hair`, dính dưới thanh đầu trang (`top: 61px`, cao `calc(100vh - 61px)`, cuộn riêng). Mục: cao 40 px, bo 8 px, biểu tượng 18 px `--muted`, chữ `--ink-soft`; rê nền `--soft`; đang mở nền `--accent-tint`, chữ và biểu tượng `--accent`, đậm 600, `aria-current="page"`. Số đếm 12 px `--muted` `tabular-nums` sát phải; riêng Sự cố khi có sự cố mở: nền `--fail`, chữ `--on-fail`, bo 4 px. Tên nhóm 12/500 `--muted`, viết thường. Cuối menu (`margin-top: auto`): dòng phiên bản của M15, `id="dong-phien-ban"`, mono 12 px; admin thì là liên kết tới `/settings#phien-ban` và có ô vuông 6 px `--ink` khi có bản mới. **Bỏ chân trang.** "Đăng xuất" rời khỏi menu trái (vào menu tài khoản).
- **Nhóm và thứ tự:** (không tên) Bảng điều khiển, Công cụ, Site, Cơ sở dữ liệu, SSL, Backup, Cron, Thùng rác; **Máy chủ**: Sự cố, Lịch sử, Stack, Cập nhật, Bảo mật; **AI**: Phiên AI; **Tài khoản**: Người dùng, Gói, Cài đặt. User thường không thấy mục của màn admin (như bản 1).
- **Biểu tượng** (tên trong `Icon`): `home` Bảng điều khiển, `grid` Công cụ, `globe` Site, `database` Cơ sở dữ liệu, `lock` SSL, `archive` Backup, `clock` Cron, `trash` Thùng rác, `alert` Sự cố, `list` Lịch sử, `layers` Stack, `arrow-up-circle` Cập nhật, `shield` Bảo mật, `chat` Phiên AI, `users` Người dùng, `package` Gói, `sliders` Cài đặt; thêm `plus`, `search`, `chevron-down`, `more`, `chart`, `menu`, `close`, `check`, `copy`, `inbox` (Plan chờ duyệt). Hình vẽ lấy từ ba trang mẫu (vẽ riêng cho TPAN), cái thiếu vẽ cùng kiểu.
- **Màn hẹp (dưới 1024 px).** Menu trái ẩn. Thanh đầu trang có nút "Menu" (`aria-expanded`, `aria-controls="menu-chinh"`): bấm thì menu hiện thành một khối ngay dưới thanh đầu trang, đẩy nội dung xuống; chọn một mục thì khối tự đóng. Thanh đầu trang được xuống dòng; dưới 640 px ô tìm thành nút biểu tượng và tên máy ẩn. Nội dung đệm ngang 16 px. Cả trang không cuộn ngang.
- **Nội dung.** `<main id="noi-dung">` là cột flex, các khối ngoài cùng của một màn cách nhau 20 px. **Màn trả về một dãy khối** (`ScreenHeader`, `Card`, …) và không tự thêm khoảng trên ở khối ngoài cùng. Bề rộng tối đa: bảng 1200 px (cả vùng nội dung), form 560 px, đoạn văn 72 ký tự.
- **Lớp nổi.** Ngăn AI: khung nổi 400 × 640 px góc dưới phải (như quyết định ngày 20/11), bo 10 px, `shadow-pop`. Thẻ theo dõi job: góc dưới trái, lệch phải menu. Hai thứ này không đổi hành vi.
- Giữ nguyên từ bản 1: liên kết "Bỏ qua menu, tới nội dung"; phím `⌘K` và `⌘/`; cuộn tới neo (`#phien-ban`, `#bm-co-lap`); dải "đang xem như" (nền `--ink`, chữ `--on-ink`).

### 3.6 Đường dẫn, trang chủ theo vai

| Đường dẫn | Admin | User thường |
|---|---|---|
| `/` | Bảng điều khiển | chuyển sang `/tools` (`replace`) |
| `/dashboard` | chuyển sang `/` (`replace`) | Bảng điều khiển của họ |
| `/tools` | Công cụ | Công cụ |

- `web/src/screens/trang-chu/index.tsx` xuất hai component nhỏ chọn theo `useWho().isAdmin`: `Home` (cho `/`) và `DashboardAlias` (cho `/dashboard`). Cả hai chỉ dựng `Dashboard` hoặc `<Navigate replace>`.
- Mục menu "Bảng điều khiển" trỏ tới `/` với admin và `/dashboard` với user thường; "Công cụ" luôn trỏ `/tools`. Logo luôn trỏ `/`.
- Sau đăng nhập về `?next=` hoặc `/` như cũ; `/` tự chọn theo vai.
- Phiên "xem như" mang danh tính của user được xem (vai user), nên admin đang xem như sẽ thấy đúng trang chủ của user đó.
- Server cũ không gửi `user` trong phiên: `useWho()` coi là admin (như bản 1), nên `/` là Bảng điều khiển.
- Đây chỉ là chuyện hiển thị. Quyền thật vẫn do luật của từng route ở server giữ (`internal/server/routes.go`), M16 không đổi luật nào.

Trang một site giữ tab đang mở trong `?tab=`: `tong-quan` (mặc định, không ghi vào địa chỉ), `phep-kiem`, `log`, `php`, `backup`, `cai-dat`. Giá trị lạ thì coi như `tong-quan`.

### 3.7 Từng màn

Quy ước chung cho mọi màn: mở bằng `ScreenHeader` (đường dẫn, `h1`, tóm tắt, hành động chính bên phải); mỗi nhóm nội dung là một `Card`; danh sách là `TableList` trong thẻ; cột Việc dùng `RowActions`; ô nhập dùng `Field`/`Input`/`Select`; bằng chứng nằm trong `Disclosure`, **đóng sẵn trừ dòng đang lỗi** (`status === "fail"`); bản xem trước (`PlanReview`) và lỗi (`ErrorNote`) vẫn hiện dưới bảng và vẫn được cuộn tới như bản 1. Chức năng, lời gọi API, thứ tự ba chặng "nhập → xem trước → chạy" không đổi.

Đường dẫn ở đầu màn: màn thuộc nhóm có tên thì "<tên nhóm> / <tên màn>" (ví dụ "Tài khoản / Người dùng"); trang chi tiết thì "<tên màn cha, là liên kết> / <tên đối tượng>" (ví dụ "Site / blog.example.com", "Lịch sử / 8f3a…"); màn của nhóm không tên thì không có đường dẫn.

**Người dùng** (`/users`, phiên chính làm, khớp trang mẫu). Đầu màn: "Tài khoản / Người dùng", nút chính "Thêm user" có biểu tượng `plus`. Bảng (`minWidth` 900): ô chọn; Người dùng (`Avatar` 32, tên đậm 600 sans, nhãn "bạn" ở hàng của mình; tên đầy đủ và email ở dòng phụ 12,5 px); Vai; Gói (liên kết tới `/packages`); Site; **DB rời**; Dung lượng (ba cột này dùng `UsageMeter`); Trạng thái (`StatusLine`; dòng phụ khi tạm khoá; thêm dòng cảnh báo khi vượt gói); Việc: nút chữ "Sửa", "Đổi mật khẩu", menu ba chấm gồm Tạm khoá hoặc Mở khoá, Xem như, Xoá (nguy hiểm). Hàng của chính mình: ô chọn **bị khoá**, nhãn đọc "Chọn <tên>: không tự chọn được tài khoản của mình"; menu ba chấm của hàng đó không có Tạm khoá, Xoá, Xem như (bản 1 ẩn ba nút này), nên hàng đó không có nút ba chấm. Chân bảng: "N người dùng" và "∞ là không giới hạn theo gói. Xem các gói". Chọn nhiều hàng: dải "Đã chọn N" phía trên bảng như bản 1.

**Cơ sở dữ liệu** (`/databases`, phiên chính làm, khớp trang mẫu). Nút chính "Tạo DB". Bảng (`minWidth` 960): "DB và user" (tên DB mono 13; dòng phụ "user cùng tên" hoặc tên user mono; ghi chú; nhãn tạm khoá); "Của site" (tên miền sans, là liên kết; DB rời thì nhãn "DB rời"); Chủ (chỉ admin); Bộ ký tự (collation trong `title`); Cỡ và Số bảng (canh phải, `tabular-nums`); Việc. DB của site: "Đổi mật khẩu", "Xuất .sql", menu ba chấm (Nhập từ bản đã xuất, Đổi charset, Tạm khoá hoặc Mở khoá với admin). DB rời: "Sửa", "Đổi mật khẩu", menu ba chấm (Tạm khoá hoặc Mở khoá với admin, Đưa vào thùng rác). Hàng trong thùng rác giữ câu và liên kết như bản 1. Chân bảng: "N cơ sở dữ liệu" kèm cách chia ("đều là DB của site", hoặc "A của site, B DB rời") và ", C trong thùng rác" khi có; N là số hàng `status === "active"`, **cùng con số với menu** (mục 3.8).

**Công cụ** (`/tools`, phiên chính làm). Đầu màn: "Công cụ", tóm tắt "Mọi việc làm được với tài khoản này, xếp theo nhóm." Hai cột có xuống dòng: cột ô công cụ (`flex: 999 1 520px`) và cột bên (`flex: 1 1 280px`).

Ô công cụ là một liên kết: ô biểu tượng 36 px nền `--accent-tint` biểu tượng 20 px `--accent`; tên 14/500 kèm số đếm nếu có; một dòng mô tả 12,5 px `--muted`. Lưới `repeat(auto-fit, minmax(min(230px, 100%), 1fr))`. Danh sách ô là một bảng tĩnh trong `screens/cong-cu/tools.ts`; ô có cờ `admin` thì user thường không thấy; nhóm không còn ô nào thì không vẽ.

| Nhóm | Ô (đích) | Ai thấy |
|---|---|---|
| Website | Site, có số (`/sites`); Tạo site (`/sites/new`) | mọi người |
| Tệp và sao lưu | Backup (`/backups`); Thùng rác (`/trash`) | mọi người |
| Cơ sở dữ liệu | Cơ sở dữ liệu, có số (`/databases`) | mọi người |
| Bảo mật | SSL (`/ssl`) | mọi người |
| | Bảo mật máy (`/security`) | admin |
| Theo dõi | Truy cập và lỗi (Bảng điều khiển của vai đó); Sự cố, có số khi lớn hơn 0 (`/incidents`); Lịch sử (`/jobs`) | mọi người |
| Nâng cao | Cron (`/cron`); PHP theo site (`/sites`) | mọi người |
| | Stack (`/stack`); Cập nhật (`/updates`) | admin |
| AI | Phiên AI (`/ai`); Plan chờ duyệt (`/plans`) | admin |
| Tài khoản | Cài đặt (`/settings`) | mọi người |
| | Người dùng (`/users`); Gói (`/packages`) | admin |

Hai ô không có màn riêng trỏ tới nơi việc đó đang nằm, và mô tả nói thẳng: "PHP theo site: Bản PHP của từng site; đổi ở tab PHP của site" (`/sites`); "Truy cập và lỗi: Request và lỗi 5xx một giờ qua của từng site" (Bảng điều khiển). Test B9 kiểm mọi đích là một đường dẫn có trong `routes.tsx`.

Cột bên, ba thẻ:
- **Tài khoản**: Người dùng, Vai, Gói, Máy (mono), Stack ("LEMP · nginx · PHP 8.3", hoặc "chưa cài stack"), TPAN (mono).
- **Đang dùng**: Site, DB rời, Dung lượng, mỗi dòng một `UsageMeter`; dòng "∞ là không giới hạn theo gói."
- **Cần xem**: "N sự cố đang mở" (dấu lỗi `sm`, chữ `--fail-text`, liên kết tới `/incidents`), hoặc "Không có sự cố đang mở."

Nguồn dữ liệu, không gọi API mới: phiên (`/api/v1/session`: tên, vai, tên máy, bản TPAN); tổng quan (`/api/v1/overview`: stack, sự cố, `quota` của user thường, `db_count`). Riêng admin không có `quota` trong tổng quan: gói và mức dùng của admin lấy từ hàng của chính họ ở `GET /api/v1/users` (route chỉ admin; **user thường không bao giờ gọi**, truy vấn bị tắt bằng `enabled: isAdmin`). Phiên "xem như" là vai user nên đi đường `quota`.

**Bảng điều khiển** (thợ T1). Đầu màn: tóm tắt như bản 1; `meta` là nhãn stack (LEMP hoặc LAMP, tên web server, "PHP x.y", hoặc "chưa cài stack"; chỉ admin); `actions` là "Kiểm lại" (admin). Hai cột có xuống dòng như bản 1 (cột chính `999 1 560px`, cột bên `1 1 340px`).
- Thẻ "Đường đi request của từng site", chú giải bốn trạng thái nằm ở phần `actions` của thẻ. Bản đồ tuyến giữ nguyên hình, ga, nét, cách bấm ga mở bằng chứng. Cột tên site: `flex: 0 1 320px; min-width: 220px`; tên là liên kết sans 14/500 màu `--ink`, **một dòng**, cắt bằng dấu ba chấm, có `title`, mang `data-site-name`; dòng request bên dưới 12 px `--muted`. Nhãn ga 12/500 `--muted` viết thường. Hàng cao 52 px. Bề rộng tối thiểu của bản đồ 720 px trong khung cuộn riêng. Khối bằng chứng của ga đang mở nằm trong chính thẻ này.
- Thẻ "Máy" (bốn `MetricBar`), thẻ "TPAN tự kiểm" (mỗi phép kiểm một `Disclosure`, đóng sẵn trừ dòng lỗi), thẻ "Lịch sử máy" (mã job mono 13 `--muted`). User thường: thẻ "Hạn mức gói" thay hai thẻ đầu.
- Cột bên. "Sự cố đang mở": mỗi sự cố một thẻ gọn: dấu lỗi, tiêu đề **tối đa hai dòng** (`line-clamp-2`, đủ chữ trong `title`), một dòng phụ, nút chữ "Mở sự cố"; lệnh cho Claude Code nằm trong `Disclosure` **đóng sẵn** (chỉ admin). "Cảnh báo": **một thẻ**, mỗi cảnh báo một `Disclosure` (dấu cảnh báo, nơi, câu tóm tắt một dòng; mở ra là nguyên nhân và bằng chứng). "Chờ bạn duyệt" như bản 1 (admin).

**Trang một site** (`/sites/:domain`, thợ T2). Đầu màn: "Site / <tên miền>"; `h1` là tên miền, **sans**; `meta`: web server, PHP, chủ (admin), nhãn tạm khoá, nhãn thùng rác. Dưới đó là `Tabs`:

| Tab | Nội dung | Hiện khi |
|---|---|---|
| Tổng quan | thẻ "Đường đi request" (tuyến của site, chú giải, dòng "Hỏng ở …", "Kiểm lại", giờ cập nhật; nút "Request tới site này đi đường nào" của M14, kết quả hiện trong thẻ); thẻ "Request một giờ qua"; thẻ "Sự cố của site" | luôn |
| Phép kiểm | các nhóm theo chặng như bản 1, mỗi phép kiểm một dòng mở ra được (`CheckItem`); nhóm cô lập gộp (dưới) | luôn |
| Log | `LiveLog` | site đang chạy và đã cài stack |
| PHP | `PhpBlock` | như trên |
| Backup | `Backups` | luôn |
| Cài đặt site | chế độ `.htaccess` (chỉ Apache); tạm khoá; "Tên và đường dẫn"; đưa vào thùng rác | luôn |

- Bấm một ga trên tuyến: chuyển sang tab Phép kiểm, mở các phép kiểm của chặng đó và đưa focus tới tiêu đề nhóm (bản 1 làm đúng việc này trên cùng một trang). Ga đang chọn là state của component cha (`SiteView`), vì nội dung tab bị gỡ khi rời tab.
- "Sự cố của site": các sự cố đang mở có `target` bằng tên miền, lọc từ `overview.incidents` (cùng truy vấn với khung trang, không gọi thêm); mỗi dòng là dấu lỗi, tiêu đề, giờ mở, liên kết tới `/incidents/:id`; không có thì "Không có sự cố đang mở."
- "Tên và đường dẫn": giá trị vẫn là `MonoText` (đúng là thứ để chép).
- **Gộp phép kiểm cô lập.** Phép kiểm có `id` dạng `isolation:<từ>:<tới>` (chặng `wordpress`) không còn in từng dòng. Hàm thuần `groupIsolation(checks, domain)` ở `screens/site/isolation.ts` (chỉ `import type`) trả `null` khi không có phép nào, hoặc:

  ```ts
  type IsolationPair = { other: string; out?: CheckResult; in?: CheckResult }; // out: isolation:<domain>:<other>; in: isolation:<other>:<domain>
  type IsolationGroup = {
    others: number;            // số site khác (đếm theo tên miền, không trùng)
    total: number;             // số phép thử
    bad: number;               // số phép có status khác "pass"
    status: Status;            // xấu nhất, theo thứ tự pass < unknown < warn < fail (cùng thứ tự với route.ts)
    pairs: IsolationPair[];    // xếp: cặp có phép không đạt trước, rồi theo tên
    openIds: string[];         // id các phép cần mở sẵn: mọi phép có status "fail"
  };
  ```

  `id` được tách bằng tiền tố `isolation:` rồi tìm đúng `<domain>` ở đầu hoặc cuối phần còn lại (tên miền không chứa dấu hai chấm, nên `isolation:a.com:b.com` tách được không nhầm); phép nào không có `domain` ở một trong hai đầu thì bỏ qua (không thuộc site này). Giao diện: một `Disclosure` có dòng tóm tắt "Cô lập với N site khác: đạt (X phép thử)" khi `bad === 0`; khác đi thì "Cô lập với N site khác: K trên X phép thử không đạt". Mở ra: mỗi site khác một nhóm, hai dòng "site này → <kia>" và "<kia> → site này", mỗi dòng là một `CheckItem` mở ra được bằng chứng. `openIds` khác rỗng thì khối tự mở và **chỉ** các phép trong `openIds` mở sẵn. Dấu của chặng WordPress vẫn tính trên **mọi** phép kiểm của chặng, kể cả các phép đã gộp (không đổi cách tính trạng thái).

**Bảo mật** (`/security`, thợ T3). Hai thẻ thay cho một trang dài:
- "Kiểm tra bảo mật máy": nút "Kiểm lại" ở `actions`; mỗi dòng một `Disclosure` (`CheckRow`), **đóng sẵn trừ dòng lỗi** (bản 1 mở cả dòng cảnh báo); dòng lệnh đầu tiên dưới tên dòng bỏ đi (lệnh nằm trong bằng chứng); `EvidenceBlock clamp={20}`; nút "Bật tường lửa", "Cài fail2ban" và câu "TPAN không sửa dòng này" vẫn nằm ngoài phần mở ra, chỉ hiện khi dòng chưa đạt.
- "Cô lập giữa các site" (`id="bm-co-lap"` giữ nguyên): lời dẫn, nút "Áp bản vá cô lập"; ma trận như bản 1 (ô đang chọn viền `--accent` thay `--ink`; nhãn site ở đầu cột và đầu hàng dùng sans); hai nhóm "Phép kiểm cấp máy" và "Phép kiểm của từng site" là hai `Disclosure` có dòng tóm tắt "N phép kiểm: A đạt, B chưa đạt", **đóng sẵn**, tự mở khi nhóm có dòng lỗi; bảng chế độ `.htaccess` (Apache) là `TableList`.
- Bản xem trước và khối "Cổng sẽ được giữ mở" như bản 1.

**Cài đặt** (`/settings`, thợ T4): mỗi mục một thẻ: Mật khẩu; Phiên đang đăng nhập; Phiên bản và cập nhật (`id="phien-ban"`, chỉ admin); Giao diện (ngôn ngữ, sáng/tối/theo hệ điều hành, Gợi ý: cùng state với menu tài khoản); AI và bộ não (admin); Agent và MCP (admin). Khối "Phiên bản và cập nhật" của M15 giữ nguyên chức năng và mọi chuỗi mà kịch bản M15 dựa vào, trừ ba nhãn chữ hoa đổi thành viết thường (danh sách ở mục 3.11).

**Các màn còn lại** đổi theo cùng thành phần, không đổi bố cục ngoài quy ước chung:

| Màn | Thợ | Ghi chú riêng |
|---|---|---|
| Site (danh sách) | T2 | tên miền sans, là liên kết; cột Việc (admin) là một nút chữ Tạm khoá hoặc Mở khoá; chân bảng "N site" |
| Cập nhật | T4 | bốn thẻ; khối TPAN dùng chung với Cài đặt |
| Lịch sử, trang job | T5 | bảng trong thẻ; trang job: các bước là danh sách trong thẻ, output đang chạy **không** `clamp` |
| Sự cố, trang sự cố | T5 | ba cách sửa là ba thẻ hoặc tab trong thẻ; bằng chứng `clamp={20}` |
| SSL, Backup, Cron, Thùng rác | T6 | bảng trong thẻ, form trong thẻ |
| Stack, Gói, Tạo site, Đăng nhập | T7 | Tạo site giữ `id` các ô (mục 3.11); Đăng nhập là một thẻ giữa trang trên nền `--paper` |
| Ngăn AI, thanh lệnh, Phiên AI, Plan chờ duyệt | T8 | bỏ nút nổi; thanh lệnh thêm dòng "Site: <tên miền>" cho từng site đang chạy (đọc `useOverview()`), lọc bỏ dấu như các lệnh khác |
| Duyệt Plan (`PlanReview`), lỗi (`ErrorNote`), `CheckItem` | phiên chính | diff vẫn có dấu `+` và `−`; nút chạy mang `data-plan-run` |

### 3.8 API: trường `db_count`

`GET /api/v1/overview` thêm `"db_count": <số nguyên>`:

> `db_count` = số site `status = active` trong phạm vi của người gọi **cộng** số DB rời `status = active` trong phạm vi đó.

- Phạm vi như mọi phần khác của tổng quan: admin thấy mọi thứ, user thường chỉ của mình (`scopeOf`). Trong phiên "xem như" là phạm vi của user được xem.
- Tính trong `handleOverview` từ thứ đã đọc sẵn (`sites`) và `Store.StandaloneDBs(ctx, scope)`. Không gọi MariaDB.
- Vì sao "đang chạy" chứ không phải "mọi hàng": mục Site trong menu đếm site đang chạy và Thùng rác có số riêng; DB của site trong thùng rác đi theo site đó, DB rời trong thùng rác đã bị `DROP`. Màn Cơ sở dữ liệu vẫn liệt kê cả hàng trong thùng rác, và chân bảng nói rõ con số ấy.
- Giao diện: `Overview.db_count?: number` (server cũ không gửi thì menu không hiện số). Số ở menu, ở ô "Cơ sở dữ liệu" của trang Công cụ và ở chân bảng của màn Cơ sở dữ liệu là **cùng một định nghĩa**.
- Không đổi trường nào khác. Cột "DB rời" của màn Người dùng vẫn đọc `usage.dbs` (đúng là số DB rời, chỉ tên cột bản 1 gây hiểu nhầm).

### 3.9 Dữ liệu demo

`internal/demo` thêm ba site, tạo bằng chính `core.Sites` như các site mẫu khác:

| Tên miền | Dài | Chủ | Tạo cách đây |
|---|---|---|---|
| `cua-hang-so-mot.khach-hang-vi-du.example.com` | 44 | `khach_a` | 16 ngày |
| `hoc-truc-tuyen.lop-hoc-a.example.net` | 36 | `admin` | 6 ngày |
| `tin-tuc.to-soan-bao-vi-du.example.org` | 37 | `khach_b` | 1 ngày |

`khach_a` (gói `co_ban`, tối đa 2 site) thành đủ 2/2 site: màn Người dùng và trang Công cụ của user này có một thanh mức dùng đầy. Nếu test demo của M13 đang dựa vào việc `khach_a` còn chỗ tạo site thì site 44 ký tự thuộc `admin`, và ghi lại ở `docs/thay-doi/M16.md`. Các test demo đếm site (`demo_test.go`, `m12_test.go`, `m13_test.go`, `m14_route_test.go`) sửa theo.

### 3.10 Đồ nghề

Mọi thứ chạy qua `make` (luật 9 của LO-TRINH), không gọi `node`, `curl`, script trần:

| Lệnh | Làm gì |
|---|---|
| `make test-web` | `node --test "web/src/**/*.test.ts"`; nằm trong `make check` và CI |
| `make do-giao-dien` | chạy `scripts/anh-readme -bo m16-do` trên demo: bảng số đo; thêm `NGUONG=1` thì thoát mã khác 0 khi vi phạm một ngưỡng cứng (mục 7) |
| `make anh-m16` | bộ ảnh `-bo m16`: mọi màn 1440 × 900 sáng và tối, sáu màn chính 390 × 844, vào `docs/bang-chung/M16/`; mỗi màn kiểm không lỗi console và không cuộn ngang |
| `make e2e-m16` | `scripts/e2e-m16.sh` trên máy ảo trong `.tpan-vm`, log vào `docs/bang-chung/M16.log` |
| `make lay-dev` | `git fetch origin` (cho bước ghép cuối mốc) |

`scripts/anh-readme` (Go, Chrome DevTools Protocol) thêm: giữ lại sự kiện lỗi của Chrome (`Runtime.exceptionThrown`, `console.error`, `Log.entryAdded` mức lỗi: gồm vi phạm CSP và request hỏng), gõ phím và bấm chuột thật, đổi cỡ cửa sổ, đăng nhập theo tên. Mật khẩu của panel máy ảo đi qua biến môi trường do script e2e đặt, không vào dòng lệnh, không vào log.

### 3.11 Móc cho kiểm thử (thợ không được đổi)

| Móc | Ở đâu | Ai dùng |
|---|---|---|
| `main#noi-dung`, `main h1` | khung trang, đầu màn | script chụp, e2e |
| `nav#menu-chinh`, `#dong-phien-ban` | menu trái | e2e, kịch bản M15 (thay cho `footer`) |
| `#ten-dang-nhap`, `#mat-khau` | Đăng nhập | e2e |
| `#o-domain`, `#o-email`, `form button[type=submit]` | Tạo site | script chụp, e2e |
| `[data-plan-run]` | nút chạy của `PlanReview` | e2e |
| `#phien-ban`, chữ "Kiểm bản mới", "Cập nhật lên", "Quay về bản trước", "Tải lại trang", "Đã lên ", "Đã quay về ", "Đang cập nhật TPAN", "Đang quay về bản trước", "Panel đang khởi động lại" | khối Phiên bản và cập nhật | kịch bản M15 |
| `[data-site-name]` | tên site trên bản đồ tuyến | script đo |
| `#bm-co-lap` | màn Bảo mật | liên kết từ khối cập nhật |
| `[role=tab]` có `id="tab-<giá trị>"` | trang một site | e2e, script chụp |

Ba nhãn chữ hoa mà kịch bản M15 đang tìm ("SHA256 SẼ KIỂM", "LƯỢT GẦN NHẤT") đổi thành viết thường cùng lúc với script (`m15_that.go`, `shotsM15`), trong cùng commit của thợ T4 và phiên chính ghép.

### 3.12 Bảng quy đổi bản 1 → bản 2 (cho thợ)

| Bản 1 (trong các màn) | Bản 2 |
|---|---|
| `<ScreenHeader>` rồi `<div className="pt-[26px] …">` chứa `h2` và nút | `ScreenHeader` có `actions`; nội dung vào `Card title=…` |
| `<section className="pt-[26px]">`, `<h2 className="m-0 … text-[16px] font-semibold">` | `<Card title="…">` |
| `border border-ink …` trên ô nhập, ô chọn (chuỗi class tự viết trong màn) | `Input`, `Select`, `Textarea`, `Field` |
| `border border-ink p-[12px]` làm khung ghi chú | `Card` hoặc `Notice` |
| `font-mono text-[11px] tracking-[0.08em] text-muted` (nhãn) | `text-[12px] font-medium text-muted` và chuỗi viết thường |
| `font-mono` trên tên miền, tên user, tên gói | bỏ (sans) |
| `font-mono text-[12px]` / `text-[13px]` trên đường dẫn, lệnh, mã job | `<MonoText>` (bấm là chép) hoặc `<Code>` (chỉ để đọc) |
| nhiều `<Button>` trong một ô bảng | `RowActions` (tối đa hai nút chữ, còn lại vào menu) |
| `<Tag>` viền đen | `Tag` (tự đổi), thêm `tone="surface"` khi đặt trên nền trang |
| `<Card incident>` | `Card` thường, dấu lỗi ở đầu tiêu đề |
| `hover:bg-pre` trên hàng bấm được | `hover:bg-soft` |
| `text-[11px]`, `text-[15px]`, `text-[17px]`, `text-[22px]`, `text-[30px]` | 12, 14, 16, 18 hoặc 24 theo bảng 3.2 |
| bằng chứng mở sẵn | `Disclosure` đóng sẵn, trừ dòng lỗi |
| `<p className="border-t border-hair py-[10px] text-muted">` (rỗng) | `EmptyState` |
| `border-line`, `border-line-dash` (nét tuyến) | `border-route`, `border-route-dash` |

Không viết mã màu, không viết kiểu dáng riêng ngoài các class bố cục (flex, grid, gap, đệm, bề rộng). Thiếu một thành phần thì thợ dừng và báo, không tự vẽ.

## 4. Bất biến

Mỗi câu gắn với một test có tên. "web" là gói Go `web` (`web/*_test.go`), chạy trong `go test ./...`.

| # | Bất biến | Test |
|---|---|---|
| B1 | `db_count` của một người gọi bằng đúng số hàng `status = active` mà `GET /api/v1/databases` trả cho chính người đó | `TestOverviewDBCountMatchesDatabases` (`internal/server`), với admin và với một user thường có site, DB rời, và một DB rời trong thùng rác |
| B2 | `db_count` của user thường không tính site hay DB của người khác; phiên "xem như" cho đúng số của user được xem | `TestOverviewDBCountScoped` (`internal/server`) |
| B3 | Mọi cặp chữ trên nền của bảng 3.1 đạt 4,5:1, mọi cặp hình và viền ô nhập đạt 3:1, ở cả sáng và tối; giá trị đọc thẳng từ `web/src/styles.css` | `TestTuongPhan` (web) |
| B4 | `--accent-tint` đúng bằng `--accent` pha 10% (sáng) và 16% (tối) trên `--surface` | `TestAccentTint` (web) |
| B5 | Hai chế độ khai đủ cùng một bộ biến màu (không biến nào chỉ có ở một chế độ) | `TestHaiCheDoCungBien` (web) |
| B6 | Trong `web/src`: không cỡ chữ nào ngoài {12; 12,5; 13; 14; 16; 18; 24} px; không `border-ink`, `border-card-border`, `border-line-dash`, `uppercase`, `tracking-[0.08em]`, `tracking-[0.06em]`; không mã màu hex, `rgb(`, `hsl(` trong file `.tsx`/`.ts`; không `rounded-[`, không `shadow-` ngoài `shadow-pop`; `font-mono` chỉ ở các file được phép (mục 3.2); `bg-ink` chỉ ở danh sách cho phép (dải "đang xem như", dòng log đang chọn, ô báo bản mới, phần đầy của thanh) | `TestKieuDang` (web) |
| B7 | Không file nào trong `web/src` dùng `dangerouslySetInnerHTML`, `innerHTML`, `eval(`, `new Function`; không `fetch(`, `EventSource(`, `src=`, `href=` của thẻ nạp tài nguyên, `@import`, `url(` trỏ ra `http://` hay `https://` | `TestKhongRaMangKhongChenHTML` (web) |
| B8 | Mọi menu thả đi qua `components/Menu.tsx`: `@radix-ui/react-dropdown-menu` chỉ được import ở đúng file đó, và file đó có `modal={false}` | `TestMenuKhongModal` (web) |
| B9 | Mọi đích của ô công cụ là một đường dẫn có trong `routes.tsx`; ô có đích là màn admin thì mang cờ `admin` | `tools.test.ts` (`node --test`) |
| B10 | `groupIsolation`: không phép nào thì `null`; N site khác, 2N phép đều đạt thì `bad = 0`, `openIds` rỗng; một phép lỗi thì `openIds` chỉ có đúng `id` đó và cặp ấy đứng đầu; phép không thuộc site bị bỏ qua; tên miền là tiền tố của tên khác (`a.com`, `aa.com`) không bị tách nhầm | `isolation.test.ts` (`node --test`) |
| B11 | Chuỗi nhãn trong `web/src/i18n/*.ts` không viết hoa cả câu (từ hai từ trở lên, không chữ thường nào), trừ danh sách cho phép ghi trong test | `TestNhanKhongVietHoa` (web) |
| B12 | Bản tiếng Anh có đủ khoá như bản tiếng Việt | `tsc --noEmit` (kiểu của `defineStrings`, như bản 1) |
| B13 | Không màn nào có chữ dưới 12 px, ô viền màu chữ, hay cuộn ngang cả trang, đo trên trình duyệt thật | `make do-giao-dien NGUONG=1`, `make anh-m16` |
| B14 | Mở từng màn, mở menu tài khoản và một menu ba chấm: 0 lỗi console, 0 vi phạm CSP | `make anh-m16` (demo), `make e2e-m16` (máy ảo, hai vai) |
| B15 | User thường mở `/` thì tới `/tools`; mở `/users` thì thấy trang "không có quyền"; không request nào của họ bị 403 trên các màn họ được vào | `make e2e-m16` phần user |
| B16 | Mọi route của server vẫn có luật và test phân quyền ba vai vẫn đạt (M16 không thêm route) | `authz_test.go` (có sẵn) |

## 5. Mô hình đe doạ

M16 đổi phần hiển thị và thêm một con số đếm. Bề mặt tấn công mới rất nhỏ, nhưng có năm chỗ phải giữ.

| # | Ai, đi đường nào | Chặn bằng gì | Test |
|---|---|---|---|
| Đ1 | User thường đọc số DB của người khác qua `db_count` | đếm trong phạm vi `scopeOf(r)`, cùng chỗ với mọi phần khác của tổng quan; không nhận tham số nào từ người gọi | B1, B2 |
| Đ2 | Thư viện mới (menu, tab) làm panel phải nới CSP, hoặc chèn `<style>` | chỉ dùng `modal={false}` (T2: 0 vi phạm); CSP giữ nguyên `default-src 'self'`; không sửa `auth.go` | B8, B14; `auth_test.go` (có sẵn) kiểm header |
| Đ3 | Tên miền, output lệnh, dòng log, ghi chú DB (thứ do site hay khách kiểm soát) được vẽ thành HTML ở thành phần mới (`title`, `EvidenceBlock` có "xem hết", dòng "Site: …" của thanh lệnh) | mọi thứ vào DOM qua JSX như chữ thường; không `dangerouslySetInnerHTML`; `title` là thuộc tính, React tự thoát | B7 |
| Đ4 | Giao diện gọi ra ngoài (font, biểu tượng, ảnh) làm lộ địa chỉ panel | font nhúng như bản 1; biểu tượng là SVG viết trong mã; không ảnh ngoài | B7; B14 (request ra ngoài bị CSP chặn sẽ hiện ở console) |
| Đ5 | User thường lợi dụng trang chủ theo vai hoặc trang Công cụ để tới màn admin | giao diện chỉ ẩn bớt; route server giữ luật cũ; trang Công cụ của user không gọi API admin | B15, B16 |

**Panel chạy bằng root đụng vào thứ do site kiểm soát** (symlink, đua thời điểm, tên file lạ): M16 **không thêm và không sửa** thao tác nào chạy bằng root. Không Step, không lệnh, không ghi file trên máy chủ. Ba site demo mới chạy trên `FakeSystem`. Phần này vì vậy không có đường tấn công mới; nếu trong lúc làm phát sinh nhu cầu sửa `internal/steps`, `internal/system`, `internal/engine` thì đó là dấu hiệu lệch phạm vi: dừng và ghi vào `docs/thay-doi/M16.md`.

Bí mật: giao diện không hiện thêm bí mật nào. Hai khối mật khẩu hiện một lần (`SecretBox`, `DbSecretBox`, `PasswordBox`) chỉ đổi kiểu dáng; vẫn không vào bộ đệm của TanStack Query hay `localStorage`. Ảnh chụp và log của e2e: mật khẩu, cookie, IP máy ảo được che như e2e M15; ảnh chỉ chụp trên máy demo (dữ liệu `example.com`), không chụp panel máy ảo vào repo trừ ảnh không có bí mật.

## 6. Nâng cấp và lùi

- **Từ v0.2.0 lên.** Không migration, không setting mới, không đổi file nào trên máy chủ ngoài file chạy `tpan`. Hành vi của site đang có không đổi (không đụng vhost, pool, cron). Cookie phiên và mật khẩu không đổi: phiên đăng nhập cũ còn dùng được. `localStorage` giữ nguyên ba khoá (`tpan-theme`, `tpan-lang`, `tpan-tips`) và cùng giá trị: người đang dùng chế độ tối, tiếng Anh vẫn thấy đúng như cũ sau khi lên.
- **Tab đang mở lúc nâng.** File JS của mỗi màn có tên theo mã băm; sau khi lên, tab cũ bấm sang một màn chưa tải sẽ hỏi một file không còn. `index.html` được gửi với `Cache-Control: no-cache`, nên **tải lại trang** là đủ. Đây là hành vi có sẵn từ các bản trước (khối cập nhật của M15 có nút "Tải lại trang"); M16 không làm nó tệ hơn và không sửa nó. e2e kiểm đúng điều cam kết: tải lại thì vào được giao diện mới bằng phiên cũ.
- **Đường dẫn cũ.** `/` với admin vẫn là Bảng điều khiển. Mọi đường dẫn khác giữ nguyên. Đường dẫn mới: `/tools`, `/dashboard`. User thường có dấu trang ở `/` sẽ tới `/tools`.
- **Lùi.** `tpan self-update --rollback` hoặc nút "Quay về bản trước" về v0.2.0 được: lõi của M15 chỉ chặn khi file cũ không mở được DB, mà M16 không đổi DB. Bản cũ bỏ qua `db_count`. Không có dữ liệu nào do M16 ghi mà bản cũ không hiểu.
- **Mốc khác.** Trước khi M16 vào `dev`, các mốc khác không sửa `web/` (luật 4 của LO-TRINH). Sau đó họ dùng thành phần ở mục 3.4; test B6 chặn kiểu dáng bản 1 quay lại.

## 7. Kế hoạch kiểm thử

### Unit (`make check`)

- Go: B1, B2 (server); B3 đến B8, B11 (gói `web`); test demo sửa theo ba site mới; mọi test có sẵn.
- `node --test`: B9, B10.
- `tsc --noEmit`: B12 và kiểu của mọi màn.

### Đo và ảnh trên demo

`make do-giao-dien NGUONG=1` ở 1440 × 900, sáng, tiếng Việt, dữ liệu demo có ba site tên dài. **Ngưỡng cứng**, sai một cái là thoát mã khác 0:

| Ngưỡng | Cách đo |
|---|---|
| Không chữ nào dưới 12 px, trên mọi màn được đo | `coChuNhoNhatPx >= 12` |
| Không ô nào có viền màu chữ chính, trên mọi màn được đo | `oVienDen === 0`; ngoại lệ có chủ đích (nếu có) liệt kê trong script và trong `docs/thay-doi/M16.md` |
| Danh sách Site: chữ mono không quá 15% | `phanTramChuMono <= 15` |
| Tab Tổng quan của trang một site: không quá 1,5 màn hình, mono không quá 30% | đo trên site tên dài 44 ký tự và trên `wiki.example.org` (site đang hỏng); cả hai phải đạt |
| Cơ sở dữ liệu: 5 hàng vừa một màn hình | đáy hàng thứ năm của bảng không quá 900 px |
| Bảo mật lúc mới mở: không quá 2 màn hình | `trangDaiPx <= 1800` |
| Tên site 36 ký tự trên bản đồ tuyến nằm một dòng | mọi phần tử `[data-site-name]` cao một dòng; riêng tên 36 ký tự không bị cắt (bề rộng chữ không vượt bề rộng ô) ở 1440 px |
| Không màn nào cuộn ngang cả trang | `scrollWidth <= innerWidth`, ở 1440 và ở 390 px |

Bảng trước và sau (T1 so với lần đo cuối) ghi vào `docs/thay-doi/M16.md`.

`make anh-m16`: mọi màn ở 1440 × 900, sáng và tối; sáu màn chính (Bảng điều khiển, Công cụ, Site, trang một site, Cơ sở dữ liệu, Người dùng) ở 390 × 844; thêm Công cụ và Bảng điều khiển của user thường. Với mỗi lần mở màn: 0 lỗi console (trừ đúng dòng CSP đã biết khi ảnh có hộp thoại đang mở), không cuộn ngang. Ba kiểm thêm, vì ảnh không tự nói:

- Bàn phím: trên Người dùng, Cơ sở dữ liệu, Công cụ, trang một site, script gõ Tab đi hết màn và khẳng định (a) phần tử đang focus luôn nhìn thấy được, (b) mọi nút, liên kết, ô nhập của `main` đều được tới; mở menu ba chấm bằng Enter, đi bằng mũi tên, đóng bằng Escape, focus về nút; đổi tab bằng mũi tên.
- Tiếng Anh: mở sáu màn chính và trang Cài đặt ở tiếng Anh, 1440 và 390 px; khẳng định không cuộn ngang và không phần tử chữ nào có `scrollWidth > clientWidth` mà không được phép cắt (phần tử cắt có chủ đích mang `title`).
- Khớp trang mẫu: chụp ba trang mẫu ở cùng cỡ; phiên chính xem cạnh ảnh của bản thật (Người dùng, Cơ sở dữ liệu, Công cụ) trước khi mở đợt thợ. Đây là kiểm bằng mắt của phiên chính, ghi kết luận vào `docs/thay-doi/M16.md`.

### e2e bước 19 (`make e2e-m16`, máy ảo `tpan-m16`)

Mỗi phần bắt đầu bằng trạng thái ghi rõ; khẳng định in "=> ĐẠT" hoặc "=> KHÔNG ĐẠT" như các e2e trước; log che bí mật và IP.

| Phần | Việc | Mong đợi |
|---|---|---|
| A | Máy sạch (`make vm-reset`). `make release`; cài bằng `install.sh --web nginx` của gói đó | `tpan version` là bản của nhánh; ba dịch vụ `active`; panel trả 200 ở cổng 8443 |
| B | Chrome của script chụp mở panel, **đăng nhập qua form** bằng `admin` | tới `/` (Bảng điều khiển); mở lần lượt mọi màn admin: mỗi màn có `h1`, 0 lỗi console, không cuộn ngang; mở menu tài khoản và một menu ba chấm: 0 vi phạm CSP |
| C | Tạo site tên 36 ký tự từ `/sites/new`: gõ tên miền và email, bấm xem trước, bấm `[data-plan-run]`, chờ trang job báo xong | job `succeeded`; trong máy ảo `curl` với header `Host` trả **200**; site có trong `/sites`; tên nằm một dòng trên bản đồ tuyến; số ở mục "Cơ sở dữ liệu" của menu bằng số hàng đang chạy của `/api/v1/databases` |
| D | `tpan user add` một user thường; tạo cho họ một site bằng CLI; đăng xuất, đăng nhập bằng user đó qua form | tới `/tools`; ô và menu không có mục admin; mở mọi màn của user: 0 lỗi console, **0 request bị 403**; gõ `/users` thấy trang "không có quyền"; `/dashboard` là Bảng điều khiển của họ |
| E | Máy sạch. Cài **v0.2.0** từ bản phát hành; tạo một site; đăng nhập bằng Chrome (giao diện bản 1), giữ nguyên trình duyệt. `tpan self-update --url` (nguồn tải trên loopback của máy ảo, như e2e M15) lên bản của nhánh | panel khởi động lại; **cùng cookie** gọi `/api/v1/session` vẫn `logged_in`; tải lại trang thì thấy giao diện bản 2 (có mục "Công cụ"), 0 lỗi console; đăng xuất rồi đăng nhập lại bằng mật khẩu cũ được; site vẫn 200; `tpan health` không có dòng lỗi mới |
| F | Trên máy của phần E: chạy lại kịch bản bấm thật của M15 (`-bo m15-that`) với script đã sửa theo giao diện mới | cập nhật bằng nút, panel lên lại, dòng phiên bản ở cuối menu đổi, quay về bản trước: như M15 |

### Hồi quy

- `make check` đầy đủ (mọi test Go của các mốc trước, gồm test phân quyền ba vai).
- Phần F ở trên là hồi quy của M15 trên giao diện mới (kịch bản duy nhất của các mốc trước chạy qua mã giao diện).
- Các e2e khác (M1 đến M14) gọi CLI và API, không chạy mã giao diện; M16 không đổi mã chúng dùng, trừ một trường thêm ở tổng quan. Chạy lại `scripts/e2e-m3.sh` (API và việc phục vụ giao diện) như phép thử khói cho phần đó.

## 8. Bảng chia việc

Model và effort theo luật 8 của LO-TRINH. Toàn bộ mốc là phần **không nhạy cảm** (không đụng mã chạy bằng root, đăng nhập, phân quyền, bí mật, migration); riêng trường `db_count` nằm trong `handleOverview` cạnh mã lọc theo chủ nên phiên chính tự viết. Không hai thợ chung file. File dùng chung chỉ phiên chính sửa.

| Đợt | Phần | File được sửa | Ai | Model, effort |
|---|---|---|---|---|
| 0 | Nghiên cứu, bản thử, thiết kế này | `docs/thay-doi/M16-thiet-ke.md`, `docs/bang-chung/M16-nghien-cuu/` | phiên chính | fable, max |
| 0 | Soát thiết kế | chỉ đọc; phát hiện ghi cuối file này | thợ soát | fable, max |
| 1 | Nền bản 2: token, thành phần, khung trang, đường dẫn, chuỗi khung; `db_count`; demo; test B1 đến B8, B11; ba màn Người dùng, Cơ sở dữ liệu, Công cụ; `PlanReview`, `ErrorNote`, `CheckItem`; script chụp và đo; `docs/GIAO-DIEN.md` | `web/src/styles.css`, `Layout.tsx`, `routes.tsx`, `main.tsx`, `components/**`, `api/**`, `i18n/index.tsx`, `i18n/khung.ts`, `i18n/m13-chung.ts`, `i18n/agent.ts`, `i18n/duyet-plan.ts`, `i18n/nguoi-dung.ts`, `i18n/co-so-du-lieu.ts`, `i18n/cong-cu.ts`, `screens/nguoi-dung/**`, `screens/co-so-du-lieu/**`, `screens/cong-cu/**`, `screens/trang-chu/**`, `screens/duyet-plan/**`, `screens/site/ErrorNote.tsx`, `screens/site/CheckItem.tsx`, `screens/cai-dat/Note.tsx`, `web/*_test.go`, `web/package.json`, `internal/server/**`, `internal/demo/**`, `scripts/**`, `Makefile`, `docs/**` | phiên chính | fable, max |
| 2 | T1 Bảng điều khiển | `screens/bang-dieu-khien/**`, `i18n/bang-dieu-khien.ts` | thợ giao diện | sonnet, mặc định |
| 2 | T2 Site: danh sách và trang một site (tab, gộp cô lập, `isolation.ts` và test của nó) | `screens/site/**` trừ `ErrorNote.tsx`, `CheckItem.tsx`; `i18n/site.ts` | thợ giao diện | sonnet, mặc định |
| 2 | T3 Bảo mật | `screens/bao-mat/**`, `i18n/bao-mat.ts` | thợ giao diện | sonnet, mặc định |
| 2 | T4 Cài đặt và Cập nhật | `screens/cai-dat/**` trừ `Note.tsx`; `screens/cap-nhat/**`; `i18n/cai-dat.ts`, `i18n/cap-nhat.ts`, `i18n/phien-ban.ts` | thợ giao diện | sonnet, mặc định |
| 3 | T5 Lịch sử, Sự cố | `screens/lich-su/**`, `screens/su-co/**`, `i18n/lich-su.ts`, `i18n/su-co.ts` | thợ giao diện | sonnet, mặc định |
| 3 | T6 SSL, Backup, Cron, Thùng rác | `screens/ssl/**`, `screens/backup/**`, `screens/cron/**`, `screens/thung-rac/**` và bốn file chuỗi cùng tên | thợ giao diện | sonnet, mặc định |
| 3 | T7 Stack, Gói, Tạo site, Đăng nhập | `screens/stack/**`, `screens/goi/**`, `screens/tao-site/**`, `screens/dang-nhap/**` và bốn file chuỗi cùng tên | thợ giao diện | sonnet, mặc định |
| 3 | T8 Ngăn AI, thanh lệnh, Phiên AI, Plan chờ duyệt | `ai/**`, `screens/phien-ai/**`, `screens/plan-cho-duyet/**`, `i18n/ai.ts`, `i18n/phien-ai.ts` | thợ giao diện | sonnet, mặc định |
| 4 | Ảnh, số đo, e2e bước 19 trên `tpan-m16` | `scripts/e2e-m16.sh`, `docs/bang-chung/M16*`, `docs/thay-doi/M16.md` | phiên chính | fable, max |
| 5 | File giải thích | `docs/giai-thich/16-m16-giao-dien-ban-2.md` | thợ viết | sonnet, mặc định |
| 5 | Soát cuối (đọc diff, thử phá giao diện bằng script chụp) | chỉ đọc; phát hiện trả về phiên chính | thợ soát | fable, max |
| 6 | README đổi ảnh, KE-HOACH mục 10, ghép `origin/dev`, `make check`, đẩy `moc/M16:dev` | `README.md`, `docs/KE-HOACH.md`, `docs/anh/**` | phiên chính | fable, max |

Mỗi thợ đợt 2 và 3 nhận lời giao việc năm ý: việc phải làm (mục của bảng 3.7 dành cho màn đó); danh sách file được sửa; tài liệu phải đọc (`docs/GIAO-DIEN.md` bản 2, mục 3.4, 3.7, 3.11, 3.12 của file này); file hợp đồng (`web/src/components/*`, và hai màn mẫu `screens/nguoi-dung`, `screens/co-so-du-lieu` để theo); lệnh kiểm của riêng phần đó: `pnpm -C web typecheck`, `go test ./web/`, và với T2 thêm `make test-web`.

Phiên chính nhận một phần khi: đọc hết diff của thợ; hai lệnh kiểm xanh; ảnh chụp màn đó ở sáng, tối và 390 px đã xem; số đo của màn đó đạt ngưỡng. Thấy thợ phải đoán thì trả lại và sửa file này.

## 9. Phát hiện của thợ soát

(Điền sau bước soát thiết kế.)
