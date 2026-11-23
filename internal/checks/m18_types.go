package checks

// Hợp đồng hai phép kiểm của M18 (mục 3.13 của docs/thay-doi/M18-thiet-ke.md).
// Thợ đợt 1 viết m18.go với đúng hai hàm:
//
//	// S3Target (ID "tpan.s3:target", StageTPAN; health bọc bằng Heavy): gọi
//	// probe. probe lỗi (không kiểm được): Unknown. Problem rỗng: Pass, kèm
//	// Evidence của probe. Problem là một mã s3_* (lỗi của đích): Fail, Cause
//	// nói vì sao, ManualFix có "sudo tpan backup target test" và "sudo tpan
//	// backup target set …". Missing khác rỗng: Fail "kho mất bản …", ManualFix
//	// có "sudo tpan backup push".
//	func S3Target(probe func(ctx context.Context) (S3Probe, error)) Check
//	// S3Fresh (ID "tpan.s3:fresh", StageTPAN, nhẹ): gọi state, xét bốn ý
//	// (a) Failed > 0 hoặc TargetError khác rỗng; (b) Schedule và Stale khác
//	// rỗng; (c) Schedule và HasKey và (PanelLast zero hoặc cũ hơn 48 giờ so
//	// với now, hoặc PanelError khác rỗng); (d) !HasKey. Có ý nào: Warn, tóm
//	// tắt nêu số bản lỗi, tối đa 5 domain, lỗi gần nhất; ManualFix có
//	// "sudo tpan backup push" (và "sudo tpan backup key new" với ý d). Không
//	// ý nào: Pass. state lỗi: Unknown.
//	func S3Fresh(state func(ctx context.Context) (S3State, error), now func() time.Time) Check
//
// Cả hai chỉ được health thêm vào khi đã đặt đích (B11).

import "time"

// S3Probe là kết quả core trả cho S3Target.
type S3Probe struct {
	// Evidence: các lời gọi đã làm (Check, Head từng bản), không có khoá.
	Evidence []Evidence
	// Problem: rỗng khi nối được; một mã s3_* (s3_unreachable,
	// s3_auth_failed, s3_bucket_not_found, s3_error) khi không.
	Problem string
	// Detail: câu lỗi đã làm sạch của kho khi Problem khác rỗng.
	Detail string
	// Missing: "<domain> (<id>)" của các bản stored mới nhất mà kho không còn
	// object, hoặc object sai cỡ.
	Missing []string
}

// S3State là kết quả core trả cho S3Fresh (chỉ đọc tpan.db và settings).
type S3State struct {
	// Failed: số dòng remote='failed'; FailedDomains tối đa 5 domain.
	Failed        int
	FailedDomains []string
	// LastError: remote_error mới nhất của các dòng failed.
	LastError string
	// TargetError: setting s3.target_error.
	TargetError string
	// Schedule: lịch backup hằng ngày đang bật (backup.at khác rỗng).
	Schedule bool
	// Stale: domain các site active tạo từ 24 giờ trước mà không có bản full
	// lý do manual hay schedule, tạo trong 48 giờ, stored ở đích này.
	Stale []string
	// HasKey: có khoá sao lưu.
	HasKey bool
	// PanelLast: lúc của gói panel mới nhất đã lên kho (zero: chưa có).
	PanelLast  time.Time
	PanelError string
}
