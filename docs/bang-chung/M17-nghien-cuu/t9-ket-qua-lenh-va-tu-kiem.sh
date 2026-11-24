# t9: hai việc còn lại trước khi chốt thiết kế.
#  a. Phân biệt "lệnh tự thoát mã 1" với "lệnh bị giết vì chạm trần RAM hay quá
#     giờ": systemd-run đều trả mã 1. Bỏ --collect thì unit hỏng còn lại để hỏi
#     Result, hỏi xong phải reset-failed.
#  b. Tự kiểm hàng rào chỉ bằng lệnh argv (không shell): đúng những lệnh phép kiểm
#     cage.fence sẽ chạy. Kèm danh sách cố định các bản PHP trong InaccessiblePaths
#     và www-data trong passwd riêng.
S=$SA; SVC=tpan-cage-$S-php.service; CG=/sys/fs/cgroup/tpan.slice/tpan-$S.slice
long_go $A >/dev/null 2>&1; long_dung $A 200 0 0
D=/etc/systemd/system/tpan-cage-$S-.service.d
{ echo "[Service]"; echo "SystemCallFilter=~@privileged @resources"
  for v in 5.6 7.0 7.1 7.2 7.3 7.4 8.0 8.1 8.2 8.3 8.4 8.5 8.6 8.7 8.8 8.9 9.0 9.1; do echo "InaccessiblePaths=-/etc/php/$v/fpm/pool.d"; done; } > $D/20-them.conf
{ getent passwd root www-data "$UA" nobody; } > /etc/tpan/cage/$S/passwd
{ getent group root www-data "$UA" nogroup; } > /etc/tpan/cage/$S/group
systemctl daemon-reload; long_vao $A >/dev/null; echo "A: HTTP $(code_a '')"
rin(){ local u=$1; shift; systemd-run --quiet --wait --pipe --expand-environment=no -E LC_ALL=C --unit="$u" "$@"; }

echo; echo "## a. Kết quả của lệnh (không --collect)"
hoi(){ local u=$1; echo "   unit $u: LoadState=$(systemctl show -p LoadState --value $u) ActiveState=$(systemctl show -p ActiveState --value $u) Result=$(systemctl show -p Result --value $u) ExecMainCode=$(systemctl show -p ExecMainCode --value $u) ExecMainStatus=$(systemctl show -p ExecMainStatus --value $u)"; }
rin tpan-cage-$S-run-ok -- true; echo "true: mã $?"; hoi tpan-cage-$S-run-ok.service
rin tpan-cage-$S-run-e3 -- sh -c 'exit 3'; echo "exit 3: mã $?"; hoi tpan-cage-$S-run-e3.service
rin tpan-cage-$S-run-oom -- php -d memory_limit=-1 -r '$a=[]; for($i=0;$i<40;$i++){ $a[]=str_repeat("x",10*1024*1024);} echo "xong\n";'; echo "vượt RAM: mã $?"; hoi tpan-cage-$S-run-oom.service
rin tpan-cage-$S-run-to -p RuntimeMaxSec=1 -- sleep 30; echo "quá giờ: mã $?"; hoi tpan-cage-$S-run-to.service
rin tpan-cage-$S-run-sig -- sh -c 'kill -TERM $$'; echo "tự chết vì SIGTERM: mã $?"; hoi tpan-cage-$S-run-sig.service
rin tpan-cage-$S-run-nf -- khong-co-lenh-nay; echo "không có chương trình: mã $?"; hoi tpan-cage-$S-run-nf.service
echo "unit hỏng còn lại: $(systemctl list-units --all --state=failed 'tpan-cage-*-run-*' --no-legend | wc -l)"
systemctl reset-failed "tpan-cage-$S-run-*" 2>&1 | head -2
echo "sau reset-failed 'tpan-cage-$S-run-*': $(systemctl list-units --all 'tpan-cage-*-run-*' --no-legend | wc -l) unit"
echo "dịch vụ PHP không bị reset nhầm: $(systemctl is-active $SVC)"

echo; echo "## b. Tự kiểm hàng rào bằng lệnh argv"
k(){ printf '  %-58s -> ' "$*"; out=$(rin tpan-cage-$S-run-k$RANDOM -- "$@" 2>&1); rc=$?; echo "mã $rc [$(echo "$out" | tr '\n' ' ' | cut -c1-110)]"; systemctl reset-failed "tpan-cage-$S-run-*" 2>/dev/null; }
k id -un
k ls -A /var/www
k ls -A /tmp
k ls -A /run
k find /proc -maxdepth 1 -type d -regex '/proc/[0-9]+' ! -user $UA
k getent passwd
k test -w /etc
k test -w /usr
k test -w /var/www/$A/public
k test -e /etc/nginx/nginx.conf
k ls /etc/php/8.3/fpm/pool.d
k ls /etc/php/8.4/fpm/pool.d
k ls /etc/php/8.3/fpm/conf.d
k ls /var/log/tpan/sites
k ls /sys/fs/cgroup
k cat /proc/1/cmdline
k ps -eo user=
echo "  getent group www-data trong lồng: $(rin tpan-cage-$S-run-g -- getent group www-data 2>&1)"

echo; echo "## c. systemctl try-restart, socket đang tắt thì không bật lên"
systemctl stop $SVC; systemctl try-restart $SVC; echo "try-restart khi dịch vụ đang ngủ: dịch vụ $(systemctl is-active $SVC) (vẫn ngủ là đúng)"
code_a '' >/dev/null; P=$(systemctl show -p MainPID --value $SVC); systemctl try-restart $SVC; echo "try-restart khi đang chạy: MainPID $P -> $(systemctl show -p MainPID --value $SVC), A: HTTP $(code_a '')"
echo "phiên bản systemd: $(systemctl --version | head -1 | cut -d' ' -f2) | cgroup v2: $(test -f /sys/fs/cgroup/cgroup.controllers && echo co || echo khong)"

echo; echo "## Dọn"
long_go $A
echo "sau khi dọn: A HTTP $(code_a ''), B HTTP $(code_b '')"
