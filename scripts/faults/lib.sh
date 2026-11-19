# shellcheck shell=bash
# Phần chung của các kịch bản gây lỗi trong scripts/faults/. Được `source` từ
# từng kịch bản, không chạy riêng.
#
# Mọi lệnh trong máy ảo đi qua `scripts/vm.sh root <argv>`: vm.sh chuyển argv
# nguyên vẹn cho `multipass exec <vm> -- sudo ...`, không qua shell trong máy
# ảo. Vì vậy ở đây không bao giờ ghép chuỗi thành một dòng lệnh shell; giá trị
# đọc từ tpan (đường dẫn, tên DB) được kiểm bằng regex trước khi dùng, vì các
# lệnh chạy bằng root.
set -euo pipefail
cd "$(dirname "$0")/../.." || exit 2

VM=scripts/vm.sh

die() { printf 'lỗi: %s\n' "$*" >&2; exit 2; }

# vm_root chạy argv bằng root trong máy ảo. </dev/null: không có terminal
# (agent, script nền) thì multipass exec chờ stdin mãi.
vm_root() { "$VM" root "$@" </dev/null; }

# need_mode kiểm đối số đầu là break hoặc restore.
need_mode() {
  case "${1:-}" in
    break | restore) ;;
    *) die "dùng: $0 break|restore${2:+ $2}" ;;
  esac
}

# need_domain kiểm domain đủ để dùng làm đối số (tpan còn kiểm lại kỹ hơn).
need_domain() {
  [[ "${1:-}" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] || die "dùng: $0 break|restore <domain>"
}

# json_get <đường đi> đọc một trường từ JSON trên stdin, ví dụ
# "layout.socket". Đọc bằng python3 trên Mac để không cài thêm jq trong máy ảo.
json_get() {
  python3 -I -c '
import json, sys
v = json.load(sys.stdin)
for k in sys.argv[1].split("."):
    v = v[k]
print(v)
' "$1"
}

# site_field <domain> <trường> đọc từ `tpan site info <domain> --json`.
site_field() {
  vm_root tpan site info "$1" --json | json_get "$2"
}

# stack_field <trường> đọc config.<trường> từ `tpan stack status --json`.
stack_field() {
  vm_root tpan stack status --json | json_get "config.$1"
}

# site_php <domain>: bản PHP của site, không có thì bản mặc định của stack.
site_php() {
  local v
  v="$(site_field "$1" site.php_version)"
  [ -n "$v" ] || v="$(stack_field php_version)"
  [[ "$v" =~ ^[0-9]+\.[0-9]+$ ]] || die "bản PHP lạ: $v"
  printf '%s\n' "$v"
}

# hint in lệnh để xem health sau khi gây lỗi hoặc trả lại.
hint() {
  printf 'Xem kết quả: %s root tpan health%s\n' "$VM" "${1:+ $1}"
  printf 'Sửa tự động: %s root tpan health%s --fix --yes\n' "$VM" "${1:+ $1}"
}
