-- Bản 900: backup và cron của M9 (mục 4.4, 6.5, 6.9, 6.10 của docs/KE-HOACH.md).
-- Khoảng số 900-999 là của M9.

-- Mỗi file backup tpan tạo ra: bản đầy đủ (.tar.gz: thư mục public, bản xuất
-- DB, manifest.json) hoặc bản xuất DB riêng (.sql, mục 6.9).
-- domain KHÔNG có khoá ngoại tới sites: backup phải sống sót sau khi site bị
-- xoá hẳn (backup "trước khi xoá hẳn" là đường lấy lại duy nhất lúc đó).
-- id là 16 ký tự hex ngẫu nhiên; path là đường dẫn tuyệt đối dưới
-- /var/lib/tpan/backups/<domain>/.
CREATE TABLE backups (
    id          TEXT PRIMARY KEY,
    domain      TEXT NOT NULL,
    kind        TEXT NOT NULL,             -- "full" | "sql"
    reason      TEXT NOT NULL,             -- "manual" | "schedule" | "before-restore" | "before-purge" | "before-import"
    path        TEXT NOT NULL UNIQUE,
    size        INTEGER NOT NULL,          -- byte
    sha256      TEXT NOT NULL,             -- hex của cả file
    wp_version  TEXT NOT NULL DEFAULT '',
    php_version TEXT NOT NULL DEFAULT '',
    job_id      TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL              -- RFC 3339, UTC
) STRICT;

CREATE INDEX backups_domain_created ON backups (domain, created_at);

-- Lần chạy gần nhất của cron WordPress cho từng site (`tpan cron run`, mỗi 5
-- phút). Chỉ giữ một dòng mỗi site: màn Cron chỉ cần "lần gần nhất và kết
-- quả"; lịch sử dài là việc của log, không phải của DB.
CREATE TABLE cron_runs (
    domain      TEXT PRIMARY KEY,
    started_at  TEXT NOT NULL,
    duration_ms INTEGER NOT NULL,
    status      TEXT NOT NULL,             -- "ok" | "fail" | "skipped"
    exit_code   INTEGER NOT NULL,
    output      TEXT NOT NULL              -- tối đa 2 KB cuối của stdout+stderr
) STRICT;
