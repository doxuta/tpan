-- Bản 2900 (M29): loại ứng dụng của site. Site có từ trước đều là WordPress.
-- 'php' là site PHP trống: không có bước cài, phép kiểm hay cron của WordPress.
ALTER TABLE sites ADD COLUMN app TEXT NOT NULL DEFAULT 'wordpress' CHECK (app IN ('wordpress', 'php'));
