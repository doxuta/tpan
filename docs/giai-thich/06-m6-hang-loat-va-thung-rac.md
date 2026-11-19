# M6: hàng loạt, thùng rác, khôi phục, dọn tự động

Đọc kèm: `01-m1-cai-stack.md` (engine, Step, Check rồi Apply rồi Check), `02-m2-site-wordpress.md` (Plan tạo site, `site rm` của M2), `00-go-can-ban.md` (goroutine, mutex, channel), `docs/KE-HOACH.md` mục 6.4 (thùng rác) và mục 7 (hàng loạt), và `docs/thay-doi/M6.md` (mọi chỗ khác đặc tả của mốc này, kèm lý do).

## 1. Mốc này làm được gì

- `sudo tpan site rm shop.example.com` không còn xoá hẳn. Site vào thùng rác: tắt vhost và pool, khoá user DB, khoá user Linux, chuyển thư mục sang `/var/lib/tpan/trash/<slug>`. Dữ liệu còn nguyên trong 7 ngày (đổi bằng `tpan trash settings --days N`).
- `sudo tpan site restore shop.example.com` lấy site ra, mở khoá, bật lại, rồi chạy đủ các phép kiểm của lúc tạo site. `tpan trash list` xem thùng rác, `tpan trash purge <domain>` xoá hẳn một site, `tpan trash purge --expired` xoá các site đã quá hạn. Trong `tpan serve` có bộ hẹn giờ làm việc đó mỗi giờ.
- `sudo tpan apply -f sites.yaml` đưa máy về đúng danh sách trong file: site thiếu thì tạo, site đã có thì bỏ qua. Thêm `--prune` thì site ngoài danh sách vào thùng rác. `tpan export` in ra file từ trạng thái hiện tại. Trên giao diện, màn Tạo site có chế độ dán danh sách domain.
- Cả lô chạy trong một job: tối đa 3 site song song, nginx (hoặc apache2) và PHP-FPM chỉ reload một lần cho cả lô. Site nào lỗi thì hoàn tác riêng site đó, các site khác đi tiếp. Đo trên máy ảo: 10 site trong 28,9 giây.

## 2. Chạy thử

Mọi output dưới đây lấy nguyên văn từ `docs/bang-chung/M6.log`: lần chạy `scripts/e2e-m6.sh all` trên mã cuối của mốc (bản `be45181`, có bản sửa `http.gone` ở mục 5), ngày 19/11/2026, từ 22:35:13 tới 22:39:14, dòng cuối là `TỔNG KẾT: 167 khẳng định đạt, 0 khẳng định không đạt` (dòng 3724). Máy ảo của mốc này tên `tpan-m6` (ghi trong file `.tpan-vm`), Multipass, Ubuntu 24.04, arm64. Lượt Nginx chạy đầy đủ trước, lượt Apache chạy ngắn sau, mỗi lượt từ máy sạch.

Hai log hồi quy chạy trên bản tpan của M6: kịch bản của M2 (182 khẳng định đạt, 0 không đạt, `docs/bang-chung/M6-hoi-quy-m2.log`) và kịch bản của M5 (256 đạt, 0 không đạt, `docs/bang-chung/M6-hoi-quy-m5.log`). Log M5 có phần site có SSL vào thùng rác, khôi phục và xoá hẳn; mục 2.13 trích phần đó.

Những điều cần biết khi đọc log:

- Mọi lệnh gõ trên Mac, trong thư mục repo. `scripts/vm.sh exec X` chạy X trong máy ảo; `scripts/vm.sh root X` chạy X trong máy ảo bằng `sudo`. Riêng các lệnh `curl https://<vm>:8443/...` ở mục 2.11 chạy thẳng trên Mac.
- Kịch bản in lệnh bằng `$*` nên mất dấu nháy. Lệnh dưới đây mình chép từ `scripts/e2e-m6.sh` (có nháy đúng) và thay biến bằng giá trị thật; output giữ nguyên.
- Kịch bản che IP máy ảo (`<vm>`), thư mục tạm (`<tmp>`), và mọi mật khẩu. Đó là chữ của script, không phải của tpan.
- Mấy chỗ kịch bản đọc thẳng `tpan.db` bằng một đoạn `python3` dài (hàm `reload_count` và đoạn đọc job trong `scripts/e2e-m6.sh`). Mình chỉ chép phần output.

### 2.1 Chuẩn bị: `/var/www` và thùng rác có cùng phân vùng không

Chạy trên Mac:

```bash
make vm-reset
make vm-deploy
scripts/vm.sh root tpan stack install --web nginx --yes
scripts/vm.sh root df -h /var/www /var/lib/tpan
```

Output của lệnh cuối (M6.log dòng 165 tới 170):

```
Filesystem      Size  Used Avail Use% Mounted on
/dev/sda1        19G  2.4G   16G  14% /
/dev/sda1        19G  2.4G   16G  14% /
```

Hai thư mục cùng nằm trên `/dev/sda1`. Điều này quan trọng: chuyển thư mục site vào thùng rác dùng `rename`, và `rename` chỉ chạy được (và chạy tức thì) khi nguồn và đích cùng phân vùng. Mục 3.11 và 4.1 nói kỹ. Trên VPS thật chưa đo: lệnh `df /var/www /var/lib` nằm ở mục "Việc chủ dự án tự làm trên VPS" của `docs/thay-doi/M6.md`.

### 2.2 `sites.yaml` và xem trước

File kịch bản ghi vào máy ảo (M6.log dòng 178 tới 192):

```yaml
# sites.yaml của e2e M6
defaults:
  admin_email: admin@example.com
sites:
  - domain: s1.example.com
  - domain: s2.example.com
  ...
  - domain: s10.example.com
```

(Dấu `...` là mình rút gọn: file thật có đủ 10 dòng `s1` tới `s10`.)

```bash
scripts/vm.sh root tpan apply -f /root/e2e-sites.yaml --dry-run
```

Output rút gọn (M6.log dòng 224 tới 697):

```
DOMAIN                                   VIỆC             LÝ DO
s1.example.com                           tạo              
s2.example.com                           tạo              
...
Kế hoạch: Áp danh sách site: tạo 10, đưa vào thùng rác 0
133 bước sẽ chạy, 0 bước đã đúng sẽ bỏ qua, 0 bước có thể gián đoạn dịch vụ.

 1. [sẽ chạy] Tải WordPress 7.1.5 vào /var/cache/tpan  (wp.archive:7.1.5)
 2. [sẽ chạy] Tạo user Linux tp_s1_example_com_090c1b cho site  (site.user:tp_s1_example_com_090c1b)
 ...
121. [sẽ chạy] Bật /etc/nginx/sites-enabled/tpan-s10.example.com.conf → /etc/nginx/sites-available/tpan-s10.example.com.conf  (symlink:/etc/nginx/sites-enabled/tpan-s10.example.com.conf)
    $ nginx -t
122. [sẽ chạy] Kiểm cấu hình rồi nạp lại php8.3-fpm một lần cho cả lô  (service.reload:php8.3-fpm)
    $ php-fpm8.3 -t
    $ systemctl reload php8.3-fpm
123. [sẽ chạy] Kiểm cấu hình rồi nạp lại nginx một lần cho cả lô  (service.reload:nginx)
    $ nginx -t
    $ systemctl reload nginx
124. [sẽ chạy] Cài WordPress cho s1.example.com  (wp.install:s1.example.com)
```

Đếm 133 bước: 1 bước tải WordPress chung (pha "Pre"), 10 site × 12 bước chuẩn bị (bước 2 tới 121, pha A), 2 bước reload (pha B), 10 bước cài WordPress (pha C). Đó đúng là thứ tự `layoutBatch` trải lô ra (mục 3.7). Bước 121 vẫn có `$ nginx -t` mà không có `systemctl reload nginx`: mục 3.12 giải thích.

Sau đó kịch bản kiểm máy không đổi gì (M6.log dòng 699 tới 705): `user tp_: 0`, `DB tp_: 0`, `/var/www` chỉ có `html`, `sites-enabled` chỉ có `tpan-default.conf`, và `tpan.db` vẫn 1 job (dòng 732 tới 734).

### 2.3 Tạo 10 site trong một job

```bash
scripts/vm.sh root tpan apply -f /root/e2e-sites.yaml --yes --json
```

Phần tiến trình in ra stderr (M6.log dòng 740 tới khoảng 1083), rút đoạn đầu và đoạn giữa:

```
Bắt đầu job 82ebbaa550b52ffc
- Tải WordPress 7.1.5 vào /var/cache/tpan ...
  xong
- Tạo user Linux tp_s3_example_com_25da68 cho site ...
- Tạo user Linux tp_s1_example_com_090c1b cho site ...
- Tạo user Linux tp_s2_example_com_36299e cho site ...
  xong
- Tạo thư mục /var/www/s3.example.com ...
...
- Kiểm cấu hình rồi nạp lại php8.3-fpm một lần cho cả lô ...
  xong
- Kiểm cấu hình rồi nạp lại nginx một lần cho cả lô ...
  xong
- Cài WordPress cho s3.example.com ...
- Cài WordPress cho s1.example.com ...
- Cài WordPress cho s2.example.com ...
```

Ba dòng "Tạo user Linux" liền nhau trước một chữ "xong", rồi s3 chạy trước s1: đó là dấu hiệu nhìn thấy được của việc 3 site chạy song song. Các dòng "xong" của các site xen kẽ nhau vì cùng in ra một chỗ.

Phần JSON trên stdout, kịch bản đã lọc qua `jq` cho gọn (hàm `apply_json`; M6.log dòng 1091 tới 1129):

```
  "job": {
    "id": "82ebbaa550b52ffc",
    "kind": "apply",
    "target": "10 site",
    "status": "succeeded",
    "started": "2026-11-19T22:36:01.368229633+07:00",
    "finished": "2026-11-19T22:36:30.28197723+07:00",
    "error": null
  },
  "units": [
    "s1.example.com succeeded",
    ...
    "s10.example.com succeeded"
  ],
  ...
  "isolation": "90/90 phép kiểm cô lập đạt",
  "ssl": []
}
Mật khẩu quản trị trong output --json: 10; mỗi cái đúng một lần: 1
[2026-11-19 22:36:30] (mã thoát 0)
THỜI GIAN apply 10 site (đo từ Mac, gồm multipass exec): 29 giây
```

Hai con số thời gian: 29 giây đo từ Mac (gồm cả thời gian `multipass exec` mở lệnh), và 28,9 giây là thời gian của chính job, tính từ cột `started_at`, `finished_at` trong `tpan.db` (M6.log dòng 1170: `thời gian job: 28.9 giây`). Khi người review hỏi "10 site mất bao lâu", con số của tpan là 28,9 giây. Các lần chạy trước trên cùng máy ảo đo được 27,8 và 30,8 giây (ghi ở `docs/thay-doi/M6.md`; bản M6.log có 27,8 giây nằm trong lịch sử git ở commit `ac432d8`). Con số dao động vài giây giữa các lần.

`90/90`: với 10 site mới, mỗi cặp hai site khác nhau có một phép kiểm "site X không đọc được `wp-config.php` của site Y", 10 × 9 = 90.

### 2.4 Đếm reload: sự kiện của job và journalctl

Đây là phần "Xong khi" của M6: tạo 10 site một lượt chỉ reload một lần. Kịch bản kiểm bằng hai nguồn độc lập.

Nguồn 1: bảng `job_events` trong `tpan.db`. Kịch bản lấy mọi sự kiện `command` của job và giữ lại dòng có `systemctl`, `nginx -t`, `php-fpm`. Output rút gọn (M6.log dòng 1167 tới 1218):

```
job apply 82ebbaa550b52ffc succeeded | target: 10 site
bắt đầu: 2026-11-19T15:36:01.368229633Z | xong: 2026-11-19T15:36:30.281977230Z
thời gian job: 28.9 giây
số lệnh (sự kiện command) của job: 275
  command file:/etc/php/8.3/fpm/pool.d/tpan-s3_example_com_25da68.conf php-fpm8.3 -t
  command file:/etc/php/8.3/fpm/pool.d/tpan-s2_example_com_36299e.conf php-fpm8.3 -t
  command file:/etc/php/8.3/fpm/pool.d/tpan-s1_example_com_090c1b.conf php-fpm8.3 -t
  command file:/etc/nginx/sites-available/tpan-s2.example.com.conf nginx -t
  command symlink:/etc/nginx/sites-enabled/tpan-s2.example.com.conf nginx -t
  ...
  command symlink:/etc/nginx/sites-enabled/tpan-s10.example.com.conf nginx -t
  command service.reload:php8.3-fpm php-fpm8.3 -t
  command service.reload:php8.3-fpm systemctl reload php8.3-fpm
  command service.reload:nginx nginx -t
  command service.reload:nginx systemctl reload nginx
  unit.done s1.example.com {"target":"s1.example.com","status":"succeeded"}
  unit.done s3.example.com {"target":"s3.example.com","status":"succeeded"}
  unit.done s2.example.com {"target":"s2.example.com","status":"succeeded"}
  unit.done s4.example.com {"target":"s4.example.com","status":"succeeded"}
  unit.done s5.example.com {"target":"s5.example.com","status":"succeeded"}
  unit.done s6.example.com {"target":"s6.example.com","status":"succeeded"}
  unit.done s9.example.com {"target":"s9.example.com","status":"succeeded"}
  unit.done s7.example.com {"target":"s7.example.com","status":"succeeded"}
  ...
  => ĐẠT: sự kiện của job: đúng 1 lệnh `systemctl reload nginx`
  => ĐẠT: sự kiện của job: đúng 1 lệnh `systemctl reload php8.3-fpm`
  => ĐẠT: sự kiện của job: không có lệnh reload hay restart nào khác
```

Đọc kỹ: có 30 lần kiểm cú pháp ở pha A (10 file pool với `php-fpm8.3 -t`, 10 file vhost và 10 link với `nginx -t`), rồi thêm một lần kiểm và một lần reload cho mỗi dịch vụ ở pha B. "Reload một lần" không có nghĩa "kiểm một lần". Thứ tự `unit.done` (s1, s3, s2, s4, s5, s6, s9, s7...) khác thứ tự trong file, và khác cả lần chạy trước (lần đó s3 báo trước): site nào xong trước thì báo trước. Thứ tự các lệnh kiểm ở pha A cũng đổi giữa các lần chạy vì cùng lý do: site nào xong trước thì báo trước.

Nguồn 2: journal của systemd, không qua tpan. Lệnh trong kịch bản (hàm `reload_count`):

```bash
scripts/vm.sh root bash -c 'for u in "$1" "$2"; do echo "== journalctl -u $u --since \"$3\" --until \"$4\""; journalctl -u "$u" --since "$3" --until "$4" --no-pager -o short-iso | grep -vE "^-- " ; done' _ nginx php8.3-fpm "2026-11-19 22:36:00" "2026-11-19 22:36:31"
```

Output (M6.log dòng 1223 tới 1234):

```
== journalctl -u nginx --since "2026-11-19 22:36:00" --until "2026-11-19 22:36:31"
2026-11-19T22:36:12+07:00 tpan-m6 systemd[1]: Reloading nginx.service - A high performance web server and a reverse proxy server...
2026-11-19T22:36:12+07:00 tpan-m6 nginx[14593]: 2026/11/19 22:36:12 [notice] 14593#14593: signal process started
2026-11-19T22:36:12+07:00 tpan-m6 systemd[1]: Reloaded nginx.service - A high performance web server and a reverse proxy server.
== journalctl -u php8.3-fpm --since "2026-11-19 22:36:00" --until "2026-11-19 22:36:31"
2026-11-19T22:36:12+07:00 tpan-m6 systemd[1]: Reloading php8.3-fpm.service - The PHP 8.3 FastCGI Process Manager...
2026-11-19T22:36:12+07:00 tpan-m6 systemd[1]: Reloaded php8.3-fpm.service - The PHP 8.3 FastCGI Process Manager.
2026-11-19T22:36:12+07:00 tpan-m6 php-fpm8.3[13273]: [19-Nov-2026 22:36:12] NOTICE: using inherited socket fd=8, "/run/php/php8.3-fpm.sock"
  => ĐẠT: journal của nginx trong khoảng job: đúng 1 dòng Reloaded
  => ĐẠT: journal của php8.3-fpm trong khoảng job: đúng 1 dòng Reloaded
  => ĐẠT: journal: không dịch vụ nào bị dừng hay khởi động lại trong khoảng job
```

Lần reload duy nhất là lúc 22:36:12. Job bắt đầu 22:36:01 và xong 22:36:30, nên đọc ra được: pha A (chuẩn bị 10 site) mất khoảng 11 giây, pha C (cài WordPress và kiểm chứng 10 site) khoảng 18 giây.

Sau đó (M6.log dòng 1252 tới 1285) cả 10 site trả HTTP 200 với tiêu đề là chính domain, và `runuser -u tp_s1_example_com_090c1b -- cat /var/www/s2.example.com/public/wp-config.php` trả `Permission denied` theo cả hai chiều.

### 2.5 Chạy lại, và `tpan export`

```bash
scripts/vm.sh root tpan apply -f /root/e2e-sites.yaml --yes
scripts/vm.sh root tpan export
```

Output (M6.log dòng 1317 tới 1384):

```
DOMAIN                                   VIỆC             LÝ DO
s1.example.com                           bỏ qua (đã có)   
...
s10.example.com                          bỏ qua (đã có)   

Không có gì thay đổi.
```

```
# File này do "tpan export" sinh từ trạng thái hiện tại của máy.
# Chỉ có domain và ssl: tpan không lưu title và admin_email của từng site,
# nên hai trường đó không có ở đây. Muốn tạo site mới từ file này thì thêm
# admin_email (ở defaults hoặc từng dòng) và title nếu cần.
sites:
  - domain: s1.example.com
  - domain: s10.example.com
  ...
```

Chạy lại không tạo job mới (`tpan.db` vẫn 2 job, dòng 1361 tới 1363). Đưa chính file export vào `apply --dry-run` cũng ra 10 dòng "bỏ qua" (dòng 1388 tới 1404).

### 2.6 Đưa vào thùng rác (e2e bước 7, nửa đầu)

Trước khi xoá, kịch bản để lại hai dấu trong `s2.example.com`: một bài viết trong DB (`wp post create`, id 5) và một file `e2e-dau.txt` (M6.log dòng 1406 tới 1800), rồi tính mã băm của cả cây thư mục và đọc trạng thái khoá trước khi xoá (dòng 1802 tới 1819):

```
e81e72d694f8d1efa5ce623aabe98fab4ab1beeb24484a3dcf056251ccb96482
3785
...
Account expires						: never
tp_s2_example_com_36299e	NULL
```

`NULL` nghĩa là user DB chưa từng bị khoá: khoá `account_locked` chưa có trong cột JSON. Sau một lần khoá rồi mở thì nó là `0` (mục 2.7).

```bash
scripts/vm.sh root tpan site rm s2.example.com --yes
```

Output (M6.log dòng 1824 tới 1843):

```
Bắt đầu job dc57bf51bfda062b
- Tắt vhost của s2.example.com ...
  xong
- Gỡ pool PHP-FPM tpan-s2_example_com_36299e ...
  xong
- Khoá user DB tp_s2_example_com_36299e của site s2.example.com ...
  xong
- Khoá user Linux tp_s2_example_com_36299e của s2.example.com ...
  xong
- Chuyển thư mục của s2.example.com vào thùng rác ...
  xong
  [đạt] Domain s2.example.com không còn được phục vụ

Job dc57bf51bfda062b: thành công
Kiểm chứng:
  [đạt] Domain s2.example.com không còn được phục vụ: Domain không còn được phục vụ (curl mã thoát 52, HTTP 000)
Site s2.example.com đã vào thùng rác; xoá hẳn lúc 2026-11-26 22:36. Khôi phục: sudo tpan site restore s2.example.com
```

Job mất 0,1 giây (dòng 1876). Năm bước theo đúng thứ tự của mục 6.4. Các lệnh của job (dòng 1878 tới 1892) có `usermod --expiredate 1 tp_s2_example_com_36299e`, không có `usermod -L` (mục 5 nói vì sao).

Kiểm từng thứ bằng lệnh của Linux, không qua tpan:

```bash
scripts/vm.sh root bash -c 'chage -l tp_s2_example_com_36299e | grep -i "^Account expires"; mariadb -N -B -e "SELECT User, JSON_VALUE(Priv, \"\$.account_locked\") FROM mysql.global_priv WHERE User = \"tp_s2_example_com_36299e\""'
```

```
Account expires						: Jan 02, 1970
tp_s2_example_com_36299e	1
```

User Linux đã hết hạn từ ngày 2/1/1970, user DB có `account_locked = 1` (M6.log dòng 1913 tới 1914).

```bash
scripts/vm.sh root bash -c 'ls -ld /var/lib/tpan/trash /var/lib/tpan/trash/s2_example_com_36299e /var/www/s2.example.com 2>&1'
```

```
ls: cannot access '/var/www/s2.example.com': No such file or directory
drwx------ 3 root                     root     4096 Nov 19 22:36 /var/lib/tpan/trash
drwxr-s--- 4 tp_s2_example_com_36299e www-data 4096 Nov 19 22:36 /var/lib/tpan/trash/s2_example_com_36299e
```

Thư mục site giữ nguyên chủ và quyền 2750 của nó, nhưng nằm trong `/var/lib/tpan/trash` quyền 700 của root, nên web server và user của site khác không vào được. Tên thư mục là slug `s2_example_com_36299e`, không phải domain. Trong `/etc/nginx/sites-available` vẫn còn `tpan-s2.example.com.conf`, còn `sites-enabled` và `pool.d` thì không (M6.log dòng 1919 tới 1964).

Mã băm cả cây trong thùng rác là `e81e72d694f8d1efa5ce623aabe98fab4ab1beeb24484a3dcf056251ccb96482` với 3785 file, giống hệt trước khi xoá (dòng 1806 và 1970).

`scripts/vm.sh root runuser -u tp_s2_example_com_36299e -- id -un` vẫn in `tp_s2_example_com_36299e` (dòng 1975 tới 1977): root vẫn chạy lệnh được dưới một user đã hết hạn. Nhờ vậy WP-CLI của tpan không bị ảnh hưởng.

```
DOMAIN                                   XOÁ LÚC          HẠN XOÁ HẲN
s2.example.com                           2026-11-19 22:36 2026-11-26 22:36
```

Đó là `tpan trash list` (dòng 1981 tới 1982). `tpan health` ngay sau đó không báo lỗi, không báo lệch, không mở sự cố nào (dòng 2026 tới 2030).

### 2.7 Khôi phục (e2e bước 7, nửa sau)

```bash
scripts/vm.sh root tpan site restore s2.example.com --yes
```

Output rút gọn (M6.log dòng 2034 tới 2099):

```
Bắt đầu job 15593544f6dd2dcf
- Chuyển thư mục của s2.example.com ra khỏi thùng rác ...
  xong
- Mở khoá user Linux tp_s2_example_com_36299e của s2.example.com ...
  xong
- Mở khoá user DB tp_s2_example_com_36299e của site s2.example.com ...
  xong
- Ghi pool PHP-FPM tpan-s2_example_com_36299e cho s2.example.com ...
  xong
- Ghi vhost nginx cho s2.example.com ...
  đã đúng, bỏ qua
- Bật /etc/nginx/sites-enabled/tpan-s2.example.com.conf → /etc/nginx/sites-available/tpan-s2.example.com.conf ...
  xong
  [đạt] Vhost của site có, do tpan quản lý và đang được bật
  [đạt] Socket của pool PHP có, đúng chủ và đúng quyền
  [đạt] Mã nguồn WordPress nguyên vẹn (khớp checksum)
  [đạt] wp-config.php có quyền 0600 và đúng chủ
  [đạt] User DB của site đăng nhập được, chỉ thấy DB của site
  [đạt] Gọi HTTP vào site trả 200 và đúng là WordPress của site
  ...
Job 15593544f6dd2dcf: thành công
```

"Ghi vhost ... đã đúng, bỏ qua": file vhost vẫn nằm trong `sites-available` từ lúc vào thùng rác, nên chỉ cần bật lại link. Kiểm chứng gồm 6 phép của lúc tạo site và 18 phép cô lập với 9 site còn lại.

Sau khôi phục (M6.log dòng 2101 tới 2183): mã băm cây thư mục vẫn `e81e72d6...` với 3785 file; `http://s2.example.com/` trả 200; `e2e-dau.txt` trả đúng chuỗi dấu; `wp post get 5` trả `e2e-m6 dau du lieu s2`; `chage -l` về `never`, `account_locked` về `0`; `/var/lib/tpan/trash` trống. Khôi phục lần hai bị từ chối:

```
Từ chối: site s2.example.com không ở thùng rác; đưa vào thùng rác trước bằng: sudo tpan site rm s2.example.com
```

### 2.8 Xoá hẳn

Xoá hẳn một site đang chạy bị từ chối với mã `site_not_trashed` (M6.log dòng 2187 tới 2198), và `s1.example.com` vẫn trả 200. Xoá hẳn chỉ đi qua thùng rác:

```bash
scripts/vm.sh root tpan site rm s3.example.com --yes
scripts/vm.sh root tpan trash purge s3.example.com --yes
```

Output của lệnh thứ hai (M6.log dòng 2233 tới 2251):

```
Bắt đầu job 6e049f642f6c2631
- Tắt vhost của s3.example.com ...
  đã đúng, bỏ qua
- Gỡ file vhost của s3.example.com ...
  xong
- Gỡ pool PHP-FPM tpan-s3_example_com_25da68 ...
  đã đúng, bỏ qua
- Xoá user DB và DB tp_s3_example_com_25da68 của site s3.example.com ...
  xong
- Xoá hẳn /var/lib/tpan/trash/s3_example_com_25da68 và mọi thứ bên trong ...
  xong
- Xoá hẳn /var/log/tpan/sites/s3.example.com và mọi thứ bên trong ...
  xong
- Xoá user Linux tp_s3_example_com_25da68 ...
  xong

Job 6e049f642f6c2631: thành công
```

Đây là Plan xoá hẳn của M2, chỉ khác thư mục bị xoá là thư mục trong thùng rác. Hai bước đầu và bước pool tự bỏ qua vì lúc vào thùng rác đã làm rồi. Kịch bản kiểm 12 thứ (user, nhóm, DB, user DB, bốn thư mục và file, socket, link, file vhost, dòng trong bảng `sites`): tất cả "không có" (dòng 2302 tới 2315). Purge lần hai bị từ chối: `không có site s3.example.com trong tpan`.

### 2.9 Hết hạn

```bash
scripts/vm.sh root tpan trash settings --days 0
scripts/vm.sh root tpan site rm s4.example.com --yes
scripts/vm.sh root tpan trash list
scripts/vm.sh root tpan trash purge --expired --yes
```

Output (M6.log dòng 2331 tới 2420, rút gọn):

```
Số ngày giữ site trong thùng rác: 0 (đổi số này chỉ áp cho site vào thùng rác sau đó).
...
Site s4.example.com đã vào thùng rác; xoá hẳn lúc 2026-11-19 22:37. Khôi phục: sudo tpan site restore s4.example.com
...
DOMAIN                                   XOÁ LÚC          HẠN XOÁ HẲN
s4.example.com                           2026-11-19 22:37 2026-11-19 22:37
...
Bắt đầu job 1f42f2b297044974
- Tắt vhost của s4.example.com ...
  đã đúng, bỏ qua
...
== s4.example.com

Job 1f42f2b297044974: thành công
```

Đặt 0 ngày thì hạn xoá hẳn trùng lúc xoá, nên `--expired` xoá ngay. Chạy `--expired` lần nữa in `Không có site nào quá hạn giữ.` (dòng 2476). Kịch bản đặt lại 7 ngày ở cuối.

### 2.10 `apply --prune`

File mới bỏ `s9` và `s10` (và `s3`, `s4` đã bị xoá hẳn ở trên):

```bash
scripts/vm.sh root tpan apply -f /root/e2e-sites.yaml --prune --yes
```

Output rút gọn (M6.log dòng 2580 tới 2624):

```
s10.example.com                          vào thùng rác    
s9.example.com                           vào thùng rác    

Bắt đầu job 0182c5c4c94a5969
- Tắt vhost của s9.example.com ...
- Tắt vhost của s10.example.com ...
  xong
...
- Kiểm cấu hình rồi nạp lại php8.3-fpm một lần cho cả lô ...
  xong
- Kiểm cấu hình rồi nạp lại nginx một lần cho cả lô ...
  xong
  [đạt] Domain s10.example.com không còn được phục vụ
== s10.example.com: xong
  [đạt] Domain s9.example.com không còn được phục vụ
== s9.example.com: xong

Job 0182c5c4c94a5969: thành công
Kiểm chứng:
  [đạt] Domain s10.example.com không còn được phục vụ: Domain không còn được phục vụ (curl mã thoát 0, HTTP 404)
  [đạt] Domain s9.example.com không còn được phục vụ: Domain không còn được phục vụ (curl mã thoát 0, HTTP 404)
```

Job 0,2 giây, đúng một `systemctl reload nginx` và một `systemctl reload php8.3-fpm` trong sự kiện của job, journal mỗi dịch vụ đúng một dòng `Reloaded` (dòng 2655 tới 2718). Lúc kiểm chứng trong job, curl nhận HTTP 404; vài giây sau, kịch bản gọi lại thì curl nhận mã thoát 52 (site mặc định, dòng 2728 tới 2734). Mục 6, câu 9 giải thích vì sao. Chạy lại `apply --prune` cùng file: `Không có gì thay đổi.` (dòng 2783 tới 2785).

### 2.11 API

Sau `tpan setup`, kịch bản gọi API từ Mac (M6.log dòng 2847 tới 3124):

- `POST /api/v1/apply?dry_run=1` với `s1` (đã có) và `s11` (mới): HTTP 200, `s1.example.com skip`, `s11.example.com create`, không tạo user, không tạo job.
- `DELETE /api/v1/sites/s5.example.com`: HTTP 202, job `site.rm` thành công, curl vào s5 nhận mã 52.
- `GET /api/v1/trash`: 200, có s5, s9, s10, `"days": 7`.
- `POST /api/v1/sites/s5.example.com/restore`: 202, job `site.restore` thành công, s5 trả 200 lại.
- `DELETE /api/v1/trash/s1.example.com?dry_run=1` (site đang chạy): 409 `site_not_trashed`.
- `GET /api/v1/export`: 200, `content-type: text/yaml`, `content-disposition: attachment; filename="sites.yaml"`.

### 2.12 Apache

Lượt Apache từ máy sạch (M6.log dòng 3125 tới 3712): `apply` 3 site trong 12,5 giây (dòng 3503), đúng một `systemctl reload apache2` và một `systemctl reload php8.3-fpm`. Ở pha A, bước kiểm của mỗi file là `apachectl configtest` thay cho `nginx -t`. `site rm s2.example.com` xong thì kiểm chứng nhận HTTP 403 (site mặc định của Apache), `restore` thì 200 lại, mã băm cây thư mục khớp.

### 2.13 Site có SSL: vào thùng rác, khôi phục, xoá hẳn (hồi quy M5)

`M6.log` không có site SSL. Phần này lấy từ `docs/bang-chung/M6-hoi-quy-m5.log`: kịch bản `scripts/e2e-m5.sh all` của M5, chạy lại trên tpan của M6 ngày 19/11/2026 từ 22:19:38 tới 22:23:40, 256 khẳng định đạt, 0 không đạt. Kịch bản đó xin chứng chỉ cho `s1.example.com` bằng pebble (máy chủ ACME thử chạy trong máy ảo, xem `05-m5-ssl-va-bao-mat.md` mục 2.1), rồi:

```bash
scripts/vm.sh root tpan site rm s1.example.com --yes
scripts/vm.sh exec curl -sS -k -m 20 --resolve s1.example.com:443:127.0.0.1 -o /dev/null -w '%{http_code}\n' https://s1.example.com/
scripts/vm.sh root tpan site restore s1.example.com --yes
```

Output rút gọn, lượt Nginx (M6-hoi-quy-m5.log dòng 1465 tới 1617):

```
serial=77755EE3658C5B7B
...
Job 15b552eb8d20a679: thành công
Kiểm chứng:
  [đạt] Domain s1.example.com không còn được phục vụ: Domain không còn được phục vụ (curl mã thoát 52, HTTP 000)
...
  => ĐẠT: job site.rm không có lệnh certbot nào (chứng chỉ giữ để khôi phục)
curl: (35) OpenSSL/3.0.13: error:0A000458:SSL routines::tlsv1 unrecognized name
000
...
- Ghi vhost nginx có HTTPS cho s1.example.com ...
  đã đúng, bỏ qua
...
  [đạt] Chứng chỉ SSL có, khớp tên miền và còn hạn: Chứng chỉ khớp s1.example.com, còn 1825 ngày, do Pebble Intermediate CA 76435d cấp
...
  [đạt] Gọi HTTPS vào site trả 200 và đúng là WordPress của site: Site trả 200 qua https, đúng là WordPress của s1.example.com
...
serial=77755EE3658C5B7B
```

Đọc ra: lúc ở thùng rác, file vhost 443 và chứng chỉ giữ nguyên (cùng sha256 của file vhost, cùng serial), chỉ link ở `sites-enabled` bị gỡ; `https://` không còn trả 200 (nginx từ chối bắt tay vì không còn vhost nào nhận tên đó, curl mã thoát 35). Khôi phục bật lại đúng vhost 443 đó, kiểm chứng có thêm phép kiểm chứng chỉ và https, và cổng 443 phục vụ đúng serial cũ. Lượt Apache (dòng 3799 tới 3952) cho cùng kết quả, chỉ khác lúc ở thùng rác `https://` trả 403 và kiểm chứng `http.gone` nhận HTTP 403.

Xoá hẳn site có SSL (dòng 1620 tới 1722):

```
- Xoá user Linux tp_s1_example_com_090c1b ...
  xong
- Xoá chứng chỉ của s1.example.com ...
  xong

Job 1440066ebd16467a: thành công
...
  command ssl.delete:s1.example.com certbot delete --cert-name s1.example.com --non-interactive
...
No certificates found.
```

`certbot delete` là bước cuối của `purgeSteps` (mục 3.4), và sau đó `certbot certificates` không còn chứng chỉ nào. `site rm` thì không có lệnh certbot nào.

### 2.14 Chưa chạy

Những thứ sau có trong mã nhưng không có trong M6.log hay các log hồi quy:

- Bộ hẹn giờ dọn thùng rác mỗi giờ trong `tpan serve` (`internal/server/trash_timer.go`). Chỉ lệnh tay `trash purge --expired` được chạy. Cách tự kiểm: đặt `--days 0`, `site rm` một site, để `tpan serve` chạy hơn 3 phút, rồi `scripts/vm.sh root journalctl -u tpan --since "-5 min"` và xem nhật ký có thao tác `trash.purge_expired` với IP `timer`.
- Chế độ dán danh sách trên giao diện (`web/src/screens/tao-site/DanhSach.tsx`). Chỉ API mà nó gọi (`POST /api/v1/apply?dry_run=1`) được chạy.
- Một site lỗi giữa lô: lỗi ở pha A, lỗi ở pha C rồi chạy lại reload, lỗi ở pha B làm hoàn tác cả lô. Chỉ có unit test: `TestBatchPrepareFailUndoesOnlyThatUnit`, `TestBatchFinishOrVerifyFailRecommits`, `TestBatchCommitFailUndoesEverything` trong `internal/engine/batch_test.go`, và `TestApplyOneFailsOthersStay` trong `internal/core/apply_test.go`. Mình chạy lại trên Mac lúc viết file này: `go test ./internal/engine/ -run TestBatch -v` in 14 dòng `--- PASS` (từ `TestBatchAllSucceedCommitOnceInOrder` tới `TestBatchLockError`) rồi dòng `ok` của gói `internal/engine` (0,992 giây); `go test ./internal/core/ -run TestApplyOneFailsOthersStay -v` in `--- PASS: TestApplyOneFailsOthersStay (0.02s)`. Unit test chạy trên `FakeSystem`, không có nginx hay MariaDB thật.
- `/var/www` và `/var/lib/tpan` khác phân vùng (lỗi `EXDEV`). Máy ảo chỉ có một phân vùng. Cách tpan cư xử khi đó (từ chối, chưa chép rồi xoá) đã ghi ở `docs/thay-doi/M6.md`.
- Khôi phục khi `/var/www/<domain>` đã có lại (từ chối `site_exists_unmanaged`).
- SSL trong `apply` (pha D): file e2e không có `ssl: true`, JSON có `"ssl": []`.

## 3. Đi qua mã theo thứ tự chạy

Ba luồng: thùng rác (3.1 tới 3.4), hàng loạt (3.5 tới 3.10), và các Step mới (3.11 tới 3.14). Bộ hẹn giờ ở 3.15.

### 3.1 `tpan site rm`: `cmd/tpan/site.go`, `siteRmCmd` rồi `runSiteJob`

`siteRmCmd` không tự làm gì nhiều. Nó đưa cho `runSiteJob` (hàm chung của `site rm`, `site restore`, `trash purge <domain>`) một hàm dựng job:

```go
return runSiteJob(cmd, e, &f, args[0], "site.rm",
	func(ctx context.Context, s *core.Sites, d validate.Domain) (siteJob, error) {
		rm, err := s.PrepareRemove(ctx, d)
		if err != nil {
			return siteJob{}, err
		}
		return siteJob{plan: rm.Plan, preview: rm.Preview, execute: rm.Execute}, nil
	},
```

`runSiteJob` làm khuôn quen thuộc từ M2: kiểm domain, đòi root (trừ `--dry-run`), mở DB, gọi hàm dựng job, xem trước hoặc hỏi lại, chạy, ghi nhật ký. Hàm thứ hai (`after`) in câu "đã vào thùng rác; xoá hẳn lúc ..." bằng cách đọc lại `PurgeAt` từ DB.

Tên hàm `PrepareRemove` giữ nguyên từ M2 dù nghĩa đã đổi, để công cụ `propose_site_remove` của M7 và các chỗ gọi khác không phải sửa.

### 3.2 `internal/core/site.go`: `PrepareRemove` và `trashSteps`

`PrepareRemove` từ chối `site_trashed` nếu site đã ở thùng rác, rồi dựng Plan `site.rm` bằng `trashSteps`, kèm một phép kiểm chứng `siteGone`. Phần đáng đọc là `trashSteps`:

```go
webAbsent, err := steps.SiteWebAbsent(l, string(web), php)
...
for _, st := range webAbsent {
	a, ok := st.(*steps.Absent)
	...
	if a.Path == conf {
		continue
	}
	a.Why = why
	ss = append(ss, a)
}
if len(ss) != 2 {
	return nil, fmt.Errorf("bước gỡ web của %s: cần đúng hai bước (link vhost, pool), có %d", d, len(ss))
}
```

Nó lấy ba bước gỡ web của M2 (gỡ link, gỡ file vhost, gỡ pool), bỏ bước gỡ file vhost trong `sites-available`. Lý do: khôi phục chỉ cần bật lại link. Dòng `len(ss) != 2` là cái chốt: nếu ai đó đổi `SiteWebAbsent` mà quên chỗ này, `site rm` báo lỗi ngay thay vì đưa vào thùng rác một site vẫn còn được phục vụ. Sau hai bước đó là `DBUserLock`, `SiteUserLock`, `MoveTree` (mục 3.11 và 3.14).

Mọi bước của Plan này hoàn tác được, nên Plan có kiểm chứng. `siteGone` (cuối `internal/core/trash.go`) gọi `curl --resolve <domain>:80:127.0.0.1` và đạt khi: HTTP 404, hoặc nginx đóng kết nối (curl mã thoát 52), hoặc apache trả 403. Không đạt thì engine hoàn tác cả Plan, site chạy lại như cũ. Không dùng được `checks.HTTPDefault` của M1 vì phép đó gọi IP không kèm tên miền, nên không biết vhost của chính domain này còn bật không.

Phép kiểm này không gọi curl một lần rồi phán. `goneCheck.Run` gọi `once` (một lần curl và phán), và nếu kết quả là `Fail` thì gọi lại sau mỗi `goneEvery` (200 ms), tới khi đạt hoặc hết `goneWait` (5 giây); điều kiện dừng là `if r.Status != checks.Fail || time.Now().After(deadline)`. Kết quả `Unknown` (curl không chạy được) trả ngay. Lý do: `systemctl reload nginx` trả về trước khi nginx thật sự dùng cấu hình mới, nên request gửi ngay sau đó vẫn được trả lời theo cấu hình cũ. Mục 5, chỗ sai 4, kể chuyện này được phát hiện thế nào. Test: `TestSiteGoneCheckWaitsForReload` trong `internal/core/trash_test.go` (curl giả trả 301 hai lần rồi mã 52: phải đạt ở lần gọi thứ ba; trả 301 mãi: phải `Fail` sau trần chờ, không treo).

### 3.3 `markTrashed`: ghi trạng thái và hạn xoá hẳn

Job thành công thì `SiteRemove.Execute` gọi `markTrashed` (`internal/core/trash.go`):

```go
func (s *Sites) markTrashed(ctx context.Context, st site.Site) error {
	days, err := s.TrashDays(ctx)
	if err != nil {
		return err
	}
	now := s.now().UTC()
	st.Status, st.TrashedAt, st.PurgeAt = site.Trashed, now, now.AddDate(0, 0, days)
	if err := s.Store.SaveSite(ctx, st); err != nil {
		return fmt.Errorf("ghi site %s là đã vào thùng rác: %w", st.Domain, err)
	}
	return nil
}
```

Hạn xoá hẳn được tính một lần, lúc vào thùng rác, và lưu ở cột `sites.purge_at` (migration 600). Vì vậy đổi số ngày giữ chỉ áp cho site vào thùng rác sau đó. `TrashDays` đọc khoá `trash.days` trong bảng `settings`, chưa đặt thì 7, giá trị hỏng (không phải số, ngoài 0..365) thì báo lỗi chứ không đoán: đoán sai là xoá hẳn sớm. `markTrashed` dùng chung cho `site rm` và cho unit thùng rác của `apply --prune`, để hai đường ghi DB giống hệt nhau.

### 3.4 `internal/core/trash.go`: khôi phục, xoá hẳn, hết hạn

`PrepareRestore` đòi site đang ở thùng rác (`findTrashed`, không thì `site_not_trashed`), và từ chối `site_exists_unmanaged` nếu `/var/www/<domain>` đã có lại. Bước `MoveTree` cũng sẽ dừng trong trường hợp đó, nhưng từ chối ở đây thì người dùng biết trước khi bấm. Các bước làm ngược `trashSteps`:

```go
ss := []engine.Step{
	&steps.MoveTree{From: site.TrashDir(l), To: l.Dir,
		Title: i18n.T("Chuyển thư mục của "+d+" ra khỏi thùng rác", "Move the directory of "+d+" out of the trash")},
	&steps.SiteUserLock{Layout: l, Unlock: true},
	&steps.DBUserUnlock{Layout: l},
	pool,
}
return append(ss, vhost...), nil
```

`pool` và `vhost` dựng y như lúc tạo site (`steps.SitePool`, `siteVhost`), nên site có SSL nhận lại vhost 443. Kiểm chứng là `siteChecks`, đúng bộ phép của lúc tạo site. `DBUserUnlock` có sẵn từ M4 (sửa tự động).

`PreparePurge` cũng đòi `findTrashed`: xoá hẳn chỉ đi qua thùng rác. `purgeSteps` là Plan xoá hẳn của M2 với một chỗ khác: `RemoveTree{Path: site.TrashDir(l), Under: site.TrashRoot}` thay cho thư mục trong `/var/www`. Plan này không có kiểm chứng: kiểm chứng lỗi thì engine hoàn tác, mà hoàn tác lúc này chỉ trả lại file vhost cho một site đã mất DB và file, tệ hơn là để yên. Job thành công thì xoá dòng trong bảng `sites`.

`Expired(now)` lấy các site trong thùng rác có `PurgeAt` khác rỗng và không sau `now`. Site có `PurgeAt` rỗng (dòng hỏng) không bao giờ bị xoá tự động. `PurgeExpired` xoá lần lượt từng site, mỗi site một job; site lỗi thì ghi lại rồi đi tiếp site sau.

### 3.5 `tpan apply`: `cmd/tpan/apply.go`, `readSitesFile` rồi `core.ParseSitesFile`

`applyCmd` đọc file trước mọi việc khác, kể cả trước khi đòi root, để file sai bị từ chối sớm. `-f -` đọc từ stdin; khi đó stdin đã hết nên không hỏi "Chạy? [y/N]" được, và lệnh đòi `--yes` hoặc `--dry-run` (từ chối `confirm_required`).

`ParseSitesFile` trong `internal/core/sitesfile.go` dùng thư viện `go.yaml.in/yaml/v3` v3.0.6 (thư viện chuẩn của Go không đọc YAML):

```go
raw, err := io.ReadAll(io.LimitReader(r, MaxSitesFileBytes+1))
...
if len(raw) > MaxSitesFileBytes {
	return SitesFile{}, sitesFileErr(...)
}
dec := yaml.NewDecoder(bytes.NewReader(raw))
dec.KnownFields(true)
```

Ba chặn: đọc tối đa 1 MiB cộng 1 byte (đọc được byte thứ thừa là file quá cỡ; `-f /dev/zero` không làm tpan chạy bằng root đọc vô hạn); `KnownFields(true)` để gõ nhầm `admin_mail` bị báo lỗi thay vì bị lặng lẽ bỏ qua; và một lần `Decode` thứ hai phải gặp `io.EOF`, để file có hai tài liệu YAML (ngăn bằng `---`) không bị bỏ nửa sau. `Resolve` kiểm từng dòng (domain, email, tiêu đề, không trùng, tối đa 500 site) và báo lỗi kèm "dòng N".

### 3.6 `internal/core/apply.go`: `PrepareApply` rồi `applyBatch`

`PrepareApply` chia từng domain thành bốn việc: `create`, `skip` (đã có và đang active), `trash` (ngoài file, có `--prune`), `refused` (không làm được, kèm mã và lý do). Một dòng lỗi không làm hỏng cả lô; chỉ từ chối cả lô khi không dựng được gì. Có một từ chối đặc biệt: danh sách rỗng cộng `--prune` bị từ chối `prune_empty`, vì một file rỗng (gõ nhầm, file bị cắt cụt) cộng `--prune` sẽ đưa mọi site vào thùng rác.

Unit tạo site được dựng bằng chính `PrepareAdd` của M2 (cùng điều kiện đầu, cùng Plan), rồi `applyBatch` cắt Plan đó ra các pha:

```go
for _, st := range add.Plan.Steps {
	// WPArchive lên Pre (một lần cho cả lô: trùng StepID giữa các
	// unit thì engine từ chối); cài WordPress xuống pha C, sau khi
	// web server đã nạp vhost ở pha B.
	if _, ok := st.(*steps.WPArchive); ok || st == engine.Step(add.install) {
		continue
	}
	prep = append(prep, st)
}
```

Phần còn lại thành `Prepare: applySerial(mu, prep)` của unit, `add.install` (bước cài WordPress) thành `Finish`, và `siteChecks` (bộ phép kiểm của lúc tạo site) thành `Verify`. Nhờ dựng từ `PrepareAdd`, tạo một site và tạo mười site không thể lệch nhau về các bước. Unit thùng rác dùng nguyên Plan của `PrepareRemove`. Pha B (`b.Commit`) là `steps.PHPReload` cho mỗi bản PHP có trong lô (sắp xếp để thứ tự ổn định), rồi `steps.WebReload`. PHP trước web server, như Plan một site: web server nạp vhost mới thì socket của pool đã có. `Parallel` là 3 (`applyParallel`, mục 7: "tối đa 3 site một lúc").

`applySerial` làm hai việc với các bước pha A: gọi `steps.WithoutReload` để bỏ reload của từng file, rồi bọc `steps.Serial` quanh các bước không được chạy cùng lúc với site khác (đổi user Linux, và mọi bước ghi file có lệnh kiểm). Mục 3.12 giải thích vì sao.

`others` (để kiểm cô lập lúc tạo) bỏ các site mà chính lô này đưa vào thùng rác: thư mục và user của chúng đang được chuyển và khoá song song, kiểm vào lúc đó có thể hỏng oan và hoàn tác một site mới lành. Cô lập giữa các site mới cùng lô thì kiểm sau job (`ApplyResult.Isolation`, chính là `90/90` ở mục 2.3), chỉ báo, không hoàn tác.

### 3.7 `internal/engine/batch.go`: `ExecuteBatch`, pha A và B

Đây là phần mới lớn nhất của M6. Engine của M1 có `Execute(Plan)`: một danh sách bước phẳng, bước nào lỗi thì hoàn tác cả job. `ExecuteBatch(Batch)` thêm khái niệm "unit": mỗi site một unit, hoàn tác riêng. Kiểu `Batch` có `Pre` (chạy một lần trước cả lô), `Units`, `Commit` (pha B, một lần cho cả lô) và `Parallel` (số unit tối đa chạy cùng lúc). Mỗi `Unit` có `Target` (domain), `Prepare` (pha A), `Finish` (pha C) và `Verify` (phép kiểm chạy sau `Finish`).

Trước khi lấy khoá toàn cục, `checkBatch` từ chối Batch sai: không có unit nào, hai unit cùng `Target`, hai bước cùng StepID. Trùng StepID thì sự kiện và bảng kết quả không còn chỉ ra được bước nào của site nào. Đó là lý do `WPArchive` (StepID `wp.archive:7.1.5`, giống nhau cho mọi site) phải lên `Pre`.

`layoutBatch` trải mọi bước ra một danh sách phẳng theo thứ tự Pre, Prepare từng unit, Commit, Finish từng unit, và nhớ mỗi phần nằm ở khoảng nào (`span`). Nhờ vậy mỗi bước có đúng một ô trong `Report.Steps`; goroutine của unit nào chỉ ghi vào ô của unit đó, nên không cần khoá cho Report. Đây cũng là thứ tự 133 bước của bản xem trước ở mục 2.2.

Thân chính là `batchRun.exec`, bỏ bớt chi tiết:

```go
// Pha A.
br.parallel(all, func(k int) {
	if sid, err := br.runSteps(br.ctx, br.l.prep[k], &br.applied[k]); err != nil {
		br.unitFail(k, "prepare", sid, err)
	}
})
...
// Pha B. Lỗi ở đây không chỉ ra được site nào gây ra (nginx -t kiểm cả
// config), nên mọi unit còn sống bị hoàn tác.
if _, err := br.runSteps(br.ctx, br.l.commit, &br.commitApplied); err != nil {
	...
	for i := len(live) - 1; i >= 0; i-- {
		br.unitFail(live[i], "commit", "", err)
	}
```

### 3.8 Pha C, chạy lại pha B, và kết quả job

Pha C chạy `Finish` của từng unit còn sống (song song), rồi `Verify` của chính unit đó. Hỏng ở đâu thì `unitFail` hoàn tác cả `Finish` lẫn `Prepare` của unit đó, theo thứ tự ngược. Sau pha C có một bước dễ bỏ sót:

```go
if undoneInC {
	// File của unit vừa gỡ đã được web server và PHP-FPM nạp ở pha B: chạy
	// lại Commit để chúng thôi phục vụ cấu hình đó.
	uctx, cancel := context.WithTimeout(context.WithoutCancel(br.ctx), undoTimeout)
	var again []int
	if _, err := br.runSteps(uctx, br.l.commit, &again); err != nil {
		br.recommitErr = fmt.Errorf("chạy lại phần chung sau khi hoàn tác: %w", err)
	}
	cancel()
```

Lý do: ở pha A, các bước ghi file không reload (mục 3.12), nên hoàn tác chúng cũng không reload. Với unit hỏng ở pha A thì đúng: file của nó chưa bao giờ được nạp. Nhưng unit hỏng ở pha C thì file của nó đã được nạp ở pha B. Gỡ file mà không reload thì nginx vẫn phục vụ vhost đã gỡ. Nên chạy lại pha B một lần nữa. `WithoutCancel` vì đây là dọn dẹp: người dùng bấm dừng job thì vẫn phải dọn.

Kết quả job: `succeeded` nếu ít nhất một unit thành và không có lần hoàn tác nào lỗi; `rolled_back` nếu không unit nào thành; `undo_failed` nếu có một lần hoàn tác lỗi ở bất kỳ đâu. Khác `Execute`: `Job.Error` liệt kê các site lỗi kể cả khi job `succeeded` (một phần lô thành công). Người đọc xem `Job.Status`, không xem `Error` rỗng hay không.

Mỗi unit có kết quả cuối thì `unitDone` phát sự kiện `unit.done`, nội dung là JSON của `UnitResult` (site, trạng thái, pha lỗi, bước lỗi, lý do). Sự kiện nằm trong bảng `job_events`, nên trang job của giao diện đọc lại được bảng kết quả từng site về sau. Đó là các dòng `unit.done` ở mục 2.4.

### 3.9 `batchRun.parallel`: tối đa 3 cùng lúc

```go
func (br *batchRun) parallel(units []int, fn func(k int)) {
	sem := make(chan struct{}, max(br.b.Parallel, 1))
	var wg sync.WaitGroup
	for _, k := range units {
		// Giữ chỗ trước khi tạo goroutine: Parallel = 1 thì các unit chạy đúng
		// thứ tự trong slice.
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			fn(k)
		})
	}
	wg.Wait()
}
```

`sem` là một channel chứa được 3 phần tử. Muốn chạy một unit thì phải nhét được một phần tử vào; đầy rồi thì vòng `for` đứng chờ tới khi một unit xong và rút ra. `wg.Wait()` chờ mọi unit xong mới sang pha sau. Mục 4.8 nói thêm.

### 3.10 Sau job: `Apply.Execute` và API

`Apply.Execute` (cuối `internal/core/apply.go`) đọc `rep.Units[k]` theo đúng chỉ số của `a.units`: unit tạo site thành công thì `SaveSite` (active) và lấy mật khẩu quản trị; unit thùng rác thành công thì `markTrashed`. Unit tạo site bị hoàn tác ở pha C có thể đã chạy `wp core install`, nên chỉ unit `succeeded` mới trả mật khẩu. Rồi kiểm cô lập giữa các site mới, rồi pha D: mỗi site có `ssl` một job SSL riêng (dùng lại M5), lần lượt.

Phía API, `handleApply` trong `internal/server/api_trash.go` nhận cùng cấu trúc với `sites.yaml` dưới dạng JSON. `?dry_run=1` trả 200 kể cả khi không có gì để làm (để giao diện hiện lý do từng dòng); chạy thật mà không có gì thì 409 `nothing_to_do`. `firstJobOnly` chỉ chuyển sự kiện của job đầu tiên (job hàng loạt) vào luồng sự kiện trực tiếp của trang job, vì các job SSL của pha D có JobID riêng và trang riêng.

Màn "Dán danh sách" (`web/src/screens/tao-site/DanhSach.tsx`) tách ô văn bản thành từng dòng domain, gọi `POST /api/v1/apply?dry_run=1` để xem trước, rồi gửi đúng nội dung đã xem trước (không phải chữ trong ô lúc bấm). Giao diện không gửi `prune`: xoá hàng loạt chỉ có ở dòng lệnh.

### 3.11 `internal/steps/trash.go`: `MoveTree`

`MoveTree` chuyển cả thư mục bằng một lần `system.Rename`. Trước khi làm gì, `validate` đòi `From` và `To` là đường dẫn tuyệt đối sạch, nằm hẳn bên dưới `/var/www` hoặc `site.TrashRoot` (`/var/lib/tpan/trash`), và không cái nào nằm trong cái kia. Một lệnh rename chạy bằng root mà nhận nhầm đường dẫn có thể chuyển mất `/etc`.

`state` đọc máy và chỉ chấp nhận hai tình huống rõ ràng: nguồn còn, đích chưa có (chưa làm); hoặc nguồn mất, đích là thư mục (đã làm). Cả hai cùng có thì báo lỗi "hai bản của cùng một site, tpan không đoán bản nào đúng". Nguồn là symlink thì cũng lỗi, vì rename sẽ chỉ chuyển cái link.

```go
if err := sys.Rename(s.From, s.To); err != nil {
	if errors.Is(err, syscall.EXDEV) {
		err = fmt.Errorf("chuyển %s sang %s: /var/www và /var/lib/tpan nằm trên hai phân vùng khác nhau; tpan chưa hỗ trợ chép rồi xoá: %w", s.From, s.To, err)
	} else {
		err = fmt.Errorf("chuyển %s sang %s: %w", s.From, s.To, err)
	}
	// Engine không Undo bước có Apply lỗi: tự dọn thư mục cha vừa tạo.
	if rerr := removeDirs(sys, created); rerr != nil {
		return errors.Join(err, rerr)
	}
	return err
}
```

Thư mục cha của đích (`/var/lib/tpan/trash`) được tạo với quyền 0700 của root nếu chưa có. `Undo` rename ngược lại, nhưng dừng nếu ở chỗ cũ đã có gì đó: `rename(2)` đè được lên một thư mục rỗng, tpan không đè. Thư mục cha còn đồ của site khác (job hàng loạt) thì để lại, không coi là hoàn tác lỗi.

Tên thư mục trong thùng rác do `site.TrashDir` (`internal/site/trash.go`) đặt: `TrashRoot + "/" + l.Slug.String()`. Mục 5 nói vì sao là slug.

### 3.12 `internal/steps/batch.go`: `WithoutReload` và `Serial`

`WithoutReload` duyệt các bước và xoá trường `Reload` của các bước `*File`, `*Symlink`, `*Absent`, giữ nguyên trường `Test`:

```go
for _, s := range ss {
	switch s := s.(type) {
	case *File:
		s.Reload = nil
	case *Symlink:
		s.Reload = nil
	case *Absent:
		s.Reload = nil
	}
}
```

Giữ `Test` để file sai bị bắt ngay ở bước ghi nó, đúng site gây ra lỗi, và chỉ site đó bị hoàn tác. Đó là 30 lần `nginx -t`, `php-fpm8.3 -t` ở mục 2.4.

`Serial` bọc một bước vào kiểu `batchSerial` (nhúng `engine.Step`, thêm trường `mu *sync.Mutex`), để `Apply` và `Undo` của nó chạy dưới mutex:

```go
func (b *batchSerial) Apply(ctx context.Context, sys system.System) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Step.Apply(ctx, sys)
}
```

Hai việc không được chạy cùng lúc giữa các site:

- `useradd`, `userdel`, `usermod` khoá file `/etc/passwd` trong lúc sửa. Tiến trình thứ hai chạy cùng lúc thì lỗi `cannot lock /etc/passwd`.
- Ghi file config rồi chạy `nginx -t`. Lệnh này kiểm cả config, không chỉ file vừa ghi. Nếu site B đang ghi dở vhost của nó đúng lúc site A chạy `nginx -t`, site A hỏng oan và bị hoàn tác vì lỗi của site B. Cặp "ghi file, kiểm" phải liền một khối.

Vì một mutex chung cho cả lô (tạo trong `applyBatch`), phần lớn các bước ghi config ở pha A thực ra chạy lần lượt. Phần được lợi từ song song là tạo DB, giải nén WordPress, tạo `wp-config.php`, và cả pha C (`wp core install`).

### 3.13 `ConfigReload`: bước của pha B

`ConfigReload` (cùng file `internal/steps/batch.go`) có một chỗ lạ ở `Check`:

```go
func (s *ConfigReload) Check(context.Context, system.System) (bool, error) {
	done := s.reloaded
	s.reloaded = false
	return done, nil
}
```

Reload không có "trạng thái đã đúng" để đọc trên máy, nên `Check` trả `false` để bước luôn chạy. Nhưng `runStep` của engine (`internal/engine/engine.go`) gọi `Check` lần nữa ngay sau `Apply` và báo lỗi "đã chạy xong nhưng kiểm lại vẫn chưa đạt" nếu vẫn `false`. Nên `Apply` thành công đặt cờ `reloaded`, lần `Check` ngay sau trả `true` rồi xoá cờ. Khi pha C cần chạy lại Commit (mục 3.8), `Check` lại là `false` và reload chạy thật. Cờ này không cần khoá vì engine không bao giờ chạy Commit song song. `WebReload` và `PHPReload` dựng `ConfigReload` với đúng lệnh kiểm và lệnh reload mà các bước ghi vhost, pool vẫn dùng (`webVhostOf`, `webPHPCmds` trong `site_web.go`), để không có hai chỗ viết `systemctl reload nginx`.

### 3.14 `DBUserLock`, `SiteUserLock`, và chờ socket sau reload

`DBUserLock` chạy `ALTER USER '<db>'@'localhost' ACCOUNT LOCK` qua stdin của `mariadb` (dùng lại câu SQL của M4 trong `fix.go`). Trạng thái đọc từ `mysql.global_priv`, cột JSON `account_locked`. User DB không có cũng coi là đạt (mục 5 nói vì sao). `Undo` chỉ mở khoá nếu chính `Apply` đã khoá, để không mở một user vốn bị khoá từ trước.

`SiteUserLock` khoá user Linux bằng hạn tài khoản:

```go
func trashUsermod(name string, lock bool) system.Cmd {
	date := ""
	if lock {
		date = "1"
	}
	return system.Cmd{Name: "usermod", Args: []string{"--expiredate", date, name}}
}
```

`--expiredate 1` là hết hạn từ ngày thứ 1 sau 1/1/1970; `--expiredate ""` (một phần tử argv rỗng, không phải chuỗi có dấu nháy) là bỏ hạn. Trạng thái đọc bằng `chage -l`, dòng `Account expires`: `never` là chưa khoá, ngày nào khác là đã khoá. Trước khi đổi gì, `locked` hỏi `getent passwd` để chắc user có và là của tpan (`userOurs`), không khoá nhầm một user trùng tên do người khác tạo.

Chờ socket: lúc tạo site thường hay khôi phục, bước ghi pool (`steps.SitePool` trong `site_web.go`) đặt `WaitFor: l.Socket`. Sau khi reload PHP-FPM, `File.Apply` gọi `waitForPath` (trong `internal/steps/common.go`) để chờ tới khi file socket xuất hiện, tối đa 5 giây, mỗi 100 ms hỏi một lần. Hết giờ thì không báo lỗi: phép kiểm chứng "Socket của pool PHP có" sẽ nói. Mục 5 kể vì sao phải thêm. Trong pha A của job hàng loạt, `WithoutReload` đã bỏ `Reload` nên không chờ.

### 3.15 `internal/server/trash_timer.go`: bộ hẹn giờ

`trashTimer.Run` chờ 3 phút sau khi `tpan serve` khởi động (lúc đó `tpan setup` thường còn giữ khoá), rồi mỗi giờ gọi `round`. Mỗi vòng:

```go
if !s.api.claim() {
	slog.Info("thùng rác: đang có job, bỏ vòng dọn này")
	return false
}
if s.lockHeldElsewhere() {
	s.api.release()
	slog.Info("thùng rác: khoá toàn cục đang bị tiến trình khác giữ, bỏ vòng dọn này")
	return false
}
res, err := t.purge(context.WithoutCancel(ctx), s.now())
```

Xoá hẳn đổi máy, nên chạy chen giữa một job khác là phá luật "cùng lúc chỉ một job". Đang có job thì bỏ vòng, chờ giờ sau: site hết hạn chậm một giờ không hại gì. `WithoutCancel`: xoá hẳn không hoàn tác được, cắt ngang vì `serve` đang tắt là để máy dở dang (DB đã xoá, file còn). Mỗi site ghi một dòng nhật ký với IP `timer` và thao tác `trash.purge_expired`, để người đọc biết không ai bấm gì cả. Phần này chưa chạy trên máy ảo (mục 2.14).

## 4. Kiến thức nền

### 4.1 `rename` và phân vùng

Trên Linux, một thư mục là một mục trong thư mục cha trỏ tới một inode (chỗ lưu thông tin và vị trí dữ liệu). `rename(2)` cùng phân vùng chỉ sửa mục đó: không chép byte nào, 3785 file hay 3 triệu file đều xong tức thì, và không bao giờ có lúc nửa này nửa kia. Khác phân vùng thì inode không mang sang được, `rename` trả lỗi `EXDEV` và không làm gì. Trong mã: `MoveTree.Apply` gọi `sys.Rename` và bắt `syscall.EXDEV`. Tự xem: `scripts/vm.sh root stat -c '%d %i %n' /var/www/s1.example.com` in số thiết bị và số inode; số thiết bị giống `stat` của `/var/lib/tpan` thì cùng phân vùng.

### 4.2 Khoá mật khẩu và hạn tài khoản

`/etc/shadow` có mỗi user một dòng; trường thứ hai là mật khẩu đã băm, trường thứ tám là ngày hết hạn tài khoản (đếm theo ngày từ 1/1/1970). `usermod -L` chỉ thêm `!` vào trước trường mật khẩu; user không có mật khẩu thì trường đó vốn đã là `!`. Hạn tài khoản là chuyện khác: PAM (lớp kiểm đăng nhập của Linux) từ chối đăng nhập của user đã hết hạn, và theo `docs/thay-doi/M6.md` thì cron của user đó cũng bị chặn (M6.log không có lần kiểm cron). Root dùng `runuser` thì bỏ qua kiểm này (M6.log dòng 1984 tới 1986). Trong mã: `SiteUserLock`. Tự xem: `scripts/vm.sh root chage -l tp_s1_example_com_090c1b` và `scripts/vm.sh root passwd -S tp_s1_example_com_090c1b`.

### 4.3 Khoá user trong MariaDB

`ALTER USER ... ACCOUNT LOCK` giữ nguyên user, mật khẩu, quyền và DB; chỉ từ chối đăng nhập mới. Trạng thái nằm trong bảng `mysql.global_priv`, cột `Priv` dạng JSON, khoá `account_locked`: chưa có (`NULL`) khi user chưa từng bị khoá, `1` khi đang khoá, `0` sau khi mở khoá (M6.log dòng 1817, 1914, 2141). Trong mã: `DBUserLock` và `dbLockStateSQL` trong `internal/steps/fix.go`. Tự xem: `scripts/vm.sh root mariadb -N -B -e "SELECT User, JSON_VALUE(Priv, '$.account_locked') FROM mysql.global_priv WHERE User LIKE 'tp_%'"`.

### 4.4 Reload và kiểm cú pháp

`systemctl reload nginx` gửi tín hiệu cho tiến trình chính của nginx: nó đọc lại config, mở worker mới, và để worker cũ phục vụ nốt các kết nối đang mở rồi mới thoát. Không rớt kết nối nào, nhưng mỗi lần reload là một lần đọc lại toàn bộ config. Với PHP-FPM, `systemctl reload` trả về ngay, các pool mới (và socket của chúng) có sau đó một chút. `nginx -t` đọc toàn bộ config (mọi file trong `sites-enabled`), không chỉ một file. Trong mã: `ConfigReload`, `runTest`, `runReload`. Tự xem: `scripts/vm.sh root journalctl -u nginx --since "-10 min" --no-pager` sau một lần `apply` có tạo site.

### 4.5 `/etc/passwd` và khoá file

`useradd`, `usermod`, `userdel` ghi `/etc/passwd`, `/etc/shadow`, `/etc/group`. Để hai lệnh không ghi đè nhau, chúng giữ một khoá trên các file đó trong lúc sửa; lệnh thứ hai không lấy được khoá thì lỗi `cannot lock /etc/passwd` (theo chú thích của `Serial`; mình chưa đo nó chờ bao lâu trước khi bỏ cuộc). Trong mã: `applySerial` bọc `Serial` quanh `*steps.SiteUser`, `*steps.SiteUserAbsent`, `*steps.SiteUserLock`. Tự xem: `scripts/vm.sh root ls -l /etc/passwd /etc/shadow`; khoá chỉ tồn tại trong chớp mắt nên thường không thấy (chưa chạy thử bắt nó).

### 4.6 YAML

YAML là định dạng file cấu hình dựa vào thụt lề; `-` mở một phần tử của danh sách, `#` mở chú thích. Một file có thể chứa nhiều "tài liệu" ngăn bằng dòng `---`. Trong mã: `ParseSitesFile` và `Marshal` (`SetIndent(2)` để file export giống ví dụ ở mục 7). Tự xem: `scripts/vm.sh root tpan export`.

### 4.7 Go: struct nhúng và type switch

`batchSerial` nhúng `engine.Step` (viết tên kiểu không kèm tên trường), nên có sẵn `Info` và `Check` của bước gốc; nó chỉ tự viết lại `Apply` và `Undo`. Đây cũng là cách `emailOverlay` của M5 làm (`00-go-can-ban.md` có phần interface). Hệ quả: bước đã bọc không còn là `*steps.File` nữa, nên `WithoutReload` phải gọi trước `Serial`, và `applySerial` xét kiểu trước khi bọc. `switch s := s.(type)` trong `WithoutReload` là type switch: hỏi giá trị interface đang chứa kiểu cụ thể nào. Tự xem: `go test ./internal/steps/ -run 'TestWithoutReload|TestSerial' -v` trên Mac.

### 4.8 Go: goroutine, channel làm semaphore, `sync.WaitGroup`

`00-go-can-ban.md` mục goroutine nói goroutine là gì và mutex là gì. M6 thêm hai thứ. Channel có sức chứa N (`make(chan struct{}, 3)`) dùng làm semaphore: gửi vào khi đầy thì đứng chờ, nên tối đa N goroutine giữ chỗ cùng lúc. `sync.WaitGroup` đếm số goroutine đang chạy; `wg.Go(f)` vừa tăng đếm vừa chạy `f` trong goroutine mới và giảm đếm khi `f` xong; `wg.Wait()` chờ đếm về 0. Trong mã: `batchRun.parallel`. Tự xem: `go test ./internal/engine/ -run TestBatchParallelLimit -v` (test đếm số unit chạy cùng lúc không bao giờ quá `Parallel`).

## 5. Cách khác đã cân nhắc, và chỗ đã làm sai

### Dùng Plan thường cho hàng loạt

Cách thứ nhất: gọi `site add` mười lần, mười job. Mỗi site có bước ghi pool, ghi vhost, bật link, mỗi bước tự reload, nên mười site là khoảng 20 lần reload nginx và 10 lần reload PHP-FPM, và mỗi job chờ khoá toàn cục của job trước.

Cách thứ hai: gộp mọi bước của mười site vào một Plan phẳng và gọi `engine.Execute`. Một job, nhưng `Execute` hoàn tác cả job khi một bước lỗi: site thứ mười hỏng thì chín site đã xong cũng bị gỡ. Mục 7 đòi "site nào lỗi thì hoàn tác riêng site đó". Và các bước vẫn tự reload.

Vì vậy M6 viết `engine.ExecuteBatch` trong file mới, dùng lại `runStep`, `undoStep` của `engine.go` mà không sửa `engine.go` (M8 và M12 chạy cùng lượt). Ghi ở `docs/thay-doi/M6.md`.

### Mỗi bước của pha A chạy song song không khoá

Bản xem trước của Batch (mục 2.2) cho thấy bước ghi vhost có `nginx -t`. Không bọc `Serial` thì hai site cùng chạy `useradd` dễ gặp `cannot lock /etc/passwd`, và `nginx -t` của site này có thể đọc vhost đang ghi dở của site kia. Cả hai làm hỏng oan một site lành. Mutex làm phần ghi config chạy lần lượt; phần chậm thật (giải nén WordPress, tạo DB, cài WordPress) vẫn song song. `TestSerial` trong `internal/steps/batch_test.go` kiểm hai `Apply` bọc cùng mutex không chồng lên nhau.

### Chép rồi xoá khi khác phân vùng

Mục 6.4 ghi "khác phân vùng thì chép rồi xoá". Mã chưa làm: `MoveTree` gặp `EXDEV` thì báo lỗi rõ và dừng. Chú thích của `MoveTree` chỉ ghi ba điều: chép rồi xoá chưa làm (`ponytail:`), mục 16 của KE-HOACH ghi máy ảo cùng phân vùng (mục 2.1 ở trên cũng thấy vậy), và VPS cần đo. `docs/thay-doi/M6.md` ghi chỗ khác đặc tả này ("Khác phân vùng thì từ chối, chưa chép rồi xoá") kèm hướng nâng cấp về sau: chép bằng `cp -a` rồi xoá, có hoàn tác. Lệnh `df /var/www /var/lib` nằm ở mục "Việc chủ dự án tự làm trên VPS" của cùng file, chạy trước lần `site rm` đầu tiên.

### Id thùng rác là số tự tăng hay domain

Mục 5 của KE-HOACH ghi `/var/lib/tpan/trash/<id>/`. Mã dùng slug (`s2_example_com_36299e`). Slug suy được từ domain nên không cần lưu thêm cột id; slug là duy nhất (cột `slug` của bảng `sites` có `UNIQUE`); một domain chỉ có một dòng trong `sites` nên không bao giờ có hai bản cùng nằm trong thùng rác. Tên user và tên DB của site là `tp_` cộng slug (`tp_s2_example_com_36299e`), nên nhìn thư mục trong thùng rác là biết của user nào. Dùng thẳng domain làm tên thư mục cũng được, nhưng slug (`validate.SlugOf`: 16 ký tự đầu của domain, ký tự nào không phải chữ thường hay số thì đổi thành `_`, cộng `_` và 6 ký tự hex của sha256 domain) chỉ có chữ thường, số và `_`, an toàn hơn khi ghép vào đường dẫn. Ghi ở `docs/thay-doi/M6.md`.

### Chỗ sai 1: `usermod -L` không khoá được gì (commit `1c5a134`)

Mục 6.4 bước 3 ghi `usermod -L`. Bản đầu (commit `2877c2c`) làm đúng như vậy, và `Check` đọc `passwd -S` (cột 2 là `L` thì coi là đã khoá). Thử trên máy ảo thì thấy (ghi trong `docs/thay-doi/M6.md` và chú thích của `SiteUserLock`, không nằm trong `M6.log`): user của site tạo bằng `useradd` không có mật khẩu nên trường mật khẩu trong shadow là `!`, `passwd -S` báo `L` ngay từ đầu. `usermod -L` thoát 0 mà không đổi gì đọc được. `usermod -U` in `usermod: unlocking the user's password would result in a passwordless account.` và vẫn thoát 0, `passwd -S` vẫn `L`. Tức là bước khoá luôn "đã đúng, bỏ qua", và bước mở khoá không mở được gì. Sửa: dùng hạn tài khoản (`--expiredate`), đọc bằng `chage -l`. Bằng chứng sau sửa: `Account expires : Jan 02, 1970` lúc ở thùng rác, `never` sau khôi phục (mục 2.6, 2.7).

### Chỗ sai 2: site lệch không vào được thùng rác (commit `08c73ae`)

Bản đầu của `DBUserLock` báo lỗi khi user DB không có. Một site lệch kiểu "`wp-config.php` có mà user DB mất" (mục 6.2, "Chạy lại") khi đó không vào được thùng rác; mà xoá hẳn chỉ đi qua thùng rác, nên site đó không có đường nào để xoá. Sửa: user không có cũng là đạt, vì không ai đăng nhập được bằng một user không tồn tại, đúng điều bước này muốn. Khôi phục site như vậy thì `DBUserUnlock` vẫn báo lỗi, đúng vì site vẫn lệch. Commit chỉ ghi lý do, không ghi phát hiện bằng cách nào.

### Chỗ sai 3: kiểm chứng của `site restore` hỏng oan vì socket chưa có (commit `9a1cad4`)

Khôi phục chạy bước ghi pool (có reload PHP-FPM), rồi ngay sau đó phép kiểm "Socket của pool PHP có". Trên máy ảo, kiểm chứng này hỏng và job bị hoàn tác dù không có gì sai. Lý do (đo trên máy ảo, ghi trong chú thích của `File.WaitFor` và commit): `systemctl reload php8.3-fpm` chỉ gửi tín hiệu rồi trả về sau khoảng 10 ms, socket của pool mới xuất hiện sau khoảng 175 ms (commit ghi 0,18 giây). Sửa: bước ghi pool chờ socket tối đa 5 giây sau reload (mục 3.14). Vì sao `site add` của M2 không gặp thì mình chỉ suy ra từ thứ tự bước, chưa đo: sau bước pool còn giải nén WordPress và tạo `wp-config.php`, mất vài giây, nên tới lúc kiểm chứng thì socket đã có. Bằng chứng sau sửa: mục 2.7, `[đạt] Socket của pool PHP có, đúng chủ và đúng quyền`. Bài học giống M5: unit test chạy trên `FakeSystem` không có độ trễ của tiến trình thật, chỉ máy ảo mới thấy.

### Chỗ sai 4: site có SSL không vào được thùng rác (commit `b70f462`)

Bản đầu của `siteGone` gọi curl đúng một lần, ngay sau bước gỡ link vhost (có `systemctl reload nginx`). Với site không SSL thì lọt: cấu hình cũ vẫn trả lời, nhưng thư mục đã chuyển đi nên nó trả 404, và 404 được tính là đạt. Lần chạy đầu của hồi quy e2e M5 trên tpan của M6 thì hỏng: site có SSL không vào được thùng rác. Lý do (đo trên máy ảo, ghi trong commit `b70f462`, chú thích của `goneCheck.Run` và `docs/thay-doi/M6.md`): ngay sau `systemctl reload nginx`, request vẫn được trả lời theo cấu hình cũ trong khoảng 0,17 giây. Gọi sau 0,012 giây thì vhost cũ của site có SSL trả 301 (chuyển sang https), không phải 404 hay 444, nên phép kiểm `Fail` và engine hoàn tác cả Plan. Gọi sau 0,168 giây thì đã ra mã 52. Log của lần hỏng đó không được giữ trong repo; `M6-hoi-quy-m5.log` là lần chạy lại sau khi sửa.

Sửa: `goneCheck.Run` gọi lại mỗi 200 ms tới 5 giây khi kết quả là `Fail` (mục 3.2). Bằng chứng sau sửa: mục 2.13, `site rm` của site có SSL thành công ở cả lượt Nginx và lượt Apache, rồi khôi phục với đúng chứng chỉ cũ. `docs/thay-doi/M6.md` ghi thêm: các phép kiểm khác chạy ngay sau reload (ví dụ https lúc khôi phục) chưa được bọc như vậy; hiện chúng đạt vì trước chúng còn có bước chậm.

Chỗ sai 3 và chỗ sai 4 cùng một kiểu: lệnh reload trả về trước khi dịch vụ thật sự dùng cấu hình mới. Với PHP-FPM thì là socket chưa có, với nginx thì là request vẫn theo cấu hình cũ.

## 6. Tự kiểm

1. Vì sao `tpan apply` không gọi `engine.Execute` với một Plan chứa mọi bước của mười site? Nêu hai lý do.
2. Ở mục 2.4 có 30 lần `nginx -t` hoặc `php-fpm8.3 -t` trước lần reload duy nhất. Nếu `WithoutReload` bỏ luôn `Test` thì nhanh hơn, vậy vì sao giữ?
3. Bỏ `steps.Serial` ở `applySerial` thì hai lỗi nào có thể xảy ra, và site nào chịu thiệt?
4. `ConfigReload.Check` trả `false` gần như mọi lúc. Nếu nó luôn trả `false` thì sao? Nếu luôn trả `true` thì sao?
5. Trong lô 10 site, `wp core install` của `s7` hỏng ở pha C. Kể theo thứ tự những gì xảy ra với `s7`, với 9 site kia, với nginx, và job kết thúc ở trạng thái nào.
6. Vì sao khoá user Linux bằng `usermod --expiredate 1` mà không bằng `usermod -L` như đặc tả? Khoá như vậy có làm WP-CLI của tpan chạy dưới user đó hỏng không?
7. Vì sao thư mục trong thùng rác tên `s2_example_com_36299e` chứ không phải `s2.example.com` hay một số tự tăng? Và vì sao `/var/lib/tpan/trash` là 0700?
8. (Cần máy ảo) Chứng minh `site rm` không chép dữ liệu mà chỉ đổi tên.
9. (Cần máy ảo) Ở mục 2.10, kiểm chứng trong job nhận HTTP 404 nhưng curl vài giây sau nhận mã thoát 52. Vì sao lại khác nhau? Và nếu site đó có SSL thì vì sao `siteGone` phải gọi lại nhiều lần mới đạt? Tự thấy hiện tượng đó bằng lệnh nào?
10. Bạn chạy `tpan trash settings --days 0` khi trong thùng rác đã có `s9` với hạn 7 ngày. `s9` có bị xoá ở lần dọn tiếp theo không? Và nếu đúng giờ dọn mà đang có job `apply` chạy thì sao?

## Đáp án

1. Thứ nhất, `Execute` hoàn tác cả job khi một bước lỗi: site thứ mười hỏng thì chín site đã xong cũng bị gỡ, trong khi mục 7 đòi hoàn tác riêng từng site. Thứ hai, các bước ghi pool, vhost, link của Plan thường tự reload, nên mười site là hàng chục lần reload. `ExecuteBatch` chia unit theo site (hoàn tác riêng bằng `unitFail`) và dồn reload vào pha B (`ConfigReload`, chạy một lần). Bằng chứng: mục 2.4, đúng một `systemctl reload` mỗi dịch vụ trong `job_events` và một dòng `Reloaded` trong journal.
2. Lần kiểm ở pha A chỉ ra đúng site gây lỗi: file vhost sai của `s7` làm bước ghi vhost của `s7` hỏng, chỉ `s7` bị hoàn tác. Nếu chỉ kiểm một lần ở pha B, lỗi ở đó không biết do site nào (`nginx -t` kiểm cả config), nên engine phải hoàn tác mọi site còn sống (`TestBatchCommitFailUndoesEverything`). Lần kiểm ở pha B vẫn cần vì đó là lần cuối trước khi nạp, và các file đã ghi xong hết.
3. Một: hai `useradd` cùng lúc, lệnh thứ hai lỗi `cannot lock /etc/passwd`. Hai: site A chạy `nginx -t` đúng lúc site B ghi dở vhost, `nginx -t` đọc file dở và báo lỗi. Trong cả hai trường hợp, site bị hoàn tác là site gặp lỗi (A), không phải site gây ra (B). Kết quả không ổn định giữa các lần chạy, rất khó tìm.
4. Luôn `false`: `runStep` gọi `Check` lại ngay sau `Apply`, thấy `false` và báo lỗi "đã chạy xong nhưng kiểm lại vẫn chưa đạt", nên pha B luôn hỏng và mọi site bị hoàn tác. Luôn `true`: engine coi bước "đã đúng, bỏ qua", không bao giờ reload, site mới không được phục vụ, và lần chạy lại pha B sau khi hoàn tác ở pha C cũng không reload. Cờ `reloaded` cho đúng một lần `true` ngay sau một `Apply` thành công. Test: `TestConfigReload` trong `internal/steps/batch_test.go`.
5. Theo mã và unit test, chưa chạy trên máy ảo. `runSteps` của `s7` ở pha C trả lỗi, `unitFail(k, "finish", ...)` hoàn tác các bước đã Apply của `s7` theo thứ tự ngược (bước cài WordPress nếu nó đã Apply, rồi các bước pha A từ cuối lên đầu: link, vhost, `wp-config.php`, mã WordPress, pool, DB, các thư mục, user), không reload vì `WithoutReload`. Phát `unit.done` của `s7` với `"status":"rolled_back","phase":"finish"`. 9 site kia chạy tiếp và thành công. Vì có unit bị hoàn tác ở pha C, `exec` chạy lại Commit một lần nữa: `php-fpm8.3 -t`, reload PHP-FPM, `nginx -t`, reload nginx, để nginx thôi phục vụ vhost của `s7` đã nạp ở pha B. Job `succeeded` (có ít nhất một site thành), `Job.Error` có dòng `1/10 đối tượng lỗi: s7.example.com (finish): ...`. `tpan apply` thoát 1 vì không phải mọi site đều xong (`applyOK`). Test: `TestBatchFinishOrVerifyFailRecommits`, `TestApplyOneFailsOthersStay`.
6. Đã thử trên máy ảo (`docs/thay-doi/M6.md`): user của site không có mật khẩu nên shadow vốn là `!`, `passwd -S` báo `L` từ đầu; `usermod -L` không đổi gì, `usermod -U` từ chối mở (in cảnh báo) mà vẫn thoát 0. Bước khoá không khoá gì và bước mở không mở gì. Hạn tài khoản thì đổi được và đọc lại được bằng `chage -l`, và chặn cron cùng đăng nhập qua PAM. WP-CLI của tpan không hỏng: tpan chạy nó bằng `runuser -u` từ root, và root bỏ qua kiểm tài khoản. Bằng chứng: mục 2.6, `runuser -u tp_s2_example_com_36299e -- id -un` vẫn in tên user khi user đang hết hạn.
7. Slug suy được từ domain (không cần lưu thêm), duy nhất (cột `slug` có `UNIQUE`), và một domain chỉ có một dòng trong `sites` nên không có hai bản trong thùng rác. Tên user và DB của site là `tp_` cộng slug, nên nhìn là biết thư mục của user nào; slug chỉ gồm chữ thường, số, `_`. Số tự tăng thì phải lưu thêm và nhìn thư mục không biết của site nào. 0700 của root: thư mục site bên trong vẫn giữ chủ là user của site và nhóm `www-data` (quyền 2750, mục 2.6), nên nếu thùng rác mở thì web server vẫn đọc được file của site đã xoá. Thư mục cha 0700 chặn mọi user không phải root đi qua.
8. Lệnh: `scripts/vm.sh root stat -c '%d %i' /var/www/s1.example.com`, rồi `scripts/vm.sh root tpan site rm s1.example.com --yes`, rồi `scripts/vm.sh root bash -c 'stat -c "%d %i" /var/lib/tpan/trash/s1_*'`. Theo `rename(2)`, số thiết bị và số inode phải giữ nguyên: cùng một thư mục, chỉ đổi chỗ trong cây. Chép thì thư mục mới có inode mới. Thêm một dấu: job mất 0,1 giây cho 3785 file (mục 2.6). Phần so inode chưa chạy; mã băm cả cây giống hệt trước và sau thì đã có trong log, nhưng mã băm không phân biệt được chép hay đổi tên. Nhớ `site restore s1.example.com --yes` sau khi thử.
9. `systemctl reload nginx` trả về trước khi nginx thật sự dùng cấu hình mới. Trong khoảng 0,17 giây đầu (đo trên máy ảo, ghi ở commit `b70f462` và `docs/thay-doi/M6.md`), request vẫn được trả lời theo cấu hình cũ: vhost của site vẫn còn, nhưng thư mục đã chuyển vào thùng rác nên nó trả 404. Kiểm chứng trong job chạy đúng khoảng đó. Vài giây sau, cấu hình mới có hiệu lực, không vhost nào nhận tên này, request rơi vào site mặc định (444, curl mã 52). `siteGone` chấp nhận cả 404 lẫn 52 (apache thì 403) vì cả hai đều nghĩa là "không còn site". Site có SSL thì khác: vhost cũ ở cổng 80 trả 301 sang https, không phải 404, nên lần gọi đầu là `Fail`. Trước bản sửa, site như vậy bị hoàn tác oan (mục 5, chỗ sai 4). Giờ `goneCheck.Run` gọi lại mỗi 200 ms tới 5 giây, nên tới lần gọi sau khoảng 0,17 giây thì đạt. Cách tự thấy (chưa chạy đúng lệnh này, số đo ở trên là của phiên chính): đưa một site vào thùng rác rồi gọi curl liên tục ngay sau đó, ví dụ `scripts/vm.sh root bash -c 'tpan site rm s9.example.com --yes >/dev/null & for i in $(seq 40); do curl -s -o /dev/null -w "%{http_code} " --resolve s9.example.com:80:127.0.0.1 http://s9.example.com/; sleep 0.05; done; wait'`. Dãy mã in ra phải chuyển từ 200, qua vài lần 404 (hay 301 với site có SSL), sang 000 (mã 52). Lưu ý: dòng lệnh có `$(...)` trong `bash -c`, gõ trên Mac rồi chạy trong máy ảo là được; nhớ `site restore` sau khi thử.
10. Không. Hạn xoá hẳn được tính và ghi vào `sites.purge_at` một lần, lúc vào thùng rác (`markTrashed`); `trash settings` chỉ đổi khoá `trash.days`, áp cho site vào thùng rác sau đó. Lệnh cũng in rõ điều này (mục 2.9). `s9` sẽ bị xoá khi tới hạn 7 ngày cũ. Nếu đúng giờ dọn mà đang có job, `round` không giành được `claim` (hoặc thấy khoá toàn cục bị CLI giữ), ghi một dòng log "bỏ vòng dọn này" và chờ giờ sau. Muốn dọn ngay thì `sudo tpan trash purge --expired`.
