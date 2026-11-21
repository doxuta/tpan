#!/usr/bin/env bash
# e2e M28, phần D (nâng từ v0.2.0), pha TRƯỚC khi thay file chạy. Chạy bằng
# root trong máy ảo, sau dung-may.sh trên bản v0.2.0 (cài bằng install.sh của
# gói). Stack Apache.
#
# - Lưu mục .htaccess (file thường) của bản xem trước khach1 vào
#   /root/m28-e2e/xt-truoc.json để pha SAU so lại.
# - Lưu tpan health vào /root/m28-e2e/health.truoc để pha SAU so dòng [lỗi] mới.
# - Khẳng định ba mục tiêu T1 trên v0.2.0 là LỘ (bộ thử bắt được lỗ trên bản cũ).
. "$(dirname "$0")/chung.sh"
can_may
trap tra_lai EXIT
login khach1 /root/m28-khach1.pw || bail "khach1 không đăng nhập được"

step "D0. v0.2.0: lưu mục .htaccess (file thường) của bản xem trước khach1"
tra_lai
dry "$W/xt.resp" 60
ok "v0.2.0: bản xem trước .htaccess thường HTTP 200, lưu được mục" \
  bash -c '[ "$1" = 200 ] && python3 "$2" luu-muc "$3" "$4" "$5"' _ "$HTTP" "$D/phan-tich.py" "$W/xt.resp" "$HT" "$W/xt-truoc.json"

step "D0. v0.2.0: tpan health (lưu dòng [lỗi] làm mốc)"
tpan health >"$W/health.full" 2>&1
grep -E '^\[lỗi\]' "$W/health.full" | sort >"$W/health.truoc"
echo "  số dòng [lỗi] của v0.2.0: $(grep -c . "$W/health.truoc")"

step "D0. v0.2.0: ba mục tiêu T1 phải LỘ (bộ thử bắt được lỗ trên bản cũ)"
bash "$D/t1.sh" v020-lo
rm -f "$W/xt.resp" "$W/health.full"
