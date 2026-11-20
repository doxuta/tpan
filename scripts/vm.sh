#!/usr/bin/env bash
# Quản lý máy ảo test của tpan bằng Multipass. Chạy trên Mac, không chạy trên VPS.
#
#   scripts/vm.sh up        tạo máy ảo (nếu chưa có) và chụp snapshot "clean"
#   scripts/vm.sh reset     đưa máy ảo về snapshot "clean" (mất mọi thay đổi sau đó)
#   scripts/vm.sh deploy    chép bin/tpan-linux-<arch> vào máy ảo
#   scripts/vm.sh shell     mở shell trong máy ảo
#   scripts/vm.sh exec ...  chạy một lệnh trong máy ảo
#   scripts/vm.sh root ...  chạy một lệnh trong máy ảo bằng quyền root
#   scripts/vm.sh ip        in IP của máy ảo
#   scripts/vm.sh ssh-key <file .pub>   thêm khoá công khai vào ~ubuntu/.ssh/authorized_keys
#                           của máy ảo (không thêm trùng), để SSH thẳng từ Mac vào máy ảo
#   scripts/vm.sh down      tắt máy ảo
#
# Đổi tên hoặc cấu hình máy ảo bằng biến môi trường TPAN_VM, TPAN_VM_IMAGE,
# TPAN_VM_CPUS, TPAN_VM_MEM, TPAN_VM_DISK.
#
# TPAN_VM_CAPTURE=1 (cho script e2e chạy không có người ngồi xem): exec và root
# không cho output chảy thẳng qua `multipass exec` nữa. Output ghi vào file
# trong máy ảo rồi lấy về bằng `multipass transfer` sau khi lệnh xong. Lý do:
# `multipass exec` trên Mac có lúc treo giữa chừng khi output dài (tiến trình
# trong máy ảo đã xong, output về Mac bị cắt giữa dòng, lệnh trên Mac không bao
# giờ trả về; gặp ở e2e M6 và M14). Mã thoát, stdin và việc tách stdout/stderr
# giữ nguyên; chỉ khác là output hiện ra một lần ở cuối thay vì chảy dần.
set -euo pipefail

# Chưa đặt TPAN_VM thì đọc tên máy ảo từ file .tpan-vm ở gốc repo (mỗi worktree
# của một mốc có máy ảo riêng, mục 6 của agent-dev/CHAY-NHIEU-AGENT.md);
# không có file thì dùng tpan-test.
if [ -z "${TPAN_VM:-}" ] && [ -f "$(dirname "$0")/../.tpan-vm" ]; then
  TPAN_VM="$(tr -d '[:space:]' < "$(dirname "$0")/../.tpan-vm")"
fi
VM="${TPAN_VM:-tpan-test}"
IMAGE="${TPAN_VM_IMAGE:-24.04}"
CPUS="${TPAN_VM_CPUS:-2}"
MEM="${TPAN_VM_MEM:-4G}"
DISK="${TPAN_VM_DISK:-20G}"
SNAP="clean"

exists() { multipass info "$VM" >/dev/null 2>&1; }

has_snapshot() {
  multipass list --snapshots 2>/dev/null |
    awk -v vm="$VM" -v snap="$SNAP" '$1 == vm && $2 == snap { found = 1 } END { exit !found }'
}

# vm_exec <lệnh...>: chạy lệnh trong máy ảo (xem TPAN_VM_CAPTURE ở đầu file).
vm_exec() {
  if [ "${TPAN_VM_CAPTURE:-}" != 1 ]; then
    multipass exec "$VM" -- "$@"
    return
  fi
  local merged=0 rc
  # Người gọi gộp 2>&1 (stdout và stderr là cùng một file hay pipe) thì ghi
  # chung một file để giữ đúng thứ tự dòng; không thì ghi riêng hai file.
  [ /dev/stdout -ef /dev/stderr ] && merged=1
  # File 0600 ở nhà của user ubuntu, lần chạy sau ghi đè. Không đổi umask của
  # chính lệnh: sudo gộp umask của người gọi vào umask của lệnh root.
  # shellcheck disable=SC2016
  rc="$(multipass exec "$VM" -- bash -c 'm=$1; shift; f=$HOME/.vm-cap; (umask 077; : >"$f"; : >"$f.err")
    if [ "$m" = 1 ]; then "$@" >"$f" 2>&1; else "$@" >"$f" 2>"$f.err"; fi; echo "vm-cap-rc=$?"' _ "$merged" "$@" | tr -d '\r' | sed -n 's/^vm-cap-rc=//p' | tail -1)" || true
  if [ -z "$rc" ]; then
    echo "scripts/vm.sh: không chạy được lệnh trong máy ảo $VM (multipass exec lỗi)" >&2
    return 255
  fi
  multipass transfer "$VM:/home/ubuntu/.vm-cap" - </dev/null
  [ "$merged" = 1 ] || multipass transfer "$VM:/home/ubuntu/.vm-cap.err" - </dev/null >&2
  return "$rc"
}

cmd="${1:-help}"
shift || true

case "$cmd" in
  up)
    if ! exists; then
      multipass launch "$IMAGE" --name "$VM" --cpus "$CPUS" --memory "$MEM" --disk "$DISK"
    fi
    if ! has_snapshot; then
      # Snapshot chỉ chụp được khi máy ảo đã tắt.
      multipass stop "$VM"
      multipass snapshot "$VM" --name "$SNAP" --comment "Ubuntu sach, chua cai gi"
    fi
    multipass start "$VM"
    multipass info "$VM"
    ;;
  reset)
    has_snapshot || { echo "Chưa có snapshot '$SNAP'. Chạy: scripts/vm.sh up" >&2; exit 1; }
    multipass stop "$VM"
    # --destructive: bỏ hẳn trạng thái hiện tại, không tự chụp lại trước khi khôi phục.
    multipass restore --destructive "$VM.$SNAP"
    multipass start "$VM"
    ;;
  deploy)
    arch="$(multipass exec "$VM" -- dpkg --print-architecture)"
    bin="bin/tpan-linux-$arch"
    [ -f "$bin" ] || { echo "Chưa có $bin. Chạy: make build-linux" >&2; exit 1; }
    multipass transfer "$bin" "$VM:/tmp/tpan"
    multipass exec "$VM" -- sudo install -m 0755 /tmp/tpan /usr/local/bin/tpan
    # </dev/null: chạy không có terminal (agent, script nền) thì multipass exec
    # chờ stdin mãi và lệnh treo.
    multipass exec "$VM" -- tpan version </dev/null
    ;;
  shell) multipass shell "$VM" ;;
  exec) vm_exec "$@" ;;
  root) vm_exec sudo "$@" ;;
  ip) multipass info "$VM" --format json | jq -r --arg vm "$VM" '.info[$vm].ipv4[0]' ;;
  ssh-key)
    # Bắt buộc đưa đường dẫn: script không tự đọc ~/.ssh của máy cá nhân.
    key="${1:-}"
    [ -n "$key" ] && [ -f "$key" ] || { echo "Cách dùng: scripts/vm.sh ssh-key <file khoá công khai .pub>" >&2; exit 2; }
    # Chỉ nhận đúng một dòng khoá công khai: chặn nhầm file khoá riêng.
    if grep -q 'PRIVATE KEY' "$key" || [ "$(grep -c . "$key")" -ne 1 ] ||
      ! grep -qE '^(ssh-|ecdsa-|sk-)[a-z0-9@.-]+ [A-Za-z0-9+/=]+( .*)?$' "$key"; then
      echo "$key không phải một khoá công khai OpenSSH (một dòng, bắt đầu bằng ssh-... hoặc ecdsa-...)" >&2
      exit 2
    fi
    # Khoá đi qua stdin, không nối vào chuỗi lệnh. grep -qxF: đã có đúng dòng
    # đó thì không thêm lần nữa.
    multipass exec "$VM" -- bash -c 'set -eu
      k="$(cat)"
      install -d -m 700 "$HOME/.ssh"
      f="$HOME/.ssh/authorized_keys"
      touch "$f"
      chmod 600 "$f"
      if grep -qxF -- "$k" "$f"; then echo "Khoá đã có trong $f, không thêm."; else printf "%s\n" "$k" >>"$f"; echo "Đã thêm khoá vào $f."; fi' <"$key"
    ;;
  down) multipass stop "$VM" ;;
  *) sed -n '2,16p' "$0" | sed 's/^# \{0,1\}//' ;;
esac
