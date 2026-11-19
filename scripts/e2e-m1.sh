#!/usr/bin/env bash
# e2e của mốc M1 (bước 1 ở mục 12 của docs/KE-HOACH.md) trên máy ảo tpan-test.
# Chạy trên Mac, trong thư mục repo:
#
#   scripts/e2e-m1.sh all       nginx rồi apache, mỗi bên từ máy sạch, cùng một bản build
#   scripts/e2e-m1.sh nginx     chỉ nginx
#   scripts/e2e-m1.sh apache    chỉ apache
#   scripts/e2e-m1.sh probe     dò các điểm của mục 16 trên máy sạch (không dùng tpan)
#   scripts/e2e-m1.sh mirror <đường dẫn trong pool>   máy nào của ports.ubuntu.com có file đó
#
# Output ra stdout: mỗi lệnh có dấu thời gian, output nguyên văn và mã thoát;
# mỗi khẳng định in "=> ĐẠT" hoặc "=> KHÔNG ĐẠT". Dòng cuối là tổng kết.
# Không dùng set -e: nhiều lệnh ở đây được chờ là phải lỗi (dry-run để lại gì
# không, curl tới site mặc định của nginx, cài apache trên máy đang là nginx).
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2

VM=scripts/vm.sh
FAILS=0
OUT=""
RC=0

ts() { date '+%Y-%m-%d %H:%M:%S'; }
section() { printf '\n\n######## [%s] %s\n' "$(ts)" "$*"; }

# run CMD...: in lệnh, output nguyên văn (stdout và stderr gộp), mã thoát.
# Output giữ trong $OUT, mã thoát trong $RC cho các khẳng định phía sau.
run() {
  printf '\n[%s] $ %s\n' "$(ts)" "$*"
  OUT="$("$@" 2>&1)"
  RC=$?
  [ -n "$OUT" ] && printf '%s\n' "$OUT"
  printf '[%s] (mã thoát %s)\n' "$(ts)" "$RC"
}

ok() {
  local desc=$1
  shift
  if "$@"; then
    printf '  => ĐẠT: %s\n' "$desc"
  else
    printf '  => KHÔNG ĐẠT: %s\n' "$desc"
    FAILS=$((FAILS + 1))
  fi
}

rc_is() { [ "$RC" -eq "$1" ]; }
rc_not() { [ "$RC" -ne "$1" ]; }
has() { grep -qE -- "$1" <<<"$OUT"; }
hasnt() { ! grep -qE -- "$1" <<<"$OUT"; }

vexec() { run "$VM" exec "$@"; }
vroot() { run "$VM" root "$@"; }

reset_vm() {
  run make vm-reset
  ok "máy ảo về snapshot clean" rc_is 0
  # Ngay sau khi start, SSH của multipass có thể chưa sẵn ("No route to host",
  # mã 255): thử lại vài lần trước khi coi là lỗi.
  local _
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    vexec cloud-init status --wait
    [ "$RC" -ne 255 ] && break
    sleep 3
  done
  ok "cloud-init xong" rc_is 0
  vexec bash -c 'dpkg -l | wc -l; dpkg -l | grep -c "^ii"'
}

# DATE cố định cho cả lần chạy: buildinfo.Date là thứ duy nhất đổi giữa hai lần
# build cùng mã, nên với DATE cố định thì nửa nginx và nửa apache dùng đúng một
# file chạy (so sha256 in ra bên dưới).
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

deploy() {
  run make vm-deploy DATE="$BUILD_DATE"
  ok "build và chép tpan vào máy ảo" rc_is 0
  run shasum -a 256 bin/tpan-linux-arm64 bin/tpan-linux-amd64
  run git log --oneline -1
  run git status --short
  run git diff --stat
}

# Ảnh chụp những gì tpan quản lý, để so trước và sau một lần chạy lại: md5 của
# mọi file cấu hình của bốn dịch vụ và logrotate, đích của mọi symlink, quyền
# của thư mục tpan, wp, và PID chính của ba dịch vụ cùng các tiến trình con
# của web server (reload hay restart đều đổi PID, md5 thì không thấy).
SNAP_SCRIPT='
for d in /etc/nginx /etc/apache2 /etc/mysql /etc/php /etc/logrotate.d; do
  [ -d "$d" ] || continue
  find "$d" -type f -print0 | sort -z | xargs -0 md5sum
  find "$d" -type l -printf "%p -> %l\n" | sort
done
md5sum /usr/local/bin/wp
stat -c "%a %U:%G %n" /var/lib/tpan /var/lib/tpan/acme /var/cache/tpan
for u in nginx apache2 php8.3-fpm mariadb; do
  systemctl show -p Id -p MainPID -p ExecMainStartTimestamp -p ActiveState -p UnitFileState "$u" 2>/dev/null | tr "\n" " "
  echo
done
echo "con của web server:"
pgrep -a -x nginx | sort
pgrep -a -x apache2 | sort
'

snapshot() { # snapshot <tên file trên Mac>
  "$VM" root bash -c "$SNAP_SCRIPT" >"$1" 2>&1
}

# ---------------------------------------------------------------------------

stack_run() { # stack_run nginx|apache
  local web=$1 unit=$1
  [ "$web" = apache ] && unit=apache2
  local tmp
  tmp="$(mktemp -d)"

  section "$web (a): máy sạch, triển khai tpan"
  reset_vm
  deploy
  vexec tpan version
  ok "tpan version chạy được" rc_is 0

  section "$web (b): --dry-run in kế hoạch, không đổi gì"
  vroot tpan stack install --web "$web" --dry-run
  ok "dry-run thoát 0" rc_is 0
  ok "dry-run in kế hoạch" has 'Kế hoạch:'
  vexec dpkg -l "$unit"
  ok "dpkg -l $unit: vẫn chưa có gói" hasnt '^ii'
  vexec bash -c 'for p in /var/lib/tpan /var/lib/tpan/tpan.db /etc/tpan /var/cache/tpan /run/tpan; do if [ -e "$p" ]; then echo "CÓ $p"; else echo "không có $p"; fi; done'
  ok "không có /var/lib/tpan, tpan.db, /etc/tpan, /var/cache/tpan, /run/tpan" hasnt '^CÓ '

  section "$web (c): cài thật lần một"
  vroot tpan stack install --web "$web" --yes
  ok "cài thoát 0" rc_is 0
  ok "job thành công" has '^Job .*: thành công'
  ok "không phép kiểm nào lỗi hoặc chưa rõ" hasnt '\[(lỗi|chưa rõ)\]'
  local php
  php="$("$VM" exec bash -c 'ls /etc/php' | tr -d '\r' | head -1)"
  printf '\nBản PHP dò được trong /etc/php: %s\n' "$php"

  section "$web (d): kiểm độc lập với tpan"
  for u in "$unit" "php$php-fpm" mariadb; do
    vexec systemctl is-active "$u"
    ok "$u active" has '^active$'
    vexec systemctl is-enabled "$u"
    ok "$u enabled" has '^enabled$'
  done
  vroot ss -tlnp
  ok "có 127.0.0.1:3306" has '127\.0\.0\.1:3306 '
  ok "3306 không nghe ở địa chỉ nào khác" bash -c '! grep -E ":3306 " <<<"$1" | grep -vq "127\.0\.0\.1:3306 "' _ "$OUT"
  ok "có cổng 80" has ':80 '

  local ip
  ip="$("$VM" ip)"
  vexec curl -sS -m 5 -o /dev/null -w '%{http_code}\n' http://127.0.0.1/
  if [ "$web" = nginx ]; then
    ok "curl 127.0.0.1 (trong máy ảo): nginx đóng kết nối, curl mã 52" rc_is 52
  else
    ok "curl 127.0.0.1 (trong máy ảo): 403" has '^403$'
  fi
  run curl -sS -m 5 -o /dev/null -w '%{http_code}\n' "http://$ip/"
  if [ "$web" = nginx ]; then
    ok "curl IP máy ảo từ Mac: nginx đóng kết nối, curl mã 52" rc_is 52
  else
    ok "curl IP máy ảo từ Mac: 403" has '^403$'
  fi

  if [ "$web" = nginx ]; then
    vexec ls -la /etc/nginx/sites-enabled/
    vexec test -e /etc/nginx/sites-enabled/default
    ok "không còn sites-enabled/default" rc_is 1
    vexec test -L /etc/nginx/sites-enabled/tpan-default.conf
    ok "có sites-enabled/tpan-default.conf (symlink)" rc_is 0
    vroot nginx -t
    ok "nginx -t" rc_is 0
  else
    vexec ls -la /etc/apache2/sites-enabled/
    vexec test -e /etc/apache2/sites-enabled/000-default.conf
    ok "không còn sites-enabled/000-default.conf" rc_is 1
    vexec test -L /etc/apache2/sites-enabled/000-tpan-default.conf
    ok "có sites-enabled/000-tpan-default.conf (symlink)" rc_is 0
    vroot apachectl -M
    ok "apachectl -M thoát 0" rc_is 0
    ok "có mpm_event" has 'mpm_event_module'
    ok "có proxy_fcgi" has 'proxy_fcgi_module'
    ok "không có mpm_prefork" hasnt 'mpm_prefork_module'
    ok "không có mod php" hasnt 'php'
    # M1 chưa bật ssl (mục 6.1): bật ssl thì ports.conf của Ubuntu mở cổng 443
    # mà site mặc định chỉ có *:80, nên 443 trả trang mặc định của distro.
    ok "không có ssl_module" hasnt 'ssl_module'
    vroot apachectl configtest
    ok "apachectl configtest" rc_is 0
    vroot apachectl -S
  fi

  # Cổng 443 thuộc mốc SSL (mục 6.3); ở M1 không web server nào được nghe ở đó.
  vroot ss -tlnH
  ok "không ai nghe cổng 443" hasnt ':443 '
  vexec curl -sS -m 5 -o /dev/null -w '%{http_code}\n' http://127.0.0.1:443/
  ok "curl http://127.0.0.1:443/ không kết nối được (curl mã 7)" rc_is 7

  vroot logrotate -d /etc/logrotate.d/tpan-sites
  ok "logrotate -d thoát 0" rc_is 0
  vroot wp --info --allow-root
  ok "wp --info" rc_is 0
  vroot stat -c '%a %U:%G %n' /var/lib/tpan /var/lib/tpan/acme /var/lib/tpan/tpan.db
  ok "/var/lib/tpan 755 root:root" has '^755 root:root /var/lib/tpan$'
  ok "/var/lib/tpan/acme 755 root:root" has '^755 root:root /var/lib/tpan/acme$'
  ok "/var/lib/tpan/tpan.db 600 root:root" has '^600 root:root /var/lib/tpan/tpan.db$'

  vroot bash -c 'mkdir -p /var/lib/tpan/acme/.well-known/acme-challenge && echo tpan-e2e-acme-ok > /var/lib/tpan/acme/.well-known/acme-challenge/tpan-e2e && ls -la /var/lib/tpan/acme/.well-known/acme-challenge/'
  vexec curl -sS -m 5 -w '\n%{http_code}\n' -H 'Host: la.example' http://127.0.0.1/.well-known/acme-challenge/tpan-e2e
  ok "ACME với Host lạ: 200 và đúng nội dung" bash -c 'grep -qx 200 <<<"$1" && grep -qx tpan-e2e-acme-ok <<<"$1"' _ "$OUT"
  vroot rm -r /var/lib/tpan/acme/.well-known
  ok "xoá file thử ACME" rc_is 0

  vroot mariadb -e "SELECT User, Host, JSON_VALUE(Priv, '\$.plugin') AS plugin, JSON_EXTRACT(Priv, '\$.auth_or') AS auth_or FROM mysql.global_priv ORDER BY User, Host"
  vroot mariadb -N -B -e "SELECT COUNT(*) FROM mysql.global_priv WHERE User=''"
  ok "đếm user ẩn danh = 0" has '^0$'
  vroot mariadb -e 'SHOW DATABASES'
  ok "không có DB test" hasnt '^test$'
  vroot mariadb -e 'SHOW GRANTS FOR root@localhost'
  ok "root@localhost dùng unix_socket" has 'unix_socket'

  section "$web (e): chạy lại lần hai, không được đổi gì"
  snapshot "$tmp/before"
  printf '\n[%s] Ảnh chụp trước lần chạy lại:\n' "$(ts)"
  cat "$tmp/before"
  vroot tpan stack install --web "$web" --yes --verbose
  ok "chạy lại thoát 0" rc_is 0
  ok "job thành công" has '^Job .*: thành công'
  ok "không bước nào chạy (không có dòng \"xong\")" hasnt '^  xong$'
  local steps skipped
  steps="$(grep -c '^- ' <<<"$OUT")"
  skipped="$(grep -c '^  đã đúng, bỏ qua$' <<<"$OUT")"
  printf '\nSố bước: %s, số bước bỏ qua: %s\n' "$steps" "$skipped"
  ok "mọi bước đều bỏ qua" test "$steps" -gt 0 -a "$steps" -eq "$skipped"
  printf '\nCác lệnh tpan đã chạy trong lần hai (dòng "$"), gom và đếm:\n'
  grep -E '^    \$ ' <<<"$OUT" | sed 's/^    \$ //' | sort | uniq -c
  ok "chỉ có lệnh đọc (dpkg-query, systemctl is-*, mariadb -N -B đọc trạng thái, a2query -m)" \
    bash -c '! grep -E "^    \\$ " <<<"$1" | sed "s/^    \\$ //" | grep -vqE "^(dpkg-query --show |systemctl is-(active|enabled) |mariadb -N -B$|a2query -m )"' _ "$OUT"
  snapshot "$tmp/after"
  run diff "$tmp/before" "$tmp/after"
  ok "máy không đổi (md5, symlink, quyền, PID)" rc_is 0

  section "$web (f): status và health"
  vroot tpan stack status
  ok "stack status thoát 0" rc_is 0
  vroot tpan health
  ok "health thoát 0" rc_is 0
  vroot tpan health --json
  ok "health --json thoát 0" rc_is 0
  ok "health --json có installed: true" has '"installed": ?true'

  if [ "$web" = nginx ]; then
    section "$web (g): cài apache trên máy đang là nginx phải bị từ chối"
    snapshot "$tmp/before-g"
    vroot tpan stack install --web apache --yes
    ok "bị từ chối (mã thoát khác 0)" rc_not 0
    ok "lý do: phải dùng stack switch" has 'stack switch'
    vexec dpkg -l apache2
    ok "apache2 không được cài" hasnt '^ii'
    snapshot "$tmp/after-g"
    run diff "$tmp/before-g" "$tmp/after-g"
    ok "máy không đổi" rc_is 0
  fi
}

# Kho ports.ubuntu.com có nhiều máy sau một tên; khi kho đang đồng bộ, vài máy
# có thể chưa có file .deb mà chỉ mục đã nhắc tới (404). Gọi trước khi chạy
# để biết lỗi apt là do kho hay do tpan.
mirror() { # mirror <đường dẫn trong pool>
  local ip
  for ip in $("$VM" exec getent ahostsv4 ports.ubuntu.com | awk '{print $1}' | sort -u); do
    vexec curl -s -o /dev/null -w "$ip %{http_code}\n" --resolve "ports.ubuntu.com:80:$ip" "http://ports.ubuntu.com/ubuntu-ports/pool/$1"
  done
}

probe() {
  section "mục 16: máy sạch, chưa có tpan"
  reset_vm
  vexec lsb_release -a
  vexec uname -m

  section "mục 16: runuser với đúng môi trường tpan đặt cho lệnh con (fixedEnv khi chạy bằng root)"
  vroot env -i PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin LC_ALL=C DEBIAN_FRONTEND=noninteractive HOME=/root runuser -u www-data -- env
  vroot env -i PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin LC_ALL=C DEBIAN_FRONTEND=noninteractive HOME=/root runuser -u nobody -- sh -c 'echo "HOME=$HOME"; cd ~ 2>&1; pwd'

  section "mục 16: MariaDB của Ubuntu 24.04 ngay sau khi cài, chưa siết"
  vroot env DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=120 update -q
  vroot env DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=120 install -y -q mariadb-server
  vexec systemctl is-active mariadb
  vexec systemctl is-enabled mariadb
  vroot mariadb --version
  vroot mariadb -e "SELECT User, Host, JSON_VALUE(Priv, '\$.plugin') AS plugin, JSON_EXTRACT(Priv, '\$.auth_or') AS auth_or FROM mysql.global_priv ORDER BY User, Host"
  vroot mariadb -e 'SHOW DATABASES'
  vroot mariadb -e 'SHOW GRANTS FOR root@localhost'
  vroot ss -tlnp

  section "mục 16: apache2 cài bình thường có tự khởi động không"
  vroot env DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=120 install -y -q apache2
  vexec systemctl is-active apache2
  vexec systemctl is-enabled apache2
  vroot ss -tlnp
  vexec curl -sS -m 5 -o /dev/null -w '%{http_code}\n' http://127.0.0.1/

  section "mục 16: policy-rc.d (exit 101) có chặn apache2 tự khởi động không (máy sạch lại)"
  reset_vm
  vroot bash -c 'printf "#!/bin/sh\nexit 101\n" > /usr/sbin/policy-rc.d && chmod 755 /usr/sbin/policy-rc.d && cat /usr/sbin/policy-rc.d'
  vroot env DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=120 update -q
  vroot env DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=120 install -y -q apache2
  vexec systemctl is-active apache2
  vexec systemctl is-enabled apache2
  vroot ss -tlnp
  vroot rm /usr/sbin/policy-rc.d
}

case "${1:-}" in
  mirror)
    section "kho: các máy của ports.ubuntu.com có $2 không"
    mirror "$2"
    ;;
  nginx | apache) stack_run "$1" ;;
  all)
    stack_run nginx
    stack_run apache
    ;;
  probe) probe ;;
  *)
    sed -n '2,10p' "$0" | sed 's/^# \{0,1\}//'
    exit 2
    ;;
esac

section "TỔNG KẾT: $FAILS khẳng định không đạt"
[ "$FAILS" -eq 0 ]
