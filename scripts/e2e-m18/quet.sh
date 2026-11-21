#!/usr/bin/env bash
# e2e M18, phần H (và L): đếm số lần một bí mật xuất hiện trong các file hay thư
# mục đưa vào. Chạy TRONG máy ảo bằng root:
#
#   bash /root/e2e-m18/quet.sh <file|thư mục>... < <file bí mật thêm, mỗi dòng một cái>
#
# Bí mật được đọc ngay trong máy ảo, không đi qua dòng lệnh và không được in:
#   - hai khoá truy cập của từng kho thử (/root/s3thu.env, /root/garage.env nếu có);
#   - khoá sao lưu (tpan backup key show --reveal --json);
#   - mỗi dòng của stdin (ví dụ mật khẩu quản trị mà render vừa in).
# Mỗi bí mật đếm riêng bằng grep -a -c -F -e (không dùng -f: một dòng rỗng
# trong file mẫu khớp mọi dòng). Bí mật ngắn hơn 8 ký tự bị bỏ để không khớp
# nhầm. Output: các dòng mau_*=, rồi "dem=<số dòng chứa bí mật> <file>" cho
# từng file, "khong-co <đường dẫn>" cho đường dẫn không có, cuối là "tong=<n>".
set -u

pats=()
add() { [ "${#1}" -ge 8 ] && pats+=("$1"); return 0; }
them=0
while IFS= read -r l || [ -n "$l" ]; do
  add "$l"
  [ "${#l}" -ge 8 ] && them=$((them + 1))
done
kho=0
for envf in /root/s3thu.env /root/garage.env; do
  [ -f "$envf" ] || continue
  # shellcheck disable=SC1090
  ak="$(. "$envf" && printf '%s' "$S3_ACCESS_KEY")"
  # shellcheck disable=SC1090
  sk="$(. "$envf" && printf '%s' "$S3_SECRET_KEY")"
  add "$ak"
  add "$sk"
  kho=$((kho + 2))
done
bk=""
if command -v tpan >/dev/null 2>&1; then
  bk="$(tpan backup key show --reveal --json </dev/null 2>/dev/null | jq -r '.key // empty' 2>/dev/null)"
fi
add "$bk"
echo "mau_kho=$kho mau_khoa_sao_luu=$([ -n "$bk" ] && echo 1 || echo 0) mau_them=$them tong_mau=${#pats[@]}"

tong=0
scan() {
  local f=$1 n=0 c p
  for p in ${pats[@]+"${pats[@]}"}; do
    c="$(grep -a -c -F -e "$p" -- "$f" 2>/dev/null)"
    n=$((n + ${c:-0}))
  done
  echo "dem=$n $f"
  tong=$((tong + n))
}
for t in "$@"; do
  if [ -d "$t" ]; then
    while IFS= read -r -d '' f; do scan "$f"; done < <(find "$t" -type f -print0 | sort -z)
  elif [ -f "$t" ]; then
    scan "$t"
  else
    echo "khong-co $t"
  fi
done
echo "tong=$tong"
