<?php
{{template "chung"}}
// L4 (chỉ Apache), phía B: chạy được qua tên miền của A nghĩa là .htaccess của
// A đã chuyển request sang socket PHP của B (SetHandler, ProxyFCGISetEnvIf).
tp_kq(true, 0);
