#!/usr/bin/env bash
# Lấy nguyên văn các file cấu hình mà tpan ghi ra trên một máy thật (máy ảo
# tpan-test), để người đọc thấy tpan đã đặt gì lên máy chứ không chỉ đọc kết
# luận "đạt". Chạy từ máy sạch: cài stack nginx, tạo một site, bật ufw,
# fail2ban, lịch backup, cập nhật tự động, rồi chép từng file ra
# docs/bang-chung/cau-hinh/. Sau đó đổi sang apache và chép thêm vhost apache.
#
#   scripts/lay-cau-hinh.sh
#
# Bí mật bị che trước khi ghi: mật khẩu DB và các khoá muối trong wp-config.php,
# mật khẩu trong file .cnf, IP máy ảo. Tên miền trong máy ảo là a.example.com.
set -euo pipefail
cd "$(dirname "$0")/.."

OUT=docs/bang-chung/cau-hinh
D=a.example.com
mkdir -p "$OUT"

vroot() { scripts/vm.sh root "$@" </dev/null; }

VM_IP=""
# che: bỏ IP máy ảo và mọi giá trị bí mật trong output.
che() {
  sed -E \
    -e "s/${VM_IP//./\\.}/<vm>/g" \
    -e "s/(define\( *'(DB_PASSWORD|AUTH_KEY|SECURE_AUTH_KEY|LOGGED_IN_KEY|NONCE_KEY|AUTH_SALT|SECURE_AUTH_SALT|LOGGED_IN_SALT|NONCE_SALT)', *')[^']*'/\1<đã che>'/" \
    -e "s/^(password[[:space:]]*=[[:space:]]*).*/\1<đã che>/"
}

# lay <tên file đích> <đường dẫn trên máy>: ghi quyền, chủ và nội dung file.
lay() {
  local name=$1 path=$2
  {
    printf '# %s\n' "$path"
    vroot stat -c '# quyền %a, chủ %U:%G, %s byte' "$path"
    printf '\n'
    vroot cat "$path"
  } 2>&1 | che >"$OUT/$name"
  printf 'đã lấy %s\n' "$path"
}

# chay <tên file đích> <lệnh...>: ghi output của một lệnh chỉ đọc.
chay() {
  local name=$1
  shift
  # tpan health thoát 1 khi có cảnh báo (ví dụ chưa có backup): vẫn lấy output.
  { printf '$ %s\n\n' "$*"; vroot "$@" || printf '\n(mã thoát %s)\n' "$?"; } 2>&1 | che >"$OUT/$name"
  printf 'đã chạy %s\n' "$*"
}

make vm-reset
make vm-deploy
VM_IP="$(scripts/vm.sh ip)"

# setup in mật khẩu quản trị: không cần ở đây, bỏ output.
vroot tpan setup >/dev/null
vroot tpan stack install --web nginx --yes
vroot tpan site add "$D" --yes
vroot tpan security ufw --yes
vroot tpan security fail2ban --yes
vroot tpan backup settings --keep 5 --at 03:30 --yes
vroot tpan update auto --yes

LUSER="$(vroot tpan site info "$D" --json | jq -r '.. | .user? // empty' | head -1)"

lay nginx-vhost-site.conf "/etc/nginx/sites-available/tpan-$D.conf"
lay nginx-default.conf /etc/nginx/sites-available/tpan-default.conf
lay nginx-tpan.conf /etc/nginx/conf.d/tpan.conf
chay nginx-sites-enabled.txt ls -l /etc/nginx/sites-enabled/
chay php-fpm-pools.txt ls -l /etc/php/8.3/fpm/pool.d/
lay php-fpm-pool-site.conf "$(vroot sh -c "ls /etc/php/8.3/fpm/pool.d/tpan-*.conf" | head -1)"
lay wp-config.php "/var/www/$D/wp-config.php"
chay www-quyen.txt ls -la "/var/www/$D"
lay mariadb-99-tpan.cnf /etc/mysql/mariadb.conf.d/99-tpan.cnf
lay cron-site.txt "$(vroot sh -c "ls /etc/cron.d/tpan-* | grep -v backup" | head -1)"
lay cron-backup.txt /etc/cron.d/tpan-backup
lay logrotate-tpan-sites.txt /etc/logrotate.d/tpan-sites
lay fail2ban-tpan-sshd.conf /etc/fail2ban/jail.d/tpan-sshd.conf
chay ufw-status.txt ufw status verbose
lay apt-52tpan-unattended-upgrades /etc/apt/apt.conf.d/52tpan-unattended-upgrades
lay hosts.txt /etc/hosts
lay systemd-tpan.service /etc/systemd/system/tpan.service
chay systemd-timers.txt systemctl list-timers --all --no-pager
chay tpan-dirs.txt ls -la /etc/tpan /var/lib/tpan
chay linux-user-site.txt id "$LUSER"
chay health.txt tpan health

vroot tpan stack switch --to apache --yes
lay apache-vhost-site.conf "/etc/apache2/sites-available/tpan-$D.conf"
lay apache-default.conf /etc/apache2/sites-available/000-tpan-default.conf
chay apache-sites-enabled.txt ls -l /etc/apache2/sites-enabled/
chay health-apache.txt tpan health
vroot tpan stack switch --to nginx --yes

printf '\nXong. File nằm ở %s\n' "$OUT"
