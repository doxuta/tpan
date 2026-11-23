package checks

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
)

// Phép kiểm của M28 đợt 2 (docs/thay-doi/M28-dot-2-thiet-ke.md, chốt ở
// tra-loi-soat.md): F4 (public của site là symlink sang site khác), D5 (health
// không thấy F4), H5 (mod_status của Apache), M1-DBCONN (user DB không có trần
// kết nối). Năm hàm xuất ra theo "Hợp đồng tên"; nối vào health và Plan là việc
// của core.

// ---- site.docroot (F4, D5) ----

// docrootLinkMax là số ký tự tối đa của đích symlink đưa vào bằng chứng. Đích
// do user của site đặt (chuỗi không tin được, S14): lọc ký tự điều khiển rồi cắt.
const docrootLinkMax = 200

// docrootCutMark đánh dấu đích đã bị cắt.
const docrootCutMark = " [cut]"

// untrustedLine lọc mọi ký tự điều khiển (cả C0, DEL và C1) thành dấu cách rồi
// cắt còn max ký tự: chuỗi đọc từ máy đi vào một dòng bằng chứng hay lời báo.
func untrustedLine(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	if r := []rune(s); len(r) > max {
		return string(r[:max]) + docrootCutMark
	}
	return s
}

// SiteDocroot kiểm thư mục public của site là thư mục thật thuộc user của site.
// Stat không đi theo symlink (cửa sitefs của M28 đợt 1), nên public là symlink
// thì thấy ngay chính link.
//
// public là symlink là ca KHÔNG hỗ trợ (S14): sau bản vá F4 web server từ chối
// phục vụ khi đích thuộc chủ khác, trên vhost cũ thì web server trả file của
// chỗ khác dưới tên miền của site; backup cũng từ chối site như vậy. Symlink
// BÊN TRONG public trỏ vào thứ của chính site thì vẫn hợp lệ, phép này không
// xét. Không có cách sửa tự động: tpan không biết chủ site định làm gì.
func SiteDocroot(l site.Layout) Check {
	b := siteBase("site.docroot", StageWordPress, l,
		i18n.T("Thư mục public của site là thư mục thật của chính site", "The site's public directory is a real directory owned by the site"))
	if g := siteGuard(b, l, true, l.Public); g != nil {
		return g
	}
	recheck := line("ls", "-ld", l.Public)
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		info, ev, missing, err := siteStat(sys, l.Public)
		if err != nil {
			return siteStatUnknown(b, l.Public, err, ev)
		}
		switch {
		case missing:
			return b.bad(Fail,
				i18n.T("Không có thư mục "+l.Public, "The directory "+l.Public+" is missing"),
				i18n.T("Mã nguồn của site đã bị xoá hoặc chuyển đi: web server không có gì để phục vụ. Khôi phục từ bản sao lưu.",
					"The site's source was deleted or moved away: the web server has nothing to serve. Restore it from a backup."),
				[]string{recheck}, ev)
		case info.Mode&fs.ModeSymlink != 0:
			// Đích chỉ vào bằng chứng, không vào Summary hay Cause (S14).
			linkEv := Evidence{Command: "readlink " + l.Public + " (tpan)"}
			if target, err := sys.Readlink(l.Public); err != nil {
				linkEv.ExitCode, linkEv.Output = -1, err.Error()
			} else {
				linkEv.Output = untrustedLine(target, docrootLinkMax)
			}
			return b.bad(Fail,
				i18n.T(l.Public+" là symlink, không phải thư mục thật (đích ở bằng chứng)", l.Public+" is a symlink, not a real directory (the target is in the evidence)"),
				i18n.T("tpan không hỗ trợ public là symlink. Vhost của bản vá F4 từ chối phục vụ khi đích thuộc chủ khác (site trả 403); vhost cũ thì trả file của chỗ khác dưới tên miền của site này, kể cả file của site khác. Backup cũng từ chối site như vậy. Đổi public lại thành thư mục thật (chép hay chuyển nội dung về đó); symlink bên trong public trỏ vào thứ của chính site thì vẫn được.",
					"tpan does not support public being a symlink. A vhost with the F4 fix refuses to serve when the target has another owner (the site answers 403); an older vhost serves files from elsewhere under this site's domain, other sites' files included. Backups refuse such a site too. Turn public back into a real directory (copy or move the content there); symlinks inside public that point at the site's own files are fine."),
				[]string{line("readlink", l.Public), recheck}, ev, linkEv)
		case !info.Mode.IsDir():
			return b.bad(Fail,
				i18n.T(l.Public+" không phải thư mục", l.Public+" is not a directory"),
				i18n.T("Có một file nằm ở chỗ của thư mục mã nguồn, nên site không chạy. Đổi lại thành thư mục thật chứa mã nguồn của site.",
					"A file sits where the source directory should be, so the site does not run. Turn it back into a real directory holding the site's source."),
				[]string{recheck}, ev)
		case orRoot(info.Owner.User) != l.User:
			return b.bad(Fail,
				i18n.T(l.Public+" thuộc "+orRoot(info.Owner.User)+", phải là "+l.User, l.Public+" is owned by "+orRoot(info.Owner.User)+", it must be "+l.User),
				i18n.T("Thư mục mã nguồn phải thuộc user của site: thuộc user khác thì PHP của site không ghi được (cập nhật, upload hỏng), và nếu đó là user của site khác thì site kia sửa được mã nguồn của site này.",
					"The source directory must belong to the site user: owned by someone else, the site's PHP cannot write to it (updates and uploads break), and if it is another site's user, that site can change this site's source."),
				[]string{line("chown", l.User+":"+site.WebGroup, l.Public), recheck}, ev)
		}
		return b.pass(i18n.T(l.Public+" là thư mục thật của "+l.User, l.Public+" is a real directory owned by "+l.User), ev)
	})
}

// ---- web.docroot_guard (F4) ----

// DocrootGuard kiểm vhost của site có dấu của bản vá F4: web server không đi
// theo chính public khi nó là symlink sang chủ khác (S5 đến S7).
//
//   - nginx: có dòng templates.NginxSymlinkGuardV2, và mọi dòng disable_symlinks
//     đang có hiệu lực đều đúng bằng dòng đó (dòng cũ có from= không xét chính
//     public; "disable_symlinks off;" trong một location mở lại lỗ).
//   - apache: MỌI <VirtualHost> của file (cả khối cổng 80 của bản SSL) có khối
//     <Directory "<neo>"> mà dòng Options cuối cùng là đúng
//     "Options SymLinksIfOwnerMatch" (khối sau đè khối trước, như Apache gộp).
//
// Khác VhostHardened (nằm trong kiểm chứng của mọi Plan chạm site và nhận cả
// dòng cũ), phép này chỉ dùng ở health và kiểm chứng của harden. Chỉ dùng cho
// site đang chạy: vhost tạm khoá không trỏ vào public và không có các dòng này.
func DocrootGuard(web string, l site.Layout) Check {
	b := siteBase("web.docroot_guard", StageWeb, l,
		i18n.T("Vhost không đi theo public khi nó là symlink sang chủ khác", "The vhost does not follow public when it is a symlink to another owner"))
	var conf, pattern string
	var hole i18n.Text
	switch web {
	case "nginx":
		conf, pattern = l.NginxConf, "disable_symlinks"
		hole = i18n.T("nginx không xét chính thư mục public, nên site đổi public thành symlink sang thư mục của site khác là nginx trả file của site kia (dump SQL, backup, .git) dưới tên miền của site này",
			"nginx does not check the public directory itself, so a site that turns public into a symlink to another site's directory gets that site's files (SQL dumps, backups, .git) served under its own domain")
	case "apache":
		conf, pattern = l.ApacheConf, "VirtualHost|Directory|Options"
		hole = i18n.T("Apache xét thành phần public theo Options của /var/www (FollowSymLinks của Ubuntu), nên site đổi public thành symlink sang thư mục của site khác là Apache trả file của site kia dưới tên miền của site này",
			"Apache checks the public component against the Options of /var/www (Ubuntu's FollowSymLinks), so a site that turns public into a symlink to another site's directory gets that site's files served under its own domain")
	default:
		return checkFunc(func(context.Context, system.System) Result {
			return b.unknown(i18n.T("web server phải là nginx hoặc apache (nhận được \""+web+"\")", "web server must be nginx or apache (got \""+web+"\")"))
		})
	}
	if g := siteGuard(b, l, false, conf, l.Dir); g != nil {
		return g
	}
	patternRE := regexp.MustCompile(pattern)
	recheck := line("grep", "-nE", pattern, conf)

	return checkFunc(func(ctx context.Context, sys system.System) Result {
		ev := Evidence{Command: recheck + " (tpan)"}
		data, err := sys.ReadFile(conf)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			ev.ExitCode, ev.Output = 2, "No such file or directory"
			return b.unknown(i18n.T("không có file vhost "+conf+" (phép kiểm vhost báo việc này)", "the vhost file "+conf+" is missing (the vhost check reports that)"), ev)
		case err != nil:
			ev.ExitCode, ev.Output = -1, err.Error()
			return b.unknown(i18n.T("không đọc được "+conf+" ("+err.Error()+")", "could not read "+conf+" ("+err.Error()+")"), ev)
		}
		var shown []string
		var active []vhostLine
		for i, ln := range strings.Split(string(data), "\n") {
			ln = strings.TrimRight(ln, "\r")
			if patternRE.MatchString(ln) {
				shown = append(shown, strconv.Itoa(i+1)+":"+ln)
			}
			if f := strings.Fields(ln); len(f) > 0 && !strings.HasPrefix(f[0], "#") {
				active = append(active, vhostLine{no: i + 1, f: f})
			}
		}
		ev.Output = clip(strings.Join(shown, "\n"))
		if len(shown) == 0 {
			ev.ExitCode = 1
		}

		var gaps []i18n.Text
		if web == "nginx" {
			gaps = nginxGuardGaps(active)
		} else {
			gaps = apacheAnchorGaps(active, l.Dir)
		}
		if len(gaps) == 0 {
			return b.pass(i18n.T("Vhost có dấu của bản vá F4: không đi theo public khi nó là symlink sang chủ khác",
				"The vhost carries the F4 fix: it does not follow public when it is a symlink to another owner"), ev)
		}
		var vi, en []string
		for _, g := range gaps {
			vi, en = append(vi, g.Vi), append(en, g.En)
		}
		return b.bad(Fail,
			i18n.T("Vhost chưa có bản vá F4: "+strings.Join(vi, "; "), "The vhost lacks the F4 fix: "+strings.Join(en, "; ")),
			i18n.T("Vhost do bản tpan trước M28 đợt 2 ghi hoặc đã bị sửa. Lỗ còn mở: "+hole.Vi+". `"+m14Harden+"` (hoặc `tpan health --fix` cho riêng site này) ghi lại vhost theo template mới.",
				"The vhost was written by a tpan older than M28 part 2 or was edited. Still open: "+hole.En+". `"+m14Harden+"` (or `tpan health --fix` for this site alone) rewrites the vhost from the current template."),
			[]string{m14Harden, recheck}, ev)
	})
}

// vhostLine là một dòng không phải chú thích của vhost, đã tách từ.
type vhostLine struct {
	no int
	f  []string
}

func (v vhostLine) text() string { return strings.Join(v.f, " ") }

// nginxGuardGaps: có ít nhất một dòng NginxSymlinkGuardV2 và không có dòng
// disable_symlinks nào khác.
func nginxGuardGaps(active []vhostLine) []i18n.Text {
	var gaps []i18n.Text
	n := 0
	for _, v := range active {
		switch {
		case v.f[0] != "disable_symlinks":
		case v.text() == templates.NginxSymlinkGuardV2:
			n++
		default:
			s := strconv.Itoa(v.no) + ": \"" + untrustedLine(v.text(), 120) + "\""
			gaps = append(gaps, i18n.T("dòng "+s+" khác \""+templates.NginxSymlinkGuardV2+"\"", "line "+s+" differs from \""+templates.NginxSymlinkGuardV2+"\""))
		}
	}
	if n == 0 {
		gaps = append(gaps, i18n.T("thiếu dòng \""+templates.NginxSymlinkGuardV2+"\"", "the line \""+templates.NginxSymlinkGuardV2+"\" is missing"))
	}
	return gaps
}

// apacheAnchorGaps: mỗi <VirtualHost> phải có khối <Directory "<dir>"> (có
// nháy hay không, tên thẻ không phân biệt hoa thường, so cả đường dẫn sau
// path.Clean, không so tiền tố) mà dòng Options cuối cùng là đúng
// "Options SymLinksIfOwnerMatch". Khối sau ghi đè khối trước như Apache gộp
// các khối <Directory> cùng đường dẫn theo thứ tự trong file.
func apacheAnchorGaps(active []vhostLine, dir string) []i18n.Text {
	var gaps []i18n.Text
	vhosts := 0
	var name string
	inVhost, inAnchor := false, false
	ok, blockOpts := false, ""
	closeVhost := func() {
		if !ok {
			gaps = append(gaps, i18n.T(name+" thiếu khối <Directory \""+dir+"\"> với Options SymLinksIfOwnerMatch",
				name+" lacks a <Directory \""+dir+"\"> block with Options SymLinksIfOwnerMatch"))
		}
		inVhost, inAnchor = false, false
	}
	for _, v := range active {
		tag := strings.ToLower(v.f[0])
		switch {
		case strings.HasPrefix(tag, "<virtualhost"):
			if inVhost {
				closeVhost()
			}
			vhosts++
			addr := strings.TrimSpace(strings.TrimSuffix(strings.Join(v.f[1:], " "), ">"))
			name = "<VirtualHost " + untrustedLine(addr, 60) + "> (" + strconv.Itoa(v.no) + ")"
			inVhost, ok = true, false
		case tag == "</virtualhost>":
			if inVhost {
				closeVhost()
			}
		case !inVhost:
		case tag == "<directory":
			arg := strings.TrimSpace(strings.TrimSuffix(strings.Join(v.f[1:], " "), ">"))
			arg = strings.Trim(arg, `"`)
			inAnchor = path.IsAbs(arg) && path.Clean(arg) == dir
			blockOpts = ""
		case tag == "</directory>":
			if inAnchor && blockOpts != "" {
				ok = blockOpts == "options symlinksifownermatch"
			}
			inAnchor = false
		case inAnchor && tag == "options":
			blockOpts = strings.ToLower(v.text())
		}
	}
	if inVhost {
		closeVhost()
	}
	if vhosts == 0 {
		gaps = append(gaps, i18n.T("file không có khối <VirtualHost> nào", "the file has no <VirtualHost> block"))
	}
	return gaps
}

// ---- php.pool (F4 phía PHP, S8) ----

// poolShowRE chọn các dòng của file pool đưa vào bằng chứng (như grep -nE).
const poolShowRE = `^[[:space:]]*(\[|listen|user|group)`

// poolSection là một khối [tên] của file pool, khoá đã đọc (dòng sau đè dòng trước).
type poolSection struct {
	name string
	kv   map[string]string
}

// parsePool đọc file pool theo cú pháp INI của PHP-FPM: chú thích bắt đầu
// bằng dấu chấm phẩy, giá trị có thể nằm trong nháy kép, sau giá trị không
// nháy có thể có chú thích. Dòng trước khối đầu tiên bỏ qua.
func parsePool(data []byte) []poolSection {
	var out []poolSection
	for _, raw := range strings.Split(string(data), "\n") {
		ln := strings.TrimSpace(raw)
		switch {
		case ln == "" || strings.HasPrefix(ln, ";"):
		case strings.HasPrefix(ln, "[") && strings.HasSuffix(ln, "]"):
			out = append(out, poolSection{name: ln[1 : len(ln)-1], kv: map[string]string{}})
		case len(out) > 0:
			if k, v, ok := strings.Cut(ln, "="); ok {
				out[len(out)-1].kv[strings.TrimSpace(k)] = iniValue(v)
			}
		}
	}
	return out
}

func iniValue(v string) string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, `"`) {
		if end := strings.IndexByte(v[1:], '"'); end >= 0 {
			return v[1 : 1+end]
		}
	}
	v, _, _ = strings.Cut(v, ";")
	return strings.TrimSpace(v)
}

// PHPPool kiểm file pool của site (tpan-<slug>.conf) ở mọi bản PHP trong
// versions (các bản đang cài; bản không có file pool của site thì bỏ qua). Mọi
// khối [...] của file phải có:
//
//   - listen đúng socket của site cho chính bản PHP đó (không phải host:port:
//     pool nghe TCP thì PHP của mọi site gửi được request thẳng vào PHP của
//     site này, tức chạy mã dưới user của nó);
//   - user và group là user của site (template: group = user);
//   - listen.owner, listen.group là www-data, listen.mode 0660;
//   - không có listen.acl_users, listen.acl_groups (PHP-FPM bỏ qua
//     listen.owner, listen.group khi có ACL).
//
// File pool nằm ngoài vùng site, do root đọc. Chỉ dùng ở health (S8): không
// đưa vào Isolated, để một pool sửa tay không chặn thao tác trên site khác.
func PHPPool(l site.Layout, versions []string) Check {
	b := siteBase("php.pool", StagePHP, l,
		i18n.T("Pool PHP-FPM của site nghe đúng socket riêng và chạy dưới user của site", "The site's PHP-FPM pool listens on its own socket and runs as the site user"))
	unknown := func(why i18n.Text) Check {
		return checkFunc(func(context.Context, system.System) Result { return b.unknown(why) })
	}
	if g := siteGuard(b, l, true); g != nil {
		return g
	}
	if !sitePoolRE.MatchString(l.Pool) {
		return unknown(i18n.T("tên pool không hợp lệ (\""+l.Pool+"\")", "the pool name is not valid (\""+l.Pool+"\")"))
	}
	if len(versions) == 0 {
		return unknown(i18n.T("chưa biết bản PHP nào đang cài", "no installed PHP version is known"))
	}
	var vers []string
	for _, v := range versions {
		if !phpVersionRE.MatchString(v) {
			return unknown(i18n.T("bản PHP không hợp lệ (\""+v+"\")", "the PHP version is not valid (\""+v+"\")"))
		}
		if !containsStr(vers, v) {
			vers = append(vers, v)
		}
	}
	showRE := regexp.MustCompile(poolShowRE)
	fix := line("sudo", "tpan", "health", "--fix")

	return checkFunc(func(ctx context.Context, sys system.System) Result {
		var evs []Evidence
		var vi, en, rechecks []string
		bad := func(where, sVi, sEn string) {
			vi, en = append(vi, where+": "+sVi), append(en, where+": "+sEn)
		}
		for _, ver := range vers {
			conf := l.PoolConf(ver)
			grep := line("grep", "-nE", poolShowRE, conf)
			ev := Evidence{Command: grep + " (tpan)"}
			data, err := sys.ReadFile(conf)
			switch {
			case errors.Is(err, fs.ErrNotExist):
				ev.ExitCode, ev.Output = 2, "No such file or directory: PHP "+ver+" has no pool for this site, skipped"
				evs = append(evs, ev)
				continue
			case err != nil:
				ev.ExitCode, ev.Output = -1, err.Error()
				return b.unknown(i18n.T("không đọc được "+conf+" ("+err.Error()+")", "could not read "+conf+" ("+err.Error()+")"), append(evs, ev)...)
			}
			var shown []string
			for i, ln := range strings.Split(string(data), "\n") {
				if showRE.MatchString(ln) {
					shown = append(shown, strconv.Itoa(i+1)+":"+strings.TrimRight(ln, "\r"))
				}
			}
			ev.Output = clip(strings.Join(shown, "\n"))
			evs = append(evs, ev)
			rechecks = append(rechecks, grep)

			secs := parsePool(data)
			if len(secs) == 0 {
				bad(conf, "không có khối pool nào", "there is no pool section")
			}
			sock := l.SocketFor(ver)
			for _, s := range secs {
				where := "[" + untrustedLine(s.name, 60) + "] " + conf
				listen := s.kv["listen"]
				switch {
				case listen == sock:
				case listen != "" && !strings.HasPrefix(listen, "/"):
					bad(where, "listen là địa chỉ TCP \""+untrustedLine(listen, 80)+"\", phải là socket "+sock,
						"listen is the TCP address \""+untrustedLine(listen, 80)+"\", it must be the socket "+sock)
				default:
					bad(where, "listen là \""+untrustedLine(listen, 80)+"\", phải là "+sock,
						"listen is \""+untrustedLine(listen, 80)+"\", it must be "+sock)
				}
				for _, w := range [][2]string{
					{"user", l.User}, {"group", l.User},
					{"listen.owner", "www-data"}, {"listen.group", site.WebGroup},
				} {
					if got := s.kv[w[0]]; got != w[1] {
						bad(where, w[0]+" là \""+untrustedLine(got, 80)+"\", phải là "+w[1],
							w[0]+" is \""+untrustedLine(got, 80)+"\", it must be "+w[1])
					}
				}
				if m := s.kv["listen.mode"]; strings.TrimLeft(m, "0") != "660" {
					bad(where, "listen.mode là \""+untrustedLine(m, 20)+"\", phải là 0660", "listen.mode is \""+untrustedLine(m, 20)+"\", it must be 0660")
				}
				for _, k := range []string{"listen.acl_users", "listen.acl_groups"} {
					if _, has := s.kv[k]; has {
						bad(where, "có "+k, k+" is set")
					}
				}
			}
		}
		if len(rechecks) == 0 {
			return b.unknown(i18n.T("không có file pool của site ở bản PHP nào ("+strings.Join(vers, ", ")+")",
				"the site has no pool file for any PHP version ("+strings.Join(vers, ", ")+")"), evs...)
		}
		if len(vi) == 0 {
			return b.pass(i18n.T(fmt.Sprintf("Pool của site (%d file) nghe đúng socket riêng, chạy dưới %s", len(rechecks), l.User),
				fmt.Sprintf("The site pool (%d file(s)) listens on its own socket and runs as %s", len(rechecks), l.User)), evs...)
		}
		return b.bad(Fail,
			i18n.T("Pool PHP-FPM của site sai: "+strings.Join(vi, "; "), "The site's PHP-FPM pool is wrong: "+strings.Join(en, "; ")),
			i18n.T("File pool bị sửa tay. listen là địa chỉ TCP thì PHP của mọi site trên máy gửi được request thẳng vào PHP của site này (chạy mã dưới user của nó); user hay group khác thì PHP của site chạy dưới user khác, mất cô lập file; listen.owner, listen.group, listen.mode hay listen.acl_* khác thì user khác ghi được vào socket, hoặc web server không mở được nó (lỗi 502). `tpan health --fix` ghi lại pool theo template.",
				"The pool file was edited by hand. With a TCP listen address any site's PHP on this machine can send requests straight to this site's PHP (run code as its user); with another user or group the site's PHP runs as someone else and file isolation is lost; with other listen.owner, listen.group, listen.mode or listen.acl_* values other users can write to the socket, or the web server cannot open it (error 502). `tpan health --fix` rewrites the pool from the template."),
			append([]string{fix}, rechecks...), evs...)
	})
}

// ---- web.mod_status (H5) ----

// ApacheModStatus kiểm mod_status của Apache đang tắt, bằng `apachectl -M`
// (danh sách module Apache đã nạp sau khi đọc cấu hình; S15: a2query hụt một
// trường hợp). Chỉ dùng khi stack là Apache, ở health và kiểm chứng của
// harden, không ở kiểm tra stack của `tpan update`.
func ApacheModStatus() Check {
	b := base{id: "web.mod_status:apache2", stage: StageWeb,
		title: i18n.T("mod_status của Apache đang tắt", "Apache mod_status is off")}
	cmd := system.Cmd{Name: "apachectl", Args: []string{"-M"}}
	recheck := cmd.String()
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		res, ev, err := execCmd(ctx, sys, cmd)
		if err != nil {
			return b.cantRun(err, ev)
		}
		if res.ExitCode != 0 {
			return b.unknown(i18n.T("apachectl -M báo lỗi (mã "+strconv.Itoa(res.ExitCode)+": "+firstLineWith(ev.Output, "AH", "rror")+")",
				"apachectl -M failed (exit "+strconv.Itoa(res.ExitCode)+": "+firstLineWith(ev.Output, "AH", "rror")+")"), ev)
		}
		if !strings.Contains(res.Stdout, "Loaded Modules") {
			return b.unknown(i18n.T("output của apachectl -M không có danh sách module", "the apachectl -M output has no module list"), ev)
		}
		for _, ln := range strings.Split(res.Stdout, "\n") {
			if f := strings.Fields(ln); len(f) > 0 && f[0] == "status_module" {
				return b.bad(Fail,
					i18n.T("mod_status của Apache đang bật", "Apache mod_status is on"),
					i18n.T("Ubuntu bật mod_status sẵn, với /server-status chỉ mở cho máy cục bộ (Require local). PHP của mọi site chạy trên chính máy này nên gọi được 127.0.0.1/server-status và thấy request của mọi site, kể cả URL kèm tham số (token đặt lại mật khẩu, khoá API). tpan không dùng trang này. `"+m14Harden+"` hay `tpan health --fix` tắt nó (a2dismod -q status rồi reload Apache). Đã chạy mà vẫn bật thì module được nạp bằng một dòng LoadModule ngoài mods-enabled: tìm bằng `apachectl -t -D DUMP_INCLUDES`, xoá hay ghi chú dòng `LoadModule status_module` đó rồi reload Apache.",
						"Ubuntu enables mod_status by default, with /server-status open to the local machine only (Require local). Every site's PHP runs on this machine, so it can call 127.0.0.1/server-status and see every site's requests, URLs with their parameters included (password reset tokens, API keys). tpan does not use this page. `"+m14Harden+"` or `tpan health --fix` turns it off (a2dismod -q status, then an Apache reload). If it is still on after that, the module is loaded by a LoadModule line outside mods-enabled: find it with `apachectl -t -D DUMP_INCLUDES`, delete or comment out that `LoadModule status_module` line, then reload Apache."),
					[]string{m14Harden, line("apachectl", "-t", "-D", "DUMP_INCLUDES"), recheck}, ev)
			}
		}
		return b.pass(i18n.T("mod_status của Apache đang tắt", "Apache mod_status is off"), ev)
	})
}

// ---- db.user_limit (M1-DBCONN) ----

// dbUserRE là dạng tên user DB, chép đúng dbNameRe của steps (site_db.go):
// checks không import steps. Tên đi thẳng vào câu SQL nên phải chắc chỉ có
// [a-z0-9_].
var dbUserRE = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)

// DBUserLimit đọc MAX_USER_CONNECTIONS của user DB user@localhost và
// max_connections của MariaDB bằng client mariadb của root (unix_socket). Câu
// SQL dựng từ tên đã qua dbUserRE, đi qua stdin, không chạm mật khẩu.
//
// Đạt khi 0 < trần ≤ max_connections (giữ trần admin đã nâng, S16). Cảnh báo
// khi trần là 0 (không có trần: một site bị chiếm mở hết kết nối của máy),
// khi trần lớn hơn max_connections (trần không có tác dụng), và khi user không
// có (phép db.access báo chuyện đó; ở đây chỉ nói không kiểm được trần).
func DBUserLimit(user string) Check {
	b := base{id: "db.user_limit:" + untrustedLine(user, 80), stage: StageDB,
		title: i18n.T("User DB có trần số kết nối cùng lúc", "The DB user has a concurrent connection limit")}
	if !dbUserRE.MatchString(user) {
		return checkFunc(func(context.Context, system.System) Result {
			return b.unknown(i18n.T("tên user DB không hợp lệ (chỉ a-z, 0-9, _ và tối đa 64 ký tự)", "the DB user name is not valid (a-z, 0-9 and _ only, at most 64 characters)"))
		})
	}
	sql := "SELECT 'limit', max_user_connections FROM mysql.user WHERE User='" + user + "' AND Host='localhost'; " +
		"SELECT 'max', @@GLOBAL.max_connections;"
	cmd := system.Cmd{Name: "mariadb", Args: []string{"-N", "-B"}, Stdin: []byte(sql + "\n")}
	// SQL không có $, ` hay nháy kép nên đặt trong nháy kép an toàn (như MariaDBSecure).
	recheck := `mariadb -N -B -e "` + sql + `"`
	want := strconv.Itoa(site.DBMaxUserConnections)
	alter := `mariadb -e "ALTER USER '` + user + `'@'localhost' WITH MAX_USER_CONNECTIONS ` + want + `;"`

	return checkFunc(func(ctx context.Context, sys system.System) Result {
		res, ev, err := execCmd(ctx, sys, cmd)
		if err != nil {
			return b.cantRun(err, ev)
		}
		if res.ExitCode != 0 {
			return b.unknown(i18n.T("mariadb báo lỗi (mã "+strconv.Itoa(res.ExitCode)+": "+firstLineWith(ev.Output, "ERROR")+")",
				"mariadb failed (exit "+strconv.Itoa(res.ExitCode)+": "+firstLineWith(ev.Output, "ERROR")+")"), ev)
		}
		got := map[string]string{}
		for _, ln := range strings.Split(res.Stdout, "\n") {
			if k, v, ok := strings.Cut(strings.TrimSpace(ln), "\t"); ok {
				got[k] = strings.TrimSpace(v)
			}
		}
		maxConn, err := strconv.Atoi(got["max"])
		if err != nil {
			return b.unknown(i18n.T("output của mariadb thiếu max_connections", "the mariadb output has no max_connections"), ev)
		}
		raw, exists := got["limit"]
		if !exists {
			return b.bad(Warn,
				i18n.T("User DB "+user+"@localhost không có trong MariaDB", "The DB user "+user+"@localhost does not exist in MariaDB"),
				i18n.T("Không có user này thì không có trần nào để kiểm. Site dùng user này sẽ không vào được DB (phép kiểm db.access báo chi tiết).",
					"Without this user there is no limit to check. The site using it cannot reach its database (the db.access check has the details)."),
				[]string{`mariadb -e "SELECT User, Host FROM mysql.user WHERE User='` + user + `'"`, recheck}, ev)
		}
		n, err := strconv.Atoi(raw)
		if err != nil {
			return b.unknown(i18n.T("max_user_connections không phải số", "max_user_connections is not a number"), ev)
		}
		maxS := strconv.Itoa(maxConn)
		switch {
		case n == 0:
			return b.bad(Warn,
				i18n.T("User DB "+user+" không có trần số kết nối", "The DB user "+user+" has no connection limit"),
				i18n.T("PHP của site bị chiếm mở được hết "+maxS+" kết nối của MariaDB (max_connections), và mọi site khác trả lỗi 500. `"+m14Harden+"` đặt trần "+want+" cho mọi user DB của site.",
					"A compromised site's PHP can open all "+maxS+" MariaDB connections (max_connections), and every other site answers 500. `"+m14Harden+"` sets a limit of "+want+" on every site DB user."),
				[]string{m14Harden, recheck}, ev)
		case n < 0 || n > maxConn:
			return b.bad(Warn,
				i18n.T("Trần kết nối của user DB "+user+" là "+raw+", ngoài khoảng 1 đến "+maxS+" (max_connections)", "The connection limit of the DB user "+user+" is "+raw+", outside 1 to "+maxS+" (max_connections)"),
				i18n.T("Trần lớn hơn max_connections của MariaDB thì không có tác dụng: một site bị chiếm vẫn mở hết kết nối của máy. Đặt lại trần "+want+" (hoặc một số nhỏ hơn max_connections).",
					"A limit above MariaDB's max_connections has no effect: a compromised site can still open every connection of the machine. Set the limit back to "+want+" (or another number below max_connections)."),
				[]string{alter, recheck}, ev)
		}
		return b.pass(i18n.T("User DB "+user+" có trần "+raw+" kết nối (max_connections là "+maxS+")", "The DB user "+user+" is limited to "+raw+" connections (max_connections is "+maxS+")"), ev)
	})
}
