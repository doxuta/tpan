<?php
{{template "chung"}}
// C3: A cấp phát 320 MB. memory_limit nâng lên trước, để thứ chặn (nếu có) là
// trần RAM của lồng chứ không phải mặc định 128M của PHP.
@ini_set('memory_limit', '1024M');
$want = 320 * 1024 * 1024;
$s = str_repeat('x', $want);
tp_kq(strlen($s) >= $want, intdiv(strlen($s), 1048576));
