#!/usr/bin/env bash
# e2e M28, phần D (nâng từ v0.2.0), pha SAU khi thay file chạy bằng bản của cây
# và restart tpan. Chạy bằng root trong máy ảo. Stack Apache.
#
# - Mục .htaccess (file thường) trong bản xem trước giống hệt lúc trước khi nâng
#   (path, old, new, old_exists): hành vi đúng không đổi.
# - tpan health không có dòng [lỗi] mới so với trước khi nâng.
# - Ba site trả 200.
# - Ba mục tiêu T1 giờ BỊ CHẶN (câu trả lời không chứa file đích).
. "$(dirname "$0")/chung.sh"
can_may
trap tra_lai EXIT
[ -f "$W/xt-truoc.json" ] || bail "chưa có $W/xt-truoc.json (nang-truoc.sh chưa chạy)"
login khach1 /root/m28-khach1.pw || bail "khach1 không đăng nhập được"

step "D1. phiên bản tpan đang chạy (bản của cây)"
tpan version

step "D2. mục .htaccess trong bản xem trước giống hệt lúc trước khi nâng"
tra_lai
dry "$W/xt.resp" 60
ok "bản xem trước .htaccess thường HTTP 200, mục giống hệt v0.2.0 (path, old, new, old_exists)" \
  bash -c '[ "$1" = 200 ] && python3 "$2" giong-muc "$3" "$4" "$5"' _ "$HTTP" "$D/phan-tich.py" "$W/xt.resp" "$HT" "$W/xt-truoc.json"

step "D3. tpan health không có [lỗi] mới so với trước khi nâng"
tpan health >"$W/health.full" 2>&1
grep -E '^\[lỗi\]' "$W/health.full" | sort >"$W/health.sau"
moi="$(comm -13 "$W/health.truoc" "$W/health.sau")"
[ -n "$moi" ] && printf '  [lỗi] mới:\n%s\n' "$moi"
echo "  số dòng [lỗi]: trước $(grep -c . "$W/health.truoc"), sau $(grep -c . "$W/health.sau")"
ok "không có dòng [lỗi] mới sau khi nâng" test -z "$moi"

step "D4. ba site trả 200"
for d in "$VICTIM" "$OTHER" "$DOM"; do ok "$d trả 200" web200 "$d"; done

step "D5. ba mục tiêu T1 giờ bị chặn"
bash "$D/t1.sh" chan
rm -f "$W/xt.resp" "$W/health.full"
