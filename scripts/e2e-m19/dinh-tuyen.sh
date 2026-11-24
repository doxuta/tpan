#!/usr/bin/env bash
# e2e M19, nhóm định tuyến: E50 (bộ R01-R14 dựng lại ở mục 2.3 của thiết kế, đo bằng
# request thô qua http-raw.py), E51, E52 (tpan route explain với tên phụ). Chạy bằng root
# trong máy ảo, sau ten.sh (cần $SITE với $WWW redirect, $NET và $VN serve; chưa có SSL).
. "$(dirname "$0")/chung.sh"
can_may
RAW="$D/http-raw.py"

# rr <nhãn> <mong đợi nginx> <mong đợi Apache> <Location mong đợi, rỗng là không xét> <request thô>
# Request thô viết dạng printf %b. Thoát 0 khi mã (hay CLOSED) và Location đúng bảng.
rr() {
  local id=$1 en=$2 ea=$3 eloc=$4 req=$5 want out st loc
  want=$en
  [ "$WEB" = apache ] && want=$ea
  out="$(printf '%b' "$req" | python3 -I "$RAW" 80)"
  st="$(awk '$1 == "STATUS" { print $2 }' <<<"$out")"
  loc="$(awk '$1 == "LOCATION" { print $2 }' <<<"$out")"
  echo "  $id ($WEB): STATUS $st, LOCATION $loc (bảng: $want${eloc:+, Location $eloc})"
  [ "$st" = "$want" ] && { [ -z "$eloc" ] || [ "$loc" = "$eloc" ]; }
}
req() { # req <Host: giá trị> [dòng yêu cầu]: request GET đóng kết nối sau khi trả lời
  printf '%s HTTP/1.1\\r\\nHost:%s\\r\\nConnection: close\\r\\n\\r\\n' "${2:-GET /}" "$1"
}

TC="http://$SITE/"
LONG="$(python3 -I -c 'print("a" * 250 + ".example.org")')"

step "E50. bộ R01-R14 trên stack $WEB (trạng thái: chưa SSL; $WWW redirect; $NET và $VN serve)"
ok "R01: Host WWW.EXAMPLE.COM -> 301 về tên chính" rr R01 301 301 "$TC" "$(req ' WWW.EXAMPLE.COM')"
ok "R02: Host XN--VITNAM-JK8B.EXAMPLE.NET -> 200" rr R02 200 200 "" "$(req ' XN--VITNAM-JK8B.EXAMPLE.NET' 'GET /readme.html')"
ok "R03: Host www.example.com. (chấm cuối) -> 301" rr R03 301 301 "$TC" "$(req ' www.example.com.')"
ok "R04: Host xn--vitnam-jk8b.example.net. (chấm cuối) -> 200" rr R04 200 200 "" "$(req ' xn--vitnam-jk8b.example.net.' 'GET /readme.html')"
ok "R05: Host www.example.com:80 -> 301" rr R05 301 301 "$TC" "$(req ' www.example.com:80')"
ok "R06: Host www.example.com:8080 -> 301" rr R06 301 301 "$TC" "$(req ' www.example.com:8080')"
ok "R07: Host việtnam.example.net (UTF-8 thô) -> nginx đóng kết nối, Apache 400" rr R07 CLOSED 400 "" "$(req ' việtnam.example.net')"
ok "R08: Host evil.example.org -> vhost mặc định (nginx đóng kết nối, Apache 403)" rr R08 CLOSED 403 "" "$(req ' evil.example.org')"
ok "R09: Host 127.0.0.1 -> vhost mặc định" rr R09 CLOSED 403 "" "$(req ' 127.0.0.1')"
ok "R10: Host rỗng -> 400" rr R10 400 400 "" "$(req '')"
ok "R11: Host 262 ký tự -> vhost mặc định" rr R11 CLOSED 403 "" "$(req " $LONG")"
ok "R12: Host x.www.example.com (tên con chưa khai, không có tên đại diện) -> vhost mặc định" rr R12 CLOSED 403 "" "$(req ' x.www.example.com')"
ok "R13: GET http://evil.example.org/, Host example.com -> tên ở dòng yêu cầu thắng, vhost mặc định" \
  rr R13 CLOSED 403 "" "$(req ' example.com' 'GET http://evil.example.org/')"
ok "R14: GET http://www.example.com/x, Host evil.example.org -> 301 (chọn theo dòng yêu cầu)" \
  rr R14 301 301 "" "$(req ' evil.example.org' 'GET http://www.example.com/x')"

step "E51. tpan route explain với tên phụ"
tp route explain "$WWW"
ok "E51: route explain $WWW (redirect) thoát 0" test "$RC" -eq 0
ok "E51: nêu file vhost tpan-$SITE.conf" grep -q "tpan-$SITE.conf" "$W/tp.out"
ok "E51: có chặng chuyển hướng (redirect/chuyển)" grep -qiE 'redirect|chuyển' "$W/tp.out"
tp route explain "$NET"
ok "E51: route explain $NET (serve) thoát 0" test "$RC" -eq 0
ok "E51: nêu file vhost tpan-$SITE.conf" grep -q "tpan-$SITE.conf" "$W/tp.out"
ok "E51: đi tới chặng HTTP" grep -qi 'http' "$W/tp.out"
tpan route explain "$WWW" --json >"$W/route.json" 2>/dev/null </dev/null
ok "E51: --json có host = $WWW" pj "$W/route.json" "d['host'] == '$WWW'"

step "E52. tpan route explain evil.example.org (không thuộc site nào)"
tp route explain evil.example.org
ok "E52: thoát 1" test "$RC" -eq 1
ok "E52: nói tên không thuộc site nào (name_unknown)" grep -qiE 'name_unknown|không site nào có tên này|no site (has|owns) this name' "$W/tp.out"
