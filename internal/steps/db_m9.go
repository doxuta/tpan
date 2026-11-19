package steps

// Đổi mật khẩu DB (mục 6.9) và hằng của wp-config.php (mục 6.10). Luật chung
// ở m9_types.go.

import (
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

// DBPassword đổi mật khẩu user DB của site (mục 6.9) mà mật khẩu không bao
// giờ nằm trên dòng lệnh hay trong log. Apply:
//  1. Đọc mật khẩu cũ từ wp-config.php bằng sys.ReadFile (không qua Run, vì
//     mọi output của Run vào log job): Stat (lstat) phải là file thường;
//     DB_PASSWORD phải đúng dạng mật khẩu tpan sinh ([A-Za-z0-9]{32}), không
//     thì từ chối (mật khẩu không do tpan sinh: đổi tay). Giữ để hoàn tác.
//  2. ALTER USER '<db>'@'localhost' IDENTIFIED BY '<mới>' qua Stdin của mariadb.
//  3. `wp config set DB_PASSWORD --prompt=value --quiet` dưới user của site,
//     mật khẩu qua Stdin (cách này phải kiểm trên máy ảo; mục 16).
//     Bước 3 lỗi thì đặt lại mật khẩu cũ ở bước 2 rồi mới trả lỗi.
//
// Check: chỉ true sau Apply của chính bước. Undo: bước 2 và 3 với mật khẩu cũ.
type DBPassword struct {
	Layout   site.Layout
	Password system.Secret
}

// WPConstant đặt một hằng trong wp-config.php bằng `wp config set <Name>
// <Value> --raw --type=constant --quiet` dưới user của site (giá trị không bí
// mật). Check: `wp config get <Name> --type=constant` in đúng Value (WP-CLI
// in true là "1"). Undo: khôi phục giá trị cũ (đọc trước khi Apply) hoặc
// `wp config delete` nếu trước đó chưa có. Dùng cho DISABLE_WP_CRON = true.
type WPConstant struct {
	Layout site.Layout
	Name   string // chỉ [A-Z_]
	Value  string // giá trị raw: true, false, hoặc số
}
