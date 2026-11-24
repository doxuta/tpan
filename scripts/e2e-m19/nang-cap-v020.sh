#!/usr/bin/env bash
# e2e M19, phần nâng cấp TỪ v0.2.0 (gói phát hành): luật 2 của agent-dev/LO-TRINH.md và bất
# biến 5 của agent-dev/TU-CHAY-M19.md. Chạy bằng root trong máy ảo. Hai pha, giữa hai pha
# driver (scripts/e2e-m19.sh, phần nang-cap-v020) thay file chạy bằng bản của cây và restart:
#
#   truoc  (trên v0.2.0, cài bằng install.sh của gói) dựng máy nginx, pebble, site $S1
#          (example.com: tên miền gốc, SSL bằng pebble) và $OLD2, ghi mốc: http, SAN, health.
#   sau    (trên bản của cây) site_names chỉ có hai dòng primary; alias list chỉ tên chính;
#          http và https như trước; health không có phép tên mới nào chưa đạt; `site add`
#          chạy lại không thêm tên phụ.
#
# Chỉ MỘT stack (nginx), vì đường nâng cấp (migration 1900, đọc site cũ thành tên chính) không
# đi qua mã riêng của stack; Apache đã qua phần nang-cap (từ bản ngay trước M19, mặc định
# Apache) và phần apache. Nginx chọn để hai phần nang-cap cùng phủ cả hai stack.
#
# KHÔNG so sha256 vhost với v0.2.0: template site đã đổi từ v0.2.0 (M14, M28), nên vhost M19
# ghi lại khác v0.2.0 là đúng. Phần so từng byte nằm ở phần nang-cap (bản ngay trước M19).
#
# $S1 là tên miền gốc: nếu nâng cấp hay `site add` chạy lại thêm nhầm tên phụ mặc định, sẽ
# lộ ra là www.$S1 trong site_names, alias list, vhost hay chứng chỉ.
. "$(dirname "$0")/chung.sh"
PHA="${1:-}"
STACK="${2:-nginx}"
S1=$SITE
OLD2=old2.example.com
V1="$(vhost_of "$S1")"
CAFILE="$W/pebble-root.pem"
# Tiền tố ID phép kiểm của M19 (tên phụ, chứng chỉ, tên miền gốc, trùng server_name).
MA_M19=(site.alias. ssl.names names.root.shared web.server_name.unique)

# https_ca <tên> <đường dẫn>: GET https qua --resolve tới 127.0.0.1:443, kiểm chứng chỉ thật
# bằng CA gốc của pebble (không -k). Đặt HC.
https_ca() {
  HC="$(curl -s -m 30 --cacert "$CAFILE" -o "$W/h.body" -w '%{http_code}' --resolve "$1:443:127.0.0.1" "https://$1$2")"
  echo "  GET https://$1$2 (CA pebble): HTTP ${HC:-000} (curl thoát $?)"
}

# ma_chua_dat <health.json>: in "id status" của phép kiểm có ID bắt đầu bằng một tiền tố MA_M19
# mà status là warn hay fail (không in gì nghĩa là sạch).
ma_chua_dat() {
  python3 -I -c '
import json, sys
d = json.load(open(sys.argv[1]))
pre = tuple(sys.argv[2:])
def walk(x):
    if isinstance(x, dict):
        i = str(x.get("id", ""))
        if i.startswith(pre) and str(x.get("status")) in ("warn", "fail"):
            print(i, x.get("status"))
        for v in x.values(): walk(v)
    elif isinstance(x, list):
        for v in x: walk(v)
walk(d)' "$1" "${MA_M19[@]}"
}

case "$PHA" in
truoc)
  step "N0. bản đang chạy (phải là v0.2.0)"
  tpan version
  cai_may "$STACK"
  dung_pebble
  hosts_them "$S1" "$OLD2"

  step "N1. trên v0.2.0: site $S1 (tên miền gốc, SSL bằng pebble) và $OLD2"
  site_add_cli "$S1"
  ok "site add $S1 thoát 0" test "$RC" -eq 0
  site_add_cli "$OLD2"
  ok "site add $OLD2 thoát 0" test "$RC" -eq 0
  tp ssl issue "$S1" --yes
  ok "ssl issue $S1 thoát 0" test "$RC" -eq 0
  # Lệnh con lạ của bản cũ chỉ in trợ giúp và thoát 0, nên đo bằng DB: v0.2.0 chưa có
  # migration 1900, tức chưa có bảng site_names.
  ok "v0.2.0 chưa có bảng site_names (xác nhận đang đo đúng bản cũ)" test "$(sqlq "SELECT COUNT(*) FROM sqlite_master WHERE name = 'site_names'")" = 0

  step "N2. mốc trên v0.2.0: http, chứng chỉ, health"
  hreq "$S1" /readme.html
  ok "http://$S1/ có đáp án (200, hay 301 vì site có SSL)" bash -c '[ "$1" = 200 ] || [ "$1" = 301 ]' _ "$HC"
  echo "$HC $HLOC" >"$W/nc020.http"
  hreq "$OLD2" /readme.html
  ok "http://$OLD2/ trả 200" test "$HC" = 200
  san "$S1" | sort >"$W/nc020.san"
  echo "  SAN của chứng chỉ $S1: $(tr '\n' ' ' <"$W/nc020.san")"
  ok "chứng chỉ của $S1 chỉ có đúng tên $S1" test "$(cat "$W/nc020.san")" = "$S1"
  curl -sk -m 30 -o "$CAFILE" https://localhost:15000/roots/0
  ok "lấy được CA gốc của pebble (để kiểm chứng chỉ thật)" grep -q 'BEGIN CERTIFICATE' "$CAFILE"
  https_ca "$S1" /readme.html
  ok "https://$S1/ trả 200 (chứng chỉ tin được bằng CA pebble)" test "$HC" = 200
  tpan health </dev/null >"$W/nc020.health" 2>&1
  echo "  số dòng [lỗi] hay [cảnh báo] trên v0.2.0: $(grep -cE '^\[(lỗi|cảnh báo)\]' "$W/nc020.health")"
  ;;

sau)
  [ -s "$W/nc020.http" ] || bail "chưa có $W/nc020.http (pha truoc chưa chạy)"

  step "N3. bản đang chạy (bản của cây)"
  tpan version

  step "K1. site_names: đúng hai dòng primary active, không có tên phụ"
  ok "site_names có đúng 2 dòng" test "$(sqlq "SELECT COUNT(*) FROM site_names")" = 2
  ok "cả hai dòng là primary, active" test "$(sqlq "SELECT COUNT(*) FROM site_names WHERE kind = 'primary' AND state = 'active'")" = 2
  ok "tên là $S1 và $OLD2" test "$(sqlq "SELECT group_concat(name, ' ') FROM (SELECT name FROM site_names ORDER BY name)")" = "$S1 $OLD2"
  alias_list "$S1"
  ok "site alias list $S1: chỉ tên chính" pj "$W/names.json" "len(d) == 1 and d[0]['name'] == '$S1' and d[0]['kind'] == 'primary'"
  alias_list "$OLD2"
  ok "site alias list $OLD2: chỉ tên chính" pj "$W/names.json" "len(d) == 1 and d[0]['name'] == '$OLD2' and d[0]['kind'] == 'primary'"

  step "K2. http và https như trước khi nâng"
  hreq "$S1" /readme.html
  ok "http://$S1/ giữ đáp án như trên v0.2.0 (mã và Location)" test "$HC $HLOC" = "$(cat "$W/nc020.http")"
  hreq "$OLD2" /readme.html
  ok "http://$OLD2/ trả 200" test "$HC" = 200
  https_ca "$S1" /readme.html
  ok "https://$S1/ trả 200 (chứng chỉ tin được bằng CA pebble)" test "$HC" = 200
  ok "chứng chỉ $S1 không đổi: vẫn chỉ có tên $S1" test "$(san "$S1" | sort)" = "$(cat "$W/nc020.san")"

  step "K3. health: không có phép về tên phụ, chứng chỉ, tên miền gốc, trùng server_name chưa đạt"
  tpan health --json >"$W/nc020.health.json" 2>/dev/null </dev/null
  echo "  trạng thái các phép M19: site.alias.: [$(hs "$W/nc020.health.json" site.alias.)] ssl.names: [$(hs "$W/nc020.health.json" ssl.names)] names.root.shared: [$(hs "$W/nc020.health.json" names.root.shared)] web.server_name.unique: [$(hs "$W/nc020.health.json" web.server_name.unique)]"
  chua="$(ma_chua_dat "$W/nc020.health.json")"
  [ -n "$chua" ] && printf '  chưa đạt:\n%s\n' "$chua"
  ok "health không có phép ${MA_M19[*]} nào ở trạng thái warn hay fail" test -z "$chua"
  # Dòng [lỗi]/[cảnh báo] mới khác (ví dụ của M14, M28) chỉ ghi lại, không khẳng định:
  # v0.2.0 -> cây có nhiều mốc, phần nâng cấp từng mốc đã có e2e riêng.
  tpan health </dev/null >"$W/nc020.health.sau" 2>&1
  echo "  số dòng [lỗi] hay [cảnh báo]: v0.2.0 $(grep -cE '^\[(lỗi|cảnh báo)\]' "$W/nc020.health"), bản của cây $(grep -cE '^\[(lỗi|cảnh báo)\]' "$W/nc020.health.sau")"

  step "K4. site add $S1 chạy lại (tên miền gốc): không thêm tên phụ"
  site_add_cli "$S1"
  ok "site add $S1 chạy lại thoát 0" test "$RC" -eq 0
  cho_roi
  alias_list "$S1"
  ok "site alias list $S1: vẫn chỉ tên chính (không có www.$S1)" pj "$W/names.json" "len(d) == 1 and d[0]['kind'] == 'primary'"
  ok "site_names vẫn đúng 2 dòng" test "$(sqlq "SELECT COUNT(*) FROM site_names")" = 2
  ok "vhost $S1 không nhắc www.$S1" bash -c '! grep -q "www\.$1" "$2"' _ "$S1" "$V1"
  ok "chứng chỉ $S1 vẫn chỉ có tên $S1" test "$(san "$S1" | sort)" = "$(cat "$W/nc020.san")"
  https_ca "$S1" /readme.html
  ok "https://$S1/ vẫn 200" test "$HC" = 200
  ;;

*)
  bail "dùng: nang-cap-v020.sh truoc|sau [nginx|apache]"
  ;;
esac
