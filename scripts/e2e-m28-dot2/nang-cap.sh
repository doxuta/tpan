#!/usr/bin/env bash
# e2e M28 đợt 2, phần nâng cấp. Chạy bằng root trong máy ảo, stack Apache, sau
# dung-may.sh trên bản v0.2.0 (cài bằng install.sh của gói; a.example.com và
# b.example.com là hai site WordPress của admin, e.example.com của khach1).
#
#   truoc   (trên v0.2.0) đặt mật khẩu biết trước cho user admin WordPress của
#           hai site, đăng nhập thử; các phép F4, H5, DBCONN phải LỘ (bộ thử bắt
#           được lỗ trên bản cũ); lưu dòng [lỗi] của health làm mốc.
#   sau     (sau khi thay file chạy bằng bản của cây và restart tpan) ghi lại
#           tpan health nói gì, chạy tpan security harden --yes, rồi: cả hai site
#           200, đăng nhập WordPress được, F4, H5, DBCONN chuyển sang chặn.
#
# Bản thiết kế đợt 2 chưa qua soát: các từ khoá để nhận dòng health nằm ở đây.
RE_HEALTH_VHOST='[Vv]host'
RE_HEALTH_STATUS='mod_status|status'
RE_HEALTH_DBLIM='MAX_USER_CONNECTIONS|user_limit|kết nối'
WP_SITES=("a.example.com" "b.example.com")
WP_BIN=/usr/local/bin/wp
. "$(dirname "$0")/chung2.sh"
PHA="${1:-}"
can_may
[ "$WEB" = apache ] || bail "phần nâng cấp chạy trên stack Apache"

# wp_user <domain> <lệnh con của wp...>: chạy wp dưới user Linux của site (không --allow-root).
wp_user() {
  local d=$1 su
  shift
  su="$(stat -c '%U' "/var/www/$d")"
  (cd / && runuser -u "$su" -- env HOME=/tmp "$WP_BIN" --path="/var/www/$d/public" "$@")
}

# wp_dat_mk <domain>: đặt mật khẩu biết trước cho user quản trị đầu tiên của
# WordPress (wp user update, dưới user của site). Tên đăng nhập vào
# $W/wp-<domain>.log, mật khẩu vào $W/wp-<domain>.pw (0600, không in).
wp_dat_mk() {
  local d=$1 id login
  id="$(wp_user "$d" user list --role=administrator --field=ID 2>"$W/wp.err" | head -1)"
  login="$(wp_user "$d" user get "$id" --field=user_login 2>>"$W/wp.err" | head -1)"
  if [ -z "$id" ] || [ -z "$login" ]; then
    echo "  $d: không tìm được user quản trị WordPress"
    tail -3 "$W/wp.err"
    return 1
  fi
  printf '%s' "$login" >"$W/wp-$d.log"
  printf '%s' "m28$(rnd)$(rnd)" >"$W/wp-$d.pw"
  wp_user "$d" user update "$id" --user_pass="$(cat "$W/wp-$d.pw")" --skip-email >/dev/null 2>>"$W/wp.err" || {
    tail -3 "$W/wp.err"
    return 1
  }
  echo "  $d: đặt mật khẩu biết trước cho user quản trị (ID $id, tên đăng nhập $login; mật khẩu không in)"
}

# wp_dang_nhap <domain>: POST wp-login.php; cookie wordpress_logged_in_ có trong jar.
wp_dang_nhap() {
  local d=$1 c rc=1
  local jar="$W/wp-$d.jar"
  : >"$jar"
  c="$(curl -s -m 60 -o /dev/null -w '%{http_code}' -c "$jar" -b 'wordpress_test_cookie=WP Cookie check' \
    --resolve "$d:80:127.0.0.1" \
    --data-urlencode "log@$W/wp-$d.log" --data-urlencode "pwd@$W/wp-$d.pw" \
    --data-urlencode 'wp-submit=Log In' --data-urlencode 'testcookie=1' "http://$d/wp-login.php")"
  grep -q 'wordpress_logged_in_' "$jar" && rc=0
  echo "  $d: POST wp-login.php: HTTP $c, cookie wordpress_logged_in_ $([ "$rc" = 0 ] && echo có || echo không có)"
  rm -f "$jar"
  return "$rc"
}

# loi_health <file>: dòng [lỗi] và [cảnh báo] đã sắp xếp.
loi_health() { grep -E '^\[(lỗi|cảnh báo)\]' "$1" | sort; }

trap 'rm -f "$W"/wp-*.jar "$W/wp.err"' EXIT

case "$PHA" in
truoc)
  step "N0. bản đang chạy (v0.2.0)"
  tpan version

  step "N1. v0.2.0: hai site WordPress của admin trả 200, đặt mật khẩu biết trước cho user quản trị"
  for d in "${WP_SITES[@]}"; do
    ok "$d trả 200" web200 "$d"
    ok "$d: đặt được mật khẩu cho user quản trị WordPress" wp_dat_mk "$d"
  done

  step "N2. v0.2.0: ghi lại tpan health (dòng [lỗi], [cảnh báo] làm mốc)"
  tpan health </dev/null >"$W/health.full" 2>&1
  loi_health "$W/health.full" >"$W/health.truoc"
  echo "  số dòng [lỗi] hay [cảnh báo] của v0.2.0: $(grep -c . "$W/health.truoc")"
  sed 's/^/  | /' "$W/health.truoc"
  rm -f "$W/health.full"

  step "N3. v0.2.0: F4, H5, DBCONN phải LỘ (bộ thử bắt được lỗ trên bản cũ)"
  bash "$D/f4.sh" lo
  bash "$D/h5.sh" lo
  bash "$D/dbconn.sh" lo
  sleep 3

  step "N4. v0.2.0: sau các phép thử, hai site còn 200 và đăng nhập WordPress được"
  for d in "${WP_SITES[@]}"; do
    ok "$d trả 200" web200 "$d"
    ok "$d: đăng nhập WordPress được (cookie wordpress_logged_in_)" wp_dang_nhap "$d"
  done
  ;;

sau)
  [ -s "$W/health.truoc" ] || bail "chưa có $W/health.truoc (pha truoc chưa chạy)"

  step "N5. bản đang chạy (bản của cây)"
  tpan version

  step "N6. ngay sau khi nâng, trước harden: tpan health nói gì (ghi lại, không khẳng định)"
  tpan health </dev/null >"$W/health.full" 2>&1
  loi_health "$W/health.full" >"$W/health.giua"
  echo "  số dòng [lỗi] hay [cảnh báo]: trước khi nâng $(grep -c . "$W/health.truoc"), sau khi nâng $(grep -c . "$W/health.giua")"
  echo "  dòng mới so với trước khi nâng:"
  comm -13 "$W/health.truoc" "$W/health.giua" | cut -c1-300 | sed 's/^/  | /'
  note "nhắc tới vhost: $(comm -13 "$W/health.truoc" "$W/health.giua" | grep -cE "$RE_HEALTH_VHOST") dòng; mod_status: $(comm -13 "$W/health.truoc" "$W/health.giua" | grep -cE "$RE_HEALTH_STATUS") dòng; trần kết nối DB: $(comm -13 "$W/health.truoc" "$W/health.giua" | grep -cE "$RE_HEALTH_DBLIM") dòng (bản thiết kế mục 6 kỳ vọng mỗi thứ có một dòng)"
  rm -f "$W/health.full"
  # Khẳng định theo ID phép kiểm (soát cuối D7): mỗi thứ của mục 6 có một phép
  # chưa đạt trên máy vừa nâng (web.mod_status chỉ có trên Apache).
  tpan health --json </dev/null >"$W/health.json" 2>/dev/null
  for id in web.docroot_guard web.mod_status db.user_limit; do
    ok "N6: health có phép $id chưa đạt trên máy vừa nâng" python3 -I -c '
import json, sys
d = json.load(open(sys.argv[1]))
def walk(x):
    if isinstance(x, dict):
        yield x
        for v in x.values(): yield from walk(v)
    elif isinstance(x, list):
        for v in x: yield from walk(v)
bad = [o for o in walk(d) if str(o.get("id", "")).startswith(sys.argv[2]) and str(o.get("status", "")).lower() not in ("pass", "ok", "đạt")]
sys.exit(0 if bad else 1)' "$W/health.json" "$id"
  done
  rm -f "$W/health.json"

  # Ca D2 của soát cuối: K1 đổi quyền neo của site mình thành 2751 (phép Isolated
  # của site khác với nó sẽ Fail). Harden vẫn phải xong và giữ bản vá cho cả máy.
  step "N6b. K1 (user của $DOM) đổi neo của site mình thành 2751 trước harden"
  runuser -u "$SU" -- chmod 2751 "/var/www/$DOM"
  stat -c '  %n %a' "/var/www/$DOM"

  step "N7. tpan security harden --yes"
  tpan security harden --yes </dev/null >"$W/harden.txt" 2>&1
  rc=$?
  tail -15 "$W/harden.txt" | cut -c1-300 | sed 's/^/  | /'
  rm -f "$W/harden.txt"
  ok "security harden --yes thoát 0 dù neo của $DOM bị K1 đổi thành 2751 (soát cuối D2)" test "$rc" -eq 0
  runuser -u "$SU" -- chmod 2750 "/var/www/$DOM"
  sleep 3

  step "N8. sau harden: tpan health so với trước khi nâng"
  tpan health </dev/null >"$W/health.full" 2>&1
  loi_health "$W/health.full" >"$W/health.sau"
  moi="$(comm -13 "$W/health.truoc" "$W/health.sau")"
  [ -n "$moi" ] && printf '  dòng mới:\n%s\n' "$moi" | cut -c1-300
  ok "sau harden không có dòng [lỗi] hay [cảnh báo] mới so với trước khi nâng" test -z "$moi"
  rm -f "$W/health.full"

  step "N9. sau harden: hai site WordPress của admin trả 200 và đăng nhập được"
  for d in "${WP_SITES[@]}"; do
    ok "$d trả 200" web200 "$d"
    ok "$d: đăng nhập WordPress được (cookie wordpress_logged_in_)" wp_dang_nhap "$d"
  done
  ok "$DOM trả 200" web200 "$DOM"

  step "N10. sau harden: F4, H5, DBCONN chuyển sang chặn"
  bash "$D/f4.sh" chan
  bash "$D/h5.sh" chan
  bash "$D/dbconn.sh" chan

  step "N11. sau các phép thử: hai site còn 200 và đăng nhập được"
  sleep 2
  for d in "${WP_SITES[@]}"; do
    ok "$d trả 200" web200 "$d"
    ok "$d: đăng nhập WordPress được" wp_dang_nhap "$d"
  done
  ;;

*)
  bail "dùng: nang-cap.sh truoc|sau"
  ;;
esac
