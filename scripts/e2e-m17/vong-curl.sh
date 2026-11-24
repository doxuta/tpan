#!/bin/bash
# Vòng gọi trang chủ của một site liên tục, ghi mã HTTP và thời gian từng
# request, để đo gián đoạn khi vào lồng, ra lồng, hạ trần (e2e M17, bất biến 12
# và mục "vòng curl" của mục 7). CHỈ chạy trong máy ảo test của mốc, bằng root.
# Trên Mac, trong thư mục repo:
#
#   scripts/vm.sh root bash -s -- <chế độ> [domain] < scripts/e2e-m17/vong-curl.sh
#
#   bat <domain> [ram]
#                  chạy vòng ở nền (unit e2e-m17-vong), mỗi request một dòng
#                  "<bắt đầu µs> <kết thúc µs> <mã HTTP>" ở /root/e2e-m17-vong.log.
#                  Có "ram": gọi file PHP thử cấp phát 96 MB mỗi request thay cho
#                  trang chủ (đặt vào public của site, thuộc user của site, xoá
#                  khi dung). Dùng cho phép hạ trần RAM xuống 64 MiB (bất biến
#                  12): trang chủ chỉ cần khoảng 40 MB nên có khi vẫn 200 dưới
#                  trần 64 MiB, phép thử khi đó không chứng minh gì; request cần
#                  96 MB thì chắc chắn chết dưới 64 MiB và chắc chắn sống ở 200 MiB.
#   moc            ghi một dòng mốc "# MOC <µs>" (ví dụ ngay sau lệnh nâng trần)
#   dung           dừng vòng, in tóm tắt:
#                    TONG=<số request>  HONG=<số request mã khác 200>
#                    MA=<mã:số lần ...>
#                    GIAN_DOAN_MS=<đợt hỏng liền nhau dài nhất: từ đầu request
#                      hỏng đầu tiên tới hết request hỏng cuối cùng của đợt>
#                    GIAN_DOAN_TONG_MS=<tổng các đợt>
#                    GIAN_DOAN_DAI_MS=<như GIAN_DOAN_MS nhưng tính tới lúc request
#                      đầu tiên trả 200 trở lại: cận trên>
#                    CHAM_NHAT_MS=<request lâu nhất, kể cả request trả 200>
#                    PHUC_HOI_MS=<từ mốc tới đầu request 200 đầu tiên sau mốc; -1
#                      khi không có mốc hay không có request 200 nào sau đó>
#
# Toàn bộ nằm trong hàm và dòng cuối gọi main (xem cgroup.sh).
set -u

LOG=/root/e2e-m17-vong.log
STOP=/root/e2e-m17-vong.stop
LOOP=/root/e2e-m17-vong-loop.sh
UNIT=e2e-m17-vong

RAM_PHP=e2e-m17-vong-ram.php

# vong <domain> [đường dẫn]: chạy tới khi có file $STOP (hoặc quá 10 phút). Hàm
# này được `declare -f` chép vào $LOOP cho unit nền, nên chỉ dùng biến LOG và STOP.
vong() {
  local d=$1 p=${2:-} t0 t1 c limit=$((SECONDS + 600))
  while [ ! -e "$STOP" ] && [ "$SECONDS" -lt "$limit" ]; do
    t0=${EPOCHREALTIME/[.,]/}
    c=$(curl -s -o /dev/null -m 5 --resolve "$d:80:127.0.0.1" -w '%{http_code}' "http://$d/$p")
    t1=${EPOCHREALTIME/[.,]/}
    echo "$t0 $t1 $c" >>"$LOG"
    sleep 0.05
  done
}

# Đầu vào là tệp log: mỗi dòng "t0 t1 mã", hoặc "# MOC t".
tom_tat() {
  awk '
    $1 == "#" && $2 == "MOC" { moc = $3; next }
    $1 == "#" { next }
    {
      n++; ma[$3]++
      d = $2 - $1
      if (d > cham) cham = d
      if ($3 != 200) {
        hong++
        if (!trong) { trong = 1; dau = $1 }
        cuoi = $2
        if (cuoi - dau > dai1) dai1 = cuoi - dau
      } else {
        if (trong) {
          trong = 0
          tong += cuoi - dau
          if ($1 - dau > dai2) dai2 = $1 - dau
        }
        if (moc != "" && !co && $1 >= moc) { co = 1; hoi = $1 - moc }
      }
    }
    END {
      if (trong) { tong += cuoi - dau; if (cuoi - dau > dai2) dai2 = cuoi - dau }
      s = ""
      for (c in ma) s = s c ":" ma[c] " "
      printf "TONG=%d\nHONG=%d\nMA=%s\n", n, hong, s
      printf "GIAN_DOAN_MS=%.0f\nGIAN_DOAN_TONG_MS=%.0f\nGIAN_DOAN_DAI_MS=%.0f\n", dai1 / 1000, tong / 1000, dai2 / 1000
      printf "CHAM_NHAT_MS=%.0f\n", cham / 1000
      printf "PHUC_HOI_MS=%.0f\n", (moc != "" && co) ? hoi / 1000 : -1
    }
  ' "$1"
}

main() {
  local mode=${1:?thiếu chế độ} d=${2:-} i p t
  case "$mode" in
    bat)
      [ -n "$d" ] || { echo "LOI=thiếu domain"; return 2; }
      systemctl stop "$UNIT" </dev/null 2>/dev/null
      systemctl reset-failed "$UNIT" </dev/null 2>/dev/null
      rm -f "$LOG" "$STOP"
      p=""
      if [ "${3:-}" = ram ]; then
        p=$RAM_PHP
        t=$(mktemp)
        echo '<?php ini_set("memory_limit", "-1"); $a = []; for ($i = 0; $i < 12; $i++) { $a[] = str_repeat(chr(65 + $i), 8 * 1024 * 1024); } echo "96 MB\n";' >"$t"
        install -o "$(stat -c %U "/var/www/$d")" -g www-data -m 640 "$t" "/var/www/$d/public/$p"
        rm -f "$t"
      fi
      # Vòng chạy từ một file chứ không từ đối số của systemd-run: systemd thay
      # ${...} và %x trong dòng lệnh của unit trước khi bash kịp chạy.
      { printf 'LOG=%s\nSTOP=%s\n' "$LOG" "$STOP"; declare -f vong; printf 'vong "$1" "$2"\n'; } >"$LOOP"
      systemd-run --quiet --collect --unit="$UNIT" /bin/bash "$LOOP" "$d" "$p" </dev/null
      for i in $(seq 1 20); do [ -s "$LOG" ] && break; sleep 0.2; done
      echo "VONG=$(systemctl is-active "$UNIT" </dev/null) DONG_LOG=$(wc -l <"$LOG" 2>/dev/null || echo 0)"
      ;;
    moc)
      printf '# MOC %s\n' "${EPOCHREALTIME/[.,]/}" >>"$LOG"
      echo "DA_GHI_MOC=1"
      ;;
    dung)
      touch "$STOP"
      for i in $(seq 1 60); do
        systemctl is-active --quiet "$UNIT" </dev/null || break
        sleep 0.5
      done
      rm -f "$STOP" /var/www/*/public/"$RAM_PHP"
      if [ -s "$LOG" ]; then tom_tat "$LOG"; else echo "TONG=0"; fi
      ;;
    *)
      echo "LOI=chế độ lạ: $mode"
      return 2
      ;;
  esac
}

main "$@"; exit $?
