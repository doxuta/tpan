#!/bin/bash
# Bộ phép thử hồi quy của M14 (mục 6.13 của docs/KE-HOACH.md): chạy lại chín
# đường tấn công L1 đến L9 và các phép "đã thử, không lộ" giữa hai site do TPAN
# tạo. Dựng lại từ các script nghiên cứu t1 đến t9 ở docs/bang-chung/M14-nghien-cuu/.
#
# CHỈ chạy trong máy ảo test của mốc, bằng quyền root. Trên Mac, trong thư mục repo:
#
#   scripts/vm.sh root bash -s -- a.example.com b.example.com < scripts/e2e-m14/tan-cong.sh
#
# "A" (tham số 1) là site đã bị chiếm, "B" (tham số 2) là nạn nhân. Script tự
# nhận ra stack (nginx hay Apache) và bỏ qua phép thử không thuộc stack đó.
#
# Kết quả là các dòng bắt đầu bằng "KQ":
#   KQ L1 LO   <chi tiết>     đường tấn công còn mở
#   KQ L1 CHAN <chi tiết>     đã bị chặn
#   KQ L3 BO   <lý do>        không thuộc stack này
#   KQ N1 DAT / KQ N1 HONG    phép "không lộ" và phép "site vẫn chạy"
# Dòng cuối: "TONG lo=<n> chan=<n> hong=<n>".
#
# Không phá: file thử mang tên có hậu tố ngẫu nhiên, .htaccess của A được trả
# về sau mỗi phép, mọi file thử bị xoá ở cuối. Mã thử chỉ in tên user và
# CO/KHONG, không in mật khẩu hay nội dung file của B.
set -u
A=${1:?thiếu tên miền site A}; B=${2:?thiếu tên miền site B}
DA=/var/www/$A; DB=/var/www/$B
[ -d "$DA/public" ] && [ -d "$DB/public" ] || { echo "không thấy $DA/public hoặc $DB/public" >&2; exit 2; }
UA=$(stat -c %U "$DA"); UB=$(stat -c %U "$DB")
R=$(head -c 6 /dev/urandom | od -An -tx1 | tr -d ' \n')
T=/root/tpan-e2e-$R; mkdir -p "$T"
if systemctl is-active --quiet apache2; then WEB=apache; else WEB=nginx; fi
POOLB=$(grep -l "^user = $UB" /etc/php/*/fpm/pool.d/*.conf | head -1)
SOCKB=$(awk -F' = ' '/^listen = /{print $2}' "$POOLB")
VB=$(echo "$POOLB" | cut -d/ -f4)
POOLA=$(grep -l "^user = $UA" /etc/php/*/fpm/pool.d/*.conf | head -1)
VA=$(echo "$POOLA" | cut -d/ -f4)
HT=$DA/public/.htaccess
LO=0; CHAN=0; HONG=0

ca(){ local p=$1; shift; curl -s --max-time 15 --resolve "$A:80:127.0.0.1" "$@" "http://$A/$p"; }
cb(){ local p=$1; shift; curl -s --max-time 15 --resolve "$B:80:127.0.0.1" "$@" "http://$B/$p"; }
code(){ ca "$1" -o /dev/null -w '%{http_code}'; }
as_a(){ runuser -u "$UA" -- "$@"; }
kq(){ # kq <mã> <LO|CHAN|BO|DAT|HONG> <chi tiết>
  case "$2" in LO) LO=$((LO+1));; CHAN) CHAN=$((CHAN+1));; HONG) HONG=$((HONG+1));; esac
  printf 'KQ %-3s %-4s %s\n' "$1" "$2" "$3"; }
# php_a <tên file> : ghi mã PHP từ stdin vào public của A dưới user A.
php_a(){ as_a tee "$DA/public/$1" >/dev/null; }
giu_ht(){ if [ -f "$HT" ]; then cp -p "$HT" "$T/ht.goc"; else : > "$T/ht.khong-co"; fi; }
tra_ht(){ if [ -f "$T/ht.goc" ]; then cp -p "$T/ht.goc" "$HT"; else rm -f "$HT"; fi; }
# them_dau: chèn stdin lên đầu .htaccess của A (giữ phần WordPress phía sau).
them_dau(){ { cat; [ -f "$T/ht.goc" ] && cat "$T/ht.goc"; } > "$T/ht.moi"; cp "$T/ht.moi" "$HT"; chown "$UA:www-data" "$HT"; chmod 640 "$HT"; }
don(){
  [ "$WEB" = apache ] && tra_ht
  rm -f "$DA/public/"*"-$R".php "$DA/public/"*"-$R".txt "$DA/public/tu-$R" "$DA/public/.user.ini.$R" \
        "$DB/public/"*"-$R".txt "$DB/public/"*"-$R".php "$DB/public/x-$R" "/tmp/tpan-e2e-$R.php"
  rm -f "/etc/nginx/sites-enabled/aaa-trung-$R.conf" "/etc/apache2/sites-enabled/aaa-trung-$R.conf"
  rm -rf "/srv/trang-la-$R" "$T"
}
trap don EXIT

echo "## Môi trường: stack $WEB | A=$A user=$UA php=$VA | B=$B user=$UB php=$VB | hậu tố $R"
[ "$WEB" = apache ] && giu_ht

# ---- File thử ---------------------------------------------------------------
printf 'NOI_DUNG_RIENG_CUA_B\n' > "$DB/public/rieng-$R.txt"
printf '<?php $khoa = "KHOA_TRONG_MA_NGUON_CUA_B";\n' > "$DB/public/nguon-$R.php"
chown "$UB:www-data" "$DB/public/rieng-$R.txt" "$DB/public/nguon-$R.php"; chmod 640 "$DB/public/rieng-$R.txt" "$DB/public/nguon-$R.php"
php_a "o1-$R.php" <<EOF
<?php
\$s = function_exists("opcache_get_status") ? @opcache_get_status(true) : false;
\$n = 0;
if (\$s && isset(\$s["scripts"])) foreach (\$s["scripts"] as \$k => \$v) if (strpos(\$k, "$DB/") === 0) \$n++;
echo \$s ? "GOI_DUOC thay \$n file cua B\n" : "BI_CHAN\n";
EOF
php_a "o2-$R.php" <<EOF
<?php
register_shutdown_function(function(){ echo "\nKETQUA:".(defined("DB_PASSWORD")?"LAY_DUOC":"KHONG")."\n"; });
@include "$DB/public/wp-config.php";
EOF
php_a "t-$R.php" <<'EOF'
<?php echo "user=".posix_getpwuid(posix_geteuid())["name"]."|open_basedir=".ini_get("open_basedir")."|ngoai=".(@file_get_contents("/etc/hostname")!==false?"CO":"KHONG")."\n";
EOF
php_a "s-$R.php" <<'EOF'
<?php
$me = posix_getpwuid(posix_geteuid())["name"];
$n = trim((string)@shell_exec("ps -eo user= | grep -vc '^".$me."\$'"));
echo "khac=".($n === "" ? "?" : $n)."\n";
EOF

# ---- L1, L2: OPcache --------------------------------------------------------
l1=KHONG
for i in 1 2 3; do cb "" -o /dev/null; ca "o2-$R.php" | grep -q 'KETQUA:LAY_DUOC' && l1=LAY_DUOC; done
if [ "$l1" = LAY_DUOC ]; then kq L1 LO "PHP của A include wp-config.php của B: lấy được hằng DB_PASSWORD"
else kq L1 CHAN "PHP của A include wp-config.php của B: không lấy được (3 lần thử)"; fi
cb "" -o /dev/null; l2=$(ca "o1-$R.php" | tail -1)
case "$l2" in GOI_DUOC*) kq L2 LO "opcache_get_status từ A: $l2";; BI_CHAN*) kq L2 CHAN "opcache_get_status từ A: bị chặn";; *) kq L2 HONG "trả lời lạ: $(echo "$l2" | head -c 80)";; esac

# ---- L3, L4, L9: .htaccess (chỉ Apache) --------------------------------------
if [ "$WEB" = apache ]; then
  printf 'SetEnv PHP_ADMIN_VALUE "open_basedir=/"\nSetEnv PHP_VALUE "open_basedir=/"\n' | them_dau
  l3=$(ca "t-$R.php"); tra_ht
  case "$l3" in
    *"|open_basedir=/|"*|*"|ngoai=CO"*) kq L3 LO ".htaccess SetEnv PHP_ADMIN_VALUE: $l3";;
    *"|open_basedir=$DA/|ngoai=KHONG"*) kq L3 CHAN ".htaccess SetEnv PHP_ADMIN_VALUE: open_basedir giữ nguyên";;
    *) kq L3 CHAN ".htaccess SetEnv PHP_ADMIN_VALUE: site A tự hỏng (HTTP $(code "t-$R.php") sau khi trả .htaccess), open_basedir không đổi";;
  esac

  as_a tee "/tmp/tpan-e2e-$R.php" >/dev/null <<EOF
<?php echo "MA_CUA_A chay duoi user=".posix_getpwuid(posix_geteuid())["name"]."|doc wp-config cua B=".(is_readable("$DB/public/wp-config.php")?"CO":"KHONG")."\n"; exit;
EOF
  chmod 644 "/tmp/tpan-e2e-$R.php"; as_a touch "$DA/public/qua-$R.php"
  them_dau <<EOF
<Files "qua-$R.php">
    SetHandler "proxy:unix:$SOCKB|fcgi://localhost"
    ProxyFCGISetEnvIf "true" SCRIPT_FILENAME "$DB/public/index.php"
    ProxyFCGISetEnvIf "true" PHP_ADMIN_VALUE "open_basedir=/ \n auto_prepend_file=/tmp/tpan-e2e-$R.php"
</Files>
EOF
  l4=$(ca "qua-$R.php" | head -c 200); l4c=$(code "qua-$R.php"); tra_ht
  # Lối vòng còn dùng được với danh sách cho phép: RewriteRule [H=...] và [P].
  them_dau <<EOF
RewriteEngine On
RewriteRule ^qua2-$R\$ - [H=proxy:unix:$SOCKB|fcgi://localhost,E=PHP_ADMIN_VALUE:auto_prepend_file=/tmp/tpan-e2e-$R.php,E=PHP_VALUE:auto_prepend_file=/tmp/tpan-e2e-$R.php]
EOF
  l4b=$(ca "qua2-$R" | head -c 200); tra_ht
  if echo "$l4$l4b" | grep -q "MA_CUA_A chay duoi user=$UB"; then
    kq L4 LO "mã do A viết chạy dưới user của B: $(echo "$l4$l4b" | grep -o 'MA_CUA_A[^<]*' | head -1)"
    systemctl restart "php$VB-fpm"; sleep 1   # cấu hình bơm vào nằm lại trong tiến trình PHP của B
  else kq L4 CHAN "SetHandler + ProxyFCGISetEnvIf sang socket của B: HTTP $l4c, mã của A không chạy dưới user B (cả lối RewriteRule [H=])"; fi
  hb=$(cb "" | head -c 400)
  if echo "$hb" | grep -q 'MA_CUA_A'; then kq N9 HONG "trang chủ của B vẫn chạy mã của A sau phép thử L4"; else kq N9 DAT "trang chủ của B không dính mã của A sau phép thử L4"; fi

  as_a ln -sf "$DB/public/rieng-$R.txt" "$DA/public/lay-$R.txt"
  c0=$(code "lay-$R.txt")
  [ "$c0" = 403 ] && kq N5 DAT "symlink của A sang file của B qua Apache: HTTP 403 (SymLinksIfOwnerMatch)" || kq N5 HONG "symlink của A sang file của B qua Apache: HTTP $c0, đáng lẽ 403"
  l9=CHAN; l9d=""
  for o in "Options FollowSymLinks" "Options +FollowSymLinks" "Options All"; do
    printf '%s\n' "$o" | them_dau; c=$(code "lay-$R.txt"); th=$(ca "lay-$R.txt" | head -c 40); tra_ht
    l9d="$l9d [$o: HTTP $c]"; echo "$th" | grep -q NOI_DUNG_RIENG_CUA_B && l9=LO
  done
  kq L9 "$l9" "symlink sang file 640 của B sau khi .htaccess đổi Options:$l9d"
  printf 'Options -Indexes\n' | them_dau; c=$(code ""); tra_ht
  [ "$c" = 200 ] && kq N10 DAT ".htaccess có 'Options -Indexes' (plugin hay ghi): trang chủ A vẫn HTTP 200" || kq N10 HONG ".htaccess có 'Options -Indexes': trang chủ A ra HTTP $c"
  them_dau <<EOF
RewriteEngine On
RewriteRule ^lay2-$R\$ $DB/public/rieng-$R.txt [L]
EOF
  c=$(code "lay2-$R"); th=$(ca "lay2-$R" | head -c 40); tra_ht
  echo "$th" | grep -q NOI_DUNG_RIENG_CUA_B && kq N6 HONG "RewriteRule của A trỏ đường dẫn tuyệt đối của B: lộ (HTTP $c)" || kq N6 DAT "RewriteRule của A trỏ đường dẫn tuyệt đối của B: HTTP $c, không lộ"
  kq L5 BO "symlink qua nginx: stack này là Apache (xem N5 và L9)"
else
  kq L3 BO "stack nginx không có .htaccess"; kq L4 BO "stack nginx không có .htaccess"; kq L9 BO "stack nginx không có .htaccess"
  # ---- L5: symlink qua nginx ----
  as_a ln -sf "$DB/public/rieng-$R.txt" "$DA/public/lay-$R.txt"
  as_a ln -sf "$DB/public/nguon-$R.php" "$DA/public/nguon-$R.txt"
  as_a ln -sfn "$DA/public/wp-includes/css" "$DA/public/tu-$R"
  c1=$(code "lay-$R.txt"); c2=$(code "nguon-$R.txt")
  if ca "lay-$R.txt" | grep -q NOI_DUNG_RIENG_CUA_B || ca "nguon-$R.txt" | grep -q KHOA_TRONG_MA_NGUON_CUA_B; then
    kq L5 LO "symlink của A sang file 640 của B: HTTP $c1; sang mã nguồn PHP của B dưới tên .txt: HTTP $c2"
  else kq L5 CHAN "symlink của A sang file của B: HTTP $c1; sang mã nguồn PHP của B: HTTP $c2"; fi
  c=$(code "tu-$R/dist/block-library/style.min.css")
  [ "$c" = 200 ] && kq N5 DAT "symlink của A trỏ vào file của chính A: HTTP 200" || kq N5 HONG "symlink của A trỏ vào file của chính A: HTTP $c, đáng lẽ 200"
  printf 'open_basedir=/\n' | as_a tee "$DA/public/.user.ini" >/dev/null
  systemctl reload "php$VA-fpm"; sleep 1
  l=$(ca "t-$R.php"); rm -f "$DA/public/.user.ini"
  case "$l" in *"|open_basedir=$DA/|ngoai=KHONG"*) kq N6 DAT ".user.ini của A đặt open_basedir=/: không đổi được";; *) kq N6 HONG ".user.ini của A đặt open_basedir=/: $l";; esac
fi

# ---- L6: log ----------------------------------------------------------------
cb "" -o /dev/null
if as_a head -c 1 "/var/log/tpan/sites/$B/access.log" >/dev/null 2>&1; then kq L6 LO "user A đọc được access.log của B ($(stat -c '%A %U:%G' "/var/log/tpan/sites/$B") )"
else kq L6 CHAN "user A không đọc được access.log của B ($(stat -c '%A %U:%G' "/var/log/tpan/sites/$B"))"; fi
runuser -u "$UB" -- head -c 1 "/var/log/tpan/sites/$B/access.log" >/dev/null 2>&1 && kq N7 DAT "user B đọc được access.log của chính mình" || kq N7 HONG "user B không đọc được access.log của chính mình"
n1=$(wc -l < "/var/log/tpan/sites/$B/access.log"); cb "dem-$R" -o /dev/null; sleep 1; n2=$(wc -l < "/var/log/tpan/sites/$B/access.log")
[ "$n2" -gt "$n1" ] && kq N8 DAT "web server vẫn ghi access.log của B ($n1 -> $n2 dòng)" || kq N8 HONG "web server không ghi access.log của B ($n1 -> $n2 dòng)"

# ---- L7: /proc --------------------------------------------------------------
l7=$(ca "s-$R.php" | tail -1)
case "$l7" in
  khac=0) kq L7 CHAN "PHP của A gọi ps: thấy 0 tiến trình của user khác";;
  khac=[0-9]*) kq L7 LO "PHP của A gọi ps: thấy ${l7#khac=} tiến trình của user khác";;
  *) kq L7 HONG "trả lời lạ: $(echo "$l7" | head -c 80)";;
esac

# ---- L8: vhost lạ trùng tên miền của A ---------------------------------------
mkdir -p "/srv/trang-la-$R"; echo "DAY LA TRANG LA" > "/srv/trang-la-$R/index.html"
if [ "$WEB" = nginx ]; then
  printf 'server {\n    listen 80;\n    server_name %s;\n    root /srv/trang-la-%s;\n}\n' "$A" "$R" > "/etc/nginx/sites-enabled/aaa-trung-$R.conf"
  nginx -t >/dev/null 2>&1; systemctl reload nginx
else
  printf '<VirtualHost *:80>\n    ServerName %s\n    DocumentRoot /srv/trang-la-%s\n    <Directory /srv/trang-la-%s>\n        Require all granted\n    </Directory>\n</VirtualHost>\n' "$A" "$R" "$R" > "/etc/apache2/sites-enabled/aaa-trung-$R.conf"
  apache2ctl configtest >/dev/null 2>&1; systemctl reload apache2
fi
sleep 2
la=$(ca "" | head -c 40 | tr '\n' ' ')
hl=$(tpan health 2>&1 | grep -c "aaa-trung-$R")
if [ "$hl" -gt 0 ]; then kq L8 CHAN "vhost lạ trùng tên $A (tên miền đang trả: '$la'): tpan health chỉ ra file aaa-trung-$R.conf ($hl dòng)"
else kq L8 LO "vhost lạ trùng tên $A (tên miền đang trả: '$la'): tpan health không chỉ ra file trùng"; fi
rm -f "/etc/nginx/sites-enabled/aaa-trung-$R.conf" "/etc/apache2/sites-enabled/aaa-trung-$R.conf"
if [ "$WEB" = nginx ]; then systemctl reload nginx; else systemctl reload apache2; fi
sleep 2

# ---- Các phép "không lộ" và "site vẫn chạy" ------------------------------------
as_a test -r "$DB/public/wp-config.php" 2>/dev/null && kq N1 HONG "user A đọc được wp-config.php của B" || kq N1 DAT "user A không đọc được wp-config.php của B"
as_a ls "$DB" >/dev/null 2>&1 && kq N2 HONG "user A liệt kê được thư mục của B" || kq N2 DAT "user A không liệt kê được thư mục của B"
as_a touch "$DB/public/x-$R" 2>/dev/null && kq N3 HONG "user A ghi được vào public của B" || kq N3 DAT "user A không ghi được vào public của B"
as_a test -w "$SOCKB" 2>/dev/null && kq N4 HONG "user A ghi được vào socket FPM của B" || kq N4 DAT "user A không ghi được vào socket FPM của B"
g(){ curl -s --max-time 10 -o "$T/than" -w '%{http_code}' -H "Host: $1" http://127.0.0.1/ 2>/dev/null; }
ai(){ if grep -q "//$A" "$T/than" 2>/dev/null; then echo A; elif grep -q "//$B" "$T/than" 2>/dev/null; then echo B; else echo khong; fi; }
c=$(g "$(echo "$A" | tr a-z A-Z)"); [ "$c" = 200 ] && [ "$(ai)" = A ] && kq N11 DAT "Host viết hoa về đúng site A" || kq N11 HONG "Host viết hoa: HTTP $c, site $(ai)"
c=$(g "$B."); [ "$c" = 200 ] && [ "$(ai)" = B ] && kq N12 DAT "Host có dấu chấm cuối về đúng site B" || kq N12 HONG "Host có dấu chấm cuối: HTTP $c, site $(ai)"
c=$(g "khong-co-$R.example.com")
if [ "$WEB" = nginx ]; then [ "$c" = 000 ] && kq N13 DAT "tên lạ: nginx đóng kết nối" || kq N13 HONG "tên lạ: HTTP $c, đáng lẽ đóng kết nối"
else [ "$c" = 403 ] && kq N13 DAT "tên lạ: Apache trả 403" || kq N13 HONG "tên lạ: HTTP $c, đáng lẽ 403"; fi
ha=$(code ""); hb=$(cb "" -o /dev/null -w '%{http_code}')
[ "$ha" = 200 ] && [ "$hb" = 200 ] && kq N14 DAT "trang chủ WordPress của A và B: HTTP 200 / 200" || kq N14 HONG "trang chủ WordPress của A và B: HTTP $ha / $hb"
pa=$(code "hello-world/"); pk=$(code "khong-co-trang-$R/"); wk=$(ca "khong-co-trang-$R/" | grep -c 'wp-')
[ "$pa" = 200 ] && [ "$pk" = 404 ] && [ "$wk" -gt 0 ] && kq N15 DAT "permalink của A: /hello-world/ HTTP 200, trang không có HTTP 404 do WordPress trả" || kq N15 HONG "permalink của A: /hello-world/ HTTP $pa, trang không có HTTP $pk (dấu WordPress: $wk)"
sa=$(code "wp-includes/css/dist/block-library/style.min.css"); [ "$sa" = 200 ] && kq N16 DAT "file tĩnh của A: HTTP 200" || kq N16 HONG "file tĩnh của A: HTTP $sa"

echo "TONG lo=$LO chan=$CHAN hong=$HONG"
