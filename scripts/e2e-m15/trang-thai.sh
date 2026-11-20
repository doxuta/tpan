#!/usr/bin/env bash
# ĐỒ NGHỀ TEST của scripts/e2e-m15.sh, chỉ chạy TRONG máy ảo test bằng root.
# In một ảnh chụp những thứ một lượt cập nhật tpan có thể đổi, mỗi thứ một dòng
# "tên=giá trị", để e2e so trước và sau. Chỉ đọc máy.
#
#   trang-thai.sh            ảnh chụp
#   trang-thai.sh cho [N]    trước đó chờ đơn vị tạm tpan-self-update chạy xong
#                            (tối đa N giây, mặc định 90)
set -uo pipefail

BIN=/usr/local/bin/tpan
UNIT=tpan-self-update
DIR="$(dirname "$0")"

sha() { if [ -e "$1" ]; then sha256sum "$1" | cut -d' ' -f1; else echo "(không có)"; fi; }

if [ "${1:-}" = cho ]; then
  max="${2:-90}"
  i=0
  while [ "$i" -lt "$max" ]; do
    case "$(systemctl is-active "$UNIT" 2>/dev/null)" in
      active | activating | deactivating) ;;
      *) break ;;
    esac
    sleep 1
    i=$((i + 1))
  done
  if [ "$i" -ge "$max" ]; then
    echo "cho-don-vi=QUÁ HẠN: sau $max giây đơn vị $UNIT vẫn chạy"
  else
    echo "cho-don-vi=xong sau $i giây"
  fi
fi

echo "luc=$(date '+%Y-%m-%d %H:%M:%S')"
echo "phien-ban=$("$BIN" version 2>&1 | head -1)"
echo "sha-tpan=$(sha "$BIN")"
echo "sha-old=$(sha "$BIN.old")"
echo "sha-new=$(sha "$BIN.new")"
echo "MainPID=$(systemctl show tpan -p MainPID --value)"
echo "ActiveState=$(systemctl show tpan -p ActiveState --value)"
echo "NRestarts=$(systemctl show tpan -p NRestarts --value)"
echo "don-vi-tam=$(systemctl is-active "$UNIT" 2>/dev/null)"
# Lần gần nhất tpan.service rời trạng thái active rồi active lại (đồng hồ đơn
# điệu của systemd, micro giây): ước lượng thời gian panel không phục vụ.
x="$(systemctl show tpan -p ActiveExitTimestampMonotonic --value)"
n="$(systemctl show tpan -p ActiveEnterTimestampMonotonic --value)"
if [ "${x:-0}" -gt 0 ] && [ "${n:-0}" -ge "$x" ]; then
  echo "khong-active-ms=$(((n - x) / 1000))"
else
  echo "khong-active-ms=(chưa restart lần nào)"
fi
# Cùng lần restart đó theo journal của tpan.service: từ dòng "Stopping
# tpan.service" cuối cùng tới dòng "tpan đang chạy" kế sau nó (tiến trình mới
# đã mở cổng nghe). Đây là khoảng panel thật sự không nhận kết nối.
journalctl -u tpan -o short-unix --no-pager -n 400 2>/dev/null | awk '
  / Stopping tpan\.service/ { s = $1; l = "" }
  /tpan đang chạy/ { if (s != "" && l == "") l = $1 }
  END {
    if (s != "" && l != "") printf "tat-den-nghe-ms=%d\n", (l - s) * 1000
    else print "tat-den-nghe-ms=(chưa restart lần nào)"
  }'
python3 "$DIR/db.py" get update.self_url
python3 "$DIR/db.py" get update.self_last
