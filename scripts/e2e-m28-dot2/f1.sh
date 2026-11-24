#!/usr/bin/env bash
# e2e M28 đợt 2 nhóm B, F1: UID của site bị dùng lại sau khi xoá hẳn, và dấu
# vết của user cũ (crontab, tiến trình, file ở /tmp, /var/tmp, /dev/shm) sống
# sang site mới nhận lại UID đó. Chạy bằng root trong máy ảo, sau dung-may.sh.
#
# Site x1 (admin tạo) để lại: một file setuid ở /var/tmp (bản sao của `id`,
# vô hại, bit setuid của chủ), một file ở /dev/shm, một crontab, một tiến trình
# ngủ. Rồi xoá hẳn x1 (site rm, trash purge) và tạo site x2.
#
# Tham số:
#   chan (mặc định)  x2 nhận UID khác x1 (lớn hơn); sau xoá hẳn không còn file,
#                    crontab, tiến trình, nhóm của user x1; x2 chạy bình thường.
#   lo               bản chưa vá: x2 nhận lại đúng UID của x1 và file setuid sót
#                    lại thành của user x2 (bộ thử bắt được lỗ).
. "$(dirname "$0")/chung2.sh"
MODE="${1:-chan}"
can_may
X1=x1.example.com
X2=x2.example.com
SP=""
cleanup() {
  [ -n "$SP" ] && kill "$SP" 2>/dev/null
  rm -f /var/tmp/m28f1-* /dev/shm/m28f1-* 2>/dev/null
}
trap cleanup EXIT

# user_of <domain>: user Linux của site (chủ thư mục site).
user_of() { stat -c '%U' "/var/www/$1"; }

step "F1.0. admin tạo site $X1"
tpan site add "$X1" --email admin@example.com --yes --json >/dev/null 2>"$W/f1.err"
ok "site add $X1 thoát 0" test "$?" = 0
U1="$(user_of "$X1")"
UID1="$(id -u "$U1")"
GID1="$(id -g "$U1")"
echo "  user $U1: uid $UID1, gid $GID1"
ok "$X1 trả 200" web200 "$X1"

step "F1.1. user $U1 để lại dấu vết: file setuid ở /var/tmp, file ở /dev/shm, crontab, tiến trình"
runuser -u "$U1" -- cp /usr/bin/id /var/tmp/m28f1-id
runuser -u "$U1" -- chmod 4755 /var/tmp/m28f1-id
runuser -u "$U1" -- cp /etc/hostname /dev/shm/m28f1-shm
printf '%s\n' '* * * * * true # m28f1' >"$W/f1.cron"
crontab -u "$U1" "$W/f1.cron"
runuser -u "$U1" -- sleep 900 &
SP=$!
ls -ln /var/tmp/m28f1-id /dev/shm/m28f1-shm | sed 's/^/  /'
echo "  crontab của $U1: $(crontab -l -u "$U1" 2>&1 | tail -1)"
echo "  tiến trình của uid $UID1: $(pgrep -u "$UID1" | wc -l)"
ok "đã đặt đủ dấu vết (file setuid, crontab, tiến trình)" bash -c '[ -u /var/tmp/m28f1-id ] && [ -f "/var/spool/cron/crontabs/$1" ] && pgrep -u "$2" >/dev/null' _ "$U1" "$UID1"

step "F1.2. xoá hẳn $X1 (site rm, rồi trash purge)"
tpan site rm "$X1" --yes >/dev/null 2>>"$W/f1.err"
ok "site rm $X1 thoát 0" test "$?" = 0
tpan trash purge "$X1" --yes >"$W/f1.out" 2>>"$W/f1.err"
rc=$?
tail -4 "$W/f1.out" | cut -c1-200 | sed 's/^/  | /'
ok "trash purge $X1 thoát 0" test "$rc" = 0
wait "$SP" 2>/dev/null
SP=""
echo "  sau xoá hẳn: getent passwd $UID1: $(getent passwd "$UID1" | cut -d: -f1); getent group $GID1: $(getent group "$GID1" | cut -d: -f1)"
ls -ln /var/tmp/m28f1-id /dev/shm/m28f1-shm 2>&1 | sed 's/^/  /'

step "F1.3. admin tạo site $X2 (sau xoá hẳn): $MODE"
tpan site add "$X2" --email admin@example.com --yes --json >/dev/null 2>>"$W/f1.err"
ok "site add $X2 thoát 0" test "$?" = 0
U2="$(user_of "$X2")"
UID2="$(id -u "$U2")"
GID2="$(id -g "$U2")"
echo "  user $U2: uid $UID2, gid $GID2"
if [ "$MODE" = lo ]; then
  ok "bản chưa vá: $X2 nhận lại đúng UID của $X1 ($UID2 = $UID1; bộ thử bắt được lỗ)" test "$UID2" = "$UID1"
else
  ok "$X2 không nhận lại UID của $X1 ($UID2 khác $UID1, và lớn hơn)" test "$UID2" -gt "$UID1"
  ok "GID của $U2 bằng UID ($GID2)" test "$GID2" = "$UID2"
  ok "không còn file của uid $UID1 ở /var/tmp, /dev/shm" bash -c '[ ! -e /var/tmp/m28f1-id ] && [ ! -e /dev/shm/m28f1-shm ]'
  ok "không còn crontab của $U1" test ! -e "/var/spool/cron/crontabs/$U1"
  ok "không còn tiến trình của uid $UID1" bash -c '! pgrep -u "$1" >/dev/null' _ "$UID1"
  ok "không còn user, nhóm của $X1" bash -c '! getent passwd "$1" >/dev/null && ! getent group "$2" >/dev/null' _ "$U1" "$U1"
fi
ok "$X2 trả 200" web200 "$X2"
