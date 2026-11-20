#!/usr/bin/env bash
# Chạy lần lượt các kịch bản e2e trên máy ảo, mỗi kịch bản một log trong
# docs/bang-chung/hoi-quy-phat-hanh/, tóm tắt nối vào tom-tat.txt.
#
#   scripts/hoi-quy.sh                 chạy tất cả (M1 tới M12, M8 cả real, API)
#   scripts/hoi-quy.sh M3 M5 M6 ...    chỉ chạy các kịch bản đã nêu, theo thứ tự đó
#
# Tên hợp lệ: M1..M12, M8-real (Claude Code thật, tốn hạn mức gói), API.
set -u
cd "$(dirname "$0")/.."
OUT=docs/bang-chung/hoi-quy-phat-hanh
mkdir -p "$OUT"
SUM="$OUT/tom-tat.txt"

cmd_of() {
  case "$1" in
    M7) echo "scripts/e2e-m7.sh" ;;
    M8) echo "scripts/e2e-m8.sh fake" ;;
    M8-real) echo "scripts/e2e-m8.sh real" ;;
    API) echo "scripts/e2e-api.sh" ;;
    M*) echo "scripts/e2e-m${1#M}.sh all" ;;
  esac
}

run() {
  local name=$1 log="$OUT/$1.log" cmd t0 rc
  cmd="$(cmd_of "$name")"
  [ -n "$cmd" ] || { echo "không biết kịch bản $name" | tee -a "$SUM"; return; }
  t0=$(date +%s)
  echo "== $(date '+%H:%M:%S') bắt đầu $name: $cmd" | tee -a "$SUM"
  # shellcheck disable=SC2086 # cmd cố ý tách thành tên script và chế độ
  E2E_LOG="$log" $cmd </dev/null >"$log" 2>&1
  rc=$?
  echo "   $name: mã thoát $rc, đạt $(grep -c '=> ĐẠT' "$log"), không đạt $(grep -c '=> KHÔNG ĐẠT' "$log"), $(($(date +%s) - t0)) giây" | tee -a "$SUM"
}

if [ $# -eq 0 ]; then
  set -- M1 M2 M3 M4 M5 M6 M7 M8 M8-real M9 M10 M11 M12 API
fi
for n in "$@"; do run "$n"; done
echo "== $(date '+%H:%M:%S') xong: $*" | tee -a "$SUM"
