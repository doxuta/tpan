# Rà giao diện bằng trình duyệt (20/11/2026)

Phần agent tự làm được của mục 10 trong `docs/GIAO-DIEN.md`. Phần cần nhìn bằng mắt (so với trang mẫu, chế độ xám, trình đọc màn hình thật) nằm ở cuối, chờ chủ dự án.

## Cách chạy

- Backend: `go run ./cmd/tpan serve --demo --listen 127.0.0.1:18151`. Giao diện: `TPAN_API=http://127.0.0.1:18151 pnpm exec vite --port 5195 --strictPort` (vite dev, không build).
- Trình duyệt có sẵn trong ứng dụng Claude, một tab riêng. Bề rộng đặt bằng công cụ giả lập cỡ cửa sổ: 1440 × 900 và 390 × 844. Ở 390 px, công cụ giả lập điện thoại (user agent Android, có cảm ứng).
- Sáng/tối và VI/EN đổi bằng nút trên thanh đầu trang. Ở các lượt sau, giá trị được đặt vào `localStorage` (`tpan-theme`, `tpan-lang`) rồi tải lại trang, để màu đo được không phải màu đang chuyển dở. Khi khung trình duyệt bị ẩn, hiệu ứng chuyển màu 120 ms không chạy hết, nên nếu đo ngay sau khi bấm nút sẽ ra màu cũ của thanh đầu trang.
- Kiểm bằng JavaScript chạy trong trang (đo kích thước, quét chữ), không chỉ nhìn ảnh. Mỗi màn được quét những thứ sau:
  - `documentElement.scrollWidth > clientWidth` (trang cuộn ngang).
  - Phần tử có chữ mà `scrollWidth > clientWidth` (chữ tràn hoặc bị cắt), và phần tử vượt mép phải mà không nằm trong khung cuộn.
  - Tương phản chữ so với nền thật phía sau theo công thức WCAG: chữ thường cần từ 4,5:1, chữ lớn từ 3:1.
  - Viền trùng màu nền: tỉ lệ dưới 1,15 so với cả nền cha lẫn nền của chính nó.
  - Ở 390 px: mọi `button`, `select`, ô nhập và liên kết dạng nút thấp hơn 44 px.
  - Mọi dấu trạng thái (`StatusMark`): có `role="img"` kèm nhãn, hoặc bị ẩn nhưng ngay cạnh có chữ nói trạng thái.
  - Bản EN: chữ, `aria-label`, `title`, `placeholder` còn dấu tiếng Việt. Bản VI: chuỗi trùng đúng bản tiếng Anh trong `web/src/i18n/*.ts`.
- Focus bàn phím: bấm Tab thật một lần, sau đó gọi `focus()` lần lượt cho mọi phần tử bấm được trên màn và kiểm ba điều: có `:focus-visible`, viền là 2 px liền, và viền không bị khung `overflow` của phần tử cha cắt mất (vùng kiểm là 4 px quanh phần tử).
- Trước khi quét, các nút `aria-expanded="false"` trong `main` được bấm mở (ga trên bản đồ tuyến, chặng ở trang Site, bước của job, khối bằng chứng).
- Console: `read_console_messages` chỉ lấy lỗi, và đuôi log của vite.

## Kết quả: màn × 4 tổ hợp

Ký hiệu trong bảng:

- "đạt": không còn lỗi nào ở các mục trên, tính cả sau khi đã sửa.
- "đạt*": đạt, trừ chữ mono bấm-là-chép thấp hơn 44 px. Chưa sửa, xem câu hỏi 1 ở cuối.
- "đạt (dữ liệu)": trong bản EN có chữ tiếng Việt, nhưng đó là dữ liệu từ Go hoặc nội dung file cấu hình, không phải chuỗi giao diện. Xem mục "Còn lại, ngoài `web/src`".

| Màn | Sáng VI 1440 | Tối EN 1440 | Sáng EN 390 | Tối VI 390 |
|---|---|---|---|---|
| `/` Bảng điều khiển | đạt (sau sửa 2) | đạt | đạt | đạt |
| `/sites` | đạt (sau sửa 2) | đạt | đạt | đạt |
| `/sites/new` | đạt | đạt | đạt | đạt |
| `/sites/blog.example.com` | đạt (sau sửa 2) | đạt (dữ liệu) | đạt* (dữ liệu) | đạt* |
| `/sites/wiki.example.org` (site hỏng) | đạt (sau sửa 2) | đạt (dữ liệu) | đạt* (dữ liệu) | đạt* |
| `/databases` | đạt (sau sửa 2) | đạt | đạt* | đạt* |
| `/ssl` | đạt (sau sửa 2) | đạt | đạt (sau sửa 1) | đạt (sau sửa 1) |
| `/backups` | đạt (sau sửa 2) | đạt | đạt* (sau sửa 1) | đạt* (sau sửa 1) |
| `/cron` | đạt (sau sửa 2) | đạt | đạt (sau sửa 1) | đạt (sau sửa 1) |
| `/trash` | đạt (sau sửa 2) | đạt | đạt | đạt |
| `/incidents` | đạt | đạt | đạt | đạt |
| `/incidents/1`, `/incidents/2` | đạt | đạt (dữ liệu) | đạt* (dữ liệu) | đạt* |
| `/jobs` | đạt | đạt | đạt* | đạt* |
| `/jobs/:id` | đạt | đạt (dữ liệu) | đạt* (dữ liệu) | đạt* |
| `/plans` (danh sách rỗng) | đạt | đạt | đạt | đạt |
| `/plans/:id` | chỉ rà trang "không có Plan này" | như cột trước | như cột trước | như cột trước |
| `/stack` | đạt | đạt | đạt* | đạt* |
| `/updates` | đạt | đạt | đạt* | đạt* |
| `/security` | đạt | đạt (dữ liệu) | đạt (dữ liệu) | đạt |
| `/ai` | đạt | đạt | đạt | đạt |
| `/ai/:id` Phiên AI | đạt (sau sửa 4) | đạt | đạt* | đạt* |
| `/settings` | đạt | đạt | đạt* | đạt* |
| `/dang-nhap` (cả lúc gõ sai mật khẩu) | đạt | đạt | đạt* | đạt* |
| Ngăn AI đang mở (trên `/`) | đạt | đạt | đạt | đạt |
| Thanh lệnh ⌘K | đạt | đạt | đạt | đạt |
| Bản xem trước "Sửa tự động" (sự cố 1) | đạt | đạt (dữ liệu) | đạt (sau sửa 3) | đạt |
| Hộp hỏi lại "Chạy 1 bước này?" (mở rồi Esc, không chạy) | đạt | đạt | đạt | đạt |
| Bản xem trước "Xoá hẳn" (thùng rác, không chạy) | đạt | đạt (dữ liệu) | đạt (sau sửa 3) | đạt |
| Bản xem trước tạo site | đạt | đạt (dữ liệu) | đạt (sau sửa 3) | đạt |

Tổng:

- 22 mục trong `routes.tsx`, rà qua 24 URL: 23 URL trong khung trang, gồm hai trang site và hai sự cố, cộng `/dang-nhap`.
- 7 trạng thái mở thêm.
- 4 lỗi đã sửa, trong 6 file.

Những mục đã quét và không tìm thấy lỗi nào ở cả 4 tổ hợp:

- Tương phản chữ.
- Viền trùng nền ở chế độ tối.
- Dấu trạng thái thiếu nhãn. Mọi dấu hoặc có `role="img"` kèm nhãn dạng "Pool PHP: lỗi", hoặc bị ẩn và ngay cạnh có chữ trạng thái: chú giải, bước job ("xong", "lỗi, đã hoàn tác"), lỗi của ô nhập. Tên truy cập của logo có trạng thái máy: "TPAN, về Bảng điều khiển · máy: lỗi".
- Chuỗi tiếng Anh còn sót trong bản VI.
- Chuỗi viết thẳng trong component: grep `aria-label`, `title`, `placeholder` và chữ JSX ngoài `i18n/` chỉ ra `placeholder="admin@example.com"`, chữ "TPAN" và vài chú thích code.

## Đã sửa (chỉ trong `web/src`, `pnpm -C web typecheck` xanh sau mỗi lần sửa)

1. **Trang cuộn ngang ở 390 px trên `/ssl` (621 px), `/backups` (600 px), `/cron` (425 px).**
   - Nguyên nhân: `MonoText` (và các nút chép khác) có một span `sr-only` dùng `position: absolute` để báo "Đã chép". Khung cuộn ngang của bảng không đặt `position`, nên span đó không bị khung cắt. Nó nằm ở cột xa bên phải và kéo bề rộng cả trang ra.
   - Sửa: thêm `relative` vào bốn khung cuộn ngang: `web/src/components/TableList.tsx`, `web/src/screens/ssl/Sites.tsx`, `web/src/screens/site/RouteMap.tsx`, `web/src/screens/bang-dieu-khien/RouteMap.tsx`.
   - Kiểm lại: `scrollWidth` bằng `clientWidth` (390) trên cả 23 URL trong khung trang và trên `/dang-nhap`, ở cả sáng EN lẫn tối VI.
2. **Viền focus bị cắt.** Hai chỗ:
   - Bảng điều khiển: liên kết tên site nằm trong một div `w-[180px] overflow-hidden`, nên viền focus bị cắt gần hết. Đã bỏ `overflow-hidden` ở div bọc (`bang-dieu-khien/RouteMap.tsx`). Liên kết và dòng request vẫn tự cắt chữ bằng ellipsis.
   - Ở mép bốn khung cuộn nói trên, viền của liên kết cột đầu, mã HTTP cột cuối và nút ga bị mất một cạnh. Đã thêm `-mx-[4px] px-[4px]` cho hai bảng và `-m-[4px] p-[4px]` cho hai bản đồ tuyến. Lề âm trừ đúng phần đệm thêm vào nên bố cục không đổi: đã đo, mép trái của bảng vẫn thẳng với `h1` (257 px) và mép phải vẫn ở 1408 px.
   - Kiểm lại: ở 1440 px, 0 phần tử bị cắt viền trên 21 URL. Lượt này chạy trước khi thêm `/ai/:id` và `/plans/khong-co` vào danh sách, và chưa tính `/dang-nhap`; trang đăng nhập được kiểm riêng ở 1440 px sáng VI: 6 phần tử, 0 lỗi.
   - Chiều dọc cũng không đổi. Cha của hai bản đồ tuyến không dùng `space-y`. Lề âm 4 px gộp với lề của khối kề bên, rồi phần đệm 4 px bù lại đúng chỗ đó. Phần tử `absolute` duy nhất trong bốn khung cuộn là nét tuyến và dấu ga, mà chúng nằm trong một span `relative` riêng, cùng với các span `sr-only`. Không có menu thả nào bị khung cắt.
3. **Tên bước tràn khỏi hàng ở 390 px** trong bản xem trước Plan: "Create /var/log/tpan/sites/…", "Permanently delete /var/lib/tpan/trash/…", "Set DISABLE_WP_CRON = true in /var/www/…".
   - Sửa: thêm `[overflow-wrap:anywhere]` cho `h4` tên bước trong `web/src/screens/duyet-plan/index.tsx`.
   - Kiểm lại: hết tràn ở bản xem trước sửa sự cố, xoá hẳn và tạo site, ở 390 px EN.
4. **Câu trả lời AI hiện nguyên dấu Markdown**, ví dụ "Xem chi tiết ở [blog.example.com](Trang site blog.example.com)".
   - Nguyên nhân: bộ não demo, và Claude thật cũng hay làm vậy, bọc liên kết `tpan:` trong cú pháp `[chữ](tpan:…)`. Giao diện chỉ đổi phần `tpan:…` thành tên mục, nên phần bọc còn lại.
   - Sửa: `web/src/ai/AnswerText.tsx` bỏ phần bọc `[…](…)` quanh liên kết `tpan:` trước khi tách chữ.
   - Kiểm lại: `/ai/:id` hiện "Xem chi tiết ở Trang site blog.example.com."

## Còn lại, ngoài `web/src` (chưa sửa, cần phiên chính hoặc chủ dự án)

- **Chữ tiếng Việt trong bản EN, đến từ Go.** Đây là output của bằng chứng hoặc bản xem trước, không phải chuỗi giao diện:
  - `internal/checks/site.go:75` ("file thường") và `internal/checks/health.go:360` ("mode 0644, chủ root:root"): output stat chỉ có tiếng Việt.
  - `internal/checks/m9.go:59`, `m12.go:53`, `health.go:455`, `security.go:419`: hai thứ tiếng dính trong một chuỗi ("không có / no such file or directory").
  - `internal/steps/backup.go:253`: chỗ giữ tên file `<giờ lúc chạy>` nằm trong lệnh hiện ở bản EN.
  - Chú thích tiếng Việt trong nội dung file cấu hình (vhost, pool, cron) là nội dung thật của file nên để nguyên.
- **Chữ lặp ở bản xem trước:** "Không hoàn tác được: Không hoàn tác: khởi động lại…" (EN: "Cannot be rolled back: Not undone: …"). Nhãn `noUndo` trong `web/src/i18n/duyet-plan.ts` đã có tiền tố, mà lý do bên Go (`internal/steps/apt_m12.go`, `internal/core/oneway.go`, …) cũng mở đầu bằng "Không hoàn tác:". Nên bỏ tiền tố ở một bên. Mình chưa sửa vì đây là việc chọn câu chữ, và phần lớn nằm bên Go.
- **`/plans/:id` chưa rà với dữ liệu thật.** Ở chế độ demo không có Plan nào chờ duyệt: Plan chỉ sinh ra khi agent gửi qua MCP. Đã rà trang "không có Plan này". Bố cục duyệt Plan (dùng chung với bản xem trước) đã rà qua ba bản xem trước ở trên.
- Lỗi console thấy được:
  - "useAi chỉ dùng được trong AiProvider" chỉ xuất hiện ngay sau khi vite nạp nóng (HMR) một file vừa sửa, tải lại trang là hết. Đây là lỗi của chế độ dev, bản build không có.
  - Ngoài ra chỉ có các lỗi 404 đúng như mong đợi cho `/api/v1/plans/khong-co` và `/api/v1/jobs/khong-co`. Plan không có bị gọi lại 4 lần (TanStack Query thử lại), không ảnh hưởng gì.
  - Vite dev không gửi CSP, nên lỗi CSP (nếu có) chỉ lộ ra ở bản build. Chưa kiểm.

## Câu hỏi đặc tả và việc chủ dự án cần tự nhìn

1. **Chữ mono bấm-là-chép thấp hơn 44 px ở 390 px.** Gồm `MonoText`: domain, đường dẫn, mã job, tên DB, lệnh ở Cài đặt; cao 17 đến 38 px. Và nút chép mã backup ở `web/src/screens/backup/List.tsx`, cao 17 px.
   - Mục 6 nói "nút cao 44 px" ở màn hẹp. Những thứ trên về mặt kỹ thuật là `<button>`, nhưng trông như chữ trong bảng.
   - Nâng lên 44 px sẽ đổi chiều cao mọi hàng bảng, tức là đổi bố cục đã chốt, nên mình chưa làm.
   - Cần chủ dự án quyết: quy tắc 44 px có tính cả chữ chép không. Nếu có, cách ít đổi bố cục nhất là nới vùng bấm bằng một pseudo-element ở màn hẹp, còn chữ giữ nguyên chỗ.
2. **Đặt cạnh trang mẫu** `docs/mau-giao-dien/*.html` ở sáng và tối: khoảng cách, cỡ chữ, độ dày nét. Agent không so được bằng mắt.
3. **Thanh cuộn ngang của bảng** ở 390 px giờ dài hơn bảng 4 px mỗi bên, do phần đệm chừa cho viền focus. Nhìn xem có chấp nhận được không.
4. **Ở 390 px, khi phần tử nằm sát mép đang cuộn của khung cuộn,** viền focus vẫn có thể mất tới 4 px ở cạnh đó. Đây là giới hạn của mọi khung cuộn, ba cạnh còn lại vẫn thấy.
5. **Chế độ xám thật của hệ điều hành:** bốn dấu trạng thái có còn phân biệt được không. Hình dạng đúng đặc tả theo mã, nhưng chưa nhìn bằng mắt.
6. **Trình đọc màn hình thật** (VoiceOver): thứ tự đọc, nhãn dấu trạng thái, vùng `aria-live` "Đã chép". Mình mới kiểm thuộc tính trong DOM, chưa nghe thật.
7. **Đi hết màn bằng Tab thật** từ đầu tới cuối. Mình mới kiểm từng phần tử có viền focus, chưa kiểm thứ tự Tab hay bẫy focus trong hộp hỏi lại và ngăn AI ở 390 px.

## Cập nhật sau khi rà (20/11/2026)

Các chuỗi từ mã Go còn tiếng Việt ở bản EN đã sửa: bằng chứng của phép kiểm viết tiếng Anh như output của công cụ thật, tên file backup giữ chỗ là `<YYYYMMDD-HHMMSS>`, lý do "không hoàn tác" của các bước bỏ tiền tố trùng với nhãn giao diện. Test `internal/checks/i18n_scan_test.go` quét mã nguồn để bắt lại các lỗi này. Còn lại: lỗi trả về từ `err.Error()` có thể mang chữ Việt vào bản EN (quét tĩnh không bắt được), và dữ liệu đã lưu trước đó giữ chữ cũ.
