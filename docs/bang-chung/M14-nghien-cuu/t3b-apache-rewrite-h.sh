#!/bin/bash
# Bổ sung cho t3: sau khi áp cách chặn 1, thử đường còn lại là cờ [H=] và [E=] của RewriteRule.
# Chạy trên Mac:  multipass exec tpan-nc -- sudo bash < t3b-apache-rewrite-h.sh
set -u
A=a.example.com; B=b.example.com; DA=/var/www/$A; DB=/var/www/$B
UA=$(stat -c %U $DA); UB=$(stat -c %U $DB); V=$(php -r 'echo PHP_MAJOR_VERSION.".".PHP_MINOR_VERSION;')
SOCKB=$(awk -F' = ' '/^listen = /{print $2}' "$(grep -l "^user = $UB" /etc/php/*/fpm/pool.d/*.conf)")
VH=/etc/apache2/sites-available/tpan-$A.conf; HT=$DA/public/.htaccess
ca(){ local p=$1; shift; curl -s --max-time 10 --resolve $A:80:127.0.0.1 "$@" "http://$A/$p"; }
cb(){ local p=$1; shift; curl -s --max-time 10 --resolve $B:80:127.0.0.1 "$@" "http://$B/$p"; }
cp -p $HT /root/ht.goc; cp -p $VH /root/vh.goc
runuser -u $UA -- tee /tmp/tpan-nc-dau.php >/dev/null <<'EOF'
<?php echo "MA DO A VIET dang chay duoi user: ".posix_getpwuid(posix_geteuid())["name"]."\n"; exit;
EOF
chmod 644 /tmp/tpan-nc-dau.php; runuser -u $UA -- touch $DA/public/qua.php
RULE="RewriteEngine On
RewriteRule ^qua\\.php\$ - [H=proxy:unix:$SOCKB|fcgi://localhost,E=PHP_ADMIN_VALUE:auto_prepend_file=/tmp/tpan-nc-dau.php,E=PHP_VALUE:auto_prepend_file=/tmp/tpan-nc-dau.php,L]"
dat(){ { echo "$RULE"; cat /root/ht.goc; } > $HT; chown $UA:www-data $HT; chmod 640 $HT; }
echo "## Chưa chặn (vhost gốc của TPAN): RewriteRule [H=socket B, E=PHP_ADMIN_VALUE]"
dat; echo "  GET A/qua.php -> $(ca qua.php | head -c 120 | tr '\n' ' ')"
systemctl restart php$V-fpm; sleep 1
echo "## Đã chặn: thêm 2 dòng gỡ biến vào vhost (chưa cần danh sách cho phép)"
sed -i 's|</VirtualHost>|    ProxyFCGISetEnvIf "true" !PHP_VALUE\n    ProxyFCGISetEnvIf "true" !PHP_ADMIN_VALUE\n</VirtualHost>|' $VH
apache2ctl configtest 2>&1 | tail -1; systemctl reload apache2; sleep 1
echo "  GET A/qua.php -> HTTP $(ca qua.php -o /dev/null -w '%{http_code}') | thân: $(ca qua.php | head -c 120 | tr '\n' ' ')"
echo "  trang chủ B: $(cb '' | head -c 40 | tr '\n' ' ')"
cp -p /root/ht.goc $HT; cp -p /root/vh.goc $VH; rm -f /root/ht.goc /root/vh.goc $DA/public/qua.php /tmp/tpan-nc-dau.php
apache2ctl configtest 2>&1 | tail -1; systemctl reload apache2; systemctl restart php$V-fpm; echo "đã trả về"
