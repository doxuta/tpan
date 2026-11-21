#!/usr/bin/env bash
# e2e M28, khẳng định 9 (hồi quy M14, health, backup). Chạy bằng root trong
# máy ảo, sau dung-may.sh. Dùng cho cả Apache và nginx.
#
# - Bộ L1 đến L9 của M14 (scripts/e2e-m28/tan-cong.sh, bản chép của
#   scripts/e2e-m14/tan-cong.sh) với A=e.example.com (site đã bị chiếm),
#   B=a.example.com (nạn nhân): dòng TONG có lo=0 (không đường nào còn lộ).
# - tpan health không có dòng [lỗi].
# - tpan backup create e.example.com --yes rồi backup restore e.example.com
#   --yes thành công, site e vẫn 200.
. "$(dirname "$0")/chung.sh"
can_may
trap tra_lai EXIT
# tan-cong.sh cần .htaccess của e là file thường trước khi chạy.
tra_lai

step "9a. bộ tấn công L1 đến L9 của M14 (A=$DOM bị chiếm, B=$VICTIM nạn nhân)"
bash "$D/tan-cong.sh" "$DOM" "$VICTIM" >"$W/tancong.out" 2>&1
rc=$?
grep -E '^KQ |^TONG ' "$W/tancong.out" || { echo "  (không có dòng KQ; 300 ký tự đầu output:)"; head -c 300 "$W/tancong.out"; }
tong="$(grep -E '^TONG ' "$W/tancong.out" | tail -1)"
echo "  $tong (mã thoát $rc)"
ok "bộ phép thử chạy hết (có dòng TONG)" bash -c 'grep -qE "^TONG lo=[0-9]+ chan=[0-9]+ hong=[0-9]+$" "$1"' _ "$W/tancong.out"
ok "không đường tấn công nào còn lộ (TONG lo=0)" bash -c 'grep -qE "^TONG lo=0 " "$1"' _ "$W/tancong.out"
hong="$(sed -n 's/^TONG .*hong=\([0-9]*\)$/\1/p' <<<"$tong")"
[ "${hong:-0}" != 0 ] && note "bộ phép thử có hong=${hong} (phép dựng cảnh hỏng, không phải lỗ; xem $W/tancong.out)"

step "9b. tpan health không có [lỗi]"
tpan health >"$W/health.out" 2>&1
grep -E '^\[lỗi\]' "$W/health.out" | sed 's/^/  /' || true
nloi="$(grep -cE '^\[lỗi\]' "$W/health.out")"
echo "  số dòng [lỗi]: $nloi"
ok "tpan health không có dòng [lỗi]" test "$nloi" -eq 0

step "9c. backup create rồi restore site $DOM"
tra_lai
tpan backup create "$DOM" --yes >"$W/bk-create.out" 2>&1
rc1=$?
tail -2 "$W/bk-create.out"
ok "backup create $DOM thoát 0" test "$rc1" -eq 0
tpan backup restore "$DOM" --yes >"$W/bk-restore.out" 2>&1
rc2=$?
tail -2 "$W/bk-restore.out"
ok "backup restore $DOM thoát 0" test "$rc2" -eq 0
ok "site $DOM vẫn trả 200 sau khôi phục" web200 "$DOM"
rm -f "$W/tancong.out" "$W/health.out" "$W"/bk-*.out
