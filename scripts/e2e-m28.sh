#!/usr/bin/env bash
# e2e bước 31 của mốc M28 (vá T1 và họ "root đụng thứ site kiểm soát"). Chạy
# trên Mac, trong thư mục repo, điều khiển máy ảo (tên trong .tpan-vm). Gọi qua
# Makefile để lệnh được duyệt sẵn:
#
#   make e2e-m28 P="apache nginx" L=docs/bang-chung/M28.log
#   make e2e-m28 P=apache B=0 BIN=bin/tpan-truoc-ban-va-linux L=docs/bang-chung/M28-truoc-ban-va.log
#
# Các phần (tham số dòng lệnh):
#   apache    máy sạch, stack Apache, khẳng định A1..A9 (mục 7 bản thiết kế)
#   nginx     máy sạch, stack nginx, khẳng định phần B
#   goc       make m28-test-vm (test cần root của sitefs, system trong máy ảo)
#   nang-cap  phần D: cài v0.2.0 bằng install.sh của gói, nâng lên bản của cây
#   chinh     = apache nginx goc
#   all       = chinh nang-cap
#
# Biến môi trường (Makefile truyền vào):
#   E2E_BUILD=0        bỏ qua make build-linux (phiên chính có thể đang sửa mã)
#   E2E_BIN_PREFIX     tiền tố file chạy chép vào máy ảo (mặc định bin/tpan-linux);
#                      khác mặc định thì chép <prefix>-<arch> thay cho vm.sh deploy
#   E2E_LOG            chỉ để ghi nhận ở đầu log (Makefile đã chuyển hướng stdout)
#
# Logic kiểm nằm trong scripts/e2e-m28/*.sh (chạy TRONG máy ảo bằng root,
# bash + python3). Mỗi khẳng định in đúng một dòng "=> ĐẠT" hoặc
# "=> KHÔNG ĐẠT". Script này in dấu thời gian đầu mỗi phần, output nguyên văn,
# đếm số đạt/không đạt mỗi phần và cả lần chạy; thoát khác 0 khi có khẳng định
# không đạt. Nhiều lệnh ở đây được chờ là phải lỗi: không dùng set -e.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2

VM=scripts/vm.sh
VMNAME="${TPAN_VM:-$(tr -d '[:space:]' 2>/dev/null <.tpan-vm)}"
VMNAME="${VMNAME:-tpan-test}"
# Mọi lệnh máy ảo đi qua lớp bắt output của vm.sh (TPAN_VM_CAPTURE=1): ghi vào
# file trong máy ảo rồi lấy về, không để `multipass exec` treo giữa chừng khi
# output dài. Và mọi lệnh phải có stdin </dev/null: không có nó `multipass
# exec` chờ stdin mãi khi chạy không có terminal (bẫy đã biết ở skill tpan-may-ao).
export TPAN_VM_CAPTURE=1
E2E_BUILD="${E2E_BUILD:-1}"
BIN_PREFIX="${E2E_BIN_PREFIX:-bin/tpan-linux}"
DEFAULT_PREFIX=bin/tpan-linux
R=/home/ubuntu/e2e-m28          # thư mục script trong máy ảo
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

# mask: che IP máy ảo, thư mục nhà và tên user Mac khỏi output, và che mật
# khẩu: install.sh và tpan setup in mật khẩu quản trị panel (26 chữ in hoa và
# số) một lần, tpan site add in mật khẩu WordPress (32 chữ và số). Che theo
# dạng vì script trên Mac không biết trước giá trị.
mask() {
  local s=$1
  [ -n "$VM_IP" ] && s="${s//"$VM_IP"/<vm>}"
  s="${s//"$HOME"/~}"
  s="${s//"$USER"/<user>}"
  printf '%s' "$s" | sed -E \
    -e 's/(^|[^A-Za-z0-9])[A-Z0-9]{26}([^A-Za-z0-9]|$)/\1<mật khẩu đã che>\2/g' \
    -e 's/(^|[^A-Za-z0-9])[A-Za-z0-9]{32}([^A-Za-z0-9]|$)/\1<mật khẩu đã che>\2/g'
}

# verdicts <output>: cộng số "=> ĐẠT" và "=> KHÔNG ĐẠT" vào PASSES/FAILS.
verdicts() {
  local p f
  p="$(grep -c '=> ĐẠT:' <<<"$1")"
  f="$(grep -c '=> KHÔNG ĐẠT:' <<<"$1")"
  PASSES=$((PASSES + p))
  FAILS=$((FAILS + f))
}

# vm_run <nhãn> <script trong R> [tham số...]: chạy một script trong máy ảo
# bằng root, in output nguyên văn, đếm khẳng định. 2>&1 để output gộp một luồng
# (vm.sh với TPAN_VM_CAPTURE giữ đúng thứ tự dòng khi stdout và stderr là một).
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
    # Snapshot sạch không có tpan. Còn file chạy hay /var/lib/tpan nghĩa là
    # restore đã không chạy: lần chạy 13:04 ngày 21/11, một bước multipass hỏng,
    # output bị vứt, và e2e chạy tiếp trên máy cũ mà vẫn in "máy ảo sạch".
    if ! "$VM" exec test -e /usr/local/bin/tpan -o -e /var/lib/tpan </dev/null >/dev/null 2>&1; then
      break
    fi
    printf '[%s] máy ảo chưa về sạch (lần %s): còn /usr/local/bin/tpan hay /var/lib/tpan\n' "$(ts)" "$try"
    if [ "$try" = 2 ]; then
      echo "  => KHÔNG ĐẠT: máy ảo $VMNAME về được snapshot sạch"
      FAILS=$((FAILS + 1))
      return 1
    fi
  done
  VM_IP="$(TPAN_VM_CAPTURE= "$VM" ip </dev/null 2>/dev/null | tr -d '[:space:]')"
  ARCH="$("$VM" exec dpkg --print-architecture </dev/null 2>/dev/null | tr -d '[:space:]')"
  printf '[%s] máy ảo sạch, kiến trúc %s, IP <vm>\n' "$(ts)" "$ARCH"
}

# deploy_bin: chép file chạy tpan vào máy ảo. Bản của cây (prefix mặc định) thì
# build nếu E2E_BUILD khác 0 rồi vm.sh deploy; prefix khác thì chép sẵn file đó.
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
    # Bản build sẵn do BIN chỉ ra: bản chưa vá lúc đo trước khi vá, hay bản
    # cố định của cây lúc chạy bằng chứng cuối. Dòng "tpan version" in ngay sau
    # cho biết đúng commit. (Log M28.log ngày 21/11 còn nhãn cũ "bản chưa vá"
    # cho cả bản vá: nhãn sai, commit in ở dòng sau mới đúng.)
    local bin="$BIN_PREFIX-$ARCH"
    printf '[%s] chép bản build sẵn %s vào máy ảo\n' "$(ts)" "$bin"
    [ -f "$bin" ] || { echo "Chưa có $bin (build ở đúng commit cần đo rồi chép thành tên đó)"; return 1; }
    "$VM" push "$bin" /tmp/tpan-m28 </dev/null >/dev/null 2>&1
    "$VM" root install -m 0755 /tmp/tpan-m28 /usr/local/bin/tpan </dev/null >/dev/null 2>&1
  fi
  "$VM" exec tpan version </dev/null 2>&1 | head -1
}

# deploy_tree: chép bản của cây (dùng ở nang-cap để thay file chạy v0.2.0). Luôn
# dùng prefix (phiên chính gọi với BIN=bin/tpan-truoc-ban-va-linux trong lúc thử).
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

push_scripts() {
  printf '[%s] chép scripts/e2e-m28/ vào máy ảo (%s)\n' "$(ts)" "$R"
  local f
  for f in scripts/e2e-m28/*; do
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
    vm_run "A1,A3 bản xem trước" xem-truoc.sh
    vm_run "A2 ba mục tiêu T1" t1.sh chan
    vm_run "A4 đua" dua.sh
    vm_run "A5 từ chối phục vụ" tu-choi.sh
    vm_run "A5 đua FIFO (S1)" tu-choi-fifo.sh
    vm_run "A6 chạy thật" chay-that.sh
    vm_run "A7 đặt quyền thư mục" quyen-thu-muc.sh
    vm_run "A8 admin accept" admin-accept.sh
    vm_run "A9 hồi quy M14, health, backup" hoi-quy.sh
  else
    vm_run "B bản xem trước không có file /var/www, tên lạ" xem-truoc.sh
    vm_run "B7 đặt quyền thư mục" quyen-thu-muc.sh
    vm_run "B9 hồi quy M14, health, backup" hoi-quy.sh
    vm_run "B wp-config.php: đổi mật khẩu DB, file 2 GiB" nginx-db.sh
    vm_run "B chuyển web server khi public là symlink (S2)" chuyen-symlink.sh
  fi
  part_end "$(tr a-z A-Z <<<"$web")"
}

part_goc() {
  part_begin "C (test cần root trong máy ảo): make m28-test-vm"
  local out
  out="$(make m28-test-vm 2>&1)"
  mask "$out"
  echo
  if grep -q '^PASS\|^ok ' <<<"$out" && ! grep -q 'SKIP\|--- SKIP' <<<"$out" && ! grep -q '^FAIL' <<<"$out"; then
    echo "  => ĐẠT: m28-test-vm PASS, không có dòng SKIP"
    PASSES=$((PASSES + 1))
  else
    echo "  => KHÔNG ĐẠT: m28-test-vm chưa PASS hoặc có SKIP/FAIL (gói internal/sitefs có thể chưa tồn tại)"
    FAILS=$((FAILS + 1))
  fi
  part_end C
}

part_nangcap() {
  part_begin "D: nâng từ $OLD_VER lên bản của cây (Apache)"
  reset_vm || { part_end D; return; }
  stage_v020
  push_scripts
  vm_plain "cài $OLD_VER bằng install.sh của gói (--web apache)" \
    exec bash -c "cd /home/ubuntu/$OLD_VER && sudo bash ./install.sh --web apache"
  vm_run "dựng site trên $OLD_VER" dung-may.sh apache
  vm_run "D pha trước (lưu bản xem trước, health; ba mục tiêu T1 LỘ)" nang-truoc.sh
  printf '\n[%s] thay file chạy bằng bản của cây rồi restart tpan\n' "$(ts)"
  deploy_tree
  "$VM" exec tpan version </dev/null 2>&1 | head -1
  vm_run "D pha sau (xem trước giống, health không lỗi mới, 3 site 200, T1 bị chặn)" nang-sau.sh
  part_end D
}

# ---------------------------------------------------------------- điều phối

PARTS=()
for a in "$@"; do
  case "$a" in
    chinh) PARTS+=(apache nginx goc) ;;
    all) PARTS+=(apache nginx goc nang-cap) ;;
    apache | nginx | goc | nang-cap) PARTS+=("$a") ;;
    *) echo "Phần lạ: $a (dùng: apache nginx goc nang-cap chinh all)" >&2; exit 2 ;;
  esac
done
[ ${#PARTS[@]} -gt 0 ] || { echo "Cách dùng: $0 <phần>...  (apache nginx goc nang-cap chinh all)" >&2; exit 2; }

section "e2e M28 bước 31 — máy ảo $VMNAME — phần: ${PARTS[*]} — log ${E2E_LOG:-(stdout)}"
printf 'E2E_BUILD=%s  E2E_BIN_PREFIX=%s\n' "$E2E_BUILD" "$BIN_PREFIX"

for p in "${PARTS[@]}"; do
  case "$p" in
    apache | nginx) part_stack "$p" ;;
    goc) part_goc ;;
    nang-cap) part_nangcap ;;
  esac
done

section "TỔNG KẾT: $PASSES khẳng định đạt, $FAILS khẳng định không đạt"
[ "$FAILS" -eq 0 ]
