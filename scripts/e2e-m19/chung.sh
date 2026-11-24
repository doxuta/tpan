# shellcheck shell=bash
# shellcheck disable=SC2034 # biến dùng ở các script nạp file này
# Phần dùng chung của các script e2e M19 chạy TRONG máy ảo bằng root. Mỗi script nạp:
#   . "$(dirname "$0")/chung.sh"
#
# Không in nội dung file mật khẩu hay cookie: mật khẩu nằm ở /root/m19-*.pw (0600),
# cookie ở $W/jar-<tên> (0600); `tpan site add` luôn chạy với --json vào file.
# Máy ảo không có jq: đọc JSON bằng python3 -I (hàm pj và pv).
set -u
umask 077
D="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
W=/root/m19-e2e
mkdir -p "$W"
chmod 700 "$W"
API=https://127.0.0.1:8443/api/v1
EMAIL=admin@example.com
ACME=https://localhost:14000/dir
PEBBLE_UNIT=tpan-e2e-pebble
PEBBLE_DIR=/etc/tpan-e2e-pebble
TPAN_DB=/var/lib/tpan/tpan.db

# ---- bảng tên (xem đầu scripts/e2e-m19.sh) ----
SITE=example.com                       # admin, SSL
WWW=www.example.com                    # tên phụ mặc định của $SITE
SUBSITE=blog.example.com               # admin
NET=example.net                        # tên phụ serve của $SITE (E10)
VN=xn--vitnam-jk8b.example.net         # tên phụ serve của $SITE (R02, R04)
VN_UTF8='việtnam.example.net'          # dạng UTF-8 của VN: ParseDomain đổi sang punycode
NODNS=nodns.example.invalid            # tên phụ không có DNS (E40)
UA=khach_a
A_SITE=www.example.org                 # site của khach_a
A_ALIAS=example.org                    # tên phụ mặc định của $A_SITE
UB=khach_b
B_SITE=shop.khach-b.test               # site của khach_b
RACE=www.e24.test                      # E24

HTTP=""
TIME=""
CRC=0
RC=0

ok() {
  local d=$1
  shift
  if "$@"; then
    echo "  => ĐẠT: $d"
  else
    echo "  => KHÔNG ĐẠT: $d"
    return 1
  fi
}
# bail: điều kiện đầu của cả nhóm hỏng; in một khẳng định không đạt rồi dừng.
bail() {
  echo "  => KHÔNG ĐẠT: DỪNG NHÓM: $*"
  exit 2
}
note() { echo "  -- GHI NHẬN: $*"; }
step() { printf '\n--- [%s] %s\n' "$(date '+%H:%M:%S')" "$*"; }
sha() { sha256sum "$1" | cut -c1-16; }

# WEB: stack đang chạy (apache | nginx).
WEB=nginx
systemctl is-active --quiet apache2 && WEB=apache
vhost_of() { # vhost_of <domain>: đường dẫn file vhost của stack đang chạy
  if [ "$WEB" = apache ]; then echo "/etc/apache2/sites-available/tpan-$1.conf"; else echo "/etc/nginx/sites-available/tpan-$1.conf"; fi
}
web_test() { # cú pháp config của stack đang chạy, in dòng cuối
  if [ "$WEB" = apache ]; then apachectl -t 2>&1; else nginx -t 2>&1; fi
}

# ---- JSON bằng python3 (không có jq) ----
# pj <file> <biểu thức Python theo biến d>: thoát 0 khi biểu thức đúng.
pj() {
  python3 -I -c 'import json, sys
d = json.load(open(sys.argv[1]))
sys.exit(0 if eval(sys.argv[2]) else 1)' "$1" "$2" 2>/dev/null
}
# pv <file> <biểu thức Python theo biến d>: in giá trị (rỗng khi lỗi).
pv() {
  python3 -I -c 'import json, sys
d = json.load(open(sys.argv[1]))
v = eval(sys.argv[2])
print("" if v is None else v)' "$1" "$2" 2>/dev/null
}
errcode() { pv "$1" 'd["error"]["code"]'; }

# no_leak <file câu trả lời> <chuỗi>...: không giá trị chuỗi nào của câu trả lời CHỨA CHUỖI
# làm nguyên một từ (từ = dãy [A-Za-z0-9_.-], bỏ dấu chấm cuối). Tên đang xin có thể nằm
# trong lời, nên không dò chuỗi con: chỉ cấm nguyên tên site hay tên user của người khác.
no_leak() {
  python3 -I -c 'import json, re, sys
d = json.load(open(sys.argv[1]))
bad = set(sys.argv[2:])
def walk(x):
    if isinstance(x, dict):
        for v in x.values(): yield from walk(v)
    elif isinstance(x, list):
        for v in x: yield from walk(v)
    elif isinstance(x, str):
        yield x
for s in walk(d):
    for t in re.findall(r"[A-Za-z0-9_.-]+", s):
        if t.rstrip(".") in bad:
            print("  lộ:", t); sys.exit(1)
sys.exit(0)' "$@"
}

# sqlq <câu> [tham số...]: đọc tpan.db (chỉ đọc), in một giá trị.
sqlq() {
  python3 -I -W ignore -c '
import sqlite3, sys
c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True)
r = c.execute(sys.argv[1], sys.argv[2:]).fetchone()
print(r[0] if r else "")' "$@"
}

# ---- phiên API ----
# login <tên> <file mật khẩu>: đăng nhập, cookie vào $W/jar-<tên>. Thoát 0 khi 204.
login() {
  local name=$1 pwf=$2
  python3 -I -c 'import json, sys; print(json.dumps({"name": sys.argv[1], "password": open(sys.argv[2]).read().strip()}))' \
    "$name" "$pwf" >"$W/login.json"
  : >"$W/jar-$name"
  HTTP="$(curl -sk -m 30 -c "$W/jar-$name" -o /dev/null -w '%{http_code}' -X POST "$API/login" \
    -H 'Content-Type: application/json' --data @"$W/login.json")"
  rm -f "$W/login.json"
  echo "  đăng nhập $name: HTTP $HTTP"
  [ "$HTTP" = 204 ]
}

# call <tên phiên> <METHOD> <đường dẫn sau /api/v1> <file câu trả lời> <hạn giây> [thân JSON]
# Đặt HTTP, TIME, CRC. In một dòng tóm tắt (không in thân).
call() {
  local who=$1 m=$2 p=$3 out=$4 lim=$5 body=${6-} res
  : >"$out"
  local args=(-sk -m "$lim" -b "$W/jar-$who" -o "$out" -w '%{http_code} %{time_total}' -X "$m" "$API$p")
  [ -n "$body" ] && args+=(-H 'Content-Type: application/json' --data "$body")
  res="$(curl "${args[@]}")"
  CRC=$?
  HTTP="${res%% *}"
  TIME="${res##* }"
  echo "  $who $m $p: HTTP ${HTTP:-000}, ${TIME:-?}s (curl thoát $CRC), $(wc -c <"$out") byte; lỗi: $(errcode "$out")"
}

# cho_job <id>: chờ job xong (tối đa 300 giây), in trạng thái.
cho_job() {
  local st="" _
  for _ in $(seq 1 300); do
    st="$(sqlq "SELECT status FROM jobs WHERE id = ?" "$1")"
    case "$st" in running | queued | "") sleep 1 ;; *) break ;; esac
  done
  echo "$st"
}
# cho_roi: chờ tới khi không còn job nào đang chạy (kể cả job SSL tự chạy tiếp), tối đa 300 giây.
cho_roi() {
  local n _
  for _ in $(seq 1 300); do
    n="$(sqlq "SELECT COUNT(*) FROM jobs WHERE status IN ('running','queued')")"
    [ "${n:-1}" = 0 ] && return 0
    sleep 1
  done
  return 1
}
# call_job <phiên> <METHOD> <đường dẫn> <thân>: gọi, nếu 202 thì chờ job. Đặt HTTP, JOBID, JOBST.
JOBID=""
JOBST=""
call_job() {
  call "$1" "$2" "$3" "$W/cj.out" 300 "${4-}"
  JOBID=""
  JOBST=""
  if [ "$HTTP" = 202 ]; then
    JOBID="$(pv "$W/cj.out" 'd["job_id"]')"
    JOBST="$(cho_job "$JOBID")"
    echo "  job $JOBID: $JOBST"
  fi
}

# ---- lệnh tpan ----
# tp <lệnh...>: chạy tpan, in tối đa 1500 byte output, đặt RC. Không dùng cho lệnh in mật khẩu.
tp() {
  tpan "$@" </dev/null >"$W/tp.out" 2>&1
  RC=$?
  echo "  \$ tpan $* (thoát $RC)"
  head -c 1500 "$W/tp.out" | sed 's/^/    | /'
  return 0
}
# site_add_cli <domain> [cờ...]: tpan site add (admin), --json vào file (mật khẩu WordPress không in).
site_add_cli() {
  local d=$1
  shift
  tpan site add "$d" --email "$EMAIL" --yes --json "$@" >"$W/site-$d.json" 2>"$W/site-$d.err" </dev/null
  RC=$?
  echo "  \$ tpan site add $d $* (thoát $RC)"
  tail -3 "$W/site-$d.err" | sed 's/^/    | /'
  return 0
}
# alias_list <domain>: tpan site alias list --json vào $W/names.json.
alias_list() {
  tpan site alias list "$1" --json >"$W/names.json" 2>"$W/names.err" </dev/null
  RC=$?
  echo "  \$ tpan site alias list $1 --json (thoát $RC)"
  python3 -I -c 'import json, sys
for n in json.load(open(sys.argv[1])):
    print("    |", n["name"], n["kind"], n["mode"], n["state"], "dns=" + str(n.get("dns")), "in_cert=" + str(n.get("in_cert")))' "$W/names.json" 2>/dev/null
  return 0
}
# ten_co <tên> <biểu thức theo n>: $W/names.json có dòng tên đó thỏa biểu thức.
ten_co() { pj "$W/names.json" "any(n['name'] == '$1' and ($2) for n in d)"; }

# ---- HTTP ----
HC=""
HLOC=""
HXRB=""
# hreq <Host> <đường dẫn> [cờ curl...]: GET http://127.0.0.1<đường dẫn> với Host đó. Đặt HC, HLOC, HXRB.
hreq() {
  local h=$1 p=$2
  shift 2
  : >"$W/h.hdr"
  : >"$W/h.body"
  HC="$(curl -s -m 30 -D "$W/h.hdr" -o "$W/h.body" -w '%{http_code}' -H "Host: $h" "$@" "http://127.0.0.1$p")"
  HLOC="$(tr -d '\r' <"$W/h.hdr" | awk 'tolower($1) == "location:" { sub(/^[^:]*: */, ""); print; exit }')"
  HXRB="$(tr -d '\r' <"$W/h.hdr" | awk 'tolower($1) == "x-redirect-by:" { sub(/^[^:]*: */, ""); print; exit }')"
  echo "  GET http://$h$p: HTTP ${HC:-000}, Location: ${HLOC:--}, X-Redirect-By: ${HXRB:--}"
}
# hsreq <tên> <đường dẫn>: GET https://<tên><đường dẫn> tới 127.0.0.1:443 (có SNI, -k vì CA là pebble).
hsreq() {
  : >"$W/h.hdr"
  : >"$W/h.body"
  HC="$(curl -sk -m 30 -D "$W/h.hdr" -o "$W/h.body" -w '%{http_code}' --resolve "$1:443:127.0.0.1" "https://$1$2")"
  CRC=$?
  HLOC="$(tr -d '\r' <"$W/h.hdr" | awk 'tolower($1) == "location:" { sub(/^[^:]*: */, ""); print; exit }')"
  echo "  GET https://$1$2: HTTP ${HC:-000} (curl thoát $CRC), Location: ${HLOC:--}"
}
is_wp() { grep -qiE 'wp-content|wordpress' "$W/h.body"; }

# ---- chứng chỉ ----
cert_of() { echo "/etc/letsencrypt/live/$1/cert.pem"; }
# san <domain>: các tên trong chứng chỉ, mỗi tên một dòng.
san() { openssl x509 -in "$(cert_of "$1")" -noout -ext subjectAltName 2>/dev/null | tr ',' '\n' | sed -n 's/^ *DNS://p'; }
serial_of() { openssl x509 -in "$(cert_of "$1")" -noout -serial 2>/dev/null; }

# ---- cài máy ----
# cai_may <apache|nginx>: tpan setup (bỏ qua nếu đã có), chờ apt, stack install.
cai_may() {
  local stack=$1 i rc
  step "tpan version"
  tpan version
  step "tpan setup"
  if [ ! -s /root/m19-admin.pw ] && [ "$(systemctl is-active tpan)" != active ]; then
    tpan setup --yes >/root/m19-setup.txt 2>&1
    rc=$?
    # Dòng mật khẩu là dòng duy nhất chỉ gồm chữ in và số dài từ 20 ký tự.
    grep -oE '[A-Z0-9]{20,}' /root/m19-setup.txt | head -1 >/root/m19-admin.pw
    grep -vE '[A-Z0-9]{20,}' /root/m19-setup.txt | tail -6
    ok "tpan setup thoát 0, có mật khẩu quản trị (giữ ở /root/m19-admin.pw, không in)" test "$rc" -eq 0 -a -s /root/m19-admin.pw
  else
    note "tpan đã setup (bỏ qua)"
  fi
  ok "tpan.service active" test "$(systemctl is-active tpan)" = active

  step "chờ apt và dpkg của máy rảnh (unattended-upgrades sau khi khởi động)"
  local locks="/var/lib/dpkg/lock-frontend /var/lib/dpkg/lock /var/lib/apt/lists/lock /var/cache/apt/archives/lock"
  for i in $(seq 1 240); do
    # shellcheck disable=SC2086 # locks cố ý tách thành nhiều đường dẫn
    if ! pgrep -x 'apt-get|apt|dpkg' >/dev/null && ! fuser $locks >/dev/null 2>&1; then
      break
    fi
    sleep 5
  done
  echo "  apt rảnh sau khoảng $(((i - 1) * 5)) giây"

  step "tpan stack install --web $stack --yes"
  tpan stack install --web "$stack" --yes >"$W/stack.txt" 2>&1
  rc=$?
  tail -4 "$W/stack.txt"
  ok "stack install --web $stack thoát 0" test "$rc" -eq 0
}

# dung_pebble: máy chủ ACME thử pebble (đồ nghề test, chỉ trong máy ảo) và cài đặt SSL của tpan.
dung_pebble() {
  step "pebble (đồ nghề test) và tpan ssl settings"
  bash "$D/pebble.sh" "$PEBBLE_DIR" "$PEBBLE_UNIT" 2>&1 | tail -6
  ok "pebble trả lời, có newOrder" bash -c 'curl -sS https://localhost:14000/dir | grep -q newOrder'
  tpan ssl settings --email "$EMAIL" --server "$ACME" </dev/null 2>&1 | tail -3
  ok "ssl settings thoát 0" test "${PIPESTATUS[0]}" -eq 0
}

# hosts_them <tên>...: /etc/hosts trỏ các tên về 127.0.0.1 (chưa có thì thêm).
hosts_them() {
  local n
  for n in "$@"; do
    grep -qE "^127\.0\.0\.1 $n\$" /etc/hosts || echo "127.0.0.1 $n" >>/etc/hosts
  done
  echo "  /etc/hosts: $* -> 127.0.0.1"
}

# user_them <tên> <file mật khẩu>: tpan user add (vai user), mật khẩu vào file, không in.
user_them() {
  [ -s "$2" ] && return 0
  tpan user add "$1" --yes --json 2>"$W/user.err" </dev/null |
    python3 -I -c 'import json, sys; print(json.load(sys.stdin)["password"])' >"$2"
  ok "user add $1 thoát 0, có mật khẩu (không in)" test "${PIPESTATUS[0]}" -eq 0 -a -s "$2"
}

# can_may: dừng nhóm khi máy chưa dựng.
can_may() {
  [ -s /root/m19-admin.pw ] || bail "chưa có /root/m19-admin.pw (chạy dung-may.sh trước)"
  [ -s /root/m19-khach_a.pw ] && [ -s /root/m19-khach_b.pw ] || bail "chưa có hai user thường (chạy dung-may.sh trước)"
}

# hs <file health --json> <tiền tố ID>: in trạng thái của các phép kiểm có ID bắt đầu bằng tiền tố.
hs() {
  python3 -I -c '
import json, sys
d = json.load(open(sys.argv[1]))
out = []
def walk(x):
    if isinstance(x, dict):
        if str(x.get("id", "")).startswith(sys.argv[2]):
            out.append(str(x.get("status")))
        for v in x.values(): walk(v)
    elif isinstance(x, list):
        for v in x: walk(v)
walk(d)
print(",".join(out))' "$1" "$2" 2>/dev/null
}
