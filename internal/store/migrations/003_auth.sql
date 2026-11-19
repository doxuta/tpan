-- Bản 3: đăng nhập và nhật ký của M3. Mục 4.4 và 11 của docs/KE-HOACH.md.
-- Hash mật khẩu quản trị (argon2id, dạng PHC "$argon2id$v=19$m=...,t=...,p=...$salt$hash")
-- nằm ở bảng settings, khoá "admin.password_hash"; không bảng nào giữ mật khẩu thật.

-- Phiên đăng nhập. Chỉ lưu sha256 (hex) của token; token thật chỉ có ở cookie
-- của trình duyệt. id là mã ngắn ngẫu nhiên để liệt kê và xoá phiên trên
-- giao diện mà không lộ gì về token.
CREATE TABLE sessions (
    token_sha256 TEXT PRIMARY KEY,
    id           TEXT NOT NULL UNIQUE,
    ip           TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    last_seen_at TEXT NOT NULL
) STRICT;

-- Đếm lần đăng nhập sai theo IP: sai 5 lần thì khoá 15 phút. Nằm trong DB chứ
-- không trong bộ nhớ vì `tpan admin passwd` (một tiến trình khác) phải gỡ được khoá.
CREATE TABLE login_failures (
    ip           TEXT PRIMARY KEY,
    failures     INTEGER NOT NULL,
    first_at     TEXT NOT NULL,
    locked_until TEXT              -- NULL khi chưa bị khoá
) STRICT;

-- Nhật ký: mọi thao tác có thay đổi (thời điểm, IP, thao tác, đối tượng, kết quả).
CREATE TABLE audit (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    time    TEXT NOT NULL,
    ip      TEXT NOT NULL,          -- "cli" khi gõ ở dòng lệnh
    action  TEXT NOT NULL,          -- ví dụ "login", "login.fail", "site.add", "admin.passwd"
    target  TEXT NOT NULL DEFAULT '',
    result  TEXT NOT NULL,          -- "ok" hoặc mã lỗi
    job_id  TEXT NOT NULL DEFAULT ''
) STRICT;

CREATE INDEX audit_time ON audit (time);
