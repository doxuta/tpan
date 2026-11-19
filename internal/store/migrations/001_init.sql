-- Bản 1: các bảng của M1. Mục 4.4 của docs/KE-HOACH.md.
-- Thời gian lưu dạng chữ UTC, độ rộng cố định (xem timeLayout trong store.go),
-- nên so sánh chuỗi cũng là so sánh thời gian.
-- Không có chỗ nào ở đây lưu mật khẩu hay khoá: bảng chỉ giữ thứ engine đưa vào,
-- và system.Secret đã tự che trước khi tới đây.

-- Cài đặt dạng khoá-giá trị: web server đang dùng, bản PHP mặc định, email Let's Encrypt...
CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL
) STRICT;

-- Mỗi lần chạy một Plan là một job. id do engine sinh (16 ký tự hex).
CREATE TABLE jobs (
    id          TEXT PRIMARY KEY,
    kind        TEXT NOT NULL,
    target      TEXT NOT NULL,
    title_vi    TEXT NOT NULL,
    title_en    TEXT NOT NULL,
    status      TEXT NOT NULL,
    started_at  TEXT NOT NULL,
    finished_at TEXT,            -- NULL khi job còn đang chạy
    error       TEXT NOT NULL DEFAULT ''
) STRICT;

-- Danh sách lịch sử hiện mới nhất trước, nên cần chỉ mục theo started_at.
CREATE INDEX jobs_started_at ON jobs (started_at);

-- Log từng bước của job. Các cột tách riêng để lọc và xem nhanh; cột data là
-- JSON của cả engine.Event và là nguồn khi đọc lại, nên thêm trường vào Event
-- không cần migration.
CREATE TABLE job_events (
    job_id  TEXT    NOT NULL REFERENCES jobs (id) ON DELETE CASCADE,
    seq     INTEGER NOT NULL,
    time    TEXT    NOT NULL,
    kind    TEXT    NOT NULL,
    step_id TEXT    NOT NULL DEFAULT '',
    message TEXT    NOT NULL DEFAULT '',
    data    TEXT    NOT NULL,
    PRIMARY KEY (job_id, seq)
) STRICT;

-- File tpan đã ghi cùng sha256 lúc ghi, để health phát hiện sửa tay (M4).
-- job_id không có khoá ngoại: file vẫn được quản lý khi job cũ bị dọn khỏi lịch sử.
CREATE TABLE managed_files (
    path       TEXT PRIMARY KEY,
    sha256     TEXT NOT NULL,
    job_id     TEXT NOT NULL,
    updated_at TEXT NOT NULL
) STRICT;
