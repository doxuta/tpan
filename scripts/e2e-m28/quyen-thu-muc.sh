#!/usr/bin/env bash
# e2e M28, khẳng định 7 (đặt quyền thư mục). Chạy bằng root trong máy ảo, sau
# dung-may.sh. Theo cách của n3-lenh-root.sh mục find-chmod.
#
# Một thư mục nạn nhân 0755 của root (/srv/m28-victim). User site tráo liên
# tục một thư mục con trong public (public/wp-includes) với symlink tới nạn
# nhân trong lúc admin chạy `tpan site add e.example.com --yes` 30 lần. Nạn
# nhân phải giữ 0755.
#
# CHÚ THÍCH BẮT BUỘC (bản thiết kế mục 7): trên bản CHƯA VÁ, bước chmod
# (find ... -type d -exec chmod 2750) chỉ chạy khi WPFiles giải nén lại
# WordPress, mà `site add` trên site đã có WordPress không giải nén lại. Vì
# vậy khẳng định này có thể ĐẠT cả trước bản vá: nó không phải bằng chứng bắt
# lỗ, chỉ là kiểm hành vi đúng vẫn đúng. Sau bản vá, chmodDirs đi qua cửa nên
# vẫn ĐẠT.
. "$(dirname "$0")/chung.sh"
ROUNDS="${1:-10}"
V=/srv/m28-victim
SUB=$PUB/wp-includes
can_may

dung_tra() {
  pkill -TERM -f '^python3 - m28-tra-doi' 2>/dev/null
  local _
  for _ in $(seq 1 50); do pgrep -f '^python3 - m28-tra-doi' >/dev/null || break; sleep 0.1; done
  pkill -KILL -f '^python3 - m28-tra-doi' 2>/dev/null
}
don() {
  dung_tra
  if [ -L "$SUB" ] && [ -d "$SUB.m28-lien" ]; then as rm -f "$SUB"; as mv "$SUB.m28-lien" "$SUB"; fi
  [ -L "$SUB.m28-lien" ] && as rm -f "$SUB.m28-lien"
  rm -rf "$V"
  tra_lai
}
trap don EXIT

step "7. admin chạy site add $DOM $ROUNDS lần, user site tráo $SUB <-> symlink tới $V (0755 của root)"
rm -rf "$V"
mkdir -p "$V/con"
chmod 0755 "$V" "$V/con"
chown root:root "$V" "$V/con"
echo "  nạn nhân trước: $(stat -c '%n %U:%G %a' "$V" "$V/con" | tr '\n' ' ')"
as mkdir -p "$SUB"
runuser -u "$SU" -- python3 - m28-tra-doi dir "$SUB" "$V" <"$D/tra-doi.py" >"$W/dem-chmod" 2>&1 &
sleep 1
pgrep -f '^python3 - m28-tra-doi' >/dev/null && echo "  kẻ tráo đang chạy (user $SU)" || echo "  kẻ tráo KHÔNG chạy: $(head -c 300 "$W/dem-chmod")"
rc_all=0
for r in $(seq 1 "$ROUNDS"); do
  tpan site add "$DOM" --owner khach1 --email admin@example.com --yes >"$W/add.$r" 2>&1 || rc_all=1
  m="$(stat -c %a "$V" 2>/dev/null)"
  if [ "$m" != 755 ]; then
    echo "  vòng $r: nạn nhân thành $m"
    break
  fi
done
dung_tra
wait 2>/dev/null
tr=$(sed -n 's/^so-lan-trao=//p' "$W/dem-chmod")
echo "  số lần tráo: ${tr:-?}; nạn nhân sau: $(stat -c '%n %U:%G %a' "$V" "$V/con" | tr '\n' ' ')"
ok "thư mục nạn nhân giữ quyền 0755 qua $ROUNDS vòng site add" bash -c '[ "$(stat -c %a "$1")" = 755 ] && [ "$(stat -c %a "$1/con")" = 755 ]' _ "$V"
note "site add lặp lại thoát $([ "$rc_all" = 0 ] && echo "0 mọi lần" || echo "khác 0 ít nhất một lần")"
rm -f "$W"/add.*
