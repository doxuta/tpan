# t3b: làm rõ bốn chỗ t3 để lại.
#  a. Lệnh chạy trong lồng (systemd-run) vượt trần RAM: kết quả, mã thoát, bộ đếm.
#     OOMPolicy thật của dịch vụ khi không ghi gì.
#  b. Sinh tiến trình ở trần TasksMax: bao nhiêu cái lên được, lỗi gì, bộ đếm
#     pids.events nằm ở cgroup nào (kernel 6.8).
#  c. Trần CPU chính xác tới đâu: máy rảnh so với máy bận (giả thuyết: ngắt hẹn
#     giờ của vCPU rảnh đến trễ nên chu kỳ 100 ms dài ra).
#  d. restart dưới tải khi đã có process_control_timeout: còn request nào hỏng.
#  e. tar giải nén trong lồng khi đã lọc @privileged.
S=$SA; SVC=tpan-cage-$S-php.service; CG=/sys/fs/cgroup/tpan.slice/tpan-$S.slice
dung(){ long_go $A >/dev/null 2>&1; long_dung $A "$@"; echo "[Service]
SystemCallFilter=~@privileged @resources" > /etc/systemd/system/tpan-cage-$S-.service.d/20-them.conf; systemctl daemon-reload; long_vao $A >/dev/null; code_a '' >/dev/null; }

echo "## a. Lệnh trong lồng vượt trần RAM 200M"
dung 200 0 0
echo "OOMPolicy của dịch vụ (unit ghi continue): $(systemctl show -p OOMPolicy --value $SVC) | mặc định của máy: $(systemctl show -p DefaultOOMPolicy --value)"
run_in $A php -d memory_limit=-1 -r '$a=[]; for($i=0;$i<40;$i++){ $a[]=str_repeat("x",10*1024*1024);} echo "xong\n";' >/tmp/o.txt 2>/tmp/e.txt; rc=$?
echo "php cấp phát 400 MB qua systemd-run: mã thoát $rc | stdout=[$(cat /tmp/o.txt)] stderr=[$(head -c 200 /tmp/e.txt)]"
echo "memory.events của slice: $(tr '\n' ' ' < $CG/memory.events)| dịch vụ PHP: $(systemctl is-active $SVC) | A: HTTP $(code_a '')"
sed -i '/^OOMPolicy=/d' /etc/systemd/system/$SVC; systemctl daemon-reload; systemctl restart $SVC; code_a '' >/dev/null
echo "bỏ dòng OOMPolicy khỏi unit -> OOMPolicy=$(systemctl show -p OOMPolicy --value $SVC)"
echo '<?php ini_set("memory_limit","-1"); $a=[]; for($i=0;$i<40;$i++){ $a[]=str_repeat("x", 10*1024*1024); } echo "xong";' | put_php $A m.php
c=$(code_a m.php); sleep 1.5
echo "A cấp phát 400 MB: HTTP $c | 1,5 giây sau: dịch vụ $(systemctl is-active $SVC), Result=$(systemctl show -p Result --value $SVC), NRestarts=$(systemctl show -p NRestarts --value $SVC)"
journalctl -u $SVC --since "-10s" --no-pager -o cat | grep -iE "oom|Failed|Scheduled|Stopp|Started" | tail -5 | sed 's/^/   /'
echo "A: HTTP $(code_a '')"

echo; echo "## b. TasksMax=30: sinh 200 tiến trình"
dung 0 0 30
put_php $A f.php <<'PHP'
<?php
$t = microtime(true);
exec('i=0; while [ $i -lt 200 ]; do sleep 25 >/dev/null 2>&1 & i=$((i+1)); done; echo het-vong 2>&1', $out, $rc);
printf("ma %d, %.2f giay, output: %s\n", $rc, microtime(true) - $t, substr(implode(" | ", $out), 0, 300));
PHP
t0=$(date +%s.%N); r=$(ca f.php --max-time 50); t1=$(date +%s.%N)
echo "request sinh tiến trình: [$r] ($(echo "$t1 - $t0" | bc) giây)"
echo "slice:   pids.current=$(cat $CG/pids.current) pids.peak=$(cat $CG/pids.peak) pids.max=$(cat $CG/pids.max) pids.events: $(tr '\n' ' ' < $CG/pids.events)"
echo "dịch vụ: pids.current=$(cat $CG/$SVC/pids.current) pids.events: $(tr '\n' ' ' < $CG/$SVC/pids.events)"
t0=$(date +%s.%N); c=$(code_a '' --max-time 20); t1=$(date +%s.%N)
echo "trang chủ của A lúc đang đầy: HTTP $c sau $(echo "$t1 - $t0" | bc) giây | B: HTTP $(code_b '')"
run_in $A true 2>/tmp/e.txt; echo "lệnh trong lồng lúc đầy: mã $? [$(head -c 160 /tmp/e.txt)]"
tail -2 /var/log/tpan/sites/$A/error.log | cut -c1-220
journalctl -u $SVC --since "-40s" --no-pager -o cat | grep -iE "fork|resource|unable" | tail -3 | sed 's/^/   journal: /'
sleep 26; echo "sau khi các sleep thoát: pids.current=$(cat $CG/pids.current) | A: HTTP $(code_a '')"

echo; echo "## c. Trần CPU: máy rảnh và máy bận"
dung 0 0 0
do_cpu(){ # nhãn, quota%: đốt CPU 10 giây trong lồng, đo phần CPU thật nhận được
  local q=$2 u0 u1 p0 p1 n0 n1 th0 th1 w0 w1
  sed -i '/^CPUQuota=/d' /etc/systemd/system/tpan-$S.slice; sed -i "s/^\[Slice\]/[Slice]\nCPUQuota=${q}%/" /etc/systemd/system/tpan-$S.slice; systemctl daemon-reload
  u0=$(awk '/^usage_usec/{print $2}' $CG/cpu.stat); p0=$(awk '/^nr_periods/{print $2}' $CG/cpu.stat); n0=$(awk '/^nr_throttled/{print $2}' $CG/cpu.stat); w0=$(date +%s.%N)
  run_in $A timeout 10 sh -c 'while :; do :; done'
  u1=$(awk '/^usage_usec/{print $2}' $CG/cpu.stat); p1=$(awk '/^nr_periods/{print $2}' $CG/cpu.stat); n1=$(awk '/^nr_throttled/{print $2}' $CG/cpu.stat); w1=$(date +%s.%N)
  local wall eff per; wall=$(echo "$w1 - $w0" | bc); eff=$(echo "scale=1; ($u1 - $u0) / 10000 / $wall" | bc)
  per=$(echo "scale=1; $wall * 1000 / ($p1 - $p0 + 1)" | bc)
  printf '  %-10s trần %3s%%: nhận %5s%% (%s%% của trần) | %s chu kỳ trong %s giây = %s ms mỗi chu kỳ (đặt 100) | bị bóp %s chu kỳ\n' "$1" "$q" "$eff" "$(echo "scale=0; $eff * 100 / $q" | bc)" "$((p1 - p0))" "$wall" "$per" "$((n1 - n0))"
}
for q in 100 50 25; do do_cpu "máy rảnh" $q; done
# Máy bận: mỗi nhân một vòng lặp ưu tiên thấp nhất, ngoài lồng, để vCPU không ngủ.
for i in $(seq 1 $(nproc)); do nice -n 19 sh -c 'while :; do :; done' & done; sleep 1
for q in 100 50 25; do do_cpu "máy bận" $q; done
kill $(jobs -p) 2>/dev/null; wait 2>/dev/null
echo "  clocksource: $(cat /sys/devices/system/clocksource/clocksource0/current_clocksource) | HZ: $(grep -E '^CONFIG_HZ=' /boot/config-$(uname -r))"

echo; echo "## d. restart dưới tải, process_control_timeout=10s (5 lượt, mỗi lượt 60 request)"
dung 0 0 0
echo '<?php usleep(150000); echo "ok";' | put_php $A w.php
for k in 1 2 3 4 5; do
  ( for i in $(seq 1 60); do code_a w.php; echo; done > /tmp/rs$k.txt ) &
  sleep 1.5; t0=$(date +%s.%N); systemctl restart $SVC; t1=$(date +%s.%N); wait
  echo "  lượt $k: restart $(echo "$t1 - $t0" | bc) giây | mã: $(sort /tmp/rs$k.txt | uniq -c | tr '\n' ' ')"
done
echo "  reload (USR2) dưới tải:"
( for i in $(seq 1 60); do code_a w.php; echo; done > /tmp/rl.txt ) &
sleep 1.5; systemctl reload $SVC; wait; echo "  mã: $(sort /tmp/rl.txt | uniq -c | tr '\n' ' ')"

echo; echo "## e. tar giải nén trong lồng (có lọc @privileged)"
run_in $A sh -c "mkdir -p /var/www/$A/tmp/x && tar -xzf /var/cache/tpan/wordpress-*.tar.gz -C /var/www/$A/tmp/x 2>&1 | tail -2; echo \"mã tar: \$?; số file: \$(find /var/www/$A/tmp/x -type f | wc -l)\"; rm -rf /var/www/$A/tmp/x"
run_in $A sh -c "cp -p /var/www/$A/public/index.php /var/www/$A/tmp/i.php; echo \"cp -p: mã \$?\"; chgrp www-data /var/www/$A/tmp/i.php 2>&1; rm -f /var/www/$A/tmp/i.php"

echo; echo "## Dọn"
rm -f /var/www/$A/public/{m,f,w}.php
long_go $A
echo "sau khi dọn: A HTTP $(code_a ''), B HTTP $(code_b '')"
