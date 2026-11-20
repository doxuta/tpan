-- Bản 1300: nhiều người dùng, gói, tạm khoá, DB rời của M13 (mục 6.12 của
-- docs/KE-HOACH.md). Khoảng số 1300-1399 là của M13.
--
-- Mọi cột thêm vào bảng cũ đều có DEFAULT: dòng có từ trước (site, job, phiên
-- của bản v0.1.x) tự thuộc về admin mà không cần UPDATE.
--
-- Không dựng lại bảng sites để đổi CHECK của status: tạm khoá nằm ở cột riêng
-- (suspended), không phải một giá trị mới của status. Site tạm khoá vẫn là
-- 'active' theo nghĩa "không ở thùng rác".

-- Gói hạn mức. 0 là không giới hạn.
CREATE TABLE packages (
    name        TEXT PRIMARY KEY,
    max_sites   INTEGER NOT NULL DEFAULT 0 CHECK (max_sites >= 0),
    max_dbs     INTEGER NOT NULL DEFAULT 0 CHECK (max_dbs >= 0),
    disk_mb     INTEGER NOT NULL DEFAULT 0 CHECK (disk_mb >= 0),
    max_backups INTEGER NOT NULL DEFAULT 0 CHECK (max_backups >= 0),
    created_at  TEXT NOT NULL
) STRICT;

INSERT INTO packages (name, created_at) VALUES ('default', strftime('%Y-%m-%dT%H:%M:%S.000000000Z', 'now'));

-- User của TPAN. Không phải user Linux: họ chỉ dùng panel (mục 6.12).
-- password_hash là argon2id dạng PHC như trước (store/auth.go).
-- disk_bytes: lần đo gần nhất (thư mục site + DB), -1 là chưa đo.
CREATE TABLE users (
    name             TEXT PRIMARY KEY,
    full_name        TEXT NOT NULL DEFAULT '',
    email            TEXT NOT NULL DEFAULT '',
    role             TEXT NOT NULL CHECK (role IN ('admin', 'user')),
    package          TEXT NOT NULL DEFAULT 'default' REFERENCES packages (name),
    status           TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended')),
    password_hash    TEXT NOT NULL,
    created_at       TEXT NOT NULL,
    suspended_at     TEXT,
    suspend_reason   TEXT NOT NULL DEFAULT '',
    disk_bytes       INTEGER NOT NULL DEFAULT -1,
    disk_measured_at TEXT
) STRICT;

-- Tài khoản quản trị duy nhất của bản cũ thành user 'admin' vai admin, giữ
-- nguyên hash (đăng nhập bằng mật khẩu cũ được). Máy chưa chạy tpan setup thì
-- chưa có hash và chưa có dòng nào; setup tạo sau.
INSERT INTO users (name, role, password_hash, created_at)
    SELECT 'admin', 'admin', value, updated_at FROM settings WHERE key = 'admin.password_hash';
DELETE FROM settings WHERE key = 'admin.password_hash';

-- Phiên đang mở của bản cũ đều là của admin: giữ nguyên, không ai bị đăng xuất.
-- view_as: tên user mà admin đang "xem như" (phiên chỉ đọc); rỗng là bình thường.
ALTER TABLE sessions ADD COLUMN user_name TEXT NOT NULL DEFAULT 'admin';
ALTER TABLE sessions ADD COLUMN view_as TEXT NOT NULL DEFAULT '';

-- Chủ và tạm khoá của site. suspended, db_suspended: '' | 'self' | 'user'
-- (account.Suspension: vì sao bị khoá, để mở khoá user chỉ mở đúng thứ nó khoá).
ALTER TABLE sites ADD COLUMN owner TEXT NOT NULL DEFAULT 'admin';
ALTER TABLE sites ADD COLUMN suspended TEXT NOT NULL DEFAULT '' CHECK (suspended IN ('', 'self', 'user'));
ALTER TABLE sites ADD COLUMN suspended_at TEXT;
ALTER TABLE sites ADD COLUMN db_suspended TEXT NOT NULL DEFAULT '' CHECK (db_suspended IN ('', 'self', 'user'));
CREATE INDEX sites_owner ON sites (owner);

-- DB rời. name cũng là tên user DB ('<owner>_<tên>'). owner không có khoá
-- ngoại: xoá user bị từ chối khi còn DB (core), và dòng của DB đã xoá hẳn thì
-- không còn ở đây.
CREATE TABLE databases (
    name         TEXT PRIMARY KEY,
    owner        TEXT NOT NULL,
    charset      TEXT NOT NULL,
    collation    TEXT NOT NULL,
    note         TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'trashed')),
    suspended    TEXT NOT NULL DEFAULT '' CHECK (suspended IN ('', 'self', 'user')),
    suspended_at TEXT,
    created_at   TEXT NOT NULL,
    trashed_at   TEXT,
    purge_at     TEXT
) STRICT;
CREATE INDEX databases_owner ON databases (owner);

-- Ai chạy job (actor: tên user TPAN; rỗng là dòng lệnh, MCP hoặc bộ hẹn giờ)
-- và job thuộc về ai (owner: chủ của đối tượng job chạm vào, để user thấy cả
-- job admin chạy trên site của mình). Job cũ thuộc admin.
ALTER TABLE jobs ADD COLUMN actor TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs ADD COLUMN owner TEXT NOT NULL DEFAULT 'admin';
CREATE INDEX jobs_owner ON jobs (owner, started_at);

-- Ai làm (mục 6.12, "Audit"). Rỗng là dòng lệnh, MCP hoặc bộ hẹn giờ.
ALTER TABLE audit ADD COLUMN actor TEXT NOT NULL DEFAULT '';

-- Backup sống lâu hơn site (xoá hẳn site vẫn giữ backup), nên giữ chủ ngay ở
-- dòng backup thay vì suy từ bảng sites.
ALTER TABLE backups ADD COLUMN owner TEXT NOT NULL DEFAULT 'admin';
