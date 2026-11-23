#!/usr/bin/env bash
# e2e M28, khẳng định 4 (đua, chỉ Apache). Chạy bằng root trong máy ảo, sau
# dung-may.sh.
#
# 4a. Một tiến trình python3 chạy bằng user site (tra-doi.py) tráo liên tục
#     public/.htaccess giữa bản sao file thường và symlink tới /etc/shadow (tên
#     tạm rồi os.replace). Trong lúc đó khach1 xin bản xem trước 1000 lần, 4
#     curl song song. Không lần nào được chứa /etc/shadow.
# 4b. Như trên nhưng tráo cả thư mục public với symlink tới một thư mục nạn
#     nhân chỉ root đọc được (renameat2 RENAME_EXCHANGE), 300 lần.
#
# Mọi câu trả lời lưu thành file rồi phân tích một lần bằng phan-tich.py dua
# (chỉ in số đếm). Số lần tráo phải lớn hơn số lần gọi: kẻ tráo thật sự chạy
# suốt lúc gọi.
. "$(dirname "$0")/chung.sh"
N_FILE="${1:-1000}"
N_DIR="${2:-300}"
JOBS=4
V=/srv/m28/nan-nhan
can_may

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
  if [ -L "$PUB" ] && [ -d "$PUB.m28-lien" ]; then
    # Kẻ tráo bị giết giữa chừng: public thật đang nằm ở tên kia.
    as rm -f "$PUB"
    as mv "$PUB.m28-lien" "$PUB"
  fi
  [ -L "$PUB.m28-lien" ] && as rm -f "$PUB.m28-lien"
  as rm -f "$PUB/.m28-tra-doi.tmp" 2>/dev/null
  tra_lai
  rm -rf /srv/m28
}
trap don EXIT
[ "$WEB" = apache ] || bail "khẳng định 4 chỉ cho stack Apache"
login khach1 /root/m28-khach1.pw || bail "khach1 không đăng nhập được"
tra_lai

# goi <thư mục> <số lần>: JOBS curl song song, mỗi câu trả lời một file.
goi() {
  local dir=$1 n=$2 k pids=()
  rm -rf "$dir"
  mkdir -p "$dir"
  for k in $(seq 1 "$JOBS"); do
    (
      for i in $(seq "$k" "$JOBS" "$n"); do
        curl -sk -m 30 -b "$W/jar-khach1" -o "$dir/resp.$i" -w "$i %{http_code}\n" -X POST "$API/sites?dry_run=1" \
          -H 'Content-Type: application/json' --data "$BODY" >>"$dir/codes.$k"
      done
    ) &
    pids+=($!)
  done
  wait "${pids[@]}"
}

step "4a. tráo .htaccess <-> symlink tới /etc/shadow, $N_FILE lần xin bản xem trước ($JOBS song song)"
p0="$(pid)"
n0="$(nrestarts)"
runuser -u "$SU" -- python3 - m28-tra-doi file "$HT" /etc/shadow <"$D/tra-doi.py" >"$W/dem-file" 2>&1 &
sleep 1
pgrep -f '^python3 - m28-tra-doi' >/dev/null && echo "  kẻ tráo đang chạy (user $SU)" || echo "  kẻ tráo KHÔNG chạy: $(head -c 300 "$W/dem-file")"
t0=$(date +%s)
goi "$W/dua-file" "$N_FILE"
echo "  $N_FILE lần gọi xong trong $(($(date +%s) - t0)) giây"
dung_tra
wait 2>/dev/null
tr=$(sed -n 's/^so-lan-trao=//p' "$W/dem-file")
echo "  số lần tráo: ${tr:-?} $(grep -v '^so-lan-trao=' "$W/dem-file" | head -c 300)"
stat -c '  sau khi dừng: %n là %F' "$HT"
ok "4a: kẻ tráo đã tráo hơn $N_FILE lần trong lúc gọi (${tr:-0} lần)" test "${tr:-0}" -gt "$N_FILE"
ok "4a: đủ $N_FILE câu trả lời" test "$(cat "$W"/dua-file/codes.* | wc -l)" -eq "$N_FILE"
ok "4a: không câu trả lời nào trong $N_FILE câu chứa /etc/shadow" pt dua "$W/dua-file" /etc/shadow
ok "4a: tpan còn sống, PID không đổi" song "$p0" "$n0" || doi_tpan
tra_lai

step "4b. tráo public <-> symlink tới thư mục nạn nhân, $N_DIR lần xin bản xem trước"
mkdir -p "$V"
chmod 700 /srv/m28 "$V"
head -c 600 /dev/urandom | base64 >"$V/.htaccess"
cp "/var/www/$VICTIM/public/wp-config.php" "$V/wp-config.php"
p0="$(pid)"
n0="$(nrestarts)"
runuser -u "$SU" -- python3 - m28-tra-doi dir "$PUB" "$V" <"$D/tra-doi.py" >"$W/dem-dir" 2>&1 &
sleep 1
pgrep -f '^python3 - m28-tra-doi' >/dev/null && echo "  kẻ tráo đang chạy (user $SU)" || echo "  kẻ tráo KHÔNG chạy: $(head -c 300 "$W/dem-dir")"
t0=$(date +%s)
goi "$W/dua-dir" "$N_DIR"
echo "  $N_DIR lần gọi xong trong $(($(date +%s) - t0)) giây"
dung_tra
wait 2>/dev/null
tr=$(sed -n 's/^so-lan-trao=//p' "$W/dem-dir")
echo "  số lần tráo: ${tr:-?} $(grep -v '^so-lan-trao=' "$W/dem-dir" | head -c 300)"
stat -c '  sau khi dừng: %n là %F' "$PUB"
ok "4b: kẻ tráo đã tráo hơn $N_DIR lần trong lúc gọi (${tr:-0} lần)" test "${tr:-0}" -gt "$N_DIR"
ok "4b: đủ $N_DIR câu trả lời" test "$(cat "$W"/dua-dir/codes.* | wc -l)" -eq "$N_DIR"
ok "4b: không câu trả lời nào trong $N_DIR câu chứa file của thư mục nạn nhân" pt dua "$W/dua-dir" "$V/.htaccess" "$V/wp-config.php"
ok "4b: tpan còn sống, PID không đổi" song "$p0" "$n0" || doi_tpan
rm -rf "$W/dua-file" "$W/dua-dir"
