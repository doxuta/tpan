<?php
{{template "chung"}}
// L2: opcache_get_status() gọi từ A. Không bị restrict_api chặn thì A liệt kê
// được đường dẫn file của mọi site (và opcache_reset() xoá được cache của họ).
$s = function_exists('opcache_get_status') ? @opcache_get_status(true) : false;
$n = 0;
if (is_array($s) && isset($s['scripts']) && is_array($s['scripts'])) {
    foreach ($s['scripts'] as $k => $v) {
        if (strpos((string) $k, '{{.BDir}}/') === 0) {
            $n++;
        }
    }
}
tp_kq(is_array($s), $n);
