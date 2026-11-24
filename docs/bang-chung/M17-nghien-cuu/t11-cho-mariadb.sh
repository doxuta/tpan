# t11: lúc máy vừa lên, request có thể đánh thức lồng TRƯỚC khi MariaDB tạo
# /run/mysqld (t10 cho thấy thư mục đó không mất khi MariaDB restart, nhưng sau
# khi máy khởi động lại thì /run trống). Giả lập: dừng MariaDB, xoá /run/mysqld,
# cho MariaDB chạy nền rồi gọi site ngay.
#  a. Hàng rào như lib.sh (gắn /run/mysqld không có dấu "-"): thư mục thiếu thì sao.
#  b. Thêm dấu "-" (thiếu thì bỏ qua): lồng chạy nhưng có thấy socket DB về sau không;
#     nhận ra tình trạng đó bằng /proc/<MainPID>/mountinfo.
#  c. Thêm After=mariadb.service vào unit dịch vụ của lồng.
S=$SA; SVC=tpan-cage-$S-php.service; SK=tpan-cage-$S-php.socket
F=/etc/systemd/system/tpan-cage-$S-.service.d/10-hang-rao.conf
gia_lap(){ # dừng lồng, dừng MariaDB, xoá /run/mysqld, chạy MariaDB ở nền, gọi A ngay
  systemctl stop $SVC 2>/dev/null; systemctl reset-failed $SVC $SK 2>/dev/null; systemctl start $SK
  systemctl stop mariadb; rm -rf /run/mysqld
  ( sleep 1; systemctl start mariadb ) &
  local c1 c2; c1=$(code_a '' --max-time 30)
  wait; sleep 2; c2=$(code_a '')
  local mp; mp=$(systemctl show -p MainPID --value $SVC)
  echo "  request ngay lúc MariaDB chưa lên: HTTP $c1 | sau khi MariaDB lên (không đụng lồng): HTTP $c2 | dịch vụ $(systemctl is-active $SVC) ($(systemctl show -p Result --value $SVC), NRestarts=$(systemctl show -p NRestarts --value $SVC)) | socket $(systemctl is-active $SK)"
  echo "  mountinfo của lồng có /run/mysqld: $( [ "$mp" != 0 ] && grep -c ' /run/mysqld ' /proc/$mp/mountinfo || echo 'không có tiến trình')"
}
long_go $A >/dev/null 2>&1; long_dung $A 256 0 0; long_vao $A >/dev/null; echo "trước: A HTTP $(code_a '')"

echo; echo "## a. Gắn /run/mysqld bắt buộc (như lib.sh)"
gia_lap
journalctl -u $SVC --since '-15s' --no-pager -o cat | grep -iE 'namespac|mount|Failed' | head -3 | sed 's/^/  journal: /'

echo; echo "## b. Gắn có dấu - (thiếu thì bỏ qua)"
sed -i 's|^BindReadOnlyPaths=/run/mysqld:/var/run/mysqld|BindReadOnlyPaths=-/run/mysqld:/var/run/mysqld|; s|^BindReadOnlyPaths=/run/mysqld$|BindReadOnlyPaths=-/run/mysqld|; s|^BindReadOnlyPaths=/run/systemd/resolve|BindReadOnlyPaths=-/run/systemd/resolve|' $F
systemctl daemon-reload
gia_lap
echo "  restart lồng: $(systemctl restart $SVC; code_a '') | mountinfo có /run/mysqld: $(grep -c ' /run/mysqld ' /proc/$(systemctl show -p MainPID --value $SVC)/mountinfo)"

echo; echo "## c. Thêm After=mariadb.service"
sed -i 's|^\[Unit\]|[Unit]\nAfter=mariadb.service|' /etc/systemd/system/$SVC; systemctl daemon-reload
gia_lap
gia_lap

echo; echo "## d. MariaDB bị tắt hẳn (disable) thì lồng có treo chờ không"
systemctl stop $SVC; systemctl stop mariadb
t0=$(date +%s.%N); c=$(code_a '' --max-time 30); t1=$(date +%s.%N)
echo "  MariaDB dừng, gọi A: HTTP $c sau $(echo "$t1 - $t0" | bc) giây (lồng: $(systemctl is-active $SVC))"
systemctl start mariadb; sleep 2; echo "  MariaDB chạy lại: A HTTP $(code_a '')"

echo; echo "## Dọn"
long_go $A
echo "sau khi dọn: A HTTP $(code_a ''), B HTTP $(code_b '') | mariadb $(systemctl is-active mariadb) | /run/mysqld: $(stat -c '%a %U:%G' /run/mysqld)"
