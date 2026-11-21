#!/usr/bin/env bash
# M18, đợt 0: e2e phần L và M cần máy ảo phụ (-ci) tải file chạy và nói chuyện
# với kho S3 đặt trên máy ảo chính. Thử trước: hai máy ảo Multipass có nối được
# tới nhau qua IP không, ngay trong lúc cloud-init của máy phụ đang chạy.
#
#   make m18-nc S=13-hai-may-ao-noi-nhau
set -uo pipefail
cd "$(dirname "$0")/../../.."
main="$(tr -d '[:space:]' <.tpan-vm)"
ci="$main-ci"
W="$(mktemp -d)"
trap 'rm -rf "$W"' EXIT
ip="$(scripts/vm.sh ip </dev/null)"
echo "== $(date -u +%FT%TZ): máy ảo chính $main có IP $ip"

scripts/vm.sh root bash -c 'mkdir -p /srv/thu && echo "xin chao tu may chinh" >/srv/thu/chao.txt; systemctl stop thu-http 2>/dev/null; systemd-run --quiet --unit=thu-http --working-directory=/srv/thu python3 -m http.server 8099 --bind 0.0.0.0; sleep 1; ss -tln | grep 8099; ufw status | head -1' </dev/null

cat >"$W/user-data.yaml" <<EOF
#cloud-config
runcmd:
  - [ sh, -c, 'curl -fsS -m 10 -o /root/chao.txt -w "http=%{http_code}\n" http://$ip:8099/chao.txt > /root/chao.rc 2>&1; echo "ma thoat curl: \$?" >> /root/chao.rc' ]
EOF
scripts/vm.sh delete "$ci" >/dev/null 2>&1
scripts/vm.sh cloud-init "$W/user-data.yaml" "$ci" >/dev/null 2>&1; echo "tạo máy phụ: mã $?"
scripts/vm.sh on "$ci" root bash -c 'cat /root/chao.rc; cat /root/chao.txt; hostname -I' </dev/null
scripts/vm.sh delete "$ci"
scripts/vm.sh root bash -c 'systemctl stop thu-http; rm -rf /srv/thu; echo "đã dọn máy chính"' </dev/null
