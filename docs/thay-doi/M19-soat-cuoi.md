# M19: soát cuối (thử phá)

Người soát: thợ soát cuối, phiên headless ngày 2026-11-25, trên nhánh `moc/M19`. Soát ở HEAD `7adb79a`; lúc viết xong HEAD là `22dbed8`, nhưng `internal/`, `cmd/`, `web/src` không đổi giữa hai bản (`git diff 7adb79a..HEAD --stat` rỗng). Phiên chính làm câu 1 (chiếm tên, phân quyền, nâng cấp); hai nhánh con làm câu 2 (chèn cấu hình) và câu 3 (chứng chỉ, thao tác toàn máy).

Cách làm: đọc mã, rồi chạy test Go tạm trên máy phát triển. Không dùng máy ảo, không sửa mã sản phẩm. Nhánh câu 2 có chạy nginx thật trong một container dùng một lần, và `httpd` của máy không cần root.

Các file test tạm (`zz_soatcuoi_*_test.go`) đã xoá sau khi chạy. Output trích trong file này là output thật của các lần chạy đó. Log của nhánh câu 2 nằm ở `tmp-phien/soat-cuoi-q2/` (git loại trừ).

Mức: NẶNG (chiếm tên người khác, chèn cấu hình, làm hỏng site hay chứng chỉ của người khác, leo quyền), VỪA, NHẸ.

## Tóm tắt

1 NẶNG, 5 VỪA, 4 NHẸ.

| Mã | Mức | Một dòng |
|---|---|---|
| C1 | NẶNG | `POST /apply` của user thường không đặt `NotAdmin`: tạo được site dưới tên miền gốc mà người khác giữ bằng tên phụ, và khoá người đó khỏi tên miền gốc của mình |
| C2 | VỪA | Cùng gốc C1: `/apply` trả `name_taken` cho tên phụ của người khác, khác `site_not_found` của `/sites`, nên dò được tên phụ trên máy |
| C3 | VỪA | Trần 5 job SSL (S17) chỉ áp cho job tự chạy; user đổi tên phụ rồi gọi `POST /sites/{d}/ssl` tay, mỗi lần một đơn Let's Encrypt mới của tài khoản ACME chung |
| C4 | VỪA | Plan tên phụ dựng vhost lúc Prepare, ngoài khoá; lệnh dòng lệnh xác nhận muộn ghi lại tên đã sang site của user khác |
| C8 | VỪA | Tên dài hơn 46 byte làm `nginx -t` hỏng: `www.` tự thêm làm site add với tên gốc 43 tới 46 byte hỏng (hồi quy); trên Apache, một tên phụ dài chặn đổi stack của cả máy |
| C9 | VỪA | Vá S13 chưa trọn: tên `www.` mặc định ở site add và tên khớp wildcard của vhost tay không qua kiểm "vhost không do tpan quản" |
| C5 | NHẸ | Gỡ tên phụ khi đã chạm trần SSL: tên vẫn nằm trong SAN của chứng chỉ cũ (B9 không còn đúng) |
| C6 | NHẸ | Bước thử ACME cho tên phụ đi theo DNS thật: SSRF mù từ máy chủ (cổng 80, GET, đường dẫn cố định); thân trả về chỉ in ra cho admin ở dòng lệnh |
| C7 | NHẸ | Xoá hẳn site `ssl=0` không xoá lineage certbot còn sót |
| C10 | NHẸ | Vhost chuyển tên phụ của Apache trỏ `DocumentRoot` vào public, không chặn (gia cố; chưa vượt được) |

Không tìm được đường chèn chỉ thị vào nginx hay Apache, đường che luật chặn, hay chuyển hướng mở (chi tiết ở "Đã thử mà không phá được").

## Phát hiện

### C1 (NẶNG): `POST /api/v1/apply` của user thường bỏ qua luật tên miền gốc của bảng `site_names`

**Ở đâu**

- `internal/server/api_trash.go:155`: `handleApply` dùng `sites := s.apiSites()` mà không đặt `sites.NotAdmin = !who(r).IsAdmin()` (khác `api_job.go:260` của `POST /sites` và `m19Sites` của `api_m19.go:36`).
- `internal/core/names_m19.go:633`: `addClaims` dựng claim tên chính với `ByAdmin: !s.NotAdmin`, nên qua `/apply` claim của user thường mang `ByAdmin = true`.
- `internal/site/names.go:128`: `CheckClaims` bỏ hẳn `RootForeign` khi `ByAdmin`.
- `internal/core/apply.go:182` gọi `RegistrableTaken` (`internal/core/m28_p4.go:28`). Đây là lớp chặn sớm của M28, và nó chỉ đọc bảng `sites` (tên chính): không thấy tên phụ, cũng không thấy dòng pending.
- `internal/core/apply.go:235` (`CheckNames`) và `:495` (`ClaimNames`) xét lại các claim trên, nhưng vẫn với `ByAdmin = true`.

**Hậu quả**

User A tạo được site mới (qua `/apply`; giao diện gọi route này ở chế độ "Dán danh sách" của màn tạo site, `web/src/screens/tao-site/DanhSach.tsx:63,74`) với tên chính là tên con của một tên miền gốc mà user B đang giữ bằng TÊN PHỤ. Ví dụ B có site `example.net` với tên phụ `example.org`; A tạo được `shop.example.org`. A phục vụ PHP dưới tên đó và xin được chứng chỉ cho nó khi DNS (thường là bản ghi `*`) trỏ về máy. Đúng mối đe doạ P4 của M28 mà M19 hứa vá cho cả tên phụ. Thêm nữa: từ lúc đó chính B bị khoá khỏi tên miền gốc của mình (thêm `www.example.org` hay bất kỳ tên nào dưới `example.org` vào site của B thì bị `name_unavailable`, vì `RootForeign` thấy dòng của A).

Nói gọn: trên đường `/apply`, luật tên miền gốc chỉ còn lớp M28, mà lớp này chỉ nhìn tên chính trong bảng `sites`. Tên phụ có tên miền gốc khác tên chính của site (như `example.org` của site `example.net`) không được tính, nên tên miền gốc đó coi như còn trống. `ClaimNames` trong transaction cũng không xét lại `RootForeign`, vì claim mang `ByAdmin = true`. (Đua song song giữa hai request API thì không có, vì cờ "một job một lúc" của server giành chỗ trước Prepare; lỗ này đi tuần tự, không cần đua.)

**Tái hiện**

Hai test tạm (đã xoá; phần lõi chép dưới đây để dựng lại).

1. Qua route thật (`internal/server/zz_soatcuoi_q1_test.go`, dựng trên `p4Env` của `api_m28_p4_test.go`). B (`authzB`) có site `b.example.net` và tên phụ active `example.edu`. Lõi của test:

   ```go
   e, h := p4Env(t)
   bSite, alias := apiDomain(t, "b.example.net"), apiDomain(t, "example.edu")
   e.st.ClaimNames(ctx, []site.Claim{{Name: alias, Site: bSite, Owner: authzB, Kind: site.KindAlias, Mode: site.ModeServe}})
   e.st.ActivateNames(ctx, bSite, []validate.Domain{alias})
   p4Create(e, h, authzA, "shop.example.edu", "?dry_run=1") // POST /sites
   e.do(h, "POST", "/api/v1/apply?dry_run=1",
       `{"defaults":{"admin_email":"x@example.com"},"sites":[{"domain":"shop.example.edu"}]}`, e.cookies[authzA])
   ```

   Chạy `go test ./internal/server/ -run TestSoatCuoiApplyRoute -v`:

   ```
   A POST /sites?dry_run=1 shop.example.edu: 404 {"error":{"code":"site_not_found","message":{"vi":"không tạo được site shop.example.edu từ tài khoản này: domain đã được dùng",...}}}
   A POST /apply?dry_run=1 shop.example.edu: action=create code="" reason=""
   A POST /apply?dry_run=1 example.edu: action=refused code="name_taken" reason="tên example.edu đã thuộc một site khác trên máy này"
   B POST /sites/b.example.net/names?dry_run=1 www.example.edu: 409 {"error":{"code":"name_unavailable",...}}
   ```

   Dòng 1 là đường `/sites` chặn đúng. Dòng 2 là lỗ: cùng tên, đường `/apply` cho tạo. Dòng 4 là hậu quả cho B, sau khi A đã có `shop.example.edu`: B không thêm được `www.example.edu` vào site của chính mình. Dòng 3 là C2 bên dưới.

2. Chạy thật trọn job qua core (`internal/core/zz_soatcuoi_q1_test.go`, `newNamesEnv`, store thật). B thêm tên phụ `example.org` qua `PrepareAliasAdd` (`NotAdmin = true`). Sau đó `PrepareApply` với `Scope: "khach_a"` và `NotAdmin = false`, đúng như `handleApply` dựng. Chạy `go test ./internal/core/ -run TestSoatCuoiApply -v`:

   ```
   POST /sites của A, shop.example.org: err = site_not_found: không tạo được site shop.example.org từ tài khoản này: domain đã được dùng
   apply của A: dòng shop.example.org: action=create code=""
   claim shop.example.org owner=khach_a ByAdmin=true
   job 0a1af6388461b1ea: succeeded; Created=[{Domain:shop.example.org Status:succeeded WPInstalled:true ...}]
   site shop.example.org có trong DB: true, owner="khach_a"
   dòng tên của shop.example.org: map[shop.example.org:{... Owner:khach_a Kind:primary Mode:serve State:active ...}]
   ```

Đã xem mọi chỗ gọi `PrepareAdd` và `PrepareApply`. Chỉ đường API `/apply` thiếu `NotAdmin`. Các đường còn lại hoặc đã đặt nó (`POST /sites`, các route M19), hoặc là việc của admin: `tpan apply` và `tpan site add` chạy bằng root, cloud-init (`POST /cloud-init/render` chỉ admin, chạy `PrepareApply` bằng root lúc máy khởi động), Plan của AI (`/plans/*` chỉ admin duyệt).

**Đề xuất sửa**

Trong `handleApply` đặt `sites.NotAdmin = !who(r).IsAdmin()` như `handleSiteCreate`. Chắc hơn: đổi nghĩa zero value cho an toàn (ví dụ `PrepareApply` suy `ByAdmin` từ `in.Scope == ""`, hay trường `Admin bool` thay cho `NotAdmin` để quên đặt là đóng chứ không mở). Thêm test route `POST /apply` với tên phụ của người khác vào `api_m28_p4_test.go`.

### C2 (VỪA): `POST /apply` của user thường dò được tên phụ của người khác

**Ở đâu:** `internal/core/apply.go:229-238`. `PrepareAdd` từ chối bằng `siteAddRefusal`, mà `s.NotAdmin = false` nên lời đi theo nhánh admin (`aliasRefusal`, `names_m19.go:93-94`). Cùng gốc với C1.

**Hậu quả:** khi user thường hỏi một tên đúng bằng tên phụ của site người khác, `/apply` (cả `?dry_run=1`, không ghi gì) trả `code="name_taken"` kèm câu "đã thuộc một site khác trên máy này". `POST /sites` với cùng tên trả `404 site_not_found`, đúng như tên chưa ai có. Vậy chỉ cần gọi xem trước là biết tên nào đang là tên phụ trên máy, tức là dò được khách hàng của máy. M28 và mục 3.2 "Không lộ thông tin" muốn chặn đúng việc này.

Lộ đúng với tên phụ có tên miền gốc khác tên chính của site. Tên chính của người khác, và tên phụ cùng tên miền gốc với một tên chính, thì không lộ: chúng bị lớp M28 chặn trước bằng `site_not_found`.

**Tái hiện:** dòng 3 của test route ở C1:

```
A POST /apply?dry_run=1 example.edu: action=refused code="name_taken" reason="tên example.edu đã thuộc một site khác trên máy này"
```

**Đề xuất sửa:** cùng bản sửa của C1. Khi `NotAdmin` đúng, `siteAddRefusal` trả `site_not_found` như `POST /sites`.

### C3 (VỪA): trần 5 job SSL mỗi chủ (S17) không giữ được hạn mức Let's Encrypt chung của máy

**Ở đâu**

- `internal/core/names_m19.go:520-553`: trần chỉ áp cho job `site.ssl` tự chạy tiếp sau thao tác tên.
- `internal/server/api_ssl.go:143-152`: `handleSiteSSL` (`POST /sites/{domain}/ssl`, luật `ownerSite` ở `routes.go:115`) không đếm job.

**Hậu quả**

Trước M19, gọi lại SSL khi bộ tên không đổi thì certbot không xin đơn mới. Từ M19, user thường đổi bộ tên phụ (thêm, gỡ, đổi kiểu) rồi tự bấm SSL. Mỗi vòng là một bộ tên khác, nên mỗi lần ra một đơn certbot mới.

Máy dùng chung một tài khoản ACME, với hạn mức đơn mới mỗi tài khoản trong 3 giờ. Một user đốt hết hạn mức đó thì user khác xin hay gia hạn chứng chỉ bị Let's Encrypt từ chối. Cùng lúc user đó giữ luôn chỗ "một job một lúc" của API.

Quan hệ với quyết định đã chốt: S17 ("nhận, cắt được") chỉ đặt trần cho job tự chạy. Chú thích ở `names_m19.go:517-519` ghi rằng gọi tay `tpan ssl issue` không bị giới hạn. Điều đó hợp lý khi người gọi tay là admin ở dòng lệnh. Nhưng user thường cũng gọi tay được qua API, và M19 cho họ cách đổi bộ tên tuỳ ý. Vậy trần S17 không giữ được mục đích S17 ghi ("giữ hạn mức Let's Encrypt chung của máy").

**Tái hiện** (test tạm `TestSoatCuoiQ3CapBypass` trong `internal/core`, store thật, certbot giả)

- Sau khi chạm trần, thêm một tên phụ thì không có job SSL chạy tiếp: `FollowUpJob="" certbot them=0; ghi chu="ssl_rate_limited"`.
- Chạy tiếp 6 vòng "đổi tên phụ rồi gọi SSL tay" thì có `certbot certonly chay them 6 lan; CountJobs=11 (tran 5); so bo ten khac nhau: 6`.

**Đề xuất:** áp `CountJobs` (hoặc một trần riêng cho số đơn mới) cho cả `POST /sites/{d}/ssl` khi người gọi không phải admin. Dòng lệnh của admin thì miễn.

### C4 (VỪA): Plan tên phụ dựng vhost lúc Prepare, ngoài khoá; Execute muộn ghi lại tên đã sang tay người khác

**Ở đâu**

- `internal/core/names_m19.go:405`: `siteVhost(o.next, cfg)` dựng nội dung vhost lúc Prepare.
- `internal/engine/engine.go:131`: khoá toàn máy chỉ lấy lúc Execute.
- `cmd/tpan/site_m19.go:152-175`: dòng lệnh dựng Plan, in bản xem trước, chờ người gõ xác nhận rồi mới Execute.
- Cờ "một job một lúc" của server (`api_ssl.go:66-77`) chỉ chặn giữa các request API với nhau, không chặn dòng lệnh.

**Hậu quả**

Admin mở `tpan site alias mode` cho tên `shop.example.org` của site A rồi để đó chờ xác nhận. Trong lúc ấy, qua API, A gỡ tên đó và user B thêm nó vào site của B (`site_names.owner = khach_b`). Admin xác nhận, job cũ chạy: vhost của A khai lại `shop.example.org`. Job vẫn báo `succeeded`, rồi mới báo lỗi ghi DB (`store: không tìm thấy`).

Kết quả: hai vhost cùng khai một tên, nginx dùng file nạp trước, nên lưu lượng tên của B có thể vào site A. Phép kiểm `declaredGuard` (S13) chỉ chạy ở `alias.add`, không bắt được đường này. Còn `web.server_name.unique` lúc đó khuyên `mv <vhost của A> /root/` (`checks/m14.go:852`), làm sập cả site A.

Cùng kiểu với mọi Plan vẽ vhost dựng trước khi lấy khoá (harden, đổi stack, `health --fix`, ssl, php, luật chuyển hướng): tên phụ hay luật thêm trong cửa sổ đó cũng mất khỏi vhost. Mức VỪA vì cần admin đang giữ một lệnh dòng lệnh chờ xác nhận đúng lúc đó.

**Tái hiện** (test tạm `TestSoatCuoiQ3StaleOpHijack` trong `internal/core`)

1. Dựng sẵn op đổi kiểu `shop.example.org` của A.
2. A gỡ tên đó, B thêm nó (dòng DB `owner=khach_b`).
3. Chạy op cũ ra `status=succeeded`, rồi lỗi `store: không tìm thấy`. Vhost của A có lại `server_name shop.example.org;`, và `nginx -T` có 2 chỗ khai tên này.

**Đề xuất:** trong Execute, sau khi lấy khoá, đọc lại site từ DB và dựng lại vhost; hoặc thêm bước đầu chạy dưới khoá kiểm bộ tên và luật trong DB không đổi so với lúc Prepare, đổi thì từ chối.

### C5 (NHẸ): tên phụ đã gỡ còn trong chứng chỉ của site cũ khi job SSL chạy tiếp bị trần

- **Ở đâu:** `internal/core/names_m19.go:548-552`. Lần gỡ tên đang nằm trong SAN cũng bị trần S17 chặn. Vậy khi chủ đã chạm trần, khẳng định B9 trong bảng của thiết kế ("Gỡ tên phụ đang nằm trong chứng chỉ thì chứng chỉ được xin lại không có nó") không còn đúng.
- **Hậu quả:** chứng chỉ của A vẫn phủ tên mà B đã giữ (`owner=khach_b state=active`) cho tới lần SSL sau. Không đổi định tuyến, vì khối 443 của B chỉ dùng chứng chỉ của B. Nhưng mỗi lần gia hạn, chứng chỉ của A lại xin cho tên của B. Gia hạn đó vẫn đạt vì mọi vhost dùng chung thư mục ACME (`site-nginx-name-redirect.tmpl:12-14`). Còn nếu DNS của tên đó chuyển đi nơi khác thì gia hạn cả chứng chỉ của A hỏng.
- **Tái hiện:** test tạm `TestSoatCuoiQ3RemovedAliasStaysInSAN`. Sau khi B giữ tên, SAN của A vẫn là `[example.com shop.example.org]`.
- **Đề xuất:** job SSL chạy tiếp sau khi gỡ tên đang có trong chứng chỉ thì không bị trần.

### C6 (NHẸ): bước thử ACME cho tên phụ đi theo DNS thật (SSRF mù từ máy chủ)

- **Ở đâu:** `internal/steps/ssl.go:387-399` gọi tới tên mà không có `--resolve`. 300 byte đầu của thân trả về được chép vào `Reason` (`:550`), rồi vào `Notes` (`names_m19.go:574-578`).
- **Hậu quả:** user trỏ DNS của một tên phụ về một địa chỉ trong mạng nội bộ, rồi thêm tên đó vào site HTTPS. Máy chủ (root) gửi GET tới `http://<IP đó>:80/.well-known/acme-challenge/<token>`. Cổng và đường dẫn cố định, không theo chuyển hướng.
  - User thường không đọc lại được thân. Qua API, `namesPlan.Execute` (`api_m19.go:110-113`) chỉ trả `Report` và bỏ `Notes`. `api_ssl.go` và `api_job.go` cũng không đưa `Names` hay `Notes` ra. Chỉ admin chạy dòng lệnh mới thấy 300 byte đó in ra màn hình.
  - Vậy với user thường, đây là SSRF mù (cổng 80, GET, đường dẫn cố định), chỉ biết đạt hay hỏng qua `in_cert` của tên.
- Lỗ có sẵn với tên chính từ M5. Ở đường tên chính, lỗi của bước thử làm job hỏng, nên phần trích có thể nằm trong `job.Error` mà chủ site đọc được; chưa kiểm phần này. M19 nhân đường gọi lên 24 tên mỗi site, và nó tự chạy sau mỗi lần thêm tên.
- **Tái hiện:** test tạm `TestSoatCuoiQ3ProbeExcerptLeak` (nhánh câu 3) có ghi chú `...nội dung khác file thử: BI-MAT-NOI-BO: token=abc123...` trong `NamesResult.Notes`.
- **Đề xuất:** không chép thân vào lời (chỉ nêu mã HTTP và IP đã gọi), và không thử tới IP riêng (RFC 1918, 127/8, 169.254/16) khi tên không trỏ về máy.

### C7 (NHẸ): xoá hẳn site có `ssl=0` không xoá lineage certbot

- **Ở đâu:** `internal/core/trash.go:467-473` (chỉ đọc mã).
- **Hậu quả:** job SSL hỏng sau bước certbot thì lineage vẫn nằm lại trên đĩa, kèm các tên phụ trong SAN. Sau khi xoá hẳn site, certbot vẫn tự gia hạn cho những tên đã nhả cho người khác.
- **Đề xuất:** xoá lineage khi thư mục chứng chỉ có, không dựa vào cờ `st.SSL`.

### C8 (VỪA): tên dài hơn 46 byte làm `nginx -t` hỏng; `www.` tự thêm biến S19 thành hồi quy, và trên Apache nó chặn đổi stack của cả máy

**Ở đâu**

- `internal/templates/files/nginx-org-include.conf.tmpl:10` đặt `server_names_hash_bucket_size 64` (gói nginx của Ubuntu cũng mặc định 64).
- `internal/core/names_m19.go:645-662`: `addClaims` mặc định thêm `www.<gốc>` kiểu redirect vào `rec.Names`, nên vhost của job tạo site có luôn tên đó.
- `internal/core/stack_switch.go:383`: đổi stack vẽ lại vhost của mọi site rồi chạy `nginx -t`.

**Hậu quả**

1. Trên nginx, tạo site với tên gốc dài 43 tới 46 byte (`tpan site add` hay `POST /sites`) giờ hỏng ở `nginx -t` và hoàn tác cả job: tên www tự thêm dài 47 tới 50 byte. Trước M19 lệnh này chạy được. Lời lỗi `could not build server_names_hash` khó hiểu với người dùng.
2. Trên máy Apache không có gì bắt tên dài. User thường thêm được một tên phụ từ 47 byte, hay tạo site có tên gốc 43 tới 46 byte (www tự thêm). Về sau `tpan stack switch --to nginx` của admin hỏng ở bước vẽ vhost và hoàn tác, cho cả máy. Một tên của một user chặn thao tác toàn máy.

S19 ghi rủi ro này là "để rủi ro", nhưng chỉ xét `alias add` trên nginx (hoàn tác được); không xét www tự thêm và đổi stack.

**Tái hiện**

Nhánh soát câu 2 chạy nginx thật (`nginx:1.30-alpine` trong một container dùng một lần; phần mã băm tên không đổi so với 1.24 của Ubuntu), với một khối `default_server` như trên máy thật, và vhost vẽ bằng `SiteVhostNames`. Log ở `tmp-phien/soat-cuoi-q2/nginx-bucket-boundary.log` (thư mục bị git loại trừ):

```
== b46 (có default_server)
nginx: configuration file /t/b46.conf test is successful
== b47 (có default_server)
nginx: configuration file /t/b47.conf test failed
== root44-www-64d (có default_server)
nginx: [emerg] could not build server_names_hash, you should increase server_names_hash_bucket_size: 64
nginx: configuration file /t/main-root44-www-64d.conf test failed
== root44-nowww-64d (có default_server)
nginx: configuration file /t/main-root44-nowww-64d.conf test is successful
```

Với Apache, `httpd -t` 2.4.67 của máy phát triển nhận vhost 24 tên dài 252 byte (`Syntax OK`). Cùng bộ tên đó, nginx với bucket 64 báo `emerg`, còn với bucket 512 thì `test is successful` (`nginx-t.log`: `heavy-80-64 test failed`, `heavy-80-512 test is successful`).

**Đề xuất**

Ghi `server_names_hash_bucket_size 512;` vào một file `conf.d` cho cả gói Ubuntu lẫn nginx.org; tên 253 byte cần từ 272 trở lên. Tối thiểu thì làm hai việc:

- `defaultWWW` bỏ tên www, kèm ghi chú, khi tên đó dài hơn 46 byte.
- `alias add` từ chối tên dài hơn 46 byte trên mọi stack, và Prepare của đổi stack từ chối sớm, chỉ ra tên dài.

### C9 (VỪA): vá S13 chưa trọn: tên `www.` mặc định do M19 tự thêm ở site add, và tên khớp wildcard của vhost tay, không qua kiểm "vhost không do tpan quản"

**Ở đâu:** `declaredGuard` và `declaredElsewhere` (`internal/core/names_m19.go:197-272`) chỉ được gọi trong `PrepareAliasAdd`. `PrepareAdd` (`internal/core/site.go`) thêm tên phụ `www.` mặc định qua `addClaims` (`names_m19.go:645-662`), mà chỉ kiểm thư mục, user Linux và DB qua `unmanaged` (`site.go:239-265`). Nó không đọc `nginx -T` hay `apache2ctl -S`. `checks.DeclaredNames` thì chỉ so khớp đúng từng chữ.

**Quan hệ với quyết định đã chốt:** S13 ghi rằng lỗ này "có sẵn với tên chính", và chốt vá cho `alias.add`. Phát hiện này không nhắc lại phần tên chính đó. Nó chỉ ra hai đường M19 tự mở mà bản vá S13 không phủ:

1. **Tên `www.` mặc định là tên phụ M19 tự thêm ở site add**, kiểu redirect, và không qua `declaredGuard`. Ví dụ admin có vhost tay khai `www.company.example` (tên miền gốc không có trong DB của tpan). User tạo site `company.example` thì được luôn `www.company.example` mà không bị từ chối. Thêm đúng tên đó bằng `alias add` thì bị từ chối.
2. **Vhost tay khai wildcard `*.example.com`:** tên phụ `x.example.com` vẫn qua `declaredGuard`, mà nginx ưu tiên tên khớp đúng hơn wildcard. Nhánh soát câu 2 ghi nhận điểm này.

**Hậu quả:** hai vhost cùng nhận một tên. Với tên trùng hệt, web server dùng vhost nạp trước; file của tpan là `tpan-<domain>.conf`, nên đứng trước mọi vhost tay có tên xếp sau "tpan-". Với wildcard, nginx luôn chọn tên khớp đúng. Site của user lặng lẽ nhận lưu lượng của dịch vụ kia và xin được chứng chỉ cho tên đó (webroot ACME dùng chung). Đây đúng là ca gây lỗi S13 mô tả.

Kiểm thêm cùng lúc: tên chính cũng không qua kiểm này. Đây là phần S13 đã ghi là có sẵn từ trước, nên không tính vào phát hiện.

**Tái hiện:** test tạm `TestSoatCuoiWWWDeclaredElsewhere` (`internal/core`, `newNamesEnv`, nginx giả đọc `sites-enabled`). File lạ `/etc/nginx/sites-enabled/www-company.conf` khai `server_name www.company.example` với `proxy_pass`. User `khach_a` (`NotAdmin = true`) tạo site `company.example` với www mặc định. Chạy `go test ./internal/core/ -run TestSoatCuoiWWWDeclared -v`:

```
site add company.example (user, www mặc định): err=<nil> job=succeeded notes=[]
vhost /etc/nginx/sites-available/tpan-company.example.conf khai www.company.example: true
dòng tên: {Name:www.company.example Site:company.example Owner:khach_a Kind:alias Mode:redirect State:active ...}
```

Cùng file lạ đó, `alias add` bị từ chối: test sẵn có `TestAliasAddDeclaredElsewhere` khẳng định `name_unavailable` cho user thường. Một test tạm khác (`TestSoatCuoiPrimaryDeclaredElsewhere`) cho kết quả tương tự với tên chính `tool.example.org` (`PrepareAdd err=<nil>`, `job=succeeded`); đó là phần S13 đã biết.

Trên máy thật, nếu file lạ đứng trước `tpan-*` theo thứ tự chữ cái thì bước `NameProbe` của tên www có thể hỏng, và site add hoàn tác (không chiếm được, nhưng tạo site hỏng với lời khó hiểu). Nếu file lạ đứng sau, như `www-company.conf` ở đây, thì vhost của tpan thắng và tên bị chiếm.

**Đề xuất**

- Trong `addClaims`, gọi `declaredElsewhere` cho tên www mặc định. Trùng thì bỏ tên www kèm ghi chú, như khi tên www bị người khác giữ.
- Tiện thể làm luôn cho tên chính, và chạy `declaredGuard` làm bước đầu của Plan tạo site, như `alias add`. Với user thường, lời từ chối giữ đúng `site_not_found`.
- `DeclaredNames` ghi nhận cả tên wildcard, và từ chối tên phụ khớp một wildcard của file lạ.

### C10 (NHẸ, gia cố): vhost chuyển tên phụ của Apache trỏ `DocumentRoot` vào thư mục public, không có handler PHP và không chặn

- **Ở đâu:** `internal/templates/files/site-apache-name-redirect.tmpl:8` và `site-apache-name-redirect-ssl.tmpl`. Khối `<Directory "{{.Dir}}">` không có `Require`, nên kế thừa quyền của `apache2.conf`; trên Ubuntu đó là `<Directory /var/www/> Require all granted`.
- Chú thích của `site-apache-ssl.conf.tmpl:8-11` ("hỏng thì đóng" nhờ `<Directory /> Require all denied`) không đúng cho vhost này.
- **Hậu quả:** nếu một ngày `RewriteRule ^/(.*)$` bị vượt, Apache trả nguyên văn mã nguồn PHP, gồm cả `wp-config.php` có mật khẩu DB. Hiện chưa vượt được: nhánh soát câu 2 thử `%0A`, `/wp-config.php/%0Ax`, `../`, `%2e%2e`, request line dạng URL tuyệt đối trên httpd 2.4.67 mô phỏng `apache2.conf` của Ubuntu, và mọi đường đều ra 301.
- **Đề xuất:** cho `DocumentRoot` của vhost chuyển tên trỏ vào một thư mục rỗng (ví dụ thư mục trang tạm khoá), hoặc thêm `Require all denied` cho thư mục site trong vhost này; thử thách ACME có `Alias` riêng nên không ảnh hưởng.

## Đã thử mà không phá được

### Chiếm tên, phân quyền, nâng cấp (câu 1)

- **Đua song song giữ tên.** `TestClaimNamesRace` (hai `Store` mở cùng file, 20 goroutine, hai chủ, cùng tên miền gốc) chạy `go test ./internal/store/ -run TestClaimNamesRace -race -count=5`: `ok`. `_txlock=immediate` làm "đọc ảnh chụp, kiểm, chèn" tuần tự trên toàn máy, kể cả giữa CLI và `tpan serve`. Riêng đường `/apply` thì lọt, vì nó không xét `RootForeign` (C1).
- **`POST /sites` của user thường** dưới tên miền gốc mà người khác giữ bằng tên phụ: `404 site_not_found` (`TestSoatCuoiApplyRoute`, dòng 1). Thêm tên phụ là tên chính, tên phụ hay tên con của tên miền gốc của người khác cũng bị từ chối, xem `TestAliasAddForeignRootRefused` sẵn có. Lời từ chối là `name_unavailable` chung, không nêu site hay chủ. Lớp `nameRefusal` ở server dịch lại lần nữa cho chắc.
- **Tên chính của site trong thùng rác hay đang tạm khoá.** Dòng `site_names` chỉ xoá ở `DeleteSite` (xoá hẳn), nên tên vẫn bị giữ suốt lúc ở thùng rác hay tạm khoá. `nameSnapshot` còn hợp thêm bảng `sites` cho dòng thiếu. Khôi phục không phải giữ chỗ lại. Thao tác tên và luật trên site trong thùng rác hay tạm khoá đều bị từ chối (`namesSite`, `redirectSite`).
- **Dòng pending.** Chỉ nhận lại được khi cùng site, cùng chủ, cùng kiểu (`names.go:118`). Dòng quá hạn chỉ bị xoá trong transaction của `ClaimNames`. `ActivateNames` khớp theo `site` và `name`, nên dòng đã sang site khác thì trả `ErrNotFound` chứ không kích hoạt nhầm. Đường còn hở là Plan cũ ở dòng lệnh, xem C4.
- **Chuẩn hoá tên.** `ParseDomain` hạ chữ thường, bỏ khoảng trắng hai đầu, từ chối chấm cuối, `_`, `*`, khoảng trắng giữa, ký tự vô hình, và địa chỉ IP (`203.0.113.7`, `127.0.0.1`, `0x7f.1`). Unicode được đổi sang punycode trước khi thành khoá, nên `bücher.de` và `xn--bcher-kva.de` là một dòng. Chữ đồng hình (`exаmple.com` với chữ а Cyrillic) ra `xn--exmple-4nf.com`, một tên miền gốc khác, không đụng tên của nạn nhân trên máy.
- **Public suffix, cả ICANN lẫn private.** Gốc tính đúng cho các tên sau (test tạm `TestSoatCuoiPSL`):
  - `a.公司.cn` ra `a.xn--55qx5d.cn`. Bảng PSL của `x/net` ở dạng punycode, khớp với đầu ra của `ParseDomain`.
  - `公司.cn`, `github.io`, `blogspot.com`, `b.ck` đều là `domain_public_suffix`.
  - Ngoại lệ `www.ck` là tên đăng ký được, còn `x.a.co.uk` có gốc `a.co.uk`.
  - Phần private tách đúng: `x.s3.amazonaws.com` là gốc riêng, khác `amazonaws.com`. Đó là ý định của PSL; ghi lại để chủ dự án biết.
- **Route mới.** Bảy route trong `routes.go:248-254` dùng `ownerSite`, và `/domains` (dòng 255) là `anyone`. Ba vai đã được kiểm thật cho cả tám route bằng `go test ./internal/server/ -run 'TestPhanQuyenMoiRoute|TestXemNhuChiDoc|TestAuthzTable'`: đều PASS (admin qua, user khác nhận 404, phiên "xem như" nhận 403 khi ghi).
- **Thao tác trên tên hay luật của site khác qua site của mình.** `DELETE /sites/<site của A>/names/<tên phụ của B>` không làm được gì: `aliasRow` chỉ tìm trong dòng của site A, nên trả `alias_not_found`. Đổi kiểu tên cũng vậy. Luật chuyển hướng tìm theo `st.Names.Rules` của site A, và store xoá theo cặp `site = ? AND id = ?`.
- **`GET /domains`** lọc theo `scopeOf(r)`.
- **`?host=` của route explain.** Tên của site khác và tên không có đều trả cùng `404 name_unknown` (`route_m19.go:43-52`); tên phụ còn pending cũng vậy.
- **MCP.** `list_site_names` và `list_site_redirects` chỉ đọc; phiên MCP là của chủ máy. Bộ che ở mức mặc định (`LevelAll`) che cả tên phụ chưa học và host của đích chuyển hướng. Test tạm `TestSoatCuoiRedactTenPhu`: `site.alias.dns:example.org ... target https://khach-khac.example.info/x` thành `site.alias.dns:site-b.example ... target https://site-c.example/x`.
- **Nâng cấp.** Migration 1900 chép đúng mỗi tên chính. Vhost cũ (`f6a68b9`) chỉ khai `server_name {{.Domain}}`, nên không có tên `www.` nào đang phục vụ mà DB không biết. `sites.owner` có `DEFAULT 'admin'` từ 1300, nên `site_names.owner` không rỗng. `site_redirects` có `ON DELETE CASCADE` và DSN bật `foreign_keys(1)`. Không migration nào sau 1900 dựng lại bảng `sites`, nên luật không bị xoá theo.
- **Lấy cookie phiên admin bằng tên phụ trùng tên của panel.** Không gặp ở cấu hình mặc định. Panel vào bằng `https://<IP máy>:8443` hoặc đường hầm SSH tới 127.0.0.1 (`cmd/tpan/setup.go`), và cookie `__Host-tpan` gắn với host đó. Tên phụ không thể là IP (`ParseDomain` từ chối), nên site của user không nhận được cookie này.
  - Nếu admin tự vào panel bằng một hostname mà tên miền gốc của nó chưa ai giữ trên máy, user có thể lấy hostname đó làm tên (cookie không tách theo cổng). Rủi ro này có từ trước M19, qua tên chính. Ghi lại để chủ dự án biết; nên giữ riêng hostname của panel nếu sau này có cấu hình đó.
- **`tpan apply` của admin dừng cả lô khi một tên bị giữ trước.** Nhánh câu 3 nêu điểm này (`apply.go:495`). Không tính là phát hiện vì đó là quyết định có chủ ý: chú thích ở `apply.go:484-486` ghi "Một tên không giữ được (đua với thao tác khác từ lúc xem trước) thì không chạy gì". Bản xem trước đã báo đúng dòng bị từ chối; cả lô chỉ dừng khi có đua giữa lúc xem trước và lúc chạy.
- **Ai được tạo site, tên.** Mọi chỗ gọi `PrepareAdd`, `PrepareApply`:
  - `POST /sites` và các route M19 đặt `NotAdmin`; `/apply` thì không (C1).
  - Admin: `tpan site add`, `tpan apply`, cloud-init chạy bằng root; Plan của AI chỉ admin duyệt (`/plans/*` là `adminOnly`).

### Chèn cấu hình, mất luật chặn, chuyển hướng mở (câu 2)

Nhánh soát câu 2 chạy nginx thật trong container (`nginx:1.30-alpine`, thư mục cấu hình gắn chỉ đọc) và `httpd` 2.4.67 của máy phát triển ở cổng 18080, không root. Log ở `tmp-phien/soat-cuoi-q2/`.

- **Fuzz** 30 giây mỗi hàm, đều PASS: `FuzzParseRedirect` 6,7 triệu lần, `FuzzVhostRedirects` 7,5 triệu lần (`fuzz-validate.log`, `fuzz-templates.log`).
- **Ký tự cú pháp trong nguồn và đích bị chặn.** Gồm `; { } " ' \ $ # %`, khoảng trắng, xuống dòng, cờ Apache `[...]`, `$1`, `%{...}`. Chặn ở hai lớp: danh sách cho phép (`internal/validate/redirect.go:174-202`) và lớp kiểm lại `m19Rule` lúc vẽ (`internal/templates/m19.go:104`). Không biến nào của nginx hay Apache lọt vào đích, nên đích không do request quyết định.
- **Host của đích sai dạng bị từ chối** (`redirect.go:116-142`). Gồm host có `@ # \ %`, dấu chấm toàn chiều rộng, khoảng trắng, và cổng `0`, `65536`, cổng rỗng, `:8080:9`.
- **Đích tương đối không thành chuyển hướng mở.**
  - Đích dạng `//evil` bị từ chối (đoạn rỗng).
  - Đích tương đối được vẽ thành URL tuyệt đối theo tên chính (`m19.go:126-128`), nên host trong `Location` không lấy từ request. Apache thật với `Host: example.com:8080` vẫn trả `Location: http://example.com/moved`.
- **Khối chuyển tên phụ giữ host cố định.** Thử `GET //evil.example.net/`, `/\evil.example.net/` và request line dạng URL tuyệt đối. nginx trả `Location: http://example.com//evil.example.net/`, Apache trả `.../%5cevil...`, đều trên host của site. Dạng có `@` thì Apache trả 400, nginx 444.
- **Không che được thử thách ACME hay file ẩn.** Mọi đoạn nguồn bắt đầu bằng `.` bị từ chối. Đo thật trên cả nginx và Apache: token ACME đi qua tên phụ trả 200, `/.env` qua tên serve trả 403.
- **Luật trên đường PHP trong uploads không chạy được PHP.** Nguồn `/wp-content/uploads/x.php` chỉ đổi 403 thành 302 (query `?a=1` được giữ). `X.php` và `x.php/` vẫn 403/404; `//x.php` và `%78.php` cũng chỉ ra 302.
- **Đổi stack hỏng vì cú pháp luật: không gặp.** Bộ thử: 50 luật cực trị (nguồn 255 byte, đích khoảng 770 byte có cổng 65535, query `a=&`, host punycode) cộng 24 tên.
  - `httpd -t` báo `Syntax OK` cho cả 8 biến thể: thường, SSL, lồng, tạm khoá, chế độ `tat`. Dòng dài nhất 6087 byte.
  - `nginx -t` với các luật đó báo `successful` ở bucket 64. Chỉ tên dài làm hỏng (C8).
- **Tên phụ không mang wildcard hay regex.** Tên dạng `*`, `_`, `~`, `.x`, IP, chấm cuối bị `ParseDomain` (`validate.go:73-170`) và regex `m19Name` (`m19.go:21`) chặn. Vậy `server_name` và `ServerAlias` không bao giờ có wildcard hay regex do user đưa vào.
- **`RedirectProbe` và `NameProbe`** không theo chuyển hướng (curl không có `-L`) và gọi qua `--resolve` 127.0.0.1.
- **Giao diện mới** (`Names.tsx`, `Redirects.tsx`, `ten-mien/index.tsx`) không có `dangerouslySetInnerHTML`, cũng không có `href` lấy từ dữ liệu.

### Chứng chỉ, site của người khác, thao tác toàn máy (câu 3)

- **Lineage `--cert-name`.** Đặt theo tên chính (duy nhất, đã qua `validate`). `SetInCert`, `CertbotDelete` và `sslCertHasOthers` chỉ đụng site của chính mình.
- **Chứng chỉ cũ mất tên đang có.** Tên đang trong chứng chỉ mà bước thử hỏng thì giữ chứng chỉ cũ (S3); chạy lại thì Check bỏ qua.
- **Tên lạ trên cổng 443.** Khối 443 chỉ lấy tên `in_cert` của chính site; tên khác rơi về `tpan-default-ssl`.
- **SSRF qua `NameProbe` và `site.alias.http` của health.** Cả hai dùng `--resolve` về 127.0.0.1.
- **Một tên hay một luật làm vẽ lại toàn máy hỏng.** `splitNames` và `redirectLines` không lỗi với dữ liệu đã qua kiểm, và đích dài nhất 772 ký tự (dưới 1024). Ngoại lệ là tên dài trên Apache khi đổi sang nginx (C8).
- **`health --fix` sửa vhost của người khác.** Các phép kiểm M19 không có bước sửa tự động.
- **Trần 5 job.** Đếm theo chủ site, và job chạy tiếp mang đúng owner. Còn hở đường gọi SSL tay (C3).
- **Site tạm khoá có tên phụ.** Cổng 80 phủ mọi tên, cổng 443 chỉ tên đã phủ, không vẽ luật. Bỏ khoá thì vẽ lại đủ tên.
- **Khối chuyển tên** vẫn phục vụ thử thách ACME, nên gia hạn không gãy.
- **Plan cũ giữa hai request API.** Không có cửa sổ, vì `planJob` giành cờ "một job một lúc" trước khi Prepare (`api_ssl.go:66`).

## Trả lời của phiên chính (25/11/2026)

- **C1 (sửa).** Gốc lỗi là giá trị mặc định "zero là admin" của `core.Sites.NotAdmin` khi server dựng `Sites`. Sửa tận gốc: `apiSites()` mặc định `NotAdmin: true` (chặt); chỉ handler biết vai (tạo site, apply, tên phụ) đặt lại theo phiên. Test `TestApplyUserRespectsAliasRoot`. Plan do AI đề xuất (`apiPlanner`) giữ luật chặt.
- **C2 (sửa cùng C1).** Sau sửa, `/apply` của user thường trả `site_not_found` đúng lời của `POST /sites` (cùng test).
- **C3 (để rủi ro).** `POST .../ssl` và `tpan ssl issue` gọi tay không có hạn mức là hành vi có từ M5; trần tự chạy của M19 chặn đường tự động. Ghi ở mục "Giới hạn còn lại" của KE-HOACH 6.17 và giải thích.
- **C4 (để rủi ro).** Mọi Plan của TPAN dựng lúc Prepare và lấy khoá lúc Execute; cửa sổ chỉ mở khi admin để một lệnh dòng lệnh chờ xác nhận trong lúc tên đổi chủ qua API. Giữ chỗ (`ClaimNames`) trong Execute vẫn chặn được `alias.add`; ca còn lại là `alias.rm`/`alias.set` cũ ghi lại vhost cũ. Sửa đúng là dựng lại nội dung vhost dưới khoá; để mốc sau.
- **C5, C6, C7, C10 (nhẹ, để sau).** Ghi lại ở đây; không cái nào cho user chiếm tên hay chèn cấu hình.
- **C8 (sửa).** Tên phụ dài hơn 46 byte bị từ chối (`name_too_long`, mọi stack, vì máy Apache có thể đổi sang nginx); tên `www.` mặc định dài thì bỏ qua kèm ghi chú. Không đổi `server_names_hash_bucket_size` toàn máy (đổi hành vi máy nâng cấp). Test `TestAliasNameTooLong`, `TestSiteAddLongNameSkipsWWW`, `TestCheckClaimsNameTooLong`.
- **C9 (sửa).** Tên `www.` mặc định qua cùng phép kiểm "vhost khác đã khai" như `alias.add`; `DeclaredFile` hiểu wildcard nginx (`*.x`, `.x`, `x.*`) và Apache. Test `TestSiteAddWWWDeclaredElsewhere`, `TestAliasAddDeclaredWildcard`, `TestDeclaredNamesWildcard`.
- **Ghi chú quy trình.** Nhánh soát câu 2 chạy nginx trong một container dùng một lần và `httpd` trên máy Mac, không root, không đụng máy thật hay VPS; chỉ để đo cú pháp. Ghi lại để chủ dự án biết.

Sau các bản sửa: `make check` xanh; e2e bước 22 chạy lại trên mã đã gộp `dev`: 373 đạt, 0 không đạt.
