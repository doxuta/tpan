#!/usr/bin/env bash
# e2e của mốc M5 (SSL, trang bảo mật, ufw, fail2ban) trên máy ảo của mốc
# (tên trong file .tpan-vm; scripts/vm.sh tự đọc). Chạy trên Mac, trong thư mục repo:
#
#   scripts/e2e-m5.sh all       nginx rồi apache, mỗi bên từ máy sạch (mặc định)
#   scripts/e2e-m5.sh nginx     chỉ lượt nginx (kể cả phần bảo mật, chạy cuối)
#   scripts/e2e-m5.sh apache    chỉ lượt apache
#
# Mỗi lượt: máy sạch, cài stack, dựng máy chủ ACME thử, tạo 2 site, các lời từ
# chối trước khi gọi ACME, xin chứng chỉ, kiểm độc lập (https, 301, thử thách
# không bị chuyển hướng, 443 với tên lạ, hook gia hạn chạy thật), chạy lại
# không đổi gì, xoá site thì chứng chỉ bị xoá. Lượt nginx thêm `tpan setup`,
# API /ssl và /security, fail2ban và ufw (cuối cùng, vì ufw đụng SSH mà
# `multipass exec` dùng).
#
# ĐỒ NGHỀ TEST, CHỈ TRONG MÁY ẢO (không phải cách triển khai):
#   - Máy ảo không có tên miền thật nên không xin được chứng chỉ của Let's
#     Encrypt. Thay vào đó chạy `pebble` (máy chủ ACME thử của chính Let's
#     Encrypt, gói `pebble` 2.4.0 của Ubuntu 24.04) ngay trong máy ảo, nghe
#     127.0.0.1:14000, và cho tpan dùng nó bằng cài đặt ẩn
#     `tpan ssl settings --server https://localhost:14000/dir`. certbot thật,
#     nginx/apache thật, thử thách HTTP-01 thật đi vào cổng 80 của máy ảo
#     (httpPort 80 trong cấu hình pebble là cổng pebble GỌI TỚI, không phải cổng nó nghe).
#   - Gói pebble của Ubuntu không kèm chứng chỉ cho API của nó (không có
#     test/certs/ như repo gốc), nên script tự tạo một CA thử và chứng chỉ cho
#     localhost bằng openssl, rồi thêm CA đó vào kho tin cậy hệ thống
#     (update-ca-certificates) để certbot tin API của pebble. Cách này cần vì
#     tpan chạy certbot qua system.Cmd, không truyền được REQUESTS_CA_BUNDLE.
#   - Tên miền thử trỏ về 127.0.0.1 bằng /etc/hosts của máy ảo (getent, curl,
#     pebble đều đọc /etc/hosts; script kiểm lại điều đó).
#   Trên VPS thật không có gì trong số này: tpan dùng Let's Encrypt (staging
#   mặc định bật), xem docs/thay-doi/M5.md, mục "Việc chủ dự án tự làm trên VPS".
#
# Đặt E2E_LOG=<file log> để cuối lần chạy script tự tìm trong chính file log
# đó: mọi mật khẩu đã gặp, IP máy ảo (IPv4 và IPv6), IP của Mac, thư mục nhà và
# tên user Mac, giá trị cookie (phải là 0 lần).
#
# Output ra stdout như e2e-m2.sh và e2e-m3.sh: mỗi lệnh có dấu thời gian,
# output nguyên văn (đã che) và mã thoát; mỗi khẳng định in "=> ĐẠT" hoặc
# "=> KHÔNG ĐẠT". Che: mật khẩu (panel, quản trị WordPress, DB của site), giá
# trị cookie, IP máy ảo (<vm>, <vm-v6>), IP của Mac (<mac>), mạng của Multipass,
# thư mục nhà và tên user Mac.
# Không dùng set -e: nhiều lệnh ở đây được chờ là phải lỗi.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2

VM=scripts/vm.sh
FAILS=0
OUT=""
RC=0
EMAIL=admin@example.com
# S1 xin chứng chỉ; /etc/hosts của máy ảo trỏ nó về 127.0.0.1.
S1=s1.example.com
# S2 giữ http. Đuôi .test là đuôi dành riêng cho thử nghiệm (RFC 6761), không
# bao giờ có bản ghi DNS thật: đúng thứ cần cho "domain không có bản ghi nào".
S2=s2.example.test
ACME=https://localhost:14000/dir
PEBBLE_UNIT=tpan-e2e-pebble
PEBBLE_DIR=/etc/tpan-e2e-pebble
ACME_ROOT=/var/lib/tpan/acme
TMPD="$(mktemp -d)"
chmod 700 "$TMPD"
SECRETS="$TMPD/secrets"
: >"$SECRETS"
chmod 600 "$SECRETS"
trap 'rm -rf "$TMPD"' EXIT

ts() { date '+%Y-%m-%d %H:%M:%S'; }
section() { printf '\n\n######## [%s] %s\n' "$(ts)" "$*"; }

VM_IP=""
HOST_IP=""
NET4=""   # ba octet đầu của mạng Multipass, kèm dấu chấm cuối
VM_V6=""  # các địa chỉ IPv6 global của máy ảo, mỗi dòng một
V6PFX=""  # tiền tố /64 của IPv6 đó (4 nhóm đầu)
BASE=""

# mask_str che một chuỗi (stdin) theo mọi thứ đã biết, in ra stdout.
mask_str() {
  local s a p
  s="$(cat)"
  [ -n "$VM_IP" ] && s="${s//"$VM_IP"/<vm>}"
  [ -n "$HOST_IP" ] && s="${s//"$HOST_IP"/<mac>}"
  [ -n "$NET4" ] && s="${s//"$NET4"/<mạng máy ảo>.}"
  while IFS= read -r a; do
    [ -n "$a" ] && s="${s//"$a"/<vm-v6>}"
  done <<<"$VM_V6"
  [ -n "$V6PFX" ] && s="${s//"$V6PFX"/<tiền tố v6>}"
  s="${s//"$TMPD"/<tmp>}"
  s="${s//"$HOME"/~}"
  s="${s//"$USER"/<user>}"
  while IFS= read -r p; do
    [ -n "$p" ] && s="${s//"$p"/<mật khẩu, đã che>}"
  done <"$SECRETS"
  # Lưới an toàn: mọi địa chỉ IPv6 mà `ip addr` in ra (bằng chứng của phép DNS).
  s="$(sed -E -e 's/^([Ss]et-[Cc]ookie: [^=]+=)[^;]*/\1<đã che>/' -e 's/inet6 [0-9a-f:]{3,}/inet6 <vm-v6>/g' <<<"$s")"
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
out_is() { [ "$(tr -d '\r' <<<"$OUT")" = "$1" ]; }

vexec() { run "$VM" exec "$@"; }
vroot() { run "$VM" root "$@"; }

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

# ---------------------------------------------------------------- máy ảo

reset_vm() {
  printf 'Máy ảo: %s (file .tpan-vm)\n' "$(tr -d '[:space:]' <.tpan-vm)"
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

# addrs đọc mọi địa chỉ cần che (IPv4, IPv6 của máy ảo; IP của Mac như máy ảo thấy).
addrs() {
  VM_IP="$("$VM" ip)"
  NET4="${VM_IP%.*}."
  HOST_IP="$("$VM" exec bash -c 'echo "${SSH_CLIENT%% *}"' | tr -d '\r')"
  # Ngay sau khi máy ảo khởi động, IPv6 (SLAAC) có thể chưa có: chờ tới 30 giây.
  local _
  for _ in $(seq 1 30); do
    VM_V6="$("$VM" exec bash -c 'ip -o -6 addr show scope global | awk "{print \$4}" | cut -d/ -f1' | tr -d '\r')"
    [ -n "$VM_V6" ] && break
    sleep 1
  done
  V6PFX="$(head -1 <<<"$VM_V6" | cut -d: -f1-4)"
  BASE="https://$VM_IP:8443"
  printf 'IP máy ảo: <vm> (IPv6: <vm-v6>); IP của Mac trong mạng Multipass: <mac> (đều được che trong log)\n'
}

BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

deploy() {
  addrs
  run make vm-deploy DATE="$BUILD_DATE"
  ok "build và chép tpan vào máy ảo" rc_is 0
  run shasum -a 256 bin/tpan-linux-arm64 bin/tpan-linux-amd64
  run git log --oneline -1
  run git status --short
}

# ---------------------------------------------------------------- đoạn chạy trong máy ảo

# Dựng pebble (đồ nghề test, xem đầu file). Chạy bằng root trong máy ảo.
PEBBLE_SCRIPT='
set -e
export DEBIAN_FRONTEND=noninteractive
apt-get -o DPkg::Lock::Timeout=120 install -y -q pebble >/dev/null
dpkg-query -W -f "gói: \${Package} \${Version}\n" pebble
d=$1
mkdir -p "$d"
chmod 700 "$d"
cd "$d"
# CA thử chỉ dùng cho API của pebble; hạn 7 ngày, nằm trong máy ảo test.
openssl req -x509 -newkey rsa:2048 -nodes -days 7 -keyout ca.key -out ca.pem \
  -subj "/CN=tpan e2e pebble API CA (chi may ao test)" -addext "keyUsage=critical,keyCertSign,cRLSign" 2>/dev/null
openssl req -newkey rsa:2048 -nodes -keyout key.pem -out api.csr -subj "/CN=localhost" 2>/dev/null
printf "subjectAltName=DNS:localhost,IP:127.0.0.1\nbasicConstraints=CA:FALSE\nkeyUsage=critical,digitalSignature,keyEncipherment\nextendedKeyUsage=serverAuth\n" >api.ext
openssl x509 -req -in api.csr -CA ca.pem -CAkey ca.key -CAcreateserial -days 7 -out cert.pem -extfile api.ext 2>/dev/null
openssl x509 -in cert.pem -noout -subject -issuer -ext subjectAltName
cp ca.pem /usr/local/share/ca-certificates/tpan-e2e-pebble.crt
update-ca-certificates 2>&1 | tail -2
printf "{\"pebble\":{\"listenAddress\":\"127.0.0.1:14000\",\"managementListenAddress\":\"127.0.0.1:15000\",\"certificate\":\"%s/cert.pem\",\"privateKey\":\"%s/key.pem\",\"httpPort\":80,\"tlsPort\":443,\"ocspResponderURL\":\"\",\"externalAccountBindingRequired\":false}}\n" "$d" "$d" >config.json
cat config.json
# NOSLEEP: không ngủ ngẫu nhiên trước khi kiểm; NONCEREJECT=0: không cố ý từ
# chối nonce; AUTHZREUSE=0: lần nào cũng kiểm HTTP-01 thật, không dùng lại.
systemd-run --unit='"$PEBBLE_UNIT"' --setenv=PEBBLE_VA_NOSLEEP=1 --setenv=PEBBLE_WFE_NONCEREJECT=0 --setenv=PEBBLE_AUTHZREUSE=0 \
  /usr/bin/pebble -config "$d/config.json"
for i in $(seq 1 30); do
  curl -s -o /dev/null https://localhost:14000/dir && break
  sleep 1
done
'

# Job mới nhất của một loại (kind) và các sự kiện chính của nó.
JOB_PY='
import sqlite3, sys
c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True)
n = c.execute("SELECT COUNT(*) FROM jobs").fetchone()[0]
print("số job trong tpan.db:", n)
if len(sys.argv) > 1:
    j = c.execute("SELECT id, status FROM jobs WHERE kind = ? ORDER BY started_at DESC, rowid DESC LIMIT 1", (sys.argv[1],)).fetchone()
    if j is None:
        print("không có job", sys.argv[1])
    else:
        print("job", sys.argv[1], j[0], j[1])
        for kind, step, msg in c.execute("SELECT kind, step_id, message FROM job_events WHERE job_id = ? AND kind IN (\"command\", \"step.skip\", \"step.done\", \"step.fail\", \"undo.done\", \"undo.fail\") ORDER BY seq", (j[0],)):
            print(" ", kind, step, msg[:300].replace("\n", " "))
'

# Trạng thái sau một lần chạy lại phải giống hệt trước: vhost, site mặc định
# 443, hook, chứng chỉ (số bản trong archive, serial đang phục vụ), web server.
STATE_SCRIPT='
d=$1; avail=$2; unit=$3
for f in $avail/tpan-$d.conf $avail/*tpan-default-ssl.conf /etc/letsencrypt/renewal-hooks/deploy/tpan /etc/letsencrypt/renewal/$d.conf; do
  sha256sum "$f" 2>&1
done
ls /etc/letsencrypt/archive/$d 2>&1 | tr "\n" " "; echo
echo | openssl s_client -connect 127.0.0.1:443 -servername "$d" 2>/dev/null | openssl x509 -noout -serial 2>&1
systemctl show -p MainPID -p ExecMainStartTimestamp "$unit" | tr "\n" " "; echo
'

# ---------------------------------------------------------------- site

PW_HITS=0
site_add() {
  local d=$1
  shift
  local cmd=("$VM" root tpan site add "$d" --email "$EMAIL" --yes "$@")
  printf '\n[%s] $ %s\n' "$(ts)" "${cmd[*]}"
  OUT="$("${cmd[@]}" 2>&1)"
  RC=$?
  local pw dbpw
  pw="$(sed -nE 's/^  Mật khẩu: +([A-Za-z0-9]{32})\r?$/\1/p' <<<"$OUT" | head -1)"
  PW_HITS=0
  if [ -n "$pw" ]; then
    add_secret "$pw"
    PW_HITS="$(grep -oF -- "$pw" <<<"$OUT" | wc -l | tr -d ' ')"
  fi
  dbpw="$("$VM" root bash -c 'cd / && runuser -u "$2" -- wp config get DB_PASSWORD --path="/var/www/$1/public" 2>/dev/null' _ "$d" "$(user_of "$d")" | tr -d '\r')"
  add_secret "$dbpw"
  mask
  printf '%s\n[%s] (mã thoát %s)\n' "$OUT" "$(ts)" "$RC"
  printf 'Mật khẩu quản trị trong output: %s lần; đã đọc mật khẩu DB của site vào danh sách che (%s ký tự)\n' "$PW_HITS" "${#dbpw}"
}

# http_code <domain> <port> <url> [cờ curl]: mã HTTP gọi trong máy ảo, nối thẳng 127.0.0.1.
http_code() {
  local d=$1 port=$2 url=$3
  shift 3
  vexec curl -sS -k -m 20 --resolve "$d:$port:127.0.0.1" -o /dev/null -w '%{http_code}\n' "$@" "$url"
}

# ---------------------------------------------------------------- panel (từ Mac)

JAR="$TMPD/jar"
CODE=""
api() {
  local m=$1 p=$2
  shift 2
  : >"$TMPD/body"
  run curl -sS -m 120 --cacert "$TMPD/cert.pem" -b "$JAR" -c "$JAR" -X "$m" -D - -o "$TMPD/body" \
    -w '-> HTTP %{http_code}, %{time_total}s\n' "$@" "$BASE$p"
  CODE="$(sed -nE 's/^-> HTTP ([0-9]{3}),.*/\1/p' <<<"$OUT" | tail -1)"
}
code_is() { [ "$CODE" = "$1" ]; }
show() {
  OUT="$(jq "${1:-.}" "$TMPD/body" 2>&1)"
  mask
  printf '%s\n' "$OUT"
}

panel_setup_login() {
  section "nginx: tpan setup (panel nghe 8443) rồi đăng nhập từ Mac"
  local cmd=("$VM" root tpan setup --yes) pw
  printf '\n[%s] $ %s\n' "$(ts)" "${cmd[*]}"
  OUT="$("${cmd[@]}" 2>&1)"
  RC=$?
  pw="$(grep -A1 '^Mật khẩu quản trị (chỉ hiện MỘT lần' <<<"$OUT" | sed -nE 's/^    ([A-Z2-7]{26})\r?$/\1/p' | head -1)"
  add_secret "$pw"
  (umask 077 && printf '%s' "$pw" | jq -Rs '{password: .}' >"$TMPD/login.json")
  mask
  printf '%s\n[%s] (mã thoát %s)\n' "$OUT" "$(ts)" "$RC"
  ok "setup thoát 0" rc_is 0
  ok "đọc được mật khẩu panel" test -n "$pw"
  vroot ss -tlnp
  ok "tpan nghe 8443" has ':8443 .*"tpan"'
  "$VM" root cat /etc/tpan/tls/cert.pem | tr -d '\r' >"$TMPD/cert.pem"
  : >"$JAR"
  chmod 600 "$JAR"
  api POST /api/v1/login -H 'Content-Type: application/json' --data-binary "@$TMPD/login.json"
  ok "đăng nhập panel: 204" code_is 204
}

# ================================================================ một lượt

run_web() {
  local web=$1 unit=$1 avail
  [ "$web" = apache ] && unit=apache2
  [ "$web" = apache ] && avail=/etc/apache2/sites-available || avail=/etc/nginx/sites-available
  local u1 u2
  u1="$(user_of "$S1")"
  u2="$(user_of "$S2")"

  section "$web bước 0: máy sạch, triển khai tpan"
  reset_vm
  deploy

  section "$web bước 1: cài stack"
  vroot tpan stack install --web "$web" --yes
  ok "stack install thoát 0" rc_is 0
  ok "job thành công" has '^Job .*: thành công'
  vexec systemctl is-active "$unit" php8.3-fpm mariadb certbot.timer
  ok "$unit, php8.3-fpm, mariadb, certbot.timer active" bash -c '[ "$(grep -cx active <<<"$1")" -eq 4 ]' _ "$(tr -d '\r' <<<"$OUT")"
  vroot ss -tlnp
  ok "cổng 443 chưa mở (chưa có site SSL nào)" hasnt ':443 '

  section "$web đồ nghề: máy chủ ACME thử pebble trong máy ảo (KHÔNG phải cách triển khai)"
  vroot bash -c "$PEBBLE_SCRIPT" _ "$PEBBLE_DIR"
  ok "dựng pebble thoát 0" rc_is 0
  vexec systemctl is-active "$PEBBLE_UNIT"
  ok "pebble đang chạy" has '^active'
  vexec curl -sS https://localhost:14000/dir
  ok "curl KHÔNG có -k tin API của pebble (CA thử đã vào kho hệ thống), có newOrder" has '"newOrder"'
  vexec python3 -c 'import certifi, requests; print("certifi.where():", certifi.where()); print("requests", requests.__version__)'
  ok "python của certbot dùng kho chứng chỉ hệ thống" hasF '/etc/ssl/certs/ca-certificates.crt'
  vexec certbot --version

  section "$web bước 1: tạo 2 site"
  local d
  for d in "$S1" "$S2"; do
    site_add "$d"
    ok "site add $d thoát 0" rc_is 0
    ok "job $d thành công" has '^Job .*: thành công'
    ok "mật khẩu quản trị in đúng một lần" test "$PW_HITS" -eq 1
  done
  vroot bash -c 'echo "127.0.0.1 $1" >>/etc/hosts && grep -n "example" /etc/hosts' _ "$S1"
  ok "(đồ nghề) /etc/hosts: $S1 -> 127.0.0.1" rc_is 0
  vexec getent ahostsv4 "$S1"
  ok "getent đọc /etc/hosts: $S1 -> 127.0.0.1" has '^127\.0\.0\.1 '
  vexec getent ahostsv4 "$S2"
  ok "$S2 không có bản ghi nào (getent mã 2)" rc_is 2
  vroot tpan ssl list
  ok "ssl list: hai site, chưa SSL" bash -c 'grep -q "$2 .* không" <<<"$1" && grep -q "$3 .* không" <<<"$1"' _ "$OUT" "$S1" "$S2"

  section "$web bước 2: chưa có email thì từ chối acme_email_missing"
  vroot tpan ssl settings
  ok "chưa có email" hasF '(chưa có'
  ok "staging mặc định bật" hasF 'Staging:   bật'
  vroot tpan ssl issue "$S1" --yes --json
  ok "ssl issue thoát 1, mã acme_email_missing" bash -c '[ "$2" = 1 ] && [ "$(jq -r .error.code <<<"$1")" = acme_email_missing ]' _ "$OUT" "$RC"
  vroot tpan ssl issue "$S1" --yes
  ok "bản chữ: báo cách đặt email" hasF 'sudo tpan ssl settings --email'
  vroot python3 -c "$JOB_PY"
  local jobs0
  jobs0="$(sed -nE 's/^số job trong tpan.db: ([0-9]+).*/\1/p' <<<"$OUT")"

  section "$web: đặt email và máy chủ ACME thử (cờ --server ẩn)"
  vroot tpan ssl settings --email "$EMAIL" --server "$ACME"
  ok "settings thoát 0" rc_is 0
  ok "ACME là pebble" hasF "ACME:      $ACME"
  vroot tpan ssl settings --server 'http://localhost:14000/dir'
  ok "đối chứng: --server không phải https bị từ chối" rc_is 1
  vroot tpan ssl settings --json
  ok "cấu hình giữ nguyên sau lần từ chối" bash -c '[ "$(jq -r .server <<<"$1")" = "$2" ] && [ "$(jq -r .email <<<"$1")" = "$3" ]' _ "$OUT" "$ACME" "$EMAIL"

  section "$web bước 2a: $S2 không có bản ghi nào: từ chối trước khi gọi ACME"
  vroot tpan ssl issue "$S2" --yes --json
  ok "thoát 1, precondition_failed, phép dns.a lỗi" bash -c '[ "$2" = 1 ] && jq -e ".error.code == \"precondition_failed\" and (.error.checks[0].id | startswith(\"dns.a\")) and .error.checks[0].status == \"fail\"" <<<"$1" >/dev/null' _ "$OUT" "$RC"
  vroot tpan ssl issue "$S2" --yes
  ok "bản chữ: chưa có bản ghi A" hasF "$S2 chưa có bản ghi A"
  vroot python3 -c "$JOB_PY"
  ok "không tạo job nào (vẫn $jobs0 job)" hasF "số job trong tpan.db: $jobs0"
  vroot bash -c 'ls -la /etc/letsencrypt/renewal /etc/letsencrypt/live 2>&1; grep -c "$1" /var/log/letsencrypt/letsencrypt.log 2>&1; journalctl -u "$2" --no-pager | grep -c "$1"' _ "$S2" "$PEBBLE_UNIT"
  ok "không có renewal/live của $S2, certbot và pebble chưa từng thấy $S2" bash -c '! grep -qF "$2" <<<"$1" && [ "$(tr -d "\r" <<<"$1" | grep -cx 0)" -ge 1 ]' _ "$OUT" "$S2"

  section "$web bước 2b: $S2 trỏ về 192.0.2.1 (máy khác): DNS chỉ cảnh báo, bước thử HTTP lỗi, hoàn tác"
  vroot bash -c 'echo "192.0.2.1 $1" >>/etc/hosts && getent ahostsv4 "$1"' _ "$S2"
  ok "(đồ nghề) $S2 -> 192.0.2.1" has '^192\.0\.2\.1 '
  vroot sha256sum "$avail/tpan-$S2.conf"
  local vh2="$OUT"
  vroot tpan ssl issue "$S2" --yes --verbose
  ok "ssl issue thoát 1" rc_is 1
  ok "DNS chỉ cảnh báo (192.0.2.1 không nằm trên máy này)" has '^\[cảnh báo\] .*192\.0\.2\.1'
  ok "job hỏng, đã hoàn tác sạch" has '^Job .*: hỏng, đã hoàn tác sạch'
  ok "lỗi ở bước thử HTTP, có nói domain trỏ về đâu" hasF "$S2 đang trỏ về 192.0.2.1"
  vroot python3 -c "$JOB_PY" site.ssl
  ok "job có lệnh curl tới acme-challenge, không có lệnh certbot nào" bash -c 'grep -q "command .*curl .*acme-challenge" <<<"$1" && ! grep -q "command .*certbot" <<<"$1"' _ "$OUT"
  vroot sha256sum "$avail/tpan-$S2.conf"
  ok "vhost của $S2 không đổi" test "$OUT" = "$vh2"
  vroot bash -c 'grep -cE "^[^#]*(listen|VirtualHost).*443" "$1"; ls "${1%/sites-available/*}/sites-enabled"; ls -la /etc/letsencrypt/live /etc/letsencrypt/renewal 2>&1; find "$2" 2>&1; ss -tln | grep -c ":443 "' _ "$avail/tpan-$S2.conf" "$ACME_ROOT"
  ok "không còn gì: vhost không có 443, không site mặc định 443, không chứng chỉ, không file thử, không ai nghe 443" bash -c '! grep -q "default-ssl" <<<"$1" && ! grep -q " $2" <<<"$1" && ! grep -q "acme-challenge/" <<<"$1" && [ "$(head -1 <<<"$1" | tr -d "\r")" = 0 ] && [ "$(tail -1 <<<"$1" | tr -d "\r")" = 0 ]' _ "$OUT" "$S2"
  http_code "$S2" 80 "http://$S2/"
  ok "$S2 vẫn http 200" out_is 200
  vroot bash -c 'sed -i "/ $1\$/d" /etc/hosts; getent ahostsv4 "$1"; echo "getent mã $?"' _ "$S2"

  section "$web bước 3: ssl issue $S1 với pebble"
  vroot tpan ssl issue "$S1" --yes --verbose
  ok "ssl issue thoát 0" rc_is 0
  ok "job thành công" has '^Job .*: thành công'
  ok "mọi phép kiểm chứng đạt" bash -c 'grep -q "^Kiểm chứng:" <<<"$1" && ! grep -qE "^  \[(lỗi|chưa rõ|cảnh báo)\]" <<<"$1"' _ "$OUT"
  ok "certbot renew --dry-run (với --server của pebble) đạt" has '^  \[đạt\] .*certbot gia hạn thử'
  ok "https 200 đúng WordPress, 301 ở cổng 80, chứng chỉ khớp tên" bash -c 'grep -qE "^  \[đạt\] .*https, đúng là WordPress" <<<"$1" && grep -qE "^  \[đạt\] .*chuyển hướng 301" <<<"$1" && grep -qE "^  \[đạt\] .*Chứng chỉ khớp" <<<"$1"' _ "$OUT"
  vroot python3 -c "$JOB_PY" site.ssl
  ok "job có đúng một lệnh certbot certonly với --server của pebble" bash -c '[ "$(grep -c "command .*certbot certonly .*--server https://localhost:14000/dir" <<<"$1")" = 1 ]' _ "$OUT"
  vroot bash -c 'journalctl -u "$1" --no-pager | grep -E "$2" | tail -8' _ "$PEBBLE_UNIT" "$S1"
  ok "pebble đã kiểm HTTP-01 của $S1" hasi 'http'
  vroot bash -c 'grep "acme-challenge" "/var/log/tpan/sites/$1/access.log" | tail -4' _ "$S1"
  ok "access log của $S1 có request thử thách (đi vào đúng $web)" hasF 'acme-challenge'
  vroot tpan ssl list
  ok "ssl list: $S1 có chứng chỉ đạt" has "$S1 .* có .*\[đạt\]"

  section "$web bước 3: kiểm độc lập"
  vroot openssl x509 -in "/etc/letsencrypt/live/$S1/cert.pem" -noout -subject -issuer -dates -ext subjectAltName
  ok "SAN là $S1, do pebble cấp" bash -c 'grep -q "DNS:$2" <<<"$1" && grep -qi "pebble" <<<"$1"' _ "$OUT" "$S1"
  vexec bash -c 'curl -sS -k -m 20 --resolve "$1:443:127.0.0.1" -w "\nHTTP %{http_code}\n" "https://$1/" | { body=$(cat); printf "%s\n" "$body" | tail -1; printf "%s" "$body" | grep -o "<title>[^<]*</title>"; echo "wp-content: $(printf "%s" "$body" | grep -c wp-content) dòng; nhắc https://$1: $(printf "%s" "$body" | grep -cF "https://$1") dòng; nhắc http://$1: $(printf "%s" "$body" | grep -cF "http://$1") dòng"; }' _ "$S1"
  ok "https://$S1/ 200, là WordPress, link trong trang là https" bash -c 'grep -q "^HTTP 200" <<<"$1" && ! grep -q "wp-content: 0 " <<<"$1" && ! grep -q "nhắc https://[^:]*: 0 " <<<"$1" && grep -q "nhắc http://[^:]*: 0 " <<<"$1"' _ "$OUT"
  vexec curl -sS -m 20 --resolve "$S1:80:127.0.0.1" -o /dev/null -D - "http://$S1/duong/dan?a=1&b=2"
  ok "http 301 sang https giữ đường dẫn và query" bash -c 'grep -q "^HTTP/1.1 301" <<<"$1" && grep -qi "^location: https://$2/duong/dan?a=1&b=2" <<<"$1"' _ "$(tr -d '\r' <<<"$OUT")" "$S1"
  # Thư mục acme-challenge là của Plan SSL (bước Dir); chỉ thêm rồi xoá một file thử trong đó.
  vroot bash -c 'f="$2/.well-known/acme-challenge/e2e-thu"; ls -ld "${f%/*}"; echo noi-dung-thu-e2e >"$f"; curl -sS -m 20 --resolve "$1:80:127.0.0.1" -D - "http://$1/.well-known/acme-challenge/e2e-thu"; rm -f "$f"; curl -sS -m 20 --resolve "$1:80:127.0.0.1" -o /dev/null -w "file không có: %{http_code}\n" "http://$1/.well-known/acme-challenge/x"' _ "$S1" "$ACME_ROOT"
  ok "thử thách ở cổng 80 không bị chuyển hướng: file có thì 200 đúng nội dung, không có thì 404" bash -c 'grep -q "^HTTP/1.1 200" <<<"$1" && grep -q "^noi-dung-thu-e2e" <<<"$1" && grep -q "file không có: 404" <<<"$1" && ! grep -qi "^location:" <<<"$1"' _ "$(tr -d '\r' <<<"$OUT")"
  vexec bash -c 'for u in https://la.example.net/ https://127.0.0.1/ "https://$1/"; do h=${u#https://}; h=${h%%/*}; echo "== $u"; curl -sS -k -m 10 --resolve "la.example.net:443:127.0.0.1" --resolve "$1:443:127.0.0.1" -o /tmp/e2e-body -w "HTTP %{http_code}\n" "$u"; echo "curl mã $?"; grep -ciE "Ubuntu Default Page|Welcome to nginx|wp-content" /tmp/e2e-body 2>/dev/null; rm -f /tmp/e2e-body; done' _ "$S2"
  if [ "$web" = nginx ]; then
    ok "443 với SNI lạ, với IP, với $S2 (chưa SSL): Nginx từ chối bắt tay (curl mã 35), không trang nào lộ" bash -c '[ "$(grep -c "^curl mã 35" <<<"$1")" = 3 ] && ! grep -q "^HTTP [1-9]" <<<"$1"' _ "$(tr -d '\r' <<<"$OUT")"
  else
    ok "443 với SNI lạ, với IP, với $S2 (chưa SSL): Apache 403, không trang mặc định hay WordPress" bash -c '[ "$(grep -c "^HTTP 403" <<<"$1")" = 3 ] && [ "$(grep -cx 0 <<<"$1")" = 3 ]' _ "$(tr -d '\r' <<<"$OUT")"
    vroot bash -c 'echo | openssl s_client -connect 127.0.0.1:443 -servername la.example.net 2>/dev/null | openssl x509 -noout -fingerprint -sha256 -subject; openssl x509 -in /etc/ssl/certs/ssl-cert-snakeoil.pem -noout -fingerprint -sha256 -subject'
    ok "Apache đưa chứng chỉ snakeoil cho SNI lạ (không lộ chứng chỉ của $S1)" bash -c '[ "$(grep -c "^sha256 Fingerprint" <<<"$1")" = 2 ] && [ "$(grep "^sha256 Fingerprint" <<<"$1" | sort -u | wc -l | tr -d " ")" = 1 ]' _ "$(tr -d '\r' <<<"$OUT")"
    vroot bash -c 'apachectl -M 2>/dev/null | grep -E "ssl_module|rewrite_module"; dpkg-query -W -f "\${Package} \${Status}\n" ssl-cert; ls -l /etc/ssl/certs/ssl-cert-snakeoil.pem /etc/ssl/private/ssl-cert-snakeoil.key; apachectl configtest'
    ok "mod ssl bật, gói ssl-cert có, file snakeoil có, configtest đạt" bash -c 'grep -q ssl_module <<<"$1" && grep -q "ssl-cert install ok installed" <<<"$1" && grep -q "ssl-cert-snakeoil.key" <<<"$1" && grep -q "Syntax OK" <<<"$1"' _ "$OUT"
  fi
  vroot ls -l /etc/nginx/sites-enabled /etc/apache2/sites-enabled
  ok "không còn site mặc định của Ubuntu (default, 000-default, default-ssl)" bash -c '! grep -qE " (default|000-default\.conf|default-ssl\.conf)( |$)" <<<"$1"' _ "$OUT"
  vroot bash -c 'cd / && for o in home siteurl; do runuser -u "$2" -- wp option get "$o" --path="/var/www/$1/public" 2>/dev/null; done' _ "$S1" "$u1"
  ok "WordPress home và siteurl là https://$S1" bash -c '[ "$(tr -d "\r" <<<"$1")" = "$(printf "https://%s\nhttps://%s" "$2" "$2")" ]' _ "$OUT" "$S1"
  http_code "$S2" 80 "http://$S2/"
  ok "$S2 vẫn http 200" out_is 200
  vroot bash -c 'cd / && runuser -u "$2" -- wp option get home --path="/var/www/$1/public" 2>/dev/null' _ "$S2" "$u2"
  ok "$S2 vẫn là http://$S2" out_is "http://$S2"
  vroot bash -c 'stat -c "%a %U:%G %n" "$1"; cat "$1"' _ /etc/letsencrypt/renewal-hooks/deploy/tpan
  ok "hook gia hạn 755 root, gọi tpan internal reload-web" bash -c 'grep -q "^755 root:root " <<<"$1" && grep -q "^#!/bin/sh" <<<"$1" && grep -q "exec /usr/local/bin/tpan internal reload-web" <<<"$1"' _ "$OUT"
  vexec systemctl is-active certbot.timer
  ok "certbot.timer active" has '^active'

  section "$web bước 3: hook gia hạn chạy thật (gia hạn ép với pebble)"
  vroot bash -c 'echo | openssl s_client -connect 127.0.0.1:443 -servername "$1" 2>/dev/null | openssl x509 -noout -serial' _ "$S1"
  local serial0="$OUT"
  local since
  since="$("$VM" exec date '+%Y-%m-%d %H:%M:%S' | tr -d '\r')"
  sleep 1
  # --no-random-sleep-on-renew: không có terminal thì certbot renew tự chờ
  # ngẫu nhiên tới 8 phút trước khi gia hạn (để máy chủ của Let's Encrypt
  # không nhận cùng lúc mọi bộ hẹn giờ); lần chạy tay này không cần chờ.
  vroot certbot renew --force-renewal --no-random-sleep-on-renew --cert-name "$S1" --server "$ACME" --non-interactive
  ok "certbot renew --force-renewal thoát 0" rc_is 0
  vroot bash -c 'grep -E "Running deploy-hook command|Dry run: skipping deploy hook" /var/log/letsencrypt/letsencrypt.log | tail -3'
  ok "certbot đã chạy hook deploy của tpan (letsencrypt.log)" has 'INFO:certbot.compat.misc:Running deploy-hook command: /etc/letsencrypt/renewal-hooks/deploy/tpan'
  vroot bash -c 'journalctl -u "$1" --since "$2" --no-pager | grep -iE "reload"' _ "$unit" "$since"
  ok "$unit được reload sau khi gia hạn" hasi 'Reloaded'
  vroot bash -c 'echo | openssl s_client -connect 127.0.0.1:443 -servername "$1" 2>/dev/null | openssl x509 -noout -serial; openssl x509 -in "/etc/letsencrypt/live/$1/cert.pem" -noout -serial' _ "$S1"
  ok "chứng chỉ đang phục vụ đổi sang bản vừa gia hạn (serial mới, khớp live/)" bash -c '[ "$(sed -n 1p <<<"$1")" = "$(sed -n 2p <<<"$1")" ] && [ "$(sed -n 1p <<<"$1")" != "$2" ]' _ "$(tr -d '\r' <<<"$OUT")" "$(tr -d '\r' <<<"$serial0")"
  since="$("$VM" exec date '+%Y-%m-%d %H:%M:%S' | tr -d '\r')"
  sleep 1
  vroot tpan internal reload-web
  ok "tpan internal reload-web thoát 0" rc_is 0
  vroot bash -c 'journalctl -u "$1" --since "$2" --no-pager | grep -iE "reload"' _ "$unit" "$since"
  ok "$unit được reload bởi tpan internal reload-web" hasi 'Reloaded'

  section "$web bước 4: chạy lại ssl issue $S1: mọi bước bỏ qua"
  vroot bash -c "$STATE_SCRIPT" _ "$S1" "$avail" "$unit"
  local st0="$OUT"
  vroot python3 -c "$JOB_PY"
  vroot tpan ssl issue "$S1" --yes
  ok "thoát 0, job thành công" bash -c '[ "$2" = 0 ] && grep -q "^Job .*: thành công" <<<"$1"' _ "$OUT" "$RC"
  ok "không bước nào chạy (không có dòng \"xong\")" hasnt '^  xong$'
  ok "có bước \"đã đúng, bỏ qua\"" hasF 'đã đúng, bỏ qua'
  vroot python3 -c "$JOB_PY" site.ssl
  ok "job chạy lại không có lệnh certbot certonly, không step.done" bash -c '! grep -q "command .*certbot certonly" <<<"$1" && ! grep -q "^  step.done" <<<"$1"' _ "$OUT"
  vroot bash -c 'grep -hE "Arguments: \[.--dry-run|random delay" /var/log/letsencrypt/letsencrypt.log* | cut -c1-200'
  ok "mọi lần gia hạn thử của tpan có --no-random-sleep-on-renew, certbot không chờ ngẫu nhiên" bash -c 'n=$(grep -c "Arguments: \[.--dry-run" <<<"$1"); [ "$n" -ge 2 ] && [ "$(grep -c "no-random-sleep-on-renew" <<<"$1")" = "$n" ] && ! grep -q "random delay" <<<"$1"' _ "$OUT"
  vroot bash -c "$STATE_SCRIPT" _ "$S1" "$avail" "$unit"
  ok "vhost, mặc định 443, hook, chứng chỉ, serial, $unit không đổi" test "$OUT" = "$st0"

  section "$web bước 4: chạy lại site add $S1: vẫn vhost 443"
  site_add "$S1"
  ok "site add chạy lại thoát 0, thành công" bash -c '[ "$2" = 0 ] && grep -q "^Job .*: thành công" <<<"$1"' _ "$OUT" "$RC"
  ok "không bước nào chạy" hasnt '^  xong$'
  ok "kiểm chứng của site có SSL (có phép chuyển hướng 301 và https)" bash -c 'grep -qE "^  \[đạt\] .*chuyển hướng 301" <<<"$1" && grep -qE "^  \[đạt\] .*https, đúng là WordPress" <<<"$1"' _ "$OUT"
  vroot bash -c "$STATE_SCRIPT" _ "$S1" "$avail" "$unit"
  ok "vhost vẫn bản 443, mọi thứ không đổi" test "$OUT" = "$st0"
  vroot grep -cE '^[^#]*(listen|VirtualHost).*443' "$avail/tpan-$S1.conf"
  ok "vhost $S1 có 443" bash -c '[ "$(tr -d "\r" <<<"$1")" -ge 1 ]' _ "$OUT"
  vexec curl -sS -m 20 --resolve "$S1:80:127.0.0.1" -o /dev/null -w '%{http_code} %{redirect_url}\n' "http://$S1/"
  ok "http 301 sang https://$S1/" out_is "301 https://$S1/"
  http_code "$S1" 443 "https://$S1/"
  ok "https 200" out_is 200

  if [ "$web" = nginx ]; then
    panel_setup_login
    section "nginx: GET /api/v1/ssl"
    api GET /api/v1/ssl
    ok "GET /api/v1/ssl 200" code_is 200
    show '{config, sites: [.sites[] | {domain, ssl, cert: (.cert | if . then {id, status, summary: .summary.vi} else null end)}]}'
    ok "đúng dạng: config.email, config.server, $S1 ssl=true cert pass, $S2 ssl=false không cert" bash -c 'jq -e --arg s1 "$2" --arg s2 "$3" --arg e "$4" --arg a "$5" ".config.email == \$e and .config.server == \$a and .config.staging == true and ([.sites[] | select(.domain == \$s1 and .ssl and .cert.status == \"pass\")] | length) == 1 and ([.sites[] | select(.domain == \$s2 and (.ssl | not) and .cert == null)] | length) == 1" "$1" >/dev/null' _ "$TMPD/body" "$S1" "$S2" "$EMAIL" "$ACME"
  fi

  section "$web bước 5: site rm $S1: chứng chỉ bị certbot delete"
  vroot tpan site rm "$S1" --yes
  ok "site rm thoát 0, thành công" bash -c '[ "$2" = 0 ] && grep -q "^Job .*: thành công" <<<"$1"' _ "$OUT" "$RC"
  vroot python3 -c "$JOB_PY" site.rm
  ok "job có lệnh certbot delete --cert-name $S1" hasF "certbot delete --cert-name $S1"
  vroot bash -c 'ls /etc/letsencrypt/renewal /etc/letsencrypt/live /etc/letsencrypt/archive 2>&1; certbot certificates 2>&1 | tail -4'
  ok "không còn renewal, live, archive của $S1; certbot không còn chứng chỉ nào" bash -c '! grep -q "$2" <<<"$1" && grep -q "No certificates found" <<<"$1"' _ "$OUT" "$S1"
  http_code "$S2" 80 "http://$S2/"
  ok "$S2 vẫn http 200" out_is 200
}

# ================================================================ bảo mật (lượt nginx, chạy cuối)

run_security() {
  section "nginx bước 6: trang bảo mật"
  vroot tpan security status --json
  ok "security status thoát 0" rc_is 0
  OUT="$(jq -c '.results[] | {id, status, summary: .summary.vi}' <<<"$OUT" 2>&1)"
  printf '%s\n' "$OUT"
  ok "có đủ các dòng ufw, fail2ban, ssh root, ssh mật khẩu, swap, cập nhật, cổng panel" bash -c 'for k in sec.ufw sec.fail2ban sec.ssh.root sec.ssh.password sec.swap sec.updates; do grep -q "\"id\":\"$k" <<<"$1" || exit 1; done; grep -q "\"id\":\"sec.panel" <<<"$1"' _ "$OUT"

  section "nginx bước 6: fail2ban"
  vroot tpan security fail2ban --yes
  ok "fail2ban thoát 0, thành công" bash -c '[ "$2" = 0 ] && grep -q "^Job .*: thành công" <<<"$1"' _ "$OUT" "$RC"
  vroot bash -c 'systemctl is-active fail2ban; fail2ban-client status sshd; cat /etc/fail2ban/jail.d/tpan-sshd.conf'
  ok "fail2ban active, jail sshd chạy" bash -c 'grep -q "^active" <<<"$1" && grep -q "Status for the jail: sshd" <<<"$1"' _ "$OUT"
  vroot tpan security fail2ban --yes
  ok "chạy lại: không bước nào chạy" bash -c '[ "$2" = 0 ] && ! grep -q "^  xong$" <<<"$1"' _ "$OUT" "$RC"

  section "nginx bước 6: ufw (xem trước, đối chiếu cổng SSH thật)"
  vroot bash -c 'sshd -T 2>/dev/null | grep -i "^port "; ss -tlnp | grep -E ":(22|80|443|8443) "'
  local sshp
  sshp="$(sed -nE 's/^port ([0-9]+).*/\1/p' <<<"$OUT" | head -1)"
  ok "đọc được cổng SSH thật ($sshp)" test -n "$sshp"
  vroot tpan security ufw --dry-run
  ok "xem trước thoát 0" rc_is 0
  ok "xem trước mở đúng $sshp/tcp, 80/tcp, 443/tcp, 8443/tcp" bash -c 'grep -qF "$2/tcp, 80/tcp, 443/tcp, 8443/tcp" <<<"$1"' _ "$OUT" "$sshp"
  vexec ufw version
  vroot ufw status
  ok "ufw chưa bật" hasF 'Status: inactive'
  # Đồ nghề: một cổng không nằm trong danh sách mở, để thấy ufw chặn thật.
  vroot systemd-run --unit=tpan-e2e-port9090 python3 -m http.server 9090 --directory /srv
  sleep 2
  run curl -sS -m 5 -o /dev/null -w '%{http_code}\n' "http://$VM_IP:9090/"
  ok "trước ufw: từ Mac vào được cổng 9090 (đối chứng)" has '^200$'

  section "nginx bước 6: ufw --yes"
  vroot tpan security ufw --yes
  ok "ufw thoát 0, thành công" bash -c '[ "$2" = 0 ] && grep -q "^Job .*: thành công" <<<"$1"' _ "$OUT" "$RC"
  vroot ufw status verbose
  ok "ufw active, mặc định chặn vào, mở $sshp, 80, 443, 8443" bash -c 'grep -q "^Status: active" <<<"$1" && grep -q "deny (incoming)" <<<"$1" && for p in "$2" 80 443 8443; do grep -qE "^$p/tcp +ALLOW IN" <<<"$1" || exit 1; done' _ "$OUT" "$sshp"
  local ufw0="$OUT"
  vexec echo "multipass exec vẫn vào được máy ảo"
  ok "multipass exec (SSH) vẫn vào được sau khi bật ufw" rc_is 0
  run curl -sS -m 5 -o /dev/null -w '%{http_code}\n' "http://$VM_IP:9090/"
  ok "sau ufw: cổng 9090 bị chặn từ Mac (curl hết giờ, mã 28)" rc_is 28
  run curl -sS -m 10 --cacert "$TMPD/cert.pem" -o /dev/null -w '%{http_code}\n' "$BASE/api/v1/health"
  ok "sau ufw: panel 8443 vẫn vào được từ Mac" has '^200$'
  run curl -sS -m 10 --resolve "$S2:80:$VM_IP" -o /dev/null -w '%{http_code}\n' "http://$S2/"
  ok "sau ufw: $S2 cổng 80 vẫn vào được từ Mac" has '^200$'
  vroot systemctl stop tpan-e2e-port9090
  vroot tpan security ufw --yes
  ok "chạy lại: thoát 0, không bước nào chạy" bash -c '[ "$2" = 0 ] && ! grep -q "^  xong$" <<<"$1"' _ "$OUT" "$RC"
  vroot ufw status verbose
  ok "ufw status không đổi" test "$OUT" = "$ufw0"

  section "nginx bước 6: GET /api/v1/security (từ Mac, qua ufw)"
  api GET /api/v1/security
  ok "GET /api/v1/security 200" code_is 200
  show '[.results[] | {id, status, summary: .summary.vi}]'
  ok "đúng dạng: results[] có id, status; ufw và fail2ban đạt" bash -c 'jq -e "(.results | length) >= 7 and all(.results[]; has(\"id\") and has(\"status\") and has(\"summary\")) and ([.results[] | select(.id == \"sec.ufw\" and .status == \"pass\")] | length) == 1 and ([.results[] | select(.id == \"sec.fail2ban\" and .status == \"pass\")] | length) == 1" "$1" >/dev/null' _ "$TMPD/body"
}

case "${1:-all}" in
  all)
    run_web nginx
    run_security
    run_web apache
    ;;
  nginx)
    run_web nginx
    run_security
    ;;
  apache) run_web apache ;;
  *)
    sed -n '2,8p' "$0" | sed 's/^# \{0,1\}//'
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
  ok "log không chứa IP máy ảo (IPv4)" bash -c '! grep -qF -- "$1" "$2"' _ "$VM_IP" "$E2E_LOG"
  ok "log không chứa mạng Multipass" bash -c '! grep -qF -- "$1" "$2"' _ "$NET4" "$E2E_LOG"
  ok "log không chứa IPv6 của máy ảo" bash -c '[ -z "$1" ] || ! grep -qF -- "$1" "$2"' _ "$V6PFX" "$E2E_LOG"
  ok "log không chứa IP của Mac" bash -c '! grep -qF -- "$1" "$2"' _ "$HOST_IP" "$E2E_LOG"
  ok "log không chứa thư mục nhà hay tên user Mac" bash -c '! grep -qF -- "$1" "$3" && ! grep -qF -- "$2" "$3"' _ "$HOME" "$USER" "$E2E_LOG"
  ok "log không chứa giá trị cookie nào" bash -c '! grep -iE "^set-cookie: [^=]+=" "$1" | grep -vqE "=<đã che>"' _ "$E2E_LOG"
fi

section "TỔNG KẾT: $FAILS khẳng định không đạt"
[ "$FAILS" -eq 0 ]
