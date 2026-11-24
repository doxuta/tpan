#!/usr/bin/env bash
# e2e M19, nhóm chứng chỉ nhiều tên (pebble): E11, E12, E40, E42, E43, E44, E53. Chạy bằng
# root trong máy ảo, sau ten.sh, dinh-tuyen.sh, redirect.sh (cần $SITE với $WWW redirect,
# $NET và $VN serve; dung-may.sh đã đặt `tpan ssl settings` trỏ về pebble).
# E41 (giao diện) ở e2e-m19-gd.
. "$(dirname "$0")/chung.sh"
can_may
AD=/var/lib/tpan/acme/.well-known/acme-challenge
trap 'rm -f /var/lib/tpan/acme/.well-known/acme-challenge/e2e-m19' EXIT

# hs_fail <file health --json>: ID các phép kiểm của M19 (site.alias, ssl.names, names.root) ở mức fail.
hs_fail() {
  python3 -I -c '
import json, sys
d = json.load(open(sys.argv[1]))
out = []
def walk(x):
    if isinstance(x, dict):
        i = str(x.get("id", ""))
        if i.startswith(("site.alias", "ssl.names", "names.root")) and x.get("status") == "fail":
            out.append(i)
        for v in x.values(): walk(v)
    elif isinstance(x, list):
        for v in x: walk(v)
walk(d)
print(",".join(out))' "$1" 2>/dev/null
}

step "E11. tpan ssl issue $SITE (pebble): chứng chỉ có $SITE, $WWW, $NET"
tp ssl issue "$SITE" --yes
ok "E11: ssl issue thoát 0" test "$RC" -eq 0
cho_roi
echo "  tên trong chứng chỉ: $(san "$SITE" | tr '\n' ' ')"
for n in "$SITE" "$WWW" "$NET"; do
  ok "E11: subjectAltName có $n" bash -c 'san() { openssl x509 -in "$1" -noout -ext subjectAltName | tr "," "\n" | sed -n "s/^ *DNS://p"; }; san "$1" | grep -qx "$2"' _ "$(cert_of "$SITE")" "$n"
done
alias_list "$SITE"
ok "E11: $WWW in_cert true" ten_co "$WWW" "n['in_cert'] is True"
ok "E11: $NET in_cert true" ten_co "$NET" "n['in_cert'] is True"

step "E12. https qua curl --resolve: $WWW (redirect) và $NET (serve)"
hsreq "$WWW" /
ok "E12: https://$WWW/ -> 301, Location https://$SITE/" test "$HC" = 301 -a "$HLOC" = "https://$SITE/"
hsreq "$NET" /readme.html
ok "E12: https://$NET/readme.html (serve) -> 200" test "$HC" = 200
hreq "$NET" "/x?y=1"
ok "E12: http://$NET/x?y=1 (serve, có trong chứng chỉ) -> 301 https://$NET/x?y=1" test "$HC" = 301 -a "$HLOC" = "https://$NET/x?y=1"
hreq "$SITE" /
ok "E12: http://$SITE/ -> 301 https://$SITE/" test "$HC" = 301 -a "$HLOC" = "https://$SITE/"

step "E40 (S3). thêm tên phụ $NODNS (không có DNS) cho site SSL: tên thêm được, chứng chỉ cũ còn nguyên"
s0="$(serial_of "$SITE")"
echo "  serial trước: $s0"
tp site alias add "$SITE" "$NODNS" --mode serve --yes
ok "E40: alias add thoát 0 (tên phụ thêm được dù không có DNS)" test "$RC" -eq 0
cho_roi
note "lý do trong output CLI: $(grep -c "$NODNS" "$W/tp.out") dòng nhắc $NODNS"
alias_list "$SITE"
ok "E40: $NODNS active, in_cert false" ten_co "$NODNS" "n['state'] == 'active' and n['in_cert'] is False"
ok "E40: serial chứng chỉ không đổi ($(serial_of "$SITE"))" test "$(serial_of "$SITE")" = "$s0"
ok "E40: job site.ssl mới nhất không hỏng (S3: không chạy certbot hoặc đạt kèm ghi chú)" test "$(sqlq "SELECT status FROM jobs WHERE kind = 'site.ssl' ORDER BY started_at DESC LIMIT 1")" = succeeded
hsreq "$SITE" /readme.html
ok "E40: https://$SITE/ vẫn 200" test "$HC" = 200

step "E42. https tới $NODNS (chưa có trong chứng chỉ): không vào vhost của site"
hsreq "$NODNS" /readme.html
ok "E42: không trả trang WordPress của site (HTTP ${HC:-000}, curl thoát $CRC; nginx đóng bắt tay, Apache 403)" bash -c '! { [ "$1" = 200 ] && grep -qi wordpress "$2"; } && [ "$1" != 301 ]' _ "$HC" "$W/h.body"
hreq "$NODNS" /
ok "E42: http://$NODNS/ (serve, chưa phủ) chuyển 301 về https://$SITE/ (chứng chỉ của tên chính hợp lệ)" test "$HC" = 301 -a "$HLOC" = "https://$SITE/"

step "E43. thử thách ACME qua mọi tên (file thử): tên chính, serve, redirect, tên chưa có trong chứng chỉ"
mkdir -p "$AD"
chmod 755 /var/lib/tpan/acme /var/lib/tpan/acme/.well-known "$AD"
printf 'e2e-m19-ok' >"$AD/e2e-m19"
chmod 644 "$AD/e2e-m19"
for h in "$SITE" "$NET" "$WWW" "$NODNS"; do
  hreq "$h" /.well-known/acme-challenge/e2e-m19
  ok "E43: $h: 200 và đúng nội dung file thử" bash -c '[ "$1" = 200 ] && grep -qx e2e-m19-ok "$2"' _ "$HC" "$W/h.body"
done
rm -f /var/lib/tpan/acme/.well-known/acme-challenge/e2e-m19

step "E44. gỡ $NET: chứng chỉ mới không còn $NET; Host $NET rơi vào vhost mặc định"
s1="$(serial_of "$SITE")"
tp site alias rm "$SITE" "$NET" --yes
ok "E44: alias rm thoát 0" test "$RC" -eq 0
cho_roi
echo "  tên trong chứng chỉ: $(san "$SITE" | tr '\n' ' ')"
ok "E44: chứng chỉ không còn $NET" bash -c '! { openssl x509 -in "$1" -noout -ext subjectAltName | tr "," "\n" | sed -n "s/^ *DNS://p" | grep -qx "$2"; }' _ "$(cert_of "$SITE")" "$NET"
ok "E44: chứng chỉ vẫn có $SITE và $WWW" bash -c 'for n in "$2" "$3"; do openssl x509 -in "$1" -noout -ext subjectAltName | tr "," "\n" | sed -n "s/^ *DNS://p" | grep -qx "$n" || exit 1; done' _ "$(cert_of "$SITE")" "$SITE" "$WWW"
ok "E44: chứng chỉ đã được xin lại (serial đổi: $s1 -> $(serial_of "$SITE"))" test "$(serial_of "$SITE")" != "$s1"
hreq "$NET" /
want=000
[ "$WEB" = apache ] && want=403
ok "E44: Host $NET rơi vào vhost mặc định (nginx đóng kết nối: 000; Apache 403; được $HC)" test "$HC" = "$want"
hsreq "$SITE" /readme.html
ok "E44: https://$SITE/ vẫn 200" test "$HC" = 200

step "E53. tpan health: site.alias.dns:$NODNS và ssl.names là CẢNH BÁO, không lỗi"
# ssl.names cảnh báo khi một tên trong chứng chỉ không còn trỏ về máy: bỏ $VN khỏi /etc/hosts (đồ nghề).
cp /etc/hosts "$W/hosts.goc"
sed -i "/ $VN\$/d" /etc/hosts
tpan health --json >"$W/health.json" 2>/dev/null </dev/null
echo "  site.alias.dns:$NODNS: $(hs "$W/health.json" "site.alias.dns:$NODNS")"
echo "  ssl.names:$SITE: $(hs "$W/health.json" "ssl.names:$SITE")"
ok "E53: site.alias.dns:$NODNS là warn" test "$(hs "$W/health.json" "site.alias.dns:$NODNS")" = warn
ok "E53: ssl.names:$SITE là warn" test "$(hs "$W/health.json" "ssl.names:$SITE")" = warn
ok "E53: không phép kiểm site.alias, ssl.names, names.root nào ở mức fail (có: $(hs_fail "$W/health.json"))" test -z "$(hs_fail "$W/health.json")"
cp "$W/hosts.goc" /etc/hosts
