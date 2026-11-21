# M28: vá lỗ hổng T1 sau rà soát v0.2.0, "panel chạy root không tin thứ nằm trong thư mục site"

Đọc kèm: `14-m14-co-lap-va-dinh-tuyen.md` (chín đường L1 đến L9 mà M14 đã chặn, user Linux của từng site, quyền 2750), `13-m13-nhieu-nguoi-dung.md` (vai `admin` và `user`), `docs/thay-doi/M28-thiet-ke.md` (bản thiết kế đầy đủ, bảng bất biến, phát hiện của thợ soát), `docs/thay-doi/M28.md` (quyết định và lý do), `docs/thay-doi/M28-soat-cuoi.md` (thợ soát cuối, chưa đọc mã mốc này).

Cách đọc file này: phần "Nói trong một phút" là thứ bạn nói được khi bị hỏi đột xuất. Muốn trả lời sâu thì đọc mục 3. Muốn tự chạy lại thì đọc mục 7. Các câu hỏi của người review nằm ở mục 8, đáp án ở cuối file.

Quy ước: **K1** là mã chạy dưới user Linux của site (ví dụ PHP của một site bị chiếm). **K2** là tài khoản panel vai `user`, chủ của site đó. Site thử trong log là `e.example.com`, chủ là `khach1`. Nạn nhân là `a.example.com` (site của admin).

## Nói trong một phút

Panel TPAN chạy bằng root. Root đọc được mọi file trên máy. Khi bạn xin "xem trước" việc tạo một site, panel đọc nội dung file `.htaccess` hiện có của site để vẽ so sánh cũ và mới. Đường dẫn của file đó nằm trong thư mục của site, mà thư mục đó là của user Linux của site: user đó tạo, xoá, đổi tên được mọi thứ trong đó, kể cả biến `.htaccess` thành symlink (một cái "lối tắt") trỏ tới `/etc/shadow`. Root đọc theo lối tắt thì ra `/etc/shadow`, rồi panel trả nội dung đó cho chủ site qua API. Mình đã tái hiện trên máy ảo: đọc được `wp-config.php` của site khác, `/etc/shadow`, cả file cơ sở dữ liệu của panel.

M14 chặn những gì *site* làm được với file của site khác. Lỗ này khác: site không tự đọc, mà nhờ *panel* đọc hộ bằng quyền root. Vì vậy chín đường của M14 không chạm tới nó.

Bản vá không bỏ riêng dòng `.htaccess`. Mình đặt một cái cửa (gói `internal/sitefs`) cho mọi chỗ panel chạm vào `/var/www/<tên>/`. Cửa mở từng cấp thư mục bằng cách không đi theo symlink, chỉ đọc file thường của đúng user site, tối đa 1 MiB, không bao giờ treo. Thêm một lớp thứ hai ở API: nội dung trả cho vai `user` chỉ được giữ khi chính user Linux của site đọc ra đúng những byte đó. Hỏng một lớp thì lớp kia vẫn chặn.

Bài học một câu: panel chạy root không được tin thứ nằm trong thư mục site. Tên file trong đó không nói gì về thứ nó trỏ tới.

## 1. Mốc này làm được gì

- Chủ site (vai `user`) không còn lấy được nội dung file ngoài site của mình qua bản xem trước. `.htaccess` là symlink thì bản xem trước nói "thay symlink (đích) bằng file thường" và không đọc đích.
- Một file khổng lồ (2 GiB), FIFO hay `/dev/zero` đặt vào chỗ `.htaccess` không còn làm panel hết RAM hay treo: câu trả lời đọc được về trong vài giây, panel giữ nguyên PID.
- `.htaccess` là file thường thì bản xem trước vẫn hiện so sánh y như v0.2.0.
- Vai `user` không nhận nội dung các file của máy (ví dụ vhost mặc định); đường dẫn và tên bước vẫn hiện, JSON có trường `hidden`.
- Nâng từ v0.2.0 không migration, không đổi file nào trên đĩa, site đang chạy không đổi gì.

Kết quả bộ e2e bước 31 trên bản cuối (commit `81a83b4`: đã gộp `dev` có M16, có các bản sửa C12, C14; mỗi stack từ máy ảo sạch): 170 khẳng định đạt, 0 không đạt (`docs/bang-chung/M28.log`): Apache 86, nginx 54, test cần root trong máy ảo 1, nâng từ v0.2.0 29.

## 2. Chạy thử

Mọi output dưới đây trích nguyên văn từ file bằng chứng có trong repo, kèm tên file và số dòng. Máy ảo trong log là `tpan-m28` (Multipass, Ubuntu 24.04, kernel `6.8.0-147-generic`).

Nguồn:

- `docs/bang-chung/M28-nghien-cuu/n1-t1-truoc-ban-va.ket-qua.txt`: tái hiện T1 trên bản chưa vá (commit `4269cc1`).
- `docs/bang-chung/M28-nghien-cuu/n2-so-cach-mo.ket-qua.txt`: so năm cách root mở một file trong thư mục của user khác.
- `docs/bang-chung/M28-nghien-cuu/n3-lenh-root.ket-qua.txt`: lệnh `tar`, `rm`, `du`, `find -exec chmod` chạy bằng root trên cây của site.
- `docs/bang-chung/M28-truoc-ban-va.log`: bộ e2e M28 chạy trên bản chưa vá. Những dòng `KHÔNG ĐẠT` ở đó là điều mong muốn: nó chứng minh bộ thử bắt được lỗ. Dòng cuối: `TỔNG KẾT: 39 khẳng định đạt, 21 khẳng định không đạt`.
- Năm lệnh `go test` mình vừa chạy trên Mac trong phiên viết (mục 2.5).
- `docs/bang-chung/M28.log`: bộ e2e trên bản cuối (`81a83b4`, nhãn `tpan v0.2.0-103-g81a83b4`), gồm cả phần C (test cần root trong máy ảo) và phần D (nâng từ v0.2.0). Dòng cuối: `TỔNG KẾT: 170 khẳng định đạt, 0 khẳng định không đạt`.
- `docs/bang-chung/M28-tren-dev.log`: cùng bộ e2e chạy lại trên đúng commit đẩy lên `dev` (`f403aab`, đã gộp thêm M18): 170 khẳng định đạt, 0 không đạt.
- Ba hồi quy trên bản cuối (`81a83b4`): M13 293, M14 270, M15 200 khẳng định đạt, 0 không đạt (`docs/bang-chung/M28-hoi-quy-m13.log`, `-m14.log`, `-m15.log`), đúng bằng số của chính các mốc đó.

### 2.1 Lỗ T1 trên bản chưa vá (n1-t1, dòng 9 tới 36)

`.htaccess` là file thường (hành vi đúng):

```
KQ thuong:.htaccess: DUNG (old đúng bằng file trên đĩa, vẽ được diff)
```

Rồi user của site biến nó thành symlink và `khach1` gọi `POST /api/v1/sites?dry_run=1`:

```
=== 1. .htaccess -> symlink tới /var/www/a.example.com/public/wp-config.php ===
  file /var/www/e.example.com/public/.htaccess: old 3480 byte sha=6b4539e96a17ba18 {'old_exists': True} TRÙNG file đích
KQ symlink:/var/www/a.example.com/public/wp-config.php: LO (file đích 3480 byte sha=6b4539e96a17ba18 nằm trong bản xem trước)

=== 2. .htaccess -> symlink tới /etc/shadow ===
KQ symlink:/etc/shadow: LO (file đích 980 byte sha=979bb4db2557b58a nằm trong bản xem trước)

=== 3. .htaccess -> symlink tới /var/lib/tpan/tpan.db ===
KQ symlink:/var/lib/tpan/tpan.db: LO (file đích 884736 byte sha=7091b7f706fe1f3d nằm trong bản xem trước)
```

Đọc dòng này thế nào: `old` là trường của JSON bản xem trước chứa nội dung cũ của file. `sha` rút gọn của `old` trùng sha của file đích, nghĩa là byte nào cũng giống. Script không in nội dung thật của `/etc/shadow`.

Biến thể thư mục cha: `public` thành symlink tới thư mục chỉ root đọc được (dòng 32 tới 36): `KQ cha-la-symlink: LO`. Hardlink: kernel Ubuntu từ chối `ln`, nhưng khi tắt `fs.protected_hardlinks` thì `ln` được và cũng lộ (dòng 38 tới 44).

Từ chối phục vụ (dòng 47 tới 64):

```
KQ file-2GiB: câu trả lời không phải JSON (0 byte)
  tpan: PID 17735 -> 0, activating, NRestarts=2
KQ fifo: câu trả lời không phải JSON (0 byte)
KQ dev-zero: câu trả lời không phải JSON (0 byte)
  tpan: PID 21378 -> 0, activating, NRestarts=3
Nov 21 03:24:11 tpan-m28 kernel: Out of memory: Killed process 21378 (tpan) total-vm:8372032kB, anon-rss:3580840kB
```

File 2 GiB và `/dev/zero` làm kernel giết panel. FIFO làm request treo quá 20 giây.

### 2.2 Bộ e2e trên bản chưa vá bắt được lỗ (M28-truoc-ban-va.log)

Dòng 132 tới 150 và 165, 177 (đúng dạng `KHÔNG ĐẠT` mong muốn):

```
  => KHÔNG ĐẠT: symlink -> /etc/shadow: câu trả lời không chứa file đích
  => KHÔNG ĐẠT: symlink -> /etc/shadow: HTTP 200, mục .htaccess có old_link, không có old
  => KHÔNG ĐẠT: 4a: không câu trả lời nào trong 1000 câu chứa /etc/shadow
  => KHÔNG ĐẠT: 4b: không câu trả lời nào trong 300 câu chứa file của thư mục nạn nhân
```

Dòng 163 tới 175 cho biết đua: với 1000 lần xin bản xem trước trong lúc kẻ tráo đổi `.htaccess` qua lại giữa file thường và symlink tới `/etc/shadow`, bản chưa vá làm lộ 941 lần (`941 câu trả lời chứa file đích`). Tráo `public` thì lộ 160 trong 300 lần.

### 2.3 Năm cách mở file, đua 20000 lần (n2, dòng 90 tới 102)

```
=== đua: tráo file thường <-> symlink tới bí mật, mỗi cách đọc 20000 lần ===
  readfile  20000 lần: LỘ 9757, đọc file của site 10243, từ chối 0
  lstat     20000 lần: LỘ 95, đọc file của site 5232, từ chối 14673
  osroot    20000 lần: LỘ 0, đọc file của site 12211, từ chối 7789
  openat2   20000 lần: LỘ 0, đọc file của site 7385, từ chối 12615
  walk      20000 lần: LỘ 0, đọc file của site 10585, từ chối 9415

=== đua: tráo thư mục public <-> symlink tới thư mục nạn nhân, mỗi cách đọc 20000 lần ===
  readfile  20000 lần: LỘ 9425, ...
  lstat     20000 lần: LỘ 9966, ...
  osroot    20000 lần: LỘ 0, ...
  openat2   20000 lần: LỘ 0, ...
  walk      20000 lần: LỘ 0, ...
```

`readfile` là `os.ReadFile` (cách của `engine.Preview` cũ). `lstat` là "kiểm bằng `os.Lstat` rồi đọc" (cách của các Step ở v0.2.0). `walk` là cách bản vá chọn. Bảng loại file (n2 dòng 14 tới 88) nói thêm: `lstat` vẫn đọc hết file 2 GiB, vẫn đọc qua `public` là symlink; `osroot` đọc được symlink trỏ tới file khác *trong* site; cả năm cách đều đọc được hardlink tới file bí mật (dòng 88: `cả năm cách đều đọc: hardlink là file thường`).

### 2.4 Lệnh root trên cây site (n3) và test cần root

n3 dòng 36 tới 39: `find <public> -type d -exec chmod 2750 {} +` chạy bằng root (cách `WPFiles` gọi trước M28) thua đua:

```
KQ find-chmod: LO sau 5 vòng: root đã chmod thư mục nạn nhân thành 2750
```

Trong lúc `find` liệt kê và lúc `chmod` chạy, user của site tráo một thư mục con thành symlink; root đổi quyền thư mục nạn nhân. `tar --create`, `rm -r --one-file-system`, `du -sb` thì an toàn (dòng 15, 29, 34).

Test cần root, chạy trong máy ảo bằng root trên đúng bản cuối (phần C của `docs/bang-chung/M28.log`, từ dòng 633: `make m28-test-vm` build `go test -c` cho Linux, đẩy vào máy ảo, chạy bằng root). Các dòng kết quả tiêu biểu:

```
--- PASS: TestCuaSymlinkKhongDoiChuThuBiTrao (0.00s)
--- PASS: TestCuaKhongTreo (0.00s)
--- PASS: TestCuaChuFile (0.00s)
    gate_test.go:355: đọc được 39, từ chối 961
    gate_test.go:355: đọc được 244, từ chối 756
--- PASS: TestCuaDua (0.01s)
--- PASS: TestCuaDuaChmod (0.00s)
--- PASS: TestCuaGocKhongAnToan (0.00s)
--- PASS: TestLenhRootHuyKhongDeLaiTienTrinh (1.26s)
--- PASS: TestVungSiteDocLaiBangUserSite (0.02s)
  => ĐẠT: m28-test-vm PASS, không có dòng SKIP
```

Trên Mac các test này tự bỏ qua (cần root và Linux); "không có dòng SKIP" trong máy ảo nghĩa là chúng chạy thật.

### 2.5 Test trên Mac, mình vừa chạy trong phiên này

Chạy trong thư mục repo trên Mac (những test cần root tự bỏ qua trên Mac):

```
go test -count=1 ./internal/engine -run 'TestXemTruoc|TestSuKien' -v
--- PASS: TestXemTruocKhongDocQuaSymlink (0.00s)
--- PASS: TestXemTruocSymlinkHienDich (0.00s)
--- PASS: TestXemTruocFileThuongGiuDiff (0.00s)
--- PASS: TestXemTruocChaLaSymlink (0.00s)
--- PASS: TestXemTruocLoaiKhac (0.00s)
--- PASS: TestXemTruocNgoaiVungNhuCu (0.00s)
--- PASS: TestSuKienKhongChuaNoiDungFile (0.00s)
```

```
go test -count=1 ./internal/sitefs -run 'TestCuaKhongDiTheoSymlink$|TestCuaDua$|TestCuaChiDocFileThuong|TestCuaKhongTreo' -v
--- PASS: TestCuaKhongDiTheoSymlink (0.06s)
--- PASS: TestCuaChiDocFileThuong (0.00s)
    --- SKIP: TestCuaChiDocFileThuong/thiet-bi (0.00s)
--- PASS: TestCuaKhongTreo (0.00s)
    gate_test.go:355: đọc được 375, từ chối 625
    gate_test.go:355: đọc được 225, từ chối 775
--- PASS: TestCuaDua (0.09s)
```

Số "đọc được / từ chối" đổi mỗi lần chạy vì đây là cuộc đua thật giữa hai goroutine. Điều không đổi là không lần nào đọc ra nội dung ngoài site.

```
go test -count=1 ./internal/steps -run 'TestChmodDirs|TestWPFiles' -v
--- PASS: TestWPFilesKhongChayLenhRoot (0.00s)
--- PASS: TestWPFilesDatQuyenQuaCua (0.00s)
--- PASS: TestChmodDirsLoi (0.00s)
--- PASS: TestChmodDirsThuMucConBiTrao (0.00s)
```

```
go test -count=1 ./internal/server -run 'TestBanXemTruoc|TestMoiHandlerXemTruoc' -v
--- PASS: TestBanXemTruocLocTheoVai (0.01s)
--- PASS: TestBanXemTruocHaiLop (0.01s)
--- PASS: TestMoiHandlerXemTruocQuaWritePreview (0.01s)
```

`go test ./internal/system -run 'TestKhongGoiOSTrucTiep|TestLenhRootKhongNhanDuongDanSite'` trả `ok` (mình chạy không `-v`).

### 2.6 Chưa có trong tay

- FUSE: thiết kế ghi "chưa thử mount thật" (`M28.md` mục 17).

## 3. Đi qua mã theo thứ tự chạy

Đường đi của một lần xin bản xem trước: `POST /api/v1/sites?dry_run=1` đến lúc JSON trả về. Từng chặng: file, hàm, làm gì, vì sao.

### 3.1 Sơ đồ chữ của lỗ T1 trên bản chưa vá

```
K1 (PHP site e, user tp_e_...)     K2 (khach1, vai user)            panel tpan (root)
 ln -s /etc/shadow
     public/.htaccess
                                    POST /sites?dry_run=1 ---------> dựng Plan tạo site
                                                                     bước SiteHtaccess khai
                                                                       Files: public/.htaccess
                                                                     engine.Preview:
                                                                       os.ReadFile(.htaccess)
                                                                       (root, đi theo symlink)
                                                                     kernel mở /etc/shadow
                                    <------------------------------- JSON: steps[].info.files[].old
                                                                       = nội dung /etc/shadow
```

Ba điều kiện cùng đúng thì lỗ mới mở: (1) đường dẫn do site kiểm soát nằm trong `StepInfo.Files`; (2) panel đọc nó bằng quyền root và đi theo symlink; (3) API trả kết quả cho vai `user` mà không lọc. K1 và K2 có thể là một người: khách thuê hosting có PHP trên site và một tài khoản panel.

Vì sao chín đường L1 đến L9 của M14 không chặn nó: M14 chặn những gì *site* làm với file của site khác bằng quyền của chính user site (OPcache dùng chung, `.htaccess` tự nới `open_basedir`, symlink qua web server, đọc log, `/proc`, vhost trùng tên). Ở T1 site không tự đọc gì cả. Nó đặt một cái lối tắt trong thư mục của mình rồi nhờ một tiến trình mạnh hơn (panel, root) đi theo. Quyền của kernel chặn user site, nhưng không chặn root. Chín cánh cửa đóng kín, còn người gác cổng (panel) thì tự mở cửa phụ khi được nhờ.

### 3.2 Chặng 1: `handleSiteCreate` (internal/server/api_job.go)

```go
owner, ok := s.siteCreateOwner(w, r, d, body.Owner)
...
if apiDryRun(r) {
	add, err := sites.PrepareAdd(r.Context(), in)
	...
	p, err := add.Preview(r.Context())
	...
	// d đã qua siteCreateOwner: site mới (hay site của chính người gọi).
	s.writePreview(w, r, p, d)
	return
}
```

Hàm kiểm quyền (`siteCreateOwner`), dựng Plan (`PrepareAdd`), xem trước (`Preview`), rồi trả. Trước M28 dòng cuối là `writeJSON`: trả thẳng. Giờ là `writePreview`, chặng cuối (3.9). Nhớ: `d` được truyền vào để bộ lọc biết site đang được tạo là của người gọi.

### 3.3 Chặng 2: Plan có bước `.htaccess` (internal/core/site.go, internal/steps/site_web.go)

`addPlan` thêm bước `.htaccess` khi stack là Apache:

```go
if web == Apache && mode != site.HtaccessOff {
	ht, err := steps.SiteHtaccess(l)
	...
	ss = append(ss, ht)
}
```

`SiteHtaccess` trả một `File` có `Path: l.Public + "/.htaccess"`. `File.Info()` khai `Files: [{Path: ...}]`. Mọi bước khai file nó sẽ đụng vào để engine đọc nội dung cũ mà vẽ so sánh. Đây là chỗ duy nhất trong mã đưa một đường dẫn *trong vùng site* vào `StepInfo.Files` (bản thiết kế mục 3.9). Nginx không đưa `.htaccess` vào Plan, nên nginx không dính T1.

### 3.4 Chặng 3: `engine.Preview` và `fillOld` (internal/engine/engine.go)

`Preview` gọi `fillOld` cho từng file khai trong `Files`. Hàm `fillOld` (dòng 92 trở đi) đã sửa:

```go
if sitefs.InZone(f.Path) {
	fi, err := e.sys.Stat(f.Path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		f.Old, f.OldExists = nil, false
		return nil
	case err != nil:
		return fmt.Errorf("kiểm %s: %w", f.Path, err)
	case fi.Mode&fs.ModeSymlink != 0:
		target, err := e.sys.Readlink(f.Path)
		...
		f.Old, f.OldLink, f.OldExists = nil, target, true
		return nil
	case !fi.Mode.IsRegular():
		return &sitefs.Error{Op: "xem trước", Path: f.Path, Err: sitefs.ErrNotRegular}
	}
}
old, err := e.sys.ReadFile(f.Path)
```

Đọc thế này: nếu đường dẫn nằm trong vùng site, hỏi `Stat` trước (một lệnh "lstat" qua cửa, không đi theo symlink). Không có thì "chưa có file". Là symlink thì chỉ ghi *đích* của link vào trường mới `OldLink` và không đọc đích. Là thứ lạ (thư mục, FIFO) thì từ chối. Chỉ file thường mới rơi xuống dòng `ReadFile`. Vì sao ghi `OldLink` thay vì báo lỗi với symlink: lúc chạy thật, bước `File` thay symlink bằng file thường, nên bản xem trước nói đúng điều sẽ xảy ra.

Dòng `e.sys.ReadFile` này vẫn không đáng tin theo đường dẫn: giữa `Stat` và `ReadFile` kẻ tấn công có thể tráo file. Chỗ đó được cửa lo (3.6): cửa kiểm lại trên đúng file đang mở.

Lưu ý: `engine` không gọi `os.ReadFile` nữa. Nó gọi `e.sys`, tức `system.System`. Đó là cách "một cửa" hoạt động: mọi chỗ đều đi qua cùng một giao diện.

### 3.5 Chặng 4: chỗ rẽ vào cửa trong `LocalSystem` (internal/system/local.go)

```go
func (s *LocalSystem) ReadFile(path string) ([]byte, error) {
	if a, r, ok := sitefs.Split(s.SiteRoot, path); ok {
		return sitefs.ReadFile(a, r, sitefs.MaxFile)
	}
	b, err := os.ReadFile(path)
	...
```

`sitefs.Split(root, p)` (internal/sitefs/zone.go) cắt `/var/www/e.example.com/public/.htaccess` thành neo `/var/www/e.example.com` và `rel` là `public/.htaccess`. "Neo" là chính thư mục `/var/www/<tên>`: tên này nằm trong `/var/www` (của root), user site không đổi tên nó được. Chính neo và mọi đường dẫn ngoài `/var/www/<tên>/...` không thuộc vùng, chạy mã cũ. Mười hai hàm file của `System` đều có đúng cùng chỗ rẽ này (`WriteFileAtomic`, `MkdirAll`, `Remove`, `Rename`, `Symlink`, `Readlink`, `Stat`, `ReadDir`, `Chown`, `Chmod`, `StatFS`). Vì chỗ rẽ nằm trong `System`, một Step viết sau này gọi `sys.ReadFile` cũng tự đi qua cửa: không quên được. Điều này đã được thử thật lúc gộp M18 vào: M18 thêm hai hàm đọc ghi theo dòng (`OpenRead`, `CreateFile`, mở theo đường dẫn, chỉ chặn symlink ở tên cuối), và test `TestMoiHamCoLuatVungSite` đỏ ngay vì bảng vùng site chưa có dòng cho chúng. Hai hàm đó giờ từ chối hẳn đường dẫn trong vùng site (`ErrZoneStream`); chúng chỉ dùng cho file sao lưu của tpan.

### 3.6 Chặng 5: cửa mở từng cấp (internal/sitefs/walk.go và ops.go)

Mở neo (`openAnchor`, dòng 71):

```go
rfd, err := openat(unix.AT_FDCWD, path.Dir(anchor), rootFlags, 0)
...
if err := unix.Fstat(rfd, &st); err != nil { ... }
if int(st.Uid) != os.Geteuid() || uint32(st.Mode)&0o022 != 0 {
	return -1, st, ErrRootUnsafe
}
fd, err := openDir(rfd, path.Base(anchor))
```

Trước hết cửa mở `/var/www` và kiểm nó thuộc tiến trình đang chạy (root) và nhóm, người khác không ghi được (`0o022` là hai bit ghi của nhóm và người khác). Nếu ai đó làm theo hướng dẫn trên mạng `chown -R www-data /var/www` thì một tiến trình `www-data` đổi tên được neo của site này thành tên của site kia, và mọi luật bên dưới mất nghĩa. Cửa từ chối với `ErrRootUnsafe` và chỉ cách sửa.

Rồi `walk` (dòng 97) đi từng thành phần:

```go
for _, c := range parts[:len(parts)-1] {
	next, err := openDir(fd, c)
	closeFD(fd)
	...
	fd = next
}
return parent{fd: fd, name: parts[len(parts)-1], anchor: ast}, nil
```

`openDir` gọi `openat(dirfd, name, O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC)` (hằng `dirFlags`, dòng 16). Mỗi bước mở thư mục con *tính từ fd thư mục cha đã mở*, và `O_NOFOLLOW` làm lệnh thất bại nếu thành phần đó là symlink. Hàm `classify` (dòng 150) chỉ để phân loại lỗi cho câu báo: kernel 6.8 trả `ENOTDIR` chứ không phải `ELOOP` khi `O_DIRECTORY|O_NOFOLLOW` gặp symlink, nên cửa nhìn thêm bằng `fstatat(AT_SYMLINK_NOFOLLOW)` để báo đúng "là symlink".

Đọc file (`readLeaf`, ops.go dòng 31):

```go
st, err := lstatAt(p.fd, p.name)                 // (a) nhìn trước, không mở
if err := leafKind(&st, false); err != nil { ... }
fd, err := openLeaf(p.fd, p.name)                // (b) mở O_NOFOLLOW|O_NONBLOCK
if err := checkRead(fd, &p.anchor, max); err != nil { ... } // (c) kiểm trên fd
return readLimit(fd, max)                        // (d) đọc có trần
```

(a) loại ngay symlink, FIFO, thiết bị mà không mở chúng. (b) vẫn phòng khi có ai tráo FIFO vào sau (a): `O_NONBLOCK` làm lần mở không chờ. (c) là phép kiểm có giá trị: `fstat` trên *chính fd đã mở*, nơi không ai tráo được nữa. `checkRead` đòi: file thường, `st.Uid` bằng uid của neo (chặn hardlink tới file của root), `st.Size <= max`. (d) `readLimit` không bao giờ đọc quá `max+1` byte, phòng file lớn dần sau `fstat`. Trần `MaxFile` là 1 MiB.

Vì sao viết vậy: mọi phép kiểm "đáng tin" nằm trên fd. Phép kiểm theo tên (a) chỉ để từ chối sớm, không ai dựa vào nó.

### 3.7 Chặng 6: lệnh root và `Run` giết cả nhóm (internal/system/local.go, rootcmd.go)

`LocalSystem.Run` đầu tiên gọi `checkRootZone`:

```go
if err := checkRootZone(s.SiteRoot, cmd); err != nil {
	return Result{ExitCode: -1}, err
}
```

Lệnh chạy bằng root có chứa chuỗi `/var/www/` trong tên, đối số, thư mục chạy hay biến môi trường thì bị từ chối (`ErrRootZoneCommand`), trừ danh sách trắng `rootZoneCommands` trong `rootcmd.go`: `tar` chỉ với `--create` và đúng các cờ tpan dùng, `du -sb --one-file-system`, `useradd`. Lệnh chạy bằng user của site (tên dạng `tp_...`) được miễn, vì kernel giới hạn nó trong quyền của user đó. Test `TestLenhRootKhongNhanDuongDanSite` giữ điều này, và luật cũng bật trong `FakeSystem` nên mọi test chạy Plan đều kiểm.

Cũng ở `Run` (dòng 64):

```go
c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
c.Cancel = func() error {
	err := syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
	...
```

Mỗi lệnh chạy trong một nhóm tiến trình riêng; hết hạn thì giết cả nhóm bằng `kill(-pgid)`. Lý do ở mục 4.6.

Lệnh `find ... -exec chmod` trong `WPFiles` được thay bằng hàm `chmodDirs` (internal/steps/site_wp.go dòng 408) duyệt cây bằng `sys.ReadDir`, `sys.Stat`, `sys.Chmod`, cả ba đi qua cửa:

```go
fi, err := sys.Stat(p)
if chmodVanished(err) { continue }
...
if !fi.Mode.IsDir() { continue } // file, symlink, thứ khác: không đụng
...
if err := sys.Chmod(p, perm); chmodVanished(err) { continue }
```

`Chmod` qua cửa dùng `fchmod` trên fd đã mở, không theo tên. Mục biến mất hay thành symlink giữa chừng thì bỏ qua (chỉ hại chính site). "Thành symlink giữa chừng" có ba dạng lỗi: `ErrSymlink` của cửa, và `ELOOP`, `ENOTDIR` nguyên của kernel khi tên là symlink đúng lúc cửa mở rồi bị tráo lại thành thư mục trước khi cửa kịp nhìn lại để phân loại. `chmodVanished` nhận cả ba (mục 5.5, ý 5).

### 3.8 Chặng 7: lớp thứ hai ở API, `filterPreview` (internal/server/preview_m28.go)

Sau khi `Preview` xong, `writePreview` gọi `filterPreview`:

```go
if who(r).IsAdmin() { return nil }
...
for _, d := range targets { layouts = append(layouts, site.LayoutOf(d)) }
```

Admin không bị lọc. Vai `user`: tập được phép là các site của người gọi (`Store.SitesOf`) cộng `targets` (domain handler đã phân quyền). Rồi `filterFile` cho từng file:

```go
switch {
case owner == nil:
	f.Old, f.New, f.OldLink = nil, nil, ""
	f.Hidden = hiddenForeign
case sitefs.InZone(f.Path) && len(f.Old) > 0:
	if !verifyAsSiteUser(ctx, s.deps.Sys, owner.User, f.Path, f.Old) {
		f.Old = nil
		f.Hidden = hiddenUnverified
	}
}
```

`owner` tìm bằng `Layout.Owns(path)` (internal/site/owns.go): path nằm trong thư mục site hoặc thư mục log của site, hoặc đúng file vhost, pool, cron của site. Không site nào làm chủ: xoá nội dung, ghi `hidden: "foreign"`. Có chủ và nằm trong vùng site: nội dung chỉ được giữ khi `verifyAsSiteUser` đúng.

`verifyAsSiteUser` (dòng 138):

```go
res, err := sys.Run(ctx, system.Cmd{
	Name: "timeout",
	Args: []string{"-k", "1", "2", "dd", "if=" + path, "iflag=nonblock,nofollow",
		"bs=" + strconv.FormatInt(sitefs.MaxFile+1, 10), "count=1", "status=none"},
	AsUser: user,
})
return err == nil && res.ExitCode == 0 && res.Stdout == string(old)
```

Một tiến trình chạy bằng *user Linux của site* đọc lại file, và nội dung phải trùng từng byte với `old`. Nếu `.htaccess` là symlink tới `/etc/shadow` mà lớp đọc ở dưới lỡ đi theo, user Linux của site vẫn không đọc nổi `/etc/shadow` (kernel chặn), nên không khớp và `old` bị xoá. Lớp này không dựa vào mã của tpan mà dựa vào quyền file của kernel. Hai lớp độc lập: hỏng cửa thì lớp này chặn; hỏng lớp này thì cửa chặn (test `TestBanXemTruocHaiLop`).

`nonblock` để FIFO không làm `dd` chờ, `nofollow` để symlink bị từ chối ở tên cuối, `timeout` đứng bên trong `runuser` để lệnh con chết theo hạn kể cả khi `runuser` bị giết.

### 3.9 Chặng 8: lỗi của cửa thành HTTP 409

`sitePathUnsafe` (cùng file, dòng 158) đổi lỗi của cửa thành HTTP 409 mã `site_path_unsafe`, câu song ngữ nêu đường dẫn, lý do và lệnh `ls -la <path>` để xem. Câu không bao giờ chứa nội dung file hay đích của symlink (bất biến B14, test `TestLoiCuaKhongLoNoiDung`). Trước đây những lỗi này ra 500.

### 3.10 Ba câu để nhớ (bản thiết kế mục 5), kèm ví dụ trong bằng chứng

1. **Tên trong thư mục của site không nói gì về thứ nó trỏ tới. Chỉ tin thứ đã mở: mở không theo symlink, rồi hỏi chính fd đó.** Ví dụ: ở n2, `.htaccess` là file thường 2 GiB đúng chủ, đúng quyền. Bản `lstat` thấy "file thường", vẫn `ĐỌC KHÔNG TRẦN` (dòng 75). Bản `walk` mở xong rồi hỏi fd thì `fstat: 2147483648 byte, quá trần 1048576` (dòng 78).
2. **Kiểm theo đường dẫn rồi làm theo đường dẫn là hai lần đi đường; người giữ thư mục đổi được đường giữa hai lần.** Làm trên fd, hoặc để kernel giới hạn bằng cách chạy với quyền của user site. Ví dụ: "lstat rồi đọc" vẫn lộ 95 lần (tráo file) và 9966 lần (tráo thư mục cha) trong 20000 lần đọc (n2 dòng 91 và 99). Ví dụ thứ hai: `find -exec chmod` chạy bằng root thua sau 5 vòng (n3 dòng 39). Lớp "user của site đọc lại" ở 3.8 là vế "để kernel giới hạn".
3. **Thứ root đọc được trong thư mục site là dữ liệu của site: có cỡ tuỳ ý, có thể không bao giờ kết thúc, và có thể là một câu lệnh cài cho AI.** Ví dụ: `/dev/zero` và file thưa 2 GiB làm panel bị kernel giết (n1 dòng 62 tới 64). Về AI: nội dung do site viết vào ngữ cảnh AI (`tools.managedFile`, `textPreview`) được bọc `ai.Untrusted(...)` để AI không coi nó là chỉ thị.

## 4. Kiến thức nền

Mỗi khái niệm: ý chính, một chỗ trong mã, một lệnh để tự nhìn trên máy ảo. Các lệnh dưới đây mình chưa chạy trong phiên viết file này; phần "sẽ thấy" dựa trên man page, bằng chứng ở mục 2 và mục 9 của bản thiết kế.

### 4.1 Symlink

Symlink (liên kết mềm) là một file nhỏ mà nội dung là *một đường dẫn*. Mở nó thì kernel đi theo đường dẫn đó, trừ khi bạn yêu cầu đừng đi theo. User của site tạo symlink trong thư mục của mình mà không cần quyền gì thêm, trỏ tới bất kỳ đâu, kể cả nơi nó không đọc được. Cái nguy hiểm: quyền kiểm theo người *mở*. Mở bằng root thì root đọc được đích.

Chỗ trong mã: `engine.fillOld` ở 3.4, nhánh `fi.Mode&fs.ModeSymlink != 0`.

Tự nhìn (máy ảo):

```bash
ln -s /etc/hostname /tmp/loi-tat && ls -l /tmp/loi-tat && cat /tmp/loi-tat
```

Sẽ thấy `ls -l` in `lrwxrwxrwx ... /tmp/loi-tat -> /etc/hostname` (chữ `l` đầu dòng là symlink) và `cat` in nội dung của `/etc/hostname`.

### 4.2 Hardlink

Hardlink là *tên thứ hai* của cùng một file (cùng inode). Không có "đích" để từ chối: sau khi tạo, hai tên bình đẳng. Vì vậy mọi cách mở file đều coi nó là file thường (n2 dòng 88). Ubuntu bật `fs.protected_hardlinks=1`, kernel chỉ cho bạn hardlink file bạn làm chủ (hoặc đọc ghi được), nên user site không hardlink được `/etc/shadow`. Bản vá không dựa vào cấu hình đó: cửa còn đòi chủ file bằng chủ của neo (`checkRead`, dòng `anchor.Uid != 0 && st.Uid != anchor.Uid`), và đòi `Nlink == 1` khi đổi chủ hay quyền (`ErrLinked`).

Tự nhìn:

```bash
cd /tmp && echo x > a && ln a b && ls -li a b && sysctl fs.protected_hardlinks
```

Sẽ thấy hai dòng cùng số inode ở cột đầu, cột đếm liên kết là 2, và `fs.protected_hardlinks = 1`.

### 4.3 FIFO (named pipe)

FIFO là file đặc biệt làm "ống": mở để đọc sẽ *chờ* tới khi có người mở để ghi. `os.ReadFile` trên FIFO không có người ghi thì treo vĩnh viễn (n1: request quá 20 giây; n2 dòng 45: `TREO (quá 3s)`). Cửa tránh bằng hai việc: không mở khi `lstat` đã thấy là FIFO, và mở với `O_NONBLOCK` để cả khi bị tráo vào sau cũng không chờ.

Chỗ trong mã: hằng `leafFlags` ở `internal/sitefs/walk.go` dòng 17 và `readLeaf` ở 3.6.

Tự nhìn:

```bash
mkfifo /tmp/ong && timeout 3 cat /tmp/ong; echo "mã thoát: $?"
dd if=/tmp/ong iflag=nonblock bs=1 count=1 status=none; echo "mã thoát: $?"
```

Sẽ thấy `cat` bị `timeout` giết sau 3 giây (mã thoát 124), còn `dd` với `nonblock` về ngay (thiết kế mục 9 ghi đã thử: 4 ms, mã 0, không byte nào).

### 4.4 `O_NOFOLLOW`, `openat` và fd

Mở file trong C là gọi `open(đường dẫn, cờ)` và nhận về một số nguyên gọi là *file descriptor* (fd): một "tay cầm" tới thứ đã mở. Sau khi có fd, các lệnh `fstat`, `read`, `fchmod` làm trên chính thứ đó, bất kể tên của nó sau này bị đổi thế nào. `openat(dirfd, tên, cờ)` mở một tên *tính từ một thư mục đã mở* (dirfd) thay vì từ gốc `/`. Cờ `O_NOFOLLOW` bảo kernel: nếu thành phần cuối là symlink thì báo lỗi, đừng đi theo. Ghép lại: đi từ neo, mở từng cấp bằng `openat(..., O_NOFOLLOW)` thì ở mọi cấp, nếu có symlink, kernel báo lỗi thay vì dẫn root đi chỗ khác.

Chỗ trong mã: `openDir`, `openLeaf` trong `internal/sitefs/walk.go`, và `walk` ở 3.6.

Tự nhìn (cần `strace`; chưa có thì `sudo apt install strace`):

```bash
mkdir -p /tmp/cay/con && echo x > /tmp/cay/con/f && strace -f -e trace=openat,unlinkat rm -r /tmp/cay 2>&1 | grep -E 'openat|unlinkat' | tail -6
```

Sẽ thấy `rm -r` mở từng cấp bằng `openat(<số fd>, "con", ... O_NOFOLLOW ...)` và xoá bằng `unlinkat(<số fd>, ...)`. Đây đúng là kiểu `rm` mà bằng chứng n3 (dòng 22 tới 29) đo trên cây site. Xem fd của một tiến trình: `ls -l /proc/$$/fd`.

### 4.5 TOCTOU (kiểm rồi mới làm)

TOCTOU là *time of check, time of use*: bạn kiểm điều kiện ở thời điểm 1 rồi hành động ở thời điểm 2, và giữa hai thời điểm người khác đổi tình hình. Ở đây: panel `lstat` thấy "file thường", rồi `open` theo tên; giữa hai lệnh, user của site `rename` một symlink đè lên tên đó. Root mở nhầm. Chữa: bỏ lần kiểm theo tên, kiểm trên fd đã mở.

Chỗ trong mã: bước (c) của `readLeaf` ở 3.6; test `TestCuaDua` (`internal/sitefs/gate_test.go` dòng 332) cho một goroutine tráo liên tục trong lúc đọc 1000 lần.

Tự nhìn (Mac hoặc máy ảo có Go, trong thư mục repo):

```bash
go test -count=1 ./internal/sitefs -run 'TestCuaDua$' -v
```

Sẽ thấy hai dòng dạng `đọc được N, từ chối M` (N và M đổi mỗi lần, tổng 1000) và `--- PASS: TestCuaDua`. Trong 1000 lần không lần nào đọc ra nội dung ngoài site.

### 4.6 Nhóm tiến trình và `runuser`

`runuser -u <user> -- lệnh` chạy `lệnh` dưới user khác. Nó không thay chính nó bằng `lệnh`, mà *fork* ra lệnh thật rồi đứng chờ (vì mở phiên PAM). Hệ quả đã thử trên máy ảo (thiết kế mục 9, trả lời S1): giết `runuser` thì lệnh con vẫn sống, mồ côi, ở lại trong cgroup của `tpan.service` và giữ đầu ghi của ống output, nên `Wait` của Go chờ thêm. Chữa: mỗi lệnh một *nhóm tiến trình* (mọi tiến trình con cháu cùng số `pgid`), và khi huỷ thì `kill(-pgid, SIGKILL)` giết cả nhóm. Dấu trừ trước pid nghĩa là "cả nhóm".

Chỗ trong mã: `LocalSystem.Run` ở 3.7 (`Setpgid: true`, `syscall.Kill(-c.Process.Pid, ...)`); test cần root `TestLenhRootHuyKhongDeLaiTienTrinh`.

Tự nhìn:

```bash
sudo runuser -u nobody -- id; sleep 300 & ps -o pid,pgid,comm -p $!
```

Sẽ thấy `uid=65534(nobody) ...` và một dòng `ps` có cột `PGID`. Giết cả nhóm: `kill -- -<số PGID>`.

## 5. Cách khác đã cân nhắc, và chỗ đã làm sai

### 5.1 Chỉ bỏ `.htaccess` khỏi `StepInfo.Files`

Cách rẻ nhất: bản xem trước thôi đọc `.htaccess`. Không chọn. Đó là vá triệu chứng. Mã v0.2.0 còn nhiều chỗ khác đọc, ghi, đổi chủ, đổi quyền theo đường dẫn trong thư mục site (T1b, T1c, T3, T8, T8b trong bảng 3.9 của thiết kế): `WriteFileAtomic` ghi sang thư mục đích khi `public` bị tráo, `os.Chmod` đi theo symlink, `find -exec chmod` bằng root thua đua. Bỏ một dòng đóng một chỗ và để cả họ lỗi lại. Cũng cần: một Step viết sau này khai lại đường dẫn site thì lỗ mở lại.

### 5.2 `os.Root` của Go

Gói chuẩn có sẵn, chặn thoát ra ngoài. Đo ở n2: 0 lần lộ trong 20000 lần đua. Không chọn vì nó chặn *thoát ra ngoài* nhưng vẫn *đi theo symlink trỏ vào trong site* (n2 dòng 33: `osroot đọc được 9 byte`), trái bất biến "không đi theo symlink ở bất kỳ thành phần nào". Tài liệu `go doc os.Root` của Go 1.27 còn ghi `Chmod`, `Chown` của nó có đua và nó không chặn file thiết bị.

### 5.3 `openat2` với `RESOLVE_BENEATH|RESOLVE_NO_SYMLINKS`

Một lời gọi kernel làm đúng việc, kết quả đo giống `walk`. Không chọn vì chỉ có trên Linux từ 5.6, mà unit test của repo chạy trên Mac: dùng nó thì mã chạy thật và mã được test là hai nhánh khác nhau. `walk` chậm hơn khoảng 0,6 µs mỗi lần đọc (n2 dòng 108 và 109: 2665 ns so với 3250 ns), không đáng kể.

### 5.4 "lstat rồi đọc"

Cách của v0.2.0. Thua khi thư mục cha bị tráo, thua đua, không có trần (mục 3.10, câu 2).

### 5.5 Chỗ đã làm sai trong mốc này

Những chỗ người review hay hỏi nhất. Mỗi cái: sai gì, phát hiện bằng cách nào.

1. **Lớp đọc lại ban đầu dùng `head`.** Bản thiết kế đầu viết lớp thứ hai bằng `head -c ... -- <path>` dưới user của site. Thợ soát thiết kế chỉ ra (S1, mức nặng): nếu đối phương tráo một FIFO vào giữa lần đọc của cửa và lần đọc lại, `head` chặn ở `open`; hết hạn, Go chỉ giết `runuser`, `head` mồ côi nằm lại, và mỗi bản xem trước để lại một tiến trình, cộng dồn tới khi tpan không fork được lệnh nào. Phiên chính thử trên máy ảo trước khi sửa: giết `runuser` thì `sleep` con vẫn sống cùng nhóm tiến trình. Sửa ba lớp: `dd iflag=nonblock,nofollow` (không chờ), `timeout` bên trong `runuser`, và `Run` giết cả nhóm. `ScanHtaccess` của M14 có cùng cơ chế nên đổi theo.
2. **Một câu sai trong bảng N3.** Bản thiết kế đầu ghi rằng "chủ site gọi lại được bước `find -exec chmod` qua `POST /sites`". Sai: `WPFiles.Apply` từ chối `public` không rỗng, còn `site add` chạy lại khi thiếu `wp-config.php` thì bị từ chối `site_drift` (`M28.md` mục 13). Bước đó chỉ chạy lúc tạo site lần đầu, hoặc khi `public` bị xoá sạch đúng giữa kiểm `drift()` và bước `wp.files`. Vẫn thay bằng `chmodDirs` vì rẻ và cùng họ lỗi.
3. **e2e A7 bản đầu không chạy tới bước cần thử.** Thợ soát thiết kế (S8) chỉ ra: viết như chữ thì 50 vòng không vòng nào tới `chmodDirs`, nạn nhân "giữ nguyên quyền" và test xanh mà không kiểm gì. Sửa: tạo site mới mỗi vòng, một vòng lặp root chờ `public` có mục đầu tiên (tar đã bắt đầu giải nén) rồi mới khởi động kẻ tráo; mỗi vòng đòi thấy sự kiện `step.done` của `wp.files:<domain>`, nên khẳng định không thể xanh khi bước cần thử chưa chạy. Lần sửa đầu cho kẻ tráo đổi chính `wp-includes/js`: tar hỏng giữa chừng, job hoàn tác, vẫn không tới bước đặt quyền. Khẳng định mới bắt được điều đó ngay: `docs/bang-chung/M28-truoc-ban-va-2.log` dòng 261 tới 300 đỏ vì `0 sự kiện step.done của wp.files:f.example.com`. Bản cuối cho kẻ tráo dùng thư mục riêng `public/zz-m28/con` mà tar không đụng tới.
4. **Luật lệnh root miễn theo tên chương trình.** Bản đầu miễn `tar`, `du` nói chung khi đường dẫn là một neo hoặc `<neo>/<một thành phần>`. Thợ soát cuối (C5, chưa từng đọc mã) chỉ ra 15 dạng lọt qua: các cờ *nhận một file* mà chương trình mở theo đường dẫn, đi theo symlink ở tên cuối, như `tar --file=`, `-T`, `-I`, `-F`, `du --files0-from`, `du -L`, `useradd -P`. Hôm nay tpan không dùng dạng nào nên chưa có lỗ thật, nhưng lời hứa "mã mới thêm lệnh root thì test đỏ" không giữ. Sửa: danh sách trắng theo từng cờ (`tarCreateOnly` và hàm tương tự trong `rootcmd.go`), mọi cờ khác bị từ chối khi lệnh chạm vùng site. Commit `519959e`.
5. **`chmodDirs` coi một kiểu "mục bị tráo" là lỗi của cả job.** e2e A7 trên nginx làm job tạo site hỏng ở vòng 4, hai lần chạy liền. An toàn vẫn giữ (thư mục nạn nhân ngoài site giữ `0755` ở mọi vòng), chỉ job hỏng. Đoán đầu tiên của phiên chính là lúc liệt kê thư mục con, và sửa chỗ đó (commit `e1d7531`); lần chạy sau, script đã in lỗi gốc lấy từ JSON và cho thấy đoán sai: chính `Chmod` trả `too many levels of symbolic links` (`ELOOP`). Tên là symlink đúng lúc cửa mở nó, rồi bị tráo lại thành thư mục trước khi cửa `lstat` để phân loại, nên cửa trả nguyên lỗi của kernel thay cho `ErrSymlink`. Sửa ở hàm dùng chung `chmodVanished` (nhận cả `ENOTDIR`, `ELOOP`, dùng cho `Stat`, `Chmod` và lúc liệt kê), commit `841bff3`, test `TestChmodDirsLoi` (có đúng lỗi `ELOOP` đó) và `TestChmodDirsThuMucConBiTrao`. Bài học: đọc lỗi gốc trước khi sửa, đừng sửa theo đoán.
6. **Chỗ nhỏ hơn đã sửa theo thợ soát:** mất bit setgid (S3: `fs.ModeSetgid` của Go là bit 22, không phải `0o2000` của kernel; làm sai thì `public` ra `0750` thay vì `2750` mà mọi test trên máy giả vẫn xanh), symlink do root tạo trong vùng thuộc về root nên qua được `SymLinksIfOwnerMatch` (S14), `/var/www` có thể bị `chown` sai (S15), `Symlink` đổi chủ cái link theo tên lần hai (C8, sửa qua fd `O_PATH`), CLI chưa lọc ký tự điều khiển (C6), khôi phục thùng rác `stat` qua `public` (C7). Pha 2 của soát cuối (thử phá trên máy ảo) còn tìm ra hai chỗ ngay trong các bản sửa trên, đều đã sửa kèm test: luật `--file=` của C5 chỉ chặn giá trị có chứa `/var/www/`, nên đường dẫn tương đối trỏ vào vùng site vẫn lọt (C12, giờ đòi tuyệt đối, sạch, ngoài vùng); và backup, khôi phục khi `public` là symlink trả 500 thay cho 409 (C14).

## 6. Còn hở

Những điều mốc này *không* sửa, nói bằng lời thường.

1. **Họ lỗi thứ hai, để đợt 2 (C1 đến C4, C9 của thợ soát cuối).** Lỗ T1 là panel *mở nhầm file* theo đường dẫn do site vẽ. Họ thứ hai khác: panel *tin cỡ và thời gian* của dữ liệu đi qua một lệnh chạy trên thứ site điều khiển, nên một site bị chiếm có thể làm panel chậm, giữ khoá hay hết bộ nhớ. Chữa tận gốc là đặt trần output và hạn giờ ngay trong `Run` cho mọi lệnh, đổi hành vi của nhiều luồng (giải nén backup lớn, `nginx -T`, xuất log), nên cần thiết kế và soát riêng. Không làm trong đợt 1 vì: không phải "đọc, ghi theo đường dẫn của site" như phạm vi đã chốt; có từ v0.1, bản vá này không làm nó tệ hơn; và sửa vội ở `Run` dễ làm hỏng chức năng khác. Lý do đầy đủ ở mục 20 của `docs/thay-doi/M28.md`. Thợ soát cuối đánh giá C1 đến C3 là nặng; pha 2 xác nhận C3 và C9 trên máy ảo. Repo công khai, nên cách gây ra và bằng chứng của các lỗ chưa vá không nằm trong repo (mục 24 của `docs/thay-doi/M28.md`).
2. **Giao diện web chưa hiện hai trường mới `hidden` và `old_link`.** Luật 4 của `agent-dev/LO-TRINH.md` cấm sửa `web/` trước khi M16 vào `dev`. Hậu quả: với vai `user`, màn duyệt hiện file của máy bị ẩn là "sửa" kèm "Nội dung không đổi", và `.htaccess` là symlink hiện cả file là dòng thêm, không nhắc tới symlink. JSON và CLI đúng; chỉ màn web chưa. Việc để mục "Để sau" của KE-HOACH, làm sau khi M16 vào `dev`.
3. **Cửa chỉ chặn được thứ đi qua `system.System`.** Ai đó gọi thẳng `os.ReadFile("/var/www/...")` trong mã mới thì cửa không biết. Hàng rào giữ cho điều này đúng về sau là test quét mã `TestKhongGoiOSTrucTiep` (`internal/system/noos_test.go`), dùng `go/ast` cấm gọi hàm file của `os` ở ngoài hai gói `system` và `sitefs`, trừ vài ngoại lệ có lý do (`store` mở `tpan.db`, `ai/bridge` mở socket, `cmd/tpan` đọc file người gõ lệnh đưa). Test này là hàng rào, không phải bằng chứng toán học.
4. **Ba phát hiện mới của pha 2, để đợt 2.** C10 và C13: từ chối phục vụ do một site gây ra, ở web server và ở backup, có từ trước M28, không phải panel đọc theo đường dẫn. C11: bản sửa C7 vẫn còn một khoảng đua lộ một bit thông tin mỗi lần khôi phục từ thùng rác. Chi tiết giữ ngoài repo tới khi vá.
5. Danh sách đầy đủ còn có: `tar` bằng root đọc hardlink khi máy tắt `fs.protected_hardlinks`; tiến trình của user site còn sống giữ fd vào cây đã vào thùng rác; Plan gộp nhiều site bị từ chối cả khi một site có `public` là symlink; FUSE chưa thử mount thật. Xem mục 5 của `docs/thay-doi/M28-thiet-ke.md`.

## 7. Tự thử lại

Tối đa 6 lệnh. Máy ảo là `tpan-m28` (hay tên trong file `.tpan-vm`), đã dựng bằng bước "dựng máy" của e2e (`scripts/e2e-m28/dung-may.sh apache`): có stack Apache, site `e.example.com` của `khach1`. Lệnh 2, 3, 4 đã chạy thật trên máy ảo `tpan-m28` lúc 13:03 ngày 21/11/2026 (bản `15a9923`, ngay sau e2e); output dán dưới mỗi lệnh là output thật. Đừng chạy lúc `make e2e-m28` đang dùng máy ảo.

1. **Mac**, trong thư mục repo: chép bản mới vào máy ảo.

   ```bash
   make vm-deploy
   ```

   Sẽ thấy build xong và `tpan` được chép vào máy ảo.

2. **Máy ảo, quyền root** (`scripts/vm.sh root bash` từ Mac để vào): tạo một file bí mật giả, rồi để user Linux của site biến `.htaccess` thành symlink tới nó.

   ```bash
   U=$(stat -c %U /var/www/e.example.com); echo "NOI-DUNG-BI-MAT-12345" > /root/bi-mat.txt; chmod 600 /root/bi-mat.txt
   runuser -u "$U" -- ln -sfn /root/bi-mat.txt /var/www/e.example.com/public/.htaccess; ls -l /var/www/e.example.com/public/.htaccess
   ```

   Sẽ thấy một symlink:

   ```
   lrwxrwxrwx 1 tp_e_example_com_25cfc7 www-data 16 Nov 21 13:03 /var/www/e.example.com/public/.htaccess -> /root/bi-mat.txt
   ```

3. **Máy ảo, root**: xin bản xem trước, xem dòng của `.htaccess`, rồi đếm số lần nội dung bí mật xuất hiện.

   ```bash
   tpan site add e.example.com --email admin@example.com --dry-run > /tmp/xem.txt 2>&1; grep -n 'symlink' /tmp/xem.txt; grep -c 'NOI-DUNG-BI-MAT-12345' /tmp/xem.txt
   ```

   Sẽ thấy (dòng 21 của bản xem trước, rồi số lần chuỗi bí mật xuất hiện):

   ```
   21:    thay symlink (→ "/root/bi-mat.txt") bằng file thường: /var/www/e.example.com/public/.htaccess
   0
   ```

   Bản xem trước có 1 bước sẽ chạy (ghi lại `.htaccess` thành file thường) và 18 bước đã đúng. Trên v0.2.0 cùng thao tác đó lộ nội dung (mục 2.1).

4. **Máy ảo, root**: thay symlink bằng FIFO và đo thời gian.

   ```bash
   U=$(stat -c %U /var/www/e.example.com); H=/var/www/e.example.com/public/.htaccess
   runuser -u "$U" -- rm -f "$H"; runuser -u "$U" -- mkfifo "$H"
   time tpan site add e.example.com --email admin@example.com --dry-run 2>&1 | tail -3; pgrep -u "$U" 'dd|head' | wc -l
   ```

   Sẽ thấy lời từ chối, lệnh về ngay, không còn tiến trình `dd` hay `head` nào:

   ```
   tpan: xem trước bước file:/var/www/e.example.com/public/.htaccess: xem trước /var/www/e.example.com/public/.htaccess: không phải file thường: tpan không mở thư mục, thiết bị, FIFO hay socket ở chỗ cần file thường

   real	0m0.206s
   0
   ```

   Trên v0.2.0 cùng thao tác đó làm request treo (mục 2.1).

5. **Mac hoặc máy có Go**, trong thư mục repo: cuộc đua tráo file.

   ```bash
   go test -count=1 ./internal/sitefs -run 'TestCuaDua$' -v
   ```

   Sẽ thấy hai dòng `đọc được N, từ chối M` và `--- PASS: TestCuaDua` (mục 2.5).

6. **Mac**: test cần root của cả ba gói, trong máy ảo.

   ```bash
   make m28-test-vm
   ```

   Sẽ thấy `PASS` ở ba gói (`sitefs`, `system`, `server`) và các test như `TestCuaDuaChmod`, `TestVungSiteDocLaiBangUserSite`, không dòng `SKIP` nào (như phần C của `docs/bang-chung/M28.log`, mục 2.4).

Dọn: `make vm-reset` đưa máy ảo về sạch.

## 8. Câu hay bị hỏi

Đáp án ở cuối file. Câu 3 và 9 cần chạy lệnh trên máy ảo mới trả lời chắc được.

1. Sao không chỉ bỏ dòng `.htaccess` khỏi bản xem trước?
2. Nginx có dính T1 không? Vì sao?
3. Hardlink thì sao? Trên máy của bạn kernel chặn hay cửa chặn?
4. Kẻ tấn công tráo đúng lúc giữa kiểm và đọc thì sao?
5. Nâng từ v0.2.0 lên có đổi gì ở site đang chạy?
6. Giao diện web có hiện đúng không?
7. M14 đã chặn chín đường, sao đường này lọt?
8. Đã có cửa rồi, sao còn lớp "user của site đọc lại" ở API?
9. Vì sao lớp đọc lại dùng `dd iflag=nonblock,nofollow` chứ không dùng `head`?
10. Vì sao cửa kiểm `/var/www` thuộc root và không ai khác ghi được? Máy bạn có đúng không?

## Đáp án

1. Bỏ dòng đó đóng một chỗ và để nguyên họ lỗi. Cùng loại "panel root đụng đường dẫn site vẽ" còn ở `WriteFileAtomic` (ghi sang thư mục khác khi `public` bị tráo), `os.Chmod` (đi theo symlink), `find -exec chmod` chạy bằng root (thua đua sau 5 vòng, n3 dòng 39), `Chown` theo đường dẫn, và công cụ `managed_file` đọc `.htaccess` trong sổ file. Một Step viết sau này khai lại đường dẫn site thì lỗ mở lại. Cửa ở `system.System` đóng cả họ và không Step nào quên được. Cũng đã đo: cách "lstat rồi đọc" vẫn lộ 95 và 9966 lần trong 20000 lần (n2).

2. Không dính theo đường này. Trên nginx `.htaccess` không nằm trong Plan, nên không có `FileChange` nào trỏ vào vùng site để đọc (`addPlan` chỉ thêm `SiteHtaccess` khi `web == Apache`). Bằng chứng: bộ e2e nginx trên bản chưa vá `docs/bang-chung/M28-truoc-ban-va-nginx.log` có dòng cuối `TỔNG KẾT: 29 khẳng định đạt, 0 khẳng định không đạt`. Nhưng cửa vẫn áp cho nginx, vì cùng họ lỗi (ví dụ `chmodDirs`, `WriteFileAtomic`, đường `wp-config.php`) có ở cả hai stack.

3. Hai lớp. Kernel Ubuntu mặc định `fs.protected_hardlinks=1` chặn user site hardlink file không phải của nó (n1 dòng 39 và 40: `ln ... Operation not permitted`). Máy cấu hình lỏng (tắt sysctl): hardlink là file thường với mọi cách mở (n2 dòng 88), nên cửa kiểm *chủ của file* bằng `fstat` trên fd (`checkRead`: `st.Uid` phải bằng uid của neo, không thì `ErrOwner`) và đổi chủ, đổi quyền đòi `Nlink == 1` (`ErrLinked`). Tự kiểm trên máy ảo: `sysctl fs.protected_hardlinks` phải ra `= 1`; đó là lớp kernel. Lớp cửa chỉ lộ ra khi tắt sysctl, mà đừng tắt nếu không cần.

4. Cửa không tin lần kiểm theo tên. Nó mở từng cấp bằng `openat(..., O_NOFOLLOW)` từ fd thư mục cha đã mở, rồi `fstat` trên chính fd của file (`readLeaf`, bước c). Tráo sau khi đã mở không đổi được thứ fd trỏ vào. Tráo trước khi mở thì `O_NOFOLLOW` từ chối. Đo: n2, 0 lần lộ trong 20000 lần đua cho `walk`; test `TestCuaDua` (1000 vòng) và `TestCuaDuaGhi` cho mọi thao tác ghi. e2e A4 trên bản cuối (`docs/bang-chung/M28.log` dòng 168 và 180): tráo `.htaccess` qua lại với symlink tới `/etc/shadow` hơn 3,2 triệu lần trong lúc gọi bản xem trước 1000 lần, `{'200': 892, '409': 108}; 1000 câu trả lời, 0 câu trả lời chứa file đích`; tráo `public` với symlink tới thư mục nạn nhân 2,6 triệu lần trong 300 lần gọi, `{'200': 37, '409': 263}`, cũng 0 câu chứa file đích. Mã 409 là lúc cửa bắt gặp symlink đúng lúc đọc và từ chối (`site_path_unsafe`); 200 là lúc gặp file thường hay ghi được `old_link`. PID của tpan không đổi.

5. Không migration, không đổi schema, không đổi file nào trên đĩa, không thao tác nào tự chạy lúc nâng (bản thiết kế mục 6). Site đang chạy không đổi gì. Khác chỉ ở máy ở trạng thái lạ: (a) file trong site không thuộc user của site (ví dụ admin `sudo sed -i` làm nó thuộc root) thì tpan từ chối đọc và chỉ lệnh `chown`; (b) `.htaccess` lớn hơn 1 MiB bị từ chối; (c) neo `/var/www/<tên>` là symlink (admin dời site sang đĩa khác bằng `ln -s`) không còn được đi theo, phải thay bằng `mount --bind`; (d) `/var/www` không thuộc root hoặc người khác ghi được thì mọi thao tác trong vùng trả `site_path_unsafe`. Lùi về v0.2.0 chạy được vì DB không đổi, nhưng lùi là mở lại T1. Kết quả e2e phần nâng cấp (phần D của `docs/bang-chung/M28.log`): 29 khẳng định đạt, 0 không đạt. Trên v0.2.0, ba mục tiêu T1 đều lộ (bộ thử bắt được lỗ trên bản cũ); sau khi nâng, cả ba bị chặn, mục `.htaccess` thường giống hệt v0.2.0 (`path, old, new, old_exists`), ba site vẫn 200, không có dòng `[lỗi]` mới.

6. Chưa hoàn toàn. API và CLI nói đúng. Màn web chưa biết trường `hidden` và `old_link` (luật 4 của `LO-TRINH.md` cấm sửa `web/` đến khi M16 vào `dev`), nên với vai `user` nó hiện file của máy bị ẩn là "sửa, Nội dung không đổi" và symlink là "cả file là dòng thêm". Không lộ dữ liệu, chỉ hiển thị chưa đúng. Việc ghi ở mục "Để sau" của KE-HOACH.

7. M14 chặn những gì *site* làm được với file của site khác bằng quyền của chính user site (OPcache, `.htaccess`, symlink qua web server, log, `/proc`, vhost trùng tên). T1 là site nhờ *panel*, chạy root, đọc hộ qua một đường dẫn site vẽ. Quyền của kernel chặn user site nhưng không chặn root. Nên cần luật ở chỗ khác: panel không được tin đường dẫn trong thư mục site.

8. Để hỏng một lớp vẫn không lộ. Lớp cửa dựa vào mã tpan: nếu một ngày ai đó sửa nó đi theo symlink, lớp API vẫn chặn vì nó dựa vào kernel (user Linux của site không đọc được `/etc/shadow`). Ngược lại, lớp API chỉ lọc cho vai `user`; admin không bị lọc, nên cửa là lớp chặn cho admin. Hai test giữ điều đó: `TestBanXemTruocHaiLop` và `TestVungSiteDocLaiBangUserSite` (cần root, trong máy ảo).

9. `head` mở theo đường dẫn và chặn ở `open()` khi gặp FIFO không có người ghi. Hết hạn thì Go chỉ giết `runuser`; `head` do `runuser` fork ra mồ côi, nằm mãi trong cgroup của `tpan.service`. Mỗi bản xem trước bị đua để lại một tiến trình, cộng dồn đến khi tpan không fork được. `dd iflag=nonblock` mở FIFO xong ngay, `nofollow` từ chối symlink, `timeout` bên trong `runuser` làm lệnh con chết theo hạn, và `Run` giết cả nhóm tiến trình. Tự kiểm trên máy ảo: `mkfifo /tmp/ong; timeout 3 cat /tmp/ong; echo $?` (ra 124), rồi `dd if=/tmp/ong iflag=nonblock bs=1 count=1 status=none; echo $?` (ra 0 ngay).

10. Điều kiện của cả thiết kế là `/var/www` thuộc root và người khác không ghi được. Nếu không, một tiến trình `www-data` (web server bị chiếm) đổi tên được neo của site này thành tên của site kia, và luật chủ của cửa so với một neo đã bị tráo. Cửa kiểm ở mỗi lời gọi (`openAnchor`: `st.Uid != os.Geteuid() || mode&0o022 != 0` thì `ErrRootUnsafe`) và chỉ cách sửa: `chown root:root /var/www && chmod 755 /var/www`. Tự kiểm trên máy ảo: `stat -c '%U:%G %a' /var/www` phải ra `root:root 755`. Test: `TestCuaGocKhongAnToan`.
