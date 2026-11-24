<?php
{{template "chung"}}
// L1: PHP của A include wp-config.php của B. OPcache dùng chung mà không kiểm
// quyền (validate_permission=0) thì bản trong cache được dùng dù open_basedir
// và quyền file đều cấm, và hằng DB_PASSWORD của B lộ ra.
register_shutdown_function(function () {
    tp_kq(defined('DB_PASSWORD'), 0);
});
@include '{{.BWPConfig}}';
