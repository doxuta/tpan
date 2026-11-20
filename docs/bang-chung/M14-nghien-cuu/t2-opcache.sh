#!/bin/bash
# OPcache dùng chung giữa các pool PHP-FPM: site A thấy gì của site B?
# Chạy trên Mac:  multipass exec tpan-nc -- sudo bash < t2-opcache.sh
set -u
A=a.example.com; B=b.example.com
DA=/var/www/$A; DB=/var/www/$B
UA=$(stat -c %U $DA)
V=$(php -r 'echo PHP_MAJOR_VERSION.".".PHP_MINOR_VERSION;')
ca(){ local p=$1; shift; curl -s --max-time 10 --resolve $A:80:127.0.0.1 "$@" "http://$A/$p"; }
cb(){ local p=$1; shift; curl -s --max-time 10 --resolve $B:80:127.0.0.1 "$@" "http://$B/$p"; }
as_a(){ runuser -u "$UA" -- "$@"; }
echo "## Cấu hình OPcache của PHP-FPM $V"
php-fpm$V -i 2>/dev/null | grep -E "^opcache\.(enable|validate_permission|validate_root|restrict_api|revalidate_freq) "
as_a tee $DA/public/o1.php >/dev/null <<EOF
<?php
\$s = function_exists("opcache_get_status") ? @opcache_get_status(true) : false;
\$n = 0; \$vd = "";
if (\$s && isset(\$s["scripts"])) foreach (\$s["scripts"] as \$k => \$v) if (strpos(\$k, "$DB/") === 0) { \$n++; if (\$vd === "") \$vd = \$k; }
echo \$s ? "goi duoc opcache_get_status; thay \$n file cua site B (vi du: \$vd)\n" : "opcache_get_status bi chan\n";
EOF
as_a tee $DA/public/o2.php >/dev/null <<EOF
<?php
register_shutdown_function(function(){ echo "open_basedir=".ini_get("open_basedir")." -> include wp-config.php cua B: ".(defined("DB_PASSWORD")?"LAY DUOC hang DB_PASSWORD":"khong lay duoc")."\n"; });
@include "$DB/public/wp-config.php";
EOF
cb "" -o /dev/null
echo; echo "## 1. Từ site A gọi API của OPcache"
echo -n "  "; ca o1.php
echo; echo "## 2. Từ site A include wp-config.php của B (B vừa có khách nên file đang nằm trong cache)"
cb "" -o /dev/null; echo -n "  open_basedir còn nguyên:  "; ca o2.php | tail -1
cp -p $DA/public/.htaccess /root/htaccess-a.goc
printf '\nSetEnv PHP_ADMIN_VALUE "open_basedir=/"\n' | as_a tee -a $DA/public/.htaccess >/dev/null
for i in 1 2 3; do cb "" -o /dev/null; echo -n "  open_basedir đã nới (lần $i): "; ca o2.php | tail -1; done
cp -p /root/htaccess-a.goc $DA/public/.htaccess
echo; echo "## Dọn"
rm -f $DA/public/o1.php $DA/public/o2.php /root/htaccess-a.goc
systemctl restart php$V-fpm; echo "đã dọn, đã khởi động lại PHP-FPM"
