# M28 đợt 2 nhóm B: tên con của domain người khác (P4) và chủ của job suy từ tên (P7)

Đọc kèm: `28b-m28-dot-2-nhom-a.md` (cùng đợt, nhóm A; có mục P1 và P2 và quy ước K0, K1, K2), `13-m13-nhieu-nguoi-dung.md` (vai `admin` và `user`, chủ của site, DB rời, job). Đặc tả: mục 6.28 của `docs/KE-HOACH.md`. Quyết định và lý do: `docs/thay-doi/M28.md` mục 37 đến 41.

Cách đọc: "Nói trong một phút" là thứ bạn nói được khi bị hỏi đột xuất. Muốn trả lời sâu thì đọc mục 3. Muốn tự chạy lại thì đọc mục 2. Câu hỏi của người review ở mục 7, đáp án ở cuối file.

Nhân vật trong log, giống nhóm A: `khach1`, `khach2` là hai tài khoản panel vai `user`; `admin` là vai quản trị. Mọi tên miền dùng `example.com`, `example.net`, `example.org`.

## Nói trong một phút

Nhóm A vá năm lỗ. Nhóm B vá hai lỗ nhỏ hơn trong cùng bản rà soát v0.2.0, và sửa một phép thử của nhóm A:

- **P4.** Khách A (vai `user`) tạo được site tên `www.<site của khách B>` hay `mail.<site của khách B>`. Bản cũ chỉ so đúng nguyên tên, mà `www.example.net` khác `example.net` nên qua. Hậu quả: A phục vụ nội dung dưới tên của B, và xin được chứng chỉ Let's Encrypt cho tên đó. Bản vá: site MỚI của user thường không được cùng "tên miền đăng ký được" (eTLD+1) với site của người khác. Trùng thì trả 404 `site_not_found`, y như site không tồn tại. Hệ quả có chủ ý: hai khách không có hai tên con của cùng một domain gốc.
- **P7.** Mỗi job (việc tpan chạy) có một "chủ" quyết định ai được xem nó. Bản cũ suy chủ từ cái tên mà job nhắm tới (`target`). Admin khoá user `khach1_p7y`, nhưng `khach1` cũng đặt được một DB rời tên đầy đủ là `khach1_p7y`, nên bị coi là chủ của job khoá user và đọc được nó. Bản vá: chỉ suy chủ từ tên với những loại job kê đích danh; loại khác, kể cả loại thêm sau này, không suy. Job trên user và trên gói luôn thuộc admin. Migration 2800 sửa dòng cũ. Thêm: tên user không được trùng tên DB rời và ngược lại.
- **Sửa phép thử P2.** Phép thử P2 của nhóm A trông xanh nhưng phần lớn request không bao giờ chạm tới chỗ kiểm mật khẩu. Đã viết lại, mục 4.

Kết quả e2e trên bản vá `v0.2.2-100-g64ef6b7`: **192 khẳng định đạt, 0 không đạt**. Cùng bộ thử trên bản nhóm A (chưa có P4, P7): phần nginx 66 đạt, 7 không đạt, đúng các lỗ.

## 1. Mốc này làm được gì

- `khach1` xin tạo `www.example.net` hay `mail.example.net` khi `khach2` đã có `example.net`: nhận 404 `site_not_found`. Không có site nào được tạo.
- `khach2` tạo `www.example.net` của chính mình vẫn được. `khach1` tạo tên dưới một domain gốc chưa ai dùng (ví dụ `shop-xxxx.example.org`) vẫn được.
- `khach1` không tạo được DB rời trùng tên một user (409), và admin không tạo được user trùng tên một DB rời (409). Mã lỗi từng chiều ở mục 3.8.
- Job admin khoá user `khach1_p7y` ghi chủ `admin`. `khach1` không thấy nó trong `GET /jobs` và `GET /jobs/<id>` trả 404.
- Job tạo DB rời `khach1_p7z` của chính `khach1` vẫn thuộc `khach1`, `khach1` đọc được (200).
- Phép thử P2 giờ thật sự đẩy các request tới chỗ so mật khẩu (49 đến 53 lần so, còn lại 429), thay vì dừng ở hàng chờ.

## 2. Chạy thử

Mọi output dưới đây trích nguyên văn từ hai file log trong repo. Phiên viết file này không dùng máy ảo, nên mọi lệnh "tự chạy" ghi là **chưa chạy trong phiên viết**.

- Bản vá: `docs/bang-chung/M28-dot2b.log`, gọi tắt "log bản vá". Bản build `v0.2.2-100-g64ef6b7`.
- Bản nhóm A: `docs/bang-chung/M28-dot2b-truoc-ban-va.log`, gọi tắt "log chưa vá". Bản build `v0.2.2-92-gfc7d33a`. "Chưa vá" ở đây nghĩa là chưa có P4, P7; nhóm A thì có rồi.
- Script: `scripts/e2e-m28-dot2/p4.sh`, `p7.sh`, `p2.sh`. P4 và P7 chỉ chạy ở phần nginx của bộ e2e (`scripts/e2e-m28-dot2.sh` dòng 197, 198); Apache và nâng cấp không chạy chúng.

Chạy lại từ Mac (dùng máy ảo `tpan-m28`, chưa chạy trong phiên viết):

```bash
make e2e-m28-dot2 P="nginx" L=docs/bang-chung/M28-dot2b-thu-lai.log
```

Tên `L=` đổi để không đè log gốc.

### 2.1 P4: tên con của domain người khác

Dựng (log bản vá dòng 226 đến 234): `khach2` tạo thật site `example.net` qua API, đợi nó `active`:

```
  khach2 POST /sites: HTTP 202, 0.007772s (curl thoát 0; 28 là quá hạn 300s), câu trả lời 30 byte
  => ĐẠT: khach2 gửi tạo site example.net: 202 (HTTP 202)
  DOMAIN                                   TRẠNG THÁI PHP   SSL  TẠO LÚC          CHỦ            TẠM KHOÁ
  example.net                              active   8.3   không 2026-11-25 00:57 khach2         -
```

Rồi `khach1` xin BẢN XEM TRƯỚC (`POST /sites?dry_run=1`) của site add cho `www.example.net` và `mail.example.net`. Bản xem trước đủ để thấy lỗ vì nó đi qua đúng cánh cửa kiểm của `POST /sites`, và không tạo gì trên máy.

Log chưa vá (dòng 236 đến 242):

```
  khach1 POST /sites?dry_run=1: HTTP 200, 0.032016s (curl thoát 0; 28 là quá hạn 120s), câu trả lời 24793 byte
  => KHÔNG ĐẠT: khach1 không tạo được www.example.net: 404 (HTTP 200)
  => KHÔNG ĐẠT: lời là site_not_found, như site không tồn tại
  khach1 POST /sites?dry_run=1: HTTP 200, 0.029603s (curl thoát 0; 28 là quá hạn 120s), câu trả lời 24917 byte
  => KHÔNG ĐẠT: khach1 không tạo được mail.example.net: 404 (HTTP 200)
  => KHÔNG ĐẠT: lời là site_not_found, như site không tồn tại
```

Log bản vá (dòng 236 đến 243):

```
  khach1 POST /sites?dry_run=1: HTTP 404, 0.001974s (curl thoát 0; 28 là quá hạn 120s), câu trả lời 220 byte
  => ĐẠT: khach1 không tạo được www.example.net: 404 (HTTP 404)
  => ĐẠT: lời là site_not_found, như site không tồn tại
  khach1 POST /sites?dry_run=1: HTTP 404, 0.001765s (curl thoát 0; 28 là quá hạn 120s), câu trả lời 222 byte
  => ĐẠT: khach1 không tạo được mail.example.net: 404 (HTTP 404)
  => ĐẠT: lời là site_not_found, như site không tồn tại
```

Chú ý hai điều. Một: bản chưa vá trả 200 và một Plan dài 24 793 byte, tức panel sẵn sàng làm thật. Hai: lời 404 chỉ 220 byte, vì chỉ là một câu lỗi.

Ca hợp lệ (dòng 244 đến 248 của log bản vá):

```
  khach2 POST /sites?dry_run=1: HTTP 200, 0.030044s (curl thoát 0; 28 là quá hạn 120s), câu trả lời 24793 byte
  => ĐẠT: khach2 tạo được www.example.net của chính mình (bản xem trước 200, HTTP 200)
  khach1 POST /sites?dry_run=1: HTTP 200, 0.031447s (curl thoát 0; 28 là quá hạn 120s), câu trả lời 25865 byte
  => ĐẠT: khach1 tạo được shop-06e3641ea4.example.org (tên miền gốc không ai dùng; HTTP 200)
```

Chặn đúng người, không chặn nhầm: chủ thật của `example.net` vẫn tạo được tên con của mình, và người khác vẫn tạo được tên ở nơi chưa ai chiếm.

Tự chạy phần luật (không cần máy ảo, chạy ở Mac trong repo; chưa chạy trong phiên viết):

```bash
go test -count=1 ./internal/core -run 'TestRegistrableTaken' -v
```

Test này có bảng các ca: `www.b.example.net` trùng gốc với site của người khác (bị chặn), `example.co.uk` bị chặn vì có `shop.example.co.uk` của người khác (dù site đó đang trong thùng rác), `a.github.io` KHÔNG bị chặn dù ai đó đã có `b.github.io`, `www.a.example.com` không bị chặn vì chung gốc với site của chính người gọi. Sẽ thấy `PASS`.

### 2.2 P7: chủ của job

Dựng (dòng 253 đến 257 của log bản vá): admin tạo user `khach1_p7y` (vai user). `khach1` xin tạo DB rời tên `p7y`; tên đầy đủ của DB rời là `<tên chủ>_<tên>` nên thành `khach1_p7y`, trùng tên user.

Log chưa vá (dòng 253 đến 258):

```
  khach1 POST /dbs: HTTP 202, 0.010670s (curl thoát 0; 28 là quá hạn 120s), câu trả lời 30 byte
  => KHÔNG ĐẠT: khach1 không tạo được DB rời trùng tên user khach1_p7y: 409 (HTTP 202)
  job tạo DB: fbe99f8fb4d2af67, xong: succeeded
  dòng DB rời khach1_p7y trong tpan.db: 1
```

Log bản vá (dòng 253 đến 258):

```
  khach1 POST /dbs: HTTP 409, 0.003057s (curl thoát 0; 28 là quá hạn 120s), câu trả lời 262 byte
  => ĐẠT: khach1 không tạo được DB rời trùng tên user khach1_p7y: 409 (HTTP 409)
  dòng DB rời khach1_p7y trong tpan.db: 0
```

Rồi admin khoá user `khach1_p7y` qua `POST /users/khach1_p7y/suspend`, và script đọc cột `owner` của job đó trong `tpan.db`. Log chưa vá (dòng 265): `job khoá user: 8f357a3233b9805f, xong: succeeded, chủ ghi trong tpan.db: khach1`. Log bản vá (dòng 259 đến 264):

```
  admin POST /users/khach1_p7y/suspend: HTTP 202, 0.004008s (curl thoát 0; 28 là quá hạn 120s), câu trả lời 30 byte
  => ĐẠT: admin gửi khoá user khach1_p7y: 202 (HTTP 202)
  job khoá user: da5157c37a9cab41, xong: succeeded, chủ ghi trong tpan.db: admin
```

Rồi `khach1` tìm job đó. Log chưa vá (dòng 267 đến 272):

```
  job 8f357a3233b9805f trong GET /jobs của khach1: 1 lần
  khach1 GET /jobs/8f357a3233b9805f: HTTP 200, 0.001921s (curl thoát 0; 28 là quá hạn 60s), câu trả lời 607 byte
  => KHÔNG ĐẠT: khach1 không đọc được job khoá user của admin: 404 (HTTP 200)
  => KHÔNG ĐẠT: job đó không có trong GET /jobs của khach1
```

Log bản vá (dòng 266 đến 271):

```
  job da5157c37a9cab41 trong GET /jobs của khach1: 0 lần
  khach1 GET /jobs/da5157c37a9cab41: HTTP 404, 0.002052s (curl thoát 0; 28 là quá hạn 60s), câu trả lời 95 byte
  => ĐẠT: khach1 không đọc được job khoá user của admin: 404 (HTTP 404)
  => ĐẠT: job đó không có trong GET /jobs của khach1
```

Ở bản chưa vá, `khach1` đọc được thân 607 byte của job admin khoá một user. Log không in nội dung thân, nên mình không nói được trong đó có gì; KE-HOACH mục 6.28 mô tả đó là tên site, DB, đường dẫn, user Linux của người khác.

Ca hợp lệ (dòng 273 đến 281 của log bản vá): `khach1` tạo DB rời `p7z` (tên đầy đủ `khach1_p7z`, không trùng user nào).

```
  => ĐẠT: khach1 tạo được DB rời p7z (HTTP 202)
  job: 9cbaf6c3bc7f8e80, xong: succeeded
  => ĐẠT: chủ của job tạo DB khach1_p7z là khach1
  khach1 GET /jobs/9cbaf6c3bc7f8e80: HTTP 200, 0.002360s (curl thoát 0; 28 là quá hạn 60s), câu trả lời 2294 byte
  => ĐẠT: khach1 đọc được job của chính mình (HTTP 200)
```

Đây là lý do `jobOwnerLookup` (mục 3.3) vẫn phải tra chủ cho loại job `dbs.*`: nếu vá bằng cách tắt hẳn việc suy chủ thì `khach1` mất quyền đọc job của chính mình.

Tự chạy phần mã (Mac, chưa chạy trong phiên viết):

```bash
go test -count=1 ./internal/store -run 'TestCreateJobChuTheoLoaiJob' -v
go test -count=1 ./internal/core -run 'TestP7' -v
go test -count=1 ./internal/server -run 'TestP7' -v
```

Tự nhìn chủ của một job trên máy ảo (root; `tpan.db` là SQLite, mở chỉ đọc như `p7.sh` làm; chưa chạy trong phiên viết):

```bash
python3 -I -c '
import sqlite3
c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True)
for r in c.execute("SELECT id, kind, target, owner FROM jobs ORDER BY started_at DESC LIMIT 10"):
    print(r)'
```

Sẽ thấy mỗi job một dòng `(id, kind, target, owner)`. Job `user.suspend` phải có `owner` là `admin`.

## 3. Đi qua mã theo thứ tự chạy

### 3.1 P4 chặng 1: cửa của `POST /sites`

`internal/server/api_m13.go`, hàm `siteCreateOwner`. Hàm này quyết "chủ của site sắp tạo là ai, hay từ chối". Đoạn quan trọng (rút gọn):

```go
owner, found, err := s.deps.Store.SiteOwner(r.Context(), d.String())
...
taken := found && owner != id.Name
if !found {
	all, err := s.deps.Store.Sites(r.Context())
	...
	if taken, err = core.RegistrableTaken(all, d, id.Name); err != nil { ... }
}
if taken {
	apiFail(w, r, siteNotFound(d))
	return "", false
}
return id.Name, true
```

Đọc theo thứ tự:

1. Admin đi thẳng qua ở đầu hàm (không trích ở trên): admin luôn được, kể cả tạo hộ khách bằng `--owner`.
2. User thường mà đặt chủ khác mình thì 403 (luật từ M13).
3. `SiteOwner` hỏi bảng `sites` có đúng tên này chưa. Có và là của người khác: `taken`, 404. Có và là của mình: chạy lại site của chính mình, không xét luật mới.
4. Chưa có site tên đó (`!found`): MỚI vào luật P4. Lấy mọi dòng của bảng `sites` rồi hỏi `core.RegistrableTaken`.
5. `taken` thì trả `siteNotFound(d)`, cùng lời với "site này không tồn tại". Nếu là yêu cầu thật (không phải `dry_run`) thì ghi thêm một dòng nhật ký `audit` loại `site.add`, kết quả `site_not_found`; bản xem trước không ghi.

Hàm này chạy cho cả bản xem trước (đó là lý do e2e dùng `dry_run=1`): nếu xem trước trả 200 còn tạo thật trả 404 thì bản xem trước thành chỗ nói dối.

### 3.2 P4 chặng 2: luật eTLD+1 ở một chỗ

`internal/core/m28_p4.go` (54 dòng). Hai hàm.

`registrable` đổi một tên miền thành "tên miền đăng ký được":

```go
func registrable(d string) (string, bool) {
	r, err := publicsuffix.EffectiveTLDPlusOne(strings.ToLower(d))
	return r, err == nil
}
```

`publicsuffix.EffectiveTLDPlusOne` là hàm của thư viện `golang.org/x/net/publicsuffix` (go.mod dòng 14: `golang.org/x/net v0.63.0`). Ví dụ: `www.example.net` ra `example.net`; `shop.example.co.uk` ra `example.co.uk`; `a.github.io` ra `a.github.io` (vì `github.io` nằm trong danh sách). Tên không ra được (chính là một đuôi công cộng như `co.uk`, hay chỉ một nhãn như `com`) thì có `err` và trả `false`.

`RegistrableTaken` so:

```go
want, ok := registrable(d.String())
if !ok { return false, refuse(validate.CodeDomainInvalid, ...) }
for _, st := range all {
	if ownerOf(st) == caller { continue }
	if r, ok := registrable(st.Domain.String()); ok && r == want {
		return true, nil
	}
}
return false, nil
```

Ba điểm:

- Bỏ qua site của chính người gọi (`ownerOf(st) == caller`): `khach2` có `example.net` thì được thêm `www.example.net`. `ownerOf` coi owner rỗng của dòng cũ là admin.
- `all` là mọi dòng của bảng `sites`, không lọc trạng thái. Site trong thùng rác hay đang khoá vẫn giữ tên. Lý do: khôi phục site từ thùng rác dựng lại đúng tên đó, nên nếu cho người khác chen vào thì khôi phục sẽ đụng nhau.
- Tên là chính đuôi công cộng thì trả lỗi `domain_invalid` (không phải 404): "chỉ admin tạo được site với tên này". Admin không đi qua hàm này.

Chú thích `ponytail:` trong mã ghi cách làm đơn giản có chủ ý: duyệt cả bảng trong Go, đủ cho một máy vài trăm site. Nhiều hơn thì thêm cột eTLD+1 có chỉ mục.

### 3.3 P4 chặng 3: `POST /apply`

`internal/core/apply.go` dòng 176 đến 196 (hàm chuẩn bị lô apply). Apply là cửa thứ hai tạo site hàng loạt (`tpan apply -f sites.yaml`), nên luật phải có ở đây, không thì khách đi cửa này. Đoạn chính:

```go
taken, claimErr := false, error(nil)
if scope != "" && !found && !d.IsZero() {
	taken, claimErr = RegistrableTaken(all, d, scope)
}
...
case found && !mine(st), taken:
	item, err = refusedItem(item, refuse("site_not_found", ...
		"không tạo được site "+d.String()+" từ tài khoản này: domain đã được dùng"
```

`scope` là tên người gọi khi họ là user thường; admin có `scope` rỗng nên không vào luật (`scopeOf` ở `internal/server/routes.go` dòng 333: admin trả chuỗi rỗng). Điều khác so với `POST /sites`: apply giữ lời có sẵn của nó cho dòng trùng ("domain đã được dùng"), vì lời đó đã dùng cho trường hợp trùng đúng tên. Mã vẫn là `site_not_found`. Hai cửa chung một hàm `RegistrableTaken`, nên luật nằm ở một chỗ.

### 3.4 P7 chặng 1: chủ của job suy ở đâu

`internal/store/store.go`, `CreateJob`. Mọi job chạy qua `engine` đều ghi một dòng vào bảng `jobs` qua hàm này. Cột `owner` được chọn bằng `COALESCE`, nghĩa là "lấy giá trị đầu tiên không rỗng" (đoạn dưới là phần chủ của câu `INSERT`, các chú thích bên phải là của mình):

```sql
COALESCE(NULLIF(?, ''),                             -- 1. chủ do người gọi đặt sẵn (job.Owner)
  (SELECT owner FROM sites WHERE domain = ?),       -- 2. chủ của site có domain là tên đó
  (SELECT owner FROM databases WHERE name = ?),     -- 3. chủ của DB rời có tên là tên đó
  NULLIF(?, ''), 'admin')                           -- 4. người bấm (Actor), cuối cùng là admin
```

Hai tham số thứ 2 và 3 là `siteName`, `dbName` do `jobOwnerLookup(job.Kind, job.Target)` trả về. Đây là chỗ cũ sai: bản cũ truyền `job.Target` vào CẢ HAI chỗ cho mọi loại job. Job `user.suspend` có `Target = "khach1_p7y"` là tên user, nhưng nó cũng là tên đầy đủ của một DB rời của `khach1`, nên mục 3 trả `khach1`, thắng mục 4 (người bấm là admin).

Giá trị rỗng không ra dòng nào: `SELECT owner FROM sites WHERE domain = ''` trả NULL, `COALESCE` đi tiếp. Nên "không tra" chỉ cần truyền chuỗi rỗng.

### 3.5 P7 chặng 2: `jobOwnerLookup`, danh sách kê đích danh

Cùng file, hàm `jobOwnerLookup` (rút gọn):

```go
func jobOwnerLookup(kind, target string) (siteDomain, dbName string) {
	switch {
	case strings.HasPrefix(kind, "dbs."):
		return "", target
	case strings.HasPrefix(kind, "site."), strings.HasPrefix(kind, "db."):
		return target, ""
	}
	switch kind {
	case "backup.create", "backup.restore", "cage.enable", ..., "fix.php.restart", ... :
		return target, ""
	case "fix.db.user-limit":
		return "", target
	}
	return "", ""
}
```

Quy tắc: chỉ tra khi biết CHẮC `target` là gì.

- Loại `dbs.*` (DB rời): `target` là tên đầy đủ của DB rời, tra bảng `databases`.
- Loại `site.*`, `db.*` (DB của site), và một danh sách kê tên: `target` là domain của site, tra bảng `sites`.
- Mọi loại khác trả `"", ""`: không tra. Gồm `user.*` (target là tên user), `cage.sync` (target là tên gói), và mọi loại thêm sau này cho tới khi ai đó nghĩ tới chuyện nó có cần suy chủ không.

Vì sao danh sách "cho phép" chứ không phải danh sách "cấm": bản cũ là danh sách cấm ngầm (không cấm gì), và mỗi loại job mới là một lỗ chờ nổ. Danh sách cho phép đổi mặc định thành an toàn: loại mới không có chủ suy từ tên, rơi về người bấm rồi admin.

Vì sao liệt kê TỪNG loại `fix.*` thay vì cho cả tiền tố: ghi chú trong mã nói rõ, `validate.ParseDomain` nhận `php8.3-fpm` là một domain hợp lệ. `fix.service.start` có target là tên dịch vụ `php8.3-fpm`. Nếu cho tiền tố `fix.` tra bảng `sites`, một khách tạo site tên `php8.3-fpm` sẽ thành chủ của job admin khởi động lại dịch vụ đó. Nên chỉ những `fix.*` có target chắc chắn là domain mới có tên trong danh sách. Phần "nên siết `ParseDomain`" ghi ở mục 9.

### 3.6 P7 chặng 3: `adminOwned`, đặt chủ rõ ràng

`internal/core/users.go`, hàm `adminOwned` (dòng 471):

```go
func adminOwned(p *Planned) *Planned {
	p.owner = account.AdminName
	return p
}
```

Dùng bọc ở các Plan có target là tên user hay tên gói: `internal/core/users.go` dòng 440 (`PrepareSuspendUser`), 533 (`PrepareUnsuspendUser`), 615 (`PrepareTrashAll`) và `internal/core/cage.go` dòng 782, 839 (hai chỗ dựng Plan áp trần của gói, mang comment "P7: Target là tên gói hay tên user"). Mỗi chỗ gọi: `adminOwned(newPlanned(...))`.

Tại sao cần khi store đã không tra? Đó là "hai lớp": comment trong mã nói "đặt rõ ở đây để không phụ thuộc vào danh sách đó". Lớp store là danh sách có thể bị sửa sau này; lớp core đặt chủ ngay trên Plan, nên chủ của các việc này không còn phụ thuộc vào danh sách.

### 3.7 P7 chặng 4: migration 2800, sửa dòng cũ

`internal/store/migrations/2800_job_owner.sql` (10 dòng, chỉ một câu SQL):

```sql
UPDATE jobs SET owner = 'admin' WHERE kind LIKE 'user.%' OR kind = 'cage.sync';
```

Các máy đã chạy bản cũ có sẵn dòng job `user.*` ghi chủ là khách. Chỉ vá mã mà không sửa dòng cũ thì khách vẫn đọc được các job lịch sử. Migration chạy một lần: `internal/store/store.go` (từ dòng 162) ghi mỗi bản đã áp vào bảng `schema_migrations`, và bản nào chưa có trong bảng thì được áp khi `tpan` mở DB. Con số 2800 vì khoảng 2800 đến 2899 là của mốc M28 (comment đầu file). Câu SQL ghi `admin` cả vào những dòng vốn đã là `admin`, vô hại.

Câu SQL chỉ chạm `user.%` và `cage.sync`; nó không đụng loại job nào khác. Trong tài liệu của repo mình không thấy lý do viết ra cho ranh giới này ngoài comment đầu file (hai loại đó là loại mà Target là tên user hay tên gói), nên nếu bị hỏi "còn loại khác thì sao" thì trả lời đúng như vậy.

### 3.8 P7 chặng 5: cấm trùng tên, hai chiều

Hai chỗ, mã lỗi khác nhau theo chiều.

Tạo user (`internal/core/users.go` khoảng dòng 220 đến 235): sau khi kiểm user đã tồn tại, gọi `a.DBs.Store.StandaloneDB(ctx, name)`. Có DB rời tên đó (kể cả DB trong thùng rác: khôi phục sẽ dựng lại chỗ trùng) thì từ chối với mã `user_exists` (HTTP 409).

Tạo DB rời (`internal/core/dbs.go` khoảng dòng 226 đến 237): gọi `d.Accounts.User(ctx, name)`. Có user tên đó (kể cả user đang khoá) thì từ chối với mã `db_exists` (HTTP 409).

Nghĩa là mã lỗi theo THỨ ĐANG ĐƯỢC TẠO: tạo user mà trùng thì `user_exists`; tạo DB mà trùng thì `db_exists`. Log bản vá dòng 253 khớp: `khach1` tạo DB, HTTP 409.

KE-HOACH gọi đây là "phòng thủ thêm": lỗ P7 chính đã đóng ở mục 3.5 và 3.6. Cấm trùng tên để hai thứ của hai người khác nhau không mang cùng một cái tên trong màn hình và nhật ký. Nó không thay được hai lớp trên, vì user và DB rời đã có từ trước bản vá vẫn có thể trùng nhau (mục 8).

## 4. Sửa phép thử P2

Phép thử P2 của nhóm A (mục 2.5 của `28b-m28-dot-2-nhom-a.md`) nhắm vào trần argon2id: tối đa 2 lần kiểm mật khẩu cùng lúc, mỗi lần 64 MiB. Muốn chứng minh trần đó, phép thử phải cho NHIỀU request thật sự tới được chỗ kiểm mật khẩu. Bản cũ không làm được. Thợ viết giải thích nhóm A phát hiện ra, ghi ở `docs/thay-doi/M28.md` mục 40.

**Vì sao bản cũ không chạm trần.** Bản cũ cho mỗi request một địa chỉ nguồn riêng và gọi tới CHÍNH địa chỉ đó (nguồn và đích cùng một IP của máy). Bản vá P1 (hàm `selfConn` trong `internal/server/auth_m28.go`, dòng 98) coi kết nối mà địa chỉ nguồn trùng địa chỉ panel nhận kết nối là "từ chính máy", giống loopback, và khoá theo UID của tiến trình đầu bên kia: `uid:0` (root, người chạy script). Tức mọi request chung MỘT khoá nguồn. Khoá đó chỉ cho tối đa 4 lượt (đang chạy cộng chờ), và khoá sau 5 lần sai. Nên phần lớn request nhận 429 từ hàng chờ, hay 423 từ khoá, mà chưa bao giờ xin chỗ argon2. Số liệu nhóm A với bản cũ, N = 60 (mục 2.5 của file nhóm A): 3 câu 401, 2 câu 423, 55 câu 429. Chỉ 3 lần được so mật khẩu; "RSS không tăng" xanh vì hầu như chẳng có gì chạy.

**Bản mới** (`scripts/e2e-m28-dot2/p2.sh`): nguồn là `10.98.0.<n>` (địa chỉ gắn tạm vào giao diện `lo`, mỗi request một địa chỉ), đích luôn là `127.0.0.1`. Địa chỉ nguồn khác địa chỉ panel nhận (`127.0.0.1`) và không phải loopback, nên `sourceKey` trả chuỗi IPv4 `10.98.0.<n>`: mỗi request một khoá riêng, qua được hàng chờ, và đi tới semaphore argon2. N không còn cố định: script đo thời gian một lần đăng nhập sai (`t1`) rồi tính `N = 12 / t1 + 1`, kẹp trong khoảng 20 đến 150. Ý: tổng việc `N × t1` chia 2 chỗ phải vượt hẳn 3 giây chờ, để chắc có request bị 429 do argon2. Thêm khẳng định mới: "hơn 5 câu 401". Một khoá chỉ có 5 lần sai, nên hơn 5 câu 401 chỉ xảy ra khi các request không chung khoá. Đây là bằng chứng rằng 429 đến từ trần argon2 chứ không từ hàng chờ.

Số liệu:

| | N | 401 | 429 | RSS tăng |
|---|---|---|---|---|
| Bản cũ, trên bản nhóm A | 60 | 3 (cộng 2 câu 423) | 55 | 0 MiB |
| Bản mới, trên bản nhóm A (log chưa vá, dòng 204 đến 222) | 150 | 49 | 101 | 197 MiB |
| Bản mới, trên bản vá (log bản vá, dòng 204 đến 222) | 138 | 53 | 85 | 191 MiB |

Phần log bản vá:

```
  một lần đăng nhập sai mất 0.087196s; N = 138
  mã      53 401
  mã      85 429
  thời gian: nhanh nhất 0.384018s, chậm nhất 3.369204s
  RSS đỉnh 357 MiB, tăng 191 MiB so với trước; 429: 85; dòng OOM mới trong dmesg: 0
  => ĐẠT: hơn 5 câu 401 (53): mỗi request một khoá nguồn, 429 đến từ trần argon2
```

Kiểm lại phép tính: `12 / 0.087196` ra 137,6; cắt phần lẻ rồi cộng 1 là 138. Ở bản chưa vá `t1 = 0.064782` nên tính ra 186, kẹp xuống 150. Thời gian chậm nhất 3,37 giây khớp với chờ tối đa 3 giây của `argonRun` cộng một lần argon2.

Ba điều nên nhớ khi bị hỏi:

- RSS tăng 191 đến 197 MiB, không phải 0. Trần 2 chỗ nghĩa là bộ nhớ sống của argon2 không quá 2 × 64 = 128 MiB; phần dư là bộ đệm và goroutine của 138 kết nối HTTPS đang chờ. Mình suy luận từ số, chưa đo riêng từng phần. Khẳng định của phép thử là "không quá 400 MiB", và 191 nằm trong đó.
- Bản cũ chạy vẫn xanh. Phép thử xanh không phải là bằng chứng bản vá hoạt động; nó phải kèm một phép chứng minh bước cần thử đã chạy (mục 5.3 điểm 7 của file nhóm A; ở đây đó là khẳng định "hơn 5 câu 401").
- Phép thử mới chưa chạm đường "tra UID từ `/proc/net/tcp` với bảng socket lớn". Đó vẫn chỉ có unit test `TestD1*`, như file nhóm A đã ghi.

## 5. Kiến thức nền

Mỗi khái niệm: ý chính, một chỗ trong mã, một lệnh để tự nhìn. Các lệnh chưa chạy trong phiên viết.

### 5.1 eTLD+1 và danh sách Public Suffix

Tên miền chia theo dấu chấm. "Đuôi" (TLD) là phần cuối: `com`, `net`. Nhưng có đuôi gồm hai nhãn: `co.uk`, `com.vn`. Và có những tên mà ai cũng đăng ký được một tên con riêng bên dưới: `github.io`, `appspot.com`. Danh sách Public Suffix (PSL) là một danh sách công khai, do cộng đồng giữ (publicsuffix.org), liệt kê mọi "đuôi công cộng" như vậy. "eTLD" là "effective TLD", đuôi công cộng theo danh sách. "eTLD+1" là đuôi cộng thêm một nhãn bên trái: đó là phần mà một người thật sự ĐĂNG KÝ và sở hữu.

Ví dụ: `www.example.net` có eTLD là `net`, eTLD+1 là `example.net`. `shop.example.co.uk` có eTLD `co.uk`, eTLD+1 `example.co.uk`. `a.github.io` có eTLD `github.io`, nên eTLD+1 chính là `a.github.io` (và `b.github.io` là một tên đăng ký khác).

Vì sao không chỉ "bỏ nhãn đầu" hay "lấy hai nhãn cuối": với `example.co.uk` hai nhãn cuối là `co.uk`, thành "một chủ" cho cả nước Anh. Danh sách chính là cách đúng.

Chỗ trong mã: `registrable` ở mục 3.2. Thư viện nhúng cả danh sách vào file chạy (không tải lúc chạy), nên danh sách đúng với phiên bản `golang.org/x/net` đã khoá trong `go.mod`; muốn danh sách mới thì nâng thư viện. Mình chưa kiểm ngày của bản danh sách trong v0.63.0.

Tự nhìn (Mac, trong repo, chưa chạy trong phiên viết): `go test -count=1 ./internal/core -run TestRegistrableTaken -v`, rồi đọc bảng ca trong `internal/core/m28_p4_test.go` dòng 38 đến 49.

### 5.2 Let's Encrypt, thử thách HTTP-01 và vì sao tên con là lỗ

Let's Encrypt cấp chứng chỉ HTTPS miễn phí cho ai chứng minh được mình kiểm soát một tên. Với kiểu HTTP-01, nó đặt một chuỗi bí mật, rồi gọi `http://<tên>/.well-known/acme-challenge/<chuỗi>`. Ai trả về đúng chuỗi thì được chứng chỉ cho tên đó. Trên máy dùng chung, web server chọn vhost theo tên trong request: yêu cầu cho `www.example.net` đi vào vhost có `server_name www.example.net`. Nếu vhost đó là của khách A thì A trả được chuỗi, và nhận chứng chỉ cho `www.example.net`: tên thuộc người khác.

Chỗ trong mã: không có mã nào trong mốc này gọi Let's Encrypt; lỗ nằm ở chỗ tpan để A có vhost mang tên đó. Mình mô tả đường tấn công theo mục 6.28 của KE-HOACH; bản vá chặn từ lúc tạo site.

### 5.3 Vì sao trả 404 chứ không báo "tên đã có người dùng"

Khi `khach1` hỏi về site của `khach2` (ví dụ xem, sửa), panel trả 404 như thể site không tồn tại, để `khach1` không dùng panel để dò xem tên miền nào đang có trên máy hay thuộc ai. P4 dùng cùng lời: `siteNotFound(d)`. Nếu trả lời "tên này của người khác" hay 403 thì lời đó đã tiết lộ ai là chủ, và người ta lặp qua nhiều tên để lập bảng chủ-tên miền của cả máy.

Điều 404 làm được là không nói tên thuộc ai. Mình ghi rõ cho chính xác: nó không biến câu trả lời thành giống hệt câu trả lời cho một tên rảnh (tên rảnh nhận 200 và Plan). Người review có thể hỏi chỗ này.

Chỗ trong mã: `siteCreateOwner` mục 3.1, hàm `siteNotFound`.

### 5.4 "Chủ" của job và cột `owner`

Bảng `jobs` trong `tpan.db` ghi mỗi việc tpan chạy: loại (`kind`), mục tiêu (`target`), người bấm (`actor`), chủ (`owner`). User thường chỉ thấy job có `owner` là mình (`GET /jobs`, `GET /jobs/<id>`); admin thấy tất cả. Chủ có thể khác người bấm: job `dbs.*` hay `site.*` mà target là site hay DB của một khách thì chủ là khách đó, dù người bấm là admin (đó là lý do `CreateJob` suy chủ từ site hay DB mà job nhắm tới, thay vì chỉ lấy người bấm). Job trên user thì chủ luôn là admin (mục 3.6).

Chỗ trong mã: `CreateJob` mục 3.4. Tự nhìn: lệnh Python ở mục 2.2.

### 5.5 `COALESCE` và `NULLIF` của SQL

`COALESCE(a, b, c)` trả giá trị đầu tiên không NULL. `NULLIF(x, '')` biến chuỗi rỗng thành NULL (để chuỗi rỗng không bị coi là "có giá trị"). Ghép lại: "dùng a nếu a có, rồi tới b...". Một truy vấn con `(SELECT owner FROM sites WHERE domain = ?)` không ra dòng nào thì cũng là NULL. Nhờ vậy `CreateJob` biểu diễn "thứ tự ưu tiên của chủ" trong một câu SQL, không cần nhiều vòng truy vấn trong Go.

Chỗ trong mã: `CreateJob`, mục 3.4. Tự nhìn: `sqlite3` có thể không có sẵn; dùng `python3 -I -c 'import sqlite3; print(sqlite3.connect(":memory:").execute("SELECT COALESCE(NULL, NULLIF(\"\", \"\"), \"x\", \"y\")").fetchone())'`, sẽ thấy `('x',)`.

### 5.6 Migration của tpan

Thư mục `internal/store/migrations/` chứa các file SQL đánh số (`2800_job_owner.sql`). Khi mở `tpan.db`, tpan chạy những file có số lớn hơn số đã ghi, theo thứ tự, mỗi file một lần. Cách đó đưa DB của máy đã cài lên đúng hình dạng mà mã mới cần, mà không cần xoá hay dựng lại. Khoảng số 2800 đến 2899 dành cho M28 (comment đầu file).

Chỗ trong mã: mục 3.7. Tự nhìn: `ls internal/store/migrations/ | tail -5` ở Mac.

## 6. Cách khác đã cân nhắc, và chỗ đã làm sai

### 6.1 Cách khác

Chỉ ghi những lựa chọn có trong `docs/thay-doi/M28.md` mục 37 đến 40 hay trong mã. Mình không bịa thêm phương án.

**P4: chỉ so đúng tên (như bản cũ) hay so theo eTLD+1.** Chọn eTLD+1, theo mẫu chính sách "Enforce subdomain ownership" mặc định bật của HestiaCP (nguồn: mục 38; mình chưa tra lại tài liệu của HestiaCP trong phiên viết). Cái giá là hệ quả có chủ ý: hai khách không có hai tên con của cùng một domain gốc. Ai tạo `www.X` trước thì chủ thật của `X` phải nhờ admin.

**P4: lời từ chối.** Chọn 404 `site_not_found` như site không tồn tại, thay cho lời nói rõ "tên này của người khác" (mục 5.3). `POST /apply` giữ lời có sẵn cho dòng trùng ("domain đã được dùng"), vì lời đó đã có từ trước cho ca trùng đúng tên (mục 38).

**P7: loại job mới mặc định có suy chủ từ tên hay không.** Chọn không suy, và kê đích danh từng loại được suy (mục 39). Kể cả từng loại `fix.*`, không cho cả tiền tố, vì ví dụ `php8.3-fpm` (mục 3.5).

**P7: thêm cấm trùng tên user và DB rời.** Mục 39 gọi là "phòng thủ thêm": lỗ chính đã đóng bằng danh sách và `adminOwned`, cấm trùng là lớp thứ ba, dùng lại mã lỗi có sẵn `user_exists`, `db_exists`.

**P2: sửa phép thử hay để nguyên.** Chọn sửa (mục 40 và mục 4 ở đây).

### 6.2 Chỗ đã làm sai trong nhóm B

1. **Lần chạy đỏ đầu của P4, P7 hỏng vì khoá đăng nhập của chính script** (`docs/thay-doi/M28.md` mục 41). P1 và P2 bản cũ chạy trước để lại khoá nguồn `uid:0`, vì curl của root nối qua loopback. P4 và P7 sau đó đăng nhập từ cùng root, nhận 423 (khoá), và các khẳng định đỏ không do mã P4 hay P7. Sửa: hai script gỡ mọi khoá bằng `tpan admin unlock` trước khi đăng nhập (dòng đầu sau `can_may`). Bài học: một phép thử xanh hay đỏ chỉ có nghĩa khi nó không dính trạng thái của phép thử trước.
2. **P2 bản cũ không chạm trần argon2** (mục 4 ở đây). Phép thử xanh mà không chứng minh điều nó nói; do thợ viết giải thích nhóm A chỉ ra (mục 40 của `docs/thay-doi/M28.md`). Số liệu trong log nhóm A khớp: 3 câu 401, 2 câu 423.
3. **Lúc phát hiện P4, P7 chỉ đo trên "máy giả".** Mục 6.28 ghi: hai lỗ này bản rà soát mới đo trên máy giả, nên nhóm B tái hiện trên máy ảo thật trước khi vá (log chưa vá ở trên là bằng chứng: 7 khẳng định không đạt, đúng các lỗ P4 và P7).

## 7. Tự kiểm

Đáp án ở cuối file. Câu 3, 5 và 9 cần chạy lệnh mới trả lời chắc được; câu 3 và 9 cần máy ảo.

1. `khach1` tạo `www.example.net` khi `khach2` có `example.net`: kết quả, và vì sao đây là lỗ (hai hậu quả)?
2. `khach2` tạo `www.example.net` (khi chính `khach2` có `example.net`) thì sao? `a.github.io` khi người khác có `b.github.io` thì sao? Vì sao hai ca này khác ca 1?
3. (Máy ảo hay Mac) Chạy `go test -count=1 ./internal/core -run TestRegistrableTaken -v` và cho biết ca nào trong bảng trả `false` dù domain có vẻ liên quan tới site của người khác.
4. Vì sao trả 404 mà không phải 403 hay "tên này đã có người dùng"? Cái 404 này giấu được gì và KHÔNG giấu được gì?
5. (Mac) Tìm trong `internal/core/apply.go` chỗ `POST /apply` dùng `RegistrableTaken`. Admin có đi qua luật này không, và ở đâu thấy điều đó?
6. Hai khách A và B cùng muốn `www.X` và `shop.X`. Ai tạo trước thì sao, ai phải nhờ admin? Đây là lỗi hay có chủ ý?
7. Bản cũ suy chủ của job từ đâu, và vì sao `khach1` thành chủ của job admin khoá user `khach1_p7y`? Chỉ ra đúng chỗ trong SQL.
8. Vì sao `jobOwnerLookup` là danh sách "cho phép" kê đích danh chứ không phải "cấm" vài loại? Ví dụ `php8.3-fpm` ở đó nói lên điều gì?
9. (Máy ảo) Sau khi admin khoá một user, làm sao biết `owner` của job đó trong `tpan.db`? Giá trị đúng là gì? Và job tạo DB rời của chính `khach1` có `owner` là gì?
10. Phép thử P2 bản cũ vì sao không chạm trần argon2? Bản mới đổi hai điều gì, và khẳng định nào chứng minh nó đã chạm?

## 8. Nâng cấp từ bản cũ

Máy đã cài v0.2.0, v0.2.1 hay v0.2.2 rồi `tpan self-update` lên bản này.

- **P4** là mã của panel, có hiệu lực ngay khi chạy bản mới. Nhưng nó chỉ chặn việc TẠO site mới. Site kiểu `www.<site người khác>` đã được tạo từ trước không bị gỡ và vẫn chạy. Admin muốn dọn thì tự xem danh sách site (`tpan site list` có cột chủ) và xử lý.
- **P7** có migration 2800: chạy một lần khi `tpan` mở DB lần đầu sau khi nâng, sửa chủ của job `user.*` và `cage.sync` đã ghi về `admin`. Không đổi gì trên đĩa ngoài `tpan.db`.
- Cặp user và DB rời đã trùng tên từ trước bản vá không bị đổi tên hay xoá. Chỉ cấm tạo cặp mới. Mã thì an toàn với cặp cũ nhờ mục 3.5 và 3.6.
- Phần nâng cấp của bộ e2e (`nang-cap.sh`) không chạy P4 và P7 (chỉ phần nginx chạy); log ghi 80 khẳng định đạt cho phần đó, kiểm các lỗ của nhóm A.
- Lùi về bản cũ: chưa chạy, chưa kiểm. Migration 2800 chỉ ghi `admin` vào cột `owner` đã có sẵn, nhưng mình không khẳng định bản cũ đọc ổn khi đã qua migration.

## 9. Còn hở

Chép từ mục 6.28 của `docs/KE-HOACH.md`:

- `validate.ParseDomain` nhận cả tên như `php8.3-fpm` (nhãn cuối không phải TLD thật); không mở lỗ nào đã biết (P7 đã kê đích danh loại `fix.*` có `target` là domain) nhưng nên siết sau.
- F1, P06, T3 và các phát hiện vừa còn lại của rà soát chưa làm.

Nếu bị hỏi "đợt 2 đã vá hết chưa": chưa. Hai gạch trên, và các mục "còn hở" trong `28b-m28-dot-2-nhom-a.md`.

## Đáp án

1. 404 `site_not_found`, không tạo gì (log bản vá dòng 236 đến 238). Bản cũ trả 200 kèm Plan (log chưa vá dòng 236 đến 238). Hai hậu quả nếu cho qua: `khach1` phục vụ nội dung dưới tên `www.example.net` của người khác; và `khach1` xin được chứng chỉ Let's Encrypt cho tên đó, vì thử thách HTTP-01 đi vào vhost của `khach1` (mục 5.2).

2. Cả hai đều cho qua (200). `khach2` tạo `www.example.net`: `RegistrableTaken` bỏ qua site có chủ là người gọi (`ownerOf(st) == caller`), nên không có site của người khác cùng eTLD+1. Log bản vá dòng 244 đến 246. `a.github.io`: `github.io` là một đuôi công cộng trong PSL (phần private), nên eTLD+1 của `a.github.io` là chính nó, khác `b.github.io`: hai tên đăng ký của hai người khác nhau. Khác ca 1 vì ca 1 hai tên chung eTLD+1 (`example.net`) và hai người khác nhau. Có trong `TestRegistrableTaken`.

3. Chưa chạy trong phiên viết; đọc bảng ca trong `internal/core/m28_p4_test.go` dòng 38 đến 49 thì các ca `false` gồm: `a.github.io` (do `github.io` là đuôi công cộng), `www.a.example.com` và `example.com` (cùng gốc với site của chính người gọi `usera`), và `other.example.co.uk.example.com` (eTLD+1 là `example.com`, không phải `example.co.uk`). Sẽ thấy `PASS` cho cả hàm.

4. Cùng lời với site không tồn tại để không lộ tên đó thuộc ai (mục 5.3): nếu panel trả 403 hay "tên này của người khác" thì lời đó cho phép lặp qua nhiều tên để biết tên nào thuộc ai. Nó giấu: ai là chủ. Nó KHÔNG giấu: tên đó đang bị chiếm, vì tên rảnh nhận 200 kèm Plan.

5. `internal/core/apply.go` dòng 177 đến 178: `if scope != "" && !found && !d.IsZero() { taken, claimErr = RegistrableTaken(all, d, scope) }`. Admin có `scope` rỗng (`scopeOf` ở `internal/server/routes.go` dòng 333) nên không vào luật. Với `POST /sites`, admin thoát ngay ở đầu `siteCreateOwner` (`id.IsAdmin()`). Chưa chạy trong phiên viết; đọc từ mã.

6. Ai tạo trước giữ gốc: người thứ hai bị 404 cho `shop.X` nếu người thứ nhất đã tạo `www.X`. Người sau (hay chủ thật của `X`) nhờ admin tạo hộ bằng `--owner`. Đây là hệ quả có chủ ý, ghi ở mục 6.28 và mục 38 của `docs/thay-doi/M28.md`, cùng chính sách như HestiaCP. Cái giá: khách thật muốn hai tên con cùng một gốc phải nhờ admin.

7. `CreateJob` truyền `job.Target` vào cả hai truy vấn con `SELECT owner FROM sites WHERE domain = ?` và `SELECT owner FROM databases WHERE name = ?` cho mọi loại job. Job `user.suspend` có `Target = "khach1_p7y"`; DB rời `p7y` của `khach1` có tên đầy đủ `khach1_p7y`, nên truy vấn thứ hai trả `khach1`. `COALESCE` thứ tự: chủ đặt sẵn, site, DB rời, người bấm, `admin`. DB rời đứng trước người bấm nên thắng; bản cũ ghi `chủ ghi trong tpan.db: khach1` (log chưa vá dòng 265).

8. Danh sách cho phép đổi mặc định thành an toàn: loại job mới (hôm nay chưa có, mai có) không suy chủ từ tên cho tới khi có người kê nó vào. Danh sách cấm thì mỗi loại mới là lỗ tiềm ẩn. Ví dụ `php8.3-fpm`: `validate.ParseDomain` coi nó là domain hợp lệ; nếu cho tiền tố `fix.` tra bảng `sites`, khách tạo site tên `php8.3-fpm` sẽ thành chủ của job admin `fix.service.start` có target đó. Nên từng loại `fix.*` có target là domain mới được kê tên.

9. Chạy lệnh Python ở mục 2.2 trên máy ảo (root). Chưa chạy trong phiên viết. Job `user.suspend` phải có `owner` là `admin` (log bản vá dòng 264: `chủ ghi trong tpan.db: admin`), còn job tạo DB rời `khach1_p7z` của `khach1` có `owner` là `khach1` (log bản vá dòng 277: `ĐẠT: chủ của job tạo DB khach1_p7z là khach1`).

10. Bản cũ gọi tới chính địa chỉ nguồn, nên `selfConn` thấy "từ chính máy" và khoá theo UID của root (`uid:0`): mọi request chung một khoá, nên đa số nhận 429 từ hàng chờ hay 423 từ khoá, và chưa tới chỗ so mật khẩu (nhóm A: 3 câu 401, 2 câu 423, 55 câu 429, N = 60). Bản mới đổi: nguồn là `10.98.0.<n>` riêng từng request, đích luôn là `127.0.0.1`; và N tính theo thời gian một lần argon2 để vượt hẳn 2 chỗ nhân 3 giây. Khẳng định chứng minh: "hơn 5 câu 401" (53 ở log bản vá), vì một khoá nguồn chỉ có 5 lần sai.
