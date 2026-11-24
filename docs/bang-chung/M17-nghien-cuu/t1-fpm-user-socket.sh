# t1: PHP-FPM riêng của site A chạy bằng CHÍNH user của A (không có tiến trình root
# nào trong lồng), socket do systemd giữ. B vẫn ở PHP-FPM chung để so.
# Câu hỏi: (1) FPM nhận socket qua FPM_SOCKETS có chạy không, ai là chủ tiến trình,
# còn capability nào; (2) drop-in theo tiền tố có áp cho unit tạm của systemd-run
# không; (3) mã thoát, stdin, stderr của lệnh chạy qua systemd-run; (4) reload,
# restart, stop: socket còn không, request trong lúc đó ra sao; (5) điểm
# systemd-analyze security.
long_go $A >/dev/null 2>&1
long_dung $A 200 50 40
S=$SA; SVC=tpan-cage-$S-php.service; SK=tpan-cage-$S-php.socket; SOCK=$(sock_of $A)

echo "## 1. Sau khi bật socket, chưa có request nào"
echo "socket: $(systemctl is-active $SK) | dịch vụ: $(systemctl is-active $SVC) | file: $(stat -c '%a %U:%G' $SOCK)"
echo "tiến trình php-fpm của lồng: $(pgrep -u $UA -c php-fpm || true)"
long_vao $A
t0=$(date +%s.%N); c=$(code_a ''); t1=$(date +%s.%N)
echo "request đầu (dịch vụ tự khởi động): HTTP $c sau $(echo "$t1 - $t0" | bc) giây | B: HTTP $(code_b '')"
t0=$(date +%s.%N); c=$(code_a ''); t1=$(date +%s.%N)
echo "request thứ hai: HTTP $c sau $(echo "$t1 - $t0" | bc) giây"
echo "dịch vụ: $(systemctl is-active $SVC)"

echo; echo "## 2. Ai chạy, còn quyền gì"
ps -eo user:28,pid,ppid,cmd | grep "[p]hp-fpm" | cut -c1-120
MP=$(systemctl show -p MainPID --value $SVC)
echo "MainPID=$MP: $(grep -E '^(Uid|Gid|CapInh|CapPrm|CapEff|CapBnd|NoNewPrivs|Seccomp):' /proc/$MP/status | tr '\n' ' ' | tr '\t' ' ')"
echo "cgroup của tiến trình chính: $(cat /proc/$MP/cgroup)"
journalctl -u $SVC --no-pager -o cat | head -8

echo; echo "## 3. Từ PHP của A (shell_exec) nhìn thấy gì"
put_php $A s.php <<'PHP'
<?php
function sh($c){ return trim((string)@shell_exec($c." 2>&1")); }
echo "user=".posix_getpwuid(posix_geteuid())["name"]." uid=".posix_geteuid()."\n";
echo "ls /var/www: ".sh("ls /var/www | tr '\\n' ' '")."\n";
echo "ls /var: ".sh("ls /var | tr '\\n' ' '")."\n";
echo "ls /run: ".sh("ls /run | tr '\\n' ' '")."\n";
echo "ls /tmp: [".sh("ls -A /tmp | tr '\\n' ' '")."]  ls /dev/shm: [".sh("ls -A /dev/shm | tr '\\n' ' '")."]\n";
echo "ls /var/log/tpan/sites: ".sh("ls /var/log/tpan/sites | tr '\\n' ' '")."\n";
echo "ls /etc/tpan/cage: ".sh("ls /etc/tpan/cage | tr '\\n' ' '")."\n";
echo "tien trinh nhin thay (so thu muc so trong /proc): ".sh("ls /proc | grep -c '^[0-9]'")." | cua user khac: ".sh("for p in /proc/[0-9]*; do stat -c %u \$p; done | grep -vc '^".posix_geteuid()."\$'")."\n";
echo "getent passwd: ".sh("getent passwd | cut -d: -f1 | tr '\\n' ' '")."\n";
foreach (["/etc/nginx/sites-enabled","/etc/php/8.3/fpm/pool.d","/etc/cron.d","/etc/systemd/system","/sys/fs/cgroup","/var/lib/tpan","/root","/home","/boot","/run/php"] as $p)
  echo "ls $p: ".substr(sh("ls $p | head -3 | tr '\\n' ' '"),0,90)."\n";
echo "ghi /etc: ".(@file_put_contents("/etc/tpan-thu","x")!==false?"DUOC":"khong")." | touch /usr/x: ".sh("touch /usr/x && echo DUOC || echo khong")."\n";
echo "ghi vao site: ".(@file_put_contents(__DIR__."/thu.txt","x")!==false?"duoc":"KHONG")."\n"; @unlink(__DIR__."/thu.txt");
echo "/proc/meminfo: ".(is_readable("/proc/meminfo")?"doc duoc":"khong co")." | loadavg: ".json_encode(sys_getloadavg())."\n";
echo "id: ".sh("id")."\n";
PHP
ca s.php | sed 's/^/  /'

echo; echo "## 4. Lệnh chạy thay cho site qua systemd-run (unit tạm cùng tiền tố tên)"
run_in $A sh -c 'echo "  user=$(id -un) | ls /var/www: $(ls /var/www | tr "\n" " ")| ls /run: $(ls /run | tr "\n" " ")| /tmp: [$(ls -A /tmp)]"'
U=tpan-cage-$S-run-thu
systemd-run --quiet --collect --unit=$U -- sleep 30; sleep 0.3
echo "  unit tạm $U: Slice=$(systemctl show -p Slice --value $U) User=$(systemctl show -p User --value $U) ProtectSystem=$(systemctl show -p ProtectSystem --value $U) DropInPaths=$(systemctl show -p DropInPaths --value $U)"
echo "  cgroup: $(systemctl show -p ControlGroup --value $U)"
systemctl stop $U
run_in $A sh -c 'exit 7'; echo "  mã thoát của 'exit 7': $?"
echo "  stdin: $(echo xin-chao | run_in $A cat)"
run_in $A sh -c 'echo ra-stdout; echo ra-stderr >&2' >/tmp/o.txt 2>/tmp/e.txt; echo "  stdout=[$(cat /tmp/o.txt)] stderr=[$(cat /tmp/e.txt)]"
t0=$(date +%s.%N); run_in $A true; t1=$(date +%s.%N); echo "  systemd-run true: $(echo "$t1 - $t0" | bc) giây"
t0=$(date +%s.%N); runuser -u $UA -- true; t1=$(date +%s.%N); echo "  runuser true:     $(echo "$t1 - $t0" | bc) giây"
echo "  wp option get siteurl (trong lồng): $(run_in $A wp option get siteurl --path=/var/www/$A/public 2>&1 | tail -1)"
t0=$(date +%s.%N); run_in $A wp core is-installed --path=/var/www/$A/public; rc=$?; t1=$(date +%s.%N); echo "  wp core is-installed: mã $rc, $(echo "$t1 - $t0" | bc) giây"
t0=$(date +%s.%N); runuser -u $UA -- wp core is-installed --path=/var/www/$A/public; rc=$?; t1=$(date +%s.%N); echo "  (ngoài lồng, runuser): mã $rc, $(echo "$t1 - $t0" | bc) giây"

echo; echo "## 5. reload (USR2), restart, stop"
P1=$(systemctl show -p MainPID --value $SVC)
systemctl reload $SVC; sleep 1
echo "reload: MainPID $P1 -> $(systemctl show -p MainPID --value $SVC), dịch vụ $(systemctl is-active $SVC), HTTP $(code_a '')"
# 40 request liên tục trong lúc restart: đếm mã trả về.
( for i in $(seq 1 40); do code_a ''; echo; sleep 0.05; done > /tmp/rs.txt ) &
sleep 0.4; t0=$(date +%s.%N); systemctl restart $SVC; t1=$(date +%s.%N); wait
echo "restart mất $(echo "$t1 - $t0" | bc) giây; 40 request trong lúc đó: $(sort /tmp/rs.txt | uniq -c | tr '\n' ' ')"
systemctl stop $SVC
echo "stop: dịch vụ $(systemctl is-active $SVC) | socket $(systemctl is-active $SK) | file socket còn: $(test -S $SOCK && echo CO || echo MAT)"
t0=$(date +%s.%N); c=$(code_a ''); t1=$(date +%s.%N)
echo "request sau khi stop: HTTP $c sau $(echo "$t1 - $t0" | bc) giây (dịch vụ: $(systemctl is-active $SVC))"

echo; echo "## 6. systemd-analyze security"
systemd-analyze security php$V-fpm.service --no-pager | tail -1
systemd-analyze security $SVC --no-pager | tail -1
systemd-analyze security $SVC --no-pager | grep -v '✓' | head -30

echo; echo "## Dọn"
rm -f /var/www/$A/public/s.php
long_go $A
echo "sau khi dọn: A HTTP $(code_a ''), B HTTP $(code_b '')"
