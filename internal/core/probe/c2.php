<?php
{{template "chung"}}
// C2, phía A: ghi một file rỗng vào /tmp. c2b.php của B xem có thấy không.
list($c, $o) = tp_run(['touch', '{{.TmpFile}}']);
tp_kq($c === 0, 0);
