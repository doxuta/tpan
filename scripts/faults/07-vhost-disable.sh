#!/usr/bin/env bash
# Kịch bản gây lỗi 07: tắt vhost của một site (giống người gỡ nhầm symlink).
#
#   scripts/faults/07-vhost-disable.sh break <domain>
#   scripts/faults/07-vhost-disable.sh restore <domain>
#
# Gây lỗi: xoá symlink bật vhost của site ở sites-enabled của web server đang
# dùng (nginx hoặc apache), kiểm cú pháp (vẫn qua: chỉ bớt một vhost) rồi
# reload. File vhost ở sites-available còn nguyên. Site rơi vào site mặc định
# của tpan (nginx: 444, curl mã 52; apache: 403). Các site khác không bị đụng.
# Chặng chuyển sang lỗi: Web server. Mong đợi Fail:
#   web.vhost:<domain>   (vhost có nhưng chưa được bật)
#   http.site:<domain>   (chặng đầu cuối lỗi theo, như mục 6.7 cho phép)
# Sửa tự động mong đợi: "web.rewrite", nhánh của web.vhost (core.Fixes
# vhostRewrite): chỉ ghi lại vhost và symlink của đúng site đó, rồi kiểm cú
# pháp và reload; Verify là web.vhost và config.<web server>.
# restore: tạo lại symlink, kiểm cú pháp, reload. Chỉ dùng khi sửa tự động
# hỏng.
#
# Lệnh reload trả về trước khi nginx áp dụng xong (worker cũ còn trả lời thêm
# khoảng 100 ms, docs/thay-doi/M8.md): ai gọi HTTP ngay sau kịch bản này thì
# tự chờ hiện tượng xuất hiện.
# shellcheck source=scripts/faults/lib.sh
source "$(dirname "$0")/lib.sh"

need_mode "${1:-}" "<domain>"
need_domain "${2:-}"
mode=$1 domain=$2

web="$(stack_field web)"
case "$web" in
  nginx) conf="$(site_field "$domain" layout.nginx_conf)" link="$(site_field "$domain" layout.nginx_link)" test=(nginx -t) unit=nginx ;;
  apache) conf="$(site_field "$domain" layout.apache_conf)" link="$(site_field "$domain" layout.apache_link)" test=(apachectl configtest) unit=apache2 ;;
  *) die "web server lạ: $web" ;;
esac
# rm và ln chạy bằng root: chỉ nhận đúng đường dẫn tpan đặt (mục 5).
[[ "$conf" =~ ^/etc/(nginx|apache2)/sites-available/[a-z0-9.-]+\.conf$ ]] || die "đường dẫn vhost lạ: $conf"
[[ "$link" =~ ^/etc/(nginx|apache2)/sites-enabled/[a-z0-9.-]+\.conf$ ]] || die "đường dẫn symlink lạ: $link"

case "$mode" in
  break)
    # Chạy hai lần không lỗi: symlink đã không còn thì thôi.
    if vm_root test -L "$link"; then
      vm_root rm "$link"
      printf 'Đã xoá symlink %s.\n' "$link"
    else
      printf '%s đã không còn, không xoá nữa.\n' "$link"
    fi
    vm_root "${test[@]}"
    vm_root systemctl reload "$unit"
    printf 'Đã kiểm cú pháp và reload %s. Mong đợi Fail: web.vhost:%s\n' "$unit" "$domain"
    ;;
  restore)
    vm_root ln -sfn "$conf" "$link"
    vm_root "${test[@]}"
    vm_root systemctl reload "$unit"
    printf 'Đã tạo lại %s -> %s và reload %s.\n' "$link" "$conf" "$unit"
    ;;
esac
hint ""
