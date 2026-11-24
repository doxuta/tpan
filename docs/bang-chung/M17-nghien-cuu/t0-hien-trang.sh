#!/bin/bash
# t0: hiện trạng của máy trước khi có lồng. Chạy bằng root trong máy ảo tpan-m17.
# Mục đích: biết chính xác một site NGOÀI lồng nhìn thấy gì (để biết hàng rào
# phải che gì), và điểm systemd-analyze security của dịch vụ PHP-FPM chung.
set -u
A=a.example.com; B=b.example.com
UA=$(stat -c %U /var/www/$A); UB=$(stat -c %U /var/www/$B)
as_a(){ runuser -u "$UA" -- "$@"; }

echo "## 0. Phiên bản"
. /etc/os-release; echo "$PRETTY_NAME | kernel $(uname -r) | $(systemctl --version | head -1)"
echo "php $(php -r 'echo PHP_VERSION;') | $(nginx -v 2>&1) | $(mariadb --version | cut -c1-40)"
echo "user A=$UA  user B=$UB"

echo; echo "## 1. Dịch vụ PHP-FPM chung: unit, tiến trình, điểm bảo mật"
systemctl cat php8.3-fpm.service 2>/dev/null | grep -v '^#' | grep -v '^$'
echo "-- tiến trình:"
ps -eo user:28,pid,rss,cmd | grep '[p]hp-fpm'
echo "-- systemd-analyze security php8.3-fpm.service (dòng tổng):"
systemd-analyze security php8.3-fpm.service --no-pager 2>/dev/null | tail -2

echo; echo "## 2. Dưới user của A (ngoài lồng): những chỗ đọc được tên site khác"
for p in /var/www /var/log/tpan/sites /etc/nginx/sites-enabled /etc/php/8.3/fpm/pool.d /etc/cron.d /run/php \
         /var/lib/tpan /var/lib/tpan/backups /etc/letsencrypt /etc/systemd/system /sys/fs/cgroup/system.slice /var/lib/logrotate; do
  out=$(as_a ls "$p" 2>&1 | tr '\n' ' ' | cut -c1-170)
  printf '  ls %-32s -> %s\n' "$p" "$out"
done
echo "  getent passwd | grep tp_ -> $(as_a getent passwd | grep -c '^tp_') dòng: $(as_a getent passwd | grep '^tp_' | cut -d: -f1 | tr '\n' ' ')"
echo "  grep -c example /var/lib/logrotate/status -> $(as_a grep -c example /var/lib/logrotate/status 2>&1)"
echo "  /etc/passwd- đọc được: $(as_a test -r /etc/passwd- && echo CO || echo KHONG); quyền: $(stat -c '%a %U:%G' /etc/passwd- 2>&1)"
echo "  /dev/shm: $(stat -c '%a %U:%G' /dev/shm) | /var/lib/php/sessions: $(stat -c '%a %U:%G' /var/lib/php/sessions 2>&1)"

echo; echo "## 3. Quyền các thư mục của tpan"
stat -c '%a %U:%G %n' /var/lib/tpan /var/lib/tpan/* /var/log/tpan /var/log/tpan/sites /var/log/tpan/sites/* /run/php /run/php/* /etc/tpan 2>&1

echo; echo "## 4. cgroup: controller bật ở gốc và ở system.slice"
echo "gốc: $(cat /sys/fs/cgroup/cgroup.subtree_control)"
echo "system.slice: $(cat /sys/fs/cgroup/system.slice/cgroup.subtree_control)"
echo "đĩa của /var/www: $(findmnt -no SOURCE,FSTYPE -T /var/www) | scheduler: $(cat /sys/block/sda/queue/scheduler 2>/dev/null)"
echo "php mặc định của CLI: $(readlink -f /usr/bin/php) | wp: $(head -c 20 /usr/local/bin/wp | head -1 | tr -d '\0')"
echo "runuser PAM: $(grep -v '^#' /etc/pam.d/runuser | grep -v '^$' | tr '\n' ';')"
