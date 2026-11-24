#!/bin/bash
# sha256 của các file cấu hình, để so trước và sau một thao tác (e2e M17: bất
# biến 1 và 2 của docs/thay-doi/M17-thiet-ke.md). CHỈ chạy trong máy ảo test
# của mốc, bằng root. Trên Mac, trong thư mục repo:
#
#   scripts/vm.sh root bash -s -- <chế độ> ... < scripts/e2e-m17/bam.sh
#
#   site <domain>            năm file của site, mỗi file một dòng "<nhãn> <sha256>":
#                            pool (/etc/php/*/fpm/pool.d/tpan-<slug>.conf), vhost-nginx,
#                            vhost-apache, cron (/etc/cron.d/tpan-<slug>), wp-config,
#                            htaccess. File không có: "<nhãn> KHONG_CO". Hai lần chạy
#                            giống nhau thì output giống từng byte.
#   cay <nhãn> <thư mục...>  sha256 mọi file thường dưới các thư mục, lưu ở
#                            /root/e2e-m17-bam-<nhãn>.txt; in BAM_<nhãn>_SO_FILE
#                            và BAM_<nhãn>_TONG (sha256 của cả danh sách)
#   so <nhãn1> <nhãn2>       so hai danh sách đã lưu: DIFF_DONG=<số dòng khác>,
#                            kèm tối đa 40 dòng khác
#
# Toàn bộ nằm trong hàm và dòng cuối gọi main (xem cgroup.sh).
set -u

main() {
  local mode=${1:?thiếu chế độ} d s u f h l n t
  case "$mode" in
    site)
      d=${2:?thiếu domain}
      u=$(stat -c %U "/var/www/$d" 2>/dev/null)
      [ -n "$u" ] || { echo "LOI=không thấy thư mục /var/www/$d"; return 2; }
      s=${u#tp_}
      for l in \
        "pool:$(ls /etc/php/*/fpm/pool.d/tpan-"$s".conf 2>/dev/null | head -1)" \
        "vhost-nginx:/etc/nginx/sites-available/tpan-$d.conf" \
        "vhost-apache:/etc/apache2/sites-available/tpan-$d.conf" \
        "cron:/etc/cron.d/tpan-$s" \
        "wp-config:/var/www/$d/public/wp-config.php" \
        "htaccess:/var/www/$d/public/.htaccess"; do
        f=${l#*:}
        if [ -n "$f" ] && [ -f "$f" ]; then
          echo "${l%%:*} $(sha256sum <"$f" | cut -c1-64)"
        else
          echo "${l%%:*} KHONG_CO"
        fi
      done
      ;;
    cay)
      l=${2:?thiếu nhãn}
      shift 2
      f=/root/e2e-m17-bam-$l.txt
      for d in "$@"; do
        [ -d "$d" ] && find "$d" -type f -print0
      done | sort -z | xargs -0 sha256sum >"$f" 2>/dev/null
      n=$(wc -l <"$f" | tr -d ' ')
      h=$(sha256sum <"$f" | cut -c1-64)
      echo "BAM_${l}_SO_FILE=$n"
      echo "BAM_${l}_TONG=$h"
      ;;
    so)
      t=$(diff "/root/e2e-m17-bam-${2:?thiếu nhãn 1}.txt" "/root/e2e-m17-bam-${3:?thiếu nhãn 2}.txt")
      if [ -z "$t" ]; then n=0; else n=$(printf '%s\n' "$t" | grep -c '^[<>]'); fi
      echo "DIFF_DONG=$n"
      [ "$n" -gt 0 ] && printf '%s\n' "$t" | head -40
      true
      ;;
    *)
      echo "LOI=chế độ lạ: $mode"
      return 2
      ;;
  esac
}

main "$@"; exit $?
