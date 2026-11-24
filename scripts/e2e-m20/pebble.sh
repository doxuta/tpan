#!/usr/bin/env bash
# ĐỒ NGHỀ TEST của scripts/e2e-m20.sh (T50), chỉ chạy TRONG máy ảo test, bằng
# root. Dựng pebble (máy chủ ACME thử của Let's Encrypt, gói Ubuntu) như
# e2e-m13 và docs/bang-chung/M20-nghien-cuu/thu-vm.sh: CA của API pebble vào
# kho tin cậy của máy, pebble nghe ở 127.0.0.1:14000, kiểm HTTP-01 ở cổng 80.
# KHÔNG phải cách triển khai thật.
#
#   pebble.sh <thư mục> <tên unit systemd>
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
d=$1 unit=$2
apt-get -o DPkg::Lock::Timeout=180 install -y -q pebble >/dev/null
dpkg-query -W -f 'gói: ${Package} ${Version}\n' pebble
mkdir -p "$d"
chmod 700 "$d"
cd "$d"
openssl req -x509 -newkey rsa:2048 -nodes -days 7 -keyout ca.key -out ca.pem \
  -subj "/CN=tpan e2e m20 pebble API CA (chi may ao test)" -addext "keyUsage=critical,keyCertSign,cRLSign" 2>/dev/null
openssl req -newkey rsa:2048 -nodes -keyout key.pem -out api.csr -subj "/CN=localhost" 2>/dev/null
printf "subjectAltName=DNS:localhost,IP:127.0.0.1\nbasicConstraints=CA:FALSE\nkeyUsage=critical,digitalSignature,keyEncipherment\nextendedKeyUsage=serverAuth\n" >api.ext
openssl x509 -req -in api.csr -CA ca.pem -CAkey ca.key -CAcreateserial -days 7 -out cert.pem -extfile api.ext 2>/dev/null
cp ca.pem /usr/local/share/ca-certificates/tpan-e2e-m20-pebble.crt
update-ca-certificates 2>&1 | tail -1
printf '{"pebble":{"listenAddress":"127.0.0.1:14000","managementListenAddress":"127.0.0.1:15000","certificate":"%s/cert.pem","privateKey":"%s/key.pem","httpPort":80,"tlsPort":443,"ocspResponderURL":"","externalAccountBindingRequired":false}}\n' "$d" "$d" >config.json
systemd-run --unit="$unit" --setenv=PEBBLE_VA_NOSLEEP=1 --setenv=PEBBLE_WFE_NONCEREJECT=0 --setenv=PEBBLE_AUTHZREUSE=0 \
  /usr/bin/pebble -config "$d/config.json"
for _ in $(seq 1 30); do
  curl -s -o /dev/null https://localhost:14000/dir && break
  sleep 1
done
curl -sS -o /dev/null -w 'pebble /dir: HTTP %{http_code}\n' https://localhost:14000/dir
