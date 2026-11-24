<?php
{{template "chung"}}
// C2, phía B: thấy file A vừa ghi vào /tmp nghĩa là hai site dùng chung /tmp.
list($c, $o) = tp_run(['test', '-e', '{{.TmpFile}}']);
tp_kq($c === 0, 0);
