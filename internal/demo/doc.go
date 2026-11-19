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
package demo
