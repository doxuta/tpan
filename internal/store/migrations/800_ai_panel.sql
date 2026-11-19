-- Bản 800: AI trong panel của M8. Mục 4.4, 8.2 và 8.4 của docs/KE-HOACH.md.
--
-- Một phiên panel (ai_sessions.entry = 'panel') có nhiều lượt hỏi; mỗi lượt là
-- một dòng ai_turns. Lượt giữ ĐÚNG thứ đã gửi cho bộ não (bản đã che) để trang
-- Phiên AI cho người xem lại AI đã thấy gì (nguyên tắc 3 của mục 8.1).

-- UUID của phiên Claude Code ứng với phiên tpan: lượt đầu chạy
-- `claude --session-id <uuid>`, lượt sau `--resume <uuid>`. Claude Code đòi
-- UUID, còn id phiên tpan là 16 hex, nên phải giữ riêng.
ALTER TABLE ai_sessions ADD COLUMN brain_session TEXT NOT NULL DEFAULT '';

CREATE TABLE ai_turns (
    session_id TEXT    NOT NULL REFERENCES ai_sessions (id) ON DELETE CASCADE,
    seq        INTEGER NOT NULL,             -- 1, 2, ... trong phiên
    lang       TEXT    NOT NULL CHECK (lang IN ('vi', 'en')),
    question   TEXT    NOT NULL,             -- câu hỏi đã che, đúng thứ gửi đi
    context    TEXT    NOT NULL,             -- JSON []ai.Chip đã che, đúng thứ gửi đi
    playbook   TEXT    NOT NULL DEFAULT '',  -- tên bản hướng dẫn gửi kèm lượt này; rỗng nếu không gửi
    prompt     TEXT    NOT NULL,             -- toàn văn đưa vào stdin của claude -p (đã che)
    answer     TEXT    NOT NULL DEFAULT '',  -- câu trả lời cuối, đúng như AI viết (dạng bí danh)
    events     TEXT    NOT NULL DEFAULT '[]',-- JSON []ai.Event đã nhận (dạng bí danh)
    status     TEXT    NOT NULL CHECK (status IN ('running', 'done', 'error', 'refused')),
    error      TEXT    NOT NULL DEFAULT '',
    started_at TEXT    NOT NULL,
    ended_at   TEXT,                         -- NULL khi còn chạy
    PRIMARY KEY (session_id, seq)
) STRICT;

-- Đếm số câu hỏi trong ngày (trần mỗi ngày, mục 8.1 nguyên tắc 6).
CREATE INDEX ai_turns_started ON ai_turns (started_at);

-- Các lần người dùng chuyển mục trên panel (mục 8.4, "Ngữ cảnh làm việc").
-- Công cụ current_view (chạy trong tiến trình `tpan mcp`, khác tiến trình
-- serve) đọc ở đây. Một tài khoản quản trị nên chỉ có một dòng thời gian
-- chung; store chỉ giữ 50 dòng mới nhất.
CREATE TABLE ai_views (
    id    INTEGER PRIMARY KEY AUTOINCREMENT,
    time  TEXT NOT NULL,
    path  TEXT NOT NULL,    -- đường dẫn của màn, ví dụ /sites/shop.example.com
    label TEXT NOT NULL     -- tên mục người đọc được, theo ngôn ngữ giao diện lúc đó
) STRICT;
