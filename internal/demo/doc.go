// Package demo dựng một máy giả cho `tpan serve --demo`: FakeSystem có sẵn
// stack, vài site (có site đạt, có site hỏng ở một chặng), số liệu /proc,
// lịch sử job; và một tpan.db riêng. Nhờ vậy làm và xem giao diện được trên
// máy dev mà không đụng máy thật (mục 4.3 của docs/KE-HOACH.md).
//
// Hợp đồng M3 (thợ phần demo viết ở file khác trong gói):
//
//	// New dựng máy giả và DB trong thư mục dir (tạo nếu chưa có; dir rỗng
//	// thì dùng một thư mục tạm). Thao tác thật qua API (tạo site, xoá site)
//	// chạy được trên máy giả: các lệnh được FakeSystem trả lời như máy thật.
//	func New(ctx context.Context, dir string) (*system.FakeSystem, *store.Store, error)
//
// Bổ sung khi viết (không đổi chữ ký ở trên):
//
//   - Máy giả có stack nginx + PHP 8.3 + MariaDB, 3 site tạo bằng chính
//     core.Sites (blog.example.com và shop.example.net đạt mọi chặng;
//     wiki.example.org hỏng ở chặng PHP vì mất socket của pool, nên chặng gọi
//     HTTP cũng đỏ), một job tạo site thất bại đã hoàn tác (news.example.com),
//     /proc của máy 4 CPU 4 GB, đĩa 80 GiB đã dùng 42%.
//   - Máy giả không dùng được với core.Sites và core.Stack để trống: core ghim
//     mã băm của WordPress và WP-CLI thật, mà file giả không thể trùng. Hãy
//     dựng chúng bằng NewSites và NewStack, nếu không tạo site trên giao diện
//     demo hỏng ở bước tải WordPress và phép kiểm WP-CLI của stack báo lỗi.
//   - dir không được có sẵn tpan.db (xem chú thích của New).
//   - ctx của New phải sống suốt tiến trình: nó giữ goroutine làm số liệu CPU
//     nhúc nhích (hai lần đọc /proc/stat cách nhau ngắn phải thấy chênh lệch).
//   - Mật khẩu quản trị panel không do gói này đặt.
package demo
