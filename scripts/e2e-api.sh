#!/usr/bin/env bash
# e2e các route API mà giao diện web dùng nhưng chưa kịch bản e2e nào gọi, gọi
# thật từ Mac tới panel trên máy ảo (tên trong file .tpan-vm, không có thì
# tpan-test; scripts/vm.sh tự đọc). Chạy trên Mac, trong thư mục repo:
#
#   scripts/e2e-api.sh        từ máy sạch tới hết (khoảng 45-70 phút)
#
# Thân request lấy đúng như giao diện gửi (web/src/screens/**, web/src/api/types.ts):
# POST không thân (backup, đổi mật khẩu DB, xuất DB, cập nhật, bản vá tự động,
# duyệt và từ chối Plan) gửi không có thân, như client.ts làm. Mọi request ghi
# mang Sec-Fetch-Site: same-origin và Origin của panel như trình duyệt.
#
# Các phần, từ dễ tới khó:
#    1  máy sạch, stack nginx, tpan setup, site A và B bằng CLI, đăng nhập
#    2  đọc: /health, /checks, /stack, /php, /updates, /ai/brain, /ai/sessions
#    3  POST /health/run (cả ?domain=)
#    4  /sites/{domain}/log (SSE log trực tiếp)
#    5  CSRF trên các route mới
#    6  /ssl/settings, /trash/settings (không phải job, không có dry_run)
#    7  /ai/preview, /ai/ask khi bộ não tắt
#    8  /security/fail2ban
#    9  /fixes: dừng php8.3-fpm, gỡ symlink vhost, sửa qua API
#   10  /sites/{domain}/backups, /backups/{id}/restore, /backups/settings
#   11  /databases/{domain}/password, /dump, /import
#   12  Plan tạo bằng `tpan mcp` (stdio), /plans/{id}/approve, /reject; đọc
#       phiên AI; bật bộ não (chưa nối) để có một lượt hỏi thật: /ai/preview,
#       /ai/ask, /ai/sessions/{id}, /ai/sessions/{id}/turns/{seq}/events
#   13  /php/install, /sites/{domain}/php, /php/remove (8.4)
#   14  /stack/switch nginx -> apache -> nginx
#   15  /updates (áp dụng), /updates/auto
#   16  /mariadb/upgrade: CHỈ ?dry_run=1 (xem dưới)
#   17  DELETE /account/sessions/{id}
#   18  /account/password
#
# Route chạy job (planJob, /fixes): ?dry_run=1 trước, khẳng định trả bản xem
# trước, bảng jobs không thêm dòng và máy không đổi (lệnh độc lập trong máy
# ảo); rồi chạy thật, nghe GET /jobs/{id}/events (SSE) tới job.done, GET
# /jobs/{id} (chờ job nhả chỗ chạy), kiểm máy bằng lệnh độc lập. Các route
# không qua planJob (duyệt, từ chối Plan, /ssl/settings, /trash/settings,
# /health/run, /account/*, /ai/preview) bỏ qua ?dry_run=1 và làm thật ngay,
# nên ở đó không có bước xem trước.
#
# /mariadb/upgrade chỉ chạy ?dry_run=1: nâng MariaDB là một chiều (không hạ
# lại được trên máy ảo này, các phần sau đó sẽ chạy trên MariaDB khác), tốn
# nhiều phút, và đã được chạy thật trên bản sao ở phần D của scripts/e2e-m12.sh.
#
# CSRF: tpan dùng http.CrossOriginProtection của Go. Request không có cả
# Sec-Fetch-Site lẫn Origin thì được cho qua (Go coi là không phải trình duyệt:
# trình duyệt luôn gửi một trong hai cho POST, DELETE); phần 5 khẳng định đúng
# hành vi đó, cùng các trường hợp bị chặn (cross-site, same-site, Origin lạ).
#
# Đồ nghề test chỉ trong máy ảo: hạ bộ gói php8.3-* về bản của kho release để
# có gói đang chờ cho phần 15 (như phần C của e2e-m12.sh).
#
# Đặt E2E_LOG=<file log> để cuối lần chạy script tự tìm trong chính file log
# đó: mọi mật khẩu đã gặp, IP máy ảo (IPv4, IPv6), IP của Mac, thư mục nhà và
# tên user Mac, giá trị cookie (phải là 0 lần). Ví dụ:
#
#   E2E_LOG=docs/bang-chung/API.log scripts/e2e-api.sh >docs/bang-chung/API.log 2>&1
#
# Output như e2e-m3.sh, e2e-m12.sh: mỗi lệnh có dấu thời gian, output nguyên
# văn (đã che) và mã thoát; mỗi khẳng định in "=> ĐẠT" hoặc "=> KHÔNG ĐẠT";
# cuối mỗi phần và cuối lần chạy in số đạt, không đạt. Che: mật khẩu (panel,
# quản trị WordPress, DB của site), giá trị cookie, IP máy ảo, IP của Mac,
# mạng của Multipass, thư mục nhà và tên user Mac. Mật khẩu và cookie chỉ nằm
# trong file 0600 ở thư mục tạm của Mac, xoá khi script kết thúc; mật khẩu
# không nằm trên argv của lệnh được in (đi qua file).
# Không dùng set -e: nhiều lệnh ở đây được chờ là phải lỗi.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2

VM=scripts/vm.sh
VMNAME="$(tr -d '[:space:]' <.tpan-vm 2>/dev/null)"
VMNAME="${VMNAME:-tpan-test}"
FAILS=0
PASSES=0
OUT=""
RC=0
EMAIL=admin@example.com
A=a.example.com
B=b.example.com
C=c.example.com
D=d.example.com
TMPD="$(mktemp -d)"
chmod 700 "$TMPD"
SECRETS="$TMPD/secrets"
: >"$SECRETS"
chmod 600 "$SECRETS"
trap 'jobs -p | xargs kill 2>/dev/null; rm -rf "$TMPD"' EXIT
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
  s="${s//"$TMPD"/<tmp>}"
  s="${s//"$HOME"/~}"
  s="${s//"$USER"/<user>}"
  while IFS= read -r p; do
    [ -n "$p" ] && s="${s//"$p"/<mật khẩu, đã che>}"
  done <"$SECRETS"
  # Giá trị cookie: HTTP/2 viết tên header thường, HTTP/1.1 viết hoa.
  s="$(sed -E -e 's/^([Ss]et-[Cc]ookie: [^=]+=)[^;]*/\1<đã che>/' -e 's/inet6 [0-9a-f:]{3,}/inet6 <v6>/g' <<<"$s")"
  printf '%s' "$s"
}

mask() { OUT="$(mask_str <<<"$OUT")"; }

# run in lệnh, chạy (stdin là /dev/null: multipass exec không có terminal thì
# chờ stdin mãi), in output đã che và mã thoát.
run() {
  printf '\n[%s] $ %s\n' "$(ts)" "$(mask_str <<<"$*")"
  OUT="$("$@" 2>&1 </dev/null)"
  RC=$?
  OUT="$(tr -d '\r' <<<"$OUT")"
  mask
  [ -n "$OUT" ] && printf '%s\n' "$OUT"
  printf '[%s] (mã thoát %s)\n' "$(ts)" "$RC"
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
SUMMARY=()
part_begin() { P0=$PASSES F0=$FAILS; section "PHẦN $*"; }
part_end() {
  printf '\n==== Phần %s: %s đạt, %s không đạt\n' "$1" "$((PASSES - P0))" "$((FAILS - F0))"
  SUMMARY+=("$(printf '%-4s %5s đạt %5s không đạt' "$1" "$((PASSES - P0))" "$((FAILS - F0))")")
}

rc_is() { [ "$RC" -eq "$1" ]; }
rc_not() { [ "$RC" -ne "$1" ]; }
has() { grep -qE -- "$1" <<<"$OUT"; }
hasnt() { ! grep -qE -- "$1" <<<"$OUT"; }
hasF() { grep -qF -- "$1" <<<"$OUT"; }
out_is() { [ "$OUT" = "$1" ]; }

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

# dbpw <domain>: mật khẩu DB của site, đọc trong máy ảo (không in).
dbpw() {
  "$VM" root bash -c 'cd / && runuser -u "$2" -- wp config get DB_PASSWORD --path="/var/www/$1/public" 2>/dev/null' _ "$1" "$(user_of "$1")" </dev/null | tr -d '\r'
}
# add_dbpw <domain>: đưa mật khẩu DB của site vào danh sách che.
add_dbpw() {
  local p
  p="$(dbpw "$1")"
  add_secret "$p"
  printf 'Đã đọc mật khẩu DB của %s vào danh sách che (%s ký tự).\n' "$1" "${#p}"
}

# SQL_PY: chạy một câu SELECT trên tpan.db (chỉ đọc), mỗi dòng một bản ghi,
# cột cách nhau bằng tab. Tham số thứ hai trở đi là giá trị cho dấu ?.
SQL_PY='
import sqlite3, sys
c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True)
for r in c.execute(sys.argv[1], sys.argv[2:]):
    print(*r, sep="\t")
'
sqlq() { "$VM" root python3 -c "$SQL_PY" "$@" </dev/null | tr -d '\r'; }
jobs_n() { sqlq 'SELECT COUNT(*) FROM jobs'; }
setting() { sqlq 'SELECT value FROM settings WHERE key = ?' "$1"; }

# ---------------------------------------------------------------- máy ảo

reset_vm() {
  printf 'Máy ảo: %s (file .tpan-vm)\n' "$VMNAME"
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

addrs() {
  VM_IP="$("$VM" ip)"
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

BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

deploy() {
  addrs
  run make vm-deploy DATE="$BUILD_DATE"
  ok "build (có giao diện) và chép tpan vào máy ảo" rc_is 0
  run shasum -a 256 bin/tpan-linux-arm64 bin/tpan-linux-amd64
  run git log --oneline -1
  run git status --short
}

# site_add <domain>: tpan site add bằng CLI; mật khẩu quản trị và mật khẩu DB
# vào danh sách che trước khi in.
site_add() {
  local d=$1 pw
  local cmd=("$VM" root tpan site add "$d" --email "$EMAIL" --yes)
  printf '\n[%s] $ %s\n' "$(ts)" "${cmd[*]}"
  OUT="$("${cmd[@]}" 2>&1 </dev/null | tr -d '\r')"
  RC=$?
  pw="$(sed -nE 's/^  Mật khẩu: +([A-Za-z0-9]{32})$/\1/p' <<<"$OUT" | head -1)"
  add_secret "$pw"
  add_dbpw "$d" >/dev/null
  mask
  printf '%s\n[%s] (mã thoát %s)\n' "$OUT" "$(ts)" "$RC"
}

# http_code <domain> [đường dẫn]: mã HTTP gọi trong máy ảo, nối thẳng 127.0.0.1 cổng 80.
http_code() { vexec curl -sS -m 20 --resolve "$1:80:127.0.0.1" -o /dev/null -w '%{http_code}\n' "http://$1${2:-/}"; }
# site_200 <domain> [số lần thử]: chờ site trả 200 (reload của web server có
# độ trễ ngắn), in mã cuối cùng.
site_200() {
  local i
  for i in $(seq 1 "${2:-10}"); do
    http_code "$1"
    [ "$OUT" = 200 ] && break
    sleep 1
  done
  ok "$1 trả 200" out_is 200
}

# ---------------------------------------------------------------- HTTP tới panel

JAR_NONE="$TMPD/jar-none" # luôn rỗng: request chưa đăng nhập
: >"$JAR_NONE"
JAR="$JAR_NONE"
CODE=""

# api METHOD PATH [cờ curl...]: gọi panel từ Mac, kiểm chứng chỉ bằng đúng file
# cert.pem lấy từ máy ảo (--cacert, không -k). In header và mã HTTP; thân nằm ở
# $TMPD/body (in bằng show, sau khi đã nhặt bí mật nếu có). Cookie đọc và ghi
# ở $JAR, không bao giờ in.
api() {
  local m=$1 p=$2
  shift 2
  : >"$TMPD/body"
  run curl -sS -m 300 --cacert "$TMPD/cert.pem" -b "$JAR" -c "$JAR" -X "$m" -D - -o "$TMPD/body" \
    -w '-> HTTP %{http_code}, %{time_total}s\n' "$@" "$BASE$p"
  CODE="$(sed -nE 's/^-> HTTP ([0-9]{3}),.*/\1/p' <<<"$OUT" | tail -1)"
}
# apiw: như api nhưng mang header của trình duyệt khi gọi từ chính panel.
apiw() {
  local m=$1 p=$2
  shift 2
  api "$m" "$p" -H 'Sec-Fetch-Site: same-origin' -H "Origin: $BASE" "$@"
}
# post <đường dẫn> [file JSON]: POST như client.ts: có thân thì gửi JSON
# (in thân đã che trước), không thì không gửi thân.
post() {
  local p=$1 f=${2:-}
  if [ -n "$f" ]; then
    OUT="$(jq -c . "$f" 2>&1)"
    mask
    printf '\n[%s] thân gửi tới %s: %s' "$(ts)" "$p" "$OUT"
    apiw POST "$p" -H 'Content-Type: application/json' --data-binary "@$f"
  else
    apiw POST "$p"
  fi
}

code_is() { [ "$CODE" = "$1" ]; }
# err_is <mã HTTP> <mã lỗi>: response vừa nhận là lỗi đúng dạng ErrorBody.
err_is() { [ "$CODE" = "$1" ] && [ "$(jq -r '.error.code // empty' "$TMPD/body" 2>/dev/null)" = "$2" ]; }
# bj <bộ lọc jq>: thân response thoả bộ lọc (jq -e).
bj() { jq -e "$@" "$TMPD/body" >/dev/null 2>&1; }

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
# login <jar> <file mật khẩu>: đăng nhập vào jar mới; JAR trỏ tới jar đó.
login() {
  JAR=$1
  : >"$JAR"
  chmod 600 "$JAR"
  login_file "$2" "$TMPD/login.json"
  api POST /api/v1/login -H 'Content-Type: application/json' --data-binary "@$TMPD/login.json"
}

# take_secrets: nhặt mật khẩu quản trị WordPress (Secret, Secrets) của thân
# GET /jobs/{id} vào danh sách che, TRƯỚC khi in gì. TOOK là số mật khẩu nhặt được.
TOOK=0
take_secrets() {
  local s
  TOOK=0
  while IFS= read -r s; do
    [ -n "$s" ] || continue
    add_secret "$s"
    TOOK=$((TOOK + 1))
  done < <(jq -r '(.secret.password // empty), (.secrets[]?.password // empty)' "$TMPD/body" 2>/dev/null)
}

# SSE của job: sse_start <job id> <file>: mở luồng ở nền, mỗi dòng có thời
# điểm Mac nhận được. Chờ bằng sse_wait.
SSE_PID=""
sse_start() {
  printf '\n[%s] $ curl -sN --cacert ... -b <cookie> %s/api/v1/jobs/%s/events | <gắn thời điểm nhận> (chạy nền)\n' "$(ts)" '<vm>' "$1"
  (curl -sN -m 1800 --cacert "$TMPD/cert.pem" -b "$JAR" "$BASE/api/v1/jobs/$1/events" |
    perl -MTime::HiRes=time -ne 'BEGIN { $| = 1 } printf "%.3f %s", time, $_') >"$2" 2>&1 &
  SSE_PID=$!
}
sse_wait() {
  wait "$SSE_PID"
  printf '[%s] luồng SSE đã đóng (mã thoát %s)\n' "$(ts)" "$?"
}

# sse_report <file> <job id>: bảng sự kiện đã nhận (bỏ dòng output cho gọn) và
# khẳng định về luồng.
SSE_SUMMARY=""
sse_report() {
  local f=$1 id=$2
  sed -nE 's/^([0-9.]+) data: (.*)$/{"recv":\1,"ev":\2}/p' "$f" >"$f.jsonl"
  printf '\nSự kiện SSE đã nhận (trừ output; thời điểm nhận tính từ sự kiện đầu; message cắt 160 ký tự):\n'
  OUT="$(jq -rs '(.[0].recv) as $t0 | .[] | select(.ev.kind != "output") | "+\(((.recv - $t0) * 1000 | round) / 1000)s  #\(.ev.seq)  \(.ev.kind)  \(.ev.step_id // "")  \(if .ev.check then "[\(.ev.check.status)] \(.ev.check.id)" else (.ev.message // "" | .[0:160]) end)"' "$f.jsonl" 2>&1)"
  mask
  printf '%s\n' "$OUT"
  OUT="$(jq -s '{
      events: length,
      first: .[0].ev.kind,
      last: .[-1].ev.kind,
      last_message: .[-1].ev.message,
      job_ids: ([.[].ev.job_id] | unique),
      seq_contiguous: ([.[].ev.seq] == [range(1; length + 1)]),
      kinds: (group_by(.ev.kind) | map({(.[0].ev.kind): length}) | add),
      checks: ([.[] | select(.ev.kind == "check") | .ev.check.status] | group_by(.) | map({(.[0]): length}) | add)
    }' "$f.jsonl" 2>&1)"
  mask
  printf '\nTóm tắt luồng:\n%s\n' "$OUT"
  SSE_SUMMARY="$OUT"
  ok "SSE: mọi sự kiện thuộc job $id" bash -c '[ "$(jq -c .job_ids <<<"$1")" = "[\"$2\"]" ]' _ "$SSE_SUMMARY" "$id"
  ok "SSE: sự kiện đầu là job.start, cuối là job.done, Seq liền mạch từ 1" bash -c 'jq -e ".first == \"job.start\" and .last == \"job.done\" and .seq_contiguous" <<<"$1" >/dev/null' _ "$SSE_SUMMARY"
}

# dry <mô tả> <kind mong đợi> <đường dẫn> [file thân]: POST ?dry_run=1 như nút
# "Xem trước". Khẳng định 200, đúng kind, có bước, và bảng jobs không thêm
# dòng. Bản xem trước giữ ở $TMPD/preview.json.
dry() {
  local desc=$1 kind=$2 p=$3 f=${4:-} n0 n1
  n0="$(jobs_n)"
  post "$p?dry_run=1" "$f"
  cp "$TMPD/body" "$TMPD/preview.json"
  show '{kind, target, title: .title.vi, steps: [.steps[]? | "\(if .done then "[đã đúng] " else "" end)\(.info.id): \(.info.title.vi)"], error}'
  n1="$(jobs_n)"
  ok "$desc: xem trước 200, kind $kind, có bước" bash -c '[ "$1" = 200 ] && jq -e --arg k "$3" "(.kind == \$k or (\$k | endswith(\".\")) and (.kind | startswith(\$k))) and (.steps | length) > 0" "$2" >/dev/null' _ "$CODE" "$TMPD/body" "$kind"
  ok "$desc: xem trước không tạo job (bảng jobs vẫn $n0 dòng, giờ $n1)" test "$n0" = "$n1"
}
# pv <bộ lọc jq>: bản xem trước vừa nhận thoả bộ lọc.
pv() { jq -e "$@" "$TMPD/preview.json" >/dev/null 2>&1; }

# job <mô tả> <đường dẫn> [file thân]: chạy thật như nút "Chạy": 202 có
# job_id, nghe SSE tới job.done, rồi GET /jobs/{id} (handler chờ goroutine
# của job xong hẳn, tức đã nhả chỗ chạy job, nên POST tiếp theo không gặp
# 409 job_running). JID là id của job.
JID=""
job() {
  local desc=$1 p=$2 f=${3:-}
  post "$p" "$f"
  JID="$(jb '.job_id // empty')"
  show
  ok "$desc: 202 có job_id" bash -c '[ "$1" = 202 ] && [[ "$2" =~ ^[0-9a-f]{16}$ ]]' _ "$CODE" "$JID"
  [ -n "$JID" ] || return 1
  job_wait "$desc" "$JID"
}
job_wait() {
  local desc=$1 id=$2
  sse_start "$id" "$TMPD/sse-$id"
  sse_wait
  sse_report "$TMPD/sse-$id" "$id"
  ok "$desc: SSE job.done báo succeeded" bash -c 'jq -e ".last_message == \"succeeded\"" <<<"$1" >/dev/null' _ "$SSE_SUMMARY"
  api GET "/api/v1/jobs/$id"
  take_secrets
  show '{job: (.job | {id, kind, target, status, error}), events: (.events | length), has_secret: (has("secret") or has("secrets"))}'
  ok "$desc: GET /jobs/$id: 200, status succeeded" bash -c '[ "$1" = 200 ] && jq -e ".job.status == \"succeeded\"" "$2" >/dev/null' _ "$CODE" "$TMPD/body"
}

# health_run [domain]: POST /health/run như nút "Kiểm lại"; in gọn, giữ báo
# cáo ở $TMPD/hr.json.
HR_SHOW='{installed, groups: [.groups[] | {target: (if .target == "" then "(máy)" else .target end), pass: ([.results[] | select(.status == "pass")] | length), not_pass: [.results[] | select(.status != "pass") | "\(.status) \(.id)"]}], opened: [.changes.opened[]? | .check_id], closed: [.changes.closed[]? | .check_id]}'
health_run() {
  apiw POST "/api/v1/health/run${1:+?domain=$1}"
  cp "$TMPD/body" "$TMPD/hr.json"
  show "$HR_SHOW"
}
# hr_status <check id>: trạng thái của phép kiểm trong báo cáo vừa chạy.
hr_status() { jq -r --arg id "$1" '[.groups[].results[] | select(.id == $id) | .status][0] // "không có"' "$TMPD/hr.json"; }
# hr_all_pass: mọi phép kiểm (trừ nhóm tpan) đạt. Riêng cảnh báo DNS (dns.a) và
# TLS (tls.cert) của site chưa xin SSL là có chủ ý (mục 6.7): tên miền thử không
# có bản ghi A thật, site chưa có chứng chỉ.
hr_all_pass() { jq -e '[.groups[] | select(.target != "tpan") | .results[] | select(.status != "pass" and ((.status == "warn" and (.id | test("^(dns[.]a|tls[.]cert):"))) | not))] | length == 0' "$TMPD/hr.json" >/dev/null 2>&1; }

# tpan_health: `tpan health --json` trong máy ảo (đối chứng độc lập với API).
HJ=""
tpan_health() {
  local cmd=("$VM" root tpan health --json)
  printf '\n[%s] $ %s\n' "$(ts)" "${cmd[*]}"
  HJ="$("${cmd[@]}" 2>&1 </dev/null | tr -d '\r')"
  RC=$?
  OUT="$(jq -r '.groups[] | (if .target == "" then "(máy)" else .target end) as $t | .results[] | select(.status != "pass") | "\($t)  \(.id)  \(.status)"' <<<"$HJ" 2>&1)"
  mask
  printf '%s\n(chỉ in phép kiểm không đạt; JSON đầy đủ không chép vào log)\n[%s] (mã thoát %s)\n' "${OUT:-(không có)}" "$(ts)" "$RC"
}
tpan_health_pass() { jq -e '[.groups[] | select(.target != "tpan") | .results[] | select(.status != "pass" and ((.status == "warn" and (.id | test("^(dns[.]a|tls[.]cert):"))) | not))] | length == 0' <<<"$HJ" >/dev/null 2>&1; }

# backups_of <domain>: GET /backups?domain=, giữ ở $TMPD/bl.json.
backups_of() {
  api GET "/api/v1/backups?domain=$1"
  cp "$TMPD/body" "$TMPD/bl.json"
  show '{settings, items: [.items[] | "\(.kind) \(.reason) \(.id) \(.size) \(.path)"]}'
}
blq() { jq -e "$@" "$TMPD/bl.json" >/dev/null 2>&1; }
blr() { jq -r "$1" "$TMPD/bl.json"; }

restart_tpan() {
  vroot systemctl restart tpan
  ok "restart tpan thoát 0" rc_is 0
  local _
  for _ in $(seq 1 40); do
    curl -s -m 2 --cacert "$TMPD/cert.pem" -o /dev/null "$BASE/api/v1/health" && break
    sleep 0.5
  done
}

# ---------------------------------------------------------------- đoạn chạy trong máy ảo

# Tiến trình con của PHP-FPM: pool, user, và master (file php-fpm.conf) của nó.
PROC_PY='
import subprocess
out = subprocess.run(["ps", "-eo", "pid=,ppid=,user:40=,args="], capture_output=True, text=True).stdout
rows = {}
for l in out.splitlines():
    p = l.split(None, 3)
    if len(p) == 4:
        rows[p[0]] = (p[1], p[2], p[3])
seen = set()
for pid, (ppid, user, args) in rows.items():
    if args.startswith("php-fpm: pool "):
        m = rows.get(ppid, ("", "", ""))[2]
        conf = m[m.find("(") + 1:m.find(")")] if "(" in m else m
        seen.add("pool " + args.split()[2] + " user " + user + " master " + conf)
    elif args.startswith("php-fpm: master"):
        seen.add("master " + args)
for s in sorted(seen):
    print(s)
'

# ================================================================ 1

PANEL_PW="" # file mật khẩu panel đang dùng
J1=""
p1_setup() {
  part_begin "1: máy sạch, stack nginx, tpan setup, site $A và $B bằng CLI, đăng nhập panel"
  reset_vm
  deploy
  vroot tpan stack install --web nginx --yes
  ok "stack install thoát 0, job thành công" bash -c '[ "$2" = 0 ] && grep -q "^Job .*: thành công" <<<"$1"' _ "$OUT" "$RC"

  local cmd=("$VM" root tpan setup --yes) pw
  printf '\n[%s] $ %s\n' "$(ts)" "${cmd[*]}"
  OUT="$("${cmd[@]}" 2>&1 </dev/null | tr -d '\r')"
  RC=$?
  pw="$(grep -A1 '^Mật khẩu quản trị (chỉ hiện MỘT lần' <<<"$OUT" | sed -nE 's/^    ([A-Z2-7]{26})$/\1/p' | head -1)"
  PANEL_PW="$TMPD/panel-pw-1"
  if [ -n "$pw" ]; then
    add_secret "$pw"
    (umask 077 && printf '%s' "$pw" >"$PANEL_PW")
  fi
  mask
  printf '%s\n[%s] (mã thoát %s)\n' "$OUT" "$(ts)" "$RC"
  ok "setup thoát 0, nhặt được mật khẩu panel" bash -c '[ "$1" -eq 0 ] && [ -s "$2" ]' _ "$RC" "$PANEL_PW"
  vexec systemctl is-active tpan
  ok "tpan.service active" out_is active

  local d
  for d in "$A" "$B"; do
    site_add "$d"
    ok "site add $d thoát 0, job thành công" bash -c '[ "$2" = 0 ] && grep -q "^Job .*: thành công" <<<"$1"' _ "$OUT" "$RC"
    site_200 "$d"
  done
  vexec systemctl is-active nginx php8.3-fpm mariadb
  ok "nginx, php8.3-fpm, mariadb active" bash -c '[ "$(grep -cx active <<<"$1")" -eq 3 ]' _ "$OUT"

  "$VM" root cat /etc/tpan/tls/cert.pem </dev/null | tr -d '\r' >"$TMPD/cert.pem"
  run openssl x509 -in "$TMPD/cert.pem" -noout -subject -dates
  ok "lấy được cert.pem của panel từ máy ảo" rc_is 0
  J1="$TMPD/jar1"
  login "$J1" "$PANEL_PW"
  ok "đăng nhập phiên 1: 204" code_is 204
  part_end 1
}

# ================================================================ 2

p2_reads() {
  part_begin "2: các route chỉ đọc"
  JAR="$J1"

  section "2: GET /api/v1/health (công khai) so với tpan version"
  api GET /api/v1/health
  show
  local ver
  ver="$(jb .version)"
  ok "200, status ok, có version" bash -c '[ "$1" = 200 ] && [ -n "$2" ] && jq -e ".status == \"ok\"" "$3" >/dev/null' _ "$CODE" "$ver" "$TMPD/body"
  vexec tpan version
  ok "version khớp dòng đầu của tpan version trong máy ảo" bash -c 'head -1 <<<"$1" | grep -qF "tpan $2 "' _ "$OUT" "$ver"
  local jn=$JAR
  JAR="$JAR_NONE"
  api GET /api/v1/health
  ok "không cần đăng nhập: 200" code_is 200
  api GET /api/v1/checks
  ok "đối chứng: /checks khi chưa đăng nhập: 401 unauthorized" err_is 401 unauthorized
  JAR="$jn"

  section "2: GET /api/v1/checks (phép kiểm của stack)"
  api GET /api/v1/checks
  show '{installed, config, results: [.results[] | "\(.status)  \(.stage)  \(.id)"]}'
  ok "200, installed, có phép kiểm, không phép kiểm nào fail" bash -c '[ "$1" = 200 ] && jq -e ".installed and (.results | length) > 0 and ([.results[] | select(.status == \"fail\")] | length == 0)" "$2" >/dev/null' _ "$CODE" "$TMPD/body"
  ok "có service.active của nginx, php8.3-fpm, mariadb, cả ba pass" bj '[.results[] | select((.id == "service.active:nginx" or .id == "service.active:php8.3-fpm" or .id == "service.active:mariadb") and .status == "pass")] | length == 3'
  vexec systemctl is-active nginx php8.3-fpm mariadb
  ok "đối chứng độc lập: systemctl is-active cả ba active" bash -c '[ "$(grep -cx active <<<"$1")" -eq 3 ]' _ "$OUT"

  section "2: GET /api/v1/stack"
  api GET /api/v1/stack
  show '{installed, config, services: [.services[] | "\(.status) \(.id)"]}'
  ok "200, installed, web nginx, php 8.3, 3 dịch vụ pass" bash -c '[ "$1" = 200 ] && jq -e ".installed and .config.web == \"nginx\" and .config.php_version == \"8.3\" and (.services | length) == 3 and all(.services[]; .status == \"pass\")" "$2" >/dev/null' _ "$CODE" "$TMPD/body"
  vexec dpkg-query -W -f '${Package} ${db:Status-Status}\n' nginx apache2
  ok "đối chứng: nginx installed, apache2 không" bash -c 'grep -q "^nginx installed" <<<"$1" && ! grep -q "^apache2 installed" <<<"$1"' _ "$OUT"

  section "2: GET /api/v1/php"
  api GET /api/v1/php
  show
  ok "200, 8.3 đã cài, mặc định, site [$A, $B], sury chưa thêm" bash -c '[ "$1" = 200 ] && jq -e --arg a "$3" --arg b "$4" "(.sury_enabled | not) and ([.versions[] | select(.version == \"8.3\" and .installed and .default and .sites == [\$a, \$b])] | length == 1)" "$2" >/dev/null' _ "$CODE" "$TMPD/body" "$A" "$B"
  ok "chỉ 8.3 là installed" bj '[.versions[] | select(.installed) | .version] == ["8.3"]'
  vexec bash -c 'ls /etc/php; ls /etc/apt/sources.list.d/'
  ok "đối chứng: /etc/php chỉ có 8.3, không có tpan-sury" bash -c '[ "$(grep -xE "[0-9]+\.[0-9]+" <<<"$1")" = 8.3 ] && ! grep -q tpan-sury <<<"$1"' _ "$OUT"

  section "2: GET /api/v1/updates"
  api GET /api/v1/updates
  show '{packages: [.packages[] | "\(.name) \(.from) -> \(.to)"], others, others_security, lists_updated, auto: (.auto | {id, status}), auto_origins, mariadb, self}'
  ok "200, packages là mảng, có auto, mariadb.line, self.version" bash -c '[ "$1" = 200 ] && jq -e "(.packages | type == \"array\") and (.auto.id | length > 0) and (.mariadb.line | length > 0) and (.self.version | length > 0)" "$2" >/dev/null' _ "$CODE" "$TMPD/body"
  local line mver
  line="$(jb .mariadb.line)"
  mver="$(jb .mariadb.version)"
  ok "self.version bằng version của /health" test "$(jb .self.version)" = "$ver"
  vexec bash -c 'mariadb --version; dpkg-query -W -f "\${Version}\n" mariadb-server'
  ok "đối chứng: mariadb --version thuộc dòng $line, mariadb.version = bản gói mariadb-server" bash -c 'head -1 <<<"$1" | grep -qF " $2." && [ "$(tail -1 <<<"$1")" = "$3" ]' _ "$OUT" "$line" "$mver"

  section "2: GET /api/v1/ai/brain, /api/v1/ai/sessions (chưa có phiên nào)"
  api GET /api/v1/ai/brain
  show
  ok "200, mode off, chưa nối, có lệnh nối" bash -c '[ "$1" = 200 ] && jq -e ".mode == \"off\" and (.connected | not) and (.connect_command | startswith(\"tpan brain \"))" "$2" >/dev/null' _ "$CODE" "$TMPD/body"
  api GET /api/v1/ai/sessions
  show
  ok "200, []" bash -c '[ "$1" = 200 ] && jq -e ". == []" "$2" >/dev/null' _ "$CODE" "$TMPD/body"
  api GET '/api/v1/ai/sessions?limit=0'
  show
  ok "limit=0: 400 limit_invalid" err_is 400 limit_invalid
  api GET /api/v1/ai/sessions/0123456789abcdef
  show
  ok "phiên không có: 404 ai_session_not_found" err_is 404 ai_session_not_found
  api GET /api/v1/ai/sessions/0123456789abcdef/turns/1/events
  show
  ok "luồng của lượt không có: 404 ai_turn_not_found" err_is 404 ai_turn_not_found
  vroot python3 -c "$SQL_PY" 'SELECT (SELECT COUNT(*) FROM ai_sessions), (SELECT COUNT(*) FROM ai_turns)'
  ok "đối chứng: bảng ai_sessions và ai_turns rỗng" out_is "$(printf '0\t0')"
  part_end 2
}

# ================================================================ 3

p3_health_run() {
  part_begin "3: POST /api/v1/health/run (nút \"Kiểm lại\")"
  JAR="$J1"
  local n0
  n0="$(sqlq 'SELECT COUNT(*) FROM check_results')"
  health_run
  ok "200, installed, có nhóm máy, $A, $B" bash -c '[ "$1" = 200 ] && jq -e --arg a "$3" --arg b "$4" ".installed and ([.groups[].target] | index(\"\") != null and index(\$a) != null and index(\$b) != null)" "$2" >/dev/null' _ "$CODE" "$TMPD/hr.json" "$A" "$B"
  ok "mọi phép kiểm (trừ nhóm tpan) đạt" hr_all_pass
  vroot python3 -c "$SQL_PY" 'SELECT COUNT(*) FROM check_results'
  printf '(check_results trước: %s dòng)\n' "$n0"
  ok "check_results có dòng (health ghi kết quả)" test "$OUT" -gt 0
  tpan_health
  ok "đối chứng độc lập: tpan health --json cũng đạt hết" tpan_health_pass

  section "3: POST /api/v1/health/run?domain=$A"
  health_run "$A"
  ok "200, chỉ có nhóm $A" bash -c '[ "$1" = 200 ] && jq -e --arg a "$3" "[.groups[].target] | unique == [\$a]" "$2" >/dev/null' _ "$CODE" "$TMPD/hr.json" "$A"
  ok "mọi phép kiểm của $A đạt" hr_all_pass
  apiw POST "/api/v1/health/run?domain=nope.example.com"
  show
  ok "?domain= site không có: 404 site_not_found" err_is 404 site_not_found
  apiw POST "/api/v1/health/run?domain=khong_hop_le"
  show '.error.code'
  ok "?domain= sai dạng: 400 domain_invalid" err_is 400 domain_invalid
  part_end 3
}

# ================================================================ 4

p4_log() {
  part_begin "4: GET /api/v1/sites/{domain}/log (SSE log trực tiếp)"
  JAR="$J1"
  local mark f="$TMPD/log-sse" rc
  mark="e2e-api-log-$(date +%s)"
  printf '\n[%s] $ curl -sS -N -m 8 --cacert ... -b <cookie> %s/api/v1/sites/%s/log?file=access (chạy nền)\n' "$(ts)" '<vm>' "$A"
  (curl -sS -N -m 8 --cacert "$TMPD/cert.pem" -b "$JAR" -D "$f.hdr" "$BASE/api/v1/sites/$A/log?file=access" >"$f" 2>"$f.err"
    echo "$?" >"$f.rc") &
  local pid=$!
  sleep 3
  http_code "$A" "/$mark"
  ok "gọi $A/$mark trong lúc luồng mở (404 của WordPress là đủ)" bash -c '[ -n "$1" ]' _ "$OUT"
  wait "$pid"
  rc="$(cat "$f.rc")"
  OUT="$(cat "$f.hdr"; printf -- '--- %s dòng data:, 5 dòng cuối:\n' "$(grep -c '^data: ' "$f")"; grep -E '^(event|data): ' "$f" | tail -10)"
  mask
  printf '%s\n[%s] curl thoát mã %s\n' "$OUT" "$(ts)" "$rc"
  ok "curl dừng vì hết -m 8 (mã 28): luồng log không tự đóng" test "$rc" = 28
  ok "Content-Type: text/event-stream" grep -qi '^content-type: text/event-stream' "$f.hdr"
  ok "có sự kiện line, mỗi data là JSON {line}" bash -c 'grep -q "^event: line" "$1" && sed -n "s/^data: //p" "$1" | jq -e -s "length > 0 and all(.[]; has(\"line\"))" >/dev/null' _ "$f"
  ok "dòng có $mark tới qua luồng trong lúc mở" bash -c 'sed -n "s/^data: //p" "$1" | jq -r .line | grep -qF "$2"' _ "$f" "$mark"
  vroot grep -c "$mark" "/var/log/tpan/sites/$A/access.log"
  ok "đối chứng: access.log của $A có đúng 1 dòng $mark" out_is 1
  api GET "/api/v1/sites/$A/log?file=passwd"
  show
  ok "file lạ: 400 file_invalid" err_is 400 file_invalid
  api GET "/api/v1/sites/nope.example.com/log?file=access"
  show '.error.code'
  ok "site không có: 404 site_not_found" err_is 404 site_not_found
  part_end 4
}

# ================================================================ 5

p5_csrf() {
  part_begin "5: CSRF trên các route mới (request có thay đổi, đã đăng nhập)"
  JAR="$J1"
  local n0 n1
  n0="$(jobs_n)"
  printf '{"version":"8.4"}' >"$TMPD/php84.json"
  printf '{"to":"apache"}' >"$TMPD/to-apache.json"
  api POST /api/v1/php/install -H 'Content-Type: application/json' -H 'Sec-Fetch-Site: cross-site' --data-binary "@$TMPD/php84.json"
  show
  ok "POST /php/install, Sec-Fetch-Site: cross-site: 403 csrf" err_is 403 csrf
  api POST /api/v1/stack/switch -H 'Content-Type: application/json' -H 'Origin: https://evil.example' --data-binary "@$TMPD/to-apache.json"
  show '.error.code'
  ok "POST /stack/switch, Origin lạ (không Sec-Fetch-Site): 403 csrf" err_is 403 csrf
  api POST /api/v1/updates/auto -H 'Sec-Fetch-Site: same-site'
  show '.error.code'
  ok "POST /updates/auto, Sec-Fetch-Site: same-site (trang cùng site khác origin): 403 csrf" err_is 403 csrf
  api POST /api/v1/health/run -H 'Sec-Fetch-Site: cross-site'
  show '.error.code'
  ok "POST /health/run cross-site: 403 csrf" err_is 403 csrf
  api POST "/api/v1/sites/$A/backups" -H 'Sec-Fetch-Site: cross-site'
  show '.error.code'
  ok "POST /sites/$A/backups cross-site: 403 csrf" err_is 403 csrf
  api POST "/api/v1/databases/$A/password" -H 'Origin: https://evil.example'
  show '.error.code'
  ok "POST /databases/$A/password, Origin lạ: 403 csrf" err_is 403 csrf
  printf '{}' >"$TMPD/empty.json"
  api POST /api/v1/account/password -H 'Content-Type: application/json' -H 'Sec-Fetch-Site: cross-site' --data-binary "@$TMPD/empty.json"
  show '.error.code'
  ok "POST /account/password cross-site: 403 csrf" err_is 403 csrf
  api DELETE /api/v1/account/sessions/0123456789abcdef -H 'Sec-Fetch-Site: cross-site'
  show '.error.code'
  ok "DELETE /account/sessions/{id} cross-site: 403 csrf" err_is 403 csrf
  api POST /api/v1/ai/preview -H 'Content-Type: application/json' -H 'Sec-Fetch-Site: cross-site' --data-binary "@$TMPD/empty.json"
  show '.error.code'
  ok "POST /ai/preview cross-site: 403 csrf" err_is 403 csrf
  n1="$(jobs_n)"
  ok "không request nào ở trên tạo job (bảng jobs vẫn $n0 dòng, giờ $n1)" test "$n0" = "$n1"
  vexec bash -c 'ls /etc/apt/sources.list.d/; dpkg-query -W -f "\${Package} \${db:Status-Status}\n" apache2 php8.4-fpm 2>&1; ls /var/lib/tpan/backups 2>&1'
  ok "đối chứng: máy không đổi (không tpan-sury, không apache2, không php8.4-fpm, chưa có backup)" bash -c '! grep -q tpan-sury <<<"$1" && ! grep -qE "^(apache2|php8\.4-fpm) installed" <<<"$1" && ! grep -qF "$2" <<<"$1"' _ "$OUT" "$A"

  section "5: đối chứng: request không mang Origin lẫn Sec-Fetch-Site (không phải trình duyệt) được cho qua"
  printf '(http.CrossOriginProtection của Go: thiếu cả hai header thì coi là cùng origin hoặc không phải trình duyệt; trình duyệt luôn gửi một trong hai cho POST.)\n'
  api POST "/api/v1/health/run?domain=$A"
  ok "POST /health/run?domain=$A không header: 200" code_is 200
  apiw POST "/api/v1/health/run?domain=$A"
  ok "POST /health/run?domain=$A, Sec-Fetch-Site same-origin, Origin của panel: 200" code_is 200
  part_end 5
}

# ================================================================ 6

p6_settings() {
  part_begin "6: /ssl/settings và /trash/settings (chỉ ghi settings, không phải job)"
  JAR="$J1"
  section "6: POST /api/v1/ssl/settings"
  printf '{"email":"%s","staging":true}' "$EMAIL" >"$TMPD/ssl1.json"
  post /api/v1/ssl/settings "$TMPD/ssl1.json"
  show
  ok "200, trả email và staging true" bash -c '[ "$1" = 200 ] && jq -e --arg e "$3" ".email == \$e and .staging == true" "$2" >/dev/null' _ "$CODE" "$TMPD/body" "$EMAIL"
  vroot python3 -c "$SQL_PY" "SELECT key, value FROM settings WHERE key LIKE 'ssl.%' ORDER BY key"
  ok "đối chứng: settings ssl.email = $EMAIL, ssl.staging = 1" bash -c 'grep -qxF "$(printf "ssl.email\t%s" "$2")" <<<"$1" && grep -qx "$(printf "ssl.staging\t1")" <<<"$1"' _ "$OUT" "$EMAIL"
  printf '{"staging":false}' >"$TMPD/ssl2.json"
  post /api/v1/ssl/settings "$TMPD/ssl2.json"
  show
  ok "chỉ gửi staging false: 200, email giữ nguyên" bash -c '[ "$1" = 200 ] && jq -e --arg e "$3" ".email == \$e and .staging == false" "$2" >/dev/null' _ "$CODE" "$TMPD/body" "$EMAIL"
  ok "đối chứng: ssl.staging = 0" test "$(setting ssl.staging)" = 0
  printf '{"server":"https://acme.example.com/directory"}' >"$TMPD/ssl3.json"
  post /api/v1/ssl/settings "$TMPD/ssl3.json"
  show '.error.code'
  ok "trường server (chỉ đặt ở CLI): 400 bad_request" err_is 400 bad_request
  ok "đối chứng: không có ssl.server trong settings" test -z "$(setting ssl.server)"
  printf '{"email":"khong-phai-email"}' >"$TMPD/ssl4.json"
  post /api/v1/ssl/settings "$TMPD/ssl4.json"
  show '.error.code'
  ok "email sai: 400 email_invalid" err_is 400 email_invalid
  ok "đối chứng: ssl.email vẫn $EMAIL" test "$(setting ssl.email)" = "$EMAIL"

  section "6: POST /api/v1/trash/settings"
  printf '{"days":14}' >"$TMPD/trash14.json"
  post /api/v1/trash/settings "$TMPD/trash14.json"
  show
  ok "200, days 14, sites []" bash -c '[ "$1" = 200 ] && jq -e ".days == 14 and .sites == []" "$2" >/dev/null' _ "$CODE" "$TMPD/body"
  ok "đối chứng: settings trash.days = 14" test "$(setting trash.days)" = 14
  printf '{"days":400}' >"$TMPD/trash400.json"
  post /api/v1/trash/settings "$TMPD/trash400.json"
  show '.error.code'
  ok "days 400: 400 trash_days_invalid" err_is 400 trash_days_invalid
  ok "đối chứng: trash.days vẫn 14" test "$(setting trash.days)" = 14
  printf '{"days":7}' >"$TMPD/trash7.json"
  post /api/v1/trash/settings "$TMPD/trash7.json"
  ok "đặt lại 7: 200" bash -c '[ "$1" = 200 ] && jq -e ".days == 7" "$2" >/dev/null' _ "$CODE" "$TMPD/body"
  vroot python3 -c "$SQL_PY" "SELECT action, target, result FROM audit WHERE action IN ('ssl.settings', 'trash.settings') ORDER BY id"
  ok "audit ghi ssl.settings ok (2 lần), trash.settings ok và lần bị từ chối trash_days_invalid" bash -c '[ "$(grep -cE "^ssl\.settings[[:blank:]].*ok$" <<<"$1")" = 2 ] && grep -qE "^trash\.settings[[:blank:]]+14[[:blank:]]+ok$" <<<"$1" && grep -qE "^trash\.settings[[:blank:]]+400[[:blank:]]+trash_days_invalid$" <<<"$1"' _ "$OUT"
  part_end 6
}

# ================================================================ 7

# ask_body <file> <câu hỏi>: thân AskInput như ngăn AI gửi (đang ở trang site A).
ask_body() {
  jq -n --arg q "$2" --arg a "$A" '{question: $q, lang: "vi", view: {path: "/sites/\($a)", label: "Trang site \($a)"}, trail: [{path: "/", label: "Bảng điều khiển"}, {path: "/sites/\($a)", label: "Trang site \($a)"}]}' >"$1"
}
ai_counts() { sqlq 'SELECT (SELECT COUNT(*) FROM ai_sessions), (SELECT COUNT(*) FROM ai_turns)'; }

p7_ai_off() {
  part_begin "7: /ai/preview, /ai/ask khi bộ não tắt (ai.brain = off)"
  JAR="$J1"
  ask_body "$TMPD/ask.json" "Site $A có ổn không?"
  local c0
  c0="$(ai_counts)"
  post /api/v1/ai/preview "$TMPD/ask.json"
  show
  ok "POST /ai/preview: 409 ai_off" err_is 409 ai_off
  ok "lời lỗi nói cách bật: tpan mcp config --brain bridge" bash -c 'jq -r .error.message.vi "$1" | grep -qF "tpan mcp config --brain bridge"' _ "$TMPD/body"
  post /api/v1/ai/ask "$TMPD/ask.json"
  show '.error.code'
  ok "POST /ai/ask: 409 ai_off" err_is 409 ai_off
  printf '{"cau_hoi":"x"}' >"$TMPD/ask-bad.json"
  post /api/v1/ai/preview "$TMPD/ask-bad.json"
  show '.error.code'
  ok "thân có trường lạ: 400 bad_request (kiểm JSON trước khi xét AI tắt)" err_is 400 bad_request
  ok "đối chứng: ai_sessions và ai_turns không thêm dòng" test "$(ai_counts)" = "$c0"
  part_end 7
}

# ================================================================ 8

p8_fail2ban() {
  part_begin "8: POST /api/v1/security/fail2ban"
  JAR="$J1"
  dry "fail2ban" security.fail2ban /api/v1/security/fail2ban
  vexec bash -c 'dpkg-query -W -f "\${db:Status-Status}\n" fail2ban 2>&1; systemctl is-active fail2ban'
  ok "đối chứng: xem trước không cài fail2ban" hasnt '^installed$'
  job "fail2ban" /api/v1/security/fail2ban
  vroot bash -c 'systemctl is-active fail2ban; fail2ban-client status sshd'
  ok "fail2ban active, jail sshd chạy" bash -c 'grep -q "^active" <<<"$1" && grep -q "Status for the jail: sshd" <<<"$1"' _ "$OUT"
  part_end 8
}

# ================================================================ 9

p9_fixes() {
  part_begin "9: POST /api/v1/fixes (sửa tự động qua API)"
  JAR="$J1"
  local cid="service.active:php8.3-fpm"

  section "9: gây lỗi 01: dừng php8.3-fpm"
  run scripts/faults/01-php-fpm-stop.sh break "$A"
  ok "break thoát 0" rc_is 0
  vexec systemctl is-active php8.3-fpm
  ok "php8.3-fpm không active" hasnt '^active$'
  health_run
  ok "health/run: $cid fail" test "$(hr_status "$cid")" = fail
  ok "health/run: php.socket:$A fail" test "$(hr_status "php.socket:$A")" = fail
  printf '{"check_id":"khong.co:phep-kiem"}' >"$TMPD/fix-none.json"
  post '/api/v1/fixes?dry_run=1' "$TMPD/fix-none.json"
  show
  ok "check_id không có kết quả: 404 check_not_found" err_is 404 check_not_found
  printf '{"check_id":"%s"}' "$cid" >"$TMPD/fix1.json"
  dry "sửa $cid" fix. /api/v1/fixes "$TMPD/fix1.json"
  vexec systemctl is-active php8.3-fpm
  ok "đối chứng: xem trước không khởi động php8.3-fpm" hasnt '^active$'
  job "sửa $cid" /api/v1/fixes "$TMPD/fix1.json"
  vexec systemctl is-active php8.3-fpm
  ok "php8.3-fpm active lại" out_is active
  site_200 "$A"
  site_200 "$B"
  health_run
  ok "health/run: $cid pass" test "$(hr_status "$cid")" = pass
  ok "health/run: mọi phép kiểm đạt" hr_all_pass

  section "9: gây lỗi 07: gỡ symlink vhost của $B"
  local link="/etc/nginx/sites-enabled/tpan-$B.conf"
  cid="web.vhost:$B"
  run scripts/faults/07-vhost-disable.sh break "$B"
  ok "break thoát 0" rc_is 0
  vexec bash -c 'ls -la "$1" 2>&1' _ "$link"
  ok "symlink đã gỡ" hasF 'No such file'
  health_run
  ok "health/run: $cid fail" test "$(hr_status "$cid")" = fail
  printf '{"check_id":"%s"}' "$cid" >"$TMPD/fix2.json"
  dry "sửa $cid" fix. /api/v1/fixes "$TMPD/fix2.json"
  vexec bash -c 'ls -la "$1" 2>&1' _ "$link"
  ok "đối chứng: xem trước không tạo lại symlink" hasF 'No such file'
  job "sửa $cid" /api/v1/fixes "$TMPD/fix2.json"
  vexec readlink -f "$link"
  ok "symlink có lại, trỏ tới sites-available" out_is "/etc/nginx/sites-available/tpan-$B.conf"
  site_200 "$B"
  site_200 "$A"
  health_run
  ok "health/run: $cid pass, mọi phép kiểm đạt" bash -c '[ "$1" = pass ]' _ "$(hr_status "$cid")"
  ok "health/run: mọi phép kiểm đạt" hr_all_pass
  vroot python3 -c "$SQL_PY" "SELECT action, target, result FROM audit WHERE action LIKE 'fix.%' ORDER BY id"
  ok "audit có hai lần sửa ok" bash -c '[ "$(grep -cE "[[:blank:]]ok$" <<<"$1")" -ge 2 ]' _ "$OUT"
  part_end 9
}

# ================================================================ 10

UA=""
p10_backups() {
  part_begin "10: backup và khôi phục qua API, lịch backup"
  JAR="$J1"
  UA="$(user_of "$A")"
  local mark1 mark2 bid bpath bsha nfiles

  section "10: dấu trước backup (file và DB của $A)"
  mark1="e2e-api-truoc-backup-$(date +%s)"
  vroot bash -c 'printf "%s\n" "$3" | runuser -u "$2" -- tee "/var/www/$1/public/e2e-api.txt" >/dev/null && chmod 640 "/var/www/$1/public/e2e-api.txt"; cd / && runuser -u "$2" -- wp post update 1 --post_title="e2e-api tieu de luc backup" --path="/var/www/$1/public"' _ "$A" "$UA" "$mark1"
  ok "ghi file dấu và tiêu đề bài 1" rc_is 0

  section "10: POST /api/v1/sites/$A/backups"
  vroot bash -c 'ls "/var/lib/tpan/backups/$1" 2>/dev/null | wc -l' _ "$A"
  nfiles="$OUT"
  dry "backup $A" backup.create "/api/v1/sites/$A/backups"
  vroot bash -c 'ls "/var/lib/tpan/backups/$1" 2>/dev/null | wc -l' _ "$A"
  ok "đối chứng: xem trước không tạo file backup ($nfiles file)" out_is "$nfiles"
  job "backup $A" "/api/v1/sites/$A/backups"
  backups_of "$A"
  ok "GET /backups?domain=$A: 1 bản full manual" blq '[.items[] | select(.kind == "full" and .reason == "manual")] | length == 1'
  bid="$(blr '[.items[] | select(.kind == "full" and .reason == "manual")][0].id')"
  bpath="$(blr '[.items[] | select(.kind == "full" and .reason == "manual")][0].path')"
  bsha="$(blr '[.items[] | select(.kind == "full" and .reason == "manual")][0].sha256')"
  vroot bash -c 'sha256sum "$1"; stat -c "%a %U:%G %n" "$1"' _ "$bpath"
  ok "đối chứng: file có, sha256sum khớp API, 600 root" bash -c 'grep -qx "$2  $3" <<<"$1" && grep -qx "600 root:root $3" <<<"$1"' _ "$OUT" "$bsha" "$bpath"

  section "10: sửa nội dung sau backup"
  mark2="e2e-api-sau-backup-$(date +%s)"
  vroot bash -c 'printf "%s\n" "$3" | runuser -u "$2" -- tee "/var/www/$1/public/e2e-api.txt" >/dev/null; cd / && runuser -u "$2" -- wp post update 1 --post_title="e2e-api tieu de da sua" --path="/var/www/$1/public"' _ "$A" "$UA" "$mark2"
  vexec curl -sS -m 20 --resolve "$A:80:127.0.0.1" "http://$A/e2e-api.txt"
  ok "site trả nội dung đã sửa" out_is "$mark2"

  section "10: POST /api/v1/backups/{id}/restore"
  apiw POST "/api/v1/backups/khong-hop-le/restore?dry_run=1"
  show '.error.code'
  ok "id sai dạng: 404 backup_not_found" err_is 404 backup_not_found
  apiw POST "/api/v1/backups/0123456789abcdef/restore?dry_run=1"
  show '.error.code'
  ok "id không có: 404 backup_not_found" err_is 404 backup_not_found
  dry "khôi phục $bid" backup.restore "/api/v1/backups/$bid/restore"
  ok "bản xem trước nhắm đúng $A" pv --arg a "$A" '.target == $a'
  vexec curl -sS -m 20 --resolve "$A:80:127.0.0.1" "http://$A/e2e-api.txt"
  ok "đối chứng: xem trước không khôi phục (file vẫn bản đã sửa)" out_is "$mark2"
  job "khôi phục $bid" "/api/v1/backups/$bid/restore"
  vexec curl -sS -m 20 --resolve "$A:80:127.0.0.1" "http://$A/e2e-api.txt"
  ok "file về như lúc backup (qua HTTP)" out_is "$mark1"
  vroot mariadb -N -B "$UA" -e "SELECT post_title FROM wp_posts WHERE ID = 1"
  ok "DB về như lúc backup" out_is 'e2e-api tieu de luc backup'
  site_200 "$A"
  backups_of "$A"
  ok "có thêm bản before-restore, bản cũ còn với sha256 cũ" blq --arg id "$bid" --arg s "$bsha" '([.items[] | select(.reason == "before-restore")] | length == 1) and ([.items[] | select(.id == $id and .sha256 == $s)] | length == 1)'

  section "10: POST /api/v1/backups/settings"
  local cf=/etc/cron.d/tpan-backup
  printf '{"keep":3,"at":"25:00"}' >"$TMPD/bs-bad.json"
  post '/api/v1/backups/settings?dry_run=1' "$TMPD/bs-bad.json"
  show '.error.code'
  ok "at 25:00: 400 backup_at_invalid" err_is 400 backup_at_invalid
  printf '{"keep":0,"at":""}' >"$TMPD/bs-bad2.json"
  post '/api/v1/backups/settings?dry_run=1' "$TMPD/bs-bad2.json"
  show '.error.code'
  ok "keep 0: 400 backup_keep_invalid" err_is 400 backup_keep_invalid
  printf '{"keep":3,"at":"03:30"}' >"$TMPD/bs-on.json"
  dry "lịch backup 03:30" backup.settings /api/v1/backups/settings "$TMPD/bs-on.json"
  vexec bash -c 'ls -la "$1" 2>&1' _ "$cf"
  ok "đối chứng: xem trước không ghi $cf" hasF 'No such file'
  job "lịch backup 03:30" /api/v1/backups/settings "$TMPD/bs-on.json"
  vroot bash -c 'stat -c "%U:%G %a %n" "$1"; grep -v "^#" "$1"' _ "$cf"
  ok "$cf root:root 644, chạy 30 03 * * * tpan backup create --all (giờ 03 hay 3 cron hiểu như nhau)" bash -c 'grep -qx "root:root 644 $2" <<<"$1" && grep -qE "^30 0?3 \* \* \* root .*tpan backup create --all" <<<"$1"' _ "$OUT" "$cf"
  api GET /api/v1/backups
  show '.settings'
  ok "GET /backups: settings keep 3, at 03:30" bj '.settings == {"keep": 3, "at": "03:30"}'
  printf '{"keep":3,"at":""}' >"$TMPD/bs-off.json"
  job "tắt lịch backup" /api/v1/backups/settings "$TMPD/bs-off.json"
  vexec bash -c 'ls -la "$1" 2>&1' _ "$cf"
  ok "$cf đã gỡ" hasF 'No such file'
  ok "đối chứng: settings backup.at rỗng, backup.keep 3" bash -c '[ -z "$1" ] && [ "$2" = 3 ]' _ "$(setting backup.at)" "$(setting backup.keep)"
  part_end 10
}

# ================================================================ 11

p11_db() {
  part_begin "11: thao tác DB của $A qua API"
  JAR="$J1"
  UA="$(user_of "$A")"
  local old new sid spath i code

  section "11: POST /api/v1/databases/$A/password"
  old="$(dbpw "$A")"
  add_secret "$old"
  ok "đọc được mật khẩu DB cũ (32 ký tự, đã che)" test "${#old}" = 32
  dry "đổi mật khẩu DB $A" db.passwd "/api/v1/databases/$A/password"
  ok "đối chứng: xem trước không đổi mật khẩu trong wp-config.php" test "$(dbpw "$A")" = "$old"
  job "đổi mật khẩu DB $A" "/api/v1/databases/$A/password"
  new="$(dbpw "$A")"
  add_secret "$new"
  ok "mật khẩu mới 32 ký tự, khác mật khẩu cũ" bash -c '[ "${#1}" = 32 ] && [ "$1" != "$2" ]' _ "$new" "$old"
  ok "mật khẩu mới không có trong luồng SSE và GET /jobs của job" bash -c '! grep -qF -- "$1" "$2" && ! grep -qF -- "$1" "$3"' _ "$new" "$TMPD/sse-$JID" "$TMPD/body"
  for i in $(seq 1 15); do
    code="$("$VM" exec curl -sS -m 10 -o /dev/null -w '%{http_code}' --resolve "$A:80:127.0.0.1" "http://$A/" </dev/null | tr -d '\r')"
    [ "$code" = 200 ] && break
    sleep 1
  done
  printf 'site %s sau đổi mật khẩu: %s (lần thử %s)\n' "$A" "$code" "$i"
  ok "$A trả 200 sau đổi mật khẩu" test "$code" = 200
  vroot bash -c 'cd / && pw="$(runuser -u "$2" -- wp config get DB_PASSWORD --path="/var/www/$1/public")"; MYSQL_PWD="$pw" mariadb -u "$2" -N -e "SELECT CURRENT_USER()"' _ "$A" "$UA"
  ok "đối chứng: user DB đăng nhập được bằng mật khẩu trong wp-config.php" out_is "$UA@localhost"

  section "11: POST /api/v1/databases/$A/dump"
  dry "xuất DB $A" db.dump "/api/v1/databases/$A/dump"
  backups_of "$A"
  ok "đối chứng: xem trước chưa có bản sql nào" blq '[.items[] | select(.kind == "sql")] | length == 0'
  sleep 1
  job "xuất DB $A" "/api/v1/databases/$A/dump"
  backups_of "$A"
  ok "có 1 bản sql manual" blq '[.items[] | select(.kind == "sql" and .reason == "manual")] | length == 1'
  sid="$(blr '[.items[] | select(.kind == "sql" and .reason == "manual")][0].id')"
  spath="$(blr '[.items[] | select(.kind == "sql" and .reason == "manual")][0].path')"
  vroot bash -c 'sha256sum "$1"; stat -c "%a %U:%G %n" "$1"; tail -1 "$1"' _ "$spath"
  ok "đối chứng: file .sql 0600 root, sha256 khớp, dòng cuối Dump completed" bash -c 'grep -qx "$2  $3" <<<"$1" && grep -qx "600 root:root $3" <<<"$1" && grep -q "^-- Dump completed" <<<"$1"' _ "$OUT" "$(blr '[.items[] | select(.kind == "sql" and .reason == "manual")][0].sha256')" "$spath"

  section "11: sửa DB rồi POST /api/v1/databases/$A/import"
  vroot mariadb -N -B "$UA" -e "UPDATE wp_posts SET post_title = 'e2e-api sua sau dump' WHERE ID = 1; INSERT INTO wp_options (option_name, option_value, autoload) VALUES ('e2e_api_sau_dump', '1', 'no'); SELECT post_title FROM wp_posts WHERE ID = 1"
  ok "DB đã sửa sau khi xuất" out_is 'e2e-api sua sau dump'
  printf '{"backup_id":"0123456789abcdef"}' >"$TMPD/imp-bad.json"
  post "/api/v1/databases/$A/import?dry_run=1" "$TMPD/imp-bad.json"
  show '.error.code'
  ok "backup_id không có: 404 backup_not_found" err_is 404 backup_not_found
  jq -n --arg id "$sid" '{backup_id: $id}' >"$TMPD/imp.json"
  dry "nhập DB $A" db.import "/api/v1/databases/$A/import" "$TMPD/imp.json"
  vroot mariadb -N -B "$UA" -e "SELECT post_title FROM wp_posts WHERE ID = 1"
  ok "đối chứng: xem trước không nhập (DB vẫn bản đã sửa)" out_is 'e2e-api sua sau dump'
  sleep 1
  job "nhập DB $A" "/api/v1/databases/$A/import" "$TMPD/imp.json"
  vroot mariadb -N -B "$UA" -e "SELECT post_title FROM wp_posts WHERE ID = 1; SELECT COUNT(*) FROM wp_options WHERE option_name = 'e2e_api_sau_dump'"
  ok "DB về như lúc xuất (tiêu đề cũ, dòng thêm sau xuất không còn)" out_is "$(printf 'e2e-api tieu de luc backup\n0')"
  backups_of "$A"
  ok "có bản before-import (sql)" blq '[.items[] | select(.kind == "sql" and .reason == "before-import")] | length == 1'
  site_200 "$A"
  part_end 11
}

# ================================================================ 12

# mcp_raw <tên> <dòng JSON-RPC>...: một phiên `tpan mcp` (stdio) trong máy ảo,
# gửi tay các dòng JSON-RPC qua stdin của scripts/vm.sh root (multipass exec
# chuyển stdin). stdin phải còn mở tới khi server trả lời xong (đóng stdin là
# client ngắt kết nối, server huỷ request đang chạy): giữ mở 20 giây như
# scripts/e2e-m7.sh. Đây là chỗ duy nhất lệnh multipass exec có stdin khác
# /dev/null.
mcp_raw() {
  local f="$TMPD/mcp-$1"
  shift
  printf '%s\n' "$@" >"$f.in"
  printf '\n[%s] $ { %s dòng JSON-RPC (initialize, initialized, tools/call...); sleep 20; } | %s root tpan mcp\n' "$(ts)" "$#" "$VM"
  { cat "$f.in"; sleep 20; } | "$VM" root tpan mcp >"$f.out" 2>"$f.err"
  RC=$?
  OUT="$(jq -c '{id, error, server: .result.serverInfo.name, is_error: .result.isError, text: (.result.content[0].text // "" | .[0:300])}' "$f.out" 2>&1)"
  mask
  printf '%s\n' "$OUT"
  if [ -s "$f.err" ]; then
    OUT="$(tr -d '\r' <"$f.err")"
    mask
    printf -- '--- stderr:\n%s\n' "$OUT"
  fi
  printf '[%s] (mã thoát %s)\n' "$(ts)" "$RC"
  OUT="$(cat "$f.out")"
}

p12_plans_ai() {
  part_begin "12: Plan từ tpan mcp, duyệt và từ chối qua API; phiên AI"
  JAR="$J1"
  local pc pd msid psid c0 c1

  section "12: tpan mcp (stdio) đề xuất tạo $C và $D"
  mcp_raw plans \
    '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"tpan-e2e-api","version":"1"}}}' \
    '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
    "$(jq -nc --arg d "$C" --arg e "$EMAIL" '{jsonrpc: "2.0", id: 2, method: "tools/call", params: {name: "propose_site_add", arguments: {domain: $d, email: $e, title: "Site C"}}}')" \
    "$(jq -nc --arg d "$D" --arg e "$EMAIL" '{jsonrpc: "2.0", id: 3, method: "tools/call", params: {name: "propose_site_add", arguments: {domain: $d, email: $e, title: "Site D"}}}')"
  ok "tpan mcp thoát 0, trả lời initialize và hai lần gọi, không isError" bash -c '[ "$2" = 0 ] && [ "$(jq -s "[.[] | select(.id == 2 or .id == 3) | select(.result != null and .result.isError != true)] | length" <<<"$1")" = 2 ]' _ "$OUT" "$RC"

  section "12: GET /api/v1/plans?status=pending"
  api GET '/api/v1/plans?status=pending'
  show '[.[] | {id, kind, target, origin, status, untrusted, steps: (.preview.steps | length)}]'
  pc="$(jq -r --arg d "$C" '[.[] | select(.target == $d)][0].id // empty' "$TMPD/body")"
  pd="$(jq -r --arg d "$D" '[.[] | select(.target == $d)][0].id // empty' "$TMPD/body")"
  ok "2 Plan site.add chờ duyệt cho $C và $D, origin mcp, có bản xem trước" bash -c '[ "$1" = 200 ] && [ -n "$3" ] && [ -n "$4" ] && jq -e "length == 2 and all(.[]; .kind == \"site.add\" and .origin == \"mcp\" and .status == \"pending\" and (.preview.steps | length) > 0)" "$2" >/dev/null' _ "$CODE" "$TMPD/body" "$pc" "$pd"
  api GET '/api/v1/plans?status=xyz'
  ok "status lạ: 400 status_invalid" err_is 400 status_invalid
  vexec bash -c 'ls /var/www; id "$1" 2>&1; id "$2" 2>&1' _ "$(user_of "$C")" "$(user_of "$D")"
  ok "đối chứng: đề xuất không tạo gì trên máy" bash -c '! grep -qxF "$2" <<<"$1" && ! grep -qxF "$3" <<<"$1" && [ "$(grep -c "no such user" <<<"$1")" = 2 ]' _ "$OUT" "$C" "$D"

  section "12: POST /api/v1/plans/$pc/approve"
  api POST "/api/v1/plans/$pc/approve" -H 'Sec-Fetch-Site: cross-site'
  show '.error.code'
  ok "duyệt từ trang khác: 403 csrf" err_is 403 csrf
  api GET "/api/v1/plans/$pc"
  ok "Plan $pc vẫn pending" bj '.status == "pending"'
  job "duyệt Plan tạo $C" "/api/v1/plans/$pc/approve"
  local apj=$JID
  ok "GET /jobs lần đầu có mật khẩu quản trị WordPress của $C (đã che)" test "$TOOK" = 1
  api GET "/api/v1/jobs/$apj"
  take_secrets
  ok "GET /jobs lần hai: không còn mật khẩu" bash -c '[ "$1" = 0 ] && jq -e "has(\"secret\") | not" "$2" >/dev/null' _ "$TOOK" "$TMPD/body"
  add_dbpw "$C"
  site_200 "$C"
  vexec bash -c 'id "$1"; ls -ld "/var/www/$2"' _ "$(user_of "$C")" "$C"
  ok "đối chứng: user Linux và thư mục của $C có" rc_is 0
  api GET "/api/v1/plans/$pc"
  show '{id, status, decided_by, job_id}'
  ok "Plan $pc: applied, decided_by web:<ip>, job_id là job vừa chạy" bash -c 'jq -e --arg j "$2" ".status == \"applied\" and (.decided_by | startswith(\"web:\")) and .job_id == \$j" "$1" >/dev/null' _ "$TMPD/body" "$apj"
  apiw POST "/api/v1/plans/$pc/approve"
  show '.error.code'
  ok "duyệt lần hai: 409 plan_not_pending" err_is 409 plan_not_pending

  section "12: POST /api/v1/plans/$pd/reject"
  apiw POST "/api/v1/plans/$pd/reject"
  ok "từ chối: 204" code_is 204
  api GET "/api/v1/plans/$pd"
  show '{id, status, decided_by, job_id}'
  ok "Plan $pd: rejected, decided_by web:<ip>, không có job" bj '.status == "rejected" and (.decided_by | startswith("web:")) and ((.job_id // "") == "")'
  apiw POST "/api/v1/plans/$pd/reject"
  show '.error.code'
  ok "từ chối lần hai: 409 plan_not_pending" err_is 409 plan_not_pending
  apiw POST "/api/v1/plans/$pd/approve"
  show '.error.code'
  ok "duyệt Plan đã từ chối: 409 plan_not_pending" err_is 409 plan_not_pending
  apiw POST /api/v1/plans/khong-co/reject
  show '.error.code'
  ok "id sai: 404 plan_not_found" err_is 404 plan_not_found
  vexec bash -c 'ls /var/www; id "$1" 2>&1' _ "$(user_of "$D")"
  ok "đối chứng: $D không được tạo" bash -c '! grep -qxF "$2" <<<"$1" && grep -q "no such user" <<<"$1"' _ "$OUT" "$D"
  vroot python3 -c "$SQL_PY" "SELECT action, target, result FROM audit WHERE action LIKE 'plan.%' ORDER BY id"
  ok "audit: plan.approve ok, plan.reject ok, các lần bị từ chối plan_not_pending" bash -c 'grep -qx "$(printf "plan.approve\t%s\tok" "$2")" <<<"$1" && grep -qx "$(printf "plan.reject\t%s\tok" "$3")" <<<"$1" && grep -q "plan_not_pending$" <<<"$1"' _ "$OUT" "$pc" "$pd"

  section "12: GET /api/v1/ai/sessions, /ai/sessions/{id} (phiên của tpan mcp)"
  api GET /api/v1/ai/sessions
  show '[.[] | {id, entry, client, turns, calls, plans}]'
  msid="$(jq -r '[.[] | select(.entry == "mcp")][0].id // empty' "$TMPD/body")"
  ok "1 phiên entry mcp, client tpan-e2e-api, 2 lần gọi, 2 Plan, 0 lượt" bash -c '[ "$1" = 200 ] && jq -e "[.[] | select(.entry == \"mcp\" and (.client | startswith(\"tpan-e2e-api\")) and .calls == 2 and .plans == 2 and .turns == 0)] | length == 1" "$2" >/dev/null' _ "$CODE" "$TMPD/body"
  api GET "/api/v1/ai/sessions/$msid"
  show '{session: (.session | {id, entry, untrusted}), turns: (.turns | length), calls: [.calls[] | {seq, tool, plan_id, error}], plans: [.plans[] | {id, target, status}]}'
  ok "chi tiết: 2 lần gọi propose_site_add có plan_id $pc, $pd; Plan applied và rejected" bash -c '[ "$1" = 200 ] && jq -e --arg c "$3" --arg d "$4" "([.calls[] | select(.tool == \"propose_site_add\") | .plan_id] | sort) == ([\$c, \$d] | sort) and ([.plans[] | .status] | sort) == [\"applied\", \"rejected\"] and (.turns | length) == 0" "$2" >/dev/null' _ "$CODE" "$TMPD/body" "$pc" "$pd"
  api GET "/api/v1/ai/sessions/$msid/turns/1/events"
  show
  ok "phiên mcp không có lượt hỏi: luồng lượt 1 là 404 ai_turn_not_found" err_is 404 ai_turn_not_found

  section "12: bật bộ não (ai.brain = bridge) nhưng không nối tpan brain nào"
  printf '(Không chạy claude, không chạy tpan brain: chỉ mở socket bộ não. Lượt hỏi được ghi rồi bị từ chối brain_offline, đủ để đọc lại qua API.)\n'
  vroot tpan mcp config --brain bridge
  ok "mcp config --brain bridge thoát 0" rc_is 0
  restart_tpan
  vroot bash -c 'for i in $(seq 1 20); do [ -S /run/tpan/brain.sock ] && break; sleep 0.5; done; systemctl is-active tpan; stat -c "%A %U:%G %n" /run/tpan/brain.sock'
  ok "tpan active, /run/tpan/brain.sock là socket 0600 của root" bash -c 'grep -qx active <<<"$1" && grep -qE "^srw------- root:root /run/tpan/brain.sock$" <<<"$1"' _ "$OUT"
  api GET /api/v1/session
  ok "phiên đăng nhập còn sau khi khởi động lại" bj '.logged_in == true'
  api GET /api/v1/ai/brain
  show
  ok "GET /ai/brain: mode bridge, chưa nối" bj '.mode == "bridge" and (.connected | not)'

  section "12: POST /api/v1/ai/preview (thứ sẽ gửi, không ghi gì)"
  c0="$(ai_counts)"
  post /api/v1/ai/preview "$TMPD/ask.json"
  show '{session_id, question, playbook, chips: [.chips[] | {key, omitted, content: (.content | .[0:200])}], prompt_chars: (.prompt | length)}'
  ok "200, có câu hỏi, có nhãn view, prompt khác rỗng" bash -c '[ "$1" = 200 ] && jq -e "(.question | length) > 0 and ([.chips[].key] | index(\"view\") != null) and (.prompt | length) > 0" "$2" >/dev/null' _ "$CODE" "$TMPD/body"
  ok "prompt và câu hỏi đã che: không có $A, IP máy ảo" bash -c '! jq -r ".prompt, .question" "$1" | grep -qF -- "$2" && ! jq -r .prompt "$1" | grep -qF -- "$3"' _ "$TMPD/body" "$A" "$VM_IP"
  c1="$(ai_counts)"
  ok "đối chứng: xem trước không ghi ai_sessions, ai_turns ($c0 -> $c1)" test "$c0" = "$c1"

  section "12: POST /api/v1/ai/ask khi chưa có bộ não nào nối"
  post /api/v1/ai/ask "$TMPD/ask.json"
  show
  ok "409 brain_offline, lời lỗi chỉ lệnh tpan brain" bash -c '[ "$1" = 409 ] && jq -e ".error.code == \"brain_offline\" and (.error.message.vi | contains(\"tpan brain\"))" "$2" >/dev/null' _ "$CODE" "$TMPD/body"
  vroot python3 -c "$SQL_PY" 'SELECT s.entry, t.seq, t.status, t.error FROM ai_turns t JOIN ai_sessions s ON s.id = t.session_id'
  ok "đối chứng: một lượt của phiên panel, status error (bộ não chưa nối)" bash -c '[ "$(grep -c . <<<"$1")" = 1 ] && grep -qE "^panel[[:blank:]]+1[[:blank:]]+error" <<<"$1"' _ "$OUT"
  api GET /api/v1/ai/sessions
  show '[.[] | {id, entry, turns, calls, plans}]'
  psid="$(jq -r '[.[] | select(.entry == "panel")][0].id // empty' "$TMPD/body")"
  ok "GET /ai/sessions: thêm 1 phiên panel có 1 lượt" bash -c '[ -n "$1" ] && jq -e "[.[] | select(.entry == \"panel\" and .turns == 1)] | length == 1" "$2" >/dev/null' _ "$psid" "$TMPD/body"
  api GET "/api/v1/ai/sessions/$psid"
  show '{session: (.session | {id, entry}), turns: [.turns[] | {seq, status, error, events: [.events[] | .kind], prompt_chars: (.prompt | length)}]}'
  ok "chi tiết: lượt 1 status error, sự kiện cuối là error" bash -c '[ "$1" = 200 ] && jq -e ".turns | length == 1 and .[0].seq == 1 and .[0].status == \"error\" and (.[0].events[-1].kind == \"error\")" "$2" >/dev/null' _ "$CODE" "$TMPD/body"

  section "12: GET /api/v1/ai/sessions/{id}/turns/1/events (SSE của lượt đã kết thúc)"
  local f="$TMPD/ai-sse" rc
  printf '\n[%s] $ curl -sS -N -m 30 --cacert ... -b <cookie> %s/api/v1/ai/sessions/%s/turns/1/events\n' "$(ts)" '<vm>' "$psid"
  curl -sS -N -m 30 --cacert "$TMPD/cert.pem" -b "$JAR" -D "$f.hdr" "$BASE/api/v1/ai/sessions/$psid/turns/1/events" >"$f" 2>"$f.err"
  rc=$?
  OUT="$(cat "$f.hdr" "$f")"
  mask
  printf '%s\n[%s] (mã thoát %s)\n' "$OUT" "$(ts)" "$rc"
  ok "server tự đóng luồng sau sự kiện kết thúc (curl thoát 0, không hết giờ)" test "$rc" = 0
  ok "text/event-stream, sự kiện tên ai, đúng một sự kiện kind error" bash -c 'grep -qi "^content-type: text/event-stream" "$1.hdr" && [ "$(grep -c "^event: ai$" "$1")" = 1 ] && sed -n "s/^data: //p" "$1" | jq -e -s "length == 1 and .[0].kind == \"error\"" >/dev/null' _ "$f"
  api GET "/api/v1/ai/sessions/$psid/turns/2/events"
  show '.error.code'
  ok "lượt 2 không có: 404 ai_turn_not_found" err_is 404 ai_turn_not_found
  part_end 12
}

# ================================================================ 13

p13_php() {
  part_begin "13: PHP 8.4: cài, đổi site $A, gỡ"
  JAR="$J1"
  local slug u
  slug="$(slug_of "$A")"
  u="$(user_of "$A")"
  printf '{"version":"8.4"}' >"$TMPD/php84.json"
  printf '{"version":"8.3"}' >"$TMPD/php83.json"
  printf '{"version":"8"}' >"$TMPD/php8.json"

  section "13: POST /api/v1/php/install"
  post '/api/v1/php/install?dry_run=1' "$TMPD/php8.json"
  show '.error.code'
  ok "bản sai dạng: 409 php_version_invalid" err_is 409 php_version_invalid
  dry "cài PHP 8.4" php.install /api/v1/php/install "$TMPD/php84.json"
  ok "xem trước có bước thêm kho sury (apt.repo:sury)" pv 'any(.steps[]; .info.id | startswith("apt.repo:sury"))'
  vexec bash -c 'ls /etc/apt/sources.list.d/ /etc/apt/keyrings/ 2>&1; dpkg-query -W -f "\${Package} \${db:Status-Status}\n" php8.4-fpm 2>&1'
  ok "đối chứng: xem trước không thêm kho, không cài php8.4-fpm" bash -c '! grep -q tpan-sury <<<"$1" && ! grep -q "^php8.4-fpm installed" <<<"$1"' _ "$OUT"
  job "cài PHP 8.4" /api/v1/php/install "$TMPD/php84.json"
  vexec bash -c 'dpkg-query -W -f "\${Package} \${db:Status-Status}\n" php8.4-fpm; systemctl is-active php8.4-fpm; ls /etc/apt/sources.list.d/; ls /etc/php/8.4/fpm/pool.d'
  ok "php8.4-fpm installed, active, có tpan-sury.sources, pool.d chỉ tpan-idle.conf" bash -c 'grep -q "^php8.4-fpm installed" <<<"$1" && grep -qx active <<<"$1" && grep -q "tpan-sury.sources" <<<"$1" && grep -qx "tpan-idle.conf" <<<"$1" && ! grep -q "www.conf" <<<"$1"' _ "$OUT"
  api GET /api/v1/php
  show '{sury_enabled, versions: [.versions[] | select(.installed) | {version, default, sites}]}'
  ok "GET /php: sury_enabled, 8.4 installed, chưa site nào dùng" bj '.sury_enabled and ([.versions[] | select(.version == "8.4" and .installed and (.sites // []) == [] and (.default | not))] | length == 1)'

  section "13: POST /api/v1/sites/$A/php"
  dry "đổi $A sang 8.4" php.use "/api/v1/sites/$A/php" "$TMPD/php84.json"
  vexec bash -c 'ls /etc/php/8.3/fpm/pool.d/tpan-$1.conf /etc/php/8.4/fpm/pool.d/tpan-$1.conf 2>&1' _ "$slug"
  ok "đối chứng: xem trước không chuyển pool (file pool của $A vẫn ở 8.3, chưa có ở 8.4)" bash -c 'grep -qx "/etc/php/8.3/fpm/pool.d/tpan-$2.conf" <<<"$1" && grep -q "/etc/php/8.4/fpm/pool.d/tpan-$2.conf.*No such file" <<<"$1"' _ "$OUT" "$slug"
  job "đổi $A sang 8.4" "/api/v1/sites/$A/php" "$TMPD/php84.json"
  site_200 "$A"
  site_200 "$B"
  vroot python3 -c "$PROC_PY"
  ok "pool tpan-$slug chạy dưới $u, master 8.4" grep -qxF "pool tpan-$slug user $u master /etc/php/8.4/fpm/php-fpm.conf" <<<"$OUT"
  vexec bash -c 'ls /run/php/; ls /etc/php/8.3/fpm/pool.d/tpan-$1.conf /etc/php/8.4/fpm/pool.d/tpan-$1.conf 2>&1' _ "$slug"
  ok "socket tpan-$slug-8.4.sock, file pool chỉ ở 8.4" bash -c 'grep -qx "tpan-$2-8.4.sock" <<<"$1" && ! grep -qx "tpan-$2-8.3.sock" <<<"$1" && grep -q "/etc/php/8.3/fpm/pool.d/tpan-$2.conf.*No such file" <<<"$1"' _ "$OUT" "$slug"
  api GET /api/v1/php
  ok "GET /php: 8.4 có site $A, 8.3 còn $B" bj --arg a "$A" --arg b "$B" '([.versions[] | select(.version == "8.4") | .sites][0] == [$a]) and ([.versions[] | select(.version == "8.3") | .sites][0] | index($b) != null and index($a) == null)'
  post "/api/v1/sites/$A/php?dry_run=1" "$TMPD/php84.json"
  show '.error.code'
  ok "đổi lại đúng bản đang dùng: 409 php_same_version" err_is 409 php_same_version
  post '/api/v1/php/remove?dry_run=1' "$TMPD/php84.json"
  show '{code: .error.code, checks: [.error.checks[]? | .id]}'
  ok "gỡ 8.4 khi $A còn dùng: 409 php_in_use" err_is 409 php_in_use
  job "đổi $A về 8.3" "/api/v1/sites/$A/php" "$TMPD/php83.json"
  site_200 "$A"
  vroot python3 -c "$PROC_PY"
  ok "pool tpan-$slug lại dưới master 8.3" grep -qxF "pool tpan-$slug user $u master /etc/php/8.3/fpm/php-fpm.conf" <<<"$OUT"

  section "13: POST /api/v1/php/remove"
  dry "gỡ PHP 8.4" php.remove /api/v1/php/remove "$TMPD/php84.json"
  vexec dpkg-query -W -f '${Package} ${db:Status-Status}\n' php8.4-fpm
  ok "đối chứng: xem trước không gỡ" has '^php8\.4-fpm installed'
  job "gỡ PHP 8.4" /api/v1/php/remove "$TMPD/php84.json"
  vexec bash -c 'dpkg-query -W -f "\${Package} \${db:Status-Status}\n" "php8.4*" 2>&1; ps -eo cmd | grep "[p]hp-fpm: master"'
  ok "không gói php8.4-* nào còn installed, không còn master 8.4" bash -c '! grep -qE "^php8\.4[^ ]* installed" <<<"$1" && ! grep -q "/etc/php/8.4/" <<<"$1"' _ "$OUT"
  api GET /api/v1/php
  ok "GET /php: chỉ 8.3 installed" bj '[.versions[] | select(.installed) | .version] == ["8.3"]'
  site_200 "$A"
  site_200 "$B"
  part_end 13
}

# ================================================================ 14

sites_200() {
  local d
  for d in "$A" "$B" "$C"; do site_200 "$d"; done
}

p14_switch() {
  part_begin "14: POST /api/v1/stack/switch nginx -> apache -> nginx"
  JAR="$J1"
  printf '{"to":"apache"}' >"$TMPD/to-apache.json"
  printf '{"to":"nginx"}' >"$TMPD/to-nginx.json"
  printf '{"to":"lighttpd"}' >"$TMPD/to-bad.json"
  post '/api/v1/stack/switch?dry_run=1' "$TMPD/to-nginx.json"
  show '.error.code'
  ok "sang đúng web server đang chạy: 409 stack_same_web" err_is 409 stack_same_web
  post '/api/v1/stack/switch?dry_run=1' "$TMPD/to-bad.json"
  show '.error.code'
  ok "web server lạ: 409 web_invalid" err_is 409 web_invalid
  dry "chuyển sang apache" stack.switch /api/v1/stack/switch "$TMPD/to-apache.json"
  ok "xem trước có bước web.swap" pv 'any(.steps[]; .info.id | startswith("web.swap:"))'
  vexec bash -c 'dpkg-query -W -f "\${Package} \${db:Status-Status}\n" apache2 2>&1; systemctl is-active nginx'
  ok "đối chứng: xem trước không cài apache2, nginx vẫn active" bash -c '! grep -q "^apache2 installed" <<<"$1" && grep -qx active <<<"$1"' _ "$OUT"
  job "chuyển sang apache" /api/v1/stack/switch "$TMPD/to-apache.json"
  vexec bash -c 'echo "apache2 $(systemctl is-active apache2)"; echo "nginx $(systemctl is-active nginx)"; ss -tlnp 2>/dev/null | grep -E ":80 " | head -3'
  ok "apache2 active, nginx không" bash -c 'grep -qx "apache2 active" <<<"$1" && ! grep -qx "nginx active" <<<"$1"' _ "$OUT"
  sites_200
  api GET /api/v1/stack
  show '{config, services: [.services[] | "\(.status) \(.id)"]}'
  ok "GET /stack: web apache, mọi dịch vụ pass" bj '.config.web == "apache" and all(.services[]; .status == "pass")'
  job "chuyển về nginx" /api/v1/stack/switch "$TMPD/to-nginx.json"
  vexec bash -c 'echo "apache2 $(systemctl is-active apache2)"; echo "nginx $(systemctl is-active nginx)"'
  ok "nginx active, apache2 không" bash -c 'grep -qx "nginx active" <<<"$1" && ! grep -qx "apache2 active" <<<"$1"' _ "$OUT"
  sites_200
  api GET /api/v1/stack
  ok "GET /stack: web nginx, mọi dịch vụ pass" bj '.config.web == "nginx" and all(.services[]; .status == "pass")'
  part_end 14
}

# ================================================================ 15

p15_updates() {
  part_begin "15: POST /api/v1/updates (áp dụng), /api/v1/updates/auto"
  JAR="$J1"
  local old new pkgs

  section "15: (đồ nghề test, chỉ trong máy ảo) hạ bộ php8.3-* về bản của kho release"
  vexec apt-cache madison php8.3-fpm
  old="$(awk -F'|' '$3 ~ /ubuntu.* noble\/(main|universe) / {gsub(/ /, "", $2); print $2; exit}' <<<"$OUT")"
  new="$("$VM" exec dpkg-query -W -f '${Version}' php8.3-fpm </dev/null | tr -d '\r')"
  printf 'Bản đang cài: %s; bản cũ hơn của kho release: %s\n' "$new" "$old"
  if [ -z "$old" ] || [ "$old" = "$new" ]; then
    printf 'Kho release không có bản cũ hơn: không dựng được gói đang chờ, bỏ phần áp dụng cập nhật.\n'
    ok "có bản cũ hơn trong kho release (để có gói đang chờ)" false
  else
    pkgs="$("$VM" exec dpkg-query -W -f '${Package} ${db:Status-Status}\n' 'php8.3-*' </dev/null | tr -d '\r' | awk '$2 == "installed" {print $1 "='"$old"'"}' | tr '\n' ' ')"
    printf 'Gói hạ cùng lúc: %s\n' "$pkgs"
    # shellcheck disable=SC2086
    vroot env DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=300 install -y --allow-downgrades $pkgs
    ok "hạ gói thoát 0" rc_is 0
    vexec dpkg-query -W -f '${Version}' php8.3-fpm
    ok "php8.3-fpm ở bản $old" out_is "$old"

    section "15: GET /api/v1/updates thấy gói đang chờ"
    api GET /api/v1/updates
    show '{packages: [.packages[] | "\(.name) \(.from) -> \(.to)\(if .security then " [bảo mật]" else "" end)"], others}'
    ok "php8.3-fpm đang chờ, bản hiện tại $old" bj --arg o "$old" '[.packages[] | select(.name == "php8.3-fpm" and .from == $o)] | length == 1'

    section "15: POST /api/v1/updates"
    dry "nâng gói của stack" update.apply /api/v1/updates
    ok "bản xem trước nêu php8.3-fpm" pv 'tostring | contains("php8.3-fpm")'
    vexec dpkg-query -W -f '${Version}' php8.3-fpm
    ok "đối chứng: xem trước không nâng (vẫn $old)" out_is "$old"
    job "nâng gói của stack" /api/v1/updates
    vexec bash -c 'dpkg-query -W -f "\${Version}\n" php8.3-fpm; ls /etc/php/8.3/fpm/pool.d; systemctl is-active php8.3-fpm'
    ok "php8.3-fpm đã lên bản khác $old, www.conf không trở lại, active" bash -c 'v="$(head -1 <<<"$1")"; [ -n "$v" ] && [ "$v" != "$2" ] && ! grep -qx "www.conf" <<<"$1" && grep -qx active <<<"$1"' _ "$OUT" "$old"
    api GET /api/v1/updates
    ok "GET /updates: php8.3-fpm không còn chờ" bj '[.packages[] | select(.name == "php8.3-fpm")] | length == 0'
    sites_200
  fi

  section "15: POST /api/v1/updates/auto"
  local f=/etc/apt/apt.conf.d/52tpan-unattended-upgrades before
  "$VM" exec test -e "$f" </dev/null
  before=$?
  printf '%s trước khi bật: %s\n' "$f" "$([ "$before" = 0 ] && echo có || echo không có)"
  dry "bật bản vá tự động" update.auto /api/v1/updates/auto
  "$VM" exec test -e "$f" </dev/null
  ok "đối chứng: xem trước không đổi $f" test "$?" = "$before"
  job "bật bản vá tự động" /api/v1/updates/auto
  vexec bash -c 'cat "$1"; systemctl is-enabled unattended-upgrades apt-daily-upgrade.timer 2>&1' _ "$f"
  "$VM" exec test -s "$f" </dev/null
  ok "$f có, khác rỗng" test "$?" = 0
  api GET /api/v1/updates
  show '{auto: (.auto | {id, status, summary: .summary.vi}), auto_origins}'
  ok "GET /updates: auto pass" bj '.auto.status == "pass"'
  part_end 15
}

# ================================================================ 16

p16_mariadb() {
  part_begin "16: POST /api/v1/mariadb/upgrade (CHỈ ?dry_run=1)"
  JAR="$J1"
  local d mv0
  printf 'Nâng MariaDB là một chiều, tốn nhiều phút và đã chạy thật trên bản sao ở phần D của e2e-m12.sh;\nở đây chỉ gọi ?dry_run=1. Bản xem trước đòi mọi site active có bản full trong 24 giờ: backup %s, %s qua API trước (%s đã có từ phần 10).\n' "$B" "$C" "$A"
  for d in "$B" "$C"; do
    job "backup $d" "/api/v1/sites/$d/backups"
  done
  printf '{"line":"10.6"}' >"$TMPD/mdb-old.json"
  printf '{"line":"11.8"}' >"$TMPD/mdb.json"
  post '/api/v1/mariadb/upgrade?dry_run=1' "$TMPD/mdb-old.json"
  show '.error.code'
  ok "dòng cũ hơn: 409 mariadb_not_newer" err_is 409 mariadb_not_newer
  mv0="$("$VM" exec mariadb --version </dev/null | tr -d '\r')"
  dry "nâng MariaDB lên 11.8" mariadb.upgrade /api/v1/mariadb/upgrade "$TMPD/mdb.json"
  ok "xem trước có bước thêm kho MariaDB (apt.repo:mariadb)" pv 'any(.steps[]; .info.id | startswith("apt.repo:mariadb"))'
  vexec bash -c 'ls /etc/apt/sources.list.d/; mariadb --version'
  ok "đối chứng: không thêm kho MariaDB, mariadb --version không đổi" bash -c '! grep -q tpan-mariadb <<<"$1" && grep -qxF "$2" <<<"$1"' _ "$OUT" "$mv0"
  part_end 16
}

# ================================================================ 17

p17_sessions() {
  part_begin "17: DELETE /api/v1/account/sessions/{id}"
  local J2="$TMPD/jar2" id2
  login "$J2" "$PANEL_PW"
  ok "đăng nhập phiên 2: 204" code_is 204
  api GET /api/v1/account/sessions
  show
  id2="$(jq -r '[.[] | select(.current)][0].id // empty' "$TMPD/body")"
  ok "phiên 2 thấy chính nó (current)" test -n "$id2"
  JAR="$J1"
  api GET /api/v1/account/sessions
  show
  ok "phiên 1 thấy 2 phiên, phiên 2 không phải current" bj --arg id "$id2" 'length == 2 and ([.[] | select(.id == $id and (.current | not))] | length == 1)'
  apiw DELETE "/api/v1/account/sessions/$id2"
  ok "phiên 1 xoá phiên 2: 204" code_is 204
  JAR="$J2"
  api GET /api/v1/session
  show
  ok "phiên 2: logged_in false" bj '.logged_in == false'
  api GET /api/v1/overview
  show '.error.code'
  ok "phiên 2 gọi /overview: 401 unauthorized" err_is 401 unauthorized
  JAR="$J1"
  api GET /api/v1/session
  ok "phiên 1 còn đăng nhập" bj '.logged_in == true'
  apiw DELETE "/api/v1/account/sessions/$id2"
  show '.error.code'
  ok "xoá lại phiên đã xoá: 404 session_not_found" err_is 404 session_not_found
  vroot python3 -c "$SQL_PY" 'SELECT COUNT(*) FROM sessions'
  ok "đối chứng: bảng sessions còn 1 dòng" out_is 1
  part_end 17
}

# ================================================================ 18

p18_password() {
  part_begin "18: POST /api/v1/account/password"
  local J3="$TMPD/jar3" P2="$TMPD/panel-pw-2" h0
  (umask 077 && LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c 24 >"$P2")
  add_secret "$(cat "$P2")"
  printf 'Mật khẩu mới: 24 ký tự ngẫu nhiên (file 0600 trong thư mục tạm, đã che).\n'
  h0="$(hash_fp)"
  login "$J3" "$PANEL_PW"
  ok "đăng nhập phiên 3: 204" code_is 204
  JAR="$J1"
  jq -n --rawfile c "$PANEL_PW" '{current: $c, new: "ngan"}' >"$TMPD/pw-short.json"
  chmod 600 "$TMPD/pw-short.json"
  post /api/v1/account/password "$TMPD/pw-short.json"
  show '.error.code'
  ok "mật khẩu mới ngắn: 400 password_too_short" err_is 400 password_too_short
  jq -n --rawfile n "$P2" '{current: "mat-khau-hien-tai-sai", new: $n}' >"$TMPD/pw-wrong.json"
  chmod 600 "$TMPD/pw-wrong.json"
  post /api/v1/account/password "$TMPD/pw-wrong.json"
  show '.error.code'
  ok "sai mật khẩu hiện tại: 401 wrong_password (lần sai 1/5)" err_is 401 wrong_password
  api GET /api/v1/session
  ok "phiên 1 vẫn đăng nhập sau lần sai (401 wrong_password không đá ra)" bj '.logged_in == true'
  jq -n --rawfile c "$PANEL_PW" --rawfile n "$P2" '{current: $c, new: $n}' >"$TMPD/pw.json"
  chmod 600 "$TMPD/pw.json"
  post /api/v1/account/password "$TMPD/pw.json"
  ok "đổi mật khẩu: 204" code_is 204
  local h1
  h1="$(hash_fp)"
  printf 'sha256 (16 ký tự đầu) của hash mật khẩu trong settings: trước %s, sau %s\n' "$h0" "$h1"
  ok "đối chứng: hash mật khẩu trong settings đã đổi" bash -c '[ -n "$1" ] && [ "$1" != "$2" ]' _ "$h0" "$h1"
  api GET /api/v1/session
  ok "phiên 1 (phiên đổi mật khẩu) vẫn đăng nhập" bj '.logged_in == true'
  JAR="$J3"
  api GET /api/v1/session
  ok "phiên 3 (phiên khác) đã bị huỷ: logged_in false" bj '.logged_in == false'
  login "$TMPD/jar-old" "$PANEL_PW"
  show '.error.code'
  ok "mật khẩu cũ: 401 wrong_password (lần sai 2/5)" err_is 401 wrong_password
  login "$TMPD/jar-new" "$P2"
  ok "mật khẩu mới: 204" code_is 204
  api GET /api/v1/session
  ok "phiên mới logged_in true" bj '.logged_in == true'
  PANEL_PW="$P2"
  vroot python3 -c "$SQL_PY" "SELECT action, target, result FROM audit WHERE action IN ('account.password', 'session.delete', 'login.fail') ORDER BY id"
  ok "audit: session.delete ok, account.password ok" bash -c 'grep -qE "^session\.delete[[:blank:]].*[[:blank:]]ok$" <<<"$1" && grep -qE "^account\.password[[:blank:]]+ok$" <<<"$1"' _ "$OUT"
  part_end 18
}
# hash_fp: dấu vân tay ngắn của hash mật khẩu quản trị (không in hash).
hash_fp() { sqlq "SELECT value FROM settings WHERE key = 'admin.password_hash'" | shasum -a 256 | cut -c1-16; }

# ================================================================ cuối

p_leaks() {
  part_begin "19: mật khẩu không lọt vào tpan.db, log của tpan.service"
  local f s k=0 nd nj
  for f in tpan.db tpan.db-wal; do
    "$VM" root cat "/var/lib/tpan/$f" </dev/null >"$TMPD/$f" 2>/dev/null || : >"$TMPD/$f"
  done
  "$VM" root journalctl -u tpan --no-pager -o short-iso </dev/null >"$TMPD/journal" 2>&1
  nd="$(cat "$TMPD/tpan.db" "$TMPD/tpan.db-wal" | grep -acF 'site.add')"
  printf 'Đối chứng: chuỗi "site.add" có trong tpan.db + WAL ở %s dòng; log tpan.service %s dòng\n' "$nd" "$(wc -l <"$TMPD/journal" | tr -d ' ')"
  ok "đối chứng: cách tìm này thấy được chữ có thật trong DB" test "$nd" -gt 0
  while IFS= read -r s; do
    [ -n "$s" ] || continue
    k=$((k + 1))
    nd="$(cat "$TMPD/tpan.db" "$TMPD/tpan.db-wal" | grep -acF -- "$s")"
    nj="$(grep -cF -- "$s" "$TMPD/journal")"
    ok "bí mật thứ $k: không có trong tpan.db + WAL ($nd dòng) và log tpan.service ($nj dòng)" test "$nd" -eq 0 -a "$nj" -eq 0
  done <"$SECRETS"
  OUT="$(grep -E 'level=(ERROR|WARN)' "$TMPD/journal")"
  mask
  printf 'Dòng ERROR/WARN trong log tpan.service:\n%s\n' "${OUT:-(không có)}"
  part_end 19
}

main() {
  p1_setup
  p2_reads
  p3_health_run
  p4_log
  p5_csrf
  p6_settings
  p7_ai_off
  p8_fail2ban
  p9_fixes
  p10_backups
  p11_db
  p12_plans_ai
  p13_php
  p14_switch
  p15_updates
  p16_mariadb
  p17_sessions
  p18_password
  p_leaks
}

case "${1:-all}" in
  all) main ;;
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
  ok "log không chứa IP máy ảo" bash -c '[ -z "$1" ] || ! grep -qF -- "$1" "$2"' _ "$VM_IP" "$E2E_LOG"
  ok "log không chứa mạng Multipass" bash -c '[ -z "$1" ] || ! grep -qF -- "$1" "$2"' _ "$NET4" "$E2E_LOG"
  ok "log không chứa IPv6 của máy ảo" bash -c '[ -z "$1" ] || ! grep -qF -- "$1" "$2"' _ "$V6PFX" "$E2E_LOG"
  ok "log không chứa IP của Mac" bash -c '[ -z "$1" ] || ! grep -qF -- "$1" "$2"' _ "$HOST_IP" "$E2E_LOG"
  ok "log không chứa thư mục nhà hay tên user Mac" bash -c '! grep -qF -- "$1" "$3" && ! grep -qF -- "$2" "$3"' _ "$HOME" "$USER" "$E2E_LOG"
  ok "log không chứa giá trị cookie nào" bash -c '! grep -iE "^set-cookie: [^=]+=" "$1" | grep -vqE "=<đã che>"' _ "$E2E_LOG"
fi

section "TỔNG KẾT"
printf '%s\n' "${SUMMARY[@]}"
printf '\nTỔNG KẾT: %s khẳng định đạt, %s khẳng định không đạt\n' "$PASSES" "$FAILS"
[ "$FAILS" -eq 0 ]
