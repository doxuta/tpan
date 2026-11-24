# t13b: t13 cho thấy ExecStartPre=-+ trong unit dịch vụ KHÔNG làm lồng thấy
# /run/mysqld. Làm rõ: lệnh có chạy không (systemctl show -p ExecStartPre ghi mã
# của lần chạy gần nhất), và namespace của ExecStart được dựng trước hay sau nó.
S=$SA; SVC=tpan-cage-$S-php.service
F=/etc/systemd/system/tpan-cage-$S-.service.d/10-hang-rao.conf
long_go $A >/dev/null 2>&1; long_dung $A 256 0 0; long_vao $A >/dev/null
sed -i 's|^BindReadOnlyPaths=/run/mysqld:/var/run/mysqld|BindReadOnlyPaths=-/run/mysqld:/var/run/mysqld|; s|^BindReadOnlyPaths=/run/mysqld$|BindReadOnlyPaths=-/run/mysqld|' $F
mt(){ local p; p=$(systemctl show -p MainPID --value $SVC); [ "$p" != 0 ] && { grep -c ' /run/mysqld ' /proc/$p/mountinfo; true; } || echo 'không có tiến trình'; }
thu(){ # nhãn, dòng ExecStartPre
  systemctl stop $SVC; sed -i '/^ExecStartPre=/d' /etc/systemd/system/$SVC
  sed -i "s|^\[Service\]|[Service]\n$2|" /etc/systemd/system/$SVC; systemctl daemon-reload
  systemctl stop mariadb; rm -rf /run/mysqld
  systemctl start $SVC; sleep 0.5
  echo "$1: /run/mysqld sau khi lồng chạy: $(stat -c '%a %U:%G' /run/mysqld 2>&1 | cut -c1-60) | mountinfo của lồng: $(mt)"
  echo "   ExecStartPre: $(systemctl show -p ExecStartPre --value $SVC | tr ';' '\n' | grep -E 'status=|path=' | tr '\n' ' ' | cut -c1-200)"
  systemctl start mariadb; sleep 2; echo "   MariaDB lên, không đụng lồng: A HTTP $(code_a '')"
  journalctl -u $SVC --since '-10s' --no-pager -o cat | grep -iE 'install|mkdir|ExecStartPre|Failed' | head -2 | sed 's/^/   journal: /'
}
thu "ExecStartPre=-+install"   "ExecStartPre=-+/usr/bin/install -d -m 0755 -o mysql -g root /run/mysqld"
thu "ExecStartPre=+install"    "ExecStartPre=+/usr/bin/install -d -m 0755 -o mysql -g root /run/mysqld"
thu "ExecStartPre=+mkdir"      "ExecStartPre=+/usr/bin/mkdir -p /run/mysqld"
echo; echo "## Đối chứng: thư mục có sẵn trước khi lồng chạy"
systemctl stop $SVC; sed -i '/^ExecStartPre=/d' /etc/systemd/system/$SVC; systemctl daemon-reload
systemctl stop mariadb; rm -rf /run/mysqld; install -d -m 0755 -o mysql -g root /run/mysqld
systemctl start $SVC; sleep 0.5; echo "tạo tay trước: mountinfo $(mt)"; systemctl start mariadb; sleep 2; echo "   A HTTP $(code_a '')"
echo; echo "## Dọn"
long_go $A
echo "sau khi dọn: A HTTP $(code_a ''), B HTTP $(code_b '') | mariadb $(systemctl is-active mariadb) | /run/mysqld $(stat -c '%a %U:%G' /run/mysqld)"
