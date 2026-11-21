#!/bin/bash
# Site A (trong lồng) nhìn thấy gì: chạy mã PHP của A, gọi shell từ đó (như kẻ đã
# chiếm được site, mục 5 của docs/thay-doi/M17-thiet-ke.md: K1.1, K1.2, K1.3,
# K1.4, K1.5; các đường C1 đến C5 của phép thử tấn công). CHỈ chạy trong máy ảo
# test của mốc, bằng root. Trên Mac, trong thư mục repo:
#
#   scripts/vm.sh root bash -s -- <A> <B> < scripts/e2e-m17/long-nhin.sh
#
# A là site trong lồng, B là site khác (cũng trong lồng). Kết quả là các dòng
# "<khoá>=<giá trị>":
#   LS_VAR_WWW        tên các mục của /var/www thấy từ A (mong đợi: đúng tên A)
#   TMP_MUC           số mục của /tmp (mong đợi: 0, PrivateTmp)
#   TIEN_TRINH_KHAC   số tiến trình của user khác thấy từ A (mong đợi: 0)
#   PASSWD_DONG       số dòng của `getent passwd` (mong đợi: 4)
#   PASSWD_TP         số user tp_* trong đó (mong đợi: 1, chính A)
#   B_THU_MUC         "ls -d /var/www/<B>" thấy gì (mong đợi: báo không có)
#   B_WPCONFIG        đọc wp-config.php của B (mong đợi: báo không có)
#   ETC_NGINX         ls /etc/nginx và /etc/apache2 (mong đợi: báo không có)
#   ETC_TPAN_CAGE     các mục của /etc/tpan/cage (mong đợi: chỉ slug của A)
#   RUN_TPAN_CAGE     ls /run/tpan-cage (mong đợi: báo không có)
#   CGROUPFS          ls /sys/fs/cgroup (mong đợi: báo không có hay rỗng)
#   UNIX_SOCKET_B     số dòng của /proc/self/net/unix nhắc slug hay tên miền của B
#                     (mong đợi: 0, C5). Không đếm phần băm: socket của lồng B
#                     nằm ở /run/tpan-cage/<16 hex của sha256(B)>/php.sock và
#                     thấy được từ mọi lồng vì mạng dùng chung; thiết kế (mục
#                     3.2, 3.9) chọn phần băm chính là để tên đó không mang tên
#                     miền, và phép thử C5 của tpan cũng chỉ đếm slug.
#   CRONTAB_RC        mã thoát của `crontab -l` (mong đợi: khác 0)
#   SUDO_RC           mã thoát của `sudo -n true` (mong đợi: khác 0)
#   CAPEFF            CapEff của tiến trình shell (mong đợi: 0000000000000000)
#   NONEWPRIVS        NoNewPrivs (mong đợi: 1)
#   CGROUP_CUA_A      cgroup của tiến trình shell (mong đợi: dưới slice của A)
#
# File PHP thử mang tên e2e-m17-nhin-<hậu tố ngẫu nhiên>.php và bị xoá ở cuối.
# Toàn bộ nằm trong hàm và dòng cuối gọi main (xem cgroup.sh).
set -u

slug_of() { stat -c %U "/var/www/$1" | sed 's/^tp_//'; }

main() {
  local A=${1:?thiếu A} B=${2:?thiếu B} ua sb r t
  ua=$(stat -c %U "/var/www/$A")
  sb=$(slug_of "$B")
  r=$(head -c 6 /dev/urandom | od -An -tx1 | tr -d ' \n')
  F=/var/www/$A/public/e2e-m17-nhin-$r.php
  trap 'rm -f "$F"' EXIT
  t=$(mktemp)
  cat >"$t" <<'PHP'
<?php
function sh($c) { return trim((string)shell_exec($c . ' 2>&1')); }
function dong($s) { return $s === '' ? 0 : count(explode("\n", $s)); }
$me = sh('id -u');
echo "LS_VAR_WWW=" . str_replace("\n", ",", sh('ls -A /var/www')) . "\n";
echo "TMP_MUC=" . dong(sh('ls -A /tmp')) . "\n";
echo "TIEN_TRINH_KHAC=" . sh('ps -eo uid= | tr -d " " | grep -vxc ' . escapeshellarg($me)) . "\n";
echo "PASSWD_DONG=" . dong(sh('getent passwd')) . "\n";
echo "PASSWD_TP=" . sh('getent passwd | grep -c "^tp_"') . "\n";
echo "B_THU_MUC=" . str_replace("\n", " ", sh('ls -d /var/www/@B@')) . "\n";
echo "B_WPCONFIG=" . str_replace("\n", " ", sh('head -c 40 /var/www/@B@/public/wp-config.php')) . "\n";
echo "ETC_NGINX=" . str_replace("\n", " ", sh('ls /etc/nginx /etc/apache2')) . "\n";
echo "ETC_TPAN_CAGE=" . str_replace("\n", ",", sh('ls -A /etc/tpan/cage')) . "\n";
echo "RUN_TPAN_CAGE=" . str_replace("\n", " ", sh('ls -A /run/tpan-cage')) . "\n";
echo "CGROUPFS=" . str_replace("\n", " ", sh('ls -A /sys/fs/cgroup')) . "\n";
echo "UNIX_SOCKET_B=" . sh('grep -c -F -e "@SB@" -e "@B@" /proc/self/net/unix') . "\n";
exec('crontab -l >/dev/null 2>&1', $o, $rc);
echo "CRONTAB_RC=" . $rc . "\n";
exec('sudo -n true >/dev/null 2>&1', $o, $rc);
echo "SUDO_RC=" . $rc . "\n";
echo "CAPEFF=" . sh('awk \'/^CapEff:/ {print $2}\' /proc/self/status') . "\n";
echo "NONEWPRIVS=" . sh('awk \'/^NoNewPrivs:/ {print $2}\' /proc/self/status') . "\n";
echo "CGROUP_CUA_A=" . sh('cat /proc/self/cgroup') . "\n";
PHP
  # Chỗ giữ chỗ @B@, @SB@: thay bằng tên miền và slug của B.
  sed -i -e "s|@B@|$B|g" -e "s|@SB@|$sb|g" "$t"
  install -o "$ua" -g www-data -m 640 "$t" "$F"
  rm -f "$t"
  curl -s -m 30 --resolve "$A:80:127.0.0.1" "http://$A/${F##*/}"
}

main "$@"; exit $?
