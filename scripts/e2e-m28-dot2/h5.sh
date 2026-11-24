#!/usr/bin/env bash
# e2e M28 đợt 2, H5 (chỉ Apache): mod_status. Mã chạy dưới user Linux của site e
# (K1) gọi http://127.0.0.1/server-status từ chính máy, trong lúc có request tới
# site b mang chuỗi đánh dấu ngẫu nhiên. Chạy bằng root trong máy ảo, sau dung-may.sh.
#
# Tham số:
#   chan (mặc định)  câu trả lời không chứa chuỗi đánh dấu và mã khác 200.
#   lo               bản chưa vá: mã 200 (bộ thử bắt được lỗ). Chuỗi đánh dấu có
#                    hiện trong bảng hay không chỉ ghi nhận (?auto không liệt kê
#                    URL; bảng HTML chỉ liệt kê request còn đang chạy).
. "$(dirname "$0")/chung2.sh"
MODE="${1:-chan}"
can_may
[ "$WEB" = apache ] || bail "H5 chỉ cho stack Apache"
RND="$(rnd)"
MARK="m28marker-$RND"
STOP="$W/h5.stop"
rm -f "$STOP"
cleanup() {
  : >"$STOP"
  wait 2>/dev/null
  rm -f "$STOP" "$W/h5.raw"
}
trap cleanup EXIT

step "H5.0. mod_status trong máy ($MODE)"
a2query -m status 2>&1 | sed 's/^/  /'

step "H5.1. vòng lặp request tới $OTHER mang chuỗi đánh dấu, rồi user $SU gọi /server-status"
# 6 vòng song song để luôn có request đang chạy khi user của e hỏi.
for _ in 1 2 3 4 5 6; do
  (
    while [ ! -e "$STOP" ]; do
      curl -s -m 5 -o /dev/null --resolve "$OTHER:80:127.0.0.1" "http://$OTHER/?m28marker=$MARK"
    done
  ) &
done
sleep 1
for u in 'server-status?auto' 'server-status'; do
  runuser -u "$SU" -- curl -s -m 10 -w '\n%{http_code}' "http://127.0.0.1/$u" >"$W/h5.raw" 2>/dev/null
  code="$(tail -n 1 "$W/h5.raw")"
  echo "  user $SU gọi 127.0.0.1/$u: HTTP $code, $(wc -c <"$W/h5.raw") byte"
  if [ "$MODE" = lo ]; then
    ok "bản chưa vá, /$u: HTTP 200 cho user của site (bộ thử bắt được lỗ)" test "$code" = 200
    co "$W/h5.raw" "$MARK" && note "/$u có chuỗi đánh dấu của $OTHER" || note "/$u không có chuỗi đánh dấu (lúc này không có request đang chạy hay ?auto)"
  else
    ok "/$u: câu trả lời không chứa chuỗi đánh dấu của $OTHER" khong_co "$W/h5.raw" "$MARK"
    # Mã phải là mã HTTP thật (000 là không nối được: phép thử vô nghĩa).
    ok "/$u: mã khác 200 (HTTP $code)" bash -c '[[ $1 =~ ^[0-9]{3}$ && $1 != 000 && $1 != 200 ]]' _ "$code"
  fi
done
: >"$STOP"
wait
ok "$OTHER trả 200 sau phép thử" web200 "$OTHER"
