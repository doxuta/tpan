#!/usr/bin/env bash
# e2e M18: việc nhỏ trên một site WordPress. Chạy TRONG máy ảo bằng root:
#
#   bash /root/e2e-m18/site.sh tong <domain>            checksum mọi file của site và bản xuất DB
#   bash /root/e2e-m18/site.sh moc <domain> <chữ>       ghi file đánh dấu public/e2e-moc.txt
#   bash /root/e2e-m18/site.sh ten <domain> <chữ>       đổi tên blog (option blogname)
#   bash /root/e2e-m18/site.sh xem <domain>             mã HTTP trang chủ, tên blog trong <title>, file đánh dấu
#   bash /root/e2e-m18/site.sh to <domain> <MiB>        thêm file ngẫu nhiên (không nén được) vào public/
#
# File do root ghi được trả về đúng chủ của site và nhóm www-data, quyền 640,
# như các file khác mà web server phục vụ (tránh disable_symlinks và quyền
# đọc làm sai kết quả). Bản xuất DB dùng mysqldump của root (socket) với
# --skip-dump-date --skip-comments để hai lần xuất của cùng dữ liệu giống hệt.
set -u
act=$1
d=$2
pub=/var/www/$d/public
u="$(stat -c %U "/var/www/$d")"
put() { # put <file> <chữ>
  printf '%s\n' "$2" >"$1"
  chown "$u:www-data" "$1"
  chmod 640 "$1"
}
case "$act" in
  tong)
    echo "tep=$(cd "$pub" && find . -type f -print0 | LC_ALL=C sort -z | xargs -0 sha256sum | sha256sum | cut -c1-64) so-tep=$(find "$pub" -type f | wc -l)"
    db="$(cd / && runuser -u "$u" -- wp config get DB_NAME --path="$pub" 2>/dev/null)"
    f="$(mktemp)"
    mysqldump --skip-dump-date --skip-comments --single-transaction "$db" >"$f" 2>/dev/null
    echo "db=$(sha256sum <"$f" | cut -c1-64) db-byte=$(wc -c <"$f") db-ten=${db:+co}"
    rm -f "$f"
    ;;
  moc) put "$pub/e2e-moc.txt" "$3" && echo "moc $d: $3" ;;
  ten) cd / && runuser -u "$u" -- wp option update blogname "$3" --path="$pub" ;;
  xem)
    f="$(mktemp)"
    c="$(curl -s -m 20 --resolve "$d:80:127.0.0.1" -o "$f" -w '%{http_code}' "http://$d/")"
    echo "trang-chu=$c title=$(grep -o '<title>[^<]*</title>' "$f" | head -1)"
    c="$(curl -s -m 20 --resolve "$d:80:127.0.0.1" -o "$f" -w '%{http_code}' "http://$d/e2e-moc.txt")"
    echo "moc=$c $(head -c 100 "$f" | tr -d '\n')"
    rm -f "$f"
    ;;
  to)
    head -c $(($3 * 1024 * 1024)) /dev/urandom >"$pub/e2e-to.bin"
    chown "$u:www-data" "$pub/e2e-to.bin"
    chmod 640 "$pub/e2e-to.bin"
    ls -l "$pub/e2e-to.bin"
    ;;
  *) echo "không biết việc $act" >&2; exit 2 ;;
esac
