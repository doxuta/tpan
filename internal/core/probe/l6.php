<?php
{{template "chung"}}
// L6: user của A đọc access.log của B (IP khách, URL kèm tham số).
list($c, $o) = tp_run(['head', '-c', '1', '{{.BAccessLog}}']);
tp_kq($c === 0 && $o !== '', 0);
