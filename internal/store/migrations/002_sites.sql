-- Bản 2: bảng sites của M2. Mục 4.4 và 5 của docs/KE-HOACH.md.
-- Không có cột mật khẩu nào: mật khẩu DB nằm trong wp-config.php của site,
-- mật khẩu quản trị WordPress chỉ hiện một lần lúc tạo.

CREATE TABLE sites (
    -- domain đã qua validate.ParseDomain: chữ thường, punycode.
    domain      TEXT PRIMARY KEY,
    -- slug suy từ domain (validate.SlugOf). UNIQUE để hai domain trùng slug
    -- (khó xảy ra nhờ 6 ký tự hash) bị chặn ngay ở DB, không dùng chung user Linux.
    slug        TEXT NOT NULL UNIQUE,
    status      TEXT NOT NULL CHECK (status IN ('active', 'trashed')),
    php_version TEXT NOT NULL,
    ssl         INTEGER NOT NULL DEFAULT 0 CHECK (ssl IN (0, 1)),
    created_at  TEXT NOT NULL,
    trashed_at  TEXT              -- NULL khi site không ở thùng rác
) STRICT;
