#!/usr/bin/env bash
# e2e của mốc M6 (hàng loạt, thùng rác, khôi phục, dọn tự động) trên máy ảo
# của mốc (tên trong file .tpan-vm; scripts/vm.sh tự đọc). Chạy trên Mac, trong
# thư mục repo:
#
#   scripts/e2e-m6.sh all       nginx (đầy đủ) rồi apache (ngắn), mỗi bên từ máy sạch (mặc định)
#   scripts/e2e-m6.sh nginx     chỉ lượt nginx
#   scripts/e2e-m6.sh apache    chỉ lượt apache
#
# Lượt nginx:
#   a  stack nginx
#   b  sites.yaml 10 site; apply --dry-run (không đổi máy); apply --yes --json
#   c  "chỉ reload một lần": sự kiện của job hàng loạt trong tpan.db có đúng
#      một `systemctl reload nginx` và một `systemctl reload php<bản>-fpm`;
#      đối chứng độc lập bằng journalctl của nginx và php-fpm
#   d  10 site trả 200 đúng nội dung của mình; cô lập s1 / s2
#   e  chạy lại apply: không có gì thay đổi, không job mới; tpan export
#   f  e2e bước 7 của mục 12: dấu dữ liệu trong s2, site rm (444, user và user
#      DB bị khoá, thư mục ở thùng rác), site restore (200, dữ liệu còn
#      nguyên, mở khoá); health không có sự cố hay lệch giả
#   g  xoá hẳn: rm s3, trash purge s3; purge site đang chạy bị từ chối
#   h  hết hạn: trash settings --days 0, rm s4, trash purge --expired
#   i  prune: sites.yaml bớt s9, s10, apply --prune: một job, reload một lần
#   k  tpan setup, API: POST /apply?dry_run=1, DELETE /sites/{d}, POST /restore
# Lượt apache: stack apache, apply 3 site (reload apache2 một lần), rm (403)
# rồi restore (200).
#
# Đặt E2E_LOG=<file log> để cuối lần chạy script tự tìm trong chính file log
# đó: mọi mật khẩu đã gặp, IP máy ảo (IPv4 và IPv6), IP của Mac, thư mục nhà và
# tên user Mac, giá trị cookie (phải là 0 lần).
#
# Output như e2e-m5.sh: mỗi lệnh có dấu thời gian, output nguyên văn (đã che)
# và mã thoát; mỗi khẳng định in "=> ĐẠT" hoặc "=> KHÔNG ĐẠT"; cuối cùng in số
# khẳng định đạt và không đạt. Che: mật khẩu (quản trị WordPress, DB của site,
# panel), giá trị cookie, IP máy ảo, IP của Mac, mạng Multipass, thư mục nhà và
# tên user Mac.
# Không dùng set -e: nhiều lệnh ở đây được chờ là phải lỗi.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2

VM=scripts/vm.sh
FAILS=0
PASSES=0
OUT=""
RC=0
EMAIL=admin@example.com
YAML=/root/e2e-sites.yaml
TMPD="$(mktemp -d)"
chmod 700 "$TMPD"
SECRETS="$TMPD/secrets"
: >"$SECRETS"
chmod 600 "$SECRETS"
HJ="$TMPD/health.json"
trap 'rm -rf "$TMPD"' EXIT

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

run() {
  local shown
  shown="$(mask_str <<<"$*")"
  printf '\n[%s] $ %s\n' "$(ts)" "$shown"
  # Output vào file rồi mới đọc, không qua pipe: `multipass exec` (bản trên
  # Mac) thỉnh thoảng treo, quay 100% CPU, khi stdout là pipe và lệnh in vài
  # chục KB trở lên (thấy ở 60 KB, 100 KB; xem lần chạy thử của M6). Ghi vào
  # file thường thì không gặp.
  "$@" >"$TMPD/run.out" 2>&1
  RC=$?
  OUT="$(cat "$TMPD/run.out")"
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

rc_is() { [ "$RC" -eq "$1" ]; }
rc_not() { [ "$RC" -ne "$1" ]; }
has() { grep -qE -- "$1" <<<"$OUT"; }
hasnt() { ! grep -qE -- "$1" <<<"$OUT"; }
hasF() { grep -qF -- "$1" <<<"$OUT"; }
out_is() { [ "$(tr -d '\r' <<<"$OUT")" = "$1" ]; }
# count_is <số> <regex>: số dòng của OUT khớp regex (đã bỏ \r).
count_is() { [ "$(tr -d '\r' <<<"$OUT" | grep -cE -- "$2")" -eq "$1" ]; }

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
  ok "build và chép tpan vào máy ảo" rc_is 0
  run shasum -a 256 bin/tpan-linux-arm64 bin/tpan-linux-amd64
  run git log --oneline -1
  run git status --short
}

# ---------------------------------------------------------------- đoạn chạy trong máy ảo

# Job mới nhất của một loại (kind): thời gian, các lệnh reload/restart/kiểm
# config, kết quả từng unit, lỗi và hoàn tác. In thêm số job trong tpan.db.
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
n = c.execute("SELECT COUNT(*) FROM job_events WHERE job_id = ? AND kind = \"command\"", (j[0],)).fetchone()[0]
print("số lệnh (sự kiện command) của job:", n)
for kind, step, msg in c.execute("SELECT kind, step_id, message FROM job_events WHERE job_id = ? AND kind IN (\"command\", \"unit.done\", \"step.fail\", \"undo.done\", \"undo.fail\") ORDER BY seq", (j[0],)):
    if kind == "command" and not any(w in msg for w in ("systemctl", "nginx -t", "apachectl", "php-fpm", "certbot", "usermod", "userdel")) and not (step or "").startswith(("db.", "user.", "dir.")):
        continue
    print(" ", kind, step or "-", msg[:300].replace("\n", " "))
'

# Các site còn gì trên máy: user, nhóm, DB, user DB, thư mục, thùng rác, log,
# pool, link và file vhost, dòng trong bảng sites. "CÒN" là còn.
LEFT_SCRIPT='
d=$1; u=$2; slug=$3; php=$4; web=$5
getent passwd "$u" >/dev/null && echo "CÒN user Linux $u" || echo "không có user Linux $u (getent mã $?)"
getent group "$u" >/dev/null && echo "CÒN nhóm $u" || echo "không có nhóm $u"
mariadb -N -B -e "SHOW DATABASES" | grep -qx "$u" && echo "CÒN DB $u" || echo "không có DB $u"
mariadb -N -B -e "SELECT User FROM mysql.user" | grep -qx "$u" && echo "CÒN user DB $u" || echo "không có user DB $u"
for p in "/var/www/$d" "/var/lib/tpan/trash/$slug" "/var/log/tpan/sites/$d" "/etc/php/$php/fpm/pool.d/tpan-$slug.conf" "/run/php/tpan-$slug.sock" "/etc/$web/sites-enabled/tpan-$d.conf" "/etc/$web/sites-available/tpan-$d.conf"; do
  if [ -e "$p" ] || [ -L "$p" ]; then echo "CÒN $p"; else echo "không có $p"; fi
done
python3 -c "import sqlite3,sys; c = sqlite3.connect(\"file:/var/lib/tpan/tpan.db?mode=ro\", uri=True); r = c.execute(\"SELECT status FROM sites WHERE domain = ?\", (sys.argv[1],)).fetchone(); print((\"CÒN dòng \" + sys.argv[1] + \" trong bảng sites (\" + r[0] + \")\") if r else (\"không có dòng \" + sys.argv[1] + \" trong bảng sites\"))" "$d"
'

# Trạng thái khoá của user Linux và user DB của một site (độc lập với tpan).
LOCK_SCRIPT='
u=$1
chage -l "$u" | grep -i "^Account expires"
mariadb -N -B -e "SELECT User, JSON_VALUE(Priv, \"\$.account_locked\") FROM mysql.global_priv WHERE User = \"$u\""
'

# Dấu vân tay của cây thư mục một site: sha256 của danh sách (đường dẫn, sha256
# từng file), không tính thời gian hay quyền.
TREE_SCRIPT='
cd "$1" && find . -type f -print0 | sort -z | xargs -0 sha256sum | sha256sum | cut -c1-64
find . -type f | wc -l
'

# mã HTTP, curl mã, tiêu đề, số lần nhắc http://<domain> của trang chủ từng site.
PAGES_SCRIPT='
for d in "$@"; do
  body=$(curl -sS -m 20 --resolve "$d:80:127.0.0.1" -w "\n%{http_code}" "http://$d/" 2>/dev/null); rc=$?
  code=$(printf "%s" "$body" | tail -1)
  title=$(printf "%s" "$body" | grep -o "<title>[^<]*</title>" | head -1)
  n=$(printf "%s" "$body" | grep -cF "http://$d/")
  echo "$d curl=$rc http=$code nhắc=$n $title"
done
'

# ---------------------------------------------------------------- health

health_json() {
  printf '\n[%s] $ %s root tpan health --json\n' "$(ts)" "$VM"
  # stdout vào file, không qua pipe: `multipass exec` có stdout là pipe và
  # stderr là file thì treo (quay CPU) ở đúng 100 KiB; báo cáo health của 9-10
  # site khoảng 180 KB (thấy ở lần chạy thử của M6).
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
    "số phép drift: \([.groups[].results[] | select(.id | startswith("drift:"))] | length)",
    ([.groups[].results[] | select(.status != "pass")] | if length == 0 then "(mọi phép kiểm đạt)" else (.[] |
      "--- \(.status) \(.id) (chặng \(.stage)): \(.summary.vi)") end),
    "sự cố vừa mở: \([.changes.opened[]? | "#\(.id) \(.check_id)"] | join(", "))",
    "sự cố vừa đóng: \([.changes.closed[]? | "#\(.id) \(.check_id) (\(.close_reason))"] | join(", "))"' "$HJ")"
  mask
  printf '%s\n[%s] (mã thoát %s)\n' "$OUT" "$(ts)" "$RC"
}
hq() { jq -e "$@" "$HJ" >/dev/null 2>&1; }
no_fail() { hq '[.groups[].results[] | select(.status == "fail")] | length == 0'; }
no_drift_bad() { hq '[.groups[].results[] | select((.id | startswith("drift:")) and .status != "pass")] | length == 0'; }
no_opened() { hq '[.changes.opened[]?] | length == 0'; }
has_target() { hq --arg t "$1" 'any(.groups[]; .target == $t)'; }
target_all_pass() { hq --arg t "$1" '[.groups[] | select(.target == $t) | .results[] | select(.status == "fail" or ((.id | startswith("drift:")) and .status != "pass"))] | length == 0'; }

health_clean() {
  local when=$1
  health_json
  ok "health $when: thoát 0, không Fail" bash -c '[ "$1" = 0 ]' _ "$RC"
  ok "health $when: không có lệch (drift) nào khác đạt" no_drift_bad
  ok "health $when: không mở sự cố nào" no_opened
}

# ---------------------------------------------------------------- apply

# write_yaml <file trên Mac> <domain...>: sites.yaml với defaults admin_email.
write_yaml() {
  local f=$1 d
  shift
  {
    printf '# sites.yaml của e2e M6\ndefaults:\n  admin_email: %s\nsites:\n' "$EMAIL"
    for d in "$@"; do printf '  - domain: %s\n' "$d"; done
  } >"$f"
}

# push_file <file trên Mac> <đích trong máy ảo>: nội dung đi bằng một đối số
# (multipass exec không đóng stdin của lệnh khi stdin bên Mac hết, `tee` treo).
push_file() {
  printf '\n[%s] $ %s root bash -c '\''printf "%%s" "$1" >"$2"'\'' _ "<nội dung %s>" %s\n' "$(ts)" "$VM" "$(mask_str <<<"$1")" "$2"
  "$VM" root bash -c 'printf "%s" "$1" >"$2"' _ "$(cat "$1")" "$2" </dev/null
  RC=$?
  printf '[%s] (mã thoát %s)\n' "$(ts)" "$RC"
}

# push_yaml <file trên Mac>: chép vào $YAML trong máy ảo rồi in lại.
push_yaml() {
  push_file "$1" "$YAML"
  vroot cat "$YAML"
}

# read_dbpw <domain...>: đọc mật khẩu DB của từng site vào danh sách che (không in).
read_dbpw() {
  local d pw n=0
  for d in "$@"; do
    pw="$("$VM" root bash -c 'cd / && runuser -u "$2" -- wp config get DB_PASSWORD --path="/var/www/$1/public" 2>/dev/null' _ "$d" "$(user_of "$d")" </dev/null | tr -d '\r')"
    [ -n "$pw" ] && n=$((n + 1))
    add_secret "$pw"
  done
  printf 'Đã đọc mật khẩu DB của %s/%s site vào danh sách che (không in).\n' "$n" "$#"
}

# apply_json <cờ...>: tpan apply ... --json. Nhặt mật khẩu quản trị khỏi output
# (vào danh sách che) TRƯỚC khi in; in bản tóm tắt đã che. APPLY_JSON giữ
# output thô (chỉ trong $TMPD), APPLY_PW_OK=1 khi mỗi mật khẩu xuất hiện đúng
# một lần trong output thô.
APPLY_PW_OK=0
apply_json() {
  local cmd=("$VM" root tpan apply "$@" --json) pw n
  printf '\n[%s] $ %s\n' "$(ts)" "${cmd[*]}"
  "${cmd[@]}" </dev/null >"$TMPD/apply.raw" 2>"$TMPD/apply.err"
  RC=$?
  tr -d '\r' <"$TMPD/apply.raw" >"$TMPD/apply.json"
  APPLY_PW_OK=1
  n=0
  while IFS= read -r pw; do
    [ -n "$pw" ] || continue
    n=$((n + 1))
    add_secret "$pw"
    [ "$(grep -oF -- "$pw" "$TMPD/apply.json" | wc -l | tr -d ' ')" = 1 ] || APPLY_PW_OK=0
  done < <(jq -r '.created[]?.admin_password // empty' "$TMPD/apply.json" 2>/dev/null)
  OUT="$(cat "$TMPD/apply.err")"
  mask
  [ -n "$OUT" ] && printf 'stderr:\n%s\n' "$OUT"
  OUT="$(jq '{items: [.items[] | "\(.domain) \(.action)\(if .code then " (\(.code))" else "" end)"],
    job: (.report.job // null | if . then {id, kind, target, status, started, finished, error} else null end),
    units: [.report.units[]? | "\(.target) \(.status)\(if .phase then " \(.phase)" else "" end)\(if .step_id then " \(.step_id)" else "" end)\(if .error then ": \(.error)" else "" end)"],
    created: [.created[] | "\(.domain) \(.status) wp_installed=\(.wp_installed) admin_user=\(.admin_user // "") mật khẩu: \(.admin_password // "" | length) ký tự"],
    isolation: "\([.isolation[]? | select(.status == "pass")] | length)/\([.isolation[]?] | length) phép kiểm cô lập đạt",
    ssl: [.ssl[]? | .job.status]}' "$TMPD/apply.json" 2>&1)"
  mask
  printf '%s\n' "$OUT"
  printf 'Mật khẩu quản trị trong output --json: %s; mỗi cái đúng một lần: %s\n[%s] (mã thoát %s)\n' "$n" "$APPLY_PW_OK" "$(ts)" "$RC"
}
aq() { jq -e "$@" "$TMPD/apply.json" >/dev/null 2>&1; }

# apply_text <cờ...>: tpan apply ... dạng chữ, nhặt mọi dòng "Mật khẩu:" vào danh sách che.
apply_text() {
  local cmd=("$VM" root tpan apply "$@") pw
  printf '\n[%s] $ %s\n' "$(ts)" "${cmd[*]}"
  "${cmd[@]}" </dev/null >"$TMPD/apply.txt" 2>&1
  RC=$?
  OUT="$(cat "$TMPD/apply.txt")"
  APPLY_PW_OK=1
  APPLY_PW_N=0
  while IFS= read -r pw; do
    [ -n "$pw" ] || continue
    APPLY_PW_N=$((APPLY_PW_N + 1))
    add_secret "$pw"
    [ "$(grep -oF -- "$pw" <<<"$OUT" | wc -l | tr -d ' ')" = 1 ] || APPLY_PW_OK=0
  done < <(sed -nE 's/^  Mật khẩu: +([A-Za-z0-9]{32})\r?$/\1/p' <<<"$OUT")
  mask
  printf '%s\n[%s] (mã thoát %s)\n' "$OUT" "$(ts)" "$RC"
  printf 'Mật khẩu quản trị trong output: %s; mỗi cái đúng một lần: %s\n' "$APPLY_PW_N" "$APPLY_PW_OK"
}

# reload_count <job kind> <web unit> <php unit> <since> <until>: đếm lệnh reload
# trong sự kiện của job mới nhất (tpan.db) và dòng "Reloaded" trong journal
# của hai dịch vụ trong khoảng thời gian của job.
reload_count() {
  local kind=$1 web=$2 php=$3 since=$4 until=$5
  vroot python3 -c "$JOB_PY" "$kind"
  JOBOUT="$OUT"
  ok "job $kind thành công" has "^job $kind [0-9a-f]+ succeeded"
  ok "sự kiện của job: đúng 1 lệnh \`systemctl reload $web\`" count_is 1 "^  command [^ ]+ systemctl reload $web\$"
  ok "sự kiện của job: đúng 1 lệnh \`systemctl reload $php\`" count_is 1 "^  command [^ ]+ systemctl reload $php\$"
  ok "sự kiện của job: không có lệnh reload hay restart nào khác" count_is 2 '^  command [^ ]+ systemctl (reload|restart|reload-or-restart|try-reload-or-restart) '
  # journald ghi chậm một nhịp: chờ rồi mới đọc.
  sleep 2
  vroot bash -c 'for u in "$1" "$2"; do echo "== journalctl -u $u --since \"$3\" --until \"$4\""; journalctl -u "$u" --since "$3" --until "$4" --no-pager -o short-iso | grep -vE "^-- " ; done' _ "$web" "$php" "$since" "$until"
  local jr="$OUT"
  ok "journal của $web trong khoảng job: đúng 1 dòng Reloaded" bash -c '[ "$(awk -v u="$2" "/^== /{cur=\$4} cur==u" <<<"$1" | grep -c "Reloaded ")" = 1 ]' _ "$jr" "$web"
  ok "journal của $php trong khoảng job: đúng 1 dòng Reloaded" bash -c '[ "$(awk -v u="$2" "/^== /{cur=\$4} cur==u" <<<"$1" | grep -c "Reloaded ")" = 1 ]' _ "$jr" "$php"
  ok "journal: không dịch vụ nào bị dừng hay khởi động lại trong khoảng job" bash -c '! grep -qE "(Stopping|Stopped|Starting|Started) " <<<"$1"' _ "$jr"
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
jb() { jq -r "$1" "$TMPD/body" 2>/dev/null; }
MUT=(-H 'Accept: application/json' -H 'Sec-Fetch-Site: same-origin')

# wait_job <id>: chờ job xong (GET /jobs/{id} mỗi giây, tối đa 300 giây), rồi in tóm tắt.
JOBST=""
wait_job() {
  local id=$1 i
  JOBST=""
  for i in $(seq 1 300); do
    JOBST="$(curl -sS -m 10 --cacert "$TMPD/cert.pem" -b "$JAR" "$BASE/api/v1/jobs/$id" 2>/dev/null | jq -r '.job.status // empty' 2>/dev/null)"
    [ -n "$JOBST" ] && [ "$JOBST" != running ] && break
    sleep 1
  done
  printf '(chờ job %s: %s giây)\n' "$id" "$i"
  api GET "/api/v1/jobs/$id"
  show '{job: (.job | {kind, target, status, error}), commands: [.events[] | select(.kind == "command") | .message | select(test("systemctl|usermod|ACCOUNT"))]}'
}

panel_setup_login() {
  section "nginx k: tpan setup (panel nghe 8443) rồi đăng nhập từ Mac"
  local cmd=("$VM" root tpan setup --yes) pw
  printf '\n[%s] $ %s\n' "$(ts)" "${cmd[*]}"
  "${cmd[@]}" </dev/null >"$TMPD/setup.txt" 2>&1
  RC=$?
  OUT="$(cat "$TMPD/setup.txt")"
  pw="$(grep -A1 '^Mật khẩu quản trị (chỉ hiện MỘT lần' <<<"$OUT" | sed -nE 's/^    ([A-Z2-7]{26})\r?$/\1/p' | head -1)"
  add_secret "$pw"
  (umask 077 && printf '%s' "$pw" | jq -Rs '{password: .}' >"$TMPD/login.json")
  mask
  printf '%s\n[%s] (mã thoát %s)\n' "$OUT" "$(ts)" "$RC"
  ok "setup thoát 0" rc_is 0
  ok "đọc được mật khẩu panel" test -n "$pw"
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

# ================================================================ lượt nginx

run_nginx() {
  local web=nginx d i
  local ALL=() KEEP=()
  for i in 1 2 3 4 5 6 7 8 9 10; do ALL+=("s$i.example.com"); done
  local S1=s1.example.com S2=s2.example.com S3=s3.example.com S4=s4.example.com S5=s5.example.com
  local u1 u2
  u1="$(user_of "$S1")"
  u2="$(user_of "$S2")"

  section "nginx 0: máy sạch, triển khai tpan"
  reset_vm
  deploy

  section "nginx a: tpan stack install --web nginx"
  vroot tpan stack install --web nginx --yes
  ok "stack install thoát 0" rc_is 0
  ok "job thành công" has '^Job .*: thành công'
  vexec php -r 'echo PHP_MAJOR_VERSION.".".PHP_MINOR_VERSION, "\n";'
  local PHPV PHPU
  PHPV="$(tr -d '\r\n' <<<"$OUT")"
  PHPU="php$PHPV-fpm"
  ok "đọc được bản PHP của máy ($PHPV)" bash -c '[[ "$1" =~ ^[0-9]+\.[0-9]+$ ]]' _ "$PHPV"
  vexec systemctl is-active nginx "$PHPU" mariadb
  ok "nginx, $PHPU, mariadb active" count_is 3 '^active$'
  vroot df -h /var/www /var/lib/tpan
  ok "/var/www và /var/lib/tpan cùng một phân vùng (rename vào thùng rác là tức thì)" bash -c '[ "$(tail -n +2 <<<"$1" | awk "{print \$1}" | sort -u | wc -l | tr -d " ")" = 1 ]' _ "$(tr -d '\r' <<<"$OUT")"

  section "nginx b: sites.yaml 10 site, xem trước (--dry-run)"
  write_yaml "$TMPD/sites.yaml" "${ALL[@]}"
  push_yaml "$TMPD/sites.yaml"
  ok "sites.yaml trong máy ảo có 10 domain" count_is 10 '^  - domain: s[0-9]+\.example\.com$'
  vroot python3 -c "$JOB_PY"
  local jobs0
  jobs0="$(sed -nE 's/^số job trong tpan.db: ([0-9]+).*/\1/p' <<<"$OUT")"
  vroot tpan apply -f "$YAML" --dry-run
  ok "dry-run thoát 0" rc_is 0
  ok "bảng kế hoạch: 10 dòng \"tạo\"" count_is 10 '^s[0-9]+\.example\.com +tạo +$'
  ok "bản xem trước có bước nạp lại nginx và $PHPU một lần cho cả lô" bash -c 'grep -qF "nạp lại nginx một lần cho cả lô" <<<"$1" && grep -qF "nạp lại $2 một lần cho cả lô" <<<"$1"' _ "$OUT" "$PHPU"
  vroot bash -c 'echo "user tp_: $(getent passwd | grep -c "^tp_")"; echo "DB tp_: $(mariadb -N -B -e "SHOW DATABASES" | grep -c "^tp_")"; ls /var/www; ls /etc/nginx/sites-enabled'
  ok "dry-run không đổi máy: không user tp_, không DB tp_, không thư mục site, không vhost" bash -c 'grep -q "^user tp_: 0" <<<"$1" && grep -q "^DB tp_: 0" <<<"$1" && ! grep -q "example.com" <<<"$1"' _ "$(tr -d '\r' <<<"$OUT")"
  vroot python3 -c "$JOB_PY"
  ok "dry-run không tạo job (vẫn $jobs0 job)" hasF "số job trong tpan.db: $jobs0"

  section "nginx b: tpan apply -f sites.yaml --yes --json (10 site một job)"
  local since until t0 t1
  since="$(vm_now)"
  sleep 1
  t0="$(date +%s)"
  apply_json -f "$YAML" --yes
  t1="$(date +%s)"
  sleep 1 # --until của journalctl tính tới giây: lấy mốc sau khi job xong hẳn
  until="$(vm_now)"
  printf 'THỜI GIAN apply 10 site (đo từ Mac, gồm multipass exec): %s giây\n' "$((t1 - t0))"
  ok "apply thoát 0" rc_is 0
  ok "một job kind apply, target \"10 site\", succeeded" aq '.report.job.kind == "apply" and .report.job.target == "10 site" and .report.job.status == "succeeded"'
  ok "10 unit succeeded" aq '[.report.units[] | select(.status == "succeeded")] | length == 10'
  ok "10 site created, WordPress đã cài" aq '[.created[] | select(.status == "succeeded" and .wp_installed)] | length == 10'
  ok "10 mật khẩu quản trị, mỗi cái đúng một lần trong output" bash -c '[ "$1" = 1 ] && [ "$(jq "[.created[].admin_password | select(length == 32)] | length" "$2")" = 10 ]' _ "$APPLY_PW_OK" "$TMPD/apply.json"
  ok "cô lập giữa các site mới: mọi phép kiểm đạt" aq '([.isolation[]] | length) > 0 and ([.isolation[] | select(.status != "pass")] | length) == 0'
  read_dbpw "${ALL[@]}"

  section "nginx c: chỉ reload một lần (tpan.db và journal)"
  reload_count apply nginx "$PHPU" "$since" "$until"
  vroot python3 -c 'import sqlite3; c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True); [print(*r) for r in c.execute("SELECT domain, status, php_version FROM sites ORDER BY domain")]'
  ok "bảng sites: 10 site active" count_is 10 '^s[0-9]+\.example\.com active '
  ok "bản PHP của site trong tpan.db là $PHPV" bash -c '[ "$(tr -d "\r" <<<"$1" | awk "{print \$3}" | sort -u)" = "$2" ]' _ "$OUT" "$PHPV"

  section "nginx d: 10 site trả 200 đúng nội dung của mình; cô lập"
  vexec bash -c "$PAGES_SCRIPT" _ "${ALL[@]}"
  ok "10 site: HTTP 200, tiêu đề là chính domain, trang nhắc http://<domain>/" bash -c 'n=0; for i in 1 2 3 4 5 6 7 8 9 10; do d="s$i.example.com"; grep -qE "^$d curl=0 http=200 nhắc=[1-9][0-9]* <title>$d</title>" <<<"$1" && n=$((n+1)); done; [ "$n" = 10 ]' _ "$(tr -d '\r' <<<"$OUT")"
  vroot runuser -u "$u1" -- cat "/var/www/$S2/public/wp-config.php"
  ok "user của $S1 không đọc được wp-config.php của $S2 (Permission denied)" bash -c '[ "$2" != 0 ] && grep -q "Permission denied" <<<"$1"' _ "$OUT" "$RC"
  vroot runuser -u "$u2" -- cat "/var/www/$S1/public/wp-config.php"
  ok "chiều ngược lại cũng bị chặn" bash -c '[ "$2" != 0 ] && grep -q "Permission denied" <<<"$1"' _ "$OUT" "$RC"

  section "nginx e: chạy lại apply: không có gì thay đổi; tpan export"
  vroot python3 -c "$JOB_PY"
  local jobs1
  jobs1="$(sed -nE 's/^số job trong tpan.db: ([0-9]+).*/\1/p' <<<"$OUT")"
  vroot tpan apply -f "$YAML" --yes
  ok "chạy lại thoát 0" rc_is 0
  ok "in \"Không có gì thay đổi.\"" hasF 'Không có gì thay đổi.'
  ok "10 dòng \"bỏ qua (đã có)\"" count_is 10 '^s[0-9]+\.example\.com +bỏ qua \(đã có\)'
  vroot python3 -c "$JOB_PY"
  ok "không job mới (vẫn $jobs1 job)" hasF "số job trong tpan.db: $jobs1"
  vroot tpan export
  ok "export thoát 0" rc_is 0
  ok "export có đủ 10 domain" bash -c 'for i in 1 2 3 4 5 6 7 8 9 10; do grep -qE "domain: s$i\.example\.com\$" <<<"$1" || exit 1; done' _ "$(tr -d '\r' <<<"$OUT")"
  "$VM" root tpan export </dev/null >"$TMPD/export.raw" 2>&1
  tr -d '\r' <"$TMPD/export.raw" >"$TMPD/export.yaml"
  push_file "$TMPD/export.yaml" /root/e2e-export.yaml
  vroot tpan apply -f /root/e2e-export.yaml --dry-run
  ok "đưa lại file export vào apply --dry-run: 10 dòng bỏ qua, không có gì thay đổi" bash -c '[ "$2" = 0 ] && [ "$(grep -cE "^s[0-9]+\.example\.com +bỏ qua" <<<"$1")" = 10 ] && grep -qF "Không có gì thay đổi." <<<"$1"' _ "$OUT" "$RC"

  section "nginx f: e2e bước 7: dấu dữ liệu trong $S2"
  vroot bash -c 'cd / && runuser -u "$2" -- wp post create --post_title="e2e-m6 dau du lieu s2" --post_status=publish --porcelain --path="/var/www/$1/public" 2>&1' _ "$S2" "$u2"
  local POST
  POST="$(tr -d '\r\n' <<<"$OUT")"
  ok "wp post create (user của site) trả id bài viết" bash -c '[[ "$1" =~ ^[0-9]+$ ]]' _ "$POST"
  local MARK
  MARK="e2e-m6-dau-file-$(date +%s)"
  vroot bash -c 'printf "%s\n" "$3" | runuser -u "$2" -- tee "/var/www/$1/public/e2e-dau.txt" && stat -c "%a %U:%G %n" "/var/www/$1/public/e2e-dau.txt"' _ "$S2" "$u2" "$MARK"
  ok "ghi file dấu dưới user của site" rc_is 0
  vexec curl -sS -m 20 --resolve "$S2:80:127.0.0.1" "http://$S2/e2e-dau.txt"
  ok "http://$S2/e2e-dau.txt trả đúng dấu" out_is "$MARK"
  vexec curl -sSL -m 20 --resolve "$S2:80:127.0.0.1" "http://$S2/?p=$POST"
  ok "bài viết dấu hiện trên site" hasF 'e2e-m6 dau du lieu s2'
  vroot bash -c "$TREE_SCRIPT" _ "/var/www/$S2"
  local TREE0="$OUT"
  ok "đọc được dấu vân tay cây thư mục $S2" has '^[0-9a-f]{64}$'
  vroot bash -c "$LOCK_SCRIPT" _ "$u2"
  ok "trước khi xoá: user không hết hạn (never), user DB chưa từng khoá (account_locked chưa có: NULL, hoặc 0)" bash -c 'grep -q ": never" <<<"$1" && grep -qE "^$2\s+(0|NULL)$" <<<"$1"' _ "$(tr -d '\r' <<<"$OUT")" "$u2"

  section "nginx f: tpan site rm $S2 --yes (đưa vào thùng rác)"
  since="$(vm_now)"
  sleep 1
  t0="$(date +%s)"
  vroot tpan site rm "$S2" --yes
  t1="$(date +%s)"
  printf 'THỜI GIAN site rm (đo từ Mac): %s giây\n' "$((t1 - t0))"
  ok "site rm thoát 0, job thành công" bash -c '[ "$2" = 0 ] && grep -q "^Job .*: thành công" <<<"$1"' _ "$OUT" "$RC"
  ok "báo đã vào thùng rác, cách khôi phục" hasF "sudo tpan site restore $S2"
  vroot python3 -c "$JOB_PY" site.rm
  ok "job site.rm có bước khoá user DB (db.lock) và khoá user Linux (usermod --expiredate 1)" bash -c 'grep -qE "^  command db\.lock:$2 " <<<"$1" && grep -q "user.lock:$2 usermod --expiredate 1" <<<"$1"' _ "$OUT" "$S2"
  vexec curl -sS -m 10 -o /dev/null -w '%{http_code}\n' --resolve "$S2:80:127.0.0.1" "http://$S2/"
  ok "$S2 rơi vào site mặc định (nginx 444, curl mã 52)" rc_is 52
  vexec curl -sS -m 10 -o /dev/null -w '%{http_code}\n' --resolve "$S2:80:127.0.0.1" "http://$S2/e2e-dau.txt"
  ok "file dấu cũng không còn phục vụ (curl mã 52)" rc_is 52
  vroot bash -c "$LOCK_SCRIPT" _ "$u2"
  ok "user Linux bị khoá (Account expires khác never)" bash -c 'grep -qi "^Account expires" <<<"$1" && ! grep -q ": never" <<<"$1"' _ "$(tr -d '\r' <<<"$OUT")"
  ok "user DB bị khoá (mysql.global_priv account_locked = 1)" bash -c 'grep -qE "^$2\s+1$" <<<"$1"' _ "$(tr -d '\r' <<<"$OUT")" "$u2"
  vroot bash -c 'ls -ld /var/lib/tpan/trash "/var/lib/tpan/trash/$2" "/var/www/$1" 2>&1; stat -c "%a %U:%G %n" /var/lib/tpan/trash "/var/lib/tpan/trash/$2" "/var/lib/tpan/trash/$2/public/e2e-dau.txt" 2>&1; ls /etc/nginx/sites-enabled /etc/nginx/sites-available; echo "== pool.d"; ls /etc/php/*/fpm/pool.d' _ "$S2" "$(slug_of "$S2")"
  ok "thư mục ở /var/lib/tpan/trash/<slug>, /var/www/$S2 không còn" bash -c 'grep -q "/var/lib/tpan/trash/$2\$" <<<"$1" && grep -q "cannot access .*/var/www/$3.: No such file" <<<"$1"' _ "$(tr -d '\r' <<<"$OUT")" "$(slug_of "$S2")" "$S2"
  ok "link vhost gỡ, file vhost giữ trong sites-available, pool gỡ" bash -c '[ "$(grep -c "^tpan-$2.conf$" <<<"$1")" = 1 ] && ! grep -q "tpan-$3.conf" <<<"$(sed -n "/^== pool.d/,\$p" <<<"$1")"' _ "$(tr -d '\r' <<<"$OUT")" "$S2" "$(slug_of "$S2")"
  vroot bash -c "$TREE_SCRIPT" _ "/var/lib/tpan/trash/$(slug_of "$S2")"
  ok "cây thư mục trong thùng rác giống hệt trước khi xoá" test "$OUT" = "$TREE0"
  vroot runuser -u "$u2" -- id -un
  ok "(mục 16) root vẫn runuser được dưới user đã hết hạn" bash -c '[ "$2" = 0 ] && [ "$(tr -d "\r" <<<"$1")" = "$3" ]' _ "$OUT" "$RC" "$u2"
  vroot tpan trash list
  ok "trash list có $S2 và hạn xoá hẳn" has "^$S2 +[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9:]{5} +[0-9]{4}-"
  vroot tpan site list
  vexec bash -c "$PAGES_SCRIPT" _ "$S1" "$S3"
  ok "$S1 và $S3 vẫn 200" bash -c '[ "$(grep -c "http=200" <<<"$1")" = 2 ]' _ "$OUT"
  health_clean "sau site rm $S2"
  ok "health không kiểm dịch vụ của $S2 (đang ở thùng rác): chỉ còn phép drift của file vhost được giữ, và đạt" bash -c 'jq -e --arg t "$1" "[.groups[] | select(.target == \$t) | .results[]] as \$r | (\$r | length) >= 1 and all(\$r[]; (.id | startswith(\"drift:\")) and .status == \"pass\")" "$2" >/dev/null' _ "$S2" "$HJ"

  section "nginx f: tpan site restore $S2 --yes"
  t0="$(date +%s)"
  vroot tpan site restore "$S2" --yes
  t1="$(date +%s)"
  printf 'THỜI GIAN site restore (đo từ Mac): %s giây\n' "$((t1 - t0))"
  ok "restore thoát 0, job thành công" bash -c '[ "$2" = 0 ] && grep -q "^Job .*: thành công" <<<"$1"' _ "$OUT" "$RC"
  vroot bash -c "$TREE_SCRIPT" _ "/var/www/$S2"
  ok "cây thư mục sau khôi phục giống hệt trước khi xoá" test "$OUT" = "$TREE0"
  vexec bash -c "$PAGES_SCRIPT" _ "$S2"
  ok "$S2 trả 200, đúng trang của mình" has "^$S2 curl=0 http=200 nhắc=[1-9][0-9]* <title>$S2</title>"
  vexec curl -sS -m 20 --resolve "$S2:80:127.0.0.1" "http://$S2/e2e-dau.txt"
  ok "file dấu còn nguyên" out_is "$MARK"
  vroot bash -c 'cd / && runuser -u "$2" -- wp post get "$3" --field=post_title --path="/var/www/$1/public" 2>&1' _ "$S2" "$u2" "$POST"
  ok "bài viết dấu còn trong DB (wp post get)" out_is 'e2e-m6 dau du lieu s2'
  vroot bash -c "$LOCK_SCRIPT" _ "$u2"
  ok "user Linux mở khoá (never), user DB mở khoá (0)" bash -c 'grep -q ": never" <<<"$1" && grep -qE "^$2\s+0$" <<<"$1"' _ "$(tr -d '\r' <<<"$OUT")" "$u2"
  vroot bash -c 'ls -d "/var/lib/tpan/trash/$1" 2>&1; ls /var/lib/tpan/trash | wc -l' _ "$(slug_of "$S2")"
  ok "thùng rác không còn thư mục của $S2" has 'No such file'
  vroot tpan trash list
  ok "trash list trống" hasF 'Thùng rác trống.'
  vroot runuser -u "$u1" -- cat "/var/www/$S2/public/wp-config.php"
  ok "sau khôi phục: user của $S1 vẫn không đọc được wp-config.php của $S2" bash -c '[ "$2" != 0 ] && grep -q "Permission denied" <<<"$1"' _ "$OUT" "$RC"
  vroot tpan site restore "$S2" --yes
  ok "restore lần hai bị từ chối (site không ở thùng rác)" rc_not 0
  health_clean "sau site restore $S2"
  ok "health có kiểm lại $S2, mọi phép của nó không Fail" bash -c 'jq -e --arg t "$1" "any(.groups[]; .target == \$t)" "$2" >/dev/null' _ "$S2" "$HJ"
  ok "phép kiểm của $S2 không Fail, không lệch" target_all_pass "$S2"

  section "nginx g: xoá hẳn $S3 (rm rồi trash purge)"
  vroot tpan trash purge "$S1" --yes --json
  ok "purge site đang chạy ($S1) bị từ chối: site_not_trashed" bash -c '[ "$2" != 0 ] && [ "$(jq -r .error.code <<<"$1")" = site_not_trashed ]' _ "$OUT" "$RC"
  vexec bash -c "$PAGES_SCRIPT" _ "$S1"
  ok "$S1 vẫn 200 sau lời từ chối" has 'http=200'
  vroot tpan site rm "$S3" --yes
  ok "site rm $S3 thoát 0" rc_is 0
  vroot tpan trash purge "$S3" --yes
  ok "trash purge $S3 thoát 0, job thành công" bash -c '[ "$2" = 0 ] && grep -q "^Job .*: thành công" <<<"$1"' _ "$OUT" "$RC"
  vroot python3 -c "$JOB_PY" site.purge
  vroot bash -c "$LEFT_SCRIPT" _ "$S3" "$(user_of "$S3")" "$(slug_of "$S3")" "$PHPV" nginx
  ok "không còn gì của $S3: user, nhóm, DB, user DB, thư mục thùng rác, log, pool, vhost, dòng sites" hasnt '^CÒN'
  vexec curl -sS -m 10 -o /dev/null --resolve "$S3:80:127.0.0.1" "http://$S3/"
  ok "$S3 rơi vào site mặc định (curl mã 52)" rc_is 52
  vroot tpan trash purge "$S3" --yes
  ok "purge lần hai bị từ chối" rc_not 0

  section "nginx h: hết hạn: trash settings --days 0, rm $S4, trash purge --expired"
  vroot tpan trash settings --days 0
  ok "đặt số ngày giữ 0" bash -c '[ "$2" = 0 ] && grep -qF "thùng rác: 0 " <<<"$1"' _ "$OUT" "$RC"
  vroot tpan site rm "$S4" --yes
  ok "site rm $S4 thoát 0" rc_is 0
  vroot tpan trash list
  ok "trash list có $S4" has "^$S4 "
  vroot tpan trash purge --expired --dry-run
  ok "purge --expired --dry-run thoát 0, chỉ nói tới $S4" bash -c '[ "$2" = 0 ] && grep -q "^== $3" <<<"$1" && [ "$(grep -c "^== " <<<"$1")" = 1 ]' _ "$OUT" "$RC" "$S4"
  vroot tpan trash purge --expired --yes
  ok "purge --expired thoát 0" rc_is 0
  ok "xoá hẳn đúng $S4" bash -c 'grep -q "^== $2" <<<"$1" && [ "$(grep -c "^== " <<<"$1")" = 1 ]' _ "$OUT" "$S4"
  vroot bash -c "$LEFT_SCRIPT" _ "$S4" "$(user_of "$S4")" "$(slug_of "$S4")" "$PHPV" nginx
  ok "không còn gì của $S4" hasnt '^CÒN'
  KEEP=("$S1" "$S2" s5.example.com s6.example.com s7.example.com s8.example.com s9.example.com s10.example.com)
  vexec bash -c "$PAGES_SCRIPT" _ "${KEEP[@]}"
  ok "8 site còn lại vẫn 200" count_is 8 ' curl=0 http=200 '
  vroot tpan trash list
  ok "thùng rác trống" hasF 'Thùng rác trống.'
  vroot tpan trash purge --expired --yes
  ok "purge --expired lần hai: không có gì" bash -c '[ "$2" = 0 ] && grep -qF "Không có site hay DB rời nào quá hạn giữ." <<<"$1"' _ "$OUT" "$RC"
  vroot tpan trash settings --days 7
  ok "đặt lại số ngày giữ 7" hasF 'thùng rác: 7 '

  local S9=s9.example.com S10=s10.example.com
  section "nginx i: prune: sites.yaml bớt $S9 $S10 (và $S3, $S4 đã xoá hẳn)"
  write_yaml "$TMPD/sites-prune.yaml" "$S1" "$S2" s5.example.com s6.example.com s7.example.com s8.example.com
  push_yaml "$TMPD/sites-prune.yaml"
  vroot tpan apply -f "$YAML" --prune --dry-run
  ok "dry-run: 6 bỏ qua, 2 vào thùng rác ($S9, $S10), không tạo gì" bash -c '[ "$(grep -cE " bỏ qua \(đã có\)" <<<"$1")" = 6 ] && [ "$(grep -cE "^($2|$3) +vào thùng rác" <<<"$1")" = 2 ] && ! grep -qE "^s[0-9]+\.example\.com +tạo" <<<"$1" && grep -qF "tạo 0, đưa vào thùng rác 2" <<<"$1"' _ "$OUT" "$S9" "$S10"
  since="$(vm_now)"
  sleep 1
  t0="$(date +%s)"
  vroot tpan apply -f "$YAML" --prune --yes
  t1="$(date +%s)"
  sleep 1 # --until của journalctl tính tới giây: lấy mốc sau khi job xong hẳn
  until="$(vm_now)"
  printf 'THỜI GIAN apply --prune 2 site (đo từ Mac): %s giây\n' "$((t1 - t0))"
  ok "apply --prune thoát 0, job thành công" bash -c '[ "$2" = 0 ] && grep -q "^Job .*: thành công" <<<"$1"' _ "$OUT" "$RC"
  ok "bảng kết quả: $S9 và $S10 xong" bash -c 'grep -qE "^$2 +xong" <<<"$1" && grep -qE "^$3 +xong" <<<"$1"' _ "$OUT" "$S9" "$S10"
  reload_count apply nginx "$PHPU" "$since" "$until"
  ok "job prune có 2 unit.done ($S9, $S10), đều succeeded" bash -c '[ "$(grep -cE "^  unit.done ($2|$3) .*\"status\":\"succeeded\"" <<<"$1")" = 2 ]' _ "$(tr -d '\r' <<<"$JOBOUT")" "$S9" "$S10"
  vroot tpan trash list
  ok "trash list có $S9 và $S10" bash -c 'grep -q "^$2 " <<<"$1" && grep -q "^$3 " <<<"$1"' _ "$OUT" "$S9" "$S10"
  vroot bash -c 'for d in "$@"; do curl -sS -m 10 -o /dev/null --resolve "$d:80:127.0.0.1" "http://$d/"; echo "$d curl mã $?"; done' _ "$S9" "$S10"
  ok "$S9, $S10 rơi vào site mặc định (curl mã 52)" count_is 2 ' curl mã 52$'
  for d in "$S9" "$S10"; do
    vroot bash -c "$LOCK_SCRIPT" _ "$(user_of "$d")"
    ok "$d: user và user DB bị khoá" bash -c '! grep -q ": never" <<<"$1" && grep -qE "^$2\s+1$" <<<"$1"' _ "$(tr -d '\r' <<<"$OUT")" "$(user_of "$d")"
  done
  KEEP=("$S1" "$S2" s5.example.com s6.example.com s7.example.com s8.example.com)
  vexec bash -c "$PAGES_SCRIPT" _ "${KEEP[@]}"
  ok "6 site trong file vẫn 200" count_is 6 ' curl=0 http=200 '
  vroot tpan apply -f "$YAML" --prune --yes
  ok "chạy lại prune: không có gì thay đổi" bash -c '[ "$2" = 0 ] && grep -qF "Không có gì thay đổi." <<<"$1"' _ "$OUT" "$RC"
  health_clean "sau prune"

  panel_setup_login
  section "nginx k: API: POST /api/v1/apply?dry_run=1"
  vroot python3 -c "$JOB_PY"
  local jobsk
  jobsk="$(sed -nE 's/^số job trong tpan.db: ([0-9]+).*/\1/p' <<<"$OUT")"
  printf '{"defaults":{"admin_email":"%s"},"sites":[{"domain":"%s"},{"domain":"s11.example.com"}]}' "$EMAIL" "$S1" >"$TMPD/apply-body.json"
  api POST '/api/v1/apply?dry_run=1' "${MUT[@]}" -H 'Content-Type: application/json' --data-binary "@$TMPD/apply-body.json"
  ok "POST /apply?dry_run=1: 200" code_is 200
  show '{items, steps: ([.preview.steps[]?] | length)}'
  ok "items: $S1 skip, s11.example.com create" bash -c 'jq -e --arg a "$2" "[.items[] | \"\(.domain) \(.action)\"] == [\"\(\$a) skip\", \"s11.example.com create\"]" "$1" >/dev/null' _ "$TMPD/body" "$S1"
  vroot bash -c 'getent passwd | grep -c "^tp_s11" ; ls /var/www'
  ok "xem trước qua API không tạo gì cho s11" bash -c '[ "$(head -1 <<<"$1")" = 0 ] && ! grep -q s11 <<<"$1"' _ "$(tr -d '\r' <<<"$OUT")"
  vroot python3 -c "$JOB_PY"
  ok "không job mới (vẫn $jobsk job)" hasF "số job trong tpan.db: $jobsk"

  section "nginx k: API: DELETE /api/v1/sites/$S5 rồi POST /restore"
  api DELETE "/api/v1/sites/$S5?dry_run=1" "${MUT[@]}"
  ok "DELETE ?dry_run=1: 200, kind site.rm" bash -c '[ "$1" = 200 ] && jq -e ".kind == \"site.rm\"" "$2" >/dev/null' _ "$CODE" "$TMPD/body"
  api DELETE "/api/v1/sites/$S5" "${MUT[@]}"
  ok "DELETE: 202 có job_id" code_is 202
  wait_job "$(jb .job_id)"
  ok "job site.rm succeeded" test "$JOBST" = succeeded
  vexec curl -sS -m 10 -o /dev/null --resolve "$S5:80:127.0.0.1" "http://$S5/"
  ok "$S5 rơi vào site mặc định (curl mã 52)" rc_is 52
  api GET /api/v1/trash
  show '{days, sites: [.sites[] | {domain, status}]}'
  ok "GET /trash: 200, có $S5, $S9, $S10, days 7" bash -c '[ "$1" = 200 ] && jq -e --arg d "$3" "(.days == 7) and ([.sites[].domain] | index(\$d) != null) and (.sites | length == 3)" "$2" >/dev/null' _ "$CODE" "$TMPD/body" "$S5"
  api POST "/api/v1/sites/$S5/restore?dry_run=1" "${MUT[@]}"
  ok "POST /restore?dry_run=1: 200" code_is 200
  api POST "/api/v1/sites/$S5/restore" "${MUT[@]}"
  ok "POST /restore: 202" code_is 202
  wait_job "$(jb .job_id)"
  ok "job site.restore succeeded" test "$JOBST" = succeeded
  vexec bash -c "$PAGES_SCRIPT" _ "$S5"
  ok "$S5 trả 200 lại" has "^$S5 curl=0 http=200 "
  api DELETE "/api/v1/trash/$S1?dry_run=1" "${MUT[@]}"
  ok "DELETE /trash/$S1 (site đang chạy): 409 site_not_trashed" bash -c '[ "$1" = 409 ] && [ "$(jq -r .error.code "$2")" = site_not_trashed ]' _ "$CODE" "$TMPD/body"
  api GET /api/v1/export
  ok "GET /export: 200, YAML có $S1" bash -c '[ "$1" = 200 ] && grep -q "domain: $3" "$2"' _ "$CODE" "$TMPD/body" "$S1"
}

# ================================================================ lượt apache (ngắn)

run_apache() {
  local A=(s1.example.com s2.example.com s3.example.com) S2=s2.example.com
  section "apache 0: máy sạch, triển khai tpan"
  reset_vm
  deploy

  section "apache: tpan stack install --web apache"
  vroot tpan stack install --web apache --yes
  ok "stack install thoát 0" rc_is 0
  vexec php -r 'echo PHP_MAJOR_VERSION.".".PHP_MINOR_VERSION, "\n";'
  local PHPV PHPU
  PHPV="$(tr -d '\r\n' <<<"$OUT")"
  PHPU="php$PHPV-fpm"
  vexec systemctl is-active apache2 "$PHPU" mariadb
  ok "apache2, $PHPU, mariadb active" count_is 3 '^active$'

  section "apache: apply 3 site"
  write_yaml "$TMPD/sites-a.yaml" "${A[@]}"
  push_yaml "$TMPD/sites-a.yaml"
  local since until t0 t1
  since="$(vm_now)"
  sleep 1
  t0="$(date +%s)"
  apply_text -f "$YAML" --yes
  t1="$(date +%s)"
  sleep 1 # --until của journalctl tính tới giây: lấy mốc sau khi job xong hẳn
  until="$(vm_now)"
  printf 'THỜI GIAN apply 3 site apache (đo từ Mac): %s giây\n' "$((t1 - t0))"
  ok "apply thoát 0, job thành công" bash -c '[ "$2" = 0 ] && grep -q "^Job .*: thành công" <<<"$1"' _ "$OUT" "$RC"
  ok "3 mật khẩu quản trị, mỗi cái đúng một lần" bash -c '[ "$1" = 3 ] && [ "$2" = 1 ]' _ "$APPLY_PW_N" "$APPLY_PW_OK"
  read_dbpw "${A[@]}"
  reload_count apply apache2 "$PHPU" "$since" "$until"
  vexec bash -c "$PAGES_SCRIPT" _ "${A[@]}"
  ok "3 site 200, đúng trang của mình" bash -c 'n=0; for d in "${@:2}"; do grep -qE "^$d curl=0 http=200 nhắc=[1-9][0-9]* <title>$d</title>" <<<"$1" && n=$((n+1)); done; [ "$n" = 3 ]' _ "$(tr -d '\r' <<<"$OUT")" "${A[@]}"
  vroot runuser -u "$(user_of "${A[0]}")" -- cat "/var/www/$S2/public/wp-config.php"
  ok "cô lập: user của ${A[0]} không đọc được wp-config.php của $S2" bash -c '[ "$2" != 0 ] && grep -q "Permission denied" <<<"$1"' _ "$OUT" "$RC"

  section "apache: rm $S2 (403) rồi restore (200)"
  vroot bash -c "$TREE_SCRIPT" _ "/var/www/$S2"
  local TREE0="$OUT"
  vroot tpan site rm "$S2" --yes
  ok "site rm thoát 0, job thành công" bash -c '[ "$2" = 0 ] && grep -q "^Job .*: thành công" <<<"$1"' _ "$OUT" "$RC"
  vexec curl -sS -m 10 -o /dev/null -w '%{http_code}\n' --resolve "$S2:80:127.0.0.1" "http://$S2/"
  ok "$S2 ở thùng rác: Apache trả 403 (site mặc định)" out_is 403
  vroot bash -c "$LOCK_SCRIPT" _ "$(user_of "$S2")"
  ok "user và user DB bị khoá" bash -c '! grep -q ": never" <<<"$1" && grep -qE "^$2\s+1$" <<<"$1"' _ "$(tr -d '\r' <<<"$OUT")" "$(user_of "$S2")"
  vroot ls /etc/apache2/sites-enabled /etc/apache2/sites-available
  ok "link vhost gỡ, file vhost giữ" bash -c '[ "$(grep -c "^tpan-$2.conf$" <<<"$1")" = 1 ]' _ "$(tr -d '\r' <<<"$OUT")" "$S2"
  vroot tpan site restore "$S2" --yes
  ok "restore thoát 0, job thành công" bash -c '[ "$2" = 0 ] && grep -q "^Job .*: thành công" <<<"$1"' _ "$OUT" "$RC"
  vexec bash -c "$PAGES_SCRIPT" _ "$S2"
  ok "$S2 trả 200 lại, đúng trang" has "^$S2 curl=0 http=200 nhắc=[1-9][0-9]* <title>$S2</title>"
  vroot bash -c "$TREE_SCRIPT" _ "/var/www/$S2"
  ok "cây thư mục giống hệt trước khi xoá" test "$OUT" = "$TREE0"
  vroot bash -c "$LOCK_SCRIPT" _ "$(user_of "$S2")"
  ok "mở khoá cả hai" bash -c 'grep -q ": never" <<<"$1" && grep -qE "^$2\s+0$" <<<"$1"' _ "$(tr -d '\r' <<<"$OUT")" "$(user_of "$S2")"
  health_clean "apache sau restore"
}

case "${1:-all}" in
  all)
    run_nginx
    run_apache
    ;;
  nginx) run_nginx ;;
  apache) run_apache ;;
  *)
    sed -n '2,9p' "$0" | sed 's/^# \{0,1\}//'
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

section "TỔNG KẾT: $PASSES khẳng định đạt, $FAILS khẳng định không đạt"
[ "$FAILS" -eq 0 ]
