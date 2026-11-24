#!/usr/bin/env bash
# e2e của mốc M16 (giao diện bản 2; bước 19 ở mục 12 của docs/KE-HOACH.md) trên
# máy ảo (tên trong file .tpan-vm; không có thì tpan-test). Chạy trên Mac, trong
# thư mục repo, từ máy sạch:
#
#   make e2e-m16                     (log in ra màn hình)
#   E2E_LOG=docs/bang-chung/M16.log bash scripts/e2e-m16.sh > docs/bang-chung/M16.log 2>&1
#
# Khác các e2e trước: phần lớn khẳng định ở đây do TRÌNH DUYỆT làm. Chrome không
# giao diện trên Mac (scripts/anh-readme, bộ m16-that) mở panel thật của máy ảo
# qua https và làm như người dùng: gõ vào form đăng nhập, mở từng màn, bấm menu,
# tạo site từ form. Mỗi lần mở màn kiểm: có tiêu đề, không lỗi console (lỗi JS,
# request hỏng như 403, vi phạm CSP), cả trang không cuộn ngang.
#
# Hai bản tpan:
#   bản cũ  v0.2.0, gói đã phát hành ở dist/release/v0.2.0 (không build lại;
#           chưa có thì script tải bằng gh release download)
#   bản A   bản của cây đang đứng (make release)
#   bản B   cùng mã với A, khác tên bản (make release VERSION=<A>-e2e-b): "bản
#           mới" cho phần F bấm nút cập nhật
#
# Kịch bản:
#   0  đóng gói A và B; dựng thư mục đồ nghề.
#   A  máy sạch, cài A bằng install.sh của chính gói A (--web nginx).
#   B  admin đăng nhập qua form, mở mọi màn admin, mở menu tài khoản và một
#      menu ba chấm; số DB ở menu bằng số DB của API; dòng phiên bản cuối menu.
#   C  tạo site tên 36 ký tự từ form Tạo site, bấm chạy trên bản xem trước, chờ
#      job; trong máy ảo site trả 200; tên nằm một dòng trên bản đồ tuyến.
#   D  user thường (tạo bằng CLI, có một site): đăng nhập qua form tới Công cụ,
#      không thấy mục admin, mở mọi màn của mình không lỗi (không request nào
#      403), gõ /users thấy trang "không có quyền".
#   E  máy sạch, cài v0.2.0, tạo site, đăng nhập bằng giao diện cũ; v0.2.0 tự
#      lên A bằng `tpan self-update` của chính nó: cùng cookie vẫn đăng nhập,
#      tải lại trang thấy giao diện bản 2, đăng nhập lại bằng mật khẩu cũ được,
#      site vẫn 200, health không có [lỗi] mới.
#   F  hồi quy M15 trên giao diện mới: bấm thật nút cập nhật A lên B rồi quay
#      về (scripts/anh-readme -bo m15-that).
#
# ĐỒ NGHỀ TEST, CHỈ TRONG MÁY ẢO: nguồn tải là các thư mục phục vụ bằng
# `python3 -m http.server` trên 127.0.0.1 (đơn vị tạm tpan-e2e-http); setting
# update.self_url ghi thẳng bằng scripts/e2e-m15/db.py (như e2e M15).
#
# Đặt E2E_LOG=<file log> để cuối lần chạy script tự tìm trong chính file log
# đó: mọi mật khẩu đã gặp, IP máy ảo, IP của Mac, thư mục nhà và tên user Mac
# (phải là 0 lần).
#
# Output: mỗi lệnh có dấu thời gian, output nguyên văn (đã che) và mã thoát;
# mỗi khẳng định in "=> ĐẠT" hoặc "=> KHÔNG ĐẠT"; cuối mỗi phần và cuối lần
# chạy in số đạt, không đạt. Không dùng set -e.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2

VM=scripts/vm.sh
VMNAME="${TPAN_VM:-$(tr -d '[:space:]' 2>/dev/null <.tpan-vm)}"
VMNAME="${VMNAME:-tpan-test}"
FAILS=0
PASSES=0
OUT=""
RC=0
EMAIL=admin@example.com
SITE_CLI=a.example.com                         # site tạo bằng CLI
SITE_LONG=hoc-truc-tuyen.lop-hoc-a.example.net # 36 ký tự, tạo từ giao diện
SITE_USER=cua-user.example.org                 # site của user thường
UU=khach_m16
OLD_VER=v0.2.0
OLD_DIR="${OLD_DIR:-dist/release/$OLD_VER}"
VER="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
VER_B="$VER-e2e-b"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
STAGE=dist/e2e-m16
E=/home/ubuntu/e2e-m16 # thư mục đồ nghề trong máy ảo
PORT=8016
SRC="http://127.0.0.1:$PORT"
HTTP_UNIT=tpan-e2e-http
DEBUG_PORT="${E2E_DEBUG_PORT:-18172}" # cổng DevTools riêng; nhiều phiên mốc chạy e2e-m16 cùng lúc thì đặt E2E_DEBUG_PORT khác nhau
ARCH=""

TMPD="$(mktemp -d)"
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
BASE=""

# mask_str che một chuỗi (stdin) theo mọi thứ đã biết, in ra stdout.
mask_str() {
  local s p
  s="$(cat)"
  [ -n "$VM_IP" ] && s="${s//"$VM_IP"/<vm>}"
  [ -n "$HOST_IP" ] && s="${s//"$HOST_IP"/<mac>}"
  [ -n "$NET4" ] && s="${s//"$NET4"/<mạng máy ảo>.}"
  s="${s//"$TMPD"/<tmp>}"
  s="${s//"$HOME"/~}"
  s="${s//"$USER"/<user>}"
  while IFS= read -r p; do
    [ -n "$p" ] && s="${s//"$p"/<bí mật, đã che>}"
  done <"$SECRETS"
  printf '%s' "$s"
}
mask() { OUT="$(mask_str <<<"$OUT")"; }

# Lớp bọc lệnh máy ảo (như e2e M15): output ghi vào file trong máy ảo, ngắn thì
# in về trong cùng lần gọi, dài thì lấy về bằng multipass transfer, vì
# `multipass exec` trên Mac có lúc treo giữa chừng khi output dài.
CAP_MAX=4000
CAP_MERGED='f=$HOME/.e2e-cap; (umask 077; : >"$f"); "$@" >"$f" 2>&1; rc=$?; n=$(wc -c <"$f"); [ "$n" -le '"$CAP_MAX"' ] && cat "$f"; printf "\n__E2E_CAP rc=%s n=%s e=0\n" "$rc" "$n"'
CAP_SPLIT='f=$HOME/.e2e-cap; (umask 077; : >"$f"; : >"$f.err"); "$@" >"$f" 2>"$f.err"; rc=$?; printf "__E2E_CAP rc=%s n=%s e=%s\n" "$rc" "$(wc -c <"$f")" "$(wc -c <"$f.err")"'
cap_fetch() {
  local _
  for _ in 1 2 3; do
    multipass transfer "$VMNAME:$1" "$2" </dev/null >/dev/null 2>&1 && return 0
    sleep 1
  done
  return 1
}
vm_cap() {
  local in=$1 how=$2 line n
  shift 2
  [ "$how" = root ] && set -- sudo "$@"
  multipass exec "$VMNAME" -- bash -c "$CAP_MERGED" _ "$@" <"$in" >"$TMPD/cap.raw" 2>&1
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
bail() {
  printf '  => KHÔNG ĐẠT: DỪNG KỊCH BẢN: %s\n' "$*"
  FAILS=$((FAILS + 1))
  leak_check
  section "TỔNG KẾT: $PASSES khẳng định đạt, $FAILS khẳng định không đạt (dừng giữa chừng)"
  exit 1
}
note() { printf '  -- GHI NHẬN: %s\n' "$*"; }
P0=0
F0=0
part_begin() { P0=$PASSES F0=$FAILS; section "PHẦN $*"; }
part_end() { printf '\n==== Phần %s: %s đạt, %s không đạt\n' "$1" "$((PASSES - P0))" "$((FAILS - F0))"; }

rc_is() { [ "$RC" -eq "$1" ]; }
has() { grep -qE -- "$1" <<<"$OUT"; }
hasF() { grep -qF -- "$1" <<<"$OUT"; }
out_is() { [ "$OUT" = "$1" ]; }
job_ok() { [ "$RC" = 0 ] && grep -q '^Job .*: thành công' <<<"$OUT"; }

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
  "$VM" root bash -c 'cd / && runuser -u "$2" -- wp config get DB_PASSWORD --path="/var/www/$1/public" 2>/dev/null' _ "$1" "$(user_of "$1")" </dev/null | tr -d '\r'
}

# ---------------------------------------------------------------- máy ảo

addrs() {
  VM_IP="$("$VM" ip </dev/null)"
  NET4="${VM_IP%.*}."
  HOST_IP="$("$VM" exec bash -c 'echo "${SSH_CLIENT%% *}"' </dev/null | tr -d '\r')"
  BASE="https://$VM_IP:8443"
  printf 'IP máy ảo: <vm>; IP của Mac trong mạng Multipass: <mac> (đều được che trong log)\n'
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

# site_add <domain> [cờ...]: tpan site add, nhặt mật khẩu quản trị WordPress và
# mật khẩu DB của site vào danh sách che trước khi in.
site_add() {
  local d=$1 pw
  shift
  runq "$VM" root tpan site add "$d" --email "$EMAIL" --yes "$@"
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
  shift # bỏ "$VM"
  vm_cap2 "$@"
  pw="$(tr -d '\r' <"$TMPD/cap.out" | jq -r "$expr // empty" 2>/dev/null)"
  [ -n "$pw" ] && save_pw "$name" "$pw"
  cat "$TMPD/cap.err" "$TMPD/cap.out" >"$TMPD/run.out"
  show_last
  JS_PW="$pw"
}

# page <domain>: mã HTTP của trang chủ site, gọi từ trong máy ảo. Kết quả ở OUT.
page() { vexec curl -s -o /dev/null -m 20 -w '%{http_code}\n' --resolve "$1:80:127.0.0.1" "http://$1/"; }
is200() { [ "$OUT" = 200 ]; }

health_errs() {
  vroot tpan health
  grep -E '^\[lỗi\]' <<<"$OUT" | sort >"$1"
  printf 'Số dòng [lỗi] của tpan health: %s\n' "$(grep -c . "$1")"
}
new_errs() { comm -13 "$1" "$2"; }

# wait_panel: chờ panel trả lời ở cổng 8443 (tối đa 60 giây). Chứng chỉ tự ký:
# -k chỉ ở phép chờ này; trình duyệt đi bằng --ignore-certificate-errors.
wait_panel() {
  local _
  for _ in $(seq 1 60); do
    [ "$(curl -sk -m 5 -o /dev/null -w '%{http_code}' "$BASE/api/v1/health" </dev/null 2>/dev/null)" = 200 ] && return 0
    sleep 1
  done
  return 1
}

# browser <mô tả> <cờ của bộ m16-that hoặc m15-that...>: chạy chương trình
# trình duyệt; mỗi dòng "=> ĐẠT"/"=> KHÔNG ĐẠT" nó in được tính vào tổng.
# Mật khẩu đi qua biến môi trường, không vào dòng lệnh.
browser() {
  local desc=$1
  shift
  printf '\n[%s] $ anh-readme %s   (Chrome không giao diện trên Mac, panel <panel>)\n' "$(ts)" "$(mask_str <<<"$*")"
  TPAN_PANEL_PASSWORD="$(cat "$TMPD/pw.admin")" TPAN_USER_PASSWORD="$(cat "$TMPD/pw.u" 2>/dev/null)" \
    "$ANH" -panel "$BASE" -debug-port "$DEBUG_PORT" -out "$TMPD/anh-out" "$@" </dev/null >"$TMPD/br.out" 2>&1
  RC=$?
  browser_report "$desc"
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
  # Chương trình dừng giữa chừng mà không in khẳng định nào hỏng: tính một lần không đạt.
  if [ "$RC" -ne 0 ] && [ "$f" -eq 0 ]; then
    ok "$desc: chương trình trình duyệt chạy hết" false
  fi
}

# ================================================================ PHẦN 0

part_0() {
  part_begin "0: đóng gói bản A và bản B, dựng thư mục đồ nghề"
  run git log --oneline -1
  run git status --short
  if [ ! -f "$OLD_DIR/SHA256SUMS" ]; then
    run gh release download "$OLD_VER" --repo doxuta/tpan --dir "$OLD_DIR"
    ok "tải gói $OLD_VER từ trang phát hành" rc_is 0
  fi
  run bash -c 'cd "$1" && ls -l && shasum -a 256 -c SHA256SUMS' _ "$OLD_DIR"
  ok "gói $OLD_VER đã phát hành: đủ file, sha256 khớp SHA256SUMS" rc_is 0
  run make release VERSION="$VER" DATE="$BUILD_DATE"
  ok "make release (bản A, $VER)" rc_is 0
  [ "$RC" -eq 0 ] || bail "không đóng gói được bản A"
  run make release VERSION="$VER_B" DATE="$BUILD_DATE"
  ok "make release VERSION=$VER_B (bản B, cùng mã, khác tên bản)" rc_is 0
  run go build -o "$ANH" ./scripts/anh-readme
  ok "build chương trình trình duyệt (scripts/anh-readme)" rc_is 0
  [ "$RC" -eq 0 ] || bail "không build được chương trình trình duyệt"
  # shellcheck disable=SC2016
  run bash -c '
    set -e
    st=$1; a=$2; b=$3
    rm -rf dist/e2e-m16
    mkdir -p "$st/rel/a" "$st/rel/b"
    cp "dist/release/$a/"* "$st/rel/a/"
    cp "dist/release/$b/"* "$st/rel/b/"
    cp scripts/e2e-m15/db.py "$st/"
    ls -lR "$st"
  ' _ "$STAGE" "$VER" "$VER_B"
  ok "dựng thư mục đồ nghề $STAGE (gói a, b, db.py)" rc_is 0
  part_end 0
}

# chép gói và đồ nghề sang máy ảo, dựng nguồn tải loopback.
stage_vm() {
  ARCH="$("$VM" exec dpkg --print-architecture </dev/null | tr -d '\r')"
  printf 'Kiến trúc máy ảo: %s\n' "$ARCH"
  run multipass transfer -r "$STAGE" "$VMNAME:/home/ubuntu/"
  ok "chép thư mục đồ nghề sang máy ảo ($E)" rc_is 0
  run multipass transfer -r "$OLD_DIR" "$VMNAME:/home/ubuntu/"
  ok "chép gói $OLD_VER sang máy ảo" rc_is 0
  vroot systemd-run --unit "$HTTP_UNIT" --collect python3 -m http.server "$PORT" --bind 127.0.0.1 --directory "$E/rel"
  sleep 2
  vexec bash -c 'for d in a b; do printf "%s: SHA256SUMS %s, VERSION %s\n" "$d" "$(curl -s -o /dev/null -w "%{http_code}" "$1/$d/SHA256SUMS")" "$(curl -s "$1/$d/VERSION")"; done' _ "$SRC"
  ok "(đồ nghề) nguồn tải loopback trả gói a và b" bash -c '[ "$(grep -c "SHA256SUMS 200" <<<"$1")" -eq 2 ]' _ "$OUT"
}

# ================================================================ PHẦN A

part_a() {
  part_begin "A: máy sạch, cài bản A bằng install.sh của chính gói A"
  reset_vm
  stage_vm
  install_run "$VM" exec bash -c "cd $E/rel/a && sudo bash ./install.sh --web nginx"
  ok "install.sh của bản A thoát 0" rc_is 0
  [ "$RC" -eq 0 ] || bail "không cài được bản A"
  ok "mật khẩu quản trị in đúng một lần (giữ ngoài log)" test "$PW_HITS" -eq 1
  save_pw admin "$PW"
  vexec tpan version
  ok "tpan version là bản A ($VER)" has "^tpan $VER "
  vexec systemctl is-active tpan nginx php8.3-fpm mariadb
  ok "tpan.service và ba dịch vụ của stack đều active" bash -c '[ "$(grep -cx active <<<"$1")" -eq 4 ]' _ "$OUT"
  wait_panel
  ok "panel trả lời ở cổng 8443" rc_is 0
  site_add "$SITE_CLI"
  page "$SITE_CLI"
  ok "$SITE_CLI trả 200" is200
  part_end A
}

# ================================================================ PHẦN B, C, D

part_b() {
  part_begin "B: admin đăng nhập qua form, mở mọi màn admin"
  browser "admin" -bo m16-that -buoc admin
  part_end B
}

part_c() {
  part_begin "C: tạo site tên ${#SITE_LONG} ký tự từ giao diện"
  browser "tạo site" -bo m16-that -buoc tao-site -site "$SITE_LONG"
  add_secret "$(dbpw "$SITE_LONG")"
  page "$SITE_LONG"
  ok "$SITE_LONG (tạo từ giao diện) trả 200 khi gọi trong máy ảo" is200
  vroot tpan site list
  ok "tpan site list có $SITE_LONG" hasF "$SITE_LONG"
  part_end C
}

part_d() {
  part_begin "D: user thường"
  json_secret u '.password' "$VM" root tpan user add "$UU" --full-name "Khach M16" --email khach@example.com --yes --json
  ok "user add $UU: thoát 0, mật khẩu sinh một lần (đã che)" bash -c '[ "$1" = 0 ] && [ -n "$2" ]' _ "$RC" "$JS_PW"
  site_add "$SITE_USER" --owner "$UU"
  browser "user thường" -bo m16-that -buoc user -ten "$UU" -site "$SITE_USER"
  part_end D
}

# ================================================================ PHẦN E

H0=""
part_e() {
  part_begin "E: máy sạch, cài $OLD_VER, đăng nhập bằng giao diện cũ, $OLD_VER tự lên bản A"
  reset_vm
  stage_vm
  install_run "$VM" exec bash -c "cd /home/ubuntu/$OLD_VER && sudo bash ./install.sh --web nginx"
  ok "install.sh $OLD_VER thoát 0" rc_is 0
  [ "$RC" -eq 0 ] || bail "không cài được $OLD_VER"
  save_pw admin "$PW"
  vexec tpan version
  ok "tpan version là $OLD_VER" has "^tpan $OLD_VER "
  wait_panel
  site_add "$SITE_CLI"
  page "$SITE_CLI"
  ok "$SITE_CLI trả 200 trên $OLD_VER" is200
  H0="$TMPD/health.0"
  health_errs "$H0"

  # Trình duyệt chạy nền: đăng nhập bằng giao diện cũ, in "CHỜ NÂNG CẤP", rồi
  # chờ file $TMPD/da-nang. Script nâng cấp trong lúc trình duyệt giữ nguyên
  # tab và cookie, rồi tạo file đó.
  local wait_file="$TMPD/da-nang" pid _
  printf '\n[%s] $ anh-readme -bo m16-that -buoc nang-cap   (chạy nền, panel <panel>)\n' "$(ts)"
  TPAN_PANEL_PASSWORD="$(cat "$TMPD/pw.admin")" "$ANH" -panel "$BASE" -debug-port "$DEBUG_PORT" -out "$TMPD/anh-out" \
    -bo m16-that -buoc nang-cap -cho-tep "$wait_file" </dev/null >"$TMPD/br.out" 2>&1 &
  pid=$!
  for _ in $(seq 1 120); do
    grep -q '^CHỜ NÂNG CẤP' "$TMPD/br.out" 2>/dev/null && break
    kill -0 "$pid" 2>/dev/null || break
    sleep 1
  done
  ok "trình duyệt đã đăng nhập bằng giao diện $OLD_VER và đang chờ" grep -q '^CHỜ NÂNG CẤP' "$TMPD/br.out"
  vroot tpan self-update --yes --url "$SRC/a"
  ok "tpan self-update của $OLD_VER lên bản A: thoát 0, job thành công" job_ok
  ok "in $OLD_VER -> $VER" hasF "$OLD_VER -> $VER"
  wait_panel
  touch "$wait_file"
  wait "$pid"
  RC=$?
  browser_report "nâng cấp"

  vexec tpan version
  ok "sau khi nâng: tpan version là bản A ($VER)" has "^tpan $VER "
  page "$SITE_CLI"
  ok "$SITE_CLI vẫn 200 sau khi nâng" is200
  local h1="$TMPD/health.1" moi
  health_errs "$h1"
  moi="$(new_errs "$H0" "$h1")"
  [ -n "$moi" ] && printf '[lỗi] mới so với trước khi nâng:\n%s\n' "$moi"
  ok "tpan health sau khi nâng không có [lỗi] mới" test -z "$moi"
  part_end E
}

# ================================================================ PHẦN F

part_f() {
  part_begin "F: hồi quy M15 trên giao diện mới: bấm thật nút cập nhật A lên B rồi quay về"
  vroot python3 "$E/db.py" seturl "$SRC/b"
  ok "(đồ nghề) setting update.self_url là $SRC/b" out_is "update.self_url=$SRC/b"
  browser "nút cập nhật (M15)" -bo m15-that
  ok "kịch bản bấm thật của M15 chạy hết, thoát 0" rc_is 0
  vexec tpan version
  ok "sau khi cập nhật rồi quay về: tpan version là bản A ($VER)" has "^tpan $VER "
  part_end F
}

# ================================================================ CUỐI

part_end_all() {
  part_begin "CUỐI: health, site, tắt đồ nghề"
  local h2="$TMPD/health.2" moi
  health_errs "$h2"
  moi="$(new_errs "$H0" "$h2")"
  [ -n "$moi" ] && printf '[lỗi] mới so với lúc mới cài %s:\n%s\n' "$OLD_VER" "$moi"
  ok "cuối kịch bản tpan health không có [lỗi] mới so với lúc mới cài $OLD_VER" test -z "$moi"
  page "$SITE_CLI"
  ok "$SITE_CLI vẫn 200" is200
  vroot systemctl stop "$HTTP_UNIT"
  ok "(đồ nghề) tắt máy chủ tải $HTTP_UNIT" rc_is 0
  part_end CUỐI
}

leak_check() {
  [ -n "${E2E_LOG:-}" ] && [ -f "$E2E_LOG" ] || return 0
  section "bí mật, IP, tên user Mac có lọt vào chính file log không"
  local n=0 s
  while IFS= read -r s; do
    [ -n "$s" ] || continue
    n=$((n + $(grep -cF -- "$s" "$E2E_LOG")))
  done <"$SECRETS"
  printf 'Số bí mật đã gặp: %s; số dòng của log chứa một trong số đó: %s\n' "$(grep -c . "$SECRETS")" "$n"
  ok "log không chứa mật khẩu nào" test "$n" -eq 0
  ok "log không chứa IP máy ảo" bash -c '[ -z "$1" ] || ! grep -qF -- "$1" "$2"' _ "$VM_IP" "$E2E_LOG"
  ok "log không chứa mạng Multipass" bash -c '[ -z "$1" ] || ! grep -qF -- "$1" "$2"' _ "$NET4" "$E2E_LOG"
  ok "log không chứa IP của Mac" bash -c '[ -z "$1" ] || ! grep -qF -- "$1" "$2"' _ "$HOST_IP" "$E2E_LOG"
  ok "log không chứa thư mục nhà hay tên user Mac" bash -c '! grep -qF -- "$1" "$3" && ! grep -qF -- "$2" "$3"' _ "$HOME" "$USER" "$E2E_LOG"
}

printf 'e2e M16 (bước 19). Bản cũ: %s; bản A (của cây): %s; bản B: %s; máy ảo: %s\n' "$OLD_VER" "$VER" "$VER_B" "$VMNAME"
part_0
part_a
part_b
part_c
part_d
part_e
part_f
part_end_all

leak_check
section "TỔNG KẾT: $PASSES khẳng định đạt, $FAILS khẳng định không đạt"
[ "$FAILS" -eq 0 ]
