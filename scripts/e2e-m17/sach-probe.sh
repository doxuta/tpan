#!/bin/bash
# Tìm mọi dấu vết của phép thử tấn công trên máy (bất biến 5 của
# docs/thay-doi/M17-thiet-ke.md: thử tấn công luôn dọn sạch). Hai site thử mang
# tên tpan-probe-a-<12 hex>.test và tpan-probe-b-<12 hex>.test, nên slug, user,
# DB, unit của chúng đều có "tpan_probe" hay "tpan-probe". CHỈ chạy trong máy
# ảo test của mốc, bằng root. Trên Mac, trong thư mục repo:
#
#   scripts/vm.sh root bash -s -- <chế độ> [nhãn...] < scripts/e2e-m17/sach-probe.sh
#
#   quet            RAC_<loại>=<số> cho từng nơi (user, nhóm Linux, thư mục site,
#                   thư mục log, tên file và nội dung dưới /etc, tên file ở /tmp
#                   /var/tmp /dev/shm /run /var/lib/php, DB và user DB, unit
#                   systemd, bảng managed_files, check_results, incidents của
#                   tpan.db), kèm RAC_TONG và vài dòng RAC_CHI_TIET
#   tmp-luu <nhãn>  lưu danh sách mục của /tmp, /var/tmp, /dev/shm (bỏ
#                   systemd-private-*) ở /root/e2e-m17-tmp-<nhãn>.txt
#   tmp-so <a> <b>  DIFF_TMP=<số dòng khác> giữa hai danh sách đã lưu
#   so-dong         PROBE_RUNS=<state:cleaned,...>: các dòng của sổ probe_runs
#
# Toàn bộ nằm trong hàm và dòng cuối gọi main (xem cgroup.sh).
set -u

PAT='tpan[-_]probe'

dem() { # dem <khoá> <số>
  echo "RAC_$1=$2"
  TONG=$((TONG + $2))
}

main() {
  local mode=${1:?thiếu chế độ} n t k v
  TONG=0
  case "$mode" in
    quet)
      dem USER "$(getent passwd | grep -cE "$PAT")"
      dem NHOM "$(getent group | grep -cE "$PAT")"
      dem THU_MUC_WWW "$(ls /var/www 2>/dev/null | grep -cE "$PAT")"
      dem THU_MUC_LOG "$(ls /var/log/tpan/sites 2>/dev/null | grep -cE "$PAT")"
      dem THU_MUC_RAC "$(ls /var/lib/tpan/trash 2>/dev/null | grep -cE "$PAT")"
      t=$(find /etc /tmp /var/tmp /dev/shm /run /var/lib/php /var/cache/tpan /var/lib/tpan -name '*tpan[-_]probe*' 2>/dev/null)
      dem TEN_FILE "$(printf '%s' "$t" | grep -c .)"
      dem NOI_DUNG_ETC "$(grep -rlIE "$PAT" /etc 2>/dev/null | wc -l | tr -d ' ')"
      dem DB "$(mysql -N -e 'SHOW DATABASES' </dev/null 2>/dev/null | grep -cE "$PAT")"
      dem USER_DB "$(mysql -N -e 'SELECT user FROM mysql.user' </dev/null 2>/dev/null | grep -cE "$PAT")"
      dem UNIT "$(systemctl list-units --all --no-legend --plain </dev/null | grep -cE "$PAT")"
      dem UNIT_FILE "$(systemctl list-unit-files --no-legend --plain </dev/null | grep -cE "$PAT")"
      n=$(python3 -c '
import re, sqlite3
c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True)
pat = re.compile(r"tpan[-_]probe")
for t in ("managed_files", "check_results", "incidents"):
    print("%s %d" % (t, sum(1 for r in c.execute("SELECT * FROM " + t) if pat.search(repr(r)))))
' </dev/null)
      while read -r k v; do
        dem "SQLITE_$k" "$v"
      done <<<"$n"
      echo "RAC_TONG=$TONG"
      find /etc /tmp /var/tmp /dev/shm /run /var/lib/php -name '*tpan[-_]probe*' 2>/dev/null | head -10 | sed 's/^/RAC_CHI_TIET /'
      ;;
    tmp-luu)
      { for t in /tmp /var/tmp /dev/shm; do ls -A "$t" 2>/dev/null | grep -v '^systemd-private-' | sed "s|^|$t/|"; done; } | sort >"/root/e2e-m17-tmp-${2:?thiếu nhãn}.txt"
      echo "TMP_LUU=$(wc -l <"/root/e2e-m17-tmp-$2.txt" | tr -d ' ')"
      ;;
    tmp-so)
      t=$(diff "/root/e2e-m17-tmp-${2:?thiếu nhãn 1}.txt" "/root/e2e-m17-tmp-${3:?thiếu nhãn 2}.txt")
      if [ -z "$t" ]; then n=0; else n=$(printf '%s\n' "$t" | grep -c '^[<>]'); fi
      echo "DIFF_TMP=$n"
      [ "$n" -gt 0 ] && printf '%s\n' "$t" | head -20
      true
      ;;
    so-dong)
      echo "PROBE_RUNS=$(python3 -c '
import sqlite3
c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True)
print(",".join("%s:%d" % r for r in c.execute("SELECT state, cleaned FROM probe_runs ORDER BY started_at")))
' </dev/null)"
      ;;
    *)
      echo "LOI=chế độ lạ: $mode"
      return 2
      ;;
  esac
}

main "$@"; exit $?
