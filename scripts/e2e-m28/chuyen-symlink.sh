#!/usr/bin/env bash
# e2e M28, phần B (S2): chuyển web server khi `public` của một site là symlink.
# Chạy bằng root trong máy ảo, sau dung-may.sh, stack nginx.
#
# User của site e.example.com đổi tên public thật thành public.that rồi đặt
# `public` thành symlink tới public.that (một thư mục khác của chính nó). Admin
# chạy `tpan stack switch --to apache --dry-run`. Đòi: thoát khác 0, lời từ
# chối nêu e.example.com hoặc /var/www/e.example.com/public, không có thứ gì
# của Apache được ghi (cây /etc/apache2, /etc/nginx, /etc/php không đổi), nginx
# vẫn active. Sau cùng trả public về như cũ.
. "$(dirname "$0")/chung.sh"
can_may
[ "$WEB" = nginx ] || bail "chuyen-symlink.sh chỉ cho stack nginx"

don() {
  if [ -L "$PUB" ] && [ -d "$PUB.that" ]; then
    as rm -f "$PUB"
    as mv "$PUB.that" "$PUB"
  fi
}
trap don EXIT

# cay: một dấu vân tay của mọi file cấu hình web server (đường dẫn, mtime, cỡ).
cay() {
  find /etc/apache2 /etc/nginx /etc/php -type f -printf '%p %T@ %s\n' 2>/dev/null | sort | sha256sum | cut -c1-16
}

step "B-S2. public của $DOM là symlink tới public.that, tpan stack switch --to apache --dry-run"
ok "public của $DOM là thư mục thật trước khi thử" test -d "$PUB" -a ! -L "$PUB"
as mv "$PUB" "$PUB.that"
as ln -s "$PUB.that" "$PUB"
stat -c '  %n: %F -> %N' "$PUB"
t0="$(cay)"
tpan stack switch --to apache --dry-run >"$W/switch.out" 2>&1
rc=$?
head -c 600 "$W/switch.out" | sed 's/^/  | /'
echo "  mã thoát $rc"
t1="$(cay)"
ok "thoát khác 0 (từ chối)" test "$rc" -ne 0
ok "lời từ chối nêu $DOM hoặc /var/www/$DOM/public" bash -c 'grep -qE "e\.example\.com|/var/www/e\.example\.com/public" "$1"' _ "$W/switch.out"
ok "không có vhost Apache nào của $DOM được ghi" bash -c '! grep -rlq "$1" /etc/apache2/sites-available /etc/apache2/sites-enabled 2>/dev/null' _ "$DOM"
ok "cây cấu hình /etc/apache2, /etc/nginx, /etc/php không đổi ($t0 -> $t1)" test "$t0" = "$t1"
ok "nginx vẫn active" test "$(systemctl is-active nginx)" = active

step "B-S2. trả public về như cũ"
don
ok "public của $DOM lại là thư mục thật" test -d "$PUB" -a ! -L "$PUB"
rm -f "$W/switch.out"
