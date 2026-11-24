#!/usr/bin/env bash
# M19 đợt 0, bản thử t1 (nginx 1.24, Ubuntu 24.04, máy ảo tpan-m19). Chạy bằng root
# trong máy ảo, sau `tpan stack install --web nginx` và `tpan site add example.com`.
#   $1 = đường dẫn vhost thử (t1-nginx-vhost.conf đã chép vào máy ảo)
# Thay vhost của example.com bằng bản thử, đo bằng curl, rồi trả vhost cũ lại.
set -u
NEW=$1
VH=/etc/nginx/sites-available/tpan-example.com.conf
PUB=/var/www/example.com/public
OWNER=$(stat -c %U "$PUB")
cp "$VH" /root/t1-vhost.bak

echo "== phiên bản"; nginx -v 2>&1
cp "$NEW" "$VH"
echo "== nginx -t"; nginx -t 2>&1; echo "mã $?"
systemctl reload nginx
# reload êm: worker cũ còn phục vụ theo cấu hình cũ một lúc (lần chạy đầu thấy 000 ở N02-N04).
sleep 3

# File thử: PHP trong uploads, thử thách ACME.
mkdir -p "$PUB/wp-content/uploads"
printf '<?php echo "PHP-RAN";\n' >"$PUB/wp-content/uploads/evil.php"
printf '<?php echo "PHP-RAN";\n' >"$PUB/wp-content/uploads/evil2.php"
chown "$OWNER:$OWNER" "$PUB/wp-content/uploads/evil.php" "$PUB/wp-content/uploads/evil2.php"
mkdir -p /var/lib/tpan/acme/.well-known/acme-challenge
echo t1-acme-ok >/var/lib/tpan/acme/.well-known/acme-challenge/t1probe

# r <nhãn> <Host hoặc - để bỏ Host> <đường dẫn> [cờ curl thêm...]
r() {
  local label=$1 host=$2 path=$3; shift 3
  local hdr=(-H "Host: $host")
  [ "$host" = "-" ] && hdr=(-H "Host;")
  local out body
  rm -f /tmp/t1.body
  out=$(curl -s -m 5 -o /tmp/t1.body -w '%{http_code} loc=%{redirect_url}' "${hdr[@]}" "$@" "http://127.0.0.1$path" 2>&1)
  local rc=$?
  body=$(head -c 60 /tmp/t1.body 2>/dev/null | tr -d '\n' | tr -c '[:print:]' '.')
  printf '%-34s curl=%s %s body=[%s]\n' "$label" "$rc" "$out" "$body"
}

echo "== tên chính, tên phụ phục vụ, tên phụ chuyển"
r "N01 tên chính /"                 example.com /
r "N02 tên phụ phục vụ (punycode) /" xn--vitnam-jk8b.example.net /
r "N03 tên phụ chuyển /"             www.example.com /
r "N04 tên phụ chuyển /a?b=1"        www.example.com '/a?b=1'
r "N05 tên phụ chuyển: thử thách"    www.example.com /.well-known/acme-challenge/t1probe
r "N06 tên chính: thử thách"         example.com /.well-known/acme-challenge/t1probe

echo "== luật chuyển hướng theo đường dẫn"
r "P01 /old-page"                    example.com /old-page
r "P02 /old-page?x=1 (giữ query?)"   example.com '/old-page?x=1'
r "P03 /old-page/ (không khớp)"      example.com /old-page/
r "P04 /OLD-PAGE (không khớp)"       example.com /OLD-PAGE
r "P05 //old-page (gộp gạch chéo)"   example.com //old-page
r "P06 /old%2Dpage (mã hoá %)"       example.com /old%2Dpage
r "P07 /keepq?a=1&b=2"               example.com '/keepq?a=1&b=2'
r "P08 /keepq (không query)"         example.com /keepq
r "P09 /dropq?a=1"                   example.com '/dropq?a=1'
r "P10 /old-page qua tên phụ phục vụ" xn--vitnam-jk8b.example.net /old-page
r "P11 /old-page qua tên phụ chuyển" www.example.com /old-page

echo "== luật chặn có sẵn vẫn đứng"
r "S01 uploads/evil.php (chặn)"      example.com /wp-content/uploads/evil.php
r "S02 uploads/evil2.php (có luật =)" example.com /wp-content/uploads/evil2.php
r "S03 /.env (chặn file ẩn)"         example.com /.env
r "S04 tên phụ phục vụ: evil.php"    xn--vitnam-jk8b.example.net /wp-content/uploads/evil.php

echo "== bộ định tuyến dựng lại R01-R14 (cho tên phụ)"
r "R01 hoa: WWW.EXAMPLE.COM"         WWW.EXAMPLE.COM /
r "R02 hoa: XN--VITNAM-JK8B.EXAMPLE.NET" XN--VITNAM-JK8B.EXAMPLE.NET /
r "R03 chấm cuối: www.example.com."  www.example.com. /
r "R04 chấm cuối: punycode."         xn--vitnam-jk8b.example.net. /
r "R05 cổng: www.example.com:80"     www.example.com:80 /
r "R06 cổng lạ: www.example.com:8080" www.example.com:8080 /
r "R07 Unicode thô trong Host"       'việtnam.example.net' /
r "R08 Host lạ: evil.example.org"    evil.example.org /
r "R09 Host là IP 127.0.0.1"         127.0.0.1 /
r "R10 Host rỗng"                    - /
r "R11 Host dài 300 ký tự"           "$(printf 'a%.0s' $(seq 1 250)).example.com" /
r "R12 tên con chưa khai: x.www.example.com" x.www.example.com /
r "R13 dòng yêu cầu dạng URI tuyệt đối, Host khác" example.com / --request-target http://evil.example.org/
r "R14 URI tuyệt đối trỏ tên phụ, Host lạ" evil.example.org / --request-target http://www.example.com/x

echo "== \$host trong khối chuyển https (đo cho bản SSL)"
r "H01 hostprobe.example.com"        hostprobe.example.com /p
r "H02 HOSTPROBE.EXAMPLE.COM."       HOSTPROBE.EXAMPLE.COM. /p
r "H03 hostprobe.example.com:80"     hostprobe.example.com:80 /p
r "H04 URI tuyệt đối khác, Host đúng" hostprobe.example.com /p --request-target http://evil.example.org/p
r "H05 URI tuyệt đối đúng, Host lạ"  evil.example.org /p --request-target http://hostprobe.example.com/p

echo "== trả vhost cũ"
cp /root/t1-vhost.bak "$VH"
rm -f "$PUB/wp-content/uploads/evil.php" "$PUB/wp-content/uploads/evil2.php" /var/lib/tpan/acme/.well-known/acme-challenge/t1probe
nginx -t 2>&1 | tail -1
systemctl reload nginx
echo "xong"
