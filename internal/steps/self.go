package steps

// SelfDownload và SelfInstall: `tpan self-update` (mục 6.11 của
// docs/KE-HOACH.md; hợp đồng trong m12_types.go).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"path"
	"regexp"
	"strings"
	"unicode"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

var (
	_ engine.Step = (*SelfDownload)(nil)
	_ engine.Step = (*SelfInstall)(nil)
)

// ================================================================ SelfDownload

// SelfDownload tải tpan-linux-<Arch> từ Base vào Dest và kiểm sha256 theo
// SHA256SUMS ở cùng chỗ, rồi chạy `Dest version` để đọc bản mới.
//
// sha256 tính trong Go từ file đã tải (không gọi sha256sum): file chạy bằng
// quyền root, nên kiểm toàn vẹn phải nằm ở mã tpan kiểm soát. Kiểm này chỉ
// chống file hỏng hay bị sửa giữa đường khi SHA256SUMS đến từ nguồn đáng tin,
// vì vậy Base phải là https (http chỉ cho máy chủ tải chạy ngay trên máy, dùng
// cho e2e trên máy ảo).
type SelfDownload struct {
	Base, Arch, Dest string
	// NewVersion khác nil thì nhận bản của file vừa tải: chữ thứ hai trong dòng
	// "tpan <bản> (commit ..., build ...)" mà `tpan version` in ra, tức là
	// buildinfo.Version của bản mới.
	NewVersion *string

	sum     string // sha256 lấy từ SHA256SUMS của lần Apply này; rỗng: chưa tải
	prev    saved
	applied bool
}

// selfLoopback là các host được dùng http (máy chủ tải chạy trên chính máy).
var selfLoopback = map[string]bool{"127.0.0.1": true, "localhost": true, "::1": true}

// selfSumLine là một dòng của sha256sum: 64 hex, một hoặc hai dấu cách, có
// thể có "*" (chế độ nhị phân) trước tên file.
var selfSumLine = regexp.MustCompile(`^([0-9a-fA-F]{64}) {1,2}\*?(\S+)$`)

const selfMode fs.FileMode = 0o755

// checkSelfBase nhận Base là https://host[/đường], hoặc http:// khi host là
// loopback. Không nhận user:pass, query, fragment, khoảng trắng hay ký tự điều
// khiển: Base được nối thành URL đưa cho curl, chữ lạ ở đây là curl tải nhầm chỗ.
func checkSelfBase(base string) (string, error) {
	if base == "" {
		return "", errors.New("gốc tải bản phát hành đang trống")
	}
	if strings.ContainsFunc(base, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return "", fmt.Errorf("gốc tải %q có khoảng trắng hoặc ký tự điều khiển", base)
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("gốc tải %q không phải URL: %w", base, err)
	}
	switch {
	case u.Host == "" || u.Opaque != "":
		return "", fmt.Errorf("gốc tải %q thiếu tên máy chủ", base)
	case u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "":
		return "", fmt.Errorf("gốc tải %q không được có user, query hay fragment", base)
	case u.Scheme == "https":
	case u.Scheme == "http" && selfLoopback[u.Hostname()]:
	default:
		return "", fmt.Errorf("gốc tải %q phải là https:// (http:// chỉ với 127.0.0.1, localhost, [::1])", base)
	}
	return strings.TrimRight(base, "/"), nil
}

// checkSelfArch: bản phát hành chỉ có hai file, tpan-linux-amd64 và
// tpan-linux-arm64. Arch được nối vào URL, nên chỉ nhận đúng hai chữ đó.
func checkSelfArch(arch string) error {
	if arch != "amd64" && arch != "arm64" {
		return fmt.Errorf("kiến trúc %q không hỗ trợ (amd64 hoặc arm64)", arch)
	}
	return nil
}

func (s *SelfDownload) validate() (base string, err error) {
	if base, err = checkSelfBase(s.Base); err != nil {
		return "", err
	}
	if err := checkSelfArch(s.Arch); err != nil {
		return "", err
	}
	if err := checkPath("file tải về", s.Dest); err != nil {
		return "", err
	}
	return base, nil
}

func (s *SelfDownload) asset() string { return "tpan-linux-" + s.Arch }

// curl: -f để lỗi HTTP (404) thành mã thoát khác 0 thay vì ghi trang lỗi vào
// file; --proto-redir =https để chuyển hướng (GitHub chuyển sang máy lưu
// file) không bao giờ hạ xuống http.
func selfCurl(maxTime string, args ...string) system.Cmd {
	return system.Cmd{Name: "curl", Args: append([]string{"-fsSL", "--max-time", maxTime, "--proto-redir", "=https"}, args...)}
}

func (s *SelfDownload) cmds(base string) (sums, bin, version system.Cmd) {
	return selfCurl("30", base+"/SHA256SUMS"),
		selfCurl("300", "-o", s.Dest, base+"/"+s.asset()),
		system.Cmd{Name: s.Dest, Args: []string{"version"}}
}

func (s *SelfDownload) Info() engine.StepInfo {
	info := engine.StepInfo{
		ID:    "self.download:" + s.Dest,
		Title: i18n.T("Tải bản tpan mới vào "+s.Dest, "Download the new tpan release to "+s.Dest),
		Why: i18n.T(
			"Bản mới được tải vào file riêng và kiểm sha256 theo SHA256SUMS của bản phát hành trước khi đụng tới file đang chạy. File hỏng hay bị sửa giữa đường thì bị xoá, không bao giờ được chạy bằng quyền root.",
			"The new release is downloaded to a separate file and its sha256 is checked against the release's SHA256SUMS before the running binary is touched. A corrupted or tampered file is deleted and never run as root.",
		),
	}
	if base, err := s.validate(); err == nil {
		sums, bin, version := s.cmds(base)
		info.Commands = []string{sums.String(), bin.String(), version.String()}
	}
	return info
}

// Check: Dest là file thường 0755 và sha256 khớp SHA256SUMS đã tải trong lần
// Apply này. Trước Apply thì chưa có SHA256SUMS để so, nên luôn false: bước
// này mỗi lần chạy đều tải lại, không tin một file .new còn sót.
func (s *SelfDownload) Check(_ context.Context, sys system.System) (bool, error) {
	if _, err := s.validate(); err != nil {
		return false, err
	}
	if s.sum == "" {
		return false, nil
	}
	fi, err := sys.Stat(s.Dest)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("kiểm %s: %w", s.Dest, err)
	}
	if !fi.Mode.IsRegular() || fi.Mode.Perm() != selfMode {
		return false, nil
	}
	data, err := sys.ReadFile(s.Dest)
	if err != nil {
		return false, fmt.Errorf("đọc %s: %w", s.Dest, err)
	}
	return sha256Hex(data) == s.sum, nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// parseSums tìm sha256 của name trong nội dung SHA256SUMS. Tên phải khớp đúng
// (tpan-linux-amd64.sig không tính). Hai dòng cùng tên mà khác số thì lỗi:
// không đoán dòng nào đúng.
func parseSums(text, name string) (string, error) {
	found := ""
	for line := range strings.Lines(text) {
		m := selfSumLine.FindStringSubmatch(strings.TrimRight(line, "\r\n"))
		if m == nil || m[2] != name {
			continue
		}
		sum := strings.ToLower(m[1])
		if found != "" && found != sum {
			return "", fmt.Errorf("SHA256SUMS có hai dòng khác nhau cho %s", name)
		}
		found = sum
	}
	if found == "" {
		return "", fmt.Errorf("SHA256SUMS không có dòng nào cho %s", name)
	}
	return found, nil
}

// parseVersion lấy bản từ dòng đầu của `tpan version`: "tpan <bản> (...)".
func parseVersion(out string) (string, error) {
	line, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
	f := strings.Fields(line)
	if len(f) < 2 || f[0] != "tpan" {
		return "", fmt.Errorf("output lạ %q (cần \"tpan <bản> ...\")", line)
	}
	return f[1], nil
}

func (s *SelfDownload) Apply(ctx context.Context, sys system.System) error {
	base, err := s.validate()
	if err != nil {
		return err
	}
	sumsCmd, binCmd, versionCmd := s.cmds(base)

	// SHA256SUMS trước: tải lỗi hay thiếu dòng thì máy chưa đổi gì, khỏi tải
	// cả file chạy vô ích.
	res, err := sys.Run(ctx, sumsCmd)
	if err != nil {
		return withStdout(fmt.Errorf("tải SHA256SUMS từ %s: %w", base, err), res)
	}
	want, err := parseSums(res.Stdout, s.asset())
	if err != nil {
		return fmt.Errorf("đọc SHA256SUMS từ %s: %w", base, err)
	}

	prev, err := capture(sys, s.Dest)
	if err != nil {
		return fmt.Errorf("tải bản tpan mới: %w", err)
	}
	// Từ đây lỗi nào cũng trả Dest về như trước (xoá nếu trước đó không có).
	restore := func(err error) error { return errors.Join(err, prev.restore(sys)) }
	if prev.exists {
		// Gỡ thứ đang ở Dest (bản .new còn sót, hay một symlink): curl -o đi
		// theo symlink và sẽ ghi đè đích của nó bằng quyền root.
		if err := sys.Remove(s.Dest); err != nil {
			return fmt.Errorf("gỡ %s cũ trước khi tải: %w", s.Dest, err)
		}
	}
	if res, err := sys.Run(ctx, binCmd); err != nil {
		return restore(withStdout(fmt.Errorf("tải %s từ %s: %w", s.asset(), base, err), res))
	}
	data, err := sys.ReadFile(s.Dest)
	if err != nil {
		return restore(fmt.Errorf("đọc file vừa tải %s: %w", s.Dest, err))
	}
	if got := sha256Hex(data); got != want {
		return restore(fmt.Errorf("%s tải về có sha256 %s, SHA256SUMS ghi %s; đã bỏ file %s", s.asset(), got, want, s.Dest))
	}
	if err := setPermOwner(sys, s.Dest, selfMode, system.Root); err != nil {
		return restore(err)
	}
	res, err = sys.Run(ctx, versionCmd)
	if err != nil {
		return restore(withStdout(fmt.Errorf("chạy thử bản vừa tải %s: %w", s.Dest, err), res))
	}
	v, err := parseVersion(res.Stdout)
	if err != nil {
		return restore(fmt.Errorf("đọc bản của %s: %w", s.Dest, err))
	}
	if s.NewVersion != nil {
		*s.NewVersion = v
	}
	s.sum, s.prev, s.applied = want, prev, true
	return nil
}

// Undo xoá file đã tải (hoặc trả thứ đã ở Dest trước đó).
func (s *SelfDownload) Undo(_ context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("tải bản tpan mới: %w", errNotApplied)
	}
	return s.prev.restore(sys)
}

// ================================================================== SelfLatest

// selfVersionName là dạng tên bản được nhận từ file VERSION của gốc tải:
// v0.2.0, v0.2.0-1-g025edb6, 1.0+build_5. Chữ này đến từ mạng và được in lên
// màn hình của panel, nên chỉ nhận chữ, số và . + _ -, tối đa 64 ký tự.
var selfVersionName = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.+_-]{0,63}$`)

// selfLatestTimeout (giây) ngắn hơn lúc tải thật (30): đây là nút "Kiểm bản
// mới" và lần tự kiểm của dòng phiên bản cuối trang, người dùng đang chờ.
const selfLatestTimeout = "15"

// SelfLatest hỏi gốc tải base xem bản đang phát hành là gì, không tải file
// chạy (M15, mục 6.14 ý 6). Hai lệnh curl, không ghi gì vào máy:
//   - <base>/SHA256SUMS, bắt buộc: sum là sha256 ghi cho tpan-linux-<arch>,
//     tức là số mà file tải về sẽ phải khớp.
//   - <base>/VERSION, tuỳ có (make release ghi file này từ M15): version là
//     tên bản. Không tải được, hoặc nội dung không phải đúng một tên bản
//     (trang lỗi HTML, nhiều dòng, ký tự lạ) thì version rỗng, không phải lỗi.
//
// base và arch được kiểm như SelfDownload trước khi chạy lệnh nào.
func SelfLatest(ctx context.Context, sys system.System, base, arch string) (sum, version string, err error) {
	if base, err = checkSelfBase(base); err != nil {
		return "", "", err
	}
	if err := checkSelfArch(arch); err != nil {
		return "", "", err
	}
	res, err := sys.Run(ctx, selfCurl(selfLatestTimeout, base+"/SHA256SUMS"))
	if err != nil {
		return "", "", fmt.Errorf("tải SHA256SUMS từ %s: %w", base, err)
	}
	if sum, err = parseSums(res.Stdout, "tpan-linux-"+arch); err != nil {
		return "", "", fmt.Errorf("đọc SHA256SUMS từ %s: %w", base, err)
	}
	// VERSION chỉ để hiện tên; quyết định "có bản khác" dựa vào sha256 ở trên.
	if res, err := sys.Run(ctx, selfCurl(selfLatestTimeout, base+"/VERSION")); err == nil {
		if v := strings.TrimSpace(res.Stdout); selfVersionName.MatchString(v) {
			version = v
		}
	}
	return sum, version, nil
}

// ================================================================= SelfInstall

// SelfInstall thay file chạy: Bin → Old, New → Bin. Hai lần rename trong cùng
// thư mục: rename là nguyên tử và Linux cho thay file đang chạy (tiến trình cũ
// giữ inode cũ tới khi thoát). Old là đường lùi cho `self-update --rollback`.
type SelfInstall struct {
	Bin, New, Old string

	prevOld saved
	applied bool
}

func (s *SelfInstall) validate() error {
	for _, p := range [][2]string{{"file chạy", s.Bin}, {"file mới", s.New}, {"file giữ bản cũ", s.Old}} {
		if err := checkPath(p[0], p[1]); err != nil {
			return err
		}
	}
	if s.Bin == s.New || s.Bin == s.Old || s.New == s.Old {
		return fmt.Errorf("file chạy, file mới, file giữ bản cũ phải khác nhau (%s, %s, %s)", s.Bin, s.New, s.Old)
	}
	// Cùng thư mục thì cùng phân vùng: rename không bao giờ thành chép dở.
	if d := path.Dir(s.Bin); path.Dir(s.New) != d || path.Dir(s.Old) != d {
		return fmt.Errorf("%s, %s, %s phải cùng một thư mục", s.Bin, s.New, s.Old)
	}
	return nil
}

func (s *SelfInstall) Info() engine.StepInfo {
	return engine.StepInfo{
		ID:    "self.install:" + s.Bin,
		Title: i18n.T("Thay "+s.Bin+" bằng "+s.New+", giữ bản cũ ở "+s.Old, "Replace "+s.Bin+" with "+s.New+", keeping the old one as "+s.Old),
		Why: i18n.T(
			"Đây là lúc bản mới thành file chạy của tpan. Bản cũ không bị xoá mà đổi tên thành "+s.Old+", để quay về được nếu bản mới có lỗi. Dịch vụ tpan chỉ dùng bản mới sau khi khởi động lại.",
			"This is when the new release becomes tpan's executable. The old one is not deleted but renamed to "+s.Old+", so you can go back if the new release misbehaves. The tpan service only uses the new release after a restart.",
		),
		Danger: true,
	}
}

// regular trả lỗi khi p không phải file thường: rename một symlink vào Bin là
// để root chạy thứ mà symlink trỏ tới.
func regular(sys system.System, what, p string) error {
	fi, err := sys.Stat(p)
	if err != nil {
		return fmt.Errorf("kiểm %s %s: %w", what, p, err)
	}
	if !fi.Mode.IsRegular() {
		return fmt.Errorf("%s %s không phải file thường (%v)", what, p, fi.Mode.Type())
	}
	return nil
}

// Check: New không còn và Bin có, tức là đã thay.
func (s *SelfInstall) Check(_ context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	_, err := sys.Stat(s.New)
	switch {
	case err == nil:
		return false, nil
	case !errors.Is(err, fs.ErrNotExist):
		return false, fmt.Errorf("kiểm %s: %w", s.New, err)
	}
	_, err = sys.Stat(s.Bin)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("kiểm %s: %w", s.Bin, err)
	}
	return true, nil
}

func (s *SelfInstall) Apply(_ context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	if err := regular(sys, "file mới", s.New); err != nil {
		return err
	}
	if err := regular(sys, "file chạy", s.Bin); err != nil {
		return err
	}
	// Old có sẵn (bản lùi của lần cập nhật trước) sẽ bị rename đè: giữ nội
	// dung để Undo trả lại.
	prevOld, err := capture(sys, s.Old)
	if err != nil {
		return fmt.Errorf("giữ bản cũ %s: %w", s.Old, err)
	}
	if err := sys.Rename(s.Bin, s.Old); err != nil {
		return fmt.Errorf("đổi %s thành %s: %w", s.Bin, s.Old, err)
	}
	if err := sys.Rename(s.New, s.Bin); err != nil {
		err = fmt.Errorf("đặt %s vào %s: %w", s.New, s.Bin, err)
		// Đưa file chạy cũ về chỗ ngay, rồi trả Old như trước.
		if rerr := sys.Rename(s.Old, s.Bin); rerr != nil {
			return errors.Join(err, fmt.Errorf("trả %s về %s: %w", s.Old, s.Bin, rerr))
		}
		return errors.Join(err, prevOld.restore(sys))
	}
	s.prevOld, s.applied = prevOld, true
	return nil
}

// Undo: Bin → New, Old → Bin, rồi trả Old như trước Apply.
func (s *SelfInstall) Undo(_ context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("thay file chạy %s: %w", s.Bin, errNotApplied)
	}
	if err := s.validate(); err != nil {
		return err
	}
	if err := sys.Rename(s.Bin, s.New); err != nil {
		return fmt.Errorf("đổi %s về %s: %w", s.Bin, s.New, err)
	}
	if err := sys.Rename(s.Old, s.Bin); err != nil {
		return fmt.Errorf("trả bản cũ %s về %s: %w", s.Old, s.Bin, err)
	}
	return s.prevOld.restore(sys)
}
