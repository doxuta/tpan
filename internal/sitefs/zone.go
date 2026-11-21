package sitefs

import (
	"path"
	"strings"
)

// Split nhận p khi p nằm hẳn bên dưới một neo <root>/<tên>: trả neo và phần
// còn lại. Chính root, chính neo, và mọi đường dẫn khác thì ok=false: chúng
// nằm trong thư mục của root, user của site không tráo được.
//
// p được path.Clean trước, nên "/var/www/a/../b/x" là đường dẫn trong neo
// /var/www/b. p không tuyệt đối thì ok=false.
func Split(root, p string) (anchor, rel string, ok bool) {
	if root == "" || !path.IsAbs(p) {
		return "", "", false
	}
	root = path.Clean(root)
	p = path.Clean(p)
	rest, found := strings.CutPrefix(p, root+"/")
	if !found || root == "/" {
		return "", "", false
	}
	name, rel, found := strings.Cut(rest, "/")
	if !found || name == "" || rel == "" {
		return "", "", false
	}
	return root + "/" + name, rel, true
}

// InZone cho biết p có nằm trong vùng site (dưới Root) không.
func InZone(p string) bool {
	_, _, ok := Split(Root, p)
	return ok
}

// IsAnchor cho biết p có đúng là một neo <root>/<tên> không.
func IsAnchor(root, p string) bool {
	if root == "" || !path.IsAbs(p) {
		return false
	}
	root = path.Clean(root)
	rest, found := strings.CutPrefix(path.Clean(p), root+"/")
	return found && root != "/" && rest != "" && !strings.Contains(rest, "/")
}
