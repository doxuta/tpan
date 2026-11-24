#!/bin/bash
# Đọc và sửa nội dung một site WordPress mà không chạy lệnh nào dưới user của
# site (e2e M17: bước "sao lưu, sửa nội dung, khôi phục" của site trong lồng;
# lệnh dưới user của site ngoài hàng rào là thứ bất biến 8 cấm, nên đi bằng
# mysql của root và file do root đặt). CHỈ chạy trong máy ảo test của mốc, bằng
# root. Trên Mac, trong thư mục repo:
#
#   scripts/vm.sh root bash -s -- <chế độ> <domain> [đối số] < scripts/e2e-m17/noi-dung.sh
#
#   doc <domain>            BLOGNAME=<tên site trong wp_options>
#   sua <domain> <giá trị>  đặt blogname rồi in BLOGNAME=
#   tao-file <domain> <tên> tạo public/<tên> (thuộc user của site), in CO_FILE=1
#   co-file <domain> <tên>  CO_FILE=1 nếu public/<tên> còn, 0 nếu không
#
# Không in mật khẩu DB: chỉ đọc DB_NAME và table_prefix từ wp-config.php.
# Toàn bộ nằm trong hàm và dòng cuối gọi main (xem cgroup.sh).
set -u

main() {
  local mode=${1:?thiếu chế độ} d=${2:?thiếu domain} u cfg db pre val f
  u=$(stat -c %U "/var/www/$d" 2>/dev/null)
  [ -n "$u" ] || { echo "LOI=không thấy thư mục /var/www/$d"; return 2; }
  cfg=/var/www/$d/public/wp-config.php
  db=$(sed -nE "s/^define\( *'DB_NAME' *, *'([^']*)' *\);.*/\1/p" "$cfg" | head -1)
  pre=$(sed -nE "s/^\\\$table_prefix *= *'([^']*)'.*/\1/p" "$cfg" | head -1)
  [ -n "$db" ] && [ -n "$pre" ] || { echo "LOI=không đọc được DB_NAME hay table_prefix"; return 3; }
  case "$mode" in
    doc | sua)
      if [ "$mode" = sua ]; then
        val=${3:?thiếu giá trị}
        mysql -N -e "UPDATE \`$db\`.\`${pre}options\` SET option_value = '${val//\'/}' WHERE option_name = 'blogname'" </dev/null
      fi
      echo "BLOGNAME=$(mysql -N -e "SELECT option_value FROM \`$db\`.\`${pre}options\` WHERE option_name = 'blogname'" </dev/null)"
      ;;
    tao-file)
      f=/var/www/$d/public/${3:?thiếu tên}
      printf 'e2e-m17\n' >"$f"
      chown "$u:www-data" "$f"
      chmod 640 "$f"
      echo "CO_FILE=1"
      ;;
    co-file)
      f=/var/www/$d/public/${3:?thiếu tên}
      if [ -e "$f" ]; then echo "CO_FILE=1"; else echo "CO_FILE=0"; fi
      ;;
    *)
      echo "LOI=chế độ lạ: $mode"
      return 2
      ;;
  esac
}

main "$@"; exit $?
