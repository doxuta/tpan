<?php
{{template "chung"}}
// C2, dọn: A xoá file nó đã ghi (trong lồng, /tmp riêng của A chỉ A thấy).
list($c, $o) = tp_run(['rm', '-f', '{{.TmpFile}}']);
tp_kq($c === 0, 0);
