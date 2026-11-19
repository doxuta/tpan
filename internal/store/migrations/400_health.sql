-- Bản 400: health của M4 (mục 4.4 và 6.7 của docs/KE-HOACH.md). Khoảng số
-- 400-499 là của M4 (mục 6.3 của agent-dev/CHAY-NHIEU-AGENT.md).

-- Kết quả gần nhất của từng phép kiểm. id là checks.Result.ID, ổn định giữa
-- các lần chạy ("service.active:nginx", "php.socket:example.com"...). target là
-- đối tượng: "" cho máy và stack, domain cho site, "tpan" cho chính tpan.
-- data là JSON của cả checks.Result (bằng chứng, cách sửa) và là nguồn khi đọc
-- lại; các cột khác tách ra để lọc.
CREATE TABLE check_results (
    id         TEXT PRIMARY KEY,
    target     TEXT NOT NULL,
    stage      TEXT NOT NULL,
    status     TEXT NOT NULL,
    data       TEXT NOT NULL,
    checked_at TEXT NOT NULL,
    changed_at TEXT NOT NULL    -- lần gần nhất status đổi giá trị
) STRICT;

CREATE INDEX check_results_target ON check_results (target);

-- Sự cố: mở khi một phép kiểm chuyển sang fail, đóng khi nó pass lại (hoặc
-- khi đối tượng không còn được kiểm, ví dụ site đã xoá). Giữ bằng chứng lúc
-- mở và lúc đóng (JSON checks.Result).
CREATE TABLE incidents (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    check_id   TEXT NOT NULL,
    target     TEXT NOT NULL,
    stage      TEXT NOT NULL,
    title_vi   TEXT NOT NULL,
    title_en   TEXT NOT NULL,
    opened_at  TEXT NOT NULL,
    open_data  TEXT NOT NULL,
    closed_at  TEXT,              -- NULL khi còn mở
    close_data TEXT,              -- NULL khi còn mở, hoặc đóng vì không còn kiểm
    close_reason TEXT NOT NULL DEFAULT ''  -- "pass" | "gone" | "" khi còn mở
) STRICT;

-- Mỗi phép kiểm có nhiều nhất một sự cố đang mở: chặn ngay ở DB.
CREATE UNIQUE INDEX incidents_open ON incidents (check_id) WHERE closed_at IS NULL;
CREATE INDEX incidents_opened_at ON incidents (opened_at);

-- Những việc đã làm giữa lúc mở và lúc đóng: các job sửa (tự động) chạy cho sự cố.
-- job_id không có khoá ngoại: sự cố vẫn giữ dấu vết khi job cũ bị dọn khỏi lịch sử.
CREATE TABLE incident_jobs (
    incident_id INTEGER NOT NULL REFERENCES incidents (id) ON DELETE CASCADE,
    job_id      TEXT    NOT NULL,
    added_at    TEXT    NOT NULL,
    PRIMARY KEY (incident_id, job_id)
) STRICT;
