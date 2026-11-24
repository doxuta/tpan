# M28 đợt 2, soát cuối nhóm B: B1 (tên dùng để vào panel) và B2 (điểm mount FUSE)

Đọc kèm: `28c-m28-dot-2-nhom-b.md` (P4 và P7), `28d-m28-dot-2-f1.md` (F1, bản vá mà B2 sửa lại), `docs/thay-doi/M28-dot-2b-soat-cuoi.md` (báo cáo rút gọn), `docs/KE-HOACH.md` mục 6.28 (đoạn "Soát cuối nhóm B"), `docs/thay-doi/M28.md` mục 44.

Phiên viết file này không dùng máy ảo và không chạy gì. Vì vậy:

- Không có khối output nào dưới đây là output thật. Chỗ nào cần output, mình ghi "chưa chạy trong phiên viết" kèm lệnh để bạn tự chạy.
- Các log trong `docs/bang-chung/` chưa có đoạn nào cho bước P4.1b (B1) hay cho B2. Log `M28-dot2d-truoc-ban-va.log` mới có phần đầu (dựng máy). B2 theo đúng báo cáo chỉ có unit test, "chưa thử trên máy ảo".

Hai bản vá này đến từ một thợ soát được giao đặc tả và commit nhưng chưa đọc mã, nhiệm vụ là tìm đường vòng qua các bản vá P4, P7, F1. Thợ tìm ra 12 phát hiện; hai cái đáng vá ngay là B1 (nặng) và B2 (vừa). Các phát hiện còn lại ghi ở `docs/thay-doi/M28-dot-2b-soat-cuoi.md`, không nhắc lại ở đây.

## 1. Mốc này làm được gì

- B1: một khách (vai `user`) không còn tạo được site mang đúng tên mà admin dùng để vào panel. Tên đó bị giữ riêng cho mọi vai, kể cả admin và cả dòng lệnh `tpan site add`. Khách xin thì nhận 400 `domain_reserved`.
- Lý do: site ở tên đó nhận được cookie phiên đăng nhập của panel. Admin mở trang đó một lần là khách có phiên admin, tức là có root. Lỗ này có từ M13, lúc khách bắt đầu tự tạo site được.
- B2: xoá hẳn một site không còn hỏng chỉ vì trong `/tmp` có điểm mount FUSE của user khác. Trước vá, một user bất kỳ đủ làm mọi lần xoá hẳn trên máy thất bại.
- Hai bản vá đều không đổi giao diện. Chỉ B1 có thêm một khoá trong bảng `settings` (`panel.hosts`).

## 2. Chạy thử

Chưa chạy trong phiên viết. Dưới đây là các lệnh để bạn tự chạy; mỗi khối ghi rõ chạy ở đâu.

### 2.1 Test của B1 (chạy ở Mac, trong thư mục repo)

```bash
go test ./internal/core -run 'B1' -count=1 -v
go test ./internal/server -run 'TestDangNhapAdminGiuTenPanelB1' -count=1 -v
```

Ba test ở `internal/core/m28_b1_test.go` (`TestPrepareAddTenCuaPanelB1`, `TestPanelHostNameB1`, `TestRecordPanelHostB1`) và một test ở `internal/server/auth_m28_b1_test.go`. Output: chưa chạy trong phiên viết.

### 2.2 Phép thử e2e của B1 (chạy trong máy ảo, bằng root)

Bước P4.1b của `scripts/e2e-m28-dot2/p4.sh`. Admin đăng nhập bằng `https://panel.example.org:8443` (curl dùng `--resolve` để tên đó trỏ về `127.0.0.1`), rồi `khach1` xin bản xem trước tạo site `panel.example.org`:

- Bản vá (tham số `chan`, mặc định): khẳng định "khach1 không tạo được site mang tên dùng để vào panel" với HTTP 400 và lời chứa `"domain_reserved"`.
- Bản chưa vá (tham số `lo`): script chỉ ghi chú HTTP khach1 nhận được, không khẳng định gì. Đó là cách bộ thử cho thấy lỗ.

Cách chạy trọn từ Mac, giống 28c và 28d (chưa chạy trong phiên viết):

```bash
make e2e-m28-dot2 P="nginx" L=docs/bang-chung/M28-dot2d-thu-lai.log
```

Tên log đặt khác để không đè log gốc. Output: chưa chạy trong phiên viết.

### 2.3 Test của B2 (chạy ở Mac)

```bash
go test ./internal/steps -run 'TestSiteUserAbsentDonDauVet' -count=1 -v
```

Ca của B2 có tên "điểm mount FUSE của user khác: cả hai lần find thoát 1, không in file nào, vẫn userdel (B2)". Muốn chạy riêng một ca, thay khoảng trắng bằng dấu gạch dưới trong `-run`, ví dụ `-run 'TestSiteUserAbsentDonDauVet/điểm_mount_FUSE'`. Output: chưa chạy trong phiên viết.

B2 chưa được thử với một điểm mount FUSE thật. Test dùng máy giả: nó cho lệnh `find` giả thoát 1 và in `Permission denied`.

## 3. Đi qua mã theo thứ tự chạy

### 3.1 B1, chặng một: admin đăng nhập, tên được ghi lại (`internal/server/auth.go`, dòng 473 đến 479)

```go
if u.Role == account.RoleAdmin {
	if err := core.RecordPanelHost(r.Context(), st, r.Host); err != nil {
		slog.Error("ghi tên vào panel", "err", err)
	}
}
```

Đoạn này nằm sau khi mật khẩu đã đúng và phiên đã tạo, ngay trước khi đặt cookie. Chỉ admin được ghi: khách đăng nhập bằng tên nào cũng không làm tên đó thành tên dành riêng (không thì khách tự khoá tên của người khác). Lỗi ghi chỉ vào nhật ký, không làm hỏng lần đăng nhập: admin không đăng nhập được vì một lỗi ghi settings thì tệ hơn việc thiếu một tên trong danh sách.

`r.Host` là giá trị header `Host` trình duyệt gửi, ví dụ `panel.example.org:8443`.

### 3.2 B1, chặng hai: chuẩn hoá và ghi (`internal/core/m28_b1.go`)

`PanelHostName` (dòng 55 đến 68) biến chuỗi `Host` thành tên miền chuẩn:

1. Bỏ cổng bằng `net.SplitHostPort` (không có cổng thì giữ nguyên).
2. Bỏ cặp ngoặc vuông của IPv6.
3. Nếu `net.ParseIP` nhận ra là IP thì trả `ok=false`. IP không phải tên, và không site nào mang tên là IP.
4. Qua `validate.ParseDomain` (cùng bộ lọc tên miền của cả dự án; hạ chữ thường). Không hợp lệ thì `ok=false`.

Bước 4 còn là hàng rào an ninh: `Host` là thứ client tự gửi, nên `a.com; rm -rf /` hay `$(id).example.com` phải bị loại trước khi vào DB. Test `TestPanelHostNameB1` liệt kê các ca đó.

`RecordPanelHost` (dòng 77 đến 99) giữ danh sách trong `settings`, khoá `panel.hosts`, dạng JSON, mới nhất đứng đầu:

```go
if len(names) > 0 && names[0] == d.String() {
	return nil // thường gặp: không ghi lại DB mỗi lần đăng nhập
}
names = slices.DeleteFunc(names, func(n string) bool { return n == d.String() })
names = append([]string{d.String()}, names...)
if len(names) > panelHostsMax {
	names = names[:panelHostsMax]
}
```

Tên đã có ở đầu thì không ghi lại. Tên đã có ở chỗ khác thì rút ra rồi đặt lên đầu (không trùng). Trần `panelHostsMax = 20` để một admin đổi tên liên tục không làm danh sách phình mãi. Mã tự ghi nhận một giới hạn: đọc rồi ghi, không khoá, nên hai admin đăng nhập cùng lúc bằng hai tên khác nhau có thể mất một tên, lần đăng nhập sau ghi lại (comment `ponytail:` ở dòng 75).

### 3.3 B1, chặng ba: tạo site, kiểm tên (`internal/core/site.go`, dòng 514 đến 518, và `m28_b1.go` dòng 139 đến 150)

```go
} else {
	// ...
	if err := s.checkPanelReserved(ctx, d); err != nil {
		return nil, err
	}
```

Đây là nhánh "site mới" của `PrepareAdd`. Site đã có thì chạy lại vẫn được (để sửa hay dựng lại site cũ không bị chặn), nên kiểm ở nhánh này mà không ở trước nhánh. Cả CLI lẫn API đều đi qua `PrepareAdd`, nên "mọi vai, cả dòng lệnh" không cần mã riêng.

`checkPanelReserved`:

```go
names, err := panelHosts(ctx, s.Settings)
...
if !slices.Contains(names, d.String()) && !slices.Contains(s.panelCertNames(), d.String()) {
	return nil
}
return refuse(CodeDomainReserved, i18n.T(...))
```

Hai nguồn tên:

- (a) `panelCertNames` (dòng 117 đến 135): đọc `/etc/tpan/tls/cert.pem` qua `s.Sys.ReadFile` (không qua `os`, nên test dùng máy giả), giải mã PEM, `x509.ParseCertificate`, lấy `DNSNames` hạ chữ thường. `tpan setup` ghi hostname vào chứng chỉ, nên tên đó được giữ ngay từ ngày cài, trước cả lần đăng nhập đầu. Không đọc hay không parse được chứng chỉ (máy demo, máy giả, chưa setup) thì trả rỗng, không làm hỏng việc tạo site.
- (b) `panelHosts`: danh sách ở `settings`, tức các `Host` đã thấy ở lần đăng nhập đúng của admin. Giá trị hỏng thì coi như rỗng.

Vì sao cần cả (b): chứng chỉ chỉ biết tên mà `tpan setup` biết. Admin có thể vào panel bằng tên khác (một bí danh DNS, hay panel chạy bằng `--tls-cert` ở đường dẫn khác, `ponytail:` dòng 16). Tên admin thực sự dùng là tên nguy hiểm, không phải tên đã khai lúc cài. Vì thế bản vá lấy cả `Host` của lần đăng nhập.

Từ chối trả `refuse(CodeDomainReserved, ...)`. `internal/server/api.go` dòng 126 xếp `core.CodeDomainReserved` vào nhóm 400 (từ M17, mã này đã dùng cho tên miền dành cho phép thử tấn công). Lời báo nêu tên và hướng dẫn: vào panel bằng một tên khác hoặc bằng IP. Có hai bản, tiếng Việt và tiếng Anh, qua `i18n.T`.

### 3.4 B2, chặng một: xoá hẳn gọi bước dọn (`internal/steps/site_user.go`, `userPurge`, dòng 572 đến 587)

`userPurge` làm theo thứ tự: gỡ crontab, giết tiến trình, rồi với mỗi thư mục trong `userTmpDirs` (`/tmp`, `/var/tmp`, `/dev/shm`, theo 28d) gọi `userTmpClean`. Bất kỳ bước nào trả lỗi thì `userdel` không chạy và job hoàn tác. Đó là chủ ý của F1: không xoá user khi còn dấu vết dưới UID của nó.

### 3.5 B2, chặng hai: `userTmpClean` (dòng 646 đến 666)

```go
if res, err := sys.Run(ctx, userFindFiles(dir, uid)); err != nil {
	left, lerr := sys.Run(ctx, userFindLeft(dir, uid))
	if strings.TrimSpace(left.Stdout) != "" {
		return withStdout(fmt.Errorf("xoá file của UID %s trong %s (còn %q, hỏi lại: %v): %w",
			uid, dir, strings.TrimSpace(left.Stdout), lerr, err), res)
	}
}
_, _ = sys.Run(ctx, userFindDirs(dir, uid))
return nil
```

Ba lệnh, đều dựng ở dòng 468 đến 481 (argv, không `sh -c`):

- `userFindFiles`: `find <dir> -xdev -uid <uid> ! -type d -delete`. Xoá mọi thứ không phải thư mục thuộc UID, không vượt sang ổ đĩa khác (`-xdev`).
- `userFindLeft`: cùng điều kiện nhưng `-print -quit`: in file đầu tiên còn lại rồi dừng.
- `userFindDirs`: xoá thư mục rỗng của UID.

Bản F1 đầu coi mọi mã thoát khác 0 của lệnh xoá là hỏng. Bản vá B2 đổi điều kiện hỏng: lệnh xoá thoát khác 0 thì hỏi lại, và chỉ hỏng khi lần hỏi lại in ra một file của UID. Mã thoát của lần hỏi lại bị bỏ (biến `lerr` chỉ để ghi vào lời báo): lần hỏi lại gặp đúng điểm mount đó nên cũng thoát 1, và điều đó không nói gì về việc còn file hay không. Chỉ có đầu ra nói.

Test của ca B2 (`internal/steps/site_user_f1_test.go`, dòng 385 đến 395) cho cả hai lệnh `find` giả thoát 1 với lời `Permission denied` và không in file nào, rồi khẳng định `Apply` thành công và `userdel` có chạy. Ca bên cạnh (dòng 397 đến 408) là chiều ngược: lệnh xoá lỗi và lệnh hỏi lại in `/var/tmp/bash` thì `Apply` lỗi, `userdel` không chạy.

## 4. Kiến thức nền

### 4.1 Cookie, `__Host-`, và vì sao nó không tách theo cổng

Cookie là một mẩu chữ mà server dặn trình duyệt giữ, rồi trình duyệt tự gửi lại ở mọi request sau tới cùng server. TPAN dùng cookie làm "vé" phiên đăng nhập: ai cầm vé là được coi là admin. Mã đặt cookie ở `internal/server/auth.go`, hàm `authSetCookie`, có `HttpOnly` (script trên trang không đọc được vé), `Secure` (chỉ gửi qua HTTPS) và `SameSite=Strict` (không gửi kèm request xuất phát từ trang của một site khác).

Tên `__Host-tpan` có tiền tố `__Host-`. Trình duyệt chỉ nhận cookie có tiền tố này nếu nó có `Secure`, `Path=/` và không có thuộc tính `Domain`. Nghĩa là cookie gắn với đúng một tên máy (host-only): trang ở tên miền con hay tên miền cha không đọc và không ghi đè được (comment dòng 47 đến 49 của `auth.go`).

Điều tiền tố này không làm: nó không gắn cookie với một cổng. Theo quy tắc cookie của trình duyệt, cookie gắn với tên máy, không với cổng. Panel ở `https://panel.example.org:8443` và một site ở `https://panel.example.org/` (cổng 443) là hai "origin" khác nhau với script, nhưng với cookie thì chúng là cùng một tên máy. Nên khi admin đang đăng nhập và mở site đó, trình duyệt gửi cookie `__Host-tpan` tới nó. Trang đó lại tự gọi chính nó, là request cùng site, nên `SameSite=Strict` không chặn.

Hệ quả: site ở đúng tên máy của panel nhận được vé admin, dù trình duyệt không bao giờ đưa vé đó cho site ở tên khác. Vì thế bản vá giữ riêng tên, chứ không đổi cookie.

Tự nhìn: mở DevTools của trình duyệt, tab Application, mục Cookies, chọn tên máy của panel. Cột Domain/Path không có cột cổng. Chưa chạy trong phiên viết (cần panel và trình duyệt thật).

### 4.2 Tên nào được giữ

- Mọi tên DNS trong chứng chỉ của panel (`/etc/tpan/tls/cert.pem`), nguồn (a).
- Mọi tên (`Host`, bỏ cổng, không nhận IP) mà admin đã dùng ở lần đăng nhập thành công, nguồn (b). Tối đa 20 tên gần nhất.

Chỉ so đúng một tên, không so tên miền gốc: giữ `panel.example.org` không cấm `shop.example.org`. Cookie host-only chỉ rơi vào site có đúng tên đó, nên giữ đúng tên là đủ và không chặn nhầm khách.

Tự nhìn trên máy ảo (chưa chạy trong phiên viết):

```bash
# máy ảo, root: tên trong chứng chỉ của panel
openssl x509 -in /etc/tpan/tls/cert.pem -noout -ext subjectAltName
# máy ảo, root: tên admin đã dùng để đăng nhập (danh sách JSON, mới nhất trước)
python3 -I -c 'import sqlite3; c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True); print(c.execute("SELECT value FROM settings WHERE key = ?", ("panel.hosts",)).fetchall())'
```

Câu Python giống kiểu `p1.sh` dùng để đọc `tpan.db`. Dòng `fetchall` ra danh sách rỗng nếu chưa ai (admin) đăng nhập bằng tên miền.

### 4.3 Vì sao 400, không phải 404

Ở P4 (28c), khách xin tên của người khác nhận 404 giống hệt site không tồn tại, để khách không dò được người khác có site gì. Ở B1 không có gì để giấu: tên máy của panel khách vốn đã biết (chính họ vào panel ở đó). Nên trả 400 kèm mã `domain_reserved` và lời giải thích, để khách hiểu vì sao bị chặn (nhật ký mục 44 của `docs/thay-doi/M28.md`).

### 4.4 Vào panel bằng một tên riêng hay bằng IP

Lời khuyên trong thông báo lỗi và rút ra từ bản vá:

- Vào panel bằng một tên riêng, chỉ dùng cho panel (ví dụ một tên con như `panel.example.com`, không dùng làm site). Tên đó thành tên dành riêng, không ai mở site ở đó, nên không có site nào nhận cookie.
- Hoặc vào panel bằng IP. Cookie `__Host-` gắn với IP đó, không với tên miền nào. `PanelHostName` không ghi IP (xem `TestPanelHostNameB1`), nên đăng nhập bằng IP không khoá tên nào.
- Điều nên tránh: vào panel bằng chính tên miền của một site thật (ví dụ `example.com` đang chạy WordPress). Máy nâng cấp lên bản này mà đã có site mang tên của panel thì bản vá không đụng được site đó: nó chỉ chặn site mới. Chưa có phép kiểm health nào báo tình huống này (comment `ponytail:` dòng 14 của `m28_b1.go`).

### 4.5 FUSE ở mức lời thường

Thông thường chỉ nhân (kernel) Linux mới biết cách đọc ổ đĩa. FUSE (Filesystem in USErspace) cho một chương trình thường, không cần quyền root, tự trình bày một thư mục như thể nó là một ổ đĩa: ai đọc thư mục đó thì chương trình kia trả lời. Ví dụ quen thuộc: `sshfs` (gắn thư mục máy khác vào máy này), `fuse-overlayfs` (Podman chạy không root dùng nó để dựng hệ file của container).

Mặc định, chỉ chính user đã mount mới được dùng điểm mount FUSE đó. Người khác, kể cả root, nhận `Permission denied`, trừ khi user mount bật tuỳ chọn `allow_other`. Đây là một cơ chế an ninh của nhân (tránh để user thường lừa root vào một thư mục do mình điều khiển).

Điểm mount là chỗ thư mục "gắn" vào cây thư mục. `/tmp/k1/m` trong test là một điểm mount như vậy.

Tự nhìn trên máy ảo (chưa chạy trong phiên viết):

```bash
# máy ảo: liệt kê các điểm mount FUSE (không có thì không in gì)
findmnt -t fuse,fuse.sshfs,fuse.fuse-overlayfs
# máy ảo: thiết bị FUSE của nhân
ls -l /dev/fuse
```

Mình chưa dựng điểm mount FUSE trên máy ảo, nên cũng chưa có output của `find` gặp nó.

### 4.6 Vì sao `find` của root thoát 1

`find` duyệt cả cây dưới `/tmp`. Khi nó vào điểm mount FUSE của user khác và nhận `Permission denied`, nó in lỗi lên stderr, tiếp tục duyệt phần còn lại, và đến cuối thoát với mã 1 ("có lỗi khi duyệt"). Mã 1 không có nghĩa là các file khác không xoá được. F1 bản đầu coi mã khác 0 là hỏng, nên một user có điểm mount FUSE trong `/tmp` (cố ý, hoặc vô tình khi chạy Podman không root) làm mọi lần xoá hẳn của mọi khách thất bại, kể cả bộ hẹn giờ dọn thùng rác, vì chúng đều đi qua `userPurge`. Mà job xoá hẳn hoàn tác khi hỏng nên không dọn được, mỗi lần thử lại cũng hỏng.

`-xdev` dặn `find` không bước vào ổ khác, nhưng báo cáo ghi rằng `find` vẫn thoát 1 trong trường hợp này. Mình chưa tái hiện để biết chính xác `find` dừng ở bước nào (chưa chạy).

### 4.7 Vì sao giờ chỉ hỏng khi còn thấy file của UID, và vì sao an toàn

Mục đích của bước dọn là: đừng để lại file nào mang số UID của user sắp xoá, vì số đó có thể rơi vào tay user khác (đúng lỗ F1 trong 28d). Hai tình huống khi `find` thoát khác 0:

- Lần hỏi lại in ra một file của UID: còn dấu vết thật, hỏng, không `userdel`.
- Lần hỏi lại không in gì: không thấy file nào của UID ở những nơi `find` đọc được. Phần `find` không đọc được là dưới điểm mount FUSE của user khác. File của UID có thể nằm dưới đó, nhưng F1 đảm bảo UID không bao giờ cấp lại (số ghi trong `tpan.db` và chỉ tăng, 28d mục 3.4 và 4.4). File mồ côi mang số UID đã chết không thuộc về ai, không có user nào mở được nó với tư cách chủ. Nên bỏ sót chúng không tái lập lỗ F1.

Nói cách khác, bước dọn là lớp thứ hai; lớp thứ nhất (UID không dùng lại) mới là lớp giữ an toàn. Bước dọn chỉ làm gọn máy. Đó là lý do bản vá đổi nó thành "cố gắng tối đa".

Một chỗ cần nhớ khi bị hỏi: lần hỏi lại in ra tên file thì mới tính là hỏng, kể cả khi mã thoát của nó là 1. Test dòng 397 đến 408 là ca này.

## 5. Cách khác đã cân nhắc

Phần này gồm những gì có thể rút ra từ báo cáo và mã. Chỗ nào báo cáo không ghi lý do, mình ghi rõ.

B1:

1. Chỉ giữ tên theo (a) (chứng chỉ). Không chọn: chứng chỉ chỉ biết tên mà `tpan setup` biết; admin vào panel bằng tên khác thì tên đó không được giữ. Bản vá thêm (b).
2. Chỉ giữ tên theo (b) (đăng nhập). Không chọn: tên chỉ được ghi từ lần đăng nhập đầu của admin; trước đó khách có thể đặt site đúng tên của chứng chỉ. (a) che khoảng đó.
3. Đổi cách đặt cookie để gắn với cổng. Không thể: trình duyệt không có cơ chế gắn cookie với cổng, nên không có cách đặt nào từ phía server để làm việc đó. Vì vậy chuyển sang giữ riêng tên. (Đây là lý giải từ cách cookie hoạt động; báo cáo không nêu cách này như một phương án đã bàn.)
4. Trả 404 giống P4. Không chọn: tên của panel khách đã biết, không có gì để giấu; 400 kèm lời giải thích hữu ích hơn.

B2:

1. Giữ nguyên (mã thoát khác 0 là hỏng). Không chọn: đó chính là lỗi.
2. Bỏ qua mọi lỗi của `find`, không hỏi lại. Không chọn: file của UID còn lại ở nơi đọc được là dấu vết thật, UID mới không dùng lại nhưng F1 vẫn muốn máy sạch ở những chỗ có thể sạch. Lần hỏi lại là cách phân biệt "lỗi vì điểm mount" với "lỗi và còn file".
3. Dùng cờ của `find` để bỏ qua điểm mount. Không có trong báo cáo hay mã; mình không viết điều chưa kiểm.

### Chỗ đã làm sai

- B1: P4 (28c) chỉ so tên với các site đang có, không nhìn tên của chính panel. Thợ soát chưa đọc mã, chỉ đọc đặc tả, và tìm ra đường vòng đó. Mình ghi lại cách phát hiện đúng như báo cáo: soát đối kháng không đọc mã, không dùng máy ảo.
- B2: F1 bản đầu coi mã thoát khác 0 của `find` là hỏng. Cùng thợ soát tìm ra: một điểm mount FUSE của user khác làm `find` thoát 1. Podman không root cũng gây ra mà không cần ai cố ý. Phát hiện bằng đọc mã, chưa tái hiện trên máy ảo.
- Cả hai đều chưa thử trên một máy có điểm mount FUSE thật hay một trình duyệt thật. B1 có test đơn vị và một bước e2e (P4.1b, chưa có log); B2 chỉ có test đơn vị.

## 6. Tự kiểm

1. Cookie `__Host-tpan` là host-only. Vì sao site ở `https://<tên máy>/` vẫn nhận được nó khi panel ở `https://<tên máy>:8443`?
2. `SameSite=Strict` có cứu được không? Vì sao?
3. Bản vá giữ riêng những tên nào? Nêu hai nguồn và lý do cần cả hai.
4. Vì sao lấy cả `Host` của lần đăng nhập, chứ không chỉ tên trong chứng chỉ? Và vì sao chỉ lấy khi người đăng nhập là admin?
5. Khách xin site mang tên `panel2.example.org` mà admin đã đăng nhập bằng tên đó. Khách nhận mã HTTP nào, mã lỗi nào, và vì sao không phải 404?
6. Admin đăng nhập bằng `https://192.0.2.10:8443`. Tên nào được thêm vào `panel.hosts`? Lệnh nào kiểm được trên máy ảo?
7. Site `old.example.net` đã có từ trước, rồi tên đó vào chứng chỉ. `tpan site add old.example.net` lần nữa có bị chặn không? Vì sao?
8. FUSE là gì, và vì sao root cũng nhận `Permission denied` ở một điểm mount FUSE của user khác?
9. Bản F1 đầu hỏng thế nào khi `find` gặp điểm mount đó, và bản B2 đổi điều kiện hỏng thành gì? Vì sao mã thoát của lần hỏi lại bị bỏ?
10. Vì sao bỏ sót file dưới điểm mount đó mà vẫn `userdel` là an toàn?

## Đáp án

1. Cookie gắn với tên máy, không gắn với cổng. Trình duyệt coi `<tên máy>:8443` và `<tên máy>:443` là cùng tên máy khi gửi cookie. `__Host-` chỉ buộc cookie là host-only, `Secure`, `Path=/`, không `Domain`; nó không nói gì về cổng (mục 4.1).

2. Không. Trang ở `https://<tên máy>/` tự gọi lại chính nó, là request cùng site (same-site). `SameSite=Strict` chỉ ngăn cookie đi kèm request xuất phát từ một site khác. Báo cáo nói đúng điểm này (`docs/thay-doi/M28-dot-2b-soat-cuoi.md`, mục B1).

3. (a) Các tên DNS trong chứng chỉ `/etc/tpan/tls/cert.pem`, có từ lúc `tpan setup`, nên che được từ trước lần đăng nhập đầu. (b) Các `Host` (bỏ cổng, không nhận IP) của lần đăng nhập thành công của admin, lưu ở `settings` `panel.hosts`, tối đa 20 tên. (a) không thấy tên admin dùng nhưng không có trong chứng chỉ (hay chứng chỉ ở đường dẫn khác); (b) chưa có tên nào trước lần đăng nhập đầu. Hợp cả hai thì kín hơn.

4. Tên nguy hiểm là tên mà admin thực sự gõ vào trình duyệt, vì chỉ ở tên đó cookie phiên mới rơi vào site. Chứng chỉ chỉ ghi tên đã khai lúc cài. Chỉ lấy khi là admin, vì phiên đáng bảo vệ là phiên admin; nếu khách đăng nhập cũng ghi tên thì khách khoá được tên của người khác. Test `TestDangNhapAdminGiuTenPanelB1` có ca khách B đăng nhập bằng `panel2.example.org` mà `panel.hosts` vẫn không được ghi.

5. HTTP 400, mã lỗi `domain_reserved` (`apiErrorStatus` xếp mã này vào nhóm 400 ở `internal/server/api.go` dòng 126). Không phải 404 vì tên máy của panel khách vốn đã biết, nên không có gì để giấu như ở P4; lời giải thích giúp khách hiểu.

6. Không tên nào: `PanelHostName` loại IP (`net.ParseIP` nhận ra) và trả `ok=false`, nên `RecordPanelHost` không ghi. Kiểm trên máy ảo (chưa chạy trong phiên viết): đăng nhập admin bằng IP rồi chạy câu Python đọc `panel.hosts` ở mục 4.2; nếu chưa ai đăng nhập bằng tên miền thì không có dòng nào. Test: `TestPanelHostNameB1` ca `192.0.2.10:8443`.

7. Không bị chặn. `checkPanelReserved` chỉ gọi ở nhánh site mới của `PrepareAdd` (`internal/core/site.go` dòng 514 đến 518). Test `TestPrepareAddTenCuaPanelB1` kiểm đúng ca này: cert có `old.example.net`, site đã tạo trước đó, chạy lại vẫn qua. Mặt trái: một site đã có mang tên panel vẫn tồn tại và vẫn nhận cookie; chưa có phép kiểm health báo điều đó.

8. FUSE cho chương trình thường tự trình bày một thư mục như một ổ đĩa. Mặc định chỉ user đã mount được dùng điểm mount; người khác, kể cả root, bị nhân từ chối trừ khi có `allow_other`. Đó là cơ chế để user thường không lừa được root đi vào một thư mục do mình điều khiển (mục 4.5).

9. Bản F1 đầu: lệnh `find ... -delete` thoát khác 0 là coi cả bước hỏng, không `userdel`, job hoàn tác. Bản B2: lệnh xoá thoát khác 0 thì chạy `find ... -print -quit`; chỉ hỏng khi nó in ra tên một file. Mã thoát của lần hỏi lại bị bỏ vì nó gặp lại đúng điểm mount và cũng thoát 1, nên mã đó không phản ánh việc còn file hay không; chỉ stdout phản ánh.

10. Vì bước dọn chỉ là lớp thứ hai. Lớp thứ nhất của F1: UID chỉ tăng, không bao giờ cấp lại, nên file mồ côi mang UID cũ không thuộc về user nào mới. Bỏ sót chúng không tái lập lỗ "site mới thừa kế file của site cũ" (28d). Bước dọn vẫn xoá những file đọc được và vẫn hỏng nếu còn thấy file của UID.

Hai câu cần máy ảo mới trả lời được: câu 6 (đọc `panel.hosts` sau khi đăng nhập admin bằng IP, rồi bằng tên) và câu 3 (đọc SAN của chứng chỉ bằng `openssl x509 -in /etc/tpan/tls/cert.pem -noout -ext subjectAltName`). Cả hai: chưa chạy trong phiên viết.
