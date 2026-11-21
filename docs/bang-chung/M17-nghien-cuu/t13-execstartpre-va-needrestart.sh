# t13: ba việc còn mở sau t12.
#  a. ExecStartPre=-+ ngay trong unit DỊCH VỤ (chạy bằng root, không rào, trước
#     mỗi lần khởi động) tạo /run/mysqld: phủ cả lần khởi động do socket đánh thức
#     lẫn mọi lần restart, nên không cần ExecStartPre ở unit socket.
#  b. Cập nhật gói PHP (giả lập bằng cài lại php8.3-fpm): dịch vụ của lồng có được
#     khởi động lại không (needrestart của Ubuntu), hay chạy tiếp bản đã bị thay.
#  c. Chép file cgroup thật làm mẫu cho unit test của ReadUsage: một lồng đang
#     chạy, một slice chưa có cgroup, một lồng vừa chạm trần RAM và trần tiến trình.
S=$SA; SVC=tpan-cage-$S-php.service; SK=tpan-cage-$S-php.socket
F=/etc/systemd/system/tpan-cage-$S-.service.d/10-hang-rao.conf
CG=/sys/fs/cgroup/tpan.slice/tpan-$S.slice
long_go $A >/dev/null 2>&1; long_dung $A 200 50 40; long_vao $A >/dev/null
sed -i 's|^BindReadOnlyPaths=/run/mysqld:/var/run/mysqld|BindReadOnlyPaths=-/run/mysqld:/var/run/mysqld|; s|^BindReadOnlyPaths=/run/mysqld$|BindReadOnlyPaths=-/run/mysqld|; s|^BindReadOnlyPaths=/run/systemd/resolve|BindReadOnlyPaths=-/run/systemd/resolve|' $F
sed -i 's|^\[Service\]|[Service]\nExecStartPre=-+/usr/bin/install -d -m 0755 -o mysql -g root /run/mysqld|' /etc/systemd/system/$SVC
systemctl daemon-reload; systemctl restart $SVC
echo "## a. ExecStartPre=-+ trong unit dịch vụ"
echo "unit: $(grep ExecStartPre /etc/systemd/system/$SVC) | A HTTP $(code_a '')"
mt(){ local p; p=$(systemctl show -p MainPID --value $SVC); [ "$p" != 0 ] && grep -c ' /run/mysqld ' /proc/$p/mountinfo || echo 'không có tiến trình'; }
systemctl stop $SVC; systemctl stop mariadb; rm -rf /run/mysqld
( sleep 1; systemctl start mariadb ) &
c1=$(code_a '' --max-time 30); wait; sleep 2
echo "MariaDB tắt và /run/mysqld bị xoá, request đánh thức lồng: HTTP $c1 | sau khi MariaDB lên, không đụng lồng: HTTP $(code_a '') | mountinfo có /run/mysqld: $(mt) | NRestarts=$(systemctl show -p NRestarts --value $SVC)"
rm -rf /run/mysqld.x; systemctl stop mariadb; rm -rf /run/mysqld; systemctl restart $SVC; systemctl start mariadb; sleep 2
echo "restart lồng lúc /run/mysqld không có: lồng $(systemctl is-active $SVC), mountinfo $(mt) | A HTTP $(code_a '')"
echo "/run/mysqld: $(stat -c '%a %U:%G' /run/mysqld)"
chmod 0700 /run/mysqld; systemctl restart $SVC; echo "install -d trên thư mục đã có (đổi quyền về 0700 trước): sau khi restart lồng /run/mysqld là $(stat -c '%a %U:%G' /run/mysqld)"; chmod 0755 /run/mysqld

echo; echo "## b. Cài lại php8.3-fpm khi lồng đang chạy"
echo "needrestart: $(dpkg-query -W -f '${Status} ${Version}' needrestart 2>&1) | cấu hình restart: $(grep -rhE '^\s*\$nrconf\{restart\}' /etc/needrestart/ 2>/dev/null | tr '\n' ' ')"
code_a '' >/dev/null
P1=$(systemctl show -p MainPID --value $SVC); Q1=$(systemctl show -p MainPID --value php8.3-fpm)
echo "trước: lồng MainPID=$P1 exe=$(readlink /proc/$P1/exe) | php8.3-fpm MainPID=$Q1"
DEBIAN_FRONTEND=noninteractive apt-get install -y --reinstall -o DPkg::Lock::Timeout=120 php8.3-fpm > /tmp/re.txt 2>&1; echo "apt-get install --reinstall: mã $? | $(grep -iE 'restart|needrestart|Scanning' /tmp/re.txt | tail -3 | tr '\n' ' ' | cut -c1-250)"
sleep 2
P2=$(systemctl show -p MainPID --value $SVC); Q2=$(systemctl show -p MainPID --value php8.3-fpm)
echo "sau: lồng MainPID=$P2 exe=$(readlink /proc/$P2/exe 2>&1) | php8.3-fpm MainPID=$Q2 | A HTTP $(code_a '')"
echo "needrestart -r l (chỉ liệt kê): $(needrestart -r l -b 2>/dev/null | grep -E 'NEEDRESTART-SVC' | tr '\n' ' ' | cut -c1-300)"
systemctl restart $SVC; P3=$(systemctl show -p MainPID --value $SVC); echo "sau khi restart lồng: exe=$(readlink /proc/$P3/exe)"

echo; echo "## c. Mẫu file cgroup"
OUT=/root/cgroup-mau; rm -rf $OUT; mkdir -p $OUT/dang-chay $OUT/cham-tran
code_a '' >/dev/null
for f in memory.current memory.peak memory.max memory.swap.max memory.events memory.stat cpu.stat cpu.max pids.current pids.peak pids.max pids.events io.stat io.max; do cat $CG/$f > $OUT/dang-chay/$f 2>&1; done
ls $CG/$SVC/pids.events >/dev/null 2>&1 && cat $CG/$SVC/pids.events > $OUT/dang-chay/dich-vu.pids.events
# chạm trần RAM và trần tiến trình
echo '<?php ini_set("memory_limit","-1"); $a=[]; for($i=0;$i<40;$i++){ $a[]=str_repeat("x", 10*1024*1024); } echo "xong";' | put_php $A m.php
echo '<?php exec("i=0; while [ \$i -lt 100 ]; do sleep 20 >/dev/null 2>&1 & i=\$((i+1)); done"); echo "xong";' | put_php $A f.php
code_a m.php >/dev/null; code_a f.php >/dev/null
for f in memory.current memory.peak memory.max memory.events cpu.stat cpu.max pids.current pids.peak pids.max pids.events io.stat io.max; do cat $CG/$f > $OUT/cham-tran/$f 2>&1; done
cat $CG/$SVC/pids.events > $OUT/cham-tran/dich-vu.pids.events
echo "đã chép: $(ls $OUT/dang-chay | wc -l) và $(ls $OUT/cham-tran | wc -l) file"
echo "--- dang-chay/memory.events"; cat $OUT/dang-chay/memory.events
echo "--- cham-tran/memory.events"; cat $OUT/cham-tran/memory.events
echo "--- cham-tran/pids.events (slice) và dich-vu.pids.events"; cat $OUT/cham-tran/pids.events; cat $OUT/cham-tran/dich-vu.pids.events
echo "--- cham-tran/cpu.stat"; cat $OUT/cham-tran/cpu.stat
echo "--- cham-tran/io.stat"; cat $OUT/cham-tran/io.stat
echo "--- cham-tran/memory.peak, pids.peak, cpu.max, io.max"; cat $OUT/cham-tran/memory.peak $OUT/cham-tran/pids.peak $OUT/cham-tran/cpu.max $OUT/cham-tran/io.max
sleep 21
long_go $A >/dev/null 2>&1
echo "slice sau khi gỡ lồng: $(test -d $CG && echo con || echo khong-con) (mẫu 'chưa có cgroup' = thư mục không tồn tại)"
tar -C /root -czf /home/ubuntu/cgroup-mau.tgz cgroup-mau && chown ubuntu /home/ubuntu/cgroup-mau.tgz
rm -f /var/www/$A/public/m.php /var/www/$A/public/f.php
echo "sau khi dọn: A HTTP $(code_a ''), B HTTP $(code_b '')"
