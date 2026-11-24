-- Bản 2801: bổ sung cho bản 2800 (soát cuối M28 đợt 2 nhóm B, B6). Hai loại
-- job chỉ admin chạy mà Target vẫn qua được validate.ParseDomain, nên bản cũ
-- (suy chủ từ Target cho mọi loại) có thể đã ghi chủ là một khách đặt site
-- trùng đúng tên đó: fix.service.start (Target là tên dịch vụ, ví dụ
-- php8.3-fpm) và backup.key.new (Target là "backup.key"). Dạng hẹp, chỉ hai
-- loại này: vài loại khác ngoài danh sách tra chủ đã được đặt chủ rõ có chủ ý.
UPDATE jobs SET owner = 'admin' WHERE kind IN ('fix.service.start', 'backup.key.new');
