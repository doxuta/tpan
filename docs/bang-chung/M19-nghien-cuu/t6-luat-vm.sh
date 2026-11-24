#!/usr/bin/env bash
# M19 đợt 2 (thợ 2a), bản thử t6: vhost có 50 luật chuyển hướng ngẫu nhiên hợp
# lệ, theo khuôn t5-nginx-443.sh. Vhost KHÔNG viết tay: nó là output của
# templates.SiteVhostNames / SiteVhostCagedNames sinh trên Mac bằng
#   M19_VM_DUMP_RULES=<thư mục> go test ./internal/templates/ -run TestM19RedirectsDumpForVM
#   M19_VM_DUMP=<thư mục>       go test ./internal/templates/ -run TestM19DumpForVM
# (luật: chuỗi ngẫu nhiên từ bảng ký tự cho phép, chỉ giữ cặp mà
# validate.ParseRedirectSource/Target nhận; hạt giống cố định; danh sách ở
# rules.txt: "nguồn mã đích giữ-query"). Bộ tên như t5.
#
# Máy ảo tpan-m19 (Ubuntu 24.04 arm64), chỉ cài gói nginx rồi apache2 (không cài
# TPAN, không PHP). Chạy bằng root. $1 = thư mục chứa output đã sinh.
#   $2 trống  : nginx -t cho 4 biến thể có luật, rồi curl cả 50 luật qua bản
#               thường cổng 80 (request mang ?x=1, so mã và Location với đích
#               mong đợi: đích đường dẫn thành http://example.com<đích>; giữ
#               query thì thêm ?x=1; bỏ query thì đích giữ nguyên). So header
#               Location thô (%header{location}), không dùng %{redirect_url}: curl
#               chuẩn hoá URL đó (thêm "/" khi đích không có đường dẫn), lần chạy
#               đầu ra 7 "lệch" giả vì vậy.
#   $2 = apache (sau khi dừng nginx, cài apache2): apachectl -t cho 4 biến thể,
#               rồi cùng phép curl qua Apache.
set -u
G="$1"

check_rules() { # check_rules <nhãn web>
  local ok=0 bad=0 src code dst keep exp out
  while read -r src code dst keep; do
    exp="$dst"
    case "$dst" in /*) exp="http://example.com$dst" ;; esac
    [ "$keep" = true ] && exp="$exp?x=1"
    out=$(curl -s -m 5 -o /dev/null -w '%{http_code} %header{location}' --resolve example.com:80:127.0.0.1 "http://example.com$src?x=1")
    if [ "$out" = "$code $exp" ]; then
      ok=$((ok + 1))
    else
      bad=$((bad + 1))
      printf 'LỆCH %s: nguồn %s muốn [%s %s] được [%s]\n' "$1" "$src" "$code" "$exp" "$out"
    fi
  done <"$G/rules.txt"
  echo "$1: $ok luật đúng, $bad lệch (trên $(wc -l <"$G/rules.txt"))"
  echo "== thử thách ACME và luật chặn có sẵn vẫn đứng ($1)"
  printf 'acme: %s\n' "$(curl -s -m 5 -w ' %{http_code}' --resolve example.com:80:127.0.0.1 http://example.com/.well-known/acme-challenge/t6probe)"
  printf '/.env: %s\n' "$(curl -s -m 5 -o /dev/null -w '%{http_code}' --resolve example.com:80:127.0.0.1 http://example.com/.env)"
}

LIVE=/etc/letsencrypt/live/example.com
if [ "${2:-}" = apache ]; then
  a2enmod ssl rewrite proxy_fcgi >/dev/null
  a2dissite 000-default >/dev/null 2>&1
  cp "$G/apache-default.conf" /etc/apache2/sites-available/000-tpan-default.conf
  cp "$G/apache-default-ssl.conf" /etc/apache2/sites-available/000-tpan-default-ssl.conf
  a2ensite 000-tpan-default 000-tpan-default-ssl >/dev/null
  echo "== phiên bản"; apache2 -v 2>&1 | head -1
  echo "== apachectl -t từng biến thể có 50 luật"
  for v in luat-thuong-apache luat-thuong-apache-ssl luat-long-apache luat-long-apache-ssl; do
    cp "$G/$v.conf" /etc/apache2/sites-available/tpan-example.com.conf
    a2ensite tpan-example.com >/dev/null
    out=$(apachectl -t 2>&1); rc=$?
    printf '%-24s mã %s | %s | dòng RewriteRule: %s\n' "$v" "$rc" "$(echo "$out" | grep -v AH00558 | tr '\n' ' ')" "$(grep -c '^    RewriteRule \^ ' "$G/$v.conf")"
  done
  echo "== curl 50 luật qua Apache (luat-thuong-apache, cổng 80)"
  cp "$G/luat-thuong-apache.conf" /etc/apache2/sites-available/tpan-example.com.conf
  systemctl reload apache2 || systemctl restart apache2
  sleep 3
  check_rules apache
  echo xong
  exit 0
fi

AV=/etc/nginx/sites-available
EN=/etc/nginx/sites-enabled
mkdir -p "$LIVE" /var/log/tpan/sites/example.com /var/www/example.com/public \
  /var/lib/tpan/acme/.well-known/acme-challenge
openssl req -x509 -newkey rsa:2048 -nodes -days 2 -keyout "$LIVE/privkey.pem" -out "$LIVE/fullchain.pem" \
  -subj "/CN=example.com" \
  -addext "subjectAltName=DNS:example.com,DNS:www.example.com,DNS:xn--vitnam-jk8b.example.net" 2>/dev/null
echo t6-acme-ok >/var/lib/tpan/acme/.well-known/acme-challenge/t6probe
echo "== phiên bản"; nginx -v 2>&1
rm -f "$EN/default"
cp "$G/nginx-default.conf" "$AV/tpan-default.conf"
cp "$G/nginx-default-ssl.conf" "$AV/tpan-default-ssl.conf"
ln -sf "$AV/tpan-default.conf" "$EN/tpan-default.conf"
ln -sf "$AV/tpan-default-ssl.conf" "$EN/tpan-default-ssl.conf"
ln -sf "$AV/tpan-example.com.conf" "$EN/tpan-example.com.conf"
echo "== nginx -t từng biến thể có 50 luật"
for v in luat-thuong-nginx luat-thuong-nginx-ssl luat-long-nginx luat-long-nginx-ssl; do
  cp "$G/$v.conf" "$AV/tpan-example.com.conf"
  out=$(nginx -t 2>&1); rc=$?
  printf '%-24s mã %s | %s | cảnh báo: %s | location =: %s\n' "$v" "$rc" "$(echo "$out" | tail -1)" \
    "$(echo "$out" | grep -c '\[warn\]')" "$(grep -c '^    location = ' "$G/$v.conf")"
done
echo "== curl 50 luật qua nginx (luat-thuong-nginx, cổng 80)"
cp "$G/luat-thuong-nginx.conf" "$AV/tpan-example.com.conf"
systemctl reload nginx || systemctl restart nginx
sleep 3
check_rules nginx
echo xong
