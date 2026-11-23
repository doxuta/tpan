#!/usr/bin/env bash
# e2e M28, khẳng định 6 (chạy thật, chỉ Apache). Chạy bằng root trong máy ảo,
# sau dung-may.sh. public/.htaccess của site e là symlink tới /etc/shadow;
# khach1 gọi POST /api/v1/sites (không dry_run, cùng body với bản xem trước)
# rồi chờ job qua GET /api/v1/jobs/{id}.
#
# Phải: job succeeded; .htaccess thành file thường của user site; sha256 của
# /etc/shadow không đổi; không câu trả lời nào của GET /jobs/{id} (lưu hết lại)
# chứa dấu của /etc/shadow (thô và base64). Câu trả lời job có thể mang mật
# khẩu (secret, db_secret): không bao giờ in, chỉ phân tích trong máy ảo.
. "$(dirname "$0")/chung.sh"
can_may
trap 'rm -rf "$W/job"; tra_lai' EXIT
[ "$WEB" = apache ] || bail "khẳng định 6 chỉ cho stack Apache"
login khach1 /root/m28-khach1.pw || bail "khach1 không đăng nhập được"
rm -rf "$W/job"
mkdir -p "$W/job"

step "6. .htaccess -> symlink tới /etc/shadow, khach1 chạy thật POST /sites"
as ln -sfn /etc/shadow "$HT"
s0="$(sha256sum /etc/shadow | cut -d' ' -f1)"
echo "  /etc/shadow trước: $(stat -c '%s byte' /etc/shadow) sha=${s0:0:16}"
call khach1 POST /sites "$W/job/start" 120 "$BODY"
ok "POST /sites (chạy thật) nhận job: HTTP 202" test "$HTTP" = 202
jid="$(pt job-id "$W/job/start")"
echo "  job_id có ${#jid} ký tự"
st=""
if [ -n "$jid" ]; then
  for i in $(seq 1 300); do
    curl -sk -m 30 -b "$W/jar-khach1" -o "$W/job/get.$i" "$API/jobs/$jid"
    st="$(pt job-status "$W/job/get.$i")"
    case "$st" in running | "") sleep 2 ;; *) break ;; esac
  done
  echo "  $(ls "$W/job" | grep -c '^get\.') lần GET /jobs/{id}; trạng thái cuối: ${st:-?}"
fi
ok "job thành công (status succeeded)" test "$st" = succeeded
stat -c '  sau job: %n là %F, chủ %U:%G, quyền %a, %s byte' "$HT"
ok ".htaccess thành file thường của user site" bash -c '[ ! -L "$1" ] && [ "$(stat -c "%F %U" "$1")" = "regular file $2" ]' _ "$HT" "$SU"
s1="$(sha256sum /etc/shadow | cut -d' ' -f1)"
echo "  /etc/shadow sau: sha=${s1:0:16}"
ok "sha256 của /etc/shadow không đổi" test "$s0" = "$s1"
cat "$W"/job/start "$W"/job/get.* >"$W/job/all" 2>/dev/null
ok "toàn bộ câu trả lời của POST /sites và GET /jobs/{id} không chứa /etc/shadow (thô và base64)" pt khong-lo "$W/job/all" /etc/shadow
