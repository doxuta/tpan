#!/usr/bin/env bash
# e2e M18, đồ nghề: dựng máy chủ S3 thử versitygw v1.11.0 (backend posix) trong
# máy ảo, khoá sinh tại chỗ. Chạy TRONG máy ảo bằng root (scripts/e2e-m18.sh
# chép file này vào /root/e2e-m18/):
#
#   bash /root/e2e-m18/versitygw.sh
#
# Gói .deb tải từ trang phát hành của versity/versitygw, kiểm sha256 theo
# checksums.txt của chính bản phát hành trước khi cài. Cấu hình như bản thử
# docs/bang-chung/M18-nghien-cuu/04-versitygw-dung.sh: dịch vụ versitygw@tpan
# nghe 127.0.0.1:7070 (http, chỉ loopback), vùng "vn-thu-1", bucket "tpan-thu",
# mỗi object là một file thường /var/lib/s3thu/tpan-thu/<key>. Hai khoá nằm ở
# /root/s3thu.env (0600) và không bao giờ được in ra.
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive

ver=1.11.0
arch="$(dpkg --print-architecture)"
deb="versitygw_${ver}_linux_${arch}.deb"
url="https://github.com/versity/versitygw/releases/download/v$ver"
dl=/root/e2e-m18/versitygw-tai

if ! dpkg-query -W versitygw >/dev/null 2>&1; then
  rm -rf "$dl"
  install -d -m 0700 "$dl"
  cd "$dl"
  curl -fsSL --max-time 300 -o "$deb" "$url/$deb"
  curl -fsSL --max-time 60 -o checksums.txt "$url/checksums.txt"
  echo "dòng của $deb trong checksums.txt của bản phát hành:"
  grep -F "$deb" checksums.txt
  # --ignore-missing: checksums.txt kê mọi file của bản phát hành, ta chỉ tải
  # một. Không có dòng nào khớp file đã tải thì sha256sum cũng thoát khác 0.
  sha256sum --ignore-missing -c checksums.txt
  apt-get -o DPkg::Lock::Timeout=600 install -y -q "./$deb" </dev/null >/dev/null
  cd /
fi
dpkg-query -W -f 'gói: ${Package} ${Version}\n' versitygw

root=/var/lib/s3thu
install -d -m 0700 "$root" "$root/tpan-thu"
if [ ! -f /root/s3thu.env ]; then
  ak="TPANTHU$(openssl rand -hex 6 | tr a-f A-F)"
  sk="$(openssl rand -base64 30 | tr -d '\n')"
  (umask 077 && printf 'export S3_ENDPOINT=http://127.0.0.1:7070\nexport S3_REGION=vn-thu-1\nexport S3_BUCKET=tpan-thu\nexport S3_ACCESS_KEY=%s\nexport S3_SECRET_KEY=%s\n' "$ak" "$sk" >/root/s3thu.env)
fi
# shellcheck disable=SC1091
. /root/s3thu.env
install -d -m 0755 /etc/versitygw.d
(umask 077 && printf 'VGW_BACKEND=posix\nVGW_BACKEND_ARG=%s\nROOT_ACCESS_KEY_ID=%s\nROOT_SECRET_ACCESS_KEY=%s\nVGW_PORT=127.0.0.1:7070\nVGW_REGION=%s\n' \
  "$root" "$S3_ACCESS_KEY" "$S3_SECRET_KEY" "$S3_REGION" >/etc/versitygw.d/tpan.conf)
systemctl restart versitygw@tpan
for _ in $(seq 1 30); do
  ss -tln | grep -q '127.0.0.1:7070 ' && break
  sleep 1
done
echo "versitygw@tpan: $(systemctl is-active versitygw@tpan)"
ss -tln | grep ':7070 ' || echo "(chưa thấy cổng 7070)"
echo "khoá truy cập dài ${#S3_ACCESS_KEY} ký tự, khoá bí mật dài ${#S3_SECRET_KEY} ký tự (không in giá trị)"
