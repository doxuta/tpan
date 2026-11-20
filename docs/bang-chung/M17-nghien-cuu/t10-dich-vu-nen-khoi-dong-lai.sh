# t10: lồng gắn (bind) vài thứ dưới /run vào không gian riêng của nó. Nếu dịch vụ
# đứng sau thứ đó khởi động lại và TẠO LẠI thư mục hay socket, lồng đang chạy có
# còn thấy bản mới không?
#  a. MariaDB restart, và stop rồi start: WordPress trong lồng còn nối DB không.
#  b. Lồng khởi động lúc MariaDB đang tắt (như lúc máy vừa lên), rồi MariaDB mới chạy.
#  c. systemd-resolved restart: phân giải tên trong lồng.
#  d. systemd-journald restart: log của FPM còn vào journal không.
#  e. Thư mục /run/systemd/journal có gì đọc ra được tên unit của site khác không.
S=$SA; SVC=tpan-cage-$S-php.service
long_go $A >/dev/null 2>&1; long_dung $A 256 0 0; long_vao $A >/dev/null
echo "trước: A HTTP $(code_a '')"
echo "mariadb.service: $(systemctl cat mariadb 2>/dev/null | grep -iE 'RuntimeDirectory|PIDFile' | tr '\n' ' ') | tmpfiles: $(grep -rh mysqld /usr/lib/tmpfiles.d/ 2>/dev/null | tr '\n' ' ')"
echo "resolved: $(systemctl cat systemd-resolved | grep -i RuntimeDirectory | tr '\n' ' ') | journald: $(systemctl cat systemd-journald | grep -i RuntimeDirectory | tr '\n' ' ')"

echo; echo "## a. MariaDB khởi động lại khi lồng đang chạy"
i0=$(stat -c %i /run/mysqld); systemctl restart mariadb; sleep 2
echo "restart: inode /run/mysqld $i0 -> $(stat -c %i /run/mysqld) | A HTTP $(code_a '') | B (ngoài lồng) HTTP $(code_b '')"
systemctl stop mariadb; sleep 1; echo "stop: /run/mysqld $(test -d /run/mysqld && echo con || echo MAT) | A HTTP $(code_a '')"
systemctl start mariadb; sleep 2
echo "start lại: inode $(stat -c %i /run/mysqld) | A HTTP $(code_a '') | B HTTP $(code_b '') | wp trong lồng: $(run_in $A wp db query 'SELECT 1' --skip-column-names --path=/var/www/$A/public 2>&1 | tail -1)"

echo; echo "## b. Lồng khởi động lúc MariaDB đang tắt"
systemctl stop $SVC; systemctl stop mariadb; sleep 1
echo "MariaDB tắt, /run/mysqld $(test -d /run/mysqld && echo con || echo MAT). Gọi A (đánh thức lồng): HTTP $(code_a '') | dịch vụ lồng: $(systemctl is-active $SVC)"
systemctl start mariadb; sleep 2
echo "MariaDB chạy lại, KHÔNG restart lồng: A HTTP $(code_a '')"
systemctl restart $SVC; echo "sau khi restart lồng: A HTTP $(code_a '')"

echo; echo "## c. systemd-resolved khởi động lại"
i0=$(stat -c %i /run/systemd/resolve); systemctl restart systemd-resolved; sleep 1
echo "inode /run/systemd/resolve $i0 -> $(stat -c %i /run/systemd/resolve) | phân giải trong lồng: $(run_in $A getent hosts api.wordpress.org | head -1 | cut -c1-40) | từ PHP của A: $(echo '<?php echo gethostbyname("api.wordpress.org");' | put_php $A d.php; ca d.php)"

echo; echo "## d. systemd-journald khởi động lại"
i0=$(stat -c %i /run/systemd/journal/dev-log); systemctl restart systemd-journald; sleep 1
echo "inode dev-log $i0 -> $(stat -c %i /run/systemd/journal/dev-log)"
systemctl reload $SVC; sleep 2
echo "dòng log của FPM sau khi journald restart (reload sinh dòng mới): $(journalctl -u $SVC --since '-5s' --no-pager -o cat | grep -c 'reloading\|ready to handle\|using inherited')"
echo "A HTTP $(code_a '') | dịch vụ: $(systemctl is-active $SVC)"

echo; echo "## e. /run/systemd/journal có gì"
ls -la /run/systemd/journal/ | head -12
echo "streams: $(ls /run/systemd/journal/streams | wc -l) file, quyền $(stat -c '%a %U:%G' /run/systemd/journal/streams/* 2>/dev/null | sort | uniq -c | tr '\n' ' ')"
f=$(ls /run/systemd/journal/streams | head -1); echo "user của A đọc một file streams: $(runuser -u $UA -- head -c 80 /run/systemd/journal/streams/$f 2>&1 | tr '\n' ' ')"

echo; echo "## Dọn"
rm -f /var/www/$A/public/d.php
long_go $A
echo "sau khi dọn: A HTTP $(code_a ''), B HTTP $(code_b '') | mariadb $(systemctl is-active mariadb)"
