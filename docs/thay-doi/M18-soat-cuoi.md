# M18: kết quả soát cuối (bảo mật và mất dữ liệu)

Soát mã của mốc M18 trên nhánh `moc/M18`, commit `6367886` (các commit `025edb6..HEAD` của mốc), trước khi vào `dev`. Người soát chưa đọc mã của mốc trước lượt này. Đã đọc đặc tả `docs/thay-doi/M18-thiet-ke.md` (mục 3, 4, 5, 9.2, 9.3) rồi đọc mã theo danh sách ưu tiên: `internal/core/remote.go`, `remote_target.go`, `panel.go`, `types_m18.go`, `cloudinit.go`, phần M18 của `backup.go` và `types_m12.go`; `internal/s3/` (`sign.go`, `client.go`, `multipart.go`, `types.go`); `internal/steps/s3.go`, `panel.go`, `m18_types.go`, phần `secret` của `files.go`; `internal/server/api_m18.go`, `pusher.go`, phần M18 của `routes.go`, `api.go`, `api_m9.go`, `server.go`; `cmd/tpan/backup_s3.go`, phần M18 của `backup.go`, `planned.go`, `cloudinit.go`; `internal/cloudinit/`, `internal/agebox/`; `internal/store/remote.go`, `jobs_m18.go`, `ForgetBackupLocal`, migration 1800; `internal/system/filelock.go`, `OpenRead`, `CreateFile`, `Stat`; phần s3 của `internal/health/collect.go`; `internal/tools/m9.go`; cùng các chỗ cần để hiểu ngữ cảnh (`engine.go`, `system.Secret`, `planJob`, `jobStart`, luật route của health, sự cố, job).

Đã chạy: `go test` cho `internal/s3`, `agebox`, `cloudinit`, `steps`, `core`, `store`, `server`, `system`, `health`, `tools` và `cmd/tpan` (đều đạt). Viết sáu test thử trong `tmp-phien/soatcuoi/` (đã xoá sau khi soát) để tái hiện các phát hiện bên dưới; output thật của chúng được trích ở từng mục. Không chạy gì trên máy ảo, không chạy `claude -p`, không sửa file mã nào.

**Đếm phát hiện:** nặng 1, vừa 1, nhẹ 3.

| # | Mức | Tóm tắt |
|---|---|---|
| F1 | vừa | Tải từ kho không có trần byte: người ghi được vào bucket làm đầy đĩa lúc tải về; kho độc hại làm lượt đẩy chạy mãi |
| F2 | nặng | Dọn trên máy chen giữa lúc đang đẩy: object đã đọc lại khớp sha256 bị xoá, bản mất hẳn, mà lượt đẩy vẫn báo "đã lên kho" |
| F3 | nhẹ | `remote_tries` không đặt lại khi đổi khoá hay đổi đích: bản đã hỏng 5 lần ở cấu hình cũ không bao giờ tự lên kho, và không nằm trong `Failed` |
| F4 | nhẹ | `ListRemote` và `tpan backup list --remote` in nguyên tên object có ký tự điều khiển, ký tự đảo chiều (mục 5 hứa đã bỏ) |
| F5 | nhẹ | `path_style=false` đổi host thật thành `<bucket>.<host>`: luật "http chỉ cho loopback" áp sai host; R1 không so `path_style` |

**Đã xử lý (phiên chính, cùng ngày).** Cả năm điểm đã sửa, mỗi điểm có test bắt được lỗi:

| # | Sửa | Test |
|---|---|---|
| F2 | `after` của job đẩy: dòng đã mất thì ghi lại thành `local=0, stored` (`Store.AddBackupRemoteOnly`), không xoá object; dọn trên kho ngay sau đó quyết theo `remoteSurvives` | `core.TestDonTrenMayGiuaLucDayKhongMatBan` |
| F1 | `countWriter` có trần đúng bằng cỡ đã biết (`S3Pull`: cột size; đọc lại của `S3Push`: số byte vừa gửi; `S3Probe`: cỡ object thử); vượt trần thì dừng ngay, `S3Push` coi là `ErrRemoteMismatch` | `steps.TestS3PullKhoGuiDaiHonCoThiDung` |
| F3 | `after` của `target set` đặt lại `remote_tries` của mọi dòng | `core.TestDatDichNoiLai` (thêm ca) |
| F4 | `ListRemote` thay ký tự Cc, Cf bằng `?` trong tên hiển thị; ghép với bảng `backups` vẫn theo tên gốc | `core.TestListRemoteObjectLa` (thêm ca) |
| F5 | endpoint `http://` bắt buộc kiểu đường dẫn; R1 so thêm `path_style` | `core.TestDatDichHttpCanKieuDuongDan`, `core.TestDatDichGiuKhoaChiKhiCungCho` (thêm ca) |

### F1. Tải từ kho không có trần byte: đầy đĩa lúc tải về, lượt đẩy chạy mãi

- **Mức**: vừa (từ chối dịch vụ cả máy do một tác nhân có trong mô hình đe doạ; không lộ bí mật, không sửa được nội dung site).
- **Ở đâu**:
  - `internal/s3/client.go:426-467` (`Client.Get` chép mọi byte của thân vào `w` cho tới `io.EOF`);
  - `internal/steps/s3.go:561` (`S3Pull.Apply` đưa writer của file `.part` vào `Get`), `:569` (so cỡ và sha256 chỉ sau khi đã nhận hết);
  - `internal/steps/s3.go:417` (đọc lại của `S3Push` vào `io.Discard`);
  - `internal/steps/s3.go:245-246` (`S3Probe` đọc lại vào `bytes.Buffer`);
  - `internal/steps/s3.go:167-178` (`countWriter` chỉ đếm, không chặn).
- **Kịch bản**:
  1. Người ghi được vào bucket (tác nhân ở dòng thứ ba của mục 5: lộ khoá ghi, nhân viên nhà cung cấp, máy khác dùng chung bucket) thay `sites/<domain>/<tên>.tar.gz` bằng một object vài trăm GB, hoặc kho trả thân dài vô hạn.
  2. Site bị phá; admin chạy `sudo tpan backup restore <domain>` (CLI tự tải về) hoặc `POST /api/v1/backups/{id}/pull`.
  3. `S3Pull` kiểm chỗ trống cho đúng `Size` ghi trong `tpan.db` (luật M9), rồi ghi vào `/var/lib/tpan/backups/<domain>/<tên>.part` mọi byte kho gửi. Đĩa gốc (cùng đĩa với MariaDB, `tpan.db`, log, session PHP) đầy dần tới 100%. Lúc đầy, MariaDB, mọi site và chính `tpan.db` cùng gặp ENOSPC; InnoDB có thể dừng hẳn khi ghi redo log lỗi. Chỉ sau đó `S3Pull` mới lỗi, xoá `.part`.
  4. Lặp lại mỗi lần admin thử tải bản đó, đúng lúc đang cần khôi phục.
  5. Biến thể khi kho độc hại: thân GET dài vô hạn, liên tục có byte nên đồng hồ im lặng 120 giây không bao giờ hết. Đọc lại của `S3Push` không bao giờ xong, lượt đẩy giữ khoá lượt đẩy mãi. `target set`, `target rm`, `key new` và mọi lần tải về chờ 30 phút rồi bị từ chối, cho tới khi khởi động lại `tpan serve`. Với `S3Probe` (đặt đích, thử đích), thân lớn phình `bytes.Buffer` trong bộ nhớ của tiến trình root.
- **Căn cứ**:
  - `client.go:436-456`: vòng `for { m, rerr := k.Read(buf); ... w.Write(buf[:m]) ... if rerr == io.EOF { break } }` không có trần. `:457`: `if cl := k.res.ContentLength; cl >= 0 && n != cl` chỉ so với Content-Length do chính kho khai, và chỉ sau khi đọc xong.
  - `s3.go:534`: `if need := uint64(s.Size); !SpaceOK(...)` chỉ giữ chỗ cho `Size`. `:569`: `if n != s.Size || sum != s.SHA256` chạy sau khi đã ghi hết.
  - Mục 5 của thiết kế (dòng "Người ghi được vào bucket") nói "tải về sai sha256 thì dừng": thực tế chỉ dừng sau khi đã ghi toàn bộ.
  - Test thử, kho giả gửi 64 MiB cho một dòng 1024 byte: `dòng backups ghi cỡ 1024 byte; kho gửi 67108864 byte; writer của .part nhận 67108864 byte trước khi S3Pull so sha256`. Client thật với máy chủ `httptest` (http loopback, Content-Length trung thực 32 MiB): `cỡ trong DB 1024 byte, byte đã ghi vào .part: 33554432`.
- **Đề xuất sửa**:
  - `S3Pull` và đọc lại của `S3Push` bọc writer bằng một writer có trần: quá `Size` byte thì trả lỗi ngay (lỗi của riêng bản, `ErrRemoteMismatch`). `S3Probe` giới hạn ở `len(data)+1`.
  - Có thể thêm vào `Client.Get` một tham số cỡ mong đợi: từ chối trước khi đọc thân khi `Content-Length` lớn hơn, và dừng ngay khi vượt.
  - Thêm một ca vào `steps.TestS3PullSaiSha256KhongDeLaiFile`: kho giả trả thân lớn hơn `Size`, writer không được nhận quá `Size+1` byte.

### F2. Dọn trên máy chen giữa lúc đang đẩy: object đã khớp sha256 bị xoá, bản mất hẳn

- **Mức**: nặng (mất dữ liệu thật). Một bản sao lưu mà chính sách `s3.keep` lẽ ra giữ bị xoá không lấy lại được, ở cả máy lẫn kho, kèm thông báo thành công giả.
  - Cửa sổ chạy đua dài bằng cả lượt tải lên và đọc lại, có thể hàng giờ.
  - Cái gây ra nó là việc thường ngày: cron đêm, sao lưu tay, bản `before-restore` của một lần khôi phục.
  - Phạm vi: mỗi lần chạm, mất đúng bản đang được đẩy. Bản đó luôn cũ hơn `backup.keep` bản còn trên máy, nên site vẫn còn điểm khôi phục mới hơn.
  - Hay gặp nhất khi bộ đẩy đang đuổi theo: lần đặt đích đầu trên máy đã có bản, hoặc sau vài đêm kho hỏng.
- **Ở đâu**:
  - `internal/core/remote.go:781-790` (`after` của job `backup.push`: dòng mất thì xoá object);
  - `internal/core/remote.go:1107-1114` (job đạt thì id vào `Pushed`);
  - `internal/core/backup.go:282-288` (`prune` của M9: xoá file rồi `ForgetBackupLocal`; chỉ giữ khoá job, không giữ khoá lượt đẩy);
  - `internal/store/backup.go:160-183` (`ForgetBackupLocal` xoá dòng khi `remote <> 'stored'`).
- **Kịch bản**:
  1. `backup.keep = 3`, `s3.keep = 7` (mặc định). Kho vừa hỏng hai đêm, hoặc đây là lần đặt đích đầu trên máy đã có sẵn bản. Domain `a.example.com` có A1, A2, A3 trên máy, chưa lên kho.
  2. Bộ đẩy nền trong `tpan serve` đuổi theo, mới nhất trước: A3, A2 đã lên. Nó đang tải A1 (vài GB, có thể mất hàng giờ) và đọc lại.
  3. Đúng lúc đó cron chạy `tpan backup create --all` (tiến trình khác, chỉ giữ khoá job). Hoặc admin bấm sao lưu tay, hoặc một lần khôi phục tạo bản `before-restore`.
  4. A4 ra đời; `prune` thấy A1 là bản thứ tư nên xoá file A1, rồi `ForgetBackupLocal(A1)`. A1 chưa `stored` nên dòng bị xoá.
  5. Lượt đẩy đọc lại A1 khớp sha256. `after` gọi `MarkBackupStored`, nhận `ok=false`, rồi **xoá object vừa đọc lại khớp**.
  6. A1 mất ở cả máy lẫn kho, dù `s3.keep = 7` lẽ ra giữ nó (nó chỉ có A2, A3, A4 mới hơn). Cùng thứ tự đó mà lần dọn trên máy chạy chậm một giây (sau `MarkBackupStored`) thì A1 thành `local=0, stored` và còn nguyên trên kho: kết cục phụ thuộc vào thời điểm.
  7. Thêm nữa, job vẫn `succeeded` và `after` trả nil, nên `Sync` cho A1 vào `Pushed`. `tpan backup push` in "Đã lên kho: <A1>", log `backup create` in "đã lên kho: …", cho một bản không còn ở đâu cả.
- **Căn cứ**:
  - `remote.go:785-790`: `if !ok { // Dòng đã mất (dọn cục bộ chen vào) … if err := store.Delete(ctx, key); … return nil }`.
  - `remote.go:1107-1114`: `if rep.Job.Status == engine.JobSucceeded { … res.Pushed = append(res.Pushed, row.ID) }`.
  - Bước đọc lại các dòng trước mỗi bản (R15c, `remote.go:1049-1062`) chỉ tránh được việc *bắt đầu* đẩy một dòng đã mất, không che được dòng mất trong lúc đang đẩy.
  - Test thử (`hookStore` gọi đúng hai việc của `prune` ngay sau `Put`). Test chạy `Sync{Only, Retry}` (đường của `tpan backup push <id>`); bộ đẩy nền dùng `Sync{}` nhưng đi đúng cùng `preparePush` và cùng `after`, nên kết cục như nhau. B1 trong output là A1 của kịch bản:

    ```
    prune trên máy chen vào sau Put của sites/a.example.com/a.example.com-20261120-010000.tar.gz: ForgetBackupLocal(B1) deleted=true
    Sync: err=<nil> pushed=[e8bcce9bcafc475a] failed=[]
    job backup.push succeeded
    sau lượt: object … còn trên kho=false; dòng B1 còn trong DB=false
    ```

  - Mục 3.5 của thiết kế có ghi "dòng đã mất thì xoá object vừa tải". Đó là lựa chọn của thiết kế, nhưng nó trái với lời hứa ở mục 1 ý 3 ("chính sách giữ bản trên kho tách với chính sách giữ bản trên máy") đúng trong cửa sổ chạy đua này.
- **Đề xuất sửa**: khi `MarkBackupStored` trả `ok=false`, dựng lại dòng từ `row` (id, domain, path, size, sha256, lý do, giờ tạo đều có trong bộ nhớ) thành `local=0, remote='stored'`, trong một transaction của store (ví dụ `AddRemoteOnlyBackup`), thay vì xoá object. Đó đúng là trạng thái nếu lần dọn trên máy chạy sau. Lần dọn kho kế tiếp áp `s3.keep` như thường. Nếu giữ cách xoá thì ít nhất không cho id đó vào `Pushed`. Thêm test vào `core`: dọn trên máy chen giữa `Put` và `after`, bản phải còn trên kho với dòng `local=0, stored`.

### F3. `remote_tries` không đặt lại khi đổi khoá hay đổi đích

- **Mức**: nhẹ (bản không bao giờ tự lên kho cho tới khi có người gõ `tpan backup push`; `tpan.s3:fresh` có cảnh báo dòng `failed`).
- **Ở đâu**:
  - `internal/core/remote.go:640-641` (`pushBlock`: `!retry && row.RemoteTries >= RemoteMaxTries`);
  - `internal/core/remote.go:883` (lượt tự động bỏ dòng bị chặn mà không ghi vào `Failed`);
  - `internal/core/remote.go:416-454` (`afterSetTarget` không đụng `remote_tries`);
  - `internal/store/remote.go:45-63`.
- **Kịch bản**:
  1. Kho trả một mã 4xx không phải 401, 403, 404 cho mọi PUT, mà LIST vẫn được. Ví dụ: 400 vì bucket đòi một header mà client không gửi.
     - `Classify` xếp lỗi đó vào `s3_error`, không phải lỗi của đích, nên `fail()` đếm (`countTry = true`) mà không ngắt cầu dao, không lùi.
     - Mỗi lượt 2 phút, mọi bản trong danh sách chờ đều `remote_tries++`. Khoảng 10 phút sau, mọi bản đang chờ đều có `remote_tries = 5`.
     - Đường R2b (lỗi của đích mà `Check` lại vẫn đạt) cũng đếm, nhưng mỗi lượt chỉ một bản rồi ngắt cầu dao và lùi, nên chậm hơn nhiều.
  2. Người quản trị sửa bằng `tpan backup target set …`: chuyển sang đích khác, hoặc đổi cấu hình của kho rồi đặt lại.
  3. Bộ đẩy nền chỉ đẩy các bản mới. Các bản cũ có `remote_tries = 5` bị bỏ qua mãi, và vì `len(want) == 0` nên không vào `SyncResult.Failed`: log của bộ đẩy và output của `backup create` không nhắc tới chúng.
  4. Lần dọn trên máy xoá chúng (chưa `stored`), nên chúng không bao giờ lên kho dù cấu hình đã đúng.
- **Căn cứ**: `case why != "", here && len(want) == 0:` (nhánh rỗng, `remote.go:883`). `afterSetTarget` chỉ xoá `s3.target_*`, `s3.panel_*`. Test thử: sau khi đổi đích, `lượt tự động sau khi đổi đích: err=<nil> ran=true pushed=[] failed=[]; dòng: remote="failed" tries=5`.
- **Đề xuất sửa**: `afterSetTarget` đặt `remote_tries = 0` cho mọi dòng `remote <> 'stored'` (một câu UPDATE). Lỗi đó thuộc khoá hay đích cũ, không phải của riêng bản. Có thể thêm một dòng vào câu kết quả của `target set`: "N bản đang chờ sẽ được đẩy lại".

### F4. `ListRemote` và `tpan backup list --remote` in nguyên tên object có ký tự điều khiển

- **Mức**: nhẹ (giả mạo hiển thị trên terminal của root; không thực thi được gì).
- **Ở đâu**:
  - `internal/s3/client.go:517-525` (`List` chỉ cắt tiền tố, không kiểm `CheckKey`);
  - `internal/core/remote.go:1296-1303` (`ListRemote` chép `o.Key` nguyên văn);
  - `cmd/tpan/backup_s3.go:864` (`printf(w, "%-60s …", o.Key, …)`).
- **Kịch bản**:
  1. Người ghi được vào bucket tạo object có tên chứa `\r`, `\n` hay U+202E (đảo chiều chữ), ví dụ `sites/x\rĐã lên kho: a.example.com\n…`.
  2. Root chạy `sudo tpan backup list --remote` để xem kho sau sự cố.
  3. Terminal in đè dòng hoặc đảo tên, nên root đọc sai bảng kho đúng lúc đang đối chiếu.
- **Căn cứ**: mục 5 của thiết kế hứa "`ListRemote` chỉ hiển thị (tên đã bỏ ký tự điều khiển)" (dòng 643); mã không có bước đó. Test thử với client thật và máy chủ `httptest` trả XML có `&#13;`, `&#10;`, `&#x202E;`: `key "sites/x\rĐã lên kho: a.example.com\ngia"`, `key "panel/‮ega.zg.rat"`.
- **Đề xuất sửa**: trong `ListRemote`, tên không qua `s3.CheckKey` thì không ghép với dòng nào, và hiển thị qua `s3.CleanMessage` (hoặc `strconv.Quote`) kèm nhãn "tên lạ". CLI in tên đã làm sạch.

### F5. `path_style=false` đổi host thật; luật "http chỉ cho loopback" áp sai host; R1 không so `path_style`

- **Mức**: nhẹ (cần thêm vị trí trên đường DNS hoặc resolver trả lời cho tên `<bucket>.<IP>`; phạm vi chỉ là máy đặt đích `http://` loopback).
- **Ở đâu**:
  - `internal/s3/client.go:138-141` (kiểu `bucket.host`: `host = c.bucket+"."+c.host`, giữ scheme `http`);
  - `internal/s3/types.go:270-273` (`CheckEndpoint` chỉ kiểm host của endpoint);
  - `internal/core/remote.go:323` (R1 so endpoint, vùng, bucket, CA; không so `PathStyle`);
  - `internal/server/api_m18.go:91`.
- **Kịch bản**:
  1. Máy đặt đích `http://127.0.0.1:9000` (kho cục bộ như versitygw; mục 3.3 cho phép http vì "loopback").
  2. Root gõ `--vhost`, hoặc một phiên admin không có root gửi `POST /api/v1/backups/target` với cùng endpoint, vùng, bucket, hai khoá rỗng và `"path_style": false`. R1 cho qua vì không so `path_style`.
  3. Client gửi mọi request tới `http://tpan-thu.127.0.0.1:9000`. Tên này không phải loopback: nó đi qua DNS (systemd-resolved chuyển tiếp lên resolver ngoài, bằng UDP rõ). Một resolver có search domain khớp, resolver "chiếm NXDOMAIN", hay kẻ đứng trên đường DNS trả về một IP ngoài máy.
  4. Lúc đó mọi request đi dạng rõ tới IP đó: access key đầy đủ, chữ ký để dò secret, và thân PUT là bản sao lưu site không mã hoá (có `wp-config.php`, bản xuất DB). Đây đúng là điều mục 3.3 dùng luật "http chỉ cho loopback" để chặn.
- **Căn cứ**:
  - Test thử với `s3.New` (PathStyle mặc định false) trên endpoint `httptest` `http://127.0.0.1:<cổng>`: `Get "http://bkt.127.0.0.1:60965/…": dial tcp: lookup bkt.127.0.0.1: no such host`, tức là có tra DNS.
  - Test thử R1: `PrepareSetTarget(KeepKeys, path_style=false): err=<nil>`; kho được dựng với `Endpoint=http://127.0.0.1:9000 PathStyle=false AccessKey=AKIAEXAMPLE0000000 (khoá đang lưu)`.
- **Đề xuất sửa**:
  - `s3.New` (và `PrepareSetTarget`) từ chối `PathStyle=false` khi scheme là `http` hoặc host là địa chỉ IP: kiểu `bucket.host` cần DNS dạng `*.host`, vô nghĩa với IP.
  - Thêm `PathStyle` vào phép so của R1 (`remote.go:323`) và vào ca của `server.TestKhongLoKhoaS3`.

## Đã kiểm mà không thấy lỗi

- **B3, lộ bí mật.**
  - Ba file bí mật chỉ qua `SecretFile`, `SecretAbsent`: `Info().Files` rỗng, nên `engine.Preview` không đọc nội dung cũ và `managed_files` không ghi sha256.
  - `File`, `Absent` từ chối ba đường dẫn; `checkPath` bắt dạng chuẩn nên không lách được bằng `//`, `/./`.
  - `system.Secret` có `String`, `GoString`, `LogValue`, `MarshalJSON`, `MarshalText` đều che.
  - Lỗi của `readSecret`, `readKeys`, `readBackupKey`, `agebox.CheckKey`, `readBackupKeyFile` chỉ nêu đường dẫn và độ dài.
  - `s3.Error` thay access key, secret key, chữ ký trong `Code`, `Message`; thân không phải XML thì `Message` rỗng; 3xx thay hẳn câu; thân lỗi đọc tối đa 64 KB, câu cắt 300 ký tự; lỗi mạng chỉ mang URL (không ký bằng query).
  - `engine.Note` của các bước chỉ có key, cỡ, sha256, nhãn đích.
  - Nhật ký API chỉ ghi target đã qua kiểm hoặc `?`.
  - Không cờ CLI nào nhận khoá; `--keys-stdin` đọc có trần, lời từ chối không chép nội dung.
  - Khoá sao lưu chỉ in ở `key new`, `key show --reveal`, `target set` khi vừa sinh (kể cả bản `--json`).
  - Mật khẩu của `cloud-init render` chỉ ra stderr hay một trường JSON, API có `Cache-Control: no-store`.
  - `list_backups` (MCP) chỉ thêm `location`.
- **R1 và đường khác để phiên admin không có root lấy khoá.**
  - Kiểm giữ khoá chạy trước khi dựng kho.
  - Không có route settings chung nào ghi được `s3.*`: mọi `SetSetting` là khoá cố định.
  - Không route nào trả khoá sao lưu.
  - `managed_file` (MCP) chỉ đọc file có trong `managed_files`.
  - AI chạy với `--tools ""`.
  - Bản xem trước `SecretFile.Check` chỉ cho biết "trùng hay không" sau khi `Check` với khoá của người gọi đã đạt: không thành đường dò.
  - Ngoại lệ duy nhất tìm được là `path_style` (F5).
- **B5, phân quyền.**
  - Mọi route M18 là `adminOnly`.
  - `GET /api/v1/backups` xoá `remote_key`, `remote_error` với user thường.
  - Job đẩy, tải, gói thuộc `admin`, mà `GET /jobs`, `/jobs/{id}`, `/jobs/{id}/events` lọc theo chủ.
  - `GET /api/v1/checks` trả rỗng với user thường; sự cố của chặng tpan chỉ admin thấy.
  - Lời từ chối `backup_not_on_machine` gửi tới user chỉ nêu id, domain, mã đích băm.
- **B2.**
  - Chỉ `MarkBackupStored` đặt `stored`, và chỉ trong `after` của job đẩy đạt: `S3Push` đọc lại cả object so sha256 và cỡ, rồi kiểm chứng HEAD.
  - `Check` của `S3Push` chỉ đúng sau `Apply`.
  - Bước ghép nhiều phần lỗi mà HEAD đúng cỡ vẫn phải qua đọc lại.
- **Không có trạng thái `local=0, remote<>'stored'`**: `ForgetBackupLocal` và `ForgetBackupRemote` là transaction có điều kiện, chạy theo thứ tự nào cũng ra đúng; `MarkBackupPushFailed` giữ `stored` cho mọi dòng đang `stored`.
- **B4.**
  - Khôi phục từ chối bản cần tải về (`checkOnMachine`) trước mọi bước ghi.
  - CLI tải về bằng `Remote.Pull` (giữ khoá lượt đẩy) rồi mới dựng lại `PrepareRestore` theo đúng id.
  - Đường dẫn đích của `S3Pull` lấy từ cột `path`: `path.Clean` và phải nằm ngay dưới `site.BackupDir` của đúng domain. Tên object không bao giờ thành đường dẫn trên máy.
  - `.part` tạo bằng `O_EXCL|O_NOFOLLOW` trong thư mục root 0700; sai sha256 thì xoá `.part`.
- **B7.** `CheckKey` chặn `.`, `..`, `/` đầu; `List` bỏ key ngoài tiền tố; `AbortUploads` kiểm key do kho trả; `Delete`, `Head` của dòng DB đều qua `CheckKey`.
- **B8.** Bốn vectơ mẫu của AWS (GET Bucket list, lifecycle, GET Object có Range, PUT Object) khớp. Đường dẫn và query ký đúng chuỗi gửi đi (`RawPath`, `RawQuery`); `host` được ký nên chữ ký gắn host.
- **Mạng.**
  - Không đi theo chuyển hướng; TLS tối thiểu 1.2.
  - CA riêng thì chỉ tin CA đó; không có tuỳ chọn bỏ kiểm chứng chỉ.
  - `http` chỉ cho host loopback của endpoint (trừ F5).
  - Thân XML tối đa 16 MiB, thân lỗi 64 KB.
  - Proxy theo biến môi trường dùng CONNECT cho https và bỏ qua loopback.
- **B9, B10, khoá.**
  - Tải nhiều phần lỗi thì huỷ bằng context riêng 30 giây.
  - Job đẩy, tải, gói chạy trên `heldLockSystem` (không lấy khoá job).
  - `target set`, `target rm`, `key new` giữ khoá lượt đẩy từ trước engine tới sau `after`. Thứ tự luôn là khoá lượt đẩy rồi khoá job, nên không khoá chéo.
  - `FileLock` mở fd mới mỗi lần, `flock` theo open file description nên lần lấy thứ hai trong cùng tiến trình bị chặn đúng (không "lấy được giả"). Không có chỗ nào lấy lồng khoá lượt đẩy. Chờ có hạn và huỷ được bằng ctx. fd có `O_CLOEXEC` (mặc định của Go) nên lệnh con không thừa hưởng khoá.
  - `AbortStaleJobs` chỉ chạy sau khi đã giữ khoá lượt đẩy, và mọi job `backup.push`, `backup.pull`, `backup.panel` (CLI lẫn API) đều chạy dưới khoá đó, nên không đóng nhầm job đang sống.
- **Goroutine.**
  - Bộ đẩy nền dừng theo ctx; `Run` của server chờ nó.
  - Goroutine tải về dừng theo `s.bgCtx()`; khi dừng, `S3Pull` xoá `.part`.
  - Ghi nhận (không phải lỗi bảo mật hay mất dữ liệu): `Run` không chờ `s.api.wg`, nên goroutine tải về có thể còn đang ghi job hay dòng `backups` lúc store đã đóng. Kết cục là job đó ghi lỗi; lượt đẩy sau đóng job `running` (R12) và `S3Pull` kiểm theo sha256 ở lần tải sau.
- **B13, mật mã.**
  - Khoá sao lưu: 32 byte `crypto/rand`, base32 chữ thường, luật `^tpan-key-1-[a-z2-7]{52}$`.
  - `age` chế độ scrypt, hệ số công 15 khi mã hoá, `SetMaxWorkFactor(15)` khi giải mã. Test có ca hệ số 18 bị từ chối, sửa một byte, cắt cụt, sai khoá.
  - Luồng mã hoá được `Close` trước file. Dấu vân tay là 4 byte sha256, không lộ gì.
- **Gói panel.**
  - Thư mục tạm `.tmp-<giờ>` root 0700, luôn xoá kể cả khi lỗi; tiến trình chết thì lượt đẩy dọn sau một giờ.
  - Gói chỉ có `manifest.json` và bản `VACUUM INTO` của `tpan.db`; không có `s3.json`, `backup.key`, khoá TLS.
  - `panelEncrypt` đọc khoá lúc mã hoá.
- **B6, B12, B15, cloud-init.**
  - user-data dựng bằng bộ ghi YAML từ struct; đường dẫn và `runcmd` là hằng; giá trị người dùng chỉ vào nội dung base64.
  - `sites.yaml` được ghi lại từ cấu trúc đã kiểm (domain, tiêu đề, email).
  - Bốn giá trị vào script (`base`, tên bản, hai sha256) qua luật ký tự chặt và nằm trong nháy đơn.
  - sha256 file chạy được kiểm trước khi `mv`. Có `first-boot.json` và file chạy thực thi được thì script không đụng file chạy.
  - `http://` bắt buộc `--sums`; API không tải gì.
  - Mật khẩu WordPress chỉ nối vào `/root/tpan-first-boot-passwords.txt` (0600). Output vào `cloud-init-output.log` chỉ có tên bước và kết quả; mật khẩu WP đi qua stdin với `--quiet` từ trước.
  - Mật khẩu quản trị 32 ký tự từ `crypto/rand`, chỉ bản băm argon2id vào user-data.
- **Dọn hai nơi.**
  - `remoteSurvives` dùng chung cho chọn bản và dọn kho, đơn điệu (R8 đã áp), nên không có vòng đẩy rồi dọn.
  - Dọn kho xoá object trước, sửa DB sau.
  - Gói panel trên máy giữ 3 gói, trên kho giữ `s3.keep` gói, theo tên có giờ UTC.
  - Thứ tự dòng của `Store.Backups` có `rowid` phá hoà.
- **Nối lại (R5).** Cờ bền `s3.relink_due`; `after` của `target set` chạy dưới khoá lượt đẩy nên không chen với dọn kho; chỉ đổi `remote_target` của dòng `stored`.
- **B11.** Chưa đặt đích thì health không thêm phép kiểm, `backup list` không thêm cột, `backup create` không gọi gì thêm; bộ đẩy nền chỉ đọc settings rồi thoát.
