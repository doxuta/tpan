<?php
{{template "chung"}}
// L3 (chỉ Apache): .htaccess của A vừa ghi SetEnv PHP_ADMIN_VALUE
// "open_basedir=/". Vhost không gỡ biến đó thì open_basedir của A thành "/".
tp_kq(ini_get('open_basedir') !== '{{.ADir}}/', 0);
