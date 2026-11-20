-- Bản 1400: chế độ .htaccess của từng site (M14, mục 6.13 của
-- docs/KE-HOACH.md). Khoảng số 1400-1499 là của M14.
--
-- DEFAULT 'day-du' nói đúng thực tế của site có từ trước M14: vhost Apache
-- của chúng đang là AllowOverride All. `tpan security harden` chuyển từng site
-- sang 'an-toan' sau khi quét .htaccess của nó. Site tạo từ M14 trở đi được
-- core ghi rõ 'an-toan' (store không bao giờ để cột này nhận DEFAULT).
ALTER TABLE sites ADD COLUMN htaccess TEXT NOT NULL DEFAULT 'day-du' CHECK (htaccess IN ('an-toan', 'tat', 'day-du'));
