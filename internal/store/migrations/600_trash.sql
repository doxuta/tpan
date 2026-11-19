-- Bản 600: thùng rác của M6 (mục 6.4 của docs/KE-HOACH.md).
-- purge_at là hạn xoá hẳn của site đang ở thùng rác (trashed_at + số ngày giữ,
-- setting trash.days, mặc định 7). NULL khi site không ở thùng rác. Ghi hạn lúc
-- đưa vào thùng rác thay vì tính lúc dọn: đổi trash.days về sau không làm site
-- đã vào thùng rác bị xoá sớm hơn lời đã hứa lúc xoá.
ALTER TABLE sites ADD COLUMN purge_at TEXT;
