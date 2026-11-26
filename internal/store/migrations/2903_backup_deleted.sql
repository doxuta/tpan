-- Bản 2903: dấu "người dùng đã xoá bản backup này". Xoá một bản đang được đẩy
-- lên kho thì dòng của nó mất ngay, nhưng job đẩy vẫn tải xong object; dấu này
-- cho job đẩy biết phải xoá object đó thay vì ghi lại dòng "chỉ còn trên kho"
-- (cách xử lý của bản bị DỌN giữa lúc đẩy). Dấu cũ hơn 7 ngày tự được bỏ.
CREATE TABLE backup_deleted (
    id         TEXT PRIMARY KEY,
    deleted_at TEXT NOT NULL
) STRICT;
