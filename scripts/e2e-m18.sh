#!/usr/bin/env bash
# Lệnh trong máy ảo viết trong nháy đơn, biến của chúng mở ra trong máy ảo:
# shellcheck disable=SC2016
# e2e của mốc M18 (sao lưu ra kho S3, khôi phục từ kho, cài bằng cloud-init;
# bảng "E2E bước 21" ở mục 7 của docs/thay-doi/M18-thiet-ke.md) trên máy ảo
# (tên trong file .tpan-vm; không có thì tpan-test). Chạy trên Mac, trong thư
# mục repo, một hoặc nhiều nhóm theo thứ tự gõ:
#
#   scripts/e2e-m18.sh chinh      A B C D E F G H I K: từ máy sạch, stack nginx, ba site
#   scripts/e2e-m18.sh ci         L: trên máy ảo chính (cần tpan của nhánh đã ở trong đó,
#                                 thường là máy của "chinh") cộng máy ảo phụ <tên>-ci
#   scripts/e2e-m18.sh nang-cap   N: nâng từ v0.2.0, từ máy sạch
#   scripts/e2e-m18.sh all        = chinh ci nang-cap
#
# Gọi qua make (log vào docs/bang-chung/M18.log): make e2e-m18 P="chinh"
#
# Trạng thái mỗi phần cần (không có phần M: lệnh dựng lại máy đã cắt):
#   A  reset máy ảo, cài tpan của cây, setup, stack nginx, user thường khach_a,
#      ba site (C thuộc khach_a), versitygw, đặt đích.
#   B..K  mỗi phần dùng máy VÀ biến của các phần trước trong CÙNG lần chạy
#      (mật khẩu admin, id bản sao lưu): chỉ chạy được trong nhóm "chinh".
#      B: đẩy site A. C: kho tắt rồi đẩy bù (A). D: proxy gây lỗi, site B có
#      file 30 MiB. E: object hỏng (bản chỉ còn trên kho của A, từ B và C).
#      F: khôi phục từ kho (A). G: giữ bản (site C, chưa có lịch sử nào).
#      H: quét bí mật. I: Garage (A). K: phân quyền (site C của khach_a).
#   L  máy ảo chính có tpan của cây (kiểm bằng tpan version); tạo, thử rồi
#      XOÁ máy phụ <tên>-ci (trap ở EXIT xoá cả khi lỗi giữa chừng).
#   N  reset máy ảo; cần dist/release/v0.2.0 (tpan-linux-*, SHA256SUMS).
#
# ĐỒ NGHỀ TEST, CHỈ TRONG MÁY ẢO (thư mục /root/e2e-m18):
#   - versitygw v1.11.0 (.deb tải từ trang phát hành, kiểm sha256 theo
#     checksums.txt), dịch vụ versitygw@tpan ở 127.0.0.1:7070; Garage v2.5.0,
#     dịch vụ garage-thu ở 127.0.0.1:3900. Khoá của hai kho sinh tại chỗ, nằm
#     ở /root/s3thu.env và /root/garage.env (0600). Khoá chỉ đi qua stdin của
#     `tpan backup target set --keys-stdin`, đọc thẳng trong máy ảo; script
#     đọc chúng vào danh sách che ngay khi chúng được sinh (không in).
#   - faultproxy (scripts/e2e-m18/faultproxy, build cho kiến trúc của máy ảo):
#     proxy trả 503 từ phần thứ 3 của lần tải nhiều phần, unit tpan-e2e-proxy.
#   - mcpclient của e2e M7 (build cho máy ảo), gọi `tpan mcp` ngay trong máy.
#   - jq, age (apt); các script nhỏ trong scripts/e2e-m18/.
#   - phần L: thư mục /srv/e2e-m18-tai phục vụ bằng python3 -m http.server
#     (unit tpan-e2e-http, cổng 8099) cho máy phụ tải file chạy.
#
# File chạy v0.2.0: thư mục OLD_DIR (mặc định dist/release/v0.2.0, dist/ không
# vào git) có tpan-linux-<arch> và SHA256SUMS của bản phát hành.
#
# Đặt E2E_LOG=<file log> để cuối lần chạy script tự tìm trong chính file log
# đó mọi bí mật đã gặp (khoá S3, khoá sao lưu, mật khẩu), IP máy ảo, IP của
# Mac, thư mục nhà và tên user Mac (phải là 0 lần).
#
# LỚP BỌC LỆNH MÁY ẢO (vm_cap, chép từ e2e-m14.sh): mọi lệnh `scripts/vm.sh
# exec|root ...` (và `scripts/vm.sh on <máy phụ> exec|root ...`) in trong log
# được chạy bằng `multipass exec` qua một lớp bọc: output của lệnh ghi vào
# file TRONG máy ảo, ngắn thì in về trong cùng lần gọi, dài thì lấy về bằng
# `multipass transfer`. Lý do: `multipass exec` trên Mac có lúc treo giữa chừng
# khi output dài (tiến trình trong máy ảo đã xong, output về Mac bị cắt giữa
# dòng, lệnh trên Mac không bao giờ trả về). Lệnh, output và mã thoát in trong
# log là của chính lệnh thật.
#
# Output: mỗi lệnh có dấu thời gian, output nguyên văn (đã che) và mã thoát;
# mỗi khẳng định in "=> ĐẠT" hoặc "=> KHÔNG ĐẠT"; cuối mỗi phần và cuối lần
# chạy in số đạt, không đạt. Script đọc JSON (--json, jq) thay cho câu chữ
# tiếng Việt khi có thể.
# Không dùng set -e: nhiều lệnh ở đây được chờ là phải lỗi.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2

VM=scripts/vm.sh
VMNAME="${TPAN_VM:-$(tr -d '[:space:]' 2>/dev/null <.tpan-vm)}"
VMNAME="${VMNAME:-tpan-test}"
CI="$VMNAME-ci"
TOOLS=scripts/e2e-m18
RT=/root/e2e-m18 # đồ nghề trong máy ảo
FAILS=0
PASSES=0
OUT=""
RC=0
CODE=""
EMAIL=admin@example.com
A=a.example.com
B=b.example.com
C=c.example.com # của user thường khach_a
UA=khach_a
PREFIX=tpan/e2e-m18 # tiền tố cố định: đường dẫn object trên đĩa versitygw biết trước
VGW_ENV=/root/s3thu.env
GAR_ENV=/root/garage.env
VGW_OBJ=/var/lib/s3thu/tpan-thu # bucket của versitygw: mỗi object là <đây>/<tiền tố>/<key>
SRV_DIR=/srv/e2e-m18-tai
HTTP_PORT=8099
OLD_VER=v0.2.0
OLD_DIR="${OLD_DIR:-dist/release/$OLD_VER}"
VER="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

mkdir -p tmp-phien
TMPD="$(mktemp -d "$PWD/tmp-phien/e2e-m18.XXXXXX")"
chmod 700 "$TMPD"
SECRETS="$TMPD/secrets"
: >"$SECRETS"
chmod 600 "$SECRETS"
HJ="$TMPD/health.json"

CI_MADE=0   # 1: đã gọi tạo máy phụ, phải xoá
HTTP_UP=0   # 1: unit tpan-e2e-http đang chạy trên máy ảo chính
cleanup() {
  if [ "$CI_MADE" = 1 ]; then
    printf '\n[%s] (dọn) xoá máy ảo phụ %s\n' "$(ts)" "$CI"
    "$VM" delete "$CI" </dev/null >/dev/null 2>&1
    CI_MADE=0
  fi
  if [ "$HTTP_UP" = 1 ]; then
    multipass exec "$VMNAME" -- sudo bash -c 'systemctl stop tpan-e2e-http 2>/dev/null; rm -rf "$1"' _ "$SRV_DIR" </dev/null >/dev/null 2>&1
    HTTP_UP=0
  fi
}
trap 'cleanup; rm -rf "$TMPD"' EXIT
trap 'exit 130' INT TERM

ts() { date '+%Y-%m-%d %H:%M:%S'; }
section() { printf '\n\n######## [%s] %s\n' "$(ts)" "$*"; }

VM_IP=""
HOST_IP=""
NET4=""
VM_V6=""
V6PFX=""
CI_IP=""
VM_ARCH=""

# mask_str che một chuỗi (stdin) theo mọi thứ đã biết, in ra stdout.
mask_str() {
  local s a p
  s="$(cat)"
  [ -n "$VM_IP" ] && s="${s//"$VM_IP"/<vm>}"
  [ -n "$CI_IP" ] && s="${s//"$CI_IP"/<vm-ci>}"
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
# Lệnh `"$VM" exec|root ...` và `"$VM" on <máy phụ> exec|root ...` đi qua lớp
# bọc vm_cap; lệnh khác chạy thẳng, stdin là /dev/null (multipass exec chờ
# stdin mãi khi không có terminal).
runq() {
  RUNQ_CMD="$(mask_str <<<"$*")"
  RUNQ_TS="$(ts)"
  if [ "$1" = "$VM" ]; then
    if [ "${2:-}" = exec ] || [ "${2:-}" = root ]; then
      shift
      CAP_VM="" vm_cap /dev/null "$@"
      return
    fi
    if [ "${2:-}" = on ] && { [ "${4:-}" = exec ] || [ "${4:-}" = root ]; }; then
      local target=$3
      shift 3
      CAP_VM="$target" vm_cap /dev/null "$@"
      return
    fi
  fi
  "$@" </dev/null >"$TMPD/run.out" 2>&1
  RC=$?
}

# Lớp bọc chạy trong máy ảo (xem đầu file). CAP_MERGED gộp stdout và stderr
# theo đúng thứ tự vào một file; CAP_SPLIT giữ riêng hai luồng (lệnh --json).
# File nằm ở nhà của user ubuntu, quyền 0600, lần chạy sau ghi đè lần trước.
# Không đổi umask của chính lệnh (sudo gộp umask của người gọi vào umask của
# lệnh root, tpan sẽ tạo file với quyền khác đi): chỉ tạo sẵn file 0600.
# CAP_VM: máy ảo đích (rỗng: máy ảo chính).
CAP_MAX=4000
CAP_MERGED='f=$HOME/.e2e-cap; (umask 077; : >"$f"); "$@" >"$f" 2>&1; rc=$?; n=$(wc -c <"$f"); [ "$n" -le '"$CAP_MAX"' ] && cat "$f"; printf "\n__E2E_CAP rc=%s n=%s e=0\n" "$rc" "$n"'
CAP_SPLIT='f=$HOME/.e2e-cap; (umask 077; : >"$f"; : >"$f.err"); "$@" >"$f" 2>"$f.err"; rc=$?; printf "__E2E_CAP rc=%s n=%s e=%s\n" "$rc" "$(wc -c <"$f")" "$(wc -c <"$f.err")"'
capvm() { printf '%s' "${CAP_VM:-$VMNAME}"; }
cap_fetch() { # cap_fetch <file trong máy ảo> <file trên Mac>: đọc lại được nên thử tối đa 3 lần
  local i
  for i in 1 2 3; do
    multipass transfer "$(capvm):$1" "$2" </dev/null >/dev/null 2>&1 && return 0
    sleep 1
  done
  return 1
}
# vm_cap <file stdin> <exec|root> <lệnh...>: output gộp ở $TMPD/run.out, mã thoát ở RC.
vm_cap() {
  local in=$1 how=$2 line n
  shift 2
  [ "$how" = root ] && set -- sudo "$@"
  multipass exec "$(capvm)" -- bash -c "$CAP_MERGED" _ "$@" <"$in" >"$TMPD/cap.raw" 2>&1
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
  multipass exec "$(capvm)" -- bash -c "$CAP_SPLIT" _ "$@" </dev/null >"$TMPD/cap.raw" 2>&1
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
count_is() { [ "$(grep -cE -- "$2" <<<"$OUT")" -eq "$1" ]; }
job_ok() { [ "$RC" = 0 ] && grep -q '^Job .*: thành công' <<<"$OUT"; }
# refused <mã>: lệnh --json thoát khác 0 với .error.code là mã đó.
refused() { [ "$RC" != 0 ] && [ "$(jq -r '.error.code // empty' "$TMPD/run.json" 2>/dev/null)" = "$1" ]; }
# jraw <biểu thức jq>: jq -e trên stdout THÔ (chưa che) của lệnh --json vừa chạy.
jraw() { jq -e "$@" "$TMPD/run.json" >/dev/null 2>&1; }
# t_ok <biểu thức jq>: thoát 0 và JSON thoả biểu thức.
t_ok() { [ "$RC" = 0 ] && jraw "$@"; }
# no_raw_secret: output thô của lệnh vừa chạy không chứa bí mật nào đã biết.
no_raw_secret() { ! grep -qF -f "$SECRETS" "$TMPD/run.out"; }

vexec() { run "$VM" exec "$@"; }
vroot() { run "$VM" root "$@"; }
vci() { run "$VM" on "$CI" root "$@"; }

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

# harvest_env <file env trong máy ảo> [biến]: đọc hai khoá của một kho thử vào
# danh sách che, KHÔNG in. Có tên biến thì giữ access key ở biến đó (để kiểm
# "chỉ hiện bốn ký tự cuối").
harvest_env() {
  local l first=""
  runq "$VM" root bash -c '. "$1" && printf "%s\n%s\n" "$S3_ACCESS_KEY" "$S3_SECRET_KEY"' _ "$1"
  while IFS= read -r l; do
    [[ "$l" =~ ^[^[:space:]]{8,}$ ]] || continue
    add_secret "$l"
    [ -z "$first" ] && first=$l
  done < <(tr -d '\r' <"$TMPD/run.out")
  [ -n "${2:-}" ] && printf -v "$2" '%s' "$first"
  printf '\n[%s] (đọc hai khoá trong %s vào danh sách che; không in giá trị; mã thoát %s)\n' "$RUNQ_TS" "$1" "$RC"
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
  VM_ARCH="$("$VM" exec dpkg --print-architecture </dev/null | tr -d '\r')"
  printf 'IP máy ảo: <vm> (IPv6: <vm-v6>); IP của Mac trong mạng Multipass: <mac> (đều được che trong log); kiến trúc: %s\n' "$VM_ARCH"
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

# push_tools: chép scripts/e2e-m18/*.sh, *.py vào $RT trong máy ảo chính.
# multipass transfer ghi bằng user ubuntu, nên chép vào /tmp rồi install bằng root.
push_tools() {
  local f n=0
  for f in "$TOOLS"/*.sh "$TOOLS"/*.py; do
    "$VM" push "$f" "/tmp/e2e-m18-${f##*/}" </dev/null >/dev/null 2>&1 && n=$((n + 1))
  done
  vroot bash -c 'install -d -m 0700 "$1" && for f in /tmp/e2e-m18-*; do install -m 0700 "$f" "$1/${f#/tmp/e2e-m18-}"; rm -f "$f"; done; ls "$1"' _ "$RT"
  ok "(đồ nghề) $n script của $TOOLS đã chép vào $RT" bash -c '[ "$2" -gt 0 ] && [ "$(grep -cE "\.(sh|py)$" <<<"$1")" -eq "$2" ]' _ "$OUT" "$n"
}
# push_bin <file trên Mac> <tên trong $RT>
push_bin() {
  run "$VM" push "$1" "/tmp/e2e-m18-bin-$2"
  vroot bash -c 'install -m 0700 "/tmp/e2e-m18-bin-$2" "$1/$2" && rm -f "/tmp/e2e-m18-bin-$2" && ls -l "$1/$2"' _ "$RT" "$2"
}

# ---------------------------------------------------------------- health (như e2e-m14)

health_json() {
  printf '\n[%s] $ %s root tpan health --json\n' "$(ts)" "$VM"
  CAP_VM="" vm_cap2 root tpan health --json
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
check_status() { hq --arg id "$1" --arg st "$2" '[.groups[].results[] | select(.id == $id and .status == $st)] | length == 1'; }
health_ids() { jq -r '[.groups[].results[].id] | unique | .[]' "$HJ" 2>/dev/null; }

# ---------------------------------------------------------------- CLI

# site_add_q <domain> [cờ...]: tpan site add, nhặt mật khẩu quản trị WordPress
# và mật khẩu DB của site vào danh sách che trước khi in.
site_add_q() {
  local d=$1 pw
  shift
  runq "$VM" root tpan site add "$d" --email "$EMAIL" --yes "$@"
  pw="$(tr -d '\r' <"$TMPD/run.out" | sed -nE 's/^  Mật khẩu: +([A-Za-z0-9]{32})$/\1/p' | head -1)"
  add_secret "$pw"
  add_secret "$(dbpw "$d")"
  show_last
}
site_add() {
  site_add_q "$@"
  ok "site add $*: thoát 0, job thành công" job_ok
}

# json_secret <tên> <biểu thức jq> "$VM" root|exec <lệnh...>: chạy lệnh
# --json trong máy ảo chính, đọc bí mật theo biểu thức jq vào $TMPD/pw.<tên>
# rồi mới in. stdout thô ở $TMPD/run.json, stderr thô ở $TMPD/run.err.
JS_PW=""
json_secret() {
  local name=$1 expr=$2 pw
  shift 2
  RUNQ_CMD="$(mask_str <<<"$*")"
  RUNQ_TS="$(ts)"
  shift # bỏ "$VM"
  CAP_VM="" vm_cap2 "$@"
  tr -d '\r' <"$TMPD/cap.out" >"$TMPD/run.json"
  tr -d '\r' <"$TMPD/cap.err" >"$TMPD/run.err"
  pw="$(jq -r "$expr // empty" "$TMPD/run.json" 2>/dev/null)"
  [ -n "$pw" ] && save_pw "$name" "$pw"
  cat "$TMPD/run.err" "$TMPD/run.json" >"$TMPD/run.out"
  show_last
  JS_PW="$pw"
}

# vjson [--ci] <lệnh ... --json> (bằng root): stdout THÔ ở $TMPD/run.json,
# stderr ở $TMPD/run.err; in cả hai (đã che).
vjson() {
  local target="" label="$VM root"
  if [ "${1:-}" = --ci ]; then
    target=$CI
    label="$VM on $CI root"
    shift
  fi
  RUNQ_CMD="$(mask_str <<<"$label $*")"
  RUNQ_TS="$(ts)"
  CAP_VM="$target" vm_cap2 root "$@"
  tr -d '\r' <"$TMPD/cap.out" >"$TMPD/run.json"
  tr -d '\r' <"$TMPD/cap.err" >"$TMPD/run.err"
  cat "$TMPD/run.err" "$TMPD/run.json" >"$TMPD/run.out"
  show_last
}
err_note() { note "stderr của lệnh: $(mask_str <"$TMPD/run.err" | grep . | tail -3 | tr '\n' ' ')"; }

# target_set <file env> <endpoint thay thế hoặc ""> [cờ thêm...]: đặt đích. Hai
# khoá đọc từ file env NGAY TRONG máy ảo và chỉ đi qua stdin của tpan. Khoá
# sao lưu (nếu vừa sinh) vào danh sách che trước khi in; giữ ở BK, số lần nó
# xuất hiện trong output thô ở BK_HITS.
BK=""
BK_HITS=0
target_set() {
  local envf=$1 ep=$2
  shift 2
  json_secret bk '.backup_key' "$VM" root bash -c '. "$1" && ep="${2:-$S3_ENDPOINT}" && shift 2 && printf "%s\n%s\n" "$S3_ACCESS_KEY" "$S3_SECRET_KEY" | tpan backup target set --endpoint "$ep" --region "$S3_REGION" --bucket "$S3_BUCKET" "$@" --keys-stdin --yes --json' _ "$envf" "$ep" "$@"
  if [ -n "$JS_PW" ]; then
    BK="$JS_PW"
    BK_HITS="$(grep -oF -- "$JS_PW" "$TMPD/run.out" | wc -l | tr -d ' ')"
  fi
}
target_ok() { t_ok '.report.job.status == "succeeded" and .report.job.kind == "backup.target.set"'; }
probe_ran() { jraw '[.report.steps[] | select(.id | startswith("s3.probe:")) | .outcome] == ["applied"]'; }

# bk_list <domain>: tpan backup list <domain> --json, giữ ở $TMPD/list.json.
bk_list() {
  vjson tpan backup list "$1" --json
  cp "$TMPD/run.json" "$TMPD/list.json"
}
bkq() { jq -e "$@" "$TMPD/list.json" >/dev/null 2>&1; }
bkr() { jq -r "$@" "$TMPD/list.json" 2>/dev/null; }
newest_full() { bkr '[.[] | select(.kind == "full")][0].id // empty'; }
# field <id> <trường>
field() { bkr --arg i "$1" --arg f "$2" '[.[] | select(.id == $i)][0][$f] // empty'; }

# obj_sha <remote_key> <sha256 mong đợi> <nhãn>: object là file thường trên đĩa
# versitygw; sha256sum của nó phải bằng cột sha256 của dòng backups.
obj_sha() {
  vroot bash -c 'f="$1/$2/$3"; ls -l "$f" && sha256sum "$f"' _ "$VGW_OBJ" "$PREFIX" "$1"
  ok "$3: object $1 có trên đĩa của versitygw, sha256sum bằng backups.sha256" bash -c '[ -n "$2" ] && grep -q "^$2 " <<<"$1"' _ "$OUT" "$2"
}

jobs_py() { vroot python3 "$RT/jobs.py" "$@"; }
job_mark() { "$VM" root python3 "$RT/jobs.py" mark </dev/null | tr -d '\r' | sed -nE 's/^moc ([0-9]+)$/\1/p'; }
# two_jobs: output của "jobs.py since" có đúng backup.pull rồi backup.restore, cả hai thành công.
two_jobs() { [ "$(grep -E '^job ' <<<"$OUT" | awk '$4 == "backup.pull" || $4 == "backup.restore" {printf "%s %s;", $4, $3}')" = "backup.pull succeeded;backup.restore succeeded;" ]; }

site_view() { vroot bash "$RT/site.sh" xem "$1"; }

# render_run <sites.yaml> <base> <SHA256SUMS> <file ra> <tên>: tpan cloud-init
# render trong máy ảo chính. Mật khẩu quản trị là dòng stderr duy nhất
# "<nhãn>: <32 ký tự>"; nhặt vào danh sách che ($TMPD/pw.render-<tên>) trước
# khi in. Kết quả: RC, RPW, RPW_HITS (số lần mật khẩu có trong output thô),
# RERR_LINES (số dòng stderr).
RPW=""
RPW_HITS=0
RERR_LINES=0
render_run() {
  RUNQ_CMD="$(mask_str <<<"$VM root tpan cloud-init render --web nginx --sites $1 --base $2 --sums $3 --out $4")"
  RUNQ_TS="$(ts)"
  CAP_VM="" vm_cap2 root tpan cloud-init render --web nginx --sites "$1" --base "$2" --sums "$3" --out "$4"
  tr -d '\r' <"$TMPD/cap.out" >"$TMPD/run.json"
  tr -d '\r' <"$TMPD/cap.err" >"$TMPD/run.err"
  cat "$TMPD/run.err" "$TMPD/run.json" >"$TMPD/run.out"
  RPW="$(sed -nE 's/^.*: ([A-Za-z0-9]{32})$/\1/p' "$TMPD/run.err" | tail -1)"
  RPW_HITS=0
  if [ -n "$RPW" ]; then
    save_pw "render-$5" "$RPW"
    RPW_HITS="$(grep -oF -- "$RPW" "$TMPD/run.out" | wc -l | tr -d ' ')"
  fi
  RERR_LINES="$(grep -c . "$TMPD/run.err")"
  show_last
  printf 'Mật khẩu quản trị trong output: %s lần; số dòng stderr: %s\n' "$RPW_HITS" "$RERR_LINES"
}

# scan_vm <file stdin> <đường dẫn trong máy ảo...>: quet.sh, in kết quả.
scan_vm() {
  local in=$1
  shift
  RUNQ_CMD="$VM root bash $RT/quet.sh $* < $(mask_str <<<"$in")"
  RUNQ_TS="$(ts)"
  CAP_VM="" vm_cap "$in" root bash "$RT/quet.sh" "$@"
  show_last
}
scan_zero() { has '^tong=0$' && hasnt '^dem=[1-9]'; }

# ---------------------------------------------------------------- API (trong máy ảo)

# api <phiên> <METHOD> <đường dẫn> [thân JSON]: gọi panel ngay trong máy ảo
# chính (scripts/e2e-m18/api.sh, curl -k tới https://127.0.0.1:8443). Thân đi
# qua stdin; mã HTTP ở CODE, thân trả về ở $TMPD/body. In thân đã che.
api() {
  local jar=$1 m=$2 p=$3 body=${4-}
  (umask 077 && printf '%s' "$body" >"$TMPD/req.json")
  printf '\n[%s] $ curl -k -X %s https://127.0.0.1:8443%s   (trong máy ảo, phiên %s)\n' "$(ts)" "$m" "$p" "$jar"
  [ -n "$body" ] && printf '    thân gửi: %s\n' "$(mask_str <<<"$body")"
  CAP_VM="" vm_cap "$TMPD/req.json" root bash "$RT/api.sh" "$jar" "$m" "$p"
  CODE="$(tr -d '\r' <"$TMPD/run.out" | sed -nE '1s/^__HTTP ([0-9]{3})$/\1/p')"
  tr -d '\r' <"$TMPD/run.out" | sed '1d' >"$TMPD/body"
  harvest
  printf -- '-> HTTP %s\n' "${CODE:-?}"
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
  while IFS= read -r s; do add_secret "$s"; done < <(jq -r '[.. | objects | (.password?, .db_password?, .admin_password?) | strings] | .[]' "$TMPD/body" 2>/dev/null)
}
code_is() { [ "$CODE" = "$1" ]; }
ecode() { jq -r '.error.code // empty' "$TMPD/body" 2>/dev/null; }
expect() { [ "$CODE" = "$1" ] && [ "$(ecode)" = "$2" ]; }
bq() { jq -e "$@" "$TMPD/body" >/dev/null 2>&1; }
# code_bq <mã HTTP> <đối số jq...>: đúng mã và thân thoả biểu thức.
code_bq() {
  local c=$1
  shift
  [ "$CODE" = "$c" ] && bq "$@"
}
jb() { jq -r "$1" "$TMPD/body" 2>/dev/null; }

# login <phiên> <tên> <file mật khẩu>: POST /api/v1/login; thân không in.
login() {
  local body
  body="$(jq -nc --arg n "$2" --rawfile p "$3" '{name: $n, password: $p}')"
  (umask 077 && printf '%s' "$body" >"$TMPD/req.json")
  printf '\n[%s] $ curl -k -X POST https://127.0.0.1:8443/api/v1/login   (trong máy ảo, phiên %s; thân {name: %s, password: <đã che>})\n' "$(ts)" "$1" "$2"
  CAP_VM="" vm_cap "$TMPD/req.json" root bash "$RT/api.sh" "$1" POST /api/v1/login
  CODE="$(tr -d '\r' <"$TMPD/run.out" | sed -nE '1s/^__HTTP ([0-9]{3})$/\1/p')"
  printf -- '-> HTTP %s\n' "${CODE:-?}"
}

# wait_job <phiên> <id>: hỏi GET /jobs/{id} mỗi giây tới khi xong (tối đa 600 lần).
JOBST=""
wait_job() {
  local jar=$1 id=$2 i
  JOBST=""
  for i in $(seq 1 600); do
    CAP_VM="" vm_cap /dev/null root bash "$RT/api.sh" "$jar" GET "/api/v1/jobs/$id"
    JOBST="$(tr -d '\r' <"$TMPD/run.out" | sed '1d' | jq -r '.job.status // empty' 2>/dev/null)"
    [ -n "$JOBST" ] && [ "$JOBST" != running ] && break
    sleep 1
  done
  OUT="$(tr -d '\r' <"$TMPD/run.out" | sed '1d' | jq -c '.job | {kind, target, status, actor, owner, error}' 2>&1)"
  mask
  printf '(chờ job %s: %s lần hỏi)\n%s\n' "$id" "$i" "$OUT"
}

# ================================================================ PHẦN A

part_a() {
  part_begin "A: đặt đích (máy sạch, stack nginx, ba site, versitygw)"
  reset_vm
  deploy_new
  push_tools
  vroot bash -c 'export DEBIAN_FRONTEND=noninteractive; apt-get -o DPkg::Lock::Timeout=120 update -q >/dev/null && apt-get -o DPkg::Lock::Timeout=120 install -y -q jq age >/dev/null; command -v jq age; dpkg-query -W -f "\${Package} \${Version}\n" jq age'
  ok "(đồ nghề) jq và age của apt đã cài" bash -c 'grep -qx "/usr/bin/jq" <<<"$1" && grep -qx "/usr/bin/age" <<<"$1"' _ "$OUT"

  section "A: setup, stack, user thường, ba site"
  json_secret admin '.admin_password' "$VM" root tpan setup --yes --json
  ok "tpan setup: thoát 0, job thành công" t_ok '.report.job.status == "succeeded"'
  ok "mật khẩu quản trị panel in một lần (đã che)" bash -c '[ -n "$1" ] && [ "$(grep -oF -- "$1" "$2" | wc -l)" -eq 1 ]' _ "$JS_PW" "$TMPD/run.out"
  vroot tpan stack install --web nginx --yes
  ok "stack install --web nginx: job thành công" job_ok
  json_secret a '.password' "$VM" root tpan user add "$UA" --full-name "Khach A" --email a@example.com --yes --json
  ok "user add $UA: thoát 0, mật khẩu sinh một lần (đã che)" bash -c '[ "$1" = 0 ] && [ -n "$2" ]' _ "$RC" "$JS_PW"
  site_add "$A"
  site_add "$B"
  site_add "$C" --owner "$UA"
  for d in "$A" "$B" "$C"; do
    site_view "$d"
    ok "site $d trả 200" has '^trang-chu=200 '
  done

  section "A: (đồ nghề) versitygw trong máy ảo, khoá sinh tại chỗ"
  vroot bash "$RT/versitygw.sh"
  ok "versitygw: .deb khớp sha256 của checksums.txt, dịch vụ versitygw@tpan chạy" bash -c '[ "$2" = 0 ] && grep -qE "versitygw_1\.11\.0_linux_[a-z0-9]+\.deb: OK$" <<<"$1" && grep -qx "versitygw@tpan: active" <<<"$1"' _ "$OUT" "$RC"
  AK_VGW=""
  harvest_env "$VGW_ENV" AK_VGW
  ok "(đồ nghề) đọc được hai khoá của versitygw vào danh sách che" test "${#AK_VGW}" -ge 12

  section "A: target set với khoá bí mật sai"
  vjson bash -c '. "$1" && printf "%s\n%s\n" "$S3_ACCESS_KEY" "$(openssl rand -hex 20)" | tpan backup target set --endpoint "$S3_ENDPOINT" --region "$S3_REGION" --bucket "$S3_BUCKET" --prefix "$2" --keys-stdin --yes --json' _ "$VGW_ENV" "$PREFIX"
  ok "sai khoá bí mật: từ chối s3_auth_failed" refused s3_auth_failed
  vroot bash -c 'for f in /etc/tpan/s3.json /etc/tpan/s3-ca.pem /etc/tpan/backup.key; do if [ -e "$f" ]; then echo "co $f"; else echo "khong $f"; fi; done; python3 "$1/jobs.py" count backup.target.set; python3 "$1/jobs.py" settings' _ "$RT"
  ok "không file nào được ghi (s3.json, s3-ca.pem, backup.key), không có job backup.target.set, không có setting s3.endpoint" bash -c '[ "$(grep -c "^khong " <<<"$1")" -eq 3 ] && grep -qx "so-job backup.target.set 0" <<<"$1" && ! grep -q "^set s3.endpoint=." <<<"$1"' _ "$OUT"

  section "A: target set với khoá đúng"
  target_set "$VGW_ENV" "" --prefix "$PREFIX"
  ok "đúng khoá: thoát 0, job backup.target.set thành công" target_ok
  ok "job có bước thử ghi/đọc/xoá (s3.probe:...) và bước đó đã chạy" probe_ran
  ok "khoá sao lưu đúng dạng tpan-key-1-<52 ký tự base32>" bash -c '[[ "$1" =~ ^tpan-key-1-[a-z2-7]{52}$ ]]' _ "$BK"
  ok "khoá sao lưu in đúng một lần trong output" test "$BK_HITS" -eq 1
  ok "kết quả có phần nối lại (relinked, other_target)" jraw '.result | has("relinked") and has("other_target")'
  FP="$(printf '%s' "$BK" | shasum -a 256 | cut -c1-8)"
  ok "key_fingerprint trong JSON là 8 hex đầu của sha256 khoá sao lưu ($FP)" jraw --arg fp "$FP" '.key_fingerprint == $fp'
  vroot stat -c '%U:%G %a %n' /etc/tpan/s3.json /etc/tpan/backup.key
  ok "s3.json và backup.key là root:root 600" bash -c 'grep -qx "root:root 600 /etc/tpan/s3.json" <<<"$1" && grep -qx "root:root 600 /etc/tpan/backup.key" <<<"$1"' _ "$OUT"
  vroot tpan backup target show
  ok "target show (chữ): có bốn ký tự cuối của access key và dấu vân tay $FP" bash -c 'grep -qF -- "$2" "$1" && grep -qF -- "$3" "$1"' _ "$TMPD/run.out" "${AK_VGW: -4}" "$FP"
  ok "target show (chữ): không có access key, khoá bí mật hay khoá sao lưu đầy đủ" no_raw_secret
  ok "target show (chữ): chỉ cách xem lại khoá (key show --reveal)" hasF 'key show --reveal'
  vjson tpan backup target show --json
  ok "target show --json: configured, đúng endpoint, bucket, tiền tố, bốn ký tự cuối, có khoá sao lưu, đúng dấu vân tay" jraw --arg p "$PREFIX" --arg t "${AK_VGW: -4}" --arg fp "$FP" \
    '.configured == true and .endpoint == "http://127.0.0.1:7070" and .bucket == "tpan-thu" and .prefix == $p and .access_key_tail == $t and .has_backup_key == true and .key_fingerprint == $fp'
  ok "target show --json: không có bí mật nào" no_raw_secret
  vjson tpan backup key show --json
  ok "key show --json (không --reveal): has_key, đúng dấu vân tay, không có trường key" jraw --arg fp "$FP" '.has_key == true and .key_fingerprint == $fp and (has("key") | not)'
  part_end A
}

# ================================================================ PHẦN B

part_b() {
  part_begin "B: đẩy lên kho (site $A)"
  vroot bash "$RT/site.sh" moc "$A" moc-B
  vjson tpan backup create "$A" --yes --json
  ok "backup create $A: thoát 0, job backup.create thành công" t_ok '.job.status == "succeeded"'
  err_note
  bk_list "$A"
  ID_B="$(newest_full)"
  ok "bản mới nhất $ID_B: local=true, remote=stored, remote_key là sites/$A/<tên file>" bkq --arg i "$ID_B" --arg d "$A" '[.[] | select(.id == $i)][0] | .local == true and .remote == "stored" and (.remote_key | startswith("sites/" + $d + "/"))'
  obj_sha "$(field "$ID_B" remote_key)" "$(field "$ID_B" sha256)" "bản $ID_B"

  section "B: job backup.push có bằng chứng (tpan.db, bảng job_events)"
  jobs_py last backup.push "$A"
  ok "job backup.push mới nhất của $A thành công" has '^job [0-9a-f]+ succeeded backup\.push '
  ok "bước s3.push:$ID_B chạy sha256sum file cục bộ (sự kiện command)" has "^ev [0-9]+ command s3\\.push:$ID_B .*sha256sum"
  ok "bước s3.push:$ID_B có dòng kết quả ghi lại (sự kiện output: tiến độ, đọc lại)" has "^ev [0-9]+ output s3\\.push:$ID_B "
  ok "bước s3.push:$ID_B xong (step.done)" has "^ev [0-9]+ step\\.done s3\\.push:$ID_B"
  note "dòng output của bước đẩy (đọc lại khớp sha256 $(field "$ID_B" sha256 | cut -c1-12)...): $(grep -E "^ev [0-9]+ output s3\\.push:$ID_B " <<<"$OUT" | cut -c1-200 | tr '\n' ';')"
  note "phép kiểm s3.object:$ID_B trong sự kiện: $(grep -cF "s3.object:$ID_B" <<<"$OUT") dòng"

  section "B: gói panel trên kho, mở bằng lệnh age của apt"
  vroot bash -c 'ls -l "$1/$2/panel/" 2>&1' _ "$VGW_OBJ" "$PREFIX"
  ok "có gói panel tpan-<giờ UTC>.tar.gz.age trên kho" has 'tpan-[0-9]{8}-[0-9]{6}\.tar\.gz\.age$'
  PANEL="$(sed -nE 's/.*(tpan-[0-9]{8}-[0-9]{6}\.tar\.gz\.age)$/\1/p' <<<"$OUT" | sort | tail -1)"
  vjson tpan backup target show --json
  ok "target show: panel_last là gói mới nhất trên kho ($PANEL)" jraw --arg n "$PANEL" '.panel_last.name == $n'
  vroot bash "$RT/mo-goi-panel.sh" "$VGW_OBJ/$PREFIX/panel/$PANEL" "$RT/goi-b" "$ID_B"
  ok "age -d mở được gói bằng khoá sao lưu (không qua tpan)" has '^age-ma=0 '
  ok "trong gói có manifest.json và tpan.db" bash -c 'grep -q "^tep-trong-goi: .*manifest\.json" <<<"$1" && grep -q "^tep-trong-goi: .*tpan\.db" <<<"$1"' _ "$OUT"
  ok "manifest format 1, có danh sách migration" has '^manifest: .*"format":1.*"so_migration":[1-9]'
  ok "tpan.db trong gói: dòng $ID_B của $A ở trạng thái stored" has "^dong $ID_B local=1 remote=stored$"
  part_end B
}

# ================================================================ PHẦN C

part_c() {
  part_begin "C: kho hỏng (versitygw tắt), rồi đẩy bù"
  vroot bash "$RT/site.sh" moc "$A" moc-C
  vroot bash -c 'systemctl stop versitygw@tpan; systemctl is-active versitygw@tpan'
  ok "(đồ nghề) versitygw đã tắt" has '^inactive$'
  vjson tpan backup create "$A" --yes --json
  ok "kho tắt: backup create $A vẫn thoát 0, job backup.create thành công (B1)" t_ok '.job.status == "succeeded"'
  err_note
  bk_list "$A"
  ID_C="$(newest_full)"
  ok "dòng mới $ID_C (khác $ID_B): local=true, remote khác stored" bkq --arg i "$ID_C" --arg b "$ID_B" '$i != $b and ([.[] | select(.id == $i)][0] | .local == true and .remote != "stored")'
  note "dòng $ID_C: remote='$(field "$ID_C" remote)', remote_error='$(field "$ID_C" remote_error | cut -c1-160)'"
  health_json
  ok "tpan.s3:fresh là cảnh báo" check_status tpan.s3:fresh warn
  ok "tpan.s3:target là lỗi" check_status tpan.s3:target fail
  jq -r '.groups[].results[] | select(.id | startswith("tpan.s3:")) | "\(.status) \(.id): \(.summary.vi)\n    sửa tay: \((.manual_fix // []) | join(" ; ") | .[:300])"' "$HJ" | mask_str
  printf '\n'

  section "C: bật lại kho, tpan backup push"
  vroot bash -c 'systemctl start versitygw@tpan; for i in $(seq 1 30); do ss -tln | grep -q "127.0.0.1:7070 " && break; sleep 1; done; systemctl is-active versitygw@tpan'
  ok "(đồ nghề) versitygw chạy lại" has '^active$'
  vjson tpan backup push --json
  ok "backup push: thoát 0, lượt có chạy, không bản nào lỗi" t_ok '.ran == true and ((.failed // []) | length == 0)'
  bk_list "$A"
  ok "cả hai bản $ID_B và $ID_C đều stored (đẩy bù đủ)" bkq --arg b "$ID_B" --arg c "$ID_C" '[.[] | select(.id == $b or .id == $c) | .remote] == ["stored", "stored"]'
  obj_sha "$(field "$ID_C" remote_key)" "$(field "$ID_C" sha256)" "bản $ID_C"
  health_json
  ok "sau khi đẩy bù: tpan.s3:target và tpan.s3:fresh đạt" bash -c 'jq -e "([.groups[].results[] | select(.id == \"tpan.s3:target\" or .id == \"tpan.s3:fresh\") | .status] | sort) == [\"pass\", \"pass\"]" "$1" >/dev/null' _ "$HJ"
  part_end C
}

# ================================================================ PHẦN D

part_d() {
  part_begin "D: lỗi giữa lúc tải nhiều phần (proxy trả 503 từ phần 3)"
  vroot bash "$RT/site.sh" to "$B" 30
  ok "(đồ nghề) $B có file ngẫu nhiên 30 MiB" has ' 31457280 '
  run env CGO_ENABLED=0 GOOS=linux GOARCH="$VM_ARCH" go build -C "$TOOLS/faultproxy" -o "$TMPD/faultproxy" .
  ok "(đồ nghề) build faultproxy cho linux/$VM_ARCH" rc_is 0
  push_bin "$TMPD/faultproxy" faultproxy
  vroot bash -c 'systemctl stop tpan-e2e-proxy 2>/dev/null; systemctl reset-failed tpan-e2e-proxy 2>/dev/null; systemd-run --quiet --unit=tpan-e2e-proxy "$1/faultproxy" -listen 127.0.0.1:7071 -backend http://127.0.0.1:7070 -from 3 -status 503; for i in $(seq 1 20); do ss -tln | grep -q "127.0.0.1:7071 " && break; sleep 0.5; done; systemctl is-active tpan-e2e-proxy' _ "$RT"
  ok "(đồ nghề) proxy gây lỗi nghe 127.0.0.1:7071 (unit tpan-e2e-proxy)" has '^active$'

  target_set "$VGW_ENV" http://127.0.0.1:7071 --prefix "$PREFIX"
  ok "đích trỏ qua proxy: job backup.target.set thành công" target_ok
  ok "đổi endpoint là đổi mã đích: 2 dòng stored của $A được nối lại sang mã mới, 0 dòng ở đích khác" jraw '.result.relinked == 2 and .result.other_target == 0'
  ok "không sinh khoá sao lưu mới (máy đã có khoá)" jraw '(has("backup_key") or has("key_fingerprint")) | not'

  vjson tpan backup create "$B" --yes --json
  ok "backup create $B: thoát 0, job backup.create thành công (bản cục bộ xong dù đẩy lỗi)" t_ok '.job.status == "succeeded"'
  err_note
  bk_list "$B"
  ID_D="$(newest_full)"
  ok "bản $ID_D lớn hơn 16 MiB (tải nhiều phần, phần 8 MiB)" bkq --arg i "$ID_D" '[.[] | select(.id == $i)][0].size > 16777216'
  ok "bản $ID_D chưa lên kho (remote khác stored)" bkq --arg i "$ID_D" '[.[] | select(.id == $i)][0].remote != "stored"'
  note "dòng $ID_D: remote='$(field "$ID_D" remote)', remote_error='$(field "$ID_D" remote_error | cut -c1-200)'"
  jobs_py last backup.push "$B"
  ok "job backup.push của $B không thành công" bash -c 'grep -qE "^job [0-9a-f]+ " <<<"$1" && ! grep -qE "^job [0-9a-f]+ succeeded " <<<"$1"' _ "$OUT"
  vroot journalctl -u tpan-e2e-proxy --no-pager -o cat
  ok "proxy đã trả 503 cho phần 3 (lỗi có chủ ý, client thử lại rồi bỏ)" has '^faultproxy: PUT [^ ]+ \?partNumber=3&uploadId -> 503 \(deliberate\)$'
  ok "client đã gọi huỷ lần tải dở (DELETE ?uploadId qua proxy, kho trả 2xx)" has '^faultproxy: DELETE [^ ]+ \?uploadId -> 2[0-9][0-9]$'
  vroot bash -c 'd="$1/.sgwtmp/multipart"; if [ -d "$d" ]; then echo "phan-tai-do=$(find "$d" -type f | wc -l)"; find "$d" -type f | head -5; else echo "phan-tai-do=0 (không có thư mục $d)"; fi' _ "$VGW_OBJ"
  ok "kho không còn lần tải nhiều phần dở (thư mục multipart của versitygw không có file nào)" has '^phan-tai-do=0'

  section "D: bỏ proxy, đẩy lại"
  vroot bash -c 'systemctl stop tpan-e2e-proxy; systemctl is-active tpan-e2e-proxy'
  target_set "$VGW_ENV" "" --prefix "$PREFIX"
  ok "đích trỏ thẳng lại versitygw: job thành công, 2 dòng được nối lại" t_ok '.report.job.status == "succeeded" and .result.relinked == 2'
  vjson tpan backup push --json
  ok "backup push: thoát 0, không bản nào lỗi" t_ok '(.failed // []) | length == 0'
  bk_list "$B"
  ok "bản $ID_D giờ stored" bkq --arg i "$ID_D" '[.[] | select(.id == $i)][0].remote == "stored"'
  obj_sha "$(field "$ID_D" remote_key)" "$(field "$ID_D" sha256)" "bản $ID_D (nhiều phần)"
  part_end D
}

# ================================================================ PHẦN E

part_e() {
  part_begin "E: object hỏng trên kho, khôi phục phải dừng ở job tải về"
  vroot tpan backup settings --keep 1 --yes
  ok "backup settings --keep 1: job thành công" job_ok
  vroot bash "$RT/site.sh" moc "$A" moc-E
  vjson tpan backup create "$A" --yes --json
  ok "backup create $A: thoát 0, job thành công" t_ok '.job.status == "succeeded"'
  bk_list "$A"
  ok "giữ 1 bản trên máy: $ID_B và $ID_C chỉ còn trên kho (local=false, stored)" bkq --arg b "$ID_B" --arg c "$ID_C" '[.[] | select(.id == $b or .id == $c) | [.local, .remote]] == [[false, "stored"], [false, "stored"]]'
  KEY_C="$(field "$ID_C" remote_key)"
  PATH_C="$(field "$ID_C" path)"
  vroot bash -c 'if [ -e "$1" ]; then echo "con-file-cuc-bo"; else echo "khong-file-cuc-bo"; fi' _ "$PATH_C"
  ok "file cục bộ của $ID_C đã bị dọn" has '^khong-file-cuc-bo$'

  # tpan serve (bộ theo dõi health, bộ đẩy nền) gọi trang của site; một lần
  # gọi có thể chạy wp-cron và ghi vào DB, làm hai bản xuất DB lệch nhau dù
  # tpan không khôi phục gì. Dừng nó trong lúc so; restore là lệnh CLI, không cần serve.
  vroot bash -c 'systemctl stop tpan; systemctl is-active tpan'
  ok "(đồ nghề) tạm dừng tpan serve để trạng thái site đứng yên" has '^inactive$'
  vroot bash "$RT/site.sh" tong "$A"
  printf '%s\n' "$OUT" | grep -E '^(tep|db)=' >"$TMPD/e-truoc"
  ok "(đồ nghề) checksum mọi file và bản xuất DB của $A (DB xuất được, khác rỗng)" has 'db-byte=[0-9]{4,} db-ten=co$'
  vroot python3 "$RT/doi-byte.py" "$VGW_OBJ/$PREFIX/$KEY_C" 100000
  ok "(đồ nghề) đã đổi 1 byte của object $KEY_C trên đĩa của kho" has '^doi-byte '
  local mark
  mark="$(job_mark)"
  vroot tpan backup restore "$A" --id "$ID_C" --yes
  ok "backup restore $A --id $ID_C: thoát khác 0" rc_not 0
  ok "câu lỗi nhắc sha256" bash -c 'grep -qi "sha256" <<<"$1"' _ "$OUT"
  jobs_py since "$mark"
  ok "có đúng một job backup.pull, nó lỗi, lỗi nhắc sha256" bash -c '[ "$(grep -cE "^job [0-9a-f]+ [a-z_]+ backup\.pull " <<<"$1")" -eq 1 ] && grep -E "^job [0-9a-f]+ [a-z_]+ backup\.pull " <<<"$1" | grep -v " succeeded " | grep -qi sha256' _ "$OUT"
  ok "không có job backup.restore nào (dừng ở job tải về)" hasnt '^job [0-9a-f]+ [a-z_]+ backup\.restore '
  vroot bash -c 'ls -la "$(dirname "$1")"; if [ -e "$1" ]; then echo "con-file-dich"; else echo "khong-file-dich"; fi; echo "so-file-part=$(find "$(dirname "$1")" -name "*.part" | wc -l)"' _ "$PATH_C"
  ok "không để lại file đích hay file .part" bash -c 'grep -qx "khong-file-dich" <<<"$1" && grep -qx "so-file-part=0" <<<"$1"' _ "$OUT"
  vroot bash "$RT/site.sh" tong "$A"
  printf '%s\n' "$OUT" | grep -E '^(tep|db)=' >"$TMPD/e-sau"
  run diff "$TMPD/e-truoc" "$TMPD/e-sau"
  ok "checksum mọi file của site và bản xuất DB trước và sau giống hệt (B4)" rc_is 0
  vroot bash -c 'systemctl start tpan; for i in $(seq 1 30); do curl -sk -o /dev/null https://127.0.0.1:8443/api/v1/health && break; sleep 1; done; systemctl is-active tpan'
  ok "(đồ nghề) tpan serve chạy lại, panel trả lời" has '^active$'
  part_end E
}

# ================================================================ PHẦN F

part_f() {
  part_begin "F: khôi phục bản chỉ còn trên kho ($ID_B của $A)"
  # Giữ 3 bản: lần dọn sau bản before-restore không được lật bản vừa tải về
  # thành "chỉ còn trên kho" lần nữa.
  vroot tpan backup settings --keep 3 --yes
  ok "backup settings --keep 3: job thành công" job_ok
  vroot bash "$RT/site.sh" ten "$A" "DA SUA E2E"
  vroot bash "$RT/site.sh" moc "$A" moc-SUA
  site_view "$A"
  ok "(đồ nghề) site đã sửa: tên blog DA SUA E2E, file đánh dấu moc-SUA" bash -c 'grep -qF "DA SUA E2E" <<<"$1" && grep -qx "moc=200 moc-SUA" <<<"$1"' _ "$OUT"
  local mark
  mark="$(job_mark)"
  vroot tpan backup restore "$A" --id "$ID_B" --yes
  ok "backup restore $A --id $ID_B: thoát 0" rc_is 0
  jobs_py since "$mark"
  ok "hai job theo thứ tự: backup.pull rồi backup.restore, cả hai thành công" two_jobs
  site_view "$A"
  ok "site $A trả 200" has '^trang-chu=200 '
  ok "file đánh dấu là của bản $ID_B (moc-B), không phải nội dung đã sửa" has '^moc=200 moc-B$'
  ok "tên blog không còn DA SUA E2E" hasntF 'DA SUA E2E'
  bk_list "$A"
  ok "dòng $ID_B về local=true, vẫn stored" bkq --arg i "$ID_B" '[.[] | select(.id == $i)][0] | .local == true and .remote == "stored"'
  part_end F
}

# ================================================================ PHẦN G

part_g() {
  part_begin "G: giữ bản (s3.keep = 2, backup.keep = 4, site $C)"
  target_set "$VGW_ENV" "" --prefix "$PREFIX" --keep 2
  ok "target set --keep 2: job thành công" target_ok
  vjson tpan backup target show --json
  ok "target show: keep = 2" jraw '.keep == 2'
  vroot tpan backup settings --keep 4 --yes
  ok "backup settings --keep 4: job thành công" job_ok
  local i n0 n1
  for i in 1 2 3 4; do
    vjson tpan backup create "$C" --yes --json
    ok "bản $i của $C: thoát 0, job thành công" t_ok '.job.status == "succeeded"'
  done
  bk_list "$C"
  vroot bash -c 'ls -1 "$1/$2/sites/$3/"' _ "$VGW_OBJ" "$PREFIX" "$C"
  ok "kho còn đúng 2 object của $C" count_is 2 '\.tar\.gz$'
  ok "2 bản mới nhất stored, 2 bản cũ hơn đã dọn khỏi kho (remote rỗng) nhưng còn trên máy" bkq '[.[] | select(.kind == "full")] | length == 4 and (.[0:2] | all(.remote == "stored")) and (.[2:4] | all(.remote == "" and .local == true))'
  n0="$("$VM" root python3 "$RT/jobs.py" count backup.push </dev/null | tr -d '\r' | awk '{print $3}')"
  vjson tpan backup push --json
  ok "tpan backup push lần 1: thoát 0, không đẩy bản nào" t_ok '((.pushed // []) | length == 0) and ((.failed // []) | length == 0)'
  vjson tpan backup push --json
  ok "tpan backup push lần 2: thoát 0, không đẩy bản nào" t_ok '((.pushed // []) | length == 0) and ((.failed // []) | length == 0)'
  n1="$("$VM" root python3 "$RT/jobs.py" count backup.push </dev/null | tr -d '\r' | awk '{print $3}')"
  printf 'Số job backup.push trong tpan.db: trước %s, sau %s\n' "$n0" "$n1"
  ok "không có job backup.push mới (B14)" bash -c '[ -n "$1" ] && [ "$1" = "$2" ]' _ "$n0" "$n1"
  vroot bash -c 'ls -1 "$1/$2/sites/$3/"' _ "$VGW_OBJ" "$PREFIX" "$C"
  ok "kho vẫn đúng 2 object của $C" count_is 2 '\.tar\.gz$'
  part_end G
}

# ================================================================ PHẦN H

# get_routes_missing: đối chiếu mọi `pattern: "GET ..."` trong
# internal/server/routes.go (đọc lúc chạy, nên route mới thêm cũng bị đòi)
# với các route api-get-het.sh đã gọi (OUT của nó). Kết quả ở MISSING.
# Ngoại lệ: sự kiện của một lượt AI (cần phiên AI có lượt; e2e không mở phiên nào).
ROUTE_EXCEPT='/api/v1/ai/sessions/{id}/turns/{seq}/events'
MISSING=""
get_routes_missing() {
  local want got
  want="$(sed -nE 's/.*pattern: "GET ([^"]+)".*/\1/p' internal/server/routes.go | sort -u)"
  got="$({ sed -nE 's|^[0-9]{3} [0-9]+ (/api/[^ ?]+).*|\1|p' <<<"$OUT"; sed -nE 's/^phu-route (.*)$/\1/p' <<<"$OUT"; } |
    sed -e "s|^/api/v1/sites/$A|/api/v1/sites/{domain}|" -e "s|^/api/v1/users/$UA\$|/api/v1/users/{name}|" | sort -u)"
  MISSING="$(comm -23 <(printf '%s\n' "$want") <(printf '%s\n' "$got") | grep -vxF "$ROUTE_EXCEPT" | tr '\n' ' ')"
  MISSING="${MISSING% }"
}

part_h() {
  part_begin "H: quét bí mật (hai khoá truy cập và khoá sao lưu phải xuất hiện 0 lần)"
  harvest_env "$VGW_ENV"

  section "H1: tpan.db và -wal"
  scan_vm /dev/null /var/lib/tpan/tpan.db /var/lib/tpan/tpan.db-wal
  ok "mẫu quét đủ: 2 khoá truy cập của versitygw, khoá sao lưu" has '^mau_kho=2 mau_khoa_sao_luu=1 '
  ok "tpan.db: 0 dòng chứa bí mật" has '^dem=0 /var/lib/tpan/tpan\.db$'
  if has '^khong-co /var/lib/tpan/tpan\.db-wal$'; then
    note "lúc quét không có tpan.db-wal (đã checkpoint vào tpan.db)"
  else
    ok "tpan.db-wal: 0 dòng chứa bí mật" has '^dem=0 /var/lib/tpan/tpan\.db-wal$'
  fi

  section "H2: journalctl -u tpan"
  vroot bash -c 'journalctl -u tpan --no-pager -o cat >"$1/journal-tpan.txt" 2>&1; echo "so-dong=$(wc -l <"$1/journal-tpan.txt")"' _ "$RT"
  scan_vm /dev/null "$RT/journal-tpan.txt"
  ok "journal của tpan: 0 dòng chứa bí mật" scan_zero

  section "H3: mọi câu trả lời GET của API (phiên admin) và DELETE /backups/target?dry_run=1"
  RUNQ_CMD="$VM root bash $RT/api-get-het.sh $RT/h-api $A $UA < <file mật khẩu admin>"
  RUNQ_TS="$(ts)"
  CAP_VM="" vm_cap "$TMPD/pw.admin" root bash "$RT/api-get-het.sh" "$RT/h-api" "$A" "$UA"
  show_last
  ok "admin đăng nhập: 204" has '^dang-nhap=204$'
  ok "GET /api/v1/backups, /backups/target, /backups/remote trả 200" bash -c 'grep -qE "^200 [0-9]+ /api/v1/backups$" <<<"$1" && grep -qE "^200 [0-9]+ /api/v1/backups/target$" <<<"$1" && grep -qE "^200 [0-9]+ /api/v1/backups/remote$" <<<"$1"' _ "$OUT"
  get_routes_missing
  printf 'Route GET trong internal/server/routes.go chưa được gọi: %s\n' "${MISSING:-(không có)}"
  ok "mọi route GET khai trong internal/server/routes.go đều được gọi (trừ $ROUTE_EXCEPT)" test -z "$MISSING"
  ok "đã gọi ít nhất 40 lần GET (route tĩnh và từng job)" bash -c 'n=$(sed -nE "s/^so-cau-tra-loi-get=([0-9]+)$/\1/p" <<<"$1"); [ "${n:-0}" -ge 40 ]' _ "$OUT"
  ok "DELETE /api/v1/backups/target?dry_run=1 trả 200 (bản xem trước)" has '^200 [0-9]+ DELETE /api/v1/backups/target\?dry_run=1$'
  note "số trường old/new trong bản xem trước gỡ đích: $(sed -nE 's/^truong-old-new=(.*)$/\1/p' <<<"$OUT")"
  vjson tpan backup target show --json
  ok "bản xem trước không gỡ đích (vẫn configured)" jraw '.configured == true'
  scan_vm /dev/null "$RT/h-api"
  ok "mọi câu trả lời GET, bản xem trước DELETE và các trường old/new đã giải base64: 0" scan_zero
  ok "file đã giải base64 có được quét" hasF "dem=0 $RT/h-api/delete-dry-giai-base64.txt"

  section "H4: tpan mcp, mọi công cụ đọc"
  run env CGO_ENABLED=0 GOOS=linux GOARCH="$VM_ARCH" go build -o "$TMPD/mcpclient" ./scripts/e2e-m7/mcpclient
  ok "(đồ nghề) build mcpclient của e2e M7 cho linux/$VM_ARCH" rc_is 0
  push_bin "$TMPD/mcpclient" mcpclient
  local jpush jset jpanel
  jpush="$("$VM" root python3 "$RT/jobs.py" lastid backup.push </dev/null | tr -d '\r')"
  jset="$("$VM" root python3 "$RT/jobs.py" lastid backup.target.set </dev/null | tr -d '\r')"
  jpanel="$("$VM" root python3 "$RT/jobs.py" lastid backup.panel </dev/null | tr -d '\r')"
  {
    printf '%s\n' list-tools list_sites 'site_health {"domain":"{{site:0}}"}' stack_status machine \
      'tail_log {"domain":"{{site:0}}","log":"access"}' \
      'managed_file {"path":"/etc/tpan/s3.json"}' 'managed_file {"path":"/etc/tpan/backup.key"}' \
      'list_jobs {"limit":200}'
    for j in "$jpush" "$jset" "$jpanel"; do
      [[ "$j" =~ ^[0-9a-f]{16}$ ]] && printf 'get_job {"id":"%s"}\n' "$j"
    done
    printf '%s\n' list_plans list_backups current_view
  } >"$TMPD/mcp.in"
  printf '\nKịch bản MCP (mỗi dòng một lời gọi):\n'
  sed 's/^/    /' "$TMPD/mcp.in"
  RUNQ_CMD="$VM root bash -c '$RT/mcpclient -timeout 5m -- /usr/local/bin/tpan mcp >$RT/mcp.out 2>$RT/mcp.err; ...' < kịch bản"
  RUNQ_TS="$(ts)"
  CAP_VM="" vm_cap "$TMPD/mcp.in" root bash -c '"$1/mcpclient" -timeout 5m -- /usr/local/bin/tpan mcp >"$1/mcp.out" 2>"$1/mcp.err"; echo "mcpclient mã $?"; jq -c "{op, tool, loi: (if .error then .error else (.result.isError // false) end), exit_code}" "$1/mcp.out"' _ "$RT"
  show_last
  ok "phiên MCP kết nối được và tpan mcp thoát 0 khi đóng" bash -c 'grep -q "\"op\":\"connect\"" <<<"$1" && grep -q "\"op\":\"close\".*\"exit_code\":0" <<<"$1"' _ "$OUT"
  ok "đã gọi đủ các công cụ đọc của kịch bản" bash -c '[ "$(grep -c "\"op\":\"call\"" <<<"$1")" -eq "$2" ]' _ "$OUT" "$(grep -vc '^list-tools$' "$TMPD/mcp.in")"
  ok "managed_file với /etc/tpan/s3.json và /etc/tpan/backup.key không trả nội dung (lỗi)" bash -c '[ "$(grep -c "\"tool\":\"managed_file\",\"loi\":false" <<<"$1")" -eq 0 ]' _ "$OUT"
  scan_vm /dev/null "$RT/mcp.out" "$RT/mcp.err"
  ok "output của tpan mcp (mọi công cụ đọc): 0" scan_zero

  section "H5: gói panel đã giải mã"
  vroot bash -c 'ls -1 "$1/$2/panel/"' _ "$VGW_OBJ" "$PREFIX"
  local panel
  panel="$(grep -E '^tpan-[0-9]{8}-[0-9]{6}\.tar\.gz\.age$' <<<"$OUT" | sort | tail -1)"
  vroot bash "$RT/mo-goi-panel.sh" "$VGW_OBJ/$PREFIX/panel/$panel" "$RT/goi-h"
  ok "age -d mở được gói mới nhất ($panel)" has '^age-ma=0 '
  scan_vm /dev/null "$RT/goi-h"
  ok "gói panel đã giải mã: tpan.db có được quét" has '^dem=0 .*/goi-h/tpan\.db$'
  ok "gói panel đã giải mã (tpan.db, manifest.json): 0" scan_zero

  section "H6: user-data do tpan cloud-init render sinh"
  printf '%s\n' '# sites.yaml của e2e M18 (phần H)' 'defaults:' "  admin_email: $EMAIL" 'sites:' \
    '  - domain: h1.m18.example' '    title: "Site H1 có dấu: và # và nháy"' '  - domain: h2.m18.example' >"$TMPD/h-sites.yaml"
  run "$VM" push "$TMPD/h-sites.yaml" /tmp/e2e-m18-h-sites.yaml
  vroot bash -c 'install -m 0600 /tmp/e2e-m18-h-sites.yaml "$1/h-sites.yaml" && rm -f /tmp/e2e-m18-h-sites.yaml; s=$(sha256sum /usr/local/bin/tpan | cut -c1-64); printf "%s  tpan-linux-amd64\n%s  tpan-linux-arm64\n" "$s" "$s" >"$1/h-SHA256SUMS"; cat "$1/h-SHA256SUMS"' _ "$RT"
  render_run "$RT/h-sites.yaml" https://downloads.example.com/tpan "$RT/h-SHA256SUMS" "$RT/h-user-data.yaml" h
  ok "render: thoát 0, mật khẩu quản trị in đúng một lần, stderr đúng một dòng" bash -c '[ "$1" = 0 ] && [ "$2" = 1 ] && [ "$3" = 1 ]' _ "$RC" "$RPW_HITS" "$RERR_LINES"
  vroot bash -c 'stat -c "%a %U:%G %s" "$1"; python3 "$2/giai-user-data.py" "$1" "$2/h-ud"' _ "$RT/h-user-data.yaml" "$RT"
  ok "file --out là 600 root:root" has '^600 root:root [0-9]+$'
  ok "user-data: dòng đầu #cloud-config, đúng một runcmd (B12)" bash -c 'grep -qx "dong-dau #cloud-config" <<<"$1" && grep -qx "so-runcmd 1" <<<"$1"' _ "$OUT"
  ok "user-data có config.json (b64) và sites.yaml (gz+b64) đã giải được" bash -c 'grep -qE "^tep /etc/tpan/cloud/config.json [0-9]+ b64 [1-9]" <<<"$1" && grep -qE "^tep /etc/tpan/cloud/sites.yaml [0-9]+ gz\+b64 [1-9]" <<<"$1"' _ "$OUT"
  scan_vm "$TMPD/pw.render-h" "$RT/h-user-data.yaml" "$RT/h-ud"
  ok "mẫu quét có thêm mật khẩu quản trị render vừa in" has ' mau_them=1 '
  ok "user-data (thô, và mọi file đã giải base64, gzip): 0 khoá, 0 mật khẩu dạng rõ (B6)" scan_zero
  part_end H
}

# ================================================================ PHẦN I

part_i() {
  part_begin "I: máy chủ S3 thứ hai (Garage, vùng garage)"
  vroot bash "$RT/garage.sh"
  ok "(đồ nghề) Garage chạy, khoá sinh tại chỗ" bash -c '[ "$2" = 0 ] && grep -qx "garage-thu: active" <<<"$1"' _ "$OUT" "$RC"
  harvest_env "$GAR_ENV"
  target_set "$GAR_ENV" "" --prefix "$PREFIX"
  ok "đặt đích Garage: job thành công" target_ok
  ok "có bước thử ghi/đọc/xoá trên Garage và bước đó đã chạy" probe_ran
  note "nối lại khi đổi sang Garage: $(jq -c '.result' "$TMPD/run.json" 2>/dev/null)"
  vjson tpan backup target show --json
  ok "target show: endpoint Garage, vùng garage" jraw '.endpoint == "http://127.0.0.1:3900" and .region == "garage"'

  vroot tpan backup settings --keep 1 --yes
  ok "backup settings --keep 1: job thành công" job_ok
  vroot bash "$RT/site.sh" moc "$A" moc-I1
  vjson tpan backup create "$A" --yes --json
  ok "backup create $A: thoát 0, job thành công" t_ok '.job.status == "succeeded"'
  bk_list "$A"
  ID_I1="$(newest_full)"
  ok "bản $ID_I1 stored trên Garage" bkq --arg i "$ID_I1" '[.[] | select(.id == $i)][0] | .local == true and .remote == "stored"'
  local key size
  key="$(field "$ID_I1" remote_key)"
  size="$(field "$ID_I1" size)"
  vjson tpan backup list --remote --json
  ok "backup list --remote: Garage có object $key đúng cỡ, ghép với bản $ID_I1" jraw --arg k "$key" --argjson s "${size:-0}" --arg i "$ID_I1" '[.objects[] | select(.key == $k and .size == $s and .backup_id == $i)] | length == 1'
  vroot bash "$RT/site.sh" moc "$A" moc-I2
  vjson tpan backup create "$A" --yes --json
  ok "backup create $A lần hai: thoát 0" t_ok '.job.status == "succeeded"'
  bk_list "$A"
  ok "bản $ID_I1 giờ chỉ còn trên Garage (local=false, stored)" bkq --arg i "$ID_I1" '[.[] | select(.id == $i)][0] | .local == false and .remote == "stored"'

  vroot tpan backup settings --keep 3 --yes
  ok "backup settings --keep 3: job thành công" job_ok
  local mark
  mark="$(job_mark)"
  vroot tpan backup restore "$A" --id "$ID_I1" --yes
  ok "khôi phục $ID_I1 từ Garage: thoát 0" rc_is 0
  jobs_py since "$mark"
  ok "hai job: backup.pull rồi backup.restore, cả hai thành công" two_jobs
  site_view "$A"
  ok "site $A trả 200 với file đánh dấu của bản $ID_I1 (moc-I1)" bash -c 'grep -q "^trang-chu=200 " <<<"$1" && grep -qx "moc=200 moc-I1" <<<"$1"' _ "$OUT"
  part_end I
}

# ================================================================ PHẦN K

part_k() {
  part_begin "K: phân quyền (user thường $UA, site $C)"
  vroot tpan backup settings --keep 1 --yes
  ok "backup settings --keep 1: job thành công" job_ok
  vjson tpan backup create "$C" --yes --json
  ok "backup create $C (K1): thoát 0" t_ok '.job.status == "succeeded"'
  bk_list "$C"
  local K1 K2 jid r
  K1="$(newest_full)"
  vjson tpan backup create "$C" --yes --json
  ok "backup create $C (K2): thoát 0" t_ok '.job.status == "succeeded"'
  bk_list "$C"
  K2="$(newest_full)"
  ok "$K1 chỉ còn trên kho, $K2 trên máy" bkq --arg a "$K1" --arg b "$K2" '([.[] | select(.id == $a)][0] | .local == false and .remote == "stored") and ([.[] | select(.id == $b)][0].local == true)'
  vroot tpan backup settings --keep 3 --yes
  ok "backup settings --keep 3: job thành công" job_ok

  login adm admin "$TMPD/pw.admin"
  ok "admin đăng nhập: 204" code_is 204
  login a "$UA" "$TMPD/pw.a"
  ok "$UA đăng nhập: 204" code_is 204
  api adm GET /api/v1/backups/target
  ok "(đối chứng) admin GET /api/v1/backups/target: 200, route có thật" code_is 200

  local zero64
  zero64="$(printf '0%.0s' $(seq 1 64))"
  for r in "GET /api/v1/backups/target" \
    "POST /api/v1/backups/target?dry_run=1" \
    "DELETE /api/v1/backups/target?dry_run=1" \
    "POST /api/v1/backups/target/test" \
    "POST /api/v1/backups/$K2/push?dry_run=1" \
    "POST /api/v1/backups/$K1/pull?dry_run=1" \
    "GET /api/v1/backups/remote" \
    "POST /api/v1/cloud-init/render"; do
    case "$r" in
      "POST /api/v1/backups/target?dry_run=1") api a POST "${r#POST }" '{"endpoint":"https://s3.example.com","bucket":"thu-e2e","access_key":"","secret_key":""}' ;;
      "POST /api/v1/cloud-init/render") api a POST "${r#POST }" "{\"web\":\"nginx\",\"base\":\"https://downloads.example.com/tpan\",\"sha256_amd64\":\"$zero64\",\"sha256_arm64\":\"$zero64\"}" ;;
      *) api a "${r%% *}" "${r#* }" ;;
    esac
    ok "user thường: $r -> 403" code_is 403
  done

  api a GET /api/v1/backups
  ok "user thường GET /api/v1/backups: 200, chỉ bản của $C (ít nhất 2), không có remote_key, remote_error" \
    code_bq 200 --arg c "$C" 'length >= 2 and all(.[]; .domain == $c) and all(.[]; (has("remote_key") or has("remote_error")) | not)'
  ok "các dòng có trường local và remote" bq 'all(.[]; has("local") and has("remote"))'
  API_MAX=300 api a GET "/api/v1/jobs?limit=500"
  ok "user thường không thấy job backup.push, backup.pull, backup.panel (B5)" \
    code_bq 200 '[.[] | select(.kind | test("^backup[.](push|pull|panel)$"))] | length == 0'
  api a POST "/api/v1/backups/$K1/restore"
  ok "user thường khôi phục bản của mình chỉ còn trên kho ($K1): 409 backup_not_on_machine" expect 409 backup_not_on_machine
  api a POST "/api/v1/backups/$K2/restore"
  ok "user thường khôi phục bản của mình trên máy ($K2): 202" code_is 202
  jid="$(jb '.job_id // empty')"
  wait_job a "$jid"
  ok "job khôi phục $K2 thành công" test "$JOBST" = succeeded
  site_view "$C"
  ok "site $C trả 200 sau khi khôi phục" has '^trang-chu=200 '
  part_end K
}

# ================================================================ PHẦN L

part_l() {
  part_begin "L: cloud-init (máy ảo phụ $CI)"
  addrs
  vexec tpan version
  if ! has "^tpan $VER "; then
    ok "máy ảo chính có tpan $VER (phần L cần tpan của cây; chạy trước: scripts/e2e-m18.sh chinh)" false
    part_end L
    return
  fi
  local pinned t0 t1 i
  # "ci" có thể chạy ở một lần gọi riêng sau "chinh": chép lại đồ nghề cho chắc là bản mới nhất.
  push_tools
  run bash -c 'ls -l "bin/tpan-linux-amd64" "bin/tpan-linux-arm64" && cd bin && shasum -a 256 tpan-linux-amd64 tpan-linux-arm64 >"$1/SHA256SUMS" && cat "$1/SHA256SUMS"' _ "$TMPD"
  ok "file chạy của nhánh có cho cả amd64 và arm64; SHA256SUMS sinh ngay (tên trần)" rc_is 0
  pinned="$(awk -v f="tpan-linux-$VM_ARCH" '$2 == f {print $1}' "$TMPD/SHA256SUMS")"
  vexec bash -c 'sha256sum /usr/local/bin/tpan | cut -c1-64'
  ok "tpan trong máy ảo chính là đúng file bin/tpan-linux-$VM_ARCH sẽ phục vụ" out_eq "$pinned"

  section "L: phục vụ file chạy từ máy ảo chính (python3 -m http.server dưới systemd-run)"
  for f in tpan-linux-amd64 tpan-linux-arm64; do
    run "$VM" push "bin/$f" "/tmp/e2e-m18-$f"
  done
  run "$VM" push "$TMPD/SHA256SUMS" /tmp/e2e-m18-SHA256SUMS
  HTTP_UP=1
  vroot bash -c 'set -e; systemctl stop tpan-e2e-http 2>/dev/null || true; systemctl reset-failed tpan-e2e-http 2>/dev/null || true
    rm -rf "$1"; install -d -m 0755 "$1"
    for f in tpan-linux-amd64 tpan-linux-arm64 SHA256SUMS; do install -m 0644 "/tmp/e2e-m18-$f" "$1/$f"; rm -f "/tmp/e2e-m18-$f"; done
    cd "$1" && sha256sum -c SHA256SUMS
    systemd-run --quiet --unit=tpan-e2e-http --working-directory="$1" python3 -m http.server "$2" --bind 0.0.0.0
    for i in $(seq 1 20); do curl -fsS -o /dev/null "http://127.0.0.1:$2/SHA256SUMS" && break; sleep 0.5; done
    echo "http=$(curl -s -o /dev/null -w "%{http_code}" "http://127.0.0.1:$2/SHA256SUMS")"' _ "$SRV_DIR" "$HTTP_PORT"
  ok "(đồ nghề) máy chính phục vụ $SRV_DIR ở cổng $HTTP_PORT, sha256 của file đã chép khớp" bash -c '[ "$2" = 0 ] && grep -qx "http=200" <<<"$1" && [ "$(grep -c ": OK$" <<<"$1")" -eq 2 ]' _ "$OUT" "$RC"

  section "L: tpan cloud-init render trong máy ảo chính"
  printf '%s\n' '# sites.yaml của e2e M18 (phần L)' 'defaults:' "  admin_email: $EMAIL" 'sites:' \
    '  - domain: ci-a.m18.example' '    title: "Site CI A"' '  - domain: ci-b.m18.example' >"$TMPD/ci-sites.yaml"
  run "$VM" push "$TMPD/ci-sites.yaml" /tmp/e2e-m18-ci-sites.yaml
  vroot bash -c 'install -d -m 0700 "$1" && install -m 0600 /tmp/e2e-m18-ci-sites.yaml "$1/ci-sites.yaml" && rm -f /tmp/e2e-m18-ci-sites.yaml && cat "$1/ci-sites.yaml"' _ "$RT"
  render_run "$RT/ci-sites.yaml" "http://$VM_IP:$HTTP_PORT" "$SRV_DIR/SHA256SUMS" "$RT/user-data.yaml" ci
  ok "render --base http://<vm>:$HTTP_PORT --sums: thoát 0, mật khẩu quản trị in đúng một lần, stderr một dòng" bash -c '[ "$1" = 0 ] && [ "$2" = 1 ] && [ "$3" = 1 ]' _ "$RC" "$RPW_HITS" "$RERR_LINES"
  vroot bash -c 'stat -c "%a %U:%G %s" "$1"; python3 "$2/giai-user-data.py" "$1" "$2/ci-ud"; grep -c "sha256" "$2/ci-ud/"*tpan-first-boot' _ "$RT/user-data.yaml" "$RT"
  ok "user-data: 600 root:root, #cloud-config, đúng một runcmd, có script khởi động đầu" bash -c 'grep -qE "^600 root:root [0-9]+$" <<<"$1" && grep -qx "dong-dau #cloud-config" <<<"$1" && grep -qx "so-runcmd 1" <<<"$1" && grep -q "^tep /usr/local/sbin/tpan-first-boot " <<<"$1"' _ "$OUT"
  vroot bash -c 'grep -nE "^ *(amd64|arm64)\) sum=|curl " "$1/ci-ud/"*tpan-first-boot' _ "$RT"
  ok "script khởi động đầu ghim đúng sha256 của file chạy $VM_ARCH" hasF "$pinned"
  scan_vm "$TMPD/pw.render-ci" "$RT/user-data.yaml" "$RT/ci-ud"
  ok "user-data của máy phụ (thô và đã giải): 0 khoá, 0 mật khẩu dạng rõ" scan_zero
  vroot bash -c 'install -m 0600 -o ubuntu -g ubuntu "$1" /home/ubuntu/e2e-m18-user-data.yaml' _ "$RT/user-data.yaml"
  run "$VM" pull /home/ubuntu/e2e-m18-user-data.yaml "$TMPD/user-data.yaml"
  vexec rm -f /home/ubuntu/e2e-m18-user-data.yaml
  ok "lấy user-data về Mac" bash -c '[ -s "$1" ] && head -1 "$1" | grep -qx "#cloud-config"' _ "$TMPD/user-data.yaml"

  section "L: tạo máy phụ $CI từ user-data"
  run "$VM" delete "$CI"
  CI_MADE=1
  t0="$(date +%s)"
  run "$VM" cloud-init "$TMPD/user-data.yaml" "$CI"
  ok "multipass launch xong (chờ cloud-init chạy hết)" rc_is 0
  CI_IP="$("$VM" on "$CI" ip </dev/null 2>/dev/null)"
  t1=""
  for i in $(seq 1 60); do
    runq "$VM" on "$CI" exec bash -c 'for d in ci-a.m18.example ci-b.m18.example; do printf "%s=%s\n" "$d" "$(curl -s -m 10 --resolve "$d:80:127.0.0.1" -o /dev/null -w "%{http_code}" "http://$d/")"; done'
    if [ "$(grep -cE '^ci-[ab]\.m18\.example=200$' "$TMPD/run.out")" -eq 2 ]; then
      t1="$(date +%s)"
      break
    fi
    sleep 5
  done
  show_last
  if [ -n "$t1" ]; then
    note "THỜI GIAN từ lúc tạo máy phụ (multipass launch) tới lúc hai site trả 200: $((t1 - t0)) giây"
  else
    note "THỜI GIAN: hai site chưa trả 200 sau $(($(date +%s) - t0)) giây kể từ lúc tạo máy phụ"
  fi
  ok "hai site ci-a, ci-b trả 200 trên máy phụ" test -n "$t1"
  vci cloud-init status
  ok "cloud-init status: done" has '^status: done$'
  vjson --ci tpan cloud-init status --json
  ok "tpan cloud-init status --json: ok, mọi việc (setup, admin, stack, sites, health) done" jraw '.ok == true and ([.steps[].name] == ["setup", "admin", "stack", "sites", "health"]) and ([.steps[].status] | all(. == "done"))'
  vci bash -c 'echo "health=$(curl -sk -o /dev/null -w "%{http_code}" https://127.0.0.1:8443/api/v1/health)"; stat -c "%a %U" /root/tpan-first-boot-passwords.txt /var/log/tpan/first-boot.log'
  ok "panel trên máy phụ trả 200 ở /api/v1/health" has '^health=200$'
  ok "mật khẩu WordPress ghi vào /root/tpan-first-boot-passwords.txt 600 root, log first-boot 600 root (không in)" bash -c '[ "$(grep -cx "600 root" <<<"$1")" -eq 2 ]' _ "$OUT"
  (umask 077 && jq -nc --rawfile p "$TMPD/pw.render-ci" '{name: "admin", password: $p}' >"$TMPD/req.json")
  RUNQ_CMD="$VM on $CI root bash -c 'curl -sk ... --data-binary @- https://127.0.0.1:8443/api/v1/login' < {name: admin, password: <mật khẩu render in ra, đã che>}"
  RUNQ_TS="$(ts)"
  CAP_VM="$CI" vm_cap "$TMPD/req.json" root bash -c 'curl -sk -m 30 -o /dev/null -w "dang-nhap=%{http_code}\n" -H "Content-Type: application/json" -H "Sec-Fetch-Site: same-origin" --data-binary @- https://127.0.0.1:8443/api/v1/login'
  show_last
  ok "đăng nhập panel máy phụ bằng mật khẩu render đã in: 204" has '^dang-nhap=204$'
  vci tpan health
  ok "tpan health trên máy phụ thoát 0" rc_is 0
  vci bash -c 'sha256sum /usr/local/bin/tpan | cut -c1-64'
  ok "file chạy trên máy phụ là bản đã ghim" out_eq "$pinned"

  section "L: B15, thay file chạy bằng v0.2.0 (giả một lần nâng bản) rồi chạy lại script khởi động đầu"
  local oldsum
  oldsum="$(awk -v f="tpan-linux-$VM_ARCH" '$2 == f {print $1}' "$OLD_DIR/SHA256SUMS" 2>/dev/null)"
  run "$VM" on "$CI" push "$OLD_DIR/tpan-linux-$VM_ARCH" /tmp/tpan-old
  vci bash -c 'install -m 0755 /tmp/tpan-old /usr/local/bin/tpan && rm -f /tmp/tpan-old && sha256sum /usr/local/bin/tpan | cut -c1-64'
  ok "(đồ nghề) máy phụ giờ chạy file của $OLD_VER" out_eq "$oldsum"
  vci bash -c '/usr/local/sbin/tpan-first-boot </dev/null; echo "first-boot mã $?"; echo "sau=$(sha256sum /usr/local/bin/tpan | cut -c1-64)"'
  note "script chạy lại exec bản $OLD_VER (không có lệnh cloud-init) nên mã thoát khác 0 là đúng; điều cần kiểm là file chạy"
  ok "file chạy không đổi: vẫn là $OLD_VER, không bị hạ về bản đã ghim (B15)" bash -c 'grep -qx "sau=$2" <<<"$1" && [ "$2" != "$3" ]' _ "$OUT" "$oldsum" "$pinned"

  section "L: đổi một byte của file chạy trên máy phục vụ rồi tạo lại máy phụ"
  run "$VM" delete "$CI"
  vroot python3 "$RT/doi-byte.py" "$SRV_DIR/tpan-linux-$VM_ARCH" 4096
  ok "(đồ nghề) file phục vụ đã đổi 1 byte (SHA256SUMS và user-data giữ nguyên)" has '^doi-byte '
  CI_IP=""
  run "$VM" cloud-init "$TMPD/user-data.yaml" "$CI"
  note "multipass launch trả $RC (Multipass trả 0 cả khi cloud-init lỗi)"
  CI_IP="$("$VM" on "$CI" ip </dev/null 2>/dev/null)"
  vci cloud-init status
  ok "cloud-init status: error" has '^status: error$'
  vci bash -c 'if [ -e /usr/local/bin/tpan ]; then echo "co-tpan"; else echo "khong-tpan"; fi; if [ -e /var/lib/tpan/first-boot.json ]; then echo "co-first-boot-json"; else echo "khong-first-boot-json"; fi; ls /usr/local/bin/tpan.first-boot 2>&1 | tail -1; grep -a "^tpan: " /var/log/cloud-init-output.log | tail -3'
  ok "không có /usr/local/bin/tpan, không có file trạng thái lần khởi động đầu" bash -c 'grep -qx "khong-tpan" <<<"$1" && grep -qx "khong-first-boot-json" <<<"$1"' _ "$OUT"
  ok "log cloud-init có dòng sha256 không khớp của script khởi động đầu" has '^tpan: sha256 '

  section "L: dọn"
  run "$VM" delete "$CI"
  CI_MADE=0
  CI_IP=""
  vroot bash -c 'systemctl stop tpan-e2e-http; rm -rf "$1"; echo "đã dọn $1"' _ "$SRV_DIR"
  HTTP_UP=0
  run multipass info "$CI"
  ok "máy phụ $CI đã xoá" rc_not 0
  part_end L
}
out_eq() { [ -n "$1" ] && [ "$(tr -d '[:space:]' <<<"$OUT")" = "$1" ]; }

# ================================================================ PHẦN N

part_n() {
  part_begin "N: nâng từ $OLD_VER lên $VER (máy không đặt đích: như cũ, B11)"
  reset_vm
  run bash -c 'cd "$1" && ls -l "tpan-linux-$2" SHA256SUMS && grep " tpan-linux-$2\$" SHA256SUMS | shasum -a 256 -c -' _ "$OLD_DIR" "$VM_ARCH"
  ok "file chạy $OLD_VER ($VM_ARCH) của bản phát hành: sha256 khớp SHA256SUMS" rc_is 0
  run "$VM" push "$OLD_DIR/tpan-linux-$VM_ARCH" /tmp/tpan
  vroot install -m 0755 /tmp/tpan /usr/local/bin/tpan
  vexec tpan version
  ok "tpan version là $OLD_VER" has "^tpan $OLD_VER "
  push_tools
  vroot tpan stack install --web nginx --yes
  ok "$OLD_VER: stack install --web nginx thành công" job_ok
  site_add "$A"
  site_add "$B"
  vroot tpan backup create "$A" --yes
  ok "$OLD_VER: backup create $A thành công" job_ok
  vjson tpan backup list "$A" --json
  local old_id
  old_id="$(jq -r '[.[] | select(.kind == "full")][0].id // empty' "$TMPD/run.json")"
  ok "$OLD_VER: có một bản đầy đủ $old_id" test -n "$old_id"
  vroot tpan backup settings --at 03:30 --yes
  ok "$OLD_VER: bật lịch backup 03:30" job_ok
  vroot bash -c 'cat /etc/cron.d/tpan-backup; sha256sum /etc/cron.d/tpan-backup'
  local cron0
  cron0="$(sed -nE 's|^([0-9a-f]{64})  /etc/cron.d/tpan-backup$|\1|p' <<<"$OUT")"
  ok "$OLD_VER: có /etc/cron.d/tpan-backup" test -n "$cron0"
  health_json
  health_ids >"$TMPD/ids-truoc"
  printf 'Số ID phép kiểm trước khi nâng: %s\n' "$(grep -c . "$TMPD/ids-truoc")"
  vroot tpan backup list
  head -1 <<<"$OUT" >"$TMPD/list-head-truoc"

  section "N: thay file chạy bằng bản $VER"
  deploy_new
  vroot bash -c 'sha256sum /etc/cron.d/tpan-backup'
  ok "/etc/cron.d/tpan-backup không đổi" has "^$cron0 "
  vroot bash -c 'grep -vE "^[[:space:]]*(#|$|[A-Z_]+=)" /etc/cron.d/tpan-backup | head -1 | cut -d" " -f7-'
  local cron_cmd
  cron_cmd="$(tr -d '\r' <<<"$OUT" | head -1)"
  ok "đọc được lệnh của dòng cron (cột 7 trở đi, chạy bằng root)" test -n "$cron_cmd"
  vroot bash -c "$cron_cmd"
  ok "lệnh trong /etc/cron.d/tpan-backup chạy được bằng bản mới (thoát 0)" rc_is 0
  vroot tpan backup restore "$A" --id "$old_id" --yes
  ok "bản sao lưu cũ $old_id (của $OLD_VER) khôi phục được" job_ok
  site_view "$A"
  ok "site $A trả 200 sau khi khôi phục" has '^trang-chu=200 '
  health_json
  health_ids >"$TMPD/ids-sau"
  run diff "$TMPD/ids-truoc" "$TMPD/ids-sau"
  ok "tpan health trước và sau khi nâng: cùng tập ID phép kiểm" rc_is 0
  ok "không có phép kiểm tpan.s3:* (chưa đặt đích)" bash -c '! grep -q "^tpan\.s3:" "$1"' _ "$TMPD/ids-sau"
  vroot tpan backup list
  head -1 <<<"$OUT" >"$TMPD/list-head-sau"
  run diff "$TMPD/list-head-truoc" "$TMPD/list-head-sau"
  ok "backup list (chữ): dòng tiêu đề như $OLD_VER, không có cột mới" rc_is 0
  vjson tpan backup list --json
  ok "backup list --json: mọi dòng local=true, remote rỗng" jraw 'length >= 1 and all(.[]; .local == true and .remote == "")'
  vroot bash -c 'for k in backup.push backup.pull backup.panel; do python3 "$1/jobs.py" count "$k"; done' _ "$RT"
  ok "không có job backup.push, backup.pull, backup.panel nào" bash -c '[ "$(grep -cE "^so-job backup\.(push|pull|panel) 0$" <<<"$1")" -eq 3 ]' _ "$OUT"
  part_end N
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
  ok "log không chứa khoá S3, khoá sao lưu, mật khẩu hay cookie nào" test "$n" -eq 0
  ok "log không chứa chuỗi nào dạng khoá sao lưu" bash -c '! grep -qE "tpan-key-1-[a-z2-7]{52}" "$1"' _ "$E2E_LOG"
  ok "mọi trường password trong log đều đã che" bash -c '! grep -E "\"(db_|admin_)?password\": *\"[^<\"]" "$1" | grep -vq "có (đã che)"' _ "$E2E_LOG"
  ok "log không chứa IP máy ảo (IPv4)" bash -c '[ -z "$1" ] || ! grep -qF -- "$1" "$2"' _ "$VM_IP" "$E2E_LOG"
  ok "log không chứa mạng Multipass" bash -c '[ -z "$1" ] || ! grep -qF -- "$1" "$2"' _ "$NET4" "$E2E_LOG"
  ok "log không chứa IPv6 của máy ảo" bash -c '[ -z "$1" ] || ! grep -qiF -- "$1" "$2"' _ "$V6PFX" "$E2E_LOG"
  ok "log không chứa IP của Mac" bash -c '[ -z "$1" ] || ! grep -qF -- "$1" "$2"' _ "$HOST_IP" "$E2E_LOG"
  ok "log không chứa thư mục nhà hay tên user Mac" bash -c '! grep -qF -- "$1" "$3" && ! grep -qF -- "$2" "$3"' _ "$HOME" "$USER" "$E2E_LOG"
}

usage() { sed -n '4,15p' "$0" | sed 's/^# \{0,1\}//'; exit 2; }
[ $# -ge 1 ] || usage
PARTS=()
for p in "$@"; do
  case "$p" in
    chinh) PARTS+=(a b c d e f g h i k) ;;
    ci) PARTS+=(l) ;;
    nang-cap) PARTS+=(n) ;;
    all) PARTS+=(a b c d e f g h i k l n) ;;
    *) usage ;;
  esac
done

printf 'tpan của cây: %s; bản cũ: %s; máy ảo: %s (máy phụ: %s); các phần: %s\n' "$VER" "$OLD_VER" "$VMNAME" "$CI" "${PARTS[*]}"
for p in "${PARTS[@]}"; do
  "part_$p"
done

leak_check
section "TỔNG KẾT: $PASSES khẳng định đạt, $FAILS khẳng định không đạt"
[ "$FAILS" -eq 0 ]
