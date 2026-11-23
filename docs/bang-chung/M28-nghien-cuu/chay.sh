#!/usr/bin/env bash
# Chạy một bản thử của đợt 0 mốc M28 trên máy ảo (tên trong .tpan-vm) và lưu
# output nguyên văn vào <tên>.ket-qua.txt cạnh script. Chạy trên Mac, từ gốc
# repo, qua Makefile:
#
#   make m28-nghien-cuu T=n0-dung-may            # script chạy bằng root trong máy ảo
#   make m28-nghien-cuu T=n0-dung-may A=nginx    # A: tham số cho script
#   make m28-nghien-cuu T=n1-t1 S=truoc          # S: hậu tố của file kết quả
#
# Script trong máy ảo không in mật khẩu: mật khẩu tpan sinh ra nằm ở file
# /root/m28-*.pw (0600) của máy ảo, máy ảo bị xoá cùng snapshot.
set -uo pipefail
cd "$(dirname "$0")/../../.." || exit 2

T="${1:-}"
A="${2:-}"
S="${3:-}"
D=docs/bang-chung/M28-nghien-cuu
VM=scripts/vm.sh
R=/home/ubuntu/m28
[ -n "$T" ] && [ -f "$D/$T.sh" ] || { echo "Không có $D/$T.sh" >&2; exit 2; }

arch="$(multipass exec "$(tr -d '[:space:]' <.tpan-vm)" -- dpkg --print-architecture </dev/null)"
CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -o bin/m28-thu-cua "./$D/_thu-cua" || exit 1
"$VM" push bin/m28-thu-cua "$R/thu-cua" || exit 1
for f in "$D"/*.sh "$D"/*.py; do
  [ -f "$f" ] && { "$VM" push "$f" "$R/$(basename "$f")" || exit 1; }
done

out="$D/$T${S:+-$S}.ket-qua.txt"
{
  echo "# $T${A:+ $A}  ($(date '+%Y-%m-%d %H:%M:%S %z'), máy ảo $(tr -d '[:space:]' <.tpan-vm), $(git rev-parse --short HEAD))"
  # shellcheck disable=SC2086 # A cố ý tách thành nhiều tham số
  TPAN_VM_CAPTURE=1 "$VM" root bash "$R/$T.sh" $A 2>&1
  echo "# mã thoát: $?"
} >"$out"
cat "$out"
