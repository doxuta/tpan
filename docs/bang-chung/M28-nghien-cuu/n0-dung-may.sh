#!/usr/bin/env bash
# N0: dựng máy thử từ máy ảo sạch đã có /usr/local/bin/tpan. Chạy bằng root
# trong máy ảo. Tham số: nginx | apache (mặc định apache).
#
# Kết quả: panel chạy ở 127.0.0.1:8443; stack đã cài; hai site của admin
# (a.example.com, b.example.com); user thường khach1 và site e.example.com của
# khach1. Mật khẩu nằm ở /root/m28-admin.pw và /root/m28-khach1.pw (0600),
# không in ra.
set -u
WEB="${1:-apache}"
umask 077

json() { python3 -c 'import json,sys; print(json.load(sys.stdin)[sys.argv[1]])' "$1"; }

echo "=== tpan version ==="
tpan version

echo "=== tpan setup ==="
if [ ! -f /root/m28-admin.pw ]; then
  tpan setup --yes >/root/m28-setup.txt 2>&1
  echo "tpan setup: mã thoát $?"
  # Dòng mật khẩu là dòng duy nhất chỉ gồm chữ in và số dài từ 20 ký tự.
  grep -oE '[A-Z0-9]{20,}' /root/m28-setup.txt | head -1 >/root/m28-admin.pw
  grep -vE '[A-Z0-9]{20,}' /root/m28-setup.txt | head -30
fi
systemctl is-active tpan

echo "=== tpan stack install --web $WEB ==="
tpan stack install --web "$WEB" --yes 2>&1 | tail -4

for d in a.example.com b.example.com; do
  echo "=== tpan site add $d ==="
  tpan site add "$d" --email admin@example.com --yes --json >/root/m28-site-"$d".json 2>/root/m28-site-"$d".err
  echo "mã thoát $?"
done

echo "=== user khach1 và site e.example.com ==="
if [ ! -f /root/m28-khach1.pw ]; then
  tpan user add khach1 --yes --json 2>/dev/null | json password >/root/m28-khach1.pw
  echo "user add: mã thoát ${PIPESTATUS[0]}"
fi
tpan site add e.example.com --owner khach1 --email admin@example.com --yes --json >/root/m28-site-e.json 2>/root/m28-site-e.err
echo "site add e.example.com --owner khach1: mã thoát $?"

echo "=== hiện trạng ==="
tpan site list
for d in a.example.com b.example.com e.example.com; do
  stat -c '%n  %U:%G %a' /var/www/"$d" /var/www/"$d"/public
  ls -la /var/www/"$d"/public/.htaccess 2>&1
done
echo "kích thước file mật khẩu: admin $(wc -c </root/m28-admin.pw) byte, khach1 $(wc -c </root/m28-khach1.pw) byte"
