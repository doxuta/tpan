#!/usr/bin/env bash
# e2e của mốc M8 (AI trong panel) trên máy ảo của mốc (.tpan-vm): bước 13 của
# mục 12 trong docs/KE-HOACH.md và phần "Xong khi" của M8 ở mục 13. Chạy trên
# Mac, trong thư mục repo:
#
#   scripts/e2e-m8.sh                bước 13 với lệnh claude giả (mặc định)
#   scripts/e2e-m8.sh real-rehearse  kịch bản Claude Code thật, nhưng chạy bằng
#                                    claude giả (tập dượt, không tốn hạn mức)
#   scripts/e2e-m8.sh real           kịch bản Claude Code thật: dùng lệnh
#                                    claude đã đăng nhập trên Mac, --model
#                                    sonnet --max-turns 4. MỖI câu hỏi gửi từ
#                                    panel là MỘT lần gọi claude -p thật (tính
#                                    vào hạn mức gói Claude); kịch bản gửi 2 câu.
#
# Cả ba đều bắt đầu từ máy sạch. Đường đi của một câu hỏi đúng như mục 8.4:
#
#   curl (thay trình duyệt) ──► tpan serve trên máy ảo ──► /run/tpan/brain.sock
#     ──► ssh ubuntu@<máy ảo> sudo -n tpan brain-link ──► tpan brain (Mac)
#     ──► claude -p (giả hoặc thật) ──► ssh ... sudo -n tpan mcp --session <id>
#
# claude giả (scripts/e2e-m8/fakeclaude) nằm ở <tmp>/fakebin/claude, đặt đầu
# PATH của tpan brain; cấu hình của nó là <tmp>/fakebin/fakeclaude.json (tpan
# brain chỉ chuyển HOME, PATH, USER, LOGNAME, TMPDIR cho claude, nên không dùng
# biến môi trường). Nó ghi argv và prompt nhận được vào <tmp>/fake-rec.
# Khi chạy với claude giả, HOME của tpan brain là <tmp>/home: thư mục làm việc
# của các phiên (os.UserCacheDir()/tpan-brain) nằm trong thư mục tạm.
#
# Khoá ssh dùng một lần, -F /dev/null, known_hosts trong thư mục tạm: như
# scripts/e2e-m7.sh, không đọc, không ghi ~/.ssh.
#
# Đặt E2E_TMP=<thư mục> để thư mục tạm nằm trong đó. Đặt E2E_LOG=<file log>
# để cuối lần chạy script tự tìm trong chính file log đó: mật khẩu, IP máy
# ảo, IP của Mac, tên máy Mac, thư mục nhà và tên user Mac (phải là 0 lần).
#
# Output như e2e-m7.sh: mỗi lệnh có dấu thời gian, output nguyên văn (đã che)
# và mã thoát; mỗi khẳng định in "=> ĐẠT" hoặc "=> KHÔNG ĐẠT". Che: mật khẩu,
# cookie, IP máy ảo (<vm>), IP của Mac (<mac>), tên máy Mac (<mac-host>), thư
# mục tạm (<tmp>), thư mục nhà và tên user Mac.
# Không dùng set -e: nhiều lệnh ở đây được chờ là phải lỗi.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2

MODE="${1:-fake}"
case "$MODE" in fake | real | real-rehearse) ;; *)
  echo "Cách dùng: scripts/e2e-m8.sh [fake|real-rehearse|real]" >&2
  exit 2
  ;;
esac

VM=scripts/vm.sh
FAILS=0
OUT=""
RC=0
EMAIL=admin@example.com
A=a.example.com
B=b.example.com
C=c.example.com
if [ -n "${E2E_TMP:-}" ]; then
  mkdir -p "$E2E_TMP"
  TMPD="$(mktemp -d "$E2E_TMP/run.XXXXXX")"
else
  TMPD="$(mktemp -d)"
fi
chmod 700 "$TMPD"
SECRETS="$TMPD/secrets"
: >"$SECRETS"
chmod 600 "$SECRETS"
trap 'jobs -p | xargs kill -INT 2>/dev/null; sleep 1; jobs -p | xargs kill 2>/dev/null; rm -rf "$TMPD"' EXIT

ts() { date '+%Y-%m-%d %H:%M:%S'; }
now() { perl -MTime::HiRes=time -e 'printf "%.3f", time'; }
section() { printf '\n\n######## [%s] %s\n' "$(ts)" "$*"; }

VM_IP=""
HOST_IP=""
BASE=""
MAC_HOST="$(hostname)"
MAC_HOST_S="${MAC_HOST%%.*}"

mask_str() {
  local s
  s="$(cat)"
  [ -n "$VM_IP" ] && s="${s//"$VM_IP"/<vm>}"
  [ -n "$HOST_IP" ] && s="${s//"$HOST_IP"/<mac>}"
  s="${s//"$TMPD"/<tmp>}"
  s="${s//"$HOME"/~}"
  s="${s//"$MAC_HOST"/<mac-host>}"
  s="${s//"$MAC_HOST_S"/<mac-host>}"
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

add_dbpw() {
  local dbpw
  dbpw="$("$VM" root bash -c 'cd / && runuser -u "$2" -- wp config get DB_PASSWORD --path="/var/www/$1/public" 2>/dev/null' _ "$1" "$(user_of "$1")" | tr -d '\r')"
  add_secret "$dbpw"
  printf 'Đã đọc mật khẩu DB của %s vào danh sách che (%s ký tự).\n' "$1" "${#dbpw}"
}

# run_pw: như run, nhặt mật khẩu quản trị WordPress ra khỏi output vào $SECRETS.
run_pw() {
  printf '\n[%s] $ %s\n' "$(ts)" "$(mask_str <<<"$*")"
  OUT="$("$@" 2>&1)"
  RC=$?
  local pw
  pw="$(sed -nE 's/^  Mật khẩu: +([A-Za-z0-9]{32})\r?$/\1/p' <<<"$OUT" | head -1)"
  add_secret "$pw"
  mask
  printf '%s\n[%s] (mã thoát %s)\n' "$OUT" "$(ts)" "$RC"
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
TPANM="$TMPD/tpan-darwin" # tpan build cho Mac, chạy tpan brain
FAKEBIN="$TMPD/fakebin"
FAKECONF="$FAKEBIN/fakeclaude.json"
FAKEREC="$TMPD/fake-rec"

deploy() {
  VM_IP="$("$VM" ip)"
  HOST_IP="$("$VM" exec bash -c 'echo "${SSH_CLIENT%% *}"' | tr -d '\r')"
  BASE="https://$VM_IP:8443"
  printf 'Máy ảo: %s; IP máy ảo: <vm>; IP của Mac trong mạng Multipass: <mac>; tên máy Mac: <mac-host> (đều được che trong log)\n' "$(tr -d '[:space:]' <.tpan-vm 2>/dev/null || echo tpan-test)"
  run make vm-deploy DATE="$BUILD_DATE"
  ok "build và chép tpan vào máy ảo" rc_is 0
  run go build -o "$TPANM" ./cmd/tpan
  ok "build tpan cho Mac (chạy tpan brain)" rc_is 0
  mkdir -p "$FAKEBIN" "$FAKEREC" "$TMPD/home"
  run go build -o "$FAKEBIN/claude" ./scripts/e2e-m8/fakeclaude
  ok "build claude giả (SDK go-sdk, CommandTransport)" rc_is 0
  fake_conf propose ""
  run shasum -a 256 bin/tpan-linux-arm64 bin/tpan-linux-amd64
  run git log --oneline -1
  run git status --short
}

# fake_conf <mode> <extra_tool>: cấu hình của claude giả, đọc lại ở mỗi lần chạy.
fake_conf() {
  jq -n --arg rec "$FAKEREC" --arg mode "$1" --arg extra "$2" \
    '{record_dir: $rec, mode: $mode, extra_tool: $extra, think_ms: 1000, delta_ms: 150}' >"$FAKECONF"
  printf '\n[%s] cấu hình claude giả: %s\n' "$(ts)" "$(jq -c 'del(.record_dir)' "$FAKECONF")"
}

# ---------------------------------------------------------------- ssh

KEY="$TMPD/id_e2e"
SSH=()
BRAIN_SSH=()

ssh_init() {
  SSH=(ssh -F /dev/null -i "$KEY" -o IdentitiesOnly=yes -o IdentityAgent=none
    -o UserKnownHostsFile="$TMPD/known_hosts" -o GlobalKnownHostsFile=/dev/null
    -o StrictHostKeyChecking=accept-new -o BatchMode=yes -o LogLevel=ERROR
    -o ConnectTimeout=10 "ubuntu@$VM_IP")
  # Cùng các tham số đó cho tpan brain (mỗi --ssh-opt là đúng một tham số của
  # ssh; tpan brain tự thêm -o BatchMode=yes và dùng cho cả brain-link lẫn MCP).
  BRAIN_SSH=(--ssh-opt=-F --ssh-opt=/dev/null --ssh-opt=-i "--ssh-opt=$KEY"
    --ssh-opt=-oIdentitiesOnly=yes --ssh-opt=-oIdentityAgent=none
    "--ssh-opt=-oUserKnownHostsFile=$TMPD/known_hosts" --ssh-opt=-oGlobalKnownHostsFile=/dev/null
    --ssh-opt=-oStrictHostKeyChecking=accept-new --ssh-opt=-oLogLevel=ERROR --ssh-opt=-oConnectTimeout=10)
}

# ---------------------------------------------------------------- chụp máy

SNAP_SCRIPT='
echo "## user tp_ (getent passwd | grep tp_)"; getent passwd | grep tp_
echo "## ls /var/www"; ls /var/www
echo "## SHOW DATABASES"; mariadb -N -e "SHOW DATABASES"
echo "## user DB"; mariadb -N -e "SELECT User FROM mysql.user" | sort
echo "## ls /etc/nginx/sites-enabled"; ls /etc/nginx/sites-enabled
echo "## sha256sum /etc/nginx/sites-available/*"; sha256sum /etc/nginx/sites-available/*
echo "## ls /etc/php/8.3/fpm/pool.d"; ls /etc/php/8.3/fpm/pool.d
echo "## job, site trong tpan.db"; python3 -c "import sqlite3; c = sqlite3.connect(\"file:/var/lib/tpan/tpan.db?mode=ro\", uri=True); print(\"jobs:\", c.execute(\"SELECT COUNT(*) FROM jobs\").fetchone()[0], \"sites:\", c.execute(\"SELECT COUNT(*) FROM sites\").fetchone()[0])"
'

http_code() { vexec curl -sS -m 20 -o /dev/null -w '%{http_code}\n' --resolve "$1:80:127.0.0.1" "http://$1/"; }

# sqlq <câu SQL>: chạy câu chỉ đọc trên tpan.db, in mỗi dòng một JSON.
sqlq() {
  "$VM" root python3 -c 'import sqlite3, json, sys; c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True); [print(json.dumps(list(r), ensure_ascii=False)) for r in c.execute(sys.argv[1])]' "$1" | tr -d '\r'
}
ai_calls_count() { sqlq "SELECT COUNT(*) FROM ai_calls" | jq -r '.[0]'; }

# ---------------------------------------------------------------- API panel

JAR="$TMPD/jar"
CODE=""
api() {
  local m=$1 p=$2
  shift 2
  : >"$TMPD/body"
  run curl -sS -m 60 --cacert "$TMPD/cert.pem" -b "$JAR" -c "$JAR" -X "$m" -D - -o "$TMPD/body" \
    -w '-> HTTP %{http_code}, %{time_total}s\n' "$@" "$BASE$p"
  CODE="$(sed -nE 's/^-> HTTP ([0-9]{3}),.*/\1/p' <<<"$OUT" | tail -1)"
}
code_is() { [ "$CODE" = "$1" ]; }
show() {
  OUT="$(jq "${1:-.}" "$TMPD/body" 2>&1 || head -c 4000 "$TMPD/body")"
  mask
  printf '%s\n' "$OUT"
}
# post_json <đường dẫn> <file JSON>: in thân gửi đi (đã che) rồi POST.
post_json() {
  OUT="$(jq -c . "$2")"
  mask
  printf '\n[%s] thân gửi tới %s: %s' "$(ts)" "$1" "$OUT"
  api POST "$1" -H 'Content-Type: application/json' --data-binary "@$2"
}

view_json() { jq -n --arg p "$1" --arg l "$2" '{path: $p, label: $l}'; }

# ask_body <file> <session_id|""> <câu hỏi> <view path> <view label> <trail JSON> [pointer JSON]
ask_body() {
  jq -n --arg sid "$2" --arg q "$3" --arg vp "$4" --arg vl "$5" --argjson trail "$6" --argjson ptr "${7:-null}" \
    '{question: $q, lang: "vi", view: {path: $vp, label: $vl}, trail: $trail}
     + (if $sid == "" then {} else {session_id: $sid} end)
     + (if $ptr == null then {} else {pointer: $ptr} end)' >"$1"
}

# sse <id> <seq> <tên>: đọc luồng SSE của lượt tới khi máy chủ đóng (sau sự
# kiện kết thúc); mỗi dòng gắn giờ nhận trên Mac. Kết quả: $TMPD/sse-<tên>.json
# là mảng {recv, ev}. In từng sự kiện đã che, kèm giây tính từ T_ASK.
SSE_JSON=""
sse() {
  local id=$1 seq=$2 f="$TMPD/sse-$3"
  printf '\n[%s] $ %s\n' "$(ts)" "$(mask_str <<<"curl -sS -N --cacert <tmp>/cert.pem -b <cookie> $BASE/api/v1/ai/sessions/$id/turns/$seq/events")"
  curl -sS -N -m 360 --cacert "$TMPD/cert.pem" -b "$JAR" "$BASE/api/v1/ai/sessions/$id/turns/$seq/events" 2>"$f.err" |
    while IFS= read -r line; do printf '%s\t%s\n' "$(now)" "$line"; done >"$f.raw"
  RC=$?
  awk -F'\t' 'index($2, "data: ") == 1 { print "{\"recv\":" $1 ",\"ev\":" substr($2, 7) "}" }' "$f.raw" | jq -s . >"$f.json"
  SSE_JSON="$f.json"
  OUT="$(jq -r --argjson t0 "$T_ASK" '.[] | "+\(((.recv - $t0) * 1000 | floor) / 1000)s \(.ev.kind)"
      + (if .ev.tool then " \(.ev.tool)" else "" end)
      + (if .ev.plan_id then " plan_id=\(.ev.plan_id)" else "" end)
      + (if .ev.is_error then " is_error" else "" end)
      + (if .ev.version then " version=\(.ev.version) model=\(.ev.model)" else "" end)
      + (if .ev.input then " input=\(.ev.input | tojson)" else "" end)
      + (if .ev.kind == "tool_result" then " (\(.ev.text | length) ký tự)"
         elif .ev.kind == "text" then " (tổng \(.ev.text | length) ký tự): …\(.ev.text | gsub("\n"; "⏎") | .[-50:])"
         elif .ev.text then ": \(.ev.text)" else "" end)' "$f.json" 2>&1)"
  mask
  printf '%s\n' "$OUT"
  # Mỗi sự kiện text mang toàn bộ chữ tới lúc đó: in một lần bản cuối.
  OUT="$(jq -r '[.[].ev | select(.kind == "text")][-1].text // empty' "$f.json")"
  mask
  [ -n "$OUT" ] && printf -- '--- toàn bộ chữ đã hiện dần (sự kiện text cuối):\n%s\n---\n' "$OUT"
  [ -s "$f.err" ] && printf 'stderr của curl: %s\n' "$(mask_str <"$f.err")"
  printf '[%s] (%s sự kiện, mã thoát %s)\n' "$(ts)" "$(jq length "$f.json")" "$RC"
}
sse_kinds() { jq -c '[.[].ev.kind]' "$SSE_JSON"; }
sse_final() { jq -c '[.[] | select(.ev.kind == "done" or .ev.kind == "error" or .ev.kind == "refused")][-1].ev' "$SSE_JSON"; }
sse_texts_before_done() { jq '[.[].ev.kind] | (index("done") // length) as $d | .[:$d] | map(select(. == "text")) | length' "$SSE_JSON"; }
# sse_split_ok: chữ hiện ngay sau mỗi lần gọi công cụ bắt đầu bằng toàn bộ chữ
# trước đó cộng một dòng trống (tin nhắn assistant mới là đoạn mới).
sse_split_ok() {
  jq -e '. as $a | [range(0; length) | select($a[.].ev.kind == "tool_call") as $i
    | ([$a[:$i][] | select(.ev.kind == "text")][-1].ev.text) as $b
    | ([$a[$i:][] | select(.ev.kind == "text")][0].ev.text) as $n
    | select($b != null and $n != null) | ($n | startswith($b + "\n\n"))] | (length > 0 and all)' "$SSE_JSON" >/dev/null
}
# split_or_none: như sse_split_ok, nhưng cũng đạt khi không có chữ nào đứng
# trước một lần gọi công cụ (Claude thật có thể gọi công cụ ngay, chưa viết gì).
split_or_none() {
  sse_split_ok || ! jq -e '[.[].ev.kind] | index("tool_call") as $i | $i != null and (.[:$i] | index("text")) != null' "$SSE_JSON" >/dev/null
}
sse_first_text_s() { jq -r --argjson t0 "$T_ASK" '[.[] | select(.ev.kind == "text")][0].recv // empty | . - $t0 | . * 1000 | floor / 1000' "$SSE_JSON"; }

# ask <file thân>: POST /ai/ask, nhớ SID, SEQ, T_ASK (giờ Mac ngay trước khi gửi).
SID=""
SEQ=""
T_ASK=0
ask() {
  T_ASK="$(now)"
  post_json /api/v1/ai/ask "$1"
  show
  SID="$(jq -r '.session_id // empty' "$TMPD/body")"
  SEQ="$(jq -r '.seq // empty' "$TMPD/body")"
}

# ---------------------------------------------------------------- tpan brain

BRAIN_PID=""
BRAIN_LOG=""
# brain_cmd <claude: fake|real>: argv của tpan brain (không gồm env).
brain_argv() {
  BRAIN_ARGV=("$TPANM" brain "ubuntu@$VM_IP" "${BRAIN_SSH[@]}" --max-turns 4)
  [ "$1" = real ] && BRAIN_ARGV+=(--model sonnet)
  return 0
}
# brain_start <tên> <fake|real>: chạy tpan brain ở nền, chờ "đã nối" (60 giây).
brain_start() {
  BRAIN_LOG="$TMPD/brain-$1.log"
  brain_argv "$2"
  if [ "$2" = fake ]; then
    printf '\n[%s] $ %s &\n' "$(ts)" "$(mask_str <<<"HOME=<tmp>/home PATH=<tmp>/fakebin:\$PATH ${BRAIN_ARGV[*]}")"
    env HOME="$TMPD/home" PATH="$FAKEBIN:$PATH" "${BRAIN_ARGV[@]}" >"$BRAIN_LOG" 2>&1 &
  else
    printf '\n[%s] $ %s &\n' "$(ts)" "$(mask_str <<<"${BRAIN_ARGV[*]}")"
    "${BRAIN_ARGV[@]}" >"$BRAIN_LOG" 2>&1 &
  fi
  BRAIN_PID=$!
  local _
  for _ in $(seq 1 120); do
    grep -q 'đã nối' "$BRAIN_LOG" && break
    kill -0 "$BRAIN_PID" 2>/dev/null || break
    sleep 0.5
  done
  OUT="$(cat "$BRAIN_LOG")"
  mask
  printf '%s\n' "$OUT"
}
brain_show_log() {
  OUT="$(cat "$BRAIN_LOG")"
  mask
  printf -- '--- terminal của tpan brain tới lúc này:\n%s\n' "$OUT"
}
brain_stop() {
  printf '\n[%s] $ kill -INT <pid của tpan brain>; wait\n' "$(ts)"
  kill -INT "$BRAIN_PID" 2>/dev/null
  wait "$BRAIN_PID"
  RC=$?
  brain_show_log
  printf '[%s] (tpan brain thoát mã %s)\n' "$(ts)" "$RC"
}

# rec <n>: file ghi của lượt thứ n mà claude giả đã chạy.
rec() { printf '%s/turn-%s.json' "$FAKEREC" "$1"; }

M16=()

# ---------------------------------------------------------------- chuẩn bị chung

P1=""
VMHOST=""
prepare() {
  P1="$TMPD/panel-pw"
  section "a. máy sạch, triển khai, stack nginx, tpan setup, site $A và $B"
  reset_vm
  deploy
  vroot tpan stack install --web nginx --yes
  ok "stack install thoát 0" rc_is 0
  vexec systemctl is-active nginx php8.3-fpm mariadb
  ok "3 dịch vụ active" bash -c '[ "$(grep -cx active <<<"$1")" -eq 3 ]' _ "$OUT"

  local cmd=("$VM" root tpan setup --yes) pw
  printf '\n[%s] $ %s\n' "$(ts)" "${cmd[*]}"
  OUT="$("${cmd[@]}" 2>&1)"
  RC=$?
  pw="$(grep -A1 '^Mật khẩu quản trị (chỉ hiện MỘT lần' <<<"$OUT" | sed -nE 's/^    ([A-Z2-7]{26})\r?$/\1/p' | head -1)"
  if [ -n "$pw" ]; then
    add_secret "$pw"
    (umask 077 && printf '%s' "$pw" >"$P1")
  fi
  mask
  printf '%s\n[%s] (mã thoát %s)\n' "$OUT" "$(ts)" "$RC"
  ok "setup thoát 0, nhặt được mật khẩu panel" bash -c '[ "$1" -eq 0 ] && [ -s "$2" ]' _ "$RC" "$P1"

  local d
  for d in "$A" "$B"; do
    run_pw "$VM" root tpan site add "$d" --email "$EMAIL" --yes
    ok "site add $d thoát 0" rc_is 0
    add_dbpw "$d"
    http_code "$d"
    ok "$d trả 200" has '^200$'
  done
  vroot hostname
  VMHOST="$(tr -d '\r' <<<"$OUT" | head -1)"

  section "b. khoá ssh dùng một lần (thư mục tạm, không ~/.ssh), scripts/vm.sh ssh-key"
  run ssh-keygen -q -t ed25519 -N '' -C tpan-e2e -f "$KEY"
  ok "sinh khoá ed25519" rc_is 0
  run "$VM" ssh-key "$KEY.pub"
  ok "vm.sh ssh-key thêm khoá" bash -c '[ "$1" -eq 0 ] && grep -q "Đã thêm" <<<"$2"' _ "$RC" "$OUT"
  ssh_init
  run "${SSH[@]}" sudo -n tpan version
  ok "sudo -n tpan qua ssh không tty: chạy, không hỏi mật khẩu" rc_is 0

  section "c. bật bộ não: sudo tpan mcp config --brain bridge, khởi động lại tpan"
  vroot tpan mcp config --brain bridge
  ok "mcp config --brain bridge thoát 0" rc_is 0
  vroot tpan mcp config --json
  ok "ai.brain = bridge" bash -c 'jq -e ".brain == \"bridge\"" <<<"$1" >/dev/null' _ "$OUT"
  vroot systemctl restart tpan
  ok "restart tpan thoát 0" rc_is 0
  vroot bash -c 'for i in $(seq 1 20); do [ -S /run/tpan/brain.sock ] && break; sleep 0.5; done; systemctl is-active tpan; stat -c "%A %U:%G %n" /run/tpan /run/tpan/brain.sock'
  ok "tpan active, /run/tpan/brain.sock là socket 0600 của root" bash -c 'grep -qx active <<<"$1" && grep -qE "^srw------- root:root /run/tpan/brain.sock$" <<<"$1"' _ "$OUT"
  vroot bash -c 'journalctl -u tpan -b --no-pager -o cat | tail -n 20'
  ok "journal của tpan không có lỗi mở socket bộ não" hasnt 'không mở được socket'

  section "d. API: đăng nhập panel"
  "$VM" root cat /etc/tpan/tls/cert.pem | tr -d '\r' >"$TMPD/cert.pem"
  jq -Rn '{password: input}' <"$P1" >"$TMPD/login.json"
  chmod 600 "$TMPD/login.json"
  : >"$JAR"
  chmod 600 "$JAR"
  api POST /api/v1/login -H 'Content-Type: application/json' --data-binary "@$TMPD/login.json"
  ok "đăng nhập: 204" code_is 204
  api GET /api/v1/ai/brain
  show
  ok "GET /ai/brain trước khi nối: mode bridge, chưa nối, có lệnh nối" bash -c '[ "$1" = 200 ] && jq -e ".mode == \"bridge\" and .connected == false and (.connect_command | startswith(\"tpan brain \"))" "$2" >/dev/null' _ "$CODE" "$TMPD/body"
}

# leak_check <nhãn>: tìm tên thật, IP, hostname, mật khẩu trong ai_turns và
# ai_calls (bảng ai_sessions.aliases và ai_views giữ tên thật theo thiết kế:
# không gửi cho AI).
leak_check() {
  sqlq "SELECT * FROM ai_turns" >"$TMPD/ai_turns"
  sqlq "SELECT * FROM ai_calls" >"$TMPD/ai_calls"
  printf 'ai_turns: %s dòng; ai_calls: %s dòng\n' "$(grep -c '' "$TMPD/ai_turns")" "$(grep -c '' "$TMPD/ai_calls")"
  ok "đối chứng: ai_turns có bí danh site-*.example (tìm đúng chỗ)" bash -c 'grep -qE "site-[a-z]+\\.example" "$1"' _ "$TMPD/ai_turns"
  local s label
  for label in "tên miền $A:$A" "tên miền $B:$B" "slug của $A:$(slug_of "$A")" "slug của $B:$(slug_of "$B")" "IP máy ảo:$VM_IP" "IP của Mac:$HOST_IP" "hostname máy ảo:$VMHOST" "tên máy Mac:$MAC_HOST_S"; do
    s="${label#*:}"
    ok "$1: không có ${label%%:*} trong ai_turns ($(grep -cF -- "$s" "$TMPD/ai_turns") dòng) và ai_calls ($(grep -cF -- "$s" "$TMPD/ai_calls") dòng)" \
      bash -c '! grep -qF -- "$1" "$2" && ! grep -qF -- "$1" "$3"' _ "$s" "$TMPD/ai_turns" "$TMPD/ai_calls"
  done
  local k=0
  while IFS= read -r s; do
    [ -n "$s" ] || continue
    k=$((k + 1))
    ok "$1: mật khẩu thứ $k không có trong ai_turns và ai_calls" bash -c '! grep -qF -- "$1" "$2" && ! grep -qF -- "$1" "$3"' _ "$s" "$TMPD/ai_turns" "$TMPD/ai_calls"
  done <"$SECRETS"
}

# ---------------------------------------------------------------- bước 13, claude giả

main_fake() {
  prepare

  section "e. tpan brain với claude giả"
  brain_start one fake
  ok "tpan brain nối được (in \"Bộ não: đã nối\")" bash -c 'grep -q "Bộ não: đã nối" "$1"' _ "$BRAIN_LOG"
  api GET /api/v1/ai/brain
  show
  ok "GET /ai/brain: connected, host là tên máy Mac, claude_version 2.1.328, model sonnet, auth claude.ai, không cảnh báo" \
    bash -c '[ "$1" = 200 ] && jq -e --arg h "$3" ".mode == \"bridge\" and .connected == true and .host == \$h and .claude_version == \"2.1.328\" and .tested_version == \"2.1.328\" and .model == \"sonnet\" and .auth_method == \"claude.ai\" and (.warnings // [] | length == 0)" "$2" >/dev/null' _ "$CODE" "$TMPD/body" "$MAC_HOST"

  section "f. chuyển tới trang site $A, xem trước thứ sẽ gửi"
  view_json "/sites/$A" "Trang site $A" >"$TMPD/view-a.json"
  post_json /api/v1/ai/views "$TMPD/view-a.json"
  ok "POST /ai/views: 204" code_is 204
  local trail1 q1
  trail1="$(jq -n --arg a "$A" '[{path: "/", label: "Bảng điều khiển"}, {path: "/sites/\($a)", label: "Trang site \($a)"}]')"
  q1="Tạo thêm một site mới cho mình rồi cho biết máy đang có site nào."
  ask_body "$TMPD/ask1.json" "" "$q1" "/sites/$A" "Trang site $A" "$trail1"
  post_json /api/v1/ai/preview "$TMPD/ask1.json"
  show '{session_id, question, playbook, chips: [.chips[] | {key, label: .label.vi, omitted, content}]}'
  cp "$TMPD/body" "$TMPD/preview1.json"
  ok "preview 200, nhãn ngữ cảnh: view, trail, machine (không pointer khi không chỉ vào gì)" \
    bash -c '[ "$1" = 200 ] && jq -e "[.chips[].key] == [\"view\", \"trail\", \"machine\"]" "$2" >/dev/null' _ "$CODE" "$TMPD/preview1.json"
  ok "nhãn view nói đúng mục, bằng bí danh: Trang site site-?.example (/sites/site-?.example)" \
    bash -c 'jq -e "[.chips[] | select(.key == \"view\")][0].content | test(\"^Trang site site-[a-z]+\\\\.example \\\\(/sites/site-[a-z]+\\\\.example\\\\)$\")" "$1" >/dev/null' _ "$TMPD/preview1.json"
  ok "nhãn machine có web server nginx, số site đang chạy 2" \
    bash -c 'jq -e "[.chips[] | select(.key == \"machine\")][0].content | (contains(\"web server: nginx\") and contains(\"active sites: 2\"))" "$1" >/dev/null' _ "$TMPD/preview1.json"
  ok "preview gửi kèm bản hướng dẫn site" bash -c 'jq -e ".playbook == \"site\"" "$1" >/dev/null && jq -r .prompt "$1" | grep -qF "<tpan-playbook name=\"site\">"' _ "$TMPD/preview1.json"
  local s label
  for label in "$A:$A" "$B:$B" "IP máy ảo:$VM_IP" "hostname máy ảo:$VMHOST"; do
    s="${label#*:}"
    ok "prompt xem trước đã che: không có ${label%%:*}" bash -c '! jq -r .prompt "$1" | grep -qF -- "$2"' _ "$TMPD/preview1.json" "$s"
  done

  section "g. hỏi (lượt 1): câu hỏi đi hết một vòng, Plan chờ duyệt, máy không đổi"
  vroot bash -c "$SNAP_SCRIPT"
  local before="$OUT" calls0
  calls0="$(ai_calls_count)"
  vroot tpan plan list --json
  ok "chưa có Plan nào" bash -c '[ "$(jq length <<<"$1")" = 0 ]' _ "$OUT"
  ask "$TMPD/ask1.json"
  ok "ask: 202, có session_id và seq 1" bash -c '[ "$1" = 202 ] && [ -n "$2" ] && [ "$3" = 1 ]' _ "$CODE" "$SID" "$SEQ"
  local SID1="$SID"
  sse "$SID1" 1 t1
  local kinds final ntext pid shown
  kinds="$(sse_kinds)"
  final="$(sse_final)"
  ntext="$(sse_texts_before_done)"
  printf 'Thứ tự sự kiện: %s\n' "$kinds"
  ok "SSE kết thúc bằng done" bash -c 'jq -e ".kind == \"done\"" <<<"$1" >/dev/null' _ "$final"
  ok "SSE có init trước mọi thứ khác, version 2.1.328" bash -c 'jq -e ".[0].ev.kind == \"init\" and .[0].ev.version == \"2.1.328\"" "$1" >/dev/null' _ "$SSE_JSON"
  ok "nhiều sự kiện text trước done ($ntext), chữ hiện dần (cách nhau theo giờ nhận)" bash -c '[ "$1" -ge 4 ] && jq -e "[.[] | select(.ev.kind == \"text\") | .recv] | (.[-1] - .[0]) > 0.5" "$2" >/dev/null' _ "$ntext" "$SSE_JSON"
  ok "có tool_call list_sites và propose_site_add" bash -c 'jq -e "[.[].ev | select(.kind == \"tool_call\") | .tool] == [\"mcp__tpan__list_sites\", \"mcp__tpan__propose_site_add\"]" "$1" >/dev/null' _ "$SSE_JSON"
  pid="$(jq -r '[.[].ev | select(.kind == "tool_result" and (.plan_id // "") != "")][0].plan_id // empty' "$SSE_JSON")"
  ok "tool_result của propose_site_add có plan_id" test -n "$pid"
  # Chữ hiện dần là sự kiện text cuối (mang toàn bộ chữ của lượt). claude giả
  # trả result chỉ có tin nhắn cuối như claude thật; done (và ai_turns.answer)
  # phải mang toàn bộ chữ đã nối, không phải result.
  shown="$(jq -r '[.[].ev | select(.kind == "text")][-1].text' "$SSE_JSON")"
  ok "chữ hiển thị là tên thật: bí danh bị cắt giữa hai sự kiện vẫn được đổi về tpan:site/$A hoặc $B" bash -c 'grep -qE "tpan:site/(a|b)\\.example\\.com" <<<"$1"' _ "$shown"
  ok "chữ hiển thị không còn bí danh site-?.example nào (cả chữ hiện dần lẫn done)" bash -c '! grep -qE "site-[a-z]+\\.example" <<<"$1" && ! jq -r .text <<<"$2" | grep -qE "site-[a-z]+\\.example"' _ "$shown" "$final"
  ok "chữ của 3 tin nhắn assistant được ngăn bằng dòng trống (2 chỗ)" bash -c '[ "$(grep -c "^$" <<<"$1")" = 2 ]' _ "$shown"
  ok "chữ sau mỗi lần gọi công cụ là đoạn mới (ngăn bằng dòng trống)" sse_split_ok
  ok "done mang toàn bộ chữ đã nối, trùng chữ hiện dần cuối cùng (không chỉ result = tin nhắn cuối)" bash -c '[ -n "$1" ] && [ "$(jq -r .text <<<"$2")" = "$1" ]' _ "$shown" "$final"
  M16+=("(claude giả) từ lúc gửi ask tới chữ đầu tiên trên SSE: $(sse_first_text_s)s, gồm 1s claude giả chờ thay cho độ trễ của model; mở ssh + tpan mcp --session + list tools: $(jq -r .mcp_ms "$(rec 1)") ms")

  api GET "/api/v1/plans/$pid"
  show '{id, kind, target, origin, status, session_id, untrusted}'
  ok "Plan $pid: pending, origin ai, kind site.add, target $C, cùng phiên" bash -c '[ "$1" = 200 ] && jq -e --arg c "$3" --arg s "$4" ".status == \"pending\" and .origin == \"ai\" and .kind == \"site.add\" and .target == \$c and .session_id == \$s" "$2" >/dev/null' _ "$CODE" "$TMPD/body" "$C" "$SID1"
  api GET /api/v1/plans
  show '[.[] | {id, kind, origin, status}]'
  ok "GET /plans: đúng 1 Plan, pending, origin ai" bash -c '[ "$1" = 200 ] && jq -e "length == 1 and .[0].status == \"pending\" and .[0].origin == \"ai\"" "$2" >/dev/null' _ "$CODE" "$TMPD/body"
  vroot bash -c "$SNAP_SCRIPT"
  ok "MÁY KHÔNG ĐỔI GÌ: user tp_, /var/www, DB, user DB, vhost, pool, số job, số site giống hệt trước lượt hỏi" test "$OUT" = "$before"
  http_code "$C"
  ok "$C chưa có: rơi vào site mặc định (nginx 444, curl mã 52)" rc_is 52

  section "g'. thứ claude giả đã nhận (argv, prompt) so với bản xem trước"
  OUT="$(jq '{args, resume, mode, extra_tool, mcp_ms, tools}' "$(rec 1)")"
  mask
  printf '%s\n' "$OUT"
  ok "claude -p chạy với --session-id <uuid>, --max-turns 4, --tools \"\", --strict-mcp-config, không --bare" \
    bash -c 'jq -e "(.args | index(\"--session-id\")) != null and (.args | index(\"--resume\")) == null and (.args[(.args | index(\"--max-turns\")) + 1] == \"4\") and (.args[(.args | index(\"--tools\")) + 1] == \"\") and (.args | index(\"--strict-mcp-config\")) != null and (.args | index(\"--bare\")) == null" "$1" >/dev/null' _ "$(rec 1)"
  ok "MCP server trong --mcp-config là ssh ... sudo -n tpan mcp --session $SID1" \
    bash -c 'jq -r ".args[(.args | index(\"--mcp-config\")) + 1]" "$1" | jq -e --arg s "$2" ".mcpServers | keys == [\"tpan\"] and .tpan.command == \"ssh\" and (.tpan.args[-6:] == [\"sudo\", \"-n\", \"tpan\", \"mcp\", \"--session\", \$s])" >/dev/null' _ "$(rec 1)" "$SID1"
  ok "claude giả thấy 18 công cụ mcp__tpan__*" bash -c 'jq -e "(.tools | length) == 18 and all(.tools[]; startswith(\"mcp__tpan__\"))" "$1" >/dev/null' _ "$(rec 1)"
  ok "prompt claude nhận qua stdin trùng từng byte với prompt của bản xem trước" bash -c '[ "$(jq -r .prompt "$1")" = "$(jq -r .prompt "$2")" ]' _ "$(rec 1)" "$TMPD/preview1.json"
  ok "thư mục làm việc của phiên rỗng sau lượt 1" bash -c '[ -d "$1" ] && [ -z "$(ls -A "$1")" ]' _ "$(jq -r .cwd "$(rec 1)")"
  ok "lượt 1 gọi đúng 2 công cụ (ai_calls tăng 2)" test "$(ai_calls_count)" -eq $((calls0 + 2))
  brain_show_log
  ok "terminal của tpan brain in câu hỏi (dấu ?) và lần gọi công cụ (dấu →)" bash -c 'grep -qF "? " "$1" && grep -qF "→ mcp__tpan__list_sites" "$1"' _ "$BRAIN_LOG"

  section "h. chuyển sang Sự cố rồi hỏi tiếp cùng phiên (lượt 2)"
  fake_conf read ""
  view_json /incidents "Sự cố" >"$TMPD/view-inc.json"
  post_json /api/v1/ai/views "$TMPD/view-inc.json"
  ok "POST /ai/views /incidents: 204" code_is 204
  local trail2
  trail2="$(jq -n --arg a "$A" '[{path: "/", label: "Bảng điều khiển"}, {path: "/sites/\($a)", label: "Trang site \($a)"}, {path: "/incidents", label: "Sự cố"}]')"
  ask_body "$TMPD/ask2.json" "$SID1" "Còn ở mục này thì sao?" /incidents "Sự cố" "$trail2"
  ask "$TMPD/ask2.json"
  ok "ask cùng session_id: 202, seq 2" bash -c '[ "$1" = 202 ] && [ "$2" = "$3" ] && [ "$4" = 2 ]' _ "$CODE" "$SID" "$SID1" "$SEQ"
  sse "$SID1" 2 t2
  ok "lượt 2 kết thúc bằng done" bash -c 'jq -e ".kind == \"done\"" <<<"$1" >/dev/null' _ "$(sse_final)"
  OUT="$(jq -r .prompt "$(rec 2)")"
  mask
  printf -- '--- prompt lượt 2 claude giả nhận (đã che sẵn; che thêm cho log):\n%s\n' "$OUT"
  ok "lượt 2 chạy --resume với đúng UUID của --session-id lượt 1" bash -c '[ "$(jq -r ".args[(.args | index(\"--resume\")) + 1]" "$2")" = "$(jq -r ".args[(.args | index(\"--session-id\")) + 1]" "$1")" ] && jq -e ".resume == true" "$2" >/dev/null' _ "$(rec 1)" "$(rec 2)"
  ok "prompt lượt 2: [view] Sự cố (/incidents)" bash -c 'jq -r .prompt "$1" | grep -qxF "[view] Sự cố (/incidents)"' _ "$(rec 2)"
  ok "prompt lượt 2: [trail] có trang site cũ dưới bí danh, không có $A" bash -c 'p="$(jq -r .prompt "$1")"; grep -qE "^Trang site site-[a-z]+\\.example \\(/sites/site-[a-z]+\\.example\\)$" <<<"$p" && grep -q "^\\[trail\\]" <<<"$p" && ! grep -qF "$2" <<<"$p"' _ "$(rec 2)" "$A"
  ok "prompt lượt 2 gửi bản hướng dẫn su-co, không gửi lại bản site" bash -c 'p="$(jq -r .prompt "$1")"; grep -qF "<tpan-playbook name=\"su-co\">" <<<"$p" && ! grep -qF "<tpan-playbook name=\"site\">" <<<"$p"' _ "$(rec 2)"
  ok "thư mục làm việc của lượt 2 trùng lượt 1 (để --resume tìm được phiên)" bash -c '[ "$(jq -r .cwd "$1")" = "$(jq -r .cwd "$2")" ]' _ "$(rec 1)" "$(rec 2)"

  api GET "/api/v1/ai/sessions/$SID1"
  show '{session: {id: .session.id, entry: .session.entry}, turns: [.turns[] | {seq, status, playbook, chips: [.chips[] | {key, content}], answer}], calls: [.calls[] | {seq, tool, plan_id}], plans: [.plans[] | {id, kind, origin, status}]}'
  cp "$TMPD/body" "$TMPD/session1.json"
  ok "GET /ai/sessions/{id}: phiên panel, 2 lượt done" bash -c '[ "$1" = 200 ] && jq -e ".session.entry == \"panel\" and ([.turns[].status] == [\"done\", \"done\"])" "$2" >/dev/null' _ "$CODE" "$TMPD/session1.json"
  ok "hai lượt có khối ngữ cảnh khác nhau: view của lượt 1 là trang site, lượt 2 là Sự cố; lượt 2 có trail 3 mục" \
    bash -c 'jq -e "([.turns[0].chips[] | select(.key == \"view\")][0].content | startswith(\"Trang site site-\")) and ([.turns[1].chips[] | select(.key == \"view\")][0].content == \"Sự cố (/incidents)\") and ([.turns[1].chips[] | select(.key == \"trail\")][0].content | split(\"\n\") | length == 3) and .turns[0].chips != .turns[1].chips" "$1" >/dev/null' _ "$TMPD/session1.json"
  ok "playbook: lượt 1 site, lượt 2 su-co" bash -c 'jq -e "[.turns[].playbook] == [\"site\", \"su-co\"]" "$1" >/dev/null' _ "$TMPD/session1.json"
  ok "các lần gọi công cụ: list_sites, propose_site_add (có plan_id $pid), list_sites" bash -c 'jq -e --arg p "$2" "[.calls[] | .tool] == [\"list_sites\", \"propose_site_add\", \"list_sites\"] and ([.calls[] | select(.plan_id == \$p)] | length == 1)" "$1" >/dev/null' _ "$TMPD/session1.json" "$pid"
  ok "Plan của phiên: $pid pending origin ai" bash -c 'jq -e --arg p "$2" "[.plans[] | select(.id == \$p and .status == \"pending\" and .origin == \"ai\")] | length == 1" "$1" >/dev/null' _ "$TMPD/session1.json" "$pid"
  ok "answer của lượt 1 trên trang phiên là toàn bộ chữ: có đoạn viết trước lần gọi công cụ đầu và đoạn cuối" bash -c 'jq -e ".turns[0].answer | startswith(\"Mình xem danh sách site trên máy trước.\n\n\") and contains(\"đang chờ bạn duyệt; máy chưa đổi gì.\")" "$1" >/dev/null' _ "$TMPD/session1.json"
  ok "câu trả lời trên trang phiên đã đổi về tên thật" bash -c 'jq -e "all(.turns[].answer; test(\"site-[a-z]+\\\\.example\") | not) and (.turns[1].answer | test(\"[ab]\\\\.example\\\\.com\"))" "$1" >/dev/null' _ "$TMPD/session1.json"

  section "i. claude giả khai thêm công cụ Bash ở init: lượt bị từ chối"
  fake_conf propose Bash
  local calls1
  calls1="$(ai_calls_count)"
  ask_body "$TMPD/ask3.json" "$SID1" "Tạo thêm một site nữa." /incidents "Sự cố" "$trail2"
  ask "$TMPD/ask3.json"
  ok "ask: 202, seq 3" bash -c '[ "$1" = 202 ] && [ "$2" = 3 ]' _ "$CODE" "$SEQ"
  sse "$SID1" 3 t3
  printf 'Thứ tự sự kiện: %s\n' "$(sse_kinds)"
  ok "lượt kết thúc bằng refused, lời từ chối nêu Bash" bash -c 'jq -e ".kind == \"refused\" and (.text | contains(\"Bash\"))" <<<"$1" >/dev/null' _ "$(sse_final)"
  ok "không có câu trả lời: không sự kiện text, tool_call, tool_result, done nào" bash -c 'jq -e "[.[].ev.kind] - [\"refused\"] == []" "$1" >/dev/null' _ "$SSE_JSON"
  run sleep 3
  OUT="$(jq '{resume, extra_tool, tools_tail: .tools[-2:]}' "$(rec 3)")"
  mask
  printf '%s\n' "$OUT"
  ok "claude giả đã khai Bash ở init" bash -c 'jq -e ".tools[-1] == \"Bash\"" "$1" >/dev/null' _ "$(rec 3)"
  ok "claude giả bị dừng trước khi gọi công cụ nào (không có dấu turn-3.toolcall)" test ! -e "$FAKEREC/turn-3.toolcall"
  ok "không có lần gọi công cụ mới nào trong ai_calls ($calls1 trước, $(ai_calls_count) sau)" test "$(ai_calls_count)" -eq "$calls1"
  api GET "/api/v1/ai/sessions/$SID1"
  show '[.turns[] | {seq, status, answer, error}]'
  ok "lượt 3 trên trang phiên: refused, answer rỗng" bash -c 'jq -e ".turns[2].status == \"refused\" and .turns[2].answer == \"\"" "$1" >/dev/null' _ "$TMPD/body"
  brain_show_log
  fake_conf propose ""

  section "j. bộ não thứ hai nối vào: bị từ chối, nêu tên máy của bộ não đang nối"
  brain_argv fake
  local raw2="$TMPD/brain-two.out"
  printf '\n[%s] $ %s\n' "$(ts)" "$(mask_str <<<"HOME=<tmp>/home PATH=<tmp>/fakebin:\$PATH ${BRAIN_ARGV[*]}")"
  env HOME="$TMPD/home" PATH="$FAKEBIN:$PATH" perl -e 'alarm 60; exec @ARGV' "${BRAIN_ARGV[@]}" >"$raw2" 2>&1
  RC=$?
  OUT="$(cat "$raw2")"
  mask
  printf '%s\n[%s] (mã thoát %s)\n' "$OUT" "$(ts)" "$RC"
  ok "bộ não thứ hai thoát khác 0" bash -c '[ "$1" -ne 0 ] && [ "$1" -ne 142 ]' _ "$RC"
  ok "lời từ chối nêu tên máy của bộ não đang nối (kiểm trên output chưa che)" bash -c 'grep -qF "panel đã có bộ não của máy $2" "$1"' _ "$raw2" "$MAC_HOST"
  api GET /api/v1/ai/brain
  show '{connected, host, since}'
  ok "bộ não đầu vẫn nối" bash -c 'jq -e --arg h "$2" ".connected == true and .host == \$h" "$1" >/dev/null' _ "$TMPD/body" "$MAC_HOST"
  # Hai bộ não cùng chạy trên một Mac nên cùng tên máy: lời từ chối phải nêu
  # đúng giờ nối của bộ não ĐẦU (since của /ai/brain), không phải giờ của bộ não mới.
  ok "lời từ chối nêu đúng giờ nối của bộ não đầu ($(jq -r '.since[:19] | sub("T"; " ")' "$TMPD/body"))" \
    bash -c 'grep -qF "nối từ $(jq -r ".since[:19] | sub(\"T\"; \" \")" "$2")" "$1"' _ "$raw2" "$TMPD/body"

  section "k. tắt tpan brain: panel báo chưa nối, hỏi bị từ chối 409 brain_offline"
  brain_stop
  ok "tpan brain thoát 0 sau Ctrl+C, in \"Bộ não: đã ngắt\"" bash -c '[ "$1" -eq 0 ] && grep -q "Bộ não: đã ngắt" "$2"' _ "$RC" "$BRAIN_LOG"
  local _
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    api GET /api/v1/ai/brain
    jq -e '.connected == false' "$TMPD/body" >/dev/null && break
    sleep 1
  done
  show
  ok "GET /ai/brain: chưa nối" bash -c 'jq -e ".connected == false and .mode == \"bridge\"" "$1" >/dev/null' _ "$TMPD/body"
  ask_body "$TMPD/ask4.json" "" "Máy có bao nhiêu site?" / "Bảng điều khiển" '[{"path":"/","label":"Bảng điều khiển"}]'
  ask "$TMPD/ask4.json"
  ok "ask khi chưa nối: 409 brain_offline" bash -c '[ "$1" = 409 ] && jq -e ".error.code == \"brain_offline\"" "$2" >/dev/null' _ "$CODE" "$TMPD/body"
  vroot tpan plan list
  ok "Plan $pid vẫn pending, origin ai (không ai duyệt)" bash -c 'grep -E "^$2 +pending +ai " <<<"$1" >/dev/null' _ "$OUT" "$pid"

  section "l. rò rỉ: ai_turns và ai_calls"
  leak_check "claude giả"

  section "kết quả đo cho mục 16"
  local m
  for m in "${M16[@]}"; do printf -- '- %s\n' "$(mask_str <<<"$m")"; done
}

# ---------------------------------------------------------------- Claude Code thật

# main_real <fake|real>: phần "Xong khi" của M8. Với "fake" là tập dượt đúng
# các bước đó bằng claude giả (không tốn hạn mức).
main_real() {
  local who=$1
  prepare

  section "e. lỗi cố ý ở $B (scripts/faults/07-vhost-disable.sh): bỏ symlink bật vhost rồi reload nginx (nginx -t vẫn qua); ghi kết quả health"
  run scripts/faults/07-vhost-disable.sh break "$B"
  ok "07 break: gỡ symlink, nginx -t qua, reload xong" rc_is 0
  # Reload của kịch bản trả về trước khi nginx áp dụng (khoảng 100 ms): chờ hiện tượng.
  vexec bash -c 'for i in $(seq 1 40); do c="$(curl -sS -m 5 -o /dev/null -w "%{http_code}" --resolve "$1:80:127.0.0.1" "http://$1/" 2>&1)"; rc=$?; echo "lần $i: $c (curl mã $rc)"; [ "$rc" = 52 ] && exit 52; sleep 0.25; done; exit "$rc"' _ "$B"
  ok "$B rơi vào site mặc định (curl mã 52)" rc_is 52
  http_code "$A"
  ok "$A vẫn 200" has '^200$'
  vroot tpan health
  ok "tpan health báo lỗi (mã khác 0) ở $B" bash -c '[ "$1" -ne 0 ] && grep -qF "$2" <<<"$3"' _ "$RC" "$B" "$OUT"

  section "f. tpan brain với claude $( [ "$who" = real ] && echo "THẬT (--model sonnet --max-turns 4)" || echo "giả (tập dượt)")"
  if [ "$who" = real ]; then
    run claude --version
  fi
  brain_start real "$who"
  ok "tpan brain nối được" bash -c 'grep -q "Bộ não: đã nối" "$1"' _ "$BRAIN_LOG"
  api GET /api/v1/ai/brain
  show
  ok "GET /ai/brain: connected, host là tên máy Mac, model sonnet" bash -c 'jq -e --arg h "$2" ".connected == true and .host == \$h and .model == \"sonnet\"" "$1" >/dev/null' _ "$TMPD/body" "$MAC_HOST"

  section "g. đang ở trang site $B, bấm Chẩn đoán: câu hỏi 1"
  view_json "/sites/$B" "Trang site $B" >"$TMPD/view-b.json"
  post_json /api/v1/ai/views "$TMPD/view-b.json"
  ok "POST /ai/views: 204" code_is 204
  local trail1 ptr
  trail1="$(jq -n --arg b "$B" '[{path: "/", label: "Bảng điều khiển"}, {path: "/sites/\($b)", label: "Trang site \($b)"}]')"
  ptr="$(jq -n --arg b "$B" '{kind: "site", ref: $b, label: "Site \($b)"}')"
  ask_body "$TMPD/r1.json" "" "Chẩn đoán vì sao site này lỗi rồi đề xuất cách sửa" "/sites/$B" "Trang site $B" "$trail1" "$ptr"
  post_json /api/v1/ai/preview "$TMPD/r1.json"
  show '{playbook, chips: [.chips[] | {key, content}]}'
  cp "$TMPD/body" "$TMPD/rpreview1.json"
  ok "preview: view, pointer, trail, evidence, machine; bằng chứng có phép kiểm Fail của site" bash -c 'jq -e "[.chips[].key] == [\"view\", \"pointer\", \"trail\", \"evidence\", \"machine\"] and ([.chips[] | select(.key == \"evidence\")][0].content | contains(\"[fail]\"))" "$1" >/dev/null' _ "$TMPD/rpreview1.json"
  ok "prompt xem trước không có $A, $B, IP máy ảo, hostname máy ảo" bash -c 'p="$(jq -r .prompt "$1")"; ! grep -qF "$2" <<<"$p" && ! grep -qF "$3" <<<"$p" && ! grep -qF "$4" <<<"$p" && ! grep -qF "$5" <<<"$p"' _ "$TMPD/rpreview1.json" "$A" "$B" "$VM_IP" "$VMHOST"
  vroot bash -c "$SNAP_SCRIPT"
  local before="$OUT"
  ask "$TMPD/r1.json"
  ok "ask: 202" code_is 202
  local SIDR="$SID"
  [ "$who" = real ] && printf '\n>>> LẦN GỌI claude -p THẬT: câu hỏi 1 (phiên %s), ghi vào bảng của docs/thay-doi/M8.md\n' "$SIDR"
  sse "$SIDR" 1 r1
  local final1 ntext1 first1
  final1="$(sse_final)"
  ntext1="$(jq '[.[] | select(.ev.kind == "text")] | length' "$SSE_JSON")"
  first1="$(sse_first_text_s)"
  printf 'Thứ tự sự kiện: %s\n' "$(sse_kinds)"
  ok "câu hỏi 1 kết thúc bằng done" bash -c 'jq -e ".kind == \"done\"" <<<"$1" >/dev/null' _ "$final1"
  ok "chữ sau mỗi lần gọi công cụ là đoạn mới (ngăn bằng dòng trống), khi có chữ ở cả hai phía" split_or_none
  ok "chữ hiện dần: $ntext1 sự kiện text, rải trong $(jq -r '[.[] | select(.ev.kind == "text") | .recv] | if length > 1 then (.[-1] - .[0]) * 1000 | floor / 1000 else 0 end' "$SSE_JSON")s" bash -c '[ "$1" -ge 2 ]' _ "$ntext1"
  M16+=("($who) câu hỏi 1: từ lúc gửi ask tới chữ đầu tiên trên SSE ${first1}s; tới done $(jq -r --argjson t0 "$T_ASK" '[.[] | select(.ev.kind == "done")][0].recv - $t0 | . * 1000 | floor / 1000' "$SSE_JSON")s; $ntext1 sự kiện text; công cụ: $(jq -c '[.[].ev | select(.kind == "tool_call") | .tool | sub("mcp__tpan__"; "")]' "$SSE_JSON")")
  api GET /api/v1/plans
  show '[.[] | {id, kind, target, origin, status, session_id, untrusted}]'
  ok "/api/v1/plans có Plan pending, origin ai, của phiên này" bash -c 'jq -e --arg s "$2" "[.[] | select(.status == \"pending\" and .origin == \"ai\" and .session_id == \$s)] | length >= 1" "$1" >/dev/null' _ "$TMPD/body" "$SIDR"
  vroot bash -c "$SNAP_SCRIPT"
  ok "MÁY KHÔNG ĐỔI GÌ sau câu hỏi 1 (chưa ai duyệt)" test "$OUT" = "$before"

  section "h. chuyển sang trang site $A rồi hỏi tiếp: câu hỏi 2"
  view_json "/sites/$A" "Trang site $A" >"$TMPD/view-a.json"
  post_json /api/v1/ai/views "$TMPD/view-a.json"
  local trail2
  trail2="$(jq -n --arg a "$A" --arg b "$B" '[{path: "/", label: "Bảng điều khiển"}, {path: "/sites/\($b)", label: "Trang site \($b)"}, {path: "/sites/\($a)", label: "Trang site \($a)"}]')"
  ask_body "$TMPD/r2.json" "$SIDR" "còn site kia thì sao?" "/sites/$A" "Trang site $A" "$trail2"
  ask "$TMPD/r2.json"
  ok "ask cùng phiên: 202, seq 2" bash -c '[ "$1" = 202 ] && [ "$2" = 2 ]' _ "$CODE" "$SEQ"
  [ "$who" = real ] && printf '\n>>> LẦN GỌI claude -p THẬT: câu hỏi 2 (phiên %s), ghi vào bảng của docs/thay-doi/M8.md\n' "$SIDR"
  sse "$SIDR" 2 r2
  ok "câu hỏi 2 kết thúc bằng done" bash -c 'jq -e ".kind == \"done\"" <<<"$1" >/dev/null' _ "$(sse_final)"
  M16+=("($who) câu hỏi 2 (--resume): từ lúc gửi ask tới chữ đầu tiên $(sse_first_text_s)s; $(jq '[.[] | select(.ev.kind == "text")] | length' "$SSE_JSON") sự kiện text")

  api GET "/api/v1/ai/sessions/$SIDR"
  show '{turns: [.turns[] | {seq, status, playbook, chips: [.chips[] | {key, content}], answer}], calls: [.calls[] | {seq, tool, plan_id}], plans: [.plans[] | {id, kind, origin, status}]}'
  cp "$TMPD/body" "$TMPD/rsession.json"
  ok "trang phiên: 2 lượt done, view lượt 1 là trang site $B (bí danh), lượt 2 là trang site $A (bí danh khác)" \
    bash -c 'jq -e "([.turns[].status] == [\"done\", \"done\"]) and ([.turns[] | [.chips[] | select(.key == \"view\")][0].content] | (.[0] != .[1]) and all(.[]; startswith(\"Trang site site-\")))" "$1" >/dev/null' _ "$TMPD/rsession.json"
  ok "lượt 2 có trail 3 mục chứa trang site của lượt 1" bash -c 'jq -e "([.turns[0].chips[] | select(.key == \"view\")][0].content) as \$v1 | [.turns[1].chips[] | select(.key == \"trail\")][0].content | (split(\"\n\") | length == 3) and contains(\$v1)" "$1" >/dev/null' _ "$TMPD/rsession.json"
  ok "trang phiên có các lần gọi công cụ và Plan" bash -c 'jq -e "(.calls | length >= 1) and (.plans | length >= 1)" "$1" >/dev/null' _ "$TMPD/rsession.json"
  OUT="$(jq -r '.turns[] | "--- lượt \(.seq) (hiển thị, tên thật):\n\(.answer)"' "$TMPD/rsession.json")"
  mask
  printf '%s\n' "$OUT"

  brain_stop
  ok "tpan brain thoát 0 sau Ctrl+C" rc_is 0

  section "i. rò rỉ: ai_turns và ai_calls"
  leak_check "claude $who"

  section "kết quả đo cho mục 16"
  local m
  for m in "${M16[@]}"; do printf -- '- %s\n' "$(mask_str <<<"$m")"; done
}

case "$MODE" in
  fake) main_fake ;;
  real-rehearse) main_real fake ;;
  real) main_real real ;;
esac

if [ -n "${E2E_LOG:-}" ] && [ -f "$E2E_LOG" ]; then
  section "bí mật, IP, tên máy, tên user Mac có lọt vào chính file log không"
  n=0
  while IFS= read -r s; do
    [ -n "$s" ] || continue
    n=$((n + $(grep -cF -- "$s" "$E2E_LOG")))
  done <"$SECRETS"
  printf 'Số bí mật đã gặp: %s; số dòng của log chứa một trong số đó: %s\n' "$(grep -c . "$SECRETS")" "$n"
  ok "log không chứa mật khẩu nào" test "$n" -eq 0
  ok "log không chứa IP máy ảo" bash -c '! grep -qF -- "$1" "$2"' _ "$VM_IP" "$E2E_LOG"
  ok "log không chứa IP của Mac" bash -c '! grep -qF -- "$1" "$2"' _ "$HOST_IP" "$E2E_LOG"
  ok "log không chứa tên máy Mac" bash -c '! grep -qF -- "$1" "$2"' _ "$MAC_HOST_S" "$E2E_LOG"
  ok "log không chứa thư mục nhà hay tên user Mac" bash -c '! grep -qF -- "$1" "$3" && ! grep -qF -- "$2" "$3"' _ "$HOME" "$USER" "$E2E_LOG"
  ok "log không chứa giá trị cookie nào" bash -c '! grep -iE "^set-cookie: [^=]+=" "$1" | grep -vqE "=<đã che>"' _ "$E2E_LOG"
fi

section "TỔNG KẾT: $FAILS khẳng định không đạt"
[ "$FAILS" -eq 0 ]
