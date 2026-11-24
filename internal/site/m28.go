package site

// DBMaxUserConnections là trần số kết nối cùng lúc của mỗi user DB do tpan
// tạo (MAX_USER_CONNECTIONS của MariaDB; M28 đợt 2, lỗ M1-DBCONN). Thiếu trần,
// PHP của một site bị chiếm mở hết max_connections (151 mặc định) và mọi site
// khác trả lỗi 500.
//
// Vì sao 20: pool của site có pm.max_children = 5 (site-pool.conf.tmpl),
// WordPress giữ tối đa một kết nối mỗi request PHP, cộng các lệnh wp của tpan
// (health, backup, cron) chạy cùng lúc không quá 4: tối đa khoảng 9 kết nối
// hợp lệ, 20 là gấp đôi. Một chỗ cho ba nơi dùng (câu tạo user DB, Step
// DBUserLimit, phép kiểm db.user_limit) để chúng không lệch nhau. Đặt ở gói
// site vì checks không được import steps.
const DBMaxUserConnections = 20
