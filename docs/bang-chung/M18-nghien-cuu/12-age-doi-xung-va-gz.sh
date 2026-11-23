#!/usr/bin/env bash
# M18, đợt 0: hai điểm của bản thiết kế còn dựa vào suy luận, thử cho chắc.
# Chạy trên Mac:
#
#   make m18-nc S=12-age-doi-xung-va-gz
#
# 1. Chế độ khoá đối xứng của age (mật khẩu scrypt, hệ số công 15): file do thư
#    viện ghi có giải mã được bằng lệnh `age` của apt không; đỉnh RAM; file mã
#    với hệ số công cao hơn trần thì bị từ chối; sửa một byte thì lỗi.
# 2. cloud-init có nhận `encoding: gz+b64` trong write_files không (khuôn
#    user-data của mục 3.9 dùng nó cho sites.yaml).
set -euo pipefail
cd "$(dirname "$0")/../../.."
here=docs/bang-chung/M18-nghien-cuu
main="$(tr -d '[:space:]' <.tpan-vm)"
ci="$main-ci"
W="$(mktemp -d)"
trap 'rm -rf "$W"' EXIT
echo "== ngay thu: $(date -u +%Y-%m-%dT%H:%M:%SZ)"

arch="$(scripts/vm.sh exec dpkg --print-architecture </dev/null | tr -d '\r')"
(
  cd "$here/ban-thu-age"
  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" GOFLAGS=-mod=mod GOWORK=off go build -trimpath -ldflags '-s -w' -o "$W/agethu" .
)
scripts/vm.sh push "$W/agethu" /tmp/agethu
scripts/vm.sh root install -m 0755 /tmp/agethu /usr/local/bin/agethu </dev/null

echo
echo "=== 1. age, khoá đối xứng"
scripts/vm.sh root bash -s <<'EOF'
set -u
cd /root && rm -f agep.* && umask 077
# Khoá theo đúng dạng của thiết kế: tpan-key-1- cộng 52 ký tự base32 của 32 byte ngẫu nhiên.
key="tpan-key-1-$(head -c 32 /dev/urandom | base32 | tr -d '=\n' | tr 'A-Z' 'a-z')"
printf '%s\n' "$key" >agep.key
echo "khoá dài ${#key} ký tự (không in), đúng dạng: $(printf '%s' "$key" | grep -Ec '^tpan-key-1-[a-z2-7]{52}$')"
head -c $((50 * 1024 * 1024)) /dev/urandom >agep.plain
want="$(sha256sum agep.plain | cut -d' ' -f1)"
echo "--- mã hoá 50 MiB bằng thư viện, scrypt hệ số công 15"
/usr/bin/time -f "thời gian %es" agethu encp agep.key agep.plain agep.enc
head -c 120 agep.enc | head -3 | cut -c1-60
echo "--- giải mã bằng thư viện"
agethu decp agep.key agep.enc agep.out
[ "$want" = "$(sha256sum agep.out | cut -d' ' -f1)" ] && echo "ĐẠT: thư viện giải mã ra đúng sha256" || echo "KHÔNG ĐẠT"
echo "--- giải mã bằng LỆNH age của apt ($(age --version)), mật khẩu gõ qua một terminal giả"
rm -f agep.out
script -qec "age -d -o agep.out agep.enc" /dev/null <agep.key >/root/agep.script.txt 2>&1; echo "mã thoát của age: $?"
[ -f agep.out ] && [ "$want" = "$(sha256sum agep.out | cut -d' ' -f1)" ] && echo "ĐẠT: lệnh age giải mã ra đúng sha256 $want" || { echo "KHÔNG ĐẠT (output của age:)"; cat /root/agep.script.txt | tr -d '\r' | sed "s/$key/(khoá)/g" | tail -5; }
echo "--- khoá sai"
printf 'tpan-key-1-%s\n' "$(head -c 32 /dev/urandom | base32 | tr -d '=\n' | tr 'A-Z' 'a-z')" >agep.key2
rm -f agep.out
agethu decp agep.key2 agep.enc agep.out; echo "mã thoát: $?"
echo "--- sửa một byte ở giữa bản mã"
cp agep.enc agep.enc2
printf '\377' | dd of=agep.enc2 bs=1 seek=$((25 * 1024 * 1024)) conv=notrunc 2>/dev/null
rm -f agep.out
agethu decp agep.key agep.enc2 agep.out; echo "mã thoát: $?"
# Phép thử "file ghi hệ số công cao hơn trần 15 thì bị từ chối" không làm ở
# đây: lần chạy đầu, `age -p` (hỏi mật khẩu hai lần qua terminal giả) treo vô
# hạn. Trần hệ số công là hành vi của thư viện (ScryptIdentity.SetMaxWorkFactor),
# được phủ bằng unit test của internal/agebox.
rm -f agep.* /root/agep.script.txt
EOF

echo
echo "=== 2. cloud-init: encoding gz+b64"
ud="$W/user-data.yaml"
payload="$W/sites.yaml"
printf 'defaults:\n  admin_email: admin@example.com\nsites:\n  - domain: a.example.com\n    title: "Tiêu đề có dấu: và # và \\"nháy\\""\n' >"$payload"
{
  printf '#cloud-config\nwrite_files:\n  - path: /etc/tpan-ci/sites.yaml\n    permissions: '"'"'0600'"'"'\n    encoding: gz+b64\n    content: '
  gzip -9 -c "$payload" | base64 | tr -d '\n'
  printf '\nruncmd:\n  - [ sh, -c, "echo xong" ]\n'
} >"$ud"
echo "user-data: $(wc -c <"$ud" | tr -d ' ') byte; file gốc: $(wc -c <"$payload" | tr -d ' ') byte, sha256 $(shasum -a 256 "$payload" | cut -d' ' -f1)"
scripts/vm.sh delete "$ci" >/dev/null 2>&1 || true
scripts/vm.sh cloud-init "$ud" "$ci" >/dev/null 2>&1 && echo "tạo máy phụ: xong" || echo "tạo máy phụ: lỗi"
scripts/vm.sh on "$ci" root bash -c 'cloud-init status; ls -l /etc/tpan-ci/sites.yaml; sha256sum /etc/tpan-ci/sites.yaml; cat /etc/tpan-ci/sites.yaml' </dev/null
scripts/vm.sh delete "$ci"
