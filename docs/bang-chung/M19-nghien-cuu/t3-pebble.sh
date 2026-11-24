#!/usr/bin/env bash
# M19 đợt 0, bản thử t3: certbot (bản của Ubuntu 24.04, cách TPAN đang xin chứng chỉ,
# internal/steps/ssl.go) với máy chủ ACME thử pebble trong máy ảo tpan-m19. Chạy bằng
# root trong máy ảo, sau `tpan site add example.com`. Đo:
#   C1 một tên; C2 thêm tên (cùng --cert-name); C3 thêm một tên không trỏ về máy;
#   C4 bớt tên; C5 trần số tên (pebble cũng giữ trần 100 như Let's Encrypt?).
# Pebble dựng như scripts/e2e-m10.sh (đồ nghề test, không phải cách triển khai).
set -u
export DEBIAN_FRONTEND=noninteractive
apt-get -o DPkg::Lock::Timeout=120 install -y -q pebble >/dev/null
dpkg-query -W -f 'gói: ${Package} ${Version}\n' pebble certbot
d=/etc/tpan-m19-pebble
mkdir -p "$d"; chmod 700 "$d"; cd "$d"
openssl req -x509 -newkey rsa:2048 -nodes -days 7 -keyout ca.key -out ca.pem \
  -subj "/CN=tpan m19 pebble API CA (chi may ao test)" -addext "keyUsage=critical,keyCertSign,cRLSign" 2>/dev/null
openssl req -newkey rsa:2048 -nodes -keyout key.pem -out api.csr -subj "/CN=localhost" 2>/dev/null
printf "subjectAltName=DNS:localhost,IP:127.0.0.1\nbasicConstraints=CA:FALSE\nkeyUsage=critical,digitalSignature,keyEncipherment\nextendedKeyUsage=serverAuth\n" >api.ext
openssl x509 -req -in api.csr -CA ca.pem -CAkey ca.key -CAcreateserial -days 7 -out cert.pem -extfile api.ext 2>/dev/null
cp ca.pem /usr/local/share/ca-certificates/tpan-m19-pebble.crt
update-ca-certificates >/dev/null 2>&1
printf '{"pebble":{"listenAddress":"127.0.0.1:14000","managementListenAddress":"127.0.0.1:15000","certificate":"%s/cert.pem","privateKey":"%s/key.pem","httpPort":80,"tlsPort":443,"ocspResponderURL":"","externalAccountBindingRequired":false}}\n' "$d" "$d" >config.json
systemctl stop tpan-m19-pebble 2>/dev/null
systemd-run --unit=tpan-m19-pebble --setenv=PEBBLE_VA_NOSLEEP=1 --setenv=PEBBLE_WFE_NONCEREJECT=0 --setenv=PEBBLE_AUTHZREUSE=0 \
  /usr/bin/pebble -config "$d/config.json" >/dev/null 2>&1
for i in $(seq 1 30); do curl -s -o /dev/null https://localhost:14000/dir && break; sleep 1; done
echo "pebble: $(curl -s https://localhost:14000/dir | tr -d ' \n' | head -c 80)"

# Tên thử trỏ về máy (chỉ trong máy ảo). nodns.example.invalid cố ý KHÔNG có.
grep -q 'm19-t3' /etc/hosts || printf '127.0.0.1 example.com www.example.com alias.example.net # m19-t3\n' >>/etc/hosts

S=https://localhost:14000/dir
cb() { # cb <nhãn> <tên...>: certbot như steps.CertbotCert, nhiều -d
  local label=$1; shift
  local args=(certonly --webroot -w /var/lib/tpan/acme --cert-name example.com --non-interactive --agree-tos -m admin@example.com --server "$S")
  for n in "$@"; do args+=(-d "$n"); done
  echo "== $label: certbot ${args[*]}"
  certbot "${args[@]}" >/tmp/t3.out 2>&1
  local rc=$?
  grep -vE '^\s*$|Saving debug log|^- - - -' /tmp/t3.out | tail -14
  echo "mã thoát certbot: $rc"
  show
}
show() {
  local c=/etc/letsencrypt/live/example.com/cert.pem
  if [ -e "$c" ]; then
    echo "   chứng chỉ đang dùng: $(openssl x509 -in "$c" -noout -serial) | $(openssl x509 -in "$c" -noout -ext subjectAltName | tail -1 | tr -s ' ')"
  else
    echo "   chưa có chứng chỉ"
  fi
}

cb "C1 một tên" example.com
cb "C2 thêm www (cùng --cert-name)" example.com www.example.com
cb "C3 thêm tên chưa trỏ về máy" example.com www.example.com nodns.example.invalid
cb "C3b thêm tên trỏ về máy" example.com www.example.com alias.example.net
cb "C4 bớt tên" example.com www.example.com
cb "C4b chạy lại đúng bộ tên (không đổi gì?)" example.com www.example.com

echo "== C5 trần số tên: 101 tên (pebble)"
names=(example.com)
for i in $(seq 1 100); do names+=("n$i.example.com"); done
printf '127.0.0.1 %s # m19-t3\n' "${names[@]:1}" >>/etc/hosts
cb "C5 101 tên" "${names[@]}"

echo "== dọn"
certbot delete --cert-name example.com --non-interactive >/dev/null 2>&1; echo "certbot delete mã $?"
sed -i '/# m19-t3$/d' /etc/hosts
systemctl stop tpan-m19-pebble
rm -f /usr/local/share/ca-certificates/tpan-m19-pebble.crt; update-ca-certificates --fresh >/dev/null 2>&1
echo xong
