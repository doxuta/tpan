#!/usr/bin/env bash
# e2e của mốc M15 (nút cập nhật TPAN trong panel; mục 6.14, bước 18 ở mục 12 của
# docs/KE-HOACH.md) trên máy ảo (tên trong file .tpan-vm; không có thì
# tpan-test). Chạy trên Mac, trong thư mục repo, từ máy sạch:
#
#   E2E_LOG=docs/bang-chung/M15.log scripts/e2e-m15.sh > docs/bang-chung/M15.log 2>&1
#
# Ba bản tpan trong kịch bản:
#   bản cũ  v0.2.0, gói đã phát hành ở dist/release/v0.2.0 (không build lại)
#   bản A   bản của cây đang đứng (make release)
#   bản B   cùng mã với A, chỉ khác tên bản (make release VERSION=<A>-e2e-b):
#           "bản mới" để nút cập nhật có thứ mà lên
#
# Kịch bản (mỗi phần có khẳng định):
#   0  máy sạch; đóng gói A và B; dựng nguồn tải trên loopback trong máy ảo.
#   A  cài v0.2.0 bằng install.sh của chính gói đó (--web nginx), một site,
#      đăng nhập API lấy cookie C1.
#   B  v0.2.0 lên A bằng `tpan self-update --url` CỦA v0.2.0 (v0.2.0 chưa có
#      API cập nhật): panel restart, C1 còn dùng được, site 200, health không
#      có [lỗi] mới.
#   C  API trên bản A, gọi như giao diện gọi: GET /self; "Kiểm bản mới" với
#      nguồn tải mặc định (lần duy nhất ra mạng thật) rồi với nguồn loopback;
#      auto=1 dùng lại lần kiểm; phân quyền (user thường 403, không cookie
#      401) và CSRF; một lượt cùng lúc (giữ khoá toàn cục cho lượt chạy phải
#      chờ: lần bấm thứ hai 409, dòng lệnh của đơn vị tạm là hằng); nhả khoá:
#      A lên B, panel restart, C1 còn dùng được, last/audit/jobs/journal.
#   D  nguồn tải có SHA256SUMS sai: file chạy cũ còn nguyên, không còn
#      tpan.new, last.status failed kèm lý do có hai số sha256. Nguồn tải
#      chết: check.error, failed.
#   C7 (sau D, lý do ở đầu part_c7) đã mới nhất thì lượt chạy kết thúc "same"
#      và để lại tpan.new cùng bản; nguồn sai lần nữa: tpan.new đó còn nguyên.
#   E  POST /self/rollback: B về A. Lần nữa: 409 self_no_old.
#   F  đặt file chạy v0.1.2 làm tpan.old: API và CLI từ chối self_rollback_db
#      kèm câu của chính file cũ, không đổi gì.
#   G  đơn vị tạm bị giết giữa chừng: last.status thành interrupted, lượt sau
#      vẫn chạy được.
#   H  CLI không đổi hành vi: --dry-run ba bước, `internal` ẩn khỏi help,
#      self-update rồi --rollback ghi audit ip "cli".
#
# ĐỒ NGHỀ TEST, CHỈ TRONG MÁY ẢO (không phải cách dùng tpan):
#   - Nguồn tải: ba thư mục a, b, sai phục vụ bằng `python3 -m http.server`
#     trên 127.0.0.1, chạy như đơn vị tạm tpan-e2e-http. Thư mục "sai" là file
#     chạy của B với SHA256SUMS ghi 64 số 0.
#   - Setting update.self_url (nguồn tải của nút cập nhật): API không nhận URL
#     từ trình duyệt và chưa có lệnh CLI nào đặt setting này, nên e2e ghi thẳng
#     bảng settings của tpan.db bằng scripts/e2e-m15/db.py (python3 + sqlite3).
#   - Giữ khoá toàn cục của tpan (/run/tpan/tpan.lock, khoá mọi job lấy) bằng
#     `flock ... sleep` trong đơn vị tạm tpan-e2e-lock, để lượt cập nhật phải
#     chờ và e2e kịp gọi lần hai.
#   - Đặt file chạy v0.1.2 vào /usr/local/bin/tpan.old (phần F), gỡ sau khi thử.
#
# Bản cũ: OLD_DIR (mặc định dist/release/v0.2.0) và OLDER_DIR (mặc định
# dist/release/v0.1.2), dist/ không vào git. Chưa có thì:
#   gh release download v0.2.0 --repo <repo> --dir dist/release/v0.2.0
#
# Đặt E2E_LOG=<file log> để cuối lần chạy script tự tìm trong chính file log
# đó: mọi mật khẩu và cookie đã gặp, IP máy ảo (IPv4, IPv6), IP của Mac, thư
# mục nhà và tên user Mac (phải là 0 lần).
#
# LỚP BỌC LỆNH MÁY ẢO (vm_cap, như e2e-m14.sh): mọi lệnh `scripts/vm.sh
# exec|root ...` in trong log được chạy bằng `multipass exec` qua một lớp bọc:
# output ghi vào file TRONG máy ảo, ngắn thì in về trong cùng lần gọi, dài thì
# lấy về bằng `multipass transfer` (`multipass exec` trên Mac có lúc treo giữa
# chừng khi output dài). Lệnh, output và mã thoát in trong log là của chính
# lệnh thật.
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
SITE=a.example.com
UU=khach_m15
OLD_VER=v0.2.0
OLD_DIR="${OLD_DIR:-dist/release/$OLD_VER}"
OLDER_VER=v0.1.2
OLDER_DIR="${OLDER_DIR:-dist/release/$OLDER_VER}"
VER="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
VER_B="$VER-e2e-b"
# Thư mục "sai" mang một tên bản khác B: theo hợp đồng (core.SelfCheck),
# latest trùng bản đang chạy thì up_to_date là true dù sha256 khác.
VER_SAI="$VER-e2e-sai"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
STAGE=dist/e2e-m15
E=/home/ubuntu/e2e-m15   # thư mục đồ nghề trong máy ảo
PORT=8015
DEAD_PORT=8016           # không có gì nghe ở cổng này
SRC="http://127.0.0.1:$PORT"
DEFAULT_URL=https://github.com/doxuta/tpan/releases/latest/download
UNIT=tpan-self-update
HTTP_UNIT=tpan-e2e-http
LOCK_UNIT=tpan-e2e-lock
LOCK=/run/tpan/tpan.lock
ZEROS=0000000000000000000000000000000000000000000000000000000000000000
ARCH=""
SUM_OLD=""
SUM_A=""
SUM_B=""

TMPD="$(mktemp -d)"
chmod 700 "$TMPD"
SECRETS="$TMPD/secrets"
: >"$SECRETS"
chmod 600 "$SECRETS"
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
# stdin là /dev/null (multipass exec chờ stdin mãi khi không có terminal).
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
  local _
  for _ in 1 2 3; do
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
# bail <lý do>: điều kiện đầu của cả kịch bản hỏng. Dừng ngay (tính là một
# khẳng định không đạt) thay vì in hàng trăm dòng "không đạt" kéo theo.
bail() {
  printf '  => KHÔNG ĐẠT: DỪNG KỊCH BẢN: %s\n' "$*"
  FAILS=$((FAILS + 1))
  leak_check
  section "TỔNG KẾT: $PASSES khẳng định đạt, $FAILS khẳng định không đạt (dừng giữa chừng)"
  exit 1
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
job_ok() { [ "$RC" = 0 ] && grep -q '^Job .*: thành công' <<<"$OUT"; }
eq() { [ "$1" = "$2" ]; }
ne() { [ "$1" != "$2" ]; }

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

# install_run <lệnh...>: một lần chạy install.sh; nhặt mật khẩu quản trị (che,
# đếm số lần in) trước khi in. Kết quả: RC, OUT, PW, PW_HITS.
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

# site_add <domain>: tpan site add, nhặt mật khẩu quản trị WordPress và mật
# khẩu DB của site vào danh sách che trước khi in.
site_add() {
  local d=$1 pw
  runq "$VM" root tpan site add "$d" --email "$EMAIL" --yes
  pw="$(tr -d '\r' <"$TMPD/run.out" | sed -nE 's/^  Mật khẩu: +([A-Za-z0-9]{32})$/\1/p' | head -1)"
  add_secret "$pw"
  add_secret "$(dbpw "$d")"
  show_last
  ok "site add $d: thoát 0, job thành công" job_ok
}

# json_secret <tên> <biểu thức jq> <lệnh...>: chạy lệnh --json, đọc mật khẩu
# theo biểu thức jq vào $TMPD/pw.<tên> rồi mới in.
JS_PW=""
json_secret() {
  local name=$1 expr=$2 pw
  shift 2
  RUNQ_CMD="$(mask_str <<<"$*")"
  RUNQ_TS="$(ts)"
  shift # bỏ "$VM": lệnh luôn là `"$VM" root|exec ...`
  vm_cap2 "$@"
  pw="$(tr -d '\r' <"$TMPD/cap.out" | jq -r "$expr // empty" 2>/dev/null)"
  [ -n "$pw" ] && save_pw "$name" "$pw"
  cat "$TMPD/cap.err" "$TMPD/cap.out" >"$TMPD/run.out"
  show_last
  JS_PW="$pw"
}

# page: mã HTTP của trang chủ site, gọi từ trong máy ảo. Kết quả ở OUT.
page() { vexec curl -s -o /dev/null -m 20 -w '%{http_code}\n' --resolve "$SITE:80:127.0.0.1" "http://$SITE/"; }
is200() { [ "$OUT" = 200 ]; }

# state: ảnh chụp những thứ một lượt cập nhật có thể đổi (scripts/e2e-m15/
# trang-thai.sh chạy trong máy ảo). `state cho [N]`: chờ đơn vị tạm xong trước.
# Kết quả ở các biến ST_*.
ST_NOW=""
ST_VER=""
ST_SHA=""
ST_OLD=""
ST_NEW=""
ST_PID=""
ST_ACT=""
ST_UNIT=""
ST_URL=""
ST_LAST=""
ST_WAIT=""
ST_GAP=""
ST_GAP2=""
stv() { sed -n "s/^$1=//p" <<<"$OUT" | head -1; }
state() {
  vroot bash "$E/trang-thai.sh" "$@"
  ST_NOW="$(stv luc)"
  ST_VER="$(stv phien-ban | awk '{print $2}')"
  ST_SHA="$(stv sha-tpan)"
  ST_OLD="$(stv sha-old)"
  ST_NEW="$(stv sha-new)"
  ST_PID="$(stv MainPID)"
  ST_ACT="$(stv ActiveState)"
  ST_UNIT="$(stv don-vi-tam)"
  ST_URL="$(stv update.self_url)"
  ST_LAST="$(stv update.self_last)"
  ST_WAIT="$(stv cho-don-vi)"
  ST_GAP="$(stv khong-active-ms)"
  ST_GAP2="$(stv tat-den-nghe-ms)"
}
unit_done() { [ "${ST_WAIT#xong}" != "$ST_WAIT" ]; }

# db <lệnh của db.py...>: đọc (hoặc đặt nguồn tải) thẳng tpan.db trong máy ảo.
db() { vroot python3 "$E/db.py" "$@"; }
set_url() {
  db seturl "$1"
  ok "(đồ nghề) setting update.self_url là $1" out_is "update.self_url=$1"
}

# Giữ và nhả khoá toàn cục của tpan (đồ nghề, xem đầu file).
lock_hold() {
  vroot systemd-run --unit "$LOCK_UNIT" --collect flock "$LOCK" sleep 300
  sleep 1
  vroot bash -c 'flock -n "$1" true; echo "thử lấy khoá không chờ: mã $?"' _ "$LOCK"
  ok "(đồ nghề) khoá toàn cục $LOCK đang bị giữ" hasF 'thử lấy khoá không chờ: mã 1'
}
lock_release() { vroot systemctl stop "$LOCK_UNIT"; }

# ---------------------------------------------------------------- health

# health_errs <file>: chạy tpan health (dạng chữ), ghi các dòng [lỗi] đã xếp vào file.
health_errs() {
  vroot tpan health
  grep -E '^\[lỗi\]' <<<"$OUT" | sort >"$1"
  printf 'Số dòng [lỗi] của tpan health: %s\n' "$(grep -c . "$1")"
}
# new_errs <file trước> <file sau>: các dòng [lỗi] chỉ có ở file sau.
new_errs() { comm -13 "$1" "$2"; }

# ---------------------------------------------------------------- API (từ Mac)

# api <jar> <METHOD> <đường dẫn> [thân JSON]: gọi panel qua TLS (--cacert là
# cert.pem lấy từ máy ảo, không -k), cookie ở jar. Mặc định mọi request mang
# Origin của chính panel và Sec-Fetch-Site: same-origin như trình duyệt; phép
# thử CSRF đổi hai header đó qua API_SFS và API_ORIGIN ("-" là không gửi).
# Thân trả về ở $TMPD/body; mọi trường "password" trong đó và giá trị cookie
# vào danh sách che TRƯỚC khi in.
API_SFS=same-origin
API_ORIGIN=""
API_MAX=1600
api() {
  local jar=$1 m=$2 p=$3 body=${4-} hdr=""
  local args=(-sS -m 120 --cacert "$TMPD/cert.pem" -b "$jar" -c "$jar" -X "$m" -o "$TMPD/body"
    -w '-> HTTP %{http_code}\n' -H 'Accept: application/json')
  if [ "$API_SFS" != - ]; then args+=(-H "Sec-Fetch-Site: $API_SFS"); fi
  if [ "$API_ORIGIN" != - ]; then args+=(-H "Origin: ${API_ORIGIN:-$BASE}"); fi
  if [ "$API_SFS" != same-origin ] || [ -n "$API_ORIGIN" ]; then
    hdr="   [Sec-Fetch-Site: ${API_SFS/#-/(không gửi)}; Origin: ${API_ORIGIN:-<panel>}]"
  fi
  : >"$TMPD/body"
  if [ -n "$body" ]; then
    (umask 077 && printf '%s' "$body" >"$TMPD/req.json")
    args+=(-H 'Content-Type: application/json' --data-binary "@$TMPD/req.json")
  fi
  printf '\n[%s] $ curl -X %s <panel>%s   (phiên: %s)%s\n' "$(ts)" "$m" "$p" "${jar##*/}" "$hdr"
  [ -n "$body" ] && printf '    thân gửi: %s\n' "$(mask_str <<<"$body")"
  curl "${args[@]}" "$BASE$p" </dev/null >"$TMPD/run.out" 2>&1
  RC=$?
  CODE="$(sed -nE 's/^-> HTTP ([0-9]{3})$/\1/p' "$TMPD/run.out" | tail -1)"
  harvest "$jar"
  OUT="$(tr -d '\r' <"$TMPD/run.out")"
  mask
  printf '%s\n' "$OUT"
  if jq -e . "$TMPD/body" >/dev/null 2>&1; then
    OUT="$(jq -c . "$TMPD/body" | head -c "$API_MAX")"
  else
    OUT="$(head -c 400 "$TMPD/body")"
  fi
  mask
  [ -n "$OUT" ] && printf '    thân nhận (tối đa %s byte): %s\n' "$API_MAX" "$OUT"
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
# bq <biểu thức jq>: jq -e trên thân vừa nhận (thô, chưa che). Có sẵn $a (bản
# A), $b (bản B), $old (bản cũ), $arch, $src (gốc nguồn tải loopback).
bq() { jq -e --arg a "$VER" --arg b "$VER_B" --arg old "$OLD_VER" --arg arch "$ARCH" --arg src "$SRC" "$@" "$TMPD/body" >/dev/null 2>&1; }
jb() { jq -r "$1" "$TMPD/body" 2>/dev/null; }

new_jar() { : >"$TMPD/jar.$1"; chmod 600 "$TMPD/jar.$1"; printf '%s' "$TMPD/jar.$1"; }

# login <jar> <tên> <file mật khẩu>: POST /api/v1/login.
login() {
  api "$1" POST /api/v1/login "$(jq -nc --arg n "$2" --rawfile p "$3" '{name: $n, password: $p}')"
}

fetch_cert() {
  "$VM" root cat /etc/tpan/tls/cert.pem </dev/null | tr -d '\r' >"$TMPD/cert.pem"
  wait_panel
}
# wait_panel: chờ panel trả lời (tối đa 30 giây).
wait_panel() {
  local _
  for _ in $(seq 1 30); do
    curl -sS -m 5 --cacert "$TMPD/cert.pem" -o /dev/null "$BASE/api/v1/health" </dev/null 2>/dev/null && return 0
    sleep 1
  done
  return 1
}

now_ms() { perl -MTime::HiRes=time -e 'printf "%d\n", time * 1000'; }

# poll_bg <jar> <bản chờ> <tối đa giây>: chạy NỀN, hỏi GET /api/v1/session bằng
# cookie của jar cho tới khi version là bản chờ, như giao diện hỏi lại sau khi
# bấm nút. Lỗi kết nối trong lúc panel restart là bình thường: đếm, không dừng.
# Hỏi dày (0,05 giây một lần; giao diện thật hỏi thưa hơn) để ước lượng được
# khoảng panel không trả lời. poll_wait chờ nó xong, in tóm tắt, đặt POLL_*.
POLL_PID=""
POLL_OK=0
POLL_FAILS=0
POLL_MS=0
POLL_GAP=0
poll_bg() {
  local jar=$1 want=$2 max=$3
  : >"$TMPD/poll.res"
  (
    t0="$(now_ms)"
    n=0 fails=0 okk=0 got="" f1=0 f2=0 t="$t0"
    while [ $((t - t0)) -lt $((max * 1000)) ]; do
      n=$((n + 1))
      if curl -sS -m 3 --cacert "$TMPD/cert.pem" -b "$jar" -H 'Accept: application/json' -o "$TMPD/poll.json" "$BASE/api/v1/session" </dev/null 2>/dev/null; then
        t="$(now_ms)"
        # Lần trả lời được đầu tiên sau một chuỗi lần hỏng: hết gián đoạn.
        if [ "$f1" -gt 0 ] && [ "$f2" -eq 0 ]; then f2="$t"; fi
        got="$(jq -r '.version // empty' "$TMPD/poll.json" 2>/dev/null)"
        if [ "$got" = "$want" ]; then
          okk=1
          break
        fi
      else
        t="$(now_ms)"
        fails=$((fails + 1))
        [ "$f1" -eq 0 ] && f1="$t"
      fi
      sleep 0.05
    done
    gap=0
    [ "$f1" -gt 0 ] && [ "$f2" -gt 0 ] && gap=$((f2 - f1))
    printf 'ok=%s n=%s fails=%s ms=%s gap=%s got=%s\n' "$okk" "$n" "$fails" "$((t - t0))" "$gap" "$got" >"$TMPD/poll.res"
  ) &
  POLL_PID=$!
}
poll_wait() {
  local want=$1 res n got
  wait "$POLL_PID" 2>/dev/null
  res="$(cat "$TMPD/poll.res")"
  POLL_OK="$(sed -nE 's/^ok=([0-9]+) .*/\1/p' <<<"$res")"
  n="$(sed -nE 's/.* n=([0-9]+) .*/\1/p' <<<"$res")"
  POLL_FAILS="$(sed -nE 's/.* fails=([0-9]+) .*/\1/p' <<<"$res")"
  POLL_MS="$(sed -nE 's/.* ms=([0-9]+) .*/\1/p' <<<"$res")"
  POLL_GAP="$(sed -nE 's/.* gap=([0-9]+) .*/\1/p' <<<"$res")"
  got="$(sed -nE 's/.* got=(.*)$/\1/p' <<<"$res")"
  POLL_OK="${POLL_OK:-0}"
  printf '\n[%s] (nền) hỏi GET <panel>/api/v1/session bằng cookie C1, 0,05 giây một lần, tới khi version là %s:\n' "$(ts)" "$want"
  printf '    %s lần hỏi trong %s ms; %s lần hỏng kết nối (panel đang restart); version ở lần hỏi cuối: %s\n' "${n:-0}" "${POLL_MS:-0}" "${POLL_FAILS:-0}" "${got:-(không có)}"
  if [ "${POLL_FAILS:-0}" -gt 0 ]; then
    printf '    gián đoạn nhìn từ trình duyệt: khoảng %s ms (từ lần hỏi hỏng đầu tiên tới lần trả lời được kế đó)\n' "${POLL_GAP:-0}"
  else
    printf '    không lần hỏi nào hỏng: lần restart lọt giữa hai lần hỏi (ngắn hơn khoảng cách hỏi)\n'
  fi
}
poll_ok() { [ "$POLL_OK" = 1 ] && [ "${POLL_MS:-999999}" -le 60000 ]; }

# journal_unit <từ lúc>: journal của đơn vị tạm từ một thời điểm (giờ của máy ảo).
journal_unit() { vroot journalctl -u "$UNIT" --no-pager -o short-precise --since "$1"; }

# ================================================================ PHẦN 0

part_0() {
  part_begin "0: máy sạch, đóng gói bản A và bản B, dựng nguồn tải trên loopback"
  reset_vm
  ARCH="$("$VM" exec dpkg --print-architecture </dev/null | tr -d '\r')"
  printf 'Kiến trúc máy ảo: %s\n' "$ARCH"

  run bash -c 'cd "$1" && ls -l && cat SHA256SUMS && shasum -a 256 -c SHA256SUMS' _ "$OLD_DIR"
  ok "gói $OLD_VER đã phát hành: đủ file, sha256 khớp SHA256SUMS" rc_is 0
  run bash -c 'cd "$1" && grep " tpan-linux-$2\$" SHA256SUMS | shasum -a 256 -c -' _ "$OLDER_DIR" "$ARCH"
  ok "file chạy $OLDER_VER ($ARCH) đã phát hành: sha256 khớp SHA256SUMS" rc_is 0
  SUM_OLD="$(awk -v f="tpan-linux-$ARCH" '$2 == f { print $1 }' "$OLD_DIR/SHA256SUMS")"

  run git log --oneline -1
  run git status --short
  run make release VERSION="$VER" DATE="$BUILD_DATE"
  ok "make release (bản A, $VER)" rc_is 0
  # make release build đè bin/: làm A xong mới làm B.
  run make release VERSION="$VER_B" DATE="$BUILD_DATE"
  ok "make release VERSION=$VER_B (bản B, cùng mã, khác tên bản)" rc_is 0

  # shellcheck disable=SC2016
  run bash -c '
    set -e
    st=$1; a=$2; b=$3; arch=$4; older=$5; sai=$6; zeros=$7
    rm -rf dist/e2e-m15
    mkdir -p "$st/rel/a" "$st/rel/b" "$st/rel/sai"
    cp "dist/release/$a/tpan-linux-$arch" "dist/release/$a/SHA256SUMS" "dist/release/$a/VERSION" "$st/rel/a/"
    cp "dist/release/$b/tpan-linux-$arch" "dist/release/$b/SHA256SUMS" "dist/release/$b/VERSION" "$st/rel/b/"
    cp "dist/release/$b/tpan-linux-$arch" "$st/rel/sai/"
    printf "%s  tpan-linux-%s\n" "$zeros" "$arch" >"$st/rel/sai/SHA256SUMS"
    printf "%s\n" "$sai" >"$st/rel/sai/VERSION"
    cp "$older/tpan-linux-$arch" "$st/tpan-cu"
    cp scripts/e2e-m15/db.py scripts/e2e-m15/trang-thai.sh "$st/"
    for d in a b sai; do
      echo "== $st/rel/$d: VERSION $(cat "$st/rel/$d/VERSION")"
      cat "$st/rel/$d/SHA256SUMS"
      (cd "$st/rel/$d" && shasum -a 256 "tpan-linux-$arch")
    done
  ' _ "$STAGE" "$VER" "$VER_B" "$ARCH" "$OLDER_DIR" "$VER_SAI" "$ZEROS"
  ok "dựng thư mục đồ nghề $STAGE (a, b, sai, file chạy $OLDER_VER, script phụ)" rc_is 0
  SUM_A="$(awk -v f="tpan-linux-$ARCH" '$2 == f { print $1 }' "$STAGE/rel/a/SHA256SUMS")"
  SUM_B="$(awk -v f="tpan-linux-$ARCH" '$2 == f { print $1 }' "$STAGE/rel/b/SHA256SUMS")"
  ok "đọc được sha256 của ba file chạy ($OLD_VER, A, B) và A khác B" bash -c '[ ${#1} -eq 64 ] && [ ${#2} -eq 64 ] && [ ${#3} -eq 64 ] && [ "$2" != "$3" ]' _ "$SUM_OLD" "$SUM_A" "$SUM_B"

  run multipass transfer -r "$OLD_DIR" "$VMNAME:/home/ubuntu/"
  ok "chép gói $OLD_VER sang máy ảo" rc_is 0
  run multipass transfer -r "$STAGE" "$VMNAME:/home/ubuntu/"
  ok "chép thư mục đồ nghề sang máy ảo ($E)" rc_is 0

  vroot systemd-run --unit "$HTTP_UNIT" --collect python3 -m http.server "$PORT" --bind 127.0.0.1 --directory "$E/rel"
  sleep 2
  vexec bash -c 'for d in a b sai; do printf "%s: SHA256SUMS %s, VERSION %s (%s)\n" "$d" "$(curl -s -o /dev/null -w "%{http_code}" "$1/$d/SHA256SUMS")" "$(curl -s -o /dev/null -w "%{http_code}" "$1/$d/VERSION")" "$(curl -s "$1/$d/VERSION")"; done; curl -sS -m 3 -o /dev/null "http://127.0.0.1:$2/" ; echo "cổng chết $2: curl mã $?"' _ "$SRC" "$DEAD_PORT"
  ok "(đồ nghề) nguồn tải loopback trả a, b, sai" bash -c '[ "$(grep -c "SHA256SUMS 200, VERSION 200" <<<"$1")" -eq 3 ]' _ "$OUT"
  ok "(đồ nghề) cổng $DEAD_PORT không có gì nghe (curl mã 7)" hasF "cổng chết $DEAD_PORT: curl mã 7"
  part_end 0
}

# ================================================================ PHẦN A

PID_OLD=""
H0=""
part_a() {
  part_begin "A: cài $OLD_VER từ bản phát hành bằng install.sh của chính gói đó"
  # `sudo bash ./install.sh`, không phải `sudo ./install.sh`: file tải về từ
  # trang bản phát hành không mang quyền chạy (gh release download, trình
  # duyệt), và e2e dùng đúng các file đó, không chmod lại.
  install_run "$VM" exec bash -c "cd /home/ubuntu/$OLD_VER && sudo bash ./install.sh --web nginx"
  ok "install.sh $OLD_VER thoát 0" rc_is 0
  [ "$RC" -eq 0 ] || bail "không cài được $OLD_VER: các phần sau không có nghĩa"
  ok "mật khẩu quản trị in đúng một lần (giữ ngoài log)" test "$PW_HITS" -eq 1
  save_pw admin "$PW"
  vexec tpan version
  ok "tpan version là $OLD_VER" has "^tpan $OLD_VER "
  vexec systemctl is-active tpan nginx php8.3-fpm mariadb
  ok "tpan.service và ba dịch vụ của stack đều active" bash -c '[ "$(grep -cx active <<<"$1")" -eq 4 ]' _ "$OUT"
  site_add "$SITE"
  page
  ok "$SITE trả 200" is200

  fetch_cert
  ok "panel trả lời qua https (--cacert là chứng chỉ của chính panel)" rc_is 0
  C1="$(new_jar C1)"
  login "$C1" admin "$TMPD/pw.admin"
  ok "$OLD_VER: đăng nhập admin lấy cookie C1: 204" code_is 204
  api "$C1" GET /api/v1/session
  ok "C1: logged_in, user admin, version $OLD_VER" bq '.logged_in == true and .user.name == "admin" and .version == $old'
  api "$C1" GET /api/v1/self
  ok "$OLD_VER chưa có API cập nhật: GET /api/v1/self không phải 200" bash -c '[ "$1" != 200 ]' _ "$CODE"

  state
  PID_OLD=$ST_PID
  ok "file chạy đang cài đúng là file của gói $OLD_VER (sha256), tpan.service active" bash -c '[ "$1" = "$2" ] && [ "$3" = active ]' _ "$ST_SHA" "$SUM_OLD" "$ST_ACT"
  H0="$TMPD/health.0"
  health_errs "$H0"
  part_end A
}

# ================================================================ PHẦN B

part_b() {
  part_begin "B: $OLD_VER lên bản A ($VER) bằng tpan self-update của chính $OLD_VER"
  vroot tpan self-update --dry-run --url "$SRC/a"
  ok "self-update --dry-run ($OLD_VER) thoát 0" rc_is 0
  vroot tpan self-update --yes --url "$SRC/a"
  ok "self-update ($OLD_VER) thoát 0, job thành công" job_ok
  ok "in $OLD_VER -> $VER" hasF "$OLD_VER -> $VER"
  wait_panel
  state
  ok "tpan version là A ($VER)" eq "$ST_VER" "$VER"
  ok "file chạy là file của gói A, tpan.old là file của $OLD_VER (sha256)" bash -c '[ "$1" = "$2" ] && [ "$3" = "$4" ]' _ "$ST_SHA" "$SUM_A" "$ST_OLD" "$SUM_OLD"
  ok "panel đã restart: MainPID đổi ($PID_OLD -> $ST_PID), tpan.service active" bash -c '[ -n "$2" ] && [ "$1" != "$2" ] && [ "$3" = active ]' _ "$PID_OLD" "$ST_PID" "$ST_ACT"
  api "$C1" GET /api/v1/session
  ok "cookie C1 (tạo bởi $OLD_VER) vẫn đăng nhập: logged_in, version là A" bq '.logged_in == true and .user.name == "admin" and .version == $a'
  page
  ok "$SITE vẫn 200" is200

  local h1="$TMPD/health.1" moi
  health_errs "$h1"
  moi="$(new_errs "$H0" "$h1")"
  if [ -n "$moi" ]; then
    note "health có [lỗi] mới sau khi nâng; chạy tpan security harden rồi kiểm lại:"
    printf '%s\n' "$moi"
    vroot tpan security harden --yes
    ok "tpan security harden thoát 0, job thành công" job_ok
    health_errs "$h1"
    moi="$(new_errs "$H0" "$h1")"
  fi
  ok "tpan health sau khi nâng không có [lỗi] mới so với trước khi nâng" test -z "$moi"
  part_end B
}

# ================================================================ PHẦN C

PID_A=""
part_c() {
  part_begin "C: API cập nhật trên bản A, gọi như giao diện gọi"
  local before after ck jid t0

  section "C1: GET /api/v1/self"
  api "$C1" GET /api/v1/self
  ok "GET /self 200: version A, has_old true, busy false" bash -c '[ "$1" = 200 ]' _ "$CODE"
  ok "version là A, has_old true, busy false" bq '.version == $a and .has_old == true and .busy == false'
  ok "chưa có check (chưa kiểm lần nào), chưa có last (chưa lượt nào chạy từ panel)" bq '(has("check") | not) and (has("last") | not)'

  section "C2: Kiểm bản mới với nguồn tải mặc định (lần duy nhất ra mạng thật)"
  api "$C1" POST '/api/v1/self/update?dry_run=1'
  ok "POST /self/update?dry_run=1: 200" code_is 200
  ok "check.url là nguồn tải mặc định ($DEFAULT_URL)" bq --arg u "$DEFAULT_URL" '.check.url == $u and .url == $u'
  if bq '.check | has("error")'; then
    note "máy ảo không hỏi được nguồn tải mặc định (không ra mạng được hoặc GitHub không trả lời); lý do nguyên văn: $(jb '.check.error.vi' | mask_str)"
    ok "không ra mạng được: check.error có lời giải thích, vẫn có current là A" bq '(.check.error.vi | length > 0) and .check.current == $a'
  else
    ok "không có check.error" bq '.check | has("error") | not'
    if bq --arg s "$SUM_OLD" '.check.sha256 == $s'; then
      ok "check.sha256 bằng đúng dòng tpan-linux-$ARCH trong SHA256SUMS của gói $OLD_VER (bản mới nhất trên GitHub)" bq --arg s "$SUM_OLD" '.check.sha256 == $s'
    else
      note "bản mới nhất trên GitHub không còn là $OLD_VER: sha256 khác dòng của gói $OLD_VER, chỉ khẳng định dạng"
      ok "check.sha256 là 64 ký tự hex" bq '.check.sha256 | test("^[0-9a-f]{64}$")'
    fi
    # Từ v0.2.1 bản phát hành có file VERSION (M15), nên latest là tên bản
    # (v0.2.0 thì rỗng). Khẳng định đúng cho cả hai, phần còn lại như cũ.
    ok "check.latest rỗng (bản cũ chưa có file VERSION) hoặc là tên bản phát hành, up_to_date false, asset tpan-linux-$ARCH" bq '(.check.latest == "" or (.check.latest | test("^v[0-9]+\\.[0-9]+\\.[0-9]+$"))) and .check.up_to_date == false and .check.asset == ("tpan-linux-" + $arch)'
  fi

  section "C3: Kiểm bản mới với nguồn tải loopback (bản B)"
  set_url "$SRC/b"
  api "$C1" POST '/api/v1/self/update?dry_run=1'
  ok "dry_run=1: 200, check.latest là B ($VER_B), check.current là A" bq '.check.latest == $b and .check.current == $a'
  ok "check.sha256 khớp SHA256SUMS của b" bq --arg s "$SUM_B" '.check.sha256 == $s'
  ok "up_to_date false, check.asset là tpan-linux-$ARCH, check.url là $SRC/b, không có check.error" bq '.check.up_to_date == false and .check.asset == ("tpan-linux-" + $arch) and .check.url == ($src + "/b") and (.check | has("error") | not)'
  api "$C1" GET /api/v1/self
  ck="$(jb '.check.checked_at')"
  ok "GET /self sau đó còn nhớ check (latest B, có checked_at)" bq '.check.latest == $b and (.check.checked_at | length > 0)'
  sleep 1
  api "$C1" POST '/api/v1/self/update?dry_run=1&auto=1'
  ok "dry_run=1&auto=1: trả cùng checked_at (không kiểm lại)" bq --arg ck "$ck" '.check.checked_at == $ck and .check.latest == $b'
  api "$C1" POST '/api/v1/self/update?dry_run=1'
  ok "dry_run=1 không auto (người bấm nút): kiểm lại, checked_at mới" bq --arg ck "$ck" '.check.checked_at != $ck and .check.latest == $b'
  state
  ok "kiểm bản mới không tải file chạy, không đổi gì: file chạy vẫn là A, không có tpan.new, chưa có update.self_last" bash -c '[ "$1" = "$2" ] && [ "$3" = "(không có)" ] && [ "$4" = "(không có)" ]' _ "$ST_SHA" "$SUM_A" "$ST_NEW" "$ST_LAST"

  section "C4: phân quyền và CSRF với handler thật"
  json_secret u '.password' "$VM" root tpan user add "$UU" --full-name "Khach M15" --email khach@example.com --yes --json
  ok "user add $UU: thoát 0, mật khẩu sinh một lần (đã che)" bash -c '[ "$1" = 0 ] && [ -n "$2" ]' _ "$RC" "$JS_PW"
  local JU JN
  JU="$(new_jar user-thuong)"
  JN="$(new_jar khong-cookie)"
  login "$JU" "$UU" "$TMPD/pw.u"
  ok "user thường $UU đăng nhập: 204" code_is 204
  state
  before="$ST_SHA|$ST_OLD|$ST_NEW|$ST_PID|$ST_UNIT|$ST_URL|$ST_LAST"
  db audit
  local audit0=$OUT
  api "$JU" GET /api/v1/self
  ok "user thường: GET /self 403 forbidden" expect 403 forbidden
  api "$JU" POST '/api/v1/self/update?dry_run=1'
  ok "user thường: POST /self/update?dry_run=1 403 forbidden" expect 403 forbidden
  api "$JU" POST /api/v1/self/update
  ok "user thường: POST /self/update 403 forbidden" expect 403 forbidden
  api "$JU" POST '/api/v1/self/rollback?dry_run=1'
  ok "user thường: POST /self/rollback?dry_run=1 403 forbidden" expect 403 forbidden
  api "$JU" POST /api/v1/self/rollback
  ok "user thường: POST /self/rollback 403 forbidden" expect 403 forbidden
  api "$JN" GET /api/v1/self
  ok "không cookie: GET /self 401 unauthorized" expect 401 unauthorized
  api "$JN" POST '/api/v1/self/update?dry_run=1'
  ok "không cookie: POST /self/update?dry_run=1 401 unauthorized" expect 401 unauthorized
  api "$JN" POST /api/v1/self/update
  ok "không cookie: POST /self/update 401 unauthorized" expect 401 unauthorized
  api "$JN" POST /api/v1/self/rollback
  ok "không cookie: POST /self/rollback 401 unauthorized" expect 401 unauthorized
  API_SFS=cross-site API_ORIGIN=https://evil.example.com
  api "$C1" POST /api/v1/self/update
  ok "admin, Sec-Fetch-Site: cross-site: POST /self/update bị chặn 403 csrf" expect 403 csrf
  api "$C1" POST /api/v1/self/rollback
  ok "admin, Sec-Fetch-Site: cross-site: POST /self/rollback bị chặn 403 csrf" expect 403 csrf
  API_SFS=-
  api "$C1" POST /api/v1/self/update
  ok "admin, không Sec-Fetch-Site, Origin lạ: POST /self/update bị chặn 403 csrf" expect 403 csrf
  API_SFS=same-origin API_ORIGIN=""
  state
  after="$ST_SHA|$ST_OLD|$ST_NEW|$ST_PID|$ST_UNIT|$ST_URL|$ST_LAST"
  ok "sau cả loạt: file chạy, tpan.old, MainPID, update.self_url, update.self_last không đổi" eq "$before" "$after"
  ok "đơn vị tạm $UNIT không chạy" eq "$ST_UNIT" inactive
  db audit
  ok "bảng audit không có thêm dòng self.* nào (không lượt nào được khởi)" eq "$audit0" "$OUT"

  section "C5: một lượt cùng lúc (giữ khoá toàn cục để lượt cập nhật phải chờ)"
  PID_A=$ST_PID
  t0=$ST_NOW
  lock_hold
  api "$C1" POST /api/v1/self/update '{"url":"http://evil.example.com/x","version":"v9"}'
  ok "POST /self/update (kèm thân JSON lạ): 202" code_is 202
  ok "run.status running, run.from là A, run.actor admin, run.action update" bq '.run.status == "running" and .run.from == $a and .run.actor == "admin" and .run.action == "update"'
  api "$C1" GET /api/v1/self
  ok "ngay sau đó GET /self: busy true, last.status running" bq '.busy == true and .last.status == "running" and .last.action == "update"'
  api "$C1" POST /api/v1/self/update
  ok "POST /self/update lần hai: 409 self_update_running" expect 409 self_update_running
  api "$C1" POST /api/v1/self/rollback
  ok "POST /self/rollback trong lúc đó: 409 self_update_running" expect 409 self_update_running
  vexec systemctl is-active "$UNIT"
  ok "systemctl is-active $UNIT in active" out_is active
  vexec systemctl show "$UNIT" -p ExecStart -p Description -p ControlGroup
  ok "dòng lệnh của đơn vị tạm đúng là /usr/local/bin/tpan internal self-update" hasF 'argv[]=/usr/local/bin/tpan internal self-update ;'
  ok "đơn vị tạm nằm ở cgroup riêng (/system.slice/$UNIT.service)" hasF "ControlGroup=/system.slice/$UNIT.service"
  ok "dòng lệnh và mô tả không chứa evil.example.com, tên user hay IP nào" bash -c '! grep -qE "evil|example\.com|admin|v9|<mac>|<vm>|[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+" <<<"$1"' _ "$OUT"
  state
  ok "lượt chạy đang chờ khoá: file chạy vẫn là A, MainPID chưa đổi" bash -c '[ "$1" = "$2" ] && [ "$3" = "$4" ]' _ "$ST_SHA" "$SUM_A" "$ST_PID" "$PID_A"

  section "C6: nhả khoá, A lên B qua đơn vị tạm, panel restart"
  poll_bg "$C1" "$VER_B" 60
  lock_release
  poll_wait "$VER_B"
  ok "GET /session trả version B ($VER_B) trong vòng 60 giây" poll_ok
  state cho 60
  ok "đơn vị tạm đã chạy xong ($ST_WAIT)" unit_done
  note "gián đoạn của lần restart này: tpan.service không active $ST_GAP ms (ActiveExit -> ActiveEnter của systemd); từ lúc systemd bắt đầu dừng tới lúc tiến trình mới mở cổng nghe $ST_GAP2 ms (journal); nhìn từ trình duyệt: $POLL_FAILS lần hỏi hỏng, khoảng $POLL_GAP ms"
  ok "MainPID của tpan.service đổi ($PID_A -> $ST_PID), dịch vụ active" bash -c '[ -n "$2" ] && [ "$1" != "$2" ] && [ "$3" = active ]' _ "$PID_A" "$ST_PID" "$ST_ACT"
  ok "tpan version là B" eq "$ST_VER" "$VER_B"
  ok "file chạy là file của b, tpan.old là file của A (sha256)" bash -c '[ "$1" = "$2" ] && [ "$3" = "$4" ]' _ "$ST_SHA" "$SUM_B" "$ST_OLD" "$SUM_A"
  ok "systemctl is-active $UNIT in inactive (đơn vị đã tự dọn)" eq "$ST_UNIT" inactive
  ok "setting update.self_url vẫn là $SRC/b (thân request không đổi được nguồn tải)" eq "$ST_URL" "$SRC/b"
  api "$C1" GET /api/v1/session
  ok "cookie C1 vẫn logged_in, version là B" bq '.logged_in == true and .user.name == "admin" and .version == $b'
  page
  ok "$SITE vẫn 200" is200
  api "$C1" GET /api/v1/self
  jid="$(jb '.last.job_id // empty')"
  ok "GET /self: last.action update, status ok, from A, to B" bq '.last.action == "update" and .last.status == "ok" and .last.from == $a and .last.to == $b'
  ok "last.actor admin, last.ip không phải cli, có job_id, có finished" bq '.last.actor == "admin" and .last.ip != "cli" and (.last.ip | length > 0) and (.last.job_id | length > 0) and (.last.finished | length > 0)'
  ok "last.ip là IP của máy đã bấm (ở đây là Mac, đã che trong log)" bq --arg ip "$HOST_IP" '.last.ip == $ip'
  ok "busy false; version B; has_old true; check vắng (panel vừa restart nên quên lần kiểm)" bq '.busy == false and .version == $b and .has_old == true and (has("check") | not)'
  db audit
  ok "audit: dòng self.update kết quả started, actor admin, target là A" hasF "|admin|self.update|$VER|started|"
  ok "audit: dòng self.update target \"A -> B\" kết quả ok, actor admin, cùng job_id với last" bash -c '[ -n "$2" ] && grep -qF -- "|admin|self.update|$3 -> $4|ok|$2" <<<"$1"' _ "$OUT" "$jid" "$VER" "$VER_B"
  ok "audit: hai lần bấm bị từ chối trong lúc đang chạy có dòng self_update_running (update và rollback)" bash -c 'grep -qF -- "|admin|self.update|$2|self_update_running|" <<<"$1" && grep -qF -- "|admin|self.rollback|$2|self_update_running|" <<<"$1"' _ "$OUT" "$VER"
  db jobs
  ok "jobs: job self.update succeeded, actor admin, đúng job_id của last" bash -c '[ -n "$2" ] && grep -qF -- "job|$2|self.update|succeeded|admin|" <<<"$1"' _ "$OUT" "$jid"
  journal_unit "$t0"
  ok "journal của đơn vị tạm có tiến độ từng bước (tải, thay file, khởi động lại) và dòng A -> B" bash -c 'grep -qF "Tải bản tpan mới" <<<"$1" && grep -qF "Thay /usr/local/bin/tpan bằng" <<<"$1" && grep -qF "Khởi động lại tpan" <<<"$1" && grep -qF -- "$2 -> $3" <<<"$1"' _ "$OUT" "$VER" "$VER_B"
  ok "journal không chứa evil.example.com" hasntF evil.example.com

  part_end C
}

# ================================================================ PHẦN C7 (chạy sau D)

# Chạy sau phần D, không phải ngay sau C6: lượt "same" để lại file tpan.new
# cùng bản (thiết kế của M12: lần self-update sau tự gỡ nó trước khi tải), mà
# phần D cần khẳng định "sai sha256 thì không còn tpan.new". Bước tải lỗi trả
# tpan.new về như trước lượt chạy, nên muốn thấy "không còn" thì trước đó phải
# không có.
part_c7() {
  part_begin "C7: đã là bản mới nhất (chạy sau D, xem chú thích trong script)"
  local pid0 new0
  set_url "$SRC/b"
  state
  pid0=$ST_PID
  api "$C1" POST '/api/v1/self/update?dry_run=1'
  ok "dry_run=1: up_to_date true (latest B trùng bản đang chạy, sha256 trùng file đang cài)" bq --arg s "$SUM_B" '.check.up_to_date == true and .check.latest == $b and .check.current == $b and .check.sha256 == $s'
  api "$C1" POST /api/v1/self/update
  ok "POST /self/update lần nữa: 202" code_is 202
  state cho 60
  ok "đơn vị tạm đã chạy xong ($ST_WAIT)" unit_done
  ok "MainPID không đổi (không restart), file chạy vẫn là b" bash -c '[ "$1" = "$2" ] && [ "$3" = "$4" ]' _ "$ST_PID" "$pid0" "$ST_SHA" "$SUM_B"
  api "$C1" GET /api/v1/self
  ok "last.status là same, from B, to B" bq '.last.status == "same" and .last.action == "update" and .last.from == $b and .last.to == $b and .busy == false'
  db audit
  ok "audit: dòng self.update \"B -> B\" kết quả same" hasF "|admin|self.update|$VER_B -> $VER_B|same|"
  ok "lượt same để lại tpan.new cùng bản (file của b, đã kiểm sha256); tpan.old vẫn là A" bash -c '[ "$1" = "$2" ] && [ "$3" = "$4" ]' _ "$ST_NEW" "$SUM_B" "$ST_OLD" "$SUM_A"

  section "C7: nguồn tải sai sha256 lần nữa, lúc đang có tpan.new còn sót của lượt same"
  new0=$ST_NEW
  set_url "$SRC/sai"
  api "$C1" POST /api/v1/self/update
  ok "POST /self/update: 202" code_is 202
  state cho 60
  ok "đơn vị tạm đã chạy xong ($ST_WAIT)" unit_done
  api "$C1" GET /api/v1/self
  ok "last.status failed, lý do có chữ sha256" bq '.last.status == "failed" and (.last.error | test("sha256"))'
  ok "bước tải lỗi trả máy về như trước lượt chạy: tpan.new còn sót vẫn nguyên (sha256 như trước), file chạy và MainPID không đổi" bash -c '[ "$1" = "$2" ] && [ "$3" = "$4" ] && [ "$5" = "$6" ]' _ "$ST_NEW" "$new0" "$ST_SHA" "$SUM_B" "$ST_PID" "$pid0"
  note "vì vậy \"sai sha256 thì không còn tpan.new\" (phần D) chỉ đúng khi trước lượt chạy không có tpan.new; có sẵn thì nó được trả lại, file vừa tải vẫn bị bỏ"
  part_end C7
}

# ================================================================ PHẦN D

part_d() {
  part_begin "D: nguồn tải có SHA256SUMS sai, rồi nguồn tải chết"
  local pid0 err t0
  state
  pid0=$ST_PID
  t0=$ST_NOW
  ok "trước lượt chạy: đang ở B, không có tpan.new (lượt A lên B đã dùng nó làm file chạy)" bash -c '[ "$1" = "$2" ] && [ "$3" = "(không có)" ]' _ "$ST_VER" "$VER_B" "$ST_NEW"
  set_url "$SRC/sai"
  api "$C1" POST '/api/v1/self/update?dry_run=1'
  ok "dry_run=1: check.sha256 toàn số 0, up_to_date false" bq --arg z "$ZEROS" '.check.sha256 == $z and .check.up_to_date == false'
  api "$C1" POST /api/v1/self/update
  ok "POST /self/update: 202" code_is 202
  state cho 60
  ok "đơn vị tạm đã chạy xong ($ST_WAIT)" unit_done
  ok "sha256 của /usr/local/bin/tpan không đổi (vẫn là b), tpan version vẫn B" bash -c '[ "$1" = "$2" ] && [ "$3" = "$4" ]' _ "$ST_SHA" "$SUM_B" "$ST_VER" "$VER_B"
  ok "MainPID không đổi (panel không restart), dịch vụ active" bash -c '[ "$1" = "$2" ] && [ "$3" = active ]' _ "$ST_PID" "$pid0" "$ST_ACT"
  ok "không còn /usr/local/bin/tpan.new (file tải về sai sha256 đã bị bỏ); tpan.old vẫn là A" bash -c '[ "$1" = "(không có)" ] && [ "$2" = "$3" ]' _ "$ST_NEW" "$ST_OLD" "$SUM_A"
  api "$C1" GET /api/v1/self
  err="$(jb '.last.error // empty')"
  printf '    last.error nguyên văn: %s\n' "$(mask_str <<<"$err")"
  ok "GET /self: last.status failed, from B" bq '.last.status == "failed" and .last.action == "update" and .last.from == $b and .busy == false'
  ok "last.error có chữ sha256 và cả hai số (số của file tải về và số ghi trong SHA256SUMS)" bash -c 'grep -qF sha256 <<<"$1" && grep -qF -- "$2" <<<"$1" && grep -qF -- "$3" <<<"$1"' _ "$err" "$SUM_B" "$ZEROS"
  db audit
  ok "audit: dòng self.update kết quả rolled_back (job hỏng, đã hoàn tác)" has "\\|admin\\|self\\.update\\|.* -> .*\\|rolled_back\\|[0-9a-f]+"
  journal_unit "$t0"
  ok "journal của đơn vị tạm nói rõ lý do (sha256)" hasF sha256
  api "$C1" GET /api/v1/session
  ok "panel vẫn trả 200, C1 vẫn đăng nhập, version B" bash -c '[ "$1" = 200 ]' _ "$CODE"
  ok "C1 logged_in, version B" bq '.logged_in == true and .version == $b'

  section "D: nguồn tải chết (cổng loopback không có gì nghe)"
  set_url "http://127.0.0.1:$DEAD_PORT/khong-co"
  api "$C1" POST '/api/v1/self/update?dry_run=1'
  ok "dry_run=1 vẫn 200, có check.error" bash -c '[ "$1" = 200 ]' _ "$CODE"
  printf '    check.error nguyên văn: %s\n' "$(jb '.check.error.vi // empty' | mask_str)"
  ok "check.error có lời giải thích; check.current là B; version, has_old vẫn có" bq '(.check.error.vi | length > 0) and (.check.error.en | length > 0) and .check.current == $b and .version == $b and .has_old == true'
  api "$C1" POST /api/v1/self/update
  ok "POST /self/update: 202" code_is 202
  state cho 90
  ok "đơn vị tạm đã chạy xong ($ST_WAIT)" unit_done
  api "$C1" GET /api/v1/self
  printf '    last.error nguyên văn: %s\n' "$(jb '.last.error // empty' | mask_str)"
  ok "last.status failed kèm lý do" bq '.last.status == "failed" and (.last.error | length > 0) and .busy == false'
  ok "file chạy và MainPID không đổi, không có tpan.new" bash -c '[ "$1" = "$2" ] && [ "$3" = "$4" ] && [ "$5" = "(không có)" ]' _ "$ST_SHA" "$SUM_B" "$ST_PID" "$pid0" "$ST_NEW"
  part_end D
}

# ================================================================ PHẦN E

part_e() {
  part_begin "E: quay về bản trước (B về A) bằng POST /api/v1/self/rollback"
  local pid0 jid
  set_url "$SRC/b"
  state
  pid0=$ST_PID
  api "$C1" POST '/api/v1/self/rollback?dry_run=1'
  ok "POST /self/rollback?dry_run=1: 200 {current: B, old: A}" bash -c '[ "$1" = 200 ]' _ "$CODE"
  ok "current là B, old là A" bq '.current == $b and .old == $a'
  poll_bg "$C1" "$VER" 60
  api "$C1" POST /api/v1/self/rollback
  ok "POST /self/rollback: 202, run.action rollback, run.status running, run.from B" bash -c '[ "$1" = 202 ]' _ "$CODE"
  ok "run.action rollback, status running, from B, actor admin" bq '.run.action == "rollback" and .run.status == "running" and .run.from == $b and .run.actor == "admin"'
  poll_wait "$VER"
  ok "GET /session trả version A trong vòng 60 giây" poll_ok
  state cho 60
  ok "đơn vị tạm đã chạy xong ($ST_WAIT)" unit_done
  note "gián đoạn của lần restart này: tpan.service không active $ST_GAP ms; từ lúc systemd bắt đầu dừng tới lúc tiến trình mới mở cổng nghe $ST_GAP2 ms (journal); nhìn từ trình duyệt: $POLL_FAILS lần hỏi hỏng, khoảng $POLL_GAP ms"
  ok "tpan version là A, file chạy là file của A (sha256)" bash -c '[ "$1" = "$2" ] && [ "$3" = "$4" ]' _ "$ST_VER" "$VER" "$ST_SHA" "$SUM_A"
  ok "MainPID đổi ($pid0 -> $ST_PID), dịch vụ active" bash -c '[ -n "$2" ] && [ "$1" != "$2" ] && [ "$3" = active ]' _ "$pid0" "$ST_PID" "$ST_ACT"
  ok "không còn tpan.old; bản vừa bị thay ra (B) nằm ở tpan.new" bash -c '[ "$1" = "(không có)" ] && [ "$2" = "$3" ]' _ "$ST_OLD" "$ST_NEW" "$SUM_B"
  api "$C1" GET /api/v1/session
  ok "C1 vẫn logged_in, version A" bq '.logged_in == true and .user.name == "admin" and .version == $a'
  api "$C1" GET /api/v1/self
  jid="$(jb '.last.job_id // empty')"
  ok "last.action rollback, status ok, from B, to A, actor admin" bq '.last.action == "rollback" and .last.status == "ok" and .last.from == $b and .last.to == $a and .last.actor == "admin" and .last.ip != "cli"'
  ok "has_old false, busy false" bq '.has_old == false and .busy == false'
  db audit
  ok "audit: dòng self.rollback started (target B) và dòng \"B -> A\" kết quả ok cùng job_id" bash -c '[ -n "$2" ] && grep -qF -- "|admin|self.rollback|$4|started|" <<<"$1" && grep -qF -- "|admin|self.rollback|$4 -> $3|ok|$2" <<<"$1"' _ "$OUT" "$jid" "$VER" "$VER_B"
  page
  ok "$SITE vẫn 200" is200
  api "$C1" POST /api/v1/self/rollback
  ok "POST /self/rollback lần nữa: 409 self_no_old" expect 409 self_no_old
  api "$C1" POST '/api/v1/self/rollback?dry_run=1'
  ok "POST /self/rollback?dry_run=1 khi không còn tpan.old: 409 self_no_old" expect 409 self_no_old
  part_end E
}

# ================================================================ PHẦN F

part_f() {
  part_begin "F: lõi từ chối quay về khi file cũ không mở được DB (đặt file $OLDER_VER làm tpan.old)"
  local before after msg
  vroot install -o root -g root -m 0755 "$E/tpan-cu" /usr/local/bin/tpan.old
  ok "(đồ nghề) đặt file chạy $OLDER_VER vào /usr/local/bin/tpan.old" rc_is 0
  vroot /usr/local/bin/tpan.old version
  ok "(đồ nghề) tpan.old là $OLDER_VER" has "^tpan $OLDER_VER "
  state
  before="$ST_SHA|$ST_OLD|$ST_NEW|$ST_PID|$ST_UNIT|$ST_VER|$ST_LAST"
  API_MAX=3000
  api "$C1" POST '/api/v1/self/rollback?dry_run=1'
  ok "POST /self/rollback?dry_run=1: 409 self_rollback_db" expect 409 self_rollback_db
  msg="$(jb '.error.message.vi // .error.message // empty')"
  printf '    lời từ chối nguyên văn: %s\n' "$(mask_str <<<"$msg")"
  ok "lời nhắn có câu của chính file cũ (\"chỉ biết tới bản\")" bash -c 'grep -qF "chỉ biết tới bản" <<<"$1"' _ "$msg"
  api "$C1" POST /api/v1/self/rollback
  ok "POST /self/rollback: 409 self_rollback_db" expect 409 self_rollback_db
  API_MAX=1600
  db audit
  ok "audit: dòng self.rollback kết quả self_rollback_db (actor admin, target là bản đang chạy A)" hasF "|admin|self.rollback|$VER|self_rollback_db|"
  vroot tpan self-update --rollback --yes
  ok "CLI tpan self-update --rollback --yes: thoát khác 0" rc_not 0
  ok "CLI in lời từ chối có câu của file cũ" hasF "chỉ biết tới bản"
  vroot tpan self-update --rollback --dry-run
  ok "CLI --rollback --dry-run cũng từ chối (thoát khác 0)" rc_not 0
  state
  after="$ST_SHA|$ST_OLD|$ST_NEW|$ST_PID|$ST_UNIT|$ST_VER|$ST_LAST"
  ok "file chạy, tpan.old, MainPID, phiên bản, update.self_last không đổi" eq "$before" "$after"
  ok "đơn vị tạm không chạy; tpan.service active" bash -c '[ "$1" = inactive ] && [ "$2" = active ]' _ "$ST_UNIT" "$ST_ACT"
  vroot rm -f /usr/local/bin/tpan.old
  ok "(đồ nghề) gỡ file $OLDER_VER khỏi /usr/local/bin/tpan.old" rc_is 0
  part_end F
}

# ================================================================ PHẦN G

part_g() {
  part_begin "G: lượt chạy bị giết giữa chừng"
  local pid0 sha0
  state
  pid0=$ST_PID
  sha0=$ST_SHA
  ok "trước khi thử: đang ở bản A" eq "$ST_VER" "$VER"
  lock_hold
  api "$C1" POST /api/v1/self/update
  ok "POST /self/update: 202" code_is 202
  vexec systemctl is-active "$UNIT"
  ok "đơn vị tạm đang chạy (chờ khoá)" out_is active
  # --kill-whom=main: chỉ gửi KILL cho tiến trình chính (đơn vị chỉ có một
  # tiến trình). Không có cờ này systemctl gửi thêm cho "tiến trình phụ" của
  # đơn vị; tiến trình chính vừa chết thì đơn vị được dọn ngay, và có lần
  # systemctl in "Failed to send signal SIGKILL to auxiliary processes:
  # Invalid argument", thoát mã 1 dù đơn vị đã bị giết (gặp 1 trong 2 lần chạy).
  vroot systemctl kill --kill-whom=main -s KILL "$UNIT"
  ok "systemctl kill --kill-whom=main -s KILL $UNIT thoát 0 (KILL dọn được đơn vị, không cần systemctl stop)" rc_is 0
  sleep 2
  vexec systemctl is-active "$UNIT"
  ok "sau KILL đơn vị tạm không còn chạy (--collect dọn cả đơn vị bị giết)" bash -c '[ "$1" != active ] && [ "$1" != activating ] && [ "$1" != deactivating ]' _ "$OUT"
  lock_release
  api "$C1" GET /api/v1/self
  ok "ngay sau khi bị giết (chưa quá 15 giây): busy false, last.status còn running" bq '.busy == false and .last.status == "running"'
  printf '\n[%s] (chờ 16 giây: quá hạn selfInterruptAfter 15 giây)\n' "$(ts)"
  sleep 16
  api "$C1" GET /api/v1/self
  ok "sau 16 giây: busy false, last.status là interrupted" bq '.busy == false and .last.status == "interrupted" and .last.action == "update"'
  state
  ok "file chạy không đổi, MainPID không đổi, panel vẫn chạy" bash -c '[ "$1" = "$2" ] && [ "$3" = "$4" ] && [ "$5" = active ]' _ "$ST_SHA" "$sha0" "$ST_PID" "$pid0" "$ST_ACT"
  ok "settings vẫn giữ dấu gốc (status running): interrupted chỉ có trong câu trả lời của API" bash -c 'grep -qF "\"status\":\"running\"" <<<"$1"' _ "$ST_LAST"

  section "G: lượt bình thường sau đó vẫn chạy được (A lên B, kết thúc ok)"
  poll_bg "$C1" "$VER_B" 60
  api "$C1" POST /api/v1/self/update
  ok "POST /self/update: 202 (không bị đơn vị cũ chặn)" code_is 202
  poll_wait "$VER_B"
  ok "GET /session trả version B trong vòng 60 giây" poll_ok
  state cho 60
  ok "đơn vị tạm đã chạy xong ($ST_WAIT)" unit_done
  note "gián đoạn của lần restart này: tpan.service không active $ST_GAP ms; từ lúc systemd bắt đầu dừng tới lúc tiến trình mới mở cổng nghe $ST_GAP2 ms (journal); nhìn từ trình duyệt: $POLL_FAILS lần hỏi hỏng, khoảng $POLL_GAP ms"
  api "$C1" GET /api/v1/self
  ok "last.status ok (lúc bấm đang ở A, nguồn tải là B): from A, to B" bq '.last.status == "ok" and .last.action == "update" and .last.from == $a and .last.to == $b and .busy == false'
  ok "tpan version là B, tpan.old là A, MainPID đổi" bash -c '[ "$1" = "$2" ] && [ "$3" = "$4" ] && [ "$5" != "$6" ]' _ "$ST_VER" "$VER_B" "$ST_OLD" "$SUM_A" "$ST_PID" "$pid0"
  part_end G
}

# ================================================================ PHẦN H

part_h() {
  part_begin "H: CLI không đổi hành vi"
  local jid
  vroot tpan self-update --dry-run --url "$SRC/b"
  ok "self-update --dry-run thoát 0, in kế hoạch" bash -c '[ "$1" = 0 ] && grep -q "^Kế hoạch: Cập nhật tpan" <<<"$2"' _ "$RC" "$OUT"
  ok "kế hoạch có ba bước: tải, thay file, khởi động lại" bash -c 'grep -qE "^ 1\. .*Tải bản tpan mới" <<<"$1" && grep -qE "^ 2\. .*Thay /usr/local/bin/tpan bằng" <<<"$1" && grep -qE "^ 3\. .*Khởi động lại tpan" <<<"$1" && ! grep -qE "^ 4\. " <<<"$1"' _ "$OUT"
  vexec tpan --help
  ok "tpan --help thoát 0, có self-update, KHÔNG hiện lệnh internal (lệnh ẩn)" bash -c '[ "$1" = 0 ] && grep -qE "^ +self-update " <<<"$2" && ! grep -qE "^ +internal( |$)" <<<"$2"' _ "$RC" "$OUT"
  vexec tpan internal --help
  ok "tpan internal --help vẫn chạy, có lệnh con self-update" bash -c '[ "$1" = 0 ] && grep -qE "^ +self-update " <<<"$2"' _ "$RC" "$OUT"

  section "H: CLI self-update rồi --rollback như M12"
  # Đang ở B (sau phần G). Về A trước để lần self-update dưới đây có thứ mà lên.
  vroot tpan self-update --rollback --yes
  ok "self-update --rollback --yes (B về A): thoát 0, job thành công" job_ok
  wait_panel
  vroot tpan self-update --yes --url "$SRC/b"
  ok "self-update --yes --url .../b: thoát 0, job thành công" job_ok
  ok "in A -> B" hasF "$VER -> $VER_B"
  jid="$(sed -nE 's/^Job ([0-9a-f]+): thành công$/\1/p' <<<"$OUT" | head -1)"
  wait_panel
  state
  ok "tpan version là B, tpan.old là A, dịch vụ active" bash -c '[ "$1" = "$2" ] && [ "$3" = "$4" ] && [ "$5" = active ]' _ "$ST_VER" "$VER_B" "$ST_OLD" "$SUM_A" "$ST_ACT"
  db audit
  ok "audit: lượt CLI ghi ip cli, action self.update, target là gốc tải, kết quả ok" bash -c '[ -n "$2" ] && grep -qF -- "|cli||self.update|$3/b|ok|$2" <<<"$1"' _ "$OUT" "$jid" "$SRC"
  vroot tpan self-update --rollback --yes
  ok "self-update --rollback --yes: thoát 0, job thành công" job_ok
  ok "in B -> A" hasF "$VER_B -> $VER"
  jid="$(sed -nE 's/^Job ([0-9a-f]+): thành công$/\1/p' <<<"$OUT" | head -1)"
  wait_panel
  state
  ok "tpan version là A, file chạy là file của A, dịch vụ active" bash -c '[ "$1" = "$2" ] && [ "$3" = "$4" ] && [ "$5" = active ]' _ "$ST_VER" "$VER" "$ST_SHA" "$SUM_A" "$ST_ACT"
  db audit
  ok "audit: lượt CLI ghi ip cli, action self.rollback, kết quả ok" bash -c '[ -n "$2" ] && grep -qE -- "\|cli\|\|self\.rollback\|[^|]*\|ok\|$2\$" <<<"$1"' _ "$OUT" "$jid"
  api "$C1" GET /api/v1/self
  ok "lượt CLI không ghi update.self_last: last vẫn là lượt cuối chạy từ panel (update, ok, A -> B)" bq '.last.action == "update" and .last.status == "ok" and .last.from == $a and .last.to == $b and .version == $a'
  api "$C1" GET /api/v1/session
  ok "sau cả kịch bản cookie C1 vẫn logged_in, version A" bq '.logged_in == true and .version == $a'
  part_end H
}

# ================================================================ CUỐI

part_end_all() {
  part_begin "CUỐI: health, bảng audit và jobs của cả kịch bản, tắt đồ nghề"
  local h2="$TMPD/health.2" moi
  health_errs "$h2"
  moi="$(new_errs "$H0" "$h2")"
  [ -n "$moi" ] && printf '[lỗi] mới so với đầu kịch bản:\n%s\n' "$moi"
  ok "cuối kịch bản tpan health không có [lỗi] mới so với lúc mới cài $OLD_VER" test -z "$moi"
  page
  ok "$SITE vẫn 200" is200
  db audit
  db jobs
  vroot systemctl stop "$HTTP_UNIT"
  ok "(đồ nghề) tắt máy chủ tải $HTTP_UNIT" rc_is 0
  vexec systemctl is-active "$HTTP_UNIT" "$LOCK_UNIT" "$UNIT"
  ok "không đơn vị tạm nào còn chạy (nguồn tải, khoá, cập nhật)" bash -c '! grep -qx active <<<"$1"' _ "$OUT"
  part_end CUỐI
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

printf 'e2e M15. Bản cũ: %s; bản A (của cây): %s; bản B: %s; máy ảo: %s\n' "$OLD_VER" "$VER" "$VER_B" "$VMNAME"
C1=""
part_0
part_a
part_b
part_c
part_d
part_c7
part_e
part_f
part_g
part_h
part_end_all

leak_check
section "TỔNG KẾT: $PASSES khẳng định đạt, $FAILS khẳng định không đạt"
[ "$FAILS" -eq 0 ]
