package system

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/doxuta/tpan/internal/sitefs"
)

// ErrRootZoneCommand: một lệnh chạy bằng root (hoặc bằng user không phải user
// site) được đưa một đường dẫn trong vùng site, mà lệnh đó không nằm trong
// các ngoại lệ ở rootZoneCommands. Đây là lỗi lập trình trong tpan, không
// phải cổng từ chối: lệnh không được chạy. Thiết kế: M28-thiet-ke.md, 3.4.
var ErrRootZoneCommand = errors.New("lệnh chạy bằng root nhận đường dẫn trong thư mục site (mục 3.4 của M28-thiet-ke.md): dùng system.System, hoặc chạy bằng user của site")

// ErrZoneStream: OpenRead hay CreateFile (M18: đọc ghi theo dòng cho file vài
// GB của tpan, như bản sao lưu) được đưa một đường dẫn trong vùng site. Hai
// hàm đó mở theo đường dẫn và chỉ chặn symlink ở tên cuối, nên trong thư mục
// site chúng mở lại đúng lỗ T1 (thư mục cha bị tráo thành symlink). Lỗi lập
// trình trong tpan, như ErrRootZoneCommand: file trong thư mục site đi qua
// ReadFile, WriteFileAtomic (cửa sitefs), không qua hai hàm này.
var ErrZoneStream = errors.New("đọc ghi theo dòng không làm trên đường dẫn trong thư mục site (mục 3.3 của M28-thiet-ke.md): dùng ReadFile, WriteFileAtomic qua cửa sitefs")

// siteUserRe là dạng của site.Layout.User. Lệnh chạy bằng một user như vậy bị
// kernel giới hạn trong quyền của user đó, nên không cần kiểm.
// www-data, root hay user nào khác thì kiểm như root.
var siteUserRe = regexp.MustCompile(`^tp_[a-z0-9_]{1,29}$`)

// rootZoneCommands là các chương trình duy nhất root được chạy trên đường dẫn
// site, mỗi cái kèm dạng tham số được phép. Dù vậy, mọi đường dẫn site trong
// argv phải là một neo hoặc <anchor>/<một tên> (xem checkRootZone): đường dẫn
// sâu hơn có thành phần giữa do site điều khiển, mà kernel sẽ đi theo trước
// khi chương trình kịp mở nó bằng fd.
//
// Danh sách trắng theo từng cờ, không theo tên chương trình (soát cuối M28,
// C5): miễn "tar" hay "du" nói chung thì lọt các cờ nhận một FILE mà chương
// trình mở theo đường dẫn, đi theo symlink ở tên cuối (tar --file, -T,
// --exclude-from, --index-file, -I/--use-compress-program, -F; du
// --files0-from, -X, -L; useradd -P). Mọi cờ ngoài đúng dạng tpan đang dùng
// làm cả lệnh bị từ chối khi lệnh chạm vùng site.
var rootZoneCommands = map[string]func(args []string, prefix string) bool{
	// Backup: tar --create mở cây bằng fd với O_NOFOLLOW và lưu symlink thành
	// symlink (N3). Giải nén vào một cây do site nắm giữ là kiểu tấn công
	// symlink kinh điển, nên mọi dạng giải nén hay dereference đều bị từ chối.
	"tar": tarCreateOnly,
	// Dung lượng đĩa: du -sb --one-file-system không đi theo symlink và chỉ
	// in ra số (N3). Chỉ đúng dạng đó.
	"du": func(args []string, _ string) bool {
		for _, a := range args {
			switch {
			case a == "-sb", a == "-s", a == "-b", a == "--one-file-system", a == "--":
			case strings.HasPrefix(a, "-"):
				return false
			}
		}
		return true
	},
	// User site: --home-dir <anchor> chỉ là một chuỗi ghi vào /etc/passwd,
	// miễn là có --no-create-home. Chỉ đúng các cờ tpan dùng; giá trị
	// của --shell không được nằm trong vùng.
	"useradd": func(args []string, prefix string) bool {
		for i := 0; i < len(args); i++ {
			switch a := args[i]; a {
			case "--system", "--user-group", "--no-create-home":
			case "--home-dir":
				// Tuyệt đối, sạch, và trong vùng thì phải đúng là một neo
				// (checkRootZone còn cho cả <neo>/<một tên>, rộng hơn mức cần;
				// soát cuối M28, C12).
				if i+1 >= len(args) {
					return false
				}
				v := args[i+1]
				if !path.IsAbs(v) || path.Clean(v) != v || (strings.HasPrefix(v, prefix) && !sitefs.IsAnchor(strings.TrimSuffix(prefix, "/"), v)) {
					return false
				}
				i++
			case "--shell":
				if i+1 >= len(args) || strings.Contains(args[i+1], prefix) {
					return false
				}
				i++
			default:
				if strings.HasPrefix(a, "-") {
					return false
				}
			}
		}
		return slices.Contains(args, "--no-create-home")
	},
}

// tarCreateOnly: --create, và không gì giải nén hay đi theo symlink.
// GNU tar nhận tiền tố không mơ hồ của cờ dài (--ext, --deref) và cụm cờ
// ngắn (-xzf), nên kiểm cả hai.
//
// Cũng bị từ chối, vì checkRootZone không theo dõi được thư mục làm việc của
// tar: tham số đầu không có "-" (kiểu cũ "tar h ..." mà nó là một cụm cờ),
// và "C" nằm trong cụm cờ ("-zC <dir>"); chỉ hiểu "-C <dir>" đứng riêng
// hoặc dạng --directory.
//
// Chỉ nhận đúng các cờ tpan dùng cho backup (soát cuối C5): --create, --gzip,
// --one-file-system, --file=<ngoài vùng> (tar ghi file nén theo đường dẫn, đi
// theo symlink ở tên cuối, nên không bao giờ được nằm trong vùng),
// --directory=<thư mục> và -C <thư mục> (checkRootZone đòi thư mục trong vùng
// phải đúng là một neo). Cờ khác, kể cả dạng viết tắt, cụm cờ ngắn và
// --directory tách giá trị, đều bị từ chối.
func tarCreateOnly(args []string, prefix string) bool {
	if len(args) == 0 || !strings.HasPrefix(args[0], "-") {
		return false
	}
	create := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--create":
			create = true
		case a == "--gzip", a == "--one-file-system":
		case strings.HasPrefix(a, "--file="):
			// Tuyệt đối, sạch, ngoài vùng (soát cuối M28, C12): giá trị tương
			// đối được tar ghép với thư mục làm việc nên vẫn trỏ được vào vùng
			// site mà không chứa chuỗi "/var/www/".
			if v := strings.TrimPrefix(a, "--file="); !path.IsAbs(v) || path.Clean(v) != v || strings.HasPrefix(v+"/", prefix) {
				return false
			}
		case strings.HasPrefix(a, "--directory="):
		case a == "-C":
			i++ // giá trị kiểm ở checkRootZone
		case strings.HasPrefix(a, "-"):
			return false
		}
	}
	return create
}

// tarDirOption: a là --directory, hoặc tiền tố không mơ hồ của nó (--dir),
// có hoặc không có "=<dir>".
func tarDirOption(a string) (val string, hasVal, ok bool) {
	name, val, hasVal := strings.Cut(a, "=")
	return val, hasVal, len(name) > 3 && strings.HasPrefix("--directory", name)
}

// checkRootZone áp dụng mục 3.4 cho cmd với thư mục gốc site root. Một lệnh
// chạm vùng khi Name, Dir, một phần tử Args hoặc một giá trị Env chứa
// "<root>/" ở bất kỳ đâu, hoặc một phần tử Args không bắt đầu bằng "-" là
// đường dẫn tương đối mà khi nối với Dir ("" là "/") nằm dưới root. Báo nhầm
// chỉ làm test đỏ; không bao giờ mở ra lỗ hổng.
func checkRootZone(root string, cmd Cmd) error {
	if root == "" || siteUserRe.MatchString(cmd.AsUser) {
		return nil
	}
	root = path.Clean(root)
	prefix := root + "/"
	dir := cmd.Dir
	if dir == "" {
		dir = "/"
	}
	touches := strings.Contains(cmd.Name, prefix) || strings.Contains(cmd.Dir, prefix)
	for _, kv := range cmd.Env {
		touches = touches || strings.Contains(kv, prefix)
	}
	relUnder := func(v string) bool {
		return v != "" && !strings.HasPrefix(v, "-") && !path.IsAbs(v) && strings.HasPrefix(path.Join(dir, v), prefix)
	}
	for _, a := range cmd.Args {
		// Cả giá trị của --opt=<đường dẫn tương đối> (tar --directory=var/www/...).
		_, v, isOpt := strings.Cut(a, "=")
		touches = touches || strings.Contains(a, prefix) || relUnder(a) ||
			(strings.HasPrefix(a, "--") && isOpt && relUnder(v))
	}
	if !touches {
		return nil
	}
	refuse := fmt.Errorf("%w: %s", ErrRootZoneCommand, cmd.String())
	form, ok := rootZoneCommands[cmd.Name]
	if !ok || !form(cmd.Args, prefix) {
		return refuse
	}
	for _, kv := range cmd.Env {
		if strings.Contains(kv, prefix) {
			return refuse
		}
	}
	// Thư mục làm việc được vào bằng chdir, mà chdir cũng đi theo thành phần
	// cuối, nên trong vùng nó phải đúng là một neo.
	if strings.HasPrefix(dir+"/", prefix) && !sitefs.IsAnchor(root, dir) {
		return refuse
	}
	shallow := func(p string) bool {
		_, rel, ok := sitefs.Split(root, p)
		return sitefs.IsAnchor(root, p) || (ok && !strings.Contains(rel, "/"))
	}
	cwd := dir
	chdirNext := false
	for _, a := range cmd.Args {
		// tar -C <dir> / --directory=<dir>: các tên tương đối về sau được lấy
		// từ đó, và chính thư mục được vào bằng chdir.
		isDir := chdirNext
		chdirNext = false
		val := a
		if cmd.Name == "tar" && !isDir {
			if a == "-C" {
				chdirNext = true
				continue
			}
			if v, hasVal, ok := tarDirOption(a); ok {
				if !hasVal {
					chdirNext = true // --directory <dir>: giá trị là tham số kế tiếp
					continue
				}
				val, isDir = v, true
			}
		}
		if !isDir && strings.HasPrefix(a, "--") {
			_, val, _ = strings.Cut(a, "=")
		}
		var p string
		switch {
		case strings.Contains(val, prefix):
			// Chỉ hiểu đường dẫn tuyệt đối sạch, đứng riêng hoặc sau "--opt=";
			// -C/var/www/..., "source /var/www/..." thì không.
			if !path.IsAbs(val) || path.Clean(val) != val {
				return refuse
			}
			p = val
		case val != "" && !path.IsAbs(val) && (isDir || !strings.HasPrefix(val, "-")):
			p = path.Join(cwd, val)
			if strings.HasPrefix(p, prefix) && path.Clean(val) != val {
				return refuse
			}
		default:
			continue
		}
		inZone := strings.HasPrefix(p, prefix)
		if isDir {
			if inZone && !sitefs.IsAnchor(root, p) {
				return refuse
			}
			cwd = p
			continue
		}
		if inZone && !shallow(p) {
			return refuse
		}
	}
	return nil
}
