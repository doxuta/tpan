#!/usr/bin/env bash
# e2e M28, phần nginx: đường wp-config.php của khach1. Chạy bằng root trong máy
# ảo, sau dung-may.sh (stack nginx). Chạy CUỐI phần nginx vì đổi mật khẩu DB
# thật của site e; cuối script khôi phục wp-config.php của e từ bản sao và đồng
# bộ lại mật khẩu DB.
#
# 1. wp-config.php của site e là symlink tới wp-config.php của site a:
#    POST /api/v1/databases/e.example.com/password (khach1, chạy thật) KHÔNG
#    đổi mật khẩu DB của site a (site a vẫn 200); job hỏng hoặc bị từ chối.
# 2. wp-config.php của site e là file 2 GiB thưa: gọi đó trả lời trong 30 giây,
#    tpan giữ PID.
. "$(dirname "$0")/chung.sh"
WPE=$PUB/wp-config.php
WPA=/var/www/$VICTIM/public/wp-config.php
can_may
[ "$WEB" = nginx ] || bail "nginx-db.sh chỉ cho stack nginx"

restore_wpe() {
  if [ -f "$W/wpconfig-e.goc" ]; then
    as rm -f "$WPE" 2>/dev/null || rm -f "$WPE"
    cp -a "$W/wpconfig-e.goc" "$WPE"
    chown "$SU:www-data" "$WPE"
    chmod 0640 "$WPE"
  fi
}
don() {
  restore_wpe
  # Đồng bộ lại: đổi mật khẩu DB của site e một lần với wp-config.php thường,
  # để mật khẩu trong DB và trong file khớp sau khi phép 1 có thể đã động vào.
  login khach1 /root/m28-khach1.pw >/dev/null 2>&1 &&
    call khach1 POST "/databases/$DOM/password" "$W/db-sync" 120 >/dev/null 2>&1
  jid="$(pt job-id "$W/db-sync" 2>/dev/null)"
  [ -n "$jid" ] && for i in $(seq 1 60); do
    curl -sk -m 20 -b "$W/jar-khach1" -o "$W/db-sync.get" "$API/jobs/$jid"
    case "$(pt job-status "$W/db-sync.get")" in running | "") sleep 2 ;; *) break ;; esac
  done
  web200 "$DOM" >/dev/null 2>&1 || true
  rm -f "$W/db-sync" "$W/db-sync.get"
}
trap don EXIT
login khach1 /root/m28-khach1.pw || bail "khach1 không đăng nhập được"
[ -f "$WPE" ] && [ ! -L "$WPE" ] && cp -a "$WPE" "$W/wpconfig-e.goc"

# job_wait <file start> <file lưu kết quả cuối>: chờ job qua GET /jobs/{id}.
job_wait() {
  local jid st=""
  jid="$(pt job-id "$1")"
  [ -n "$jid" ] || { echo ""; return; }
  for i in $(seq 1 90); do
    curl -sk -m 30 -b "$W/jar-khach1" -o "$2" "$API/jobs/$jid"
    st="$(pt job-status "$2")"
    case "$st" in running | "") sleep 2 ;; *) break ;; esac
  done
  echo "$st"
}

step "1. wp-config.php của $DOM -> symlink tới wp-config.php của $VICTIM, đổi mật khẩu DB (chạy thật)"
sa0="$(sha256sum "$WPA" | cut -d' ' -f1)"
ok "site $VICTIM trả 200 trước phép thử" web200 "$VICTIM"
as rm -f "$WPE"
as ln -sfn "$WPA" "$WPE"
p0="$(pid)"
n0="$(nrestarts)"
call khach1 POST "/databases/$DOM/password" "$W/db1.start" 120
st="$(job_wait "$W/db1.start" "$W/db1.get")"
echo "  trạng thái job: ${st:-không có job (bị từ chối ngay)}; HTTP lúc gọi $HTTP"
sa1="$(sha256sum "$WPA" | cut -d' ' -f1)"
echo "  wp-config.php của $VICTIM: trước sha=${sa0:0:16}, sau sha=${sa1:0:16}"
ok "wp-config.php của $VICTIM không bị đổi (sha256 giữ nguyên)" test "$sa0" = "$sa1"
ok "site $VICTIM vẫn trả 200" web200 "$VICTIM"
ok "job hỏng hoặc bị từ chối (không succeeded)" bash -c '[ "$1" != succeeded ]' _ "${st:-refused}"
ok "câu trả lời không chứa wp-config.php của $VICTIM" bash -c 'cat "$2" "$3" >"$4" 2>/dev/null; python3 "$1" khong-lo "$4" "$5"' _ "$D/phan-tich.py" "$W/db1.start" "$W/db1.get" "$W/db1.all" "$WPA"
song "$p0" "$n0" || doi_tpan
restore_wpe

step "2. wp-config.php của $DOM là file thường 2 GiB thưa: gọi đổi mật khẩu DB trả lời trong 30 giây"
as rm -f "$WPE"
as truncate -s 2G "$WPE"
as chmod 0640 "$WPE"
stat -c '  %n: %F, %s byte, chủ %U:%G' "$WPE"
p0="$(pid)"
n0="$(nrestarts)"
call khach1 POST "/databases/$DOM/password" "$W/db2.start" 35
ok "gọi đổi mật khẩu DB trả lời trong 30 giây" bash -c '[ "$1" = 0 ] && awk -v t="$2" "BEGIN { exit !(t + 0 <= 30) }"' _ "$CRC" "$TIME"
st="$(job_wait "$W/db2.start" "$W/db2.get")"
echo "  HTTP $HTTP, ${TIME}s; trạng thái job: ${st:-không có}"
ok "tpan giữ PID, NRestarts không tăng" song "$p0" "$n0" || doi_tpan
rm -f "$W"/db1.* "$W"/db2.*
