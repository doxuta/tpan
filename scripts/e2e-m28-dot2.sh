#!/usr/bin/env bash
# e2e đợt 2 (nhóm A) của mốc M28: F4 (và D5), H5, M1-DBCONN, P1, P2, và nâng cấp
# từ v0.2.0. Chạy trên Mac, trong thư mục repo, điều khiển máy ảo (tên trong
# .tpan-vm). Cùng khuôn scripts/e2e-m28.sh. Gọi qua Makefile:
#
#   make e2e-m28-dot2 P="nginx apache" L=docs/bang-chung/M28-dot2.log
#   make e2e-m28-dot2 P=all B=0 BIN=bin/tpan-truoc-ban-va-linux L=docs/bang-chung/M28-dot2-truoc-ban-va.log
#
# Các phần (tham số dòng lệnh):
#   nginx     máy sạch, stack nginx: f4.sh, dbconn.sh, p1.sh, p2.sh
#   apache    máy sạch, stack Apache: f4.sh, h5.sh, dbconn.sh
#   nang-cap  cài v0.2.0 bằng install.sh của gói, hai site WordPress của admin,
#             nâng lên bản của cây, health, security harden --yes, rồi 200, đăng
#             nhập WordPress, F4, H5, DBCONN chuyển sang chặn
#   all       = nginx apache nang-cap
#
# Biến môi trường (Makefile truyền vào): E2E_BUILD, E2E_BIN_PREFIX, E2E_LOG như
# scripts/e2e-m28.sh.
#
# Logic kiểm nằm trong scripts/e2e-m28-dot2/*.sh, chạy TRONG máy ảo bằng root.
# Chúng dùng lại scripts/e2e-m28/chung.sh và dung-may.sh của đợt 1: cả hai thư
# mục được chép vào cùng một chỗ trong máy ảo. Mỗi khẳng định in đúng một dòng
# "=> ĐẠT" hoặc "=> KHÔNG ĐẠT". Thoát khác 0 khi có khẳng định không đạt. Nhiều
# lệnh ở đây được chờ là phải lỗi: không dùng set -e.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2

VM=scripts/vm.sh
VMNAME="${TPAN_VM:-$(tr -d '[:space:]' 2>/dev/null <.tpan-vm)}"
VMNAME="${VMNAME:-tpan-test}"
# Mọi lệnh máy ảo qua lớp bắt output của vm.sh và có stdin </dev/null (xem
# scripts/e2e-m28.sh).
export TPAN_VM_CAPTURE=1
E2E_BUILD="${E2E_BUILD:-1}"
BIN_PREFIX="${E2E_BIN_PREFIX:-bin/tpan-linux}"
DEFAULT_PREFIX=bin/tpan-linux
R=/home/ubuntu/e2e-m28 # thư mục script trong máy ảo (cả hai bộ)
OLD_VER=v0.2.0
OLD_DIR="dist/release/$OLD_VER"
PASSES=0
FAILS=0
P0=0
F0=0
ARCH=""
VM_IP=""

ts() { date '+%Y-%m-%d %H:%M:%S'; }
section() { printf '\n\n######## [%s] %s\n' "$(ts)" "$*"; }

# mask: che IP máy ảo, thư mục nhà, tên user Mac, và mật khẩu (tpan setup in mật
# khẩu quản trị 26 chữ in hoa và số; tpan site add in mật khẩu WordPress 32 chữ và số).
mask() {
  local s=$1
  [ -n "$VM_IP" ] && s="${s//"$VM_IP"/<vm>}"
  s="${s//"$HOME"/~}"
  s="${s//"$USER"/<user>}"
  printf '%s' "$s" | sed -E \
    -e 's/(^|[^A-Za-z0-9])[A-Z0-9]{26}([^A-Za-z0-9]|$)/\1<mật khẩu đã che>\2/g' \
    -e 's/(^|[^A-Za-z0-9])[A-Za-z0-9]{32}([^A-Za-z0-9]|$)/\1<mật khẩu đã che>\2/g'
}

verdicts() {
  local p f
  p="$(grep -c '=> ĐẠT:' <<<"$1")"
  f="$(grep -c '=> KHÔNG ĐẠT:' <<<"$1")"
  PASSES=$((PASSES + p))
  FAILS=$((FAILS + f))
}

# vm_run <nhãn> <script trong R> [tham số...]: chạy một script trong máy ảo bằng root.
vm_run() {
  local label=$1 script=$2 out t0 t1
  shift 2
  printf '\n[%s] >>> %s  (bash %s %s)\n' "$(ts)" "$label" "$script" "$*"
  t0="$(date +%s)"
  out="$("$VM" root bash "$R/$script" "$@" </dev/null 2>&1)"
  t1="$(date +%s)"
  mask "$out"
  printf '\n[%s] <<< %s xong trong %s giây\n' "$(ts)" "$label" "$((t1 - t0))"
  verdicts "$out"
}

# vm_plain <nhãn> <lệnh vm.sh...>: chạy một lệnh khác (không đếm khẳng định).
vm_plain() {
  local label=$1 out
  shift
  printf '\n[%s] $ %s\n' "$(ts)" "$label"
  out="$("$VM" "$@" </dev/null 2>&1)"
  local rc=$?
  mask "$out"
  printf '\n[%s] (mã thoát %s)\n' "$(ts)" "$rc"
  return "$rc"
}

part_begin() { P0=$PASSES F0=$FAILS; section "PHẦN $*"; }
part_end() { printf '\n==== Phần %s: %s khẳng định đạt, %s không đạt\n' "$1" "$((PASSES - P0))" "$((FAILS - F0))"; }

reset_vm() {
  printf '\n[%s] đưa máy ảo %s về sạch\n' "$(ts)" "$VMNAME"
  local _ try
  for try in 1 2; do
    TPAN_VM_CAPTURE= "$VM" reset </dev/null >/dev/null 2>&1
    for _ in $(seq 1 20); do
      "$VM" exec cloud-init status --wait </dev/null >/dev/null 2>&1 && break
      sleep 3
    done
    # Như scripts/e2e-m28.sh: máy chưa về snapshot sạch (còn tpan), hay không
    # chạy được lệnh nào trong máy (đã gặp 23/11: multipass exec "No route to
    # host") thì dừng phần đó, không chạy tiếp trên máy cũ.
    if "$VM" exec true </dev/null >/dev/null 2>&1 &&
      ! "$VM" exec test -e /usr/local/bin/tpan -o -e /var/lib/tpan </dev/null >/dev/null 2>&1; then
      break
    fi
    printf '[%s] máy ảo chưa về sạch hay không chạy được lệnh (lần %s)\n' "$(ts)" "$try"
    if [ "$try" = 2 ]; then
      echo "  => KHÔNG ĐẠT: máy ảo $VMNAME về được snapshot sạch và chạy được lệnh"
      FAILS=$((FAILS + 1))
      return 1
    fi
  done
  VM_IP="$(TPAN_VM_CAPTURE= "$VM" ip </dev/null 2>/dev/null | tr -d '[:space:]')"
  ARCH="$("$VM" exec dpkg --print-architecture </dev/null 2>/dev/null | tr -d '[:space:]')"
  printf '[%s] máy ảo sạch, kiến trúc %s, IP <vm>\n' "$(ts)" "$ARCH"
}

# deploy_bin: chép file chạy tpan vào máy ảo (như scripts/e2e-m28.sh).
deploy_bin() {
  if [ "$BIN_PREFIX" = "$DEFAULT_PREFIX" ]; then
    if [ "$E2E_BUILD" != 0 ]; then
      printf '[%s] make build-linux\n' "$(ts)"
      make build-linux >/dev/null 2>&1 || { echo "make build-linux lỗi"; return 1; }
    else
      printf '[%s] E2E_BUILD=0: không build, dùng bin/ sẵn có\n' "$(ts)"
    fi
    TPAN_VM_CAPTURE= "$VM" deploy </dev/null >/dev/null 2>&1
  else
    local bin="$BIN_PREFIX-$ARCH"
    printf '[%s] chép bản build sẵn %s vào máy ảo\n' "$(ts)" "$bin"
    [ -f "$bin" ] || { echo "Chưa có $bin (build ở đúng commit cần đo rồi chép thành tên đó)"; return 1; }
    "$VM" push "$bin" /tmp/tpan-m28 </dev/null >/dev/null 2>&1
    "$VM" root install -m 0755 /tmp/tpan-m28 /usr/local/bin/tpan </dev/null >/dev/null 2>&1
  fi
  "$VM" exec tpan version </dev/null 2>&1 | head -1
}

# deploy_tree: thay file chạy v0.2.0 bằng bản của cây rồi restart tpan.
deploy_tree() {
  local bin
  if [ "$BIN_PREFIX" = "$DEFAULT_PREFIX" ] && [ "$E2E_BUILD" != 0 ]; then
    make build-linux >/dev/null 2>&1 || return 1
  fi
  bin="$BIN_PREFIX-$ARCH"
  [ -f "$bin" ] || { echo "Chưa có $bin"; return 1; }
  "$VM" push "$bin" /tmp/tpan-m28 </dev/null >/dev/null 2>&1
  "$VM" root install -m 0755 /tmp/tpan-m28 /usr/local/bin/tpan </dev/null >/dev/null 2>&1
  "$VM" root systemctl restart tpan </dev/null >/dev/null 2>&1
}

# push_scripts: chép scripts/e2e-m28/ (chung.sh, dung-may.sh...) và
# scripts/e2e-m28-dot2/ vào cùng một thư mục trong máy ảo.
push_scripts() {
  printf '[%s] chép scripts/e2e-m28/ và scripts/e2e-m28-dot2/ vào máy ảo (%s)\n' "$(ts)" "$R"
  local f
  for f in scripts/e2e-m28/* scripts/e2e-m28-dot2/*; do
    "$VM" push "$f" "$R/$(basename "$f")" </dev/null >/dev/null 2>&1
  done
}

# stage_v020: chép gói v0.2.0 vào máy ảo (install.sh, SHA256SUMS, hai file chạy).
stage_v020() {
  printf '[%s] chép gói %s vào máy ảo\n' "$(ts)" "$OLD_VER"
  [ -d "$OLD_DIR" ] || { echo "Chưa có $OLD_DIR"; return 1; }
  local f
  for f in install.sh SHA256SUMS tpan-linux-amd64 tpan-linux-arm64; do
    [ -f "$OLD_DIR/$f" ] && "$VM" push "$OLD_DIR/$f" "/home/ubuntu/$OLD_VER/$f" </dev/null >/dev/null 2>&1
  done
}

# ---------------------------------------------------------------- các phần

part_stack() { # part_stack <apache|nginx>
  local web=$1
  part_begin "$(tr a-z A-Z <<<"$web")"
  reset_vm || { part_end "$(tr a-z A-Z <<<"$web")"; return; }
  deploy_bin
  push_scripts
  vm_run "dựng máy ($web)" dung-may.sh "$web"
  if [ "$web" = apache ]; then
    vm_run "F4 public là symlink sang site khác (Apache)" f4.sh
    vm_run "H5 mod_status" h5.sh
    vm_run "M1-DBCONN trần kết nối DB" dbconn.sh
  else
    vm_run "F4 public là symlink sang site khác (nginx), D5 health" f4.sh
    vm_run "M1-DBCONN trần kết nối DB" dbconn.sh
    vm_run "P1 khoá đăng nhập" p1.sh
    vm_run "P2 trần argon2id cùng lúc" p2.sh
  fi
  part_end "$(tr a-z A-Z <<<"$web")"
}

part_nangcap() {
  part_begin "NÂNG CẤP: từ $OLD_VER lên bản của cây (Apache), security harden"
  reset_vm || { part_end NÂNG-CẤP; return; }
  stage_v020
  push_scripts
  vm_plain "cài $OLD_VER bằng install.sh của gói (--web apache)" \
    exec bash -c "cd /home/ubuntu/$OLD_VER && sudo bash ./install.sh --web apache"
  vm_run "dựng hai site WordPress của admin và site e trên $OLD_VER" dung-may.sh apache
  vm_run "pha trước (WordPress, F4 H5 DBCONN phải LỘ, health làm mốc)" nang-cap.sh truoc
  printf '\n[%s] thay file chạy bằng bản của cây rồi restart tpan\n' "$(ts)"
  deploy_tree
  "$VM" exec tpan version </dev/null 2>&1 | head -1
  vm_run "pha sau (health, harden, 200, đăng nhập WordPress, F4 H5 DBCONN chặn)" nang-cap.sh sau
  part_end NÂNG-CẤP
}

# ---------------------------------------------------------------- điều phối

PARTS=()
for a in "$@"; do
  case "$a" in
    all) PARTS+=(nginx apache nang-cap) ;;
    nginx | apache | nang-cap) PARTS+=("$a") ;;
    *) echo "Phần lạ: $a (dùng: nginx apache nang-cap all)" >&2; exit 2 ;;
  esac
done
[ ${#PARTS[@]} -gt 0 ] || { echo "Cách dùng: $0 <phần>...  (nginx apache nang-cap all)" >&2; exit 2; }

section "e2e M28 đợt 2 (nhóm A) — máy ảo $VMNAME — phần: ${PARTS[*]} — log ${E2E_LOG:-(stdout)}"
printf 'E2E_BUILD=%s  E2E_BIN_PREFIX=%s\n' "$E2E_BUILD" "$BIN_PREFIX"

for p in "${PARTS[@]}"; do
  case "$p" in
    apache | nginx) part_stack "$p" ;;
    nang-cap) part_nangcap ;;
  esac
done

section "TỔNG KẾT: $PASSES khẳng định đạt, $FAILS khẳng định không đạt"
[ "$FAILS" -eq 0 ]
