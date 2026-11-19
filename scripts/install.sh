#!/usr/bin/env bash
# Cài tpan lên một máy Ubuntu 24.04 sạch bằng một lệnh (mốc M11, mục 5 và 9
# của docs/KE-HOACH.md). Chạy bằng root.
#
# Ba cách lấy file chạy:
#   (a) Đứng trong thư mục gói phát hành đã chép sang máy:
#         sudo ./install.sh
#       dùng file tpan-linux-<arch> nằm cạnh install.sh, kiểm theo SHA256SUMS bên cạnh.
#   (b) Chỉ định một file chạy có sẵn:
#         sudo ./install.sh --binary /đường/dẫn/tpan
#   (c) Tải từ bản phát hành (repo phải công khai thì mới tải được từ GitHub):
#         curl -fsSL <base>/install.sh | sudo bash -s -- [cờ...]
#         sudo ./install.sh --from <base>
#       tải <base>/tpan-linux-<arch> và <base>/SHA256SUMS.
#
# Sau khi đặt /usr/local/bin/tpan, script chạy `tpan setup` (dịch vụ, chứng chỉ,
# mật khẩu quản trị) và, nếu có --web, `tpan stack install`. Chạy lại được:
# thứ gì đã đúng thì không đổi.
#
# set -E: bẫy ERR (on_err) có hiệu lực cả trong các hàm; thiếu nó thì lệnh hỏng
# trong hàm làm script thoát im lặng.
#
# Toàn bộ thân script nằm trong hàm main, dòng cuối mới gọi main. Khi chạy kiểu
# `curl ... | bash`, bash đọc script từ ống dẫn tới đâu chạy tới đó; tải đứt
# giữa chừng thì chưa có dòng gọi main, nên không có nửa script nào được chạy.
set -Eeuo pipefail

DEFAULT_BASE="https://github.com/doxuta/tpan/releases/latest/download"
BIN_DST="/usr/local/bin/tpan"
OS_RELEASE="/etc/os-release"

LANG_UI="vi"
CUR_STEP=""

# t in câu theo ngôn ngữ đã chọn (--lang vi|en), giống tpan --lang.
t() {
  if [ "$LANG_UI" = "en" ]; then printf '%s' "$2"; else printf '%s' "$1"; fi
}

say() { printf '%s\n' "$*"; }

# step ghi lại bước đang làm để thông báo lỗi nói được "lỗi ở đâu".
step() {
  CUR_STEP="$1"
  printf '\n==> %s\n' "$1"
}

# die dừng ở lỗi đầu tiên: chuyện gì xảy ra (tham số 1), làm gì tiếp (tham số 2).
die() {
  {
    printf '\n%s %s\n' "$(t 'LỖI:' 'ERROR:')" "$1"
    if [ -n "$CUR_STEP" ]; then
      printf '%s %s\n' "$(t 'Ở bước:' 'At step:')" "$CUR_STEP"
    fi
    if [ -n "${2:-}" ]; then
      printf '%s %s\n' "$(t 'Làm gì tiếp:' 'What to do next:')" "$2"
    fi
  } >&2
  exit 1
}

# on_err bắt các lệnh hỏng mà script không kiểm riêng (set -e chỉ thoát, không
# nói gì): in bước, dòng và lệnh hỏng.
on_err() {
  local code="$1" line="$2" cmd="$3"
  trap - ERR
  die "$(t "lệnh \"$cmd\" thoát với mã $code (dòng $line của install.sh)" \
          "command \"$cmd\" exited with code $code (line $line of install.sh)")" \
      "$(t 'đọc thông báo ngay phía trên, sửa nguyên nhân rồi chạy lại install.sh (chạy lại an toàn).' \
           'read the message just above, fix the cause, then run install.sh again (re-running is safe).')"
}

usage() {
  cat <<'EOF'
Usage / Cách dùng:
  sudo ./install.sh [--binary FILE | --from BASE] [--private] [--web nginx|apache]
                    [--force] [--check-only] [--lang vi|en]
  curl -fsSL BASE/install.sh | sudo bash -s -- [flags]

  --binary FILE   dùng file chạy có sẵn / use an existing tpan executable
  --from BASE     tải tpan-linux-<arch> và SHA256SUMS từ BASE (https; http chỉ
                  cho 127.0.0.1, localhost, [::1]) / download from BASE
                  mặc định / default: https://github.com/doxuta/tpan/releases/latest/download
  --private       tpan setup --private: panel chỉ nghe 127.0.0.1 / listen on 127.0.0.1 only
  --web W         cài thêm stack / also install the stack: nginx (LEMP) | apache (LAMP)
  --force         bỏ qua kiểm Ubuntu 24.04 (rủi ro) / skip the Ubuntu 24.04 check (risky)
  --check-only    chỉ tìm hoặc tải file chạy và kiểm sha256, không cần root,
                  không cài gì / only fetch and verify the executable, install nothing
  --lang vi|en    ngôn ngữ thông báo / message language
EOF
}

# check_base nhận gốc tải theo đúng luật của self-update (checkSelfBase trong
# internal/steps/self.go): https://host[/đường], hoặc http:// khi host là
# loopback (e2e trên máy ảo). Không có khoảng trắng, user@, ?, #. Gốc được nối
# thành URL đưa cho curl, chữ lạ ở đây là tải nhầm chỗ.
check_base() {
  local b="$1"
  case "$b" in
    *[[:space:]]* | *[[:cntrl:]]* | *@* | *\?* | *#*)
      return 1 ;;
  esac
  case "$b" in
    https://?*) ;;
    http://127.0.0.1 | http://127.0.0.1[:/]* | http://localhost | http://localhost[:/]* | http://\[::1\] | http://\[::1\][:/]*) ;;
    *) return 1 ;;
  esac
  return 0
}

# sum_line tìm dòng của SHA256SUMS ($1) ứng với tên file $2 và để nó ở biến
# SUM_LINE (dạng "<hex>  <tên>"). Tên phải khớp đúng (tpan-linux-amd64.sig không
# tính), giống parseSums của self-update. Không có dòng nào: mã 1. Hai dòng cùng
# tên mà khác số: mã 2 (không đoán). Trả kết quả qua biến, không qua $(...):
# hàm trả mã khác 0 trong $(...) làm bẫy ERR nổ trong tiến trình con.
SUM_LINE=""
sum_line() {
  local sums="$1" name="$2" line hex fname found=""
  while IFS= read -r line || [ -n "$line" ]; do
    line="${line%$'\r'}"
    hex="${line%% *}"
    fname="${line#* }"
    fname="${fname# }"
    fname="${fname#\*}"
    [ "$fname" = "$name" ] || continue
    case "$hex" in
      *[!0-9a-fA-F]*) continue ;;
    esac
    [ "${#hex}" -eq 64 ] || continue
    if [ -n "$found" ] && [ "$found" != "$hex" ]; then
      return 2
    fi
    found="$hex"
  done < "$sums"
  [ -n "$found" ] || return 1
  SUM_LINE="$found  $name"
}

# verify_sha256 kiểm file $1 theo SHA256SUMS $2 bằng sha256sum -c với đúng một
# dòng của file đó. sha256sum đọc tên file tương đối, nên chạy trong thư mục
# chứa file.
verify_sha256() {
  local file="$1" sums="$2" name dir rc=0
  name="$(basename "$file")"
  dir="$(dirname "$file")"
  sum_line "$sums" "$name" || rc=$?
  case "$rc" in
    0) ;;
    2) die "$(t "$sums có hai dòng khác nhau cho $name" "$sums has two different lines for $name")" \
           "$(t 'gói phát hành bị sửa hoặc ghép nhầm; lấy lại gói từ nơi tin cậy.' \
                'the release package was altered or mixed up; get it again from a trusted place.')" ;;
    *) die "$(t "$sums không có dòng nào cho $name" "$sums has no line for $name")" \
           "$(t 'gói phát hành thiếu file cho kiến trúc này; build lại bằng make release hoặc lấy đúng gói.' \
                'the release package lacks this architecture; rebuild with make release or get the right package.')" ;;
  esac
  if ! (cd "$dir" && printf '%s\n' "$SUM_LINE" | sha256sum -c --strict --status -); then
    die "$(t "sha256 của $file không khớp $sums: file hỏng hoặc bị sửa giữa đường. Không cài." \
             "sha256 of $file does not match $sums: the file is corrupted or was tampered with. Not installing.")" \
        "$(t 'tải hoặc chép lại gói phát hành rồi chạy lại.' 'download or copy the release package again, then re-run.')"
  fi
  say "$(t "sha256 khớp: $name" "sha256 matches: $name")"
}

# fetch tải $1 vào $2. Chỉ https (kể cả khi bị chuyển hướng); http chỉ khi gốc
# là loopback, đã kiểm ở check_base.
fetch() {
  local url="$1" out="$2" proto="=https"
  case "$url" in
    http://*) proto="=http" ;;
  esac
  say "$(t 'Tải' 'Download') $url"
  if ! curl -fsSL --proto "$proto" --proto-redir '=https' --connect-timeout 20 --retry 2 -o "$out" "$url"; then
    die "$(t "không tải được $url" "cannot download $url")" \
        "$(t 'kiểm mạng và địa chỉ --from; với GitHub, repo phải công khai và đã có bản phát hành. Có gói trên máy thì chép sang rồi chạy ./install.sh trong thư mục gói.' \
             'check the network and the --from address; for GitHub the repo must be public with a release. If you have the package, copy it over and run ./install.sh inside it.')"
  fi
}

# file_sha in sha256 hex của $1.
file_sha() {
  local out
  out="$(sha256sum "$1")"
  printf '%s' "${out%% *}"
}

main() {
  local binary="" from="" private=0 web="" force=0 check_only=0
  while [ $# -gt 0 ]; do
    case "$1" in
      --binary) [ $# -ge 2 ] || { usage >&2; exit 2; }; binary="$2"; shift 2 ;;
      --from) [ $# -ge 2 ] || { usage >&2; exit 2; }; from="$2"; shift 2 ;;
      --private) private=1; shift ;;
      --web) [ $# -ge 2 ] || { usage >&2; exit 2; }; web="$2"; shift 2 ;;
      --force) force=1; shift ;;
      --check-only) check_only=1; shift ;;
      --lang) [ $# -ge 2 ] || { usage >&2; exit 2; }; LANG_UI="$2"; shift 2 ;;
      -h | --help) usage; exit 0 ;;
      *) usage >&2; printf '\n%s %s\n' "$(t 'Cờ lạ:' 'Unknown flag:')" "$1" >&2; exit 2 ;;
    esac
  done
  trap 'on_err "$?" "$LINENO" "$BASH_COMMAND"' ERR

  # Kiểm hết tham số trước khi đụng tới máy.
  case "$LANG_UI" in
    vi | en) ;;
    *) LANG_UI="vi"; die "--lang phải là vi hoặc en / --lang must be vi or en" ;;
  esac
  case "$web" in
    "" | nginx | apache) ;;
    *) die "$(t "--web phải là nginx hoặc apache (nhận được \"$web\")" "--web must be nginx or apache (got \"$web\")")" ;;
  esac
  if [ -n "$binary" ] && [ -n "$from" ]; then
    die "$(t 'chỉ dùng một trong --binary và --from' 'use only one of --binary and --from')"
  fi
  if [ -n "$from" ]; then
    from="${from%/}"
    check_base "$from" || die \
      "$(t "gốc tải \"$from\" không hợp lệ: cần https://..., http chỉ cho 127.0.0.1, localhost, [::1]; không có khoảng trắng, @, ?, #" \
           "download base \"$from\" is invalid: use https://..., http only for 127.0.0.1, localhost, [::1]; no spaces, @, ?, #")"
  fi

  step "$(t 'Kiểm máy' 'Check the machine')"
  if [ "$check_only" -eq 0 ]; then
    [ "$(id -u)" -eq 0 ] || die "$(t 'cần chạy bằng root' 'must run as root')" \
      "$(t 'chạy lại với sudo: sudo ./install.sh ... hoặc curl ... | sudo bash -s -- ...' \
           'run again with sudo: sudo ./install.sh ... or curl ... | sudo bash -s -- ...')"
    local os_id="" os_ver="" k v
    if [ -r "$OS_RELEASE" ]; then
      # Đọc từng dòng KEY=giá trị, không source: source là chạy nội dung file
      # như mã. Không dùng ống grep | cut trong $(...): thiếu dòng thì bẫy ERR
      # nổ trong tiến trình con, in một lỗi giả.
      while IFS='=' read -r k v || [ -n "$k" ]; do
        v="${v//\"/}"
        case "$k" in
          ID) os_id="$v" ;;
          VERSION_ID) os_ver="$v" ;;
        esac
      done < "$OS_RELEASE"
    fi
    if [ "$os_id" = "ubuntu" ] && [ "$os_ver" = "24.04" ]; then
      say "Ubuntu 24.04"
    elif [ "$force" -eq 1 ]; then
      say "$(t "CẢNH BÁO: máy này là \"$os_id $os_ver\", không phải Ubuntu 24.04. Tiếp tục vì có --force. tpan chỉ được kiểm trên Ubuntu 24.04: tên gói, đường dẫn config và dịch vụ có thể khác, cài stack hoặc tạo site có thể hỏng giữa chừng." \
               "WARNING: this machine is \"$os_id $os_ver\", not Ubuntu 24.04. Continuing because of --force. tpan is only tested on Ubuntu 24.04: package names, config paths and services may differ, and stack install or site creation may fail halfway.")"
    else
      die "$(t "tpan chỉ hỗ trợ Ubuntu 24.04, máy này là \"$os_id $os_ver\"" "tpan only supports Ubuntu 24.04, this machine is \"$os_id $os_ver\"")" \
          "$(t 'dùng một máy Ubuntu 24.04; hoặc thêm --force nếu chấp nhận rủi ro tpan chạy sai trên hệ khác.' \
               'use an Ubuntu 24.04 machine, or add --force if you accept the risk of tpan misbehaving elsewhere.')"
    fi
  else
    say "$(t '--check-only: bỏ qua kiểm root và hệ điều hành' '--check-only: skipping the root and OS checks')"
  fi
  command -v dpkg >/dev/null 2>&1 || die "$(t 'không có lệnh dpkg để biết kiến trúc máy' 'dpkg is missing, cannot tell the machine architecture')" \
    "$(t 'tpan chỉ chạy trên Ubuntu (amd64 hoặc arm64).' 'tpan only runs on Ubuntu (amd64 or arm64).')"
  local arch
  arch="$(dpkg --print-architecture)"
  case "$arch" in
    amd64 | arm64) say "$(t 'Kiến trúc:' 'Architecture:') $arch" ;;
    *) die "$(t "kiến trúc \"$arch\" không được hỗ trợ (chỉ amd64, arm64)" "architecture \"$arch\" is not supported (amd64, arm64 only)")" ;;
  esac
  command -v sha256sum >/dev/null 2>&1 || die "$(t 'không có lệnh sha256sum' 'sha256sum is missing')" \
    "$(t 'cài gói coreutils: apt-get install -y coreutils' 'install coreutils: apt-get install -y coreutils')"

  local name="tpan-linux-$arch" src="" tmp
  tmp="$(mktemp -d)"
  # Dọn thư mục tạm khi thoát, kể cả khi lỗi.
  # shellcheck disable=SC2064 # muốn mở rộng $tmp ngay lúc đặt trap
  trap "rm -rf '$tmp'" EXIT

  # Tìm thư mục chứa install.sh. Chạy kiểu `curl | bash` thì BASH_SOURCE trống
  # (không có file), nên không có thư mục gói: tải về.
  local here="" pkg=0
  if [ -n "${BASH_SOURCE[0]:-}" ] && [ -f "${BASH_SOURCE[0]}" ]; then
    here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  fi
  if [ -z "$binary" ] && [ -z "$from" ]; then
    if [ -n "$here" ] && [ -f "$here/$name" ]; then
      binary="$here/$name"
      pkg=1
    else
      from="$DEFAULT_BASE"
    fi
  fi

  if [ -n "$from" ]; then
    step "$(t "Tải tpan từ $from" "Download tpan from $from")"
    command -v curl >/dev/null 2>&1 || die "$(t 'không có lệnh curl để tải' 'curl is missing')" \
      "$(t 'cài curl: apt-get install -y curl; hoặc chép gói phát hành sang máy rồi chạy ./install.sh trong đó.' \
           'install curl: apt-get install -y curl; or copy the release package over and run ./install.sh inside it.')"
    fetch "$from/SHA256SUMS" "$tmp/SHA256SUMS"
    fetch "$from/$name" "$tmp/$name"
    src="$tmp/$name"
    step "$(t 'Kiểm sha256' 'Verify sha256')"
    verify_sha256 "$src" "$tmp/SHA256SUMS"
  else
    step "$(t "Dùng file chạy $binary" "Use the executable $binary")"
    [ -f "$binary" ] || die "$(t "không thấy file $binary" "file $binary not found")" \
      "$(t 'kiểm đường dẫn --binary, hoặc đứng trong thư mục gói phát hành.' 'check the --binary path, or run inside the release package folder.')"
    src="$binary"
    step "$(t 'Kiểm sha256' 'Verify sha256')"
    local sums bn rc=0
    sums="$(dirname "$binary")/SHA256SUMS"
    bn="$(basename "$binary")"
    if [ -f "$sums" ]; then
      sum_line "$sums" "$bn" || rc=$?
    else
      rc=3
    fi
    if [ "$pkg" -eq 1 ] && [ "$rc" -eq 3 ]; then
      die "$(t "thư mục gói thiếu SHA256SUMS cạnh $name" "the package folder has no SHA256SUMS next to $name")" \
          "$(t 'chép đủ gói phát hành (cả SHA256SUMS) rồi chạy lại.' 'copy the whole release package (including SHA256SUMS) and run again.')"
    elif [ "$pkg" -eq 1 ] || [ "$rc" -eq 0 ] || [ "$rc" -eq 2 ] || { [ "$rc" -eq 1 ] && [ "$bn" = "$name" ]; }; then
      # Gói phát hành, hoặc file mang đúng tên tpan-linux-<arch> nằm cạnh một
      # SHA256SUMS: luôn kiểm, thiếu dòng là lỗi. File tên khác (tự build): kiểm
      # khi SHA256SUMS cạnh nó có dòng cho đúng tên đó.
      verify_sha256 "$src" "$sums"
    else
      say "$(t "CẢNH BÁO: không có SHA256SUMS ghi tên $bn cạnh file, KHÔNG kiểm sha256. Chỉ dùng file bạn tự build hoặc tin nguồn của nó." \
               "WARNING: no SHA256SUMS listing $bn next to the file, sha256 NOT checked. Only use a file you built yourself or whose source you trust.")"
    fi
  fi

  if [ "$check_only" -eq 1 ]; then
    say ""
    say "$(t "Xong (--check-only): $src hợp lệ, không cài gì." "Done (--check-only): $src is valid, nothing installed.")"
    return 0
  fi

  step "$(t "Đặt $BIN_DST" "Install $BIN_DST")"
  local changed=0 new_sum old_sum="" probe
  new_sum="$(file_sha "$src")"
  if [ -f "$BIN_DST" ]; then
    old_sum="$(file_sha "$BIN_DST")"
  fi
  if [ "$new_sum" = "$old_sum" ]; then
    say "$(t "$BIN_DST đã đúng bản này, giữ nguyên" "$BIN_DST already has this build, kept")"
    chown root:root "$BIN_DST"
    chmod 0755 "$BIN_DST"
    probe="$BIN_DST"
  else
    # Chép vào tên tạm cùng thư mục, đặt quyền, thử chạy, rồi mới mv (rename).
    # - Thử chạy bản đã chép chứ không chạy file nguồn: file tải bằng curl hay
    #   chép qua scp không có quyền chạy, và /tmp có máy gắn noexec.
    # - rename là thay nguyên tử và không ghi đè file đang chạy (Linux báo
    #   "Text file busy" nếu tpan.service đang chạy nó).
    # - Không dùng tên .new/.old: đó là chỗ của tpan self-update.
    probe="$BIN_DST.install-tmp"
    cp "$src" "$probe"
    chown root:root "$probe"
    chmod 0755 "$probe"
  fi
  # Chạy `tpan version` trước khi thay: file hỏng hay sai kiến trúc thì dừng ở
  # đây, bản đang cài (nếu có) vẫn nguyên.
  local ver_out
  ver_out="$("$probe" version 2>&1)" || ver_out=""
  case "$ver_out" in
    "tpan "*) say "${ver_out%%$'\n'*}" ;;
    *)
      if [ "$probe" != "$BIN_DST" ]; then rm -f "$probe"; fi
      die "$(t "$src version không chạy được hoặc in dòng lạ" "$src version does not run or prints something unexpected")" \
          "$(t "file hỏng hoặc không hợp kiến trúc $arch; lấy đúng file tpan-linux-$arch." \
               "the file is broken or not built for $arch; use the right tpan-linux-$arch file.")" ;;
  esac
  if [ "$probe" != "$BIN_DST" ]; then
    mv -f "$probe" "$BIN_DST"
    changed=1
    say "$(t "Đã đặt $BIN_DST" "Installed $BIN_DST")"
  fi
  # tpan setup chỉ khởi động lại dịch vụ khi file unit đổi; file chạy đổi mà
  # dịch vụ đang chạy thì tiến trình cũ vẫn chạy bản cũ.
  if [ "$changed" -eq 1 ] && systemctl is-active --quiet tpan 2>/dev/null; then
    say "$(t 'Dịch vụ tpan đang chạy bản cũ, khởi động lại' 'The tpan service runs the old build, restarting it')"
    systemctl restart tpan
  fi

  step "$(t 'Khởi tạo panel: tpan setup' 'Set up the panel: tpan setup')"
  # --yes bắt buộc: chạy kiểu curl | bash thì stdin không phải terminal, tpan
  # setup không hỏi được nên sẽ từ chối. Không bắt hay ghi output: mật khẩu quản
  # trị chỉ in ra đúng một lần ở đây.
  local setup_args=(setup --yes --lang "$LANG_UI")
  if [ "$private" -eq 1 ]; then
    setup_args+=(--private)
  fi
  "$BIN_DST" "${setup_args[@]}" || die "$(t 'tpan setup không thành công' 'tpan setup failed')" \
    "$(t 'đọc báo cáo của tpan phía trên; sửa rồi chạy lại install.sh hoặc: sudo tpan setup' \
         "read tpan's report above; fix it, then re-run install.sh or: sudo tpan setup")"

  if [ -n "$web" ]; then
    step "$(t "Cài stack: tpan stack install --web $web" "Install the stack: tpan stack install --web $web")"
    "$BIN_DST" stack install --web "$web" --yes --lang "$LANG_UI" || die \
      "$(t 'tpan stack install không thành công' 'tpan stack install failed')" \
      "$(t "đọc báo cáo phía trên; chạy lại: sudo tpan stack install --web $web, kiểm bằng: sudo tpan health" \
           "read the report above; re-run: sudo tpan stack install --web $web, check with: sudo tpan health")"
  fi

  CUR_STEP=""
  say ""
  say "$(t 'Xong. Bước tiếp:' 'Done. Next:')"
  if [ -z "$web" ]; then
    say "  sudo tpan stack install --web nginx    # $(t 'hoặc apache' 'or apache')"
  fi
  say "  sudo tpan site add example.com --email admin@example.com"
  say "  sudo tpan health"
}

main "$@"
