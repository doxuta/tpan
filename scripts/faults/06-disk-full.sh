#!/usr/bin/env bash
# Kịch bản gây lỗi 06: làm đầy phân vùng / tới khoảng 91% bằng một file giả.
#
#   scripts/faults/06-disk-full.sh break
#   scripts/faults/06-disk-full.sh restore
#
# Gây lỗi: fallocate một file /var/tmp/tpan-fault-fill đủ lớn để / dùng khoảng
# 91% (tính như phép kiểm: (size - avail) / size, avail là phần user thường
# còn dùng được, cột Avail của df).
# Chặng chuyển sang lỗi: Máy. Mong đợi Fail:
#   disk.usage:/
# Sửa tự động: KHÔNG CÓ. tpan chạy bằng root mà tự xoá file nó không tạo ra là
# quá nguy hiểm (không biết file nào là dữ liệu của người dùng), nên phép kiểm
# đĩa chỉ có sửa tay (docs/thay-doi/M4.md, "Quyết định lúc lập hợp đồng").
# restore: xoá /var/tmp/tpan-fault-fill. Đây là cách DUY NHẤT đưa máy về, nên
# luôn chạy restore sau break; e2e chạy kịch bản này cuối cùng.
# shellcheck source=scripts/faults/lib.sh
source "$(dirname "$0")/lib.sh"

need_mode "${1:-}"
FILL=/var/tmp/tpan-fault-fill

case "$1" in
  break)
    if vm_root test -e "$FILL"; then
      die "$FILL đã có; chạy restore trước rồi break lại"
    fi
    # df -B1 --output=size,avail /: dòng 2 là "<size> <avail>" tính bằng byte.
    read -r size avail < <(vm_root df -B1 --output=size,avail / | awk 'NR == 2 { print $1, $2 }')
    [[ "$size" =~ ^[0-9]+$ && "$avail" =~ ^[0-9]+$ ]] || die "không đọc được df: size=$size avail=$avail"
    # Sau khi lấp: avail còn 9% size, tức đã dùng 91%.
    fill=$((avail - size * 9 / 100))
    if [ "$fill" -le 0 ]; then
      printf '/ đã dùng từ 91%% trở lên, không cần lấp thêm.\n'
    else
      vm_root fallocate -l "$fill" "$FILL"
      printf 'Đã tạo %s (%d byte).\n' "$FILL" "$fill"
    fi
    vm_root df -h /
    printf 'Mong đợi Fail: disk.usage:/ (không có sửa tự động; nhớ chạy restore)\n'
    ;;
  restore)
    vm_root rm -f "$FILL"
    vm_root df -h /
    printf 'Đã xoá %s.\n' "$FILL"
    ;;
esac
hint ""
