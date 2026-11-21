#!/usr/bin/env bash
# Thử của M20, đợt 0 (đăng nhập hai bước, token API, chứng chỉ panel được tin).
# Chạy TRONG máy ảo test bằng quyền root, không chạy ở đâu khác. Gọi từ Mac:
#
#   make thu-m20      (build, đưa máy ảo về sạch, chép tpan và script này vào, chạy, lưu log)
#
# Câu hỏi cần trả lời bằng output thật (docs/thay-doi/M20-thiet-ke.md, mục 2):
#   1. Đồng hồ của máy: có dịch vụ đồng bộ giờ nào chạy sẵn (systemd-timesyncd,
#      chrony)? TOTP chỉ đúng khi giờ máy chủ đúng.
#   2. Cổng 80 do web server của site giữ. Thử thách HTTP-01 cho tên của panel
#      (không phải tên site nào) có đi qua vhost mặc định của tpan, đọc từ webroot
#      ACME chung, mà không đụng vhost của site không? Trên nginx và trên apache.
#   3. certbot --webroot với một lineage riêng (--cert-name tpan-panel) xin được
#      chứng chỉ của pebble cho tên panel không; gia hạn ép chạy được không, và
#      hook deploy nào chạy sau đó.
#   4. Tên panel trùng tên một site: vhost của site có phục vụ thử thách từ cùng
#      webroot không (để biết vì sao phải từ chối trùng tên, hay chỉ cần lineage riêng).
#   5. Header CSP hiện tại của panel (QR vẽ bằng cách nào thì không bị chặn).
# Không dùng set -e: vài lệnh được chờ là phải lỗi.
set -uo pipefail

PANEL=panel.m20.test
SITE=s1.m20.test
ACME=https://localhost:14000/dir
PEBBLE_DIR=/etc/tpan-e2e-pebble
PEBBLE_UNIT=tpan-e2e-pebble
WEBROOT=/var/lib/tpan/acme

say() { printf '\n\n######## %s\n' "$*"; }
run() {
  printf '\n$ %s\n' "$*"
  "$@"
  printf '(mã thoát %s)\n' "$?"
}

say "0. Máy"
run lsb_release -ds
run uname -m
run tpan version

say "1. Đồng hồ của máy"
run timedatectl status
run timedatectl show -p NTP -p NTPSynchronized -p Timezone
run systemctl is-active systemd-timesyncd chrony
run dpkg-query -W -f '${Package} ${Version} ${Status}\n' systemd-timesyncd chrony

say "2a. Cài stack nginx, setup panel, một site"
run tpan stack install --web nginx --yes
run tpan setup --yes
run tpan site add "$SITE" --email admin@example.com --title Thu --yes

say "2b. Dựng pebble (máy chủ ACME thử, như e2e M5)"
export DEBIAN_FRONTEND=noninteractive
run apt-get -o DPkg::Lock::Timeout=180 install -y -q pebble
mkdir -p "$PEBBLE_DIR" && chmod 700 "$PEBBLE_DIR"
(
  cd "$PEBBLE_DIR" || exit 1
  openssl req -x509 -newkey rsa:2048 -nodes -days 7 -keyout ca.key -out ca.pem \
    -subj "/CN=tpan m20 pebble API CA (chi may ao test)" -addext "keyUsage=critical,keyCertSign,cRLSign" 2>/dev/null
  openssl req -newkey rsa:2048 -nodes -keyout key.pem -out api.csr -subj "/CN=localhost" 2>/dev/null
  printf "subjectAltName=DNS:localhost,IP:127.0.0.1\nbasicConstraints=CA:FALSE\nkeyUsage=critical,digitalSignature,keyEncipherment\nextendedKeyUsage=serverAuth\n" >api.ext
  openssl x509 -req -in api.csr -CA ca.pem -CAkey ca.key -CAcreateserial -days 7 -out cert.pem -extfile api.ext 2>/dev/null
  cp ca.pem /usr/local/share/ca-certificates/tpan-m20-pebble.crt
  update-ca-certificates >/dev/null 2>&1
  printf '{"pebble":{"listenAddress":"127.0.0.1:14000","managementListenAddress":"127.0.0.1:15000","certificate":"%s/cert.pem","privateKey":"%s/key.pem","httpPort":80,"tlsPort":443,"ocspResponderURL":"","externalAccountBindingRequired":false}}\n' "$PEBBLE_DIR" "$PEBBLE_DIR" >config.json
)
run systemd-run --unit="$PEBBLE_UNIT" --setenv=PEBBLE_VA_NOSLEEP=1 --setenv=PEBBLE_WFE_NONCEREJECT=0 --setenv=PEBBLE_AUTHZREUSE=0 \
  /usr/bin/pebble -config "$PEBBLE_DIR/config.json"
for _ in $(seq 1 30); do curl -s -o /dev/null "$ACME" && break; sleep 1; done
run curl -sS -o /dev/null -w 'pebble /dir: HTTP %{http_code}\n' "$ACME"
# pebble và curl đều đọc /etc/hosts: hai tên thử trỏ về chính máy.
printf '127.0.0.1 %s %s\n' "$PANEL" "$SITE" >>/etc/hosts
run grep -n "m20.test" /etc/hosts

probe() { # probe <nhãn>: file thử trong webroot chung, gọi qua tên panel và tên site
  local name="thu-$1" want="noi-dung-$1-$RANDOM"
  mkdir -p "$WEBROOT/.well-known/acme-challenge"
  printf '%s' "$want" >"$WEBROOT/.well-known/acme-challenge/$name"
  printf '\nfile thử %s, nội dung mong đợi: %s\n' "$name" "$want"
  run curl -sS -m 5 -w '  <- HTTP %{http_code}\n' "http://$PANEL/.well-known/acme-challenge/$name"
  run curl -sS -m 5 -o /dev/null -w 'trang chủ qua tên panel: HTTP %{http_code}\n' "http://$PANEL/"
  run curl -sS -m 5 -w '  <- HTTP %{http_code}\n' "http://$SITE/.well-known/acme-challenge/$name"
  rm -f "$WEBROOT/.well-known/acme-challenge/$name"
}

say "2c. nginx: tên panel (không phải site) rơi vào vhost mặc định; thử thách đọc từ webroot chung"
run ls /etc/nginx/sites-enabled/
probe nginx
run grep -n "acme-challenge\|default_server\|return 444" /etc/nginx/sites-available/tpan-default.conf

say "3a. certbot xin chứng chỉ cho tên panel, lineage riêng tpan-panel, từ pebble"
run certbot certonly --webroot -w "$WEBROOT" -d "$PANEL" --cert-name tpan-panel \
  --non-interactive --agree-tos -m admin@example.com --server "$ACME"
run ls -l /etc/letsencrypt/live/tpan-panel/
run openssl x509 -in /etc/letsencrypt/live/tpan-panel/fullchain.pem -noout -subject -issuer -serial -enddate -ext subjectAltName
run stat -c '%n %U:%G %a' /etc/letsencrypt/live /etc/letsencrypt/archive /etc/letsencrypt/archive/tpan-panel/privkey1.pem
run grep -n "server\|webroot\|authenticator" /etc/letsencrypt/renewal/tpan-panel.conf
run ls -la /etc/letsencrypt/renewal-hooks/deploy/

say "3b. gia hạn ép (như certbot.timer làm khi tới hạn), serial phải đổi; RENEWED_LINEAGE mà hook nhận"
mkdir -p /etc/letsencrypt/renewal-hooks/deploy
printf '#!/bin/sh\necho "hook thu: RENEWED_LINEAGE=$RENEWED_LINEAGE RENEWED_DOMAINS=$RENEWED_DOMAINS" >>/root/hook-thu.log\n' >/etc/letsencrypt/renewal-hooks/deploy/zz-thu
chmod 755 /etc/letsencrypt/renewal-hooks/deploy/zz-thu
run certbot renew --cert-name tpan-panel --force-renewal --no-random-sleep-on-renew --server "$ACME"
run openssl x509 -in /etc/letsencrypt/live/tpan-panel/fullchain.pem -noout -serial -enddate
run cat /root/hook-thu.log
rm -f /etc/letsencrypt/renewal-hooks/deploy/zz-thu
run systemctl list-timers certbot.timer --all --no-pager

say "4. tên panel trùng tên site: vhost của site cũng phục vụ thử thách từ webroot chung?"
run grep -n "acme-challenge" "/etc/nginx/sites-available/tpan-$SITE.conf"

say "5. CSP của panel"
run curl -skI https://127.0.0.1:8443/
run curl -skI https://127.0.0.1:8443/api/v1/session

say "6. apache: chuyển web server rồi thử lại thử thách và gia hạn"
run tpan stack switch --to apache --yes
run ls /etc/apache2/sites-enabled/
probe apache
run certbot renew --cert-name tpan-panel --force-renewal --no-random-sleep-on-renew --server "$ACME"
run openssl x509 -in /etc/letsencrypt/live/tpan-panel/fullchain.pem -noout -serial -enddate

say "7. Go tls nạp chứng chỉ do certbot ghi (PKCS#8 hay EC/RSA?)"
run head -1 /etc/letsencrypt/live/tpan-panel/privkey.pem
run openssl pkey -in /etc/letsencrypt/live/tpan-panel/privkey.pem -noout -text_pub 2>&1 | head -2

say "xong"
