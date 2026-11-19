#!/usr/bin/env bash
# Test của scripts/install.sh, chạy được trên Mac lẫn Linux, không cần root:
#   bash scripts/install_test.sh
#
# Chỉ thử phần không đụng tới máy: đọc tham số, tìm hoặc tải file chạy, kiểm
# sha256 (cờ --check-only). Phần cài thật (đặt /usr/local/bin/tpan, tpan setup)
# cần root và Ubuntu 24.04, kiểm trên máy ảo.
#
# Kiến trúc máy lấy từ `dpkg --print-architecture`; Mac không có dpkg nên test
# đặt một dpkg giả đầu PATH, in $FAKE_ARCH.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INSTALL="$ROOT/scripts/install.sh"
WORK="$(mktemp -d)"
SERVER_PID=""
cleanup() {
  if [ -n "$SERVER_PID" ]; then kill "$SERVER_PID" 2>/dev/null || true; fi
  rm -rf "$WORK"
}
trap cleanup EXIT

mkdir -p "$WORK/fakebin"
# shellcheck disable=SC2016 # $FAKE_ARCH phải nằm nguyên trong file dpkg giả
printf '#!/bin/sh\necho "${FAKE_ARCH:-amd64}"\n' > "$WORK/fakebin/dpkg"
chmod +x "$WORK/fakebin/dpkg"
export PATH="$WORK/fakebin:$PATH"

sha() { local o; o="$(sha256sum "$1")"; printf '%s' "${o%% *}"; }

# mkpkg <thư mục>: dựng một gói phát hành giả giống make release.
mkpkg() {
  local d="$1" a
  mkdir -p "$d"
  for a in amd64 arm64; do
    printf '#!/bin/sh\necho "tpan v-test (%s)"\n' "$a" > "$d/tpan-linux-$a"
    chmod +x "$d/tpan-linux-$a"
  done
  cp "$INSTALL" "$d/install.sh"
  (cd "$d" && sha256sum tpan-linux-amd64 tpan-linux-arm64 install.sh > SHA256SUMS)
}

PASS=0
FAIL=0
# run <tên> <mã thoát mong đợi> <chữ phải có trong output> -- <lệnh...>
run() {
  local name="$1" want_code="$2" want_text="$3" out code=0
  shift 4
  out="$("$@" 2>&1)" || code=$?
  # Mỗi lần dừng chỉ được in đúng một thông báo lỗi.
  if [ "$code" -eq "$want_code" ] && printf '%s' "$out" | grep -qF -- "$want_text" &&
    [ "$(printf '%s\n' "$out" | grep -cE '^(LỖI|ERROR):')" -le 1 ]; then
    PASS=$((PASS + 1))
    printf 'ok    %s\n' "$name"
  else
    FAIL=$((FAIL + 1))
    printf 'FAIL  %s (mã %s, cần %s; cần chữ "%s")\n%s\n' "$name" "$code" "$want_code" "$want_text" "$out"
  fi
}

# 1. Gói đúng, chạy ./install.sh trong thư mục gói.
P="$WORK/pkg-good"; mkpkg "$P"
run "gói đúng" 0 "sha256 khớp: tpan-linux-amd64" -- bash "$P/install.sh" --check-only
run "gói đúng, arm64" 0 "sha256 khớp: tpan-linux-arm64" -- env FAKE_ARCH=arm64 bash "$P/install.sh" --check-only
run "kiến trúc lạ" 1 'kiến trúc "i386"' -- env FAKE_ARCH=i386 bash "$P/install.sh" --check-only

# 2. File chạy bị sửa một byte sau khi tính SHA256SUMS.
P="$WORK/pkg-tampered"; mkpkg "$P"
printf 'x' >> "$P/tpan-linux-amd64"
run "file bị sửa" 1 "không khớp" -- bash "$P/install.sh" --check-only

# 3. SHA256SUMS thiếu dòng của kiến trúc.
P="$WORK/pkg-noline"; mkpkg "$P"
grep -v ' tpan-linux-amd64$' "$P/SHA256SUMS" > "$P/s" && mv "$P/s" "$P/SHA256SUMS"
run "thiếu dòng" 1 "không có dòng nào cho tpan-linux-amd64" -- bash "$P/install.sh" --check-only

# 4. Chỉ có dòng .sig: tên phải khớp đúng.
P="$WORK/pkg-sig"; mkpkg "$P"
printf '%s  tpan-linux-amd64.sig\n' "$(sha "$P/tpan-linux-amd64")" > "$P/SHA256SUMS"
run "chỉ có dòng .sig" 1 "không có dòng nào cho tpan-linux-amd64" -- bash "$P/install.sh" --check-only

# 5. Hai dòng cùng tên khác số.
P="$WORK/pkg-dup"; mkpkg "$P"
printf '%s  tpan-linux-amd64\n' "$(printf '0%.0s' $(seq 64))" >> "$P/SHA256SUMS"
run "hai dòng khác nhau" 1 "hai dòng khác nhau" -- bash "$P/install.sh" --check-only

# 6. Dòng dạng nhị phân (*tên) và CRLF vẫn nhận.
P="$WORK/pkg-star"; mkpkg "$P"
printf '%s *tpan-linux-amd64\r\n' "$(sha "$P/tpan-linux-amd64")" > "$P/SHA256SUMS"
run "dòng *tên, CRLF" 0 "sha256 khớp" -- bash "$P/install.sh" --check-only

# 7. Gói thiếu SHA256SUMS: không được cài mà không kiểm.
P="$WORK/pkg-nosums"; mkpkg "$P"; rm "$P/SHA256SUMS"
run "gói thiếu SHA256SUMS" 1 "thiếu SHA256SUMS" -- bash "$P/install.sh" --check-only

# 8. --binary: tên khác, không có SHA256SUMS thì cảnh báo; có dòng thì kiểm.
mkdir -p "$WORK/own"
printf '#!/bin/sh\necho "tpan v-own"\n' > "$WORK/own/tpan"
run "--binary không SHA256SUMS" 0 "KHÔNG kiểm sha256" -- bash "$INSTALL" --check-only --binary "$WORK/own/tpan"
printf '%s  tpan\n' "$(printf '0%.0s' $(seq 64))" > "$WORK/own/SHA256SUMS"
run "--binary có dòng sai" 1 "không khớp" -- bash "$INSTALL" --check-only --binary "$WORK/own/tpan"
run "--binary không có file" 1 "không thấy file" -- bash "$INSTALL" --check-only --binary "$WORK/nope"
# File tên tpan-linux-<arch> cạnh một SHA256SUMS thiếu dòng của nó: lỗi, không chỉ cảnh báo.
run "--binary tên gói, thiếu dòng" 1 "không có dòng nào cho tpan-linux-amd64" -- bash "$INSTALL" --check-only --binary "$WORK/pkg-noline/tpan-linux-amd64"
run "--binary tên gói, đúng" 0 "sha256 khớp: tpan-linux-amd64" -- bash "$INSTALL" --check-only --binary "$WORK/pkg-good/tpan-linux-amd64"

# 9. Tham số sai: dừng trước khi đụng tới máy hay mạng.
run "--from http không loopback" 1 "không hợp lệ" -- bash "$INSTALL" --check-only --from http://example.com
run "--from có user@" 1 "không hợp lệ" -- bash "$INSTALL" --check-only --from https://u@example.com
run "--from có query" 1 "không hợp lệ" -- bash "$INSTALL" --check-only --from 'https://example.com/?a=1'
run "--from có khoảng trắng" 1 "không hợp lệ" -- bash "$INSTALL" --check-only --from 'https://example.com/a b'
run "--from ftp" 1 "không hợp lệ" -- bash "$INSTALL" --check-only --from ftp://example.com
run "--web lạ" 1 "--web phải là nginx hoặc apache" -- bash "$INSTALL" --check-only --web 'nginx;id'
run "--binary và --from" 1 "chỉ dùng một" -- bash "$INSTALL" --check-only --binary /x --from https://example.com
run "cờ lạ" 2 "Cờ lạ" -- bash "$INSTALL" --bogus
run "--lang en" 1 "must be nginx or apache" -- bash "$INSTALL" --lang en --check-only --web x
run "--lang lạ" 1 "--lang phải là vi hoặc en" -- bash "$INSTALL" --lang fr

# 10. Không có --check-only thì phải là root.
if [ "$(id -u)" -ne 0 ]; then
  run "không root" 1 "cần chạy bằng root" -- bash "$WORK/pkg-good/install.sh"
fi

# 11. Tải qua http loopback (đường tải thật bằng curl), kể cả kiểu curl | bash.
if command -v python3 >/dev/null 2>&1 && command -v curl >/dev/null 2>&1; then
  S="$WORK/srv"; mkpkg "$S/good"; mkpkg "$S/bad"
  printf 'x' >> "$S/bad/tpan-linux-amd64"
  PORT=$((20000 + $$ % 20000))
  python3 -m http.server "$PORT" --bind 127.0.0.1 --directory "$S" >/dev/null 2>&1 &
  disown
  SERVER_PID=$!
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    curl -fs "http://127.0.0.1:$PORT/good/SHA256SUMS" >/dev/null 2>&1 && break
    sleep 0.3
  done
  BASE="http://127.0.0.1:$PORT"
  run "--from loopback đúng" 0 "sha256 khớp: tpan-linux-amd64" -- bash "$INSTALL" --check-only --from "$BASE/good/"
  run "--from loopback file bị sửa" 1 "không khớp" -- bash "$INSTALL" --check-only --from "$BASE/bad"
  run "--from loopback không có file" 1 "không tải được" -- bash "$INSTALL" --check-only --from "$BASE/missing"
  run "curl | bash" 0 "sha256 khớp" -- bash -c "curl -fsSL '$BASE/good/install.sh' | bash -s -- --check-only --from '$BASE/good'"
else
  printf 'skip  tải qua http loopback (thiếu python3 hoặc curl)\n'
fi

printf '\n%d đạt, %d hỏng\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
