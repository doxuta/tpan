# t5: chi phí của lồng với 10 site.
#  a. RAM: 10 site ở PHP-FPM chung so với 10 site mỗi cái một lồng (chỉ còn tiến
#     trình chính, sau khi tiến trình con ondemand đã thoát). Đo MemAvailable của
#     máy, và RSS/PSS/USS của từng tiến trình chính, memory.current của từng slice.
#  b. Thời gian khởi động một lồng; request đầu khi dịch vụ đang ngủ (socket đánh thức).
#  c. Độ trễ thêm của mỗi request: cùng trang chủ WordPress, trong lồng so với ngoài.
SITES="a b c d e f g h i j"
for x in $SITES; do d=$x.example.com
  [ -d /var/www/$d ] || tpan site add $d --email admin@example.com --yes >/dev/null 2>&1 || echo "tạo $d hỏng"
done
echo "số site: $(ls /var/www | grep -c example.com)"
hit_all(){ for x in $SITES; do d=$x.example.com; curl -s -o /dev/null --max-time 60 --resolve $d:80:127.0.0.1 http://$d/; done; }
avail(){ awk '/^MemAvailable/{print $2}' /proc/meminfo; }
roll(){ awk '/^Rss:/{r=$2} /^Pss:/{p=$2} /^Private_Clean:/{u+=$2} /^Private_Dirty:/{u+=$2} END{printf "RSS %.1f MB, PSS %.1f MB, USS %.1f MB", r/1024, p/1024, u/1024}' /proc/$1/smaps_rollup; }

echo; echo "## a. RAM"
for x in $SITES; do long_go $x.example.com >/dev/null 2>&1; done
systemctl restart php$V-fpm; sleep 1; hit_all; hit_all; sleep 14; sync; echo 1 > /proc/sys/vm/drop_caches; sleep 1
M0=$(avail)
echo "10 site ở PHP-FPM chung (tiến trình con đã thoát): MemAvailable=$((M0/1024)) MB | tiến trình php-fpm: $(pgrep -c php-fpm)"
echo "  tiến trình chính chung: $(roll $(systemctl show -p MainPID --value php$V-fpm))"
for x in $SITES; do d=$x.example.com; long_dung $d 256 100 60; long_vao $d >/dev/null; done
hit_all; hit_all; sleep 14; sync; echo 1 > /proc/sys/vm/drop_caches; sleep 1
M1=$(avail)
echo "10 site mỗi cái một lồng (chỉ còn tiến trình chính): MemAvailable=$((M1/1024)) MB | tiến trình php-fpm: $(pgrep -c php-fpm)"
echo "  chênh: $(( (M0-M1)/1024 )) MB cho 10 lồng = $(( (M0-M1)/10240 )) MB mỗi lồng"
tot=0; for x in $SITES; do d=$x.example.com; s=$(slug_of $d); p=$(systemctl show -p MainPID --value tpan-cage-$s-php.service)
  mc=$(cat /sys/fs/cgroup/tpan.slice/tpan-$s.slice/memory.current); tot=$((tot+mc))
  [ "$x" = a ] || [ "$x" = j ] && echo "  $d: $(roll $p) | memory.current của slice $((mc/1048576)) MB (anon $(awk '/^anon /{printf "%.1f", $2/1048576}' /sys/fs/cgroup/tpan.slice/tpan-$s.slice/memory.stat), file $(awk '/^file /{printf "%.1f", $2/1048576}' /sys/fs/cgroup/tpan.slice/tpan-$s.slice/memory.stat), shmem $(awk '/^shmem /{printf "%.1f", $2/1048576}' /sys/fs/cgroup/tpan.slice/tpan-$s.slice/memory.stat) MB)"
done
echo "  tổng memory.current của 10 slice: $((tot/1048576)) MB"
# Cho mọi lồng ngủ: dừng dịch vụ, chỉ còn socket.
for x in $SITES; do systemctl stop tpan-cage-$(slug_of $x.example.com)-php.service 2>/dev/null; done; sleep 1; sync; echo 1 > /proc/sys/vm/drop_caches; sleep 1
M2=$(avail)
echo "10 lồng đang ngủ (dừng dịch vụ, socket vẫn nghe): MemAvailable=$((M2/1024)) MB (so với PHP-FPM chung: $(( (M0-M2)/1024 )) MB) | tiến trình php-fpm: $(pgrep -c php-fpm)"

echo; echo "## b. Thời gian khởi động"
# Lần chạy đầu của t5 đo sai ở đây: 5 lần start liền nhau chạm giới hạn mặc định
# của systemd (5 lần trong 10 giây), socket vào failed và mọi request sau đó là 502
# trả về trong 0,2 ms. Giờ in cả mã HTTP, và lib.sh đã nới StartLimit.
s=$SA
for i in 1 2 3 4 5; do systemctl stop tpan-cage-$s-php.service 2>/dev/null; t0=$(date +%s.%N); systemctl start tpan-cage-$s-php.service; t1=$(date +%s.%N); printf '  start dịch vụ: %s giây\n' "$(echo "$t1 - $t0" | bc)"; sleep 3; done | tr '\n' ' '; echo
for i in 1 2 3 4 5; do systemctl stop tpan-cage-$s-php.service 2>/dev/null; sleep 3; printf '  request đầu khi đang ngủ: %s' "$(ca '' -o /dev/null -w 'HTTP %{http_code}, %{time_total} giây')"; printf ' | request kế: %s\n' "$(ca '' -o /dev/null -w 'HTTP %{http_code}, %{time_total} giây')"; done
echo "  socket: $(systemctl is-active tpan-cage-$s-php.socket) | dịch vụ: $(systemctl is-active tpan-cage-$s-php.service)"
echo "  (để so) B ở PHP-FPM chung sau khi tiến trình con đã thoát:"
long_go $B >/dev/null 2>&1; sleep 12
for i in 1 2 3; do printf '  B request đầu (ondemand sinh tiến trình con): %s | kế: %s\n' "$(cb '' -o /dev/null -w '%{time_total}')" "$(cb '' -o /dev/null -w '%{time_total}')"; sleep 11; done

echo; echo "## c. Độ trễ mỗi request: 300 lượt xen kẽ, A trong lồng và B ngoài lồng (ms)"
for i in $(seq 1 20); do ca '' -o /dev/null; cb '' -o /dev/null; done
echo "  mã HTTP trước khi đo: A $(code_a ''), B $(code_b '')"
: > /tmp/la.txt; : > /tmp/lb.txt
for i in $(seq 1 300); do ca '' -o /dev/null -w '%{time_total}\n' >> /tmp/la.txt; cb '' -o /dev/null -w '%{time_total}\n' >> /tmp/lb.txt; done
st(){ sort -n $1 | awk '{a[NR]=$1; s+=$1} END{printf "trung bình %.1f, trung vị %.1f, p95 %.1f, lớn nhất %.1f", s/NR*1000, a[int(NR/2)]*1000, a[int(NR*0.95)]*1000, a[NR]*1000}'; }
echo "  A (trong lồng): $(st /tmp/la.txt)"
echo "  B (ngoài lồng): $(st /tmp/lb.txt)"
echo '<?php echo "ok";' | put_php $A p.php; echo '<?php echo "ok";' | put_php $B p.php
: > /tmp/la.txt; : > /tmp/lb.txt
for i in $(seq 1 300); do ca p.php -o /dev/null -w '%{time_total}\n' >> /tmp/la.txt; cb p.php -o /dev/null -w '%{time_total}\n' >> /tmp/lb.txt; done
echo "  file PHP một dòng, A: $(st /tmp/la.txt)"
echo "  file PHP một dòng, B: $(st /tmp/lb.txt)"
rm -f /var/www/$A/public/p.php /var/www/$B/public/p.php

echo; echo "## Dọn (giữ 10 site cho các bản thử sau, gỡ hết lồng)"
for x in $SITES; do long_go $x.example.com >/dev/null 2>&1; done
systemctl restart php$V-fpm; sleep 1
echo "sau khi dọn: A HTTP $(code_a ''), B HTTP $(code_b '') | unit tpan-cage còn: $(systemctl list-units --all 'tpan-*' --no-legend | wc -l) | /etc/tpan: $(ls /etc/tpan 2>&1 | tr '\n' ' ')"
