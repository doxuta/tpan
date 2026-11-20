# M17: lồng theo site, trần tài nguyên theo gói, thử tấn công (bản thiết kế)

Bản thiết kế của đợt 0, viết ngày 21/11/2026 trước khi có dòng mã nào của mốc. Phiên chính của M17 viết; mọi con số ở đây đều có lệnh và output thật ở `docs/bang-chung/M17-nghien-cuu/` (script `t0` đến `t9b`, chạy trên máy ảo `tpan-m17`: Ubuntu 24.04.5, kernel 6.8.0-147, systemd 255.4, PHP 8.3.6, nginx 1.24.0, Apache 2.4.58, MariaDB 10.11.14). Khi mốc xong, file này được gộp vào mục 6.15 của `docs/KE-HOACH.md`.

Từ dùng trong file: **lồng** là toàn bộ những thứ bao quanh PHP của một site; **hàng rào** là phần "nhìn thấy gì" (namespace); **trần** là phần "dùng được bao nhiêu" (cgroup); **ngoài lồng** là cách chạy của v0.2.0 (mọi site chung một dịch vụ PHP-FPM cho mỗi bản PHP).

## 1. Vấn đề và phạm vi

M14 đã bịt chín đường tấn công giữa các site bằng cấu hình. Còn ba việc cấu hình không giải được:

1. Mã PHP gọi lệnh shell vẫn thấy cả cây thư mục của máy. Đo ở `t0`: dưới user của site A, `ls /var/www`, `ls /var/log/tpan/sites`, `ls /etc/nginx/sites-enabled`, `ls /etc/php/8.3/fpm/pool.d`, `ls /etc/cron.d`, `ls /run/php`, `getent passwd` đều đọc ra tên của mọi site khác.
2. Một site ngốn hết RAM, CPU hoặc sinh tiến trình vô hạn thì mọi site chết theo: không có trần nào.
3. Người quản trị không có cách tự kiểm rằng cô lập đang hoạt động.

**Làm trong phần lõi này:**

- Lồng theo site bằng systemd: mỗi site trong lồng có một slice (trần), một dịch vụ PHP-FPM riêng chạy bằng chính user của site, một socket do systemd giữ, và một hàng rào dùng chung cho dịch vụ đó lẫn mọi lệnh tpan chạy thay cho site (WP-CLI, cron).
- Trần theo gói: CPU, RAM, số tiến trình, đọc ghi đĩa, số worker PHP. Cờ lồng nằm ở gói, mặc định tắt.
- Số liệu đọc thẳng từ cgroup v2; lấy mẫu 5 phút một lần, giữ 8 ngày, gộp theo giờ giữ 30 ngày; gợi ý trần từ số đo 7 ngày.
- `tpan isolation probe`: dựng hai site thử, cho A đánh B theo từng đường, ghi kết quả kèm bằng chứng, dọn sạch.
- CLI, API, phép kiểm health, và ghép với đổi bản PHP, chuyển web server, tạm khoá, sao lưu và khôi phục, thùng rác, cron.

**Không làm, và vì sao:**

- Giao diện (`web/`): phần F, sau khi M16 vào `dev`.
- Trần theo user (cộng dồn mọi site của một khách): khung điều phối đã chốt "mỗi site một slice"; một khách có 3 site thì dùng tối đa 3 lần trần của gói. Màn "theo user" của phần giao diện chỉ cộng số của các site.
- Số liệu riêng cho site ngoài lồng: PHP của chúng chạy chung một dịch vụ, không có cgroup riêng để đọc. `tpan usage` nói rõ điều này thay vì ước lượng.
- Lồng ngủ khi rảnh (tự dừng dịch vụ của site không có khách để trả lại RAM): đo được là làm được (mục 2.6) nhưng là một chính sách mới, để sau.
- Chặn mạng nội bộ (`IPAddressDeny`), cấm chạy chương trình trong thư mục site (`NoExecPaths`): đã thử ở `t2b`, ghi ở mục 2.3; chưa bật vì đổi hành vi của plugin.
- Gửi mail qua `sendmail` của máy từ trong lồng: không chạy được (cần `/var/spool` và quyền setgid). TPAN không cài máy chủ mail (mục 1 của KE-HOACH), và `wp_mail` vốn đã không gửi được ở cả trong lẫn ngoài lồng trên stack của TPAN (`t2`).

## 2. Nghiên cứu

Nguồn đã tra (21/11/2026): `man systemd.exec`, `systemd.resource-control`, `systemd.socket`, `systemd.unit`, `systemd-run` của systemd 255 trên chính máy ảo; mã nguồn `sapi/fpm/fpm/fpm_sockets.c` và `fpm_process_ctl.c` của PHP (nhớ lại, rồi kiểm bằng bản thử: không dòng nào dưới đây chỉ dựa vào trí nhớ); tài liệu cgroup v2 của kernel (`memory.peak`, `pids.events`, `cpu.max`, `io.max`). Mỗi mục dưới đây ghi script đã chạy.

### 2.1 Ai chạy tiến trình chính của PHP-FPM (`t1`)

Bản thử `t5` của M14 để tiến trình chính chạy bằng root. Cách đó vẫn cần root giữ `CAP_SETUID`, `CAP_SETGID`, `CAP_CHOWN`, `CAP_KILL` ngay trong lồng. Thiết kế này đi xa hơn một bước:

- **Tiến trình chính của PHP-FPM chạy bằng chính user của site** (`User=tp_<slug>` trong unit). Trong lồng không còn tiến trình root nào.
- **Socket do systemd tạo và giữ** (unit `.socket`, `SocketUser=www-data`, `SocketGroup=www-data`, `SocketMode=0660`). FPM nhận socket đã mở qua biến môi trường `FPM_SOCKETS=<đường dẫn>=3`, đúng cơ chế FPM tự dùng khi reload (`USR2`). Nhờ vậy FPM không phải `chown` socket, nên không cần root.

Kết quả đo: `Uid: 999`, `CapEff: 0000000000000000`, `CapBnd: 0000000000000000`, `NoNewPrivs: 1`; journal ghi `using inherited socket fd=3`. `systemd-analyze security`: dịch vụ chung `php8.3-fpm.service` **9.4 UNSAFE**, dịch vụ của lồng **1.3 OK** (với hàng rào ở 2.3).

Ba hệ quả tốt của việc systemd giữ socket, đều đo được:

- Mỗi site có đúng một socket và systemd chỉ chạy đúng một bản của dịch vụ đứng sau nó. Bất biến "không bao giờ hai tiến trình chính cùng giữ một socket" đúng theo cấu trúc, không theo kỷ luật của mã.
- Restart dịch vụ dưới tải không rớt request nào: kết nối mới xếp hàng trên socket trong lúc dịch vụ dừng rồi chạy lại. `t3b`: 5 lượt restart, mỗi lượt 60 request liên tục, 300/300 trả 200; trên Apache (`t8`) 60/60. Điều kiện: `process_control_timeout = 10s` trong cấu hình FPM. Mặc định là 0, khi đó FPM gửi `SIGQUIT` rồi ngay lập tức `SIGTERM` cho tiến trình con và request đang chạy bị cắt (`t1` lần đầu: 1 trong 40 request ra 502).
- Dừng dịch vụ thì socket vẫn nghe; request kế tiếp tự đánh thức dịch vụ (0,30 đến 0,48 giây cho trang chủ WordPress, `t5`). FPM thử xoá file socket khi thoát nhưng không xoá được vì `/run` trong lồng chỉ đọc; đã kiểm file còn nguyên.

Bẫy đã gặp:

- `error_log = /proc/self/fd/2` làm FPM không khởi động (`No such device or address`): stderr của unit là socket của journald. Dùng `error_log = syslog`; log vào journal của đúng unit.
- systemd chỉ cho 5 lần khởi động trong 10 giây. Quá thì dịch vụ **và socket** cùng vào `failed`, file socket bị gỡ, site ra 502 tới khi có người `reset-failed` (`t4`, `t5` lần đầu). Unit đặt `StartLimitIntervalSec=30`, `StartLimitBurst=10`; bước của tpan `reset-failed` trước khi bật; health có phép kiểm và sửa tự động.
- Kiểm cấu hình FPM bằng root thì hỏng (`FPM initialization failed`: chạy bằng root đòi có `user`). Phải chạy `php-fpm<bản> -t` bằng user của site, trong lồng (`t4`).

### 2.2 Một hàng rào cho cả dịch vụ lẫn lệnh chạy thay cho site (`t1`, `t4`, `t9`, `t9b`)

Khung điều phối đòi mọi thứ chạy mã PHP của site phải ở trong cùng hàng rào. systemd nạp drop-in theo **tiền tố tên unit**: file trong `tpan-cage-<slug>-.service.d/` được áp cho mọi unit `.service` có tên bắt đầu bằng `tpan-cage-<slug>-`. Vậy:

- dịch vụ PHP tên `tpan-cage-<slug>-php.service`;
- lệnh chạy thay cho site là unit tạm `tpan-cage-<slug>-run-<ngẫu nhiên>.service` (qua `systemd-run --unit=...`);
- cả hai nhận đúng một file hàng rào. Đã kiểm: unit tạm có `DropInPaths` là file đó, `Slice`, `User`, `ProtectSystem=strict`, và cgroup `/tpan.slice/tpan-<slug>.slice/tpan-cage-<slug>-run-....service`.

Slug chỉ gồm `a-z0-9_` nên không sinh thêm tiền tố nào. Slug có dấu `-` thì slice bị lồng thành nhiều tầng (`t6`: `tpan-co.slice/tpan-co-gach.slice/...`): mã phải kiểm slug đúng dạng trước khi ghép tên.

`systemd-run` không tự giữ nghĩa "argv, không shell". Đo ở `t4`, `t9`, `t9b`:

| Việc | Mặc định | Cách dùng đã chốt |
|---|---|---|
| Tham số có `$HOME`, `${PATH}` | bị thay bằng giá trị biến; tham số `$(id)` **biến mất** | `--expand-environment=no`: 14 tham số khó (rỗng, xuống dòng, nháy, `%`, `$`) giữ nguyên từng byte |
| Tham số có `%` (`/%postname%/`) | giữ nguyên | không cần làm gì |
| Mã thoát, stdin, stdout tách stderr | đúng (`--wait --pipe`) | giữ |
| Lệnh chết vì `SIGTERM` | **được coi là thành công, mã 0** | `--service-type=oneshot`: mã 255, `Result=signal` |
| Lệnh bị giết vì chạm trần RAM, hoặc quá giờ | mã 1, không phân biệt được với lệnh tự thoát mã 1 | không dùng `--collect`; mã khác 0 thì hỏi `systemctl show -p Result`: `exit-code`, `oom-kill`, `timeout`, `signal`; hỏi xong `reset-failed` |
| Tiến trình gọi (tpan) bị giết | lệnh trong lồng **chạy tiếp** | luôn đặt `-p TimeoutStartSec=<hạn>`; ctx bị huỷ thì `systemctl stop` unit |
| Không có chương trình | mã 1, không tạo unit | mã khác 0 mà unit `not-found` là "không chạy được" |

Chi phí mỗi lệnh: `true` mất 0,03 giây qua `systemd-run` (0,002 giây qua `runuser`); một lệnh `wp` mất 0,23 đến 0,36 giây trong lồng so với 0,14 đến 0,17 giây ngoài lồng (`t2`).

Nếu file hàng rào biến mất đúng lúc một lệnh được gọi, unit tạm sẽ chạy **bằng root không có rào**. Vì vậy `User=` và `Group=` luôn có mặt ngay trên dòng lệnh `systemd-run` và ngay trong file unit của dịch vụ, không chỉ trong drop-in. Mất drop-in thì lệnh chạy bằng user của site như trước M17, không bao giờ bằng root.

### 2.3 Hàng rào: từng thuộc tính (`t1`, `t2`, `t2b`, `t9`)

Phép thử "WordPress còn chạy không" (`t2`) chạy cùng một script ở A (trong lồng, đủ hàng rào) và B (ngoài lồng) rồi so từng dòng: nạp trang chủ qua DB, phân giải tên, HTTPS ra ngoài bằng WP HTTP API, tải và giải nén một plugin, xoá plugin, upload ảnh và sinh 4 cỡ thumbnail, session, `preg` có JIT, `disk_free_space`; và 16 lệnh WP-CLI trong lồng (`core verify-checksums`, `config set/get/delete`, `db query`, `db export`, `cron event run`, `plugin install --activate`, `plugin delete`, `rewrite structure`, mật khẩu qua stdin với `--prompt`, giải nén tarball WordPress). **Mọi dòng của A giống B**, kể cả hai dòng không đạt ở cả hai (`wp_mail` vì máy không có `sendmail`; gọi lại chính mình vì tên miền thử không có DNS).

Hàng rào đã chốt (file `fence.conf`, mục 3.3). Mọi thuộc tính dưới đây bật cùng lúc trong phép thử trên:

| Nhóm | Thuộc tính | Để làm gì |
|---|---|---|
| Danh tính | `User`, `Group`, `Slice` | chạy bằng user của site, trong slice của site |
| Đặc quyền | `NoNewPrivileges=yes`, `CapabilityBoundingSet=` (rỗng), `AmbientCapabilities=` (rỗng), `RestrictSUIDSGID=yes` | không lấy thêm được quyền qua `sudo`, `su`, `crontab`, file setuid |
| Cây thư mục | `ProtectSystem=strict`, `ProtectHome=yes`, `PrivateTmp=yes`, `PrivateDevices=yes`, `TemporaryFileSystem=/var:ro` rồi gắn lại đúng `/var/www/<domain>` (ghi được), `/var/log/tpan/sites/<domain>`, `/var/cache/tpan` (chỉ đọc), `/var/run/mysqld` | cả máy chỉ đọc; dưới `/var` chỉ thấy đồ của mình |
| | `TemporaryFileSystem=/run:ro` rồi gắn lại `/run/mysqld`, `/run/systemd/resolve`, `/run/systemd/notify`, `/run/systemd/journal/dev-log` | không thấy socket và tên unit của site khác; vẫn nối DB, phân giải tên, báo `READY` cho systemd, ghi log |
| | `TemporaryFileSystem=/dev/shm:...` | `/dev/shm` là chỗ ghi chung thứ hai sau `/tmp`; `PrivateDevices` không che nó |
| | `TemporaryFileSystem=/etc/tpan:ro` rồi gắn lại `/etc/tpan/cage/<slug>`; `/etc/passwd` và `/etc/group` thay bằng bản riêng 4 dòng (root, www-data, user của site, nobody) | không đọc ra user `tp_*` của site khác |
| | `InaccessiblePaths=` cho `/etc/nginx`, `/etc/apache2`, `/etc/cron.d`, `/etc/letsencrypt`, `/etc/systemd`, `/etc/php/<mọi bản>/fpm/pool.d`, `/etc/passwd-`, `/etc/group-`, `/etc/shadow*`, `/etc/gshadow*`, `/sys/fs/cgroup`, `/boot`, `/mnt`, `/media`, `/srv` | các chỗ còn lại đọc ra tên site khác, và vài thư mục PHP không có việc gì ở đó |
| Tiến trình | `ProtectProc=invisible`, `ProcSubset=pid`, `PrivateIPC=yes` | chỉ thấy tiến trình của mình; không thấy `/proc/meminfo`, `/proc/1/...` |
| Nhân | `ProtectKernelTunables`, `ProtectKernelModules`, `ProtectKernelLogs`, `ProtectControlGroups`, `ProtectClock`, `ProtectHostname`, `RestrictNamespaces`, `RestrictRealtime`, `LockPersonality` (đều `yes`), `SystemCallArchitectures=native` | thu hẹp bề mặt nhân |
| Syscall | `SystemCallFilter=@system-service`, `SystemCallFilter=~@privileged @resources`, `SystemCallErrorNumber=EPERM` | lệnh bị cấm trả lỗi `EPERM` (như khi user thường gọi) thay vì giết tiến trình |
| Mạng | `RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6` | bỏ netlink, packet, bluetooth... |

Đã thử và **không** đưa vào (`t2b`):

| Thuộc tính | Kết quả | Vì sao không bật |
|---|---|---|
| `PrivateUsers=yes` | WordPress chạy; điểm 1.3 | máy đặt `user.max_user_namespaces=0` thì dịch vụ không khởi động (502, mã 217); trong lồng nhóm `www-data` hiện thành `65534`. Lợi ích thêm nhỏ khi đã không có capability và có `NoNewPrivileges` |
| `MemoryDenyWriteExecute=yes` | WordPress chạy | kẻ tấn công đã chạy được PHP thì không cần bộ nhớ ghi-rồi-chạy; rủi ro với JIT và FFI của plugin |
| `NoExecPaths=<thư mục site>` | WordPress chạy | chặn được file nhị phân tự mang tới, nhưng vỡ plugin có kèm file chạy; là chính sách, để sau thành lựa chọn của gói |
| `IPAddressDeny=localhost` | **hỏng**: không phân giải được tên (resolver ở `127.0.0.53`) | cần danh sách cho phép đi kèm; để sau |
| `PrivateNetwork=yes` | **hỏng**: không ra được mạng | WordPress cần mạng |

Phép tự kiểm hàng rào chỉ bằng lệnh argv (không shell), chạy trong lồng, đo ở `t9`: `id -un` ra user của site; `ls -A /var/www` ra đúng một tên; `ls -A /tmp` rỗng; `ls -A /run` ra `mysqld systemd`; `find /proc -maxdepth 1 -type d -regex '/proc/[0-9]+' ! -user <user>` không ra dòng nào; `getent passwd` ra 4 dòng; `test -w /etc` mã 1. Đây là nội dung của phép kiểm `cage.fence`.

Danh sách `pool.d` phải kê mọi bản PHP: `t6` cho thấy khi chỉ che bản đang dùng thì site vẫn đọc được tên pool ở thư mục của bản khác. Đường dẫn có dấu `-` ở đầu nên bản chưa cài không gây lỗi; bản cài sau được che từ lần khởi động kế tiếp của dịch vụ. Danh sách cố định: 5.6, 7.0 đến 7.4, 8.0 đến 8.9, 9.0 đến 9.9.

### 2.4 Trần (`t3`, `t3b`)

Trần đặt ở slice của site, nên tính chung cho dịch vụ PHP và mọi lệnh chạy thay cho site.

| Trần | Đo được |
|---|---|
| RAM cứng (`MemoryMax=200M`, `MemorySwapMax=0`, dịch vụ có `OOMPolicy=continue`) | Script cấp phát 400 MB: tiến trình con PHP đó bị giết sau 0,06 giây, request đó ra **502** (`recv() failed (104: Connection reset by peer)`); request kế tiếp của A trả 200 ngay (0 lần hỏng, 0,04 giây); B 60/60 trả 200 trong lúc đó. Dịch vụ không khởi động lại (`NRestarts=0`). `memory.events`: `oom_kill 1`; `memory.peak` = 209715200 |
| Cùng phép thử, bỏ `OOMPolicy=continue` | systemd dừng cả dịch vụ (`Failed with result 'oom-kill'`) rồi chạy lại (`NRestarts=1`): mọi request đang chạy của site đều chết, OPcache mất. Vì vậy unit ghi rõ `continue` |
| RAM mềm (`MemoryHigh=200M`, không có `MemoryMax`) | Script cấp phát không bị giết mà bị bóp: sau 40 giây chưa xong; **trang chủ của A mất 27,8 giây**. Trần mềm làm cả site treo thay vì cho một request chết nhanh. **Không dùng `MemoryHigh`** |
| Lệnh trong lồng vượt trần | Lệnh bị giết (`Result=oom-kill`), dịch vụ PHP của site không bị ảnh hưởng |
| CPU (`CPUQuota`) | Máy bận (mỗi nhân có một vòng lặp ưu tiên thấp nhất ở ngoài lồng): trần 50% nhận 50,0%, trần 25% nhận 25,0%, trần 100% nhận 99,5%. Máy rảnh: trần 50% nhận 35,7%, trần 25% nhận 15,7%. Không lần nào vượt trần. Lý do của phần thiếu khi máy rảnh: vCPU đang ngủ của máy ảo Multipass thức dậy trễ khoảng 10 đến 14 ms ở đầu mỗi chu kỳ 100 ms (số chu kỳ vẫn đúng 100 ms). B không chậm đi khi A bị bóp (7,76 so với 7,60 giây) |
| Số tiến trình (`TasksMax=30`) | Vòng lặp sinh 200 tiến trình: dừng ở 29, `sh` báo không fork được (mã 2). `pids.peak` = 30. Trên kernel 6.8 bộ đếm "chạm trần" nằm ở cgroup **nơi gọi fork** (`.../tpan-cage-<slug>-php.service/pids.events: max 1`), ở slice vẫn là 0 |
| Đĩa (`IOReadBandwidthMax`, `IOWriteBandwidthMax` = `/var/www 4M`) | **Có hiệu lực** trên đĩa của máy ảo (`sda`, ext4): ghi trực tiếp 32 MB mất 10,0 giây (3,3 MB/s), ghi có đệm rồi `fsync` 8,9 giây, đọc sau khi xoá cache 10,9 giây; ngoài lồng cùng lệnh ghi mất 0,008 giây. `io.max`: `8:0 rbps=4000000 wbps=4000000`. Giữ hai trường này |
| Đổi trần khi site đang chạy | Ghi lại file slice rồi `daemon-reload` (0,12 giây): `memory.max`, `cpu.max`, `pids.max` đổi ngay, `MainPID` giữ nguyên, site vẫn 200. **Không cần khởi động lại** |
| Hạ trần RAM xuống 8M khi đang dùng 58 MB | dịch vụ rơi vào vòng chết rồi chạy lại, site 502. Vì vậy có trần dưới cho từng trường (mục 3.1) và Plan đổi trần kiểm site trả 200 trước khi coi là xong |

`daemon-reload` hai lần liền trong lúc 40 request đang chạy: 40/40 trả 200, `MainPID` và các bộ đếm giữ nguyên (`t7a`).

### 2.5 Khởi động lại máy (`t7a`, `t7b`)

- Socket đã `enable` tự nghe lại sau khi máy lên; dịch vụ chưa chạy cho tới request đầu; request đầu trả 200 và trần được áp đúng.
- Socket đã `disable` (site tạm khoá) nằm yên.
- Slice và cgroup của nó chỉ có từ lúc dịch vụ chạy lần đầu. Mọi bộ đếm cộng dồn (`oom_kill`, `usage_usec`, `memory.peak`) về 0 sau khi máy khởi động lại; dừng rồi chạy lại riêng dịch vụ thì bộ đếm của slice **giữ nguyên**. Bộ lấy mẫu phải coi "số giảm đi" là bộ đếm vừa về 0.

### 2.6 Chi phí với 10 site (`t5`)

| Thứ đo | Kết quả |
|---|---|
| RAM, 10 lồng đang thức, chỉ còn tiến trình chính | 341 và 397 MB (hai lần đo) so với 10 site ở PHP-FPM chung: **34 đến 39 MB mỗi lồng**. Trong đó OPcache riêng của site là 24,5 MB (`shmem` của slice); tiến trình chính: RSS 31,6 MB, PSS 10,8 MB |
| RAM, 10 lồng đang ngủ (dịch vụ dừng, socket nghe) | bằng mức của PHP-FPM chung (chênh −10 và +34 MB giữa hai lần đo, tức trong sai số) |
| Khởi động dịch vụ của một lồng | 0,05 đến 0,10 giây |
| Request đầu khi lồng đang ngủ | 0,30 đến 0,48 giây; request kế 0,025 giây. Để so: ngoài lồng, request đầu sau khi tiến trình con `ondemand` đã thoát mất 0,07 đến 0,10 giây |
| Độ trễ thêm mỗi request (300 lượt xen kẽ) | trang chủ WordPress: trong lồng trung vị 25,3 ms, p95 28,8 ms; ngoài lồng 24,7 và 28,0 ms. File PHP một dòng: 0,1 ms ở cả hai. **Không đo được độ trễ thêm** |

Ghi chú về OPcache: ở ngoài lồng mọi site chen nhau trong một vùng 128 MB (mặc định của PHP), 10 site WordPress cần khoảng 245 MB nên có site bị đẩy khỏi cache. Trong lồng mỗi site có vùng riêng: đó là phần lớn của 34 MB, và cũng là lý do site này không làm chậm được site kia qua cache.

### 2.7 Các bản PHP, Apache, và phần của M14 (`t6`, `t8`)

- PHP 7.4.33, 8.1.34, 8.2.36, 8.3.6, 8.4.28, 8.5.13: FPM đều nhận socket qua `FPM_SOCKETS`, WordPress 200, hàng rào còn nguyên, reload (`USR2`) chạy.
- Apache (`mod_proxy_fcgi`, MPM event): `SetHandler "proxy:unix:<socket của lồng>|fcgi://localhost"` nối được; permalink 200, trang không có 404, restart lồng dưới tải 60/60, ngủ rồi thức 0,39 giây.
- File ini OPcache của M14 (`99-tpan.ini`) nằm ở `conf.d` của bản PHP nên lồng cũng đọc: `validate_permission=1`, API bị khoá. Trong lồng hai dòng đó là lớp thứ hai: OPcache của lồng chỉ chứa file của chính site, và file của site khác không còn nhìn thấy, nên L1 và L2 không còn đường đi.
- Drop-in `ProtectProc` của M14 gắn vào `php<bản>-fpm.service`, không đụng tới lồng; lồng có `ProtectProc` riêng trong hàng rào.
- `wp` trong lồng chạy bằng PHP CLI mặc định của máy (như ngoài lồng), không theo bản PHP của site. Không đổi ở mốc này.

## 3. Thiết kế

### 3.1 Dữ liệu

Migration `1700_cage.sql` (mọi cột mới có DEFAULT: nâng từ v0.2.0 không đổi dòng nào):

```sql
ALTER TABLE packages ADD COLUMN cage        INTEGER NOT NULL DEFAULT 0 CHECK (cage IN (0, 1));
ALTER TABLE packages ADD COLUMN cpu_percent INTEGER NOT NULL DEFAULT 0 CHECK (cpu_percent >= 0);
ALTER TABLE packages ADD COLUMN ram_mb      INTEGER NOT NULL DEFAULT 0 CHECK (ram_mb >= 0);
ALTER TABLE packages ADD COLUMN tasks       INTEGER NOT NULL DEFAULT 0 CHECK (tasks >= 0);
ALTER TABLE packages ADD COLUMN io_read_mb  INTEGER NOT NULL DEFAULT 0 CHECK (io_read_mb >= 0);
ALTER TABLE packages ADD COLUMN io_write_mb INTEGER NOT NULL DEFAULT 0 CHECK (io_write_mb >= 0);
ALTER TABLE packages ADD COLUMN php_workers INTEGER NOT NULL DEFAULT 0 CHECK (php_workers >= 0);
ALTER TABLE sites    ADD COLUMN caged       INTEGER NOT NULL DEFAULT 0 CHECK (caged IN (0, 1));
```

cùng ba bảng mới `usage_samples`, `usage_hourly`, `probe_runs` (cột ở 3.8 và 3.9).

Kiểu Go (`internal/account`): `Package` thêm `Cage bool`, `CPUPercent`, `RAMMB`, `Tasks`, `IOReadMB`, `IOWriteMB`, `PHPWorkers int`; kiểu mới `Limits` (sáu số trên) với `Package.Limits()`. `site.Site` thêm `Caged bool`.

Nghĩa và giới hạn của từng trường (kiểm ở `core`, mã từ chối `package_limit_invalid`):

| Trường | Đơn vị | 0 nghĩa là | Giá trị nhận | Thành thuộc tính |
|---|---|---|---|---|
| `cpu_percent` | % của một nhân | không giới hạn | 0, hoặc 5 đến 6400 | `CPUQuota=<n>%` |
| `ram_mb` | MiB | không giới hạn | 0, hoặc 64 đến 1048576 | `MemoryMax=<n>M` và `MemorySwapMax=0` |
| `tasks` | tiến trình và luồng | không giới hạn | 0, hoặc 10 đến 100000, và ≥ số worker + 5 | `TasksMax=<n>` |
| `io_read_mb`, `io_write_mb` | MB/giây (10⁶ byte) | không giới hạn | 0, hoặc 1 đến 100000 | `IOReadBandwidthMax=/var/www <n>M`, `IOWriteBandwidthMax=...` |
| `php_workers` | tiến trình con PHP cùng lúc | **mặc định 5** (PHP-FPM bắt buộc có một con số; đây là số của pool ngoài lồng) | 0, hoặc 1 đến 200 | `pm.max_children` |

Trần chỉ được áp bằng cgroup của lồng. Gói có một trần khác 0 mà `cage` tắt thì bị từ chối (`package_limits_need_cage`): không lưu một con số không có hiệu lực. Trần dưới của RAM (64) chỉ để chặn gõ nhầm làm site chết vòng; con số hợp lý cho WordPress đến từ phần gợi ý (3.8).

**Trạng thái của một site về lồng** có hai nguồn, cố ý tách:

- **Muốn**: cờ `cage` của gói của chủ site. Đổi cờ hay đổi gói của user không tự chạy gì.
- **Đang là**: `sites.caged`, chỉ đổi khi Plan `cage.enable` hoặc `cage.disable` thành công.

Hai bên lệch thì health cảnh báo (`cage.state`) kèm sửa tự động. Lý do: mọi thay đổi trên site đang chạy phải là một Plan có xem trước (luật 2 của lộ trình).

### 3.2 Tên và đường dẫn

Thêm vào `site.Layout` (hàm, không thêm trường; mọi tên suy từ slug như mục 5 của KE-HOACH):

| Hàm | Giá trị |
|---|---|
| `CageSlice()` | `tpan-<slug>.slice` (cgroup `/sys/fs/cgroup/tpan.slice/tpan-<slug>.slice`; `tpan.slice` là slice cha ngầm, không có file) |
| `CageSocketUnit()`, `CageServiceUnit()` | `tpan-cage-<slug>-php.socket`, `tpan-cage-<slug>-php.service` |
| `CageRunPrefix()` | `tpan-cage-<slug>-run-` |
| `CageSocket()` | `/run/tpan-cage/<slug>/php.sock` |
| `CageConfDir()` | `/etc/tpan/cage/<slug>` chứa `php-fpm.conf`, `passwd`, `group` |
| `CageFenceDir()` | `/etc/systemd/system/tpan-cage-<slug>-.service.d` chứa `fence.conf` |
| `CageUnitFiles()` | ba file ở `/etc/systemd/system/`: slice, socket, service |

`site.CageSlugOfUser(user)` đổi `tp_<slug>` về slug (chỉ nhận `^tp_[a-z0-9_]{1,40}$`). `site.Site.Socket()` trả `CageSocket()` khi `Caged`. Tên dài nhất: `tpan-cage-<23 ký tự>-run-<16 hex>.service` là 62 ký tự (systemd nhận tới 255).

### 3.3 File của lồng

Bảy file, sinh từ template trong `internal/templates` (golden test), dòng đầu là dòng "Do tpan quản lý" theo đúng dấu chú thích của loại file.

**Slice** (`tpan-<slug>.slice`): `[Slice]` với `MemoryAccounting`, `TasksAccounting`, `CPUAccounting`, `IOAccounting` (đều `yes`) và các dòng trần của bảng 3.1; trần bằng 0 thì không có dòng đó.

**Socket** (`tpan-cage-<slug>-php.socket`):

```ini
[Socket]
ListenStream=/run/tpan-cage/<slug>/php.sock
SocketUser=www-data
SocketGroup=www-data
SocketMode=0660
DirectoryMode=0755
RemoveOnStop=yes
[Install]
WantedBy=sockets.target
```

**Dịch vụ** (`tpan-cage-<slug>-php.service`):

```ini
[Unit]
StartLimitIntervalSec=30
StartLimitBurst=10
[Service]
Type=notify
User=tp_<slug>
Group=tp_<slug>
NoNewPrivileges=yes
Environment=FPM_SOCKETS=/run/tpan-cage/<slug>/php.sock=3
ExecStart=/usr/sbin/php-fpm<bản> --nodaemonize --fpm-config /etc/tpan/cage/<slug>/php-fpm.conf
ExecReload=/bin/kill -USR2 $MAINPID
KillSignal=SIGQUIT
TimeoutStopSec=30
OOMPolicy=continue
Restart=on-failure
```

`User`, `Group`, `NoNewPrivileges` lặp lại ở đây dù hàng rào cũng có (lý do ở 2.2).

**Hàng rào** (`fence.conf`): đúng danh sách ở 2.3, theo thứ tự của `lib.sh` trong thư mục nghiên cứu cộng dòng `SystemCallFilter=~@privileged @resources` và danh sách `pool.d` của mọi bản PHP.

**Cấu hình FPM** (`php-fpm.conf`): `[global]` có `error_log = syslog`, `syslog.ident = php-fpm-<slug>`, `daemonize = no`, `process_control_timeout = 10s`; pool `[tpan-<slug>]` có `listen = <socket của lồng>`, `pm = ondemand`, `pm.max_children = <worker>`, `pm.process_idle_timeout = 10s`, `pm.max_requests = 500`, và đúng các dòng `php_admin_*` của pool ngoài lồng (`open_basedir`, ba đường dẫn tạm, hai giới hạn upload, `log_errors`, `error_log`). Không có `user`, `group`, `listen.owner`, `listen.group`, `listen.mode`, `pid`: FPM không chạy bằng root nên các dòng đó không có tác dụng.

**`passwd`, `group`**: mỗi file 4 dòng lấy từ `getent` lúc Step chạy (root, www-data, user của site, nobody; nhóm tương ứng với `nogroup`).

### 3.4 Lệnh chạy thay cho site: `internal/cage`

Gói mới, hai việc.

**`cage.Wrap(sys system.System) system.System`.** Bọc `System`; `cmd/tpan/main.go` bọc `system.NewLocal()` một lần, nên CLI, `tpan serve`, cron và MCP đều đi qua. Với mỗi `Run(cmd)`:

1. `cmd.AsUser` rỗng, hoặc `cmd.Bare` (trường mới của `system.Cmd`), hoặc `AsUser` không phải dạng `tp_<slug>`: chuyển thẳng xuống dưới (chạy bằng `runuser` như trước).
2. `Stat` file hàng rào của slug đó. Không có: chuyển thẳng xuống dưới. Lỗi khác: trả lỗi, **không chạy gì**.
3. Có: chạy

   ```
   systemd-run --quiet --wait --pipe --expand-environment=no --service-type=oneshot
     --unit=tpan-cage-<slug>-run-<16 hex> -p User=<user> -p Group=<user> -p NoNewPrivileges=yes
     -p TimeoutStartSec=<giây> -E LC_ALL=C [--working-directory=<Dir>] -- <Name> <Args...>
   ```

   `Stdin` và `OnOutput` truyền nguyên. `TimeoutStartSec` là thời gian còn lại của ctx cộng 1 giây; ctx không có hạn thì 3600.
4. Mã thoát khác 0: hỏi `systemctl show <unit> -p LoadState -p Result -p ExecMainStatus`, rồi `systemctl reset-failed <unit>`. `Result=exit-code` trả `*system.ExitError` với mã thật. `oom-kill`, `timeout`, `signal` trả lỗi **không phải** `ExitError` (bọc `cage.ErrKilled`), để nơi gọi không nhầm "bị giết vì chạm trần" với một mã thoát có nghĩa (`wp core is-installed` trả 1 khi chưa cài). Unit `not-found` là không chạy được lệnh.
5. ctx bị huỷ giữa chừng: `systemctl stop <unit>` bằng một ctx mới 10 giây.

"Site có lồng hay không" đọc từ **máy** (file hàng rào), không từ bảng `sites`: giữa một Plan vào lồng, bảng chưa đổi mà dịch vụ đã chạy trong lồng, và bước kiểm chứng của chính Plan đó phải chạy `wp` trong lồng thì mới chứng minh được gì.

`Bare` chỉ dùng ở `checks.Isolated`: phép đó thử lớp quyền Linux bên dưới (user A có đọc được file của B không). Chạy nó trong lồng thì A không còn thấy B, phép thử luôn "bị từ chối" và không còn kiểm được lớp quyền. Test tĩnh: ngoài `internal/checks/site.go` không file nào đặt `Bare: true`, và không lệnh `Bare` nào có `Name` là `wp` hay `php`.

Bằng chứng phải là lệnh thật: `system.Describe(sys, cmd)` (giao diện tuỳ chọn `system.Describer`) trả dòng `systemd-run ...` với đuôi unit `xem` khi lệnh sẽ chạy trong lồng. `engine` (sự kiện `command`) và `checks.execCmd` (bằng chứng) dùng hàm này thay cho `cmd.String()`.

**`cage.ReadUsage(sys, layout) (Usage, error)`.** Đọc các file cgroup của slice (bảng ở 3.8). Thư mục cgroup không có: `Usage{Running: false}`, không lỗi.

### 3.5 Step mới (`internal/steps/cage.go`)

**`CageFiles{Layout, PHP, Limits, Remove}`**

- `Remove=false`: `Check` đúng khi cả bảy file có đúng nội dung, quyền `0644 root:root` (thư mục `0755`). `Apply`: giữ bản cũ, ghi cả bảy bằng `WriteFileAtomic`, `systemctl daemon-reload`; nếu dịch vụ đang chạy và một trong ba file (dịch vụ, hàng rào, cấu hình FPM) đổi thì `systemctl try-restart <dịch vụ>`. Chỉ file slice đổi thì không restart. Lỗi ở giữa: trả mọi file về rồi `daemon-reload`. `Undo`: trả bản cũ (file mới tạo thì xoá, thư mục mới tạo thì gỡ), `daemon-reload`, `try-restart` theo cùng luật.
- `Remove=true`: `Check` đúng khi không còn file hay thư mục nào của lồng. `Apply` gỡ hết rồi `daemon-reload`; gỡ `/etc/tpan/cage` và `/etc/tpan` nếu rỗng. `Undo` trả lại.
- `Info().Files` kê cả bảy file kèm nội dung (trừ `passwd`, `group` khi user chưa có lúc dựng Plan), để bản xem trước có diff và engine ghi sha256 vào `managed_files`.

**`CageSocket{Layout, PHP, Disable}`**

- Bật: `Check` đúng khi socket `enabled`, `active`, file socket có và là socket. `Apply`: `php-fpm<bản> -t --fpm-config <conf>` với `AsUser` (đi qua `cage.Wrap` nên chạy trong lồng, kiểm luôn hàng rào); `systemctl reset-failed` hai unit (bỏ qua lỗi "không nạp"); `systemctl enable --now <socket>`; `systemctl start <dịch vụ>`; chờ tối đa 20 giây tới khi file socket có và dịch vụ `active`. Hỏng thì lỗi kèm 20 dòng cuối `journalctl -u <dịch vụ>`. `Undo`: `disable --now` socket, `stop` dịch vụ.
- Tắt (`Disable=true`): `disable --now` socket, `stop` dịch vụ (chờ request đang chạy, tối đa 30 giây), `stop` slice. `Undo`: bật lại như trên, không kiểm cấu hình.

**Vhost**: `templates` và `steps` thêm bản "trong lồng" của bốn hàm vhost (`NginxSiteCaged`, `ApacheSiteCaged`, hai bản SSL; `steps.SiteVhostCaged`, `SiteVhostSSLCaged`): giống hệt bản thường, chỉ khác socket là `CageSocket()`. Chữ ký cũ không đổi, golden của vhost ngoài lồng không đổi một byte.

### 3.6 Plan

Mọi Plan dưới đây dựng ở `internal/core/cage.go` (kiểu `Cages`), trả `*Planned` như các ca sử dụng từ M12. Một hàm duy nhất `sitePHPSteps(st, cfg, limits)` chọn giữa pool chung (`steps.SitePool`) và lồng (`CageFiles` + `CageSocket`), như `siteVhost` là chỗ duy nhất chọn vhost; `siteVhost` thêm nhánh `st.Caged`.

**`cage.enable <domain>`**

Điều kiện đầu (mã từ chối): site có và `active` (`site_not_found`, `site_trashed`); không tạm khoá (`site_suspended`), DB không tạm khoá (`db_suspended`): bước kiểm 200 không qua được; gói của chủ site bật lồng (`cage_not_in_package`, kèm lệnh `tpan package edit <gói> --cage on`); máy có cgroup v2 (`/sys/fs/cgroup/cgroup.controllers`, `cage_cgroup_v1`) và systemd từ 254 (`cage_systemd_old`: cờ `--expand-environment` có từ bản đó); bản PHP của site đang cài. Site đã ở trong lồng thì **không từ chối**: Plan chạy lại, bước đã đúng được bỏ qua (đây cũng là cách áp lại trần và sửa lệch).

Bước:

1. `CageFiles` với trần của gói.
2. `CageSocket` (bật). Từ đây lồng chạy song song với pool chung; vhost còn trỏ pool chung.
3. Vhost bản trong lồng qua `siteVhost` (kiểm cú pháp, reload web server).
4. Kiểm giữa chừng: site trả 200 hai lần liên tiếp (như `php.use`).
5. Gỡ pool chung của site (`Absent` file pool, `php-fpm<bản> -t`, reload `php<bản>-fpm`).

Kiểm chứng: bộ phép kiểm của site bản trong lồng (3.7) gồm `cage.socket`, `cage.fence`, `cage.limits`. Thành công thì ghi `caged=1`. Hoàn tác đi ngược: pool chung về, vhost về, lồng tắt, file gỡ. Không có lúc nào vhost trỏ vào một socket không ai nghe, nên không có gián đoạn theo thiết kế; e2e đo bằng vòng `curl`.

**`cage.disable <domain>`**

Điều kiện đầu: site `active`, đang `caged` (`cage_not_caged`), không tạm khoá. Không đòi cờ của gói: admin được đưa một site ra để gỡ lỗi; health sẽ cảnh báo cho tới khi đưa vào lại.

Bước: `steps.SitePool` (ghi, kiểm, reload, chờ socket) → vhost bản thường → kiểm 200 hai lần → `CageSocket{Disable}` → `CageFiles{Remove}`. Kiểm chứng: bộ phép kiểm thường. Thành công thì `caged=0`. File pool và vhost sinh lại từ template nên giống từng byte bản trước khi vào lồng.

**`cage.sync`** (không có lệnh riêng; dùng khi sửa gói, đổi gói của user, và sửa tự động của `cage.limits`): với mỗi site đang trong lồng trong danh sách: `CageFiles` với trần hiện tại của gói, rồi kiểm site trả 200 một lần (bỏ với site tạm khoá). Kiểm chứng: `cage.limits` của từng site. Trần mới thấp tới mức site không trả 200 thì job hoàn tác về trần cũ.

**Ghép với thao tác đã có** (site đang `caged`):

| Thao tác | Đổi gì |
|---|---|
| Tạo site mới (`site.add`) | Plan tạo site không đổi (site sinh ra ở ngoài lồng). Job thành công và gói của chủ bật lồng thì `SiteAdd.Execute` chạy tiếp một job `cage.enable`; job đó hỏng thì site vẫn dùng được, kết quả ghi lý do và health cảnh báo. Job hàng loạt (`apply`) làm như pha D của SSL: sau lô, mỗi site một job |
| Chạy lại `site add` | `sitePHPSteps` ra bước của lồng, `siteVhost` ra vhost của lồng: không ghi lại pool chung |
| Đổi bản PHP (`php.use`) | Plan riêng: `CageFiles` với bản mới (file dịch vụ đổi `ExecStart`, nên có `try-restart`; socket giữ nguyên, kết nối xếp hàng) → kiểm 200 hai lần. Không đụng vhost, không đụng pool chung |
| Tạm khoá, mở khoá | `suspendSteps`: vhost tạm khoá → `CageSocket{Disable}` → cron. `unsuspendSteps`: `CageSocket` bật → cron → vhost → kiểm 200. File của lồng giữ nguyên; `disable` nên máy khởi động lại site vẫn nằm yên |
| Thùng rác, khôi phục | `trashSteps`: gỡ link vhost → `CageSocket{Disable}` → `CageFiles{Remove}` → như cũ. `restoreSteps`: như cũ nhưng `sitePHPSteps` thay cho pool. `caged` giữ nguyên trong lúc ở thùng rác |
| Xoá hẳn | `purgeSteps` luôn thêm `CageSocket{Disable}`, `CageFiles{Remove}` (site không có lồng thì hai bước tự bỏ qua) trước bước xoá user |
| Chuyển web server, SSL, harden, sửa tự động vhost | đi qua `siteVhost` nên tự ra vhost của lồng |
| Sao lưu, khôi phục, DB | lệnh `wp` tự chạy trong lồng qua `cage.Wrap`; bộ phép kiểm chứng là bản trong lồng |
| Cron | `tpan cron run` gọi `wp` với `AsUser`, tự vào lồng; hạn 4 phút thành `TimeoutStartSec` |
| `self-update --rollback` | từ chối `cage_active` khi còn site `caged` (mục 6) |

### 3.7 Phép kiểm (`internal/checks/cage.go`)

Site trong lồng dùng `SiteChecksCaged` / `SiteChecksSSLCaged`: như bản thường, thay `php.socket` bằng ba phép dưới. `siteStateChecks` chọn bản theo `st.Caged`; site tạm khoá giữ nguyên cách của M13.

| ID | Chặng | Kiểm gì | Không đạt | Sửa tự động |
|---|---|---|---|---|
| `cage.socket:<domain>` | PHP | file socket là socket, `www-data:www-data 0660`; unit socket `active`; dịch vụ không `failed` | lỗi | `cage.start`: `CageSocket` bật |
| `cage.fence:<domain>` | PHP, nặng (mỗi giờ) | bảy lệnh ở 2.3 chạy trong lồng (qua `AsUser`), mỗi lệnh một dòng bằng chứng | lỗi | `cage.sync` |
| `cage.limits:<domain>` | PHP | `memory.max`, `memory.swap.max`, `cpu.max`, `pids.max`, `io.max` của slice bằng trần của gói. Slice chưa có cgroup (lồng chưa chạy từ khi máy lên): đạt, ghi rõ lý do | lỗi | `cage.sync` |
| `cage.state:<domain>` | PHP | (a) gói muốn lồng mà site ở ngoài, hoặc ngược lại: **cảnh báo**; (b) `caged=1` mà thiếu file hàng rào hoặc còn file pool chung; `caged=0` mà còn file hàng rào: **lỗi** | như cột bên | (a) `cage.enable` hoặc `cage.disable`; (b) chạy lại Plan đúng với `caged` |
| `probe.leftover:tpan` | TPAN | còn lượt thử tấn công chưa dọn (3.9) | lỗi | `probe.cleanup` |

`cage.state` chỉ sinh ra cho site có `caged=1` hoặc gói có `cage=1`; site thường của máy nâng từ v0.2.0 không có thêm phép kiểm nào.

`checks.Isolated`: lệnh `test` đặt `Bare: true`; phép thử socket xét thêm `CageSocket()` của site đích.

`tpan route explain`: với site trong lồng, chặng socket mong đợi `CageSocket()`; chặng pool đọc `php-fpm.conf` của lồng; chặng user đọc `User=` trong file dịch vụ.

### 3.8 Số liệu và gợi ý

`cage.ReadUsage` đọc, cho slice của site:

| Số | File | Ghi chú |
|---|---|---|
| RAM hiện tại, đỉnh | `memory.current`, `memory.peak` | đỉnh tính từ khi slice có (kernel 6.8 không đặt lại được) |
| Lần chạm trần RAM | `memory.events`: `oom_kill` (bị giết), `max` (đụng trần) | |
| CPU đã dùng, bị bóp | `cpu.stat`: `usage_usec`, `nr_throttled`, `throttled_usec` | không có `CPUQuota` thì hai số bóp không có: coi là 0 |
| Số tiến trình, đỉnh, lần chạm trần | `pids.current`, `pids.peak`, `pids.events` | có `pids.events.local` (kernel mới): đọc `pids.events` của slice; không có (6.8): cộng `pids.events` của mọi cgroup con |
| Đọc ghi đĩa | `io.stat`: tổng `rbytes`, `wbytes` | |
| Trần đang áp | `memory.max`, `cpu.max`, `pids.max`, `io.max` | cho `cage.limits` và để hiện cạnh số đang dùng |

**Bất biến 6** ("con số hiện ra bằng con số đọc từ cgroup"): `tpan usage` và API đọc cgroup ngay lúc được gọi, không qua bảng mẫu.

**Lấy mẫu.** Một bộ hẹn giờ trong `tpan serve` (như bộ dọn thùng rác): 5 phút một lần ghi một dòng `usage_samples(domain, at, running, mem_current, mem_peak, oom_kills, cpu_usec, throttled_usec, nr_throttled, tasks, tasks_peak, tasks_hits, io_rbytes, io_wbytes)` cho mỗi site `caged` và `active`. Mỗi giờ gộp các mẫu của những giờ đã trọn vào `usage_hourly(domain, hour, samples, mem_avg, mem_max, cpu_usec, cpu_pct_max, throttled_usec, nr_throttled, tasks_max, oom_kills, tasks_hits, io_rbytes, io_wbytes)`, rồi xoá mẫu cũ hơn 8 ngày và dòng giờ cũ hơn 30 ngày. Hiệu của một bộ đếm giữa hai mẫu: `sau − trước`, hoặc `sau` khi `sau < trước` (bộ đếm vừa về 0). Lệnh ẩn `tpan usage --sample` ghi một mẫu ngay, cho e2e.

**Gợi ý trần** (hàm thuần `suggest(samples, limits)`, có bảng test). Dùng mẫu 7 ngày gần nhất của site:

- `n` mẫu; dưới 288 (24 giờ) thì gợi ý vẫn ra nhưng mang cờ `low_data`.
- RAM: `đỉnh = max(mem_current, mem_peak)` trên mọi mẫu. Gợi ý = `đỉnh × 1,25`, làm tròn lên bội của 32, không dưới 128. Nếu trong 7 ngày có `oom_kill` thì số đo đã bị chính trần cắt (nhu cầu thật cao hơn): gợi ý không thấp hơn `trần hiện tại × 1,5`, và ghi rõ lý do.
- CPU: phần trăm của từng khoảng 5 phút = `hiệu cpu_usec / hiệu thời gian`; lấy phân vị 95. Gợi ý = `p95 × 1,5`, làm tròn lên bội của 10, không dưới 25. Nếu thời gian bị bóp vượt 1% của 7 ngày: không thấp hơn `trần hiện tại × 1,5`.
- Số tiến trình: `đỉnh tasks_peak × 2`, làm tròn lên bội của 10, không dưới 20; có lần chạm trần thì không thấp hơn `trần × 1,5`.
- Không gợi ý trần đĩa và số worker (cỡ pool là việc của M21).

Kết quả kèm mọi số đã dùng (`n`, khoảng thời gian, đỉnh, p95, số lần chạm trần, công thức). Gợi ý cho một gói = lớn nhất trong các site của gói. Chỉ là gợi ý: áp bằng `tpan package edit` (một Plan `cage.sync` có xem trước).

### 3.9 Thử tấn công

`tpan isolation probe [--only ngoai|trong] [--dry-run] [--yes] [--json]`, `tpan isolation probe --cleanup`.

Một job `isolation.probe`. Tên hai site thử: `tpan-probe-a-<12 hex>.test` và `tpan-probe-b-<12 hex>.test` (cùng đuôi ngẫu nhiên từ `crypto/rand`; `.test` là tên miền dành riêng). `Sites.PrepareAdd` từ chối mọi domain khớp mẫu này (`domain_reserved`).

Thứ tự:

1. **Ghi sổ trước**: một dòng `probe_runs(id, started_at, state='running', domain_a, domain_b, mode)` trước khi tạo bất cứ thứ gì.
2. Bước tạo A rồi B: đúng các bước của `site.add` (WordPress thật). Hai site không có dòng trong bảng `sites`.
3. Lượt **ngoài lồng**: một bước chạy mọi đường, ghi kết quả.
4. Đưa cả hai vào lồng (bước của `cage.enable`) với trần cố định của phép thử: RAM 160 MB, CPU 100%, 60 tiến trình, 3 worker.
5. Lượt **trong lồng**: chạy lại mọi đường.
6. Dọn: `probeCleanupSteps` cho B rồi A (gỡ vhost và file lạ của L8, tắt và gỡ lồng, gỡ pool, xoá DB và user DB, xoá thư mục site và log, gỡ cron, xoá user Linux). Mọi bước tự bỏ qua khi thứ đó không còn.
7. Ghi kết quả và `state='done'`, `cleaned=1`.

`--only ngoai` bỏ bước 4 và 5; `--only trong` bỏ bước 3.

**Dọn trong mọi trường hợp (bất biến 5).** Một bước hỏng giữa chừng: engine hoàn tác ngược, mà hoàn tác của bước tạo chính là dọn. Panel bị tắt giữa lúc chạy: dòng `running` còn trong sổ; `probe.leftover` báo lỗi; `tpan isolation probe --cleanup`, lượt thử kế tiếp, và bộ hẹn giờ của `tpan serve` (khi không có job) đều chạy Plan `probe.cleanup` gồm `probeCleanupSteps` cho hai tên trong sổ, rồi đặt `cleaned=1`. Plan dọn **chỉ nhận tên vừa khớp mẫu vừa có trong sổ**; mọi đường dẫn nó chạm đều suy từ `site.LayoutOf(tên đó)`.

**Các đường.** Mỗi đường là một file PHP nhỏ đặt vào `public` của A (hoặc một lệnh), gọi qua `curl --resolve <A>:80:127.0.0.1`; kết quả chỉ in `CO`/`KHONG`, tên user và số đếm, không in nội dung hay tên của site thật.

| Mã | Đường | Ngoài lồng | Trong lồng |
|---|---|---|---|
| L1 | PHP của A `include` `wp-config.php` của B qua OPcache | bị chặn | bị chặn |
| L2 | `opcache_get_status()` từ A | bị chặn | bị chặn |
| L3, L4, L9 | `.htaccess` của A (chỉ Apache; nginx ghi "không thuộc stack") | bị chặn | bị chặn |
| L5 | symlink của A sang file của B (chỉ nginx) | bị chặn | bị chặn |
| L6 | A đọc `access.log` của B | bị chặn | bị chặn |
| L7 | PHP của A đếm tiến trình của user khác | bị chặn | bị chặn |
| L8 | vhost lạ trùng tên miền của A: phép kiểm `web.server_name.unique` phải chỉ ra file | bị chặn (phát hiện) | như ngoài lồng (không chạy lại) |
| C1 | A gọi shell: thư mục của B có trong `/var/www` không; `/var/www` có mấy mục | **lộ (cần lồng)** | bị chặn |
| C2 | A ghi một file vào `/tmp`; B có thấy không | **lộ (cần lồng)** | bị chặn |
| C3 | A cấp phát 320 MB | **lộ (cần lồng)** | bị chặn: tiến trình bị giết, `oom_kill` tăng 1, B vẫn 200 |
| C4 | A đọc ra tên user `tp_*` khác qua `getent passwd` | **lộ (cần lồng)** | bị chặn |

Khác đặc tả ở một chỗ: "không thấy tiến trình của ai khác" đã được M14 chặn cả ở ngoài lồng (`ProtectProc` của dịch vụ chung), nên nó là L7 và ra "bị chặn" ở cả hai cột, không phải "lộ (cần lồng)". C3 ngoài lồng chỉ chạy khi máy còn ít nhất 1 GB RAM trống; không thì ghi "bỏ qua" kèm lý do. Phép thử không sinh tiến trình hàng loạt và không đốt CPU ở ngoài lồng: hai việc đó làm hại máy thật.

Trạng thái của một ô: `blocked`, `exposed`, `needs_cage`, `skipped`, `na`. Lệnh thoát mã 1 khi có ô `exposed` (đường lẽ ra phải bị chặn mà lộ).

### 3.10 CLI và API

```
tpan cage status [<domain>] [--json]
tpan cage enable  <domain> | --all   [--dry-run] [--yes] [--json]   (--all: mọi site gói muốn lồng mà còn ở ngoài)
tpan cage disable <domain> | --all   [--dry-run] [--yes] [--json]   (--all: mọi site trong lồng mà gói không còn muốn)
tpan package add|edit <tên> ... [--cage on|off] [--cpu N] [--ram-mb N] [--tasks N] [--io-read-mb N] [--io-write-mb N] [--php-workers N]
tpan package set                     (tên khác của edit)
tpan usage [<domain>] [--suggest] [--json]
tpan isolation probe [--only ngoai|trong] [--cleanup] [--dry-run] [--yes] [--json]
```

`package edit` đổi trần của gói có site trong lồng: ghi gói, rồi chạy job `cage.sync` cho các site đó (với `--dry-run`: in cả hai). `user edit --package` cũng vậy cho site của user đó. Job hỏng thì gói đã đổi, trần cũ còn áp: `cage.limits` báo lỗi và có sửa tự động.

| Route | Luật |
|---|---|
| `GET /api/v1/cage` | admin |
| `POST /api/v1/sites/{domain}/cage[?dry_run=1]`, thân `{"enabled": true|false}` | admin |
| `GET /api/v1/usage` | any (lọc theo chủ) |
| `GET /api/v1/sites/{domain}/usage[?range=day|week|month]` | owner:site |
| `GET /api/v1/usage/suggestions` | admin |
| `GET /api/v1/security/probe` | admin |
| `POST /api/v1/security/probe[?dry_run=1]`, thân `{"only": ""|"ngoai"|"trong"}` | admin |
| `POST /api/v1/security/probe/cleanup` | admin |
| `POST /api/v1/packages`, `POST /api/v1/packages/{name}` | admin (như cũ); nhận thêm bảy trường; trả thêm `sync_job_id` khi có job |

Mã lỗi mới: `cage_not_in_package`, `cage_not_caged`, `cage_cgroup_v1`, `cage_systemd_old`, `cage_active`, `package_limit_invalid`, `package_limits_need_cage`, `domain_reserved`, `probe_running`, `usage_not_caged` (404 cho site ngoài lồng ở route usage của site: không có cgroup riêng để đọc).

## 4. Bất biến

Mỗi câu gắn với test. `U` là unit test (tên hàm), `E` là khẳng định trong `scripts/e2e-m17.sh`.

| # | Luôn đúng | Test |
|---|---|---|
| 1 | Site ngoài lồng không đổi hành vi khi máy nâng cấp hoặc khi site khác vào lồng | U `TestVhostNgoaiLongKhongDoiByte` (golden cũ); U `TestUpgradeTuV020KhongDoiDong`; E "nâng cấp: sha256 mọi file cấu hình trước và sau bằng nhau"; E "B ngoài lồng: sha256 pool và vhost không đổi sau khi A vào lồng" |
| 2 | Vào lồng rồi ra lồng: file cấu hình của site về đúng từng byte | U `TestCageEnableDisableTraLaiTungByte` (FakeSystem, so `Snapshot`); E "sha256 pool, vhost, cron, wp-config, .htaccess trước khi vào bằng sau khi ra" |
| 3 | Mỗi site đúng một nơi phục vụ PHP; không hai tiến trình chính cùng một socket | U `TestSitePHPStepsChiMotNoi`; U `TestCageStateBaoCaHaiCungCo`; E "trong lồng: không có file pool chung, đúng một `php-fpm: master` của user đó; ngoài lồng: không có unit `tpan-cage-<slug>-*`" |
| 4 | Trần ghi trong gói bằng trần trên cgroup; lệch thì health báo và sửa được | U `TestCageLimitsSoTungFile`; E "sửa tay file slice rồi `daemon-reload`: `cage.limits` lỗi; `tpan health --fix` đưa về; `package edit` đổi RAM: `memory.max` đổi, `MainPID` giữ" |
| 5 | Thử tấn công luôn dọn sạch, kể cả khi hỏng giữa chừng hoặc panel bị tắt | U `TestProbeHongGiuaChungVanDon`; U `TestProbeCleanupChiNhanTenTrongSo`; E "`kill -9` tpan giữa lượt thử, rồi `--cleanup`: không còn user, thư mục, DB, vhost, unit, cron, file `/tmp` nào mang tên phép thử" |
| 6 | Con số hiện ra bằng con số đọc từ cgroup | U `TestReadUsageDocDungFile`; E "sau một lần cấp phát vượt trần: `tpan usage --json` có `oom_kills` tăng đúng 1 và `mem_peak` bằng `memory.peak` đọc tay" |
| 7 | User thường không tự nâng trần, không tự ra khỏi lồng | U `TestPhanQuyenMoiRoute` (phủ route mới); U `TestMoiThaoTacCuaChuGiuLong` (đổi PHP, khôi phục, chạy lại, sao lưu trên site `caged` đều ra Plan của lồng); E "user chủ gọi `POST /sites/{d}/cage` và `POST /packages/...`: 403; sau đổi PHP, tạm khoá rồi mở, khôi phục, thùng rác rồi khôi phục, chuyển stack: `caged` vẫn 1 và `cage.limits` đạt" |
| 8 (thêm) | Mã PHP của site trong lồng không bao giờ chạy ngoài hàng rào do tpan gọi | U `TestWrapLuonCoUserTrenDongLenh`; U `TestBareChiOChecksIsolated`; U `TestWrapStatLoiThiKhongChay` |
| 9 (thêm) | Lệnh trong lồng bị giết không bao giờ bị đọc thành một mã thoát có nghĩa | U `TestWrapOOMKhongPhaiExitError`; U `TestWrapGiuNguyenThamSo` (kiểm có `--expand-environment=no`, `--service-type=oneshot`) |
| 10 (thêm) | Plan dọn của phép thử không chạm thứ gì ngoài tên nó đã ghi sổ | U `TestProbeCleanupTuChoiTenLa` (tên của site thật, tên khớp mẫu nhưng không có trong sổ) |

## 5. Mô hình đe doạ

**Ai tấn công.** (K1) Kẻ đã chạy được PHP dưới user của site A trong lồng. (K2) User thường của panel, chủ của A. (K3) Thứ do site kiểm soát mà tiến trình root của panel chạm vào.

| # | Đi đường nào | Chặn bằng | Chứng minh |
|---|---|---|---|
| K1.1 | Đọc file, tên, log của site khác | hàng rào: `/var`, `/run`, `/etc/tpan` là tmpfs chỉ gắn lại đồ của mình; `passwd` riêng; các `InaccessiblePaths` | `cage.fence`; probe C1, C4, L6; E "liệt kê `/var/www` chỉ thấy A" |
| K1.2 | Ghi ra ngoài thư mục site (`/tmp` chung, `/dev/shm`, `/var/lib/php/sessions`) | `PrivateTmp`, tmpfs `/dev/shm`, `ProtectSystem=strict` | probe C2; `t1` |
| K1.3 | Nhìn và gửi tín hiệu cho tiến trình khác | `ProtectProc=invisible`, `ProcSubset=pid`, khác uid | probe L7; `cage.fence` |
| K1.4 | Lấy thêm quyền: setuid (`sudo`, `su`, `crontab`), capability, namespace mới | `NoNewPrivileges`, tập capability rỗng, `RestrictSUIDSGID`, `RestrictNamespaces`, lọc syscall | `t1` (`CapEff=0`, `NoNewPrivs=1`); E "`crontab -l` trong lồng không chạy được" |
| K1.5 | Thoát trần: chuyển cgroup, đặt việc hẹn giờ chạy ngoài slice, nói chuyện với systemd | `/sys/fs/cgroup` và `/run` (D-Bus, socket của systemd) không thấy; không có `/var/spool`; mọi lệnh tpan chạy thay cho site nằm trong slice | E "`tpan cron run` của site: cgroup của tiến trình `wp` nằm dưới slice của site"; soát cuối |
| K1.6 | Ngốn RAM, CPU, tiến trình, đĩa để làm chết site khác | `MemoryMax` + `MemorySwapMax=0`, `CPUQuota`, `TasksMax`, `IO*BandwidthMax` ở slice | E bốn phép quá tải, B không rớt request nào |
| K1.7 | Tự làm sập lồng liên tục để chiếm lượt khởi động | `StartLimit` nới + health `cage.socket` + sửa tự động; chỉ site đó chịu | `t4`; U của `CageSocket` |
| K1.8 | Chạy mã trong tiến trình chính của FPM (cùng uid) | không có gì để lấy: tiến trình chính không giữ quyền hay bí mật nào ngoài của chính site; socket của site khác không nằm trong đó | cấu trúc (2.1) |
| K1.9 | Đánh sang socket của lồng khác | socket `www-data 0660`; `/run` trong lồng không có `tpan-cage` | `Isolated` (phép socket); probe L4 |
| K2.1 | Gọi API đưa site ra khỏi lồng hoặc sửa gói | route `admin`; mặc định là cấm | `TestPhanQuyenMoiRoute` |
| K2.2 | Dùng thao tác của chủ (đổi PHP, khôi phục, chạy lại) để rơi về pool chung | một hàm `sitePHPSteps` cho mọi Plan; `cage.state` báo lỗi nếu hai nơi cùng có | bất biến 7, 3 |
| K2.3 | Tạo domain trùng tên phép thử để bị dọn nhầm hoặc chiếm chỗ | `domain_reserved`; đuôi ngẫu nhiên 48 bit; Plan dọn đòi tên có trong sổ | bất biến 10 |

**Panel chạy bằng root đụng vào thứ do site kiểm soát (K3).**

- File của lồng nằm ở `/etc/systemd/system` và `/etc/tpan/cage`: thư mục của root, site không ghi được. Không Step nào của M17 ghi file bằng root vào thư mục của một site thật.
- Nguồn của các lệnh gắn (`BindPaths`) là `/var/www/<domain>` và `/var/log/tpan/sites/<domain>`. Site sở hữu thư mục thứ nhất nhưng không sửa được mục cha `/var/www` (của root), nên không thay được chính thư mục đó bằng symlink; bên trong nó thì site làm gì cũng chỉ là đồ của site. Thư mục log thuộc root.
- Mọi lệnh đọc nội dung do site viết (`wp`, `find`, `head` của phép quét `.htaccess`) chạy bằng user của site, giờ thêm hàng rào. Root không nạp mã PHP của site ở đâu cả (như trước).
- `cage.Wrap`: tên unit ghép từ slug đã kiểm dạng và 16 hex ngẫu nhiên; lệnh và tham số đi sau `--` dưới dạng argv; `--expand-environment=no`; `User=` có trên dòng lệnh. `Stat` hàng rào lỗi thì không chạy. Đua giữa `Stat` và lúc chạy (lồng vừa bị gỡ): lệnh chạy bằng user của site không có rào, tức đúng mức của v0.2.0, không bao giờ bằng root.
- Số liệu đọc từ cgroupfs (file của nhân, site không ghi được vì không thấy và không có quyền). Phân tích từng dòng `khoá giá-trị`; dòng lạ thì bỏ, không panic.
- Phép thử tấn công ghi file thử bằng root vào `public` của hai site thử: hai site đó do chính job tạo, không ai khác điều khiển. Trước khi ghi, bước kiểm thư mục đích thuộc đúng user của site thử và nằm dưới `/var/www/tpan-probe-`. File PHP thử không nhận tham số từ request.
- Plan dọn xoá cây thư mục bằng `steps.RemoveTree` (đã có giới hạn `Under`) với đường dẫn suy từ tên trong sổ.

## 6. Nâng cấp và lùi

**Từ v0.2.0 lên.** Migration 1700 chỉ thêm cột có DEFAULT và bảng mới. Không file nào trên máy được ghi lại: template của pool và vhost ngoài lồng không đổi (golden cũ giữ nguyên), không có bước nào chạy lúc nâng. Health không thêm phép kiểm nào cho site khi không gói nào bật lồng. Sau khi nâng, mọi site ở ngoài lồng như cũ; muốn dùng thì `tpan package edit <gói> --cage on ...` rồi `tpan cage enable <domain>`.

**Lùi.**

- Đưa một site ra khỏi lồng: `tpan cage disable`, bất kỳ lúc nào, trả lại đúng từng byte.
- Lùi file chạy về v0.2.0 (`tpan self-update --rollback`): bản cũ không biết gì về lồng (health của nó sẽ báo thiếu socket, `site add` chạy lại sẽ ghi lại pool chung trong khi vhost trỏ vào lồng). Vì vậy lõi **từ chối lùi khi còn site `caged`** (`cage_active`, kèm lệnh `tpan cage disable --all`). Ngoài ra, như từ M13: bản cũ từ chối mở `tpan.db` đã ở bản 1700; muốn chạy bản cũ phải chép lại bản sao `tpan.db` từ trước khi nâng. Không đổi điều đó ở mốc này.
- Gỡ hẳn tpan khỏi máy: không thuộc mốc này; `cage disable --all` trước là đủ để máy không còn unit nào của tpan cho site.

## 7. Kế hoạch kiểm thử

**Unit** (Mac, `make check`): golden cho năm template của lồng và bốn vhost bản trong lồng; bốn trường hợp chuẩn của mỗi Step (máy sạch → `Apply` → `Check`; chạy lại không lệnh nào; `Apply` rồi `Undo` thì `Snapshot` về như cũ; lệnh lỗi có ngữ cảnh); `cage.Wrap` với FakeSystem (bảng: không `AsUser`, `Bare`, user lạ, không có hàng rào, có hàng rào, `Stat` lỗi, mã thoát thường, `oom-kill`, `timeout`, không tạo được unit, ctx huỷ); `ReadUsage` với file cgroup mẫu chép từ máy ảo (cả kernel 6.8 lẫn dạng có `pids.events.local`); `suggest` (bảng: ít mẫu, có `oom_kill`, bộ đếm về 0 giữa chừng, bị bóp CPU); mọi Plan ở 3.6 với FakeSystem; phép kiểm; validate gói; store (migration, mẫu, gộp giờ, xoá cũ, sổ phép thử); `TestPhanQuyenMoiRoute` phủ route mới; các test ở mục 4.

**E2E bước 20** (`scripts/e2e-m17.sh`, chạy qua `make e2e-m17 P=<phần>`, mỗi phần từ máy sạch trên `tpan-m17`, log ở `docs/bang-chung/M17*.log`):

| Phần | Khẳng định chính (kết quả mong đợi) |
|---|---|
| `nginx` | stack nginx; gói `long` (RAM 200, CPU 50, 40 tiến trình, 3 worker, lồng bật); user `khach`; hai site A, B của `khach` tự vào lồng sau khi tạo. Từ PHP của A: `ls /var/www` đúng một tên; `/tmp` rỗng; 0 tiến trình của user khác. A cấp phát 400 MB: 502, A trả 200 lại trong 2 giây (thiết kế: request kế tiếp), B 0 request hỏng trên 60. Vòng CPU với máy bận: phần CPU nhận trong 45% đến 52% (trần 50%); không bao giờ trên 52%. Sinh 200 tiến trình: `pids.current` ≤ 40, `pids.peak` = 40, bộ đếm chạm trần ≥ 1, B vẫn 200. `tpan usage --json` và API: `oom_kills` = 1, `mem_peak` = `memory.peak` đọc tay, `tasks_peak` = 40. `tpan isolation probe --json`: mọi ô "trong lồng" `blocked`; ngoài lồng L1, L2, L5, L6, L7, L8 `blocked`, C1 đến C4 `needs_cage`; mã thoát 0; không còn gì mang tên `tpan-probe-`. `kill -9` giữa một lượt thử rồi `--cleanup`: sạch. Với A trong lồng: đổi sang PHP 8.4 và về lại; tạm khoá (403, socket `disabled`) rồi mở (200); sao lưu, sửa nội dung, khôi phục; thùng rác rồi khôi phục; `stack switch --to apache` rồi về nginx: sau mỗi việc A 200, `caged` = 1, `cage.limits` đạt, health không có lỗi. `package edit` đổi RAM 200 → 300: `memory.max` = 314572800, `MainPID` không đổi. Vòng `curl` trong lúc `cage enable` và `cage disable`: 0 request hỏng. Ra lồng: sha256 năm file về như trước. Khởi động lại máy ảo: A, B 200; site tạm khoá vẫn 403. Phân quyền: user chủ bị 403 ở route của admin, 404 ở site của người khác |
| `apache` | như trên với stack Apache (L3, L4, L9 thay L5), bỏ phần khởi động lại máy |
| `nang-cap` | cài gói v0.2.0 thật, stack, hai site, ghi sha256 mọi file dưới `/etc/nginx`, `/etc/php`, `/etc/cron.d`, `/etc/systemd/system`; lên bản của nhánh; sha256 không đổi, health như trước, không unit `tpan-cage-*`; bật lồng cho gói: hai site 200 trong lồng; tắt lồng: sha256 về đúng như trước; `self-update --rollback` khi còn site trong lồng bị từ chối `cage_active` |
| hồi quy | `scripts/e2e-m13.sh chinh`, `scripts/e2e-m14.sh chinh`, `scripts/e2e-m9.sh`: vẫn 0 không đạt |

**Soát cuối**: một thợ `fable`, `effort: max`, chưa đọc mã của mốc, trên máy ảo: thử thoát lồng, thử nâng trần bằng tài khoản user thường, thử làm hỏng lượt thử tấn công giữa chừng. Phát hiện và cách sửa ghi vào `docs/thay-doi/M17.md`.

## 8. Bảng chia việc

Luật 8 của `agent-dev/LO-TRINH.md` (bản 02:25 ngày 21/11): phần nhạy cảm dùng `opus`, `effort: high`; phần không nhạy cảm dùng `sonnet`; thợ soát dùng `fable`, `effort: max`. Mỗi đợt tối đa 4 thợ, không hai thợ chung file. Phiên chính đọc diff của thợ `sonnet` trước khi nhận, chạy `make check`, commit, đẩy `moc/M17`.

| Đợt | Phần | Vùng file | Thợ | Loại | Model |
|---|---|---|---|---|---|
| 0 | Hợp đồng: kiểu và chữ ký (thân rỗng trả lỗi "chưa viết"), migration 1700, dòng route kèm luật, trường `Bare` và `Describer` | `internal/account/types.go`, `internal/site/cage.go`, `internal/system/types.go`, `internal/*/cage_types.go` hoặc `types_m17.go` của từng gói, `internal/store/migrations/1700_cage.sql`, `internal/server/routes.go` | phiên chính | | |
| 1 | Template của lồng và vhost bản trong lồng, golden | `internal/templates/cage.go`, `cage_test.go`, `files/cage-*.tmpl`, `testdata/cage-*`, phần thêm ở `site.go`, `ssl.go` | `tho-loi` | nhạy cảm (sinh unit, vhost, pool) | opus |
| 1 | Store: cột mới của `sites`, `packages`; mẫu, gộp giờ, sổ phép thử | `internal/store/sites.go`, `users.go`, `usage.go`, `probe.go` và test | `tho-phu` | không | sonnet |
| 1 | `cage.Wrap`, `ReadUsage`; engine và checks dùng `system.Describe` | `internal/cage/`, `internal/engine/engine.go` (chỉ `recSys`), `internal/checks/run.go` (chỉ `execCmd`) | `tho-loi` | nhạy cảm (chạy lệnh thay cho site) | opus |
| 1 | Phép kiểm của lồng; `Isolated` thêm `Bare` và socket của lồng | `internal/checks/cage.go`, `cage_test.go`, `site.go` (chỉ `Isolated`) | `tho-phu` | không | sonnet |
| 2 | Step `CageFiles`, `CageSocket`, vhost bản trong lồng | `internal/steps/cage.go`, `cage_test.go`, phần thêm ở `site_web.go`, `ssl.go` | `tho-loi` | nhạy cảm | opus |
| 2 | Số liệu: lấy mẫu, gộp, gợi ý | `internal/core/usage.go`, `usage_test.go` | `tho-phu` | không | sonnet |
| 2 | Kiểm trần của gói; `route explain` cho site trong lồng | `internal/core/users.go` (chỉ phần gói), `route_m14.go` và test | `tho-phu` | không | sonnet |
| 3 | Plan của lồng và ghép với thao tác đã có | `internal/core/cage.go`, `cage_test.go`, `site.go`, `suspend.go`, `trash.go`, `php.go`, `fix.go`, `health.go`, `backup.go`, `apply.go`, `selfupdate.go` | `tho-loi` | nhạy cảm | opus |
| 4 | Thử tấn công | `internal/core/probe.go`, `probe_test.go`, `internal/core/probe/` (file PHP thử) | `tho-loi` | nhạy cảm (root ghi file, tạo và xoá user) | opus |
| 4 | API của lồng, số liệu, gói; bộ hẹn giờ lấy mẫu | `internal/server/api_m17.go`, `api_m17_test.go`, `usage_timer.go`, `api_m13.go` (chỉ hai handler của gói), `internal/health/collect.go` | `tho-phu` | không (luật của route do phiên chính ghi ở đợt 0) | sonnet |
| 4 | CLI `cage`, `usage`, cờ mới của `package` | `cmd/tpan/cage.go`, `usage.go`, `package.go` và test | `tho-phu` | không | sonnet |
| 5 | API và CLI của thử tấn công; dọn tự động trong bộ hẹn giờ | `internal/server/api_probe.go`, `cmd/tpan/isolation.go` và test | `tho-phu` | không | sonnet |
| 5 | Script e2e (viết, chưa chạy) | `scripts/e2e-m17.sh`, `scripts/e2e-m17/`, target `e2e-m17` trong `Makefile` | `tho-may-ao` | | opus |
| 6 | Chạy e2e bước 20 hai stack, nâng cấp, hồi quy; sửa lỗi gốc | máy ảo `tpan-m17`, `docs/bang-chung/M17*.log` | `tho-may-ao` | | opus |
| 7 | Soát cuối trên máy ảo | chỉ đọc mã; ghi `docs/thay-doi/M17.md` | thợ soát | | fable, max |
| 7 | Giải thích | `docs/giai-thich/17-m17-long-theo-site.md` | `tho-viet` | không | sonnet |

Mục phải đọc trong file này: đợt 1 đọc 2.1 đến 2.4, 3.1 đến 3.4, 3.7, 3.8, 5; đợt 2 đọc thêm 3.5, 3.8; đợt 3 đọc 3.5, 3.6, 4, 5; đợt 4 và 5 đọc 3.8 đến 3.10, 4, 5; đợt 6 đọc mục 7.

## Soát thiết kế

(Thợ soát ghi phát hiện ở đây; phiên chính ghi cách đã sửa ngay dưới từng phát hiện.)
