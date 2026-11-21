<?php
{{template "chung"}}
// C5: A đọc /proc/self/net/unix (đường dẫn mọi Unix socket của máy, vì lồng
// dùng chung mạng) và đếm socket mang slug của B.
list($c, $o) = tp_run(['cat', '/proc/self/net/unix']);
$n = substr_count($o, '{{.BSlug}}');
tp_kq($n > 0, $n);
