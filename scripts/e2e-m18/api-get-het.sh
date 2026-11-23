#!/usr/bin/env bash
# e2e M18, phần H: đăng nhập admin rồi gọi MỌI route GET của API, cộng
# DELETE /api/v1/backups/target?dry_run=1, lưu từng câu trả lời vào một thư mục
# để quet.sh đếm bí mật. Chạy TRONG máy ảo bằng root:
#
#   bash /root/e2e-m18/api-get-het.sh <thư mục ra> <domain> <user thường> < <file mật khẩu admin>
#
# Danh sách route lấy từ các dòng `pattern: "GET ...` của
# internal/server/routes.go (bản M18, cộng hai route GET mới của mục 3.11);
# chỗ {domain}, {name} thay bằng đối số, {id} của job, sự cố, Plan, phiên AI
# lấy từ chính câu trả lời của route danh sách. Output: một dòng
# "<mã HTTP> <số byte> <đường dẫn>" cho mỗi route tĩnh, một dòng gộp cho các
# route theo id, rồi số trường old/new đã giải base64 của bản xem trước DELETE.
set -u
out=$1
dom=$2
usr=$3
base=https://127.0.0.1:8443
rm -rf "$out"
install -d -m 0700 "$out"
jar="$out/.jar"
(umask 077 && : >"$jar")
# Mật khẩu đọc thẳng từ stdin vào jq (-R: chuỗi thô), không lên dòng lệnh nào.
login="$(jq -Rsc '{name: "admin", password: .}')"
code="$(printf '%s' "$login" | curl -sk -m 60 -o /dev/null -w '%{http_code}' -b "$jar" -c "$jar" \
  -H 'Content-Type: application/json' -H 'Sec-Fetch-Site: same-origin' --data-binary @- "$base/api/v1/login")"
unset login
echo "dang-nhap=$code"

n=0
get() { # get <đường dẫn>: lưu thân vào $out/<số>.json, in mã và cỡ
  local f c
  n=$((n + 1))
  f="$out/$(printf '%04d' "$n").json"
  c="$(curl -sk -m 180 -o "$f" -w '%{http_code}' -b "$jar" -H 'Accept: application/json' "$base$1")"
  LAST="$f"
  CODE="$c"
}
show() { get "$1"; echo "$CODE $(wc -c <"$LAST") $1"; }

for p in /api/v1/health /api/v1/session /api/v1/account/sessions /api/v1/overview /api/v1/machine \
  /api/v1/stack /api/v1/checks /api/v1/sites /api/v1/databases /api/v1/ssl /api/v1/security \
  /api/v1/trash /api/v1/export /api/v1/ai/brain /api/v1/agent /api/v1/php /api/v1/updates \
  /api/v1/cron /api/v1/users /api/v1/packages /api/v1/collations /api/v1/security/isolation /api/v1/self \
  /api/v1/backups /api/v1/backups/target /api/v1/backups/remote \
  "/api/v1/sites/$dom" "/api/v1/sites/$dom/traffic" "/api/v1/sites/$dom/log" "/api/v1/sites/$dom/route" \
  "/api/v1/users/$usr"; do
  show "$p"
done

# Các route theo id: danh sách trước, rồi từng id trong đó.
byid() { # byid <route danh sách> <tiền tố route theo id> <hậu tố...>
  local list=$1 pre=$2 id s codes=""
  shift 2
  show "$list"
  local ids
  ids="$(jq -r '(if type == "array" then . else (.items // .sessions // .incidents // .plans // []) end) | .[]? | .id? // empty' "$LAST" 2>/dev/null)"
  local k=0
  for id in $ids; do
    k=$((k + 1))
    get "$pre/$id"
    codes="$codes $CODE"
    for s in "$@"; do
      get "$pre/$id$s"
      codes="$codes $CODE"
    done
  done
  echo "  $pre/{id}$*: $k id, mã HTTP:$(tr ' ' '\n' <<<"$codes" | grep . | sort | uniq -c | awk '{printf " %s×%s", $2, $1}')"
  # Mẫu route đã phủ, để script trên Mac đối chiếu với routes.go.
  echo "phu-route $pre/{id}"
  for s in "$@"; do echo "phu-route $pre/{id}$s"; done
}
byid "/api/v1/jobs?limit=500" /api/v1/jobs /events
byid /api/v1/incidents /api/v1/incidents
byid /api/v1/plans /api/v1/plans
byid /api/v1/ai/sessions /api/v1/ai/sessions
echo "so-cau-tra-loi-get=$n"

# Bản xem trước của gỡ đích: trường old/new của bước SecretAbsent là base64 của
# nội dung file (mục 9, S1). Giải ra để quét cả dạng rõ.
c="$(curl -sk -m 120 -o "$out/delete-dry.json" -w '%{http_code}' -b "$jar" -c "$jar" -X DELETE \
  -H 'Accept: application/json' -H 'Sec-Fetch-Site: same-origin' "$base/api/v1/backups/target?dry_run=1")"
echo "$c $(wc -c <"$out/delete-dry.json") DELETE /api/v1/backups/target?dry_run=1"
jq -r '[.. | objects | (.old?, .new?) | strings | (try @base64d catch "")] | .[]' "$out/delete-dry.json" \
  >"$out/delete-dry-giai-base64.txt" 2>/dev/null
echo "truong-old-new=$(jq '[.. | objects | (.old?, .new?) | strings] | length' "$out/delete-dry.json" 2>/dev/null)"
rm -f "$jar"
