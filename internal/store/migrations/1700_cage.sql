-- Bản 1700: lồng theo site, trần theo gói, số liệu tài nguyên, sổ phép thử tấn
-- công của M17 (mục 3.1, 3.8, 3.9 của docs/thay-doi/M17-thiet-ke.md; cuối mốc
-- gộp vào mục 6.15 của docs/KE-HOACH.md). Khoảng số 1700-1799 là của M17.
--
-- Mọi cột thêm vào bảng cũ đều có DEFAULT: máy nâng từ v0.2.0 không đổi dòng
-- nào, gói cũ không bật lồng, site cũ ở ngoài lồng (bất biến 1).

-- Gói: cờ lồng ("muốn") và các trần. 0 là không giới hạn; riêng php_workers: 0
-- là mặc định 5 (pm.max_children của pool ngoài lồng). Khoảng giá trị kiểm ở core.
ALTER TABLE packages ADD COLUMN cage        INTEGER NOT NULL DEFAULT 0 CHECK (cage IN (0, 1));
ALTER TABLE packages ADD COLUMN cpu_percent INTEGER NOT NULL DEFAULT 0 CHECK (cpu_percent >= 0);
ALTER TABLE packages ADD COLUMN ram_mb      INTEGER NOT NULL DEFAULT 0 CHECK (ram_mb >= 0);
ALTER TABLE packages ADD COLUMN tasks       INTEGER NOT NULL DEFAULT 0 CHECK (tasks >= 0);
ALTER TABLE packages ADD COLUMN io_read_mb  INTEGER NOT NULL DEFAULT 0 CHECK (io_read_mb >= 0);
ALTER TABLE packages ADD COLUMN io_write_mb INTEGER NOT NULL DEFAULT 0 CHECK (io_write_mb >= 0);
ALTER TABLE packages ADD COLUMN php_workers INTEGER NOT NULL DEFAULT 0 CHECK (php_workers >= 0);

-- Site: "đang ở trong lồng". Chỉ Plan cage.enable, cage.disable đổi cột này,
-- khi job thành công.
ALTER TABLE sites ADD COLUMN caged INTEGER NOT NULL DEFAULT 0 CHECK (caged IN (0, 1));

-- Mẫu 5 phút của mỗi site trong lồng, giữ 8 ngày. Bộ đếm dồn ghi nguyên giá
-- trị lúc đọc (cage.Sample). Không khoá ngoại tới sites: site xoá hẳn thì mẫu
-- cũ hết hạn theo lịch xoá.
CREATE TABLE usage_samples (
    domain         TEXT    NOT NULL,
    at             TEXT    NOT NULL,
    running        INTEGER NOT NULL CHECK (running IN (0, 1)),
    mem_current    INTEGER NOT NULL,
    mem_peak       INTEGER NOT NULL,
    oom_kills      INTEGER NOT NULL,
    cpu_usec       INTEGER NOT NULL,
    throttled_usec INTEGER NOT NULL,
    nr_throttled   INTEGER NOT NULL,
    tasks          INTEGER NOT NULL,
    tasks_peak     INTEGER NOT NULL,
    tasks_hits     INTEGER NOT NULL,
    io_rbytes      INTEGER NOT NULL,
    io_wbytes      INTEGER NOT NULL,
    -- Trần đang áp lúc lấy mẫu (0 là không giới hạn): luật "đã chạm trần" của
    -- gợi ý dùng trần của lúc xảy ra, không phải trần của gói hiện tại.
    mem_max        INTEGER NOT NULL DEFAULT 0,
    cpu_quota_pct  INTEGER NOT NULL DEFAULT 0,
    tasks_max      INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (domain, at)
) STRICT;
CREATE INDEX usage_samples_at ON usage_samples (at);

-- Dòng gộp theo giờ, giữ 30 ngày (cage.Hour).
CREATE TABLE usage_hourly (
    domain         TEXT    NOT NULL,
    hour           TEXT    NOT NULL,
    samples        INTEGER NOT NULL,
    mem_avg        INTEGER NOT NULL,
    mem_max        INTEGER NOT NULL,
    cpu_usec       INTEGER NOT NULL,
    cpu_pct_max    REAL    NOT NULL,
    throttled_usec INTEGER NOT NULL,
    nr_throttled   INTEGER NOT NULL,
    tasks_max      INTEGER NOT NULL,
    oom_kills      INTEGER NOT NULL,
    tasks_hits     INTEGER NOT NULL,
    io_rbytes      INTEGER NOT NULL,
    io_wbytes      INTEGER NOT NULL,
    PRIMARY KEY (domain, hour)
) STRICT;
CREATE INDEX usage_hourly_hour ON usage_hourly (hour);

-- Sổ phép thử tấn công (cage.ProbeRun). Dòng ghi trước khi tạo thứ gì trên máy;
-- cleaned = 1 chỉ khi đã dọn hết thứ mang hai tên miền này.
CREATE TABLE probe_runs (
    id          TEXT    PRIMARY KEY,
    started_at  TEXT    NOT NULL,
    finished_at TEXT,
    state       TEXT    NOT NULL CHECK (state IN ('running', 'done', 'failed')),
    domain_a    TEXT    NOT NULL,
    domain_b    TEXT    NOT NULL,
    mode        TEXT    NOT NULL DEFAULT '' CHECK (mode IN ('', 'ngoai', 'trong')),
    cleaned     INTEGER NOT NULL DEFAULT 0 CHECK (cleaned IN (0, 1)),
    report      TEXT    NOT NULL DEFAULT ''
) STRICT;
