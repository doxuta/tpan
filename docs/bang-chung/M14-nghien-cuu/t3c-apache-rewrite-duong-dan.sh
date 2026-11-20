#!/bin/bash
# Apache: .htaccess của A dùng RewriteRule trỏ thẳng vào đường dẫn file của B (không cần symlink).
# Chạy trên Mac:  multipass exec tpan-nc -- sudo bash < t3c-apache-rewrite-duong-dan.sh
set -u
A=a.example.com; B=b.example.com; DA=/var/www/$A; DB=/var/www/$B
UA=$(stat -c %U $DA); UB=$(stat -c %U $DB)
VH=/etc/apache2/sites-available/tpan-$A.conf; HT=$DA/public/.htaccess
ca(){ local p=$1; shift; curl -s --max-time 10 --resolve $A:80:127.0.0.1 "$@" "http://$A/$p"; }
cp -p $HT /root/ht.goc; cp -p $VH /root/vh.goc
printf 'NOI_DUNG_RIENG_CUA_B\n' > $DB/public/rieng.txt
printf '<?php $khoa = "KHOA_API_TRONG_MA_NGUON_CUA_B";\n' > $DB/public/cau-hinh-rieng.php
chown $UB:www-data $DB/public/rieng.txt $DB/public/cau-hinh-rieng.php; chmod 640 $DB/public/rieng.txt $DB/public/cau-hinh-rieng.php
{ cat <<EOF
RewriteEngine On
RewriteRule ^lay\$ $DB/public/rieng.txt [L]
RewriteRule ^nguon\$ $DB/public/cau-hinh-rieng.php [H=default-handler,T=text/plain,L]
EOF
cat /root/ht.goc; } > $HT; chown $UA:www-data $HT; chmod 640 $HT
thu(){ echo "  /lay   -> HTTP $(ca lay -o /dev/null -w '%{http_code}') | $(ca lay | head -c 60 | tr '\n' ' ')"
       echo "  /nguon -> HTTP $(ca nguon -o /dev/null -w '%{http_code}') | $(ca nguon | head -c 70 | tr '\n' ' ')"; }
echo "## Vhost gốc của TPAN"; thu
echo "## Thêm vào vhost của A: <Directory /var/www> Require all denied (đặt trước khối Directory của chính site)"
python3 - "$VH" "$DA" <<'PY'
import sys
p, da = sys.argv[1], sys.argv[2]
s = open(p).read()
k = '    <Directory "%s/public">' % da
assert k in s
s = s.replace(k, '    <Directory "/var/www">\n        Require all denied\n    </Directory>\n' + k, 1)
open(p, "w").write(s)
PY
apache2ctl configtest 2>&1 | tail -1; systemctl reload apache2; sleep 1; thu
echo "  trang chủ A còn chạy: HTTP $(ca '' -o /dev/null -w '%{http_code}') | file tĩnh của A: HTTP $(ca wp-includes/css/dist/block-library/style.min.css -o /dev/null -w '%{http_code}')"
cp -p /root/ht.goc $HT; cp -p /root/vh.goc $VH; rm -f /root/ht.goc /root/vh.goc $DB/public/rieng.txt $DB/public/cau-hinh-rieng.php
apache2ctl configtest 2>&1 | tail -1; systemctl reload apache2; echo "đã trả về"
