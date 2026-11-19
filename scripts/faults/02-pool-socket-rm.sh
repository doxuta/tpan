#!/usr/bin/env bash
# Kịch bản gây lỗi 02: xoá socket của pool PHP-FPM của một site.
#
#   scripts/faults/02-pool-socket-rm.sh break <domain>
#   scripts/faults/02-pool-socket-rm.sh restore <domain>
#
# Gây lỗi: rm -f /run/php/tpan-<slug>.sock (đường dẫn lấy từ
# `tpan site info <domain> --json`, trường layout.socket). PHP-FPM vẫn chạy
# nhưng web server không còn chỗ để nói chuyện với pool: site trả 502.
# Chặng chuyển sang lỗi: PHP. Mong đợi Fail:
#   php.socket:<domain>
#   http.site:<domain>          (chặng đầu cuối lỗi theo)
# Sửa tự động mong đợi: core.FixPHPRestart ("php.restart"): khởi động lại
# PHP-FPM để socket được tạo lại.
# restore: systemctl restart php<v>-fpm (tạo lại socket). Chỉ dùng khi sửa tự
# động hỏng.
# shellcheck source=scripts/faults/lib.sh
source "$(dirname "$0")/lib.sh"

need_mode "${1:-}" "<domain>"
need_domain "${2:-}"
mode=$1 domain=$2

case "$mode" in
  break)
    sock="$(site_field "$domain" layout.socket)"
    # rm chạy bằng root: chỉ nhận đúng dạng socket tpan đặt (mục 5).
    [[ "$sock" =~ ^/run/php/tpan-[a-z0-9_-]+\.sock$ ]] || die "đường dẫn socket lạ: $sock"
    vm_root rm -f "$sock"
    printf 'Đã xoá %s. Mong đợi Fail: php.socket:%s\n' "$sock" "$domain"
    ;;
  restore)
    unit="php$(site_php "$domain")-fpm"
    vm_root systemctl restart "$unit"
    printf 'Đã khởi động lại %s.\n' "$unit"
    ;;
esac
hint "$domain"
