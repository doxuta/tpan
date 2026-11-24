<?php
{{template "chung"}}
// L4 (chỉ Apache), phía A: file đích của khối <Files> trong .htaccess của A.
// Request tới file này mà .htaccess bị bỏ qua thì chính file này chạy và in
// user của A (KHONG). Request bị chuyển sang socket PHP của B thì l4b.php của
// B chạy thay và in user của B.
tp_kq(false, 0);
