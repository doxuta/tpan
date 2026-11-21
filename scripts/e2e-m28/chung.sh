# shellcheck shell=bash
# Phần dùng chung của các script e2e M28 chạy TRONG máy ảo bằng root. Mỗi
# script nạp file này bằng: . "$(dirname "$0")/chung.sh"
#
# Người tấn công luôn là user Linux của site e.example.com (đặt file bằng
# `as ...`, tức runuser -u <user đó> --) cộng phiên API của khach1 (vai user,
# chủ site e). Nạn nhân: a.example.com (site của admin), /etc/shadow,
# /var/lib/tpan/tpan.db.
#
# Không in nội dung file, mật khẩu hay cookie: mật khẩu nằm ở /root/m28-*.pw
# (0600, do dung-may.sh ghi), cookie ở /root/m28-e2e/jar-* (0600); mọi phân
# tích câu trả lời đi qua phan-tich.py (chỉ in cỡ, sha256 rút gọn, kết luận).
set -u
umask 077
D="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DOM=e.example.com
VICTIM=a.example.com
OTHER=b.example.com
API=https://127.0.0.1:8443/api/v1
PUB=/var/www/$DOM/public
HT=$PUB/.htaccess
W=/root/m28-e2e
mkdir -p "$W"
chmod 700 "$W"
SU=""
[ -d "/var/www/$DOM" ] && SU="$(stat -c '%U' "/var/www/$DOM")"
BODY="{\"domain\":\"$DOM\",\"email\":\"admin@example.com\"}"
HTTP=""
TIME=""
CRC=0

ok() {
  local d=$1
  shift
  if "$@"; then
    echo "  => ĐẠT: $d"
  else
    echo "  => KHÔNG ĐẠT: $d"
    return 1
  fi
}
# bail: điều kiện đầu của cả nhóm hỏng; in một khẳng định không đạt rồi dừng.
bail() {
  echo "  => KHÔNG ĐẠT: DỪNG NHÓM: $*"
  exit 2
}
note() { echo "  -- GHI NHẬN: $*"; }
step() { printf '\n--- [%s] %s\n' "$(date '+%H:%M:%S')" "$*"; }
as() { runuser -u "$SU" -- "$@"; }
pt() { python3 "$D/phan-tich.py" "$@"; }
pid() { systemctl show -p MainPID --value tpan; }
nrestarts() { systemctl show -p NRestarts --value tpan; }
sha() { sha256sum "$1" | cut -c1-16; }
# le <số thực> <số thực>: $1 <= $2.
le() { awk -v a="$1" -v b="$2" 'BEGIN { exit !(a + 0 <= b + 0) }'; }

# doi_tpan: chờ tpan active và trả lời /health (tối đa 90 giây).
doi_tpan() {
  local _
  for _ in $(seq 1 90); do
    [ "$(systemctl is-active tpan)" = active ] && curl -sk -m 3 -o /dev/null "$API/health" && return 0
    sleep 1
  done
  return 1
}

# login <tên> <file mật khẩu>: đăng nhập, cookie vào $W/jar-<tên>. In mã HTTP.
login() {
  local name=$1 pwf=$2
  python3 -c 'import json,sys; print(json.dumps({"name": sys.argv[1], "password": open(sys.argv[2]).read().strip()}))' \
    "$name" "$pwf" >"$W/login.json"
  : >"$W/jar-$name"
  HTTP="$(curl -sk -m 30 -c "$W/jar-$name" -o /dev/null -w '%{http_code}' -X POST "$API/login" \
    -H 'Content-Type: application/json' --data @"$W/login.json")"
  rm -f "$W/login.json"
  echo "  đăng nhập $name: HTTP $HTTP"
  [ "$HTTP" = 204 ]
}

# call <tên phiên> <METHOD> <đường dẫn sau /api/v1> <file câu trả lời> <hạn giây> [thân JSON]
# Đặt HTTP, TIME, CRC (mã thoát của curl; 28 là quá hạn). In một dòng tóm tắt.
call() {
  local who=$1 m=$2 p=$3 out=$4 lim=$5 body=${6-} res
  : >"$out"
  local args=(-sk -m "$lim" -b "$W/jar-$who" -o "$out" -w '%{http_code} %{time_total}' -X "$m" "$API$p")
  [ -n "$body" ] && args+=(-H 'Content-Type: application/json' --data "$body")
  res="$(curl "${args[@]}")"
  CRC=$?
  HTTP="${res%% *}"
  TIME="${res##* }"
  echo "  $who $m $p: HTTP ${HTTP:-000}, ${TIME:-?}s (curl thoát $CRC; 28 là quá hạn ${lim}s), câu trả lời $(wc -c <"$out") byte"
}

# dry <file câu trả lời> [hạn]: khach1 xin bản xem trước của site add e.example.com.
dry() { call khach1 POST "/sites?dry_run=1" "$1" "${2:-60}" "$BODY"; }

# tra_lai: .htaccess của site e về bản gốc (file thường, chủ user site, 0640).
# Stack nginx không có .htaccess (không có bản gốc): chỉ xoá thứ đang ở đó.
tra_lai() {
  as rm -rf "$HT" 2>/dev/null || rm -rf "$HT"
  [ -f "$W/htaccess.goc" ] || return 0
  cp -a "$W/htaccess.goc" "$HT"
  chown "$SU:www-data" "$HT"
  chmod 0640 "$HT"
}

# song <PID trước> <NRestarts trước>: tpan còn đúng tiến trình đó.
song() {
  local p n
  p="$(pid)"
  n="$(nrestarts)"
  echo "  tpan: PID $1 -> ${p:-không còn} ($(systemctl is-active tpan)), NRestarts $2 -> $n"
  [ "$p" = "$1" ] && [ "$n" = "$2" ] && [ "$(systemctl is-active tpan)" = active ]
}

# web200 <domain>: trang chủ của site trả 200 (gọi qua 127.0.0.1).
web200() {
  local c
  c="$(curl -s -m 30 -o /dev/null -w '%{http_code}' --resolve "$1:80:127.0.0.1" "http://$1/")"
  echo "  $1: HTTP $c"
  [ "$c" = 200 ]
}

# can_may: dừng nhóm khi máy chưa dựng (chưa có site e, chưa có mật khẩu).
can_may() {
  [ -n "$SU" ] || bail "chưa có site $DOM (chạy dung-may.sh trước)"
  [ -s /root/m28-khach1.pw ] || bail "chưa có /root/m28-khach1.pw"
}

# WEB: stack đang chạy (apache | nginx).
WEB=nginx
systemctl is-active --quiet apache2 && WEB=apache
