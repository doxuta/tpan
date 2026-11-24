# t7a: chuẩn bị rồi khởi động lại máy (t7b chạy sau khi máy lên).
#   A: trong lồng, đang chạy, trần RAM 200M, đã chạm trần một lần.
#   B: trong lồng nhưng "tạm khoá" (socket disable + stop).
#   C: trong lồng, đang ngủ (dịch vụ dừng, socket bật).
# Trước khi khởi động lại: daemon-reload lúc lồng đang chạy có làm gì không.
C=c.example.com; SC=$(slug_of $C)
for d in $A $B $C; do long_go $d >/dev/null 2>&1; done
long_dung $A 200 50 40; long_vao $A >/dev/null
long_dung $B 200 50 40; long_vao $B >/dev/null
long_dung $C 200 50 40; long_vao $C >/dev/null
cc(){ curl -s --max-time 60 --resolve $C:80:127.0.0.1 -o /dev/null -w '%{http_code}' "http://$C/"; }
echo "trước: A $(code_a '') B $(code_b '') C $(cc)"
echo '<?php ini_set("memory_limit","-1"); $a=[]; for($i=0;$i<40;$i++){ $a[]=str_repeat("x", 10*1024*1024); } echo "xong";' | put_php $A m.php
echo "A chạm trần RAM: HTTP $(code_a m.php)"; rm -f /var/www/$A/public/m.php
CG=/sys/fs/cgroup/tpan.slice/tpan-$SA.slice
echo "A: memory.max=$(cat $CG/memory.max) oom_kill=$(awk '/^oom_kill /{print $2}' $CG/memory.events) memory.peak=$(cat $CG/memory.peak) usage_usec=$(awk '/^usage_usec/{print $2}' $CG/cpu.stat)"

echo; echo "## daemon-reload khi lồng đang chạy"
P=$(systemctl show -p MainPID --value tpan-cage-$SA-php.service)
( for i in $(seq 1 40); do code_a ''; echo; sleep 0.02; done > /tmp/dr.txt ) &
sleep 0.3; systemctl daemon-reload; systemctl daemon-reload; wait
echo "MainPID $P -> $(systemctl show -p MainPID --value tpan-cage-$SA-php.service) | 40 request trong lúc đó: $(sort /tmp/dr.txt | uniq -c | tr '\n' ' ')"
echo "bộ đếm sau daemon-reload: oom_kill=$(awk '/^oom_kill /{print $2}' $CG/memory.events) memory.peak=$(cat $CG/memory.peak)"

echo; echo "## Đặt trạng thái trước khi khởi động lại"
systemctl disable --now --quiet tpan-cage-$SB-php.socket; systemctl stop tpan-cage-$SB-php.service
systemctl stop tpan-cage-$SC-php.service
for d in $A $B $C; do s=$(slug_of $d); echo "$d: socket $(systemctl is-active tpan-cage-$s-php.socket)/$(systemctl is-enabled tpan-cage-$s-php.socket 2>&1) | dịch vụ $(systemctl is-active tpan-cage-$s-php.service)"; done
echo "A $(code_a '') B $(code_b '') C(ngủ, request này đánh thức) $(cc)"; systemctl stop tpan-cage-$SC-php.service
date +%s > /root/t7-truoc.txt
echo "uptime trước: $(cut -d' ' -f1 /proc/uptime) giây"
