package steps

// Các Step apt và dịch vụ của M12 (mục 6.11 của docs/KE-HOACH.md): thêm kho
// apt ngoài Ubuntu, nâng và gỡ gói, khởi động lại hoặc dừng dịch vụ, thêm user
// vào nhóm, xuất toàn bộ MariaDB và chạy mariadb-upgrade. Hợp đồng ở
// m12_types.go; ba luật chung ở đầu common.go.

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

var (
	_ engine.Step = (*AptRepo)(nil)
	_ engine.Step = (*AptUpgrade)(nil)
	_ engine.Step = (*AptRemove)(nil)
	_ engine.Step = (*ServiceRestart)(nil)
	_ engine.Step = (*ServiceStop)(nil)
	_ engine.Step = (*UserInGroup)(nil)
	_ engine.Step = (*MariaDBDump)(nil)
	_ engine.Step = (*MariaDBUpgradeRun)(nil)
)

// aptLock: máy mới tạo có apt-daily và unattended-upgrades chạy nền giữ khoá
// dpkg vài phút. Không có tuỳ chọn này thì apt-get lỗi ngay ("Could not get
// lock"); có thì apt chờ tối đa 5 phút rồi mới lỗi.
var aptLock = []string{"-o", "DPkg::Lock::Timeout=300"}

// aptGet dựng `apt-get <lệnh và tuỳ chọn> -o DPkg::Lock::Timeout=300 <gói...>`.
func aptGet(cmd []string, pkgs ...string) system.Cmd {
	return system.Cmd{Name: "apt-get", Args: slices.Concat(cmd, aptLock, pkgs)}
}

var aptUpdateLocked = aptGet([]string{"update"})

// ===================================================================== AptRepo

// AptKeyPath là nơi AptRepo đặt khoá của kho name: keyring nhị phân mà dòng
// Signed-By của file sources trỏ tới. Khoá nằm riêng từng kho (không ở
// trusted.gpg.d) để khoá của kho này không ký được cho kho khác.
func AptKeyPath(name string) string { return "/etc/apt/keyrings/tpan-" + name + ".gpg" }

func aptSourcesPath(name string) string { return "/etc/apt/sources.list.d/tpan-" + name + ".sources" }
func aptPinPath(name string) string     { return "/etc/apt/preferences.d/tpan-" + name }

// AptRepo thêm một kho apt ngoài Ubuntu (mục 6.11): khoá ký, file sources
// deb822, file ghim, rồi apt-get update. Xem hợp đồng ở m12_types.go.
type AptRepo struct {
	Name         string
	Title        i18n.Text
	KeyURL       string
	Fingerprints []string
	Sources      []byte
	Pin          []byte

	prev    []saved  // ba file trước Apply, theo thứ tự đã đổi
	created []string // thư mục Apply tạo (keyrings trên máy cũ)
	applied bool
}

var (
	repoName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
	fprHex   = regexp.MustCompile(`^[0-9A-F]{40}$`)
)

// fprs trả các vân tay đã chuẩn hoá: chữ hoa, không dấu cách. Vân tay hay được
// chép từ trang web dạng "1505 8500 A023 ...", còn gpg in liền chữ hoa.
func (s *AptRepo) fprs() []string {
	out := make([]string, 0, len(s.Fingerprints))
	for _, f := range s.Fingerprints {
		out = append(out, strings.ToUpper(strings.ReplaceAll(f, " ", "")))
	}
	return out
}

func (s *AptRepo) validate() error {
	if !repoName.MatchString(s.Name) {
		return fmt.Errorf("tên kho apt %q trống, quá dài hoặc có ký tự không cho phép", s.Name)
	}
	if err := checkKeyURL(s.KeyURL); err != nil {
		return fmt.Errorf("kho %s: %w", s.Name, err)
	}
	if len(s.Fingerprints) == 0 {
		return fmt.Errorf("kho %s: chưa có vân tay khoá nào để tin", s.Name)
	}
	for _, f := range s.fprs() {
		if !fprHex.MatchString(f) {
			return fmt.Errorf("kho %s: vân tay %q không phải 40 ký tự hex", s.Name, f)
		}
	}
	// Sources không trỏ Signed-By vào đúng khoá này thì apt tin mọi khoá trong
	// trusted.gpg.d cho kho này, tức là khoá ta vừa kiểm chẳng giới hạn gì.
	if !strings.Contains(string(s.Sources), "Signed-By: "+AptKeyPath(s.Name)) {
		return fmt.Errorf("kho %s: nội dung sources thiếu dòng \"Signed-By: %s\"", s.Name, AptKeyPath(s.Name))
	}
	return nil
}

// checkKeyURL chỉ nhận https, có host, không có user:mật khẩu, không có khoảng
// trắng hay ký tự điều khiển. Khoá tải qua http thì ai đứng giữa đường cũng
// thay được; vân tay vẫn chặn, nhưng không lý do gì để mở cửa đó.
func checkKeyURL(raw string) error {
	if raw == "" || len(raw) > 2048 || strings.ContainsFunc(raw, func(r rune) bool { return r <= 0x20 || r == 0x7f }) {
		return fmt.Errorf("URL khoá %q trống, quá dài hoặc có khoảng trắng, ký tự điều khiển", raw)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("URL khoá %q: %w", raw, err)
	}
	if u.Scheme != "https" || u.Host == "" || u.User != nil {
		return fmt.Errorf("URL khoá %q phải là https://<host>/..., không có user", raw)
	}
	return nil
}

// ---- Thư mục tạm cho gpg ----

// gpg cần một homedir ghi được (kể cả cho --show-keys: nó tạo pubring.kbx và
// trustdb.gpg; homedir không có thì lỗi "directory does not exist"). Dùng
// homedir của root thì để lại rác và trộn khoá của kho vào khoá của người vận
// hành, nên mỗi lần dùng một thư mục mktemp riêng (0700, tên ngẫu nhiên, tạo
// nguyên tử nên không bị tráo bằng symlink) rồi xoá.
const gpgTmpPattern = "tpan-apt-key.XXXXXXXX"

var gpgTmpDir = regexp.MustCompile(`^/tmp/tpan-apt-key\.[A-Za-z0-9]{8}$`)

// gpgTmpShow là tên hiển thị trong Info (thư mục thật chỉ có lúc chạy).
const gpgTmpShow = "/tmp/" + gpgTmpPattern

// withGPGHome tạo homedir tạm, gọi fn, rồi luôn dọn. Đường dẫn mktemp trả về
// được kiểm khớp mẫu trước khi dùng: nó đi vào `rm -r` chạy bằng root.
func withGPGHome(ctx context.Context, sys system.System, fn func(home string) error) (err error) {
	res, err := sys.Run(ctx, system.Cmd{Name: "mktemp", Args: []string{"-d", "-p", "/tmp", gpgTmpPattern}})
	if err != nil {
		return withStdout(fmt.Errorf("tạo thư mục tạm cho gpg: %w", err), res)
	}
	home := strings.TrimSpace(res.Stdout)
	if !gpgTmpDir.MatchString(home) {
		return fmt.Errorf("tạo thư mục tạm cho gpg: mktemp trả đường dẫn lạ %q", home)
	}
	defer func() {
		// Job bị huỷ giữa chừng thì ctx đã hỏng, mà thư mục tạm vẫn phải dọn
		// (trong đó có khoá tải về): dọn bằng ctx không bị huỷ, có hạn giờ riêng.
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		// gpg --import khởi động gpg-agent cho homedir này; xoá thư mục mà không
		// tắt nó thì agent còn chạy tới khi tự phát hiện socket mất.
		var errs []error
		if r, kerr := sys.Run(cctx, system.Cmd{Name: "gpgconf", Args: []string{"--homedir", home, "--kill", "all"}}); kerr != nil {
			errs = append(errs, withStdout(fmt.Errorf("tắt gpg-agent của %s: %w", home, kerr), r))
		}
		if r, rerr := sys.Run(cctx, system.Cmd{Name: "rm", Args: []string{"-r", "--one-file-system", "--", home}}); rerr != nil {
			errs = append(errs, withStdout(fmt.Errorf("xoá thư mục tạm %s: %w", home, rerr), r))
		}
		err = errors.Join(append([]error{err}, errs...)...)
	}()
	return fn(home)
}

func gpgCmd(home string, args ...string) system.Cmd {
	return system.Cmd{Name: "gpg", Args: append([]string{"--homedir", home, "--batch"}, args...)}
}

func gpgKeyring(home string, args ...string) system.Cmd {
	return gpgCmd(home, append([]string{"--no-default-keyring", "--keyring", home + "/k.kbx"}, args...)...)
}

// primaryFprs đọc vân tay các khoá CHÍNH trong file bằng
// `gpg --show-keys --with-colons`. Mỗi khoá là một dòng "pub" theo sau là dòng
// "fpr" của nó; dòng "fpr" sau "sub" là của khoá con và bị bỏ qua, vì Signed-By
// tin cả khoá chính lẫn khoá con của nó, nên thứ phải khớp là khoá chính.
// Trường 10 của dòng fpr là vân tay (`man gpg`, DETAILS trong mã nguồn GnuPG).
func primaryFprs(ctx context.Context, sys system.System, home, file string) ([]string, error) {
	res, err := sys.Run(ctx, gpgCmd(home, "--show-keys", "--with-colons", file))
	if err != nil {
		return nil, withStdout(fmt.Errorf("đọc vân tay trong %s: %w", file, err), res)
	}
	var out []string
	afterPub := false
	for line := range strings.Lines(res.Stdout) {
		f := strings.Split(strings.TrimRight(line, "\n"), ":")
		switch f[0] {
		case "pub":
			afterPub = true
		case "fpr":
			if afterPub && len(f) > 9 {
				out = append(out, strings.ToUpper(f[9]))
			}
			afterPub = false
		case "sub", "sec", "ssb":
			afterPub = false
		}
	}
	return out, nil
}

// trusted: có ít nhất một khoá và mọi khoá chính đều nằm trong want.
func trusted(got, want []string) bool {
	if len(got) == 0 {
		return false
	}
	for _, f := range got {
		if !slices.Contains(want, f) {
			return false
		}
	}
	return true
}

func (s *AptRepo) curl(home string) system.Cmd {
	return system.Cmd{Name: "curl", Args: []string{"-fsSL", "--proto", "=https", "--max-time", "60", "-o", home + "/key", s.KeyURL}}
}

func (s *AptRepo) exportCmd(home string) system.Cmd {
	return gpgKeyring(home, append([]string{"--yes", "--output", home + "/export.gpg", "--export"}, s.fprs()...)...)
}

// fetchKey tải khoá, giữ lại CHỈ các khoá chính có vân tay trong danh sách, và
// trả keyring nhị phân. Chưa ghi gì vào /etc: khoá lạ thì máy không đổi.
func (s *AptRepo) fetchKey(ctx context.Context, sys system.System) (key []byte, err error) {
	err = withGPGHome(ctx, sys, func(home string) error {
		if res, err := sys.Run(ctx, s.curl(home)); err != nil {
			return withStdout(fmt.Errorf("tải khoá của kho %s từ %s: %w", s.Name, s.KeyURL, err), res)
		}
		if res, err := sys.Run(ctx, gpgKeyring(home, "--import", home+"/key")); err != nil {
			return withStdout(fmt.Errorf("đọc khoá của kho %s (file tải về không phải khoá OpenPGP?): %w", s.Name, err), res)
		}
		if res, err := sys.Run(ctx, s.exportCmd(home)); err != nil {
			return withStdout(fmt.Errorf("tách khoá của kho %s: %w", s.Name, err), res)
		}
		out := home + "/export.gpg"
		noMatch := fmt.Errorf("khoá tải từ %s không có khoá chính nào mang vân tay %s; không thêm kho %s",
			s.KeyURL, strings.Join(s.fprs(), ", "), s.Name)
		// gpg không ghi file khi không có vân tay nào khớp ("nothing exported").
		if _, err := sys.Stat(out); errors.Is(err, fs.ErrNotExist) {
			return noMatch
		} else if err != nil {
			return fmt.Errorf("kiểm %s: %w", out, err)
		}
		// Kiểm lại thứ sắp ghi: gpg khớp cả vân tay khoá con rồi xuất nguyên
		// khoá chính của nó, mà khoá chính đó không có trong danh sách.
		got, err := primaryFprs(ctx, sys, home, out)
		if err != nil {
			return err
		}
		if !trusted(got, s.fprs()) {
			return noMatch
		}
		if key, err = sys.ReadFile(out); err != nil {
			return fmt.Errorf("đọc khoá đã tách của kho %s: %w", s.Name, err)
		}
		return nil
	})
	return key, err
}

// target là một file AptRepo quản lý: data nil nghĩa là file phải không có.
type aptTarget struct {
	path string
	data []byte
}

func (s *AptRepo) targets(key []byte) []aptTarget {
	return []aptTarget{
		{AptKeyPath(s.Name), key},
		{aptSourcesPath(s.Name), s.Sources},
		{aptPinPath(s.Name), s.Pin},
	}
}

func (s *AptRepo) Info() engine.StepInfo {
	title := orText(s.Title, i18n.T(s.Name, s.Name))
	pin := engine.FileChange{Path: aptPinPath(s.Name), New: s.Pin}
	if s.Pin == nil {
		pin = engine.FileChange{Path: aptPinPath(s.Name), Delete: true}
	}
	return engine.StepInfo{
		ID:    "apt.repo:" + s.Name,
		Title: i18n.T("Thêm "+title.Vi, "Add "+title.En),
		Why: i18n.T(
			"Đây là nguồn gói ngoài Ubuntu: từ giờ apt cài và cập nhật gói từ kho này, ký bằng khoá có vân tay "+strings.Join(s.fprs(), ", ")+". Không có bước này thì không cài được bản bạn chọn (Ubuntu không có). File ghim giữ cho các gói khác vẫn lấy từ Ubuntu.",
			"This is a package source outside Ubuntu: from now on apt installs and updates packages from it, signed by the key with fingerprint "+strings.Join(s.fprs(), ", ")+". Without this step the version you chose cannot be installed (Ubuntu does not ship it). The pin file keeps every other package coming from Ubuntu.",
		),
		// Không khai khoá trong Files: nó là file nhị phân (không vẽ diff được),
		// và engine ghi sha256 của Files.New vào sổ file tpan quản lý, nên một
		// dòng mô tả ở đó sẽ làm health báo khoá "bị sửa tay". Vân tay nằm ở
		// lệnh gpg --export dưới đây và ở Why.
		Commands: []string{
			s.curl(gpgTmpShow).String(),
			gpgKeyring(gpgTmpShow, "--import", gpgTmpShow+"/key").String(),
			s.exportCmd(gpgTmpShow).String() + "  # → " + AptKeyPath(s.Name),
			aptUpdateLocked.String(),
		},
		Files:  []engine.FileChange{{Path: aptSourcesPath(s.Name), New: s.Sources}, pin},
		Danger: true,
	}
}

// fileIs: p là file thường, 0644, chủ root, đúng nội dung.
func fileIs(sys system.System, p string, want []byte) (bool, error) {
	fi, err := sys.Stat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("kiểm %s: %w", p, err)
	}
	if !fi.Mode.IsRegular() || fi.Mode.Perm() != 0o644 || normOwner(fi.Owner) != system.Root {
		return false, nil
	}
	if want == nil {
		return true, nil
	}
	data, err := sys.ReadFile(p)
	if err != nil {
		return false, fmt.Errorf("đọc %s: %w", p, err)
	}
	return string(data) == string(want), nil
}

func absent(sys system.System, p string) (bool, error) {
	_, err := sys.Stat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("kiểm %s: %w", p, err)
	}
	return false, nil
}

// Check: sources và pin đúng nội dung, khoá chỉ chứa khoá chính có vân tay
// trong danh sách. Apply luôn chạy apt-get update trước khi báo xong (lỗi thì
// trả file về), nên "ba file đúng" cũng là "đã update sau khi ghi". Khoá được so
// bằng vân tay chứ không so byte: cùng khoá xuất ở hai lần có thể khác byte
// (chữ ký mới thêm vào khoá trên máy chủ của kho).
func (s *AptRepo) Check(ctx context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	ok, err := fileIs(sys, aptSourcesPath(s.Name), s.Sources)
	if err != nil || !ok {
		return false, err
	}
	if s.Pin != nil {
		ok, err = fileIs(sys, aptPinPath(s.Name), s.Pin)
	} else {
		ok, err = absent(sys, aptPinPath(s.Name))
	}
	if err != nil || !ok {
		return false, err
	}
	key := AptKeyPath(s.Name)
	if ok, err = fileIs(sys, key, nil); err != nil || !ok {
		return false, err
	}
	err = withGPGHome(ctx, sys, func(home string) error {
		got, err := primaryFprs(ctx, sys, home, key)
		ok = err == nil && trusted(got, s.fprs())
		return err
	})
	return ok, err
}

func (s *AptRepo) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	key, err := s.fetchKey(ctx, sys)
	if err != nil {
		return err
	}
	var (
		done    []saved
		created []string
	)
	// fail trả mọi thứ đã đổi về như trước: engine không Undo bước có Apply lỗi.
	fail := func(err error) error {
		errs := []error{err}
		for _, p := range slices.Backward(done) {
			if rerr := p.restore(sys); rerr != nil {
				errs = append(errs, rerr)
			}
		}
		if rerr := removeDirs(sys, created); rerr != nil {
			errs = append(errs, rerr)
		}
		return errors.Join(errs...)
	}
	for _, t := range s.targets(key) {
		dir := path.Dir(t.path)
		if t.data != nil {
			// /etc/apt/keyrings có sẵn từ Ubuntu 22.04, nhưng máy nâng cấp từ bản
			// cũ có thể thiếu; tạo thì nhớ để Undo xoá.
			miss, err := missingDirs(sys, dir)
			if err != nil {
				return fail(err)
			}
			if len(miss) > 0 {
				if err := sys.MkdirAll(dir, 0o755, system.Root); err != nil {
					return fail(fmt.Errorf("tạo thư mục %s: %w", dir, err))
				}
				created = append(created, miss...)
			}
		}
		prev, err := capture(sys, t.path)
		if err != nil {
			return fail(fmt.Errorf("thêm kho %s: %w", s.Name, err))
		}
		switch {
		case t.data != nil:
			done = append(done, prev)
			if err := sys.WriteFileAtomic(t.path, t.data, 0o644, system.Root); err != nil {
				return fail(fmt.Errorf("ghi %s: %w", t.path, err))
			}
		case prev.exists:
			done = append(done, prev)
			if err := sys.Remove(t.path); err != nil {
				return fail(fmt.Errorf("gỡ %s: %w", t.path, err))
			}
		}
	}
	if res, err := RunAptUpdate(ctx, sys, aptUpdateLocked); err != nil {
		return fail(withStdout(fmt.Errorf("apt-get update sau khi thêm kho %s: %w", s.Name, err), res))
	}
	s.prev, s.created, s.applied = done, created, true
	return nil
}

// Undo trả ba file và thư mục về như trước rồi update, để apt quên kho này.
func (s *AptRepo) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("kho apt %s: %w", s.Name, errNotApplied)
	}
	var errs []error
	for _, p := range slices.Backward(s.prev) {
		if err := p.restore(sys); err != nil {
			errs = append(errs, err)
		}
	}
	if err := removeDirs(sys, s.created); err != nil {
		errs = append(errs, err)
	}
	if res, err := RunAptUpdate(ctx, sys, aptUpdateLocked); err != nil {
		errs = append(errs, withStdout(fmt.Errorf("apt-get update sau khi gỡ kho %s: %w", s.Name, err), res))
	}
	return errors.Join(errs...)
}

// ================================================================== AptUpgrade

// AptUpgrade nâng các gói đã cài lên bản mới nhất mà kho đang có, giữ file cấu
// hình đang dùng (--force-confold: không hỏi, không thay config tpan đã ghi).
type AptUpgrade struct{ Packages []string }

func (s *AptUpgrade) cmd() system.Cmd {
	return system.Cmd{Name: "apt-get", Args: slices.Concat([]string{"install", "-y", "--only-upgrade"}, aptLock,
		[]string{"-o", "Dpkg::Options::=--force-confdef", "-o", "Dpkg::Options::=--force-confold"}, s.Packages)}
}

// simulate có -s đứng trước "install": apt chỉ in việc sẽ làm, không đổi gì.
func (s *AptUpgrade) simulate() system.Cmd {
	return aptGet([]string{"-s", "install", "--only-upgrade"}, s.Packages...)
}

func (s *AptUpgrade) Info() engine.StepInfo {
	names := strings.Join(s.Packages, " ")
	return engine.StepInfo{
		ID:    "apt.upgrade:" + strings.Join(s.Packages, ","),
		Title: i18n.T("Nâng gói: "+names, "Upgrade packages: "+names),
		Why: i18n.T(
			"Bản mới vá lỗi và lỗ hổng. Không nâng thì máy tiếp tục chạy bản có lỗi đã biết. File cấu hình đang dùng được giữ nguyên.",
			"New versions fix bugs and security holes. Without upgrading the machine keeps running versions with known problems. The configuration files in use are kept.",
		),
		Commands: []string{s.cmd().String()},
		// Gói dịch vụ tự khởi động lại dịch vụ trong lúc nâng.
		Danger: true,
		NoUndo: i18n.T(
			"apt không hạ gói về bản cũ một cách an toàn (bản cũ có thể không còn trên kho, gói khác có thể đã cần bản mới).",
			"apt cannot safely downgrade a package (the old version may be gone from the repository, and other packages may now need the new one).",
		),
	}
}

func (s *AptUpgrade) Check(ctx context.Context, sys system.System) (bool, error) {
	if err := checkNames("gói", debName, s.Packages); err != nil {
		return false, err
	}
	res, err := sys.Run(ctx, s.simulate())
	if err != nil {
		return false, withStdout(fmt.Errorf("hỏi apt gói nào có bản mới: %w", err), res)
	}
	// Dòng dạng "Inst nginx [1.24.0-2ubuntu7.4] (1.24.0-2ubuntu7.5 ...)"; gói
	// kiến trúc khác có đuôi ":i386". Inst của gói phụ thuộc thì không tính.
	for line := range strings.Lines(res.Stdout) {
		rest, ok := strings.CutPrefix(line, "Inst ")
		if !ok {
			continue
		}
		name, _, _ := strings.Cut(rest, " ")
		name, _, _ = strings.Cut(name, ":")
		if slices.Contains(s.Packages, name) {
			return false, nil
		}
	}
	return true, nil
}

func (s *AptUpgrade) Apply(ctx context.Context, sys system.System) error {
	if err := checkNames("gói", debName, s.Packages); err != nil {
		return err
	}
	if res, err := sys.Run(ctx, s.cmd()); err != nil {
		return withStdout(fmt.Errorf("nâng gói %s: %w", strings.Join(s.Packages, " "), err), res)
	}
	cageRebindIdentity(ctx, sys) // gói có thể tạo user, nhóm hệ thống (apache2 tạo nhóm ssl-cert)
	return nil
}

func (s *AptUpgrade) Undo(context.Context, system.System) error { return nil }

// =================================================================== AptRemove

// AptRemove gỡ gói, giữ file cấu hình (remove, không purge): cài lại thì
// cấu hình cũ quay về.
type AptRemove struct{ Packages []string }

func (s *AptRemove) cmd() system.Cmd {
	return aptGet([]string{"remove", "-y"}, s.Packages...)
}

func (s *AptRemove) Info() engine.StepInfo {
	names := strings.Join(s.Packages, " ")
	return engine.StepInfo{
		ID:    "apt.remove:" + strings.Join(s.Packages, ","),
		Title: i18n.T("Gỡ gói: "+names, "Remove packages: "+names),
		Why: i18n.T(
			"Gói không còn site nào dùng vẫn chạy dịch vụ, chiếm RAM và vẫn phải vá. Gỡ đi thì máy nhẹ hơn và ít chỗ phải lo hơn. File cấu hình được giữ.",
			"A package no site uses still runs its service, takes memory and still needs patching. Removing it makes the machine lighter and leaves less to look after. Configuration files are kept.",
		),
		Commands: []string{s.cmd().String()},
		Danger:   true,
		NoUndo: i18n.T(
			"Cài lại có thể kéo bản mới hơn bản vừa gỡ. Cần thì cài lại bằng tay; file cấu hình vẫn còn.",
			"Reinstalling may pull a newer version than the one removed. Reinstall by hand if needed; the configuration files are still there.",
		),
	}
}

func (s *AptRemove) Check(ctx context.Context, sys system.System) (bool, error) {
	if err := checkNames("gói", debName, s.Packages); err != nil {
		return false, err
	}
	missing, err := missingPackages(ctx, sys, s.Packages)
	return len(missing) == len(s.Packages), err
}

func (s *AptRemove) Apply(ctx context.Context, sys system.System) error {
	if err := checkNames("gói", debName, s.Packages); err != nil {
		return err
	}
	if res, err := sys.Run(ctx, s.cmd()); err != nil {
		return withStdout(fmt.Errorf("gỡ gói %s: %w", strings.Join(s.Packages, " "), err), res)
	}
	cageRebindIdentity(ctx, sys) // gói có thể tạo user, nhóm hệ thống (apache2 tạo nhóm ssl-cert)
	return nil
}

func (s *AptRemove) Undo(context.Context, system.System) error { return nil }

// ================================================================ dịch vụ

func checkUnit(unit string) error {
	if !unitName.MatchString(unit) {
		return fmt.Errorf("tên dịch vụ %q trống hoặc có ký tự không cho phép", unit)
	}
	return nil
}

// unitActive: `systemctl is-active` thoát 0 khi active, khác 0 (3: inactive,
// 4: không có unit) khi không. Mọi *ExitError là câu trả lời "không".
func unitActive(ctx context.Context, sys system.System, unit string) (bool, error) {
	_, err := sys.Run(ctx, system.Cmd{Name: "systemctl", Args: []string{"is-active", unit}})
	if err == nil {
		return true, nil
	}
	if _, ok := isExit(err); ok {
		return false, nil
	}
	return false, fmt.Errorf("hỏi systemd trạng thái %s: %w", unit, err)
}

func systemctl(verb, unit string) system.Cmd {
	return system.Cmd{Name: "systemctl", Args: []string{verb, unit}}
}

// ============================================================== ServiceRestart

// ServiceRestart kiểm cấu hình (Test) rồi khởi động lại dịch vụ, ví dụ sau khi
// nâng gói mà reload không đủ (file chạy mới chỉ được nạp khi khởi động lại).
type ServiceRestart struct {
	Unit         string
	Test         *system.Cmd
	OnlyIfActive bool

	// restarted: Apply của chính bước này đã xong. Không có trạng thái nào trên
	// máy cho biết "đã khởi động lại sau lần nâng", nên Step tự nhớ.
	restarted bool
}

func (s *ServiceRestart) Info() engine.StepInfo {
	restart := systemctl("restart", s.Unit)
	return engine.StepInfo{
		ID:    "service.restart:" + s.Unit,
		Title: i18n.T("Khởi động lại "+s.Unit, "Restart "+s.Unit),
		Why: i18n.T(
			"Dịch vụ đang chạy vẫn dùng bản cũ trong bộ nhớ cho tới khi được khởi động lại; không làm bước này thì bản vá chưa có tác dụng. Cấu hình được kiểm trước, sai thì không khởi động lại.",
			"A running service keeps using the old version in memory until it is restarted; without this step the fix does not take effect. The configuration is checked first and the service is not restarted if the check fails.",
		),
		Commands: cmdStrings(s.Test, &restart),
		Danger:   true,
		NoUndo: i18n.T(
			"Khởi động lại không đổi cấu hình nào, không có trạng thái cũ để trả về.",
			"A restart changes no configuration, so there is no earlier state to return to.",
		),
	}
}

func (s *ServiceRestart) Check(ctx context.Context, sys system.System) (bool, error) {
	if err := checkUnit(s.Unit); err != nil {
		return false, err
	}
	if s.restarted {
		return true, nil
	}
	if !s.OnlyIfActive {
		return false, nil
	}
	// Dịch vụ đang tắt (bản PHP chưa có site nào) thì không có gì để nạp lại;
	// khởi động nó lên là đổi thứ người vận hành đã chọn.
	active, err := unitActive(ctx, sys, s.Unit)
	return !active, err
}

func (s *ServiceRestart) Apply(ctx context.Context, sys system.System) error {
	if err := checkUnit(s.Unit); err != nil {
		return err
	}
	if err := runTest(ctx, sys, s.Test, "nâng gói của "+s.Unit); err != nil {
		return err
	}
	if res, err := sys.Run(ctx, systemctl("restart", s.Unit)); err != nil {
		return withStdout(fmt.Errorf("khởi động lại %s: %w", s.Unit, err), res)
	}
	s.restarted = true
	return nil
}

func (s *ServiceRestart) Undo(context.Context, system.System) error { return nil }

// ================================================================= ServiceStop

// ServiceStop dừng một dịch vụ, ví dụ MariaDB trước khi đổi kho và nâng dòng.
type ServiceStop struct {
	Unit string

	wasActive, applied bool
}

func (s *ServiceStop) Info() engine.StepInfo {
	return engine.StepInfo{
		ID:    "service.stop:" + s.Unit,
		Title: i18n.T("Dừng "+s.Unit, "Stop "+s.Unit),
		Why: i18n.T(
			"Bước sau thay file chạy hoặc dữ liệu của dịch vụ này; làm khi nó đang chạy thì dữ liệu có thể hỏng giữa chừng. Trong lúc dừng, mọi site dùng nó không phục vụ được.",
			"The next steps replace this service's program or data; doing that while it runs can corrupt data halfway. While it is stopped, every site that uses it is down.",
		),
		Commands: []string{systemctl("stop", s.Unit).String()},
		Danger:   true,
	}
}

func (s *ServiceStop) Check(ctx context.Context, sys system.System) (bool, error) {
	if err := checkUnit(s.Unit); err != nil {
		return false, err
	}
	active, err := unitActive(ctx, sys, s.Unit)
	return !active, err
}

func (s *ServiceStop) Apply(ctx context.Context, sys system.System) error {
	if err := checkUnit(s.Unit); err != nil {
		return err
	}
	active, err := unitActive(ctx, sys, s.Unit)
	if err != nil {
		return err
	}
	if res, err := sys.Run(ctx, systemctl("stop", s.Unit)); err != nil {
		err = withStdout(fmt.Errorf("dừng %s: %w", s.Unit, err), res)
		// Stop lỗi có thể để dịch vụ nửa chừng: thử bật lại cho site chạy tiếp.
		if active {
			if r, rerr := sys.Run(ctx, systemctl("start", s.Unit)); rerr != nil {
				return errors.Join(err, withStdout(fmt.Errorf("bật lại %s: %w", s.Unit, rerr), r))
			}
		}
		return err
	}
	s.wasActive, s.applied = active, true
	return nil
}

func (s *ServiceStop) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("dừng %s: %w", s.Unit, errNotApplied)
	}
	if !s.wasActive {
		return nil
	}
	if res, err := sys.Run(ctx, systemctl("start", s.Unit)); err != nil {
		return withStdout(fmt.Errorf("bật lại %s: %w", s.Unit, err), res)
	}
	return nil
}

// ================================================================= UserInGroup

// UserInGroup thêm User vào nhóm phụ Group, ví dụ user "nginx" của gói
// nginx.org vào "www-data" để đọc được thư mục site (2750, nhóm www-data).
type UserInGroup struct {
	User, Group string

	applied, added bool
}

// sysName là tên user hoặc nhóm Linux theo NAME_REGEX mặc định của Ubuntu
// (adduser): bắt đầu bằng chữ thường hoặc "_", tối đa 32 ký tự. Không bắt đầu
// bằng "-" nên không bao giờ bị usermod, gpasswd đọc thành tuỳ chọn.
var sysName = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

func (s *UserInGroup) validate() error {
	if !sysName.MatchString(s.User) {
		return fmt.Errorf("tên user %q trống hoặc không hợp lệ", s.User)
	}
	if !sysName.MatchString(s.Group) {
		return fmt.Errorf("tên nhóm %q trống hoặc không hợp lệ", s.Group)
	}
	return nil
}

func (s *UserInGroup) usermod() system.Cmd {
	return system.Cmd{Name: "usermod", Args: []string{"-aG", s.Group, s.User}}
}

func (s *UserInGroup) Info() engine.StepInfo {
	return engine.StepInfo{
		ID:    "user.group:" + s.User + ":" + s.Group,
		Title: i18n.T("Thêm user "+s.User+" vào nhóm "+s.Group, "Add user "+s.User+" to group "+s.Group),
		Why: i18n.T(
			"Thư mục site chỉ cho nhóm "+s.Group+" đọc. Tiến trình chạy dưới "+s.User+" mà không trong nhóm này thì không đọc được file của site và trả 403. Dịch vụ chạy dưới user này cần khởi động lại để nhận nhóm mới.",
			"Site directories are readable only by the "+s.Group+" group. A process running as "+s.User+" outside this group cannot read the site's files and returns 403. Services running as this user must be restarted to pick up the new group.",
		),
		Commands: []string{s.usermod().String()},
	}
}

// inGroup đọc nhóm của user bằng `id -nG`. User chưa có (id thoát 1) là
// "chưa trong nhóm", không phải lỗi: lúc xem trước, user nginx chỉ có sau
// bước cài gói nằm trước bước này.
func (s *UserInGroup) inGroup(ctx context.Context, sys system.System) (bool, error) {
	res, err := sys.Run(ctx, system.Cmd{Name: "id", Args: []string{"-nG", s.User}})
	if err != nil {
		if _, ok := isExit(err); ok {
			return false, nil
		}
		return false, fmt.Errorf("đọc nhóm của user %s: %w", s.User, err)
	}
	return slices.Contains(strings.Fields(res.Stdout), s.Group), nil
}

func (s *UserInGroup) Check(ctx context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	return s.inGroup(ctx, sys)
}

func (s *UserInGroup) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	in, err := s.inGroup(ctx, sys)
	if err != nil {
		return err
	}
	if !in {
		if res, err := sys.Run(ctx, s.usermod()); err != nil {
			return withStdout(fmt.Errorf("thêm user %s vào nhóm %s: %w", s.User, s.Group, err), res)
		}
		cageRebindIdentity(ctx, sys) // usermod thay /etc/group: gắn lại bản riêng của các lồng
	}
	// Undo không bỏ user ra khỏi nhóm nó vốn đã ở.
	s.applied, s.added = true, !in
	return nil
}

func (s *UserInGroup) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("thêm %s vào nhóm %s: %w", s.User, s.Group, errNotApplied)
	}
	if !s.added {
		return nil
	}
	if res, err := sys.Run(ctx, system.Cmd{Name: "gpasswd", Args: []string{"-d", s.User, s.Group}}); err != nil {
		return withStdout(fmt.Errorf("bỏ user %s khỏi nhóm %s: %w", s.User, s.Group, err), res)
	}
	cageRebindIdentity(ctx, sys)
	return nil
}

// ================================================================= MariaDBDump

// mariadbBackupDir: mọi file xuất của MariaDBDump nằm dưới đây (mục 5).
const mariadbBackupDir = "/var/lib/tpan/backups"

// dumpDone là đầu dòng cuối mariadb-dump ghi khi xuất trọn vẹn ("-- Dump
// completed on <ngày giờ>"). Xuất bị ngắt giữa chừng thì không có dòng này.
const dumpDone = "-- Dump completed"

// MariaDBDump xuất toàn bộ DB ra một file trước khi nâng dòng MariaDB: đó là
// đường lùi duy nhất (mục 6.11).
//
// M9: DB khác rỗng thì chỉ xuất DB đó (`mariadb-dump --single-transaction
// --quick <DB>`), dùng cho xuất DB của site (mục 6.9) và bản xuất trước khi
// nhập hay khôi phục; Path khi đó được nằm dưới site.BackupRoot hoặc
// site.RestoreRoot. DB rỗng: như M12.
type MariaDBDump struct {
	Path string
	DB   string
}

func (s *MariaDBDump) validate() error {
	if err := checkPath("file xuất MariaDB", s.Path); err != nil {
		return err
	}
	if s.DB == "" {
		if !strings.HasPrefix(s.Path, mariadbBackupDir+"/") {
			return fmt.Errorf("file xuất MariaDB %q không nằm dưới %s", s.Path, mariadbBackupDir)
		}
		return nil
	}
	// Tên DB đi thẳng vào argv: dbNameRe không cho ký tự đầu là "-", nên nó
	// không bao giờ bị mariadb-dump đọc thành tuỳ chọn.
	if !dbNameRe.MatchString(s.DB) {
		return fmt.Errorf("tên DB %q có ký tự không cho phép", s.DB)
	}
	if !strings.HasPrefix(s.Path, site.BackupRoot+"/") && !strings.HasPrefix(s.Path, site.RestoreRoot+"/") {
		return fmt.Errorf("file xuất DB %s %q không nằm dưới %s hoặc %s", s.DB, s.Path, site.BackupRoot, site.RestoreRoot)
	}
	return nil
}

func (s *MariaDBDump) cmd() system.Cmd {
	if s.DB != "" {
		// Một DB, không --databases: file không có CREATE DATABASE hay USE, nên
		// nhập lại được vào đúng DB của site (DBImport chọn DB bằng --database).
		// --single-transaction: bản xuất nhất quán mà không khoá bảng InnoDB
		// của site đang chạy; --quick: đọc từng dòng, không giữ cả bảng trong RAM.
		return system.Cmd{Name: "mariadb-dump", Args: []string{
			"--single-transaction", "--quick", "--result-file=" + s.Path, s.DB,
		}}
	}
	return system.Cmd{Name: "mariadb-dump", Args: []string{
		"--all-databases", "--single-transaction", "--routines", "--events", "--triggers",
		"--result-file=" + s.Path,
	}}
}

// tail đọc dòng cuối thay vì ReadFile: file xuất có thể vài GB, còn ReadFile
// đọc cả file vào bộ nhớ.
func (s *MariaDBDump) tail() system.Cmd {
	return system.Cmd{Name: "tail", Args: []string{"-n", "1", "--", s.Path}}
}

func (s *MariaDBDump) Info() engine.StepInfo {
	if s.DB != "" {
		return engine.StepInfo{
			ID:    "mariadb.dump:" + s.Path,
			Title: i18n.T("Xuất DB "+s.DB+" ra "+s.Path, "Dump database "+s.DB+" to "+s.Path),
			Why: i18n.T(
				"Bản xuất này là đường lùi của DB: nhập lại nó là DB về đúng lúc xuất. Bỏ bước này thì thao tác tiếp theo (nhập, khôi phục) ghi đè DB mà không còn bản cũ để trả về. File chỉ root đọc được vì có toàn bộ dữ liệu của site.",
				"This dump is the way back for the database: importing it returns the database to this moment. Without it the next operation (import, restore) overwrites the database with no old copy to go back to. Only root can read the file because it holds all of the site's data.",
			),
			Commands: []string{dbShow(measureDBSQL(s.DB)), s.cmd().String(), s.tail().String()},
			NoUndo: i18n.T(
				"File xuất được giữ lại vì nó là đường lùi (hoặc chính là bản người dùng muốn xuất).",
				"The dump is kept because it is the way back (or the very export the user asked for).",
			),
		}
	}
	return engine.StepInfo{
		ID:    "mariadb.dump:" + s.Path,
		Title: i18n.T("Xuất toàn bộ MariaDB ra "+s.Path, "Dump all MariaDB databases to "+s.Path),
		Why: i18n.T(
			"Nâng dòng MariaDB là một chiều: dữ liệu đã nâng thì bản cũ không đọc lại được. File xuất này là đường lùi duy nhất: cài lại dòng cũ rồi nhập lại nó. File chỉ root đọc được vì có cả mật khẩu băm của user DB.",
			"Upgrading the MariaDB series is one-way: the old version cannot read upgraded data. This dump is the only way back: reinstall the old series and import it. Only root can read the file because it contains the database users' password hashes.",
		),
		Commands: []string{s.cmd().String(), s.tail().String()},
		NoUndo: i18n.T(
			"File xuất được giữ lại vì nó là đường lùi của bản nâng cấp.",
			"The dump is kept because it is the way back from the upgrade.",
		),
	}
}

// complete: file có, là file thường, dòng cuối là dòng xuất xong.
func (s *MariaDBDump) complete(ctx context.Context, sys system.System) (bool, error) {
	fi, err := sys.Stat(s.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("kiểm %s: %w", s.Path, err)
	}
	if !fi.Mode.IsRegular() {
		return false, fmt.Errorf("%s không phải file thường (%v)", s.Path, fi.Mode.Type())
	}
	res, err := sys.Run(ctx, s.tail())
	if err != nil {
		return false, withStdout(fmt.Errorf("đọc dòng cuối của %s: %w", s.Path, err), res)
	}
	return strings.HasPrefix(res.Stdout, dumpDone), nil
}

func (s *MariaDBDump) Check(ctx context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	return s.complete(ctx, sys)
}

func (s *MariaDBDump) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	dir := path.Dir(s.Path)
	if s.DB != "" {
		// Chặn đầy đĩa lại lúc Apply (mục 6.5, 6.9): core đã đo lúc dựng Plan,
		// nhưng Plan chờ duyệt có thể chạy sau đó hàng giờ.
		if err := dumpSpaceOK(ctx, sys, s.DB, dir); err != nil {
			return err
		}
	}
	// Thư mục cha của backups (/var/lib/tpan) giữ 0755: bên trong còn acme mà
	// web server phải đi qua được. Chỉ backups trở xuống là 0700.
	if err := sys.MkdirAll(path.Dir(mariadbBackupDir), 0o755, system.Root); err != nil {
		return fmt.Errorf("tạo thư mục %s: %w", path.Dir(mariadbBackupDir), err)
	}
	if err := sys.MkdirAll(dir, 0o700, system.Root); err != nil {
		return fmt.Errorf("tạo thư mục %s: %w", dir, err)
	}
	return s.write(ctx, sys)
}

// write xuất ra s.Path khi thư mục cha đã có. BackupArchive gọi thẳng hàm này
// (thư mục tạm của nó đã có, và nó đã chặn đầy đĩa cho cả bản backup).
func (s *MariaDBDump) write(ctx context.Context, sys system.System) error {
	// Tạo file rỗng 0600 trước: mariadb-dump mở file có sẵn bằng "w" nên giữ
	// quyền này, không có lúc nào file chứa dữ liệu mà quyền rộng hơn.
	if err := sys.WriteFileAtomic(s.Path, nil, 0o600, system.Root); err != nil {
		return fmt.Errorf("tạo %s: %w", s.Path, err)
	}
	// fail gỡ file dở: để lại thì người đọc tưởng đó là bản xuất dùng được.
	fail := func(err error) error {
		if rerr := sys.Remove(s.Path); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
			return errors.Join(err, fmt.Errorf("gỡ file xuất dở %s: %w", s.Path, rerr))
		}
		return err
	}
	if res, err := sys.Run(ctx, s.cmd()); err != nil {
		return fail(withStdout(fmt.Errorf("xuất MariaDB ra %s: %w", s.Path, err), res))
	}
	if err := setPermOwner(sys, s.Path, 0o600, system.Root); err != nil {
		return fail(err)
	}
	ok, err := s.complete(ctx, sys)
	if err != nil {
		return fail(err)
	}
	if !ok {
		return fail(fmt.Errorf("file xuất %s không kết thúc bằng dòng %q: bản xuất không trọn vẹn", s.Path, dumpDone))
	}
	return nil
}

func (s *MariaDBDump) Undo(context.Context, system.System) error { return nil }

// =========================================================== MariaDBUpgradeRun

// mariadbUpgradeInfo: mariadb-upgrade ghi bản server vào đây khi chạy xong
// (ví dụ "11.4.4-MariaDB"), và chính nó đọc file này để biết đã nâng chưa.
const mariadbUpgradeInfo = "/var/lib/mysql/mariadb_upgrade_info"

// MariaDBUpgradeRun chạy mariadb-upgrade sau khi nâng gói: nó sửa bảng hệ
// thống (mysql.*) cho khớp bản server mới.
type MariaDBUpgradeRun struct{}

var mariadbVersionCmd = system.Cmd{Name: "mariadb", Args: []string{"-N", "-B", "-e", "SELECT VERSION()"}}

var mariadbUpgradeCmd = system.Cmd{Name: "mariadb-upgrade"}

func (s *MariaDBUpgradeRun) Info() engine.StepInfo {
	return engine.StepInfo{
		ID:    "mariadb.upgrade:localhost",
		Title: i18n.T("Chạy mariadb-upgrade", "Run mariadb-upgrade"),
		Why: i18n.T(
			"Bản server mới cần bảng hệ thống theo dạng mới. Không chạy bước này thì một số lệnh quản lý user và quyền lỗi, log đầy cảnh báo, và lần nâng sau dễ hỏng.",
			"The new server version needs its system tables in the new format. Without this step some user and privilege commands fail, the log fills with warnings, and the next upgrade is more likely to break.",
		),
		Commands: []string{mariadbVersionCmd.String(), mariadbUpgradeCmd.String()},
		NoUndo: i18n.T(
			"Bảng hệ thống đã nâng không hạ lại được. Đường lùi là file xuất ở bước trước.",
			"Upgraded system tables cannot be downgraded. The way back is the dump from the earlier step.",
		),
	}
}

// Check so phần số của bản trong mariadb_upgrade_info với bản server đang chạy
// (phần trước dấu "-"). So bằng nhau chứ không so tiền tố: "11.4.10" bắt đầu
// bằng "11.4.1" mà không phải cùng bản.
func (s *MariaDBUpgradeRun) Check(ctx context.Context, sys system.System) (bool, error) {
	data, err := sys.ReadFile(mariadbUpgradeInfo)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("đọc %s: %w", mariadbUpgradeInfo, err)
	}
	res, err := sys.Run(ctx, mariadbVersionCmd)
	if err != nil {
		return false, withStdout(fmt.Errorf("hỏi bản MariaDB đang chạy: %w", err), res)
	}
	running, _, _ := strings.Cut(strings.TrimSpace(res.Stdout), "-")
	done, _, _ := strings.Cut(strings.TrimSpace(string(data)), "-")
	if running == "" {
		return false, fmt.Errorf("hỏi bản MariaDB đang chạy: kết quả rỗng")
	}
	return running == done, nil
}

func (s *MariaDBUpgradeRun) Apply(ctx context.Context, sys system.System) error {
	if res, err := sys.Run(ctx, mariadbUpgradeCmd); err != nil {
		return withStdout(fmt.Errorf("chạy mariadb-upgrade: %w", err), res)
	}
	return nil
}

func (s *MariaDBUpgradeRun) Undo(context.Context, system.System) error { return nil }
