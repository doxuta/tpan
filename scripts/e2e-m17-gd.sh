#!/usr/bin/env bash
# Lệnh trong máy ảo viết trong nháy đơn, biến của chúng mở ra trong máy ảo:
# shellcheck disable=SC2016
# e2e GIAO DIỆN của mốc M17 (lồng theo site, mức tối đa theo gói, số liệu, thử
# tấn công) trên máy ảo (tên trong file .tpan-vm; không có thì tpan-test).
# Chạy trên Mac, trong thư mục repo, từ máy sạch:
#
#   make e2e-m17-gd     (log vào tmp-phien/ rồi chép vào docs/bang-chung/M17-giao-dien.log)
#
# Gõ thêm tên phần để chỉ chạy vài phần, theo thứ tự (mặc định: a b c d e f):
#   bash scripts/e2e-m17-gd.sh a b c
#
# Như e2e giao diện M16, M18: các khẳng định chính do TRÌNH DUYỆT làm. Chrome
# không giao diện trên Mac (scripts/anh-readme, bộ m17-that) mở panel thật của
# máy ảo qua https và bấm như người dùng. Script kiểm lại trong máy ảo bằng
# lệnh độc lập với giao diện (tpan cage status --json, systemctl show của
# slice, trang của site).
#
# Kịch bản:
#   A  máy sạch; build và chép tpan của cây (make vm-deploy), tpan setup, stack
#      nginx; bằng CLI: gói "long" CHƯA bật lồng, user "khach" dùng gói đó, site
#      A của khach (ở ngoài lồng vì gói chưa bật lồng).
#   B  trình duyệt -buoc bat-long: Sửa gói long, chọn lồng, CPU 50, RAM 200,
#      40 tiến trình, 3 worker, Lưu. Trong máy ảo: package list --json đúng;
#      cage status: A chờ vào lồng (can-vao); A vẫn ngoài lồng.
#   C  trình duyệt -buoc vao-long: trang của A, "Vào lồng", xem trước, chạy.
#      Trong máy ảo: A trong lồng; slice có MemoryMax 200 MiB, CPUQuota 50%,
#      TasksMax 40; dịch vụ PHP của lồng chạy bằng user của A; A trả 200.
#   D  trình duyệt -buoc sua-goi: Sửa gói long, RAM 256, xem trước (cage.sync),
#      chạy. Trong máy ảo: slice có MemoryMax 256 MiB; A trả 200.
#   E  trình duyệt -buoc tan-cong: màn Bảo mật, dòng "Thử tấn công", chạy cả
#      hai lượt. Trong máy ảo: không còn user, thư mục hay unit nào của site
#      thử; tpan health không có probe.leftover lỗi.
#   F  trình duyệt -buoc ra-long: trang của A, "Ra khỏi lồng". Trong máy ảo: A
#      ngoài lồng, không còn file tpan-cage-*, A trả 200.
#   G  (luôn chạy cuối, khi có E2E_LOG) quét chính file log: không mật khẩu,
#      IP máy ảo, IP của Mac, thư mục nhà.
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
RT=/root/e2e-m18 # đồ nghề trong máy ảo (dùng lại site.sh, jobs.py của scripts/e2e-m18)
FAILS=0
PASSES=0
OUT=""
RC=0
EMAIL=admin@example.com
A=a.example.com
VER="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
DEBUG_PORT=18192 # cổng DevTools riêng, không đụng anh-m16 (18162), e2e-m16 (18172), e2e-m18-gd (18182)

mkdir -p tmp-phien
TMPD="$(mktemp -d "$PWD/tmp-phien/e2e-m17-gd.XXXXXX")"
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

wait_panel() {
  local _
  for _ in $(seq 1 60); do
    [ "$(curl -sk -m 5 -o /dev/null -w '%{http_code}' "$BASE/api/v1/health" </dev/null 2>/dev/null)" = 200 ] && return 0
    sleep 1
  done
  return 1
}

# browser <mô tả> <cờ của bộ m17-that...>: chạy chương trình trình duyệt; mỗi
# dòng "=> ĐẠT"/"=> KHÔNG ĐẠT" nó in được tính vào tổng. Mật khẩu và hai khoá
# đi qua biến môi trường, không vào dòng lệnh. Output giữ ở $TMPD/br.out cho
# tới browser_report (để kịp đọc bí mật mới vào danh sách che trước khi in).
browser_run() {
  printf '\n[%s] $ anh-readme %s   (Chrome không giao diện trên Mac, panel <panel>)\n' "$(ts)" "$(mask_str <<<"$*")"
  TPAN_PANEL_PASSWORD="$(cat "$TMPD/pw.admin")" \
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


# ---------------------------------------------------------------- việc riêng của M17

PKG=long
UK=khach
SLICE="tpan-$(slug_of "$A").slice"
SVC="tpan-cage-$(slug_of "$A")-php.service"

# cage_row <domain>: dòng của site trong `tpan cage status --json`, ở $TMPD/run.json.
cage_row() {
  vjson tpan cage status "$1" --json
  jq -c --arg d "$1" 'if type == "array" then (.[] | select(.domain == $d)) else . end' "$TMPD/run.json" >"$TMPD/row.json" 2>/dev/null && mv "$TMPD/row.json" "$TMPD/run.json"
}
jout() { jq -e "$@" "$TMPD/run.json" >/dev/null 2>&1; }
# slice_show: ba trần đang áp trên slice của A (systemctl show, đọc từ systemd).
slice_show() {
  vroot systemctl show "$SLICE" -p MemoryMax -p CPUQuotaPerSecUSec -p TasksMax -p MemorySwapMax
}

# ================================================================ PHẦN A

part_a() {
  part_begin "A: máy sạch, tpan của cây, setup, stack nginx; gói $PKG chưa bật lồng, user $UK, site $A"
  run git log --oneline -1
  run git status --short
  run go build -o "$ANH" ./scripts/anh-readme
  ok "build chương trình trình duyệt (scripts/anh-readme)" rc_is 0
  reset_vm
  run make vm-deploy DATE="$BUILD_DATE"
  ok "make vm-deploy: build và chép tpan $VER vào máy ảo" rc_is 0
  [ "$RC" -eq 0 ] || bail "không build hoặc không chép được tpan"
  vexec tpan version
  ok "tpan version là $VER" has "^tpan $VER "
  push_tools

  json_secret admin '.admin_password' "$VM" root tpan setup --yes --json
  ok "tpan setup: thoát 0, job thành công" t_ok '.report.job.status == "succeeded"'
  [ -s "$TMPD/pw.admin" ] || bail "không có mật khẩu quản trị panel"
  vroot tpan stack install --web nginx --yes
  ok "stack install --web nginx: job thành công" job_ok

  vroot tpan package add "$PKG" --max-sites 3 --max-dbs 3 --yes
  ok "package add $PKG (chưa bật lồng, chưa có mức tối đa)" rc_is 0
  json_secret k '.password' "$VM" root tpan user add "$UK" --full-name "Khach" --email k@example.com --package "$PKG" --yes --json
  ok "user add $UK (gói $PKG): thoát 0, mật khẩu sinh một lần (đã che)" bash -c '[ "$1" = 0 ] && [ -n "$2" ]' _ "$RC" "$JS_PW"

  local pw
  runq "$VM" root tpan site add "$A" --email "$EMAIL" --owner "$UK" --yes
  pw="$(tr -d '\r' <"$TMPD/run.out" | sed -nE 's/^  Mật khẩu: +([A-Za-z0-9]{32})$/\1/p' | head -1)"
  add_secret "$pw"
  add_secret "$(dbpw "$A")"
  show_last
  ok "site add $A --owner $UK: thoát 0, job thành công" job_ok
  site_view "$A"
  ok "site $A trả 200" has '^trang-chu=200 '
  cage_row "$A"
  ok "cage status --json: $A ngoài lồng, gói $PKG chưa muốn lồng (state ngoai)" jout '.caged == false and .want == false and .state == "ngoai"'

  wait_panel
  ok "panel trả lời ở https://<vm>:8443 (gọi từ Mac)" rc_is 0
  part_end A
}

# ================================================================ PHẦN B

part_b() {
  part_begin "B: bật lồng và đặt mức tối đa cho gói $PKG qua màn Gói"
  browser_run -bo m17-that -buoc bat-long -goi "$PKG"
  browser_report "bật lồng cho gói"

  section "B: kiểm trong máy ảo"
  vjson tpan package list --json
  ok "package list --json: $PKG có cage true, CPU 50, RAM 200, 40 tiến trình, 3 worker" jout --arg p "$PKG" '[.[] | (.package // .) | select(.name == $p)][0] | .cage == true and .cpu_percent == 50 and .ram_mb == 200 and .tasks == 40 and .php_workers == 3'
  cage_row "$A"
  ok "cage status --json: $A chờ vào lồng (want true, caged false, state can-vao)" jout '.caged == false and .want == true and .state == "can-vao"'
  part_end B
}

# ================================================================ PHẦN C

part_c() {
  part_begin "C: đưa $A vào lồng từ trang của site"
  browser_run -bo m17-that -buoc vao-long -site "$A"
  browser_report "vào lồng"

  section "C: kiểm trong máy ảo"
  cage_row "$A"
  ok "cage status --json: $A trong lồng, đủ file hàng rào (state trong, files true)" jout '.caged == true and .files == true and .state == "trong"'
  slice_show
  ok "slice của $A: MemoryMax 200 MiB, swap 0, CPU 50% (500ms mỗi giây), TasksMax 40" bash -c 'grep -qx "MemoryMax=209715200" <<<"$1" && grep -qx "MemorySwapMax=0" <<<"$1" && grep -qx "CPUQuotaPerSecUSec=500ms" <<<"$1" && grep -qx "TasksMax=40" <<<"$1"' _ "$OUT"
  site_view "$A"
  ok "site $A trả 200 (qua socket của lồng)" has '^trang-chu=200 '
  vroot systemctl show "$SVC" -p User -p ActiveState
  ok "dịch vụ PHP của lồng chạy bằng user của $A ($(user_of "$A"))" bash -c 'grep -qx "User=$2" <<<"$1" && grep -qx "ActiveState=active" <<<"$1"' _ "$OUT" "$(user_of "$A")"
  part_end C
}

# ================================================================ PHẦN D

part_d() {
  part_begin "D: sửa mức RAM của gói $PKG khi có site trong lồng (bản xem trước, job cage.sync)"
  browser_run -bo m17-that -buoc sua-goi -goi "$PKG"
  browser_report "sửa gói có site trong lồng"

  section "D: kiểm trong máy ảo"
  slice_show
  ok "slice của $A: MemoryMax 256 MiB (mức mới của gói đã áp)" bash -c 'grep -qx "MemoryMax=268435456" <<<"$1"' _ "$OUT"
  site_view "$A"
  ok "site $A vẫn trả 200" has '^trang-chu=200 '
  part_end D
}

# ================================================================ PHẦN E

part_e() {
  part_begin "E: thử tấn công từ màn Bảo mật (hai site thử, ngoài lồng rồi trong lồng)"
  browser_run -bo m17-that -buoc tan-cong
  browser_report "thử tấn công"

  section "E: kiểm trong máy ảo: không còn gì của site thử"
  vroot bash -c 'getent passwd | grep -c "^tp_tpan_probe" ; ls /var/www | grep -c "^tpan-probe-" ; systemctl list-units --all --no-legend "tpan-cage-tpan_probe*" | wc -l'
  ok "không còn user, thư mục, unit nào của site thử (0 0 0)" bash -c '[ "$(tr -s "\n" " " <<<"$1" | sed "s/ *$//")" = "0 0 0" ]' _ "$OUT"
  vjson tpan health --json
  ok "tpan health --json: không có phép probe.leftover lỗi" jout '[.groups[].results[] | select((.id | startswith("probe.leftover")) and .status == "fail")] | length == 0'
  site_view "$A"
  ok "site thật $A vẫn trả 200" has '^trang-chu=200 '
  part_end E
}

# ================================================================ PHẦN F

part_f() {
  part_begin "F: đưa $A ra khỏi lồng từ trang của site"
  browser_run -bo m17-that -buoc ra-long -site "$A"
  browser_report "ra khỏi lồng"

  section "F: kiểm trong máy ảo"
  cage_row "$A"
  ok "cage status --json: $A ngoài lồng, không còn file hàng rào (caged false, files false)" jout '.caged == false and .files == false'
  vroot bash -c 'ls /etc/systemd/system | grep -c "^tpan-cage-" || true'
  ok "không còn file tpan-cage-* trong /etc/systemd/system" bash -c '[ "$(tail -1 <<<"$1")" = 0 ]' _ "$OUT"
  site_view "$A"
  ok "site $A trả 200 (về pool chung)" has '^trang-chu=200 '
  part_end F
}

# ================================================================ PHẦN G

leak_check() {
  [ -n "${E2E_LOG:-}" ] && [ -f "$E2E_LOG" ] || return 0
  section "PHẦN G: mật khẩu, IP, thư mục nhà có lọt vào chính file log không"
  local n=0 s
  while IFS= read -r s; do
    [ -n "$s" ] || continue
    n=$((n + $(grep -cF -- "$s" "$E2E_LOG")))
  done <"$SECRETS"
  printf 'Số bí mật đã gặp: %s; số dòng của log chứa một trong số đó: %s\n' "$(grep -c . "$SECRETS")" "$n"
  ok "log không chứa mật khẩu nào" test "$n" -eq 0
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

# ================================================================ chạy

PARTS=("$@")
[ ${#PARTS[@]} -gt 0 ] || PARTS=(a b c d e f)
for p in "${PARTS[@]}"; do
  case "$p" in
    a | b | c | d | e | f) ;;
    *) printf 'phần chỉ nhận a, b, c, d, e, f (đang là %s)\n' "$p" >&2; exit 2 ;;
  esac
done

printf 'e2e giao diện M17. tpan của cây: %s; máy ảo: %s; các phần: %s\n' "$VER" "$VMNAME" "${PARTS[*]}"
for p in "${PARTS[@]}"; do
  if [ -z "$VM_IP" ] && [ "$p" != a ]; then
    bail "phần $p cần biến của phần A trong cùng lần chạy (mật khẩu admin): chạy kèm a"
  fi
  "part_$p"
done

leak_check
section "TỔNG KẾT: $PASSES khẳng định đạt, $FAILS khẳng định không đạt"
[ "$FAILS" -eq 0 ]
