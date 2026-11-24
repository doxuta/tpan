#!/usr/bin/env bash
# M19 đợt 1 (thợ 1b), bản thử t5 cho nginx: bố cục cổng 443 của mục 3.4, như
# t5-apache-443.sh nhưng vhost KHÔNG viết tay: nó là output của
# templates.SiteVhostNames (nginx, ssl) sinh trên Mac bằng
#   M19_VM_DUMP=<thư mục> go test ./internal/templates/ -run TestM19DumpForVM
# rồi chép vào máy ảo. Bộ tên: tên chính example.com; tên phụ serve
# xn--vitnam-jk8b.example.net (có trong chứng chỉ); tên phụ redirect
# www.example.com (có trong chứng chỉ); tên phụ serve uncovered.example.net
# (cố ý KHÔNG có trong chứng chỉ).
#
# Máy ảo tpan-m19 (Ubuntu 24.04 arm64), chỉ cài gói nginx (không cài TPAN, không
# PHP): đo bằng /readme.html tĩnh. Chạy bằng root. $1 = thư mục chứa output đã sinh.
#
# Khác Apache ở T04: vhost mặc định 443 của TPAN cho nginx là tpan-default-ssl
# với ssl_reject_handshake on, nên tên chưa phủ qua https KHÔNG ra mã HTTP nào:
# bắt tay TLS bị từ chối (curl mã 35). Apache trả 403 vì vhost mặc định của nó
# có chứng chỉ snakeoil. Cả hai đều đúng: tên chưa phủ không tới site.
#
# Phần hai (nginx -t từng biến thể): mỗi file thuong/long/tamkhoa × có/không SSL
# của nginx đặt một mình làm vhost của site rồi chạy nginx -t.
#
# Phần ba ($2 = apache, chạy sau khi dừng nginx và cài gói apache2): apachectl -t
# cho từng biến thể Apache (cũng output của các hàm mới, cùng bộ tên), rồi
# T01-T11 qua Apache với vhost thuong-apache-ssl (T04: 403 của vhost mặc định
# snakeoil, như t5-apache-443).
set -u
G="$1"
r() { # r <nhãn> <scheme> <tên> <đường dẫn>
  local port=80; [ "$2" = https ] && port=443
  local out
  out=$(curl -sk -m 5 -o /tmp/t5.body -w '%{http_code} loc=%{redirect_url}' --resolve "$3:$port:127.0.0.1" "$2://$3$4" 2>&1)
  local rc=$?
  local cn=""
  if [ "$2" = https ]; then
    cn=$(echo | openssl s_client -connect 127.0.0.1:443 -servername "$3" 2>/dev/null | openssl x509 -noout -subject 2>/dev/null | tr -d '\n')
    [ -n "$cn" ] || cn="(không có chứng chỉ: bắt tay bị từ chối)"
  fi
  printf '%-46s curl=%s %s %s body=[%s]\n' "$1" "$rc" "$out" "$cn" "$(head -c 30 /tmp/t5.body 2>/dev/null | tr -c '[:print:]' '.')"
  rm -f /tmp/t5.body
}
if [ "${2:-}" = apache ]; then
  a2enmod ssl rewrite proxy_fcgi >/dev/null
  a2dissite 000-default >/dev/null 2>&1
  cp "$G/apache-default.conf" /etc/apache2/sites-available/000-tpan-default.conf
  cp "$G/apache-default-ssl.conf" /etc/apache2/sites-available/000-tpan-default-ssl.conf
  a2ensite 000-tpan-default 000-tpan-default-ssl >/dev/null
  echo "== apachectl -t từng biến thể (cùng bộ tên)"
  for v in thuong-apache thuong-apache-ssl long-apache long-apache-ssl tamkhoa-apache tamkhoa-apache-ssl; do
    cp "$G/$v.conf" /etc/apache2/sites-available/tpan-example.com.conf
    a2ensite tpan-example.com >/dev/null
    out=$(apachectl -t 2>&1); rc=$?
    printf '%-20s mã %s | %s\n' "$v" "$rc" "$(echo "$out" | grep -v AH00558 | tr '\n' ' ')"
  done
  echo "== apache2ctl -S với thuong-apache-ssl"
  cp "$G/thuong-apache-ssl.conf" /etc/apache2/sites-available/tpan-example.com.conf
  apache2ctl -S 2>&1 | grep -E 'namevhost|alias'
  echo "== curl T01-T11 qua Apache (vhost thuong-apache-ssl, không PHP: đo /readme.html)"
  mkdir -p /var/log/tpan/sites/example.com
  systemctl reload apache2 || systemctl restart apache2
  sleep 3
  echo "== 443"
  r "T01 https tên chính /readme.html"            https example.com /readme.html
  r "T02 https tên phụ serve có phủ"              https xn--vitnam-jk8b.example.net /readme.html
  r "T03 https tên phụ redirect có phủ /x?y=1"    https www.example.com '/x?y=1'
  r "T04 https tên phụ chưa phủ (vhost mặc định)" https uncovered.example.net /readme.html
  echo "== 80"
  r "T05 http tên phụ serve có phủ /a?b=1"        http xn--vitnam-jk8b.example.net '/a?b=1'
  r "T06 http tên phụ chưa phủ /a"                http uncovered.example.net /a
  r "T07 http tên phụ redirect /a"                http www.example.com /a
  r "T08 http thử thách: serve có phủ"            http xn--vitnam-jk8b.example.net /.well-known/acme-challenge/t5probe
  r "T09 http thử thách: chưa phủ"                http uncovered.example.net /.well-known/acme-challenge/t5probe
  r "T10 http thử thách: redirect"                http www.example.com /.well-known/acme-challenge/t5probe
  r "T11 http tên chính /a (khối có sẵn)"         http example.com /a
  echo xong
  exit 0
fi
AV=/etc/nginx/sites-available
EN=/etc/nginx/sites-enabled
LIVE=/etc/letsencrypt/live/example.com

mkdir -p "$LIVE" /var/log/tpan/sites/example.com /var/www/example.com/public \
  /var/lib/tpan/acme/.well-known/acme-challenge /var/lib/tpan/suspended
openssl req -x509 -newkey rsa:2048 -nodes -days 2 -keyout "$LIVE/privkey.pem" -out "$LIVE/fullchain.pem" \
  -subj "/CN=example.com" \
  -addext "subjectAltName=DNS:example.com,DNS:www.example.com,DNS:xn--vitnam-jk8b.example.net" 2>/dev/null
echo "== SAN thử: $(openssl x509 -in "$LIVE/fullchain.pem" -noout -ext subjectAltName | tail -1 | tr -s ' ')"
echo '<!DOCTYPE html><html>t5-readme</html>' >/var/www/example.com/public/readme.html
echo t5-acme-ok >/var/lib/tpan/acme/.well-known/acme-challenge/t5probe
echo tpan-suspended-page >/var/lib/tpan/suspended/tpan-suspended.html

rm -f "$EN/default"
cp "$G/nginx-default.conf" "$AV/tpan-default.conf"
cp "$G/nginx-default-ssl.conf" "$AV/tpan-default-ssl.conf"
ln -sf "$AV/tpan-default.conf" "$EN/tpan-default.conf"
ln -sf "$AV/tpan-default-ssl.conf" "$EN/tpan-default-ssl.conf"
cp "$G/thuong-nginx-ssl.conf" "$AV/tpan-example.com.conf"
ln -sf "$AV/tpan-example.com.conf" "$EN/tpan-example.com.conf"

echo "== vhost (bỏ chú thích): server_name và đích"
grep -nE 'listen|server_name|return 301|ssl_certificate ' "$AV/tpan-example.com.conf"
echo "== nginx -t"; nginx -t 2>&1; echo "mã $?"
systemctl reload nginx || systemctl restart nginx
sleep 3

echo "== 443"
r "T01 https tên chính /readme.html"            https example.com /readme.html
r "T02 https tên phụ serve có phủ"              https xn--vitnam-jk8b.example.net /readme.html
r "T03 https tên phụ redirect có phủ /x?y=1"    https www.example.com '/x?y=1'
r "T04 https tên phụ chưa phủ (vhost mặc định)" https uncovered.example.net /readme.html
echo "== 80"
r "T05 http tên phụ serve có phủ /a?b=1"        http xn--vitnam-jk8b.example.net '/a?b=1'
r "T06 http tên phụ chưa phủ /a"                http uncovered.example.net /a
r "T07 http tên phụ redirect /a"                http www.example.com /a
r "T08 http thử thách: serve có phủ"            http xn--vitnam-jk8b.example.net /.well-known/acme-challenge/t5probe
r "T09 http thử thách: chưa phủ"                http uncovered.example.net /.well-known/acme-challenge/t5probe
r "T10 http thử thách: redirect"                http www.example.com /.well-known/acme-challenge/t5probe
r "T11 http tên chính /a (khối có sẵn)"         http example.com /a

echo "== nginx -t từng biến thể (cùng bộ tên)"
for v in thuong-nginx thuong-nginx-ssl long-nginx long-nginx-ssl tamkhoa-nginx tamkhoa-nginx-ssl; do
  cp "$G/$v.conf" "$AV/tpan-example.com.conf"
  out=$(nginx -t 2>&1); rc=$?
  printf '%-20s mã %s | %s | cảnh báo: %s\n' "$v" "$rc" "$(echo "$out" | tail -1)" "$(echo "$out" | grep -c '\[warn\]')"
done
cp "$G/thuong-nginx-ssl.conf" "$AV/tpan-example.com.conf"
echo xong
