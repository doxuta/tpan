# M9: backup, khôi phục, thao tác DB, cron thật

Đọc kèm: `01-m1-cai-stack.md` (engine, Step, Check rồi Apply rồi Check), `06-m6-hang-loat-va-thung-rac.md` (thùng rác, `MoveTree`, `rename` và phân vùng), `12-m12-phien-ban-va-cap-nhat.md` (`Planned`, `MariaDBDump`, struct nhúng để thay một phương thức), `docs/KE-HOACH.md` mục 6.5, 6.7, 6.9, 6.10, và `docs/thay-doi/M9.md` (mọi chỗ khác đặc tả của mốc này, kèm lý do).

## 1. Mốc này làm được gì

- `sudo tpan backup create <domain>` gói mã nguồn, file tải lên và DB của một site vào một file `.tar.gz` có sha256; `--all` làm cho mọi site đang chạy rồi chép luôn `tpan.db`. `tpan backup settings --at 03:30` đặt lịch hằng ngày, giữ 3 bản mỗi site (đổi bằng `--keep`).
- `sudo tpan backup restore <domain>` đưa site về đúng một bản backup: tự backup hiện trạng trước, giải nén ra chỗ riêng, tráo thư mục bằng một lần `rename`, nhập lại DB. Xoá hẳn một site và nâng MariaDB cũng tự đòi có backup trước.
- `tpan db passwd | dump | import` đổi mật khẩu user DB mà mật khẩu không bao giờ nằm trên dòng lệnh hay trong log, xuất DB ra `.sql`, và nhập lại một bản đã xuất (luôn tự xuất bản hiện tại trước).
- Mỗi site có cron thật trong `/etc/cron.d/tpan-<slug>`, 5 phút một lần, và `DISABLE_WP_CRON` được bật: bài hẹn giờ được đăng cả khi không có ai vào trang. `tpan cron list` cho biết lần chạy gần nhất và kết quả.
- Đĩa gần đầy thì backup và xuất DB bị từ chối trước khi ghi byte nào (luật 1,2 lần và 10%).

## 2. Chạy thử

Mọi output ở mục 2.1 tới 2.13 lấy nguyên văn từ `docs/bang-chung/M9.log`: lần chạy `scripts/e2e-m9.sh` ngày 20/11/2026 từ 01:11:44 tới 01:15:56 (giờ máy ảo, +07:00), dòng cuối là `TỔNG KẾT: 128 khẳng định đạt, 0 khẳng định không đạt` (dòng 2490). Máy ảo tên `tpan-m9` (file `.tpan-vm`), Multipass, Ubuntu 24.04, arm64, chỉ chạy lượt Nginx.

Một điều cần biết về bản mã của log này: dòng 90 tới 101 cho thấy HEAD lúc đó là `9a1def4`, còn `internal/steps/backup.go` đang có thay đổi chưa commit. Thay đổi đó chính là bản sửa nhóm của `wp-config.php` sau khôi phục (mục 5, chỗ sai 1), được commit ngay sau thành `1454ce8`. Lượt chạy trước bản sửa nằm ở `docs/bang-chung/M9-lan1.log` (124 đạt, 3 không đạt).

Cách đọc log, giống các file giải thích trước:

- Mọi lệnh gõ trên Mac, trong thư mục repo. `scripts/vm.sh exec X` chạy X trong máy ảo; `scripts/vm.sh root X` chạy X trong máy ảo bằng `sudo`. Riêng `curl https://<vm>:8443/...` chạy thẳng trên Mac.
- Kịch bản in lệnh bằng `$*` nên mất dấu nháy. Lệnh `bash -c` dưới đây mình chép từ `scripts/e2e-m9.sh` (có nháy đúng); output giữ nguyên.
- Kịch bản che IP (`<vm>`), thư mục tạm (`<tmp>`) và mọi mật khẩu. Vài chỗ kịch bản đọc thẳng `tpan.db` bằng một đoạn `python3` dài; mình chỉ chép phần output.
- Giờ trong tên file backup và trong JSON là UTC (`181505` là 18:15:05 UTC, tức 01:15:05 giờ máy ảo).

### 2.1 Tạo site: hai bước mới

Chạy trên Mac:

```bash
make vm-reset
make vm-deploy
scripts/vm.sh root tpan stack install --web nginx --yes
scripts/vm.sh root tpan site add m9a.example.com --email admin@example.com --yes
```

Phần giữa output của lệnh cuối (M9.log dòng 179 tới 234):

```
- Giải nén WordPress vào /var/www/m9a.example.com/public ...
  xong
- Tạo /var/www/m9a.example.com/public/wp-config.php ...
  xong
- Đặt DISABLE_WP_CRON = true trong /var/www/m9a.example.com/public/wp-config.php ...
  xong
- Đặt lịch cron WordPress của m9a.example.com (5 phút một lần) ...
  xong
- Ghi vhost nginx cho m9a.example.com ...
  xong
```

Hai bước giữa là của M9. Chúng nằm ngay sau bước tạo `wp-config.php` và trước vhost; mục 3.16 nói vì sao đúng chỗ đó. Kịch bản tạo thêm `m9b.example.com` y như vậy.

### 2.2 File cron và `DISABLE_WP_CRON`

```bash
scripts/vm.sh root stat -c '%U:%G %a %n' /etc/cron.d/tpan-m9a_example_com_c2b3ff
scripts/vm.sh root cat /etc/cron.d/tpan-m9a_example_com_c2b3ff
scripts/vm.sh root bash -c 'cd / && runuser -u "$2" -- wp config get DISABLE_WP_CRON --type=constant --path="/var/www/$1/public"' _ m9a.example.com tp_m9a_example_com_c2b3ff
```

Output (M9.log dòng 340 tới 378, bỏ phần chú thích giữa file):

```
root:root 644 /etc/cron.d/tpan-m9a_example_com_c2b3ff
# Do tpan quản lý. Sửa tay sẽ bị tpan health báo lệch.
#
# Cron của WordPress cho m9a.example.com: mỗi 5 phút chạy các việc đã đến giờ
# (bài hẹn giờ, kiểm cập nhật, thư dọn dẹp của plugin).
...
# Lệnh dùng đường dẫn tuyệt đối vì PATH của cron rất hẹp.
*/5 * * * * root /usr/local/bin/tpan cron run m9a.example.com >/dev/null 2>&1
1
```

Ba điều để ý. Tên file theo slug (`m9a_example_com_c2b3ff`), không theo domain: tên có dấu chấm thì cron bỏ qua (mục 4.3). Dòng lệnh có cột thứ sáu là `root`: đó là cú pháp riêng của file trong `/etc/cron.d`. Và `wp config get` in `1`, không in `true`: không có `--format=json` thì WP-CLI in giá trị PHP `true` thành `1` (câu hỏi 4 ở mục 6).

Journal của cron (M9.log dòng 381 tới 393) chỉ có các dòng `Started cron.service` lúc khởi động máy, không có dòng `RELOAD` nào. Vậy mà file mới vẫn được chạy ở mục 2.3: cron tự thấy file mới, tpan không reload gì.

### 2.3 Bài hẹn giờ được đăng khi không có ai vào trang

Kịch bản hỏi WordPress giờ hiện tại cộng 2 phút, tạo một bài `future` đúng giờ đó, rồi chỉ đọc DB mỗi 15 giây, không gọi site lần nào:

```bash
scripts/vm.sh root bash -c 'cd / && runuser -u "$2" -- wp post create --post_title="e2e-m9 bai hen gio" --post_status=future --post_date="$3" --porcelain --path="/var/www/$1/public"' _ m9a.example.com tp_m9a_example_com_c2b3ff "2026-11-19 18:14:59"
```

Output và phần chờ (M9.log dòng 399 tới 444, rút gọn):

```
2026-11-19 18:14:59|+00:00|2026-11-19 18:12:59
5
future	2026-11-19 18:14:59	2026-11-19 18:14:59
hook	next_run_gmt	next_run_relative
publish_future_post	2026-11-19 18:14:59	2 minutes
3 /var/log/tpan/sites/m9a.example.com/access.log
...
[2026-11-20 01:14:46] lần 7: post_status=future
[2026-11-20 01:15:01] lần 8: post_status=publish
publish	2026-11-19 18:14:59	2026-11-19 18:14:59
3 /var/log/tpan/sites/m9a.example.com/access.log
  => ĐẠT: access.log của m9a.example.com không đổi số dòng (vẫn 3): không khách nào vào
```

Đừng nhầm giờ: WordPress của site dùng múi `+00:00` (dòng đầu), còn máy ảo dùng +07:00. Hẹn 18:14:59 của WordPress là 01:14:59 giờ máy ảo. Lần cron 01:15:01 đăng bài; access.log vẫn 3 dòng (cả 3 là curl lúc tạo site, 01:12), nên không lượt xem nào kích hoạt việc này.

Lần này gặp may: giờ hẹn rơi ngay trước một mốc 5 phút. Lượt chạy đầu (`M9-lan1.log` dòng 395 tới 450) cho thấy độ trễ xấu nhất: hẹn 18:05:45 (01:05:45 giờ máy), lần cron 01:05:01 chạy sớm hơn giờ hẹn 44 giây nên chưa đăng, tới lần 01:10:01 mới đăng (kịch bản thấy `publish` lúc 01:10:06). Trễ tối đa khoảng 5 phút, đúng nhịp `*/5`.

Sau đó màn Cron đã có lần chạy (M9.log dòng 495 tới 517):

```
Mỗi site chạy `wp cron event run --due-now` theo lịch */5 * * * *

DOMAIN                                   BẬT    CHẠY LÚC         KẾT QUẢ  MẤT
m9a.example.com                          bật    2026-11-20 01:15 ok       178ms
m9b.example.com                          bật    2026-11-20 01:15 ok       172ms

Lịch backup: tắt. Bật bằng: sudo tpan backup settings --at HH:MM
```

```
m9b.example.com 2026-11-19T18:15:01.184347969Z 172 ok 0 
m9a.example.com 2026-11-19T18:15:01.190136604Z 178 ok 0 
2026-11-20T01:15:01+07:00 tpan-m9 CRON[15263]: (root) CMD (/usr/local/bin/tpan cron run m9b.example.com >/dev/null 2>&1)
2026-11-20T01:15:01+07:00 tpan-m9 CRON[15264]: (root) CMD (/usr/local/bin/tpan cron run m9a.example.com >/dev/null 2>&1)
```

Hai dòng đầu là bảng `cron_runs` trong `tpan.db` (domain, giờ bắt đầu, mili giây, trạng thái, mã thoát, output). Hai dòng sau là journal của cron. Cùng một lần chạy, nhìn từ hai phía.

### 2.4 Mã thoát của `wp cron event run --due-now`

Mục 16 của KE-HOACH hỏi lệnh này thoát mấy khi không có việc. M9.log dòng 521 tới 537:

```
Executed the cron event 'do_pings' in 0.863s.
Success: Executed a total of 1 cron event.
mã thoát: 0
Success: Executed a total of 0 cron events.
mã thoát (lần hai): 0
mã thoát (--quiet): 0
```

Không có việc vẫn thoát 0, nên `Cron.Run` coi mã khác 0 là lỗi thật. Có `--quiet` thì không in gì khi mọi việc thành công; output còn lại chủ yếu là lỗi, đúng thứ màn Cron cần giữ.

### 2.5 Backup một site

Trước khi backup, kịch bản ghi một file dấu (`e2e-m9.txt`), đổi tiêu đề bài 1 thành `e2e-m9 tieu de luc backup`, và chụp lại hai dấu vân tay của cây `public`: mã băm nội dung mọi file, và mã băm của danh sách quyền, chủ, loại của từng mục (M9.log dòng 553 tới 585):

```
d41ed1bda3ea15183d1ebfe01ee7b2a07e7a492edc8fe2277fda50383cb3793e
3785
29d2514bbbee5c9cbfcc61693e070fabebc5791efc92e3c7b1018731b1184740
    471 2750 tp_m9a_example_com_c2b3ff:www-data d
      1 600 tp_m9a_example_com_c2b3ff:www-data f
   3784 640 tp_m9a_example_com_c2b3ff:www-data f
```

3785 file, 471 thư mục. Thư mục 2750, file 640, đúng một file 600 (`wp-config.php`), tất cả chủ `tp_...:www-data`. Không có symlink nào (`symlink: 0`, dòng 583).

```bash
scripts/vm.sh root tpan backup create m9a.example.com --yes
scripts/vm.sh root tpan backup list m9a.example.com --json
```

Output (M9.log dòng 591 tới 663, rút gọn phần đọc `tpan.db`):

```
Bắt đầu job bb08bf97b3d771da
- Backup site m9a.example.com vào /var/lib/tpan/backups/m9a.example.com ...
  xong

Job bb08bf97b3d771da: thành công
...
thời gian job: 2.3 giây
  command backup.create:m9a.example.com du -sb --one-file-system -- /var/www/m9a.example.com/public
  command backup.create:m9a.example.com mariadb -N -B
  command backup.create:m9a.example.com mariadb-dump --single-transaction --quick --result-file=/var/lib/tpan/backups/m9a.example.com/.tmp-20261119-181505/db.sql tp_m9a_example_com_c2b3ff
  command backup.create:m9a.example.com tail -n 1 -- /var/lib/tpan/backups/m9a.example.com/.tmp-20261119-181505/db.sql
  command backup.create:m9a.example.com sha256sum -- /var/lib/tpan/backups/m9a.example.com/.tmp-20261119-181505/db.sql
  command backup.create:m9a.example.com runuser -u tp_m9a_example_com_c2b3ff -- wp core version --path=/var/www/m9a.example.com/public
  command backup.create:m9a.example.com tar --create --gzip --file=/var/lib/tpan/backups/m9a.example.com/m9a.example.com-20261119-181505.tar.gz.part --one-file-system --directory=/var/lib/tpan/backups/m9a.example.com/.tmp-20261119-181505 manifest.json db.sql --directory=/var/www/m9a.example.com public
  command backup.create:m9a.example.com sha256sum -- /var/lib/tpan/backups/m9a.example.com/m9a.example.com-20261119-181505.tar.gz
[
  {
    "id": "fd1eacba34104f63",
    "domain": "m9a.example.com",
    "kind": "full",
    "reason": "manual",
    "path": "/var/lib/tpan/backups/m9a.example.com/m9a.example.com-20261119-181505.tar.gz",
    "size": 35623411,
    "sha256": "3ac52b242818e0c70d2cff092fbdddd4eedf8a581a1ab807ea3b55040bfba05d",
    "wp_version": "7.1.5",
    "php_version": "8.3",
    "job_id": "bb08bf97b3d771da",
    "created": "2026-11-19T18:15:07.481062875Z"
  }
]
```

Tám lệnh trên là đúng thứ tự trong `BackupArchive.Apply` (mục 3.3): đo, xuất DB, kiểm dòng cuối, băm DB, hỏi phiên bản WordPress, nén, băm file nén. Dòng `mariadb -N -B` không kèm câu SQL vì câu SQL đi qua stdin (mục 4.8).

Kiểm file từ phía ngoài tpan (M9.log dòng 668 tới 684):

```
3ac52b242818e0c70d2cff092fbdddd4eedf8a581a1ab807ea3b55040bfba05d  /var/lib/tpan/backups/m9a.example.com/m9a.example.com-20261119-181505.tar.gz
600 root:root 35623411 /var/lib/tpan/backups/m9a.example.com/m9a.example.com-20261119-181505.tar.gz
700 root:root 4096 /var/lib/tpan/backups/m9a.example.com
700 root:root 4096 /var/lib/tpan/backups
manifest.json
db.sql
public/
số mục trong tar: 4258
{
  "domain": "m9a.example.com",
  "wp_version": "7.1.5",
  "php_version": "8.3",
  "created": "2026-11-19T18:15:05Z",
  "db_sha256": "a5c012f3cac5722a201e24daf9ab7b5a7c243ad7318ef971d27d10d141bc7614"
}
```

sha256 khớp dòng trong bảng `backups`. File 0600, hai tầng thư mục 0700, đều của root: file có toàn bộ DB của site nên chỉ root được đọc. 4258 mục = 3785 file + 471 thư mục + `manifest.json` + `db.sql`.

### 2.6 Sửa site rồi khôi phục

Kịch bản đổi nội dung file dấu, thêm file `e2e-m9-moi.txt`, đổi tiêu đề bài 1 thành `e2e-m9 tieu de da sua` (M9.log dòng 690 tới 704). Rồi xem trước:

```bash
scripts/vm.sh root tpan backup restore m9a.example.com --dry-run
```

Output rút gọn (M9.log dòng 709 tới 747):

```
Kế hoạch: Khôi phục site m9a.example.com từ bản backup fd1eacba34104f63
6 bước sẽ chạy, 0 bước đã đúng sẽ bỏ qua, 3 bước có thể gián đoạn dịch vụ.

 1. [sẽ chạy] Backup site m9a.example.com vào /var/lib/tpan/backups/m9a.example.com  (backup.create:m9a.example.com)
    ...
    $ tar --create --gzip '--file=/var/lib/tpan/backups/m9a.example.com/m9a.example.com-<giờ lúc chạy>.tar.gz.part' ...
 2. [sẽ chạy] Giải nén /var/lib/tpan/backups/m9a.example.com/m9a.example.com-20261119-181505.tar.gz vào /var/lib/tpan/restore/m9a_example_com_c2b3ff  (backup.extract:m9a.example.com)
    $ sha256sum -- /var/lib/tpan/backups/m9a.example.com/m9a.example.com-20261119-181505.tar.gz
    $ tar --list --verbose --gzip --file=/var/lib/tpan/backups/m9a.example.com/m9a.example.com-20261119-181505.tar.gz
    $ tar --extract --gzip --file=/var/lib/tpan/backups/m9a.example.com/m9a.example.com-20261119-181505.tar.gz --directory=/var/lib/tpan/restore/m9a_example_com_c2b3ff --no-same-owner --no-same-permissions
    $ chown -R -h tp_m9a_example_com_c2b3ff:www-data -- /var/lib/tpan/restore/m9a_example_com_c2b3ff/public
    $ find -P /var/lib/tpan/restore/m9a_example_com_c2b3ff/public -type d -exec chmod 2750 '{}' +
    $ find -P /var/lib/tpan/restore/m9a_example_com_c2b3ff/public -type f -exec chmod 0640 '{}' +
 3. [sẽ chạy] Cất mã nguồn hiện tại của m9a.example.com sang /var/lib/tpan/restore/m9a_example_com_c2b3ff/old-public  (dir.move:/var/www/m9a.example.com/public)
    NGUY HIỂM: bước này có thể làm gián đoạn dịch vụ.
 4. [sẽ chạy] Đưa mã nguồn từ bản backup vào /var/www/m9a.example.com/public  (dir.move:/var/lib/tpan/restore/m9a_example_com_c2b3ff/public)
    NGUY HIỂM: bước này có thể làm gián đoạn dịch vụ.
 5. [sẽ chạy] Xuất DB tp_m9a_example_com_c2b3ff ra /var/lib/tpan/restore/m9a_example_com_c2b3ff/db-before.sql  (mariadb.dump:/var/lib/tpan/restore/m9a_example_com_c2b3ff/db-before.sql)
 6. [sẽ chạy] Nhập /var/lib/tpan/restore/m9a_example_com_c2b3ff/db.sql vào DB tp_m9a_example_com_c2b3ff  (db.import:m9a.example.com)
    NGUY HIỂM: bước này có thể làm gián đoạn dịch vụ.
    $ mariadb -N -B  (stdin: DROP DATABASE IF EXISTS `tp_m9a_example_com_c2b3ff`;
CREATE DATABASE `tp_m9a_example_com_c2b3ff` CHARACTER SET utf8mb4;)
    $ mariadb --database=tp_m9a_example_com_c2b3ff --abort-source-on-error -e 'source /var/lib/tpan/restore/m9a_example_com_c2b3ff/db.sql'
```

Bước 4 hiện `[sẽ chạy]`. Nghe hiển nhiên, nhưng bản mã đầu tiên in bước này là "đã đúng, bỏ qua" (mục 5, chỗ sai 2). Tên file của bước 1 là `<giờ lúc chạy>` vì giờ chỉ được chọn lúc Apply (mục 3.3).

```bash
scripts/vm.sh root tpan backup restore m9a.example.com --yes
```

Output rút gọn (M9.log dòng 750 tới 783):

```
Bắt đầu job e19af6e5419c0106
- Backup site m9a.example.com vào /var/lib/tpan/backups/m9a.example.com ...
  xong
- Giải nén /var/lib/tpan/backups/m9a.example.com/m9a.example.com-20261119-181505.tar.gz vào /var/lib/tpan/restore/m9a_example_com_c2b3ff ...
  xong
- Cất mã nguồn hiện tại của m9a.example.com sang /var/lib/tpan/restore/m9a_example_com_c2b3ff/old-public ...
  xong
- Đưa mã nguồn từ bản backup vào /var/www/m9a.example.com/public ...
  xong
- Xuất DB tp_m9a_example_com_c2b3ff ra /var/lib/tpan/restore/m9a_example_com_c2b3ff/db-before.sql ...
  xong
- Nhập /var/lib/tpan/restore/m9a_example_com_c2b3ff/db.sql vào DB tp_m9a_example_com_c2b3ff ...
  xong
  ...
Job e19af6e5419c0106: thành công
Kiểm chứng:
  ...
  [đạt] wp-config.php có quyền 0600 và đúng chủ: wp-config.php quyền 0600, chủ tp_m9a_example_com_c2b3ff
  [đạt] User DB của site đăng nhập được, chỉ thấy DB của site: User DB đăng nhập được và chỉ thấy DB tp_m9a_example_com_c2b3ff
  [đạt] Gọi HTTP vào site trả 200 và đúng là WordPress của site: Site trả 200, đúng là WordPress của m9a.example.com
```

Kiểm chứng là đủ bộ phép kiểm của lúc tạo site (`siteChecks`), kể cả hai phép cô lập giữa `m9a` và `m9b`. Job mất 8,0 giây, nhưng có 4314 sự kiện và 523808 byte trong `job_events`, trong đó 4270 là sự kiện `output` (M9.log dòng 786 tới 846). Gần như toàn bộ là output của `tar --list --verbose`: mỗi mục trong bản backup một dòng. Đây là một chỗ M9.md ghi là "nâng cấp về sau".

Kiểm kết quả, chạy trên Mac:

```bash
scripts/vm.sh exec curl -sS -m 20 --resolve m9a.example.com:80:127.0.0.1 http://m9a.example.com/e2e-m9.txt
scripts/vm.sh exec curl -sS -m 20 -o /dev/null -w '%{http_code}\n' --resolve m9a.example.com:80:127.0.0.1 http://m9a.example.com/e2e-m9-moi.txt
scripts/vm.sh root mariadb -N -B tp_m9a_example_com_c2b3ff -e "SELECT post_title FROM wp_posts WHERE ID = 1"
scripts/vm.sh root bash -c 'cd "$1" && find . -type f -print0 | sort -z | xargs -0 sha256sum | sha256sum | cut -c1-64; find . -type f | wc -l' _ /var/www/m9a.example.com/public
scripts/vm.sh root tpan backup list m9a.example.com --json
scripts/vm.sh root bash -c 'ls -la /var/lib/tpan/restore 2>&1; ls -d "/var/lib/tpan/restore/$1" 2>&1' _ m9a_example_com_c2b3ff
scripts/vm.sh root bash -c 'cd "$1" && find . -printf "%m %u:%g %y %p\n" | sort | sha256sum | cut -c1-64; find . -printf "%m %u:%g %y\n" | sort | uniq -c' _ /var/www/m9a.example.com/public
```

(Kịch bản viết hai lệnh `find` thành nhiều dòng và in `backup list` kèm một dòng tóm tắt mỗi bản; mình gộp lại cho gọn, không đổi lệnh.) Output (M9.log dòng 849 tới 964, rút gọn):

```
e2e-m9-truoc-backup-1795112104
404
e2e-m9 tieu de luc backup
d41ed1bda3ea15183d1ebfe01ee7b2a07e7a492edc8fe2277fda50383cb3793e
3785
...
1263b6cd5eb09f2b m9a.example.com full before-restore 35623972 07534c7da72054464c535724a95421d4b80996709950c7ea870d1e34d3ed8529 /var/lib/tpan/backups/m9a.example.com/m9a.example.com-20261119-181510.tar.gz
fd1eacba34104f63 m9a.example.com full manual 35623411 3ac52b242818e0c70d2cff092fbdddd4eedf8a581a1ab807ea3b55040bfba05d /var/lib/tpan/backups/m9a.example.com/m9a.example.com-20261119-181505.tar.gz
...
ls: cannot access '/var/lib/tpan/restore/m9a_example_com_c2b3ff': No such file or directory
...
29d2514bbbee5c9cbfcc61693e070fabebc5791efc92e3c7b1018731b1184740
    471 2750 tp_m9a_example_com_c2b3ff:www-data d
      1 600 tp_m9a_example_com_c2b3ff:www-data f
   3784 640 tp_m9a_example_com_c2b3ff:www-data f
```

Đọc từng dòng: file dấu về nội dung lúc backup; file tạo sau backup không còn (404); DB về tiêu đề lúc backup; mã băm nội dung cây `public` giống hệt mục 2.5 (`d41ed1...`); có thêm bản `before-restore` (bản tự backup ở bước 1); thư mục khôi phục đã được gỡ; và mã băm của quyền, chủ từng mục cũng giống hệt (`29d251...`). Dòng cuối này là dòng đã trượt ở lượt đầu (mục 5, chỗ sai 1).

### 2.7 Chặn đầy đĩa

Kịch bản lấp đĩa bằng `scripts/faults/06-disk-full.sh` của M4 rồi thử backup và xuất DB (M9.log dòng 1014 tới 1052):

```bash
scripts/faults/06-disk-full.sh break
scripts/vm.sh root tpan backup create m9a.example.com --yes
scripts/vm.sh root tpan db dump m9a.example.com --yes --json
```

```
Đã tạo /var/tmp/tpan-fault-fill (14895353529 byte).
Filesystem      Size  Used Avail Use% Mounted on
/dev/sda1        19G   17G  1.7G  91% /
...
Từ chối: không đủ chỗ trống để backup m9a.example.com: cần khoảng 105.8 MiB (mã nguồn và DB của site), đĩa còn trống 1.6 GiB trên tổng 18.3 GiB. Luật: chỗ trống phải ít nhất gấp 1,2 lần chỗ cần và sau khi ghi vẫn còn 10% đĩa. Xem bản cũ bằng: sudo tpan backup list m9a.example.com, và xem đĩa bằng: df -h /var/lib/tpan/backups
...
    "code": "disk_space_low",
    "message": {
      "vi": "không đủ chỗ trống để xuất DB tp_m9a_example_com_c2b3ff: cần khoảng 752.0 KiB, đĩa còn trống 1.6 GiB trên tổng 18.3 GiB. ...
```

Cả hai thoát 1. Sau đó (dòng 1055 tới 1094) thư mục backup vẫn đúng hai file cũ, 0 file `.part`, bảng `backups` vẫn 2 dòng.

Tự tính xem vế nào của luật chặn. Tổng đĩa 18,3 GiB, nên 10% là khoảng 1,83 GiB. Còn trống 1,6 GiB, trừ đi 105,8 MiB thì còn khoảng 1,5 GiB, nhỏ hơn 1,83 GiB: vế 10% từ chối. Vế 1,2 lần thì qua (cần 105,8 MiB × 1,2 = 127 MiB, có 1,6 GiB). Với bản xuất DB 752 KiB cũng vậy. Ở mức 91% đầy, vế 10% gần như luôn là vế quyết định.

Chú ý thêm: "cần khoảng 105.8 MiB" là cỡ chưa nén (`du -sb` của `public` cộng cỡ DB trong `information_schema`), trong khi file `.tar.gz` thật chỉ 35,6 MB. Luật cố ý ước lượng dư (mục 3.2).

### 2.8 Đổi mật khẩu DB trong lúc site đang được gọi

Kịch bản chạy một vòng `curl` mỗi 0,1 giây trong máy ảo (dòng 1120), rồi đổi mật khẩu:

```bash
scripts/vm.sh root systemd-run --unit=tpan-e2e-curl bash -c 'while :; do printf "%s %s\n" "$(date +%s.%N)" "$(curl -s -m 5 -o /dev/null -w "%{http_code}" --resolve "$1:80:127.0.0.1" "http://$1/")" >>/root/e2e-curl.log; sleep 0.1; done' _ m9a.example.com
scripts/vm.sh root tpan db passwd m9a.example.com --yes
scripts/vm.sh root systemctl stop tpan-e2e-curl
scripts/vm.sh root bash -c 'echo "số request: $(wc -l </root/e2e-curl.log)"; echo "theo mã:"; awk "{print \$2}" /root/e2e-curl.log | sort | uniq -c; echo "các request khác 200 (giờ, mã):"; awk "\$2 != 200" /root/e2e-curl.log | head -20'
```

Output (M9.log dòng 1124 tới 1154, rút gọn):

```
Bắt đầu job 711e69fc3b7052c4
- Đổi mật khẩu user DB tp_m9a_example_com_c2b3ff của site m9a.example.com ...
  xong
  [đạt] User DB của site đăng nhập được, chỉ thấy DB của site
  [đạt] Gọi HTTP vào site trả 200 và đúng là WordPress của site

Job 711e69fc3b7052c4: thành công
...
số request: 37
theo mã:
     34 200
      3 500
các request khác 200 (giờ, mã):
1795112123.720120213 500
1795112123.859200857 500
1795112123.983471705 500
```

Ba lần 500 nằm trong 0,26 giây, ở giây 18:15:23 UTC. Job chạy từ 18:15:23.594 tới 18:15:24.153 (dòng 1157 tới 1196). Vậy cả ba đều nằm giữa `ALTER USER` và `wp config set`, lúc MariaDB đã nhận mật khẩu mới mà `wp-config.php` còn mật khẩu cũ. Sau khi job xong không còn lần 500 nào.

Sự kiện của job chỉ có hai lệnh (dòng 1192 tới 1195):

```
  command mariadb.db-password:m9a.example.com mariadb -N -B
  command mariadb.db-password:m9a.example.com runuser -u tp_m9a_example_com_c2b3ff -- wp config set DB_PASSWORD --prompt --type=constant --quiet --path=/var/www/m9a.example.com/public
```

Không lệnh nào mang mật khẩu. Kịch bản đọc mật khẩu mới rồi tìm nó ở mọi chỗ có thể lọt (dòng 1198 tới 1205):

```bash
scripts/vm.sh root bash -c 'cd / && pw="$(runuser -u "$2" -- wp config get DB_PASSWORD --path="/var/www/$1/public")"; for f in /var/lib/tpan/tpan.db /var/lib/tpan/tpan.db-wal /var/lib/tpan/tpan.db-shm; do if [ -e "$f" ]; then echo "$f: $(grep -caF -- "$pw" "$f") dòng chứa mật khẩu mới"; else echo "$f: không có file"; fi; done; journalctl --no-pager -o cat --since "-10 min" | grep -cF -- "$pw" | sed "s/^/journal: /"; stat -c "%a %U:%G %n" "/var/www/$1/public/wp-config.php"; MYSQL_PWD="$pw" mariadb -u "$2" -N -e "SELECT CURRENT_USER()"' _ m9a.example.com tp_m9a_example_com_c2b3ff
```

Lệnh này của kịch bản cố ý đọc mật khẩu ra một biến trong máy ảo để dò; nó không in mật khẩu ra.

```
/var/lib/tpan/tpan.db: 0 dòng chứa mật khẩu mới
/var/lib/tpan/tpan.db-wal: 0 dòng chứa mật khẩu mới
/var/lib/tpan/tpan.db-shm: 0 dòng chứa mật khẩu mới
journal: 0
600 tp_m9a_example_com_c2b3ff:www-data /var/www/m9a.example.com/public/wp-config.php
tp_m9a_example_com_c2b3ff@localhost
```

Dòng cuối: user DB đăng nhập được bằng mật khẩu mới.

### 2.9 Xuất DB, sửa DB, nhập lại

```bash
scripts/vm.sh root tpan db dump m9a.example.com --yes
scripts/vm.sh root mariadb -N -B tp_m9a_example_com_c2b3ff -e "UPDATE wp_posts SET post_title = 'e2e-m9 sua sau dump' WHERE ID = 1; INSERT INTO wp_options (option_name, option_value, autoload) VALUES ('e2e_m9_sau_dump', '1', 'no'); SELECT post_title FROM wp_posts WHERE ID = 1"
scripts/vm.sh root tpan db import m9a.example.com --id 1684526a4b33c72d --yes
scripts/vm.sh root mariadb -N -B tp_m9a_example_com_c2b3ff -e "SELECT post_title FROM wp_posts WHERE ID = 1; SELECT COUNT(*) FROM wp_options WHERE option_name = 'e2e_m9_sau_dump'"
```

Output rút gọn (M9.log dòng 1213 tới 1348):

```
- Xuất DB tp_m9a_example_com_c2b3ff ra /var/lib/tpan/backups/m9a.example.com/m9a.example.com-db-20261119-181528.sql ...
  xong
...
600 root:root /var/lib/tpan/backups/m9a.example.com/m9a.example.com-db-20261119-181528.sql
-- Dump completed on 2026-11-20  1:15:28
e2e-m9 sua sau dump
Bắt đầu job d2303a46dee00e6c
- Xuất DB tp_m9a_example_com_c2b3ff ra /var/lib/tpan/backups/m9a.example.com/m9a.example.com-db-20261119-181528-before-import.sql ...
  xong
- Nhập /var/lib/tpan/backups/m9a.example.com/m9a.example.com-db-20261119-181528.sql vào DB tp_m9a_example_com_c2b3ff ...
  xong
  [đạt] User DB của site đăng nhập được, chỉ thấy DB của site
  [đạt] Gọi HTTP vào site trả 200 và đúng là WordPress của site
...
e2e-m9 tieu de luc backup
0
```

Bản xuất và bản tự xuất trước khi nhập cùng ra trong giây 18:15:28. Hai tên không trùng nhờ đuôi `-before-import` (mục 3.13). Sau khi nhập, tiêu đề về như lúc xuất và dòng `wp_options` thêm sau đó không còn (`0`): nhập xoá rồi tạo lại cả DB, nên bảng hay dòng thêm sau lúc xuất đều mất. `backup list` có thêm một dòng `sql before-import` (dòng 1351 tới 1402).

### 2.10 Thùng rác, cron bị bỏ qua, xoá hẳn tự backup

```bash
scripts/vm.sh root tpan site rm m9b.example.com --yes
scripts/vm.sh root ls -la /etc/cron.d/tpan-m9b_example_com_36b746
scripts/vm.sh root tpan cron run m9b.example.com
scripts/vm.sh root tpan cron list --json
```

Output rút gọn (M9.log dòng 1430 tới 1500):

```
- Khoá user Linux tp_m9b_example_com_36b746 của m9b.example.com ...
  xong
- Gỡ lịch cron WordPress của m9b.example.com ...
  xong
- Chuyển thư mục của m9b.example.com vào thùng rác ...
  xong
...
ls: cannot access '/etc/cron.d/tpan-m9b_example_com_36b746': No such file or directory
...
      "domain": "m9b.example.com",
      "file": "/etc/cron.d/tpan-m9b_example_com_36b746",
      "schedule": "*/5 * * * *",
      "command": "/usr/local/bin/tpan cron run m9b.example.com",
      "enabled": false,
      "last": {
        "domain": "m9b.example.com",
        "started": "2026-11-19T18:15:30.008030058Z",
        "duration_ns": 0,
        "status": "skipped",
        "exit_code": 0,
        "output": "site m9b.example.com không active (trashed), bỏ qua"
```

`tpan cron run` cho site trong thùng rác thoát 0, không in gì, và ghi `skipped`. Backup một site trong thùng rác vẫn được (nguồn là thư mục trong thùng rác; dòng 1504 tới 1536 có bản `full manual` với `wp-config.php` và `db.sql` bên trong, không có `wp_version` vì `wp` không chạy được ở đó).

Xoá hẳn (M9.log dòng 1541 tới 1673, rút gọn):

```bash
scripts/vm.sh root tpan trash purge m9b.example.com --yes
```

```
Bắt đầu job 55373c715ce5dc0a
- Tự backup m9b.example.com trước khi xoá hẳn, vào /var/lib/tpan/backups/m9b.example.com ...
  xong
- Tắt vhost của m9b.example.com ...
  đã đúng, bỏ qua
- Gỡ file vhost của m9b.example.com ...
  xong
- Gỡ pool PHP-FPM tpan-m9b_example_com_36b746 ...
  đã đúng, bỏ qua
- Gỡ lịch cron WordPress của m9b.example.com ...
  đã đúng, bỏ qua
- Xoá user DB và DB tp_m9b_example_com_36b746 của site m9b.example.com ...
  xong
...
ed17122d6471e154 m9b.example.com full before-purge 35623258 d10e8a32ad4661caf2bb45f5723148b6106847df461868fea68a50a815da58b7 /var/lib/tpan/backups/m9b.example.com/m9b.example.com-20261119-181533.tar.gz
...
không có user tp_m9b_example_com_36b746
0
  => ĐẠT: file before-purge còn, sha256 khớp
  => ĐẠT: file cron không còn
  => ĐẠT: site đã xoá hẳn: không thư mục, không user, không DB
```

Bước tự backup đứng đầu Plan xoá hẳn. Site đã mất hẳn (không thư mục, không user, `0` DB), nhưng dòng `before-purge` vẫn còn trong `backup list`: bảng `backups` không có khoá ngoại tới bảng `sites`.

### 2.11 Nâng MariaDB đòi backup, `backup create --all`, bản sao `tpan.db`

Kịch bản tạo site thứ ba `m9c.example.com` chưa backup lần nào, rồi (M9.log dòng 1836 tới 1953, rút gọn):

```bash
scripts/vm.sh root tpan mariadb upgrade --to 11.8 --dry-run
scripts/vm.sh root tpan backup create --all --yes
scripts/vm.sh root tpan mariadb upgrade --to 11.8 --dry-run
```

```
Từ chối: nâng MariaDB là một chiều, mà các site sau chưa có bản backup đầy đủ trong 24 giờ qua: m9c.example.com. Backup trước bằng: sudo tpan backup create --all --yes, rồi chạy lại.
...
Bắt đầu job 0352572975f40531
- Backup site m9a.example.com vào /var/lib/tpan/backups/m9a.example.com ...
  xong
Bắt đầu job 6520e357659c1080
- Backup site m9c.example.com vào /var/lib/tpan/backups/m9c.example.com ...
  xong
...
Bản sao tpan.db: /var/lib/tpan/backups/_tpan/tpan-20261119-181546.db
...
Kế hoạch: Nâng MariaDB từ dòng 10.11 lên 11.8. Một chiều, không tự hoàn tác: đường lùi là cài lại dòng 10.11 rồi nhập lại /var/lib/tpan/backups/mariadb-upgrade-20261120-011547.sql
```

Rồi kiểm bản sao `tpan.db` và health (M9.log dòng 1927 tới 1953; `tpan health --json` được kịch bản lọc qua một đoạn `python3`, mình chép dòng của `tpan.backup:db`):

```bash
scripts/vm.sh root bash -c 'stat -c "%a %U:%G %s %n" /var/lib/tpan/backups/_tpan /var/lib/tpan/backups/_tpan/*'
scripts/vm.sh root bash -c 'f="$(ls /var/lib/tpan/backups/_tpan/tpan-*.db | tail -1)"; python3 -c "import sqlite3,sys; c=sqlite3.connect(\"file:\"+sys.argv[1]+\"?mode=ro\", uri=True); print(\"integrity:\", c.execute(\"PRAGMA integrity_check\").fetchone()[0]); print(\"sites:\", c.execute(\"SELECT COUNT(*) FROM sites\").fetchone()[0]); print(\"backups:\", c.execute(\"SELECT COUNT(*) FROM backups\").fetchone()[0])" "$f"'
scripts/vm.sh root tpan health --json
```

```
700 root:root 4096 /var/lib/tpan/backups/_tpan
600 root:root 3379200 /var/lib/tpan/backups/_tpan/tpan-20261119-181546.db
integrity: ok
sites: 2
backups: 8
tpan.backup:db = pass: Bản sao lưu tpan.db mới nhất: tpan-20261119-181546.db
```

Lời từ chối chỉ nêu `m9c`, không nêu `m9a` (đã có bản trong 24 giờ). Từ chối xảy ra cả với `--dry-run`. `--all` chạy mỗi site một job. Bản sao `tpan.db` đọc được, `integrity_check` là `ok`, và phép kiểm `tpan.backup:db` chuyển từ Warn (lúc đầu, dòng 315 tới 333: `chưa có bản sao lưu tpan.db nào`) sang Pass.

Hai cách đặt giờ trong tên file nằm cạnh nhau ở đây: `tpan-20261119-181546.db` là UTC, còn `mariadb-upgrade-20261120-011547.sql` (của M12) là giờ máy. M9.md ghi đây là điểm chưa thống nhất.

### 2.12 Lịch backup hằng ngày

```bash
scripts/vm.sh root tpan backup settings --at 03:30 --yes
scripts/vm.sh root cat /etc/cron.d/tpan-backup
scripts/vm.sh root tpan backup settings --at 03:30 --yes
scripts/vm.sh root tpan backup settings --off --yes
```

Output rút gọn (M9.log dòng 1961 tới 2073):

```
- Đặt lịch backup hằng ngày lúc 03:30 ...
  xong
...
30 03 * * * root /usr/local/bin/tpan backup create --all --scheduled --yes >/dev/null 2>&1
...
- Đặt lịch backup hằng ngày lúc 03:30 ...
  đã đúng, bỏ qua
...
- Gỡ lịch backup hằng ngày /etc/cron.d/tpan-backup ...
  xong
```

Chạy lại lần hai thì "đã đúng, bỏ qua", sha256 của file không đổi. Lịch chưa bao giờ thật sự chạy lúc 03:30 trên máy ảo (mục 2.16).

### 2.13 Mười lần `tpan cron run` cùng lúc với `tpan serve`

Sau khi `tpan setup` đã bật `tpan.service`, kịch bản chạy 5 lần cho `m9a` và 5 lần cho `m9c` song song (M9.log dòng 2450 tới 2477):

```
a1 mã 0
...
c5 mã 0
dòng locked|busy trong journal của tpan: 0
```

Cả 10 thoát 0, không có `busy` hay `locked`. Lúc đó `tpan serve` gần như rảnh, nên chưa chứng minh được gì cho lúc serve ghi nhiều.

### 2.14 Hồi quy M6 và M12

- `docs/bang-chung/M9-hoi-quy-m6.log`: kịch bản của M6 trên tpan M9, `167 khẳng định đạt, 0 khẳng định không đạt` (dòng 3917). Xem trước 10 site giờ là `153 bước sẽ chạy` (dòng 255), thay vì 133 ở M6: thêm đúng 10 × 2 bước cron. Job 10 site mất 37,1 giây (dòng 1287), ở M6 là 28,9 giây; mình chưa đo phần chênh do đâu. Khôi phục từ thùng rác ghi lại file cron (`Đặt lịch cron WordPress của s2.example.com ...` rồi `xong`, dòng 2162 tới 2163). Lượt Apache của kịch bản này cũng tạo 3 site bằng Plan có bước cron.
- `docs/bang-chung/M9-hoi-quy-m12.log`: `175 khẳng định đạt, 0 khẳng định không đạt` (dòng cuối). Kịch bản M12 phải thêm `tpan backup create --all --yes` trước khi nâng MariaDB (dòng 4410 tới 4430), vì luật `backup_required` của M9.

### 2.15 Unit test, chạy trên Mac lúc viết tài liệu này

Vài đường mã không có trên máy ảo (khôi phục mà bước cuối lỗi, kiểm chứng phải thử lại, cron với site không có) chỉ có unit test. Mình chạy lại trên Mac:

```bash
go test ./internal/steps/ -run 'TestSpaceOK|TestRestoreChain|TestParseTarLine' -v
go test ./internal/core/ -run 'TestBackupRestoreFailUndo|TestDBPasswordVerifyRetries|TestCronRunSkipsAndRefuses' -v
```

```
=== RUN   TestSpaceOK
--- PASS: TestSpaceOK (0.00s)
=== RUN   TestParseTarLine
--- PASS: TestParseTarLine (0.00s)
=== RUN   TestRestoreChain
=== RUN   TestRestoreChain/thành_công:_site_về_bản_backup,_DB_về_bản_backup,_wp-config_giữ_mật_khẩu_mới
=== RUN   TestRestoreChain/bước_cuối_lỗi:_engine_hoàn_tác,_máy_về_y_như_trước
--- PASS: TestRestoreChain (0.00s)
    --- PASS: TestRestoreChain/thành_công:_site_về_bản_backup,_DB_về_bản_backup,_wp-config_giữ_mật_khẩu_mới (0.00s)
    --- PASS: TestRestoreChain/bước_cuối_lỗi:_engine_hoàn_tác,_máy_về_y_như_trước (0.00s)
PASS
ok  	github.com/doxuta/tpan/internal/steps	0.441s
=== RUN   TestBackupRestoreFailUndo
--- PASS: TestBackupRestoreFailUndo (0.02s)
=== RUN   TestCronRunSkipsAndRefuses
--- PASS: TestCronRunSkipsAndRefuses (0.02s)
=== RUN   TestDBPasswordVerifyRetries
--- PASS: TestDBPasswordVerifyRetries (0.05s)
PASS
ok  	github.com/doxuta/tpan/internal/core	0.503s
```

Unit test chạy trên `FakeSystem` (máy giả trong bộ nhớ), không chứng minh được hành vi của tar hay MariaDB thật.

### 2.16 Chưa chạy

Theo `docs/thay-doi/M9.md` mục "Chưa kiểm" và theo log, những việc dưới đây chưa có output thật:

- e2e M9 với Apache (chỉ có lượt Apache của hồi quy M6, không có backup hay khôi phục).
- Lịch backup chạy thật lúc 03:30 qua cron.
- GNU tar thoát mã 1 ("file changed as we read it").
- Khôi phục mà kiểm chứng sau đó lỗi trên máy ảo (engine trả `old-public` về chỗ cũ): chỉ có `TestRestoreChain` và `TestBackupRestoreFailUndo` ở trên.
- `DBImport` gặp lỗi giữa file trên máy ảo; lời từ chối `restore_dir_exists`, `backup_corrupt`, `backup_not_found`, `backup_exists` trên máy ảo.
- Tạo lại site cùng domain rồi khôi phục bản `before-purge`.
- Site có symlink trong `public` (site trên máy ảo có 0 symlink).
- SQLite khi `tpan serve` đang ghi nhiều cùng lúc với nhiều `tpan cron run`.
- Thời gian `sha256sum` khi dựng Plan khôi phục với site vài GB.
- Giao diện (màn Backup, màn Cron, ba việc ở màn Cơ sở dữ liệu): chưa ai nhìn bằng mắt.
- Mọi lệnh "tự xem" ở mục 4 và các lệnh trong đáp án mục 6 có ghi "chưa chạy".
- Hai kết quả kiểm `--prompt=value` (thoát 1) và `source` không có `--abort-source-on-error` (thoát 0) đã chạy trên máy ảo theo `docs/thay-doi/M9.md`, nhưng log của lần đó không được lưu trong repo.

## 3. Đi qua mã theo thứ tự chạy

Ba luồng: backup rồi khôi phục (3.1 tới 3.10), thao tác DB (3.11 tới 3.13), cron (3.14 tới 3.18). Trước hết là một khuôn chung.

### 3.0 Khuôn chung: `Planned` và `after`

Mọi thao tác có thay đổi của M9 trả về `*core.Planned` (từ M12, `internal/core/types_m12.go`). `Planned.Execute` chạy Plan qua engine, rồi mới gọi `after`:

```go
rep, err := engine.New(p.sys, engine.Options{Journal: p.journal, OnEvent: onEvent}).Execute(ctx, p.Plan)
if err != nil {
	return rep, fmt.Errorf("chạy %s: %w", p.Plan.Kind, err)
}
if rep.Job.Status != engine.JobSucceeded || p.after == nil {
	return rep, nil
}
if err := p.after(ctx, rep); err != nil {
	return rep, fmt.Errorf("job %s xong nhưng chưa ghi được kết quả: %w", rep.Job.ID, err)
}
```

Mọi Plan của M9 dựng qua `Planned` (backup, khôi phục, cài đặt lịch, ba thao tác DB) ghi bảng `backups` trong `after`. Vì vậy bảng không có dòng trỏ tới một file mà job đã hoàn tác (chú thích đầu `internal/core/backup.go`). Ngoại lệ là xoá hẳn: `SitePurge.Execute` (`internal/core/trash.go`) tự ghi dòng `before-purge`, và ghi cả khi job hỏng mà file backup được giữ lại (mục 3.10). Phía CLI, mọi lệnh đi qua `runPlanned` (M12): xem trước, hỏi "Chạy?", chạy có tiến độ, ghi nhật ký.

### 3.1 `tpan backup create`: `cmd/tpan/backup.go`, `backupCreateCmd`

Lệnh nhận đúng một trong hai: domain, hoặc `--all`. Dòng kiểm đầu tiên viết gọn:

```go
if all == (len(args) == 1) || (scheduled && !all) {
	return fail(e, f.asJSON, &core.RefusedError{Code: "args_invalid", ...
```

`all == (len(args) == 1)` đúng khi có cả hai hoặc không có cái nào. Một site thì: đòi root (trừ `--dry-run`), `openBackups`, `b.PrepareCreate(ctx, d, core.ReasonManual)`, rồi `runPlanned`. `--all` đi sang `backupCreateAll`: dựng Plan từng site để xem trước, hỏi một lần cho cả lô, rồi gọi `core.Backups.CreateAll`.

### 3.2 `internal/core/backup.go`: `PrepareCreate` và chặn đầy đĩa lần một

```go
l := st.Layout()
src := l.Dir
if st.Status == site.Trashed {
	src = site.TrashDir(l)
}
what := i18n.T("backup "+d.String(), "back up "+d.String())
if err := b.backupSpace(ctx, l, src, what); err != nil {
	return nil, err
}
a := &steps.BackupArchive{Layout: l, Src: src, Dir: site.BackupDir(l), PHPVersion: sitePHP(st, cfg)}
```

Site trong thùng rác thì nguồn là thư mục trong thùng rác (mục 2.10). `backupSpace` gọi `steps.MeasureBackup` rồi `steps.SpaceOK`; không đủ chỗ thì từ chối `disk_space_low` bằng `diskLow`, câu có số đo và lệnh để xem đĩa (mục 2.7). Từ chối ở đây, trước khi dựng Plan, để người bấm biết ngay.

`MeasureBackup` (`internal/steps/backup.go`) tính "cần" = `du -sb --one-file-system <src>/public` + `MeasureDB` (tổng `DATA_LENGTH + INDEX_LENGTH` trong `information_schema`). "Còn trống" và "tổng" lấy bằng `StatFS` ở thư mục backup; lần backup đầu thư mục chưa có, nên `statFSNear` đi ngược lên thư mục cha gần nhất đã có. `du` dùng `--one-file-system` vì tar cũng dùng cờ đó: thứ gì được mount vào giữa site thì không nằm trong backup, nên cũng không tính.

### 3.3 `internal/steps/backup.go`: `BackupArchive.Apply`

Bước này đo lại chỗ trống (Plan chờ duyệt qua MCP có thể chạy sau đó hàng giờ), chọn giờ, rồi làm theo thứ tự: xuất DB vào thư mục tạm `.tmp-<giờ>`, băm `db.sql`, chạy `wp core version` dưới user của site, ghi `manifest.json`, nén, đổi tên, băm file nén. Phần nén:

```go
// .part tạo trước với 0600: tar mở file có sẵn để ghi đè nên giữ quyền
// này; tên thật chỉ xuất hiện khi file đã trọn vẹn.
if err := sys.WriteFileAtomic(part, nil, 0o600, system.Root); err != nil {
	return fail(fmt.Errorf("tạo %s: %w", part, err))
}
if res, err := sys.Run(ctx, tarCreate(part, tmp, s.Src)); err != nil {
	if ee, ok := isExit(err); !ok || ee.Code != 1 {
		return fail(withStdout(fmt.Errorf("nén backup %s: %w", part, err), res))
	}
}
if err := setPermOwner(sys, part, 0o600, system.Root); err != nil {
	return fail(err)
}
if err := sys.Rename(part, final); err != nil {
```

Ba ý. Một: file `.part` có trước với quyền 0600, nên không có lúc nào file chứa dữ liệu mà người khác đọc được. Hai: tar thoát 1 nghĩa là một file đổi trong lúc đọc (WordPress đang chạy); bản backup vẫn dùng được nên chấp nhận (chưa gặp trên máy ảo). Ba: tên thật chỉ xuất hiện sau `rename`, nên ai thấy file `<domain>-<giờ>.tar.gz` là thấy một file trọn vẹn.

`tarCreate` dùng hai `--directory` theo thứ tự: cái đầu áp cho `manifest.json db.sql` trong thư mục tạm, cái sau áp cho `public` trong thư mục site. Kết quả là một file có đúng ba thứ ở gốc (mục 2.5). Không có `-h`, nên symlink trong site được lưu là symlink (mục 4.1).

Vì sao giờ chọn lúc Apply chứ không lúc dựng Plan: Plan qua Planner (MCP) được dựng hai lần, lúc đề xuất và lúc duyệt, rồi so dấu vân tay. Tên file theo giờ dựng Plan thì hai lần không bao giờ khớp. `Info()` vì vậy in `<giờ lúc chạy>` (mục 2.6). `Check` chỉ trả `true` sau Apply của chính bước đó: backup luôn là một bản mới, "đã có một file backup nào đó" không phải trạng thái đích. `Undo` xoá file vừa tạo.

### 3.4 Sau job: `archived`, `AddBackup`, `prune`

`after` của `PrepareCreate` đọc kết quả từ chính bước (`a.Path`, `a.SHA256`, `a.Size`, `a.WPVersion`) qua `archived`, ghi một dòng bằng `Store.AddBackup` (`internal/store/backup.go`, id là 16 ký tự hex từ `crypto/rand`), rồi gọi `prune`:

```go
for _, r := range rows {
	if r.Domain != domain || r.Reason == ReasonBeforePurge {
		continue
	}
	seen[r.Kind]++
	if seen[r.Kind] <= set.Keep || slices.Contains(protect, r.ID) {
		continue
	}
	if err := b.removeBackupFile(r.Path); err != nil {
```

Đếm riêng bản `full` và bản `sql`, mỗi loại giữ `Keep` bản mới nhất. Bản `before-purge` không bao giờ bị dọn và không chiếm chỗ trong `Keep`: đó là đường lùi duy nhất của một site đã xoá hẳn. Xoá file trước, xoá dòng sau: lỗi giữa chừng thì còn một dòng trỏ tới file đã mất (lần sau dọn tiếp), không bao giờ còn file mồ côi. `removeBackupFile` chỉ xoá đường dẫn nằm hẳn dưới `/var/lib/tpan/backups`: tpan chạy bằng root, dòng bị sửa tay trỏ ra `/etc` thì dừng.

### 3.5 `PrepareRestore`: chọn bản, kiểm bản, dựng sáu bước

`cmd/tpan/backup.go`, `backupRestoreCmd` gọi `b.PrepareRestore(ctx, d, id)`. Hàm này từ chối sớm theo thứ tự: site trong thùng rác (`site_trashed`), không tìm được bản (`restoreSource`, `backup_not_found`; `--id` trống là bản full mới nhất), file hỏng (`checkIntact`: đường dẫn phải nằm dưới thư mục backup của site, là file thường, sha256 phải khớp dòng; không thì `backup_corrupt`), thư mục khôi phục còn sót (`restore_dir_exists`), không đủ chỗ cho bản tự backup (`disk_space_low`). Rồi dựng Plan:

```go
Steps: []engine.Step{
	pre,
	&steps.BackupExtract{Layout: l, Archive: src.Path, SHA256: src.SHA256, Dir: rd},
	setAside,
	moveIn,
	&steps.MariaDBDump{DB: l.DB, Path: rd + "/db-before.sql"},
	&steps.DBImport{Layout: l, File: rd + "/db.sql", UndoFile: rd + "/db-before.sql"},
},
Verify: siteChecks(cfg.Web, php, l, others, st.SSL),
```

`pre` là một `BackupArchive` của hiện trạng. `setAside` là `MoveTree` từ `l.Public` sang `<rd>/old-public`. `moveIn` là `MoveTree` từ `<rd>/public` về `l.Public`, bọc trong `afterAside` (mục 3.7). `rd` là `site.RestoreDir(l)`, tức `/var/lib/tpan/restore/<slug>`.

Thư mục khôi phục còn sót thì từ chối, không tự xoá: nó có thể chứa `old-public` của một lần khôi phục hỏng giữa chừng, tức bản duy nhất còn lại của mã nguồn cũ. Lời từ chối có lệnh `sudo rm -r --one-file-system -- <rd>` để người dùng tự xoá sau khi đã xem.

### 3.6 `BackupExtract.Apply`: kiểm trước, giải nén vào chỗ kín, chuẩn hoá

Thứ tự trong hàm: kiểm file là file thường, sha256 lại một lần nữa (lúc dựng Plan đã kiểm một lần, giờ kiểm lại lúc chạy), đọc danh sách thành viên bằng `tar --list --verbose` rồi đưa cho `checkTarList`, tạo `rd` (root 0700), giải nén, kiểm `public` là thư mục và hai file kia là file thường, đọc `manifest.json` và đòi `domain` đúng site, rồi chuẩn hoá chủ và quyền.

`checkTarList` là lớp chặn thứ hai sau chính GNU tar. Nó chỉ nhận đúng dạng tpan tạo ra:

```go
switch {
case m.name == "manifest.json" || m.name == "db.sql":
	if m.typ != '-' {
		return fmt.Errorf("%s phải là file thường", m.name)
	}
case m.name == "public/":
	if m.typ != 'd' {
		return fmt.Errorf("public/ phải là thư mục")
	}
case strings.HasPrefix(m.name, "public/"):
default:
	return fmt.Errorf("thành viên %q nằm ngoài manifest.json, db.sql, public/", m.name)
}
```

Thêm vào đó: không tên tuyệt đối, không thành phần `..`, không tên lặp, không thiết bị hay FIFO, link cứng phải trỏ vào trong `public/`. `parseTarLine` đọc từng dòng dạng GNU tar 1.35; dòng không đúng dạng thì từ chối, không đoán.

Giải nén có `--no-same-owner --no-same-permissions`: root mặc định lấy chủ và quyền từ header của tar, tức là tin file backup. Sau đó tpan tự đặt lại (`extractChown`, `extractChmod`): `chown -R -h tp_<slug>:www-data`, thư mục 2750, file 0640. `find -P ... -type d|f` không đi theo symlink và bỏ qua symlink.

Cuối cùng là `wp-config.php`. Bản trong backup có thể mang mật khẩu DB cũ (mật khẩu có thể đã đổi sau lúc backup), mà nhập DB không đổi mật khẩu của user DB. Nên tpan đặt bản đang chạy của site vào cây khôi phục:

```go
cur, ok, err := currentWPConfig(sys, l)
if err != nil {
	return fail(err)
}
if ok {
	// Cùng chủ và nhóm như mọi file khác trong public (extractChown): thư
	// mục site có setgid nên file WP-CLI tạo lúc dựng site mang nhóm
	// www-data, không phải nhóm riêng của user (e2e M9 đo được).
	if err := sys.WriteFileAtomic(cfg, cur, wpConfigMode, system.Owner{User: l.User, Group: site.WebGroup}); err != nil {
```

Ghi sau cùng để `chmod 0640` ở trên không đè lên 0600. Ba dòng chú thích là kết quả của chỗ sai 1 (mục 5). `currentWPConfig` đọc file bằng `ReadFile`, không bằng lệnh; mục 4.8 nói vì sao, và nói ba lớp chặn nó dùng.

### 3.7 Tráo thư mục: `MoveTree` và `afterAside`

`MoveTree` (M6, `internal/steps/trash.go`) đổi tên một thư mục bằng `sys.Rename`. M9 nới danh sách gốc được phép thành `{"/var/www", site.TrashRoot, site.RestoreRoot}` (`trashRoots`). Hai lần `rename` liền nhau: cất `public` đang chạy sang `old-public`, đưa `public` đã chuẩn hoá vào. Giữa hai lần có một khoảng rất ngắn site không có `public` (bước 3 và 4 có dấu "NGUY HIỂM"), nhưng không bao giờ có một site nửa cũ nửa mới.

`Check` của `MoveTree` là "nguồn không còn và đích có". Lúc xem trước, đích của bước 4 là `l.Public`, mà `l.Public` thì đang có sẵn (site đang chạy), nguồn `<rd>/public` thì chưa có. Một mình `MoveTree.Check` sẽ báo "đã xong, bỏ qua". `afterAside` sửa đúng chỗ đó:

```go
type afterAside struct {
	*steps.MoveTree
	aside string
}

func (a *afterAside) Check(ctx context.Context, sys system.System) (bool, error) {
	ok, err := pathExists(sys, a.aside)
	if err != nil || !ok {
		return false, err
	}
	return a.MoveTree.Check(ctx, sys)
}
```

Đây là struct nhúng: `afterAside` có sẵn `Info`, `Apply`, `Undo` của `MoveTree`, chỉ viết lại `Check` (cùng cách `afterStep` của M12, `12-m12-phien-ban-va-cap-nhat.md` mục 4). Chỉ khi `old-public` đã có thì mới hỏi tiếp `MoveTree.Check`. Vì sao không dùng thẳng `afterStep`: mục 5.

### 3.8 DB: `MariaDBDump` một DB, rồi `DBImport`

`MariaDBDump` (M12, `internal/steps/apt_m12.go`) được M9 mở rộng: `DB` khác rỗng thì chỉ xuất DB đó.

```go
return system.Cmd{Name: "mariadb-dump", Args: []string{
	"--single-transaction", "--quick", "--result-file=" + s.Path, s.DB,
}}
```

Không `--databases`, nên file không có `CREATE DATABASE` hay `USE`: nhập lại được vào đúng DB của site, kể cả khi tên DB khác. Bản xuất được coi là trọn vẹn khi dòng cuối bắt đầu bằng `-- Dump completed` (`dumpDone`), đọc bằng `tail -n 1` chứ không `ReadFile` (file có thể vài GB). `Apply` chặn đầy đĩa lại cho riêng bản xuất (`dumpSpaceOK`). Không có `Undo`: file xuất là đường lùi, giữ lại.

`DBImport.Apply`:

```go
// Kiểm cả hai file TRƯỚC khi xoá DB: không có đường lùi thì không xoá gì.
for _, p := range []string{s.UndoFile, s.File} {
	if err := s.ready(ctx, sys, p); err != nil {
		return fmt.Errorf("nhập DB %s: %w", s.Layout.DB, err)
	}
}
if err := s.load(ctx, sys, s.File); err != nil {
	// Engine không Undo bước có Apply lỗi: tự nhập lại bản trước đó.
	if rerr := s.load(ctx, sys, s.UndoFile); rerr != nil {
		return errors.Join(err, fmt.Errorf("trả DB về %s: %w", s.UndoFile, rerr))
	}
	return err
}
```

`load` làm hai việc: `DROP DATABASE` rồi `CREATE DATABASE` (qua stdin của `mariadb`), rồi `mariadb --database=<db> --abort-source-on-error -e 'source <file>'`. Engine chỉ `Undo` những bước đã Apply thành công; bước có `Apply` lỗi thì không, nên `DBImport` phải tự nhập lại bản trước. Nếu không, một lần nhập hỏng giữa file để lại DB chỉ có nửa đầu của file (mục 4.6).

Đường dẫn file đi vào câu `source`, nơi client `mariadb` không có cách thoát ký tự. `importPathOK` chỉ nhận `[A-Za-z0-9_./-]` và chỉ dưới `/var/lib/tpan/backups` hoặc `/var/lib/tpan/restore`.

### 3.9 Sau khôi phục: kiểm chứng rồi mới dọn

`Verify` là `siteChecks`, đúng bộ của lúc tạo site. Kiểm chứng lỗi thì engine hoàn tác ngược mọi bước: nhập lại `db-before.sql`, đưa `public` từ backup về `<rd>/public`, đưa `old-public` về `l.Public`, gỡ `<rd>`, xoá bản tự backup. Vì hoàn tác cần `old-public`, thư mục khôi phục chỉ được gỡ trong `after`, tức khi job đã thành công. `after` làm ba việc: ghi dòng `before-restore` cho bản tự backup ở bước 1, gỡ `<rd>` bằng `RemoveTree` (chỉ dưới `RestoreRoot`), và `prune` với danh sách `protect` gồm bản vừa dùng để khôi phục và bản vừa tự backup.

### 3.10 Chỗ khác dùng backup

- Xoá hẳn (`internal/core/trash.go`, `PreparePurge`): bước đầu là `purgeBackup`, một `BackupArchive` có nguồn là thư mục trong thùng rác. `purgeBackup` nhúng `BackupArchive` và viết lại `Undo`: các bước sau (xoá DB, xoá thư mục) không hoàn tác được, nên khi engine hoàn tác, file backup chỉ bị xoá nếu `purgeIntact` thấy dữ liệu gốc còn nguyên; không thì giữ và đánh dấu `kept` để vẫn ghi dòng. Không đủ chỗ thì từ chối, site ở lại thùng rác, bộ hẹn giờ thử lại giờ sau.
- Nâng MariaDB (`internal/core/updates.go`, `requireBackups`): còn site active không có bản `full` mới hơn `BackupFreshFor` (24 giờ) thì từ chối `backup_required` (mục 2.11).
- `CreateAll` chạy `PrepareCreate` rồi `Execute` cho từng site active, site lỗi thì ghi vào kết quả rồi đi tiếp, cuối cùng `SnapshotTPANDB`: tạo `_tpan` 0700, gọi `Store.SnapshotDB` (`VACUUM INTO`, mục 4.7), giữ 3 bản theo tên. `checks.TPANBackupAge` (`internal/checks/m9.go`) đọc giờ trong tên bản sao mới nhất: chưa có hoặc cũ hơn 48 giờ thì Warn.
- `PrepareSettings` ghi `/etc/cron.d/tpan-backup` bằng `steps.File` từ `templates.BackupCron(at)`, hoặc gỡ nó bằng `steps.Absent` khi `--off`. Hai setting `backup.keep`, `backup.at` chỉ được ghi trong `after`.
- MCP (`internal/tools/m9.go`): `list_backups` (không trả đường dẫn và sha256), `propose_backup_create`, `propose_backup_restore`. `Planner.prepareBackup` (`internal/core/plans.go`) đổi `BackupID` trống thành id của bản mới nhất ngay lúc đề xuất, để lúc duyệt khôi phục đúng bản người đã xem.

### 3.11 `tpan db ...`: `cmd/tpan/db.go`, `dbPlanCmd`

Ba lệnh `passwd`, `dump`, `import` dùng chung `dbPlanCmd`: kiểm domain, đòi root, `openBackups`, gọi hàm `prepare` tương ứng (`PrepareDBPassword`, `PrepareDBDump`, `PrepareDBImport`), rồi `runPlanned`. `--id` chỉ có ở `import` và là cờ bắt buộc. Cả ba nằm trên `core.Backups` vì bản xuất DB cũng là một dòng của bảng `backups` (kind `sql`).

### 3.12 Đổi mật khẩu: `PrepareDBPassword` rồi `DBPassword.Apply`

`PrepareDBPassword` (`internal/core/dbops.go`) sinh mật khẩu mới 32 ký tự bằng `sitePassword` (`internal/core/site.go`, cùng hàm sinh mật khẩu DB lúc tạo site), dựng Plan một bước `steps.DBPassword`, kiểm chứng là `dbVerify`. `DBPassword.Apply` (`internal/steps/db_m9.go`):

```go
old, err := s.readOld(sys)
...
l := s.Layout
if err := s.alter(ctx, sys, s.Password, "đổi mật khẩu user DB "+l.DB); err != nil {
	return wpHide(err, s.Password, old)
}
if err := s.write(ctx, sys, s.Password, "ghi mật khẩu DB mới vào "+l.WPConfig); err != nil {
	// Engine không Undo bước có Apply lỗi: tự đặt lại mật khẩu cũ để khớp
	// wp-config.php (vẫn là mật khẩu cũ), site chạy lại như trước.
	if rerr := s.alter(ctx, sys, old, "đặt lại mật khẩu cũ cho user DB "+l.DB); rerr != nil {
		err = errors.Join(err, rerr)
	}
	return wpHide(err, s.Password, old)
}
```

- `readOld` đọc mật khẩu cũ để hoàn tác được. Nó đọc `wp-config.php` bằng `ReadFile`, đòi file thường của user site, đúng một dòng `define` cho mỗi `DB_NAME`, `DB_USER`, `DB_PASSWORD`, `DB_NAME` và `DB_USER` đúng tên DB của site, và mật khẩu cũ đúng dạng tpan sinh (32 chữ và số). Mật khẩu khác dạng là do người khác đặt, có thể còn dùng ở chỗ khác: tpan từ chối, bảo đổi tay.
- `alter` chạy `ALTER USER '<db>'@'localhost' IDENTIFIED BY '<mới>'` qua stdin của `mariadb` (`dbRun`); lỗi thì lọc mật khẩu khỏi thông báo (`dbRedact`).
- `write` chạy `wp config set DB_PASSWORD --prompt --type=constant --quiet` dưới user của site, mật khẩu là một dòng trên stdin (`setCmd`). `--quiet` cần vì không có nó WP-CLI in lại cả lệnh kèm giá trị vừa hỏi.
- `wpHide` thay mọi lần mật khẩu xuất hiện trong lỗi bằng `••••`.

`Check` chỉ đọc cờ `applied` trong bộ nhớ: không có cách đọc lại "mật khẩu đã đổi" mà không đọc chính mật khẩu. Trường `old` là con trỏ `*system.Secret` vì một lý do Go nhỏ (chú thích ở `DBPassword`): `fmt` không gọi `String()` của trường không xuất, nên in cả bước bằng `%v` sẽ lộ giá trị nằm thẳng trong trường; con trỏ thì `fmt` chỉ in địa chỉ.

`dbVerify` gồm `checks.DBAccess` và phép kiểm đầu cuối bọc trong `dbRetry`: kết quả `Fail` thì thử lại mỗi 300 ms, tối đa 6 giây. Mục 2.8 cho thấy khoảng 500 chỉ dài 0,26 giây và nằm trong job, nên lúc kiểm chứng chạy site đã trả 200 ngay lần thử 1. Chú thích trong mã của `dbRetry` và `DBPassword` vẫn nói opcache có thể giữ `wp-config.php` cũ tới 2 giây; số đo trên máy ảo (M9.md) không thấy điều đó. Hiểu `dbRetry` như một tấm đệm an toàn.

### 3.13 Xuất và nhập: `PrepareDBDump`, `PrepareDBImport`, `dbRecord`

`PrepareDBDump` nhận site active hoặc trong thùng rác (DB của site trong thùng rác vẫn còn), chặn đầy đĩa theo cỡ DB (`dbSpaceCheck`), chọn tên bằng `dbDumpPath`, dựng Plan một bước `MariaDBDump`. `after` gọi `dbRecord`: băm file bằng `sha256sum`, đọc cỡ, `AddBackup` với kind `sql`, rồi `prune`.

`PrepareDBImport` chỉ nhận một id có trong bảng `backups`, kind `sql`, cùng domain. Không nhận đường dẫn tuỳ ý: một file `.sql` lạ chạy bằng root là chạy SQL tuỳ ý. Sau đó: file phải nằm trong thư mục backup của site, là file thường, sha256 phải khớp lúc xuất (không thì `backup_corrupt`), đủ chỗ cho bản tự xuất. Plan có hai bước:

```go
undo, err := dbDumpPath(b, l, "-before-import")
...
Steps: []engine.Step{
	&steps.MariaDBDump{DB: l.DB, Path: undo},
	&steps.DBImport{Layout: l, File: bk.Path, UndoFile: undo},
},
Verify: dbVerify(st),
```

Đuôi `-before-import` để bản tự xuất không bao giờ trùng tên với bản người dùng vừa xuất trong cùng giây (mục 2.9). Hai bản xuất bình thường trong cùng một giây thì `dbDumpPath` từ chối `backup_exists`: `MariaDBDump` coi file trọn vẹn có sẵn là "đã xong" và cột `path` của bảng là `UNIQUE`.

### 3.14 Cron bắt đầu từ Plan tạo site: `internal/core/site.go`

Plan tạo site gọi `siteCronSteps(l)` ngay sau `WPConfig`:

```go
cron, err := siteCronSteps(l)
if err != nil {
	return engine.Plan{}, wrap(err)
}
ss = append(ss, cron...)
// Bước 8: vhost (Apache cần thêm .htaccess, phải có trước khi vhost được bật).
```

Chú thích phía trên đoạn này (dòng 466 tới 475) giải thích vì sao đặt ở đó: `WPConstant` sửa chính `wp-config.php` nên phải đứng sau bước tạo nó; đứng trước vhost thì lượt xem đầu tiên đã không còn gánh cron; và job hàng loạt của M6 tách bước cài WordPress xuống pha C, nên bước nào đứng sau bước cài sẽ có thứ tự khác nhau giữa tạo một site và tạo cả lô. File cron có trước khi WordPress được cài cũng không sao: `tpan cron run` từ chối site chưa có trong bảng `sites`, mà dòng `sites` chỉ được ghi khi job thành công.

Site tạo trước M9 chưa có hai bước này. Chạy lại `sudo tpan site add <domain>` thì hai bước đó Apply, các bước khác "đã đúng, bỏ qua".

### 3.15 Hai bước cron: `siteCronSteps` (`internal/core/cron.go`)

```go
return []engine.Step{
	&steps.WPConstant{Layout: l, Name: "DISABLE_WP_CRON", Value: "true"},
	&steps.File{
		Path: site.CronFile(l), Content: content, Perm: 0o644, Owner: system.Root,
```

Tắt cron theo lượt xem trước, rồi mới đặt cron thật; ngược lại thì có lúc việc chạy từ hai nơi. `WPConstant` (`internal/steps/db_m9.go`) đặt hằng bằng `wp config set DISABLE_WP_CRON true --raw --type=constant --quiet` dưới user của site. `Check` của nó đọc bằng `wp config get ... --format=json` và so với `true`. Nó nhớ giá trị cũ để `Undo` ghi lại đúng giá trị đó, hoặc `wp config delete` nếu trước đó chưa có.

`content` đến từ `templates.SiteCron(l)` (`internal/templates/m9.go`) với template `files/site-cron.tmpl`. Dòng cron là dòng lệnh của `/bin/sh`, mà `text/template` không thoát ký tự, nên `SiteCron` kiểm lại domain bằng `cronDomain` (`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`) dù domain đã qua `validate.Domain`. Đường dẫn tpan cố định `/usr/local/bin/tpan` vì PATH của cron rất hẹp. `site.CronFile(l)` (`internal/site/backup.go`) trả `/etc/cron.d/tpan-<slug>`.

Không có bước reload cron nào. Cron của Debian tự thấy file mới (mục 2.2 và 4.3).

### 3.16 Cron gọi tpan: `cmd/tpan/cron.go`, `cronRunCmd`

Mỗi 5 phút, cron chạy `/usr/local/bin/tpan cron run <domain>` bằng root. `cronRunCmd` kiểm domain, đòi root, mở `tpan.db`, gọi `core.Cron.Run`. Thành công hay `wp` lỗi đều im lặng và thoát 0: kết quả đã nằm trong `tpan.db`, còn cron sẽ gửi thư hay ghi log mỗi khi lệnh in ra gì đó. Chỉ in và thoát khác 0 khi không ghi được kết quả hoặc site không có trong tpan (dòng cron sai thì thấy trong log của cron).

### 3.17 `core.Cron.Run`: chạy `wp` dưới user của site, ghi `cron_runs`

```go
if st.Status != site.Active {
	// Site trong thùng rác: user Linux của nó bị khoá bằng hạn tài khoản,
	// nhưng root gọi runuser bỏ qua hạn đó, nên phải chặn ở đây.
	run.Status = CronSkipped
	run.Output = "site " + d.String() + " không active (" + string(st.Status) + "), bỏ qua"
} else {
	rctx, cancel := context.WithTimeout(ctx, cronTimeout)
	res, err := c.Sites.Sys.Run(rctx, cronCmd(st.Layout()))
	cancel()
	run.Duration = c.now().Sub(start)
	run.Status, run.ExitCode = CronOK, res.ExitCode
	out := strings.TrimSpace(res.Stdout + "\n" + res.Stderr)
```

- `cronCmd` là `wp cron event run --due-now --quiet --path=<public>` với `AsUser: l.User`: `system.System` chạy nó bằng `runuser -u tp_<slug> --`. WordPress không bao giờ chạy bằng root.
- `cronTimeout` là 4 phút: một lần chạy treo (plugin gọi mạng không trả lời) bị cắt trước lượt kế tiếp, để các lần chạy không chồng lên nhau.
- Output giữ 2 KB cuối (`cronTail`, không cắt giữa một ký tự UTF-8): phần cuối là chỗ có lỗi.
- Site trong thùng rác ghi `skipped` (mục 2.10). Lý do nằm trong chú thích: M6 khoá user của site bằng hạn tài khoản, mà root gọi `runuser` thì bỏ qua hạn đó (`06-m6-hang-loat-va-thung-rac.md` mục 4.2).

`Run` không đi qua engine và không lấy khoá toàn cục: nó chạy 5 phút một lần cho mỗi site; giữ khoá thì người đang bấm trên panel bị 409. Kết quả ghi bằng `Store.SaveCronRun` (`internal/store/backup.go`):

```go
`INSERT INTO cron_runs (domain, started_at, duration_ms, status, exit_code, output)
 VALUES (?, ?, ?, ?, ?, ?)
 ON CONFLICT (domain) DO UPDATE SET started_at = excluded.started_at,
   duration_ms = excluded.duration_ms, status = excluded.status,
   exit_code = excluded.exit_code, output = excluded.output`,
```

Mỗi domain một dòng, ghi đè: bảng không lớn dần dù cron chạy 288 lần một ngày mỗi site.

### 3.18 `tpan cron list` và các điểm nối với thùng rác

`Cron.List` đọc danh sách site và bảng `cron_runs`, rồi với mỗi site active hoặc trong thùng rác, hỏi máy xem file cron có không (`pathExists`), chứ không suy từ trạng thái: site tạo trước M9 chưa có file, và màn Cron phải nói đúng điều đó. Cuối cùng thêm lịch backup từ `Backups.Settings`.

Thùng rác: `trashSteps` (`internal/core/site.go`) thêm `siteCronAbsent(l)` trước bước chuyển thư mục, để cron không gọi `wp` vào một thư mục đang đi. Khôi phục từ thùng rác (`internal/core/trash.go`) gọi lại `siteCronSteps` sau khi thư mục về chỗ và user được mở khoá. Xoá hẳn có `siteCronAbsent` trong `purgeSteps` (thường là "đã đúng, bỏ qua", mục 2.10).

## 4. Kiến thức nền

### 4.1 tar, symlink, và chủ, quyền trong header

Một file tar là chuỗi các mục, mỗi mục có header ghi tên, loại (file, thư mục, symlink, link cứng, thiết bị), chủ, nhóm, quyền. Không có `-h`, GNU tar lưu symlink là symlink (ghi đích của link), không mở file mà link trỏ tới. Điều này quan trọng vì tpan chạy tar bằng root: user của site có thể tạo `public/x -> /etc/shadow`, và nếu tar đi theo link thì bản backup sẽ chứa `/etc/shadow`. Khi giải nén, root mặc định dùng chủ và quyền trong header; `--no-same-owner --no-same-permissions` tắt điều đó. Trong mã: `tarCreate`, `tarExtract`, `checkTarList` (`internal/steps/backup.go`). Tự xem (chưa chạy): `scripts/vm.sh root bash -c 'tar -tvzf "$(ls /var/lib/tpan/backups/m9a.example.com/*.tar.gz | head -1)" | head'`, cột đầu là loại và quyền, cột hai là `chủ/nhóm`.

### 4.2 `rename` cùng phân vùng, lần này cho khôi phục

`06-m6-hang-loat-va-thung-rac.md` mục 4.1 giải thích `rename(2)`: cùng phân vùng thì chỉ sửa một mục trong thư mục cha, tức thì, không bao giờ có bản chép dở; khác phân vùng thì lỗi `EXDEV`. M9 dùng điều này hai lần: đổi `.tar.gz.part` thành tên thật (cùng thư mục), và tráo `public` giữa `/var/www/<domain>` và `/var/lib/tpan/restore/<slug>`. Lần sau đòi `/var/lib/tpan/restore` cùng phân vùng với `/var/www`, như thùng rác. Trong mã: `MoveTree.Apply`, `BackupArchive.Apply`. M9.log không có lệnh `df` so hai thư mục này. Tự xem (chưa chạy trong M9): `scripts/vm.sh root df /var/www /var/lib/tpan`, hai dòng phải cùng cột `Filesystem`.

### 4.3 `/etc/cron.d`: dòng có cột user, tên file, quyền

`crontab -e` của mỗi user có 5 cột giờ rồi lệnh. File trong `/etc/cron.d` có thêm cột thứ sáu là user chạy lệnh (`*/5 * * * * root ...`). `*/5` ở cột phút nghĩa là phút chia hết cho 5. Cron của Debian và Ubuntu bỏ qua, không báo gì, file trong `/etc/cron.d` có tên chứa ký tự ngoài chữ, số, `_` và `-` (theo quy ước tên của `run-parts`): vì vậy tên là `tpan-<slug>` (slug chỉ có chữ thường, số, `_`), không phải `tpan-m9a.example.com`. Cron cũng bỏ qua file mà người khác root ghi được, nên file là root 0644. Cron tự thấy file mới mà không cần reload (mục 2.2). Trong mã: `site.CronFile` (`internal/site/backup.go`), `siteCronSteps`. Tự xem (chưa chạy): `scripts/vm.sh exec bash -c 'man 8 cron | grep -i -A4 "cron.d"'`.

### 4.4 wp-cron theo lượt xem và `DISABLE_WP_CRON`

WordPress có lịch việc riêng (đăng bài hẹn giờ, kiểm cập nhật, việc của plugin) lưu trong DB. Mặc định, mỗi lượt xem trang WordPress kiểm có việc tới hạn không, có thì gọi `wp-cron.php` để chạy. Site không có khách thì việc bị trễ vô hạn; site đông khách thì mỗi lượt xem tốn thêm một lần kiểm. `define('DISABLE_WP_CRON', true)` trong `wp-config.php` tắt cơ chế đó; lúc đó phải có ai khác gọi, và `wp cron event run --due-now` chạy đúng những việc đã tới hạn. Trong mã: `WPConstant`, `cronCmd`. Tự xem: `scripts/vm.sh root bash -c 'cd / && runuser -u tp_m9a_example_com_c2b3ff -- wp cron event list --path=/var/www/m9a.example.com/public'` (mục 2.3 đã chạy bản có lọc `publish_future_post`).

### 4.5 `mariadb-dump --single-transaction --quick`

`--single-transaction` mở một giao dịch với ảnh chụp nhất quán trước khi đọc, nên mọi bảng InnoDB được xuất đúng như tại một thời điểm, mà không khoá bảng: site vẫn ghi được trong lúc xuất. Bảng không phải InnoDB (MyISAM) không được bảo vệ như vậy. `--quick` đọc từng dòng từ server thay vì giữ cả bảng trong RAM. Bản xuất trọn vẹn kết thúc bằng dòng `-- Dump completed on ...`; tpan đọc dòng đó bằng `tail -n 1` để biết file không bị cắt giữa chừng. Trong mã: `MariaDBDump.cmd`, `MariaDBDump.complete`. Tự xem: `scripts/vm.sh root tail -1 /var/lib/tpan/backups/m9a.example.com/m9a.example.com-db-20261119-181528.sql` (mục 2.9 đã chạy, in `-- Dump completed on 2026-11-20  1:15:28`).

### 4.6 `DROP DATABASE`, quyền của user, và `source`

Quyền của user DB trên một DB (`GRANT ... ON db.*`) lưu theo tên DB trong bảng hệ thống, không nằm trong DB đó. Xoá DB rồi tạo lại cùng tên thì user vẫn dùng được (tài liệu MariaDB, trích ở đầu `internal/steps/backup.go`, và M9.md đã kiểm). `source <file>` là lệnh của client `mariadb`, đọc file và chạy từng câu. Theo M9.md (kiểm trên máy ảo, log không lưu trong repo): không có `--abort-source-on-error` thì gặp câu lỗi vẫn chạy tiếp và thoát 0; có cờ thì thoát 1, nhưng các câu trước chỗ lỗi đã chạy rồi. Vì vậy `DBImport` lỗi luôn tự nhập lại bản trước. Trong mã: `importResetSQL`, `importCmd`, `DBImport.load`. Tự xem (chưa chạy): `scripts/vm.sh root mariadb -N -e "SHOW GRANTS FOR 'tp_m9a_example_com_c2b3ff'@'localhost'"`.

### 4.7 `VACUUM INTO` và WAL của SQLite

`tpan.db` chạy ở chế độ WAL: thay đổi mới ghi vào file `tpan.db-wal` trước, rồi mới gộp vào file chính. Chép riêng `tpan.db` bằng `cp` lúc đó có thể ra một bản thiếu dữ liệu hoặc hỏng. `VACUUM INTO '<file>'` là câu SQL bảo chính SQLite viết ra một bản sao nhất quán, gọn, không kèm `-wal`. Trong mã: `Store.SnapshotDB` (`internal/store/backup.go`); nó tạo file đích trước bằng `O_EXCL` với quyền 0600 (vừa chặn ghi đè, vừa không có lúc file mới mang quyền 0644), và truyền đường dẫn bằng tham số `?` chứ không nối chuỗi. Tự xem: lệnh `python3` ở mục 2.11 (`PRAGMA integrity_check` trên bản sao trả `ok`).

### 4.8 Vì sao mật khẩu đi qua stdin, và vì sao đọc `wp-config.php` bằng `ReadFile`

Dòng lệnh (argv) của mọi tiến trình đọc được bởi mọi user trên máy qua `ps` hay `/proc/<pid>/cmdline`. Mật khẩu đặt trên dòng lệnh (`--password=...`, hay `wp config set DB_PASSWORD <giá trị>`) thì lộ trong khoảng lệnh chạy. Thêm một lý do riêng của tpan: engine ghi mọi lệnh và mọi output của `Run` vào `job_events`, rồi đưa ra SSE và MCP. Nên mật khẩu chỉ đi qua stdin (`Cmd.Stdin`), và lệnh hiện trong log không mang nó (mục 2.8). Cùng lý do, `wp-config.php` không được đọc bằng `cat` hay `wp config get`: output của chúng sẽ vào `job_events`. tpan đọc thẳng bằng `sys.ReadFile`. Nhưng root đọc một đường dẫn trong thư mục do user của site kiểm soát thì có nguy cơ khác: user đó tráo file thành symlink trỏ tới `wp-config.php` của site khác. Ba lớp chặn trong `readOld` và `currentWPConfig`: `Stat` không đi theo symlink và đòi file thường; cỡ nhỏ; và nội dung đọc được phải ghi `DB_NAME`, `DB_USER` đúng tên DB của site này, nên lỡ bị tráo giữa `Stat` và `ReadFile` thì thứ đọc được bị từ chối. Tự xem (chưa chạy): `scripts/vm.sh exec bash -c 'sleep 300 & cat /proc/$!/cmdline | tr "\0" " "; echo; ps -eo user,args | grep "[s]leep 300"'`; user thường đọc được dòng lệnh của mọi tiến trình.

### 4.9 Chặn đầy đĩa: 1,2 lần và 10%

Luật của mục 6.5: chỉ ghi khi chỗ trống ≥ cỡ cần × 1,2 và sau khi ghi vẫn còn ≥ 10% đĩa. Vế đầu chừa sai số cho ước lượng; vế sau giữ cho máy không bao giờ bị backup đẩy tới mức MariaDB hay log không ghi được. `SpaceOK` viết bằng số nguyên, làm tròn lên ở phép chia:

```go
rem := free - need
return rem >= ceilDiv(need, 5) && rem >= ceilDiv(total, 10)
```

`free ≥ need × 1,2` tương đương `free − need ≥ need / 5`. Làm tròn lên để không bao giờ cho qua một ca sát nút mà luật không cho. "Cần" là cỡ chưa nén, nên luật ước lượng dư (mục 2.7). Luật được áp hai lần: lúc dựng Plan (từ chối sớm) và lúc Apply (Plan chờ duyệt có thể chạy sau đó hàng giờ). Tự xem: `scripts/vm.sh root df -B1 --output=size,avail,pcent /` (mục 2.7 đã chạy, in `19682557952 16666783744  16%` trước khi lấp đĩa).

### 4.10 Thư mục setgid và nhóm của file mới

Bit setgid trên một thư mục (chữ số `2` trong `2750`) làm file mới tạo bên trong mang nhóm của thư mục, không phải nhóm chính của người tạo. Thư mục site là `tp_<slug>:www-data` 2750, nên `wp-config.php` do WP-CLI tạo dưới user `tp_<slug>` mang nhóm `www-data`. Khi tpan tự ghi file bằng `WriteFileAtomic` với chủ ghi rõ, nó phải ghi đúng `tp_<slug>:www-data`, không thì lệch với lúc tạo (mục 5, chỗ sai 1). Trong mã: `BackupExtract.Apply`, `system.Owner{User: l.User, Group: site.WebGroup}`. Tự xem: `scripts/vm.sh root stat -c '%a %U:%G %n' /var/www/m9a.example.com /var/www/m9a.example.com/public/wp-config.php` (mục 2.6 đã chạy phần thư mục).

## 5. Cách khác đã cân nhắc, và chỗ đã làm sai

### 5.1 Nén bằng `archive/tar` và `compress/gzip` của Go

Mục 6.5 của KE-HOACH ghi "nén bằng thư viện chuẩn của Go, không cần gói ngoài". Mã không làm vậy. Mọi chỗ tpan chạm vào máy đi qua `system.System`, mà `System` chỉ có `ReadFile` và `WriteFileAtomic` giữ cả file trong bộ nhớ, còn `Run` giữ output tới khi lệnh xong. Một site vài GB thì không được. GNU tar và gzip là gói Essential của Ubuntu (luôn có, nên vẫn đúng ý "không cần gói ngoài"), đọc cây của site mà không đi theo symlink, và tpan vẫn chạy nó bằng argv qua `system.Run`. Đường nâng cấp ghi ở `docs/thay-doi/M9.md` và đầu `internal/steps/m9_types.go`: thêm API đọc ghi theo luồng vào `System`, rồi dùng `archive/tar`. Cái giá của lựa chọn này: kiểm danh sách thành viên phải đọc output dạng chữ của `tar --list --verbose` (`parseTarLine`), và mỗi mục thành một sự kiện trong log job (4270 sự kiện ở mục 2.6).

### 5.2 Dòng cron gọi thẳng `wp` dưới user của site

Mục 6.10 chỉ nói chạy `wp cron event run --due-now` "dưới user của site". Cách thẳng nhất là dòng cron `*/5 * * * * tp_<slug> wp cron event run --due-now --path=...`. Không chọn vì hai lẽ (M9.md và chú thích trong `files/site-cron.tmpl`). Một: màn Cron cần lần chạy gần nhất và kết quả, mà cron không ghi kết quả vào đâu tpan đọc được; tpan đứng giữa để ghi `cron_runs`. Hai: site trong thùng rác phải không chạy, và tpan muốn tự quyết điều đó theo trạng thái trong `tpan.db` (ghi `skipped`) thay vì dựa vào việc user bị khoá. `wp` vẫn chạy dưới user của site (`runuser`). File cron vẫn được gỡ khi vào thùng rác; kiểm trạng thái trong `Cron.Run` là lớp thứ hai.

### 5.3 `wp config set DB_PASSWORD --prompt=value`

Mục 16 hỏi `wp config set` có cách nhận giá trị không lộ trên dòng lệnh không. Ý đầu là `--prompt=value`, để WP-CLI chỉ hỏi đúng tham số đó. Kiểm trên máy ảo với WP-CLI 2.12.0 (M9.md và chú thích của `DBPassword`; log của lần kiểm không lưu trong repo): lệnh thoát 1 và in usage, vì `--prompt=<danh sách>` chỉ hỏi các tham số dạng `--tên=giá trị`, còn `<value>` là tham số theo vị trí. `--prompt` trơn thì hỏi mọi tham số còn thiếu theo thứ tự synopsis, `<value>` trước; hết stdin thì WP-CLI dừng hỏi và chạy với những gì đã có. Kết quả: thoát 0, `wp-config.php` có đúng dòng mới, quyền vẫn 600. Thiếu `--quiet` thì giá trị bị in lại hai dòng, nên `--quiet` là bắt buộc.

### 5.4 Đọc mật khẩu cũ bằng `wp config get`, đọc `wp-config.php` bằng `cat`

Hợp đồng ban đầu của `BackupExtract` ghi "đọc bằng cat dưới user của site". Cách này an toàn về symlink (lệnh chạy dưới user của site, không đọc được gì user đó không đọc được), nhưng engine ghi mọi output của `Run` vào `job_events`, nên mật khẩu DB và các khoá muối của WordPress sẽ nằm trong `tpan.db`, đi ra SSE và MCP. `wp config get DB_PASSWORD` cho `DBPassword.readOld` cũng vậy. Mã chuyển sang `ReadFile` có ba lớp chặn (mục 4.8). Chỗ đổi này ghi trong chú thích của `currentWPConfig` và trong M9.md; bằng chứng là mục 2.8: mật khẩu mới xuất hiện 0 lần trong `tpan.db`, WAL, SHM và journal.

### 5.5 Dùng `afterStep` của M12 cho bước tráo thứ hai

M12 có sẵn `afterStep{dep}` (`internal/core/oneway.go`): bước chỉ "đã đúng" khi bước `dep` đứng trước cũng đã đúng. Ý tự nhiên là `afterStep{dep: setAside}`. Không được: sau khi bước 4 chạy, cả `l.Public` lẫn `old-public` đều có, và `MoveTree.Check` của `setAside` trả lỗi "cả hai đều có" (nguồn còn và đích cũng có). Engine gọi `Check` lại ngay sau `Apply` để xác nhận, nên bước 4 sẽ luôn hỏng ở lần kiểm lại đó. `afterAside` hỏi thẳng "`old-public` có chưa", đúng cả lúc xem trước, trước và sau Apply (chú thích ở `internal/core/backup.go`).

### 5.6 Chỗ sai 1: `wp-config.php` sai nhóm sau khôi phục (commit `1454ce8`)

Bản đầu của `BackupExtract.Apply` ghi `wp-config.php` đang chạy vào cây khôi phục với chủ `tp_<slug>:tp_<slug>`, theo suy nghĩ "file do WP-CLI tạo dưới user của site thì mang nhóm riêng của user". Unit test `TestBackupExtract` lúc đó chỉ kiểm user của file, không kiểm nhóm, nên qua (diff của `1454ce8` đổi phép kiểm `e.Owner.User != f.l.User` thành so cả chủ lẫn nhóm). Lượt e2e đầu (`M9-lan1.log`) bắt được bằng phép so quyền và chủ từng mục trước và sau khôi phục:

```
sai chủ: 1
/var/www/m9a.example.com/public/wp-config.php
...
wp-config.php 600 tp_m9a_example_com_c2b3ff:tp_m9a_example_com_c2b3ff
  => KHÔNG ĐẠT: sau khôi phục: chủ tp_m9a_example_com_c2b3ff:www-data, thư mục 2750, file 640, wp-config.php 600
...
07b687425585e8785805795851c5c72c47132440d0cbd192aa432e23ba4c9c06
    471 2750 tp_m9a_example_com_c2b3ff:www-data d
      1 600 tp_m9a_example_com_c2b3ff:tp_m9a_example_com_c2b3ff f
   3784 640 tp_m9a_example_com_c2b3ff:www-data f
  => KHÔNG ĐẠT: quyền và chủ từng mục giống hệt lúc trước backup
```

(`M9-lan1.log` dòng 940 tới 970.) Mã băm `07b687...` khác `29d251...` của lúc trước backup. Khẳng định trượt thứ ba (dòng 1217, ở phần đổi mật khẩu) là hệ quả của cùng lỗi: file vẫn mang nhóm sai từ lần khôi phục trước, `DBPassword` không đổi nhóm. Ba khẳng định trượt, một nguyên nhân. Lý do đúng là thư mục site có setgid (mục 4.10). Sửa: ghi với `Group: site.WebGroup`, sửa luôn unit test cho khớp, chạy lại e2e từ máy sạch (M9.log, 128/0). Quyền 600 nên lỗi này không làm ai đọc được file, và `checks.WPConfigPerm` chỉ kiểm user nên health không báo; chỉ phép so toàn cây mới thấy. M9.md ghi thêm: `steps.WPConfigPerm` (sửa tự động của M4) cũng đặt nhóm `tp_<slug>`, cùng lỗi, chưa sửa vì ngoài vùng của M9.

Bài học: unit test chạy trên `FakeSystem` không có setgid, nên nó chỉ kiểm được điều người viết test tin là đúng.

### 5.7 Chỗ sai 2: xem trước khôi phục báo bước tráo là "đã đúng, bỏ qua" (commit `5dd4edc`)

Bản đầu dựng bước 4 bằng `MoveTree` trần. Như mục 3.7, lúc xem trước `l.Public` đang có, nên `Check` của bước 4 trả "đã xong" và bản xem trước in `[đã đúng, bỏ qua]` cho đúng bước quan trọng nhất của lần khôi phục. Engine chạy thật thì không sai (lúc tới bước 4, `MoveTree.Check` thấy nguồn còn và đích không còn), nhưng người duyệt nhìn bản xem trước sẽ hiểu nhầm, và Planner của MCP lưu dấu vân tay của chính bản xem trước đó lúc đề xuất (chú thích đầu `internal/core/plans.go`). Sửa bằng `afterAside`, và `TestBackupRestore` (`internal/core/backup_test.go`) thêm kiểm "không bước nào của bản xem trước hiện là đã xong". Commit chỉ ghi "xem trước khôi phục đúng bước tráo", không ghi phát hiện bằng cách nào. Bằng chứng sau sửa: mục 2.6, `6 bước sẽ chạy, 0 bước đã đúng sẽ bỏ qua`.

## 6. Tự kiểm

1. Đĩa ở mức 91% (mục 2.7), backup cần 105,8 MiB mà đĩa còn 1,6 GiB. Vậy vì sao bị từ chối? Vế nào của luật quyết định, và nếu bỏ vế đó thì chuyện gì có thể xảy ra?
2. Vì sao tên file backup chỉ được chọn lúc Apply, và vì sao bản xem trước in `<giờ lúc chạy>`? Nếu chọn lúc dựng Plan thì cái gì hỏng?
3. Khôi phục giải nén vào `/var/lib/tpan/restore/<slug>` rồi tráo bằng `rename`. Vì sao không giải nén thẳng vào `/var/www/<domain>/public`? Nêu ít nhất hai lý do. Và vì sao thư mục khôi phục chỉ bị gỡ trong `after`, không phải bước cuối của Plan?
4. `wp config get DISABLE_WP_CRON --type=constant` trên máy ảo in `1`, nhưng `WPConstant.Check` so với `true`. Có mâu thuẫn không?
5. Đặt tên file cron là `/etc/cron.d/tpan-m9a.example.com` thì sao? Bạn sẽ thấy lỗi ở đâu?
6. `tpan cron run` chạy bằng root. Vậy WordPress có chạy bằng root không? Và vì sao site trong thùng rác phải được chặn trong `Cron.Run`, trong khi file cron đã gỡ rồi?
7. Vì sao `db import` chỉ nhận `--id` của một bản trong bảng `backups`, không nhận đường dẫn file `.sql`? Và nếu file nhập lỗi ở giữa thì DB của site ở trạng thái nào?
8. Trong lúc đổi mật khẩu DB, site trả 500 ba lần (mục 2.8). Vì sao? Có tránh được không? Và vì sao kiểm chứng vẫn đạt ở lần thử đầu?
9. (Cần máy ảo) Chứng minh rằng mật khẩu đặt trên dòng lệnh thì một user thường khác đọc được, để thấy vì sao `DBPassword` đưa mật khẩu qua stdin.
10. (Cần máy ảo) User của site tạo một symlink `public/lienket -> /etc/shadow` rồi bạn backup. Bản backup chứa gì ở chỗ đó? Khôi phục xong thì sao? Bạn kiểm bằng lệnh nào?

## Đáp án

1. Tổng đĩa 18,3 GiB nên 10% là khoảng 1,83 GiB. Sau khi ghi 105,8 MiB, đĩa còn khoảng 1,5 GiB, nhỏ hơn 1,83 GiB: vế "còn ≥ 10% đĩa" từ chối. Vế "≥ 1,2 lần" thì qua (cần 127 MiB, có 1,6 GiB). Bỏ vế 10% thì backup được phép đẩy đĩa tới gần 100%: MariaDB, PHP-FPM, nginx không ghi được log hay file tạm, và site hỏng vì chính bản backup đáng lẽ để cứu nó. Trong mã: `SpaceOK`, `rem >= ceilDiv(total, 10)`.
2. Plan đi qua Planner (MCP) được dựng hai lần, lúc đề xuất và lúc duyệt, và hai lần phải giống hệt nhau (so dấu vân tay); tên theo giờ dựng Plan thì không bao giờ khớp và mọi Plan backup qua MCP bị coi là đã đổi. `Info()` vì vậy dùng chữ cố định `backupLater`. Giờ thật được chọn trong `BackupArchive.Apply` (`now.Format(backupStamp)`, UTC).
3. Một: lỗi giữa chừng (đĩa đầy, tar hỏng) sẽ để lại một site nửa cũ nửa mới, không có bản nào trọn vẹn. Hai: chủ và quyền phải được chuẩn hoá trước khi web server thấy file; giải nén thẳng thì có khoảng thời gian file mang chủ, quyền từ header. Ba: thư mục root 0700 không ai khác vào được, nên user của site không tráo được file thành symlink giữa lúc tpan kiểm và lúc `chown`, `chmod`. `rename` cùng phân vùng thì tức thì. Thư mục khôi phục chứa `old-public`, bản duy nhất của mã nguồn trước khi khôi phục; kiểm chứng (chạy sau mọi bước) mà lỗi thì engine hoàn tác, và hoàn tác cần `old-public`. Nên chỉ gỡ khi job đã thành công.
4. Không. Không có `--format`, WP-CLI in giá trị PHP: `true` in ra `1`, `false` in ra dòng trống. `WPConstant.getCmd` dùng `--format=json`, nên in `true` và so được đúng với `Value`. Lệnh trong e2e (mục 2.2) không có `--format=json` nên thấy `1`. Chú thích trên kiểu `WPConstant` ghi điều này.
5. Cron của Debian bỏ qua file đó mà không báo gì (tên có dấu chấm), nên không có lần chạy nào, bài hẹn giờ không bao giờ được đăng (vì `DISABLE_WP_CRON` đã bật). Bạn sẽ thấy ở màn Cron: cột "Chạy lúc" trống hoặc đứng yên ở lần chạy cũ; journal của cron không có dòng `CMD (... tpan cron run ...)`. Chưa chạy thử trên máy ảo; cách kiểm: đổi tên file thật thành tên có dấu chấm `scripts/vm.sh root mv /etc/cron.d/tpan-m9a_example_com_c2b3ff /etc/cron.d/tpan-m9a.thu`, chờ hơn 5 phút, rồi `scripts/vm.sh root bash -c 'journalctl -u cron --since "-6 min" --no-pager | grep "tpan cron run m9a"'` (không được có dòng nào) và `scripts/vm.sh root tpan cron list` (màn Cron báo `tắt` vì file đúng tên không còn). Nhớ đổi tên lại.
6. Không. `cronCmd` có `AsUser: l.User`, nên `system.System` chạy `runuser -u tp_<slug> -- wp ...`; tpan chỉ đứng giữa bằng root để ghi `cron_runs` và đọc trạng thái site. Chặn trong `Cron.Run` là lớp thứ hai, vì hai lẽ. Một: file cron có thể bị ai đó chép tay lại. Hai: M6 khoá user của site bằng hạn tài khoản, mà root dùng `runuser` thì bỏ qua hạn đó, nên khoá không chặn được cron; phải kiểm trạng thái trong `tpan.db`. Bằng chứng: mục 2.10, gọi tay `tpan cron run m9b.example.com` và nhận `status: skipped`. Lưu ý: trạng thái `trashed` chỉ được ghi khi job thùng rác đã thành công (`markTrashed`), nên một lần cron chạy đúng lúc job đó đang chạy vẫn thấy `active`; phép kiểm này không che khoảng đó.
7. `db import` chạy SQL bằng root vào DB của site. Một đường dẫn tuỳ ý (do ai đó ghi được) nghĩa là chạy SQL tuỳ ý. Chỉ file tpan đã tự ghi, có dòng trong bảng `backups`, nằm trong thư mục backup của site (root 0700), và sha256 còn khớp lúc xuất mới được nhận. Lỗi giữa file: `--abort-source-on-error` làm `mariadb` dừng và thoát 1, nhưng các câu trước chỗ lỗi đã chạy. `DBImport.Apply` thấy lỗi thì tự `load` lại `UndoFile` (bản vừa xuất ngay trước), nên DB về như trước khi nhập, rồi mới trả lỗi. Nếu nhập lại cũng lỗi thì lỗi trả về nêu cả hai. Trên máy ảo chưa chạy ca lỗi; có trong `TestDBImport` (`internal/steps/backup_test.go`).
8. Đổi mật khẩu là hai lệnh: `ALTER USER` trong MariaDB, rồi `wp config set` trong `wp-config.php`. Giữa hai lệnh, PHP còn dùng mật khẩu cũ mà MariaDB đã đổi, nên request trong khoảng đó nhận "Error establishing a database connection" (HTTP 500). Đo được 3 lần trong 0,26 giây, đều trong lúc job chạy. Đảo thứ tự (ghi file trước) thì khoảng lỗi vẫn có, chỉ theo chiều ngược lại. tpan không xoá được khoảng này, chỉ giữ nó ngắn (hai lệnh liền nhau trong một bước) và nói trước trong `Why` của bước. Kiểm chứng chạy sau khi job xong cả hai lệnh, nên site đã trả 200 ngay; `dbRetry` vẫn thử lại tối đa 6 giây phòng khi PHP còn giữ file cũ.
9. Chưa chạy. Lệnh: `scripts/vm.sh exec bash -c 'sleep 300 & cat /proc/$!/cmdline | tr "\0" " "; echo; ps -eo user,args | grep "[s]leep 300"'` cho thấy dòng lệnh đọc được qua `/proc`. Để thấy user khác đọc được, chạy một tiến trình dưới user của site rồi nhìn nó từ user `ubuntu` (user mặc định của máy ảo Multipass): `scripts/vm.sh root bash -c 'runuser -u tp_m9a_example_com_c2b3ff -- sleep 301 & sleep 1; runuser -u ubuntu -- ps -eo user,args | grep "[s]leep 301"; kill $!'`. Phải thấy dòng `tp_m9a_... sleep 301`. Thay `301` bằng một mật khẩu là thấy vấn đề. Thêm vào đó, engine ghi mọi lệnh vào `job_events`, nên mật khẩu trên dòng lệnh còn nằm lâu dài trong `tpan.db`. Mục 2.8 là bằng chứng ngược: hai lệnh của job không mang mật khẩu, và mật khẩu xuất hiện 0 lần trong `tpan.db`. Dọn: `scripts/vm.sh root pkill -f "sleep 30[01]"`.
10. Theo mã, bản backup chứa một mục loại symlink (`l`) tên `public/lienket`, đích `/etc/shadow`, không chứa nội dung của `/etc/shadow`: `tarCreate` không có `-h`. `checkTarList` nhận symlink trong `public/`. Khôi phục xong thì `public/lienket` vẫn là symlink như cũ; `chown -h` đổi chủ của chính cái link, `find -P -type f` bỏ qua nó, nên không `chmod` nhầm `/etc/shadow`. Web server và user của site vẫn không đọc được `/etc/shadow` qua link vì quyền của chính file đó. Chưa chạy (site trên máy ảo có 0 symlink). Lệnh: `scripts/vm.sh root runuser -u tp_m9a_example_com_c2b3ff -- ln -s /etc/shadow /var/www/m9a.example.com/public/lienket`, rồi `scripts/vm.sh root tpan backup create m9a.example.com --yes`, rồi `scripts/vm.sh root bash -c 'tar -tvzf "$(ls -t /var/lib/tpan/backups/m9a.example.com/*.tar.gz | head -1)" | grep lienket'` (phải thấy dòng bắt đầu bằng `l` và `-> /etc/shadow`). Nhớ xoá link sau khi thử.
