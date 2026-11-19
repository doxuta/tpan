#!/usr/bin/env bash
# Kịch bản gây lỗi 05: khoá user DB của một site.
#
#   scripts/faults/05-db-user-lock.sh break <domain>
#   scripts/faults/05-db-user-lock.sh restore <domain>
#
# Gây lỗi: ALTER USER '<db>'@'localhost' ACCOUNT LOCK qua `mariadb -e` (root
# đăng nhập MariaDB bằng unix_socket, không cần mật khẩu). Kết nối mới của
# WordPress bị từ chối: "Error establishing a database connection".
# Chặng chuyển sang lỗi: DB. Mong đợi Fail:
#   db.access:<domain>
#   http.site:<domain>          (chặng đầu cuối lỗi theo)
# Sửa tự động mong đợi: core.FixDBUnlock ("db.unlock"): ALTER USER ... ACCOUNT UNLOCK.
# restore: ALTER USER '<db>'@'localhost' ACCOUNT UNLOCK. Chỉ dùng khi sửa tự
# động hỏng.
# shellcheck source=scripts/faults/lib.sh
source "$(dirname "$0")/lib.sh"

need_mode "${1:-}" "<domain>"
need_domain "${2:-}"
mode=$1 domain=$2

db="$(site_field "$domain" layout.db)"
# Tên DB nằm trong câu SQL: chỉ nhận đúng dạng tpan sinh (tp_<slug>, mục 5),
# không có dấu nháy nào lọt vào được.
[[ "$db" =~ ^tp_[a-z0-9_]+$ ]] || die "tên user DB lạ: $db"

case "$mode" in
  break) verb=LOCK ;;
  restore) verb=UNLOCK ;;
esac
vm_root mariadb -e "ALTER USER '$db'@'localhost' ACCOUNT $verb;"
printf 'Đã ACCOUNT %s user %s@localhost.\n' "$verb" "$db"
[ "$mode" = break ] && printf 'Mong đợi Fail: db.access:%s\n' "$domain"
hint "$domain"
