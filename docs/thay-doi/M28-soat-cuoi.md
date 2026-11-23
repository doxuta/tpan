# M28 đợt 1: soát cuối (tìm đường vòng qua bản vá)

Ngày 21/11/2026. Thợ soát cuối (chưa đọc mã của mốc trước lượt này). Phạm vi: `docs/thay-doi/M28-thiet-ke.md`, `git diff 4269cc1..62fa060 -- internal cmd` và mã liên quan (steps, core, checks, server, tools, cmd/tpan, templates); pha 2 thêm các commit sửa theo pha 1: 519959e (C5), 33e4640 (C6), e3060db (C7), 9a80f92 (C8), e1d7531 và 841bff3 (`chmodDirs`).

**Trạng thái: PHA 2 xong.** Pha 1 đọc mã. Pha 2 thử trên máy ảo `tpan-m28` từ 12:43 tới 12:54, bản `15a9923` (stack Apache, site `a.example.com` và `b.example.com` của admin, `e.example.com` của khach1 vai `user`), chạy bằng `scripts/vm.sh root`, dùng lại `scripts/e2e-m28/chung.sh` và `phan-tich.py`. Không in nội dung file bí mật nào: mọi kết luận "lộ" hay "bị chặn" là của `phan-tich.py khong-lo` với ba mục tiêu `/etc/shadow`, `wp-config.php` của `a.example.com`, `/var/lib/tpan/tpan.db`. Script thử và chi tiết các phát hiện chưa vá giữ ngoài repo tới khi vá.

Suốt pha 2, tpan giữ PID 3535 và `NRestarts` 0. Cuối pha, site e về đúng như cũ, ba site trả 200, không tiến trình nào của user site còn chạy, không symlink nào thuộc root trong `/var/www`.

## Tóm tắt

**Không tìm được đường vòng nào qua bản vá T1 trên máy thật.** Với `.htaccess` là symlink tới ba mục tiêu, `public` là symlink tới site khác, FIFO, `/dev/zero`, hardlink (kernel chặn), tráo liên tục trong lúc job chạy thật, trên 16 route có bản xem trước hay dữ liệu của site mà vai `user` gọi được, 7 route của admin, CLI của admin, sửa tự động của health, nhận bản sửa tay, khôi phục backup và thùng rác: không câu trả lời nào chứa mục tiêu, root không ghi, đổi chủ hay đổi quyền gì ngoài site, panel không sập (mục "Đã thử, không phá được").

Đường vòng tìm được nằm ở **hai bản sửa của pha 1**: C11 (một dạng đua còn lại của bản sửa C7, lộ một bit mỗi lần; chưa vá) và C12 (bản sửa C5 chỉ chặn giá trị `--file=` tuyệt đối nên giá trị tương đối vẫn lọt luật; ẩn, chưa lời gọi nào dùng; đã sửa). Pha 2 còn thấy ba chỗ mới: C10 và C13 (từ chối phục vụ do một site gây ra, ở web server và ở backup, có từ trước M28; chưa vá) và C14 (backup báo 500 thay cho 409 khi `public` là symlink; đã sửa).

Họ thứ hai (panel chưa đặt trần cỡ và thời gian cho output của lệnh chạy trên dữ liệu do site điều khiển) đã để đợt 2 theo mục 20 của `docs/thay-doi/M28.md`; pha 2 xác nhận C3 và C9 trên máy ảo.

Tổng: **3 nặng, 5 vừa, 6 nhẹ.** Nặng: C1, C2, C3 (đợt 2). Vừa: C4 (đợt 2), C5 (đã sửa), C10, C12, C13. Nhẹ: C6, C7, C8 (đã sửa), C9 (đợt 2), C11, C14.

## Phát hiện

### C1 (nặng): chưa vá, để đợt 2.

Chi tiết, đường đi và bằng chứng giữ ngoài repo tới khi vá (repo công khai; bản đầy đủ của file này nằm ở `agent-dev/`, không vào git).

### C2 (nặng): chưa vá, để đợt 2.

Chi tiết, đường đi và bằng chứng giữ ngoài repo tới khi vá (repo công khai; bản đầy đủ của file này nằm ở `agent-dev/`, không vào git).

### C3 (nặng): chưa vá, để đợt 2.

Chi tiết, đường đi và bằng chứng giữ ngoài repo tới khi vá (repo công khai; bản đầy đủ của file này nằm ở `agent-dev/`, không vào git).

### C4 (vừa): chưa vá, để đợt 2.

Chi tiết, đường đi và bằng chứng giữ ngoài repo tới khi vá (repo công khai; bản đầy đủ của file này nằm ở `agent-dev/`, không vào git).

### C5 (vừa): (Đã sửa ở 519959e.) Luật lệnh root (mục 3.4, bất biến B6) coi mọi đường dẫn vùng site trong argv của `tar --create`, `du`, `useradd` là toán hạng "thành viên", nên cho qua các cờ nhận FILE mà chương trình mở theo đường dẫn, ghi đè, hay chạy như chương trình.

- Đường đi: luật cũ chỉ đòi đường dẫn vùng site là neo hoặc `<neo>/<một tên>`. Lý do "tar, du không đi theo symlink ở thành phần cuối" (N3) đúng cho toán hạng thành viên, không đúng cho giá trị của cờ. 15 dạng lọt: `tar --create --file=<neo>/x`, `-f`, `-T`, `--files-from`, `--exclude-from`, `--index-file`, `--volno-file`, `--listed-incremental`, `-I`, `--use-compress-program`, `-F`; `du --files0-from`, `-X`, `-L`; `useradd -P`. Chưa lời gọi nào của tpan dùng chúng.
- Bằng chứng: pha 1, trên Mac: `go run ./tmp-phien/soat-cuoi/rulecheck/main.go` (gọi `system.NewFake().Run`, cùng `checkRootZone` với `LocalSystem`, không chạy lệnh thật) ra 15 dòng `ALLOWED by rule`. Sau 519959e: `go run ./tmp-phien/soat-cuoi/rulecheck2/main.go` cho cả năm dạng đại diện (`--file=` tuyệt đối, `-I`, `--files0-from`, `du -L`, `useradd -P`) là `refused`, ba dạng tpan đang dùng (tar của backup, `du -sb --one-file-system --`, `useradd` của `SiteUser`) vẫn qua. Bản sửa có đường vòng: xem C12.
- Đề xuất sửa: đã làm (danh sách trắng theo từng cờ); xem C12 cho phần còn thiếu.

### C6 (nhẹ): (Đã sửa ở 33e4640.) CLI in nội dung do site viết (output của `wp`) mà không lọc ký tự điều khiển, ngoài hai chỗ commit a549648 đã sửa.

- Đường đi: `progress.onEvent` (`EventOutput`, `EventStepFail`), `printReport` (`rep.Job.Error`), `printResult` (Summary chép `firstLineWith(out, "Error")` của `wp`).
- Bằng chứng (máy ảo, `soat3.sh esc`, 12:52:11, bản đã sửa): `.wp-cli/config.yml` của site e `require` một file in `ESC ] 0 ; M28-ESC BEL Error: m28 ESC [2K` rồi `exit(1)`. Chạy thẳng `wp` dưới user site: output có 2 byte ESC. `tpan site add e.example.com --dry-run`: 0 ESC, 0 BEL, chữ `M28-ESC` vẫn hiện (đã lọc). `tpan health e.example.com`: 0 ESC, 0 BEL. `tpan backup create e.example.com --yes --verbose` (job thật, in output từng lệnh): 0 ESC, 0 BEL. Bản sửa giữ được.
- Đề xuất sửa: đã làm.

### C7 (nhẹ): (Đã sửa ở e3060db, còn dạng đua ở C11.) Khôi phục site từ thùng rác trên Apache gọi `htaccessIfMissing(TrashDir/public)` bằng `sys.Stat` ngoài vùng site, đi theo `public` nếu là symlink.

- Đường đi: `internal/core/trash.go:216`, `internal/core/stack_switch.go` (`htaccessIfMissing`). Bản sửa thêm một lần `sys.Stat(public)` trước: là symlink thì trả `sitefs.ErrSymlink` (409), không phải thư mục thì lỗi.
- Bằng chứng (máy ảo, `soat2.sh c7`, 12:49:37): khach1 đưa site e vào thùng rác (job `succeeded`); bằng root đổi `TrashDir/public` thành symlink tới `/root`, chủ là user site (giả lập tiến trình còn sống của site, vì thùng rác là 0700 root). khach1 và admin `POST /sites/e.example.com/restore?dry_run=1`: cả hai **409 `site_path_unsafe`**, không chứa mục tiêu nào. Trả `public` thật: bản xem trước 200; khôi phục thật `succeeded`, site e trả 200. Dạng tĩnh đã chặn; dạng đua thì chưa: C11.
- Đề xuất sửa: xem C11.

### C8 (nhẹ): (Đã sửa ở 9a80f92.) `sitefs.Symlink` đổi chủ cái link bằng `fchownat` theo tên ngay sau `symlinkat`; trên máy tắt `fs.protected_hardlinks`, một hardlink tráo vào giữa hai lời gọi bị root đổi chủ về user site.

- Đường đi: `replaceSymlink` (`internal/steps/common.go`) tạo link ở tên đoán trước được `public/.htaccess.tpan-tmp`, nên K1 chỉ cần inotify trên tên đó. Bản sửa: `chownLink` mở cái link bằng `O_PATH|O_NOFOLLOW`, `fstat` phải là `S_IFLNK`, rồi `fchownat(fd, "", AT_EMPTY_PATH)` trên chính fd (`internal/sitefs/chownlink_linux.go`); bản macOS dùng `O_SYMLINK` và `fchown`. Đọc mã: đúng, không còn lần đổi chủ nào theo tên; nhánh lỗi chỉ `unlinkat` cái tên (xoá một hardlink thì không xoá file đích).
- Bằng chứng: chưa thử, vì cần `sysctl fs.protected_hardlinks=0` mà phạm vi pha 2 không cho. Gián tiếp: sau ba job chạy thật bị hoàn tác trong lúc tráo `.htaccess` (mục "Đã thử"), `find /var/www -type l -user root` ra 0.
- Đề xuất sửa: đã làm. Nên đưa phép kiểm health cho `fs.protected_hardlinks`, `fs.protected_symlinks` lên sớm (mục 5 đã đề xuất).

### C9 (nhẹ): chưa vá, để đợt 2.

Chi tiết, đường đi và bằng chứng giữ ngoài repo tới khi vá (repo công khai; bản đầy đủ của file này nằm ở `agent-dev/`, không vào git).

### C10 (vừa): chưa vá, để đợt 2.

Chi tiết, đường đi và bằng chứng giữ ngoài repo tới khi vá (repo công khai; bản đầy đủ của file này nằm ở `agent-dev/`, không vào git).

### C11 (nhẹ): chưa vá, để đợt 2.

Chi tiết, đường đi và bằng chứng giữ ngoài repo tới khi vá (repo công khai; bản đầy đủ của file này nằm ở `agent-dev/`, không vào git).

### C12 (vừa): (Mới ở pha 2, đường vòng qua bản sửa C5.) Bản sửa C5 chỉ chặn giá trị `--file=` chứa chuỗi `/var/www/`; giá trị tương đối vẫn trỏ vào vùng site và vẫn qua luật.

- Đường đi: `tarCreateOnly` (sau 519959e) từ chối `--file=` khi `strings.Contains(a, prefix)`. Giá trị tương đối không chứa chuỗi đó; phần sau của `checkRootZone` ghép nó với `Dir` (rỗng là `/`), thấy nằm trong vùng, rồi chỉ đòi "neo hoặc `<neo>/<một tên>`" như toán hạng thành viên, nên cho qua. tar mở file nén theo đường dẫn từ thư mục làm việc ban đầu và đi theo symlink ở tên cuối: root ghi đè file bất kỳ. Chưa lời gọi nào của tpan dùng dạng này (ẩn, như C5).
- Bằng chứng (Mac, `go run ./tmp-phien/soat-cuoi/rulecheck2/main.go`, `FakeSystem.Run`, không chạy lệnh thật): `ALLOWED by rule` cho `tar --create --gzip --file=var/www/a.example.com/out.tar ... --directory=<neo> public` (`Dir` rỗng); `--file=out.tar` với `Dir` là neo; `--file=www/a.example.com/out.tar` với `Dir=/var`; `--file=../a.example.com/out.tar` với `Dir` là neo của site khác. Cũng qua: `useradd --home-dir <neo>/public` (vô hại vì chỉ là chuỗi trong `/etc/passwd`, nhưng trái chú thích "phải là neo hay ngoài vùng").
- Đề xuất sửa: trong `tarCreateOnly`, đòi `--file=` là đường dẫn tuyệt đối, sạch, ngoài vùng (`path.IsAbs(v) && path.Clean(v) == v && !strings.HasPrefix(v+"/", prefix)`); làm tương tự cho mọi giá trị cờ. Thêm bốn dòng trên vào bảng test của B6.

### C13 (vừa): chưa vá, để đợt 2.

Chi tiết, đường đi và bằng chứng giữ ngoài repo tới khi vá (repo công khai; bản đầy đủ của file này nằm ở `agent-dev/`, không vào git).

### C14 (nhẹ): (Mới ở pha 2.) Backup và khôi phục backup trả HTTP 500 `internal` thay cho 409 `site_path_unsafe` khi `public` là symlink.

- Đường đi: `backupPublic` (`internal/steps/backup.go:132-142`) tự kiểm bằng `sys.Stat` và trả `fmt.Errorf` thường (không bọc `*sitefs.Error`), nên `apiErrorStatus` ra 500 và log `ERROR API lỗi`. Bảng mã lỗi ở mục 3.7 (S12) hứa trạng thái do site tự tạo không ra 500. Không lộ gì: lời lỗi chỉ có đường dẫn và loại file.
- Bằng chứng (máy ảo, `soat2.sh cha`, 12:46:48): `public` của site e là symlink tới `public` của site a; khach1 `POST /sites/e.example.com/backups?dry_run=1` và `POST /backups/1cbe89fccd995242/restore?dry_run=1`: **500 `internal`**, lời "đo chỗ cần cho backup e.example.com: /var/www/e.example.com/public không phải thư mục (L---------); tpan không backup qua symlink"; journal của tpan có ba dòng `ERROR API lỗi`. Ở `soat3.sh backupdua`, 5 trong 6 lần backup khi `public` đang bị tráo cũng ra 500.
- Đề xuất sửa: `backupPublic` trả `&sitefs.Error{Op: "kiểm", Path: p, Err: sitefs.ErrSymlink}` (hay `ErrNotRegular`) để ra 409 có câu hướng dẫn.

## Phép thử chưa chạy (cần phiên chính cho phép, hoặc để đợt 2)

Giữ ngoài repo cùng bản đầy đủ.

## Đã thử, không phá được

Máy ảo `tpan-m28`, bản `15a9923`, 12:43 tới 12:54; tpan giữ PID 3535, `NRestarts` 0 suốt các phép dưới đây. "Bị chặn" là `phan-tich.py khong-lo` với ba mục tiêu.

- **T1 trên mọi route** (`soat2.sh t1`, 12:44:20): `.htaccess` của site e lần lượt là symlink tới `/etc/shadow`, `wp-config.php` của site a, `tpan.db`. Với mỗi mục tiêu, khach1 gọi: `POST /sites?dry_run=1` (200, có `old_link`), `POST /sites/e/ssl?dry_run=1` (409 `acme_email_missing`), `POST /sites/e/backups?dry_run=1` (200), `POST /sites/e/htaccess?dry_run=1` chế độ `tat` (200), `POST /sites/e/php?dry_run=1` (409 `php_same_version`), `DELETE /sites/e?dry_run=1` (200), `POST /databases/e/password?dry_run=1` (200), `POST /databases/e/dump?dry_run=1` (200), `POST /apply?dry_run=1` (200), `POST /backups/<id>/restore?dry_run=1` (200), `GET /sites/e/route`, `GET /sites/e`, `GET /checks`, `GET /overview`, `GET /jobs` (200); admin gọi `POST /sites?dry_run=1` (200, admin không bị lọc), `POST /security/harden?dry_run=1` (200), `POST /stack/switch?dry_run=1` sang nginx (409 `precondition_failed`), `POST /files/accept` với `.htaccess` (409 `site_path_unsafe`), `GET /checks`. Cả 60 câu trả lời: bị chặn. `.htaccess` về đúng bản gốc (sha256 `323969eac053238b`).
- **Thư mục cha là symlink** (`soat2.sh cha`, 12:46:48): `public` của site e là symlink tới `public` của site a; cùng bộ route trên, cộng khach1 chạy thật `POST /sites`. `POST /sites?dry_run=1` của khach1 và admin, chạy thật, `files/accept`: 409 `site_path_unsafe`; backup và restore dry-run: 500 (C14); còn lại 200 hoặc 409 như trên. Cả 21 câu trả lời: bị chặn. sha256, chủ, quyền của `wp-config.php`, `.htaccess`, neo và `public` của site a, và của `/etc/shadow`: không đổi. Không symlink nào thuộc root trong `/var/www`.
- **FIFO và `/dev/zero`** (`soat2.sh fifo`, 12:44:41): `.htaccess` là FIFO: `POST /sites?dry_run=1` 409 `site_path_unsafe` trong 0,09 giây; `htaccess`, `backups`, `DELETE` dry-run 200; `harden` dry-run 200 trong 0,01 giây (`dd iflag=nonblock`); `files/accept` 409. `.htaccess` là symlink tới `/dev/zero`: mọi route 200 hay 409 dưới 0,5 giây. Không câu nào chứa mục tiêu; không `dd`, `head` nào của user site còn lại.
- **CLI của admin** (`soat2.sh cli`, 12:44:57): `.htaccess` là symlink tới `/etc/shadow`; `tpan site add e.example.com --dry-run` (thoát 0, 2 dòng nhắc symlink) và `tpan health e.example.com`: bị chặn, không có byte ESC.
- **Tráo trong lúc job chạy thật** (`soat2.sh chaythat`, 12:47:36): (a) kẻ tráo của e2e (`tra-doi.py file`) dưới user site đổi `.htaccess` giữa file thường và symlink tới `/etc/shadow` (80 710 lần) trong lúc khach1 chạy thật `POST /sites` ba lần: cả ba job `rolled_back` ở bước `file:.../.htaccess` (kiểm lại sau khi ghi không đạt vì file đổi liên tục); câu trả lời `GET /jobs/{id}` bị chặn. (b) `tra-doi.py dir` đổi `public` với symlink tới một thư mục nạn nhân `0755` của root (396 633 lần): hai lần `POST /sites` bị từ chối 409 ngay lúc dựng Plan. Thư mục nạn nhân giữ nguyên chủ, quyền, danh sách; site a và `/etc/shadow` giữ nguyên; không symlink nào thuộc root trong `/var/www`.
- **Backup chạy thật trong lúc tráo `public`** (`soat3.sh backupdua`, 12:52:20): `tra-doi.py dir` tới một thư mục nạn nhân chứa file đánh dấu `0600`; 6 lần khach1 backup: 5 lần 500 (C14) lúc dựng Plan, 1 lần job chạy rồi `rolled_back` (không để lại archive). Không archive nào của site e có tên file đánh dấu.
- **Sửa tự động của health** (`soat3.sh fix`, 12:52:57): `wp-config.php` của site e đổi 0644, health chạy lại; rồi thay nó bằng symlink tới `wp-config.php` của site a. Admin `POST /fixes?dry_run=1` với `wp.config:e.example.com`: 200, bị chặn; chạy thật: job `rolled_back`. sha256, chủ, quyền (`600`) của `wp-config.php` site a không đổi. `wp-config.php` của site e trả về đúng bản gốc, `600`.
- **Nhận bản sửa tay** (admin `POST /files/accept` trên `.htaccess`): symlink và FIFO đều 409 `site_path_unsafe`, không có nội dung (trong `t1`, `fifo`, `cha`).
- **Khôi phục thùng rác khi `public` là symlink** (C7 tĩnh): 409 cho cả khach1 và admin (xem C7).
- **Hardlink**: `ln /etc/shadow public/m28-hl` dưới user site: `Operation not permitted` (`fs.protected_hardlinks=1`).
- **Mã của các commit sau pha 1** (đọc mã): e1d7531 và 841bff3 (`chmodDirs` bỏ qua `ENOTDIR`, `ELOOP` ở `Stat`, `Chmod` và lúc liệt kê thư mục con): chỉ bỏ qua thư mục của chính site vừa bị tráo, lỗi ở chính `public` (độ sâu 1) vẫn làm hỏng bước, nên không mở đường nào ra ngoài site; e2e A7 của phiên chính đã chạy bản này. 9a80f92: đúng (xem C8). 33e4640: đúng (xem C6). b29f6dd: không xét lại (phiên chính đã so cây cú pháp).
- **Nhập bản xuất DB bằng root** (đọc mã, chưa xem dòng đầu của một bản xuất thật): `mariadb-dump` thoát ký tự xuống dòng trong tên bảng ở chú thích (sửa từ CVE-2016-5483) và bản 10.11 mới có dòng "sandbox mode" ở đầu file.
- **Đọc mã ở pha 1** (vẫn đúng ở pha 2, không có gì mới phá được): cửa `sitefs` đi bằng `openat(O_NOFOLLOW)` từng thành phần và thao tác tên cuối bằng fd; `Split` làm sạch đường dẫn trước khi so; trùng tên user và DB giữa hai domain bị `unmanaged` từ chối; khôi phục backup giải nén vào thư mục `0700` của root với `checkTarList`, `chown -R -h`, `find -P`; thư mục log của site thuộc root nên site không tráo được tên nào trong đó; pool PHP-FPM và vhost không có đường dẫn nào trong thư mục site mà tiến trình chủ (root) mở; lệnh dưới user site có `Dir` trong vùng site chỉ `chdir` bằng root rồi mọi truy cập bị kernel kiểm bằng quyền user site; `wp` không chạy với `--allow-root`; `DBPassword` chỉ nhận mật khẩu cũ dạng `[A-Za-z0-9]{32}`; FUSE không có `allow_other` thì root bị từ chối ngay; lease ghi thì cửa trả `EWOULDBLOCK` ngay; sự kiện job không mang `Old`, `New`.

## Trả lời của phiên chính (21/11/2026, 13:10)

- **Không có đường vòng qua bản vá T1** (pha 1 đọc mã, pha 2 thử trên máy ảo với 16 route của vai `user`, 7 route admin, CLI, sửa tự động, nhận bản sửa tay, khôi phục backup, thùng rác): giữ kết luận đó cho đợt 1.
- **Sửa trong đợt 1, mỗi cái một commit kèm test:** C5 `519959e`, C6 `33e4640`, C7 `e3060db`, C8 `9a80f92` (sau pha 1); C12 `e2eab8d` (`--file=` của `tar` phải tuyệt đối, sạch, ngoài vùng; `--home-dir` của `useradd` trong vùng phải đúng là neo; tám dòng `c12-*` trong bảng của `TestLenhRootKhongNhanDuongDanSite`); C14 `c5ff3bf` (`backupPublic` trả `*sitefs.Error`, API ra 409 `site_path_unsafe`; `TestBackupPublicLoiCuaCua`). e2e bước 31 chạy lại trên bản có hai bản sửa này (`docs/bang-chung/M28.log`).
- **Để đợt 2, chưa vá:** C1, C2, C3, C4, C9 (họ "output và thời gian của lệnh không có trần"; lý do ở mục 20 của `docs/thay-doi/M28.md`; C3, C9 đã xác nhận trên máy ảo ở pha 2), C10, C11, C13. Chi tiết giữ ngoài repo tới khi vá.
