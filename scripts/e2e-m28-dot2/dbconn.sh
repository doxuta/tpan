#!/usr/bin/env bash
# e2e M28 đợt 2, M1-DBCONN: user DB của site e mở tối đa 160 kết nối song song
# (mỗi kết nối `mariadb ... -e 'SELECT SLEEP(40)'`) trong lúc site b được gọi
# ba lần. Chạy bằng root trong máy ảo, sau dung-may.sh, cả nginx lẫn Apache.
#
# Tên user, mật khẩu, tên DB lấy từ wp-config.php của site e (root đọc trong máy
# ảo test); mật khẩu truyền bằng biến môi trường MYSQL_PWD, không in ra.
#
# Tham số:
#   chan (mặc định)  e không mở được quá 20 kết nối; b trả 200 cả ba lần.
#   lo               bản chưa vá: e mở được quá 20 kết nối (bộ thử bắt được lỗ).
#                    Mã của b trong lúc đó chỉ ghi nhận.
. "$(dirname "$0")/chung2.sh"
MODE="${1:-chan}"
can_may
CFG="/var/www/$DOM/public/wp-config.php"
T="$W/dbconn"
mkdir -p "$T"
PIDS=()

# wpdef <tên hằng>: giá trị define('<tên>', '<giá trị>') của wp-config.php.
wpdef() {
  python3 -I -c '
import re, sys
s = open(sys.argv[1]).read()
m = re.search(r"define\(\s*[\x27\"]" + sys.argv[2] + r"[\x27\"]\s*,\s*[\x27\"]([^\x27\"]*)[\x27\"]", s)
print(m.group(1) if m else "")' "$CFG" "$1"
}

cleanup() {
  local p id
  for p in "${PIDS[@]}"; do kill "$p" 2>/dev/null; done
  wait 2>/dev/null
  # Luồng phía máy chủ của SELECT SLEEP còn sống sau khi client chết: KILL.
  if [[ ${DBU:-} =~ ^[A-Za-z0-9_]+$ ]]; then
    for id in $(mariadb -N -B -e "SELECT id FROM information_schema.PROCESSLIST WHERE USER = '$DBU'" 2>/dev/null); do
      mariadb -e "KILL $id" 2>/dev/null
    done
  fi
  rm -rf "$T"
}
trap cleanup EXIT

DBU="$(wpdef DB_USER)"
DBP="$(wpdef DB_PASSWORD)"
DBN="$(wpdef DB_NAME)"
DBH="$(wpdef DB_HOST)"
DBH="${DBH:-localhost}"
[[ $DBU =~ ^[A-Za-z0-9_]+$ ]] || bail "không đọc được DB_USER của $DOM từ wp-config.php (được '${DBU:0:8}')"
[ -n "$DBP" ] || bail "không đọc được DB_PASSWORD của $DOM"
[[ $DBN =~ ^[A-Za-z0-9_]+$ ]] || bail "không đọc được DB_NAME của $DOM"
echo "  user DB của $DOM: $DBU (mật khẩu có $(printf %s "$DBP" | wc -c) ký tự, không in), DB $DBN, host $DBH"
echo "  max_user_connections của user này: $(mariadb -N -B -e "SELECT max_user_connections FROM mysql.user WHERE User = '$DBU' AND Host = 'localhost'" 2>&1 | tr '\n' ' ')"

step "DBCONN.0. mốc: site b trả 200, tài khoản của e vào được DB ($MODE)"
ok "$OTHER trả 200 trước phép thử" web200 "$OTHER"
# Mật khẩu đi bằng môi trường, không đi bằng argv (ps).
db_vao() { mariadb -h "$DBH" -u "$DBU" -D "$DBN" -N -B -e "SELECT 1" >/dev/null 2>&1; }
MYSQL_PWD="$DBP" ok "tài khoản DB của e kết nối được (thông tin lấy từ wp-config.php đúng)" db_vao

step "DBCONN.1. mở $DB_THU kết nối song song bằng tài khoản của e, giữ 40 giây"
for i in $(seq 1 "$DB_THU"); do
  MYSQL_PWD="$DBP" mariadb -h "$DBH" -u "$DBU" -D "$DBN" -N -B -e 'SELECT SLEEP(40)' >/dev/null 2>"$T/e.$i" </dev/null &
  PIDS+=("$!")
done
sleep 8
echo "  kết nối của $DBU đang mở (root nhìn): $(mariadb -N -B -e "SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE USER = '$DBU'" 2>&1)"
echo "  Threads_connected: $(mariadb -N -B -e "SHOW GLOBAL STATUS LIKE 'Threads_connected'" 2>&1 | tr '\t' ' ')"

step "DBCONN.2. trong lúc đó gọi $OTHER ba lần"
CODES=()
for i in 1 2 3; do
  c="$(curl -s -m 20 -o /dev/null -w '%{http_code}' --resolve "$OTHER:80:127.0.0.1" "http://$OTHER/")"
  CODES+=("$c")
  sleep 1
done
echo "  mã HTTP của $OTHER: ${CODES[*]}"

step "DBCONN.3. đếm kết nối của e thành công (tiến trình mariadb thoát 0)"
THANH=0
for p in "${PIDS[@]}"; do
  wait "$p" 2>/dev/null && THANH=$((THANH + 1))
done
PIDS=()
echo "  thành công: $THANH trên $DB_THU; lỗi của các kết nối bị từ chối:"
cat "$T"/e.* 2>/dev/null | grep -oE 'ERROR [0-9]+ \([0-9A-Z]+\)' | sort | uniq -c | sed 's/^/    /'
if [ "$MODE" = lo ]; then
  ok "bản chưa vá: e mở được hơn $DB_TRAN kết nối ($THANH; bộ thử bắt được lỗ)" test "$THANH" -gt "$DB_TRAN"
  note "bản chưa vá: mã của $OTHER trong lúc đó: ${CODES[*]}"
else
  ok "e mở được ít nhất 1 kết nối (phép thử có nghĩa)" test "$THANH" -ge 1
  ok "e không mở được quá $DB_TRAN kết nối ($THANH)" test "$THANH" -le "$DB_TRAN"
  ok "$OTHER trả 200 cả ba lần trong lúc e mở kết nối (${CODES[*]})" \
    bash -c '[ "$1" = 200 ] && [ "$2" = 200 ] && [ "$3" = 200 ]' _ "${CODES[@]}"
fi

step "DBCONN.4. dọn và kiểm sau phép thử"
for id in $(mariadb -N -B -e "SELECT id FROM information_schema.PROCESSLIST WHERE USER = '$DBU'" 2>/dev/null); do
  mariadb -e "KILL $id" 2>/dev/null
done
sleep 2
ok "không còn kết nối của $DBU" test "$(mariadb -N -B -e "SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE USER = '$DBU'")" = 0
ok "$OTHER trả 200 sau phép thử" web200 "$OTHER"
ok "$DOM trả 200 sau phép thử" web200 "$DOM"

# DBCONN.5 (N3b của soát thiết kế S4, soát cuối D4): đo một lỗ chưa vá nên
# script đo giữ ngoài repo tới khi vá (repo công khai).
