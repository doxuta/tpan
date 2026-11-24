#!/usr/bin/env bash
# e2e của mốc M13 (nhiều người dùng, gói, tạm khoá user/site/DB, DB rời; mục
# 6.12 và hàng M13 ở mục 13 của docs/KE-HOACH.md) trên máy ảo (tên trong file
# .tpan-vm; không có thì tpan-test). Chạy trên Mac, trong thư mục repo:
#
#   scripts/e2e-m13.sh nang-cap   nâng từ bản phát hành v0.1.1 (máy sạch)
#   scripts/e2e-m13.sh chinh      e2e bước 16 (máy sạch lần nữa)
#   scripts/e2e-m13.sh all        nang-cap rồi chinh
#
# NÂNG CẤP (nang-cap), từ máy sạch:
#   U0 gói v0.1.1 thật (dist/release/v0.1.1, KHÔNG build lại từ tag), máy sạch
#   U1 `cd v0.1.1 && sudo ./install.sh --web nginx` của chính gói đó, tạo một
#      site, đăng nhập API (chỉ mật khẩu, như v0.1.1), giữ cookie
#   U2 nâng lên bản của cây bằng `tpan self-update --url` CỦA BẢN CŨ, gói của
#      bản mới (make release) phục vụ từ máy chủ HTTP loopback trong máy ảo
#   U3 sau khi khởi động lại: cookie cũ vẫn dùng được; login chỉ có mật khẩu
#      đạt; site cũ có chủ admin; hash mật khẩu giữ nguyên; tpan user list có
#      admin; tpan admin passwd chạy, mật khẩu mới đăng nhập được; (từ M14)
#      health chỉ còn ba loại lỗi cô lập của M14 và `tpan security harden`
#      đưa về không còn lỗi
#   U4 `tpan self-update --rollback`: ghi nguyên output; từ M15 lõi thử file
#      cũ với tpan.db trước khi đổi file và từ chối self_rollback_db (KE-HOACH
#      mục 6.14 ý 7), kèm câu của file cũ "DB ở bản <migration cao nhất>, tpan
#      này chỉ biết tới bản 900"; file chạy và dịch vụ giữ nguyên. Trước M15
#      phần này kiểm chỗ hở cũ (rollback thoát 0, dịch vụ khởi động lại liên tục).
#   U5 đưa máy về bản mới bằng install.sh của gói mới (file chạy có sẵn)
#
# CHÍNH (chinh), từ máy sạch:
#   P0 tpan, stack nginx, tpan setup, pebble (máy chủ ACME thử, như e2e-m5)
#   P1 hai gói, user A (gói nhỏ: 2 site, 1 DB rời) và B; mỗi user có site, DB
#      rời, job, backup; B có thêm site và DB rời trong thùng rác, một sự cố
#   P2 cô lập qua API: A với đồ của B ra 404 (GET, DELETE, POST, SSE, khôi
#      phục backup, xoá hẳn trong thùng rác), tạo site trùng domain của B 404,
#      owner B 403, apply và prune theo phạm vi, export, danh sách, route admin
#   P3 hạn mức: site thứ 3 và DB rời thứ 2 của A bị từ chối, máy không đổi
#   P4 tạm khoá site (cả site SSL): 403 kèm trang, pool, cron, nginx -t,
#      certbot renew --dry-run qua vhost tạm khoá; chuyển sang apache và về
#   P5 tạm khoá DB rời (KILL kết nối socket và TCP, đổi mật khẩu vẫn khoá),
#      DB của site (health xám, không mở sự cố), COLLATIONS, du -sb
#   P6 tạm khoá user A (thứ khoá riêng từ trước vẫn khoá sau khi mở), xem như
#      user (kể cả user đang khoá), không khoá được admin cuối
#   P7 xoá user B: user_not_empty, trash-all, xoá hẳn, rồi xoá được
#   P8 mật khẩu tpan sinh ra không có trong tpan.db, WAL, SHM, job_events, journal
#
# ĐỒ NGHỀ TEST, CHỈ TRONG MÁY ẢO: pebble (unit tpan-e2e-pebble) và CA thử
# trong kho tin cậy; dòng /etc/hosts cho site SSL; máy chủ HTTP loopback
# (unit tpan-e2e-rel) phục vụ gói phát hành; file /root/e2e-*.cnf (0600) chứa
# mật khẩu DB rời cho kết nối thử.
#
# Đặt E2E_LOG=<file log> để cuối lần chạy script tự tìm trong chính file log
# đó: mọi mật khẩu và cookie đã gặp, IP máy ảo (IPv4, IPv6), IP của Mac, thư
# mục nhà và tên user Mac (phải là 0 lần).
#
# Output như các e2e trước: mỗi lệnh có dấu thời gian, output nguyên văn (đã
# che) và mã thoát; mỗi khẳng định in "=> ĐẠT" hoặc "=> KHÔNG ĐẠT"; cuối mỗi
# phần và cuối lần chạy in số đạt, không đạt.
# Không dùng set -e: nhiều lệnh ở đây được chờ là phải lỗi.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2

VM=scripts/vm.sh
VMNAME="${TPAN_VM:-$(tr -d '[:space:]' 2>/dev/null <.tpan-vm)}"
VMNAME="${VMNAME:-tpan-test}"
FAILS=0
PASSES=0
OUT=""
RC=0
CODE=""
EMAIL=admin@example.com
# Từ M28 đợt 2 (P4): site mới của user thường không được chung tên miền gốc
# (eTLD+1) với site của người khác, nên mỗi chủ một tên miền gốc: admin
# example.com, khach_a example.net, khach_b example.org.
S1=s1.example.com # của admin, có SSL (pebble)
A1=a1.example.net # của khach_a, tạo bằng CLI --owner
A2=a2.example.net # của khach_a, tạo qua API bằng phiên của A
A3=a3.example.net # site thứ 3 của A: phải bị từ chối quota_sites
B1=b1.example.org # của khach_b, tạo bằng CLI --owner
B2=b2.example.org # của khach_b, tạo qua API rồi đưa vào thùng rác
N1=n1.example.org # A thử tạo cho B: 403
UPD=up.example.com # site của phần nâng cấp
UA=khach_a
UB=khach_b
PKN=goi_nho
PKL=goi_lon
DA1="${UA}_d1"
DA2="${UA}_d2"
DB1="${UB}_d1"
DB2="${UB}_d2"
ACME=https://localhost:14000/dir
PEBBLE_UNIT=tpan-e2e-pebble
PEBBLE_DIR=/etc/tpan-e2e-pebble
ACME_ROOT=/var/lib/tpan/acme
PORT=18713
OLD_VER=v0.1.1
# Bản migration cao nhất của cây (tên file lớn nhất trong internal/store/migrations).
NEW_MIG="$(find internal/store/migrations -name '[0-9]*_*.sql' -exec basename {} \; | sed 's/_.*//' | sort -n | tail -1)"
# Gói v0.1.1 đã phát hành nằm ở dist/release/v0.1.1 của repo chính (dist/ không
# vào git); worktree chưa có thì chép sang. Đặt OLD_PKG để chỉ chỗ khác.
OLD_PKG="${OLD_PKG:-$(git rev-parse --path-format=absolute --git-common-dir 2>/dev/null)/../dist/release/$OLD_VER}"
VER="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# E2E_TMP=<thư mục>: giữ thư mục tạm (mật khẩu, cookie, id) sau khi chạy, để
# `tu <phần>` chạy tiếp từ một phần trên máy ảo đang dở khi sửa script. Bằng
# chứng luôn là một lần chạy trọn từ máy sạch, không dùng chế độ này.
if [ -n "${E2E_TMP:-}" ]; then
  TMPD="$E2E_TMP"
  mkdir -p "$TMPD"
else
  TMPD="$(mktemp -d)"
fi
chmod 700 "$TMPD"
SECRETS="$TMPD/secrets"
[ -f "$SECRETS" ] || : >"$SECRETS"
chmod 600 "$SECRETS"
HJ="$TMPD/health.json"
REL_UP=0
cleanup() {
  [ "$REL_UP" = 1 ] && "$VM" root systemctl stop tpan-e2e-rel </dev/null >/dev/null 2>&1
  [ -n "${E2E_TMP:-}" ] || rm -rf "$TMPD"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

ts() { date '+%Y-%m-%d %H:%M:%S'; }
section() { printf '\n\n######## [%s] %s\n' "$(ts)" "$*"; }

VM_IP=""
HOST_IP=""
NET4=""
VM_V6=""
V6PFX=""
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
  [ -n "$V6PFX" ] && s="${s//"$(tr 'a-f' 'A-F' <<<"$V6PFX")"/<tiền tố v6>}"
  s="${s//"$TMPD"/<tmp>}"
  s="${s//"$HOME"/~}"
  s="${s//"$USER"/<user>}"
  while IFS= read -r p; do
    [ -n "$p" ] && s="${s//"$p"/<bí mật, đã che>}"
  done <"$SECRETS"
  s="$(sed -E -e 's/inet6 [0-9a-f:]{3,}/inet6 <v6>/g' -e 's/^(set-cookie: *[^=]*=)[^;]*/\1<cookie, đã che>/I' <<<"$s")"
  printf '%s' "$s"
}
mask() { OUT="$(mask_str <<<"$OUT")"; }

# runq chạy lệnh, giữ output thô ở $TMPD/run.out, KHÔNG in: dùng khi output có
# thể chứa một bí mật chưa biết, để đọc nó vào danh sách che rồi mới show_last.
# Output vào file rồi mới đọc (multipass exec trên Mac có lúc treo khi stdout
# là pipe); stdin là /dev/null (multipass exec chờ stdin mãi khi không có terminal).
runq() {
  RUNQ_CMD="$(mask_str <<<"$*")"
  RUNQ_TS="$(ts)"
  "$@" </dev/null >"$TMPD/run.out" 2>&1
  RC=$?
}
show_last() {
  printf '\n[%s] $ %s\n' "$RUNQ_TS" "$RUNQ_CMD"
  OUT="$(tr -d '\r' <"$TMPD/run.out")"
  mask
  [ -n "$OUT" ] && printf '%s\n' "$OUT"
  printf '[%s] (mã thoát %s)\n' "$(ts)" "$RC"
}
run() {
  runq "$@"
  show_last
}
# run_in <file> <lệnh...>: như run, nhưng stdin là file (bí mật đi qua stdin,
# không bao giờ nằm trên dòng lệnh: sudo ghi dòng lệnh vào journal).
run_in() {
  local f=$1
  shift
  RUNQ_CMD="$(mask_str <<<"$*") < (bí mật qua stdin)"
  RUNQ_TS="$(ts)"
  "$@" <"$f" >"$TMPD/run.out" 2>&1
  RC=$?
  show_last
}

ok() {
  local desc=$1
  shift
  if "$@"; then
    printf '  => ĐẠT: %s\n' "$desc"
    PASSES=$((PASSES + 1))
  else
    printf '  => KHÔNG ĐẠT: %s\n' "$desc"
    FAILS=$((FAILS + 1))
  fi
}
P0=0
F0=0
part_begin() { P0=$PASSES F0=$FAILS; section "PHẦN $*"; }
part_end() {
  printf '\n==== Phần %s: %s đạt, %s không đạt\n' "$1" "$((PASSES - P0))" "$((FAILS - F0))"
  declare -p JB JA2ADD BKB INC_B T_A2 HFP0 JAR_ADM JAR_A JAR_B JAR_OLD 2>/dev/null >"$TMPD/state"
}

rc_is() { [ "$RC" -eq "$1" ]; }
rc_not() { [ "$RC" -ne "$1" ]; }
has() { grep -qE -- "$1" <<<"$OUT"; }
hasnt() { ! grep -qE -- "$1" <<<"$OUT"; }
hasF() { grep -qF -- "$1" <<<"$OUT"; }
hasntF() { ! grep -qF -- "$1" <<<"$OUT"; }
out_is() { [ "$OUT" = "$1" ]; }
count_is() { [ "$(grep -cE -- "$2" <<<"$OUT")" -eq "$1" ]; }
job_ok() { [ "$RC" = 0 ] && grep -q '^Job .*: thành công' <<<"$OUT"; }
# jout <biểu thức jq>: jq -e trên OUT (output --json của tpan).
jout() { jq -e "$@" <<<"$OUT" >/dev/null 2>&1; }
# refused <mã>: lệnh --json thoát khác 0 với .error.code là mã đó.
refused() { [ "$RC" != 0 ] && [ "$(jq -r '.error.code // empty' <<<"$OUT" 2>/dev/null)" = "$1" ]; }

vexec() { run "$VM" exec "$@"; }
vroot() { run "$VM" root "$@"; }

add_secret() {
  [ -n "${1:-}" ] || return 0
  grep -qxF -- "$1" "$SECRETS" || printf '%s\n' "$1" >>"$SECRETS"
}
# save_pw <tên> <mật khẩu>: giữ một mật khẩu ở $TMPD/pw.<tên> (0600) và che nó.
save_pw() {
  add_secret "$2"
  (umask 077 && printf '%s' "$2" >"$TMPD/pw.$1")
}

slug_of() {
  local d=$1 head hash
  head="$(printf '%s' "${d:0:16}" | tr -c 'a-z0-9' '_')"
  hash="$(printf '%s' "$d" | shasum -a 256 | cut -c1-6)"
  printf '%s_%s' "$head" "$hash"
}
user_of() { printf 'tp_%s' "$(slug_of "$1")"; }
vm_now() { "$VM" exec date '+%Y-%m-%d %H:%M:%S' </dev/null | tr -d '\r'; }

# dbpw <domain>: mật khẩu DB của site trong wp-config.php (không in).
dbpw() {
  "$VM" root bash -c 'cd / && runuser -u "$2" -- wp config get DB_PASSWORD --path="/var/www/$1/public" 2>/dev/null' _ "$1" "$(user_of "$1")" </dev/null | tr -d '\r'
}

# ---------------------------------------------------------------- máy ảo

addrs() {
  VM_IP="$("$VM" ip </dev/null)"
  NET4="${VM_IP%.*}."
  HOST_IP="$("$VM" exec bash -c 'echo "${SSH_CLIENT%% *}"' </dev/null | tr -d '\r')"
  local _
  for _ in $(seq 1 30); do
    VM_V6="$("$VM" exec bash -c 'ip -o -6 addr show scope global | awk "{print \$4}" | cut -d/ -f1' </dev/null | tr -d '\r')"
    [ -n "$VM_V6" ] && break
    sleep 1
  done
  V6PFX="$(head -1 <<<"$VM_V6" | cut -d: -f1-4)"
  BASE="https://$VM_IP:8443"
  printf 'IP máy ảo: <vm> (IPv6: <vm-v6>); IP của Mac trong mạng Multipass: <mac> (đều được che trong log)\n'
}

reset_vm() {
  printf 'Máy ảo: %s\n' "$VMNAME"
  run make vm-reset
  ok "máy ảo về snapshot clean" rc_is 0
  local _
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    vexec cloud-init status --wait
    [ "$RC" -ne 255 ] && break
    sleep 3
  done
  ok "cloud-init xong" rc_is 0
  addrs
  vexec bash -c 'command -v tpan; ls /etc/tpan /var/lib/tpan 2>&1'
  ok "máy sạch: chưa có tpan, /etc/tpan, /var/lib/tpan" bash -c '! grep -q "/usr/local/bin/tpan" <<<"$1" && [ "$(grep -c "No such file or directory" <<<"$1")" -eq 2 ]' _ "$OUT"
}

# install_run <lệnh...>: một lần chạy install.sh; nhặt mật khẩu quản trị (che,
# đếm số lần in) trước khi in. Kết quả: RC, OUT, PW, PW_HITS.
PW=""
PW_HITS=0
install_run() {
  runq "$@"
  local pw
  pw="$(tr -d '\r' <"$TMPD/run.out" | grep -A1 '^Mật khẩu quản trị (chỉ hiện MỘT lần' | sed -nE 's/^    ([A-Z2-7]{26})$/\1/p' | head -1)"
  PW_HITS=0
  if [ -n "$pw" ]; then
    PW="$pw"
    add_secret "$pw"
    PW_HITS="$(grep -oF -- "$pw" "$TMPD/run.out" | wc -l | tr -d ' ')"
  fi
  show_last
  printf 'Mật khẩu quản trị panel trong output: %s lần\n' "$PW_HITS"
}

# ---------------------------------------------------------------- đoạn chạy trong máy ảo

# Dựng pebble (đồ nghề test, như e2e-m5.sh). Chạy bằng root trong máy ảo.
PEBBLE_SCRIPT='
set -e
export DEBIAN_FRONTEND=noninteractive
apt-get -o DPkg::Lock::Timeout=120 install -y -q pebble >/dev/null
dpkg-query -W -f "gói: \${Package} \${Version}\n" pebble
d=$1
mkdir -p "$d"
chmod 700 "$d"
cd "$d"
openssl req -x509 -newkey rsa:2048 -nodes -days 7 -keyout ca.key -out ca.pem \
  -subj "/CN=tpan e2e pebble API CA (chi may ao test)" -addext "keyUsage=critical,keyCertSign,cRLSign" 2>/dev/null
openssl req -newkey rsa:2048 -nodes -keyout key.pem -out api.csr -subj "/CN=localhost" 2>/dev/null
printf "subjectAltName=DNS:localhost,IP:127.0.0.1\nbasicConstraints=CA:FALSE\nkeyUsage=critical,digitalSignature,keyEncipherment\nextendedKeyUsage=serverAuth\n" >api.ext
openssl x509 -req -in api.csr -CA ca.pem -CAkey ca.key -CAcreateserial -days 7 -out cert.pem -extfile api.ext 2>/dev/null
cp ca.pem /usr/local/share/ca-certificates/tpan-e2e-pebble.crt
update-ca-certificates 2>&1 | tail -1
printf "{\"pebble\":{\"listenAddress\":\"127.0.0.1:14000\",\"managementListenAddress\":\"127.0.0.1:15000\",\"certificate\":\"%s/cert.pem\",\"privateKey\":\"%s/key.pem\",\"httpPort\":80,\"tlsPort\":443,\"ocspResponderURL\":\"\",\"externalAccountBindingRequired\":false}}\n" "$d" "$d" >config.json
systemd-run --unit='"$PEBBLE_UNIT"' --setenv=PEBBLE_VA_NOSLEEP=1 --setenv=PEBBLE_WFE_NONCEREJECT=0 --setenv=PEBBLE_AUTHZREUSE=0 \
  /usr/bin/pebble -config "$d/config.json"
for i in $(seq 1 30); do
  curl -s -o /dev/null https://localhost:14000/dir && break
  sleep 1
done
'

# Một lần gọi vào site: mã HTTP, số dòng có dấu trang tạm khoá
# (templates.SuspendedMarker = tpan-suspended-page), <title>.
# PAGE_SCRIPT <domain> [http|https] [đường dẫn]
PAGE_SCRIPT='
d=$1; scheme=${2:-http}; path=${3:-/}
port=80; [ "$scheme" = https ] && port=443
f=$(mktemp)
code=$(curl -sk -m 20 --resolve "$d:$port:127.0.0.1" -o "$f" -w "%{http_code}" "$scheme://$d$path"); rc=$?
echo "$scheme://$d$path curl=$rc http=$code marker=$(grep -c tpan-suspended-page "$f") title=$(grep -o "<title>[^<]*</title>" "$f" | head -1)"
rm -f "$f"
'
page() { vexec bash -c "$PAGE_SCRIPT" _ "$@"; }
is403() { has 'http=403 marker=[1-9]'; }
is200() { has 'http=200 marker=0 title=<title>'; }

# Trạng thái pool, socket, cron và vhost của một site.
# SITE_FILES_SCRIPT <slug> <bản PHP> <domain> <socket>
SITE_FILES_SCRIPT='
slug=$1 v=$2 d=$3 sock=$4
echo "== pool"; ls /etc/php/$v/fpm/pool.d/ | grep -F "tpan-$slug" | sed "s/^/pool: /"
if [ -S "$sock" ]; then echo "socket: có"; else echo "socket: không có"; fi
echo "== cron"; ls /etc/cron.d/ | grep -F "tpan-$slug" | sed "s/^/cron: /"
echo "== php-fpm$v -t"; php-fpm$v -t 2>&1 | tail -2; echo "php-fpm -t mã ${PIPESTATUS[0]}"
systemctl is-active php$v-fpm | sed "s/^/php$v-fpm: /"
for f in /etc/nginx/sites-available/tpan-$d.conf /etc/apache2/sites-available/tpan-$d.conf; do
  [ -f "$f" ] && echo "vhost $f: dấu trang tạm khoá $(grep -c tpan-suspended "$f") dòng, 443: $(grep -cE "^[^#]*(listen|VirtualHost).*443" "$f") dòng"
done
'

# Job mới nhất của một loại: trạng thái, các lệnh.
JOB_PY='
import sqlite3, sys
c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True)
print("số job trong tpan.db:", c.execute("SELECT COUNT(*) FROM jobs").fetchone()[0])
print("số sự kiện command:", c.execute("SELECT COUNT(*) FROM job_events WHERE kind = \"command\"").fetchone()[0])
if len(sys.argv) < 2:
    sys.exit(0)
j = c.execute("SELECT id, status, target, actor, owner FROM jobs WHERE kind = ? ORDER BY started_at DESC, rowid DESC LIMIT 1", (sys.argv[1],)).fetchone()
if j is None:
    print("không có job", sys.argv[1]); sys.exit(0)
print("job", sys.argv[1], j[0], j[1], "| target:", j[2], "| actor:", j[3] or "-", "| owner:", j[4])
for kind, step, msg in c.execute("SELECT kind, step_id, message FROM job_events WHERE job_id = ? AND kind IN (\"command\", \"step.fail\", \"undo.done\", \"undo.fail\") ORDER BY seq", (j[0],)):
    print(" ", kind, step or "-", msg[:240].replace("\n", " "))
'

# SQL chỉ đọc trên tpan.db: SQL_PY <câu> [tham số...], in từng dòng.
SQL_PY='
import sqlite3, sys
c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True)
for r in c.execute(sys.argv[1], sys.argv[2:]):
    print(*[str(x) for x in r], sep="\t")
'
# -W ignore::DeprecationWarning: Python 3.12 cảnh báo ra stderr khi tham số
# ?1..?5 đi kèm một danh sách, và OUT gộp stdout với stderr theo thứ tự không cố
# định, nên hai lần chạy cùng một câu có thể ra hai chuỗi khác nhau (hồi quy M28
# ngày 21/11: P2 đỏ dù hai lần đếm đều ra "8 181").
sql() { vroot python3 -W ignore::DeprecationWarning -c "$SQL_PY" "$@"; }
# Job do một user chạy (actor) và job trên vài target, kèm số sự kiện command của chúng.
# Năm đối số theo thứ tự: actor, bốn target. Chỉ dùng chỗ trống `?` thường:
# `?1`..`?5` với một dãy đối số làm Python 3.12 in DeprecationWarning ra
# stderr, lẫn vào output đem so (thứ tự dòng stdout/stderr không cố định, hồi
# quy M13 trong M18 hỏng vì thế dù số liệu y hệt).
JOBS_SQL="WITH p AS (SELECT ? AS a, ? AS t1, ? AS t2, ? AS t3, ? AS t4), j AS (SELECT jobs.id FROM jobs, p WHERE jobs.actor = p.a OR jobs.target IN (p.t1, p.t2, p.t3, p.t4)) SELECT (SELECT COUNT(*) FROM j), (SELECT COUNT(*) FROM job_events e WHERE e.kind = 'command' AND e.job_id IN (SELECT id FROM j))"

# Trạng thái khoá của user MariaDB (mục 16: đọc mysql.global_priv, KHÔNG
# SHOW CREATE USER vì câu đó in cả hash mật khẩu).
locked_of() { vroot mariadb -N -B -e "SELECT User, JSON_VALUE(Priv, '\$.account_locked') FROM mysql.global_priv WHERE User = '$1' AND Host = 'localhost'"; }

# db_cnf <tên DB rời>: ghi /root/e2e-<tên>.cnf (0600) trong máy ảo từ mật khẩu
# đang giữ ở $TMPD/pw.<tên>; mật khẩu đi qua stdin.
db_cnf() {
  (umask 077 && printf '[client]\nuser=%s\npassword=%s\n' "$1" "$(cat "$TMPD/pw.$1")" >"$TMPD/cnf")
  run_in "$TMPD/cnf" "$VM" root bash -c 'umask 077; cat >"/root/e2e-$1.cnf"; stat -c "%a %U %n" "/root/e2e-$1.cnf"' _ "$1"
}
# db_login <tên DB rời>: đăng nhập MariaDB bằng user và mật khẩu của DB đó.
db_login() { vroot bash -c 'mariadb --defaults-extra-file="/root/e2e-$1.cnf" -N -B -e "SELECT CURRENT_USER()" 2>&1; echo "mã $?"' _ "$1"; }
login_locked() { has 'ERROR 4151' && has '^mã 1$'; }
login_ok() { has "^$1@localhost\$" && has '^mã 0$'; }

# ---------------------------------------------------------------- health

health_json() {
  printf '\n[%s] $ %s root tpan health %s --json\n' "$(ts)" "$VM" "$*"
  "$VM" root tpan health "$@" --json </dev/null >"$TMPD/health.raw" 2>"$TMPD/health.err"
  RC=$?
  tr -d '\r' <"$TMPD/health.raw" >"$HJ"
  OUT="$(cat "$TMPD/health.err")"
  mask
  [ -n "$OUT" ] && printf 'stderr:\n%s\n' "$OUT"
  if ! jq -e .groups "$HJ" >/dev/null 2>&1; then
    OUT="$(head -c 3000 "$HJ")"
    mask
    printf 'stdout không phải báo cáo JSON:\n%s\n[%s] (mã thoát %s)\n' "$OUT" "$(ts)" "$RC"
    return
  fi
  OUT="$(jq -r '"nhóm: \([.groups[] | if .target == "" then "(máy, stack)" else .target end] | join(", "))",
    "số phép kiểm theo trạng thái: \([.groups[].results[].status] | group_by(.) | map("\(.[0])=\(length)") | join(" "))",
    ([.groups[].results[] | select(.status != "pass")] | if length == 0 then "(mọi phép kiểm đạt)" else (.[] |
      "--- \(.status)\(if .suspended then " [tạm khoá]" else "" end) \(.id) (chặng \(.stage)): \(.summary.vi)") end),
    "sự cố vừa mở: \([.changes.opened[]? | "#\(.id) \(.check_id)"] | join(", "))",
    "sự cố vừa đóng: \([.changes.closed[]? | "#\(.id) \(.check_id)"] | join(", "))"' "$HJ")"
  mask
  printf '%s\n[%s] (mã thoát %s)\n' "$OUT" "$(ts)" "$RC"
}
hq() { jq -e "$@" "$HJ" >/dev/null 2>&1; }
no_fail() { hq '[.groups[].results[] | select(.status == "fail")] | length == 0'; }
none_opened() { hq '(.changes.opened // []) | length == 0'; }
check_status() { hq --arg id "$1" --arg st "$2" '[.groups[].results[] | select(.id == $id and .status == $st)] | length == 1'; }
# stage_grey <domain> <chặng>: mọi kết quả của chặng đó là unknown + suspended.
stage_grey() { hq --arg d "$1" --arg s "$2" '[.groups[] | select(.target == $d) | .results[] | select(.stage == $s)] as $r | ($r | length) > 0 and all($r[]; .status == "unknown" and .suspended == true)'; }
stage_pass() { hq --arg d "$1" --arg s "$2" '[.groups[] | select(.target == $d) | .results[] | select(.stage == $s)] as $r | ($r | length) > 0 and all($r[]; .status == "pass" and (.suspended | not))'; }

# ---------------------------------------------------------------- CLI

# site_add <domain> [cờ...]: tpan site add, nhặt mật khẩu quản trị WordPress
# và mật khẩu DB của site vào danh sách che trước khi in.
site_add() {
  local d=$1 pw
  shift
  runq "$VM" root tpan site add "$d" --email "$EMAIL" --yes "$@"
  pw="$(tr -d '\r' <"$TMPD/run.out" | sed -nE 's/^  Mật khẩu: +([A-Za-z0-9]{32})$/\1/p' | head -1)"
  add_secret "$pw"
  add_secret "$(dbpw "$d")"
  show_last
  ok "site add $d $*: thoát 0, job thành công" job_ok
}

# json_secret <tên> <biểu thức jq> <lệnh...>: chạy lệnh --json, đọc mật khẩu
# theo biểu thức jq vào $TMPD/pw.<tên> rồi mới in.
# --json: stdout chỉ có JSON, tiến độ của job ra stderr; đọc riêng hai luồng,
# in stderr trước rồi tới stdout. OUT chỉ là JSON (cho jout).
json_secret() {
  local name=$1 expr=$2 pw
  shift 2
  RUNQ_CMD="$(mask_str <<<"$*")"
  RUNQ_TS="$(ts)"
  "$@" </dev/null >"$TMPD/run.json" 2>"$TMPD/run.err"
  RC=$?
  pw="$(tr -d '\r' <"$TMPD/run.json" | jq -r "$expr // empty" 2>/dev/null)"
  [ -n "$pw" ] && save_pw "$name" "$pw"
  cat "$TMPD/run.err" "$TMPD/run.json" >"$TMPD/run.out"
  show_last
  OUT="$(tr -d '\r' <"$TMPD/run.json")"
  mask
  JS_PW="$pw"
}

# ---------------------------------------------------------------- API (từ Mac)

# api <jar> <METHOD> <đường dẫn> [thân JSON]: gọi panel qua TLS (--cacert là
# cert.pem lấy từ máy ảo, không -k), cookie ở jar. Mọi request mang Origin và
# Sec-Fetch-Site như trình duyệt (để thứ được kiểm là luật của route, không
# phải chặn CSRF). Thân trả về ở $TMPD/body; mọi trường "password" trong đó và
# giá trị cookie vào danh sách che TRƯỚC khi in.
api() {
  local jar=$1 m=$2 p=$3 body=${4-}
  local args=(-sS -m 60 --cacert "$TMPD/cert.pem" -b "$jar" -c "$jar" -X "$m" -o "$TMPD/body"
    -w '-> HTTP %{http_code}\n' -H 'Accept: application/json' -H 'Sec-Fetch-Site: same-origin' -H "Origin: $BASE")
  : >"$TMPD/body"
  if [ -n "$body" ]; then
    (umask 077 && printf '%s' "$body" >"$TMPD/req.json")
    args+=(-H 'Content-Type: application/json' --data-binary "@$TMPD/req.json")
  fi
  printf '\n[%s] $ curl -X %s <panel>%s   (phiên: %s)\n' "$(ts)" "$m" "$p" "${jar##*/}"
  [ -n "$body" ] && printf '    thân gửi: %s\n' "$(mask_str <<<"$body")"
  curl "${args[@]}" "$BASE$p" </dev/null >"$TMPD/run.out" 2>&1
  RC=$?
  CODE="$(sed -nE 's/^-> HTTP ([0-9]{3})$/\1/p' "$TMPD/run.out" | tail -1)"
  harvest "$jar"
  OUT="$(tr -d '\r' <"$TMPD/run.out")"
  mask
  printf '%s\n' "$OUT"
  if jq -e . "$TMPD/body" >/dev/null 2>&1; then
    OUT="$(jq -c . "$TMPD/body" | head -c 700)"
  else
    OUT="$(head -c 400 "$TMPD/body")"
  fi
  mask
  [ -n "$OUT" ] && printf '    thân nhận (tối đa 700 byte): %s\n' "$OUT"
}
harvest() {
  local s
  while IFS= read -r s; do add_secret "$s"; done < <(jq -r '[.. | objects | (.password?, .db_password?) | strings] | .[]' "$TMPD/body" 2>/dev/null)
  [ -f "$1" ] && while IFS= read -r s; do add_secret "$s"; done < <(awk -F'\t' 'NF == 7 && ($1 !~ /^#/ || $1 ~ /^#HttpOnly_/) { print $7 }' "$1")
}
code_is() { [ "$CODE" = "$1" ]; }
ecode() { jq -r '.error.code // empty' "$TMPD/body" 2>/dev/null; }
# expect <mã HTTP> <mã lỗi>
expect() { [ "$CODE" = "$1" ] && [ "$(ecode)" = "$2" ]; }
bq() { jq -e "$@" "$TMPD/body" >/dev/null 2>&1; }
jb() { jq -r "$1" "$TMPD/body" 2>/dev/null; }
# lacks <chuỗi>...: thân trả về không chứa chuỗi nào trong số đó.
lacks() {
  local s
  for s in "$@"; do grep -qF -- "$s" "$TMPD/body" && return 1; done
  return 0
}
contains() {
  local s
  for s in "$@"; do grep -qF -- "$s" "$TMPD/body" || return 1; done
  return 0
}

JAR_ADM="" JAR_A="" JAR_B=""
new_jar() { : >"$TMPD/jar.$1"; chmod 600 "$TMPD/jar.$1"; printf '%s' "$TMPD/jar.$1"; }

# login <jar> <tên|""> <file mật khẩu>: POST /api/v1/login.
login() {
  local jar=$1 name=$2 f=$3 body
  if [ -n "$name" ]; then
    body="$(jq -nc --arg n "$name" --rawfile p "$f" '{name: $n, password: $p}')"
  else
    body="$(jq -nc --rawfile p "$f" '{password: $p}')"
  fi
  api "$jar" POST /api/v1/login "$body"
}

fetch_cert() {
  "$VM" root cat /etc/tpan/tls/cert.pem </dev/null | tr -d '\r' >"$TMPD/cert.pem"
  local i
  for i in $(seq 1 30); do
    curl -sS -m 5 --cacert "$TMPD/cert.pem" -o /dev/null "$BASE/api/v1/health" 2>/dev/null && return 0
    sleep 1
  done
  return 1
}

# wait_job <jar> <id>: hỏi GET /jobs/{id} mỗi giây tới khi xong (tối đa 600
# giây). Lần GET đầu sau khi thành công có mật khẩu một lần (secret,
# db_secret): thân của mọi lần hỏi đều được nhặt bí mật rồi giữ lại ở
# $TMPD/body (không bỏ đi). In tóm tắt.
JOBST=""
wait_job() {
  local jar=$1 id=$2 i
  JOBST=""
  for i in $(seq 1 600); do
    curl -sS -m 10 --cacert "$TMPD/cert.pem" -b "$jar" -H 'Accept: application/json' -o "$TMPD/body" "$BASE/api/v1/jobs/$id" </dev/null 2>/dev/null
    harvest "$jar"
    JOBST="$(jq -r '.job.status // empty' "$TMPD/body" 2>/dev/null)"
    [ -n "$JOBST" ] && [ "$JOBST" != running ] && break
    sleep 1
  done
  OUT="$(jq -c '{job: (.job | {kind, target, status, actor, owner, error}), secret: (if .secret then "có (đã che)" else null end), db_secret: (if .db_secret then {db: .db_secret.db, user: .db_secret.user, password: "có (đã che)"} else null end), commands: [.events[] | select(.kind == "command") | .message[:160]] | .[:12]}' "$TMPD/body" 2>&1)"
  mask
  printf '(chờ job %s: %s giây)\n%s\n' "$id" "$i" "$OUT"
}

# ================================================================ NÂNG CẤP

part_u0() {
  part_begin "U0: gói phát hành $OLD_VER (bản đã phát hành, không build lại), máy sạch"
  if [ ! -d "dist/release/$OLD_VER" ]; then
    run mkdir -p dist/release
    run cp -R "$OLD_PKG" "dist/release/$OLD_VER"
  fi
  run bash -c 'cd "$1" && ls -l && cat SHA256SUMS && shasum -a 256 -c SHA256SUMS' _ "dist/release/$OLD_VER"
  ok "gói $OLD_VER có đủ file, SHA256SUMS khớp" rc_is 0
  reset_vm
  run multipass transfer -r "dist/release/$OLD_VER" "$VMNAME:/home/ubuntu/"
  ok "chép gói $OLD_VER sang máy ảo" rc_is 0
  part_end U0
}

HFP0=""
part_u1() {
  part_begin "U1: cài $OLD_VER bằng install.sh của chính nó, tạo site, đăng nhập"
  install_run "$VM" exec bash -c "cd /home/ubuntu/$OLD_VER && sudo ./install.sh --web nginx"
  ok "install.sh $OLD_VER thoát 0" rc_is 0
  ok "install.sh in bản $OLD_VER" has "^tpan $OLD_VER "
  ok "mật khẩu quản trị in đúng một lần" test "$PW_HITS" -eq 1
  save_pw admin "$PW"
  vexec tpan version
  ok "tpan version là $OLD_VER" has "^tpan $OLD_VER "
  site_add "$UPD"
  page "$UPD"
  ok "$UPD trả 200" is200
  fetch_cert
  ok "panel trả lời qua https (--cacert)" rc_is 0
  JAR_OLD="$(new_jar old)"
  login "$JAR_OLD" "" "$TMPD/pw.admin"
  ok "$OLD_VER: POST /login chỉ có mật khẩu: 204" code_is 204
  api "$JAR_OLD" GET /api/v1/sites
  ok "$OLD_VER: GET /sites 200, có $UPD" bash -c '[ "$1" = 200 ] && grep -qF "$2" "$3"' _ "$CODE" "$UPD" "$TMPD/body"
  vroot python3 -c 'import sqlite3, hashlib; c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True); print("hash mật khẩu (sha256 của hash):", hashlib.sha256(c.execute("SELECT value FROM settings WHERE key = ?", ("admin.password_hash",)).fetchone()[0].encode()).hexdigest()[:16]); print("migration:", [r[0] for r in c.execute("SELECT version FROM schema_migrations ORDER BY version")])'
  HFP0="$(sed -nE 's/^hash mật khẩu \(sha256 của hash\): ([0-9a-f]{16})$/\1/p' <<<"$OUT")"
  ok "đọc được dấu của hash mật khẩu quản trị trong settings ($OLD_VER)" test -n "$HFP0"
  ok "$OLD_VER chưa có migration 1300" hasntF '1300'
  part_end U1
}

part_u2() {
  part_begin "U2: nâng lên $VER bằng tpan self-update --url của bản $OLD_VER"
  run git log --oneline -1
  run git status --short
  run make release VERSION="$VER" DATE="$BUILD_DATE"
  ok "make release VERSION=$VER" rc_is 0
  run bash -c 'cd "$1" && cat SHA256SUMS && shasum -a 256 -c SHA256SUMS' _ "dist/release/$VER"
  ok "SHA256SUMS của gói $VER khớp" rc_is 0
  vexec mkdir -p /home/ubuntu/rel
  run multipass transfer -r "dist/release/$VER" "$VMNAME:/home/ubuntu/rel/"
  ok "chép gói $VER sang máy ảo" rc_is 0
  vroot systemd-run --unit=tpan-e2e-rel python3 -m http.server "$PORT" --bind 127.0.0.1 --directory /home/ubuntu/rel
  REL_UP=1
  sleep 2
  local base="http://127.0.0.1:$PORT/$VER"
  vexec curl -sS -o /dev/null -w '%{http_code}\n' "$base/SHA256SUMS"
  ok "(đồ nghề) máy chủ HTTP loopback trả $VER/SHA256SUMS" out_is 200
  vroot tpan self-update --url "$base" --dry-run
  ok "self-update --dry-run (bản $OLD_VER) thoát 0" rc_is 0
  vexec systemctl show -p MainPID tpan
  local svc0=$OUT
  vroot tpan self-update --url "$base" --yes
  ok "self-update (bản $OLD_VER) thoát 0, job thành công" job_ok
  ok "in $OLD_VER -> $VER" hasF "$OLD_VER -> $VER"
  vexec bash -c 'tpan version; /usr/local/bin/tpan.old version; ls -la /usr/local/bin'
  ok "tpan version là $VER, tpan.old là $OLD_VER" bash -c 'grep -q "^tpan $2 " <<<"$1" && grep -q "^tpan $3 " <<<"$1"' _ "$OUT" "$VER" "$OLD_VER"
  local i
  for i in $(seq 1 30); do
    curl -sS -m 5 --cacert "$TMPD/cert.pem" -o /dev/null "$BASE/api/v1/health" 2>/dev/null && break
    sleep 1
  done
  vexec systemctl show -p MainPID -p ActiveState tpan
  ok "tpan.service active, PID mới" bash -c 'grep -q "ActiveState=active" <<<"$1" && [ "$(grep MainPID <<<"$1")" != "$2" ]' _ "$OUT" "$svc0"
  vroot bash -c 'p=$(systemctl show -p MainPID --value tpan); /proc/$p/exe version'
  ok "tiến trình của tpan.service chạy bản $VER" has "^tpan $VER "
  part_end U2
}

part_u3() {
  part_begin "U3: sau khi nâng: cookie cũ, đăng nhập chỉ mật khẩu, chủ của site cũ, admin passwd"
  api "$JAR_OLD" GET /api/v1/session
  ok "cookie cũ (tạo bởi $OLD_VER): GET /session 200, logged_in, user admin vai admin" bash -c '[ "$1" = 200 ] && jq -e ".logged_in and .user.name == \"admin\" and .user.role == \"admin\"" "$2" >/dev/null' _ "$CODE" "$TMPD/body"
  api "$JAR_OLD" GET /api/v1/sites
  ok "cookie cũ: GET /sites 200, $UPD có chủ admin" bash -c '[ "$1" = 200 ] && jq -e --arg d "$3" "[.. | objects | select(.domain? == \$d and .owner? == \"admin\")] | length >= 1" "$2" >/dev/null' _ "$CODE" "$TMPD/body" "$UPD"
  local J2
  J2="$(new_jar new)"
  login "$J2" "" "$TMPD/pw.admin"
  ok "POST /login chỉ có mật khẩu (không tên): 204" code_is 204
  api "$J2" GET /api/v1/session
  ok "phiên mới là admin" bq '.user.name == "admin"'
  api "$J2" GET /api/v1/users
  ok "GET /users (route admin của M13) 200, có admin" bash -c '[ "$1" = 200 ] && grep -q "\"name\":\"admin\"" "$2"' _ "$CODE" "$TMPD/body"
  vroot tpan site list
  ok "tpan site list: $UPD, cột CHỦ là admin" has "^$UPD .* admin "
  vroot tpan site info "$UPD" --json
  ok "site info --json: owner admin, không tạm khoá" jout '.site.owner == "admin" and ((.site.suspended // "") == "")'
  vroot tpan user list
  ok "tpan user list có admin" has '^admin '
  vroot tpan user list --json
  ok "user list --json: admin vai admin, gói default, active" jout '[.[] | select(.user.name == "admin" and .user.role == "admin" and .user.package == "default" and .user.status == "active")] | length == 1'
  vroot python3 -c 'import sqlite3, hashlib; c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True); print("hash mật khẩu (sha256 của hash):", hashlib.sha256(c.execute("SELECT password_hash FROM users WHERE name = ?", ("admin",)).fetchone()[0].encode()).hexdigest()[:16]); print("settings còn admin.password_hash:", c.execute("SELECT COUNT(*) FROM settings WHERE key = ?", ("admin.password_hash",)).fetchone()[0]); print("migration 1300:", c.execute("SELECT COUNT(*) FROM schema_migrations WHERE version = 1300").fetchone()[0]); print("phiên:", c.execute("SELECT user_name, view_as FROM sessions").fetchall())'
  ok "hash mật khẩu chuyển sang users giữ nguyên ($HFP0), settings không còn khoá cũ, có migration 1300" bash -c 'grep -q "^hash mật khẩu (sha256 của hash): $2$" <<<"$1" && grep -q "^settings còn admin.password_hash: 0$" <<<"$1" && grep -q "^migration 1300: 1$" <<<"$1"' _ "$OUT" "$HFP0"
  page "$UPD"
  ok "$UPD vẫn 200" is200

  section "U3: tpan admin passwd"
  json_secret admin '.admin_password' "$VM" root tpan admin passwd --yes --json
  ok "admin passwd thoát 0, có mật khẩu mới (đã che)" bash -c '[ "$1" = 0 ] && [ -n "$2" ]' _ "$RC" "$JS_PW"
  api "$J2" GET /api/v1/sites
  ok "phiên cũ bị huỷ: 401" expect 401 unauthorized
  JAR_ADM="$(new_jar adm)"
  login "$JAR_ADM" admin "$TMPD/pw.admin"
  ok "đăng nhập tên admin + mật khẩu mới: 204" code_is 204
  api "$JAR_ADM" GET /api/v1/users
  ok "GET /users 200" code_is 200

  # M14 (mục 6.13 của docs/KE-HOACH.md): máy nâng từ bản trước M14 thiếu file
  # ini OPcache, quyền log theo nhóm và dòng chặn trong vhost cho tới khi chạy
  # Plan "Áp bản vá cô lập". Trước M14 chỗ này chỉ khẳng định "health không có
  # Fail"; giờ là: chỉ có đúng ba loại lỗi đó, và harden đưa về không còn lỗi.
  section "U3: (M14) tpan security harden trên máy vừa nâng"
  health_json
  ok "health sau khi nâng: có lỗi, và chỉ thuộc ba loại của M14 (php.opcache, log.perm, web.hardened)" hq '[.groups[].results[] | select(.status == "fail") | .id | split(":")[0]] | (length > 0 and ((. - ["php.opcache", "log.perm", "web.hardened"]) | length == 0))'
  vroot tpan security harden --yes
  ok "tpan security harden thoát 0, job thành công" job_ok
  health_json
  ok "health không có Fail" no_fail
  page "$UPD"
  ok "$UPD vẫn 200 sau harden" is200
  part_end U3
}

part_u4() {
  part_begin "U4: tpan self-update --rollback (ghi nguyên output cho hướng dẫn)"
  vexec systemctl show -p MainPID -p NRestarts tpan
  local svc0
  svc0="$OUT"
  vroot tpan self-update --rollback --dry-run
  # Từ M15 (KE-HOACH mục 6.14 ý 7), PrepareRollback chạy `tpan.old site list`
  # trước khi đổi file; file cũ không mở được tpan.db thì từ chối
  # self_rollback_db, kèm chính câu file cũ in ra. Trước M15 lệnh này thoát 0,
  # bản cũ được cài rồi tpan.service khởi động lại liên tục (chỗ hở mục 6.12):
  # phần này từng kiểm đúng chỗ hở đó, giờ kiểm là nó đã đóng.
  ok "rollback --dry-run thoát 1, từ chối: bản trước không mở được tpan.db (câu của file cũ: DB ở bản $NEW_MIG)" bash -c '[ "$2" = 1 ] && grep -qF "Từ chối: bản trước (/usr/local/bin/tpan.old) không mở được /var/lib/tpan/tpan.db" <<<"$1" && grep -qF "DB ở bản $3, tpan này chỉ biết tới bản 900" <<<"$1"' _ "$OUT" "$RC" "$NEW_MIG"
  vroot tpan self-update --rollback --yes
  printf '(Output thật của lệnh rollback ở trên.)\n'
  ok "rollback --yes thoát 1, từ chối như trên, chưa đổi gì" bash -c '[ "$2" = 1 ] && grep -qF "nên chưa đổi gì" <<<"$1" && grep -qF "DB ở bản $3, tpan này chỉ biết tới bản 900" <<<"$1"' _ "$OUT" "$RC" "$NEW_MIG"
  vexec bash -c 'tpan version; ls -la /usr/local/bin'
  ok "file chạy vẫn là $VER, tpan.old vẫn còn" bash -c 'grep -q "^tpan $2 " <<<"$1" && grep -q " tpan.old$" <<<"$1"' _ "$OUT" "$VER"
  vexec systemctl show -p MainPID -p NRestarts tpan
  ok "tpan.service không khởi động lại: MainPID và NRestarts như trước rollback" bash -c '[ -n "$1" ] && [ "$1" = "$2" ]' _ "$svc0" "$OUT"
  vexec systemctl is-active tpan
  ok "tpan.service active" has '^active$'
  run curl -sS -m 5 --cacert "$TMPD/cert.pem" -o /dev/null -w '%{http_code}\n' "$BASE/api/v1/health"
  ok "panel vẫn trả lời 200" bash -c '[ "$2" = 0 ] && grep -qx 200 <<<"$1"' _ "$OUT" "$RC"
  vroot tpan site list
  ok "tpan site list của bản $VER chạy được, còn $UPD" bash -c '[ "$2" = 0 ] && grep -qF "$3" <<<"$1"' _ "$OUT" "$RC" "$UPD"
  part_end U4
}

part_u5() {
  part_begin "U5: về lại $VER bằng install.sh của gói $VER (file chạy có sẵn trong gói)"
  install_run "$VM" exec bash -c "cd /home/ubuntu/rel/$VER && sudo ./install.sh"
  ok "install.sh $VER thoát 0" rc_is 0
  ok "kiểm sha256 trong install.sh: khớp" hasF "sha256 khớp: tpan-linux-"
  ok "install.sh không sinh mật khẩu quản trị mới (đã có)" test "$PW_HITS" -eq 0
  vexec tpan version
  ok "tpan version là $VER" has "^tpan $VER "
  local i
  for i in $(seq 1 30); do
    curl -sS -m 5 --cacert "$TMPD/cert.pem" -o /dev/null "$BASE/api/v1/health" 2>/dev/null && break
    sleep 1
  done
  vroot bash -c 'p=$(systemctl show -p MainPID --value tpan); /proc/$p/exe version; systemctl is-active tpan'
  ok "tpan.service active, chạy bản $VER" bash -c 'grep -q "^tpan $2 " <<<"$1" && grep -qx active <<<"$1"' _ "$OUT" "$VER"
  JAR_ADM="$(new_jar adm)"
  login "$JAR_ADM" admin "$TMPD/pw.admin"
  ok "đăng nhập admin bằng mật khẩu đặt ở U3: 204" code_is 204
  api "$JAR_ADM" GET /api/v1/sites
  ok "GET /sites 200, còn $UPD" bash -c '[ "$1" = 200 ] && grep -qF "$2" "$3"' _ "$CODE" "$UPD" "$TMPD/body"
  vroot python3 -c 'import sqlite3; c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True); print("integrity:", c.execute("PRAGMA integrity_check").fetchone()[0]); print("migration:", [r[0] for r in c.execute("SELECT version FROM schema_migrations ORDER BY version")]); print("users:", c.execute("SELECT name, role, status FROM users").fetchall()); print("job self.*:", c.execute("SELECT kind, status FROM jobs WHERE kind LIKE ? ORDER BY started_at", ("self.%",)).fetchall())'
  ok "tpan.db nguyên vẹn sau khi bản cũ chạm vào" has '^integrity: ok$'
  page "$UPD"
  ok "$UPD 200" is200
  health_json
  ok "health không có Fail" no_fail
  vroot systemctl stop tpan-e2e-rel
  REL_UP=0
  part_end U5
}

# ================================================================ CHÍNH

part_p0() {
  part_begin "P0: máy sạch, tpan $VER, stack nginx, tpan setup, pebble"
  reset_vm
  run make vm-deploy DATE="$BUILD_DATE"
  ok "build và chép tpan vào máy ảo" rc_is 0
  run git log --oneline -1
  run git status --short
  vroot tpan stack install --web nginx --yes
  ok "stack install thoát 0, job thành công" job_ok
  vexec systemctl is-active nginx php8.3-fpm mariadb cron
  ok "nginx, php8.3-fpm, mariadb, cron active" count_is 4 '^active$'
  vexec mariadb --version

  section "P0: tpan setup, đăng nhập admin bằng tên"
  install_run "$VM" root tpan setup --yes
  ok "setup thoát 0" rc_is 0
  ok "mật khẩu quản trị in đúng một lần" test "$PW_HITS" -eq 1
  save_pw admin "$PW"
  fetch_cert
  ok "panel trả lời qua https" rc_is 0
  JAR_ADM="$(new_jar adm)"
  login "$JAR_ADM" admin "$TMPD/pw.admin"
  ok "admin đăng nhập bằng tên + mật khẩu: 204" code_is 204
  grep -q '__Host-tpan' "$JAR_ADM"
  ok "cookie là __Host-tpan (TLS)" rc_is 0

  section "P0: đồ nghề: pebble và /etc/hosts cho $S1 (KHÔNG phải cách triển khai)"
  vroot bash -c "$PEBBLE_SCRIPT" _ "$PEBBLE_DIR"
  ok "dựng pebble thoát 0" rc_is 0
  vroot bash -c 'echo "127.0.0.1 $1" >>/etc/hosts && getent ahostsv4 "$1" | head -1' _ "$S1"
  ok "$S1 -> 127.0.0.1" has '^127\.0\.0\.1 '
  vroot tpan ssl settings --email "$EMAIL" --server "$ACME"
  ok "ssl settings: email và máy chủ ACME thử" rc_is 0
  part_end P0
}

JB="" BKB="" INC_B=""
part_p1() {
  part_begin "P1: gói, user, và đồ của mỗi user"
  vroot tpan package add "$PKN" --max-sites 2 --max-dbs 1 --yes
  ok "package add $PKN (2 site, 1 DB rời)" rc_is 0
  vroot tpan package add "$PKL" --max-sites 5 --max-dbs 5 --yes
  ok "package add $PKL" rc_is 0
  vroot tpan package list
  ok "package list có default, $PKN, $PKL" bash -c 'grep -q "^default " <<<"$1" && grep -q "^$2 " <<<"$1" && grep -q "^$3 " <<<"$1"' _ "$OUT" "$PKN" "$PKL"
  json_secret a '.password' "$VM" root tpan user add "$UA" --full-name "Khach A" --email a@example.com --package "$PKN" --yes --json
  ok "user add $UA (gói $PKN): thoát 0, mật khẩu sinh một lần (đã che)" bash -c '[ "$1" = 0 ] && [ -n "$2" ]' _ "$RC" "$JS_PW"
  json_secret b '.password' "$VM" root tpan user add "$UB" --full-name "Khach B" --email b@example.com --package "$PKL" --yes --json
  ok "user add $UB (gói $PKL)" bash -c '[ "$1" = 0 ] && [ -n "$2" ]' _ "$RC" "$JS_PW"
  vroot tpan user list
  ok "user list: admin, $UA, $UB" bash -c 'grep -q "^admin " <<<"$1" && grep -q "^$2 " <<<"$1" && grep -q "^$3 " <<<"$1"' _ "$OUT" "$UA" "$UB"
  JAR_A="$(new_jar a)"
  JAR_B="$(new_jar b)"
  login "$JAR_A" "$UA" "$TMPD/pw.a"
  ok "$UA đăng nhập bằng tên: 204" code_is 204
  login "$JAR_B" "$UB" "$TMPD/pw.b"
  ok "$UB đăng nhập bằng tên: 204" code_is 204
  api "$JAR_A" GET /api/v1/session
  ok "phiên của A: user $UA vai user" bq --arg n "$UA" '.user.name == $n and .user.role == "user"'

  section "P1: site của admin ($S1, có SSL) và của từng user"
  site_add "$S1"
  vroot tpan ssl issue "$S1" --yes
  ok "ssl issue $S1 (pebble): job thành công" job_ok
  page "$S1" https
  ok "https://$S1 200" is200
  site_add "$A1" --owner "$UA"
  api "$JAR_A" POST /api/v1/sites "$(jq -nc --arg d "$A2" --arg e a@example.com '{domain: $d, email: $e}')"
  ok "A tạo $A2 qua API: 202" code_is 202
  wait_job "$JAR_A" "$(jb .job_id)"
  ok "job site.add $A2 của A thành công, chủ $UA, người chạy $UA, có mật khẩu WordPress một lần" bash -c '[ "$1" = succeeded ] && jq -e --arg u "$2" ".job.owner == \$u and .job.actor == \$u and .secret != null" "$3" >/dev/null' _ "$JOBST" "$UA" "$TMPD/body"
  JA2ADD="$(jb .job.id)"
  add_secret "$(dbpw "$A2")"
  site_add "$B1" --owner "$UB"
  api "$JAR_B" POST /api/v1/sites "$(jq -nc --arg d "$B2" --arg e b@example.com '{domain: $d, email: $e}')"
  ok "B tạo $B2 qua API: 202" code_is 202
  wait_job "$JAR_B" "$(jb .job_id)"
  ok "job site.add $B2 thành công" test "$JOBST" = succeeded
  add_secret "$(dbpw "$B2")"
  api "$JAR_B" DELETE "/api/v1/sites/$B2"
  ok "B đưa $B2 vào thùng rác: 202" code_is 202
  wait_job "$JAR_B" "$(jb .job_id)"
  ok "job site.rm $B2 thành công" test "$JOBST" = succeeded
  vroot tpan site list --json
  ok "chủ: $S1 admin, $A1 và $A2 $UA, $B1 và $B2 $UB" jout --arg s "$S1" --arg a1 "$A1" --arg a2 "$A2" --arg b1 "$B1" --arg b2 "$B2" --arg ua "$UA" --arg ub "$UB" \
    '(map({(.domain): .owner}) | add) as $o | $o[$s] == "admin" and $o[$a1] == $ua and $o[$a2] == $ua and $o[$b1] == $ub and $o[$b2] == $ub'
  vroot tpan site list
  page "$A1"
  ok "$A1 200" is200
  page "$B1"
  ok "$B1 200" is200

  section "P1: DB rời"
  json_secret "$DA1" '.db_password' "$VM" root tpan db add d1 --owner "$UA" --note "e2e M13" --yes --json
  ok "db add d1 --owner $UA: $DA1, job thành công, mật khẩu một lần" bash -c '[ "$1" = 0 ] && [ -n "$2" ] && jq -e --arg n "$3" ".db == \$n and .report.job.status == \"succeeded\"" <<<"$4" >/dev/null' _ "$RC" "$JS_PW" "$DA1" "$OUT"
  api "$JAR_B" POST /api/v1/dbs '{"name":"d1"}'
  ok "B tạo DB rời d1 qua API: 202" code_is 202
  wait_job "$JAR_B" "$(jb .job_id)"
  ok "job dbs.add của B thành công, db_secret có ở lần GET đầu" bash -c '[ "$1" = succeeded ] && jq -e --arg n "$2" ".db_secret.db == \$n and (.db_secret.password | length) > 0" "$3" >/dev/null' _ "$JOBST" "$DB1" "$TMPD/body"
  save_pw "$DB1" "$(jb .db_secret.password)"
  JB="$(jb .job.id)"
  api "$JAR_B" GET "/api/v1/jobs/$JB"
  ok "lần GET thứ hai không còn mật khẩu" bq '.db_secret == null'
  api "$JAR_B" POST /api/v1/dbs '{"name":"d2"}'
  wait_job "$JAR_B" "$(jb .job_id)"
  ok "B tạo $DB2" test "$JOBST" = succeeded
  api "$JAR_B" DELETE "/api/v1/dbs/$DB2"
  ok "B đưa $DB2 vào thùng rác: 202" code_is 202
  wait_job "$JAR_B" "$(jb .job_id)"
  ok "job dbs.trash $DB2 thành công" test "$JOBST" = succeeded
  vroot tpan db list
  vroot tpan trash list --json
  ok "trash list --json: {sites, dbs}, có $B2 và $DB2" jout --arg b "$B2" --arg d "$DB2" '([.sites[].domain] | index($b) != null) and ([.dbs[].name] | index($d) != null)'

  section "P1: backup, job, sự cố của B"
  vroot tpan backup create "$A1" --yes
  ok "backup create $A1 (của A) thành công" job_ok
  api "$JAR_B" POST "/api/v1/sites/$B1/backups"
  ok "B backup $B1 qua API: 202" code_is 202
  JB="$(jb .job_id)"
  wait_job "$JAR_B" "$JB"
  ok "job backup.create của B thành công, chủ $UB" bash -c '[ "$1" = succeeded ] && jq -e --arg u "$2" ".job.owner == \$u" "$3" >/dev/null' _ "$JOBST" "$UB" "$TMPD/body"
  api "$JAR_B" GET "/api/v1/backups?domain=$B1"
  BKB="$(jq -r '.items[0].id // empty' "$TMPD/body")"
  ok "B thấy bản backup của $B1 (id $BKB)" test -n "$BKB"
  sql "SELECT id, domain, owner FROM backups WHERE domain = ?" "$B1"
  ok "bảng backups: bản của $B1 có owner $UB" has "^$BKB	$B1	$UB\$"
  run scripts/faults/04-wpconfig-perm.sh break "$B1"
  health_json "$B1"
  INC_B="$(jq -r --arg d "$B1" '[.changes.opened[]? | select(.target == $d)][0].id // empty' "$HJ")"
  ok "health $B1 mở một sự cố của B (#$INC_B)" test -n "$INC_B"
  part_end P1
}

# deny <mã HTTP> <mã lỗi> <METHOD> <đường dẫn> [thân]: phiên của A gọi rồi khẳng định.
deny() {
  local c=$1 e=$2
  shift 2
  api "$JAR_A" "$@"
  ok "A: $1 $2 -> $c $e" expect "$c" "$e"
}

part_p2() {
  part_begin "P2: cô lập qua API (phiên của A với đồ của B)"
  # Job do A chạy, hay job trên đồ của B: không được thêm dòng nào trong P2
  # (không so tổng số job: health của tpan serve có thể tự sửa và tạo job).
  sql "$JOBS_SQL" "$UA" "$B1" "$B2" "$DB1" "$DB2"
  local J0="$OUT"

  section "P2: đối chứng: B (chủ) và admin gọi được đúng các đường dẫn đó"
  api "$JAR_B" GET "/api/v1/sites/$B1"
  ok "B: GET /sites/$B1 200" code_is 200
  api "$JAR_B" GET "/api/v1/jobs/$JB"
  ok "B: GET /jobs/$JB 200" code_is 200
  run curl -sS -m 5 --cacert "$TMPD/cert.pem" -b "$JAR_B" -o "$TMPD/sse" -w '-> HTTP %{http_code}\n' "$BASE/api/v1/jobs/$JB/events"
  ok "B: GET /jobs/$JB/events (SSE) 200" has '^-> HTTP 200$'
  api "$JAR_B" GET "/api/v1/incidents/$INC_B"
  ok "B: GET /incidents/$INC_B 200" code_is 200
  api "$JAR_B" POST "/api/v1/backups/$BKB/restore?dry_run=1"
  ok "B: POST /backups/$BKB/restore?dry_run=1 200" code_is 200
  api "$JAR_B" DELETE "/api/v1/trash/$B2?dry_run=1"
  ok "B: DELETE /trash/$B2?dry_run=1 200" code_is 200
  api "$JAR_B" DELETE "/api/v1/trash/dbs/$DB2?dry_run=1"
  ok "B: DELETE /trash/dbs/$DB2?dry_run=1 200" code_is 200
  api "$JAR_B" POST "/api/v1/dbs/$DB1" '{"note":"cua B"}'
  ok "B: POST /dbs/$DB1 (ghi chú) 200" code_is 200
  api "$JAR_ADM" GET "/api/v1/sites/$B1"
  ok "admin: GET /sites/$B1 200" code_is 200

  section "P2: A gọi vào đồ của B: 404 như không tồn tại (không dry_run: chạy thật nếu lọt)"
  deny 404 site_not_found GET "/api/v1/sites/$B1"
  local m404b m404x
  m404b="$(jq -r .error.message.vi "$TMPD/body" | sed "s/$B1/<domain>/g")"
  deny 404 site_not_found GET "/api/v1/sites/khong-co.example.com"
  m404x="$(jq -r .error.message.vi "$TMPD/body" | sed "s/khong-co.example.com/<domain>/g")"
  ok "404 của site người khác giống hệt 404 của site không có (trừ tên domain)" test "$m404b" = "$m404x"
  deny 404 site_not_found DELETE "/api/v1/sites/$B1"
  deny 404 site_not_found POST "/api/v1/sites/$B1/ssl"
  deny 404 site_not_found POST "/api/v1/sites/$B1/php" '{"version":"8.3"}'
  deny 404 site_not_found POST "/api/v1/sites/$B1/backups"
  deny 404 site_not_found GET "/api/v1/sites/$B1/traffic"
  deny 404 site_not_found GET "/api/v1/sites/$B1/log"
  deny 404 site_not_found POST "/api/v1/sites/$B2/restore"
  deny 404 site_not_found DELETE "/api/v1/trash/$B2"
  deny 404 site_not_found POST "/api/v1/databases/$B1/password"
  deny 404 site_not_found POST "/api/v1/databases/$B1/dump"
  deny 404 site_not_found POST "/api/v1/databases/$B1/import" '{"id":"x"}'
  deny 404 site_not_found POST "/api/v1/databases/$B1/charset" '{"charset":"utf8mb4","collation":"utf8mb4_general_ci"}'
  deny 404 job_not_found GET "/api/v1/jobs/$JB"
  deny 404 job_not_found GET "/api/v1/jobs/0123456789abcdef"
  run curl -sS -m 10 --cacert "$TMPD/cert.pem" -b "$JAR_A" -o "$TMPD/sse" -w '-> HTTP %{http_code}\n' "$BASE/api/v1/jobs/$JB/events"
  ok "A: GET /jobs/$JB/events (SSE) 404 job_not_found" bash -c 'grep -q "^-> HTTP 404$" <<<"$1" && [ "$(jq -r .error.code "$2")" = job_not_found ]' _ "$OUT" "$TMPD/sse"
  deny 404 incident_not_found GET "/api/v1/incidents/$INC_B"
  deny 404 backup_not_found POST "/api/v1/backups/$BKB/restore"
  deny 404 db_not_found POST "/api/v1/dbs/$DB1" '{"note":"cua A"}'
  deny 404 db_not_found POST "/api/v1/dbs/$DB1/password"
  deny 404 db_not_found DELETE "/api/v1/dbs/$DB1"
  deny 404 db_not_found POST "/api/v1/dbs/$DB2/restore"
  deny 404 db_not_found DELETE "/api/v1/trash/dbs/$DB2"

  section "P2: tạo site: domain của B, owner B"
  api "$JAR_A" POST /api/v1/sites "$(jq -nc --arg d "$B1" --arg e a@example.com '{domain: $d, email: $e}')"
  ok "A: POST /sites với domain của B ($B1): 404 site_not_found" expect 404 site_not_found
  api "$JAR_A" POST /api/v1/sites "$(jq -nc --arg d "$N1" --arg e a@example.com --arg o "$UB" '{domain: $d, email: $e, owner: $o}')"
  ok "A: POST /sites owner $UB: 403 forbidden" expect 403 forbidden

  section "P2: apply, prune, export theo phạm vi của A"
  api "$JAR_A" POST '/api/v1/apply?dry_run=1' "$(jq -nc --arg d "$B1" '{sites: [{domain: $d}]}')"
  ok "A: apply?dry_run=1 có domain của B: dòng đó refused site_not_found" bash -c '[ "$1" = 200 ] && jq -e --arg d "$3" "[.items[] | select(.domain == \$d and .action == \"refused\" and .code == \"site_not_found\")] | length == 1" "$2" >/dev/null' _ "$CODE" "$TMPD/body" "$B1"
  api "$JAR_A" POST /api/v1/apply "$(jq -nc --arg d "$B1" '{sites: [{domain: $d}]}')"
  ok "A: apply thật có domain của B: không có gì để làm (409 nothing_to_do)" expect 409 nothing_to_do
  api "$JAR_A" POST '/api/v1/apply?dry_run=1' "$(jq -nc --arg d "$A1" '{sites: [{domain: $d}], prune: true}')"
  ok "A: apply?dry_run=1 prune chỉ có $A1: chỉ đưa $A2 vào thùng rác, không đụng $B1, $S1" bash -c '[ "$1" = 200 ] && jq -e --arg a2 "$3" "[.items[] | select(.action == \"trash\") | .domain] == [\$a2]" "$2" >/dev/null && ! grep -qF "$4" "$2" && ! grep -qF "$5" "$2"' _ "$CODE" "$TMPD/body" "$A2" "$B1" "$S1"
  api "$JAR_A" POST /api/v1/apply "$(jq -nc --arg a1 "$A1" --arg a2 "$A2" '{sites: [{domain: $a1}, {domain: $a2}], prune: true}')"
  ok "A: apply thật prune=true với đủ site của A: 409 nothing_to_do (site của B không bị chọn)" expect 409 nothing_to_do
  api "$JAR_B" GET "/api/v1/sites/$B1"
  ok "sau apply của A: $B1 vẫn active (B xem: 200)" bash -c '[ "$1" = 200 ] && grep -q "\"status\":\"active\"" "$2"' _ "$CODE" "$TMPD/body"
  page "$B1"
  ok "$B1 vẫn 200" is200
  api "$JAR_A" GET /api/v1/export
  ok "A: GET /export 200, có $A1, không có $B1, $B2, $S1" bash -c '[ "$1" = 200 ] && grep -qF "$3" "$2" && ! grep -qF "$4" "$2" && ! grep -qF "$5" "$2" && ! grep -qF "$6" "$2"' _ "$CODE" "$TMPD/body" "$A1" "$B1" "$B2" "$S1"
  api "$JAR_ADM" GET /api/v1/export
  ok "admin: export có $B1 kèm owner $UB" bash -c 'grep -qF "$2" "$1" && grep -qF "owner: $3" "$1"' _ "$TMPD/body" "$B1" "$UB"

  section "P2: danh sách của A không có đồ của B (B thấy đồ của mình)"
  local p
  for p in sites jobs databases backups trash incidents overview cron php ssl; do
    api "$JAR_A" GET "/api/v1/$p"
    ok "A: GET /$p 200, không có $B1, $B2, $UB, $S1" bash -c '[ "$1" = 200 ] && ! grep -qF "$3" "$2" && ! grep -qF "$4" "$2" && ! grep -qF "$5" "$2" && ! grep -qF "$6" "$2"' _ "$CODE" "$TMPD/body" "$B1" "$B2" "$UB" "$S1"
  done
  api "$JAR_A" GET /api/v1/sites
  ok "A: GET /sites có $A1 và $A2" contains "$A1" "$A2"

  section "P2: phép kiểm cô lập (chạy cho mọi cặp site) không lộ tên site của người khác"
  api "$JAR_A" GET "/api/v1/sites/$A1"
  ok "A: GET /sites/$A1 (phép kiểm chạy lúc gọi): không có $B1, $S1; vẫn có isolation:$A1:$A2" bash -c '[ "$1" = 200 ] && ! grep -qF "$3" "$2" && ! grep -qF "$4" "$2" && grep -qF "isolation:$5:$6" "$2"' _ "$CODE" "$TMPD/body" "$B1" "$S1" "$A1" "$A2"
  api "$JAR_ADM" GET "/api/v1/sites/$A1"
  ok "admin: GET /sites/$A1 vẫn có phép cô lập với $S1 và $B1" bash -c 'grep -qE "isolation:[a-z0-9.:]*$2" "$1" && grep -qE "isolation:[a-z0-9.:]*$3" "$1"' _ "$TMPD/body" "${S1//./\\.}" "${B1//./\\.}"
  api "$JAR_A" GET "/api/v1/jobs/$JA2ADD"
  ok "A: GET /jobs/$JA2ADD (site.add $A2 của A): sự kiện kiểm chứng không có $S1, vẫn có cô lập $A2 với $A1" bash -c '[ "$1" = 200 ] && ! grep -qF "$3" "$2" && grep -qE "isolation:($4:$5|$5:$4)" "$2"' _ "$CODE" "$TMPD/body" "$S1" "${A2//./\\.}" "${A1//./\\.}"
  api "$JAR_ADM" GET "/api/v1/jobs/$JA2ADD"
  ok "admin: cùng job đó có phép cô lập với $S1" bash -c 'grep -qE "isolation:[a-z0-9.:]*$2" "$1"' _ "$TMPD/body" "${S1//./\\.}"
  run curl -sS -m 10 --cacert "$TMPD/cert.pem" -b "$JAR_A" -o "$TMPD/sse" -w '-> HTTP %{http_code}\n' "$BASE/api/v1/jobs/$JA2ADD/events"
  ok "A: SSE của job đó: 200, có job.done, không có $S1" bash -c 'grep -q "^-> HTTP 200$" <<<"$1" && grep -q "^event: job.done" "$2" && ! grep -qF "$3" "$2"' _ "$OUT" "$TMPD/sse" "$S1"
  api "$JAR_B" GET /api/v1/sites
  ok "B: GET /sites có $B1, không có $A1" bash -c 'grep -qF "$2" "$1" && ! grep -qF "$3" "$1"' _ "$TMPD/body" "$B1" "$A1"
  api "$JAR_B" GET /api/v1/jobs
  ok "B: GET /jobs có job của $B1 (cả job admin chạy bằng CLI --owner)" contains "$B1"
  api "$JAR_B" GET /api/v1/databases
  ok "B: GET /databases có $DB1" contains "$DB1"
  api "$JAR_B" GET /api/v1/backups
  ok "B: GET /backups có $B1" contains "$B1"
  api "$JAR_B" GET /api/v1/trash
  ok "B: GET /trash có $B2 và $DB2" contains "$B2" "$DB2"
  api "$JAR_B" GET /api/v1/incidents
  ok "B: GET /incidents có sự cố của $B1" contains "$B1"

  section "P2: route admin: A nhận 403 forbidden"
  for p in users packages stack machine security updates plans agent ai/brain checks; do
    api "$JAR_A" GET "/api/v1/$p"
    if [ "$p" = checks ]; then
      ok "A: GET /checks (any) 200, results rỗng (phép kiểm của máy chỉ admin)" bash -c '[ "$1" = 200 ] && jq -e ".results == []" "$2" >/dev/null' _ "$CODE" "$TMPD/body"
    else
      ok "A: GET /$p -> 403 forbidden" expect 403 forbidden
    fi
  done
  deny 403 forbidden POST '/api/v1/stack/switch?dry_run=1' '{"to":"apache"}'
  deny 403 forbidden POST "/api/v1/sites/$A1/suspend?dry_run=1"
  deny 403 forbidden POST "/api/v1/dbs/$DA1/suspend?dry_run=1"
  deny 403 forbidden POST /api/v1/users '{"name":"khach_c"}'
  deny 403 forbidden POST /api/v1/packages '{"name":"goi_x"}'
  deny 403 forbidden POST "/api/v1/users/$UB/view-as"

  section "P2: sau mọi lần thử của A: đồ của B nguyên vẹn, không lệnh nào chạy"
  sql "$JOBS_SQL" "$UA" "$B1" "$B2" "$DB1" "$DB2"
  ok "không thêm job nào do A chạy hay trên đồ của B (so với đầu P2)" test "$OUT" = "$J0"
  vroot tpan site list
  ok "$B1 active, $B2 trashed, chủ $UB" bash -c 'grep -qE "^$2 +active .* $4 " <<<"$1" && grep -qE "^$3 +trashed .* $4 " <<<"$1"' _ "$OUT" "$B1" "$B2" "$UB"
  vroot tpan db list --json
  ok "$DB1 active, ghi chú vẫn của B" jout --arg n "$DB1" '[.[] | select(.db == $n and .status == "active" and .note == "cua B")] | length == 1'
  run scripts/faults/04-wpconfig-perm.sh restore "$B1"
  health_json "$B1"
  ok "health $B1 không còn Fail (sự cố của B đóng)" no_fail
  part_end P2
}

# Chụp những thứ một lệnh tạo site hay DB sẽ để lại trên máy.
MACHINE_SCRIPT='
echo "user tp_: $(getent passwd | grep -c "^tp_")"
echo "/var/www: $(ls /var/www | tr "\n" " ")"
echo "DB: $(mariadb -N -B -e "SHOW DATABASES" | grep -vE "^(information_schema|mysql|performance_schema|sys)$" | sort | tr "\n" " ")"
echo "user DB: $(mariadb -N -B -e "SELECT User FROM mysql.global_priv WHERE User LIKE \"tp\\_%\" OR User LIKE \"khach\\_%\"" | sort | tr "\n" " ")"
echo "pool: $(ls /etc/php/*/fpm/pool.d | grep -c tpan-)"
'

part_p3() {
  part_begin "P3: hạn mức gói $PKN (2 site, 1 DB rời)"
  vroot tpan user info "$UA" --json
  ok "$UA đang dùng 2/2 site, 1/1 DB rời" jout '.usage.sites == 2 and .usage.dbs == 1 and .limits.max_sites == 2 and .limits.max_dbs == 1'
  vroot bash -c "$MACHINE_SCRIPT"
  local M0="$OUT"
  sql "$JOBS_SQL" "$UA" "$A3" "$DA2" "$A3" "$DA2"
  local J0="$OUT"
  vroot tpan site add "$A3" --email "$EMAIL" --owner "$UA" --yes --json
  ok "CLI: site thứ 3 của $UA bị từ chối quota_sites" refused quota_sites
  vroot tpan site add "$A3" --email "$EMAIL" --owner "$UA" --yes
  ok "lời từ chối dạng chữ có 'Từ chối'" hasF 'Từ chối'
  api "$JAR_A" POST /api/v1/sites "$(jq -nc --arg d "$A3" --arg e a@example.com '{domain: $d, email: $e}')"
  ok "API (A): POST /sites site thứ 3: quota_sites (4xx)" bash -c '[ "${1:0:1}" = 4 ] && [ "$2" = quota_sites ]' _ "$CODE" "$(ecode)"
  vroot tpan db add d2 --owner "$UA" --yes --json
  ok "CLI: DB rời thứ 2 của $UA bị từ chối quota_dbs" refused quota_dbs
  api "$JAR_A" POST /api/v1/dbs '{"name":"d2"}'
  ok "API (A): POST /dbs thứ 2: quota_dbs (4xx)" bash -c '[ "${1:0:1}" = 4 ] && [ "$2" = quota_dbs ]' _ "$CODE" "$(ecode)"
  vroot bash -c "$MACHINE_SCRIPT"
  ok "máy không đổi: user tp_, /var/www, DB, user DB, pool như trước" test "$OUT" = "$M0"
  sql "$JOBS_SQL" "$UA" "$A3" "$DA2" "$A3" "$DA2"
  ok "không job nào cho $A3, $DA2 hay do A chạy, không sự kiện command mới" test "$OUT" = "$J0"

  section "P3: nâng gói lên 2 DB rời, A có DB rời thứ hai (để P6 có DB khoá theo user)"
  vroot tpan package edit "$PKN" --max-dbs 2 --yes
  ok "package edit $PKN --max-dbs 2" rc_is 0
  json_secret "$DA2" '.db_password' "$VM" root tpan db add d2 --owner "$UA" --yes --json
  ok "db add d2 --owner $UA giờ đạt ($DA2)" bash -c '[ "$1" = 0 ] && [ -n "$2" ]' _ "$RC" "$JS_PW"
  part_end P3
}

# site_state <domain>: đọc slug, bản PHP, socket bằng site info --json rồi in
# pool, socket, cron, vhost của site. Kết quả trong OUT.
site_state() {
  local d=$1 info slug v sock
  info="$("$VM" root tpan site info "$d" --json </dev/null | tr -d '\r')"
  slug="$(jq -r .layout.slug <<<"$info")"
  v="$(jq -r .site.php_version <<<"$info")"
  sock="$(jq -r .socket <<<"$info")"
  printf '(%s: slug %s, PHP %s, socket %s)\n' "$d" "$slug" "$v" "$sock"
  vroot bash -c "$SITE_FILES_SCRIPT" _ "$slug" "$v" "$d" "$sock"
  SLUG_OF_STATE="$slug"
}
st_suspended() {
  has "^pool: tpan-$SLUG_OF_STATE\.conf\.suspended$" && ! has "^pool: tpan-$SLUG_OF_STATE\.conf$" && has '^socket: không có$' &&
    has "^cron: tpan-$SLUG_OF_STATE\.suspended$" && ! has "^cron: tpan-$SLUG_OF_STATE$" && has '^php-fpm -t mã 0$' && has 'fpm: active$'
}
st_active() {
  has "^pool: tpan-$SLUG_OF_STATE\.conf$" && ! has 'conf\.suspended' && has '^socket: có$' &&
    has "^cron: tpan-$SLUG_OF_STATE$" && ! has 'suspended$' && has '^php-fpm -t mã 0$'
}

# suspended_web <domain> <ssl 0|1>: mọi đường dẫn 403 kèm trang tạm khoá.
suspended_web() {
  local d=$1 ssl=$2
  page "$d"
  ok "http://$d/ -> 403 kèm trang tạm khoá" is403
  page "$d" http /wp-login.php
  ok "http://$d/wp-login.php -> 403 kèm trang tạm khoá" is403
  if [ "$ssl" = 1 ]; then
    page "$d" https
    ok "https://$d/ -> 403 kèm trang tạm khoá" is403
    page "$d" https /wp-admin/
    ok "https://$d/wp-admin/ -> 403 kèm trang tạm khoá" is403
    vroot bash -c 'f="$2/.well-known/acme-challenge/e2e-m13"; mkdir -p "${f%/*}"; echo e2e-m13-thu >"$f"; curl -sS -m 10 --resolve "$1:80:127.0.0.1" -w "\nHTTP %{http_code}\n" "http://$1/.well-known/acme-challenge/e2e-m13"; rm -f "$f"' _ "$d" "$ACME_ROOT"
    ok "http://$d/.well-known/acme-challenge/ vẫn đi qua (200, đúng nội dung)" bash -c 'grep -qx "e2e-m13-thu" <<<"$1" && grep -qx "HTTP 200" <<<"$1"' _ "$OUT"
  fi
}

T_A2=""
part_p4() {
  part_begin "P4: tạm khoá site trên nginx ($S1 có SSL, $A2 không), rồi chuyển apache và về"
  site_state "$S1"
  ok "$S1 trước khi khoá: pool, socket, cron đang bật" st_active
  vroot tpan site suspend "$S1" --dry-run
  vroot tpan site suspend "$S1" --yes
  ok "site suspend $S1: job thành công" job_ok
  vroot python3 -c "$JOB_PY" site.suspend
  suspended_web "$S1" 1
  site_state "$S1"
  ok "$S1: pool .conf.suspended, socket biến mất, cron .suspended, php-fpm -t đạt" st_suspended
  ok "$S1: vhost nginx có trang tạm khoá và vẫn có 443" has "sites-available/tpan-$S1.conf: dấu trang tạm khoá [1-9][0-9]* dòng, 443: [1-9]"
  vroot tpan site suspend "$A2" --yes
  ok "site suspend $A2: job thành công" job_ok
  T_A2="$(vm_now)"
  suspended_web "$A2" 0
  site_state "$A2"
  ok "$A2: pool .conf.suspended, socket biến mất, cron .suspended, php-fpm -t đạt" st_suspended
  vroot nginx -t
  ok "nginx -t đạt với hai vhost tạm khoá (80 và 80+443)" rc_is 0
  vroot certbot renew --dry-run --no-random-sleep-on-renew --cert-name "$S1" --server "$ACME"
  ok "certbot renew --dry-run cho $S1 đang khoá (nginx) đạt" bash -c '[ "$2" = 0 ] && grep -q "Congratulations, all simulated renewals succeeded" <<<"$1"' _ "$OUT" "$RC"
  health_json "$S1"
  ok "health $S1: http.suspended:$S1 đạt" check_status "http.suspended:$S1" pass
  ok "health $S1: chặng php, wordpress, db xám (unknown + tạm khoá)" bash -c 'for s in php wordpress db; do jq -e --arg d "$1" --arg s "$s" "[.groups[] | select(.target == \$d) | .results[] | select(.stage == \$s)] as \$r | (\$r | length) > 0 and all(\$r[]; .status == \"unknown\" and .suspended == true)" "$2" >/dev/null || exit 1; done' _ "$S1" "$HJ"
  ok "health $S1: không Fail, không mở sự cố nào" bash -c 'jq -e "([.groups[].results[] | select(.status == \"fail\")] | length == 0) and ((.changes.opened // []) | length == 0)" "$1" >/dev/null' _ "$HJ"
  page "$A1"
  ok "$A1 vẫn 200" is200
  page "$B1"
  ok "$B1 vẫn 200" is200
  vroot tpan site suspend "$S1" --yes --json
  ok "khoá lần hai: từ chối site_suspended (đã khoá rồi)" refused site_suspended

  section "P4: chuyển sang apache khi $S1 và $A2 đang khoá"
  vroot tpan stack switch --to apache --yes
  ok "stack switch --to apache: job thành công" job_ok
  suspended_web "$S1" 1
  suspended_web "$A2" 0
  page "$A1"
  ok "apache: $A1 200" is200
  page "$B1"
  ok "apache: $B1 200" is200
  vroot apachectl configtest
  ok "apachectl configtest đạt với hai vhost tạm khoá (80 và 80+443)" hasF 'Syntax OK'
  site_state "$S1"
  ok "apache: vhost của $S1 có trang tạm khoá và có 443" has "apache2/sites-available/tpan-$S1.conf: dấu trang tạm khoá [1-9][0-9]* dòng, 443: [1-9]"
  ok "apache: pool của $S1 vẫn .suspended" st_suspended
  vroot certbot renew --dry-run --no-random-sleep-on-renew --cert-name "$S1" --server "$ACME"
  ok "certbot renew --dry-run cho $S1 đang khoá (apache) đạt" bash -c '[ "$2" = 0 ] && grep -q "Congratulations, all simulated renewals succeeded" <<<"$1"' _ "$OUT" "$RC"
  vroot tpan site unsuspend "$S1" --yes
  ok "apache: site unsuspend $S1: job thành công" job_ok
  page "$S1" https
  ok "apache: https://$S1 200 sau mở khoá" is200
  vroot tpan site suspend "$S1" --yes
  ok "apache: site suspend $S1 lần nữa: job thành công" job_ok
  page "$S1" https
  ok "apache: https://$S1 403 kèm trang" is403
  vroot apachectl configtest
  ok "apachectl configtest đạt" hasF 'Syntax OK'

  section "P4: về nginx"
  vroot tpan stack switch --to nginx --yes
  ok "stack switch --to nginx: job thành công" job_ok
  suspended_web "$S1" 1
  suspended_web "$A2" 0
  page "$A1"
  ok "nginx: $A1 200" is200
  vroot nginx -t
  ok "nginx -t đạt" rc_is 0

  section "P4: cron không chạy file .suspended ($A2 khoá liên tục từ $T_A2)"
  local now el
  now="$(vm_now)"
  el=$(($(date -j -f '%Y-%m-%d %H:%M:%S' "$now" +%s) - $(date -j -f '%Y-%m-%d %H:%M:%S' "$T_A2" +%s)))
  if [ "$el" -lt 330 ]; then
    printf 'Mới %s giây kể từ khi khoá %s; chờ thêm %s giây để qua ít nhất một lượt */5 của cron.\n' "$el" "$A2" "$((330 - el))"
    sleep $((330 - el))
  fi
  vroot bash -c 'journalctl -u cron --no-pager -o short-iso --since "$1" | grep -E "CMD .*tpan cron run" | tail -8; echo "lần chạy $2: $(journalctl -u cron --no-pager --since "$1" | grep -cF "tpan cron run $2")"; echo "lần chạy $3: $(journalctl -u cron --no-pager --since "$1" | grep -cF "tpan cron run $3")"' _ "$T_A2" "$A2" "$A1"
  ok "từ lúc khoá: cron không chạy tpan cron run $A2, nhưng vẫn chạy cho $A1" bash -c 'grep -q "^lần chạy $2: 0$" <<<"$1" && grep -qE "^lần chạy $3: [1-9]" <<<"$1"' _ "$OUT" "$A2" "$A1"

  section "P4: mở khoá $S1 trên nginx"
  vroot tpan site unsuspend "$S1" --yes
  ok "site unsuspend $S1: job thành công" job_ok
  page "$S1" https
  ok "https://$S1 200" is200
  vexec curl -sS -m 10 --resolve "$S1:80:127.0.0.1" -o /dev/null -w '%{http_code} %{redirect_url}\n' "http://$S1/"
  ok "http://$S1 301 sang https" has "^301 https://$S1/"
  site_state "$S1"
  ok "$S1: pool .conf, socket, cron trở lại" st_active
  vroot nginx -t
  ok "nginx -t đạt" rc_is 0
  part_end P4
}

part_p5() {
  part_begin "P5: tạm khoá DB rời $DA1 (cắt kết nối), DB của site $S1, COLLATIONS, du -sb"
  db_cnf "$DA1"
  db_login "$DA1"
  ok "$DA1 đăng nhập được trước khi khoá" login_ok "$DA1"
  locked_of "$DA1"
  vroot systemd-run --unit=e2e-m13-sock mariadb --defaults-extra-file="/root/e2e-$DA1.cnf" -N -e 'SELECT SLEEP(120)'
  vroot systemd-run --unit=e2e-m13-tcp mariadb --defaults-extra-file="/root/e2e-$DA1.cnf" --protocol=TCP -h 127.0.0.1 -N -e 'SELECT SLEEP(120)'
  sleep 3
  vroot mariadb -N -B -e "SELECT ID, USER, HOST, COMMAND, LEFT(INFO, 30) FROM information_schema.PROCESSLIST WHERE USER = '$DA1' ORDER BY ID"
  ok "(mục 16 M13) hai kết nối đang mở của $DA1: một qua socket (HOST localhost), một qua TCP (HOST có cổng)" bash -c '[ "$(grep -c . <<<"$1")" = 2 ] && [ "$(awk -F"\t" "\$3 == \"localhost\"" <<<"$1" | wc -l)" -eq 1 ] && [ "$(awk -F"\t" "\$3 ~ /^(localhost|127[.]0[.]0[.]1):[0-9]+\$/" <<<"$1" | wc -l)" -eq 1 ]' _ "$OUT"
  vroot tpan db suspend "$DA1" --yes
  ok "db suspend $DA1: job thành công" job_ok
  vroot python3 -c "$JOB_PY" dbs.suspend
  sleep 1
  vroot mariadb -N -B -e "SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE USER = '$DA1'"
  ok "không còn kết nối nào của $DA1 (KILL bắt cả kết nối TCP)" out_is 0
  vroot bash -c 'for u in e2e-m13-sock e2e-m13-tcp; do systemctl show -p ActiveState -p Result -p ExecMainStatus "$u" | tr "\n" " "; echo; journalctl -u "$u" --no-pager -o cat | grep -iE "error|lost|kill" | tail -2; done'
  ok "hai lệnh SELECT SLEEP(120) bị cắt giữa chừng (thoát lỗi, chưa đủ 120 giây)" bash -c '[ "$(grep -c "ExecMainStatus=[1-9]" <<<"$1")" = 2 ]' _ "$OUT"
  db_login "$DA1"
  ok "đăng nhập bằng user của $DA1 bị từ chối: ERROR 4151 account is locked" login_locked
  locked_of "$DA1"
  ok "mysql.global_priv: account_locked = 1" has "^$DA1	1$"
  json_secret "$DA1" '.db_password' "$VM" root tpan db passwd "$DA1" --yes --json
  ok "db passwd $DA1 khi đang khoá: thoát 0, mật khẩu mới (đã che)" bash -c '[ "$1" = 0 ] && [ -n "$2" ]' _ "$RC" "$JS_PW"
  db_cnf "$DA1"
  db_login "$DA1"
  ok "(mục 16 M13) ALTER USER ... IDENTIFIED BY giữ ACCOUNT LOCK: mật khẩu mới vẫn bị 4151" login_locked
  locked_of "$DA1"
  ok "account_locked vẫn 1" has "^$DA1	1$"
  vroot tpan db list --json
  ok "db list: $DA1 suspended self" jout --arg n "$DA1" '[.[] | select(.db == $n and .suspended == "self")] | length == 1'

  section "P5: khoá DB của site $S1"
  local U1
  U1="$(user_of "$S1")"
  vroot tpan db suspend "$S1" --yes
  ok "db suspend $S1 (DB của site): job thành công" job_ok
  locked_of "$U1"
  ok "user DB của $S1 bị khoá" has "^$U1	1$"
  health_json "$S1"
  ok "health $S1: chặng db là unknown + suspended" stage_grey "$S1" db
  ok "health $S1: không mở sự cố nào, không Fail" bash -c 'jq -e "([.groups[].results[] | select(.status == \"fail\")] | length == 0) and ((.changes.opened // []) | length == 0)" "$1" >/dev/null' _ "$HJ"
  api "$JAR_ADM" GET /api/v1/incidents
  ok "GET /incidents: không sự cố nào đang mở cho $S1" bash -c '! jq -e --arg d "$2" "[.. | objects | select(.target? == \$d and ((.closed? // \"\") == \"\"))] | length > 0" "$1" >/dev/null' _ "$TMPD/body" "$S1"
  page "$S1" https
  printf '(trang của site khi DB của nó bị khoá, ghi để biết)\n'
  vroot tpan db unsuspend "$S1" --yes
  ok "db unsuspend $S1: job thành công" job_ok
  health_json "$S1"
  ok "health $S1: chặng db đạt, không Fail" bash -c 'jq -e --arg d "$2" "[.groups[] | select(.target == \$d) | .results[] | select(.stage == \"db\")] as \$r | (\$r | length) > 0 and all(\$r[]; .status == \"pass\")" "$1" >/dev/null && jq -e "[.groups[].results[] | select(.status == \"fail\")] | length == 0" "$1" >/dev/null' _ "$HJ" "$S1"
  page "$S1" https
  ok "https://$S1 200" is200

  section "P5: (mục 16 M13) information_schema.COLLATIONS của MariaDB trên máy và tpan db charset"
  vroot mariadb -N -B -e "SELECT VERSION(); SELECT COUNT(*), SUM(CHARACTER_SET_NAME IS NULL), SUM(CHARACTER_SET_NAME IS NOT NULL) FROM information_schema.COLLATIONS; SELECT CHARACTER_SET_NAME, COLLATION_NAME FROM information_schema.COLLATIONS WHERE IS_DEFAULT = 'Yes' AND CHARACTER_SET_NAME IN ('utf8mb4', 'latin1'); SELECT COUNT(*) FROM information_schema.COLLATION_CHARACTER_SET_APPLICABILITY WHERE COLLATION_NAME LIKE 'utf8mb4_uca1400%'"
  local NN
  NN="$(sed -n 2p <<<"$OUT" | awk -F'\t' '{print $3}')"
  api "$JAR_ADM" GET /api/v1/collations
  ok "GET /collations có đúng số cặp có charset ($NN) của information_schema.COLLATIONS" bash -c '[ "$1" = 200 ] && [ "$(jq length "$2")" = "$3" ]' _ "$CODE" "$TMPD/body" "$NN"
  vroot tpan db charset "$DA2" --charset utf8mb4 --collation utf8mb4_unicode_520_ci --yes
  ok "db charset $DA2 utf8mb4 / utf8mb4_unicode_520_ci: job thành công" job_ok
  vroot mariadb -N -B -e "SELECT DEFAULT_CHARACTER_SET_NAME, DEFAULT_COLLATION_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = '$DA2'"
  ok "SCHEMATA: $DA2 là utf8mb4 / utf8mb4_unicode_520_ci" out_is "$(printf 'utf8mb4\tutf8mb4_unicode_520_ci')"
  vroot tpan db charset "$DA2" --charset latin1 --collation utf8mb4_general_ci --yes --json
  ok "cặp không khớp (latin1 / utf8mb4_general_ci) bị từ chối collation_invalid" refused collation_invalid
  vroot tpan db charset "$DA2" --collation utf8mb4_uca1400_ai_ci --yes --json
  printf '(ghi để biết: collation uca1400 của 10.10+ không có charset trong COLLATIONS, xem kết quả trên)\n'
  vroot tpan db list
  vroot tpan db list --json
  ok "db list --json: $DA2 charset utf8mb4, collation utf8mb4_unicode_520_ci" jout --arg n "$DA2" '[.[] | select(.db == $n and .charset == "utf8mb4" and .collation == "utf8mb4_unicode_520_ci")] | length == 1'

  section "P5: (mục 16 M13) dung lượng: du -sb so với số tpan đo cho $UA"
  health_json
  vroot tpan user info "$UA" --json
  local DISK
  DISK="$(jq -r '.usage.disk_bytes' <<<"$OUT")"
  vroot bash -c 'du -sb --one-file-system "/var/www/$1" "/var/www/$2"; echo "tổng du: $(du -sbc --one-file-system "/var/www/$1" "/var/www/$2" | tail -1 | cut -f1)"; echo "tổng DB: $(mariadb -N -B -e "SELECT COALESCE(SUM(DATA_LENGTH + INDEX_LENGTH), 0) FROM information_schema.TABLES WHERE TABLE_SCHEMA IN (\"$3\", \"$4\", \"$5\", \"$6\")")"' _ "$A1" "$A2" "$(user_of "$A1")" "$(user_of "$A2")" "$DA1" "$DA2"
  local DU DBS
  DU="$(sed -nE 's/^tổng du: ([0-9]+)$/\1/p' <<<"$OUT")"
  DBS="$(sed -nE 's/^tổng DB: ([0-9]+)$/\1/p' <<<"$OUT")"
  printf 'tpan đo: %s byte; du -sb hai thư mục site: %s; DB (information_schema): %s; chênh: %s byte\n' "$DISK" "$DU" "$DBS" "$((DISK - DU - DBS))"
  ok "số tpan đo khớp du -sb + dung lượng DB (lệch dưới 2 MiB)" bash -c 'd=$(($1 - $2 - $3)); [ "$1" -gt 0 ] && [ "${d#-}" -lt 2097152 ]' _ "${DISK:-0}" "${DU:-0}" "${DBS:-0}"
  part_end P5
}

part_p6() {
  part_begin "P6: tạm khoá user $UA (có $A2 và $DA1 đã khoá riêng từ trước)"
  vroot tpan site list --json
  ok "trước: $A1 không khoá, $A2 khoá self" jout --arg a1 "$A1" --arg a2 "$A2" '(map({(.domain): (.suspended // "")}) | add) as $s | $s[$a1] == "" and $s[$a2] == "self"'
  vroot tpan db list --json
  ok "trước: $DA1 khoá self, $DA2 không khoá" jout --arg d1 "$DA1" --arg d2 "$DA2" '(map({(.db): (.suspended // "")}) | add) as $s | $s[$d1] == "self" and $s[$d2] == ""'
  db_cnf "$DA2"
  db_login "$DA2"
  ok "$DA2 đăng nhập được" login_ok "$DA2"
  JAR_A="$(new_jar a)"
  login "$JAR_A" "$UA" "$TMPD/pw.a"
  ok "A đăng nhập: 204 (giữ cookie này)" code_is 204
  api "$JAR_A" GET /api/v1/sites
  ok "cookie của A dùng được: 200" code_is 200

  vroot tpan user suspend "$UA" --reason "e2e: chua thanh toan" --yes
  ok "user suspend $UA: job thành công" job_ok
  vroot python3 -c "$JOB_PY" user.suspend
  api "$JAR_A" GET /api/v1/sites
  ok "cookie cũ của A: 401" expect 401 unauthorized
  local JA2
  JA2="$(new_jar a2)"
  login "$JA2" "$UA" "$TMPD/pw.a"
  ok "A đăng nhập lại (đúng mật khẩu): 403 user_suspended" expect 403 user_suspended
  page "$A1"
  ok "$A1 403 kèm trang tạm khoá" is403
  page "$A2"
  ok "$A2 403 kèm trang tạm khoá" is403
  db_login "$DA2"
  ok "$DA2 bị khoá theo user: 4151" login_locked
  db_login "$DA1"
  ok "$DA1 vẫn khoá: 4151" login_locked
  vroot tpan site list --json
  ok "$A1 khoá theo user ('user'), $A2 vẫn 'self'" jout --arg a1 "$A1" --arg a2 "$A2" '(map({(.domain): (.suspended // "")}) | add) as $s | $s[$a1] == "user" and $s[$a2] == "self"'
  vroot tpan db list --json
  ok "$DA2 'user', $DA1 vẫn 'self'" jout --arg d1 "$DA1" --arg d2 "$DA2" '(map({(.db): (.suspended // "")}) | add) as $s | $s[$d1] == "self" and $s[$d2] == "user"'
  page "$B1"
  ok "$B1 (của B) vẫn 200" is200
  page "$S1" https
  ok "$S1 (của admin) vẫn 200" is200
  vroot tpan user info "$UA"

  section "P6: xem như $UA (đang bị khoá)"
  api "$JAR_ADM" POST "/api/v1/users/$UA/view-as"
  ok "admin: view-as $UA (đang khoá): 200, user $UA, viewed_by admin" bash -c '[ "$1" = 200 ] && jq -e --arg u "$3" ".user.name == \$u and .user.viewed_by == \"admin\"" "$2" >/dev/null' _ "$CODE" "$TMPD/body" "$UA"
  api "$JAR_ADM" GET /api/v1/session
  ok "phiên của admin không bị đăng xuất: logged_in, đang xem như $UA" bash -c '[ "$1" = 200 ] && jq -e --arg u "$3" ".logged_in and .user.name == \$u and .user.viewed_by == \"admin\"" "$2" >/dev/null' _ "$CODE" "$TMPD/body" "$UA"
  api "$JAR_ADM" GET /api/v1/sites
  ok "xem như: GET /sites chỉ có $A1, $A2" bash -c '[ "$1" = 200 ] && grep -qF "$3" "$2" && grep -qF "$4" "$2" && ! grep -qF "$5" "$2" && ! grep -qF "$6" "$2"' _ "$CODE" "$TMPD/body" "$A1" "$A2" "$B1" "$S1"
  api "$JAR_ADM" GET /api/v1/databases
  ok "xem như: GET /databases không có DB của B" lacks "$DB1" "$UB"
  api "$JAR_ADM" GET /api/v1/users
  ok "xem như: GET /users 403 forbidden (vai user)" expect 403 forbidden
  api "$JAR_ADM" POST "/api/v1/sites/$A1/backups"
  ok "xem như: POST 403 read_only_view" expect 403 read_only_view
  api "$JAR_ADM" POST /api/v1/dbs '{"name":"x"}'
  ok "xem như: POST /dbs 403 read_only_view" expect 403 read_only_view
  api "$JAR_ADM" POST /api/v1/session/view-as/exit
  ok "thoát xem như: 200, về admin" bash -c '[ "$1" = 200 ] && jq -e ".user.name == \"admin\" and (.user.viewed_by // \"\") == \"\"" "$2" >/dev/null' _ "$CODE" "$TMPD/body"
  api "$JAR_ADM" GET /api/v1/users
  ok "về admin: GET /users 200" code_is 200

  section "P6: mở khoá $UA"
  vroot tpan user unsuspend "$UA" --yes
  ok "user unsuspend $UA: job thành công" job_ok
  page "$A1"
  ok "$A1 về 200" is200
  page "$A2"
  ok "$A2 (khoá riêng từ trước) VẪN 403 kèm trang" is403
  db_login "$DA2"
  ok "$DA2 đăng nhập lại được" login_ok "$DA2"
  db_login "$DA1"
  ok "$DA1 (khoá riêng từ trước) VẪN khoá: 4151" login_locked
  vroot tpan site list --json
  ok "$A1 không khoá, $A2 vẫn 'self'" jout --arg a1 "$A1" --arg a2 "$A2" '(map({(.domain): (.suspended // "")}) | add) as $s | $s[$a1] == "" and $s[$a2] == "self"'
  vroot tpan db list --json
  ok "$DA2 không khoá, $DA1 vẫn 'self'" jout --arg d1 "$DA1" --arg d2 "$DA2" '(map({(.db): (.suspended // "")}) | add) as $s | $s[$d1] == "self" and $s[$d2] == ""'
  login "$JA2" "$UA" "$TMPD/pw.a"
  ok "A đăng nhập lại được: 204" code_is 204
  api "$JA2" GET /api/v1/sites
  ok "A: GET /sites 200, có $A1, $A2" bash -c '[ "$1" = 200 ] && grep -qF "$3" "$2" && grep -qF "$4" "$2"' _ "$CODE" "$TMPD/body" "$A1" "$A2"
  JAR_A="$JA2"

  section "P6: không khoá được chính mình và admin cuối cùng"
  api "$JAR_ADM" POST '/api/v1/users/admin/suspend?dry_run=1'
  ok "admin tự khoá mình qua API: từ chối last_admin" bash -c '[ "${1:0:1}" = 4 ] && [ "$2" = last_admin ]' _ "$CODE" "$(ecode)"
  vroot tpan user suspend admin --yes --json
  ok "CLI: khoá admin (admin cuối cùng): từ chối last_admin" refused last_admin
  vroot tpan user list
  ok "admin vẫn hoạt động" has '^admin .*hoạt động$'

  section "P6: thứ tự ngược: admin đang xem như $UB thì $UB bị khoá (bằng CLI)"
  api "$JAR_ADM" POST "/api/v1/users/$UB/view-as"
  ok "admin: view-as $UB: 200" code_is 200
  vroot tpan user suspend "$UB" --yes
  ok "user suspend $UB: job thành công" job_ok
  api "$JAR_ADM" GET /api/v1/session
  ok "phiên của admin không bị đăng xuất khi user đang được xem bị khoá" bash -c '[ "$1" = 200 ] && jq -e ".logged_in" "$2" >/dev/null' _ "$CODE" "$TMPD/body"
  api "$JAR_ADM" GET /api/v1/users
  ok "admin vẫn thao tác được: GET /users 200" code_is 200
  api "$JAR_ADM" POST /api/v1/session/view-as/exit
  vroot tpan user unsuspend "$UB" --yes
  ok "user unsuspend $UB: job thành công" job_ok
  page "$B1"
  ok "$B1 200 lại" is200
  login "$JAR_B" "$UB" "$TMPD/pw.b"
  ok "B đăng nhập lại: 204" code_is 204
  part_end P6
}

part_p7() {
  part_begin "P7: xoá user $UB"
  vroot tpan user rm "$UB" --yes --json
  ok "user rm $UB khi còn site, DB rời: từ chối user_not_empty" refused user_not_empty
  vroot tpan user trash-all "$UB" --yes
  ok "user trash-all $UB: thoát 0" rc_is 0
  vroot tpan trash list
  vroot tpan user rm "$UB" --yes --json
  ok "user rm sau trash-all: vẫn user_not_empty (thùng rác vẫn tính, mục 6.12 'kể cả trong thùng rác')" refused user_not_empty
  vroot tpan trash purge "$B1" --yes
  ok "trash purge $B1" job_ok
  vroot tpan trash purge "$B2" --yes
  ok "trash purge $B2" job_ok
  vroot tpan db purge "$DB1" --yes
  ok "db purge $DB1" job_ok
  vroot tpan db purge "$DB2" --yes
  ok "db purge $DB2" job_ok
  vroot tpan user rm "$UB" --yes
  ok "user rm $UB: thoát 0" rc_is 0
  vroot tpan user list
  ok "user list không còn $UB" hasnt "^$UB "
  api "$JAR_B" GET /api/v1/sites
  ok "phiên của B (đã xoá): 401" expect 401 unauthorized
  sql "SELECT 'job', COUNT(*) FROM jobs WHERE owner = ? OR actor = ? UNION ALL SELECT 'audit', COUNT(*) FROM audit WHERE actor = ?" "$UB" "$UB" "$UB"
  ok "job và nhật ký cũ của $UB vẫn giữ tên" bash -c 'grep -qE "^job	[1-9]" <<<"$1" && grep -qE "^audit	[1-9]" <<<"$1"' _ "$OUT"
  vroot bash -c "$MACHINE_SCRIPT"
  ok "trên máy không còn site, DB, user DB của $UB" bash -c '! grep -qF "$2" <<<"$1" && ! grep -qF "$3" <<<"$1" && ! grep -qF "$4" <<<"$1"' _ "$OUT" "$B1" "$B2" "$UB"
  part_end P7
}

# Tìm mọi bí mật đã gặp (đọc từ stdin, mỗi dòng một cái) trong tpan.db, WAL,
# SHM, job_events và journal. Chỉ in số đếm.
SCAN_SCRIPT='
n=0; total=0
while IFS= read -r s; do
  [ -n "$s" ] || continue
  n=$((n + 1))
  for f in /var/lib/tpan/tpan.db /var/lib/tpan/tpan.db-wal /var/lib/tpan/tpan.db-shm; do
    [ -e "$f" ] && total=$((total + $(grep -caF -- "$s" "$f")))
  done
  j=$(python3 -c "import sqlite3,sys; c=sqlite3.connect(\"file:/var/lib/tpan/tpan.db?mode=ro\", uri=True); print(c.execute(\"SELECT COUNT(*) FROM job_events WHERE instr(message, ?) > 0\", (sys.argv[1],)).fetchone()[0])" "$s")
  total=$((total + j))
  total=$((total + $(journalctl --no-pager -o cat | grep -cF -- "$s")))
done
ls -la /var/lib/tpan/ | grep tpan.db
echo "số bí mật đã tìm: $n"
echo "số lần gặp trong tpan.db, WAL, SHM, job_events, journal: $total"
'

part_p8() {
  part_begin "P8: mật khẩu tpan sinh ra không nằm trong tpan.db, WAL, job_events, journal"
  run_in "$SECRETS" "$VM" root bash -c "$SCAN_SCRIPT"
  ok "đã tìm đủ mọi bí mật (user, DB rời, WordPress, DB của site, panel, cookie)" bash -c '[ "$(sed -nE "s/^số bí mật đã tìm: ([0-9]+)$/\1/p" <<<"$1")" = "$2" ] && [ "$2" -ge 10 ]' _ "$OUT" "$(grep -c . "$SECRETS")"
  ok "không lần nào" has '^số lần gặp trong tpan.db, WAL, SHM, job_events, journal: 0$'
  part_end P8
}

leak_check() {
  [ -n "${E2E_LOG:-}" ] && [ -f "$E2E_LOG" ] || return 0
  section "bí mật, IP, tên user Mac, cookie có lọt vào chính file log không"
  local n=0 s
  while IFS= read -r s; do
    [ -n "$s" ] || continue
    n=$((n + $(grep -cF -- "$s" "$E2E_LOG")))
  done <"$SECRETS"
  printf 'Số bí mật đã gặp: %s; số dòng của log chứa một trong số đó: %s\n' "$(grep -c . "$SECRETS")" "$n"
  ok "log không chứa mật khẩu hay cookie nào" test "$n" -eq 0
  # Lưới thứ hai cho bí mật chưa từng vào danh sách che (đọc hỏng): mọi trường
  # "password"/"db_password"/"admin_password" trong log phải là chữ đã che.
  ok "mọi trường password trong log đều đã che" bash -c '! grep -E "\"(db_|admin_)?password\": *\"[^<\"]" "$1" | grep -vq "có (đã che)"' _ "$E2E_LOG"
  ok "log không chứa IP máy ảo (IPv4)" bash -c '[ -z "$1" ] || ! grep -qF -- "$1" "$2"' _ "$VM_IP" "$E2E_LOG"
  ok "log không chứa mạng Multipass" bash -c '[ -z "$1" ] || ! grep -qF -- "$1" "$2"' _ "$NET4" "$E2E_LOG"
  ok "log không chứa IPv6 của máy ảo" bash -c '[ -z "$1" ] || ! grep -qiF -- "$1" "$2"' _ "$V6PFX" "$E2E_LOG"
  ok "log không chứa IP của Mac" bash -c '[ -z "$1" ] || ! grep -qF -- "$1" "$2"' _ "$HOST_IP" "$E2E_LOG"
  ok "log không chứa thư mục nhà hay tên user Mac" bash -c '! grep -qF -- "$1" "$3" && ! grep -qF -- "$2" "$3"' _ "$HOME" "$USER" "$E2E_LOG"
}

run_upgrade() {
  part_u0
  part_u1
  part_u2
  part_u3
  part_u4
  part_u5
}
run_main() {
  part_p0
  part_p1
  part_p2
  part_p3
  part_p4
  part_p5
  part_p6
  part_p7
  part_p8
}

printf 'tpan của cây: %s; bản cũ: %s; máy ảo: %s\n' "$VER" "$OLD_VER" "$VMNAME"
case "${1:-}" in
  nang-cap) run_upgrade ;;
  chinh) run_main ;;
  all)
    run_upgrade
    run_main
    ;;
  tu)
    # tu <phần> [phần cuối]: chạy tiếp từ một phần (p1..p8, u1..u5) trên máy
    # ảo đang dở, dùng trạng thái giữ ở E2E_TMP.
    [ -n "${E2E_TMP:-}" ] || { echo "tu cần E2E_TMP" >&2; exit 2; }
    # shellcheck disable=SC1091
    [ -f "$TMPD/state" ] && source "$TMPD/state"
    addrs
    go=0
    for p in u0 u1 u2 u3 u4 u5 p0 p1 p2 p3 p4 p5 p6 p7 p8; do
      [ "$p" = "$2" ] && go=1
      [ "$go" = 1 ] && "part_$p"
      [ "$p" = "${3:-}" ] && break
    done
    ;;
  *)
    sed -n '2,9p' "$0" | sed 's/^# \{0,1\}//'
    exit 2
    ;;
esac

leak_check
section "TỔNG KẾT: $PASSES khẳng định đạt, $FAILS khẳng định không đạt"
[ "$FAILS" -eq 0 ]
