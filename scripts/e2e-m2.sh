#!/usr/bin/env bash
# e2e của mốc M2 (bước 2 tới 6 ở mục 12 của docs/KE-HOACH.md) trên máy ảo tpan-test.
# Chạy trên Mac, trong thư mục repo:
#
#   scripts/e2e-m2.sh all       nginx (bước 2-6, site rm) rồi apache (bước 2-4), mỗi bên từ máy sạch
#   scripts/e2e-m2.sh nginx     chỉ nửa nginx
#   scripts/e2e-m2.sh apache    chỉ nửa apache
#
# Đặt E2E_LOG=<file log> để cuối lần chạy script tự tìm mọi mật khẩu đã gặp
# trong chính file log đó (phải là 0 lần).
#
# Output ra stdout: mỗi lệnh có dấu thời gian, output nguyên văn và mã thoát;
# mỗi khẳng định in "=> ĐẠT" hoặc "=> KHÔNG ĐẠT". Dòng cuối là tổng kết.
# Mật khẩu (quản trị WordPress, DB của site) không bao giờ được in: output của
# mọi lệnh đi qua mask() trước khi in, và mật khẩu chỉ nằm trong một file 0600
# ở thư mục tạm của Mac, xoá khi script kết thúc.
# Không dùng set -e: nhiều lệnh ở đây được chờ là phải lỗi.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2

VM=scripts/vm.sh
FAILS=0
OUT=""
RC=0
EMAIL=admin@example.com
TMPD="$(mktemp -d)"
chmod 700 "$TMPD"
SECRETS="$TMPD/secrets"
: >"$SECRETS"
chmod 600 "$SECRETS"
trap 'rm -rf "$TMPD"' EXIT

ts() { date '+%Y-%m-%d %H:%M:%S'; }
section() { printf '\n\n######## [%s] %s\n' "$(ts)" "$*"; }

# IP của máy ảo (mạng riêng của Multipass) cũng được che trong log, theo luật
# không ghi IP vào repo.
VM_IP=""

# mask thay mọi mật khẩu đã biết và IP máy ảo trong $OUT bằng dấu che.
mask() {
  local s
  [ -n "$VM_IP" ] && OUT="${OUT//"$VM_IP"/<IP máy ảo>}"
  while IFS= read -r s; do
    [ -n "$s" ] && OUT="${OUT//"$s"/<mật khẩu, đã che>}"
  done <"$SECRETS"
  # Cookie phiên đăng nhập WordPress (thử đăng nhập bên dưới) cũng che.
  if [[ "$OUT" == *"Set-Cookie:"* ]]; then
    OUT="$(sed -E 's/^(Set-Cookie: [^=]+=)[^;]*/\1<đã che>/' <<<"$OUT")"
  fi
  # Mã băm mật khẩu của MariaDB (SHOW GRANTS) cũng không vào log.
  if [[ "$OUT" == *"IDENTIFIED BY PASSWORD"* ]]; then
    OUT="$(sed -E "s/(IDENTIFIED BY PASSWORD )'\*[0-9A-F]{40}'/\1'<mã băm, đã che>'/" <<<"$OUT")"
  fi
}

# run CMD...: in lệnh, output (đã che mật khẩu), mã thoát. $OUT và $RC cho khẳng định.
run() {
  local shown="$*"
  [ -n "$VM_IP" ] && shown="${shown//"$VM_IP"/<IP máy ảo>}"
  printf '\n[%s] $ %s\n' "$(ts)" "$shown"
  OUT="$("$@" 2>&1)"
  RC=$?
  mask
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
hasF() { grep -qF -- "$1" <<<"$OUT"; }
hasntF() { ! grep -qF -- "$1" <<<"$OUT"; }

vexec() { run "$VM" exec "$@"; }
vroot() { run "$VM" root "$@"; }

# slug_of tính slug theo mục 5, độc lập với mã Go: 16 ký tự đầu của domain,
# ký tự ngoài a-z0-9 thành _, rồi _ và 6 ký tự hex đầu của sha256(domain).
slug_of() {
  local d=$1 head hash
  head="$(printf '%s' "${d:0:16}" | tr -c 'a-z0-9' '_')"
  hash="$(printf '%s' "$d" | shasum -a 256 | cut -c1-6)"
  printf '%s_%s' "$head" "$hash"
}
user_of() { printf 'tp_%s' "$(slug_of "$1")"; }

reset_vm() {
  run make vm-reset
  ok "máy ảo về snapshot clean" rc_is 0
  local _
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    vexec cloud-init status --wait
    [ "$RC" -ne 255 ] && break
    sleep 3
  done
  ok "cloud-init xong" rc_is 0
}

BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

deploy() {
  run make vm-deploy DATE="$BUILD_DATE"
  ok "build và chép tpan vào máy ảo" rc_is 0
  VM_IP="$("$VM" ip)"
  run shasum -a 256 bin/tpan-linux-arm64 bin/tpan-linux-amd64
  run git log --oneline -1
  run git status --short
}

# ---------------------------------------------------------------------------
# Đoạn script chạy trong máy ảo (bash -c '...' _ đối số). Không đoạn nào in
# mật khẩu: mật khẩu DB được đọc vào biến bằng `wp config get` dưới user của
# site và chỉ đưa cho mariadb qua MYSQL_PWD.

# Ảnh chụp những gì một lần `site add` chạy lại có thể đổi.
SNAP_SCRIPT='
for d in /etc/nginx /etc/apache2 /etc/php; do
  [ -d "$d" ] || continue
  find "$d" -type f -print0 | sort -z | xargs -0 sha256sum
  find "$d" -type l -printf "%p -> %l\n" | sort
done
for f in /var/www/*/public/wp-config.php /var/www/*/public/.htaccess; do
  [ -e "$f" ] && sha256sum "$f" && stat -c "%a %U:%G %Y %n" "$f"
done
echo "quyền và chủ của mọi thứ trong /var/www và /var/log/tpan:"
find /var/www /var/log/tpan -printf "%m %u:%g %p\n" | sort | sha256sum
find /var/www /var/log/tpan -printf "x\n" | wc -l
stat -c "%a %U:%G %Y %n" /var/www/* /var/www/*/public /var/www/*/tmp /var/log/tpan/sites/*
echo "user và nhóm tp_:"
getent passwd | grep "^tp_" | sort
getent group | grep "^tp_" | sort
echo "DB:"
mariadb -N -B -e "SELECT User, Host FROM mysql.user ORDER BY User, Host; SHOW DATABASES; SELECT GRANTEE, TABLE_SCHEMA, PRIVILEGE_TYPE FROM information_schema.SCHEMA_PRIVILEGES ORDER BY 1, 2, 3"
echo "dịch vụ:"
for u in nginx apache2 php8.3-fpm mariadb; do
  systemctl show -p Id -p MainPID -p ExecMainStartTimestamp -p ActiveState "$u" 2>/dev/null | tr "\n" " "
  echo
done
pgrep -a -x nginx | sort
pgrep -a -x apache2 | sort
echo "bảng sites:"
python3 -c "import sqlite3; c = sqlite3.connect(\"file:/var/lib/tpan/tpan.db?mode=ro\", uri=True); [print(r) for r in c.execute(\"SELECT * FROM sites ORDER BY domain\")]"
'

snapshot() { # snapshot <tên file trên Mac>
  "$VM" root bash -c "$SNAP_SCRIPT" >"$1" 2>&1
}

# HTTP_SCRIPT <domain> <domain khác>...: mã HTTP, <title>, và thân trang có
# nhắc domain của mình và của từng site khác bao nhiêu lần.
HTTP_SCRIPT='
d=$1; shift
body=$(curl -sS -m 20 --resolve "$d:80:127.0.0.1" -w "\nHTTP %{http_code}\n" "http://$d/")
printf "%s\n" "$body" | tail -1
printf "%s\n" "$body" | grep -o "<title>[^<]*</title>"
echo "thân nhắc $d: $(printf "%s" "$body" | grep -cF "$d") dòng"
for o in "$@"; do echo "thân nhắc $o: $(printf "%s" "$body" | grep -cF "$o") dòng"; done
'

# DBISO_SCRIPT <domain> <user> <db khác>: đăng nhập bằng user DB của site.
DBISO_SCRIPT='
pw=$(cd / && runuser -u "$2" -- wp config get DB_PASSWORD --path="/var/www/$1/public") || { echo "không đọc được DB_PASSWORD"; exit 1; }
echo "SHOW DATABASES dưới user DB $2:"
MYSQL_PWD="$pw" mariadb -u "$2" -N -B -e "SHOW DATABASES"
echo "đếm wp_options trong DB của chính mình:"
MYSQL_PWD="$pw" mariadb -u "$2" -N -B -e "SELECT COUNT(*) FROM $2.wp_options"
echo "USE $3 dưới user DB $2:"
MYSQL_PWD="$pw" mariadb -u "$2" -N -B -e "USE $3"
echo "mã thoát USE: $?"
'

# PS_SCRIPT <domain>...: gọi mọi site để pool ondemand có worker, rồi xem ps.
PS_SCRIPT='
for d in "$@"; do curl -s -o /dev/null --resolve "$d:80:127.0.0.1" "http://$d/"; done
ps -eo user:32,cmd | grep "[p]hp-fpm"
'

# LEFT_SCRIPT <domain> <user> <slug> <php> <web>: in "CÒN ..." cho mọi thứ của
# site còn trên máy.
LEFT_SCRIPT='
d=$1; u=$2; slug=$3; php=$4; web=$5
getent passwd "$u" >/dev/null && echo "CÒN user Linux $u" || echo "không có user Linux $u (getent mã $?)"
getent group "$u" >/dev/null && echo "CÒN nhóm $u" || echo "không có nhóm $u"
mariadb -N -B -e "SHOW DATABASES" | grep -qx "$u" && echo "CÒN DB $u" || echo "không có DB $u"
mariadb -N -B -e "SELECT User FROM mysql.user" | grep -qx "$u" && echo "CÒN user DB $u" || echo "không có user DB $u"
for p in "/var/www/$d" "/var/log/tpan/sites/$d" "/etc/php/$php/fpm/pool.d/tpan-$slug.conf" "/run/php/tpan-$slug.sock" "/etc/$web/sites-enabled/tpan-$d.conf"; do
  if [ -e "$p" ] || [ -L "$p" ]; then echo "CÒN $p"; else echo "không có $p"; fi
done
p="/etc/$web/sites-available/tpan-$d.conf"
if [ -d "$p" ]; then echo "thư mục giả $p (do e2e tạo)"; elif [ -e "$p" ]; then echo "CÒN $p"; else echo "không có $p"; fi
python3 -c "import sqlite3,sys; c = sqlite3.connect(\"file:/var/lib/tpan/tpan.db?mode=ro\", uri=True); n = c.execute(\"SELECT COUNT(*) FROM sites WHERE domain = ?\", (sys.argv[1],)).fetchone()[0]; print((\"CÒN\" if n else \"không có\") + \" dòng \" + sys.argv[1] + \" trong bảng sites\")" "$d"
'

# ---------------------------------------------------------------------------

# site_add <domain> [cờ thêm]: như run, nhưng nhặt mật khẩu quản trị ra khỏi
# output (ghi vào $SECRETS), đếm nó xuất hiện mấy lần (phải đúng 1: dòng
# "Mật khẩu:"; hơn 1 là WP-CLI hay --verbose đã in lại), và đọc luôn mật khẩu
# DB của site vào $SECRETS (không in) trước khi in output đã che.
PW_HITS=0
DBPW_HITS=0
site_add() {
  local d=$1
  shift
  local cmd=("$VM" root tpan site add "$d" --email "$EMAIL" --yes --verbose "$@")
  printf '\n[%s] $ %s\n' "$(ts)" "${cmd[*]}"
  OUT="$("${cmd[@]}" 2>&1)"
  RC=$?
  local pw dbpw
  pw="$(sed -nE 's/^  Mật khẩu: +([A-Za-z0-9]{32})\r?$/\1/p' <<<"$OUT" | head -1)"
  PW_HITS=0
  if [ -n "$pw" ]; then
    printf '%s\n' "$pw" >>"$SECRETS"
    # Giữ riêng (0600, không xuống dòng) để check_login đưa cho curl qua file.
    (umask 077 && printf '%s' "$pw" >"$TMPD/admin-$d")
    PW_HITS="$(grep -oF -- "$pw" <<<"$OUT" | wc -l | tr -d ' ')"
  fi
  DBPW_HITS=0
  dbpw="$("$VM" root bash -c 'cd / && runuser -u "$2" -- wp config get DB_PASSWORD --path="/var/www/$1/public" 2>/dev/null' _ "$d" "$(user_of "$d")" | tr -d '\r')"
  if [ -n "$dbpw" ]; then
    grep -qxF -- "$dbpw" "$SECRETS" || printf '%s\n' "$dbpw" >>"$SECRETS"
    DBPW_HITS="$(grep -oF -- "$dbpw" <<<"$OUT" | wc -l | tr -d ' ')"
  fi
  mask
  printf '%s\n' "$OUT"
  printf '[%s] (mã thoát %s)\n' "$(ts)" "$RC"
  printf 'Mật khẩu quản trị trong output: %s lần; mật khẩu DB của site trong output: %s lần\n' "$PW_HITS" "$DBPW_HITS"
}

# create_sites <domain>...: bước 2 của mục 12.
create_sites() {
  local d
  for d in "$@"; do
    site_add "$d"
    ok "site add $d thoát 0" rc_is 0
    ok "job $d thành công" has '^Job .*: thành công'
    ok "không phép kiểm nào lỗi hoặc chưa rõ" hasnt '\[(lỗi|chưa rõ)\]'
    ok "mật khẩu quản trị được in đúng một lần (WP-CLI không in lại)" test "$PW_HITS" -eq 1
    ok "mật khẩu DB không có trong output --verbose" test "$DBPW_HITS" -eq 0
    vroot tpan site info "$d" --json
    ok "site info $d thoát 0" rc_is 0
    local slug
    slug="$(jq -r .layout.slug <<<"$OUT")"
    printf 'slug tpan: %s; slug tính theo mục 5: %s\n' "$slug" "$(slug_of "$d")"
    ok "slug của $d đúng quy tắc mục 5" test "$slug" = "$(slug_of "$d")"
  done
  vroot tpan site list
  ok "site list thoát 0" rc_is 0
}

# check_http <domain>...: mỗi site 200, có tên mình, không có tên site khác.
check_http() {
  local d o others ip
  ip="$("$VM" ip)"
  VM_IP=$ip
  for d in "$@"; do
    others=()
    for o in "$@"; do [ "$o" != "$d" ] && others+=("$o"); done
    vexec bash -c "$HTTP_SCRIPT" _ "$d" "${others[@]}"
    ok "$d: HTTP 200" has '^HTTP 200$'
    ok "$d: thân trang có tên $d" has "^thân nhắc ${d//./\\.}: [1-9][0-9]* dòng$"
    for o in "${others[@]}"; do
      ok "$d: thân trang không có tên $o" hasF "thân nhắc $o: 0 dòng"
    done
    run curl -sS -m 20 -o /dev/null -w '%{http_code}\n' --resolve "$d:80:$ip" "http://$d/"
    ok "$d gọi từ Mac qua IP máy ảo: 200" has '^200$'
  done
}

# check_login <domain>...: đăng nhập wp-login.php bằng mật khẩu tpan in ra
# (bằng chứng WP-CLI nhận đúng mật khẩu qua stdin với --prompt=admin_password),
# và đối chứng bằng một mật khẩu sai. Mật khẩu đi qua file, không qua argv.
check_login() {
  local d f wrong="$TMPD/wrong-password"
  (umask 077 && printf '%s' 'mat-khau-sai-de-doi-chung' >"$wrong")
  for d in "$@"; do
    f="$TMPD/admin-$d"
    ok "có mật khẩu quản trị của $d để thử đăng nhập" test -s "$f"
    run curl -sS -m 20 -D - -o /dev/null --resolve "$d:80:$VM_IP" -b 'wordpress_test_cookie=WP%20Cookie%20check' \
      -d log=admin -d testcookie=1 --data-urlencode "pwd@$f" "http://$d/wp-login.php"
    ok "$d: đăng nhập admin bằng mật khẩu tpan in ra: 302" has '^HTTP/1\.1 302'
    ok "$d: có cookie wordpress_logged_in_" has '^Set-Cookie: wordpress_logged_in_'
    run curl -sS -m 20 -D - -o /dev/null --resolve "$d:80:$VM_IP" -b 'wordpress_test_cookie=WP%20Cookie%20check' \
      -d log=admin -d testcookie=1 --data-urlencode "pwd@$wrong" "http://$d/wp-login.php"
    ok "$d: đối chứng mật khẩu sai: không có cookie wordpress_logged_in_" hasnt '^Set-Cookie: wordpress_logged_in_'
  done
}

# check_isolation <web> <domain>...: bước 3 của mục 12.
check_isolation() {
  local web=$1
  shift
  local d o u
  for d in "$@"; do
    u="$(user_of "$d")"
    vroot runuser -u "$u" -- test -r "/var/www/$d/public/wp-config.php"
    ok "$u đọc được wp-config.php của chính mình" rc_is 0
    vroot runuser -u www-data -- test -r "/var/www/$d/public/wp-config.php"
    ok "www-data không đọc được wp-config.php của $d" rc_is 1
    vroot stat -c '%a %U:%G %n' "/var/www/$d" "/var/www/$d/public" "/var/www/$d/tmp" "/var/www/$d/public/wp-config.php" "/var/www/$d/public/index.php" "/var/log/tpan/sites/$d" "/var/log/tpan/sites/$d/php-error.log" "/run/php/tpan-$(slug_of "$d").sock"
    ok "thư mục site 2750 $u:www-data" hasF "2750 $u:www-data /var/www/$d"
    ok "wp-config.php 600 $u" has "^600 $u:"
    ok "socket 660 www-data:www-data" hasF "660 www-data:www-data /run/php/tpan-"
    for o in "$@"; do
      [ "$o" = "$d" ] && continue
      vroot runuser -u "$u" -- cat "/var/www/$o/public/wp-config.php"
      ok "$u đọc wp-config.php của $o: bị từ chối" bash -c '[ "$1" -ne 0 ] && grep -qF "Permission denied" <<<"$2"' _ "$RC" "$OUT"
      vroot runuser -u "$u" -- ls "/var/www/$o/public"
      ok "$u liệt kê public/ của $o: bị từ chối" bash -c '[ "$1" -ne 0 ] && grep -qF "Permission denied" <<<"$2"' _ "$RC" "$OUT"
    done
  done

  vroot bash -c "$PS_SCRIPT" _ "$@"
  ok "master php-fpm chạy dưới root" has '^root +php-fpm: master process'
  for d in "$@"; do
    u="$(user_of "$d")"
    ok "pool tpan-$(slug_of "$d") chạy dưới $u" has "^$u +php-fpm: pool tpan-$(slug_of "$d")"
  done
  # Pool "www" của gói php-fpm (www-data) vẫn còn: mục 5 và 6.1 không nói gỡ nó.
  ok "không pool tpan- nào chạy dưới www-data" hasnt '^www-data +php-fpm: pool tpan-'

  local first=$1 second=$2
  local u1 u2
  u1="$(user_of "$first")"
  u2="$(user_of "$second")"
  vroot bash -c "$DBISO_SCRIPT" _ "$first" "$u1" "$u2"
  ok "user DB $u1 đăng nhập được, thấy DB của mình" has "^$u1\$"
  ok "user DB $u1 không thấy DB $u2" hasnt "^$u2\$"
  ok "user DB $u1 đọc được bảng của mình" has '^[1-9][0-9]*$'
  ok "USE $u2 dưới $u1: bị từ chối (Access denied)" bash -c 'grep -qF "Access denied" <<<"$1" && grep -qE "^mã thoát USE: [1-9]" <<<"$1"' _ "$OUT"
  for d in "$@"; do
    u="$(user_of "$d")"
    # -r: in thô. Không có nó, chế độ batch (không terminal) in mỗi \ thành \\.
    vroot mariadb -r -e "SHOW GRANTS FOR '$u'@'localhost'"
    ok "SHOW GRANTS của $u có \`${u//_/\\_}\`.* (mọi gạch dưới được thoát)" hasF "ON \`${u//_/\\_}\`.* TO \`$u\`@\`localhost\`"
    ok "SHOW GRANTS của $u chỉ có USAGE toàn cục và ALL trên DB của mình" bash -c '[ "$(grep -c "^GRANT" <<<"$1")" -eq 2 ]' _ "$OUT"
  done

  for d in "$@"; do
    vroot tpan health "$d"
    ok "health $d thoát 0" rc_is 0
    ok "health $d không có phép kiểm lỗi" hasnt '\[(lỗi|chưa rõ)\]'
  done
  vroot tpan site info "$first"
  ok "site info $first thoát 0" rc_is 0

  local php
  php="$("$VM" exec bash -c 'ls /etc/php' | tr -d '\r' | head -1)"
  vroot bash -c "head -1 /etc/php/$php/fpm/pool.d/tpan-$(slug_of "$first").conf; php-fpm$php -t"
  ok "php-fpm$php -t nhận file pool có dòng đầu '; # Do tpan quản lý'" bash -c '[ "$1" -eq 0 ] && grep -q "^; # Do tpan quản lý" <<<"$2"' _ "$RC" "$OUT"
  if [ "$web" = nginx ]; then
    vroot ls -l /etc/nginx/fastcgi.conf
    ok "/etc/nginx/fastcgi.conf có sẵn" rc_is 0
    vroot nginx -t
    ok "nginx -t" rc_is 0
  else
    vroot apachectl configtest
    ok "apachectl configtest" rc_is 0
    vroot apachectl -S
  fi
}

# rerun_same <domain>...: bước 4 của mục 12.
rerun_same() {
  local before after d
  before="$TMPD/before-$RANDOM"
  after="$TMPD/after-$RANDOM"
  snapshot "$before"
  printf '\n[%s] Ảnh chụp trước lần chạy lại:\n' "$(ts)"
  cat "$before"
  for d in "$@"; do
    site_add "$d"
    ok "chạy lại $d thoát 0" rc_is 0
    ok "job thành công" has '^Job .*: thành công'
    ok "không bước nào chạy (không có dòng \"xong\")" hasnt '^  xong$'
    local steps skipped
    steps="$(grep -c '^- ' <<<"$OUT")"
    skipped="$(grep -c '^  đã đúng, bỏ qua$' <<<"$OUT")"
    printf '\nSố bước: %s, số bước bỏ qua: %s\n' "$steps" "$skipped"
    ok "mọi bước đều bỏ qua" test "$steps" -gt 0 -a "$steps" -eq "$skipped"
    ok "không in mật khẩu quản trị (WordPress không cài lại)" test "$PW_HITS" -eq 0
    printf '\nCác lệnh tpan đã chạy khi chạy lại %s (dòng "$"), gom và đếm:\n' "$d"
    grep -E '^    \$ ' <<<"$OUT" | sed 's/^    \$ //' | sort | uniq -c
  done
  snapshot "$after"
  run diff "$before" "$after"
  ok "máy không đổi (sha256 cấu hình và wp-config, quyền, user, DB, PID, bảng sites)" rc_is 0
}

# secrets_in_db: mật khẩu nào đã gặp có nằm trong tpan.db (kể cả file WAL) hay
# trong cột message, data của job_events không. Chỉ in số lần.
secrets_in_db() {
  local f n s i=0
  for f in tpan.db tpan.db-wal; do
    "$VM" root cat "/var/lib/tpan/$f" >"$TMPD/$f" 2>/dev/null || : >"$TMPD/$f"
  done
  "$VM" root python3 -c 'import sqlite3; c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True); [print(r[0], r[1], r[2], r[3]) for r in c.execute("SELECT job_id, kind, message, data FROM job_events")]' >"$TMPD/events" 2>&1
  printf '\nSố byte đã chép về Mac: tpan.db %s, tpan.db-wal %s, job_events (dạng chữ) %s\n' \
    "$(wc -c <"$TMPD/tpan.db" | tr -d ' ')" "$(wc -c <"$TMPD/tpan.db-wal" | tr -d ' ')" "$(wc -c <"$TMPD/events" | tr -d ' ')"
  run "$VM" root python3 -c 'import sqlite3; c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True); print("jobs:", c.execute("SELECT COUNT(*) FROM jobs").fetchone()[0], "job_events:", c.execute("SELECT COUNT(*) FROM job_events").fetchone()[0]); [print(r) for r in c.execute("SELECT kind, target, status FROM jobs ORDER BY started_at")]'
  n="$(grep -acF 'site.add' "$TMPD/tpan.db" "$TMPD/tpan.db-wal" | awk -F: '{s += $2} END {print s}')"
  printf 'Đối chứng: chuỗi "site.add" có trong tpan.db + WAL ở %s dòng\n' "$n"
  ok "đối chứng: cách tìm này thấy được chữ có thật trong DB" test "$n" -gt 0
  n="$(grep -cF -- '--prompt=dbpass' "$TMPD/events")"
  printf 'Đối chứng: "--prompt=dbpass" có trong job_events ở %s dòng\n' "$n"
  ok "đối chứng: job_events có ghi lệnh wp config create" test "$n" -gt 0
  while IFS= read -r s; do
    [ -n "$s" ] || continue
    i=$((i + 1))
    n="$(cat "$TMPD/tpan.db" "$TMPD/tpan.db-wal" | grep -acF -- "$s")"
    local m
    m="$(grep -cF -- "$s" "$TMPD/events")"
    printf 'Mật khẩu thứ %s: trong tpan.db + WAL %s dòng, trong job_events %s dòng\n' "$i" "$n" "$m"
    ok "mật khẩu thứ $i không có trong tpan.db" test "$n" -eq 0 -a "$m" -eq 0
  done <"$SECRETS"
  ok "đã kiểm ít nhất một mật khẩu" test "$i" -gt 0
}

# ---------------------------------------------------------------------------

nginx_run() {
  local A=a.example.com B=b.example.com C=c.example.com D=d.example.com
  local php

  section "nginx (0): máy sạch, triển khai tpan, cài stack"
  reset_vm
  deploy
  vexec printf '[%s]\n' 'a.com; echo INJECTED'
  ok "multipass exec giữ nguyên argv (một tham số, không bị shell tách)" bash -c '[ "$1" = "[a.com; echo INJECTED]" ]' _ "$OUT"
  vroot tpan stack install --web nginx --yes
  ok "stack install thoát 0" rc_is 0
  ok "job thành công" has '^Job .*: thành công'
  php="$("$VM" exec bash -c 'ls /etc/php' | tr -d '\r' | head -1)"
  printf '\nBản PHP dò được trong /etc/php: %s\n' "$php"
  vroot wp --version --allow-root

  section "nginx bước 2: tạo 3 site"
  create_sites "$A" "$B" "$C"
  check_http "$A" "$B" "$C"
  check_login "$A" "$B" "$C"

  section "nginx bước 3: cô lập"
  check_isolation nginx "$A" "$B" "$C"

  section "nginx bước 4: chạy lại site add cho cả 3, không được đổi gì"
  rerun_same "$A" "$B" "$C"

  section "mục 16: mật khẩu không lọt vào DB của tpan"
  secrets_in_db

  section "mục 16: PHP ghi được php-error.log của site"
  vroot bash -c 'f=/var/www/$1/public/tpan-e2e-log.php; printf "%s\n" "<?php error_log(\"tpan-e2e-php-error-log\"); echo \"ok\";" > "$f"; chown "$2:www-data" "$f"; chmod 640 "$f"; curl -sS -m 20 --resolve "$1:80:127.0.0.1" "http://$1/tpan-e2e-log.php"; echo; sleep 1; ls -l "/var/log/tpan/sites/$1/"; echo "dòng thử trong php-error.log: $(grep -c tpan-e2e-php-error-log "/var/log/tpan/sites/$1/php-error.log")"; rm -f "$f"' _ "$A" "$(user_of "$A")"
  ok "php-error.log có dòng do PHP của site ghi" has '^dòng thử trong php-error.log: [1-9]'

  section "mục 16: RAM của một worker PHP WordPress (ps -o rss, KB)"
  vroot bash -c 'for i in $(seq 1 15); do for d in "$@"; do curl -s -o /dev/null --resolve "$d:80:127.0.0.1" "http://$d/"; curl -s -o /dev/null --resolve "$d:80:127.0.0.1" "http://$d/wp-login.php"; done; done; ps -o rss=,user:32=,cmd= -C "php-fpm$0" | sort -n' "$php" "$A" "$B" "$C"

  section "mục 16: df /var/www /var/lib"
  vexec df -h /var/www /var/lib
  vexec stat -c '%d %m %n' /var/www /var/lib

  section "mục 16: getent khi không có user, userdel khi user còn tiến trình"
  vexec bash -c 'getent passwd tp_khong_co_ai; echo "getent passwd mã $?"'
  ok "getent passwd trả mã 2 khi không có user" has '^getent passwd mã 2$'
  vroot bash -c 'useradd -M -s /usr/sbin/nologin tpanprobe; runuser -u tpanprobe -- sleep 60 & sleep 1; ps -o user:12,pid,cmd -u tpanprobe; userdel tpanprobe; echo "userdel khi còn tiến trình: mã $?"; pkill -u tpanprobe; sleep 1; userdel tpanprobe; echo "userdel sau khi dừng tiến trình: mã $?"; wait'

  section "nginx bước 5: domain có lệnh chèn bị từ chối, không lệnh nào chạy"
  local before after
  before="$TMPD/b5-before"
  after="$TMPD/b5-after"
  local S5='sha256sum /var/lib/tpan/tpan.db*; ls -la /var/www /var/log/tpan/sites; getent passwd | grep "^tp_"; getent group | grep "^tp_"; mariadb -N -B -e "SHOW DATABASES; SELECT User FROM mysql.user"'
  "$VM" root bash -c "$S5" >"$before" 2>&1
  cat "$before"
  vroot tpan site add 'a.com; rm -rf /' --email "$EMAIL" --yes
  ok "bị từ chối (mã thoát khác 0)" rc_not 0
  ok "lời từ chối nói về tên miền" has 'tên miền'
  vroot tpan site add 'a.com; rm -rf /' --email "$EMAIL" --yes --json
  ok "--json: bị từ chối, có mã lỗi domain_invalid" bash -c '[ "$1" -ne 0 ] && grep -q "domain_invalid" <<<"$2"' _ "$RC" "$OUT"
  "$VM" root bash -c "$S5" >"$after" 2>&1
  run diff "$before" "$after"
  ok "không có gì đổi: tpan.db và WAL, /var/www, user, nhóm, DB" rc_is 0
  vexec ls /
  ok "/ vẫn còn nguyên (có /etc, /usr, /var)" bash -c 'grep -qx etc <<<"$1" && grep -qx usr <<<"$1" && grep -qx var <<<"$1"' _ "$OUT"

  section "nginx bước 6: bước ghi vhost lỗi có chủ ý, site được hoàn tác sạch"
  local fake="/etc/nginx/sites-available/tpan-$D.conf"
  vroot mkdir "$fake"
  ok "tạo thư mục giả đúng chỗ file vhost" rc_is 0
  site_add "$D"
  ok "site add $D thất bại (mã khác 0)" rc_not 0
  ok "job hỏng và đã hoàn tác sạch" has '^Job .*: hỏng, đã hoàn tác sạch'
  ok "không có dòng hoàn tác lỗi" hasnt 'hoàn tác cũng lỗi'
  ok "không in mật khẩu quản trị" test "$PW_HITS" -eq 0
  vroot bash -c "$LEFT_SCRIPT" _ "$D" "$(user_of "$D")" "$(slug_of "$D")" "$php" nginx
  ok "không còn gì của $D (user, nhóm, DB, user DB, thư mục, log, pool, socket, symlink, dòng sites)" hasnt '^CÒN'
  ok "thư mục giả vẫn ở đó (tpan không đụng vào thứ không phải của mình)" hasF 'thư mục giả'
  vroot rmdir "$fake"
  ok "dọn thư mục giả" rc_is 0
  vroot nginx -t
  ok "nginx -t sau khi hoàn tác" rc_is 0
  vroot tpan site add "$D" --email "$EMAIL" --dry-run
  ok "dry-run cho $D không bị từ chối là site lạ (không sót gì)" bash -c '[ "$1" -eq 0 ] && ! grep -q site_exists_unmanaged <<<"$2" && grep -q "Kế hoạch:" <<<"$2"' _ "$RC" "$OUT"
  check_http "$A" "$B" "$C"

  # Từ M6, `site rm` chỉ đưa site vào thùng rác; xoá hẳn là `trash purge`
  # ngay sau đó (cùng một lệnh, để worker của site vẫn còn vừa chạy như ở M2).
  section "nginx thêm: xoá hẳn site c khi worker của nó còn chạy (site rm rồi trash purge)"
  vroot bash -c 'curl -s -o /dev/null -w "c: %{http_code}\n" --resolve "$1:80:127.0.0.1" "http://$1/"; ps -eo user:32,pid,cmd | grep "[p]ool tpan-$2"; tpan site rm "$1" --yes --verbose; echo "site rm mã $?"; tpan trash purge "$1" --yes --verbose' _ "$C" "$(slug_of "$C")"
  ok "site rm $C thoát 0" has '^site rm mã 0'
  ok "trash purge $C thoát 0" rc_is 0
  ok "job đưa vào thùng rác và job xoá hẳn đều thành công" bash -c '[ "$(grep -c "^Job .*: thành công" <<<"$1")" = 2 ]' _ "$OUT"
  vroot bash -c "$LEFT_SCRIPT" _ "$C" "$(user_of "$C")" "$(slug_of "$C")" "$php" nginx
  ok "không còn gì của $C" hasnt '^CÒN'
  vexec curl -sS -m 10 -o /dev/null -w '%{http_code}\n' --resolve "$C:80:127.0.0.1" "http://$C/"
  ok "$C giờ rơi vào site mặc định (nginx 444, curl mã 52)" rc_is 52
  check_http "$A" "$B"
  vroot tpan site list
  ok "site list chỉ còn a và b" bash -c 'grep -qF "$2" <<<"$1" && grep -qF "$3" <<<"$1" && ! grep -qF "$4" <<<"$1"' _ "$OUT" "$A" "$B" "$C"
  for d in "$A" "$B"; do
    vroot tpan health "$d"
    ok "health $d thoát 0 sau khi xoá c" rc_is 0
  done
  vroot tpan health
  ok "health chung thoát 0" rc_is 0
}

apache_run() {
  local A=a.example.com B=b.example.com

  section "apache (0): máy sạch, triển khai tpan, cài stack"
  reset_vm
  deploy
  vroot tpan stack install --web apache --yes
  ok "stack install thoát 0" rc_is 0
  ok "job thành công" has '^Job .*: thành công'

  section "apache bước 2: tạo 2 site"
  create_sites "$A" "$B"
  check_http "$A" "$B"
  check_login "$A" "$B"
  vroot ls -l "/var/www/$A/public/.htaccess"
  ok ".htaccess có" rc_is 0

  section "apache bước 3: cô lập"
  check_isolation apache "$A" "$B"

  section "apache bước 4: chạy lại, không được đổi gì"
  rerun_same "$A" "$B"

  section "apache: mật khẩu không lọt vào DB của tpan"
  secrets_in_db
}

case "${1:-}" in
  nginx) nginx_run ;;
  apache) apache_run ;;
  all)
    nginx_run
    apache_run
    ;;
  *)
    sed -n '2,10p' "$0" | sed 's/^# \{0,1\}//'
    exit 2
    ;;
esac

if [ -n "${E2E_LOG:-}" ] && [ -f "$E2E_LOG" ]; then
  section "mật khẩu có lọt vào chính file log không"
  n=0
  while IFS= read -r s; do
    [ -n "$s" ] || continue
    n=$((n + $(grep -cF -- "$s" "$E2E_LOG")))
  done <"$SECRETS"
  printf 'Số mật khẩu đã gặp: %s; số dòng của %s chứa một trong số đó: %s\n' "$(grep -c . "$SECRETS")" "$E2E_LOG" "$n"
  ok "log không chứa mật khẩu nào" test "$n" -eq 0
fi

section "TỔNG KẾT: $FAILS khẳng định không đạt"
[ "$FAILS" -eq 0 ]
