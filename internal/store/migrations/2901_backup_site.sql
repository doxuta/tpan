-- Bản 2901 (M29): bản backup nhớ site của nó là loại gì và có những tên phụ
-- nào, để "tạo lại site rồi khôi phục" dựng lại đúng site cũ. Dòng có từ trước
-- để rỗng: app rỗng nghĩa là WordPress (trước bản này chỉ có WordPress).
-- names: các tên phụ dạng "<tên>:<serve|redirect>", cách nhau một dấu cách.
ALTER TABLE backups ADD COLUMN app TEXT NOT NULL DEFAULT '';
ALTER TABLE backups ADD COLUMN names TEXT NOT NULL DEFAULT '';
