#!/usr/bin/env bash
# M18, đợt 0: thử thư viện mã hoá age cho bản sao lưu panel. Chạy trên Mac:
#
#   make m18-nc S=11-age-thu
#
# 1. Đo phần file chạy tpan tăng thêm khi nhúng filippo.io/age (so chương
#    trình nền có sẵn net/http và x/crypto/argon2, như tpan, với chính nó cộng age).
# 2. Build bản thử ban-thu-age cho máy ảo, rồi trong máy ảo: sinh khoá, mã hoá
#    theo dòng một file 300 MiB (đo RAM), giải mã bằng LỆNH `age` của apt
#    (một chương trình khác hẳn, bản 1.1.1) và so sha256; sửa một byte của bản
#    mã thì giải mã phải lỗi.
set -euo pipefail
cd "$(dirname "$0")/../../.."
here=docs/bang-chung/M18-nghien-cuu
W="$(mktemp -d)"
trap 'rm -rf "$W"' EXIT
echo "== ngay thu: $(date -u +%Y-%m-%dT%H:%M:%SZ), $(go version)"

mkdir -p "$W/nen" "$W/coage"
cat >"$W/nen/main.go" <<'EOF'
package main

import (
	"fmt"
	"net/http"
	"os"

	"golang.org/x/crypto/argon2"
)

func main() {
	fmt.Println(argon2.IDKey([]byte("a"), []byte("saltsalt"), 1, 64, 1, 32))
	_, _ = http.Get(os.Args[1])
}
EOF
sed 's|"golang.org/x/crypto/argon2"|"golang.org/x/crypto/argon2"\n\t"filippo.io/age"|; s|_, _ = http.Get(os.Args\[1\])|_, _ = http.Get(os.Args[1])\n\tid, _ := age.GenerateX25519Identity()\n\tw, _ := age.Encrypt(os.Stdout, id.Recipient())\n\t_ = w.Close()\n\t_, _ = age.Decrypt(os.Stdin, id)|' "$W/nen/main.go" >"$W/coage/main.go"
for n in nen coage; do
  (cd "$W/$n" && GOFLAGS=-mod=mod GOWORK=off go mod init "thu/$n" >/dev/null 2>&1 && GOFLAGS=-mod=mod GOWORK=off go mod tidy >/dev/null 2>&1 &&
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOFLAGS=-mod=mod GOWORK=off go build -trimpath -ldflags '-s -w' -o "$W/$n.bin" . &&
    echo "$n: $(wc -c <"$W/$n.bin" | tr -d ' ') byte, $(GOFLAGS=-mod=mod GOWORK=off go list -deps . | wc -l | tr -d ' ') goi")
done
echo "age thêm: $(($(wc -c <"$W/coage.bin") - $(wc -c <"$W/nen.bin"))) byte"
(cd "$W/coage" && GOFLAGS=-mod=mod GOWORK=off go list -deps . | grep -E 'filippo|c2sp|x/crypto' | sed 's/^/   goi: /')

arch="$(scripts/vm.sh exec dpkg --print-architecture </dev/null | tr -d '\r')"
(
  cd "$here/ban-thu-age"
  GOFLAGS=-mod=mod GOWORK=off go mod tidy >/dev/null 2>&1
  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" GOFLAGS=-mod=mod GOWORK=off go build -trimpath -ldflags '-s -w' -o "$W/agethu" .
  grep -E 'filippo.io/age ' go.mod
)
scripts/vm.sh push "$W/agethu" /tmp/agethu
scripts/vm.sh root install -m 0755 /tmp/agethu /usr/local/bin/agethu </dev/null

scripts/vm.sh root bash -s <<'EOF'
set -u
apt-get install -y -qq age >/dev/null 2>&1
echo "lệnh age của apt: $(age --version)"
cd /root && rm -f age-thu.* && umask 077
agethu keygen >age-thu.keys 2>/dev/null
sed -n 1p age-thu.keys >age-thu.id
rcpt="$(sed -n 2p age-thu.keys)"
echo "khoá công khai dài ${#rcpt} ký tự, bắt đầu bằng ${rcpt:0:4}; khoá bí mật dài $(wc -c <age-thu.id) byte (không in)"
head -c $((300 * 1024 * 1024)) /dev/urandom >age-thu.plain
want="$(sha256sum age-thu.plain | cut -d' ' -f1)"
echo "--- mã hoá 300 MiB bằng thư viện (agethu enc)"
/usr/bin/time -f "thời gian %es" agethu enc "$rcpt" age-thu.plain age-thu.enc
ls -l age-thu.plain age-thu.enc | awk '{print $5, $9}'
echo "--- giải mã bằng LỆNH age của apt (không phải mã của ta)"
/usr/bin/time -f "thời gian %es, đỉnh RAM %MkB" age -d -i age-thu.id -o age-thu.out age-thu.enc
got="$(sha256sum age-thu.out | cut -d' ' -f1)"
[ "$want" = "$got" ] && echo "ĐẠT: lệnh age giải mã ra đúng sha256 $got" || echo "KHÔNG ĐẠT: $want != $got"
echo "--- giải mã bằng thư viện (agethu dec)"
rm -f age-thu.out
agethu dec age-thu.id age-thu.enc age-thu.out
[ "$want" = "$(sha256sum age-thu.out | cut -d' ' -f1)" ] && echo "ĐẠT: thư viện giải mã ra đúng sha256" || echo "KHÔNG ĐẠT"
echo "--- sửa một byte ở giữa bản mã: giải mã phải lỗi, không ra dữ liệu sai"
printf '\377' | dd of=age-thu.enc bs=1 seek=$((150 * 1024 * 1024)) conv=notrunc 2>/dev/null
rm -f age-thu.out
agethu dec age-thu.id age-thu.enc age-thu.out; echo "mã thoát của agethu dec: $?"
echo "--- khoá sai: giải mã phải lỗi"
agethu keygen 2>/dev/null | sed -n 1p >age-thu.id2
rm -f age-thu.out
agethu dec age-thu.id2 age-thu.enc age-thu.out; echo "mã thoát của agethu dec: $?"
rm -f age-thu.*
EOF
