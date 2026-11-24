#!/usr/bin/env bash
# e2e bước 22 của mốc M19 (tên phụ, chuyển hướng, chứng chỉ phủ đủ các tên):
# PHẦN DÒNG LỆNH VÀ API. Đặc tả: docs/thay-doi/M19-thiet-ke.md mục 7.2 (E01-E71),
# cộng mục "Trả lời soát lượt 1" ở cuối file (S1, S2, S6, S7, S11, S12 đè lên 7.2).
# Chạy trên Mac, trong thư mục repo, điều khiển máy ảo (tên trong .tpan-vm).
# Cùng khuôn scripts/e2e-m28-dot2.sh. Gọi qua Makefile:
#
#   make e2e-m19 P="nginx apache nang-cap" L=docs/bang-chung/M19.log
#   make e2e-m19 P=nginx B=0          # chỉ stack nginx, dùng bin/ sẵn có
#
# Các phần (tham số dòng lệnh):
#   nginx     máy sạch, stack nginx: dung-may, ten, dinh-tuyen, redirect, ssl
#   apache    máy ảo về sạch rồi làm lại đúng các bước trên với stack Apache
#   nang-cap  cài BẢN NGAY TRƯỚC M19 (commit f6a68b9), hai site (một có SSL), nâng lên
#             bản của cây: E60, E61, E62
#   all       = nginx apache nang-cap
#
# Biến môi trường (Makefile truyền vào):
#   E2E_BUILD            1 (mặc định): make build-linux trước; 0: dùng bin/ sẵn có
#   E2E_BIN_PREFIX       tiền tố file chạy của cây (mặc định bin/tpan-linux)
#   E2E_OLD_BIN_PREFIX   tiền tố file chạy của bản trước M19 (mặc định bin/tpan-truoc-m19-linux)
#   E2E_OLD_COMMIT       commit bản trước M19 (mặc định f6a68b9)
#   E2E_NC_WEB           stack của phần nang-cap (mặc định apache)
#
# KHÔNG thuộc script này (ghi trong log để người đọc không tưởng là bỏ sót):
#   E10 phần giao diện, E23, E41: cần trình duyệt, ở e2e-m19-gd. Ở đây E10 thay bằng
#       thêm example.net qua API (đường dòng lệnh/API của cùng thao tác).
#   E70, E71: hồi quy e2e-m3/m5/m13/m14/m16, chạy riêng từng file (make e2e-... của chúng).
#       e2e-m14.sh đã đổi tên thử ở đúng một dòng: C=c.example.net (xem bảng tên dưới).
#       Khẳng định thêm của E71 trong e2e-m13.sh (khach_b tạo b9.example.net bị từ chối)
#       được kiểm ở đây thay thế: E22 (POST /sites của B cho tên thuộc tên miền gốc người khác).
#
# BẢNG TÊN (luật P4 của M28/M19: người không phải admin cần tên miền gốc riêng; admin
# được miễn luật tên miền gốc, nhưng KHÔNG được miễn luật "một tên một site"):
#   admin    example.com (site, SSL bằng pebble), www.example.com (tên phụ mặc định, redirect),
#            blog.example.com (site, E06, S12), example.net (tên phụ serve, E10),
#            việtnam.example.net = xn--vitnam-jk8b.example.net (tên phụ serve, R02 R04),
#            nodns.example.invalid (tên phụ serve, KHÔNG có trong /etc/hosts, E40)
#   khach_a  www.example.org (site, E05; tên phụ mặc định example.org)
#   khach_b  shop.khach-b.test (site; tên miền gốc khach-b.test)
#   E24      www.e24.test: tên miền gốc chưa ai dùng; khach_a, khach_b và dòng lệnh cùng giành
#   nang-cap old1.example.com (SSL), old2.example.com (admin)
# Lệch so với bảng 7.2 vì luật P4: A của 7.2 chia làm admin (example.com) và khach_a
# (example.org); B của 7.2 là khach_b (khach-b.test); .test dành riêng (RFC 2606) thay example.edu.
#
# Chọn cách dựng "bản ngay trước M19" cho phần nang-cap (S7): build file chạy từ
# `git archive f6a68b9` (không dùng git worktree, không checkout), cài thẳng bản đó
# lên máy sạch, rồi thay bằng bản của cây. Đơn giản hơn đường v0.2.0 -> trước M19 -> M19
# (cần gói phát hành v0.2.0 và hai lần nâng) và đúng điều S7 hỏi: output của bản ngay
# trước M19 so với bản M19. Migration từ đời cũ hơn đã có test (TestMigration1900OnlyPrimaries).
# web/dist của cây hiện tại được chép vào bản giải nén chỉ để go:embed biên dịch được;
# e2e này không dùng giao diện.
#
# Logic kiểm nằm trong scripts/e2e-m19/*.sh, chạy TRONG máy ảo bằng root. Mỗi khẳng định
# in đúng một dòng "=> ĐẠT" hoặc "=> KHÔNG ĐẠT" kèm mã E... Thoát khác 0 khi có khẳng định
# không đạt. Nhiều lệnh ở đây được chờ là phải lỗi: không dùng set -e.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2

VM=scripts/vm.sh
VMNAME="${TPAN_VM:-$(tr -d '[:space:]' 2>/dev/null <.tpan-vm)}"
VMNAME="${VMNAME:-tpan-test}"
# Mọi lệnh máy ảo qua lớp bắt output của vm.sh và có stdin </dev/null (xem scripts/e2e-m28.sh).
export TPAN_VM_CAPTURE=1
E2E_BUILD="${E2E_BUILD:-1}"
BIN_PREFIX="${E2E_BIN_PREFIX:-bin/tpan-linux}"
DEFAULT_PREFIX=bin/tpan-linux
OLD_PREFIX="${E2E_OLD_BIN_PREFIX:-bin/tpan-truoc-m19-linux}"
OLD_COMMIT="${E2E_OLD_COMMIT:-f6a68b9}"
NC_WEB="${E2E_NC_WEB:-apache}"
R=/home/ubuntu/e2e-m19 # thư mục script trong máy ảo
PASSES=0
FAILS=0
P0=0
F0=0
ARCH=""
VM_IP=""

ts() { date '+%Y-%m-%d %H:%M:%S'; }
section() { printf '\n\n######## [%s] %s\n' "$(ts)" "$*"; }

# mask: che IP máy ảo, thư mục nhà, tên user Mac, và mật khẩu (tpan setup in mật khẩu
# quản trị 26 chữ in hoa và số; tpan site add in mật khẩu WordPress 32 chữ và số).
mask() {
  local s=$1
  [ -n "$VM_IP" ] && s="${s//"$VM_IP"/<vm>}"
  s="${s//"$HOME"/~}"
  s="${s//"$USER"/<user>}"
  printf '%s' "$s" | sed -E \
    -e 's/(^|[^A-Za-z0-9])[A-Z0-9]{26}([^A-Za-z0-9]|$)/\1<mật khẩu đã che>\2/g' \
    -e 's/(^|[^A-Za-z0-9])[A-Za-z0-9]{32}([^A-Za-z0-9]|$)/\1<mật khẩu đã che>\2/g'
}

verdicts() {
  local p f
  p="$(grep -c '=> ĐẠT:' <<<"$1")"
  f="$(grep -c '=> KHÔNG ĐẠT:' <<<"$1")"
  PASSES=$((PASSES + p))
  FAILS=$((FAILS + f))
}

# vm_run <nhãn> <script trong R> [tham số...]: chạy một script trong máy ảo bằng root.
vm_run() {
  local label=$1 script=$2 out t0 t1
  shift 2
  printf '\n[%s] >>> %s  (bash %s %s)\n' "$(ts)" "$label" "$script" "$*"
  t0="$(date +%s)"
  out="$("$VM" root bash "$R/$script" "$@" </dev/null 2>&1)"
  t1="$(date +%s)"
  mask "$out"
  printf '\n[%s] <<< %s xong trong %s giây\n' "$(ts)" "$label" "$((t1 - t0))"
  verdicts "$out"
}

part_begin() { P0=$PASSES F0=$FAILS; section "PHẦN $*"; }
part_end() { printf '\n==== Phần %s: %s khẳng định đạt, %s không đạt\n' "$1" "$((PASSES - P0))" "$((FAILS - F0))"; }

reset_vm() {
  printf '\n[%s] đưa máy ảo %s về sạch\n' "$(ts)" "$VMNAME"
  local _ try
  for try in 1 2; do
    TPAN_VM_CAPTURE= "$VM" reset </dev/null >/dev/null 2>&1
    for _ in $(seq 1 20); do
      "$VM" exec cloud-init status --wait </dev/null >/dev/null 2>&1 && break
      sleep 3
    done
    # Như scripts/e2e-m28-dot2.sh: máy chưa về snapshot sạch (còn tpan) hay không chạy được
    # lệnh nào thì dừng phần đó, không chạy tiếp trên máy cũ.
    if "$VM" exec true </dev/null >/dev/null 2>&1 &&
      ! "$VM" exec test -e /usr/local/bin/tpan -o -e /var/lib/tpan </dev/null >/dev/null 2>&1; then
      break
    fi
    printf '[%s] máy ảo chưa về sạch hay không chạy được lệnh (lần %s)\n' "$(ts)" "$try"
    if [ "$try" = 2 ]; then
      echo "  => KHÔNG ĐẠT: máy ảo $VMNAME về được snapshot sạch và chạy được lệnh"
      FAILS=$((FAILS + 1))
      return 1
    fi
  done
  VM_IP="$(TPAN_VM_CAPTURE= "$VM" ip </dev/null 2>/dev/null | tr -d '[:space:]')"
  ARCH="$("$VM" exec dpkg --print-architecture </dev/null 2>/dev/null | tr -d '[:space:]')"
  printf '[%s] máy ảo sạch, kiến trúc %s, IP <vm>\n' "$(ts)" "$ARCH"
}

# deploy_bin: chép file chạy tpan của cây vào máy ảo (như scripts/e2e-m28-dot2.sh).
deploy_bin() {
  if [ "$BIN_PREFIX" = "$DEFAULT_PREFIX" ]; then
    if [ "$E2E_BUILD" != 0 ]; then
      printf '[%s] make build-linux\n' "$(ts)"
      make build-linux >/dev/null 2>&1 || { echo "make build-linux lỗi"; return 1; }
    else
      printf '[%s] E2E_BUILD=0: không build, dùng bin/ sẵn có\n' "$(ts)"
    fi
    TPAN_VM_CAPTURE= "$VM" deploy </dev/null >/dev/null 2>&1
  else
    install_bin "$BIN_PREFIX-$ARCH"
  fi
  "$VM" exec tpan version </dev/null 2>&1 | head -1
}

# install_bin <file>: chép một file chạy vào /usr/local/bin/tpan trong máy ảo.
install_bin() {
  local bin=$1
  printf '[%s] chép %s vào máy ảo\n' "$(ts)" "$bin"
  [ -f "$bin" ] || { echo "Chưa có $bin"; return 1; }
  "$VM" push "$bin" /tmp/tpan-m19 </dev/null >/dev/null 2>&1
  "$VM" root install -m 0755 /tmp/tpan-m19 /usr/local/bin/tpan </dev/null >/dev/null 2>&1
}

# build_old: file chạy của bản ngay trước M19 (S7), từ git archive của OLD_COMMIT.
build_old() {
  local bin="$OLD_PREFIX-$ARCH" dir="$PWD/tmp-phien/truoc-m19" out
  out="$PWD/$bin"
  [ -f "$bin" ] && { printf '[%s] dùng %s có sẵn\n' "$(ts)" "$bin"; return 0; }
  printf '[%s] build bản trước M19 (%s) từ git archive vào tmp-phien/truoc-m19\n' "$(ts)" "$OLD_COMMIT"
  mkdir -p "$dir" "$(dirname "$bin")"
  git archive "$OLD_COMMIT" | tar -x -C "$dir" || return 1
  mkdir -p "$dir/web/dist"
  cp -R web/dist/. "$dir/web/dist/"
  (cd "$dir" && CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" go build -trimpath -o "$out" ./cmd/tpan) || return 1
  [ -f "$bin" ]
}

# deploy_tree: thay file chạy bằng bản của cây rồi restart tpan (phần nâng cấp).
deploy_tree() {
  if [ "$BIN_PREFIX" = "$DEFAULT_PREFIX" ] && [ "$E2E_BUILD" != 0 ]; then
    make build-linux >/dev/null 2>&1 || return 1
  fi
  install_bin "$BIN_PREFIX-$ARCH" || return 1
  "$VM" root systemctl restart tpan </dev/null >/dev/null 2>&1
}

# push_scripts: chép scripts/e2e-m19/ vào máy ảo.
push_scripts() {
  printf '[%s] chép scripts/e2e-m19/ vào máy ảo (%s)\n' "$(ts)" "$R"
  local f
  for f in scripts/e2e-m19/*; do
    "$VM" push "$f" "$R/$(basename "$f")" </dev/null >/dev/null 2>&1
  done
}

# ---------------------------------------------------------------- các phần

part_stack() { # part_stack <apache|nginx>
  local web=$1 up
  up="$(tr a-z A-Z <<<"$web")"
  part_begin "$up"
  reset_vm || { part_end "$up"; return; }
  deploy_bin || { part_end "$up"; return; }
  push_scripts
  vm_run "dựng máy ($web), pebble, hai user thường" dung-may.sh "$web"
  vm_run "E01-E06, E10, E20-E24, S1, S12: tên phụ và quyền ($web)" ten.sh
  vm_run "E50-E52: bộ R01-R14 và route explain ($web)" dinh-tuyen.sh
  vm_run "E30-E33, S11: luật chuyển hướng ($web)" redirect.sh
  vm_run "E11, E12, E40-E44, E53: chứng chỉ nhiều tên ($web)" ssl.sh
  part_end "$up"
}

part_nangcap() {
  part_begin "NÂNG CẤP: bản ngay trước M19 ($OLD_COMMIT) lên bản của cây ($NC_WEB)"
  reset_vm || { part_end NÂNG-CẤP; return; }
  build_old || { echo "  => KHÔNG ĐẠT: E60 dựng được bản trước M19 từ $OLD_COMMIT"; FAILS=$((FAILS + 1)); part_end NÂNG-CẤP; return; }
  install_bin "$OLD_PREFIX-$ARCH" || { part_end NÂNG-CẤP; return; }
  push_scripts
  vm_run "cài bản trước M19, hai site, SSL cho một site, ghi sha256 vhost và health" nang-cap.sh truoc "$NC_WEB"
  printf '\n[%s] thay file chạy bằng bản của cây rồi restart tpan\n' "$(ts)"
  deploy_tree
  "$VM" exec tpan version </dev/null 2>&1 | head -1
  vm_run "E60-E62: sau nâng cấp" nang-cap.sh sau "$NC_WEB"
  part_end NÂNG-CẤP
}

# ---------------------------------------------------------------- điều phối

PARTS=()
for a in "$@"; do
  case "$a" in
    all) PARTS+=(nginx apache nang-cap) ;;
    nginx | apache | nang-cap) PARTS+=("$a") ;;
    *) echo "Phần lạ: $a (dùng: nginx apache nang-cap all)" >&2; exit 2 ;;
  esac
done
[ ${#PARTS[@]} -gt 0 ] || { echo "Cách dùng: $0 <phần>...  (nginx apache nang-cap all)" >&2; exit 2; }

section "e2e M19 bước 22 (dòng lệnh và API) — máy ảo $VMNAME — phần: ${PARTS[*]} — log ${E2E_LOG:-(stdout)}"
printf 'E2E_BUILD=%s  E2E_BIN_PREFIX=%s  E2E_OLD_BIN_PREFIX=%s  E2E_OLD_COMMIT=%s\n' "$E2E_BUILD" "$BIN_PREFIX" "$OLD_PREFIX" "$OLD_COMMIT"
echo "E10 giao diện: ở e2e-m19-gd (ở đây E10 thay bằng thêm example.net qua API)"
echo "E23 giao diện, E41 giao diện: ở e2e-m19-gd"
echo "E70, E71: hồi quy các e2e cũ, chạy riêng (ngoài script này)"

for p in "${PARTS[@]}"; do
  case "$p" in
    apache | nginx) part_stack "$p" ;;
    nang-cap) part_nangcap ;;
  esac
done

section "TỔNG KẾT: $PASSES khẳng định đạt, $FAILS khẳng định không đạt"
[ "$FAILS" -eq 0 ]
