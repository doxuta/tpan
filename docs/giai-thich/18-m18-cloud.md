# M18: sao lưu ra kho S3, khôi phục từ kho, cài máy mới bằng cloud-init

Đọc kèm: `09-m9-backup-va-cron.md` (bản sao lưu site, bảng `backups`, luật khôi phục, giữ bản trên máy), `15-m15-nut-cap-nhat.md` (khoá toàn cục của máy, job, `system.Secret`), `00-go-can-ban.md` (interface, error, context), `docs/thay-doi/M18-thiet-ke.md` (bản thiết kế đầy đủ; mục 9.3 đè lên mục 3 khi khác nhau), `docs/thay-doi/M18.md` (quyết định và chỗ khác đặc tả), `docs/bang-chung/M18-nghien-cuu/` (bản thử và số đo thật).

Hai từ dùng suốt file. **Kho** là một bucket S3 tương thích ở ngoài máy (nhà cung cấp lưu trữ nào cũng được, miễn nói chuyện bằng giao thức S3). **Đích** là cấu hình trỏ tới kho: endpoint, vùng, bucket, tiền tố, hai khoá truy cập. **Lượt đẩy** là một lần chạy `Remote.Sync` (`internal/core/remote.go`).

## 1. Mốc này làm được gì

- Bạn đặt một đích S3 cho cả máy: `sudo tpan backup target set ...`. TPAN thử ghi, đọc, xoá một object thật rồi mới lưu. Sau đó mỗi bản sao lưu đầy đủ của site được đẩy lên kho, đọc lại, và chỉ khi sha256 khớp mới được coi là "đã lên kho".
- Bản sao lưu trên máy bị dọn theo luật của M9 thì dòng trong `tpan backup list` vẫn còn, ghi "kho". `sudo tpan backup restore <domain>` tự tải bản đó về, kiểm sha256, rồi khôi phục như M9.
- Chính panel (file `tpan.db`: danh sách site, user, gói, cài đặt, lịch sử) được đóng thành "gói panel", mã hoá bằng một khoá sao lưu, rồi đẩy lên kho.
- `tpan cloud-init render` sinh một file user-data. Tạo máy mới với file đó thì lần khởi động đầu tự cài TPAN, stack và các site ghi trong `sites.yaml`. File user-data không chứa mật khẩu hay khoá dạng rõ.
- Hai phép kiểm mới trong `tpan health` (chỉ có khi đã đặt đích) và API (chỉ admin) cho tất cả những việc trên.

### Nói trong một phút

Một VPS có thể mất bất cứ lúc nào: nhà cung cấp lỗi, bị xoá nhầm, đĩa hỏng. Trước M18, bản sao lưu của M9 nằm trên chính đĩa của máy, nên mất máy là mất cả bản sao lưu. M18 cho bản sao lưu ra khỏi máy, và cho một máy mới tự dựng được thành máy có panel.

Ba ý chính để nói:

1. **Không tin kho, chỉ tin sha256 do mình tính.** Mỗi bản đẩy lên đều bị đọc lại cả object và băm lại. Bản thử cho thấy kho có thể hỏng dữ liệu mà ETag (con số kho trả về như một "dấu vân tay") không đổi, nên ETag không dùng làm bằng chứng.
2. **Kho hỏng không được làm hỏng việc sao lưu.** Sao lưu trên máy là một job, đẩy lên kho là một job khác, chạy sau, không giữ khoá của máy. Kho chết thì bản trên máy vẫn xong, panel cảnh báo, lần sau đẩy bù.
3. **Bí mật không bao giờ ra khỏi chỗ của nó.** Hai khoá truy cập và khoá sao lưu nằm trong ba file `root 0600`, không vào bản xem trước, log, API, user-data hay gói panel. Chỉ riêng gói panel được mã hoá; bản sao lưu site thì để nguyên `.tar.gz` (hệ quả ở mục 3.4).

M18 cố ý **không làm** lệnh dựng lại cả máy tự động (mục 5.1). Có đường dựng bằng tay đã chạy thật.

## 2. Chạy thử

Mình là thợ viết, không được dùng máy ảo trong phiên này. Lúc mình viết, `docs/bang-chung/M18.log` (e2e đầy đủ) chưa có, và `docs/bang-chung/M18-thu-chinh.log` mới có phần chuẩn bị máy (đặt lại máy ảo, build, chép tpan, chép script đồ nghề). Vì vậy:

- Output của các lệnh dưới đây là **chưa chạy** trong phiên viết file này. Phiên chính sẽ dán output thật từ `M18.log` vào chỗ có dòng `SỐ LIỆU`.
- Số đo của bản thử nghiên cứu (file 2 GiB, RAM, thời gian) là thật, lấy từ `docs/bang-chung/M18-nghien-cuu/07-file-2gb.out`, chạy trên máy ảo ngày 20/11/2026, qua vòng lặp nội bộ chứ không qua mạng thật. Mình ghi rõ "số của bản thử" mỗi lần dùng.

Số e2e (điền sau):

<!-- SỐ LIỆU: số khẳng định đạt của e2e M18, dòng "TỔNG KẾT: N khẳng định đạt, M không đạt" cuối docs/bang-chung/M18.log -->

### 2.1 Tự thử lại: sáu lệnh

Kho thử là `versitygw` (một máy chủ S3 nhỏ, backend lưu mỗi object thành một file thường) nghe ở `127.0.0.1:7070` trong máy ảo. Script dựng nó nằm ở `scripts/e2e-m18/versitygw.sh`; nó ghi hai khoá sinh tại chỗ vào `/root/s3thu.env` (0600) và không bao giờ in chúng. Hai khoá đi vào TPAN qua `--keys-stdin`, đọc thẳng từ file đó trong máy ảo; không khoá nào nằm trên dòng lệnh bạn gõ.

Điều kiện: máy ảo có stack và một site, ví dụ làm theo mục 2.1 của `09-m9-backup-va-cron.md` (`tpan stack install --web nginx --yes`, rồi `tpan site add m18.example.com --email admin@example.com --yes`). Mọi lệnh gõ trên Mac, trong thư mục repo.

```bash
# 1. Dựng kho thử trong máy ảo (tải .deb của versitygw, kiểm sha256 theo checksums.txt của bản phát hành)
scripts/vm.sh push scripts/e2e-m18/versitygw.sh /tmp/versitygw.sh && scripts/vm.sh root bash /tmp/versitygw.sh

# 2. Đặt đích. Hai khoá đọc từ /root/s3thu.env, đi qua stdin. Lệnh in khoá sao lưu MỘT lần: chép ra chỗ khác.
scripts/vm.sh root bash -c '. /root/s3thu.env && printf "%s\n%s\n" "$S3_ACCESS_KEY" "$S3_SECRET_KEY" | tpan backup target set --endpoint "$S3_ENDPOINT" --region "$S3_REGION" --bucket "$S3_BUCKET" --keys-stdin --yes'

# 3. Sao lưu site; cuối lệnh có một lượt đẩy lên kho
scripts/vm.sh root tpan backup create m18.example.com --yes

# 4. Bản vừa tạo nằm ở máy và kho; kho có thêm gói panel
scripts/vm.sh root tpan backup list m18.example.com
scripts/vm.sh root tpan backup list --remote

# 5. Giả lập máy mất bản sao lưu: xoá file trên máy (dòng trong tpan.db vẫn ghi "stored")
scripts/vm.sh root bash -c 'rm -f /var/lib/tpan/backups/m18.example.com/*.tar.gz'

# 6. Khôi phục: TPAN thấy bản chỉ còn trên kho, tải về (một job), kiểm sha256, rồi khôi phục (một job nữa)
scripts/vm.sh root tpan backup restore m18.example.com --yes
```

Lệnh 4 gồm hai dòng gõ liền; mình đếm là một bước. Điều bạn sẽ thấy, theo mã (chưa chạy trong phiên này):

- Lệnh 2: job `backup.target.set` thành công, rồi một khối in khoá sao lưu dạng `tpan-key-1-...` kèm dấu vân tay 8 ký tự hex. Đây là lần duy nhất khoá hiện ra trên màn; xem lại bằng `sudo tpan backup key show --reveal`.
- Lệnh 3: job `backup.create`, rồi một job `backup.push` cho bản đó, rồi một job `backup.panel` (gói panel). Hai job sau có chủ là `admin` trong Lịch sử.
- Lệnh 4: cột `Ở ĐÂU` ghi `máy+kho`. `--remote` liệt kê object dưới tiền tố `sites/<domain>/...` và `panel/tpan-<YYYYMMDD-HHMMSS>.tar.gz.age`.
- Lệnh 6: hai job liên tiếp, `backup.pull` rồi `backup.restore`.

Muốn thử thêm bản sao lưu bị hỏng trên kho, xem câu 6 ở mục 6.

### 2.2 Số đo của bản thử (không phải e2e)

File ngẫu nhiên 2 GiB, tải lên kho thử bằng client tự viết, phần 8 MiB, trong máy ảo 2 CPU 4 GB (`07-file-2gb.out`):

| Việc | Thời gian | Đỉnh RAM |
|---|---|---|
| Tải lên (256 phần × 8 MiB), versitygw | 12,4 giây | 24,0 MB |
| Tải về theo dòng và tính sha256 | 3,8 đến 5,5 giây | 6,2 đến 6,5 MB |

sha256 tải về khớp bản gốc ở cả bốn lần thử. Hai điều bản thử còn cho thấy, và cả hai ảnh hưởng thiết kế:

- Cùng một file, hai cỡ phần khác nhau cho **hai ETag khác nhau** (`7adb…-256` và `ad69…-128`). ETag của object nhiều phần là "md5 của các md5" cộng số phần, không phải băm nội dung.
- Ghi đè đúng 1 byte của object trên đĩa của kho: HEAD vẫn trả đúng cỡ và đúng ETag cũ; GET trả dữ liệu hỏng; sha256 do ta tính thì khác. Đây là bằng chứng cho luật "không tin ETag".

Số của máy cloud-init và của gói panel (điền sau, từ `M18.log`):

<!-- SỐ LIỆU: thời gian lần khởi động đầu của máy cloud-init, cộng các "seconds" trong /var/lib/tpan/first-boot.json ở phần L của docs/bang-chung/M18.log -->

<!-- SỐ LIỆU: cỡ gói panel, byte của object panel/tpan-*.tar.gz.age trên kho, từ phần B hoặc C của docs/bang-chung/M18.log -->

## 3. Đi qua mã theo thứ tự chạy

### 3.1 Đặt đích: `tpan backup target set`

File `cmd/tpan/backup_s3.go`, hàm `backupTargetSetCmd` (khoảng dòng 282 tới 365), rồi `core.Remote.PrepareSetTarget` trong `internal/core/remote.go`.

1. CLI đọc hai khoá từ stdin (`--keys-stdin`, `readKeysStdin`: dòng 1 access key, dòng 2 secret key) hoặc hỏi trên terminal (secret không hiện chữ). Không có cờ nào nhận khoá trên dòng lệnh, vì dòng lệnh hiện trong `ps` và lịch sử shell.
2. `PrepareSetTarget` kiểm từng trường bằng hàm của `internal/s3` (`CheckEndpoint`, `CheckRegion`, `CheckBucket`, `CheckPrefix`, `CheckKeep`...). Một ví dụ: endpoint `http://` chỉ được khi host là loopback (`127.0.0.1`, `localhost`). Lý do: bản sao lưu có `wp-config.php` và bản xuất DB, thân request đi dạng rõ nếu không có TLS.
3. Dựng một Plan gồm: bước `S3Probe` (ghi một object nhỏ ngẫu nhiên `probe/<16 hex>`, HEAD, đọc lại so từng byte, xoá, HEAD lần nữa phải ra "không có"), bước `SecretFile` ghi `/etc/tpan/s3.json`, và nếu chưa có khoá sao lưu thì một `SecretFile` nữa cho `/etc/tpan/backup.key` (chỉ CLI sinh khoá; API không bao giờ sinh).
4. Chỉ khi job thành công, `afterSetTarget` mới ghi các setting `s3.*`. Thứ tự "thử thật rồi mới ghi" bảo đảm không bao giờ lưu một đích chưa ghi, đọc, xoá được.

Mã đích (`TargetID`) là 16 ký tự hex đầu của `sha256(endpoint + "\n" + bucket + "\n" + prefix)`. Mỗi dòng `backups` nhớ mã này, nên khi bạn đổi đích, TPAN biết bản nào nằm ở đích cũ. Chú ý: mã **không** gồm hai khoá truy cập. Đổi khoá mà giữ nguyên chỗ thì mã không đổi (dùng ở bảng "mất khoá" bên dưới).

### 3.2 Vì sao đổi endpoint phải nhập lại khoá (R1)

Đây là chỗ bản soát hẹp tìm ra, và là điều người review rất dễ hỏi. Tình huống: một phiên admin bị chiếm (kẻ xấu chưa có root) gọi API đặt đích, để trống hai khoá (nghĩa là "giữ hai khoá đang lưu") và đổi endpoint sang máy của kẻ đó. Nếu TPAN làm theo, bước kiểm đầu tiên (`Check`, kể cả khi chỉ xem trước) sẽ gửi một request ký bằng khoá thật tới máy của kẻ xấu: lộ access key đầy đủ và chữ ký để dò secret key.

Luật sau sửa: giữ khoá cũ chỉ khi endpoint (sau chuẩn hoá), vùng, bucket và nội dung CA đều trùng đích đang lưu, và kiểm **trước khi dựng client**:

```go
// internal/core/remote.go, PrepareSetTarget (rút gọn)
cur, ok, err := r.Config(ctx)
...
if !ok || cur.Endpoint != ep || cur.Region != region || cur.Bucket != in.Bucket || !bytes.Equal(curCA, ca) {
    return nil, refuse(s3.CodeKeysInvalid, i18n.T(
        "đổi endpoint, vùng, bucket hay CA thì gửi lại cả hai khoá (access key và secret key)", ...
```

Không request nào ký bằng khoá cũ đi tới host mới. Luật áp cho cả CLI lẫn API. Test: `server.TestKhongLoKhoaS3` có một máy chủ giả ở endpoint khác và khẳng định nó không nhận request nào.

### 3.3 Sao lưu xong thì đẩy: `Remote.Sync`

`tpan backup create` (và lịch hằng đêm, vốn gọi đúng lệnh này) chạy Plan `backup.create` của M9, xong mới gọi `Remote.Sync{Wait: true}` ở cuối (`backupSyncTail` trong `cmd/tpan/backup_s3.go`). Hai việc là hai job khác nhau, và đó là lý do kho hỏng không làm hỏng việc sao lưu (bất biến B1): không bước nào của `backup.create` chạm mạng. Lệnh thoát 0 khi bản trên máy xong, kể cả khi đẩy lỗi; nó in ra stderr câu "đã sao lưu trên máy, CHƯA lên kho: <lý do>; sẽ đẩy bù ở lượt sau, hoặc: sudo tpan backup push".

Ngoài lệnh gõ tay, còn một **bộ đẩy nền** trong `tpan serve` (`internal/server/pusher.go`), cứ 2 phút gọi một lượt `Sync{}`. Khi không có việc thì lượt chỉ là vài truy vấn SQLite, không gọi mạng, nên nhịp 2 phút rẻ.

**Vì sao lượt đẩy không giữ khoá job của máy.** Tải vài GB qua mạng chậm có thể mất hàng giờ. Nếu job đẩy giữ khoá toàn cục (cái khoá khiến mọi thao tác đổi máy phải xếp hàng, xem M15) thì trong lúc đó panel không tạo được site, health bỏ mọi vòng kiểm. Đẩy lại không đổi gì trên máy, nên không cần khoá đó. Thay vào đó lượt đẩy có khoá riêng: `flock` trên file `/run/tpan/remote.lock` (`system.DefaultRemoteLockPath`). Lệnh gõ tay chờ khoá tối đa 30 phút (`RemoteLockWait`); bộ đẩy nền không chờ, bận thì bỏ. Thứ tự luôn là khoá lượt đẩy trước, khoá job sau, và lượt đẩy không bao giờ lấy khoá job, nên hai khoá không thể chờ nhau vòng tròn. Test: `core.TestSyncKhongGiuKhoaToanCuc`.

Trong `Sync` có mười việc theo thứ tự. Những việc bạn nên kể được:

1. Chưa đặt đích: trả về ngay, không làm gì.
2. Lấy khoá lượt đẩy; bận thì bỏ.
3. Nếu giờ hiện tại chưa tới `s3.retry_at` (nhịp lùi, xem 3.6): bỏ, trừ khi lệnh gõ tay `tpan backup push` (cờ `Retry`).
4. Tính danh sách bản cần đẩy bằng `pickPush`. Không có bản nào và không cần gói panel: trả về **không gọi mạng**.
5. `Check` một lần (ListObjectsV2 một kết quả). Hỏng là lỗi của đích: cầu dao (3.6).
6. Đẩy lần lượt, mới nhất trước; mỗi bản một job `backup.push`. Sau mỗi bản đạt: dọn kho của domain đó.
7. Cuối lượt: nếu cần, tạo và đẩy gói panel (3.8).

### 3.4 Một bản đẩy lên kho: `steps.S3Push`

File `internal/steps/s3.go`, hàm `(*S3Push).Apply`. Sáu việc, theo đúng thứ tự trong mã:

1. `Stat` file cục bộ: phải là file thường, cỡ bằng `backups.size`.
2. `sha256sum` file cục bộ (chạy bằng lệnh, nên có dấu vết trong log job) phải bằng sha256 ghi lúc tạo. Sai thì **không gửi byte nào**: đẩy một bản đã hỏng lên kho chỉ tạo cảm giác an toàn giả.
3. `AbortUploads(key, 0)`: dọn phần tải dở của chính key này, nếu lần chạy trước bị giết giữa chừng.
4. `OpenRead` rồi `Put` theo dòng. sha256 của các byte đã gửi phải bằng sha256 gốc; khác thì xoá object (file đổi giữa chừng).
5. **Đọc lại cả object** từ kho vào `io.Discard`, tính sha256 và đếm byte:

```go
// internal/steps/s3.go, (*S3Push).Apply, việc 5
rn, rsum, err := s.Store.Get(ctx, key, &countWriter{w: io.Discard, fn: down.update})
if err != nil {
    return drop(err) // object chưa qua đọc lại thì không để trên kho
}
if rn != size || rsum != sum {
    return drop(fmt.Errorf("%w: đọc lại %s được %d byte, sha256 %s; đã gửi %d byte, sha256 %s",
        ErrRemoteMismatch, key, rn, rsum, size, sum))
}
```

6. Ghi một dòng kết quả vào log job (`engine.Note`), không bao giờ chứa bí mật.

**Vì sao đọc lại cả object, không tin ETag.** Chữ ký SigV4 của từng phần (phần SigV4 ở mục 4) chỉ chứng minh từng phần tới nơi nguyên vẹn. Nó không chứng minh object sau khi kho ghép các phần lại giống file của bạn. ETag thì bản thử đã cho thấy không đổi khi dữ liệu hỏng (mục 2.2). Đọc lại là phép kiểm duy nhất không phải tin kho. Cái giá là mỗi bản tốn thêm một lượt tải về; không có chế độ "kiểm nhanh" nào, đó là bất biến B2: không dòng nào có `remote='stored'` trước khi đọc lại khớp. Test: `steps.TestS3PushDocLaiMoiDanhDau`.

Chỉ sau khi job thành công, `after` của Plan mới gọi `MarkBackupStored`, một transaction có điều kiện trong `internal/store/remote.go`.

Hệ quả cần nói rõ về **bản site không mã hoá**: file đẩy lên kho chính là `.tar.gz` của M9, gồm mã nguồn, `wp-config.php` (có mật khẩu DB của site) và bản xuất DB. Ai đọc được bucket thì đọc được những thứ đó. M18 chỉ mã hoá gói panel, không mã hoá bản site (lý do ở mục 5.1). Hãy dùng bucket riêng tư và bật mã hoá phía kho nếu nhà cung cấp có.

### 3.5 Giữ bản trên kho tách với trên máy, và `remoteSurvives`

Trên máy, luật giữ của M9 (`backup settings --keep`, mặc định 3) chỉ đếm dòng `local=1`. Bản bị dọn khỏi máy mà đã lên kho thì dòng thành `local=0, remote='stored'`: vẫn có trong `tpan backup list`, ghi "kho". Trên kho, luật riêng là `s3.keep` (1 tới 365, mặc định 7 bản mỗi site).

Có một cái bẫy mà bản thiết kế đầu mắc phải (điểm S2 của bản soát): hai luật cho kết quả lệch nhau thì sinh vòng lặp. Ví dụ `s3.keep = 2`; bản B3, B4 đã lên kho, B5 mới. Lượt đẩy dọn B3 khỏi kho vì đã vượt 2 bản. Nhưng B3 vẫn còn file trên máy (luật máy giữ 3), dòng của nó quay về `remote=''`, và nếu "chọn bản cần đẩy" chỉ nhìn "chưa lên kho" thì lượt sau đẩy B3 lên lại, rồi dọn lại, mãi mãi, tốn băng thông vô ích.

Cách sửa là **một hàm duy nhất** trả lời "bản này có được sống trên kho không", dùng cho cả hai việc: chọn bản cần đẩy và chọn bản cần dọn. Đó là `remoteSurvives` trong `internal/core/remote.go`:

```go
func remoteSurvives(row Backup, rows []Backup, targetID string, keep int) bool {
    if row.Reason == ReasonBeforePurge {
        return true // bản trước khi xoá site: không bao giờ bị dọn
    }
    ...
    for i, s := range rows { // đếm bản MỚI HƠN row, cùng domain, đã stored ở đích này
        ...
    }
    return !scheduled(row.Reason) || n < keep
}
```

Hiểu bằng lời: bản `manual` hay `schedule` sống khi có ít hơn `keep` bản `manual`/`schedule` mới hơn nó đã nằm trên kho. Bản `before-purge` luôn sống. Bản lý do khác (`before-restore`, `before-import`) sống khi không có bản cùng lý do mới hơn. Vì cả "đẩy" và "dọn" cùng hỏi hàm này, bản bị dọn chắc chắn không bao giờ vào hàng chờ đẩy lại. Test: `core.TestDonTrenKhoKhongDayLai` (bất biến B14). Lệnh gõ tay `tpan backup push <id>` cho một bản nằm ngoài `s3.keep` bị từ chối `backup_outside_keep` vì cùng lý do.

Dọn trên kho (`pruneRemote`) xoá object **trước**, sửa DB **sau**: hỏng giữa chừng thì lần sau làm lại, và xoá một object đã mất vẫn tính là thành công.

### 3.6 Kho hỏng: cầu dao và nhịp lùi

Hai loại lỗi được phân biệt (`s3.IsTargetError`):

- **Lỗi của đích**: 401/403 (sai khoá), bucket không có, lỗi mạng, DNS, TLS, hết giờ, và 5xx hay 429 sau khi đã thử lại. Nó không nói gì về một bản sao lưu cụ thể.
- **Lỗi của riêng bản**: file cục bộ sai cỡ hay sai sha256, kho trả về dữ liệu khác thứ đã gửi, kho từ chối đúng object đó.

Bản chỉ bị đếm lỗi (`remote_tries`) khi lỗi là của riêng nó; đủ 5 lần (`RemoteMaxTries`) thì bộ đẩy nền thôi, tới khi bạn gõ `tpan backup push <id>` (đặt lại số đếm). Lý do tách: nếu kho sập ba tiếng, mọi bản cùng lỗi, và không bản nào đáng bị gạch tên vĩnh viễn vì chuyện đó.

Khi gặp lỗi của đích, lượt đẩy **dừng ngay** (cầu dao): ghi `s3.target_error`, tăng `s3.target_failures`, đặt `s3.retry_at = now + lùi`, với lùi là 10 phút, 30 phút, 1 giờ, rồi 6 giờ (`RemoteBackoff`). Từ lúc đó bộ đẩy nền bỏ qua mỗi lượt 2 phút một lần cho tới `retry_at`, không tạo job nào và không đập vào kho đang chết. Chỗ tinh tế (điểm R2 của bản soát): `Check` đạt **không** xoá lỗi, vì kho có thể cho phép LIST mà từ chối PUT. Chỉ khi một job đẩy hay job gói thật sự đạt thì `targetOK` mới xoá `target_error`, `target_failures`, `retry_at`.

Trên panel, `tpan.s3:fresh` (cảnh báo, nhẹ) thấy `s3.target_error` khác rỗng hay dòng `remote='failed'` thì báo. Mạng đứt lúc đang đẩy giữa chừng: client thử lại mỗi request tối đa 4 lần (nghỉ 0,5, 1, 2 giây); tải nhiều phần lỗi thì gọi `AbortMultipartUpload` để không để phần dở nằm lại; nếu tiến trình bị giết thì lần đẩy sau, việc 3 ở trên, dọn phần dở của lần trước, và lượt đẩy huỷ mọi lần tải dở cũ hơn một ngày.

### 3.7 Khôi phục từ kho

`tpan backup restore` giữ nguyên lệnh của M9. Chỗ thêm (`cmd/tpan/backup.go`, quanh dòng 395 đến 415, hàm `restorePull` ở `backup_s3.go`):

- `Backups.NeedsPull` (`internal/core/backup.go`): bản cần tải về là bản `stored` ở **đích đang đặt**, và file của nó không còn trên máy (`local=0`, hoặc file ở `path` không có hay không phải file thường).
- CLI gặp bản đó thì chạy Plan `backup.pull`, **một job riêng**, in tiến độ, rồi dựng lại Plan khôi phục của M9. Bạn thấy hai job.
- Bản `stored` ở một đích khác mà không còn trên máy: từ chối `backup_not_on_machine`, chỉ đường "đặt lại đích đó để tải về". Qua API, `POST /api/v1/backups/{id}/restore` trả 409 cho bản cần tải về; admin gọi `POST .../pull` trước.

Plan `backup.pull` có một bước, `steps.S3Pull` (`internal/steps/s3.go`):

1. `Check` theo trạng thái: `Dest` là file thường, đúng cỡ, `sha256sum` đúng thì không làm gì. Nhờ vậy chạy lại là an toàn.
2. `Apply`: luật chặn đầy đĩa của M9 (`SpaceOK`); xoá `Dest.part` còn sót; tải vào `Dest.part` theo dòng, **vừa tải vừa tính sha256**; số byte và sha256 phải bằng giá trị ghi trong `tpan.db` từ lúc tạo, khác thì xoá `.part` và lỗi "bản trên kho không khớp sha256 ghi lúc tạo"; đúng thì `Rename` sang `Dest`.

Điểm cần nhấn: sha256 để so lấy từ `tpan.db` **trên máy**, không lấy từ kho. Người ghi được vào bucket có thay một bản sao lưu bằng bản có mã độc thì tải về sai sha256, dừng ngay, trước khi có byte nào chạm vào site. Rồi job khôi phục của M9 còn kiểm sha256 một lần nữa (`BackupExtract`) trước khi giải nén. Đây là bất biến B4: tải về là một job xong hẳn trước khi job khôi phục bắt đầu, và khôi phục không ghi gì vào site trước khi bản tải về qua kiểm. Test: `steps.TestS3PullSaiSha256KhongDeLaiFile`, `core.TestKhoiPhucBanTrenKhoPhaiTaiVeTruoc`.

`S3Pull` chỉ chạy dưới khoá lượt đẩy, nên hai lần tải cùng một bản không giẫm lên `.part` của nhau, và việc dọn trên kho (cũng chỉ chạy trong lượt đẩy) không chen vào giữa lúc tải.

### 3.8 Gói panel và khoá sao lưu

Gói panel trả lời câu hỏi "mất máy thì mất `tpan.db` luôn à?". Nó là file `tpan-<YYYYMMDD-HHMMSS>.tar.gz.age` (giờ UTC), tạo ở cuối mỗi lượt đẩy khi có thay đổi (hoặc cũ hơn 24 giờ, `PanelMaxAge`). File `internal/core/panel.go` dựng Plan `backup.panel` gồm hai bước:

1. `steps.PanelBundle` (`internal/steps/panel.go`): chụp `tpan.db` bằng `VACUUM INTO` (một bản nhất quán, không cần dừng panel); ghi `manifest.json` (`format`, giờ tạo, tên máy, bản TPAN, **cả danh sách migration** của bảng `schema_migrations`, sha256 của DB); nén tar.gz; mã hoá theo dòng; luôn xoá thư mục tạm chứa bản rõ của `tpan.db`, kể cả khi lỗi.
2. `steps.S3Push` với key `panel/<tên>`: **cùng bước đẩy như bản site, nên cũng bị đọc lại và so sha256**.

Gói **không** có: `s3.json`, `backup.key`, khoá TLS của panel, chứng chỉ Let's Encrypt. Nó có `tpan.db`, trong đó có mật khẩu băm argon2id, sha256 của token phiên, cài đặt, lịch sử, và bảng sha256 của mọi bản sao lưu.

**Khoá sao lưu** (`internal/agebox/agebox.go`): `tpan-key-1-` cộng 52 ký tự base32 chữ thường của 32 byte ngẫu nhiên từ `crypto/rand`. Mã hoá bằng thư viện `age`, chế độ mật khẩu (scrypt, hệ số công 15). Khi giải mã luôn đặt trần hệ số công bằng đúng 15 (`SetMaxWorkFactor`), để kẻ ghi được vào bucket không đặt được file có hệ số công cực cao mà làm hết RAM của máy lúc giải mã. Hai điều khác:

- Mã hoá đối xứng có xác thực: sửa một byte của gói thì giải mã lỗi; không có khoá thì không tạo được gói nào giải ra được. Đó là lý do chọn đối xứng, không chọn cặp khoá công khai (xem 5.2).
- File mở được bằng lệnh `age` của Ubuntu (`age -d`), không cần TPAN. Bản thử 12 đã mở thật một gói, mật khẩu đưa qua terminal giả, sha256 khớp.

Chỉ CLI sinh khoá: lần đầu `target set`, hoặc `sudo tpan backup key new`. Không route API nào nhận hay trả khoá sao lưu. `sudo tpan backup key show` in dấu vân tay (8 hex đầu của sha256 chuỗi khoá), `--reveal` in cả khoá.

### 3.9 Mất khoá thì mất gì

Bảng này phải nhớ. Cột trái là tình huống, cột phải là hậu quả đã tính theo thiết kế. Chưa chạy lại từng dòng trong phiên viết này; lệnh để kiểm nằm ở mục 6.

| Tình huống | Mất gì, còn gì, làm gì |
|---|---|
| Quên khoá sao lưu, máy còn sống | Không mất gì. Khoá nằm ở `/etc/tpan/backup.key`; `sudo tpan backup key show --reveal` in lại. |
| **Mất khoá sao lưu** (máy cũng mất, không còn bản chép) | Mất **gói panel**: không mở được, nên mất danh sách user, gói, mật khẩu băm, cài đặt, lịch sử, bảng ghi sha256 của các bản sao lưu. **Không mất nội dung site**: các file `.tar.gz` của site trên kho không mã hoá, tải bằng công cụ S3 bất kỳ, giải nén ra `public/`, `db.sql`, `manifest.json`, rồi nhập tay vào site mới cùng domain. |
| **Lộ khoá sao lưu** | Ai có khoá mà đọc được bucket thì mở được gói panel: thấy mật khẩu băm (argon2id, không dò ngược được với mật khẩu mạnh), cài đặt, lịch sử. Nếu còn ghi được vào bucket thì giả được gói panel. Khoá sao lưu không cho quyền truy cập vào kho. Xử lý: `sudo tpan backup key new` (các gói sau mã hoá bằng khoá mới); gói cũ vẫn mở được bằng khoá đã lộ cho tới khi bị dọn khỏi kho (kho giữ `s3.keep` gói mới nhất) hoặc bạn xoá tay trên kho; đổi mật khẩu admin. |
| **Mất hai khoá truy cập** (access key và secret key), máy còn | Khoá nằm ở `/etc/tpan/s3.json` trên máy nên TPAN vẫn đẩy được. Mất cả ở máy lẫn ở bạn: cấp khoá mới ở nhà cung cấp rồi `tpan backup target set` lại với khoá mới. Mã đích tính từ endpoint, bucket, tiền tố, không từ khoá, nên mọi bản `stored` vẫn là "đúng đích". |
| **Lộ hai khoá truy cập** | Ai có chúng đọc được **mọi bản site** (không mã hoá), xoá được hay ghi đè được object. Không giả được gói panel (cần khoá sao lưu). Không thay âm thầm được bản site mà khôi phục không phát hiện: sha256 chuẩn nằm trong `tpan.db` trên máy. Nhưng xoá thì xoá được: `tpan.s3:target` báo khi bản mới nhất của một site không còn trên kho, và bản trên máy còn đó tới khi máy mất. Xử lý: thu hồi khoá ở nhà cung cấp, cấp khoá mới, `tpan backup target set` với khoá mới. |
| Mất máy, còn khoá sao lưu | Giải mã gói panel bằng `age -d` ra `tpan.db` cũ, rồi dựng lại bằng tay (mục 5.1). Chú ý: gói panel **không chứa hai khoá truy cập**, nên để tải được gói từ kho, bạn vẫn phải lấy hai khoá truy cập từ trang quản lý của nhà cung cấp kho. |
| Ai đọc được bucket (khoá chỉ đọc bị lộ, bucket để công khai nhầm, nhân viên nhà cung cấp) đọc được gì | Gói panel: không, nó mã hoá. Bản site: **có**, ở bản này không mã hoá: mã nguồn, `wp-config.php` (mật khẩu DB của site), bản xuất DB (tài khoản, mật khẩu băm của WordPress, nội dung). Dùng bucket riêng tư và mã hoá phía kho. |

### 3.10 Bí mật nằm ở đâu và vì sao an toàn

Ba file, đều `root:root 0600` trong `/etc/tpan` (thư mục 0700 từ M3):

| File | Nội dung |
|---|---|
| `/etc/tpan/s3.json` | hai khoá truy cập |
| `/etc/tpan/s3-ca.pem` | chứng chỉ gốc riêng của kho (tuỳ chọn) |
| `/etc/tpan/backup.key` | khoá sao lưu |

Các lớp bảo vệ, mỗi lớp có test:

- **Không vào bản xem trước.** Engine đọc nội dung cũ của mọi file trong `Info().Files` của một bước để tạo bản xem trước (`old`, `new`) và băm vào `managed_files`. Hai bước ghi bí mật mới, `steps.SecretFile` và `steps.SecretAbsent` (`internal/steps/m18_types.go`), luôn trả `Info().Files` rỗng, nên nội dung ba file này không bao giờ vào bản xem trước hay `managed_files`. `steps.File` và `steps.Absent` từ chối đúng ba đường dẫn này, để không ai ghi chúng bằng đường khác. Test: `steps.TestBuocBiMatKhongKhaiFiles`.
- **API không bao giờ trả khoá.** `GET /api/v1/backups/target` trả `TargetView`: mọi trường không bí mật, cộng `access_key_hint` (dấu `…` và bốn ký tự cuối khi access key dài từ 12 ký tự, không thì chỉ ghi số ký tự), `has_ca`, `has_backup_key`, và dấu vân tay của khoá sao lưu. Không có trường nào chứa secret key, kể cả dạng che. Handler bọc secret key vào `system.Secret` ngay sau khi đọc thân request; kiểu này (`internal/system/types.go`) tự in ra `••••` khi bị in, ghi log hay đổi sang JSON, nên lỡ `fmt.Println` cũng không lộ. Test: `server.TestKhongLoKhoaS3` đặt đích bằng khoá đánh dấu, gọi mọi route GET và mọi route M18 (cả `?dry_run=1`, ba vai), giải base64 các trường `old`, `new` rồi quét; quét cả `job_events`, `audit`, `settings`.
- **Câu lỗi của kho được làm sạch.** Kho độc hại có thể chép lại header `Authorization` vào thân lỗi. Client S3 (`internal/s3/types.go`) thay mọi lần xuất hiện của access key, secret key, chữ ký bằng `••••`, bỏ ký tự điều khiển, cắt còn 300 ký tự, đọc tối đa 64 KB. Test: `s3.TestLoiKhongCoKhoa`.
- **Không có đường API nào sinh hay trả khoá sao lưu**; chỉ có CLI chạy bằng root.
- User thường không đặt, xem, thử, gỡ đích; không thấy chi tiết kho (endpoint, bucket, câu lỗi) kể cả qua Lịch sử, vì job đẩy, tải, gói panel đều thuộc `admin`. Test: `server.TestUserKhongDungDuocS3`.

### 3.11 cloud-init: máy mới tự thành máy có panel

`tpan cloud-init render --web nginx|apache [--sites FILE] ...` (`cmd/tpan/cloudinit.go`, `internal/core/cloudinit.go`, `internal/cloudinit/cloudinit.go`). Không cần root, không đổi gì trên máy, chỉ sinh user-data.

**Vì sao user-data phải coi là không bí mật.** Bản thử 08 trên máy ảo: user-data nằm ở `/var/lib/cloud/instance/user-data.txt` chỉ root đọc, nhưng trên cloud thật có dịch vụ metadata (địa chỉ 169.254.169.254) mà mọi tiến trình trên máy hỏi được (kể cả PHP của một site bị chiếm). Thiết kế coi user-data là "ai trên máy cũng đọc được, mãi mãi". Hệ quả:

- **User-data chỉ có bản băm mật khẩu.** `render` sinh một mật khẩu quản trị ngẫu nhiên 32 ký tự (khoảng 190 bit), in ra stderr **một lần** cho người gõ lệnh, và chỉ đặt bản băm argon2id vào user-data. Bản băm của bí mật 190 bit không dò ngược được. Không nhận mật khẩu do bạn tự chọn, vì bản băm của mật khẩu yếu thì dò được. Test: `cloudinit.TestKhongCoBiMatDangRo`.
- **Chỉ một `runcmd` cố định.** Bản thử 08: lệnh lỗi ở giữa `runcmd` thì các lệnh sau vẫn chạy và `cloud-init status` vẫn báo `done`; chỉ lệnh cuối quyết định mã thoát. Vì thế user-data chỉ có đúng một lệnh, `/usr/local/sbin/tpan-first-boot`, để mã thoát của script là mã thoát của cả lượt. Mọi giá trị do người dùng đưa (tiêu đề site, email, `sites.yaml`...) chỉ đi vào **nội dung file** (base64), không bao giờ vào `runcmd`, đường dẫn hay khoá YAML. File ra được sinh bằng bộ ghi YAML từ cấu trúc Go (`userData` trong `internal/cloudinit/cloudinit.go`), không ghép chuỗi. Test: `cloudinit.TestChenNoiDung`.
- **sha256 ghim cho file chạy.** Script tải `tpan-linux-<arch>` rồi kiểm sha256 đã ghim sẵn trong script trước khi cài; sai thì xoá file tải về và thoát lỗi. sha256 lấy từ `--sums FILE` (bạn đưa), hoặc từ `SHA256SUMS` tải qua **https** khi không đưa `--sums`; `--base` là `http://` mà thiếu `--sums` thì từ chối. API `POST /api/v1/cloud-init/render` còn chặt hơn: bắt buộc nhận hai sha256, không tải gì từ `base`.
- **Script không thay file chạy sau lần đầu.** Đây là chỗ nhiều người bỏ sót:

```sh
# script do cloudinit.Script sinh (internal/cloudinit/cloudinit.go, scriptTmpl)
if [ -e /var/lib/tpan/first-boot.json ] && [ -x "$bin" ]; then
  exec "$bin" cloud-init run
fi
```

Lý do: máy có thể đã `self-update` (M12, M15) lên bản mới hơn bản đã ghim trong user-data. Nếu script chạy lại (cloud-init chạy lại `scripts_user` khi tạo máy từ snapshot) và kiểm sha256 theo bản cũ thì sẽ hạ file chạy về bản cũ. Nên file trạng thái `first-boot.json` được tạo **trước** việc đầu tiên; từ lúc đó script chỉ giao cho `tpan cloud-init run`, không đụng file chạy nữa. Test: `cloudinit.TestScriptKhongThayFileChaySauLanDau` (bất biến B15).

**Năm việc của lần khởi động đầu** (`firstBoot.run` trong `cmd/tpan/cloudinit.go`, theo thứ tự trong mảng `jobs`):

1. `setup`: đúng Plan của `tpan setup` (mở `tpan.db`, cấu hình panel). Không sinh mật khẩu.
2. `admin`: nếu DB chưa có mật khẩu băm của admin thì ghi `admin_hash` từ `config.json`, sau khi kiểm đúng dạng argon2id.
3. `stack`: `stack install --web nginx|apache`.
4. `sites` (chỉ khi có file site): `apply` file `sites.yaml`. Mật khẩu quản trị WordPress của từng site được ghi nối vào `/root/tpan-first-boot-passwords.txt` (0600), không in ra output.
5. `health`: như `tpan health`; có phép kiểm lỗi thì việc này lỗi.

Mỗi việc ghi một dòng có giờ vào `/var/log/tpan/first-boot.log` và cập nhật `first-boot.json` (tên việc, trạng thái, `job_id`, số giây, lỗi). Xem bằng `sudo tpan cloud-init status`.

**Chạy lại thì làm tiếp việc chưa đạt** (R14 của bản soát hẹp). `tpan cloud-init run` không `--force` không chạy lại việc đã `done` hay `skipped` ở lần trước. Lý do đã tính: máy tạo từ snapshot của một máy đã xoá vài site, mà chạy lại việc `sites` thì `apply` sẽ tạo lại đúng các site đã xoá. `--force` mới chạy từ đầu; lúc đó mọi Plan bỏ qua bước đã đúng. Lần khởi động đầu đã xong và không `--force`: in "lần khởi động đầu đã xong lúc <giờ>" rồi thoát 0.

User-data tối đa mặc định 16384 byte (giới hạn nhỏ nhất đã tra, của AWS EC2); lớn hơn thì từ chối `cloudinit_too_large` kèm cỡ thật. `sites.yaml` được nén gzip trước khi base64 vì nó là phần lớn nhất.

Số đo thời gian lần khởi động đầu: xem chỗ giữ chỗ ở mục 2.2.

### 3.12 Health và API, ngắn

- Hai phép kiểm, chỉ tồn tại khi đã đặt đích (máy không đặt đích thì kết quả `tpan health` không đổi một dòng nào so với v0.2.0, bất biến B11): `tpan.s3:target` (nặng, mỗi giờ: nối được kho không, và với mỗi site, `Head` bản `stored` mới nhất, mất hay sai cỡ thì báo lỗi) và `tpan.s3:fresh` (nhẹ, chỉ đọc DB: có bản đẩy lỗi, site thiếu bản lên kho trong 48 giờ, gói panel cũ, chưa có khoá sao lưu thì cảnh báo).
- Mọi route mới là `admin` (`internal/server/routes.go`). `POST /api/v1/backups/{id}/push` trả 202 và đưa vào hàng chờ của bộ đẩy nền; hàng chờ nằm trong bộ nhớ nên mất khi panel khởi động lại, câu trả lời của API nói rõ điều này. `POST .../pull` chạy trong một goroutine riêng, không qua hàng chờ đó.
- Phép kiểm "job treo" của M4 bỏ qua ba loại `backup.push`, `backup.pull`, `backup.panel`, vì chúng được phép chạy lâu.

## 4. Kiến thức nền

Chỉ những khái niệm M18 thật sự dùng. Mỗi mục: nó là gì, chỗ trong mã, lệnh để tự nhìn.

**S3 và SigV4.** S3 là giao thức HTTP để lưu "object" (file có tên) trong "bucket". Mỗi request phải ký bằng thuật toán SigV4: lấy request (phương thức, đường dẫn đã chuẩn hoá, query, vài header, sha256 của thân), dùng secret key (qua một chuỗi HMAC-SHA256) để ra một chữ ký đặt trong header `Authorization`. Secret key không bao giờ đi trên dây. Trong mã: `internal/s3/sign.go` (122 dòng) và `client.go`. Thân request luôn mang sha256 thật (không dùng `UNSIGNED-PAYLOAD`), nên kho tự từ chối một phần bị hỏng trên đường đi. Tự nhìn: `go test ./internal/s3 -run SigV4` trên Mac chạy vectơ mẫu công khai của AWS (test `TestSigV4VectoAWS`). Chưa chạy trong phiên viết file này.

**Tải nhiều phần (multipart) và ETag.** File lớn không đẩy được bằng một PUT. S3 cho mở một lần tải (`POST ?uploads`), gửi từng phần (`PUT ?partNumber=N&uploadId=...`), rồi ghép (`POST ?uploadId=...`). TPAN dùng phần 8 MiB, tải tuần tự, nên RAM đỉnh khoảng 24 MB cho mọi file tới khoảng 70 GiB (8 MiB × 9000 phần); file lớn hơn thì phần to ra để không quá 9000 phần. Phần tải dở nằm lại trong kho nếu bị đứt, nên có `AbortUploads`. Trong mã: `internal/s3/multipart.go`. ETag của object nhiều phần là "md5 của các md5" cộng số phần, không phải băm nội dung, nên không dùng để kiểm toàn vẹn. Tự nhìn: số đo ở mục 2.2.

**sha256 và "đọc lại".** sha256 là hàm băm: cùng nội dung ra cùng chuỗi 64 hex, đổi 1 bit ra chuỗi khác hẳn. Bản sao lưu có sha256 ghi lúc tạo trong `tpan.db`; mọi nơi bản đó đi qua (đẩy lên, đọc lại, tải về, giải nén) so lại với con số đó. Tự nhìn trên máy ảo: `scripts/vm.sh root sha256sum /var/lib/tpan/backups/m18.example.com/*.tar.gz` rồi so với cột sha256 của bảng `backups` (cách đọc bảng xem M9).

**Khoá `flock` và hai khoá khác nhau.** `flock` là khoá file của Linux: tiến trình nào giữ khoá trên một file thì tiến trình khác phải chờ hoặc bỏ. Khoá job của máy (M1, M15) và khoá lượt đẩy `/run/tpan/remote.lock` là hai file khác nhau, nên đẩy lên kho (lâu) không chặn việc khác của panel. `/run` nằm trong RAM và sạch sau mỗi lần khởi động, nên khoá không bao giờ "kẹt" sau khi máy khởi động lại. Tự nhìn: `scripts/vm.sh root ls -l /run/tpan/` sau khi chạy một lượt đẩy.

**Cầu dao (circuit breaker) và nhịp lùi (backoff).** Khi một phụ thuộc bên ngoài hỏng, đừng cứ gọi liên tục: dừng hẳn một lúc, và kéo dài khoảng nghỉ nếu vẫn hỏng. TPAN lùi 10 phút, 30 phút, 1 giờ, 6 giờ. Trong mã: `backoffFor`, `bump`, `targetFailed` trong `internal/core/remote.go`. Tự nhìn: sau khi tắt kho thử, `sudo tpan backup target show` hiển thị `retry_at` và lỗi gần nhất.

**`age` và mã hoá bằng mật khẩu.** `age` là một công cụ mã hoá file đơn giản, có sẵn trong apt của Ubuntu. Chế độ mật khẩu dùng scrypt, một hàm cố ý tốn CPU và 32 MB RAM để dò mật khẩu rất chậm. Trong mã: `internal/agebox/agebox.go` (`Encrypt`, `Decrypt`, `GenerateKey`). Tự nhìn: `age -d` trên một gói panel đã giải ra từ kho, nhập khoá khi được hỏi. Chưa chạy trong phiên này; bản thử 12 đã chạy thật.

**cloud-init.** Là chương trình chạy một lần ở lần khởi động đầu của máy ảo trên cloud: đọc "user-data" (YAML bắt đầu bằng `#cloud-config`) và làm theo (`write_files` ghi file, `runcmd` chạy lệnh), bằng quyền root. Log ở `/var/log/cloud-init-output.log`. Tự nhìn trên máy ảo bất kỳ của Multipass: `scripts/vm.sh exec cloud-init status --long`.

**Interface trong Go (ôn lại từ 00).** `steps.ObjectStore` là một interface bảy phương thức (`Check`, `Put`, `Get`, `Head`, `Delete`, `List`, `AbortUploads`); client thật `*s3.Client` và kho giả `*s3.Mem` (`internal/s3/mem.go`) đều thoả nó. Nhờ vậy test của steps và core chạy được với kho giả có thể bơm lỗi (`Fail`) và bơm dữ liệu hỏng (`Corrupt`) mà không cần mạng.

## 5. Cách khác đã cân nhắc

### 5.1 Cái đã cắt, và vì sao

M18 bản đầu có ba thứ mà bản này không làm.

**Lệnh dựng lại cả máy** (`tpan machine restore --from-s3`, và chế độ dựng lại của `render`). Đã thiết kế, rồi cắt ở bản sửa sau soát. Lý do: phiên mất 6 giờ vì hết hạn mức model; bản soát tìm ra hai điểm nặng và khoảng tám điểm vừa chỉ nằm trên đường này (nối lại dòng `backups` theo object thật, tưởng nhầm "máy trắng", giàn giáo đổi mật khẩu DB, site tạm khoá mở công khai, dữ liệu máy cũ mang sang như `managed_files`...); và đặc tả cho phép cắt khi không làm chắc được trong hạn. Danh sách việc cần giải quyết trước khi làm lại ở mục 3.10 của bản thiết kế.

Những gì còn lại để dựng lại: gói panel mã hoá trên kho (mở được bằng `age`), bản sao lưu site trên kho, và một đường bằng tay đã chạy thật ở bản thử 09 (`09-dung-lai-may-thu.out`). Trên máy ảo mới, hai site trở lại và trả 200 sau **1 phút 50 giây** kể từ lúc tạo máy. Thứ tự đúng là:

1. Cài TPAN (và `tpan setup`).
2. Thay `tpan.db` mới bằng `tpan.db` lấy từ gói panel của máy cũ.
3. `tpan stack install` rồi bản PHP cần dùng.
4. Với từng site: `tpan site add` (chạy lại trên dòng đã có trong DB thì tạo site), chép file `.tar.gz` từ kho về đúng đường dẫn mà bảng `backups` ghi, rồi `tpan backup restore`.

Lệnh cụ thể từng bước nằm trong `docs/bang-chung/M18-nghien-cuu/09-dung-lai-may-thu.sh`; mình chưa chạy lại chúng trong phiên này. Chỗ phụ thuộc tên máy và IP cũ phải sửa tay: chứng chỉ tự ký của panel, bản ghi DNS của từng site, tiền tố của đích.

**Mã hoá bản sao lưu của site trên kho.** Khung thiết kế chỉ đòi mã hoá gói panel. Hệ quả là mất khoá sao lưu vẫn lấy lại được nội dung site (bảng ở 3.9), cái giá là ai đọc được bucket thì đọc được site. Để sau: tuỳ chọn mã hoá cả bản site.

**Chặn hai máy cùng ghi vào một tiền tố.** Ca chính của nó là lệnh dựng lại máy (máy mới và máy cũ cùng trỏ một tiền tố). Cắt lệnh đó thì ca đó biến mất; còn lại là một luật cho người dùng: mỗi máy một tiền tố, tiền tố mặc định chứa tên máy (`tpan/<tên máy>`). Hệ thống không chặn bạn nếu bạn cố tình đặt hai máy vào cùng một tiền tố.

### 5.2 Các quyết định kỹ thuật

| Chọn | Cách khác | Vì sao không chọn cách khác |
|---|---|---|
| Tự ký SigV4 bằng thư viện chuẩn (`crypto/hmac`, `crypto/sha256`, `net/http`) | `aws-sdk-go-v2`, `minio-go` | Số đo ở bản thử: `aws-sdk-go-v2` kéo 17 module, thêm 81 gói và tăng file chạy 2476 KiB; `minio-go` thêm 2304 KiB. TPAN chỉ cần mười lời gọi, phần ký khoảng 60 dòng. Mục 14 của KE-HOACH cấm thêm thư viện khi thư viện chuẩn làm được. Rủi ro của tự viết (chuẩn hoá đường dẫn, query) được chặn bằng ba lớp: vectơ mẫu của AWS, một máy chủ giả tính lại chữ ký, và hai máy chủ S3 thật khác nhau (versitygw, Garage) trong e2e. |
| `age`, chế độ mật khẩu (đối xứng) | `age` với cặp khoá công khai; tự viết khuôn mã hoá | Mã hoá khoá công khai chỉ giữ kín, không chứng thực người gửi: ai biết khoá công khai và ghi được vào bucket đều tạo được một gói "hợp lệ". Với khoá đối xứng, chỉ người có khoá tạo được gói giải ra được. Cái giá: máy chủ phải giữ khoá (file chỉ root đọc) để tự sao lưu; người quản trị giữ bản chép. Không tự viết khuôn mã hoá: `age` là khuôn công khai, có công cụ độc lập. |
| Lượt đẩy có khoá riêng, trong bộ đẩy nền | Đẩy là một bước trong job `backup.create`, giữ khoá job | Tải vài GB qua mạng chậm mất hàng giờ; trong lúc đó mọi thao tác khác bị chặn và health bỏ mọi vòng kiểm. Kho hỏng còn làm cả việc sao lưu hỏng (trái B1). |
| Máy chủ thử versitygw (chính) và Garage (thứ hai) | MinIO | Repo MinIO đã lưu trữ, đường tải bản chạy trả HTTP 410: không còn cài dễ. versitygw lưu mỗi object thành file thường nên "sửa hỏng một object trên kho" chỉ là ghi đè một byte. Garage thêm vào vì là bản cài đặt S3 khác hẳn (Rust, câu lỗi khác): client chạy với cả hai là bằng chứng "không gắn với một nhà cung cấp". |

### 5.3 Chỗ đã làm sai trong mốc này, và phát hiện bằng cách nào

Bản thiết kế đầu (commit f1f342a) qua một lượt soát tìm ra 34 điểm, 5 điểm nặng; một lượt soát hẹp sau đó tìm thêm một điểm nặng. Những điểm người review hay hỏi:

- **Vòng "đẩy, dọn, đẩy lại"** (S2): hai luật chọn đẩy và chọn dọn khác nhau, bản bị dọn khỏi kho mà còn file trên máy quay lại hàng chờ. Sửa bằng `remoteSurvives` (3.5). Soát phát hiện khi lần theo sơ đồ trạng thái một bản sao lưu.
- **Lỗi 5xx của kho bị tính là lỗi riêng của bản** (S6): kho sập một tối thì năm lượt là bỏ hẳn mọi bản. Sửa: phân biệt lỗi của đích và lỗi của bản; lỗi của đích có cầu dao và nhịp lùi (3.6).
- **Đổi endpoint mà giữ khoá cũ gửi chữ ký thật tới host lạ** (R1, mục 3.2): lỗi mới do chính bản sửa lỗi trước tạo ra, soát hẹp mới thấy.
- **Script khởi động đầu chạy lại sau `self-update` hạ file chạy về bản đã ghim** (S13): sửa bằng điều kiện `first-boot.json` (3.11).
- **Chạy lại cloud-init tạo lại các site đã xoá** (R14): sửa bằng "chỉ làm tiếp việc chưa đạt".
- **Gói panel luôn chậm hơn bản site một nhịp** (S3): gói chụp `tpan.db` trước khi các dòng vừa `stored` được ghi. Sửa: gói làm ở cuối lượt, sau mọi lần đẩy và dọn.
- **Câu lỗi do kho trả có thể chép lại access key** (S11): sửa bằng việc thay khoá bằng `••••` trước khi dựng `Error`.
- Trong quá trình làm, hai thợ viết dùng lệnh sửa file tại chỗ (`sed -i`, `perl -0pi`) trái luật 7 của AGENTS.md. Nội dung đã đọc lại, không có gì sai; lời giao việc từ đợt sau nhắc rõ luật.
- Phát hiện ngoài phạm vi, ghi lại để bạn quyết: **DB rời (M13) không có trong bản sao lưu nào**. M9 chỉ sao lưu site. Mất máy là mất DB rời.

## 6. Tự kiểm

Mười câu người review dễ hỏi. Đáp án ở cuối file. Câu 6 và câu 9 phải chạy lệnh trên máy ảo mới trả lời được.

1. Vì sao TPAN tự ký SigV4 thay vì dùng thư viện của AWS?
2. Kho trả ETag đúng và đúng cỡ. Sao TPAN vẫn tải lại cả object về?
3. Sao lưu site chạy lúc 03:00 đúng lúc kho S3 sập. Bản sao lưu trên máy có xong không? Panel có treo không?
4. Mất mạng giữa chừng lúc đang đẩy file 2 GiB lên thì chuyện gì xảy ra với phần đã gửi?
5. Vì sao đổi endpoint phải gõ lại hai khoá? Đổi riêng vùng thì sao?
6. Bạn sửa đúng 1 byte của object bản sao lưu trên đĩa của kho thử, rồi chạy `sudo tpan backup pull <id>` (sau khi xoá file trên máy). Chuyện gì xảy ra, và file nào còn trên máy?
7. Vì sao không mã hoá luôn bản sao lưu của site? Ai đọc được bucket thì đọc được gì?
8. User thường (khách của panel) làm được gì với kho?
9. Sau khi đặt đích, `sudo tpan backup target show --json` và `sudo tpan backup target set ... --dry-run --json` có in secret key không? Chạy lệnh để chứng minh.
10. Hai luật giữ bản (trên máy và trên kho) khác nhau thì sao không sinh vòng "đẩy, dọn, đẩy lại"?

### Đáp án

1. Số đo ở bản thử: SDK của AWS kéo 17 module và thêm 2476 KiB vào file chạy; `minio-go` thêm 2304 KiB. TPAN chỉ cần mười lời gọi, phần ký khoảng 60 dòng, và KE-HOACH cấm thêm thư viện khi thư viện chuẩn làm được. Rủi ro tự viết sai chữ ký được chặn bằng vectơ mẫu của AWS, máy chủ giả tính lại chữ ký, và e2e với hai máy chủ S3 thật khác nhau.
2. Bản thử đã ghi đè 1 byte của object trên đĩa kho: HEAD vẫn trả đúng cỡ và ETag cũ, GET trả dữ liệu hỏng. ETag của object nhiều phần lại là md5 của các md5, không phải băm nội dung (hai cỡ phần cho hai ETag khác nhau với cùng file). Chỉ có sha256 do chính mình tính trên dữ liệu đọc về mới chứng minh được object đúng. Đó là bất biến B2.
3. Có xong. `backup.create` và `backup.push` là hai job khác nhau; `backup.create` không có bước nào chạm mạng (B1). `tpan backup create` thoát 0 và in "đã sao lưu trên máy, CHƯA lên kho: ...; sẽ đẩy bù ở lượt sau". Panel không treo vì lượt đẩy không giữ khoá job của máy (B10). Bộ đẩy nền ghi `s3.target_error` và lùi 10 phút, rồi tăng dần; `tpan.s3:fresh` cảnh báo.
4. Mỗi request được thử lại tối đa 4 lần (nghỉ 0,5, 1, 2 giây). Vẫn hỏng thì client gọi `AbortMultipartUpload` để không để phần dở nằm lại kho; nếu tiến trình bị giết thì chưa kịp huỷ, và lượt đẩy sau dọn phần dở của chính key đó, đồng thời huỷ mọi lần tải dở cũ hơn một ngày. Bản thử đo được: dừng kho sau 1 giây thì 249 MB nằm lại, `AbortMultipartUpload` dọn về còn 24 KB. Bản chưa lên kho (`remote_tries` chưa tới 5, hoặc lỗi của đích nên không đếm) sẽ được đẩy lại ở lượt sau.
5. Câu hỏi có hai nửa. Đổi endpoint, vùng, bucket hay CA thì phải gửi lại cả hai khoá (R1): nếu không, request kiểm đầu tiên ký bằng khoá cũ sẽ đi tới host mới do người gọi chọn, và một phiên admin bị chiếm lấy được chữ ký. Kiểm xảy ra trước khi dựng client. Vùng nằm trong bốn trường đó, nên đổi riêng vùng cũng phải gõ lại khoá. Chỉ khi cả bốn trùng với đích đang lưu thì để trống khoá nghĩa là "giữ khoá đang lưu". Đổi tiền tố hay `keep` thì không phải gõ lại.
6. `S3Pull.Apply` tải vào `Dest.part`, tính sha256 lúc tải, thấy khác sha256 trong `tpan.db`, xoá `.part` và báo lỗi "bản trên kho không khớp sha256 ghi lúc tạo". Không file nào còn trên máy, job `backup.pull` hỏng, và vì khôi phục chỉ chạy sau khi tải xong nên site không bị đụng tới (B4). Lệnh thử (chưa chạy trong phiên viết file này): chép `scripts/e2e-m18/doi-byte.py` vào máy ảo bằng `scripts/vm.sh push`, rồi `scripts/vm.sh root bash -c 'f=$(find /var/lib/s3thu/tpan-thu -name "*.tar.gz" | head -1); python3 /tmp/doi-byte.py "$f" 100'`, xoá file local, rồi `sudo tpan backup pull <id>`.
7. Vì khung thiết kế chỉ đòi mã hoá gói panel, và để mất khoá sao lưu vẫn lấy lại được nội dung site (bảng ở 3.9). Cái giá: ai đọc được bucket đọc được mã nguồn, `wp-config.php` (mật khẩu DB của site) và bản xuất DB. Khuyên bucket riêng tư và mã hoá phía kho. Mã hoá cả bản site là việc để sau.
8. Không đặt, xem, thử, gỡ đích; không đẩy, tải hay liệt kê kho; không sinh user-data; không thấy chi tiết kho (endpoint, bucket, câu lỗi), kể cả trong Lịch sử, vì job đẩy, tải, gói panel thuộc `admin`. `GET /api/v1/backups` của họ chỉ thêm `local`, `remote`, `remote_at`; `remote_key` và `remote_error` bị xoá. Khôi phục bản của chính mình vẫn theo luật `owner:backup` của M13, nhưng bản chỉ còn trên kho thì phải nhờ admin tải về trước.
9. Không. Kiểm chứng (chưa chạy trong phiên viết file này; kỳ vọng ra `0` ở cả hai): `scripts/vm.sh root bash -c '. /root/s3thu.env; tpan backup target show --json | grep -c -F "$S3_SECRET_KEY"'`, rồi cùng lệnh đó với `printf "%s\n%s\n" "$S3_ACCESS_KEY" "$S3_SECRET_KEY" | tpan backup target set --endpoint "$S3_ENDPOINT" --region "$S3_REGION" --bucket "$S3_BUCKET" --keys-stdin --dry-run --json | grep -c -F "$S3_SECRET_KEY"`. Lý do: `SecretFile` có `Info().Files` rỗng nên không vào bản xem trước hay `managed_files`; secret key là `system.Secret` nên bị in là `••••`; `TargetView` không có trường nào cho secret key. e2e phần H quét mọi nơi (API, `job_events`, `audit`, `settings`).
10. Vì chỉ có một hàm, `remoteSurvives`, quyết định cả "bản nào cần đẩy" lẫn "bản nào cần dọn khỏi kho". Bản bị dọn là bản mà hàm này trả sai; bản đó cũng không qua điều kiện chọn đẩy, nên không bao giờ quay lại hàng chờ dù file vẫn còn trên máy (B14).

## 7. Giới hạn đã biết

Hai điểm soát hẹp để lại, ghi thẳng vào đây (mục 9.3 của bản thiết kế):

- **R6 (a).** Lượt đẩy dài (đang tải một bản lớn) thì khôi phục một bản chỉ còn trên kho phải chờ khoá lượt đẩy, tối đa 30 phút. API tải về chạy trong goroutine riêng nên không treo panel, nhưng lệnh gõ tay chờ.
- **R7.** Việc tải về (dưới khoá lượt đẩy) và việc sao lưu (dưới khoá job) có thể chạy song song, và cả hai cùng qua luật chặn đầy đĩa vì mỗi bên chỉ nhìn chỗ trống hiện tại. Giữ chỗ trước bằng `fallocate` cần đổi `Streamer` và hai bước đã xong; làm ở mốc sau.

Những giới hạn khác đã ghi ở mục 1 của bản thiết kế, bạn nên nhắc được: bản site không mã hoá trên kho; chưa có lệnh dựng lại cả máy; chưa chặn hai máy ghi cùng một tiền tố; DB rời của M13 không có bản sao lưu; chứng chỉ Let's Encrypt không được mang sang máy mới; kiểu địa chỉ `bucket.host` (`--vhost`) có unit test nhưng chưa thử e2e vì cần DNS kiểu `*.host`; một đích duy nhất cho cả máy, chưa có khoá tạm STS hay vai của máy ảo. Nâng cấp từ v0.2.0 an toàn (migration 1800 chỉ thêm cột có DEFAULT). Lùi về v0.2.0 sau khi nâng thì phải chép lại bản sao `tpan.db` cũ; nếu đã có bản chỉ còn trên kho thì `tpan backup pull` các bản cần giữ trước khi lùi.
