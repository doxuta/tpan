#!/usr/bin/env bash
# Kịch bản gây lỗi 04: đổi quyền wp-config.php của một site thành 644.
#
#   scripts/faults/04-wpconfig-perm.sh break <domain>
#   scripts/faults/04-wpconfig-perm.sh restore <domain>
#
# Gây lỗi: chmod 644 <layout.wp_config>. Site vẫn chạy, nhưng mật khẩu DB của
# site đọc được bởi user khác trên máy (mục 5, cô lập từng site).
# Chặng chuyển sang lỗi: WordPress. Mong đợi Fail:
#   wp.config:<domain>
# Sửa tự động mong đợi: core.FixWPConfigPerm ("wp.config.perm"): đưa file về
# 0600 và đúng chủ.
# restore: chmod 600 <layout.wp_config>. Chỉ dùng khi sửa tự động hỏng.
# shellcheck source=scripts/faults/lib.sh
source "$(dirname "$0")/lib.sh"

need_mode "${1:-}" "<domain>"
need_domain "${2:-}"
mode=$1 domain=$2

wpc="$(site_field "$domain" layout.wp_config)"
[[ "$wpc" =~ ^/var/www/[a-z0-9.-]+/public/wp-config\.php$ ]] || die "đường dẫn wp-config.php lạ: $wpc"

case "$mode" in
  break)
    vm_root chmod 644 "$wpc"
    printf 'Đã chmod 644 %s. Mong đợi Fail: wp.config:%s\n' "$wpc" "$domain"
    ;;
  restore)
    vm_root chmod 600 "$wpc"
    printf 'Đã chmod 600 %s.\n' "$wpc"
    ;;
esac
hint "$domain"
