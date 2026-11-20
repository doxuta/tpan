#!/usr/bin/env bash
# M18, đợt 0: dựng máy chủ S3 thử thứ hai, Garage v2.5.0 (một node), trong máy
# ảo, để thấy client tự ký chạy với hai bản cài đặt S3 khác hẳn nhau (versitygw
# viết bằng Go, lưu object thành file thường; Garage viết bằng Rust, lưu thành
# khối có băm). Chạy TRONG máy ảo bằng root:
#
#   scripts/vm.sh root bash -s < docs/bang-chung/M18-nghien-cuu/06-garage-dung.sh
#
# Kết quả: Garage nghe 127.0.0.1:3900, vùng "garage", bucket "tpan-thu"; khoá ở
# /root/garage.env (0600).
set -euo pipefail

url=https://garagehq.deuxfleurs.fr/_releases/v2.5.0/aarch64-unknown-linux-musl/garage
if [ ! -x /usr/local/bin/garage ]; then
  curl -fsSL --max-time 300 -o /tmp/garage "$url"
  install -m 0755 /tmp/garage /usr/local/bin/garage
  rm -f /tmp/garage
fi
garage --version

install -d -m 0700 /var/lib/garage/meta /var/lib/garage/data
if [ ! -f /etc/garage.toml ]; then
  umask 077
  cat >/etc/garage.toml <<EOF
metadata_dir = "/var/lib/garage/meta"
data_dir = "/var/lib/garage/data"
db_engine = "sqlite"
replication_factor = 1
rpc_bind_addr = "127.0.0.1:3901"
rpc_public_addr = "127.0.0.1:3901"
rpc_secret = "$(openssl rand -hex 32)"

[s3_api]
s3_region = "garage"
api_bind_addr = "127.0.0.1:3900"
root_domain = ".s3.garage.localhost"

[admin]
api_bind_addr = "127.0.0.1:3903"
admin_token = "$(openssl rand -base64 32)"
EOF
fi

cat >/etc/systemd/system/garage-thu.service <<'EOF'
[Unit]
Description=Garage (may chu S3 thu cua tpan M18)
After=network-online.target

[Service]
ExecStart=/usr/local/bin/garage -c /etc/garage.toml server
Restart=on-failure

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl restart garage-thu
for _ in $(seq 1 30); do
  garage -c /etc/garage.toml status >/dev/null 2>&1 && break
  sleep 1
done
garage -c /etc/garage.toml status

node="$(garage -c /etc/garage.toml node id -q | cut -d@ -f1)"
if ! garage -c /etc/garage.toml layout show 2>/dev/null | grep -q "${node:0:16}"; then
  garage -c /etc/garage.toml layout assign -z dc1 -c 5G "$node"
  garage -c /etc/garage.toml layout apply --version 1
fi
garage -c /etc/garage.toml bucket info tpan-thu >/dev/null 2>&1 || garage -c /etc/garage.toml bucket create tpan-thu
if [ ! -f /root/garage.env ]; then
  out="$(garage -c /etc/garage.toml key create tpan-thu-key)"
  ak="$(printf '%s\n' "$out" | sed -n 's/^Key ID: *//p')"
  sk="$(printf '%s\n' "$out" | sed -n 's/^Secret key: *//p')"
  [ -n "$ak" ] && [ -n "$sk" ] || { echo "không đọc được khoá từ output của garage key create (dạng output đã đổi?)" >&2; printf '%s\n' "$out" | sed 's/\(Secret key: *\).*/\1(che)/' >&2; exit 1; }
  umask 077
  printf 'export S3_ENDPOINT=http://127.0.0.1:3900\nexport S3_REGION=garage\nexport S3_BUCKET=tpan-thu\nexport S3_ACCESS_KEY=%s\nexport S3_SECRET_KEY=%s\n' "$ak" "$sk" >/root/garage.env
fi
garage -c /etc/garage.toml bucket allow --read --write --owner tpan-thu --key tpan-thu-key >/dev/null
garage -c /etc/garage.toml bucket info tpan-thu | sed 's/\(secret[^:]*: *\).*/\1(che)/I' | head -20
echo "xong: khoá ở /root/garage.env (không in giá trị)"
