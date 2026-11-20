#!/bin/bash
# Stack Apache: .htaccess cua A dung "Options +FollowSymLinks" de mo lai symlink sang file cua B?
# Thu tren vhost goc (AllowOverride All), roi ban an-toan co Options trong AllowOverrideList,
# roi ban gioi han tuy chon bang AllowOverride Options=...
# Chay: scripts/vm.sh root bash -s < t9-apache-options-symlink.sh   (chi tren may ao tpan-m14)
set -u
A=a.example.com; B=b.example.com; DA=/var/www/$A; DB=/var/www/$B
UA=$(stat -c %U $DA); UB=$(stat -c %U $DB)
VH=/etc/apache2/sites-available/tpan-$A.conf
HT=$DA/public/.htaccess
ca(){ local p=$1; shift; curl -s --max-time 10 --resolve $A:80:127.0.0.1 "$@" "http://$A/$p"; }
code(){ ca "$1" -o /dev/null -w '%{http_code}'; }
as_a(){ runuser -u "$UA" -- "$@"; }
cp -p $HT /root/ht.goc; cp -p $VH /root/vh.goc
tra_ht(){ cp -p /root/ht.goc $HT; }
them_dau(){ { cat; cat /root/ht.goc; } > /root/ht.moi; cp /root/ht.moi $HT; chown $UA:www-data $HT; chmod 640 $HT; }
nap(){ apache2ctl configtest 2>&1 | tail -1; systemctl reload apache2; sleep 1; }
printf 'NOI_DUNG_RIENG_CUA_B\n' > $DB/public/rieng.txt; chown $UB:www-data $DB/public/rieng.txt; chmod 640 $DB/public/rieng.txt
as_a ln -sf $DB/public/rieng.txt $DA/public/lay.txt
thu(){ echo "  khong co gi them:            GET /lay.txt -> HTTP $(code lay.txt)"
       printf 'Options +FollowSymLinks\n' | them_dau
       echo "  Options +FollowSymLinks:     GET /lay.txt -> HTTP $(code lay.txt) | $(ca lay.txt | head -c 30 | tr '\n' ' ') | trang chu: HTTP $(code '')"; tra_ht
       printf 'Options FollowSymLinks\n' | them_dau
       echo "  Options FollowSymLinks:      GET /lay.txt -> HTTP $(code lay.txt) | trang chu: HTTP $(code '')"; tra_ht
       printf 'Options -Indexes\n' | them_dau
       echo "  Options -Indexes:            trang chu: HTTP $(code '') | permalink la: HTTP $(code 'khong-co/')"; tra_ht
       printf 'Options +SymLinksIfOwnerMatch -MultiViews\n' | them_dau
       echo "  Options +SymLinksIfOwnerMatch -MultiViews: trang chu: HTTP $(code '') | /lay.txt: HTTP $(code lay.txt)"; tra_ht
       printf 'Options All\n' | them_dau
       echo "  Options All:                 GET /lay.txt -> HTTP $(code lay.txt) | trang chu: HTTP $(code '')"; tra_ht
       tail -n 1 /var/log/tpan/sites/$A/error.log | cut -c1-200; }
echo "## 0. Vhost goc (AllowOverride All)"; apache2 -v | head -1; thu

DS="<Files <FilesMatch <IfModule <If <Else <ElseIf <Limit <LimitExcept <RequireAll <RequireAny <RequireNone RewriteEngine RewriteRule RewriteCond RewriteBase RewriteOptions Require Order Allow Deny Satisfy AuthType AuthName AuthUserFile AuthGroupFile AuthBasicProvider Header RequestHeader ExpiresActive ExpiresByType ExpiresDefault FileETag AddType AddEncoding AddCharset AddDefaultCharset AddOutputFilterByType AddOutputFilter SetOutputFilter SetEnv SetEnvIf SetEnvIfNoCase UnsetEnv BrowserMatch BrowserMatchNoCase Options DirectoryIndex ErrorDocument Redirect RedirectMatch RedirectPermanent RedirectTemp LimitRequestBody ServerSignature AddLanguage DefaultLanguage"
echo; echo "## 1. AllowOverride None + AllowOverrideList co Options (dac ta 2.3)"
sed "s|AllowOverride All|AllowOverride None\n        AllowOverrideList $DS|" /root/vh.goc > $VH; nap; thu

DS2=$(echo "$DS" | sed 's/ Options / /')
echo; echo "## 2. AllowOverride Options=Indexes,MultiViews,SymLinksIfOwnerMatch + AllowOverrideList KHONG co Options"
sed "s|AllowOverride All|AllowOverride Options=Indexes,MultiViews,SymLinksIfOwnerMatch\n        AllowOverrideList $DS2|" /root/vh.goc > $VH; nap; thu
echo "  chi thi khac con chay (Header set): $(printf 'Header set X-Thu "1"\n' | them_dau; ca '' -o /dev/null -D - | grep -ci '^x-thu'; tra_ht)"
echo "  SetHandler van bi chan: HTTP $(printf 'SetHandler none\n' | them_dau; code ''; tra_ht)"

echo; echo "## 3. AllowOverride None + AllowOverrideList KHONG co Options"
sed "s|AllowOverride All|AllowOverride None\n        AllowOverrideList $DS2|" /root/vh.goc > $VH; nap; thu

echo; echo "## Tra ve nhu cu"
cp -p /root/vh.goc $VH; tra_ht; nap
rm -f $DA/public/lay.txt $DB/public/rieng.txt /root/ht.goc /root/vh.goc /root/ht.moi; echo "da tra ve"
