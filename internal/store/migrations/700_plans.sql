-- Bản 700: Plan chờ duyệt của M7. Mục 4.2, 4.4 và 8.1 của docs/KE-HOACH.md.
-- Một dòng là một engine.PendingPlan. Không có mật khẩu nào ở đây: request chỉ
-- giữ domain, email, tiêu đề; mật khẩu được sinh mới lúc dựng lại Plan.

CREATE TABLE plans (
    id          TEXT PRIMARY KEY,
    kind        TEXT NOT NULL,
    target      TEXT NOT NULL,
    title_vi    TEXT NOT NULL,
    title_en    TEXT NOT NULL,
    request     TEXT NOT NULL,            -- JSON của core.PlanRequest, giá trị thật
    preview     TEXT NOT NULL,            -- JSON của engine.Preview mới nhất
    fingerprint TEXT NOT NULL,            -- engine.Fingerprint(preview)
    origin      TEXT NOT NULL CHECK (origin IN ('human', 'ai', 'mcp')),
    session_id  TEXT NOT NULL DEFAULT '', -- ai_sessions.id; rỗng khi người đề xuất
    untrusted   INTEGER NOT NULL DEFAULT 0 CHECK (untrusted IN (0, 1)),
    status      TEXT NOT NULL CHECK (status IN ('pending', 'applied', 'rejected')),
    created_at  TEXT NOT NULL,
    decided_at  TEXT,                     -- NULL khi còn chờ
    decided_by  TEXT NOT NULL DEFAULT '',
    job_id      TEXT NOT NULL DEFAULT ''
) STRICT;

-- Màn duyệt và Bảng điều khiển đọc "các Plan đang chờ, mới nhất trước".
CREATE INDEX plans_status_created ON plans (status, created_at);
