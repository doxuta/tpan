#!/usr/bin/env bash
# e2e của mốc M4 (health đầy đủ) trên máy ảo của mốc (tên trong .tpan-vm, không
# có thì tpan-test): bước 9 và 11 ở mục 12 của docs/KE-HOACH.md, máy mới khoẻ
# (không lỗi, không lệch giả), log trực tiếp và số request, và các điều chưa
# chắc của M4 (đo thật trong máy ảo).
# Chạy trên Mac, trong thư mục repo:
#
#   scripts/e2e-m4.sh all       nginx (A-E, đo) rồi apache (F), mỗi bên từ máy sạch (mặc định)
#   scripts/e2e-m4.sh nginx     chỉ nửa nginx
#   scripts/e2e-m4.sh apache    chỉ nửa apache
#
# Các phần:
#   A  máy sạch, stack nginx, tpan setup, 2 site
#   B  máy mới khoẻ: không Fail, không lệch giả, đủ phép kiểm của tpan
#   C  bước 9: sửa tay vhost -> lệch đúng file đó, sự cố; --fix ghi lại; sửa tay
#      lần nữa rồi nhận bản sửa tay qua API
#   D  bước 11: scripts/faults/01..05 và 07 -> đúng chặng, sự cố; --fix -> đạt,
#      sự cố đóng; 06 (đầy đĩa) cuối cùng, chỉ restore
#   E  số request và log trực tiếp (SSE), không sót tiến trình tail
#   F  apache: không lệch giả, 03 và 07 trên apache và sửa tự động
#
# Đặt E2E_LOG=<file log> để cuối lần chạy script tự tìm trong chính file log
# đó: mọi mật khẩu đã gặp, IP máy ảo, IP của Mac, tên user Mac (phải là 0 lần).
#
# Output như e2e-m3.sh: mỗi lệnh có dấu thời gian, output nguyên văn (đã che)
# và mã thoát; mỗi khẳng định in "=> ĐẠT" hoặc "=> KHÔNG ĐẠT".
# Không dùng set -e: nhiều lệnh ở đây được chờ là phải lỗi (tpan health thoát 1
# khi có Fail).
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2

VM=scripts/vm.sh
FAILS=0
OUT=""
RC=0
EMAIL=admin@example.com
A=a.example.com
B=b.example.com
C=c.example.com
TMPD="$(mktemp -d)"
chmod 700 "$TMPD"
SECRETS="$TMPD/secrets"
: >"$SECRETS"
chmod 600 "$SECRETS"

# FILL_ON=1 từ lúc 06-disk-full break tới lúc restore: script dừng giữa chừng
# (lỗi, Ctrl-C) vẫn phải xoá file giả, không thì máy ảo đầy đĩa nằm đó.
FILL_ON=0
cleanup() {
  if [ "$FILL_ON" = 1 ]; then
    printf '\n[%s] (trap) restore 06-disk-full\n' "$(ts)"
    scripts/faults/06-disk-full.sh restore </dev/null >/dev/null 2>&1
    FILL_ON=0
  fi
  jobs -p | xargs kill 2>/dev/null
  rm -rf "$TMPD"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

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
  s="$(sed -E 's/^([Ss]et-[Cc]ookie: [^=]+=)[^;]*/\1<đã che>/' <<<"$s")"
  printf '%s' "$s"
}

mask() { OUT="$(mask_str <<<"$OUT")"; }

run() {
  local shown
  shown="$(mask_str <<<"$*")"
  printf '\n[%s] $ %s\n' "$(ts)" "$shown"
  OUT="$("$@" 2>&1 </dev/null)"
  RC=$?
  OUT="${OUT//$'\r'/}"
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

# add_dbpw <domain>: đọc mật khẩu DB của site vào $SECRETS (không in).
add_dbpw() {
  local dbpw
  dbpw="$("$VM" root bash -c 'cd / && runuser -u "$2" -- wp config get DB_PASSWORD --path="/var/www/$1/public" 2>/dev/null' _ "$1" "$(user_of "$1")" </dev/null | tr -d '\r')"
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
  VM_IP="$("$VM" ip)"
  HOST_IP="$("$VM" exec bash -c 'echo "${SSH_CLIENT%% *}"' </dev/null | tr -d '\r')"
  BASE="https://$VM_IP:8443"
  printf 'IP máy ảo: <vm>; IP của Mac trong mạng Multipass: <mac> (cả hai được che trong log)\n'
}

BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
BUILT=0

deploy() {
  if [ "$BUILT" = 0 ]; then
    run make vm-deploy DATE="$BUILD_DATE"
    ok "build (có giao diện) và chép tpan vào máy ảo" rc_is 0
    BUILT=1
    run shasum -a 256 bin/tpan-linux-arm64 bin/tpan-linux-amd64
    run git log --oneline -1
    run git status --short
  else
    # Đã build ở nửa trước của lần chạy này: chỉ chép lại đúng file đó.
    run "$VM" deploy
    ok "chép tpan (bản đã build ở trên) vào máy ảo" rc_is 0
  fi
}

# ---------------------------------------------------------------- HTTP tới panel

JAR="$TMPD/jar"
CODE=""

# api METHOD PATH [cờ curl...]: gọi panel từ Mac, tin đúng cert.pem của panel
# (--cacert, không -k). Thân response ở $TMPD/body.
api() {
  local m=$1 p=$2
  shift 2
  : >"$TMPD/body"
  run curl -sS -m 120 --cacert "$TMPD/cert.pem" -b "$JAR" -c "$JAR" -X "$m" -D - -o "$TMPD/body" \
    -w '-> HTTP %{http_code}, %{time_total}s\n' "$@" "$BASE$p"
  CODE="$(sed -nE 's/^-> HTTP ([0-9]{3}),.*/\1/p' <<<"$OUT" | tail -1)"
}
apiw() { api "$@" -H 'Sec-Fetch-Site: same-origin' -H "Origin: $BASE"; }

code_is() { [ "$CODE" = "$1" ]; }

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

jb() { jq -r "$1" "$TMPD/body" 2>/dev/null; }

# setup_panel: tpan setup --yes, nhặt mật khẩu quản trị (che), lấy cert.pem, đăng nhập.
setup_panel() {
  local cmd=("$VM" root tpan setup --yes) pw
  printf '\n[%s] $ %s\n' "$(ts)" "${cmd[*]}"
  OUT="$("${cmd[@]}" 2>&1 </dev/null)"
  RC=$?
  OUT="${OUT//$'\r'/}"
  pw="$(grep -A1 '^Mật khẩu quản trị (chỉ hiện MỘT lần' <<<"$OUT" | sed -nE 's/^    ([A-Z2-7]{26})$/\1/p' | head -1)"
  add_secret "$pw"
  (umask 077 && printf '%s' "$pw" >"$TMPD/panel-pw")
  mask
  printf '%s\n[%s] (mã thoát %s)\n' "$OUT" "$(ts)" "$RC"
  ok "setup thoát 0" rc_is 0
  ok "có mật khẩu quản trị panel (đã che)" test -n "$pw"
  vexec systemctl is-active tpan
  ok "tpan.service active" has '^active'
  "$VM" root cat /etc/tpan/tls/cert.pem </dev/null | tr -d '\r' >"$TMPD/cert.pem"
  local i
  for i in $(seq 1 30); do
    curl -s -m 2 --cacert "$TMPD/cert.pem" -o /dev/null "$BASE/api/v1/health" && break
    sleep 0.5
  done
  jq -Rn '{password: input}' <"$TMPD/panel-pw" >"$TMPD/login.json"
  chmod 600 "$TMPD/login.json"
  : >"$JAR"
  chmod 600 "$JAR"
  api POST /api/v1/login -H 'Content-Type: application/json' --data-binary "@$TMPD/login.json"
  ok "đăng nhập panel: 204" code_is 204
}

# site_add <domain>: tpan site add qua CLI; mật khẩu quản trị và DB vào danh sách che.
site_add() {
  local d=$1 cmd pw
  cmd=("$VM" root tpan site add "$d" --email "$EMAIL" --yes)
  printf '\n[%s] $ %s\n' "$(ts)" "${cmd[*]}"
  OUT="$("${cmd[@]}" 2>&1 </dev/null)"
  RC=$?
  OUT="${OUT//$'\r'/}"
  pw="$(sed -nE 's/^  Mật khẩu: +([A-Za-z0-9]{32})$/\1/p' <<<"$OUT" | head -1)"
  add_secret "$pw"
  mask
  printf '%s\n[%s] (mã thoát %s)\n' "$OUT" "$(ts)" "$RC"
  ok "site add $d thoát 0" rc_is 0
  ok "job $d thành công" has '^Job .*: thành công'
  add_dbpw "$d"
}

# ---------------------------------------------------------------- health

HJ="$TMPD/health.json"
ROUTE='{"dns":0,"tls":1,"web":2,"php":3,"wordpress":4,"db":5,"e2e":6}'

# health_json [đối số thêm]: tpan health --json trong máy ảo. stdout (JSON)
# vào $HJ, stderr riêng. In bảng "trạng thái  chặng  nhóm  id  tóm tắt", rồi
# bằng chứng của mọi phép kiểm không đạt, rồi sự cố vừa mở, vừa đóng.
# shellcheck disable=SC2120
health_json() {
  printf '\n[%s] $ %s root tpan health --json%s\n' "$(ts)" "$VM" "${*:+ $*}"
  "$VM" root tpan health --json "$@" </dev/null 2>"$TMPD/health.err" | tr -d '\r' >"$HJ"
  RC=${PIPESTATUS[0]}
  OUT="$(cat "$TMPD/health.err")"
  mask
  [ -n "$OUT" ] && printf 'stderr:\n%s\n' "$OUT"
  if ! jq -e .groups "$HJ" >/dev/null 2>&1; then
    OUT="$(head -c 4000 "$HJ")"
    mask
    printf 'stdout không phải báo cáo JSON:\n%s\n' "$OUT"
    printf '[%s] (mã thoát %s)\n' "$(ts)" "$RC"
    return
  fi
  OUT="$(jq -r '"started \(.started)  finished \(.finished)  installed \(.installed)",
    (.groups[] | (if .target == "" then "(máy, stack)" else .target end) as $t | .results[] |
      "  \(.status | ascii_upcase | .[0:4])  \(.stage)\t\($t)\t\(.id)\t\(.summary.vi)\(if .auto_fix then "  [sửa tự động: \(.auto_fix)]" else "" end)")' "$HJ")"
  mask
  printf '%s\n' "$OUT"
  OUT="$(jq -r '[.groups[].results[] | select(.status != "pass")] | if length == 0 then "(mọi phép kiểm đạt)" else (.[] |
      "--- \(.status) \(.id) (chặng \(.stage))\n    \(.summary.vi)\n    nguyên nhân: \(.cause.vi // "")\n    sửa tay: \(.manual_fix // [] | join(" ; "))",
      (.evidence[]? | "    $ \(.command)  (mã \(.exit_code))\n\(.output | split("\n") | map("      | " + .) | join("\n"))")) end' "$HJ")"
  mask
  printf 'Không đạt, kèm bằng chứng:\n%s\n' "$OUT"
  OUT="$(jq -r '"sự cố vừa mở: \([.changes.opened[]? | "#\(.id) \(.check_id)"] | join(", "))", "sự cố vừa đóng: \([.changes.closed[]? | "#\(.id) \(.check_id) (\(.close_reason))"] | join(", "))"' "$HJ")"
  mask
  printf '%s\n[%s] (mã thoát %s)\n' "$OUT" "$(ts)" "$RC"
}

hq() { jq -e "$@" "$HJ" >/dev/null 2>&1; }
no_fail() { hq '[.groups[].results[] | select(.status == "fail")] | length == 0'; }
no_drift_bad() { hq '[.groups[].results[] | select((.id | startswith("drift:")) and .status != "pass")] | length == 0'; }
is_fail() { hq --arg id "$1" 'any(.groups[].results[]; .id == $id and .status == "fail")'; }
is_pass() { hq --arg id "$1" 'any(.groups[].results[]; .id == $id and .status == "pass")'; }
has_id_prefix() { hq --arg p "$1" 'any(.groups[].results[]; .id | startswith($p))'; }
fail_ids() { jq -r '[.groups[].results[] | select(.status == "fail") | .id] | join(" ")' "$HJ"; }

# stage_only <chặng>: mọi Fail (bỏ chặng e2e) thuộc đúng một chặng này.
stage_only() { hq --arg s "$1" '[.groups[].results[] | select(.status == "fail" and .stage != "e2e") | .stage] | unique == [$s]'; }

# first_stage <domain> <chặng>: trên đường đi request của site (phép kiểm của
# site cộng phép kiểm dùng chung của stack), chặng Fail đầu tiên là <chặng>.
first_stage() {
  hq --arg d "$1" --arg s "$2" --argjson o "$ROUTE" '[.groups[] | select(.target == $d or .target == "") | .results[] |
    select(.status == "fail" and $o[.stage] != null)] | sort_by($o[.stage]) | .[0].stage == $s'
}
site_no_fail() { hq --arg d "$1" '[.groups[] | select(.target == $d) | .results[] | select(.status == "fail")] | length == 0'; }

# incidents_all: GET /api/v1/incidents?state=all vào $TMPD/inc.json.
incidents_all() {
  api GET '/api/v1/incidents?state=all&limit=500'
  cp "$TMPD/body" "$TMPD/inc.json"
  show '[.[] | "#\(.id) \(if .closed then "đóng(\(.close_reason))" else "MỞ" end) [\(.stage)] \(.check_id)"]'
}
iq() { jq -e "$@" "$TMPD/inc.json" >/dev/null 2>&1; }
inc_open() { iq --arg id "$1" '[.[] | select(.check_id == $id and (.closed | not))] | length == 1'; }
inc_open_id() { jq -r --arg id "$1" '[.[] | select(.check_id == $id and (.closed | not))][0].id // empty' "$TMPD/inc.json"; }
inc_closed_pass() { iq --argjson n "$1" 'any(.[]; .id == $n and .closed and .close_reason == "pass")'; }
no_dup_open() { iq '[.[] | select(.closed | not) | .check_id] | (length == (unique | length))'; }
no_open_in() { iq --arg ids "$1" '($ids | split(" ")) as $l | [.[] | select((.closed | not) and (.check_id as $c | $l | index($c)))] | length == 0'; }

# http_code_in <domain>: mã HTTP trang chủ gọi trong máy ảo (độc lập với tpan).
http_in() { vexec curl -sS -m 20 -o /dev/null -w '%{http_code}\n' --resolve "$1:80:127.0.0.1" "http://$1/"; }

# ---------------------------------------------------------------- faults

# fault <script> <chặng> "<id mong đợi Fail>" "<site bị ảnh hưởng>" "<site không bị>" <hiện tượng: hàm kiểm độc lập>
fault() {
  local script=$1 stage=$2 ids=$3 hit=$4 clean=$5 symptom=$6 d id
  local -a incs=()
  section "bước 11: $script ($stage) trên $hit"
  health_json
  ok "trước khi gây lỗi: không Fail nào" no_fail
  run "scripts/faults/$script" break "${hit%% *}"
  ok "$script break thoát 0" rc_is 0
  "$symptom" break
  health_json
  ok "tpan health thoát 1 (có Fail)" rc_is 1
  printf 'Fail: %s\n' "$(fail_ids)"
  for id in $ids; do
    ok "$id là Fail" is_fail "$id"
  done
  ok "mọi Fail ngoài chặng e2e đều ở chặng $stage" stage_only "$stage"
  for d in $hit; do
    ok "$d: chặng Fail đầu tiên trên đường đi request là $stage" first_stage "$d" "$stage"
  done
  for d in $clean; do
    ok "$d (không bị đụng): không Fail nào" site_no_fail "$d"
  done
  incidents_all
  for id in $ids; do
    ok "có đúng một sự cố mở cho $id" inc_open "$id"
    incs+=("$(inc_open_id "$id")")
  done
  ok "không check_id nào có hai sự cố mở" no_dup_open

  run "$VM" root tpan health --fix --dry-run
  ok "--fix --dry-run thoát 0" rc_is 0
  ok "--dry-run có kế hoạch sửa" has '^Kế hoạch:'
  health_json
  ok "--dry-run không sửa gì: vẫn còn Fail" rc_is 1

  run "$VM" root tpan health --fix --yes
  ok "--fix --yes thoát 0" rc_is 0
  ok "sửa tự động: mọi job sửa thành công" bash -c '! grep -qE "^Job .*: hỏng" <<<"$1" && grep -qE "^Job .*: thành công" <<<"$1"' _ "$OUT"
  "$symptom" fixed
  health_json
  ok "sau khi sửa: tpan health thoát 0" rc_is 0
  ok "sau khi sửa: không Fail nào" no_fail
  incidents_all
  local i=0
  for id in $ids; do
    ok "sự cố #${incs[$i]} ($id) đã đóng, lý do pass" inc_closed_pass "${incs[$i]:-0}"
    i=$((i + 1))
  done
  ok "không còn sự cố mở nào cho các phép kiểm đó" no_open_in "$ids"
  ok "không check_id nào có hai sự cố mở" no_dup_open
}

# Hiện tượng của từng kịch bản, kiểm bằng lệnh độc lập với tpan.
sym01() {
  if [ "$1" = break ]; then
    vexec systemctl is-active php8.3-fpm
    ok "php8.3-fpm không active" hasnt '^active$'
    http_in "$A"
    ok "$A trả 502" has '^502$'
  else
    vexec systemctl is-active php8.3-fpm
    ok "php8.3-fpm active lại" has '^active$'
    http_in "$A"
    ok "$A trả 200" has '^200$'
    http_in "$B"
    ok "$B trả 200" has '^200$'
  fi
}
sym02() {
  local sock
  sock="/run/php/tpan-$(slug_of "$A").sock"
  if [ "$1" = break ]; then
    vroot test -S "$sock"
    ok "socket của $A không còn" rc_not 0
    http_in "$A"
    ok "$A trả 502" has '^502$'
    http_in "$B"
    ok "$B vẫn 200" has '^200$'
  else
    vroot test -S "$sock"
    ok "socket của $A có lại" rc_is 0
    http_in "$A"
    ok "$A trả 200" has '^200$'
  fi
}
sym04() {
  vroot stat -c '%a %U:%G %n' "/var/www/$A/public/wp-config.php"
  if [ "$1" = break ]; then
    ok "wp-config.php 644" has '^644 '
  else
    ok "wp-config.php về 600, chủ là user của site" has "^600 $(user_of "$A"):"
    vroot runuser -u "$(user_of "$B")" -- cat "/var/www/$A/public/wp-config.php"
    ok "user của $B không đọc được wp-config.php của $A" rc_not 0
  fi
}
sym05() {
  local u
  u="$(user_of "$A")"
  vroot mariadb -N -B -e "SELECT User, Host, JSON_EXTRACT(Priv, '\$.account_locked'), JSON_VALUE(Priv, '\$.account_locked') FROM mysql.global_priv WHERE User = '$u'"
  if [ "$1" = break ]; then
    ok "(mục 16 ý 2) JSON_VALUE account_locked = 1 sau ACCOUNT LOCK (không phải chữ true)" has '	1$'
    vexec curl -sS -m 20 -w '\n%{http_code}\n' --resolve "$A:80:127.0.0.1" "http://$A/"
    ok "$A: Error establishing a database connection" hasF 'Error establishing a database connection'
  else
    ok "(mục 16 ý 2) JSON_VALUE account_locked = 0 sau sửa tự động (ACCOUNT UNLOCK)" has '	0$'
    http_in "$A"
    ok "$A trả 200" has '^200$'
  fi
}

# ---------------------------------------------------------------- các phần

part_a() {
  section "A: máy sạch, triển khai tpan, cài stack nginx"
  reset_vm
  deploy
  section "mục 16 ý 4: scripts/vm.sh root giữ nguyên đối số có dấu cách, ký tự đặc biệt"
  vroot printf '[%s]\n' 'a b' 'c  d' '$HOME' "x'y" '*'
  ok "5 đối số giữ nguyên: [a b] [c  d] [\$HOME] [x'y] [*]" test "$OUT" = "[a b]
[c  d]
[\$HOME]
[x'y]
[*]"
  vroot tpan stack install --web nginx --yes
  ok "stack install thoát 0" rc_is 0
  vexec systemctl is-active nginx php8.3-fpm mariadb
  ok "3 dịch vụ active" bash -c '[ "$(grep -cx active <<<"$1")" -eq 3 ]' _ "$OUT"

  section "A: tpan setup (tpan.service, chứng chỉ panel, bộ hẹn giờ health)"
  setup_panel
  section "mục 16 ý 6: quyền thật sau setup"
  vroot stat -c '%a %U:%G %n' /etc/tpan /etc/tpan/tls /etc/tpan/tls/cert.pem /etc/tpan/tls/key.pem /var/lib/tpan /var/lib/tpan/tpan.db /var/lib/tpan/tpan.db-wal /var/lib/tpan/tpan.db-shm
  ok "key.pem 600 root:root" has '^600 root:root /etc/tpan/tls/key.pem$'
  ok "tpan.db 600 root:root" has '^600 root:root /var/lib/tpan/tpan.db$'

  section "A: tạo 2 site"
  site_add "$A"
  site_add "$B"
  http_in "$A"
  ok "$A 200" has '^200$'
  http_in "$B"
  ok "$B 200" has '^200$'
}

last_run() {
  "$VM" root python3 -c 'import sqlite3; c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True); r = c.execute("SELECT value FROM settings WHERE key = ?", ("health.last_run",)).fetchone(); print(r[0] if r else "")' </dev/null | tr -d '\r'
}

part_b() {
  section "B: máy mới khoẻ"
  # Bộ hẹn giờ chạy một vòng đầy đủ ngay khi tpan.service khởi động (vòng đó
  # lúc setup có thể bị bỏ vì setup đang giữ khoá): khởi động lại sau khi đã có
  # 2 site để có một vòng thật, chờ nó ghi xong.
  vroot python3 -c 'import sqlite3; c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True); [print(*r) for r in c.execute("SELECT key, value FROM settings WHERE key LIKE ?", ("health.%",))]'
  local i before now
  before="$(last_run)"
  # last_run ghi theo giây (RFC 3339): chờ sang giây khác để thấy nó đổi.
  sleep 1.1
  vroot systemctl restart tpan
  ok "restart tpan" rc_is 0
  for i in $(seq 1 90); do
    now="$(last_run)"
    [ -n "$now" ] && [ "$now" != "$before" ] && break
    sleep 1
  done
  printf '[%s] health.last_run trước restart: %s; sau: %s (chờ %s giây)\n' "$(ts)" "${before:-<chưa có>}" "$now" "$i"
  ok "tpan.service đã chạy xong một vòng đầy đủ (health.last_run đổi)" test -n "$now" -a "$now" != "$before"
  vroot python3 -c 'import sqlite3; c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True); [print(*r) for r in c.execute("SELECT key, value FROM settings WHERE key LIKE ?", ("health.%",))]'
  ok "có health.scheduler_beat" has '^health\.scheduler_beat '
  vroot journalctl -u tpan --no-pager -o short-iso
  # Vài request cho access.log và phép kiểm có gì để đọc.
  for i in 1 2 3; do
    curl -s -m 10 -o /dev/null --resolve "$A:80:$VM_IP" "http://$A/"
    curl -s -m 10 -o /dev/null --resolve "$B:80:$VM_IP" "http://$B/"
  done

  health_json
  ok "tpan health thoát 0" rc_is 0
  ok "không Fail nào" no_fail
  ok "không phép kiểm lệch (drift:*) nào khác Pass" no_drift_bad
  ok "có phép kiểm lệch (drift:*)" has_id_prefix drift:
  for p in tpan.cert: tpan.perm: tpan.db:integrity tpan.jobs:stuck tpan.scheduler:health memory:ram disk.free:; do
    ok "có phép kiểm $p" has_id_prefix "$p"
  done
  ok "tpan.scheduler:health Pass" is_pass tpan.scheduler:health
  printf '\nDanh sách file do tpan quản lý (managed_files):\n'
  vroot python3 -c 'import sqlite3; c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True); [print(*r) for r in c.execute("SELECT path, job_id FROM managed_files ORDER BY path")]'

  section "mục 16 ý 5: thời gian một lần tpan health đầy đủ với 2 site, và riêng wp.checksums"
  vroot bash -c 'time tpan health --json >/dev/null'
  run jq -r '"started \(.started)\nfinished \(.finished)"' "$HJ"
  vroot bash -c "cd / && time runuser -u $(user_of "$A") -- wp core verify-checksums --path=/var/www/$A/public"
  vroot bash -c 'time tpan health a.example.com --json >/dev/null'
}

part_c() {
  local conf="/etc/nginx/sites-available/tpan-$A.conf" dpath iid
  dpath="drift:$conf"
  section "C (bước 9): sửa tay vhost của $A bằng một dòng chú thích hợp lệ"
  vroot sha256sum "$conf"
  vroot sed -i '$a # e2e-m4: sua tay boi nguoi van hanh' "$conf"
  ok "sed thoát 0" rc_is 0
  vroot tail -n 3 "$conf"
  vroot nginx -t
  ok "nginx -t vẫn đạt (chú thích hợp lệ)" rc_is 0
  health_json
  ok "tpan health thoát 1" rc_is 1
  ok "$dpath là Fail" is_fail "$dpath"
  ok "lệch ở chặng web, nhóm $A" hq --arg id "$dpath" --arg d "$A" 'any(.groups[] | select(.target == $d) | .results[]; .id == $id and .stage == "web")'
  ok "không file nào khác lệch" hq --arg id "$dpath" '[.groups[].results[] | select((.id | startswith("drift:")) and .status != "pass") | .id] == [$id]'
  ok "Fail duy nhất là lệch đó" hq --arg id "$dpath" '[.groups[].results[] | select(.status == "fail") | .id] == [$id]'
  run "$VM" root tpan health
  ok "bản chữ nhắc lại đúng file lệch" hasF "File bị sửa ngoài tpan (lệch):
  $conf"
  incidents_all
  ok "có đúng một sự cố mở cho $dpath" inc_open "$dpath"
  iid="$(inc_open_id "$dpath")"
  api GET "/api/v1/incidents/$iid"
  show '{incident: (.incident | {id, check_id, target, stage, opened, closed}), current: .current, jobs}'
  ok "GET /incidents/$iid 200, chặng web, target $A" bash -c '[ "$1" = 200 ] && jq -e ".incident.stage == \"web\" and .incident.target == \"$3\"" "$2" >/dev/null' _ "$CODE" "$TMPD/body" "$A"

  section "C: tpan health --fix --dry-run (xem diff), rồi --fix --yes"
  run "$VM" root tpan health --fix --dry-run
  ok "--dry-run thoát 0" rc_is 0
  ok "--dry-run nêu đúng file" hasF "$conf"
  ok "--dry-run hiện diff: dòng sửa tay sẽ bị bỏ" has '^ *- *# e2e-m4: sua tay boi nguoi van hanh'
  vroot grep -c 'e2e-m4: sua tay' "$conf"
  ok "--dry-run không đổi file" has '^1$'
  run "$VM" root tpan health --fix --yes
  ok "--fix --yes thoát 0" rc_is 0
  vroot grep -c 'e2e-m4: sua tay' "$conf"
  ok "dòng sửa tay đã bị ghi đè" has '^0$'
  health_json
  ok "hết lệch, không Fail" no_fail
  ok "$dpath Pass" is_pass "$dpath"
  incidents_all
  ok "sự cố #$iid đã đóng, lý do pass" inc_closed_pass "$iid"
  http_in "$A"
  ok "$A 200" has '^200$'

  section "C: sửa tay lần nữa, nhận bản sửa tay làm chuẩn qua API"
  vroot sed -i '$a # e2e-m4: sua tay lan hai, giu lai' "$conf"
  health_json
  ok "$dpath Fail lại" is_fail "$dpath"
  incidents_all
  ok "có đúng một sự cố mở mới cho $dpath" inc_open "$dpath"
  iid="$(inc_open_id "$dpath")"
  printf '{"path":"%s"}' "/etc/passwd" >"$TMPD/accept-bad.json"
  apiw POST /api/v1/files/accept -H 'Content-Type: application/json' --data-binary "@$TMPD/accept-bad.json"
  show
  ok "đối chứng: nhận /etc/passwd (không do tpan quản lý) bị từ chối 400 path_invalid" bash -c '[ "$1" = 400 ] && [ "$(jq -r .error.code "$2")" = path_invalid ]' _ "$CODE" "$TMPD/body"
  printf '{"path":"%s"}' "$conf" >"$TMPD/accept.json"
  api POST /api/v1/files/accept -H 'Content-Type: application/json' -H 'Sec-Fetch-Site: cross-site' --data-binary "@$TMPD/accept.json"
  ok "đối chứng: cross-site bị CSRF chặn 403" code_is 403
  apiw POST /api/v1/files/accept -H 'Content-Type: application/json' --data-binary "@$TMPD/accept.json"
  ok "POST /api/v1/files/accept: 204" code_is 204
  vroot python3 -c 'import sqlite3,sys; c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True); [print(*r) for r in c.execute("SELECT path, sha256, job_id FROM managed_files WHERE path = ?", (sys.argv[1],))]' "$conf"
  ok "managed_files ghi job_id manual" has ' manual$'
  vroot sha256sum "$conf"
  health_json
  ok "hết lệch, không Fail" no_fail
  ok "$dpath Pass" is_pass "$dpath"
  vroot grep -c 'e2e-m4: sua tay lan hai' "$conf"
  ok "bản sửa tay vẫn nằm trong file" has '^1$'
  incidents_all
  ok "sự cố #$iid đã đóng, lý do pass" inc_closed_pass "$iid"
  vroot python3 -c 'import sqlite3; c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True); [print(*r) for r in c.execute("SELECT time, action, target, result FROM audit ORDER BY id DESC LIMIT 5")]'
  ok "audit có lần nhận file" has "$conf"
}

part_d() {
  fault 01-php-fpm-stop.sh php "service.active:php8.3-fpm php.socket:$A php.socket:$B" "$A $B" "" sym01
  fault 02-pool-socket-rm.sh php "php.socket:$A" "$A" "$B" sym02

  section "mục 16 ý 1: socket pool bị xoá: reload có tạo lại không, restart có không (đo, không phải sửa)"
  local sock
  sock="/run/php/tpan-$(slug_of "$A").sock"
  vroot rm -f "$sock"
  vroot test -S "$sock"
  printf '(test -S sau khi xoá: mã %s)\n' "$RC"
  vroot systemctl reload php8.3-fpm
  sleep 2
  vroot ls -l "$sock"
  printf '=> sau systemctl reload php8.3-fpm: socket %s\n' "$([ "$RC" -eq 0 ] && echo 'CÓ lại' || echo 'KHÔNG có lại')"
  vroot rm -f "$sock"
  vroot systemctl restart php8.3-fpm
  sleep 1
  vroot ls -l "$sock"
  printf '=> sau systemctl restart php8.3-fpm: socket %s\n' "$([ "$RC" -eq 0 ] && echo 'CÓ lại' || echo 'KHÔNG có lại')"
  ok "sau restart socket có lại" rc_is 0
  http_in "$A"
  ok "$A 200" has '^200$'

  # 03 trên site B: vhost của A đang mang bản sửa tay đã được nhận ở phần C;
  # web.rewrite ghi lại mọi vhost theo template, nên chạy 03 trên A thì bản sửa
  # tay đã nhận cũng bị ghi đè (ghi lại ở báo cáo).
  fault 03-vhost-break.sh web "config.nginx:nginx drift:/etc/nginx/sites-available/tpan-$B.conf" "$B" "$A" sym03b
  section "C/D: bản sửa tay đã nhận của $A sau web.rewrite"
  vroot grep -c 'e2e-m4: sua tay lan hai' "/etc/nginx/sites-available/tpan-$A.conf"
  ok "bản sửa tay đã nhận của $A còn nguyên sau web.rewrite (chỉ file hỏng được ghi lại)" test "$OUT" = 1

  fault 04-wpconfig-perm.sh wordpress "wp.config:$A" "$A" "$B" sym04
  fault 05-db-user-lock.sh db "db.access:$A" "$A" "$B" sym05
  # 07 trên A: vhost của A là bản sửa tay đã nhận ở phần C, nên sửa tự động
  # chỉ còn bước symlink (file đã nhận được giữ).
  fault 07-vhost-disable.sh web "web.vhost:$A" "$A" "$B" sym07
  section "C/D: bản sửa tay đã nhận của $A sau sửa web.vhost"
  vroot grep -c 'e2e-m4: sua tay lan hai' "/etc/nginx/sites-available/tpan-$A.conf"
  ok "bản sửa tay đã nhận của $A còn nguyên sau sửa web.vhost (chỉ symlink được tạo lại)" test "$OUT" = 1
}

# wait_http <domain> <mã curl>:<mã HTTP>: sau 07 break, gọi lại trang chủ tới
# khi thấy đúng kết quả (tối đa 10 giây), in mọi lần. Kịch bản reload bằng
# systemctl, lệnh này trả về khoảng 100 ms trước khi nginx áp dụng; đây là e2e
# chờ hiện tượng xuất hiện, không phải phép kiểm của tpan.
wait_http() {
  vexec bash -c 'for i in $(seq 1 40); do c="$(curl -s -m 5 -o /dev/null -w "%{http_code}" --resolve "$1:80:127.0.0.1" "http://$1/")"; rc=$?; echo "lần $i: HTTP $c, curl mã $rc"; [ "$rc:$c" = "$2" ] && exit 0; sleep 0.25; done; exit 1' _ "$1" "$2"
}

sym07() {
  vroot test -L "/etc/nginx/sites-enabled/tpan-$A.conf"
  if [ "$1" = break ]; then
    ok "symlink bật vhost của $A không còn" rc_not 0
    wait_http "$A" 52:000
    ok "$A rơi vào site mặc định (nginx 444, curl mã 52)" rc_is 0
    http_in "$B"
    ok "$B vẫn 200" has '^200$'
  else
    ok "symlink bật vhost của $A có lại" rc_is 0
    http_in "$A"
    ok "$A trả 200" has '^200$'
  fi
}

sym03b() {
  vroot nginx -t
  if [ "$1" = break ]; then
    ok "nginx -t báo lỗi" rc_not 0
    http_in "$B"
    ok "$B vẫn 200 (chưa reload, nginx chạy bản cũ)" has '^200$'
  else
    ok "nginx -t đạt" rc_is 0
    vroot grep -c tpan_fault_invalid "/etc/nginx/sites-available/tpan-$B.conf"
    ok "dòng hỏng đã bị ghi đè" has '^0$'
    http_in "$B"
    ok "$B trả 200" has '^200$'
  fi
}

part_e() {
  section "E: số request và log trực tiếp của $A"
  local mark i
  mark="e2e-m4-$(date +%s)"
  for i in 1 2 3 4 5; do
    curl -s -m 10 -o /dev/null --resolve "$A:80:$VM_IP" "http://$A/?$mark-dem-$i"
  done
  api GET "/api/v1/sites/$A/traffic"
  show
  ok "traffic 200, requests > 0" bash -c '[ "$1" = 200 ] && jq -e ".requests > 0" "$2" >/dev/null' _ "$CODE" "$TMPD/body"
  vroot bash -c "grep -c '$mark-dem' /var/log/tpan/sites/$A/access.log"
  ok "access.log có 5 request vừa gọi" has '^5$'

  vexec pgrep -a tail
  ok "trước khi mở log: không có tiến trình tail" rc_is 1
  printf '\n[%s] $ curl -sN -m 8 --cacert ... -b <cookie> https://<vm>:8443/api/v1/sites/%s/log?file=access   (chạy nền)\n' "$(ts)" "$A"
  curl -sN -m 8 --cacert "$TMPD/cert.pem" -b "$JAR" "$BASE/api/v1/sites/$A/log?file=access" >"$TMPD/sse-log" 2>&1 &
  local pid=$!
  sleep 2
  vexec pgrep -a tail
  ok "trong lúc mở log: có đúng tiến trình tail -n 100 -F access.log của $A" hasF "tail -n 100 -F /var/log/tpan/sites/$A/access.log"
  for i in 1 2 3; do
    curl -s -m 10 -o /dev/null --resolve "$A:80:$VM_IP" "http://$A/?$mark-sse-$i"
    sleep 0.5
  done
  wait "$pid"
  RC=$?
  printf '[%s] curl SSE xong (mã thoát %s; 28 là hết -m 8 giây, đúng ý)\n' "$(ts)" "$RC"
  OUT="$(grep -E "^event: |$mark-sse" "$TMPD/sse-log" | tail -n 12)"
  mask
  printf 'Phần cuối luồng (dòng event: và các dòng có dấu %s):\n%s\n' "$mark" "$OUT"
  printf 'Tổng: %s dòng "event: line"\n' "$(grep -c '^event: line' "$TMPD/sse-log")"
  ok "SSE có event: line" bash -c 'grep -q "^event: line" "$1"' _ "$TMPD/sse-log"
  ok "SSE có cả 3 request vừa gọi trong lúc mở" bash -c 'for i in 1 2 3; do grep -q "^data: .*$2-sse-$i" "$1" || exit 1; done' _ "$TMPD/sse-log" "$mark"
  sleep 2
  vexec pgrep -a tail
  ok "đóng curl 2 giây sau: không còn tiến trình tail nào" rc_is 1

  section "mục 16 ý 3: chữ báo lỗi của tail khi file không có"
  vexec tail -n 1 /var/log/tpan/khong-co-file-nay.log
  vexec timeout 3 tail -n 1 -F /var/log/tpan/khong-co-file-nay.log
  vroot ls -l "/var/log/tpan/sites/$A/"
  # Luồng log khi file không có: tạm đổi tên php-error.log rồi trả lại ngay.
  local php="/var/log/tpan/sites/$A/php-error.log"
  vroot mv "$php" "$php.e2e-tam"
  printf '\n[%s] $ curl -sN -m 5 --cacert ... -b <cookie> https://<vm>:8443/api/v1/sites/%s/log?file=php   (php-error.log đang không có)\n' "$(ts)" "$A"
  OUT="$(curl -sN -m 5 --cacert "$TMPD/cert.pem" -b "$JAR" "$BASE/api/v1/sites/$A/log?file=php" 2>&1)"
  RC=$?
  mask
  printf '%s\n[%s] (mã thoát %s)\n' "$OUT" "$(ts)" "$RC"
  vroot mv "$php.e2e-tam" "$php"
  ok "trả lại php-error.log" rc_is 0
  sleep 1
  vexec pgrep -a tail
  ok "sau luồng php: không còn tail" rc_is 1
}

part_lock() {
  section "mục 16 ý 7: bộ hẹn giờ bỏ vòng khi một job CLI đang giữ khoá"
  printf '\n[%s] $ %s root tpan site add %s --email %s --yes   (chạy nền)\n' "$(ts)" "$VM" "$C" "$EMAIL"
  "$VM" root tpan site add "$C" --email "$EMAIL" --yes </dev/null >"$TMPD/cli-c" 2>&1 &
  local pid=$! held=no i since
  for i in $(seq 1 60); do
    if ! "$VM" root flock -n /run/tpan/tpan.lock true </dev/null >/dev/null 2>&1; then
      held=yes
      break
    fi
    sleep 0.5
  done
  printf '[%s] khoá toàn cục đang bị giữ = %s (sau %s lần thử)\n' "$(ts)" "$held" "$i"
  ok "CLI đang giữ khoá" test "$held" = yes
  since="$("$VM" exec date '+%Y-%m-%d %H:%M:%S' </dev/null | tr -d '\r')"
  vroot systemctl restart tpan
  sleep 4
  vroot journalctl -u tpan --no-pager -o short-iso --since "$since"
  ok "journal của tpan.service: vòng đầu sau restart bị bỏ vì đang có job" hasF 'health: đang có job, bỏ vòng này'
  wait "$pid"
  RC=$?
  OUT="$(cat "$TMPD/cli-c")"
  OUT="${OUT//$'\r'/}"
  add_secret "$(sed -nE 's/^  Mật khẩu: +([A-Za-z0-9]{32})$/\1/p' <<<"$OUT" | head -1)"
  mask
  printf '%s\n(mã thoát %s)\n' "$OUT" "$RC"
  ok "site add $C thoát 0" rc_is 0
  add_dbpw "$C"
  vroot tpan site rm "$C" --yes
  ok "xoá $C (dọn lại, để các phần sau chỉ có 2 site)" rc_is 0
  health_json
  ok "sau khi xoá $C: không Fail" no_fail
}

part_disk() {
  section "bước 11: 06-disk-full (chặng máy, không có sửa tự động)"
  health_json
  ok "trước khi gây lỗi: không Fail nào" no_fail
  FILL_ON=1
  run scripts/faults/06-disk-full.sh break
  ok "06 break thoát 0" rc_is 0
  vexec df -h /
  health_json
  ok "tpan health thoát 1" rc_is 1
  ok "Fail duy nhất là disk.usage:/" hq '[.groups[].results[] | select(.status == "fail") | .id] == ["disk.usage:/"]'
  ok "disk.usage:/ ở chặng machine" hq 'any(.groups[].results[]; .id == "disk.usage:/" and .stage == "machine")'
  ok "chặng tpan không Fail" hq '[.groups[].results[] | select(.stage == "tpan" and .status == "fail")] | length == 0'
  incidents_all
  ok "có đúng một sự cố mở cho disk.usage:/" inc_open "disk.usage:/"
  local iid
  iid="$(inc_open_id "disk.usage:/")"
  run "$VM" root tpan health --fix --dry-run
  ok "--fix --dry-run: disk.usage không có sửa tự động" hasF 'Không có phép kiểm nào sửa tự động được.'
  run scripts/faults/06-disk-full.sh restore
  ok "06 restore thoát 0" rc_is 0
  FILL_ON=0
  health_json
  ok "sau restore: tpan health thoát 0, không Fail" no_fail
  incidents_all
  ok "sự cố #$iid đã đóng, lý do pass" inc_closed_pass "$iid"
}

part_end() {
  section "cuối nửa nginx: log dịch vụ, mọi sự cố"
  "$VM" root journalctl -u tpan --no-pager -o short-iso </dev/null >"$TMPD/journal" 2>&1
  OUT="$(grep -E 'level=(ERROR|WARN)|bỏ vòng' "$TMPD/journal")"
  mask
  printf 'Log của tpan.service: %s dòng; level=ERROR: %s; "database is locked": %s\n%s\n' "$(wc -l <"$TMPD/journal" | tr -d ' ')" \
    "$(grep -c 'level=ERROR' "$TMPD/journal")" "$(grep -c 'database is locked' "$TMPD/journal")" "$OUT"
  ok "log dịch vụ không có 'database is locked'" bash -c '! grep -q "database is locked" "$1"' _ "$TMPD/journal"
  incidents_all
  ok "không còn sự cố mở nào" iq '[.[] | select(.closed | not)] | length == 0'
  vexec systemctl is-active nginx php8.3-fpm mariadb tpan
  ok "4 dịch vụ active" bash -c '[ "$(grep -cx active <<<"$1")" -eq 4 ]' _ "$OUT"
}

main_nginx() {
  part_a
  part_b
  part_c
  part_d
  part_e
  part_lock
  part_disk
  part_end
}

main_apache() {
  section "F: apache từ máy sạch, 1 site"
  reset_vm
  deploy
  vroot tpan stack install --web apache --yes
  ok "stack install apache thoát 0" rc_is 0
  vexec systemctl is-active apache2 php8.3-fpm mariadb
  ok "3 dịch vụ active" bash -c '[ "$(grep -cx active <<<"$1")" -eq 3 ]' _ "$OUT"
  setup_panel
  site_add "$A"
  http_in "$A"
  ok "$A 200" has '^200$'
  local u
  u="$(user_of "$A")"
  vroot sha256sum "/var/www/$A/public/.htaccess"
  # WordPress tự ghi lại .htaccess khi flush rewrite (lưu permalink...): gây
  # đúng việc đó trước khi kiểm lệch.
  vroot bash -c "cd / && runuser -u $u -- wp rewrite flush --hard --path=/var/www/$A/public"
  vroot sha256sum "/var/www/$A/public/.htaccess"
  vroot cat "/var/www/$A/public/.htaccess"
  health_json
  ok "không Fail nào" no_fail
  ok "không phép kiểm lệch (drift:*) nào khác Pass" no_drift_bad
  ok "có phép kiểm lệch (drift:*)" has_id_prefix drift:
  fault 03-vhost-break.sh web "config.apache:apache2 drift:/etc/apache2/sites-available/tpan-$A.conf" "$A" "" symapache
  fault 07-vhost-disable.sh web "web.vhost:$A" "$A" "" sym07apache
}

sym07apache() {
  vroot test -L "/etc/apache2/sites-enabled/tpan-$A.conf"
  if [ "$1" = break ]; then
    ok "symlink bật vhost của $A không còn" rc_not 0
    wait_http "$A" 0:403
    ok "$A rơi vào site mặc định của apache (403)" rc_is 0
  else
    ok "symlink bật vhost của $A có lại" rc_is 0
    http_in "$A"
    ok "$A trả 200" has '^200$'
  fi
}

symapache() {
  vroot apache2ctl configtest
  if [ "$1" = break ]; then
    ok "apache2ctl configtest báo lỗi" rc_not 0
    http_in "$A"
    ok "$A vẫn 200 (chưa reload)" has '^200$'
  else
    ok "apache2ctl configtest đạt" rc_is 0
    http_in "$A"
    ok "$A trả 200" has '^200$'
  fi
}

case "${1:-all}" in
  all)
    main_nginx
    main_apache
    ;;
  nginx) main_nginx ;;
  apache) main_apache ;;
  *)
    sed -n '2,24p' "$0" | sed 's/^# \{0,1\}//'
    exit 2
    ;;
esac

if [ -n "${E2E_LOG:-}" ] && [ -f "$E2E_LOG" ]; then
  section "bí mật, IP, tên user Mac có lọt vào chính file log không"
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
fi

section "TỔNG KẾT: $FAILS khẳng định không đạt"
[ "$FAILS" -eq 0 ]
