#!/usr/bin/env bash
# e2e M28, khẳng định 5 biến thể đua FIFO (S1, chỉ Apache). Chạy bằng root
# trong máy ảo, sau dung-may.sh (e2e-m28.sh gọi ngay sau tu-choi.sh).
#
# .htaccess là file thường của user site. Một tiến trình của user đó
# (tra-doi.py, chế độ fifo) liên tục đặt một FIFO đè lên .htaccess (tên tạm rồi
# os.replace) rồi đặt lại file thường. Trong lúc đó khach1 xin bản xem trước 50
# lần, tuần tự: cửa có thể thấy file thường rồi lớp đọc lại thấy FIFO. Đòi:
# lần nào cũng có câu trả lời trong 5 giây (HTTP 200 hay 409 đều được); không
# câu nào chứa /etc/shadow; sau cùng không còn dd, head, timeout nào của user
# site; PID của tpan.service không đổi, NRestarts không tăng.
. "$(dirname "$0")/chung.sh"
N="${1:-50}"
LIM=20
can_may
[ "$WEB" = apache ] || bail "biến thể đua FIFO chỉ cho stack Apache"

dung_tra() {
  pkill -TERM -f '^python3 - m28-tra-doi' 2>/dev/null
  local _
  for _ in $(seq 1 50); do
    pgrep -f '^python3 - m28-tra-doi' >/dev/null || break
    sleep 0.1
  done
  pkill -KILL -f '^python3 - m28-tra-doi' 2>/dev/null
}
don() {
  dung_tra
  as rm -f "$PUB/.m28-tra-doi.tmp" 2>/dev/null
  tra_lai
  rm -rf "$W/tc-fifo"
}
trap don EXIT
login khach1 /root/m28-khach1.pw || bail "khach1 không đăng nhập được"
tra_lai

step "5d. đua FIFO: kẻ tráo đặt FIFO đè lên .htaccess, $N lần xin bản xem trước (tuần tự)"
p0="$(pid)"
n0="$(nrestarts)"
rm -rf "$W/tc-fifo"
mkdir -p "$W/tc-fifo"
runuser -u "$SU" -- python3 - m28-tra-doi fifo "$HT" - <"$D/tra-doi.py" >"$W/dem-fifo" 2>&1 &
sleep 1
pgrep -f '^python3 - m28-tra-doi' >/dev/null && echo "  kẻ tráo đang chạy (user $SU)" || echo "  kẻ tráo KHÔNG chạy: $(head -c 300 "$W/dem-fifo")"
tre=0
sai_ma=0
for i in $(seq 1 "$N"); do
  call khach1 POST "/sites?dry_run=1" "$W/tc-fifo/resp.$i" "$LIM" "$BODY" >/dev/null
  echo "$i $HTTP" >>"$W/tc-fifo/codes.1"
  [ "$CRC" = 0 ] && le "$TIME" 5 || tre=$((tre + 1))
  case "$HTTP" in 200 | 409) ;; *) sai_ma=$((sai_ma + 1)) ;; esac
done
dung_tra
wait 2>/dev/null
tr=$(sed -n 's/^so-lan-trao=//p' "$W/dem-fifo")
echo "  số lần tráo: ${tr:-?} $(grep -v '^so-lan-trao=' "$W/dem-fifo" | head -c 300)"
echo "  $N lần gọi: $tre lần quá 5 giây hay không có câu trả lời, $sai_ma lần mã HTTP khác 200 và 409"
sleep 1
ok "5d: kẻ tráo đã tráo hơn $N lần trong lúc gọi (${tr:-0} lần)" test "${tr:-0}" -gt "$N"
ok "5d: đủ $N câu trả lời, lần nào cũng trong 5 giây" test "$tre" -eq 0 -a "$(wc -l <"$W/tc-fifo/codes.1")" -eq "$N"
ok "5d: mã HTTP chỉ là 200 hoặc 409" test "$sai_ma" -eq 0
ok "5d: không câu trả lời nào chứa /etc/shadow" pt dua "$W/tc-fifo" /etc/shadow
for prog in dd head timeout; do
  ok "5d: không còn tiến trình $prog nào của user $SU" bash -c '! pgrep -u "$1" -x "$2" >/dev/null' _ "$SU" "$prog"
done
ok "5d: PID của tpan.service không đổi, NRestarts không tăng" song "$p0" "$n0" || doi_tpan
