-- Bản 2800: sửa chủ của job đã ghi bởi bản cũ (M28 đợt 2, P7 của rà soát
-- v0.2.0). Khoảng số 2800-2899 là của M28.
--
-- Bản cũ suy chủ của mọi job từ Target: chủ của site có domain trùng Target,
-- không có thì chủ của DB rời có tên trùng Target. Job trên user (user.*) và
-- trên gói (cage.sync) có Target là tên user hay tên gói, nên một khách đặt DB
-- rời trùng tên đó (khach tạo DB "a" thành "khach_a") thành chủ của job admin
-- chạy trên user khach_a và đọc được nó. Từ bản này các job đó luôn thuộc
-- admin (store.jobOwnerLookup, core.adminOwned); dòng cũ cũng về admin.
UPDATE jobs SET owner = 'admin' WHERE kind LIKE 'user.%' OR kind = 'cage.sync';
