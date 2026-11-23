#!/usr/bin/env bash
# ĐỒ NGHỀ TEST của scripts/e2e-m20.sh (T16, T17), chỉ chạy TRONG máy ảo test
# (user ubuntu).
#
#   khoa-ip.sh <địa chỉ panel> <tên user> <file mật khẩu> <thư mục làm việc>
#
# Gọi panel từ CHÍNH máy ảo tới <địa chỉ panel> (e2e truyền https://<IP máy
# ảo>:8443: IP nguồn là IP của máy ảo, khác IP của Mac và khác 127.0.0.1), nên
# khoá IP ở đây không khoá đường mà phần còn lại của e2e dùng.
#
# Kịch bản: đăng nhập bằng mật khẩu (user đã bật hai bước) → có phiếu; gửi 5 mã
# sai trên cùng phiếu (file <thư mục>/ma-sai.json do ma.sh sai ghi); hỏi
# /session bằng cookie phiếu; đăng nhập lại bằng đúng mật khẩu. Mỗi request in
# một dòng "<nhãn>: HTTP <mã> <error.code> [Retry-After]" để script trên Mac
# khẳng định. Không in mật khẩu, mã, cookie.
#
# -k: chứng chỉ tự ký của panel không có IP máy ảo trong SAN; đây là gọi vòng
# trong chính máy ảo test, không qua mạng nào khác.
set -uo pipefail
umask 077
base=$1 user=$2 pwfile=$3 dir=$4
jar="$dir/jar.khoa"
: >"$jar"

call() { # call <nhãn> <METHOD> <đường dẫn> [file thân]
  local label=$1 m=$2 p=$3 body=${4-} args code ec ra
  args=(-sk -m 20 -b "$jar" -c "$jar" -X "$m" -o "$dir/khoa.body" -D "$dir/khoa.hdr" -w '%{http_code}' -H 'Accept: application/json')
  [ -n "$body" ] && args+=(-H 'Content-Type: application/json' --data-binary "@$body")
  : >"$dir/khoa.body"
  code=$(curl "${args[@]}" "$base$p")
  ec=$(jq -r '.error.code // empty' "$dir/khoa.body" 2>/dev/null)
  ra=$(tr -d '\r' <"$dir/khoa.hdr" | sed -n 's/^[Rr]etry-[Aa]fter: *//p' | head -1)
  printf '%s: HTTP %s %s%s\n' "$label" "$code" "${ec:--}" "${ra:+ (Retry-After $ra)}"
}

jq -nc --arg n "$user" --rawfile p "$pwfile" '{name: $n, password: $p}' >"$dir/khoa.login.json"
call "đăng nhập bằng mật khẩu" POST /api/v1/login "$dir/khoa.login.json"
printf 'thân: %s\n' "$(jq -c '{mfa_required}' "$dir/khoa.body" 2>/dev/null)"
printf 'cookie phiếu trong jar: %s\n' "$(grep -c 'tpan-2fa' "$jar")"
for i in 1 2 3 4 5; do
  call "mã sai lần $i" POST /api/v1/login/2fa "$dir/ma-sai.json"
done
call "GET /session bằng cookie phiếu" GET /api/v1/session
printf 'thân: %s\n' "$(jq -c '{logged_in, mfa_pending}' "$dir/khoa.body" 2>/dev/null)"
call "đăng nhập lại bằng ĐÚNG mật khẩu từ IP đang khoá" POST /api/v1/login "$dir/khoa.login.json"
rm -f "$dir/khoa.login.json" "$jar"
