-- Bản 2902 (M29): bản backup nhớ thêm các luật chuyển hướng của site, dạng
-- mảng JSON [{"source","target","code","keep_query"}]; rỗng là không ghi.
ALTER TABLE backups ADD COLUMN rules TEXT NOT NULL DEFAULT '';
