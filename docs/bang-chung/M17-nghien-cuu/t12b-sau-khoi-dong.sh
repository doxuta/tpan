# t12b: đọc kết quả của t12a sau khi máy khởi động lại.
S=$SA; SVC=tpan-cage-$S-php.service; SK=tpan-cage-$S-php.socket
echo "uptime: $(cut -d' ' -f1 /proc/uptime) giây"
echo "ghi lúc khởi động: $(tr '\n' ';' < /root/t12.txt 2>&1)"
mp=$(systemctl show -p MainPID --value $SVC)
echo "dịch vụ lồng: $(systemctl is-active $SVC) (NRestarts=$(systemctl show -p NRestarts --value $SVC), Result=$(systemctl show -p Result --value $SVC)) | socket: $(systemctl is-active $SK)"
echo "mountinfo của lồng có /run/mysqld: $( [ "$mp" != 0 ] && grep -c ' /run/mysqld ' /proc/$mp/mountinfo )"
echo "bây giờ (MariaDB $(systemctl is-active mariadb)), KHÔNG đụng lồng: A HTTP $(code_a '') | B HTTP $(code_b '')"
journalctl -b -u $SVC --no-pager -o cat | grep -iE 'namespac|Failed|Started|Scheduled' | head -6 | sed 's/^/  journal: /'
echo "/run/mysqld: $(stat -c '%a %U:%G' /run/mysqld)"
echo "## Dọn"
systemctl disable --quiet t12-goi-som.service; rm -f /etc/systemd/system/t12-goi-som.service /root/t12.txt
long_go $A
echo "sau khi dọn: A HTTP $(code_a ''), B HTTP $(code_b '')"
