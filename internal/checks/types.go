// Package checks chứa các phép kiểm: hàm nhỏ chỉ đọc, trả về trạng thái kèm
// bằng chứng. Một bộ phép kiểm dùng chung cho bốn chỗ: kiểm chứng sau mỗi thao
// tác, health định kỳ, lệnh `tpan health`, và các khẳng định trong e2e. Nhờ vậy
// "lúc tạo đã đúng" và "bây giờ còn đúng" có cùng một định nghĩa.
//
// Đặc tả: mục 6.7 của docs/KE-HOACH.md.
//
// Gói này chỉ import system và i18n. Nó KHÔNG được import engine, steps hay
// core: engine import checks để chạy bước kiểm chứng, nên chiều ngược lại sẽ
// tạo vòng import.
package checks

import (
	"context"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

// Check là một phép kiểm. Run chỉ đọc, không bao giờ thay đổi máy, và không trả
// lỗi: không kiểm được thì Result có Status là Unknown kèm lý do.
type Check interface {
	Run(ctx context.Context, sys system.System) Result
}

// Status là kết quả của một phép kiểm. Giao diện vẽ mỗi trạng thái một hình
// khác nhau (mục 10): chấm tròn, hình thoi, ô vuông có dấu ×, vòng rỗng.
type Status string

const (
	Pass    Status = "pass"    // đạt
	Warn    Status = "warn"    // cảnh báo: còn chạy nhưng sắp có chuyện (ví dụ đĩa 80%)
	Fail    Status = "fail"    // lỗi
	Unknown Status = "unknown" // chưa kiểm được (lệnh không chạy được, thiếu quyền)
)

// Stage là chặng mà phép kiểm thuộc về. Phép kiểm của một site xếp theo đường
// đi của request; phép kiểm của máy và của chính tpan có chặng riêng.
type Stage string

const (
	StageDNS       Stage = "dns"
	StageTLS       Stage = "tls"
	StageWeb       Stage = "web"       // web server: dịch vụ, config, vhost
	StagePHP       Stage = "php"       // PHP-FPM: dịch vụ, pool, socket
	StageWordPress Stage = "wordpress" // mã nguồn và quyền file
	StageDB        Stage = "db"        // MariaDB
	StageEndToEnd  Stage = "e2e"       // gọi HTTP thật vào site
	StageMachine   Stage = "machine"   // chung của máy: đĩa, RAM, cổng, dịch vụ dùng chung
	StageTPAN      Stage = "tpan"      // chính tpan
)

// Evidence là một lệnh đã chạy để kiểm, kèm output thật. Đây là thứ giao diện
// hiện ra khi người dùng bấm vào một dấu trạng thái (mục 10, ý 4).
type Evidence struct {
	Command  string `json:"command"` // system.Cmd.String()
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output"` // stdout rồi stderr, đã cắt bớt nếu quá dài
}

// Result là kết quả của một lần chạy phép kiểm.
type Result struct {
	// ID ổn định giữa các lần chạy, dạng "<loại>:<đối tượng>", ví dụ
	// "service.active:nginx". Health dùng nó để mở và đóng sự cố (M4).
	ID     string    `json:"id"`
	Stage  Stage     `json:"stage"`
	Title  i18n.Text `json:"title"` // phép kiểm này kiểm gì, viết cho người đọc
	Status Status    `json:"status"`
	// Summary là một câu nói kết quả, ví dụ "nginx đang chạy" hoặc
	// "cổng 3306 đang mở ra ngoài 127.0.0.1".
	Summary  i18n.Text  `json:"summary"`
	Evidence []Evidence `json:"evidence"`

	// Ba trường dưới chỉ có khi Status khác Pass.

	// Cause là nguyên nhân có thể.
	Cause i18n.Text `json:"cause,omitzero"`
	// ManualFix là các lệnh để người vận hành tự gõ, kèm lệnh kiểm lại ở cuối.
	// Luôn có khi Status là Fail (mục 6.7: "cách này luôn có").
	ManualFix []string `json:"manual_fix,omitempty"`
	// AutoFix là tên cách sửa tự động, để core dựng Plan sửa (M4). Rỗng là không
	// có. Là tên chứ không phải Plan, vì checks không được import engine.
	AutoFix string `json:"auto_fix,omitempty"`
}
