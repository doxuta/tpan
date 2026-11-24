#!/usr/bin/env bash
# e2e M19, phần nâng cấp (E60, E61, E62, S7): bản NGAY TRƯỚC M19 lên bản của cây. Chạy bằng
# root trong máy ảo. Hai pha, giữa hai pha driver thay file chạy bằng bản của cây và restart:
#
#   truoc <web>  (trên bản trước M19) dựng máy, pebble, site $SITE (SSL) và old2.example.com,
#                ghi sha256 hai vhost và dòng [lỗi]/[cảnh báo] của health làm mốc.
#   sau <web>    (trên bản của cây) E60: site_names có đúng hai dòng primary, sha256 hai vhost
#                không đổi, health không có dòng mới; E61: sửa lệch vhost rồi `tpan health
#                --fix` ghi lại, sha256 vẫn bằng bản trước (output vàng, B5); E62: `site add`
#                chạy lại trên site là tên miền gốc không thêm tên phụ nào ($SITE là tên miền
#                gốc: nếu lỗi, lần chạy lại sẽ lộ ra bằng việc thêm www.$SITE).
. "$(dirname "$0")/chung.sh"
PHA="${1:-}"
STACK="${2:-apache}"
OLD2=old2.example.com
S1=$SITE
[ "$STACK" = apache ] && WEB=apache || WEB=nginx
V1="$(vhost_of "$S1")"
V2="$(vhost_of "$OLD2")"

loi_health() { grep -E '^\[(lỗi|cảnh báo)\]' "$1" | sort; }

case "$PHA" in
truoc)
  step "N0. bản đang chạy (trước M19)"
  tpan version
  cai_may "$STACK"
  dung_pebble
  hosts_them "$S1" "$OLD2"

  step "N1. trên bản trước M19: site $S1 (SSL bằng pebble) và $OLD2"
  site_add_cli "$S1"
  ok "site add $S1 thoát 0" test "$RC" -eq 0
  site_add_cli "$OLD2"
  ok "site add $OLD2 thoát 0" test "$RC" -eq 0
  tp ssl issue "$S1" --yes
  ok "ssl issue $S1 thoát 0" test "$RC" -eq 0
  # Lệnh con lạ của bản cũ chỉ in trợ giúp và thoát 0, nên đo bằng DB: bản
  # trước M19 chưa có migration 1900, tức chưa có bảng site_names.
  ok "bản trước M19 chưa có bảng site_names (xác nhận đang đo đúng bản cũ)" test "$(sqlq "SELECT COUNT(*) FROM sqlite_master WHERE name = 'site_names'")" = 0

  step "N2. mốc: sha256 hai vhost và health"
  sha256sum "$V1" "$V2" | tee "$W/nc.sha"
  ok "có sha256 của cả hai vhost" test "$(wc -l <"$W/nc.sha")" -eq 2
  tpan health </dev/null >"$W/nc.health.full" 2>&1
  loi_health "$W/nc.health.full" >"$W/nc.health.truoc"
  echo "  số dòng [lỗi] hay [cảnh báo] trên bản trước M19: $(grep -c . "$W/nc.health.truoc")"
  sed 's/^/  | /' "$W/nc.health.truoc"
  ;;

sau)
  [ -s "$W/nc.sha" ] || bail "chưa có $W/nc.sha (pha truoc chưa chạy)"

  step "N3. bản đang chạy (bản của cây)"
  tpan version

  step "E60. sau nâng: site_names chỉ có hai dòng primary; vhost không đổi; health không có dòng mới"
  ok "E60: site_names có đúng 2 dòng" test "$(sqlq "SELECT COUNT(*) FROM site_names")" = 2
  ok "E60: cả hai dòng là primary, active" test "$(sqlq "SELECT COUNT(*) FROM site_names WHERE kind = 'primary' AND state = 'active'")" = 2
  ok "E60: tên là $S1 và $OLD2" test "$(sqlq "SELECT group_concat(name, ' ') FROM (SELECT name FROM site_names ORDER BY name)")" = "$S1 $OLD2"
  alias_list "$S1"
  ok "E60: site alias list $S1: chỉ tên chính" pj "$W/names.json" "len(d) == 1 and d[0]['kind'] == 'primary'"
  ok "E60: sha256 hai vhost không đổi so với bản trước M19" sha256sum -c "$W/nc.sha"
  tpan health </dev/null >"$W/nc.health.full" 2>&1
  loi_health "$W/nc.health.full" >"$W/nc.health.sau"
  moi="$(comm -13 "$W/nc.health.truoc" "$W/nc.health.sau")"
  [ -n "$moi" ] && printf '  dòng mới:\n%s\n' "$moi" | cut -c1-300
  ok "E60: tpan health không có dòng [lỗi] hay [cảnh báo] mới so với trước khi nâng" test -z "$moi"
  hsreq "$S1" /readme.html
  ok "E60: https://$S1/ trả 200" test "$HC" = 200
  hreq "$OLD2" /readme.html
  ok "E60: http://$OLD2/ trả 200" test "$HC" = 200

  step "E61. sửa lệch vhost (bỏ dòng đầu của tpan), tpan health --fix ghi lại, sha256 vẫn bằng bản trước"
  sed -i 1d "$V1" "$V2"
  ok "E61: sau khi bỏ dòng đầu, sha256 khác bản trước (đã lệch thật)" bash -c '! sha256sum -c "$1" >/dev/null 2>&1' _ "$W/nc.sha"
  tpan health --json >"$W/health.json" 2>/dev/null </dev/null
  echo "  web.vhost:$S1: $(hs "$W/health.json" "web.vhost:$S1"); web.vhost:$OLD2: $(hs "$W/health.json" "web.vhost:$OLD2")"
  ok "E61: health thấy hai vhost lệch (web.vhost fail)" bash -c '[[ "$1" == *fail* ]] && [[ "$2" == *fail* ]]' _ "$(hs "$W/health.json" "web.vhost:$S1")" "$(hs "$W/health.json" "web.vhost:$OLD2")"
  tp health --fix --yes
  ok "E61: health --fix thoát 0" test "$RC" -eq 0
  cho_roi
  sha256sum "$V1" "$V2"
  ok "E61: sha256 hai vhost sau khi ghi lại bằng bản M19 bằng bản trước M19 (output vàng, B5)" sha256sum -c "$W/nc.sha"
  hsreq "$S1" /readme.html
  ok "E61: https://$S1/ vẫn 200" test "$HC" = 200

  step "E62. site add chạy lại trên site cũ (tên miền gốc): không có tên phụ mới"
  site_add_cli "$S1"
  ok "E62: site add $S1 chạy lại thoát 0" test "$RC" -eq 0
  alias_list "$S1"
  ok "E62: vẫn chỉ tên chính (không có www.$S1)" pj "$W/names.json" "len(d) == 1 and d[0]['kind'] == 'primary'"
  ok "E62: site_names vẫn đúng 2 dòng" test "$(sqlq "SELECT COUNT(*) FROM site_names")" = 2
  ok "E62: sha256 hai vhost vẫn như bản trước M19" sha256sum -c "$W/nc.sha"
  ;;

*)
  bail "dùng: nang-cap.sh truoc|sau <apache|nginx>"
  ;;
esac
