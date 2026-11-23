#!/usr/bin/env bash
# e2e M28, khẳng định 8 (admin). Chạy bằng root trong máy ảo, sau dung-may.sh.
# public/.htaccess của site e là symlink tới /etc/shadow; admin gọi
# POST /api/v1/files/accept với body {"path": "<.htaccess>"}.
#
# Phải: không phải HTTP 204 (sau bản vá là 409); tpan còn sống. handleAcceptFile
# chỉ nhận path có trong bảng managed_files, nên trước hết đưa .htaccess vào sổ
# đó (precondition), nếu không thì accept trả 400 và khẳng định "không 204"
# đạt một cách rỗng tuếch.
. "$(dirname "$0")/chung.sh"
can_may
trap tra_lai EXIT
[ "$WEB" = apache ] || bail "khẳng định 8 chỉ cho stack Apache"
login admin /root/m28-admin.pw || bail "admin không đăng nhập được"
p0="$(pid)"
n0="$(nrestarts)"

step "8a. precondition: đưa .htaccess vào managed_files (lúc còn là file thường)"
tra_lai
# RecordFile qua acceptFile cần file thường, chủ đúng: chấp nhận lúc này để có
# dòng trong sổ, rồi mới tráo thành symlink.
call admin POST /files/accept "$W/acc.pre" 30 "{\"path\":\"$HT\"}"
echo "  accept lần đầu (file thường): HTTP $HTTP"
inrows="$(python3 -c 'import sqlite3,sys
c=sqlite3.connect("file:/var/lib/tpan/tpan.db?mode=ro", uri=True)
print(c.execute("SELECT count(*) FROM managed_files WHERE path=?", (sys.argv[1],)).fetchone()[0])' "$HT")"
echo "  số dòng managed_files cho .htaccess: $inrows"
ok "precondition: .htaccess có trong managed_files (accept sắp tới không bị 400 rỗng)" test "$inrows" -ge 1

step "8b. .htaccess -> symlink tới /etc/shadow, admin POST /files/accept"
as ln -sfn /etc/shadow "$HT"
s0="$(sha256sum /etc/shadow | cut -d' ' -f1)"
call admin POST /files/accept "$W/acc.resp" 30 "{\"path\":\"$HT\"}"
ok "POST /files/accept trên symlink tới /etc/shadow: KHÔNG phải HTTP 204" test "$HTTP" != 204
echo "  (mã HTTP: $HTTP; sau bản vá mong đợi 409)"
ok "câu trả lời không chứa /etc/shadow" pt khong-lo "$W/acc.resp" /etc/shadow
s1="$(sha256sum /etc/shadow | cut -d' ' -f1)"
ok "sha256 của /etc/shadow không đổi" test "$s0" = "$s1"
ok "tpan còn sống, PID không đổi" song "$p0" "$n0" || doi_tpan
rm -f "$W/acc.pre" "$W/acc.resp"
