#!/usr/bin/env bash
# e2e của mốc M3 trên máy ảo tpan-test: tpan setup, đăng nhập, CSRF, header
# bảo mật, tạo site qua API như trình duyệt làm (xem trước, chạy, log trực tiếp
# qua SSE, bằng chứng, mật khẩu quản trị một lần), số liệu máy, cơ sở dữ liệu,
# khoá toàn cục khi CLI và API cùng chạy, xoá site qua API.
# Chạy trên Mac, trong thư mục repo:
#
#   scripts/e2e-m3.sh all           từ máy sạch tới hết bước 5 (mặc định)
#   scripts/e2e-m3.sh http-up       (sau all) mở đồ nghề HTTP cho bước 4 bằng tay
#   scripts/e2e-m3.sh http-down     tắt đồ nghề đó, kiểm tpan.service vẫn chạy
#
# Đồ nghề HTTP cho bước 4 (trình duyệt): CHỈ dùng trong máy ảo test, KHÔNG
# phải cách triển khai. Trình duyệt dựng sẵn không vào được panel thật vì
# chứng chỉ tự ký, nên trong máy ảo chạy thêm một bản `tpan serve --listen
# 127.0.0.1:8080` (HTTP thường, chỉ loopback như mã cho phép; cùng tpan.db,
# cùng khoá flock với tpan.service) và socat chuyển cổng 8081 của máy ảo về
# đó. Panel thật trên máy chủ luôn là HTTPS 8443 (tpan setup); không bao giờ
# để HTTP thường nghe ra mạng như thế này ngoài máy ảo test.
#
# Đặt E2E_LOG=<file log> để cuối lần chạy script tự tìm trong chính file log
# đó: mọi mật khẩu đã gặp, IP máy ảo, IP của Mac, tên user Mac, giá trị cookie
# phiên (phải là 0 lần).
#
# Output ra stdout như e2e-m2.sh: mỗi lệnh có dấu thời gian, output nguyên văn
# (đã che) và mã thoát; mỗi khẳng định in "=> ĐẠT" hoặc "=> KHÔNG ĐẠT".
# Che trong output: mật khẩu (panel, quản trị WordPress, DB của site), giá trị
# cookie, IP máy ảo (<vm>), IP của Mac trong mạng Multipass (<mac>), thư mục
# nhà và tên user Mac. Mật khẩu và cookie chỉ nằm trong file 0600 ở thư mục tạm
# của Mac, xoá khi script kết thúc; mật khẩu không bao giờ nằm trên argv của
# lệnh được in (đi qua file).
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
trap 'jobs -p | xargs kill 2>/dev/null; rm -rf "$TMPD"' EXIT

ts() { date '+%Y-%m-%d %H:%M:%S'; }
section() { printf '\n\n######## [%s] %s\n' "$(ts)" "$*"; }

VM_IP=""
HOST_IP=""
BASE=""

# mask_str che một chuỗi (stdin) theo mọi thứ đã biết, in ra stdout.
mask_str() {
  local s
  s="$(cat)"
  [ -n "$VM_IP" ] && s="${s//"$VM_IP"/<vm>}"
  [ -n "$HOST_IP" ] && s="${s//"$HOST_IP"/<mac>}"
  s="${s//"$TMPD"/<tmp>}"
  s="${s//"$HOME"/~}"
  s="${s//"$USER"/<user>}"
  local p
  while IFS= read -r p; do
    [ -n "$p" ] && s="${s//"$p"/<mật khẩu, đã che>}"
  done <"$SECRETS"
  # Giá trị cookie (của panel lẫn của WordPress). HTTP/2 viết tên header
  # thường, HTTP/1.1 viết hoa: không phân biệt hoa thường.
  s="$(sed -E 's/^([Ss]et-[Cc]ookie: [^=]+=)[^;]*/\1<đã che>/' <<<"$s")"
  printf '%s' "$s"
}

mask() { OUT="$(mask_str <<<"$OUT")"; }

run() {
  local shown
  shown="$(mask_str <<<"$*")"
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
hasi() { grep -qiE -- "$1" <<<"$OUT"; }
hasnt() { ! grep -qE -- "$1" <<<"$OUT"; }
hasF() { grep -qF -- "$1" <<<"$OUT"; }

vexec() { run "$VM" exec "$@"; }
vroot() { run "$VM" root "$@"; }

# add_secret ghi một bí mật mới vào $SECRETS (không in).
add_secret() {
  [ -n "$1" ] || return 0
  grep -qxF -- "$1" "$SECRETS" || printf '%s\n' "$1" >>"$SECRETS"
}

slug_of() {
  local d=$1 head hash
  head="$(printf '%s' "${d:0:16}" | tr -c 'a-z0-9' '_')"
  hash="$(printf '%s' "$d" | shasum -a 256 | cut -c1-6)"
  printf '%s_%s' "$head" "$hash"
}
user_of() { printf 'tp_%s' "$(slug_of "$1")"; }

# add_dbpw <domain>: đọc mật khẩu DB của site vào $SECRETS (không in), để mọi
# output sau được che và để tìm nó trong DB của tpan.
add_dbpw() {
  local dbpw
  dbpw="$("$VM" root bash -c 'cd / && runuser -u "$2" -- wp config get DB_PASSWORD --path="/var/www/$1/public" 2>/dev/null' _ "$1" "$(user_of "$1")" | tr -d '\r')"
  add_secret "$dbpw"
  printf 'Đã đọc mật khẩu DB của %s vào danh sách che (%s ký tự).\n' "$1" "${#dbpw}"
}

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
  VM_IP="$("$VM" ip)"
  # IP của Mac như máy ảo thấy (SSH_CLIENT của multipass exec): là IP mà panel
  # ghi vào phiên, nhật ký, khoá đăng nhập.
  HOST_IP="$("$VM" exec bash -c 'echo "${SSH_CLIENT%% *}"' | tr -d '\r')"
  BASE="https://$VM_IP:8443"
  printf 'IP máy ảo: <vm>; IP của Mac trong mạng Multipass: <mac> (cả hai được che trong log)\n'
  run make vm-deploy DATE="$BUILD_DATE"
  ok "build (có build giao diện) và chép tpan vào máy ảo" rc_is 0
  run shasum -a 256 bin/tpan-linux-arm64 bin/tpan-linux-amd64
  run git log --oneline -1
  run git status --short
  run ls -la web/dist web/dist/assets
  ok "web/dist có index.html và assets" bash -c 'grep -q "index.html" <<<"$1" && grep -q "\.js$" <<<"$1"' _ "$OUT"
}

# ---------------------------------------------------------------- HTTP tới panel

JAR_NONE="$TMPD/jar-none" # luôn rỗng: request chưa đăng nhập
JAR="$JAR_NONE"
: >"$JAR_NONE"
CODE=""

# api METHOD PATH [cờ curl...]: gọi panel từ Mac, kiểm chứng chỉ bằng đúng file
# cert.pem lấy từ máy ảo (--cacert, không -k). In header và mã HTTP; thân nằm ở
# $TMPD/body (in bằng show, sau khi đã nhặt bí mật nếu có). Cookie đọc và ghi
# ở $JAR, không bao giờ in.
api() {
  local m=$1 p=$2
  shift 2
  : >"$TMPD/body"
  run curl -sS -m 120 --cacert "$TMPD/cert.pem" -b "$JAR" -c "$JAR" -X "$m" -D - -o "$TMPD/body" \
    -w '-> HTTP %{http_code}, %{time_total}s\n' "$@" "$BASE$p"
  CODE="$(sed -nE 's/^-> HTTP ([0-9]{3}),.*/\1/p' <<<"$OUT" | tail -1)"
}

code_is() { [ "$CODE" = "$1" ]; }

# show [bộ lọc jq]: in thân response (đã che). Thân không phải JSON thì in nguyên.
show() {
  local f=${1:-.}
  if jq -e . "$TMPD/body" >/dev/null 2>&1; then
    OUT="$(jq "$f" "$TMPD/body" 2>&1)"
  else
    OUT="$(head -c 4000 "$TMPD/body")"
  fi
  mask
  printf '%s\n' "$OUT"
}

# jb BỘ_LỌC: đọc một giá trị từ thân response (không in).
jb() { jq -r "$1" "$TMPD/body" 2>/dev/null; }

# login_file <file mật khẩu> <file json>: thân POST /login, mật khẩu không lên argv.
login_file() { jq -Rn '{password: input}' <"$1" >"$2"; chmod 600 "$2"; }

# SSE: sse_start <job id> <file>: mở luồng sự kiện ở nền, mỗi dòng có thời
# điểm Mac nhận được (giây, 3 số lẻ). Chờ bằng sse_wait.
SSE_PID=""
sse_start() {
  printf '\n[%s] $ curl -sN --cacert ... -b <cookie> %s/api/v1/jobs/%s/events | <gắn thời điểm nhận> (chạy nền)\n' "$(ts)" '<vm>' "$1"
  (curl -sN -m 900 --cacert "$TMPD/cert.pem" -b "$JAR" "$BASE/api/v1/jobs/$1/events" |
    perl -MTime::HiRes=time -ne 'BEGIN { $| = 1 } printf "%.3f %s", time, $_') >"$2" 2>&1 &
  SSE_PID=$!
}
sse_wait() {
  wait "$SSE_PID"
  printf '[%s] luồng SSE đã đóng (mã thoát %s)\n' "$(ts)" "$?"
}

# sse_report <file> <job id>: bảng sự kiện đã nhận và các khẳng định về log trực tiếp.
sse_report() {
  local f=$1 id=$2
  sed -nE 's/^([0-9.]+) data: (.*)$/{"recv":\1,"ev":\2}/p' "$f" >"$f.jsonl"
  printf '\nSự kiện SSE đã nhận (thời điểm nhận tính từ sự kiện đầu; time là giờ của máy ảo; message cắt 160 ký tự):\n'
  OUT="$(jq -rs '(.[0].recv) as $t0 | .[] | "+\(((.recv - $t0) * 1000 | round) / 1000)s  #\(.ev.seq)  \(.ev.kind)  \(.ev.time)  \(.ev.step_id // "")  \(if .ev.check then "[\(.ev.check.status)] \(.ev.check.id)" else (.ev.message // "" | .[0:160]) end)"' "$f.jsonl")"
  mask
  printf '%s\n' "$OUT"
  OUT="$(jq -s '(.[-1].recv) as $lr | {
      events: length,
      first: .[0].ev.kind,
      last: .[-1].ev.kind,
      last_message: .[-1].ev.message,
      job_ids: ([.[].ev.job_id] | unique),
      seq_contiguous: ([.[].ev.seq] == [range(1; length + 1)]),
      span_seconds: ($lr - .[0].recv),
      received_2s_before_done: ([.[] | select(.recv <= ($lr - 2))] | length),
      kinds: (group_by(.ev.kind) | map({(.[0].ev.kind): length}) | add),
      checks: ([.[] | select(.ev.kind == "check") | .ev.check.status] | group_by(.) | map({(.[0]): length}) | add)
    }' "$f.jsonl")"
  mask
  printf '\nTóm tắt luồng:\n%s\n' "$OUT"
  SSE_SUMMARY="$OUT"
  local raw
  raw="$(grep -c ' data: ' "$f")"
  printf 'Số dòng data: trong luồng thô: %s; số dòng id:/event:/data: %s/%s/%s\n' "$raw" \
    "$(grep -c ' id: ' "$f")" "$(grep -c ' event: ' "$f")" "$raw"
  ok "SSE: mọi sự kiện thuộc job $id" bash -c '[ "$(jq -c .job_ids <<<"$1")" = "[\"$2\"]" ]' _ "$SSE_SUMMARY" "$id"
  ok "SSE: sự kiện đầu là job.start, cuối là job.done" bash -c 'jq -e ".first == \"job.start\" and .last == \"job.done\"" <<<"$1" >/dev/null' _ "$SSE_SUMMARY"
  ok "SSE: Seq liền mạch từ 1" bash -c 'jq -e ".seq_contiguous" <<<"$1" >/dev/null' _ "$SSE_SUMMARY"
}

# check_login_wp <domain> <file mật khẩu>: đăng nhập wp-login.php như e2e M2.
check_login_wp() {
  local d=$1 f=$2 wrong="$TMPD/wrong-password"
  (umask 077 && printf '%s' 'mat-khau-sai-de-doi-chung' >"$wrong")
  run curl -sS -m 30 -D - -o /dev/null --resolve "$d:80:$VM_IP" -b 'wordpress_test_cookie=WP%20Cookie%20check' \
    -d log=admin -d testcookie=1 --data-urlencode "pwd@$f" "http://$d/wp-login.php"
  ok "$d: đăng nhập wp-login.php bằng mật khẩu GET /jobs/{id} trả: 302" has '^HTTP/1\.1 302'
  ok "$d: có cookie wordpress_logged_in_" has '^Set-Cookie: wordpress_logged_in_'
  run curl -sS -m 30 -D - -o /dev/null --resolve "$d:80:$VM_IP" -b 'wordpress_test_cookie=WP%20Cookie%20check' \
    -d log=admin -d testcookie=1 --data-urlencode "pwd@$wrong" "http://$d/wp-login.php"
  ok "$d: đối chứng mật khẩu sai: không có cookie wordpress_logged_in_" hasnt '^Set-Cookie: wordpress_logged_in_'
}

LEFT_SCRIPT='
d=$1; u=$2; slug=$3
getent passwd "$u" >/dev/null && echo "CÒN user Linux $u" || echo "không có user Linux $u"
mariadb -N -B -e "SHOW DATABASES" | grep -qx "$u" && echo "CÒN DB $u" || echo "không có DB $u"
mariadb -N -B -e "SELECT User FROM mysql.user" | grep -qx "$u" && echo "CÒN user DB $u" || echo "không có user DB $u"
for p in /var/www/$d /var/log/tpan/sites/$d /etc/php/*/fpm/pool.d/tpan-$slug.conf /run/php/tpan-$slug-8.3.sock /etc/nginx/sites-enabled/tpan-$d.conf /etc/nginx/sites-available/tpan-$d.conf; do
  if [ -e "$p" ] || [ -L "$p" ]; then echo "CÒN $p"; else echo "không có $p"; fi
done
python3 -c "import sqlite3,sys; c = sqlite3.connect(\"file:/var/lib/tpan/tpan.db?mode=ro\", uri=True); n = c.execute(\"SELECT COUNT(*) FROM sites WHERE domain = ?\", (sys.argv[1],)).fetchone()[0]; print((\"CÒN\" if n else \"không có\") + \" dòng \" + sys.argv[1] + \" trong bảng sites\")" "$d"
'

# ---------------------------------------------------------------------------
# Đồ nghề HTTP cho bước 4 (xem đầu file): hai unit tạm của systemd-run, mất khi
# stop hoặc khi máy ảo khởi động lại. --collect: socat thoát mã 143 khi bị
# SIGTERM nên unit thành "failed"; không có --collect thì unit failed nằm lại
# và lần systemd-run sau cùng tên bị từ chối.

HTTP_UNIT=tpan-e2e-http
SOCAT_UNIT=tpan-e2e-socat

http_up() {
  vroot env DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=120 install -y -q socat
  ok "cài socat (đồ nghề test)" rc_is 0
  vroot systemd-run --collect --unit="$HTTP_UNIT" --description="tpan e2e: serve HTTP loopback (chi may ao test)" /usr/local/bin/tpan serve --listen 127.0.0.1:8080
  ok "chạy bản serve HTTP 127.0.0.1:8080" rc_is 0
  vroot systemd-run --collect --unit="$SOCAT_UNIT" --description="tpan e2e: socat 8081 -> 127.0.0.1:8080 (chi may ao test)" /usr/bin/socat TCP-LISTEN:8081,fork,reuseaddr TCP:127.0.0.1:8080
  ok "chạy socat 8081 -> 127.0.0.1:8080" rc_is 0
  local i
  for i in $(seq 1 40); do
    curl -s -m 2 -o /dev/null "http://$VM_IP:8081/api/v1/health" && break
    sleep 0.25
  done
  vroot ss -tlnp
  ok "8080 chỉ nghe ở 127.0.0.1 (tpan), 8081 do socat nghe" bash -c 'grep -qE "127\.0\.0\.1:8080 .*\"tpan\"" <<<"$1" && grep -qE ":8081 .*\"socat\"" <<<"$1" && ! grep -qE "(\*|0\.0\.0\.0):8080 " <<<"$1"' _ "$OUT"
  run curl -sS -m 10 -D - -o /dev/null "http://$VM_IP:8081/"
  ok "GET http://<vm>:8081/ 200, vẫn có CSP" bash -c 'grep -qE "^HTTP/1\.1 200" <<<"$1" && grep -qi "^content-security-policy: default-src" <<<"$1"' _ "$OUT"
}

# http_check <file mật khẩu panel>: đăng nhập qua http://<vm>:8081 như trình
# duyệt; cookie phải tên "tpan", HttpOnly, SameSite=Strict, không Secure (trang
# http:// không đặt được cookie Secure).
http_check() {
  local jar="$TMPD/jar-http"
  : >"$jar"
  chmod 600 "$jar"
  login_file "$1" "$TMPD/login-http.json"
  run curl -sS -m 30 -D - -o /dev/null -b "$jar" -c "$jar" -H 'Content-Type: application/json' \
    -H "Origin: http://$VM_IP:8081" --data-binary "@$TMPD/login-http.json" "http://$VM_IP:8081/api/v1/login"
  ok "đăng nhập qua http://<vm>:8081: 204" has '^HTTP/1\.1 204'
  ok "cookie tên tpan, Path=/, HttpOnly, SameSite=Strict, không Secure" bash -c 'l="$(grep -i "^set-cookie: tpan=" <<<"$1")"; [ -n "$l" ] && grep -q "Path=/" <<<"$l" && grep -q "HttpOnly" <<<"$l" && grep -q "SameSite=Strict" <<<"$l" && ! grep -qi "secure" <<<"$l"' _ "$OUT"
  run curl -sS -m 10 -b "$jar" "http://$VM_IP:8081/api/v1/session"
  ok "phiên qua http://<vm>:8081 logged_in=true" has '"logged_in":true'
  run curl -sS -m 10 -o /dev/null -w '%{http_code}\n' -b "$jar" -X POST -H 'Content-Type: application/json' \
    -H 'Sec-Fetch-Site: cross-site' --data-binary "@$TMPD/siteC.json" "http://$VM_IP:8081/api/v1/sites?dry_run=1"
  ok "CSRF qua socat vẫn chặn cross-site: 403" has '^403$'
  run curl -sS -m 10 -o /dev/null -w '%{http_code}\n' -b "$jar" -X POST -H 'Content-Type: application/json' \
    -H 'Sec-Fetch-Site: same-origin' -H "Origin: http://$VM_IP:8081" --data-binary "@$TMPD/siteC.json" "http://$VM_IP:8081/api/v1/sites?dry_run=1"
  ok "đối chứng same-origin qua socat: 200" has '^200$'
}

http_down() {
  vroot systemctl stop "$SOCAT_UNIT" "$HTTP_UNIT"
  ok "tắt socat và bản serve HTTP" rc_is 0
  vroot ss -tlnp
  ok "không còn gì nghe 8080, 8081; tpan.service vẫn nghe 8443" bash -c '! grep -qE ":(8080|8081) " <<<"$1" && grep -qE ":8443 .*\"tpan\"" <<<"$1"' _ "$OUT"
  vexec systemctl is-active tpan
  ok "tpan.service active" has '^active'
  [ -s "$TMPD/cert.pem" ] || "$VM" root cat /etc/tpan/tls/cert.pem | tr -d '\r' >"$TMPD/cert.pem"
  run curl -sS -m 10 --cacert "$TMPD/cert.pem" -o /dev/null -w '%{http_code}\n' "$BASE/api/v1/health"
  ok "https://<vm>:8443/api/v1/health 200" has '^200$'
}

main_run() {
  local A=a.example.com B=b.example.com C=c.example.com
  local P1="$TMPD/panel-pw-setup" P2="$TMPD/panel-pw-new"

  section "bước 0: máy sạch, triển khai tpan (file chạy nhúng giao diện), cài stack nginx"
  reset_vm
  deploy
  vroot tpan stack install --web nginx --yes
  ok "stack install thoát 0" rc_is 0
  ok "job thành công" has '^Job .*: thành công'
  vexec systemctl is-active nginx php8.3-fpm mariadb
  ok "3 dịch vụ active" bash -c '[ "$(grep -cx active <<<"$1")" -eq 3 ]' _ "$OUT"
  vexec systemctl is-active tpan
  ok "chưa có tpan.service trước setup" rc_not 0

  section "bước 1: sudo tpan setup --yes"
  local cmd=("$VM" root tpan setup --yes)
  printf '\n[%s] $ %s\n' "$(ts)" "${cmd[*]}"
  OUT="$("${cmd[@]}" 2>&1)"
  RC=$?
  local pw fp hits
  pw="$(grep -A1 '^Mật khẩu quản trị (chỉ hiện MỘT lần' <<<"$OUT" | sed -nE 's/^    ([A-Z2-7]{26})\r?$/\1/p' | head -1)"
  fp="$(grep -A1 '^Dấu vân tay SHA-256' <<<"$OUT" | sed -nE 's/^    ([0-9A-F:]{95})\r?$/\1/p' | head -1)"
  hits=0
  if [ -n "$pw" ]; then
    add_secret "$pw"
    (umask 077 && printf '%s' "$pw" >"$P1")
    hits="$(grep -oF -- "$pw" <<<"$OUT" | wc -l | tr -d ' ')"
  fi
  mask
  printf '%s\n[%s] (mã thoát %s)\n' "$OUT" "$(ts)" "$RC"
  printf 'Mật khẩu quản trị panel trong output: %s lần (%s ký tự base32)\n' "$hits" "${#pw}"
  ok "setup thoát 0" rc_is 0
  ok "job setup thành công" has '^Job .*: thành công'
  ok "mật khẩu quản trị in đúng một lần" test "$hits" -eq 1
  ok "có fingerprint SHA-256 dạng AB:CD:..." test -n "$fp"
  vexec systemctl is-active tpan
  ok "tpan.service active" has '^active'
  vexec systemctl is-enabled tpan
  ok "tpan.service enabled" has '^enabled'
  vroot ss -tlnp
  ok "tpan nghe cổng 8443" has ':8443 .*"tpan"'
  vroot stat -c '%a %U:%G %n' /etc/tpan /etc/tpan/tls /etc/tpan/tls/cert.pem /etc/tpan/tls/key.pem /var/lib/tpan /var/lib/tpan/tpan.db /etc/systemd/system/tpan.service
  ok "khoá riêng 600 root" has '^600 root:root /etc/tpan/tls/key.pem$'
  vroot cat /etc/systemd/system/tpan.service
  vroot openssl x509 -in /etc/tpan/tls/cert.pem -noout -fingerprint -sha256
  ok "fingerprint setup in ra khớp openssl x509 -fingerprint -sha256 trong máy ảo" hasF "sha256 Fingerprint=$fp"
  vroot openssl x509 -in /etc/tpan/tls/cert.pem -noout -subject -dates -ext subjectAltName,keyUsage,extendedKeyUsage
  ok "SAN của chứng chỉ có IP máy ảo" hasF 'IP Address:<vm>'
  "$VM" root cat /etc/tpan/tls/cert.pem | tr -d '\r' >"$TMPD/cert.pem"
  run bash -c 'echo | openssl s_client -connect "$1" -servername localhost 2>/dev/null | openssl x509 -noout -fingerprint -sha256' _ "$VM_IP:8443"
  ok "fingerprint chứng chỉ panel đang phục vụ (đọc từ Mac bằng openssl s_client) khớp" hasF "sha256 Fingerprint=$fp"
  local pin
  pin="$(openssl x509 -in "$TMPD/cert.pem" -pubkey -noout | openssl pkey -pubin -outform der | openssl dgst -sha256 -binary | base64)"
  printf '\nsha256 của khoá công khai (để ghim): %s\n' "$pin"
  run curl -sS -m 20 -k --pinnedpubkey "sha256//$pin" -o /dev/null -w '%{http_code}\n' "$BASE/api/v1/health"
  ok "curl --pinnedpubkey đúng khoá: 200" has '^200$'
  run curl -sS -m 20 -k --pinnedpubkey "sha256//AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=" -o /dev/null -w '%{http_code}\n' "$BASE/api/v1/health"
  ok "đối chứng: ghim sai khoá thì curl từ chối (mã 90)" rc_is 90
  run curl -sS -m 20 -o /dev/null -w '%{http_code}\n' "$BASE/api/v1/health"
  ok "đối chứng: không --cacert thì curl không tin chứng chỉ tự ký (mã 60)" rc_is 60

  printf '\nTrạng thái trước khi chạy lại setup:\n'
  vroot bash -c 'sha256sum /etc/tpan/tls/cert.pem /etc/tpan/tls/key.pem /etc/systemd/system/tpan.service; systemctl show -p MainPID -p ExecMainStartTimestamp tpan; python3 -c "import sqlite3; c = sqlite3.connect(\"file:/var/lib/tpan/tpan.db?mode=ro\", uri=True); print(\"hash mật khẩu (sha256 của hash):\", __import__(\"hashlib\").sha256(c.execute(\"SELECT value FROM settings WHERE key = ?\", (\"admin.password_hash\",)).fetchone()[0].encode()).hexdigest()[:16])"'
  local before="$OUT"
  section "bước 1: chạy lại setup: không đổi gì, không mật khẩu mới"
  vroot tpan setup --yes
  ok "chạy lại setup thoát 0" rc_is 0
  ok "không in mật khẩu mới (không dòng mật khẩu base32 nào)" hasnt '^    [A-Z2-7]{26}$'
  ok "báo mật khẩu đã có, giữ nguyên" hasF 'đã có từ trước, giữ nguyên'
  ok "không bước nào chạy (không có dòng \"xong\")" hasnt '^  xong$'
  ok "cùng fingerprint" hasF "$fp"
  vroot bash -c 'sha256sum /etc/tpan/tls/cert.pem /etc/tpan/tls/key.pem /etc/systemd/system/tpan.service; systemctl show -p MainPID -p ExecMainStartTimestamp tpan; python3 -c "import sqlite3; c = sqlite3.connect(\"file:/var/lib/tpan/tpan.db?mode=ro\", uri=True); print(\"hash mật khẩu (sha256 của hash):\", __import__(\"hashlib\").sha256(c.execute(\"SELECT value FROM settings WHERE key = ?\", (\"admin.password_hash\",)).fetchone()[0].encode()).hexdigest()[:16])"'
  ok "cert, key, unit, PID dịch vụ, hash mật khẩu không đổi" test "$OUT" = "$before"

  section "bước 2: từ Mac tới https://<vm>:8443 (curl --cacert cert.pem của panel)"
  JAR="$JAR_NONE"
  api GET /
  ok "GET / 200" code_is 200
  ok "CSP default-src 'self' và frame-ancestors 'none'" hasi "^content-security-policy: default-src 'self'; frame-ancestors 'none'"
  ok "X-Content-Type-Options: nosniff" hasi '^x-content-type-options: nosniff'
  ok "Referrer-Policy: no-referrer" hasi '^referrer-policy: no-referrer'
  show
  local asset
  asset="$(grep -oE 'src="/assets/[^"]+\.js"' "$TMPD/body" | head -1 | sed -E 's/src="(.*)"/\1/')"
  ok "index.html nhúng trong file chạy có thẻ script trỏ tới /assets/*.js" test -n "$asset"
  api GET "$asset" -o /dev/null
  ok "GET $asset 200, kiểu javascript" bash -c '[ "$1" = 200 ] && grep -qi "^content-type: text/javascript" <<<"$2"' _ "$CODE" "$OUT"
  api GET /sites/new
  ok "đường dẫn của giao diện (/sites/new) trả index.html" bash -c '[ "$1" = 200 ] && grep -q "<div id=\"root\">" "$2"' _ "$CODE" "$TMPD/body"
  api GET /api/v1/session
  show
  ok "/api/v1/session logged_in=false" bash -c '[ "$(jq -r .logged_in "$1")" = false ]' _ "$TMPD/body"
  api GET /api/v1/overview
  show
  ok "/api/v1/overview khi chưa đăng nhập: 401 unauthorized" bash -c '[ "$1" = 401 ] && [ "$(jq -r .error.code "$2")" = unauthorized ]' _ "$CODE" "$TMPD/body"

  section "bước 2: đăng nhập bằng mật khẩu setup, rồi sai 5 lần"
  login_file "$P1" "$TMPD/login1.json"
  local J1="$TMPD/jar1"
  : >"$J1"
  chmod 600 "$J1"
  JAR="$J1"
  api POST /api/v1/login -H 'Content-Type: application/json' --data-binary "@$TMPD/login1.json"
  ok "đăng nhập bằng mật khẩu setup: 204" code_is 204
  api GET /api/v1/session
  show
  ok "phiên 1 logged_in=true" bash -c '[ "$(jq -r .logged_in "$1")" = true ]' _ "$TMPD/body"
  JAR="$JAR_NONE"
  printf '%s' '{"password":"mat-khau-sai-de-thu"}' >"$TMPD/wrong.json"
  local i
  for i in 1 2 3 4 5; do
    api POST /api/v1/login -H 'Content-Type: application/json' --data-binary "@$TMPD/wrong.json"
    local hdr="$OUT"
    show '.error.code'
    if [ "$i" -lt 5 ]; then
      ok "lần sai $i: 401 wrong_password" bash -c '[ "$1" = 401 ] && [ "$(jq -r .error.code "$2")" = wrong_password ]' _ "$CODE" "$TMPD/body"
    else
      ok "lần sai 5: 423 login_locked, có Retry-After" bash -c '[ "$1" = 423 ] && [ "$(jq -r .error.code "$2")" = login_locked ] && grep -qi "^retry-after: [0-9]" <<<"$3"' _ "$CODE" "$TMPD/body" "$hdr"
    fi
  done
  JAR="$TMPD/jar-tmp"
  : >"$JAR"
  api POST /api/v1/login -H 'Content-Type: application/json' --data-binary "@$TMPD/login1.json"
  show
  ok "đúng mật khẩu nhưng IP đang khoá: vẫn 423" code_is 423
  ok "không đặt cookie khi bị khoá" hasnt '^[Ss]et-[Cc]ookie'

  section "bước 2: sudo tpan admin passwd gỡ khoá, huỷ phiên, in mật khẩu mới"
  cmd=("$VM" root tpan admin passwd --yes)
  printf '\n[%s] $ %s\n' "$(ts)" "${cmd[*]}"
  OUT="$("${cmd[@]}" 2>&1)"
  RC=$?
  pw="$(grep -A1 '^Mật khẩu quản trị mới' <<<"$OUT" | sed -nE 's/^    ([A-Z2-7]{26})\r?$/\1/p' | head -1)"
  hits=0
  if [ -n "$pw" ]; then
    add_secret "$pw"
    (umask 077 && printf '%s' "$pw" >"$P2")
    hits="$(grep -oF -- "$pw" <<<"$OUT" | wc -l | tr -d ' ')"
  fi
  mask
  printf '%s\n[%s] (mã thoát %s)\n' "$OUT" "$(ts)" "$RC"
  ok "admin passwd thoát 0" rc_is 0
  ok "mật khẩu mới in đúng một lần" test "$hits" -eq 1
  ok "mật khẩu mới khác mật khẩu setup" bash -c '! cmp -s "$1" "$2"' _ "$P1" "$P2"
  JAR="$J1"
  api GET /api/v1/session
  show
  ok "phiên 1 (đăng nhập trước khi đặt lại) đã bị huỷ: logged_in=false" bash -c '[ "$(jq -r .logged_in "$1")" = false ]' _ "$TMPD/body"
  JAR="$TMPD/jar-tmp"
  : >"$JAR"
  api POST /api/v1/login -H 'Content-Type: application/json' --data-binary "@$TMPD/login1.json"
  show '.error.code'
  ok "mật khẩu setup cũ: 401 (IP đã được gỡ khoá, không còn 423)" code_is 401
  login_file "$P2" "$TMPD/login2.json"
  JAR="$TMPD/jar"
  : >"$JAR"
  chmod 600 "$JAR"
  api POST /api/v1/login -H 'Content-Type: application/json' --data-binary "@$TMPD/login2.json"
  ok "mật khẩu mới: 204" code_is 204
  ok "cookie __Host-tpan có Path=/, Secure, HttpOnly, SameSite=Strict" bash -c 'l="$(grep -i "^set-cookie: __Host-tpan=" <<<"$1")"; [ -n "$l" ] && grep -q "Path=/" <<<"$l" && grep -q "; Secure" <<<"$l" && grep -q "; HttpOnly" <<<"$l" && grep -q "SameSite=Strict" <<<"$l" && ! grep -qi "domain=" <<<"$l"' _ "$OUT"
  api GET /api/v1/session
  show '.hostname = "<hostname>" | .'
  ok "logged_in=true" bash -c '[ "$(jq -r .logged_in "$1")" = true ]' _ "$TMPD/body"
  api GET /api/v1/account/sessions
  show
  ok "chỉ còn đúng 1 phiên (phiên mới)" bash -c '[ "$(jq length "$1")" = 1 ] && [ "$(jq -r ".[0].current" "$1")" = true ]' _ "$TMPD/body"

  section "bước 2: CSRF (request có thay đổi, đã đăng nhập)"
  printf '{"domain":"%s","title":"Site A","email":"%s"}' "$A" "$EMAIL" >"$TMPD/siteA.json"
  printf '{"domain":"%s","title":"Site B","email":"%s"}' "$B" "$EMAIL" >"$TMPD/siteB.json"
  printf '{"domain":"%s","title":"Site C","email":"%s"}' "$C" "$EMAIL" >"$TMPD/siteC.json"
  api POST '/api/v1/sites?dry_run=1' -H 'Content-Type: application/json' -H 'Sec-Fetch-Site: cross-site' --data-binary "@$TMPD/siteA.json"
  show
  ok "Sec-Fetch-Site: cross-site: 403 csrf" bash -c '[ "$1" = 403 ] && [ "$(jq -r .error.code "$2")" = csrf ]' _ "$CODE" "$TMPD/body"
  api POST '/api/v1/sites?dry_run=1' -H 'Content-Type: application/json' -H 'Origin: https://evil.example' --data-binary "@$TMPD/siteA.json"
  show '.error.code'
  ok "Origin lạ (không có Sec-Fetch-Site): 403 csrf" bash -c '[ "$1" = 403 ] && [ "$(jq -r .error.code "$2")" = csrf ]' _ "$CODE" "$TMPD/body"
  api DELETE "/api/v1/sites/$A?dry_run=1" -H 'Sec-Fetch-Site: cross-site'
  show '.error.code'
  ok "DELETE cross-site: 403 csrf" code_is 403
  api POST /api/v1/logout -H 'Sec-Fetch-Site: cross-site'
  show '.error.code'
  ok "logout cross-site: 403 (trang khác không đá được người dùng ra)" code_is 403
  api POST '/api/v1/sites?dry_run=1' -H 'Content-Type: application/json' -H 'Sec-Fetch-Site: same-origin' -H "Origin: $BASE" --data-binary "@$TMPD/siteA.json"
  ok "đối chứng: Sec-Fetch-Site same-origin, Origin của chính panel: không bị chặn (200)" code_is 200

  section "bước 3: tạo site $A qua API như trình duyệt: xem trước"
  api POST '/api/v1/sites?dry_run=1' -H 'Content-Type: application/json' -H 'Sec-Fetch-Site: same-origin' -H "Origin: $BASE" --data-binary "@$TMPD/siteA.json"
  show '{kind, target, title: .title.vi, steps: [.steps[] | "\(if .done then "[đã đúng] " else "" end)\(.info.id): \(.info.title.vi)\(if .info.files then " (" + ([.info.files[].path] | join(", ")) + ")" else "" end)"]}'
  ok "Preview 200, kind site.add, target $A, có bước" bash -c '[ "$1" = 200 ] && jq -e ".kind == \"site.add\" and .target == \"$3\" and (.steps | length) > 5" "$2" >/dev/null' _ "$CODE" "$TMPD/body" "$A"
  vexec bash -c "ls /var/www; id $(user_of "$A") 2>&1"
  ok "xem trước không tạo gì trên máy (không thư mục, không user)" bash -c '! grep -qxF "$2" <<<"$1" && grep -qF "no such user" <<<"$1"' _ "$OUT" "$A"

  section "bước 3: chạy thật, nghe SSE trong lúc job chạy"
  api POST /api/v1/sites -H 'Content-Type: application/json' -H 'Sec-Fetch-Site: same-origin' -H "Origin: $BASE" --data-binary "@$TMPD/siteA.json"
  show
  ok "POST /api/v1/sites: 202 có job_id" bash -c '[ "$1" = 202 ] && jq -e ".job_id | test(\"^[0-9a-f]{16}$\")" "$2" >/dev/null' _ "$CODE" "$TMPD/body"
  local JA
  JA="$(jb .job_id)"
  sse_start "$JA" "$TMPD/sseA"
  sleep 2
  api GET "/api/v1/jobs/$JA"
  show '{job: .job, events: (.events | length), has_secret: has("secret")}'
  ok "2 giây sau: job vẫn running, đã có sự kiện, chưa có secret" bash -c 'jq -e ".job.status == \"running\" and (.events | length) > 0 and (has(\"secret\") | not)" "$1" >/dev/null' _ "$TMPD/body"
  run cat "$TMPD/sseA"
  printf '(đã in ở trên: những gì luồng SSE nhận được tới lúc này, %s dòng)\n' "$(wc -l <"$TMPD/sseA" | tr -d ' ')"
  sse_wait
  sse_report "$TMPD/sseA" "$JA"
  ok "SSE: job.done báo succeeded" bash -c 'jq -e ".last_message == \"succeeded\"" <<<"$1" >/dev/null' _ "$SSE_SUMMARY"
  ok "SSE: log trực tiếp: luồng kéo dài trên 10 giây" bash -c 'jq -e ".span_seconds > 10" <<<"$1" >/dev/null' _ "$SSE_SUMMARY"
  ok "SSE: hơn 20 sự kiện tới trước job.done ít nhất 2 giây" bash -c 'jq -e ".received_2s_before_done > 20" <<<"$1" >/dev/null' _ "$SSE_SUMMARY"
  ok "SSE: có phép kiểm (bằng chứng), không phép kiểm nào fail/unknown" bash -c 'jq -e "(.checks.pass // 0) > 0 and (.checks.fail // 0) == 0 and (.checks.unknown // 0) == 0" <<<"$1" >/dev/null' _ "$SSE_SUMMARY"

  section "bước 3: GET /api/v1/jobs/{id}: lần đầu có mật khẩu quản trị WordPress, lần hai không"
  api GET "/api/v1/jobs/$JA"
  local wpw
  wpw="$(jb '.secret.password // empty')"
  add_secret "$wpw"
  [ -n "$wpw" ] && (umask 077 && printf '%s' "$wpw" >"$TMPD/admin-$A")
  add_dbpw "$A"
  show '{job: .job, events: (.events | length), secret: .secret}'
  ok "lần đầu: job succeeded, có secret (url, user, password)" bash -c 'jq -e ".job.status == \"succeeded\" and (.secret.password | length) == 32 and .secret.user != \"\" and (.secret.url | test(\"^http://a\\\\.example\\\\.com/wp-admin/$\"))" "$1" >/dev/null' _ "$TMPD/body"
  local nev
  nev="$(jb '.events | length')"
  ok "GET /jobs có đủ sự kiện như luồng SSE đã nhận" bash -c '[ "$1" = "$(jq .events <<<"$2")" ]' _ "$nev" "$SSE_SUMMARY"
  api GET "/api/v1/jobs/$JA"
  show '{job: .job.status, events: (.events | length), has_secret: has("secret")}'
  ok "lần hai: không còn secret" bash -c 'jq -e "has(\"secret\") | not" "$1" >/dev/null' _ "$TMPD/body"
  check_login_wp "$A" "$TMPD/admin-$A"

  section "bước 3: GET /api/v1/sites/$A: phép kiểm theo đường đi request"
  api GET "/api/v1/sites/$A"
  show '{site: .site, web, checks: [.checks[] | "\(.status)  \(.stage)  \(.id)  \(.summary.vi)"]}'
  ok "200, mọi phép kiểm pass" bash -c '[ "$1" = 200 ] && jq -e "(.checks | length) > 0 and all(.checks[]; .status == \"pass\")" "$2" >/dev/null' _ "$CODE" "$TMPD/body"
  ok "phép kiểm của site không còn config.phpfpm (đã chuyển hẳn sang phép kiểm stack, commit b94a707)" bash -c '! jq -e "any(.checks[]; .id | startswith(\"config.phpfpm\"))" "$1" >/dev/null' _ "$TMPD/body"
  printf '\nBằng chứng (lệnh và output) của phép kiểm đầu tiên:\n'
  show '.checks[0].evidence'

  section "bước 3: GET /api/v1/machine so với /proc của máy ảo"
  api GET /api/v1/machine
  show
  cp "$TMPD/body" "$TMPD/machine-idle"
  vexec bash -c 'nproc; grep -E "^(MemTotal|MemAvailable|SwapTotal|SwapFree):" /proc/meminfo; cat /proc/loadavg /proc/uptime; df -B1 --output=size,used,avail,target /var/www; stat -f -c "blocks=%b bfree=%f bavail=%a bsize=%S" /var/www'
  local proc="$OUT"
  ok "cpus = nproc" bash -c '[ "$(jq .cpus "$1")" = "$(head -1 <<<"$2")" ]' _ "$TMPD/body" "$proc"
  ok "mem_total = MemTotal × 1024" bash -c '[ "$(jq .mem_total "$1")" = "$(( $(awk "/^MemTotal:/ {print \$2}" <<<"$2") * 1024 ))" ]' _ "$TMPD/body" "$proc"
  ok "disk.total = df size của /var/www" bash -c '[ "$(jq .disk.total "$1")" = "$(awk "\$NF == \"/\" {print \$1}" <<<"$2")" ]' _ "$TMPD/body" "$proc"
  ok "cpu_percent đo được (0..100, khác -1)" bash -c 'jq -e ".cpu_percent >= 0 and .cpu_percent <= 100" "$1" >/dev/null' _ "$TMPD/body"
  ok "không có lỗi đọc" bash -c 'jq -e "(.errors // []) | length == 0" "$1" >/dev/null' _ "$TMPD/body"
  printf '\n[%s] Tạo tải: chạy `yes` trên mọi CPU của máy ảo trong 8 giây, đọc /machine giữa chừng.\n' "$(ts)"
  "$VM" exec bash -c 'for i in $(seq "$(nproc)"); do timeout 8 yes >/dev/null & done; wait' >/dev/null 2>&1 &
  local LOADPID=$!
  sleep 3
  api GET /api/v1/machine
  show '{cpu_percent, cpus, load1}'
  local busy idle
  busy="$(jb .cpu_percent)"
  idle="$(jq .cpu_percent "$TMPD/machine-idle")"
  wait "$LOADPID"
  printf 'CPU%% lúc rỗi: %s; lúc chạy yes trên mọi CPU: %s\n' "$idle" "$busy"
  ok "CPU% lúc có tải > 80 và lớn hơn lúc rỗi" bash -c 'awk -v b="$1" -v i="$2" "BEGIN { exit !(b > 80 && b > i) }"' _ "$busy" "$idle"

  section "bước 3: GET /api/v1/databases so với câu SQL chạy tay"
  api GET /api/v1/databases
  show
  local db
  db="$(user_of "$A")"
  ok "dung lượng và số bảng khác -1, lớn hơn 0" bash -c 'jq -e "length == 1 and .[0].size_bytes > 0 and .[0].tables > 0" "$1" >/dev/null' _ "$TMPD/body"
  local sql="SELECT s.SCHEMA_NAME, COUNT(t.TABLE_NAME), COALESCE(SUM(t.DATA_LENGTH + t.INDEX_LENGTH), 0) FROM information_schema.SCHEMATA s LEFT JOIN information_schema.TABLES t ON t.TABLE_SCHEMA = s.SCHEMA_NAME AND t.TABLE_TYPE IN ('BASE TABLE', 'SYSTEM VERSIONED') WHERE s.SCHEMA_NAME IN ('$db') GROUP BY s.SCHEMA_NAME;"
  printf '\nCâu SQL của core.Databases (databaseSizeSQL), chạy nguyên văn bằng mariadb -N -B, và kiểu cột của nó:\n'
  vroot mariadb -N -B -e "$sql"
  local raw="$OUT"
  ok "output thô: số nguyên, không có phần thập phân" bash -c 'grep -qE "^[a-z0-9_]+	[0-9]+	[0-9]+\r?$" <<<"$1"' _ "$raw"
  vroot mariadb -t --column-type-info -e "$sql"
  vroot mariadb -N -B -e "SELECT COUNT(*), SUM(DATA_LENGTH), SUM(INDEX_LENGTH) FROM information_schema.TABLES WHERE TABLE_SCHEMA = '$db'"
  local indep="$OUT"
  ok "API khớp câu SQL chạy tay (số bảng, byte)" bash -c '[ "$(jq -r ".[0] | \"\(.db)\t\(.tables)\t\(.size_bytes)\"" "$1")" = "$(tr -d "\r" <<<"$2")" ]' _ "$TMPD/body" "$raw"
  ok "số bảng khớp phép đếm độc lập" bash -c '[ "$(jq ".[0].tables" "$1")" = "$(cut -f1 <<<"$2" | tr -d "\r")" ]' _ "$TMPD/body" "$indep"

  section "mục 16: khoá toàn cục khi CLI đang chạy job và API cũng gọi"
  printf '\n[%s] $ %s root tpan site add %s --email %s --yes --verbose   (chạy nền, output vào file tạm)\n' "$(ts)" "$VM" "$B" "$EMAIL"
  "$VM" root tpan site add "$B" --email "$EMAIL" --yes --verbose >"$TMPD/cli-b" 2>&1 &
  local CLIPID=$!
  local held=no
  for i in $(seq 1 60); do
    if ! "$VM" root flock -n /run/tpan/tpan.lock true >/dev/null 2>&1; then
      held=yes
      break
    fi
    sleep 0.5
  done
  printf '[%s] flock -n /run/tpan/tpan.lock (từ một tiến trình khác trong máy ảo): khoá đang bị giữ = %s (sau %s lần thử)\n' "$(ts)" "$held" "$i"
  ok "CLI đang giữ khoá toàn cục" test "$held" = yes
  api POST /api/v1/sites -H 'Content-Type: application/json' -H 'Sec-Fetch-Site: same-origin' -H "Origin: $BASE" --data-binary "@$TMPD/siteC.json"
  local t
  t="$(sed -nE 's/^-> HTTP [0-9]+, ([0-9.]+)s$/\1/p' <<<"$OUT")"
  show
  ok "API trả 409 job_running" bash -c '[ "$1" = 409 ] && [ "$(jq -r .error.code "$2")" = job_running ]' _ "$CODE" "$TMPD/body"
  ok "sau khoảng 2 giây chờ khoá (1.9 ≤ t < 5): $t s" bash -c 'awk -v t="$1" "BEGIN { exit !(t >= 1.9 && t < 5) }"' _ "$t"
  api POST '/api/v1/sites?dry_run=1' -H 'Content-Type: application/json' -H 'Sec-Fetch-Site: same-origin' -H "Origin: $BASE" --data-binary "@$TMPD/siteC.json"
  ok "xem trước vẫn được trong lúc CLI chạy (không cần khoá): 200" code_is 200
  api GET /api/v1/overview
  ok "đọc /overview trong lúc CLI chạy: 200" code_is 200
  printf '\n[%s] Chờ CLI xong...\n' "$(ts)"
  wait "$CLIPID"
  RC=$?
  OUT="$(cat "$TMPD/cli-b")"
  pw="$(sed -nE 's/^  Mật khẩu: +([A-Za-z0-9]{32})\r?$/\1/p' <<<"$OUT" | head -1)"
  add_secret "$pw"
  add_dbpw "$B"
  mask
  printf '%s\n[%s] (mã thoát %s)\n' "$OUT" "$(ts)" "$RC"
  ok "CLI site add $B thoát 0" rc_is 0
  ok "job CLI thành công" has '^Job .*: thành công'
  ok "CLI không gặp 'database is locked'" hasnt 'database is locked|SQLITE_BUSY'
  vexec bash -c "ls /var/www; id $(user_of "$C") 2>&1; echo"
  ok "site C không được tạo (không thư mục, không user)" bash -c '! grep -qxF "$2" <<<"$1" && grep -qF "no such user" <<<"$1"' _ "$OUT" "$C"
  api GET /api/v1/jobs
  show '[.[] | {id, kind, target, status}]'
  local JB
  JB="$(jq -r --arg d "$B" '[.[] | select(.kind == "site.add" and .target == $d)][0].id' "$TMPD/body")"
  printf '\nSSE của job CLI (đã xong, ở tiến trình khác): gửi hết từ store rồi đóng.\n'
  sse_start "$JB" "$TMPD/sseB"
  sse_wait
  sse_report "$TMPD/sseB" "$JB"

  section "mục 16: GET /api/v1/overview lần đầu với 2 site (sau khi khởi động lại tpan, bộ nhớ đệm phép kiểm rỗng)"
  vroot systemctl restart tpan
  ok "restart tpan" rc_is 0
  for i in $(seq 1 30); do
    curl -s -m 2 --cacert "$TMPD/cert.pem" -o /dev/null "$BASE/api/v1/health" && break
    sleep 0.5
  done
  api GET /api/v1/session
  ok "phiên còn sau khi khởi động lại (nằm trong DB)" bash -c '[ "$(jq -r .logged_in "$1")" = true ]' _ "$TMPD/body"
  api GET /api/v1/overview
  local t1
  t1="$(sed -nE 's/^-> HTTP [0-9]+, ([0-9.]+)s$/\1/p' <<<"$OUT")"
  show '{hostname: "<hostname>", stack, sites: [.sites[] | {domain: .site.domain, checks: [.checks[] | .status] | group_by(.) | map({(.[0]): length}) | add}], machine: .machine | {cpu_percent, cpus, mem_total, mem_used, disk}, jobs: [.jobs[] | "\(.kind) \(.target) \(.status)"], stack_checks: [.stack_checks[] | "\(.status) \(.id)"]}'
  ok "overview 200, 2 tuyến, mọi phép kiểm site và stack pass" bash -c '[ "$1" = 200 ] && jq -e "(.sites | length) == 2 and all(.sites[].checks[]; .status == \"pass\") and all(.stack_checks[]; .status == \"pass\") and .stack.installed and .stack.web == \"nginx\"" "$2" >/dev/null' _ "$CODE" "$TMPD/body"
  api GET /api/v1/overview
  local t2
  t2="$(sed -nE 's/^-> HTTP [0-9]+, ([0-9.]+)s$/\1/p' <<<"$OUT")"
  printf '\nThời gian GET /overview: lần đầu (bộ nhớ đệm rỗng, 2 site) %s s; lần hai (đệm còn hạn) %s s\n' "$t1" "$t2"
  # CPU% của /overview phải là tải của máy, không phải tải của chính các phép
  # kiểm mà /overview chạy (lỗi tìm thấy ở lần chạy đầu: 15-30% khi máy rỗi).
  local cpus=""
  for i in 1 2 3; do
    api GET '/api/v1/overview?refresh=1'
    cpus="$cpus $(jb .machine.cpu_percent)"
  done
  api GET /api/v1/machine
  show '{cpu_percent}'
  printf '\nCPU%% trong 3 lần GET /overview?refresh=1 (chạy lại mọi phép kiểm):%s; /machine riêng: %s\n' "$cpus" "$(jb .cpu_percent)"
  ok "CPU% của /overview khi máy rỗi dưới 10 (không đếm tải của chính phép kiểm)" bash -c 'for c in $1; do awk -v c="$c" "BEGIN { exit !(c >= 0 && c < 10) }" || exit 1; done' _ "$cpus"

  section "bước 5: xoá site $B qua API (xem trước rồi chạy thật)"
  api DELETE "/api/v1/sites/$B?dry_run=1" -H 'Sec-Fetch-Site: same-origin' -H "Origin: $BASE"
  show '{kind, target, steps: [.steps[] | "\(if .info.danger then "[nguy hiểm] " else "" end)\(.info.id): \(.info.title.vi)"]}'
  ok "Preview 200, kind site.rm" bash -c '[ "$1" = 200 ] && jq -e ".kind == \"site.rm\" and .target == \"$3\"" "$2" >/dev/null' _ "$CODE" "$TMPD/body" "$B"
  vexec ls /var/www
  ok "xem trước không xoá gì" hasF "$B"
  api DELETE "/api/v1/sites/$B" -H 'Sec-Fetch-Site: same-origin' -H "Origin: $BASE"
  show
  ok "DELETE: 202 có job_id" code_is 202
  local JD
  JD="$(jb .job_id)"
  sse_start "$JD" "$TMPD/sseD"
  sse_wait
  sse_report "$TMPD/sseD" "$JD"
  ok "job xoá succeeded" bash -c 'jq -e ".last_message == \"succeeded\"" <<<"$1" >/dev/null' _ "$SSE_SUMMARY"
  api GET "/api/v1/sites/$B"
  show
  ok "GET /sites/$B: 404 site_not_found" bash -c '[ "$1" = 404 ] && [ "$(jq -r .error.code "$2")" = site_not_found ]' _ "$CODE" "$TMPD/body"
  api GET /api/v1/sites
  show '[.[].domain]'
  ok "chỉ còn $A" bash -c 'jq -e "[.[].domain] == [\"$2\"]" "$1" >/dev/null' _ "$TMPD/body" "$A"
  vroot bash -c "$LEFT_SCRIPT" _ "$B" "$(user_of "$B")" "$(slug_of "$B")"
  ok "không còn gì của $B" hasnt '^CÒN'
  vexec curl -sS -m 10 -o /dev/null -w '%{http_code}\n' --resolve "$B:80:127.0.0.1" "http://$B/"
  ok "$B giờ rơi vào site mặc định (nginx 444, curl mã 52)" rc_is 52
  vexec curl -sS -m 10 -o /dev/null -w '%{http_code}\n' --resolve "$A:80:127.0.0.1" "http://$A/"
  ok "$A vẫn 200" has '^200$'

  section "bước 4 (đồ nghề, chỉ máy ảo test): bản serve HTTP loopback + socat 8081, cookie tpan qua http://<vm>:8081"
  http_up
  http_check "$P2"
  http_down

  section "nhật ký audit và log dịch vụ"
  vroot python3 -c 'import sqlite3; c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True); [print(*r) for r in c.execute("SELECT time, ip, action, target, result, job_id FROM audit ORDER BY id")]'
  ok "audit có login.fail, locked, admin.passwd, login, site.add job_running, site.rm" bash -c 'for a in "<mac> login.fail" " locked" "cli admin.passwd" "login  ok" "site.add c.example.com job_running" "site.rm b.example.com ok"; do grep -qF -- "$a" <<<"$1" || { echo "thiếu: $a"; exit 1; }; done' _ "$OUT"
  "$VM" root journalctl -u tpan --no-pager -o short-iso >"$TMPD/journal" 2>&1
  printf '\nLog của tpan.service: %s dòng; "database is locked": %s; level=ERROR: %s\n' "$(wc -l <"$TMPD/journal" | tr -d ' ')" \
    "$(grep -c 'database is locked' "$TMPD/journal")" "$(grep -c 'level=ERROR' "$TMPD/journal")"
  OUT="$(grep -E 'level=(ERROR|WARN)' "$TMPD/journal")"
  mask
  printf '%s\n' "$OUT"
  ok "log dịch vụ không có 'database is locked'" bash -c '! grep -q "database is locked" "$1"' _ "$TMPD/journal"

  section "mật khẩu không lọt vào DB, log dịch vụ, luồng SSE"
  local f n s k=0
  for f in tpan.db tpan.db-wal; do
    "$VM" root cat "/var/lib/tpan/$f" >"$TMPD/$f" 2>/dev/null || : >"$TMPD/$f"
  done
  n="$(cat "$TMPD/tpan.db" "$TMPD/tpan.db-wal" | grep -acF 'site.add')"
  printf 'Đối chứng: chuỗi "site.add" có trong tpan.db + WAL ở %s dòng\n' "$n"
  ok "đối chứng: cách tìm này thấy được chữ có thật trong DB" test "$n" -gt 0
  while IFS= read -r s; do
    [ -n "$s" ] || continue
    k=$((k + 1))
    local nd nj ns
    nd="$(cat "$TMPD/tpan.db" "$TMPD/tpan.db-wal" | grep -acF -- "$s")"
    nj="$(grep -cF -- "$s" "$TMPD/journal")"
    ns="$(cat "$TMPD/sseA" "$TMPD/sseB" "$TMPD/sseD" | grep -cF -- "$s")"
    printf 'Bí mật thứ %s: tpan.db + WAL %s dòng; log dịch vụ %s dòng; luồng SSE %s dòng\n' "$k" "$nd" "$nj" "$ns"
    ok "bí mật thứ $k không nằm trong DB, log dịch vụ, SSE" test "$nd" -eq 0 -a "$nj" -eq 0 -a "$ns" -eq 0
  done <"$SECRETS"
  ok "đã kiểm 2 mật khẩu panel, 2 mật khẩu WordPress, 2 mật khẩu DB" test "$k" -eq 6
}

case "${1:-all}" in
  all) main_run ;;
  http-up)
    VM_IP="$("$VM" ip)"
    HOST_IP="$("$VM" exec bash -c 'echo "${SSH_CLIENT%% *}"' | tr -d '\r')"
    section "bước 4 (đồ nghề, chỉ máy ảo test): mở bản serve HTTP loopback + socat 8081"
    http_up
    printf '\nMở bằng trình duyệt: http://<vm>:8081/ . Xong thì: scripts/e2e-m3.sh http-down\n'
    ;;
  http-down)
    VM_IP="$("$VM" ip)"
    HOST_IP="$("$VM" exec bash -c 'echo "${SSH_CLIENT%% *}"' | tr -d '\r')"
    BASE="https://$VM_IP:8443"
    section "bước 4 (đồ nghề): tắt bản serve HTTP và socat"
    http_down
    ;;
  *)
    sed -n '2,12p' "$0" | sed 's/^# \{0,1\}//'
    exit 2
    ;;
esac

if [ -n "${E2E_LOG:-}" ] && [ -f "$E2E_LOG" ]; then
  section "bí mật, IP, tên user Mac, cookie có lọt vào chính file log không"
  n=0
  while IFS= read -r s; do
    [ -n "$s" ] || continue
    n=$((n + $(grep -cF -- "$s" "$E2E_LOG")))
  done <"$SECRETS"
  printf 'Số bí mật đã gặp: %s; số dòng của log chứa một trong số đó: %s\n' "$(grep -c . "$SECRETS")" "$n"
  ok "log không chứa mật khẩu nào" test "$n" -eq 0
  ok "log không chứa IP máy ảo" bash -c '! grep -qF -- "$1" "$2"' _ "$VM_IP" "$E2E_LOG"
  ok "log không chứa IP của Mac" bash -c '! grep -qF -- "$1" "$2"' _ "$HOST_IP" "$E2E_LOG"
  ok "log không chứa thư mục nhà hay tên user Mac" bash -c '! grep -qF -- "$1" "$3" && ! grep -qF -- "$2" "$3"' _ "$HOME" "$USER" "$E2E_LOG"
  ok "log không chứa giá trị cookie nào" bash -c '! grep -iE "^set-cookie: [^=]+=" "$1" | grep -vqE "=<đã che>"' _ "$E2E_LOG"
fi

section "TỔNG KẾT: $FAILS khẳng định không đạt"
[ "$FAILS" -eq 0 ]
