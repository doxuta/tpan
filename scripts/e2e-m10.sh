#!/usr/bin/env bash
# e2e của mốc M10 (chuyển LEMP ⇄ LAMP tại chỗ) trên máy ảo của mốc (tên trong
# file .tpan-vm; scripts/vm.sh tự đọc). Chạy trên Mac, trong thư mục repo:
#
#   scripts/e2e-m10.sh all   A, B, E, C, D, E, F rồi G (mặc định)
#   scripts/e2e-m10.sh a     máy sạch, nginx, 4 site (SSL, PHP 8.4, thùng rác) (A)
#   scripts/e2e-m10.sh b     nginx -> apache (B): cần máy đã qua A
#   scripts/e2e-m10.sh c     apache -> nginx (C): cần máy đã qua B
#   scripts/e2e-m10.sh d     hoàn tác khi lỗi (D): cần máy đã qua C (đang nginx)
#   scripts/e2e-m10.sh e     khởi động lại máy ảo (E): máy đã chuyển ít nhất một lần
#   scripts/e2e-m10.sh f     máy sạch, nginx từ nginx.org, qua apache rồi về (F)
#   scripts/e2e-m10.sh g     máy sạch, LAMP trước, chuyển sang nginx (G)
#
# Các phần:
#   A  máy sạch, `tpan stack install --web nginx`, máy chủ ACME thử pebble (như
#      e2e-m5.sh), 4 site: a có SSL, b có bài viết với permalink /%postname%/,
#      c chuyển sang PHP 8.4, d có permalink rồi vào thùng rác.
#   B  bước 8 ở mục 12 của docs/KE-HOACH.md, nginx -> apache: --dry-run không
#      đổi gì (so dịch vụ, gói, cây /etc/nginx và /etc/apache2, policy-rc.d, số
#      job), rồi --yes trong lúc vòng lặp curl chạy nền trong máy ảo gọi từng
#      site (http và https). Kiểm độc lập: site, permalink, socket 8.4, dịch vụ,
#      policy-rc.d, cổng 443 mặc định, logrotate, file cũ, managed_files,
#      health, health --fix --dry-run, stack status; khôi phục d; sửa tay một
#      vhost thì health báo lệch đúng file đó, --fix đưa về.
#   C  apache -> nginx, giống B; chạy lại switch bị từ chối stack_same_web;
#      stack install chạy lại không đổi gì.
#   D  điều kiện đầu (site hỏng sẵn: precondition_failed), hoàn tác khi lỗi sớm
#      (file cấu hình apache hỏng) và khi lỗi sau khi đổi (.htaccess chỉ hỏng trên
#      Apache), đo gián đoạn của lần hoàn tác.
#   E  multipass restart: chỉ web server đang dùng chạy, mọi site 200.
#   F  máy sạch, nginx.org, một site, sang apache rồi về nginx: nguồn nginx.org,
#      worker dưới user nginx thuộc www-data. Kèm phép thử mục 16: gói nginx của
#      nginx.org cài lại dưới policy-rc.d.
#   G  máy sạch, `stack install --web apache`, một site, chuyển sang nginx: gói
#      nginx của Ubuntu cài mới dưới policy-rc.d (mục 16), --dry-run trên máy
#      chưa có nginx.
#
# Đo gián đoạn: mỗi cặp (site, scheme) một vòng lặp curl chạy nền trong máy ảo
# (systemd-run, root): mỗi request một kết nối mới (`Connection: close`,
# --resolve về 127.0.0.1), nghỉ 20 ms giữa hai request, mỗi dòng ghi thời điểm
# bắt đầu (EPOCHREALTIME), mã thoát của curl, mã HTTP, time_total. Mã coi là
# đạt: 301 cho http của site SSL, 200 cho phần còn lại. Mọi con số in vào khối
# "ĐO GIÁN ĐOẠN" cuối mỗi phần đo và cuối lần chạy.
#
# ĐỒ NGHỀ TEST, CHỈ TRONG MÁY ẢO: pebble và CA thử của API (xem đầu
# scripts/e2e-m5.sh), /etc/hosts trỏ các site về 127.0.0.1, policy-rc.d tự ghi ở
# phép thử mục 16 (F, và cuối B), file cấu hình hỏng và .htaccess hỏng ở D.
#
# Đặt E2E_LOG=<file log> để cuối lần chạy script tự tìm trong chính file log
# đó: mọi mật khẩu đã gặp, IP máy ảo (IPv4, IPv6), IP của Mac, thư mục nhà và
# tên user Mac (phải là 0 lần).
#
# Output như e2e-m5.sh và e2e-m12.sh: mỗi lệnh có dấu thời gian, output nguyên
# văn (đã che) và mã thoát; mỗi khẳng định in "=> ĐẠT" hoặc "=> KHÔNG ĐẠT"; cuối
# mỗi phần và cuối lần chạy in số đạt, không đạt.
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
S1=a.example.com # có SSL (pebble)
S2=b.example.com # bài viết, permalink /%postname%/; .htaccess hỏng ở D
S3=c.example.com # PHP 8.4
S4=d.example.com # permalink, vào thùng rác trước khi chuyển
POST=bai-e2e-m10
ACME=https://localhost:14000/dir
PEBBLE_UNIT=tpan-e2e-pebble
PEBBLE_DIR=/etc/tpan-e2e-pebble
TMPD="$(mktemp -d)"
chmod 700 "$TMPD"
SECRETS="$TMPD/secrets"
: >"$SECRETS"
chmod 600 "$SECRETS"
DO_FILE="$TMPD/do-gian-doan"
: >"$DO_FILE"

cleanup() {
  # Vòng lặp curl sót (script bị ngắt giữa chừng) không được dội tiếp vào lần sau.
  "$VM" root systemctl stop 'tpan-e2e-curl-*' </dev/null >/dev/null 2>&1
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
  s="$(sed -E -e 's/inet6 [0-9a-f:]{3,}/inet6 <v6>/g' <<<"$s")"
  printf '%s' "$s"
}

mask() { OUT="$(mask_str <<<"$OUT")"; }

# run in lệnh, chạy, in output đã che và mã thoát. Output vào file rồi mới đọc
# (multipass exec trên Mac có lúc treo khi stdout là pipe và output dài, M6);
# stdin là /dev/null (multipass exec không có terminal thì chờ stdin mãi).
run() {
  printf '\n[%s] $ %s\n' "$(ts)" "$(mask_str <<<"$*")"
  "$@" >"$TMPD/run.out" 2>&1 </dev/null
  RC=$?
  OUT="$(tr -d '\r' <"$TMPD/run.out")"
  mask
  [ -n "$OUT" ] && printf '%s\n' "$OUT"
  printf '[%s] (mã thoát %s)\n' "$(ts)" "$RC"
}

# quiet chạy như run nhưng không in output (chỉ dùng cho ảnh chụp trạng thái
# rồi so; phần khác nhau được in sau).
quiet() {
  printf '\n[%s] $ %s   (output giữ để so, không in)\n' "$(ts)" "$(mask_str <<<"$*")"
  "$@" >"$TMPD/run.out" 2>&1 </dev/null
  RC=$?
  OUT="$(tr -d '\r' <"$TMPD/run.out")"
  mask
  printf '[%s] (mã thoát %s; %s dòng)\n' "$(ts)" "$RC" "$(grep -c . <<<"$OUT")"
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
job_ok() { [ "$RC" = 0 ] && grep -q '^Job .*: thành công' <<<"$OUT"; }
job_rolled_back() { [ "$RC" != 0 ] && grep -q '^Job .*: hỏng, đã hoàn tác sạch' <<<"$OUT"; }

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

# Dựng pebble (đồ nghề test, chép từ scripts/e2e-m5.sh). Chạy bằng root.
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
update-ca-certificates 2>&1 | tail -2
printf "{\"pebble\":{\"listenAddress\":\"127.0.0.1:14000\",\"managementListenAddress\":\"127.0.0.1:15000\",\"certificate\":\"%s/cert.pem\",\"privateKey\":\"%s/key.pem\",\"httpPort\":80,\"tlsPort\":443,\"ocspResponderURL\":\"\",\"externalAccountBindingRequired\":false}}\n" "$d" "$d" >config.json
systemd-run --unit='"$PEBBLE_UNIT"' --setenv=PEBBLE_VA_NOSLEEP=1 --setenv=PEBBLE_WFE_NONCEREJECT=0 --setenv=PEBBLE_AUTHZREUSE=0 \
  /usr/bin/pebble -config "$d/config.json"
for i in $(seq 1 30); do
  curl -s -o /dev/null https://localhost:14000/dir && break
  sleep 1
done
'

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

# Dòng thời gian của job mới nhất thuộc một loại: bước, không in output lệnh.
JOB_PY='
import sqlite3, sys
c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True)
j = c.execute("SELECT id, status FROM jobs WHERE kind = ? ORDER BY started_at DESC, rowid DESC LIMIT 1", (sys.argv[1],)).fetchone()
if j is None:
    print("không có job", sys.argv[1]); sys.exit(0)
print("job", sys.argv[1], j[0], j[1])
for t, kind, step, msg in c.execute("SELECT time, kind, step_id, message FROM job_events WHERE job_id = ? AND kind NOT IN (\"output\", \"command\") ORDER BY seq", (j[0],)):
    print(" ", t[11:23], kind, step[:70], msg[:200].replace("\n", " "))
'

# Số job trong tpan.db.
NJOBS_PY='
import sqlite3
print("số job:", sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True).execute("SELECT COUNT(*) FROM jobs").fetchone()[0])
'

# Dòng output của job stack.switch mới nhất có một trong các chữ cho trước (để
# thấy policy-rc.d chặn khởi động và disable --now gỡ link, không in hết apt).
JOBOUT_PY='
import sqlite3, sys, re
c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True)
j = c.execute("SELECT id FROM jobs WHERE kind = \"stack.switch\" ORDER BY started_at DESC, rowid DESC LIMIT 1").fetchone()
pat = re.compile(sys.argv[1])
for t, kind, step, msg in c.execute("SELECT time, kind, step_id, message FROM job_events WHERE job_id = ? AND kind IN (\"output\", \"command\") ORDER BY seq", (j[0],)):
    if pat.search(msg):
        print(" ", t[11:23], kind, step[:40], msg[:200].replace("\n", " "))
'

# Lần "Started <unit>" trong journal so với lúc bắt đầu bước web.swap của job
# stack.switch mới nhất (cả hai là đồng hồ của máy ảo).
JSTART_PY='
import sqlite3, sys, json, subprocess, datetime
unit = sys.argv[1]
def ep(s):
    a, _, b = s.rstrip("Z").partition(".")
    return datetime.datetime.fromisoformat(a + "." + (b[:6] or "0")).replace(tzinfo=datetime.timezone.utc).timestamp()
c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True)
j = c.execute("SELECT id, started_at FROM jobs WHERE kind = \"stack.switch\" ORDER BY started_at DESC, rowid DESC LIMIT 1").fetchone()
sw = c.execute("SELECT time FROM job_events WHERE job_id = ? AND step_id LIKE \"web.swap:%\" AND kind = \"step.start\"", (j[0],)).fetchone()
t0 = ep(sw[0])
out = subprocess.run(["journalctl", "-u", unit, "-o", "json", "--no-pager"], capture_output=True, text=True).stdout
starts = []
for l in out.splitlines():
    e = json.loads(l)
    m = e.get("MESSAGE", "")
    if isinstance(m, str) and m.startswith("Started ") and unit in m:
        starts.append(int(e["__REALTIME_TIMESTAMP"]) / 1e6)
print("số lần Started", unit, "trong journal:", len(starts))
for s in starts:
    print(f"  Started lúc {(s - t0) * 1000:+.0f} ms so với step.start của web.swap")
print("Started trước web.swap:", sum(1 for s in starts if s < t0))
'

# Ảnh chụp để so trước và sau --dry-run: dịch vụ, gói, policy-rc.d, cây
# /etc/nginx và /etc/apache2 (loại, đường dẫn, đích link, sha256 file), số job.
SNAP_SCRIPT='
for u in nginx apache2; do echo "$u $(systemctl is-active $u) $(systemctl is-enabled $u 2>&1)"; done
dpkg-query -W -f "\${Package} \${db:Status-Status}\n" nginx apache2 2>&1
ls -la /usr/sbin/policy-rc.d 2>&1
for d in /etc/nginx /etc/apache2 /etc/logrotate.d; do [ -d $d ] && find $d -printf "%y %p %l\n" | sort; done
find /etc/nginx /etc/apache2 /etc/logrotate.d -type f 2>/dev/null | sort | xargs -r sha256sum
python3 -c "$1"
'

# Cây /etc/<dir>: loại, đường dẫn, đích link, sha256 file.
TREE_SCRIPT='
cd "$1" || exit 1
find . -printf "%y %p %l\n" | sort
find . -type f | sort | xargs -r sha256sum
'

# managed_files so với đĩa. Đối số: thư mục của web server mới, thư mục của
# web server cũ. In mọi dòng (khớp/LỆCH), dòng còn trỏ vào thư mục cũ, file
# tpan của web server mới trên đĩa mà không có dòng.
MF_PY='
import sqlite3, hashlib, os, sys
new, old = sys.argv[1], sys.argv[2]
c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True)
rows = dict(c.execute("SELECT path, sha256 FROM managed_files").fetchall())
bad = 0
for p in sorted(rows):
    try:
        got = hashlib.sha256(open(p, "rb").read()).hexdigest()
    except FileNotFoundError:
        got = "không có file"
    if got != rows[p]:
        bad += 1
    print("khớp" if got == rows[p] else "LỆCH", p)
print("số dòng:", len(rows), "; lệch hoặc thiếu file:", bad)
print("dòng còn trỏ vào", old + ":", sum(1 for p in rows if p.startswith(old + "/")))
want = ["/etc/logrotate.d/tpan-sites"]
for root, dirs, files in os.walk(new):
    for f in files:
        p = os.path.join(root, f)
        if "tpan" in f and not os.path.islink(p):
            want.append(p)
miss = [p for p in want if p not in rows]
for p in miss:
    print("KHÔNG CÓ DÒNG", p)
print("file tpan của", new, "trên đĩa:", len(want), "; không có dòng:", len(miss))
'

# Vòng lặp curl: $1 file log, $2 URL, còn lại là cờ curl (--resolve...).
CURL_LOOP='f=$1; u=$2; shift 2; : >"$f"; while :; do a=$EPOCHREALTIME; o=$(curl -s -o /dev/null --connect-timeout 1 -m 3 -H "Connection: close" -w "%{http_code} %{time_total}" "$@" "$u" 2>/dev/null); r=$?; echo "$a $r $o" >>"$f"; sleep 0.02; done'

# Phân tích vòng lặp curl. argv[1]: nhãn lượt; còn lại "tên:file:mã đạt".
# Mốc 0 là step.start của bước web.swap trong job stack.switch mới nhất.
# "Khoảng dài nhất không có mã đạt": khoảng lớn nhất giữa hai lần NHẬN được
# mã đạt liên tiếp (lúc request đạt trước kết thúc tới lúc request đạt sau kết
# thúc); lúc bình thường khoảng này cỡ 20 ms nghỉ cộng thời gian một request.
LOOP_PY='
import sys, sqlite3, datetime, collections, statistics
tag = sys.argv[1]
def ep(s):
    a, _, b = s.rstrip("Z").partition(".")
    return datetime.datetime.fromisoformat(a + "." + (b[:6] or "0")).replace(tzinfo=datetime.timezone.utc).timestamp()
c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True)
j = c.execute("SELECT id, status FROM jobs WHERE kind = \"stack.switch\" ORDER BY started_at DESC, rowid DESC LIMIT 1").fetchone()
marks = {}
for t, k in c.execute("SELECT time, kind FROM job_events WHERE job_id = ? AND step_id LIKE \"web.swap:%\" ORDER BY seq", (j[0],)):
    marks.setdefault(k, ep(t))
t0 = marks.get("step.start", 0.0)
print("job stack.switch", j[0], j[1])
for k in ("step.start", "step.done", "step.fail", "undo.start", "undo.done", "undo.fail"):
    if k in marks:
        print(f"  web.swap {k}: {(marks[k] - t0) * 1000:+.0f} ms")
sd, us, ud = marks.get("step.done"), marks.get("undo.start"), marks.get("undo.done")
if "step.start" in marks and sd:
    print("ĐO", tag, f"bước web.swap (step.start -> step.done): {(sd - t0) * 1000:.0f} ms")
if us and ud:
    print("ĐO", tag, f"hoàn tác web.swap (undo.start -> undo.done): {(ud - us) * 1000:.0f} ms (undo.start ở {(us - t0) * 1000:+.0f} ms)")
for spec in sys.argv[2:]:
    name, f, want = spec.split(":")
    rows = []
    for l in open(f):
        p = l.split()
        if len(p) >= 4:
            rows.append((float(p[0]), p[1], p[2], float(p[3])))
    good = [r for r in rows if r[1] == "0" and r[2] == want]
    bad = [r for r in rows if not (r[1] == "0" and r[2] == want)]
    codes = collections.Counter(f"curl {r[1]}/HTTP {r[2]}" for r in bad)
    ends = [r[0] + r[3] for r in good]
    gaps = [(ends[i + 1] - ends[i], ends[i], ends[i + 1]) for i in range(len(ends) - 1)]
    g = max(gaps) if gaps else (0, 0, 0)
    med = statistics.median(x[0] for x in gaps) if gaps else 0
    # Gián đoạn: mỗi chuỗi request không đạt liền nhau, tính từ lúc request
    # đạt ngay trước nó kết thúc tới lúc request đạt ngay sau nó kết thúc.
    outs, i = [], 0
    while i < len(rows):
        if rows[i][1] == "0" and rows[i][2] == want:
            i += 1
            continue
        k = i
        while k < len(rows) and not (rows[k][1] == "0" and rows[k][2] == want):
            k += 1
        a = rows[i - 1][0] + rows[i - 1][3] if i > 0 else rows[i][0]
        b = rows[k][0] + rows[k][3] if k < len(rows) else rows[k - 1][0] + rows[k - 1][3]
        outs.append((b - a, a, b, k - i))
        i = k
    o = max(outs) if outs else (0, t0, t0, 0)
    slow = max(rows, key=lambda r: r[3]) if rows else (0, "", "", 0)
    print(f"ĐO {tag} {name} (đạt = {want}): {len(rows)} request, {len(bad)} không đạt {dict(codes)}; gián đoạn dài nhất (quanh một chuỗi không đạt): {o[0] * 1000:.0f} ms, {o[3]} request, từ {(o[1] - t0) * 1000:+.0f} tới {(o[2] - t0) * 1000:+.0f} ms so với web.swap; khoảng dài nhất giữa hai lần nhận {want} cả lượt: {g[0] * 1000:.0f} ms (từ {(g[1] - t0) * 1000:+.0f} ms); trung vị {med * 1000:.0f} ms; request chậm nhất {slow[3] * 1000:.0f} ms (bắt đầu {(slow[0] - t0) * 1000:+.0f} ms)")
    if bad:
        print(f"   {name}: không đạt đầu tiên bắt đầu {(bad[0][0] - t0) * 1000:+.0f} ms, cuối cùng kết thúc {(bad[-1][0] + bad[-1][3] - t0) * 1000:+.0f} ms")
    tail = rows[-10:]
    print("ĐUÔI", name, "đạt" if tail and all(r[1] == "0" and r[2] == want for r in tail) else "lỗi", "(10 request cuối)")
'

# Phép thử mục 16 (đồ nghề): cài lại gói chứa <unit>.service dưới một
# policy-rc.d exit 101 tự ghi, khi unit đang tắt và không bật khi khởi động
# (web server kia đang giữ cổng). In output apt liên quan, trạng thái trước,
# sau, và sau `systemctl disable --now`. Gỡ policy-rc.d sau đó.
POLICY_PROBE='
unit=$1
pkg=$(dpkg -S "/usr/lib/systemd/system/$unit.service" 2>/dev/null | cut -d: -f1 | head -1)
[ -n "$pkg" ] || pkg=$(dpkg -S "/lib/systemd/system/$unit.service" 2>/dev/null | cut -d: -f1 | head -1)
echo "gói chứa $unit.service: $pkg $(dpkg-query -W -f "\${Version}" "$pkg")"
echo "trước: $(systemctl is-active "$unit") $(systemctl is-enabled "$unit")"
[ -e /usr/sbin/policy-rc.d ] && { echo "đã có policy-rc.d, dừng"; exit 3; }
printf "#!/bin/sh\nexit 101\n" >/usr/sbin/policy-rc.d
chmod 755 /usr/sbin/policy-rc.d
DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=300 install --reinstall -y "$pkg" >/tmp/probe-apt.log 2>&1
echo "apt-get mã $?"
grep -iE "policy-rc|invoke-rc|denied|Setting up|symlink|Removed" /tmp/probe-apt.log
rm -f /usr/sbin/policy-rc.d
echo "sau khi cài lại: $(systemctl is-active "$unit") $(systemctl is-enabled "$unit")"
systemctl disable --now "$unit" 2>&1
echo "sau disable --now: $(systemctl is-active "$unit") $(systemctl is-enabled "$unit")"
ls -la /usr/sbin/policy-rc.d 2>&1
'

# Phép thử thêm (đồ nghề): như lúc apt nâng gói của web server ĐÃ TẮT sau khi
# chuyển (unattended-upgrades vá nginx trên máy đang chạy Apache): cài lại gói
# chứa <unit>.service, KHÔNG có policy-rc.d. Postinst có khởi động lại unit
# disabled đó không (nó sẽ tranh cổng với web server đang chạy)? Đối số: unit,
# unit của web server đang chạy. Có lỡ chạy thì dừng lại cho các phần sau.
NOPOLICY_PROBE='
unit=$1; run=$2
pkg=$(dpkg -S "/usr/lib/systemd/system/$unit.service" 2>/dev/null | cut -d: -f1 | head -1)
echo "gói chứa $unit.service: $pkg $(dpkg-query -W -f "\${Version}" "$pkg")"
echo "trước: $unit $(systemctl is-active "$unit") $(systemctl is-enabled "$unit"); $run $(systemctl is-active "$run")"
ls -la /usr/sbin/policy-rc.d 2>&1
DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=300 install --reinstall -y "$pkg" >/tmp/probe-apt2.log 2>&1
echo "apt-get mã $?"
grep -iE "invoke-rc|deb-systemd|restart|start|denied|Job for|fail|Setting up" /tmp/probe-apt2.log
echo "sau khi cài lại (không policy-rc.d): $unit $(systemctl is-active "$unit") $(systemctl is-enabled "$unit"); $run $(systemctl is-active "$run")"
if systemctl is-active -q "$unit" || systemctl is-failed -q "$unit"; then systemctl stop "$unit"; systemctl reset-failed "$unit"; echo "đã dừng lại $unit"; fi
'

# Mọi site: mã HTTP và URL chuyển hướng. Đối số: "tên|URL|--resolve".
SITES_SCRIPT='
for s in "$@"; do
  n=${s%%|*}; r=${s#*|}; u=${r%%|*}; res=${r#*|}
  c=$(curl -sS -k -m 20 -o /dev/null -w "%{http_code} %{redirect_url}" --resolve "$res" "$u" 2>/dev/null)
  echo "$n $c" | sed "s/ *$//"
done
'

# ---------------------------------------------------------------- site

PW_HITS=0
site_add() {
  local d=$1
  shift
  local cmd=("$VM" root tpan site add "$d" --email "$EMAIL" --yes "$@")
  printf '\n[%s] $ %s\n' "$(ts)" "${cmd[*]}"
  "${cmd[@]}" >"$TMPD/run.out" 2>&1 </dev/null
  RC=$?
  OUT="$(tr -d '\r' <"$TMPD/run.out")"
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

# health_check [domain...]: `tpan health --json`, in gọn id và trạng thái.
HJ=""
health_check() {
  local cmd=("$VM" root tpan health "$@" --json)
  printf '\n[%s] $ %s\n' "$(ts)" "${cmd[*]}"
  "${cmd[@]}" >"$TMPD/run.out" 2>&1 </dev/null
  RC=$?
  HJ="$(tr -d '\r' <"$TMPD/run.out")"
  OUT="$(jq -r '.groups[] | (if .target == "" then "(máy)" else .target end) as $t | .results[] | "\($t)  \(.id)  \(.status)" + (if .status != "pass" then "  -- " + .summary.vi else "" end)' <<<"$HJ" 2>&1)"
  mask
  printf '%s\n(chỉ in id, trạng thái và tóm tắt của phép kiểm không đạt; JSON đầy đủ không chép vào log)\n[%s] (mã thoát %s)\n' "$OUT" "$(ts)" "$RC"
  printf 'Số phép kiểm: %s; Fail: %s; drift: %s\n' "$(jq '[.groups[].results[]] | length' <<<"$HJ" 2>/dev/null)" "$(jq '[.groups[].results[] | select(.status == "fail")] | length' <<<"$HJ" 2>/dev/null)" "$(jq '[.groups[].results[] | select(.id | startswith("drift:"))] | length' <<<"$HJ" 2>/dev/null)"
}
# health_pass: mọi phép kiểm của máy, stack và site đều "pass" (nhóm "tpan"
# không tính: tự kiểm của tpan có "chưa rõ" khi chưa chạy tpan setup).
# Cảnh báo DNS (dns.a) và TLS (tls.cert) là có chủ ý trên máy ảo: tên miền thử
# không có bản ghi A thật (mục 6.7).
health_pass() { jq -e '[.groups[] | select(.target != "tpan") | .results[] | select(.status != "pass" and ((.status == "warn" and (.id | test("^(dns[.]a|tls[.]cert):"))) | not))] | length == 0' <<<"$HJ" >/dev/null 2>&1; }
health_has() { jq -e --arg id "$1" --arg st "$2" '[.groups[].results[] | select(.id == $id and .status == $st)] | length >= 1' <<<"$HJ" >/dev/null 2>&1; }
health_fails() { jq -r '[.groups[].results[] | select(.status == "fail") | .id] | join(" ")' <<<"$HJ" 2>/dev/null; }

njobs() { "$VM" root python3 -c "$NJOBS_PY" </dev/null | tr -d '\r'; }

# Mục tiêu của vòng lặp curl và kiểm site: "tên|URL|--resolve|mã đạt".
T_A_HTTP="a-http|http://$S1/|$S1:80:127.0.0.1|301"
T_A_HTTPS="a-https|https://$S1/|$S1:443:127.0.0.1|200"
T_B="b-http|http://$S2/|$S2:80:127.0.0.1|200"
T_C="c-http|http://$S3/|$S3:80:127.0.0.1|200"
T_D="d-http|http://$S4/|$S4:80:127.0.0.1|200"
T_PL="b-permalink|http://$S2/$POST/|$S2:80:127.0.0.1|200"

# sites_ok <mục tiêu...>: gọi từng mục tiêu một lần, khẳng định đúng mã (301
# của site SSL phải chuyển sang https://<site>/).
sites_ok() {
  local args=() t n u res want
  for t in "$@"; do
    IFS='|' read -r n u res want <<<"$t"
    args+=("$n|$u|$res")
  done
  vexec bash -c "$SITES_SCRIPT" _ "${args[@]}"
  for t in "$@"; do
    IFS='|' read -r n u res want <<<"$t"
    if [ "$want" = 301 ]; then
      ok "$n: 301 sang https://${u#http://}" grep -qxF "$n 301 https://${u#http://}" <<<"$OUT"
    else
      ok "$n: $want" grep -qxF "$n $want" <<<"$OUT"
    fi
  done
}

LOOPS=()
LOOP_TAG=""
loops_start() {
  LOOP_TAG=$1
  shift
  LOOPS=()
  local t n u res want unit
  for t in "$@"; do
    IFS='|' read -r n u res want <<<"$t"
    unit="tpan-e2e-curl-$LOOP_TAG-$n"
    vroot systemd-run --unit="$unit" bash -c "$CURL_LOOP" _ "/root/$unit.log" "$u" -k --resolve "$res"
    ok "vòng lặp curl $n đã chạy" rc_is 0
    LOOPS+=("$n:/root/$unit.log:$want")
  done
  sleep 3
}
loops_stop() {
  local l units=()
  sleep 3
  for l in "${LOOPS[@]}"; do units+=("tpan-e2e-curl-$LOOP_TAG-${l%%:*}"); done
  vroot systemctl stop "${units[@]}"
  vroot python3 -c "$LOOP_PY" "$LOOP_TAG" "${LOOPS[@]}"
  ok "mọi vòng lặp: 10 request cuối đều đạt" hasnt '^ĐUÔI .* lỗi'
  grep '^ĐO ' <<<"$OUT" | sed 's/^ĐO //' >>"$DO_FILE"
}

do_block() {
  section "ĐO GIÁN ĐOẠN (tới lúc này)"
  printf 'Quy ước: đạt = 301 cho http của site SSL, 200 cho phần còn lại; mốc 0 = step.start của bước web.swap trong job; mỗi request một kết nối mới, nghỉ 20 ms.\n'
  cat "$DO_FILE"
}

# web_state <web mới>: các khẳng định chung sau khi chuyển xong sang <web>.
# $1: nginx | apache; $2: 1 khi có site SSL (mặc định), 0 khi không.
web_state() {
  local web=$1 ssl=${2:-1} unit other ounit dir odir
  if [ "$web" = apache ]; then
    unit=apache2 other=nginx ounit=nginx dir=/etc/apache2 odir=/etc/nginx
  else
    unit=nginx other=apache ounit=apache2 dir=/etc/nginx odir=/etc/apache2
  fi
  section "sau khi chuyển sang $web: dịch vụ, policy-rc.d"
  vexec bash -c 'for u in "$@"; do echo "$u $(systemctl is-active $u) $(systemctl is-enabled $u 2>&1)"; done; ls -la /usr/sbin/policy-rc.d 2>&1' _ "$unit" "$ounit"
  ok "$unit active+enabled" grep -qx "$unit active enabled" <<<"$OUT"
  ok "$ounit inactive+disabled" grep -qx "$ounit inactive disabled" <<<"$OUT"
  ok "/usr/sbin/policy-rc.d không còn" hasF 'cannot access'
  vroot ss -tlnp
  ok "cổng 80 do $unit giữ, không có $ounit" bash -c 'grep -E ":80 " <<<"$1" | grep -q "\"$2\"" && ! grep -q "\"$3\"" <<<"$1"' _ "$OUT" "$unit" "$ounit"

  section "sau khi chuyển sang $web: cổng 443 với tên lạ"
  vexec bash -c 'curl -sS -k -m 10 --resolve la.example.net:443:127.0.0.1 -o /dev/null -w "HTTP %{http_code}\n" https://la.example.net/; echo "curl mã $?"'
  if [ "$ssl" = 0 ]; then
    ok "không site nào có SSL: cổng 443 đóng (curl mã 7)" hasF 'curl mã 7'
  elif [ "$web" = apache ]; then
    ok "443 với tên lạ: site mặc định Apache trả 403" hasF 'HTTP 403'
  else
    ok "443 với tên lạ: nginx từ chối bắt tay (curl mã 35)" hasF 'curl mã 35'
  fi

  section "sau khi chuyển sang $web: logrotate"
  vexec cat /etc/logrotate.d/tpan-sites
  if [ "$web" = apache ]; then
    ok "logrotate là bản apache (invoke-rc.d apache2 reload, không /run/nginx.pid)" bash -c 'grep -q "invoke-rc.d apache2 reload" <<<"$1" && ! grep -q "/run/nginx.pid" <<<"$1"' _ "$OUT"
  else
    ok "logrotate là bản nginx (/run/nginx.pid, không apache2)" bash -c 'grep -q "/run/nginx.pid" <<<"$1" && ! grep -q "apache2 reload" <<<"$1"' _ "$OUT"
  fi
  vroot logrotate -d /etc/logrotate.d/tpan-sites
  ok "logrotate -d thoát 0, không dòng error" bash -c '[ "$2" = 0 ] && ! grep -qi "^error" <<<"$1"' _ "$OUT" "$RC"

  section "sau khi chuyển sang $web: file tpan của $other không còn"
  vroot find "$odir" -name '*tpan*'
  ok "không file tpan nào trong $odir" bash -c '[ "$2" = 0 ] && [ -z "$1" ]' _ "$OUT" "$RC"
  vroot find "$dir" -name '*tpan*'

  section "sau khi chuyển sang $web: managed_files so với đĩa"
  vroot python3 -c "$MF_PY" "$dir" "$odir"
  ok "mọi dòng managed_files khớp sha256 trên đĩa" hasF 'lệch hoặc thiếu file: 0'
  ok "không dòng nào còn trỏ vào $odir" hasF "dòng còn trỏ vào $odir: 0"
  ok "mọi file tpan của $web trên đĩa (và logrotate) đều có dòng" has '; không có dòng: 0$'

  section "sau khi chuyển sang $web: health, health --fix --dry-run, stack status"
  health_check
  ok "health: máy, stack và mọi site đạt hết (kể cả drift)" health_pass
  ok "health mã thoát 0" rc_is 0
  vroot tpan health --fix --dry-run
  ok "health --fix --dry-run: không có gì để sửa" hasF 'Không có phép kiểm nào sửa tự động được.'
  vroot tpan stack status --json
  ok "stack status: web = $web" bash -c 'jq -e --arg w "$2" ".config.web == \$w" <<<"$1" >/dev/null' _ "$OUT" "$web"
  vroot tpan stack status
}

# switch_apply <to> <tag> <mục tiêu...>: switch --yes --verbose trong lúc vòng
# lặp curl chạy. RC và OUT là của lệnh switch (SW_OUT giữ lại).
SW_OUT=""
SW_RC=0
switch_apply() {
  local to=$1 tag=$2
  shift 2
  loops_start "$tag" "$@"
  vroot tpan stack switch --to "$to" --yes --verbose
  SW_OUT=$OUT SW_RC=$RC
  loops_stop
  OUT=$SW_OUT RC=$SW_RC
}

# dry_run_unchanged <to>: ảnh chụp trước, --dry-run, ảnh chụp sau, so.
dry_run_unchanged() {
  local to=$1 before
  quiet "$VM" root bash -c "$SNAP_SCRIPT" _ "$NJOBS_PY"
  before=$OUT
  vroot tpan stack switch --to "$to" --dry-run
  ok "dry-run thoát 0" rc_is 0
  ok "xem trước có bước cài không cho chạy, đổi web server, kiểm từng site" bash -c 'grep -qF "(apt.install-nostart:" <<<"$1" && grep -qF "(web.swap:" <<<"$1" && grep -qF "(stack.switch.check:" <<<"$1"' _ "$OUT"
  printf '\nBước "chưa kiểm được lúc xem trước" (%s bước):\n' "$(grep -c '(chưa kiểm được lúc xem trước:' <<<"$OUT")"
  grep -B12 '(chưa kiểm được lúc xem trước:' <<<"$OUT" | grep -E '^ ?[0-9]+\. |chưa kiểm được' || true
  quiet "$VM" root bash -c "$SNAP_SCRIPT" _ "$NJOBS_PY"
  ok "dry-run không đổi gì (dịch vụ, gói, policy-rc.d, cây /etc/nginx /etc/apache2 /etc/logrotate.d, số job)" test "$OUT" = "$before"
  if [ "$OUT" != "$before" ]; then
    diff <(printf '%s\n' "$before") <(printf '%s\n' "$OUT") | head -40
  fi
  printf 'Trạng thái lúc xem trước:\n'
  grep -E '^(nginx|apache2) |policy-rc' <<<"$before"
}

# ================================================================ A

part_a() {
  part_begin "A: máy sạch, nginx, 4 site (SSL, PHP 8.4, permalink, thùng rác)"
  reset_vm
  deploy
  vroot tpan stack install --web nginx --yes
  ok "stack install thoát 0, job thành công" job_ok
  vexec systemctl is-active nginx php8.3-fpm mariadb
  ok "nginx, php8.3-fpm, mariadb active" bash -c '[ "$(grep -cx active <<<"$1")" -eq 3 ]' _ "$OUT"
  vexec dpkg-query -W -f '${Package} ${db:Status-Status}\n' apache2
  ok "máy chưa có apache2" hasnt 'apache2 installed'

  section "A đồ nghề: máy chủ ACME thử pebble trong máy ảo (KHÔNG phải cách triển khai)"
  vroot bash -c "$PEBBLE_SCRIPT" _ "$PEBBLE_DIR"
  ok "dựng pebble thoát 0" rc_is 0
  vexec curl -sS https://localhost:14000/dir
  ok "API của pebble trả lời, có newOrder" has '"newOrder"'

  section "A: 4 site"
  local d
  for d in "$S1" "$S2" "$S3" "$S4"; do
    site_add "$d"
    ok "site add $d: thoát 0, job thành công" job_ok
    ok "mật khẩu quản trị in đúng một lần" test "$PW_HITS" -eq 1
  done
  vroot bash -c 'printf "127.0.0.1 %s\n" "$@" >>/etc/hosts && grep -n example /etc/hosts' _ "$S1" "$S2" "$S3" "$S4"
  ok "(đồ nghề) /etc/hosts: 4 site -> 127.0.0.1" rc_is 0

  section "A: SSL cho $S1 (pebble)"
  vroot tpan ssl settings --email "$EMAIL" --server "$ACME"
  ok "ssl settings thoát 0" rc_is 0
  vroot tpan ssl issue "$S1" --yes
  ok "ssl issue $S1: thoát 0, job thành công" job_ok

  section "A: PHP 8.4 cho $S3"
  vroot tpan php install 8.4 --yes
  ok "php install 8.4: thoát 0, job thành công" job_ok
  vroot tpan php use "$S3" 8.4 --yes
  ok "php use $S3 8.4: thoát 0, job thành công" job_ok
  vroot tpan site info "$S3" --json
  ok "site info $S3: php_version 8.4, socket -8.4" bash -c 'jq -e --arg s "/run/php/tpan-$2-8.4.sock" ".site.php_version == \"8.4\" and .socket == \$s" <<<"$1" >/dev/null' _ "$OUT" "$(slug_of "$S3")"

  section "A: bài viết và permalink /%postname%/ cho $S2 và $S4"
  for d in "$S2" "$S4"; do
    vroot bash -c 'cd / && u=$2 p=/var/www/$1/public; runuser -u "$u" -- wp rewrite structure "/%postname%/" --path="$p" 2>&1 | grep -v "HTTP_HOST" | tail -2; runuser -u "$u" -- wp post create --post_status=publish --post_title="Bai e2e M10" --post_name="$3" --porcelain --path="$p" 2>/dev/null; runuser -u "$u" -- wp option get permalink_structure --path="$p" 2>/dev/null; ls -la "$p/.htaccess" 2>&1' _ "$d" "$(user_of "$d")" "$POST"
    ok "$d: permalink /%postname%/, có bài viết, CHƯA có .htaccess (site dựng trên nginx)" bash -c 'grep -qx "/%postname%/" <<<"$1" && grep -qE "^[0-9]+$" <<<"$1" && grep -q "No such file" <<<"$1"' _ "$OUT"
  done
  sites_ok "$T_A_HTTP" "$T_A_HTTPS" "$T_B" "$T_C" "$T_D" "$T_PL" "d-permalink|http://$S4/$POST/|$S4:80:127.0.0.1|200"

  section "A: $S4 vào thùng rác"
  vroot tpan site rm "$S4" --yes
  ok "site rm $S4: thoát 0, job thành công" job_ok
  vexec bash -c 'curl -sS -m 10 --resolve "$1:80:127.0.0.1" -o /dev/null -w "HTTP %{http_code}\n" "http://$1/"; echo "curl mã $?"' _ "$S4"
  ok "$S4 không còn trả 200" hasnt 'HTTP 200'
  vroot tpan site list
  health_check
  ok "health: máy, stack và mọi site đạt hết" health_pass
  vroot python3 -c "$JOBS_PY"
  part_end A
}

# ================================================================ B

part_b() {
  part_begin "B: bước 8, nginx -> apache"
  [ -n "$VM_IP" ] || addrs
  section "B: --dry-run trên máy chưa có apache2 (mục 16): máy không đổi gì"
  dry_run_unchanged apache

  section "B (mục 16): apachectl chưa có thì sao; nginx -t lúc này"
  vexec bash -c 'command -v apachectl a2query a2enmod; echo "mã $?"'

  section "B: tpan stack switch --to apache --yes trong lúc curl liên tục"
  switch_apply apache B "$T_A_HTTP" "$T_A_HTTPS" "$T_B" "$T_C"
  ok "switch thoát 0, job thành công" job_ok
  ok "health sau job: stack và mọi site vẫn đạt" hasF 'Stack và mọi site vẫn đạt.'
  vroot python3 -c "$JOB_PY" stack.switch
  ok "bước web.swap xong, không bước nào lỗi" bash -c 'grep -q "step.done web.swap:nginx:apache2" <<<"$1" && ! grep -q "step.fail" <<<"$1"' _ "$OUT"

  section "B (mục 16): policy-rc.d có chặn postinst của apache2 không, disable --now"
  vroot python3 -c "$JOBOUT_PY" 'policy-rc|invoke-rc|denied|Created symlink|Removed|disable --now|apache2.service'
  ok "output apt có 'policy-rc.d denied execution of start'" hasF 'policy-rc.d denied execution of'
  ok "disable --now gỡ link bật-khi-khởi-động của apache2 do postinst tạo" has 'Removed .*multi-user\.target\.wants/apache2\.service'
  vroot python3 -c "$JSTART_PY" apache2.service
  ok "apache2 không chạy lần nào trước bước web.swap" hasF 'Started trước web.swap: 0'
  ok "apache2 Started đúng một lần (ở web.swap)" hasF 'số lần Started apache2.service trong journal: 1'

  section "B: mọi site trên Apache"
  sites_ok "$T_A_HTTP" "$T_A_HTTPS" "$T_B" "$T_C" "$T_PL"
  vroot bash -c 'ls -la /var/www/$1/public/.htaccess /var/www/$2/public/.htaccess /var/www/$3/public/.htaccess 2>&1' _ "$S1" "$S2" "$S3"
  ok ".htaccess đã có cho 3 site active" bash -c '[ "$(grep -c "^-rw" <<<"$1")" -eq 3 ]' _ "$OUT"
  vexec grep -n 'sock' "/etc/apache2/sites-available/tpan-$S3.conf" "/etc/apache2/sites-available/tpan-$S2.conf"
  ok "vhost Apache của $S3 trỏ socket -8.4.sock, $S2 -8.3.sock" bash -c 'grep -qF "tpan-$4.conf:" <<<"$1" && grep "tpan-$4.conf:" <<<"$1" | grep -q "tpan-$2-8\.4\.sock" && grep "tpan-$5.conf:" <<<"$1" | grep -q "tpan-$3-8\.3\.sock"' _ "$OUT" "$(slug_of "$S3")" "$(slug_of "$S2")" "$S3" "$S2"
  web_state apache

  section "B (mục 16): nginx -t khi nginx tắt và Apache giữ cổng"
  vroot nginx -t
  ok "nginx -t thoát 0 khi nginx tắt" rc_is 0
  vroot python3 -c "$JOBOUT_PY" '^apachectl configtest|^nginx -t'

  section "B: khôi phục $S4 từ thùng rác trên Apache"
  vroot tpan site restore "$S4" --yes
  ok "site restore $S4: thoát 0, job thành công" job_ok
  sites_ok "$T_D" "d-permalink|http://$S4/$POST/|$S4:80:127.0.0.1|200"
  vexec ls -la "/var/www/$S4/public/.htaccess" "/etc/apache2/sites-enabled/tpan-$S4.conf"
  health_check
  ok "health: mọi thứ đạt sau khi khôi phục" health_pass

  section "B: sửa tay vhost Apache của $S2 -> health báo lệch đúng file đó"
  local vh="/etc/apache2/sites-available/tpan-$S2.conf" sha0
  vroot sha256sum "$vh"
  sha0=$OUT
  vroot bash -c 'printf "# sua tay e2e M10\n" >>"$1"' _ "$vh"
  health_check
  ok "drift:$vh Fail" health_has "drift:$vh" fail
  ok "chỉ phép kiểm lệch của đúng file đó Fail" test "$(health_fails)" = "drift:$vh"
  vroot tpan health --fix --dry-run
  vroot tpan health --fix --yes
  ok "health --fix --yes thoát 0" rc_is 0
  vroot sha256sum "$vh"
  ok "vhost về đúng bản tpan đã ghi (sha256 như trước khi sửa tay)" test "$OUT" = "$sha0"
  health_check
  ok "health đạt hết sau khi sửa" health_pass

  section "B (mục 16): gói nginx của Ubuntu cài lại dưới policy-rc.d khi nginx tắt (đồ nghề)"
  vroot bash -c "$POLICY_PROBE" _ nginx
  ok "apt-get thoát 0" hasF 'apt-get mã 0'
  ok "policy-rc.d chặn khởi động nginx khi cài lại" bash -c 'grep -q "^sau khi cài lại: inactive" <<<"$1"' _ "$OUT"
  ok "sau disable --now: inactive disabled" hasF 'sau disable --now: inactive disabled'
  sites_ok "$T_A_HTTPS" "$T_B"
  vroot find /etc/nginx/sites-enabled
  nopolicy_probe nginx apache2
  vroot python3 -c "$JOBS_PY"
  do_block
  part_end B
}

# nopolicy_probe <unit đã tắt> <unit đang chạy>: NOPOLICY_PROBE và khẳng định.
nopolicy_probe() {
  section "(thêm) nâng gói của $1 đã tắt, KHÔNG có policy-rc.d (như unattended-upgrades sau khi chuyển)"
  vroot bash -c "$NOPOLICY_PROBE" _ "$1" "$2"
  ok "apt-get thoát 0" hasF 'apt-get mã 0'
  ok "$1 vẫn inactive+disabled, $2 vẫn active sau khi gói được cài lại" hasF "sau khi cài lại (không policy-rc.d): $1 inactive disabled; $2 active"
  sites_ok "$T_A_HTTPS" "$T_B"
}

# ================================================================ C

part_c() {
  part_begin "C: apache -> nginx"
  [ -n "$VM_IP" ] || addrs
  section "C: --dry-run: máy không đổi gì"
  dry_run_unchanged nginx

  section "C: tpan stack switch --to nginx --yes trong lúc curl liên tục"
  switch_apply nginx C "$T_A_HTTP" "$T_A_HTTPS" "$T_B" "$T_C" "$T_D"
  ok "switch thoát 0, job thành công" job_ok
  vroot python3 -c "$JOB_PY" stack.switch
  ok "bước web.swap xong, không bước nào lỗi" bash -c 'grep -q "step.done web.swap:apache2:nginx" <<<"$1" && ! grep -q "step.fail" <<<"$1"' _ "$OUT"
  ok "bước cài nginx được bỏ qua (gói có sẵn, tắt sẵn)" has 'step.skip apt.install-nostart:nginx'
  vroot python3 -c "$JSTART_PY" nginx.service
  ok "nginx không chạy lại trước bước web.swap (kể từ job này)" bash -c 'grep "Started lúc" <<<"$1" | tail -1 | grep -q "lúc +"' _ "$OUT"

  section "C: mọi site trên nginx"
  sites_ok "$T_A_HTTP" "$T_A_HTTPS" "$T_B" "$T_C" "$T_D" "$T_PL" "d-permalink|http://$S4/$POST/|$S4:80:127.0.0.1|200"
  vexec grep -n 'fastcgi_pass' "/etc/nginx/sites-available/tpan-$S3.conf"
  ok "vhost nginx của $S3 trỏ socket -8.4.sock" has "tpan-$(slug_of "$S3")-8\.4\.sock"
  web_state nginx

  section "C (mục 16): apachectl configtest khi apache2 tắt và nginx giữ cổng"
  vroot apachectl configtest
  ok "apachectl configtest thoát 0 khi apache2 tắt" rc_is 0
  nopolicy_probe apache2 nginx

  section "C: chạy lại switch sang nginx: từ chối stack_same_web"
  local n0
  n0="$(njobs)"
  vroot tpan stack switch --to nginx --yes --json
  ok "thoát khác 0, mã stack_same_web" bash -c '[ "$2" != 0 ] && jq -e ".error.code == \"stack_same_web\"" <<<"$1" >/dev/null' _ "$OUT" "$RC"
  ok "không tạo job ($n0)" test "$(njobs)" = "$n0"

  section "C: stack install --web nginx chạy lại: không đổi gì"
  quiet "$VM" root bash -c "$SNAP_SCRIPT" _ 'print()'
  local snap=$OUT
  vroot tpan stack install --web nginx --yes
  ok "thoát 0, job thành công" job_ok
  ok "không bước nào chạy (không có dòng 'xong')" hasnt '^  xong$'
  quiet "$VM" root bash -c "$SNAP_SCRIPT" _ 'print()'
  ok "dịch vụ và cây cấu hình không đổi" test "$OUT" = "$snap"
  vroot python3 -c "$JOBS_PY"
  do_block
  part_end C
}

# ================================================================ D

part_d() {
  part_begin "D: điều kiện đầu và hoàn tác khi lỗi (đang chạy nginx)"
  [ -n "$VM_IP" ] || addrs
  local n0 tree0

  section "D0: một site hỏng sẵn trên nginx (kịch bản lỗi 02 trên $S2): từ chối precondition_failed"
  run scripts/faults/02-pool-socket-rm.sh break "$S2"
  ok "break thoát 0" rc_is 0
  n0="$(njobs)"
  vroot tpan stack switch --to apache --yes --json
  ok "thoát khác 0, precondition_failed, phép kiểm của $S2" bash -c '[ "$2" != 0 ] && jq -e --arg d "$3" ".error.code == \"precondition_failed\" and ([.error.checks[].id] | any(endswith(\$d)))" <<<"$1" >/dev/null' _ "$OUT" "$RC" "$S2"
  vroot tpan stack switch --to apache --yes
  ok "bản chữ: thoát khác 0" rc_not 0
  ok "không tạo job ($n0)" test "$(njobs)" = "$n0"
  vroot tpan health "$S2" --fix --yes
  ok "health --fix thoát 0" rc_is 0
  sites_ok "$T_B"

  section "D1: lỗi sớm: file cấu hình hỏng trong /etc/apache2/conf-enabled"
  quiet "$VM" root bash -c "$TREE_SCRIPT" _ /etc/nginx
  tree0=$OUT
  vroot bash -c 'ls -d /etc/apache2/conf-enabled && printf "DongSaiCuPhapE2E on\n" >/etc/apache2/conf-enabled/zz-hong.conf && apachectl configtest; echo "configtest mã $?"'
  ok "(đồ nghề) zz-hong.conf làm configtest lỗi" bash -c 'grep -q "configtest mã [1-9]" <<<"$1"' _ "$OUT"
  vroot tpan stack switch --to apache --yes --verbose
  ok "switch thoát khác 0, job hỏng, đã hoàn tác sạch" job_rolled_back
  vroot python3 -c "$JOB_PY" stack.switch
  ok "job rolled_back" has '^job stack.switch .* rolled_back$'
  ok "lỗi trước bước web.swap (không có step.start của web.swap)" hasnt 'step.start web.swap'
  ok "có bước lỗi vì configtest" bash -c 'grep "step.fail" <<<"$1" | grep -qi "apachectl\|configtest\|DongSai"' _ "$OUT"
  sites_ok "$T_A_HTTP" "$T_A_HTTPS" "$T_B" "$T_C" "$T_D"
  vexec bash -c 'for u in nginx apache2; do echo "$u $(systemctl is-active $u) $(systemctl is-enabled $u 2>&1)"; done; ls -la /usr/sbin/policy-rc.d 2>&1'
  ok "nginx active+enabled, apache2 inactive+disabled, không policy-rc.d" bash -c 'grep -qx "nginx active enabled" <<<"$1" && grep -qx "apache2 inactive disabled" <<<"$1" && grep -q "cannot access" <<<"$1"' _ "$OUT"
  quiet "$VM" root bash -c "$TREE_SCRIPT" _ /etc/nginx
  ok "cây /etc/nginx giống hệt trước" test "$OUT" = "$tree0"
  vroot find /etc/apache2 -name '*tpan*'
  ok "không file tpan nào sót trong /etc/apache2" bash -c '[ -z "$1" ]' _ "$OUT"
  vroot tpan stack status --json
  ok "stack status vẫn nginx" bash -c 'jq -e ".config.web == \"nginx\"" <<<"$1" >/dev/null' _ "$OUT"
  vroot rm -f /etc/apache2/conf-enabled/zz-hong.conf
  health_check
  ok "health đạt hết sau lần hoàn tác" health_pass

  section "D2: lỗi sau khi đổi: .htaccess của $S2 có chỉ thị sai (chỉ hỏng trên Apache)"
  local ht="/var/www/$S2/public/.htaccess"
  vroot bash -c 'cp -p "$1" /root/htaccess-b.bak && printf "DongSaiCuPhapE2E on\n" >>"$1" && tail -2 "$1"' _ "$ht"
  ok "(đồ nghề) đã thêm dòng sai vào .htaccess" rc_is 0
  sites_ok "$T_B"
  quiet "$VM" root bash -c "$TREE_SCRIPT" _ /etc/nginx
  tree0=$OUT
  switch_apply apache D2 "$T_A_HTTP" "$T_A_HTTPS" "$T_B" "$T_C" "$T_D"
  ok "switch thoát khác 0, job hỏng, đã hoàn tác sạch" job_rolled_back
  vroot python3 -c "$JOB_PY" stack.switch
  ok "job rolled_back" has '^job stack.switch .* rolled_back$'
  ok "bước lỗi là stack.switch.check:$S2" has "step.fail stack.switch.check:$S2"
  ok "web.swap được hoàn tác" has 'undo.done web.swap:nginx:apache2'
  sites_ok "$T_A_HTTP" "$T_A_HTTPS" "$T_B" "$T_C" "$T_D"
  vexec bash -c 'for u in nginx apache2; do echo "$u $(systemctl is-active $u) $(systemctl is-enabled $u 2>&1)"; done; ls -la /usr/sbin/policy-rc.d 2>&1'
  ok "nginx active+enabled, apache2 inactive+disabled, không policy-rc.d" bash -c 'grep -qx "nginx active enabled" <<<"$1" && grep -qx "apache2 inactive disabled" <<<"$1" && grep -q "cannot access" <<<"$1"' _ "$OUT"
  quiet "$VM" root bash -c "$TREE_SCRIPT" _ /etc/nginx
  ok "cây /etc/nginx giống hệt trước" test "$OUT" = "$tree0"
  vroot find /etc/apache2 -name '*tpan*'
  ok "không file tpan nào sót trong /etc/apache2" bash -c '[ -z "$1" ]' _ "$OUT"
  vroot bash -c 'cp -p /root/htaccess-b.bak "$1" && tail -1 "$1"' _ "$ht"
  vroot tpan stack status --json
  ok "stack status vẫn nginx" bash -c 'jq -e ".config.web == \"nginx\"" <<<"$1" >/dev/null' _ "$OUT"
  health_check
  ok "health đạt hết sau lần hoàn tác" health_pass
  vroot python3 -c "$JOBS_PY"
  do_block
  part_end D
}

# ================================================================ E

part_e() {
  local unit ounit web
  [ -n "$VM_IP" ] || addrs
  web="$("$VM" root tpan stack status --json </dev/null | jq -r .config.web)"
  [ "$web" = apache ] && unit=apache2 ounit=nginx || unit=nginx ounit=apache2
  part_begin "E: khởi động lại máy ảo (đang chạy $web)"
  run multipass restart "$VMNAME"
  ok "multipass restart thoát 0" rc_is 0
  local _
  for _ in $(seq 1 40); do
    "$VM" exec true </dev/null >/dev/null 2>&1 && break
    sleep 3
  done
  addrs
  vexec bash -c 'uptime -s; for u in "$@"; do echo "$u $(systemctl is-active $u) $(systemctl is-enabled $u 2>&1)"; done' _ "$unit" "$ounit"
  ok "$unit active+enabled sau khởi động lại" grep -qx "$unit active enabled" <<<"$OUT"
  ok "$ounit inactive+disabled sau khởi động lại" grep -qx "$ounit inactive disabled" <<<"$OUT"
  vroot ss -tlnp
  ok "không có $ounit nghe cổng nào" hasnt "\"$ounit\""
  sites_ok "$T_A_HTTP" "$T_A_HTTPS" "$T_B" "$T_C" "$T_D" "$T_PL"
  health_check
  ok "health đạt hết sau khởi động lại" health_pass
  part_end E
}

# ================================================================ F

part_f() {
  part_begin "F: máy sạch, nginx từ nginx.org, sang apache rồi về"
  reset_vm
  deploy
  vroot tpan stack install --web nginx --web-source nginx.org --yes
  ok "stack install thoát 0, job thành công" job_ok
  site_add "$S1"
  ok "site add $S1: thoát 0, job thành công" job_ok
  vroot bash -c 'printf "127.0.0.1 %s\n" "$1" >>/etc/hosts' _ "$S1"
  local t1="a-http|http://$S1/|$S1:80:127.0.0.1|200"
  sites_ok "$t1"
  vexec bash -c 'dpkg-query -W nginx; ps -eo user,cmd | grep "[n]ginx: worker"; id -nG nginx'

  section "F: sang apache"
  switch_apply apache F1 "$t1"
  ok "switch thoát 0, job thành công" job_ok
  sites_ok "$t1"
  vroot tpan stack status --json
  ok "stack status: apache, web_source ubuntu" bash -c 'jq -e ".config.web == \"apache\" and .config.web_source == \"ubuntu\"" <<<"$1" >/dev/null' _ "$OUT"
  vroot find /etc/nginx -name '*tpan*'
  ok "không file tpan nào trong /etc/nginx (kể cả conf.d/tpan.conf)" bash -c '[ -z "$1" ]' _ "$OUT"
  vexec ls /etc/apt/sources.list.d/

  section "F (mục 16): gói nginx của nginx.org cài lại dưới policy-rc.d khi nginx tắt (đồ nghề)"
  vroot bash -c "$POLICY_PROBE" _ nginx
  ok "apt-get thoát 0" hasF 'apt-get mã 0'
  ok "gói là của nginx.org (~noble)" has 'gói chứa nginx.service: nginx .*~noble'
  ok "policy-rc.d chặn khởi động nginx (nginx.org) khi cài lại" bash -c 'grep -q "^sau khi cài lại: inactive" <<<"$1"' _ "$OUT"
  ok "sau disable --now: inactive disabled" hasF 'sau disable --now: inactive disabled'
  sites_ok "$t1"

  section "F: về nginx"
  switch_apply nginx F2 "$t1"
  ok "switch thoát 0, job thành công" job_ok
  vroot tpan stack status --json
  ok "stack status: nginx, web_source nginx.org" bash -c 'jq -e ".config.web == \"nginx\" and .config.web_source == \"nginx.org\"" <<<"$1" >/dev/null' _ "$OUT"
  vexec bash -c 'dpkg-query -W nginx; ps -eo user,cmd | grep "[n]ginx: worker"; id -nG nginx; ls /etc/nginx/conf.d'
  ok "worker nginx chạy dưới user nginx" has '^nginx +nginx: worker'
  ok "id -nG nginx có www-data" has '^nginx( .*)? www-data'
  ok "conf.d có tpan.conf, không default.conf" bash -c 'grep -qx "tpan.conf" <<<"$1" && ! grep -qx "default.conf" <<<"$1"' _ "$OUT"
  sites_ok "$t1"
  web_state nginx 0
  vroot python3 -c "$JOBS_PY"
  do_block
  part_end F
}

# ================================================================ G

part_g() {
  part_begin "G: máy sạch, LAMP trước, chuyển sang nginx của Ubuntu"
  reset_vm
  deploy
  vroot tpan stack install --web apache --yes
  ok "stack install --web apache thoát 0, job thành công" job_ok
  site_add "$S1"
  ok "site add $S1: thoát 0, job thành công" job_ok
  vroot bash -c 'printf "127.0.0.1 %s\n" "$1" >>/etc/hosts' _ "$S1"
  local t1="a-http|http://$S1/|$S1:80:127.0.0.1|200"
  sites_ok "$t1"
  vexec dpkg-query -W -f '${Package} ${db:Status-Status}\n' nginx
  ok "máy chưa có nginx" hasnt 'nginx installed'

  section "G: --dry-run trên máy chưa có nginx: máy không đổi gì"
  dry_run_unchanged nginx

  section "G: sang nginx"
  switch_apply nginx G "$t1"
  ok "switch thoát 0, job thành công" job_ok
  vroot python3 -c "$JOBOUT_PY" 'policy-rc|invoke-rc|denied|Created symlink|Removed|disable --now'
  ok "postinst của nginx bị policy-rc.d chặn khởi động (dòng 'returned 101, not running start nginx.service')" hasF "policy-rc.d returned 101, not running 'start nginx.service'"
  ok "disable --now gỡ link bật-khi-khởi-động của nginx" has 'Removed .*multi-user\.target\.wants/nginx\.service'
  vroot python3 -c "$JSTART_PY" nginx.service
  ok "nginx không chạy lần nào trước bước web.swap" hasF 'Started trước web.swap: 0'
  sites_ok "$t1"
  web_state nginx 0
  vroot python3 -c "$JOBS_PY"
  do_block
  part_end G
}

case "${1:-all}" in
  all)
    part_a
    part_b
    part_e
    part_c
    part_d
    part_e
    part_f
    part_g
    ;;
  a) part_a ;;
  b) part_b ;;
  c) part_c ;;
  d) part_d ;;
  e) part_e ;;
  f) part_f ;;
  g) part_g ;;
  *)
    sed -n '2,12p' "$0" | sed 's/^# \{0,1\}//'
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
  ok "log không chứa mạng Multipass" bash -c '[ -z "$1" ] || ! grep -qF -- "$1" "$2"' _ "$NET4" "$E2E_LOG"
  ok "log không chứa IPv6 của máy ảo" bash -c '[ -z "$1" ] || ! grep -qF -- "$1" "$2"' _ "$V6PFX" "$E2E_LOG"
  ok "log không chứa IP của Mac" bash -c '[ -z "$1" ] || ! grep -qF -- "$1" "$2"' _ "$HOST_IP" "$E2E_LOG"
  ok "log không chứa thư mục nhà hay tên user Mac" bash -c '! grep -qF -- "$1" "$3" && ! grep -qF -- "$2" "$3"' _ "$HOME" "$USER" "$E2E_LOG"
fi

do_block
section "TỔNG KẾT: $PASSES khẳng định đạt, $FAILS khẳng định không đạt"
[ "$FAILS" -eq 0 ]
