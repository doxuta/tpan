#!/bin/bash
# Bản thử "lồng theo site": site A có dịch vụ PHP-FPM riêng do systemd quản, với
#   - cây thư mục riêng (không thấy site khác), /proc chỉ thấy tiến trình của mình, /tmp riêng
#   - trần RAM, CPU, số tiến trình (cgroups)
# Site B vẫn dùng PHP-FPM chung như TPAN hiện nay để so sánh. Stack nginx.
# Chạy trên Mac:  multipass exec tpan-nc -- sudo bash < t5-long-theo-site.sh
set -u
A=a.example.com; B=b.example.com; DA=/var/www/$A; DB=/var/www/$B
UA=$(stat -c %U $DA); UB=$(stat -c %U $DB); V=$(php -r 'echo PHP_MAJOR_VERSION.".".PHP_MINOR_VERSION;')
VH=/etc/nginx/sites-available/tpan-$A.conf; U=tpan-nc-php-a
ca(){ local p=$1; shift; curl -s --max-time 30 --resolve $A:80:127.0.0.1 "$@" "http://$A/$p"; }
cb(){ local p=$1; shift; curl -s --max-time 30 --resolve $B:80:127.0.0.1 "$@" "http://$B/$p"; }
as_a(){ runuser -u "$UA" -- "$@"; }
cp -p $VH /root/vh.goc
SOCKA_CU=$(grep -o 'unix:[^;]*' $VH | head -1 | cut -d: -f2)

mkdir -p /etc/tpan-nc
cat > /etc/tpan-nc/fpm-a.conf <<EOF
[global]
pid = /run/php/$U.pid
error_log = /run/php/$U.log
daemonize = no
[$U]
user = $UA
group = $UA
listen = /run/php/$U.sock
listen.owner = www-data
listen.group = www-data
listen.mode = 0660
pm = ondemand
pm.max_children = 5
pm.process_idle_timeout = 10s
php_admin_value[open_basedir] = $DA/
php_admin_value[upload_tmp_dir] = $DA/tmp
php_admin_value[session.save_path] = $DA/tmp
php_admin_value[sys_temp_dir] = $DA/tmp
php_admin_value[error_log] = /var/log/tpan/sites/$A/php-error.log
EOF
cat > /etc/systemd/system/$U.service <<EOF
[Unit]
Description=PHP-FPM rieng cua $A (ban thu long theo site)
After=network.target
[Service]
Type=notify
ExecStart=/usr/sbin/php-fpm$V --nodaemonize --fpm-config /etc/tpan-nc/fpm-a.conf
# --- long: site chi thay thu muc cua minh ---
ProtectSystem=strict
ReadWritePaths=/run/php $DA /var/log/tpan/sites/$A
TemporaryFileSystem=/var/www:ro
BindPaths=$DA
ProtectHome=yes
PrivateTmp=yes
ProtectProc=invisible
PrivateDevices=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
NoNewPrivileges=yes
RestrictSUIDSGID=yes
# --- tran tai nguyen ---
MemoryMax=200M
MemorySwapMax=0
CPUQuota=50%
TasksMax=40
# Mac dinh OOMPolicy=stop: mot tien trinh con bi giet vi het RAM thi systemd dung ca dich vu
# (lan chay dau: site 502 luon). continue = chi tien trinh do chet, dich vu van song.
OOMPolicy=continue
Restart=on-failure
EOF
systemctl daemon-reload; systemctl start $U; sleep 1
echo "## 0. Dịch vụ riêng của A: $(systemctl is-active $U)"
sed -i "s|unix:$SOCKA_CU|unix:/run/php/$U.sock|" $VH; nginx -t 2>&1 | tail -1; systemctl reload nginx; sleep 1
echo "  WordPress của A qua dịch vụ riêng: HTTP $(ca '' -o /dev/null -w '%{http_code}') | của B (FPM chung): HTTP $(cb '' -o /dev/null -w '%{http_code}')"

SPHP='<?php
echo "user=".posix_getpwuid(posix_geteuid())["name"]."\n";
echo "ls /var/www: ".trim((string)@shell_exec("ls /var/www | tr \"\\n\" \" \""))."\n";
echo "so tien trinh cua user khac nhin thay: ".trim((string)@shell_exec("ps -eo user= | grep -vc \"^$(id -un)\$\""))."\n";
echo "ghi duoc /etc/thu: ".(@file_put_contents("/etc/tpan-nc-thu","x")!==false || trim((string)@shell_exec("touch /etc/tpan-nc-thu 2>&1"))==="" && file_exists("/etc/tpan-nc-thu") ? "CO" : "KHONG")."\n";
echo "ls /tmp: [".trim((string)@shell_exec("ls /tmp | head -5 | tr \"\\n\" \" \""))."]\n";'
echo "$SPHP" | as_a tee $DA/public/s.php >/dev/null
echo "$SPHP" > $DB/public/s.php; chown $UB:www-data $DB/public/s.php
echo; echo "## 1. Site nhìn thấy gì qua lệnh shell"
echo " A (trong lồng):";   ca s.php | sed 's/^/   /'
echo " B (FPM chung, như TPAN hiện nay):"; cb s.php | sed 's/^/   /'

echo; echo "## 2. OPcache: A include wp-config.php của B (không bật validate_permission)"
as_a tee $DA/public/o2.php >/dev/null <<EOF
<?php
register_shutdown_function(function(){ echo "include wp-config.php cua B: ".(defined("DB_PASSWORD")?"LAY DUOC hang DB_PASSWORD":"khong lay duoc")."\n"; });
@include "$DB/public/wp-config.php";
EOF
cb "" -o /dev/null; echo -n "  "; ca o2.php | tail -1

echo; echo "## 3. Trần RAM 200M: script cố cấp phát 400 MB"
MPHP='<?php ini_set("memory_limit","-1"); $a=[]; for($i=0;$i<40;$i++){ $a[]=str_repeat(chr(65+$i%26), 10*1024*1024); } echo "da cap phat ".(count($a)*10)." MB\n";'
echo "$MPHP" | as_a tee $DA/public/m.php >/dev/null
echo "$MPHP" > $DB/public/m.php; chown $UB:www-data $DB/public/m.php
echo "  A (trong lồng): HTTP $(ca m.php -o /dev/null -w '%{http_code}') | $(ca m.php | head -c 60 | tr '\n' ' ')"
echo "  B (FPM chung):  HTTP $(cb m.php -o /dev/null -w '%{http_code}') | $(cb m.php | head -c 60 | tr '\n' ' ')"
journalctl -k --since "-2min" --no-pager 2>/dev/null | grep -i "oom\|killed process" | tail -2 | cut -c1-200
echo "  WordPress của A sau đó: HTTP $(ca '' -o /dev/null -w '%{http_code}')"

echo; echo "## 4. Trần CPU 50%: cùng một vòng lặp"
CPHP='<?php $t=microtime(true); $x=0; for($i=0;$i<150000000;$i++){ $x+=$i%7; } printf("%.1f giay\n", microtime(true)-$t);'
echo "$CPHP" | as_a tee $DA/public/c.php >/dev/null
echo "$CPHP" > $DB/public/c.php; chown $UB:www-data $DB/public/c.php
echo "  A (trong lồng, CPUQuota=50%): $(ca c.php)"
echo "  B (FPM chung):                $(cb c.php)"

echo; echo "## 5. Tốn thêm bao nhiêu RAM cho một site"
sleep 14   # chờ tiến trình con ondemand thoát, chỉ còn tiến trình chủ
systemctl show $U -p MemoryCurrent -p MemoryPeak -p TasksCurrent | tr '\n' ' '; echo
ps -o rss=,cmd= -C php-fpm$V | awk '{printf "   %6.1f MB  %s %s %s %s\n", $1/1024, $2, $3, $4, $5}'

echo; echo "## 6. Chạy lệnh cho site (WP-CLI, cron) trong cùng kiểu lồng bằng systemd-run"
WP=$(command -v wp || ls /usr/local/bin/wp /var/cache/tpan/wp-cli*.phar 2>/dev/null | head -1)
systemd-run --quiet --wait --pipe --collect -p User=$UA -p ProtectSystem=strict -p ReadWritePaths=$DA \
  -p TemporaryFileSystem=/var/www:ro -p BindPaths=$DA -p PrivateTmp=yes -p ProtectProc=invisible -p ProtectHome=yes \
  -p NoNewPrivileges=yes -p MemoryMax=300M -p CPUQuota=50% \
  sh -c "echo \"  user=\$(id -un) | ls /var/www: \$(ls /var/www | tr '\n' ' ') | tiến trình của user khác: \$(ps -eo user= | grep -vc \"^\$(id -un)\$\")\"; echo -n '  wp option get siteurl: '; php $WP option get siteurl --path=$DA/public 2>&1 | tail -1"

echo; echo "## Dọn"
cp -p /root/vh.goc $VH; nginx -t 2>&1 | tail -1; systemctl reload nginx
systemctl stop $U; rm -f /etc/systemd/system/$U.service /etc/tpan-nc/fpm-a.conf /run/php/$U.log; rmdir /etc/tpan-nc 2>/dev/null; systemctl daemon-reload
rm -f $DA/public/{s.php,o2.php,m.php,c.php} $DB/public/{s.php,m.php,c.php} /root/vh.goc /etc/tpan-nc-thu
systemctl restart php$V-fpm
echo "  sau khi dọn: A HTTP $(ca '' -o /dev/null -w '%{http_code}'), B HTTP $(cb '' -o /dev/null -w '%{http_code}')"
