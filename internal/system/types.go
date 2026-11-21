// Package system là chỗ duy nhất trong tpan chạm vào máy: chạy lệnh, đọc ghi
// file, đổi quyền. Mọi gói khác đi qua giao diện System, nhờ vậy:
//
//   - unit test chạy trên Mac không cần root (dùng FakeSystem);
//   - không gói nào tự gọi os/exec hay nối chuỗi thành lệnh shell (mục 11);
//   - về sau tách đặc quyền chỉ phải thay bản cài đặt của System (mục 11, "Để sau").
//
// Đặc tả: mục 4.3 của docs/KE-HOACH.md.
//
// Gói này phải có hai bản cài đặt, viết ở các file khác trong gói:
//
//	// LocalSystem chạy lệnh thật và ghi file thật. Dùng trên máy chủ.
//	func NewLocal() *LocalSystem
//
//	// FakeSystem giữ hệ thống file và danh sách lệnh trong bộ nhớ.
//	// Dùng cho unit test và chế độ --demo. An toàn khi dùng từ nhiều goroutine.
//	func NewFake() *FakeSystem
//
// Cả hai thoả `var _ System = (*LocalSystem)(nil)` và `(*FakeSystem)(nil)`.
//
// FakeSystem có thêm các hàm cho test (các gói steps, checks, engine, core dựa
// vào đúng những chữ ký này):
//
//	// SetFile đặt sẵn một file thường, tự tạo các thư mục cha (chủ root).
//	func (f *FakeSystem) SetFile(path string, data []byte, perm fs.FileMode, owner Owner)
//	// SetDir đặt sẵn một thư mục, tự tạo các thư mục cha.
//	func (f *FakeSystem) SetDir(path string, perm fs.FileMode, owner Owner)
//	// SetSocket đặt sẵn một Unix socket (Stat trả Mode có fs.ModeSocket).
//	func (f *FakeSystem) SetSocket(path string, perm fs.FileMode, owner Owner)
//	// SetSymlink đặt sẵn một symlink.
//	func (f *FakeSystem) SetSymlink(target, link string)
//	// Handle đăng ký cách trả lời cho các lệnh có argv bắt đầu bằng prefix
//	// (prefix[0] là Cmd.Name). Nhiều handler khớp thì handler có prefix dài
//	// nhất thắng. fn có thể sửa hệ thống file giả (ví dụ giả lập apt-get install
//	// tạo /etc/nginx). Result có ExitCode khác 0 thì Run trả *ExitError như LocalSystem.
//	// Lệnh không khớp handler nào: ExitCode 0, output rỗng.
//	func (f *FakeSystem) Handle(prefix []string, fn func(cmd Cmd) Result)
//	// Fail làm cho thao tác op trên đúng đường dẫn path trả về err.
//	// op là một trong: "write", "remove", "rename", "mkdir", "symlink", "chown", "chmod".
//	func (f *FakeSystem) Fail(op, path string, err error)
//	// Ran trả về các lệnh đã gọi qua Run, theo thứ tự.
//	func (f *FakeSystem) Ran() []Cmd
//	// ResetRan xoá danh sách lệnh đã gọi (để test "chạy lại thì không lệnh nào được gọi").
//	func (f *FakeSystem) ResetRan()
//	// Snapshot chụp toàn bộ hệ thống file giả, để so trước và sau một thao tác.
//	func (f *FakeSystem) Snapshot() map[string]FakeEntry
//	// SetSite dựng dir và dir/public, chủ user:www-data, quyền 2750, như một
//	// site thật, để luật chủ của vùng site bật trong test (M28, S16).
//	func (f *FakeSystem) SetSite(dir, user string)
package system

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"
)

// System là mọi thao tác tpan được làm trên máy.
//
// Quy ước chung cho mọi hàm có tham số path: path là đường dẫn tuyệt đối.
// Đường dẫn không tồn tại thì lỗi trả về thoả errors.Is(err, fs.ErrNotExist),
// ở cả LocalSystem lẫn FakeSystem, để Step viết một kiểu kiểm cho cả hai.
//
// Vùng site (M28, docs/thay-doi/M28-thiet-ke.md mục 3.3): với đường dẫn mà
// sitefs.Split nhận (nằm hẳn bên dưới /var/www/<tên>/), mười hai hàm có
// đường dẫn đi qua cửa sitefs và theo luật C1 đến C10 thay cho câu tả riêng
// của từng hàm dưới đây: không đi theo symlink ở bất kỳ thành phần nào (kể cả
// neo; ReadFile, ReadDir, Chown, Chmod, StatFS từ chối symlink ở tên cuối),
// ReadFile chỉ đọc file thường của chính user site và không quá
// sitefs.MaxFile, Chown và Chmod chỉ nhận thư mục hoặc file thường một tên.
// Lời từ chối là *sitefs.Error (sitefs.Refused). Run từ chối lệnh chạy bằng
// root nhận đường dẫn trong vùng (ErrRootZoneCommand, mục 3.4). Ngoài vùng:
// giữ nguyên như dưới đây.
type System interface {
	// Run chạy một chương trình bằng argv, KHÔNG qua shell. Kết quả luôn được
	// trả về đầy đủ (kể cả khi có lỗi) để làm bằng chứng.
	//
	// Lỗi:
	//   - chương trình chạy xong với mã thoát khác 0: *ExitError;
	//   - không chạy được (không có chương trình, ctx bị huỷ): lỗi khác.
	// Ai coi mã thoát khác 0 là câu trả lời bình thường (ví dụ `systemctl
	// is-active` trả 3) thì dùng errors.As để lấy *ExitError.
	//
	// LocalSystem luôn đặt môi trường cố định LC_ALL=C và
	// DEBIAN_FRONTEND=noninteractive (cộng PATH chuẩn), không kế thừa môi
	// trường của tiến trình gọi.
	Run(ctx context.Context, cmd Cmd) (Result, error)

	// ReadFile đọc cả file. Đi theo symlink.
	ReadFile(path string) ([]byte, error)

	// WriteFileAtomic ghi ra file tạm trong cùng thư mục rồi rename, để không bao
	// giờ có file config ghi dở. Đặt quyền perm và chủ owner trước khi rename.
	// Thư mục cha phải có sẵn.
	WriteFileAtomic(path string, data []byte, perm fs.FileMode, owner Owner) error

	// MkdirAll tạo thư mục và các thư mục cha còn thiếu. Chỉ thư mục được tạo
	// mới nhận perm và owner; thư mục đã có giữ nguyên.
	MkdirAll(path string, perm fs.FileMode, owner Owner) error

	// Remove xoá một file, một symlink (không xoá đích của nó) hoặc một thư mục rỗng.
	Remove(path string) error

	// Rename đổi tên hoặc chuyển chỗ, như os.Rename.
	Rename(oldPath, newPath string) error

	// Symlink tạo link trỏ tới target. link đã tồn tại thì lỗi.
	Symlink(target, link string) error

	// Readlink đọc đích của một symlink.
	Readlink(path string) (string, error)

	// Stat KHÔNG đi theo symlink (như os.Lstat): symlink trả về chính nó, với
	// Mode có bit fs.ModeSymlink. Nhờ vậy kiểm được link ở sites-enabled.
	Stat(path string) (FileInfo, error)

	// ReadDir trả về tên các mục trong thư mục, đã sắp xếp. Dùng để dò
	// /etc/php/*/fpm mà không cần glob.
	ReadDir(path string) ([]string, error)

	// Chown đổi chủ và nhóm. Không đi theo symlink.
	Chown(path string, owner Owner) error

	// Chmod đổi quyền.
	Chmod(path string, perm fs.FileMode) error

	// StatFS cho biết dung lượng của phân vùng chứa path.
	StatFS(path string) (DiskUsage, error)

	// Lock giữ khoá toàn cục "đang có một job thay đổi máy" (mục 4.2). Khoá có
	// hiệu lực giữa các tiến trình: `tpan stack install` gõ ở CLI và `tpan serve`
	// là hai tiến trình khác nhau. LocalSystem dùng flock trên
	// /run/tpan/tpan.lock (tạo thư mục nếu thiếu); FakeSystem dùng mutex.
	// Chờ tới khi lấy được khoá hoặc ctx bị huỷ. Gọi unlock đúng một lần.
	Lock(ctx context.Context) (unlock func(), err error)
}

// Cmd là một lệnh sẽ chạy. Không có trường nào nhận cả câu lệnh shell.
type Cmd struct {
	// Name là chương trình, ví dụ "apt-get". LocalSystem tìm trong PATH cố định.
	Name string
	// Args là các tham số, mỗi phần tử là đúng một tham số, không bao giờ được tách lại.
	Args []string
	// Stdin là dữ liệu đưa vào chương trình. Dùng cho mật khẩu và câu SQL để
	// chúng không hiện trên dòng lệnh (`ps` xem được dòng lệnh của mọi tiến trình).
	// Không bao giờ được in ra log hay bằng chứng.
	Stdin []byte
	// AsUser khác rỗng thì chạy dưới user đó bằng `runuser -u <user> --`.
	// Rỗng là chạy bằng user hiện tại (root trên máy chủ).
	AsUser string
	// OnOutput khác nil thì được gọi cho từng dòng stdout và stderr ngay khi có,
	// không chờ lệnh xong. Engine dùng nó để đẩy log trực tiếp ra job.
	OnOutput func(line string)
	// Dir là thư mục làm việc; rỗng là "/". M8: `claude` của bộ não chạy trong
	// một thư mục rỗng riêng của phiên (mục 8.2).
	Dir string
	// Env là các biến môi trường THÊM vào môi trường cố định (dạng "K=V"; trùng
	// tên thì Env thắng). Chỉ dùng trên máy của người vận hành (`tpan brain`):
	// `claude` cần HOME để tự tìm đăng nhập của nó, và PATH để tìm được chính
	// nó. Có PATH trong Env thì Name cũng được tìm theo PATH đó. Không bao giờ
	// dùng cho lệnh chạy trên máy chủ.
	Env []string
}

// String trả về lệnh dạng người đọc được, dùng để hiển thị và làm bằng chứng.
// Không in Stdin. Tham số có khoảng trắng hoặc ký tự đặc biệt được đặt trong
// nháy đơn, chỉ để dễ đọc: chuỗi này không bao giờ được đem đi chạy.
func (c Cmd) String() string {
	var b strings.Builder
	if c.AsUser != "" {
		b.WriteString("runuser -u " + c.AsUser + " -- ")
	}
	b.WriteString(quote(c.Name))
	for _, a := range c.Args {
		b.WriteByte(' ')
		b.WriteString(quote(a))
	}
	return b.String()
}

func quote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n'\"\\$`;&|<>(){}*?[]!#~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Result là kết quả của một lệnh đã chạy xong.
type Result struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

// ExitError là lỗi Run trả về khi chương trình chạy xong với mã thoát khác 0.
type ExitError struct {
	Cmd    string // Cmd.String() của lệnh đã chạy
	Code   int
	Stderr string // vài dòng cuối của stderr, để thông báo lỗi có ngữ cảnh
}

func (e *ExitError) Error() string {
	msg := fmt.Sprintf("%s: mã thoát %d", e.Cmd, e.Code)
	if s := strings.TrimSpace(e.Stderr); s != "" {
		msg += ": " + s
	}
	return msg
}

// Owner là chủ và nhóm của một file. Trường rỗng nghĩa là root.
type Owner struct {
	User  string `json:"user"`
	Group string `json:"group"`
}

// Root là chủ mặc định của mọi file config tpan ghi.
var Root = Owner{User: "root", Group: "root"}

// FileInfo là thông tin của một file, thư mục hoặc symlink.
type FileInfo struct {
	Mode  fs.FileMode // gồm cả bit loại: fs.ModeDir, fs.ModeSymlink
	Size  int64
	Owner Owner
}

// DiskUsage là dung lượng của một phân vùng, tính bằng byte.
type DiskUsage struct {
	Total uint64 `json:"total"`
	Free  uint64 `json:"free"` // phần user thường dùng được (như cột Avail của df)
}

// FakeEntry là một mục trong hệ thống file giả, trả về bởi FakeSystem.Snapshot.
type FakeEntry struct {
	Mode   fs.FileMode // gồm bit loại như FileInfo.Mode
	Owner  Owner
	Data   string // nội dung, chỉ với file thường
	Target string // đích, chỉ với symlink
}

// Secret giữ một giá trị bí mật (mật khẩu, khoá). Mọi cách in thông thường
// (fmt, slog, JSON) chỉ ra "••••", nên lỡ đưa vào log cũng không lộ.
// Lấy giá trị thật bằng Reveal, và chỉ để đưa vào Cmd.Stdin hoặc file có quyền 600.
type Secret struct{ v string }

// NewSecret bọc một giá trị bí mật.
func NewSecret(v string) Secret { return Secret{v: v} }

// Reveal trả về giá trị thật.
func (s Secret) Reveal() string { return s.v }

func (s Secret) String() string               { return "••••" }
func (s Secret) GoString() string             { return "system.Secret(••••)" }
func (s Secret) LogValue() slog.Value         { return slog.StringValue("••••") }
func (s Secret) MarshalJSON() ([]byte, error) { return []byte(`"••••"`), nil }
func (s Secret) MarshalText() ([]byte, error) { return []byte("••••"), nil }
