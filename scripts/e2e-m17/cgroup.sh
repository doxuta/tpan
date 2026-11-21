#!/bin/bash
# Đọc và chỉnh file cgroup, unit của lồng một site (e2e M17, mục 7 của
# docs/thay-doi/M17-thiet-ke.md). CHỈ chạy trong máy ảo test của mốc, bằng root.
# Trên Mac, trong thư mục repo:
#
#   scripts/vm.sh root bash -s -- <chế độ> <domain> [đối số...] < scripts/e2e-m17/cgroup.sh
#
# Chế độ (mỗi kết quả là một dòng "<khoá>=<giá trị>"):
#   doc <domain> <file...>     "<file>=<giá trị>" cho từng file trong cgroup của slice
#                              (memory.max, memory.peak, memory.events, pids.peak,
#                              io.max...). Chờ tới 15 giây cho file xuất hiện: slice
#                              chỉ có cgroup khi dịch vụ PHP của lồng đang chạy.
#   trang-thai <domain>        trạng thái unit socket và dịch vụ, file socket
#   sua-slice <domain> <MB>    sửa tay MemoryMax trong file slice rồi daemon-reload
#                              (phép thử bất biến 4: lệch giữa file và gói)
#   main-pid <domain>          MainPID của dịch vụ PHP của lồng
#
# Toàn bộ nằm trong hàm main và dòng cuối gọi nó: bash đọc script từ stdin, nên
# lệnh nào đọc stdin giữa chừng sẽ ăn mất phần script còn lại; gọi main ở dòng
# cuối thì phần còn lại đã trống.
set -u

slug_of() { stat -c %U "/var/www/$1" 2>/dev/null | sed 's/^tp_//'; }

main() {
  local mode=${1:?thiếu chế độ} d=${2:?thiếu domain} s cg svc sock f i v run
  shift 2
  s=$(slug_of "$d")
  [ -n "$s" ] || { echo "LOI=không thấy thư mục /var/www/$d"; return 2; }
  cg=/sys/fs/cgroup/tpan.slice/tpan-$s.slice
  svc=tpan-cage-$s-php.service
  sock=tpan-cage-$s-php.socket
  run=/run/tpan-cage/$(printf '%s' "$d" | sha256sum | cut -c1-16)
  case "$mode" in
    doc)
      for f in "$@"; do
        for i in $(seq 1 30); do
          [ -r "$cg/$f" ] && break
          sleep 0.5
        done
        if [ -r "$cg/$f" ]; then
          v=$(tr '\n' ' ' <"$cg/$f")
          echo "$f=${v% }"
        else
          echo "$f=(không có)"
        fi
      done
      ;;
    trang-thai)
      echo "SOCKET_ACTIVE=$(systemctl is-active "$sock" </dev/null)"
      echo "SOCKET_ENABLED=$(systemctl is-enabled "$sock" </dev/null 2>&1)"
      echo "DICH_VU_ACTIVE=$(systemctl is-active "$svc" </dev/null)"
      echo "DICH_VU_RESULT=$(systemctl show -p Result --value "$svc" </dev/null)"
      echo "MAIN_PID=$(systemctl show -p MainPID --value "$svc" </dev/null)"
      echo "N_RESTARTS=$(systemctl show -p NRestarts --value "$svc" </dev/null)"
      if [ -S "$run/php.sock" ]; then echo "FILE_SOCKET=co"; else echo "FILE_SOCKET=mat"; fi
      ;;
    sua-slice)
      f=/etc/systemd/system/tpan-$s.slice
      [ -f "$f" ] || { echo "LOI=không có $f"; return 3; }
      grep -q '^MemoryMax=' "$f" || { echo "LOI=slice không có dòng MemoryMax"; return 3; }
      sed -i "s/^MemoryMax=.*/MemoryMax=${1:?thiếu số MB}M/" "$f"
      systemctl daemon-reload </dev/null
      echo "DA_SUA=$(grep '^MemoryMax=' "$f")"
      ;;
    main-pid)
      echo "MAIN_PID=$(systemctl show -p MainPID --value "$svc" </dev/null)"
      ;;
    *)
      echo "LOI=chế độ lạ: $mode"
      return 2
      ;;
  esac
}

main "$@"; exit $?
