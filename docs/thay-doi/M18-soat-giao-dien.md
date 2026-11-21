# M18: kết quả soát cuối phần giao diện

Soát phần giao diện của mốc M18 trên nhánh `moc/M18`, commit `4ebd92c`, gồm các commit `9eb9e00..4ebd92c`: ba commit phía máy chủ (`b1c3f4e` trường `location`, `d4d34a0` `job_id` của route tải về, `b4499c0` kho giả của demo) và năm commit phía giao diện (`cca582f`, `3fb0b01`, `752966e`, `2a5dcb1`, `4ebd92c`). Người soát chưa đọc mã phần này trước lượt này. Trong lúc soát, nhánh có thêm `432a0b9` (ô CA của thẻ "Đích ngoài máy" đổi từ nút đoạn sang ô chọn) và `ccee0b9` (script ảnh và e2e). `432a0b9` chỉ đổi khối CA của `Target.tsx` và không đổi phát hiện nào dưới đây; số dòng của `Target.tsx` trong file này tính theo `4ebd92c`.

Đã đọc `docs/GIAO-DIEN.md` (cả file), các mục 3.2, 3.3, 3.6, 3.9, 3.11, 3.15, 3.16, 4, 5, 9.2, 9.3 của `docs/thay-doi/M18-thiet-ke.md` và `docs/thay-doi/M18-soat-cuoi.md`. Mã đã đọc:

- giao diện: toàn bộ `web/src/screens/backup/` (index, List, StorageFlow, Target, model, target-model), `web/src/screens/cai-may-moi/`, `web/src/screens/site/Backups.tsx`, `web/src/screens/cong-cu/`, `web/src/api/m18-types.ts`, phần M18 của `web/src/api/types.ts`, chuỗi `web/src/i18n/backup.ts`, `backup-dich.ts`, `cai-may-moi.ts`;
- các thành phần và khung mà các màn đó dùng: `Dialog`, `focusReturn`, `Field`, `RowActions`, `Notice`, `StatusMark`, `Tag`, `Disclosure`, `SegmentButton`, `CommandBlock`, `JobTracker`, `useCopy`, `MonoText`, `PlanReview`, `ErrorNote`, `api/client.ts`, `api/queries.ts`, `useWho`, `Layout.tsx`, `main.tsx`, `routes.tsx`, thanh lệnh;
- máy chủ: `internal/server/api_m18.go`, `handleBackups` của `api_m9.go`, `pusher.go`, phần M18 của `routes.go`, `server.go`, `api_health.go`; `internal/core/location_m18.go`; `Target`, `PrepareSetTarget`, `PreparePush`, `Pull` của `remote.go`; `readTarget`; `NeedsPull`, `checkOnMachine`, `checkIntact` của `backup.go`; `internal/s3/mem.go` và luật kiểm ở `internal/s3/types.go`; `system.MemLock`; `internal/demo/m18.go`; `cmd/tpan/root.go`; phần s3 của `internal/health/collect.go`; `engine.Run`;
- thư viện đang cài: TanStack Query 5.108.0 (`mutationObserver.js`, `mutation.js`, `removable.js`, `queryObserver.js` trong `web/node_modules/.pnpm/@tanstack+query-core@5.108.0`).

Đã chạy (giữa `4ebd92c` và `ccee0b9`, mã web và Go chỉ khác nhau đúng một file `Target.tsx`; mã Go của `internal/` và `cmd/` không đổi):

- ở `ccee0b9`: `cd web && pnpm typecheck` đạt; `make test-web` 37/37 đạt; `make kieu-dang DIR=src/screens/backup/,src/screens/cai-may-moi/,src/screens/site/Backups.tsx,src/i18n/backup.ts,src/i18n/backup-dich.ts,src/i18n/cai-may-moi.ts,src/screens/cong-cu/` đạt; `go test ./web/` đạt;
- một lần trong phiên, trước khi thấy hai commit mới: `go test ./web/ ./internal/server/ ./internal/demo/` đạt (`internal/server` 36 giây, `internal/demo` 82 giây).

Không chạy trình duyệt, máy ảo, `make dev-*`, `anh-*`, `e2e-*` hay `claude -p`. Vì không chạy trình duyệt, các ý về hành vi của trình duyệt (tự điền mật khẩu, lỗi console) dưới đây là suy từ mã, không phải quan sát. Không sửa file nào ngoài báo cáo này.

**Đếm phát hiện:** nặng 0, vừa 1, nhẹ 10. Không tìm thấy đường nào làm lộ hai khoá S3 hay mật khẩu quản trị của máy mới, đường nào chạy việc mà không qua bản xem trước, hay lời gọi nào của user thường tới route của admin.

| # | Mức | Tóm tắt |
|---|---|---|
| G1 | vừa | Tải về chờ khoá lượt đẩy quá 30 phút, hoặc bị từ chối sau khi lấy khoá, thì hỏng lặng lẽ: không có job nào, còn giao diện vẫn nói "đang tải về … xem kết quả ở Lịch sử" |
| G2 | nhẹ | Sau khi gỡ hay đổi đích, bản còn nguyên trên máy hiện "kho khác", và title nói "Đặt lại kho đó thì mới dùng được" |
| G3 | nhẹ | Khôi phục vẫn ra lời từ chối chỉ đường bằng lệnh root ở ba ca giao diện không bắt (R16, bản "kho khác" đã rời máy, setting đích hỏng) |
| G4 | nhẹ | Bấm "Xem trước" lúc lượt đặt đích đang chạy: lượt cũ xoá thân của bản xem trước mới |
| G5 | nhẹ | Câu báo của việc kho: dấu "đạt" cho việc mới xếp hàng, hai câu trái nhau cùng lúc, tab site không báo đang dựng bản xem trước tải về |
| G6 | nhẹ | Mật khẩu quản trị của máy mới còn trong state sau "Đã lưu, ẩn đi"; sinh lại thì thay mật khẩu mà không hỏi |
| G7 | nhẹ | Bàn phím và trình đọc màn hình: form đích không đưa con trỏ tới ô lỗi; lời giải thích của nhãn "Ở đâu" chỉ nằm trong `title`; focus rơi về `<body>` sau khi chạy tải về |
| G8 | nhẹ | Kiểu dáng: nút chính thứ ba trên màn Backup và form đích mở sẵn; khối mật khẩu một lần viết kiểu dáng ngay trong màn; mono chưa đúng chỗ; biểu tượng trùng ở trang Công cụ |
| G9 | nhẹ | Chữ: ví dụ endpoint không qua được luật kiểm, ba câu lỗi của đích thiếu "làm gì tiếp", "bản sao lưu" lẫn "backup", "0 ký tự", dòng "chạy ở đâu" thiếu thư mục, chữ "Trang" trong bảng thuật ngữ |
| G10 | nhẹ | Cảnh báo gỡ đích chỉ có con số, không nói bản nào, và đếm thiếu |
| G11 | nhẹ | Demo: "Đẩy lên kho" xếp hàng mãi vì bộ đẩy nền tắt; `demoS3` là biến toàn cục gán lại ở mỗi `demo.New` |

**Đã xử lý (phiên chính, cùng ngày).**

| # | Xử lý |
|---|---|
| G1 | Sửa. Máy chủ (`b740be0`): `Pull` hỏng trước khi job bắt đầu mà còn trong 5 giây chờ thì câu trả lời là lời từ chối có câu lỗi, không phải 202; test `server.TestTaiVeHongTruocJobBaoNgay`. Câu chờ nói rõ "có thể tới 30 phút; không thấy job ở Lịch sử thì bấm lại". Giao diện (`998a42b`): không có `job_id` thì hiện đúng câu đó, bảng tự tải lại 15 giây một lần trong 30 phút. Còn lại: lỗi về sau 5 giây (chờ khoá quá 30 phút) chỉ có trong log và `audit`, ghi vào giới hạn đã biết. |
| G2 | Sửa ở giao diện: dòng `other_target` còn file trên máy ghi "máy", `title` nói thêm phần kho; dòng lỗi của lần đẩy hiện cả cho `other_target`. CLI giữ "kho (đích khác)"; lựa chọn ghi ở dòng "Ở đâu" của bảng thuật ngữ trong `docs/GIAO-DIEN.md`. |
| G3 | (b) sửa: khôi phục bản ở kho khác mà không còn trên máy nói rõ ngay (admin: đặt lại đích đó; user: nhờ admin), không gọi API. (a) R16 trên giao diện và (c) setting đích hỏng: để sau, ghi vào giới hạn đã biết (admin có `sudo tpan backup pull <id>`). |
| G4 | Sửa: form, "Gỡ đích", "Thử kết nối" khoá khi lượt chạy đang bay; thân xem trước chỉ bị xoá khi đúng là thân vừa gửi. |
| G5 | Sửa: câu "đã xếp hàng", "đang chờ tải về" không dùng dấu đạt; câu chờ ẩn khi bản đã về; tab site báo "Đang dựng bản xem trước…" ở đúng nút. |
| G6 | Sửa phần chính: "Đã lưu, ẩn đi" bỏ mật khẩu khỏi state. Hỏi lại khi sinh lại: để sau. |
| G7 – G11 | Để sau, ghi vào giới hạn đã biết của phần giao diện. |

Ngoài các mục trên, ảnh chụp lộ một lỗi bố cục mà phép kiểm tự động chưa bắt: ở 1440 px, cột "Ở đâu" đẩy bảng Backup rộng quá khung, cột Việc phải cuộn ngang mới thấy. Đã sửa (`998a42b`): khi cột "Ở đâu" hiện thì ẩn cột "Đường dẫn", bảng về 1040 px như trước M18.

### G1. Tải về chờ khoá quá 30 phút, hoặc bị từ chối sau khi lấy khoá, thì hỏng lặng lẽ

- **Mức**: vừa. Luồng hỏng ở đúng đường khôi phục: việc người dùng đã bấm chạy thất bại mà giao diện không bao giờ biết, còn câu báo chỉ người dùng tới Lịch sử, nơi sẽ không có gì.
- **Ở đâu**:
  - `internal/server/api_m18.go:276-303` (goroutine gọi `rem.Pull`; `select` chờ `started` tối đa 5 giây), `:232-234` (`pullNote`);
  - `internal/core/remote.go:1283-1297` (`Pull`: `Acquire` hết hạn hay `PreparePull` từ chối thì trả lỗi trước khi có job);
  - `web/src/screens/backup/StorageFlow.tsx:89-95` (`ran`: không có `job_id` thì hiện `note` và bật `watch`);
  - `web/src/screens/backup/model.ts:80-91` (`pullPollWindowMs` là 2 phút).
- **Kịch bản**:
  1. Máy đã đặt đích từ lâu; `blog.example.com` có một bản cũ chỉ còn trên kho (đã bị dọn trên máy). Kho vừa hỏng hai đêm, nên lượt đẩy hôm nay đuổi theo nhiều bản lớn của các site (hoặc đang tải lên rồi đọc lại một bản 20 GB) và giữ khoá lượt đẩy quá 30 phút. Mục 9.3 của thiết kế (R6 (a)) đã ghi là một lượt đẩy có thể giữ khoá hàng giờ.
  2. Site `blog.example.com` bị phá. Admin bấm "Khôi phục" ở bản chỉ còn trên kho, chọn "Tải về máy" trong hộp hỏi, xem bản xem trước, bấm chạy.
  3. Handler chờ 5 giây mà không có sự kiện nào (`Pull` còn chờ khoá), trả 202 không có `job_id`. Màn hiện câu có dấu đạt "Đang tải về ở nền (chờ lượt đẩy đang chạy nếu có); xem kết quả ở Lịch sử, rồi khôi phục.", tự tải lại bảng mỗi 3 giây trong 2 phút rồi thôi.
  4. Tới phút thứ 30, `Acquire` hết hạn và `Pull` trả "một lượt đẩy lên kho đang chạy quá 30m0s; thử lại sau". Goroutine chỉ ghi `slog` và một dòng `audit` với kết quả "error". Engine chưa chạy nên không có job: Lịch sử trống. Giao diện không có màn nào đọc `audit`. Bản vẫn ghi "kho", không một câu nào nói việc tải đã bị bỏ.
  5. Biến thể: trong lúc chờ, chính lượt đẩy đó dọn kho và xoá đúng bản này (có bản mới hơn lên kho nên bản cũ ra ngoài `s3.keep`). `PreparePull` chạy sau khi lấy khoá từ chối `backup_not_found`, cũng không có job. Dòng biến mất khỏi bảng mà không có lời giải thích.
  6. Phụ: khi `Pull` lỗi ngay (khoá rảnh nhưng `PreparePull` hay `CreateJob` lỗi), handler vẫn chờ đủ 5 giây vì không có kênh nào báo "đã xong", rồi vẫn trả câu "đang tải về".
- **Căn cứ**:
  - `api_m18.go:288-294`: `if ctx.Err() == nil { slog.Error("tải bản sao lưu từ kho", …) }` rồi `s.audit(ar, action, target, result, rep.Job.ID)`; không ghi thứ gì mà giao diện đọc được.
  - `remote.go:1288-1290`: `if !ok { return engine.Report{}, fmt.Errorf("một lượt đẩy lên kho đang chạy quá %s; thử lại sau", r.lockWait()) }`, nằm trước `p.Execute`, nên `CreateJob` (`engine.go:117`) chưa chạy.
  - `api_m18.go:298-302`: `select` chỉ có `started`, `time.After(pullStartWait)` và `r.Context().Done()`.
  - `grep -rn audit web/src` không có kết quả.
  - `pullPollWindowMs = 2 * 60 * 1000`, trong khi `RemoteLockWait = 30 * time.Minute` (`internal/core/types_m18.go:123`).
- **Đề xuất sửa**:
  - Máy chủ: goroutine gửi lỗi vào một kênh đệm 1; handler `select` thêm kênh đó và trả lời từ chối (`jobRefused`) khi lỗi về trước sự kiện đầu tiên. Lỗi về sau 5 giây thì ghi một job `backup.pull` ở trạng thái hỏng, kèm câu lỗi đã làm sạch, để Lịch sử có dấu vết.
  - Giao diện: câu báo khi không có `job_id` nói rõ "có thể chờ tới 30 phút; không thấy job tải về ở Lịch sử thì bấm lại"; bảng tự tải lại theo cùng hạn 30 phút (thưa hơn, ví dụ 15 giây một lần).

### G2. Bản còn trên máy hiện "kho khác" sau khi gỡ hay đổi đích

- **Mức**: nhẹ. Khôi phục vẫn chạy, nên đây không phải luồng hỏng; máy chủ làm đúng mục 3.12 và 3.16 của thiết kế, nơi `other_target` ("kho (đích khác)") là "stored ở đích khác", không xét `local`. Phần sai là câu `title` khẳng định bản chưa dùng được, và cột không còn nói "máy" cho bản vẫn nằm trên máy. Mục 8 của `docs/GIAO-DIEN.md` ("bản nằm ở máy, ở kho hay cả hai") và mục 3.16 mâu thuẫn nhau ở ca `local=1`; chủ dự án cần chọn theo bên nào. Chuyện xảy ra ngay sau hai thao tác thường làm là đổi đích và gỡ đích.
- **Ở đâu**:
  - `internal/core/location_m18.go:24-27`: dòng `stored` ở đích khác, hay mọi dòng `stored` khi chưa đặt đích, luôn ra `other_target`, trước khi xét `Local`;
  - `web/src/screens/backup/List.tsx:72-89` (`Where` chỉ đọc `location`);
  - `web/src/i18n/backup.ts:44` và `:137` (title của `other_target`);
  - `web/src/screens/backup/model.ts:72-77` (không có việc kho nào cho `other_target`).
- **Kịch bản**:
  1. Đích T1 đang đặt. Ba bản của `blog.example.com` ghi "máy và kho" (`local=1`, `stored` ở T1).
  2. Admin bấm "Gỡ đích" (ví dụ để đổi nhà cung cấp) và chạy. Plan `backup.target.rm` không đổi cột `remote*` nào (mục 3.3), nên ba dòng vẫn `stored` với mã của T1.
  3. Màn Backup tải lại. `CurrentTargetID` trả `has=false`, nên `BackupLocation` trả `other_target` cho cả ba. Cột "Ở đâu" vẫn hiện (vì có dòng không phải "máy") và ghi "kho khác"; rê chuột thấy "Bản nằm ở một kho khác với kho đang đặt. Đặt lại kho đó thì mới dùng được." Lúc này không có kho nào đang đặt, và cả ba bản vẫn nằm trên máy.
  4. Người vận hành đọc bảng lúc cần khôi phục. Bản chưa từng lên kho ghi "máy", bản đã lên kho ghi "kho khác", nên họ dễ hiểu là bản "kho khác" không có trên máy. Thật ra bấm "Khôi phục" vẫn chạy được: `NeedsPull` trả false cho dòng `local=1` còn file, và `checkOnMachine` cho qua.
  5. Sau "Đổi đích" sang bucket hay tiền tố khác cũng vậy, cho tới khi bộ đẩy nền đẩy lại từng bản lên đích mới. Bản nằm ngoài `s3.keep` của đích mới không bao giờ được đẩy lại, nên ghi "kho khác" mãi. User thường thấy đúng nhãn này trên bản của họ.
  6. Phụ: lần đẩy lên đích mới mà lỗi thì dòng vẫn giữ `stored` của đích cũ và chỉ có thêm `remote_error`. Dòng vẫn là "kho khác", không có dấu cảnh báo, và câu lỗi không hiện (chỉ `push_failed` mới hiện `remote_error`).
- **Căn cứ**:
  - `location_m18.go:24-27`: `case site.RemoteStored: if !hasTarget || b.RemoteTarget != cur { return LocationOtherTarget }`.
  - `backup.go:452-460`: dòng `Local` có file thường thì `NeedsPull` trả false ngay, không xét đích.
  - `i18n/backup.ts:44`: `other_target: "Bản nằm ở một kho khác với kho đang đặt. Đặt lại kho đó thì mới dùng được."`
  - `docs/GIAO-DIEN.md:275`: "Ở đâu (tên cột) … bản nằm ở máy, ở kho hay cả hai."
- **Đề xuất sửa**:
  - Trong `Where`: dòng `other_target` mà `b.local` là true thì hiện "máy" (có thể thêm "kho khác" làm chữ phụ), title đúng sự thật ("Bản có trên máy này; bản trên kho nằm ở một kho khác"). Khi chưa đặt đích thì bỏ cụm "khác với kho đang đặt".
  - Hiện `remote_error` cho cả dòng `other_target` có lỗi.
  - Muốn sửa một chỗ cho cả CLI ("kho (đích khác)") và MCP thì đổi `core.BackupLocation` và ghi lại vào mục 3.16 của thiết kế.

### G3. Khôi phục vẫn ra lời từ chối chỉ đường bằng lệnh root ở ba ca giao diện không bắt

- **Mức**: nhẹ. Ca hiếm, và admin có đường CLI. Nhưng nó trái mục tiêu mà chính chú thích ở `site/Backups.tsx:13-17` ghi: "để người dùng không gặp lỗi 409 backup_not_on_machine trơ trọi".
- **Ở đâu**:
  - `web/src/screens/backup/StorageFlow.tsx:76-86` (`restore` chỉ bắt `location === "remote"`);
  - `web/src/screens/backup/model.ts:74` (nút "Tải về máy" chỉ cho `remote`);
  - `internal/core/backup.go:448-467`, `:481-488`, `:503-508`;
  - `internal/server/api_m9.go:71-78`.
- **Kịch bản**:
  - (a) R16. Dòng `local=1`, `stored` ở đích đang đặt, file trên máy bị xoá hay hỏng. `location` vẫn là "máy và kho" (tính từ cột DB). Bấm "Khôi phục": file mất thì nhận 409 `backup_not_on_machine` "tải về trước: sudo tpan backup pull <id>"; file lệch sha256 thì nhận `backup_corrupt` "Bản trên kho còn; tải lại: sudo tpan backup pull <id>". Máy chủ cho tải về dòng này (R16 ở 9.3), nhưng giao diện không có nút "Tải về máy" cho bản "máy và kho" và không mở hộp hỏi tải về.
  - (b) Sau khi gỡ hay đổi đích, bản chỉ còn trên kho cũ thành "kho khác" (`local=0`). `flow.restore` trả false, màn gọi `restore?dry_run=1`, máy chủ trả 409 kèm "đặt lại đích đó (sudo tpan backup target set …) để tải về". User thường nhận nguyên câu này: một lệnh root họ không chạy được, thay vì câu "nhờ admin" như ở ca "kho".
  - (c) Setting đích hỏng (sửa tay DB). `handleBackups` bỏ trường `location` của mọi dòng, nên bản chỉ còn trên kho trông như bản thường và bảng không có cột. Bấm "Khôi phục" ở bản đó: `NeedsPull` lỗi khi đọc đích, ra 500 `internal` với câu "đọc đích sao lưu: setting … hỏng …", user thường cũng thấy.
  - Cả ba ca đều là lời gọi trả 409 hay 500. Theo cùng lý lẽ của lời giao việc (một 403 làm bẩn console mà e2e đếm), mỗi lần bấm như vậy cũng để lại một dòng lỗi console. Điều này suy từ mã; chưa chạy trình duyệt.
- **Căn cứ**:
  - `StorageFlow.tsx:77`: `if (backup.location !== "remote") return false;`
  - `model.ts:74`: `if (b.location === "remote") return "pull";`
  - `backup.go:483`, `:486`, `:507`: ba câu đều chỉ lệnh có `sudo`.
  - `backup.go:462-464`: lỗi thường, không phải `RefusedError`; `internal/server/api.go:139-142`: 500 trả nguyên câu lỗi.
- **Đề xuất sửa**:
  - `flow.restore` bắt thêm `other_target` có `local` là false: admin thấy câu "đặt lại đích cũ để tải về", user thấy "nhờ admin".
  - Khi bản xem trước khôi phục của admin trả `backup_not_on_machine` hay `backup_corrupt` cho một dòng `stored`, mở hộp hỏi tải về thay vì chỉ in lỗi.
  - Với user thường, câu từ chối nói "nhờ admin" thay cho lệnh `sudo` (máy chủ chọn câu theo vai).

### G4. Bấm "Xem trước" lúc lượt đặt đích đang chạy

- **Mức**: nhẹ. Cửa sổ hẹp; kết cục là một lỗi 400 khó hiểu, không chạy nhầm việc.
- **Ở đâu**:
  - `web/src/screens/backup/Target.tsx:63-75`: `run.onSuccess` đặt `body.current = null`, `setOpen(null)`, `setFormOpen(false)`;
  - `:150`, `:153`: `busy={dry.isPending}`, không tính `run.isPending`;
  - `:91-105` (`ask`, `previewSet`).
- **Kịch bản**:
  1. Admin bấm "Chạy" ở bản xem trước A. POST chờ khoá lượt đẩy, tối đa 10 giây (`remoteLockWaitAPI`).
  2. Form vẫn mở và nút "Xem trước" vẫn bấm được. Admin sửa một ô và bấm "Xem trước" (B): `body.current = B`, `run.reset()`.
  3. Lượt A trả 202. `onSuccess` của hook vẫn chạy (reset không chặn callback của chính mutation): đặt `body.current = null` và đóng bản xem trước.
  4. Nếu bản xem trước B về sau bước 3, PlanReview của B hiện ra với `body.current` rỗng. Bấm "Chạy" gửi thân `null`, máy chủ trả 400 "endpoint không hợp lệ: đang trống". Nếu B về trước bước 3, nó bị đóng mất.
  5. Ngược lại, lượt A lỗi sau `run.reset()` thì lỗi đó không hiện ở đâu.
- **Căn cứ**: `mutationObserver.js` của TanStack 5.108, hàm `reset`: "the mutation itself keeps running to completion and its own callbacks still fire"; `mutation.js` gọi `this.options.onSuccess` ngay trong `execute`.
- **Đề xuất sửa**: form dùng `busy={dry.isPending || run.isPending}`, nút "Gỡ đích" cũng khoá khi `run.isPending`; trong `run.onSuccess` chỉ xoá `body.current` khi nó còn là đúng thân đã gửi (so tham chiếu).

### G5. Câu báo của việc kho

- **Mức**: nhẹ.
- **Ở đâu**: `web/src/screens/backup/StorageFlow.tsx:89-95`, `:104-136`; `web/src/screens/site/Backups.tsx:177-186`.
- **Kịch bản**:
  - Đẩy lên kho, và tải về chưa có `job_id`, hiện câu kèm dấu "đạt" (chấm xanh), trong khi việc mới xếp hàng hay đang chờ. Mục 2 của `docs/GIAO-DIEN.md` dành chấm xanh cho "đạt"; thẻ theo dõi job dùng dấu "chưa kiểm được" cho job đang chạy (`JobTracker.tsx:116`).
  - Tải về chưa có `job_id` từ hộp hỏi khôi phục: khi dòng thành "máy và kho", màn hiện cùng lúc "Đã tải về. Bấm Khôi phục để xem trước việc khôi phục." và câu cũ "Đang tải về ở nền …; xem kết quả ở Lịch sử, rồi khôi phục.", vì `msg` không bị xoá khi `pulled` đúng.
  - Ở tab Backup của trang site, lúc đang dựng bản xem trước tải về (sau hộp hỏi), không nút nào đổi chữ thành "Đang dựng bản xem trước…", vì `busy` chỉ xét `restore`. Người dùng chỉ thấy mọi nút bị khoá.
- **Căn cứ**: `StorageFlow.tsx:90` `setMsg({ kind: "pass", text: … })`; `:122-133` hai khối `pulled` và `msg` độc lập; `site/Backups.tsx:182` `busy: pending?.action === "restore" && …`.
- **Đề xuất sửa**: câu "đã xếp hàng", "đang tải về" dùng dấu "chưa kiểm được" hoặc không dấu; khi `pulled` đúng thì bỏ `msg`; tab site hiện một dòng "Đang dựng bản xem trước tải về…" khi `pending?.action === "pull"`.

### G6. Mật khẩu quản trị của máy mới

- **Mức**: nhẹ. Mật khẩu không ra khỏi trình duyệt và không hiện lại.
- **Ở đâu**: `web/src/screens/cai-may-moi/index.tsx:65-66`, `:77-83`, `:94-101`, `:228-234`.
- **Kịch bản**:
  - Bấm "Đã lưu, ẩn đi": `pwHidden` thành true, nhưng `result.res.admin_password` vẫn nằm trong state của trang (thấy được bằng React DevTools, nằm trong bộ nhớ) tới khi rời trang. Không gì hiện lại nó nên không trái B3, nhưng một bí mật dùng một lần bị giữ lâu hơn cần.
  - Bấm "Sinh user-data" lần nữa, ví dụ sau khi sửa một ô: mật khẩu mới thay mật khẩu cũ ngay, không hỏi. Ai đã dán user-data cũ vào nhà cung cấp mà chưa chép mật khẩu cũ thì máy mới có mật khẩu không ai biết (đường dự phòng: `sudo tpan admin passwd` qua SSH).
  - Sửa ô sau khi đã sinh: kết quả cũ vẫn hiện, không có dấu nào nói nó không còn khớp với form.
- **Căn cứ**: `index.tsx:230` `{pwHidden ? <p …>{t.secretHidden}</p> : <PasswordBox password={result.res.admin_password} … />}`; `:77-83` (`edit` chỉ xoá lỗi, cố ý giữ kết quả).
- **Đề xuất sửa**: khi ẩn thì thay `result` bằng bản không có `admin_password`; đang có mật khẩu chưa ẩn mà bấm sinh lại thì hỏi một câu ("Mật khẩu cũ sẽ mất. Sinh lại?"); sửa ô sau khi đã sinh thì ghi rõ "đây là kết quả của lần sinh trước".

### G7. Bàn phím và trình đọc màn hình

- **Mức**: nhẹ.
- **Ở đâu**: `web/src/screens/backup/Target.tsx:270-279`; `web/src/screens/backup/List.tsx:88`; `web/src/screens/backup/index.tsx:92-103`.
- **Kịch bản**:
  - Form đích còn ô sai mà bấm "Xem trước": lỗi hiện (`role="alert"`) nhưng con trỏ ở lại nút gửi; người dùng bàn phím phải lùi Tab qua tới chín ô để tìm ô sai. Trang "Cài lên máy ảo mới" thì có đưa con trỏ tới ô lỗi đầu tiên (`cai-may-moi/index.tsx:89-91`).
  - Nhãn "kho", "máy và kho", "kho khác" ở cột "Ở đâu" có lời giải thích (ví dụ "Tải về máy thì mới khôi phục được") chỉ trong `title` của `Tag`: không tới được bằng bàn phím, màn cảm ứng, và phần lớn trình đọc màn hình.
  - Chạy xong "Tải về máy" có `job_id`: `setOpen(null)` gỡ PlanReview đang giữ focus, mà không câu báo nào nhận focus (khác với đẩy lên kho, có `msg`), nên focus rơi về `<body>`. Kiểu này có từ trước M18 ở khôi phục và "Backup ngay".
- **Căn cứ**: `Target.tsx:273-275` `if (problems.length > 0) { setTried(true); return; }`; `List.tsx:88` `<Tag title={t.locTitles[loc]}>`; `StorageFlow.tsx:90` chỉ đặt `msg` khi đẩy hoặc khi không có `job_id`.
- **Đề xuất sửa**: đưa con trỏ tới ô lỗi đầu tiên như trang cài máy mới; đưa lời giải thích của "Ở đâu" vào chữ phụ hay `aria-describedby`; sau khi chạy, đưa focus về nút đã mở bản xem trước hoặc về tiêu đề bảng.

### G8. Kiểu dáng

- **Mức**: nhẹ.
- **Ở đâu**: `web/src/screens/backup/Target.tsx:148-154`, `:407`, `:223`, `:306`, `:371-382`, `:356-366`; `web/src/screens/cai-may-moi/index.tsx:259-304`; `web/src/screens/cong-cu/tools.ts:68-69`.
- **Kịch bản**:
  - Máy chưa đặt đích: màn Backup có ba nút chính ("Backup ngay" ở `Create.tsx:55`, "Xem trước" của lịch ở `Settings.tsx:72`, "Xem trước" của đích ở `Target.tsx:407`), và form đích chín ô luôn mở ở cuối màn. Mục 6 của `docs/GIAO-DIEN.md`: "Mỗi màn một nút chính"; mục 1: "Thứ ít xem nằm sau một tab hoặc một khối mở ra". Hai nút đầu có từ trước M18; M18 thêm nút thứ ba.
  - `PasswordBox` và `SecretRow` của trang "Cài lên máy ảo mới" tự dựng khối mật khẩu một lần ngay trong màn (`cardClass`, `text-[12px] font-medium text-muted`, `text-[16px] font-semibold`, hàng `border-t border-hair`). Mục 6: "Màn chỉ ghép thành phần … không viết kiểu dáng riêng trong màn. Thiếu một thành phần thì thêm vào đây trước". Đây là bản chép thứ ba của cùng một khối, sau `components/DbSecretBox.tsx` và `screens/nguoi-dung/PasswordBox.tsx`; `DbSecretBox` không dùng lại thẳng được vì gắn với kiểu `DBSecret` và chuỗi của màn Cơ sở dữ liệu. Ba bản đã lệch nhau: nút "Đã lưu, ẩn đi" là nút chính ở hai bản cũ, nút phụ ở bản mới.
  - Mono chưa đúng chỗ (mục 4: "giá trị cấu hình" là mono): vùng hiện bằng sans ở dòng xem (`:223`) và ô nhập (`:306`), trong khi endpoint, bucket, tiền tố là mono. Ô access key và ô PEM cũng không mono, dù là thứ người ta so từng ký tự.
  - Ô "Cài lên máy ảo mới" ở trang Công cụ dùng lại biểu tượng dấu cộng của ô "Tạo site" (chú thích ở `tools.ts:68` ghi là tạm).
- **Căn cứ**: các dòng nêu trên; `docs/GIAO-DIEN.md:26`, `:126`, `:148`.
- **Đề xuất sửa**: khi chưa đặt đích, đặt form trong một `Disclosure` "Đặt đích" như khi đã đặt; tách một thành phần `SecretBox` chung (nhãn, tiêu đề, câu, các hàng có nút chép, nút ẩn) vào `components/` cho cả ba chỗ; thêm `mono` cho vùng, access key và PEM; thêm một biểu tượng "máy chủ" vào `components/Icon.tsx`.

### G9. Chữ

- **Mức**: nhẹ.
- **Ở đâu**: `web/src/i18n/backup-dich.ts:11`, `:27`, `:33`, `:38`, `:39-41`, `:60`, `:134`; `web/src/screens/backup/Target.tsx:191-210`; `web/src/screens/backup/target-model.ts:100-102`; `internal/core/remote.go:218-226`; `web/src/i18n/cai-may-moi.ts:29-30`, `:64`; `web/src/i18n/backup.ts:50`; `docs/GIAO-DIEN.md:302`.
- **Kịch bản**:
  - Gợi ý của ô endpoint viết "ví dụ s3.example.com, bắt đầu bằng https". Gõ đúng ví dụ thì `CheckEndpoint` từ chối "endpoint không hợp lệ: có đường dẫn", vì `url.Parse` coi chuỗi không có scheme là đường dẫn. Ví dụ nên là `https://s3.example.com`.
  - Ba câu báo lỗi của thẻ đích chỉ nói chuyện gì xảy ra (và lúc nào thử lại), không nói làm gì tiếp, trái luật "lỗi nói ba điều" của mục 8: "Lần đẩy gần nhất lên kho hỏng." kèm câu của kho và "Lượt đẩy thử lại lúc …"; "Lần tạo gói panel gần nhất hỏng: …". Nên thêm một bước tiếp, ví dụ "Bấm Thử kết nối để xem kho trả lời gì" hay "Xem job hỏng ở Lịch sử".
  - Thẻ đích dùng "Bản sao lưu" (dòng đếm, câu mở đầu, câu về khoá sao lưu), trong khi cùng màn dùng "bản backup" (`backup.ts:24`, `:83`). Mục 8 giữ "backup" ở cả hai bản và đòi một từ cho một khái niệm.
  - Đã đặt đích mà `/etc/tpan/s3.json` không còn (bị xoá tay): `Target()` không đặt `access_key_tail` lẫn `access_key_len`, nên dòng "Khoá truy cập" hiện "0 ký tự" thay vì nói chưa có khoá.
  - Hai `CommandBlock` có dòng "chạy ở đâu" riêng ("Chạy trên máy của bạn, không cần quyền root."; "Chạy trên máy mới, qua SSH, bằng quyền root (lệnh đã có sudo).") thiếu "thư mục nào", điều thứ ba mà mục 6 đòi. Dòng mặc định của thành phần thì có đủ ba điều.
  - Bảng thuật ngữ gọi `/tools/new-server` là "Trang", trái chính dòng "Màn" của bảng ("'Trang' chỉ dùng cho trang của một site, trang Công cụ hoặc trang lỗi").
  - `pushErrorHeading` cho ra "Chưa dựng được bản xem trước đẩy lên kho blog.example.com", đọc như tên kho là tên miền; nên viết "… đẩy bản của blog.example.com lên kho".
- **Căn cứ**: `internal/s3/types.go:255-256` (`if u.Path != "" && u.Path != "/" { return bad("có đường dẫn", …) }`); các dòng nêu trên.
- **Đề xuất sửa**: sửa các câu như trên; `keyHint` trả "chưa có khoá" khi cả hai trường đều vắng.

### G10. Cảnh báo gỡ đích chỉ có con số

- **Mức**: nhẹ. Đúng với mục 3.3 ("tiêu đề bước nêu số bản"), nhưng chưa nói bản nào.
- **Ở đâu**: `web/src/screens/backup/Target.tsx:170`; `web/src/i18n/backup-dich.ts:49-52`; `internal/core/remote.go:243-252`.
- **Kịch bản**: admin bấm "Gỡ đích". Câu đậm "3 bản chỉ còn trên kho: các bản này sẽ không tải về được cho tới khi đặt lại đúng đích này." không nói ba bản đó của site nào, tạo lúc nào. Người muốn tải về trước khi gỡ phải tự dò bảng. Con số lấy từ `TargetView` đã tải trước đó (có thể cũ), và không tính dòng `local=1` đã mất file (S28), dù dòng đó cũng sẽ không tải về được.
- **Căn cứ**: `remote.go:245-246` chỉ đếm khi `b.Remote == site.RemoteStored && b.RemoteTarget == c.ID && !b.Local`.
- **Đề xuất sửa**: liệt kê domain và giờ tạo của các dòng "kho" trong câu cảnh báo (lấy từ danh sách màn đã có, hoặc đưa vào tiêu đề bước của Plan), kèm gợi ý "Tải về máy trước những bản cần giữ".

### G11. Demo

- **Mức**: nhẹ. Chỉ ở chế độ `--demo`.
- **Ở đâu**: `internal/server/pusher.go:169-174`; `internal/server/api_m18.go:197-199`; `internal/demo/m18.go:48-51`, `:98`.
- **Kịch bản**:
  - Demo giờ có kho giả đủ nội dung, nên "Đẩy lên kho" qua được bản xem trước và nhận 202 "Đã xếp hàng: chạy ở lượt đẩy kế tiếp …". Nhưng bộ đẩy nền không chạy ở demo, nên lượt kế tiếp không bao giờ đến và bản ghi "máy" mãi. Ảnh chụp hay kịch bản bấm thật nào bấm nút này sẽ thấy một câu sai.
  - `demoS3` là biến toàn cục; `seedM18` gán kho mới mỗi lần `demo.New`, và `S3()` trả biến đó. Hai lần `demo.New` trong một tiến trình (ví dụ test chạy song song sau này) sẽ đua trên biến này, hoặc server của lần trước đọc kho của lần sau. Hiện chưa test nào chạy song song.
  - `newS3Store` của demo trả cùng một kho cho mọi cấu hình, nên đổi sang endpoint hay tiền tố nào cũng "thử được" và thấy object của đích cũ. Chấp nhận được cho demo, nhưng nên ghi trong chú thích.
- **Căn cứ**: `pusher.go:171` `if s.deps.Demo { close(done); return done }`; `m18.go:98` `demoS3 = s3.NewMem()`.
- **Đề xuất sửa**: chạy bộ đẩy nền ở demo (kho giả trong bộ nhớ, không gọi mạng), hoặc ở demo trả câu "demo không đẩy lên kho"; cho `demo.New` trả kho cùng với máy giả thay vì dùng biến toàn cục.

## Đã kiểm mà không thấy lỗi

- **Hai khoá S3 không vào bộ nhớ đệm.** Hai mutation `dry`, `run` của thẻ đích nhận đối số `"set"` hoặc `"rm"`; thân request nằm trong một `useRef` (`Target.tsx:50`, `:57`, `:64`). Ở TanStack 5.108, `mutate(variables)` ghi đúng `variables` vào `state.variables` (`mutation.js`, action `pending`), nên MutationCache chỉ giữ chữ "set" (5 phút, gcTime mặc định), bản xem trước và `ApiError`. Không `queryKey` nào chứa khoá. Không có `localStorage`, `sessionStorage`, `console`, địa chỉ trang hay `title`, `aria-*` nào mang khoá; cả `web/src` không có lời gọi `console.` nào.
- **Khoá không hiện lại.** Hai ô khoá được xoá trong `.finally` của lượt xem trước, đạt hay lỗi (`Target.tsx:278`); `dry.onError` bỏ thân; "Huỷ" (`close`) bỏ thân; rời màn thì ref mất theo component. Lượt chạy gửi đúng thân đã xem trước; chạy lỗi thì giữ thân, để bấm lại không thành "giữ khoá đang lưu". Thân `null` ở G4 bị máy chủ từ chối ngay ở `CheckEndpoint`, không thành "giữ khoá".
- **Câu lỗi không chép khoá.** `CheckAccessKey`, `CheckSecretKey` và các luật kiểm khác không chép giá trị người dùng (`internal/s3/types.go:219-405`); câu của `ApiError` chỉ có method, đường dẫn và mã HTTP, không có thân request (`client.ts:69`, `:94`).
- **Ô khoá bí mật** là `type="password"` với `autoComplete="new-password"`, nên không nhận mật khẩu đăng nhập panel đã lưu; các ô khác là `autoComplete="off"`. Chưa chạy trình duyệt nên không khẳng định thêm: Chrome hay Safari có thể đề nghị lưu cặp (access key, secret key) như một mật khẩu của site sau khi form gửi đi. Đó là lựa chọn của người dùng, panel không tự làm.
- **R1 ở giao diện.** Giao diện không tự đoán luật của máy chủ; nó chỉ gửi hai khoá rỗng khi người dùng để trống cả hai. `PrepareSetTarget` so endpoint, vùng, bucket, kiểu địa chỉ và CA trước khi dựng kho (`remote.go:323-357`, dựng kho ở `:359`). `formFromView` và `buildRequest` giữ đúng `path_style` và CA khi không ô nào đổi (`target-model.ts:40-54`, `:85-97`), nên việc "chỉ đổi số bản giữ" không bị từ chối oan.
- **Mật khẩu cloud-init.** `useMutation` có `gcTime: 0`, và giá trị này thành 0 thật (`removable.js` lấy `max(this.gcTime || 0, 0)`). `reset()` trong `onSuccess` của lần gọi gỡ observer và lên lịch xoá mutation khỏi MutationCache; rời trang lúc đang chờ thì mutation không còn observer và bị xoá ngay khi xong. Câu trả lời có `Cache-Control: no-store`. Nút chép có nhãn "Chép mật khẩu", không mang mật khẩu; `Code` không có `title`. Đối số của `mutate` (`CloudInitRequest`) không chứa bí mật.
- **Xem trước rồi mới chạy.** Đặt đích, gỡ đích, đẩy lên kho, tải về và khôi phục đều gọi `?dry_run=1` trước; nút chạy chỉ có trong PlanReview của bản xem trước đã về. "Thử kết nối" và "Sinh user-data" không có bản xem trước, đúng hợp đồng 3.11 (hai route không có `dry_run`; một cái chỉ ghi, đọc, xoá một object thử trên kho, một cái không đổi gì trên máy).
- **Đúng id, đúng đường dẫn.** `pathOf` đi qua `pushPath`, `pullPath`, `restorePath`, đều `encodeURIComponent(id)`. Hộp hỏi tải về giữ đúng `Backup` đã bấm; tab site dùng `pullPath(r.backup.id)`.
- **Không tự khôi phục.** Tải xong chỉ báo "Đã tải về. Bấm Khôi phục …". Khôi phục một bản "kho" không gọi API (`StorageFlow.tsx:76-86`), và máy chủ từ chối khôi phục bản chưa về máy ở cả bản xem trước lẫn lượt chạy (`checkOnMachine`).
- **Đẩy bản ngoài `s3.keep`.** Nút "Đẩy lên kho" hiện cho mọi bản full ghi "máy" hay "đẩy lỗi", nhưng `PreparePush`, dùng ở cả bản xem trước và lượt chạy, từ chối `backup_outside_keep` bằng câu rõ (`remote.go:847-859`). Không có ca "đã xếp hàng" rồi bị bỏ lặng lẽ.
- **`refetchInterval` có điểm dừng.** `pullPoll` trả `false` khi quá 2 phút, khi dòng không còn là "kho", hoặc khi dòng biến mất. TanStack tính lại khoảng chờ sau mỗi lần tải và mỗi lần render (`queryObserver.js:130-131`, `:286-299`); rời màn thì observer bị huỷ; tab nằm nền thì không tải.
- **Phân quyền.** `Layout` chỉ vẽ màn sau khi đọc được phiên (`Layout.tsx:48`), nên `useWho` không bao giờ coi user thường là admin trong lúc đang tải. Truy vấn đích có `enabled: isAdmin`; `TargetCard` và form lịch chỉ vẽ cho admin; `storageWork` trả `null` với user thường; `/tools/new-server` được bọc `AdminOnly` (`main.tsx:25-38`); ô ở trang Công cụ lọc bằng `visibleGroups` và `isAdminPath`; thanh lệnh lọc `!s.admin`. Phiên "xem như" mang vai của user được xem nên cũng không gọi route admin. Không tìm thấy lời gọi nào của user thường tới route admin, nên không có 403 nào làm bẩn console. Các lời gọi trả 409 hay 500 ở G3 thì có để lại dòng lỗi console.
- **`location` với user thường** chỉ là một trong năm nhãn; `remote_key` và `remote_error` bị xoá (`api_m9.go:62-70`); câu từ chối gửi cho user chỉ nêu id, domain và mã đích băm. Không lộ endpoint, bucket hay tiền tố.
- **B11.** Máy chưa từng đặt đích:
  - user thường không thấy gì đổi: không cột mới, không lời gọi mới, khôi phục đi như cũ;
  - admin, màn Backup: thêm thẻ "Đích ngoài máy" (câu mở đầu và form, xem G8) và một lời gọi `GET /api/v1/backups/target`; bảng không có cột "Ở đâu", `minWidth` như cũ, khôi phục đi đúng đường cũ;
  - admin, tab Backup của trang site: không đổi gì nhìn thấy được, thêm đúng một lời gọi `GET /api/v1/backups/target`;
  - admin, trang Công cụ và thanh lệnh: thêm ô và dòng "Cài lên máy ảo mới";
  - `GET /api/v1/backups` có thêm trường `location` (giá trị `machine`), giao diện không vẽ gì từ nó.

  Máy đã từng đặt rồi gỡ đích thì khác: xem G2.
- **Chờ 5 giây ở route tải về.**
  - Kênh `started` đệm 1 và được gửi bằng `select … default`, nên goroutine không bao giờ kẹt vì kênh.
  - Handler trả lời sau tối đa 5 giây, hoặc ngay khi client ngắt. Go 1.27 tự thu timer của `time.After` không dùng tới, và server không đặt `WriteTimeout`.
  - Goroutine dừng theo `s.bgCtx()`, và `stop()` gỡ `AfterFunc`.
  - `CreateJob` chạy trước sự kiện đầu tiên (`engine.go:117-121`), nên thẻ theo dõi đọc được job ngay khi có `job_id`. Job thuộc `admin` mà admin xem được (luật `ownerJob`).
- **Setting đích hỏng.** `handleBackups` bỏ trường `location` thay vì từ chối cả danh sách. Giao diện coi `location` vắng là "không có việc kho" (`storageWork`, `showWhere`), còn thẻ đích hiện câu lỗi của `GET /backups/target` kèm nút thử lại. Chỗ chưa ổn ở G3 (c).
- **Demo không gọi ra mạng.**
  - Mọi `core.Remote` của server dựng qua `apiRemote()` với `NewStore: s.newS3Store`, tức kho giả khi `Deps.S3` khác nil.
  - Health của server chạy với `NoSelf: s.deps.Demo`, nên không có phép kiểm S3; bộ đẩy nền tắt; `cloud-init render` không tải gì.
  - Khoá demo sinh bằng `crypto/rand` mỗi lần chạy, chỉ nằm trong `FakeSystem` (bộ nhớ) và không được in ra log. Giao diện chỉ hiện bốn ký tự cuối và dấu vân tay 8 hex, nên ảnh chụp không mang khoá. Test `TestM18KhongCoKhoa` kiểm hai lần dựng ra khoá khác nhau và DB không có chuỗi dạng khoá.
- **Kho giả dùng chung.** `s3.Mem` khoá `sync.Mutex` cho mọi thao tác. `Get` ghi ra ngoài khoá, nhưng mảng byte của object không bao giờ bị sửa tại chỗ (`Put`, `SetObject`, `Corrupt` đều tạo mảng mới). `List` chép danh sách khi còn giữ khoá. `MemLock` là một kênh đệm 1, chờ có hạn và theo ctx. Không thấy đua dữ liệu giữa các request.
- **Hộp hỏi tải về.** Radix Dialog có tiêu đề và mô tả; nút làm việc là động từ ("Tải về máy") và đứng trước "Huỷ". Đóng hộp thì `returnFocus` trả focus về nút "Khôi phục"; bản xem trước về thì nhận focus.
- **Kiểu dáng mà máy kiểm được** (`make kieu-dang` đạt cho các thư mục M18: cỡ chữ, viền, mã màu, nhãn viết thường).
  - Ba màu trạng thái chỉ dùng cho trạng thái: dấu cảnh báo cho "đẩy lỗi", dấu lỗi cho lỗi của đích, dấu cảnh báo cho "chưa có khoá sao lưu".
  - Nút là động từ; ô nhập có nhãn; chuỗi đủ hai bản (`defineStrings`, typecheck đạt).
  - Thuật ngữ mới đúng bảng ở mục 8: Đích ngoài máy, Đổi đích, Gỡ đích, Thử kết nối, Đẩy lên kho, Tải về máy, kho, Ở đâu, Khoá sao lưu, Gói panel, Tiền tố, Kiểu địa chỉ, user-data.
  - Không có "tải lên", "đồng bộ", "kéo về", "click", "Vui lòng" hay dấu chấm than.
- **`CommandBlock`.** Khối `tpan backup key new` tự thêm sudo và dùng dòng "chạy ở đâu" mặc định. Khối `tpan cloud-init status` có sudo, khớp chữ "lệnh đã có sudo", và chỉ đúng máy mới. Khối `curl …/SHA256SUMS` không thêm sudo và chỉ máy của người dùng.
