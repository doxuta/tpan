<?php
{{template "chung"}}
// C1: A gọi lệnh liệt kê /var/www. Có thư mục của B không, và /var/www có mấy
// mục (chỉ in số đếm, không in tên).
list($c, $o) = tp_run(['ls', '-1A', '/var/www']);
$names = array_values(array_filter(array_map('trim', explode("\n", $o)), 'strlen'));
tp_kq($c === 0 && in_array('{{.BDomain}}', $names, true), count($names));
