#!/bin/bash
# Dựng pebble (máy chủ ACME thử) trong máy ảo test, như e2e M14 và M13: ĐỒ NGHỀ
# TEST, chỉ trong máy ảo (unit chạy pebble, CA thử trong kho tin cậy). CHỈ chạy
# trong máy ảo test của mốc, bằng root. Trên Mac, trong thư mục repo:
#
#   scripts/vm.sh root bash -s -- <thư mục làm việc> <tên unit> < scripts/e2e-m17/pebble.sh
#
# Máy chủ nghe ở https://localhost:14000/dir; thử thách HTTP-01 đi tới cổng 80
# của máy (nên tên miền thử phải trỏ về 127.0.0.1 trong /etc/hosts).
# Toàn bộ nằm trong hàm và dòng cuối gọi main (xem cgroup.sh).
set -u

main() {
  local d=${1:?thiếu thư mục} unit=${2:?thiếu tên unit} i
  export DEBIAN_FRONTEND=noninteractive
  apt-get -o DPkg::Lock::Timeout=120 install -y -q pebble </dev/null >/dev/null || return 1
  dpkg-query -W -f 'gói: ${Package} ${Version}\n' pebble
  mkdir -p "$d"
  chmod 700 "$d"
  cd "$d" || return 1
  openssl req -x509 -newkey rsa:2048 -nodes -days 7 -keyout ca.key -out ca.pem \
    -subj "/CN=tpan e2e pebble API CA (chi may ao test)" -addext "keyUsage=critical,keyCertSign,cRLSign" 2>/dev/null || return 1
  openssl req -newkey rsa:2048 -nodes -keyout key.pem -out api.csr -subj "/CN=localhost" 2>/dev/null || return 1
  printf "subjectAltName=DNS:localhost,IP:127.0.0.1\nbasicConstraints=CA:FALSE\nkeyUsage=critical,digitalSignature,keyEncipherment\nextendedKeyUsage=serverAuth\n" >api.ext
  openssl x509 -req -in api.csr -CA ca.pem -CAkey ca.key -CAcreateserial -days 7 -out cert.pem -extfile api.ext 2>/dev/null || return 1
  cp ca.pem /usr/local/share/ca-certificates/tpan-e2e-pebble.crt
  update-ca-certificates 2>&1 | tail -1
  printf '{"pebble":{"listenAddress":"127.0.0.1:14000","managementListenAddress":"127.0.0.1:15000","certificate":"%s/cert.pem","privateKey":"%s/key.pem","httpPort":80,"tlsPort":443,"ocspResponderURL":"","externalAccountBindingRequired":false}}\n' "$d" "$d" >config.json
  systemd-run --unit="$unit" --setenv=PEBBLE_VA_NOSLEEP=1 --setenv=PEBBLE_WFE_NONCEREJECT=0 --setenv=PEBBLE_AUTHZREUSE=0 \
    /usr/bin/pebble -config "$d/config.json" </dev/null || return 1
  for i in $(seq 1 30); do
    curl -s -o /dev/null https://localhost:14000/dir && return 0
    sleep 1
  done
  echo "pebble không trả lời sau 30 giây"
  return 1
}

main "$@"; exit $?
