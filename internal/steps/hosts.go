package steps

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

var _ engine.Step = (*HostsBlock)(nil)

const (
	hostsPath = "/etc/hosts"
	// hostsTemplate là file mẫu cloud-init dùng để sinh lại /etc/hosts mỗi
	// lần khởi động khi manage_etc_hosts bật (thấy trên máy ảo M12: dòng chỉ
	// ghi vào /etc/hosts mất sau `multipass restart`). File mẫu có thì ghi
	// cùng dòng vào đó; manage_etc_hosts tắt thì cloud-init không đọc nó, ghi
	// thừa không hại. Không có file mẫu (máy không dùng cloud-init) thì bỏ qua.
	hostsTemplate = "/etc/cloud/templates/hosts.debian.tmpl"
	// hostsMark đánh dấu dòng của tpan. Chỉ dòng nguyên văn
	// "0.0.0.0 <Name> <hostsMark>" là của tpan; dòng người dùng tự viết (kể cả
	// chặn đúng tên đó) không bao giờ bị đụng.
	// ponytail: lời đánh dấu ghi cứng cho telemetry của kho Sury, việc duy
	// nhất đang dùng bước này. Chặn tên khác thì thêm trường lý do vào Step.
	hostsMark = "# tpan: chặn telemetry của kho PHP Sury"
)

// hostName: tên miền chặt, chữ thường ASCII, có ít nhất một dấu chấm, mỗi nhãn
// 1-63 ký tự không mở đầu hay kết thúc bằng gạch nối. Không dấu cách, không
// "#", không xuống dòng: tên lọt các ký tự đó sẽ thành một dòng hosts khác.
var hostName = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// HostsBlock đảm bảo /etc/hosts, và file mẫu hosts của cloud-init nếu có,
// mỗi file có đúng một dòng "0.0.0.0 <Name> # tpan: chặn telemetry của kho
// PHP Sury" (Remove: không còn dòng đó). Các dòng khác giữ nguyên từng byte,
// quyền và chủ của file cũng vậy.
//
// Vì sao cần: php-fpm của kho Ondřej Surý (từ 8.4) hỏi DNS tên
// telemetry.sury.org mỗi lần khởi động hoặc chạy `-t`, rồi gửi một gói tin
// DNS 277 byte thẳng tới địa chỉ đó và chờ trả lời (~2 giây trên máy ảo M12).
// Tên đó trỏ về 0.0.0.0 thì gói tin tới chính máy này, bị từ chối ngay.
//
// Info không khai Files: hai file này là của hệ thống (cloud-init, người
// quản trị cùng sửa). Khai vào Files thì engine ghi sha256 của CẢ file vào
// managed_files và health báo "sửa tay" mỗi khi ai khác thêm một dòng.
type HostsBlock struct {
	Name   string
	Remove bool

	prev    []saved // các file Apply đã ghi, theo thứ tự ghi
	applied bool
}

func (s *HostsBlock) line() string { return "0.0.0.0 " + s.Name + " " + hostsMark }

func (s *HostsBlock) validate() error {
	if len(s.Name) > 253 || !hostName.MatchString(s.Name) {
		return fmt.Errorf("tên %q không phải tên miền hợp lệ để chặn trong %s", s.Name, hostsPath)
	}
	return nil
}

func (s *HostsBlock) Info() engine.StepInfo {
	if s.Remove {
		return engine.StepInfo{
			ID:    "hosts.unblock:" + s.Name,
			Title: i18n.T("Gỡ dòng chặn "+s.Name+" khỏi "+hostsPath, "Remove the "+s.Name+" block from "+hostsPath),
			Why: i18n.T(
				"Không còn bản PHP nào cài từ kho của Ondřej Surý, nên dòng \""+s.line()+"\" tpan đã thêm vào "+hostsPath+" (và vào file mẫu "+hostsTemplate+" của cloud-init nếu có) không còn việc gì. Chỉ gỡ đúng dòng có dấu của tpan; mọi dòng khác giữ nguyên. Bước này hoàn tác được.",
				"No PHP version from Ondřej Surý's repository is left, so the line \""+s.line()+"\" tpan added to "+hostsPath+" (and to cloud-init's template "+hostsTemplate+" if present) has no job any more. Only the line with tpan's marker is removed; every other line stays. This step can be undone."),
		}
	}
	return engine.StepInfo{
		ID:    "hosts.block:" + s.Name,
		Title: i18n.T("Chặn "+s.Name+" trong "+hostsPath, "Block "+s.Name+" in "+hostsPath),
		Why: i18n.T(
			"PHP-FPM của kho Ondřej Surý gửi một gói tin DNS tới máy chủ "+s.Name+" mỗi lần khởi động hoặc kiểm cấu hình, rồi chờ trả lời khoảng 2 giây. tpan thêm dòng \""+s.line()+"\" vào "+hostsPath+": tên đó trỏ về chính máy này nên không có gì được gửi ra ngoài, hết 2 giây chờ, PHP chạy y hệt. Máy có cloud-init thì dòng này cũng được thêm vào file mẫu "+hostsTemplate+", vì cloud-init có thể sinh lại "+hostsPath+" từ file mẫu mỗi lần khởi động. Bỏ bước này thì mỗi lần cài, đổi bản hay nạp lại PHP chậm thêm 2 giây và máy báo về bên ngoài. Chỉ thêm đúng một dòng có dấu của tpan, không đụng các dòng khác.",
			"PHP-FPM from Ondřej Surý's repository sends a DNS packet to the server "+s.Name+" every time it starts or checks its configuration, then waits about 2 seconds for an answer. tpan adds the line \""+s.line()+"\" to "+hostsPath+": the name points back to this machine, so nothing leaves it, the 2-second wait is gone, and PHP runs exactly the same. On machines with cloud-init the line also goes into the template "+hostsTemplate+", because cloud-init may regenerate "+hostsPath+" from it at every boot. Without this step every PHP install, version switch or reload takes 2 seconds longer and the server reports to an outside host. Only one line with tpan's marker is added; other lines are not touched."),
	}
}

// readHostsFile đọc một trong hai file. Symlink, thư mục hay file đặc biệt thì
// từ chối: WriteFileAtomic sẽ thay symlink bằng file thường và cắt đứt thứ nó
// trỏ tới.
func readHostsFile(sys system.System, p string) (saved, error) {
	prev, err := capture(sys, p)
	if err != nil {
		return saved{}, err
	}
	if prev.link != "" {
		return saved{}, fmt.Errorf("%s là symlink tới %s; tpan không ghi qua symlink", p, prev.link)
	}
	return prev, nil
}

// targets đọc các file bước này lo: /etc/hosts (chưa có thì vẫn tính, Apply
// tạo), và file mẫu của cloud-init chỉ khi nó có.
func (s *HostsBlock) targets(sys system.System) ([]saved, error) {
	var out []saved
	for _, p := range []string{hostsPath, hostsTemplate} {
		f, err := readHostsFile(sys, p)
		if err != nil {
			return nil, err
		}
		if f.exists || p == hostsPath {
			out = append(out, f)
		}
	}
	return out, nil
}

// count đếm số dòng của tpan trong data.
func (s *HostsBlock) count(data []byte) int {
	n := 0
	for l := range strings.Lines(string(data)) {
		if strings.TrimRight(l, " \t\r\n") == s.line() {
			n++
		}
	}
	return n
}

func (s *HostsBlock) ok(f saved) bool {
	n := s.count(f.data)
	if s.Remove {
		return n == 0
	}
	return n == 1
}

func (s *HostsBlock) Check(_ context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	fs, err := s.targets(sys)
	if err != nil {
		return false, err
	}
	for _, f := range fs {
		if !s.ok(f) {
			return false, nil
		}
	}
	return true, nil
}

// edit trả nội dung mới của f: bỏ mọi dòng của tpan, rồi (khi chặn) thêm một
// dòng ở cuối.
func (s *HostsBlock) edit(f saved) []byte {
	var b strings.Builder
	for l := range strings.Lines(string(f.data)) {
		if strings.TrimRight(l, " \t\r\n") != s.line() {
			b.WriteString(l)
		}
	}
	out := b.String()
	if !s.Remove {
		if out != "" && !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		out += s.line() + "\n"
	}
	return []byte(out)
}

func (s *HostsBlock) Apply(_ context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	fs, err := s.targets(sys)
	if err != nil {
		return err
	}
	var done []saved
	for _, f := range fs {
		if s.ok(f) {
			continue
		}
		perm, owner := f.perm, f.owner
		if !f.exists {
			perm, owner = 0o644, system.Root
		}
		if err := sys.WriteFileAtomic(f.path, s.edit(f), perm, owner); err != nil {
			// Engine không gọi Undo cho Apply lỗi: tự trả các file đã ghi.
			err = fmt.Errorf("ghi %s: %w", f.path, err)
			return errors.Join(err, restoreAll(sys, done))
		}
		done = append(done, f)
	}
	s.prev, s.applied = done, true
	return nil
}

func (s *HostsBlock) Undo(_ context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("%s: %w", hostsPath, errNotApplied)
	}
	return restoreAll(sys, s.prev)
}

// restoreAll trả các file về nội dung cũ, theo thứ tự ngược với lúc ghi.
func restoreAll(sys system.System, fs []saved) error {
	var errs []error
	for _, f := range slices.Backward(fs) {
		errs = append(errs, f.restore(sys))
	}
	return errors.Join(errs...)
}
