// Package cloudinit sinh file user-data cho cloud-init (M18, mục 2.5 và 3.9
// của docs/thay-doi/M18-thiet-ke.md): máy ảo mới tạo từ file này tự tải đúng
// bản tpan đã ghim (kiểm sha256), rồi giao cho `tpan cloud-init run` dựng
// panel, stack và các site. Gói chỉ sinh chữ, không đọc ghi gì trên máy.
//
// Luật chống chèn nội dung (bất biến B12): giá trị do người dùng đưa chỉ đi vào
// NỘI DUNG file (base64), không bao giờ vào runcmd, đường dẫn hay khoá YAML.
// Bốn giá trị duy nhất đi vào script shell là Base, Version và hai sha256; cả
// bốn qua luật ký tự (CheckBase, versionRe, sha256) rồi mới được đặt trong nháy đơn.
//
// File này là hợp đồng (đợt 0). Thợ đợt 1 viết cloudinit.go với đúng các hàm:
//
//	// Render kiểm p (CheckBase, hai sha256 dạng 64 hex thường, Version theo
//	// VersionOK thì giữ, không thì "unknown"), dựng user-data bằng bộ ghi YAML
//	// go.yaml.in/yaml/v3 từ một cấu trúc Go (không ghép chuỗi): dòng đầu
//	// "#cloud-config", một dòng chú thích, write_files (config.json b64 0600;
//	// sites.yaml gz+b64 0600 khi có; script 0700), runcmd đúng một phần tử
//	// [ScriptPath]. Cỡ lớn hơn p.MaxBytes (0: DefaultMaxBytes) thì
//	// *TooLargeError. Đầu vào sai thì *InvalidError.
//	func Render(p Params) ([]byte, error)
//	// Script là nội dung script khởi động đầu (mục 3.9, nguyên văn khuôn).
//	func Script(p Params) string
//	// CheckBase: http:// hoặc https://, chỉ [A-Za-z0-9._~:/-], không "..",
//	// không "/" cuối, có host.
//	func CheckBase(s string) error
//	// VersionOK: ^[A-Za-z0-9._+-]{1,64}$.
//	func VersionOK(v string) bool
//	// ParseSums đọc một file SHA256SUMS (dòng "<64 hex>  <tên>" hoặc
//	// "<64 hex> *<tên>"), trả sha256 của tpan-linux-amd64 và tpan-linux-arm64.
//	// Thiếu một trong hai, hoặc một tên có hai dòng khác nhau: *InvalidError.
//	func ParseSums(text []byte) (amd64, arm64 string, err error)
//	// ParseConfig đọc và kiểm config.json (lần khởi động đầu): Version 1, Web
//	// là nginx hoặc apache, AdminHash rỗng hoặc bắt đầu bằng "$argon2id$",
//	// SitesFile rỗng hoặc đúng SitesPath. Trường lạ: lỗi.
//	func ParseConfig(b []byte) (Config, error)
package cloudinit

import (
	"strconv"
	"time"

	"github.com/doxuta/tpan/internal/i18n"
)

// Đường dẫn trên máy mới (cố định, không lấy từ người dùng).
const (
	ConfigPath    = "/etc/tpan/cloud/config.json"
	SitesPath     = "/etc/tpan/cloud/sites.yaml"
	ScriptPath    = "/usr/local/sbin/tpan-first-boot"
	StatePath     = "/var/lib/tpan/first-boot.json"
	LogPath       = "/var/log/tpan/first-boot.log"
	PasswordsPath = "/root/tpan-first-boot-passwords.txt"
	BinPath       = "/usr/local/bin/tpan"
	// DefaultMaxBytes: giới hạn nhỏ nhất đã tra (AWS EC2, 16 KB trước base64).
	DefaultMaxBytes = 16384
	// DefaultBaseFormat: nơi tải mặc định, %s là tên bản đang chạy.
	DefaultBaseFormat = "https://github.com/doxuta/tpan/releases/download/%s"
)

// Config là nội dung ConfigPath (mục 3.9). Không có bí mật dạng rõ: AdminHash
// là bản băm argon2id của một mật khẩu 32 ký tự ngẫu nhiên.
type Config struct {
	Version   int    `json:"version"` // luôn 1
	Web       string `json:"web"`     // "nginx" | "apache"
	Private   bool   `json:"private"`
	AdminHash string `json:"admin_hash"`
	SitesFile string `json:"sites_file,omitempty"` // "" hoặc SitesPath
}

// Params là đầu vào của Render.
type Params struct {
	Version  string // tên bản tpan (dòng chú thích của script)
	Base     string // nơi có tpan-linux-<arch>
	SumAMD64 string // sha256 hex của tpan-linux-amd64
	SumARM64 string // sha256 hex của tpan-linux-arm64
	Config   Config
	// SitesYAML là sites.yaml ĐÃ GHI LẠI từ cấu trúc đã kiểm (core làm);
	// nil: không có file site.
	SitesYAML []byte
	MaxBytes  int // 0: DefaultMaxBytes
}

// InvalidError: đầu vào của Render, ParseSums, ParseConfig sai. core đổi
// thành lời từ chối cloudinit_invalid.
type InvalidError struct{ Msg i18n.Text }

func (e *InvalidError) Error() string { return "cloudinit_invalid: " + e.Msg.Vi }

// TooLargeError: user-data lớn hơn trần. core đổi thành cloudinit_too_large.
type TooLargeError struct{ Size, Max int }

func (e *TooLargeError) Error() string {
	return "cloudinit_too_large: user-data " + strconv.Itoa(e.Size) + " byte, tối đa " + strconv.Itoa(e.Max) + " byte"
}

// State là nội dung StatePath: tiến độ của lần khởi động đầu (mục 3.9). File
// được tạo TRƯỚC việc đầu tiên; script khởi động đầu thấy file này thì không
// bao giờ đụng lại file chạy (bất biến B15).
type State struct {
	Started  time.Time   `json:"started"`
	Finished time.Time   `json:"finished,omitzero"`
	OK       bool        `json:"ok"`
	Steps    []StepState `json:"steps"`
}

// StepState là một việc của lần khởi động đầu: setup, admin, stack, sites, health.
type StepState struct {
	Name    string  `json:"name"`
	Status  string  `json:"status"` // "running" | "done" | "failed" | "skipped"
	JobID   string  `json:"job_id,omitempty"`
	Seconds float64 `json:"seconds"`
	Error   string  `json:"error,omitempty"`
}
