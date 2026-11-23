#!/usr/bin/env bash
# e2e M28, khẳng định 5 (từ chối phục vụ, chỉ Apache). Chạy bằng root trong
# máy ảo, sau dung-may.sh. public/.htaccess của site e lần lượt là: file
# thường 2 GiB (thưa) đúng chủ và quyền, FIFO, symlink tới /dev/zero. Mỗi lần
# khach1 xin bản xem trước phải được trả lời trong 5 giây và tpan giữ PID,
# NRestarts không tăng. File 2 GiB và FIFO: HTTP 409, JSON có error.code.
# /dev/zero: user của site chỉ đặt được nó bằng symlink (mknod bị kernel cấm),
# và bản thiết kế (mục 3.5) nói symlink ở chính tên file thì bản xem trước
# trả 200, ghi đích vào old_link, không đọc đích: khẳng định đúng như vậy.
# Cách hiểu "trả lỗi đọc được" của TU-CHAY-M28: một câu trả lời JSON đọc
# được trong hạn, không sập, không treo (ghi ở docs/thay-doi/M28.md).
#
# Trên bản chưa vá: file 2 GiB và /dev/zero làm tpan chết (kernel giết vì hết
# RAM), FIFO làm request treo. /dev/zero chạy cuối. Sau mỗi phép, tpan chết thì
# chờ systemd khởi động lại rồi mới sang phép sau; FIFO được mở ghi một lần
# bằng user site để lời đọc đang treo của tpan (nếu có) kết thúc.
. "$(dirname "$0")/chung.sh"
LIM=20
can_may
trap tra_lai EXIT
[ "$WEB" = apache ] || bail "khẳng định 5 chỉ cho stack Apache"
login khach1 /root/m28-khach1.pw || bail "khach1 không đăng nhập được"
p_dau="$(pid)"
n_dau="$(nrestarts)"

thu() { # thu <tên>: gọi bản xem trước và khẳng định
  local name=$1 p0 n0
  p0="$(pid)"
  n0="$(nrestarts)"
  dry "$W/tc.resp" "$LIM"
  ok "$name: trả lời trong 5 giây" bash -c '[ "$1" = 0 ] && awk -v t="$2" "BEGIN { exit !(t + 0 <= 5) }"' _ "$CRC" "$TIME"
  if [ "$name" = /dev/zero ]; then
    ok "$name: HTTP 200, mục .htaccess có old_link, không có old (không đọc đích)" \
      bash -c '[ "$1" = 200 ] && python3 "$2" old-link "$3" "$4"' _ "$HTTP" "$D/phan-tich.py" "$W/tc.resp" "$HT"
  else
    ok "$name: HTTP 409, JSON có error.code" bash -c '[ "$1" = 409 ] && python3 "$2" loi "$3" "*"' _ "$HTTP" "$D/phan-tich.py" "$W/tc.resp"
  fi
  [ "$name" = FIFO ] && { as timeout 3 bash -c 'exec 3>"$1"' _ "$HT" 2>/dev/null; sleep 1; }
  sleep 1
  ok "$name: PID của tpan.service không đổi, NRestarts không tăng" song "$p0" "$n0" || doi_tpan
}

step "5a. .htaccess là file thường 2 GiB (thưa), đúng chủ và quyền"
as rm -f "$HT"
as truncate -s 2G "$HT"
as chmod 0640 "$HT"
stat -c '  %n: %F, %s byte, chủ %U:%G, quyền %a' "$HT"
thu "file 2 GiB"
tra_lai

step "5b. .htaccess là FIFO"
as rm -f "$HT"
as mkfifo -m 0640 "$HT"
stat -c '  %n: %F, chủ %U:%G, quyền %a' "$HT"
thu FIFO
tra_lai

step "5c. .htaccess -> symlink tới /dev/zero"
as ln -sfn /dev/zero "$HT"
thu "/dev/zero"
journalctl -k --since '-2 min' --no-pager 2>/dev/null | grep -iE 'out of memory|oom-kill' | sed 's/^/  kernel: /' | tail -3
tra_lai

step "tổng qua cả ba"
ok "qua cả ba phép: PID của tpan.service không đổi, NRestarts không tăng" song "$p_dau" "$n_dau" || doi_tpan
rm -f "$W/tc.resp"
