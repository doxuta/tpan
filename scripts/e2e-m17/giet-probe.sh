#!/bin/bash
# Bắt đầu một lượt thử tấn công rồi `kill -9` tiến trình tpan giữa chừng, như
# panel bị tắt đột ngột (bất biến 5 của docs/thay-doi/M17-thiet-ke.md). CHỈ chạy
# trong máy ảo test của mốc, bằng root. Trên Mac, trong thư mục repo:
#
#   scripts/vm.sh root bash -s < scripts/e2e-m17/giet-probe.sh
#
# Chạy `tpan isolation probe --yes` ở nền, chờ thư mục của site thử A xuất hiện
# (tối đa 300 giây; job đã ghi sổ và đang tạo site), chờ thêm 3 giây rồi
# kill -9. In:
#   TPAN_PID=<pid>     DA_GIET=1 (tiến trình đã chết) hay 0
#   THU_MUC_SOT=<số thư mục /var/www/tpan-probe-* còn lại sau khi giết>
#   PROBE_RUNS=<state:cleaned,...>   các dòng của sổ probe_runs lúc đó
#   LOG_TPAN=<vài dòng cuối output của tpan>
# Toàn bộ nằm trong hàm và dòng cuối gọi main (xem cgroup.sh).
set -u

main() {
  local pid i
  tpan isolation probe --yes >/root/e2e-m17-probe.log 2>&1 </dev/null &
  pid=$!
  echo "TPAN_PID=$pid"
  for i in $(seq 1 300); do
    ls -d /var/www/tpan-probe-a-*.test >/dev/null 2>&1 && break
    kill -0 "$pid" 2>/dev/null || break
    sleep 1
  done
  sleep 3
  kill -9 "$pid" 2>/dev/null
  for i in $(seq 1 20); do
    kill -0 "$pid" 2>/dev/null || break
    sleep 0.5
  done
  if kill -0 "$pid" 2>/dev/null; then echo "DA_GIET=0"; else echo "DA_GIET=1"; fi
  wait "$pid" 2>/dev/null
  echo "THU_MUC_SOT=$(ls -d /var/www/tpan-probe-* 2>/dev/null | wc -l | tr -d ' ')"
  echo "PROBE_RUNS=$(python3 -c '
import sqlite3
c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True)
print(",".join("%s:%d" % r for r in c.execute("SELECT state, cleaned FROM probe_runs ORDER BY started_at")))
' </dev/null)"
  echo "LOG_TPAN=$(tail -3 /root/e2e-m17-probe.log | tr '\n' '|')"
}

main "$@"; exit $?
