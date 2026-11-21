#!/usr/bin/env bash
# e2e M28, khẳng định 2 (T1, ba mục tiêu): user Linux của site e.example.com
# biến public/.htaccess thành symlink tới một file nó không đọc được, rồi
# khach1 (vai user, chủ site e) xin bản xem trước POST /api/v1/sites?dry_run=1.
# Chạy bằng root trong máy ảo, sau dung-may.sh, stack Apache.
#
# Tham số:
#   chan (mặc định)  khẳng định của bản vá: câu trả lời không chứa file đích,
#                    HTTP 200 có old_link ở mục .htaccess.
#   v020-lo          dùng ở phần nâng cấp trên v0.2.0: khẳng định bộ thử BẮT
#                    ĐƯỢC lỗ (câu trả lời chứa file đích), để "sau khi nâng thì
#                    bị chặn" có nghĩa.
. "$(dirname "$0")/chung.sh"
MODE="${1:-chan}"
can_may
trap tra_lai EXIT
login khach1 /root/m28-khach1.pw || bail "khach1 không đăng nhập được"

for t in "/var/www/$VICTIM/public/wp-config.php" /etc/shadow /var/lib/tpan/tpan.db; do
  step ".htaccess của $DOM -> symlink tới $t"
  as ln -sfn "$t" "$HT"
  p0="$(pid)"
  n0="$(nrestarts)"
  dry "$W/t1.resp" 60
  if [ "$MODE" = v020-lo ]; then
    ok "v0.2.0, symlink -> $t: bản xem trước CHỨA file đích (bộ thử bắt được lỗ T1 trên bản cũ)" \
      bash -c '! python3 "$1" khong-lo "$2" "$3"' _ "$D/phan-tich.py" "$W/t1.resp" "$t"
  else
    ok "symlink -> $t: câu trả lời không chứa file đích" pt khong-lo "$W/t1.resp" "$t"
    ok "symlink -> $t: HTTP 200, mục .htaccess có old_link, không có old" \
      bash -c '[ "$1" = 200 ] && python3 "$2" old-link "$3" "$4"' _ "$HTTP" "$D/phan-tich.py" "$W/t1.resp" "$HT"
  fi
  song "$p0" "$n0" || doi_tpan
done
rm -f "$W/t1.resp"
