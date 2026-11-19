#!/usr/bin/env bash
# Quản lý máy ảo test của tpan bằng Multipass. Chạy trên Mac, không chạy trên VPS.
#
#   scripts/vm.sh up        tạo máy ảo (nếu chưa có) và chụp snapshot "clean"
#   scripts/vm.sh reset     đưa máy ảo về snapshot "clean" (mất mọi thay đổi sau đó)
#   scripts/vm.sh deploy    chép bin/tpan-linux-<arch> vào máy ảo
#   scripts/vm.sh shell     mở shell trong máy ảo
#   scripts/vm.sh exec ...  chạy một lệnh trong máy ảo
#   scripts/vm.sh ip        in IP của máy ảo
#   scripts/vm.sh down      tắt máy ảo
#
# Đổi tên hoặc cấu hình máy ảo bằng biến môi trường TPAN_VM, TPAN_VM_IMAGE,
# TPAN_VM_CPUS, TPAN_VM_MEM, TPAN_VM_DISK.
set -euo pipefail

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
    multipass exec "$VM" -- tpan version
    ;;
  shell) multipass shell "$VM" ;;
  exec) multipass exec "$VM" -- "$@" ;;
  ip) multipass info "$VM" --format json | jq -r --arg vm "$VM" '.info[$vm].ipv4[0]' ;;
  down) multipass stop "$VM" ;;
  *) sed -n '2,13p' "$0" | sed 's/^# \{0,1\}//' ;;
esac
