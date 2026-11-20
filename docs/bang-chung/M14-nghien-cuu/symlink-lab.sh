#!/bin/bash
# Thử nghiệm "symlink attack" trên máy ảo lab (Multipass), dựng lại đúng kiểu
# thư mục của TPAN: thư mục site 2750, chủ <user site>:www-data.
# Chạy trên Mac: multipass exec lab -- sudo bash < symlink-lab.sh
# Cuối script tự dọn (xoá user, thư mục, vhost thử).
set -u
R=/srv/lab-iso
echo "## 1. Dựng hai site giả: isoa và isob (2750, chủ <site>:www-data như TPAN)"
for u in isoa isob; do
  id $u >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin $u
  mkdir -p $R/$u/public
  chown -R $u:www-data $R/$u
  chmod 2750 $R/$u $R/$u/public
done
chmod 755 $R
# File bí mật của site B mà www-data đọc được qua nhóm (giống file do PHP của
# site tạo ra: setgid nên mang nhóm www-data, umask 022 nên nhóm đọc được).
echo "API_KEY_CUA_SITE_B=bi-mat-123" > $R/isob/public/config-plugin.php
chown isob:www-data $R/isob/public/config-plugin.php; chmod 640 $R/isob/public/config-plugin.php
echo "trang cua site A" > $R/isoa/public/index.html
chown isoa:www-data $R/isoa/public/index.html; chmod 640 $R/isoa/public/index.html
ls -l $R/isob/public

echo; echo "## 2. User của site A đọc thẳng file của site B (mong đợi: bị từ chối)"
runuser -u isoa -- cat $R/isob/public/config-plugin.php; echo "mã thoát: $?"

echo; echo "## 3. Site A (giả sử đã bị chiếm) tạo symlink trong thư mục của chính nó"
runuser -u isoa -- ln -sf $R/isob/public/config-plugin.php $R/isoa/public/lay.txt
ls -l $R/isoa/public/lay.txt

cat > /etc/nginx/sites-enabled/lab-iso.conf <<'EOF'
server {
    listen 8081;
    server_name _;
    root /srv/lab-iso/isoa/public;
}
EOF
nginx -t 2>&1 | tail -1 && systemctl reload nginx && sleep 2
echo; echo "## 4. Gọi qua nginx KHÔNG có disable_symlinks (nginx chạy dưới www-data)"
curl -s -w "  <- HTTP %{http_code}\n" http://127.0.0.1:8081/lay.txt

sed -i 's|root /srv/lab-iso/isoa/public;|root /srv/lab-iso/isoa/public;\n    disable_symlinks if_not_owner from=$document_root;|' /etc/nginx/sites-enabled/lab-iso.conf
nginx -t 2>&1 | tail -1 && systemctl reload nginx && sleep 2
echo; echo "## 5. Gọi lại SAU KHI thêm: disable_symlinks if_not_owner from=\$document_root;"
curl -s -o /dev/null -w "lay.txt     -> HTTP %{http_code}\n" http://127.0.0.1:8081/lay.txt
curl -s -o /dev/null -w "index.html  -> HTTP %{http_code} (file thật của site A vẫn chạy)\n" http://127.0.0.1:8081/index.html
tail -1 /var/log/nginx/error.log

echo; echo "## 6. Dọn dẹp"
rm -f /etc/nginx/sites-enabled/lab-iso.conf; systemctl reload nginx
rm -rf $R; userdel isoa; userdel isob
echo "đã dọn"
