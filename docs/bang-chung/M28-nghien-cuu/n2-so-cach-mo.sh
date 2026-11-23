#!/usr/bin/env bash
# N2: so năm cách một tiến trình root đọc "public/.htaccess" của một thư mục do
# user khác kiểm soát (xem _thu-cua/main.go), trên từng thứ user đó đặt được ở
# tên ấy, rồi chạy đua tráo file và đo thời gian. Không cần tpan. Chạy bằng
# root trong máy ảo. Mọi thứ nằm dưới /srv/m28-thu, script tự dọn ở cuối.
set -u
T=/home/ubuntu/m28/thu-cua
B=/srv/m28-thu
S=$B/www/a
HT=$S/public/.htaccess
SEC=$B/nan-nhan/bi-mat.txt
N="${1:-20000}"

rm -rf "$B"
id tp_thu >/dev/null 2>&1 || useradd --system --user-group --no-create-home --shell /usr/sbin/nologin tp_thu
mkdir -p "$B/www" "$B/nan-nhan" "$B/nan-nhan2"
chmod 755 "$B" "$B/www"
chmod 700 "$B/nan-nhan" "$B/nan-nhan2"
install -d -o tp_thu -g www-data -m 2750 "$S" "$S/public"
head -c 300 /dev/urandom | base64 >"$SEC"
cp "$SEC" "$B/nan-nhan2/.htaccess"
chmod 600 "$SEC" "$B/nan-nhan2/.htaccess"
as() { runuser -u tp_thu -- "$@"; }
moi() { rm -rf "$HT" "$HT.kia"; echo; echo "--- $* ---"; }
doc() { "$T" doc "$S" public/.htaccess "$SEC"; }

echo "=== máy ==="
"$T" ho-tro
echo "user của site thử: $(id tp_thu)"
echo "file bí mật: $(stat -c '%U:%G %a, %s byte' "$SEC") trong thư mục $(stat -c '%U:%G %a' "$B/nan-nhan")"

echo
echo "=== từng thứ user của site đặt được ở public/.htaccess ==="
moi "1. file thường của site"
as cp /etc/hostname "$HT"
doc

moi "2. symlink tới file bí mật ở ngoài site"
as ln -s "$SEC" "$HT"
doc

moi "3. symlink tới một file khác ở TRONG site"
as cp /etc/hostname "$S/public/khac.txt"
as ln -s khac.txt "$HT"
doc
rm -f "$S/public/khac.txt"

moi "4. thư mục cha public là symlink ra thư mục của nạn nhân"
as mv "$S/public" "$S/public.that"
as ln -s "$B/nan-nhan2" "$S/public"
"$T" doc "$S" public/.htaccess "$B/nan-nhan2/.htaccess"
as rm "$S/public"
as mv "$S/public.that" "$S/public"

moi "5. FIFO"
as mkfifo "$HT"
doc

moi "6. Unix socket"
as python3 -c 'import socket,sys; s=socket.socket(socket.AF_UNIX); s.bind(sys.argv[1])' "$HT"
doc

moi "7. file thiết bị (root tạo hộ: user thường không mknod được)"
as mknod "$HT" c 1 5 2>&1 | sed 's/^/  user của site thử mknod: /'
mknod "$HT" c 1 5
doc

moi "8. symlink tới /dev/zero"
as ln -s /dev/zero "$HT"
doc

moi "9. file thường 2 GiB (thưa)"
as truncate -s 2G "$HT"
doc

moi "10. hardlink tới file bí mật"
as ln "$SEC" "$HT" 2>&1 | sed 's/^/  user của site thử ln (fs.protected_hardlinks=1): /'
ln "$SEC" "$HT"
stat -c '  root tạo hộ (như máy tắt fs.protected_hardlinks): %F, chủ %U:%G, quyền %a, %h tên' "$HT"
doc
echo "  => cả năm cách đều đọc: hardlink là file thường. Phải kiểm thêm chủ của file (fstat trên fd)."

echo
echo "=== đua: tráo file thường <-> symlink tới bí mật, mỗi cách đọc $N lần ==="
rm -rf "$HT" "$HT.kia"
as cp /etc/hostname "$HT"
as ln -s "$SEC" "$HT.kia"
"$T" dua "$S" public/.htaccess "$SEC" "$N" file

echo
echo "=== đua: tráo thư mục public <-> symlink tới thư mục nạn nhân, mỗi cách đọc $N lần ==="
rm -rf "$HT" "$HT.kia"
as cp /etc/hostname "$HT"
as ln -s "$B/nan-nhan2" "$S/public.kia"
"$T" dua "$S" public/.htaccess "$B/nan-nhan2/.htaccess" "$N" cha
rm -f "$S/public.kia"

echo
echo "=== thời gian một lần đọc file thường (100000 lần) ==="
"$T" do "$S" public/.htaccess 100000

rm -rf "$B"
userdel tp_thu 2>/dev/null
