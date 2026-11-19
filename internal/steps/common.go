// Package steps giữ các Step cụ thể mà core ghép thành Plan: cài gói, bật dịch
// vụ, ghi file config, bật module Apache, siết MariaDB, dò bản PHP, tải WP-CLI.
//
// Đặc tả: mục 4.2 (Step), 6.1 (cài stack) và 11 (bảo mật) của docs/KE-HOACH.md;
// cách viết theo agent-dev/skills/tpan-step.
//
// Ba luật chung cho mọi Step trong gói:
//
//   - Step là con trỏ (&steps.File{...}): Apply ghi lại trạng thái cũ vào chính
//     Step để Undo trả về đúng như trước. Plan được dựng mới cho mỗi lần chạy nên
//     trạng thái này không lẫn giữa hai job.
//   - Apply đã đổi máy mà bước sau đó lỗi (Test, Reload, configtest...) thì
//     Apply tự trả máy về rồi mới báo lỗi. Lý do: engine coi Apply lỗi là "chưa
//     Apply" nên KHÔNG gọi Undo cho nó (xem runStep trong engine.go).
//   - Tham số sai (đường dẫn tương đối, tên gói có ký tự lạ...) thì Check và
//     Apply đều trả lỗi, không chạy lệnh nào. Tham số do core dựng, nhưng một
//     tên gói như "-o..." lọt vào argv vẫn bị apt-get đọc thành tuỳ chọn, nên
//     kiểm ở đây là lớp chặn cuối.
package steps

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/system"
)

// Mọi Step của gói phải thoả engine.Step; lỡ sai chữ ký thì không biên dịch được.
var (
	_ engine.Step = (*AptUpdate)(nil)
	_ engine.Step = (*AptInstall)(nil)
	_ engine.Step = (*Service)(nil)
	_ engine.Step = (*Dir)(nil)
	_ engine.Step = (*File)(nil)
	_ engine.Step = (*Symlink)(nil)
	_ engine.Step = (*Absent)(nil)
	_ engine.Step = (*ApacheModules)(nil)
	_ engine.Step = (*MariaDBSecure)(nil)
	_ engine.Step = (*DetectPHP)(nil)
	_ engine.Step = (*WPCLI)(nil)
)

// errNotApplied: Undo được gọi cho bước chưa Apply. Engine không làm vậy; lỗi
// này chỉ để lỡ có ai gọi sai thì không "hoàn tác" bừa bằng trạng thái rỗng.
var errNotApplied = errors.New("hoàn tác khi bước chưa được thực hiện")

// ---- Kiểm tham số ----

// debName là tên gói Debian, tên dịch vụ systemd, tên module Apache: ký tự đầu
// phải là chữ hoặc số để không bao giờ bị đọc thành tuỳ chọn ("-o", "--force").
var (
	debName  = regexp.MustCompile(`^[a-z0-9][a-z0-9.+-]*$`)
	unitName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9@._:-]*$`)
	modName  = regexp.MustCompile(`^[a-z0-9][a-z0-9_]*$`)
)

// checkPath nhận đường dẫn tuyệt đối, đã chuẩn (không "..", không "//", không
// "/" ở cuối) và không có ký tự điều khiển. Đường dẫn không chuẩn có thể trỏ ra
// ngoài chỗ người đọc tưởng (/etc/nginx/../../root/.ssh).
func checkPath(what, p string) error {
	switch {
	case p == "":
		return fmt.Errorf("%s đang trống", what)
	case !path.IsAbs(p):
		return fmt.Errorf("%s %q không phải đường dẫn tuyệt đối", what, p)
	case path.Clean(p) != p:
		return fmt.Errorf("%s %q chưa ở dạng chuẩn (có \"..\", \"//\" hoặc \"/\" ở cuối)", what, p)
	case p == "/":
		return fmt.Errorf("%s không được là thư mục gốc", what)
	case strings.ContainsFunc(p, func(r rune) bool { return r < 0x20 || r == 0x7f }):
		return fmt.Errorf("%s %q có ký tự điều khiển", what, p)
	}
	return nil
}

func checkNames(what string, re *regexp.Regexp, names []string) error {
	if len(names) == 0 {
		return fmt.Errorf("danh sách %s đang trống", what)
	}
	for _, n := range names {
		if !re.MatchString(n) {
			return fmt.Errorf("%s %q có ký tự không cho phép", what, n)
		}
	}
	return nil
}

// checkPerm chỉ nhận 9 bit quyền thường: file config không cần setuid, setgid
// hay sticky, và bit setuid trên một file do root ghi là lỗ hổng. Thư mục được
// thêm setgid qua checkDirPerm.
func checkPerm(perm fs.FileMode) error {
	if perm&^fs.ModePerm != 0 {
		return fmt.Errorf("quyền %v có bit đặc biệt (setgid, setuid, sticky) chưa được hỗ trợ", perm)
	}
	return nil
}

// checkDirPerm như checkPerm nhưng nhận thêm setgid: thư mục site là 2750
// (mục 5), để file PHP tạo ra (ảnh tải lên) mang nhóm www-data và web server
// đọc được. setuid và sticky trên thư mục vẫn chưa có chỗ dùng nên vẫn từ chối.
func checkDirPerm(perm fs.FileMode) error {
	return checkPerm(perm &^ fs.ModeSetgid)
}

// dirMode là phần quyền của thư mục mà Dir so và giữ: 9 bit thường cùng setgid.
func dirMode(m fs.FileMode) fs.FileMode { return m & (fs.ModePerm | fs.ModeSetgid) }

// normOwner hiểu Owner rỗng là root:root, đúng như system hiểu nó. Không chuẩn
// hoá thì File{Owner: {}} so với Stat (trả "root") sẽ không bao giờ khớp.
func normOwner(o system.Owner) system.Owner {
	if o.User == "" {
		o.User = "root"
	}
	if o.Group == "" {
		o.Group = "root"
	}
	return o
}

// ---- Chạy lệnh ----

// isExit cho biết err là "lệnh chạy xong với mã thoát khác 0". Với các lệnh
// hỏi trạng thái (systemctl is-active, a2query), đó là câu trả lời "không",
// còn lỗi khác (không có chương trình, ctx bị huỷ) mới là lỗi thật.
func isExit(err error) (*system.ExitError, bool) {
	var ee *system.ExitError
	ok := errors.As(err, &ee)
	return ee, ok
}

// withStdout thêm stdout của lệnh lỗi vào thông báo. ExitError đã kèm stderr,
// nhưng vài chương trình (curl, mariadb) in lý do ra stdout.
func withStdout(err error, res system.Result) error {
	if s := strings.TrimSpace(res.Stdout); s != "" {
		return fmt.Errorf("%w\n%s", err, s)
	}
	return err
}

// cmdStrings trả các lệnh khác nil dưới dạng chữ, để đưa vào StepInfo.Commands.
func cmdStrings(cmds ...*system.Cmd) []string {
	var out []string
	for _, c := range cmds {
		if c != nil {
			out = append(out, c.String())
		}
	}
	return out
}

// runTest chạy lệnh kiểm cú pháp (nginx -t, apachectl configtest). Lỗi kèm
// output của chính lệnh kiểm: đó là chỗ nói dòng config nào sai.
func runTest(ctx context.Context, sys system.System, test *system.Cmd, what string) error {
	if test == nil {
		return nil
	}
	res, err := sys.Run(ctx, *test)
	if err != nil {
		return withStdout(fmt.Errorf("kiểm cấu hình sau khi %s: %w", what, err), res)
	}
	return nil
}

// waitForTimeout, waitForEvery: trần và nhịp chờ của File.WaitFor. Biến (không
// phải hằng) để test rút ngắn.
var (
	waitForTimeout = 5 * time.Second
	waitForEvery   = 100 * time.Millisecond
)

// waitForPath chờ tới khi p có (Stat không lỗi), ctx bị huỷ, hoặc hết
// waitForTimeout. Không trả lỗi: người gọi để phép kiểm chứng phán.
func waitForPath(ctx context.Context, sys system.System, p string) {
	deadline := time.Now().Add(waitForTimeout)
	for {
		if _, err := sys.Stat(p); err == nil || time.Now().After(deadline) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(waitForEvery):
		}
	}
}

func runReload(ctx context.Context, sys system.System, reload *system.Cmd, what string) error {
	if reload == nil {
		return nil
	}
	res, err := Reload(ctx, sys, *reload)
	if err != nil {
		return withStdout(fmt.Errorf("nạp lại dịch vụ sau khi %s: %w", what, err), res)
	}
	return nil
}

// ---- Reload nginx: chờ tới khi config mới thật sự nhận kết nối ----

// NginxPIDFile là nơi nginx.service của Ubuntu ghi PID của tiến trình master.
const NginxPIDFile = "/run/nginx.pid"

// Thời hạn và nhịp hỏi khi chờ nginx áp dụng một lần reload. Là biến để test
// chạy nhanh.
var (
	reloadWaitMax  = 5 * time.Second
	reloadWaitTick = 20 * time.Millisecond
)

// Reload chạy lệnh reload. Với `systemctl reload nginx`, nó chỉ trả về khi
// mọi worker còn nhận kết nối đều thuộc thế hệ mới (đã đọc config mới).
//
// Vì sao phải chờ: `systemctl reload nginx` chỉ gửi SIGHUP cho master rồi trả
// về ngay. Master đọc config, mở worker mới, rồi chờ thêm 100 ms mới bảo worker
// cũ thôi nhận kết nối. Đo trên máy ảo (docs/bang-chung/M8-reload.log): 20/20
// lần, request đầu tiên sau khi reload trả về vẫn do worker cũ trả lời, và kết
// quả chỉ đổi sau 93 đến 102 ms. Phép kiểm HTTP chạy trong khoảng đó thấy máy
// như trước reload: vhost vừa gỡ vẫn 200 (đạt giả), vhost vừa bật vẫn 444.
//
// Apache (`apachectl graceful`) không cần chờ: đo cùng cách, lúc reload trả về
// tiến trình con cũ đã thoát và mọi trả lời đã theo config mới. Tiến trình con
// cũ của apache còn phục vụ request dở thì vẫn mang tên cũ, nên chờ nó thoát là
// chờ cả những lần tải file dài.
//
// Không đọc được PID của master (máy giả trong test, --demo, nginx chưa chạy)
// thì không chờ.
func Reload(ctx context.Context, sys system.System, cmd system.Cmd) (system.Result, error) {
	if cmd.Name != "systemctl" || !slices.Equal(cmd.Args, []string{"reload", "nginx"}) {
		return sys.Run(ctx, cmd)
	}
	master, old := nginxWorkers(ctx, sys, "")
	res, err := sys.Run(ctx, cmd)
	if err != nil || master == "" || len(old) == 0 {
		return res, err
	}
	return res, waitNginxReload(ctx, sys, master, old)
}

// nginxWorkers trả PID của master (đọc NginxPIDFile khi master rỗng) và tập
// PID các worker còn nhận kết nối: tiến trình con có tên đúng "nginx: worker
// process". Worker cũ đã nhận lệnh thôi việc đổi tên thành "nginx: worker
// process is shutting down" (nó đã đóng cổng nghe, chỉ phục vụ nốt kết nối
// đang mở); cache manager, cache loader không nhận request. Lỗi thì trả tập rỗng.
func nginxWorkers(ctx context.Context, sys system.System, master string) (string, map[string]bool) {
	if master == "" {
		b, err := sys.ReadFile(NginxPIDFile)
		if err != nil {
			return "", nil
		}
		master = strings.TrimSpace(string(b))
		if master == "" || strings.Trim(master, "0123456789") != "" {
			return "", nil
		}
	}
	res, err := sys.Run(ctx, system.Cmd{Name: "ps", Args: []string{"-o", "pid=,args=", "--ppid", master}})
	if err != nil {
		return master, nil
	}
	ws := map[string]bool{}
	for _, line := range strings.Split(res.Stdout, "\n") {
		pid, args, _ := strings.Cut(strings.TrimSpace(line), " ")
		if strings.TrimSpace(args) == "nginx: worker process" {
			ws[pid] = true
		}
	}
	return master, ws
}

// waitNginxReload chờ tới khi có worker nhận kết nối và không worker nào trong
// số đó thuộc tập old. Quá reloadWaitMax là lỗi: master chưa áp dụng config
// mới (nginx chỉ ghi lý do vào error.log, systemctl vẫn báo thành công).
func waitNginxReload(ctx context.Context, sys system.System, master string, old map[string]bool) error {
	deadline := time.Now().Add(reloadWaitMax)
	for {
		_, cur := nginxWorkers(ctx, sys, master)
		stale := false
		for p := range cur {
			stale = stale || old[p]
		}
		if len(cur) > 0 && !stale {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("nginx chưa áp dụng cấu hình mới sau %v: worker cũ vẫn nhận kết nối (xem /var/log/nginx/error.log)", reloadWaitMax)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(reloadWaitTick):
		}
	}
}

// verifyOrRestore chạy Test rồi Reload sau một thay đổi. Hỏng ở đâu thì trả
// máy về (prev.restore) trước khi báo lỗi, vì engine không Undo bước có Apply lỗi.
//
//   - Test lỗi: dịch vụ chưa nạp gì, chỉ cần trả file cũ.
//   - Reload lỗi: dịch vụ có thể đang ở trạng thái lạ, nên trả file cũ rồi
//     reload thêm lần nữa để nó chạy lại bằng config cũ (vẫn hỏng thì gộp lỗi).
func verifyOrRestore(ctx context.Context, sys system.System, prev saved, test, reload *system.Cmd, what string) error {
	if err := runTest(ctx, sys, test, what); err != nil {
		if rerr := prev.restore(sys); rerr != nil {
			return errors.Join(err, rerr)
		}
		return err
	}
	if err := runReload(ctx, sys, reload, what); err != nil {
		if rerr := prev.restore(sys); rerr != nil {
			return errors.Join(err, rerr)
		}
		if rerr := runReload(ctx, sys, reload, "trả lại "+prev.path); rerr != nil {
			return errors.Join(err, rerr)
		}
		return err
	}
	return nil
}

// ---- Giữ và trả lại một đường dẫn ----

// saved là trạng thái của một đường dẫn trước khi bước đổi nó: chưa có, là
// symlink (giữ đích), hoặc là file thường (giữ nội dung, quyền, chủ).
type saved struct {
	path   string
	exists bool
	link   string // khác rỗng: là symlink trỏ tới link
	data   []byte
	perm   fs.FileMode
	owner  system.Owner
}

// capture đọc trạng thái hiện tại của p. Thư mục và file đặc biệt (socket,
// thiết bị) bị từ chối: các bước ở đây không bao giờ được ghi đè chúng.
func capture(sys system.System, p string) (saved, error) {
	fi, err := sys.Stat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return saved{path: p}, nil
	}
	if err != nil {
		return saved{}, fmt.Errorf("đọc trạng thái %s: %w", p, err)
	}
	s := saved{path: p, exists: true, perm: fi.Mode.Perm(), owner: normOwner(fi.Owner)}
	switch {
	case fi.Mode&fs.ModeSymlink != 0:
		if s.link, err = sys.Readlink(p); err != nil {
			return saved{}, fmt.Errorf("đọc đích symlink %s: %w", p, err)
		}
	case fi.Mode.IsDir():
		return saved{}, fmt.Errorf("%s là thư mục, bước này chỉ làm việc với file hoặc symlink", p)
	case !fi.Mode.IsRegular():
		return saved{}, fmt.Errorf("%s không phải file thường (%v)", p, fi.Mode.Type())
	default:
		if s.data, err = sys.ReadFile(p); err != nil {
			return saved{}, fmt.Errorf("đọc nội dung cũ của %s: %w", p, err)
		}
	}
	return s, nil
}

// restore đưa đường dẫn về đúng như lúc capture.
func (s saved) restore(sys system.System) error {
	switch {
	case !s.exists:
		if err := sys.Remove(s.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("xoá %s để trả về như cũ: %w", s.path, err)
		}
	case s.link != "":
		if err := replaceSymlink(sys, s.link, s.path); err != nil {
			return fmt.Errorf("trả lại symlink %s: %w", s.path, err)
		}
	default:
		// WriteFileAtomic thay luôn symlink nếu lúc này ở đó là symlink.
		if err := sys.WriteFileAtomic(s.path, s.data, s.perm, s.owner); err != nil {
			return fmt.Errorf("trả lại nội dung cũ của %s: %w", s.path, err)
		}
	}
	return nil
}

// replaceSymlink đặt link trỏ tới target, thay thứ đang ở link (nếu có) một
// cách nguyên tử: tạo link tạm cùng thư mục rồi rename đè lên. Nhờ vậy không
// có lúc nào sites-enabled thiếu link nếu tpan chết giữa chừng.
//
// Tên link tạm bắt đầu bằng dấu chấm: `include sites-enabled/*` của Nginx
// (glob) bỏ qua file ẩn, nên lỡ tpan chết để sót link tạm thì nó không thành
// một vhost thứ hai làm `nginx -t` hỏng.
func replaceSymlink(sys system.System, target, link string) error {
	tmp := path.Join(path.Dir(link), "."+path.Base(link)+".tpan-tmp")
	if err := sys.Remove(tmp); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("dọn link tạm %s: %w", tmp, err)
	}
	if err := sys.Symlink(target, tmp); err != nil {
		return fmt.Errorf("tạo link tạm %s: %w", tmp, err)
	}
	if err := sys.Rename(tmp, link); err != nil {
		_ = sys.Remove(tmp) // dọn link tạm; lỗi chính là rename
		return fmt.Errorf("đặt link %s: %w", link, err)
	}
	return nil
}
