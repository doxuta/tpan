# t3: các trần đặt ở slice của site A. B ở PHP-FPM chung và bị gọi liên tục để xem
# có rớt request nào không.
#  1. Trần RAM cứng (MemoryMax): cái gì bị giết, request đang dở ra mã gì, bao lâu
#     thì A trả 200 lại, B có rớt không, bộ đếm nào ghi lại.
#  2. Trần RAM mềm (MemoryHigh): khác gì.
#  3. Trần CPU: tỷ lệ bóp đo được so với trần.
#  4. Trần số tiến trình: sinh tiến trình liên tục.
#  5. Trần đọc ghi đĩa trên đĩa của máy ảo.
#  6. Đổi trần khi site đang chạy (ghi lại file slice + daemon-reload).
HANG_RAO_THEM="SystemCallFilter=~@privileged @resources"
S=$SA; SVC=tpan-cage-$S-php.service; CG=/sys/fs/cgroup/tpan.slice/tpan-$S.slice
dung(){ long_go $A >/dev/null 2>&1; long_dung $A "$@"; echo "[Service]
$HANG_RAO_THEM" > /etc/systemd/system/tpan-cage-$S-.service.d/20-them.conf; systemctl daemon-reload; long_vao $A >/dev/null; code_a '' >/dev/null; }
goi_b(){ # gọi B n lần, in số lần không phải 200
  local n=$1 i bad=0; for i in $(seq 1 $n); do [ "$(code_b '')" = 200 ] || bad=$((bad+1)); sleep 0.05; done; echo $bad; }
MPHP='<?php ini_set("memory_limit","-1"); $mb=(int)($_GET["mb"]??400); $a=[]; for($i=0;$i<$mb/10;$i++){ $a[]=str_repeat(chr(65+$i%26), 10*1024*1024); } echo "da cap phat $mb MB\n";'
CPHP='<?php $t=microtime(true); $x=0; for($i=0;$i<(int)($_GET["n"]??150000000);$i++){ $x+=$i%7; } printf("%.2f", microtime(true)-$t);'

echo "## 1. Trần RAM cứng: MemoryMax=200M, MemorySwapMax=0, OOMPolicy=continue"
dung 200 0 0
echo "$MPHP" | put_php $A m.php; echo "$CPHP" | put_php $A c.php; echo "$MPHP" | put_php $B m.php; echo "$CPHP" | put_php $B c.php
echo "cgroup: memory.max=$(cat $CG/memory.max) memory.swap.max=$(cat $CG/memory.swap.max)"
echo "trước: memory.events: $(tr '\n' ' ' < $CG/memory.events)| memory.peak=$(cat $CG/memory.peak)"
( goi_b 60 > /tmp/b-bad.txt ) &
sleep 0.5
t0=$(date +%s.%N); c=$(code_a 'm.php?mb=400'); t1=$(date +%s.%N)
echo "A cấp phát 400 MB: HTTP $c sau $(echo "$t1 - $t0" | bc) giây"
t0=$(date +%s.%N); n=0; while [ "$(code_a '')" != 200 ]; do n=$((n+1)); sleep 0.05; [ $n -gt 200 ] && break; done; t1=$(date +%s.%N)
echo "A trả 200 lại sau $(echo "$t1 - $t0" | bc) giây (thử $n lần hỏng trước đó)"
wait; echo "B: $(cat /tmp/b-bad.txt)/60 request không phải 200 trong lúc đó"
echo "sau: memory.events: $(tr '\n' ' ' < $CG/memory.events)| memory.peak=$(cat $CG/memory.peak) ($(( $(cat $CG/memory.peak) / 1048576 )) MB)"
echo "dịch vụ: $(systemctl is-active $SVC), NRestarts=$(systemctl show -p NRestarts --value $SVC), Result=$(systemctl show -p Result --value $SVC)"
journalctl -k --since "-1min" --no-pager -o cat 2>/dev/null | grep -i "killed process" | tail -1 | cut -c1-160
journalctl -u $SVC --since "-1min" --no-pager -o cat | grep -iE "oom|signal|exited" | tail -3
tail -1 /var/log/tpan/sites/$A/error.log | cut -c1-200
echo "-- B (không có trần) cấp phát 400 MB: HTTP $(code_b 'm.php?mb=400')"
echo "-- lệnh trong lồng (systemd-run) cấp phát 400 MB: $(run_in $A php -d memory_limit=-1 -r '$a=[]; for($i=0;$i<40;$i++){ $a[]=str_repeat("x",10*1024*1024);} echo "xong";' 2>&1 | tail -1); mã $?"
echo "   memory.events: $(tr '\n' ' ' < $CG/memory.events)"
echo "-- A sau đó: HTTP $(code_a '')"

echo; echo "## 1b. Cùng phép thử, KHÔNG có OOMPolicy=continue (mặc định của systemd: stop)"
sed -i '/^OOMPolicy=/d' /etc/systemd/system/$SVC; systemctl daemon-reload; systemctl restart $SVC; code_a '' >/dev/null
c=$(code_a 'm.php?mb=400'); sleep 0.3
echo "A cấp phát 400 MB: HTTP $c | dịch vụ ngay sau đó: $(systemctl is-active $SVC), Result=$(systemctl show -p Result --value $SVC)"
t0=$(date +%s.%N); n=0; while [ "$(code_a '')" != 200 ]; do n=$((n+1)); sleep 0.05; [ $n -gt 200 ] && break; done; t1=$(date +%s.%N)
echo "A trả 200 lại sau $(echo "$t1 - $t0" | bc) giây ($n lần hỏng) | NRestarts=$(systemctl show -p NRestarts --value $SVC)"

echo; echo "## 2. Trần RAM mềm: MemoryHigh=200M, không có MemoryMax"
dung 0 0 0
sed -i 's/^\[Slice\]/[Slice]\nMemoryHigh=200M\nMemorySwapMax=0/' /etc/systemd/system/tpan-$S.slice; systemctl daemon-reload
echo "cgroup: memory.high=$(cat $CG/memory.high) memory.max=$(cat $CG/memory.max)"
( sleep 2; t0=$(date +%s.%N); c=$(code_a ''); t1=$(date +%s.%N); echo "   trang chủ của A trong lúc bị bóp: HTTP $c sau $(echo "$t1 - $t0" | bc) giây" ) &
t0=$(date +%s.%N); c=$(ca 'm.php?mb=400' --max-time 40 -o /dev/null -w '%{http_code}'); t1=$(date +%s.%N)
wait
echo "A cấp phát 400 MB: HTTP $c sau $(echo "$t1 - $t0" | bc) giây (curl chờ tối đa 40 giây)"
echo "memory.events: $(tr '\n' ' ' < $CG/memory.events)| memory.current=$(( $(cat $CG/memory.current) / 1048576 )) MB"
systemctl restart $SVC

echo; echo "## 3. Trần CPU (máy ảo có $(nproc) nhân). Vòng lặp một luồng, n=300 triệu"
dung 0 0 0
base=$(ca 'c.php?n=300000000'); echo "không trần: $base giây"
for q in 100 75 50 25 10; do
  sed -i '/^CPUQuota=/d' /etc/systemd/system/tpan-$S.slice; sed -i "s/^\[Slice\]/[Slice]\nCPUQuota=${q}%/" /etc/systemd/system/tpan-$S.slice; systemctl daemon-reload
  s0=$(grep -E '^(usage_usec|nr_throttled|throttled_usec) ' $CG/cpu.stat | tr '\n' ' ')
  u0=$(awk '/^usage_usec/{print $2}' $CG/cpu.stat); w0=$(date +%s.%N)
  t=$(ca 'c.php?n=300000000')
  u1=$(awk '/^usage_usec/{print $2}' $CG/cpu.stat); w1=$(date +%s.%N)
  eff=$(echo "scale=1; ($u1 - $u0) / 10000 / ($w1 - $w0)" | bc)
  echo "CPUQuota=${q}% (cpu.max=$(cat $CG/cpu.max)): $t giây, chậm $(echo "scale=2; $t / $base" | bc) lần (mong đợi $(echo "scale=2; 100 / $q" | bc)); CPU thật dùng = ${eff}% | nr_throttled=$(awk '/^nr_throttled/{print $2}' $CG/cpu.stat) throttled_usec=$(awk '/^throttled_usec/{print $2}' $CG/cpu.stat)"
done
echo "-- B trong lúc A bị bóp ở 10%: $( (ca 'c.php?n=300000000' >/dev/null &) ; sleep 0.3; cb 'c.php?n=300000000') giây (không trần: $base)"
sleep 12

echo; echo "## 4. Trần số tiến trình: TasksMax=30"
dung 0 0 30
echo "cgroup: pids.max=$(cat $CG/pids.max) pids.current=$(cat $CG/pids.current)"
put_php $A f.php <<'PHP'
<?php $out = shell_exec('n=0; for i in $(seq 1 200); do (sleep 20 &) 2>/dev/null && n=$((n+1)); done; echo "vong lap xong"; ps -u $(id -u) --no-headers 2>/dev/null | wc -l') ; echo trim((string)$out);
PHP
echo "A sinh 200 tiến trình sleep: $(ca f.php | tail -2 | tr '\n' ' ')"
echo "pids.current=$(cat $CG/pids.current) pids.peak=$(cat $CG/pids.peak 2>/dev/null) pids.events: $(tr '\n' ' ' < $CG/pids.events)"
echo "trang chủ của A trong lúc đầy tiến trình: HTTP $(code_a '') | B: HTTP $(code_b '')"
echo "lệnh trong lồng lúc đầy: $(run_in $A true 2>&1 | tail -1) mã $?"
sleep 21; echo "sau 21 giây (các sleep đã thoát): pids.current=$(cat $CG/pids.current) | A: HTTP $(code_a '')"

echo; echo "## 5. Trần đọc ghi đĩa: IOWriteBandwidthMax=/var/www 4M, IOReadBandwidthMax=/var/www 4M"
dung 0 0 0
sed -i 's|^\[Slice\]|[Slice]\nIOWriteBandwidthMax=/var/www 4M\nIOReadBandwidthMax=/var/www 4M|' /etc/systemd/system/tpan-$S.slice; systemctl daemon-reload
systemctl restart $SVC; code_a '' >/dev/null
echo "controller bật cho slice cha: $(cat /sys/fs/cgroup/tpan.slice/cgroup.subtree_control) | io.max: $(cat $CG/io.max 2>&1)"
D=/var/www/$A/tmp
echo "ghi 32 MB, direct:   $(run_in $A dd if=/dev/zero of=$D/io.bin bs=1M count=32 oflag=direct 2>&1 | tail -1 | sed 's/.*copied, //')   (mong đợi khoảng 8 giây, 4 MB/s)"
echo "ghi 32 MB, có đệm + fsync: $(run_in $A dd if=/dev/zero of=$D/io2.bin bs=1M count=32 conv=fsync 2>&1 | tail -1 | sed 's/.*copied, //')"
sync; echo 3 > /proc/sys/vm/drop_caches
echo "đọc 32 MB (đã xoá cache): $(run_in $A dd if=$D/io.bin of=/dev/null bs=1M 2>&1 | tail -1 | sed 's/.*copied, //')"
echo "-- cùng việc ở ngoài lồng (user của B, không trần): ghi direct $(runuser -u $UB -- dd if=/dev/zero of=/var/www/$B/tmp/io.bin bs=1M count=32 oflag=direct 2>&1 | tail -1 | sed 's/.*copied, //')"
echo "io.stat của slice: $(cat $CG/io.stat 2>&1 | head -2 | tr '\n' ' ')"
rm -f $D/io.bin $D/io2.bin /var/www/$B/tmp/io.bin

echo; echo "## 6. Đổi trần khi site đang chạy"
dung 200 50 40
P=$(systemctl show -p MainPID --value $SVC)
echo "trước: memory.max=$(cat $CG/memory.max) cpu.max=$(cat $CG/cpu.max) pids.max=$(cat $CG/pids.max) | MainPID=$P"
sed -i 's/^MemoryMax=.*/MemoryMax=300M/; s/^CPUQuota=.*/CPUQuota=80%/; s/^TasksMax=.*/TasksMax=60/' /etc/systemd/system/tpan-$S.slice
t0=$(date +%s.%N); systemctl daemon-reload; t1=$(date +%s.%N)
echo "sau khi ghi file slice + daemon-reload ($(echo "$t1 - $t0" | bc) giây): memory.max=$(cat $CG/memory.max) cpu.max=$(cat $CG/cpu.max) pids.max=$(cat $CG/pids.max) | MainPID=$(systemctl show -p MainPID --value $SVC) | A: HTTP $(code_a '')"
sed -i '/^MemoryMax=/d; /^MemorySwapMax=/d; /^CPUQuota=/d; /^TasksMax=/d' /etc/systemd/system/tpan-$S.slice; systemctl daemon-reload
echo "gỡ hết trần + daemon-reload: memory.max=$(cat $CG/memory.max) cpu.max=$(cat $CG/cpu.max) pids.max=$(cat $CG/pids.max)"
echo "hạ trần RAM xuống dưới mức đang dùng (memory.current=$(( $(cat $CG/memory.current) / 1048576 )) MB -> MemoryMax=8M):"
sed -i 's/^\[Slice\]/[Slice]\nMemoryMax=8M\nMemorySwapMax=0/' /etc/systemd/system/tpan-$S.slice; systemctl daemon-reload; sleep 1
echo "   memory.max=$(cat $CG/memory.max) memory.current=$(( $(cat $CG/memory.current) / 1048576 )) MB | events: $(tr '\n' ' ' < $CG/memory.events)| dịch vụ $(systemctl is-active $SVC) | A: HTTP $(code_a '')"

echo; echo "## Dọn"
rm -f /var/www/$A/public/{m,c,f}.php /var/www/$B/public/{m,c}.php
long_go $A
echo "sau khi dọn: A HTTP $(code_a ''), B HTTP $(code_b '')"
