<?php
{{template "chung"}}
// C4: A đọc danh sách user của máy (getent passwd) và đếm user tp_* khác mình
// (chỉ in số đếm, không in tên).
list($c, $o) = tp_run(['getent', 'passwd']);
$me = tp_user();
$n = 0;
foreach (explode("\n", $o) as $line) {
    $u = (string) strstr($line, ':', true);
    if (strncmp($u, 'tp_', 3) === 0 && $u !== $me) {
        $n++;
    }
}
tp_kq($n > 0, $n);
