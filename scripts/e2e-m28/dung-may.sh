#!/usr/bin/env bash
# Dựng máy cho e2e M28 (từ docs/bang-chung/M28-nghien-cuu/n0-dung-may.sh), chạy
# bằng root trong máy ảo đã có /usr/local/bin/tpan. Tham số: apache | nginx.
#
# Kết quả: tpan setup (bỏ qua khi đã có /root/m28-admin.pw, ví dụ install.sh
# của v0.2.0 đã chạy setup), stack, hai site của admin (a.example.com,
# b.example.com), user khach1 vai user và site e.example.com của khach1. Mật
# khẩu ở /root/m28-admin.pw, /root/m28-khach1.pw (0600), không in ra. Bản gốc
# .htaccess của site e (Apache) giữ ở /root/m28-e2e/htaccess.goc để các nhóm
# sau trả lại sau mỗi phép thử.
. "$(dirname "$0")/chung.sh"
STACK="${1:-apache}"

step "tpan version"
tpan version

step "tpan setup"
# Bỏ qua setup khi đã có mật khẩu quản trị, HOẶC khi tpan đã chạy (ví dụ
# install.sh của v0.2.0 ở phần nâng cấp đã setup rồi): setup lại trên máy đã
# cài sẽ hỏng.
if [ ! -s /root/m28-admin.pw ] && [ "$(systemctl is-active tpan)" != active ]; then
  tpan setup --yes >/root/m28-setup.txt 2>&1
  rc=$?
  # Dòng mật khẩu là dòng duy nhất chỉ gồm chữ in và số dài từ 20 ký tự.
  grep -oE '[A-Z0-9]{20,}' /root/m28-setup.txt | head -1 >/root/m28-admin.pw
  grep -vE '[A-Z0-9]{20,}' /root/m28-setup.txt | tail -8
  ok "tpan setup thoát 0, có mật khẩu quản trị (giữ ở /root/m28-admin.pw, không in)" \
    test "$rc" -eq 0 -a -s /root/m28-admin.pw
else
  note "tpan đã setup (bỏ qua); /root/m28-admin.pw $([ -s /root/m28-admin.pw ] && echo "có" || echo "không có, phần này không cần admin")"
fi
ok "tpan.service active" test "$(systemctl is-active tpan)" = active

# Máy ảo vừa về snapshot sạch thì unattended-upgrades của Ubuntu chạy ngay và
# có lần giữ khoá dpkg lâu hơn 5 phút tpan chờ (lần chạy 11:17 ngày 21/11:
# "Unable to acquire the dpkg frontend lock"). Đây là đồ nghề của e2e: chờ
# khoá nhả, tối đa 20 phút, rồi mới cài stack.
step "chờ apt và dpkg của máy rảnh (unattended-upgrades sau khi khởi động)"
# Chỉ xét ai đang giữ khoá của dpkg và apt, và tiến trình apt-get, apt, dpkg:
# tên tiến trình "unattended-upgr" khớp cả unattended-upgrade-shutdown, một
# trình nền lúc nào cũng chạy (lần chạy 11:29 chờ phí vì nó).
LOCKS="/var/lib/dpkg/lock-frontend /var/lib/dpkg/lock /var/lib/apt/lists/lock /var/cache/apt/archives/lock"
for i in $(seq 1 240); do
  # shellcheck disable=SC2086 # LOCKS cố ý tách thành nhiều đường dẫn
  if ! pgrep -x 'apt-get|apt|dpkg' >/dev/null && ! fuser $LOCKS >/dev/null 2>&1; then
    break
  fi
  # shellcheck disable=SC2086
  [ "$i" = 1 ] && echo "  đang chờ: $(fuser $LOCKS 2>&1 | tr '\n' ' ') $(pgrep -a -x 'apt-get|apt|dpkg' | head -3 | tr '\n' ';')"
  sleep 5
done
echo "  apt rảnh sau khoảng $(( (i - 1) * 5 )) giây"

step "tpan stack install --web $STACK --yes"
tpan stack install --web "$STACK" --yes >"$W/stack.txt" 2>&1
rc=$?
tail -4 "$W/stack.txt"
ok "stack install --web $STACK thoát 0" test "$rc" -eq 0

for d in "$VICTIM" "$OTHER"; do
  step "tpan site add $d (admin)"
  # --json: mật khẩu WordPress chỉ nằm trong file 0600, không in.
  tpan site add "$d" --email admin@example.com --yes --json >"$W/site-$d.json" 2>"$W/site-$d.err"
  rc=$?
  tail -3 "$W/site-$d.err"
  ok "site add $d thoát 0" test "$rc" -eq 0
done

step "user khach1 (vai user) và site $DOM của khach1"
if [ ! -s /root/m28-khach1.pw ]; then
  tpan user add khach1 --yes --json 2>"$W/user.err" |
    python3 -c 'import json,sys; print(json.load(sys.stdin)["password"])' >/root/m28-khach1.pw
  ok "user add khach1 thoát 0, có mật khẩu (không in)" test "${PIPESTATUS[0]}" -eq 0 -a -s /root/m28-khach1.pw
fi
tpan site add "$DOM" --owner khach1 --email admin@example.com --yes --json >"$W/site-$DOM.json" 2>"$W/site-$DOM.err"
rc=$?
tail -3 "$W/site-$DOM.err"
ok "site add $DOM --owner khach1 thoát 0" test "$rc" -eq 0

SU="$(stat -c '%U' "/var/www/$DOM")"
if [ -f "$HT" ] && [ ! -L "$HT" ]; then
  cp -a "$HT" "$W/htaccess.goc"
fi

step "hiện trạng"
tpan site list
for d in "$VICTIM" "$OTHER" "$DOM"; do
  stat -c '  %n  %U:%G %a' "/var/www/$d" "/var/www/$d/public"
  stat -c '  %n  %F %U:%G %a %s byte' "/var/www/$d/public/.htaccess" 2>/dev/null || echo "  /var/www/$d/public/.htaccess: không có"
  ok "$d trả 200" web200 "$d"
done
echo "  site $DOM chạy bằng user Linux $SU; stack $WEB"
sysctl fs.protected_symlinks fs.protected_hardlinks
login khach1 /root/m28-khach1.pw >/dev/null
ok "khach1 đăng nhập API: HTTP 204" test "$HTTP" = 204
call khach1 GET /session "$W/session.json" 30
ok "phiên của khach1 có vai user" python3 -c 'import json,sys; u=json.load(open(sys.argv[1]))["user"]; print("  phiên:", u["name"], "vai", u["role"]); sys.exit(u["role"] != "user")' "$W/session.json"
