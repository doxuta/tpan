#!/usr/bin/env bash
# Kịch bản gây lỗi 03: sửa hỏng vhost của một site (giống người sửa tay gõ sai).
#
#   scripts/faults/03-vhost-break.sh break <domain>
#   scripts/faults/03-vhost-break.sh restore <domain>
#
# Gây lỗi: thêm dòng "tpan_fault_invalid on;" (nginx) hoặc
# "tpan_fault_invalid on" (apache) vào cuối vhost của site, bằng
# `sed -i '$a ...'`. KHÔNG reload: web server vẫn chạy bản cũ trong bộ nhớ,
# site vẫn trả 200, nhưng lần reload tới sẽ hỏng. Đúng thứ health phải bắt.
# Chặng chuyển sang lỗi: Web server. Mong đợi:
#   config.nginx:nginx hoặc config.apache:apache2   Fail (nginx -t / apachectl configtest)
#   drift:<đường dẫn vhost>                         Warn (file khác bản tpan đã ghi)
# Sửa tự động mong đợi: core.FixWebRewrite ("web.rewrite") ghi lại mọi file web
# của tpan (file đúng được bỏ qua, chỉ vhost này được ghi) rồi kiểm cú pháp và
# reload; sau đó core.FixFileRewrite ("file.rewrite") của drift thấy file đã
# đúng và bỏ qua.
# restore: xoá dòng đã thêm bằng `sed -i '/tpan_fault_invalid/d'`. Chỉ dùng khi
# sửa tự động hỏng.
# shellcheck source=scripts/faults/lib.sh
source "$(dirname "$0")/lib.sh"

need_mode "${1:-}" "<domain>"
need_domain "${2:-}"
mode=$1 domain=$2

web="$(stack_field web)"
case "$web" in
  nginx) conf="$(site_field "$domain" layout.nginx_conf)" bad='tpan_fault_invalid on;' check=config.nginx:nginx ;;
  apache) conf="$(site_field "$domain" layout.apache_conf)" bad='tpan_fault_invalid on' check=config.apache:apache2 ;;
  *) die "web server lạ: $web" ;;
esac
# sed -i chạy bằng root: chỉ nhận đúng đường dẫn vhost tpan đặt (mục 5).
[[ "$conf" =~ ^/etc/(nginx|apache2)/sites-available/[a-z0-9.-]+\.conf$ ]] || die "đường dẫn vhost lạ: $conf"

case "$mode" in
  break)
    # Chạy hai lần không thêm hai dòng: grep -q mã 0 là đã có.
    if vm_root grep -q tpan_fault_invalid "$conf"; then
      printf '%s đã có dòng hỏng, không thêm nữa.\n' "$conf"
    else
      vm_root sed -i "\$a $bad" "$conf"
      printf 'Đã thêm dòng sai cú pháp vào %s (không reload).\n' "$conf"
    fi
    printf 'Mong đợi: %s Fail, drift:%s Warn\n' "$check" "$conf"
    ;;
  restore)
    vm_root sed -i '/tpan_fault_invalid/d' "$conf"
    printf 'Đã xoá dòng hỏng khỏi %s.\n' "$conf"
    ;;
esac
hint ""
