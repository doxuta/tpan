#!/usr/bin/env bash
# M19 đợt 0, bản thử t5: bố cục cổng 443 (mục 3.4) trên Apache 2.4.58, máy ảo tpan-m19,
# stack Apache có site example.com (sau t2). Chạy bằng root. $1 = t5-apache-443.conf.
# Rút gọn: khối nội dung 443 không có PHP (đo bằng /readme.html tĩnh của WordPress).
set -u
CONF=/etc/apache2/sites-enabled/001-t5-m19.conf
mkdir -p /root/t5
openssl req -x509 -newkey rsa:2048 -nodes -days 2 -keyout /root/t5/key.pem -out /root/t5/cert.pem \
  -subj "/CN=example.com" \
  -addext "subjectAltName=DNS:example.com,DNS:www.example.com,DNS:xn--vitnam-jk8b.example.net" 2>/dev/null
echo "== SAN thử: $(openssl x509 -in /root/t5/cert.pem -noout -ext subjectAltName | tail -1 | tr -s ' ')"
a2query -m ssl >/dev/null 2>&1 || { a2enmod ssl >/dev/null; echo "(bật mod ssl)"; }
cp "$1" "$CONF"
echo "== apachectl -t"; apachectl -t 2>&1 | grep -v AH00558; echo "mã ${PIPESTATUS[0]}"
systemctl restart apache2; sleep 2
echo "== apache2ctl -S (443)"; apache2ctl -S 2>&1 | grep -E '443|namevhost|alias'
mkdir -p /var/lib/tpan/acme/.well-known/acme-challenge
echo t5-acme-ok >/var/lib/tpan/acme/.well-known/acme-challenge/t5probe

r() { # r <nhãn> <scheme> <tên> <đường dẫn>
  local port=80; [ "$2" = https ] && port=443
  local out
  out=$(curl -sk -m 5 -o /tmp/t5.body -w '%{http_code} loc=%{redirect_url}' --resolve "$3:$port:127.0.0.1" "$2://$3$4" 2>&1)
  local rc=$?
  local cn=""
  if [ "$2" = https ]; then
    cn=$(echo | openssl s_client -connect 127.0.0.1:443 -servername "$3" 2>/dev/null | openssl x509 -noout -subject 2>/dev/null | tr -d '\n')
  fi
  printf '%-46s curl=%s %s %s body=[%s]\n' "$1" "$rc" "$out" "$cn" "$(head -c 30 /tmp/t5.body 2>/dev/null | tr -c '[:print:]' '.')"
  rm -f /tmp/t5.body
}
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

echo "== dọn"
rm -f "$CONF" /var/lib/tpan/acme/.well-known/acme-challenge/t5probe
a2dismod ssl >/dev/null 2>&1
apachectl -t 2>&1 | grep -v AH00558
systemctl restart apache2
echo xong
