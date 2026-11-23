#!/usr/bin/env bash
# M18, đợt 0: dựng máy chủ S3 thử versitygw (backend posix) trong máy ảo, với
# khoá sinh tại chỗ. Chạy TRONG máy ảo bằng root:
#
#   scripts/vm.sh root bash -s < docs/bang-chung/M18-nghien-cuu/04-versitygw-dung.sh
#
# Gói versitygw_1.11.0_linux_<arch>.deb phải đã cài (tải từ trang phát hành của
# versity/versitygw, kiểm sha256 theo checksums.txt của bản phát hành).
# Kết quả: dịch vụ versitygw@tpan nghe 127.0.0.1:7070 (http), vùng "vn-thu-1",
# bucket "tpan-thu"; khoá nằm ở /root/s3thu.env (0600) để các bản thử sau nạp.
set -euo pipefail

root=/var/lib/s3thu
install -d -m 0700 "$root" "$root/tpan-thu"
if [ ! -f /root/s3thu.env ]; then
  ak="TPANTHU$(openssl rand -hex 6 | tr a-f A-F)"
  sk="$(openssl rand -base64 30 | tr -d '\n')"
  umask 077
  printf 'export S3_ENDPOINT=http://127.0.0.1:7070\nexport S3_REGION=vn-thu-1\nexport S3_BUCKET=tpan-thu\nexport S3_ACCESS_KEY=%s\nexport S3_SECRET_KEY=%s\n' "$ak" "$sk" >/root/s3thu.env
fi
# shellcheck disable=SC1091
. /root/s3thu.env
umask 077
printf 'VGW_BACKEND=posix\nVGW_BACKEND_ARG=%s\nROOT_ACCESS_KEY_ID=%s\nROOT_SECRET_ACCESS_KEY=%s\nVGW_PORT=127.0.0.1:7070\nVGW_REGION=%s\n' \
  "$root" "$S3_ACCESS_KEY" "$S3_SECRET_KEY" "$S3_REGION" >/etc/versitygw.d/tpan.conf
systemctl restart versitygw@tpan
sleep 1
systemctl is-active versitygw@tpan
ss -tlnp | grep 7070 || true
echo "khoá truy cập dài ${#S3_ACCESS_KEY} ký tự, khoá bí mật dài ${#S3_SECRET_KEY} ký tự (không in giá trị)"
