#!/usr/bin/env bash
# e2e của mốc M14 (cô lập sâu giữa các site và định tuyến có bằng chứng; mục
# 6.13, bước 17 ở mục 12 và hàng M14 ở mục 13 của docs/KE-HOACH.md) trên máy ảo
# (tên trong file .tpan-vm; không có thì tpan-test). Chạy trên Mac, trong thư
# mục repo, một hoặc nhiều phần theo thứ tự gõ:
#
#   scripts/e2e-m14.sh nginx       phần A: stack nginx từ máy sạch
#   scripts/e2e-m14.sh apache      phần B: stack Apache từ máy sạch
#   scripts/e2e-m14.sh tinh-nang   phần C: trên máy của phần B (không reset)
#   scripts/e2e-m14.sh nang-cap    phần D: nâng từ v0.1.2, nginx rồi Apache,
#                                  mỗi stack từ máy sạch
#   scripts/e2e-m14.sh chinh       = nginx apache tinh-nang
#   scripts/e2e-m14.sh all         = chinh rồi nang-cap
#
# A (nginx, máy sạch): stack install, hai site; bộ phép thử tấn công
#   scripts/e2e-m14/tan-cong.sh (L1, L2, L5, L6, L7, L8 bị chặn; L3, L4, L9
#   không thuộc stack; không phép "không lộ" nào hỏng); health không [lỗi];
#   chạy lại site add không đổi gì; logrotate thật rồi log mới nằm đúng
#   access.log; thử thách ACME đi qua vhost có disable_symlinks; cài PHP 8.4 có
#   file ini và drop-in, chuyển site A sang 8.4 rồi chạy lại bộ phép thử;
#   route explain 8 chặng, vhost lạ trùng tên làm chặng vhost đỏ; site SSL
#   (pebble) và tên lạ ở cổng 443.
# B (Apache, máy sạch): như A; L1, L2, L3, L4, L6, L7, L8, L9 bị chặn, L5 không
#   thuộc stack.
# C (tính năng, trên máy của B): ba chế độ .htaccess (tat, an-toan, day-du),
#   quét chỉ thị và lời từ chối htaccess_blocked, health web.htaccess, route
#   explain (8 chặng, vhost lạ, site tạm khoá), bốn route API mới và quyền.
# D (nâng cấp, mỗi stack từ máy sạch): file chạy v0.1.2 đã phát hành, stack,
#   hai site; bộ phép thử trên v0.1.2 phải LỘ; thay file chạy bằng bản của
#   cây; health có [lỗi] và chỉ sang harden; thao tác thường trên site cũ bị
#   hoàn tác với lời nhắn chỉ sang harden; harden lỗi có chủ ý (vhost hỏng cú
#   pháp ngoài tpan) thì máy về như trước; harden --dry-run, --yes, chạy lại;
#   health hết [lỗi]; bộ phép thử sau harden; logrotate; site vẫn 200.
#
# ĐỒ NGHỀ TEST, CHỈ TRONG MÁY ẢO: pebble (máy chủ ACME thử, unit
# tpan-e2e-pebble) và CA thử trong kho tin cậy; dòng /etc/hosts cho site SSL;
# vhost lạ aaa-e2e-la.conf và vhost hỏng zzz-e2e-hong.conf (đặt rồi gỡ).
#
# File chạy v0.1.2: thư mục OLD_DIR (mặc định dist/release/v0.1.2, dist/ không
# vào git) có tpan-linux-<arch> và SHA256SUMS của bản phát hành. Chưa có thì:
#   gh release download v0.1.2 --repo <repo> --pattern 'tpan-linux-*' --pattern SHA256SUMS --dir dist/release/v0.1.2
#
# Đặt E2E_LOG=<file log> để cuối lần chạy script tự tìm trong chính file log
# đó: mọi mật khẩu và cookie đã gặp, IP máy ảo (IPv4, IPv6), IP của Mac, thư
# mục nhà và tên user Mac (phải là 0 lần).
#
# LỚP BỌC LỆNH MÁY ẢO (vm_cap): mọi lệnh `scripts/vm.sh exec|root ...` in
# trong log được chạy bằng `multipass exec` qua một lớp bọc: output của lệnh
# ghi vào file TRONG máy ảo, ngắn thì in về trong cùng lần gọi, dài thì lấy về
# bằng `multipass transfer`. Lý do: `multipass exec` trên Mac có lúc treo giữa
# chừng khi output dài (gặp 2 lần với `tpan health`: tiến trình trong máy ảo
# đã xong, output về Mac bị cắt giữa dòng, lệnh trên Mac không bao giờ trả về).
# Lệnh, output và mã thoát in trong log là của chính lệnh thật.
#
# Output như các e2e trước: mỗi lệnh có dấu thời gian, output nguyên văn (đã
# che) và mã thoát; mỗi khẳng định in "=> ĐẠT" hoặc "=> KHÔNG ĐẠT"; cuối mỗi
# phần và cuối lần chạy in số đạt, không đạt.
# Không dùng set -e: nhiều lệnh ở đây được chờ là phải lỗi.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2

VM=scripts/vm.sh
VMNAME="${TPAN_VM:-$(tr -d '[:space:]' 2>/dev/null <.tpan-vm)}"
VMNAME="${VMNAME:-tpan-test}"
FAILS=0
PASSES=0
OUT=""
RC=0
CODE=""
EMAIL=admin@example.com
A=a.example.com
B=b.example.com
C=c.example.com   # của user thường khach_a (phần C, API)
S=s.example.com   # site SSL (pebble)
UA=khach_a
TC=scripts/e2e-m14/tan-cong.sh
ACME=https://localhost:14000/dir
PEBBLE_UNIT=tpan-e2e-pebble
PEBBLE_DIR=/etc/tpan-e2e-pebble
OLD_VER=v0.1.2
OLD_DIR="${OLD_DIR:-dist/release/$OLD_VER}"
VER="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

TMPD="$(mktemp -d)"
chmod 700 "$TMPD"
SECRETS="$TMPD/secrets"
: >"$SECRETS"
chmod 600 "$SECRETS"
HJ="$TMPD/health.json"
trap 'rm -rf "$TMPD"' EXIT
trap 'exit 130' INT TERM

ts() { date '+%Y-%m-%d %H:%M:%S'; }
section() { printf '\n\n######## [%s] %s\n' "$(ts)" "$*"; }

VM_IP=""
HOST_IP=""
NET4=""
VM_V6=""
V6PFX=""
BASE=""

# mask_str che một chuỗi (stdin) theo mọi thứ đã biết, in ra stdout.
mask_str() {
  local s a p
  s="$(cat)"
  [ -n "$VM_IP" ] && s="${s//"$VM_IP"/<vm>}"
  [ -n "$HOST_IP" ] && s="${s//"$HOST_IP"/<mac>}"
  [ -n "$NET4" ] && s="${s//"$NET4"/<mạng máy ảo>.}"
  while IFS= read -r a; do
    [ -n "$a" ] && s="${s//"$a"/<vm-v6>}"
  done <<<"$VM_V6"
  [ -n "$V6PFX" ] && s="${s//"$V6PFX"/<tiền tố v6>}"
  [ -n "$V6PFX" ] && s="${s//"$(tr 'a-f' 'A-F' <<<"$V6PFX")"/<tiền tố v6>}"
  s="${s//"$TMPD"/<tmp>}"
  s="${s//"$HOME"/~}"
  s="${s//"$USER"/<user>}"
  while IFS= read -r p; do
    [ -n "$p" ] && s="${s//"$p"/<bí mật, đã che>}"
  done <"$SECRETS"
  s="$(sed -E -e 's/inet6 [0-9a-f:]{3,}/inet6 <v6>/g' -e 's/^(set-cookie: *[^=]*=)[^;]*/\1<cookie, đã che>/I' <<<"$s")"
  printf '%s' "$s"
}
mask() { OUT="$(mask_str <<<"$OUT")"; }

# runq chạy lệnh, giữ output thô ở $TMPD/run.out, KHÔNG in: dùng khi output có
# thể chứa một bí mật chưa biết, để đọc nó vào danh sách che rồi mới show_last.
# Output vào file rồi mới đọc (multipass exec trên Mac có lúc treo khi stdout
# là pipe); stdin là /dev/null (multipass exec chờ stdin mãi khi không có terminal).
runq() {
  RUNQ_CMD="$(mask_str <<<"$*")"
  RUNQ_TS="$(ts)"
  if [ "$1" = "$VM" ] && { [ "${2:-}" = exec ] || [ "${2:-}" = root ]; }; then
    shift
    vm_cap /dev/null "$@"
    return
  fi
  "$@" </dev/null >"$TMPD/run.out" 2>&1
  RC=$?
}

# Lớp bọc chạy trong máy ảo (xem đầu file). CAP_MERGED gộp stdout và stderr
# theo đúng thứ tự vào một file; CAP_SPLIT giữ riêng hai luồng (lệnh --json).
# File nằm ở nhà của user ubuntu, quyền 0600, lần chạy sau ghi đè lần trước.
# Không đổi umask của chính lệnh (sudo gộp umask của người gọi vào umask của
# lệnh root, tpan sẽ tạo file với quyền khác đi): chỉ tạo sẵn file 0600.
CAP_MAX=4000
CAP_MERGED='f=$HOME/.e2e-cap; (umask 077; : >"$f"); "$@" >"$f" 2>&1; rc=$?; n=$(wc -c <"$f"); [ "$n" -le '"$CAP_MAX"' ] && cat "$f"; printf "\n__E2E_CAP rc=%s n=%s e=0\n" "$rc" "$n"'
CAP_SPLIT='f=$HOME/.e2e-cap; (umask 077; : >"$f"; : >"$f.err"); "$@" >"$f" 2>"$f.err"; rc=$?; printf "__E2E_CAP rc=%s n=%s e=%s\n" "$rc" "$(wc -c <"$f")" "$(wc -c <"$f.err")"'
cap_fetch() { # cap_fetch <file trong máy ảo> <file trên Mac>: đọc lại được nên thử tối đa 3 lần
  local i
  for i in 1 2 3; do
    multipass transfer "$VMNAME:$1" "$2" </dev/null >/dev/null 2>&1 && return 0
    sleep 1
  done
  return 1
}
# vm_cap <file stdin> <exec|root> <lệnh...>: output gộp ở $TMPD/run.out, mã thoát ở RC.
vm_cap() {
  local in=$1 how=$2 line n
  shift 2
  [ "$how" = root ] && set -- sudo "$@"
  multipass exec "$VMNAME" -- bash -c "$CAP_MERGED" _ "$@" <"$in" >"$TMPD/cap.raw" 2>&1
  RC=$?
  line="$(tr -d '\r' <"$TMPD/cap.raw" | grep -a '^__E2E_CAP rc=' | tail -1)"
  if [ -z "$line" ]; then
    # Không có dòng đuôi: chính multipass lỗi (máy ảo đang khởi động lại...).
    cp "$TMPD/cap.raw" "$TMPD/run.out"
    return
  fi
  RC="$(sed -E 's/.* rc=([0-9]+) .*/\1/' <<<"$line")"
  n="$(sed -E 's/.* n=([0-9]+) .*/\1/' <<<"$line")"
  if [ "$n" -le "$CAP_MAX" ]; then
    sed '$d' "$TMPD/cap.raw" >"$TMPD/run.out"
  elif ! cap_fetch /home/ubuntu/.e2e-cap "$TMPD/run.out"; then
    printf '(e2e: không lấy được output %s byte từ máy ảo bằng multipass transfer)\n' "$n" >"$TMPD/run.out"
    RC=254
  fi
}
# vm_cap2 <exec|root> <lệnh...>: như vm_cap nhưng stdout ở $TMPD/cap.out và
# stderr ở $TMPD/cap.err (cho lệnh --json: stdout chỉ có JSON).
vm_cap2() {
  local how=$1 line n e
  shift
  [ "$how" = root ] && set -- sudo "$@"
  : >"$TMPD/cap.out"
  : >"$TMPD/cap.err"
  multipass exec "$VMNAME" -- bash -c "$CAP_SPLIT" _ "$@" </dev/null >"$TMPD/cap.raw" 2>&1
  RC=$?
  line="$(tr -d '\r' <"$TMPD/cap.raw" | grep -a '^__E2E_CAP rc=' | tail -1)"
  if [ -z "$line" ]; then
    cp "$TMPD/cap.raw" "$TMPD/cap.err"
    return
  fi
  RC="$(sed -E 's/.* rc=([0-9]+) .*/\1/' <<<"$line")"
  n="$(sed -E 's/.* n=([0-9]+) .*/\1/' <<<"$line")"
  e="$(sed -E 's/.* e=([0-9]+).*/\1/' <<<"$line")"
  if [ "$n" -gt 0 ] && ! cap_fetch /home/ubuntu/.e2e-cap "$TMPD/cap.out"; then RC=254; fi
  if [ "$e" -gt 0 ] && ! cap_fetch /home/ubuntu/.e2e-cap.err "$TMPD/cap.err"; then RC=254; fi
}
show_last() {
  printf '\n[%s] $ %s\n' "$RUNQ_TS" "$RUNQ_CMD"
  OUT="$(tr -d '\r' <"$TMPD/run.out")"
  mask
  [ -n "$OUT" ] && printf '%s\n' "$OUT"
  printf '[%s] (mã thoát %s)\n' "$(ts)" "$RC"
}
run() {
  runq "$@"
  show_last
}
ok() {
  local desc=$1
  shift
  if "$@"; then
    printf '  => ĐẠT: %s\n' "$desc"
    PASSES=$((PASSES + 1))
  else
    printf '  => KHÔNG ĐẠT: %s\n' "$desc"
    FAILS=$((FAILS + 1))
  fi
}
# note: một dòng ghi nhận (không phải khẳng định, không tính đạt hay không đạt).
note() { printf '  -- GHI NHẬN: %s\n' "$*"; }
P0=0
F0=0
part_begin() { P0=$PASSES F0=$FAILS; section "PHẦN $*"; }
part_end() { printf '\n==== Phần %s: %s đạt, %s không đạt\n' "$1" "$((PASSES - P0))" "$((FAILS - F0))"; }

rc_is() { [ "$RC" -eq "$1" ]; }
rc_not() { [ "$RC" -ne "$1" ]; }
has() { grep -qE -- "$1" <<<"$OUT"; }
hasnt() { ! grep -qE -- "$1" <<<"$OUT"; }
hasF() { grep -qF -- "$1" <<<"$OUT"; }
hasntF() { ! grep -qF -- "$1" <<<"$OUT"; }
out_is() { [ "$OUT" = "$1" ]; }
count_is() { [ "$(grep -cE -- "$2" <<<"$OUT")" -eq "$1" ]; }
job_ok() { [ "$RC" = 0 ] && grep -q '^Job .*: thành công' <<<"$OUT"; }
# jout <biểu thức jq>: jq -e trên OUT (output --json của tpan).
jout() { jq -e "$@" <<<"$OUT" >/dev/null 2>&1; }
# refused <mã>: lệnh --json thoát khác 0 với .error.code là mã đó.
refused() { [ "$RC" != 0 ] && [ "$(jq -r '.error.code // empty' <<<"$OUT" 2>/dev/null)" = "$1" ]; }

vexec() { run "$VM" exec "$@"; }
vroot() { run "$VM" root "$@"; }

add_secret() {
  [ -n "${1:-}" ] || return 0
  grep -qxF -- "$1" "$SECRETS" || printf '%s\n' "$1" >>"$SECRETS"
}
# save_pw <tên> <mật khẩu>: giữ một mật khẩu ở $TMPD/pw.<tên> (0600) và che nó.
save_pw() {
  add_secret "$2"
  (umask 077 && printf '%s' "$2" >"$TMPD/pw.$1")
}

slug_of() {
  local d=$1 head hash
  head="$(printf '%s' "${d:0:16}" | tr -c 'a-z0-9' '_')"
  hash="$(printf '%s' "$d" | shasum -a 256 | cut -c1-6)"
  printf '%s_%s' "$head" "$hash"
}
user_of() { printf 'tp_%s' "$(slug_of "$1")"; }

# dbpw <domain>: mật khẩu DB của site trong wp-config.php (không in).
dbpw() {
  "$VM" root bash -c 'cd / && runuser -u "$2" -- wp config get DB_PASSWORD --path="/var/www/$1/public" 2>/dev/null' _ "$1" "$(user_of "$1")" </dev/null | tr -d '\r'
}

# ---------------------------------------------------------------- máy ảo

addrs() {
  VM_IP="$("$VM" ip </dev/null)"
  NET4="${VM_IP%.*}."
  HOST_IP="$("$VM" exec bash -c 'echo "${SSH_CLIENT%% *}"' </dev/null | tr -d '\r')"
  local _
  for _ in $(seq 1 30); do
    VM_V6="$("$VM" exec bash -c 'ip -o -6 addr show scope global | awk "{print \$4}" | cut -d/ -f1' </dev/null | tr -d '\r')"
    [ -n "$VM_V6" ] && break
    sleep 1
  done
  V6PFX="$(head -1 <<<"$VM_V6" | cut -d: -f1-4)"
  BASE="https://$VM_IP:8443"
  printf 'IP máy ảo: <vm> (IPv6: <vm-v6>); IP của Mac trong mạng Multipass: <mac> (đều được che trong log)\n'
}

reset_vm() {
  printf 'Máy ảo: %s\n' "$VMNAME"
  run make vm-reset
  ok "máy ảo về snapshot clean" rc_is 0
  local _
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    vexec cloud-init status --wait
    [ "$RC" -ne 255 ] && break
    sleep 3
  done
  ok "cloud-init xong" rc_is 0
  addrs
  vexec bash -c 'command -v tpan; ls /etc/tpan /var/lib/tpan 2>&1'
  ok "máy sạch: chưa có tpan, /etc/tpan, /var/lib/tpan" bash -c '! grep -q "/usr/local/bin/tpan" <<<"$1" && [ "$(grep -c "No such file or directory" <<<"$1")" -eq 2 ]' _ "$OUT"
}

deploy_new() {
  run make vm-deploy DATE="$BUILD_DATE"
  ok "build và chép tpan $VER vào máy ảo" rc_is 0
  run git log --oneline -1
  run git status --short
  vexec tpan version
  ok "tpan version là $VER" has "^tpan $VER "
}

# install_run <lệnh...>: một lần chạy in mật khẩu quản trị panel (tpan setup);
# nhặt mật khẩu (che, đếm số lần in) trước khi in. Kết quả: RC, OUT, PW, PW_HITS.
PW=""
PW_HITS=0
install_run() {
  runq "$@"
  local pw
  pw="$(tr -d '\r' <"$TMPD/run.out" | grep -A1 '^Mật khẩu quản trị (chỉ hiện MỘT lần' | sed -nE 's/^    ([A-Z2-7]{26})$/\1/p' | head -1)"
  PW_HITS=0
  if [ -n "$pw" ]; then
    PW="$pw"
    add_secret "$pw"
    PW_HITS="$(grep -oF -- "$pw" "$TMPD/run.out" | wc -l | tr -d ' ')"
  fi
  show_last
  printf 'Mật khẩu quản trị panel trong output: %s lần\n' "$PW_HITS"
}

# ---------------------------------------------------------------- đoạn chạy trong máy ảo

# Dựng pebble (đồ nghề test, như e2e-m5.sh và e2e-m13.sh). Chạy bằng root trong máy ảo.
PEBBLE_SCRIPT='
set -e
export DEBIAN_FRONTEND=noninteractive
apt-get -o DPkg::Lock::Timeout=120 install -y -q pebble >/dev/null
dpkg-query -W -f "gói: \${Package} \${Version}\n" pebble
d=$1
mkdir -p "$d"
chmod 700 "$d"
cd "$d"
openssl req -x509 -newkey rsa:2048 -nodes -days 7 -keyout ca.key -out ca.pem \
  -subj "/CN=tpan e2e pebble API CA (chi may ao test)" -addext "keyUsage=critical,keyCertSign,cRLSign" 2>/dev/null
openssl req -newkey rsa:2048 -nodes -keyout key.pem -out api.csr -subj "/CN=localhost" 2>/dev/null
printf "subjectAltName=DNS:localhost,IP:127.0.0.1\nbasicConstraints=CA:FALSE\nkeyUsage=critical,digitalSignature,keyEncipherment\nextendedKeyUsage=serverAuth\n" >api.ext
openssl x509 -req -in api.csr -CA ca.pem -CAkey ca.key -CAcreateserial -days 7 -out cert.pem -extfile api.ext 2>/dev/null
cp ca.pem /usr/local/share/ca-certificates/tpan-e2e-pebble.crt
update-ca-certificates 2>&1 | tail -1
printf "{\"pebble\":{\"listenAddress\":\"127.0.0.1:14000\",\"managementListenAddress\":\"127.0.0.1:15000\",\"certificate\":\"%s/cert.pem\",\"privateKey\":\"%s/key.pem\",\"httpPort\":80,\"tlsPort\":443,\"ocspResponderURL\":\"\",\"externalAccountBindingRequired\":false}}\n" "$d" "$d" >config.json
systemd-run --unit='"$PEBBLE_UNIT"' --setenv=PEBBLE_VA_NOSLEEP=1 --setenv=PEBBLE_WFE_NONCEREJECT=0 --setenv=PEBBLE_AUTHZREUSE=0 \
  /usr/bin/pebble -config "$d/config.json"
for i in $(seq 1 30); do
  curl -s -o /dev/null https://localhost:14000/dir && break
  sleep 1
done
'

# Các phép gọi vào WordPress của một site: trang chủ, permalink, trang không
# có, /wp-admin/, file tĩnh. WEB_SCRIPT <domain>
WEB_SCRIPT='
d=$1
c(){ curl -s -m 20 --resolve "$d:80:127.0.0.1" "$@"; }
echo "trang-chu=$(c -o /dev/null -w "%{http_code}" "http://$d/")"
echo "permalink=$(c -o /dev/null -w "%{http_code}" "http://$d/hello-world/")"
f=$(mktemp); k=$(c -o "$f" -w "%{http_code}" "http://$d/khong-co-trang-e2e/"); echo "khong-co=$k dau-wordpress=$(grep -c "wp-" "$f")"; rm -f "$f"
echo "wp-admin=$(c -o /dev/null -w "%{http_code} -> %{redirect_url}" "http://$d/wp-admin/")"
echo "tinh=$(c -o /dev/null -w "%{http_code}" "http://$d/wp-includes/css/dist/block-library/style.min.css")"
'
web() { vexec bash -c "$WEB_SCRIPT" _ "$1"; }
web_ok() { has '^trang-chu=200$' && has '^permalink=200$' && has '^khong-co=404 dau-wordpress=[1-9]' && has "^wp-admin=302 -> http://$1/wp-login\\.php" && has '^tinh=200$'; }
# web_assert <domain> <nhãn>: bốn khẳng định quen thuộc.
web_assert() {
  web "$1"
  ok "$2: trang chủ $1 trả 200" has '^trang-chu=200$'
  ok "$2: permalink /hello-world/ trả 200" has '^permalink=200$'
  ok "$2: trang không có trả 404 do WordPress trả" has '^khong-co=404 dau-wordpress=[1-9]'
  ok "$2: /wp-admin/ chuyển hướng tới wp-login.php" has "^wp-admin=302 -> http://$1/wp-login\\.php"
}

# Xoay log thật rồi gọi 3 request mới. ROTATE_SCRIPT <domain> <nhãn>
ROTATE_SCRIPT='
d=$1; tag=$2; dir=/var/log/tpan/sites/$d
g(){ curl -s -o /dev/null -m 10 --resolve "$d:80:127.0.0.1" "http://$d/?e2e=$1"; }
g "$tag-truoc-1"; g "$tag-truoc-2"; sleep 1
echo "trước khi xoay: access.log có $(wc -l <"$dir/access.log") dòng, $(grep -c "e2e=$tag-truoc-" "$dir/access.log") dòng mang dấu truoc"
logrotate -f /etc/logrotate.d/tpan-sites; echo "logrotate mã $?"
sleep 3
g "$tag-sau-1"; g "$tag-sau-2"; g "$tag-sau-3"; sleep 1
ls -la "$dir"
echo "sau khi xoay: access.log có $(grep -c "e2e=$tag-sau-" "$dir/access.log") dòng mang dấu sau"
if [ -f "$dir/access.log.1" ]; then echo "access.log.1 có $(grep -c "e2e=$tag-sau-" "$dir/access.log.1") dòng mang dấu sau, $(grep -c "e2e=$tag-truoc-" "$dir/access.log.1") dòng mang dấu truoc"; else echo "không có access.log.1"; fi
stat -c "%U:%G %a %n" "$dir" "$dir/access.log" "$dir/error.log"
'
# rotate_assert <domain> <nhãn>
rotate_assert() {
  local u
  u="$(user_of "$1")"
  vroot bash -c "$ROTATE_SCRIPT" _ "$1" "$2"
  ok "logrotate -f thoát 0" has '^logrotate mã 0$'
  ok "3 request mới nằm ở access.log" has '^sau khi xoay: access.log có 3 dòng mang dấu sau$'
  ok "không request mới nào rơi vào access.log.1; 2 request cũ đã sang access.log.1 (log có xoay thật)" has '^access.log.1 có 0 dòng mang dấu sau, 2 dòng mang dấu truoc$'
  ok "thư mục log vẫn root:$u 750, access.log và error.log vẫn root:$u 640" bash -c 'grep -qx "root:$2 750 /var/log/tpan/sites/$3" <<<"$1" && grep -qx "root:$2 640 /var/log/tpan/sites/$3/access.log" <<<"$1" && grep -qx "root:$2 640 /var/log/tpan/sites/$3/error.log" <<<"$1"' _ "$OUT" "$u" "$1"
}

# Thử thách ACME qua vhost của site. ACME_SCRIPT <domain> <dấu>
ACME_SCRIPT='
d=$1; tok=e2e-$2; dir=/var/lib/tpan/acme/.well-known/acme-challenge
ls -ld /var/lib/tpan/acme "$dir" 2>&1
moi=0; [ -d "$dir" ] || moi=1
mkdir -p "$dir"; printf "noi-dung-thu-thach-%s" "$2" >"$dir/$tok"; chmod 644 "$dir/$tok"
f=$(mktemp); code=$(curl -s -m 10 --resolve "$d:80:127.0.0.1" -o "$f" -w "%{http_code}" "http://$d/.well-known/acme-challenge/$tok")
echo "http=$code than=$(head -c 200 "$f")"
rm -f "$f" "$dir/$tok"
[ "$moi" = 1 ] && rmdir "$dir" /var/lib/tpan/acme/.well-known
true
'

# L5 qua cổng 443: symlink của site SSL trỏ sang file 640 của site khác, và
# symlink trỏ vào file của chính nó. SSL_LINK_SCRIPT <site SSL> <site nạn nhân>
SSL_LINK_SCRIPT='
s=$1; b=$2; us=$(stat -c %U "/var/www/$s"); ub=$(stat -c %U "/var/www/$b")
printf "NOI_DUNG_RIENG_CUA_B\n" >"/var/www/$b/public/e2e-rieng.txt"; chown "$ub:www-data" "/var/www/$b/public/e2e-rieng.txt"; chmod 640 "/var/www/$b/public/e2e-rieng.txt"
runuser -u "$us" -- ln -sf "/var/www/$b/public/e2e-rieng.txt" "/var/www/$s/public/e2e-lay.txt"
runuser -u "$us" -- ln -sfn "/var/www/$s/public/wp-includes/css" "/var/www/$s/public/e2e-tu"
f=$(mktemp)
c=$(curl -sk -m 10 --resolve "$s:443:127.0.0.1" -o "$f" -w "%{http_code}" "https://$s/e2e-lay.txt"); echo "symlink sang file của B qua https: http=$c lộ=$(grep -c NOI_DUNG_RIENG_CUA_B "$f")"
c=$(curl -sk -m 10 --resolve "$s:443:127.0.0.1" -o /dev/null -w "%{http_code}" "https://$s/e2e-tu/dist/block-library/style.min.css"); echo "symlink vào file của chính mình qua https: http=$c"
rm -f "$f" "/var/www/$b/public/e2e-rieng.txt" "/var/www/$s/public/e2e-lay.txt" "/var/www/$s/public/e2e-tu"
'

# Sửa .htaccess của một site dưới đúng chủ và quyền của file (không sed -i:
# nó thay file bằng một file mới của root). HT_SCRIPT <domain> <them-dau|them-cuoi|go> <dòng>
HT_SCRIPT='
d=$1; act=$2; line=$3; f=/var/www/$d/public/.htaccess; t=$(mktemp)
u=$(stat -c %U "/var/www/$d")
if [ ! -f "$f" ]; then install -o "$u" -g www-data -m 640 /dev/null "$f"; fi
case "$act" in
  them-dau) { printf "%s\n" "$line"; cat "$f"; } >"$t" ;;
  them-cuoi) { cat "$f"; printf "%s\n" "$line"; } >"$t" ;;
  go) grep -vxF -- "$line" "$f" >"$t" ;;
esac
cat "$t" >"$f"; rm -f "$t"
stat -c "%U:%G %a %n" "$f"
grep -nxF -- "$line" "$f" || echo "(không có dòng: $line)"
'
ht() { vroot bash -c "$HT_SCRIPT" _ "$@"; }

# Đặt hoặc gỡ một vhost lạ trùng tên miền của site (như L8 của tan-cong.sh),
# rồi in cấu hình web server đang nạp. ALIEN_SCRIPT <dat|go> <domain> [địa chỉ nghe]
# Có địa chỉ nghe (127.0.0.1): vhost lạ nằm ở một địa chỉ riêng, nên web server
# chọn nó trước mọi vhost nghe "*" cho request tới địa chỉ đó, và `apache2ctl -S`
# in nó theo dạng gọn một dòng "<địa chỉ>:<cổng> <tên> (<file>:<dòng>)".
ALIEN_SCRIPT='
act=$1; d=$2; addr=${3:-}
if systemctl is-active --quiet apache2; then web=apache; f=/etc/apache2/sites-enabled/aaa-e2e-la.conf; else web=nginx; f=/etc/nginx/sites-enabled/aaa-e2e-la.conf; fi
if [ "$act" = dat ]; then
  mkdir -p /srv/e2e-trang-la; echo "DAY LA TRANG LA" >/srv/e2e-trang-la/index.html
  if [ "$web" = nginx ]; then
    printf "server {\n    listen %s80;\n    server_name %s;\n    root /srv/e2e-trang-la;\n}\n" "${addr:+$addr:}" "$d" >"$f"
    nginx -t 2>&1 | tail -3; echo "nginx -t mã ${PIPESTATUS[0]}"; systemctl reload nginx
  else
    printf "<VirtualHost %s:80>\n    ServerName %s\n    DocumentRoot /srv/e2e-trang-la\n    <Directory /srv/e2e-trang-la>\n        Require all granted\n    </Directory>\n</VirtualHost>\n" "${addr:-*}" "$d" >"$f"
    apache2ctl configtest 2>&1 | tail -3; echo "configtest mã ${PIPESTATUS[0]}"; systemctl reload apache2
  fi
else
  rm -f "$f"; rm -rf /srv/e2e-trang-la
  if [ "$web" = nginx ]; then systemctl reload nginx; else systemctl reload apache2; fi
fi
sleep 2
echo "file lạ: $(ls "$f" 2>/dev/null || echo "(đã gỡ)")"
echo "tên miền đang trả: $(curl -s -m 10 --resolve "$d:80:127.0.0.1" "http://$d/" | head -c 40 | tr "\n" " ")"
echo "== cấu hình đang nạp ($web)"
if [ "$web" = nginx ]; then nginx -T 2>/dev/null | grep -nE "^# configuration file|^[[:space:]]*(server_name|listen|root|fastcgi_pass)[[:space:]]"; else apache2ctl -S 2>&1; fi
'
alien() { vroot bash -c "$ALIEN_SCRIPT" _ "$@"; }

# Đặt hoặc gỡ một vhost hỏng cú pháp ngoài tpan (để bước kiểm cú pháp của
# harden lỗi có chủ ý). BROKEN_SCRIPT <dat|go>
BROKEN_SCRIPT='
act=$1
if systemctl is-active --quiet apache2; then web=apache; f=/etc/apache2/sites-enabled/zzz-e2e-hong.conf; else web=nginx; f=/etc/nginx/sites-enabled/zzz-e2e-hong.conf; fi
if [ "$act" = dat ]; then
  if [ "$web" = nginx ]; then printf "server {\n    listen 80;\n    server_name hong.example.com;\n    chi_thi_khong_co on;\n}\n" >"$f"; else printf "<VirtualHost *:80>\n    ServerName hong.example.com\n    ChiThiKhongCo on\n</VirtualHost>\n" >"$f"; fi
else
  rm -f "$f"
fi
echo "file hỏng: $(ls "$f" 2>/dev/null || echo "(đã gỡ)")"
if [ "$web" = nginx ]; then nginx -t 2>&1 | tail -2; echo "kiểm cú pháp mã ${PIPESTATUS[0]}"; else apache2ctl configtest 2>&1 | tail -2; echo "kiểm cú pháp mã ${PIPESTATUS[0]}"; fi
'

# Ảnh chụp những thứ harden đụng tới, để so trước và sau một lần hoàn tác.
SNAP_SCRIPT='
shopt -s nullglob
for f in /etc/nginx/sites-available/tpan-*.conf /etc/apache2/sites-available/tpan-*.conf /etc/logrotate.d/tpan-sites \
         /etc/php/*/fpm/conf.d/99-tpan.ini /etc/systemd/system/php*-fpm.service.d/tpan.conf /var/www/*/public/.htaccess; do
  echo "$(sha256sum <"$f" | cut -c1-16) $(stat -c "%a %U:%G" "$f") $f"
done
stat -c "%a %U:%G %n" /var/log/tpan/sites/* /var/log/tpan/sites/*/*.log
echo "sites-enabled: $(ls /etc/nginx/sites-enabled /etc/apache2/sites-enabled 2>/dev/null | grep -v zzz-e2e-hong | tr "\n" " ")"
for v in /etc/php/*/fpm; do v=${v#/etc/php/}; v=${v%/fpm}; echo "php$v-fpm: $(systemctl is-active "php$v-fpm") $(systemctl show -p ProtectProc "php$v-fpm")"; done
python3 -c "
import sqlite3
c = sqlite3.connect(\"file:/var/lib/tpan/tpan.db?mode=ro\", uri=True)
for r in c.execute(\"SELECT domain, status, htaccess FROM sites ORDER BY domain\"): print(\"sites:\", *r)
print(\"managed_files:\", c.execute(\"SELECT COUNT(*) FROM managed_files\").fetchone()[0])
"
'
snap() {
  vm_cap /dev/null root bash -c "$SNAP_SCRIPT"
  tr -d '\r' <"$TMPD/run.out" >"$1"
  printf '\n[%s] Ảnh chụp (%s):\n' "$(ts)" "${1##*/}"
  mask_str <"$1"
  printf '\n'
}

# ---------------------------------------------------------------- bộ phép thử tấn công

# tancong <site A bị chiếm> <site B nạn nhân>
tancong() {
  RUNQ_CMD="$VM root bash -s -- $1 $2 < $TC"
  RUNQ_TS="$(ts)"
  vm_cap "$TC" root bash -s -- "$1" "$2"
  show_last
}
kq() { has "^KQ $1 +$2( |\$)"; }
# kqs <LO|CHAN|BO> <mã>...: mọi mã đều có đúng kết quả đó.
kqs() {
  local st=$1 l
  shift
  for l in "$@"; do kq "$l" "$st" || return 1; done
}
no_hong() { hasnt '^KQ [A-Z0-9]+ +HONG'; }
tong() { grep -E '^TONG ' <<<"$OUT" | tail -1; }

# ---------------------------------------------------------------- health

health_json() {
  printf '\n[%s] $ %s root tpan health %s --json\n' "$(ts)" "$VM" "$*"
  vm_cap2 root tpan health "$@" --json
  tr -d '\r' <"$TMPD/cap.out" >"$HJ"
  OUT="$(cat "$TMPD/cap.err")"
  mask
  [ -n "$OUT" ] && printf 'stderr:\n%s\n' "$OUT"
  if ! jq -e .groups "$HJ" >/dev/null 2>&1; then
    OUT="$(head -c 3000 "$HJ")"
    mask
    printf 'stdout không phải báo cáo JSON:\n%s\n[%s] (mã thoát %s)\n' "$OUT" "$(ts)" "$RC"
    return
  fi
  OUT="$(jq -r '"nhóm: \([.groups[] | if .target == "" then "(máy, stack)" else .target end] | join(", "))",
    "số phép kiểm theo trạng thái: \([.groups[].results[].status] | group_by(.) | map("\(.[0])=\(length)") | join(" "))",
    ([.groups[].results[] | select(.status != "pass")] | if length == 0 then "(mọi phép kiểm đạt)" else (.[] |
      "--- \(.status) \(.id) (chặng \(.stage)): \(.summary.vi)") end)' "$HJ")"
  mask
  printf '%s\n[%s] (mã thoát %s)\n' "$OUT" "$(ts)" "$RC"
}
hq() { jq -e "$@" "$HJ" >/dev/null 2>&1; }
no_fail() { hq '[.groups[].results[] | select(.status == "fail")] | length == 0'; }
check_status() { hq --arg id "$1" --arg st "$2" '[.groups[].results[] | select(.id == $id and .status == $st)] | length == 1'; }
# health_text [đối số]: tpan health dạng chữ (thứ người vận hành đọc).
health_text() { vroot tpan health "$@"; }
no_loi() { hasnt '^\[lỗi\]'; }

# ---------------------------------------------------------------- CLI

# site_add <domain> [cờ...]: tpan site add, nhặt mật khẩu quản trị WordPress
# và mật khẩu DB của site vào danh sách che trước khi in. Không khẳng định gì.
site_add_q() {
  local d=$1 pw
  shift
  runq "$VM" root tpan site add "$d" --email "$EMAIL" --yes "$@"
  pw="$(tr -d '\r' <"$TMPD/run.out" | sed -nE 's/^  Mật khẩu: +([A-Za-z0-9]{32})$/\1/p' | head -1)"
  add_secret "$pw"
  add_secret "$(dbpw "$d")"
  PW_HITS=0
  [ -n "$pw" ] && PW_HITS=1
  show_last
}
site_add() {
  site_add_q "$@"
  ok "site add $*: thoát 0, job thành công" job_ok
}

# json_secret <tên> <biểu thức jq> <lệnh...>: chạy lệnh --json, đọc mật khẩu
# theo biểu thức jq vào $TMPD/pw.<tên> rồi mới in.
json_secret() {
  local name=$1 expr=$2 pw
  shift 2
  RUNQ_CMD="$(mask_str <<<"$*")"
  RUNQ_TS="$(ts)"
  shift # bỏ "$VM": lệnh luôn là `"$VM" root|exec ...`
  vm_cap2 "$@"
  cp "$TMPD/cap.out" "$TMPD/run.json"
  cp "$TMPD/cap.err" "$TMPD/run.err"
  pw="$(tr -d '\r' <"$TMPD/run.json" | jq -r "$expr // empty" 2>/dev/null)"
  [ -n "$pw" ] && save_pw "$name" "$pw"
  cat "$TMPD/run.err" "$TMPD/run.json" >"$TMPD/run.out"
  show_last
  OUT="$(tr -d '\r' <"$TMPD/run.json")"
  mask
  JS_PW="$pw"
}

# vjson <lệnh tpan ... --json>: stdout (JSON) vào OUT, stderr in riêng.
vjson() {
  RUNQ_CMD="$(mask_str <<<"$VM root $*")"
  RUNQ_TS="$(ts)"
  vm_cap2 root "$@"
  cp "$TMPD/cap.out" "$TMPD/run.json"
  cp "$TMPD/cap.err" "$TMPD/run.err"
  cat "$TMPD/run.err" "$TMPD/run.json" >"$TMPD/run.out"
  show_last
  OUT="$(tr -d '\r' <"$TMPD/run.json")"
  mask
}

# ---------------------------------------------------------------- API (từ Mac)

# api <jar> <METHOD> <đường dẫn> [thân JSON]: gọi panel qua TLS (--cacert là
# cert.pem lấy từ máy ảo, không -k), cookie ở jar. Mọi request mang Origin và
# Sec-Fetch-Site như trình duyệt. Thân trả về ở $TMPD/body; mọi trường
# "password" trong đó và giá trị cookie vào danh sách che TRƯỚC khi in.
api() {
  local jar=$1 m=$2 p=$3 body=${4-}
  local args=(-sS -m 120 --cacert "$TMPD/cert.pem" -b "$jar" -c "$jar" -X "$m" -o "$TMPD/body"
    -w '-> HTTP %{http_code}\n' -H 'Accept: application/json' -H 'Sec-Fetch-Site: same-origin' -H "Origin: $BASE")
  : >"$TMPD/body"
  if [ -n "$body" ]; then
    (umask 077 && printf '%s' "$body" >"$TMPD/req.json")
    args+=(-H 'Content-Type: application/json' --data-binary "@$TMPD/req.json")
  fi
  printf '\n[%s] $ curl -X %s <panel>%s   (phiên: %s)\n' "$(ts)" "$m" "$p" "${jar##*/}"
  [ -n "$body" ] && printf '    thân gửi: %s\n' "$(mask_str <<<"$body")"
  curl "${args[@]}" "$BASE$p" </dev/null >"$TMPD/run.out" 2>&1
  RC=$?
  CODE="$(sed -nE 's/^-> HTTP ([0-9]{3})$/\1/p' "$TMPD/run.out" | tail -1)"
  harvest "$jar"
  OUT="$(tr -d '\r' <"$TMPD/run.out")"
  mask
  printf '%s\n' "$OUT"
  if jq -e . "$TMPD/body" >/dev/null 2>&1; then
    OUT="$(jq -c . "$TMPD/body" | head -c "${API_MAX:-900}")"
  else
    OUT="$(head -c 400 "$TMPD/body")"
  fi
  mask
  [ -n "$OUT" ] && printf '    thân nhận (tối đa %s byte): %s\n' "${API_MAX:-900}" "$OUT"
}
harvest() {
  local s
  while IFS= read -r s; do add_secret "$s"; done < <(jq -r '[.. | objects | (.password?, .db_password?) | strings] | .[]' "$TMPD/body" 2>/dev/null)
  [ -f "$1" ] && while IFS= read -r s; do add_secret "$s"; done < <(awk -F'\t' 'NF == 7 && ($1 !~ /^#/ || $1 ~ /^#HttpOnly_/) { print $7 }' "$1")
}
code_is() { [ "$CODE" = "$1" ]; }
ecode() { jq -r '.error.code // empty' "$TMPD/body" 2>/dev/null; }
# expect <mã HTTP> <mã lỗi>
expect() { [ "$CODE" = "$1" ] && [ "$(ecode)" = "$2" ]; }
bq() { jq -e "$@" "$TMPD/body" >/dev/null 2>&1; }
jb() { jq -r "$1" "$TMPD/body" 2>/dev/null; }

new_jar() { : >"$TMPD/jar.$1"; chmod 600 "$TMPD/jar.$1"; printf '%s' "$TMPD/jar.$1"; }

# login <jar> <tên> <file mật khẩu>: POST /api/v1/login.
login() {
  api "$1" POST /api/v1/login "$(jq -nc --arg n "$2" --rawfile p "$3" '{name: $n, password: $p}')"
}

fetch_cert() {
  "$VM" root cat /etc/tpan/tls/cert.pem </dev/null | tr -d '\r' >"$TMPD/cert.pem"
  local i
  for i in $(seq 1 30); do
    curl -sS -m 5 --cacert "$TMPD/cert.pem" -o /dev/null "$BASE/api/v1/health" 2>/dev/null && return 0
    sleep 1
  done
  return 1
}

# wait_job <jar> <id>: hỏi GET /jobs/{id} mỗi giây tới khi xong (tối đa 600 giây).
JOBST=""
wait_job() {
  local jar=$1 id=$2 i
  JOBST=""
  for i in $(seq 1 600); do
    curl -sS -m 10 --cacert "$TMPD/cert.pem" -b "$jar" -H 'Accept: application/json' -o "$TMPD/body" "$BASE/api/v1/jobs/$id" </dev/null 2>/dev/null
    harvest "$jar"
    JOBST="$(jq -r '.job.status // empty' "$TMPD/body" 2>/dev/null)"
    [ -n "$JOBST" ] && [ "$JOBST" != running ] && break
    sleep 1
  done
  OUT="$(jq -c '{job: (.job | {kind, target, status, actor, owner, error}), commands: [.events[] | select(.kind == "command") | .message[:160]] | .[:12]}' "$TMPD/body" 2>&1)"
  mask
  printf '(chờ job %s: %s giây)\n%s\n' "$id" "$i" "$OUT"
}

# ---------------------------------------------------------------- khối dùng chung

HOPS8='["dns","vhost","root","socket","pool","user","open_basedir","http"]'

# route_block <domain> <nhãn>: route explain 8 chặng, --json, vhost lạ, gỡ.
route_block() {
  local d=$1 n=$2
  section "$n: tpan route explain $d"
  vroot tpan route explain "$d"
  ok "route explain thoát 0" rc_is 0
  ok "in đủ 8 chặng (dòng bắt đầu bằng [trạng thái])" count_is 8 '^\[(đạt|cảnh báo|lỗi|chưa rõ)\] '
  ok "7 chặng đạt, chặng DNS là cảnh báo (máy ảo không có bản ghi A)" bash -c '[ "$(grep -c "^\[đạt\] " <<<"$1")" -eq 7 ] && grep -q "^\[cảnh báo\] Tên miền trỏ về máy này" <<<"$1"' _ "$OUT"
  ok "câu kết: không chặng nào lỗi" hasF 'Không chặng nào lỗi'
  vroot tpan route explain "$d" --evidence
  ok "--evidence in lệnh đã chạy của từng chặng (dòng \$)" bash -c '[ "$(grep -c "^    \$ " <<<"$1")" -ge 8 ]' _ "$OUT"
  vjson tpan route explain "$d" --json
  ok "--json đọc được: 8 chặng đúng thứ tự dns, vhost, root, socket, pool, user, open_basedir, http" jout --argjson k "$HOPS8" '[.hops[].key] == $k'
  ok "--json: dns là warn, 7 chặng còn lại pass, chặng nào cũng có bằng chứng" jout '.hops[0].status == "warn" and ([.hops[1:][] | .status] | all(. == "pass")) and ([.hops[] | .evidence | length] | all(. >= 1))'
  ok "--json: chặng user là $(user_of "$d"), root là /var/www/$d/public" jout --arg u "$(user_of "$d")" --arg r "/var/www/$d/public" '([.hops[] | select(.key == "user")][0].value == $u) and ([.hops[] | select(.key == "root")][0].value == $r)'

  section "$n: vhost lạ trùng tên $d (như L8)"
  alien dat "$d"
  ok "(đồ nghề) đặt được file lạ, web server nạp nó" has '^file lạ: /etc/(nginx|apache2)/sites-enabled/aaa-e2e-la.conf$'
  vroot tpan route explain "$d"
  ok "route explain thoát 1" rc_is 1
  ok "chặng vhost là [lỗi]" has '^\[lỗi\] Vhost nhận tên miền'
  ok "nêu đúng file lạ aaa-e2e-la.conf" hasF 'aaa-e2e-la.conf'
  vjson tpan route explain "$d" --json
  ok "--json: chặng vhost fail, mã thoát 1" bash -c '[ "$2" = 1 ] && jq -e "[.hops[] | select(.key == \"vhost\")][0].status == \"fail\"" <<<"$1" >/dev/null' _ "$OUT" "$RC"
  health_json
  ok "health: web.server_name.unique là fail" check_status web.server_name.unique fail
  ok "health nêu file lạ aaa-e2e-la.conf" bash -c 'jq -e "[.groups[].results[] | select(.id == \"web.server_name.unique\") | tostring | test(\"aaa-e2e-la.conf\")] | any" "$1" >/dev/null' _ "$HJ"
  alien go "$d"
  ok "(đồ nghề) đã gỡ file lạ" hasF 'file lạ: (đã gỡ)'
  vroot tpan route explain "$d"
  ok "gỡ file lạ: route explain thoát 0, không chặng nào lỗi" bash -c '[ "$2" = 0 ] && grep -qF "Không chặng nào lỗi" <<<"$1"' _ "$OUT" "$RC"
  health_json
  ok "health: web.server_name.unique đạt lại, không còn fail" bash -c 'jq -e "([.groups[].results[] | select(.id == \"web.server_name.unique\" and .status == \"pass\")] | length == 1) and ([.groups[].results[] | select(.status == \"fail\")] | length == 0)" "$1" >/dev/null' _ "$HJ"

  section "$n: vhost lạ trùng tên $d nghe ở địa chỉ riêng 127.0.0.1:80"
  alien dat "$d" 127.0.0.1
  ok "(đồ nghề) web server nạp file lạ và request tới 127.0.0.1 với tên $d ra trang lạ" bash -c 'grep -q "^file lạ: /etc/" <<<"$1" && grep -q "^tên miền đang trả: DAY LA TRANG LA" <<<"$1"' _ "$OUT"
  vroot tpan route explain "$d"
  ok "route explain thoát 1, chặng vhost [lỗi], nêu file lạ" bash -c '[ "$2" = 1 ] && grep -q "^\[lỗi\] Vhost nhận tên miền" <<<"$1" && grep -qF "aaa-e2e-la.conf" <<<"$1"' _ "$OUT" "$RC"
  health_json
  ok "health: web.server_name.unique là fail và nêu file lạ" bash -c 'jq -e "[.groups[].results[] | select(.id == \"web.server_name.unique\" and .status == \"fail\") | tostring | test(\"aaa-e2e-la.conf\")] | any" "$1" >/dev/null' _ "$HJ"
  alien go "$d"
  vroot tpan route explain "$d"
  ok "gỡ file lạ: route explain thoát 0" rc_is 0
}

# ssl_block <nhãn>: site SSL bằng pebble, rồi tên lạ ở cổng 443 và route
# explain của site SSL. Đồ nghề test; chứng chỉ thật chỉ thử trên VPS.
ssl_block() {
  local n=$1
  section "$n: (đồ nghề) pebble, site SSL $S"
  vroot bash -c "$PEBBLE_SCRIPT" _ "$PEBBLE_DIR"
  ok "dựng pebble thoát 0" rc_is 0
  vroot bash -c 'echo "127.0.0.1 $1" >>/etc/hosts && getent ahostsv4 "$1" | head -1' _ "$S"
  ok "$S -> 127.0.0.1" has '^127\.0\.0\.1 '
  vroot tpan ssl settings --email "$EMAIL" --server "$ACME"
  ok "ssl settings: email và máy chủ ACME thử" rc_is 0
  site_add "$S"
  vroot tpan ssl issue "$S" --yes
  ok "ssl issue $S (pebble): job thành công (thử thách ACME đi qua vhost đã siết)" job_ok
  vexec bash -c 'curl -sk -m 20 --resolve "$1:443:127.0.0.1" -o /dev/null -w "https=%{http_code}\n" "https://$1/"; curl -s -m 20 --resolve "$1:80:127.0.0.1" -o /dev/null -w "http=%{http_code} -> %{redirect_url}\n" "http://$1/"' _ "$S"
  ok "$S: https 200, http chuyển hướng sang https" bash -c 'grep -qx "https=200" <<<"$1" && grep -q "^http=301 -> https://" <<<"$1"' _ "$OUT"

  vroot bash -c "$SSL_LINK_SCRIPT" _ "$S" "$A"
  ok "L5 qua cổng 443: symlink của $S sang file 640 của $A không lộ (403 hoặc 404)" has '^symlink sang file của B qua https: http=(403|404) lộ=0$'
  ok "symlink của $S vào file của chính nó qua https vẫn 200" has '^symlink vào file của chính mình qua https: http=200$'

  section "$n: tên lạ ở cổng 443 (mã thoát curl thật, phép kiểm web.default.rejects)"
  vexec bash -c 'curl -sk -m 10 --resolve "tpan-e2e-la.invalid:443:127.0.0.1" -o /dev/null -w "http=%{http_code}\n" "https://tpan-e2e-la.invalid/"; echo "curl mã $?"; curl -s -m 10 -o /dev/null -w "http=%{http_code}\n" -H "Host: tpan-e2e-la.invalid" http://127.0.0.1/; echo "curl mã $?"'
  note "tên lạ ở 443 và 80: $(tr '\n' ' ' <<<"$OUT")"
  health_json
  ok "web.default.rejects đạt khi có site SSL (kiểm cả cổng 443)" check_status web.default.rejects pass
  ok "bằng chứng của web.default.rejects có lệnh gọi cổng 443" bash -c 'jq -e "[.groups[].results[] | select(.id == \"web.default.rejects\") | .evidence[]?.command | test(\"443\")] | any" "$1" >/dev/null' _ "$HJ"
  jq -r '.groups[].results[] | select(.id == "web.default.rejects") | .summary.vi, (.evidence[]? | "    $ \(.command)  (mã thoát \(.exit_code))", "      \(.output[:200])")' "$HJ" | mask_str
  printf '\n'
  ok "health không có fail (site SSL có đủ phép kiểm M14)" no_fail

  section "$n: route explain của site SSL (lần theo vhost cổng 443)"
  vroot tpan route explain "$S" --evidence
  ok "route explain $S thoát 0" rc_is 0
  ok "chặng vhost nói cổng 443" has 'cổng 443'
  ok "chặng gọi thật là https://$S/" hasF "https://$S/"
  vexec bash -c 'if systemctl is-active --quiet apache2; then sudo apache2ctl -S 2>&1; else sudo nginx -T 2>/dev/null | grep -nE "^# configuration file|^[[:space:]]*(server_name|listen|disable_symlinks)[[:space:]]"; fi'
  note "cấu hình đang nạp khi có site SSL (định dạng thật mà parser của tpan đọc) ở ngay trên"
}

# ================================================================ PHẦN A: nginx

part_a() {
  part_begin "A: stack nginx từ máy sạch"
  reset_vm
  deploy_new
  vroot tpan stack install --web nginx --yes
  ok "stack install --web nginx thoát 0, job thành công" job_ok
  vexec systemctl is-active nginx php8.3-fpm mariadb
  ok "nginx, php8.3-fpm, mariadb active" count_is 3 '^active$'
  vroot bash -c 'cat /etc/php/8.3/fpm/conf.d/99-tpan.ini; echo ---; cat /etc/systemd/system/php8.3-fpm.service.d/tpan.conf; echo ---; systemctl show -p ProtectProc php8.3-fpm; php-fpm8.3 -i 2>/dev/null | grep -E "^opcache\.(validate_permission|restrict_api)"'
  ok "cài stack đã ghi 99-tpan.ini và drop-in ProtectProc; php-fpm nạp đúng" bash -c 'grep -q "^opcache.validate_permission=1" <<<"$1" && grep -q "^ProtectProc=invisible" <<<"$1" && grep -q "^opcache.validate_permission => On => On" <<<"$1" && grep -q "^opcache.restrict_api => /usr/local/lib/tpan/opcache-api/" <<<"$1"' _ "$OUT"
  site_add "$A"
  site_add "$B"
  web_assert "$A" "site A"
  web_assert "$B" "site B"
  vroot bash -c 'grep -n disable_symlinks /etc/nginx/sites-available/tpan-*.conf; stat -c "%U:%G %a %n" /var/log/tpan/sites/* /var/log/tpan/sites/*/*.log'
  ok "vhost của hai site có disable_symlinks if_not_owner" count_is 2 'disable_symlinks if_not_owner from=\$document_root;'

  section "A: bộ phép thử tấn công, A=$A bị chiếm, B=$B nạn nhân"
  tancong "$A" "$B"
  ok "bộ phép thử chạy hết (có dòng TONG)" has '^TONG lo=[0-9]+ chan=[0-9]+ hong=[0-9]+$'
  ok "L1, L2, L5, L6, L7, L8 bị chặn" kqs CHAN L1 L2 L5 L6 L7 L8
  ok "L3, L4, L9 không thuộc stack nginx (BO)" kqs BO L3 L4 L9
  ok "không phép nào HONG" no_hong
  ok "tổng: lo=0 chan=6 hong=0" has '^TONG lo=0 chan=6 hong=0$'

  section "A: tpan health"
  health_text
  ok "tpan health thoát 0" rc_is 0
  ok "tpan health không có dòng [lỗi] nào" no_loi
  health_json
  ok "health --json: không phép kiểm nào fail" no_fail
  ok "các phép kiểm M14 đạt: php.opcache:8.3, php.protectproc:8.3, web.server_name.unique, web.default.rejects" bash -c 'for i in php.opcache:8.3 php.protectproc:8.3 web.server_name.unique web.default.rejects; do jq -e --arg id "$i" "[.groups[].results[] | select(.id == \$id and .status == \"pass\")] | length == 1" "$1" >/dev/null || exit 1; done' _ "$HJ"
  ok "của site: log.perm, web.hardened, isolation hai chiều đạt" bash -c 'for i in "log.perm:$2" "log.perm:$3" "web.hardened:$2" "web.hardened:$3" "isolation:$2:$3" "isolation:$3:$2"; do jq -e --arg id "$i" "[.groups[].results[] | select(.id == \$id and .status == \"pass\")] | length == 1" "$1" >/dev/null || exit 1; done' _ "$HJ" "$A" "$B"
  jq -r --arg id "isolation:$A:$B" '.groups[].results[] | select(.id == $id) | .summary.vi, (.evidence[]? | "    $ \(.command)  (mã thoát \(.exit_code))")' "$HJ" | mask_str
  printf '\n'
  ok "isolation:$A:$B có 5 phép thử, mỗi phép một dòng bằng chứng" bash -c 'jq -e --arg id "$2" "[.groups[].results[] | select(.id == \$id) | .evidence | length][0] >= 5" "$1" >/dev/null' _ "$HJ" "isolation:$A:$B"

  section "A: chạy lại tpan site add $A: không đổi gì"
  snap "$TMPD/a-truoc"
  site_add_q "$A"
  ok "chạy lại thoát 0, job thành công" job_ok
  ok "không bước nào chạy (không có dòng \"xong\")" hasnt '^  xong$'
  ok "mọi bước đều bỏ qua" bash -c 's=$(grep -c "^- " <<<"$1"); k=$(grep -c "^  đã đúng, bỏ qua$" <<<"$1"); echo "    số bước: $s, bỏ qua: $k"; [ "$s" -gt 0 ] && [ "$s" -eq "$k" ]' _ "$OUT"
  ok "không in mật khẩu quản trị (WordPress không cài lại)" test "$PW_HITS" -eq 0
  snap "$TMPD/a-sau"
  run diff "$TMPD/a-truoc" "$TMPD/a-sau"
  ok "máy không đổi (vhost, ini, drop-in, logrotate, quyền log, bảng sites)" rc_is 0

  section "A: logrotate thật, log mới phải nằm ở access.log"
  rotate_assert "$A" a1
  health_json "$A"
  ok "log.perm:$A vẫn đạt sau khi xoay" check_status "log.perm:$A" pass

  section "A: thử thách ACME qua vhost có disable_symlinks"
  vroot bash -c "$ACME_SCRIPT" _ "$A" a1
  ok "file thử thách trả 200 đúng nội dung qua tên miền của site" has '^http=200 than=noi-dung-thu-thach-a1$'

  section "A: cài thêm PHP 8.4 (kho sury), bản mới phải có file ini và drop-in"
  vroot tpan php install 8.4 --yes
  ok "php install 8.4 thoát 0, job thành công" job_ok
  if job_ok; then
    vroot bash -c 'cat /etc/php/8.4/fpm/conf.d/99-tpan.ini; echo ---; cat /etc/systemd/system/php8.4-fpm.service.d/tpan.conf; echo ---; systemctl show -p ProtectProc php8.4-fpm; php-fpm8.4 -i 2>/dev/null | grep -E "^opcache\.(validate_permission|restrict_api)"; systemctl is-active php8.3-fpm php8.4-fpm'
    ok "8.4 có 99-tpan.ini, drop-in ProtectProc=invisible, php-fpm8.4 nạp đúng" bash -c 'grep -q "^opcache.validate_permission=1" <<<"$1" && grep -q "^ProtectProc=invisible" <<<"$1" && grep -q "^opcache.validate_permission => On => On" <<<"$1" && [ "$(grep -cx active <<<"$1")" -eq 2 ]' _ "$OUT"
    health_json
    ok "php.opcache:8.4 và php.protectproc:8.4 đạt" bash -c 'for i in php.opcache:8.4 php.protectproc:8.4; do jq -e --arg id "$i" "[.groups[].results[] | select(.id == \$id and .status == \"pass\")] | length == 1" "$1" >/dev/null || exit 1; done' _ "$HJ"
    ok "health không có fail" no_fail
    vroot tpan php use "$A" 8.4 --yes
    ok "php use $A 8.4: job thành công" job_ok
    tancong "$A" "$B"
    ok "A chạy PHP 8.4, B chạy 8.3: L1, L2, L5, L6, L7, L8 vẫn bị chặn (L2, L7 là của bản 8.4)" kqs CHAN L1 L2 L5 L6 L7 L8
    ok "không phép nào HONG" no_hong
    tancong "$B" "$A"
    ok "chiều ngược lại (A=$B ở 8.3, B=$A ở 8.4): lo=0 chan=6 hong=0" has '^TONG lo=0 chan=6 hong=0$'
  else
    note "BỎ các khẳng định của PHP 8.4: không cài được (kho ngoài packages.sury.org)"
  fi

  route_block "$A" "A"

  section "A: nginx -T thật (định dạng mà parser của tpan đọc)"
  vroot bash -c 'nginx -T 2>/dev/null | grep -nE "^# configuration file|^[[:space:]]*(server_name|listen|root|fastcgi_pass|disable_symlinks)[[:space:]]"'
  ok "nginx -T: mỗi site một dòng server_name trong file tpan-<domain>.conf" bash -c 'grep -q "server_name $2;" <<<"$1" && grep -q "server_name $3;" <<<"$1" && grep -q "^[0-9]*:# configuration file /etc/nginx/sites-enabled/tpan-$2.conf:" <<<"$1"' _ "$OUT" "$A" "$B"

  ssl_block "A"
  part_end A
}

# ================================================================ PHẦN B: Apache

part_b() {
  part_begin "B: stack Apache từ máy sạch"
  reset_vm
  deploy_new
  vroot tpan stack install --web apache --yes
  ok "stack install --web apache thoát 0, job thành công" job_ok
  vexec systemctl is-active apache2 php8.3-fpm mariadb
  ok "apache2, php8.3-fpm, mariadb active" count_is 3 '^active$'
  vroot apache2ctl -S
  note "apache2ctl -S khi chưa có site nào (chỉ vhost mặc định, dạng gọn một dòng) ở ngay trên"
  site_add "$A"
  site_add "$B"
  web_assert "$A" "site A"
  web_assert "$B" "site B"
  vroot cat "/etc/apache2/sites-available/tpan-$A.conf"
  ok "vhost Apache gỡ PHP_VALUE và PHP_ADMIN_VALUE" bash -c 'grep -qF "ProxyFCGISetEnvIf \"true\" !PHP_VALUE" <<<"$1" && grep -qF "ProxyFCGISetEnvIf \"true\" !PHP_ADMIN_VALUE" <<<"$1"' _ "$OUT"
  ok "chế độ an-toan: AllowOverride Options=Indexes,MultiViews,SymLinksIfOwnerMatch và một dòng AllowOverrideList 55 tên, không có Options, SetHandler" bash -c 'grep -q "AllowOverride Options=Indexes,MultiViews,SymLinksIfOwnerMatch" <<<"$1" && [ "$(grep -c "^[[:space:]]*AllowOverrideList " <<<"$1")" -eq 1 ] && n=$(grep "^[[:space:]]*AllowOverrideList " <<<"$1" | wc -w) && echo "    số tên: $((n - 1))" && [ "$((n - 1))" -eq 55 ] && ! grep "^[[:space:]]*AllowOverrideList " <<<"$1" | grep -qwE "Options|SetHandler|AddHandler|Action|ProxyFCGISetEnvIf"' _ "$OUT"
  vroot python3 -c 'import sqlite3; c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True); [print(*r) for r in c.execute("SELECT domain, htaccess FROM sites ORDER BY domain")]'
  ok "bảng sites: hai site mới ở chế độ an-toan" count_is 2 ' an-toan$'

  section "B: bộ phép thử tấn công, A=$A bị chiếm, B=$B nạn nhân"
  tancong "$A" "$B"
  ok "bộ phép thử chạy hết (có dòng TONG)" has '^TONG lo=[0-9]+ chan=[0-9]+ hong=[0-9]+$'
  ok "L1, L2, L3, L4, L6, L7, L8, L9 bị chặn" kqs CHAN L1 L2 L3 L4 L6 L7 L8 L9
  ok "L5 không thuộc stack Apache (BO)" kqs BO L5
  ok "không phép nào HONG" no_hong
  ok "tổng: lo=0 chan=8 hong=0" has '^TONG lo=0 chan=8 hong=0$'

  section "B: tpan health"
  health_text
  ok "tpan health thoát 0" rc_is 0
  ok "tpan health không có dòng [lỗi] nào" no_loi
  health_json
  ok "health --json: không phép kiểm nào fail" no_fail
  ok "các phép kiểm M14 đạt: php.opcache:8.3, php.protectproc:8.3, web.server_name.unique, web.default.rejects" bash -c 'for i in php.opcache:8.3 php.protectproc:8.3 web.server_name.unique web.default.rejects; do jq -e --arg id "$i" "[.groups[].results[] | select(.id == \$id and .status == \"pass\")] | length == 1" "$1" >/dev/null || exit 1; done' _ "$HJ"
  ok "của site: log.perm, web.hardened, web.htaccess, isolation hai chiều đạt" bash -c 'for i in "log.perm:$2" "log.perm:$3" "web.hardened:$2" "web.hardened:$3" "web.htaccess:$2" "web.htaccess:$3" "isolation:$2:$3" "isolation:$3:$2"; do jq -e --arg id "$i" "[.groups[].results[] | select(.id == \$id and .status == \"pass\")] | length == 1" "$1" >/dev/null || exit 1; done' _ "$HJ" "$A" "$B"

  section "B: chạy lại tpan site add $A: không đổi gì"
  snap "$TMPD/b-truoc"
  site_add_q "$A"
  ok "chạy lại thoát 0, job thành công" job_ok
  ok "không bước nào chạy (không có dòng \"xong\")" hasnt '^  xong$'
  snap "$TMPD/b-sau"
  run diff "$TMPD/b-truoc" "$TMPD/b-sau"
  ok "máy không đổi (vhost, ini, drop-in, logrotate, quyền log, .htaccess, bảng sites)" rc_is 0

  section "B: logrotate thật trên Apache, log mới phải nằm ở access.log"
  rotate_assert "$A" b1
  health_json "$A"
  ok "log.perm:$A vẫn đạt sau khi xoay" check_status "log.perm:$A" pass

  section "B: thử thách ACME qua vhost Apache"
  vroot bash -c "$ACME_SCRIPT" _ "$A" b1
  ok "file thử thách trả 200 đúng nội dung qua tên miền của site" has '^http=200 than=noi-dung-thu-thach-b1$'

  section "B: apache2ctl -S thật (định dạng mà parser của tpan đọc)"
  vroot apache2ctl -S
  ok "apache2ctl -S: hai site, mỗi site một dòng trỏ về file tpan-<domain>.conf" bash -c 'grep -qE "port 80 namevhost $2 \(/etc/apache2/sites-enabled/tpan-$2.conf:[0-9]+\)" <<<"$1" && grep -qE "port 80 namevhost $3 \(/etc/apache2/sites-enabled/tpan-$3.conf:[0-9]+\)" <<<"$1"' _ "$OUT" "$A" "$B"
  part_end B
}

# ================================================================ PHẦN C: tính năng

mode_of() { vroot python3 -c 'import sqlite3, sys; c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True); print("che-do", sys.argv[1], c.execute("SELECT htaccess FROM sites WHERE domain = ?", (sys.argv[1],)).fetchone()[0])' "$1"; }

part_c() {
  part_begin "C: tính năng trên stack Apache (máy của phần B)"
  addrs
  vroot bash -c 'systemctl is-active apache2; ls -d /var/www/a.example.com/public /var/www/b.example.com/public; tpan version'
  ok "máy của phần B: apache2 active, có hai site, tpan $VER" bash -c 'grep -qx active <<<"$1" && [ "$(grep -c "^/var/www/" <<<"$1")" -eq 2 ] && grep -q "^tpan $2 " <<<"$1"' _ "$OUT" "$VER"
  if ! has '^active$' || ! has "^/var/www/$A/public"; then
    printf 'Phần C cần máy của phần B (chạy: scripts/e2e-m14.sh apache tinh-nang).\n'
    part_end C
    return
  fi
  local F="/var/www/$A/public/.htaccess" VH="/etc/apache2/sites-available/tpan-$A.conf"

  section "C1: chế độ tat"
  vroot tpan site set "$A" --htaccess tat --dry-run
  ok "xem trước (--dry-run) thoát 0, nêu đổi từ an-toan sang tat" bash -c '[ "$2" = 0 ] && grep -q "từ an-toan sang tat" <<<"$1"' _ "$OUT" "$RC"
  mode_of "$A"
  ok "xem trước không đổi gì: bảng sites vẫn an-toan" has "^che-do $A an-toan$"
  vroot tpan site set "$A" --htaccess tat --yes
  ok "site set --htaccess tat: job thành công" job_ok
  mode_of "$A"
  ok "bảng sites: $A ở chế độ tat" has "^che-do $A tat$"
  vroot grep -nE 'AllowOverride|FallbackResource' "$VH"
  ok "vhost: AllowOverride None, FallbackResource /index.php, không AllowOverrideList" bash -c 'grep -q "FallbackResource /index.php" <<<"$1" && grep -q "AllowOverride None" <<<"$1" && ! grep -q "AllowOverrideList" <<<"$1" && ! grep -q "AllowOverride \(All\|Options\)" <<<"$1"' _ "$OUT"
  web_assert "$A" "chế độ tat"
  ht "$A" them-dau 'SetHandler "proxy:unix:/run/php/khong-co.sock|fcgi://localhost"'
  web "$A"
  ok ".htaccess có SetHandler mà site vẫn chạy đủ (Apache không đọc .htaccess ở chế độ tat)" web_ok "$A"
  health_json "$A"
  ok "health $A ở chế độ tat: không fail" no_fail
  vroot tpan site set "$A" --htaccess an-toan --yes
  ok "còn dòng SetHandler mà đòi về an-toan: bị từ chối (thoát khác 0)" rc_not 0
  ok "lời từ chối nêu file, số dòng, chỉ thị: public/.htaccess:1: SetHandler" has "/var/www/$A/public/\\.htaccess:1: SetHandler"
  mode_of "$A"
  ok "vẫn ở chế độ tat" has "^che-do $A tat$"
  ht "$A" go 'SetHandler "proxy:unix:/run/php/khong-co.sock|fcgi://localhost"'
  vroot tpan site set "$A" --htaccess an-toan --yes
  ok "gỡ dòng đó rồi về an-toan: job thành công" job_ok
  mode_of "$A"
  ok "bảng sites: $A về an-toan" has "^che-do $A an-toan$"
  web_assert "$A" "về an-toan"
  vroot tpan site set "$A" --htaccess an-toan --yes --json
  ok "đặt lại đúng chế độ đang có: từ chối htaccess_same" bash -c 'jq -e ".error.code == \"htaccess_same\"" <<<"$1" >/dev/null 2>&1 || grep -q "htaccess_same" <<<"$1"' _ "$OUT"

  section "C2: chế độ day-du, cảnh báo, và bộ phép thử lộ lại đúng như cảnh báo nói"
  vroot tpan site set "$A" --htaccess day-du --dry-run
  ok "bản xem trước của day-du nói rõ rủi ro (ghi được cấu hình Apache, có thể chạy mã dưới user của site khác)" hasF 'có thể chạy mã dưới user của site khác'
  vroot tpan site set "$A" --htaccess day-du --yes
  ok "site set --htaccess day-du: job thành công" job_ok
  vroot grep -nE 'AllowOverride|ProxyFCGISetEnvIf' "$VH"
  ok "vhost: AllowOverride All; hai dòng gỡ PHP_VALUE, PHP_ADMIN_VALUE vẫn có" bash -c 'grep -q "AllowOverride All" <<<"$1" && [ "$(grep -c "ProxyFCGISetEnvIf \"true\" !PHP_" <<<"$1")" -eq 2 ]' _ "$OUT"
  health_text "$A"
  ok "tpan health $A có [cảnh báo] về chế độ day-du" has '^\[cảnh báo\].*day-du'
  ok "không có [lỗi]" no_loi
  health_json "$A"
  ok "web.htaccess:$A là warn" check_status "web.htaccess:$A" warn
  tancong "$A" "$B"
  ok "A ở day-du: L4 và L9 LỘ lại (đúng như cảnh báo)" kqs LO L4 L9
  ok "L1, L2, L3, L6, L7, L8 vẫn bị chặn (không phụ thuộc chế độ .htaccess)" kqs CHAN L1 L2 L3 L6 L7 L8
  note "$(tong)"
  ht "$A" them-dau 'Options +FollowSymLinks'
  web "$A"
  ok "ở day-du, 'Options +FollowSymLinks' trong .htaccess không làm site lỗi" has '^trang-chu=200$'
  vroot tpan site set "$A" --htaccess an-toan --yes
  ok "có dòng ngoài danh sách mà đòi sang an-toan: bị từ chối (thoát khác 0)" rc_not 0
  # Phép quét nêu chỉ thị Options kèm tên tuỳ chọn bị cấm, không kèm dấu +/-.
  ok "nêu đúng file, số dòng, chỉ thị và tuỳ chọn: public/.htaccess:1: Options FollowSymLinks" has "/var/www/$A/public/\\.htaccess:1: Options FollowSymLinks"
  ok "nêu hai lối ra (sửa .htaccess, hoặc giữ day-du)" bash -c 'grep -q "Hai lối ra" <<<"$1" && grep -q -- "--htaccess day-du" <<<"$1"' _ "$OUT"
  vjson tpan site set "$A" --htaccess an-toan --yes --json
  ok "--json: mã từ chối htaccess_blocked" refused htaccess_blocked
  web "$A"
  ok "bị từ chối thì site không đổi: vẫn 200, vẫn day-du" has '^trang-chu=200$'
  ht "$A" go 'Options +FollowSymLinks'
  vroot tpan site set "$A" --htaccess an-toan --yes
  ok "gỡ dòng đó: về an-toan, job thành công" job_ok
  tancong "$A" "$B"
  ok "về an-toan: lo=0 chan=8 hong=0" has '^TONG lo=0 chan=8 hong=0$'

  section "C3: ở an-toan, plugin ghi thêm dòng lạ vào .htaccess"
  ht "$A" them-cuoi 'php_value memory_limit 256M'
  vroot grep -n 'php_value' "$F"
  local ln
  ln="$(sed -nE 's/^([0-9]+):php_value.*/\1/p' <<<"$OUT" | head -1)"
  web "$A"
  note "site $A khi .htaccess có chỉ thị ngoài danh sách: $(head -1 <<<"$OUT") (đặc tả: Apache trả 500)"
  health_text "$A"
  ok "tpan health $A thoát khác 0" rc_not 0
  ok "có [lỗi] của phép quét .htaccess, nêu đúng file và dòng $ln, chỉ thị php_value" bash -c 'grep -q "^\[lỗi\]" <<<"$1" && grep -qF "/var/www/$2/public/.htaccess:$3: php_value" <<<"$1"' _ "$OUT" "$A" "$ln"
  health_json "$A"
  ok "web.htaccess:$A là fail" check_status "web.htaccess:$A" fail
  ht "$A" go 'php_value memory_limit 256M'
  health_json "$A"
  ok "gỡ dòng đó: health $A hết fail, web.htaccess đạt" bash -c 'jq -e --arg id "web.htaccess:$2" "([.groups[].results[] | select(.status == \"fail\")] | length == 0) and ([.groups[].results[] | select(.id == \$id and .status == \"pass\")] | length == 1)" "$1" >/dev/null' _ "$HJ" "$A"
  web_assert "$A" "sau khi gỡ dòng lạ"

  route_block "$A" "C4"

  section "C4: route explain cho site tạm khoá"
  vroot tpan site suspend "$B" --yes
  ok "site suspend $B: job thành công" job_ok
  vroot tpan route explain "$B"
  ok "route explain site tạm khoá thoát 0" rc_is 0
  ok "nói site đang tạm khoá, vhost trả 403" hasF 'Site đang tạm khoá'
  vjson tpan route explain "$B" --json
  ok "--json: dừng sau vhost (chỉ còn dns, vhost, http), chặng http đạt (403 kèm trang tạm khoá)" jout '[.hops[].key] == ["dns","vhost","http"] and .hops[2].status == "pass"'
  vroot tpan site set "$B" --htaccess tat --yes --json
  ok "đổi chế độ .htaccess của site tạm khoá: bị từ chối" rc_not 0
  vroot tpan site unsuspend "$B" --yes
  ok "site unsuspend $B: job thành công" job_ok
  web_assert "$B" "sau khi mở khoá"
  vroot grep -cE '^[[:space:]]*(AllowOverrideList |ProxyFCGISetEnvIf "true" !PHP_)' "/etc/apache2/sites-available/tpan-$B.conf"
  ok "vhost của $B sau khi mở khoá vẫn là bản an-toan (1 dòng AllowOverrideList, 2 dòng gỡ biến PHP, không tính chú thích)" out_is 3

  section "C5: API (tpan setup, đăng nhập admin)"
  install_run "$VM" root tpan setup --yes
  ok "setup thoát 0" rc_is 0
  ok "mật khẩu quản trị in đúng một lần" test "$PW_HITS" -eq 1
  save_pw admin "$PW"
  fetch_cert
  ok "panel trả lời qua https (--cacert)" rc_is 0
  local JADM JA jid
  JADM="$(new_jar adm)"
  login "$JADM" admin "$TMPD/pw.admin"
  ok "admin đăng nhập: 204" code_is 204
  API_MAX=2500 api "$JADM" GET "/api/v1/sites/$A/route"
  ok "GET /sites/$A/route: 200, 8 chặng đúng thứ tự" bash -c '[ "$1" = 200 ] && jq -e --argjson k "$3" "[.hops[].key] == \$k and .domain == \"$4\" and .web == \"apache\"" "$2" >/dev/null' _ "$CODE" "$TMPD/body" "$HOPS8" "$A"
  ok "dns là warn, 7 chặng còn lại pass, mỗi chặng có evidence là mảng" bq '.hops[0].status == "warn" and ([.hops[1:][] | .status] | all(. == "pass")) and ([.hops[] | .evidence | type] | all(. == "array"))'
  health_json
  ok "health (CLI) chạy xong, không fail" no_fail
  API_MAX=1500 api "$JADM" GET /api/v1/security/isolation
  ok "GET /security/isolation: 200, web apache, hai site" bash -c '[ "$1" = 200 ] && jq -e --arg a "$3" --arg b "$4" ".web == \"apache\" and .sites == [\$a, \$b]" "$2" >/dev/null' _ "$CODE" "$TMPD/body" "$A" "$B"
  ok "ma trận có 2 ô, cả hai đạt (kết quả của lần health vừa chạy)" bq '(.cells | length == 2) and ([.cells[].result.status] | all(. == "pass")) and ([.cells[] | "\(.from)>\(.to)"] | sort == ["a.example.com>b.example.com", "b.example.com>a.example.com"])'
  ok "phép kiểm cấp máy: php.opcache, php.protectproc, web.server_name.unique, web.default.rejects đều đạt" bq '([.machine[].id | split(":")[0]] | unique) == ["php.opcache", "php.protectproc", "web.default.rejects", "web.server_name.unique"] and ([.machine[].status] | all(. == "pass"))'
  ok "per_site có log.perm và web.hardened của hai site, đều đạt" bq '(.per_site | length == 4) and ([.per_site[].status] | all(. == "pass"))'
  ok "htaccess: hai site ở an-toan kèm kết quả web.htaccess đạt; needs_harden false" bq '([.htaccess[] | .mode] == ["an-toan", "an-toan"]) and ([.htaccess[].result.status] | all(. == "pass")) and .needs_harden == false'
  api "$JADM" POST "/api/v1/sites/$A/htaccess?dry_run=1" '{"mode":"tat"}'
  ok "POST /sites/$A/htaccess?dry_run=1: 200, Preview của Plan site.htaccess có các bước" bash -c '[ "$1" = 200 ] && jq -e ".kind == \"site.htaccess\" and .target == \"$3\" and (.steps | length >= 1)" "$2" >/dev/null' _ "$CODE" "$TMPD/body" "$A"
  mode_of "$A"
  ok "dry_run không đổi gì: $A vẫn an-toan" has "^che-do $A an-toan$"
  api "$JADM" POST "/api/v1/sites/$A/htaccess?dry_run=1" '{"mode":"lung-tung"}'
  ok "mode lạ: 400 htaccess_mode_invalid" expect 400 htaccess_mode_invalid
  api "$JADM" POST "/api/v1/sites/$A/htaccess?dry_run=1" '{"mode":"an-toan"}'
  ok "đặt lại chế độ đang có: 409 htaccess_same" expect 409 htaccess_same
  api "$JADM" POST "/api/v1/security/harden?dry_run=1"
  ok "POST /security/harden?dry_run=1: 200, có steps, switched và kept là mảng (máy đã siết: cả hai rỗng)" bash -c '[ "$1" = 200 ] && jq -e ".kind == \"security.harden\" and (.steps | length >= 1) and .switched == [] and .kept == []" "$2" >/dev/null' _ "$CODE" "$TMPD/body"
  ok "mọi bước của harden đều đã đúng (máy mới không còn gì để vá)" bq '[.steps[].done] | all'

  section "C5: user thường"
  json_secret a '.password' "$VM" root tpan user add "$UA" --full-name "Khach A" --email a@example.com --yes --json
  ok "user add $UA: thoát 0, mật khẩu sinh một lần (đã che)" bash -c '[ "$1" = 0 ] && [ -n "$2" ]' _ "$RC" "$JS_PW"
  site_add "$C" --owner "$UA"
  JA="$(new_jar a)"
  login "$JA" "$UA" "$TMPD/pw.a"
  ok "$UA đăng nhập: 204" code_is 204
  api "$JA" GET "/api/v1/sites/$A/route"
  ok "user thường gọi /route của site người khác: 404 site_not_found" expect 404 site_not_found
  API_MAX=600 api "$JA" GET "/api/v1/sites/$C/route"
  ok "user thường gọi /route của site mình: 200, 8 chặng" bash -c '[ "$1" = 200 ] && jq -e "(.hops | length) == 8" "$2" >/dev/null' _ "$CODE" "$TMPD/body"
  api "$JA" POST "/api/v1/sites/$A/htaccess?dry_run=1" '{"mode":"tat"}'
  ok "user thường đổi chế độ site người khác: 404 site_not_found" expect 404 site_not_found
  api "$JA" POST "/api/v1/sites/$C/htaccess" '{"mode":"day-du"}'
  ok "user thường là chủ đặt day-du: 403 forbidden" expect 403 forbidden
  api "$JA" POST "/api/v1/sites/$C/htaccess?dry_run=1" '{"mode":"day-du"}'
  ok "kể cả dry_run: 403 forbidden" expect 403 forbidden
  mode_of "$C"
  ok "$C vẫn an-toan" has "^che-do $C an-toan$"
  api "$JA" GET /api/v1/security/isolation
  ok "user thường gọi /security/isolation: 403 forbidden" expect 403 forbidden
  api "$JA" POST "/api/v1/security/harden?dry_run=1"
  ok "user thường gọi /security/harden: 403 forbidden" expect 403 forbidden
  api "$JA" POST "/api/v1/sites/$C/htaccess" '{"mode":"tat"}'
  ok "chủ site đặt tat cho site mình (chạy thật): 202" code_is 202
  jid="$(jb '.job_id // empty')"
  wait_job "$JA" "$jid"
  ok "job site.htaccess thành công" test "$JOBST" = succeeded
  mode_of "$C"
  ok "$C ở chế độ tat" has "^che-do $C tat$"
  web_assert "$C" "site $C ở chế độ tat (đặt qua API)"
  api "$JADM" POST "/api/v1/sites/$C/htaccess" '{"mode":"day-du"}'
  ok "admin đặt day-du cho $C: 202" code_is 202
  jid="$(jb '.job_id // empty')"
  wait_job "$JADM" "$jid"
  ok "job thành công" test "$JOBST" = succeeded
  health_json
  API_MAX=400 api "$JADM" GET /api/v1/security/isolation
  ok "ma trận giờ có 6 ô (3 site), đều đạt; $C ở day-du nên needs_harden true và web.htaccess của nó là warn" bq --arg c "$C" '(.cells | length == 6) and ([.cells[].result.status] | all(. == "pass")) and .needs_harden == true and ([.htaccess[] | select(.domain == $c)][0] | .mode == "day-du" and .result.status == "warn")'
  api "$JADM" POST "/api/v1/security/harden?dry_run=1"
  ok "harden?dry_run=1 giờ nêu $C trong switched (.htaccess của nó sạch)" bq --arg c "$C" '.switched == [$c] and .kept == []'
  vroot tpan security harden --yes
  ok "tpan security harden: job thành công, chuyển $C về an-toan" bash -c '[ "$2" = 0 ] && grep -q "^Job .*: thành công" <<<"$1" && grep -q "Chuyển sang an-toan (1 site" <<<"$1"' _ "$OUT" "$RC"
  mode_of "$C"
  ok "$C về an-toan" has "^che-do $C an-toan$"
  health_text
  ok "cuối phần C: tpan health không có [lỗi]" no_loi
  tancong "$A" "$B"
  ok "cuối phần C: bộ phép thử lo=0 chan=8 hong=0" has '^TONG lo=0 chan=8 hong=0$'

  ssl_block "C6"
  part_end C
}

# ================================================================ PHẦN D: nâng cấp

# upgrade <nginx|apache>
upgrade() {
  local web=$1 arch t0 t1 u
  part_begin "D ($web): nâng từ $OLD_VER lên $VER rồi tpan security harden"
  reset_vm
  arch="$("$VM" exec dpkg --print-architecture </dev/null | tr -d '\r')"
  run bash -c 'cd "$1" && ls -l "tpan-linux-$2" SHA256SUMS && grep " tpan-linux-$2\$" SHA256SUMS | shasum -a 256 -c -' _ "$OLD_DIR" "$arch"
  ok "file chạy $OLD_VER ($arch) của bản phát hành: sha256 khớp SHA256SUMS" rc_is 0
  run multipass transfer "$OLD_DIR/tpan-linux-$arch" "$VMNAME:/tmp/tpan"
  vroot install -m 0755 /tmp/tpan /usr/local/bin/tpan
  vexec tpan version
  ok "tpan version là $OLD_VER" has "^tpan $OLD_VER "
  vroot tpan stack install --web "$web" --yes
  ok "$OLD_VER: stack install --web $web thành công" job_ok
  site_add "$A"
  site_add "$B"
  web_assert "$A" "$OLD_VER, site A"
  web_assert "$B" "$OLD_VER, site B"

  section "D ($web): TRƯỚC, bộ phép thử trên $OLD_VER phải LỘ"
  tancong "$A" "$B"
  ok "bộ phép thử chạy hết (có dòng TONG)" has '^TONG lo=[0-9]+ chan=[0-9]+ hong=[0-9]+$'
  if [ "$web" = nginx ]; then
    ok "$OLD_VER nginx: L1, L2, L5, L6, L7, L8 LỘ" kqs LO L1 L2 L5 L6 L7 L8
  else
    ok "$OLD_VER Apache: L1, L2, L3, L4, L7, L8, L9 LỘ" kqs LO L1 L2 L3 L4 L7 L8 L9
    # L6 trên Apache bản cũ tuỳ lúc đo: log do Apache tự tạo là root:adm 0640
    # (không đọc thẳng được), nhưng thư mục log là 0755 và file do tpan bản cũ
    # hay logrotate tạo thì ai cũng đọc được. Ghi nhận kết quả, không khẳng định.
    note "$OLD_VER Apache, L6: $(grep -E '^KQ L6 ' <<<"$OUT")"
  fi
  note "TRƯỚC ($OLD_VER, $web): $(tong); phép HONG: $(grep -E '^KQ [A-Z0-9]+ +HONG' <<<"$OUT" | tr '\n' ';')"
  if [ "$web" = apache ]; then
    ht "$B" them-dau 'Options +FollowSymLinks'
    web "$B"
    ok "(dựng site vướng) $B có 'Options +FollowSymLinks' trong .htaccess, vẫn 200 trên $OLD_VER" has '^trang-chu=200$'
  fi

  section "D ($web): thay file chạy bằng bản $VER"
  deploy_new
  health_text
  ok "tpan health thoát khác 0" rc_not 0
  ok "có dòng [lỗi]" has '^\[lỗi\]'
  ok "lời nhắn chỉ sang lệnh tpan security harden" hasF 'tpan security harden'
  health_json
  ok "php.opcache:8.3 là fail" check_status php.opcache:8.3 fail
  ok "log.perm của hai site là fail" bash -c 'for i in "log.perm:$2" "log.perm:$3"; do jq -e --arg id "$i" "[.groups[].results[] | select(.id == \$id and .status == \"fail\")] | length == 1" "$1" >/dev/null || exit 1; done' _ "$HJ" "$A" "$B"
  ok "web.hardened của hai site là fail" bash -c 'for i in "web.hardened:$2" "web.hardened:$3"; do jq -e --arg id "$i" "[.groups[].results[] | select(.id == \$id and .status == \"fail\")] | length == 1" "$1" >/dev/null || exit 1; done' _ "$HJ" "$A" "$B"
  ok "php.protectproc:8.3 là warn (quyền file vẫn giữ)" check_status php.protectproc:8.3 warn
  jq -r '.groups[].results[] | select(.id | test("^(php[.]opcache|php[.]protectproc|log[.]perm|web[.]hardened|web[.]htaccess):")) | "\(.status) \(.id): \(.summary.vi)\n    nguyên nhân: \(.cause.vi // "-")\n    sửa tay: \((.manual_fix // []) | join(" ; ") | .[:300])\n    sửa tự động: \(.auto_fix // "-")"' "$HJ" | mask_str
  printf '\n'
  mode_of "$A"
  ok "migration 1400: site có từ trước mang chế độ day-du" has "^che-do $A day-du$"

  section "D ($web): thao tác thường trên site cũ trước khi harden"
  snap "$TMPD/d-truoc"
  site_add_q "$A"
  printf '(Nguyên văn output của lần chạy lại site add trên site cũ ở ngay trên.)\n'
  ok "chạy lại site add $A trước harden: thoát khác 0" rc_not 0
  ok "lỗi ở kiểm chứng và đã hoàn tác" bash -c 'grep -qi "hoàn tác" <<<"$1"' _ "$OUT"
  ok "lời nhắn chỉ sang tpan security harden" hasF 'tpan security harden'
  snap "$TMPD/d-sau-site-add"
  run diff "$TMPD/d-truoc" "$TMPD/d-sau-site-add"
  ok "sau khi hoàn tác máy như trước (vhost, quyền log, bảng sites)" rc_is 0
  web "$A"
  ok "site $A vẫn 200" has '^trang-chu=200$'

  section "D ($web): harden lỗi có chủ ý (vhost hỏng cú pháp ngoài tpan): máy phải về như trước"
  vroot bash -c "$BROKEN_SCRIPT" _ dat
  ok "(đồ nghề) đặt vhost hỏng, kiểm cú pháp của web server giờ lỗi" hasnt '^kiểm cú pháp mã 0$'
  vroot tpan security harden --yes
  ok "harden thoát khác 0" rc_not 0
  ok "job đã hoàn tác" bash -c 'grep -qi "hoàn tác" <<<"$1"' _ "$OUT"
  ok "không báo hoàn tác lỗi" hasnt 'undo_failed|hoàn tác lỗi|hoàn tác không'
  snap "$TMPD/d-sau-harden-loi"
  run diff "$TMPD/d-truoc" "$TMPD/d-sau-harden-loi"
  ok "sau khi hoàn tác: vhost, file ini, drop-in, logrotate, quyền log, ProtectProc, bảng sites đúng như trước" rc_is 0
  web "$A"
  ok "site $A vẫn 200 sau khi harden hoàn tác" has '^trang-chu=200$'
  web "$B"
  ok "site $B vẫn 200 sau khi harden hoàn tác" has '^trang-chu=200$'
  vroot bash -c "$BROKEN_SCRIPT" _ go
  ok "(đồ nghề) gỡ vhost hỏng, kiểm cú pháp đạt lại" has '^kiểm cú pháp mã 0$'

  section "D ($web): tpan security harden --dry-run"
  vroot tpan security harden --dry-run
  ok "--dry-run thoát 0" rc_is 0
  ok "có bước sẽ chạy (không phải 0 bước)" hasnt '^0 bước sẽ chạy'
  if [ "$web" = nginx ]; then
    ok "in 2 site chuyển sang an-toan" bash -c 'grep -q "Chuyển sang an-toan (2 site" <<<"$1" && grep -qx "    $2" <<<"$1" && grep -qx "    $3" <<<"$1"' _ "$OUT" "$A" "$B"
    ok "không site nào giữ day-du" hasnt 'Giữ day-du'
  else
    ok "in $A chuyển sang an-toan" bash -c 'grep -q "Chuyển sang an-toan (1 site" <<<"$1" && grep -qx "    $2" <<<"$1"' _ "$OUT" "$A"
    ok "in $B giữ day-du kèm dòng vướng (file, số dòng, chỉ thị)" bash -c 'grep -q "Giữ day-du (1 site)" <<<"$1" && grep -qF "/var/www/$2/public/.htaccess:1: Options FollowSymLinks" <<<"$1"' _ "$OUT" "$B"
  fi
  snap "$TMPD/d-sau-dry"
  run diff "$TMPD/d-truoc" "$TMPD/d-sau-dry"
  ok "--dry-run không đổi gì trên máy" rc_is 0

  section "D ($web): tpan security harden --yes"
  t0="$(date +%s)"
  vroot tpan security harden --yes
  t1="$(date +%s)"
  ok "harden thoát 0, job thành công" job_ok
  note "thời gian chạy tpan security harden với hai site ($web): $((t1 - t0)) giây (tính cả lần gọi qua multipass)"
  vroot tpan security harden --dry-run
  ok "chạy lại (xem trước): 0 bước sẽ chạy" has '^0 bước sẽ chạy'
  vroot tpan security harden --yes
  ok "chạy lại lần hai: thoát 0, job thành công" job_ok
  ok "lần hai không bước nào chạy (không có dòng \"xong\")" hasnt '^  xong$'
  mode_of "$A"
  ok "$A đã chuyển sang an-toan" has "^che-do $A an-toan$"
  mode_of "$B"
  if [ "$web" = nginx ]; then
    ok "$B đã chuyển sang an-toan" has "^che-do $B an-toan$"
  else
    ok "$B (site vướng) giữ day-du" has "^che-do $B day-du$"
  fi

  section "D ($web): SAU, health và bộ phép thử"
  health_text
  ok "tpan health không còn dòng [lỗi] nào" no_loi
  ok "tpan health thoát 0" rc_is 0
  health_json
  ok "health --json: không fail" no_fail
  ok "php.opcache:8.3, php.protectproc:8.3, log.perm và web.hardened của hai site đều đạt" bash -c 'for i in php.opcache:8.3 php.protectproc:8.3 "log.perm:$2" "log.perm:$3" "web.hardened:$2" "web.hardened:$3"; do jq -e --arg id "$i" "[.groups[].results[] | select(.id == \$id and .status == \"pass\")] | length == 1" "$1" >/dev/null || exit 1; done' _ "$HJ" "$A" "$B"
  if [ "$web" = apache ]; then
    ok "site vướng $B còn cảnh báo web.htaccess; site $A thì đạt" bash -c 'jq -e --arg a "web.htaccess:$2" --arg b "web.htaccess:$3" "([.groups[].results[] | select(.id == \$a and .status == \"pass\")] | length == 1) and ([.groups[].results[] | select(.id == \$b and .status == \"warn\")] | length == 1)" "$1" >/dev/null' _ "$HJ" "$A" "$B"
  fi
  tancong "$A" "$B"
  if [ "$web" = nginx ]; then
    ok "SAU nginx: L1, L2, L5, L6, L7, L8 bị chặn" kqs CHAN L1 L2 L5 L6 L7 L8
    ok "tổng: lo=0 chan=6 hong=0" has '^TONG lo=0 chan=6 hong=0$'
  else
    ok "SAU Apache (A=$A đã chuyển an-toan): L1, L2, L3, L4, L6, L7, L8, L9 bị chặn" kqs CHAN L1 L2 L3 L4 L6 L7 L8 L9
    ok "tổng: lo=0 chan=8 hong=0" has '^TONG lo=0 chan=8 hong=0$'
    tancong "$B" "$A"
    ok "A=$B (site vướng, giữ day-du): L4 và L9 còn LỘ, đúng như cảnh báo" kqs LO L4 L9
    ok "A=$B: L1, L2, L3, L6, L7, L8 bị chặn" kqs CHAN L1 L2 L3 L6 L7 L8
    note "A=$B (day-du): $(tong); phép HONG: $(grep -E '^KQ [A-Z0-9]+ +HONG' <<<"$OUT" | tr '\n' ';')"
  fi

  section "D ($web): log sau harden và sau một lần logrotate"
  u="$(user_of "$A")"
  vroot bash -c 'd=$1; dir=/var/log/tpan/sites/$d; n1=$(wc -l <"$dir/access.log"); for i in 1 2 3; do curl -s -o /dev/null -m 10 --resolve "$d:80:127.0.0.1" "http://$d/?e2e=sau-harden-$i"; done; sleep 1; n2=$(wc -l <"$dir/access.log"); echo "access.log: $n1 -> $n2 dòng, $(grep -c "e2e=sau-harden-" "$dir/access.log") dòng mang dấu sau-harden"; stat -c "%U:%G %a %n" "$dir" "$dir"/*' _ "$A"
  ok "sau harden web server vẫn ghi access.log của $A (3 request mới)" has 'access.log: [0-9]+ -> [0-9]+ dòng, 3 dòng mang dấu sau-harden$'
  rotate_assert "$A" d1
  web_assert "$A" "SAU, site A"
  web_assert "$B" "SAU, site B"
  health_json
  ok "cuối phần: health không fail" no_fail
  part_end "D ($web)"
}

leak_check() {
  [ -n "${E2E_LOG:-}" ] && [ -f "$E2E_LOG" ] || return 0
  section "bí mật, IP, tên user Mac, cookie có lọt vào chính file log không"
  local n=0 s
  while IFS= read -r s; do
    [ -n "$s" ] || continue
    n=$((n + $(grep -cF -- "$s" "$E2E_LOG")))
  done <"$SECRETS"
  printf 'Số bí mật đã gặp: %s; số dòng của log chứa một trong số đó: %s\n' "$(grep -c . "$SECRETS")" "$n"
  ok "log không chứa mật khẩu hay cookie nào" test "$n" -eq 0
  ok "mọi trường password trong log đều đã che" bash -c '! grep -E "\"(db_|admin_)?password\": *\"[^<\"]" "$1" | grep -vq "có (đã che)"' _ "$E2E_LOG"
  ok "log không chứa IP máy ảo (IPv4)" bash -c '[ -z "$1" ] || ! grep -qF -- "$1" "$2"' _ "$VM_IP" "$E2E_LOG"
  ok "log không chứa mạng Multipass" bash -c '[ -z "$1" ] || ! grep -qF -- "$1" "$2"' _ "$NET4" "$E2E_LOG"
  ok "log không chứa IPv6 của máy ảo" bash -c '[ -z "$1" ] || ! grep -qiF -- "$1" "$2"' _ "$V6PFX" "$E2E_LOG"
  ok "log không chứa IP của Mac" bash -c '[ -z "$1" ] || ! grep -qF -- "$1" "$2"' _ "$HOST_IP" "$E2E_LOG"
  ok "log không chứa thư mục nhà hay tên user Mac" bash -c '! grep -qF -- "$1" "$3" && ! grep -qF -- "$2" "$3"' _ "$HOME" "$USER" "$E2E_LOG"
}

[ $# -ge 1 ] || { sed -n '2,13p' "$0" | sed 's/^# \{0,1\}//'; exit 2; }
PARTS=()
for p in "$@"; do
  case "$p" in
    nginx | apache | tinh-nang | nang-cap-nginx | nang-cap-apache) PARTS+=("$p") ;;
    nang-cap) PARTS+=(nang-cap-nginx nang-cap-apache) ;;
    chinh) PARTS+=(nginx apache tinh-nang) ;;
    all) PARTS+=(nginx apache tinh-nang nang-cap-nginx nang-cap-apache) ;;
    *)
      sed -n '2,13p' "$0" | sed 's/^# \{0,1\}//'
      exit 2
      ;;
  esac
done

printf 'tpan của cây: %s; bản cũ: %s; máy ảo: %s; các phần: %s\n' "$VER" "$OLD_VER" "$VMNAME" "${PARTS[*]}"
for p in "${PARTS[@]}"; do
  case "$p" in
    nginx) part_a ;;
    apache) part_b ;;
    tinh-nang) part_c ;;
    nang-cap-nginx) upgrade nginx ;;
    nang-cap-apache) upgrade apache ;;
  esac
done

leak_check
section "TỔNG KẾT: $PASSES khẳng định đạt, $FAILS khẳng định không đạt"
[ "$FAILS" -eq 0 ]
