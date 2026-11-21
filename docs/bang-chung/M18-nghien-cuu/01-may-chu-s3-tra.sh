#!/usr/bin/env bash
# M18, đợt 0, câu "máy chủ S3 để thử trong máy ảo: loại nào hiện còn cài được
# dễ dàng". Chạy TRONG máy ảo tpan-m18 (aarch64), chỉ đọc mạng, không cài gì:
#
#   scripts/vm.sh exec bash -s < docs/bang-chung/M18-nghien-cuu/01-may-chu-s3-tra.sh
#
# Hỏi API công khai của GitHub về tình trạng từng dự án (còn bảo trì không, giấy
# phép, bản phát hành mới nhất, có file chạy cho linux arm64 không) và thử HEAD
# vào đường tải file chạy. Kết quả ở 01-may-chu-s3-tra.out.
set -u

echo "== ngay tra: $(date -u +%Y-%m-%dT%H:%M:%SZ), may: $(uname -m), $(lsb_release -ds)"

repo() {
  # In vài trường của repo: archived, license, pushed_at, stars.
  local r="$1"
  echo
  echo "== repo $r"
  curl -fsS --max-time 20 "https://api.github.com/repos/$r" |
    jq -r '"archived=\(.archived) disabled=\(.disabled) license=\(.license.spdx_id // "none") pushed_at=\(.pushed_at) stars=\(.stargazers_count) description=\(.description)"' ||
    echo "KHONG HOI DUOC $r"
}

latest() {
  # Bản phát hành mới nhất và các file có chữ arm64/aarch64 cho linux.
  local r="$1"
  echo "-- latest release of $r"
  curl -fsS --max-time 20 "https://api.github.com/repos/$r/releases/latest" |
    jq -r '"tag=\(.tag_name) published_at=\(.published_at) assets=\(.assets | length)", (.assets[] | select(.name | test("(?i)linux")) | select(.name | test("(?i)(arm64|aarch64)")) | "  asset: \(.name) \(.size) bytes")' ||
    echo "KHONG CO BAN PHAT HANH (hoac khong hoi duoc) $r"
}

head_url() {
  echo "-- HEAD $1"
  curl -sS -o /dev/null --max-time 30 -L -w '  http=%{http_code} size=%header{content-length} type=%header{content-type} last-modified=%header{last-modified}\n' -I "$1" || echo "  KHONG NOI DUOC"
}

repo minio/minio
latest minio/minio
head_url https://dl.min.io/server/minio/release/linux-arm64/minio
head_url https://dl.min.io/client/mc/release/linux-arm64/mc

repo seaweedfs/seaweedfs
latest seaweedfs/seaweedfs

repo versity/versitygw
latest versity/versitygw

repo rustfs/rustfs
latest rustfs/rustfs

repo rclone/rclone
latest rclone/rclone

repo deuxfleurs-org/garage
head_url https://garagehq.deuxfleurs.fr/download/
echo "-- garage _releases.json (trang tai chinh thuc)"
curl -fsS --max-time 20 https://garagehq.deuxfleurs.fr/_releases.json | jq -r '.. | objects | select(has("url")) | select(.url | test("aarch64")) | "  \(.url)"' 2>/dev/null | tail -5 || echo "  KHONG DOC DUOC _releases.json"

echo
echo "== goi apt cua Ubuntu 24.04 (apt-cache policy)"
apt-cache policy rclone minio garage seaweedfs s3cmd awscli s5cmd 2>/dev/null | grep -E '^[a-z0-9]|Candidate' | paste - - | sed 's/  */ /g'

echo
echo "== gioi han API GitHub con lai"
curl -fsS --max-time 20 https://api.github.com/rate_limit | jq -r '"core remaining=\(.resources.core.remaining) / \(.resources.core.limit)"'
