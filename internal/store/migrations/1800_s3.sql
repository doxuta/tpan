-- Bản 1800: sao lưu ra kho S3 (M18, mục 6.16 của docs/KE-HOACH.md; thiết kế ở
-- docs/thay-doi/M18-thiet-ke.md mục 3.2). Khoảng số 1800-1899 là của M18.
--
-- Bản sao lưu nằm ở đâu. local: file ở cột path còn trên máy. remote: trạng
-- thái trên kho ngoài máy: '' chưa lên (hoặc đã dọn khỏi kho), 'stored' đã lên
-- và đã đọc lại khớp sha256, 'failed' lần đẩy gần nhất lỗi.
--
-- Mọi cột có DEFAULT: dòng của bản cũ tự thành "trên máy, chưa lên kho", không
-- cần bước chuyển dữ liệu nào.
ALTER TABLE backups ADD COLUMN local         INTEGER NOT NULL DEFAULT 1;
ALTER TABLE backups ADD COLUMN remote        TEXT    NOT NULL DEFAULT '';
-- Khoá object, tính từ sau tiền tố của đích (sites/<domain>/<tên file>).
ALTER TABLE backups ADD COLUMN remote_key    TEXT    NOT NULL DEFAULT '';
-- Mã của đích lúc đẩy (16 hex đầu của sha256 endpoint, bucket, tiền tố).
ALTER TABLE backups ADD COLUMN remote_target TEXT    NOT NULL DEFAULT '';
-- RFC 3339, lúc đọc lại khớp sha256.
ALTER TABLE backups ADD COLUMN remote_at     TEXT    NOT NULL DEFAULT '';
-- Lỗi lần đẩy gần nhất (một dòng, đã làm sạch, không có bí mật).
ALTER TABLE backups ADD COLUMN remote_error  TEXT    NOT NULL DEFAULT '';
-- Số lần đẩy lỗi "của riêng bản" liên tiếp (lỗi của đích không tính).
ALTER TABLE backups ADD COLUMN remote_tries  INTEGER NOT NULL DEFAULT 0;

CREATE INDEX backups_remote ON backups (remote, created_at);
