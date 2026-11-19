#!/usr/bin/env bash
# e2e của mốc M11 (cài bằng một lệnh, gói phát hành, self-update) trên máy ảo
# (tên trong file .tpan-vm; không có thì tpan-test). Chạy trên Mac, trong thư
# mục repo:
#
#   scripts/e2e-m11.sh all   0, 1, 2, 3, 4, 5 rồi 6 (mặc định)
#   scripts/e2e-m11.sh 0     make release (bản của cây + bản e2e-m11-b), máy sạch
#   scripts/e2e-m11.sh 1     chép gói sang máy, MỘT lệnh: sudo ./install.sh --web nginx
#   scripts/e2e-m11.sh 2     tạo site, 200, health; đăng nhập API qua https
#   scripts/e2e-m11.sh 3     chạy lại install.sh trong gói: không đổi gì
#   scripts/e2e-m11.sh 4     gói hỏng một byte: dừng ở bước kiểm sha256
#   scripts/e2e-m11.sh 5     máy sạch khác lượt, curl | sudo bash -s -- --from <loopback> --web apache
#   scripts/e2e-m11.sh 6     tpan self-update lên bản e2e-m11-b rồi --rollback (cần máy đã qua 5)
#
# Các phần:
#   0  `make release` hai lần trên Mac: bản của cây (VERSION = git describe,
#      giống Makefile) và bản e2e-m11-b (cho self-update). In SHA256SUMS. Rồi
#      `make vm-reset`: máy chưa có tpan.
#   1  `multipass transfer -r` cả thư mục gói sang máy, rồi đúng một lệnh
#      `cd <gói> && sudo ./install.sh --web nginx`. Kiểm độc lập: sha256 của
#      /usr/local/bin/tpan khớp dòng tpan-linux-<arch> của SHA256SUMS,
#      tpan.service active, enabled, nghe 8443; mật khẩu quản trị in đúng một
#      lần (che trong log); nginx, php8.3-fpm, mariadb active; health đạt cả
#      nhóm tpan, trừ đúng tpan.backup:db warn (máy mới chưa sao lưu tpan.db
#      lần nào; lịch backup hằng ngày mặc định tắt).
#   2  `sudo tpan site add a.example.com --email admin@example.com --yes`: 200,
#      <title>, thân trang nhắc tên site (curl --resolve trong máy và từ Mac),
#      health của site đạt. Từ Mac tới https://<vm>:8443 bằng --cacert là
#      cert.pem của panel, fingerprint openssl s_client khớp dòng setup in ra;
#      POST /api/v1/login rồi GET /api/v1/sites thấy site.
#   3  chạy lại đúng lệnh ở 1: sha256 file chạy, cert, key, unit, PID dịch vụ,
#      hash mật khẩu, cây cấu hình nginx/php/tpan không đổi; không in mật khẩu.
#   4  bản sao gói, đổi một byte của tpan-linux-<arch>: install.sh dừng ở bước
#      "Kiểm sha256", file đang cài và dịch vụ nguyên vẹn.
#   5  máy sạch lần hai; gói đặt trong máy, `python3 -m http.server` nghe
#      127.0.0.1 phục vụ nó (thay cho GitHub khi repo còn riêng tư); đúng một
#      lệnh `curl -fsSL http://127.0.0.1:<cổng>/install.sh | sudo bash -s --
#      --from http://127.0.0.1:<cổng> --web apache`; rồi tạo site, 200.
#   6  gói e2e-m11-b phục vụ ở <gốc>/e2e-m11-b: `tpan self-update --url ...`
#      --dry-run không đổi gì, --yes lên bản b (dịch vụ chạy bản b, có
#      tpan.old là bản đầu), --rollback về đúng file của bản đầu.
#
# ĐỒ NGHỀ TEST, CHỈ TRONG MÁY ẢO: máy chủ HTTP loopback (unit tpan-e2e-rel),
# bản sao gói hỏng ở /tmp/pkg-bad.
#
# Đặt E2E_LOG=<file log> để cuối lần chạy script tự tìm trong chính file log
# đó: mọi mật khẩu đã gặp, cookie phiên, IP máy ảo (IPv4, IPv6), IP của Mac,
# thư mục nhà và tên user Mac (phải là 0 lần).
#
# Output như các e2e trước: mỗi lệnh có dấu thời gian, output nguyên văn (đã
# che) và mã thoát; mỗi khẳng định in "=> ĐẠT" hoặc "=> KHÔNG ĐẠT"; cuối mỗi
# phần và cuối lần chạy in số đạt, không đạt.
# Không dùng set -e: nhiều lệnh ở đây được chờ là phải lỗi.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2

VM=scripts/vm.sh
VMNAME="$(tr -d '[:space:]' 2>/dev/null <.tpan-vm)"
VMNAME="${VMNAME:-tpan-test}"
FAILS=0
PASSES=0
OUT=""
RC=0
EMAIL=admin@example.com
S1=a.example.com # site ở lượt nginx (phần 2)
S2=b.example.com # site ở lượt apache (phần 5)
PORT=18711
# Bản như Makefile tự tính (VERSION ?= git describe ...). Tính một lần ở đây và
# đưa thẳng cho make, để thư mục gói chắc chắn là dist/release/$VER.
VER="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
VER_B=e2e-m11-b
PKG_A="dist/release/$VER"
PKG_B="dist/release/$VER_B"
VM_PKG="/home/ubuntu/$VER" # chỗ gói nằm trong máy ảo (multipass transfer -r giữ tên thư mục)
TMPD="$(mktemp -d)"
chmod 700 "$TMPD"
SECRETS="$TMPD/secrets"
: >"$SECRETS"
chmod 600 "$SECRETS"

# REL_UP=1 khi máy chủ HTTP loopback đang chạy: chỉ khi đó cleanup mới gọi vào
# máy ảo (một lần multipass exec lúc thoát từng treo dù không còn gì để dừng).
REL_UP=0
cleanup() {
  [ "$REL_UP" = 1 ] && "$VM" root systemctl stop tpan-e2e-rel </dev/null >/dev/null 2>&1
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
  # openssl in IPv6 trong SAN bằng chữ hoa.
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

# run in lệnh, chạy, in output đã che và mã thoát. Output vào file rồi mới đọc
# (multipass exec trên Mac có lúc treo khi stdout là pipe và output dài);
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

# quiet chạy như run nhưng không in output (ảnh chụp trạng thái để so).
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
hasntF() { ! grep -qF -- "$1" <<<"$OUT"; }
out_is() { [ "$OUT" = "$1" ]; }
job_ok() { [ "$RC" = 0 ] && grep -q '^Job .*: thành công' <<<"$OUT"; }

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

ARCH=""
arch() {
  [ -n "$ARCH" ] || ARCH="$("$VM" exec dpkg --print-architecture </dev/null | tr -d '\r')"
  printf '%s' "$ARCH"
}

# sum_of <thư mục gói> <tên file>: sha256 ghi trong SHA256SUMS của gói (trên Mac).
sum_of() { awk -v n="$2" '$2 == n || $2 == "*" n { print $1 }' "$1/SHA256SUMS"; }

# ---------------------------------------------------------------- máy ảo

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
  vexec bash -c 'command -v tpan; ls -la /usr/local/bin; systemctl cat tpan.service 2>&1 | head -1; ls /etc/tpan /var/lib/tpan 2>&1'
  ok "máy sạch: chưa có lệnh tpan" hasnt '^/usr/local/bin/tpan'
  ok "máy sạch: chưa có tpan.service" hasF 'No files found for tpan.service'
  ok "máy sạch: chưa có /etc/tpan, /var/lib/tpan" bash -c '[ "$(grep -c "No such file or directory" <<<"$1")" -eq 2 ]' _ "$OUT"
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

# ---------------------------------------------------------------- cài

# install_run <lệnh...>: chạy một lần cài, nhặt mật khẩu quản trị và
# fingerprint trong output trước khi in (mật khẩu bị che, số lần xuất hiện
# được đếm). Kết quả: RC, OUT, PW, PW_HITS, FP.
PW=""
PW_HITS=0
FP=""
install_run() {
  printf '\n[%s] $ %s\n' "$(ts)" "$(mask_str <<<"$*")"
  "$@" >"$TMPD/run.out" 2>&1 </dev/null
  RC=$?
  OUT="$(tr -d '\r' <"$TMPD/run.out")"
  local pw
  pw="$(grep -A1 '^Mật khẩu quản trị (chỉ hiện MỘT lần' <<<"$OUT" | sed -nE 's/^    ([A-Z2-7]{26})$/\1/p' | head -1)"
  FP="$(grep -A1 '^Dấu vân tay SHA-256' <<<"$OUT" | sed -nE 's/^    ([0-9A-F:]{95})$/\1/p' | head -1)"
  PW_HITS=0
  if [ -n "$pw" ]; then
    PW="$pw"
    add_secret "$pw"
    PW_HITS="$(grep -oF -- "$pw" <<<"$OUT" | wc -l | tr -d ' ')"
  fi
  mask
  printf '%s\n[%s] (mã thoát %s)\n' "$OUT" "$(ts)" "$RC"
  printf 'Mật khẩu quản trị panel trong output: %s lần\n' "$PW_HITS"
}

# Ảnh chụp trạng thái để so trước và sau một lần chạy lại.
STATE_SCRIPT='
sha256sum /usr/local/bin/tpan /etc/tpan/tls/cert.pem /etc/tpan/tls/key.pem /etc/systemd/system/tpan.service
stat -c "%a %U:%G %n" /usr/local/bin/tpan
systemctl show -p MainPID -p ExecMainStartTimestamp -p ActiveState -p UnitFileState tpan
python3 -c "import sqlite3, hashlib; c = sqlite3.connect(\"file:/var/lib/tpan/tpan.db?mode=ro\", uri=True); print(\"hash mật khẩu (sha256 của hash):\", hashlib.sha256(c.execute(\"SELECT value FROM settings WHERE key = ?\", (\"admin.password_hash\",)).fetchone()[0].encode()).hexdigest()[:16])"
echo "cây cấu hình (nginx, apache2, php, tpan, systemd):" $(find /etc/nginx /etc/apache2 /etc/php /etc/tpan /etc/systemd/system -type f 2>/dev/null | sort | xargs sha256sum | sha256sum | cut -c1-16)
echo "gói đã cài:" $(dpkg-query -W -f "\${Package} \${Version} \${db:Status-Status}\n" | sort | sha256sum | cut -c1-16)
systemctl show -p MainPID nginx php8.3-fpm mariadb apache2 | tr "\n" " "; echo
ls /usr/local/bin
'

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
  printf '%s\n(chỉ in id, trạng thái và tóm tắt của phép kiểm không đạt)\n[%s] (mã thoát %s)\n' "$OUT" "$(ts)" "$RC"
  printf 'Số phép kiểm: %s; không pass: %s\n' "$(jq '[.groups[].results[]] | length' <<<"$HJ" 2>/dev/null)" "$(jq '[.groups[].results[] | select(.status != "pass")] | length' <<<"$HJ" 2>/dev/null)"
}
# health_all_pass: mọi phép kiểm (kể cả nhóm tpan: đã chạy tpan setup) đều pass.
health_all_pass() { jq -e '([.groups[].results[]] | length) > 0 and ([.groups[].results[] | select(.status != "pass")] | length == 0)' <<<"$HJ" >/dev/null 2>&1; }
# health_fresh_pass: như trên, trừ đúng một ngoại lệ có chủ ý trên máy mới cài:
# tpan.backup:db là warn "chưa có bản sao lưu tpan.db nào" (internal/checks/m9.go:
# máy mới cài chưa sao lưu lần nào là tình trạng có thật). Không có Fail nào.
health_fresh_pass() { jq -e '([.groups[].results[]] | length) > 0 and ([.groups[].results[] | select(.status != "pass") | "\(.id) \(.status)"] - ["tpan.backup:db warn"] | length == 0)' <<<"$HJ" >/dev/null 2>&1; }
health_has_group() { jq -e --arg g "$1" '[.groups[] | select(.target == $g) | .results[]] | length > 0' <<<"$HJ" >/dev/null 2>&1; }

# service_checks <web unit>: tpan.service và stack đang chạy.
service_checks() {
  local web=$1
  vexec systemctl is-active tpan
  ok "tpan.service active" out_is active
  vexec systemctl is-enabled tpan
  ok "tpan.service enabled" out_is enabled
  vroot ss -tlnp
  ok "tpan nghe cổng 8443" has ':8443 .*"tpan"'
  vexec systemctl is-active "$web" php8.3-fpm mariadb
  ok "$web, php8.3-fpm, mariadb active" bash -c '[ "$(grep -cx active <<<"$1")" -eq 3 ]' _ "$OUT"
}

# bin_is_pkg <thư mục gói trên Mac>: /usr/local/bin/tpan đúng sha256 của
# tpan-linux-<arch> trong SHA256SUMS của gói.
bin_is_pkg() {
  local want
  want="$(sum_of "$1" "tpan-linux-$(arch)")"
  vroot sha256sum /usr/local/bin/tpan
  printf 'sha256 trong %s/SHA256SUMS cho tpan-linux-%s: %s\n' "$1" "$(arch)" "$want"
  ok "/usr/local/bin/tpan đúng sha256 của tpan-linux-$(arch) trong SHA256SUMS ($1)" test -n "$want" -a "$OUT" = "$want  /usr/local/bin/tpan"
}

# ---------------------------------------------------------------- site

site_add() {
  local d=$1
  shift
  local cmd=("$VM" root tpan site add "$d" --email "$EMAIL" --yes "$@")
  printf '\n[%s] $ %s\n' "$(ts)" "${cmd[*]}"
  "${cmd[@]}" >"$TMPD/run.out" 2>&1 </dev/null
  RC=$?
  OUT="$(tr -d '\r' <"$TMPD/run.out")"
  local pw dbpw hits=0
  pw="$(sed -nE 's/^  Mật khẩu: +([A-Za-z0-9]{32})$/\1/p' <<<"$OUT" | head -1)"
  if [ -n "$pw" ]; then
    add_secret "$pw"
    hits="$(grep -oF -- "$pw" <<<"$OUT" | wc -l | tr -d ' ')"
  fi
  dbpw="$("$VM" root bash -c 'cd / && runuser -u "$2" -- wp config get DB_PASSWORD --path="/var/www/$1/public" 2>/dev/null' _ "$d" "$(user_of "$d")" </dev/null | tr -d '\r')"
  add_secret "$dbpw"
  mask
  printf '%s\n[%s] (mã thoát %s)\n' "$OUT" "$(ts)" "$RC"
  printf 'Mật khẩu quản trị WordPress trong output: %s lần; đã đọc mật khẩu DB của site vào danh sách che (%s ký tự)\n' "$hits" "${#dbpw}"
}

# HTTP_SCRIPT <domain>: mã HTTP, <title>, số dòng thân trang nhắc tên site.
HTTP_SCRIPT='
d=$1
body=$(curl -sS -m 20 --resolve "$d:80:127.0.0.1" -w "\nHTTP %{http_code}\n" "http://$d/")
printf "%s\n" "$body" | tail -1
printf "%s\n" "$body" | grep -o "<title>[^<]*</title>"
echo "thân nhắc $d: $(printf "%s" "$body" | grep -cF "$d") dòng"
'

site_ok() {
  local d=$1
  vexec bash -c "$HTTP_SCRIPT" _ "$d"
  ok "$d: HTTP 200 (curl --resolve trong máy ảo)" has '^HTTP 200$'
  ok "$d: có <title>" has '^<title>[^<]+</title>$'
  ok "$d: thân trang có tên $d" has "^thân nhắc ${d//./\\.}: [1-9][0-9]* dòng$"
  run curl -sS -m 20 -o /dev/null -w '%{http_code}\n' --resolve "$d:80:$VM_IP" "http://$d/"
  ok "$d gọi từ Mac qua IP máy ảo (curl --resolve): 200" out_is 200
  health_check "$d"
  ok "health của $d: mọi phép kiểm pass" health_all_pass
  ok "health có nhóm của site $d" health_has_group "$d"
}

# ---------------------------------------------------------------- API từ Mac

CODE=""
JAR="$TMPD/jar"
# api METHOD PATH [cờ curl...]: gọi panel từ Mac, kiểm chứng chỉ bằng đúng
# cert.pem lấy từ máy ảo (--cacert, không -k). In mã HTTP; thân ở
# $TMPD/body. Cookie đọc và ghi ở $JAR, không in.
api() {
  local m=$1 p=$2
  shift 2
  : >"$TMPD/body"
  run curl -sS -m 60 --cacert "$TMPD/cert.pem" -b "$JAR" -c "$JAR" -X "$m" -o "$TMPD/body" \
    -w '-> HTTP %{http_code}\n' "$@" "https://$VM_IP:8443$p"
  CODE="$(sed -nE 's/^-> HTTP ([0-9]{3})$/\1/p' <<<"$OUT" | tail -1)"
}
code_is() { [ "$CODE" = "$1" ]; }

# ================================================================ 0

part_0() {
  part_begin "0: make release trên Mac, máy ảo sạch"
  run git log --oneline -1
  run git status --short
  printf 'Bản của cây (git describe --tags --always --dirty): %s\n' "$VER"
  run make release VERSION="$VER" DATE="$BUILD_DATE"
  ok "make release VERSION=$VER" rc_is 0
  run make release VERSION="$VER_B" DATE="$BUILD_DATE"
  ok "make release VERSION=$VER_B" rc_is 0
  run ls -l "$PKG_A" "$PKG_B"
  run cat "$PKG_A/SHA256SUMS" "$PKG_B/SHA256SUMS"
  run bash -c 'cd "$1" && shasum -a 256 -c SHA256SUMS && cd "$OLDPWD/$2" && shasum -a 256 -c SHA256SUMS' _ "$PKG_A" "$PKG_B"
  ok "SHA256SUMS của hai gói khớp file trong gói" rc_is 0
  ok "gói $VER có đủ 4 file" test -x "$PKG_A/tpan-linux-amd64" -a -x "$PKG_A/tpan-linux-arm64" -a -x "$PKG_A/install.sh" -a -f "$PKG_A/SHA256SUMS"
  ok "install.sh trong gói giống hệt scripts/install.sh" cmp -s "$PKG_A/install.sh" scripts/install.sh
  reset_vm
  part_end 0
}

# ================================================================ 1

part_1() {
  part_begin "1: chép gói sang máy, cài bằng MỘT lệnh: cd <gói> && sudo ./install.sh --web nginx"
  [ -n "$VM_IP" ] || addrs
  run multipass transfer -r "$PKG_A" "$VMNAME:/home/ubuntu/"
  ok "chép thư mục gói sang máy ảo" rc_is 0
  vexec bash -c 'cd "$1" && stat -c "%a %U %s %n" * && sha256sum -c SHA256SUMS' _ "$VM_PKG"
  ok "gói trong máy ảo: file chạy và install.sh có quyền chạy, SHA256SUMS khớp" bash -c 'grep -q "^755 .* install.sh$" <<<"$1" && grep -q "^755 .* tpan-linux-$2$" <<<"$1" && grep -q "^tpan-linux-$2: OK$" <<<"$1"' _ "$OUT" "$(arch)"

  section "1: lệnh cài"
  install_run "$VM" exec bash -c "cd $VM_PKG && sudo ./install.sh --web nginx"
  ok "install.sh thoát 0" rc_is 0
  ok "kiểm sha256 trong install.sh: khớp" hasF "sha256 khớp: tpan-linux-$(arch)"
  ok "in bản $VER" has "^tpan $VER "
  ok "đã đặt /usr/local/bin/tpan" hasF 'Đã đặt /usr/local/bin/tpan'
  ok "setup và stack install: hai job thành công" bash -c '[ "$(grep -c "^Job .*: thành công" <<<"$1")" -ge 2 ]' _ "$OUT"
  ok "mật khẩu quản trị in đúng một lần" test "$PW_HITS" -eq 1
  ok "có fingerprint SHA-256 dạng AB:CD:..." test -n "$FP"
  ok "kết thúc bằng 'Xong. Bước tiếp:'" hasF 'Xong. Bước tiếp:'
  (umask 077 && printf '%s' "$PW" >"$TMPD/pw1")

  section "1: kiểm độc lập"
  bin_is_pkg "$PKG_A"
  vexec bash -c 'stat -c "%a %U:%G %n" /usr/local/bin/tpan; ls -la /usr/local/bin; tpan version'
  ok "/usr/local/bin/tpan 755 root:root" has '^755 root:root /usr/local/bin/tpan$'
  ok "không sót tpan.install-tmp" hasntF 'install-tmp'
  ok "tpan version là $VER" has "^tpan $VER "
  service_checks nginx
  vroot bash -c 'p=$(systemctl show -p MainPID --value tpan); /proc/$p/exe version'
  ok "tiến trình của tpan.service chạy bản $VER" has "^tpan $VER "
  health_check
  ok "health: mọi phép kiểm pass, trừ tpan.backup:db warn (máy mới, chưa sao lưu tpan.db lần nào)" health_fresh_pass
  ok "health có nhóm tpan (tự kiểm sau setup)" health_has_group tpan
  part_end 1
}

# ================================================================ 2

part_2() {
  part_begin "2: tạo site, đăng nhập API qua https"
  [ -n "$VM_IP" ] || addrs
  [ -n "$FP" ] || FP="$("$VM" root openssl x509 -in /etc/tpan/tls/cert.pem -noout -fingerprint -sha256 </dev/null | tr -d '\r' | sed 's/^.*=//')"
  site_add "$S1"
  ok "site add $S1: thoát 0, job thành công" job_ok
  site_ok "$S1"

  section "2: https://<vm>:8443 từ Mac: chứng chỉ, fingerprint"
  "$VM" root cat /etc/tpan/tls/cert.pem </dev/null | tr -d '\r' >"$TMPD/cert.pem"
  # Riêng một lệnh: OpenSSL 4.1 trên Mac in fingerprint khác khi có thêm -ext.
  run openssl x509 -in "$TMPD/cert.pem" -noout -fingerprint -sha256
  ok "fingerprint của cert.pem khớp dòng tpan setup in ra" hasF "sha256 Fingerprint=$FP"
  run bash -c 'echo | openssl s_client -connect "$1" -servername localhost 2>/dev/null | openssl x509 -noout -fingerprint -sha256' _ "$VM_IP:8443"
  ok "fingerprint chứng chỉ panel đang phục vụ (openssl s_client từ Mac) khớp" hasF "sha256 Fingerprint=$FP"

  section "2: đăng nhập rồi GET /api/v1/sites"
  : >"$JAR"
  chmod 600 "$JAR"
  api GET /api/v1/sites
  ok "GET /api/v1/sites khi chưa đăng nhập: 401" code_is 401
  if [ -s "$TMPD/pw1" ]; then
    jq -Rn '{password: input}' <"$TMPD/pw1" >"$TMPD/login.json"
  else
    printf '{}' >"$TMPD/login.json"
  fi
  chmod 600 "$TMPD/login.json"
  api POST /api/v1/login -H 'Content-Type: application/json' --data-binary "@$TMPD/login.json"
  ok "POST /api/v1/login bằng mật khẩu install.sh in ra: 204" code_is 204
  # Giá trị cookie vào danh sách che (curl ghi cookie HttpOnly thành dòng "#HttpOnly_...").
  while IFS= read -r c; do add_secret "$c"; done < <(awk -F'\t' 'NF == 7 && ($1 !~ /^#/ || $1 ~ /^#HttpOnly_/) { print $7 }' "$JAR")
  printf 'Cookie phiên: %s giá trị (đã vào danh sách che)\n' "$(awk -F'\t' 'NF == 7 && ($1 !~ /^#/ || $1 ~ /^#HttpOnly_/)' "$JAR" | wc -l | tr -d ' ')"
  api GET /api/v1/sites
  OUT="$(jq -c '.' "$TMPD/body" 2>&1 | head -c 3000)"
  mask
  printf '%s\n' "$OUT"
  ok "GET /api/v1/sites: 200" code_is 200
  ok "danh sách site có $S1" jq -e --arg d "$S1" '[.. | objects | select(.domain? == $d)] | length >= 1' "$TMPD/body"
  part_end 2
}

# ================================================================ 3

part_3() {
  part_begin "3: chạy lại install.sh trong gói: không đổi gì"
  [ -n "$VM_IP" ] || addrs
  quiet "$VM" root bash -c "$STATE_SCRIPT"
  local before=$OUT
  printf '%s\n' "$before"
  install_run "$VM" exec bash -c "cd $VM_PKG && sudo ./install.sh --web nginx"
  ok "install.sh lần hai thoát 0" rc_is 0
  ok "báo file chạy đã đúng bản này, giữ nguyên" hasF '/usr/local/bin/tpan đã đúng bản này, giữ nguyên'
  ok "không chép lại file chạy (không có 'Đã đặt')" hasntF 'Đã đặt /usr/local/bin/tpan'
  ok "không khởi động lại dịch vụ tpan" hasntF 'Dịch vụ tpan đang chạy bản cũ'
  ok "báo mật khẩu đã có, giữ nguyên" hasF 'Mật khẩu quản trị đã có từ trước, giữ nguyên'
  ok "không in mật khẩu nào" test "$PW_HITS" -eq 0 -a -z "$(grep -A1 '^Mật khẩu quản trị (chỉ hiện MỘT lần' <<<"$OUT")"
  ok "fingerprint giữ nguyên" test -n "$FP" -a "$(grep -A1 '^Dấu vân tay SHA-256' <<<"$OUT" | tail -1 | tr -d ' ')" = "$FP"
  ok "không bước nào chạy (không có dòng \"xong\")" hasnt '^  xong$'
  quiet "$VM" root bash -c "$STATE_SCRIPT"
  printf '%s\n' "$OUT"
  ok "sha256 file chạy, cert, key, unit; PID và giờ khởi động của dịch vụ; hash mật khẩu; cây cấu hình; gói đã cài; PID nginx, php, mariadb: không đổi" test "$OUT" = "$before"
  [ "$OUT" = "$before" ] || diff <(printf '%s\n' "$before") <(printf '%s\n' "$OUT")
  site_ok "$S1"
  part_end 3
}

# ================================================================ 4

part_4() {
  part_begin "4: gói hỏng một byte: install.sh dừng ở bước kiểm sha256"
  [ -n "$VM_IP" ] || addrs
  local a
  a="$(arch)"
  quiet "$VM" root bash -c "$STATE_SCRIPT"
  local before=$OUT
  vexec bash -c 'set -e; rm -rf /tmp/pkg-bad; cp -a "$1" /tmp/pkg-bad; f=/tmp/pkg-bad/tpan-linux-$2
    sha256sum "$f"
    python3 -c "import sys; p = sys.argv[1]; b = bytearray(open(p, \"rb\").read()); b[4096] ^= 0xFF; open(p, \"wb\").write(b)" "$f"
    sha256sum "$f"; cmp -l "$1/tpan-linux-$2" "$f" || true; grep " tpan-linux-$2\$" /tmp/pkg-bad/SHA256SUMS' _ "$VM_PKG" "$a"
  ok "bản sao gói: đúng một byte khác, SHA256SUMS giữ nguyên" bash -c '[ "$(grep -cE "^ *[0-9]+ +[0-7]+ +[0-7]+$" <<<"$1")" -eq 1 ] && [ "$(grep -c "tpan-linux" <<<"$1")" -eq 3 ]' _ "$OUT"
  install_run "$VM" exec bash -c "cd /tmp/pkg-bad && sudo ./install.sh --web nginx"
  ok "install.sh thoát khác 0" rc_not 0
  ok "báo sha256 không khớp, không cài" bash -c 'grep -qF "không khớp" <<<"$1" && grep -qF "Không cài." <<<"$1"' _ "$OUT"
  ok "dừng ở bước 'Kiểm sha256'" hasF 'Ở bước: Kiểm sha256'
  ok "không tới bước đặt file chạy hay setup" bash -c '! grep -qF "==> Đặt /usr/local/bin/tpan" <<<"$1" && ! grep -qF "tpan setup" <<<"$1"' _ "$OUT"
  quiet "$VM" root bash -c "$STATE_SCRIPT"
  ok "file đang cài, dịch vụ, cấu hình, mật khẩu: nguyên vẹn" test "$OUT" = "$before"
  [ "$OUT" = "$before" ] || diff <(printf '%s\n' "$before") <(printf '%s\n' "$OUT")
  bin_is_pkg "$PKG_A"
  vexec bash -c 'ls -la /usr/local/bin; systemctl is-active tpan'
  ok "không sót tpan.install-tmp; tpan.service active" bash -c '! grep -q install-tmp <<<"$1" && grep -qx active <<<"$1"' _ "$OUT"
  part_end 4
}

# ================================================================ 5

part_5() {
  part_begin "5: máy sạch lần hai, curl | sudo bash từ máy chủ HTTP loopback, --web apache"
  reset_vm
  run multipass transfer -r "$PKG_A" "$VMNAME:/home/ubuntu/"
  ok "chép gói $VER sang máy ảo" rc_is 0
  run multipass transfer -r "$PKG_B" "$VMNAME:$VM_PKG/"
  ok "chép gói $VER_B vào $VM_PKG/$VER_B" rc_is 0
  vexec bash -c 'cd "$1" && ls -lR . | sed "s/ [A-Z][a-z][a-z] .* / /"' _ "$VM_PKG"
  vroot systemd-run --unit=tpan-e2e-rel python3 -m http.server "$PORT" --bind 127.0.0.1 --directory "$VM_PKG"
  ok "máy chủ HTTP 127.0.0.1:$PORT chạy (systemd-run)" rc_is 0
  REL_UP=1
  sleep 2
  vexec bash -c 'for p in install.sh SHA256SUMS "$2/SHA256SUMS"; do curl -sS -o /dev/null -w "%{http_code} $p\n" "http://127.0.0.1:$1/$p"; done' _ "$PORT" "$VER_B"
  ok "máy chủ HTTP trả install.sh, SHA256SUMS, $VER_B/SHA256SUMS" bash -c '[ "$(grep -c "^200 " <<<"$1")" -eq 3 ]' _ "$OUT"
  vroot ss -tlnp
  ok "máy chủ HTTP chỉ nghe 127.0.0.1:$PORT" bash -c 'grep -q "127.0.0.1:$2 .*python3" <<<"$1" && ! grep -qE "(0\.0\.0\.0|\*|\[::\]):$2 " <<<"$1"' _ "$OUT" "$PORT"

  section "5: lệnh cài"
  local base="http://127.0.0.1:$PORT"
  install_run "$VM" exec bash -c "curl -fsSL $base/install.sh | sudo bash -s -- --from $base --web apache"
  ok "curl | sudo bash thoát 0" rc_is 0
  ok "tải từ $base" hasF "Tải $base/tpan-linux-$(arch)"
  ok "kiểm sha256 khớp" hasF "sha256 khớp: tpan-linux-$(arch)"
  ok "setup và stack install: hai job thành công" bash -c '[ "$(grep -c "^Job .*: thành công" <<<"$1")" -ge 2 ]' _ "$OUT"
  ok "mật khẩu quản trị in đúng một lần" test "$PW_HITS" -eq 1
  ok "có fingerprint" test -n "$FP"

  section "5: kiểm độc lập"
  bin_is_pkg "$PKG_A"
  vexec ls -la /usr/local/bin
  ok "không sót tpan.install-tmp" hasntF 'install-tmp'
  service_checks apache2
  vexec systemctl is-active nginx
  ok "nginx không chạy (máy LAMP)" hasnt '^active$'
  health_check
  ok "health: mọi phép kiểm pass, trừ tpan.backup:db warn (máy mới, chưa sao lưu tpan.db lần nào)" health_fresh_pass
  site_add "$S2"
  ok "site add $S2: thoát 0, job thành công" job_ok
  site_ok "$S2"
  part_end 5
}

# ================================================================ 6

part_6() {
  part_begin "6: tpan self-update từ máy chủ loopback lên $VER_B, rồi --rollback"
  [ -n "$VM_IP" ] || addrs
  local base="http://127.0.0.1:$PORT/$VER_B" sha_a
  vexec systemctl is-active tpan-e2e-rel
  if [ "$OUT" != active ]; then
    vroot systemd-run --unit=tpan-e2e-rel python3 -m http.server "$PORT" --bind 127.0.0.1 --directory "$VM_PKG"
    sleep 2
  fi
  REL_UP=1
  vexec curl -sS -o /dev/null -w '%{http_code}\n' "$base/SHA256SUMS"
  ok "máy chủ HTTP trả $VER_B/SHA256SUMS" out_is 200
  vroot sha256sum /usr/local/bin/tpan
  sha_a=$OUT

  section "6: --dry-run"
  vroot tpan self-update --url "$base" --dry-run
  ok "dry-run thoát 0" rc_is 0
  vroot sha256sum /usr/local/bin/tpan
  ok "dry-run không đổi file chạy" out_is "$sha_a"

  section "6: --yes"
  vexec systemctl show -p MainPID tpan
  local svc0=$OUT
  vroot tpan self-update --url "$base" --yes
  ok "self-update thoát 0, job thành công" job_ok
  ok "in $VER -> $VER_B" hasF "$VER -> $VER_B"
  bin_is_pkg "$PKG_B"
  vexec bash -c 'tpan version; ls -la /usr/local/bin; /usr/local/bin/tpan.old version'
  ok "tpan version là $VER_B, tpan.old là $VER" bash -c 'grep -q "^tpan $2 " <<<"$1" && grep -q "^tpan $3 " <<<"$1" && grep -q "tpan.old$" <<<"$1"' _ "$OUT" "$VER_B" "$VER"
  sleep 2
  vexec systemctl show -p MainPID -p ActiveState tpan
  ok "tpan.service active, khởi động lại (PID mới)" bash -c 'grep -q "ActiveState=active" <<<"$1" && [ "$(grep MainPID <<<"$1")" != "$2" ]' _ "$OUT" "$svc0"
  vroot bash -c 'p=$(systemctl show -p MainPID --value tpan); /proc/$p/exe version'
  ok "tiến trình của dịch vụ chạy bản $VER_B" has "^tpan $VER_B "
  vroot ss -tlnp
  ok "tpan nghe cổng 8443" has ':8443 .*"tpan"'
  site_ok "$S2"

  section "6: --rollback"
  vexec systemctl show -p MainPID tpan
  svc0=$OUT
  vroot tpan self-update --rollback --yes
  ok "rollback thoát 0, job thành công" job_ok
  vexec tpan version
  ok "về $VER" has "^tpan $VER "
  vroot sha256sum /usr/local/bin/tpan
  ok "file chạy giống hệt bản trước self-update" out_is "$sha_a"
  bin_is_pkg "$PKG_A"
  sleep 2
  vexec systemctl show -p MainPID -p ActiveState tpan
  ok "tpan.service active, khởi động lại (PID mới)" bash -c 'grep -q "ActiveState=active" <<<"$1" && [ "$(grep MainPID <<<"$1")" != "$2" ]' _ "$OUT" "$svc0"
  vroot bash -c 'p=$(systemctl show -p MainPID --value tpan); /proc/$p/exe version'
  ok "tiến trình của dịch vụ chạy bản $VER" has "^tpan $VER "
  health_check
  ok "health: mọi phép kiểm pass, trừ tpan.backup:db warn (máy mới, chưa sao lưu tpan.db lần nào)" health_fresh_pass
  vroot systemctl stop tpan-e2e-rel
  REL_UP=0
  part_end 6
}

case "${1:-all}" in
  all)
    part_0
    part_1
    part_2
    part_3
    part_4
    part_5
    part_6
    ;;
  0) part_0 ;;
  1) part_1 ;;
  2) part_2 ;;
  3) part_3 ;;
  4) part_4 ;;
  5) part_5 ;;
  6) part_6 ;;
  *)
    sed -n '2,15p' "$0" | sed 's/^# \{0,1\}//'
    exit 2
    ;;
esac

if [ -n "${E2E_LOG:-}" ] && [ -f "$E2E_LOG" ]; then
  section "bí mật, cookie, IP, tên user Mac có lọt vào chính file log không"
  n=0
  while IFS= read -r s; do
    [ -n "$s" ] || continue
    n=$((n + $(grep -cF -- "$s" "$E2E_LOG")))
  done <"$SECRETS"
  printf 'Số bí mật đã gặp (mật khẩu, cookie): %s; số dòng của log chứa một trong số đó: %s\n' "$(grep -c . "$SECRETS")" "$n"
  ok "log không chứa mật khẩu hay cookie nào" test "$n" -eq 0
  ok "log không chứa IP máy ảo" bash -c '[ -z "$1" ] || ! grep -qF -- "$1" "$2"' _ "$VM_IP" "$E2E_LOG"
  ok "log không chứa mạng Multipass" bash -c '[ -z "$1" ] || ! grep -qF -- "$1" "$2"' _ "$NET4" "$E2E_LOG"
  ok "log không chứa IPv6 của máy ảo (cả chữ hoa)" bash -c '[ -z "$1" ] || ! grep -qiF -- "$1" "$2"' _ "$V6PFX" "$E2E_LOG"
  ok "log không chứa IP của Mac" bash -c '[ -z "$1" ] || ! grep -qF -- "$1" "$2"' _ "$HOST_IP" "$E2E_LOG"
  ok "log không chứa thư mục nhà hay tên user Mac" bash -c '! grep -qF -- "$1" "$3" && ! grep -qF -- "$2" "$3"' _ "$HOME" "$USER" "$E2E_LOG"
fi

section "TỔNG KẾT: $PASSES khẳng định đạt, $FAILS khẳng định không đạt"
[ "$FAILS" -eq 0 ]
