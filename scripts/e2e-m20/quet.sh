#!/usr/bin/env bash
# ĐỒ NGHỀ TEST của scripts/e2e-m20.sh (T60–T66), chỉ chạy TRONG máy ảo test,
# bằng root.
#
#   quet.sh <file mẫu> <nhãn> <đường dẫn>...
#
# <file mẫu> (0600) có các bí mật e2e đã gặp, mỗi dòng "<loại>:<giá trị>":
#   totp:<base32>   bí mật hai bước    → tìm cả dạng base32 và dạng hex của 20 byte
#   ma:<mã dự phòng có gạch>           → tìm cả dạng có gạch và dạng bỏ gạch
#   token:<token API>
# Thêm khoá niêm phong /etc/tpan/secret.key (32 byte), đọc ngay ở đây, dạng hex
# và base64 (có và không có dấu "="): khoá không bao giờ ra khỏi máy ảo.
#
# Với mỗi loại, đếm số lần xuất hiện (không phân biệt hoa thường, đọc cả file
# nhị phân như tpan.db) trong mọi <đường dẫn> (thư mục thì đi hết cây). In
# "<nhãn>: totp=… totp-hex=… ma=… ma-bo-gach=… token=… khoa-hex=… khoa-base64=… tong=…".
# Không in bí mật nào. File mẫu dựng ra trong một thư mục tạm 0700, xoá khi xong.
set -uo pipefail
umask 077
src=$1 label=$2
shift 2
w=$(mktemp -d)
trap 'rm -rf "$w"' EXIT

sed -n 's/^totp://p' "$src" >"$w/totp"
python3 -I -c '
import base64, binascii, sys
for line in open(sys.argv[1]):
    s = line.strip()
    try:
        print(base64.b32decode(s + "=" * (-len(s) % 8), casefold=True).hex())
    except (binascii.Error, ValueError):
        print("dòng totp không phải base32, bỏ dạng hex của nó", file=sys.stderr)
' "$w/totp" >"$w/totp-hex"
sed -n 's/^ma://p' "$src" >"$w/ma"
tr -d '-' <"$w/ma" >"$w/ma-bo-gach"
sed -n 's/^token://p' "$src" >"$w/token"
: >"$w/khoa-hex"
: >"$w/khoa-base64"
if [ -f /etc/tpan/secret.key ]; then
  od -An -v -tx1 /etc/tpan/secret.key | tr -d ' \n' >"$w/khoa-hex"
  echo >>"$w/khoa-hex"
  b=$(base64 -w0 /etc/tpan/secret.key)
  printf '%s\n%s\n' "$b" "${b%%=*}" >"$w/khoa-base64"
fi

for k in totp totp-hex ma ma-bo-gach token khoa-hex khoa-base64; do
  sed -i '/^$/d' "$w/$k" # dòng rỗng trong grep -F -f khớp mọi thứ
done
[ -s "$w/khoa-hex" ] || echo "(không có /etc/tpan/secret.key: không quét khoá)"

line="$label:"
total=0
for k in totp totp-hex ma ma-bo-gach token khoa-hex khoa-base64; do
  c=0
  if [ -s "$w/$k" ]; then
    c=$(grep -r -a -i -o -F -f "$w/$k" -- "$@" 2>/dev/null | wc -l | tr -d ' ')
  fi
  line="$line $k=$c"
  total=$((total + c))
done
echo "$line tong=$total"
