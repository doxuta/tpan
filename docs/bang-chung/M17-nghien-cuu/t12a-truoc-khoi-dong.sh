# t12a: thử thật lúc máy khởi động: một unit gọi site A NGAY sau khi nginx lên và
# TRƯỚC khi MariaDB được phép chạy (Before=mariadb.service), tức đúng lúc
# /run/mysqld chưa có. t12b đọc kết quả sau khi máy lên.
# Cách chữa đang thử: unit .socket của lồng tự tạo /run/mysqld trước khi nghe
# (ExecStartPre chạy bằng root, tham số cố định), và các lệnh gắn có dấu "-".
S=$SA; SVC=tpan-cage-$S-php.service; SK=tpan-cage-$S-php.socket
F=/etc/systemd/system/tpan-cage-$S-.service.d/10-hang-rao.conf
long_go $A >/dev/null 2>&1; long_dung $A 256 0 0; long_vao $A >/dev/null
sed -i 's|^BindReadOnlyPaths=/run/mysqld:/var/run/mysqld|BindReadOnlyPaths=-/run/mysqld:/var/run/mysqld|; s|^BindReadOnlyPaths=/run/mysqld$|BindReadOnlyPaths=-/run/mysqld|; s|^BindReadOnlyPaths=/run/systemd/resolve|BindReadOnlyPaths=-/run/systemd/resolve|' $F
if [ "${KHONG_CHUA:-0}" = 1 ]; then echo "(lượt đối chứng: KHÔNG có ExecStartPre trong unit socket)"; else
  sed -i 's|^\[Socket\]|[Socket]\nExecStartPre=-/usr/bin/install -d -m 0755 -o mysql -g root /run/mysqld|' /etc/systemd/system/$SK
fi
cat > /etc/systemd/system/t12-goi-som.service <<EOF
[Unit]
Description=t12: goi site A truoc khi MariaDB chay
After=nginx.service $SK network-online.target
Before=mariadb.service
[Service]
Type=oneshot
ExecStart=/bin/bash -c 'echo "luc goi: /run/mysqld \$(test -d /run/mysqld && echo CO || echo CHUA-CO), mariadb \$(systemctl is-active mariadb)" > /root/t12.txt; echo "HTTP \$(curl -s -o /dev/null -w %%{http_code} --max-time 20 --resolve $A:80:127.0.0.1 http://$A/)" >> /root/t12.txt'
[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload; systemctl enable --quiet t12-goi-som.service
rm -f /root/t12.txt
echo "trước khi khởi động lại: A HTTP $(code_a '') | socket $(systemctl is-enabled $SK)"
grep -n ExecStartPre /etc/systemd/system/$SK
