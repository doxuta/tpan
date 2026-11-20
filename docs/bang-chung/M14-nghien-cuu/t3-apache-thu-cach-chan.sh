#!/bin/bash
# Thử các cách chặn trên stack Apache, rồi trả máy về như cũ.
# Chạy trên Mac:  multipass exec tpan-nc -- sudo bash < t3-apache-thu-cach-chan.sh
set -u
A=a.example.com; B=b.example.com
DA=/var/www/$A; DB=/var/www/$B
UA=$(stat -c %U $DA); UB=$(stat -c %U $DB)
V=$(php -r 'echo PHP_MAJOR_VERSION.".".PHP_MINOR_VERSION;')
POOLB=$(grep -l "^user = $UB" /etc/php/*/fpm/pool.d/*.conf)
SOCKB=$(awk -F' = ' '/^listen = /{print $2}' "$POOLB")
VH=/etc/apache2/sites-available/tpan-$A.conf
ca(){ local p=$1; shift; curl -s --max-time 10 --resolve $A:80:127.0.0.1 "$@" "http://$A/$p"; }
cb(){ local p=$1; shift; curl -s --max-time 10 --resolve $B:80:127.0.0.1 "$@" "http://$B/$p"; }
as_a(){ runuser -u "$UA" -- "$@"; }
code(){ ca "$1" -o /dev/null -w '%{http_code}'; }
HT=$DA/public/.htaccess
cp -p $HT /root/htaccess-a.goc; cp -p $VH /root/vhost-a.goc
tra_ht(){ cp -p /root/htaccess-a.goc $HT; }
them_dau(){ { cat; cat /root/htaccess-a.goc; } > /root/ht.moi; cp /root/ht.moi $HT; chown $UA:www-data $HT; chmod 640 $HT; }

as_a tee $DA/public/t.php >/dev/null <<'EOF'
<?php echo "user=".posix_getpwuid(posix_geteuid())["name"]." | open_basedir=".ini_get("open_basedir")."\n";
EOF
as_a tee /tmp/tpan-nc-dau.php >/dev/null <<EOF
<?php echo "MA DO A VIET dang chay duoi user: ".posix_getpwuid(posix_geteuid())["name"]."\n"; exit;
EOF
chmod 644 /tmp/tpan-nc-dau.php; as_a touch $DA/public/qua.php

echo "## Cách chặn 1: vhost của A dùng danh sách cho phép + gỡ PHP_VALUE/PHP_ADMIN_VALUE"
# Apache chỉ nhận tối đa 64 tên trên một dòng AllowOverrideList (phần thừa bị bỏ lặng lẽ),
# nên đây là danh sách chọn tay: thứ WordPress và plugin hay dùng, KHÔNG có SetHandler,
# AddHandler, Action, ProxyFCGISetEnvIf.
DS="<Files <FilesMatch <IfModule <If <Else <ElseIf <Limit <LimitExcept <RequireAll <RequireAny <RequireNone RewriteEngine RewriteRule RewriteCond RewriteBase RewriteOptions Require Order Allow Deny Satisfy AuthType AuthName AuthUserFile AuthGroupFile AuthBasicProvider Header RequestHeader ExpiresActive ExpiresByType ExpiresDefault FileETag AddType AddEncoding AddCharset AddDefaultCharset AddOutputFilterByType AddOutputFilter SetOutputFilter SetEnv SetEnvIf SetEnvIfNoCase UnsetEnv BrowserMatch BrowserMatchNoCase Options DirectoryIndex ErrorDocument Redirect RedirectMatch RedirectPermanent RedirectTemp LimitRequestBody ServerSignature AddLanguage DefaultLanguage"
echo "  số chỉ thị cho phép trong .htaccess: $(echo $DS | wc -w)"
python3 - "$VH" "$DS" <<'PY'
import sys
p, ds = sys.argv[1], sys.argv[2]
s = open(p).read()
assert "AllowOverride All" in s
s = s.replace("AllowOverride All", "AllowOverride None\n        AllowOverrideList " + ds, 1)
s = s.replace("</VirtualHost>", '    ProxyFCGISetEnvIf "true" !PHP_VALUE\n    ProxyFCGISetEnvIf "true" !PHP_ADMIN_VALUE\n</VirtualHost>')
open(p, "w").write(s)
PY
apache2ctl configtest 2>&1 | tail -1; systemctl reload apache2; sleep 1

echo; echo "## a. WordPress còn chạy không"
echo "  trang chủ: HTTP $(code '')   |   đường dẫn đẹp không có thật (/khong-co/): HTTP $(code 'khong-co/') (404 do WordPress trả = rewrite còn chạy: $(ca 'khong-co/' | grep -c 'wp-'))"

echo; echo "## b. .htaccess thêm: SetEnv PHP_ADMIN_VALUE \"open_basedir=/\""
printf 'SetEnv PHP_ADMIN_VALUE "open_basedir=/"\nSetEnv PHP_VALUE "open_basedir=/"\n' | them_dau
echo -n "  "; ca t.php; tra_ht

echo; echo "## c. .htaccess dùng ProxyFCGISetEnvIf (cách tấn công ở t1 mục 7)"
them_dau <<EOF
<Files "qua.php">
    SetHandler "proxy:unix:$SOCKB|fcgi://localhost"
    ProxyFCGISetEnvIf "true" SCRIPT_FILENAME "$DB/public/index.php"
    ProxyFCGISetEnvIf "true" PHP_ADMIN_VALUE "open_basedir=/ \n auto_prepend_file=/tmp/tpan-nc-dau.php"
</Files>
EOF
echo "  GET A/qua.php -> HTTP $(code qua.php) | trang chủ A: HTTP $(code '') | trang chủ B: HTTP $(cb '' -o /dev/null -w '%{http_code}')"
tail -1 /var/log/tpan/sites/$A/error.log | cut -c1-220; tra_ht

echo; echo "## d. Chỉ dùng chỉ thị còn được phép: SetHandler + SetEnv, rồi RewriteRule [P,E=]"
them_dau <<EOF
<Files "qua.php">
    SetHandler "proxy:unix:$SOCKB|fcgi://localhost"
    SetEnv PHP_ADMIN_VALUE "auto_prepend_file=/tmp/tpan-nc-dau.php"
    SetEnv PHP_VALUE "auto_prepend_file=/tmp/tpan-nc-dau.php"
</Files>
EOF
echo "  SetHandler sang socket B: HTTP $(code qua.php) | thân: $(ca qua.php | head -c 80 | tr '\n' ' ')"; tra_ht
them_dau <<EOF
RewriteEngine On
RewriteRule ^qua2\$ "unix:$SOCKB|fcgi://localhost$DB/public/index.php" [P,E=PHP_ADMIN_VALUE:auto_prepend_file=/tmp/tpan-nc-dau.php,E=PHP_VALUE:auto_prepend_file=/tmp/tpan-nc-dau.php]
EOF
R=$(ca qua2); echo "  RewriteRule [P] sang socket B: HTTP $(code qua2) | mã của A có chạy không: $(echo "$R" | grep -c 'MA DO A VIET') | B trả trang của chính nó: $(echo "$R" | grep -c -i '<html')"; tra_ht
echo "  trang chủ B sau các lần thử: $(cb '' | head -c 60 | tr '\n' ' ')"

echo; echo "## e. Chỉ thị mà plugin WordPress hay ghi vào .htaccess"
them_dau <<'EOF'
Options -Indexes
<IfModule mod_headers.c>
    Header set X-Thu "1"
</IfModule>
<IfModule mod_deflate.c>
    AddOutputFilterByType DEFLATE text/html text/css
</IfModule>
<FilesMatch "\.(ini|log|sql)$">
    Require all denied
</FilesMatch>
SetEnvIfNoCase User-Agent "bot" la_bot
ErrorDocument 404 /index.php
<If "%{HTTP_HOST} == 'khong.co'">
    Header set X-If "1"
</If>
AddType image/webp .webp
FileETag None
EOF
echo "  trang chủ A: HTTP $(code '') | có header X-Thu: $(ca '' -o /dev/null -D - | grep -ci '^x-thu')"
tail -1 /var/log/tpan/sites/$A/error.log | cut -c1-220; tra_ht

echo; echo "## Cách chặn 2: OPcache kiểm quyền file và khoá API"
printf 'opcache.validate_permission=1\nopcache.restrict_api=/usr/local/lib/tpan/khong-co/\n' > /etc/php/$V/fpm/conf.d/99-tpan-nc.ini
systemctl restart php$V-fpm; sleep 1
as_a tee $DA/public/o1.php >/dev/null <<'EOF'
<?php $s = @opcache_get_status(true); echo $s ? "opcache_get_status: goi duoc, ".count($s["scripts"])." file\n" : "opcache_get_status: bi chan\n";
EOF
as_a tee $DA/public/o2.php >/dev/null <<EOF
<?php
register_shutdown_function(function(){ echo "include wp-config.php cua B: ".(defined("DB_PASSWORD")?"LAY DUOC hang DB_PASSWORD":"khong lay duoc")."\n"; });
@include "$DB/public/wp-config.php";
EOF
cb "" -o /dev/null; echo -n "  "; ca o1.php
for i in 1 2 3; do cb "" -o /dev/null; echo -n "  lần $i: "; ca o2.php | tail -1; done
echo "  WordPress của A và B còn chạy: HTTP $(code '') / $(cb '' -o /dev/null -w '%{http_code}')"

echo; echo "## Trả máy về như cũ"
cp -p /root/vhost-a.goc $VH; tra_ht; rm -f /etc/php/$V/fpm/conf.d/99-tpan-nc.ini
rm -f $DA/public/{t.php,qua.php,o1.php,o2.php} /tmp/tpan-nc-dau.php /root/htaccess-a.goc /root/vhost-a.goc /root/ht.moi
apache2ctl configtest 2>&1 | tail -1; systemctl reload apache2; systemctl restart php$V-fpm; echo "đã trả về"
