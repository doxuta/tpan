#!/usr/bin/env bash
# e2e M28 đợt 2 nhóm B, P4: user thường (vai user) tạo site cho tên con của
# domain thuộc người khác (www.<site của họ>, mail.<site của họ>). Chạy bằng
# root trong máy ảo, sau dung-may.sh (khach1 vai user, đã đăng nhập).
#
# Dựng: user khach2 (vai user) tạo thật site example.net qua API. Rồi khach1 xin
# bản xem trước của site add cho www.example.net, mail.example.net. Bản xem
# trước đủ để thấy lỗ: nó là cửa của POST /sites, không tạo gì trên máy.
#
# Tham số:
#   chan (mặc định)  khach1 nhận 404 giống hệt site không tồn tại; ca hợp lệ
#                    (khach2 tạo www.example.net của chính mình, khach1 tạo tên
#                    thuộc tên miền gốc không ai dùng) vẫn 200.
#   lo               bản chưa vá: khach1 nhận 200 kèm Plan (bộ thử bắt được lỗ).
. "$(dirname "$0")/chung2.sh"
MODE="${1:-chan}"
can_may
# P1, P2 chạy trước có thể đã khoá khoá nguồn uid:0 (curl của root nối qua
# loopback): gỡ mọi khoá để đăng nhập của phép thử này không bị 423.
tpan admin unlock >/dev/null 2>&1 || true
VIC=example.net
FREE="shop-$(rnd).example.org"
O="$W/p4.out"
# Site, user của phép thử để lại: phần nào chạy xong cũng đưa máy ảo về sạch.
cleanup() { rm -f "$O"; }
trap cleanup EXIT

step "P4.0. user khach2 (vai user) tạo thật site $VIC qua API"
if [ ! -s /root/m28-khach2.pw ]; then
  tpan user add khach2 --yes --json 2>/dev/null |
    python3 -I -c 'import json,sys; print(json.load(sys.stdin)["password"])' >/root/m28-khach2.pw
  chmod 600 /root/m28-khach2.pw
fi
ok "user add khach2 có mật khẩu (không in)" test -s /root/m28-khach2.pw
ok "khach2 đăng nhập được" login khach2 /root/m28-khach2.pw
call khach2 POST "/sites" "$O" 300 "{\"domain\":\"$VIC\",\"email\":\"admin@example.com\"}"
ok "khach2 gửi tạo site $VIC: 202 (HTTP $HTTP)" test "$HTTP" = 202
for _ in $(seq 1 120); do
  [ "$(tpan site list 2>/dev/null | awk -v d="$VIC" '$1 == d { print $2 }')" = active ] && break
  sleep 2
done
tpan site list 2>/dev/null | grep -E "^($VIC|DOMAIN) " | sed 's/^/  /'
ok "site $VIC của khach2 đã active" bash -c 'tpan site list 2>/dev/null | awk -v d="$1" "\$1 == d && \$2 == \"active\" { f = 1 } END { exit !f }"' _ "$VIC"

# thu <ai> <domain>: bản xem trước của site add, in mã.
thu() { call "$1" POST "/sites?dry_run=1" "$O" 120 "{\"domain\":\"$2\",\"email\":\"admin@example.com\"}"; }

step "P4.1. khach1 xin bản xem trước site add cho tên con của $VIC (của khach2): $MODE"
for sub in www mail; do
  thu khach1 "$sub.$VIC"
  if [ "$MODE" = lo ]; then
    ok "bản chưa vá: khach1 nhận Plan tạo site $sub.$VIC (HTTP $HTTP, bộ thử bắt được lỗ)" test "$HTTP" = 200
  else
    ok "khach1 không tạo được $sub.$VIC: 404 (HTTP $HTTP)" test "$HTTP" = 404
    ok "lời là site_not_found, như site không tồn tại" grep -q '"site_not_found"' "$O"
  fi
done

step "P4.2. ca hợp lệ"
thu khach2 "www.$VIC"
ok "khach2 tạo được www.$VIC của chính mình (bản xem trước 200, HTTP $HTTP)" test "$HTTP" = 200
thu khach1 "$FREE"
ok "khach1 tạo được $FREE (tên miền gốc không ai dùng; HTTP $HTTP)" test "$HTTP" = 200
