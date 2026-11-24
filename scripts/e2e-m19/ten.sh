#!/usr/bin/env bash
# e2e M19, nhóm tên phụ và quyền: E01-E06, E10 (bằng API), E20-E22, E24-E26, và S1, S2, S6,
# S12 của "Trả lời soát lượt 1". Chạy bằng root trong máy ảo, sau dung-may.sh.
# E23 (giao diện của khach_b) ở e2e-m19-gd.
. "$(dirname "$0")/chung.sh"
can_may

# dup_names <vhost>: in các tên bị lặp trong cùng một khối server/VirtualHost; thoát 1 khi có.
dup_names() {
  python3 -I -c '
import re, sys
seen = None
bad = []
for line in open(sys.argv[1]):
    if re.match(r"\s*(server\s*\{|<VirtualHost)", line):
        seen = set()
    m = re.match(r"\s*(server_name|ServerName|ServerAlias)\s+(.*?);?\s*$", line)
    if m and seen is not None:
        for t in m.group(2).split():
            if t in seen:
                bad.append(t)
            seen.add(t)
print(" ".join(bad))
sys.exit(1 if bad else 0)' "$1"
}
rows_of() { sqlq "SELECT COUNT(*) FROM site_names WHERE site = ?" "$1"; }
name_body() { printf '{"name":"%s"%s}' "$1" "${2:+,\"mode\":\"$2\"}"; }

step "E01. tpan site add $SITE (admin; mặc định --www redirect)"
site_add_cli "$SITE"
ok "E01: site add $SITE thoát 0" test "$RC" -eq 0
alias_list "$SITE"
ok "E01: site alias list có $WWW redirect active" ten_co "$WWW" "n['kind'] == 'alias' and n['mode'] == 'redirect' and n['state'] == 'active'"

step "E02. Host: $WWW (redirect) -> 301 về tên chính, giữ đường dẫn và query"
hreq "$WWW" "/a?b=1"
ok "E02: 301, Location http://$SITE/a?b=1" test "$HC" = 301 -a "$HLOC" = "http://$SITE/a?b=1"

step "E03. Host: $SITE -> 200, trang WordPress"
hreq "$SITE" /
ok "E03: 200 và là trang WordPress" bash -c '[ "$1" = 200 ]' _ "$HC"
ok "E03: thân có dấu WordPress" is_wp

step "E04. alias set $WWW --mode serve (S2: đo bằng /readme.html và X-Redirect-By)"
tp site alias set "$SITE" "$WWW" --mode serve --yes
ok "E04: alias set thoát 0" test "$RC" -eq 0
hreq "$SITE" /readme.html
c1=$HC
hreq "$WWW" /readme.html
ok "E04: cả hai tên trả 200 cho /readme.html (S2)" test "$c1" = 200 -a "$HC" = 200
hreq "$WWW" /
ok "E04: / qua $WWW kiểu serve trả 301 có X-Redirect-By: WordPress (WordPress tự đưa www về tên trong cài đặt)" \
  test "$HC" = 301 -a "$HXRB" = WordPress
tp site alias set "$SITE" "$WWW" --mode redirect --yes
ok "E04: đặt lại redirect, thoát 0" test "$RC" -eq 0
hreq "$WWW" "/a?b=1"
ok "E04: sau khi đặt lại: 301 về tên chính, không có X-Redirect-By" test "$HC" = 301 -a "$HLOC" = "http://$SITE/a?b=1" -a -z "$HXRB"

step "E05. tpan site add $A_SITE --owner $UA --www redirect (khach_a)"
site_add_cli "$A_SITE" --owner "$UA" --www redirect
ok "E05: site add $A_SITE thoát 0" test "$RC" -eq 0
alias_list "$A_SITE"
ok "E05: tên phụ $A_ALIAS kiểu redirect active" ten_co "$A_ALIAS" "n['kind'] == 'alias' and n['mode'] == 'redirect' and n['state'] == 'active'"
hreq "$A_ALIAS" /
ok "E05: $A_ALIAS chuyển về $A_SITE (301, Location http://$A_SITE/)" test "$HC" = 301 -a "$HLOC" = "http://$A_SITE/"

step "E06. tpan site add $SUBSITE (tên con: không có tên phụ nào)"
site_add_cli "$SUBSITE"
ok "E06: site add $SUBSITE thoát 0" test "$RC" -eq 0
alias_list "$SUBSITE"
ok "E06: không có tên phụ" pj "$W/names.json" "len(d) == 1 and d[0]['kind'] == 'primary'"

step "E10. thêm tên phụ $NET (serve) và $VN_UTF8 (serve) cho $SITE bằng API (admin). E10 giao diện: ở e2e-m19-gd"
call_job admin POST "/sites/$SITE/names" "$(name_body "$NET" serve)"
ok "E10: POST names $NET: 202, job succeeded" test "$HTTP" = 202 -a "$JOBST" = succeeded
hreq "$NET" /readme.html
ok "E10: Host $NET (serve) trả 200 cho /readme.html" test "$HC" = 200
call_job admin POST "/sites/$SITE/names" "$(name_body "$VN_UTF8" serve)"
ok "E10: POST names $VN_UTF8: 202, job succeeded" test "$HTTP" = 202 -a "$JOBST" = succeeded
call admin GET "/sites/$SITE/names" "$W/n.json" 60
ok "E10: tên UTF-8 được lưu dạng punycode $VN (t4)" pj "$W/n.json" "any(n['name'] == '$VN' and n['mode'] == 'serve' for n in d)"
call admin GET /domains "$W/d.json" 60
ok "E10: GET /domains (admin) nêu $SITE với các tên phụ $WWW, $NET, $VN" \
  pj "$W/d.json" "any(x['site'] == '$SITE' and set(x['names']) >= {'$WWW', '$NET', '$VN'} for x in d)"

step "E20 (S1). khach_b: POST names $WWW (tên của site người khác) -> 409 name_unavailable, không lộ tên site hay chủ"
site_add_cli "$B_SITE" --owner "$UB"
ok "(dựng) site add $B_SITE --owner $UB thoát 0" test "$RC" -eq 0
call "$UB" POST "/sites/$B_SITE/names" "$W/e20.json" 120 "$(name_body "$WWW")"
ok "E20: 409" test "$HTTP" = 409
ok "E20: mã name_unavailable (user thường không thấy name_taken/name_root_foreign)" test "$(errcode "$W/e20.json")" = name_unavailable
ok "E20: thân không có tên site hay tên chủ của người khác" no_leak "$W/e20.json" "$SITE" "$SUBSITE" "$A_SITE" "$A_ALIAS" "$UA" admin
call admin POST "/sites/$B_SITE/names?dry_run=1" "$W/e20a.json" 120 "$(name_body "$WWW")"
note "admin cùng yêu cầu trên site của khach_b (dry_run): HTTP $HTTP, mã $(errcode "$W/e20a.json") (admin được miễn luật tên miền gốc; tên đã có chủ vẫn bị từ chối bằng mã chi tiết name_taken)"
ok "E20: admin nhận mã chi tiết name_taken (S1: admin thấy mã thật)" test "$HTTP" = 409 -a "$(errcode "$W/e20a.json")" = name_taken

step "E21. khach_b: tên thuộc tên miền gốc của admin ($SITE), dạng khác nhau -> 409 hoặc 400, không thêm dòng"
before="$(rows_of "$B_SITE")"
for n in "shop.example.com" "example.com." "EXAMPLE.COM" "$VN" "$NET"; do
  call "$UB" POST "/sites/$B_SITE/names" "$W/e21.json" 120 "$(name_body "$n")"
  ok "E21: $n -> 409 hay 400 (mã $(errcode "$W/e21.json"))" bash -c '[ "$1" = 409 ] || [ "$1" = 400 ]' _ "$HTTP"
done
ok "E21: site_names của $B_SITE không thêm dòng ($before dòng trước và sau)" test "$(rows_of "$B_SITE")" = "$before"

step "E22 (S1). khach_b tạo site chung tên miền gốc với admin: 404 site_not_found như M28"
call "$UB" POST "/sites?dry_run=1" "$W/e22.json" 120 "{\"domain\":\"shop.example.com\",\"email\":\"$EMAIL\"}"
ok "E22: POST /sites shop.example.com của khach_b: 404" test "$HTTP" = 404
ok "E22: mã site_not_found" test "$(errcode "$W/e22.json")" = site_not_found
note "E22 lệch bảng 7.2: bản 7.2 viết 'admin làm hộ bị name_root_foreign'; S1 cho admin miễn luật tên miền gốc (Claim.ByAdmin), nên chỉ thử --dry-run và ghi nhận, không khẳng định"
tp site add shop.example.com --owner "$UB" --email "$EMAIL" --dry-run
ok "E22: dry-run của admin không ghi dòng nào vào site_names" test "$(sqlq "SELECT COUNT(*) FROM site_names WHERE name = 'shop.example.com'")" = 0

step "E23. giao diện của khach_b: ở e2e-m19-gd"

step "E25, E26. chủ site thường tự thêm và gỡ tên phụ của mình; user khác không thấy site (404)"
call_job "$UA" POST "/sites/$A_SITE/names" "$(name_body "blog.example.org" serve)"
ok "E25: khach_a thêm blog.example.org (tên miền gốc của chính mình): 202, job succeeded" test "$HTTP" = 202 -a "$JOBST" = succeeded
hreq "blog.example.org" /readme.html
ok "E25: tên đó (serve) trả 200 cho /readme.html" test "$HC" = 200
call "$UA" POST "/sites/$A_SITE/names/blog.example.org/mode?dry_run=1" "$W/e25.json" 60 '{"mode":"redirect"}'
ok "E25: POST names/{name}/mode?dry_run=1: 200" test "$HTTP" = 200
call_job "$UA" DELETE "/sites/$A_SITE/names/blog.example.org"
ok "E25: khach_a gỡ blog.example.org: 202, job succeeded" test "$HTTP" = 202 -a "$JOBST" = succeeded
call "$UB" GET "/sites/$A_SITE/names" "$W/e26.json" 60
ok "E26: khach_b xem tên của site khach_a: 404 site_not_found" test "$HTTP" = 404 -a "$(errcode "$W/e26.json")" = site_not_found
call "$UA" POST "/sites/$SITE/names?dry_run=1" "$W/e26.json" 60 "$(name_body "x.example.org")"
ok "E26: khach_a thêm tên cho site của admin: 404 site_not_found" test "$HTTP" = 404 -a "$(errcode "$W/e26.json")" = site_not_found

step "E24 (S6). khach_a, khach_b: 10 + 10 request song song thêm $RACE; thêm 3 lệnh CLI tpan site alias add song song"
cho_roi
body="$(name_body "$RACE")"
for i in $(seq 1 10); do
  (c="$(curl -sk -m 180 -b "$W/jar-$UA" -o "$W/e24-a$i.body" -w '%{http_code}' -X POST -H 'Content-Type: application/json' --data "$body" "$API/sites/$A_SITE/names")"
    echo "$c" >"$W/e24-a$i.code") &
  (c="$(curl -sk -m 180 -b "$W/jar-$UB" -o "$W/e24-b$i.body" -w '%{http_code}' -X POST -H 'Content-Type: application/json' --data "$body" "$API/sites/$B_SITE/names")"
    echo "$c" >"$W/e24-b$i.code") &
done
for i in 1 2 3; do
  s="$A_SITE"
  [ "$i" = 3 ] && s="$B_SITE"
  (tpan site alias add "$s" "$RACE" --yes </dev/null >"$W/e24-cli$i.out" 2>&1
    echo "$?" >"$W/e24-cli$i.rc") &
done
wait
cho_roi
win=0
wsites=""
lose_ok=1
for p in a b; do
  for i in $(seq 1 10); do
    c="$(cat "$W/e24-$p$i.code")"
    if [ "$c" = 202 ]; then
      win=$((win + 1))
      wsites="$wsites $p"
      j="$(pv "$W/e24-$p$i.body" 'd["job_id"]')"
      echo "  người thắng: API $p$i, job $j: $(cho_job "$j")"
    else
      code="$(errcode "$W/e24-$p$i.body")"
      case "$c:$code" in
        409:job_running | 409:name_unavailable | 409:alias_exists | 409:name_taken) ;;
        *) lose_ok=0; echo "  request $p$i: HTTP $c, mã '$code' (ngoài tập cho phép)" ;;
      esac
    fi
  done
done
for i in 1 2 3; do
  if [ "$(cat "$W/e24-cli$i.rc")" = 0 ]; then
    win=$((win + 1))
    if [ "$i" = 3 ]; then wsites="$wsites b"; else wsites="$wsites a"; fi
    echo "  người thắng: CLI $i"
  else
    echo "  CLI $i thua (thoát $(cat "$W/e24-cli$i.rc")): $(tr '\n' ' ' <"$W/e24-cli$i.out" | head -c 160)"
  fi
done
echo "  số người thắng (202 của API + CLI thoát 0): $win"
# Nhiều request cùng site, cùng chủ có thể cùng "thắng": lần sau nhận lại dòng
# pending của chính site đó (S9), kết quả là một dòng, không lặp tên. Bất biến
# B2 là: mọi người thắng thuộc CÙNG MỘT site.
nsite="$(printf '%s\n' $wsites | sort -u | grep -c .)"
echo "  site của người thắng:$wsites (số site khác nhau: $nsite)"
ok "E24: có người thắng và mọi người thắng thuộc cùng một site (API và CLI)" test "$win" -ge 1 -a "$nsite" = 1
ok "E24: mọi request thua nhận 409 job_running / name_unavailable / alias_exists / name_taken" test "$lose_ok" = 1
ok "E24: site_names có đúng một dòng mang tên $RACE" test "$(sqlq "SELECT COUNT(*) FROM site_names WHERE name = ?" "$RACE")" = 1
ok "E24: dòng đó active (job thắng đã xong, không còn pending mồ côi)" test "$(sqlq "SELECT state FROM site_names WHERE name = ?" "$RACE")" = active
winsite="$(sqlq "SELECT site FROM site_names WHERE name = ?" "$RACE")"
echo "  site giữ $RACE: $winsite"
for s in "$A_SITE" "$B_SITE" "$SITE"; do
  ok "E24: vhost của $s không lặp tên trong server_name/ServerAlias (lặp: $(dup_names "$(vhost_of "$s")"))" dup_names "$(vhost_of "$s")"
done
ok "E24: cú pháp config của $WEB đạt, không có 'conflicting server name'" bash -c '! grep -qi "conflicting server name" <<<"$1" && grep -qiE "syntax (is )?ok|test is successful" <<<"$1"' _ "$(web_test)"
tp site alias rm "$winsite" "$RACE" --yes
ok "E24: dọn: alias rm $RACE thoát 0" test "$RC" -eq 0

step "S12. site tạm khoá: thao tác tên phụ -> site_suspended; site trong thùng rác -> site_not_active"
tp site suspend "$SUBSITE" --yes
ok "S12: site suspend $SUBSITE thoát 0" test "$RC" -eq 0
call admin POST "/sites/$SUBSITE/names" "$W/s12.json" 120 "$(name_body "www.$SUBSITE")"
ok "S12: thêm tên phụ trên site tạm khoá: 409 site_suspended" test "$HTTP" = 409 -a "$(errcode "$W/s12.json")" = site_suspended
tp site alias add "$SUBSITE" "www.$SUBSITE" --yes
ok "S12: CLI alias add trên site tạm khoá: thoát khác 0" test "$RC" -ne 0
tp site unsuspend "$SUBSITE" --yes
ok "S12: site unsuspend $SUBSITE thoát 0" test "$RC" -eq 0
tp site rm "$SUBSITE" --yes
ok "S12: site rm $SUBSITE (vào thùng rác) thoát 0" test "$RC" -eq 0
call admin POST "/sites/$SUBSITE/names" "$W/s12.json" 120 "$(name_body "www.$SUBSITE")"
ok "S12: thêm tên phụ trên site trong thùng rác: mã site_not_active (HTTP $HTTP)" test "$(errcode "$W/s12.json")" = site_not_active
tp site restore "$SUBSITE" --yes
ok "S12: site restore $SUBSITE thoát 0" test "$RC" -eq 0
