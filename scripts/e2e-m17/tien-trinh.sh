#!/bin/bash
# Kiểm tiến trình và đơn vị của lồng một site (e2e M17: bất biến 3 và 8, mục
# K1.1b và K1.5 của mô hình đe doạ). CHỈ chạy trong máy ảo test của mốc, bằng
# root. Trên Mac, trong thư mục repo:
#
#   scripts/vm.sh root bash -s -- <chế độ> <domain> < scripts/e2e-m17/tien-trinh.sh
#
# Chế độ (mỗi kết quả là một dòng "<khoá>=<giá trị>"):
#   ngoai-slice <domain>   NGOAI_SLICE=<số tiến trình cùng uid với site mà cgroup
#                          không nằm dưới tpan-<slug>.slice>, kèm một dòng NGOAI
#                          cho từng tiến trình đó (bất biến 8)
#   quet-health <domain>   như trên nhưng quét liên tục trong lúc `tpan health`
#                          chạy: MAU=<số lần quét> NGOAI_SLICE_MAX=<lớn nhất>
#                          HEALTH_RC=<mã thoát của tpan health>
#   quet-cron <domain>     quét liên tục trong lúc `tpan cron run <domain>` chạy:
#                          MAU, WP_NGOAI_SLICE=<số lần thấy lệnh wp ngoài slice>,
#                          WP_TRONG_SLICE=<số lần thấy lệnh wp trong slice>,
#                          NGOAI_SLICE_MAX
#   kiem-ban <domain>      gọi site một lần (đánh thức lồng) rồi đếm:
#                          POOL_CHUNG_FILE (file pool chung của site),
#                          POOL_CHUNG_USER (file pool chung nào có user của site),
#                          MASTER (tiến trình "php-fpm: master" của chính user đó),
#                          UNIT_LONG (unit tpan-cage-<slug>-* mà systemd biết),
#                          FILE_LONG (file của lồng trên đĩa)
#
# Toàn bộ nằm trong hàm và dòng cuối gọi main (xem cgroup.sh).
set -u

slug_of() { stat -c %U "/var/www/$1" 2>/dev/null | sed 's/^tp_//'; }

# dem_ngoai: đặt N_NGOAI = số tiến trình có uid của site mà cgroup không nằm dưới
# slice của nó. VERBOSE=1 thì in thêm một dòng NGOAI cho từng tiến trình.
# Đọc /proc bằng `read` của bash (không fork) để một lần quét chỉ mất vài chục ms.
dem_ngoai() {
  local p k v u cg c
  N_NGOAI=0
  for p in /proc/[0-9]*; do
    p=${p#/proc/}
    u=""
    while read -r k v _; do
      if [ "$k" = "Uid:" ]; then u=$v; break; fi
    done 2>/dev/null <"/proc/$p/status"
    [ "$u" = "$UID_SITE" ] || continue
    cg=$(cat "/proc/$p/cgroup" 2>/dev/null) || continue
    case "$cg" in
      *"/tpan-$SLUG.slice/"* | *"/tpan-$SLUG.slice") continue ;;
    esac
    N_NGOAI=$((N_NGOAI + 1))
    if [ "${VERBOSE:-1}" = 1 ]; then
      c=$(tr '\0' ' ' <"/proc/$p/cmdline" 2>/dev/null | head -c 100)
      echo "NGOAI pid=$p cgroup=${cg#0::} cmd=$c"
    fi
  done
}

# dem_wp: đặt WP_TRONG và WP_NGOAI = số tiến trình của site có dòng lệnh chứa
# "cron event" (lệnh `wp cron event run`), trong và ngoài slice.
dem_wp() {
  local p k v u cg c
  WP_TRONG=0
  WP_NGOAI=0
  for p in /proc/[0-9]*; do
    p=${p#/proc/}
    u=""
    while read -r k v _; do
      if [ "$k" = "Uid:" ]; then u=$v; break; fi
    done 2>/dev/null <"/proc/$p/status"
    [ "$u" = "$UID_SITE" ] || continue
    c=$(tr '\0' ' ' <"/proc/$p/cmdline" 2>/dev/null)
    case "$c" in *"cron event"*) ;; *) continue ;; esac
    cg=$(cat "/proc/$p/cgroup" 2>/dev/null) || continue
    case "$cg" in
      *"/tpan-$SLUG.slice/"* | *"/tpan-$SLUG.slice") WP_TRONG=$((WP_TRONG + 1)) ;;
      *) WP_NGOAI=$((WP_NGOAI + 1)) ;;
    esac
  done
}

main() {
  local mode=${1:?thiếu chế độ} d=${2:?thiếu domain} hp rc mau=0 max=0 pt=0 pn=0 f n p k v c u
  SLUG=$(slug_of "$d")
  [ -n "$SLUG" ] || { echo "LOI=không thấy thư mục /var/www/$d"; return 2; }
  UID_SITE=$(id -u "tp_$SLUG" 2>/dev/null)
  [ -n "$UID_SITE" ] || { echo "LOI=không có user tp_$SLUG"; return 2; }
  case "$mode" in
    ngoai-slice)
      VERBOSE=1 dem_ngoai
      echo "NGOAI_SLICE=$N_NGOAI"
      ;;
    quet-health)
      tpan health >/root/e2e-m17-health.out 2>&1 </dev/null &
      hp=$!
      while kill -0 "$hp" 2>/dev/null; do
        VERBOSE=0 dem_ngoai
        mau=$((mau + 1))
        [ "$N_NGOAI" -gt "$max" ] && max=$N_NGOAI
      done
      wait "$hp"
      rc=$?
      echo "MAU=$mau"
      echo "NGOAI_SLICE_MAX=$max"
      echo "HEALTH_RC=$rc"
      ;;
    quet-cron)
      tpan cron run "$d" >/root/e2e-m17-cron.out 2>&1 </dev/null &
      hp=$!
      while kill -0 "$hp" 2>/dev/null; do
        VERBOSE=0 dem_ngoai
        dem_wp
        mau=$((mau + 1))
        [ "$N_NGOAI" -gt "$max" ] && max=$N_NGOAI
        [ "$WP_TRONG" -gt 0 ] && pt=$((pt + 1))
        [ "$WP_NGOAI" -gt 0 ] && pn=$((pn + 1))
      done
      wait "$hp"
      rc=$?
      echo "MAU=$mau"
      echo "WP_TRONG_SLICE=$pt"
      echo "WP_NGOAI_SLICE=$pn"
      echo "NGOAI_SLICE_MAX=$max"
      echo "CRON_RC=$rc"
      ;;
    kiem-ban)
      curl -s -o /dev/null -m 30 --resolve "$d:80:127.0.0.1" "http://$d/" </dev/null
      n=0
      for f in /etc/php/*/fpm/pool.d/"tpan-$SLUG.conf"; do [ -e "$f" ] && n=$((n + 1)); done
      echo "POOL_CHUNG_FILE=$n"
      n=0
      for f in /etc/php/*/fpm/pool.d/*.conf; do
        [ -e "$f" ] || continue
        grep -q "^user = tp_$SLUG\$" "$f" 2>/dev/null && n=$((n + 1))
      done
      echo "POOL_CHUNG_USER=$n"
      n=0
      for p in /proc/[0-9]*; do
        p=${p#/proc/}
        u=""
        while read -r k v _; do
          if [ "$k" = "Uid:" ]; then u=$v; break; fi
        done 2>/dev/null <"/proc/$p/status"
        [ "$u" = "$UID_SITE" ] || continue
        c=$(tr '\0' ' ' <"/proc/$p/cmdline" 2>/dev/null)
        case "$c" in "php-fpm: master"*) n=$((n + 1)) ;; esac
      done
      echo "MASTER=$n"
      echo "UNIT_LONG=$(systemctl list-units --all --no-legend --plain "tpan-cage-$SLUG-*" "tpan-$SLUG.slice" </dev/null | wc -l | tr -d ' ')"
      n=0
      for f in /etc/systemd/system/tpan-cage-"$SLUG"-* /etc/systemd/system/"tpan-$SLUG.slice" "/etc/tpan/cage/$SLUG"; do
        [ -e "$f" ] && n=$((n + 1))
      done
      echo "FILE_LONG=$n"
      ;;
    *)
      echo "LOI=chế độ lạ: $mode"
      return 2
      ;;
  esac
}

main "$@"; exit $?
