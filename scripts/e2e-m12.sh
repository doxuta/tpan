#!/usr/bin/env bash
# e2e của mốc M12 (phiên bản và cập nhật) trên máy ảo của mốc (tên trong file
# .tpan-vm; scripts/vm.sh tự đọc). Chạy trên Mac, trong thư mục repo:
#
#   scripts/e2e-m12.sh all   A, B, C, G, D, E rồi F (mặc định)
#   scripts/e2e-m12.sh a     máy sạch, cài stack, pool www (A)
#   scripts/e2e-m12.sh b     bước 14 (B): cần máy đã qua A
#   scripts/e2e-m12.sh c     bước 15 (C): cần máy đã qua B
#   scripts/e2e-m12.sh g     kịch bản lỗi 02 (G): cần máy đã qua B
#   scripts/e2e-m12.sh d     nâng MariaDB trên bản sao (D): cần máy đã qua B và C
#   scripts/e2e-m12.sh e     self-update (E): cần máy đã có stack
#   scripts/e2e-m12.sh f     máy sạch, nguồn nginx.org và MariaDB (F)
#
# Các phần:
#   A  `tpan stack install --web nginx`: pool www của php8.3-fpm được thay bằng
#      tpan-idle (không còn tiến trình "pool www"), php.www_pool:8.3 đạt; chạy
#      lại thì mọi bước được bỏ qua.
#   B  bước 14 ở mục 12 của docs/KE-HOACH.md: 2 site, `tpan php list`, cài PHP
#      8.4 (thêm kho sury có ghim), chuyển site 1 sang 8.4 rồi về 8.3 trong lúc
#      một vòng lặp curl gọi site 1 liên tục (0 request lỗi), site 2 ở yên 8.3,
#      gỡ 8.4.
#   C  bước 15: hạ bộ gói php8.3-* về bản của kho release (ĐỒ NGHỀ TEST, chỉ
#      trong máy ảo) để có gói đang chờ; `tpan update --check`, thử
#      unattended-upgrade --dry-run, `tpan update`, `tpan update auto`.
#   G  hồi quy: scripts/faults/02-pool-socket-rm.sh (socket có số bản) trên site 2.
#   D  nâng MariaDB 10.11 -> 11.8 trên một bản sao của máy ảo (multipass clone):
#      dữ liệu của từng site trước và sau, thời gian dừng của mariadb.
#   E  `tpan self-update` từ một máy chủ HTTP trong máy ảo (python3 -m
#      http.server trên 127.0.0.1), SHA256SUMS sai bị từ chối, --rollback.
#   F  máy sạch, `stack install --web-source nginx.org --db-source mariadb.org
#      --db-line 11.8`, một site.
#
# Đặt E2E_LOG=<file log> để cuối lần chạy script tự tìm trong chính file log
# đó: mọi mật khẩu đã gặp, IP máy ảo và bản sao (IPv4, IPv6), IP của Mac, thư
# mục nhà và tên user Mac (phải là 0 lần).
#
# Output như e2e-m5.sh: mỗi lệnh có dấu thời gian, output nguyên văn (đã che)
# và mã thoát; mỗi khẳng định in "=> ĐẠT" hoặc "=> KHÔNG ĐẠT"; cuối mỗi phần và
# cuối lần chạy in số đạt, không đạt. Che: mật khẩu (panel, quản trị WordPress,
# DB của site), IP máy ảo và bản sao, IP của Mac, mạng của Multipass, thư mục
# nhà và tên user Mac. Không in output thô của strace hay resolvectl (có IP
# resolver công khai); chỉ in số đếm.
# Không dùng set -e: nhiều lệnh ở đây được chờ là phải lỗi.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2

VM=scripts/vm.sh
VMNAME="$(tr -d '[:space:]' <.tpan-vm 2>/dev/null)"
VMNAME="${VMNAME:-tpan-test}"
CLONE="$VMNAME-clone"
FAILS=0
PASSES=0
OUT=""
RC=0
EMAIL=admin@example.com
S1=a.example.com
S2=b.example.com
S3=c.example.com
SURY_FPR=15058500A0235D97F5D10063B188E2B695BD4743
PORT=18081
TMPD="$(mktemp -d)"
chmod 700 "$TMPD"
SECRETS="$TMPD/secrets"
: >"$SECRETS"
chmod 600 "$SECRETS"
trap 'rm -rf "$TMPD"' EXIT
trap 'exit 130' INT TERM

ts() { date '+%Y-%m-%d %H:%M:%S'; }
section() { printf '\n\n######## [%s] %s\n' "$(ts)" "$*"; }

VM_IP=""
CLONE_IP=""
HOST_IP=""
NET4=""
VM_V6=""
V6PFX=""

# mask_str che một chuỗi (stdin) theo mọi thứ đã biết, in ra stdout.
mask_str() {
  local s a p
  s="$(cat)"
  [ -n "$VM_IP" ] && s="${s//"$VM_IP"/<vm>}"
  [ -n "$CLONE_IP" ] && s="${s//"$CLONE_IP"/<bản sao>}"
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
  s="$(sed -E -e 's/inet6 [0-9a-f:]{3,}/inet6 <v6>/g' <<<"$s")"
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
part_begin() { P0=$PASSES F0=$FAILS; section "PHẦN $*"; }
part_end() { printf '\n==== Phần %s: %s đạt, %s không đạt\n' "$1" "$((PASSES - P0))" "$((FAILS - F0))"; }

rc_is() { [ "$RC" -eq "$1" ]; }
rc_not() { [ "$RC" -ne "$1" ]; }
has() { grep -qE -- "$1" <<<"$OUT"; }
hasnt() { ! grep -qE -- "$1" <<<"$OUT"; }
hasF() { grep -qF -- "$1" <<<"$OUT"; }
out_is() { [ "$OUT" = "$1" ]; }

vexec() { run "$VM" exec "$@"; }
vroot() { run "$VM" root "$@"; }
cexec() { run env TPAN_VM="$CLONE" "$VM" exec "$@"; }
croot() { run env TPAN_VM="$CLONE" "$VM" root "$@"; }

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

# v6_of <tên máy>: các IPv6 global của máy đó, mỗi dòng một.
v6_of() {
  TPAN_VM="$1" "$VM" exec bash -c 'ip -o -6 addr show scope global | awk "{print \$4}" | cut -d/ -f1' </dev/null | tr -d '\r'
}

addrs() {
  VM_IP="$("$VM" ip)"
  NET4="${VM_IP%.*}."
  HOST_IP="$("$VM" exec bash -c 'echo "${SSH_CLIENT%% *}"' </dev/null | tr -d '\r')"
  local _
  for _ in $(seq 1 30); do
    VM_V6="$(v6_of "$VMNAME")"
    [ -n "$VM_V6" ] && break
    sleep 1
  done
  V6PFX="$(head -1 <<<"$VM_V6" | cut -d: -f1-4)"
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

# Danh sách job: loại, đích, trạng thái, thời gian chạy (giây).
JOBS_PY='
import sqlite3, datetime
def t(s):
    a, _, b = s.rstrip("Z").partition(".")
    return datetime.datetime.fromisoformat(a + "." + (b[:6] or "0"))
c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True)
for i, k, tg, st, a, b in c.execute("SELECT id, kind, target, status, started_at, finished_at FROM jobs ORDER BY started_at"):
    d = (t(b) - t(a)).total_seconds() if a and b else -1
    print(f"{k:16} {tg:22} {st:12} {d:7.1f} giây  ({i})")
'

# Dòng thời gian của job mới nhất thuộc một loại: bước, lệnh, không in output.
JOB_PY='
import sqlite3, sys
c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True)
j = c.execute("SELECT id, status FROM jobs WHERE kind = ? ORDER BY started_at DESC, rowid DESC LIMIT 1", (sys.argv[1],)).fetchone()
if j is None:
    print("không có job", sys.argv[1]); sys.exit(0)
print("job", sys.argv[1], j[0], j[1])
for t, kind, step, msg in c.execute("SELECT time, kind, step_id, message FROM job_events WHERE job_id = ? AND kind != \"output\" ORDER BY seq", (j[0],)):
    print(" ", t[11:23], kind, step[:70], msg[:160].replace("\n", " "))
'

# Vòng lặp curl (chạy nền bằng systemd-run, root): mỗi request một dòng
# "<thời điểm> <mã thoát của curl> <mã HTTP>". Lỗi kết nối (curl thoát khác 0)
# được ghi như mọi request khác.
CURL_LOOP='f=$1; d=$2; : >"$f"; while :; do c=$(curl -sS -m 10 -o /dev/null -w "%{http_code}" --resolve "$d:80:127.0.0.1" "http://$d/" 2>/dev/null); r=$?; echo "$(date +%s.%N) $r $c" >>"$f"; done'

LOOP_PY='
import sys
rows = [l.split() for l in open(sys.argv[1]) if l.strip()]
bad = [r for r in rows if len(r) < 3 or r[1] != "0" or r[2] != "200"]
print("tổng request:", len(rows))
print("request lỗi (curl thoát khác 0 hoặc mã khác 200):", len(bad))
if rows:
    print("từ", rows[0][0], "tới", rows[-1][0], "=", round(float(rows[-1][0]) - float(rows[0][0]), 2), "giây")
for r in bad[:20]:
    print("  lỗi:", " ".join(r))
'

# Đầu dò MariaDB (root, nền): mỗi 0,2 giây một "SELECT 1".
PROBE_LOOP='f=$1; : >"$f"; while :; do mariadb -N -e "SELECT 1" >/dev/null 2>&1; r=$?; echo "$(date +%s.%N) $r" >>"$f"; sleep 0.2; done'

PROBE_PY='
import sys
rows = [l.split() for l in open(sys.argv[1]) if l.strip()]
fails = [i for i, r in enumerate(rows) if r[1] != "0"]
print("số lần dò:", len(rows), "; số lần không vào được:", len(fails))
if fails:
    a, b = fails[0], fails[-1]
    before = float(rows[a - 1][0]) if a > 0 else float(rows[a][0])
    after = float(rows[b + 1][0]) if b + 1 < len(rows) else float(rows[b][0])
    print("lần lỗi đầu:", rows[a][0], "; lần lỗi cuối:", rows[b][0])
    print("thời gian dừng (lần đạt cuối trước khi dừng -> lần đạt đầu sau khi chạy lại):", round(after - before, 1), "giây")
'

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

# ---------------------------------------------------------------- site

PW_HITS=0
site_add() {
  local d=$1
  shift
  local cmd=("$VM" root tpan site add "$d" --email "$EMAIL" --yes "$@")
  printf '\n[%s] $ %s\n' "$(ts)" "${cmd[*]}"
  OUT="$("${cmd[@]}" 2>&1 </dev/null | tr -d '\r')"
  RC=$?
  local pw dbpw
  pw="$(sed -nE 's/^  Mật khẩu: +([A-Za-z0-9]{32})$/\1/p' <<<"$OUT" | head -1)"
  PW_HITS=0
  if [ -n "$pw" ]; then
    add_secret "$pw"
    PW_HITS="$(grep -oF -- "$pw" <<<"$OUT" | wc -l | tr -d ' ')"
  fi
  dbpw="$("$VM" root bash -c 'cd / && runuser -u "$2" -- wp config get DB_PASSWORD --path="/var/www/$1/public" 2>/dev/null' _ "$d" "$(user_of "$d")" </dev/null | tr -d '\r')"
  add_secret "$dbpw"
  mask
  printf '%s\n[%s] (mã thoát %s)\n' "$OUT" "$(ts)" "$RC"
  printf 'Mật khẩu quản trị trong output: %s lần; đã đọc mật khẩu DB của site vào danh sách che (%s ký tự)\n' "$PW_HITS" "${#dbpw}"
}

# http_code <domain>: mã HTTP gọi trong máy ảo, nối thẳng 127.0.0.1 cổng 80.
http_code() { vexec curl -sS -m 20 --resolve "$1:80:127.0.0.1" -o /dev/null -w '%{http_code}\n' "http://$1/"; }

# health_check [domain...]: `tpan health --json`, in gọn id và trạng thái
# từng phép kiểm (JSON đầy đủ có bằng chứng của mọi phép kiểm, quá dài cho
# log). HJ giữ JSON cho health_pass.
HJ=""
health_check() {
  local cmd=("$VM" root tpan health "$@" --json)
  printf '\n[%s] $ %s\n' "$(ts)" "${cmd[*]}"
  HJ="$("${cmd[@]}" 2>&1 </dev/null | tr -d '\r')"
  RC=$?
  OUT="$(jq -r '.groups[] | (if .target == "" then "(máy)" else .target end) as $t | .results[] | "\($t)  \(.id)  \(.status)" + (if .status != "pass" then "  -- " + .summary.vi else "" end)' <<<"$HJ" 2>&1)"
  mask
  printf '%s\n(chỉ in id, trạng thái và tóm tắt của phép kiểm không đạt; JSON đầy đủ không chép vào log)\n[%s] (mã thoát %s)\n' "$OUT" "$(ts)" "$RC"
}
# health_pass: mọi phép kiểm của máy, stack và site đều "pass" (nhóm "tpan"
# không tính: tự kiểm của tpan có "chưa rõ" khi chưa chạy tpan setup).
health_pass() { jq -e '[.groups[] | select(.target != "tpan") | .results[] | select(.status != "pass")] | length == 0' <<<"$HJ" >/dev/null 2>&1; }
health_has() { jq -e --arg id "$1" --arg st "$2" '[.groups[].results[] | select(.id == $id and .status == $st)] | length >= 1' <<<"$HJ" >/dev/null 2>&1; }

# curl_loop_start <unit> <domain>, curl_loop_stop <unit>: vòng lặp curl nền.
curl_loop_start() {
  vroot systemd-run --unit="$1" bash -c "$CURL_LOOP" _ "/root/$1.log" "$2"
  ok "vòng lặp curl $1 vào $2 đã chạy" rc_is 0
  sleep 2
}
curl_loop_stop() {
  sleep 3
  vroot systemctl stop "$1"
  vroot python3 -c "$LOOP_PY" "/root/$1.log"
  LOOP_N="$(sed -nE 's/^tổng request: ([0-9]+)$/\1/p' <<<"$OUT")"
  LOOP_BAD="$(sed -nE 's/^request lỗi .*: ([0-9]+)$/\1/p' <<<"$OUT")"
  ok "0 request lỗi trong lúc đổi bản PHP (lỗi: ${LOOP_BAD:-?})" test "${LOOP_BAD:-1}" -eq 0
  ok "số request đủ lớn (>= 200; có ${LOOP_N:-0})" test "${LOOP_N:-0}" -ge 200
}

# ================================================================ A: pool www

part_a() {
  part_begin "A: máy sạch, cài stack nginx, pool www thay bằng tpan-idle"
  reset_vm
  deploy
  vroot tpan stack install --web nginx --yes
  ok "stack install thoát 0" rc_is 0
  ok "job thành công" has '^Job .*: thành công'
  vexec ls -la /etc/php/8.3/fpm/pool.d
  ok "không còn www.conf" hasnt 'www\.conf'
  ok "có tpan-idle.conf" hasF 'tpan-idle.conf'
  vexec cat /etc/php/8.3/fpm/pool.d/tpan-idle.conf
  vexec bash -c 'ps -eo user,cmd | grep [p]hp-fpm'
  ok "không còn tiến trình 'pool www'" hasnt 'pool www'
  ok "chỉ có master của PHP-FPM 8.3" bash -c '[ "$(grep -c . <<<"$1")" -eq 1 ] && grep -q "^root .*master process (/etc/php/8.3/fpm/php-fpm.conf)" <<<"$1"' _ "$OUT"
  vexec systemctl is-active php8.3-fpm
  ok "php8.3-fpm active" out_is active
  vexec ls -la /run/php
  health_check
  ok "php.www_pool:8.3 đạt" health_has php.www_pool:8.3 pass
  ok "health: máy và stack đạt hết" health_pass

  section "A: chạy lại stack install: mọi bước được bỏ qua"
  vroot tpan stack install --web nginx --yes
  ok "thoát 0, thành công" bash -c '[ "$2" = 0 ] && grep -q "^Job .*: thành công" <<<"$1"' _ "$OUT" "$RC"
  ok "không bước nào chạy (không có dòng 'xong')" hasnt '^  xong$'
  vexec bash -c 'ls /etc/php/8.3/fpm/pool.d; ps -eo user,cmd | grep [p]hp-fpm'
  ok "vẫn chỉ tpan-idle.conf và master" bash -c '! grep -q "www" <<<"$1" && grep -q "tpan-idle.conf" <<<"$1"' _ "$OUT"
  vroot python3 -c "$JOBS_PY"
  part_end A
}

# ================================================================ B: bước 14

# fpm_layout: tiến trình, socket và file pool của hai site (sau vài request để
# pool ondemand có tiến trình con).
fpm_layout() {
  local _
  for _ in 1 2 3; do
    "$VM" exec curl -s -o /dev/null --resolve "$S1:80:127.0.0.1" "http://$S1/" </dev/null
    "$VM" exec curl -s -o /dev/null --resolve "$S2:80:127.0.0.1" "http://$S2/" </dev/null
  done
  vroot python3 -c "$PROC_PY"
  PROCS="$OUT"
  vexec ls -la /run/php
  SOCKS="$OUT"
  vexec bash -c 'ls /etc/php/*/fpm/pool.d/'
}

part_b() {
  part_begin "B: bước 14, nhiều bản PHP, đổi bản cho site 1 trong lúc curl liên tục"
  [ -n "$VM_IP" ] || addrs
  local slug1 slug2 u1 u2 d
  slug1="$(slug_of "$S1")" slug2="$(slug_of "$S2")"
  u1="tp_$slug1" u2="tp_$slug2"
  for d in "$S1" "$S2"; do
    site_add "$d"
    ok "site add $d thoát 0, job thành công" bash -c '[ "$2" = 0 ] && grep -q "^Job .*: thành công" <<<"$1"' _ "$OUT" "$RC"
    ok "mật khẩu quản trị in đúng một lần" test "$PW_HITS" -eq 1
  done
  vroot bash -c 'printf "127.0.0.1 %s %s\n" "$1" "$2" >>/etc/hosts && grep -n example /etc/hosts' _ "$S1" "$S2"
  ok "(đồ nghề) /etc/hosts: hai site -> 127.0.0.1" rc_is 0
  for d in "$S1" "$S2"; do
    http_code "$d"
    ok "$d trả 200" out_is 200
  done

  section "B: tpan php list (kho sury chưa thêm)"
  vroot tpan php list
  ok "php list thoát 0" rc_is 0
  ok "8.3 đã cài, mặc định, 2 site, nguồn ubuntu" has '^8\.3 +có +có +2 +ubuntu'
  ok "báo kho sury chưa thêm" hasF 'packages.sury.org'
  vroot tpan php list --json
  ok "--json: sury_enabled false, 8.3 installed+default" bash -c 'jq -e ".sury_enabled == false and ([.versions[] | select(.version == \"8.3\" and .installed and .default)] | length) == 1" <<<"$1" >/dev/null' _ "$OUT"
  vexec apt-cache search --names-only '^php[0-9]+\.[0-9]+-fpm$'

  section "B: tpan php install 8.4 --dry-run"
  vroot tpan php install 8.4 --dry-run
  ok "dry-run thoát 0" rc_is 0
  ok "có bước thêm kho sury (apt.repo:sury), sẽ chạy" has '\[sẽ chạy\] Thêm kho PHP của Ondřej Surý.*\(apt\.repo:sury\)'
  ok "bước kho có NGUY HIỂM và vân tay khoá" bash -c 'grep -A3 "(apt.repo:sury)" <<<"$1" | grep -q "NGUY HIỂM" && grep -qF "$2" <<<"$1"' _ "$OUT" "$SURY_FPR"
  ok "có bước gỡ pool www của 8.4 (php.idle:8.4)" hasF '(php.idle:8.4)'
  vexec bash -c 'ls /etc/apt/sources.list.d/ /etc/apt/keyrings/ /etc/apt/preferences.d/ 2>&1'
  ok "xem trước không thêm file kho nào" hasnt 'tpan-sury'

  section "B: tpan php install 8.4 --yes"
  vroot tpan php install 8.4 --yes
  ok "php install thoát 0, job thành công" bash -c '[ "$2" = 0 ] && grep -q "^Job .*: thành công" <<<"$1"' _ "$OUT" "$RC"
  vexec cat /etc/apt/sources.list.d/tpan-sury.sources
  ok "tpan-sury.sources có, URIs packages.sury.org, Signed-By tpan-sury.gpg" bash -c 'grep -q "^URIs: https://packages.sury.org/php/" <<<"$1" && grep -q "^Signed-By: /etc/apt/keyrings/tpan-sury.gpg" <<<"$1"' _ "$OUT"
  vexec gpg --show-keys --with-colons /etc/apt/keyrings/tpan-sury.gpg
  ok "khoá chỉ có đúng vân tay $SURY_FPR" bash -c '[ "$(grep "^fpr:" <<<"$1" | head -1 | cut -d: -f10)" = "$2" ] && [ "$(grep -c "^pub:" <<<"$1")" -eq 1 ]' _ "$OUT" "$SURY_FPR"
  vexec gpg --version
  vexec cat /etc/apt/preferences.d/tpan-sury
  ok "ghim: cả kho 100, php-common php8.4* 500" bash -c 'grep -q "^Package: \*" <<<"$1" && grep -q "^Pin-Priority: 100" <<<"$1" && grep -q "^Package: php-common php8.4\*" <<<"$1" && grep -q "^Pin-Priority: 500" <<<"$1"' _ "$OUT"
  vexec apt-cache policy php-fpm php8.3-fpm
  ok "php-fpm và php8.3-fpm: Candidate vẫn là bản Ubuntu" bash -c '[ "$(grep -c "Candidate: .*ubuntu" <<<"$1")" -eq 2 ] && ! grep -q "Candidate: .*sury" <<<"$1"' _ "$OUT"
  vexec php -v
  ok "php -v vẫn 8.3" has '^PHP 8\.3\.'
  vexec ls -la /etc/php/8.4/fpm/pool.d
  ok "8.4: không www.conf, có tpan-idle.conf" bash -c '! grep -q "www.conf" <<<"$1" && grep -q "tpan-idle.conf" <<<"$1"' _ "$OUT"
  vexec bash -c 'ps -eo user,cmd | grep [p]hp-fpm'
  ok "không tiến trình 'pool www' nào (8.3 và 8.4)" hasnt 'pool www'
  ok "có master của 8.3 và 8.4" bash -c 'grep -q "master process (/etc/php/8.3/" <<<"$1" && grep -q "master process (/etc/php/8.4/" <<<"$1"' _ "$OUT"
  vexec systemctl is-active php8.3-fpm php8.4-fpm
  vexec dpkg-query -W php8.4-fpm php-common php8.3-fpm

  section "B: (mục 16) php-fpm8.4 của kho sury: thời gian khởi động và truy vấn telemetry.sury.org"
  printf '(Chỉ in thời gian và SỐ dòng strace có chữ telemetry: output thô có IP resolver công khai.)\n'
  vroot bash -c 'for v in 8.3 8.4; do s=$(date +%s%N); php-fpm$v -t >/dev/null 2>&1; e=$(date +%s%N); echo "php-fpm$v -t: $(( (e - s) / 1000000 )) ms"; done'
  vroot bash -c 'strace -f -e trace=sendmmsg,sendto,connect -s 64 php-fpm8.4 -t 2>&1 | grep -c "telemetry"; strace -f -e trace=sendmmsg,sendto,connect -s 64 php-fpm8.3 -t 2>&1 | grep -c "telemetry"'
  vroot bash -c 'zcat /usr/share/doc/php8.4-common/changelog.Debian.gz | grep -n -i -B1 -A1 telemetry'

  section "B: đổi site 1 sang 8.4 trong lúc vòng lặp curl gọi site 1 liên tục"
  fpm_layout
  curl_loop_start tpan-e2e-curl-84 "$S1"
  vroot tpan php use "$S1" 8.4 --yes
  ok "php use $S1 8.4 thoát 0, job thành công" bash -c '[ "$2" = 0 ] && grep -q "^Job .*: thành công" <<<"$1"' _ "$OUT" "$RC"
  curl_loop_stop tpan-e2e-curl-84
  local n84=$LOOP_N
  vroot python3 -c "$JOB_PY" php.use

  section "B: site 1 ở 8.4, site 2 ở 8.3 (tiến trình, socket, file pool)"
  fpm_layout
  ok "pool tpan-$slug1 chạy dưới $u1, dưới master /etc/php/8.4/fpm/php-fpm.conf" grep -qxF "pool tpan-$slug1 user $u1 master /etc/php/8.4/fpm/php-fpm.conf" <<<"$PROCS"
  ok "không còn pool tpan-$slug1 dưới master 8.3" bash -c '! grep -qF "pool tpan-$2 user $3 master /etc/php/8.3/" <<<"$1"' _ "$PROCS" "$slug1" "$u1"
  ok "pool tpan-$slug2 chạy dưới $u2, dưới master 8.3" grep -qxF "pool tpan-$slug2 user $u2 master /etc/php/8.3/fpm/php-fpm.conf" <<<"$PROCS"
  ok "socket /run/php/tpan-$slug1-8.4.sock có (www-data, 0660), -8.3 không" bash -c 'grep -qE "^srw-rw---- .* www-data www-data .* tpan-$2-8\.4\.sock$" <<<"$1" && ! grep -q "tpan-$2-8\.3\.sock" <<<"$1"' _ "$SOCKS" "$slug1"
  ok "socket của site 2 vẫn -8.3" grep -qE "tpan-$slug2-8\.3\.sock$" <<<"$SOCKS"
  vexec bash -c 'ls -la /etc/php/8.4/fpm/pool.d/tpan-$1.conf /etc/php/8.3/fpm/pool.d/tpan-$1.conf 2>&1' _ "$slug1"
  ok "file pool site 1 ở /etc/php/8.4, không còn ở /etc/php/8.3" bash -c 'grep -q "^-rw-r--r-- .* /etc/php/8.4/fpm/pool.d/tpan-$2.conf" <<<"$1" && grep -q "/etc/php/8.3/fpm/pool.d/tpan-$2.conf.*No such file" <<<"$1"' _ "$OUT" "$slug1"
  vexec grep -n 'fastcgi_pass' "/etc/nginx/sites-available/tpan-$S1.conf" "/etc/nginx/sites-available/tpan-$S2.conf"
  vroot tpan site info "$S1" --json
  ok "site info $S1: php_version 8.4, socket -8.4" bash -c 'jq -e --arg s "/run/php/tpan-$2-8.4.sock" ".site.php_version == \"8.4\" and .socket == \$s" <<<"$1" >/dev/null' _ "$OUT" "$slug1"
  for d in "$S1" "$S2"; do
    http_code "$d"
    ok "$d trả 200" out_is 200
  done
  vexec curl -sS -m 20 --resolve "$S1:80:127.0.0.1" -o /dev/null -D - "http://$S1/"

  section "B: gỡ 8.4 khi còn site dùng: từ chối php_in_use"
  vroot tpan php remove 8.4 --yes --json
  ok "thoát khác 0, mã php_in_use" bash -c '[ "$2" != 0 ] && jq -e ".error.code == \"php_in_use\"" <<<"$1" >/dev/null' _ "$OUT" "$RC"
  vroot tpan php remove 8.4 --yes
  ok "bản chữ: thoát khác 0, nêu site $S1 còn dùng" bash -c '[ "$2" != 0 ] && grep -qF "$3" <<<"$1"' _ "$OUT" "$RC" "$S1"
  vexec dpkg-query -W -f '${Package} ${db:Status-Status}\n' php8.4-fpm
  ok "php8.4-fpm vẫn cài" hasF 'php8.4-fpm installed'

  section "B: đổi site 1 về 8.3 trong lúc curl liên tục"
  curl_loop_start tpan-e2e-curl-83 "$S1"
  vroot tpan php use "$S1" 8.3 --yes
  ok "php use $S1 8.3 thoát 0, job thành công" bash -c '[ "$2" = 0 ] && grep -q "^Job .*: thành công" <<<"$1"' _ "$OUT" "$RC"
  curl_loop_stop tpan-e2e-curl-83
  local n83=$LOOP_N
  vroot python3 -c "$JOB_PY" php.use
  fpm_layout
  ok "pool tpan-$slug1 lại dưới master 8.3" grep -qxF "pool tpan-$slug1 user $u1 master /etc/php/8.3/fpm/php-fpm.conf" <<<"$PROCS"
  ok "socket site 1 là -8.3" bash -c 'grep -q "tpan-$2-8\.3\.sock$" <<<"$1" && ! grep -q "tpan-$2-8\.4\.sock" <<<"$1"' _ "$SOCKS" "$slug1"
  vroot tpan php use "$S1" 8.3 --yes
  ok "đổi lần nữa về đúng bản đang dùng: thoát 0, không đổi gì" bash -c '[ "$2" = 0 ] && grep -q "không đổi gì" <<<"$1"' _ "$OUT" "$RC"

  section "B: gỡ PHP 8.4"
  vroot tpan php remove 8.4 --yes
  ok "php remove 8.4 thoát 0, job thành công" bash -c '[ "$2" = 0 ] && grep -q "^Job .*: thành công" <<<"$1"' _ "$OUT" "$RC"
  vexec bash -c 'dpkg-query -W -f "\${Package} \${db:Status-Status}\n" "php8.4*" 2>&1; ps -eo user,cmd | grep [p]hp-fpm; ls /etc/apt/sources.list.d/'
  ok "không gói php8.4-* nào còn installed" hasnt '^php8\.4[^ ]* installed'
  ok "không còn master 8.4" hasnt 'master process \(/etc/php/8\.4/'
  vroot tpan php list
  health_check
  ok "health: máy, stack và hai site đạt hết" health_pass
  for d in "$S1" "$S2"; do
    http_code "$d"
    ok "$d trả 200" out_is 200
  done
  printf '\nSố request của hai vòng lặp curl: 8.3->8.4: %s; 8.4->8.3: %s\n' "$n84" "$n83"
  vroot python3 -c "$JOBS_PY"
  part_end B
}

# ================================================================ C: bước 15

part_c() {
  part_begin "C: bước 15, có gói của stack đang chờ, tpan update"
  [ -n "$VM_IP" ] || addrs
  section "C: (đồ nghề, chỉ trong máy ảo) hạ bộ php8.3-* về bản của kho release"
  vexec apt-cache madison php8.3-fpm
  local old new pkgs
  old="$(awk -F'|' '$3 ~ /ubuntu.* noble\/(main|universe) / {gsub(/ /, "", $2); print $2; exit}' <<<"$OUT")"
  new="$("$VM" exec dpkg-query -W -f '${Version}' php8.3-fpm </dev/null | tr -d '\r')"
  printf 'Bản đang cài: %s; bản cũ hơn của kho release: %s\n' "$new" "$old"
  ok "có bản cũ hơn trong kho release" bash -c '[ -n "$1" ] && [ "$1" != "$2" ]' _ "$old" "$new"
  vexec apt-cache show "php8.3-fpm=$old"
  pkgs="$("$VM" exec dpkg-query -W -f '${Package} ${db:Status-Status}\n' 'php8.3-*' </dev/null | tr -d '\r' | awk '$2 == "installed" {print $1 "='"$old"'"}' | tr '\n' ' ')"
  printf 'Gói hạ cùng lúc (phụ thuộc đúng bản php8.3-common): %s\n' "$pkgs"
  # shellcheck disable=SC2086
  vroot env DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=300 install -y --allow-downgrades $pkgs
  ok "hạ gói thoát 0" rc_is 0
  vexec bash -c 'dpkg-query -W php8.3-fpm php8.3-common; ls /etc/php/8.3/fpm/pool.d; dpkg-query -W -f "\${Conffiles}\n" php8.3-fpm; systemctl is-active php8.3-fpm'
  ok "php8.3-fpm ở bản $old" bash -c '[ "$(awk "\$1 == \"php8.3-fpm\" {print \$2}" <<<"$1")" = "$2" ]' _ "$OUT" "$old"
  ok "hạ gói không đem www.conf về" bash -c '! grep -qE "^www.conf$" <<<"$1"' _ "$OUT"

  section "C: (lỗi đã sửa) khoá danh sách gói đang bị giữ, như lúc apt-daily chạy apt-get update ngay sau khi máy khởi động"
  printf '(Đồ nghề: một tiến trình python giữ khoá fcntl trên /var/lib/apt/lists/lock 12 giây, đúng loại khoá apt dùng.)\n'
  local lockpy='import fcntl, time; f = open("/var/lib/apt/lists/lock", "w"); fcntl.lockf(f, fcntl.LOCK_EX); time.sleep(12)'
  vroot systemd-run --unit=tpan-e2e-listslock1 python3 -c "$lockpy"
  sleep 1
  vroot apt-get update -o DPkg::Lock::Timeout=300
  ok "đối chứng: apt-get update lỗi ngay dù có DPkg::Lock::Timeout=300" bash -c '[ "$2" != 0 ] && grep -qF "Could not get lock /var/lib/apt/lists/lock" <<<"$1"' _ "$OUT" "$RC"
  vroot systemctl stop tpan-e2e-listslock1
  vroot systemd-run --unit=tpan-e2e-listslock2 python3 -c "$lockpy"
  sleep 1
  local t0 t1
  t0=$(date +%s)
  vroot tpan update --check --json
  t1=$(date +%s)
  printf 'tpan update --check chạy %s giây\n' "$((t1 - t0))"
  ok "tpan update --check chờ khoá rồi chạy được (thoát 0, có gói chờ)" bash -c '[ "$2" = 0 ] && jq -e ".packages | length > 0" <<<"$1" >/dev/null' _ "$OUT" "$RC"
  ok "đã thật sự chờ khoá (>= 5 giây)" test "$((t1 - t0))" -ge 5

  section "C: tpan update --check"
  vroot tpan update --check
  ok "update --check thoát 0" rc_is 0
  ok "liệt kê php8.3-fpm $old -> bản mới, đánh dấu bảo mật" has "^  php8\.3-fpm +$old -> [^ ]+ +\[bảo mật\]"
  vroot tpan update --check --json
  ok "--json: php8.3-fpm security=true, origins có -security" bash -c 'jq -e ".packages[] | select(.name == \"php8.3-fpm\") | .security == true and (.origins | any(test(\"-security\")))" <<<"$1" >/dev/null' _ "$OUT"

  section "C: (mục 16) unattended-upgrade --dry-run -d với www.conf đã xoá"
  vroot timeout 900 unattended-upgrade --dry-run -d
  local uu=$OUT
  ok "unattended-upgrade --dry-run thoát 0" rc_is 0
  printf '\nDòng liên quan (conffile, www.conf, php8.3-fpm):\n'
  grep -iE 'php8\.3-fpm|www\.conf|conffile prompt|All upgrades|left to upgrade' <<<"$uu" | grep -v '^<apt_pkg' | head -40
  ok "u-u thấy www.conf đã xoá ('in missing on the system') và không coi là conffile prompt" bash -c 'grep -qF "conffile /etc/php/8.3/fpm/pool.d/www.conf in missing on the system" <<<"$1" && ! grep -iE "conffile prompt" <<<"$1" | grep -q "php8.3"' _ "$uu"
  ok "u-u (dry-run) vẫn nâng các gói php8.3: 'All upgrades installed'" bash -c 'grep -qF "All upgrades installed" <<<"$1"' _ "$uu"

  section "C: tpan update --dry-run rồi --yes"
  vroot tpan update --dry-run
  ok "dry-run thoát 0" rc_is 0
  ok "có bước nâng gói gồm php8.3-fpm" has 'php8\.3-fpm'
  vroot tpan update --yes --verbose
  ok "update thoát 0, job thành công" bash -c '[ "$2" = 0 ] && grep -q "^Job .*: thành công" <<<"$1"' _ "$OUT" "$RC"
  ok "không có câu hỏi conffile (www.conf)" bash -c '! grep -qE "Configuration file .*www\.conf|\*\*\* www\.conf" <<<"$1"' _ "$OUT"
  ok "health sau job: stack và mọi site vẫn đạt" hasF 'Stack và mọi site vẫn đạt.'
  ok "kiểm chứng không có dòng 'chưa rõ' (bản 8.4 đã gỡ không bị kiểm)" hasnt '\[chưa rõ\]'
  vroot python3 -c "$JOB_PY" update.apply
  vexec bash -c 'dpkg-query -W php8.3-fpm php8.3-common; ls /etc/php/8.3/fpm/pool.d; dpkg --audit; ps -eo user,cmd | grep [p]hp-fpm; systemctl is-active php8.3-fpm nginx mariadb'
  ok "php8.3-fpm đã lên bản mới (khác $old)" bash -c 'v=$(awk "\$1 == \"php8.3-fpm\" {print \$2}" <<<"$1"); [ -n "$v" ] && [ "$v" != "$2" ]' _ "$OUT" "$old"
  ok "www.conf KHÔNG trở lại" bash -c '! grep -qE "^www.conf$" <<<"$1" && grep -q "tpan-idle.conf" <<<"$1"' _ "$OUT"
  ok "không tiến trình pool www" hasnt 'pool www'
  vroot tpan update --check
  ok "php8.3-fpm không còn chờ" hasnt '^  php8\.3-fpm '
  health_check
  ok "health: máy, stack và hai site đạt hết" health_pass
  for d in "$S1" "$S2"; do
    http_code "$d"
    ok "$d trả 200" out_is 200
  done

  section "C: tpan update auto"
  vroot tpan update auto --dry-run
  ok "auto --dry-run thoát 0" rc_is 0
  vroot tpan update auto --yes
  ok "auto thoát 0, job thành công" bash -c '[ "$2" = 0 ] && grep -q "^Job .*: thành công" <<<"$1"' _ "$OUT" "$RC"
  ok "kiểm chứng updates.auto đạt" hasF '[đạt] Bản vá bảo mật tự cài hằng ngày'
  vexec bash -c 'ls /etc/apt/apt.conf.d/; cat /etc/apt/apt.conf.d/52tpan-unattended-upgrades; ls /etc/apt/sources.list.d/'
  local sury=0
  hasF 'tpan-sury.sources' && sury=1
  vexec bash -c 'apt-config dump | grep -i -A3 Origins-Pattern'
  if [ "$sury" = 1 ]; then
    ok "kho sury còn file (sau php remove 8.4) nên Origins-Pattern có origin=deb.sury.org" hasF 'origin=deb.sury.org'
  else
    ok "kho sury không còn: Origins-Pattern không có deb.sury.org" bash -c '! grep -qF "origin=deb.sury.org" <<<"$1"' _ "$OUT"
  fi
  vroot tpan update auto --yes
  ok "chạy lại auto: không bước nào chạy" bash -c '[ "$2" = 0 ] && ! grep -q "^  xong$" <<<"$1"' _ "$OUT" "$RC"
  vroot python3 -c "$JOBS_PY"
  part_end C
}

# ================================================================ G: hồi quy kịch bản 02

part_g() {
  part_begin "G: scripts/faults/02-pool-socket-rm.sh trên $S2 (socket có số bản)"
  [ -n "$VM_IP" ] || addrs
  run scripts/faults/02-pool-socket-rm.sh break "$S2"
  ok "break thoát 0" rc_is 0
  health_check "$S2"
  ok "php.socket:$S2 lỗi" health_has "php.socket:$S2" fail
  vroot tpan health "$S2" --fix --yes
  ok "health --fix thoát 0" rc_is 0
  health_check "$S2"
  ok "php.socket:$S2 đạt lại" health_has "php.socket:$S2" pass
  ok "health của $S2 đạt hết" health_pass
  http_code "$S2"
  ok "$S2 trả 200" out_is 200
  part_end G
}

# ================================================================ D: nâng MariaDB trên bản sao

# site_data <hàm chạy root> <domain>: số bài viết, siteurl, checksum wp_posts.
site_data() {
  local fn=$1 d=$2 u
  u="$(user_of "$d")"
  "$fn" bash -c 'cd / && printf "posts=%s\n" "$(runuser -u "$2" -- wp post list --format=count --post_status=any --path="/var/www/$1/public" 2>/dev/null)"; printf "siteurl=%s\n" "$(runuser -u "$2" -- wp option get siteurl --path="/var/www/$1/public" 2>/dev/null)"; mariadb -N -e "CHECKSUM TABLE $2.wp_posts"' _ "$d" "$u"
}

part_d() {
  part_begin "D: nâng MariaDB 10.11 -> 11.8 trên bản sao $CLONE"
  [ -n "$VM_IP" ] || addrs
  run multipass version
  run multipass stop "$VMNAME"
  ok "dừng máy chính để clone" rc_is 0
  run multipass clone "$VMNAME" --name "$CLONE"
  ok "multipass clone thoát 0" rc_is 0
  run multipass start "$VMNAME"
  run multipass start "$CLONE"
  ok "bản sao chạy" rc_is 0
  CLONE_IP="$(TPAN_VM="$CLONE" "$VM" ip)"
  VM_V6="$VM_V6"$'\n'"$(v6_of "$CLONE")"
  printf 'IP bản sao: <bản sao> (che trong log)\n'
  cexec tpan version
  croot tpan stack status

  section "D: dữ liệu của từng site trước khi nâng"
  local d before1 before2
  site_data croot "$S1"
  before1="$OUT"
  site_data croot "$S2"
  before2="$OUT"
  ok "đọc được dữ liệu hai site" bash -c 'grep -q "^posts=[0-9]" <<<"$1" && grep -q "^posts=[0-9]" <<<"$2"' _ "$before1" "$before2"
  cexec mariadb --version

  section "D: tpan update --check (dòng nâng được)"
  croot tpan update --check
  ok "thấy dòng nâng lên 11.8" hasF 'sudo tpan mariadb upgrade --to 11.8'

  section "D: tpan mariadb upgrade --to 11.8 --dry-run"
  croot tpan mariadb upgrade --to 11.8 --dry-run
  ok "dry-run thoát 0" rc_is 0
  ok "có bước xuất DB, thêm kho MariaDB, nói đường lùi" bash -c 'grep -qi "mariadb-dump" <<<"$1" && grep -qF "(apt.repo:mariadb)" <<<"$1" && grep -q "đường lùi" <<<"$1"' _ "$OUT"
  ok "bước nâng gói hiện là sẽ chạy (không phải 'đã đúng, bỏ qua')" has '\[sẽ chạy\] Nâng gói: .*mariadb-server'
  croot bash -c 'ls /etc/apt/sources.list.d/; ls /var/lib/tpan/backups 2>&1'
  ok "xem trước không thêm kho MariaDB" hasnt 'tpan-mariadb'

  section "D: nâng thật (đầu dò mariadb chạy nền mỗi 0,2 giây)"
  croot systemd-run --unit=tpan-e2e-dbprobe bash -c "$PROBE_LOOP" _ /root/dbprobe.log
  sleep 2
  croot tpan mariadb upgrade --to 11.8 --yes --verbose
  ok "mariadb upgrade thoát 0, job thành công" bash -c '[ "$2" = 0 ] && grep -q "^Job .*: thành công" <<<"$1"' _ "$OUT" "$RC"
  ok "health sau job: stack và mọi site vẫn đạt" hasF 'Stack và mọi site vẫn đạt.'
  ok "kiểm chứng không có dòng 'chưa rõ'" hasnt '\[chưa rõ\]'
  sleep 2
  croot systemctl stop tpan-e2e-dbprobe
  croot python3 -c "$PROBE_PY" /root/dbprobe.log
  croot python3 -c "$JOB_PY" mariadb.upgrade
  printf '\n(mục 16) mariadb-upgrade: ai đã chạy nó (gói của MariaDB tự chạy khi dịch vụ khởi động?)\n'
  croot bash -c 'cat /var/lib/mysql/mariadb_upgrade_info; echo; systemctl cat mariadb | grep -E "^Exec"; journalctl -u mariadb --no-pager -o cat | grep -iE "upgrade|debian-start|Phase" | tail -15'

  section "D: sau khi nâng"
  cexec mariadb --version
  ok "mariadb --version là 11.8" has 'from 11\.8\.|11\.8\.[0-9]+-MariaDB'
  site_data croot "$S1"
  ok "dữ liệu $S1 như trước" test "$OUT" = "$before1"
  site_data croot "$S2"
  ok "dữ liệu $S2 như trước" test "$OUT" = "$before2"
  for d in "$S1" "$S2"; do
    cexec curl -sS -m 20 --resolve "$d:80:127.0.0.1" -o /dev/null -w '%{http_code}\n' "http://$d/"
    ok "$d trả 200" out_is 200
  done
  printf '\n(lệnh health dưới đây chạy trên BẢN SAO: TPAN_VM=%s)\n' "$CLONE"
  TPAN_VM="$CLONE" health_check
  ok "health trên bản sao: máy, stack và hai site đạt hết" health_pass
  croot tpan stack status --json
  ok "settings: db_source mariadb.org, db_line 11.8" bash -c 'jq -e ".config.db_source == \"mariadb.org\" and .config.db_line == \"11.8\"" <<<"$1" >/dev/null' _ "$OUT"
  croot bash -c 'ls -la /etc/mysql/mariadb.conf.d/; mariadbd --print-defaults 2>&1 | tr " " "\n" | grep -E "^--bind-address"; ss -tlnp | grep 3306'
  ok "99-tpan.cnf còn, mariadbd đọc bind-address=127.0.0.1, 3306 chỉ ở 127.0.0.1" bash -c 'grep -q "99-tpan.cnf" <<<"$1" && grep -q "^--bind-address=127.0.0.1" <<<"$1" && grep -q "127.0.0.1:3306" <<<"$1" && ! grep -qE "(0\.0\.0\.0|\*|\[::\]):3306" <<<"$1"' _ "$OUT"
  croot mariadb -N -e "SELECT User, Host, JSON_VALUE(Priv, '\$.plugin'), JSON_EXTRACT(Priv, '\$.auth_or') FROM mysql.global_priv WHERE User IN ('root', 'mariadb.sys', 'mysql')"
  ok "root@localhost vẫn có unix_socket" bash -c 'awk "\$1 == \"root\" && \$2 == \"localhost\"" <<<"$1" | grep -q unix_socket' _ "$OUT"
  croot bash -c 'ls -la /var/lib/tpan/backups/; for f in /var/lib/tpan/backups/*.sql; do echo "$f"; tail -1 "$f"; done'
  ok "file xuất có dòng cuối '-- Dump completed', quyền 0600" bash -c 'grep -q "^-- Dump completed" <<<"$1" && grep -q "^-rw------- .* root root .*\.sql$" <<<"$1"' _ "$OUT"
  croot bash -c 'dpkg-query -W "mariadb-*" galera-4 mysql-common 2>/dev/null; cat /etc/apt/sources.list.d/tpan-mariadb.sources; cat /etc/apt/preferences.d/tpan-mariadb 2>&1'
  croot tpan update --check
  croot python3 -c "$JOBS_PY"

  section "D: xoá bản sao $CLONE (chỉ máy do script này tạo)"
  if [ "${KEEP_CLONE:-0}" = 1 ]; then
    printf 'KEEP_CLONE=1: giữ bản sao để xem tiếp.\n'
  else
    run multipass delete --purge "$CLONE"
    ok "đã xoá bản sao" rc_is 0
    run bash -c 'multipass list | awk "{print \$1, \$2}"'
    ok "không còn $CLONE" hasnt "^$CLONE "
  fi
  part_end D
}

# ================================================================ E: self-update

part_e() {
  part_begin "E: tpan self-update từ máy chủ HTTP trong máy ảo"
  [ -n "$VM_IP" ] || addrs
  local arch rel=/srv/tpan-rel bad=/srv/tpan-rel-bad base sha_a
  arch="$("$VM" exec dpkg --print-architecture </dev/null | tr -d '\r')"
  run make build-linux VERSION=m12-a DATE="$BUILD_DATE"
  ok "build bản m12-a" rc_is 0
  cp "bin/tpan-linux-$arch" "$TMPD/tpan-a"
  run make build-linux VERSION=m12-b DATE="$BUILD_DATE"
  ok "build bản m12-b" rc_is 0
  cp "bin/tpan-linux-$arch" "$TMPD/tpan-b"
  run shasum -a 256 "$TMPD/tpan-a" "$TMPD/tpan-b"
  run multipass transfer "$TMPD/tpan-a" "$VMNAME:/tmp/tpan-a"
  run multipass transfer "$TMPD/tpan-b" "$VMNAME:/tmp/tpan-b"
  vroot install -m 0755 /tmp/tpan-a /usr/local/bin/tpan
  vexec tpan version
  ok "đang chạy m12-a" has '^tpan m12-a '

  section "E: tpan setup (để có tpan.service)"
  local cmd=("$VM" root tpan setup --yes) pw
  printf '\n[%s] $ %s\n' "$(ts)" "${cmd[*]}"
  OUT="$("${cmd[@]}" 2>&1 </dev/null | tr -d '\r')"
  RC=$?
  pw="$(grep -A1 '^Mật khẩu quản trị (chỉ hiện MỘT lần' <<<"$OUT" | sed -nE 's/^    ([A-Z2-7]{26})$/\1/p' | head -1)"
  add_secret "$pw"
  mask
  printf '%s\n[%s] (mã thoát %s)\n' "$OUT" "$(ts)" "$RC"
  ok "setup thoát 0" rc_is 0
  vexec systemctl is-active tpan
  ok "tpan.service active" out_is active

  section "E: dựng thư mục phát hành (bản b) và máy chủ HTTP 127.0.0.1:$PORT trong máy ảo"
  vroot bash -c 'set -e; a=$1; for d in $2 $3; do mkdir -p "$d"; install -m 0644 /tmp/tpan-b "$d/tpan-linux-$a"; done; (cd "$2" && sha256sum "tpan-linux-$a" >SHA256SUMS); printf "%064d  tpan-linux-%s\n" 0 "$a" >"$3/SHA256SUMS"; cat "$2/SHA256SUMS" "$3/SHA256SUMS"' _ "$arch" "$rel" "$bad"
  ok "thư mục phát hành và thư mục SHA256SUMS sai" rc_is 0
  vroot systemd-run --unit=tpan-e2e-rel python3 -m http.server "$PORT" --bind 127.0.0.1 --directory /srv
  sleep 2
  vexec curl -sS -o /dev/null -w '%{http_code}\n' "http://127.0.0.1:$PORT/tpan-rel/SHA256SUMS"
  ok "máy chủ HTTP trả SHA256SUMS" out_is 200
  base="http://127.0.0.1:$PORT"

  vroot sha256sum /usr/local/bin/tpan
  sha_a="$OUT"
  section "E: SHA256SUMS sai: bị từ chối, file chạy không đổi"
  vroot tpan self-update --url "$base/tpan-rel-bad" --yes
  ok "thoát khác 0" rc_not 0
  ok "báo sha256 không khớp" has 'sha256|SHA256'
  vroot sha256sum /usr/local/bin/tpan
  ok "file chạy không đổi" test "$OUT" = "$sha_a"
  vexec bash -c 'ls -la /usr/local/bin/ | grep tpan; tpan version'
  ok "không còn tpan.new, vẫn m12-a" bash -c '! grep -q "tpan.new" <<<"$1" && grep -q "^tpan m12-a " <<<"$1"' _ "$OUT"

  section "E: self-update --dry-run rồi --yes"
  vroot tpan self-update --url "$base/tpan-rel" --dry-run
  ok "dry-run thoát 0" rc_is 0
  vroot sha256sum /usr/local/bin/tpan
  ok "dry-run không đổi file chạy" test "$OUT" = "$sha_a"
  vexec systemctl show -p MainPID -p ExecMainStartTimestamp tpan
  local svc0=$OUT
  vroot tpan self-update --url "$base/tpan-rel" --yes
  ok "self-update thoát 0, job thành công" bash -c '[ "$2" = 0 ] && grep -q "^Job .*: thành công" <<<"$1"' _ "$OUT" "$RC"
  ok "in m12-a -> m12-b" hasF 'm12-a -> m12-b'
  vexec bash -c 'tpan version; ls -la /usr/local/bin/ | grep tpan; /usr/local/bin/tpan.old version'
  ok "tpan version là m12-b, tpan.old là m12-a" bash -c 'grep -q "^tpan m12-b " <<<"$1" && grep -q "tpan.old" <<<"$1" && [ "$(grep -c "^tpan m12-a " <<<"$1")" -eq 1 ]' _ "$OUT"
  sleep 2
  vexec systemctl show -p MainPID -p ExecMainStartTimestamp -p ActiveState tpan
  ok "tpan.service active, đã khởi động lại (PID mới)" bash -c 'grep -q "ActiveState=active" <<<"$1" && [ "$(grep MainPID <<<"$1")" != "$(grep MainPID <<<"$2")" ]' _ "$OUT" "$svc0"
  vroot bash -c 'p=$(systemctl show -p MainPID --value tpan); readlink /proc/$p/exe; /proc/$p/exe version'
  ok "tiến trình của dịch vụ chạy bản m12-b" has '^tpan m12-b '

  section "E: self-update --rollback --yes"
  vroot tpan self-update --rollback --yes
  ok "rollback thoát 0, job thành công" bash -c '[ "$2" = 0 ] && grep -q "^Job .*: thành công" <<<"$1"' _ "$OUT" "$RC"
  vexec tpan version
  ok "về m12-a" has '^tpan m12-a '
  vroot sha256sum /usr/local/bin/tpan
  ok "file chạy giống hệt bản a ban đầu" test "$OUT" = "$sha_a"
  sleep 2
  vexec systemctl is-active tpan
  ok "tpan.service active" out_is active
  vroot systemctl stop tpan-e2e-rel
  vroot python3 -c "$JOBS_PY"
  part_end E
}

# ================================================================ F: nginx.org và MariaDB lúc cài

part_f() {
  part_begin "F: máy sạch, nginx từ nginx.org, MariaDB 11.8 từ kho MariaDB"
  reset_vm
  deploy
  local flags=(--web nginx --web-source nginx.org --db-source mariadb.org --db-line 11.8)
  vroot tpan stack install "${flags[@]}" --dry-run
  ok "dry-run thoát 0" rc_is 0
  ok "xem trước có kho nginx và kho MariaDB" bash -c 'grep -qF "(apt.repo:nginx)" <<<"$1" && grep -qF "(apt.repo:mariadb)" <<<"$1"' _ "$OUT"
  vroot tpan stack install "${flags[@]}" --yes
  ok "stack install thoát 0, job thành công" bash -c '[ "$2" = 0 ] && grep -q "^Job .*: thành công" <<<"$1"' _ "$OUT" "$RC"
  vexec bash -c 'nginx -v 2>&1; dpkg-query -W nginx mariadb-server; mariadb --version; id nginx; ls /etc/nginx/conf.d /etc/php/8.3/fpm/pool.d; ps -eo user,cmd | grep -E "[n]ginx: worker|[p]hp-fpm"'
  ok "nginx là gói của nginx.org (~noble)" has '^nginx[[:space:]].*~noble'
  ok "MariaDB 11.8" has '11\.8\.'
  ok "user nginx thuộc nhóm www-data" has '^uid=.*\(nginx\).*www-data'
  ok "worker nginx chạy dưới user nginx" has '^nginx +nginx: worker'
  site_add "$S3"
  ok "site add $S3 thoát 0, job thành công" bash -c '[ "$2" = 0 ] && grep -q "^Job .*: thành công" <<<"$1"' _ "$OUT" "$RC"
  http_code "$S3"
  ok "$S3 trả 200" out_is 200
  health_check
  ok "health: máy, stack và site đạt hết" health_pass
  vroot tpan stack status --json
  ok "settings: web_source nginx.org, db_source mariadb.org, db_line 11.8" bash -c 'jq -e ".config.web_source == \"nginx.org\" and .config.db_source == \"mariadb.org\" and .config.db_line == \"11.8\"" <<<"$1" >/dev/null' _ "$OUT"
  vroot tpan stack install "${flags[@]}" --yes
  ok "chạy lại: thoát 0, không bước nào chạy" bash -c '[ "$2" = 0 ] && ! grep -q "^  xong$" <<<"$1"' _ "$OUT" "$RC"
  vroot python3 -c "$JOBS_PY"
  part_end F
}

case "${1:-all}" in
  all)
    part_a
    part_b
    part_c
    part_g
    part_d
    part_e
    part_f
    ;;
  a) part_a ;;
  b) part_b ;;
  c) part_c ;;
  d) part_d ;;
  e) part_e ;;
  f) part_f ;;
  g) part_g ;;
  *)
    sed -n '2,13p' "$0" | sed 's/^# \{0,1\}//'
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
  ok "log không chứa IP máy ảo" bash -c '[ -z "$1" ] || ! grep -qF -- "$1" "$2"' _ "$VM_IP" "$E2E_LOG"
  ok "log không chứa IP bản sao" bash -c '[ -z "$1" ] || ! grep -qF -- "$1" "$2"' _ "$CLONE_IP" "$E2E_LOG"
  ok "log không chứa mạng Multipass" bash -c '[ -z "$1" ] || ! grep -qF -- "$1" "$2"' _ "$NET4" "$E2E_LOG"
  ok "log không chứa IPv6 của máy ảo" bash -c '[ -z "$1" ] || ! grep -qF -- "$1" "$2"' _ "$V6PFX" "$E2E_LOG"
  ok "log không chứa IP của Mac" bash -c '[ -z "$1" ] || ! grep -qF -- "$1" "$2"' _ "$HOST_IP" "$E2E_LOG"
  ok "log không chứa thư mục nhà hay tên user Mac" bash -c '! grep -qF -- "$1" "$3" && ! grep -qF -- "$2" "$3"' _ "$HOME" "$USER" "$E2E_LOG"
fi

section "TỔNG KẾT: $PASSES khẳng định đạt, $FAILS khẳng định không đạt"
[ "$FAILS" -eq 0 ]
