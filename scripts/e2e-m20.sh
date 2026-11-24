#!/usr/bin/env bash
# e2e của mốc M20 (đăng nhập hai bước, token API, chứng chỉ panel; bước 23,
# mục 7.2 của docs/thay-doi/M20-thiet-ke.md đã sửa theo mục 11) trên máy ảo
# (tên trong file .tpan-vm; không có thì tpan-test). Chạy trên Mac, trong thư
# mục repo:
#
#   make e2e-m20                     (log vào tmp-phien/ rồi chép sang docs/bang-chung/M20.log)
#   E2E_LOG=tmp-phien/e2e-m20.log bash scripts/e2e-m20.sh > tmp-phien/e2e-m20.log 2>&1
#
# Hai lần dùng máy sạch: phần U (nâng cấp) trước, rồi máy sạch lần nữa cho mọi
# phần còn lại. Phần quét bí mật (J) chạy CUỐI, trên đúng máy có file khoá
# /etc/tpan/secret.key, nên quét được cả chính file log tới lúc đó bằng khoá
# mà khoá không bao giờ ra khỏi máy ảo.
#
# Bản tpan:
#   bản cũ  v0.2.0, gói đã phát hành ở dist/release/v0.2.0 (không build lại;
#           chưa có thì tải bằng gh release download)
#   bản A   bản của cây đang đứng (make release)
#
# Kịch bản (T<n> là khẳng định ở mục 7.2 của thiết kế):
#   0  đóng gói A; build đồ nghề trên Mac: trình duyệt (scripts/anh-readme),
#      qrpng và bangroute (scripts/e2e-m20/), mcpclient của M7 cho Linux.
#   U  (T80–T85) máy sạch, cài v0.2.0, tạo site, đăng nhập API giữ cookie; lên A
#      bằng `tpan self-update` của v0.2.0; cookie cũ còn dùng; đăng nhập mật
#      khẩu cũ 204; site 200; không có /etc/tpan/secret.key; health có cảnh báo
#      tpan.2fa:admins và không có [lỗi] mới; `tpan admin passwd` chạy.
#   A  (T1, T2) máy sạch, cài A bằng install.sh (--web nginx); đồ nghề oathtool,
#      zbar-tools, jq; user khach (user), admin2 (admin), thu_khoa (user); site
#      của admin và của khach; admin đăng nhập 204; GET /account/2fa tắt; health
#      cảnh báo tpan.2fa:admins.
#   B  (T21, T4, T3) admin2 rồi admin (chưa bật) đặt chính sách all → 409
#      mfa_enroll_self_first. admin setup: QR từ ma trận vẽ ra PNG, zbarimg đọc
#      ra đúng URI. oathtool (gói Ubuntu, cùng bí mật) cho mã được nhận khi bật;
#      ở bước hai: lệch ±90 giây bị từ chối, ±15 giây được nhận; câu trả lời sau
#      hai mã sai báo wrong_codes=2.
#   C  (T10–T15) mã vừa dùng → mfa_code_used; mã sai, mã bước −2 → mfa_code_wrong;
#      mã dự phòng → recovery_left 9; cùng mã dự phòng lần hai bị từ chối; mã dự
#      phòng chữ hoa không gạch được nhận; cookie phiếu không mở được GET /sites,
#      GET /overview, POST /sites (có Origin cùng origin), GET /account/tokens.
#   D  (T5) trình duyệt thật (-bo m20-that -buoc bat): admin2 bật hai bước ở
#      /account, đăng xuất, đăng nhập lại qua form: chặng hai, mã sai báo lỗi,
#      mã đúng vào; phiên API cũ của admin2 bị đăng xuất; health hết cảnh báo.
#   E  (T20) admin đặt chính sách all: khach đăng nhập 204 nhưng GET /sites 403
#      mfa_enroll_required, GET /account/2fa 200; trình duyệt (-buoc bat-buoc):
#      khach mở /sites bị đưa về /bat-hai-buoc, bật ngay ở đó, xong thì vào được.
#   F  (T30) lối thoát bằng CLI: `tpan user 2fa policy off`, `tpan user 2fa
#      reset khach --yes`; khach đăng nhập bằng mật khẩu 204; audit có
#      user.2fa_reset và auth.require_2fa với ip "cli".
#   G  (T40–T42) token: tạo qua API (thiếu mã → mfa_code_required; có mã → 201
#      no-store, hiện một lần) và qua CLI; bảng MỌI route × {hết hạn, thu hồi,
#      thiếu phạm vi, chỉ đọc trên route ghi, đủ phạm vi trên route chỉ phiên}
#      (giá trị giả) và {đủ phạm vi} (GET và hai POST xem trước, giá trị thật);
#      hạ admin2 xuống user → token system:read của họ GET /stack 403; khoá
#      admin2 → 401; token sai một ký tự → token_malformed.
#   H  (T50) pebble, /etc/hosts có panel.m20.test; trước khi đặt: s_client với
#      SNI thấy tự ký; `tpan panel domain set` → SNI thấy chứng chỉ pebble đúng
#      SAN, serial bằng file của lineage tpan-panel; vào bằng IP vẫn tự ký;
#      health tpan.panelcert đạt; `certbot renew --force-renewal` → serial mới
#      được phục vụ mà MainPID của tpan.service không đổi; tên trùng site →
#      panel_domain_is_site; site tên panel → domain_is_panel; Host là tên site
#      → 421 host_is_site (token được miễn, R12); `--clear` → về tự ký.
#   I  (T16, T17) thu_khoa bật hai bước; từ CHÍNH máy ảo tới IP của máy ảo (IP
#      nguồn khác IP Mac và 127.0.0.1): 4 mã sai 401, mã sai thứ 5 → 423 khoá IP;
#      phiếu bị xoá (đọc tpan.db); đúng mật khẩu từ IP đó vẫn 423; từ Mac vẫn vào.
#   J  (T60–T66) quét bí mật ĐÃ GẶP (bí mật TOTP base32 và hex, mã dự phòng có và
#      không có gạch, mọi token, file khoá hex và base64) trong: tpan.db và -wal,
#      journalctl, /var/log, sự kiện và bằng chứng mọi job (API), mọi câu trả lời
#      API đã lưu trừ câu trả lời lần đầu, output MCP (list_sites, list_jobs,
#      get_job), bản sao tpan.db do `tpan backup create --all` tạo, chính file
#      log này: phải là 0. Đối chứng: cùng phép quét trên các câu trả lời lần đầu
#      phải thấy bí mật (phép quét không mù).
#
# BÍ MẬT (mục 11.10): bí mật TOTP, mã, mã dự phòng, token KHÔNG BAO GIỜ nằm trên
# dòng lệnh hay trong log. Chúng đi qua file 0600 (thư mục tạm của script trên
# Mac, thư mục 0700 của user ubuntu trong máy ảo) và stdin: `curl -H @file`,
# `curl --data-binary @file`; thân JSON có bí mật dựng bằng jq --rawfile,
# --slurpfile hay printf (lệnh dựng sẵn của bash, không thành tiến trình). Mã
# TOTP tính TRONG máy ảo bằng oathtool (scripts/e2e-m20/ma.sh), script chỉ in số
# bước. Câu trả lời hiện bí mật một lần (setup, enable, recovery, tạo token)
# không in thân, chỉ in tên các khoá. Mọi bí mật vào danh sách che ngay khi có
# (trừ mã 6 số: không in ở đâu, và che chuỗi 6 số sẽ làm hỏng serial, cỡ file).
#
# ĐỒ NGHỀ TEST, CHỈ TRONG MÁY ẢO: nguồn tải loopback (unit tpan-e2e-http) cho
# self-update; pebble (unit tpan-e2e-pebble) và CA thử của nó; dòng /etc/hosts
# cho panel.m20.test; expires_at của một token ghi lùi bằng
# scripts/e2e-m20/db.py (chỗ ghi DB duy nhất).
#
# Đặt E2E_LOG=<file log> để cuối lần chạy script tự tìm trong chính file log
# đó mọi bí mật đã gặp, IP máy ảo, IP của Mac, thư mục nhà và tên user Mac
# (phải là 0 lần); phần J còn quét file log đó trong máy ảo bằng file khoá.
#
# Output: mỗi lệnh có dấu thời gian, output nguyên văn (đã che) và mã thoát;
# mỗi khẳng định in "=> ĐẠT" hoặc "=> KHÔNG ĐẠT"; cuối mỗi phần và cuối lần
# chạy in số đạt, không đạt. Không dùng set -e: nhiều lệnh được chờ là phải lỗi.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2
umask 077

VM=scripts/vm.sh
VMNAME="${TPAN_VM:-$(tr -d '[:space:]' 2>/dev/null <.tpan-vm)}"
VMNAME="${VMNAME:-tpan-test}"
FAILS=0
PASSES=0
OUT=""
RC=0
CODE=""

# ---------------------------------------------------------------- hằng
EMAIL=admin@example.com
SITE_A=a.example.com     # site của admin
SITE_K=khach.example.org # site của khach
SITE_UP=up.example.com   # site của phần nâng cấp
PANEL=panel.m20.test     # tên miền của panel (T50)
U_KHACH=khach            # user thường: chính sách bắt buộc (T20), lối thoát CLI (T30)
U_ADMIN2=admin2          # admin thứ hai: T21, bật bằng trình duyệt (T5), hạ vai (T41)
U_KHOA=thu_khoa          # user thường: khoá IP ở bước hai (T16, T17)
ALL_SCOPES=sites:write,databases:write,backups:write,cron:write,jobs:write,health:write,ssl:write,system:write
READ_SCOPES=sites:read,databases:read,backups:read,cron:read,jobs:read,health:read,ssl:read,system:read
OLD_VER=v0.2.0
OLD_DIR="${OLD_DIR:-dist/release/$OLD_VER}"
VER="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
STAGE=dist/e2e-m20
E=/home/ubuntu/e2e-m20 # thư mục đồ nghề trong máy ảo
BM=$E/bm               # bí mật trong máy ảo: 0700, của ubuntu
PORT=8020
SRC="http://127.0.0.1:$PORT"
HTTP_UNIT=tpan-e2e-http
ACME=https://localhost:14000/dir
PEBBLE_UNIT=tpan-e2e-pebble
PEBBLE_DIR=/etc/tpan-e2e-pebble
DEBUG_PORT=18173 # cổng DevTools riêng, không đụng script chụp của phiên khác
ARCH=""

TMPD="$(mktemp -d)"
chmod 700 "$TMPD"
SECRETS="$TMPD/secrets" # danh sách che (Mac)
SCAN="$TMPD/quet.txt"   # danh sách quét T60–T66, "<loại>:<giá trị>"
: >"$SECRETS"
: >"$SCAN"
mkdir -p "$TMPD/resp"
trap 'rm -rf "$TMPD"' EXIT
trap 'exit 130' INT TERM
ANH="$TMPD/anh"
QRPNG="$TMPD/qrpng"
BANGROUTE="$TMPD/bangroute"

ts() { date '+%Y-%m-%d %H:%M:%S'; }
section() { printf '\n\n######## [%s] %s\n' "$(ts)" "$*"; }

VM_IP=""
HOST_IP=""
NET4=""
BASE=""

# mask_str che một chuỗi (stdin) theo mọi thứ đã biết, in ra stdout.
mask_str() {
  local s p
  s="$(cat)"
  [ -n "$VM_IP" ] && s="${s//"$VM_IP"/<vm>}"
  [ -n "$HOST_IP" ] && s="${s//"$HOST_IP"/<mac>}"
  [ -n "$NET4" ] && s="${s//"$NET4"/<mạng máy ảo>.}"
  s="${s//"$TMPD"/<tmp>}"
  s="${s//"$HOME"/~}"
  s="${s//"$USER"/<user>}"
  while IFS= read -r p; do
    [ -n "$p" ] && s="${s//"$p"/<bí mật, đã che>}"
  done <"$SECRETS"
  s="$(sed -E 's/^(set-cookie: *[^=]*=)[^;]*/\1<cookie, đã che>/I' <<<"$s")"
  printf '%s' "$s"
}
mask() { OUT="$(mask_str <<<"$OUT")"; }

# Lớp bọc lệnh máy ảo (như e2e M15, M16): output ghi vào file trong máy ảo,
# ngắn thì in về trong cùng lần gọi, dài thì lấy về bằng multipass transfer, vì
# `multipass exec` trên Mac có lúc treo giữa chừng khi output dài.
CAP_MAX=4000
CAP_MERGED='f=$HOME/.e2e-cap; (umask 077; : >"$f"); "$@" >"$f" 2>&1; rc=$?; n=$(wc -c <"$f"); [ "$n" -le '"$CAP_MAX"' ] && cat "$f"; printf "\n__E2E_CAP rc=%s n=%s e=0\n" "$rc" "$n"'
CAP_SPLIT='f=$HOME/.e2e-cap; (umask 077; : >"$f"; : >"$f.err"); "$@" >"$f" 2>"$f.err"; rc=$?; printf "__E2E_CAP rc=%s n=%s e=%s\n" "$rc" "$(wc -c <"$f")" "$(wc -c <"$f.err")"'
cap_fetch() {
  local _
  for _ in 1 2 3; do
    multipass transfer "$VMNAME:$1" "$2" </dev/null >/dev/null 2>&1 && return 0
    sleep 1
  done
  return 1
}
vm_cap() {
  local in=$1 how=$2 line n
  shift 2
  [ "$how" = root ] && set -- sudo "$@"
  multipass exec "$VMNAME" -- bash -c "$CAP_MERGED" _ "$@" <"$in" >"$TMPD/cap.raw" 2>&1
  RC=$?
  line="$(tr -d '\r' <"$TMPD/cap.raw" | grep -a '^__E2E_CAP rc=' | tail -1)"
  if [ -z "$line" ]; then
    cp "$TMPD/cap.raw" "$TMPD/run.out"
    return
  fi
  RC="$(sed -E 's/.* rc=([0-9]+) .*/\1/' <<<"$line")"
  n="$(sed -E 's/.* n=([0-9]+) .*/\1/' <<<"$line")"
  if [ "$n" -le "$CAP_MAX" ]; then
    sed '$d' "$TMPD/cap.raw" >"$TMPD/run.out"
  elif ! cap_fetch /home/ubuntu/.e2e-cap "$TMPD/run.out"; then
    printf '(e2e: không lấy được output %s byte từ máy ảo bằng multipass transfer)\n' "$n" >"$TMPD/run.out"
    RC=254
  fi
}
vm_cap2() {
  local how=$1 line n e
  shift
  [ "$how" = root ] && set -- sudo "$@"
  : >"$TMPD/cap.out"
  : >"$TMPD/cap.err"
  multipass exec "$VMNAME" -- bash -c "$CAP_SPLIT" _ "$@" </dev/null >"$TMPD/cap.raw" 2>&1
  RC=$?
  line="$(tr -d '\r' <"$TMPD/cap.raw" | grep -a '^__E2E_CAP rc=' | tail -1)"
  if [ -z "$line" ]; then
    cp "$TMPD/cap.raw" "$TMPD/cap.err"
    return
  fi
  RC="$(sed -E 's/.* rc=([0-9]+) .*/\1/' <<<"$line")"
  n="$(sed -E 's/.* n=([0-9]+) .*/\1/' <<<"$line")"
  e="$(sed -E 's/.* e=([0-9]+).*/\1/' <<<"$line")"
  if [ "$n" -gt 0 ] && ! cap_fetch /home/ubuntu/.e2e-cap "$TMPD/cap.out"; then RC=254; fi
  if [ "$e" -gt 0 ] && ! cap_fetch /home/ubuntu/.e2e-cap.err "$TMPD/cap.err"; then RC=254; fi
}
runq() {
  RUNQ_CMD="$(mask_str <<<"$*")"
  RUNQ_TS="$(ts)"
  if [ "$1" = "$VM" ] && { [ "${2:-}" = exec ] || [ "${2:-}" = root ]; }; then
    shift
    vm_cap /dev/null "$@"
    return
  fi
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
bail() {
  printf '  => KHÔNG ĐẠT: DỪNG KỊCH BẢN: %s\n' "$*"
  FAILS=$((FAILS + 1))
  leak_check
  section "TỔNG KẾT: $PASSES khẳng định đạt, $FAILS khẳng định không đạt (dừng giữa chừng)"
  exit 1
}
note() { printf '  -- GHI NHẬN: %s\n' "$*"; }
P0=0
F0=0
part_begin() { P0=$PASSES F0=$FAILS; section "PHẦN $*"; }
part_end() { printf '\n==== Phần %s: %s đạt, %s không đạt\n' "$1" "$((PASSES - P0))" "$((FAILS - F0))"; }

rc_is() { [ "$RC" -eq "$1" ]; }
has() { grep -qE -- "$1" <<<"$OUT"; }
hasF() { grep -qF -- "$1" <<<"$OUT"; }
out_is() { [ "$OUT" = "$1" ]; }
count_is() { [ "$(grep -cE -- "$2" <<<"$OUT")" -eq "$1" ]; }
job_ok() { [ "$RC" = 0 ] && grep -q '^Job .*: thành công' <<<"$OUT"; }
jout() { jq -e "$@" <<<"$OUT" >/dev/null 2>&1; }
refused() { [ "$RC" != 0 ] && [ "$(jq -r '.error.code // empty' <<<"$OUT" 2>/dev/null)" = "$1" ]; }

vexec() { run "$VM" exec "$@"; }
vroot() { run "$VM" root "$@"; }

add_secret() {
  [ -n "${1:-}" ] || return 0
  grep -qxF -- "$1" "$SECRETS" || printf '%s\n' "$1" >>"$SECRETS"
}
# keep <loại> <giá trị>: một bí mật M20 (totp, ma, token): che, và đưa vào
# danh sách quét T60–T66. Mã dự phòng che cả dạng bỏ gạch.
keep() {
  [ -n "${2:-}" ] || return 0
  add_secret "$2"
  [ "$1" = ma ] && add_secret "${2//-/}"
  grep -qxF -- "$1:$2" "$SCAN" || printf '%s:%s\n' "$1" "$2" >>"$SCAN"
}
# keep_file <file "loại:giá trị"> (file -bi-mat-ra của chương trình trình duyệt).
keep_file() {
  local l
  [ -f "$1" ] || return 0
  while IFS= read -r l; do
    case $l in
    totp:*) keep totp "${l#totp:}" ;;
    ma:*) keep ma "${l#ma:}" ;;
    esac
  done <"$1"
}
save_pw() {
  add_secret "$2"
  (umask 077 && printf '%s' "$2" >"$TMPD/pw.$1")
}

# vm_put <file trên Mac> <đường dẫn trong máy ảo>: chép một file có bí mật vào
# máy ảo qua stdin (không qua dòng lệnh), quyền 0600, chủ ubuntu.
vm_put() {
  printf '\n[%s] $ (đưa %s vào máy ảo qua stdin: %s; không in nội dung)\n' "$(ts)" "$(mask_str <<<"${1##*/}")" "$2"
  vm_cap "$1" exec bash -c 'umask 077; cat >"$1" && stat -c "%a %U %s byte %n" "$1"' _ "$2"
  OUT="$(tr -d '\r' <"$TMPD/run.out")"
  mask
  printf '%s\n[%s] (mã thoát %s)\n' "$OUT" "$(ts)" "$RC"
}

slug_of() {
  local d=$1 head hash
  head="$(printf '%s' "${d:0:16}" | tr -c 'a-z0-9' '_')"
  hash="$(printf '%s' "$d" | shasum -a 256 | cut -c1-6)"
  printf '%s_%s' "$head" "$hash"
}
user_of() { printf 'tp_%s' "$(slug_of "$1")"; }
dbpw() {
  "$VM" root bash -c 'cd / && runuser -u "$2" -- wp config get DB_PASSWORD --path="/var/www/$1/public" 2>/dev/null' _ "$1" "$(user_of "$1")" </dev/null | tr -d '\r'
}

# sql <câu SELECT> [tham số...]: đọc tpan.db chỉ đọc (scripts/e2e-m20/db.py).
sql() { vroot python3 -I "$E/db.py" sql "$@"; }

# ---------------------------------------------------------------- máy ảo

addrs() {
  VM_IP="$("$VM" ip </dev/null)"
  NET4="${VM_IP%.*}."
  HOST_IP="$("$VM" exec bash -c 'echo "${SSH_CLIENT%% *}"' </dev/null | tr -d '\r')"
  BASE="https://$VM_IP:8443"
  printf 'IP máy ảo: <vm>; IP của Mac trong mạng Multipass: <mac> (đều được che trong log)\n'
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

# stage_vm [cu]: chép thư mục đồ nghề (gói A, script con, mcpclient) sang máy
# ảo, dựng thư mục bí mật 0700. "cu": chép thêm gói v0.2.0 và dựng nguồn tải
# loopback cho self-update.
stage_vm() {
  ARCH="$("$VM" exec dpkg --print-architecture </dev/null | tr -d '\r')"
  printf 'Kiến trúc máy ảo: %s\n' "$ARCH"
  run multipass transfer -r "$STAGE" "$VMNAME:/home/ubuntu/"
  ok "chép thư mục đồ nghề sang máy ảo ($E)" rc_is 0
  vexec bash -c 'mkdir -p "$1" && chmod 700 "$1" && chmod 755 "$2"/*.sh "$2"/mcpclient-* && stat -c "%a %U %n" "$1" && ls "$2"' _ "$BM" "$E"
  ok "thư mục bí mật $BM là 0700 của ubuntu" has "^700 ubuntu $BM\$"
  if [ "${1:-}" = cu ]; then
    run multipass transfer -r "$OLD_DIR" "$VMNAME:/home/ubuntu/"
    ok "chép gói $OLD_VER sang máy ảo" rc_is 0
    vroot systemd-run --unit "$HTTP_UNIT" --collect python3 -m http.server "$PORT" --bind 127.0.0.1 --directory "$E/rel"
    sleep 2
    vexec bash -c 'printf "a: SHA256SUMS %s, VERSION %s\n" "$(curl -s -o /dev/null -w "%{http_code}" "$1/a/SHA256SUMS")" "$(curl -s "$1/a/VERSION")"' _ "$SRC"
    ok "(đồ nghề) nguồn tải loopback trả gói A" hasF "SHA256SUMS 200"
  fi
}

# install_run <lệnh...>: một lần chạy install.sh; nhặt mật khẩu quản trị (che,
# đếm số lần in) trước khi in. Kết quả: RC, OUT, PW, PW_HITS.
PW=""
PW_HITS=0
install_run() {
  runq "$@"
  local pw
  pw="$(tr -d '\r' <"$TMPD/run.out" | grep -A1 '^Mật khẩu quản trị (chỉ hiện MỘT lần' | sed -nE 's/^    ([A-Z2-7]{26})$/\1/p' | head -1)"
  PW_HITS=0
  if [ -n "$pw" ]; then
    PW="$pw"
    add_secret "$pw"
    PW_HITS="$(grep -oF -- "$pw" "$TMPD/run.out" | wc -l | tr -d ' ')"
  fi
  show_last
  printf 'Mật khẩu quản trị panel trong output: %s lần\n' "$PW_HITS"
}

# site_add <domain> [cờ...]: tpan site add, nhặt mật khẩu WordPress và mật khẩu
# DB của site vào danh sách che trước khi in.
site_add() {
  local d=$1 pw
  shift
  runq "$VM" root tpan site add "$d" --email "$EMAIL" --yes "$@"
  pw="$(tr -d '\r' <"$TMPD/run.out" | sed -nE 's/^  Mật khẩu: +([A-Za-z0-9]{32})$/\1/p' | head -1)"
  add_secret "$pw"
  add_secret "$(dbpw "$d")"
  show_last
  ok "site add $d $*: thoát 0, job thành công" job_ok
}

# vjson <lệnh tpan... --json>: chạy bằng root, stdout (JSON) và stderr (tiến độ)
# đọc riêng; in stderr rồi stdout; OUT chỉ là JSON đã che (cho jout, refused).
vjson() {
  RUNQ_CMD="$(mask_str <<<"$VM root $*")"
  RUNQ_TS="$(ts)"
  vm_cap2 root "$@"
  cat "$TMPD/cap.err" "$TMPD/cap.out" >"$TMPD/run.out"
  show_last
  OUT="$(tr -d '\r' <"$TMPD/cap.out")"
  mask
}

# json_secret <tên> <biểu thức jq> <scripts/vm.sh> root <lệnh...>: như vjson,
# đọc mật khẩu theo biểu thức jq vào $TMPD/pw.<tên> (và danh sách che) TRƯỚC khi in.
JS_PW=""
json_secret() {
  local name=$1 expr=$2 pw
  shift 3 # bỏ tên, biểu thức, "$VM"
  RUNQ_CMD="$(mask_str <<<"$VM $*")"
  RUNQ_TS="$(ts)"
  shift # bỏ "root"
  vm_cap2 root "$@"
  pw="$(tr -d '\r' <"$TMPD/cap.out" | jq -r "$expr // empty" 2>/dev/null)"
  [ -n "$pw" ] && save_pw "$name" "$pw"
  cat "$TMPD/cap.err" "$TMPD/cap.out" >"$TMPD/run.out"
  show_last
  OUT="$(tr -d '\r' <"$TMPD/cap.out")"
  mask
  JS_PW="$pw"
}

# page <domain>: mã HTTP của trang chủ site, gọi từ trong máy ảo. Kết quả ở OUT.
page() { vexec curl -s -o /dev/null -m 20 -w '%{http_code}\n' --resolve "$1:80:127.0.0.1" "http://$1/"; }
is200() { [ "$OUT" = 200 ]; }

health_errs() {
  vroot tpan health
  grep -E '^\[lỗi\]' <<<"$OUT" | sort >"$1"
  printf 'Số dòng [lỗi] của tpan health: %s\n' "$(grep -c . "$1")"
}
new_errs() { comm -13 "$1" "$2"; }

# health_json: tpan health --json (stdout riêng), in tóm tắt; JSON ở $HJ.
HJ="$TMPD/health.json"
health_json() {
  printf '\n[%s] $ %s root tpan health --json\n' "$(ts)" "$VM"
  vm_cap2 root tpan health --json
  tr -d '\r' <"$TMPD/cap.out" >"$HJ"
  OUT="$(cat "$TMPD/cap.err")"
  mask
  [ -n "$OUT" ] && printf 'stderr:\n%s\n' "$OUT"
  if ! jq -e .groups "$HJ" >/dev/null 2>&1; then
    OUT="$(head -c 3000 "$HJ")"
    mask
    printf 'stdout không phải báo cáo JSON:\n%s\n[%s] (mã thoát %s)\n' "$OUT" "$(ts)" "$RC"
    return
  fi
  OUT="$(jq -r '"số phép kiểm theo trạng thái: \([.groups[].results[].status] | group_by(.) | map("\(.[0])=\(length)") | join(" "))",
    ([.groups[].results[] | select(.status != "pass" or (.id | startswith("tpan.")))] | .[] |
      "--- \(.status) \(.id): \(.summary.vi)")' "$HJ")"
  mask
  printf '%s\n[%s] (mã thoát %s)\n' "$OUT" "$(ts)" "$RC"
}
hq() { jq -e "$@" "$HJ" >/dev/null 2>&1; }
check_status() { hq --arg id "$1" --arg st "$2" '[.groups[].results[] | select(.id == $id and .status == $st)] | length == 1'; }
check_absent() { hq --arg id "$1" '[.groups[].results[] | select(.id == $id)] | length == 0'; }

# wait_panel: chờ panel trả lời ở cổng 8443 (tối đa 60 giây).
wait_panel() {
  local _
  for _ in $(seq 1 60); do
    [ "$(curl -sk -m 5 -o /dev/null -w '%{http_code}' "$BASE/api/v1/health" </dev/null 2>/dev/null)" = 200 ] && return 0
    sleep 1
  done
  return 1
}

# fetch_cert: chứng chỉ tự ký của panel lấy từ máy ảo (curl dùng --cacert, không -k).
fetch_cert() {
  "$VM" root cat /etc/tpan/tls/cert.pem </dev/null | tr -d '\r' >"$TMPD/cert.pem"
  local _
  for _ in $(seq 1 30); do
    curl -sS -m 5 --cacert "$TMPD/cert.pem" -o /dev/null "$BASE/api/v1/health" </dev/null 2>/dev/null && return 0
    sleep 1
  done
  return 1
}

# ---------------------------------------------------------------- API (từ Mac)

# body_args <thân>: rỗng; "@<file>" (có mật khẩu hay mã: không in); hay chuỗi
# JSON (in đã che). Đặt BODY_ARGS cho curl.
BODY_ARGS=()
body_args() {
  BODY_ARGS=()
  local b=${1-}
  [ -n "$b" ] || return 0
  if [ "${b:0:1}" = @ ]; then
    BODY_ARGS=(-H 'Content-Type: application/json' --data-binary "$b")
    printf '    thân gửi: (từ file %s, có mật khẩu hay mã: không in)\n' "$(mask_str <<<"${b##*/}")"
  else
    printf '%s' "$b" >"$TMPD/req.json"
    BODY_ARGS=(-H 'Content-Type: application/json' --data-binary "@$TMPD/req.json")
    printf '    thân gửi: %s\n' "$(mask_str <<<"$b")"
  fi
}

# harvest <jar|"">: mọi bí mật trong thân vừa nhận (mật khẩu, bí mật TOTP, URI,
# token, mã dự phòng) và giá trị cookie vào danh sách che TRƯỚC khi in.
harvest() {
  local s
  while IFS= read -r s; do add_secret "$s"; done < <(jq -r '[.. | objects | (.password?, .db_password?, .uri?) | strings] | .[]' "$TMPD/body" 2>/dev/null)
  # Chỉ đúng dạng mới vào danh sách quét theo loại; chuỗi khác cùng tên khoá thì chỉ che.
  while IFS= read -r s; do
    if [[ $s =~ ^[A-Z2-7]{32}$ ]]; then keep totp "$s"; else add_secret "$s"; fi
  done < <(jq -r '[.. | objects | .secret? | strings] | .[]' "$TMPD/body" 2>/dev/null)
  while IFS= read -r s; do
    if [[ $s == tpan_* ]]; then keep token "$s"; else add_secret "$s"; fi
  done < <(jq -r '[.. | objects | .token? | strings] | .[]' "$TMPD/body" 2>/dev/null)
  while IFS= read -r s; do keep ma "$s"; done < <(jq -r '[.. | objects | .recovery_codes? | arrays | .[] | strings] | .[]' "$TMPD/body" 2>/dev/null)
  [ -n "$1" ] && [ -f "$1" ] && while IFS= read -r s; do add_secret "$s"; done < <(awk -F'\t' 'NF == 7 && ($1 !~ /^#/ || $1 ~ /^#HttpOnly_/) { print $7 }' "$1")
}

# api_done <jar|"">: sau một lần curl: mã HTTP, nhặt bí mật, lưu thân (cho T64),
# in. LAN=1: câu trả lời hiện bí mật MỘT lần: không in thân, chỉ in tên khoá
# và Cache-Control; lưu riêng (T64 không quét nó, phần đối chứng quét nó).
LAN=0
RESP_N=0
api_done() {
  CODE="$(sed -nE 's/^-> HTTP ([0-9]{3})$/\1/p' "$TMPD/run.out" | tail -1)"
  harvest "$1"
  RESP_N=$((RESP_N + 1))
  if [ "$LAN" = 1 ]; then
    cp "$TMPD/body" "$TMPD/resp/$(printf %04d "$RESP_N").lan-dau.json"
  else
    cp "$TMPD/body" "$TMPD/resp/$(printf %04d "$RESP_N").json"
  fi
  OUT="$(tr -d '\r' <"$TMPD/run.out")"
  mask
  printf '%s\n' "$OUT"
  if [ "$LAN" = 1 ]; then
    printf '    thân nhận: (hiện MỘT lần, có bí mật: không in) khoá: %s; %s\n' \
      "$(jq -c 'if type == "object" then keys else type end' "$TMPD/body" 2>/dev/null)" \
      "$(tr -d '\r' <"$TMPD/hdr" | grep -i '^cache-control:' | head -1)"
    LAN=0
    return
  fi
  if jq -e . "$TMPD/body" >/dev/null 2>&1; then
    OUT="$(jq -c . "$TMPD/body" | head -c 700)"
  else
    OUT="$(head -c 400 "$TMPD/body")"
  fi
  mask
  [ -n "$OUT" ] && printf '    thân nhận (tối đa 700 byte): %s\n' "$OUT"
}

# api <jar> <METHOD> <đường dẫn> [thân]: gọi panel qua TLS (--cacert, không
# -k), cookie ở jar. Mọi request mang Origin và Sec-Fetch-Site cùng origin như
# trình duyệt (thứ được kiểm là luật của route, không phải chặn CSRF).
api() {
  local jar=$1 m=$2 p=$3 body=${4-}
  : >"$TMPD/body"
  : >"$TMPD/hdr"
  printf '\n[%s] $ curl -X %s <panel>%s   (phiên: %s)\n' "$(ts)" "$m" "$p" "${jar##*/}"
  body_args "$body"
  curl -sS -m 60 --cacert "$TMPD/cert.pem" -b "$jar" -c "$jar" -X "$m" -o "$TMPD/body" -D "$TMPD/hdr" \
    -w '-> HTTP %{http_code}\n' -H 'Accept: application/json' -H 'Sec-Fetch-Site: same-origin' -H "Origin: $BASE" \
    ${BODY_ARGS[@]+"${BODY_ARGS[@]}"} "$BASE$p" </dev/null >"$TMPD/run.out" 2>&1
  RC=$?
  api_done "$jar"
}
# api1: như api, cho câu trả lời hiện bí mật một lần (setup, enable, recovery, tạo token).
api1() {
  LAN=1
  api "$@"
}
# tapi <file header token> <METHOD> <đường dẫn> [thân]: gọi bằng token (header
# đọc từ file 0600: token không nằm trên dòng lệnh), không cookie, không Origin
# (như công cụ dòng lệnh).
tapi() {
  local h=$1 m=$2 p=$3 body=${4-}
  : >"$TMPD/body"
  : >"$TMPD/hdr"
  printf '\n[%s] $ curl -X %s <panel>%s   (token: header từ file %s)\n' "$(ts)" "$m" "$p" "${h##*/}"
  body_args "$body"
  curl -sS -m 60 --cacert "$TMPD/cert.pem" -X "$m" -o "$TMPD/body" -D "$TMPD/hdr" -w '-> HTTP %{http_code}\n' \
    -H 'Accept: application/json' -H @"$h" ${BODY_ARGS[@]+"${BODY_ARGS[@]}"} "$BASE$p" </dev/null >"$TMPD/run.out" 2>&1
  RC=$?
  api_done ""
}
code_is() { [ "$CODE" = "$1" ]; }
ecode() { jq -r '.error.code // empty' "$TMPD/body" 2>/dev/null; }
expect() { [ "$CODE" = "$1" ] && [ "$(ecode)" = "$2" ]; }
bq() { jq -e "$@" "$TMPD/body" >/dev/null 2>&1; }
jb() { jq -r "$1" "$TMPD/body" 2>/dev/null; }
no_store() { tr -d '\r' <"$TMPD/hdr" | grep -qi '^cache-control: *no-store'; }
# not_token_denied: không phải 401, 403 và error.code không phải token_*.
not_token_denied() { [ "$CODE" != 401 ] && [ "$CODE" != 403 ] && [[ $(ecode) != token_* ]]; }

new_jar() {
  : >"$TMPD/jar.$1"
  chmod 600 "$TMPD/jar.$1"
  printf '%s' "$TMPD/jar.$1"
}
# jar_cookies <jar>: "<số cookie phiếu> <số cookie phiên>".
jar_cookies() { awk -F'\t' '$6 == "__Host-tpan-2fa" { a++ } $6 == "__Host-tpan" { b++ } END { print a + 0, b + 0 }' "$1"; }

# login <jar> <tên> <tên mật khẩu (file $TMPD/pw.<tên>)>: POST /api/v1/login.
login() {
  local body
  body="$(jq -nc --arg n "$2" --rawfile p "$TMPD/pw.$3" '{name: $n, password: $p}')"
  api "$1" POST /api/v1/login "$body"
}

# ---------------------------------------------------------------- mã TOTP (trong máy ảo)

# vcode <user> <lệch giây> [bước tối thiểu]: mã TOTP của user tính TRONG máy ảo
# (ma.sh, oathtool) vào $TMPD/code.json ({"code":…}); chỉ in số bước. Không có
# bước tối thiểu thì là bước lớn hơn bước đã được nhận gần nhất của user (máy
# chủ không nhận lại một bước đã dùng): ma.sh tự chờ tới bước đó.
STEP=""
vcode() {
  local u=$1 off=$2 min=${3-} v="LAST_$1"
  [ -n "$min" ] || min=$((${!v:-0} + 1))
  rm -f "$TMPD/code.json"
  vexec bash "$E/ma.sh" dung "$BM/totp.$u" "$off" "$min" "$BM/code.json"
  STEP="$(sed -nE 's/^bước ([0-9]+) .*/\1/p' <<<"$OUT")"
  cap_fetch "$BM/code.json" "$TMPD/code.json" || printf '(e2e: không lấy được file mã từ máy ảo)\n'
}
# accepted <user>: máy chủ vừa nhận mã của bước $STEP.
accepted() { printf -v "LAST_$1" '%s' "$STEP"; }
# vwrong <user>: một mã chắc chắn sai vào $TMPD/code.json.
vwrong() {
  rm -f "$TMPD/code.json"
  vexec bash "$E/ma.sh" sai "$BM/totp.$1" "$BM/code.json"
  cap_fetch "$BM/code.json" "$TMPD/code.json" || printf '(e2e: không lấy được file mã từ máy ảo)\n'
}
# body_code <file ra> <mã dự phòng>: {"code":…} bằng printf (lệnh dựng sẵn, không thành tiến trình).
body_code() { printf '{"code":"%s"}' "$2" >"$1"; }
# body_pw_code <file ra> <tên mật khẩu>: {password, code} từ file mật khẩu và $TMPD/code.json.
body_pw_code() { jq -nc --rawfile p "$TMPD/pw.$2" --slurpfile c "$TMPD/code.json" '{password: $p, code: $c[0].code}' >"$1"; }

# setup_2fa <user> <jar>: POST /account/2fa/setup (một lần), giữ câu trả lời ở
# $TMPD/setup.<user>.json, bí mật ở $TMPD/totp.<user> và $BM/totp.<user>.
setup_2fa() {
  local u=$1 jar=$2
  api1 "$jar" POST /api/v1/account/2fa/setup '{}'
  cp "$TMPD/body" "$TMPD/setup.$u.json"
  jq -j '.secret // empty' "$TMPD/body" >"$TMPD/totp.$u"
  vm_put "$TMPD/totp.$u" "$BM/totp.$u"
}
# enable_2fa <user> <jar>: mã của bước kế tiếp + mật khẩu → POST /account/2fa/enable
# (một lần); mã dự phòng vào $TMPD/ma.<user>.
enable_2fa() {
  local u=$1 jar=$2
  vcode "$u" 0
  body_pw_code "$TMPD/req.enable" "$u"
  api1 "$jar" POST /api/v1/account/2fa/enable "@$TMPD/req.enable"
  [ "$CODE" = 200 ] && accepted "$u"
  jq -r '.recovery_codes[]?' "$TMPD/body" >"$TMPD/ma.$u" 2>/dev/null
}

# cli_token <tên> <user> <phạm vi>: `tpan token create` (in token MỘT lần, JSON);
# token vào danh sách che và quét, header "Authorization: Bearer …" vào
# $TMPD/tok.<tên>.h và $BM/tok.<tên>.h; id vào biến TOKID_<tên, '-' thành '_'>.
cli_token() {
  local n=$1 u=$2 sc=$3 tok id good=0
  RUNQ_CMD="$VM root tpan token create $u --name e2e-$n --scope $sc --days 1 --json"
  RUNQ_TS="$(ts)"
  vm_cap2 root tpan token create "$u" --name "e2e-$n" --scope "$sc" --days 1 --json
  tok="$(tr -d '\r' <"$TMPD/cap.out" | jq -r '.token // empty' 2>/dev/null)"
  id="$(tr -d '\r' <"$TMPD/cap.out" | jq -r '.row.id // .id // empty' 2>/dev/null)"
  keep token "$tok"
  cat "$TMPD/cap.err" "$TMPD/cap.out" >"$TMPD/run.out"
  show_last
  [ "$RC" = 0 ] && [[ $tok == tpan_* ]] && [ ${#tok} -eq 44 ] && [ -n "$id" ] && good=1
  ok "tpan token create $u --scope $sc: thoát 0, in token 44 ký tự tpan_… một lần (đã che), có id" test "$good" = 1
  printf 'Authorization: Bearer %s\n' "$tok" >"$TMPD/tok.$n.h"
  printf -v "TOKID_${n//-/_}" '%s' "$id"
  vm_put "$TMPD/tok.$n.h" "$BM/tok.$n.h"
}

# ---------------------------------------------------------------- trình duyệt

# browser <mô tả> <file mật khẩu của -ten hay ""> <cờ của anh-readme...>: chạy
# chương trình trình duyệt; mỗi dòng "=> ĐẠT"/"=> KHÔNG ĐẠT" nó in được tính vào
# tổng. Mật khẩu đi qua biến môi trường, không vào dòng lệnh.
browser() {
  local desc=$1 upw=$2 up=""
  shift 2
  [ -n "$upw" ] && up="$(cat "$upw")"
  printf '\n[%s] $ anh-readme %s   (Chrome không giao diện trên Mac, panel <panel>)\n' "$(ts)" "$(mask_str <<<"$*")"
  TPAN_PANEL_PASSWORD="$(cat "$TMPD/pw.admin")" TPAN_USER_PASSWORD="$up" \
    "$ANH" -panel "$BASE" -debug-port "$DEBUG_PORT" -out "$TMPD/anh-out" "$@" </dev/null >"$TMPD/br.out" 2>&1
  RC=$?
  local p f
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

# ================================================================ PHẦN 0

part_0() {
  part_begin "0: đóng gói bản A, build đồ nghề"
  run git log --oneline -1
  run git status --short
  if [ ! -f "$OLD_DIR/SHA256SUMS" ]; then
    run gh release download "$OLD_VER" --repo doxuta/tpan --dir "$OLD_DIR"
    ok "tải gói $OLD_VER từ trang phát hành" rc_is 0
  fi
  run bash -c 'cd "$1" && ls -l && shasum -a 256 -c SHA256SUMS' _ "$OLD_DIR"
  ok "gói $OLD_VER đã phát hành: đủ file, sha256 khớp SHA256SUMS" rc_is 0
  run make release VERSION="$VER" DATE="$BUILD_DATE"
  ok "make release (bản A, $VER)" rc_is 0
  [ "$RC" -eq 0 ] || bail "không đóng gói được bản A"
  run go build -o "$ANH" ./scripts/anh-readme
  ok "build chương trình trình duyệt (scripts/anh-readme)" rc_is 0
  run go build -o "$QRPNG" ./scripts/e2e-m20/qrpng
  ok "build qrpng (vẽ ma trận QR ra PNG, scripts/e2e-m20/qrpng)" rc_is 0
  run go build -o "$BANGROUTE" ./scripts/e2e-m20/bangroute
  ok "build bangroute (bảng route của server, scripts/e2e-m20/bangroute)" rc_is 0
  # shellcheck disable=SC2016
  run bash -c '
    set -e
    st=$1; a=$2
    rm -rf "$st"
    mkdir -p "$st/rel/a"
    cp "dist/release/$a/"* "$st/rel/a/"
    cp scripts/e2e-m20/*.sh scripts/e2e-m20/db.py "$st/"
    for arch in arm64 amd64; do
      CGO_ENABLED=0 GOOS=linux GOARCH=$arch go build -o "$st/mcpclient-$arch" ./scripts/e2e-m7/mcpclient
    done
    ls -lR "$st"
  ' _ "$STAGE" "$VER"
  ok "dựng thư mục đồ nghề $STAGE (gói A, script con, db.py, mcpclient cho Linux)" rc_is 0
  [ "$RC" -eq 0 ] || bail "không dựng được thư mục đồ nghề"
  part_end 0
}

# ================================================================ PHẦN U (T80–T85)

part_u() {
  part_begin "U (T80–T85): máy sạch, cài $OLD_VER, nâng lên bản A bằng self-update của $OLD_VER"
  reset_vm
  stage_vm cu
  install_run "$VM" exec bash -c "cd /home/ubuntu/$OLD_VER && sudo bash ./install.sh --web nginx"
  ok "install.sh $OLD_VER thoát 0" rc_is 0
  [ "$RC" -eq 0 ] || bail "không cài được $OLD_VER"
  save_pw admin "$PW"
  vexec tpan version
  ok "tpan version là $OLD_VER" grep -q "^tpan $OLD_VER " <<<"$OUT"
  wait_panel
  fetch_cert
  ok "panel $OLD_VER trả lời qua TLS bằng chứng chỉ của nó" rc_is 0
  site_add "$SITE_UP"
  page "$SITE_UP"
  ok "$SITE_UP trả 200 trên $OLD_VER" is200
  local jar_old jar_new h0="$TMPD/health.u0" h1="$TMPD/health.u1" moi
  jar_old="$(new_jar u-cu)"
  login "$jar_old" admin admin
  ok "T80: đăng nhập API trên $OLD_VER: 204, giữ cookie" code_is 204
  health_errs "$h0"

  vroot tpan self-update --yes --url "$SRC/a"
  ok "T80: tpan self-update của $OLD_VER lên bản A: thoát 0, job thành công" job_ok
  ok "T80: in $OLD_VER -> $VER" hasF "$OLD_VER -> $VER"
  wait_panel
  fetch_cert
  ok "panel bản A trả lời qua TLS bằng chứng chỉ tự ký cũ (không đổi chứng chỉ)" rc_is 0
  vexec tpan version
  ok "sau khi nâng: tpan version là bản A ($VER)" grep -q "^tpan $VER " <<<"$OUT"

  api "$jar_old" GET /api/v1/session
  ok "T81: cookie của $OLD_VER vẫn là phiên đăng nhập (logged_in, admin, không mfa_enroll_required)" bq '.logged_in == true and .user.name == "admin" and ((.mfa_enroll_required // false) == false)'
  api "$jar_old" GET /api/v1/sites
  ok "T81: cookie cũ gọi GET /sites: 200" code_is 200
  jar_new="$(new_jar u-moi)"
  login "$jar_new" admin admin
  ok "T82: đăng nhập bằng mật khẩu cũ trên bản A: 204 (không có chặng hai)" code_is 204
  api "$jar_new" GET /api/v1/account/2fa
  ok "T82: hai bước mặc định tắt, chính sách off" bq '.enabled == false and .policy == "off"'
  page "$SITE_UP"
  ok "T84: $SITE_UP vẫn 200 sau khi nâng" is200
  vroot bash -c 'ls -la /etc/tpan/; if [ -e /etc/tpan/secret.key ]; then echo "CÓ secret.key"; else echo "không có /etc/tpan/secret.key"; fi'
  ok "T85: không có /etc/tpan/secret.key (chỉ tạo khi có người bật hai bước)" hasF "không có /etc/tpan/secret.key"
  health_json
  ok "T85: health có cảnh báo vàng tpan.2fa:admins" check_status tpan.2fa:admins warn
  health_errs "$h1"
  moi="$(new_errs "$h0" "$h1")"
  [ -n "$moi" ] && printf '[lỗi] mới so với trước khi nâng:\n%s\n' "$moi"
  ok "T85: tpan health sau khi nâng không có [lỗi] mới" test -z "$moi"

  json_secret admin '.admin_password' "$VM" root tpan admin passwd --yes --json
  ok "T83: tpan admin passwd chạy trên bản A: thoát 0, in mật khẩu mới (đã che)" bash -c '[ "$1" = 0 ] && [ -n "$2" ]' _ "$RC" "$JS_PW"
  api "$jar_old" GET /api/v1/session
  ok "T83: admin passwd huỷ phiên cũ (cookie cũ không còn logged_in)" bq '.logged_in == false'
  jar_new="$(new_jar u-moi2)"
  login "$jar_new" admin admin
  ok "T83: mật khẩu mới đăng nhập được: 204" code_is 204
  vroot systemctl stop "$HTTP_UNIT"
  part_end U
}

# ================================================================ PHẦN A (T1, T2)

H0=""
JAR_ADM=""
part_a() {
  part_begin "A (T1, T2): máy sạch, cài bản A, đồ nghề, user, site"
  reset_vm
  stage_vm
  install_run "$VM" exec bash -c "cd $E/rel/a && sudo bash ./install.sh --web nginx"
  ok "install.sh của bản A thoát 0" rc_is 0
  [ "$RC" -eq 0 ] || bail "không cài được bản A"
  ok "mật khẩu quản trị in đúng một lần (giữ ngoài log)" test "$PW_HITS" -eq 1
  save_pw admin "$PW"
  vexec tpan version
  ok "tpan version là bản A ($VER)" grep -q "^tpan $VER " <<<"$OUT"
  vexec systemctl is-active tpan nginx php8.3-fpm mariadb
  ok "tpan.service và ba dịch vụ của stack đều active" count_is 4 '^active$'
  vroot env DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=180 install -y -q oathtool zbar-tools jq
  ok "(đồ nghề) cài oathtool, zbar-tools, jq từ kho Ubuntu" rc_is 0
  vexec bash -c 'oathtool --version | head -1; zbarimg --version; jq --version'
  wait_panel
  fetch_cert
  ok "T1: panel trả lời qua TLS ở cổng 8443" rc_is 0
  json_secret khach '.password' "$VM" root tpan user add "$U_KHACH" --full-name "Khach M20" --email khach@example.com --yes --json
  ok "user add $U_KHACH (vai user): thoát 0, mật khẩu sinh một lần (đã che)" bash -c '[ "$1" = 0 ] && [ -n "$2" ]' _ "$RC" "$JS_PW"
  json_secret admin2 '.password' "$VM" root tpan user add "$U_ADMIN2" --role admin --full-name "Admin Hai" --email admin2@example.com --yes --json
  ok "user add $U_ADMIN2 (vai admin)" bash -c '[ "$1" = 0 ] && [ -n "$2" ]' _ "$RC" "$JS_PW"
  json_secret thu_khoa '.password' "$VM" root tpan user add "$U_KHOA" --full-name "Thu Khoa" --email khoa@example.com --yes --json
  ok "user add $U_KHOA (vai user)" bash -c '[ "$1" = 0 ] && [ -n "$2" ]' _ "$RC" "$JS_PW"
  site_add "$SITE_A"
  site_add "$SITE_K" --owner "$U_KHACH"
  page "$SITE_A"
  ok "$SITE_A trả 200" is200
  H0="$TMPD/health.0"
  health_errs "$H0"

  JAR_ADM="$(new_jar adm)"
  login "$JAR_ADM" admin admin
  ok "T1: admin đăng nhập bằng API khi chưa ai bật hai bước: 204" code_is 204
  ok "T1: jar có cookie phiên, không có cookie phiếu" test "$(jar_cookies "$JAR_ADM")" = "0 1"
  api "$JAR_ADM" GET /api/v1/account/2fa
  ok "T2: GET /account/2fa: enabled=false, pending=false, policy off" bq '.enabled == false and .pending == false and .policy == "off"'
  health_json
  ok "T2: health có cảnh báo tpan.2fa:admins (admin chưa bật hai bước)" check_status tpan.2fa:admins warn
  part_end A
}

# ================================================================ PHẦN B (T21, T4, T3)

part_b() {
  part_begin "B (T21, T4, T3): chính sách khi chưa bật; QR; oathtool và đồng hồ"
  local jar_a2 jar
  jar_a2="$(new_jar a2)"
  login "$jar_a2" "$U_ADMIN2" admin2
  ok "$U_ADMIN2 đăng nhập: 204" code_is 204
  api "$jar_a2" POST /api/v1/settings/auth '{"require_2fa":"all"}'
  ok "T21: $U_ADMIN2 (admin chưa bật hai bước) đặt chính sách all: 409 mfa_enroll_self_first" expect 409 mfa_enroll_self_first
  api "$JAR_ADM" POST /api/v1/settings/auth '{"require_2fa":"admin"}'
  ok "T21: admin (chưa bật) đặt chính sách admin: 409 mfa_enroll_self_first" expect 409 mfa_enroll_self_first
  api "$JAR_ADM" GET /api/v1/settings/auth
  ok "T21: chính sách vẫn off" bq '.require_2fa == "off"'

  setup_2fa admin "$JAR_ADM"
  ok "setup: 200" code_is 200
  ok "setup: Cache-Control: no-store" no_store
  ok "setup: bí mật 32 ký tự base32, URI otpauth://totp/TPAN:admin@…, issuer TPAN, ma trận QR vuông" \
    bq '(.secret | test("^[A-Z2-7]{32}$")) and (.uri | startswith("otpauth://totp/TPAN:admin")) and .issuer == "TPAN" and (.qr | length) >= 21 and ((.qr | length) as $n | all(.qr[]; length == $n))'
  api "$JAR_ADM" GET /api/v1/account/2fa
  ok "sau setup: pending=true, enabled=false" bq '.pending == true and .enabled == false'

  # T4: ma trận → PNG (Mac) → zbarimg (máy ảo) → so với uri ngay trong máy ảo.
  jq -j '.uri // empty' "$TMPD/setup.admin.json" >"$TMPD/uri.admin"
  run "$QRPNG" -vao "$TMPD/setup.admin.json" -ra "$TMPD/qr.png"
  ok "T4: qrpng vẽ ma trận của setup ra PNG" rc_is 0
  run multipass transfer "$TMPD/qr.png" "$VMNAME:$BM/qr.png"
  vm_put "$TMPD/uri.admin" "$BM/uri.admin"
  # shellcheck disable=SC2016
  vexec bash -c 'z=$(zbarimg --raw -q "$1/qr.png" | tr -d "\n"); u=$(cat "$1/uri.admin"); if [ -n "$z" ] && [ "$z" = "$u" ]; then echo "zbarimg đọc ra đúng uri của setup (nhãn ${u%%\?*}; phần còn lại có bí mật, không in)"; else echo "KHÁC: zbarimg ra ${#z} ký tự, nhãn ${z%%\?*}"; exit 1; fi' _ "$BM"
  ok "T4: zbarimg đọc PNG ra đúng URI otpauth:// mà setup trả" hasF "đúng uri"

  # T3: oathtool (gói Ubuntu, độc lập với tpan) với cùng bí mật cho mã máy chủ nhận.
  enable_2fa admin "$JAR_ADM"
  ok "T3: mã oathtool tính trong máy ảo (bước $STEP) được máy chủ nhận khi bật: 200" code_is 200
  ok "T3: enable trả 10 mã dự phòng dạng xxxx-xxxx-xxxx-xxxx, Cache-Control: no-store" \
    bash -c '[ "$(grep -cE "^[a-z2-7]{4}(-[a-z2-7]{4}){3}$" "$1")" -eq 10 ]' _ "$TMPD/ma.admin"
  ok "T3: enable có Cache-Control: no-store" no_store
  api "$JAR_ADM" GET /api/v1/account/2fa
  ok "sau khi bật: enabled=true, recovery_left=10, phiên đang dùng vẫn sống" bq '.enabled == true and .recovery_left == 10 and .pending == false'
  vroot stat -c '%a %U %s %F %n' /etc/tpan/secret.key
  ok "file khoá /etc/tpan/secret.key: 600 root, 32 byte, file thường (tạo lúc setup đầu tiên)" hasF "600 root 32 regular file /etc/tpan/secret.key"

  section "T3: lệch đồng hồ ở bước hai: ±90 giây bị từ chối, ±15 giây được nhận"
  jar="$(new_jar t3a)"
  login "$jar" admin admin
  ok "admin (đã bật) đăng nhập bằng mật khẩu: 200 mfa_required, có cookie phiếu, chưa có phiên" bash -c '[ "$1" = 200 ] && [ "$2" = "1 0" ]' _ "$CODE" "$(jar_cookies "$jar")"
  ok "thân của bước một là {mfa_required:true}" bq '.mfa_required == true'
  vcode admin -90 0
  api "$jar" POST /api/v1/login/2fa "@$TMPD/code.json"
  ok "T3: mã của giờ máy −90 giây (bước $STEP): 401 mfa_code_wrong" expect 401 mfa_code_wrong
  vcode admin 90 0
  api "$jar" POST /api/v1/login/2fa "@$TMPD/code.json"
  ok "T3: mã của giờ máy +90 giây (bước $STEP): 401 mfa_code_wrong" expect 401 mfa_code_wrong
  vcode admin -15
  api "$jar" POST /api/v1/login/2fa "@$TMPD/code.json"
  ok "T3: mã của giờ máy −15 giây (bước $STEP): được nhận" bash -c '[ "$1" = 200 ] || [ "$1" = 204 ]' _ "$CODE"
  [ "$CODE" = 200 ] || [ "$CODE" = 204 ] && accepted admin
  ok "R5: vào sau hai mã sai thì 200 {wrong_codes: 2}" bash -c '[ "$1" = 200 ]' _ "$CODE"
  ok "R5: wrong_codes = 2" bq '.wrong_codes == 2'
  ok "vào xong: jar có cookie phiên, cookie phiếu đã xoá" test "$(jar_cookies "$jar")" = "0 1"
  jar="$(new_jar t3b)"
  login "$jar" admin admin
  vcode admin 15
  api "$jar" POST /api/v1/login/2fa "@$TMPD/code.json"
  ok "T3: mã của giờ máy +15 giây (bước $STEP): 204 (không còn lần sai nào để báo)" code_is 204
  [ "$CODE" = 204 ] && accepted admin
  api "$jar" GET /api/v1/session
  ok "phiên mới sau bước hai: logged_in, admin" bq '.logged_in == true and .user.name == "admin"'
  part_end B
}

# ================================================================ PHẦN C (T10–T15)

part_c() {
  part_begin "C (T10–T15): mã sai, mã cũ, mã đã dùng, mã dự phòng, cookie phiếu"
  local jar rc1 rc2
  jar="$(new_jar c1)"
  login "$jar" admin admin
  vcode admin 0
  api "$jar" POST /api/v1/login/2fa "@$TMPD/code.json"
  ok "mã đúng (bước $STEP): 204" code_is 204
  [ "$CODE" = 204 ] && accepted admin
  cp "$TMPD/code.json" "$TMPD/code.used.json"

  jar="$(new_jar c2)"
  login "$jar" admin admin
  api "$jar" POST /api/v1/login/2fa "@$TMPD/code.used.json"
  ok "T12: mã vừa dùng (bước $STEP) trên phiếu mới: 401 mfa_code_used" expect 401 mfa_code_used
  vwrong admin
  api "$jar" POST /api/v1/login/2fa "@$TMPD/code.json"
  ok "T10: mã sai: 401 mfa_code_wrong" expect 401 mfa_code_wrong
  vcode admin -60 0
  api "$jar" POST /api/v1/login/2fa "@$TMPD/code.json"
  ok "T11: mã của bước −2 (bước $STEP, giờ máy −60 giây): 401 mfa_code_wrong" expect 401 mfa_code_wrong
  rc1="$(sed -n 1p "$TMPD/ma.admin")"
  body_code "$TMPD/req.rc" "$rc1"
  api "$jar" POST /api/v1/login/2fa "@$TMPD/req.rc"
  ok "T13: mã dự phòng thứ nhất (có gạch) trên cùng phiếu: 200" code_is 200
  ok "T13: recovery_left = 9, wrong_codes = 2 (mã đã dùng không tính là đoán sai)" bq '.recovery_left == 9 and .wrong_codes == 2'
  api "$jar" GET /api/v1/session
  ok "T13: vào bằng mã dự phòng: logged_in" bq '.logged_in == true'

  jar="$(new_jar c3)"
  login "$jar" admin admin
  api "$jar" POST /api/v1/login/2fa "@$TMPD/req.rc"
  ok "T14: cùng mã dự phòng lần hai: 401 mfa_code_wrong" expect 401 mfa_code_wrong
  rc2="$(sed -n 2p "$TMPD/ma.admin" | tr -d '-' | tr 'a-z' 'A-Z')"
  add_secret "$rc2"
  body_code "$TMPD/req.rc" "$rc2"
  api "$jar" POST /api/v1/login/2fa "@$TMPD/req.rc"
  ok "mã dự phòng thứ hai, chữ hoa, bỏ gạch: được nhận, recovery_left = 8, wrong_codes = 1" bq '.recovery_left == 8 and .wrong_codes == 1'
  vroot python3 -I "$E/db.py" sql "SELECT action, target, result FROM audit WHERE action LIKE 'login%' ORDER BY id DESC LIMIT 8"
  ok "nhật ký có login ok_recovery và login.2fa wrong_code" bash -c 'grep -q "ok_recovery" <<<"$1" && grep -q "login.2fa.*wrong_code" <<<"$1"' _ "$OUT"

  section "T15: cookie phiếu không phải phiên"
  jar="$(new_jar phieu)"
  login "$jar" admin admin
  ok "T15: bước một xong: jar chỉ có cookie phiếu (1 phiếu, 0 phiên)" test "$(jar_cookies "$jar")" = "1 0"
  api "$jar" GET /api/v1/session
  ok "T15: GET /session: logged_in=false, mfa_pending=true" bq '.logged_in == false and .mfa_pending == true'
  api "$jar" GET /api/v1/sites
  ok "T15: cookie phiếu GET /sites: 401" code_is 401
  api "$jar" GET /api/v1/overview
  ok "T15: cookie phiếu GET /overview: 401" code_is 401
  api "$jar" POST /api/v1/sites '{"domain":"phieu.example.com","email":"admin@example.com"}'
  ok "T15: cookie phiếu POST /sites (Origin, Sec-Fetch-Site cùng origin): 401, không phải 403 csrf" code_is 401
  api "$jar" GET /api/v1/account/tokens
  ok "T15: cookie phiếu GET /account/tokens: 401" code_is 401
  api "$jar" POST /api/v1/logout
  ok "đăng xuất ở chặng hai: 204" code_is 204
  api "$jar" GET /api/v1/session
  ok "sau đăng xuất: không còn mfa_pending" bq '.logged_in == false and ((.mfa_pending // false) == false)'
  part_end C
}

# ================================================================ PHẦN D (T5)

part_d() {
  part_begin "D (T5): trình duyệt thật: $U_ADMIN2 bật hai bước ở /account, đăng nhập lại qua chặng hai"
  local jar_a2
  jar_a2="$(new_jar a2b)"
  login "$jar_a2" "$U_ADMIN2" admin2
  ok "$U_ADMIN2 đăng nhập API trước khi bật (để xem phiên này có bị đăng xuất): 204" code_is 204
  browser "T5" "$TMPD/pw.admin2" -bo m20-that -buoc bat -ten "$U_ADMIN2" -bi-mat-ra "$TMPD/bm.admin2"
  keep_file "$TMPD/bm.admin2"
  ok "chương trình trình duyệt ghi ra bí mật và 10 mã dự phòng (cho phần quét J)" \
    bash -c '[ "$(grep -c "^totp:" "$1")" -eq 1 ] && [ "$(grep -c "^ma:" "$1")" -eq 10 ]' _ "$TMPD/bm.admin2"
  api "$jar_a2" GET /api/v1/session
  ok "bật xong: phiên API khác của $U_ADMIN2 bị đăng xuất" bq '.logged_in == false'
  health_json
  ok "mọi admin đã bật hai bước: tpan.2fa:admins đạt" check_status tpan.2fa:admins pass
  ok "tpan.secretkey đạt" check_status tpan.secretkey pass
  part_end D
}

# ================================================================ PHẦN E (T20)

part_e() {
  part_begin "E (T20): chính sách bắt buộc"
  local jar_k
  api "$JAR_ADM" POST /api/v1/settings/auth '{"require_2fa":"all"}'
  ok "T20: admin (đã bật) đặt chính sách all: 200" bash -c '[ "$1" = 200 ]' _ "$CODE"
  ok "T20: require_2fa = all" bq '.require_2fa == "all"'
  jar_k="$(new_jar k)"
  login "$jar_k" "$U_KHACH" khach
  ok "T20: $U_KHACH (chưa bật) đăng nhập bằng mật khẩu: 204" code_is 204
  api "$jar_k" GET /api/v1/session
  ok "T20: phiên của $U_KHACH báo mfa_enroll_required" bq '.logged_in == true and .mfa_enroll_required == true'
  api "$jar_k" GET /api/v1/sites
  ok "T20: $U_KHACH GET /sites: 403 mfa_enroll_required" expect 403 mfa_enroll_required
  api "$jar_k" GET /api/v1/account/2fa
  ok "T20: $U_KHACH GET /account/2fa: 200, required=true" bq '.required == true and .enabled == false'
  ok "T20: GET /account/2fa trả 200" code_is 200
  browser "T20" "$TMPD/pw.khach" -bo m20-that -buoc bat-buoc -ten "$U_KHACH" -bi-mat-ra "$TMPD/bm.khach"
  keep_file "$TMPD/bm.khach"
  api "$jar_k" GET /api/v1/session
  ok "T20: bật xong trong trình duyệt: phiên API cũ của $U_KHACH bị đăng xuất" bq '.logged_in == false'
  part_end E
}

# ================================================================ PHẦN F (T30)

part_f() {
  part_begin "F (T30): lối thoát bằng CLI: chính sách off, đặt lại hai bước của $U_KHACH"
  local jar_k
  vroot tpan user 2fa policy off
  ok "T30: tpan user 2fa policy off: thoát 0" rc_is 0
  vroot tpan user 2fa policy
  ok "T30: tpan user 2fa policy in chính sách đang có: off" hasF "off"
  vroot tpan user 2fa status
  ok "tpan user 2fa status: thoát 0" rc_is 0
  vroot tpan user 2fa reset "$U_KHACH" --yes
  ok "T30: tpan user 2fa reset $U_KHACH --yes: thoát 0" rc_is 0
  vjson tpan user 2fa status "$U_KHACH" --json
  ok "T30: status --json: $U_KHACH không còn hai bước" jout '[.. | objects | select(has("enabled"))] | length > 0 and all(.enabled == false)'
  jar_k="$(new_jar k2)"
  login "$jar_k" "$U_KHACH" khach
  ok "T30: $U_KHACH đăng nhập lại bằng mật khẩu (chính sách off, đã đặt lại): 204" code_is 204
  sql "SELECT ip || ' ' || action || ' ' || target FROM audit WHERE action IN ('user.2fa_reset', 'auth.require_2fa') ORDER BY id"
  ok "T30: audit có user.2fa_reset $U_KHACH với ip cli" has "^cli user\.2fa_reset $U_KHACH\$"
  ok "T30: audit có auth.require_2fa với ip cli" has '^cli auth\.require_2fa'
  part_end F
}

# ================================================================ PHẦN G (T40–T42)

part_g() {
  part_begin "G (T40–T42): token API"
  local tok_bad job good=0
  section "tạo token qua API: đòi mật khẩu và mã, hiện một lần"
  jq -nc --rawfile p "$TMPD/pw.admin" '{name: "e2e-api", scopes: ["sites:read"], days: 1, password: $p}' >"$TMPD/req.tok"
  api "$JAR_ADM" POST /api/v1/account/tokens "@$TMPD/req.tok"
  ok "tạo token khi đã bật hai bước mà không có mã: 400 mfa_code_required" expect 400 mfa_code_required
  vcode admin 0
  jq -nc --rawfile p "$TMPD/pw.admin" --slurpfile c "$TMPD/code.json" '{name: "e2e-api", scopes: ["sites:read"], days: 1, password: $p, code: $c[0].code}' >"$TMPD/req.tok"
  api1 "$JAR_ADM" POST /api/v1/account/tokens "@$TMPD/req.tok"
  [ "$CODE" = 201 ] && accepted admin
  ok "tạo token có mã (bước $STEP): 201" code_is 201
  ok "tạo token: Cache-Control: no-store" no_store
  ok "tạo token: token tpan_… 44 ký tự, row có hint, không có token" bq '(.token | test("^tpan_[a-z2-7]{39}$")) and (.row.hint | startswith("tpan_")) and (.row | has("token") | not)'
  jq -r '.token // empty' "$TMPD/body" | sed 's/^/Authorization: Bearer /' >"$TMPD/tok.api.h"
  api "$JAR_ADM" GET /api/v1/account/tokens
  ok "GET /account/tokens: có token e2e-api, có max_days và groups, không lộ token" bq '([.tokens[] | select(.name == "e2e-api" and .status == "active")] | length == 1) and .max_days > 0 and (.groups | index("system")) != null'
  tapi "$TMPD/tok.api.h" GET /api/v1/sites
  ok "token tạo qua API (sites:read) GET /sites: 200" code_is 200

  section "tạo token qua CLI (in một lần)"
  cli_token du admin "$ALL_SCOPES"
  cli_token doc admin "$READ_SCOPES"
  cli_token thieu-a admin cron:read
  cli_token thieu-b admin jobs:read
  cli_token het-han admin "$ALL_SCOPES"
  cli_token thu-hoi admin "$ALL_SCOPES"
  cli_token a2 "$U_ADMIN2" system:read
  vroot python3 -I "$E/db.py" hethan "$TOKID_het_han"
  ok "(đồ nghề) expires_at của token het-han ghi lùi về một giờ trước" rc_is 0
  vroot tpan token revoke "$TOKID_thu_hoi" --yes
  ok "tpan token revoke: thoát 0" rc_is 0
  vjson tpan token list --json
  ok "tpan token list --json: có token expired và revoked" bash -c 'grep -q "expired" <<<"$1" && grep -q "revoked" <<<"$1"' _ "$OUT"
  ok "tpan token list --json: output thô (trước khi che) không có token nào, chỉ hint" bash -c '! grep -qE "tpan_[a-z2-7]{39}" "$1"' _ "$TMPD/cap.out"

  section "T40: bảng mọi route × trạng thái token"
  sql "SELECT id FROM jobs WHERE kind = 'site.add' AND status = 'succeeded' ORDER BY started_at LIMIT 1"
  job="$(head -1 <<<"$OUT")"
  run bash -c '"$1" -khong-co >"$2" && wc -l <"$2"' _ "$BANGROUTE" "$TMPD/route-gia.tsv"
  ok "bangroute -khong-co chạy được" rc_is 0
  run bash -c '"$1" -domain "$3" -job "$4" -user "$5" -token "$6" >"$2" && cut -f1,2,3 "$2"' _ "$BANGROUTE" "$TMPD/route-that.tsv" "$SITE_A" "$job" "$U_KHACH" "$TOKID_thu_hoi"
  ok "T40: bảng route của server có ít nhất 110 route API không công khai" bash -c '[ "$(grep -c . "$1")" -ge 110 ]' _ "$TMPD/route-that.tsv"
  vm_put "$TMPD/route-gia.tsv" "$BM/route-gia.tsv"
  vm_put "$TMPD/route-that.tsv" "$BM/route-that.tsv"
  vexec bash "$E/bang-token.sh" "$BM/route-gia.tsv" "$BM/route-that.tsv" "$BM" "$BM/t40"
  ok "T40: mọi route × {hết hạn 401 token_expired, thu hồi 401 token_revoked, chỉ phiên 403 token_forbidden, thiếu phạm vi 403 token_scope, chỉ đọc trên route ghi 403 token_scope}, đủ phạm vi không 401/403" has 'TỔNG: [0-9]+ request, sai=0$'
  ok "T40: không có dòng SAI" count_is 0 '^SAI '

  section "T41: token theo vai hiện tại của chủ"
  tapi "$TMPD/tok.a2.h" GET /api/v1/stack
  ok "T41: token system:read của $U_ADMIN2 (đang là admin) GET /stack: 200" code_is 200
  vroot tpan user edit "$U_ADMIN2" --role user --yes
  ok "T41: hạ $U_ADMIN2 xuống vai user: thoát 0" rc_is 0
  tapi "$TMPD/tok.a2.h" GET /api/v1/stack
  ok "T41: cùng token GET /stack sau khi hạ vai: 403 (luật admin của route, không phải lỗi token)" bash -c '[ "$1" = 403 ] && [[ $2 != token_* ]]' _ "$CODE" "$(ecode)"
  vroot tpan user suspend "$U_ADMIN2" --yes
  ok "T41: khoá $U_ADMIN2: thoát 0" rc_is 0
  tapi "$TMPD/tok.a2.h" GET /api/v1/stack
  ok "T41: token của user đang khoá: 401 token_invalid" expect 401 token_invalid

  section "T42: token sai dạng"
  tok_bad="$(sed -n 's/^Authorization: Bearer //p' "$TMPD/tok.du.h")"
  if [ "${tok_bad:20:1}" = a ]; then tok_bad="${tok_bad:0:20}b${tok_bad:21}"; else tok_bad="${tok_bad:0:20}a${tok_bad:21}"; fi
  add_secret "$tok_bad"
  printf 'Authorization: Bearer %s\n' "$tok_bad" >"$TMPD/tok.hong.h"
  tapi "$TMPD/tok.hong.h" GET /api/v1/sites
  ok "T42: token đổi một ký tự: 401 token_malformed (bắt bằng phần kiểm, không hỏi DB)" expect 401 token_malformed
  sed 's/Bearer /Token /' "$TMPD/tok.du.h" >"$TMPD/tok.khong-bearer.h"
  tapi "$TMPD/tok.khong-bearer.h" GET /api/v1/sites
  ok "header Authorization thiếu chữ Bearer: 401 token_invalid (có Authorization thì không dùng cookie)" expect 401 token_invalid
  tapi "$TMPD/tok.du.h" GET /api/v1/sites
  ok "token đủ phạm vi của admin GET /sites: 200" code_is 200
  [ "$CODE" = 200 ] && good=1
  ok "token đủ phạm vi đã dùng được trước khi quét (để T63 có dữ liệu)" test "$good" = 1
  part_end G
}

# ================================================================ PHẦN H (T50)

# HOST_SCRIPT <tên> <đường dẫn> [file header token]: gọi panel trong máy ảo
# bằng Host là <tên> (--resolve về 127.0.0.1). In mã HTTP, kiểu nội dung, error.code.
# shellcheck disable=SC2016
HOST_SCRIPT='d=$1 p=$2 h=${3-}; f=$(mktemp); a=(-sk -m 10 --resolve "$d:8443:127.0.0.1" -o "$f" -w "%{http_code} %{content_type}")
[ -n "$h" ] && a+=(-H @"$h")
r=$(curl "${a[@]}" "https://$d:8443$p"); ec=$(jq -r ".error.code // \"-\"" "$f" 2>/dev/null); echo "https://$d:8443$p -> HTTP $r error.code=${ec:--}"; rm -f "$f"'
cert_field() { sed -nE "s/^$1: .*$2=([^ ]*).*/\\1/p" <<<"$OUT" | head -1; }

part_h() {
  part_begin "H (T50): chứng chỉ panel được tin (pebble)"
  local pid0 pid1 s_self s_lin s_old i
  vroot bash "$E/pebble.sh" "$PEBBLE_DIR" "$PEBBLE_UNIT"
  ok "(đồ nghề) dựng pebble: /dir trả 200" hasF "pebble /dir: HTTP 200"
  vroot bash -c 'echo "127.0.0.1 $1" >>/etc/hosts && getent ahostsv4 "$1" | head -1' _ "$PANEL"
  ok "(đồ nghề) $PANEL -> 127.0.0.1 trong /etc/hosts" has '^127\.0\.0\.1 '
  vroot tpan ssl settings --email "$EMAIL" --server "$ACME"
  ok "ssl settings: email và máy chủ ACME thử (ssl.server có giá trị nên không cần tắt staging, 11.4)" rc_is 0

  vroot bash "$E/chung-chi.sh" "$PANEL"
  s_self="$(cert_field tu-ky-tren-dia serial)"
  ok "T50: trước khi đặt tên: SNI $PANEL được phục vụ chứng chỉ tự ký (serial bằng /etc/tpan/tls/cert.pem)" \
    bash -c '[ -n "$2" ] && grep -q "^phuc-vu: serial=$2 tu-ky=co " <<<"$1"' _ "$OUT" "$s_self"
  vexec systemctl show -p MainPID --value tpan
  pid0="$OUT"

  vroot tpan panel domain set "$PANEL" --yes
  ok "T50: tpan panel domain set $PANEL --yes: thoát 0, job thành công" job_ok
  vjson tpan panel domain show --json
  ok "T50: panel domain show: $PANEL" jout --arg d "$PANEL" '.domain == $d'
  for i in $(seq 1 15); do
    vroot bash "$E/chung-chi.sh" "$PANEL"
    s_lin="$(cert_field lineage-tpan-panel serial)"
    grep -q "^phuc-vu: serial=$s_lin tu-ky=khong " <<<"$OUT" && break
    sleep 1
  done
  ok "T50: SNI $PANEL: chứng chỉ do pebble ký (không tự ký), serial bằng fullchain của lineage tpan-panel (lần đọc $i)" \
    bash -c '[ -n "$2" ] && grep -q "^phuc-vu: serial=$2 tu-ky=khong issuer=[^ ]*Pebble" <<<"$1"' _ "$OUT" "$s_lin"
  ok "T50: SAN của chứng chỉ phục vụ và của lineage là DNS:$PANEL" bash -c 'grep -q "^phuc-vu: .* san=DNS:$2$" <<<"$1" && grep -q "^lineage-tpan-panel: .* san=DNS:$2$" <<<"$1"' _ "$OUT" "$PANEL"
  vroot bash "$E/chung-chi.sh" -
  ok "T50: vào bằng IP (không SNI) vẫn là chứng chỉ tự ký (B9)" bash -c 'grep -q "^phuc-vu: serial=$2 tu-ky=co " <<<"$1"' _ "$OUT" "$s_self"
  fetch_cert
  ok "T50: từ Mac vào bằng IP, tin đúng chứng chỉ tự ký cũ (--cacert): được" rc_is 0
  health_json
  ok "T50: health tpan.panelcert:$PANEL đạt" check_status "tpan.panelcert:$PANEL" pass
  ok "T50: tóm tắt nói số ngày còn hạn" hq --arg id "tpan.panelcert:$PANEL" '[.groups[].results[] | select(.id == $id)][0].summary.vi | test("ngày")'

  section "T50: gia hạn ép: serial mới được phục vụ, tpan.service không khởi động lại"
  s_old="$s_lin"
  vroot certbot renew --cert-name tpan-panel --force-renewal --no-random-sleep-on-renew --server "$ACME"
  ok "T50: certbot renew --cert-name tpan-panel --force-renewal: thoát 0" rc_is 0
  for i in $(seq 1 20); do
    vroot bash "$E/chung-chi.sh" "$PANEL"
    s_lin="$(cert_field lineage-tpan-panel serial)"
    [ "$s_lin" != "$s_old" ] && grep -q "^phuc-vu: serial=$s_lin " <<<"$OUT" && break
    sleep 1
  done
  ok "T50: serial mới của lineage khác serial cũ và được panel phục vụ (lần đọc $i)" \
    bash -c '[ -n "$2" ] && [ "$2" != "$3" ] && grep -q "^phuc-vu: serial=$2 " <<<"$1"' _ "$OUT" "$s_lin" "$s_old"
  vexec systemctl show -p MainPID --value tpan
  pid1="$OUT"
  ok "T50: MainPID của tpan.service không đổi ($pid0)" bash -c '[ -n "$1" ] && [ "$1" != 0 ] && [ "$1" = "$2" ]' _ "$pid0" "$pid1"

  section "T50: tên panel và tên site không giành nhau"
  api "$JAR_ADM" POST "/api/v1/panel/domain?dry_run=1" "{\"domain\":\"$SITE_A\"}"
  ok "T50: đặt tên panel trùng site $SITE_A: 409 panel_domain_is_site" expect 409 panel_domain_is_site
  vjson tpan site add "$PANEL" --email "$EMAIL" --dry-run --json
  ok "T50: CLI tạo site tên $PANEL (--dry-run): từ chối domain_is_panel" refused domain_is_panel
  api "$JAR_ADM" POST "/api/v1/sites?dry_run=1" "{\"domain\":\"$PANEL\",\"email\":\"$EMAIL\"}"
  ok "T50: API tạo site tên $PANEL (dry_run): 409 domain_is_panel" expect 409 domain_is_panel

  section "11.2, R2, R12: Host là tên một site → 421"
  vexec bash -c "$HOST_SCRIPT" _ "$SITE_A" /api/v1/session
  ok "Host $SITE_A, /api/v1/session: 421 host_is_site (JSON)" has 'HTTP 421 application/json.* error\.code=host_is_site'
  vexec bash -c "$HOST_SCRIPT" _ "$SITE_K" /
  ok "Host $SITE_K (site của user), đường dẫn giao diện: 421, trang HTML" has 'HTTP 421 text/html'
  vexec bash -c "$HOST_SCRIPT" _ "$SITE_A" /api/v1/sites "$BM/tok.du.h"
  ok "R12: cùng Host nhưng có Authorization (token): được miễn 421, 200" has 'HTTP 200 '
  vexec bash -c "$HOST_SCRIPT" _ "$PANEL" /api/v1/session
  ok "Host là $PANEL: không 421 (200)" has 'HTTP 200 '

  section "T50: xoá tên panel thì về tự ký"
  vroot tpan panel domain set --clear --yes
  ok "T50: tpan panel domain set --clear --yes: thoát 0, job thành công" job_ok
  for i in $(seq 1 15); do
    vroot bash "$E/chung-chi.sh" "$PANEL"
    grep -q "^phuc-vu: serial=$s_self tu-ky=co " <<<"$OUT" && break
    sleep 1
  done
  ok "T50: sau --clear, SNI $PANEL lại là chứng chỉ tự ký (lần đọc $i)" bash -c 'grep -q "^phuc-vu: serial=$2 tu-ky=co " <<<"$1"' _ "$OUT" "$s_self"
  ok "T50: lineage tpan-panel đã bị xoá (certbot delete)" hasF "lineage-tpan-panel: không có"
  health_json
  ok "T50: health không còn phép kiểm tpan.panelcert:$PANEL" check_absent "tpan.panelcert:$PANEL"
  part_end H
}

# ================================================================ PHẦN I (T16, T17)

part_i() {
  part_begin "I (T16, T17): khoá IP ở bước hai, phiếu quá 5 lần sai bị xoá"
  local jar
  jar="$(new_jar kh)"
  login "$jar" "$U_KHOA" thu_khoa
  ok "$U_KHOA đăng nhập: 204" code_is 204
  setup_2fa "$U_KHOA" "$jar"
  ok "$U_KHOA setup: 200" code_is 200
  enable_2fa "$U_KHOA" "$jar"
  ok "$U_KHOA bật hai bước bằng mã oathtool (bước $STEP): 200" code_is 200
  vexec bash "$E/ma.sh" sai "$BM/totp.$U_KHOA" "$BM/ma-sai.json"
  vm_put "$TMPD/pw.thu_khoa" "$BM/pw.thu_khoa"
  vexec bash "$E/khoa-ip.sh" "$BASE" "$U_KHOA" "$BM/pw.thu_khoa" "$BM"
  ok "bước một từ IP của máy ảo: 200 mfa_required, có cookie phiếu" bash -c 'grep -q "^đăng nhập bằng mật khẩu: HTTP 200 -$" <<<"$1" && grep -q "^cookie phiếu trong jar: 1$" <<<"$1"' _ "$OUT"
  ok "mã sai lần 1 tới 4: 401 mfa_code_wrong" count_is 4 '^mã sai lần [1-4]: HTTP 401 mfa_code_wrong$'
  ok "T17: mã sai lần 5: 423 login_locked kèm Retry-After (IP bị khoá)" has '^mã sai lần 5: HTTP 423 login_locked \(Retry-After [0-9]+\)$'
  ok "T16: cookie phiếu sau lần sai thứ 5: không còn mfa_pending" has '^thân: \{"logged_in":false,"mfa_pending":null\}$|^thân: \{"logged_in":false,"mfa_pending":false\}$'
  ok "T17: đúng mật khẩu từ IP đang khoá: 423 login_locked" has '^đăng nhập lại bằng ĐÚNG mật khẩu từ IP đang khoá: HTTP 423 login_locked'
  sql "SELECT COUNT(*) FROM login_challenges WHERE user_name = ?" "$U_KHOA"
  ok "T16: phiếu quá 5 lần sai đã bị xoá khỏi tpan.db (0 phiếu của $U_KHOA)" out_is 0
  jar="$(new_jar kh2)"
  login "$jar" "$U_KHOA" thu_khoa
  ok "T17: từ Mac (IP khác) $U_KHOA vẫn qua bước một: 200 mfa_required (khoá IP không khoá đường chính)" bash -c '[ "$1" = 200 ]' _ "$CODE"
  api "$jar" POST /api/v1/logout
  part_end I
}

# ================================================================ PHẦN J (T60–T66)

part_j() {
  part_begin "J (T60–T66): bí mật không nằm ở đâu ngoài câu trả lời lần đầu"
  local snap f
  vroot tpan backup create --all --yes
  ok "tpan backup create --all --yes: thoát 0" rc_is 0
  vroot bash -c 'ls -1t /var/lib/tpan/backups/_tpan/tpan-*.db | head -1'
  snap="$OUT"
  ok "có bản sao tpan.db vừa tạo" has '^/var/lib/tpan/backups/_tpan/tpan-[0-9]{8}-[0-9]{6}\.db$'
  vroot bash "$E/gom.sh" "$BM/tok.du.h" "$E/mcpclient-$ARCH" "$BM/gom"
  ok "gom sự kiện, bằng chứng mọi job qua API và output MCP: có job, mcpclient thoát 0" bash -c 'grep -q "^job: [1-9]" <<<"$1" && grep -q "^mcpclient: mã thoát 0" <<<"$1"' _ "$OUT"
  ok "MCP: có list_sites, list_jobs, get_job, không lời gọi nào isError" bash -c 'grep -q "list_sites" <<<"$1" && grep -q "list_jobs" <<<"$1" && grep -q "get_job" <<<"$1" && grep -q "có isError: 0;" <<<"$1" && ! grep -q "LỖI GIAO THỨC" <<<"$1"' _ "$OUT"

  # Câu trả lời API đã lưu: tách lần đầu (đối chứng) và phần còn lại (T64).
  mkdir -p "$TMPD/resp-quet" "$TMPD/resp-lan-dau"
  for f in "$TMPD"/resp/*.json; do
    case $f in
    *.lan-dau.json) cp "$f" "$TMPD/resp-lan-dau/" ;;
    *) cp "$f" "$TMPD/resp-quet/" ;;
    esac
  done
  printf 'câu trả lời API đã lưu: %s; trong đó hiện bí mật lần đầu: %s\n' "$(find "$TMPD/resp" -type f | wc -l | tr -d ' ')" "$(find "$TMPD/resp-lan-dau" -type f | wc -l | tr -d ' ')"
  run multipass transfer -r "$TMPD/resp-quet" "$VMNAME:$BM/"
  run multipass transfer -r "$TMPD/resp-lan-dau" "$VMNAME:$BM/"
  if [ -n "${E2E_LOG:-}" ] && [ -f "$E2E_LOG" ]; then
    cp "$E2E_LOG" "$TMPD/e2e.log"
    run multipass transfer "$TMPD/e2e.log" "$VMNAME:$BM/e2e.log"
  else
    note "không có E2E_LOG: phần quét chính file log trong máy ảo (bằng khoá) bỏ qua"
    printf 'không có E2E_LOG\n' >"$TMPD/e2e.log"
    run multipass transfer "$TMPD/e2e.log" "$VMNAME:$BM/e2e.log"
  fi
  printf 'danh sách quét: %s bí mật TOTP, %s mã dự phòng, %s token\n' "$(grep -c '^totp:' "$SCAN")" "$(grep -c '^ma:' "$SCAN")" "$(grep -c '^token:' "$SCAN")"
  ok "danh sách quét có đủ: 4 bí mật TOTP (admin, admin2, khach, thu_khoa), 40 mã dự phòng, 8 token (1 qua API, 7 qua CLI)" \
    bash -c '[ "$(grep -c "^totp:" "$1")" -eq 4 ] && [ "$(grep -c "^ma:" "$1")" -eq 40 ] && [ "$(grep -c "^token:" "$1")" -eq 8 ]' _ "$SCAN"
  vm_put "$SCAN" "$BM/quet.txt"
  vroot bash -c 'mkdir -p "$1" && journalctl -u tpan --no-pager >"$1/journal-tpan.txt" 2>&1; journalctl --no-pager >"$1/journal-all.txt" 2>&1; wc -l "$1"/journal-*.txt' _ "$BM/scan"

  local q="$E/quet.sh" l="$BM/quet.txt"
  vroot bash "$q" "$l" "đối chứng: câu trả lời lần đầu" "$BM/resp-lan-dau"
  ok "đối chứng: phép quét THẤY bí mật trong các câu trả lời lần đầu (phép quét không mù)" has 'totp=[1-9].* ma=[1-9].* token=[1-9]'
  vroot bash "$q" "$l" "T60 tpan.db, -wal" /var/lib/tpan/tpan.db /var/lib/tpan/tpan.db-wal
  ok "T60: không có trong tpan.db và tpan.db-wal" has 'tong=0$'
  vroot bash "$q" "$l" "T61 journalctl" "$BM/scan/journal-tpan.txt" "$BM/scan/journal-all.txt"
  ok "T61: không có trong journalctl -u tpan và cả journal" has 'tong=0$'
  vroot bash "$q" "$l" "T62 /var/log" /var/log
  ok "T62: không có trong /var/log/" has 'tong=0$'
  vroot bash "$q" "$l" "T63 job qua API" "$BM/gom/jobs.json" "$BM/gom/jobs"
  ok "T63: không có trong sự kiện và bằng chứng của mọi job" has 'tong=0$'
  vroot bash "$q" "$l" "T64 câu trả lời API" "$BM/resp-quet" "$BM/t40/body"
  ok "T64: không có trong mọi câu trả lời API đã lưu (trừ lần đầu), kể cả của bảng T40" has 'tong=0$'
  vroot bash "$q" "$l" "T65 MCP" "$BM/gom/mcp.out" "$BM/gom/mcp.err"
  ok "T65: không có trong output công cụ MCP" has 'tong=0$'
  vroot bash "$q" "$l" "T66 bản sao tpan.db" "$snap"
  ok "T66: không có trong bản sao tpan.db của backup" has 'tong=0$'
  vroot bash "$q" "$l" "T66 file log e2e" "$BM/e2e.log"
  ok "T66: không có trong file log e2e tới lúc này (cả file khoá dạng hex, base64)" has 'tong=0$'
  vroot rm -rf "$BM/resp-lan-dau" "$BM/resp-quet" "$BM/e2e.log" "$BM/scan" "$BM/gom" "$BM/t40" "$BM/quet.txt"
  part_end J
}

# ================================================================ CUỐI

part_end_all() {
  part_begin "CUỐI: health, site"
  local h2="$TMPD/health.2" moi
  health_errs "$h2"
  moi="$(new_errs "$H0" "$h2")"
  [ -n "$moi" ] && printf '[lỗi] mới so với lúc mới cài:\n%s\n' "$moi"
  ok "cuối kịch bản tpan health không có [lỗi] mới so với lúc mới cài" test -z "$moi"
  page "$SITE_A"
  ok "$SITE_A vẫn 200" is200
  vroot systemctl stop "$PEBBLE_UNIT"
  part_end CUỐI
}

leak_check() {
  [ -n "${E2E_LOG:-}" ] && [ -f "$E2E_LOG" ] || return 0
  section "bí mật, IP, tên user Mac có lọt vào chính file log không (trên Mac)"
  local n=0 s
  while IFS= read -r s; do
    [ -n "$s" ] || continue
    n=$((n + $(grep -cF -- "$s" "$E2E_LOG")))
  done <"$SECRETS"
  printf 'Số bí mật đã gặp: %s; số dòng của log chứa một trong số đó: %s\n' "$(grep -c . "$SECRETS")" "$n"
  ok "log không chứa mật khẩu, bí mật TOTP, mã dự phòng, token, cookie nào" test "$n" -eq 0
  ok "log không chứa IP máy ảo" bash -c '[ -z "$1" ] || ! grep -qF -- "$1" "$2"' _ "$VM_IP" "$E2E_LOG"
  ok "log không chứa mạng Multipass" bash -c '[ -z "$1" ] || ! grep -qF -- "$1" "$2"' _ "$NET4" "$E2E_LOG"
  ok "log không chứa IP của Mac" bash -c '[ -z "$1" ] || ! grep -qF -- "$1" "$2"' _ "$HOST_IP" "$E2E_LOG"
  ok "log không chứa thư mục nhà hay tên user Mac" bash -c '! grep -qF -- "$1" "$3" && ! grep -qF -- "$2" "$3"' _ "$HOME" "$USER" "$E2E_LOG"
}

printf 'e2e M20 (bước 23). Bản cũ: %s; bản A (của cây): %s; máy ảo: %s\n' "$OLD_VER" "$VER" "$VMNAME"
part_0
part_u
part_a
part_b
part_c
part_d
part_e
part_f
part_g
part_h
part_i
part_j
part_end_all

leak_check
section "TỔNG KẾT: $PASSES khẳng định đạt, $FAILS khẳng định không đạt"
[ "$FAILS" -eq 0 ]
