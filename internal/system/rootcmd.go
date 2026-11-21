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

// ErrRootZoneCommand: a command run as root (or as a user that is not a site
// user) was given a path inside the site zone, and it is not one of the
// exceptions in rootZoneCommands. This is a programming error in tpan, not a
// refusal of the gate: the command is not run. Design: M28-thiet-ke.md, 3.4.
var ErrRootZoneCommand = errors.New("lệnh chạy bằng root nhận đường dẫn trong thư mục site (mục 3.4 của M28-thiet-ke.md): dùng system.System, hoặc chạy bằng user của site")

// siteUserRe is the shape of site.Layout.User. A command run as such a user
// is limited by the kernel to that user's rights, so it is not checked.
// www-data, root or any other user is checked like root.
var siteUserRe = regexp.MustCompile(`^tp_[a-z0-9_]{1,29}$`)

// rootZoneCommands are the only programs root may run on a site path, each
// with the argument form it is allowed in. Even then every site path in argv
// must be an anchor or <anchor>/<one name> (see checkRootZone): a deeper path
// has a middle component the site controls, which the kernel would follow
// before the program could open it by fd.
//
// Danh sách trắng theo từng cờ, không theo tên chương trình (soát cuối M28,
// C5): miễn "tar" hay "du" nói chung thì lọt các cờ nhận một FILE mà chương
// trình mở theo đường dẫn, đi theo symlink ở tên cuối (tar --file, -T,
// --exclude-from, --index-file, -I/--use-compress-program, -F; du
// --files0-from, -X, -L; useradd -P). Mọi cờ ngoài đúng dạng tpan đang dùng
// làm cả lệnh bị từ chối khi lệnh chạm vùng site.
var rootZoneCommands = map[string]func(args []string, prefix string) bool{
	// Backup: tar --create opens the tree by fd with O_NOFOLLOW and stores a
	// symlink as a symlink (N3). Extracting into a tree the site holds is the
	// classic symlink attack, so any extract or dereference form is refused.
	"tar": tarCreateOnly,
	// Disk usage: du -sb --one-file-system does not follow symlinks and only
	// prints numbers (N3). Chỉ đúng dạng đó.
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
	// Site user: --home-dir <anchor> is only a string written to /etc/passwd,
	// as long as --no-create-home is there. Chỉ đúng các cờ tpan dùng; giá trị
	// của --shell không được nằm trong vùng.
	"useradd": func(args []string, prefix string) bool {
		for i := 0; i < len(args); i++ {
			switch a := args[i]; a {
			case "--system", "--user-group", "--no-create-home":
			case "--home-dir":
				i++ // giá trị kiểm ở checkRootZone (phải là neo hay ngoài vùng)
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

// tarCreateOnly: --create, and nothing that extracts or follows symlinks.
// GNU tar takes unambiguous prefixes of long options (--ext, --deref) and
// clusters of short ones (-xzf), so both are checked.
//
// Also refused, because checkRootZone could not follow tar's working
// directory: a first argument without "-" (old-style "tar h ..." where it is
// an option cluster), and "C" inside a cluster ("-zC <dir>"); only a bare
// "-C <dir>" or a --directory form is understood.
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
			if strings.Contains(a, prefix) {
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

// tarDirOption: a is --directory, or an unambiguous prefix of it (--dir),
// with or without "=<dir>".
func tarDirOption(a string) (val string, hasVal, ok bool) {
	name, val, hasVal := strings.Cut(a, "=")
	return val, hasVal, len(name) > 3 && strings.HasPrefix("--directory", name)
}

// checkRootZone applies 3.4 to cmd for the site root root. A command touches
// the zone when Name, Dir, an Args element or an Env value contains
// "<root>/" anywhere, or an Args element not starting with "-" is a relative
// path that, joined with Dir ("" is "/"), lies under root. False positives
// only make a test red; they never open a hole.
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
		// Also the value of --opt=<relative path> (tar --directory=var/www/...).
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
	// A working directory is entered with chdir, which follows the last
	// component too, so inside the zone it must be exactly an anchor.
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
		// tar -C <dir> / --directory=<dir>: later relative names are taken
		// from there, and the directory itself is entered with chdir.
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
					chdirNext = true // --directory <dir>: the value is the next argument
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
			// Only a clean absolute path, alone or after "--opt=", is
			// understood; -C/var/www/..., "source /var/www/..." are not.
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
