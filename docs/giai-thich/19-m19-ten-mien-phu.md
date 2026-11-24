# M19: tên miền phụ của site, chuyển hướng, chứng chỉ phủ đủ các tên

Đọc kèm: `00-go-can-ban.md` (interface, error, context), `05-m5-ssl-va-bao-mat.md` (SSL, certbot, thử thách ACME), `docs/thay-doi/M19-thiet-ke.md` (thiết kế đầy đủ; hai mục cuối "Soát thiết kế (lượt 1)" và "Trả lời soát lượt 1" đè lên phần thân khi khác nhau), `docs/bang-chung/M19-nghien-cuu/` (bản thử và số đo), `docs/bang-chung/M19-thu-1-nginx.log` (lần chạy thử e2e phần nginx).

Các từ dùng suốt file:

- **Tên chính**: tên trong `sites.domain`, ví dụ `example.com`. **Tên phụ** (alias): tên khác cùng trỏ về site đó, ví dụ `www.example.com`.
- **Kiểu của tên phụ**: `serve` (phục vụ cùng nội dung như tên chính) hoặc `redirect` (trả mã 301 để trình duyệt đi sang tên chính).
- **Tên miền gốc** (eTLD+1): phần "đăng ký được" của một tên. `www.example.com` và `shop.example.com` đều có tên miền gốc `example.com`. Mục 4.1 giải thích cách tính.
- **Pending**: dòng đã giữ chỗ trong DB nhưng job của nó chưa xong. **Active**: job đã xong, tên đang được dùng.

## 1. Mốc này làm được gì

- Một site có nhiều tên. Site mới tạo từ `example.com` tự có thêm `www.example.com` kiểu `redirect`. Bạn thêm, đổi kiểu, gỡ tên phụ bằng `tpan site alias add|set|rm|list`.
- Chuyển hướng theo đường dẫn: `tpan site redirect add example.com /old-page /new-page` làm `/old-page` trả 301 về `/new-page`. Tối đa 50 luật mỗi site, không có regex.
- Một tên chỉ thuộc một site trên cả máy, và một tên miền gốc chỉ thuộc một chủ (user thường). User B không gắn được `www.example.com` hay `shop.example.com` vào site của mình khi `example.com` đã thuộc user A.
- Chứng chỉ SSL của site phủ tên chính và mọi tên phụ đã trỏ DNS về máy (SAN, mục 4.5). Tên chưa trỏ DNS không làm hỏng chứng chỉ đang chạy.
- `tpan route explain <tên phụ>` lần theo đường đi của request mang tên đó; `tpan health` cảnh báo (không báo đỏ) khi tên phụ chưa trỏ DNS hay chứng chỉ lệch bộ tên.

### Nói trong một phút

Một site WordPress thật hầu như luôn có hai tên: `example.com` và `www.example.com`. Trước M19 TPAN chỉ biết một tên mỗi site, nên muốn thêm `www` là phải sửa vhost bằng tay, và TPAN coi sửa tay là "lệch".

Ba ý chính để nói:

1. **Tên là tài nguyên có chủ.** Hai chỗ trong DB giữ điều đó: khoá chính `name` (một tên một site) và hàm `site.RootForeign` (một tên miền gốc một chủ). Cả hai chạy trong một transaction, nên hai người bấm cùng lúc cũng chỉ một người thắng. Cùng hàm `RootForeign` là bản vá lỗ P4 của M28, không có luật thứ hai.
2. **Chuỗi người dùng gõ không bao giờ vào file cấu hình ở dạng thô.** Đường dẫn chuyển hướng chỉ được dùng các ký tự trong một danh sách cho phép (chữ, số, `. _ ~ -` và `/`), không regex. Vì vậy không có cách nào gõ ra dấu `;` hay `}` để chèn chỉ thị vào nginx hay Apache.
3. **Thêm tên không được làm hỏng cái đang chạy.** Tên chưa trỏ DNS vẫn thêm được nhưng bị loại khỏi đơn xin chứng chỉ; tên đang nằm trong chứng chỉ mà thử hỏng tạm thì giữ chứng chỉ cũ chứ không thu nhỏ nó. Site cũ nâng lên M19 không đổi một byte vhost (file vàng).

## 2. Chạy thử

### 2.1 Số liệu e2e

e2e bước 22 là `scripts/e2e-m19.sh` (`make e2e-m19`), chạy trên máy ảo `tpan-m19` ngày 25/11/2026, trên mã cuối đã gộp `dev` (gồm M20). Log: `docs/bang-chung/M19.log`. Dòng cuối:

```
==== Phần NGINX: 173 khẳng định đạt, 0 không đạt
==== Phần APACHE: 173 khẳng định đạt, 0 không đạt
==== Phần NÂNG-CẤP: 26 khẳng định đạt, 1 không đạt
==== Phần NÂNG-CẤP-V020: 29 khẳng định đạt, 0 không đạt
######## [2026-11-25 05:18:41] TỔNG KẾT: 401 khẳng định đạt, 1 khẳng định không đạt
```

Chỗ không đạt duy nhất là của script, không phải của M19: sau khi lên bản đã gộp M20, `tpan health` có thêm cảnh báo "Mọi admin đã bật đăng nhập hai bước" (phép kiểm của M20), mà khẳng định E60 coi mọi dòng mới là lỗi. Script sửa để bỏ đúng dòng đó; chạy lại riêng phần nâng cấp: `docs/bang-chung/M19-nang-cap.log`, 27 đạt, 0 không đạt.

Hai đường nâng cấp khác nhau có chủ ý. Phần `nang-cap` cài bản ngay trước M19 rồi lên: vhost phải giống từng byte. Phần `nang-cap-v020` cài gói phát hành v0.2.0 rồi lên: không so từng byte (template đã đổi từ v0.2.0 ở M14, M28), chỉ khẳng định không tên phụ nào tự sinh, site vẫn 200, health không có dòng mới về tên phụ.

Hồi quy: M16 120/0 và M14 270/0 trên mã cuối; M13 293/0, M5 256/0, M3 151/0 trên mã trước khi gộp M20 (`docs/bang-chung/M19-hoi-quy-*.log`).

Phần giao diện bấm thật bằng trình duyệt (`make e2e-m19-gd`: thêm tên phụ, lời từ chối của user B, tên chưa đạt, thêm luật): 94 khẳng định đạt, 0 không đạt, `docs/bang-chung/M19-giao-dien.log`; ảnh ở `docs/bang-chung/M19/`.

Trước đó có một lần chạy thử phần nginx khi phần chứng chỉ chưa nối (`docs/bang-chung/M19-thu-1-nginx.log`, 162 đạt, 12 không đạt). Mình giữ nó vì nó cho thấy hai điều: 11 chỗ đỏ đúng là phần SSL chưa làm (chứng chỉ chỉ có `example.com`), và 1 chỗ (E24) là kỳ vọng sai của script: ba lần "thắng" đều thêm cùng một tên vào cùng một site, kết quả một dòng. Bất biến thật là "mọi người thắng thuộc cùng một site", script đã sửa theo.

### 2.2 Output thật trích từ log thử

Các khối dưới đây trích từ lần chạy thử `docs/bang-chung/M19-thu-1-nginx.log` (máy ảo, stack nginx); lần chạy cuối `M19.log` cho cùng kết quả ở các khẳng định này.

Tạo site có sẵn tên phụ `www` (E01, E02, E03):

```
$ tpan site alias list example.com --json (thoát 0)
  example.com primary serve active dns=warn in_cert=False
  www.example.com alias redirect active dns=warn in_cert=False
GET http://www.example.com/a?b=1: HTTP 301, Location: http://example.com/a?b=1, X-Redirect-By: -
GET http://example.com/: HTTP 200, Location: -, X-Redirect-By: -
```

Đổi `www` sang `serve` (E04). Chú ý dòng cuối: tên đã được phục vụ nhưng WordPress vẫn đẩy `/` về tên trần, kèm header nói rõ ai đẩy (mục 4.2):

```
GET http://example.com/readme.html: HTTP 200, Location: -, X-Redirect-By: -
GET http://www.example.com/readme.html: HTTP 200, Location: -, X-Redirect-By: -
GET http://www.example.com/: HTTP 301, Location: http://example.com/, X-Redirect-By: WordPress
```

User B thử chiếm tên (E20, E21). User thường nhận một mã chung `name_unavailable` cho mọi kiểu trùng; admin cùng yêu cầu nhận mã chi tiết:

```
khach_b POST /sites/shop.khach-b.test/names: HTTP 409, ... lỗi: name_unavailable
=> ĐẠT: E20: thân không có tên site hay tên chủ của người khác
admin POST /sites/shop.khach-b.test/names?dry_run=1: HTTP 409, ... lỗi: name_taken
=> ĐẠT: E21: example.com. -> 409 hay 400 (mã domain_invalid)
=> ĐẠT: E21: EXAMPLE.COM -> 409 hay 400 (mã name_unavailable)
=> ĐẠT: E21: site_names của shop.khach-b.test không thêm dòng (1 dòng trước và sau)
```

Chuyển hướng theo đường dẫn (E30): `?x=1` bị bỏ, `/old-page/` và `/Old-Page` không bị luật bắt:

```
GET http://example.com/old-page?x=1: HTTP 301, Location: http://example.com/new-page
GET http://example.com/old-page/:    HTTP 404
GET http://example.com/Old-Page:     HTTP 404
```

Tên phụ chưa có DNS (E40, E42), trên site đã có SSL. Tên thêm được, chứng chỉ cũ giữ nguyên serial, cổng 443 của tên lạ không vào vhost của site, cổng 80 chuyển về tên chính:

```
nodns.example.invalid alias serve active dns=missing in_cert=False
GET https://nodns.example.invalid/readme.html: HTTP 000 (curl thoát 35)
GET http://nodns.example.invalid/: HTTP 301, Location: https://example.com/
```

`route explain` (E51, E52), cắt bớt:

```
$ tpan route explain www.example.com (thoát 0)
  Request tới www.example.com (nginx)
  Tên phụ (redirect) của site example.com
  [đạt] Vhost nhận tên miền: /etc/nginx/sites-enabled/tpan-example.com.conf:72
  [đạt] Web server chuyển tên này về tên chính (301): 301 http://example.com/
$ tpan route explain evil.example.org (thoát 1)
  Từ chối: không site nào có tên này; request với Host này rơi vào vhost mặc định và bị từ chối
```

Bằng chứng ngoài e2e: bản thử trên máy ảo ngày 25/11/2026 ở `docs/bang-chung/M19-nghien-cuu/` (nginx 1.24.0, Apache 2.4.58, certbot 2.9.0, pebble). Ví dụ `t3-pebble.out`: thêm một tên không phân giải được vào đơn thì certbot thoát mã 1 và chứng chỉ cũ giữ nguyên serial (ca C3); pebble không giữ trần số tên (ca C5, 101 tên vẫn qua), nên TPAN tự giữ trần 25.

### 2.3 Tự thử lại: sáu lệnh

Lệnh 1 và 2 chạy trên Mac, trong thư mục repo; mình đã gõ cả hai trong phiên này. Lệnh 3 đến 6 chạy trên máy ảo qua `scripts/vm.sh`, mình chưa gõ trong phiên này (nội dung tương ứng nằm ở log thử mục 2.2). Điều kiện cho 3 đến 6: máy ảo sạch có stack nginx (`scripts/vm.sh root tpan stack install --web nginx --yes`), và bản `tpan` của mốc này đã chép vào máy ảo (`make vm-deploy`). Máy ảo thử không có DNS thật, nên thêm vào `/etc/hosts` của nó dòng `127.0.0.1 example.com www.example.com` (script e2e làm việc này).

```bash
# 1. Mac. Chạy các test chính của mốc: luật sở hữu, tên miền gốc, đua giữ tên, file vàng
go test ./internal/site/ ./internal/validate/ ./internal/store/ ./internal/templates/ -run 'CheckClaims|RegisteredDomain|ClaimNamesRace|VhostZeroNamesGolden|NoHostVariables' -count=1

# 2. Mac. In bảng Public Suffix List cho nhiều tên, kể cả github.io
go run ./docs/bang-chung/M19-nghien-cuu/_t4psl

# 3. Máy ảo. Tạo site, xem tên phụ mặc định, rồi xem dòng server_name của vhost
scripts/vm.sh root tpan site add example.com --email admin@example.com --yes
scripts/vm.sh root tpan site alias list example.com

# 4. Máy ảo. Gọi vào tên phụ kiểu redirect
scripts/vm.sh exec curl -si -H 'Host: www.example.com' 'http://127.0.0.1/a?b=1'

# 5. Máy ảo. Thêm luật chuyển hướng rồi gọi thử, rồi gọi biến thể sai hoa thường
scripts/vm.sh root tpan site redirect add example.com /old-page /new-page --yes
scripts/vm.sh exec curl -si 'http://127.0.0.1/old-page?x=1' -H 'Host: example.com'

# 6. Máy ảo. Hỏi route explain cho tên phụ và xem khối vhost của nó
scripts/vm.sh root tpan route explain www.example.com
```

Điều bạn sẽ thấy:

- Lệnh 1: bốn dòng `ok  github.com/doxuta/tpan/internal/...` (site, validate, store, templates). Mình đã chạy, kết quả đúng như vậy ở commit hiện tại.
- Lệnh 2: dòng `alice.github.io` và `bob.github.io` có hai `eTLD+1` khác nhau (`alice.github.io`, `bob.github.io`); dòng `co.uk`, `com.vn`, `github.io` có `eTLD+1 LỖI: ... cannot derive eTLD+1`. Mình đã chạy.
- Lệnh 3: dòng `www.example.com alias redirect active` (theo log thử). `site alias list` có thể ghi `dns=warn` vì máy ảo thử không có IP công cộng; bình thường.
- Lệnh 4: `HTTP/1.1 301 Moved Permanently` và `Location: http://example.com/a?b=1` (theo log thử E02).
- Lệnh 5: job thành công; lần gọi thử trả `301` với `Location: http://example.com/new-page` và mất `?x=1` (theo log thử E30). Gọi `/old-page/` hay `/Old-Page` thì thấy 404 của WordPress, không bị luật bắt.
- Lệnh 6: các chặng DNS, vhost (có số dòng trong file `tpan-example.com.conf`), rồi dòng "Web server chuyển tên này về tên chính (301)" (theo log thử E51).

## 3. Đi qua mã theo thứ tự chạy

Đường đi của một lệnh điển hình: `tpan site alias add example.com www.example.com`. Phần luật chuyển hướng và chứng chỉ rẽ ra từ đường này.

### 3.1 Lệnh gõ vào: `cmd/tpan/site_m19.go`

`siteAliasAddCmd` định nghĩa lệnh, cờ `--mode` (mặc định `redirect`: tên phụ thêm vào là cách an toàn nhất, chỉ chuyển hướng). Nó không tự làm gì; nó gọi `core.Sites.PrepareAliasAdd` rồi `runAliasJob` chạy kế hoạch.

```go
RunE: func(cmd *cobra.Command, args []string) error {
	m, err := parseAliasModeArg(mode, site.ModeRedirect)
	if err != nil {
		return fail(e, f.asJSON, err)
	}
	return runAliasJob(cmd, e, &f, args, "site.alias.add",
		func(ctx context.Context, s *core.Sites, d, n validate.Domain) (*core.NamesOp, error) {
			return s.PrepareAliasAdd(ctx, d, n, m)
		})
},
```

Mẫu này giống mọi lệnh có Plan: Prepare (chỉ đọc, từ chối sớm), Preview (cho `--dry-run`), Execute (chạy thật). Bạn đã gặp ở các mốc trước.

### 3.2 Prepare: từ chối sớm, `core.PrepareAliasAdd` (`internal/core/names_m19.go`)

Hàm này kiểm theo thứ tự: site có và đang `active`, không tạm khoá; tên khác tên chính; số tên dưới 25; rồi tới quyền sở hữu:

```go
claim := site.Claim{Name: name, Site: siteD, Owner: ownerOf(st), Kind: site.KindAlias, Mode: m, ByAdmin: !s.NotAdmin}
if err := ns.CheckNames(ctx, []site.Claim{claim}); err != nil {
	return nil, s.aliasRefusal(err)
}
```

`Owner` là chủ của site, không phải người đang bấm. Vì sao: admin bấm hộ user B cũng không được gắn tên thuộc gốc của A vào site của B. `ByAdmin` chỉ dùng để miễn một nhóm luật (mục 3.3).

Rồi hàm này đọc `nginx -T` hoặc `apache2ctl -S` qua `declaredElsewhere` để từ chối tên mà một vhost không do TPAN quản lý đã khai. Không có bước này, user B thêm `webmail.company.example` (tên của vhost admin dựng tay) là cướp được lưu lượng của tên đó, vì nginx chỉ cảnh báo khi `server_name` trùng và khối nạp trước thắng (soát thiết kế S13). `declaredGuard` là bước đầu của Plan, nên lúc Execute nó kiểm lại một lần nữa dưới khoá của engine.

Lỗi của `CheckNames` đi qua `aliasRefusal` (cùng file): user thường nhận một mã `name_unavailable` và một câu chung, admin nhận mã chi tiết. Lý do: nếu lời từ chối nói "tên này thuộc site X của người Y" thì B dò được ai đang giữ tên nào trên máy.

### 3.3 Luật sở hữu: `site.CheckClaims` (`internal/site/names.go`)

Gói `site` thuần, không đụng DB, nên test được hết bằng bảng. Với mỗi claim, `CheckClaims` đi theo thứ tự: tên dành cho phép thử bị từ chối; rồi xét "đã có dòng mang tên này chưa":

```go
n := rows[i]
mine := n.Site == c.Site && n.Owner == c.Owner
switch {
case mine && c.Kind == KindPrimary && n.Kind == KindPrimary:
	continue
case mine && c.Kind == KindAlias && n.Kind == KindAlias && n.State == StatePending:
	reclaim = true
case n.Owner != c.Owner:
	return &NameError{Code: CodeNameTaken, Name: c.Name}
case n.Site == c.Site:
	return &NameError{Code: CodeAliasExists, Name: c.Name}
default:
	return &NameError{Code: CodeNameTakenSelf, Name: c.Name, Other: n.Site}
}
```

Đọc từ trên xuống:

- Tên chính của chính site, cùng chủ: bỏ qua (`continue`). Đây là trường hợp chạy lại `tpan site add` hay `tpan apply` hai lần. Điều kiện `n.Owner == c.Owner` (thêm sau soát S4) cần thiết: không có nó thì dòng pending mồ côi của X bị Y dùng lại, và DB nói X giữ tên miền gốc của site Y.
- Tên phụ pending của chính site, cùng chủ, cùng kiểu: nhận lại (`reclaim`). Job trước chết giữa chừng thì chạy lại đúng lệnh là xong.
- Còn lại là từ chối, theo ba mã. Không ai được miễn, kể cả admin: hai site không bao giờ cùng một tên (B2).

Sau đó, nếu không phải admin (`!c.ByAdmin`), gọi `RootForeign` (mục 3.4). Cuối cùng đếm số tên của site, từ `MaxNames` (25) trở lên thì `alias_limit`. Mỗi claim đã nhận được thêm vào `rows` như một dòng pending trước khi xét claim sau (S10), nên hai claim trong cùng một lô (ví dụ file `tpan apply`) cũng không lấn gốc của nhau.

### 3.4 Một chỗ duy nhất cho luật "tên miền gốc": `site.RootForeign`

```go
func RootForeign(existing []Name, d validate.Domain, owner string) (bool, error) {
	want, err := validate.RegisteredDomain(d)
	if err != nil {
		return false, &NameError{Code: CodeNamePublicSuffix, Name: d}
	}
	for _, n := range existing {
		if n.Owner == owner {
			continue
		}
		if r, err := validate.RegisteredDomain(n.Name); err == nil && r == want {
			return true, nil
		}
	}
	return false, nil
}
```

Nó nói: có dòng nào, của chủ khác, cùng tên miền gốc với `d` không? Duyệt cả danh sách và tính lại tên miền gốc mỗi dòng. Cố ý đơn giản (có comment `ponytail:` ngay trên hàm): một máy chỉ có vài trăm tên; tính lại mỗi lần còn có lợi là nâng `golang.org/x/net` thì không bao giờ có cột tên miền gốc cũ sai trong DB.

Hàm này là chỗ chung với M28. Khi soát thiết kế, người soát phát hiện M28 đã vá P4 ở nhánh `dev` bằng `core.RegistrableTaken`, còn thiết kế M19 viết một luật thứ hai khác hẳn (admin có bị chặn hay không, mã lỗi, chỗ kiểm). Quyết định: giữ một luật. `core.RegistrableTaken` giữ chữ ký và thân giờ dựng một danh sách rồi gọi `site.RootForeign`; test M28 không đổi. Admin được miễn luật này giống M28: admin là người vận hành máy, có thể cố ý gắn tên cho khách (log E22 ghi nhận điều đó). Luật "một tên một site" thì admin không được miễn.

### 3.5 Public Suffix List: `validate.RegisteredDomain` (`internal/validate/psl.go`)

```go
r, err := publicsuffix.EffectiveTLDPlusOne(strings.ToLower(d.s))
if err != nil {
	return Domain{}, &Error{Code: CodeDomainPublicSuffix, Message: i18n.T(...)}
}
return Domain{s: r}, nil
```

Đây là chỗ duy nhất trong cả repo gọi gói `golang.org/x/net/publicsuffix`. Bảng PSL nằm sẵn trong gói đó (nhúng vào file chạy, không tải từ mạng), nên không thêm dependency và không cần mạng lúc chạy. Hậu tố công cộng (`co.uk`, `com.vn`, `github.io`) trả lỗi, nên không ai đặt được chúng làm tên chính hay tên phụ.

### 3.6 Giữ chỗ trong một transaction: `Store.ClaimNames` (`internal/store/names.go`)

Prepare chỉ đọc. Khi `Execute` chạy, nó gọi `ClaimNames` trước khi chạy job:

```go
if _, err := tx.ExecContext(ctx, "DELETE FROM site_names WHERE state = 'pending' AND created_at < ?",
	fmtTime(now.Add(-pendingTTL))); err != nil {
	return fmt.Errorf("bỏ tên giữ chỗ quá hạn: %w", err)
}
existing, err := queryNames(ctx, tx, "")
if err != nil {
	return err
}
if err := site.CheckClaims(cs, existing); err != nil {
	return err
}
```

Thứ tự: bỏ pending quá hạn, đọc ảnh chụp mọi tên, chạy `CheckClaims`, rồi `INSERT` dòng `pending`. Tất cả trong một transaction. DB mở với `_txlock=immediate` (`store.go`), nghĩa là `BEGIN` lấy quyền ghi ngay, nên hai transaction ghi không chạy chen nhau, kể cả CLI (root) và `tpan serve`. Test `TestClaimNamesRace` (20 goroutine, hai chủ, cùng tên và cùng tên miền gốc) khẳng định đúng một chủ thắng; mình đã chạy test đó trong phiên này, kết quả `ok`.

"Ảnh chụp" (`nameSnapshot`) là bảng `site_names` cộng một dòng tên chính suy ra từ mỗi dòng `sites` chưa có dòng của mình. Vì vậy một đường tạo site mà quên ghi `site_names` (mã cũ, khôi phục) vẫn không để tên đó bị người khác giữ.

Bỏ pending quá 1 giờ (`pendingTTL`): nếu tiến trình chết giữa job, dòng pending của tên chính không có thao tác nào gỡ được (`alias rm` cần site có thật, mà site chưa được tạo), và nó khóa cả tên miền gốc với mọi chủ khác mãi mãi. Con số 1 giờ là lựa chọn của thiết kế, không phải số đo: trong log thử, E01 (tạo site) mất khoảng 16 giây và một lần thêm tên phụ vài giây, nên 1 giờ là biên rất rộng.

### 3.7 Ngoài DB: khoá chính `name`

`internal/store/migrations/1900_site_names.sql` tạo `site_names` với `name TEXT PRIMARY KEY`. Ràng buộc này là lớp chặn cuối cho "một tên một site": dù mã Go có sót, DB từ chối hai dòng cùng tên. Có hai chỗ cố ý không làm ràng buộc DB:

- Không có cột hay `UNIQUE` cho tên miền gốc: dữ liệu trước M19 (và trước vá P4) có thể đã có hai chủ chung một gốc, migration không được hỏng vì chuyện đó. Luật áp cho mọi lần giữ chỗ mới; `names.root.shared` trong health cảnh báo chỗ cũ.
- `site` không là khoá ngoại: tên chính của site đang được tạo được giữ chỗ (pending) trước khi có dòng trong `sites`.

Migration cũng chép tên chính của mọi site đang có (kể cả trong thùng rác) thành dòng `primary`, và không sinh tên phụ nào (`TestMigration1900OnlyPrimaries`).

### 3.8 Vẽ vhost: `splitNames` và các template (`internal/templates/m19.go`)

Plan ghi lại vhost của site, với danh sách tên lấy từ DB (`st.Names`, chỉ các dòng `active` cộng đúng tên của job đang chạy). `splitNames` quyết định tên nào nằm ở khối nào; template chỉ in danh sách đã chia. Với site có SSL, đoạn quyết định là:

```go
case serve && covered[s]:
	sp.Content = append(sp.Content, s)
	sp.Own80 = append(sp.Own80, s)
case serve:
	// Chưa có trong chứng chỉ: không lên 443 (trình duyệt báo lỗi
	// chứng chỉ), cổng 80 chuyển về tên chính.
	sp.Group80 = append(sp.Group80, s)
default:
	sp.Group80 = append(sp.Group80, s)
	if covered[s] {
		sp.Group443 = append(sp.Group443, s)
	}
}
```

Đọc thành lời: tên `serve` đã có trong chứng chỉ thì nằm cùng khối nội dung với tên chính (nên `server_name example.com www.example.com`), và cổng 80 của nó chuyển sang `https://` của chính nó. Tên `serve` chưa có trong chứng chỉ thì không bao giờ lên cổng 443 (người dùng sẽ thấy lỗi chứng chỉ), cổng 80 chuyển về tên chính. Tên `redirect` luôn là một khối `server` riêng chỉ có `return 301`.

Hai chi tiết quan trọng:

- Đích luôn là tên cố định đã kiểm, chuỗi `https://example.com$request_uri`; tuyệt đối không dùng `$host` (nginx) hay `%{HTTP_HOST}` (Apache). Nếu dùng, kẻ gửi request đặt `Host: evil.example.org` là làm trang chuyển khách sang tên tuỳ ý (open redirect). Test `TestNoHostVariables` quét output mọi biến thể để chắc điều này.
- Mọi khối chuyển tên phụ vẫn phục vụ thử thách của Let's Encrypt (`location ^~ /.well-known/acme-challenge/` ở nginx, `Alias` ở Apache). Không thì tên phụ `redirect` không xin được chứng chỉ, vì Let's Encrypt gọi tới đường dẫn đó qua đúng tên đang xin.

Template `site-nginx-name-redirect.tmpl` sinh khối sau cho `www.example.com`:

```
server {
    listen 80;
    server_name www.example.com;
    location ^~ /.well-known/acme-challenge/ { root /var/lib/tpan/acme; }
    location / { return 301 http://example.com$request_uri; }
}
```

(Mình rút gọn bớt dòng log và dòng `listen [::]:80`; nguyên văn ở `internal/templates/files/site-nginx-name-redirect.tmpl`.)

Site không có tên phụ và luật thì mọi danh sách rỗng, template không in thêm byte nào. Test `TestVhostZeroNamesGolden` so output với file vàng `internal/templates/testdata/m19-golden-*.golden` (sinh từ mã trước khi sửa template) cho các biến thể thường, SSL, lồng, tạm khoá, trên nginx và Apache. Đây là cách chứng minh "nâng từ bản cũ không đổi hành vi" (mục 4.8). Mình đã chạy test đó: `ok`.

### 3.9 Kiểm chứng sau khi ghi: `steps.NameProbe` (`internal/steps/m19.go`)

Sau khi ghi vhost và `nginx -t`, bước `NameProbe` gọi thật `curl --resolve <tên>:<cổng>:127.0.0.1` rồi so câu trả lời với kiểu của tên. Kiểu `redirect` phải là 301, `Location` đúng bằng `<scheme>://<tên chính>/`, và không có `X-Redirect-By`. Kiểu `serve` phải là 200 với dấu hiệu WordPress, hoặc 301/302 có `X-Redirect-By: WordPress`:

```go
case "301", "302":
	if strings.EqualFold(a.redirectBy, nameProbeWPMaker) {
		return nil
	}
	return fmt.Errorf("trả %s về %q mà không có X-Redirect-By: WordPress", a.code, a.location)
```

Vì sao có nhánh 301 cho kiểu `serve`: mục 4.2. Hỏng thì engine hoàn tác, vhost về như cũ, và `Execute` nhả dòng pending (`ReleaseNames`, chỉ xoá pending, không bao giờ xoá dòng active).

### 3.10 Kết thúc job: activate, rồi chạy tiếp SSL

`NamesOp.Execute` (`internal/core/names_m19.go`): `ClaimNames`, chạy engine, rồi tuỳ thao tác `ActivateNames` / `SetAliasMode` / `DeleteAlias`. Nếu ghi DB hỏng sau khi vhost đã đổi, nó không im lặng: lỗi nói rõ "job ... đã đổi vhost nhưng không ghi được tên vào DB" kèm cách sửa, vì vhost và DB lệch nhau thì lần vẽ lại sau sẽ âm thầm gỡ tên vừa thêm.

Site có SSL thì job còn chạy tiếp `followUpSSL`. Nó đếm job `site.ssl` của chủ site trong 24 giờ; từ 5 trở lên thì không chạy và ghi ghi chú `ssl_rate_limited`:

```go
if jc, ok := s.Store.(jobCounter); ok {
	n, err := jc.CountJobs(ctx, "site.ssl", owner, time.Now().Add(-sslAutoWindow))
	if err != nil {
		return err
	}
	if n >= sslAutoLimit {
		note(fmt.Sprintf("ssl_rate_limited: chủ site đã có %d job site.ssl ...", n, ...), ...) // rút gọn
		return nil
	}
}
```

Lý do: TPAN dùng một tài khoản Let's Encrypt cho mọi site. Giới hạn của Let's Encrypt (300 đơn mỗi 3 giờ mỗi tài khoản, theo bản tra ngày 25/11/2026 ở thiết kế mục 2.4) tính trên tài khoản, nên nếu một user bật tắt tên phụ liên tục thì tốn hạn mức của mọi người. Tên phụ vẫn thêm được; chỉ phần tự xin chứng chỉ bị chặn, và health `ssl.names` cảnh báo. Đếm theo chủ site (không theo site) vì M28 P7 đổi cách suy chủ của job.

### 3.11 Chứng chỉ SAN: `ACMEProbeNames` rồi `CertbotCert` (`internal/steps/ssl.go`)

Job `site.ssl` dựng bởi `sslPlan` (`internal/core/ssl.go`). Khi site có tên phụ, bước thử là `ACMEProbeNames` thay `ACMEProbe`:

```go
perr := sslProbeOne(ctx, sys, dir, n)
if perr == nil {
	r.Passed = append(r.Passed, n)
	continue
}
if i == 0 {
	return perr
}
covered := slices.Contains(s.Covered, n)
r.Failed = append(r.Failed, ProbeFail{Name: n, Covered: covered, Reason: ...})
r.Blocked = r.Blocked || covered
```

`sslProbeOne` ghi một file ngẫu nhiên vào webroot ACME rồi `curl` tới `http://<tên>/.well-known/acme-challenge/<file>`, như Let's Encrypt sẽ làm. Với từng tên:

- Qua: vào `Passed`.
- Là tên chính (`i == 0`) mà hỏng: cả bước hỏng, như trước M19.
- Là tên phụ hỏng: ghi vào `Failed` kèm lý do, bước vẫn đạt, tên bị bỏ khỏi đơn.
- Tên phụ hỏng mà đang nằm trong chứng chỉ (`Covered`): đặt `Blocked` (S3). `CertbotCert` thấy `Blocked` thì không chạy certbot, giữ chứng chỉ cũ, job kết thúc kèm ghi chú.

Vì sao `Blocked`: nếu chỉ "bỏ tên hỏng khỏi đơn", một lần resolver chập chờn làm tên đang chạy HTTPS rớt khỏi chứng chỉ mới, và khách của tên đó thấy lỗi chứng chỉ. Chỉ thu nhỏ chứng chỉ khi tên bị gỡ chính thức (`alias rm`, tên không còn trong bộ muốn có nên không đem thử). Test `TestCertbotKeepsCoveredNameOnProbeFail` cho ca này; mình thấy nó có trong repo, chưa chạy riêng nó.

Lệnh certbot do `CertbotCert.cmd` dựng, tên chính là `-d` đầu tiên:

```go
args := []string{"certonly", "--webroot", "-w", s.Webroot}
for _, n := range s.names() {
	args = append(args, "-d", n.String())
}
args = append(args, "--cert-name", d,
	"--non-interactive", "--agree-tos", "-m", s.Email.String(), "--server", s.Server)
```

`--cert-name` luôn là tên chính, nên đường dẫn `live/<tên chính>/` và hook gia hạn không đổi. Không cần `--expand`: ca C2 của bản thử cho thấy certbot tự xin lại khi bộ tên đổi nếu cùng `--cert-name`. Trần `MaxNames` (25) được kiểm trong `validate()` của cả hai bước.

Sau job: `afterCert` đọc SAN của `cert.pem` mới bằng `crypto/x509` rồi ghi cột `in_cert` qua `SetInCert`. Nếu bộ tên được phủ đổi thì chạy tiếp job `site.vhost` vẽ lại cổng 443. Vì vậy vhost vẫn là hàm thuần của DB, không cần đọc file chứng chỉ lúc dựng Plan.

### 3.12 Chuyển hướng theo đường dẫn

`tpan site redirect add example.com /old-page /new-page` đi một đường tương tự (`redirectSite`, `PrepareRedirectAdd` trong `internal/core/redirects_m19.go`) và có thêm lớp kiểm đầu vào, ở `internal/validate/redirect.go`:

```go
func isRedirectSegment(seg string) bool {
	if seg == "" || seg[0] == '.' {
		return false
	}
	for i := 0; i < len(seg); i++ {
		if !isRedirectChar(seg[i]) {
			return false
		}
	}
	return true
}
```

`isRedirectChar` chỉ nhận chữ, số, `.`, `_`, `~`, `-`. Đường dẫn là `/` cộng các đoạn như vậy nối bằng `/`; không `//`, không query ở nguồn, tối đa 255 byte. Đoạn không bắt đầu bằng `.` để luật người dùng không che được `/.well-known/` (thử thách ACME) hay `/.env`. `ParseRedirectSource` thêm: nguồn `/` và `/index.php` bị từ chối với mã `redirect_reserved`, vì mọi trang của WordPress đi qua hai đường dẫn đó (nginx `try_files` đổi permalink về `/index.php`), một luật trên đó chuyển hướng cả site.

Sau khi qua kiểm đầu vào, `checkRedirect` (core) kiểm các luật cần biết site: đích không được là chính nguồn (so phần đường dẫn của đích, bỏ query và bỏ `<scheme>://<tên của site>`), không hai luật chuyển qua lại, `--keep-query` không dùng với đích đã có query, nguồn chưa có luật, tối đa 50.

Template in mỗi luật thành một khối. nginx (`site-nginx-redirects.tmpl`): `location = /old-page { return 301 http://example.com/new-page; }`, giữ query thì thêm `$is_args$args`. Apache (`site-apache-redirects.tmpl`):

```
RewriteCond %{REQUEST_URI} =/old-page
RewriteRule ^ http://example.com/new-page [R=301,L,QSD]
```

Đích dạng đường dẫn được đổi thành URL tuyệt đối theo tên chính (`redirectLines`), để Location không phụ thuộc Host của request.

### 3.13 Route explain, health, nâng cấp, ngắn

- `core.routeExplainHost` (`internal/core/route_m19.go`) tra tên về dòng `site_names`; không có thì từ chối `name_unknown`. Tên chính đi như cũ. Tên phụ `redirect` có thêm một chặng gọi thật, thấy 301 và `Location`, rồi dừng kèm câu "đi tiếp: tpan route explain <tên chính>". API `GET /sites/{domain}/route?host=` trả `name_unknown` cho tên của site khác y như tên không có, để không lộ.
- `internal/checks/m19.go`: `AliasDNS` và `AliasHTTP` bọc `Soft` nên không bao giờ đỏ (tên phụ chưa trỏ DNS là chuyện bình thường khi bạn đang chuẩn bị chuyển DNS); `SSLNames` cảnh báo khi bộ tên trong chứng chỉ lệch bộ tên của site (đặc biệt khi tên trong chứng chỉ không còn trỏ về máy, vì lần gia hạn tự động sẽ xin lại đúng bộ đó và hỏng cả site); `NamesRootShared` cảnh báo khi dữ liệu cũ có hai chủ chung một tên miền gốc.
- Nâng từ bản cũ: migration 1900 chỉ thêm hai bảng và dòng `primary`; không ghi lại vhost nào. Vhost chỉ được ghi lại khi có thao tác khác, và khi đó (mục 3.8) output giống từng byte.

## 4. Kiến thức nền

### 4.1 Tên miền gốc (eTLD+1) và Public Suffix List

Một tên miền có phần "do nhà đăng ký bán" ở cuối: `.com`, `.co.uk`, `.com.vn`. Phần đó gọi là hậu tố công cộng. Một cái tên cộng thêm đúng một nhãn trước hậu tố là tên miền gốc, tức phần bạn thật sự mua: `example.com`, `example.co.uk`. Danh sách các hậu tố tên là Public Suffix List (PSL); Go có sẵn trong `golang.org/x/net/publicsuffix`.

PSL có hai phần: ICANN (`.com`, `.co.uk`) và private (`github.io`, nơi một nhà cung cấp cho nhiều người thuê mỗi người một tên con). Mốc này dùng cả hai: `alice.github.io` và `bob.github.io` là hai tên miền gốc khác nhau, vì hai người thuê hai tên dưới một hậu tố private là hai chủ khác nhau. Chỉ dùng phần ICANN thì gộp họ vào `github.io` và chặn nhầm người thứ hai.

Trong mã: `validate.RegisteredDomain`. Tự nhìn: lệnh 2 ở mục 2.3 (`go run ./docs/bang-chung/M19-nghien-cuu/_t4psl`).

Bảng PSL chỉ đổi khi nâng `golang.org/x/net` trong `go.mod` (bản đang dùng v0.63.0, PSL revision `d6c92f1bbb74`, ngày 2026-02-06, theo `t4-psl.out`). Hậu tố mới chưa có trong bảng chỉ khiến vài tên bị coi là "cùng gốc", tức chặt hơn chứ không lỏng hơn.

### 4.2 `www.` và tên trần, và vì sao WordPress tự chuyển

Mặc định `example.com` tự có `www.example.com` kiểu `redirect`; và ngược lại, site tạo từ `www.example.com` tự có `example.com`. Tên con sâu hơn (`blog.example.com`) không có tên phụ nào (`defaultWWW`, `internal/core/names_m19.go`). Mặc định là `redirect` để một nội dung chỉ có một địa chỉ chính: công cụ tìm kiếm không thấy hai bản trùng, cookie đăng nhập không bị tách thành hai.

WordPress tự đưa `www.` về tên trong cài đặt "Site Address" (`home`) và ngược lại: hàm `redirect_canonical` của nó chỉ chuyển khi hai host khác nhau đúng ở tiền tố `www.`. (Thông tin này do bước soát thiết kế đọc `wp-includes/canonical.php`; mình chưa mở mã WordPress.) Nó đánh dấu việc đó bằng header `X-Redirect-By: WordPress`; log E04 ở mục 2.2 cho thấy đúng như vậy. Hệ quả: đặt `www.example.com` kiểu `serve` thì `/readme.html` (file tĩnh) trả 200 ở cả hai tên, nhưng `/` qua `www` vẫn 301 về tên trần do chính WordPress. Đó là hành vi của WordPress, không phải lỗi của vhost. Muốn thật sự phục vụ hai tên khác nhau, phải đổi "Site Address" trong WordPress. Đó là lý do `NameProbe` kiểu `serve` chấp nhận 301 có `X-Redirect-By: WordPress` (mục 3.9) và kiểu `redirect` đòi không có header đó: header phân biệt "vhost của TPAN trả" với "trang của site trả".

Tự nhìn trên máy ảo: lệnh 4 và lệnh `scripts/vm.sh exec curl -si -H 'Host: www.example.com' http://127.0.0.1/` sau khi `tpan site alias set example.com www.example.com --mode serve --yes` (chưa chạy trong phiên này; log thử E04 đã có).

### 4.3 Giữ chỗ và transaction

Giữ chỗ (`pending`) trước rồi mới chạy job, kích hoạt (`active`) khi job xong, nhả khi job hỏng. Lý do: nếu chỉ kiểm lúc Prepare thì có khoảng hở giữa "kiểm" và "ghi" cho người thứ hai chen vào. Giữ chỗ trong transaction của SQLite mở với `_txlock=immediate` là tuần tự toàn máy. Vhost chỉ vẽ tên `active`, cộng đúng tên của job đang chạy, nên job khác chạy chen không vẽ tên đang pending (`TestVhostSkipsPendingNames`).

Tự nhìn: `scripts/vm.sh root sqlite3 /var/lib/tpan/tpan.db 'select name,site,owner,kind,mode,state,in_cert from site_names'` (đường dẫn mặc định của cờ `--db`, `cmd/tpan/root.go`; chưa chạy, và máy ảo cần có `sqlite3`).

### 4.4 `location =` của nginx và `RewriteCond =` của Apache

nginx chọn `location` theo thứ tự ưu tiên: khớp đúng (`=`) thắng mọi `location` khác kể cả regex. Apache dùng `RewriteCond %{REQUEST_URI} =/old-page`: toán tử `=` là so chuỗi đúng, không phải regex. Vì sao không dùng `Redirect` của Apache: bản thử t2 cho thấy `Redirect` khớp theo tiền tố (`/old-page/` cũng bị bắt, nên trộn lẫn với `.htaccess` của site).

Hệ quả cho người dùng (có ở bản thử P03 P04): khớp đúng đường dẫn, phân biệt hoa thường và dấu `/` cuối; giao diện có dòng nhắc. Luật ở vhost chạy trước `.htaccess` của site trên Apache (bản thử S06).

Không có regex: regex do người dùng viết dễ gây ReDoS (một mẫu làm máy chủ đứng CPU vì so khớp quá chậm) và dễ chèn cấu hình. `=` loại cả hai.

Tự nhìn: sau lệnh 5, `scripts/vm.sh root grep -n -B1 -A3 'location = ' /etc/nginx/sites-available/tpan-example.com.conf`.

### 4.5 Chứng chỉ SAN và xác minh HTTP-01

Một chứng chỉ có thể liệt kê nhiều tên trong trường Subject Alternative Name (SAN). Let's Encrypt xác minh từng tên bằng cách gọi `http://<tên>/.well-known/acme-challenge/<file>` về máy bạn; một tên không gọi được là cả đơn hỏng (ca C3 của bản thử: certbot thoát 1, chứng chỉ cũ không đổi). Vì vậy TPAN thử từng tên trước và chỉ đưa tên đạt vào lệnh. Trần: Let's Encrypt cho tối đa 100 tên trong profile mặc định và 25 trong các profile nhỏ (theo bản tra ngày 25/11/2026 ở thiết kế mục 2.4); TPAN đặt 25 (`site.MaxNames`) để vừa mọi profile, và pebble không giữ trần nên TPAN tự giữ.

Một điều dễ quên: `certbot renew` xin lại đúng bộ tên đang có trong chứng chỉ. Một tên đã đi mà vẫn nằm trong chứng chỉ làm lần gia hạn của cả site hỏng. Vì thế gỡ tên phụ đang trong chứng chỉ là xin lại chứng chỉ không có nó (`TestAliasRmNeedsCert`).

Tự nhìn trên máy ảo có SSL: `scripts/vm.sh root openssl x509 -in /etc/letsencrypt/live/example.com/cert.pem -noout -ext subjectAltName`.

### 4.6 Danh sách cho phép thay vì danh sách cấm

Chặn "những ký tự nguy hiểm" luôn sót một ký tự. Cho phép "chỉ những ký tự an toàn" thì thứ gì chưa nghĩ tới tự động bị từ chối. Trong `validate/redirect.go`, mọi ký tự có nghĩa với nginx hay Apache (`; { } $ # " ' \ %`, khoảng trắng, xuống dòng) nằm ngoài danh sách, nên giá trị đã qua kiểm chèn thẳng vào template mà không cần trích dẫn. Trong Go, `RedirectSource` có trường ẩn (`struct{ s string }`) nên chỉ `ParseRedirectSource` tạo được giá trị khác rỗng: nơi khác trong mã không thể dựng một nguồn chưa kiểm. Template còn một lớp chặn cuối (`m19Name`, `m19Rule`) phòng mã nội bộ dựng sai, và có fuzz test `FuzzParseRedirect`, `FuzzVhostRedirects`.

### 4.7 File vàng (golden file)

Một test so output của hàm với một file đã lưu sẵn, từng byte. File vàng của M19 sinh từ mã trước khi sửa template; test sau đó bảo đảm site không có tên phụ ra đúng file cũ. Muốn đổi output cố ý thì phải sửa file vàng, và người review thấy diff đó. Xem: `ls internal/templates/testdata | grep m19-golden`.

### 4.8 Interface nhỏ để test: `NameStore`

Core cần giữ tên nhưng không nên phụ thuộc cứng vào `*store.Store`. `core.NameStore` (`internal/core/types_m19.go`) là một interface chỉ có các hàm tên và luật; `*store.Store` thoả nó, và core lấy nó bằng `s.Store.(NameStore)`. Store giả của các test cũ không cài nó; khi đó `site add` chỉ kiểm tên chính trên bảng `sites`. Bản chạy thật luôn có `*store.Store`, và một dòng kiểm lúc biên dịch trong `cmd/tpan/main.go` giữ điều đó (Go kiểm interface lúc biên dịch: xem `00-go-can-ban.md` mục 4).

## 5. Cách khác đã cân nhắc

### 5.1 Cái đã cân nhắc và không chọn

- **Cho người dùng viết regex chuyển hướng.** Mạnh hơn nhưng mở đường ReDoS và chèn cấu hình. Chọn khớp đúng một đường dẫn; ai cần regex thì chưa có trong mốc này.
- **Ràng buộc DB "một tên miền gốc một chủ".** Gọn hơn một hàm Go, nhưng dữ liệu trước M19 có thể đã vi phạm (lỗ P4 chưa vá ở v0.2.0), nên migration sẽ hỏng trên máy thật. Chọn luật ở Go (`RootForeign`) áp cho mọi lần giữ chỗ mới, cộng cảnh báo `names.root.shared` cho dữ liệu cũ.
- **Đích chuyển hướng bằng `$host` / `%{HTTP_HOST}`.** Một khối vhost dùng chung cho mọi tên cho ngắn. Bản thử H01 đến H05 cho thấy `$host` an toàn trong nginx, nhưng thiết kế vẫn không dùng: mỗi tên một đích cố định, khỏi phải tin chứng minh đó về sau, và Apache dựng `Location` theo Host của request nếu đích là đường dẫn tương đối (S16), nên đích luôn là URL tuyệt đối theo tên chính.
- **Xin chứng chỉ "phần nào đạt thì lấy phần đó".** certbot không có chế độ đó (ca C3: một tên hỏng là cả đơn hỏng). Chọn thử từng tên trước.
- **Tên đại diện `*.example.com`.** Cần xác minh DNS-01 mà TPAN không quản DNS. Không làm.
- **Hạn mức gọi `ssl issue` tay.** Không thêm, để giữ hành vi M5; xem mục 7.

### 5.2 Chỗ đã làm sai trong mốc này, và phát hiện bằng cách nào

Bản thiết kế đợt 0 qua một lượt soát bằng thợ `opus` effort max đọc mã ở HEAD (mục "Soát thiết kế (lượt 1)" trong `M19-thiet-ke.md`), chưa có dòng mã M19 nào. Những chỗ sai đáng nhớ nhất, vì người review hay hỏi đúng chúng:

- **Viết luật sở hữu thứ hai (S1).** Thiết kế viết luật riêng, ngược với `core.RegistrableTaken` mà M28 đã có trên nhánh `dev`: khác ở chỗ admin có bị chặn không, đuôi công cộng, lời từ chối (404 hay 409), chỗ kiểm (ngoài hay trong transaction). Phát hiện bằng cách đọc `origin/dev`. Sửa: một luật `site.RootForeign`, M28 gọi lại.
- **Đoán sai hành vi WordPress (S2).** Thiết kế mong tên `www` kiểu `serve` trả 200 ở `/`. WordPress trả 301. Phát hiện khi đối chiếu mã `canonical.php`, rồi log e2e E04 xác nhận bằng header `X-Redirect-By`. Sửa: `NameProbe` nhận 301 có header đó.
- **Job SSL tự thu nhỏ chứng chỉ (S3).** Một lần resolver chập chờn có thể làm tên đang chạy HTTPS rơi khỏi chứng chỉ. Sửa: `Blocked`, giữ chứng chỉ cũ.
- **"Chạy lại tên chính là no-op" không so chủ (S4).** Dòng pending của X bị Y dùng lại. Sửa: no-op chỉ khi cùng site và cùng chủ.
- **Luật nguồn `/` và `/index.php` (S11).** Không cấm thì một user thường chặn được `tpan stack switch` của cả máy (phép kiểm "Site nào Fail thì từ chối") bằng một luật. Sửa: `redirect_reserved`.
- **Đo e2e sai thứ (S6, E24).** API chỉ chạy một job mỗi lúc trên cả máy, nên 20 request song song cho ra một 202 và 19 `job_running`; cuộc đua thật chỉ có giữa CLI và API. Rồi khi chạy thử, E24 lại sai kỳ vọng lần nữa: ba "người thắng" cùng thuộc một site (log mục 2.1). Sửa khẳng định, và `TestClaimNamesRace` là bằng chứng chính cho B2.

Cách phát hiện chung: đọc mã thật thay vì đọc thiết kế, và chạy thật rồi so log. Việc cần nhớ: mỗi lần dự đoán hành vi của phần mềm khác (WordPress, nginx, certbot) thì phải có một lần đo, không dựa vào trí nhớ.

## 6. Tự kiểm

### Câu hay bị hỏi

- **"Tên phụ khác tên chính chỗ nào?"** Tên chính là `sites.domain`; tên phụ là dòng `kind = 'alias'` trong `site_names`, `serve` (phục vụ nội dung) hoặc `redirect` (301 về tên chính). Cả hai dùng chung một vhost của site.
- **"Sao mặc định là `redirect`, không phải `serve`?"** Một nội dung chỉ có một địa chỉ chính; với WordPress thì `www` kiểu `serve` vẫn bị chính WordPress chuyển về tên trần ở `/` (mục 4.2).
- **"Sao một user không thể thêm `shop.example.com` khi `example.com` của người khác?"** Luật tên miền gốc: `RootForeign` so eTLD+1 của tên với tên của mọi chủ khác. Đó cũng là bản vá P4 của M28; không có hai luật.
- **"Vì sao admin được miễn?"** Admin là người vận hành máy và có thể cố ý gắn tên cho khách. Giống M28. Nhưng admin không được miễn luật một tên một site.
- **"Có ReDoS không?"** Không có regex do người dùng viết. nginx dùng `location =`, Apache dùng `RewriteCond ... =` (so chuỗi).
- **"Thêm tên chưa trỏ DNS có làm hỏng chứng chỉ không?"** Không: thử từng tên trước, tên hỏng bị bỏ khỏi đơn; tên đang trong chứng chỉ mà thử hỏng thì giữ chứng chỉ cũ, không thu nhỏ.
- **"Nâng từ bản cũ có đổi gì không?"** Không: migration chỉ thêm bảng và dòng `primary`; vhost giống từng byte (file vàng).

### Câu tự kiểm

1. Nếu bạn xoá điều kiện `n.Owner == c.Owner` khỏi nhánh "tên chính cùng site" trong `CheckClaims`, lỗi gì xảy ra, trong tình huống cụ thể nào?
2. Vì sao `alice.github.io` và `bob.github.io` được coi là hai chủ khác nhau, còn `www.example.com` và `shop.example.com` thì cùng một chủ? (Gợi ý: nhìn kết quả lệnh 2 ở mục 2.3.)
3. Ở vhost nginx của một site SSL, vì sao tên phụ `serve` chưa có trong chứng chỉ không nằm ở khối cổng 443?
4. Ba người cùng bấm "thêm `www.e24.test`" vào cùng một site: chuyện gì với dòng trong `site_names`, và vì sao log thử đếm 3 "người thắng"?
5. Vì sao đích chuyển hướng không dùng `$host`, dù bản thử nói `$host` an toàn?
6. Một dòng `pending` bị bỏ lại sau khi tiến trình chết. Sau bao lâu `ClaimNames` coi nó là mồ côi, và vì sao cần cơ chế đó? (Gợi ý: `alias rm` có gỡ được tên chính không?)
7. **Cần máy ảo.** Tạo site `example.com`, rồi chạy lệnh để biết `www.example.com` có kiểu gì, và gọi `curl` để chứng minh nó trả 301 chứ không phục vụ nội dung. Bạn cần những lệnh nào, và `Location` đúng là gì?
8. **Cần máy ảo.** Thêm luật `/old-page` rồi gọi `/old-page/` và `/Old-Page`. Bạn kỳ vọng mã gì, và vì sao?
9. Site cũ (từ v0.2.x) nâng lên M19, rồi `tpan health --fix` ghi lại vhost. Bằng chứng nào cho thấy vhost không đổi so với bản trước M19? Còn bằng chứng nào mình chưa có?

### Đáp án

1. Dòng pending mồ côi của X (job `site add` chết sau `ClaimNames`) sẽ bị Y dùng lại khi Y tạo site cùng tên: `SaveSite` ghi chủ Y, còn `site_names` vẫn ghi chủ X. Từ đó DB nói X giữ tên miền gốc của site Y; X thêm được `shop.example.com` vào site của X (vỡ B1), Y lại không thêm được `www` cho chính site mình. Test: `TestCheckClaimsPrimaryRerunOtherOwner`.
2. PSL (mục 4.1): `github.io` là hậu tố private, nên eTLD+1 của hai tên đó khác nhau; `com` là hậu tố ICANN nên `www.example.com` và `shop.example.com` cùng `example.com`. Lệnh 2 in `eTLD+1` tương ứng.
3. Tên chưa nằm trong chứng chỉ mà lên 443 thì trình duyệt thấy chứng chỉ không khớp tên và báo lỗi. `splitNames` đưa nó vào `Group80`: ở cổng 80 chuyển 301 về `https://<tên chính>`, mà chứng chỉ của tên chính hợp lệ. Ở log thử E42: `https://nodns.example.invalid/` curl thoát 35 (bắt tay bị từ chối), `http://...` trả 301 về `https://example.com/`.
4. Ba "người thắng" cùng một site cùng chủ nên theo luật tên phụ pending cùng site, cùng chủ, cùng kiểu thì nhận lại (`reclaim`): chỉ có đúng một dòng trong `site_names` (log ghi "site_names có đúng một dòng"). Nếu hai chủ khác nhau tranh một tên thì chỉ một chủ thắng (`TestClaimNamesRace`).
5. Nếu đích lấy từ Host của request thì kẻ gửi `Host: evil.example.org` điều khiển được trang chuyển khách đi đâu (open redirect). Dùng tên cố định thì khỏi phải tin chứng minh về hành vi của `$host` (bản thử H01 đến H05) và đích Apache tương đối còn phụ thuộc Host (S16). `TestNoHostVariables` giữ điều này.
6. `pendingTTL` = 1 giờ (`internal/store/names.go`). Với tên chính chưa có site thì `alias rm` không áp dụng được (cần site có thật, và từ chối tên chính `alias_primary`), nên không có thao tác nào gỡ; không có cơ chế quá hạn thì tên và cả tên miền gốc bị khoá mãi với chủ khác. Với tên phụ thì `alias rm` gỡ được dòng pending.
7. Ví dụ: `scripts/vm.sh root tpan site alias list example.com` (thấy `www.example.com alias redirect active`) rồi `scripts/vm.sh exec curl -si -H 'Host: www.example.com' 'http://127.0.0.1/a?b=1'`; kỳ vọng `HTTP/1.1 301` và `Location: http://example.com/a?b=1` (đường dẫn và query giữ nguyên). Chưa chạy trong phiên viết; log thử E02 có kết quả đúng như vậy.
8. Kỳ vọng 404 của WordPress cho cả hai, không phải 301: luật chỉ khớp đúng đường dẫn `/old-page`, phân biệt hoa thường và dấu `/` cuối (nginx `location =`, Apache `RewriteCond =`). Log thử E30 ghi đúng như vậy.
9. Có hai lớp. `TestVhostZeroNamesGolden` so output với file vàng sinh từ mã trước M19, cho cả bốn biến thể trên nginx và Apache. Và e2e E60 đến E62 (phần NÂNG-CẤP của `M19.log`, 27 đạt, 0 không đạt): máy cài bản ngay trước M19 (commit `f6a68b9`), hai site, một có SSL, rồi nâng: `site_names` chỉ có hai dòng tên chính, sha256 hai vhost không đổi, `health --fix` ghi lại vhost vẫn ra đúng từng byte, `site add` chạy lại không thêm tên phụ. So với bản ngay trước M19 chứ không so với file do v0.2.0 ghi, vì template đã đổi từ v0.2.0 (các bản vá M14, M28).

## 7. Giới hạn còn lại

- **Ai giữ tên miền gốc trước thì giữ.** User B có thể giữ `example.com` trước khi chủ thật tạo site; luật không phân biệt được. Admin xoá tên phụ hay site của B rồi thêm lại. Cách chặt hơn (xác minh quyền tên miền bằng bản ghi TXT) cần DNS nên để mốc sau.
- **File export chưa mang tên phụ và luật chuyển hướng.** `tpan export` rồi `tpan apply -f` trên máy mới sẽ mất chúng. `tpan export` in cảnh báo ra stderr khi có site mang tên phụ hay luật (hàm `warnExportOmitted` trong `cmd/tpan/site_m19.go`); đây là quyết định S21 (cắt, có cảnh báo). Mình chưa chạy lệnh cảnh báo đó.
- **`tpan ssl issue` và `POST .../ssl` gọi tay không có hạn mức.** Một user vẫn tự gọi lặp được để tốn đơn của tài khoản Let's Encrypt chung. Rủi ro có từ trước M19; M19 chỉ chặn đường tự chạy sau thay đổi tên phụ. Thêm: bộ đếm 5 job mỗi 24 giờ đếm mọi job `site.ssl` của chủ (kể cả gọi tay), chặt hơn đặc tả (comment `ponytail:` trong `followUpSSL`).
- **Tên dài và `server_names_hash_bucket_size`.** TPAN đặt giá trị 64 (`nginx-org-include.conf.tmpl`); nginx có thể từ chối `server_name` dài quá khoảng 46 byte. Thiết kế (S19) ghi là chưa đo. `alias add` hoàn tác được khi `nginx -t` hỏng, nhưng lời lỗi khó hiểu. Có từ trước với tên chính.
- **Dòng pending mồ côi chỉ được dọn khi có `ClaimNames` kế tiếp** (xoá dòng quá 1 giờ trong lần giữ chỗ sau); chưa có tiến trình dọn nền.
- **Dữ liệu cũ hai chủ chung một tên miền gốc:** từ M19 không chủ nào thêm được tên mới trên gốc đó cho tới khi admin gỡ bớt. `names.root.shared` báo.
- **Chuyển hướng:** nguồn chỉ ASCII an toàn (không `%`, không chữ có dấu), khớp đúng, hai luật chuyển qua lại bị phát hiện chỉ một bước. Qua tên phụ `redirect` thì luật đường dẫn chưa áp: có hai bước chuyển (bản thử P11).
- **Cập nhật PSL** chỉ khi nâng `golang.org/x/net` (phiên chính sửa `go.mod`).
- **Giao diện** (tab "Tên miền", "Chuyển hướng", màn `/domains`) có ảnh ở `docs/bang-chung/M19/`; mình không giải thích chúng ở file này vì đi theo khuôn `PreviewedOp` đã có ở các mốc trước. E10, E23, E41 dựa vào `scripts/e2e-m19-gd.sh`, ngoài `e2e-m19.sh`.
