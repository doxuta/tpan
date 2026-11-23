#!/usr/bin/env bash
# ĐỒ NGHỀ TEST của scripts/e2e-m20.sh (T40), chỉ chạy TRONG máy ảo test.
#
#   bang-token.sh <route giả.tsv> <route thật.tsv> <thư mục token> <thư mục ra>
#
# Hai file route do scripts/e2e-m20/bangroute in (mỗi dòng: METHOD, đường dẫn,
# nhóm token hay "-", luật, mẫu gốc): "giả" có placeholder là giá trị không có
# thật, "thật" có giá trị của máy ảo. Thư mục token có các file header
# "Authorization: Bearer …" (0600), mỗi file một token của admin:
#
#   tok.du.h       đủ mọi phạm vi (mọi nhóm, ghi)
#   tok.doc.h      mọi nhóm, chỉ đọc
#   tok.thieu-a.h  chỉ cron:read       tok.thieu-b.h  chỉ jobs:read
#   tok.het-han.h  đủ mọi phạm vi, expires_at đã bị ghi lùi về quá khứ
#   tok.thu-hoi.h  đủ mọi phạm vi, đã thu hồi
#
# Với MỖI route (giá trị giả, thân {} cho mọi request không phải GET):
#   het-han  → 401 token_expired       thu-hoi → 401 token_revoked
#   route chỉ phiên, token đủ phạm vi  → 403 token_forbidden
#   route có nhóm, token không có nhóm đó (cron:read; nhóm cron thì jobs:read) → 403 token_scope
#   route có nhóm mà ghi, token chỉ đọc → 403 token_scope
# Các request này dừng ở lớp token, không tới handler; lớp token có lỗi thì
# chúng rơi vào 404 hay 400 (giá trị giả, thân rỗng) chứ không đổi máy.
# "Đủ phạm vi" (giá trị thật): mọi route có nhóm mà GET, và đúng hai POST xem
# trước (?dry_run=1) đã soát là đi qua apiDryRun: POST /sites, POST
# /panel/domain → mã không phải 401, 403.
#
# In một dòng cho mỗi request sai mong đợi ("SAI …"), rồi bảng đếm theo trạng
# thái và dòng cuối "TỔNG: <n> request, sai=<k>". Thân trả lời giữ ở <thư mục
# ra>/body (cho phép quét bí mật T64). -k: gọi vòng trong chính máy ảo.
set -uo pipefail
umask 077
fake=$1 real=$2 tok=$3 out=$4
base=https://127.0.0.1:8443
mkdir -p "$out/body"
printf '{}' >"$out/rong.json"
jq -nc '{domain: "token-du.example.com", email: "admin@example.com"}' >"$out/site.json"
jq -nc '{domain: "panel.m20.test"}' >"$out/panel.json"

n=0 bad=0
: >"$out/ket-qua.tsv"
# req <trạng thái> <token> <METHOD> <đường dẫn> <thân hay ""> → đặt CODE, EC
req() {
  local st=$1 t=$2 m=$3 p=$4 body=$5 args
  n=$((n + 1))
  args=(-sk -m 8 -o "$out/body/$n.json" -w '%{http_code}' -X "$m" -H @"$tok/tok.$t.h" -H 'Accept: application/json')
  [ -n "$body" ] && args+=(-H 'Content-Type: application/json' --data-binary "@$body")
  CODE=$(curl "${args[@]}" "$base$p" 2>/dev/null)
  EC=$(jq -r '.error.code // empty' "$out/body/$n.json" 2>/dev/null)
  printf '%s\t%s\t%s\t%s\t%s\n' "$st" "$m" "$p" "$CODE" "${EC:--}" >>"$out/ket-qua.tsv"
}
# want <trạng thái> <METHOD> <đường dẫn> <mã HTTP> <error.code>
want() {
  if [ "$CODE" != "$4" ] || [ "$EC" != "$5" ]; then
    bad=$((bad + 1))
    printf 'SAI %s: %s %s -> %s %s (mong %s %s)\n' "$1" "$2" "$3" "$CODE" "${EC:--}" "$4" "$5"
  fi
}

routes=0
while IFS=$'\t' read -r m p g _ _; do
  [ -n "$m" ] || continue
  routes=$((routes + 1))
  body=""
  [ "$m" != GET ] && [ "$m" != HEAD ] && body="$out/rong.json"
  req het-han het-han "$m" "$p" "$body"
  want het-han "$m" "$p" 401 token_expired
  req thu-hoi thu-hoi "$m" "$p" "$body"
  want thu-hoi "$m" "$p" 401 token_revoked
  if [ "$g" = - ]; then
    req chi-phien du "$m" "$p" "$body"
    want chi-phien "$m" "$p" 403 token_forbidden
  else
    t=thieu-a
    [ "$g" = cron ] && t=thieu-b
    req thieu "$t" "$m" "$p" "$body"
    want thieu "$m" "$p" 403 token_scope
    if [ "$m" != GET ] && [ "$m" != HEAD ]; then
      req doc-tren-ghi doc "$m" "$p" "$body"
      want doc-tren-ghi "$m" "$p" 403 token_scope
    fi
  fi
done <"$fake"

du=0
while IFS=$'\t' read -r m p g _ pat; do
  [ -n "$m" ] && [ "$g" != - ] || continue
  case "$m $pat" in
  "GET "*) req du du GET "$p" "" ;;
  "POST POST /api/v1/sites") req du du POST "$p?dry_run=1" "$out/site.json" ;;
  "POST POST /api/v1/panel/domain") req du du POST "$p?dry_run=1" "$out/panel.json" ;;
  *) continue ;;
  esac
  du=$((du + 1))
  if [ "$CODE" = 401 ] || [ "$CODE" = 403 ] || [ "$CODE" = 000 ]; then
    bad=$((bad + 1))
    printf 'SAI du: %s %s -> %s %s (mong không phải 401, 403)\n' "$m" "$p" "$CODE" "${EC:--}"
  fi
done <"$real"

echo "số route (không công khai): $routes; request đủ phạm vi (GET và POST xem trước): $du"
echo "đếm theo trạng thái (trạng thái, mã HTTP, error.code, số request):"
cut -f1,4,5 "$out/ket-qua.tsv" | sort | uniq -c | awk '{printf "  %-14s %s %-18s %s\n", $2, $3, $4, $1}'
echo "TỔNG: $n request, sai=$bad"
