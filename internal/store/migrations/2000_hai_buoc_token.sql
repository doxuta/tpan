-- Bản 2000: đăng nhập hai bước và token API (M20, docs/thay-doi/M20-thiet-ke.md
-- mục 3.2 và 11). Khoảng số 2000-2099 là của M20.
--
-- Chỉ thêm bảng và cột có DEFAULT: dữ liệu của v0.2.0 không đổi nghĩa, không
-- ai bị đăng xuất, hai bước mặc định tắt (bất biến B6).
--
-- Mọi bảng mới có khoá ngoại tới users ON DELETE CASCADE (foreign_keys đã bật
-- cho mọi kết nối, store.go): xoá user thì token, hai bước, phiếu chờ của họ
-- mất trong cùng transaction, tạo lại user cùng tên không thừa hưởng gì (B7).

-- user_totp: một dòng mỗi user đã bắt đầu bật hai bước. enabled_at NULL = đang chờ xác nhận.
-- secret_enc = nonce(12) || AES-256-GCM(bí mật 20 byte), khoá ở /etc/tpan/secret.key.
-- last_step: bước thời gian của mã TOTP được nhận gần nhất (chống dùng lại).
-- failures, locked_until: đếm mã sai liên tiếp ở bước hai của user này (khoá theo user, mục 3.4).
-- lock_count: số lần bị khoá liên tiếp kể từ lần đúng gần nhất (khoá leo thang, cảnh báo health).
CREATE TABLE user_totp (
    user_name    TEXT PRIMARY KEY REFERENCES users (name) ON DELETE CASCADE,
    secret_enc   BLOB NOT NULL,
    created_at   TEXT NOT NULL,
    enabled_at   TEXT,
    last_step    INTEGER NOT NULL DEFAULT 0,
    failures     INTEGER NOT NULL DEFAULT 0,
    locked_until TEXT,
    lock_count   INTEGER NOT NULL DEFAULT 0
) STRICT;

-- Mã dự phòng: chỉ sha256 của mã đã chuẩn hoá (80 bit ngẫu nhiên, dò ngược không được).
CREATE TABLE user_recovery_codes (
    user_name   TEXT NOT NULL REFERENCES users (name) ON DELETE CASCADE,
    code_sha256 TEXT NOT NULL,
    used_at     TEXT,
    PRIMARY KEY (user_name, code_sha256)
) STRICT;

-- Phiếu chờ bước hai. Không phải phiên: authResolve không bao giờ đọc bảng này.
-- host: tên máy (Host bỏ cổng, chữ thường) lúc tạo phiếu; request khác host thì như không có phiếu.
CREATE TABLE login_challenges (
    token_sha256 TEXT PRIMARY KEY,
    user_name    TEXT NOT NULL REFERENCES users (name) ON DELETE CASCADE,
    ip           TEXT NOT NULL,
    host         TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    expires_at   TEXT NOT NULL,
    attempts     INTEGER NOT NULL DEFAULT 0
) STRICT;

-- Phiên gắn với tên máy đã tạo nó (S2 của mục 10): cookie host-only chỉ được
-- trình duyệt gửi lại đúng tên đó, nên request mang cookie mà Host khác là
-- cookie bị chép đi nơi khác. '' là phiên của bản cũ (nhận ở mọi host cho tới
-- khi hết hạn, để nâng cấp không đăng xuất ai).
ALTER TABLE sessions ADD COLUMN host TEXT NOT NULL DEFAULT '';

CREATE TABLE api_tokens (
    id           TEXT PRIMARY KEY,          -- 16 hex ngẫu nhiên, để liệt kê và thu hồi
    token_sha256 TEXT NOT NULL UNIQUE,
    user_name    TEXT NOT NULL REFERENCES users (name) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    scopes       TEXT NOT NULL,             -- apitoken.Scopes.String()
    hint         TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    expires_at   TEXT NOT NULL,
    revoked_at   TEXT,
    last_used_at TEXT,
    last_used_ip TEXT NOT NULL DEFAULT ''
) STRICT;
CREATE INDEX api_tokens_user ON api_tokens (user_name);

-- Nhật ký ghi token nào đã làm (rỗng: phiên đăng nhập, dòng lệnh, bộ hẹn giờ).
ALTER TABLE audit ADD COLUMN token_id TEXT NOT NULL DEFAULT '';
