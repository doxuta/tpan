#!/usr/bin/env bash
# M18, đợt 0: kho nội bộ dùng chứng chỉ do một CA riêng ký (đích có --ca-file).
# e2e phần M cần đúng cấu hình này để máy ảo phụ nói chuyện với kho trên máy ảo
# chính qua https. Chạy TRONG máy ảo bằng root:
#
#   scripts/vm.sh root bash -s < docs/bang-chung/M18-nghien-cuu/14-tls-ca-rieng.sh
#
# Cần versitygw và s3thu của các bản thử trước.
set -u
ip="$(hostname -I | cut -d' ' -f1)"
d=/root/tls-thu
rm -rf "$d" && mkdir -p "$d" && cd "$d" && umask 077
echo "== $(date -u +%FT%TZ): IP của máy $ip"

# CA riêng và chứng chỉ máy chủ có SAN là IP của máy (kho được gọi bằng IP).
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes -days 30 -subj "/CN=tpan thu CA" -keyout ca.key -out ca.pem 2>/dev/null
openssl req -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes -subj "/CN=$ip" -keyout srv.key -out srv.csr 2>/dev/null
printf 'subjectAltName=IP:%s,IP:127.0.0.1\nbasicConstraints=CA:FALSE\nextendedKeyUsage=serverAuth\n' "$ip" >ext.cnf
openssl x509 -req -in srv.csr -CA ca.pem -CAkey ca.key -CAcreateserial -days 30 -extfile ext.cnf -out srv.pem 2>/dev/null
openssl x509 -in srv.pem -noout -subject -ext subjectAltName | tr '\n' ' '; echo

# shellcheck disable=SC1091
. /root/s3thu.env
install -d -m 0700 /var/lib/s3thu-tls /var/lib/s3thu-tls/tpan-thu
umask 077
printf 'VGW_BACKEND=posix\nVGW_BACKEND_ARG=/var/lib/s3thu-tls\nROOT_ACCESS_KEY_ID=%s\nROOT_SECRET_ACCESS_KEY=%s\nVGW_PORT=0.0.0.0:7443\nVGW_REGION=%s\nVGW_CERT=%s\nVGW_KEY=%s\n' \
  "$S3_ACCESS_KEY" "$S3_SECRET_KEY" "$S3_REGION" "$d/srv.pem" "$d/srv.key" >/etc/versitygw.d/tpantls.conf
systemctl restart versitygw@tpantls; sleep 1
systemctl is-active versitygw@tpantls; ss -tln | grep 7443

echo "--- 1. https tới IP của máy, KHÔNG đưa CA: phải bị từ chối vì không tin chứng chỉ"
S3_ENDPOINT="https://$ip:7443" s3thu test; echo "mã thoát $?"
echo "--- 2. https tới IP của máy, có CA riêng: đạt"
S3_ENDPOINT="https://$ip:7443" S3_CA_FILE="$d/ca.pem" s3thu test; echo "mã thoát $?"
echo "--- 3. đúng CA nhưng gọi bằng tên không có trong chứng chỉ (localhost): phải bị từ chối"
S3_ENDPOINT="https://localhost:7443" S3_CA_FILE="$d/ca.pem" s3thu test; echo "mã thoát $?"
echo "--- 4. http thường vào cổng TLS: phải lỗi, không lặng lẽ chạy"
S3_ENDPOINT="http://$ip:7443" s3thu test; echo "mã thoát $?"

systemctl stop versitygw@tpantls
rm -rf "$d" /etc/versitygw.d/tpantls.conf /var/lib/s3thu-tls
echo "đã dọn"
