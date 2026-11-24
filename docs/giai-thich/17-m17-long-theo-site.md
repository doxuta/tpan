# M17: lồng theo site, trần tài nguyên theo gói, thử tấn công

Đọc kèm: `02-m2-site-wordpress.md` (user Linux `tp_<slug>`, pool PHP-FPM), `04-m4-health.md` (phép kiểm, sửa tự động), `13-m13-nhieu-nguoi-dung.md` (gói, tạm khoá, thùng rác), `14-m14-co-lap-va-dinh-tuyen.md` (chín đường tấn công giữa các site, mốc này đóng nốt phần cấu hình không đóng được), `docs/thay-doi/M17-thiet-ke.md` (thiết kế đã qua soát) và `docs/thay-doi/M17.md` (sổ quyết định, chỗ khác đề bài).

Quy ước dùng suốt file: **A** là site đã bị chiếm (kẻ tấn công chạy được PHP dưới user của A), **B** là nạn nhân. Trong số đo, A là `a.example.com` (user `tp_a_example_com_7653e4`), B là `b.example.com`.

Ba từ dùng xuyên suốt:

- **Lồng** là toàn bộ những thứ bao quanh PHP của một site: một slice, một dịch vụ PHP-FPM riêng, một socket, một hàng rào.
- **Hàng rào** là phần "site nhìn thấy gì" (namespace).
- **Trần** là phần "site dùng được bao nhiêu" (cgroup).

"Ngoài lồng" là cách chạy của v0.2.0: mọi site của một bản PHP chung một dịch vụ PHP-FPM.

## 1. Mốc này làm được gì

- Gói (package) có thêm cờ `cage` và sáu trần: CPU, RAM, số tiến trình, đọc đĩa, ghi đĩa, số worker PHP. Mặc định tắt, nên nâng cấp từ v0.2.0 không đổi file nào trên máy.
- `tpan cage enable <domain>` đưa một site vào lồng: PHP của site chạy trong dịch vụ riêng, bằng chính user của site (không còn tiến trình root nào), chỉ thấy thư mục của mình, `/tmp` riêng, không thấy tiến trình hay tên của site khác, cả máy chỉ đọc. `tpan cage disable` đưa ra và trả lại đúng từng byte file cấu hình cũ.
- Site vượt trần RAM thì chỉ request đó chết (502), request sau trả 200 ngay, site khác không rớt request nào. Vượt trần CPU thì bị bóp, không bị giết.
- `tpan usage` cho xem số liệu đọc thẳng từ cgroup của nhân; `tpan usage --suggest` gợi ý trần từ số đo 7 ngày. Gợi ý chỉ là gợi ý, áp bằng `tpan package edit`.
- `tpan isolation probe` tự dựng hai site thử, cho site này đánh site kia theo từng đường đã biết, ghi bằng chứng, rồi xoá sạch hai site thử. Không đụng site thật.

Nói trong một phút (để tập nói, không cần thuộc lòng):

> Trước M17, mọi site của một bản PHP chạy chung một dịch vụ PHP-FPM. Quyền file Linux ngăn site A đọc file của B, nhưng lệnh shell do PHP của A gọi vẫn thấy tên mọi site (`ls /var/www`), và A ngốn hết RAM hay CPU thì B chết theo. M17 cho mỗi site một lồng, làm bằng hai thứ có sẵn trong nhân Linux mà systemd điều khiển: namespace quyết định site nhìn thấy gì, cgroup quyết định site dùng được bao nhiêu. Không cài thêm phần mềm nào. Trần đặt theo gói. Muốn biết lồng có giữ không thì chạy một lệnh thử tấn công; lệnh tự dựng hai site thử, đánh nhau, ghi bằng chứng và dọn.

## 2. Chạy thử

### 2.0 Cách đọc mục này

Mình là thợ viết, không được dùng máy ảo trong phiên viết file này. Không lệnh nào dưới đây do mình vừa chạy. Output lấy từ hai nguồn:

- `docs/bang-chung/M17-nghien-cuu/*.ket-qua.txt`: các bản thử trên máy ảo `tpan-m17` (Ubuntu 24.04.5, kernel 6.8.0-147, systemd 255.4, PHP 8.3.6) chạy **trước khi viết mã** để biết cơ chế nào chạy được. Tên bản thử (t1, t3, t5...) là tên file. Mỗi bản thử chạy bằng một script: trên Mac, trong thư mục repo, gõ `make nc-m17 T=<tên file không đuôi>`, ví dụ `make nc-m17 T=t5-chi-phi`. Lệnh này nối `lib.sh` với script rồi chạy bằng root trong máy ảo, và lưu output vào file `.ket-qua.txt` cạnh nó (ghi đè file cũ).
- Phần của `tpan` chạy thật (lệnh `tpan cage ...`, `tpan usage`, `tpan isolation probe`): output ở `docs/bang-chung/M17.log`, do e2e bước 20 chạy ngày 23/11/2026 trên mã đã gộp `dev`. Phiên chính dán vào mục 2.1 và điền số đo ở mục 2.7.

Một lưu ý đọc số: tên slug trong output (`a_example_com_7653e4`) là tên user bỏ chữ `tp_`; đuôi 6 ký tự hex là phần ngẫu nhiên tpan thêm lúc tạo site.

### 2.1 Lệnh bạn sẽ gõ

Chạy trên Mac, trong thư mục repo. Máy ảo phải có stack và một site (xem `scripts/vm.sh` ở mốc 1 và 2). Output thật của từng lệnh nằm trong `docs/bang-chung/M17.log` (tìm dòng bắt đầu bằng `$ scripts/vm.sh root tpan cage` hay `tpan usage`); hai đoạn tiêu biểu ở cuối mục này.

```bash
# bật lồng cho gói tên "long": RAM 200 MiB, CPU 50% của một nhân, 40 tiến trình, 3 worker
scripts/vm.sh root tpan package edit long --cage on --ram-mb 200 --cpu 50 --tasks 40 --php-workers 3 --yes
scripts/vm.sh root tpan cage enable a.example.com --dry-run   # xem trước Plan, không đổi gì
scripts/vm.sh root tpan cage enable a.example.com --yes
scripts/vm.sh root tpan cage status
scripts/vm.sh root tpan usage a.example.com
scripts/vm.sh root tpan usage --suggest
scripts/vm.sh root tpan isolation probe --dry-run
scripts/vm.sh root tpan isolation probe --yes
scripts/vm.sh root tpan cage disable a.example.com --yes
```

`package edit` sửa gói đã có, nên gói `long` phải có trước (tạo bằng `tpan package add long ...`; hai lệnh nhận chung bộ cờ, xem biến `cageFlagNames` trong `cmd/tpan/package.go`).

Output thật, lấy từ `M17.log` (phần nginx, 23/11/2026). `tpan cage status` sau khi A, B vào lồng, O để ngoài:

```
SITE                             CHỦ            GÓI          TRẠNG THÁI         RAM / TIẾN TRÌNH
a.example.com                    khach          long         trong lồng         38/200 MiB, 2/40
b.example.com                    khach          long         trong lồng         38/200 MiB, 2/40
o.example.com                    admin          default      ngoài lồng         -
```

`tpan usage` sau các phép quá tải (A đã bị giết một lần vì chạm trần RAM và một lần chạm trần số tiến trình; B không chạm gì):

```
SITE                             CHỦ            RAM MiB                TIẾN TRÌNH     CHẠM TRẦN
a.example.com                    khach          40/200 (đỉnh 200)      2/40           OOM 1, tiến trình 1
b.example.com                    khach          40/200 (đỉnh 82)       2/40           OOM 0, tiến trình 0
```

Hỏi số liệu của site ngoài lồng (`tpan usage o.example.com`) thì bị từ chối với mã `usage_not_caged` và lời chỉ cách đưa vào lồng: site ngoài lồng dùng chung một dịch vụ PHP nên không có cgroup riêng để đọc.

### 2.2 Nhìn từ bên trong lồng: bản thử t1

Bản thử t1 bật lồng cho A bằng tay (chưa có mã tpan), rồi cho PHP của A chạy `shell_exec` để xem nó thấy gì. Trích từ `t1-fpm-user-socket.ket-qua.txt`, mục "2. Ai chạy, còn quyền gì":

```
root                           13488       1 php-fpm: master process (/etc/php/8.3/fpm/php-fpm.conf)
tp_a_example_com_7653e4        15186       1 php-fpm: master process (/etc/tpan/cage/a_example_com_7653e4/php-fpm.conf)
tp_a_example_com_7653e4        15188   15186 php-fpm: pool tpan-a_example_com_7653e4
tp_b_example_com_cd59d5        15194   13488 php-fpm: pool tpan-b_example_com_cd59d5
MainPID=15186: Uid: 999 999 999 999 Gid: 988 988 988 988 CapInh: 0000000000000000 CapPrm: 0000000000000000 CapEff: 0000000000000000 CapBnd: 0000000000000000 NoNewPrivs: 1 Seccomp: 2
cgroup của tiến trình chính: 0::/tpan.slice/tpan-a_example_com_7653e4.slice/tpan-cage-a_example_com_7653e4-php.service
```

Cách đọc:

- Dòng 1 là PHP-FPM chung (còn root, phục vụ B vì B ở ngoài lồng). Dòng 2 và 3 là lồng của A: tiến trình chính cũng chạy bằng `tp_a_...`, không phải root.
- `Uid: 999 999 999 999`: bốn số uid (thật, hiệu lực, đã lưu, của hệ file) đều là user của site. `CapEff: 0000...`: không còn capability nào. `NoNewPrivs: 1`: không lấy thêm được quyền qua file setuid.
- Dòng cuối: cgroup của tiến trình nằm dưới `/tpan.slice/tpan-<slug>.slice/`. Đó là chỗ trần được áp.

Cùng bản thử, mục "3. Từ PHP của A (shell_exec) nhìn thấy gì":

```
  ls /var/www: a.example.com
  ls /var: cache log run tmp www
  ls /run: mysqld systemd
  ls /tmp: []  ls /dev/shm: []
  ls /var/log/tpan/sites: a.example.com
  tien trinh nhin thay (so thu muc so trong /proc): 5 | cua user khac: 0
  getent passwd: root tp_a_example_com_7653e4 nobody
  ls /etc/nginx/sites-enabled: 
  ls /etc/php/8.3/fpm/pool.d: 
  ls /etc/cron.d: 
  ghi /etc: khong | touch /usr/x: khong
  ghi vao site: duoc
```

So với v0.2.0 (`ls /var/www` ra tên mọi site), đây là toàn bộ điều M17 muốn: A chỉ thấy đồ của A. (Bản thử t1 chưa cho `getent passwd` ra đủ 4 dòng như mã cuối cùng; bản mã dùng `passwd` riêng 4 dòng gồm cả `www-data`, mục 3.4.)

Điểm bảo mật của dịch vụ, cùng bản thử (mục "6. systemd-analyze security"):

```
→ Overall exposure level for php8.3-fpm.service: 9.4 UNSAFE
→ Overall exposure level for tpan-cage-a_example_com_7653e4-php.service: 1.3 OK
```

Lệnh `systemd-analyze security <unit>` chấm một dịch vụ theo số thuộc tính cô lập đã bật. Thấp là tốt. Dịch vụ chung 9.4, dịch vụ trong lồng 1.3 (bản thử t1; file t1 có kèm bảng các thuộc tính còn thiếu, như `PrivateNetwork`, vì WordPress cần mạng).

### 2.3 Trần RAM: bản thử t3 và t3b

Trích `t3-tran-tai-nguyen.ket-qua.txt`, mục 1 (trần cứng `MemoryMax=200M`, `MemorySwapMax=0`, dịch vụ có `OOMPolicy=continue`):

```
A cấp phát 400 MB: HTTP 502 sau .064160959 giây
A trả 200 lại sau .044238207 giây (thử 0 lần hỏng trước đó)
B: 0/60 request không phải 200 trong lúc đó
sau: memory.events: low 0 high 0 max 18 oom 1 oom_kill 1 oom_group_kill 0 | memory.peak=209715200 (200 MB)
dịch vụ: active, NRestarts=0, Result=success
```

Cách đọc: một request của A xin 400 MB, tiến trình con PHP bị nhân giết sau 0,06 giây, nginx trả 502 cho request đó. Request kế của A trả 200 sau 0,04 giây. B không rớt request nào trong 60 lần. Dịch vụ không khởi động lại (`NRestarts=0`). `memory.events` ghi `oom_kill 1`: đúng một tiến trình bị giết.

Cùng file, mục 2, trần **mềm** (`MemoryHigh=200M`, không có `MemoryMax`):

```
   trang chủ của A trong lúc bị bóp: HTTP 200 sau 27.756684212 giây
A cấp phát 400 MB: HTTP 000 sau 40.042217411 giây (curl chờ tối đa 40 giây)
```

Trần mềm không giết ai, chỉ làm site chậm lại: trang chủ mất 27,8 giây. Đó là lý do mốc này không dùng `MemoryHigh` (mục 5.1).

Trần CPU, `t3b-tran-lam-ro.ket-qua.txt`, mục c (máy ảo có 2 nhân; "máy bận" là mỗi nhân có một vòng lặp ưu tiên thấp nhất chạy ngoài lồng):

```
  máy rảnh trần  50%: nhận  35.7% (71% của trần)
  máy rảnh trần  25%: nhận  15.7% (62% của trần)
  máy bận trần 100%: nhận  99.5% (99% của trần)
  máy bận trần  50%: nhận  50.0% (100% của trần)
  máy bận trần  25%: nhận  25.0% (100% của trần)
```

Máy bận thì đúng trần. Máy rảnh thì ít hơn trần, không bao giờ nhiều hơn. Nguyên nhân (theo ghi chú của thiết kế, mục 2.4): vCPU đang ngủ của máy ảo Multipass thức dậy trễ khoảng 10 đến 14 ms ở đầu mỗi chu kỳ 100 ms. Đó là chuyện của máy ảo, không phải của thiết kế. Trên cùng bản thử, B chạy cùng lúc A bị bóp ở 10%: 7,76 giây so với 7,60 giây khi không có trần, tức B không chậm đi (t3, mục 3).

Trần số tiến trình (t3b, mục b, `TasksMax=30`):

```
slice:   pids.current=29 pids.peak=30 pids.max=30 pids.events: max 0
dịch vụ: pids.current=29 pids.events: max 1
trang chủ của A lúc đang đầy: HTTP 200 sau .027845923 giây | B: HTTP 200
```

Chú ý dòng thứ hai: trên kernel 6.8, bộ đếm "chạm trần" nằm ở cgroup nơi gọi `fork` (cgroup của dịch vụ), không phải ở slice (`max 0`). Vì vậy `cage.ReadUsage` cộng cả các cgroup con (mục 3.9).

Trần đĩa (t3, mục 5, `IOWriteBandwidthMax=/var/www 4M`):

```
ghi 32 MB, direct:   10.0323 s, 3.3 MB/s   (mong đợi khoảng 8 giây, 4 MB/s)
-- cùng việc ở ngoài lồng (user của B, không trần): ghi direct 0.00835151 s, 4.0 GB/s
```

Đổi trần khi site đang chạy (t3, mục 6):

```
trước: memory.max=209715200 cpu.max=50000 100000 pids.max=40 | MainPID=24034
sau khi ghi file slice + daemon-reload (.115814562 giây): memory.max=314572800 cpu.max=80000 100000 pids.max=60 | MainPID=24034 | A: HTTP 200
```

Đổi trần chỉ cần ghi lại file slice rồi `systemctl daemon-reload` (0,12 giây): `MainPID` giữ nguyên, site không khởi động lại.

### 2.4 Chi phí của lồng: bản thử t5

`t5-chi-phi.ket-qua.txt` (10 site):

```
10 site ở PHP-FPM chung (tiến trình con đã thoát): MemAvailable=3363 MB
10 site mỗi cái một lồng (chỉ còn tiến trình chính): MemAvailable=2965 MB
  chênh: 397 MB cho 10 lồng = 39 MB mỗi lồng
  a.example.com: RSS 31.6 MB, PSS 10.8 MB, USS 9.1 MB | memory.current của slice 31 MB (anon 6.5, file 24.5, shmem 24.5 MB)
10 lồng đang ngủ (dừng dịch vụ, socket vẫn nghe): MemAvailable=3328 MB (so với PHP-FPM chung: 34 MB)
  request đầu khi đang ngủ: HTTP 200, 0.479907 giây | request kế: HTTP 200, 0.027461 giây
  B request đầu (ondemand sinh tiến trình con): 0.095847 | kế: 0.025502
  A (trong lồng): trung bình 26.3, trung vị 25.3, p95 28.8, lớn nhất 79.5
  B (ngoài lồng): trung bình 27.9, trung vị 24.7, p95 28.0, lớn nhất 240.3
```

Cách đọc:

- Mỗi lồng **đang thức** tốn khoảng 34 đến 39 MB (con số ở bản thiết kế, mục 2.6, là hai lần đo: 341 và 397 MB cho 10 lồng; file kết quả này là lần 397). Phần lớn là OPcache riêng của site (24,5 MB `shmem`): ngoài lồng 10 site chen nhau trong một vùng OPcache chung.
- Lồng **đang ngủ** gần như không tốn gì so với PHP-FPM chung. Request đầu đánh thức dịch vụ mất 0,30 đến 0,48 giây (ngoài lồng 0,07 đến 0,10 giây); request sau 0,025 giây.
- Độ trễ mỗi request (300 lượt xen kẽ): trung vị 25,3 ms trong lồng so với 24,7 ms ngoài lồng. Không đo được độ trễ thêm.

### 2.5 Máy khởi động lại lúc MariaDB chưa lên: t11, t12b

Đây là chỗ đã làm sai một lần rồi sửa (mục 5.6). `t11-cho-mariadb.ket-qua.txt`, mục b (lệnh gắn `/run/mysqld` có dấu `-`):

```
  request ngay lúc MariaDB chưa lên: HTTP 500 | sau khi MariaDB lên (không đụng lồng): HTTP 500 | dịch vụ active (success, NRestarts=0)
  mountinfo của lồng có /run/mysqld: 0
  restart lồng: 200 | mountinfo có /run/mysqld: 1
```

Lỗi: lồng thấy `/run/mysqld` tại lúc nó khởi động. Thư mục chưa có lúc đó thì lồng không bao giờ nối được DB, kể cả khi MariaDB lên sau, cho tới khi lồng được khởi động lại. Cách sửa (unit socket tự tạo thư mục) và kết quả, `t12b-sau-khoi-dong.ket-qua.txt`:

```
ghi lúc khởi động: luc goi: /run/mysqld CO, mariadb inactive;HTTP 500;
dịch vụ lồng: active (NRestarts=0, Result=success) | socket: active
mountinfo của lồng có /run/mysqld: 1
bây giờ (MariaDB active), KHÔNG đụng lồng: A HTTP 200 | B HTTP 200
```

Lúc gọi MariaDB chưa chạy nên 500 là đúng; MariaDB lên thì A tự trả 200 mà không ai đụng lồng.

### 2.6 Lệnh trong lồng bị giết: bản thử t9b

`t9b-oneshot.ket-qua.txt`: kết quả khi chạy lệnh qua `systemd-run` với `--service-type=oneshot`:

```
exit 3: mã 3
   LoadState=loaded Result=exit-code ExecMainCode=1 ExecMainStatus=3
tự chết vì SIGTERM: mã 255
   LoadState=loaded Result=signal ExecMainCode=2 ExecMainStatus=15
TimeoutStartSec=2 với sleep 30: mã 1 sau 2.331214236 giây (sleep còn chạy: 0)
   LoadState=loaded Result=timeout ExecMainCode=2 ExecMainStatus=15
vượt RAM: mã 1
   LoadState=loaded Result=oom-kill ExecMainCode=2 ExecMainStatus=9
```

Ở bản thử t9 (không có `--service-type=oneshot`), dòng "tự chết vì SIGTERM" ra **mã 0**: lệnh bị giết mà `systemd-run` báo thành công. Mục 3.8 giải thích mã trông thế nào và vì sao `cage.Wrap` phải hỏi lại systemd.

### 2.7 Số đo của e2e

E2E bước 20 (`scripts/e2e-m17.sh chinh`, log `docs/bang-chung/M17.log`) chạy ngày 23/11/2026 trên mã đã gộp `dev` (có M16, M18, M28), từ máy sạch, lần lượt stack nginx rồi stack Apache: **672 khẳng định đạt, 0 không đạt**. Lượt này và lượt nâng cấp chạy trên mã cuối (sau khi siết luật ở mục 5.11); ba lượt hồi quy chạy cùng ngày trên mã đã gộp ngay trước lần siết đó (`a8d0a97`; phần siết chỉ chạm lệnh của lồng, ba kịch bản hồi quy không dùng lồng):

| Lượt | Log | Kết quả |
|---|---|---|
| Nâng từ v0.2.0 (`e2e-m17.sh nang-cap`) | `M17-nang-cap.log` | 93 đạt, 0 không đạt |
| Hồi quy M13 | `M17-hoi-quy-m13.log` | 293 đạt, 0 không đạt |
| Hồi quy M14 | `M17-hoi-quy-m14.log` | 270 đạt, 0 không đạt |
| Hồi quy M9 (`all`) | `M17-hoi-quy-m9.log` | 128 đạt, 0 không đạt |

Số đo đáng nhớ (nginx / Apache khi hai bên khác nhau):

| Phép thử | Kết quả |
|---|---|
| A nhìn từ trong lồng | `ls /var/www` chỉ có A; `/tmp` rỗng; không thấy tiến trình của user khác; `passwd` riêng 4 dòng; `CapEff` rỗng, `NoNewPrivs` bằng 1; `sudo`, `crontab -l` không chạy được |
| RAM: A cấp phát 400 MB, trần 200 MB | request đó ra 502 (nginx) / 503 (Apache); request kế tiếp của A 200 trong 2 giây; `oom_kill` tăng đúng 1; dịch vụ PHP của A không khởi động lại; B 0 request hỏng trên 60 |
| Số tiến trình: A sinh 200, trần 40 | `pids.peak` bằng 40; B 0 request hỏng trên 40 |
| CPU, máy bận, trần 50% | A nhận 45% đến 52% của một nhân ở cả hai lần đo, có bị bóp (`nr_throttled` tăng). Máy rảnh: 46,0% / 35,2% (dưới trần, xem mục 2.4) |
| Đĩa, trần ghi 4 MB/giây | A ghi 32 MB mất từ 5 giây trở lên; cùng lệnh ở site ngoài lồng dưới 2 giây; B 0 request hỏng |
| Vào lồng (site R đang nhận request) | 2 request hỏng trên 68 / 1 trên 59, gián đoạn 66 ms / 4 ms |
| Ra lồng | 0 request hỏng trên 68 / 45 |
| Hạ trần RAM xuống dưới mức đang dùng rồi nâng lại | A tự trả 200, phục hồi sau 2 ms / 20 ms |
| Thử tấn công `tpan isolation probe` | 14 đường (L1 đến L9 của M14, C1 đến C5 của lồng); ngoài lồng: các đường L bị chặn, C1 đến C5 là `needs_cage` (chỉ lồng chặn được); trong lồng: mọi đường bị chặn; hai site thử bị xoá sạch, kể cả khi `kill -9` tpan giữa lượt rồi chạy `--cleanup` |
| Khởi động lại máy ảo | A, B trả 200; site tạm khoá vẫn 403 và socket vẫn tắt |

### 2.8 Bấm thật trên giao diện (25/11/2026)

Trên Mac, trong thư mục repo: `make e2e-m17-gd`. Lệnh đưa máy ảo về sạch, cài tpan của cây, tạo gói `long` chưa bật lồng, user `khach` và site `a.example.com` của khach, rồi cho Chrome không giao diện (chạy trên Mac, `scripts/anh-readme`, bộ `m17-that`) mở panel thật của máy ảo và bấm như người dùng. Sau mỗi bước, script hỏi lại máy ảo bằng lệnh không đi qua giao diện. Log: `docs/bang-chung/M17-giao-dien.log`, **68 khẳng định đạt, 0 không đạt** trên mã cuối.

| Bước trên giao diện | Máy ảo xác nhận |
|---|---|
| Màn Gói: Sửa gói `long`, chọn lồng, CPU 50, RAM 200, 40 tiến trình, 3 worker, Lưu | `tpan package list --json` đúng các số; site A chuyển sang "chờ vào lồng" |
| Trang của A: "Vào lồng", xem trước, chạy | `tpan cage status`: A trong lồng; `systemctl show` slice: `MemoryMax=209715200`, `MemorySwapMax=0`, `CPUQuotaPerSecUSec=500ms`, `TasksMax=40`; dịch vụ PHP của lồng chạy bằng user của A; A trả 200 |
| Màn Gói: RAM 256, nút của hộp là "Xem trước", chạy (job `cage.sync`) | slice có `MemoryMax=268435456`; A vẫn 200 |
| Màn Bảo mật: mở dòng "Thử tấn công", chạy cả hai lượt | lượt có 14 đường, 0 ô lộ, đã dọn; không còn user, thư mục, unit nào của site thử; health không có `probe.leftover` lỗi |
| Trang của A: "Ra khỏi lồng" | A ngoài lồng, không còn file `tpan-cage-*`, A trả 200 |

Lần chạy đầu của e2e này tìm ra một lỗi thật: bấm Lưu trong hộp sửa gói bị 400 (mục 5.12).

## 3. Đi qua mã theo thứ tự chạy

Thứ tự đi: bạn gõ `tpan cage enable a.example.com`, lệnh dựng một Plan (danh sách bước, có xem trước, hoàn tác được: quy ước từ M1), engine chạy từng bước, và cuối cùng nginx trỏ vào socket của lồng. Sau đó đi theo một request, một lệnh `wp`, một số liệu, và một lượt thử tấn công.

### 3.1 Cổng vào: `cmd/tpan/cage.go`

`cageCmd` đăng ký nhóm lệnh `cage` (`status`, `enable`, `disable`). `runCage` kiểm tham số (đúng một `<domain>`, hoặc `--all`/`--stale`), đòi quyền root (trừ khi `--dry-run`), rồi gọi `c.PrepareEnable(ctx, d)` hoặc `c.PrepareDisable`, và đưa kết quả cho `runPlanned` (hàm dùng chung của mọi lệnh: in bản xem trước, hỏi "Chạy? [y/N]", chạy, in báo cáo). Phần "đưa vào lồng" không nằm ở đây: CLI chỉ chọn hàm. Quy ước cũ của repo: logic nằm ở `internal/core`, CLI và API đều gọi cùng hàm.

Một điểm khác với mốc trước nằm ở `cmd/tpan/main.go`, hàm `realEnv`:

```go
// Bọc một lần ở đây: mọi lệnh chạy thay cho một site trong lồng (CLI,
// serve, cron, MCP) đi qua hàng rào của lồng (M17, cage.Wrap).
sys:    cage.Wrap(system.NewLocal()),
```

`system.NewLocal()` là cái chạy lệnh thật trên máy. `cage.Wrap` bọc nó lại để mọi lệnh "chạy dưới user của site" tự đi vào lồng. Mục 3.8 đi sâu chỗ này.

### 3.2 Điều kiện đầu và Plan: `Cages.PrepareEnable` trong `internal/core/cage.go`

`PrepareEnable` chỉ đọc máy, không đổi gì. Nó từ chối (với mã lỗi, kèm lệnh sửa) khi:

- gói của chủ site chưa bật lồng (`CodeCageNotInPackage`, lời khuyên `sudo tpan package edit <gói> --cage on`);
- máy chưa sẵn sàng (`cageHostReady`: cgroup v2, systemd từ bản 254, vì cờ `--expand-environment` của `systemd-run` có từ bản đó);
- bản PHP của site chưa cài;
- file pool chung hoặc vhost của site đã bị sửa khác bản tpan sinh (`cageDrift`, mã `cage_drift`). Lý do: ra lồng thì tpan sinh lại hai file đó từ template, nên chỉ khi lúc vào chúng đúng template thì lúc ra mới trả lại từng byte (bất biến 2).

Site đã ở trong lồng thì **không** bị từ chối: Plan chạy lại, bước nào đã đúng thì bỏ qua. Đó là cách áp lại trần và sửa lệch.

Rồi nó dựng Plan:

```go
plan := engine.Plan{
	Kind:   "cage.enable",
	Target: dom,
	Title:  title,
	Steps:  ss,                // cageEnableSteps(...)
	Verify: policyVerify(siteChecks(cfg.Web, php, in, others, &lim)),
}
return newPlanned(c.Sys, c.Journal, plan, func(ctx context.Context, _ engine.Report) error {
	return c.markCaged(ctx, d, true)
}), nil
```

Hai thứ đáng chú ý:

- `Steps` là `cageEnableSteps` (mục 3.3 tới 3.7).
- Hàm cuối (`markCaged`) chỉ chạy khi job thành công, và đổi cột `sites.caged` thành 1. Bản thiết kế tách hai trạng thái: **muốn** (cờ `cage` của gói) và **đang là** (`sites.caged`). Đổi cờ gói không tự chạy gì; chỉ Plan này mới ghi `caged`. Vì luật 2 của lộ trình: mọi thay đổi trên site đang chạy phải là một Plan có xem trước. Hai bên lệch nhau thì health cảnh báo (`cage.state`).

### 3.3 Chỗ duy nhất chọn pool chung hay lồng: `sitePHPSteps`

```go
func sitePHPSteps(st site.Site, cfg StackConfig, lim account.Limits) ([]engine.Step, error) {
	php := sitePHP(st, cfg)
	if !st.Caged {
		pool, err := steps.SitePool(st.Layout(), php)
		...
		return []engine.Step{pool}, nil
	}
	return []engine.Step{
		&steps.CageFiles{Layout: st.Layout(), PHP: php, Limits: lim},
		&steps.CageSocket{Layout: st.Layout(), PHP: php},
	}, nil
}
```

(`internal/core/cage.go`; mình rút gọn phần xử lý lỗi.) Mọi thao tác trên site (đổi bản PHP, mở khoá, khôi phục từ thùng rác, chạy lại `site add`) đi qua hàm này để biết site đang ở đâu. Nếu chỗ nào đó vẫn tự gọi `steps.SitePool`, site trong lồng sẽ bị ghi lại một pool chung: hai nơi cùng phục vụ PHP của một site. Để chặn điều đó, có một test đọc cây cú pháp Go: `TestKhongGoiPoolChungNgoaiHamChon` trong `internal/core/nopool_m17_test.go`. Nó giữ một bảng "tên bị canh, các hàm được phép gọi" và đỏ khi có hàm khác gọi. Lần chạy đầu nó bắt được một chỗ thật: `unsuspendSteps` gọi thẳng `steps.SitePool` (ghi ở `docs/thay-doi/M17.md`, mục 12).

**Bước pool của site ngoài lồng có gác hàng rào.** Plan được dựng trước khi job lấy khoá. Nếu một Plan của chủ site (`php.use`, chạy lại `site add`, khôi phục, mở khoá, sửa tự động) dựng lúc site còn ngoài lồng, rồi admin kịp `cage enable`, thì Plan cũ chạy sau sẽ ghi lại pool chung và vhost thường: user thường tự ra khỏi lồng (soát cuối N4). Cách sửa nằm ở `internal/core/cage.go`: nhánh `!st.Caged` của `sitePHPSteps` trả `cageAbsentPool{Step: pool, ...}`, và `Apply` của nó kiểm file hàng rào **trong job** (đã giữ khoá), thấy có thì từ chối, không ghi gì. `sharedPoolStep` là chỗ duy nhất gọi `steps.SitePool` (test tĩnh canh đúng tên này); chỉ `PrepareDisable` gọi thẳng nó, vì Plan ra lồng chính là việc ghi lại pool chung.

`cageEnableSteps` ghép năm việc theo thứ tự, và thứ tự là cả chuyện an toàn:

1. `CageFiles`: ghi bảy file của lồng (mục 3.4). Hàng rào ghi **sau cùng**.
2. `CageSocket`: bật socket, systemd bắt đầu nghe (mục 3.5). Chưa khởi động dịch vụ. Vhost còn trỏ pool chung nên chưa request nào vào lồng.
3. `steps.Absent` gỡ file pool chung rồi `php-fpm<bản> -t` và reload: tiến trình con của pool chung dừng. `PrepareEnable` còn gỡ pool sót ở mọi bản PHP khác có trên máy (soát cuối V3); không thì `cage.state` báo lỗi mãi, và Apache chế độ `day-du` chuyển được request sang pool sót đó.
4. `CageReap`: gỡ crontab riêng của user site, rồi dừng mọi tiến trình mang uid của site mà không nằm trong slice (mục 3.6).
5. Vhost bản trong lồng, qua `siteVhost`. Từ đây request đi vào socket của lồng.

Hệ quả của thứ tự: giữa bước 3 và bước 5 có một khoảng ngắn mà request của site nhận 502 (pool chung đã gỡ, vhost chưa đổi). Bản thiết kế nói thẳng điều này và đề e2e đo khoảng đó; con số chưa có (mục 2.7). Đổi lại, lúc mã PHP của site bắt đầu chạy trong lồng thì không còn tiến trình nào của cùng uid ở ngoài lồng. Mục 5.5 giải thích vì sao điều đó quan trọng.

### 3.4 Bảy file của lồng: `steps.CageFiles` và `internal/templates/cage.go`

Tên file đều sinh ở `internal/site/cage.go` (các hàm của `site.Layout`), không chỗ nào khác tự ghép chuỗi `tpan-cage-`:

| Hàm | File |
|---|---|
| `CageSliceFile()` | `/etc/systemd/system/tpan-<slug>.slice` |
| `CageSocketFile()` | `/etc/systemd/system/tpan-cage-<slug>-php.socket` |
| `CageServiceFile()` | `/etc/systemd/system/tpan-cage-<slug>-php.service` |
| `CageFence()` | `/etc/systemd/system/tpan-cage-<slug>-.service.d/fence.conf` |
| `CageFPMConf()` | `/etc/tpan/cage/<slug>/php-fpm.conf` |
| `CagePasswd()`, `CageGroup()` | `/etc/tpan/cage/<slug>/passwd`, `group` |

Năm file đầu vào sổ `managed_files` (health phát hiện lệch, như mọi file tpan ghi: `CageManagedFiles()`). `passwd` và `group` không vào sổ vì nội dung chỉ biết khi user của site đã có, nên bước không khai chúng trong `Info().Files`; phép kiểm `cage.fence` kiểm chúng bằng đường khác.

Template của từng file ở `internal/templates/files/cage-*.tmpl`, hàm sinh ở `internal/templates/cage.go` (`CageSlice`, `CageSocketUnit`, `CageServiceUnit`, `CageFence`, `CageFPMConf`). Mỗi file có golden test như mọi template của repo.

**Slice** (`cage-slice.tmpl`): chỗ đặt trần.

```ini
[Slice]
MemoryAccounting=yes
TasksAccounting=yes
CPUAccounting=yes
IOAccounting=yes
{{if .Lim.CPUPercent}}CPUQuota={{.Lim.CPUPercent}}%
{{end}}{{if .Lim.RAMMB}}MemoryMax={{.Lim.RAMMB}}M
MemorySwapMax=0
{{end}}{{if .Lim.Tasks}}TasksMax={{.Lim.Tasks}}
{{end}}...
```

Trần CPU, RAM, tiến trình bằng 0 thì không có dòng đó (không giới hạn). Riêng hai dòng đĩa (`IOReadBandwidthMax`, `IOWriteBandwidthMax`) **luôn có**, ghi `infinity` khi gói không đặt trần: đợt chạy thật thấy bỏ dòng rồi `daemon-reload` thì `io.max` trên cgroup giữ nguyên trần cũ, `cage.limits` lệch mãi (`docs/thay-doi/M17.md`, đợt 4, mục 1). `MemoryMax` luôn đi kèm `MemorySwapMax=0`: không thì site vượt trần sẽ đẩy bộ nhớ ra swap và làm chậm cả máy thay vì chỉ request đó chết. Bốn dòng `...Accounting=yes` bật bộ đếm của cgroup để `tpan usage` đọc được. Cả dịch vụ PHP lẫn mọi lệnh tpan chạy thay cho site nằm trong slice này, nên trần tính chung cho chúng.

**Dịch vụ** (`cage-service.tmpl`), các dòng đáng đọc:

```ini
Type=notify
User={{.User}}
Group={{.User}}
Slice={{.Slice}}
NoNewPrivileges=yes
Environment=FPM_SOCKETS={{.Socket}}=3
ExecStart=/usr/sbin/php-fpm{{.PHP}} --nodaemonize --fpm-config {{.FPMConf}}
KillSignal=SIGQUIT
OOMPolicy=continue
Restart=on-failure
RestartSec=5s
```

- `User=` là user của site: tiến trình chính của PHP-FPM không chạy bằng root. PHP-FPM chạy bằng root để làm hai việc: đổi user cho tiến trình con và `chown` socket. Cả hai đều không cần nữa (xem 3.5 về socket), nên không còn lý do giữ root.
- `FPM_SOCKETS=<đường dẫn>=3`: cho PHP-FPM biết "socket này đã mở sẵn ở file descriptor số 3, đừng tự mở". Đó đúng là cơ chế PHP-FPM tự dùng khi reload; systemd dùng nó để trao socket.
- `OOMPolicy=continue`: một tiến trình con bị OOM kill thì chỉ nó chết. Mặc định (`stop`) systemd dừng cả dịch vụ. Số đo ở mục 5.2.
- `RestartSec=5s` cùng `StartLimitIntervalSec=30` và `StartLimitBurst=10` (ở mục `[Unit]`): mười lần khởi động lại mất 50 giây, quá cửa sổ 30 giây của giới hạn khởi động, nên một nguyên nhân tạm thời không đẩy dịch vụ và socket vào trạng thái `failed` (mục 5.7).

User, Group, Slice, NoNewPrivileges được lặp ở cả dịch vụ lẫn hàng rào. Chủ ý: nếu file hàng rào mất đúng lúc, dịch vụ vẫn chạy bằng user của site trong slice có trần, không bao giờ bằng root.

**Hàng rào** (`cage-fence.tmpl`, drop-in): mỗi dòng là một câu trả lời cho "site không được thấy gì".

```ini
ProtectSystem=strict
PrivateTmp=yes
ProtectProc=invisible
CapabilityBoundingSet=
TemporaryFileSystem=/var:ro
BindPaths={{.Dir}}
BindPaths={{.LogDir}}
TemporaryFileSystem=/run:ro
BindReadOnlyPaths=-/run/mysqld
BindReadOnlyPaths=/run/systemd/notify
TemporaryFileSystem=/etc/tpan:ro
BindReadOnlyPaths={{.Passwd}}:/etc/passwd
InaccessiblePaths=-/etc/nginx -/etc/apache2 -/etc/cron.d -/etc/letsencrypt -/etc/systemd
SystemCallFilter=@system-service
SystemCallFilter=~@privileged @resources
```

Đọc từng nhóm:

- `ProtectSystem=strict`: cả cây thư mục chỉ đọc. Chỗ ghi được chỉ là chỗ `BindPaths=` mở lại.
- `TemporaryFileSystem=/var:ro` thay `/var` bằng một thư mục rỗng chỉ đọc, rồi `BindPaths={{.Dir}}` gắn lại đúng thư mục của site (ghi được). Kết quả: `ls /var/www` chỉ ra tên của site.
- Cùng chiêu cho `/run` (không thấy socket của site khác), `/etc/tpan`, `/dev/shm`. `/etc/passwd` được thay bằng bản riêng 4 dòng (root, www-data, user của site, nobody) để `getent passwd` không lộ tên user `tp_*` khác.
- Dấu `-` trước đường dẫn (`-/run/mysqld`): thư mục chưa có thì bỏ qua lệnh gắn thay vì làm dịch vụ chết vòng. Mục 2.5 và 5.6.
- `InaccessiblePaths`: các thư mục còn lại mà đọc ra tên site khác (`/etc/nginx`, `/etc/cron.d`...). Danh sách `pool.d` kê **mọi bản PHP** (`CagePHPVersions` ở đầu `internal/templates/cage.go`), vì bản thử t6 thấy che mỗi bản đang dùng thì site vẫn đọc được `pool.d` của bản khác.
- `CapabilityBoundingSet=` (rỗng) và `NoNewPrivileges`: không có quyền đặc biệt nào, không lấy thêm được.
- `SystemCallFilter=~@privileged @resources`: lệnh hệ thống nhóm đặc quyền bị cấm, trả lỗi `EPERM` như khi user thường gọi.

Đã thử và **không** đưa vào hàng rào: `PrivateUsers=yes` (mục 5.3), `IPAddressDeny` và `PrivateNetwork=yes` (làm WordPress hỏng: không phân giải được tên, không ra được mạng), `MemoryDenyWriteExecute` (rủi ro với JIT của plugin), `NoExecPaths` (vỡ plugin có kèm file chạy). Bảng đầy đủ ở mục 2.3 của bản thiết kế.

**Cách `CageFiles` ghi file** (`internal/steps/cage.go`, hàm `applyWrite`): kiểm mọi thứ có thể hỏng mà không cần đụng máy trước (`wantAll`), chụp trạng thái cũ (`capture`) để hoàn tác, rồi ghi từng file bằng `WriteFileAtomic` (ghi file tạm rồi đổi tên: không bao giờ để lại file ghi dở), theo thứ tự `s.order()`, hàng rào cuối. Sau đó `systemctl daemon-reload`, và nếu file dịch vụ hay `php-fpm.conf` vừa ghi thì chạy `cageFPMTest`, tức `php-fpm<bản> -t -R --fpm-config <conf>` (kiểm cú pháp) bằng **root ở ngoài lồng** (`-R` cho FPM chạy bằng root, vì pool của lồng không có dòng `user`). Không chạy dưới uid của site: mã của site giết được lệnh đó (`kill -9 -1`) hay lấp số tiến trình của slice, làm Plan chính sách hỏng theo ý site (soát cuối N1). Hỏng thì `rollback` trả mọi file về như cũ. Chỉ đổi file slice thì không khởi động lại gì (trần có hiệu lực ngay sau `daemon-reload`, mục 2.3).

Điểm tinh: `passwd` và `group` được gắn vào lồng theo inode (số định danh file). Ghi bằng rename tạo ra file mới có inode mới, lồng đang chạy vẫn thấy file cũ. Nên đổi hai file đó cũng phải `try-restart` dịch vụ (dòng `s.svcActive && (written[...] ...)` ở `applyWrite`). Lỗi của `try-restart` chỉ ghi log, không làm bước hỏng: file đã đúng, `-t` đã đạt, dịch vụ sống hay chết là việc của phép kiểm `cage.socket` và `cage.runtime`.

**Gắn lại `passwd` và `group` sau lệnh đổi user.** Cùng nguyên nhân inode, nhưng ở chiều ngược lại: `useradd`, `userdel`, `usermod -aG`, `gpasswd -d` thay `/etc/passwd`, `/etc/group` thật bằng cách đổi tên đè, và kernel gỡ lệnh gắn trên file cũ trong mọi lồng đang chạy: A đọc ra tên user `tp_*` của B (phát hiện nặng của đợt chạy thật). `apt` cũng làm vậy (cài `apache2` chạy `groupadd ssl-cert`). Sửa: hàm `cageRebindIdentity` trong `internal/steps/cage.go` liệt kê các dịch vụ lồng đang `active` rồi chạy `systemctl bind --read-only <unit> <file riêng> /etc/passwd` (và `/etc/group`) cho từng lồng; các bước apt (`internal/steps/apt_m12.go`) và bước đổi user gọi nó. Hàm không bao giờ trả lỗi; lồng nào không gắn lại được thì `cage.runtime` báo lỗi, sửa tự động `cage.restart`. Còn hở: lệnh `wp` tạm đang chạy đúng lúc đó thấy `passwd` của máy tới khi nó thoát, và user do `unattended-upgrades` tạo thì phải đợi health bắt (soát cuối N5).

### 3.5 Socket do systemd giữ: `steps.CageSocket` và `cage-socket.tmpl`

Unit socket (`tpan-cage-<slug>-php.socket`) nói với systemd: "hãy tạo và giữ socket này".

```ini
[Socket]
ExecStartPre=-{{.MySQLDirCmd}}
ListenStream={{.Socket}}
SocketUser=www-data
SocketGroup=www-data
SocketMode=0660
DirectoryMode=0711
RemoveOnStop=yes
```

Đây là **socket activation**. systemd mở socket và nghe trước. Khi có kết nối đầu tiên (nginx gọi), systemd mới khởi động dịch vụ PHP và trao socket cho nó. Ba điều tốt:

1. PHP-FPM không cần root để `chown` socket (systemd làm việc đó khi tạo: `SocketUser=www-data`, `SocketMode=0660`: chỉ web server đọc ghi được, user của site khác thì không).
2. Dịch vụ khởi động lại thì socket vẫn nghe: kết nối mới xếp hàng chờ chứ không bị từ chối. Bản thử t3b mục d: 5 lượt restart dưới tải, mỗi lượt 60 request, 300 trên 300 trả 200. (Bản thử cần `process_control_timeout`; mặc định của PHP là 0 làm request đang chạy bị cắt. Bản thử t1 lần đầu ra 1 trong 40 request 502. `php-fpm.conf` của lồng đặt `process_control_timeout = 25s`.)
3. Site không có khách thì dịch vụ ngủ (không tốn RAM, mục 2.4).

Thư mục socket là `/run/tpan-cage/<16 ký tự hex>/php.sock` (`Layout.CageRunDir()` trong `internal/site/cage.go`: băm `sha256` của tên miền, lấy 8 byte đầu). Không dùng slug vì `/proc/self/net/unix` liệt kê đường dẫn mọi Unix socket của máy cho mọi tiến trình. Bản thử t14 mục P5 cho thấy từ trong lồng đọc ra 17 dòng có chữ `tpan`, gồm tên socket của site ngoài lồng như `/run/php/tpan-a_example_com_7653e4-8.3.sock`. Socket của lồng mang phần băm nên không lộ tên miền.

Dòng `ExecStartPre=-...install -d ... /run/mysqld` là cách sửa lỗi của mục 2.5: unit socket chạy bằng root ở máy thật (không trong hàng rào) trước khi nghe, nên tạo được thư mục `/run/mysqld` nếu MariaDB chưa kịp. Dấu `-` nghĩa là lệnh lỗi cũng không chặn socket.

Thư mục socket là `0711` (`DirectoryMode`): đi qua được nhưng không liệt kê được, nên site không đọc ra danh sách các lồng khác (soát cuối L6).

`CageSocket.applyEnable` (`internal/steps/cage.go`), theo thứ tự: kiểm layout, kiểm trạng thái cũ để có thể hoàn tác (`s.state`), kiểm cấu hình bằng root ngoài lồng (`cageFPMTest`, `php-fpm<bản> -t -R`), một phép thử hàng rào trong lồng (`runFenceTest`: `php-fpm -t` chạy dưới user của site qua lớp bọc, chứng minh systemd dựng được không gian riêng; lỗi do bị giết hay hết tài nguyên, `cage.ErrKilled`, thì bỏ qua vì đó là việc mã của site làm được; lỗi dựng không gian riêng như `226/NAMESPACE` thì bước hỏng), `systemctl reset-failed` (bỏ trạng thái `failed` còn từ lần trước), `systemctl enable --now <socket>`, rồi `waitCageSocket` chờ file socket xuất hiện, tối đa 20 giây. **Không khởi động dịch vụ**: request đầu tiên làm việc đó. Lý do: nếu bước này khởi động dịch vụ, mã của site đã chạy trong lồng trước khi bước giết tiến trình ngoài lồng; và không gì mã của site điều khiển được (giết tiến trình chính, trả mã lạ) làm bước này hỏng.

### 3.6 Giết tiến trình cùng uid ngoài lồng: `steps.CageReap`

```go
// cageStray: pid còn sống, mang uid của site, ở ngoài slice của site.
func cageStray(sys system.System, pid, uid, cg string) bool {
	st, err := sys.ReadFile("/proc/" + pid + "/status")
	...
	ids := strings.Fields(procField(st, "Uid"))
	if len(ids) < 2 || (ids[0] != uid && ids[1] != uid) {
		return false
	}
	...
	cgroup, err := sys.ReadFile("/proc/" + pid + "/cgroup")
	return err == nil && !cgroupUnder(cgroup, cg)
}
```

`cageReap` làm hai việc.

Một, gỡ crontab riêng của user site bằng `crontab -r -u <user>` chạy bằng root (`cageCrontabGone`; file còn sau lệnh thì lỗi). Lý do (soát cuối N3): crontab cài từ lúc site còn ngoài lồng vẫn chạy sau `cage.enable`, mỗi phút cron chạy mã của site ở ngoài hàng rào, không trần, cùng uid. Trong lồng thì `crontab` không chạy được (`NoNewPrivileges` chặn setgid, `/var/spool` bị che), nên phải gỡ từ ngoài.

Hai, dừng tiến trình, lặp tới `cageReapWait` (5 giây). Mỗi vòng:

1. `cageKillAll`: `kill -KILL -1` chạy dưới user của site **qua lớp bọc**, tức trong lồng. `kill(-1)` gửi tín hiệu tới mọi tiến trình cùng uid trên máy trong một lượt duyệt của kernel (lồng không có PID namespace riêng). Một chuỗi tiến trình "fork rồi tự thoát" liên tục (nhảy pid) sống qua được lệnh `kill <pid>`, nhưng không sống qua lượt `-1` (soát cuối N2).
2. Quét `/proc` (`cageScan`, đọc bằng Go qua `System`, không shell), tìm tiến trình mang uid của site mà cgroup không nằm dưới slice, đọc lại `status` lần nữa (pid có thể đã được dùng lại giữa lúc quét và lúc giết), rồi `kill -KILL <pid>...` bằng root. Đây là đường dự phòng khi mã của site lấp số tiến trình của slice và unit tạm của lớp bọc không khởi động được.
3. Quét lại; hết vòng 5 giây mà còn thì bước hỏng.

Lệnh `kill -1` trong lồng cũng giết cả tiến trình của site nằm trong slice. Chấp nhận được vì bước này chỉ chạy khi đằng nào cũng phải dừng (trước request đầu vào lồng, hoặc ngay trước khi `cage.restart` khởi động lại).

Vì sao cần: tiến trình ngoài lồng cùng uid với site mở cửa ra gốc thật của máy qua `/proc/<pid>/root` (bản thử t14, P4, mục 5.5). Cũng vì thế health có `cage.runtime` báo khi còn tiến trình như vậy, hoặc khi `/var/spool/cron/crontabs/<user>` còn file.

### 3.7 Vhost: `steps.SiteVhostCaged`

`internal/steps/cage.go` cuối file và `templates.SiteVhostCaged`: vhost giống hệt vhost thường, chỉ khác socket là `CageSocket()`. Chữ ký hàm vhost cũ không đổi, nên golden test của vhost ngoài lồng giữ nguyên từng byte (bất biến 1: site ngoài lồng không đổi hành vi). Trong `cageEnableSteps`, vhost lấy qua `siteVhost(in, cfg)` với bản sao `in.Caged = true`: một hàm chọn vhost cho mọi thao tác.

### 3.8 Một lệnh tpan chạy thay cho site: `cage.Wrap` trong `internal/cage/wrap.go`

Từ đây site ở trong lồng. Nhưng tpan còn chạy lệnh thay cho site: `wp` (WP-CLI), cron, sao lưu. Trước M17 các lệnh đó chạy bằng `runuser -u tp_<slug> -- wp ...`. Nếu để nguyên, `wp` chạy ngoài hàng rào và ngoài trần. Giải pháp là một **lớp bọc**:

```go
type Sys struct {
	system.System
	Rand func() (string, error)
}
func Wrap(sys system.System) *Sys { return &Sys{System: sys} }
```

`Sys` có một trường vô danh `system.System` (Go gọi là embedding): `Sys` tự có mọi phương thức của `System` bên dưới, và chỉ ghi đè cái nó muốn, ở đây là `Run`. Mọi nơi khác trong mã vẫn gọi `sys.Run(ctx, cmd)` như cũ, không biết có lớp bọc.

`Run` quyết theo bốn bước (đọc lời chú thích trên hàm):

1. `cmd.AsUser` rỗng, hoặc không có dạng `tp_<slug>` (`site.CageSlugOfUser`): chạy thẳng như cũ. Có `AsUser` kèm `Env`: từ chối (`Env` chỉ dành cho `tpan brain`).
2. `Stat` file hàng rào của slug đó (`site.CageFenceOfSlug`). Không có file (`fs.ErrNotExist`) **và** thư mục cấu hình của lồng `/etc/tpan/cage/<slug>` cũng không có: chạy như cũ, vì site đó ngoài lồng. Thiếu hàng rào mà thư mục cấu hình còn (lồng dựng dở, hoặc ai xoá nhầm `fence.conf` lúc dịch vụ vẫn chạy), hay lỗi lạ: trả lỗi, **không chạy gì**. Lớp bọc đóng khi nghi ngờ (soát cuối V1): không thì `wp` của health và cron chạy mã của site ngoài lồng. `cage disable` vẫn dọn được trường hợp chỉ còn thư mục đó. "Site có lồng hay không" được đọc từ **máy** (file hàng rào), không từ bảng `sites`: giữa một Plan vào lồng, bảng chưa đổi mà dịch vụ đã chạy trong lồng, và bước kiểm chứng của chính Plan đó phải chạy `wp` trong lồng.
3. Có hàng rào: dựng lệnh `systemd-run` bằng hàm `fenced`:

```go
args := []string{
	"--quiet", "--wait", "--pipe", "--expand-environment=no",
	"--service-type=oneshot", "--unit=" + site.CageRunPrefixOfSlug(slug) + tail,
	"-p", "User=" + cmd.AsUser, "-p", "Group=" + cmd.AsUser, "-p", "NoNewPrivileges=yes",
	"-p", "TimeoutStartSec=" + strconv.Itoa(secs), "-E", "LC_ALL=C",
}
...
args = append(append(args, "--", cmd.Name), cmd.Args...)
```

4. Mã thoát khác 0: hàm `classify` hỏi systemd lệnh chết vì sao (`systemctl show <unit> -p LoadState -p Result ...`).

Từng cờ có lý do, và đều từ số đo:

- `--unit=tpan-cage-<slug>-run-<16 hex>`: **drop-in theo tiền tố tên unit**. systemd nạp thư mục `tpan-cage-<slug>-.service.d/` cho mọi unit `.service` có tên bắt đầu bằng `tpan-cage-<slug>-`. Dịch vụ PHP tên `tpan-cage-<slug>-php.service`, lệnh tạm tên `tpan-cage-<slug>-run-<hex>.service`: cả hai nhận đúng một file `fence.conf`. Một hàng rào cho cả dịch vụ lẫn mọi lệnh. Slug chỉ có `a-z0-9_`, không có dấu `-`, nên không sinh thêm tiền tố nào lẫn vào (bản thử t6: slug có `-` làm slice lồng nhiều tầng).
- `--expand-environment=no`: mặc định `systemd-run` thay `$HOME`, `${PATH}` trong tham số bằng giá trị biến. Tham số của tpan có thể chứa `$` thật (mật khẩu, regex): phải giữ nguyên từng byte (test `TestRunThamSoKhoNguyenVen`).
- `--service-type=oneshot`: không có cờ này, lệnh chết vì `SIGTERM` được báo thành công mã 0 (t9). Có cờ: mã 255, `Result=signal` (t9b, mục 2.6).
- `-p User=... -p Group=...` lặp lại dù hàng rào cũng có: nếu `fence.conf` biến mất đúng lúc, unit tạm sẽ chạy bằng root không có rào. Có `User=` trên dòng lệnh thì mất drop-in cũng chỉ về mức của v0.2.0.
- `-p TimeoutStartSec=<giây>`: thời gian còn lại của `ctx` cộng 1, hoặc 3600 nếu không có hạn (`timeoutSec`). Khác `runuser`, lệnh trong lồng **không chết theo tpan** (nó là unit của systemd): phải tự đặt hạn.
- `-E LC_ALL=C`: ngôn ngữ cố định, output dễ đọc.

Vì sao `classify` cần hỏi systemd: `systemd-run` ra mã 1 cho cả "lệnh tự thoát mã 1" lẫn "bị giết vì chạm trần RAM" (t9: "vượt RAM: mã 1", "quá giờ: mã 1"). Mà `wp core is-installed` trả 1 để nói "chưa cài", một câu trả lời có nghĩa. Nên:

- `Result=exit-code`: lệnh tự thoát, trả `*system.ExitError` với mã thật (như trước M17).
- `Result` khác (`oom-kill`, `timeout`, `signal`...): trả `*cage.KilledError` (`internal/cage/types.go`), bọc `cage.ErrKilled`. Nơi gọi không nhầm "bị giết" với mã thoát có nghĩa.
- `LoadState=not-found`, hoặc mã từ 200 đến 243 mà `ExecMainStartTimestamp` rỗng (systemd chưa chạy được lệnh: `226/NAMESPACE`, `217/USER`, `203/EXEC`): lỗi thường "không chạy được lệnh".

Cuối `Run`: nếu `ctx` bị huỷ giữa chừng thì `systemctl stop <unit>` bằng một `ctx` mới 10 giây (`context.WithoutCancel`: bản sao của ctx cũ nhưng không bị huỷ theo). Không có việc này, lệnh trong lồng chạy tiếp sau khi tpan đã bỏ cuộc.

`Describe(cmd)` trả dòng lệnh **sẽ thật sự chạy** (`systemd-run ...` với đuôi unit là `xem`, không sinh số ngẫu nhiên). Engine và phép kiểm dùng nó thay cho `cmd.String()` để bằng chứng ghi đúng lệnh. Lý do (sổ quyết định mục 3): thiếu nó thì log của site trong lồng ghi dòng `runuser`, tức lệnh chạy ngoài lồng, và người làm theo dòng "sửa tay" sẽ tự chạy lệnh của site ngoài hàng rào.

Chi phí: `systemd-run true` mất 0,03 giây so với `runuser true` 0,002 giây (t1 mục 4); `wp core is-installed` mất 0,57 giây trong lồng so với 0,16 giây ngoài lồng trong cùng lần chạy đó. Mọi lệnh `wp` chậm thêm khoảng nửa giây. Nửa giây đó có thể thấy được khi chạy nhiều lệnh `wp` liên tiếp.

### 3.9 Số liệu: `cage.ReadUsage` và bộ lấy mẫu

`internal/cage/usage.go`, hàm `ReadUsage(sys, layout, now)`: mở thư mục `/sys/fs/cgroup/tpan.slice/tpan-<slug>.slice` (`Layout.CageCgroup()`) và đọc các file:

| Số | File cgroup |
|---|---|
| RAM hiện tại, đỉnh | `memory.current`, `memory.peak` |
| Lần bị giết vì OOM | `memory.events`: dòng `oom_kill` |
| CPU đã dùng, bị bóp | `cpu.stat`: `usage_usec`, `throttled_usec` |
| Tiến trình, đỉnh | `pids.current`, `pids.peak` |
| Đĩa | `io.stat`: `rbytes`, `wbytes` |
| Trần đang áp | `memory.max`, `cpu.max`, `pids.max`, `io.max` |

Hàm không chạy lệnh nào, chỉ `ReadFile` và `ReadDir` qua `System`, nên test dùng file mẫu chép từ máy ảo (`internal/cage/testdata`, nguồn `docs/bang-chung/M17-nghien-cuu/cgroup-mau/`). Thư mục cgroup chưa có (lồng chưa chạy từ khi máy lên) thì trả `Usage{Running: false}`, không lỗi. Dòng lạ thì bỏ, không `panic`.

Chỗ khó là `tasksHits` (số lần chạm trần tiến trình): kernel mới có `pids.events.local`, kernel 6.8 không. Hàm kiểm file đó; không có thì cộng `pids.events` của slice và của mọi cgroup con trực tiếp, vì bộ đếm nằm ở cgroup nơi gọi `fork` (số đo ở mục 2.3).

`tpan usage` và API đọc cgroup ngay lúc được gọi, không qua bảng mẫu (bất biến 6): con số bạn thấy bằng con số trong file cgroup.

Lịch sử và gợi ý, `internal/core/usage.go`:

- Cứ 5 phút (`UsageSampleEvery`) `Usages.Sample` đọc cgroup của mọi site `caged` và `active`, ghi một dòng vào bảng `usage_samples` (bộ hẹn giờ ở `internal/server/usage_timer.go`, chạy trong `tpan serve`). Mỗi giờ `Rollup` gộp mẫu thành `usage_hourly`, xoá mẫu cũ hơn 8 ngày (`UsageKeepSamples`) và dòng giờ cũ hơn 30 ngày (`UsageKeepHours`).
- Các bộ đếm cộng dồn (`oom_kill`, `usage_usec`) về 0 khi máy khởi động lại. `deltaOf` tính "phần tăng giữa hai mẫu" và coi `cpu_usec` giảm là "thế hệ cgroup mới" (`fresh`): khi đó phần tăng là giá trị mới chứ không phải hiệu âm.
- `suggest` là hàm thuần (nhận mẫu, trả `Suggestion`, có bảng test): RAM gợi ý là đỉnh nhân 1,25 làm tròn lên bội của 32 (không dưới 128); CPU là phân vị 95 nhân 1,5 làm tròn lên bội của 10 (không dưới 25). Dưới 288 mẫu (24 giờ) thì vẫn gợi ý nhưng gắn cờ `low_data`. Công thức đầy đủ ở mục 3.8 của bản thiết kế.

Giới hạn nói thẳng: site **ngoài** lồng không có cgroup riêng nên `tpan usage` không có số của chúng; lệnh nói rõ điều đó (`usage_not_caged`) thay vì ước lượng.

### 3.10 Phép kiểm và sửa tự động: `internal/checks/cage.go`

Site trong lồng dùng `SiteChecksCaged` (và `SiteChecksSSLCaged`): bộ phép kiểm thường, nhưng thay `php.socket` bằng bốn phép mới. `siteChecks` (một hàm, trong `internal/core/site.go`) là chỗ duy nhất chọn bản nào.

| ID | Kiểm gì |
|---|---|
| `cage.socket:<domain>` | file socket là socket, `www-data:www-data 0660`; unit socket `active`; dịch vụ không `failed` |
| `cage.runtime:<domain>` | khi dịch vụ chạy: tiến trình chính có `NoNewPrivs: 1`, `CapEff` bằng 0, cgroup dưới slice, `mountinfo` có tmpfs ở `/var`, `/run`, `/etc/tpan` và dòng gắn `/run/mysqld`; không có tiến trình cùng uid ngoài slice |
| `cage.fence:<domain>` | bảy lệnh chạy trong lồng (qua `AsUser`), mỗi lệnh một dòng bằng chứng |
| `cage.limits:<domain>` | `memory.max`, `memory.swap.max`, `cpu.max`, `pids.max`, `io.max` đúng với trần của gói |
| `cage.state:<domain>` | gói muốn lồng mà site ngoài lồng (hoặc ngược lại): cảnh báo; `caged=1` mà thiếu file hàng rào: lỗi |

`CageFence` là phép kiểm hay bị hỏi nhất. Nó chạy bảy lệnh argv dưới user của site và so với kết quả đúng:

```go
{system.Cmd{Name: "ls", Args: []string{"-A", "/var/www"}, AsUser: user},
	"chỉ thấy " + dom, "show only " + dom,
	func(r system.Result, _ string) int {
		return cageVerdict(r.ExitCode == 0 && cageSameSet(cageLines(r.Stdout), []string{dom}))
	}},
```

Bảy lệnh: `id -un` ra user của site; `ls -A /var/www` ra đúng tên miền; `ls -A /tmp` rỗng; `ls -A /run` ra `mysqld` và `systemd` (hoặc chỉ `systemd` khi MariaDB chưa chạy lúc lồng khởi động, vì dấu `-` ở lệnh gắn); `find /proc ... ! -user <user>` rỗng (không thấy tiến trình của user khác); `getent passwd` ra đúng 4 dòng; `test -w /etc` mã 1. Trước khi chạy, hàm `Stat` file hàng rào: thiếu file thì Fail và **không chạy lệnh nào**, vì lớp bọc chỉ đưa lệnh vào lồng khi file đó có, thiếu file thì bảy lệnh sẽ chạy ngoài lồng, đúng cái cửa mục 5.5 muốn đóng.

**Hai loại Plan, hai loại điều kiện.** Mã PHP của site quyết định mã HTTP site trả về, và giết được tiến trình chính của PHP-FPM (cùng uid). Nếu Plan `cage.enable` của admin dừng vì "site trả 500" thì site giữ được mình ở ngoài lồng. Nên `internal/core/cage.go`:

```go
// policyHard là tập phép cứng của Plan chính sách: điều site không điều khiển
// được. Chặng web, socket do systemd giữ, trần trên cgroup.
func policyHard(r checks.Result) bool {
	if r.Stage == checks.StageWeb {
		return true
	}
	for _, p := range []string{"cage.socket:", "cage.limits:", "php.socket:"} {
		if strings.HasPrefix(r.ID, p) {
			return true
		}
	}
	return false
}
```

`policyVerify` bọc mọi phép kiểm của Plan: phép không thuộc tập cứng mà Fail thì hạ thành Warn. Mã HTTP, WordPress, `cage.runtime` và cả `cage.fence` đều mềm trong Plan chính sách (`cage.enable`, `cage.disable`, `cage.sync`: sửa gói, đổi gói của user). Lý do riêng của `cage.fence` (sổ quyết định, mục 1): lúc kiểm chứng, mã của site đã nhận request và có thể lấp số tiến trình của slice bằng `exec`, làm `systemd-run` của chính `cage.fence` không khởi động được. Hàng rào đã được thử lúc `CageSocket` bật lồng (`runFenceTest`: `php-fpm -t` chạy trong lồng, trước khi mã của site vào slice). Health vẫn chạy `cage.fence` mỗi giờ như phép cứng. Plan của **chủ site** (đổi bản PHP, khôi phục, mở khoá) vẫn kiểm 200 như trước: site chỉ làm hỏng việc của chính nó.

### 3.11 Sửa gói thì trần đổi: `PrepareEditPackage`

`Cages.PrepareEditPackage` (`internal/core/cage.go`) dựng Plan `cage.sync` khi bạn đổi trần của gói mà có site đang trong lồng dùng gói đó. Mỗi site một bước `CageFiles` với trần mới; bước bỏ qua khi file đã đúng. Dòng gói được ghi **trước** job (móc `before` của `Planned`), không chờ máy: nên một site trả 500 hay tự giết tiến trình chính không giữ được trần cao (bất biến 11, `TestHaGoiKhiSiteTra500`). Trần đổi qua file slice và `daemon-reload`, có hiệu lực ngay cả khi dịch vụ đang chết (mục 2.3).

Hạ trần xuống dưới mức site đang dùng làm site vào vòng chết (bản thử t3 mục 6: hạ xuống 8M khi đang dùng 58 MB, site 502). Nên có trần dưới cho từng trường (RAM 64 MiB, `checkPackageLimits` trong `internal/core/users.go`, mã `package_limit_invalid`), và bản xem trước nêu rõ nếu trần mới thấp hơn mức đang dùng.

### 3.12 Thử tấn công: `internal/core/probe.go`

`tpan isolation probe` là một job tên `isolation.probe`. Plan dựng ở `ProbeJob.plan`:

```go
ss := []engine.Step{&probeRecord{j: j}}              // 1. ghi sổ TRƯỚC khi tạo gì
for i, l := range []site.Layout{j.a, j.b} {
	add, err := j.siteSteps(l, i == 0)                // 2. tạo A rồi B bằng bước của site.add
	...
}
ss = append(ss, &probeL8{j: j})                       // vhost lạ trùng tên (L8)
ss = append(ss, &probePass{j: j})                     // 3. lượt NGOÀI lồng
// ... cageEnableSteps cho A và B, rồi:
ss = append(ss, &probePass{j: j, inside: true})       // 5. lượt TRONG lồng
ss = append(ss, clean...)                             // 6. dọn
```

(Mình rút gọn từ hàm thật: có điều kiện `--only` và nhánh "máy không dựng được lồng".) Điểm đáng đọc:

- **Tên.** Hai site thử là `tpan-probe-a-<12 hex>.test` và `tpan-probe-b-<12 hex>.test`. `.test` là tên miền dành riêng, không bao giờ phân giải ra thật. `site.ProbeDomainRE` mô tả dạng tên; `Sites.PrepareAdd` từ chối tạo site khớp mẫu (`domain_reserved`).
- **Ghi sổ trước** (`probeRecord`): bước đầu tiên ghi dòng vào bảng `probe_runs` với trạng thái `running`, trước khi tạo thứ gì. Panel bị tắt giữa chừng thì sổ vẫn biết tên cần dọn.
- **Hai site không có dòng trong bảng `sites`** (nên health, danh sách site, ma trận cô lập không bao giờ thấy chúng) và file của chúng không vào `managed_files` (`probeJournal` bỏ qua `RecordFile`, `ForgetFile`): không thì health báo lệch cho file đã bị dọn.
- **Hai lượt cùng một bộ đường**. Lượt ngoài lồng cho biết đường nào lộ. Lượt trong lồng dùng đúng `cageEnableSteps` như thật, với trần cố định `probeLimits` (RAM 160 MB, CPU 100%, 60 tiến trình, 3 worker), rồi chạy lại. Mỗi ô kết quả là `blocked` (bị chặn), `exposed` (lộ), `needs_cage` (lộ ngoài lồng, chỉ lồng mới chặn), `skipped` hay `na`.
- **Các đường.** L1 tới L9 là chín đường của M14 (OPcache, `.htaccess`, symlink, log, `/proc`, vhost trùng); C1 tới C5 là đường riêng của lồng. Ví dụ C3 (`probeRunner.c3`): A cấp phát 320 MB. Trong lồng, hàm đọc `memory.events` trước và sau bằng `cage.ReadUsage`, rồi kết luận "bị chặn" chỉ khi tiến trình không xong việc, `oom_kill` tăng, và B vẫn trả 200. Ngoài lồng, C3 chỉ chạy khi máy còn ít nhất 1 GB RAM trống (`probeC3MinFree`): phép thử không được làm hại máy thật.
- **Dọn trong mọi trường hợp** (bất biến 5): `ProbeJob.Execute` chạy job; không thành công thì `cleanupAfterFailure` chạy ngay Plan `probe.cleanup` cho đúng hai tên đó. Panel chết giữa chừng: dòng `running` còn trong sổ; `tpan isolation probe --cleanup`, lượt thử kế tiếp và bộ hẹn giờ của `tpan serve` dọn nốt; health báo lỗi `probe.leftover` khi dòng quá 2 giờ. Plan dọn **chỉ nhận tên vừa khớp mẫu `.test` vừa có trong sổ** (`probeGated`, `probeLedger`): nó đọc sổ trong job (đã giữ khoá), nên không thể nhầm một site thật tên giống. Test: `TestProbeHongGiuaChungVanDon`, `TestProbeCleanupTuChoiTenLa`.
- **Mã thoát.** CLI (`runProbe` trong `cmd/tpan/isolation.go`) thoát mã 1 khi có ô `exposed`, hoặc job không thành công.

### 3.13 API và bộ hẹn giờ: `internal/server/api_m17.go`

Cùng ca sử dụng, bọc thành route HTTP: `handleCage` (danh sách), `handleSiteCage` (`POST /api/v1/sites/{domain}/cage`, admin), `handleUsage`, `handleSiteUsage`, `handleUsageSuggestions`, `handleProbe`, `handleProbeRun`, `handleProbeCleanup`. Quyền mỗi route nằm ở bảng route (mặc định cấm; user thường gọi route của admin thì 403), và `TestPhanQuyenMoiRoute` trong `internal/server/authz_test.go` duyệt mọi route. `handleProbeRun` kiểm `only` ở biên (chỉ rỗng, `ngoai` hay `trong`) vì giá trị đó đi vào tên sổ và quyết định bước nào chạy. Sửa gói hay sửa user mà làm trần của site trong lồng đổi thì API trả `202 JobStarted` (job `cage.sync`) thay cho `200`.

Giao diện (`web/`) gọi đúng các route này: mục 3.15.

### 3.14 Ra khỏi lồng: `PrepareDisable`

Đi ngược: `steps.SitePool` ghi lại pool chung từ template, vhost về bản thường, `CageSocket{Disable: true}` (`disable --now` socket, dừng dịch vụ và slice, gỡ thư mục socket), rồi `CageFiles{Remove: true}` gỡ **hàng rào trước tiên**, rồi các file còn lại. Vì pool và vhost sinh lại từ template và `cageDrift` đã đảm bảo lúc vào chúng đúng template, ra lồng trả lại đúng từng byte (test `TestCageEnableDisableTraLaiTungByte`). `PrepareDisable` không đòi `caged=1`: nó đọc máy xem còn file của lồng không, nên cũng là cách dọn một lồng dựng dở khi panel bị tắt giữa `cage.enable`.

### 3.15 Giao diện (phần làm sau M16, ngày 25/11/2026)

Giao diện không thêm luật mới nào: mọi việc đổi máy vẫn đi qua đúng các route ở mục 3.13, và qua luồng "xem trước rồi chạy" có sẵn (`usePlanFlow` trong `web/src/api/m13-users.ts`). Đặc tả hình thức nằm ở `docs/GIAO-DIEN.md` (mục 7, các dòng có chữ M17, và bảng thuật ngữ: "lồng", "mức tối đa", "chạm mức tối đa", "thử tấn công"...). Chữ giao diện không dùng "trần": người dùng đọc "mức tối đa".

Đi theo đường dữ liệu, từ dưới lên:

1. **Kiểu và hàm gọi API**: `web/src/api/m17-types.ts` chép đúng hình JSON của Go (`CageRow`, `SiteUsage`, `Suggestion`, `ProbeReport`...), `web/src/api/m17.ts` có các hook đọc (`useCages`, `useUsage`, `useSiteUsage`, `useSuggestions`, `useProbe`) và ba "yêu cầu Plan" (`cageRequest`, `probeRequest`, `probeCleanupRequest`) để đưa vào `usePlanFlow`.
2. **Phép tính thuần**: `web/src/screens/tai-nguyen/model.ts` (đổi byte ra MiB, phần trăm so với mức tối đa, CPU giữa hai lần đọc, gộp theo user), `screens/goi/model.ts` (kiểm form gói theo đúng thứ tự như server), `screens/bao-mat/probe-model.ts` (đổi trạng thái của một ô thử tấn công sang bốn dấu trạng thái). Ba file này có test chạy bằng `make test-web`, không cần trình duyệt.
3. **Màn mới "Tài nguyên"** (`/usage`, `screens/tai-nguyen/`): bảng theo site (RAM đang dùng so với mức tối đa, đỉnh, tiến trình, số lần chạm mức tối đa, CPU), bảng theo user (chỉ admin), thẻ "Gợi ý mức tối đa" (chỉ admin; mở từng gói thấy từng câu giải thích của `core.Suggestion.Reasons`). User thường cũng vào được nhưng chỉ thấy site của mình: route `GET /api/v1/usage` đã lọc theo chủ ở server, giao diện không lọc thêm.
4. **Màn Gói** (`screens/goi/`): cột "Lồng" và "Mức tối đa trong lồng"; hộp sửa gói có ô chọn lồng và sáu ô số. Sửa gói mà gói có site đang trong lồng và một trong sáu mức đổi thì nút của hộp đổi từ "Lưu" sang "Xem trước": server sẽ chạy job `cage.sync` để áp mức mới lên slice, nên người dùng phải thấy bản xem trước trước khi máy đổi.
5. **Trang một site**: thẻ "Lồng" ở tab Tổng quan (`screens/site/CageBlock.tsx`): trạng thái, mức tối đa của gói, số đang dùng khi trong lồng, nút "Vào lồng" hoặc "Ra khỏi lồng" (chỉ admin). Gói chưa bật lồng thì nút "Vào lồng" bị khoá kèm lời giải thích. Trạng thái "trong lồng" hay "ngoài lồng" viết bằng chữ thường, **không** tô xanh hay đỏ: ba màu trạng thái chỉ nói về tình trạng của máy, mà ngoài lồng không phải là lỗi. Chỉ "chờ vào lồng", "chờ ra khỏi lồng" mới có dấu cảnh báo, vì khi đó máy lệch với gói.
6. **Màn Bảo mật**: dòng "Thử tấn công" trong thẻ "Cô lập giữa các site" (`screens/bao-mat/Probe.tsx`). Lúc mới mở chỉ là một dòng trạng thái; mở ra có nút chọn lượt, nút chạy (qua bản xem trước, nói tên hai site thử), câu báo khi còn lượt chưa dọn kèm nút dọn, và bảng mỗi đường một hàng với hai cột Ngoài lồng, Trong lồng. Ma trận cô lập ghi chữ "lồng" ở hàng của site trong lồng (vì sao, xem câu tự kiểm 11).

Dữ liệu cho chế độ `--demo` (`internal/demo/m17.go`) đi qua **chính mã thật**: hai site vào lồng bằng `core.Cages.PrepareEnable` và job thật trên máy giả; máy giả chỉ đóng vai systemd và nhân (file cgroup có số nhúc nhích mỗi 15 giây, trả lời `systemctl show`). Nhờ vậy ảnh chụp trên demo và phép kiểm `cage.*` của demo đều thật như trên máy.

## 4. Kiến thức nền

Các lệnh trong mục này mình **chưa chạy** trong phiên viết file; chạy trên máy ảo (từ Mac, trong thư mục repo) bằng `scripts/vm.sh root <lệnh>`. Các lệnh cần một site đã vào lồng; thay `<slug>` bằng slug thật (tìm bằng `ls /sys/fs/cgroup/tpan.slice`). Output tương ứng có trong mục 2 (từ các bản thử).

### 4.1 Namespace (không gian tên)

Namespace là cách nhân Linux cho một nhóm tiến trình một "cái nhìn riêng" về một thứ: cây thư mục (mount namespace), danh sách tiến trình (PID), mạng, `/tmp`... Hai tiến trình khác namespace nhìn cùng đường dẫn `/var/www` có thể thấy hai thư mục khác nhau. Docker dựng nhóm namespace này; ở đây systemd dựng chúng từ các thuộc tính trong unit. Chỗ trong mã: `internal/templates/files/cage-fence.tmpl`. `TemporaryFileSystem=/var:ro` thay `/var` bằng một thư mục rỗng chỉ đọc (trong namespace của dịch vụ), rồi `BindPaths=` gắn lại đúng thư mục của site (bind mount: một thư mục xuất hiện ở hai nơi). `PrivateTmp=yes` cho `/tmp` riêng. `ProtectProc=invisible` giấu tiến trình của uid khác trong `/proc`.

Tự nhìn: vào máy ảo, lấy `MainPID` của dịch vụ lồng rồi xem bảng mount của nó:

```bash
scripts/vm.sh root bash -c 'pid=$(systemctl show -p MainPID --value tpan-cage-<slug>-php.service); grep -E " /(var|run|etc/tpan) " /proc/$pid/mountinfo'
```

Phép kiểm `cage.runtime` đọc đúng file `mountinfo` này để biết các tmpfs có mặt (`parseMountinfo` ở `internal/steps/cage.go`, `cageParseMounts` ở `internal/checks/cage.go`).

### 4.2 cgroup v2

cgroup (control group) là cây thư mục ảo ở `/sys/fs/cgroup` mà nhân dùng để **đếm và giới hạn** tài nguyên của một nhóm tiến trình. Mỗi thư mục là một nhóm; các file trong đó là bộ đếm và trần (`memory.max`, `cpu.max`, `pids.max`, `io.max`). Ghi một số vào `memory.max` là đặt trần RAM; đọc `memory.current` là xem đang dùng bao nhiêu. Namespace nói site thấy gì; cgroup nói site dùng được bao nhiêu. Hai thứ độc lập nhau.

Chỗ trong mã: `internal/cage/usage.go`, hàm `ReadUsage`, đọc các file trên. Và `Layout.CageCgroup()` trong `internal/site/cage.go` cho đường dẫn thư mục. Tự nhìn:

```bash
scripts/vm.sh root bash -c 'cd /sys/fs/cgroup/tpan.slice/tpan-<slug>.slice && cat memory.max cpu.max pids.max memory.current memory.events'
```

### 4.3 Slice của systemd

Slice là một cgroup có tên mà systemd quản lý. Unit nào ghi `Slice=tpan-<slug>.slice` thì tiến trình của nó nằm trong cgroup đó, và các thuộc tính `MemoryMax=`, `CPUQuota=`, `TasksMax=` ghi trong file slice trở thành trần của cgroup. Cả dịch vụ PHP lẫn lệnh `wp` tạm của site cùng slice nên **chung một trần**: site không thoát trần bằng cách chạy việc nặng qua `wp`. `tpan.slice` là slice cha ngầm (không có file), nên cgroup đầy đủ là `/sys/fs/cgroup/tpan.slice/tpan-<slug>.slice`.

Chỗ trong mã: `internal/templates/files/cage-slice.tmpl`, `Layout.CageSlice()`. Đổi trần khi site đang chạy chỉ cần ghi lại file slice rồi `daemon-reload` (mục 2.3).

```bash
scripts/vm.sh root systemctl show tpan-<slug>.slice -p MemoryMax -p CPUQuotaPerSecUSec -p TasksMax
```

### 4.4 Socket activation

systemd tạo và giữ một socket, và chỉ khởi động dịch vụ khi có kết nối đầu tiên; dịch vụ nhận socket đã mở ở file descriptor số 3 (biến môi trường `FPM_SOCKETS` báo cho PHP-FPM biết). Dịch vụ dừng hay khởi động lại thì socket vẫn nghe, kết nối mới xếp hàng chờ. Chỗ trong mã: `internal/templates/files/cage-socket.tmpl` (`ListenStream`, `SocketUser`, `SocketMode=0660`), `cage-service.tmpl` (`FPM_SOCKETS`), `steps.CageSocket.applyEnable` (`enable --now` socket, **không** khởi động dịch vụ).

Tự nhìn: dịch vụ ngủ mà socket vẫn nghe.

```bash
scripts/vm.sh root bash -c 'systemctl is-active tpan-cage-<slug>-php.socket tpan-cage-<slug>-php.service; ls -l /run/tpan-cage/*/php.sock'
```

Nếu site vừa ngủ, dòng đầu mong đợi là `active` rồi `inactive`; chạy `curl --resolve <domain>:80:127.0.0.1 http://<domain>/` rồi hỏi lại thì dịch vụ `active`.

### 4.5 Drop-in theo tiền tố tên unit

Drop-in là file cấu hình bổ sung cho một unit, đặt trong thư mục `<tên unit>.d/`. systemd còn cho đặt thư mục tên kết thúc bằng dấu `-` (ví dụ `tpan-cage-<slug>-.service.d/`): file trong đó áp cho **mọi unit** có tên bắt đầu bằng phần tiền tố đó. Mã tận dụng điều này: dịch vụ `tpan-cage-<slug>-php.service` và lệnh tạm `tpan-cage-<slug>-run-<hex>.service` nhận chung `fence.conf`. Chỗ trong mã: `Layout.CageFenceDir()` ở `internal/site/cage.go`, và `fenced` trong `internal/cage/wrap.go` (đặt tên `--unit=` theo tiền tố).

```bash
scripts/vm.sh root systemctl cat tpan-cage-<slug>-php.service
```

`systemctl cat` in file unit và mọi drop-in áp cho nó; mong đợi thấy `fence.conf` ở cuối.

### 4.6 OOM killer theo cgroup

Khi một cgroup chạm `memory.max` và không thu hồi được bộ nhớ, nhân giết **một tiến trình trong cgroup đó** (không phải cả máy). Bộ đếm `memory.events` ghi `oom_kill`. Bản thử t3 mục 1: tiến trình con PHP bị giết sau 0,06 giây, `oom_kill 1`. Với systemd, `OOMPolicy=continue` bảo systemd: tiến trình con bị giết thì dịch vụ cứ chạy tiếp; mặc định (`stop`) nó dừng cả dịch vụ. Chỗ trong mã: `cage-service.tmpl` (`OOMPolicy=continue`), `cage-slice.tmpl` (`MemoryMax` kèm `MemorySwapMax=0`), `cage.Usage.OOMKills`, và `probeRunner.c3`.

```bash
scripts/vm.sh root bash -c 'cat /sys/fs/cgroup/tpan.slice/tpan-<slug>.slice/memory.events; systemctl show -p OOMPolicy tpan-cage-<slug>-php.service'
```

### 4.7 `systemd-run`

`systemd-run` chạy một lệnh như một unit tạm của systemd, nên nó có thể được gắn vào một slice và nhận drop-in như unit thường. `--wait` chờ lệnh xong; `--pipe` nối stdin, stdout, stderr; `--service-type=oneshot` coi lệnh là việc chạy một lần. Chỗ trong mã: hàm `fenced` trong `internal/cage/wrap.go`. Tự nhìn một lệnh qua lồng (cần site đã vào lồng, chạy bằng root):

```bash
scripts/vm.sh root systemd-run --quiet --wait --pipe --service-type=oneshot --unit=tpan-cage-<slug>-run-thu -p User=tp_<slug> -- ls -A /var/www
```

Mong đợi chỉ ra tên miền của chính site (như mục 2.2). Tên unit `...-run-thu` khớp tiền tố nên nhận hàng rào. Dòng lệnh đầy đủ mà tpan dựng thì xem `fenced`.

### 4.8 Capability và `NoNewPrivileges`

Quyền root của Linux được chia thành các mảnh gọi là capability (đổi uid, `chown`, nạp module nhân...). `CapabilityBoundingSet=` rỗng nghĩa tiến trình không bao giờ có mảnh nào; `NoNewPrivileges=yes` chặn việc lấy thêm quyền qua file setuid (`sudo`, `su`). Đó là lý do tiến trình chính PHP-FPM chạy bằng user thường được: `CapEff: 0000000000000000`, `NoNewPrivs: 1` (mục 2.2). Chỗ trong mã: `cage-fence.tmpl`, `CageRuntime` trong `internal/checks/cage.go` đọc hai dòng đó từ `/proc/<MainPID>/status`.

```bash
scripts/vm.sh root bash -c 'pid=$(systemctl show -p MainPID --value tpan-cage-<slug>-php.service); grep -E "^(Uid|CapEff|NoNewPrivs)" /proc/$pid/status'
```

## 5. Cách khác đã cân nhắc, và chỗ đã làm sai

Số đo lấy từ `docs/bang-chung/M17-nghien-cuu/`; tên bản thử trong ngoặc.

### 5.1 Trần RAM mềm (`MemoryHigh`) thay cho trần cứng

Nghe hợp lý: bóp site thay vì giết request. Đã thử (t3, mục 2): đặt `MemoryHigh=200M` không có `MemoryMax`, một request xin 400 MB không bị giết mà bị bóp tới 40 giây chưa xong, và **trang chủ của A mất 27,8 giây**. Site treo toàn bộ thay vì một request chết nhanh. Chọn trần cứng: request vượt trần chết sau 0,06 giây, request sau trả 200 sau 0,04 giây. Trần mềm không còn chỗ nào trong mã (template slice chỉ có `MemoryMax`).

### 5.2 Bỏ `OOMPolicy=continue`

Mặc định systemd khi một tiến trình của dịch vụ bị OOM kill là dừng cả dịch vụ. Lần đo lại (t3b mục a) bỏ dòng này: request xin 400 MB ra 502 như cũ, nhưng journal ghi `Failed with result 'oom-kill'`, `Scheduled restart job, restart counter is at 1`: cả dịch vụ khởi động lại, mọi request đang chạy của site chết, OPcache mất. Có dòng `continue` thì `NRestarts=0` (t3). Một ghi chú để bạn không bối rối khi tự đọc: lần đo đầu (t3, mục "1b") bỏ dòng và thấy `NRestarts=0`, không tái hiện; lần đo lại có tên riêng (t3b) mới cho kết quả ổn định. Mình giữ dòng `OOMPolicy=continue` vì hậu quả của việc thiếu nó nghiêm trọng hơn và lần đo lại cho thấy nó.

### 5.3 `PrivateUsers=yes`

Thuộc tính này dựng thêm user namespace, đẹp về mặt cô lập. Đo (t2b): WordPress chạy, điểm bảo mật 1.3. Nhưng máy đặt `user.max_user_namespaces=0` (một số máy chủ tắt tính năng này) thì dịch vụ không khởi động (502, mã 217), và trong lồng nhóm `www-data` hiện thành `65534`. Lợi ích thêm nhỏ khi đã không có capability nào và có `NoNewPrivileges`. Bỏ khỏi `cage-fence.tmpl`.

### 5.4 Tiến trình chính chạy bằng root (như bản thử t5 của M14)

Đề bài gốc chỉ nói "dịch vụ PHP-FPM riêng nằm trong slice". M14 từng thử để tiến trình chính bằng root, vì PHP-FPM cần root để đổi user của tiến trình con và `chown` socket. Cách đó giữ `CAP_SETUID`, `CAP_SETGID`, `CAP_CHOWN`, `CAP_KILL` ngay trong lồng. Bản thử t1 cho thấy làm được **không cần root**: systemd giữ socket (không còn `chown`), PHP-FPM chạy thẳng bằng user của site (không còn đổi user). Kết quả: không còn tiến trình root nào trong lồng, điểm `systemd-analyze security` từ 9.4 xuống 1.3 (mục 2.2).

### 5.5 Một ngoại lệ "chạy ngoài lồng cho nhanh" (đã loại), và vì sao `CageReap` tồn tại

Bản thiết kế đầu định cho `checks.Isolated(from, to)`, phép kiểm "user A có đọc được file của B không", chạy các lệnh `test` dưới user của A **ngoài** lồng (một trường `Cmd.Bare`, cho nhanh, không qua `systemd-run`). Lần soát thiết kế chỉ ra đó là lỗ hổng và đã loại trước khi viết mã: trong mã không có `Bare`. Bản thử t14 (mục P4) là bằng chứng:

```
  trong lồng thấy pid của sleep ngoài lồng: [37294]
  ls /proc/37294/root/var/www: a.example.com b.example.com c.example.com ...
  đã giữ fd 3; chờ sleep ngoài lồng thoát...
  sleep đã thoát; ls /proc/self/fd/3/var/www: a.example.com b.example.com ...
  ghi được vào /proc/self/fd/3/tmp: CO
  file /tmp/tpan-p4-thu ở máy thật: CO
```

Nghĩa là: **một** tiến trình cùng uid với site chạy ngoài lồng, dù chỉ vài mili giây như lệnh `test`, là đủ để mã trong lồng mở `/proc/<pid>/root` ra gốc thật của máy, giữ file descriptor đó sau khi tiến trình đã thoát, và ghi được vào `/tmp` của máy. `ProtectProc=invisible` chỉ giấu tiến trình của uid *khác*; systemd 255 chưa có PID namespace riêng. Hệ quả trong mã hiện tại:

- **Không có ngoại lệ nào.** Lệnh chạy dưới user của site trong lồng luôn đi qua `cage.Wrap`, tức luôn trong hàng rào; thiếu hàng rào mà lồng còn dấu vết thì không chạy gì (mục 3.8).
- `Isolated(from, to)` **không sinh ra khi `from` ở trong lồng**. Site trong lồng không nhìn thấy site khác là điều `cage.fence` và `cage.runtime` chứng minh. Có test riêng cho việc này.
- `CageReap` tồn tại vì một lý do: trước khi mã của site chạy trong lồng, không được còn tiến trình cùng uid nào ở ngoài. Tiến trình nền do mã của site sinh từ lúc còn ngoài lồng, cron của riêng user, lệnh `wp` dở dang đều là loại đó (mục 3.6).
- `cage.runtime` báo khi còn tiến trình như vậy; mọi dòng "sửa tay" in bằng `system.Describe`, ra dạng `systemd-run` cho site trong lồng.

Còn hở, nói thẳng: người vận hành tự gõ `runuser -u tp_<slug> -- ...` bằng tay trên máy thì tự mở lại cửa này. Đừng làm vậy với site đã vào lồng.

### 5.5b Phép thử an ninh từng báo "bị chặn" mà không đo gì

Chỗ agent làm sai thật, phát hiện ở đợt chạy thật (`docs/thay-doi/M17.md`, đợt 4, mục 5). Các file PHP thử gọi lệnh bằng `proc_open` với stderr đổ vào `/dev/null`. `open_basedir` của pool chặn mở `/dev/null`, nên `proc_open` trả `false`, và mọi đường cần gọi lệnh (C1, C2, C4, C5, L6, L7) ra "bị chặn" ở cả hai cột: kết quả đẹp mà không đo gì. Sửa: stderr vào pipe, và lệnh không chạy được thì ô ra `skipped`, không bao giờ `blocked` (có test). Bài học đáng nhớ để nói với người review: một phép thử an ninh phải phân biệt "bị chặn" với "không đo được". Một ô xanh chưa phải bằng chứng; phải biết phép thử có thể đỏ.

### 5.6 Lỗi "lồng không thấy MariaDB": bốn cách đã thử, ba cách không đủ

Vấn đề (mục 2.5): lồng chỉ thấy `/run/mysqld` có lúc nó khởi động. Lúc máy vừa lên, lồng có thể được đánh thức (bởi request đầu) trước khi MariaDB tạo thư mục. Các cách đã thử:

- **Lệnh gắn bắt buộc** (không dấu `-`): lồng chết với `status=226/NAMESPACE`, systemd thử lại liên tục (`NRestarts=5`), chậm thêm chút thì chạm giới hạn khởi động và site 502 (t11, mục a).
- **Lệnh gắn có dấu `-`**: lồng chạy nhưng không bao giờ thấy MariaDB (500 mãi, kể cả sau khi MariaDB lên) cho tới khi lồng khởi động lại. Nhận ra được qua `/proc/<MainPID>/mountinfo` không có dòng ` /run/mysqld ` (t11, mục b). Đây là hành vi mã giữ, cộng với cách sửa dưới đây.
- **`After=mariadb.service`** trong dịch vụ: không giúp gì (t11, mục c). `After=` chỉ xếp thứ tự hai việc cùng được khởi động trong một lượt; không chờ MariaDB khi MariaDB không nằm trong lượt đó.
- **`ExecStartPre=+install -d ... /run/mysqld`** trong unit **dịch vụ**: dấu `+` trong systemd nghĩa là "chạy với đầy đủ quyền, không qua hạn chế của unit". Giả sử đó là lối thoát. Đo (t13b): `install: invalid user 'mysql'`. Trên systemd 255, lệnh có dấu `+` **vẫn thấy `/etc/passwd` riêng của lồng** (không có user `mysql`), tức vẫn nằm trong không gian riêng; `mkdir` cũng không tới được `/run` thật.

Cách đã chọn: đặt `ExecStartPre=-/usr/bin/install -d -m 0755 -o mysql -g root /run/mysqld` trong unit **socket** (`cage-socket.tmpl`). Unit socket chạy bằng root ở máy thật, trước khi nghe, không qua hàng rào. Đo lúc máy khởi động thật (t12b): lúc gọi 500 (MariaDB chưa chạy), MariaDB lên thì 200 mà không đụng lồng, `mountinfo` có ` /run/mysqld `. Phép kiểm `cage.runtime` đọc `mountinfo` để bắt trường hợp còn lại (thư mục mất giữa chừng), kèm sửa tự động `cage.restart`.

### 5.7 `RestartSec` mặc định: hạ trần RAM làm cả socket chết

Đo ở t14 mục P1: hạ trần RAM xuống 8M khi site đang chạy, rồi trả về 200M. Với cấu hình mặc định của systemd (5 lần khởi động trong 10 giây):

```
  sau 12 giây ở 8M: dịch vụ failed (signal, NRestarts=20) | socket failed (service-start-limit-hit) | file socket MAT
  trả 200M + daemon-reload: A HTTP 502 | dịch vụ failed | socket failed
  reset-failed + start socket: A HTTP 200 | dịch vụ active | socket active
```

Cả dịch vụ **và socket** vào `failed`, file socket bị gỡ, và trả lại trần không cứu được: chỉ `reset-failed` rồi `start` socket mới cứu. Cách sửa (t15): `RestartSec=5s`, `StartLimitIntervalSec=30`, `StartLimitBurst=10`. Mười lần × 5 giây = 50 giây, quá cửa sổ 30 giây nên không bao giờ chạm giới hạn. Thử lại dưới tải: dịch vụ ở `activating`, socket vẫn `active`, file socket còn; trả trần 200M thì site **tự** trả 200 sau 1,7 giây, không lệnh nào khác. Đồng thời mọi đường trả lại của Step làm `reset-failed` (bất biến 12: hoàn tác không bao giờ để site kẹt ở `failed` mà vẫn báo "đã hoàn tác"), và `CageFiles.Undo` trả lỗi `undo_failed` thay vì nói dối.

### 5.8 Plan chính sách chờ kiểm 200

Bản thiết kế đầu ghi dòng gói trong `after` của job có kiểm 200. Soát thiết kế chỉ ra lỗ hổng: mã PHP của site quyết định mã HTTP, giết được tiến trình chính của PHP-FPM, và lấp được số tiến trình của slice. Một site cố tình trả 500 cho `127.0.0.1` làm job hỏng, thế là admin không hạ được trần của nó, hoặc `cage.enable` không bao giờ thành công. Cách sửa: tách hai loại Plan (mục 3.10), `policyVerify` hạ phép ngoài tập cứng xuống Warn, và `PrepareEditPackage` ghi dòng gói **trước** job (móc `before`). `cage.fence` ban đầu nằm trong tập cứng, và bị chuyển sang mềm lúc viết mã vì lý do `exec` ở mục 3.10 (sổ quyết định, mục 1). Test: `TestHaGoiKhiSiteTra500`.

### 5.9 Docker, hoặc `systemd-nspawn`

Mình **chưa thử** hai cách này và không có số đo; lý do chọn systemd thuần là lý do thiết kế: tpan là một file chạy, không cần daemon, và `systemd` đã có sẵn trên mọi máy Ubuntu mà tpan hỗ trợ; thêm Docker là thêm một daemon, một kho image, một lớp mạng. Chi phí của lồng systemd đã đo: 34 đến 39 MB mỗi lồng đang thức, gần như 0 khi ngủ (mục 2.4). Nếu người review hỏi "đo Docker chưa", trả lời đúng: chưa.

### 5.10 Giới hạn còn lại, nói thẳng

Lấy từ bản thiết kế mục 5 và sổ quyết định; không phải mình chế:

- **Mạng dùng chung.** `PrivateNetwork=yes` làm WordPress hỏng (t2b). Nên từ trong lồng vẫn đọc được `/proc/self/net/tcp` (kết nối TCP của mọi site) và tên socket của site **ngoài** lồng. Probe C5 ghi thật điều này ("lộ ngoài lồng").
- **Apache, `.htaccess` chế độ `day-du`.** Apache chạy bằng `www-data` nối được mọi socket lồng; ở chế độ này site đặt được `SetHandler` trỏ sang socket của lồng khác, lồng không chặn được. Bản xem trước của `cage.enable` có dòng cảnh báo (`PrepareEnable`).
- **Không phủ:** việc site đẩy sang dịch vụ chung (truy vấn nặng trong MariaDB, TLS và file tĩnh trong web server), dung lượng đĩa (đầy đĩa), băng thông mạng.
- **Mail.** `sendmail` không chạy được trong lồng; vốn đã không gửi được ở ngoài lồng trên stack của TPAN (t2).
- **Trần CPU trên máy ảo rảnh** nhận ít hơn mức đặt (mục 2.3); không bao giờ nhiều hơn.
- **Số lần chạm mức tiến trình có thể đếm thiếu.** Trên kernel 6.8, bộ đếm nằm ở cgroup nơi gọi `fork` (bản thử t3b), tức cgroup của dịch vụ PHP. Dịch vụ khởi động lại (needrestart sau khi apt nâng gói PHP, hay đổi bản PHP) thì cgroup mới bắt đầu từ 0. Thấy thật ngày 25/11/2026: một lượt e2e bước 20 có apt nâng gói nền đúng lúc phép thử, `tpan usage` ra `tasks_hits` bằng 0 dù script đọc thẳng `pids.events` lúc chạm mức thấy 1. Số lần bị giết vì RAM (`oom_kill` ở `memory.events` của slice) không bị ảnh hưởng, vì slice không khởi động lại.

### 5.11 Gộp với M28: luật lệnh root từ chối chính lồng

M28 (vá theo rà soát v0.2.0) thêm một luật ở tầng thấp nhất, `checkRootZone` trong `internal/system/rootcmd.go`: lệnh nào chạy bằng root mà có đường dẫn nằm trong `/var/www` thì phải khớp một danh sách được phép, không thì bị từ chối trước khi chạy. Lý do: thư mục site thuộc user của site, user đó có thể đặt symlink trỏ ra `/etc/shadow`; root đi theo symlink là lộ.

Gộp `dev` vào nhánh M17 làm hỏng ba chỗ, vì M17 viết trước luật này:

1. **Lớp bọc của lồng.** `cage.Wrap` chạy lệnh của site bằng `systemd-run ... -p User=<user của site> -- <lệnh>`. `systemd-run` chạy bằng root, và lệnh sau `--` (ví dụ `wp plugin list --path=/var/www/...`) có đường dẫn trong vùng site, nên luật từ chối. Nhưng lệnh đó thật ra chạy bằng user của site trong hàng rào, đúng thứ M28 muốn. Sửa: hàm `cageRunAsSite` nhận **đúng** dạng `cage.Wrap` dựng: chỉ các cờ đã biết; đúng một `--unit=tpan-cage-<slug>-run-<16 hex>`, một `User=` và một `Group=`, cả hai phải là `tp_<slug>` lấy từ chính tên unit (thiếu tên unit thì hàng rào, vốn áp theo tiền tố tên unit, không áp; `Group=` của site khác thì đọc được file của site đó); bắt buộc `NoNewPrivileges=yes`; thư mục làm việc phải là đường dẫn tuyệt đối đã gọn, và nếu nằm trong vùng site thì chỉ được là gốc của site; không có `Dir` hay biến môi trường chạm vùng site. Lệch một chi tiết (ví dụ `User=root`, thêm `ExecStartPre=`, `User=` lặp hai lần) thì vẫn bị từ chối. Test: `internal/system/rootcmd_m17_test.go` liệt kê từng biến thể bị từ chối.
2. **Thử tấn công tạo symlink rồi đổi chủ bằng root.** `tpan isolation probe` dựng site thử có symlink. Cách cũ: root tạo symlink rồi `chown`, đúng kiểu M28 cấm. Cách mới: chạy `ln -s -T` bằng chính user của site thử, khỏi cần đổi chủ (`-T`: nếu chỗ đặt link lỡ là thư mục thì báo lỗi, không tạo link bên trong nó).
3. **Dọn site thử bằng `rm -r` của root trong `/var/www`.** Cách mới theo đúng đường của thùng rác (M10): chuyển thư mục ra `/var/lib/tpan/trash/...` rồi mới xoá ở đó, ngoài vùng site.

Chỗ đáng nhớ: không nới luật chung của M28 cho tiện. Mỗi chỗ hỏng sửa theo cách M28 muốn (chạy bằng user của site, hoặc đi đường ngoài vùng site), và chỗ duy nhất luật phải nhận thêm là một dạng lệnh hẹp tới mức test liệt kê được các biến thể bị từ chối. Bản đầu của `cageRunAsSite` còn lỏng ba chỗ (không ràng User với tên unit, không bắt buộc có unit, nhận thư mục tương đối); thợ soát tìm ra, đã siết (bảng G1 đến G4 ở `docs/thay-doi/M17.md`). Sau khi sửa, e2e bước 20 chạy lại trên mã cuối (mục 2.7).

### 5.12 Sửa gói từ giao diện bị 400: hai đầu của một hợp đồng

Phần lõi M17 đổi thân của `POST /api/v1/packages/{name}` từ `account.Package` (có trường `name`) sang `PackageEdit` (không có `name`, vì tên nằm trên đường dẫn). Server đọc JSON chặt (`DisallowUnknownFields`: trường lạ là 400, để gõ sai tên trường không bị lặng lẽ bỏ qua). Giao diện, kể cả bản trước M17, luôn gửi kèm `name`. Kết quả: từ lúc phần lõi vào `dev`, bấm Lưu trong hộp sửa gói là nhận 400. Test của phần lõi gọi API bằng thân viết tay nên không thấy; ảnh chụp trên demo chỉ mở hộp chứ không bấm Lưu. Lỗi lộ ra ở e2e bấm thật trên máy ảo (`scripts/e2e-m17-gd.sh`, phần B): hộp không đóng.

Sửa ở cả hai đầu. Server nhận lại `name` nếu nó trùng tên trên đường dẫn (khác thì từ chối `args_invalid`: tên gói không đổi được), nên khách cũ không vỡ. Giao diện thôi gửi `name` khi sửa. Bài học: đổi hình của một thân request là đổi hợp đồng; phải tìm mọi nơi gọi, kể cả giao diện, trước khi coi là xong.

### 5.13 Màn Bảo mật dài quá 2 màn hình

Bản đầu đặt "Thử tấn công" thành thẻ thứ ba, có bảng 11 đường mở sẵn: màn dài 2,8 màn hình, quá ngưỡng cứng 2 màn hình của `make do-giao-dien NGUONG=1`. Gập bảng lại vẫn 2,2 màn hình; đo riêng thì thấy không có thẻ đó màn đã dài 2,0 màn hình. Nên phần thử tấn công thành một dòng mở ra được trong thẻ cô lập, cùng khuôn với hai dòng "Phép kiểm cấp máy", "Phép kiểm của từng site": thêm đúng một dòng cao 44 px. Có ô lộ thì dòng tự mở sẵn, nên chuyện cần thấy không bị giấu.

## 6. Tự kiểm

Câu 4 và câu 9 cần chạy lệnh trên máy ảo. Đáp án ở cuối file.

1. Bạn xoá dòng `OOMPolicy=continue` khỏi `cage-service.tmpl` rồi một request của site xin 400 MB RAM với trần 200 MiB. Điều gì khác đi so với khi có dòng đó, và ảnh hưởng gì tới các request khác của chính site đó?
2. Vì sao `CageSocket.applyEnable` bật socket nhưng **không** khởi động dịch vụ? Nếu nó khởi động dịch vụ thì rủi ro gì?
3. Vì sao thứ tự trong `cageEnableSteps` là "ghi file, bật socket, gỡ pool chung, giết tiến trình ngoài lồng, đổi vhost" mà không đổi vhost trước? Hệ quả đo được của thứ tự đó là gì?
4. (máy ảo) Site A đã ở trong lồng. Gõ lệnh nào để chứng minh tiến trình chính PHP-FPM của lồng không chạy bằng root và không có capability nào? Kết quả đúng trông thế nào?
5. Vì sao `MemoryMax` luôn đi kèm `MemorySwapMax=0` trong `cage-slice.tmpl`?
6. `cage.fence` là phép mềm trong Plan `cage.enable` nhưng là phép cứng khi chạy trong health. Giải thích vì sao khác nhau.
7. Trên máy ảo rảnh, trần CPU 50% chỉ nhận 35,7%. Đó có phải lỗi của thiết kế không? Nếu người review hỏi "trần có chính xác không?", bạn trả lời bằng con số nào?
8. Panel bị `kill -9` giữa lúc `tpan isolation probe` đang chạy. Còn lại những gì, và những cơ chế nào dọn chúng?
9. (máy ảo) Làm sao tự thấy rằng dịch vụ PHP của một site trong lồng và lệnh `wp` chạy thay cho site cùng nhận **một** file `fence.conf`? Gợi ý: có hai lệnh cần chạy.
10. Người review hỏi: "Site trong lồng có tự thoát lồng được không?" Bạn liệt kê những thứ cụ thể nào đã chặn các đường thoát, và nói thẳng điều gì chưa chặn được?
11. Trên màn Bảo mật, ma trận cô lập ghi chữ "lồng" thay cho dấu trạng thái ở hàng của site trong lồng. Vì sao TPAN không chạy phép cô lập của M14 đi từ một site trong lồng, và cái gì chứng minh thay?

## Đáp án

1. Dịch vụ không có `continue` thì khi một tiến trình con bị OOM kill, systemd dừng cả dịch vụ rồi khởi động lại (`Failed with result 'oom-kill'`, `restart counter is at 1`, t3b mục a). Mọi request đang chạy của site đều chết, OPcache mất. Có `continue` thì chỉ tiến trình con đó chết (`NRestarts=0`, t3 mục 1), các request khác của site chạy bình thường. Ghi chú: lần đo đầu (t3 mục 1b) không tái hiện; t3b là lần đo lại.

2. Dịch vụ nên chỉ khởi động khi request đầu tiên tới (socket activation), đó là lúc mã của site thật sự được chạy trong lồng. Nếu bước này khởi động dịch vụ thì: (a) mã PHP của site có thể chạy trong lồng trước khi `CageReap` giết tiến trình ngoài lồng (mục 3.3 bước 4); (b) bước có thể hỏng vì điều site điều khiển được (site giết tiến trình chính, trả mã lạ), mà đây là bước của chính sách admin. Thiết kế muốn không gì do mã của site điều khiển làm bước này hỏng (`applyEnable`, đoạn chú thích cuối mục 3.5).

3. Đổi vhost trước thì request vào socket của lồng trong lúc pool chung còn chạy: sẽ có tiến trình cùng uid ở ngoài slice đúng lúc mã của site bắt đầu chạy trong lồng, mở cửa `/proc/<pid>/root` (mục 5.5). Gỡ pool chung và `CageReap` trước vhost thì khi mã site vào lồng, không còn tiến trình ngoài lồng nào. Hệ quả: một khoảng ngắn giữa "gỡ pool chung" và "đổi vhost" request nhận 502. e2e bước 20 đo được (mục 2.7): vào lồng có 2 request hỏng trên 68, gián đoạn 66 ms trên nginx; 1 request hỏng trên 59, gián đoạn 4 ms trên Apache.

4. Lấy PID của tiến trình chính rồi đọc `status` của nó. Chạy từ Mac:

   ```bash
   scripts/vm.sh root bash -c 'pid=$(systemctl show -p MainPID --value tpan-cage-<slug>-php.service); grep -E "^(Uid|CapEff|NoNewPrivs)" /proc/$pid/status'
   ```

   Kết quả đúng (theo bản thử t1, mục 2.2): `Uid` là uid của user `tp_<slug>` (không phải 0), `CapEff: 0000000000000000`, `NoNewPrivs: 1`. Nếu dịch vụ đang ngủ thì `MainPID=0`: gọi một request tới site trước.

5. Nếu chỉ có `MemoryMax`, khi site vượt trần nhân có thể đẩy bộ nhớ ra swap để cho site chạy tiếp thay vì giết tiến trình, làm chậm cả máy chứ không chỉ site đó. `MemorySwapMax=0` cấm swap cho site, nên chạm trần là một tiến trình chết ngay, đúng thiết kế "chỉ request đó chết" (chú thích trong `cage-slice.tmpl`).

6. Trong Plan `cage.enable`, lúc bước kiểm chứng chạy, mã của site đã nhận request và có thể lấp số tiến trình của slice (bằng `exec`) làm `systemd-run` của `cage.fence` không khởi động được; nếu phép đó cứng thì site chặn được việc vào lồng của chính nó. Hàng rào đã được thử lúc `CageSocket` bật lồng (`runFenceTest`), trước khi mã của site vào slice. Còn khi chạy trong health (mỗi giờ), không có Plan nào chờ nó, nên Fail của `cage.fence` là một cảnh báo thật và báo lỗi. (`policyHard` chỉ liệt kê chặng web, `cage.socket`, `cage.limits`, `php.socket`.)

7. Không phải lỗi của thiết kế. Máy bận thì đúng: trần 50% nhận 50,0%, trần 25% nhận 25,0%, trần 100% nhận 99,5% (t3b mục c). Máy rảnh thì ít hơn (35,7% cho trần 50%) vì vCPU đang ngủ của máy ảo thức dậy trễ khoảng 10 đến 14 ms ở đầu mỗi chu kỳ 100 ms. Không lần nào vượt trần. Khi trả lời: nêu cả hai con số, nói e2e đo lúc máy bận. e2e bước 20 (mục 2.7): máy bận, cả hai lần đo A nhận 45% đến 52% với trần 50%; máy rảnh 46,0% (nginx), 35,2% (Apache).

8. Dòng `running` còn trong bảng `probe_runs`, cùng hai site thử đã tạo dở (user Linux, thư mục site, DB, vhost, và có thể file lồng). Dọn bằng: `tpan isolation probe --cleanup`; lượt thử kế tiếp tự dọn lượt sót trước; bộ hẹn giờ của `tpan serve` dọn khi khoá toàn cục đang trống; health báo lỗi `probe.leftover` (sửa tự động `probe.cleanup`) khi dòng đã quá 2 giờ. Plan dọn chỉ nhận tên khớp mẫu `tpan-probe-[ab]-<12 hex>.test` **và** có trong sổ, nên không đụng được site thật (`TestProbeCleanupTuChoiTenLa`).

9. Hai lệnh, chạy trên máy ảo bằng `scripts/vm.sh root ...`. Lệnh một: `systemctl cat tpan-cage-<slug>-php.service`, in file unit kèm mọi drop-in áp cho nó; cuối output có `fence.conf`. Lệnh hai: tạo một lệnh tạm sống 60 giây rồi hỏi unit của nó, `systemd-run --unit=tpan-cage-<slug>-run-thu -p User=tp_<slug> sleep 60` và ngay sau đó `systemctl cat tpan-cage-<slug>-run-thu.service`; output cũng có cùng `fence.conf`, vì tên unit bắt đầu bằng `tpan-cage-<slug>-`. Bản thử t1 (mục 4) đã kiểm điều này: `DropInPaths` của unit tạm là file hàng rào. Mình chưa chạy lại hai lệnh trên trong phiên này.

10. Chặn: không capability (`CapEff: 0`), `NoNewPrivileges` và `RestrictSUIDSGID` (không `sudo`, `su`, `crontab`), `RestrictNamespaces` (không tạo namespace mới), `/sys/fs/cgroup` và `/run` (socket của systemd) không thấy được, `SystemCallFilter=~@privileged @resources`, và không có tiến trình cùng uid ngoài lồng (`CageReap`, `Isolated` không sinh từ site trong lồng). Chưa chặn được (nói thẳng): mạng dùng chung (đọc `/proc/self/net/tcp`), và với Apache chế độ `.htaccess` `day-du` thì site đặt được `SetHandler` sang socket của lồng khác. Danh sách từng lỗ đã tìm và đã sửa nằm trong bảng "Soát cuối, pha 1" của `docs/thay-doi/M17.md` (N1 tới N5, V1 tới V5, L1 tới L10) và mục "Phát hiện của đợt chạy thật trên máy ảo (đợt 4)". Ba lỗ nặng cần nhớ tên: N2 (tiến trình nhảy pid sống qua lượt giết), N3 (crontab riêng của user chạy mã ngoài lồng), N4 (Plan dựng trước ghi lại pool chung, user tự ra khỏi lồng). Pha 2 (thử trên máy ảo) nằm ở mục "Soát cuối, pha 2" của cùng file.

11. Phép cô lập của M14 chạy lệnh `test -r`, `test -x`... dưới user của site "từ" ở NGOÀI lồng (bằng `runuser` của root). Với site trong lồng, một tiến trình cùng uid chạy ngoài lồng là cửa cho mã trong lồng mở `/proc/<pid>/root` ra gốc thật của máy (mục 5.5), nên `core.siteChecks` không sinh phép đó cho site trong lồng (mục 3.3 và chú thích của hàm). Thay vào đó phép `cage.fence` chạy bảy lệnh thử bằng chính lớp bọc của lồng (`cage.Wrap`) và chứng minh site chỉ thấy đồ của mình. Chiều ngược lại (site khác đánh vào site trong lồng) vẫn có ô bình thường trong ma trận. Bấm một ô "lồng" thì giao diện nói đúng điều này và dẫn tới tab Phép kiểm của site đó.
