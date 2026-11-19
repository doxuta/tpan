-- Bản 701: phiên AI và các lần gọi công cụ của M7. Mục 4.4, 8.1 và 8.3 của
-- docs/KE-HOACH.md. Mỗi lần `tpan mcp` chạy là một phiên (hoặc nối vào phiên
-- có sẵn bằng --session); mọi lần gọi công cụ được ghi ở phía máy chủ, không
-- dựa vào lời kể của agent.

CREATE TABLE ai_sessions (
    id         TEXT PRIMARY KEY,
    -- mcp: agent nối trực tiếp (Claude Code, Codex gõ ở máy cá nhân);
    -- panel: câu hỏi gửi từ giao diện qua tpan brain (M8).
    entry      TEXT NOT NULL CHECK (entry IN ('mcp', 'panel')),
    client     TEXT NOT NULL DEFAULT '',   -- tên và phiên bản client MCP tự khai
    question   TEXT NOT NULL DEFAULT '',   -- M8
    context    TEXT NOT NULL DEFAULT '',   -- JSON ngữ cảnh làm việc, M8
    -- Bảng bí danh → giá trị thật (JSON). Giữ ở máy chủ để bí danh ổn định
    -- suốt phiên (mỗi lượt claude -p mở một tiến trình tpan mcp mới) và để
    -- panel đổi ngược khi hiển thị. Không bao giờ gửi cho AI.
    aliases    TEXT NOT NULL DEFAULT '{}',
    -- Phiên đã đọc nội dung do người lạ gửi tới (log, output lệnh).
    untrusted  INTEGER NOT NULL DEFAULT 0 CHECK (untrusted IN (0, 1)),
    created_at TEXT NOT NULL,
    last_at    TEXT NOT NULL
) STRICT;

CREATE TABLE ai_calls (
    session_id TEXT    NOT NULL REFERENCES ai_sessions (id) ON DELETE CASCADE,
    seq        INTEGER NOT NULL,
    time       TEXT    NOT NULL,
    tool       TEXT    NOT NULL,
    input      TEXT    NOT NULL,            -- JSON đối số đúng như agent gửi (dạng bí danh)
    output     TEXT    NOT NULL,            -- JSON kết quả đã che, đúng thứ agent nhận
    error      TEXT    NOT NULL DEFAULT '', -- lỗi đã che
    plan_id    TEXT    NOT NULL DEFAULT '', -- Plan được đề xuất ở lần gọi này
    PRIMARY KEY (session_id, seq)
) STRICT;
