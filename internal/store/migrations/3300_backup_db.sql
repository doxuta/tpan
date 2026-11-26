-- Bản 3300 (M33): bản sao lưu của DB rời. Dòng của nó nằm chung bảng backups,
-- cột domain mang TÊN DB ("<owner>_<tên>": luôn có "_", còn domain không bao
-- giờ có, nên hai loại không lẫn nhau), kind là 'sql'. Hai cột dưới nhớ
-- charset và collation của DB lúc sao lưu, để "tạo lại DB đã xoá hẳn" dựng lại
-- đúng DB cũ (dòng của DB trong bảng databases khi đó đã mất). Dòng của site
-- để rỗng.
ALTER TABLE backups ADD COLUMN db_charset TEXT NOT NULL DEFAULT '';
ALTER TABLE backups ADD COLUMN db_collation TEXT NOT NULL DEFAULT '';
