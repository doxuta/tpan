# t6: (a) lồng với các bản PHP khác (kho sury): FPM có nhận socket qua FPM_SOCKETS
#     không, WordPress có 200 không, hàng rào còn nguyên không; file ini OPcache và
#     drop-in ProtectProc của M14 còn tác dụng gì trong lồng.
#     (b) tên unit với slug lạ.
echo "## a. Các bản PHP"
for v in 7.4 8.1 8.2 8.4 8.5; do
  [ -x /usr/sbin/php-fpm$v ] || tpan php install $v --yes >/tmp/php-$v.log 2>&1 || echo "  cài PHP $v hỏng: $(tail -2 /tmp/php-$v.log | tr '\n' ' ' | cut -c1-200)"
done
echo "đã cài: $(ls /usr/sbin/php-fpm* | sed 's|/usr/sbin/php-fpm||' | tr '\n' ' ') | php mặc định của CLI: $(readlink -f /usr/bin/php)"
echo '<?php echo PHP_VERSION." user=".posix_getpwuid(posix_geteuid())["name"]." www=".trim((string)@shell_exec("ls /var/www | tr \"\\n\" \" \""))." khac=".trim((string)@shell_exec("for p in /proc/[0-9]*; do stat -c %u \$p; done | grep -vc \"^".posix_geteuid()."\$\""))." opcache_api=".(@opcache_get_status(false)?"MO":"khoa")." validate_permission=".ini_get("opcache.validate_permission")." pool_d=[".trim((string)@shell_exec("ls /etc/php/*/fpm/pool.d 2>/dev/null | tr \"\\n\" \" \""))."]";' | put_php $A v.php
for V in 8.3 7.4 8.1 8.2 8.4 8.5; do
  [ -x /usr/sbin/php-fpm$V ] || { echo "PHP $V: không có trên máy, bỏ qua"; continue; }
  long_go $A >/dev/null 2>&1; long_dung $A 256 100 60; long_vao $A >/dev/null
  S=$SA; SVC=tpan-cage-$S-php.service
  c=$(code_a '')
  echo "PHP $V: trang chủ HTTP $c | $(ca v.php | cut -c1-260)"
  echo "   journal: $(journalctl -u $SVC --since '-20s' --no-pager -o cat | grep -E 'inherited|ERROR|WARNING|ALERT' | tail -2 | tr '\n' ' ' | cut -c1-200)"
  echo "   wp trong lồng (php CLI mặc định): $(run_in $A wp core version --path=/var/www/$A/public 2>&1 | tail -1 | cut -c1-80) | php-fpm$V -t: $(run_in $A php-fpm$V -t --fpm-config /etc/tpan/cage/$S/php-fpm.conf 2>&1 | tail -1 | sed 's/.*] //' | cut -c1-90)"
  systemctl reload $SVC; sleep 1; echo "   sau reload (USR2): dịch vụ $(systemctl is-active $SVC), HTTP $(code_a '')"
done
V=8.3
echo "-- thư mục pool.d của các bản khác bản đang dùng có bị che không: xem cột pool_d ở trên (lib chỉ che /etc/php/\$V/fpm/pool.d)"
rm -f /var/www/$A/public/v.php
long_go $A >/dev/null 2>&1

echo; echo "## b. Tên unit"
thu_ten(){ # slug
  local s=$1
  printf '[Slice]\nTasksMax=50\n' > /etc/systemd/system/tpan-$s.slice
  systemctl daemon-reload
  systemd-run --quiet --collect --slice=tpan-$s.slice --unit=tpan-cage-$s-run-ten -- sleep 5 2>/tmp/e.txt; rc=$?
  sleep 0.3
  echo "slug [$s] (${#s} ký tự): systemd-run mã $rc $(head -c 120 /tmp/e.txt) | cgroup: $(systemctl show -p ControlGroup --value tpan-cage-$s-run-ten.service 2>&1 | cut -c1-150)"
  systemctl stop tpan-cage-$s-run-ten.service 2>/dev/null; systemctl stop tpan-$s.slice 2>/dev/null
  rm -f /etc/systemd/system/tpan-$s.slice; systemctl daemon-reload
}
thu_ten a_example_com_7653e4
thu_ten 123_example_com_abcdef
thu_ten xn__bcher_kva_exa_1a2b3c
thu_ten aaaaaaaaaaaaaaaa_ffffff
thu_ten co-gach-ngang_abcdef
echo "tên unit dài nhất systemd nhận: 255 ký tự; tên dài nhất của tpan: tpan-cage-<slug 23 ký tự>-run-<16 hex>.service = $(( 10 + 23 + 5 + 16 + 8 )) ký tự"

echo; echo "## Dọn"
echo "A HTTP $(code_a ''), B HTTP $(code_b '') | unit tpan-* còn: $(systemctl list-units --all 'tpan-*' --no-legend | wc -l)"
