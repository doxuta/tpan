#!/usr/bin/env bash
# e2e M18: một request tới panel ngay trong máy ảo (https://127.0.0.1:8443,
# chứng chỉ tự ký nên -k). Chạy TRONG máy ảo bằng root:
#
#   bash /root/e2e-m18/api.sh <phiên> <METHOD> <đường dẫn> < <thân JSON hoặc rỗng>
#
# Thân đọc từ stdin (mật khẩu đăng nhập không lên dòng lệnh); cookie của phiên
# nằm ở /root/e2e-m18/jar.<phiên> (0600), không bao giờ in. Header
# Sec-Fetch-Site: same-origin như trình duyệt gửi, để lớp chống CSRF của panel
# cho qua request có thay đổi. Output: dòng "__HTTP <mã>" rồi thân trả về.
set -u
d=/root/e2e-m18
jar="$d/jar.$1"
req="$d/req.json"
resp="$d/resp.body"
(umask 077 && : >>"$jar" && cat >"$req" && : >"$resp")
args=(-sk -m 300 -o "$resp" -w '%{http_code}' -b "$jar" -c "$jar" -X "$2"
  -H 'Accept: application/json' -H 'Sec-Fetch-Site: same-origin')
[ -s "$req" ] && args+=(-H 'Content-Type: application/json' --data-binary "@$req")
code="$(curl "${args[@]}" "https://127.0.0.1:8443$3")"
rm -f "$req"
printf '__HTTP %s\n' "$code"
cat "$resp"
