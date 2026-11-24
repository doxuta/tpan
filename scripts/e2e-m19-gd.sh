#!/usr/bin/env bash
# Lệnh trong máy ảo viết trong nháy đơn, biến của chúng mở ra trong máy ảo:
# shellcheck disable=SC2016
# e2e GIAO DIỆN của mốc M19 (tên phụ, chuyển hướng, chứng chỉ phủ đủ các tên) trên
# máy ảo (tên trong file .tpan-vm; không có thì tpan-test). Chạy trên Mac, trong thư
# mục repo, từ máy sạch:
#
#   make e2e-m19-gd     (log vào tmp-phien/ rồi chép vào docs/bang-chung/M19-giao-dien.log,
#                        ảnh chụp bằng trình duyệt vào docs/bang-chung/M19/M19-that-*.png)
#   E2E_LOG=tmp-phien/e2e-m19-gd.log bash scripts/e2e-m19-gd.sh > tmp-phien/e2e-m19-gd.log 2>&1
#
# Gõ thêm tên phần để chỉ chạy vài phần, theo thứ tự (mặc định: a b c d e):
#   bash scripts/e2e-m19-gd.sh a b c
#
# Phần giao diện của e2e bước 22 (docs/thay-doi/M19-thiet-ke.md mục 7.2): E10, E23, E41,
# cộng một phần luật chuyển hướng. Phần dòng lệnh và API là scripts/e2e-m19.sh. Như
# e2e M16 và M18, các khẳng định chính do TRÌNH DUYỆT làm: Chrome không giao diện trên
# Mac (scripts/anh-readme, bộ m19-that) mở panel thật của máy ảo qua https, gõ vào
# form và bấm nút như người dùng. Script kiểm lại trong máy ảo bằng lệnh độc lập với
# giao diện (tpan site alias list --json, curl, tpan.db).
#
# Kịch bản:
#   A  máy sạch; build và chép tpan của cây vào máy ảo (make vm-deploy); dựng máy bằng
#      scripts/e2e-m19/dung-may.sh (tpan setup, stack nginx, pebble làm máy chủ ACME thử,
#      user khach_a và khach_b, /etc/hosts trỏ các tên thử về 127.0.0.1); site example.com
#      của admin, site shop.khach-b.test của khach_b. Mật khẩu admin và khach_b đọc vào
#      danh sách che, không in.
#   B  E10: trình duyệt -buoc them-ten: admin thêm tên phụ example.net (phục vụ) ở tab Tên
#      miền. Trong máy ảo: site alias list --json có example.net serve active; job
#      site.alias.add thành công; Host example.net /readme.html trả 200.
#   C  E41: SSL cho example.com bằng pebble, thêm tên phụ nodns.example.invalid (không có
#      DNS) bằng CLI; trình duyệt -buoc chua-dat: tab Tên miền hiện tên đó với DNS chưa
#      đạt và "chưa" ở cột Chứng chỉ (ảnh sáng và tối).
#   D  E23: trình duyệt -buoc tu-choi: khach_b thêm www.example.com (tên của site admin) ở
#      tab Tên miền của site mình: thấy câu từ chối chung, không bản xem trước, trang
#      không nhắc tên site hay chủ khác. Trong máy ảo: site của khach_b không có tên đó.
#   E  trình duyệt -buoc them-luat: nguồn "/a b" báo lỗi dưới ô nguồn; sửa thành /cu →
#      /moi, xem trước, chạy. Trong máy ảo: site redirect list có luật /cu, https
#      example.com/cu trả 301 về /moi.
#   F  (luôn chạy cuối, khi có E2E_LOG) quét chính file log: không mật khẩu, IP máy ảo,
#      IP của Mac, thư mục nhà.
#
# ĐỒ NGHỀ TEST, CHỈ TRONG MÁY ẢO: pebble (scripts/e2e-m19/pebble.sh), jobs.py của
# scripts/e2e-m18/. Mật khẩu admin chuyển cho trình duyệt qua biến môi trường
# TPAN_PANEL_PASSWORD, mật khẩu khach_b qua TPAN_USER_PASSWORD (không qua dòng lệnh).
#
# Output: mỗi lệnh có dấu thời gian, output nguyên văn (đã che) và mã thoát; mỗi khẳng
# định in "=> ĐẠT" hoặc "=> KHÔNG ĐẠT"; cuối mỗi phần và cuối lần chạy in số đạt, không
# đạt. Không dùng set -e.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2

VM=scripts/vm.sh
VMNAME="${TPAN_VM:-$(tr -d '[:space:]' 2>/dev/null <.tpan-vm)}"
VMNAME="${VMNAME:-tpan-test}"
# Mọi lệnh máy ảo qua lớp bắt output của vm.sh (xem scripts/e2e-m19.sh).
export TPAN_VM_CAPTURE=1
R=/home/ubuntu/e2e-m19 # script trong máy ảo (như scripts/e2e-m19.sh)
FAILS=0
PASSES=0
OUT=""
RC=0
EMAIL=admin@example.com
SITE=example.com
WWW=www.example.com
NET=example.net
NODNS=nodns.example.invalid
UB=khach_b
B_SITE=shop.khach-b.test
VER="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
DEBUG_PORT=18192 # cổng DevTools riêng, không đụng anh-m16 (18162), e2e-m16 (18172), e2e-m18-gd (18182)
PHOTO_DIR=docs/bang-chung/M19

mkdir -p tmp-phien
TMPD="$(mktemp -d "$PWD/tmp-phien/e2e-m19-gd.XXXXXX")"
chmod 700 "$TMPD"
SECRETS="$TMPD/secrets"
: >"$SECRETS"
chmod 600 "$SECRETS"
trap 'rm -rf "$TMPD"' EXIT
trap 'exit 130' INT TERM
ANH="$TMPD/anh" # chương trình trình duyệt (scripts/anh-readme), build một lần
ANH_OUT="$TMPD/anh-out"
STDIN_EMPTY="$TMPD/stdin-rong"
: >"$STDIN_EMPTY"

ts() { date '+%Y-%m-%d %H:%M:%S'; }
section() { printf '\n\n######## [%s] %s\n' "$(ts)" "$*"; }

VM_IP=""
HOST_IP=""
NET4=""
VM_V6=""
V6PFX=""
BASE=""

# mask_str che một chuỗi (stdin) theo mọi thứ đã biết, in ra stdout. Hai dòng sed cuối
# che mọi chuỗi dạng mật khẩu (26 chữ in hoa và số của tpan setup, 32 chữ và số của WordPress).
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
    [ -n "$p" ] && s="${s//"$p"/<bí mật, đã che>}"
  done <"$SECRETS"
  s="$(sed -E \
    -e 's/(^|[^A-Za-z0-9])[A-Z0-9]{26}([^A-Za-z0-9]|$)/\1<mật khẩu đã che>\2/g' \
    -e 's/(^|[^A-Za-z0-9])[A-Za-z0-9]{32}([^A-Za-z0-9]|$)/\1<mật khẩu đã che>\2/g' \
    -e 's/inet6 [0-9a-f:]{3,}/inet6 <v6>/g' <<<"$s")"
  printf '%s' "$s"
}
mask() { OUT="$(mask_str <<<"$OUT")"; }

# runq chạy lệnh, giữ output thô (stdout và stderr gộp) ở $TMPD/run.out, KHÔNG in. Stdin là
# một file thường rỗng: multipass 1.17 không gửi EOF khi stdin là /dev/null.
runq() {
  RUNQ_CMD="$(mask_str <<<"$*")"
  RUNQ_TS="$(ts)"
  "$@" <"$STDIN_EMPTY" >"$TMPD/run.out" 2>&1
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
note() { printf '  -- GHI NHẬN: %s\n' "$*"; }
P0=0
F0=0
part_begin() { P0=$PASSES F0=$FAILS; section "PHẦN $*"; }
part_end() { printf '\n==== Phần %s: %s đạt, %s không đạt\n' "$1" "$((PASSES - P0))" "$((FAILS - F0))"; }

rc_is() { [ "$RC" -eq "$1" ]; }
has() { grep -qE -- "$1" <<<"$OUT"; }
hasi() { grep -qiE -- "$1" <<<"$OUT"; }
out_is() { [ "$(tr -d '[:space:]' <<<"$OUT")" = "$1" ]; }
jraw() { jq -e "$@" "$TMPD/run.json" >/dev/null 2>&1; }

# verdicts cộng các dòng "=> ĐẠT"/"=> KHÔNG ĐẠT" trong $OUT (output của script chạy trong máy ảo).
verdicts() {
  PASSES=$((PASSES + $(grep -c '=> ĐẠT:' <<<"$OUT")))
  FAILS=$((FAILS + $(grep -c '=> KHÔNG ĐẠT:' <<<"$OUT")))
}

vexec() { run "$VM" exec "$@"; }
vroot() { run "$VM" root "$@"; }

add_secret() {
  [ -n "${1:-}" ] || return 0
  grep -qxF -- "$1" "$SECRETS" || printf '%s\n' "$1" >>"$SECRETS"
}
save_pw() {
  add_secret "$2"
  (umask 077 && printf '%s' "$2" >"$TMPD/pw.$1")
}

# pull_pw <tên> <file trong máy ảo>: đọc mật khẩu vào danh sách che và $TMPD/pw.<tên> (0600). KHÔNG in.
pull_pw() {
  local pw
  pw="$("$VM" root cat "$2" <"$STDIN_EMPTY" 2>/dev/null | tr -d '[:space:]')"
  [ -n "$pw" ] && save_pw "$1" "$pw"
  printf '\n[%s] (đọc %s vào danh sách che; không in giá trị; có mật khẩu: %s)\n' "$(ts)" "$2" "$([ -n "$pw" ] && echo có || echo không)"
}

# vjson <lệnh ... --json> (bằng root): stdout THÔ ở $TMPD/run.json; in cả hai luồng (đã che).
vjson() {
  RUNQ_CMD="$(mask_str <<<"$VM root $*")"
  RUNQ_TS="$(ts)"
  "$VM" root "$@" <"$STDIN_EMPTY" >"$TMPD/run.json" 2>"$TMPD/run.err"
  RC=$?
  tr -d '\r' <"$TMPD/run.err" | cat - "$TMPD/run.json" >"$TMPD/run.out"
  show_last
}

# ---------------------------------------------------------------- máy ảo

addrs() {
  VM_IP="$("$VM" ip <"$STDIN_EMPTY" | tr -d '[:space:]')"
  NET4="${VM_IP%.*}."
  HOST_IP="$("$VM" exec bash -c 'echo "${SSH_CLIENT%% *}"' <"$STDIN_EMPTY" | tr -d '\r')"
  local _
  for _ in $(seq 1 30); do
    VM_V6="$("$VM" exec bash -c 'ip -o -6 addr show scope global | awk "{print \$4}" | cut -d/ -f1' <"$STDIN_EMPTY" | tr -d '\r')"
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

# push_tools: chép scripts/e2e-m19/* (dựng máy, pebble) và jobs.py của e2e M18 vào $R.
push_tools() {
  local f n=0
  for f in scripts/e2e-m19/*; do
    "$VM" push "$f" "$R/${f##*/}" <"$STDIN_EMPTY" >/dev/null 2>&1 && n=$((n + 1))
  done
  "$VM" push scripts/e2e-m18/jobs.py "$R/jobs.py" <"$STDIN_EMPTY" >/dev/null 2>&1 && n=$((n + 1))
  vroot bash -c 'ls "$1"' _ "$R"
  ok "(đồ nghề) $n file đã chép vào $R, có dung-may.sh, chung.sh, pebble.sh, jobs.py" bash -c '[ "$2" -gt 3 ] && for f in dung-may.sh chung.sh pebble.sh jobs.py; do grep -qx "$f" <<<"$1" || exit 1; done' _ "$OUT" "$n"
}

# vm_script <script> [tham số...]: chạy một script của $R bằng root; cộng các khẳng định nó in.
vm_script() {
  local script=$1
  shift
  runq "$VM" root bash "$R/$script" "$@"
  show_last
  verdicts
}

jobs_py() { vroot python3 "$R/jobs.py" "$@"; }
job_mark() { "$VM" root python3 "$R/jobs.py" mark <"$STDIN_EMPTY" | tr -d '\r' | sed -nE 's/^moc ([0-9]+)$/\1/p'; }
# wait_idle <mốc>: chờ tới khi mọi job sau mốc đã xong (kể cả job SSL tự chạy tiếp), tối đa 6 phút.
wait_idle() {
  local _
  for _ in $(seq 1 180); do
    runq "$VM" root python3 "$R/jobs.py" since "$1"
    grep -qE '^job [^ ]+ (running|queued) ' "$TMPD/run.out" || return 0
    sleep 2
  done
  return 1
}
# all_succeeded: mọi dòng "job ..." trong $OUT đều ở trạng thái succeeded (và có ít nhất một dòng).
all_succeeded() { grep -qE '^job ' <<<"$OUT" && [ -z "$(grep -E '^job ' <<<"$OUT" | awk '$3 != "succeeded"')" ]; }

# site_add <tên miền> [cờ...]: tpan site add bằng root, --json vào file trong máy ảo (mật khẩu
# WordPress nằm trong file đó, không bao giờ in); chỉ in mã thoát và ba dòng cuối của stderr.
site_add() {
  runq "$VM" root bash -c 'd=$1; e=$2; shift 2; tpan site add "$d" --email "$e" --yes --json "$@" >"/root/gd-site-$d.json" 2>"/root/gd-site-$d.err" </dev/null; rc=$?; echo "tpan site add $d: thoát $rc"; tail -3 "/root/gd-site-$d.err"; exit $rc' _ "$1" "$EMAIL" "${@:2}"
  show_last
}

# http_code <Host> <đường dẫn>: mã HTTP của GET http://127.0.0.1<đường dẫn> với Host đó, vào OUT.
http_code() { run "$VM" root curl -s -m 30 -o /dev/null -w '%{http_code}' -H "Host: $1" "http://127.0.0.1$2"; }

# wait_panel: chờ panel trả lời ở cổng 8443 từ Mac (tối đa 60 giây). Chứng chỉ tự ký: -k chỉ
# ở phép chờ này; trình duyệt đi bằng --ignore-certificate-errors.
wait_panel() {
  local _
  for _ in $(seq 1 60); do
    [ "$(curl -sk -m 5 -o /dev/null -w '%{http_code}' "$BASE/api/v1/health" </dev/null 2>/dev/null)" = 200 ] && return 0
    sleep 1
  done
  return 1
}

# browser_run <cờ của bộ m19-that...>: chạy chương trình trình duyệt. Mật khẩu đi qua biến
# môi trường, không vào dòng lệnh. Output giữ ở $TMPD/br.out cho tới browser_report.
browser_run() {
  printf '\n[%s] $ anh-readme %s   (Chrome không giao diện trên Mac, panel <panel>)\n' "$(ts)" "$(mask_str <<<"$*")"
  TPAN_PANEL_PASSWORD="$(cat "$TMPD/pw.admin")" TPAN_USER_PASSWORD="$(cat "$TMPD/pw.b" 2>/dev/null)" \
    "$ANH" -panel "$BASE" -debug-port "$DEBUG_PORT" -out "$ANH_OUT" "$@" </dev/null >"$TMPD/br.out" 2>&1
  RC=$?
}
browser_report() {
  local desc=$1 p f
  OUT="$(tr -d '\r' <"$TMPD/br.out")"
  mask
  printf '%s\n[%s] (mã thoát %s)\n' "$OUT" "$(ts)" "$RC"
  p="$(grep -c '^  => ĐẠT:' <<<"$OUT")"
  f="$(grep -c '^  => KHÔNG ĐẠT:' <<<"$OUT")"
  PASSES=$((PASSES + p))
  FAILS=$((FAILS + f))
  if [ "$RC" -ne 0 ] && [ "$f" -eq 0 ]; then
    ok "$desc: chương trình trình duyệt chạy hết" false
  fi
}

# save_photos: chép ảnh trình duyệt đã chụp vào docs/bang-chung/M19 (sau khi build xong, nên
# không làm tên bản tpan có đuôi -dirty).
save_photos() {
  ls "$ANH_OUT"/*.png >/dev/null 2>&1 || return 0
  mkdir -p "$PHOTO_DIR"
  cp "$ANH_OUT"/*.png "$PHOTO_DIR"/
  printf '\nẢnh của trình duyệt (%s file) đã chép vào %s\n' "$(find "$ANH_OUT" -name '*.png' | wc -l | tr -d ' ')" "$PHOTO_DIR"
}

# ================================================================ PHẦN A

part_a() {
  part_begin "A: máy sạch, tpan của cây, dựng máy (stack nginx, pebble, hai user), hai site"
  run git log --oneline -1
  run git status --short
  mkdir -p "$ANH_OUT"
  run go build -o "$ANH" ./scripts/anh-readme
  ok "build chương trình trình duyệt (scripts/anh-readme)" rc_is 0
  reset_vm
  run make vm-deploy DATE="$BUILD_DATE"
  ok "make vm-deploy: build và chép tpan $VER vào máy ảo" rc_is 0
  [ "$RC" -eq 0 ] || bail "không build hoặc không chép được tpan"
  vexec tpan version
  ok "tpan version là $VER" has "^tpan $VER "
  push_tools

  # dung-may.sh: tpan setup, stack, pebble, ssl settings, khach_a, khach_b, /etc/hosts, phiên API.
  vm_script dung-may.sh nginx
  pull_pw admin /root/m19-admin.pw
  pull_pw b "/root/m19-$UB.pw"
  [ -s "$TMPD/pw.admin" ] || bail "không có mật khẩu quản trị panel (/root/m19-admin.pw)"
  [ -s "$TMPD/pw.b" ] || bail "không có mật khẩu của $UB"

  site_add "$SITE"
  ok "site add $SITE (admin): thoát 0" rc_is 0
  site_add "$B_SITE" --owner "$UB"
  ok "site add $B_SITE --owner $UB: thoát 0" rc_is 0
  http_code "$SITE" /
  ok "site $SITE trả 200" out_is 200
  http_code "$B_SITE" /
  ok "site $B_SITE trả 200" out_is 200
  vjson tpan site alias list "$SITE" --json
  ok "$SITE có tên phụ mặc định $WWW (redirect, active) và chưa có $NET" jraw --arg w "$WWW" --arg n "$NET" 'any(.[]; .name == $w and .mode == "redirect" and .state == "active") and all(.[]; .name != $n)'

  wait_panel
  ok "panel trả lời ở https://<vm>:8443 (gọi từ Mac)" rc_is 0
  part_end A
}

# ================================================================ PHẦN B (E10)

part_b() {
  part_begin "B: E10 thêm tên phụ $NET bằng giao diện (bấm thật), thấy dòng trong bảng"
  local mark
  mark="$(job_mark)"
  printf 'Mốc job trước khi bấm: %s\n' "$mark"
  browser_run -bo m19-that -buoc them-ten -site "$SITE"
  browser_report "E10 thêm tên phụ"

  section "B: kiểm trong máy ảo"
  vjson tpan site alias list "$SITE" --json
  ok "E10: site alias list có $NET kiểu serve, active (do cú bấm của trình duyệt)" jraw --arg n "$NET" 'any(.[]; .name == $n and .kind == "alias" and .mode == "serve" and .state == "active")'
  ok "E10: $WWW vẫn là tên phụ redirect (cú bấm không đụng tên khác)" jraw --arg w "$WWW" 'any(.[]; .name == $w and .mode == "redirect")'
  jobs_py since "$mark"
  ok "E10: Lịch sử có job site.alias.add của $SITE, mọi job sau mốc đều succeeded" bash -c 'grep -qE "^job [^ ]+ succeeded site\.alias\.add $2 " <<<"$1"' _ "$OUT" "$SITE"
  ok "E10: không job nào sau mốc thất bại" all_succeeded
  http_code "$NET" /readme.html
  ok "E10: Host $NET (serve) trả 200 cho /readme.html" out_is 200
  part_end B
}

# ================================================================ PHẦN C (E41)

part_c() {
  part_begin "C: E41 tab Tên miền hiện tên chưa đạt ($NODNS), site có SSL"
  local mark
  mark="$(job_mark)"
  vroot tpan ssl issue "$SITE" --yes
  ok "E41 (dựng): ssl issue $SITE (pebble) thoát 0" rc_is 0
  ok "E41 (dựng): mọi job SSL đã xong" wait_idle "$mark"
  vroot bash -c 'openssl x509 -in "/etc/letsencrypt/live/$1/cert.pem" -noout -ext subjectAltName' _ "$SITE"
  ok "E41 (dựng): chứng chỉ có $SITE và $NET (tên thêm bằng giao diện ở phần B)" bash -c 'grep -q "DNS:$2" <<<"$1" && grep -q "DNS:$3" <<<"$1"' _ "$OUT" "$SITE" "$NET"

  mark="$(job_mark)"
  vroot tpan site alias add "$SITE" "$NODNS" --mode serve --yes
  ok "E41 (dựng): site alias add $NODNS thoát 0 (thêm được dù không có DNS)" rc_is 0
  ok "E41 (dựng): job SSL tự chạy tiếp sau khi thêm tên đã xong" wait_idle "$mark"
  vjson tpan site alias list "$SITE" --json
  ok "E41 (dựng): $NODNS active, in_cert false; $NET in_cert true" jraw --arg d "$NODNS" --arg n "$NET" 'any(.[]; .name == $d and .state == "active" and .in_cert == false) and any(.[]; .name == $n and .in_cert == true)'

  browser_run -bo m19-that -buoc chua-dat -site "$SITE"
  browser_report "E41 tên chưa đạt"
  part_end C
}

# ================================================================ PHẦN D (E23)

part_d() {
  part_begin "D: E23 khach_b thêm $WWW (tên của site admin) bằng giao diện: câu từ chối chung"
  vjson tpan site alias list "$B_SITE" --json
  local before
  before="$(jq 'length' "$TMPD/run.json" 2>/dev/null)"
  browser_run -bo m19-that -buoc tu-choi -ten "$UB" -site "$B_SITE"
  browser_report "E23 từ chối chung"

  section "D: kiểm trong máy ảo"
  vjson tpan site alias list "$B_SITE" --json
  ok "E23: site $B_SITE vẫn $before tên, không có $WWW" jraw --arg w "$WWW" --argjson n "${before:-0}" 'length == $n and all(.[]; .name != $w)'
  vjson tpan site alias list "$SITE" --json
  ok "E23: $WWW vẫn là tên phụ của $SITE (không bị cướp)" jraw --arg w "$WWW" 'any(.[]; .name == $w and .kind == "alias")'
  part_end D
}

# ================================================================ PHẦN E (luật chuyển hướng)

part_e() {
  part_begin "E: luật chuyển hướng bằng giao diện (nguồn sai báo dưới ô nguồn, rồi /cu → /moi)"
  browser_run -bo m19-that -buoc them-luat -site "$SITE"
  browser_report "luật chuyển hướng"

  section "E: kiểm trong máy ảo"
  vjson tpan site redirect list "$SITE" --json
  ok "site redirect list có luật /cu → /moi mã 301 (do cú bấm của trình duyệt)" jraw '.. | objects | select(.source? == "/cu") | .target == "/moi" and .code == 301'
  ok "nguồn sai \"/a b\" không vào danh sách" jraw '[.. | objects | select(.source? == "/a b")] | length == 0'
  run "$VM" root bash -c 'curl -sk -m 30 -o /dev/null -D - --resolve "$1:443:127.0.0.1" "https://$1/cu"' _ "$SITE"
  ok "https://$SITE/cu trả 301" has '^HTTP/[0-9.]+ 301'
  ok "Location của /cu trỏ tới /moi" hasi '^location: (https?://[^/]+)?/moi'
  part_end E
}

# ================================================================ PHẦN F

leak_check() {
  [ -n "${E2E_LOG:-}" ] && [ -f "$E2E_LOG" ] || return 0
  section "PHẦN F: mật khẩu, IP, thư mục nhà có lọt vào chính file log không"
  local n=0 s
  while IFS= read -r s; do
    [ -n "$s" ] || continue
    n=$((n + $(grep -cF -- "$s" "$E2E_LOG")))
  done <"$SECRETS"
  printf 'Số mật khẩu đã gặp: %s; số dòng của log chứa một trong số đó: %s\n' "$(grep -c . "$SECRETS")" "$n"
  ok "log không chứa mật khẩu nào" test "$n" -eq 0
  ok "log không chứa IP máy ảo (IPv4)" bash -c '[ -z "$1" ] || ! grep -qF -- "$1" "$2"' _ "$VM_IP" "$E2E_LOG"
  ok "log không chứa mạng Multipass" bash -c '[ -z "$1" ] || ! grep -qF -- "$1" "$2"' _ "$NET4" "$E2E_LOG"
  ok "log không chứa IPv6 của máy ảo" bash -c '[ -z "$1" ] || ! grep -qiF -- "$1" "$2"' _ "$V6PFX" "$E2E_LOG"
  ok "log không chứa IP của Mac" bash -c '[ -z "$1" ] || ! grep -qF -- "$1" "$2"' _ "$HOST_IP" "$E2E_LOG"
  ok "log không chứa thư mục nhà hay tên user Mac" bash -c '! grep -qF -- "$1" "$3" && ! grep -qF -- "$2" "$3"' _ "$HOME" "$USER" "$E2E_LOG"
}

bail() {
  printf '  => KHÔNG ĐẠT: DỪNG KỊCH BẢN: %s\n' "$*"
  FAILS=$((FAILS + 1))
  save_photos
  leak_check
  section "TỔNG KẾT: $PASSES khẳng định đạt, $FAILS khẳng định không đạt (dừng giữa chừng)"
  exit 1
}

PARTS=("$@")
[ ${#PARTS[@]} -gt 0 ] || PARTS=(a b c d e)
for p in "${PARTS[@]}"; do
  case "$p" in
    a | b | c | d | e) ;;
    *) printf 'phần chỉ nhận a, b, c, d, e (đang là %s)\n' "$p" >&2; exit 2 ;;
  esac
done

printf 'e2e giao diện M19. tpan của cây: %s; máy ảo: %s; các phần: %s\n' "$VER" "$VMNAME" "${PARTS[*]}"
for p in "${PARTS[@]}"; do
  # Phần chạy riêng (không có a trước nó) vẫn cần địa chỉ máy ảo, chương trình trình duyệt, mật khẩu.
  if [ -z "$VM_IP" ] && [ "$p" != a ]; then
    bail "phần $p cần biến của phần A trong cùng lần chạy (mật khẩu admin, khach_b, địa chỉ panel): chạy kèm a"
  fi
  "part_$p"
done

save_photos
leak_check
section "TỔNG KẾT: $PASSES khẳng định đạt, $FAILS khẳng định không đạt"
[ "$FAILS" -eq 0 ]
