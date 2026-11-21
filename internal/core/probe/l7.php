<?php
{{template "chung"}}
// L7: PHP của A đếm tiến trình của user khác (ps đọc /proc). So bằng uid vì ps
// cắt tên user dài.
list($c, $o) = tp_run(['ps', '-eo', 'uid=']);
$me = (string) posix_geteuid();
$n = 0;
foreach (explode("\n", $o) as $u) {
    $u = trim($u);
    if ($u !== '' && $u !== $me) {
        $n++;
    }
}
tp_kq($c === 0 && $n > 0, $n);
