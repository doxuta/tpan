# M17: lồng theo site, trần tài nguyên theo gói, thử tấn công (bản thiết kế)

Bản thiết kế của đợt 0, viết ngày 21/11/2026 trước khi có dòng mã nào của mốc. Phiên chính của M17 viết; mọi con số ở đây đều có lệnh và output thật ở `docs/bang-chung/M17-nghien-cuu/` (script `t0` đến `t13b`, chạy trên máy ảo `tpan-m17`: Ubuntu 24.04.5, kernel 6.8.0-147, systemd 255.4, PHP 8.3.6, nginx 1.24.0, Apache 2.4.58, MariaDB 10.11.14). Khi mốc xong, file này được gộp vào mục 6.15 của `docs/KE-HOACH.md`.

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
| Cây thư mục | `ProtectSystem=strict`, `ProtectHome=yes`, `PrivateTmp=yes`, `PrivateDevices=yes`, `TemporaryFileSystem=/var:ro` rồi gắn lại đúng `/var/www/<domain>` (ghi được), `/var/log/tpan/sites/<domain>`, `/var/cache/tpan` (chỉ đọc), `-/run/mysqld:/var/run/mysqld` (chỉ đọc) | cả máy chỉ đọc; dưới `/var` chỉ thấy đồ của mình |
| | `TemporaryFileSystem=/run:ro` rồi gắn lại `-/run/mysqld`, `-/run/systemd/resolve`, `/run/systemd/notify`, `/run/systemd/journal/dev-log` (đều chỉ đọc) | không thấy socket và tên unit của site khác; vẫn nối DB, phân giải tên, báo `READY` cho systemd, ghi log. Dấu `-`: thư mục chưa có thì bỏ qua thay vì làm dịch vụ chết vòng (mục 2.8) |
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

### 2.8 Dịch vụ nền khởi động lại, máy khởi động lại, cập nhật gói PHP (`t10` đến `t13b`)

Lồng gắn vài thứ dưới `/run` vào không gian riêng của nó. Một lần gắn trỏ vào thư mục lúc dịch vụ khởi động; thư mục bị tạo lại sau đó thì lồng không thấy bản mới.

| Tình huống | Kết quả |
|---|---|
| MariaDB restart, hoặc stop rồi start, khi lồng đang chạy (`t10`) | `/run/mysqld` giữ nguyên (cùng inode): trong lúc MariaDB tắt site ra 500, MariaDB lên lại thì 200, không phải đụng lồng |
| `systemd-resolved`, `systemd-journald` restart (`t10`) | thư mục giữ nguyên (`RuntimeDirectoryPreserve=yes`): phân giải tên và log của FPM vẫn chạy |
| Lồng được đánh thức lúc `/run/mysqld` chưa có (như lúc máy vừa lên), lệnh gắn bắt buộc (`t11`) | dịch vụ chết với `226/NAMESPACE`, systemd thử lại liên tục (`NRestarts=5`); chậm thêm chút là chạm giới hạn khởi động và site 502 cho tới khi có người reset |
| Như trên, lệnh gắn có dấu `-` (`t11`) | dịch vụ chạy nhưng **không bao giờ thấy MariaDB** (site 500 mãi, kể cả sau khi MariaDB lên) cho tới khi lồng khởi động lại. Nhận ra được: `/proc/<MainPID>/mountinfo` không có dòng ` /run/mysqld ` |
| Thêm `After=mariadb.service` vào dịch vụ (`t11`) | không giúp gì: `After=` chỉ xếp thứ tự hai việc cùng được khởi động, không chờ MariaDB khi MariaDB không nằm trong cùng lượt |
| `ExecStartPre=+install -d ... /run/mysqld` trong unit **dịch vụ** (`t13`, `t13b`) | không tạo được thư mục: trên systemd 255 lệnh có dấu `+` vẫn thấy `/etc/passwd` riêng của lồng (`install: invalid user 'mysql'`), tức vẫn nằm trong không gian riêng; `mkdir` cũng không tới được `/run` thật |
| `ExecStartPre=-/usr/bin/install -d -m 0755 -o mysql -g root /run/mysqld` trong unit **socket**, thử thật lúc máy khởi động với một unit gọi site trước MariaDB (`t12`) | lúc gọi thư mục đã có, MariaDB chưa chạy: 500; MariaDB lên: **200 mà không phải đụng lồng**; mountinfo có ` /run/mysqld `. Unit socket chạy bằng root ở máy thật trước khi nghe, nên tạo được thư mục; lệnh và tham số cố định, giống hệt dòng MariaDB tự chạy |
| Cài lại `php8.3-fpm` (giả lập cập nhật bản vá) khi lồng đang chạy (`t13`) | `needrestart` 3.6 của Ubuntu tự khởi động lại dịch vụ của lồng (`MainPID` đổi), site 200. Máy không có `needrestart` thì lồng chạy tiếp bản cũ: `/proc/<MainPID>/exe` kết thúc bằng ` (deleted)` |

Vì vậy: unit socket có dòng `ExecStartPre` trên; các lệnh gắn `/run/mysqld` và `/run/systemd/resolve` có dấu `-`; phép kiểm `cage.socket` đọc mountinfo và `exe` của tiến trình chính để bắt hai trường hợp còn lại (thư mục mất giữa chừng rồi lồng khởi động lại; lồng chạy bản PHP đã bị thay), kèm sửa tự động `cage.restart` (mục 3.7).

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
| `tasks` | tiến trình và luồng | không giới hạn | 0, hoặc 32 đến 100000, và ≥ số worker + 15 (`TasksMax` đếm cả luồng: ImageMagick sinh luồng theo số nhân lúc làm thumbnail, cron `wp` kèm `sh`, `mysqldump`; trần sát dễ làm upload ảnh hỏng. Chưa thử với `php-imagick`, soát thiết kế điểm 26) | `TasksMax=<n>` |
| `io_read_mb`, `io_write_mb` | MB/giây (10⁶ byte) | không giới hạn | 0, hoặc 1 đến 100000 | `IOReadBandwidthMax=/var/www <n>M`, `IOWriteBandwidthMax=...` |
| `php_workers` | tiến trình con PHP cùng lúc | **mặc định 5** (PHP-FPM bắt buộc có một con số; đây là số của pool ngoài lồng) | 0, hoặc 1 đến 200 | `pm.max_children` |

Trần chỉ được áp bằng cgroup của lồng. Gói có một trần khác 0 (kể cả `php_workers`: pool ngoài lồng luôn 5) mà `cage` tắt thì bị từ chối (`package_limits_need_cage`): không lưu một con số không có hiệu lực. Trần dưới của RAM (64) chỉ để chặn gõ nhầm làm site chết vòng; con số hợp lý cho WordPress đến từ phần gợi ý (3.8).

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
| `CageSocket()` | `/run/tpan-cage/<16 hex đầu của sha256(domain)>/php.sock`. Không mang slug: `/proc/self/net/unix` liệt kê đường dẫn mọi Unix socket của máy cho mọi tiến trình (mạng dùng chung, `t14` phần P5), nên tên socket của lồng không được để lộ tên miền |
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
ExecStartPre=-/usr/bin/install -d -m 0755 -o mysql -g root /run/mysqld
ListenStream=/run/tpan-cage/<16 hex>/php.sock
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
RequiresMountsFor=/var/www/<domain>
[Service]
Type=notify
User=tp_<slug>
Group=tp_<slug>
Slice=tpan-<slug>.slice
NoNewPrivileges=yes
Environment=FPM_SOCKETS=/run/tpan-cage/<16 hex>/php.sock=3
ExecStart=/usr/sbin/php-fpm<bản> --nodaemonize --fpm-config /etc/tpan/cage/<slug>/php-fpm.conf
ExecReload=/bin/kill -USR2 $MAINPID
KillSignal=SIGQUIT
TimeoutStopSec=30
OOMPolicy=continue
Restart=on-failure
RestartSec=5s
```

`User`, `Group`, `Slice`, `NoNewPrivileges` lặp lại ở đây dù hàng rào cũng có (lý do ở 2.2): mất drop-in thì dịch vụ vẫn chạy bằng user của site, trong slice có trần. `RestartSec=5s`: mười lần khởi động lại mất 50 giây, quá cửa sổ 30 giây của giới hạn khởi động, nên một nguyên nhân tạm thời (trần quá thấp rồi được nâng lại, MariaDB chưa lên) không bao giờ đẩy dịch vụ và socket vào `failed`. Đã thử dưới tải (`t15`: trần 8M, gọi site liên tục 40 giây): dịch vụ ở `activating`, socket vẫn `active`, file socket còn; trả trần 200M thì site tự trả 200 sau 1,7 giây, không lệnh nào khác; request tới socket không đánh thức dịch vụ nhanh hơn `RestartSec`; `t14` phần P1 đo ở `RestartSec` mặc định: hạ trần RAM xuống 8M làm cả hai kẹt ở `failed` (`service-start-limit-hit`, file socket mất), trả trần cũ rồi `try-restart` vẫn 502, chỉ `reset-failed` rồi `start` socket mới cứu. Mọi đường trả lại của Step vẫn làm `reset-failed` (3.5).

**Hàng rào** (`fence.conf`): đúng danh sách ở 2.3, theo thứ tự của `lib.sh` trong thư mục nghiên cứu cộng dòng `SystemCallFilter=~@privileged @resources` và danh sách `pool.d` của mọi bản PHP.

**Thư mục**: `/etc/tpan` thuộc `tpan setup` (0700, phép tự kiểm của tpan đòi đúng quyền đó): `CageFiles` không tạo lại, không đổi quyền, không xoá nó; chỉ tạo khi chưa có (máy dùng CLI mà chưa chạy `tpan setup`), với đúng 0700 root. `/etc/tpan/cage` là 0700 root; `/etc/tpan/cage/<slug>` là 0755 root và ba file trong đó 0644 root (trong lồng `/etc/tpan` là tmpfs mới, chỉ thư mục được gắn lại mới cần quyền đọc cho user của site).

**Cấu hình FPM** (`php-fpm.conf`): `[global]` có `error_log = syslog`, `syslog.ident = php-fpm-<slug>`, `daemonize = no`, `process_control_timeout = 25s` (dưới `TimeoutStopSec=30` một chút, để dừng hay khởi động lại chờ request đang chạy tới 25 giây); pool `[tpan-<slug>]` có `listen = <socket của lồng>`, `pm = ondemand`, `pm.max_children = <worker>`, `pm.process_idle_timeout = 10s`, `pm.max_requests = 500`, và đúng các dòng `php_admin_*` của pool ngoài lồng (`open_basedir`, ba đường dẫn tạm, hai giới hạn upload, `log_errors`, `error_log`). Không có `user`, `group`, `listen.owner`, `listen.group`, `listen.mode`, `pid`: FPM không chạy bằng root nên các dòng đó không có tác dụng.

**`passwd`, `group`**: mỗi file 4 dòng lấy từ `getent` lúc Step chạy (root, www-data, user của site, nobody; nhóm tương ứng với `nogroup`). Nội dung chỉ biết được khi user của site đã có (uid, gid), nên hai file này **không** khai trong `Info().Files` (engine chụp danh sách đó trước khi chạy bước nào) và không vào `managed_files`; `Check` của Step vẫn so chúng, và phép kiểm `cage.fence` kiểm chúng bằng con đường độc lập (`getent passwd` trong lồng ra đúng 4 dòng).

**Lệch và sửa tự động của năm file còn lại** (slice, socket, dịch vụ, hàng rào, `php-fpm.conf`): chúng vào `managed_files` như mọi file tpan ghi. Ba chỗ của health phải biết chúng:

- `checks.DriftStage`: đường dẫn bắt đầu bằng `/etc/systemd/system/tpan-` hoặc `/etc/tpan/cage/` là chặng PHP (trước M17 rơi vào chặng TPAN). `tpan.service` của `tpan setup` không có dấu `-` sau `tpan` nên không bị xếp nhầm.
- `health.fileOwner`: thêm `site.Layout.OwnsCagePath(p)` (đúng năm đường dẫn của slug đó, so bằng dấu bằng hoặc tiền tố có dấu `-`, `/` ở cuối nên slug này không nhận nhầm đường dẫn của slug dài hơn).
- `core.Fixes`: sửa `file.rewrite` cho một đường dẫn của lồng dựng Plan gồm đúng `steps.CageFiles` của site đó (bước chỉ ghi file khác nội dung, rồi `daemon-reload` và `try-restart` theo luật ở 3.5); sha256 mong đợi lấy từ `CageFiles.Want(path)`.

### 3.4 Lệnh chạy thay cho site: `internal/cage`

Gói mới, hai việc.

**`cage.Wrap(sys system.System) system.System`.** Bọc `System`; `cmd/tpan/main.go` bọc `system.NewLocal()` một lần, nên CLI, `tpan serve`, cron và MCP đều đi qua. Với mỗi `Run(cmd)`:

1. `cmd.AsUser` rỗng, hoặc `AsUser` không phải dạng `tp_<slug>`: chuyển thẳng xuống dưới (chạy bằng `runuser` như trước). `AsUser` có kèm `Env` (chỉ `tpan brain` dùng `Env`, không bao giờ trên máy chủ): từ chối, không chạy.
2. `Stat` file hàng rào của slug đó. Không có: chuyển thẳng xuống dưới. Lỗi khác: trả lỗi, **không chạy gì**.
3. Có: chạy

   ```
   systemd-run --quiet --wait --pipe --expand-environment=no --service-type=oneshot
     --unit=tpan-cage-<slug>-run-<16 hex> -p User=<user> -p Group=<user> -p NoNewPrivileges=yes
     -p TimeoutStartSec=<giây> -E LC_ALL=C [--working-directory=<Dir>] -- <Name> <Args...>
   ```

   `Stdin` và `OnOutput` truyền nguyên. `TimeoutStartSec` là thời gian còn lại của ctx cộng 1 giây; ctx không có hạn thì 3600.
4. Mã thoát khác 0: hỏi `systemctl show <unit> -p LoadState -p Result -p ExecMainStatus -p ExecMainStartTimestamp`, rồi `systemctl reset-failed <unit>`. `LoadState=not-found`, hoặc mã trong dải 200 đến 243 mà `ExecMainStartTimestamp` rỗng (systemd không dựng được môi trường: `226/NAMESPACE`, `217/USER`, `203/EXEC`): lỗi "không chạy được lệnh" (không phải `ExitError`). `Result=exit-code` còn lại: `*system.ExitError` với mã thật. Mọi `Result` khác (`oom-kill`, `timeout`, `signal`, `core-dump`, `start-limit-hit`, `resources`...): `*cage.KilledError` (bọc `cage.ErrKilled`), để nơi gọi không nhầm "bị giết vì chạm trần" với một mã thoát có nghĩa (`wp core is-installed` trả 1 khi chưa cài). Lệnh chết vì `SIGPIPE` vẫn ra mã 0 (`t9b`): không nơi nào dựa vào mã thoát khi tpan tự đóng ống đọc.
5. ctx bị huỷ giữa chừng: `systemctl stop <unit>` bằng một ctx mới 10 giây. Unit tạm sống tối đa tới `TimeoutStartSec` kể cả khi tpan chết giữa chừng (trước M17, `wp` chạy bằng `runuser` chết theo tpan); 3600 giây là trần mới cho lệnh không có hạn (ghi ở "khác đặc tả"). `tpan serve` khởi động thì `systemctl reset-failed 'tpan-cage-*-run-*'` dọn unit hỏng còn sót.

"Site có lồng hay không" đọc từ **máy** (file hàng rào), không từ bảng `sites`: giữa một Plan vào lồng, bảng chưa đổi mà dịch vụ đã chạy trong lồng, và bước kiểm chứng của chính Plan đó phải chạy `wp` trong lồng thì mới chứng minh được gì.

**Không có ngoại lệ nào chạy dưới user của site trong lồng ở ngoài hàng rào.** Bản thiết kế đầu có trường `Cmd.Bare` cho các lệnh `test` của `checks.Isolated`. Bỏ: `t14` phần P4 cho thấy mọi tiến trình cùng uid với site mà chạy ngoài lồng đều mở cửa cho mã trong lồng: lồng thấy pid của nó (`ProtectProc=invisible` chỉ giấu tiến trình khác uid; systemd 255 chưa có PID namespace riêng), mở `/proc/<pid>/root` ra gốc thật của máy, giữ fd đó sau khi tiến trình đã thoát, và ghi được vào `/tmp` của máy. Vì vậy: `Isolated(from, to)` không sinh ra khi `from` ở trong lồng (3.7); mọi dòng "sửa tay" có lệnh dưới user của site in bằng `system.Describe` (ra dạng `systemd-run` khi site có lồng); Plan vào lồng giết mọi tiến trình cùng uid còn ở ngoài slice trước khi mã của site được chạy trong lồng (`steps.CageReap`, 3.5); `cage.runtime` báo lỗi khi còn tiến trình như vậy (3.7). Test tĩnh và test của bất biến 8 (mục 4).

Bằng chứng phải là lệnh thật: `system.Describe(sys, cmd)` (giao diện tuỳ chọn `system.Describer`) trả dòng `systemd-run ...` với đuôi unit `xem` khi lệnh sẽ chạy trong lồng. `engine` (sự kiện `command`) và `checks.execCmd` (bằng chứng) dùng hàm này thay cho `cmd.String()`.

**`cage.ReadUsage(sys, layout) (Usage, error)`.** Đọc các file cgroup của slice (bảng ở 3.8). Thư mục cgroup không có: `Usage{Running: false}`, không lỗi.

### 3.5 Step mới (`internal/steps/cage.go`)

**`CageFiles{Layout, PHP, Limits, Remove}`**

- `Remove=false`: `Check` đúng khi cả bảy file có đúng nội dung, quyền và chủ (3.3). `Apply`: giữ bản cũ của mọi file và trạng thái của socket và dịch vụ (active hay không); chỉ ghi (bằng `WriteFileAtomic`) những file khác nội dung, quyền hay chủ, theo thứ tự slice, socket, dịch vụ, `php-fpm.conf`, `passwd`, `group`, **hàng rào sau cùng** (lớp bọc lệnh coi "có hàng rào" là "có lồng", nên lệnh chỉ vào lồng khi mọi file nó cần đã có); rồi `systemctl daemon-reload`. Nếu file dịch vụ hay `php-fpm.conf` vừa được ghi: chạy `php-fpm<bản> -t --fpm-config <conf>` với `AsUser` (qua lớp bọc nên chạy trong lồng); hỏng thì trả mọi file về (đường trả lại dưới đây) và báo lỗi, chưa đụng tới dịch vụ. Rồi, nếu dịch vụ đang chạy và một trong dịch vụ, hàng rào, `php-fpm.conf`, `passwd`, `group` vừa được ghi: `systemctl try-restart <dịch vụ>` (hai file sau gắn vào lồng theo inode: thay bằng rename thì lồng đang chạy vẫn thấy bản cũ). Lệnh này không thành công thì ghi vào log của bước nhưng KHÔNG làm bước lỗi: file đã đúng, `-t` đã đạt, dịch vụ sống hay chết sau đó là việc của phép kiểm (`cage.socket`, `cage.runtime`) và của Plan gọi bước (Plan của chủ site có kiểm 200, Plan chính sách của admin thì không, mục 3.6). Chỉ file slice đổi thì không restart (trần đổi ngay sau `daemon-reload`, 2.4).
- **Đường trả lại** (lỗi giữa `Apply`, và `Undo`): trả mọi file về bản cũ (file mới tạo thì xoá, thư mục mới tạo thì gỡ), `daemon-reload`, `systemctl reset-failed <dịch vụ> <socket>`, rồi đưa hai unit về đúng trạng thái đã ghi: socket đang active thì `systemctl start <socket>` và chờ file socket (tối đa 20 giây); dịch vụ đang active thì `systemctl start <dịch vụ>`. Không đưa về được thì `Undo` trả lỗi (job thành `undo_failed`, nói thật), không bao giờ báo "đã hoàn tác" khi site đang chết (soát thiết kế, điểm 1; `t14` P1).
- `Remove=true`: `Check` đúng khi không còn file hay thư mục nào của lồng. `Apply`: gỡ **hàng rào trước tiên**, rồi các file còn lại và thư mục `/etc/tpan/cage/<slug>`, `.service.d`; gỡ `/etc/tpan/cage` nếu rỗng (không bao giờ đụng `/etc/tpan`); `daemon-reload`; `systemctl stop 'tpan-cage-<slug>-run-*'` và `systemctl reset-failed 'tpan-cage-<slug>-*'` (unit tạm sót lại). `Undo` trả lại theo đường trả lại.
- `Info().Files`: đúng năm file của `l.CageManagedFiles()` kèm nội dung (Delete khi Remove); `passwd`, `group` không bao giờ khai (3.3). `Want(path) ([]byte, bool)` trả nội dung mong đợi của một trong năm file đó.

**`CageSocket{Layout, PHP, Disable}`**

- Bật: `Check` đúng khi socket `enabled`, `active`, file socket có và là socket. `Apply`: `php-fpm<bản> -t --fpm-config <conf>` với `AsUser` (trong lồng); `systemctl reset-failed <socket> <dịch vụ>` (bỏ qua lỗi "không nạp"); `systemctl enable --now <socket>`; chờ tối đa 20 giây tới khi file socket có. **Không khởi động dịch vụ**: request đầu tiên đánh thức nó (2.1), và như vậy không gì do mã của site điều khiển được (giết tiến trình chính cùng uid, trả mã lạ) làm bước này hỏng. Hỏng thì lỗi kèm 20 dòng cuối `journalctl -u <dịch vụ> -u <socket> --no-pager -o cat`, đã bỏ ký tự điều khiển và ghi rõ là output của site (log của FPM mang dòng do PHP của site ghi). `Undo`: `disable --now` socket, `stop` dịch vụ.
- Tắt (`Disable=true`): `disable --now` socket, `stop` dịch vụ (chờ request đang chạy tới `process_control_timeout`, 25 giây), `stop` slice, gỡ thư mục `l.CageRunDir()` nếu rỗng (`RemoveOnStop` chỉ gỡ file socket). Danger. `Undo`: `reset-failed`, `enable --now` socket, chờ file socket; dịch vụ đã chạy trước đó thì `start` lại.

**`CageReap{Layout}`** (mới, soát thiết kế điểm 4): giết (`SIGKILL`) mọi tiến trình có uid là user của site mà không nằm trong `l.CageSlice()`. Đọc bằng Go qua `System` (không shell): `ReadDir("/proc")`, với mỗi số đọc dòng `Uid:` của `status` và nội dung `cgroup`; tiến trình biến mất giữa chừng thì bỏ qua. Giết bằng `kill -KILL <pid>...` (argv). `Check` đúng khi không còn tiến trình nào như vậy. `Undo` không làm gì (NoUndo: tiến trình đã giết không trả lại được, và đó là tiến trình của site chạy ngoài hàng rào). Uid của user lấy bằng `id -u <user>`.

**`CageRestart{Layout}`** (sửa tự động `cage.restart`): chạy `templates.CageMySQLDirCmd` bằng root ở máy thật, rồi việc của `CageReap`, rồi `systemctl try-restart <dịch vụ>`. `Check` đúng khi dịch vụ không chạy, hoặc khi mọi điều của `cage.runtime` (3.7) đạt. `Undo` không làm gì.

**Vhost**: `templates.SiteVhostCaged(l, web, acmeRoot, ssl, mode)` và `steps.SiteVhostCaged(...)` (một hàm cho cả bản có và không có cổng 443): giống hệt vhost thường, chỉ khác socket là `CageSocket()`. Chữ ký cũ không đổi, golden của vhost ngoài lồng không đổi một byte.

### 3.6 Plan

Mọi Plan dưới đây dựng ở `internal/core/cage.go` (kiểu `Cages`), trả `*Planned` như các ca sử dụng từ M12. Một hàm duy nhất `sitePHPSteps(st, cfg, limits)` chọn giữa pool chung (`steps.SitePool`) và lồng (`CageFiles` + `CageSocket`), như `siteVhost` là chỗ duy nhất chọn vhost; `siteVhost` thêm nhánh `st.Caged` (sau nhánh tạm khoá); `siteChecks` là chỗ duy nhất chọn bộ phép kiểm, với chữ ký mới `siteChecks(web, php, st site.Site, others []site.Site, lim *account.Limits)`: `st.Caged` chọn bộ trong lồng (cần `lim`), và `others` mang cờ `Caged` của từng site để bỏ `Isolated(st, o)` khi `st` trong lồng và `Isolated(o, st)` khi `o` trong lồng. `Sites.others` trả `[]site.Site`.

**Hai loại Plan, hai loại điều kiện** (soát thiết kế điểm 3). Mã PHP của site quyết định mã HTTP site trả về và giết được tiến trình chính của FPM (cùng uid), nên:

- **Plan chính sách của admin** (`cage.enable`, `cage.disable`, `cage.sync`, sửa gói, đổi gói của user): chỉ dừng hay hoàn tác vì điều site không điều khiển được: file ghi được, `php-fpm -t` trong lồng đạt (cấu hình do tpan sinh), socket do systemd giữ đang nghe, cgroup mang đúng trần, web server nhận cấu hình. Mã HTTP và trạng thái tiến trình của site chỉ là phép kiểm mềm (`checks.Soft`: cảnh báo trong báo cáo, không làm job hỏng). Nhờ vậy một site trả 500 cho `127.0.0.1` hay tự giết tiến trình chính không giữ được trần cao, không đứng ngoài lồng được (bất biến 7, 11).
- **Plan của chủ site hay của việc bảo trì** (đổi bản PHP, khôi phục backup, khôi phục từ thùng rác, chạy lại `site add`, mở khoá): giữ kiểm 200 như trước M17. Site chỉ tự làm hỏng việc của chính mình.

**`cage.enable <domain>`**

Điều kiện đầu (mã từ chối): site có và `active` (`site_not_found`, `site_trashed`); không tạm khoá (`site_suspended`); gói của chủ site bật lồng (`cage_not_in_package`, kèm lệnh `tpan package edit <gói> --cage on`); máy có cgroup v2 (`/sys/fs/cgroup/cgroup.controllers`, `cage_cgroup_v1`) và systemd từ 254 (`cage_systemd_old`: cờ `--expand-environment` có từ bản đó); bản PHP của site đang cài; file pool chung và vhost của site đang khớp đúng nội dung tpan sinh (`cage_drift`, kèm lệnh `sudo tpan site add <domain>` hoặc `tpan health --fix`: ra lồng sinh lại hai file đó từ template, nên chỉ khi lúc vào chúng đã khớp template thì ra lồng mới trả lại đúng từng byte, bất biến 2). Site Apache ở chế độ `.htaccess` `day-du`: không từ chối, nhưng bản xem trước có dòng cảnh báo "lồng không chặn được đường chuyển request sang socket của site khác bằng `.htaccess` ở chế độ này" (soát thiết kế điểm 14). Site đã ở trong lồng thì **không từ chối**: Plan chạy lại, bước đã đúng được bỏ qua (cũng là cách áp lại trần và sửa lệch).

Bước:

1. `CageFiles` với trần của gói (hàng rào ghi sau cùng).
2. `CageSocket` (bật: `-t` trong lồng, socket nghe; dịch vụ chưa chạy). Vhost còn trỏ pool chung.
3. Gỡ pool chung của site (`Absent` file pool, `php-fpm<bản> -t`, reload `php<bản>-fpm`: tiến trình con của pool đó bị dừng).
4. `CageReap`: giết mọi tiến trình cùng uid với site còn ở ngoài slice (tiến trình nền do mã của site sinh ra từ trước, lệnh cron đang dở).
5. Vhost bản trong lồng qua `siteVhost` (kiểm cú pháp, reload web server). Từ đây request đi vào socket của lồng và đánh thức dịch vụ.

Bước 3 đến 5 nằm trước lúc request đầu tiên vào lồng, nên lúc mã của site bắt đầu chạy trong lồng thì không còn tiến trình nào cùng uid ở ngoài để mở `/proc/<pid>/root` (soát thiết kế điểm 4, `t14` P4). Đổi lại có một khoảng ngắn giữa bước 3 và bước 5 mà request được trả 502 (pool chung đã gỡ, vhost chưa đổi); e2e đo khoảng này bằng vòng `curl` và ghi con số (đề bài: "đo được thời gian gián đoạn").

Kiểm chứng: bộ phép kiểm trong lồng của site (`siteChecks` với trần của gói), mọi phép bọc `policyVerify`: Fail của phép **không** thuộc tập cứng hạ xuống Warn. Tập cứng: chặng web (cấu hình web server, vhost), `cage.socket`, `cage.limits` (và `php.socket` cho Plan ra lồng). Mềm: phép gọi HTTP, WordPress, DB, cô lập, `cage.runtime`, và cả `cage.fence` (khác bản trước của thiết kế, sửa lúc viết lời giao việc đợt 3): `cage.fence` chạy lệnh mới trong slice của site, mà lúc kiểm chứng mã của site đã nhận request và lấp được số tiến trình của slice (bằng `exec`), làm `systemd-run` không khởi động được và job hỏng theo ý site; hàng rào đã được kiểm khi `php-fpm -t` chạy trong lồng ở bước 2, trước khi mã của site vào slice; health vẫn chạy `cage.fence` như phép cứng mỗi giờ. Thành công thì ghi `caged=1`. Hoàn tác đi ngược: vhost về, pool chung về (ghi lại từ template, chờ socket), lồng tắt, file gỡ.

**`cage.disable <domain> | --all | --stale`**

Điều kiện đầu: site `active`, không tạm khoá; máy **có file của lồng** (đọc máy như lớp bọc lệnh, không đòi `caged=1`: đây cũng là Plan dọn lồng dựng dở sau một lần panel bị tắt giữa `cage.enable`, soát thiết kế điểm 6), không có thì `cage_not_caged`. Không đòi cờ của gói: admin được đưa một site ra để gỡ lỗi; health cảnh báo cho tới khi đưa vào lại.

Bước: `steps.SitePool` (ghi, kiểm, reload, chờ socket) → vhost bản thường → `CageSocket{Disable}` → `CageFiles{Remove}` (hàng rào gỡ trước tiên). Kiểm chứng: bộ phép kiểm thường của site bọc `policyVerify` (như `cage.enable`). Thành công thì `caged=0`. File pool và vhost sinh lại từ template nên giống từng byte bản trước khi vào lồng.

`--all`: mọi site đang có lồng trên máy (lời khuyên của `cage_active` ở mục 6 dùng cờ này). `--stale`: chỉ site có lồng mà gói không còn muốn lồng. `cage enable --all`: mọi site mà gói muốn lồng mà chưa có lồng. Mỗi site một job, site này hỏng không kéo site khác.

**`cage.sync`** (dùng khi sửa gói, đổi gói của user, và sửa tự động `cage.limits`, `cage.fence`): một Plan, mỗi site đang trong lồng của danh sách một bước `CageFiles` với trần mong muốn (bước bỏ qua khi file đã đúng). Không có phép gọi HTTP: trần đổi qua file slice và `daemon-reload`, có hiệu lực trên cgroup ngay cả khi dịch vụ đang chết (2.4), và site không điều khiển được bước nào. Kiểm chứng: `cage.limits` của từng site so với trần mong muốn. Trần thấp hơn mức site đang dùng: bản xem trước nêu rõ (đọc `memory.current` lúc dựng Plan), job vẫn chạy (chính sách của admin); site chạm trần thì các request lớn chết, số liệu và gợi ý nói ra điều đó.

**Sửa gói và đổi gói của user** (nhà của phần này: `internal/core/cage.go`, đợt 3):

- `Cages.PrepareEditPackage(ctx, p account.Package) (*Planned, error)`: chỉ đọc (để `--dry-run` và câu hỏi "Chạy? [y/N]" không đổi gì). Kiểm trần như `Accounts.EditPackage`; trả `nil` khi không có site `caged` và `active` nào dùng gói hay trần không đổi (người gọi dùng `Accounts.EditPackage` như trước, không có job). Có: `Planned` của Plan `cage.sync` dựng từ trần mới, với móc `before` (trường mới của `core.Planned`, chạy đầu `Execute`, trước engine) **ghi dòng gói trước** (chính sách của admin, không chờ máy) rồi engine chạy job. Job hỏng (lỗi ghi file, hiếm) thì dòng gói vẫn là trần mới; `cage.limits` của site nào chưa áp được báo lỗi và sửa tự động `cage.sync` áp trần trong bảng. Không có đường nào để một site làm job hỏng (khác bản đầu của thiết kế: bản đầu ghi gói trong `after` của job có kiểm 200, nên site chặn được việc hạ trần).
- `Cages.PrepareUserEdit(ctx, name, ch, by) (*Planned, error)`: chỉ đọc; trả `nil` khi `ch` không đổi gói hay trần của các site `caged` của user không đổi (người gọi dùng `Accounts.EditUser`); có thì `Planned` của `cage.sync` với móc `before` gọi `Accounts.EditUser(ctx, name, ch, by)`. Gói mới tắt cờ lồng: site vẫn trong lồng nhưng trần về 0 (không giới hạn), bản xem trước nói rõ, `cage.state` cảnh báo "gói không còn muốn lồng".
- CLI (`package edit`, `user edit --package`) và API (`POST /packages/{name}`, `POST /users/{name}`) gọi hai hàm này; có Plan thì chạy job (API trả `202 JobStarted`, `job_id` là job `cage.sync`).

**Ghép với thao tác đã có** (site đang `caged`):

| Thao tác | Đổi gì |
|---|---|
| Tạo site mới (`site.add`) | Plan tạo site không đổi (site sinh ra ở ngoài lồng). Lần tạo đầu (không phải chạy lại) thành công và gói của chủ bật lồng thì `SiteAdd.Execute` chạy tiếp một job `cage.enable`; job đó hỏng thì site vẫn dùng được, kết quả ghi lý do (`cage_error`) và health cảnh báo. Chạy lại `site add` của một site còn ở ngoài lồng KHÔNG tự đưa vào lồng. Job hàng loạt (`apply`) làm như pha D của SSL: sau lô, mỗi site mới một job |
| Chạy lại `site add` | `sitePHPSteps` ra bước của lồng, `siteVhost` ra vhost của lồng: không ghi lại pool chung |
| Đổi bản PHP (`php.use`) | Plan riêng: `CageFiles` với bản mới (file dịch vụ đổi `ExecStart`: `-t` trong lồng rồi `try-restart`; socket giữ nguyên, kết nối xếp hàng) → kiểm 200 hai lần (Plan của chủ site). Hỏng thì đường trả lại của `CageFiles` đưa bản cũ chạy lại. Không đụng vhost, không đụng pool chung |
| Cài bản PHP (`php.install`), "Áp bản vá cô lập" (`security.harden`) | thêm một bước cuối khởi động lại mọi dịch vụ lồng đang chạy (`cageRestartStep`), để thư mục `pool.d` của bản mới bị che (hàng rào chỉ che đường dẫn có lúc dịch vụ khởi động, soát thiết kế điểm 13). Khởi động lại hỏng thì chỉ ghi vào log của job, không làm job hỏng: đây là Plan chính sách, và mã của site làm hỏng được việc khởi động lại (lấp số tiến trình của slice); lồng không lên lại thì `cage.socket` của health báo, kèm sửa tự động |
| Xin SSL (`ssl.issue`, sửa tự động `ssl.issue`) | vhost 443 qua `siteVhost`; kiểm chứng đổi từ `checks.SiteChecksSSL` gọi thẳng (`internal/core/ssl.go`) sang `siteChecks` với trần của lồng, nên site trong lồng ra `SiteChecksSSLCaged` (soát thiết kế điểm 2) |
| Tạm khoá | `suspendSteps`: vhost tạm khoá → `CageSocket{Disable}` thay cho đổi tên file pool → cron. File của lồng giữ nguyên; socket `disable` nên máy khởi động lại site vẫn nằm yên |
| Mở khoá (site, user) | `unsuspendSteps`: `sitePHPSteps` (tức `CageFiles` rồi `CageSocket` bật: file của lồng mất trong lúc khoá thì được ghi lại, như M13 ghi lại pool từ template) → cron → vhost → kiểm 200 (soát thiết kế điểm 7). Mở khoá user (`users.go`) truyền trần của lồng vào `siteChecks` |
| Đưa vào thùng rác | `trashSteps`: gỡ link vhost → **gỡ file cron** → `CageSocket{Disable}` → `CageFiles{Remove}` → khoá user DB, khoá user, chuyển thư mục như cũ. Gỡ cron trước khi gỡ hàng rào để không lượt cron nào chạy `wp` ngoài lồng giữa hai bước (soát thiết kế điểm 8). `caged` giữ nguyên trong lúc ở thùng rác |
| Khôi phục từ thùng rác | `restoreSteps`: chuyển thư mục về → mở khoá user → **`CageFiles` ngay sau đó**, trước mọi bước chạy `wp` (bước cron có `wp config set`) → cron → mở khoá user DB → `CageSocket` bật (thay cho pool) → vhost. Không lệnh nào dưới user của site chạy trước khi hàng rào có (soát thiết kế điểm 8) |
| Xoá hẳn | `purgeSteps` thêm `CageSocket{Disable}`, `CageFiles{Remove}` trước bước xoá DB và xoá user khi site có `caged=1` hoặc máy còn file của lồng (đọc máy như lớp bọc lệnh, nên lồng dựng dở vẫn được dọn); site không có lồng giữ đúng Plan cũ. Thành công thì xoá luôn mẫu số liệu của domain đó |
| Chuyển web server, harden, sửa tự động vhost | đi qua `siteVhost` nên tự ra vhost của lồng; kiểm chứng qua `siteChecks` |
| Sao lưu, khôi phục backup, DB | lệnh `wp` tự chạy trong lồng qua `cage.Wrap`; kiểm chứng qua `siteChecks` với trần của lồng |
| Cron | `tpan cron run` gọi `wp` với `AsUser`, tự vào lồng; hạn 4 phút thành `TimeoutStartSec` |
| Cập nhật gói PHP (`update.apply`, `unattended-upgrades`) | không có bước riêng: `needrestart` của Ubuntu khởi động lại dịch vụ của lồng (đo ở `t13`); máy không có nó thì `cage.runtime` cảnh báo "đang chạy bản PHP đã bị thay" kèm sửa tự động `cage.restart` |
| `self-update --rollback` | từ chối `cage_active` (mục 6) |

**Mọi chỗ trong `internal/core` đang giả định pool chung** (soát thiết kế điểm 2), và cách đổi. Sau M17 chỉ các hàm chọn được nhắc tới pool chung. Test tĩnh `TestKhongGoiPoolChungNgoaiHamChon` (`internal/core/nopool_m17_test.go`, đọc cây cú pháp chứ không quét chữ) đỏ khi một hàm ngoài danh sách gọi các tên đó: `steps.SitePool` chỉ trong `sitePHPSteps`; `checks.SiteChecks`, `checks.SiteChecksSSL` chỉ trong `siteRequestChecks` (phần chọn bộ phép kiểm theo đường request của `siteChecks`, `ssl.go` cũng dùng); `steps.PoolSuspend`, `steps.PoolUnsuspend` chỉ trong `Sites.suspendSteps`, `Sites.unsuspendSteps` (nhánh site ngoài lồng; site trong lồng đi nhánh `CageSocket` và `sitePHPSteps` của cùng hàm); `checks.PoolSocket` chỉ trong ba nhánh dành cho site ngoài lồng: nhánh DB đang khoá của `siteStateChecks`, `Fixes.plan` (sửa tự động `php.restart`, site trong lồng bị từ chối `no_auto_fix`), `PHP.PrepareUse` (phép kiểm giữa chừng; site trong lồng có Plan riêng).

| Chỗ | Đổi |
|---|---|
| `ssl.go` (kiểm chứng `checks.SiteChecksSSL`) | qua `siteChecks` với trần của lồng |
| `health.go` nhánh DB đang khoá (`checks.PoolSocket`) | site trong lồng: `cage.socket`, `cage.runtime` thay cho `php.socket` |
| `fix.go` `FixPHPRestart` (`PHPFPMRestart`, `PoolSocket`) | site trong lồng: từ chối `no_auto_fix` (phép `php.socket` không sinh cho site đó) |
| `fix.go` `siteFiles` (`steps.SitePool`) | site trong lồng: các `File` của `CageFiles` thay cho pool |
| `php.go` `PrepareUse` (`SitePool`, `PoolSocket`) | site trong lồng: Plan riêng ở bảng trên |
| `suspend.go` (`PoolSuspend`, `PoolUnsuspend`, `SitePool`) | qua `CageSocket` và `sitePHPSteps` như bảng trên |
| `trash.go` `restoreSteps` (`SitePool`) | qua `sitePHPSteps` |
| `site.go` `addPlan` (`SitePool`), `siteChecks` (`SiteChecks*`), `Info` (`SocketFor`) | `addPlan` giữ pool chung (site mới sinh ở ngoài lồng); `siteChecks` là hàm chọn; `Info` dùng `site.Site.Socket()` |
| `apply.go`, `backup.go`, `users.go` (gọi `siteChecks`) | truyền trần của lồng |
| `route_m14.go` (`SocketFor`) | site trong lồng: socket, pool, user đọc từ file của lồng (3.7) |

### 3.7 Phép kiểm (`internal/checks/cage.go`)

Site trong lồng dùng `SiteChecksCaged` / `SiteChecksSSLCaged`: như bản thường, thay `php.socket` bằng `cage.socket`, `cage.runtime`, `cage.fence` (nặng), `cage.limits`. `siteChecks` (với trần của lồng khác `nil`) là chỗ duy nhất chọn bản (mục 3.6); `siteStateChecks` cũng vậy cho nhánh DB đang khoá; nhánh site tạm khoá thêm `cage.state`.

| ID | Chặng | Kiểm gì | Không đạt | Sửa tự động |
|---|---|---|---|---|
| `cage.socket:<domain>` | PHP | file socket là socket, `www-data:www-data 0660`; unit socket `active`; dịch vụ không `failed` (thứ systemd giữ, site không điều khiển) | lỗi | `cage.start` (`CageSocket` bật: `reset-failed`, `enable --now` socket) |
| `cage.runtime:<domain>` | PHP | Chỉ khi dịch vụ đang chạy (không chạy thì đạt, "lồng đang ngủ"), đọc tiến trình chính: `status` có `NoNewPrivs: 1` và `CapEff` bằng 0; `cgroup` nằm dưới `tpan-<slug>.slice`; `mountinfo` có tmpfs ở `/var`, `/run`, `/etc/tpan`, và dòng gắn `/run/mysqld` (chỉ đòi khi `mariadb` active) và `/run/systemd/resolve` không mang `//deleted`; thời điểm dịch vụ vào active (`systemctl show --timestamp=unix -p ActiveEnterTimestamp`) muộn hơn lần sửa file hàng rào (`stat -c %Y`). Và cho cả máy: không có tiến trình nào cùng uid với site ở ngoài slice của site. `exe` của tiến trình chính kết thúc bằng ` (deleted)` (bản PHP đã bị thay) hoặc hàng rào mới hơn lúc dịch vụ khởi động: cảnh báo | lỗi; hai trường hợp sau: cảnh báo | `cage.restart` (`CageRestart`: tạo `/run/mysqld`, giết tiến trình cùng uid ngoài slice, `try-restart`) |
| `cage.fence:<domain>` | PHP, nặng (mỗi giờ) | bảy lệnh ở 2.3 chạy trong lồng (qua `AsUser`), mỗi lệnh một dòng bằng chứng. Kiểm hàng rào mà một tiến trình MỚI nhận; hàng rào của tiến trình đang chạy do `cage.runtime` kiểm | lỗi | `cage.sync` |
| `cage.limits:<domain>` | PHP | `memory.max`, `memory.swap.max`, `cpu.max`, `pids.max`, `io.max` của slice so với trần của gói. Quy đổi: file không có hoặc rỗng là không giới hạn (`t14` P11: bỏ `CPUQuota` thì `cpu.max` biến mất; không trần đĩa thì `io.max` rỗng); `"max"` là không giới hạn; `cpu.max` "q p" là `q*100/p` %; `io.max` chỉ so `rbps`, `wbps` (bỏ qua số thiết bị: systemd áp lên cả đĩa `8:0` dù `/var/www` ở phân vùng `8:1`); MB là 10⁶ byte, MiB là 2²⁰. Slice chưa có cgroup (lồng chưa chạy từ khi máy lên): đạt, ghi rõ lý do. Gói có trần đĩa mà `io.max` rỗng sau khi áp: cảnh báo "trần đĩa không áp được trên loại đĩa này" (LVM, btrfs, ZFS: chưa thử), không có sửa tự động | lỗi (trừ trường hợp đĩa: cảnh báo) | `cage.sync` |
| `cage.state:<domain>` | PHP | (a) gói muốn lồng mà site ở ngoài, hoặc ngược lại: **cảnh báo**; (b) `caged=1` mà thiếu file hàng rào hoặc còn file pool chung; `caged=0` mà còn file hàng rào: **lỗi** | như cột bên | (a) `cage.enable` hoặc `cage.disable`; (b) `caged=1`: `cage.enable` (chạy lại, bước đúng bỏ qua); `caged=0`: `cage.disable` (dọn theo máy) |
| `probe.leftover:tpan` | TPAN | còn lượt thử tấn công chưa dọn đã bắt đầu quá 2 giờ (3.9) | lỗi | `probe.cleanup` |

`cage.state` sinh ra cho site có `caged=1`, gói có `cage=1`, hoặc có file hàng rào trên máy (một lần `Stat` cho mọi site; site thường của máy nâng từ v0.2.0 không có file đó nên không thêm phép kiểm nào).

`checks.Isolated(from, to)`: **không sinh ra khi `from` ở trong lồng** (soát thiết kế điểm 4, `t14` P4: các lệnh `test` của nó chạy dưới user của `from` ở ngoài lồng và mở cửa `/proc/<pid>/root`). Site trong lồng không nhìn thấy site khác là điều `cage.fence` và `cage.runtime` chứng minh. Chiều ngược lại (`from` ngoài lồng, `to` trong lồng) giữ nguyên; phép thử socket xét thêm `CageSocket()` của site đích chỉ khi file đó có (không có thì bỏ lặng lẽ, không đổi bằng chứng và câu kết quả của site ngoài lồng).

Mọi dòng "sửa tay" có lệnh chạy dưới user của site (`runuser -u tp_<slug> -- ...`, như của `wp.checksums`) dựng lúc chạy phép kiểm bằng `system.Describe(sys, cmd)`: site trong lồng ra dạng `systemd-run ...`, để người vận hành làm theo gợi ý không tự mở cửa của điểm 4.

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

**Lấy mẫu.** Một bộ hẹn giờ trong `tpan serve` (như bộ dọn thùng rác): 5 phút một lần ghi một dòng `usage_samples` cho mỗi site `caged` và `active` (cột ở migration 1700: các số của `cage.Sample`, cộng trần đang áp lúc đó `mem_max`, `cpu_quota_pct`, `tasks_max` để luật "đã chạm trần" dùng trần của lúc xảy ra, soát thiết kế điểm 20d). Mỗi giờ gộp các mẫu của những giờ đã trọn vào `usage_hourly`, rồi xoá mẫu cũ hơn 8 ngày và dòng giờ cũ hơn 30 ngày; xoá hẳn một site thì xoá mọi mẫu của domain đó. Hiệu của một bộ đếm giữa hai mẫu: `sau − trước` khi cùng "thế hệ" cgroup; khác thế hệ thì `sau`. Hai mẫu khác thế hệ khi `cpu_usec` giảm (slice bị tạo lại hay máy khởi động lại: `usage_usec` của một slice sống lâu lớn hơn rất nhiều phần một slice mới tích được trong 5 phút; dùng nó chứ không dùng từng bộ đếm riêng, soát thiết kế điểm 20b). Khoảng giữa hai mẫu ngắn hơn 1 phút hay dài hơn 15 phút (đồng hồ bị chỉnh, panel tắt lâu) không dùng để tính phần trăm CPU. Lệnh ẩn `tpan usage --sample` ghi một mẫu ngay, `--rollup` gộp ngay (cho e2e).

**Gợi ý trần** (hàm thuần `suggest`, có bảng test). Dùng mẫu 7 ngày gần nhất của site:

- `n` mẫu; dưới 288 (24 giờ) thì gợi ý vẫn ra nhưng mang cờ `low_data`.
- RAM: đỉnh là lớn nhất của `mem_current` trên mọi mẫu, và của `mem_peak` ở những mẫu mà nó tăng so với mẫu liền trước cùng thế hệ (đỉnh đó xảy ra trong cửa sổ; `memory.peak` tính từ khi slice có nên một đỉnh của ba tháng trước không được kéo gợi ý mãi, soát thiết kế điểm 20a). Gợi ý = `đỉnh × 1,25`, làm tròn lên bội của 32, không dưới 128. Có `oom_kill` trong 7 ngày thì số đo đã bị chính trần cắt: gợi ý không thấp hơn `trần lúc đó × 1,5`, ghi rõ lý do. (`memory.current` gồm cả page cache và OPcache, nên con số nghiêng về an toàn; ghi rõ trong lời giải thích.)
- CPU: phần trăm của từng khoảng = `hiệu cpu_usec / hiệu thời gian`; lấy phân vị 95. Gợi ý = `p95 × 1,5`, làm tròn lên bội của 10, không dưới 25. Thời gian bị bóp vượt 1% của 7 ngày: không thấp hơn `trần lúc đó × 1,5`.
- Số tiến trình: `đỉnh tasks_peak × 2`, làm tròn lên bội của 10, không dưới `max(32, số worker + CageTasksSlack)` (gợi ý không bao giờ là thứ chính `package edit` từ chối); có lần chạm trần thì không thấp hơn `trần × 1,5`. Trên kernel 6.8, lần chạm trần của lệnh tạm (cron) chỉ đếm được khi mẫu rơi đúng lúc lệnh đó chạy (bộ đếm nằm ở cgroup con và mất khi lệnh xong): ghi rõ giới hạn.
- Không gợi ý trần đĩa và số worker (cỡ pool là việc của M21).

Kết quả kèm mọi số đã dùng (`n`, khoảng thời gian, đỉnh, p95, số lần chạm trần, công thức). Gợi ý cho một gói = lớn nhất trong các site của gói. Chỉ là gợi ý: áp bằng `tpan package edit` (một Plan `cage.sync` có xem trước).

### 3.9 Thử tấn công

`tpan isolation probe [--only ngoai|trong] [--dry-run] [--yes] [--json]`, `tpan isolation probe --cleanup`.

Một job `isolation.probe`. Tên hai site thử: `tpan-probe-a-<12 hex>.test` và `tpan-probe-b-<12 hex>.test` (cùng đuôi ngẫu nhiên từ `crypto/rand`; `.test` là tên miền dành riêng). `Sites.PrepareAdd` từ chối mọi domain khớp mẫu này (`domain_reserved`).

Thứ tự (mọi bước nằm trong một job, tức là đã giữ khoá toàn cục):

1. **Ghi sổ trước**: bước đầu tiên của job ghi dòng `probe_runs(id, started_at, state='running', domain_a, domain_b, mode)`, trước khi tạo bất cứ thứ gì (ghi bên trong job chứ không lúc dựng Plan: Plan dọn dựng cùng lúc ở nơi khác chỉ đọc sổ bên trong job của nó, nên không bao giờ thấy một lượt đang chạy; soát thiết kế điểm 9).
2. Bước tạo A rồi B: đúng các bước của `site.add` (WordPress thật), trừ hai bước cron (site thử không cần việc định kỳ, và file cron là thêm một thứ phải dọn). Hai site không có dòng trong bảng `sites`, nên health, ma trận cô lập và mọi danh sách không bao giờ thấy chúng. Job chạy với một `Journal` bọc bỏ qua `RecordFile`, `ForgetFile`: file của site thử không vào `managed_files` (không thì health báo lệch cho file đã bị dọn).
3. Lượt **ngoài lồng**: một bước chạy mọi đường; kết quả ghi vào sổ ngay khi lượt xong (lượt sau hỏng thì kết quả lượt này vẫn còn).
4. Đưa cả hai vào lồng bằng đúng các bước của `cage.enable` (kể cả `CageReap`) với trần cố định của phép thử: RAM 160 MB, CPU 100%, 60 tiến trình, 3 worker. Máy không dựng được lồng (điều kiện đầu của `cage.enable`: cgroup v1, systemd cũ, ví dụ container LXC): lượt trong lồng ghi `skipped` kèm lý do, không phải lỗi.
5. Lượt **trong lồng**: chạy lại mọi đường; ghi sổ ngay.
6. Dọn: `probeCleanupSteps` cho B rồi A (gỡ vhost, tắt và gỡ lồng, gỡ pool, xoá DB và user DB, xoá thư mục site và log, xoá user Linux, `systemctl reset-failed 'tpan-cage-<slug>-*'`, gỡ thư mục socket). Mọi bước tự bỏ qua khi thứ đó không còn.
7. Ghi kết quả và `state='done'`, `cleaned=1`.

`--only ngoai` bỏ bước 4 và 5; `--only trong` bỏ bước 3. `--only trong` trên máy không dựng được lồng bị từ chối ngay ở Prepare (mã `cage_cgroup_v1` hay `cage_systemd_old`), thay vì tạo rồi xoá hai site mà mọi ô đều "bỏ qua".

**Dọn trong mọi trường hợp (bất biến 5).** Job thử kết thúc mà không thành công (bước hỏng, kể cả bước hỏng giữa `Apply` mà engine không hoàn tác được) thì `ProbeJob.Execute` chạy ngay Plan `probe.cleanup` cho đúng hai tên đó, trong cùng tiến trình. Panel bị tắt giữa lúc chạy: dòng `running` còn trong sổ; `probe.leftover` báo lỗi khi dòng đã quá 2 giờ; `tpan isolation probe --cleanup`, lượt thử kế tiếp và bộ hẹn giờ của `tpan serve` (chỉ khi khoá toàn cục đang trống, như bộ dọn thùng rác) chạy Plan `probe.cleanup`. Plan đó đọc sổ **bên trong job** (đã giữ khoá): lúc ấy không lượt thử nào đang chạy, nên mọi dòng chưa dọn đều là sót. Plan dọn **chỉ nhận tên vừa khớp `site.IsProbeDomain` vừa có trong sổ**; mọi đường dẫn nó chạm đều suy từ `site.LayoutOf(tên đó)`, và mọi chỗ ghi file thử so bằng dấu bằng với `site.LayoutOf(tên trong sổ).Public` (không so tiền tố: site thật `tpan-probe-shop.example.com` có cùng tiền tố). `probe_running`: Prepare từ chối khi sổ còn dòng chưa dọn (kèm lệnh `tpan isolation probe --cleanup`).

**L8**: file vhost lạ được tạo, kiểm bằng `checks.ServerNameUnique` và gỡ trong **cùng một bước** (Undo của bước gỡ nó), nội dung tối thiểu (chỉ tên miền của A, không `default_server`), tên `aaa-tpan-probe-<đuôi>.conf`.

**Các đường.** Mỗi đường là một file PHP nhỏ đặt vào `public` của A (hoặc một lệnh), gọi qua `curl --resolve <A>:80:127.0.0.1`; kết quả chỉ in `CO`/`KHONG`, tên user và số đếm, không in nội dung hay tên của site thật.

| Mã | Đường | Ngoài lồng | Trong lồng |
|---|---|---|---|
| L1 | PHP của A `include` `wp-config.php` của B qua OPcache | bị chặn | bị chặn |
| L2 | `opcache_get_status()` từ A | bị chặn | bị chặn |
| L3, L4, L9 | `.htaccess` của A (chỉ Apache; nginx ghi "không thuộc stack") | bị chặn | bị chặn |
| L5 | symlink của A sang file của B (chỉ nginx) | bị chặn | bị chặn |
| L6 | A đọc `access.log` của B | bị chặn | bị chặn |
| L7 | PHP của A đếm tiến trình của user khác | bị chặn | bị chặn |
| L8 | vhost lạ trùng tên miền của A: `checks.ServerNameUnique` gọi với Layout của hai site thử truyền thẳng vào (chúng không có trong bảng `sites`) phải chỉ ra đúng file lạ; file lạ mang tên `aaa-tpan-probe-<đuôi>.conf` và nằm trong danh sách dọn | bị chặn (phát hiện) | như ngoài lồng (không chạy lại) |
| C1 | A gọi shell: thư mục của B có trong `/var/www` không; `/var/www` có mấy mục | **lộ (cần lồng)** | bị chặn |
| C2 | A ghi một file vào `/tmp`; B có thấy không | **lộ (cần lồng)** | bị chặn |
| C3 | A cấp phát 320 MB | **lộ (cần lồng)** | bị chặn: tiến trình bị giết, `oom_kill` tăng 1, B vẫn 200 |
| C4 | A đọc ra tên user `tp_*` khác qua `getent passwd` | **lộ (cần lồng)** | bị chặn |
| C5 | A đọc `/proc/self/net/unix`: có socket mang tên (slug) của B không | **lộ (cần lồng)** (socket của pool chung mang slug) | bị chặn (socket của lồng mang phần băm, 3.2) |

Các đường này chạy với site thử ở chế độ `.htaccess` mặc định (`an-toan`). Site Apache ở `day-du` vẫn chuyển được request sang socket của lồng khác bằng `SetHandler` (soát thiết kế điểm 14): lồng không chặn đường đó; cảnh báo sẵn có của `day-du` nói thêm điều này. Kết nối TCP của mọi site (`/proc/self/net/tcp`) và tên socket của site ngoài lồng vẫn đọc được từ trong lồng vì lồng dùng chung mạng của máy (mục 5).

Khác đặc tả ở một chỗ: "không thấy tiến trình của ai khác" đã được M14 chặn cả ở ngoài lồng (`ProtectProc` của dịch vụ chung), nên nó là L7 và ra "bị chặn" ở cả hai cột, không phải "lộ (cần lồng)". C3 ngoài lồng chỉ chạy khi máy còn ít nhất 1 GB RAM trống; không thì ghi "bỏ qua" kèm lý do. Phép thử không sinh tiến trình hàng loạt và không đốt CPU ở ngoài lồng: hai việc đó làm hại máy thật.

Trạng thái của một ô: `blocked`, `exposed`, `needs_cage`, `skipped`, `na`. Lệnh thoát mã 1 khi có ô `exposed` (đường lẽ ra phải bị chặn mà lộ).

### 3.10 CLI và API

```
tpan cage status [<domain>] [--json]
tpan cage enable  <domain> | --all            [--dry-run] [--yes] [--json]   (--all: mọi site gói muốn lồng mà chưa có lồng)
tpan cage disable <domain> | --all | --stale  [--dry-run] [--yes] [--json]   (--all: mọi site có lồng; --stale: chỉ site mà gói không còn muốn lồng)
tpan package add|edit <tên> ... [--cage on|off] [--cpu N] [--ram-mb N] [--tasks N] [--io-read-mb N] [--io-write-mb N] [--php-workers N]
tpan package set                     (tên khác của edit)
tpan usage [<domain>] [--suggest] [--json]
tpan isolation probe [--only ngoai|trong] [--cleanup] [--dry-run] [--yes] [--json]
```

`package edit` đổi trần của gói: dòng gói được ghi trước, rồi một job `cage.sync` cho các site trong lồng dùng gói đó (`Cages.PrepareEditPackage`, mục 3.6; `--dry-run` in thay đổi của gói và bản xem trước của job, không ghi gì). Gói không có site nào trong lồng: chỉ ghi dòng gói như trước M17, không có job. `user edit --package` cũng vậy cho site của user đó (`Cages.PrepareUserEdit`). Hình JSON của `tpan cage status --json`, `tpan usage --json`, `tpan usage --suggest --json`, `tpan isolation probe --json` là đúng các kiểu `core.CageRow`, `core.SiteUsage`, `core.SuggestionReport`, `core.ProbeReport` của hợp đồng (`internal/core/types_m17.go`), và của API là các kiểu trong `internal/server/types_m17.go`.

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
| `POST /api/v1/packages`, `POST /api/v1/packages/{name}` | admin (như cũ); nhận thêm bảy trường (ở route sửa: vắng mặt là giữ nguyên, để giao diện cũ không xoá trần khi sửa bốn giới hạn cũ); có job thì trả `202 JobStarted` (`job_id` của job `cage.sync`) thay cho `200` |
| `POST /api/v1/users/{name}` | admin (như cũ); đổi gói làm đổi trần của site trong lồng thì trả `202 JobStarted` (`job_id` của job `cage.sync`) |

Mã lỗi mới: `cage_not_in_package`, `cage_not_caged`, `cage_cgroup_v1`, `cage_systemd_old`, `cage_active`, `package_limit_invalid`, `package_limits_need_cage`, `domain_reserved`, `probe_running`, `usage_not_caged` (404 cho site ngoài lồng ở route usage của site: không có cgroup riêng để đọc), `cage_error` (trong `cage_error` của kết quả tạo site và apply: job `cage.enable` sau khi tạo site hỏng vì lỗi không phải lời từ chối; site vẫn được tạo, ở ngoài lồng).

## 4. Bất biến

Mỗi câu gắn với test. `U` là unit test (tên hàm), `E` là khẳng định trong `scripts/e2e-m17.sh`.

| # | Luôn đúng | Test |
|---|---|---|
| 1 | Site ngoài lồng không đổi hành vi khi máy nâng cấp hoặc khi site khác vào lồng | U golden cũ của `internal/templates` (không đổi) và `TestSiteVhostCagedMatchesSiteVhost` (vhost trong lồng chỉ khác đúng chuỗi socket); U `TestNangCapTuBan1400LenCage` (migration giữ nguyên dòng cũ); E "nâng cấp: sha256 mọi file cấu hình trước và sau bằng nhau"; E "B ngoài lồng: sha256 pool và vhost không đổi sau khi A vào lồng" |
| 2 | Vào lồng rồi ra lồng: file cấu hình của site về đúng từng byte | U `TestCageEnableDisableTraLaiTungByte` (FakeSystem, so `Snapshot`); E "sha256 pool, vhost, cron, wp-config, .htaccess trước khi vào bằng sau khi ra" |
| 3 | Mỗi site đúng một nơi phục vụ PHP; không hai tiến trình chính cùng một socket | U `TestSitePHPStepsTrongLong`, `TestSitePHPStepsNgoaiLong`, `TestKhongGoiPoolChungNgoaiHamChon` (test tĩnh); U `TestCageState`; E "trong lồng: không có file pool chung, đúng một `php-fpm: master` của user đó; ngoài lồng: không có unit `tpan-cage-<slug>-*`" |
| 4 | Trần ghi trong gói bằng trần trên cgroup; lệch thì health báo và sửa được | U `TestCageLimitsPass`, `TestCageLimitsFail`, `TestCageLimitsUnits`; E "sửa tay file slice rồi `daemon-reload`: `cage.limits` lỗi; `tpan health --fix` đưa về; `package edit` đổi RAM: `memory.max` đổi, `MainPID` giữ" |
| 5 | Thử tấn công luôn dọn sạch, kể cả khi hỏng giữa chừng hoặc panel bị tắt | U `TestProbeHongGiuaChungVanDon`; U `TestProbeCleanupChiNhanTenTrongSo`; U `TestProbeKhongGhiManagedFiles`; E "`kill -9` tpan giữa lượt thử, rồi `--cleanup`: không còn user, thư mục, DB, vhost, unit, cron, file `/tmp` nào mang tên phép thử; không dòng nào của `managed_files`, `check_results`, `incidents` nhắc `tpan-probe-`" |
| 6 | Con số hiện ra bằng con số đọc từ cgroup | U `TestReadUsageDangChay`, `TestReadUsageChamTran`, `TestReadUsageCoIO` (file mẫu cgroup chép từ máy ảo); E "sau một lần cấp phát vượt trần: `tpan usage --json` có `oom_kills` tăng đúng 1 và `mem_peak` bằng `memory.peak` đọc tay" |
| 7 | User thường không tự nâng trần, không tự ra khỏi lồng | U `TestPhanQuyenMoiRoute` (phủ route mới); U `TestMoiThaoTacCuaChuGiuLong` (đổi PHP, khôi phục, chạy lại, sao lưu trên site `caged` đều ra Plan của lồng); E "user chủ gọi `POST /sites/{d}/cage` và `POST /packages/...`: 403; sau đổi PHP, tạm khoá rồi mở, khôi phục, thùng rác rồi khôi phục, chuyển stack: `caged` vẫn 1 và `cage.limits` đạt" |
| 8 (thêm) | Với site trong lồng, không lệnh nào tpan chạy dưới user của site nằm ngoài hàng rào, và sau khi vào lồng không còn tiến trình nào cùng uid ở ngoài slice | U `TestRunCoHangRaoArgv` (argv đủ `-p User=`, `-p Group=`, `-p NoNewPrivileges=yes`); U `TestRunStatLoiKhongChayGi`; U `TestCageFenceMissingFenceRunsNothing`; U `TestMoiThaoTacCuaChuGiuLong` (mọi thao tác trên site `caged=1` ra Plan của lồng; khôi phục: hàng rào có trước mọi bước chạy `wp`; thùng rác: gỡ cron trước khi gỡ hàng rào); U `TestIsolatedKhongSinhTuSiteTrongLong`; E "sau `cage enable`: không tiến trình nào của user A ngoài `tpan-<slug>.slice`; trong một vòng `tpan health` cũng không" |
| 9 (thêm) | Lệnh trong lồng bị giết không bao giờ bị đọc thành một mã thoát có nghĩa | U `TestRunBiDung` (oom-kill, timeout, signal ra `KilledError`, không phải `ExitError`); U `TestRunThamSoKhoNguyenVen`, `TestRunCoHangRaoArgv` (có `--expand-environment=no`, `--service-type=oneshot`) |
| 10 (thêm) | Plan dọn của phép thử không chạm thứ gì ngoài tên nó đã ghi sổ | U `TestProbeCleanupTuChoiTenLa` (tên của site thật, tên khớp mẫu nhưng không có trong sổ) |
| 11 (thêm) | Chính sách của admin (vào lồng, trần, gói) không phụ thuộc vào thứ mã của site điều khiển | U `TestHaGoiKhiSiteTra500` (FakeSystem: HTTP trả 500, tiến trình chính chết: `user edit --package` hạ gói vẫn ghi gói và áp trần, job `cage.sync` thành công); U `TestCagePolicyKhongPhuThuocHTTP`, `TestPolicyVerifyBang` |
| 12 (thêm) | Hoàn tác không bao giờ để site trong lồng kẹt ở `failed` mà vẫn báo "đã hoàn tác" | U `TestCageFilesUndoSauKhiFailed` (dịch vụ và socket ở `failed`: đường trả lại gọi `reset-failed`, `start` socket, rồi `restart` dịch vụ vì Apply đã khởi động lại nó với file mới; không được thì `undo_failed`); U `TestCageFilesUndoKhoiDongLaiBanCu`; E "hạ trần RAM của gói xuống dưới mức A đang dùng rồi nâng lại: A tự trả 200 trong 10 giây mà không cần lệnh nào" |

## 5. Mô hình đe doạ

**Ai tấn công.** (K1) Kẻ đã chạy được PHP dưới user của site A trong lồng. (K2) User thường của panel, chủ của A. (K3) Thứ do site kiểm soát mà tiến trình root của panel chạm vào.

| # | Đi đường nào | Chặn bằng | Chứng minh |
|---|---|---|---|
| K1.1 | Đọc file, tên, log của site khác | hàng rào: `/var`, `/run`, `/etc/tpan` là tmpfs chỉ gắn lại đồ của mình; `passwd` riêng; các `InaccessiblePaths`; socket của lồng mang phần băm | `cage.fence`; probe C1, C4, C5, L6; E "liệt kê `/var/www` chỉ thấy A" |
| K1.1b | Mở `/proc/<pid>/root` của một tiến trình cùng uid đang chạy ngoài lồng, ra gốc thật của máy và giữ fd (đã thử: `t14` P4, đọc được `/var/www` thật và ghi được vào `/tmp` của máy) | không có tiến trình như vậy: không ngoại lệ nào chạy lệnh của site ngoài hàng rào (3.4); `Isolated` không sinh từ site trong lồng; `CageReap` ở `cage.enable` trước khi mã của site chạy trong lồng; `cage.runtime` báo khi có; dòng "sửa tay" in lệnh vào lồng. Còn hở: người vận hành tự gõ `runuser -u tp_<slug> ...` bằng tay (tài liệu giải thích nói rõ) | bất biến 8 |
| K1.1c | Đọc qua mạng dùng chung: `/proc/self/net/tcp` (kết nối TCP của mọi site, có IP khách), `/proc/self/net/unix` (tên socket của site **ngoài** lồng mang slug) | không chặn được khi lồng dùng chung mạng của máy (`PrivateNetwork` làm hỏng WordPress, `t2b`); tên socket của site trong lồng không mang slug | probe C5 (nói thật: lộ ở ngoài lồng) |
| K1.2 | Ghi ra ngoài thư mục site (`/tmp` chung, `/dev/shm`, `/var/lib/php/sessions`) | `PrivateTmp`, tmpfs `/dev/shm`, `ProtectSystem=strict` | probe C2; `t1` |
| K1.3 | Nhìn và gửi tín hiệu cho tiến trình khác | `ProtectProc=invisible`, `ProcSubset=pid`, khác uid | probe L7; `cage.fence` |
| K1.4 | Lấy thêm quyền: setuid (`sudo`, `su`, `crontab`), capability, namespace mới | `NoNewPrivileges`, tập capability rỗng, `RestrictSUIDSGID`, `RestrictNamespaces`, lọc syscall | `t1` (`CapEff=0`, `NoNewPrivs=1`); E "`crontab -l` trong lồng không chạy được" |
| K1.5 | Thoát trần: chuyển cgroup, đặt việc hẹn giờ chạy ngoài slice, nói chuyện với systemd | `/sys/fs/cgroup` và `/run` (D-Bus, socket của systemd) không thấy; không có `/var/spool`; mọi lệnh tpan chạy thay cho site nằm trong slice | E "`tpan cron run` của site: cgroup của tiến trình `wp` nằm dưới slice của site"; soát cuối |
| K1.6 | Ngốn RAM, CPU, tiến trình, tốc độ đĩa **trong tiến trình của chính site** để làm chết site khác | `MemoryMax` + `MemorySwapMax=0`, `CPUQuota`, `TasksMax`, `IO*BandwidthMax` (thiết bị chứa `/var/www`) ở slice | E bốn phép quá tải, B không rớt request nào. Không phủ (ghi rõ, soát thiết kế điểm 12 và 23): việc site đẩy sang dịch vụ chung (truy vấn nặng trong `mariadb`, TLS và file tĩnh trong web server), dung lượng đĩa (đầy đĩa qua thư mục site hay `/tmp` riêng), băng thông mạng, và tốc độ ghi vào thiết bị khác `/var/www` |
| K1.7 | Tự làm sập lồng liên tục | `RestartSec=5s` (không bao giờ chạm giới hạn khởi động), health `cage.socket`, `cage.runtime` và sửa tự động; chỉ site đó chịu | `t4`, `t14` P1; U của `CageSocket` |
| K1.8 | Chạy mã trong tiến trình chính của FPM, hay giết nó (cùng uid) | không có gì để lấy: tiến trình chính không giữ quyền hay bí mật nào ngoài của chính site; giết nó chỉ làm hỏng site đó; không Plan chính sách nào của admin chờ tiến trình này (3.6) | cấu trúc (2.1); bất biến 11 |
| K1.9 | Đánh sang socket của lồng khác | PHP của A: socket `www-data 0660`, `/run` trong lồng không có `tpan-cage`. Apache: ở chế độ `.htaccess` `an-toan` và `tat` không có `SetHandler`; ở `day-du` **không chặn** (Apache chạy bằng `www-data` nối được mọi socket của lồng): cảnh báo của `day-du` và bản xem trước của `cage.enable` nói rõ | `Isolated` (phép socket); probe L4 (chế độ mặc định) |
| K2.4 | Chặn việc admin hạ trần hay đổi gói, hoặc đứng ngoài lồng, bằng cách trả mã HTTP lạ hay giết tiến trình chính | Plan chính sách không chờ thứ gì site điều khiển (3.6) | bất biến 11 |
| K2.1 | Gọi API đưa site ra khỏi lồng hoặc sửa gói | route `admin`; mặc định là cấm | `TestPhanQuyenMoiRoute` |
| K2.2 | Dùng thao tác của chủ (đổi PHP, khôi phục, chạy lại) để rơi về pool chung | một hàm `sitePHPSteps` cho mọi Plan; `cage.state` báo lỗi nếu hai nơi cùng có | bất biến 7, 3 |
| K2.3 | Tạo domain trùng tên phép thử để bị dọn nhầm hoặc chiếm chỗ | `domain_reserved`; đuôi ngẫu nhiên 48 bit; Plan dọn đòi tên có trong sổ | bất biến 10 |

**Panel chạy bằng root đụng vào thứ do site kiểm soát (K3).**

- File của lồng nằm ở `/etc/systemd/system` và `/etc/tpan/cage`: thư mục của root, site không ghi được. Không Step nào của M17 ghi file bằng root vào thư mục của một site thật.
- Nguồn của các lệnh gắn (`BindPaths`) là `/var/www/<domain>` và `/var/log/tpan/sites/<domain>`. Site sở hữu thư mục thứ nhất nhưng không sửa được mục cha `/var/www` (của root), nên không thay được chính thư mục đó bằng symlink; bên trong nó thì site làm gì cũng chỉ là đồ của site. Thư mục log thuộc root.
- Mọi lệnh đọc nội dung do site viết (`wp`, `find`, `head` của phép quét `.htaccess`) chạy bằng user của site, giờ thêm hàng rào. Root không nạp mã PHP của site ở đâu cả (như trước).
- `cage.Wrap`: tên unit ghép từ slug đã kiểm dạng và 16 hex ngẫu nhiên; lệnh và tham số đi sau `--` dưới dạng argv; `--expand-environment=no`; `User=` có trên dòng lệnh. `Stat` hàng rào lỗi thì không chạy. Đua giữa `Stat` và lúc chạy (lồng vừa bị gỡ): lệnh chạy bằng user của site không có rào, tức đúng mức của v0.2.0, không bao giờ bằng root.
- Số liệu đọc từ cgroupfs (file của nhân, site không ghi được vì không thấy và không có quyền). Phân tích từng dòng `khoá giá-trị`; dòng lạ thì bỏ, không panic.
- Phép thử tấn công ghi file thử bằng root vào `public` của hai site thử: hai site đó do chính job tạo, không ai khác điều khiển. Trước khi ghi, bước kiểm thư mục đích bằng đúng `site.LayoutOf(tên trong sổ).Public` (so bằng dấu bằng) và thuộc đúng user của site thử. File PHP thử không nhận tham số từ request.
- `CageReap` đọc `/proc/<pid>/status` và `/proc/<pid>/cgroup` bằng root rồi gửi `SIGKILL`: pid có thể được dùng lại giữa lúc đọc và lúc giết; đọc lại `status` ngay trước `kill` (cùng uid và vẫn ngoài slice), và chỉ giết pid của đúng uid đó, nên tệ nhất là giết một tiến trình khác của chính site.
- Lời lỗi của `CageSocket` mang dòng journal do PHP của site ghi: bỏ ký tự điều khiển và ghi rõ là output của site trước khi tới log job, giao diện hay bộ não AI.
- Plan dọn xoá cây thư mục bằng `steps.RemoveTree` (đã có giới hạn `Under`) với đường dẫn suy từ tên trong sổ.

## 6. Nâng cấp và lùi

**Từ v0.2.0 lên.** Migration 1700 chỉ thêm cột có DEFAULT và bảng mới. Không file nào trên máy được ghi lại: template của pool và vhost ngoài lồng không đổi (golden cũ giữ nguyên), không có bước nào chạy lúc nâng. Health không thêm phép kiểm nào cho site khi không gói nào bật lồng. Sau khi nâng, mọi site ở ngoài lồng như cũ; muốn dùng thì `tpan package edit <gói> --cage on ...` rồi `tpan cage enable <domain>`.

**Lùi.**

- Đưa một site ra khỏi lồng: `tpan cage disable`, bất kỳ lúc nào, trả lại đúng từng byte.
- Lùi file chạy về v0.2.0 (`tpan self-update --rollback`): bản cũ không biết gì về lồng (health của nó sẽ báo thiếu socket, `site add` chạy lại sẽ ghi lại pool chung trong khi vhost trỏ vào lồng) và không biết dọn phép thử. Vì vậy lõi **từ chối lùi** (`cage_active`) khi còn site `caged` đang `active`, còn file `tpan-cage-*` trong `/etc/systemd/system`, hoặc còn lượt thử chưa dọn trong sổ; lời từ chối liệt kê từng thứ kèm lệnh chạy được ngay (`sudo tpan cage disable --all --yes`, `sudo tpan isolation probe --cleanup --yes`; site trong lồng đang tạm khoá: `sudo tpan site unsuspend <domain> --yes` rồi `sudo tpan cage disable <domain> --yes`, vì `cage disable` từ chối site tạm khoá). Site `caged` trong thùng rác không chặn: file của lồng đã gỡ lúc đưa vào thùng rác (còn sót thì phần đọc thư mục unit bắt). Ngoài ra, như từ M13: bản cũ từ chối mở `tpan.db` đã ở bản 1700; muốn chạy bản cũ phải chép lại bản sao `tpan.db` từ trước khi nâng. Không đổi điều đó ở mốc này.
- Gỡ hẳn tpan khỏi máy: không thuộc mốc này; `cage disable --all` trước là đủ để máy không còn unit nào của tpan cho site.

## 7. Kế hoạch kiểm thử

**Unit** (Mac, `make check`): golden cho năm template của lồng và bốn vhost bản trong lồng; bốn trường hợp chuẩn của mỗi Step (máy sạch → `Apply` → `Check`; chạy lại không lệnh nào; `Apply` rồi `Undo` thì `Snapshot` về như cũ; lệnh lỗi có ngữ cảnh); `cage.Wrap` với FakeSystem (bảng: không `AsUser`, user lạ, `AsUser` kèm `Env`, không có hàng rào, có hàng rào, `Stat` lỗi, mã thoát thường, mã 226 không có thời điểm bắt đầu, `oom-kill`, `timeout`, `core-dump`, không tạo được unit, ctx huỷ); `ReadUsage` với file cgroup mẫu chép từ máy ảo (cả kernel 6.8 lẫn dạng có `pids.events.local`); `suggest` (bảng: ít mẫu, có `oom_kill`, bộ đếm về 0 giữa chừng, bị bóp CPU); mọi Plan ở 3.6 với FakeSystem; phép kiểm; validate gói; store (migration, mẫu, gộp giờ, xoá cũ, sổ phép thử); `TestPhanQuyenMoiRoute` phủ route mới; các test ở mục 4.

**E2E bước 20** (`scripts/e2e-m17.sh`, chạy qua `make e2e-m17 P=<phần>`, mỗi phần từ máy sạch trên `tpan-m17`, log ở `docs/bang-chung/M17*.log`):

| Phần | Khẳng định chính (kết quả mong đợi) |
|---|---|
| `nginx` | stack nginx; gói `long` (RAM 200, CPU 50, 40 tiến trình, 3 worker, lồng bật); user `khach`; hai site A, B của `khach` tự vào lồng sau khi tạo. Từ PHP của A: `ls /var/www` đúng một tên; `/tmp` rỗng; 0 tiến trình của user khác. A cấp phát 400 MB: 502, A trả 200 lại trong 2 giây (thiết kế: request kế tiếp), B 0 request hỏng trên 60. Vòng CPU với máy bận: phần CPU nhận trong 45% đến 52% (trần 50%); không bao giờ trên 52%. Sinh 200 tiến trình: `pids.current` ≤ 40, `pids.peak` = 40, bộ đếm chạm trần ≥ 1, B vẫn 200. `tpan usage --json` và API: `oom_kills` = 1, `mem_peak` = `memory.peak` đọc tay, `tasks_peak` = 40. `tpan isolation probe --json`: mọi ô "trong lồng" `blocked`; ngoài lồng L1, L2, L5, L6, L7, L8 `blocked`, C1 đến C5 `needs_cage`; mã thoát 0; không còn gì mang tên `tpan-probe-`. `kill -9` giữa một lượt thử rồi `--cleanup`: sạch. Với A trong lồng: đổi sang PHP 8.4 và về lại; tạm khoá (403, socket `disabled`) rồi mở (200); sao lưu, sửa nội dung, khôi phục; thùng rác rồi khôi phục; `stack switch --to apache` rồi về nginx: sau mỗi việc A 200, `caged` = 1, `cage.limits` đạt, health không có lỗi. `package edit` đổi RAM 200 → 300: `memory.max` = 314572800, `MainPID` không đổi. Hạ RAM của gói xuống 64 (dưới mức A đang dùng) rồi nâng lại 200: A tự trả 200 trong 10 giây, không lệnh nào khác (bất biến 12). Site trả 500 cho `127.0.0.1` (file PHP thử trong `wp-content/mu-plugins`) không chặn được `user edit --package` hạ gói: gói đổi, `memory.max` đổi (bất biến 11). Xin SSL (pebble, như e2e M14) cho A trong lồng: đạt, `caged` vẫn 1. Sau `cage enable` và trong một vòng `tpan health`: không tiến trình nào của user A ngoài slice (bất biến 8). Vòng `curl` trong lúc `cage enable` và `cage disable`: ghi số request hỏng và thời gian gián đoạn (mong đợi dưới 1 giây khi vào lồng, 0 khi ra lồng). Ra lồng: sha256 năm file về như trước. Khởi động lại máy ảo: A, B 200; site tạm khoá vẫn 403. Phân quyền: user chủ bị 403 ở route của admin, 404 ở site của người khác |
| `apache` | như trên với stack Apache (L3, L4, L9 thay L5), bỏ phần khởi động lại máy |
| `nang-cap` | cài gói v0.2.0 thật, stack, hai site, ghi sha256 mọi file dưới `/etc/nginx`, `/etc/php`, `/etc/cron.d`, `/etc/systemd/system`; lên bản của nhánh; sha256 không đổi, health như trước, không unit `tpan-cage-*`; bật lồng cho gói: hai site 200 trong lồng; tắt lồng: sha256 về đúng như trước; `self-update --rollback` khi còn site trong lồng bị từ chối `cage_active` |
| hồi quy | `scripts/e2e-m13.sh chinh`, `scripts/e2e-m14.sh chinh`, `scripts/e2e-m9.sh`: vẫn 0 không đạt |

**Soát cuối**: một thợ `opus`, `effort: max` (luật 8 tạm thời: hạn mức `fable` đã hết tới 25/11), chưa đọc mã của mốc, trên máy ảo: thử thoát lồng, thử nâng trần bằng tài khoản user thường, thử làm hỏng lượt thử tấn công giữa chừng. Phát hiện và cách sửa ghi vào `docs/thay-doi/M17.md`.

## 8. Bảng chia việc

Luật 8 của `agent-dev/LO-TRINH.md` (bản 02:25 ngày 21/11) cộng thay đổi tạm thời lúc hạn mức `fable` hết (sáng 21/11, tới 25/11): phần nhạy cảm dùng `opus`, `effort: high`; phần không nhạy cảm dùng `sonnet`; thợ soát dùng `opus`, `effort: max`; không gọi `fable`. Mỗi đợt tối đa 4 thợ, không hai thợ chung file. Phiên chính đọc diff của thợ `sonnet` trước khi nhận, chạy `make check`, commit, đẩy `moc/M17` ở cuối mỗi đợt (dừng giữa chừng vì hết hạn mức thì `dev` không nhận gì làm dở, nhánh vẫn có mọi đợt đã xanh).

Thứ tự đợt chọn để mỗi đợt xong là một trạng thái xanh dùng được, và phần cắt được (lấy mẫu, gợi ý) không chặn phần chính: lồng và trần, rồi ghép với thao tác cũ và số liệu đọc ngay, rồi thử tấn công.

| Đợt | Phần | Vùng file | Thợ | Loại | Model |
|---|---|---|---|---|---|
| 0 | Hợp đồng: kiểu, chữ ký (thân tạm giữ hành vi cũ hoặc trả lỗi "chưa viết") của `internal/cage`, `templates`, `steps`, `checks`, `core` (cage, usage, probe: kể cả kiểu JSON của CLI và API), `server`; migration 1700 (đủ cột của mẫu và sổ phép thử); các dòng route kèm luật (handler tạm trả 501); `system.Describer`; các hàm tên của `site.Layout`, mẫu tên site thử và từ chối `domain_reserved` ở `Sites.PrepareAdd`; `siteChecks` nhận thêm trần của lồng (mọi chỗ gọi truyền `nil`); `health.fileOwner` nhận đường dẫn của lồng | các file `types*.go`, `internal/site/cage.go`, `internal/system/types.go`, `internal/store/migrations/1700_cage.sql`, `internal/server/routes.go`, `internal/core/cage.go` (chữ ký), `internal/core/site.go` (chỉ chữ ký `siteChecks`), `internal/health/collect.go` | phiên chính | | |
| 1 | Template của lồng và vhost bản trong lồng, golden | `internal/templates/cage.go`, `cage_test.go`, `files/cage-*.tmpl`, `testdata/cage-*`; hàm vhost bản trong lồng thêm ở `site.go`, `ssl.go` của gói này | `tho-loi` | nhạy cảm (sinh unit, vhost, pool) | opus |
| 1 | `cage.Wrap`, `cage.ReadUsage`; engine và checks in lệnh thật qua `system.Describe` | `internal/cage/` (dùng mẫu ở `docs/bang-chung/M17-nghien-cuu/cgroup-mau/`), `internal/engine/engine.go` (chỉ `recSys.Run`), `internal/checks/run.go` (chỉ `execCmd`), `cmd/tpan/main.go` (bọc `NewLocal`) | `tho-loi` | nhạy cảm (chạy lệnh thay cho site) | opus |
| 1 | Store: cột mới của `sites`, `packages`; bảng mẫu, gộp giờ, sổ phép thử | `internal/store/sites.go`, `users.go` (chỉ phần gói), `usage.go`, `probe.go` và test | `tho-phu` | không | sonnet |
| 1 | Script e2e bước 20 (viết theo hợp đồng CLI và JSON; chạy ở đợt 4) | `scripts/e2e-m17.sh`, `scripts/e2e-m17/` | `tho-phu` | không (test) | sonnet |
| 2 | Step `CageFiles`, `CageSocket`, `CageReap`, `CageRestart`, vhost bản trong lồng | `internal/steps/cage.go`, `cage_test.go`; hàm thêm ở `site_web.go`, `ssl.go` của gói này | `tho-loi` | nhạy cảm | opus |
| 2 | Phép kiểm của lồng (`cage.socket`, `cage.fence`, `cage.limits`, `cage.state`, `probe.leftover`), `SiteChecksCaged`, `Isolated` thêm `Bare` và socket của lồng, `DriftStage` | `internal/checks/cage.go`, `cage_test.go`, `site.go` (chỉ `Isolated` và `SiteChecks*`), `health.go` (chỉ `DriftStage`) | `tho-phu` | không | sonnet |
| 2 | Số liệu: đọc ngay, lấy mẫu, gộp giờ, gợi ý | `internal/core/usage.go`, `usage_test.go` | `tho-phu` | không | sonnet |
| 2 | Kiểm trần của gói; `route explain` cho site trong lồng | `internal/core/users.go` (chỉ `checkPackageLimits` và chỗ gọi), `route_m14.go` và test | `tho-phu` | không | sonnet |
| 3 | Plan của lồng, sửa tự động, chọn bộ phép kiểm | `internal/core/cage.go`, `cage_test.go`, `fix.go`, `health.go` | `tho-loi` | nhạy cảm | opus |
| 3 | Ghép lồng vào thao tác đã có (kể cả `ssl.go` và chỗ gọi `siteChecks` trong `users.go`) | `internal/core/ssl.go`, `site.go`, `suspend.go`, `trash.go`, `php.go`, `backup.go`, `apply.go`, `selfupdate.go`, `dbops.go`, `users.go` (chỉ chỗ gọi `siteChecks`), `security.go` hoặc `harden_m14.go` (chỉ bước `try-restart` lồng) và test | `tho-loi` | nhạy cảm | opus |
| 3 | Thử tấn công | `internal/core/probe.go`, `probe_test.go`, `internal/core/probe/` (file PHP thử, nhúng bằng `embed`) | `tho-loi` | nhạy cảm (root ghi file, tạo và xoá user) | opus |
| 3 | API và CLI: lồng, số liệu, gói, thử tấn công; bộ hẹn giờ lấy mẫu và dọn phép thử | `internal/server/api_m17.go`, `api_m17_test.go`, `usage_timer.go`, `api_m13.go` (chỉ handler sửa gói và sửa user), `cmd/tpan/cage.go`, `usage.go`, `isolation.go`, `package.go`, `user.go` (chỉ `edit`) và test | `tho-phu` | không (luật của route đã chốt ở đợt 0) | sonnet |
| 4 | Chạy e2e bước 20 hai stack, nâng cấp, hồi quy M13, M14, M9; sửa lỗi gốc | máy ảo `tpan-m17`, `scripts/e2e-m17*`, `docs/bang-chung/M17*.log`, mã ở bất kỳ gói nào (kèm test) | `tho-may-ao` | | opus |
| 5 | Soát cuối trên máy ảo (sau đợt 4) | chỉ đọc mã; ghi `docs/thay-doi/M17.md` | thợ soát | | opus, max |
| 5 | Giải thích (song song với soát cuối, không dùng máy ảo) | `docs/giai-thich/17-m17-long-theo-site.md` | `tho-viet` | không | sonnet |

Mục phải đọc trong file này: mọi đợt đọc mục 1 và 3.1, 3.2; đợt 1 thêm 2.1 đến 2.4, 2.8, 3.3, 3.4, 3.8 (phần đọc cgroup), 3.10 (cho script e2e), 5, 7; đợt 2 thêm 3.5, 3.7, 3.8; đợt 3 thêm 3.5 đến 3.10, 4, 5, 6; đợt 4 đọc mục 7 và 4; đợt 5 đọc cả file.

## Soát thiết kế

(Thợ soát ghi phát hiện ở đây; phiên chính ghi cách đã sửa ngay dưới từng phát hiện.)

Thợ soát: opus, effort max, ngày 21/11/2026. Chỉ đọc bản thiết kế, mã hiện có ở `internal/`, `cmd/`, và bằng chứng ở `docs/bang-chung/M17-nghien-cuu/`; không chạy gì trên máy ảo. Tổng cộng 29 điểm: 3 NẶNG (1 đến 3), 3 CẦN THỬ (4, 5, 26; điểm 4 nếu xác nhận thì là NẶNG, đặt ngay sau ba điểm nặng), 9 VỪA, 14 NHẸ. Mỗi điểm cần thử có lệnh và kết quả nào thì là lỗi.

1. **NẶNG. Mục 3.5 (`CageFiles`: lỗi giữa `Apply` và `Undo`), 3.6 (`php.use`, `cage.sync`), 2.4 (dòng "Hạ trần RAM xuống 8M").**
   Vấn đề: hoàn tác của mọi thao tác ghi lại file của lồng rồi khởi động lại hoặc siết dịch vụ chỉ làm "trả file cũ, `daemon-reload`, `try-restart`". Dịch vụ của lồng có `Restart=on-failure` với `RestartSec` mặc định 100 ms, nên một cấu hình mới hỏng (bản PHP mới không nhận `php-fpm.conf`, trần RAM thấp tới mức tiến trình chính bị giết) làm systemd thử lại 10 lần trong khoảng 1 đến 2 giây và chạm `StartLimitBurst=10` của `StartLimitIntervalSec=30`. Khi đó cả dịch vụ lẫn socket vào `failed` và file socket bị gỡ, đúng điều `t4` phần f đo được (`socket: failed (service-start-limit-hit) | file socket: MAT`). `try-restart` không làm gì với unit đang `failed` (nó chỉ khởi động lại unit đang chạy, `t9` phần c), nên sau hoàn tác site vẫn 502 cho tới khi có người gõ `reset-failed`: job báo "đã hoàn tác" trong khi site đang chết. Thêm vào đó, `CageFiles.Apply` không chạy `php-fpm<bản> -t` trước khi `try-restart`; bước kiểm cấu hình chỉ nằm trong `CageSocket` (bật), mà Plan `php.use` của site trong lồng không có `CageSocket`.
   Kịch bản: site A trong lồng chạy PHP 8.3, đang có khách. Chủ site đổi sang 7.4 (`POST /sites/{domain}/php`). `CageFiles` ghi `ExecStart=/usr/sbin/php-fpm7.4 ...`, `daemon-reload`, `try-restart`. FPM 7.4 không khởi động được với cấu hình này (một dòng cấu hình hay một module thiếu). Trong 2 giây dịch vụ chết 10 lần, socket `failed`, file socket mất. Bước "kiểm 200 hai lần" hỏng, engine hoàn tác `CageFiles`: ghi lại `ExecStart` của 8.3, `daemon-reload`, `try-restart` (không làm gì). Kết quả: job `rolled_back`, A trả 502 vô thời hạn. Cùng cách đó: `package edit` hạ RAM xuống mức thấp hơn phần A đang dùng; `t3` phần 6 cho thấy dịch vụ rơi vào "activating" và 502; hoàn tác trả file slice nhưng không đưa socket ra khỏi `failed`. Bản thử `t3` dọn lồng ngay sau khi hạ 8M, nên câu "job hoàn tác về trần cũ" ở 2.4 và 3.6 chưa có bằng chứng nào cho thấy site sống lại.
   Đề xuất: (a) `CageFiles.Apply` chạy `php-fpm<bản> -t --fpm-config <conf>` trong lồng (qua `cage.Wrap`) trước khi `try-restart` mỗi khi file dịch vụ hoặc `php-fpm.conf` đổi; hỏng thì trả file về trước khi đụng tới dịch vụ. (b) Mọi đường trả lại (lỗi giữa `Apply`, `Undo` của `CageFiles` và của Plan `cage.sync`) ghi nhớ trạng thái trước đó (socket có `active` không, dịch vụ có `active` không); sau khi trả file thì `systemctl reset-failed <dịch vụ> <socket>`, `systemctl start <socket>`, và `start <dịch vụ>` nếu trước đó nó đang chạy, rồi chờ file socket như `CageSocket` bật. (c) Cân nhắc `RestartSec=1s` hoặc hơn để mười lần thử trải ra quá 30 giây, nhưng (b) vẫn bắt buộc. (d) Thêm unit test "Undo sau khi dịch vụ đã failed thì gọi reset-failed và start socket", và một khẳng định e2e. Phép thử xác nhận trên máy ảo: lặp lại phần 6 của `t3` (hạ `MemoryMax=8M`), thêm `sleep 10; systemctl is-failed <dịch vụ> <socket>`, rồi trả trần 200M, `daemon-reload`, gọi A; A vẫn 502 cho tới khi `reset-failed` là đúng lỗi này.

2. **NẶNG. Mục 3.6 (bảng "Ghép với thao tác đã có", dòng "Chuyển web server, SSL, harden..."), 3.7, mục 8 (đợt 3).**
   Vấn đề: thiết kế chỉ nói SSL "đi qua `siteVhost` nên tự ra vhost của lồng", nhưng Plan xin SSL dựng kiểm chứng thẳng bằng `checks.SiteChecksSSL(web, php, l)` (`internal/core/ssl.go` dòng 330), không qua `siteChecks`. Bộ đó có `PoolSocket(l, php)`, tức socket của pool chung `/run/php/tpan-<slug>-<bản>.sock`, thứ không còn tồn tại khi site ở trong lồng. `ssl.go` không nằm trong vùng file của thợ nào ở mục 8 (đợt 3 kê `site.go, suspend.go, trash.go, php.go, backup.go, apply.go, selfupdate.go, dbops.go`), và e2e bước 20 không xin SSL cho site trong lồng, nên lỗi sẽ lọt tới máy thật. Cùng kiểu: nhánh "DB của site đang khoá" của `siteStateChecks` (`internal/core/health.go` dòng 153) gọi thẳng `checks.PoolSocket`.
   Kịch bản: (1) site A trong lồng, admin chạy `tpan ssl issue A`. Chứng chỉ được cấp, vhost 443 bản lồng được ghi, rồi kiểm chứng gặp `php.socket:A` lỗi "Không có socket /run/php/tpan-<slug>-8.3.sock" và engine hoàn tác cả vhost 443. Không site nào trong lồng xin được SSL; sửa tự động `ssl.issue` của `tls.cert` (chứng chỉ mất hoặc sắp hết hạn) cũng hỏng y như vậy. (2) Site A trong lồng bị khoá DB: health mỗi 5 phút báo `php.socket:A` lỗi, mở sự cố, và cách sửa tự động đưa ra là `php.restart`, tức khởi động lại `php8.3-fpm` dùng chung (mọi site ngoài lồng gián đoạn vài giây) mà không bao giờ sửa được.
   Đề xuất: thêm một dòng SSL vào bảng "Ghép với thao tác đã có" nói rõ kiểm chứng là `SiteChecksSSLCaged` khi `st.Caged`; kê trong thiết kế mọi chỗ trong `internal/core` đang gọi thẳng `checks.SiteChecks`, `checks.SiteChecksSSL`, `checks.PoolSocket`, `steps.SitePool`, `l.SocketFor` (hiện có ở `ssl.go:330`, `health.go:153`, `fix.go:64`, `fix.go:281`, `php.go`, `suspend.go`, `trash.go`, `site.go`, `apply.go`, `backup.go`, `users.go:489`, `route_m14.go:750`) và chỗ nào đổi ra sao; đưa `ssl.go` vào vùng của thợ "ghép"; thêm một test tĩnh "trong `internal/core` không chỗ nào gọi `PoolSocket` hay `SiteChecks*` ngoài hàm chọn bộ phép kiểm"; e2e thêm "xin SSL (pebble) cho A trong lồng: đạt, `caged` vẫn 1".

3. **NẶNG. Mục 3.6 (`cage.sync`, `PrepareEditPackage`, `PrepareUserPackage`, bước 4 của `cage.enable`), bất biến 7, mục 5 (K2).**
   Vấn đề: phép kiểm "site trả 200" là thứ site kiểm soát được (mã PHP của site quyết định mã trả về), nhưng thiết kế dùng nó làm điều kiện để chính sách của admin có hiệu lực. `cage.sync` gom mọi site trong lồng của một gói vào một Plan: một site không trả 200 là cả job hoàn tác và dòng gói không được ghi. `PrepareUserPackage` chỉ ghi gói mới của user khi job thành công. `cage.enable` hỏng ở bước 4 thì site ở ngoài lồng, không có trần nào.
   Kịch bản: admin hạ khách `khach` từ gói `lon` (RAM 1024) xuống gói `nho` (RAM 256) vì khách không trả tiền. Khách (hoặc kẻ đã chiếm site của khách) đặt một mu-plugin trả 500 khi `REMOTE_ADDR` là `127.0.0.1`, đúng địa chỉ mọi phép gọi kiểm của tpan dùng (`curl --resolve <domain>:80:127.0.0.1`), còn khách thật vẫn thấy 200. `user edit khach --package nho` chạy `cage.sync`, phép kiểm 200 hỏng, job hoàn tác, gói của khách vẫn là `lon`: user thường tự giữ được trần cao, trái bất biến 7. Tương tự, một khách trong gói `chung` chặn được mọi lần admin hạ trần của cả gói cho mọi khách khác; một site có lỗi thật (plugin hỏng trả 500) cũng chặn việc đổi trần của cả gói. Nếu `cage enable --all` là một job thì một site hỏng giữ mọi site khác ở ngoài lồng.
   Đề xuất: (a) Không để phản hồi HTTP của site làm điều kiện cho việc hạ trần hay đổi gói. Điều kiện an toàn nằm ở phía systemd: dịch vụ không `failed`, socket `active`, file socket có, tiến trình chính còn sống sau vài giây; HTTP 200 chỉ thành một dòng cảnh báo trong báo cáo. (b) Tách `cage.sync` thành một job cho mỗi site (như pha D của SSL); dòng gói được ghi độc lập; site nào áp không được thì `cage.limits` của site đó báo lỗi, các site khác không bị kéo theo. Làm y như vậy cho `cage enable --all`. (c) `PrepareUserPackage` ghi gói mới của user bất kể kết quả áp trần từng site, rồi báo site nào chưa áp được. (d) `cage.state` ở trường hợp "gói muốn lồng mà site ở ngoài" nên là lỗi (không phải cảnh báo) khi `cage.enable` của site đó đã từng chạy và hỏng, để admin thấy site đang chạy không trần. (e) Bất biến 7 thêm một test: "site trả 500 cho 127.0.0.1 không chặn được `user edit --package` hạ gói". Riêng `cage.enable`, phép kiểm HTTP vẫn có ích (lồng làm hỏng một site đang chạy được thì nên hoàn tác), nên luật chặt hơn "bỏ hẳn" là: mã trả về sau khi đổi không được tệ hơn mã của đúng phép gọi đó ngay trước khi đổi (site vốn đã 500 thì không chặn việc vào lồng), kèm `--force` của admin cho trường hợp site cố ý nhận ra lồng.

4. **CẦN THỬ (nếu xác nhận thì NẶNG). Mục 2.3 (hàng rào), 3.4 (`Bare`), 3.7 (`checks.Isolated`), mục 5 (K1.1, K1.5).**
   Vấn đề: `ProtectProc=invisible` chỉ giấu tiến trình mà lồng không có quyền đọc theo luật ptrace; tiến trình cùng uid với site vẫn hiện trong `/proc` của lồng, kể cả tiến trình chạy ngoài lồng (lồng không có PID namespace; systemd 255 chưa có `PrivatePIDs=`). Với tiến trình cùng uid và "dumpable", kernel cho mở `/proc/<pid>/root` (kiểm `PTRACE_MODE_READ_FSCREDS`; Yama `ptrace_scope=1` của Ubuntu chỉ chặn chế độ ATTACH), và `/proc/<pid>/root` của tiến trình ngoài lồng là gốc thật của máy. Thiết kế cố ý chạy lệnh dưới user của site ở ngoài lồng: lệnh `test` có `Bare: true` của `checks.Isolated` (mỗi vòng health, ít nhất bốn lệnh cho mỗi site khác), và các dòng "sửa tay" tpan in ra cho người vận hành gõ (`runuser -u tp_<slug> -- wp core download ...` ở `WPChecksums`, các `recheck` dạng `runuser ...`). `runuser` đổi uid rồi `execve`, nên tiến trình `test` hay `wp` sau đó là dumpable. Kẻ tấn công trong lồng chỉ cần quét `/proc` liên tục (sau `hidepid` chỉ còn vài pid cùng uid) và `open("/proc/<pid>/root", O_DIRECTORY)` khi thấy pid mới; fd đó vẫn dùng được sau khi tiến trình ngoài lồng đã thoát.
   Kịch bản: A trong lồng, kẻ tấn công chạy được PHP của A và để một vòng lặp chạy nền trong dịch vụ của lồng (chịu trần nhưng vẫn chạy). Mỗi vòng health chạy `runuser -u tp_A -- test -r /var/www/B/wp-config.php` và các phép thử khác của `Isolated(A, B)`. Vòng lặp bắt được pid, giữ fd của gốc thật, rồi đọc `<fd>/var/www`, `<fd>/etc/nginx/sites-enabled`, `<fd>/etc/php/8.3/fpm/pool.d`, ghi vào `<fd>/tmp` và `<fd>/dev/shm` của máy, và nối `<fd>/run/dbus/system_bus_socket` để `ListUnits` ra tên mọi `tpan-cage-<slug>`. C1, C2, C4 của phép thử tấn công vẫn ghi "bị chặn" vì chúng chỉ thử ở lúc không có tiến trình nào như vậy. (Trần vẫn giữ vì cgroup không đổi; quyền file vẫn giữ, đọc file của B vẫn bị từ chối. Cái mất là toàn bộ phần "không nhìn thấy" của hàng rào, tức vấn đề 1 của mục 1.)
   Cách thử trên máy ảo (A trong lồng): ngoài lồng chạy `runuser -u tp_<A> -- sleep 60 &`; trong lồng chạy `systemd-run --wait --pipe --unit=tpan-cage-<slug A>-run-thu -- sh -c 'p=$(pgrep -u tp_<A> -x sleep); ls /proc/$p/root/var/www; exec 3</proc/$p/root; sleep 70; ls /proc/self/fd/3/var/www'`. Lồng in ra tên mọi site, kể cả lần thứ hai sau khi `sleep` ở ngoài đã thoát, là lỗi này có thật.
   Đề xuất: (a) Không để tiến trình dumpable cùng uid với một site trong lồng chạy ngoài lồng với cây thư mục thật. Lệnh `Bare` của `checks.Isolated` chạy qua `systemd-run` với một hàng rào thử riêng: cùng user, `TemporaryFileSystem=/var:ro`, `/run:ro`, `/etc/tpan:ro`, `PrivateTmp`, `ProtectProc=invisible`, và chỉ gắn lại đúng đường dẫn đích của phép thử; như vậy vẫn đo được lớp quyền Linux trên đúng inode thật mà không mở cả cây. Lưu ý khi gắn: phép `-r`, `-x` và phép thử socket dùng được lệnh gắn chỉ đọc (file socket không bị `EROFS`), nhưng phép `-w` trên `public` của site đích phải gắn ghi được (`BindPaths`) đúng đường dẫn đó, vì gắn chỉ đọc thì `test -w` luôn trả "bị từ chối" do `EROFS`, bất kể quyền thật, và ma trận cô lập xanh vì một lý do sai. Hoặc bỏ các phép thử xuất phát từ site trong lồng. (b) Mọi dòng "sửa tay" có `runuser -u tp_<slug>` của site trong lồng phải in dạng lệnh vào lồng (`system.Describe`), không thì người vận hành làm theo gợi ý của tpan là mở đúng cửa này. (c) Ghi giới hạn này ở mục 5 (K1.1) và ở bất biến 8.

5. **CẦN THỬ (nếu xác nhận thì VỪA). Mục 2.3, mục 5 (K1.1), 3.9 (C1, C4).**
   Vấn đề: lồng dùng chung network namespace với máy (phải thế, `PrivateNetwork` làm hỏng WordPress), và `ProcSubset=pid` vẫn để nguyên thư mục của chính tiến trình, trong đó có `/proc/self/net/`. `/proc/self/net/unix` liệt kê mọi Unix socket của network namespace kèm đường dẫn lúc bind, không phụ thuộc mount namespace; `/proc/self/net/tcp` và `tcp6` liệt kê mọi kết nối TCP của máy (IP khách của mọi site). Socket của pool chung tên `/run/php/tpan-<slug>-<bản>.sock`, của lồng tên `/run/tpan-cage/<slug>/php.sock`, và slug chứa 16 ký tự đầu của tên miền (mục 5 của KE-HOACH). Vậy mã PHP trong lồng vẫn đọc ra phần đầu tên miền của mọi site, đúng thứ hàng rào hứa che.
   Kịch bản: A trong lồng chạy `file_get_contents('/proc/self/net/unix')`, lọc dòng có `tpan-` và được `shop_example_vn_3fa1c2`, `banhang_abc_com_9c01d2`... cho mọi site trên máy.
   Cách thử: `systemd-run --wait --pipe --unit=tpan-cage-<slug A>-run-thu -- grep -c tpan /proc/self/net/unix` trên máy có ít nhất một site khác; ra số lớn hơn 0 là đúng lỗi này.
   Đề xuất: ghi rõ ở 2.3 và mục 5 là còn lộ qua đây; đặt tên socket của lồng bằng phần không suy ra tên miền (chỉ phần băm, ví dụ `/run/tpan-cage/<6 hex>/php.sock`); thêm một đường "đọc `/proc/self/net/unix`" vào phép thử tấn công để kết quả nói thật (lộ ở cả hai cột, ngoài phạm vi của lồng).

6. **VỪA. Mục 3.7 (`cage.state` (b) và cách sửa), 3.6 (điều kiện đầu của `cage.disable`), 3.5 (thứ tự ghi của `CageFiles`), 3.4.**
   Vấn đề: thiết kế nói "(b) chạy lại Plan đúng với `caged`", nhưng với `caged=0` mà máy còn file của lồng (panel bị tắt giữa `cage.enable`, sau bước 1 hoặc 2) thì không có Plan nào làm đúng việc đó: `cage.disable` từ chối `cage_not_caged` vì nó đòi `caged=1`; chạy lại `site add` với `caged=0` chỉ ghi lại pool chung và vhost thường, không gỡ lồng. Trong lúc đó `cage.Wrap` thấy file hàng rào nên đưa mọi lệnh `wp` (cron, health, sao lưu) vào một lồng có thể đang dựng dở. `CageFiles` cũng không nói thứ tự ghi: nếu `fence.conf` được ghi trước `passwd`, `group` hay `php-fpm.conf`, một lần tắt panel ở giữa để lại hàng rào trỏ vào file chưa có (lệnh gắn `$etc/passwd:/etc/passwd` không có dấu `-`), và mọi lệnh qua `Wrap` của site đó chết với `226/NAMESPACE`.
   Kịch bản: `cage enable A`; panel bị `kill -9` (hoặc máy mất điện) ngay sau khi `fence.conf` được ghi, trước `passwd`. Sau đó `tpan cron run A` mỗi 5 phút ghi "fail"; sao lưu đêm của A hỏng ở `wp core version`; health báo `cage.state:A` lỗi nhưng sửa tự động không có Plan nào để chạy; admin gõ `tpan cage disable A` thì bị từ chối `cage_not_caged`. Đường ra duy nhất là `cage enable A` lần nữa, tức là bắt site vào lồng, kể cả khi admin đã đổi ý; nếu gói đã tắt cờ lồng thì lệnh đó cũng bị từ chối (`cage_not_in_package`) và không còn đường nào.
   Đề xuất: (a) Định nghĩa một Plan "dọn lồng" (`CageSocket{Disable}` rồi `CageFiles{Remove}`, kèm pool chung và vhost thường khi thiếu) dùng cho sửa tự động của `cage.state` (b) khi `caged=0`, và cho `cage disable` khi máy còn file của lồng dù `caged=0` (điều kiện đầu đọc máy, như `Wrap`). (b) `CageFiles.Apply` ghi `fence.conf` sau cùng (sau mọi file nó gắn vào), `Remove` gỡ `fence.conf` trước tiên; nhờ vậy `Wrap` chỉ chuyển sang lồng khi lồng đã đủ file. (c) Thêm unit test "dừng giữa CageFiles ở từng file" cho cả hai chiều.

7. **VỪA. Mục 3.6 (dòng "Tạm khoá, mở khoá"), 3.7.**
   Vấn đề: `unsuspendSteps` của site trong lồng chỉ có `CageSocket` bật. M13 có lối thoát cho đúng trường hợp này với pool chung: file `.suspended` không còn thì ghi pool từ template (`unsuspendSteps` ở `internal/core/suspend.go`), vì site đang khoá thì `site add` bị từ chối. Thiết kế mới không có lối tương đương, và mọi đường khác đều đóng với site đang khoá: `cage.enable` từ chối `site_suspended`, `cage.disable` cũng vậy, chạy lại `site add` bị `refuseSuspendedPool`.
   Kịch bản: A trong lồng đang bị khoá vì chủ chưa trả tiền. Trong lúc khoá, `php-fpm.conf` hay file dịch vụ của A bị xoá (sửa tay, hoặc một lần ghi hỏng giữa chừng). Khách trả tiền, admin bấm mở khoá: `CageSocket` chạy `php-fpm -t` với file không có, job hoàn tác, site vẫn khoá, và admin không còn lệnh nào của tpan đưa A về chạy được.
   Đề xuất: `unsuspendSteps` của site trong lồng dùng `sitePHPSteps` (tức `CageFiles` rồi `CageSocket`), như M13 ghi lại pool từ template; `CageFiles` đúng sẵn thì tự bỏ qua nên trường hợp thường không tốn gì. Ghi thêm: `cage.state` có chạy cho site tạm khoá không (nhánh tạm khoá của `siteStateChecks` hiện trả về sớm, không có phép nào của lồng).

8. **VỪA. Mục 3.6 (dòng "Thùng rác, khôi phục"), bất biến 8.**
   Vấn đề: thứ tự của `restoreSteps` hiện có là chuyển thư mục về, mở khoá user, ghi cron (gồm `WPConstant`, chạy `wp config get/set` dưới user của site), mở khoá user DB, rồi mới tới pool. Thiết kế chỉ thay pool bằng `sitePHPSteps`, nên lệnh `wp` của bước cron chạy khi file hàng rào chưa có (đã bị `CageFiles{Remove}` gỡ lúc vào thùng rác): `Wrap` cho nó chạy bằng `runuser`, ngoài hàng rào. WP-CLI nạp file cấu hình chung `$HOME/.wp-cli/config.yml` với mọi lệnh, kể cả `config set`, mà HOME của user site là `/var/www/<domain>` (thấy ở `t4` phần a), thư mục do site sở hữu; khoá `require:` trong file đó chạy một file PHP tuỳ ý. Vậy đó là mã do site kiểm soát chạy ngoài lồng trên một site `caged=1`, trái câu "không bao giờ" của bất biến 8. Chiều vào thùng rác có một khe tương tự: `trashSteps` gỡ `fence.conf` trước bước gỡ file cron, mà `tpan cron run` không lấy khoá, nên một lượt cron rơi vào giữa hai bước chạy `wp cron event run` ngoài lồng.
   Kịch bản: kẻ tấn công đã đặt `/var/www/A/.wp-cli/config.yml` với `require: /var/www/A/x.php`. Chủ site đưa A vào thùng rác rồi khôi phục. Bước cron của khôi phục chạy `x.php` bằng user của A với cây thư mục thật của máy.
   Đề xuất: trong `restoreSteps` đặt `CageFiles` (chỉ cần user đã có) ngay sau bước mở khoá user, trước mọi bước chạy `wp`; trong `trashSteps` gỡ file cron trước `CageSocket{Disable}` và `CageFiles{Remove}`. Thêm một unit test duyệt mọi Plan của site `caged=1`: không lệnh nào có `AsUser` (trừ `Bare`) nằm trước bước ghi `fence.conf` hoặc sau bước gỡ nó.

9. **VỪA. Mục 3.9 ("Ghi sổ trước", "Dọn trong mọi trường hợp"), mã `probe_running`.**
   Vấn đề: thiết kế không nói dòng `probe_runs` được ghi ở đâu so với khoá toàn cục, cũng không nói "khi không có job" của bộ hẹn giờ đo bằng gì. Engine chỉ lấy khoá trong `Execute` (`internal/engine/engine.go`), còn Plan được dựng trước đó. Nếu dòng `running` được ghi lúc dựng Plan (trước khoá), một Plan `probe.cleanup` dựng cùng lúc (bộ hẹn giờ, `--cleanup` gõ ở SSH, hay lượt thử kế tiếp) thấy dòng đó, chờ khoá, chạy trước lượt thử thật, không thấy gì để dọn và đặt `cleaned=1`; lượt thử chạy sau đó mà chết giữa chừng thì không còn dòng nào báo sót, `probe.leftover` im lặng. Ngược lại, nếu "khi không có job" đọc bảng `jobs`, một lượt thử bị `kill -9` để lại job `running` mãi (từ M4 job treo chỉ được báo, không được dọn), và bộ hẹn giờ không bao giờ dọn. Thêm nữa, engine không hoàn tác bước có `Apply` lỗi, nên một bước tạo site hỏng giữa chừng (đã tạo user Linux, chưa tạo thư mục) để lại phần dở mà câu "hoàn tác của bước tạo chính là dọn" không phủ.
   Kịch bản: admin bấm "Thử tấn công" trên panel trong lúc `tpan isolation probe --cleanup` gõ ở SSH đang chờ khoá vì một job khác; thứ tự lấy khoá đưa `--cleanup` chạy trước và đánh dấu lượt mới là đã dọn. Lượt mới bị tắt giữa chừng: user `tp_tpan_probe_a_...`, DB, vhost còn lại mà health không báo gì.
   Đề xuất: ghi dòng sổ ở bước đầu tiên bên trong job (đã giữ khoá); Plan dọn đọc lại sổ bên trong job, không đọc lúc dựng; quy ước "dòng `running` không phải của job đang giữ khoá thì chắc chắn là sót"; `probe_running` nghĩa là "khoá đang bị một lượt thử giữ" (thử lấy khoá mà không chờ), không phải "có dòng running". Job thử tấn công kết thúc với trạng thái khác thành công thì chạy ngay `probe.cleanup` cho hai tên đó trong cùng tiến trình, không chờ bộ hẹn giờ.

10. **VỪA. Mục 3.10 (`cage disable --all`) và mục 6 (lùi, gỡ tpan).**
    Vấn đề: 3.10 định nghĩa `--all` của `cage disable` là "mọi site trong lồng mà gói không còn muốn", nhưng mục 6 bảo người dùng chạy đúng lệnh đó để bỏ lời từ chối `cage_active` của `self-update --rollback`, và để gỡ tpan "không còn unit nào của tpan cho site". Hai mục mâu thuẫn.
    Kịch bản: admin muốn lùi về v0.2.0, gặp `cage_active` kèm lời khuyên `tpan cage disable --all`. Gói của mọi site vẫn bật lồng nên lệnh không đưa site nào ra, `self-update --rollback` vẫn bị từ chối, lời khuyên không dẫn tới đâu.
    Đề xuất: tách hai cờ, ví dụ `--all` (mọi site đang trong lồng) và `--stale` (chỉ site mà gói không còn muốn lồng), hoặc giữ nghĩa của 3.10 và sửa mục 6 thành lệnh đúng; lời từ chối `cage_active` liệt kê site và một lệnh chạy được ngay.

11. **VỪA (một phần cần thử). Mục 3.7 (`cage.limits`), 3.8 (dòng "Trần đang áp"), 2.4 (dòng đĩa).**
    Vấn đề: `cage.limits` so `memory.max`, `memory.swap.max`, `cpu.max`, `pids.max`, `io.max` "bằng trần của gói" mà thiết kế không nói cách quy đổi, trong khi output thật có ba chỗ không hiển nhiên. (1) `t3` phần 6: bỏ `CPUQuota` thì `cpu.max` của slice biến mất hẳn (`No such file or directory`) dù vẫn có `CPUAccounting=yes`; mẫu `cgroup-mau` không có ca này. (2) `io.max` rỗng khi không có trần đĩa (mẫu `dang-chay/io.max` rỗng); khi có thì dòng mang số thiết bị systemd tự giải từ `/var/www` (máy ảo: `8:0`, cả đĩa, không phải phân vùng mà `stat` trả). (3) Máy cài bằng bộ cài Ubuntu Server mặc định để `/var/www` trên LVM (`dm-0`); trên btrfs hay ZFS systemd không tìm được thiết bị khối và bỏ dòng trần (chỉ ghi log). Khi đó trần đĩa trong gói không bao giờ có trên cgroup: `cage.limits` báo lỗi mãi, sửa tự động `cage.sync` chạy xong vẫn lỗi, mỗi lần health một sự cố.
    Kịch bản: VPS cài bằng ISO Ubuntu Server (LVM), gói có `io_write_mb=10`. Sau `cage enable`, `io.max` có dòng `253:0 ...` (nếu systemd áp được) hoặc rỗng; `cage.limits` tự tính số thiết bị bằng `stat` và báo lệch giả, hoặc báo lỗi thật mà không sửa được.
    Đề xuất: quy định trong thiết kế: file không có hoặc rỗng nghĩa là "không giới hạn"; so `io.max` chỉ theo giá trị `rbps`/`wbps`, không so số thiết bị; gói có trần đĩa mà `io.max` rỗng sau khi áp thì `cage.limits` ra "không áp được trên loại đĩa này" (cảnh báo, không có sửa tự động) thay vì lỗi lặp, và bản xem trước của `cage enable`/`package edit` nói trước điều đó. Thêm mẫu cgroup cho "không có CPUQuota" và "có trần đĩa". Cần thử: gắn một đĩa ảo thứ hai làm LVM vào `/var/www` của máy ảo, đặt `IOWriteBandwidthMax=/var/www 4M`, đọc `io.max` và đo `dd oflag=direct` như `t3`; không có dòng hoặc ghi không bị bóp là đúng lo ngại này.

12. **VỪA. Mục 5 (K1.6), mục 1 (vấn đề 2).**
    Vấn đề: trần đặt ở slice nên chỉ phủ phần việc chạy trong tiến trình của site. Việc site đẩy sang dịch vụ chung thì không: truy vấn MariaDB chạy trong `mariadb.service` (một tiến trình cho mọi site), TLS và file tĩnh chạy trong nginx hay Apache. K1.6 nói "ngốn RAM, CPU ... để làm chết site khác" đã bị chặn, còn e2e chỉ đo phía PHP.
    Kịch bản: kẻ tấn công trong A (trần CPU 50%, RAM 200 MB) mở 50 kết nối MariaDB bằng mật khẩu DB của A và chạy `SELECT BENCHMARK(...)` hoặc một phép nối lớn ra bảng tạm. `mysqld` ăn hết mọi nhân và vài GB RAM; mọi site khác chậm hoặc báo "Error establishing a database connection"; máy hết RAM thì OOM của cả máy có thể giết chính `mysqld`.
    Đề xuất: hoặc thêm giới hạn theo user DB của site (`MAX_USER_CONNECTIONS`, `max_statement_time`) như một phần của trần gói, hoặc ghi rõ ở mục 1 ("Không làm") và mục 5 rằng trần không phủ MariaDB và web server, kèm một dòng "ngoài phạm vi lồng" trong phép thử tấn công. Câu ở K1.6 phải nói đúng phạm vi.

13. **VỪA. Mục 2.3 (danh sách `pool.d`), 3.3 (file dịch vụ), 3.7 (`cage.fence`, `cage.limits`).**
    Vấn đề: hàng rào được kiểm bằng lệnh chạy trong unit tạm mới (`-run-`), tức là kiểm hàng rào sẽ áp cho tiến trình mới, không phải hàng rào của FPM đang phục vụ web. Ba chỗ làm hai thứ lệch nhau mà không phép kiểm nào thấy. (1) Mục 2.3 tự nói bản PHP cài sau "được che từ lần khởi động kế tiếp của dịch vụ": lồng đang chạy đọc được `/etc/php/<bản mới>/fpm/pool.d/` (tên `tpan-<slug>.conf` của các site dùng bản đó) cho tới khi tình cờ được khởi động lại; Plan cài bản PHP không khởi động lại lồng nào. (2) File dịch vụ lặp lại `User`, `Group`, `NoNewPrivileges` nhưng không có `Slice=`: drop-in hàng rào mất (sửa tay, lỗi ghi) rồi dịch vụ khởi động lại thì FPM chạy trong `system.slice`, không trần, không hàng rào; `cage.limits` khi đó thấy slice không có cgroup và ra "đạt, ghi rõ lý do" (3.7), chỉ còn `cage.state` cứu. (3) `cage.fence` chạy `id -un`, `ls -A /var/www`... trong unit tạm, nên vẫn đạt khi FPM đang chạy với hàng rào cũ.
    Kịch bản: A, B trong lồng chạy 8.3 từ hai tuần. Admin cài PHP 8.4 rồi chuyển site C (ngoài lồng) sang 8.4. Mã PHP của A `scandir('/etc/php/8.4/fpm/pool.d')` ra `tpan-<slug của C>.conf`. Health xanh.
    Đề xuất: (a) Plan cài bản PHP (và Plan "Áp bản vá cô lập") `try-restart` mọi dịch vụ lồng đang chạy; hoặc che `/etc/php` bằng `TemporaryFileSystem=/etc/php:ro` rồi chỉ gắn lại phần cần (`cli/`, `fpm/php.ini`, `fpm/conf.d/`, `mods-available/`), để bản cài sau mặc nhiên bị che. (b) Thêm `Slice=tpan-<slug>.slice` vào file dịch vụ, cùng lý do với `User=`. (c) Một phép kiểm (gộp vào `cage.socket`) đọc tiến trình đang chạy: `/proc/<MainPID>/status` có `NoNewPrivs: 1` và `CapEff` bằng 0; `/proc/<MainPID>/cgroup` nằm dưới `tpan-<slug>.slice`; `mountinfo` có tmpfs ở `/var`, `/run`, `/etc/tpan`; thời điểm dịch vụ vào `active` mới hơn mtime của `fence.conf`. Không đạt thì sửa bằng `cage.restart`.

14. **VỪA. Mục 5 (K1.9), 3.9 (L3, L4, L9 "bị chặn" ở cả hai cột), chế độ `day-du` của 6.13 trong KE-HOACH.**
    Vấn đề: socket của mọi lồng là `www-data:www-data 0660`, nên Apache (chạy bằng `www-data`) nối được tới socket của bất kỳ lồng nào. Site Apache ở chế độ `.htaccess` `day-du` (`AllowOverride All`, admin đặt được) vẫn viết được `SetHandler "proxy:unix:/run/tpan-cage/<slug B>/php.sock|fcgi://localhost"` kèm `ProxyFCGISetEnvIf` trong `.htaccess`: L4 nguyên vẹn, mã do A chọn chạy trong lồng của B dưới user của B. Lồng che `/run` khỏi PHP của A nhưng không che khỏi Apache. K1.9 ghi "chặn bằng: socket www-data 0660; /run trong lồng không có tpan-cage", và phép thử tấn công tạo site thử ở chế độ mặc định nên không bao giờ thấy đường này.
    Kịch bản: stack Apache, A ở `day-du` vì một plugin cần chỉ thị ngoài danh sách, A và B đều trong lồng. Kẻ chiếm A ghi `.htaccess` như trên; một request tới A đọc được hằng `DB_PASSWORD` của B. Admin tin rằng "trong lồng mọi đường bị chặn".
    Đề xuất: sửa K1.9 và bảng của 3.9 cho đúng điều kiện ("khi chế độ `.htaccess` là `an-toan` hoặc `tat`"); `cage.enable` cho site Apache ở `day-du` thì hoặc từ chối, hoặc cảnh báo rõ trong bản xem trước; cảnh báo sẵn có của `day-du` thêm câu "lồng không chặn được đường này".

15. **NHẸ. Mục 3.4 bước 4 (đọc kết quả lệnh trong lồng).**
    `t9b` cho thấy với `--service-type=oneshot`, lệnh chết vì `SIGPIPE` vẫn ra mã 0 và `Result=success`; bảng 2.2 chỉ nhắc `SIGTERM`. Thực tế vẫn an toàn vì khi tpan đóng ống đọc thì Go tự trả lỗi chép dữ liệu, nhưng nên ghi rõ để không ai dựa vào mã thoát trong ca này. `Result` còn có `core-dump`, `start-limit-hit`, `resources`, `protocol`; thiết kế chỉ kê bốn giá trị, nên quy định: mọi `Result` khác `exit-code` là lỗi không phải `ExitError`. Lỗi dựng môi trường của systemd hiện ra dưới dạng `Result=exit-code` với mã tổng hợp (`226/NAMESPACE` khi `/var/www/<domain>` chưa có, `217/USER` khi user đã bị xoá, `203/EXEC`), nên `Wrap` sẽ trả `ExitError{Code: 226}` và nơi gọi đọc nó như mã của `wp`; nên coi mã trong dải 200 đến 243 kèm `ExecMainStartTimestamp` rỗng là "không chạy được lệnh". Cuối cùng, `system.Cmd.Env` (chỉ dành cho `tpan brain`) không được nhắc: `Wrap` nên từ chối lệnh có cả `Env` lẫn `AsUser` thay vì lặng lẽ bỏ biến.

16. **NHẸ. Mục 3.4 bước 3 và 5 (hạn giờ, unit mồ côi).**
    Trước M17, `wp` chạy bằng `runuser` nằm trong cgroup của `tpan.service`, nên khi tpan chết (OOM, `kill -9`, quá `TimeoutStopSec` lúc `systemctl restart tpan`) thì `wp` chết theo. Nay unit `-run-` nằm trong slice của site và chạy tiếp tới `TimeoutStartSec` (3600 giây khi ctx không có hạn) sau khi khoá toàn cục đã nhả, nên job kế tiếp trên cùng site có thể chạy song song với nó (ví dụ `wp option update siteurl` của một Plan SSL bị gián đoạn chạy xen với Plan khôi phục backup). 3600 giây cũng là một trần mới cho mọi lệnh không có hạn, trước M17 không có. Đề xuất: ghi pid của tpan và id của job vào `Description=` của unit tạm; lúc `tpan serve` khởi động và trước mỗi job trên một site, dừng các unit `tpan-cage-<slug>-run-*` mà tiến trình gọi đã chết; ghi trần 3600 giây vào phần "khác đặc tả".

17. **NHẸ. Mục 2.8 và 2.4 (đọc bằng chứng).**
    `t13b`: lượt "Đối chứng: thư mục có sẵn trước khi lồng chạy" cũng hỏng (`Job ... failed`, A 502), nhiều khả năng vì hai lượt trước đã đẩy dịch vụ và socket vào `failed` (giới hạn khởi động), nên `t13b` không chứng minh được ca đối chứng; kết luận của 2.8 vẫn đứng nhờ `t12`. Nên ghi điều này, và nếu cần bằng chứng sạch thì chạy lại lượt đối chứng sau `reset-failed`. `t13` phần a: dòng "install -d trên thư mục đã có ... sau khi restart lồng /run/mysqld là 700" không nói gì về `install -d`, vì lúc đó lệnh `install` hỏng ngay (`invalid user 'mysql'`); thiết kế không dựa vào dòng này, đừng dẫn nó về sau. Câu "job hoàn tác về trần cũ" ở 2.4 và 3.6 chưa có bằng chứng (xem điểm 1).

18. **NHẸ. Mục 3.3 và 3.5 mâu thuẫn về `passwd`, `group`.**
    3.3 nói hai file này không khai trong `Info().Files` và không vào `managed_files`. 3.5 nói `Info().Files` kê cả bảy file "trừ `passwd`, `group` khi user chưa có lúc dựng Plan", tức là có kê khi user đã có, mà `cage.enable` trên site đang chạy thì user luôn có. Làm theo 3.5 thì hai file vào `managed_files`, `health.fileOwner` (`OwnsCagePath` chỉ nhận năm đường dẫn) không gán chúng cho site nào, `core.Fixes` không dựng lại được (`Want` chỉ trả năm file), và một lần `usermod` đổi shell hay GECOS của user là thành lệch không có cách sửa. Chọn một cách và sửa câu kia.

19. **NHẸ. Mục 8 (bảng chia việc).**
    Đợt 2 giao Step `CageRestart` nhưng 3.5 không đặc tả nó (chỉ 3.7 tả việc nó làm). Hợp đồng đợt 0 chỉ có chữ ký của `internal/core/cage.go`, trong khi ở đợt 3 thợ "Plan của lồng" phải nối sửa tự động `probe.cleanup` (Plan do thợ "thử tấn công" viết trong `probe.go`), còn thợ "thử tấn công" cần `domain_reserved` trong `Sites.PrepareAdd` (nằm ở `site.go` của thợ "ghép"): đưa chữ ký của `probe.go`, mẫu tên site thử và chỗ kiểm `domain_reserved` vào đợt 0. `ssl.go` (điểm 2) và chỗ gọi `siteChecks` ở `users.go:489` không nằm trong vùng file của ai ở đợt 3. Script e2e viết ở đợt 1 "theo hợp đồng CLI và JSON" nhưng 3.10 không định nghĩa hình JSON của `tpan usage --json` và `isolation probe --json` (tên `oom_kills`, `mem_peak`, `tasks_peak` chỉ xuất hiện trong bảng e2e), và cột của `probe_runs` chưa đủ (3.9 nhắc `cleaned` và kết quả, 3.1 không kê); đưa các kiểu đó vào đợt 0.

20. **NHẸ. Mục 3.8 (số liệu và gợi ý).**
    (a) `memory.peak` và `pids.peak` là đỉnh từ khi slice có, không phải của 7 ngày; slice sống nhiều tuần thì gợi ý RAM và số tiến trình không bao giờ giảm (một lần nhập dữ liệu từ ba tháng trước vẫn quyết định gợi ý). Chỉ lấy `mem_peak` của một mẫu khi nó tăng so với mẫu liền trước trong cửa sổ (đỉnh đó xảy ra trong khoảng ấy), còn lại dùng `mem_current`. (b) Nhận ra "bộ đếm về 0" bằng `sau < trước` đếm thiếu khi slice bị tạo lại (tạm khoá rồi mở, ra rồi vào lồng) và bộ đếm mới vượt số cũ trong vòng 5 phút; ghi kèm mỗi mẫu một "thế hệ" của cgroup (số inode của thư mục cgroup, hoặc boot id cộng lúc slice vào active) và chỉ trừ khi cùng thế hệ. (c) Phần trăm CPU chia cho hiệu `at` theo đồng hồ treo tường: đồng hồ bị chỉnh cho khoảng âm hay rất lớn; bỏ khoảng có độ dài ngoài một cửa sổ hợp lý (ví dụ 1 tới 15 phút). (d) Mẫu nên ghi trần đang áp (`memory.max`, `cpu.max`, `pids.max`) để luật "có `oom_kill` thì không thấp hơn trần × 1,5" dùng trần lúc xảy ra chứ không phải trần hiện tại của gói mới. (e) `memory.current` gồm cả page cache (`file` 24,5 MB trong `t5`), nên đỉnh × 1,25 thổi phồng nhu cầu; cân nhắc trừ phần cache sạch từ `memory.stat`. (f) Gợi ý số tiến trình phải tôn trọng `tasks ≥ số worker + 5` của 3.1, không thì gợi ý (sàn 20) bị chính `package edit` từ chối với gói 50 worker. (g) Xoá hẳn site thì xoá mẫu của domain đó, không thì site mới cùng tên thừa hưởng số cũ. (h) Trên kernel 6.8, `pids.events` của unit `-run-` mất khi unit kết thúc, nên lần chạm trần của cron chỉ được đếm nếu mẫu rơi đúng lúc nó chạy; ghi rõ giới hạn.

21. **NHẸ. Mục 3.5 và 3.9 (thứ còn sót trên máy).**
    `RemoveOnStop=yes` gỡ file socket nhưng không gỡ thư mục `/run/tpan-cage/<slug>` do systemd tạo (`long_go` trong `lib.sh` phải `rmdir` tay); `CageSocket{Disable}` và `CageFiles{Remove}` không nhắc, nên sau khi ra lồng hay dọn phép thử còn thư mục mang slug `tpan_probe_...` (khẳng định e2e "không còn gì mang tên tpan-probe-" có thể đỏ). Unit `-run-` hỏng mà tpan chết trước khi `reset-failed` thì nằm lại ở `failed`; Plan dọn và `CageFiles{Remove}` nên `systemctl reset-failed 'tpan-cage-<slug>-*'`. Câu "gỡ `/etc/tpan` nếu rỗng": `/etc/tpan` là thư mục 0700 của `tpan setup` (phép tự kiểm của tpan đòi đúng quyền đó), `CageFiles` không được tạo, đổi quyền hay xoá nó, chỉ thư mục con `cage/`.

22. **NHẸ. Mục 3.7 (`cage.socket`).**
    Kiểm "mountinfo có ` /run/mysqld `" không bắt được lần gắn đã cũ: `/run/mysqld` bị xoá rồi tạo lại khi lồng đang chạy thì dòng gắn vẫn còn (phần gốc thành `/mysqld//deleted`) mà lồng không thấy socket mới. So inode chắc hơn: `stat /proc/<MainPID>/root/run/mysqld` với `stat /run/mysqld` (root đọc được cả hai); tối thiểu thì bắt chuỗi `//deleted`. `/run/systemd/resolve` có đúng kiểu hỏng này (gắn có dấu `-`, lồng thức trước `systemd-resolved` thì phân giải tên hỏng tới khi khởi động lại) mà không được kiểm; khả năng thấp vì nginx đứng sau `network.target`, nhưng cùng một dòng kiểm là đủ.

23. **NHẸ. Mục 2.4 và 5 (K1.6).**
    `IO*BandwidthMax=/var/www ...` chỉ bóp thiết bị chứa `/var/www`; máy gắn `/var/www` trên một đĩa riêng thì việc ghi vào `/tmp` riêng của lồng (nằm trên đĩa gốc, `/tmp/systemd-private-*`) và vào log không bị bóp. Áp cùng trần cho thiết bị của `/` và của thư mục log, hoặc ghi rõ. K1.6 kê "đĩa" như đã chặn; thật ra chỉ chặn tốc độ, không chặn dung lượng (đầy đĩa qua `/tmp` riêng hay thư mục site vẫn làm chết mọi site) và không chặn băng thông mạng.

24. **NHẸ. Mục 3.9 (chi tiết phép thử tấn công).**
    (a) File vhost lạ của L8 phải nằm trong thư mục cấu hình thật của web server (`nginx -T` và `apache2ctl -S` đọc file trên đĩa), và mọi lần reload xảy ra lúc nó còn đó đều nạp nó vào web server đang phục vụ; hook gia hạn `tpan internal reload-web` (`ReloadWeb` ở `internal/core/ssl.go`) không lấy khoá nên có thể rơi đúng lúc. Quy định: tạo, kiểm và gỡ trong cùng một bước (Undo gỡ nó), nội dung tối thiểu hợp lệ (chỉ tên miền của A, không `default_server`), tên sắp sau `000-tpan-default.conf` như `aaa-` hiện đã đúng. (b) Câu "thư mục đích ... nằm dưới `/var/www/tpan-probe-`" nên là so bằng với `site.LayoutOf(tên trong sổ).Public`, vì site thật `tpan-probe-shop.example.com` không khớp mẫu đặt riêng mà vẫn nằm dưới tiền tố đó. (c) Lượt "trong lồng" hỏng (ví dụ máy là container LXC, lồng không dựng được) thì engine hoàn tác cả job và kết quả của lượt "ngoài lồng" mất; ghi kết quả từng lượt ra sổ ngay khi lượt đó xong, và coi "không dựng được lồng" là một kết quả (`skipped` kèm lý do). (d) Điều kiện đầu của `cage.enable` (cgroup v2, systemd từ 254) cũng phải áp cho lượt "trong lồng".

25. **NHẸ. Mục 3.7 (`cage.state` sinh ra khi nào), mục 6 (lời từ chối `cage_active`).**
    `cage.state` chỉ sinh cho site có `caged=1` hoặc gói có `cage=1`; file của lồng còn sót ở một site `caged=0` mà gói đã tắt cờ (điểm 6) không bao giờ được báo, trong khi `Wrap` vẫn đưa `wp` của site đó vào lồng. Một phép `Stat` file hàng rào cho mọi site, chỉ sinh kết quả khi có file, giữ đúng ý "site thường không thêm phép kiểm" mà vẫn bắt được. Lời từ chối `cage_active` của `self-update --rollback` chỉ đếm `sites.caged`; nên tính cả sự thật trên máy (còn file `tpan-cage-*` ở `/etc/systemd/system`) và dòng `probe_runs` chưa dọn, vì v0.2.0 không biết dọn phép thử.

26. **CẦN THỬ (nếu xác nhận thì NHẸ). Mục 3.1 (`tasks ≥ số worker + 5`).**
    `TasksMax` đếm cả luồng, còn `t2` chạy WordPress khi không đặt `TasksMax`. Máy có `php-imagick` thì ImageMagick sinh luồng OpenMP theo số nhân khi tạo thumbnail; cộng cron `wp` (kèm `sh` và `mysqldump` khi xuất DB) và các lệnh của `cage.fence`, trần sát kiểu `worker + 5` dễ làm upload ảnh hỏng. Cần thử: gói 5 worker, `TasksMax=10`, cài `php8.3-imagick`, upload một ảnh lớn trong lúc có 5 request song song; ảnh lỗi hoặc `pids.events` tăng là trần dưới quá thấp.

27. **NHẸ. Mục 3.3 và 3.5 (thời gian chờ khi dừng dịch vụ).**
    `CageSocket{Disable}` nói "chờ request đang chạy, tối đa 30 giây" (`TimeoutStopSec=30`), nhưng `process_control_timeout = 10s` làm FPM tự gửi `SIGTERM` cho tiến trình con sau 10 giây, nên request dài hơn 10 giây vẫn bị cắt. Nói đúng con số, hoặc nâng `process_control_timeout` gần bằng `TimeoutStopSec`.

28. **NHẸ. Mục 3.5 (`CageSocket` hỏng thì kèm "20 dòng cuối journalctl").**
    Journal của dịch vụ lồng chứa dòng do mã PHP của site ghi (`syslog()` đi qua `/run/systemd/journal/dev-log` được gắn vào lồng), nên lời lỗi của job mang chữ do site kiểm soát tới terminal của root, giao diện và bộ não AI. Lọc ký tự điều khiển và đánh dấu đó là output của site (cờ `Untrusted` của mục 8.1).

29. **NHẸ. Mục 3.1, 3.3, 3.6 (chỗ còn mơ hồ).**
    `php_workers` có tính là "trần" trong luật `package_limits_need_cage` không? Nếu không, gói tắt lồng lưu được số worker không có hiệu lực (pool ngoài lồng luôn 5), trái câu "không lưu một con số không có hiệu lực". "Job thành công và gói của chủ bật lồng thì `SiteAdd.Execute` chạy tiếp `cage.enable`": có áp cho lần chạy lại `site add` của site còn ở ngoài lồng không? Nếu có, sửa một site bằng chạy lại sẽ lẳng lặng đưa nó vào lồng. Chuyển user sang gói tắt lồng thì gói mới buộc mọi trần bằng 0, nên `PrepareUserPackage` gỡ trần của các site đang trong lồng của user (vẫn trong lồng nhưng hết trần); bản xem trước phải nói rõ. File socket unit đổi nội dung (bản sau đổi template) chỉ có hiệu lực khi socket được khởi động lại, còn `CageFiles` chỉ `daemon-reload`; ghi lại cho mốc sau. Nên thêm `RequiresMountsFor=/var/www/<domain>` vào file dịch vụ cho máy gắn `/var/www` từ đĩa khác.

Đã kiểm, đúng (những chỗ dễ bị nghi):

- Hai slug không bao giờ dùng chung drop-in theo tiền tố: slug chỉ có `a-z0-9_`, nên `tpan-cage-X-` không thể là tiền tố của tên unit thuộc slug khác. Slug dài nhất 23 ký tự, đường dẫn socket dài nhất 47 byte, dưới giới hạn 108 của Unix socket.
- `User=` có trên dòng lệnh `systemd-run` và trong file dịch vụ, nên mất drop-in thì lệnh không bao giờ chạy bằng root.
- Gỡ pool chung của site cuối cùng trên một bản PHP không làm `php<bản>-fpm` hỏng: pool `tpan-idle` của M12 luôn còn.
- Site đang tạm khoá không vào được thùng rác (quyết định 7 của M13), nên không có ca khôi phục từ thùng rác một site tạm khoá trong lồng.
- Mọi đường tạo site (`apply`, Plan của MCP và AI) đều qua `Sites.PrepareAdd`, nên kiểm `domain_reserved` ở đó là đủ.
- Route sửa user, sửa gói, Plan chờ duyệt, sửa tự động đều là admin (quyết định 11, 12 của M13); user thường không có đường trực tiếp nào tới trần hay trạng thái lồng ngoài đường gián tiếp ở điểm 3.
- Health bỏ vòng khi khoá toàn cục đang bị giữ, nên trong lúc thử tấn công health không thấy site thử.
- Sao lưu và khôi phục chạy `tar`, `mariadb-dump`, `mariadb` bằng root và tráo `public` (thư mục con của chỗ được gắn), nên lồng đang chạy thấy ngay mã mới mà không cần khởi động lại; chỉ `wp core version` đi vào lồng.
- `/etc/tpan` là 0700 trên máy nhưng trong lồng là tmpfs mới, nên FPM (user của site) vẫn đọc được `/etc/tpan/cage/<slug>/php-fpm.conf`.
- Thư mục log phải gắn ghi được vì `php-error.log` thuộc user của site; thư mục vẫn thuộc root nên site không tạo hay đổi tên file khác trong đó, logrotate an toàn như M14.
- Cách đọc `t10`, `t11`, `t12` ở mục 2.8 khớp output; dòng `ExecStartPre` ở unit socket là cách chữa có bằng chứng lúc khởi động thật.
- Không có hai thợ cùng đợt phải sửa một file; chỗ thiếu của bảng là phụ thuộc trong cùng đợt 3 và file không ai nhận (điểm 19).
- Site không đổi chủ được sau khi tạo (chạy lại `site add` bỏ qua `Owner`, ghi chú ở `SiteInput`), và gói đang có user dùng thì không xoá được (`package_in_use`), nên gói hiệu lực của một site chỉ đổi qua `package edit` và `user edit --package`, đúng hai đường thiết kế đã bắt (`PrepareEditPackage`, `PrepareUserPackage`).

### Cách đã sửa (phiên chính, 21/11/2026)

Ba điểm "cần thử" đã thử trên `tpan-m17` trước khi sửa (`t14-kiem-phat-hien-soat.sh` và output cạnh nó). Điểm 4 đúng và nặng: lồng thấy pid của `sleep` chạy bằng `runuser` ở ngoài, liệt kê `/var/www` thật qua `/proc/<pid>/root`, giữ fd sau khi `sleep` thoát, và ghi được file vào `/tmp` của máy. Điểm 5 đúng: `/proc/self/net/unix` trong lồng có 17 dòng `tpan`, tên socket mang slug của mọi site ngoài lồng. Điểm 1 đúng: hạ trần RAM xuống 8M làm dịch vụ và socket kẹt ở `failed`; trả trần cũ rồi `try-restart` vẫn 502, chỉ `reset-failed` và `start` socket mới cứu. Điểm 26 chưa thử (máy không cài `php-imagick`): nâng trần dưới thay vì thử.

| Điểm | Cách sửa (mục đã sửa) |
|---|---|
| 1 | `CageFiles` chạy `php-fpm -t` trong lồng trước khi khởi động lại; đường trả lại nhớ trạng thái socket và dịch vụ, `reset-failed`, `start` lại, không được thì `undo_failed`; `RestartSec=5s` để không bao giờ chạm giới hạn khởi động (3.3, 3.5; bất biến 12) |
| 2 | Bảng mọi chỗ trong `core` giả định pool chung và cách đổi; `ssl.go` và `users.go` vào vùng của thợ ghép; test tĩnh; e2e xin SSL cho site trong lồng (3.6, 7, 8) |
| 3 | Hai loại Plan: chính sách của admin chỉ chờ thứ site không điều khiển được; sửa gói và đổi gói ghi dòng trước rồi chạy `cage.sync` không có phép gọi HTTP; `cage.enable` không chờ dịch vụ (3.6; bất biến 11; K2.4) |
| 4 | Bỏ `Cmd.Bare`; `Isolated` không sinh từ site trong lồng; `CageReap` trong `cage.enable` trước khi mã của site chạy trong lồng (đổi lại một khoảng 502 ngắn, đo ở e2e); `cage.runtime` báo tiến trình cùng uid ngoài slice; dòng sửa tay in qua `system.Describe`; thứ tự thùng rác và khôi phục (3.4, 3.5, 3.6, 3.7; bất biến 8; K1.1b) |
| 5 | Socket của lồng mang phần băm của tên miền; còn lộ ghi rõ (tên socket của site ngoài lồng, kết nối TCP); thêm đường C5 vào phép thử (3.2, 3.9; K1.1c) |
| 6 | `cage.disable` đọc máy (dọn cả lồng dựng dở); hàng rào ghi sau cùng, gỡ trước tiên; `cage.state` (b) có sửa tự động cho cả hai chiều (3.5, 3.6, 3.7) |
| 7 | Mở khoá site trong lồng dùng `sitePHPSteps` (ghi lại file của lồng nếu mất); nhánh tạm khoá có `cage.state` (3.6, 3.7) |
| 8 | Khôi phục: `CageFiles` ngay sau mở khoá user, trước mọi `wp`; đưa vào thùng rác: gỡ cron trước khi gỡ hàng rào; test duyệt mọi Plan (3.6; bất biến 8) |
| 9 | Sổ ghi ở bước đầu bên trong job; Plan dọn đọc sổ bên trong job; job hỏng thì dọn ngay trong cùng tiến trình; bộ hẹn giờ chỉ dọn khi khoá trống (3.9) |
| 10 | `--all` là mọi site có lồng, `--stale` là site mà gói không còn muốn; lời từ chối `cage_active` liệt kê và đưa lệnh chạy được (3.6, 3.10, 6) |
| 11 | Quy đổi của `cage.limits` (file không có hay rỗng, `io.max` chỉ so tốc độ, đĩa không áp được là cảnh báo); mẫu `cgroup-mau/khong-cpu`, `co-io` (3.7). LVM chưa thử: ghi là chưa thử |
| 12, 23 | Phạm vi của trần ghi đúng ở K1.6 (không phủ MariaDB, web server, dung lượng đĩa, mạng, thiết bị khác `/var/www`) |
| 13 | `Slice=` trong unit dịch vụ; `php.install` và harden khởi động lại lồng đang chạy; `cage.runtime` đọc tiến trình đang chạy (3.3, 3.6, 3.7) |
| 14 | K1.9 và bảng phép thử nói đúng điều kiện; bản xem trước của `cage.enable` cảnh báo với site Apache ở `day-du` (3.6, 3.9, 5) |
| 15 | Mọi `Result` khác `exit-code` là `KilledError`; mã 200 đến 243 không có thời điểm bắt đầu là "không chạy được"; từ chối `AsUser` kèm `Env`; ghi chú `SIGPIPE` (3.4) |
| 16 | Unit tạm sống tới `TimeoutStartSec` khi tpan chết, 3600 giây là trần mới (ghi ở "khác đặc tả" của M17.md); `tpan serve` dọn unit tạm hỏng lúc khởi động; `CageFiles{Remove}` dừng unit tạm còn chạy (3.4, 3.5). Không chặn xen kẽ với job sau (chấp nhận, ghi lại) |
| 17 | Ghi nhận: lượt đối chứng của `t13b` không có giá trị (dịch vụ đã chạm giới hạn khởi động ở hai lượt trước); 2.8 dựa vào `t12`, không dựa vào dòng "install -d ... 700" |
| 18 | `passwd`, `group` không bao giờ vào `Info().Files` (3.3, 3.5) |
| 19 | Hợp đồng đợt 0 có chữ ký của `probe.go`, mẫu tên site thử, `domain_reserved`, kiểu JSON; `ssl.go`, `users.go` có chủ (8) |
| 20 | Đỉnh RAM chỉ tính khi tăng trong cửa sổ; thế hệ theo `cpu_usec` giảm; bỏ khoảng ngoài 1 đến 15 phút; mẫu ghi trần lúc đó; gợi ý tiến trình tôn trọng luật worker; xoá hẳn site thì xoá mẫu; giới hạn của bộ đếm trên kernel 6.8 ghi rõ (3.8). Bỏ phần cache khỏi RAM: không làm, ghi rõ con số nghiêng về an toàn |
| 21 | Gỡ thư mục socket khi tắt; `reset-failed 'tpan-cage-<slug>-*'`; không đụng `/etc/tpan` (3.3, 3.5, 3.9) |
| 22 | `cage.runtime` bắt dòng gắn mang `//deleted` cho cả `/run/mysqld` và `/run/systemd/resolve` (3.7). So inode cần mở rộng `system.FileInfo`: không làm ở mốc này |
| 24 | L8 tạo, kiểm và gỡ trong một bước; so thư mục đích bằng dấu bằng; kết quả từng lượt ghi sổ ngay; không dựng được lồng là `skipped`; điều kiện của `cage.enable` áp cho lượt trong lồng (3.9) |
| 25 | `cage.state` sinh cho site có file hàng rào; `cage_active` tính cả file trên máy và sổ phép thử (3.7, 6) |
| 26 | Trần tiến trình tối thiểu 32 và ≥ số worker + 15 (3.1); chưa thử với `php-imagick` |
| 27 | `process_control_timeout = 25s` (3.3) |
| 28 | Lời lỗi mang journal của site: bỏ ký tự điều khiển, ghi rõ là output của site (3.5, 5) |
| 29 | `php_workers` tính là trần đòi lồng (3.1); chạy lại `site add` không tự vào lồng (3.6); gói mới tắt lồng thì trần về 0 và bản xem trước nói rõ (3.6); `RequiresMountsFor` trong unit dịch vụ (3.3). File unit socket đổi chỉ có hiệu lực khi socket khởi động lại: để mốc sau |

Sau các sửa trên không còn điểm nặng nào mở.
