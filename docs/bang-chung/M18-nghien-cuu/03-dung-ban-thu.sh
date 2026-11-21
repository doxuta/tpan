#!/usr/bin/env bash
# M18, đợt 0: build client S3 bản thử (ban-thu-s3/) cho kiến trúc của máy ảo
# và chép vào máy ảo thành /usr/local/bin/s3thu. Chạy trên Mac:
#
#   make m18-nc S=03-dung-ban-thu
set -euo pipefail
cd "$(dirname "$0")/../../.."
here=docs/bang-chung/M18-nghien-cuu
arch="$(scripts/vm.sh exec dpkg --print-architecture </dev/null | tr -d '\r')"
out="$(mktemp -d)/s3thu"
CGO_ENABLED=0 GOOS=linux GOARCH="$arch" GOWORK=off go build -C "$here/ban-thu-s3" -trimpath -ldflags '-s -w' -o "$out" .
ls -l "$out"
# Chép bằng `vm.sh push` (multipass transfer). Lần đầu thử chép qua stdin của
# `vm.sh exec bash -c 'cat > file'`: file 5,7 MB tới nơi còn 2,5 MB, không có
# lỗi nào, chạy thì Segmentation fault. Vì vậy vm.sh có thêm lệnh con push/pull.
scripts/vm.sh push "$out" /tmp/s3thu
scripts/vm.sh root install -m 0755 /tmp/s3thu /usr/local/bin/s3thu </dev/null
scripts/vm.sh exec bash -c 'rm -f /tmp/s3thu; ls -l /usr/local/bin/s3thu' </dev/null
