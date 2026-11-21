# M18: thiết kế phần lõi (sao lưu ra kho S3, khôi phục từ S3, cài bằng cloud-init, dựng lại máy)

Bản thiết kế của đợt 0, viết ngày 21/11/2026 trên nhánh `moc/M18` (tách từ `dev` ở 025edb6, bản sau v0.2.0). Đặc tả gốc: `agent-dev/TU-CHAY-M18.md`. Bản thử và output thật: `docs/bang-chung/M18-nghien-cuu/`. Cuối mốc file này gộp vào mục 6.16 của `docs/KE-HOACH.md`.

Từ dùng trong file: **kho** là một bucket S3 tương thích ở ngoài máy; **đích** là cấu hình trỏ tới kho (endpoint, vùng, bucket, tiền tố, hai khoá truy cập); **bản sao lưu site** là file `.tar.gz` của M9; **gói panel** là bản sao lưu của chính panel (`tpan.db` và vài file), đã mã hoá; **khoá sao lưu** là khoá mã hoá gói panel.

## 1. Vấn đề và phạm vi

Một VPS là một cái máy có thể mất bất cứ lúc nào. Hiện nay bản sao lưu của M9 nằm trên chính đĩa của máy, và máy mới phải SSH vào gõ lệnh cài. M18 làm hai việc rồi ghép lại: bản sao lưu rời khỏi máy, và máy mới tự thành máy chạy được; ghép lại thành "máy cũ mất, tạo máy mới, mọi site trở lại từ kho".

**Làm (phần lõi, không sửa gì trong `web/`):**

1. Một đích S3 tương thích cho cả máy: đặt, thử, xem, gỡ.
2. Sau mỗi bản sao lưu đầy đủ của site: đẩy lên kho, đọc lại để kiểm sha256, rồi mới coi là "đã lên kho". Kho hỏng thì bản cục bộ vẫn xong, panel cảnh báo, lần sau đẩy bù.
3. Chính sách giữ bản trên kho tách với chính sách giữ bản trên máy. Bản đã lên kho mà bị dọn trên máy vẫn còn dòng trong bảng `backups` ("chỉ còn trên kho").
4. Khôi phục từ kho: tải về, kiểm sha256, rồi đi đúng luồng khôi phục của M9.
5. Gói panel: `tpan.db` và `config.toml`, mã hoá bằng khoá sao lưu, đẩy lên kho cùng nhịp với lịch backup.
6. `tpan cloud-init render`: sinh file user-data để tạo máy mới có panel, stack và các site trong `sites.yaml`; hoặc máy mới sẵn sàng dựng lại từ kho. Không có bí mật dạng rõ trong user-data.
7. `tpan machine restore --from-s3`: trên máy trắng đã cài TPAN, kéo gói panel và các bản sao lưu site về, dựng lại stack, từng site, rồi báo cáo.
8. CLI, API (chỉ admin, trừ khôi phục đã có luật `owner:backup` từ M13), hai phép kiểm health, migration 1800.

**Không làm, và vì sao:**

- Giao diện (chờ M16 vào `dev`).
- Mã hoá bản sao lưu của site trên kho. Khung thiết kế chỉ đòi mã hoá gói panel; bản sao lưu site để nguyên `.tar.gz` nên mất khoá sao lưu thì nội dung site vẫn lấy lại được bằng tay. Hệ quả phải nói rõ ở tài liệu: ai đọc được bucket thì đọc được mã nguồn và bản xuất DB của site. Để sau: tuỳ chọn mã hoá cả bản của site.
- Nhiều đích cùng lúc; quy tắc vòng đời phía kho; khoá tạm (STS), vai của máy ảo (instance role); gọi API của nhà cung cấp để tạo máy.
- Sao lưu DB rời (M13). M9 chỉ sao lưu site, nên DB rời không có gì trên kho; dựng lại máy báo rõ các DB rời bị mất. Đây là lỗ có từ trước M18, ghi lại để chủ dự án quyết.
- Mang chứng chỉ Let's Encrypt sang máy mới. Site có SSL trở lại ở http trước, chứng chỉ xin lại sau bằng một lệnh (mục 3.10, lý do ở mục 2.6).
- Công cụ MCP mới. `list_backups` có thêm trường `location`, không có công cụ nào chạm tới đích hay khoá.

## 2. Nghiên cứu

Mọi con số dưới đây đo trên máy ảo `tpan-m18` (Ubuntu 24.04.5, aarch64, 2 CPU, 4 GB RAM, cloud-init 26.1) hoặc trên Mac của chủ dự án, ngày 20/11/2026 giờ UTC (rạng sáng 21/11 giờ Việt Nam). Script và output ở `docs/bang-chung/M18-nghien-cuu/`.

### 2.1 Thư viện S3 hay tự ký SigV4 (`02-thu-vien-s3.sh`, `10-nguon-tra.out`)

| Cách | Module ngoài kéo vào | Gói biên dịch thêm | File chạy linux/amd64 tăng | Giấy phép, bảo trì (tra 20/11/2026) |
|---|---|---|---|---|
| Tự ký bằng thư viện chuẩn | 0 | 0 | 0 (so với chương trình nền đã có `net/http`) | không có gì để theo dõi |
| `aws-sdk-go-v2` (service/s3 v1.116.0) | 17 | +81 | +2,48 MiB | Apache-2.0, còn sống (push 19/11/2026) |
| `minio-go` v7.4.1 | 230 dòng trong đồ thị module (go.mod của nó khai cả công cụ lint) | +64 | +2,30 MiB | Apache-2.0, còn sống (push 12/11/2026); máy chủ MinIO thì đã đóng |

**Chọn: tự ký SigV4 bằng thư viện chuẩn** (`crypto/hmac`, `crypto/sha256`, `net/http`, `encoding/xml`). Lý do: TPAN chỉ cần tám lời gọi (PutObject, GetObject, HeadObject, DeleteObject, ListObjectsV2, bốn lời gọi của tải nhiều phần, ListMultipartUploads); SigV4 là khoảng 60 dòng; mục 14 của KE-HOACH cấm thêm thư viện khi thư viện chuẩn làm được. Rủi ro của việc tự viết (chuẩn hoá đường dẫn và query sai) được chặn bằng ba lớp kiểm: vectơ mẫu công khai của AWS, một máy chủ giả kiểm lại chữ ký trong unit test, và hai máy chủ S3 thật khác nhau trong e2e.

Bản thử `ban-thu-s3/main.go` (510 dòng, chỉ thư viện chuẩn) đã chạy được với cả hai máy chủ thử ngay lần đầu, kể cả tên object có dấu cách, dấu tiếng Việt, ngoặc và `&` (`05-thu-co-ban-*.out`). Nó là mã tham chiếu cho gói `internal/s3`.

### 2.2 Máy chủ S3 để thử (`01-may-chu-s3-tra.out`)

| Ứng viên | Tình trạng 20/11/2026 | Kết luận |
|---|---|---|
| MinIO | repo `minio/minio` đã lưu trữ (archived, push cuối 24/04/2026); bản phát hành cuối 15/10/2025 không có file đính kèm; `dl.min.io/.../linux-arm64/minio` trả HTTP 410 | không còn cài được dễ dàng, loại |
| **versitygw** v1.11.0 (06/11/2026) | Apache-2.0, có `.deb` cho arm64 và amd64 kèm `checksums.txt`, một file chạy, có unit systemd mẫu | **máy chủ thử chính** |
| **Garage** v2.5.0 (24/10/2026) | AGPL-3.0, một file chạy tĩnh cho aarch64 | **máy chủ thử thứ hai** (chỉ chạy bộ thử liên thông) |
| SeaweedFS 4.54, RustFS 1.0.6, rclone v1.76 `serve s3` | còn sống, có bản arm64 | không cần tới; rclone trong apt của Ubuntu 24.04 là 1.60.1, chưa có `serve s3` |

Vì sao versitygw là chính: backend `posix` lưu mỗi object thành một file thường (`<gốc>/<bucket>/<key>`), nên e2e "sửa hỏng một object trên kho" chỉ là ghi đè một byte của file đó; nó kiểm chữ ký thật (sai khoá: 403 `SignatureDoesNotMatch`), kiểm cả vùng trong chữ ký (sai vùng: 400 `AuthorizationHeaderMalformed`), hỗ trợ TLS (`VGW_CERT`, `VGW_KEY`) để thử chứng chỉ gốc riêng. Vì sao thêm Garage: nó là một bản cài đặt S3 khác hẳn (Rust, lưu thành khối có băm, vùng mặc định `garage`, câu lỗi khác: sai khoá ra 403 `AccessDenied`), nên client chạy được với cả hai là bằng chứng cho "không gắn với một nhà cung cấp". Hệ quả cho thiết kế: phân loại lỗi theo **mã HTTP** trước, tên lỗi sau.

Giấy phép AGPL của Garage không chạm tới TPAN: nó chỉ được tải về máy ảo lúc chạy e2e.

### 2.3 File 2 GiB: RAM, thời gian, đứt giữa chừng (`07-file-2gb.out`)

Chạy trên vòng lặp nội bộ của máy ảo (không qua mạng thật), file 2 GiB ngẫu nhiên:

| Việc | versitygw | Garage |
|---|---|---|
| Tải lên, phần 8 MiB (256 phần) | 12,4 giây, đỉnh RAM 24,0 MB | 12,6 giây, 23,7 MB |
| Tải lên, phần 16 MiB (128 phần) | 12,3 giây, 30,5 MB | 12,9 giây, 28,9 MB |
| Tải về theo dòng, tính sha256 | 3,8 đến 5,5 giây, 6,2 đến 6,5 MB | 3,6 giây, 6,3 MB |

- sha256 tải về khớp bản gốc, `cmp` hai file giống hệt, ở cả bốn lần.
- ETag của object nhiều phần là `md5 của các md5`-`số phần`: cùng một file mà hai cỡ phần cho hai ETag khác nhau (`7adb…-256` và `ad69…-128`). ETag không phải là băm của nội dung.
- **Dừng máy chủ sau 1 giây tải lên**: client thử lại 4 lần rồi thoát lỗi; lệnh huỷ cũng lỗi (máy chủ đã tắt). Bật lại: `ListMultipartUploads` thấy 1 lần tải dở, 249 MB nằm lại trong kho; `AbortMultipartUpload` dọn sạch (về 24 KB).
- **Kho trả 503 từ phần thứ 5** (proxy gây lỗi): client thử lại 4 lần rồi tự huỷ; sau đó không còn lần tải dở nào.
- **Ghi đè 1 byte của object trên đĩa của kho**: HEAD vẫn trả đúng cỡ và đúng ETag cũ; GET trả dữ liệu hỏng; sha256 do ta tính thì khác bản gốc. Đây là bằng chứng cho luật "không tin ETag".

Kết luận: cỡ phần mặc định **8 MiB** (RAM đỉnh khoảng 24 MB cho mọi cỡ file tới 72 GiB); file lớn hơn thì cỡ phần tăng để không quá 9000 phần. Chỉ giữ một phần trong bộ nhớ, tải tuần tự.

Ghi thêm: chép file chạy 5,7 MB vào máy ảo qua stdin của `multipass exec` bị cắt còn 2,5 MB mà không báo lỗi; `scripts/vm.sh` vì vậy có thêm lệnh con `push`, `pull` dùng `multipass transfer`.

### 2.4 Mã hoá gói panel (`11-age-thu.out`)

Thư viện `filippo.io/age` v1.3.2 (BSD-3-Clause, push 29/08/2026): file chạy tăng 356 KB, kéo thêm module `filippo.io/hpke` (`golang.org/x/crypto` đã có sẵn vì argon2). Mã hoá theo dòng 300 MiB: 0,39 giây, đỉnh RAM 4,4 MB. File do thư viện ghi **giải mã được bằng lệnh `age` 1.1.1 của apt Ubuntu 24.04**, sha256 khớp. Sửa một byte của bản mã: giải mã dừng với "failed to decrypt and authenticate payload chunk". Sai khoá: "identity did not match".

**Chọn `age`, chế độ khoá đối xứng (mật khẩu scrypt), không phải cặp khoá X25519.** Lý do nằm ở mô hình đe doạ: mã hoá khoá công khai chỉ giữ kín, không chứng thực người gửi; ai biết khoá công khai và ghi được vào bucket đều tạo được một gói panel "hợp lệ" (một `tpan.db` giả với mật khẩu quản trị và danh sách bản sao lưu do họ chọn), và lệnh dựng lại máy sẽ tin nó. Với khoá đối xứng, chỉ người có khoá mới tạo được bản mã giải ra được, nên gói panel vừa kín vừa không giả được. Cái giá: máy chủ phải giữ khoá (file chỉ root đọc) để tự sao lưu hằng đêm; người quản trị giữ một bản chép. Bản thử chạy với khoá X25519; chế độ scrypt dùng cùng khuôn file và cùng lớp mã hoá theo dòng, đợt 1 thử lại việc giải mã bằng lệnh `age` với đúng chế độ này (mục 7).

Vì sao không tự viết: tự nghĩ ra một khuôn file mã hoá theo dòng là chỗ dễ sai nhất của cả mốc; `age` là khuôn công khai, có công cụ độc lập, nên **mở được gói panel mà không cần TPAN**.

### 2.5 cloud-init (`08-cloud-init-thu.out`, bước 1 của `09-dung-lai-may-thu.out`, `10-nguon-tra.out`)

| Câu hỏi | Trả lời (đã thử trên máy ảo phụ `tpan-m18-ci`, trừ dòng ghi "nguồn") |
|---|---|
| Chạy bằng quyền gì | `uid=0(root)`, thư mục `/`, umask 0022, `PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/snap/bin`, **không có `HOME`** |
| Lỗi thấy ở đâu | output của `runcmd` vào `/var/log/cloud-init-output.log` (root:adm 0640) và console. Một lệnh lỗi ở **giữa** `runcmd`: các lệnh sau vẫn chạy, `cloud-init status` vẫn `done`, `errors: []`. Lệnh **cuối** lỗi: `status: error`, mã thoát 1, `/run/cloud-init/result.json` ghi `scripts_user ... 1 failures`. `multipass launch` trả 0 trong cả hai ca |
| Chạy lại | `runcmd` chạy một lần cho mỗi máy (khởi động lại không chạy nữa). Chạy lại bằng tay: `cloud-init single --name scripts_user --frequency always` chạy lại đúng script đó |
| Ai đọc được user-data trên máy | `/var/lib/cloud/instance/user-data.txt` là root 0600; user thường gọi `cloud-init query userdata` nhận `<redacted for non-root user>`. Nhưng trên cloud có dịch vụ metadata (169.254.169.254) thì mọi tiến trình hỏi được; Multipass không có dịch vụ đó (nguồn dữ liệu NoCloud, đĩa `cidata`) nên không thử được. Thiết kế coi user-data là **ai trên máy cũng đọc được, mãi mãi** |
| Dài tối đa | Multipass nhận user-data 308 KB (đã thử). Giới hạn do nhà cung cấp đặt (nguồn, tra 20/11/2026): AWS EC2 16 KB trước khi mã base64; OpenStack 65535 byte sau khi mã; Azure 64 KB; GCP 256 KB mỗi giá trị. DigitalOcean và Hetzner: chưa tra được con số từ trang của họ. cloud-init tự giải nén user-data dạng gzip và nhận `encoding: gz+b64` cho từng file |

Hệ quả cho thiết kế: user-data chỉ có **một** lệnh `runcmd` (script khởi động đầu), để mã thoát của nó là mã thoát của cả lượt; mặc định từ chối sinh user-data lớn hơn 16 KB; TPAN tự ghi log và file trạng thái riêng, không dựa vào `cloud-init status`.

### 2.6 Dựng lại máy (`09-dung-lai-may-thu.out`)

Làm bằng tay với bản TPAN hiện có (chưa có dòng mã nào của M18) trên một máy ảo mới: cài TPAN, `tpan setup`, thay `tpan.db` bằng bản sao của máy cũ, `tpan stack install`, với mỗi site `tpan site add` (chạy lại trên dòng đã có) rồi chép file backup về đúng đường dẫn và `tpan backup restore`.

- Kết quả: hai site trả 200 với đúng bài viết và file đánh dấu của máy cũ; `tpan health` đạt, kể cả các phép kiểm lệch (file sinh lại có đúng sha256 đã ghi trong `tpan.db` cũ).
- Thời gian: từ lúc tạo máy tới lúc cả hai site trả 200 là **1 phút 50 giây** (tạo máy 33 giây, cài stack 39 giây, tạo lại hai site 15 và 5 giây, khôi phục 8 và 7 giây).
- **Thứ tự đúng**: TPAN → `tpan.db` cũ → stack (web server và nguồn gói đọc từ `tpan.db` cũ, nên không thể lệch với máy cũ) → bản PHP các site cần → từng site: user, thư mục, DB, pool, vhost (Plan tạo site) → mã nguồn và DB từ bản sao lưu (Plan khôi phục).
- **Chỗ phụ thuộc tên máy và IP cũ**: chứng chỉ tự ký của panel (sinh lại trên máy mới, dấu vân tay mới); bản ghi DNS của từng site (phép kiểm DNS báo cho tới khi trỏ sang IP mới); tiền tố của đích trên kho (là cấu hình ghi rõ, không suy từ tên máy lúc chạy). Không chỗ nào khác: user, DB, pool, socket đều suy từ domain.
- **Chứng chỉ**: không đem theo ở bản này. Đem theo đòi một đường riêng cho Plan SSL (bỏ điều kiện DNS khi chứng chỉ đã có, dựng site mặc định 443, bật mod ssl của Apache), không thử được trong hạn vì e2e SSL cần một máy chủ ACME thử trên cả hai máy. Thay vào đó site có SSL được dựng lại ở http với URL WordPress đổi về http (không thì WordPress chuyển khách sang https mà cổng 443 chưa có gì), và báo cáo cuối liệt kê lệnh `tpan ssl issue` cho từng site sau khi DNS đã trỏ về máy mới. Để sau: đem `/etc/letsencrypt` theo trong gói panel.

**Quyết định: làm phần dựng lại máy**, vì chuỗi Plan sẵn có đã ghép được trên máy trắng mà không cần sửa Plan nào. Nó nằm ở đợt viết mã cuối; nếu tới mốc giờ kiểm ở mục 8 mà sáu phần trước chưa xanh thì cắt và ghi rõ.

## 3. Thiết kế

### 3.1 Bố trí mã

| Gói (mới trừ khi ghi "sửa") | Việc |
|---|---|
| `internal/s3` | Client S3 tự ký: `Config`, `Client`, lỗi có kiểu, kho giả trong bộ nhớ `Mem` cho test |
| `internal/agebox` | Bọc `filippo.io/age`: sinh và kiểm khoá sao lưu, mã hoá và giải mã theo dòng, bọc một mẩu nhỏ dạng chữ |
| `internal/cloudinit` | Sinh user-data từ tham số đã kiểm; không đọc ghi gì |
| `internal/system` (sửa) | Thêm đọc ghi file theo dòng: `OpenRead`, `CreateFile` |
| `internal/engine` (thêm file) | `engine.Note`: bước không chạy lệnh ghi được một dòng tiến độ vào log job |
| `internal/steps` (thêm file `s3.go`, `panel.go`) | Các Step mới (mục 3.5 đến 3.8) |
| `internal/core` (thêm `remote.go`, `panel.go`, `cloudinit.go`, `machine.go`; sửa `backup.go`) | Ca sử dụng |
| `internal/store` (thêm `remote.go`, migration `1800_s3.sql`; sửa `backup.go`) | Cột mới của `backups` và truy vấn |
| `internal/checks` (thêm `m18.go`), `internal/health` (sửa `collect.go`) | Hai phép kiểm |
| `internal/server` (thêm `types_m18.go`, `api_m18.go`, `pusher.go`; sửa `routes.go`, `api.go`) | Route, bộ đẩy nền |
| `cmd/tpan` (thêm `backup_s3.go`, `cloudinit.go`, `machine.go`; sửa một dòng `backup.go`) | CLI |

Thư viện mới: `filippo.io/age` (mục 2.4) và `golang.org/x/term` (hỏi khoá bí mật trên terminal mà không hiện chữ; thư viện chuẩn không tắt được echo). Cả hai ghi vào bảng mục 3 của KE-HOACH.

**Đọc ghi theo dòng trong `System`.** M9 phải dùng GNU tar vì `System` chỉ có `ReadFile`/`WriteFileAtomic` giữ cả file trong bộ nhớ. Tải file vài GB lên kho và ghi file tải về cần luồng:

```go
// Streamer là phần đọc ghi theo dòng của System (M18).
type Streamer interface {
    // OpenRead mở một file thường để đọc tuần tự và trả cỡ của nó lúc mở.
    // Thành phần cuối của path là symlink, hoặc không phải file thường: lỗi
    // (LocalSystem mở bằng O_NOFOLLOW rồi fstat). Không có: fs.ErrNotExist.
    OpenRead(path string) (r io.ReadCloser, size int64, err error)
    // CreateFile tạo file MỚI (O_EXCL, O_NOFOLLOW; đã có thì lỗi thoả
    // fs.ErrExist) với perm và owner, để ghi tuần tự. Close đồng bộ xuống đĩa
    // (fsync) rồi mới đóng. Thư mục cha phải có sẵn.
    CreateFile(path string, perm fs.FileMode, owner Owner) (io.WriteCloser, error)
}
```

`System` nhúng `Streamer`; `LocalSystem` và `FakeSystem` cài đặt nó (hai lớp bọc `recSys`, `jobLockSystem` nhúng `System` nên tự có). `FakeSystem.CreateFile` ghi vào hệ thống file giả lúc `Close` và tôn trọng `Fail("write", path, err)`.

**`engine.Note(sys, line)`** ghi `line` thành một sự kiện output của bước đang chạy khi `sys` là lớp bọc engine đưa cho Step; ngoài job thì không làm gì. Luật: không bao giờ đưa bí mật vào `line` (dòng này vào `job_events`, SSE và MCP nguyên văn).

### 3.2 Mô hình dữ liệu

Migration `1800_s3.sql` (khoảng số 1800 đến 1899 là của M18):

```sql
-- Bản sao lưu nằm ở đâu. local: file ở cột path còn trên máy. remote: trạng
-- thái trên kho ngoài máy: '' chưa lên (hoặc đã dọn khỏi kho), 'stored' đã lên
-- và đã đọc lại khớp sha256, 'failed' lần đẩy gần nhất lỗi.
ALTER TABLE backups ADD COLUMN local         INTEGER NOT NULL DEFAULT 1;
ALTER TABLE backups ADD COLUMN remote        TEXT    NOT NULL DEFAULT '';
ALTER TABLE backups ADD COLUMN remote_key    TEXT    NOT NULL DEFAULT '';  -- khoá object, tính từ sau tiền tố của đích
ALTER TABLE backups ADD COLUMN remote_target TEXT    NOT NULL DEFAULT '';  -- mã của đích lúc đẩy (mục 3.3)
ALTER TABLE backups ADD COLUMN remote_at     TEXT    NOT NULL DEFAULT '';  -- RFC 3339, lúc đọc lại khớp sha256
ALTER TABLE backups ADD COLUMN remote_error  TEXT    NOT NULL DEFAULT '';  -- lỗi lần đẩy gần nhất (một dòng, không có bí mật)
ALTER TABLE backups ADD COLUMN remote_tries  INTEGER NOT NULL DEFAULT 0;   -- số lần đẩy lỗi liên tiếp
CREATE INDEX backups_remote ON backups (remote, created_at);
```

Mọi cột có DEFAULT nên dòng của v0.2.0 tự thành "trên máy, chưa lên kho", và bản TPAN này đọc được DB cũ mà không cần bước chuyển dữ liệu nào.

`site.Backup` thêm: `Local bool`, `Remote BackupRemote` (`""`, `"stored"`, `"failed"`), `RemoteKey`, `RemoteTarget`, `RemoteAt time.Time`, `RemoteError string`, `RemoteTries int`. JSON: `local`, `remote`, `remote_key`, `remote_at`, `remote_error`; `remote_target` và `remote_tries` không ra JSON (`json:"-"`).

Không có bảng cho gói panel: trạng thái của nó là file trên máy, object trên kho, và một setting ghi lần đẩy thành công gần nhất (mục 3.8).

Khoá mới trong bảng `settings` (không khoá nào là bí mật):

| Khoá | Giá trị |
|---|---|
| `s3.endpoint` | `https://host[:cổng]`; rỗng nghĩa là chưa đặt đích |
| `s3.region`, `s3.bucket`, `s3.prefix` | vùng, bucket, tiền tố |
| `s3.path_style` | `1` (mặc định) hoặc `0` (kiểu `bucket.host`) |
| `s3.keep` | số bản giữ trên kho cho mỗi site, 1 đến 365, mặc định 7 |
| `s3.panel_last` | `<tên gói>\|<RFC 3339>\|<sha256>` của gói panel mới nhất đã lên kho và đã đọc lại khớp |
| `s3.panel_error` | lỗi lần tạo hoặc đẩy gói panel gần nhất; rỗng khi lần gần nhất đạt |

File ngoài `tpan.db`, đều `root:root 0600` trong `/etc/tpan` (thư mục 0700 từ M3):

| File | Nội dung |
|---|---|
| `/etc/tpan/s3.json` | `{"access_key":"…","secret_key":"…"}` |
| `/etc/tpan/s3-ca.pem` | chứng chỉ gốc riêng của kho (tuỳ chọn) |
| `/etc/tpan/backup.key` | khoá sao lưu, một dòng |

Ba file này **không** nằm trong `managed_files` (bước ghi chúng không khai `Files` trong `Info`, như khoá riêng TLS của panel ở M3): nội dung và sha256 của bí mật không được vào bản xem trước hay vào DB.

### 3.3 Đích và bí mật

**Kiểm đầu vào** (hàm ở `internal/s3`, mỗi cái một mã từ chối riêng):

| Trường | Luật |
|---|---|
| endpoint | URL chỉ có scheme, host, cổng tuỳ chọn; không user, không đường dẫn (trừ `/` trống), không query, không fragment, không ký tự điều khiển. Scheme `https`; `http` chỉ khi host là loopback (`127.0.0.0/8`, `::1`, `localhost`). Chuẩn hoá: bỏ `/` cuối, host chữ thường |
| region | `^[a-z0-9][a-z0-9-]{0,62}$`, mặc định `us-east-1` |
| bucket | `^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$` |
| prefix | 1 đến 200 ký tự, `^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$`, không đoạn nào là `.` hay `..`; mặc định `tpan/<tên máy>` với tên máy đã thay ký tự lạ bằng `-` |
| access key | `^[A-Za-z0-9/+=._-]{3,128}$` |
| secret key | 8 đến 256 ký tự ASCII in được, không khoảng trắng |
| keep | 1 đến 365 |
| CA | PEM có ít nhất một chứng chỉ đọc được, tối đa 64 KB |

Vì sao `http` chỉ cho loopback: bản sao lưu có `wp-config.php` và bản xuất DB; SigV4 không gửi khoá bí mật đi nhưng thân request thì đi dạng rõ. Kho nội bộ dùng chứng chỉ tự ký thì đưa chứng chỉ gốc qua `--ca-file`.

**Mã của đích** (`TargetID`): 16 ký tự hex đầu của `sha256(endpoint + "\n" + bucket + "\n" + prefix)`. Dòng `backups.remote_target` ghi mã này, nên đổi đích thì TPAN biết bản nào nằm ở đích cũ: bản đó được coi là chưa lên đích mới (còn file trên máy thì sẽ được đẩy), và không tải về được cho tới khi đặt lại đích cũ. Plan đặt đích hiện số bản như vậy trong tiêu đề bước, không từ chối.

**Plan `backup.target.set`** (admin; CLI và API):

1. Điều kiện đầu (chỉ đọc, chạy cả lúc xem trước): kiểm mọi trường; dựng client; `Check` (ListObjectsV2 một kết quả dưới tiền tố). Lỗi thành lời từ chối `s3_unreachable`, `s3_auth_failed`, `s3_bucket_not_found`, `s3_error` kèm mã HTTP và tên lỗi kho trả.
2. Bước `s3.probe` (`steps.S3Probe`): ghi một object nhỏ ngẫu nhiên `probe/<16 hex>`, HEAD, đọc lại và so từng byte, xoá, HEAD lần nữa phải ra "không có". Hỏng ở đâu thì cố xoá object rồi báo lỗi. Bước này không hoàn tác (nó tự dọn).
3. Bước `secret.file:/etc/tpan/s3.json` (`steps.SecretFile`): ghi hai khoá. Hoàn tác: trả nội dung cũ hoặc xoá nếu trước đó chưa có.
4. Bước `secret.file:/etc/tpan/s3-ca.pem` khi có CA; `steps.Absent` file đó khi đích mới không dùng CA.
5. Khi chưa có `/etc/tpan/backup.key`: bước `secret.file:/etc/tpan/backup.key` với một khoá sao lưu mới sinh (mục 3.8). Khoá hiện **đúng một lần** trong kết quả lệnh (CLI in ra; API trả qua cơ chế bí mật một lần của job như mật khẩu WordPress).
6. Job thành công (`after`): ghi các setting `s3.*`. Ghi setting lỗi thì báo lỗi kèm câu "chạy lại lệnh"; chạy lại là an toàn vì mọi bước đều so nội dung trước khi ghi.

Thứ tự "thử thật rồi mới ghi" bảo đảm không bao giờ lưu một đích chưa ghi, đọc, xoá được.

**Plan `backup.target.rm`**: tiêu đề bước nêu số bản chỉ còn trên kho (sẽ không tải về được cho tới khi đặt lại đích); `steps.Absent` cho `s3.json` và `s3-ca.pem`; `after` xoá các setting `s3.endpoint`, `s3.region`, `s3.bucket`, `s3.prefix`, `s3.path_style`, `s3.keep`. Không xoá gì trên kho, không đổi cột `remote*` của dòng nào (đặt lại đúng đích cũ thì các bản đó dùng lại được), không xoá khoá sao lưu.

**Plan `backup.target.test`**: một bước `s3.probe` trên đích đang lưu. Là job để có lịch sử và bằng chứng.

**Xem** (`Remote.Target`): mọi trường không bí mật, `access_key_tail` là bốn ký tự cuối của access key (đọc từ file lúc hỏi, không lưu ở đâu), `has_ca`, `key_fingerprint` (8 hex đầu của sha256 khoá sao lưu, rỗng khi chưa có), `panel_last`. Không trường nào mang khoá bí mật, kể cả dạng che.

### 3.4 Client S3 (`internal/s3`)

```go
type Config struct {
    Endpoint, Region, Bucket, Prefix string
    PathStyle bool
    CAPEM     []byte
    AccessKey string
    SecretKey system.Secret
    PartSize  int64            // 0: 8 MiB; test đặt nhỏ
    Now       func() time.Time // nil: time.Now
}
func New(cfg Config) (*Client, error)
type Object struct { Key string; Size int64; Modified time.Time; ETag string }

func (c *Client) Check(ctx context.Context) error                                   // chỉ đọc
func (c *Client) Put(ctx context.Context, key string, r io.Reader, size int64, progress func(done int64)) (sha256Hex string, err error)
func (c *Client) Get(ctx context.Context, key string, w io.Writer) (n int64, sha256Hex string, err error)
func (c *Client) Head(ctx context.Context, key string) (Object, error)              // không có: ErrNotFound
func (c *Client) Delete(ctx context.Context, key string) error                      // không có cũng là xong
func (c *Client) List(ctx context.Context, prefix string, each func(Object) error) error
func (c *Client) AbortUploads(ctx context.Context, prefix string) (int, error)      // dọn các lần tải nhiều phần còn dở
```

- **Mọi `key` và `prefix` người gọi đưa là tương đối với `Config.Prefix`**; client tự ghép `Prefix + "/" + key`, và `List` trả key đã bỏ tiền tố. Không hàm nào ra được ngoài tiền tố của đích. `key` phải khớp `^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$`, không thì lỗi trước khi gọi mạng.
- **Chữ ký**: SigV4 trong header `Authorization`. Ký `host`, `x-amz-date`, `x-amz-content-sha256` và mọi header `x-amz-*` khác. Đường dẫn mã hoá từng đoạn một lần theo RFC 3986 (chỉ giữ `A-Za-z0-9-_.~`); đường đi trên dây đặt bằng `URL.RawPath` đúng bằng đường đã ký. Query sắp theo tên rồi giá trị, mã hoá cả hai, và `URL.RawQuery` đặt đúng bằng chuỗi đã ký.
- **Thân có ký**: `x-amz-content-sha256` luôn là sha256 thật của thân (không dùng `UNSIGNED-PAYLOAD`), nên kho tự từ chối một phần bị hỏng trên đường đi. Mỗi phần nằm trọn trong bộ nhớ nên tính băm trước khi gửi và gửi lại được.
- **Tải nhiều phần**: `size` ≤ cỡ phần thì một PUT. Lớn hơn: `POST ?uploads`, từng phần `PUT ?partNumber=N&uploadId=…`, `POST ?uploadId=…` kèm danh sách ETag. Câu trả lời 200 của bước ghép vẫn phải đọc thân: thân là `<Error>` thì coi là lỗi. Cỡ phần: 8 MiB; khi `size > 8 MiB × 9000` thì `ceil(size / 9000)` làm tròn lên MiB. Lỗi ở bất kỳ đâu sau khi mở: gọi `AbortMultipartUpload` bằng một context riêng có hạn 30 giây (context của người gọi có thể đã huỷ), lỗi huỷ được gộp vào lỗi trả về.
- **Thử lại**: mỗi request tối đa 4 lần, nghỉ 0,5 giây, 1 giây, 2 giây; chỉ với lỗi mạng, mã 5xx và 429. Mã 4xx khác không thử lại.
- **Hạn giờ**: nối 15 giây, bắt tay TLS 15 giây, chờ header trả lời 60 giây; trong lúc truyền thân, 120 giây không có byte nào đi hay về thì huỷ lần thử đó. Không có hạn cho cả thao tác (file lớn, mạng chậm); người dùng huỷ job thì context huỷ.
- **Không đi theo chuyển hướng** (`CheckRedirect` trả `http.ErrUseLastResponse`): chữ ký gắn với host đã ký; mã 3xx thành lỗi nói "sai endpoint hoặc sai vùng" kèm header `x-amz-bucket-region` nếu có.
- **TLS**: tối thiểu TLS 1.2; `CAPEM` khác rỗng thì chỉ tin các chứng chỉ gốc đó. Không có tuỳ chọn bỏ kiểm chứng chỉ.
- **Lỗi**: `*s3.Error{Op, Status, Code, Message}`; `Message` đã bỏ ký tự điều khiển và cắt còn 300 ký tự; thân không phải XML thì `Code` là tên mã HTTP. `Classify(err)` trả một trong `s3_unreachable` (lỗi mạng, DNS, TLS, hết giờ), `s3_auth_failed` (401, 403), `s3_bucket_not_found` (404 khi thao tác trên bucket hoặc `NoSuchBucket`), `s3_error` (còn lại). Phân loại theo mã HTTP trước (mục 2.2).
- **Bí mật**: `SecretKey` là `system.Secret`; không lỗi, không dòng log nào chứa header `Authorization`, khoá bí mật hay access key. URL trong lỗi của `net/http` chỉ có host, đường dẫn và query, không có khoá (ta không ký bằng query).
- **`Mem`**: kho trong bộ nhớ có đúng bộ phương thức trên, thêm `Fail(op, key, err)` và `Corrupt(key)` cho test của steps, core, server.

Giao diện phía dùng (`steps.ObjectStore`) gồm đúng bảy phương thức trên; `*s3.Client` và `*s3.Mem` thoả nó.

### 3.5 Đẩy lên kho

**Luật chọn bản cần đẩy**, một luật cho cả đẩy tự động và `tpan backup push`:

> `kind = 'full'` và `local = 1` và không phải (`remote = 'stored'` và `remote_target` = mã của đích đang đặt).

Đẩy tự động bỏ qua dòng có `remote_tries ≥ 5`; `tpan backup push <id>` thì luôn thử và đặt lại số đếm. `remote_tries` chỉ tăng khi lỗi là **của riêng bản đó** (file cục bộ hỏng, kho trả dữ liệu khác thứ đã gửi, kho từ chối đúng object đó); lỗi **của đích** (`s3_unreachable`, `s3_auth_failed`, `s3_bucket_not_found`) ghi `remote = 'failed'` kèm lỗi nhưng không tăng số đếm, nên kho sập bao lâu thì khi sống lại mọi bản vẫn được đẩy bù. Bản xuất DB riêng (`kind = 'sql'`) không lên kho: nó là thao tác của màn Cơ sở dữ liệu, và bản đầy đủ đã chứa DB.

**Plan `backup.push`** (target: domain), một bước `steps.S3Push`:

1. `Stat` file cục bộ: file thường, cỡ bằng `backups.size`. Khác: lỗi "file cục bộ đã đổi".
2. `sha256sum` file cục bộ (lệnh, nên có bằng chứng trong log job) phải bằng `backups.sha256`. Khác: lỗi "bản cục bộ đã hỏng", không gửi byte nào.
3. `AbortUploads(key)`: dọn phần tải dở của chính key này từ một lần chạy bị giết. Kho không hỗ trợ lời gọi này (501, `NotImplemented`): bỏ qua.
4. `OpenRead` rồi `Put`. sha256 của các byte đã gửi phải bằng `backups.sha256`; khác (file đổi giữa bước 2 và 4): xoá object, lỗi.
5. **Đọc lại**: `Get` cả object vào hư không, tính sha256, phải bằng `backups.sha256` và đủ số byte. Khác: xoá object, lỗi "kho trả về dữ liệu khác thứ đã gửi".
6. `engine.Note` vài dòng tiến độ (mỗi 10% hoặc 30 giây) và dòng kết quả.

`Check` chỉ đúng sau `Apply` của chính bước (đẩy luôn là việc mới). `Undo`: xoá object. Kiểm chứng của Plan: `s3.object:<id>`, HEAD object, cỡ khớp; bằng chứng ghi key, cỡ, ETag và câu "sha256 đọc lại khớp". Job thành công (`after`): `MarkBackupStored(id, key, targetID, now)`; dòng đã bị xoá trong lúc tải (dọn cục bộ chen vào) thì xoá object vừa tải và coi như không có gì; rồi dọn bản cũ trên kho của domain đó (mục 3.7). Job hỏng: `MarkBackupPushFailed(id, targetID, lỗi một dòng)`.

Khoá object: `sites/<domain>/<tên file cục bộ>`, ví dụ `sites/shop.example/shop.example-20261121-033000.tar.gz`.

**Vì sao đọc lại cả object.** Chữ ký từng phần chỉ chứng minh từng phần tới nơi nguyên vẹn; nó không chứng minh object sau khi ghép bằng đúng file, và bản thử cho thấy ETag không đổi khi dữ liệu trên kho hỏng. Đọc lại là phép kiểm duy nhất không phải tin kho. Cái giá: mỗi bản tốn thêm một lượt tải về (với kho tính tiền chiều ra thì tốn tiền). Đo ở mục 2.3: 2 GiB đọc lại mất 4 đến 6 giây trên máy ảo; trên mạng thật thì bằng thời gian tải về. Không có chế độ "kiểm nhanh": bất biến 2 không có ngoại lệ.

**Ai gọi đẩy, và vì sao không giữ khoá job của máy.** Tải vài GB qua mạng chậm có thể mất hàng giờ. Nếu job đẩy giữ khoá toàn cục thì trong lúc đó mọi thao tác khác bị từ chối và health bỏ mọi vòng kiểm (health bỏ vòng khi đang có job). Đẩy không đổi gì trên máy, nên:

- `Remote.Sync(ctx, SyncOptions{Panel bool, Only []string}, onEvent)` là một lượt đẩy: lấy **khoá riêng của lượt đẩy** (`flock` không chờ trên `/run/tpan/remote.lock`; đang có lượt khác thì trả về "đang có lượt đẩy khác", không phải lỗi); khi `Panel` thì tạo và đẩy gói panel (mục 3.8); đẩy lần lượt các bản theo luật trên (hoặc đúng các id trong `Only`), mới nhất trước; mỗi bản một job `backup.push` chạy qua engine với một lớp bọc `System` có `Lock` không làm gì (khoá riêng đã giữ). Job vẫn vào Lịch sử kèm bằng chứng.
- **Cầu dao**: bản đầu tiên hỏng vì lỗi của đích (`s3_unreachable`, `s3_auth_failed`, `s3_bucket_not_found`) thì các bản còn lại của lượt đó không thử nữa, chỉ được ghi cùng lỗi (không để 50 site mỗi site chờ hết giờ một lần).
- Ba nơi gọi `Sync`: CLI sau `tpan backup create` (một site; hoặc `--all`, tức là cả lịch hằng đêm, khi đó `Panel` bật); `tpan backup push`; và **bộ đẩy nền** trong `tpan serve` (`server/pusher.go`): một goroutine được đánh thức sau mỗi job thành công của API và 10 phút một lần; không có bản nào cần đẩy thì nó chỉ tốn một truy vấn. Đích chưa đặt: `Sync` trả về ngay. Bộ đẩy nền còn giữ một hàng chờ nhỏ trong bộ nhớ cho yêu cầu đẩy hoặc tải một bản cụ thể từ API (mục 3.11).
- `backup create` thoát 0 khi bản cục bộ xong, kể cả khi đẩy lỗi; nó in rõ "đã sao lưu trên máy, CHƯA lên kho: <lý do>; sẽ đẩy bù ở lần sau, hoặc: sudo tpan backup push". `tpan backup push` thoát 1 khi còn bản đẩy lỗi.
- Phép kiểm "job treo" của M4 bỏ qua loại `backup.push`, `backup.pull`, `backup.panel` (chúng được phép chạy lâu).

Đây là cách bất biến 1 đứng vững: job sao lưu cục bộ và job đẩy là hai job khác nhau; không bước nào của Plan `backup.create` chạm tới mạng.

### 3.6 Tải về và khôi phục từ kho

**Bước `steps.S3Pull`** `{Store, Key, Dest, Dir, Size, SHA256}`:

1. `Dest` phải nằm ngay dưới `Dir`, `Dir` phải là `site.BackupDir` của domain hoặc `site.TPANDBBackupDir`; `SHA256` đúng dạng; `Size > 0`.
2. `Dest` đã có: lỗi (không ghi đè). `Dest + ".part"` còn sót từ lần trước: xoá (file của chính bước này).
3. Luật chặn đầy đĩa của M9 với `need = Size`.
4. Tạo `Dir` (0700 root), `CreateFile(Dest + ".part", 0600, root)`, `Get` vào đó.
5. Số byte phải bằng `Size` và sha256 phải bằng `SHA256`. Khác: xoá `.part`, lỗi "bản trên kho không khớp sha256 ghi lúc tạo: tải về `<a>`, mong `<b>`".
6. `Rename` sang `Dest`.

`Check`: đã `Apply` và `Dest` còn. `Undo`: xoá `Dest`.

**Khôi phục** giữ nguyên lệnh và route của M9 (`tpan backup restore <domain> [--id]`, `POST /api/v1/backups/{id}/restore`, luật `owner:backup`). `PrepareRestore` đổi như sau: dòng nguồn có `local = 1` thì y như M9. Dòng có `local = 0`: đòi `remote = 'stored'` và `remote_target` bằng mã của đích đang đặt, không thì từ chối `backup_not_on_machine`; Plan có thêm `S3Pull` làm **bước đầu tiên**, trước cả bước tự backup hiện trạng. Job thành công thì `SetBackupLocal(id, true)`. Không chỉ định id thì vẫn lấy bản đầy đủ mới nhất, dù nó nằm ở đâu.

Bất biến 4 đứng vững bằng thứ tự bước: `S3Pull` lỗi (kho hỏng, sha256 sai) thì engine dừng khi chưa bước nào khác chạy; sau đó `BackupExtract` của M9 còn kiểm sha256 lần nữa trước khi giải nén, và chỉ giải nén vào thư mục riêng của root rồi mới tráo.

**Plan `backup.pull`** (chỉ admin): một bước `S3Pull`, `after` ghi `local = 1`. Chạy dưới khoá của lượt đẩy, không giữ khoá job của máy.

**Liệt kê trên kho** (`Remote.ListRemote`, chỉ admin): `List("")`, ghép với bảng `backups` theo `remote_key`; mỗi object: key, cỡ, lúc sửa, và `known` (id và domain của dòng) hoặc "kho có mà tpan.db không biết". Kèm danh sách dòng `stored` mà object không còn ("tpan.db ghi có mà kho không thấy").

### 3.7 Giữ bản

- **Trên máy** (sửa `Backups.prune` của M9): chỉ đếm dòng `local = 1`. Bản bị dọn: xoá file; dòng có `remote = 'stored'` thì giữ dòng và đặt `local = 0`, không thì xoá dòng như M9. `fullCount` (hạn mức gói của M13) cũng chỉ đếm `local = 1`.
- **Trên kho** (`Remote.pruneRemote(domain)`, chạy sau mỗi lần đẩy thành công của domain đó): trong các dòng `kind = 'full'`, `remote = 'stored'`, `remote_target` = đích đang đặt, lý do khác `before-purge`, xếp mới nhất trước, bỏ qua `s3.keep` dòng đầu; mỗi dòng còn lại: `Delete(key)`, rồi dòng còn `local = 1` thì xoá trạng thái kho (`remote = ''`, `remote_key = ''`), không thì xoá dòng. Xoá object trước, sửa DB sau: hỏng giữa chừng thì lần sau làm lại (xoá một object đã mất là thành công).
- Bản `before-purge` không bao giờ bị dọn, ở cả hai nơi (như M9).
- Gói panel: trên máy giữ 3 gói mới nhất (cùng thư mục và cùng luật với bản sao `tpan.db` của M9); trên kho giữ `s3.keep` gói mới nhất (sắp theo tên, tên mang giờ UTC).

### 3.8 Khoá sao lưu và gói panel

**Khoá sao lưu**: `tpan-key-1-` cộng 52 ký tự base32 chữ thường (RFC 4648, không đệm) của 32 byte ngẫu nhiên từ `crypto/rand`. Luật kiểm: `^tpan-key-1-[a-z2-7]{52}$`. Dùng làm mật khẩu của `age` (người nhận scrypt), hệ số công 15 (khoảng 32 MB RAM và 0,1 giây; khoá đã là 256 bit ngẫu nhiên nên hệ số chỉ để đúng khuôn, không để chống dò). Dấu vân tay để hiển thị: 8 ký tự hex đầu của sha256 chuỗi khoá.

- Sinh ở `backup.target.set` lần đầu, hoặc `tpan backup key new` (Plan `backup.key.new`: ghi file, hiện khoá một lần). Sinh khoá mới thì các gói panel cũ vẫn mở được bằng khoá cũ nếu người quản trị còn giữ; TPAN không giữ khoá cũ.
- `tpan backup key show`: dấu vân tay và đường dẫn file; `--reveal` in cả khoá (root đọc được file thì in ra không lộ thêm gì). API chỉ trả dấu vân tay.

**Mất khoá thì mất gì** (câu này phải có nguyên trong tài liệu giải thích):

| Tình huống | Hậu quả |
|---|---|
| Quên khoá, máy còn sống | Không mất gì: khoá nằm ở `/etc/tpan/backup.key`, `sudo tpan backup key show --reveal` in lại |
| Máy mất, còn khoá | Dựng lại được cả máy bằng `tpan machine restore --from-s3` |
| Máy mất **và** không còn khoá | Mọi gói panel trên kho không mở được: mất danh sách user, gói, mật khẩu băm, cài đặt, lịch sử và bảng ghi sha256 của các bản sao lưu. **Không mất nội dung site**: các file `.tar.gz` của site trên kho không mã hoá, tải về bằng công cụ S3 bất kỳ, giải nén ra `public/`, `db.sql`, `manifest.json` rồi nhập bằng tay vào site mới tạo cùng domain. Lệnh dựng lại máy không chạy được |

**Gói panel** `tpan-<YYYYMMDD-HHMMSS>.tar.gz.age` (giờ UTC):

- Bên trong (tar, gzip): `manifest.json` `{"format":1,"created","hostname","tpan_version","schema","db_sha256"}` với `schema` là số migration lớn nhất đã áp; `tpan.db` (bản `VACUUM INTO`); `config.toml` nếu `/etc/tpan/config.toml` có.
- **Không** có: `s3.json`, `backup.key`, khoá TLS của panel, `/etc/letsencrypt`.
- Bước `steps.PanelBundle` `{Dir: site.TPANDBBackupDir, KeyFile, Snapshot func(ctx, path) error, Manifest…}`: tạo thư mục tạm `.tmp-<giờ>` (0700) trong `Dir`; gọi `Snapshot` (store chép DB) vào đó; ghi `manifest.json`; `tar --create --gzip` ba file ra `panel.tar.gz` trong thư mục tạm; mã hoá theo dòng (`OpenRead` → `agebox.Encrypt` → `CreateFile`) ra `<Dir>/<tên>.part` rồi đổi tên; tính sha256 của file mã; **luôn** xoá thư mục tạm (bản rõ của `tpan.db`) kể cả khi lỗi. Kết quả: `Path`, `Name`, `Size`, `SHA256`. `Undo`: xoá file mã.
- Plan `backup.panel`: `PanelBundle` rồi `S3Push` với key `panel/<tên>` (đọc đường dẫn và sha256 từ bước trước lúc `Apply`). Job thành công: ghi `s3.panel_last`, xoá `s3.panel_error`, dọn gói cũ trên máy và trên kho. Hỏng: ghi `s3.panel_error`.
- Chạy trong `Remote.Sync` khi được gọi từ `backup create --all` (tức là cùng nhịp lịch hằng đêm) và từ `tpan backup push --panel`. Điều kiện: đã đặt đích và có khoá sao lưu.

`tpan.db` trong gói có bảng `sessions` (chỉ sha256 của token) và mật khẩu băm argon2id; không có khoá S3 (mục 3.2), không có mật khẩu DB của site (M9: nằm trong `wp-config.php`).

### 3.9 cloud-init

**Lệnh** `tpan cloud-init render` (chỉ đọc, không cần job):

```
tpan cloud-init render --web nginx|apache [--sites FILE] [--private]
                       [--base URL] [--sums FILE] [--max-bytes N] [--out FILE]
tpan cloud-init render --restore [--private] [--base URL] [--sums FILE] [--max-bytes N] [--out FILE]
```

- `--base`: nơi có `tpan-linux-<arch>`; mặc định `https://github.com/doxuta/tpan/releases/download/<bản đang chạy>`. Nhận `https://` hoặc `http://` (toàn vẹn do sha256 ghim trong user-data bảo đảm, không do kênh truyền), chỉ gồm `[A-Za-z0-9._~:/-]`, không `..`.
- sha256 của hai file chạy (amd64, arm64) lấy từ `--sums FILE` (một file `SHA256SUMS`); không có cờ thì tải `<base>/SHA256SUMS` bằng `curl` qua `System` lúc sinh. Thiếu một trong hai dòng: từ chối.
- Chế độ **site mới** (`--web`): `--sites` qua `ParseSitesFile` và `Resolve` của M6 rồi được **ghi lại từ cấu trúc đã kiểm**, không chép nguyên văn file người dùng đưa. Ba thứ bị từ chối ngay lúc sinh (`cloudinit_invalid`), vì chắc chắn hỏng ở lần khởi động đầu: site không có email quản trị (ở dòng hoặc ở `defaults`); `owner` khác `admin` (máy mới chưa có user nào); `ssl: true` (DNS chưa trỏ về máy mới thì chưa xin được chứng chỉ; xin sau bằng `tpan ssl issue`).
- Chế độ **dựng lại** (`--restore`): đọc đích và khoá sao lưu của máy đang chạy lệnh; thiếu thì từ chối `s3_not_configured`, `backup_key_missing`.
- `--max-bytes` mặc định 16384 (giới hạn nhỏ nhất đã tra, AWS EC2); lớn hơn thì từ chối `cloudinit_too_large` kèm cỡ thật và bảng giới hạn ở mục 2.5.
- Kết quả: user-data ra stdout hoặc `--out` (0600). Chế độ site mới còn in ra **stderr, một lần**, mật khẩu quản trị panel của máy mới.

**Quyền quản trị của máy mới.** `render` sinh một mật khẩu ngẫu nhiên 32 ký tự (`store.GeneratePassword`), in ra cho người đang gõ lệnh, và chỉ đặt **bản băm argon2id** vào user-data. Vì sao: bản băm của một bí mật 190 bit không dò ngược được, nên user-data có bị mọi tiến trình đọc mãi mãi cũng không lộ gì; người chủ có mật khẩu trước cả khi máy tồn tại và không phải đọc console. Không nhận mật khẩu do người dùng tự chọn (bản băm của mật khẩu yếu thì dò được). Vì sao không in mã dùng một lần ra console: log console do nhà cung cấp giữ, ai có quyền đọc tài khoản cloud đều xem được, và TPAN chưa có luồng "mã dùng một lần" ở màn đăng nhập. Đường dự phòng không đổi: `sudo tpan admin passwd` qua SSH.

**Khuôn user-data** (sinh bằng bộ ghi YAML từ một cấu trúc Go, không ghép chuỗi):

```yaml
#cloud-config
# Do "tpan cloud-init render" sinh. Không chứa mật khẩu, khoá hay token dạng rõ.
write_files:
  - path: /etc/tpan/cloud/config.json        # cấu hình của lần khởi động đầu
    permissions: '0600'
    encoding: b64                            # hoặc gz+b64 khi nhỏ hơn
    content: …
  - path: /etc/tpan/cloud/sites.yaml         # chỉ ở chế độ site mới có --sites
    permissions: '0600'
    encoding: gz+b64
    content: …
  - path: /usr/local/sbin/tpan-first-boot
    permissions: '0700'
    content: |
      (script cố định, xem dưới)
runcmd:
  - [ /usr/local/sbin/tpan-first-boot ]
```

`config.json`: `{"version":1,"mode":"sites"|"restore","web":"nginx"|"apache"|"","private":bool,"admin_hash":"$argon2id$…"|"","sites_file":"/etc/tpan/cloud/sites.yaml"|"","restore":{"endpoint","region","bucket","prefix","path_style","ca_pem","credentials"}}`. `restore.credentials` là hai khoá truy cập của kho, **mã hoá bằng khoá sao lưu** (`agebox`, dạng chữ có đầu `-----BEGIN AGE ENCRYPTED FILE-----`). Trong user-data nó là bản mã; không có khoá sao lưu thì không ai trên máy đọc được.

**Luật chống chèn nội dung**: giá trị do người dùng đưa chỉ đi vào **nội dung file** (base64), không bao giờ vào `runcmd`, vào đường dẫn hay vào khoá YAML. Bốn giá trị duy nhất đi vào script shell là `base`, tên bản, và hai sha256; chúng đã qua luật ký tự ở trên (không có nháy, khoảng trắng, `$`, dấu chấm phẩy, xuống dòng) và được đặt trong nháy đơn. Script:

```sh
#!/bin/sh
# Do "tpan cloud-init render" sinh cho tpan <bản>. Tải đúng file chạy đã ghim,
# kiểm sha256, rồi giao phần còn lại cho chính tpan.
set -eu
arch="$(dpkg --print-architecture)"
case "$arch" in
  amd64) sum='<64 hex>' ;;
  arm64) sum='<64 hex>' ;;
  *) echo "tpan: kiến trúc $arch không được hỗ trợ" >&2; exit 1 ;;
esac
bin=/usr/local/bin/tpan
if ! { [ -x "$bin" ] && echo "$sum  $bin" | sha256sum -c --status -; }; then
  command -v curl >/dev/null 2>&1 || { apt-get update -q && apt-get install -y -q curl; }
  tmp="$bin.first-boot"
  curl -fsSL --retry 5 --retry-delay 3 --connect-timeout 20 -o "$tmp" '<base>/tpan-linux-'"$arch"
  echo "$sum  $tmp" | sha256sum -c --status - || { echo "tpan: sha256 của file tải về không khớp bản đã ghim; không cài" >&2; rm -f "$tmp"; exit 1; }
  chown root:root "$tmp" && chmod 0755 "$tmp" && mv -f "$tmp" "$bin"
fi
exec "$bin" cloud-init run
```

**`tpan cloud-init run`** (lệnh ẩn; chỉ root; đọc `/etc/tpan/cloud/config.json`; không có file thì từ chối). Mỗi việc ghi một dòng có giờ vào `/var/log/tpan/first-boot.log` (0600) và cập nhật `/var/lib/tpan/first-boot.json` (`{"mode","started","finished","ok","steps":[{"name","status","job_id","seconds","error"}]}`):

1. `setup`: đúng Plan của `tpan setup` (thư mục, chứng chỉ tự ký, unit, dịch vụ), với `--private` nếu có. **Không** sinh và không in mật khẩu quản trị.
2. `admin`: DB chưa có mật khẩu băm của admin và `admin_hash` khác rỗng thì ghi nó (sau khi kiểm đúng dạng PHC argon2id mà `store` tự sinh). Đã có thì không đụng.
3. Chế độ site mới: `stack` (`stack install --web`); `sites` (`apply` file site; mật khẩu quản trị WordPress của các site vừa tạo ghi vào `/root/tpan-first-boot-passwords.txt`, 0600, không in ra output); `health` (như `tpan health`; có phép kiểm lỗi thì việc này lỗi).
4. Chế độ dựng lại: không cài stack (web server và nguồn gói phải đọc từ `tpan.db` cũ, mục 2.6); ghi vào log câu hướng dẫn: `sudo tpan machine restore --from-s3`.

Việc nào lỗi thì dừng, mã thoát khác 0; vì script là lệnh duy nhất của `runcmd` nên `cloud-init status` cũng báo lỗi. Chạy lại (`cloud-init single --name scripts_user --frequency always`, hoặc gõ lại `tpan cloud-init run`) an toàn: file chạy đã đúng sha256 thì không tải lại, mọi Plan đều bỏ qua bước đã đúng, site đã có thì `apply` bỏ qua.

`tpan cloud-init status` in `first-boot.json` cho người đọc.

`scripts/vm.sh` có thêm: `cloud-init <file> <tên>` (tạo máy ảo phụ), `on <tên> <lệnh con>`, `delete <tên>`, `start`, `push`, `pull`. Ba lệnh đầu chỉ nhận đúng tên `<máy ảo chính>-ci`.

### 3.10 Dựng lại máy

`tpan machine restore --from-s3` (chỉ CLI, chỉ root; không có API vì lệnh thay chính `tpan.db` mà panel đang mở, cùng lý do với `self-update`).

**Đầu vào.** Đích: từ `/etc/tpan/cloud/config.json` (máy tạo bằng user-data chế độ dựng lại), hoặc từ cờ `--endpoint --bucket [--region] [--prefix] [--vhost] [--ca-file]`, hoặc từ đích đã đặt trên máy. Khoá sao lưu: hỏi trên terminal không hiện chữ, hoặc `--key-stdin`. Hai khoá truy cập: giải mã từ `restore.credentials` bằng khoá sao lưu; không có thì `--keys-stdin` (hai dòng) hoặc hỏi. `--bundle <tên>` chọn gói panel cụ thể (mặc định mới nhất). `--yes`, `--dry-run`, `--json` như mọi lệnh.

**Pha 1, chỉ đọc máy** (ghi trong `/var/lib/tpan/restore/_machine`, root 0700):

1. `List("panel/")`, chọn gói; không có thì `machine_bundle_not_found`.
2. Tải về (`CreateFile`, theo dòng), giải mã theo dòng bằng khoá sao lưu. Sai khoá: `backup_key_wrong`. Bản mã bị sửa: lỗi của lớp xác thực, dừng.
3. `tar --list --verbose` trên bản rõ: chỉ nhận đúng ba tên `manifest.json`, `tpan.db`, `config.toml`, đều là file thường; giải nén với `--no-same-owner --no-same-permissions`; sha256 của `tpan.db` phải bằng `manifest.db_sha256`; `manifest.format` phải là 1; `manifest.schema` lớn hơn số migration bản TPAN này biết thì từ chối `machine_bundle_newer` ("cài tpan bản X trở lên").
4. **Máy phải trắng**: `tpan.db` hiện tại không có site nào, không có DB rời nào, không có user nào ngoài `admin`, và `/var/www` không có thư mục con. Không thì từ chối `machine_not_empty`. Đây là chốt chặn việc gõ nhầm lệnh trên máy đang chạy.
5. In bản xem trước: gói nào, tạo lúc nào ("mọi thay đổi sau giờ đó không có trong gói"), bao nhiêu site sẽ dựng, bao nhiêu thứ không dựng được (bên dưới). Hỏi xác nhận.

**Pha 2, nhận `tpan.db` cũ**: `systemctl stop tpan`; đổi tên `tpan.db` hiện tại thành `tpan.db.before-machine-restore` (xoá `-wal`, `-shm`); đặt `tpan.db` từ gói (0600 root); mở store (migration tự chạy nếu gói từ bản cũ hơn); `List("sites/")` để biết object nào đang thật sự có trên kho; rồi trong một transaction: xoá mọi dòng `sessions`; với bảng `backups`: dòng `kind = 'full'` có `remote = 'stored'` mà `remote_key` có trên kho với đúng cỡ thì đặt `local = 0` và ghi `remote_target` = mã của đích đang dùng (nối lại theo **object thật**, không theo mã đích cũ: gõ endpoint bằng một tên khác của cùng kho không làm mất chỉ mục); mọi dòng khác bị xoá (file của chúng đã mất cùng máy cũ) và được đếm vào báo cáo; xoá mọi dòng `databases` (DB rời không có bản sao lưu) sau khi ghi tên chúng vào báo cáo. Ghi file trạng thái `/var/lib/tpan/machine-restore.json` (`{"bundle","started","phase","sites":{domain:{"status","flags","job_ids","error"}}}`) trước mỗi việc, để chạy lại thì đi tiếp.

**Pha 3, dựng lại** (mọi việc là Plan sẵn có, job ghi vào `tpan.db` vừa nhận):

1. Ghi `/etc/tpan/backup.key` bằng khoá vừa nhập (bước `SecretFile`), **rồi** Plan `backup.target.set` với đúng đích và hai khoá vừa dùng. Thứ tự này để Plan đặt đích thấy đã có khoá và không sinh khoá mới; từ đây máy mới tự sao lưu tiếp vào cùng chỗ, cùng khoá.
2. `config.toml` từ gói về `/etc/tpan/config.toml` nếu có.
3. Stack: `PrepareInstall` với web server ghi trong settings của `tpan.db` cũ (nguồn gói cũng từ đó).
4. Bản PHP: mỗi bản khác bản mặc định mà một site active đang dùng, `PHP.PrepareInstall`.
5. Từng site `active`, theo thứ tự domain. Site không có bản đầy đủ nào `stored`: ghi "không có bản sao lưu trên kho", xoá dòng `sites`, sang site sau. Còn lại:
   a. Ghi các cờ gốc vào file trạng thái: `suspended`, `db_suspended`, `ssl`. Lưu dòng site với ba cờ đó xoá (Plan tạo site từ chối site đang tạm khoá; vhost 443 cần chứng chỉ chưa có).
   b. `Sites.PrepareAdd` với email `admin@<domain>` (WordPress cài mới ở bước này chỉ là giàn giáo: DB của nó bị thay ngay ở bước c).
   c. `Backups.PrepareRebuild(domain, id bản đầy đủ mới nhất)`: như khôi phục từ kho, khác ba chỗ: không tự backup hiện trạng (hiện trạng là một WordPress trống); `BackupExtract` **giữ `wp-config.php` của bản sao lưu** (mọi tuỳ chỉnh của chủ site còn nguyên) thay vì bản của site vừa tạo; sau khi nhập DB thêm bước `DBPassword` (đặt mật khẩu mới cho user DB và ghi vào `wp-config.php` đó, vì mật khẩu cũ trong file không còn khớp user DB mới), và khi site vốn có SSL thêm `WPSiteURL` về `http://<domain>`. Kiểm chứng: bộ phép kiểm của site như M9.
   d. Cờ gốc có tạm khoá: `Sites.PrepareSuspend`, rồi ghi lại đúng nguồn khoá gốc (`self` hay `user`); DB vốn bị khoá: Plan khoá DB của site. Cờ `ssl` không ghi lại (site đang chạy http).
   e. Một site lỗi không chặn site sau; lỗi và id job vào file trạng thái.
6. Lịch backup: `backup.at` trong settings khác rỗng thì `PrepareSettings` để ghi lại file cron.
7. `systemctl start tpan`; chạy health đầy đủ; in báo cáo.

**Báo cáo cuối** (cũng là `--json`): site đã dựng; site lỗi kèm job; và danh sách **không dựng lại được**: site trong thùng rác (dòng giữ nguyên, xoá hẳn bằng `tpan trash purge`), DB rời (đã xoá dòng), bản sao lưu chỉ có trên máy cũ; và **việc phải tự làm**: trỏ DNS, `sudo tpan ssl issue <domain>` cho từng site vốn có SSL, bật lại ufw và fail2ban nếu máy cũ có bật.

Chạy lại sau khi hỏng giữa chừng: có file trạng thái và `tpan.db` đã nhận thì bỏ pha 1 và 2 (vẫn hỏi khoá để kiểm với gói đã ghi), làm tiếp các việc chưa `done`. Mỗi Plan bên trong vốn chạy lại được.

### 3.11 API

Mọi route mới khai luật trong `internal/server/routes.go`; đều là `admin`. Thân và câu trả lời ở `types_m18.go`.

| Route | Việc | Ghi chú |
|---|---|---|
| `GET /api/v1/backups/target` | `TargetView` (mục 3.3) | không có khoá nào, kể cả dạng che, ngoài `access_key_tail` |
| `POST /api/v1/backups/target[?dry_run=1]` | Plan `backup.target.set`; thân `TargetRequest{endpoint, region, bucket, prefix, path_style, keep, ca_pem, access_key, secret_key}` | `access_key`, `secret_key` rỗng nghĩa là giữ hai khoá đang lưu (đổi `keep` không phải gõ lại khoá). Khoá sao lưu mới sinh trả qua bí mật một lần của job |
| `DELETE /api/v1/backups/target[?dry_run=1]` | Plan `backup.target.rm` | |
| `POST /api/v1/backups/target/test` | Plan `backup.target.test` | |
| `POST /api/v1/backups/key[?dry_run=1]` | Plan `backup.key.new` | khoá trả qua bí mật một lần của job |
| `POST /api/v1/backups/{id}/push[?dry_run=1]` | Plan `backup.push` cho một bản | `dry_run=1` trả bản xem trước. Chạy thật: kiểm điều kiện (đã đặt đích, bản còn trên máy, là bản đầy đủ), đặt `remote_tries = 0`, đưa vào hàng chờ của bộ đẩy nền, trả 202 `{"queued":true}`. Job hiện ở Lịch sử khi nó chạy; không giữ chỗ chạy job của API |
| `POST /api/v1/backups/{id}/pull[?dry_run=1]` | Plan `backup.pull` | như trên; bản đã có trên máy: 409 `backup_already_local` |
| `GET /api/v1/backups/remote` | `ListRemote` (mục 3.6) | |
| `POST /api/v1/cloud-init/render` | thân `CloudInitRequest{mode, web, private, base, sha256_amd64, sha256_arm64, max_bytes, sites}`; trả `{user_data, bytes, admin_password}` | không đổi gì trên máy; `admin_password` chỉ có ở chế độ site mới, đúng một lần trong câu trả lời này |

Route đã có đổi nội dung: `GET /api/v1/backups` (luật `any`, lọc theo chủ như M13): mỗi dòng thêm `local`, `remote`, `remote_at`; `remote_key` và `remote_error` chỉ có khi người gọi là admin. `POST /api/v1/backups/{id}/restore` (luật `owner:backup`): tự tải về khi bản chỉ còn trên kho.

Nhật ký (`audit`) của các route trên ghi `target` là `endpoint bucket prefix` hoặc id bản sao lưu; không bao giờ ghi thân request. Thân có khoá chỉ được đọc vào `system.Secret`.

Mã HTTP: `s3_endpoint_invalid`, `s3_region_invalid`, `s3_bucket_invalid`, `s3_prefix_invalid`, `s3_keys_invalid`, `s3_keep_invalid`, `s3_ca_invalid`, `cloudinit_invalid`, `cloudinit_too_large`, `backup_key_invalid` là 400; còn lại 409 như mọi lời từ chối.

### 3.12 CLI

```
tpan backup target set --endpoint URL --bucket B [--region R] [--prefix P] [--vhost] [--ca-file F] [--keep N] [--keys-stdin]
tpan backup target show | test | rm
tpan backup key new | show [--reveal]
tpan backup push [<id>] [--panel]
tpan backup pull <id>
tpan backup list [<domain>] [--remote]
tpan cloud-init render …        (mục 3.9)
tpan cloud-init status
tpan cloud-init run             (ẩn)
tpan machine restore --from-s3 …  (mục 3.10)
```

Mọi lệnh có thay đổi nhận `--dry-run`, `--yes`, `--json`. **Không có cờ nào nhận khoá trên dòng lệnh** (dòng lệnh lọt vào `ps` và lịch sử shell): hai khoá truy cập đọc từ stdin khi có `--keys-stdin` (dòng 1 access key, dòng 2 secret key), không thì hỏi trên terminal, secret key và khoá sao lưu hỏi không hiện chữ. `target set` chạy lại không có `--keys-stdin` trên máy đã có khoá và không có terminal thì giữ hai khoá đang lưu.

`tpan backup list` có thêm cột "Ở ĐÂU": `máy`, `kho`, `máy+kho`, `kho (đích khác)`, `đẩy lỗi`.

### 3.13 Health

Hai phép kiểm, chặng `tpan`, **chỉ tồn tại khi đã đặt đích** (máy không đặt đích thì kết quả health không đổi một dòng nào so với v0.2.0):

| ID | Kiểm gì | Trạng thái |
|---|---|---|
| `tpan.s3:target` (nặng: chỉ chạy ở vòng đầy đủ mỗi giờ) | Đọc được hai khoá, dựng được client, `Check` | Đạt; `s3_unreachable`, `s3_auth_failed`, `s3_bucket_not_found`, `s3_error`: **Lỗi** kèm mã HTTP, tên lỗi, cách sửa tay (`tpan backup target test`, `tpan backup target set …`) |
| `tpan.s3:fresh` (nhẹ, chỉ đọc `tpan.db`) | (a) có dòng `remote = 'failed'`; (b) lịch backup đang bật mà có site active không có bản đầy đủ nào `stored` trên đích này trong 48 giờ; (c) có khoá sao lưu mà `s3.panel_last` cũ hơn 48 giờ hoặc `s3.panel_error` khác rỗng | Đạt; (a), (b), (c): **Cảnh báo**, tóm tắt nêu số bản lỗi, tối đa 5 domain, lỗi gần nhất; cách sửa: `sudo tpan backup push` |

48 giờ: một ngày lịch cộng một ngày trễ, cùng ngưỡng với `tpan.backup:db` của M9.

### 3.14 Mã từ chối mới

`s3_not_configured`, `s3_endpoint_invalid`, `s3_region_invalid`, `s3_bucket_invalid`, `s3_prefix_invalid`, `s3_keys_invalid`, `s3_keep_invalid`, `s3_ca_invalid`, `s3_unreachable`, `s3_auth_failed`, `s3_bucket_not_found`, `s3_error`, `backup_not_on_machine`, `backup_already_local`, `backup_key_missing`, `backup_key_invalid`, `backup_key_wrong`, `cloudinit_invalid`, `cloudinit_too_large`, `cloudinit_no_config`, `machine_bundle_not_found`, `machine_bundle_newer`, `machine_bundle_invalid`, `machine_not_empty`.

### 3.15 Máy trạng thái của một bản sao lưu

```
                  tạo (M9)
                     │
                     ▼
            ┌─ local=1, remote='' ─┐◄────────── dọn trên kho (còn file trên máy)
   đẩy lỗi  │                      │ đẩy đạt + đọc lại khớp
            ▼                      ▼
   local=1, remote='failed' ──► local=1, remote='stored' ──► local=0, remote='stored'
            │   (đẩy lại đạt)      ▲        (dọn trên máy)         │
            │                      └────── tải về / khôi phục ─────┘
            ▼                                                      │ dọn trên kho
   dọn trên máy: xoá dòng                                          ▼
                                                               xoá dòng
```

Không có trạng thái `local=0` mà `remote` khác `stored`: mọi chỗ đặt `local=0` đều đòi `remote='stored'`, và mọi chỗ xoá trạng thái kho của một dòng `local=0` đều xoá luôn dòng.

## 4. Bất biến

Mỗi câu gắn với ít nhất một test có tên. B1 đến B6 là sáu bất biến của đặc tả; B7 trở đi là của thiết kế này.

| # | Bất biến | Test |
|---|---|---|
| B1 | Sao lưu cục bộ không bao giờ hỏng vì kho hỏng: Plan `backup.create` không có bước nào chạm mạng; kho lỗi kiểu gì thì job sao lưu vẫn `succeeded` và dòng `backups` vẫn có | `core.TestSaoLuuCucBoKhongPhuThuocKho` (kho giả lỗi mọi lời gọi); e2e "tắt máy chủ S3" |
| B2 | Không dòng nào có `remote='stored'` khi chưa đọc lại object và so khớp sha256 | `steps.TestS3PushDocLaiMoiDanhDau` (kho giả trả dữ liệu khác: bước lỗi, object bị xoá, dòng không `stored`); `store.TestMarkBackupStoredChiQuaMotHam` |
| B3 | Hai khoá truy cập và khoá sao lưu không xuất hiện ở đầu ra nào | `server.TestKhongLoKhoaS3` (đặt đích với khoá đánh dấu, gọi mọi route GET và mọi route M18 với ba vai, quét mọi thân trả lời, `job_events`, `audit`, `settings`); `tools.TestMCPKhongLoKhoaS3`; `core.TestGoiPanelKhongCoKhoa` (mở gói panel, quét); `s3.TestLoiKhongCoKhoa`; e2e quét `tpan.db`, journal, log job, bằng chứng, gói panel đã giải mã |
| B4 | Khôi phục không ghi gì vào site trước khi bản tải về qua kiểm toàn vẹn | `core.TestKhoiPhucTuKhoSaiSha256KhongDungSite` (so ảnh chụp hệ thống file giả trước và sau: chỉ thư mục backup đổi); e2e "sửa hỏng object" so checksum file và bản xuất DB |
| B5 | User thường không đặt, xem, thử, gỡ được đích, không sinh được khoá, không sinh được user-data, không thấy bản sao lưu của user khác | `server.TestPhanQuyenMoiRoute` (tự phủ route mới); `server.TestUserKhongDungDuocS3` |
| B6 | user-data do TPAN sinh không chứa mật khẩu, khoá hay token dạng rõ | `cloudinit.TestKhongCoBiMatDangRo` (sinh với khoá và mật khẩu đánh dấu, quét cả bản đã giải base64 và gzip); e2e quét file thật |
| B7 | Không lời gọi nào của client ra ngoài tiền tố của đích | `s3.TestKeyKhongRaNgoaiTienTo` |
| B8 | Mọi request ký đúng: vectơ mẫu của AWS, và máy chủ giả tính lại chữ ký từ request nhận được | `s3.TestSigV4VectoAWS`, `s3.TestMayChuGiaKiemChuKy` |
| B9 | Tải nhiều phần hỏng thì không để lại phần tải dở (khi kho còn trả lời), và lần đẩy sau dọn phần dở của lần trước | `s3.TestPutLoiThiHuy`, `steps.TestS3PushDonTaiDo` |
| B10 | Lượt đẩy (`Sync`) và job `backup.pull` không giữ khoá job của máy. (Riêng bước tải về nằm trong job khôi phục thì chạy dưới khoá đó, vì khôi phục là job đổi máy.) | `core.TestSyncKhongGiuKhoaToanCuc` |
| B11 | Máy không đặt đích: kết quả health, danh sách route cũ, output của `backup create|list|restore` không đổi so với trước | `health.TestKhongDichKhongCoPhepKiemS3`; e2e nâng cấp |
| B12 | user-data chỉ có một lệnh `runcmd` cố định; mọi giá trị người dùng nằm trong nội dung file | `cloudinit.TestChenNoiDung` (tiêu đề site, email, base có xuống dòng, nháy, `$(…)`, `- [ rm ]`: hoặc bị từ chối, hoặc đọc lại YAML vẫn đúng một `runcmd` và đúng các đường dẫn file) |
| B13 | Gói panel không giả được và không sửa được khi không có khoá sao lưu | `agebox.TestSuaMotByteThiLoi`, `agebox.TestSaiKhoaThiLoi` |
| B14 | Dựng lại máy không chạy trên máy đang có site | `core.TestMayKhongTrangThiTuChoi` |
| B15 | Site đang tạm khoá trên máy cũ không bao giờ ở trạng thái mở sau khi lệnh dựng lại máy kết thúc thành công cho site đó | `core.TestDungLaiSiteTamKhoa` |

## 5. Mô hình đe doạ

| Ai | Đường đi | Chặn bằng | Test |
|---|---|---|---|
| Người đọc được bucket (lộ khoá chỉ đọc, bucket để công khai nhầm, nhân viên nhà cung cấp) | Đọc gói panel để lấy mật khẩu băm, token, lịch sử | Gói panel mã hoá bằng khoá sao lưu | B13 |
| | Đọc bản sao lưu site | **Không chặn** ở bản này (mục 1); tài liệu nói rõ; khuyên bucket riêng tư và mã hoá phía kho | |
| Người **ghi** được vào bucket | Thay một bản sao lưu site bằng bản có mã độc | sha256 ghi lúc tạo nằm trong `tpan.db` của máy, không lấy từ kho; tải về sai sha256 thì dừng | B4 |
| | Giả một gói panel để lệnh dựng lại máy nhận một `tpan.db` do họ viết | Mã hoá đối xứng có xác thực: không có khoá sao lưu thì không tạo được gói giải ra được (mục 2.4) | B13 |
| | Đặt lại một gói panel cũ nhưng thật (lùi về quá khứ), hoặc xoá hết | Không chặn được bằng mật mã; lệnh dựng lại in giờ tạo của gói và hỏi xác nhận; mất hết thì bản trên máy vẫn còn tới khi máy mất | |
| | Đặt object có tên lạ trong tiền tố | `ListRemote` chỉ hiển thị (tên đã bỏ ký tự điều khiển), không tự nhận object lạ làm bản sao lưu | `core.TestListRemoteObjectLa` |
| Kho độc hại hoặc bị giả (kẻ đứng giữa khi không TLS) | Trả dữ liệu sai, trả chuyển hướng sang host khác để lấy chữ ký | Bắt buộc https trừ loopback; không đi theo chuyển hướng; chữ ký gắn host; mọi bản tải về kiểm sha256 | `s3.TestKhongDiTheoChuyenHuong` |
| | Trả thân lỗi rất dài hoặc có ký tự điều khiển để làm bẩn log và giao diện | Thân lỗi đọc tối đa 64 KB, câu lỗi cắt 300 ký tự và bỏ ký tự điều khiển | `s3.TestLoiDuocLamSach` |
| User thường của panel | Đặt đích về máy chủ của họ để nhận bản sao lưu của mọi site; đọc khoá; tự sinh user-data có đích | Mọi route M18 là `admin`; khôi phục vẫn theo `owner:backup` và kiểm chủ của site hiện tại như M13 | B5 |
| | Dùng khôi phục để tải bản của người khác | `owner:backup` trả 404 cho bản không phải của mình | test phân quyền của M13 |
| Admin bị chiếm phiên | Trỏ endpoint vào dịch vụ nội bộ để dò mạng qua câu lỗi | Câu lỗi chỉ có mã HTTP, tên lỗi, 300 ký tự đầu; không trả thân; không đi theo chuyển hướng. Admin vốn tương đương root nên đây chỉ là giảm bề mặt | |
| Tiến trình bất kỳ trên máy mới (PHP của một site bị chiếm) | Đọc user-data qua dịch vụ metadata | user-data chỉ có bản băm của mật khẩu 190 bit và bản mã của khoá truy cập | B6 |
| Người sửa được nơi đặt file chạy (`base`) | Thay `tpan-linux-<arch>` bằng file khác | sha256 ghim trong user-data, kiểm trước khi cài | e2e "sửa file chạy: lần khởi động đầu dừng" |
| Người viết `sites.yaml` (dán từ nơi không tin) | Chèn lệnh vào user-data qua tiêu đề, email, domain | Mục 3.9: file được ghi lại từ cấu trúc đã kiểm, đi vào user-data dạng base64, không vào `runcmd` | B12 |
| Người xem `ps`, lịch sử shell, log | Thấy khoá trên dòng lệnh hay trong log job | Khoá chỉ đi qua stdin, file 0600, `system.Secret`; `engine.Note` có luật cấm bí mật | B3 |

**Panel chạy bằng root đụng vào thứ do site kiểm soát.** M18 không mở đường mới vào cây của site: `S3Push` chỉ đọc file `.tar.gz` trong `/var/lib/tpan/backups` (root 0700, do chính TPAN ghi), mở bằng `O_NOFOLLOW` và đòi file thường; `S3Pull` chỉ tạo file mới (`O_EXCL`, `O_NOFOLLOW`) trong thư mục đó. Thứ do site kiểm soát chỉ được đụng tới qua các bước của M9 (GNU tar không theo symlink, giải nén vào thư mục riêng của root, đặt lại chủ và quyền, kiểm danh sách thành viên trước khi giải nén), không đổi gì. Một điểm mới ở dựng lại máy: `wp-config.php` lấy từ bản sao lưu thay vì từ site; file đó do chủ site viết, nên nó chỉ được đặt vào `public/` với chủ là user của site, quyền 0600, và chỉ được chạm bằng `wp config set` chạy dưới user của site (bước `DBPassword` sẵn có đọc nó bằng `ReadFile` có ba lớp chặn của M9). Tên object trên kho không bao giờ thành đường dẫn trên máy: đường dẫn đích của `S3Pull` lấy từ cột `path` của dòng `backups`, và phải nằm dưới thư mục backup của đúng domain.

**Gói panel khi giải nén** (dựng lại máy): nội dung đã qua xác thực của khoá sao lưu nên tin được ngang với khoá; vẫn kiểm danh sách thành viên (đúng ba tên, file thường) và giải nén không lấy chủ, quyền từ tar.

## 6. Nâng cấp và lùi

- **Từ v0.2.0 lên**: migration 1800 chỉ thêm cột có DEFAULT và một chỉ mục; không sửa dòng nào. Không đặt đích thì: không có phép kiểm mới, không có job mới, bộ đẩy nền chỉ chạy một truy vấn rồi ngủ, `backup create|restore` đi đúng đường cũ (dòng nào cũng `local = 1`). Lịch backup cũ (`/etc/cron.d/tpan-backup`) gọi đúng lệnh cũ; lệnh đó giờ có thêm lượt đẩy ở cuối, không làm gì khi chưa đặt đích.
- **JSON**: `GET /api/v1/backups` thêm trường, không bỏ trường nào; giao diện v0.2.0 bỏ qua trường lạ.
- **Lùi về v0.2.0** sau khi đã nâng: như mọi mốc từ M1, bản cũ từ chối mở `tpan.db` có migration nó không biết. Muốn lùi thật: chép lại bản sao `tpan.db` từ trước khi nâng. Nếu đã có bản chỉ còn trên kho (`local = 0`) thì bản cũ dù có mở được DB cũng không biết tải về; vì vậy hướng dẫn nâng cấp ghi: trước khi lùi, `tpan backup pull` các bản cần giữ.
- **Gói panel từ bản TPAN cũ hơn** dựng lại trên bản mới: migration chạy lúc mở. Gói từ bản mới hơn: từ chối `machine_bundle_newer` ở pha 1, chưa đụng gì tới máy.
- **Đổi khuôn gói panel về sau**: `manifest.format` tăng; bản này từ chối `format` khác 1.

## 7. Kế hoạch kiểm thử

**Unit** (Mac, `make check`): các test có tên ở mục 4, cộng:

- `s3`: mã hoá đường dẫn và query (bảng ca: dấu cách, `+`, `/`, UTF-8, `~`); một PUT và nhiều phần với cỡ phần nhỏ; thử lại khi 503 rồi đạt; 4xx không thử lại; thân 200 chứa `<Error>` ở bước ghép; phân trang của `List`; hết giờ khi không có byte nào đi.
- `system`: `OpenRead` từ chối symlink và thư mục; `CreateFile` từ chối file đã có, đặt đúng quyền và chủ; hai bản cài đặt cho cùng kết quả.
- `agebox`: khoá đúng dạng; mã hoá rồi giải mã 5 MiB theo dòng; bọc và mở mẩu nhỏ dạng chữ.
- `cloudinit`: hai chế độ; cỡ vượt trần bị từ chối; đọc lại YAML ra đúng cấu trúc; script có đúng bốn chỗ thay.
- `store`: migration 1800 trên DB của v0.2.0 có sẵn dòng `backups`; luật chọn bản cần đẩy; các hàm đổi trạng thái theo đúng sơ đồ 3.15.
- `core`: đẩy, cầu dao, dọn trên kho và trên máy theo sơ đồ 3.15, đổi đích, khôi phục từ kho, gói panel, kế hoạch dựng lại máy (bảng ca: site thường, tạm khoá, có SSL, trong thùng rác, không có bản trên kho, DB rời).
- `server`: phân quyền, B3, bộ đẩy nền được đánh thức sau job backup.
- `checks`, `health`: hai phép kiểm, và không có chúng khi chưa đặt đích.

**E2E bước 21** (`scripts/e2e-m18.sh`, máy ảo `tpan-m18` từ sạch, log `docs/bang-chung/M18.log`; gọi qua `make e2e-m18`). Mỗi khẳng định ghi lệnh và kết quả mong đợi:

| Phần | Khẳng định |
|---|---|
| A. Đặt đích | versitygw chạy trong máy ảo, khoá sinh tại chỗ. `target set` sai khoá: từ chối `s3_auth_failed`, không file nào được ghi. Đúng khoá: job đạt, có bước thử ghi/đọc/xoá, `s3.json` là root 0600, `target show` chỉ hiện bốn ký tự cuối, khoá sao lưu in đúng một lần |
| B. Đẩy | Sao lưu site A: object có trên kho (file thật trong thư mục của versitygw), `sha256sum` của file đó bằng `backups.sha256`; dòng `remote = 'stored'`; job `backup.push` có bằng chứng đọc lại |
| C. Kho hỏng | `systemctl stop` versitygw: `backup create` thoát 0, dòng mới `local = 1`, `remote = 'failed'`, `tpan health` có cảnh báo `tpan.s3:fresh` và lỗi `tpan.s3:target`. Bật lại, sao lưu lần nữa: cả hai bản đều `stored` (đẩy bù đủ) |
| D. Lỗi giữa lúc tải nhiều phần | Site có file 30 MiB; đích trỏ qua proxy gây 503 từ phần 3: đẩy lỗi, `ListMultipartUploads` rỗng. Bỏ proxy: đẩy đạt |
| E. Object hỏng | Xoá bản cục bộ (dọn bằng `--keep 1` rồi tạo bản mới), ghi đè 1 byte của object: `backup restore --id` dừng với câu nói sha256 không khớp; checksum mọi file của site và bản xuất DB trước và sau giống hệt |
| F. Khôi phục từ kho | Sửa nội dung site, khôi phục bản chỉ còn trên kho: site trả 200 với nội dung cũ; dòng về `local = 1` |
| G. Giữ bản | `s3.keep = 2`, tạo 4 bản: kho còn đúng 2 object của site; bảng `backups` khớp sơ đồ 3.15 |
| H. Quét bí mật | Hai khoá truy cập và khoá sao lưu: 0 lần trong `tpan.db` (cả `-wal`), `journalctl -u tpan`, mọi câu trả lời `GET` của API bằng phiên admin, output của `tpan mcp` khi gọi mọi công cụ đọc, gói panel đã giải mã, user-data đã sinh (cả sau khi giải base64 và gzip) |
| I. Hai máy chủ S3 | Đổi đích sang Garage (vùng `garage`): đặt đích, đẩy, khôi phục từ kho đạt |
| K. Phân quyền | User thường: 403 ở mọi route M18; không thấy bản sao lưu của user khác; khôi phục bản của mình từ kho được |
| L. cloud-init site mới | `render --web nginx --sites` với hai site; máy ảo `tpan-m18-ci` tạo từ đó (file chạy tải từ máy ảo chính qua http, sha256 ghim): `cloud-init status` là `done`, panel trả 200 ở `/api/v1/health`, đăng nhập được bằng mật khẩu `render` đã in, hai site trả 200, `tpan health` đạt. **Ghi thời gian từ lúc tạo máy tới lúc 200.** Sửa một byte của file chạy trên máy phục vụ rồi tạo lại: lần khởi động đầu dừng, `cloud-init status` là `error`, không có `/usr/local/bin/tpan` |
| M. Dựng lại máy | Máy ảo chính: ba site (một thường, một tạm khoá, một có file 30 MiB), `backup create --all`, kho versitygw nghe TLS với chứng chỉ gốc riêng. `render --restore`; xoá máy phụ, tạo lại từ user-data đó; `machine restore --from-s3 --key-stdin --yes`: mọi site trả đúng nội dung cũ, site tạm khoá ra 403, đăng nhập panel bằng mật khẩu admin của máy cũ, `tpan health` không có lỗi ngoài DNS. **Ghi thời gian.** Chạy lệnh lần hai trên máy đã dựng: từ chối `machine_not_empty`. Sai khoá sao lưu: từ chối trước khi đụng máy |
| N. Nâng từ v0.2.0 | Cài gói v0.2.0, hai site, một bản sao lưu, bật lịch; lên bản của nhánh: bản sao lưu cũ khôi phục được, `/etc/cron.d/tpan-backup` không đổi và lệnh trong đó chạy được, `tpan health` trước và sau cùng tập ID phép kiểm, không có job `backup.push` nào |

**Hồi quy**: `scripts/e2e-m9.sh all` và `scripts/e2e-m13.sh` trên bản mới, từ máy sạch (`docs/bang-chung/M18-hoi-quy-m9.log`, `M18-hoi-quy-m13.log`).

**Soát cuối**: một thợ chưa đọc mã mốc này, tìm đường lộ khoá, đường khôi phục nhầm site của người khác, đường chèn nội dung vào user-data, và thử phá trên máy ảo.

## 8. Bảng chia việc

Model theo luật 8 của `agent-dev/LO-TRINH.md` (bản 02:25 ngày 21/11): phần **nhạy cảm** (bí mật, ký và kiểm toàn vẹn, chạy bằng root trên đường dẫn của site, phân quyền, migration trên bảng đang có dữ liệu) do thợ `opus`, `effort: high`; phần không nhạy cảm do thợ `sonnet`; thợ soát `fable`, `effort: max`. Mỗi đợt tối đa 4 thợ, không hai thợ chung file. Phiên chính đọc diff của thợ `sonnet` trước khi nhận.

| Đợt | Phần | Vùng file | Thợ | Loại |
|---|---|---|---|---|
| 0 | Hợp đồng: migration 1800; cột mới trong `site.Backup` và phần đọc của store; `Streamer` trong `System` (kèm cài đặt ở cả hai bản, vì thiếu thì không biên dịch); phần đọc đích và khoá từ settings và file (`Remote.Target`, `TargetID`, dựng `s3.Config`), vì mọi phần sau dựa vào nó; các file `types_m18.go`, `m18_types.go`; thư viện; móc đăng ký lệnh con của `backup` | `internal/store/migrations/1800_s3.sql`, `internal/site/backup.go`, `internal/store/backup.go`, `internal/system/*`, `internal/s3/types.go`, `internal/core/types_m18.go`, `internal/core/remote_target.go`, `internal/steps/m18_types.go`, `internal/server/types_m18.go`, `go.mod`, `go.sum`, `cmd/tpan/backup.go` | phiên chính | |
| 1 | Client S3 và kho giả | `internal/s3/` (trừ `types.go`) | `tho-loi` (opus) | nhạy cảm |
| 1 | Khoá sao lưu, mã hoá theo dòng | `internal/agebox/` | `tho-loi` (opus) | nhạy cảm |
| 1 | Sinh user-data | `internal/cloudinit/` | `tho-loi` (opus) | nhạy cảm |
| 1 | Truy vấn của store cho cột mới; hai phép kiểm; `engine.Note` | `internal/store/remote.go`, `internal/checks/m18.go`, `internal/engine/note.go` (kèm test) | `tho-phu` (sonnet) | không nhạy cảm |
| 2 | Các Step: `S3Probe`, `S3Push`, `S3Pull`, `SecretFile`, `PanelBundle`, `PanelUnpack` | `internal/steps/s3.go`, `internal/steps/panel.go` (kèm test) | `tho-loi` (opus) | nhạy cảm |
| 2 | Ca sử dụng cloud-init và lần khởi động đầu | `internal/core/cloudinit.go`, `cmd/tpan/cloudinit.go` (kèm test) | `tho-loi` (opus) | nhạy cảm |
| 2 | Khung e2e và công cụ thử: script, proxy gây lỗi, dựng versitygw và Garage, máy phục vụ file chạy | `scripts/e2e-m18.sh`, `scripts/e2e-m18/`, target `e2e-m18` trong `Makefile` | `tho-may-ao` (opus) | viết script, chưa chạy máy ảo |
| 3 | Ca sử dụng kho: đích, đẩy, tải, giữ bản trên kho, liệt kê | `internal/core/remote.go` (kèm test) | `tho-loi` (opus) | nhạy cảm |
| 3 | Sửa luồng M9: dọn trên máy, khôi phục từ kho, khôi phục kiểu dựng lại; gói panel | sửa `internal/core/backup.go`; `internal/core/panel.go` (kèm test) | `tho-loi` (opus) | nhạy cảm |
| 4 | API, bộ đẩy nền, nối phép kiểm vào health | `internal/server/api_m18.go`, `pusher.go`, sửa `routes.go`, `api.go`; `internal/health/collect.go`; `internal/store/health.go` (bỏ job đẩy khỏi "job treo") | `tho-loi` (opus) | nhạy cảm |
| 4 | CLI của đích, khoá, đẩy, tải, liệt kê | `cmd/tpan/backup_s3.go` (kèm test) | `tho-loi` (opus) | nhạy cảm (đọc khoá) |
| 4 | Dựng lại máy | `internal/core/machine.go`, `cmd/tpan/machine.go` (kèm test) | `tho-loi` (opus) | nhạy cảm |
| 4 | `list_backups` thêm `location`; dữ liệu demo có đích và bản trên kho | `internal/tools/m9.go`, `internal/demo/m18.go` | `tho-phu` (sonnet) | không nhạy cảm |
| 5 | Chạy e2e bước 21 (A đến N), sửa lỗi gốc, hồi quy M9 và M13 | máy ảo, `docs/bang-chung/M18*.log` | `tho-may-ao` (opus) | |
| 6 | Soát cuối, thử phá trên máy ảo | chỉ đọc mã; ghi `docs/thay-doi/M18-soat-cuoi.md` | thợ soát (fable, max) | |
| 6 | Giải thích | `docs/giai-thich/18-m18-cloud.md` | `tho-viet` (sonnet) | không nhạy cảm |

**Mốc giờ kiểm để cắt phần dựng lại máy**: hết đợt 3 mà `make check` chưa xanh, hoặc quá 13:00 ngày 21/11 mà đợt 4 chưa bắt đầu, thì bỏ dòng "Dựng lại máy" của đợt 4 và phần M của e2e, ghi rõ vào `docs/thay-doi/M18.md`, giữ sáu phần còn lại.

## 9. Kết quả soát thiết kế

Soát ngày 21/11/2026 bởi một thợ `effort: max` chỉ nhận file này. Đã đối chiếu với `agent-dev/TU-CHAY-M18.md`, `agent-dev/LO-TRINH.md`, `docs/KE-HOACH.md` (mục 4, 6.5, 6.10, 6.12, 11), mã ở nhánh `moc/M18` (tới commit 7cf9367) và output ở `docs/bang-chung/M18-nghien-cuu/` (01 đến 14). Không chạy gì trên máy ảo; lệnh duy nhất đã chạy là `go doc` (chỉ đọc) cho `filippo.io/age` v1.3.2 và `crypto/rand`. Có 5 điểm nặng (S1 đến S5), 18 điểm vừa (S6 đến S23), 11 điểm nhẹ (S24 đến S34). Điểm ghi "chưa chắc" có kèm thứ cần thử. Phiên chính ghi cách đã sửa ngay dưới từng điểm.

### S1. Gỡ đích bằng `steps.Absent` đưa cả hai khoá truy cập vào bản xem trước

- **Mức**: nặng (lộ bí mật qua câu trả lời API, trái B3).
- **Ở đâu**: mục 3.3 (Plan `backup.target.rm`), 3.11 (`DELETE /api/v1/backups/target?dry_run=1`), B3.
- **Chuyện gì xảy ra**: máy đã đặt đích. Admin (hoặc màn xác nhận của giao diện) gọi `DELETE /api/v1/backups/target?dry_run=1`, hoặc root gõ `tpan backup target rm --dry-run --json`. Plan có bước `steps.Absent` cho `/etc/tpan/s3.json`. `Absent.Info()` khai `Files: [{Path, Delete: true}]`; `engine.Preview` đọc nội dung hiện có của mọi file trong `Files` vào `FileChange.Old`; bản xem trước ra JSON có trường `old` là base64 của `{"access_key":"…","secret_key":"…"}`. Hai khoá nằm nguyên trong thân trả lời và trên stdout. `server.TestKhongLoKhoaS3` quét chuỗi khoá dạng thô nên không thấy, vì `[]byte` ra JSON là base64.
- **Căn cứ**: `internal/steps/files.go:241-253`; `internal/engine/engine.go:58-75`; `internal/engine/types.go:82-92`; `internal/server/api_ssl.go:51-63`; `cmd/tpan/planned.go:33-40`. Mục 3.2 đã tránh đúng chuyện này cho bước ghi ("không khai `Files`"), còn bước gỡ lại dùng `steps.Absent` nguyên bản.
- **Đề xuất sửa**: thêm `steps.SecretAbsent` (bọc `Absent`, `Info().Files = nil`, cùng kiểu với `setupKeyFile` ở `cmd/tpan/setup.go:278-284`) và dùng nó cho `s3.json`. Test B3 gọi mọi route M18 với `?dry_run=1` và giải base64 các trường `old`, `new` trước khi quét.

### S2. Bản bị dọn trên kho mà còn file trên máy quay lại hàng chờ đẩy: đẩy, dọn, đẩy không dứt

- **Mức**: nặng (máy trạng thái có vòng lặp không thoát; tốn băng thông và tiền chiều ra mãi mãi).
- **Ở đâu**: mục 3.5 (luật chọn bản cần đẩy), 3.7 (dọn trên kho), 3.15 (cạnh "dọn trên kho (còn file trên máy)"), e2e phần G.
- **Chuyện gì xảy ra**: `s3.keep = 2`, `backup.keep = 3`. Site có B2, B3, B4 trên máy, cả ba đã `stored`. Sau khi B4 lên kho, `pruneRemote` xoá object của B2; B2 còn `local = 1` nên về `remote = ''`. Trạng thái đó khớp đúng luật chọn, nên lượt đẩy kế tiếp (bộ đẩy nền, 10 phút một lần) tải B2 lên lại, đọc lại cả object, đánh dấu `stored`, rồi `pruneRemote` lại xoá nó. Vòng này lặp cho tới khi B2 bị dọn khỏi máy, sau đó tới lượt B3. Không có gì hãm: `remote_tries` chỉ tăng khi lỗi. Cùng chuyện với mọi dòng `local = 1` cũ hơn bản thứ `s3.keep` trên kho: đồng hồ máy từng chạy lùi, hoặc dọn trên máy lỗi nên còn nhiều bản hơn `backup.keep`. e2e phần G đặt `s3.keep = 2`; nếu lúc đó `backup.keep` còn là 3 thì khẳng định "kho còn đúng 2 object" lúc đúng lúc sai.
- **Căn cứ**: hai đoạn luật ở 3.5 và 3.7. Mục 3.3 cho `keep` từ 1 tới 365, trong khi `DefaultBackupKeep = 3`, `MaxBackupKeep = 30` (`internal/core/types_m9.go:151-152`); không có ràng buộc nào giữa hai số.
- **Đề xuất sửa**: luật chọn chỉ lấy dòng thuộc `s3.keep` bản đầy đủ mới nhất của domain (tức là bản sẽ sống qua `pruneRemote`); hoặc thêm giá trị `remote = 'pruned'` (đã lên rồi bị dọn theo chính sách; đẩy tự động bỏ qua, `tpan backup push <id>` mới thử lại). Sửa sơ đồ 3.15 và thêm `core.TestDonTrenKhoKhongDayLai`.

### S3. Gói panel luôn chậm hơn các bản của site một nhịp, nên dựng lại máy bỏ bản mới nhất và xoá site mới

- **Mức**: nặng (lệnh dựng lại máy làm sai ngay ở ca chính của mốc).
- **Ở đâu**: mục 3.5 (thứ tự việc trong `Sync`), 3.8, 3.10 pha 2 và pha 3 bước 5, e2e phần M.
- **Chuyện gì xảy ra**: 03:30 cron chạy `backup create --all --scheduled`. Mỗi site có một dòng mới `local=1, remote=''`. `Sync{Panel:true}` theo thứ tự ghi ở 3.5 tạo và đẩy gói panel trước: bản `VACUUM INTO` chụp các dòng đêm nay khi còn `remote=''`. Sau đó mới đẩy các bản của site và ghi `stored` vào DB đang chạy. 11:00 máy mất. `machine restore` lấy gói mới nhất; pha 2 ("mọi dòng khác bị xoá") xoá đúng các dòng đêm nay, dù object của chúng có trên kho, đúng cỡ, và sha256 nằm ngay trong dòng. Mỗi site trở lại từ bản đêm hôm trước. Site tạo trong ngày hôm qua chỉ có một bản: pha 3 bước 5 ghi "không có bản sao lưu trên kho" rồi xoá dòng `sites`. e2e phần M (chạy `backup create --all` một lần rồi dựng lại) sẽ đỏ vì đúng chuyện này. Kể cả khi đảo thứ tự: gói panel chỉ được tạo ở `--all` và `push --panel`, nên bản bấm tay trong ngày và bản do bộ đẩy nền đẩy bù không nằm trong gói nào cho tới đêm sau; và đêm nào kho lỗi lúc 03:30 thì đêm đó không có gói (lượt đẩy bù của bộ đẩy nền có `Panel=false`).
- **Căn cứ**: 3.5, gạch đầu dòng thứ nhất của "Ai gọi đẩy"; 3.10 pha 2; `internal/store/backup.go:149-167` (bản chụp là trạng thái lúc gọi). M9 chép `tpan.db` sau cùng (`internal/core/backup.go:313`), và bản thử `09` chép bản đó, nên chưa lộ ra.
- **Đề xuất sửa**: (1) trong `Sync`, gói panel làm sau cùng; lượt đẩy nào (kể cả của bộ đẩy nền) có ít nhất một dòng vừa thành `stored`, hoặc thấy `s3.panel_error` khác rỗng, thì kết thúc bằng một gói panel. (2) Pha 2 nối theo object thật cho mọi dòng `full`, không chỉ dòng `stored`: có object `sites/<domain>/<tên file của path>` đúng cỡ thì giữ dòng làm nguồn khôi phục (sha256 vẫn kiểm ở `S3Pull`). (3) Pha 3 không xoá dòng `sites` khi dưới `sites/<domain>/` còn object mà DB không biết: báo riêng để người quản trị tự quyết.

### S4. `manifest.schema` là "số migration lớn nhất" nên gói từ bản mới hơn lọt qua pha 1 và làm hỏng ở pha 2

- **Mức**: nặng (không làm được như mục 6 viết; khuôn manifest `format: 1` khó đổi sau khi phát hành).
- **Ở đâu**: mục 3.8 (manifest), 3.10 pha 1 bước 3 và pha 2, mục 6.
- **Chuyện gì xảy ra**: bản X có M16 và M18 (migration tới 18xx). Bản Y là X cộng M17 (thêm 17xx). Máy cũ chạy Y; user-data dựng lại sinh từ trước ghim bản X. Gói có `schema = 18xx`. Pha 1 thấy 18xx không lớn hơn số lớn nhất X biết, cho qua. Pha 2 dừng dịch vụ, đổi tên `tpan.db` của máy, đặt `tpan.db` của gói rồi mở store: `migrate` trả "DB đã áp migration 17xx mà tpan này không có". Lệnh dừng giữa chừng với một `tpan.db` mà file chạy đang cài không mở được; mọi lệnh `tpan` đều lỗi cho tới khi có người cài tay bản mới hơn. Câu "từ chối ở pha 1, chưa đụng gì tới máy" không đúng.
- **Căn cứ**: `internal/store/store.go:161-166` (chú thích nêu đúng lý do không dùng "số lớn nhất"), `201-229`; bảng mốc của `agent-dev/LO-TRINH.md` (M17 dùng 17xx, M18 dùng 18xx, làm song song và vào `dev` không theo thứ tự số).
- **Đề xuất sửa**: manifest ghi `migrations` là cả danh sách của bảng `schema_migrations`; pha 1 từ chối `machine_bundle_newer` khi có số nào bản đang chạy không biết. Thêm vào pha 1 một bước thử: `store.Open` trên bản `tpan.db` đã giải nén trong `/var/lib/tpan/restore/_machine` (migration chạy trên bản sao) trước khi đụng `tpan.db` thật. Test: `core.TestGoiCoMigrationLaTuChoiOPha1`.

### S5. Một phiên admin lấy được hai khoá truy cập qua API: sinh khoá sao lưu mới rồi xin user-data chế độ dựng lại

- **Mức**: nặng (lộ bí mật; trái khung 2 và bất biến 3 của đặc tả về thực chất).
- **Ở đâu**: mục 3.11 (`POST /api/v1/backups/key`, `POST /api/v1/cloud-init/render` với `mode: restore`), 3.9, B3.
- **Chuyện gì xảy ra**: kẻ chiếm được phiên admin (hoặc một admin thứ hai không có root) gọi `POST /api/v1/backups/key`: job trả khoá sao lưu mới K' qua bí mật một lần. Gọi tiếp `POST /api/v1/cloud-init/render` với `{"mode":"restore"}`: `user_data` chứa `restore.credentials`, tức hai khoá truy cập mã hoá bằng đúng K'. Giải mã ngoài máy bằng lệnh `age`: có access key và secret key dạng rõ, đọc, ghi, xoá được mọi thứ trong bucket. Hệ quả thứ hai: người quản trị thật vẫn cầm khoá cũ, mà gói panel từ nay mã hoá bằng K'; mất máy là không mở được gói nào còn trên kho. Test B3 không bắt được vì khoá nằm trong bản mã.
- **Căn cứ**: 3.3 ("Không trường nào mang khoá bí mật, kể cả dạng che"), 3.8 ("API chỉ trả dấu vân tay") mâu thuẫn với hai route trên; `agent-dev/TU-CHAY-M18.md` khung 2 ("không xuất hiện trong ... câu trả lời API") và bất biến 3.
- **Đề xuất sửa**: `render` chế độ dựng lại và `backup key new` chỉ có ở CLI (root), như `machine restore` và `key show --reveal`; API giữ chế độ site mới. Nếu vẫn muốn có trên API thì bỏ hẳn `POST /backups/key`, và `render` chế độ dựng lại đòi người gọi gửi khoá sao lưu hiện tại trong thân. Thêm ca vào `server.TestKhongLoKhoaS3`: không route nào trả bản mã của hai khoá truy cập cho người chưa chứng minh biết khoá sao lưu.

### S6. Lỗi 5xx và 429 của kho bị tính là lỗi "của riêng bản", năm lượt là bỏ hẳn; lỗi của đích thì không có nhịp lùi

- **Mức**: vừa.
- **Ở đâu**: mục 3.4 (`Classify`), 3.5 (`remote_tries`, cầu dao, bộ đẩy nền 10 phút), 3.13.
- **Chuyện gì xảy ra**: (a) nhà cung cấp trả 503 `SlowDown` suốt một giờ. Mỗi request thử 4 lần rồi ra `s3_error`; mã này không nằm trong ba mã "lỗi của đích", nên `remote_tries` tăng và cầu dao không ngắt: mọi bản đang chờ đều bị thử ở mỗi lượt. Sau 5 lượt (khoảng 50 phút) mọi dòng đạt `remote_tries = 5` và bị đẩy tự động bỏ qua vĩnh viễn, kể cả khi kho đã khoẻ lại: trái "lần sau đẩy bù" của khung 3. (b) khoá truy cập bị thu hồi (403). Cầu dao ngắt đúng, nhưng 10 phút sau bộ đẩy nền lại tạo một job `backup.push` hỏng: 144 job hỏng mỗi ngày trong Lịch sử và `job_events` cho tới khi có người sửa.
- **Căn cứ**: 3.4 ("`s3_error` (còn lại)"), 3.5 đoạn về `remote_tries` và cầu dao (chỉ ba mã); `07-file-2gb.out` bước 4 (503 sau 4 lần thử là lỗi thường gặp).
- **Đề xuất sửa**: 5xx, 429 và hết giờ sau khi đã thử lại là lỗi của đích (không tăng đếm, ngắt cầu dao). `remote_tries` chỉ tăng cho: file cục bộ sai cỡ hoặc sai sha256, đọc lại không khớp, 4xx khác 401, 403, 404, 429. Lỗi của đích: ghi `s3.target_error` và giờ được thử lại (10 phút, 30 phút, 1 giờ, rồi 6 giờ); đầu mỗi lượt gọi `Check` một lần, hỏng thì không tạo job nào. Đẩy được một bản thì xoá nhịp lùi. Ghi rõ `tpan backup push` không có id có thử lại dòng `remote_tries ≥ 5` hay không (`tpan.s3:fresh` đang chỉ đúng lệnh này làm cách sửa).

### S7. `Sync{Panel:true}` của lịch hằng đêm bị bỏ qua khi bộ đẩy nền đang giữ khoá lượt đẩy

- **Mức**: vừa.
- **Ở đâu**: mục 3.5 (`flock` không chờ; ba nơi gọi `Sync`), 3.8.
- **Chuyện gì xảy ra**: máy có 40 site, phần sao lưu cục bộ mất 25 phút. 03:40 bộ đẩy nền (nhịp 10 phút) thấy các dòng lịch vừa tạo, lấy `/run/tpan/remote.lock` và bắt đầu đẩy. 03:55 CLI của cron xong phần cục bộ, gọi `Sync{Panel:true}`: khoá bận, hàm trả "đang có lượt đẩy khác" (không phải lỗi), lệnh thoát 0. Các bản của site vẫn lên kho nhờ bộ đẩy nền, nhưng đêm đó không có gói panel; máy đủ lớn thì đêm nào cũng vậy, và `tpan.s3:fresh` chỉ cảnh báo sau 48 giờ. Tương tự, `tpan backup push <id>` hay `tpan backup pull <id>` gõ tay lúc bộ đẩy nền đang chạy sẽ thoát ngay mà không làm gì.
- **Căn cứ**: 3.5, hai gạch đầu dòng đầu của "Ai gọi đẩy"; 3.8 gạch đầu dòng cuối.
- **Đề xuất sửa**: lệnh do người hoặc cron gõ thì chờ khoá lượt đẩy (thử lại có nghỉ như `LocalSystem.Lock`, `internal/system/local.go:423-447`, in "đang chờ lượt đẩy khác"); chỉ bộ đẩy nền mới bỏ qua khi bận. Hoặc `Panel` thành cờ bền trong settings (`s3.panel_due`) mà lượt đẩy nào cũng xử lý ở cuối (hợp với đề xuất 1 của S3). `backup push`, `backup pull` không làm được gì thì mã thoát khác 0.

### S8. `S3Pull`: file đích còn sót thì kẹt hẳn; hai lượt tải cùng một bản giẫm `.part` của nhau; dọn trên kho chen vào giữa lúc khôi phục

- **Mức**: vừa.
- **Ở đâu**: mục 3.6 (bước 2, 6 và `Check` của `S3Pull`; Plan `backup.pull`), 3.7, B10.
- **Chuyện gì xảy ra**: (a) Kẹt. Job tải bị giết sau `Rename` và trước `after`; hoặc `after` lỗi khi ghi DB; hoặc `Undo` của `S3Pull` lỗi. Dòng vẫn `local = 0` mà `Dest` đã có. Mọi lần tải hay khôi phục sau đều dừng ở "`Dest` đã có: lỗi"; không lệnh nào của TPAN gỡ được, phải `rm` tay trong `/var/lib/tpan/backups`. (b) Đua. Job khôi phục của user (giữ khoá job của máy) và `tpan backup pull <id>` của admin (giữ khoá lượt đẩy) chạy cùng lúc trên cùng id. Lượt sau "xoá `.part` còn sót" đúng lúc lượt trước đang ghi vào nó, rồi tạo `.part` mới; lượt trước tải xong, kiểm sha256 trên dòng dữ liệu của mình rồi `Rename` cái tên `.part`, giờ là file dở của lượt sau, thành `Dest`. Tuỳ thời điểm, `BackupExtract` thấy sha256 lệch (job khôi phục lỗi với câu "backup hỏng"), hoặc lượt sau lỗi ở `Rename` vì `.part` đã mất. Site không hỏng nhờ lần kiểm thứ hai của M9. (c) Dọn chen vào. User khôi phục bản cũ nhất trên kho; `S3Pull` xong; trong lúc các bước sau chạy, một lượt đẩy của cùng domain xong và `pruneRemote` xoá object rồi xoá dòng (vẫn `local = 0`, vì `SetBackupLocal` chỉ chạy ở `after`). Job khôi phục thành công, `SetBackupLocal` không trúng dòng nào: file vài GB nằm lại mà không dòng nào trỏ tới.
- **Căn cứ**: 3.6 ("`Dest` đã có: lỗi", "`Check`: đã `Apply` và `Dest` còn"); `internal/engine/types.go:52-54` (Check trả true khi máy đã ở trạng thái mong muốn, kể cả không do tpan tạo); `internal/core/types_m12.go:249-254` (`after` chạy ngoài khoá, sau khi job đã xong).
- **Đề xuất sửa**: `Check` theo trạng thái: `Dest` là file thường, đúng cỡ, `sha256sum` khớp thì coi là xong (bước được bỏ qua, `after` đặt `local = 1`); có mà sai thì xoá rồi tải lại (thư mục chỉ root ghi). Tên tạm riêng cho mỗi lượt (`<Dest>.part.<16 hex>`); `backup.pull` và bước `S3Pull` trong job khôi phục cùng lấy một `flock` theo file đích, chỉ dọn `.part.*` khi đang giữ khoá đó. `pruneRemote` bỏ qua dòng đang bị giữ khoá ấy. Mọi câu đổi trạng thái là một lệnh SQL có điều kiện (`DELETE … WHERE id = ? AND local = 0`; `UPDATE … SET remote = '' WHERE id = ? AND local = 1`).

### S9. Khôi phục từ kho giữ khoá job của máy suốt thời gian tải về

- **Mức**: vừa.
- **Ở đâu**: mục 3.6, B10 (câu trong ngoặc), lập luận ở 3.5.
- **Chuyện gì xảy ra**: user thường bấm khôi phục một bản 20 GB chỉ còn trên kho, đường truyền 10 Mbit/s. `S3Pull` chạy khoảng 4,5 giờ dưới khoá job. Trong lúc đó mọi thao tác có thay đổi qua API trả 409 `job_running`, lịch backup 03:30 đứng chờ khoá, và health bỏ mọi vòng kiểm: đúng điều 3.5 nêu làm lý do không cho job đẩy giữ khoá. Client không có hạn cho cả thao tác nên kho chậm thì thời gian này không có trần. Route là `owner:backup`, tức là user thường kích hoạt được.
- **Căn cứ**: `internal/server/api_job.go:43-56`; `internal/server/api_health.go:58-68`; `internal/health/health.go:351-354`; `internal/system/local.go:423-447` (CLI chờ khoá không hạn).
- **Đề xuất sửa**: khôi phục một bản `local = 0` thành hai job nối nhau: `backup.pull` (khoá lượt đẩy, không giữ khoá máy), rồi `backup.restore` đúng như M9 trên bản đã về máy. API trả id của job tải; giao diện theo job khôi phục khi nó bắt đầu. B10 khi đó không còn ngoại lệ.

### S10. B5 nói user thường không xem được đích, nhưng job đẩy của site họ phơi endpoint, bucket, tiền tố và câu lỗi của kho

- **Mức**: vừa.
- **Ở đâu**: mục 3.5 (Plan `backup.push`, "target: domain"), 3.6, 3.11, B5.
- **Chuyện gì xảy ra**: job `backup.push` có `Target` là domain nên `CreateJob` gán chủ job là chủ của site. User mở Lịch sử, `GET /api/v1/jobs/{id}`: thấy mọi sự kiện của job, gồm `step.fail` dạng `Put "https://<endpoint>/<bucket>/<tiền tố>/sites/…?partNumber=…&uploadId=…": dial tcp …` và "S3 trả 403 …". Job khôi phục từ kho do chính user chạy cũng mang lỗi của `S3Pull`. `GET /api/v1/backups` giấu `remote_error` với user, còn log job thì không.
- **Căn cứ**: `internal/store/store.go:302-318`; `internal/server/routes.go:107-109`; `internal/server/api.go:300-339`; `07-file-2gb.out` bước 3 và `14-tls-ca-rieng.out` (dạng câu lỗi của `net/http`, có đủ host, bucket, tiền tố).
- **Đề xuất sửa**: job `backup.push`, `backup.pull`, `backup.panel` ghi chủ là `admin` (`engine.WithOwner`). Bước `S3Pull` nằm trong job khôi phục của user thì lỗi của bước chỉ nêu mã (`s3_unreachable`, …) và id bản; chi tiết ghi vào `remote_error`. Hoặc sửa B5 nói rõ endpoint, bucket, tiền tố không kín với chủ site. `server.TestUserKhongDungDuocS3` quét cả `GET /jobs` và `GET /jobs/{id}` của user.

### S11. Câu lỗi do kho trả có thể chép lại access key (chưa chắc)

- **Mức**: vừa.
- **Ở đâu**: mục 3.4 (gạch đầu dòng "Lỗi" và "Bí mật"), 3.2 (`remote_error`), 3.13, B3.
- **Chuyện gì xảy ra**: khoá của Garage bị xoá phía kho. Lượt đẩy đêm nhận 403 với `<Message>` chép lại chính access key vừa gửi (thợ soát nhớ mã nguồn Garage trả `Forbidden: No such key: <key id>`, chưa kiểm lại). `s3.Error.Message` chỉ được bỏ ký tự điều khiển và cắt 300 ký tự, nên access key đầy đủ đi vào `backups.remote_error`, `jobs.error`, `job_events`, bằng chứng của `tpan.s3:target`, câu trả lời `GET /backups` và `get_job` của MCP. Cùng đường với kho hay proxy trả thân lỗi không phải XML, nếu `Message` lấy từ đầu thân như bản tham chiếu đang làm.
- **Căn cứ**: `ban-thu-s3/main.go:199-202` (thân không phải XML: 200 byte đầu thành `Message`); `14-tls-ca-rieng.out` bước 4 (thân lỗi dạng chữ, có xuống dòng, đi thẳng vào câu lỗi); `05-thu-co-ban-garage.out` bước 3 (Garage chép lại phần scope của header `Authorization`). Chưa chắc ở chỗ: bản thử 05 chỉ thử sai secret, chưa thử sai access key. Cần thử: chạy `s3thu test` với access key không có, trên Garage và versitygw, đọc `<Message>`.
- **Đề xuất sửa**: trước khi dựng `s3.Error`, thay mọi lần xuất hiện của access key, secret key và chữ ký vừa gửi trong `Code`, `Message` bằng `••••`; thân không phải XML thì `Message` rỗng. `s3.TestLoiKhongCoKhoa` dùng máy chủ giả chép lại header `Authorization` vào `<Message>`.

### S12. sha256 ghim trong user-data lấy từ chính nơi đặt file chạy, có thể qua `http://`

- **Mức**: vừa.
- **Ở đâu**: mục 3.9 (`--base`, `--sums`), mục 5 (hàng "Người sửa được nơi đặt file chạy"), 3.11 (`CloudInitRequest`).
- **Chuyện gì xảy ra**: `tpan cloud-init render --web nginx --base http://mirror.example/tpan`, không có `--sums`. `render` tải `http://mirror.example/tpan/SHA256SUMS` rồi ghim. Kẻ đứng giữa (hoặc người sửa được mirror) trả một `SHA256SUMS` khớp với file chạy của họ; user-data ghim đúng mã đó; máy mới kiểm "đạt" rồi chạy file lạ bằng root. Câu "toàn vẹn do sha256 ghim bảo đảm, không do kênh truyền" chỉ đúng từ lúc sinh trở đi, không đúng cho chính lần lấy mã. Qua API: thân không có hai mã thì server có tự `curl -L <base>/SHA256SUMS` hay không (một đường để phiên admin bắt server gọi URL tuỳ ý) là chỗ chưa viết.
- **Căn cứ**: 3.9 gạch đầu dòng thứ nhất và thứ hai; script ở 3.9 chỉ so với mã đã ghim.
- **Đề xuất sửa**: `--base` là `http://` thì bắt buộc `--sums FILE`; chỉ tự tải `SHA256SUMS` khi `https://`. API: `sha256_amd64`, `sha256_arm64` bắt buộc khi `base` khác mặc định; server không tải gì từ `base` do người gọi đưa. Sửa hàng ở mục 5: chặn được việc đổi file sau lúc sinh, không chặn được nguồn đã bị sửa từ trước.

### S13. Script khởi động đầu chạy lại sau khi máy đã `self-update` sẽ hạ file chạy về bản đã ghim

- **Mức**: vừa.
- **Ở đâu**: mục 3.9 (script và đoạn "Chạy lại … an toàn"), mục 6.
- **Chuyện gì xảy ra**: máy tạo bằng user-data ghim bản X; sau đó `tpan self-update` lên Y (có migration mới). Người quản trị chạy lại theo đúng lệnh 3.9 nêu (`cloud-init single --name scripts_user --frequency always`), hoặc tạo máy từ snapshot với cùng user-data (instance-id mới nên cloud-init chạy lại `runcmd`). Script thấy sha256 của `/usr/local/bin/tpan` khác mã ghim, tải X về và `mv -f` đè lên Y. `tpan cloud-init run` (bản X) không mở được `tpan.db`. Từ lần khởi động lại dịch vụ kế tiếp panel không lên; `tpan cron run` của mọi site và lịch backup đều lỗi vì cùng lý do. Ở chế độ site mới còn thêm: `apply` chạy lại `sites.yaml` cũ, tạo lại site người quản trị đã xoá hẳn.
- **Căn cứ**: nhánh `if ! { [ -x "$bin" ] && … sha256sum -c …; }` của script; `08-cloud-init-thu.out` (module `runcmd` chạy theo `once-per-instance`); `internal/store/store.go:211-229`.
- **Đề xuất sửa**: đầu script: đã có `/var/lib/tpan/first-boot.json` thì không đụng file chạy, chỉ `exec "$bin" cloud-init run`; `cloud-init run` thấy `ok: true` thì in "đã xong từ <giờ>" và thoát 0, muốn làm lại phải `--force`. Chỉ tải khi chưa có `$bin`. Thêm khẳng định e2e: đổi file chạy (giả một lần nâng bản) rồi chạy lại script, file chạy không đổi.

### S14. `PrepareRebuild` là đường chưa thử: `DBPassword` từ chối nhiều `wp-config.php` thật, và hoàn tác của nó làm lần chạy lại kẹt

- **Mức**: vừa.
- **Ở đâu**: mục 3.10 pha 3 bước 5c, 2.6 ("không cần sửa Plan nào"), mục 8.
- **Chuyện gì xảy ra**: (a) Bản thử `09` dùng `tpan backup restore` nguyên bản, tức là giữ `wp-config.php` của site vừa tạo. Thiết kế đổi sang giữ file của bản sao lưu rồi chạy `DBPassword`. Bước này từ chối khi `DB_PASSWORD` không phải 32 ký tự chữ số do tpan sinh, khi có hơn một dòng `define` cho `DB_NAME`, `DB_USER`, `DB_PASSWORD`, hoặc khi tên DB khác. Site mà chủ đã tự đổi mật khẩu DB hay sửa tay `wp-config.php` (đúng loại site mà 5c muốn giữ tuỳ chỉnh) không dựng lại được. (b) `DBPassword` đã chạy rồi bước sau (`WPSiteURL`, kiểm chứng) lỗi: `Undo` đặt mật khẩu user DB về mật khẩu của máy cũ (đọc từ file trong bản sao lưu), rồi engine tráo `public` giàn giáo về, mà `wp-config.php` của nó mang mật khẩu `site add` vừa sinh. User DB và file lệch nhau. Chạy lại `machine restore`: `SiteDB` và `WPConfig` thấy "đã có" nên bỏ qua; `wp core is-installed` lỗi kết nối DB; `wp core install` lỗi; job hoàn tác. Site đó kẹt ở 5b mãi. (c) "`BackupExtract` giữ `wp-config.php` của bản sao lưu" đòi sửa `internal/steps/backup.go`, file không thuộc vùng của thợ nào ở mục 8.
- **Căn cứ**: `internal/steps/db_m9.go:129-166, 218-237`; `internal/steps/site_db.go:218-224`; `internal/steps/site_wp.go:456-468`; `internal/core/site.go:592-610`; `internal/steps/backup.go:810-833`; `09-dung-lai-may-thu.sh` bước 6.
- **Đề xuất sửa**: sau khi tráo và nhập DB, không đổi mật khẩu user DB. Đọc mật khẩu của giàn giáo bằng `currentWPConfig` trước khi tráo (đã có ba lớp chặn), rồi ghi nó vào `wp-config.php` của bản sao lưu bằng `wp config set DB_PASSWORD --prompt` dưới user của site; hoàn tác là ghi lại giá trị cũ của file, user DB không đổi. File của bản sao lưu không đạt điều kiện đọc thì dùng file giàn giáo như M9 và ghi vào báo cáo "tuỳ chỉnh wp-config.php chưa mang sang". Trước đợt 3: thử tay đúng chuỗi này trên `tpan-m18` với một site đã `tpan db passwd` và một site có `wp-config.php` sửa tay. Giao `internal/steps/backup.go` cho một thợ.

### S15. Site đang tạm khoá mở công khai trong lúc dựng, và mở mãi nếu bước khoá lại không chạy

- **Mức**: vừa.
- **Ở đâu**: mục 3.10 pha 3 bước 5a đến 5d, B15.
- **Chuyện gì xảy ra**: site bị khoá vì phát tán mã độc. 5a xoá cờ `suspended` trong `tpan.db` (cờ gốc chỉ còn trong `machine-restore.json`). 5b, 5c dựng site và site phục vụ bình thường. 5d lỗi (kiểm chứng của `PrepareSuspend` không đạt), hoặc tiến trình bị giết trước 5d. Site tiếp tục phục vụ nội dung cũ; panel hiện nó là site thường; không phép kiểm nào báo. Người quản trị không chạy lại lệnh thì site mở vĩnh viễn. B15 chỉ khẳng định cho ca "kết thúc thành công cho site đó", nên `core.TestDungLaiSiteTamKhoa` không chạm tới ca này.
- **Căn cứ**: 3.10 bước 5a ("Lưu dòng site với ba cờ đó xoá"), 5e; `internal/core/site.go:406-412`.
- **Đề xuất sửa**: không xoá cờ trong DB. Dựng site với vhost tạm khoá ngay từ đầu (qua `core.siteVhost`) và kiểm chứng bằng bộ phép kiểm của site tạm khoá sau khi nhập DB; hoặc một cờ "đang dựng lại" riêng mà chỉ lệnh này được dùng để vượt lời từ chối `site_suspended`. Tối thiểu: ghi danh sách site chờ khoá lại vào settings, thêm phép kiểm `tpan.restore:pending` trả Lỗi khi danh sách còn, và B15 đổi thành "lệnh không bao giờ kết thúc (kể cả lỗi) mà một site vốn khoá đang mở và không có sự cố nào mở cho nó", kèm test giết giữa 5c và 5d.

### S16. `tpan.db` cũ mang theo `managed_files`, job `running` và kết quả health của máy đã mất

- **Mức**: vừa.
- **Ở đâu**: mục 3.10 pha 2 (những thứ được dọn), pha 3 bước 7, e2e phần M ("`tpan health` không có lỗi ngoài DNS").
- **Chuyện gì xảy ra**: máy cũ có site SSL. `managed_files` trong gói có dòng cho hook gia hạn `/etc/letsencrypt/renewal-hooks/deploy/tpan` và các file của Plan SSL; tương tự với file do các Plan ufw, fail2ban, tự vá ghi (chưa mở từng Plan để đếm). Máy mới không sinh lại các file này: SSL và tường lửa nằm ở "việc phải tự làm". Health cuối lệnh: `checks.Drift` trả Lỗi "File … không còn" cho từng file và mở sự cố. Ngoài ra bản `VACUUM INTO` được chụp ngay trong job `backup.panel`, nên chính job đó nằm trong gói ở trạng thái `running` mãi mãi; job API nào đang chạy lúc 03:30 thì sau một giờ trên máy mới thành "job treo". `check_results` và sự cố đang mở của máy cũ cũng theo sang.
- **Căn cứ**: `internal/checks/health.go:58-66`; `internal/engine/engine.go:193-208`; `internal/core/ssl.go:321-327`; `internal/health/collect.go:84-100, 151-161`.
- **Đề xuất sửa**: giao dịch của pha 2 làm thêm: xoá hết `managed_files` (từng Plan ở pha 3 ghi lại file nó sinh), xoá `check_results`, đóng mọi sự cố đang mở với lý do "máy đã dựng lại", đặt mọi job `running` về một trạng thái kết thúc kèm câu "đứt vì máy mất". e2e phần M bật ufw trên máy cũ (hoặc có một site SSL bằng chứng chỉ thử) rồi khẳng định health không có phép kiểm `drift:` nào lỗi.

### S17. Không có gì ngăn hai máy cùng ghi vào một tiền tố

- **Mức**: vừa.
- **Ở đâu**: mục 3.3 (tiền tố mặc định theo tên máy), 3.7 (gói panel dọn theo danh sách trên kho), 3.10 pha 3 bước 1 và 6, e2e phần M.
- **Chuyện gì xảy ra**: diễn tập. Máy thật A vẫn chạy; người quản trị tạo máy B từ user-data dựng lại và chạy `machine restore` để biết bản sao lưu dùng được. B đặt cùng đích, cùng khoá và ghi lại cron backup. Đêm đó cả hai chạy lúc 03:30. (1) Tên file chỉ có độ phân giải giây, nên bản đầu tiên của hai máy dễ có cùng khoá object `sites/<domain>/<domain>-<giờ>.tar.gz`; máy ghi sau đè lên, dòng `stored` của máy kia trỏ tới nội dung khác và chỉ lộ ra lúc khôi phục. (2) Gói panel của hai máy chen nhau trong `panel/`; mỗi máy dọn theo tên nên xoá gói của máy kia. (3) Sau buổi diễn tập người quản trị xoá site trên B; gói mới nhất trên kho là của B và không còn site nào; A mất, `machine restore` mặc định lấy đúng gói đó. Chính e2e phần M để hai máy ảo cùng trỏ một tiền tố. Tiền tố mặc định `tpan/<tên máy>` cũng trùng nhau giữa các máy tạo từ cùng một mẫu.
- **Căn cứ**: 3.10 bước 1 ("từ đây máy mới tự sao lưu tiếp vào cùng chỗ, cùng khoá"); 3.7 gạch đầu dòng cuối; `internal/steps/backup.go:358-362`.
- **Đề xuất sửa**: object `owner.json` ở gốc tiền tố, nội dung `{machine_id, hostname, taken_at}` (id ngẫu nhiên sinh lúc `target set`, lưu ở settings và trong manifest của gói). `target set` thấy `owner.json` của máy khác thì từ chối trừ khi có `--take-over`. Mỗi lượt `Sync` đọc nó trước; khác id thì dừng với lỗi của đích và `tpan.s3:target` báo "đích đang do máy <tên> dùng". `machine restore` mặc định không nhận quyền ghi vào đích và không bật lại lịch (in hai lệnh cần gõ khi đã chắc máy cũ không còn); có `--take-over` mới ghi `owner.json` mới. Bản xem trước của `machine restore` in `hostname` của gói.

### S18. Đổi cách viết endpoint trên máy đang chạy làm các bản chỉ còn trên kho thành "đích khác"

- **Mức**: vừa.
- **Ở đâu**: mục 3.3 (`TargetID`), 3.5 (`MarkBackupPushFailed`), 3.6, 3.7; 3.10 pha 2 đã xử lý đúng cho ca dựng lại máy.
- **Chuyện gì xảy ra**: đích đặt lần đầu là `https://s3.eu.example`. Nhà cung cấp đổi tên miền, hoặc người quản trị sửa sang tên nội bộ của cùng kho. `TargetID` đổi: mọi dòng `local=0, stored` thành "kho (đích khác)", khôi phục bị từ chối `backup_not_on_machine` dù object nằm nguyên chỗ cũ; `pruneRemote` không bao giờ đụng tới chúng nữa nên cả object lẫn dòng nằm lại vĩnh viễn; mọi bản còn trên máy bị tải lên lại. Thêm: một bản đang `stored` ở đích cũ mà đẩy sang đích mới lỗi thì `MarkBackupPushFailed` ghi `remote = 'failed'`, làm mất dấu rằng đích cũ đang có bản đó; lần dọn trên máy kế tiếp xoá luôn dòng.
- **Căn cứ**: 3.3 đoạn "Mã của đích"; 3.10 pha 2 ("nối lại theo object thật, không theo mã đích cũ").
- **Đề xuất sửa**: `backup.target.set` thấy `TargetID` đổi thì chạy đúng phép nối của pha 2 như một bước của Plan: `List("sites/")`; dòng `stored` có `remote_key` tồn tại đúng cỡ thì đổi `remote_target` sang mã mới. Tiêu đề bước nêu số dòng nối được và số dòng thật sự ở đích khác. Đẩy lỗi sang đích mới không ghi đè trạng thái `stored` của đích cũ (ghi lỗi vào `remote_error` và giữ nguyên ba cột kia).

### S19. `Get` thử lại sau khi đã ghi một phần thân; hạn 60 giây chờ header ở bước ghép

- **Mức**: vừa.
- **Ở đâu**: mục 3.4 (chữ ký `Get(ctx, key, w io.Writer)`, "Thử lại", "Hạn giờ"), 3.5 bước 5, 3.6.
- **Chuyện gì xảy ra**: tải về 8 GB, mạng đứt 3 phút ở GB thứ 5. "120 giây không có byte nào thì huỷ lần thử đó", rồi "mỗi request tối đa 4 lần": thợ viết theo chữ sẽ gọi lại GET và ghi tiếp vào cùng `w`. File thành 5 GB cũ nối 8 GB mới, sha256 lệch, `S3Pull` báo "bản trên kho không khớp sha256 ghi lúc tạo": đổ lỗi sai cho kho, và `remote_tries` tăng oan ở bước đọc lại của `S3Push`. Nếu thợ chọn không thử lại sau byte đầu thì mỗi lần đứt là tải lại từ đầu. Cả hai cách đều chưa được viết ra. Chưa chắc: bước ghép của một kho lưu trên đĩa quay với object vài chục GB có thể im quá 60 giây trước khi trả header (bản thử mới đo 2 GiB trên vòng lặp nội bộ); khi đó lần thử lại nhận `NoSuchUpload` dù object đã ghép xong, và bản đó không bao giờ đẩy được.
- **Căn cứ**: 3.4; `07-file-2gb.out` (chỉ thử đứt lúc tải lên, chưa thử đứt lúc tải về).
- **Đề xuất sửa**: ghi rõ: sau byte thân đầu tiên, `Get` nối tiếp bằng `Range: bytes=<đã nhận>-` kèm `If-Match: <ETag của lần đầu>`, tối đa 4 lần nối, bộ băm chạy tiếp trên dòng nối; kho trả 200 thay vì 206 thì coi là lỗi mạng và không ghi thêm byte nào. Hạn chờ header riêng cho bước ghép (ví dụ 10 phút); bước ghép lỗi mạng hoặc `NoSuchUpload` thì `Head` object: có và đúng cỡ thì coi là đã ghép, chuyển sang đọc lại. Đợt 1 thử thêm: file 2 GiB qua proxy cắt kết nối giữa lúc tải về.

### S20. Chạy lại `machine restore` sau khi hỏng giữa pha 2 bỏ qua phần dọn DB; vòng đời của file trạng thái chưa rõ

- **Mức**: vừa.
- **Ở đâu**: mục 3.10 pha 2, đoạn "Chạy lại sau khi hỏng giữa chừng", e2e phần M.
- **Chuyện gì xảy ra**: tiến trình chết sau khi đặt `tpan.db` của gói và ghi file trạng thái, trước khi giao dịch dọn (xoá `sessions`, nối `backups`, xoá `databases`) được commit. Chạy lại: "có file trạng thái và `tpan.db` đã nhận thì bỏ pha 1 và 2". DB giữ nguyên các phiên đăng nhập của máy cũ (cookie cũ đăng nhập được máy mới); dòng `backups` còn `local = 1` trỏ tới file không có, nên dựng site nào cũng dừng ở `backup_corrupt`. Ba chỗ nữa: (a) "xoá `-wal`, `-shm`" khi đổi tên DB đang dùng làm bản `tpan.db.before-machine-restore` thiếu các giao dịch chưa gộp nếu store chưa được đóng trước. (b) Sau một lần chạy thành công, e2e phần M đòi lần hai bị từ chối `machine_not_empty`, trong khi luật chạy lại nói "làm tiếp các việc chưa `done`": chưa nói file trạng thái kết thúc thế nào. (c) Khoá sao lưu đã đổi sau lúc sinh user-data thì `restore.credentials` không mở được bằng khoá mở được gói: phải hỏi hai khoá truy cập, không phải báo `backup_key_wrong`.
- **Căn cứ**: 3.10 pha 2 ("Ghi file trạng thái … trước mỗi việc"); `internal/core/backup.go:405-431`; `internal/store/store.go:118-124`.
- **Đề xuất sửa**: file trạng thái ghi từng mốc đã xong (`db_placed`, `db_cleaned`, `target_set`, …). Giao dịch dọn viết cho chạy lại được và luôn chạy khi chưa có mốc `db_cleaned`. Đóng store (gộp WAL) rồi mới đổi tên. Thành công thì đổi tên file trạng thái thành `machine-restore.done.json`; lần gõ sau không thấy file đang dở mà máy có site thì `machine_not_empty`. Ghi thứ tự các dòng stdin khi dùng cả `--key-stdin` và `--keys-stdin`.

### S21. "Bản đầy đủ mới nhất" tính cả bản `before-restore`

- **Mức**: vừa.
- **Ở đâu**: mục 3.10 pha 3 bước 5c; 3.13 phần (b); 3.7.
- **Chuyện gì xảy ra**: 10:00 người quản trị khôi phục site về bản ba ngày trước vì một plugin mới làm hỏng site. Job tạo bản `before-restore` (trạng thái hỏng lúc 10:00) và bộ đẩy nền đưa nó lên kho. Đêm đó gói panel ghi nhận nó; nếu lần sao lưu theo lịch của site lỗi (đĩa đầy), hoặc máy mất trước đêm và S3 đã được sửa theo đề xuất 1, thì bản đầy đủ mới nhất trên kho của site chính là bản `before-restore`. Dựng lại máy đưa site về đúng trạng thái người quản trị vừa bỏ. Phép kiểm (b) cũng coi bản này là "đã có bản trên kho trong 48 giờ", và nó chiếm một chỗ trong `s3.keep`.
- **Căn cứ**: `internal/core/backup.go:378-387` (lấy dòng `full` đầu tiên, không xét lý do); `internal/site/backup.go:66-75`; 3.5 (mọi bản `full` đều được đẩy).
- **Đề xuất sửa**: dựng lại máy chọn bản mới nhất có lý do `manual` hoặc `schedule`; không có mới lấy lý do khác. Bản xem trước của pha 1 in cho từng site giờ và lý do của bản sẽ dùng. Phép kiểm (b) chỉ đếm `manual`, `schedule`.

### S22. Bảng chia việc thiếu file, và vài chỗ hai mục nói khác nhau

- **Mức**: vừa.
- **Ở đâu**: mục 8 so với 3.1, 3.5, 3.8, 3.11, 3.12, 3.14; B11.
- **Chuyện gì xảy ra**: thợ đợt 3 và 4 làm đúng vùng file của mình thì thiếu các chỗ sau, hoặc phải sửa file ngoài vùng. (1) `cmd/tpan/backup.go`: `backup create` phải gọi `Sync` và in "CHƯA lên kho", `--all` gọi `Sync{Panel:true}`, `backup list` thêm cột và `--remote`. Bảng chỉ giao file này ở đợt 0 để "móc đăng ký lệnh con"; 3.1 ghi "sửa một dòng". (2) Khoá sao lưu trả qua "bí mật một lần của job" cần trường mới trong `JobDetail`, `heldSecret`, `jobResult`, và chỗ đánh thức bộ đẩy nền sau mỗi job nằm trong `jobStart`: `internal/server/types.go` và `internal/server/api_job.go` không có trong vùng của đợt 4; tên trường JSON chưa đặt. (3) `GET /api/v1/backups` phải giấu `remote_key`, `remote_error` với user: `internal/server/api_m9.go` không được giao. (4) `internal/steps/backup.go` (S14). (5) B11 nói output của `backup list` không đổi khi chưa đặt đích; 3.12 thêm cột "Ở ĐÂU" vô điều kiện, và `--json` in thêm năm trường. (6) `backup_key_invalid` nằm trong nhóm mã 400 của API nhưng không route nào nhận khoá sao lưu; `machine_bundle_invalid` có ở 3.14 mà 3.10 không nói ca nào trả nó.
- **Căn cứ**: `cmd/tpan/backup.go:30, 38-87, 104-219, 245-296`; `internal/server/types.go:211-224`; `internal/server/api_job.go:67-74, 117-121, 173-211`; `internal/server/api_m9.go:40-63`.
- **Đề xuất sửa**: thêm các file trên vào đúng dòng của mục 8, hoặc phiên chính làm phần hợp đồng ở đợt 0 (trường `backup_key` trong `JobDetail`, hàm đánh thức bộ đẩy nền). Sửa "một dòng" ở 3.1. B11 ghi rõ cột và trường mới chỉ hiện khi đã đặt đích hoặc có dòng `remote != ''`. Bỏ hoặc gán ca cho hai mã ở (6).

### S23. Luật `key` của client cho qua đoạn `.` và `..`; luật cho tham số `prefix` chưa có

- **Mức**: vừa (B7 không được bảo đảm bởi luật đã viết).
- **Ở đâu**: mục 3.4 (gạch đầu dòng thứ nhất), 3.10 (`--bundle <tên>`), B7.
- **Chuyện gì xảy ra**: root gõ `tpan machine restore --from-s3 --bundle ../../may-khac/panel/tpan-20261120-033000.tar.gz.age`, hoặc một `remote_key` trong DB bị sửa. Chuỗi khớp `^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$` vì `..` chỉ gồm ký tự được phép. Với kho lưu object thành file (versitygw `posix`, đúng máy chủ thử chính) hoặc có proxy chuẩn hoá đường dẫn đứng trước, request ra ngoài tiền tố của đích. Luật của `prefix` ở 3.3 đã cấm hai đoạn này, luật của `key` thì chưa. Ngoài ra các lời gọi `List("")`, `List("panel/")`, `List("sites/")` đưa vào chuỗi rỗng hoặc có `/` cuối, không khớp luật của `key`: luật cho tham số này chưa được viết.
- **Căn cứ**: 3.4 so với hàng `prefix` của bảng ở 3.3; 3.6, 3.10 (các lời gọi `List`).
- **Đề xuất sửa**: `key` thêm "không đoạn nào là `.` hay `..`"; tham số `prefix` của `List`, `AbortUploads` là rỗng hoặc một `key` hợp lệ có thể kèm `/` cuối. `--bundle` chỉ nhận `^tpan-\d{8}-\d{6}\.tar\.gz\.age$`. `s3.TestKeyKhongRaNgoaiTienTo` có các ca `..`, `a/../b`, `./a`.

### S24. Con số ở mục 2 lệch với output

- **Mức**: nhẹ.
- **Ở đâu**: mục 2.1, 2.3 (và bảng quyết định của `docs/thay-doi/M18.md`, chép cùng số).
- **Chuyện gì xảy ra**: người đọc đối chiếu sẽ thấy: "+2,48 MiB" và "+2,30 MiB" trong khi output ghi +2 535 424 byte (2476 KiB, tức 2,42 MiB) và +2 359 296 byte (2304 KiB, tức 2,25 MiB): số KiB đã bị chia cho 1000. "Tám lời gọi" trong khi chính câu đó liệt kê mười. "`main.go` (510 dòng)" trong khi file hiện 665 dòng. "Tới 72 GiB" trong khi 8 MiB × 9000 = 72 000 MiB = 70,3 GiB.
- **Căn cứ**: `02-thu-vien-s3.out` dòng 271-273; `ban-thu-s3/main.go`.
- **Đề xuất sửa**: sửa bốn con số ở cả hai file.

### S25. `store.GeneratePassword` không sinh 32 ký tự, 190 bit

- **Mức**: nhẹ.
- **Ở đâu**: mục 3.9 ("Quyền quản trị của máy mới"), mục 5 (hàng "Tiến trình bất kỳ trên máy mới").
- **Chuyện gì xảy ra**: thợ gọi đúng hàm thiết kế nêu sẽ được 26 ký tự base32, 130 bit; hàm 32 ký tự chữ số (190 bit) là `sitePassword` của core. 130 bit vẫn đủ cho lập luận "bản băm không dò ngược được", nhưng tài liệu giải thích sẽ chép con số sai.
- **Căn cứ**: `internal/store/auth.go:95-98`; `go doc crypto/rand.Text` ("at least 128 bits"); `internal/core/site.go:35, 170-190`.
- **Đề xuất sửa**: sửa thành "26 ký tự, 130 bit", hoặc ghi rõ dùng `sitePassword`.

### S26. `agebox`: trần hệ số công lúc giải mã chưa có trong thiết kế; file bị cắt cụt

- **Mức**: nhẹ.
- **Ở đâu**: mục 2.4, 3.1 (`internal/agebox`), 3.8, B13.
- **Chuyện gì xảy ra**: gói panel trên kho là dữ liệu chưa tin. Người ghi được vào bucket đặt một file `age` có hệ số công rất cao; `machine restore` trên máy 1 GB RAM bị giết vì hết bộ nhớ trước khi kịp báo sai khoá (thư viện ghi: không gọi `SetMaxWorkFactor` thì dùng "a fairly high default … not suitable for systems processing untrusted files"). Bản thử `ban-thu-age/main.go:114` đã gọi `SetMaxWorkFactor(15)`, nhưng mục 3 không ghi nên thợ `agebox` có thể bỏ. Thứ hai: giải mã theo dòng nhả bản rõ trước khi gặp khối cuối; file bị cắt cụt cho ra phần đầu hợp lệ rồi mới lỗi.
- **Căn cứ**: `go doc filippo.io/age ScryptIdentity.SetMaxWorkFactor`; `12-age-doi-xung-va-gz.sh` (chú thích cuối phần 1).
- **Đề xuất sửa**: ghi vào 3.8: giải mã luôn `SetMaxWorkFactor(15)` (cả gói lẫn `restore.credentials`); bản rõ chỉ được dùng sau khi đọc tới `io.EOF` không lỗi. Thêm `agebox.TestHeSoCongCaoBiTuChoi`, `agebox.TestCatCutThiLoi` vào B13.

### S27. Hai phép kiểm health: mốc 48 giờ, điều kiện, và không nhìn tới kho

- **Mức**: nhẹ.
- **Ở đâu**: mục 3.13, 3.3 (`backup.target.rm` và các khoá `s3.panel_*`).
- **Chuyện gì xảy ra**: (b) "trong 48 giờ" tính theo `created_at` hay `remote_at` chưa viết; theo `remote_at` thì một bản 10 ngày tuổi vừa đẩy cũng đạt. Site tạo chưa qua một đêm bị cảnh báo oan. (c) không có điều kiện "lịch đang bật": máy chỉ sao lưu tay cảnh báo mãi; ngay sau `target set` lần đầu cũng cảnh báo vì chưa có `s3.panel_last`. Đổi đích không xoá `s3.panel_last`, nên 48 giờ đầu ở đích mới phép kiểm đạt nhờ gói ở đích cũ. Cả hai phép kiểm chỉ đọc `tpan.db`: object bị xoá hay bucket bị làm rỗng thì health vẫn xanh.
- **Căn cứ**: bảng ở 3.13; 3.3 (danh sách sáu khoá bị xoá khi gỡ đích).
- **Đề xuất sửa**: (b) tính theo `created_at`, bỏ qua site tạo chưa đủ 24 giờ. (c) thêm điều kiện lịch đang bật; `target set` đổi `TargetID` thì xoá hai khoá `s3.panel_*` và đặt cờ cần gói mới. `tpan.s3:target` (đã là phép kiểm nặng) `Head` thêm bản `stored` mới nhất của mỗi site, mất hoặc lệch cỡ thì Lỗi.

### S28. Trạng thái `local=0, remote=''` và dòng `local=1` đã mất file

- **Mức**: nhẹ.
- **Ở đâu**: mục 3.7, 3.15 (câu cuối), 3.6.
- **Chuyện gì xảy ra**: (a) một dòng nằm ngoài cả hai hạn giữ bị dọn trên máy (trong `after` của `backup create`) và dọn trên kho (trong `Sync` của tiến trình khác) cùng lúc. Mỗi bên đọc dòng trước khi bên kia ghi: kết quả `local=0, remote=''`, không file, không object, và không luật nào dọn dòng đó nữa. Không mất gì ngoài chính sách, nhưng trái câu cuối của 3.15. (b) dọn trên máy bị giết giữa "xoá file" và "đặt `local=0`": dòng `local=1, stored` không còn file; khôi phục bị từ chối `backup_corrupt` dù kho còn bản, cho tới lần dọn kế tiếp.
- **Căn cứ**: `internal/core/backup.go:163-177, 253-271` (dọn chạy trong `after`, ngoài khoá); `internal/core/backup.go:413-416`.
- **Đề xuất sửa**: mọi chuyển trạng thái là một lệnh SQL có điều kiện (như S8); `prune` xoá mọi dòng `local=0` mà `remote != 'stored'`. `PrepareRestore` thấy file mất mà dòng `stored` ở đích đang đặt thì đi đường `local=0`.

### S29. Rác không ai dọn; bộ đẩy nền ở chế độ demo

- **Mức**: nhẹ.
- **Ở đâu**: mục 3.5 (bước 3 của `S3Push`, bộ đẩy nền), 3.8 (`PanelBundle`), mục 8 (dữ liệu demo "có đích").
- **Chuyện gì xảy ra**: tiến trình bị giết giữa `PanelBundle`: thư mục `.tmp-<giờ>` chứa `tpan.db` dạng rõ nằm lại trong `_tpan`, lần nào bị giết thêm một cái. Tiến trình bị giết giữa lúc tải nhiều phần rồi dòng đó bị dọn khỏi máy trước lượt đẩy sau: phần tải dở nằm lại trên kho (bản thử: 249 MB) vì `AbortUploads(key)` chỉ chạy khi đẩy lại đúng key ấy. Dòng `local=0, stored` ở đích cũ không bao giờ bị dọn. `tpan serve --demo` với dữ liệu demo có đích: bộ đẩy nền gọi ra mạng 10 phút một lần.
- **Căn cứ**: 3.8 ("luôn xoá thư mục tạm" chỉ đúng khi tiến trình còn sống); `07-file-2gb.out` bước 3.
- **Đề xuất sửa**: đầu mỗi lượt `Sync`: xoá `.tmp-*` cũ hơn một giờ trong `_tpan`; huỷ các lần tải dở dưới tiền tố cũ hơn 24 giờ. `tpan backup list --remote` đếm dòng ở đích cũ. Bộ đẩy nền tắt khi `Deps.Demo`.

### S30. Đọc thân request có khoá, trần cỡ thân, và `target` của nhật ký

- **Mức**: nhẹ.
- **Ở đâu**: mục 3.11 (đoạn về `audit` và `system.Secret`), 3.3 (CA tối đa 64 KB).
- **Chuyện gì xảy ra**: thợ khai `SecretKey system.Secret` trong `TargetRequest`: giải mã JSON lỗi, vì `Secret` có `MarshalJSON` mà không có `UnmarshalJSON`. Admin gửi CA 60 KB: `apiReadJSON` cắt ở 64 KiB, trả `bad_request`. Admin dán nhầm endpoint dạng `https://<khoá>:<bí mật>@host`: lời từ chối đúng, nhưng nhật ký ghi `target` là `endpoint bucket prefix` nguyên văn thì bí mật vào bảng `audit` (và vào gói panel).
- **Căn cứ**: `internal/system/types.go:228-243`; `internal/server/api_health.go:101-116`; `internal/server/api_job.go:141-145`.
- **Đề xuất sửa**: thêm `UnmarshalText` cho `Secret` (đợt 0), hoặc đọc vào `string` rồi bọc ngay. Route đặt đích dùng `apiReadJSONMax` 128 KiB, `render` dùng 1,5 MiB. Nhật ký chỉ ghi giá trị đã qua kiểm; chưa qua thì ghi `?`.

### S31. `access_key_tail` với access key ngắn

- **Mức**: nhẹ.
- **Ở đâu**: mục 3.3 (luật access key 3 đến 128 ký tự; "Xem").
- **Chuyện gì xảy ra**: kho tự dựng dùng access key 6 ký tự (versitygw cho đặt tuỳ ý). `target show` và `GET /backups/target` hiện bốn ký tự cuối, tức hai phần ba khoá; khoá 3 hoặc 4 ký tự thì hiện cả khoá.
- **Căn cứ**: bảng "Kiểm đầu vào" ở 3.3.
- **Đề xuất sửa**: access key ngắn hơn 12 ký tự thì chỉ hiện độ dài; hoặc nâng độ dài tối thiểu lên 8 và hiện tối đa một phần tư.

### S32. `machine restore`: tiền tố mặc định, điều kiện "máy trắng", `s3.keep`

- **Mức**: nhẹ.
- **Ở đâu**: mục 3.10 ("Đầu vào", pha 1 bước 4, pha 3 bước 1), 3.3 (tiền tố mặc định).
- **Chuyện gì xảy ra**: không có user-data, người quản trị gõ `tpan machine restore --from-s3 --endpoint … --bucket …` trên máy mới tên khác: `--prefix` bỏ trống lấy mặc định `tpan/<tên máy mới>`, `List("panel/")` rỗng, lệnh báo `machine_bundle_not_found` dù gói nằm ở `tpan/<tên máy cũ>`. "Máy phải trắng" dựa vào "`/var/www` không có thư mục con": máy đã `stack install` (có `/var/www/html`) bị từ chối với mã `machine_not_empty` mà câu lỗi sẽ nói về site. Pha 3 bước 1 đặt lại đích nhưng không nói `keep` lấy từ đâu.
- **Căn cứ**: 3.3 hàng `prefix`; 3.10.
- **Đề xuất sửa**: với `machine restore`, `--prefix` bắt buộc khi dùng cờ; không thấy gói thì câu lỗi in tiền tố đã tìm. Điều kiện máy trắng nói thẳng "stack chưa cài (settings `stack.web` rỗng)" kèm mã riêng. `keep` lấy từ `s3.keep` của gói.

### S33. Bí mật chỉ hiện một lần có đường mất

- **Mức**: nhẹ.
- **Ở đâu**: mục 3.3 (bước 5 và 6 của `backup.target.set`), 3.9 (việc `sites`, chế độ dựng lại).
- **Chuyện gì xảy ra**: (a) `target set` lần đầu: job thành công, file `backup.key` đã ghi, `after` lỗi khi ghi settings (hoặc tiến trình chết trước khi in). Chạy lại theo hướng dẫn: khoá đã có nên không sinh và không hiện nữa. Người quản trị không biết mình chưa từng thấy khoá; mất máy là mất gói panel. (b) Lần khởi động đầu, lô `apply` lỗi ở site thứ ba: nếu mật khẩu chỉ ghi vào `/root/tpan-first-boot-passwords.txt` khi cả việc xong thì hai site đầu mất mật khẩu, vì chạy lại bỏ qua site đã có. (c) user-data dựng lại sinh từ trước cũ dần khi đổi khoá sao lưu, đổi khoá truy cập, đổi đích hoặc nâng TPAN; không có gì nhắc.
- **Căn cứ**: 3.3 bước 5, 6; 3.9 bước 3; `internal/server/api.go:55-58` (bí mật một lần chỉ nằm trong bộ nhớ).
- **Đề xuất sửa**: `target set` và `target show` luôn in dấu vân tay của khoá kèm câu "xem lại bằng: sudo tpan backup key show --reveal". Mật khẩu WordPress ghi nối vào file ngay khi có. `target show` in ngày sinh user-data dựng lại gần nhất và nhắc sinh lại sau bốn việc trên.

### S34. Tên, giá trị và ca thử còn thiếu

- **Mức**: nhẹ.
- **Ở đâu**: mục 3.4, 3.5, 3.6, 3.8, 3.9, 3.11, 3.12, mục 1, mục 7.
- **Chuyện gì xảy ra**: thợ phải tự đặt, và hai thợ có thể đặt khác nhau: (1) ID bước và `Info` của `S3Push`, `S3Pull`, `PanelBundle` (tên file chỉ biết lúc `Apply`, cần chữ giữ chỗ như `backupLater` của M9); `Kind` và `Target` của job `backup.panel`, `backup.pull`, `backup.key.new`. (2) Giá trị của trường `location` trong `list_backups`; `remote_at` rỗng ra JSON thế nào (`omitzero`). (3) Luật ký tự cho "tên bản" đi vào dòng chú thích của script (luật ở 3.9 viết cho `base`). (4) `POST /backups/{id}/push|pull` trả 202 không có id job, và hàng chờ trong bộ nhớ mất khi `tpan serve` khởi động lại: yêu cầu tải biến mất không dấu vết. (5) Kiểu địa chỉ `bucket.host` (`--vhost`) có trong CLI nhưng không có trong unit lẫn e2e nào. (6) `ban-thu-s3/sig_test.go` chỉ khớp được hai vectơ mức bucket; hai vectơ có đường dẫn object và thân bị bỏ vì ví dụ của AWS ký thêm `date`, `range`. (7) Gói panel có `config.toml` nhưng không dòng mã nào của TPAN đọc file đó.
- **Căn cứ**: `internal/steps/backup.go:251-253`; `ban-thu-s3/sig_test.go` (hai `t.Skip`); KE-HOACH mục 8 ("tpan chưa đọc TOML"); không có chuỗi `config.toml` trong mã ngoài một chú thích.
- **Đề xuất sửa**: thêm một bảng nhỏ vào mục 3 cho (1) đến (3). (4) trả id của dòng trong một bảng hàng chờ nhỏ, hoặc ghi rõ "mất khi khởi động lại" trong câu trả lời. (5) thêm một ca e2e với Garage kiểu `bucket.host`, hoặc bỏ cờ ở bản này. (6) cho bộ ký của test nhận thêm header để tái hiện ví dụ PUT. (7) bỏ `config.toml` khỏi gói hoặc ghi lý do giữ.

### Đã kiểm và thấy đúng

- Số đo ở 2.3 (thời gian, đỉnh RAM, hai ETag, 249 MB tải dở, 503 từ phần 5, sửa một byte) khớp `07-file-2gb.out`.
- 2.4: 356 KB, 0,39 giây, 4,4 MB, hai câu lỗi khớp `11-age-thu.out`. Chế độ khoá đối xứng với hệ số công 15 giải mã được bằng lệnh `age` 1.1.1 qua terminal giả, đỉnh RAM 36,6 MB lúc mã hoá (`12-age-doi-xung-va-gz.out`). Tính "không giả được khi không có khoá" của người nhận scrypt khớp tài liệu thư viện (`go doc filippo.io/age`, `ScryptRecipient.WrapWithLabels`).
- 2.5: uid, thư mục, umask, `PATH`, không có `HOME`, quyền của user-data, 308 KB, lệnh giữa lỗi vẫn `done`, lệnh cuối lỗi thì `error`, chạy lại bằng `cloud-init single` khớp `08-cloud-init-thu.out` và bước 1 của `09-dung-lai-may-thu.out`; `gz+b64` khớp `12`. Các giới hạn của AWS, OpenStack, Azure, GCP khớp `10-nguon-tra.out`.
- 2.6: 1 phút 50 giây và từng chặng khớp `09-dung-lai-may-thu.out`; `site add` chạy lại trên dòng đã có và `stack install` đọc web server, nguồn gói từ `tpan.db` cũ khớp `internal/core/site.go:396-433`, `internal/core/stack_sources.go:40-66`.
- 2.1, 2.2: số module, số gói (+81, +64), ngày push, tình trạng MinIO, mã lỗi 403, 400, 404 của hai máy chủ khớp `02`, `01`, `10`, `05`.
- Engine: `after` chỉ chạy khi job thành công và chạy sau khi khoá đã nhả (`internal/core/types_m12.go:236-254`); Undo theo thứ tự ngược (`internal/engine/engine.go:171-192`); health bỏ vòng khi khoá job bị giữ (`internal/server/api_health.go:58-80`).
- `recSys` và `jobLockSystem` nhúng `System`; chỉ có `LocalSystem` và `FakeSystem` cài đặt giao diện, nên thêm `Streamer` không phải sửa lớp bọc nào.
- Migration: `backups` là bảng STRICT, nhận `ADD COLUMN … NOT NULL DEFAULT`; `migrate` áp theo tập nên 1800 vào trước 17xx không sao; bản cũ từ chối DB có migration lạ (`internal/store/store.go:211-229`), đúng như mục 6 nói về lùi bản.
- Route mới không đụng mẫu cũ của `ServeMux` (`/backups/{id}/restore`, `/backups/settings`); route thiếu luật thì `mount` panic (`internal/server/routes.go:232-243`).
- `steps.Dir` sửa quyền thư mục đã có, nên `/etc/tpan` do `write_files` tạo 0755 (bằng chứng `08`: `/etc/tpan-ci` là 0755) được Plan `setup` đưa về 0700 (`internal/steps/files.go:361-386`).
- `TPANBackupAge` và `newestSnapshot` chỉ nhận tên `tpan-<giờ>.db`, nên gói `.tar.gz.age` và `.tmp-*` trong cùng thư mục không làm chúng sai (`internal/checks/m9.go:21`, `cmd/tpan/backup.go:222`). "Giữ 3 gói" khớp `DefaultBackupKeep`.
- Không có route hay lệnh nào xoá một bản sao lưu lẻ; dòng `backups` chỉ mất qua dọn, đúng với sơ đồ 3.15.
- Cron backup gọi `tpan backup create --all --scheduled --yes` (`internal/templates/backup-cron.tmpl`), đúng với mục 6. Phép kiểm DNS là loại mềm và nặng (`internal/core/health.go:181`), nên "không có lỗi ngoài DNS" ở e2e phần M là cảnh báo.
- `sessions` chỉ lưu sha256 của token (`internal/store/migrations/003_auth.sql`); `tpan.db` không có mật khẩu DB của site.
- TLS với chứng chỉ gốc riêng: không đưa CA thì bị từ chối, sai tên thì bị từ chối, `http` vào cổng TLS thì lỗi (`14-tls-ca-rieng.out`).

Không kiểm được từ những gì có trong repo: versitygw có `.deb` cho amd64 và `checksums.txt` (output `01` chỉ liệt kê bản arm64); ngày phát hành của Garage v2.5.0; con số của DigitalOcean và Hetzner (thiết kế đã ghi là chưa tra được).
