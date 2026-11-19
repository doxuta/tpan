#!/usr/bin/env bash
# Kịch bản gây lỗi 01: dừng dịch vụ PHP-FPM của site.
#
#   scripts/faults/01-php-fpm-stop.sh break <domain>
#   scripts/faults/01-php-fpm-stop.sh restore <domain>
#
# Gây lỗi: systemctl stop php<v>-fpm (v là bản PHP của site). Mọi pool của bản
# đó tắt, socket của chúng biến mất.
# Chặng chuyển sang lỗi: PHP. Mong đợi Fail:
#   service.active:php<v>-fpm   (máy và stack)
#   php.socket:<domain>         (mọi site dùng bản PHP đó: cả một cột, vẫn một chặng)
#   http.site:<domain>          (chặng đầu cuối lỗi theo, như mục 6.7 cho phép)
# Sửa tự động mong đợi: core.FixServiceStart ("service.start") cho
# service.active; sau đó core.FixPHPRestart ("php.restart") của từng site thấy
# socket đã có và bỏ qua bước khởi động lại.
# restore: systemctl start php<v>-fpm. Chỉ dùng khi sửa tự động hỏng, để đưa
# máy ảo về tay.
# shellcheck source=scripts/faults/lib.sh
source "$(dirname "$0")/lib.sh"

need_mode "${1:-}" "<domain>"
need_domain "${2:-}"
mode=$1 domain=$2
unit="php$(site_php "$domain")-fpm"

case "$mode" in
  break)
    vm_root systemctl stop "$unit"
    printf 'Đã dừng %s. Mong đợi Fail: service.active:%s, php.socket:%s\n' "$unit" "$unit" "$domain"
    ;;
  restore)
    vm_root systemctl start "$unit"
    printf 'Đã chạy lại %s.\n' "$unit"
    ;;
esac
hint ""
