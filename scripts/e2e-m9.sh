#!/usr/bin/env bash
# e2e của mốc M9 (backup, khôi phục, cron WordPress, thao tác DB) trên máy ảo
# của mốc (tên trong file .tpan-vm; scripts/vm.sh tự đọc). Chạy trên Mac, trong
# thư mục repo:
#
#   scripts/e2e-m9.sh          chạy hết từ máy sạch (mặc định, như "all")
#   scripts/e2e-m9.sh all
#
# Thứ tự (khác thứ tự chữ cái vì các phần phụ thuộc nhau):
#   0  máy sạch, triển khai tpan
#   A  stack nginx; hai site m9a, m9b trả 200
#   H0 health trước bản sao tpan.db đầu tiên: tpan.backup:db là Warn
#   B  cron WordPress (mục 6.10): file /etc/cron.d/tpan-<slug>, cron active,
#      DISABLE_WP_CRON; một bài hẹn giờ được đăng khi không có khách nào vào
#      site (access.log không đổi), cron list có lần chạy ok. Chạy TRƯỚC khi
#      có tpan serve để không gì gọi site.
#   C  e2e bước 10: backup m9a, sửa file và DB, restore: về như lúc backup;
#      sha256, bản before-restore, thư mục khôi phục đã gỡ, quyền và chủ
#   D  chặn đầy đĩa: scripts/faults/06-disk-full.sh, backup và xuất DB bị từ
#      chối disk_space_low, không file nào được tạo; dọn file giả
#   E  DB (mục 6.9): đổi mật khẩu (site 200, mật khẩu mới không ở tpan.db, WAL
#      hay output), xuất, sửa, nhập lại, bản before-import
#   F  thùng rác: rm m9b gỡ cron, cron run ghi skipped, backup từ thùng rác,
#      purge có bản before-purge, file cron không còn
#   G  nâng MariaDB đòi backup: site mới m9c chưa backup, --dry-run bị từ chối
#      backup_required; sau backup create --all thì --dry-run qua
#   H  bản sao tpan.db (Pass, 0600), lịch backup --at 03:30 rồi --off
#   I  API (tpan setup, đăng nhập): GET /backups, GET /cron, POST
#      /sites/{d}/backups?dry_run=1; cron run chạy cùng lúc với tpan serve
#
# Đặt E2E_LOG=<file log> để cuối lần chạy script tự tìm trong chính file log
# đó: mọi mật khẩu đã gặp, IP máy ảo (IPv4 và IPv6), IP của Mac, thư mục nhà và
# tên user Mac, giá trị cookie (phải là 0 lần).
#
# Output như e2e-m6.sh: mỗi lệnh có dấu thời gian, output nguyên văn (đã che)
# và mã thoát; mỗi khẳng định in "=> ĐẠT" hoặc "=> KHÔNG ĐẠT"; cuối cùng in số
# khẳng định đạt và không đạt.
# Không dùng set -e: nhiều lệnh ở đây được chờ là phải lỗi.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2

VM=scripts/vm.sh
FAILS=0
PASSES=0
OUT=""
RC=0
EMAIL=admin@example.com
A=m9a.example.com
B=m9b.example.com
C=m9c.example.com
MDB_LINE=11.8
TMPD="$(mktemp -d)"
chmod 700 "$TMPD"
SECRETS="$TMPD/secrets"
: >"$SECRETS"
chmod 600 "$SECRETS"
HJ="$TMPD/health.json"
FILL_ON=0
# Dọn file lấp đĩa nếu script dừng giữa phần D (không để máy ảo đầy đĩa).
cleanup() {
  [ "$FILL_ON" = 1 ] && scripts/faults/06-disk-full.sh restore </dev/null >/dev/null 2>&1
  rm -rf "$TMPD"
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
  s="${s//"$TMPD"/<tmp>}"
  s="${s//"$HOME"/~}"
  s="${s//"$USER"/<user>}"
  while IFS= read -r p; do
    [ -n "$p" ] && s="${s//"$p"/<mật khẩu, đã che>}"
  done <"$SECRETS"
  s="$(sed -E -e 's/^([Ss]et-[Cc]ookie: [^=]+=)[^;]*/\1<đã che>/' -e 's/inet6 [0-9a-f:]{3,}/inet6 <vm-v6>/g' <<<"$s")"
  printf '%s' "$s"
}

mask() { OUT="$(mask_str <<<"$OUT")"; }

# runq chạy lệnh, giữ output thô trong $TMPD/run.out, KHÔNG in gì: dùng khi
# output có thể chứa một bí mật chưa biết (mật khẩu DB mới), để đọc bí mật vào
# danh sách che trước rồi mới in bằng show_last.
runq() {
  RUNQ_CMD="$(mask_str <<<"$*")"
  RUNQ_TS="$(ts)"
  # Output vào file rồi mới đọc, không qua pipe: `multipass exec` (bản trên
  # Mac) thỉnh thoảng treo khi stdout là pipe và lệnh in nhiều (xem e2e-m6.sh).
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

rc_is() { [ "$RC" -eq "$1" ]; }
rc_not() { [ "$RC" -ne "$1" ]; }
has() { grep -qE -- "$1" <<<"$OUT"; }
hasnt() { ! grep -qE -- "$1" <<<"$OUT"; }
hasF() { grep -qF -- "$1" <<<"$OUT"; }
out_is() { [ "$(tr -d '\r' <<<"$OUT")" = "$1" ]; }
count_is() { [ "$(tr -d '\r' <<<"$OUT" | grep -cE -- "$2")" -eq "$1" ]; }
job_ok() { bash -c '[ "$2" = 0 ] && grep -q "^Job .*: thành công" <<<"$1"' _ "$OUT" "$RC"; }
# jout <biểu thức jq>: jq -e trên OUT (output --json của tpan).
jout() { jq -e "$@" <<<"$OUT" >/dev/null 2>&1; }

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
vm_now() { "$VM" exec date '+%Y-%m-%d %H:%M:%S' </dev/null | tr -d '\r'; }

# dbpw <domain>: mật khẩu DB trong wp-config.php (không in).
dbpw() {
  "$VM" root bash -c 'cd / && runuser -u "$2" -- wp config get DB_PASSWORD --path="/var/www/$1/public" 2>/dev/null' _ "$1" "$(user_of "$1")" </dev/null | tr -d '\r'
}

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

# Job mới nhất của một loại: trạng thái, thời gian, số sự kiện, các lệnh.
JOB_PY='
import sqlite3, sys, datetime
c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True)
print("số job trong tpan.db:", c.execute("SELECT COUNT(*) FROM jobs").fetchone()[0])
if len(sys.argv) < 2:
    sys.exit(0)
j = c.execute("SELECT id, status, target, started_at, finished_at, error FROM jobs WHERE kind = ? ORDER BY started_at DESC, rowid DESC LIMIT 1", (sys.argv[1],)).fetchone()
if j is None:
    print("không có job", sys.argv[1])
    sys.exit(0)
print("job", sys.argv[1], j[0], j[1], "| target:", j[2])
print("bắt đầu:", j[3], "| xong:", j[4])
try:
    p = lambda s: datetime.datetime.fromisoformat(s.replace("Z", "+00:00"))
    print("thời gian job: %.1f giây" % (p(j[4]) - p(j[3])).total_seconds())
except Exception as e:
    print("không tính được thời gian job:", e)
if j[5]:
    print("lỗi của job:", j[5][:400].replace("\n", " "))
n, size = c.execute("SELECT COUNT(*), COALESCE(SUM(LENGTH(message)), 0) FROM job_events WHERE job_id = ?", (j[0],)).fetchone()
print("số sự kiện của job:", n, "| tổng cỡ message:", size, "byte")
for kind, cnt in c.execute("SELECT kind, COUNT(*) FROM job_events WHERE job_id = ? GROUP BY kind ORDER BY kind", (j[0],)):
    print("  sự kiện", kind, cnt)
for kind, step, msg in c.execute("SELECT kind, step_id, message FROM job_events WHERE job_id = ? AND kind IN (\"command\", \"step.fail\", \"undo.done\", \"undo.fail\") ORDER BY seq", (j[0],)):
    print(" ", kind, step or "-", msg[:300].replace("\n", " "))
'

# Dòng cron_runs của mọi site.
CRON_PY='
import sqlite3
c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True)
for r in c.execute("SELECT * FROM cron_runs"):
    print(*[str(x).replace("\n", " ")[:300] for x in r])
'

# mã HTTP, tiêu đề trang chủ từng site.
PAGES_SCRIPT='
for d in "$@"; do
  body=$(curl -sS -m 20 --resolve "$d:80:127.0.0.1" -w "\n%{http_code}" "http://$d/" 2>/dev/null); rc=$?
  code=$(printf "%s" "$body" | tail -1)
  title=$(printf "%s" "$body" | grep -o "<title>[^<]*</title>" | head -1)
  echo "$d curl=$rc http=$code $title"
done
'

# Dấu vân tay nội dung của public: sha256 của (đường dẫn, sha256 từng file).
TREE_SCRIPT='
cd "$1" && find . -type f -print0 | sort -z | xargs -0 sha256sum | sha256sum | cut -c1-64
find . -type f | wc -l
'

# Dấu vân tay quyền và chủ của public: (quyền, chủ:nhóm, loại, đường dẫn).
PERM_SCRIPT='
cd "$1" && find . -printf "%m %u:%g %y %p\n" | sort | sha256sum | cut -c1-64
find . -printf "%m %u:%g %y\n" | sort | uniq -c
'

# Kiểm quyền theo mục 5: chủ <user>:www-data, thư mục 2750, file 640,
# wp-config.php 600. In từng chỗ sai (tối đa 20) và số đếm.
PERMCHECK_SCRIPT='
pub=$1; u=$2
echo "sai chủ: $(find "$pub" \( ! -user "$u" -o ! -group www-data \) | tee /tmp/e2e-perm1 | wc -l)"; head -5 /tmp/e2e-perm1
echo "thư mục khác 2750: $(find "$pub" -type d ! -perm 2750 | tee /tmp/e2e-perm2 | wc -l)"; head -5 /tmp/e2e-perm2
echo "file khác 640 (trừ wp-config.php): $(find "$pub" -type f ! -path "$pub/wp-config.php" ! -perm 640 | tee /tmp/e2e-perm3 | wc -l)"; head -5 /tmp/e2e-perm3
echo "symlink: $(find "$pub" -type l | wc -l)"
stat -c "wp-config.php %a %U:%G" "$pub/wp-config.php"
rm -f /tmp/e2e-perm1 /tmp/e2e-perm2 /tmp/e2e-perm3
'

# ---------------------------------------------------------------- health

health_json() {
  printf '\n[%s] $ %s root tpan health --json\n' "$(ts)" "$VM"
  "$VM" root tpan health --json </dev/null >"$TMPD/health.raw" 2>"$TMPD/health.err"
  RC=$?
  tr -d '\r' <"$TMPD/health.raw" >"$HJ"
  OUT="$(cat "$TMPD/health.err")"
  mask
  [ -n "$OUT" ] && printf 'stderr:\n%s\n' "$OUT"
  if ! jq -e .groups "$HJ" >/dev/null 2>&1; then
    OUT="$(head -c 4000 "$HJ")"
    mask
    printf 'stdout không phải báo cáo JSON:\n%s\n[%s] (mã thoát %s)\n' "$OUT" "$(ts)" "$RC"
    return
  fi
  OUT="$(jq -r '"started \(.started)  finished \(.finished)",
    "nhóm: \([.groups[] | if .target == "" then "(máy, stack)" else .target end] | join(", "))",
    "số phép kiểm theo trạng thái: \([.groups[].results[].status] | group_by(.) | map("\(.[0])=\(length)") | join(" "))",
    ([.groups[].results[] | select(.status != "pass")] | if length == 0 then "(mọi phép kiểm đạt)" else (.[] |
      "--- \(.status) \(.id) (chặng \(.stage)): \(.summary.vi)") end),
    ([.groups[].results[] | select(.id == "tpan.backup:db")][] | "tpan.backup:db = \(.status): \(.summary.vi)"),
    "sự cố vừa mở: \([.changes.opened[]? | "#\(.id) \(.check_id)"] | join(", "))"' "$HJ")"
  mask
  printf '%s\n[%s] (mã thoát %s)\n' "$OUT" "$(ts)" "$RC"
}
hq() { jq -e "$@" "$HJ" >/dev/null 2>&1; }
check_status() { hq --arg id "$1" --arg st "$2" '[.groups[].results[] | select(.id == $id and .status == $st)] | length == 1'; }
no_fail() { hq '[.groups[].results[] | select(.status == "fail")] | length == 0'; }

# ---------------------------------------------------------------- site

site_add() {
  local d=$1 pw
  runq "$VM" root tpan site add "$d" --email "$EMAIL" --yes
  pw="$(tr -d '\r' <"$TMPD/run.out" | sed -nE 's/^  Mật khẩu: +([A-Za-z0-9]{32})$/\1/p' | head -1)"
  add_secret "$pw"
  add_secret "$(dbpw "$d")"
  show_last
  ok "site add $d thoát 0, job thành công" job_ok
  ok "$d: đọc được mật khẩu quản trị (đã che)" test -n "$pw"
}

# backups_json <domain>: tpan backup list <domain> --json vào $TMPD/bl.json, in tóm tắt.
backups_json() {
  run "$VM" root tpan backup list "$@" --json
  tr -d '\r' <"$TMPD/run.out" >"$TMPD/bl.json"
  OUT="$(jq -r '.[] | "\(.id) \(.domain) \(.kind) \(.reason) \(.size) \(.sha256) \(.path)"' "$TMPD/bl.json" 2>&1)"
  mask
  printf '(tóm tắt: id domain kind reason size sha256 path)\n%s\n' "$OUT"
}
blq() { jq -e "$@" "$TMPD/bl.json" >/dev/null 2>&1; }
blr() { jq -r "$@" "$TMPD/bl.json" 2>/dev/null; }

# ---------------------------------------------------------------- panel (từ Mac)

JAR="$TMPD/jar"
CODE=""
MUT=(-H 'Accept: application/json' -H 'Sec-Fetch-Site: same-origin')
api() {
  local m=$1 p=$2
  shift 2
  : >"$TMPD/body"
  run curl -sS -m 120 --cacert "$TMPD/cert.pem" -b "$JAR" -c "$JAR" -X "$m" -D - -o "$TMPD/body" \
    -w '-> HTTP %{http_code}, %{time_total}s\n' "$@" "$BASE$p"
  CODE="$(sed -nE 's/^-> HTTP ([0-9]{3}),.*/\1/p' <<<"$OUT" | tail -1)"
}
code_is() { [ "$CODE" = "$1" ]; }
showb() {
  OUT="$(jq "${1:-.}" "$TMPD/body" 2>&1)"
  mask
  printf '%s\n' "$OUT"
}
bq() { jq -e "$@" "$TMPD/body" >/dev/null 2>&1; }

panel_setup_login() {
  section "I: tpan setup (panel nghe 8443) rồi đăng nhập từ Mac"
  local pw
  runq "$VM" root tpan setup --yes
  pw="$(tr -d '\r' <"$TMPD/run.out" | grep -A1 '^Mật khẩu quản trị (chỉ hiện MỘT lần' | sed -nE 's/^    ([A-Z2-7]{26})$/\1/p' | head -1)"
  add_secret "$pw"
  (umask 077 && printf '%s' "$pw" | jq -Rs '{password: .}' >"$TMPD/login.json")
  show_last
  ok "setup thoát 0" rc_is 0
  ok "đọc được mật khẩu panel (đã che)" test -n "$pw"
  "$VM" root cat /etc/tpan/tls/cert.pem </dev/null | tr -d '\r' >"$TMPD/cert.pem"
  : >"$JAR"
  chmod 600 "$JAR"
  local i
  for i in $(seq 1 20); do
    curl -sS -m 5 --cacert "$TMPD/cert.pem" -o /dev/null "$BASE/api/v1/health" 2>/dev/null && break
    sleep 1
  done
  api POST /api/v1/login -H 'Content-Type: application/json' --data-binary "@$TMPD/login.json"
  ok "đăng nhập panel: 204" code_is 204
  MUT=(-H 'Accept: application/json' -H 'Sec-Fetch-Site: same-origin' -H "Origin: $BASE")
}

# ================================================================ các phần

UA="" UB="" SA="" SB="" PUBA=""

part_0() {
  section "0: máy sạch, triển khai tpan"
  reset_vm
  deploy
}

part_a() {
  section "A: tpan stack install --web nginx"
  vroot tpan stack install --web nginx --yes
  ok "stack install thoát 0, job thành công" job_ok
  vexec systemctl is-active nginx php8.3-fpm mariadb cron
  ok "nginx, php8.3-fpm, mariadb, cron active" count_is 4 '^active$'

  section "A: tạo hai site $A, $B"
  site_add "$A"
  site_add "$B"
  vexec bash -c "$PAGES_SCRIPT" _ "$A" "$B"
  ok "$A và $B trả 200, tiêu đề là domain của mình" bash -c 'grep -qE "^$2 curl=0 http=200 <title>$2</title>" <<<"$1" && grep -qE "^$3 curl=0 http=200 <title>$3</title>" <<<"$1"' _ "$OUT" "$A" "$B"
}

part_h0() {
  section "H0: health trước bản sao tpan.db đầu tiên"
  vroot bash -c 'ls -la /var/lib/tpan/backups/_tpan 2>&1; ls -la /var/lib/tpan/backups 2>&1'
  health_json
  ok "tpan.backup:db là Warn (chưa có bản sao nào)" check_status tpan.backup:db warn
  ok "health không có Fail" no_fail
}

part_b() {
  section "B: cron WordPress của $A (mục 6.10)"
  local cf="/etc/cron.d/tpan-$SA"
  vroot stat -c '%U:%G %a %n' "$cf"
  ok "$cf có, root:root 0644" out_is "root:root 644 $cf"
  vroot cat "$cf"
  ok "đúng một dòng gọi /usr/local/bin/tpan cron run $A" count_is 1 "^[^#].* root /usr/local/bin/tpan cron run $A( |\$)"
  ok "dòng đó chạy mỗi 5 phút" has "^\*/5 \* \* \* \* root /usr/local/bin/tpan cron run $A "
  ok "không có dòng lệnh nào khác (ngoài chú thích)" count_is 1 '^[^#[:space:]]'
  vexec systemctl is-active cron
  ok "systemctl is-active cron: active" out_is active
  vroot bash -c 'cd / && runuser -u "$2" -- wp config get DISABLE_WP_CRON --type=constant --path="/var/www/$1/public"' _ "$A" "$UA"
  ok "wp config get DISABLE_WP_CRON --type=constant (user của site) in 1" out_is 1
  vroot bash -c 'journalctl -u cron --no-pager -o short-iso | grep -E "tpan-|RELOAD|tpan cron run" | tail -20'
  printf '(mục 16) cron đọc file mới trong /etc/cron.d không cần reload: các dòng trên là bằng chứng\n'

  section "B: bài hẹn giờ khoảng 2 phút sau (giờ của WordPress), không ai gọi site"
  vroot bash -c 'cd / && runuser -u "$2" -- wp eval "echo wp_date(\"Y-m-d H:i:s\", time() + 120), \"|\", wp_timezone_string(), \"|\", current_time(\"mysql\");" --path="/var/www/$1/public"' _ "$A" "$UA"
  local PD
  PD="$(cut -d'|' -f1 <<<"$OUT" | tr -d ' \r\n')"
  PD="${PD:0:10} ${PD:10}"
  ok "đọc được giờ hẹn theo WordPress ($PD)" bash -c '[[ "$1" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}\ [0-9]{2}:[0-9]{2}:[0-9]{2}$ ]]' _ "$PD"
  vroot bash -c 'cd / && runuser -u "$2" -- wp post create --post_title="e2e-m9 bai hen gio" --post_status=future --post_date="$3" --porcelain --path="/var/www/$1/public"' _ "$A" "$UA" "$PD"
  local PID
  PID="$(tr -d '\r\n' <<<"$OUT")"
  ok "wp post create --post_status=future trả id" bash -c '[[ "$1" =~ ^[0-9]+$ ]]' _ "$PID"
  local Q="SELECT post_status, post_date, post_date_gmt FROM wp_posts WHERE ID = $PID"
  vroot mariadb -N -B "$UA" -e "$Q"
  ok "bài đang ở trạng thái future" has '^future	'
  vroot bash -c 'cd / && runuser -u "$2" -- wp cron event list --fields=hook,next_run_gmt,next_run_relative --path="/var/www/$1/public" | grep -E "hook|publish_future_post"' _ "$A" "$UA"
  ok "có việc publish_future_post trong lịch của WordPress" hasF publish_future_post
  local logf="/var/log/tpan/sites/$A/access.log"
  vroot wc -l "$logf"
  local L0
  L0="$(awk '{print $1}' <<<"$OUT")"
  local T0
  T0="$(vm_now)"
  printf 'GIỜ HẸN (giờ WordPress): %s; giờ máy ảo lúc tạo bài: %s; số dòng access.log: %s\n' "$PD" "$T0" "$L0"

  section "B: chờ tối đa 7 phút 30 giây, chỉ đọc DB (không gọi site)"
  local i st="" TP=""
  for i in $(seq 1 30); do
    sleep 15
    st="$("$VM" root mariadb -N -B "$UA" -e "SELECT post_status FROM wp_posts WHERE ID = $PID" </dev/null | tr -d '\r')"
    printf '[%s] lần %s: post_status=%s\n' "$(ts)" "$i" "$st"
    if [ "$st" = publish ]; then
      TP="$(vm_now)"
      break
    fi
  done
  vroot mariadb -N -B "$UA" -e "$Q"
  ok "bài đã chuyển publish" has '^publish	'
  printf 'GIỜ HẸN: %s; THẤY ĐĂNG LÚC (giờ máy ảo, lần đọc DB đầu tiên thấy publish): %s\n' "$PD" "${TP:-chưa đăng}"
  vroot wc -l "$logf"
  ok "access.log của $A không đổi số dòng (vẫn $L0): không khách nào vào" bash -c '[ "$(awk "{print \$1}" <<<"$1")" = "$2" ]' _ "$OUT" "$L0"
  vroot tail -3 "$logf"
  vroot tpan cron list --json
  ok "cron list --json: $A bật, lần chạy gần nhất status ok" jout --arg d "$A" '.sites[] | select(.domain == $d) | .enabled and .last.status == "ok" and .last.exit_code == 0'
  vroot tpan cron list
  vroot python3 -c "$CRON_PY"
  vroot bash -c 'journalctl -u cron --no-pager -o short-iso --since "-10 min" | grep -E "tpan cron run" | tail -6'
  ok "journal của cron có dòng CMD chạy tpan cron run $A" hasF "tpan cron run $A"

  section "B: (mục 16) mã thoát của wp cron event run --due-now khi không có việc"
  vroot bash -c 'cd / && runuser -u "$2" -- wp cron event run --due-now --path="/var/www/$1/public"; echo "mã thoát: $?"' _ "$A" "$UA"
  vroot bash -c 'cd / && runuser -u "$2" -- wp cron event run --due-now --path="/var/www/$1/public"; echo "mã thoát (lần hai): $?"' _ "$A" "$UA"
  ok "lần hai: không còn việc tới hạn (0 cron events), mã thoát 0" bash -c 'grep -q "Executed a total of 0 cron events" <<<"$1" && grep -q "^mã thoát (lần hai): 0$" <<<"$1"' _ "$OUT"
  vroot bash -c 'cd / && runuser -u "$2" -- wp cron event run --due-now --quiet --path="/var/www/$1/public"; echo "mã thoát (--quiet): $?"' _ "$A" "$UA"
}

part_c() {
  section "C: e2e bước 10: dấu trước backup"
  local MARK1 MARK2
  MARK1="e2e-m9-truoc-backup-$(date +%s)"
  MARK2="e2e-m9-sau-backup-$(date +%s)"
  vroot bash -c 'printf "%s\n" "$3" | runuser -u "$2" -- tee "/var/www/$1/public/e2e-m9.txt" >/dev/null && chmod 640 "/var/www/$1/public/e2e-m9.txt" && stat -c "%a %U:%G %n" "/var/www/$1/public/e2e-m9.txt"' _ "$A" "$UA" "$MARK1"
  ok "ghi file dấu (user của site, 640)" rc_is 0
  vroot bash -c 'cd / && runuser -u "$2" -- wp post update 1 --post_title="e2e-m9 tieu de luc backup" --path="/var/www/$1/public" && runuser -u "$2" -- wp post get 1 --field=post_title --path="/var/www/$1/public"' _ "$A" "$UA"
  ok "đặt tiêu đề bài 1" has '^e2e-m9 tieu de luc backup$'
  vroot bash -c "$TREE_SCRIPT" _ "$PUBA"
  local TREE0="$OUT"
  vroot bash -c "$PERM_SCRIPT" _ "$PUBA"
  local PERM0
  PERM0="$(head -1 <<<"$OUT")"
  vroot bash -c "$PERMCHECK_SCRIPT" _ "$PUBA" "$UA"
  ok "trước backup: quyền và chủ đúng mục 5 (đối chứng)" bash -c 'grep -q "^sai chủ: 0$" <<<"$1" && grep -q "^thư mục khác 2750: 0$" <<<"$1" && grep -q "^file khác 640 (trừ wp-config.php): 0$" <<<"$1" && grep -q "^wp-config.php 600 " <<<"$1"' _ "$OUT"

  section "C: tpan backup create $A --yes"
  vroot tpan backup create "$A" --yes
  ok "backup create thoát 0, job thành công" job_ok
  vroot python3 -c "$JOB_PY" backup.create
  backups_json "$A"
  ok "có đúng 1 bản full manual của $A" blq '[.[] | select(.kind == "full" and .reason == "manual")] | length == 1'
  local BID BPATH BSHA
  BID="$(blr '[.[] | select(.kind == "full" and .reason == "manual")][0].id')"
  BPATH="$(blr '[.[] | select(.kind == "full" and .reason == "manual")][0].path')"
  BSHA="$(blr '[.[] | select(.kind == "full" and .reason == "manual")][0].sha256')"
  vroot bash -c 'sha256sum "$1"; stat -c "%a %U:%G %s %n" "$1" "$(dirname "$1")" /var/lib/tpan/backups; tar -tzf "$1" | grep -vE "^public/." ; echo "số mục trong tar: $(tar -tzf "$1" | wc -l)"; tar -xOzf "$1" manifest.json' _ "$BPATH"
  ok "sha256sum file backup khớp backup list --json" has "^$BSHA  $BPATH\$"
  ok "file backup 0600 root, thư mục 0700 root" bash -c 'grep -qE "^600 root:root [0-9]+ $2\$" <<<"$1" && grep -qE "^700 root:root [0-9]+ $(dirname "$2")\$" <<<"$1"' _ "$OUT" "$BPATH"
  ok "tar có manifest.json, db.sql, public/" bash -c 'grep -qx "manifest.json" <<<"$1" && grep -qx "db.sql" <<<"$1" && grep -qx "public/" <<<"$1"' _ "$OUT"

  section "C: sửa nội dung sau backup (file và DB)"
  vroot bash -c 'printf "%s\n" "$3" | runuser -u "$2" -- tee "/var/www/$1/public/e2e-m9.txt" >/dev/null; printf "moi\n" | runuser -u "$2" -- tee "/var/www/$1/public/e2e-m9-moi.txt" >/dev/null; cd / && runuser -u "$2" -- wp post update 1 --post_title="e2e-m9 tieu de da sua" --path="/var/www/$1/public"' _ "$A" "$UA" "$MARK2"
  vexec curl -sS -m 20 --resolve "$A:80:127.0.0.1" "http://$A/e2e-m9.txt"
  ok "site đang trả nội dung đã sửa" out_is "$MARK2"
  vroot mariadb -N -B "$UA" -e "SELECT post_title FROM wp_posts WHERE ID = 1"
  ok "DB đang có tiêu đề đã sửa" out_is 'e2e-m9 tieu de da sua'

  section "C: tpan backup restore $A --yes"
  vroot tpan backup restore "$A" --dry-run
  ok "restore --dry-run thoát 0" rc_is 0
  vroot tpan backup restore "$A" --yes
  ok "backup restore thoát 0, job thành công" job_ok
  vroot python3 -c "$JOB_PY" backup.restore
  printf '(mục 16 / M9.md) số sự kiện của job khôi phục ở trên: tar -tzvf đẩy mỗi file một dòng vào log job\n'
  vexec curl -sS -m 20 --resolve "$A:80:127.0.0.1" "http://$A/e2e-m9.txt"
  ok "file về như lúc backup (qua HTTP)" out_is "$MARK1"
  vexec curl -sS -m 20 -o /dev/null -w '%{http_code}\n' --resolve "$A:80:127.0.0.1" "http://$A/e2e-m9-moi.txt"
  ok "file tạo sau backup không còn (404)" out_is 404
  vroot mariadb -N -B "$UA" -e "SELECT post_title FROM wp_posts WHERE ID = 1"
  ok "DB về như lúc backup (tiêu đề bài 1)" out_is 'e2e-m9 tieu de luc backup'
  vroot bash -c "$TREE_SCRIPT" _ "$PUBA"
  ok "cây public giống hệt lúc backup (sha256 từng file)" test "$OUT" = "$TREE0"
  vexec bash -c "$PAGES_SCRIPT" _ "$A"
  ok "$A trả 200" has "^$A curl=0 http=200 "
  backups_json "$A"
  ok "bản backup cũ còn, sha256 vẫn vậy" blq --arg id "$BID" --arg s "$BSHA" '[.[] | select(.id == $id and .sha256 == $s)] | length == 1'
  ok "có thêm bản before-restore (full)" blq '[.[] | select(.kind == "full" and .reason == "before-restore")] | length == 1'
  local RPATH
  RPATH="$(blr '[.[] | select(.reason == "before-restore")][0].path')"
  vroot bash -c 'ls -la "$1"; sha256sum "$1"; echo "== /var/lib/tpan/restore"; ls -la /var/lib/tpan/restore 2>&1; ls -d "/var/lib/tpan/restore/$2" 2>&1' _ "$RPATH" "$SA"
  ok "file before-restore có, sha256 khớp" has "^$(blr '[.[] | select(.reason == "before-restore")][0].sha256')  $RPATH\$"
  ok "RestoreDir /var/lib/tpan/restore/$SA đã được gỡ" hasF "cannot access '/var/lib/tpan/restore/$SA': No such file"
  vroot bash -c "$PERMCHECK_SCRIPT" _ "$PUBA" "$UA"
  ok "sau khôi phục: chủ $UA:www-data, thư mục 2750, file 640, wp-config.php 600" bash -c 'grep -q "^sai chủ: 0$" <<<"$1" && grep -q "^thư mục khác 2750: 0$" <<<"$1" && grep -q "^file khác 640 (trừ wp-config.php): 0$" <<<"$1" && grep -q "^wp-config.php 600 $2:www-data$" <<<"$1"' _ "$OUT" "$UA"
  vroot bash -c "$PERM_SCRIPT" _ "$PUBA"
  ok "quyền và chủ từng mục giống hệt lúc trước backup" test "$(head -1 <<<"$OUT")" = "$PERM0"
  vroot bash -c 'stat -c "%a %U:%G %n" "/var/www/$1" "/var/www/$1/public"' _ "$A"
}

part_d() {
  section "D: chặn đầy đĩa"
  vroot bash -c 'df -B1 --output=size,avail,pcent /; ls -la "/var/lib/tpan/backups/$1"; find /var/lib/tpan/backups /var/lib/tpan/restore -name "*.part" 2>/dev/null | wc -l' _ "$A"
  local LS0
  LS0="$(grep -v '^total' <<<"$OUT" | grep -E '\.(tar\.gz|sql)$')"
  backups_json "$A"
  local N0
  N0="$(jq length "$TMPD/bl.json")"
  FILL_ON=1
  run scripts/faults/06-disk-full.sh break
  ok "lấp đĩa tới khoảng 91%" rc_is 0
  vroot tpan backup create "$A" --yes --json
  ok "backup create bị từ chối disk_space_low" bash -c '[ "$2" != 0 ] && [ "$(jq -r .error.code <<<"$1")" = disk_space_low ]' _ "$OUT" "$RC"
  vroot tpan backup create "$A" --yes
  ok "lời từ chối dạng chữ nói luật 1,2 lần và 10%" bash -c 'grep -q "Từ chối:" <<<"$1" && grep -qF "1,2" <<<"$1" && grep -qF "10%" <<<"$1"' _ "$OUT"
  vroot tpan db dump "$A" --yes --json
  ok "xuất DB cũng bị từ chối disk_space_low" bash -c '[ "$2" != 0 ] && [ "$(jq -r .error.code <<<"$1")" = disk_space_low ]' _ "$OUT" "$RC"
  vroot bash -c 'ls -la "/var/lib/tpan/backups/$1"; find /var/lib/tpan/backups /var/lib/tpan/restore -name "*.part" 2>/dev/null | wc -l' _ "$A"
  ok "không file nào được tạo trong thư mục backup, không .part" bash -c '[ "$(grep -E "\.(tar\.gz|sql)$" <<<"$1" | grep -v "^total")" = "$2" ] && [ "$(tail -1 <<<"$1")" = 0 ]' _ "$OUT" "$LS0"
  backups_json "$A"
  ok "bảng backups không thêm dòng (vẫn $N0)" test "$(jq length "$TMPD/bl.json")" = "$N0"
  run scripts/faults/06-disk-full.sh restore
  ok "dọn file lấp đĩa" rc_is 0
  FILL_ON=0
  vroot bash -c 'ls -la /var/tmp/tpan-fault-fill 2>&1; df -h /'
  ok "file giả đã xoá" hasF 'No such file'
}

part_e() {
  section "E: đổi mật khẩu DB của $A (mục 6.9)"
  local OLD NEW
  OLD="$(dbpw "$A")"
  add_secret "$OLD"
  ok "đọc được mật khẩu DB cũ (32 ký tự, đã che)" test "${#OLD}" = 32
  # Vòng lặp curl nền mỗi 0,1 giây trong lúc đổi: đo opcache giữ wp-config.php cũ.
  vroot systemd-run --unit=tpan-e2e-curl bash -c 'while :; do printf "%s %s\n" "$(date +%s.%N)" "$(curl -s -m 5 -o /dev/null -w "%{http_code}" --resolve "$1:80:127.0.0.1" "http://$1/")" >>/root/e2e-curl.log; sleep 0.1; done' _ "$A"
  sleep 2
  runq "$VM" root tpan db passwd "$A" --yes
  local RAWOUT="$TMPD/passwd.out"
  cp "$TMPD/run.out" "$RAWOUT"
  NEW="$(dbpw "$A")"
  add_secret "$NEW"
  show_last
  ok "db passwd thoát 0, job thành công" job_ok
  ok "mật khẩu mới đọc được, 32 ký tự, khác mật khẩu cũ" bash -c '[ "${#1}" = 32 ] && [ "$1" != "$2" ]' _ "$NEW" "$OLD"
  ok "mật khẩu mới không có trong output của lệnh" bash -c '! grep -qF -- "$1" "$2"' _ "$NEW" "$RAWOUT"
  local i code=""
  for i in $(seq 1 15); do
    code="$("$VM" exec curl -sS -m 10 -o /dev/null -w '%{http_code}' --resolve "$A:80:127.0.0.1" "http://$A/" </dev/null | tr -d '\r')"
    [ "$code" = 200 ] && break
    sleep 1
  done
  printf 'site %s sau đổi mật khẩu: %s (lần thử %s)\n' "$A" "$code" "$i"
  ok "$A trả 200 sau đổi mật khẩu" test "$code" = 200
  sleep 3
  vroot systemctl stop tpan-e2e-curl
  vroot bash -c 'echo "số request: $(wc -l </root/e2e-curl.log)"; echo "theo mã:"; awk "{print \$2}" /root/e2e-curl.log | sort | uniq -c; echo "các request khác 200 (giờ, mã):"; awk "\$2 != 200" /root/e2e-curl.log | head -20'
  printf '(mục 16 / M9.md) opcache sau đổi mật khẩu: số request khác 200 trong vòng curl ở trên\n'
  vroot python3 -c "$JOB_PY" db.passwd
  # Đọc mật khẩu ngay trong máy ảo (không qua dòng lệnh) rồi tìm trong tpan.db, WAL, SHM.
  vroot bash -c 'cd / && pw="$(runuser -u "$2" -- wp config get DB_PASSWORD --path="/var/www/$1/public")"; for f in /var/lib/tpan/tpan.db /var/lib/tpan/tpan.db-wal /var/lib/tpan/tpan.db-shm; do if [ -e "$f" ]; then echo "$f: $(grep -caF -- "$pw" "$f") dòng chứa mật khẩu mới"; else echo "$f: không có file"; fi; done; journalctl --no-pager -o cat --since "-10 min" | grep -cF -- "$pw" | sed "s/^/journal: /"; stat -c "%a %U:%G %n" "/var/www/$1/public/wp-config.php"; MYSQL_PWD="$pw" mariadb -u "$2" -N -e "SELECT CURRENT_USER()"' _ "$A" "$UA"
  ok "mật khẩu mới không có trong tpan.db, WAL, SHM, journal" bash -c '! grep -qE ": [1-9][0-9]* dòng chứa" <<<"$1" && grep -q "^journal: 0$" <<<"$1"' _ "$OUT"
  ok "wp-config.php vẫn 600 $UA:www-data" has "^600 $UA:www-data "
  ok "user DB đăng nhập được bằng mật khẩu mới" has "^$UA@localhost\$"

  section "E: tpan db dump $A --yes, sửa DB, tpan db import"
  vroot tpan db dump "$A" --yes
  ok "db dump thoát 0, job thành công" job_ok
  backups_json "$A"
  ok "có 1 bản sql manual" blq '[.[] | select(.kind == "sql" and .reason == "manual")] | length == 1'
  local SID SPATH
  SID="$(blr '[.[] | select(.kind == "sql" and .reason == "manual")][0].id')"
  SPATH="$(blr '[.[] | select(.kind == "sql" and .reason == "manual")][0].path')"
  vroot bash -c 'sha256sum "$1"; stat -c "%a %U:%G %n" "$1"; tail -1 "$1"' _ "$SPATH"
  ok "file .sql 0600 root, sha256 khớp, dòng cuối Dump completed" bash -c 'grep -q "^$2  $3\$" <<<"$1" && grep -q "^600 root:root $3\$" <<<"$1" && grep -q "^-- Dump completed" <<<"$1"' _ "$OUT" "$(blr '[.[] | select(.kind == "sql" and .reason == "manual")][0].sha256')" "$SPATH"
  vroot mariadb -N -B "$UA" -e "UPDATE wp_posts SET post_title = 'e2e-m9 sua sau dump' WHERE ID = 1; INSERT INTO wp_options (option_name, option_value, autoload) VALUES ('e2e_m9_sau_dump', '1', 'no'); SELECT post_title FROM wp_posts WHERE ID = 1"
  ok "DB đã sửa sau khi xuất" out_is 'e2e-m9 sua sau dump'
  vroot tpan db import "$A" --id "$SID" --yes
  ok "db import thoát 0, job thành công" job_ok
  vroot python3 -c "$JOB_PY" db.import
  vroot mariadb -N -B "$UA" -e "SELECT post_title FROM wp_posts WHERE ID = 1; SELECT COUNT(*) FROM wp_options WHERE option_name = 'e2e_m9_sau_dump'"
  ok "DB về như lúc xuất (tiêu đề cũ, dòng thêm sau xuất không còn)" bash -c '[ "$1" = "$(printf "e2e-m9 tieu de luc backup\n0")" ]' _ "$OUT"
  backups_json "$A"
  ok "có bản before-import (sql)" blq '[.[] | select(.kind == "sql" and .reason == "before-import")] | length == 1'
  vexec bash -c "$PAGES_SCRIPT" _ "$A"
  ok "$A trả 200 sau nhập" has "^$A curl=0 http=200 "
}

part_f() {
  section "F: thùng rác: tpan site rm $B --yes"
  local cf="/etc/cron.d/tpan-$SB"
  vroot ls -la "$cf"
  ok "trước khi xoá: file cron của $B có" rc_is 0
  vroot tpan site rm "$B" --yes
  ok "site rm thoát 0, job thành công" job_ok
  vroot ls -la "$cf"
  ok "file cron của $B đã gỡ" hasF 'No such file'
  vroot tpan cron run "$B"
  ok "tpan cron run $B thoát 0, không in gì" bash -c '[ "$2" = 0 ] && [ -z "$1" ]' _ "$OUT" "$RC"
  vroot tpan cron list --json
  ok "cron list: $B tắt, lần chạy gần nhất skipped" jout --arg d "$B" '.sites[] | select(.domain == $d) | (.enabled | not) and .last.status == "skipped"'
  vroot tpan backup create "$B" --yes
  ok "backup create $B (nguồn là thùng rác) thoát 0, job thành công" job_ok
  backups_json "$B"
  ok "có bản full manual của $B" blq '[.[] | select(.kind == "full" and .reason == "manual")] | length == 1'
  vroot bash -c 'tar -tzf "$1" | grep -cE "^public/wp-config.php$|^db.sql$"' _ "$(blr '[.[] | select(.reason == "manual")][0].path')"
  ok "bản backup từ thùng rác có wp-config.php và db.sql" out_is 2

  section "F: tpan trash purge $B --yes"
  vroot tpan trash purge "$B" --yes
  ok "trash purge thoát 0, job thành công" job_ok
  vroot python3 -c "$JOB_PY" site.purge
  ok "job xoá hẳn có lệnh tar (tự backup) trước khi xoá" has '^  command [^ ]+ tar '
  backups_json "$B"
  ok "backup list $B có bản before-purge (dòng còn sau khi site xoá hẳn)" blq '[.[] | select(.kind == "full" and .reason == "before-purge")] | length == 1'
  local PP
  PP="$(blr '[.[] | select(.reason == "before-purge")][0].path')"
  vroot bash -c 'ls -la "$1"; sha256sum "$1"; ls -la "$2" 2>&1; ls -d "/var/www/$3" "/var/lib/tpan/trash/$4" 2>&1; getent passwd "$5" || echo "không có user $5"; mariadb -N -e "SHOW DATABASES" | grep -cx "$5"' _ "$PP" "$cf" "$B" "$SB" "$UB"
  ok "file before-purge còn, sha256 khớp" has "^$(blr '[.[] | select(.reason == "before-purge")][0].sha256')  $PP\$"
  ok "file cron không còn" hasF "cannot access '$cf'"
  ok "site đã xoá hẳn: không thư mục, không user, không DB" bash -c 'grep -q "cannot access .*/var/www/$2" <<<"$1" && grep -q "không có user $3" <<<"$1" && [ "$(tail -1 <<<"$1")" = 0 ]' _ "$OUT" "$B" "$UB"
  vroot tpan site list
  ok "site list không còn $B" hasnt "$B"
  backups_json
  ok "backup list (mọi site) vẫn có các dòng của $B" blq --arg d "$B" '[.[] | select(.domain == $d)] | length >= 2'
}

part_g() {
  section "G: nâng MariaDB đòi backup: site mới $C chưa backup"
  site_add "$C"
  vexec mariadb --version
  vroot tpan mariadb upgrade --to "$MDB_LINE" --dry-run --json
  ok "mariadb upgrade --dry-run bị từ chối backup_required" bash -c '[ "$2" != 0 ] && [ "$(jq -r .error.code <<<"$1")" = backup_required ]' _ "$OUT" "$RC"
  ok "lời từ chối nêu $C, không nêu $A (đã có bản trong 24 giờ)" bash -c 'm="$(jq -r .error.message.vi <<<"$1")"; grep -qF "$2" <<<"$m" && ! grep -qF "$3" <<<"$m"' _ "$OUT" "$C" "$A"
  vroot tpan mariadb upgrade --to "$MDB_LINE" --dry-run

  section "G: tpan backup create --all --yes rồi --dry-run lại"
  vroot tpan backup create --all --yes
  ok "backup create --all thoát 0" rc_is 0
  ok "có kết quả cho $A và $C, có bản sao tpan.db" bash -c 'grep -q "^== $2" <<<"$1" && grep -q "^== $3" <<<"$1" && grep -qE "^Bản sao tpan.db: /var/lib/tpan/backups/_tpan/tpan-[0-9]{8}-[0-9]{6}\.db$" <<<"$1"' _ "$OUT" "$A" "$C"
  vroot tpan mariadb upgrade --to "$MDB_LINE" --dry-run
  ok "mariadb upgrade --dry-run giờ qua (thoát 0)" rc_is 0
  vroot bash -c 'ls /etc/apt/sources.list.d/; mariadb --version'
  ok "chỉ xem trước: không thêm kho MariaDB" hasnt 'tpan-mariadb'
}

part_h() {
  section "H: bản sao tpan.db và health"
  vroot bash -c 'stat -c "%a %U:%G %s %n" /var/lib/tpan/backups/_tpan /var/lib/tpan/backups/_tpan/*'
  ok "có bản sao tpan-*.db 0600 root:root, thư mục 0700" bash -c 'grep -qE "^600 root:root [0-9]+ /var/lib/tpan/backups/_tpan/tpan-[0-9]{8}-[0-9]{6}\.db$" <<<"$1" && grep -qE "^700 root:root [0-9]+ /var/lib/tpan/backups/_tpan$" <<<"$1"' _ "$OUT"
  vroot bash -c 'f="$(ls /var/lib/tpan/backups/_tpan/tpan-*.db | tail -1)"; python3 -c "import sqlite3,sys; c=sqlite3.connect(\"file:\"+sys.argv[1]+\"?mode=ro\", uri=True); print(\"integrity:\", c.execute(\"PRAGMA integrity_check\").fetchone()[0]); print(\"sites:\", c.execute(\"SELECT COUNT(*) FROM sites\").fetchone()[0]); print(\"backups:\", c.execute(\"SELECT COUNT(*) FROM backups\").fetchone()[0])" "$f"'
  ok "bản sao đọc được, integrity ok" has '^integrity: ok$'
  health_json
  ok "tpan.backup:db là Pass" check_status tpan.backup:db pass
  ok "health không có Fail" no_fail

  section "H: lịch backup hằng ngày"
  vroot tpan backup settings
  vroot tpan backup settings --at 03:30 --yes
  ok "backup settings --at 03:30 thoát 0, job thành công" job_ok
  vroot stat -c '%U:%G %a %n' /etc/cron.d/tpan-backup
  ok "/etc/cron.d/tpan-backup root:root 0644" out_is 'root:root 644 /etc/cron.d/tpan-backup'
  vroot cat /etc/cron.d/tpan-backup
  ok "đúng một dòng lệnh: 30 3 * * * root /usr/local/bin/tpan backup create --all --scheduled --yes" bash -c '[ "$(grep -cE "^[^#[:space:]]" <<<"$1")" = 1 ] && grep -qE "^30 0?3 \* \* \* root /usr/local/bin/tpan backup create --all --scheduled --yes >/dev/null 2>&1$" <<<"$1"' _ "$OUT"
  vroot sha256sum /etc/cron.d/tpan-backup
  local SHA1
  SHA1="$(grep -oE '^[0-9a-f]{64}' <<<"$OUT")"
  vroot tpan cron list --json
  ok "cron list: backup at 03:30, backup_file" jout '.backup.at == "03:30" and .backup_file == "/etc/cron.d/tpan-backup"'
  vroot tpan backup settings --at 03:30 --yes
  ok "chạy lại: thoát 0" rc_is 0
  vroot sha256sum /etc/cron.d/tpan-backup
  ok "chạy lại: file không đổi" has "^$SHA1 "
  vroot tpan backup settings --off --yes
  ok "backup settings --off thoát 0, job thành công" job_ok
  vroot ls -la /etc/cron.d/
  ok "/etc/cron.d/tpan-backup đã gỡ" hasnt 'tpan-backup'
  vroot tpan backup settings
  ok "settings nói lịch tắt" hasF 'Lịch backup hằng ngày: tắt'
}

part_i() {
  panel_setup_login
  section "I: API M9"
  backups_json
  local N0
  N0="$(jq length "$TMPD/bl.json")"
  api GET /api/v1/backups
  showb '{settings, n: (.items | length), first: (.items[0] | {id, domain, kind, reason, size, sha256: (.sha256 | length)})}'
  ok "GET /api/v1/backups: 200, settings.keep, items cùng số với backup list ($N0)" bash -c '[ "$1" = 200 ] && jq -e --argjson n "$3" "(.settings.keep | type == \"number\") and (.items | length == \$n) and all(.items[]; (.id | length) > 0 and (.sha256 | length == 64))" "$2" >/dev/null' _ "$CODE" "$TMPD/body" "$N0"
  api GET "/api/v1/backups?domain=$A"
  ok "GET /api/v1/backups?domain=$A: chỉ dòng của $A" bash -c '[ "$1" = 200 ] && jq -e --arg d "$3" "(.items | length) > 0 and all(.items[]; .domain == \$d)" "$2" >/dev/null' _ "$CODE" "$TMPD/body" "$A"
  api GET /api/v1/cron
  showb '{sites: [.sites[] | {domain, enabled, file, schedule, last: (.last | if . then {status, exit_code} else null end)}], backup}'
  ok "GET /api/v1/cron: 200, $A và $C bật, $A có lần chạy ok" bash -c '[ "$1" = 200 ] && jq -e --arg a "$3" --arg c "$4" "([.sites[] | select(.domain == \$a and .enabled and .last.status == \"ok\")] | length == 1) and ([.sites[] | select(.domain == \$c and .enabled)] | length == 1) and (.backup.keep | type == \"number\")" "$2" >/dev/null' _ "$CODE" "$TMPD/body" "$A" "$C"
  api POST "/api/v1/sites/$A/backups?dry_run=1" "${MUT[@]}"
  showb '{kind, target, steps: [.steps[] | {title: .info.title.vi, done}]}'
  ok "POST /sites/$A/backups?dry_run=1: 200, Preview kind backup.create có bước" bash -c '[ "$1" = 200 ] && jq -e ".kind == \"backup.create\" and (.steps | length) >= 1" "$2" >/dev/null' _ "$CODE" "$TMPD/body"
  backups_json
  ok "xem trước qua API không tạo bản backup nào (vẫn $N0)" test "$(jq length "$TMPD/bl.json")" = "$N0"

  section "I: tpan cron run chạy cùng lúc với tpan serve (SQLite busy?)"
  vroot bash -c 'for i in 1 2 3 4 5; do (tpan cron run "$1" >/tmp/e2e-cr-a$i 2>&1; echo "a$i mã $?" >>/tmp/e2e-cr.rc) & (tpan cron run "$2" >/tmp/e2e-cr-c$i 2>&1; echo "c$i mã $?" >>/tmp/e2e-cr.rc) & done; wait; sort /tmp/e2e-cr.rc; cat /tmp/e2e-cr-* ; rm -f /tmp/e2e-cr*; journalctl -u tpan --no-pager -o cat --since "-5 min" | grep -ciE "locked|busy" | sed "s/^/dòng locked|busy trong journal của tpan: /"' _ "$A" "$C"
  ok "10 lần cron run song song (cùng tpan serve) đều thoát 0, không in gì" bash -c '[ "$(grep -c " mã 0$" <<<"$1")" = 10 ] && [ "$(grep -vcE " mã 0$|^dòng locked" <<<"$1")" = 0 ]' _ "$OUT"
  api GET /api/v1/cron
  ok "API vẫn trả 200 sau đó" code_is 200
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
  ok "log không chứa mật khẩu nào" test "$n" -eq 0
  ok "log không chứa IP máy ảo (IPv4)" bash -c '! grep -qF -- "$1" "$2"' _ "$VM_IP" "$E2E_LOG"
  ok "log không chứa mạng Multipass" bash -c '! grep -qF -- "$1" "$2"' _ "$NET4" "$E2E_LOG"
  ok "log không chứa IPv6 của máy ảo" bash -c '[ -z "$1" ] || ! grep -qF -- "$1" "$2"' _ "$V6PFX" "$E2E_LOG"
  ok "log không chứa IP của Mac" bash -c '! grep -qF -- "$1" "$2"' _ "$HOST_IP" "$E2E_LOG"
  ok "log không chứa thư mục nhà hay tên user Mac" bash -c '! grep -qF -- "$1" "$3" && ! grep -qF -- "$2" "$3"' _ "$HOME" "$USER" "$E2E_LOG"
  ok "log không chứa giá trị cookie nào" bash -c '! grep -iE "^set-cookie: [^=]+=" "$1" | grep -vqE "=<đã che>"' _ "$E2E_LOG"
}

UA="$(user_of "$A")"
UB="$(user_of "$B")"
SA="$(slug_of "$A")"
SB="$(slug_of "$B")"
PUBA="/var/www/$A/public"

case "${1:-all}" in
  all)
    part_0
    part_a
    part_h0
    part_b
    part_c
    part_d
    part_e
    part_f
    part_g
    part_h
    part_i
    ;;
  *)
    sed -n '2,30p' "$0" | sed 's/^# \{0,1\}//'
    exit 2
    ;;
esac

leak_check
section "TỔNG KẾT: $PASSES khẳng định đạt, $FAILS khẳng định không đạt"
[ "$FAILS" -eq 0 ]
