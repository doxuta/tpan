#!/usr/bin/env bash
# Đo khoảng hở sau khi reload web server (mục 16, quan sát của M7): chạy TRONG
# máy ảo bằng root, một lần bash cho cả việc đổi vhost, reload và vòng curl,
# để độ trễ của `multipass exec` (khoảng 0,3 s) không che mất khoảng hở.
#
#   reload-measure.sh <nginx|apache> <domain> <off|on> [số giây đo] [tpan]
#
# off: xoá symlink bật vhost của site; on: tạo lại nó. Rồi kiểm cú pháp,
# `systemctl reload`, và gọi một file tĩnh của site liên tục (curl -m 1, không
# nghỉ) trong vài giây. Mỗi dòng: mốc tính từ lúc `systemctl reload` trả về
# (ms), mã HTTP và mã thoát của curl (52: nginx đóng kết nối, tức site mặc
# định 444), và các tiến trình con của tiến trình chính khi danh sách đó đổi.
# Gọi từ Mac: scripts/vm.sh root bash -c "$(cat scripts/e2e-m8/reload-measure.sh)" _ nginx a.example.com off
set -uo pipefail
web=$1 d=$2 dir=$3 secs=${4:-3}
case "$web" in
  nginx) unit=nginx conf=/etc/nginx/sites-available/tpan-$d.conf link=/etc/nginx/sites-enabled/tpan-$d.conf pidf=/run/nginx.pid test=(nginx -t -q) ;;
  apache) unit=apache2 conf=/etc/apache2/sites-available/tpan-$d.conf link=/etc/apache2/sites-enabled/tpan-$d.conf pidf=/run/apache2/apache2.pid test=(apachectl -t) ;;
  *) echo "web lạ: $web" >&2; exit 2 ;;
esac
case "$dir" in
  off) rm -f "$link" ;;
  on) ln -sfn "$conf" "$link" ;;
  *) echo "hướng lạ: $dir" >&2; exit 2 ;;
esac
"${test[@]}" 2>&1 || exit 1
kids() { ps -o pid=,args= --ppid "$(cat "$pidf")" | sed -E 's/^ +//' | tr '\n' ';'; }
echo "trước reload: $(kids)"
# Đối số thứ năm "tpan": reload bằng `tpan internal reload-web` (đường của
# tpan: kiểm cú pháp rồi systemctl reload) thay cho systemctl trực tiếp.
reload=(systemctl reload "$unit")
[ "${5:-}" = tpan ] && reload=(tpan internal reload-web)
t0=$(date +%s%N)
"${reload[@]}"
t1=$(date +%s%N)
echo "${reload[*]} trả về sau $(((t1 - t0) / 1000000)) ms"
last="" prev=""
end=$((t1 + secs * 1000000000))
while :; do
  now=$(date +%s%N)
  [ "$now" -ge "$end" ] && break
  code=$(curl -s -m 1 -o /dev/null -w '%{http_code}' --resolve "$d:80:127.0.0.1" "http://$d/wp-includes/css/dashicons.min.css")
  rc=$?
  ms=$(((now - t1) / 1000000))
  k=$(kids)
  r="$code/$rc"
  if [ "$r" != "$prev" ] || [ "$k" != "$last" ]; then
    echo "+${ms}ms curl $r | $k"
  fi
  prev=$r last=$k
done
echo "kết quả cuối: curl $prev"
