package steps

// Hợp đồng M6, đợt 0: các Step của thùng rác (mục 6.4 của docs/KE-HOACH.md).
// Thân viết ở đợt 1. Hàm và biến không xuất của file này mang tiền tố "trash".
//
// Đưa site vào thùng rác (core dựng Plan, theo đúng thứ tự mục 6.4):
//
//  1. Absent link vhost (Test, Reload web) và Absent pool (Test, Reload PHP):
//     dùng lại Absent của M1, KHÔNG gỡ file vhost trong sites-available (khôi
//     phục chỉ cần bật lại link).
//  2. DBUserLock: ALTER USER ... ACCOUNT LOCK; DB giữ nguyên.
//  3. SiteUserLock: usermod -L.
//  4. MoveTree{From: l.Dir, To: site.TrashDir(l)}: rename.
//
// Khôi phục làm ngược lại: MoveTree{From: TrashDir, To: l.Dir}, SiteUserUnlock,
// DBUserUnlock (của M4, fix.go), rồi ghi pool và bật vhost như tạo site.
//
// Mọi bước kiểm Layout bằng site.LayoutOf như site_web.go (webCheckLayout) và
// checkPath cho mọi đường dẫn. SQL qua stdin của mariadb như site_db.go.

import (
	"context"
	"errors"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

var (
	_ engine.Step = (*DBUserLock)(nil)
	_ engine.Step = (*SiteUserLock)(nil)
	_ engine.Step = (*MoveTree)(nil)
)

var errTrashTODO = errors.New("steps: bước thùng rác chưa viết (M6 đợt 1)")

// DBUserLock khoá user DB của site: ALTER USER '<db>'@'localhost' ACCOUNT LOCK
// (dùng lại dbLockSQL, dbLockStateSQL của fix.go). Check: user có và đang bị
// khoá; user không có thì lỗi (không có gì để khoá là máy lệch). Undo: mở khoá
// nếu chính Apply đã khoá. Danger: site thôi đăng nhập được DB.
type DBUserLock struct {
	Layout site.Layout

	locked bool // chính Apply đã khoá
}

func (s *DBUserLock) Info() engine.StepInfo                              { return engine.StepInfo{ID: "trash.dblock"} }
func (s *DBUserLock) Check(context.Context, system.System) (bool, error) { return false, errTrashTODO }
func (s *DBUserLock) Apply(context.Context, system.System) error         { return errTrashTODO }
func (s *DBUserLock) Undo(context.Context, system.System) error          { _ = s.locked; return errTrashTODO }

// SiteUserLock khoá (Unlock=false) hoặc mở khoá (Unlock=true) mật khẩu của
// user Linux của site: usermod -L | -U l.User. User của site không có mật khẩu
// và không có shell (mục 5); khoá là lớp thứ hai, để `passwd -S` cho thấy rõ
// user đang ở thùng rác. Check đọc `passwd -S l.User` (cột 2: "L" là khoá,
// "NP"/"P" là không). User không có thì lỗi. Undo: làm ngược lại nếu chính
// Apply đã đổi. ID: "user.lock:<domain>" hoặc "user.unlock:<domain>".
type SiteUserLock struct {
	Layout site.Layout
	Unlock bool

	changed bool
}

func (s *SiteUserLock) Info() engine.StepInfo { return engine.StepInfo{ID: "user.lock"} }
func (s *SiteUserLock) Check(context.Context, system.System) (bool, error) {
	return false, errTrashTODO
}
func (s *SiteUserLock) Apply(context.Context, system.System) error { return errTrashTODO }
func (s *SiteUserLock) Undo(context.Context, system.System) error  { _ = s.changed; return errTrashTODO }

// MoveTree chuyển cả một thư mục From sang To bằng system.Rename (mục 6.4,
// bước 4): cùng phân vùng thì tức thì và không bao giờ có bản chép dở.
//
//   - From và To phải là đường dẫn tuyệt đối sạch, mỗi cái nằm hẳn bên dưới
//     một trong hai gốc được phép: "/var/www" và site.TrashRoot (không bằng gốc,
//     không ra ngoài gốc, giống RemoveTree). Sai thì Check và Apply đều lỗi.
//   - Check: From không còn VÀ To có (là thư mục, Stat không theo symlink).
//     From còn và To cũng có: lỗi (hai bản, không đoán được bản nào đúng).
//     Cả hai không có: lỗi.
//   - Apply: MkdirAll thư mục cha của To (0700 root, chỉ phần chưa có) rồi
//     Rename. Rename trả lỗi khác phân vùng (EXDEV, errors.Is(err,
//     syscall.EXDEV)) thì trả lỗi nói rõ: "/var/www và /var/lib/tpan nằm trên
//     hai phân vùng khác nhau; tpan chưa hỗ trợ chép rồi xoá" (ponytail: chép
//     rồi xoá chưa làm, mục 16 ghi máy ảo cùng phân vùng; VPS cần đo).
//   - Undo: Rename To → From nếu chính Apply đã chuyển.
//   - Danger: site thôi phục vụ (file không còn ở chỗ web server tìm).
//   - ID: "dir.move:<From>". Title, Why do người gọi đặt; trống thì dùng câu
//     mặc định.
type MoveTree struct {
	From, To   string
	Title, Why i18n.Text

	moved bool
}

func (s *MoveTree) Info() engine.StepInfo                              { return engine.StepInfo{ID: "dir.move:" + s.From} }
func (s *MoveTree) Check(context.Context, system.System) (bool, error) { return false, errTrashTODO }
func (s *MoveTree) Apply(context.Context, system.System) error         { return errTrashTODO }
func (s *MoveTree) Undo(context.Context, system.System) error          { _ = s.moved; return errTrashTODO }
