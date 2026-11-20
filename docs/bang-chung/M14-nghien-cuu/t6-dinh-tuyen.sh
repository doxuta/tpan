#!/bin/bash
# Nhiều site trên một máy: request nào về site nào (stack nginx, vhost thật của TPAN).
# Chạy trên Mac:  multipass exec tpan-nc -- sudo bash < t6-dinh-tuyen.sh
set -u
A=a.example.com; B=b.example.com
g(){ # g <Host> [tham số curl]: in mã HTTP và site nào trả lời (tìm tên miền trong thân trang)
  local h=$1; shift; local r; r=$(curl -s --max-time 8 -o /tmp/than -w '%{http_code}' -H "Host: $h" "$@" http://127.0.0.1/ 2>/dev/null)
  local ai="không ai"; grep -q "//$A" /tmp/than 2>/dev/null && ai="site A"; grep -q "//$B" /tmp/than 2>/dev/null && ai="site B"; grep -q "DAY LA TRANG LA" /tmp/than 2>/dev/null && ai="TRANG LẠ (không phải site A)"
  printf '  Host: %-28s -> HTTP %s, %s\n' "$h" "$r" "$ai"; }
echo "## 1. Cùng một IP 127.0.0.1, chỉ đổi header Host"
g $A; g $B; g A.EXAMPLE.COM; g "$A."; g "$A:80"; g www.$A; g khongco.vn; g 127.0.0.1
echo -n "  không gửi Host (HTTP/1.0): "; printf 'GET / HTTP/1.0\r\n\r\n' | nc -w 3 127.0.0.1 80 | head -c 80 | wc -c | sed 's/$/ byte trả về/'
echo "  (HTTP 000 = nginx đóng kết nối không trả gì, tức mã 444 trong vhost mặc định)"
echo; echo "## 2. Bảng định tuyến nginx đang dùng"
nginx -T 2>/dev/null | awk '/# configuration file/{f=$4} /server_name|^\s*root |fastcgi_pass|listen .*default_server/{sub(/^ +/,""); print "  " f " " $0}' | sed 's|/etc/nginx/sites-enabled/||' | grep -v "^  /etc/nginx/nginx.conf"
echo; echo "## 3. Ai đó thêm tay một vhost trùng tên miền của site A (ngoài TPAN)"
mkdir -p /srv/trang-la; echo "DAY LA TRANG LA" > /srv/trang-la/index.html
printf 'server {\n    listen 80;\n    server_name %s;\n    root /srv/trang-la;\n}\n' $A > /etc/nginx/sites-enabled/aaa-trung.conf
echo "  nginx -t: mã thoát $(nginx -t >/dev/null 2>&1; echo $?) | cảnh báo: $(nginx -t 2>&1 | grep -c 'conflicting server name') dòng 'conflicting server name'"
nginx -t 2>&1 | grep conflicting | head -1 | cut -c1-160 | sed 's/^/  /'
systemctl reload nginx; sleep 1; g $A
echo "  tpan health có thấy không:"; tpan health 2>&1 | grep -i -E "trùng|conflict|aaa-trung|$A" | head -5 | cut -c1-200 | sed 's/^/   /'; echo "   (hết)"
rm -f /etc/nginx/sites-enabled/aaa-trung.conf; rm -rf /srv/trang-la /tmp/than; systemctl reload nginx; sleep 1; g $A
