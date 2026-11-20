#!/bin/bash
# Thử cô lập giữa hai site do TPAN tạo, stack nginx (LEMP), rồi thử từng cách chặn.
# Chạy trên Mac:  multipass exec tpan-nc -- sudo bash < t4-nginx-co-lap.sh
set -u
A=a.example.com; B=b.example.com; DA=/var/www/$A; DB=/var/www/$B
UA=$(stat -c %U $DA); UB=$(stat -c %U $DB); V=$(php -r 'echo PHP_MAJOR_VERSION.".".PHP_MINOR_VERSION;')
VH=/etc/nginx/sites-available/tpan-$A.conf
ca(){ local p=$1; shift; curl -s --max-time 10 --resolve $A:80:127.0.0.1 "$@" "http://$A/$p"; }
cb(){ local p=$1; shift; curl -s --max-time 10 --resolve $B:80:127.0.0.1 "$@" "http://$B/$p"; }
as_a(){ runuser -u "$UA" -- "$@"; }
code(){ ca "$1" -o /dev/null -w '%{http_code}'; }
cp -p $VH /root/vh.goc
echo "## 0. Môi trường"; nginx -v 2>&1; php -v | head -1; echo "user A=$UA, user B=$UB"

echo; echo "## 1. Log của site B dưới nginx"
ls -l /var/log/tpan/sites/$B/ | tail -n +2
as_a head -c 10 /var/log/tpan/sites/$B/access.log >/dev/null 2>&1; echo "  user A đọc access.log của B: mã $? (0 = đọc được)"

echo; echo "## 2. Symlink qua nginx trên vhost thật của TPAN"
printf 'NOI_DUNG_RIENG_CUA_B\n' > $DB/public/rieng.txt
printf '<?php $khoa = "KHOA_API_TRONG_MA_NGUON_CUA_B";\n' > $DB/public/cau-hinh-rieng.php
chown $UB:www-data $DB/public/rieng.txt $DB/public/cau-hinh-rieng.php; chmod 640 $DB/public/rieng.txt $DB/public/cau-hinh-rieng.php
as_a ln -sf $DB/public/rieng.txt $DA/public/lay.txt
as_a ln -sf $DB/public/cau-hinh-rieng.php $DA/public/nguon.txt
as_a ln -sf $DB/public/wp-config.php $DA/public/wpc.txt
as_a ln -sfn $DA/public/wp-includes/css $DA/public/css-cua-toi
thu2(){ echo "  file thường của B (640, nhóm www-data): HTTP $(code lay.txt) | $(ca lay.txt | head -c 40 | tr '\n' ' ')"
        echo "  mã nguồn PHP của B qua tên .txt:        HTTP $(code nguon.txt) | $(ca nguon.txt | head -c 60 | tr '\n' ' ')"
        echo "  wp-config.php của B (600):              HTTP $(code wpc.txt)"
        echo "  symlink của A trỏ vào file của chính A: HTTP $(code css-cua-toi/dist/block-library/style.min.css)"; }
echo " Vhost gốc:"; thu2
sed -i 's|^\(\s*\)root \(.*\);|\1root \2;\n\1disable_symlinks if_not_owner from=$document_root;|' $VH
nginx -t 2>&1 | tail -1; systemctl reload nginx; sleep 2
echo " Sau khi thêm vào vhost: disable_symlinks if_not_owner from=\$document_root;"; thu2
echo "  trang chủ A: HTTP $(code '') | file tĩnh: HTTP $(code wp-includes/css/dist/block-library/style.min.css) | trang WP 404: HTTP $(code khong-co/)"
cp -p /root/vh.goc $VH; nginx -t 2>&1 | tail -1; systemctl reload nginx

echo; echo "## 3. OPcache (cấu hình mặc định, không có .htaccess nào)"
as_a tee $DA/public/o2.php >/dev/null <<EOF
<?php
register_shutdown_function(function(){ echo "open_basedir=".ini_get("open_basedir")." -> include wp-config.php cua B: ".(defined("DB_PASSWORD")?"LAY DUOC hang DB_PASSWORD":"khong lay duoc")."\n"; });
@include "$DB/public/wp-config.php";
EOF
cb "" -o /dev/null; echo -n "  mặc định: "; ca o2.php | tail -1
printf 'opcache.validate_permission=1\nopcache.restrict_api=/usr/local/lib/tpan/khong-co/\n' > /etc/php/$V/fpm/conf.d/99-tpan-nc.ini
systemctl restart php$V-fpm; sleep 1
cb "" -o /dev/null; echo -n "  sau khi đặt opcache.validate_permission=1: "; ca o2.php | tail -1
rm -f /etc/php/$V/fpm/conf.d/99-tpan-nc.ini; systemctl restart php$V-fpm; sleep 1

echo; echo "## 4. .user.ini của A cố nới open_basedir"
as_a tee $DA/public/t.php >/dev/null <<'EOF'
<?php echo "open_basedir=".ini_get("open_basedir")." | doc /etc/hostname bang PHP: ".(@file_get_contents("/etc/hostname")!==false?"CO":"KHONG")."\n";
EOF
printf 'open_basedir=/\n' | as_a tee $DA/public/.user.ini >/dev/null
systemctl restart php$V-fpm; sleep 1
echo -n "  "; ca t.php
rm -f $DA/public/.user.ini

echo; echo "## 5. open_basedir không quản tiến trình con: PHP gọi lệnh shell"
as_a tee $DA/public/s.php >/dev/null <<'EOF'
<?php
echo "disable_functions=[".ini_get("disable_functions")."]\n";
echo "doc /etc/hostname bang shell_exec: ".(trim((string)@shell_exec("cat /etc/hostname")) !== "" ? "CO" : "KHONG")."\n";
echo "ls /var/www: ".trim((string)@shell_exec("ls /var/www | tr '\n' ' '"))."\n";
echo "so tien trinh cua user khac nhin thay: ".trim((string)@shell_exec("ps -eo user= | grep -vc \"^$(id -un)\$\""))."\n";
EOF
ca s.php | sed 's/^/  /'

echo; echo "## 6. Thêm ProtectProc=invisible cho dịch vụ PHP-FPM (drop-in systemd)"
mkdir -p /etc/systemd/system/php$V-fpm.service.d
printf '[Service]\nProtectProc=invisible\n' > /etc/systemd/system/php$V-fpm.service.d/tpan-nc.conf
systemctl daemon-reload; systemctl restart php$V-fpm; sleep 1
ca s.php | tail -1 | sed 's/^/  /'
echo "  trang chủ A/B: HTTP $(code '') / $(cb '' -o /dev/null -w '%{http_code}')"
rm -rf /etc/systemd/system/php$V-fpm.service.d; systemctl daemon-reload; systemctl restart php$V-fpm

echo; echo "## Dọn"
rm -f $DA/public/{lay.txt,nguon.txt,wpc.txt,css-cua-toi,o2.php,t.php,s.php} $DB/public/rieng.txt $DB/public/cau-hinh-rieng.php /root/vh.goc
echo "đã dọn"
