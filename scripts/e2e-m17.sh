#!/usr/bin/env bash
# e2e của mốc M17 (lồng theo site: trần RAM, CPU, tiến trình, đĩa của gói, số
# liệu và gợi ý trần, thử tấn công; bước 20 ở mục 7 của
# docs/thay-doi/M17-thiet-ke.md) trên máy ảo (tên trong file .tpan-vm; không có
# thì tpan-test). Chạy trên Mac, trong thư mục repo, một hoặc nhiều phần theo
# thứ tự gõ:
#
#   scripts/e2e-m17.sh nginx      stack nginx từ máy sạch
#   scripts/e2e-m17.sh apache     stack Apache từ máy sạch (bỏ phần khởi động lại máy)
#   scripts/e2e-m17.sh nang-cap   nâng từ gói v0.2.0 đã phát hành lên bản của cây
#   scripts/e2e-m17.sh chinh      = nginx apache
#   scripts/e2e-m17.sh all        = chinh nang-cap
#
# nginx và apache (mỗi phần từ máy sạch): gói long (RAM 200, CPU 50%, 40 tiến
#   trình, 3 worker, lồng bật); user khach; site A, B của khach tự vào lồng sau
#   khi tạo; site O của admin ở ngoài lồng (đối chứng, bất biến 1); site R của
#   khach2 là chỗ thử vào lồng, ra lồng (bất biến 2, 8). Các khẳng định: A thấy gì
#   từ PHP; bốn phép quá tải (RAM 400 MB, 200 tiến trình, CPU lúc máy bận, đĩa)
#   mà B không rớt request nào; số liệu đọc ngay khớp cgroup, mẫu và gợi ý trần;
#   thử tấn công (tpan isolation probe), kill -9 giữa chừng rồi --cleanup; A giữ
#   lồng sau đổi PHP, tạm khoá, backup rồi khôi phục, thùng rác rồi khôi phục,
#   chuyển stack, xin SSL (pebble); sửa tay file slice, sửa gói, hạ trần RAM dưới
#   mức đang dùng rồi nâng lại, site trả 500 không chặn được hạ gói (bất biến 4,
#   11, 12); vòng curl trong lúc cage enable và cage disable; phân quyền API;
#   xoá hẳn một site có lồng; (nginx) khởi động lại máy.
# nang-cap: cài gói v0.2.0 thật, stack, hai site, ghi sha256 mọi file dưới
#   /etc/nginx, /etc/php, /etc/cron.d, /etc/systemd/system; nâng bằng
#   `tpan self-update --url` của bản cũ; sha256 không đổi, health như trước,
#   không unit tpan-cage-*; bật lồng cho gói, hai site 200 trong lồng; tắt lồng,
#   sha256 về đúng như trước; self-update --rollback bị từ chối cage_active khi
#   còn site trong lồng. Đặt E2E_UP_WEB=apache để nâng trên Apache.
#
# ĐỒ NGHỀ TEST, CHỈ TRONG MÁY ẢO: pebble (máy chủ ACME thử, unit tpan-e2e-pebble)
# và CA thử trong kho tin cậy; dòng /etc/hosts cho site SSL; file PHP thử, vòng
# curl nền (unit e2e-m17-vong) và mu-plugin trả 500 (xem scripts/e2e-m17/*.sh,
# mỗi file chạy bằng `scripts/vm.sh root bash -s -- <tham số> < file`).
#
# File chạy v0.2.0: thư mục OLD_DIR (mặc định dist/release/v0.2.0, dist/ không
# vào git) có tpan-linux-<arch>, install.sh và SHA256SUMS của bản phát hành.
#
# Đặt E2E_LOG=<file log> để cuối lần chạy script tự tìm trong chính file log
# đó: mọi mật khẩu và cookie đã gặp, IP máy ảo (IPv4, IPv6), IP của Mac, thư
# mục nhà và tên user Mac (phải là 0 lần).
#
# LỚP BỌC LỆNH MÁY ẢO (vm_cap): như e2e-m14.sh. Mọi lệnh `scripts/vm.sh exec|root
# ...` in trong log chạy bằng `multipass exec` qua một lớp bọc: output ghi vào
# file TRONG máy ảo, ngắn thì in về trong cùng lần gọi, dài thì lấy về bằng
# `multipass transfer`, vì `multipass exec` trên Mac có lúc treo khi output dài.
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
A=a.example.com  # của khach, vào lồng khi tạo
B=b.example.com  # của khach, vào lồng khi tạo
O=o.example.com  # của admin, luôn ở ngoài lồng (đối chứng)
R=r.example.com  # của khach2, chỗ thử vào lồng và ra lồng
UK=khach
UK2=khach2
PKL=long
PKT=thap
V17=scripts/e2e-m17
ACME=https://localhost:14000/dir
PEBBLE_UNIT=tpan-e2e-pebble
PEBBLE_DIR=/etc/tpan-e2e-pebble
OLD_VER=v0.2.0
OLD_DIR="${OLD_DIR:-dist/release/$OLD_VER}"
PORT=18713
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

# kv <khoá>: giá trị của dòng "<khoá>=<giá trị>" cuối cùng trong OUT (output của
# các script trong scripts/e2e-m17/). Không có dòng đó thì rỗng.
kv() { awk -v k="$1" 'index($0, k "=") == 1 { v = substr($0, length(k) + 2) } END { print v }' <<<"$OUT"; }
kv_is() { [ "$(kv "$1")" = "$2" ]; }
kv_not() { local v; v="$(kv "$1")"; [ -n "$v" ] && [ "$v" != "$2" ]; }
# So sánh số: khoá không có hay không phải số thì không đạt.
kv_num() { # kv_num <khoá> <toán tử awk> <số>
  awk -v a="$(kv "$1")" -v b="$3" "BEGIN { exit !(a ~ /^-?[0-9]+([.][0-9]+)?\$/ && (a + 0) $2 (b + 0)) }"
}
kv_le() { kv_num "$1" '<=' "$2"; }
kv_lt() { kv_num "$1" '<' "$2"; }
kv_ge() { kv_num "$1" '>=' "$2"; }
kv_between() { kv_num "$1" '>=' "$2" && kv_num "$1" '<=' "$3"; }

vexec() { run "$VM" exec "$@"; }
vroot() { run "$VM" root "$@"; }
# vsc <tên> [đối số...]: chạy scripts/e2e-m17/<tên>.sh trong máy ảo bằng root
# (`bash -s -- <đối số> < file`), in như run; output để kv đọc.
vsc() {
  local f="$V17/$1.sh"
  shift
  RUNQ_CMD="$(mask_str <<<"$VM root bash -s -- $* < $f")"
  RUNQ_TS="$(ts)"
  vm_cap "$f" root bash -s -- "$@"
  show_last
}

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

# dbpw <domain>: mật khẩu DB của site trong wp-config.php (không in). Đọc bằng
# root từ file, không chạy lệnh nào dưới user của site (site có thể đang trong lồng).
dbpw() {
  "$VM" root bash -c "sed -nE \"s/^define\\( *'DB_PASSWORD' *, *'([^']*)' *\\);.*/\\1/p\" \"/var/www/\$1/public/wp-config.php\" | head -1" _ "$1" </dev/null | tr -d '\r'
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

# install_run <lệnh...>: một lần chạy in mật khẩu quản trị panel (tpan setup hay
# install.sh); nhặt mật khẩu (che, đếm số lần in) trước khi in. Kết quả: RC, OUT, PW, PW_HITS.
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

# ---------------------------------------------------------------- web

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
# web_assert <domain> <nhãn>: bốn khẳng định quen thuộc.
web_assert() {
  web "$1"
  ok "$2: trang chủ $1 trả 200" has '^trang-chu=200$'
  ok "$2: permalink /hello-world/ trả 200" has '^permalink=200$'
  ok "$2: trang không có trả 404 do WordPress trả" has '^khong-co=404 dau-wordpress=[1-9]'
  ok "$2: /wp-admin/ chuyển hướng tới wp-login.php" has "^wp-admin=302 -> http://$1/wp-login\\.php"
}
# wait_200 <domain> <số giây>: chờ trang chủ trả 200 (sau khi máy khởi động lại).
wait_200() {
  local i
  for i in $(seq 1 "$2"); do
    "$VM" exec bash -c 'curl -s -o /dev/null -m 10 -w "%{http_code}" --resolve "$1:80:127.0.0.1" "http://$1/"' _ "$1" </dev/null 2>/dev/null | grep -q '^200$' && return 0
    sleep 1
  done
  return 1
}

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
# cage_checks_pass <domain>: bốn phép kiểm của lồng đều đạt (trong báo cáo health vừa lấy).
cage_checks_pass() {
  local i
  for i in cage.socket cage.runtime cage.fence cage.limits; do
    check_status "$i:$1" pass || return 1
  done
}

# Gộp các điều kiện thành một lệnh để `ok` đếm đúng một khẳng định.
cage_pass_ab() { cage_checks_pass "$A" && cage_checks_pass "$B"; }
cage_pass_ab_no_fail() { cage_pass_ab && no_fail; }
cage_pass_r_no_fail() { cage_checks_pass "$R" && no_fail; }
limits_a_no_fail() { check_status "cage.limits:$A" pass && no_fail; }
limits_ab_pass() { check_status "cage.limits:$A" pass && check_status "cage.limits:$B" pass; }
limits_ab_no_fail() { limits_ab_pass && no_fail; }

# ---------------------------------------------------------------- CLI

# site_add_q <domain> [cờ...]: tpan site add, nhặt mật khẩu quản trị WordPress
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

# cage_row <domain>: CageRow của domain (tpan cage status <domain> --json) ở OUT;
# nhận cả hình mảng lẫn hình một đối tượng.
cage_row() {
  vjson tpan cage status "$1" --json
  OUT="$(jq -c --arg d "$1" 'if type == "array" then (.[] | select(.domain == $d)) else . end' <<<"$OUT" 2>/dev/null)"
}
# usage_row <domain>: SiteUsage của domain (tpan usage <domain> --json) ở OUT.
usage_row() {
  vjson tpan usage "$1" --json
  OUT="$(jq -c --arg d "$1" 'if type == "array" then (.[] | select(.domain == $d)) else . end' <<<"$OUT" 2>/dev/null)"
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

# ---------------------------------------------------------------- khối dùng chung của hai stack

JADM=""
JK=""

# sau_viec <nhãn>: sau mỗi việc đụng tới A (đổi PHP, tạm khoá, khôi phục, chuyển
# stack...): A và B trả 200, caged vẫn true, bốn phép kiểm của lồng đạt, health
# không có lỗi (bất biến 7).
sau_viec() {
  web "$A"
  ok "$1: A trả 200" has '^trang-chu=200$'
  web "$B"
  ok "$1: B trả 200" has '^trang-chu=200$'
  cage_row "$A"
  ok "$1: caged của A vẫn là true" jout '.caged == true'
  health_json
  ok "$1: cage.limits:$A đạt" check_status "cage.limits:$A" pass
  ok "$1: health không có lỗi" no_fail
}

# stack_setup <nginx|apache>: máy sạch, stack, panel (tpan setup), đăng nhập admin.
stack_setup() {
  local web=$1 unit=nginx
  [ "$web" = apache ] && unit=apache2
  reset_vm
  deploy_new
  vroot tpan stack install --web "$web" --yes
  ok "stack install --web $web thoát 0, job thành công" job_ok
  vexec systemctl is-active "$unit" php8.3-fpm mariadb
  ok "$unit, php8.3-fpm, mariadb active" count_is 3 '^active$'
  install_run "$VM" root tpan setup --yes
  ok "tpan setup thoát 0" rc_is 0
  ok "mật khẩu quản trị in đúng một lần" test "$PW_HITS" -eq 1
  save_pw admin "$PW"
  fetch_cert
  ok "panel trả lời qua https (--cacert)" rc_is 0
  JADM="$(new_jar adm)"
  login "$JADM" admin "$TMPD/pw.admin"
  ok "admin đăng nhập: 204" code_is 204
}

# goi_user: hai gói có lồng, hai user, và các lời từ chối khi thêm gói sai trần.
goi_user() {
  section "gói và user: kiểm trần lúc thêm gói"
  vjson tpan package add xau --max-sites 1 --cage on --ram-mb 8 --yes --json
  ok "RAM 8 MB (dưới 64): từ chối package_limit_invalid" refused package_limit_invalid
  vjson tpan package add xau --max-sites 1 --ram-mb 200 --yes --json
  ok "có trần mà không bật lồng: từ chối package_limits_need_cage" refused package_limits_need_cage
  vjson tpan package add xau --max-sites 1 --cage on --tasks 20 --php-workers 10 --yes --json
  ok "trần tiến trình 20 thấp hơn số worker 10 cộng 15: từ chối package_limit_invalid" refused package_limit_invalid
  vroot tpan package add "$PKL" --max-sites 6 --max-dbs 6 --cage on --cpu 50 --ram-mb 200 --tasks 40 --php-workers 3 --yes
  ok "package add $PKL (lồng bật, RAM 200, CPU 50%, 40 tiến trình, 3 worker)" rc_is 0
  vroot tpan package add "$PKT" --max-sites 6 --max-dbs 6 --cage on --cpu 25 --ram-mb 128 --tasks 40 --php-workers 2 --yes
  ok "package add $PKT (gói thấp hơn, để hạ gói)" rc_is 0
  vroot tpan package list
  ok "package list có default, $PKL, $PKT" bash -c 'grep -q "^default " <<<"$1" && grep -q "^$2 " <<<"$1" && grep -q "^$3 " <<<"$1"' _ "$OUT" "$PKL" "$PKT"
  json_secret k '.password' "$VM" root tpan user add "$UK" --full-name "Khach" --email k@example.com --package "$PKL" --yes --json
  ok "user add $UK (gói $PKL): thoát 0, mật khẩu sinh một lần (đã che)" bash -c '[ "$1" = 0 ] && [ -n "$2" ]' _ "$RC" "$JS_PW"
  json_secret k2 '.password' "$VM" root tpan user add "$UK2" --full-name "Khach Hai" --email k2@example.com --package default --yes --json
  ok "user add $UK2 (gói default, không lồng)" bash -c '[ "$1" = 0 ] && [ -n "$2" ]' _ "$RC" "$JS_PW"
}

# site_dau: O (ngoài lồng, đối chứng), rồi A, B của khach tự vào lồng.
SNAP_O=""
site_dau() {
  section "site: O ở ngoài lồng; A, B của $UK tự vào lồng sau khi tạo"
  site_add "$O"
  vsc bam site "$O"
  SNAP_O="$OUT"
  site_add "$A" --owner "$UK"
  site_add "$B" --owner "$UK"
  vjson tpan cage status --json
  ok "cage status --json: A, B ở trong lồng; O không" jout --arg a "$A" --arg b "$B" --arg o "$O" '(if type == "array" then . else [.] end) | ([.[] | select(.caged) | .domain] | sort) == ([$a, $b] | sort) and ([.[] | select(.domain == $o)][0] | .caged == false and .state == "ngoai")'
  cage_row "$A"
  ok "$A tự vào lồng sau khi tạo: caged, gói $PKL muốn lồng, state trong" jout --arg p "$PKL" '.caged == true and .want == true and .state == "trong" and .package == $p'
  cage_row "$B"
  ok "$B tự vào lồng sau khi tạo" jout '.caged == true and .state == "trong"'
  vroot tpan cage status
  ok "cage status (dạng chữ) thoát 0, có A, B và O" bash -c 'grep -qF "$2" <<<"$1" && grep -qF "$3" <<<"$1" && grep -qF "$4" <<<"$1"' _ "$OUT" "$A" "$B" "$O"
  web_assert "$A" "A trong lồng"
  web_assert "$B" "B trong lồng"
  web_assert "$O" "O ngoài lồng"

  section "bất biến 3: trong lồng đúng một nơi phục vụ PHP; ngoài lồng không có unit của lồng"
  vsc tien-trinh kiem-ban "$A"
  ok "A: không có file pool chung (theo tên, theo user)" bash -c '[ "$1" = 0 ] && [ "$2" = 0 ]' _ "$(kv POOL_CHUNG_FILE)" "$(kv POOL_CHUNG_USER)"
  ok "A: đúng một tiến trình php-fpm: master của chính user đó" kv_is MASTER 1
  ok "A: có unit (socket, dịch vụ, slice) và file của lồng" bash -c '[ "$1" -ge 2 ] && [ "$2" -ge 4 ]' _ "$(kv UNIT_LONG)" "$(kv FILE_LONG)"
  vsc tien-trinh kiem-ban "$O"
  ok "O (ngoài lồng): không unit tpan-cage-*, không file của lồng" bash -c '[ "$1" = 0 ] && [ "$2" = 0 ]' _ "$(kv UNIT_LONG)" "$(kv FILE_LONG)"
  ok "O: còn file pool chung, không có master riêng của user" bash -c '[ "$1" = 1 ] && [ "$2" = 0 ]' _ "$(kv POOL_CHUNG_FILE)" "$(kv MASTER)"
  vsc bam site "$O"
  ok "bất biến 1: O ngoài lồng, sha256 pool, vhost, cron, wp-config, .htaccess không đổi sau khi A và B vào lồng" out_is "$SNAP_O"

  health_json
  ok "health không có lỗi" no_fail
  ok "cage.socket, cage.runtime, cage.fence, cage.limits của A và của B đều đạt" cage_pass_ab
  ok "O ngoài lồng và gói default không muốn lồng: không có phép kiểm cage.* nào của O" hq --arg o "$O" '[.groups[].results[] | select(.id | test("^cage[.].*:" + $o + "$"))] | length == 0'
}

# nhin_tu_a: mã PHP của A (như kẻ đã chiếm site) nhìn thấy gì.
nhin_tu_a() {
  section "A trong lồng nhìn thấy gì (mã PHP của A gọi shell)"
  vsc long-nhin "$A" "$B"
  ok "ls /var/www: đúng một tên, là $A" kv_is LS_VAR_WWW "$A"
  ok "/tmp rỗng (PrivateTmp)" kv_is TMP_MUC 0
  ok "không thấy tiến trình của user khác" kv_is TIEN_TRINH_KHAC 0
  ok "passwd riêng: 4 dòng, đúng một user tp_* (chính A)" bash -c '[ "$1" = 4 ] && [ "$2" = 1 ]' _ "$(kv PASSWD_DONG)" "$(kv PASSWD_TP)"
  ok "thư mục của $B không thấy" has '^B_THU_MUC=.*cannot access'
  ok "wp-config.php của $B không đọc được" has '^B_WPCONFIG=.*cannot open'
  ok "/etc/nginx và /etc/apache2 không thấy" has '^ETC_NGINX=.*cannot (access|open)'
  ok "/etc/tpan/cage chỉ có slug của A" kv_is ETC_TPAN_CAGE "$(slug_of "$A")"
  ok "/run/tpan-cage không thấy" has '^RUN_TPAN_CAGE=.*cannot (access|open)'
  ok "cgroupfs không thấy slice nào" hasnt '^CGROUPFS=.*tpan.*slice'
  ok "C5: /proc/self/net/unix không nhắc slug hay tên miền của $B (phần băm thì thấy, đúng mục 3.2)" kv_is UNIX_SOCKET_B 0
  ok "K1.4: crontab -l không chạy được" kv_not CRONTAB_RC 0
  ok "K1.4: sudo không chạy được" kv_not SUDO_RC 0
  ok "K1.4: CapEff rỗng, NoNewPrivs bằng 1" bash -c '[ "$1" = 0000000000000000 ] && [ "$2" = 1 ]' _ "$(kv CAPEFF)" "$(kv NONEWPRIVS)"
  ok "tiến trình của PHP nằm dưới tpan-$(slug_of "$A").slice" has "^CGROUP_CUA_A=.*/tpan-$(slug_of "$A")\\.slice/"

  section "bất biến 8: không tiến trình nào của user A ngoài slice (kể cả trong một vòng health và một lần cron)"
  vsc tien-trinh ngoai-slice "$A"
  ok "A: không tiến trình nào của user A ngoài tpan-<slug>.slice" kv_is NGOAI_SLICE 0
  vsc tien-trinh quet-health "$A"
  ok "trong một vòng tpan health: quét liên tục, không lúc nào có tiến trình của A ngoài slice" bash -c '[ "$1" -ge 1 ] && [ "$2" = 0 ]' _ "$(kv MAU)" "$(kv NGOAI_SLICE_MAX)"
  vsc tien-trinh quet-cron "$A"
  ok "K1.5: tpan cron run $A thoát 0; lệnh wp thấy trong slice của A, không lần nào ngoài slice" bash -c '[ "$1" = 0 ] && [ "$2" -ge 1 ] && [ "$3" = 0 ] && [ "$4" = 0 ]' _ "$(kv CRON_RC)" "$(kv WP_TRONG_SLICE)" "$(kv WP_NGOAI_SLICE)" "$(kv NGOAI_SLICE_MAX)"
}

# so_lieu: lấy mẫu (hai mẫu bao quanh mỗi phép quá tải để bộ đếm có "phần tăng"),
# kiểm số liệu đọc ngay, gợi ý trần.
qua_tai_va_so_lieu() {
  local web=$1 oom0 peak1
  section "quá tải: RAM, số tiến trình, CPU (máy bận), đĩa; B không rớt request nào"
  vroot tpan usage --sample
  ok "usage --sample (mẫu trước phép quá tải) thoát 0" rc_is 0
  usage_row "$A"
  oom0="$(jq -r '.usage.oom_kills // empty' <<<"$OUT")"
  ok "tpan usage $A --json: đọc được oom_kills lúc đầu ($oom0)" test -n "$oom0"

  section "RAM: A cấp phát 400 MB với trần 200 MB"
  vsc qua-tai ram "$A" "$B"
  if [ "$web" = nginx ]; then
    ok "A cấp phát 400 MB: request đó ra 502 (tiến trình con PHP bị giết, nginx: recv() failed 104)" kv_is RAM_A_400_HTTP 502
  else
    # Con số 502 chỉ đo được ở nginx (t3); mod_proxy_fcgi có thể trả 503 khi backend ngắt giữa chừng.
    ok "A cấp phát 400 MB: request đó ra lỗi 5xx (Apache; nginx ra 502)" has "^RAM_A_400_HTTP=5[0-9][0-9]$"
    note "mã HTTP thật của request cấp phát 400 MB trên Apache: $(kv RAM_A_400_HTTP)"
  fi
  ok "request kế tiếp của A trả 200 trong 2 giây" kv_le RAM_A_LAI_200_GIAY 2
  ok "B: 0 request hỏng trên 60" bash -c '[ "$1" = 0 ] && [ "$2" = 60 ]' _ "$(kv RAM_B_HONG)" "$(kv RAM_B_TONG)"
  ok "memory.events: oom_kill tăng đúng 1 (từ $(kv OOM_KILL_TRUOC) lên $(kv OOM_KILL))" test "$(kv OOM_KILL)" = "$(($(kv OOM_KILL_TRUOC) + 1))"
  ok "dịch vụ PHP của A không khởi động lại (OOMPolicy=continue)" bash -c '[ "$1" = "$2" ]' _ "$(kv NRESTARTS_TRUOC)" "$(kv NRESTARTS_SAU)"

  section "bất biến 6: số liệu hiện ra bằng số đọc từ cgroup"
  vsc cgroup doc "$A" memory.peak
  peak1="$(kv memory.peak)"
  usage_row "$A"
  ok "tpan usage --json: oom_kills tăng đúng 1 so với trước phép thử" jout --argjson o "${oom0:-0}" '.usage.oom_kills == $o + 1'
  ok "tpan usage --json: mem_peak bằng memory.peak đọc tay ($peak1)" jout --argjson p "${peak1:-0}" '.usage.mem_peak == $p'
  ok "tpan usage --json: running true, trần RAM đang áp 209715200" jout '.usage.running == true and .usage.mem_max == 209715200'
  vsc cgroup doc "$A" memory.peak
  ok "memory.peak không đổi giữa hai lần đọc tay (không request nào chen vào)" kv_is memory.peak "$peak1"
  API_MAX=1200 api "$JADM" GET "/api/v1/sites/$A/usage"
  ok "API GET /sites/$A/usage: 200, cùng mem_peak, oom_kills; có lịch sử range day" bash -c '[ "$1" = 200 ] && jq -e --argjson p "$3" --argjson o "$4" ".usage.mem_peak == \$p and .usage.oom_kills == \$o + 1 and .history.range == \"day\" and .domain == \"$5\"" "$2" >/dev/null' _ "$CODE" "$TMPD/body" "${peak1:-0}" "${oom0:-0}" "$A"
  vroot tpan usage --sample
  ok "usage --sample (sau RAM) thoát 0" rc_is 0

  section "số tiến trình: A sinh 200 tiến trình với trần 40"
  vsc qua-tai tasks "$A" "$B"
  ok "pids.max của slice là 40 (trần của gói)" kv_is TASKS_PIDS_MAX 40
  ok "pids.current không vượt 40 khi đầy" kv_le TASKS_PIDS_CURRENT 40
  ok "pids.peak bằng 40" kv_is TASKS_PIDS_PEAK 40
  ok "bộ đếm chạm trần (pids.events) từ 1 trở lên" kv_ge TASKS_HITS 1
  ok "B: 0 request hỏng trên 40 trong lúc A đầy tiến trình" kv_is TASKS_B_HONG 0
  ok "các tiến trình ngủ thoát rồi thì A trả 200" kv_is TASKS_A_SAU 200
  usage_row "$A"
  ok "tpan usage --json: tasks_peak bằng 40, tasks_hits từ 1 trở lên" jout '.usage.tasks_peak == 40 and .usage.tasks_hits >= 1'
  API_MAX=600 api "$JADM" GET "/api/v1/sites/$A/usage"
  ok "API GET /sites/$A/usage: 200, tasks_peak bằng 40" bash -c '[ "$1" = 200 ] && jq -e ".usage.tasks_peak == 40" "$2" >/dev/null' _ "$CODE" "$TMPD/body"
  vroot tpan usage --sample
  ok "usage --sample (sau tiến trình) thoát 0" rc_is 0

  section "CPU: máy bận (mỗi nhân một vòng lặp ưu tiên thấp nhất ngoài lồng), trần 50%"
  vsc qua-tai cpu "$A" "$B"
  ok "cpu.max của slice là 50000 100000 (CPUQuota=50%)" kv_is CPU_MAX "50000 100000"
  ok "máy bận, lần 1: A nhận CPU trong 45% đến 52% của một nhân" kv_between CPU_BAN_1_NHAN 45 52
  ok "máy bận, lần 2: A nhận CPU trong 45% đến 52% (không bao giờ trên 52%)" kv_between CPU_BAN_2_NHAN 45 52
  ok "A bị bóp ở cả hai lần (nr_throttled tăng)" bash -c '[ "$1" -ge 1 ] && [ "$2" -ge 1 ]' _ "$(kv CPU_BAN_1_BI_BOP)" "$(kv CPU_BAN_2_BI_BOP)"
  note "máy rảnh (chỉ ghi nhận, vCPU ngủ thức trễ nên thấp hơn trần, mục 2.4): $(kv CPU_RANH_NHAN)% của một nhân"
  vroot tpan usage --sample
  ok "usage --sample (sau CPU) thoát 0" rc_is 0

  section "đĩa: trần ghi và đọc 4 MB/giây qua package edit, A ghi 32 MB trực tiếp"
  vroot tpan package edit "$PKL" --io-read-mb 4 --io-write-mb 4 --yes
  ok "package edit $PKL --io-read-mb 4 --io-write-mb 4: job thành công" job_ok
  vsc cgroup doc "$A" io.max
  ok "io.max của slice A có rbps=4000000 và wbps=4000000" has 'rbps=4000000.*wbps=4000000|wbps=4000000.*rbps=4000000'
  vsc qua-tai io "$A" "$B" "$O"
  ok "A ghi 32 MB mất từ 5 giây trở lên (4 MB/giây)" kv_ge IO_A_GIAY 5
  ok "cùng lệnh ở site ngoài lồng $O mất dưới 2 giây" kv_lt IO_NGOAI_GIAY 2
  ok "B: 0 request hỏng trong lúc A ghi đĩa" kv_is IO_B_HONG 0
  vroot tpan package edit "$PKL" --io-read-mb 0 --io-write-mb 0 --yes
  ok "package edit gỡ trần đĩa: job thành công" job_ok
  web_assert "$A" "sau bốn phép quá tải, A"
  web_assert "$B" "sau bốn phép quá tải, B"
  health_json
  ok "sau bốn phép quá tải: cage.limits của A và B đạt, health không có lỗi" limits_ab_no_fail

  section "số liệu: mẫu, gộp giờ, gợi ý trần"
  vroot tpan usage --sample
  ok "usage --sample thoát 0" rc_is 0
  vroot tpan usage --rollup
  ok "usage --rollup thoát 0 (giờ hiện tại chưa trọn nên có thể chưa gộp dòng nào)" rc_is 0
  vroot python3 -c 'import sqlite3; c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True); [print("mau", d, n) for d, n in c.execute("SELECT domain, COUNT(*) FROM usage_samples GROUP BY domain ORDER BY domain")]; print("gio", c.execute("SELECT COUNT(*) FROM usage_hourly").fetchone()[0])'
  ok "usage_samples: A và B mỗi site từ 5 mẫu; O (ngoài lồng) không có mẫu" bash -c 'a=$(sed -nE "s/^mau $2 ([0-9]+)$/\1/p" <<<"$1"); b=$(sed -nE "s/^mau $3 ([0-9]+)$/\1/p" <<<"$1"); [ "${a:-0}" -ge 5 ] && [ "${b:-0}" -ge 5 ] && ! grep -q "^mau $4 " <<<"$1"' _ "$OUT" "$A" "$B" "$O"
  vjson tpan usage --suggest --json
  ok "usage --suggest --json: có A và B, không có O" jout --arg a "$A" --arg b "$B" --arg o "$O" '([.sites[].domain] | index($a) != null and index($b) != null and index($o) == null)'
  ok "gợi ý của A: dưới 288 mẫu nên low_data; có oom_kill và chạm trần tiến trình trong 7 ngày" jout --arg a "$A" '[.sites[] | select(.domain == $a)][0] | .low_data == true and .samples >= 5 and .oom_kills >= 1 and .tasks_hits >= 1'
  ok "gợi ý RAM của A: bội của 32, từ 128, không thấp hơn trần lúc đó 200 nhân 1,5 (300); tiến trình không thấp hơn 40 nhân 1,5 (60)" jout --arg a "$A" '[.sites[] | select(.domain == $a)][0].suggested | (.ram_mb % 32 == 0) and .ram_mb >= 300 and .tasks >= 60'
  ok "gợi ý có lời giải thích (reasons) cho A" jout --arg a "$A" '[.sites[] | select(.domain == $a)][0].reasons | length >= 1'
  ok "gợi ý theo gói: gói $PKL gồm A và B" jout --arg p "$PKL" --arg a "$A" --arg b "$B" '[.packages[] | select(.package == $p)][0].sites | (index($a) != null and index($b) != null)'
  vroot tpan usage
  ok "tpan usage (dạng chữ) thoát 0, có A và B" bash -c 'grep -qF "$2" <<<"$1" && grep -qF "$3" <<<"$1"' _ "$OUT" "$A" "$B"
  vjson tpan usage "$O" --json
  ok "usage của site ngoài lồng $O: từ chối usage_not_caged" refused usage_not_caged
}

# chuyen_long: R vào lồng và ra lồng (bất biến 2, 8), vòng curl, --all, --stale.
SNAP_R=""
chuyen_long() {
  section "R (của $UK2) từ ngoài vào lồng rồi ra: vòng curl, sha256, tiến trình"
  site_add "$R" --owner "$UK2"
  vsc bam site "$R"
  SNAP_R="$OUT"
  cage_row "$R"
  ok "$R ở ngoài lồng, gói default không muốn lồng" jout '.caged == false and .want == false and .state == "ngoai"'
  vjson tpan cage enable "$R" --yes --json
  ok "cage enable khi gói chưa bật lồng: từ chối cage_not_in_package" refused cage_not_in_package
  vjson tpan cage disable "$R" --yes --json
  ok "cage disable site không ở trong lồng: từ chối cage_not_caged" refused cage_not_caged
  vroot tpan user edit "$UK2" --package "$PKL" --yes
  ok "user edit $UK2 --package $PKL thoát 0 (chưa có site trong lồng nên không có job cage.sync)" rc_is 0
  cage_row "$R"
  ok "gói muốn lồng mà site còn ngoài: state can-vao" jout '.caged == false and .want == true and .state == "can-vao"'
  health_json
  ok "health: cage.state:$R là cảnh báo (không phải lỗi)" check_status "cage.state:$R" warn
  vroot tpan cage enable "$R" --dry-run
  ok "cage enable --dry-run thoát 0" rc_is 0
  cage_row "$R"
  ok "dry-run không đổi gì: $R vẫn ngoài lồng" jout '.caged == false'

  vsc vong-curl bat "$R"
  sleep 2
  vroot tpan cage enable "$R" --yes
  ok "cage enable $R: job thành công" job_ok
  sleep 2
  vsc vong-curl dung
  ok "vòng curl vào lồng: có ít nhất 20 request" kv_ge TONG 20
  ok "vào lồng: gián đoạn dưới 1 giây (đợt request hỏng liền nhau dài nhất)" kv_lt GIAN_DOAN_MS 1000
  note "vào lồng: $(kv HONG) request hỏng trên $(kv TONG) (mã: $(kv MA)); gián đoạn $(kv GIAN_DOAN_MS) ms (cận trên $(kv GIAN_DOAN_DAI_MS) ms); request chậm nhất $(kv CHAM_NHAT_MS) ms"
  cage_row "$R"
  ok "$R đã vào lồng: caged true, state trong" jout '.caged == true and .state == "trong"'
  vsc tien-trinh ngoai-slice "$R"
  ok "bất biến 8: sau cage enable, không tiến trình nào của user R ngoài slice" kv_is NGOAI_SLICE 0
  vsc tien-trinh quet-health "$R"
  ok "bất biến 8: trong một vòng tpan health cũng không" bash -c '[ "$1" -ge 1 ] && [ "$2" = 0 ]' _ "$(kv MAU)" "$(kv NGOAI_SLICE_MAX)"
  vsc tien-trinh kiem-ban "$R"
  ok "R trong lồng: không file pool chung, đúng một master" bash -c '[ "$1" = 0 ] && [ "$2" = 0 ] && [ "$3" = 1 ]' _ "$(kv POOL_CHUNG_FILE)" "$(kv POOL_CHUNG_USER)" "$(kv MASTER)"
  vroot tpan cage enable "$R" --yes
  ok "cage enable site đã ở trong lồng: không từ chối, job thành công (chạy lại)" job_ok
  health_json
  ok "R: bốn phép kiểm của lồng đạt, health không có lỗi" cage_pass_r_no_fail

  vsc vong-curl bat "$R"
  sleep 2
  vroot tpan cage disable "$R" --yes
  ok "cage disable $R: job thành công" job_ok
  sleep 2
  vsc vong-curl dung
  ok "vòng curl ra lồng: có ít nhất 20 request" kv_ge TONG 20
  ok "ra lồng: 0 request hỏng, 0 giây gián đoạn" bash -c '[ "$1" = 0 ] && [ "$2" = 0 ]' _ "$(kv HONG)" "$(kv GIAN_DOAN_MS)"
  note "ra lồng: $(kv HONG) request hỏng trên $(kv TONG) (mã: $(kv MA)); request chậm nhất $(kv CHAM_NHAT_MS) ms"
  cage_row "$R"
  ok "$R đã ra lồng: caged false, không còn file hàng rào" jout '.caged == false and .files == false'
  vsc tien-trinh kiem-ban "$R"
  ok "ra lồng: pool chung về, không master riêng, không unit hay file của lồng" bash -c '[ "$1" = 1 ] && [ "$2" = 0 ] && [ "$3" = 0 ] && [ "$4" = 0 ]' _ "$(kv POOL_CHUNG_FILE)" "$(kv MASTER)" "$(kv UNIT_LONG)" "$(kv FILE_LONG)"
  vsc bam site "$R"
  ok "bất biến 2: ra lồng, sha256 pool, vhost, cron, wp-config, .htaccess về đúng như trước khi vào" out_is "$SNAP_R"

  section "cage enable --all, user đổi gói rồi cage disable --stale"
  vroot tpan cage enable --all --yes
  ok "cage enable --all thoát 0" rc_is 0
  cage_row "$R"
  ok "$R vào lồng lại bằng --all" jout '.caged == true'
  vroot tpan user edit "$UK2" --package default --yes
  ok "user edit $UK2 --package default thoát 0 (site vẫn trong lồng, trần về 0)" rc_is 0
  cage_row "$R"
  ok "gói không còn muốn lồng mà site còn trong: state can-ra" jout '.caged == true and .want == false and .state == "can-ra"'
  health_json
  ok "health: cage.state:$R là cảnh báo" check_status "cage.state:$R" warn
  vroot tpan cage disable --stale --yes
  ok "cage disable --stale thoát 0" rc_is 0
  cage_row "$R"
  ok "$R ra khỏi lồng bằng --stale" jout '.caged == false'
  cage_row "$A"
  ok "--stale không đụng $A (gói vẫn muốn lồng)" jout '.caged == true'
  vroot tpan user edit "$UK2" --package "$PKL" --yes
  ok "user edit $UK2 --package $PKL" rc_is 0
  vroot tpan cage enable "$R" --yes
  ok "cage enable $R (về trong lồng cho các phần sau): job thành công" job_ok
}

# thao_tac_cua_a: bất biến 7, A giữ lồng sau mọi thao tác của chủ.
thao_tac_cua_a() {
  local web=$1 other=nginx name0
  [ "$web" = nginx ] && other=apache
  section "bất biến 7: A giữ lồng sau đổi PHP"
  vroot tpan php install 8.4 --yes
  if job_ok; then
    sau_viec "cài PHP 8.4"
    vroot tpan php use "$A" 8.4 --yes
    ok "php use $A 8.4: job thành công" job_ok
    sau_viec "A chạy PHP 8.4"
    vsc tien-trinh kiem-ban "$A"
    ok "A chạy PHP 8.4 trong lồng: không file pool chung, đúng một master" bash -c '[ "$1" = 0 ] && [ "$2" = 1 ]' _ "$(kv POOL_CHUNG_FILE)" "$(kv MASTER)"
    vroot tpan php use "$A" 8.3 --yes
    ok "php use $A 8.3 (về lại): job thành công" job_ok
    sau_viec "A về PHP 8.3"
  else
    note "BỎ các khẳng định đổi PHP: không cài được 8.4 (kho ngoài packages.sury.org)"
  fi

  section "bất biến 7: tạm khoá (403, socket disabled) rồi mở khoá"
  vroot tpan site suspend "$A" --yes
  ok "site suspend $A: job thành công" job_ok
  web "$A"
  ok "A tạm khoá trả 403" has '^trang-chu=403$'
  vsc cgroup trang-thai "$A"
  ok "socket của lồng A ở trạng thái disabled, không active" bash -c '[ "$1" = disabled ] && [ "$2" != active ]' _ "$(kv SOCKET_ENABLED)" "$(kv SOCKET_ACTIVE)"
  vroot tpan site unsuspend "$A" --yes
  ok "site unsuspend $A: job thành công" job_ok
  sau_viec "A mở khoá"

  section "bất biến 7: sao lưu, sửa nội dung, khôi phục"
  vroot tpan backup create "$A" --yes
  ok "backup create $A: job thành công" job_ok
  vsc noi-dung doc "$A"
  name0="$(kv BLOGNAME)"
  ok "đọc được tên site trong wp_options ($name0)" test -n "$name0"
  vsc noi-dung sua "$A" "e2e-m17-da-sua"
  ok "đã sửa tên site thành e2e-m17-da-sua" kv_is BLOGNAME e2e-m17-da-sua
  vroot tpan backup restore "$A" --yes
  ok "backup restore $A: job thành công" job_ok
  vsc noi-dung doc "$A"
  ok "khôi phục xong: tên site về như lúc sao lưu" kv_is BLOGNAME "$name0"
  sau_viec "A sau khôi phục backup"

  section "bất biến 7: thùng rác rồi khôi phục"
  vroot tpan site rm "$A" --yes
  ok "site rm $A (vào thùng rác): job thành công" job_ok
  vroot tpan site restore "$A" --yes
  ok "site restore $A: job thành công" job_ok
  sau_viec "A sau khi khôi phục từ thùng rác"

  section "bất biến 7: chuyển stack sang $other rồi về $web"
  vroot tpan stack switch --to "$other" --yes
  ok "stack switch --to $other: job thành công" job_ok
  sau_viec "A sau khi chuyển stack sang $other"
  vroot tpan stack switch --to "$web" --yes
  ok "stack switch --to $web: job thành công" job_ok
  sau_viec "A sau khi về stack $web"
}

# ssl_trong_long: xin SSL cho A bằng pebble (như e2e M14). Chạy cuối phần: sau đó
# http của A chuyển hướng sang https nên các phép gọi http://A/ không còn trả 200.
ssl_trong_long() {
  section "xin SSL cho A trong lồng (pebble, như e2e M14): vhost 443 qua siteVhost, kiểm chứng SiteChecksSSLCaged"
  vsc pebble "$PEBBLE_DIR" "$PEBBLE_UNIT"
  ok "(đồ nghề) dựng pebble thoát 0" rc_is 0
  vroot bash -c 'echo "127.0.0.1 $1" >>/etc/hosts && getent ahostsv4 "$1" | head -1' _ "$A"
  ok "$A -> 127.0.0.1" has '^127\.0\.0\.1 '
  vroot tpan ssl settings --email "$EMAIL" --server "$ACME"
  ok "ssl settings: email và máy chủ ACME thử" rc_is 0
  vroot tpan ssl issue "$A" --yes
  ok "ssl issue $A (pebble) cho site trong lồng: job thành công" job_ok
  vexec bash -c 'curl -sk -m 20 --resolve "$1:443:127.0.0.1" -o /dev/null -w "https=%{http_code}\n" "https://$1/"; curl -s -m 20 --resolve "$1:80:127.0.0.1" -o /dev/null -w "http=%{http_code} -> %{redirect_url}\n" "http://$1/"' _ "$A"
  ok "$A: https 200, http chuyển hướng sang https" bash -c 'grep -qx "https=200" <<<"$1" && grep -q "^http=301 -> https://" <<<"$1"' _ "$OUT"
  cage_row "$A"
  ok "xin SSL xong: caged của A vẫn là true" jout '.caged == true'
  health_json
  ok "sau SSL: cage.limits:$A đạt, health không có lỗi" limits_a_no_fail
  vsc tien-trinh ngoai-slice "$A"
  ok "sau SSL: không tiến trình nào của user A ngoài slice" kv_is NGOAI_SLICE 0
}

# tran_va_goi: bất biến 4, 11, 12.
tran_va_goi() {
  local pid0 mem0
  section "bất biến 4: sửa tay file slice thì cage.limits lỗi, health --fix đưa về"
  vsc cgroup main-pid "$A"
  pid0="$(kv MAIN_PID)"
  vsc cgroup sua-slice "$A" 100
  ok "(đồ nghề) sửa tay MemoryMax=100M trong file slice rồi daemon-reload" has '^DA_SUA=MemoryMax=100M$'
  vsc cgroup doc "$A" memory.max
  ok "cgroup nhận trần sửa tay ngay (104857600)" kv_is memory.max 104857600
  health_json
  ok "health: cage.limits:$A là lỗi (cgroup lệch gói)" check_status "cage.limits:$A" fail
  vroot tpan health --fix --yes
  note "tpan health --fix --yes thoát $RC"
  vsc cgroup doc "$A" memory.max
  ok "sau health --fix: memory.max về 209715200" kv_is memory.max 209715200
  health_json
  ok "health: cage.limits:$A đạt lại, không có lỗi" limits_a_no_fail
  vsc cgroup main-pid "$A"
  ok "sửa trần không khởi động lại dịch vụ: MainPID giữ nguyên ($pid0)" bash -c '[ "$1" != 0 ] && [ "$1" = "$2" ]' _ "$pid0" "$(kv MAIN_PID)"

  section "bất biến 4: package edit đổi RAM 200 lên 300, MainPID giữ"
  vsc cgroup main-pid "$A"
  pid0="$(kv MAIN_PID)"
  vroot tpan package edit "$PKL" --ram-mb 300 --yes
  ok "package edit $PKL --ram-mb 300: job thành công" job_ok
  vsc cgroup doc "$A" memory.max
  ok "memory.max của A là 314572800 (300 MiB)" kv_is memory.max 314572800
  vsc cgroup doc "$B" memory.max
  ok "memory.max của B cũng 314572800 (cùng gói)" kv_is memory.max 314572800
  vsc cgroup main-pid "$A"
  ok "MainPID của A không đổi ($pid0)" bash -c '[ "$1" != 0 ] && [ "$1" = "$2" ]' _ "$pid0" "$(kv MAIN_PID)"
  web "$A"
  ok "A vẫn 200" has '^trang-chu=200$'
  vroot tpan package edit "$PKL" --ram-mb 200 --yes
  ok "package edit $PKL --ram-mb 200 (về lại): job thành công" job_ok

  section "bất biến 12: hạ trần RAM của gói xuống dưới mức A đang dùng rồi nâng lại, A tự trả 200 trong 10 giây"
  web "$A"
  vsc cgroup doc "$A" memory.current
  mem0="$(kv memory.current)"
  note "A đang dùng memory.current = $mem0 byte; hạ trần xuống 64 MiB = 67108864"
  vsc vong-curl bat "$A"
  sleep 3
  vroot tpan package edit "$PKL" --ram-mb 64 --yes
  ok "package edit $PKL --ram-mb 64 (dưới mức đang dùng): job thành công (chính sách của admin, không chờ site)" job_ok
  vsc cgroup doc "$A" memory.max
  ok "memory.max của A đã là 67108864" kv_is memory.max 67108864
  sleep 20
  vsc cgroup trang-thai "$A"
  note "A sau 20 giây ở trần 64 MiB: socket $(kv SOCKET_ACTIVE), dịch vụ $(kv DICH_VU_ACTIVE) (Result $(kv DICH_VU_RESULT)), NRestarts $(kv N_RESTARTS), file socket $(kv FILE_SOCKET)"
  vroot tpan package edit "$PKL" --ram-mb 200 --yes
  ok "package edit $PKL --ram-mb 200 (nâng lại): job thành công" job_ok
  vsc vong-curl moc
  sleep 15
  vsc vong-curl dung
  ok "A thật sự hỏng khi trần là 64 MiB (nếu không, phép thử này không chứng minh gì: trước khi hạ trần mọi request đều 200)" kv_ge HONG 1
  ok "A tự trả 200 trong 10 giây sau khi nâng trần, không lệnh nào khác (reset-failed, start)" kv_between PHUC_HOI_MS 0 10000
  note "hạ rồi nâng trần: $(kv HONG) request hỏng trên $(kv TONG) (mã: $(kv MA)); phục hồi sau $(kv PHUC_HOI_MS) ms"
  vsc cgroup trang-thai "$A"
  ok "socket của A active, dịch vụ không ở trạng thái failed" bash -c '[ "$1" = active ] && [ "$2" != failed ] && [ "$3" = co ]' _ "$(kv SOCKET_ACTIVE)" "$(kv DICH_VU_ACTIVE)" "$(kv FILE_SOCKET)"
  web_assert "$A" "sau khi nâng trần lại"
  health_json
  ok "health: cage.limits:$A đạt, không có lỗi" limits_a_no_fail

  section "bất biến 11: site trả 500 cho 127.0.0.1 và tiến trình chính bị giết, user edit --package vẫn hạ được gói"
  vsc thu-500 dat "$A"
  ok "(đồ nghề) A trả 500 cho request từ 127.0.0.1" kv_is MA_HTTP 500
  vsc thu-500 giet-chinh "$A"
  ok "(đồ nghề) đã kill -9 tiến trình chính của FPM của A" has '^DA_GIET=[1-9]'
  vroot tpan user edit "$UK" --package "$PKT" --yes
  ok "user edit $UK --package $PKT: thoát 0, job cage.sync thành công dù A trả 500 và tiến trình chính vừa bị giết" job_ok
  vroot tpan user info "$UK" --json
  ok "gói của $UK đã đổi sang $PKT" jout --arg p "$PKT" '(.user.package // .package) == $p'
  web "$A"
  note "A (đang trả 500 có chủ ý) lúc này: $(grep '^trang-chu=' <<<"$OUT")"
  vsc cgroup doc "$A" memory.max
  ok "memory.max của A đổi theo gói mới: 134217728 (128 MiB)" kv_is memory.max 134217728
  vsc cgroup doc "$B" memory.max
  ok "memory.max của B cũng 134217728" kv_is memory.max 134217728
  vsc thu-500 go "$A"
  ok "(đồ nghề) gỡ mu-plugin: A trả 200 lại" kv_is MA_HTTP 200
  vroot tpan user edit "$UK" --package "$PKL" --yes
  ok "user edit $UK --package $PKL (nâng lại): job thành công" job_ok
  vsc cgroup doc "$A" memory.max
  ok "memory.max của A về 209715200" kv_is memory.max 209715200
  sau_viec "A sau khi hạ gói rồi nâng lại"
}

# thu_tan_cong <nginx|apache>: tpan isolation probe, kill -9 giữa chừng, --cleanup.
thu_tan_cong() {
  local web=$1 in_ids out_ids na_ids c3 mem_mb
  if [ "$web" = nginx ]; then
    in_ids='["L1","L2","L5","L6","L7","L8","C1","C2","C3","C4","C5"]'
    out_ids='["L1","L2","L5","L6","L7","L8"]'
    na_ids='["L3","L4","L9"]'
  else
    in_ids='["L1","L2","L3","L4","L6","L7","L8","L9","C1","C2","C3","C4","C5"]'
    out_ids='["L1","L2","L3","L4","L6","L7","L8","L9"]'
    na_ids='["L5"]'
  fi
  section "thử tấn công: tpan isolation probe (trong một lượt: ngoài lồng rồi trong lồng)"
  vexec awk '/^MemAvailable:/ { print int($2 / 1024) }' /proc/meminfo
  mem_mb="$OUT"
  c3=needs_cage
  [ "${mem_mb:-0}" -lt 1024 ] 2>/dev/null && c3=skipped
  note "RAM trống ${mem_mb} MB: C3 ngoài lồng mong đợi $c3 (chỉ chạy khi còn từ 1 GB)"
  vsc sach-probe tmp-luu truoc
  vsc sach-probe so-dong
  vroot tpan isolation probe --dry-run
  ok "isolation probe --dry-run thoát 0, nêu tên hai site thử" bash -c '[ "$2" = 0 ] && grep -qE "tpan-probe-a-[0-9a-f]{12}\.test" <<<"$1" && grep -qE "tpan-probe-b-[0-9a-f]{12}\.test" <<<"$1"' _ "$OUT" "$RC"
  vsc sach-probe quet
  ok "dry-run không tạo gì: không dấu vết nào" kv_is RAC_TONG 0
  vjson tpan isolation probe --yes --json
  ok "isolation probe --json thoát 0 (không ô nào lộ)" rc_is 0
  ok "ProbeReport: exposed 0, đã dọn, đúng stack $web, 14 đường (L1 đến L9, C1 đến C5)" jout --arg w "$web" '.exposed == 0 and .cleaned == true and .web == $w and (.results | length) == 14'
  ok "mọi ô trong lồng của các đường thuộc stack đều blocked" jout --argjson ids "$in_ids" '[.results[] | select(.path as $p | $ids | index($p) != null) | .inside.status] | (length == ($ids | length)) and all(. == "blocked")'
  ok "ngoài lồng: các đường L bị chặn (blocked)" jout --argjson ids "$out_ids" '[.results[] | select(.path as $p | $ids | index($p) != null) | .outside.status] | (length == ($ids | length)) and all(. == "blocked")'
  ok "ngoài lồng: C1, C2, C4, C5 là needs_cage (chỉ lồng chặn được)" jout '[.results[] | select(.path | IN("C1", "C2", "C4", "C5")) | .outside.status] | (length == 4) and all(. == "needs_cage")'
  ok "ngoài lồng: C3 là $c3" jout --arg c "$c3" '[.results[] | select(.path == "C3")][0].outside.status == $c'
  ok "các đường không thuộc stack $web là na ở cả hai cột" jout --argjson ids "$na_ids" '[.results[] | select(.path as $p | $ids | index($p) != null) | .outside.status, .inside.status] | (length == (($ids | length) * 2)) and all(. == "na")'
  vsc sach-probe quet
  ok "bất biến 5: sau lượt thử không còn user, thư mục, DB, vhost, unit, cron, file mang tên phép thử; không dòng nào của managed_files, check_results, incidents" kv_is RAC_TONG 0
  vsc sach-probe tmp-luu sau
  vsc sach-probe tmp-so truoc sau
  ok "/tmp, /var/tmp, /dev/shm giữ nguyên danh sách mục (không file thử nào sót)" kv_is DIFF_TMP 0
  vsc sach-probe so-dong
  ok "sổ probe_runs: một lượt done đã dọn" has '^PROBE_RUNS=.*done:1$'
  vroot tpan site list
  ok "tpan site list không nhắc site thử (không dòng nào trong bảng sites)" hasntF 'tpan-probe-'
  web "$A"
  ok "A vẫn 200 sau lượt thử" has '^trang-chu=200$'
  web "$B"
  ok "B vẫn 200 sau lượt thử" has '^trang-chu=200$'
  health_json
  ok "health không có lỗi sau lượt thử" no_fail
  vjson tpan site add tpan-probe-a-0123456789ab.test --email "$EMAIL" --yes --json
  ok "site add tên giống tên phép thử: từ chối domain_reserved" refused domain_reserved
  vroot tpan site add tpan-probe-shop.example.com --email "$EMAIL" --dry-run
  ok "site thật mang tiền tố tpan-probe- nhưng không khớp mẫu: không bị từ chối" rc_is 0

  section "kill -9 tpan giữa một lượt thử, rồi --cleanup"
  vsc giet-probe
  ok "(đồ nghề) tpan isolation probe bị kill -9 giữa chừng" kv_is DA_GIET 1
  ok "sổ probe_runs còn một dòng chưa dọn (running:0) sau khi bị giết" has 'PROBE_RUNS=.*running:0'
  note "thư mục site thử còn sót ngay sau khi bị giết: $(kv THU_MUC_SOT)"
  vjson tpan isolation probe --yes --json
  ok "lượt thử kế tiếp bị từ chối: probe_running" refused probe_running
  vroot tpan isolation probe --cleanup --yes
  ok "isolation probe --cleanup thoát 0" rc_is 0
  vsc sach-probe quet
  ok "bất biến 5: sau --cleanup không còn user, thư mục, DB, vhost, unit, cron mang tên phép thử; managed_files, check_results, incidents sạch" kv_is RAC_TONG 0
  vsc sach-probe tmp-luu sau2
  vsc sach-probe tmp-so truoc sau2
  ok "/tmp, /var/tmp, /dev/shm giữ nguyên danh sách mục" kv_is DIFF_TMP 0
  vsc sach-probe so-dong
  ok "sổ probe_runs: mọi dòng đã dọn (không dòng nào cleaned 0)" hasnt ':0(,|$)'
  health_json
  ok "health không có lỗi (kể cả probe.leftover)" no_fail
  web "$A"
  ok "A vẫn 200" has '^trang-chu=200$'

  section "API của thử tấn công"
  API_MAX=700 api "$JADM" GET /api/v1/security/probe
  ok "GET /security/probe: 200, có lượt gần nhất đã dọn, không lượt nào chờ dọn" bq '.last != null and .last.cleaned == true and ((.pending // []) | length) == 0'
  api "$JADM" POST "/api/v1/security/probe?dry_run=1" '{"only":"ngoai"}'
  ok "POST /security/probe?dry_run=1: 200, kind isolation.probe, only ngoai, tên site thử đúng mẫu" bq '.kind == "isolation.probe" and .run.only == "ngoai" and (.run.domain_a | test("^tpan-probe-a-[0-9a-f]{12}[.]test$")) and (.run.domain_b | test("^tpan-probe-b-[0-9a-f]{12}[.]test$"))'
  api "$JADM" POST "/api/v1/security/probe/cleanup?dry_run=1"
  ok "POST /security/probe/cleanup?dry_run=1 khi không còn gì để dọn: 409 nothing_to_do" expect 409 nothing_to_do
}

# api_phan_quyen: user chủ bị 403 ở route của admin, 404 ở site của người khác.
api_phan_quyen() {
  local body
  section "phân quyền API: user chủ ($UK) so với admin"
  JK="$(new_jar k)"
  login "$JK" "$UK" "$TMPD/pw.k"
  ok "$UK đăng nhập: 204" code_is 204
  api "$JK" GET /api/v1/cage
  ok "user chủ GET /cage: 403 forbidden" expect 403 forbidden
  api "$JK" POST "/api/v1/sites/$A/cage" '{"enabled":false}'
  ok "user chủ POST /sites/$A/cage (đưa site ra khỏi lồng): 403 forbidden" expect 403 forbidden
  api "$JK" POST "/api/v1/sites/$A/cage?dry_run=1" '{"enabled":false}'
  ok "kể cả dry_run: 403 forbidden" expect 403 forbidden
  api "$JK" POST "/api/v1/packages/$PKL" '{"max_sites":6,"max_dbs":6,"disk_mb":0,"max_backups":0,"ram_mb":4096}'
  ok "user chủ POST /packages/$PKL (nâng trần): 403 forbidden" expect 403 forbidden
  api "$JK" POST /api/v1/packages '{"name":"lau","max_sites":1,"cage":true,"ram_mb":4096}'
  ok "user chủ POST /packages (tạo gói): 403 forbidden" expect 403 forbidden
  api "$JK" GET /api/v1/usage/suggestions
  ok "user chủ GET /usage/suggestions: 403 forbidden" expect 403 forbidden
  api "$JK" GET /api/v1/security/probe
  ok "user chủ GET /security/probe: 403 forbidden" expect 403 forbidden
  api "$JK" POST /api/v1/security/probe '{"only":""}'
  ok "user chủ POST /security/probe: 403 forbidden" expect 403 forbidden
  api "$JK" POST /api/v1/security/probe/cleanup
  ok "user chủ POST /security/probe/cleanup: 403 forbidden" expect 403 forbidden
  api "$JK" GET "/api/v1/sites/$O/usage"
  ok "user chủ GET /sites/$O/usage (site của admin): 404 site_not_found" expect 404 site_not_found
  api "$JK" GET "/api/v1/sites/$R/usage"
  ok "user chủ GET /sites/$R/usage (site của $UK2): 404 site_not_found" expect 404 site_not_found
  API_MAX=500 api "$JK" GET "/api/v1/sites/$A/usage?range=day"
  ok "user chủ GET /sites/$A/usage (site của mình): 200, đúng domain, range day" bq --arg a "$A" '.domain == $a and .history.range == "day" and .usage.running == true'
  API_MAX=500 api "$JK" GET /api/v1/usage
  ok "user chủ GET /usage: 200, chỉ thấy A và B của mình" bq --arg a "$A" --arg b "$B" '(map(.domain) | sort) == ([$a, $b] | sort)'
  cage_row "$A"
  ok "sau mọi lần bị từ chối: caged của A vẫn là true" jout '.caged == true'

  API_MAX=500 api "$JADM" GET /api/v1/cage
  ok "admin GET /cage: 200, A, B, R trong lồng; O không" bq --arg a "$A" --arg b "$B" --arg r "$R" --arg o "$O" '([.[] | select(.caged) | .domain] | (index($a) != null and index($b) != null and index($r) != null)) and ([.[] | select(.domain == $o and .caged)] | length == 0)'
  api "$JADM" GET "/api/v1/sites/$O/usage"
  ok "admin GET /sites/$O/usage (ngoài lồng): 404 usage_not_caged" expect 404 usage_not_caged
  API_MAX=500 api "$JADM" GET /api/v1/usage
  ok "admin GET /usage: 200, có A, B, R, không có O" bq --arg a "$A" --arg b "$B" --arg o "$O" '(map(.domain) | (index($a) != null and index($b) != null and index($o) == null))'
  API_MAX=500 api "$JADM" GET /api/v1/usage/suggestions
  ok "admin GET /usage/suggestions: 200, có gợi ý theo site và theo gói" bq '(.sites | length) >= 2 and (.packages | length) >= 1'
  api "$JADM" POST "/api/v1/sites/$A/cage?dry_run=1" '{"enabled":false}'
  ok "admin POST /sites/$A/cage?dry_run=1 {enabled:false}: 200, kind cage.disable, đúng target" bq --arg a "$A" '.kind == "cage.disable" and .target == $a and (.steps | length) >= 1'
  api "$JADM" POST "/api/v1/sites/$A/cage?dry_run=1" '{"enabled":true}'
  ok "admin POST ... {enabled:true} khi A đã ở trong lồng: 200, kind cage.enable (chạy lại, không từ chối)" bq --arg a "$A" '.kind == "cage.enable" and .target == $a'
  api "$JADM" GET /api/v1/packages
  body="$(jq -c --arg p "$PKL" '(if type == "array" then . else .packages end) | map(.package // .) | map(select(.name == $p))[0] | {max_sites, max_dbs, disk_mb, max_backups, ram_mb: 250}' "$TMPD/body" 2>/dev/null)"
  ok "GET /packages: đọc được gói $PKL ($body)" test -n "$body" -a "$body" != null
  api "$JADM" POST "/api/v1/packages/$PKL?dry_run=1" "$body"
  ok "admin POST /packages/$PKL?dry_run=1 {ram_mb:250}: 200, kind cage.sync" bq '.kind == "cage.sync"'
  vsc cgroup doc "$A" memory.max
  ok "dry_run không đổi gì: memory.max của A vẫn 209715200" kv_is memory.max 209715200
}

# xoa_hang_r: xoá hẳn một site có lồng.
xoa_hang_r() {
  local slug
  slug="$(slug_of "$R")"
  section "xoá hẳn R (đang ở trong lồng): không sót file, unit, mẫu số liệu"
  vroot tpan usage --sample
  vroot python3 -c 'import sqlite3, sys; c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True); print("mau", sys.argv[1], c.execute("SELECT COUNT(*) FROM usage_samples WHERE domain = ?", (sys.argv[1],)).fetchone()[0])' "$R"
  ok "trước khi xoá: R có mẫu số liệu" bash -c '[ "$(sed -nE "s/^mau [^ ]+ ([0-9]+)$/\1/p" <<<"$1")" -ge 1 ]' _ "$OUT"
  vroot tpan site rm "$R" --yes
  ok "site rm $R: job thành công" job_ok
  vroot tpan trash purge "$R" --yes
  ok "trash purge $R: thoát 0" rc_is 0
  vroot bash -c 'ls /etc/systemd/system /etc/tpan 2>/dev/null | grep -c "$1"; ls /etc/tpan/cage 2>/dev/null | grep -c "$1"; systemctl list-units --all --no-legend "tpan-cage-$1-*" "tpan-$1.slice" | wc -l; ls /run/tpan-cage 2>/dev/null | wc -l' _ "$slug"
  ok "không còn file hay unit của lồng $R (trong /etc/systemd/system, /etc/tpan/cage, systemd)" bash -c '[ "$(sed -n 1p <<<"$1")" = 0 ] && [ "$(sed -n 2p <<<"$1")" = 0 ] && [ "$(sed -n 3p <<<"$1" | tr -d " ")" = 0 ]' _ "$OUT"
  vroot python3 -c 'import sqlite3, sys; c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True); print("mau", sys.argv[1], c.execute("SELECT COUNT(*) FROM usage_samples WHERE domain = ?", (sys.argv[1],)).fetchone()[0]); print("gio", sys.argv[1], c.execute("SELECT COUNT(*) FROM usage_hourly WHERE domain = ?", (sys.argv[1],)).fetchone()[0])' "$R"
  ok "xoá hẳn cũng xoá mọi mẫu và dòng giờ của $R" bash -c 'grep -q "^mau [^ ]* 0$" <<<"$1" && grep -q "^gio [^ ]* 0$" <<<"$1"' _ "$OUT"
  health_json
  ok "health không có lỗi sau khi xoá hẳn R" no_fail
}

# khoi_dong_lai: chỉ nginx (như các e2e trước, một lần khởi động lại cho cả mốc).
khoi_dong_lai() {
  local _
  section "khởi động lại máy ảo: A, B 200; site tạm khoá vẫn 403 và socket vẫn disabled"
  vroot tpan site suspend "$R" --yes
  ok "site suspend $R (để thử qua lần khởi động lại): job thành công" job_ok
  run multipass restart "$VMNAME"
  ok "multipass restart thoát 0" rc_is 0
  for _ in $(seq 1 40); do
    "$VM" exec true </dev/null >/dev/null 2>&1 && break
    sleep 3
  done
  addrs
  ok "A trả 200 sau khi khởi động lại (request đầu đánh thức lồng, /run/mysqld được tạo lại)" wait_200 "$A" 120
  ok "B trả 200 sau khi khởi động lại" wait_200 "$B" 60
  web "$A"
  ok "A: trang chủ 200 và permalink 200" bash -c 'grep -qx "trang-chu=200" <<<"$1" && grep -qx "permalink=200" <<<"$1"' _ "$OUT"
  web "$R"
  ok "$R tạm khoá vẫn trả 403" has '^trang-chu=403$'
  vsc cgroup trang-thai "$R"
  ok "socket của $R vẫn disabled và không active (nằm yên sau khởi động lại)" bash -c '[ "$1" = disabled ] && [ "$2" != active ]' _ "$(kv SOCKET_ENABLED)" "$(kv SOCKET_ACTIVE)"
  vsc cgroup trang-thai "$A"
  ok "socket của A active, file socket có" bash -c '[ "$1" = active ] && [ "$2" = co ]' _ "$(kv SOCKET_ACTIVE)" "$(kv FILE_SOCKET)"
  vsc cgroup doc "$A" memory.max
  ok "trần của gói được áp đúng sau khởi động lại: memory.max của A là 209715200" kv_is memory.max 209715200
  usage_row "$A"
  ok "bộ đếm cộng dồn về 0 sau khi máy khởi động lại (oom_kills của A)" jout '.usage.oom_kills == 0'
  health_json
  ok "health sau khởi động lại: không có lỗi" no_fail
  ok "cage.limits của A và B đạt" limits_ab_pass
  vroot tpan site unsuspend "$R" --yes
  ok "site unsuspend $R: job thành công" job_ok
}

part_stack() {
  local web=$1
  part_begin "$web: lồng theo site trên stack $web từ máy sạch"
  stack_setup "$web"
  goi_user
  site_dau
  nhin_tu_a
  qua_tai_va_so_lieu "$web"
  chuyen_long
  thao_tac_cua_a "$web"
  tran_va_goi
  thu_tan_cong "$web"
  api_phan_quyen
  if [ "$web" = nginx ]; then
    khoi_dong_lai
  else
    note "phần apache bỏ khởi động lại máy (đã làm ở phần nginx)"
  fi
  xoa_hang_r
  section "cuối phần $web"
  health_json
  ok "health không có lỗi" no_fail
  web_assert "$A" "cuối phần, A"
  web_assert "$B" "cuối phần, B"
  web_assert "$O" "cuối phần, O"
  ssl_trong_long
  part_end "$web"
}

# ================================================================ NÂNG CẤP

hash_dirs=(/etc/nginx /etc/php /etc/cron.d /etc/systemd/system)

part_nang_cap() {
  local web=${E2E_UP_WEB:-nginx} base old_base snap_a snap_b
  part_begin "nang-cap: $OLD_VER lên $VER ($web), rồi bật lồng và tắt lồng"
  old_base="$(basename "$OLD_DIR")"

  section "U0: gói phát hành $OLD_VER (bản đã phát hành, không build lại), máy sạch"
  run bash -c 'cd "$1" && ls -l && cat SHA256SUMS && shasum -a 256 -c SHA256SUMS' _ "$OLD_DIR"
  ok "gói $OLD_VER có đủ file, SHA256SUMS khớp" rc_is 0
  reset_vm
  run multipass transfer -r "$OLD_DIR" "$VMNAME:/home/ubuntu/"
  ok "chép gói $OLD_VER sang máy ảo" rc_is 0

  section "U1: cài $OLD_VER bằng install.sh của chính nó, stack $web, hai site"
  install_run "$VM" exec bash -c "cd /home/ubuntu/$old_base && sudo ./install.sh --web $web"
  ok "install.sh $OLD_VER thoát 0" rc_is 0
  ok "install.sh in bản $OLD_VER" has "^tpan $OLD_VER "
  save_pw admin "$PW"
  vexec tpan version
  ok "tpan version là $OLD_VER" has "^tpan $OLD_VER "
  site_add "$A"
  site_add "$B"
  web_assert "$A" "$OLD_VER, site A"
  web_assert "$B" "$OLD_VER, site B"
  health_json
  jq -r '[.groups[].results[] | "\(.id) \(.status)"] | sort | .[]' "$HJ" >"$TMPD/health-truoc.txt"
  ok "(ghi lại) health trước khi nâng có phép kiểm" test -s "$TMPD/health-truoc.txt"
  vsc bam site "$A"
  snap_a="$OUT"
  vsc bam site "$B"
  snap_b="$OUT"
  vsc bam cay truoc "${hash_dirs[@]}"
  ok "sha256 mọi file dưới ${hash_dirs[*]} đã ghi" kv_ge BAM_truoc_SO_FILE 20

  section "U2: nâng lên $VER bằng tpan self-update --url của bản $OLD_VER"
  run git log --oneline -1
  run git status --short
  run make release VERSION="$VER" DATE="$BUILD_DATE"
  ok "make release VERSION=$VER" rc_is 0
  run bash -c 'cd "$1" && cat SHA256SUMS && shasum -a 256 -c SHA256SUMS' _ "dist/release/$VER"
  ok "SHA256SUMS của gói $VER khớp" rc_is 0
  vexec mkdir -p /home/ubuntu/rel
  run multipass transfer -r "dist/release/$VER" "$VMNAME:/home/ubuntu/rel/"
  ok "chép gói $VER sang máy ảo" rc_is 0
  vroot systemd-run --unit=tpan-e2e-rel python3 -m http.server "$PORT" --bind 127.0.0.1 --directory /home/ubuntu/rel
  sleep 2
  base="http://127.0.0.1:$PORT/$VER"
  vexec curl -sS -o /dev/null -w '%{http_code}\n' "$base/SHA256SUMS"
  ok "(đồ nghề) máy chủ HTTP loopback trả $VER/SHA256SUMS" out_is 200
  vroot tpan self-update --url "$base" --dry-run
  ok "self-update --dry-run (bản $OLD_VER) thoát 0" rc_is 0
  vroot tpan self-update --url "$base" --yes
  ok "self-update (bản $OLD_VER) thoát 0, job thành công" job_ok
  ok "in $OLD_VER -> $VER" hasF "$OLD_VER -> $VER"
  vroot systemctl stop tpan-e2e-rel
  vexec tpan version
  ok "tpan version là $VER" has "^tpan $VER "
  vroot python3 -c 'import sqlite3; c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True); print("migration:", [r[0] for r in c.execute("SELECT version FROM schema_migrations ORDER BY version")]); print("site trong lồng:", c.execute("SELECT COUNT(*) FROM sites WHERE caged = 1").fetchone()[0]); print("gói bật lồng:", c.execute("SELECT COUNT(*) FROM packages WHERE cage = 1").fetchone()[0]); print("site:", c.execute("SELECT COUNT(*) FROM sites").fetchone()[0])'
  ok "migration 1700 đã chạy; mọi cột mới ở giá trị mặc định (0 site trong lồng, 0 gói bật lồng), 2 site còn nguyên" bash -c 'grep -q "^migration:.*1700" <<<"$1" && grep -q "^site trong lồng: 0$" <<<"$1" && grep -q "^gói bật lồng: 0$" <<<"$1" && grep -q "^site: 2$" <<<"$1"' _ "$OUT"

  section "U3: sau khi nâng: bất biến 1 (không file nào đổi), health như trước, không unit của lồng"
  vsc bam cay sau "${hash_dirs[@]}"
  vsc bam so truoc sau
  ok "nâng cấp: sha256 mọi file cấu hình dưới ${hash_dirs[*]} trước và sau bằng nhau" kv_is DIFF_DONG 0
  vsc bam site "$A"
  ok "sha256 pool, vhost, cron, wp-config, .htaccess của $A không đổi" out_is "$snap_a"
  vsc bam site "$B"
  ok "sha256 năm file của $B không đổi" out_is "$snap_b"
  vroot bash -c 'systemctl list-units --all --no-legend "tpan-cage-*" "tpan-*.slice" | wc -l; ls /etc/systemd/system | grep -c "^tpan-cage-"; ls /etc/tpan/cage 2>&1 | head -1'
  ok "không unit hay file tpan-cage-*, không thư mục /etc/tpan/cage" bash -c '[ "$(sed -n 1p <<<"$1" | tr -d " ")" = 0 ] && [ "$(sed -n 2p <<<"$1")" = 0 ] && sed -n 3p <<<"$1" | grep -q "No such file"' _ "$OUT"
  web_assert "$A" "sau nâng, site A"
  web_assert "$B" "sau nâng, site B"
  health_json
  jq -r '[.groups[].results[] | "\(.id) \(.status)"] | sort | .[]' "$HJ" >"$TMPD/health-sau.txt"
  run bash -c 'comm -23 "$1" "$2"' _ "$TMPD/health-truoc.txt" "$TMPD/health-sau.txt"
  ok "health như trước: mọi phép kiểm có từ trước giữ nguyên trạng thái" bash -c '[ -z "$1" ]' _ "$OUT"
  run bash -c 'comm -13 "$1" "$2"' _ "$TMPD/health-truoc.txt" "$TMPD/health-sau.txt"
  note "phép kiểm mới sau khi nâng (nếu có): $(tr '\n' ';' <<<"$OUT")"
  ok "không phép kiểm mới nào là fail, không phép kiểm cage.* nào (không gói nào bật lồng)" bash -c '! grep -qE " fail$|^cage[.]" <<<"$1"' _ "$OUT"
  ok "health không có lỗi" no_fail

  section "U4: bật lồng cho gói: hai site 200 trong lồng"
  vroot tpan package add "$PKL" --max-sites 6 --max-dbs 6 --cage on --cpu 100 --ram-mb 256 --tasks 60 --php-workers 5 --yes
  ok "package add $PKL (lồng bật)" rc_is 0
  vroot tpan user edit admin --package "$PKL" --yes
  ok "user edit admin --package $PKL thoát 0 (chưa site nào trong lồng: không job)" rc_is 0
  vsc bam site "$A"
  ok "đổi gói không ghi lại file nào của site" out_is "$snap_a"
  vjson tpan cage status --json
  ok "cage status --json: hai site muốn lồng mà chưa vào (can-vao)" jout '(if type == "array" then . else [.] end) | ([.[] | select(.state == "can-vao")] | length) == 2'
  vroot tpan cage enable --all --yes
  ok "cage enable --all thoát 0" rc_is 0
  cage_row "$A"
  ok "$A trong lồng" jout '.caged == true and .state == "trong"'
  cage_row "$B"
  ok "$B trong lồng" jout '.caged == true and .state == "trong"'
  web_assert "$A" "trong lồng, A"
  web_assert "$B" "trong lồng, B"
  vsc tien-trinh kiem-ban "$A"
  ok "A trong lồng: không file pool chung, đúng một master" bash -c '[ "$1" = 0 ] && [ "$2" = 0 ] && [ "$3" = 1 ]' _ "$(kv POOL_CHUNG_FILE)" "$(kv POOL_CHUNG_USER)" "$(kv MASTER)"
  vsc tien-trinh ngoai-slice "$A"
  ok "A: không tiến trình nào của user A ngoài slice" kv_is NGOAI_SLICE 0
  health_json
  ok "health: lồng của A và B đạt, không có lỗi" cage_pass_ab_no_fail

  section "U5: tắt lồng: sha256 về đúng như trước"
  vroot tpan cage disable --all --yes
  ok "cage disable --all thoát 0" rc_is 0
  cage_row "$A"
  ok "$A ra khỏi lồng" jout '.caged == false and .files == false'
  vsc bam site "$A"
  ok "bất biến 2: sha256 năm file của $A về đúng như trước khi nâng" out_is "$snap_a"
  vsc bam site "$B"
  ok "sha256 năm file của $B về đúng như trước khi nâng" out_is "$snap_b"
  vsc bam cay sau2 "${hash_dirs[@]}"
  vsc bam so truoc sau2
  ok "sha256 mọi file dưới ${hash_dirs[*]} về đúng như trước khi nâng" kv_is DIFF_DONG 0
  web_assert "$A" "ra lồng, A"
  web_assert "$B" "ra lồng, B"
  health_json
  ok "health không có lỗi" no_fail

  section "U6: self-update --rollback bị từ chối khi còn site trong lồng"
  vroot tpan cage enable --all --yes
  ok "cage enable --all (lần hai) thoát 0" rc_is 0
  vjson tpan self-update --rollback --yes --json
  ok "self-update --rollback --json: từ chối cage_active" refused cage_active
  vroot tpan self-update --rollback --yes
  ok "self-update --rollback (dạng chữ): thoát khác 0, lời từ chối nêu lệnh tpan cage disable --all" bash -c '[ "$2" != 0 ] && grep -qF "tpan cage disable --all" <<<"$1"' _ "$OUT" "$RC"
  vexec tpan version
  ok "tpan vẫn là bản $VER (không lùi)" has "^tpan $VER "
  web_assert "$A" "sau rollback bị từ chối, A"
  vroot tpan cage disable --all --yes
  ok "cage disable --all (dọn lại) thoát 0" rc_is 0
  vroot tpan self-update --rollback --dry-run
  ok "hết site trong lồng: self-update --rollback --dry-run thoát 0 (không còn bị cage_active chặn)" rc_is 0
  part_end nang-cap
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

usage() { awk 'NR > 1 && /^#/ { sub(/^# ?/, ""); print; next } NR > 1 { exit }' "$0" | sed -n '1,13p'; }

[ $# -ge 1 ] || { usage; exit 2; }
PARTS=()
for p in "$@"; do
  case "$p" in
    nginx | apache | nang-cap) PARTS+=("$p") ;;
    chinh) PARTS+=(nginx apache) ;;
    all) PARTS+=(nginx apache nang-cap) ;;
    *)
      usage
      exit 2
      ;;
  esac
done

printf 'tpan của cây: %s; bản cũ: %s; máy ảo: %s; các phần: %s\n' "$VER" "$OLD_VER" "$VMNAME" "${PARTS[*]}"
for p in "${PARTS[@]}"; do
  case "$p" in
    nginx) part_stack nginx ;;
    apache) part_stack apache ;;
    nang-cap) part_nang_cap ;;
  esac
done

leak_check
section "TỔNG KẾT: $PASSES khẳng định đạt, $FAILS khẳng định không đạt"
[ "$FAILS" -eq 0 ]
