#!/usr/bin/env bash
# M18, đợt 0: thử client tự ký SigV4 (s3thu) với máy chủ S3 thử. Chạy TRONG máy
# ảo bằng root (đọc khoá ở file do bản thử dựng máy chủ ghi):
#
#   scripts/vm.sh root bash -s -- /root/s3thu.env < docs/bang-chung/M18-nghien-cuu/05-thu-co-ban.sh
#
# Tham số 1: file env của máy chủ cần thử (mặc định /root/s3thu.env).
set -u
envf="${1:-/root/s3thu.env}"
# shellcheck disable=SC1090
. "$envf"
echo "== máy chủ: $S3_ENDPOINT vùng $S3_REGION bucket $S3_BUCKET ($(date -u +%FT%TZ))"

step() { echo; echo "--- $*"; }

step "1. ghi, head, đọc lại, xoá một object nhỏ (tên có dấu cách, dấu tiếng Việt, ngoặc, &)"
s3thu test

step "2. sai khoá bí mật: phải bị từ chối, có mã lỗi đọc được"
S3_SECRET_KEY="sai-$S3_SECRET_KEY" s3thu test; echo "mã thoát $?"

step "3. sai vùng: kho có kiểm vùng trong chữ ký không"
S3_REGION=us-west-9 s3thu test; echo "mã thoát $?"

step "4. bucket không có"
S3_BUCKET=khong-co-bucket-nay s3thu test; echo "mã thoát $?"

step "5. file 20 MiB, phần 8 MiB (3 phần): tải lên nhiều phần, tải về, so sha256"
head -c $((20 * 1024 * 1024)) /dev/urandom >/root/thu-20m.bin
want="$(sha256sum /root/thu-20m.bin | cut -d' ' -f1)"
s3thu put /root/thu-20m.bin thu/20m.bin
s3thu head thu/20m.bin
s3thu get thu/20m.bin /root/thu-20m.out
got="$(sha256sum /root/thu-20m.out | cut -d' ' -f1)"
[ "$want" = "$got" ] && echo "ĐẠT: sha256 khớp $got" || echo "KHÔNG ĐẠT: $want != $got"

step "6. liệt kê và các lần tải dở"
s3thu list thu/
s3thu mpu ""

step "7. xoá"
s3thu rm thu/20m.bin
s3thu list thu/
rm -f /root/thu-20m.bin /root/thu-20m.out
