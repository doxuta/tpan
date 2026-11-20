# t7b: chạy sau khi máy ảo khởi động lại (t7a đã đặt trạng thái).
C=c.example.com; SC=$(slug_of $C)
cc(){ curl -s --max-time 60 --resolve $C:80:127.0.0.1 -o /dev/null -w '%{http_code}' "http://$C/"; }
echo "uptime sau: $(cut -d' ' -f1 /proc/uptime) giây (máy đã khởi động lại: $([ $(cut -d. -f1 /proc/uptime) -lt 600 ] && echo CO || echo KHONG))"
echo "## 1. Ngay sau khi lên, chưa gọi request nào"
for d in $A $B $C; do s=$(slug_of $d); echo "$d: socket $(systemctl is-active tpan-cage-$s-php.socket)/$(systemctl is-enabled tpan-cage-$s-php.socket 2>&1) | dịch vụ $(systemctl is-active tpan-cage-$s-php.service) | file socket $(test -S /run/tpan-cage/$s/php.sock && stat -c '%a %U:%G' /run/tpan-cage/$s/php.sock || echo KHONG-CO) | slice cgroup $(test -d /sys/fs/cgroup/tpan.slice/tpan-$s.slice && echo co || echo chua-co)"; done
echo "tiến trình php-fpm của các lồng: $(pgrep -u $UA,$UB,tp_$SC -c php-fpm)"
echo; echo "## 2. Gọi request"
echo "A: HTTP $(code_a '') | B (đang 'tạm khoá'): HTTP $(code_b '') | C: HTTP $(cc)"
CG=/sys/fs/cgroup/tpan.slice/tpan-$SA.slice
echo "A sau request đầu: dịch vụ $(systemctl is-active tpan-cage-$SA-php.service) | memory.max=$(cat $CG/memory.max) cpu.max=$(cat $CG/cpu.max) pids.max=$(cat $CG/pids.max)"
echo "bộ đếm của A sau khởi động lại: oom_kill=$(awk '/^oom_kill /{print $2}' $CG/memory.events) memory.peak=$(cat $CG/memory.peak) usage_usec=$(awk '/^usage_usec/{print $2}' $CG/cpu.stat)"
echo "B vẫn tắt: socket $(systemctl is-active tpan-cage-$SB-php.socket) dịch vụ $(systemctl is-active tpan-cage-$SB-php.service) tiến trình $(pgrep -u $UB -c php-fpm)"
echo "lệnh trong lồng của B khi B tạm khoá vẫn chạy được: $(run_in $B id -un 2>&1 | tail -1)"

echo; echo "## 3. Dịch vụ dừng hẳn thì slice và bộ đếm còn không"
systemctl stop tpan-cage-$SA-php.service; sleep 1
echo "sau khi stop dịch vụ của A: slice $(systemctl is-active tpan-$SA.slice) | thư mục cgroup $(test -d $CG && echo con || echo MAT) | memory.peak=$(cat $CG/memory.peak 2>&1 | cut -c1-40)"
echo "A: HTTP $(code_a '') | memory.peak=$(cat $CG/memory.peak) usage_usec=$(awk '/^usage_usec/{print $2}' $CG/cpu.stat)"

echo; echo "## Dọn"
for d in $A $B $C; do long_go $d >/dev/null 2>&1; done
echo "sau khi dọn: A HTTP $(code_a ''), B HTTP $(code_b ''), C HTTP $(cc)"
