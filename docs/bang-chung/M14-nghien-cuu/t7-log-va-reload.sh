#!/bin/bash
# Thử hai chi tiết cho thiết kế: (1) khoá thư mục log theo site, (2) reload PHP-FPM có nhận file ini mới không.
# Chạy trên Mac:  multipass exec tpan-nc -- sudo bash < t7-log-va-reload.sh
set -u
A=a.example.com; B=b.example.com; DA=/var/www/$A; DB=/var/www/$B
UA=$(stat -c %U $DA); UB=$(stat -c %U $DB); V=$(php -r 'echo PHP_MAJOR_VERSION.".".PHP_MINOR_VERSION;')
LB=/var/log/tpan/sites/$B
cb(){ local p=$1; shift; curl -s --max-time 10 --resolve $B:80:127.0.0.1 "$@" "http://$B/$p"; }
echo "## 0. Pool và dịch vụ"; ls /etc/php/$V/fpm/pool.d/; systemctl show php$V-fpm -p ProtectProc; uname -r; systemctl --version | head -1
echo; echo "## 1. Thư mục log của B: root:<nhóm của B> 0750, file 0640"
stat -c '  trước: %A %U:%G %n' $LB $LB/access.log
chgrp $UB $LB $LB/access.log $LB/error.log; chmod 0750 $LB; chmod 0640 $LB/access.log $LB/error.log
stat -c '  sau:   %A %U:%G %n' $LB $LB/access.log
systemctl reload nginx; sleep 1; N1=$(wc -l < $LB/access.log); cb "" -o /dev/null; N2=$(wc -l < $LB/access.log)
echo "  nginx còn ghi access.log: $N1 -> $N2 dòng"
runuser -u $UA -- head -c 1 $LB/access.log >/dev/null 2>&1; echo "  user A đọc access.log của B: mã $? (1 = bị từ chối)"
runuser -u $UB -- head -c 1 $LB/access.log >/dev/null 2>&1; echo "  user B đọc access.log của chính mình: mã $?"
printf '<?php error_log("thu ghi log"); echo "ok";' > $DB/public/e.php; chown $UB:www-data $DB/public/e.php
cb e.php >/dev/null; echo "  PHP của B còn ghi php-error.log: $(grep -c 'thu ghi log' $LB/php-error.log) dòng"
echo "  logrotate chạy thử (không đổi gì): $(logrotate -d /etc/logrotate.d/tpan-sites 2>&1 | grep -i -c 'error\|insecure\|skipping') dòng lỗi"
logrotate -d /etc/logrotate.d/tpan-sites 2>&1 | grep -i 'error\|insecure\|skipping' | head -3 | cut -c1-200
logrotate -f /etc/logrotate.d/tpan-sites 2>&1 | head -3; cb "" -o /dev/null; sleep 1
stat -c '  sau khi xoay vòng thật: %A %U:%G %n' $LB/access.log $LB/access.log.1 2>&1 | cut -c1-120
runuser -u $UA -- head -c 1 $LB/access.log >/dev/null 2>&1; echo "  user A đọc access.log mới của B: mã $?"
rm -f $DB/public/e.php
echo; echo "## 2. reload (không restart) có nhận opcache.validate_permission không"
printf 'opcache.validate_permission=1\n' > /etc/php/$V/fpm/conf.d/99-tpan-nc.ini
systemctl reload php$V-fpm; sleep 2
printf '<?php echo "validate_permission=".ini_get("opcache.validate_permission");' > $DB/public/i.php; chown $UB:www-data $DB/public/i.php
echo "  sau reload: $(cb i.php)"
rm -f /etc/php/$V/fpm/conf.d/99-tpan-nc.ini $DB/public/i.php; systemctl reload php$V-fpm
echo; echo "## Trả về như cũ"
chgrp root $LB $LB/access.log* $LB/error.log* 2>/dev/null; chmod 0755 $LB; chmod 0644 $LB/access.log $LB/error.log; echo "đã trả về"
