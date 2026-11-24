#!/usr/bin/env bash
# Dựng máy cho e2e M19 (chạy bằng root trong máy ảo đã có /usr/local/bin/tpan).
# Tham số: apache | nginx.
#
# Kết quả: tpan setup, stack, pebble và `tpan ssl settings` trỏ về pebble, hai user thường
# khach_a và khach_b (mật khẩu ở /root/m19-<tên>.pw, 0600, không in), phiên API của cả
# admin và hai user, /etc/hosts trỏ các tên thử về 127.0.0.1 (trừ $NODNS: cố ý không có).
# Chưa tạo site nào: các site là một phần của khẳng định (ten.sh).
. "$(dirname "$0")/chung.sh"
STACK="${1:-nginx}"

cai_may "$STACK"
dung_pebble

step "hai user thường (vai user) và /etc/hosts"
user_them "$UA" "/root/m19-$UA.pw"
user_them "$UB" "/root/m19-$UB.pw"
hosts_them "$SITE" "$WWW" "$SUBSITE" "$NET" "$VN" "$A_SITE" "$A_ALIAS" "$B_SITE" "$RACE"
ok "$NODNS không phân giải được (cố ý)" bash -c '! getent ahosts "$1" >/dev/null' _ "$NODNS"

step "phiên API: admin, $UA, $UB"
ok "admin đăng nhập" login admin /root/m19-admin.pw
ok "$UA đăng nhập" login "$UA" "/root/m19-$UA.pw"
ok "$UB đăng nhập" login "$UB" "/root/m19-$UB.pw"
call "$UA" GET /session "$W/session.json" 30
ok "phiên của $UA có vai user" pj "$W/session.json" 'd["user"]["role"] == "user"'
echo "  stack: $WEB"
