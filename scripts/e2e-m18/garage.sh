#!/usr/bin/env bash
# e2e M18, đồ nghề: dựng máy chủ S3 thử thứ hai, Garage v2.5.0 (một node), trong
# máy ảo, khoá sinh tại chỗ. Chạy TRONG máy ảo bằng root:
#
#   bash /root/e2e-m18/garage.sh
#
# Như bản thử docs/bang-chung/M18-nghien-cuu/06-garage-dung.sh, thêm chọn file
# chạy theo kiến trúc của máy. Garage nghe 127.0.0.1:3900, vùng "garage",
# bucket "tpan-thu"; khoá ở /root/garage.env (0600), không in ra.
set -euo pipefail

case "$(dpkg --print-architecture)" in
  arm64) triple=aarch64-unknown-linux-musl ;;
  amd64) triple=x86_64-unknown-linux-musl ;;
  *) echo "kiến trúc $(dpkg --print-architecture) không có file chạy Garage" >&2; exit 1 ;;
esac
url="https://garagehq.deuxfleurs.fr/_releases/v2.5.0/$triple/garage"
if [ ! -x /usr/local/bin/garage ]; then
  curl -fsSL --max-time 300 -o /root/e2e-m18/garage.tai "$url"
  install -m 0755 /root/e2e-m18/garage.tai /usr/local/bin/garage
  rm -f /root/e2e-m18/garage.tai
fi
garage --version
g() { garage -c /etc/garage.toml "$@" </dev/null; }

install -d -m 0700 /var/lib/garage/meta /var/lib/garage/data
if [ ! -f /etc/garage.toml ]; then
  (umask 077 && printf '%s\n' \
    'metadata_dir = "/var/lib/garage/meta"' \
    'data_dir = "/var/lib/garage/data"' \
    'db_engine = "sqlite"' \
    'replication_factor = 1' \
    'rpc_bind_addr = "127.0.0.1:3901"' \
    'rpc_public_addr = "127.0.0.1:3901"' \
    "rpc_secret = \"$(openssl rand -hex 32)\"" \
    '' \
    '[s3_api]' \
    's3_region = "garage"' \
    'api_bind_addr = "127.0.0.1:3900"' \
    'root_domain = ".s3.garage.localhost"' \
    '' \
    '[admin]' \
    'api_bind_addr = "127.0.0.1:3903"' \
    "admin_token = \"$(openssl rand -base64 32)\"" >/etc/garage.toml)
fi

printf '%s\n' \
  '[Unit]' \
  'Description=Garage (may chu S3 thu cua e2e tpan M18)' \
  'After=network-online.target' \
  '' \
  '[Service]' \
  'ExecStart=/usr/local/bin/garage -c /etc/garage.toml server' \
  'Restart=on-failure' \
  '' \
  '[Install]' \
  'WantedBy=multi-user.target' >/etc/systemd/system/garage-thu.service
systemctl daemon-reload
systemctl restart garage-thu
for _ in $(seq 1 30); do
  g status >/dev/null 2>&1 && break
  sleep 1
done
g status

node="$(g node id -q | cut -d@ -f1)"
if ! g layout show 2>/dev/null | grep -q "${node:0:16}"; then
  g layout assign -z dc1 -c 5G "$node"
  g layout apply --version 1
fi
g bucket info tpan-thu >/dev/null 2>&1 || g bucket create tpan-thu
if [ ! -f /root/garage.env ]; then
  out="$(g key create tpan-thu-key)"
  ak="$(printf '%s\n' "$out" | sed -n 's/^Key ID: *//p')"
  sk="$(printf '%s\n' "$out" | sed -n 's/^Secret key: *//p')"
  if [ -z "$ak" ] || [ -z "$sk" ]; then
    echo "không đọc được khoá từ output của garage key create (dạng output đã đổi?)" >&2
    printf '%s\n' "$out" | sed 's/\(Secret key: *\).*/\1(che)/' >&2
    exit 1
  fi
  (umask 077 && printf 'export S3_ENDPOINT=http://127.0.0.1:3900\nexport S3_REGION=garage\nexport S3_BUCKET=tpan-thu\nexport S3_ACCESS_KEY=%s\nexport S3_SECRET_KEY=%s\n' "$ak" "$sk" >/root/garage.env)
fi
g bucket allow --read --write --owner tpan-thu --key tpan-thu-key >/dev/null
for _ in $(seq 1 30); do
  ss -tln | grep -q '127.0.0.1:3900 ' && break
  sleep 1
done
echo "garage-thu: $(systemctl is-active garage-thu)"
ss -tln | grep ':3900 ' || echo "(chưa thấy cổng 3900)"
echo "xong: khoá ở /root/garage.env (không in giá trị)"
