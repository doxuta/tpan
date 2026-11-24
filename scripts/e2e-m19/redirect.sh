#!/usr/bin/env bash
# e2e M19, nhóm luật chuyển hướng theo đường dẫn: E30, E31, E33, E32 (theo thứ tự này: E32
# tính số luật còn thiếu tới 50), và S11, S15, S20 của "Trả lời soát lượt 1". Chạy bằng root
# trong máy ảo, sau ten.sh (site $SITE chưa SSL, nên Location có scheme http).
#
# Phần luật của M19 là đợt 2 (đang viết khi script này được viết): tên lệnh và mã lỗi gom
# thành biến ở đầu file. Lệnh `tpan site redirect` chưa có thì in một khẳng định không đạt
# rồi bỏ cả nhóm (không đổ lỗi dây chuyền).
. "$(dirname "$0")/chung.sh"
can_may

CODE_INVALID=redirect_invalid
CODE_RESERVED=redirect_reserved
CODE_LIMIT=redirect_limit
CODE_EXISTS=redirect_exists
CODE_LOOP=redirect_loop
CODE_QCONFLICT=redirect_query_conflict
MAXR=50
VH="$(vhost_of "$SITE")"

rnd() { head -c 5 /dev/urandom | od -An -tx1 | tr -d ' \n'; }
# rd_body <nguồn> <đích> [mã] [keep_query true|false]: thân JSON của POST /redirects.
rd_body() {
  python3 -I -c 'import json, sys
print(json.dumps({"source": sys.argv[1], "target": sys.argv[2], "code": int(sys.argv[3]), "keep_query": sys.argv[4] == "true"}))' \
    "$1" "$2" "${3:-301}" "${4:-false}"
}
rd_cli() { tp site redirect add "$SITE" "$1" "$2" --yes "${@:3}"; } # đặt RC
rd_try() { call admin POST "/sites/$SITE/redirects" "$W/rd.json" 300 "$(rd_body "$@")"; } # đặt HTTP
rd_job() { call_job admin POST "/sites/$SITE/redirects" "$(rd_body "$@")"; }             # đặt HTTP, JOBST
rd_count() { call admin GET "/sites/$SITE/redirects" "$W/rl.json" 60 >/dev/null; pv "$W/rl.json" 'len(d)'; }

step "E30 (cổng). tpan site redirect có không?"
if ! tpan site redirect --help >/dev/null 2>&1; then
  echo "  => KHÔNG ĐẠT: E30-E33, S11: lệnh tpan site redirect chưa có trong bản này, bỏ cả nhóm"
  exit 0
fi
echo "  có lệnh tpan site redirect"

step "E30. tpan site redirect add $SITE /old-page /new-page: 301 đúng đích, query bị bỏ"
rd_cli /old-page /new-page
ok "E30: redirect add thoát 0" test "$RC" -eq 0
hreq "$SITE" "/old-page?x=1"
ok "E30: 301, Location http://$SITE/new-page (?x=1 bị bỏ)" test "$HC" = 301 -a "$HLOC" = "http://$SITE/new-page"
hreq "$SITE" "/old-page/"
ok "E30: khớp đúng đường dẫn: /old-page/ (có / cuối) không bị luật bắt" test "$HLOC" != "http://$SITE/new-page"
hreq "$SITE" "/Old-Page"
ok "E30: phân biệt hoa thường: /Old-Page không bị luật bắt" test "$HLOC" != "http://$SITE/new-page"
tpan site redirect list "$SITE" --json >"$W/rl-cli.json" 2>/dev/null </dev/null
ok "E30: redirect list --json thoát 0 và có đúng 1 luật" bash -c '[ "$1" = 0 ]' _ "$?"
ok "E30: luật trong list là /old-page -> /new-page 301" pj "$W/rl-cli.json" "len(d) == 1 and d[0]['source'] == '/old-page' and d[0]['target'] == '/new-page' and d[0]['code'] == 301"

step "E33. keep_query, 302, và các luật từ chối (query_conflict, exists, loop)"
rd_cli /kq /dich-kq --keep-query
ok "E33: redirect add /kq --keep-query thoát 0" test "$RC" -eq 0
hreq "$SITE" "/kq?a=1&b=2"
ok "E33: keep_query: 301, Location http://$SITE/dich-kq?a=1&b=2" test "$HC" = 301 -a "$HLOC" = "http://$SITE/dich-kq?a=1&b=2"
rd_cli /tam /dich-tam --code 302
ok "E33: redirect add /tam --code 302 thoát 0" test "$RC" -eq 0
hreq "$SITE" "/tam?z=1"
ok "E33: 302, Location http://$SITE/dich-tam (query bỏ)" test "$HC" = 302 -a "$HLOC" = "http://$SITE/dich-tam"
rd_try /kc "/x?y=1" 301 true
ok "E33: --keep-query với đích đã có query: 400 $CODE_QCONFLICT" test "$HTTP" = 400 -a "$(errcode "$W/rd.json")" = "$CODE_QCONFLICT"
rd_try /old-page /new-page 301 false
ok "E33: thêm lại luật đã có: 409 $CODE_EXISTS" test "$HTTP" = 409 -a "$(errcode "$W/rd.json")" = "$CODE_EXISTS"
rd_try /lself /lself
ok "E33 (S15): nguồn bằng đích: 400 $CODE_LOOP" test "$HTTP" = 400 -a "$(errcode "$W/rd.json")" = "$CODE_LOOP"
rd_try /lc "http://$SITE/lc"
ok "E33 (S15): đích là URL tuyệt đối về chính tên của site, cùng đường dẫn với nguồn: 400 $CODE_LOOP" test "$HTTP" = 400 -a "$(errcode "$W/rd.json")" = "$CODE_LOOP"
rd_cli /la /lb
ok "E33 (S20): redirect add /la /lb thoát 0" test "$RC" -eq 0
rd_try /lb /la
ok "E33 (S20): luật /lb -> /la khép vòng với /la -> /lb: 400 $CODE_LOOP" test "$HTTP" = 400 -a "$(errcode "$W/rd.json")" = "$CODE_LOOP"
call admin GET "/sites/$SITE/redirects" "$W/rl.json" 60
lid="$(pv "$W/rl.json" "[r['id'] for r in d if r['source'] == '/la'][0]")"
tp site redirect rm "$SITE" "$lid" --yes
ok "E33: redirect rm $lid (/la) thoát 0" test "$RC" -eq 0
hreq "$SITE" "/la"
ok "E33: sau khi gỡ, /la không còn bị chuyển" test "$HC" != 301

step "S11. nguồn / và /index.php bị từ chối"
for s in / /index.php; do
  rd_try "$s" /x
  ok "S11: nguồn $s: 400 $CODE_RESERVED, field source" test "$HTTP" = 400 -a "$(errcode "$W/rd.json")" = "$CODE_RESERVED" -a "$(pv "$W/rd.json" 'd["error"].get("field") or d.get("field")')" = source
  rd_cli "$s" /x
  ok "S11: CLI nguồn $s: thoát khác 0" test "$RC" -ne 0
done

step "E31. chín đầu vào xấu: CLI thoát khác 0, API 400 $CODE_INVALID kèm field; vhost không đổi"
BAD=($'/a\nb' '/a;b' '/a}' '/a b' 'javascript:alert(1)' '//evil.example.org' '/a$x' '/%2e%2e' '/.well-known/x')
sha0="$(sha "$VH")"
for s in "${BAD[@]}"; do
  label="$(printf '%q' "$s")"
  rd_cli "$s" /x
  ok "E31: CLI nguồn $label: thoát khác 0" test "$RC" -ne 0
  rd_try "$s" /x
  ok "E31: API nguồn $label: 400 $CODE_INVALID" test "$HTTP" = 400 -a "$(errcode "$W/rd.json")" = "$CODE_INVALID"
  ok "E31: API nguồn $label: có field (source)" test "$(pv "$W/rd.json" 'd["error"].get("field") or d.get("field")')" = source
done
ok "E31: vhost không đổi sau chín đầu vào xấu (sha256 $sha0 trước, $(sha "$VH") sau)" test "$(sha "$VH")" = "$sha0"

step "E32. thêm luật hợp lệ ngẫu nhiên tới đủ $MAXR; luật thứ $((MAXR + 1)): $CODE_LIMIT"
n="$(rd_count)"
echo "  số luật hiện có: $n"
bad_add=0
while [ "${n:-0}" -lt "$MAXR" ]; do
  rd_job "/r-$(rnd)-$(rnd)" "/t-$(rnd)"
  if [ "$HTTP" != 202 ] || [ "$JOBST" != succeeded ]; then
    bad_add=1
    echo "  thêm luật thứ $((n + 1)) lỗi: HTTP $HTTP, job $JOBST, mã $(errcode "$W/cj.out")"
    break
  fi
  n=$((n + 1))
done
ok "E32: thêm đủ luật hợp lệ, mọi job succeeded" test "$bad_add" = 0
ok "E32: GET /redirects có đúng $MAXR luật" test "$(rd_count)" = "$MAXR"
web_test >"$W/wt.out" 2>&1
rc=$?
tail -2 "$W/wt.out"
ok "E32: cú pháp config của $WEB thoát 0 với $MAXR luật" test "$rc" -eq 0
hreq "$SITE" "/old-page?x=1"
ok "E32: luật đầu vẫn chạy: 301 http://$SITE/new-page" test "$HC" = 301 -a "$HLOC" = "http://$SITE/new-page"
rd_try "/r-$(rnd)-$(rnd)" "/t-$(rnd)"
ok "E32: luật thứ $((MAXR + 1)): 409 $CODE_LIMIT" test "$HTTP" = 409 -a "$(errcode "$W/rd.json")" = "$CODE_LIMIT"
