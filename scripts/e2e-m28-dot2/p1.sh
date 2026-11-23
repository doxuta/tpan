#!/usr/bin/env bash
# e2e M28 đợt 2, P1: khoá đăng nhập "sai 5 lần thì khoá". Chạy bằng root trong
# máy ảo, sau dung-may.sh (cần mật khẩu admin và khach1). Khẳng định của bản vá:
# trên bản chưa vá các khẳng định về lỗ đỏ.
#
# Sau bản vá, kết nối loopback được đếm theo UID của tiến trình gọi (không theo
# địa chỉ 127.x, vì PHP của site đổi được địa chỉ nguồn trong cả dải). Vì vậy
# mỗi phép thử chạy dưới một user Linux riêng, và request của chính script (root,
# uid 0, qua chung.sh) không bị khoá lây:
#   P1a: user Linux của site e (K1), mỗi lần đổi một địa chỉ nguồn 127.0.0.x.
#   P1b, P1c: hai user thử tạo tạm m28p1b, m28p1c.
# Dọn: `tpan admin unlock uid:<n>` (lệnh mới của bản vá) và xoá thẳng dòng
# login_failures (cho bản chưa vá).
#
# Không in mật khẩu: lần sai dùng chuỗi vô nghĩa, lần đúng đọc từ file 0600 rồi
# đưa qua stdin của curl.
. "$(dirname "$0")/chung2.sh"
can_may
[ -s /root/m28-admin.pw ] || bail "chưa có /root/m28-admin.pw"
RND="$(rnd)"
CT='Content-Type: application/json'
p0="$(pid)"
n0="$(nrestarts)"
TU=(m28p1b m28p1c)

json_dung() {
  python3 -I -c 'import json,sys; print(json.dumps({"name": sys.argv[1], "password": open(sys.argv[2]).read().strip()}))' "$1" "$2" >"$W/p1-$1.json"
  chmod 600 "$W/p1-$1.json"
}
json_dung admin /root/m28-admin.pw
json_dung khach1 /root/m28-khach1.pw

for u in "${TU[@]}"; do
  id -u "$u" >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin "$u"
done
UIDS=("$(id -u "$SU")" "$(id -u m28p1b)" "$(id -u m28p1c)")
KEYS=()
for u in "${UIDS[@]}"; do KEYS+=("uid:$u"); done
for n in $(seq 30 99); do KEYS+=("127.0.0.$n"); done

# mo_khoa: gỡ khoá của các khoá nguồn thử (bản vá có lệnh; bản chưa vá thì xoá dòng).
mo_khoa() {
  local k
  for k in "${KEYS[@]:0:3}"; do tpan admin unlock "$k" >/dev/null 2>&1 || true; done
  xoa_khoa_ip "${KEYS[@]}" >/dev/null
}
cleanup() {
  mo_khoa
  for u in "${TU[@]}"; do userdel "$u" >/dev/null 2>&1 || true; done
  rm -f "$W/p1-admin.json" "$W/p1-khach1.json" "$W"/p1-par.*
}
trap cleanup EXIT
mo_khoa

# sai <user> <nguồn> <tên> <nhãn>: một lần đăng nhập sai dưới user đó, in mã HTTP.
sai() {
  runuser -u "$1" -- curl -sk -m 60 -o /dev/null -w '%{http_code}' --interface "$2" -H "$CT" \
    -d "{\"name\":\"$3\",\"password\":\"sai-$4-$RND\"}" "$PANEL$LOGIN_PATH"
}
# dung <user> <nguồn> <tên>: một lần đăng nhập đúng (mật khẩu qua stdin), in mã HTTP.
dung() {
  runuser -u "$1" -- curl -sk -m 60 -o /dev/null -w '%{http_code}' --interface "$2" -H "$CT" \
    --data @- "$PANEL$LOGIN_PATH" <"$W/p1-$3.json"
}

step "P1a. K1 (user Linux $SU của site e): 20 lần sai mật khẩu admin, mỗi lần một nguồn 127.0.0.x, cứ 4 lần sai xen 1 lần đăng nhập đúng của khach1"
first423=0
n401=0
sau_khoa_401=0
log=""
for n in $(seq 1 20); do
  c="$(sai "$SU" "127.0.0.$((40 + n))" admin "a$n")"
  log="$log $n:$c"
  [ "$c" = 401 ] && n401=$((n401 + 1))
  [ "$c" = 423 ] && [ "$first423" = 0 ] && first423="$n"
  [ "$first423" != 0 ] && [ "$c" = 401 ] && sau_khoa_401=$((sau_khoa_401 + 1))
  if [ $((n % 4)) = 0 ]; then
    k="$(dung "$SU" "127.0.0.$((70 + n))" khach1)"
    log="$log (khach1 đúng:$k)"
  fi
done
echo "  lần:mã =$log"
echo "  số 401: $n401; lần sai đầu tiên bị 423: $first423 (0 là không lần nào)"
ok "P1a: có 423, chậm nhất ở lần sai thứ 6 (đầu tiên ở lần $first423)" test "$first423" -ge 1 -a "$first423" -le 6
ok "P1a: không quá 5 lần so mật khẩu sai (401 x$n401)" test "$n401" -le 5
ok "P1a: đã khoá thì không lần sai nào còn 401 (401 sau khoá: $sau_khoa_401)" test "$sau_khoa_401" = 0
c="$(dung "$SU" 127.0.0.99 admin)"
echo "  sau đó, ĐÚNG mật khẩu admin, cùng user, nguồn mới: HTTP $c"
ok "P1a: đang khoá thì mật khẩu admin đúng cũng 423" test "$c" = 423

step "P1b. 20 lần sai bắn song song, user m28p1b, cùng nguồn 127.0.0.32"
# sqlq <câu> [tham số...]: đọc tpan.db (chỉ đọc), in một giá trị.
sqlq() {
  python3 -I -W ignore -c '
import sqlite3, sys
c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True)
print(c.execute(sys.argv[1], sys.argv[2:]).fetchone()[0])' "$@"
}
a0="$(sqlq "SELECT COALESCE(MAX(id), 0) FROM audit")"
for n in $(seq 1 20); do
  { sai m28p1b 127.0.0.32 admin "par$n"; echo; } >"$W/p1-par.$n" &
done
wait
cat "$W"/p1-par.* | sort | uniq -c | sed 's/^/  mã /'
c401="$(cat "$W"/p1-par.* | grep -cx 401)"
ck="$(cat "$W"/p1-par.* | grep -cvx -e 401 -e 423 -e 429)"
rm -f "$W"/p1-par.*
ok "P1b: số câu 401 không quá 5 ($c401), còn lại chỉ 423 hay 429" test "$c401" -le 5 -a "$ck" = 0
c="$(dung m28p1b 127.0.0.32 admin)"
ok "P1b: sau loạt song song, mật khẩu admin đúng vẫn 423 (HTTP $c)" test "$c" = 423
nfail="$(sqlq "SELECT COUNT(*) FROM audit WHERE id > ? AND action = 'login.fail'" "$a0")"
echo "  dòng login.fail mới trong audit: $nfail"
ok "P1b: không quá 5 lần so mật khẩu sai thật (login.fail trong audit: $nfail)" test "$nfail" -le 5
nk="$(sqlq "SELECT COUNT(*) FROM login_failures WHERE ip = ?" "uid:$(id -u m28p1b)")"
ok "P1b: khoá nằm ở khoá nguồn uid:$(id -u m28p1b) trong login_failures (dòng: $nk)" test "$nk" -ge 1

step "P1c. ca hợp lệ (user m28p1c): admin sai 4 lần rồi đúng; sau đó sai 4 lần nữa chưa bị khoá; tpan admin unlock gỡ được khoá"
log=""
bad=0
for n in 1 2 3 4; do
  c="$(sai m28p1c 127.0.0.33 admin "c1-$n")"
  log="$log $c"
  [ "$c" = 401 ] || bad=1
done
echo "  4 lần sai:$log"
ok "P1c: 4 lần sai đầu đều 401" test "$bad" = 0
c="$(dung m28p1c 127.0.0.33 admin)"
ok "P1c: admin gõ đúng thì vào được (HTTP $c)" test "$c" = 204
log=""
bad=0
for n in 1 2 3 4; do
  c="$(sai m28p1c 127.0.0.33 admin "c2-$n")"
  log="$log $c"
  [ "$c" = 401 ] || bad=1
done
echo "  4 lần sai nữa:$log"
ok "P1c: bộ đếm về 0 sau lần đúng: 4 lần sai nữa vẫn 401" test "$bad" = 0
c5="$(sai m28p1c 127.0.0.33 admin c2-5)"
c6="$(dung m28p1c 127.0.0.33 admin)"
echo "  lần sai thứ 5: HTTP $c5; rồi mật khẩu đúng: HTTP $c6"
ok "P1c: lần sai thứ 5 khoá, mật khẩu đúng sau đó 423 (5: $c5, đúng: $c6)" bash -c '[ "$2" = 423 ] && { [ "$1" = 401 ] || [ "$1" = 423 ]; }' _ "$c5" "$c6"
tpan admin unlock "uid:$(id -u m28p1c)" 2>&1 | head -2 | sed 's/^/  | /'
c="$(dung m28p1c 127.0.0.33 admin)"
ok "P1c: sau tpan admin unlock, mật khẩu đúng vào được (HTTP $c)" test "$c" = 204

step "P1d. tpan còn sống"
ok "tpan: PID và NRestarts không đổi" song "$p0" "$n0" || doi_tpan
