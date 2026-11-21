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

Thử lần hai, đặt ba file đó vào `web/src/`: `tsc --noEmit` báo ba lỗi (không biết `node:test`, `node:assert/strict`; đường dẫn import có đuôi `.ts`). Sửa bằng `"exclude": ["src/**/*.test.ts"]` trong `web/tsconfig.json` (file test không qua tsc, Node vẫn chạy); thử lại: tsc xanh, `make test-web` 1 đạt. Luật rút ra: file được test bằng Node phải là `.ts` (Node không nhận `.tsx`) và chỉ được `import type`; chữ hiển thị không nằm trong file đó (file chuỗi kéo theo `i18n/index.tsx`).

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

Bỏ: `--card-border`, `--link`, `--link-hover`, `--line` (nghĩa cũ), `--line-dash`. Tên `--line` đổi nghĩa (bản 1: nét tuyến; bản 2: viền nút phụ), nên mọi class `border-line`, `border-line-dash` của hai file bản đồ tuyến (`screens/bang-dieu-khien/RouteMap.tsx:24-25`, `screens/site/RouteMap.tsx:62`) phải đổi thành `border-route`, `border-route-dash` **trong cùng commit đổi token**: phiên chính sửa đúng các class đó ở đợt 1 (hai file này thuộc T1, T2 ở đợt 2, nhưng đợt 1 chạy trước nên không chung file cùng lúc). Test B6 cấm `border-line-dash` ở mọi nơi và cấm `border-line` ngoài `components/` và `Layout.tsx`, để không sót.

Bí danh tạm trong đợt chuyển: màn chưa đổi (đợt 2, 3) còn dùng class của biến bị bỏ (`border-card-border` ở `ai/AiDrawer.tsx:162`). Đợt 1 giữ `--card-border: var(--hair)`, `--link: var(--accent)`, `--link-hover: var(--accent-hover)` cùng ánh xạ Tailwind của chúng, để màn chưa đổi không mất viền hay màu liên kết. Bước dọn cuối mốc (sau đợt 3) gỡ cả ba; lúc đó B6 đã quét mọi file nên không còn chỗ nào dùng.

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
- Class `font-mono` chỉ được xuất hiện trong `web/src/components/`, `Layout.tsx`, hai file có khối log riêng (`screens/site/LiveLog.tsx`, `ai/AnswerText.tsx`) và màn diff của `PlanReview` (`screens/duyet-plan/index.tsx`); các màn khác dùng `MonoText` (bấm là chép), `Code` (chỉ để đọc, nằm trong dòng) hoặc `Code block` (khối nhiều dòng không phải bằng chứng, ví dụ dòng mã trong ngăn AI). Test B6 giữ luật này.
- Không còn nhãn mono chữ hoa giãn chữ. Chuỗi nhãn trong `i18n/*.ts` đang viết hoa sẵn ("BẰNG CHỨNG", "SỬA TAY", "MÁY") đổi thành viết thường ("Bằng chứng", "Sửa tay", "Máy chủ").

### 3.3 Hình khối

- Thẻ: bo 10 px, viền `--hair`, nền `--surface`, đệm 16 px. Không còn vạch đỏ trên thẻ sự cố: dấu lỗi (ô vuông đỏ có ×) đã nói trạng thái.
- Ô điều khiển (nút, ô nhập, ô chọn, ô tìm): bo 8 px, cao 40 px; dưới 640 px cao 44 px.
- Nhãn nhỏ: bo tròn, nền `--paper` (đặt trên thẻ) hoặc `--surface` (đặt trên nền trang), không viền, chữ 12 px `--ink-soft`.
- Bóng đổ chỉ cho lớp nổi: menu thả, hộp thoại, thanh lệnh, ngăn AI, thẻ theo dõi job. Đặc tả chỉ kể "menu thả và hộp thoại"; ngăn AI và thẻ theo dõi job cũng nằm đè lên nội dung (không chiếm chỗ trong bố cục), nên cần bóng để tách khỏi nền như menu thả. Đây là quyết định Q18 ở `docs/thay-doi/M16.md`.
- Viền focus: `outline: 2px solid var(--accent); outline-offset: 2px`. Không bao giờ tắt.
- Liên kết: màu `--accent`. Liên kết nằm trong câu văn có gạch chân (màu nhấn so với chữ thường chỉ 2,35:1, không đủ để phân biệt bằng màu; WCAG 1.4.1). Liên kết đứng riêng (ô bảng, menu, nút) không gạch chân, gạch chân khi rê.

### 3.4 Thành phần (`web/src/components/`, chỉ phiên chính sửa)

Giữ tên file và tên export cũ ở mọi chỗ làm được, để các màn đổi ít dòng nhất. Chữ ký dưới đây là hợp đồng cho thợ.

| Thành phần | Chữ ký (rút gọn) | Hình thức, hành vi |
|---|---|---|
| `Button`, `buttonClass` | `variant: "primary" \| "secondary" \| "text" \| "danger"`; `size?: "md" \| "sm"`; `icon?: IconName` | chính: nền `--accent`, chữ `--on-accent`, rê `--accent-hover`. Phụ: viền `--line`, nền `--surface`, rê `--soft`. Chữ: không viền, chữ `--accent`, rê nền `--accent-tint`. Nguy hiểm: như nút phụ, chữ `--fail-text`. Vô hiệu: chữ `--muted`, viền `--hair`, nền `--soft`. `sm` cao 32 px (dưới 640 px vẫn 44) |
| `IconButton` | `icon: IconName; label: string` (bắt buộc) + thuộc tính nút | ô vuông 40 px không viền, biểu tượng `--muted`, rê nền `--soft`; `label` thành `aria-label` và `title` |
| `Icon` | `name: IconName; size?: 16 \| 18 \| 20` | SVG nét, khung 24, nét 1,75, đầu nét tròn, `aria-hidden`. Bộ tên ở mục 3.5 |
| `Card` | `title?; titleId?; count?; actions?; footer?; as?: "section" \| "div"; className?; children` | thẻ; có `title` thì vẽ `h2` 16/600 và `actions` bên phải; `footer` là dải nền `--soft` 13 px `--muted`. Không bọc `TableList` trong `Card` (bảng tự vẽ thẻ của nó) |
| `ScreenHeader` | `title; summary?; crumbs?: {label; to?}[]; actions?; meta?` | đầu màn: đường dẫn 13 px `--muted`, `h1`, tóm tắt rộng tối đa 72ch (giới hạn bề rộng dòng, không giới hạn số chữ), `actions` nằm bên phải (xuống dòng ở màn hẹp), `meta` là hàng nhãn nhỏ dưới tóm tắt |
| `TableList` | như bản 1, thêm `title?`, `titleId?`, `actions?`, `footer?`, `minWidth?: number`, `label?` (tên đọc của vùng); `Column` thêm `align?: "right"`, `nowrap?: boolean` (mặc định `true`) | **tự vẽ thẻ** (một `section` bo 10 px viền `--hair`): có `title` thì đầu thẻ là `h2` và `actions`; đầu bảng nền `--soft` chữ 12/500 `--muted`; ô đệm 8 × 12 px, canh giữa theo chiều dọc; hàng cao 56 px khi có nút; kẻ hàng `--hair`; khung cuộn ngang riêng; `footer` là dải nền `--soft`; không có hàng thì vẽ `EmptyState` trong thẻ |
| `RowActions` | `primary: RowAction[]` (tối đa 2); `more?: MenuAction[]`; `moreLabel: string` | cột Việc: nút chữ, rồi nút ba chấm mở menu. `moreLabel` đọc được, dạng "Việc khác với X: a, b, c". `RowAction = {label; onClick; disabled?; busyLabel?; ariaLabel}`; `MenuAction = {label; onSelect; disabled?; danger?; hint?}` |
| `Menu` và các phần | `Menu({trigger; label; align?; children})`, `MenuItem`, `MenuLink`, `MenuLabel`, `MenuSeparator`, `MenuRadioGroup`, `MenuRadioItem`, `MenuCheckboxItem` | bọc Radix DropdownMenu, **luôn `modal={false}`** (T2); khung bo 8 px, viền `--hair`, nền `--surface`, `shadow-pop`; mục cao 40 px (dưới 640 px 44), bo 6 px, mục đang tới nền `--soft` |
| `Tabs`, `TabPanel` | `Tabs({label; value; onValueChange; tabs: {value; label; count?}[]; children})`, `TabPanel({value; children})` | bọc Radix Tabs; hàng tab kẻ dưới `--hair`, cuộn ngang riêng khi hẹp; tab đang mở chữ `--accent` 600 và gạch dưới 2 px `--accent`. Nội dung tab bị gỡ khi rời tab (T2). Không đè `id` của nút tab (Radix 1.2.1 nối `aria-labelledby` của khối nội dung với id nó tự sinh, `dist/index.mjs:124-125, 174`); móc cho kiểm thử là `data-tab="<giá trị>"` trên nút tab |
| `Tag` | `children; title?; tone?: "paper" \| "surface"` | nhãn nhỏ (mục 3.3) |
| `StatusMark` | như bản 1, thêm `size: "sm"` (14 px, cho dòng chữ) | bốn hình giữ nguyên; ruột vòng "chưa kiểm được" đổi từ `--paper` sang `--surface` |
| `StatusLine` | `status; children; suspended?` | dấu trạng thái rồi chữ, trên một dòng |
| `UsageMeter` | `used: string \| null; limit: string \| null; percent?: number \| null; hint?` | "5 / ∞" (phần "/ ∞" màu `--muted`); `limit` khác `null` và có `percent` thì thêm thanh cao 4 px (rãnh `--track`, đầy `--ink`); `used` là `null` thì ghi "chưa đo" |
| `MetricBar` | như bản 1 | nhãn 12/500 sans, số 24/500, dòng phụ 13 px, thanh 6 px |
| `Disclosure` | `summary; children; open?; defaultOpen?; onOpenChange?; id?` | nút cả hàng có `aria-expanded`, mũi tên xoay; nội dung chỉ dựng khi mở |
| `EvidenceBlock` | như bản 1, thêm `clamp?: number` | nền `--pre`, bo 8 px, mono 12,5/1,55; có `clamp` và dài hơn thì chỉ vẽ chừng đó dòng đầu và nút "Xem hết N dòng" / "Thu gọn"; bấm vào khối vẫn chép **toàn bộ** |
| `CommandBlock`, `ClaudeCommand` | như bản 1 | đổi kiểu dáng; `ClaudeCommand` nằm trong `Disclosure` đóng sẵn |
| `Field`, `Input`, `Select`, `Textarea`, `inputClass`, `TextField`, `SelectField` | `Field({label; htmlFor; hint?; hintId?; error?; children})`; `Input`, `Select`, `Textarea` nhận thuộc tính gốc và `mono?: boolean`; `TextField`, `SelectField` giữ đúng chữ ký của `screens/dang-nhap/Field.tsx`, `SelectField.tsx` bản 1 (`id; label; hint?; invalid?` + thuộc tính gốc) để màn chuyển sang chỉ đổi dòng import | nhãn 14/500 nằm trên; ô cao 40/44, viền `--field`, nền `--surface`, bo 8 px; vô hiệu: nền `--soft`, viền `--hair`; lỗi: `aria-invalid`, câu lỗi có dấu lỗi |
| `Code` | `Code({children; block?: boolean; className?})` (cùng file `MonoText.tsx`) | chữ mono chỉ để đọc, không bấm chép: trong dòng 13 px; `block` là khối nhiều dòng nền `--pre`, bo 8 px, 12,5/1,55, tự xuống dòng (dòng mã trong ngăn AI, output không phải bằng chứng). `className` chỉ cho class bố cục và chip nền (ví dụ mã HTTP nền `--fail`) |
| `Checkbox` | thuộc tính gốc + `label: string`; `locked?: string` | ô 16 px, `accent-color: var(--accent)`; `locked` là lời giải thích: ô bị khoá, lời đó vào nhãn đọc và `title` |
| `SegmentButton` | như bản 1 | nhóm bo 8 px viền `--line`; ô đang chọn nền `--accent-tint`, chữ `--accent`, `aria-pressed` |
| `Dialog` | như bản 1, thêm `danger?: boolean` | Radix Dialog như cũ; khung bo 10 px, `shadow-pop`, lớp phủ `--scrim`; `danger` thì nút làm việc là nút nguy hiểm |
| `EmptyState` | `children; action?` | một câu `--muted` và tuỳ chọn một liên kết hoặc nút |
| `Notice` | `kind: "pass" \| "fail" \| "warn"; children` | dấu trạng thái rồi chữ (`screens/cai-dat/Note.tsx` xuất lại từ đây) |
| `Avatar` | `name: string; size?: 28 \| 32` | hình tròn nền `--accent-tint`, chữ cái đầu viết hoa màu `--accent`, `aria-hidden` |
| `Logo`, `MonoText`, `DomainText`, `JobTracker`, `Bulk`, `PreviewedOp`, `CharsetPicker`, `DbSecretBox`, `SuspendTag` | chữ ký như bản 1 | đổi kiểu dáng. `Logo`: ruột hai ga trên cùng màu `--surface`. `JobTracker`: thẻ nổi có `shadow-pop`, lệch phải menu (`left: 248px` từ 1024 px trở lên). `Bulk`: dải "Đã chọn N" nền `--accent-tint` |

File dùng chung nằm trong thư mục màn, do phiên chính sửa ở đợt 1 (màn khác import chúng): `screens/duyet-plan/*` (`PlanReview`), `screens/site/ErrorNote.tsx`, `screens/site/CheckItem.tsx`, `screens/bao-mat/CheckRow.tsx` (màn Cập nhật của T4 dùng), `screens/cai-dat/Note.tsx` (xuất lại `Notice`). Hai chỗ dùng chung còn lại ở trong vùng của thợ, có luật riêng:
- `Legend` của `screens/site/RouteMap.tsx` (T2) được `screens/bao-mat/Isolation.tsx` (T3) dùng, hai thợ cùng đợt 2: T2 đổi kiểu dáng nhưng giữ tên và chữ ký của `Legend`.
- `screens/plan-cho-duyet/PendingPlans.tsx` (T8, đợt 3) nằm trong cột bên của Bảng điều khiển (T1, đợt 2): giữa đợt 2 và 3 khối "Chờ bạn duyệt" còn kiểu bản 1; ảnh nghiệm thu của Bảng điều khiển chụp lại sau đợt 3.
- `screens/dang-nhap/Field.tsx`, `SelectField.tsx` (T7) đang được `nguoi-dung` (phiên chính), `goi` (T7), `cai-dat/MatKhau`, `cai-dat/BoNao` (T4), `lich-su/JobList` (T5) import. Mỗi màn đó chuyển sang `TextField`, `SelectField` của `components/Field.tsx`; T7 đổi hai file của `dang-nhap` thành dòng xuất lại từ `components/Field.tsx` (không xoá), để màn nào chưa chuyển vẫn chạy. Bước dọn cuối mốc xoá hai file đó nếu không còn ai import.

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

- **Thanh đầu trang.** Từ 1024 px trở lên thanh đầu trang **không xuống dòng** và cao đúng 61 px (đệm 10 px trên dưới, ô cao 40, kẻ 1 px): ô tìm co lại (`flex: 1 1 280px; min-width: 160px; max-width: 440px`), tên máy rộng tối đa 220 px, dài thì cắt bằng dấu ba chấm và có `title` đủ tên (tên máy VPS thật hay dài hơn "demo"). Chiều cao này là biến CSS `--header-h: 61px` mà menu dính dùng. Logo và chữ TPAN là liên kết về trang chủ (`/`). Tên máy: mono 13 px `--muted`; ở demo tên máy là "demo", kèm lời "chế độ demo" trong `title` và chữ đọc màn hình. Ô tìm là một nút mở thanh lệnh (nền `--soft`, viền `--line`, phím tắt trong khung nhỏ 12 px). Nút sự cố chỉ hiện khi `overview.incidents.length > 0`: dấu lỗi cỡ `sm` và "N sự cố", là liên kết tới `/incidents`. "Hỏi AI" chỉ admin thấy, là nút phụ, `aria-expanded` theo ngăn AI. **Không còn nút "Hỏi AI" nổi ở góc** khi ngăn đóng. Nhãn stack rời khỏi thanh này.
- **Menu tài khoản** (`Menu`, canh phải): nút gồm `Avatar`, tên, vai (12 px `--muted`), mũi tên; `aria-label` "<tên>, <vai>: mở menu tài khoản". Trong menu, từ trên xuống: nhãn tên và vai; nhóm chọn một "Ngôn ngữ" (Tiếng Việt, English); nhóm chọn một "Giao diện" (Sáng, Tối, Theo hệ điều hành); mục đánh dấu "Gợi ý"; "Đăng xuất". Đăng xuất hỏng thì một dòng `role="alert"` hiện ngay dưới thanh đầu trang. Dưới 640 px nút chỉ còn `Avatar`.
- **Menu trái.** Rộng 232 px, nền `--surface`, kẻ phải `--hair`, dính dưới thanh đầu trang (`top: var(--header-h)`, cao `calc(100vh - var(--header-h))`, cuộn riêng). Vì thanh đầu trang dính, `html` có `scroll-padding-top: 72px`: cuộn tới neo (`#phien-ban`) và cuộn theo phím Tab không để phần tử nằm khuất dưới thanh đầu trang. Mục: cao 40 px, bo 8 px, biểu tượng 18 px `--muted`, chữ `--ink-soft`; rê nền `--soft`; đang mở nền `--accent-tint`, chữ và biểu tượng `--accent`, đậm 600, `aria-current="page"`. Số đếm 12 px `--muted` `tabular-nums` sát phải; riêng Sự cố khi có sự cố mở: nền `--fail`, chữ `--on-fail`, bo 4 px. Tên nhóm 12/500 `--muted`, viết thường. Cuối menu (`margin-top: auto`): dòng phiên bản của M15, `id="dong-phien-ban"`, mono 12 px; admin thì là liên kết tới `/settings#phien-ban` và có ô vuông 6 px `--ink` khi có bản mới. **Bỏ chân trang.** "Đăng xuất" rời khỏi menu trái (vào menu tài khoản).
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
- `routes.tsx`: `MenuSpec` thêm `userPath?: string` (đường dẫn của mục này trong menu khi người xem là user thường) và `icon: IconName`. Mục "Bảng điều khiển" là `{ path: "/", menu: { label: "dashboard", userPath: "/dashboard", icon: "home" } }`; route `/dashboard` không có `menu`. Hàm dùng chung `menuPath(entry, admin)` trả `admin ? entry.path : entry.menu.userPath ?? entry.path`; menu trái, trang Công cụ và thanh lệnh đều gọi nó. "Công cụ" luôn trỏ `/tools`. Logo luôn trỏ `/`.
- Thanh lệnh (`ai/CommandPalette.tsx`) và nhãn "đang ở" gửi cho AI (`ai/views.ts`): thanh lệnh chỉ liệt kê mục có `menu` (đi tới `menuPath`) cộng các route không tham số không có `menu` trừ `/dashboard` (ví dụ "Tạo site", "Plan chờ duyệt" như bản 1), nên không còn dòng "Mở /dashboard"; `viewLabel("/dashboard")` trả tên mục "Bảng điều khiển". Phần logic này phiên chính sửa ở đợt 1 (vài dòng); T8 chỉ đổi kiểu dáng hai file đó ở đợt 3.
- Sau đăng nhập về `?next=` hoặc `/` như cũ; `/` tự chọn theo vai.
- Phiên "xem như" mang danh tính của user được xem (vai user), nên admin đang xem như sẽ thấy đúng trang chủ của user đó.
- Server cũ không gửi `user` trong phiên: `useWho()` coi là admin (như bản 1), nên `/` là Bảng điều khiển.
- Đây chỉ là chuyện hiển thị. Quyền thật vẫn do luật của từng route ở server giữ (`internal/server/routes.go`), M16 không đổi luật nào.

Trang một site giữ tab đang mở trong `?tab=`: `tong-quan` (mặc định, không ghi vào địa chỉ), `phep-kiem`, `log`, `php`, `backup`, `cai-dat`. Giá trị lạ, hoặc tab đang ẩn (Log, PHP khi site trong thùng rác hay chưa cài stack), thì coi như `tong-quan`. Đổi tab ghi địa chỉ bằng `replace` (nút Back của trình duyệt về màn trước, không đi qua từng tab).

### 3.7 Từng màn

Quy ước chung cho mọi màn: mở bằng `ScreenHeader` (đường dẫn, `h1`, tóm tắt, hành động chính bên phải); mỗi nhóm nội dung là một `Card`; danh sách là `TableList` trong thẻ; cột Việc dùng `RowActions`; ô nhập dùng `Field`/`Input`/`Select`; bằng chứng nằm trong `Disclosure`, **đóng sẵn trừ dòng đang lỗi** (`status === "fail"`); bản xem trước (`PlanReview`) và lỗi (`ErrorNote`) vẫn hiện dưới bảng và vẫn được cuộn tới như bản 1. Chức năng, lời gọi API, thứ tự ba chặng "nhập → xem trước → chạy" không đổi.

Đường dẫn ở đầu màn: màn thuộc nhóm có tên thì "<tên nhóm> / <tên màn>" (ví dụ "Tài khoản / Người dùng"); trang chi tiết thì "<tên màn cha, là liên kết> / <tên đối tượng>" (ví dụ "Site / blog.example.com", "Lịch sử / 8f3a…"); màn của nhóm không tên thì không có đường dẫn.

**Người dùng** (`/users`, phiên chính làm, khớp trang mẫu). Đầu màn: "Tài khoản / Người dùng", nút chính "Thêm user" có biểu tượng `plus`. Bảng (`minWidth` 900): ô chọn; Người dùng (`Avatar` 32, tên đậm 600 sans, nhãn "bạn" ở hàng của mình; tên đầy đủ và email ở dòng phụ 12,5 px); Vai; Gói (liên kết tới `/packages`); Site; **DB rời**; Dung lượng (ba cột này dùng `UsageMeter`); Trạng thái (`StatusLine`; dòng phụ khi tạm khoá; thêm dòng cảnh báo khi vượt gói); Việc: nút chữ "Sửa", "Đổi mật khẩu", menu ba chấm gồm Tạm khoá hoặc Mở khoá, Xem như, Xoá (nguy hiểm), theo đúng ba luật ẩn của bản 1 (`screens/nguoi-dung/index.tsx:202-223`): hàng của chính mình không có Tạm khoá, Xoá, Xem như; "Xoá" không có ở user `admin`; "Xem như" chỉ có ở hàng mang vai user (server không chặn xem như một admin, nên giao diện phải giữ luật này). Menu ba chấm của một hàng không còn mục nào thì hàng đó không có nút ba chấm. Hàng của chính mình: ô chọn **bị khoá**, nhãn đọc "Chọn <tên>: không tự chọn được tài khoản của mình". Chân bảng: "N người dùng" và "∞ là không giới hạn theo gói. Xem các gói". Chọn nhiều hàng: dải "Đã chọn N" phía trên bảng như bản 1.

**Cơ sở dữ liệu** (`/databases`, phiên chính làm, khớp trang mẫu). Nút chính "Tạo DB". Bảng (`minWidth` 960): "DB và user" (tên DB mono 13; dòng phụ "user cùng tên" hoặc tên user mono; ghi chú; nhãn tạm khoá); "Của site" (tên miền sans, là liên kết; DB rời thì nhãn "DB rời"); Chủ (chỉ admin); Bộ ký tự (collation trong `title`); Cỡ và Số bảng (canh phải, `tabular-nums`); Việc. DB của site: "Đổi mật khẩu", "Xuất .sql", menu ba chấm (Nhập từ bản đã xuất, Đổi charset, Tạm khoá hoặc Mở khoá với admin). DB rời: "Sửa", "Đổi mật khẩu", menu ba chấm (Tạm khoá hoặc Mở khoá với admin, Đưa vào thùng rác). Điều kiện của từng việc giữ đúng bản 1 (`screens/co-so-du-lieu/index.tsx:108-158`): DB của site đang trong thùng rác chỉ còn "Xuất .sql" kèm câu và liên kết tới Thùng rác; site đang tạm khoá thì "Nhập" bị khoá kèm lời; DB rời trong thùng rác chỉ có câu và liên kết. Chân bảng: "N cơ sở dữ liệu" (N là số hàng `status === "active"`, **cùng con số với menu**, mục 3.8) kèm cách chia: "đều là DB của site" (chỉ có DB của site), "đều là DB rời" (chỉ có DB rời), "A của site, B DB rời" (có cả hai), thêm ", C trong thùng rác" khi C > 0; N = 0 thì chỉ ghi "Chưa có cơ sở dữ liệu nào đang chạy" (và ", C trong thùng rác" nếu có).

**Công cụ** (`/tools`, phiên chính làm). Đầu màn: "Công cụ", tóm tắt "Mọi việc làm được với tài khoản này, xếp theo nhóm." Hai cột có xuống dòng: cột ô công cụ (`flex: 999 1 520px`) và cột bên (`flex: 1 1 280px`).

Ô công cụ là một liên kết: ô biểu tượng 36 px nền `--accent-tint` biểu tượng 20 px `--accent`; tên 14/500 kèm số đếm nếu có; một dòng mô tả 12,5 px `--muted`. Lưới `repeat(auto-fit, minmax(min(230px, 100%), 1fr))`. Danh sách ô là một bảng tĩnh trong `screens/cong-cu/tools.ts` (file `.ts` thuần, chỉ `import type`): mỗi ô là `{ path: ScreenPath; icon: IconName; label: <khoá của i18n/cong-cu.ts>; desc: <khoá>; count?: "sites" | "dbs" | "incidents"; userPath?: ScreenPath }`, trong đó `ScreenPath` là kiểu hợp mọi đường dẫn trong `routes.tsx` (mục 3.6), nên viết sai đường dẫn là `tsc` báo lỗi. Ô nào trỏ tới màn có cờ `admin` trong `routes.tsx` thì user thường không thấy (màn tra cờ ngay trong `routes.tsx`, không chép cờ sang `tools.ts`); nhóm không còn ô nào thì không vẽ.

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

Hai ô không có màn riêng trỏ tới nơi việc đó đang nằm, và mô tả nói thẳng: "PHP theo site: Bản PHP của từng site; đổi ở tab PHP của site" (`/sites`); "Truy cập và lỗi: Request và lỗi 5xx một giờ qua của từng site" (Bảng điều khiển: `/` với admin, `/dashboard` với user thường, qua `userPath`).

Số đếm trên ô là số **đang chạy trong phạm vi của người xem**, cùng nguồn với menu: "Site" là số site `active` trong tổng quan, "Cơ sở dữ liệu" là `db_count`, "Sự cố" là số sự cố đang mở. Với admin, phạm vi là cả máy.

Cột bên, ba thẻ:
- **Tài khoản**: Người dùng, Vai, Gói (liên kết tới `/packages` với admin; chữ thường với user thường, vì `/packages` là màn admin), Máy (mono), Stack ("LEMP · nginx · PHP 8.3", hoặc "chưa cài stack"), TPAN (mono).
- **Đang dùng**: Site, DB rời, Dung lượng của **riêng tài khoản này** so với gói, mỗi dòng một `UsageMeter`. Đây là con số gói dùng để chặn (`internal/store/users.go:298-299` đếm cả thứ trong thùng rác), nên có thể khác số trên ô. Dưới thẻ hai dòng chữ phụ: "Tính như gói tính: cả site và DB rời trong thùng rác." và "∞ là không giới hạn theo gói."
- **Cần xem**: "N sự cố đang mở" (dấu lỗi `sm`, chữ `--fail-text`, liên kết tới `/incidents`), hoặc "Không có sự cố đang mở."

Nguồn dữ liệu, không gọi API mới: phiên (`/api/v1/session`: tên, vai, tên máy, bản TPAN); tổng quan (`/api/v1/overview`: stack, sự cố, `quota` của user thường, `db_count`). Riêng admin không có `quota` trong tổng quan: gói và mức dùng của admin lấy từ hàng của chính họ ở `GET /api/v1/users` (route chỉ admin; **user thường không bao giờ gọi**, truy vấn bị tắt bằng `enabled: isAdmin`). Phiên "xem như" là vai user nên đi đường `quota`.

**Bảng điều khiển** (thợ T1). Đầu màn: tóm tắt như bản 1; `meta` là nhãn stack (LEMP hoặc LAMP, tên web server, "PHP x.y", hoặc "chưa cài stack"; mọi vai, như bản 1 vẽ nhãn stack trên thanh đầu trang cho mọi vai); `actions` là "Kiểm lại" (admin). Hai cột có xuống dòng như bản 1 (cột chính `999 1 560px`, cột bên `1 1 340px`).
- Thẻ "Đường đi request của từng site", chú giải bốn trạng thái nằm ở phần `actions` của thẻ. Bản đồ tuyến giữ nguyên hình, ga, nét, cách bấm ga mở bằng chứng. Cột tên site: `flex: 0 1 320px; min-width: 220px`; tên là liên kết sans 14/500 màu `--ink`, **một dòng**, cắt bằng dấu ba chấm, có `title`, mang `data-site-name`; dòng request bên dưới 12 px `--muted`. Nhãn ga 12/500 `--muted` viết thường. Hàng cao 52 px. Bề rộng tối thiểu của bản đồ 720 px trong khung cuộn riêng. Khối bằng chứng của ga đang mở nằm trong chính thẻ này.
- Thẻ "Máy" (bốn `MetricBar`), thẻ "TPAN tự kiểm" (mỗi phép kiểm một `Disclosure`, đóng sẵn trừ dòng lỗi), thẻ "Lịch sử máy" (mã job mono 13 `--muted`). User thường: thẻ "Hạn mức gói" thay hai thẻ đầu.
- Cột bên. "Sự cố đang mở": mỗi sự cố một thẻ gọn: dấu lỗi, tiêu đề **tối đa hai dòng** (`line-clamp-2`, đủ chữ trong `title`), một dòng phụ, nút chữ "Mở sự cố"; lệnh cho Claude Code nằm trong `Disclosure` **đóng sẵn** (chỉ admin). "Cảnh báo": **một thẻ**, mỗi cảnh báo một `Disclosure` (dấu cảnh báo, nơi, câu tóm tắt một dòng; mở ra là nguyên nhân và bằng chứng). "Chờ bạn duyệt" như bản 1 (admin).

**Trang một site** (`/sites/:domain`, thợ T2). Đầu màn: "Site / <tên miền>"; `h1` là tên miền, **sans**; `meta`: web server, PHP, chủ (admin), nhãn tạm khoá, nhãn thùng rác. Dưới đó là `Tabs`:

| Tab | Nội dung | Hiện khi |
|---|---|---|
| Tổng quan | thẻ "Đường đi request" (tuyến của site, chú giải, dòng "Hỏng ở …", "Kiểm lại", giờ cập nhật; nút "Request tới site này đi đường nào" của M14, kết quả hiện trong thẻ, **chỉ khi** `hasLogs` như bản 1); thẻ "Request một giờ qua" (**chỉ khi** `hasLogs`, `screens/site/index.tsx:277,369,373`); thẻ "Sự cố của site" | luôn |
| Phép kiểm | các nhóm theo chặng như bản 1, mỗi phép kiểm một dòng mở ra được (`CheckItem`); nhóm cô lập gộp (dưới) | luôn |
| Log | `LiveLog` | site đang chạy và đã cài stack |
| PHP | `PhpBlock` | như trên |
| Backup | `Backups` | luôn |
| Cài đặt site | chế độ `.htaccess` (chỉ Apache); tạm khoá; "Tên và đường dẫn"; đưa vào thùng rác | luôn |

- Bấm một ga trên tuyến: chuyển sang tab Phép kiểm, mở các phép kiểm của chặng đó và đưa focus tới tiêu đề nhóm (bản 1 làm đúng việc này trên cùng một trang). Ga đang chọn là state của component cha (`SiteView`), vì nội dung tab bị gỡ khi rời tab.
- "Sự cố của site": các sự cố đang mở, lọc từ `overview.incidents` (cùng truy vấn với khung trang, không gọi thêm), lấy sự cố có `target` bằng tên miền **hoặc** có `check_id` là phép cô lập dính tới site này (`isolation:<domain>:<khác>` hay `isolation:<khác>:<domain>`, tách như hàm `groupIsolation` dưới đây): mỗi ID cô lập chỉ được ghi một lần, dưới tên site gặp trước (`internal/health/health.go:133-142`), nên lọc riêng theo `target` sẽ sót. Mỗi dòng là dấu lỗi, tiêu đề, giờ mở, liên kết tới `/incidents/:id`; không có thì "Không có sự cố đang mở."
- "Tên và đường dẫn": giá trị vẫn là `MonoText` (đúng là thứ để chép).
- **Gộp phép kiểm cô lập.** Phép kiểm có `id` dạng `isolation:<từ>:<tới>` (chặng `wordpress`) không còn in từng dòng. Hàm thuần `groupIsolation(checks, domain)` ở `screens/site/isolation.ts` (chỉ `import type`) trả `null` khi không có phép nào, hoặc:

  ```ts
  type IsolationPair = { other: string; out?: CheckResult; in?: CheckResult }; // out: isolation:<domain>:<other>; in: isolation:<other>:<domain>
  type IsolationGroup = {
    others: number;            // số site khác (đếm theo tên miền, không trùng)
    total: number;             // số phép kiểm cô lập (mỗi cặp có hai chiều)
    bad: number;               // số phép có status khác "pass"
    status: Status;            // xấu nhất, theo thứ tự pass < unknown < warn < fail (cùng thứ tự với route.ts)
    pairs: IsolationPair[];    // xếp: cặp có phép không đạt trước, rồi theo tên
    openIds: string[];         // id các phép cần mở sẵn: mọi phép có status "fail"
  };
  ```

  Cách tách một `id`: bỏ tiền tố `isolation:`, rồi `rest.split(":")` phải ra **đúng hai phần** `[từ, tới]` (tên miền không chứa dấu hai chấm); so **bằng** từng phần với `domain`: `từ === domain` là chiều `out`, `tới === domain` là chiều `in`, không phần nào bằng thì phép đó không thuộc site này và bị bỏ qua; ra số phần khác hai thì cũng bỏ qua. Không dùng `startsWith` (với `a.com`, `isolation:a.com.vn:b.com` sẽ bị nhận nhầm). Giao diện: một `Disclosure` có dòng tóm tắt "Cô lập với N site khác: đạt (X phép kiểm)" khi `bad === 0`; khác đi thì "Cô lập với N site khác: K trên X phép kiểm không đạt". (Đặc tả viết "phép thử"; trong từ vựng của dự án, "phép thử" là từng lệnh bên trong một phép kiểm cô lập, mỗi phép kiểm có năm phép thử, mục 6.13 của KE-HOACH. Đếm phép kiểm thì đúng với dữ liệu giao diện có; ghi ở `docs/thay-doi/M16.md`, Q19.) Mở ra: mỗi site khác một nhóm, hai dòng "site này → <kia>" và "<kia> → site này", mỗi dòng là một `CheckItem` mở ra được bằng chứng. `openIds` khác rỗng thì khối tự mở và **chỉ** các phép trong `openIds` mở sẵn. Dấu của chặng WordPress vẫn tính trên **mọi** phép kiểm của chặng, kể cả các phép đã gộp (không đổi cách tính trạng thái).

**Bảo mật** (`/security`, thợ T3). Hai thẻ thay cho một trang dài:
- "Kiểm tra bảo mật máy": nút "Kiểm lại" ở `actions`; mỗi dòng một `CheckRow` (phiên chính đổi `CheckRow` ở đợt 1 thành một dòng mở ra được: dòng lệnh đầu tiên dưới tên dòng bỏ đi vì lệnh nằm trong bằng chứng; `EvidenceBlock clamp={20}`; nút "Bật tường lửa", "Cài fail2ban" và câu "TPAN không sửa dòng này" vẫn nằm ngoài phần mở ra, chỉ hiện khi dòng chưa đạt). T3 chỉ quyết dòng nào mở sẵn: **đóng sẵn trừ dòng lỗi** (bản 1 mở cả dòng cảnh báo).
- "Cô lập giữa các site" (`id="bm-co-lap"` giữ nguyên): lời dẫn, nút "Áp bản vá cô lập"; ma trận như bản 1 (ô đang chọn viền `--accent` thay `--ink`; nhãn site ở đầu cột và đầu hàng dùng sans); hai nhóm "Phép kiểm cấp máy" và "Phép kiểm của từng site" là hai `Disclosure` có dòng tóm tắt "N phép kiểm: A đạt, B chưa đạt", **đóng sẵn**, tự mở khi nhóm có dòng lỗi; bảng chế độ `.htaccess` (Apache) là `TableList`.
- Bản xem trước và khối "Cổng sẽ được giữ mở" như bản 1.

**Cài đặt** (`/settings`, thợ T4): mỗi mục một thẻ: Mật khẩu; Phiên đang đăng nhập; Phiên bản và cập nhật (`id="phien-ban"`, chỉ admin); Giao diện (ngôn ngữ, sáng/tối/theo hệ điều hành, Gợi ý: cùng state với menu tài khoản); AI và bộ não (admin); Agent và MCP (admin). Khối "Phiên bản và cập nhật" của M15 giữ nguyên chức năng và mọi chuỗi mà kịch bản M15 dựa vào, trừ hai nhãn chữ hoa đổi thành viết thường (danh sách ở mục 3.11). `MatKhau.tsx`, `BoNao.tsx` chuyển từ `dang-nhap/Field` sang `TextField` của `components/Field.tsx`. Khoá `localStorage` `tpan.brain.sshUser` (`BoNao.tsx:38`) giữ nguyên tên.

**Các màn còn lại** đổi theo cùng thành phần, không đổi bố cục ngoài quy ước chung:

| Màn | Thợ | Ghi chú riêng |
|---|---|---|
| Site (danh sách) | T2 | tên miền sans, là liên kết; cột Việc (admin) là một nút chữ Tạm khoá hoặc Mở khoá; chân bảng "N site" |
| Cập nhật | T4 | bốn thẻ; khối TPAN dùng chung với Cài đặt |
| Lịch sử, trang job | T5 | bảng trong thẻ; trang job: các bước là danh sách trong thẻ, output đang chạy **không** `clamp`; `JobList.tsx` chuyển từ `dang-nhap/SelectField` sang `SelectField` của `components/Field.tsx` |
| Sự cố, trang sự cố | T5 | ba cách sửa là ba thẻ hoặc tab trong thẻ; bằng chứng `clamp={20}` |
| SSL, Backup, Cron, Thùng rác | T6 | bảng trong thẻ, form trong thẻ |
| Stack, Gói, Tạo site, Đăng nhập | T7 | Tạo site giữ `id` các ô (mục 3.11); Đăng nhập là một thẻ giữa trang trên nền `--paper`; `goi` chuyển sang `components/Field.tsx`; `dang-nhap/Field.tsx`, `SelectField.tsx` thành dòng xuất lại (mục 3.4) |
| Ngăn AI, thanh lệnh, Phiên AI, Plan chờ duyệt | T8 | bỏ nút nổi; thanh lệnh thêm dòng "Site: <tên miền>" cho từng site đang chạy (đọc `useOverview()`), lọc bỏ dấu như các lệnh khác; phần chọn route của thanh lệnh và `views.ts` đã được phiên chính sửa ở đợt 1 (mục 3.6), T8 không đổi logic đó; dòng mã trong ngăn AI dùng `Code block` |
| Duyệt Plan (`PlanReview`), lỗi (`ErrorNote`), `CheckItem`, `CheckRow` của Bảo mật, `Note` của Cài đặt | phiên chính | diff vẫn có dấu `+` và `−`; nút chạy mang `data-plan-run` |

### 3.8 API: trường `db_count`

`GET /api/v1/overview` thêm `"db_count": <số nguyên>`:

> `db_count` = số site `status = active` trong phạm vi của người gọi **cộng** số DB rời `status = active` trong phạm vi đó.

- Phạm vi như mọi phần khác của tổng quan: admin thấy mọi thứ, user thường chỉ của mình (`scopeOf`). Trong phiên "xem như" là phạm vi của user được xem.
- Tính trong `handleOverview` từ thứ đã đọc sẵn (`sites`) và `Store.StandaloneDBs(ctx, scope)`. Không gọi MariaDB.
- Vì sao "đang chạy" chứ không phải "mọi hàng": mục Site trong menu đếm site đang chạy và Thùng rác có số riêng; DB của site trong thùng rác đi theo site đó, DB rời trong thùng rác đã bị `DROP`. Màn Cơ sở dữ liệu vẫn liệt kê cả hàng trong thùng rác, và chân bảng nói rõ con số ấy.
- Giao diện: `Overview.db_count?: number` (server cũ không gửi thì menu không hiện số). Số ở menu, ở ô "Cơ sở dữ liệu" của trang Công cụ và ở chân bảng của màn Cơ sở dữ liệu là **cùng một định nghĩa**.
- Không đổi trường nào khác. Cột "DB rời" của màn Người dùng vẫn đọc `usage.dbs`: số DB rời của user **tính cả DB rời trong thùng rác**, đúng con số gói dùng để chặn (`internal/store/users.go:298-299`); cột "Site" cũng vậy. Bản 1 gọi cột là "DB" nên người xem tưởng nó đếm mọi DB. Chân bảng của màn Người dùng thêm câu "Site và DB rời tính như gói tính: cả thứ trong thùng rác.", trang Công cụ có câu tương tự dưới thẻ "Đang dùng" (mục 3.7), để không ai đặt con số này cạnh `db_count` mà tưởng chúng cùng một luật.

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
| `make anh-m16` | bộ ảnh `-bo m16`: ảnh mọi màn 1440 × 900 sáng và tối, sáu màn chính 390 × 844, vào `docs/bang-chung/M16/`; trên **mọi** màn của danh sách: không lỗi console, không cuộn ngang ở 1440 và 390 px, đo cỡ chữ nhỏ nhất và ô viền màu chữ, đi hết bằng phím Tab, mở bản tiếng Anh ở 1440 và 390 px tìm chữ bị cắt (mục 7) |
| `make kieu-dang DIR=…` | luật B6, B11 cho riêng các thư mục ghi trong `DIR`, kể cả thư mục còn trong danh sách "chưa đổi": lệnh kiểm của thợ đợt 2, 3 |
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
| `[role=tab]` có `data-tab="<giá trị>"` (không đè `id`, mục 3.4) | trang một site | e2e, script chụp |
| `header button[aria-haspopup="menu"]`, `main button[aria-haspopup="menu"]` | nút mở menu tài khoản, nút ba chấm | script chụp, e2e |

Hai nhãn chữ hoa mà kịch bản M15 đang tìm ("SHA256 SẼ KIỂM", "LƯỢT GẦN NHẤT", ở `scripts/anh-readme/m15_that.go:100,156` và `main.go` bộ `m15`) đổi thành viết thường khi T4 đổi `i18n/phien-ban.ts`; phiên chính sửa hai chỗ trong script ngay khi nhận phần của T4, trong cùng commit.

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
| B1 | `db_count` của một người gọi bằng đúng số hàng `status = active` mà `GET /api/v1/databases` trả cho chính người đó | `TestOverviewDBCountMatchesDatabases` (`internal/server`), với admin và với một user thường có một site đang chạy, **một site trong thùng rác**, một DB rời đang chạy và một DB rời trong thùng rác (lỗi dễ gặp nhất là đếm `len(sites)` gồm cả site trong thùng rác) |
| B2 | `db_count` của user thường không tính site hay DB của người khác; phiên "xem như" cho đúng số của user được xem | `TestOverviewDBCountScoped` (`internal/server`) |
| B3 | Các cặp sau đạt ngưỡng ở cả sáng và tối, giá trị đọc thẳng từ `web/src/styles.css` (thiếu biến nào trong danh sách cũng là lỗi). Chữ ≥ 4,5: `ink` trên `paper`, `surface`, `soft`, `pre`, `accent-tint`; `ink-soft` trên `surface`, `paper`, `soft`; `muted` trên `paper`, `surface`, `soft`, `pre`, `accent-tint`; `accent` trên `surface`, `paper`, `soft`, `accent-tint`; `accent-hover` trên `surface`, `paper`, `soft`; `on-accent` trên `accent`, `accent-hover`; `fail-text` trên `paper`, `surface`, `soft`, `pre`; `ok-text` trên `paper`, `surface`, `soft`; `on-fail` trên `fail`; `on-ink` trên `ink`. Hình ≥ 3: `ok`, `fail`, `unknown` trên `paper`, `surface`, `soft`; `warn-border` trên `warn`; `route`, `route-dash` trên `surface`; `field` trên `surface`, `soft`, `paper`; `ink` trên `track` | `TestTuongPhan` (web) |
| B4 | `--accent-tint` đúng bằng `--accent` pha 10% (sáng) và 16% (tối) trên `--surface` | `TestAccentTint` (web) |
| B5 | Hai chế độ khai đủ cùng một bộ biến màu (không biến nào chỉ có ở một chế độ) | `TestHaiCheDoCungBien` (web) |
| B6 | Trong `web/src`: không cỡ chữ nào ngoài {12; 12,5; 13; 14; 16; 18; 24} px; không `border-ink`, `border-card-border`, `border-line-dash`, `uppercase`, `tracking-[0.08em]`, `tracking-[0.06em]`; `border-line` chỉ trong `components/` và `Layout.tsx`; không mã màu hex, `rgb(`, `hsl(` trong file `.tsx`/`.ts`; không `rounded-[`, không `shadow-` ngoài `shadow-pop`; `font-mono` chỉ ở các file được phép (mục 3.2); `bg-ink` chỉ ở danh sách cho phép (dải "đang xem như", dòng log đang chọn, ô báo bản mới, phần đầy của thanh). **Danh sách "chưa đổi" giảm dần:** thư mục màn chưa tới lượt đổi nằm trong một danh sách ghi trong test và không bị kiểm các luật trên; thư mục nào trong danh sách mà đã sạch thì test báo lỗi "đã đổi xong, gỡ khỏi danh sách", nên danh sách chỉ ngắn đi; phiên chính gỡ thư mục khi nhận phần của thợ; từ bước dọn cuối mốc danh sách rỗng, test quét toàn bộ. Thợ kiểm riêng thư mục của mình bằng `make kieu-dang DIR=…` (biến môi trường `KIEU_DANG_THU`), không sửa file test | `TestKieuDang` (web) |
| B7 | Không file nào trong `web/src` dùng `dangerouslySetInnerHTML`, `.innerHTML`, `eval(`, `new Function(`. Không có chuỗi `http://`, `https://` trong mã (đã bỏ chú thích) của `web/src` và trong `web/index.html`; trong `web/public/` chỉ được có không gian tên `http://www.w3.org/` của SVG. (Lời gọi `fetch(`, `new EventSource(` cùng origin như `api/client.ts` vẫn được.) | `TestKieuDang` (web), luật 7 |
| B8 | Mọi menu thả đi qua `components/Menu.tsx`: `@radix-ui/react-dropdown-menu` chỉ được import ở đúng file đó, và file đó có `modal={false}` | `TestKieuDang` (web), luật 8 |
| B9 | Mọi đích của ô công cụ là một đường dẫn có trong `routes.tsx`; ô trỏ tới màn admin không hiện với user thường | `tsc --noEmit`: trường `path` của ô có kiểu `ScreenPath` (hợp các đường dẫn của `routes.tsx`, mục 3.6, 3.7), viết sai là lỗi biên dịch; cờ admin tra thẳng trong `routes.tsx` lúc chạy nên không có bản chép để lệch. e2e phần D khẳng định trang Công cụ của user thường không có liên kết tới màn admin |
| B10 | `groupIsolation`: không phép nào thì `null`; N site khác, 2N phép đều đạt thì `bad = 0`, `openIds` rỗng; một phép lỗi thì `openIds` chỉ có đúng `id` đó và cặp ấy đứng đầu; phép không thuộc site bị bỏ qua; tên miền là đuôi trùng (`a.com`, `aa.com`) hay đầu trùng (`a.com`, `a.com.vn`) của tên khác không bị nhận nhầm; `id` có số phần khác hai sau khi tách bằng `:` bị bỏ qua | `isolation.test.ts` (`node --test`) |
| B11 | Chuỗi nhãn trong `web/src/i18n/*.ts` không viết hoa cả câu (không chữ thường nào, và có ít nhất hai từ toàn chữ hoa mỗi từ từ hai chữ cái: bắt "BẰNG CHỨNG", "MÃ JOB"; tha "PHP 8.3", "TPAN", "HTTP 502"), trừ danh sách cho phép ghi trong test. Cùng cơ chế danh sách "chưa đổi" giảm dần như B6, theo file chuỗi | `TestNhanKhongVietHoa` (web) |
| B12 | Bản tiếng Anh có đủ khoá như bản tiếng Việt | `tsc --noEmit` (kiểu của `defineStrings`, như bản 1) |
| B13 | Không màn nào (mọi màn trong danh sách ảnh của `make anh-m16`) có chữ dưới 12 px, ô viền màu chữ, hay cuộn ngang cả trang ở 1440 và 390 px, đo trên trình duyệt thật | `make anh-m16`; các ngưỡng riêng của từng màn ở `make do-giao-dien NGUONG=1` |
| B14 | Mở từng màn, mở menu tài khoản, một menu ba chấm, thanh lệnh, ngăn AI, một bản xem trước Plan và thẻ theo dõi job: 0 lỗi console, 0 vi phạm CSP (trừ đúng dòng CSP đã biết của Radix Dialog khi hộp hỏi lại đang mở) | `make anh-m16` (demo), `make e2e-m16` (máy ảo, hai vai) |
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

- **Từ v0.2.0 lên.** Không migration, không setting mới, không đổi file nào trên máy chủ ngoài file chạy `tpan`. Hành vi của site đang có không đổi (không đụng vhost, pool, cron). Cookie phiên và mật khẩu không đổi: phiên đăng nhập cũ còn dùng được. `localStorage` giữ nguyên bốn khoá (`tpan-theme`, `tpan-lang`, `tpan-tips`, `tpan.brain.sshUser`) và cùng giá trị: người đang dùng chế độ tối, tiếng Anh vẫn thấy đúng như cũ sau khi lên.
- **Tab đang mở lúc nâng.** File JS của mỗi màn có tên theo mã băm; sau khi lên, tab cũ bấm sang một màn chưa tải sẽ hỏi một file không còn. `index.html` được gửi với `Cache-Control: no-cache`, nên **tải lại trang** là đủ. Đây là hành vi có sẵn từ các bản trước (khối cập nhật của M15 có nút "Tải lại trang"); M16 không làm nó tệ hơn và không sửa nó. e2e kiểm đúng điều cam kết: tải lại thì vào được giao diện mới bằng phiên cũ.
- **Đường dẫn cũ.** `/` với admin vẫn là Bảng điều khiển. Mọi đường dẫn khác giữ nguyên. Đường dẫn mới: `/tools`, `/dashboard`. User thường có dấu trang ở `/` sẽ tới `/tools`.
- **Lùi.** `tpan self-update --rollback` hoặc nút "Quay về bản trước" về v0.2.0 được: lõi của M15 chỉ chặn khi file cũ không mở được DB, mà M16 không đổi DB. Bản cũ bỏ qua `db_count`. Không có dữ liệu nào do M16 ghi mà bản cũ không hiểu. Dấu trang tạo trên bản 2 (`/tools`, `/dashboard`) rơi vào trang "Không có trang này" của giao diện bản cũ; `?tab=` bị bỏ qua. Chấp nhận được: không mất dữ liệu, chỉ là một trang 404 của giao diện.
- **Mốc khác.** Trước khi M16 vào `dev`, các mốc khác không sửa `web/` (luật 4 của LO-TRINH). Sau đó họ dùng thành phần ở mục 3.4; test B6 chặn kiểu dáng bản 1 quay lại.

## 7. Kế hoạch kiểm thử

### Unit (`make check`)

- Go: B1, B2 (server); B3 đến B8, B11 (gói `web`, có danh sách "chưa đổi" giảm dần nên xanh ở cuối mỗi đợt); test demo sửa theo ba site mới; mọi test có sẵn.
- `node --test`: B10.
- `tsc --noEmit`: B9, B12 và kiểu của mọi màn.

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

`make anh-m16`: ảnh mọi màn ở 1440 × 900, sáng và tối (mọi route của `routes.tsx`, mỗi tab của trang một site, trang site đang hỏng, Công cụ, Bảng điều khiển, Cơ sở dữ liệu và trang "không có quyền" của user thường, trang 404), cộng các lớp nổi: menu tài khoản đang mở, thanh lệnh đang mở, ngăn AI đang mở, một bản xem trước Plan (Tạo site), thẻ theo dõi job (xuất `.sql` một DB rồi chạy). Sáu màn chính (Bảng điều khiển, Công cụ, Site, trang một site, Cơ sở dữ liệu, Người dùng) có thêm ảnh 390 × 844. Trên **mọi** màn của danh sách (không chỉ màn có ảnh 390), script khẳng định:

- 0 lỗi console (trừ đúng dòng CSP đã biết khi hộp hỏi lại đang mở), không cuộn ngang cả trang ở 1440 và ở 390 px;
- không chữ nào dưới 12 px và không ô nào viền màu chữ chính (chạy `do-giao-dien.js` ngay trên trang đó);
- bàn phím: gõ Tab đi hết màn, (a) phần tử đang focus luôn nhìn thấy được, không bị thanh đầu trang dính che, (b) mọi nút, liên kết, ô nhập của `main` đều được tới; trên Cơ sở dữ liệu, mở menu ba chấm bằng Enter, đi bằng mũi tên, đóng bằng Escape, focus về nút; trên trang một site, đổi tab bằng mũi tên;
- tiếng Anh: mở ở 1440 và 390 px, không cuộn ngang và không phần tử chữ nào có `scrollWidth > clientWidth` mà không được phép cắt (phần tử cắt có chủ đích mang `title`).
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

Model và effort theo luật 8 của LO-TRINH, với thay đổi tạm của chủ dự án lúc 09:09 ngày 21/11 (hết hạn mức Fable): phiên chính và thợ soát dùng **opus, effort max**; thợ viết mã giữ như luật 8. Toàn bộ mốc là phần **không nhạy cảm** (không đụng mã chạy bằng root, đăng nhập, phân quyền, bí mật, migration); riêng trường `db_count` nằm trong `handleOverview` cạnh mã lọc theo chủ nên phiên chính tự viết. Không hai thợ chung file. File dùng chung chỉ phiên chính sửa.

| Đợt | Phần | File được sửa | Ai | Model, effort |
|---|---|---|---|---|
| 0 | Nghiên cứu, bản thử, thiết kế này | `docs/thay-doi/M16-thiet-ke.md`, `docs/bang-chung/M16-nghien-cuu/` | phiên chính | fable max, rồi opus max |
| 0 | Soát thiết kế | chỉ đọc; phát hiện ghi cuối file này | thợ soát | opus, max (lần chạy bằng fable dừng vì hết hạn mức, chưa ghi gì) |
| 1 | Nền bản 2: token, thành phần, khung trang, đường dẫn và logic chọn route của thanh lệnh, chuỗi khung; `db_count` và test B1, B2; ba màn Người dùng, Cơ sở dữ liệu, Công cụ; `PlanReview`, `ErrorNote`, `CheckItem`, `CheckRow`, `Note`; đổi class nét tuyến ở hai `RouteMap.tsx`; script chụp và đo; `docs/GIAO-DIEN.md` | `web/src/styles.css`, `Layout.tsx`, `routes.tsx`, `main.tsx`, `components/**`, `api/**`, `i18n/index.tsx`, `i18n/khung.ts`, `i18n/m13-chung.ts`, `i18n/agent.ts`, `i18n/duyet-plan.ts`, `i18n/nguoi-dung.ts`, `i18n/co-so-du-lieu.ts`, `i18n/cong-cu.ts`, `screens/nguoi-dung/**`, `screens/co-so-du-lieu/**`, `screens/cong-cu/**`, `screens/trang-chu/**`, `screens/duyet-plan/**`, `screens/site/ErrorNote.tsx`, `screens/site/CheckItem.tsx`, `screens/bao-mat/CheckRow.tsx`, `screens/cai-dat/Note.tsx`, `screens/bang-dieu-khien/RouteMap.tsx` và `screens/site/RouteMap.tsx` (chỉ hai class nét tuyến), `ai/CommandPalette.tsx` và `ai/views.ts` (chỉ phần chọn route, mục 3.6), `web/tsconfig.json`, `web/package.json`, `internal/server/**`, `scripts/**`, `Makefile`, `docs/**` | phiên chính | opus, max |
| 1 | D1 Dữ liệu demo: ba site tên dài, test demo sửa theo (mục 3.9) | `internal/demo/**` | thợ phụ | sonnet, mặc định |
| 1 | D2 Test gói `web`: B3 đến B8, B11, danh sách "chưa đổi" giảm dần, `KIEU_DANG_THU` | `web/tuong_phan_test.go`, `web/kieu_dang_test.go` | thợ phụ | sonnet, mặc định |
| 2 | T1 Bảng điều khiển | `screens/bang-dieu-khien/**`, `i18n/bang-dieu-khien.ts` | thợ giao diện | sonnet, mặc định |
| 2 | T2 Site: danh sách và trang một site (tab, gộp cô lập, `isolation.ts` và test của nó) | `screens/site/**` trừ `ErrorNote.tsx`, `CheckItem.tsx`; `i18n/site.ts` | thợ giao diện | sonnet, mặc định |
| 2 | T3 Bảo mật | `screens/bao-mat/**` trừ `CheckRow.tsx`; `i18n/bao-mat.ts` | thợ giao diện | sonnet, mặc định |
| 2 | T4 Cài đặt và Cập nhật | `screens/cai-dat/**` trừ `Note.tsx`; `screens/cap-nhat/**`; `i18n/cai-dat.ts`, `i18n/cap-nhat.ts`, `i18n/phien-ban.ts` | thợ giao diện | sonnet, mặc định |
| 3 | T5 Lịch sử, Sự cố | `screens/lich-su/**`, `screens/su-co/**`, `i18n/lich-su.ts`, `i18n/su-co.ts` | thợ giao diện | sonnet, mặc định |
| 3 | T6 SSL, Backup, Cron, Thùng rác | `screens/ssl/**`, `screens/backup/**`, `screens/cron/**`, `screens/thung-rac/**` và bốn file chuỗi cùng tên | thợ giao diện | sonnet, mặc định |
| 3 | T7 Stack, Gói, Tạo site, Đăng nhập | `screens/stack/**`, `screens/goi/**`, `screens/tao-site/**`, `screens/dang-nhap/**` và bốn file chuỗi cùng tên | thợ giao diện | sonnet, mặc định |
| 3 | T8 Ngăn AI, thanh lệnh, Phiên AI, Plan chờ duyệt | `ai/**`, `screens/phien-ai/**`, `screens/plan-cho-duyet/**`, `i18n/ai.ts`, `i18n/phien-ai.ts` | thợ giao diện | sonnet, mặc định |
| 4 | Bước dọn: gỡ bí danh tạm của token, gỡ `dang-nhap/Field.tsx`, `SelectField.tsx` nếu không còn ai import, danh sách "chưa đổi" rỗng | `web/src/styles.css`, `web/kieu_dang_test.go`, `screens/dang-nhap/*Field.tsx` | phiên chính | opus, max |
| 4 | Ảnh, số đo, e2e bước 19 trên `tpan-m16` | `scripts/**`, `docs/bang-chung/M16*`, `docs/thay-doi/M16.md` | phiên chính | opus, max |
| 5 | File giải thích | `docs/giai-thich/16-m16-giao-dien-ban-2.md` | thợ viết | sonnet, mặc định |
| 5 | Soát cuối (đọc diff, thử phá giao diện bằng script chụp) | chỉ đọc; phát hiện trả về phiên chính | thợ soát | opus, max |
| 6 | README đổi ảnh, KE-HOACH mục 10, ghép `origin/dev`, `make check`, đẩy `moc/M16:dev` | `README.md`, `docs/KE-HOACH.md`, `docs/anh/**` | phiên chính | opus, max |

Mỗi thợ đợt 2 và 3 nhận lời giao việc năm ý: việc phải làm (mục của bảng 3.7 dành cho màn đó); danh sách file được sửa; tài liệu phải đọc (`docs/GIAO-DIEN.md` bản 2, mục 3.4, 3.7, 3.11, 3.12 của file này); file hợp đồng (`web/src/components/*`, và ba màn mẫu `screens/nguoi-dung`, `screens/co-so-du-lieu`, `screens/cong-cu` để theo); lệnh kiểm của riêng phần đó: `pnpm -C web typecheck`, `make kieu-dang DIR=<thư mục màn>/,<file chuỗi>` (thợ không sửa file test chung), và với T2 thêm `make test-web`. Thợ không chạy trình duyệt (cổng của script chụp là cố định, bốn thợ chạy cùng lúc sẽ giành cổng); phiên chính chụp sau mỗi đợt.

Phiên chính nhận một phần khi: đọc hết diff của thợ; hai lệnh kiểm xanh; ảnh chụp màn đó ở sáng, tối và 390 px đã xem; số đo của màn đó đạt ngưỡng; gỡ thư mục của màn khỏi danh sách "chưa đổi" của `web/kieu_dang_test.go` rồi `make check` xanh. Thấy thợ phải đoán thì trả lại và sửa file này.

## 9. Phát hiện của thợ soát

Soát ngày 21/11/2026 trên bản thiết kế của commit `b8219f3`, đối chiếu với mã ở cây làm việc. Trong lúc soát, phiên chính đã sửa thêm (chưa commit) mục 3.1, 3.2, 3.3 và đoạn T5, nên S3, S4, S8, S26 dưới đây có thể đã được xử lý một phần; các phát hiện vẫn ghi theo bản đã soát. Tổng: 1 nặng, 9 vừa, 18 nhẹ.

- **S1 · Mức:** nặng
- **Ở đâu:** mục 4 (B6, B11), mục 7 "Unit", bảng 8 (đợt 1 viết B6 và B11; lệnh kiểm của thợ đợt 2, 3; điều kiện nhận một phần)
- **Sai gì:** B6 và B11 quét toàn bộ `web/src`, nên chỉ xanh được khi mọi màn đã đổi xong (cuối đợt 3). Bản thiết kế lại bật chúng từ đợt 1, đòi `make check` xanh mỗi đợt, và đòi `go test ./web/` xanh mới nhận phần của từng thợ đợt 2, 3. Ba điều này không thể cùng đúng.
- **Vì sao:** hiện có 35 file chứa `tracking-[0.08em]`, 33 file `border-ink`, 36 file `text-[11px]`, rải ở mọi thư mục màn và ở `ai/AiDrawer.tsx:31-32`; chuỗi viết hoa còn ở `i18n/ssl.ts:51`, `i18n/bao-mat.ts:25` ("SỬA TAY"), `i18n/phien-ban.ts:31,52`. Sau đợt 1, `go test ./web/` đỏ vì file của T5 đến T8 (đợt 3). Thợ T1 chỉ làm nó xanh được bằng cách sửa file của người khác hoặc sửa file test chung (trái luật "không hai thợ chung file"). `agent-dev/TU-CHAY-M16.md:97` đòi "Mỗi đợt: `make check`". Danh sách cho phép sẵn có của B11 cũng không giúp được: dùng nó làm danh sách chờ thì nhiều thợ cùng sửa một file test, phạm đúng luật đó.
- **Đề nghị:** cho B6, B11 một danh sách thư mục "chưa đổi" ghi trong test, phiên chính xoá dần khi nhận từng phần (thợ không sửa test); thợ chạy test giới hạn ở vùng của mình (ví dụ biến môi trường `KIEU_DANG_DIR`). Ghi rõ từ đợt 4 test quét toàn bộ, không còn ngoại lệ.

- **S2 · Mức:** vừa
- **Ở đâu:** 3.8 (gạch cuối), 3.7 Công cụ (thẻ "Đang dùng"), 3.7 Người dùng (cột Site, DB rời)
- **Sai gì:** Bản thiết kế nói `usage.dbs` "đúng là số DB rời". Thật ra `usage.sites` và `usage.dbs` đếm cả hàng trong thùng rác. Trang Công cụ đặt chúng cạnh số "đang chạy" (`db_count`, số site), nên cùng một trang hiện hai luật đếm khác nhau mà không nói ra. Đây đúng là loại "số lệch" (nguyên nhân 7) mà mốc này phải sửa.
- **Vì sao:** `internal/store/users.go:298-299` đếm `COUNT(*) FROM sites WHERE owner` và `COUNT(*) FROM databases WHERE owner`, không lọc status. Demo có `khach_b_cu` trong thùng rác (`internal/demo/m13.go:255`): màn Người dùng và trang Công cụ của khach_b ghi "DB rời 1", trong khi chân bảng Cơ sở dữ liệu của chính họ ghi "đều là DB của site, 1 trong thùng rác", và `db_count` bỏ DB đó với lý do "đã bị DROP" (3.8). Với admin: ô "Site" đếm mọi site đang chạy của cả máy (7 ở demo mới), còn "Đang dùng: Site" là site của riêng admin, kể cả `old.example.net` trong thùng rác (3).
- **Đề nghị:** ghi định nghĩa của từng con số trên trang Công cụ và màn Người dùng (mức dùng theo gói tính cả thùng rác, như luật chặn của gói) và thêm một dòng chú thích dưới thẻ "Đang dùng". Sửa câu "đúng là số DB rời". Nói rõ với admin: ô có số là của cả máy, thẻ "Đang dùng" là của tài khoản.

- **S3 · Mức:** vừa
- **Ở đâu:** B9, 3.7 Công cụ (`screens/cong-cu/tools.ts`), T5 ở mục 2
- **Sai gì:** B9 đối chiếu ô công cụ với `routes.tsx`, nhưng `tools.test.ts` chạy bằng `node --test` không nạp được `routes.tsx` để lấy đường dẫn và cờ `admin`. Mục 3.7 cũng không ghi `tools.ts` chỉ được `import type`.
- **Vì sao:** nếu `tools.ts` lấy chữ từ `i18n/cong-cu.ts` thì kéo theo `i18n/index.tsx` (`defineStrings`), file đó không chạy được dưới `node --test`; mục 2 (T5) chỉ cho file được test `import type`. Danh sách đường dẫn và cờ `admin` lại nằm trong `routes.tsx`. Node chỉ bỏ kiểu cho đuôi `.ts`, `.mts`, `.cts`, không nhận `.tsx`: điều này mình lấy từ tài liệu "Modules: TypeScript" của Node và từ chính đoạn "Thử lần hai" mới thêm ở T5, chưa chạy thử vì phiên soát không được chạy `node`. Dò cờ `admin` trong object nhiều dòng của `routes.tsx` (ví dụ dòng 105-110) bằng regex thì rất dễ sai.
- **Đề nghị:** tách bảng (đường dẫn, admin) ra một file `.ts` thuần để `routes.tsx` dùng lại, hoặc đổi `routes.tsx` thành `routes.ts` (file này không có JSX). Ghi rõ `tools.ts` chỉ `import type` và nhãn của ô là khoá i18n.

- **S4 · Mức:** vừa
- **Ở đâu:** 3.2 (gạch 3), 3.12 (dòng `font-mono text-[12px]`), 3.4
- **Sai gì:** Các màn được bảo dùng `<Code>` ("chỉ để đọc") thay cho `font-mono`, nhưng bảng hợp đồng 3.4 không có `Code` và `web/src/components/` cũng chưa có file này. Khối mono nhiều dòng mà không phải bằng chứng (diff của `PlanReview`, dòng mã trong ngăn AI) cũng chưa có thành phần nào.
- **Vì sao:** B6 chỉ cho `font-mono` ở `components/`, `Layout.tsx`, `site/LiveLog.tsx`, `ai/AnswerText.tsx`. Trong khi đó `screens/duyet-plan/index.tsx:213` (diff), `screens/lich-su/JobDetail.tsx:49,111,281`, `ai/AiDrawer.tsx:229`, `ai/CommandPalette.tsx:191` đang dùng `font-mono`. Cuối 3.12 dặn thợ "thiếu một thành phần thì dừng và báo", nên T5 và T8 sẽ đứng.
- **Đề nghị:** thêm `Code` vào 3.4 (chữ ký; nằm trong dòng hay thành khối; cỡ 13 hay 12,5) cùng một thành phần khối mono cho diff và dòng mã. Hoặc thêm `duyet-plan/index.tsx`, `ai/AiDrawer.tsx` vào danh sách file được dùng `font-mono` của B6.

- **S5 · Mức:** vừa
- **Ở đâu:** 3.11 (dòng `[role=tab]` có `id="tab-<giá trị>"`), 3.4 `Tabs`
- **Sai gì:** Đặt `id` riêng cho nút tab làm khối nội dung tab mất nhãn đọc. Radix vẫn trỏ `aria-labelledby` của khối tới id nó tự sinh, mà id đó không còn trong DOM.
- **Vì sao:** `web/node_modules/@radix-ui/react-tabs/dist/index.mjs` (bản 1.2.1): `TabsTrigger` đặt `id: triggerId` rồi mới rải `...triggerProps` (dòng 124-125), nên `id` truyền vào đè lên; `TabsContent` vẫn đặt `"aria-labelledby": triggerId` (dòng 174), tức id tự sinh `${baseId}-trigger-${value}`.
- **Đề nghị:** dùng móc `data-tab="<giá trị>"` và không đè `id`. Hoặc ghi rõ `TabPanel` truyền `aria-labelledby="tab-<giá trị>"`. Sửa `scripts/anh-readme/m16_anh.go` (đang đọc `.id` của tab đang chọn) theo cách đã chọn.

- **S6 · Mức:** vừa
- **Ở đâu:** 3.6, 3.7 (hàng T8), bảng 8 (T8)
- **Sai gì:** Hai đường dẫn mới `/tools` và `/dashboard` chưa được tính tới ở thanh lệnh và ở nhãn "đang ở" gửi cho AI. Cấu trúc `routes.tsx` cũng chưa diễn tả được "mục Bảng điều khiển trỏ `/` với admin và `/dashboard` với user thường".
- **Vì sao:** `ai/CommandPalette.tsx:63-67` lấy mọi route không có tham số. `ai/views.ts:36-38` chỉ đặt tên cho route có `menu`; route khác thì trả nguyên đường dẫn. Kết quả: admin thấy cả "Mở Bảng điều khiển" (`/`) lẫn "Mở /dashboard"; user thường thấy "Mở Bảng điều khiển" nhưng bấm vào lại tới `/tools` (vì `/` chuyển hướng), cùng một dòng "Mở /dashboard"; ngăn AI ghi "Đang ở /dashboard". `MenuSpec` (`routes.tsx:16-25`) chỉ có một `path` cho mỗi mục. Việc giao cho T8 trong 3.7 không nhắc gì tới chuyện này.
- **Đề nghị:** ghi cách diễn tả trong `routes.tsx` (ví dụ `MenuSpec.to?: (admin: boolean) => string`, hoặc cờ "chỉ hiện trong menu với vai X"). Ghi luật lọc của thanh lệnh (bỏ `/` với user thường, bỏ `/dashboard` với admin) và nhãn của `/dashboard` trong `views.ts`. Giao rõ việc này cho T8 hay cho phiên chính.

- **S7 · Mức:** vừa
- **Ở đâu:** 3.7 Người dùng (cột Việc), 3.6 (gạch "Phiên xem như … (vai user)")
- **Sai gì:** Bản thiết kế cho mọi hàng không phải của mình một menu ba chấm gồm "Tạm khoá hoặc Mở khoá, Xem như, Xoá". Bản 1 còn hai luật nữa: ẩn "Xem như" ở hàng có vai admin, và ẩn "Xoá" ở user `admin`.
- **Vì sao:** `screens/nguoi-dung/index.tsx:214` (phải `!self && n !== "admin"` mới có Xoá), dòng 219 (phải `!self && r.user.role === "user"` mới có Xem như). Core luôn từ chối xoá `admin` (`internal/core/users.go:612-623`). Server không chặn việc xem như một admin (`internal/server/api_m13.go:437-456` không kiểm vai), nên làm đúng theo bản thiết kế sẽ mở được phiên "xem như" mang vai admin. Điều đó trái với 3.6 ("vai user") và với đường dữ liệu của trang Công cụ ("Phiên xem như là vai user nên đi đường quota"). Đặc tả cũng ghi không đổi hành vi của thao tác nào.
- **Đề nghị:** chép lại đúng hai luật của bản 1 vào 3.7: "Xem như" chỉ có ở hàng mang vai user; "Xoá" không có ở user `admin`.

- **S8 · Mức:** vừa
- **Ở đâu:** 3.1 (đoạn "Bỏ: …"), B6, bảng 8 đợt 1
- **Sai gì:** B6 chỉ cấm `border-line-dash`. Nét tuyến liền đang dùng `border-line`, mà sau khi đổi token thì `border-line` vẫn là class hợp lệ, chỉ mang nghĩa mới (`#C5CBD3`). Sót một chỗ đổi thì nét tuyến liền nhạt thành xám 1,6:1 mà không test nào bắt. Hai file phải sửa trong commit đổi token lại không nằm trong danh sách file của đợt 1.
- **Vì sao:** `screens/bang-dieu-khien/RouteMap.tsx:24` (`border-t-2 border-line`) và `screens/site/RouteMap.tsx:62` (`border-solid border-line`). Hai file này thuộc T1, T2 (đợt 2), nhưng 3.1 đòi đổi "trong cùng commit đổi token" (đợt 1), và cột "File được sửa" của đợt 1 không có chúng.
- **Đề nghị:** cho B6 cấm `border-line` ngoài `components/` và `Layout.tsx`. Thêm hai file RouteMap vào danh sách file của đợt 1, chỉ cho phần đổi class.

- **S9 · Mức:** vừa
- **Ở đâu:** mục 7 "Đo và ảnh trên demo" (ba kiểm thêm), B13, B14
- **Sai gì:** Đặc tả đòi: ở 390 px không màn nào cuộn ngang cả trang, đi hết mỗi màn bằng bàn phím, tiếng Anh không chuỗi nào tràn, và chụp mọi màn. Bản thiết kế chỉ đi bàn phím ở 4 màn, kiểm 390 px ở 6 màn, tiếng Anh ở 7 màn, và đo chữ dưới 12 px cùng viền màu chữ ở 9 màn, mà không nói đó là cố ý.
- **Vì sao:** `agent-dev/TU-CHAY-M16.md:89-91`. B13 viết "Không màn nào…", nhưng `measureM16` trong `scripts/anh-readme/m16.go` chỉ đo 9 màn, và `m16_anh.go` chỉ chụp 390 px các màn đánh dấu `narrow`. Danh sách ảnh trong `m16_anh.go` không có ngăn AI đang mở, thanh lệnh đang mở, thẻ theo dõi job, bản xem trước Plan (`PlanReview`) hay menu tài khoản đang mở, dù đặc tả kể chúng trong "Mọi màn còn lại". B14 hứa "mở menu tài khoản" trên demo, nhưng `make anh-m16` không mở nó.
- **Đề nghị:** chạy ba kiểm (bàn phím, 390 px, tiếng Anh) và phép đo cỡ chữ, viền trên mọi màn của danh sách ảnh (cùng vòng lặp, tốn thêm ít). Nếu muốn thu hẹp thì ghi lý do vào `docs/thay-doi/M16.md`. Thêm vào danh sách ảnh các lớp nổi, `PlanReview` và bước mở menu tài khoản.

- **S10 · Mức:** vừa
- **Ở đâu:** bảng 8 (đợt 2, 3), 3.4 (đoạn "Ba file dùng chung nằm trong thư mục màn")
- **Sai gì:** Một số thành phần nằm trong thư mục màn của thợ này nhưng được màn của thợ khác dùng. Bảng chỉ cấm hai thợ chung file, không xử lý phụ thuộc, nên có phần phụ thuộc phần khác cùng đợt hoặc đợt sau.
- **Vì sao:** `screens/cap-nhat/Auto.tsx:11` (T4) dùng `CheckRow` của `screens/bao-mat/CheckRow.tsx` (T3), hai thợ cùng đợt 2, mà T3 lại đổi CheckRow thành `Disclosure` đóng sẵn và bỏ dòng lệnh. `screens/bao-mat/Isolation.tsx:32` (T3) dùng `Legend` của `screens/site/RouteMap.tsx` (T2), cũng cùng đợt 2. `screens/bang-dieu-khien/Side.tsx:21` (T1, đợt 2) vẽ khối "Chờ bạn duyệt" bằng `screens/plan-cho-duyet/PendingPlans.tsx` (T8, đợt 3), nên ảnh nghiệm thu của T1 vẫn còn kiểu bản 1. `screens/dang-nhap/Field.tsx` và `SelectField.tsx` (T7, đợt 3) đang được `nguoi-dung/UserDialogs.tsx`, `goi/index.tsx`, `cai-dat/MatKhau.tsx`, `cai-dat/BoNao.tsx`, `lich-su/JobList.tsx` dùng.
- **Đề nghị:** đưa `bao-mat/CheckRow.tsx`, `Legend` của `site/RouteMap.tsx` và `plan-cho-duyet/PendingPlans.tsx` vào nhóm "file dùng chung do phiên chính sửa" ở đợt 1 (hoặc xếp T4 sau T3). Ghi rõ các màn bỏ `dang-nhap/Field` để chuyển sang `components/Field`.

- **S11 · Mức:** nhẹ
- **Ở đâu:** 3.5 (menu trái `top: 61px`, `calc(100vh - 61px)`; thanh đầu trang)
- **Sai gì:** Con số 61 px chỉ đúng khi thanh đầu trang nằm một hàng. Tên máy là dữ liệu thật, mono 13 px, không có giới hạn bề ngang, nên trong khoảng từ 1024 px tới chừng 1150 px thanh đầu trang có thể xuống dòng, và các mục đầu của menu dính bị nó che.
- **Vì sao:** tính theo trang mẫu: đệm 24 + 24, logo khoảng 84, ô tìm `flex-basis` 280, nhóm bên phải (sự cố, Hỏi AI, tài khoản) khoảng 325, ba khoảng cách 20. Ở 1024 px còn khoảng 227 px cho tên máy, tức chừng 29 ký tự mono 13 px; tên máy VPS dài hơn vậy là chuyện thường (đúng nguyên nhân 6, "thiết kế theo dữ liệu mẫu").
- **Đề nghị:** cho tên máy một `max-width`, dài thì cắt bằng dấu ba chấm và có `title`. Hoặc không cho thanh đầu trang xuống dòng từ 1024 px trở lên (để ô tìm co lại). Ghi điều đó ngay cạnh con số 61.

- **S12 · Mức:** nhẹ
- **Ở đâu:** 3.7 Trang một site (`IsolationGroup.total`, câu "đạt (X phép thử)")
- **Sai gì:** `total` đếm phép kiểm (2N). Nhưng trong tài liệu của dự án, "phép thử" là từng lệnh `test` bên trong một phép kiểm cô lập, mỗi phép kiểm có năm phép thử. Vậy con số in ra sai theo đúng từ vựng của dự án: với 4 site khác, trang ghi "8 phép thử" trong khi máy đã chạy khoảng 40.
- **Vì sao:** `docs/KE-HOACH.md` mục 6.13 ("`Isolated` năm phép thử"); chú thích của `Isolated` ở `internal/checks/site.go:415` ("chạy năm phép thử"); bảng thuật ngữ của GIAO-DIEN mục 8: "Phép kiểm | Check".
- **Đề nghị:** viết "Cô lập với N site khác: đạt (X phép kiểm)" và ghi vào M16.md là đã chỉnh chữ của đặc tả. Hoặc giữ chữ "phép thử" nhưng đếm theo số dòng bằng chứng.

- **S13 · Mức:** nhẹ
- **Ở đâu:** 3.7 (cách tách `id` của phép kiểm cô lập), B10
- **Sai gì:** Ca thử "`a.com`, `aa.com`" là ca đuôi trùng, không bắt được lỗi đầu trùng (`startsWith(domain)` không kèm dấu hai chấm). Câu tả cách tách ("tìm đúng `<domain>` ở đầu hoặc cuối") cũng mơ hồ.
- **Vì sao:** với domain `a.com`, `id` `isolation:a.com.vn:b.com` qua được `rest.startsWith("a.com")`, mà mã viết như vậy vẫn đạt ca `aa.com` của B10.
- **Đề nghị:** ghi cách tách là `rest.split(":")` ra đúng hai phần rồi so bằng. Thêm ca `a.com` với `a.com.vn` vào B10.

- **S14 · Mức:** nhẹ
- **Ở đâu:** B1
- **Sai gì:** Dữ liệu của test không có site nằm trong thùng rác. Vậy lỗi dễ gặp nhất, đếm `len(sites)` gồm cả site trong thùng rác, vẫn qua được test.
- **Vì sao:** `SitesOf` không lọc status (`internal/store/sites.go:91-93`); `core.DBs.List` trả hàng của site trong thùng rác với status `trashed` (`internal/core/databases.go:138-146`).
- **Đề nghị:** thêm một site trong thùng rác vào dữ liệu của B1, cho cả ca admin lẫn ca user thường.

- **S15 · Mức:** nhẹ
- **Ở đâu:** 3.4 `ScreenHeader` ("tóm tắt tối đa 72 ký tự")
- **Sai gì:** Hai trong ba trang mẫu mà bản thiết kế đòi khớp có tóm tắt dài hơn 72 ký tự. Trang mẫu dùng `max-width: 72ch`, tức giới hạn bề rộng dòng, không phải giới hạn độ dài.
- **Vì sao:** `docs/mau-giao-dien/v2/nguoi-dung.html:71` (96 ký tự), `co-so-du-lieu.html:70` (77 ký tự).
- **Đề nghị:** viết "tóm tắt rộng tối đa 72ch". Nếu thật sự muốn giới hạn độ dài thì nói rõ phải rút chữ tóm tắt của mọi màn về 72 ký tự.

- **S16 · Mức:** nhẹ
- **Ở đâu:** 3.11 (đoạn cuối)
- **Sai gì:** Câu viết "Ba nhãn chữ hoa mà kịch bản M15 đang tìm" nhưng chỉ kể hai nhãn.
- **Vì sao:** kịch bản chỉ tìm "SHA256 SẼ KIỂM" và "LƯỢT GẦN NHẤT" (`scripts/anh-readme/m15_that.go:100,156`, `main.go:300`); nhãn viết hoa thứ ba của `i18n/phien-ban.ts` là "SỬA TAY" (dòng 61), kịch bản không tìm nhãn này.
- **Đề nghị:** sửa thành "hai nhãn", để thợ T4 khỏi đi tìm nhãn thứ ba.

- **S17 · Mức:** nhẹ
- **Ở đâu:** mục 6 (gạch "Từ v0.2.0 lên")
- **Sai gì:** Bản thiết kế nói `localStorage` có ba khoá; thật ra có bốn.
- **Vì sao:** khoá `tpan.brain.sshUser` ở `web/src/screens/cai-dat/BoNao.tsx:38`, thuộc vùng của T4.
- **Đề nghị:** thêm khoá này vào danh sách phải giữ nguyên.

- **S18 · Mức:** nhẹ
- **Ở đâu:** 3.6 (`?tab=`), 3.7 Trang một site (bảng tab)
- **Sai gì:** Bản thiết kế không nói ba điều: `?tab=log` hay `?tab=php` khi tab đó đang ẩn (site trong thùng rác, chưa cài stack) thì xử lý ra sao; đổi tab thì ghi địa chỉ bằng `push` hay `replace`. Ngoài ra, thẻ "Request một giờ qua" và nút "Request tới site này đi đường nào" được ghi là "luôn" có, trong khi bản 1 chỉ vẽ chúng khi `hasLogs`.
- **Vì sao:** `screens/site/index.tsx:277` (`hasLogs = info.web !== "" && status === "active"`), dòng 369 và 373. Radix Tabs nhận một `value` không khớp nút nào thì không vẽ khối nội dung nào.
- **Đề nghị:** tab đang ẩn thì coi như `tong-quan`; đổi tab dùng `replace`; hai thứ trên giữ điều kiện `hasLogs` như bản 1.

- **S19 · Mức:** nhẹ
- **Ở đâu:** mục 6 (gạch "Lùi")
- **Sai gì:** Bản thiết kế không nói dấu trang tạo trên bản 2 (`/tools`, `/dashboard`, `?tab=`) sẽ ra sao sau khi lùi về v0.2.0.
- **Vì sao:** v0.2.0 không có hai route đó, nên rơi vào trang "Không có trang này" (`Layout.tsx:426-436`); còn `?tab=` thì bị bỏ qua.
- **Đề nghị:** thêm một câu vào mục 6: chấp nhận được, chỉ là trang 404 của giao diện.

- **S20 · Mức:** nhẹ
- **Ở đâu:** 3.7 Công cụ (thẻ "Tài khoản")
- **Sai gì:** Trang mẫu vẽ "Gói" là liên kết tới `/packages`. Bản thiết kế không nói user thường có liên kết này hay không, trong khi `/packages` là màn chỉ admin vào được.
- **Vì sao:** `docs/mau-giao-dien/v2/cong-cu.html:127`; `routes.tsx:136-141` (`admin: true`).
- **Đề nghị:** ghi rõ "Gói" chỉ là liên kết với admin; với user thường là chữ thường.

- **S21 · Mức:** nhẹ
- **Ở đâu:** 3.7 Cơ sở dữ liệu (chân bảng, việc của từng hàng)
- **Sai gì:** Câu ở chân bảng chỉ có hai dạng ("đều là DB của site", "A của site, B DB rời"). Bản thiết kế không nói câu đó viết thế nào khi N = 0 hoặc khi chỉ có DB rời, cũng không nói hàng DB của site trong thùng rác còn nút "Xuất .sql" hay không.
- **Vì sao:** user chỉ có DB rời, hoặc có mọi thứ trong thùng rác, sẽ thấy "0 cơ sở dữ liệu, đều là DB của site". Bản 1 vẫn có "Xuất .sql" cho hàng trong thùng rác (`screens/co-so-du-lieu/index.tsx:136`, không có điều kiện `!trashed`).
- **Đề nghị:** thêm dạng "đều là DB rời" và dạng cho N = 0. Ghi rõ hàng trong thùng rác vẫn giữ "Xuất .sql".

- **S22 · Mức:** nhẹ
- **Ở đâu:** 3.4 (`Card.flush`, `TableList`)
- **Sai gì:** `Card` có `flush` "cho bảng", nhưng `TableList` lại được tả là tự "nằm trong thẻ", có `footer` và vẽ `EmptyState` "trong thẻ". Không rõ màn phải bọc `TableList` trong `Card flush` hay `TableList` tự vẽ thẻ; làm cả hai thì ra viền đôi.
- **Vì sao:** hai dòng `Card` và `TableList` trong bảng 3.4; 3.7 viết "danh sách là `TableList` trong thẻ".
- **Đề nghị:** chọn một cách và ghi vào chữ ký, ví dụ `TableList` tự vẽ thẻ và nhận thêm `title?`, `actions?`.

- **S23 · Mức:** nhẹ
- **Ở đâu:** 3.7 Trang một site (gạch "Sự cố của site")
- **Sai gì:** Lọc sự cố theo `target === domain` bỏ sót sự cố của phép kiểm cô lập chiều "site kia → site này" khi kết quả được ghi dưới tên site kia. Lúc đó tab Tổng quan vừa vẽ ga WordPress đỏ, vừa ghi "Không có sự cố đang mở".
- **Vì sao:** mỗi ID cô lập chỉ được ghi một lần, dưới tên site gặp trước (`internal/health/health.go:133-142`); còn dấu của chặng WordPress trên trang site tính cả các phép cô lập (3.7).
- **Đề nghị:** lọc thêm các sự cố có `check_id` dạng `isolation:<domain>:*` hoặc `isolation:*:<domain>`.

- **S24 · Mức:** nhẹ
- **Ở đâu:** B7, Đ4
- **Sai gì:** Câu "không `fetch(`, `EventSource(` … trỏ ra `http://` hay `https://`" dễ bị viết thành test cấm luôn chuỗi `fetch(`. B7 cũng không quét `web/index.html` và `web/public/`, đúng chỗ một thẻ nạp font từ ngoài sẽ được đặt (cả ba trang mẫu đều nạp Google Fonts).
- **Vì sao:** `api/client.ts:62` (`fetch(`) và dòng 157, 198 (`new EventSource(`) là lời gọi cùng origin, hợp lệ; `refetch(` có ở hơn 10 màn; `docs/mau-giao-dien/v2/*.html:7` có `<link href="https://fonts.googleapis.com/...">`.
- **Đề nghị:** viết lại B7: không có chuỗi `http://`, `https://`, hay đường dẫn bắt đầu bằng `//` trong mã (trừ chú thích) của `web/src`, `web/index.html`, `web/public/`.

- **S25 · Mức:** nhẹ
- **Ở đâu:** 3.7 Bảng điều khiển (`meta` "chỉ admin")
- **Sai gì:** Đặc tả chuyển nhãn stack vào Bảng điều khiển và trang Công cụ mà không giới hạn vai, và bản 1 cho mọi vai thấy nhãn stack. Bản thiết kế ẩn nhãn này trên Bảng điều khiển của user thường nhưng vẫn hiện nó trên trang Công cụ của họ, mà không nói lý do.
- **Vì sao:** `agent-dev/TU-CHAY-M16.md:60`; `Layout.tsx:259` (`StackTags` vẽ cho mọi vai); thẻ "Tài khoản" của trang Công cụ (3.7) có dòng Stack cho mọi người.
- **Đề nghị:** hiện nhãn stack cho cả hai vai, hoặc ghi lý do vào M16.md.

- **S26 · Mức:** nhẹ
- **Ở đâu:** 3.3 (gạch "Bóng đổ"), 3.4 (`JobTracker`), 3.5 (lớp nổi)
- **Sai gì:** Đặc tả viết "Không bóng đổ, trừ menu thả và hộp thoại". Bản thiết kế thêm bóng cho ngăn AI và thẻ theo dõi job mà không ghi đó là cố ý (thanh lệnh là một hộp thoại nên không tính).
- **Vì sao:** `agent-dev/TU-CHAY-M16.md:57`; mục 1 của `docs/thay-doi/M16.md` không có quyết định nào về chuyện này.
- **Đề nghị:** ghi thành một quyết định trong M16.md (lớp nổi trên nội dung cần tách khỏi nền), hoặc bỏ bóng ở hai chỗ này.

- **S27 · Mức:** nhẹ
- **Ở đâu:** B3
- **Sai gì:** "Mọi cặp chữ trên nền của bảng 3.1" không xác định được, vì bảng 3.1 không ghi cặp nào. Danh sách 53 cặp của T4 lại thiếu hai cặp mà bản thiết kế dùng.
- **Vì sao:** 3.5 đặt số đếm `--muted` trên mục menu đang mở nền `--accent-tint`; dải "đang xem như" là `--on-ink` trên `--ink`. Cả hai cặp không có trong `t4-tuong-phan.go`. Mình tự tính `--muted` trên `--accent-tint`: 5,4 (sáng) và 5,1 (tối), đạt.
- **Đề nghị:** ghi danh sách cặp của B3 (lấy từ T4, thêm hai cặp trên) vào file test và vào mục 3 của GIAO-DIEN.

- **S28 · Mức:** nhẹ
- **Ở đâu:** bản nháp `docs/GIAO-DIEN.md` dòng 5
- **Sai gì:** Bản nháp nói bảng số đo trước và sau nằm ở `docs/giai-thich/16-m16-giao-dien-ban-2.md`, còn bản thiết kế (mục 7) và đặc tả ghi nó vào `docs/thay-doi/M16.md`.
- **Vì sao:** `docs/GIAO-DIEN.md:5`; mục 7 của file này ("Bảng trước và sau … ghi vào `docs/thay-doi/M16.md`"); `agent-dev/TU-CHAY-M16.md:90`.
- **Đề nghị:** trỏ về `docs/thay-doi/M16.md` (sau khi gộp thì về mục 10 của KE-HOACH).

Đã kiểm mà không thấy sai: `scopeOf`, `SitesOf`, `StandaloneDBs` và `DBs.List` lọc theo chủ cùng một cách, nên định nghĩa `db_count` ở 3.8 khớp số hàng `active` của `/api/v1/databases`, và phiên "xem như" mang danh tính của người được xem (`auth.go:144`); `foreignCheck` giấu với user thường các cặp cô lập có site của người khác (`api_m13.go:85-102`), nên gộp cô lập không lộ thêm gì; `id` có đúng dạng `isolation:<từ>:<tới>` (`checks/site.go:436`) và bộ phép kiểm của một site có cả hai chiều (`core/site.go:311-312`); thứ tự pass < unknown < warn < fail khớp `site/route.ts:14`; mã `--accent-tint` đúng là phép pha 10% và 16% (tự tính ra `#E7EDF8`, `#2D3645`); Radix DropdownMenu 2.2.1 có `modal` mặc định `true` (`react-dropdown-menu/dist/index.mjs:31`); `index.html` được gửi kèm `Cache-Control: no-cache` (`server.go:215`) và CSP nằm ở `auth.go:86`; phiên có tên máy, bản TPAN và danh tính (`auth.go:446-452`), tổng quan có `stack` cho mọi vai, `GET /api/v1/users` chỉ admin gọi được; lệnh của mỗi dòng bảo mật đã nằm trong bằng chứng (`bao-mat/CheckRow.tsx:49`); bản 1 đếm DB ở menu bằng số site đang chạy (`routes.tsx:61-63`) và để trống ô chọn ở hàng của chính mình, đúng như nguyên nhân 7; vào hay thoát "xem như" đều tải lại về `/` (`m13-users.ts:61-74`); `tsconfig.json` loại `src/**/*.test.ts` khỏi tsc và CI đã chạy `make test-web`; điều kiện hiện tab Log, PHP khớp `hasLogs` của bản 1; e2e M1 đến M14 không dựa vào chữ của giao diện (e2e M3 chỉ kiểm cấu trúc `index.html`).

### 9.1 Cách phiên chính xử lý (21/11/2026, 09:45 đến 10:15)

Mọi phát hiện đã sửa vào thân bản thiết kế; không còn điểm nặng. Cách sửa S1 (danh sách "chưa đổi" giảm dần trong test) không đổi kiến trúc của mốc, nên không chạy lại một lượt soát đầy đủ.

| # | Mức | Đã sửa ở đâu, thế nào |
|---|---|---|
| S1 | nặng | B6, B11, mục 7, bảng 8: danh sách thư mục "chưa đổi" trong test, test báo lỗi khi một thư mục trong danh sách đã sạch (chỉ ngắn đi), phiên chính gỡ khi nhận phần; thợ chạy `make kieu-dang DIR=…` (biến `KIEU_DANG_THU`), không sửa file test; bước dọn ở đợt 4 làm danh sách rỗng |
| S2 | vừa | 3.7 Công cụ: số trên ô là số đang chạy trong phạm vi người xem (cả máy với admin); thẻ "Đang dùng" là mức dùng của riêng tài khoản theo cách gói tính (cả thùng rác), có dòng chú thích; 3.8 sửa câu "đúng là số DB rời" và thêm chú thích ở chân bảng Người dùng |
| S3 | vừa | 3.6, 3.7, B9: `ScreenPath` lấy từ `routes.tsx`, `tools.ts` chỉ `import type` và giữ khoá i18n; B9 là lỗi biên dịch, không còn `tools.test.ts`; cờ admin tra thẳng trong `routes.tsx` |
| S4 | vừa | 3.2, 3.4: thêm `Code` (trong dòng và `block`); `duyet-plan/index.tsx` vào danh sách được dùng `font-mono` |
| S5 | vừa | 3.4, 3.11: không đè `id` của tab, móc là `data-tab`; script chụp sửa theo |
| S6 | vừa | 3.6: `MenuSpec.userPath`, hàm `menuPath`, luật lọc của thanh lệnh, nhãn `/dashboard` trong `views.ts`; phiên chính sửa logic ở đợt 1, T8 chỉ đổi kiểu dáng |
| S7 | vừa | 3.7 Người dùng: chép đủ ba luật ẩn của bản 1 |
| S8 | vừa | 3.1, B6, bảng 8: cấm `border-line` ngoài `components/` và `Layout.tsx`; hai `RouteMap.tsx` vào danh sách file của đợt 1 (chỉ hai class) |
| S9 | vừa | mục 7, B13, B14: mọi kiểm (console, 390 px, cỡ chữ, viền, bàn phím, tiếng Anh) chạy trên mọi màn của danh sách ảnh; thêm các lớp nổi và bản xem trước Plan vào danh sách |
| S10 | vừa | 3.4, bảng 8: `CheckRow` của Bảo mật thành file dùng chung của phiên chính; `Legend` giữ chữ ký; `PendingPlans` ghi rõ là kiểu bản 1 tới đợt 3; các màn bỏ `dang-nhap/Field` chuyển sang `TextField`, `SelectField` của `components/Field.tsx`, T7 đổi hai file cũ thành dòng xuất lại |
| S11 | nhẹ | 3.5: thanh đầu trang không xuống dòng từ 1024 px, tên máy tối đa 220 px có dấu ba chấm và `title`; biến `--header-h`; thêm `scroll-padding-top` cho thanh đầu trang dính |
| S12 | nhẹ | 3.7: "(X phép kiểm)", ghi ở M16.md (Q19) |
| S13 | nhẹ | 3.7, B10: tách bằng `split(":")` ra đúng hai phần rồi so bằng; thêm ca `a.com.vn` |
| S14 | nhẹ | B1: thêm site trong thùng rác vào dữ liệu test |
| S15 | nhẹ | 3.4: "tóm tắt rộng tối đa 72ch" |
| S16 | nhẹ | 3.7, 3.11: "hai nhãn", kèm vị trí trong script |
| S17 | nhẹ | mục 6, 3.7 Cài đặt: bốn khoá `localStorage` |
| S18 | nhẹ | 3.6, 3.7: tab đang ẩn coi như `tong-quan`; đổi tab bằng `replace`; thẻ request một giờ qua và nút đường đi giữ điều kiện `hasLogs` |
| S19 | nhẹ | mục 6: dấu trang của bản 2 sau khi lùi là trang 404 của giao diện cũ, chấp nhận được |
| S20 | nhẹ | 3.7 Công cụ: "Gói" chỉ là liên kết với admin |
| S21 | nhẹ | 3.7 Cơ sở dữ liệu: đủ các dạng câu ở chân bảng; giữ đúng điều kiện từng việc của bản 1 (DB của site trong thùng rác còn "Xuất .sql") |
| S22 | nhẹ | 3.4: `TableList` tự vẽ thẻ (có `title`, `actions`, `footer`), bỏ `Card.flush` |
| S23 | nhẹ | 3.7 Trang một site: lọc thêm sự cố có `check_id` cô lập dính tới site |
| S24 | nhẹ | B7: cấm chuỗi `http://`, `https://` trong mã của `web/src`, `web/index.html`; `web/public/` chỉ có không gian tên SVG |
| S25 | nhẹ | 3.7 Bảng điều khiển: nhãn stack cho mọi vai |
| S26 | nhẹ | 3.3: bóng cho ngăn AI và thẻ theo dõi job là quyết định Q18 ở M16.md |
| S27 | nhẹ | B3: liệt kê đủ cặp, thêm `muted` trên `accent-tint` và `on-ink` trên `ink` |
| S28 | nhẹ | bản nháp GIAO-DIEN dòng 5 trỏ về `docs/thay-doi/M16.md` |
