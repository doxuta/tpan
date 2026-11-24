#!/usr/bin/env bash
# e2e M28 đợt 2 nhóm B, P7: bản cũ suy chủ của job từ TÊN (Target): job trên
# user có Target là tên user, mà DB rời của khách tên "<khách>_<tên>". khach1
# tạo DB rời "p7y" (tên đầy đủ khach1_p7y) thì thành chủ của job admin khoá
# user khach1_p7y, đọc được job đó. Chạy bằng root trong máy ảo, sau
# dung-may.sh (khach1 vai user).
#
# Tham số:
#   chan (mặc định)  khach1 không tạo được DB rời trùng tên user (409); job khoá
#                    user không hiện trong GET /jobs của khach1 và GET
#                    /jobs/{id} trả 404; ca hợp lệ: DB rời tên khác vẫn tạo
#                    được và job của nó vẫn thuộc khach1.
#   lo               bản chưa vá: khach1 thấy và đọc được job của admin.
. "$(dirname "$0")/chung2.sh"
MODE="${1:-chan}"
can_may
# P1, P2 chạy trước có thể đã khoá khoá nguồn uid:0 (curl của root nối qua
# loopback): gỡ mọi khoá để đăng nhập của phép thử này không bị 423.
tpan admin unlock >/dev/null 2>&1 || true
[ -s /root/m28-admin.pw ] || bail "chưa có /root/m28-admin.pw"
U=khach1_p7y
O="$W/p7.out"
trap 'rm -f "$O"' EXIT

# sqlq <câu> [tham số...]: đọc tpan.db (chỉ đọc), in một giá trị.
sqlq() {
  python3 -I -W ignore -c '
import sqlite3, sys
c = sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True)
r = c.execute(sys.argv[1], sys.argv[2:]).fetchone()
print(r[0] if r else "")' "$@"
}
# jobid <file>: job_id trong câu trả lời 202.
jobid() { python3 -I -c 'import json,sys; print(json.load(open(sys.argv[1])).get("job_id", ""))' "$1" 2>/dev/null; }
# cho_job <id>: chờ job xong (tối đa 120 giây), in trạng thái.
cho_job() {
  local st=""
  for _ in $(seq 1 120); do
    st="$(sqlq "SELECT status FROM jobs WHERE id = ?" "$1")"
    case "$st" in running | queued | "") sleep 1 ;; *) break ;; esac
  done
  echo "$st"
}

step "P7.0. admin tạo user $U (vai user); khach1 xin tạo DB rời p7y (tên đầy đủ $U): $MODE"
tpan user add "$U" --yes --json >/dev/null 2>&1
ok "user $U có trong tpan" bash -c 'tpan user info "$1" >/dev/null 2>&1' _ "$U"
login khach1 /root/m28-khach1.pw >/dev/null
call khach1 POST "/dbs" "$O" 120 '{"name":"p7y"}'
if [ "$MODE" = lo ]; then
  ok "bản chưa vá: khach1 tạo được DB rời trùng tên user $U (HTTP $HTTP)" test "$HTTP" = 202
else
  ok "khach1 không tạo được DB rời trùng tên user $U: 409 (HTTP $HTTP)" test "$HTTP" = 409
fi
j="$(jobid "$O")"
[ -n "$j" ] && echo "  job tạo DB: $j, xong: $(cho_job "$j")"
echo "  dòng DB rời $U trong tpan.db: $(sqlq "SELECT COUNT(*) FROM databases WHERE name = ?" "$U")"

step "P7.1. admin khoá user $U qua API"
ok "admin đăng nhập được" login admin /root/m28-admin.pw
call admin POST "/users/$U/suspend" "$O" 120 '{"reason":"e2e P7"}'
ok "admin gửi khoá user $U: 202 (HTTP $HTTP)" test "$HTTP" = 202
J="$(jobid "$O")"
echo "  job khoá user: $J, xong: $(cho_job "$J"), chủ ghi trong tpan.db: $(sqlq "SELECT owner FROM jobs WHERE id = ?" "$J")"

step "P7.2. khach1 tìm job khoá user của admin"
call khach1 GET "/jobs?limit=200" "$O" 60
co_job="$(python3 -I -c 'import json,sys; d=json.load(open(sys.argv[1])); js=d if isinstance(d, list) else d.get("jobs", []); print(sum(1 for j in js if j.get("id") == sys.argv[2]))' "$O" "$J" 2>/dev/null)"
echo "  job $J trong GET /jobs của khach1: ${co_job:-?} lần"
call khach1 GET "/jobs/$J" "$O" 60
if [ "$MODE" = lo ]; then
  ok "bản chưa vá: khach1 đọc được job khoá user của admin (HTTP $HTTP, bộ thử bắt được lỗ)" test "$HTTP" = 200
else
  ok "khach1 không đọc được job khoá user của admin: 404 (HTTP $HTTP)" test "$HTTP" = 404
  ok "job đó không có trong GET /jobs của khach1" test "${co_job:-x}" = 0
fi

step "P7.3. ca hợp lệ: khach1 tạo DB rời p7z (không trùng tên user nào), job thuộc khach1"
call khach1 POST "/dbs" "$O" 120 '{"name":"p7z"}'
ok "khach1 tạo được DB rời p7z (HTTP $HTTP)" test "$HTTP" = 202
j="$(jobid "$O")"
echo "  job: $j, xong: $(cho_job "$j")"
ok "chủ của job tạo DB khach1_p7z là khach1" test "$(sqlq "SELECT owner FROM jobs WHERE id = ?" "$j")" = khach1
call khach1 GET "/jobs/$j" "$O" 60
ok "khach1 đọc được job của chính mình (HTTP $HTTP)" test "$HTTP" = 200
ok "DB rời khach1_p7z có trong tpan.db" test "$(sqlq "SELECT COUNT(*) FROM databases WHERE name = 'khach1_p7z'")" = 1
