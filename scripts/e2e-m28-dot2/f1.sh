#!/usr/bin/env bash
# e2e M28 đợt 2 nhóm B, F1: UID của site bị dùng lại sau khi xoá hẳn, và dấu
# vết của user cũ (crontab, file ở /tmp, /var/tmp, /dev/shm, tiến trình) sống
# sang site mới nhận lại UID đó. Chạy bằng root trong máy ảo, sau dung-may.sh.
#
# Pha 1 (dùng lại UID): site x1 (admin tạo) để lại một file setuid ở /var/tmp
# (bản sao của `id`, vô hại, bit setuid của chủ), một file ở /dev/shm và một
# crontab; xoá hẳn x1 (site rm, trash purge); tạo site x2.
# Pha 2 (tiến trình sót): site x3 để lại một tiến trình ngủ; xoá hẳn x3.
#
# Tham số:
#   chan (mặc định)  x2 nhận UID khác x1 (lớn hơn), GID bằng UID; sau xoá hẳn
#                    không còn file, crontab, user, nhóm của x1; xoá hẳn x3 xong
#                    và không còn tiến trình của nó; x2 trả 200.
#   lo               bản chưa vá: x2 nhận lại đúng UID của x1 (bộ thử bắt được
#                    lỗ).
. "$(dirname "$0")/chung2.sh"
MODE="${1:-chan}"
can_may
X1=x1.example.com
X2=x2.example.com
X3=x3.example.com
SP=""
cleanup() {
  [ -n "$SP" ] && kill "$SP" 2>/dev/null
  rm -f /var/tmp/m28f1-* /dev/shm/m28f1-* 2>/dev/null
}
trap cleanup EXIT

# user_of <domain>: user Linux của site (chủ thư mục site).
user_of() { stat -c '%U' "/var/www/$1"; }
# xoa_han <domain>: site rm rồi trash purge; in 4 dòng cuối, trả mã của purge.
xoa_han() {
  tpan site rm "$1" --yes >/dev/null 2>>"$W/f1.err" || return 1
  tpan trash purge "$1" --yes >"$W/f1.out" 2>>"$W/f1.err"
  local rc=$?
  tail -4 "$W/f1.out" | cut -c1-200 | sed 's/^/  | /'
  return "$rc"
}

step "F1.0. admin tạo site $X1"
tpan site add "$X1" --email admin@example.com --yes --json >/dev/null 2>"$W/f1.err"
ok "site add $X1 thoát 0" test "$?" = 0
U1="$(user_of "$X1")"
UID1="$(id -u "$U1")"
GID1="$(id -g "$U1")"
echo "  user $U1: uid $UID1, gid $GID1"

step "F1.1. user $U1 để lại dấu vết: file setuid ở /var/tmp, file ở /dev/shm, crontab"
runuser -u "$U1" -- cp /usr/bin/id /var/tmp/m28f1-id
runuser -u "$U1" -- chmod 4755 /var/tmp/m28f1-id
runuser -u "$U1" -- cp /etc/hostname /dev/shm/m28f1-shm
printf '%s\n' '* * * * * true # m28f1' >"$W/f1.cron"
crontab -u "$U1" "$W/f1.cron"
ls -ln /var/tmp/m28f1-id /dev/shm/m28f1-shm | sed 's/^/  /'
ok "đã đặt đủ dấu vết (file setuid, crontab)" bash -c '[ -u /var/tmp/m28f1-id ] && [ -f "/var/spool/cron/crontabs/$1" ]' _ "$U1"

step "F1.2. xoá hẳn $X1"
ok "xoá hẳn $X1 thoát 0" xoa_han "$X1"
echo "  sau xoá hẳn: getent passwd $UID1: '$(getent passwd "$UID1" | cut -d: -f1)'; getent group $GID1: '$(getent group "$GID1" | cut -d: -f1)'"
ls -ln /var/tmp/m28f1-id /dev/shm/m28f1-shm 2>&1 | sed 's/^/  /'

step "F1.3. admin tạo site $X2 (sau xoá hẳn): $MODE"
tpan site add "$X2" --email admin@example.com --yes --json >/dev/null 2>>"$W/f1.err"
ok "site add $X2 thoát 0" test "$?" = 0
U2="$(user_of "$X2")"
UID2="$(id -u "$U2")"
GID2="$(id -g "$U2")"
echo "  user $U2: uid $UID2, gid $GID2"
ls -l /var/tmp/m28f1-id 2>&1 | sed 's/^/  /'
if [ "$MODE" = lo ]; then
  ok "bản chưa vá: $X2 nhận lại đúng UID của $X1 ($UID2 = $UID1; bộ thử bắt được lỗ)" test "$UID2" = "$UID1"
else
  ok "$X2 không nhận lại UID của $X1 ($UID2 khác $UID1, và lớn hơn)" test "$UID2" -gt "$UID1"
  ok "GID của $U2 bằng UID ($GID2)" test "$GID2" = "$UID2"
  ok "không còn file của uid $UID1 ở /var/tmp, /dev/shm" bash -c '[ ! -e /var/tmp/m28f1-id ] && [ ! -e /dev/shm/m28f1-shm ]'
  ok "không còn crontab của $U1" test ! -e "/var/spool/cron/crontabs/$U1"
  ok "không còn user, nhóm của $X1" bash -c '! getent passwd "$1" >/dev/null && ! getent group "$1" >/dev/null' _ "$U1"
fi
ok "$X2 trả 200" web200 "$X2"

step "F1.4. tiến trình sót: site $X3 để lại một tiến trình ngủ, rồi xoá hẳn"
tpan site add "$X3" --email admin@example.com --yes --json >/dev/null 2>>"$W/f1.err"
ok "site add $X3 thoát 0" test "$?" = 0
U3="$(user_of "$X3")"
UID3="$(id -u "$U3")"
runuser -u "$U3" -- sleep 600 </dev/null >/dev/null 2>&1 &
SP=$!
sleep 1
echo "  tiến trình của uid $UID3 trước khi xoá hẳn: $(pgrep -u "$UID3" | wc -l)"
xoa_han "$X3"
rc=$?
echo "  xoá hẳn $X3: mã $rc; tiến trình của uid $UID3 còn: $(pgrep -u "$UID3" | wc -l)"
ok "xoá hẳn $X3 xong dù user còn tiến trình (mã $rc)" test "$rc" = 0
ok "không còn tiến trình của uid $UID3" bash -c '! pgrep -u "$1" >/dev/null' _ "$UID3"
kill "$SP" 2>/dev/null
SP=""
