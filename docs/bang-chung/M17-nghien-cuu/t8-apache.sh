# t8: stack Apache. Chuyển web server bằng chính tpan (mọi site ở ngoài lồng), rồi
# đưa A vào lồng bằng tay: Apache (mod_proxy_fcgi) nối được socket do systemd giữ
# không, permalink và .htaccess còn chạy không, restart dưới tải có rớt request không.
# Cuối cùng trả về nginx.
for x in a b c d e f g h i j; do long_go $x.example.com >/dev/null 2>&1; done
echo "## 0. Chuyển sang Apache"
tpan stack switch --to apache --yes > /tmp/sw.log 2>&1; echo "tpan stack switch --to apache: mã $? | $(tail -1 /tmp/sw.log | cut -c1-150)"
echo "apache2: $(systemctl is-active apache2) | nginx: $(systemctl is-active nginx) | $(apache2 -v | head -1) | MPM: $(apachectl -M 2>/dev/null | grep mpm_ | tr -d ' ')"
echo "A HTTP $(code_a '') | B HTTP $(code_b '')"

echo; echo "## 1. Đưa A vào lồng trên Apache"
long_dung $A 256 100 60
VH=/etc/apache2/sites-available/tpan-$A.conf; cp -p $VH /root/vh-apache-goc
SOCK=$(sock_of $A)
sed -i "s|proxy:unix:[^|]*|proxy:unix:$SOCK|" $VH
grep -n "SetHandler" $VH | head -2
apachectl configtest 2>&1 | tail -1; systemctl reload apache2; sleep 1
echo '<?php echo "user=".posix_getpwuid(posix_geteuid())["name"]." www=".trim((string)@shell_exec("ls /var/www | tr \"\\n\" \" \""))." apache=[".trim((string)@shell_exec("ls /etc/apache2 2>&1 | head -2 | tr \"\\n\" \" \""))."]";' | put_php $A v.php
echo "trang chủ: HTTP $(code_a '') | $(ca v.php)"
echo "permalink /hello-world/: HTTP $(code_a 'hello-world/') | trang không có: HTTP $(code_a 'khong-co-trang-nay/') | wp-login.php: HTTP $(code_a 'wp-login.php')"
echo "dịch vụ của lồng: $(systemctl is-active tpan-cage-$SA-php.service) | tiến trình: $(ps -eo user:28,cmd | grep '[p]hp-fpm: master' | grep $UA | cut -c1-110)"
echo '<?php usleep(150000); echo "ok";' | put_php $A w.php
( for i in $(seq 1 60); do code_a w.php; echo; done > /tmp/rs.txt ) &
sleep 1.5; systemctl restart tpan-cage-$SA-php.service; wait
echo "restart lồng dưới tải (60 request): $(sort /tmp/rs.txt | uniq -c | tr '\n' ' ')"
echo "ngủ rồi thức: $(systemctl stop tpan-cage-$SA-php.service 2>/dev/null; sleep 1; ca '' -o /dev/null -w 'HTTP %{http_code}, %{time_total} giây')"
tail -2 /var/log/tpan/sites/$A/error.log | cut -c1-200

echo; echo "## Dọn: trả vhost, gỡ lồng, về nginx"
rm -f /var/www/$A/public/{v,w}.php
cp -p /root/vh-apache-goc $VH; rm -f /root/vh-apache-goc; apachectl configtest 2>&1 | tail -1; systemctl reload apache2
long_go $A
echo "A trên Apache, ngoài lồng: HTTP $(code_a '')"
tpan stack switch --to nginx --yes > /tmp/sw.log 2>&1; echo "tpan stack switch --to nginx: mã $? | $(tail -1 /tmp/sw.log | cut -c1-150)"
echo "sau khi dọn: A HTTP $(code_a ''), B HTTP $(code_b '') | nginx: $(systemctl is-active nginx)"
