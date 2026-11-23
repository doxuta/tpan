#!/usr/bin/env bash
# Lệnh trong máy ảo viết trong nháy đơn, biến của chúng mở ra trong máy ảo:
# shellcheck disable=SC2016
# e2e GIAO DIỆN của mốc M18 (sao lưu ra kho S3, khôi phục từ kho) trên máy ảo
# (tên trong file .tpan-vm; không có thì tpan-test). Chạy trên Mac, trong thư
# mục repo, từ máy sạch:
#
#   make e2e-m18-gd     (log vào tmp-phien/ rồi chép vào docs/bang-chung/M18-giao-dien.log)
#   E2E_LOG=tmp-phien/e2e-m18-gd.log bash scripts/e2e-m18-gd.sh > tmp-phien/e2e-m18-gd.log 2>&1
#
# Gõ thêm tên phần để chỉ chạy vài phần, theo thứ tự (mặc định: a b c d):
#   bash scripts/e2e-m18-gd.sh a b c
#
# Như e2e M16, các khẳng định chính do TRÌNH DUYỆT làm: Chrome không giao
# diện trên Mac (scripts/anh-readme, bộ m18-that) mở panel thật của máy ảo qua
# https, gõ vào form và bấm nút như người dùng. Script kiểm lại trong máy ảo
# bằng lệnh độc lập với giao diện.
#
# Kịch bản:
#   A  máy sạch; build và chép tpan của cây vào máy ảo (make vm-deploy), tpan
#      setup, stack nginx, một site; dựng kho thử versitygw
#      (scripts/e2e-m18/versitygw.sh), khoá sinh tại chỗ trong /root/s3thu.env.
#   B  trình duyệt -buoc dich: admin đăng nhập qua form, đặt đích qua thẻ "Đích
#      ngoài máy", bấm "Thử kết nối". Trong máy ảo: target show --json có
#      configured: true; /etc/tpan/s3.json là root:root 600.
#   C  tạo một bản chỉ còn trên kho bằng CLI: ghi file đánh dấu (moc-cu),
#      backup create (tự lên kho), sửa file đánh dấu (moc-moi), backup settings
#      --keep 1, backup create lần nữa: bản cũ bị dọn khỏi máy. Kiểm backup list
#      --json: bản cũ local=false, remote=stored. Rồi --keep 3 (như phần F của
#      e2e-m18: bản vừa tải về không bị lật thành "chỉ còn trên kho" lần nữa
#      khi job khôi phục tạo bản before-restore).
#   D  trình duyệt -buoc khoi-phuc: Khôi phục trên hàng bản chỉ còn trên kho,
#      "Tải về máy", rồi Khôi phục lần nữa. Trong máy ảo: file đánh dấu về
#      moc-cu, site trả 200, tpan.db có job backup.pull rồi backup.restore,
#      cả hai thành công.
#   E  (luôn chạy cuối, khi có E2E_LOG) quét chính file log: không khoá S3,
#      khoá sao lưu, mật khẩu, IP máy ảo, IP của Mac, thư mục nhà.
#
# THỬ PHẦN MÁY ẢO KHI CHƯA CÓ GIAO DIỆN: E2E_DICH_BANG_CLI=1 thay bước trình
# duyệt của B bằng `tpan backup target set` (như e2e-m18). Lần chạy đó in một
# dòng KHÔNG ĐẠT ("phần B chưa đặt đích qua giao diện"), nên không bao giờ
# xanh được bằng cách này.
#
# ĐỒ NGHỀ TEST, CHỈ TRONG MÁY ẢO: versitygw v1.11.0 ở 127.0.0.1:7070 (vùng
# vn-thu-1, bucket tpan-thu); site.sh, jobs.py của scripts/e2e-m18/. Hai khoá
# của kho được đọc vào danh sách che ngay khi sinh; trình duyệt nhận chúng qua
# biến môi trường TPAN_S3_ACCESS, TPAN_S3_SECRET (không qua dòng lệnh), mật
# khẩu admin qua TPAN_PANEL_PASSWORD.
#
# Output: mỗi lệnh có dấu thời gian, output nguyên văn (đã che) và mã thoát;
# mỗi khẳng định in "=> ĐẠT" hoặc "=> KHÔNG ĐẠT"; cuối mỗi phần và cuối lần
# chạy in số đạt, không đạt. Không dùng set -e.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2

VM=scripts/vm.sh
VMNAME="${TPAN_VM:-$(tr -d '[:space:]' 2>/dev/null <.tpan-vm)}"
VMNAME="${VMNAME:-tpan-test}"
TOOLS=scripts/e2e-m18
RT=/root/e2e-m18 # đồ nghề trong máy ảo
FAILS=0
PASSES=0
OUT=""
RC=0
EMAIL=admin@example.com
A=a.example.com
VGW_ENV=/root/s3thu.env
VER="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
DEBUG_PORT=18182 # cổng DevTools riêng, không đụng anh-m16 (18162), e2e-m16 (18172)

mkdir -p tmp-phien
TMPD="$(mktemp -d "$PWD/tmp-phien/e2e-m18-gd.XXXXXX")"
chmod 700 "$TMPD"
SECRETS="$TMPD/secrets"
: >"$SECRETS"
chmod 600 "$SECRETS"
trap 'rm -rf "$TMPD"' EXIT
trap 'exit 130' INT TERM
ANH="$TMPD/anh" # chương trình trình duyệt (scripts/anh-readme), build một lần

ts() { date '+%Y-%m-%d %H:%M:%S'; }
section() { printf '\n\n######## [%s] %s\n' "$(ts)" "$*"; }

VM_IP=""
HOST_IP=""
NET4=""
VM_V6=""
V6PFX=""
BASE=""

# mask_str che một chuỗi (stdin) theo mọi thứ đã biết, in ra stdout. Dòng sed
# cuối che mọi chuỗi dạng khoá sao lưu, kể cả khoá chưa kịp vào danh sách che.
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
  s="${s//"$TMPD"/<tmp>}"
  s="${s//"$HOME"/~}"
  s="${s//"$USER"/<user>}"
  while IFS= read -r p; do
    [ -n "$p" ] && s="${s//"$p"/<bí mật, đã che>}"
  done <"$SECRETS"
  s="$(sed -E -e 's/tpan-key-1-[a-z2-7]{52}/<khoá sao lưu, đã che>/g' -e 's/inet6 [0-9a-f:]{3,}/inet6 <v6>/g' <<<"$s")"
  printf '%s' "$s"
}
mask() { OUT="$(mask_str <<<"$OUT")"; }

# ---------------------------------------------------------------- lớp bọc lệnh máy ảo
# Chép từ e2e-m18.sh: output của lệnh ghi vào file TRONG máy ảo, ngắn thì in về
# trong cùng lần gọi, dài thì lấy về bằng multipass transfer (multipass exec
# trên Mac có lúc treo khi output dài). multipass 1.17 không gửi EOF khi stdin
# là /dev/null: dùng một file thường rỗng.
CAP_MAX=4000
CAP_MERGED='f=$HOME/.e2e-cap; (umask 077; : >"$f"); "$@" >"$f" 2>&1; rc=$?; n=$(wc -c <"$f"); [ "$n" -le '"$CAP_MAX"' ] && cat "$f"; printf "\n__E2E_CAP rc=%s n=%s e=0\n" "$rc" "$n"'
CAP_SPLIT='f=$HOME/.e2e-cap; (umask 077; : >"$f"; : >"$f.err"); "$@" >"$f" 2>"$f.err"; rc=$?; printf "__E2E_CAP rc=%s n=%s e=%s\n" "$rc" "$(wc -c <"$f")" "$(wc -c <"$f.err")"'
STDIN_EMPTY="$TMPD/stdin-rong"
: >"$STDIN_EMPTY"
cap_fetch() {
  local _
  for _ in 1 2 3; do
    multipass transfer "$VMNAME:$1" "$2" </dev/null >/dev/null 2>&1 && return 0
    sleep 1
  done
  return 1
}
# vm_cap <exec|root> <lệnh...>: output gộp ở $TMPD/run.out, mã thoát ở RC.
vm_cap() {
  local how=$1 line n
  shift
  [ "$how" = root ] && set -- sudo "$@"
  multipass exec "$VMNAME" -- bash -c "$CAP_MERGED" _ "$@" <"$STDIN_EMPTY" >"$TMPD/cap.raw" 2>&1
  RC=$?
  line="$(tr -d '\r' <"$TMPD/cap.raw" | grep -a '^__E2E_CAP rc=' | tail -1)"
  if [ -z "$line" ]; then
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
# vm_cap2 <exec|root> <lệnh...>: stdout ở $TMPD/cap.out, stderr ở $TMPD/cap.err (lệnh --json).
vm_cap2() {
  local how=$1 line n e
  shift
  [ "$how" = root ] && set -- sudo "$@"
  : >"$TMPD/cap.out"
  : >"$TMPD/cap.err"
  multipass exec "$VMNAME" -- bash -c "$CAP_SPLIT" _ "$@" <"$STDIN_EMPTY" >"$TMPD/cap.raw" 2>&1
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

# runq chạy lệnh, giữ output thô ở $TMPD/run.out, KHÔNG in.
runq() {
  RUNQ_CMD="$(mask_str <<<"$*")"
  RUNQ_TS="$(ts)"
  if [ "$1" = "$VM" ] && { [ "${2:-}" = exec ] || [ "${2:-}" = root ]; }; then
    shift
    vm_cap "$@"
    return
  fi
  "$@" </dev/null >"$TMPD/run.out" 2>&1
  RC=$?
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
note() { printf '  -- GHI NHẬN: %s\n' "$*"; }
P0=0
F0=0
part_begin() { P0=$PASSES F0=$FAILS; section "PHẦN $*"; }
part_end() { printf '\n==== Phần %s: %s đạt, %s không đạt\n' "$1" "$((PASSES - P0))" "$((FAILS - F0))"; }

rc_is() { [ "$RC" -eq "$1" ]; }
has() { grep -qE -- "$1" <<<"$OUT"; }
hasF() { grep -qF -- "$1" <<<"$OUT"; }
job_ok() { [ "$RC" = 0 ] && grep -q '^Job .*: thành công' <<<"$OUT"; }
jraw() { jq -e "$@" "$TMPD/run.json" >/dev/null 2>&1; }
t_ok() { [ "$RC" = 0 ] && jraw "$@"; }
no_raw_secret() { ! grep -qF -f "$SECRETS" "$TMPD/run.out"; }

vexec() { run "$VM" exec "$@"; }
vroot() { run "$VM" root "$@"; }

add_secret() {
  [ -n "${1:-}" ] || return 0
  grep -qxF -- "$1" "$SECRETS" || printf '%s\n' "$1" >>"$SECRETS"
}
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
dbpw() {
  "$VM" root bash -c 'cd / && runuser -u "$2" -- wp config get DB_PASSWORD --path="/var/www/$1/public" 2>/dev/null' _ "$1" "$(user_of "$1")" <"$STDIN_EMPTY" | tr -d '\r'
}

# json_secret <tên> <biểu thức jq> "$VM" root|exec <lệnh...>: lệnh --json trong
# máy ảo; bí mật theo biểu thức jq vào $TMPD/pw.<tên> trước khi in.
JS_PW=""
json_secret() {
  local name=$1 expr=$2 pw
  shift 2
  RUNQ_CMD="$(mask_str <<<"$*")"
  RUNQ_TS="$(ts)"
  shift # bỏ "$VM"
  vm_cap2 "$@"
  tr -d '\r' <"$TMPD/cap.out" >"$TMPD/run.json"
  pw="$(jq -r "$expr // empty" "$TMPD/run.json" 2>/dev/null)"
  [ -n "$pw" ] && save_pw "$name" "$pw"
  tr -d '\r' <"$TMPD/cap.err" | cat - "$TMPD/run.json" >"$TMPD/run.out"
  show_last
  JS_PW="$pw"
}
# vjson <lệnh ... --json> (bằng root): stdout THÔ ở $TMPD/run.json; in cả hai luồng (đã che).
vjson() {
  RUNQ_CMD="$(mask_str <<<"$VM root $*")"
  RUNQ_TS="$(ts)"
  vm_cap2 root "$@"
  tr -d '\r' <"$TMPD/cap.out" >"$TMPD/run.json"
  tr -d '\r' <"$TMPD/cap.err" | cat - "$TMPD/run.json" >"$TMPD/run.out"
  show_last
}

# harvest_env: đọc hai khoá của kho thử (/root/s3thu.env) vào danh sách che và
# vào $TMPD/pw.s3ak, $TMPD/pw.s3sk (0600) cho trình duyệt. KHÔNG in.
harvest_env() {
  local ak sk
  runq "$VM" root bash -c '. "$1" && printf "%s\n%s\n" "$S3_ACCESS_KEY" "$S3_SECRET_KEY"' _ "$VGW_ENV"
  ak="$(tr -d '\r' <"$TMPD/run.out" | sed -n 1p)"
  sk="$(tr -d '\r' <"$TMPD/run.out" | sed -n 2p)"
  [[ "$ak" =~ ^[^[:space:]]{12,}$ ]] && save_pw s3ak "$ak"
  [[ "$sk" =~ ^[^[:space:]]{8,}$ ]] && save_pw s3sk "$sk"
  printf '\n[%s] (đọc hai khoá trong %s vào danh sách che; không in giá trị; mã thoát %s)\n' "$RUNQ_TS" "$VGW_ENV" "$RC"
}
# harvest_backup_key: khoá sao lưu (sinh lần đầu đặt đích) vào danh sách che. KHÔNG in.
harvest_backup_key() {
  local k
  runq "$VM" root tpan backup key show --reveal --json
  k="$(tr -d '\r' <"$TMPD/run.out" | jq -r '.key // empty' 2>/dev/null)"
  add_secret "$k"
  printf '\n[%s] (tpan backup key show --reveal --json: khoá sao lưu vào danh sách che, không in; có khoá: %s; mã thoát %s)\n' \
    "$RUNQ_TS" "$([ -n "$k" ] && echo có || echo không)" "$RC"
}

# ---------------------------------------------------------------- máy ảo

addrs() {
  VM_IP="$("$VM" ip </dev/null)"
  NET4="${VM_IP%.*}."
  HOST_IP="$("$VM" exec bash -c 'echo "${SSH_CLIENT%% *}"' <"$STDIN_EMPTY" | tr -d '\r')"
  local _
  for _ in $(seq 1 30); do
    VM_V6="$("$VM" exec bash -c 'ip -o -6 addr show scope global | awk "{print \$4}" | cut -d/ -f1' <"$STDIN_EMPTY" | tr -d '\r')"
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

# push_tools: chép scripts/e2e-m18/*.sh, *.py vào $RT (qua /tmp, install bằng root).
push_tools() {
  local f n=0
  for f in "$TOOLS"/*.sh "$TOOLS"/*.py; do
    "$VM" push "$f" "/tmp/e2e-m18-${f##*/}" </dev/null >/dev/null 2>&1 && n=$((n + 1))
  done
  vroot bash -c 'install -d -m 0700 "$1" && for f in /tmp/e2e-m18-*; do install -m 0700 "$f" "$1/${f#/tmp/e2e-m18-}"; rm -f "$f"; done; ls "$1"' _ "$RT"
  ok "(đồ nghề) $n script của $TOOLS đã chép vào $RT" bash -c '[ "$2" -gt 0 ] && [ "$(grep -cE "\.(sh|py)$" <<<"$1")" -eq "$2" ]' _ "$OUT" "$n"
}

site_view() { vroot bash "$RT/site.sh" xem "$1"; }
jobs_py() { vroot python3 "$RT/jobs.py" "$@"; }
job_mark() { "$VM" root python3 "$RT/jobs.py" mark <"$STDIN_EMPTY" | tr -d '\r' | sed -nE 's/^moc ([0-9]+)$/\1/p'; }
# two_jobs: output của "jobs.py since" có đúng backup.pull rồi backup.restore, cả hai thành công.
two_jobs() { [ "$(grep -E '^job ' <<<"$OUT" | awk '$4 == "backup.pull" || $4 == "backup.restore" {printf "%s %s;", $4, $3}')" = "backup.pull succeeded;backup.restore succeeded;" ]; }

bk_list() {
  vjson tpan backup list "$1" --json
  cp "$TMPD/run.json" "$TMPD/list.json"
}
bkq() { jq -e "$@" "$TMPD/list.json" >/dev/null 2>&1; }
newest_full() { jq -r '[.[] | select(.kind == "full")][0].id // empty' "$TMPD/list.json" 2>/dev/null; }

# wait_panel: chờ panel trả lời ở cổng 8443 từ Mac (tối đa 60 giây). Chứng chỉ
# tự ký: -k chỉ ở phép chờ này; trình duyệt đi bằng --ignore-certificate-errors.
wait_panel() {
  local _
  for _ in $(seq 1 60); do
    [ "$(curl -sk -m 5 -o /dev/null -w '%{http_code}' "$BASE/api/v1/health" </dev/null 2>/dev/null)" = 200 ] && return 0
    sleep 1
  done
  return 1
}

# browser <mô tả> <cờ của bộ m18-that...>: chạy chương trình trình duyệt; mỗi
# dòng "=> ĐẠT"/"=> KHÔNG ĐẠT" nó in được tính vào tổng. Mật khẩu và hai khoá
# đi qua biến môi trường, không vào dòng lệnh. Output giữ ở $TMPD/br.out cho
# tới browser_report (để kịp đọc bí mật mới vào danh sách che trước khi in).
browser_run() {
  printf '\n[%s] $ anh-readme %s   (Chrome không giao diện trên Mac, panel <panel>)\n' "$(ts)" "$(mask_str <<<"$*")"
  TPAN_PANEL_PASSWORD="$(cat "$TMPD/pw.admin")" TPAN_S3_ACCESS="$(cat "$TMPD/pw.s3ak" 2>/dev/null)" TPAN_S3_SECRET="$(cat "$TMPD/pw.s3sk" 2>/dev/null)" \
    "$ANH" -panel "$BASE" -debug-port "$DEBUG_PORT" -out "$TMPD/anh-out" "$@" </dev/null >"$TMPD/br.out" 2>&1
  RC=$?
}
browser_report() {
  local desc=$1 p f
  OUT="$(tr -d '\r' <"$TMPD/br.out")"
  mask
  printf '%s\n[%s] (mã thoát %s)\n' "$OUT" "$(ts)" "$RC"
  p="$(grep -c '^  => ĐẠT:' <<<"$OUT")"
  f="$(grep -c '^  => KHÔNG ĐẠT:' <<<"$OUT")"
  PASSES=$((PASSES + p))
  FAILS=$((FAILS + f))
  if [ "$RC" -ne 0 ] && [ "$f" -eq 0 ]; then
    ok "$desc: chương trình trình duyệt chạy hết" false
  fi
}

# ================================================================ PHẦN A

AK=""
part_a() {
  part_begin "A: máy sạch, tpan của cây, setup, stack nginx, một site, kho thử versitygw"
  run git log --oneline -1
  run git status --short
  run go build -o "$ANH" ./scripts/anh-readme
  ok "build chương trình trình duyệt (scripts/anh-readme)" rc_is 0
  reset_vm
  if [ "${E2E_DUNG_BIN:-}" = 1 ]; then
    # Chỉ để thử phần máy ảo khi web/ đang dựng dở và không build được.
    note "E2E_DUNG_BIN=1: không build, chép file chạy đang có trong bin/ (scripts/vm.sh deploy)"
    run "$VM" deploy
    ok "build tpan của cây bằng make vm-deploy (lần chạy này dùng bin/ có sẵn: E2E_DUNG_BIN=1)" false
  else
    run make vm-deploy DATE="$BUILD_DATE"
  fi
  ok "make vm-deploy: build và chép tpan $VER vào máy ảo" rc_is 0
  [ "$RC" -eq 0 ] || bail "không build hoặc không chép được tpan"
  vexec tpan version
  ok "tpan version là $VER" has "^tpan $VER "
  push_tools

  json_secret admin '.admin_password' "$VM" root tpan setup --yes --json
  ok "tpan setup: thoát 0, job thành công" t_ok '.report.job.status == "succeeded"'
  ok "mật khẩu quản trị panel in một lần (đã che)" bash -c '[ -n "$1" ] && [ "$(grep -oF -- "$1" "$2" | wc -l)" -eq 1 ]' _ "$JS_PW" "$TMPD/run.out"
  [ -s "$TMPD/pw.admin" ] || bail "không có mật khẩu quản trị panel"
  vroot tpan stack install --web nginx --yes
  ok "stack install --web nginx: job thành công" job_ok

  local pw
  runq "$VM" root tpan site add "$A" --email "$EMAIL" --yes
  pw="$(tr -d '\r' <"$TMPD/run.out" | sed -nE 's/^  Mật khẩu: +([A-Za-z0-9]{32})$/\1/p' | head -1)"
  add_secret "$pw"
  add_secret "$(dbpw "$A")"
  show_last
  ok "site add $A: thoát 0, job thành công" job_ok
  site_view "$A"
  ok "site $A trả 200" has '^trang-chu=200 '

  wait_panel
  ok "panel trả lời ở https://<vm>:8443 (gọi từ Mac)" rc_is 0

  section "A: (đồ nghề) versitygw trong máy ảo, khoá sinh tại chỗ"
  vroot bash "$RT/versitygw.sh"
  ok "versitygw: .deb khớp sha256 của checksums.txt, dịch vụ versitygw@tpan chạy" bash -c '[ "$2" = 0 ] && grep -qE "versitygw_1\.11\.0_linux_[a-z0-9]+\.deb: OK$" <<<"$1" && grep -qx "versitygw@tpan: active" <<<"$1"' _ "$OUT" "$RC"
  harvest_env
  AK="$(cat "$TMPD/pw.s3ak" 2>/dev/null)"
  ok "(đồ nghề) đọc được hai khoá của versitygw vào danh sách che (access key từ 12 ký tự)" bash -c '[ -s "$1/pw.s3ak" ] && [ -s "$1/pw.s3sk" ]' _ "$TMPD"
  vjson tpan backup target show --json
  ok "trước phần B: chưa có đích (configured: false)" jraw '.configured == false'
  part_end A
}

# ================================================================ PHẦN B

part_b() {
  part_begin "B: đặt đích qua giao diện (thẻ Đích ngoài máy), thử kết nối"
  if [ "${E2E_DICH_BANG_CLI:-}" = 1 ]; then
    note "E2E_DICH_BANG_CLI=1: đặt đích bằng tpan backup target set thay cho trình duyệt (chỉ để thử phần máy ảo)"
    json_secret bk '.backup_key' "$VM" root bash -c '. "$1" && printf "%s\n%s\n" "$S3_ACCESS_KEY" "$S3_SECRET_KEY" | tpan backup target set --endpoint "$S3_ENDPOINT" --region "$S3_REGION" --bucket "$S3_BUCKET" --keys-stdin --yes --json' _ "$VGW_ENV"
    ok "(thay thế bằng CLI) target set: job backup.target.set thành công" t_ok '.report.job.status == "succeeded"'
    ok "phần B đặt đích qua giao diện (lần chạy này dùng CLI: E2E_DICH_BANG_CLI=1)" false
  else
    browser_run -bo m18-that -buoc dich -s3-endpoint http://127.0.0.1:7070 -s3-region vn-thu-1 -s3-bucket tpan-thu
    harvest_backup_key
    browser_report "đặt đích"
  fi

  section "B: kiểm trong máy ảo"
  vjson tpan backup target show --json
  ok "target show --json: configured: true, endpoint, vùng, bucket đúng như đã gõ" jraw '.configured == true and .endpoint == "http://127.0.0.1:7070" and .region == "vn-thu-1" and .bucket == "tpan-thu"'
  ok "target show --json: access_key_tail là bốn ký tự cuối của access key" jraw --arg t "${AK: -4}" '.access_key_tail == $t'
  ok "target show --json: không có bí mật nào" no_raw_secret
  vroot stat -c '%U:%G %a %n' /etc/tpan/s3.json
  ok "/etc/tpan/s3.json là root:root 600" has '^root:root 600 /etc/tpan/s3\.json$'
  if [ "${E2E_DICH_BANG_CLI:-}" != 1 ]; then
    # S5 của docs/thay-doi/M18-thiet-ke.md: đặt đích qua API không sinh khoá
    # sao lưu (chỉ CLI sinh, in một lần); thẻ đích chỉ cách sinh bằng lệnh.
    vroot bash -c 'if [ -e /etc/tpan/backup.key ]; then echo "co backup.key"; else echo "khong backup.key"; fi'
    ok "đặt đích qua giao diện không sinh khoá sao lưu (S5): không có /etc/tpan/backup.key, has_backup_key=false" \
      bash -c 'grep -qx "khong backup.key" <<<"$1" && jq -e ".has_backup_key == false" "$2" >/dev/null' _ "$OUT" "$TMPD/run.json"
  fi
  jobs_py last backup.target.set
  ok "tpan.db: job backup.target.set mới nhất thành công" has '^job [0-9a-f]+ succeeded backup\.target\.set '
  if [ "${E2E_DICH_BANG_CLI:-}" != 1 ]; then
    jobs_py last backup.target.test
    ok "tpan.db: job backup.target.test (nút Thử kết nối) thành công" has '^job [0-9a-f]+ succeeded backup\.target\.test '
  fi
  part_end B
}

# ================================================================ PHẦN C

ID_OLD=""
part_c() {
  part_begin "C: tạo một bản chỉ còn trên kho bằng CLI (site $A)"
  vroot bash "$RT/site.sh" moc "$A" moc-cu
  vjson tpan backup create "$A" --yes --json
  ok "backup create $A: thoát 0, job thành công" t_ok '.job.status == "succeeded"'
  bk_list "$A"
  ID_OLD="$(newest_full)"
  ok "bản $ID_OLD đã tự lên kho: local=true, remote=stored" bkq --arg i "$ID_OLD" '[.[] | select(.id == $i)][0] | .local == true and .remote == "stored"'
  vroot bash "$RT/site.sh" moc "$A" moc-moi
  vroot tpan backup settings --keep 1 --yes
  ok "backup settings --keep 1: job thành công" job_ok
  vjson tpan backup create "$A" --yes --json
  ok "backup create $A lần hai: thoát 0, job thành công" t_ok '.job.status == "succeeded"'
  vjson tpan backup list --json
  cp "$TMPD/run.json" "$TMPD/list.json"
  ok "backup list --json: bản cũ $ID_OLD có local: false, remote: \"stored\"" bkq --arg i "$ID_OLD" '[.[] | select(.id == $i)][0] | .local == false and .remote == "stored"'
  vroot tpan backup settings --keep 3 --yes
  ok "backup settings --keep 3: job thành công (bản tải về không bị dọn lại khi khôi phục)" job_ok
  site_view "$A"
  ok "site $A trả 200, file đánh dấu là moc-moi" bash -c 'grep -q "^trang-chu=200 " <<<"$1" && grep -qx "moc=200 moc-moi" <<<"$1"' _ "$OUT"
  part_end C
}

# ================================================================ PHẦN D

part_d() {
  part_begin "D: khôi phục bản chỉ còn trên kho qua giao diện (tải về máy, rồi khôi phục)"
  local mark
  mark="$(job_mark)"
  printf 'Mốc job trước khi bấm: %s\n' "$mark"
  browser_run -bo m18-that -buoc khoi-phuc -site "$A"
  browser_report "khôi phục từ kho"

  section "D: kiểm trong máy ảo"
  site_view "$A"
  ok "site $A trả 200" has '^trang-chu=200 '
  ok "file đánh dấu về đúng nội dung cũ (moc-cu)" has '^moc=200 moc-cu$'
  jobs_py since "$mark"
  ok "Lịch sử: job backup.pull rồi backup.restore, cả hai thành công" two_jobs
  bk_list "$A"
  ok "bản $ID_OLD về máy: local=true, vẫn stored" bkq --arg i "$ID_OLD" '[.[] | select(.id == $i)][0] | .local == true and .remote == "stored"'
  part_end D
}

# ================================================================ PHẦN E

leak_check() {
  [ -n "${E2E_LOG:-}" ] && [ -f "$E2E_LOG" ] || return 0
  section "PHẦN E: bí mật, IP, thư mục nhà có lọt vào chính file log không"
  local n=0 s
  while IFS= read -r s; do
    [ -n "$s" ] || continue
    n=$((n + $(grep -cF -- "$s" "$E2E_LOG")))
  done <"$SECRETS"
  printf 'Số bí mật đã gặp: %s; số dòng của log chứa một trong số đó: %s\n' "$(grep -c . "$SECRETS")" "$n"
  ok "log không chứa khoá S3, khoá sao lưu hay mật khẩu nào" test "$n" -eq 0
  ok "log không chứa chuỗi nào dạng khoá sao lưu" bash -c '! grep -qE "tpan-key-1-[a-z2-7]{52}" "$1"' _ "$E2E_LOG"
  ok "log không chứa IP máy ảo (IPv4)" bash -c '[ -z "$1" ] || ! grep -qF -- "$1" "$2"' _ "$VM_IP" "$E2E_LOG"
  ok "log không chứa mạng Multipass" bash -c '[ -z "$1" ] || ! grep -qF -- "$1" "$2"' _ "$NET4" "$E2E_LOG"
  ok "log không chứa IPv6 của máy ảo" bash -c '[ -z "$1" ] || ! grep -qiF -- "$1" "$2"' _ "$V6PFX" "$E2E_LOG"
  ok "log không chứa IP của Mac" bash -c '[ -z "$1" ] || ! grep -qF -- "$1" "$2"' _ "$HOST_IP" "$E2E_LOG"
  ok "log không chứa thư mục nhà hay tên user Mac" bash -c '! grep -qF -- "$1" "$3" && ! grep -qF -- "$2" "$3"' _ "$HOME" "$USER" "$E2E_LOG"
}

bail() {
  printf '  => KHÔNG ĐẠT: DỪNG KỊCH BẢN: %s\n' "$*"
  FAILS=$((FAILS + 1))
  leak_check
  section "TỔNG KẾT: $PASSES khẳng định đạt, $FAILS khẳng định không đạt (dừng giữa chừng)"
  exit 1
}

PARTS=("$@")
[ ${#PARTS[@]} -gt 0 ] || PARTS=(a b c d)
for p in "${PARTS[@]}"; do
  case "$p" in
    a | b | c | d) ;;
    *) printf 'phần chỉ nhận a, b, c, d (đang là %s)\n' "$p" >&2; exit 2 ;;
  esac
done

printf 'e2e giao diện M18. tpan của cây: %s; máy ảo: %s; các phần: %s\n' "$VER" "$VMNAME" "${PARTS[*]}"
for p in "${PARTS[@]}"; do
  # Phần chạy riêng (không có a trước nó) vẫn cần địa chỉ máy ảo, chương trình trình duyệt, mật khẩu.
  if [ -z "$VM_IP" ] && [ "$p" != a ]; then
    bail "phần $p cần biến của phần A trong cùng lần chạy (mật khẩu admin, khoá kho): chạy kèm a"
  fi
  "part_$p"
done

leak_check
section "TỔNG KẾT: $PASSES khẳng định đạt, $FAILS khẳng định không đạt"
[ "$FAILS" -eq 0 ]
