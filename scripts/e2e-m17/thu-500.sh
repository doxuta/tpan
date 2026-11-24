#!/bin/bash
# Làm một site trả 500 cho 127.0.0.1, và giết tiến trình chính của PHP-FPM của
# lồng, như một site mà mã của nó điều khiển mã HTTP và tiến trình chính (bất
# biến 11 của docs/thay-doi/M17-thiet-ke.md: chính sách của admin không được
# chờ thứ mã của site điều khiển). CHỈ chạy trong máy ảo test của mốc, bằng
# root. Trên Mac, trong thư mục repo:
#
#   scripts/vm.sh root bash -s -- <chế độ> <domain> < scripts/e2e-m17/thu-500.sh
#
#   dat <domain>          đặt wp-content/mu-plugins/e2e-m17-500.php (mọi request
#                         từ 127.0.0.1 ra 500), in MA_HTTP của một request thử
#   go <domain>           gỡ file đó, in MA_HTTP
#   giet-chinh <domain>   kill -9 tiến trình chính của dịch vụ PHP của lồng
#                         (in DA_GIET=<pid>; dịch vụ tự chạy lại sau RestartSec)
#
# Toàn bộ nằm trong hàm và dòng cuối gọi main (xem cgroup.sh).
set -u

main() {
  local mode=${1:?thiếu chế độ} d=${2:?thiếu domain} u dir f s pid t
  u=$(stat -c %U "/var/www/$d" 2>/dev/null)
  [ -n "$u" ] || { echo "LOI=không thấy thư mục /var/www/$d"; return 2; }
  s=${u#tp_}
  dir=/var/www/$d/public/wp-content/mu-plugins
  f=$dir/e2e-m17-500.php
  case "$mode" in
    dat)
      install -d -o "$u" -g www-data -m 0755 "$dir"
      t=$(mktemp)
      cat >"$t" <<'PHP'
<?php
// e2e M17: mọi request từ 127.0.0.1 ra 500 (site "đóng vai" mã lạ trả mã HTTP lạ).
if (($_SERVER['REMOTE_ADDR'] ?? '') === '127.0.0.1') {
    http_response_code(500);
    exit('e2e-m17: 500 co chu y');
}
PHP
      install -o "$u" -g www-data -m 0644 "$t" "$f"
      rm -f "$t"
      echo "MA_HTTP=$(curl -s -o /dev/null -m 30 --resolve "$d:80:127.0.0.1" -w '%{http_code}' "http://$d/")"
      ;;
    go)
      rm -f "$f"
      rmdir "$dir" 2>/dev/null
      echo "MA_HTTP=$(curl -s -o /dev/null -m 30 --resolve "$d:80:127.0.0.1" -w '%{http_code}' "http://$d/")"
      ;;
    giet-chinh)
      pid=$(systemctl show -p MainPID --value "tpan-cage-$s-php.service" </dev/null)
      if [ -n "$pid" ] && [ "$pid" != 0 ]; then
        kill -9 "$pid"
        echo "DA_GIET=$pid"
      else
        echo "DA_GIET=0"
      fi
      ;;
    *)
      echo "LOI=chế độ lạ: $mode"
      return 2
      ;;
  esac
}

main "$@"; exit $?
