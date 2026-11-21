#!/usr/bin/env bash
# e2e M18, phần B và H: mở một gói panel (tpan-*.tar.gz.age) bằng lệnh `age` của
# apt, không qua tpan, để chứng minh gói mở được độc lập. Chạy TRONG máy ảo
# bằng root:
#
#   bash /root/e2e-m18/mo-goi-panel.sh <file .age> <thư mục ra> [<id bản sao lưu>]
#
# Khoá sao lưu lấy bằng `tpan backup key show --reveal --json`, ghi vào một file
# 0600 rồi đưa vào stdin của `age -d` qua một terminal giả (`script`), vì age
# chỉ đọc mật khẩu từ terminal (bản thử 12-age-doi-xung-va-gz.sh). Bản ghi của
# terminal giả có thể chứa khoá (terminal vọng lại chữ gõ vào trước khi age tắt
# vọng), nên bị xoá ngay, không bao giờ in. Gói giải ra rồi giải nén vào thư
# mục ra; có id thì đọc dòng đó trong tpan.db của gói.
set -u
f=$1
out=$2
id=${3:-}
rm -rf "$out"
install -d -m 0700 "$out"
k="$out/.khoa"
(umask 077 && tpan backup key show --reveal --json </dev/null 2>/dev/null | jq -r '.key // empty' >"$k")
if [ ! -s "$k" ]; then
  echo "khong-doc-duoc-khoa-sao-luu"
  rm -f "$k"
  exit 1
fi
timeout 120 script -qec "age -d -o $out/panel.tar.gz $f" /dev/null <"$k" >"$out/.age.txt" 2>&1
rc=$?
echo "age-ma=$rc ($(age --version 2>/dev/null))"
if [ "$rc" -ne 0 ] || [ ! -s "$out/panel.tar.gz" ]; then
  # Chỉ in phần lỗi, đã thay khoá.
  tr -d '\r' <"$out/.age.txt" | grep -v -F -f "$k" | tail -3
  rm -f "$k" "$out/.age.txt"
  exit 1
fi
rm -f "$k" "$out/.age.txt"
tar -xzf "$out/panel.tar.gz" -C "$out"
rm -f "$out/panel.tar.gz"
echo "tep-trong-goi: $(cd "$out" && find . -type f | sed 's|^\./||' | sort | tr '\n' ' ')"
[ -f "$out/manifest.json" ] && echo "manifest: $(jq -c '{format, hostname, tpan_version, so_migration: (.migrations | length)}' "$out/manifest.json" 2>&1)"
if [ -n "$id" ] && [ -f "$out/tpan.db" ]; then
  python3 /root/e2e-m18/jobs.py --db "$out/tpan.db" row "$id"
fi
