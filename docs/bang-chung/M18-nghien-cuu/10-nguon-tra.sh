#!/usr/bin/env bash
# M18, đợt 0: các nguồn đã tra cho những con số không thử được trong máy ảo
# (giới hạn cỡ user-data của từng nhà cung cấp, giấy phép và tình trạng bảo trì
# của thư viện). Chạy TRONG máy ảo (chỉ đọc mạng):
#
#   scripts/vm.sh exec bash -s < docs/bang-chung/M18-nghien-cuu/10-nguon-tra.sh
#
# Mỗi nguồn: tải trang, lọc bỏ thẻ HTML, in các câu có con số cần tìm.
set -u
echo "== ngay tra: $(date -u +%Y-%m-%dT%H:%M:%SZ)"

page() {
  # $1 = url, $2 = biểu thức grep -E (không phân biệt hoa thường)
  echo
  echo "== $1"
  curl -fsSL --max-time 30 -A "curl (tpan M18 research)" "$1" |
    sed -e 's/<[^>]*>/ /g' -e 's/&nbsp;/ /g' -e 's/&#39;/'"'"'/g' -e 's/&quot;/"/g' -e 's/[[:space:]]\{2,\}/ /g' |
    grep -iE -o ".{0,160}($2).{0,200}" | head -4 || echo "   (không lấy được hoặc không có câu khớp)"
}

page https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/instancedata-add-user-data.html '16 ?KB'
page https://docs.cloud-init.io/en/latest/explanation/format.html 'gzip|compress'
page https://docs.cloud-init.io/en/latest/explanation/security.html 'user.?data|sensitive|secret'
page https://docs.cloud-init.io/en/latest/reference/faq.html 'user.?data.*(secure|sensitive|password)|not secure'
page https://docs.digitalocean.com/products/droplets/how-to/provide-user-data/ '64 ?KiB|64 ?KB'
page https://docs.hetzner.cloud/reference/cloud '32 ?KiB|32KiB'
page https://docs.openstack.org/nova/latest/user/metadata.html '65535'
page https://cloud.google.com/compute/docs/metadata/setting-custom-metadata '256 ?KB'
page https://learn.microsoft.com/en-us/azure/virtual-machines/user-data '64 ?KB'

repo() {
  echo
  echo "== repo $1"
  curl -fsS --max-time 20 "https://api.github.com/repos/$1" |
    jq -r '"archived=\(.archived) license=\(.license.spdx_id // "none") pushed_at=\(.pushed_at) stars=\(.stargazers_count)"' || echo "KHONG HOI DUOC $1"
}
repo aws/aws-sdk-go-v2
repo minio/minio-go
repo FiloSottile/age

echo
echo "== age trong apt của Ubuntu 24.04 (công cụ giải mã độc lập với tpan)"
apt-cache policy age | sed -n 1,3p
