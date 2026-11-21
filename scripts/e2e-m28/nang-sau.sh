#!/usr/bin/env bash
# e2e M28, phần D (nâng từ v0.2.0), pha SAU khi thay file chạy bằng bản của cây
# và restart tpan. Chạy bằng root trong máy ảo. Stack Apache.
#
# - Mục .htaccess (file thường) trong bản xem trước giống hệt lúc trước khi nâng
#   (path, old, new, old_exists): hành vi đúng không đổi.
# - tpan health không có dòng [lỗi] mới so với trước khi nâng.
# - Ba site trả 200.
# - Ba mục tiêu T1 giờ BỊ CHẶN (câu trả lời không chứa file đích).
# - Chạy lại site add: các bước dir: bỏ qua, public vẫn 2750 (D6).
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

step "D6. chạy lại site add $DOM sau khi nâng: các bước dir: bỏ qua, public vẫn 2750 (S3)"
tpan site add "$DOM" --owner khach1 --email admin@example.com --yes --json >"$W/add-sau.json" 2>"$W/add-sau.err"
rc=$?
echo "  site add thoát $rc"
ok "site add $DOM chạy lại thoát 0" test "$rc" -eq 0
ok "mọi bước có ID bắt đầu bằng dir: có outcome skipped" python3 -I -c '
import json, sys
d = json.load(open(sys.argv[1]))
st = [s for s in d["report"]["steps"] if s["id"].startswith("dir:")]
for s in st:
    print("  ", s["id"], s["outcome"])
sys.exit(0 if st and all(s["outcome"] == "skipped" for s in st) else 1)' "$W/add-sau.json"
ok "public của $DOM vẫn 2750 ($(stat -c %a "$PUB"))" test "$(stat -c %a "$PUB")" = 2750
rm -f "$W/add-sau.json" "$W/add-sau.err"
