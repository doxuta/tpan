-- Bản 1900: tên của site (tên chính, tên phụ) và luật chuyển hướng (M19,
-- docs/thay-doi/M19-thiet-ke.md mục 3.1). Khoảng 1900-1999 là của M19.

CREATE TABLE site_names (
    -- Đã qua validate.ParseDomain: chữ thường, punycode. PRIMARY KEY nên một tên
    -- chỉ thuộc một site trên toàn máy, kể cả tên chính của site khác (B2).
    name       TEXT PRIMARY KEY,
    -- Tên chính của site giữ tên này (sites.domain). Không khoá ngoại: tên chính
    -- của site đang tạo được giữ chỗ (pending) trước khi có dòng trong sites.
    site       TEXT NOT NULL,
    -- Chủ của site lúc giữ chỗ. Là bản chép của sites.owner: store.SaveSite ghi
    -- lại nó cho dòng tên chính mỗi lần lưu site (S4, S5).
    owner      TEXT NOT NULL,
    kind       TEXT NOT NULL CHECK (kind IN ('primary', 'alias')),
    -- serve: phục vụ cùng nội dung; redirect: 301 về tên chính.
    mode       TEXT NOT NULL DEFAULT 'serve' CHECK (mode IN ('serve', 'redirect')),
    -- pending: đã giữ chỗ, job của nó đang chạy; vhost chỉ vẽ tên active,
    -- trừ đúng job đang thêm tên đó. Dòng pending quá 1 giờ bị bỏ ở lần giữ
    -- chỗ sau (job đã chết giữa chừng, S9).
    state      TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('pending', 'active')),
    -- 1: tên có trong SAN của chứng chỉ đang dùng (ghi sau mỗi job site.ssl
    -- thành công, đọc từ cert.pem). Vhost vẽ cổng 443 theo cột này, nên vhost
    -- vẫn chỉ là hàm của DB; health ssl.names so cột với file chứng chỉ.
    in_cert    INTEGER NOT NULL DEFAULT 0 CHECK (in_cert IN (0, 1)),
    created_at TEXT NOT NULL,
    CHECK (kind = 'alias' OR (name = site AND mode = 'serve'))
) STRICT;
CREATE INDEX site_names_site ON site_names (site);

-- Mỗi site đang có (cả trong thùng rác) giữ đúng tên chính của nó, không thêm
-- tên phụ nào (B5). Site có SSL: tên chính đang có trong chứng chỉ. Không có
-- ràng buộc "một tên miền gốc một chủ": dữ liệu cũ có thể đã vi phạm, migration
-- không được hỏng vì nó (mục 6).
INSERT INTO site_names (name, site, owner, kind, mode, state, in_cert, created_at)
    SELECT domain, domain, owner, 'primary', 'serve', 'active', ssl, created_at FROM sites;

CREATE TABLE site_redirects (
    id         INTEGER PRIMARY KEY,
    site       TEXT NOT NULL REFERENCES sites (domain) ON DELETE CASCADE,
    -- Đã qua validate.ParseRedirectSource / ParseRedirectTarget.
    source     TEXT NOT NULL,
    target     TEXT NOT NULL,
    code       INTEGER NOT NULL CHECK (code IN (301, 302)),
    keep_query INTEGER NOT NULL DEFAULT 0 CHECK (keep_query IN (0, 1)),
    state      TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('pending', 'active')),
    created_at TEXT NOT NULL,
    UNIQUE (site, source)
) STRICT;
