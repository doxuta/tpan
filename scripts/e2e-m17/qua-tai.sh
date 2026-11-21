#!/bin/bash
# Bốn phép quá tải của lồng (e2e M17: RAM, số tiến trình, CPU, đĩa; mục 2.4,
# K1.6 và bảng e2e ở mục 7 của docs/thay-doi/M17-thiet-ke.md). Mã thử là file
# PHP đặt vào public của A (site trong lồng), gọi qua curl --resolve; B là site
# cùng loại ở lồng khác, bị gọi liên tục để xem có rớt request nào không. CHỈ
# chạy trong máy ảo test của mốc, bằng root. Trên Mac, trong thư mục repo:
#
#   scripts/vm.sh root bash -s -- <chế độ> <A> <B> [O] < scripts/e2e-m17/qua-tai.sh
#
# Chế độ (mỗi kết quả là một dòng "<khoá>=<giá trị>"):
#   ram     A cấp phát 400 MB (trần của gói thấp hơn): RAM_A_400_HTTP, thời gian
#           A trả 200 lại (RAM_A_LAI_200_GIAY), RAM_B_HONG trên RAM_B_TONG
#           request, OOM_KILL, memory.peak, NRESTARTS trước và sau
#   tasks   A sinh 200 tiến trình ngủ 25 giây: TASKS_PIDS_CURRENT, _PEAK, _MAX
#           ngay khi request xong, TASKS_HITS (cộng "max" của mọi pids.events
#           dưới slice), TASKS_B_HONG, thời gian đợi tiến trình thoát, TASKS_A_SAU
#   cpu     máy bận (mỗi nhân một vòng lặp ưu tiên thấp nhất ngoài lồng, mục 2.4):
#           hai lần A đốt CPU 10 giây, CPU_BAN_<n>_NHAN là phần CPU thật (% một
#           nhân) A nhận; thêm một lần lúc máy rảnh (chỉ ghi nhận)
#   io      (cần O, một site ngoài lồng) A và O ghi 32 MB trực tiếp lên đĩa:
#           IO_A_GIAY, IO_NGOAI_GIAY, IO_B_HONG, IO_MAX (io.max của slice A)
#
# File PHP thử mang tên e2e-m17-*.php và bị xoá ở cuối. Toàn bộ nằm trong hàm
# và dòng cuối gọi main (xem cgroup.sh).
set -u

A=""
B=""
O=""
CG=""
BG_PIDS=()

slug_of() { stat -c %U "/var/www/$1" | sed 's/^tp_//'; }
epoch() { local t=$EPOCHREALTIME; echo "${t/,/.}"; }
# get <domain> <đường dẫn> [tuỳ chọn curl]: gọi site qua 127.0.0.1.
get() {
  local d=$1 p=$2
  shift 2
  curl -s --max-time 60 --resolve "$d:80:127.0.0.1" "$@" "http://$d/$p"
}
code_of() { get "$1" "$2" -o /dev/null -w '%{http_code}'; }
# put_php <domain> <tên file>: nội dung từ stdin, thuộc user của site, 640.
put_php() {
  local d=$1 name=$2 u t
  u=$(stat -c %U "/var/www/$d")
  t=$(mktemp)
  cat >"$t"
  install -o "$u" -g www-data -m 640 "$t" "/var/www/$d/public/$name"
  rm -f "$t"
}
don() {
  local p
  rm -f "/var/www/$A/public/"e2e-m17-*.php "/var/www/$B/public/"e2e-m17-*.php
  [ -n "$O" ] && rm -f "/var/www/$O/public/"e2e-m17-*.php
  for p in ${BG_PIDS[@]+"${BG_PIDS[@]}"}; do kill "$p" 2>/dev/null; done
  rm -f /root/e2e-m17-b.txt
}
# goi_b <số request>: gọi trang chủ B, ghi số request không phải 200 vào /root/e2e-m17-b.txt.
goi_b() {
  local n=$1 i bad=0
  for i in $(seq 1 "$n"); do
    [ "$(code_of "$B" '')" = 200 ] || bad=$((bad + 1))
    sleep 0.05
  done
  echo "$bad" >/root/e2e-m17-b.txt
}
num() { awk -v a="$1" -v b="$2" 'BEGIN { printf "%.2f", b - a }'; }
cg_val() { awk -v k="$2" '$1 == k { print $2; exit }' "$CG/$1"; }

modo_ram() {
  local c t0 t1 n nr0 nr1 bp
  put_php "$A" e2e-m17-m.php <<'PHP'
<?php ini_set("memory_limit", "-1"); $mb = (int)($_GET["mb"] ?? 400); $a = []; for ($i = 0; $i < $mb / 10; $i++) { $a[] = str_repeat(chr(65 + $i % 26), 10 * 1024 * 1024); } echo "da cap phat $mb MB\n";
PHP
  get "$A" '' -o /dev/null
  echo "MEMORY_MAX=$(cat "$CG/memory.max")"
  echo "OOM_KILL_TRUOC=$(cg_val memory.events oom_kill)"
  nr0=$(systemctl show -p NRestarts --value "tpan-cage-$S-php.service")
  goi_b 60 &
  bp=$!
  BG_PIDS+=("$bp")
  sleep 0.5
  c=$(code_of "$A" 'e2e-m17-m.php?mb=400')
  echo "RAM_A_400_HTTP=$c"
  t0=$(epoch)
  n=0
  while [ "$(code_of "$A" '')" != 200 ]; do
    n=$((n + 1))
    sleep 0.05
    [ "$n" -gt 200 ] && break
  done
  t1=$(epoch)
  echo "RAM_A_LAI_200_GIAY=$(num "$t0" "$t1")"
  echo "RAM_A_LAN_HONG_TRUOC_KHI_200=$n"
  wait "$bp"
  echo "RAM_B_HONG=$(cat /root/e2e-m17-b.txt)"
  echo "RAM_B_TONG=60"
  nr1=$(systemctl show -p NRestarts --value "tpan-cage-$S-php.service")
  echo "OOM_KILL=$(cg_val memory.events oom_kill)"
  echo "MEMORY_PEAK=$(cat "$CG/memory.peak")"
  echo "NRESTARTS_TRUOC=$nr0"
  echo "NRESTARTS_SAU=$nr1"
}

modo_tasks() {
  local r t0 t1 n bp
  put_php "$A" e2e-m17-f.php <<'PHP'
<?php
$t = microtime(true);
exec('i=0; while [ $i -lt 200 ]; do sleep 25 >/dev/null 2>&1 & i=$((i+1)); done; echo het-vong 2>&1', $out, $rc);
printf("ma %d, %.2f giay, output: %s\n", $rc, microtime(true) - $t, substr(implode(" | ", $out), 0, 200));
PHP
  get "$A" '' -o /dev/null
  echo "TASKS_PIDS_MAX=$(cat "$CG/pids.max")"
  goi_b 40 &
  bp=$!
  BG_PIDS+=("$bp")
  r=$(get "$A" e2e-m17-f.php --max-time 50 | tr '\n' ' ')
  echo "TASKS_RESP=$r"
  echo "TASKS_PIDS_CURRENT=$(cat "$CG/pids.current")"
  echo "TASKS_PIDS_PEAK=$(cat "$CG/pids.peak")"
  echo "TASKS_HITS=$(find "$CG" -name pids.events -print0 | xargs -0 cat | awk '$1 == "max" { s += $2 } END { print s + 0 }')"
  wait "$bp"
  echo "TASKS_B_HONG=$(cat /root/e2e-m17-b.txt)"
  t0=$(epoch)
  n=0
  while [ "$(cat "$CG/pids.current")" -gt 8 ] && [ "$n" -lt 80 ]; do
    n=$((n + 1))
    sleep 0.5
  done
  t1=$(epoch)
  echo "TASKS_DOI_THOAT_GIAY=$(num "$t0" "$t1")"
  echo "TASKS_PIDS_CURRENT_SAU=$(cat "$CG/pids.current")"
  echo "TASKS_A_SAU=$(code_of "$A" '')"
}

# do_cpu <nhãn>: A đốt CPU 10 giây; in phần CPU thật nhận được (% một nhân) và
# số chu kỳ bị bóp. Mã PHP chạy theo thời gian (không theo số vòng) nên thời gian
# tường cố định, phần CPU là chênh lệch usage_usec của slice chia cho thời gian.
do_cpu() {
  local u0 u1 w0 w1 n0 n1
  u0=$(cg_val cpu.stat usage_usec)
  n0=$(cg_val cpu.stat nr_throttled)
  w0=$(epoch)
  get "$A" 'e2e-m17-c.php?s=10' --max-time 40 -o /dev/null
  u1=$(cg_val cpu.stat usage_usec)
  n1=$(cg_val cpu.stat nr_throttled)
  w1=$(epoch)
  echo "CPU_$1_NHAN=$(awk -v u0="$u0" -v u1="$u1" -v w0="$w0" -v w1="$w1" 'BEGIN { printf "%.1f", (u1 - u0) / 1e6 / (w1 - w0) * 100 }')"
  echo "CPU_$1_BI_BOP=$((n1 - n0))"
}

modo_cpu() {
  local i busy=()
  put_php "$A" e2e-m17-c.php <<'PHP'
<?php set_time_limit(0); $end = microtime(true) + (float)($_GET["s"] ?? 10); $x = 0; while (microtime(true) < $end) { $x++; } echo "xong";
PHP
  get "$A" '' -o /dev/null
  echo "CPU_MAX=$(cat "$CG/cpu.max")"
  echo "SO_NHAN=$(nproc)"
  for i in $(seq 1 "$(nproc)"); do
    nice -n 19 sh -c 'while :; do :; done' &
    busy+=("$!")
    BG_PIDS+=("$!")
  done
  sleep 1
  do_cpu BAN_1
  do_cpu BAN_2
  kill "${busy[@]}" 2>/dev/null
  wait "${busy[@]}" 2>/dev/null
  sleep 1
  do_cpu RANH
}

modo_io() {
  local ta to bp
  put_php "$A" e2e-m17-io.php <<PHP
<?php set_time_limit(0); \$t = microtime(true); exec("dd if=/dev/zero of=/var/www/$A/tmp/io.bin bs=1M count=32 oflag=direct 2>&1; rm -f /var/www/$A/tmp/io.bin"); printf("%.2f", microtime(true) - \$t);
PHP
  put_php "$O" e2e-m17-io.php <<PHP
<?php set_time_limit(0); \$t = microtime(true); exec("dd if=/dev/zero of=/var/www/$O/tmp/io.bin bs=1M count=32 oflag=direct 2>&1; rm -f /var/www/$O/tmp/io.bin"); printf("%.2f", microtime(true) - \$t);
PHP
  get "$A" '' -o /dev/null
  echo "IO_MAX=$(tr '\n' ' ' <"$CG/io.max")"
  goi_b 20 &
  bp=$!
  BG_PIDS+=("$bp")
  ta=$(get "$A" e2e-m17-io.php --max-time 60)
  echo "IO_A_GIAY=$ta"
  wait "$bp"
  echo "IO_B_HONG=$(cat /root/e2e-m17-b.txt)"
  to=$(get "$O" e2e-m17-io.php --max-time 60)
  echo "IO_NGOAI_GIAY=$to"
  echo "IO_STAT=$(head -2 "$CG/io.stat" 2>/dev/null | tr '\n' ' ')"
}

main() {
  local mode=${1:?thiếu chế độ}
  A=${2:?thiếu A}
  B=${3:?thiếu B}
  O=${4:-}
  S=$(slug_of "$A")
  CG=/sys/fs/cgroup/tpan.slice/tpan-$S.slice
  trap don EXIT
  case "$mode" in
    ram) modo_ram ;;
    tasks) modo_tasks ;;
    cpu) modo_cpu ;;
    io)
      [ -n "$O" ] || { echo "LOI=chế độ io cần O"; return 2; }
      modo_io
      ;;
    *)
      echo "LOI=chế độ lạ: $mode"
      return 2
      ;;
  esac
}

main "$@"; exit $?
