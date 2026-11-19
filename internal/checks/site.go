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
	"unicode/utf8"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
)

// Các phép kiểm của một site xếp theo đường đi của request (mục 6.7 của
// docs/KE-HOACH.md): vhost, pool, WordPress, DB, rồi gọi HTTP thật. Hợp đồng ở
// site_types.go. Tên và đường dẫn luôn lấy từ site.Layout, không ghép lại ở đây.
//
// Phép kiểm chỉ đọc. Lệnh chạy dưới user của site dùng Cmd.AsUser (runuser).

// siteUserRE là dạng tên user Linux của site (tp_<slug>, slug chỉ có [a-z0-9_]).
// Layout dựng bằng LayoutOf thì luôn khớp; kiểm lại ở đây để một Layout dựng
// tay (hoặc đọc từ DB hỏng) không đưa một tên bắt đầu bằng "-" vào argv của runuser.
var siteUserRE = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,31}$`)

// sitePathRE là ký tự cho phép trong đường dẫn của site. Đường dẫn được nối vào
// dòng lệnh gợi ý (ManualFix) cho người gõ, nên không được có khoảng trắng, nháy,
// $, dấu huyền hay ký tự điều khiển. Layout từ LayoutOf luôn khớp (domain chỉ có [a-z0-9.-]).
var sitePathRE = regexp.MustCompile(`^[A-Za-z0-9/._-]+$`)

// siteDBRE là dạng tên DB và user DB của site (tp_<slug>), chỉ [A-Za-z0-9_]. Tên
// được đặt trong câu SQL gợi ý ở ManualFix nên phải chắc không có nháy hay ký tự lạ.
var siteDBRE = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// sitePoolRE là dạng tên pool (tpan-<slug>). Tên pool đi vào dòng lệnh gợi ý có
// dấu * để người vận hành gõ, nên phải chắc không có ký tự lạ.
var sitePoolRE = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// siteBase dựng base có ID dạng "<loại>:<domain>".
func siteBase(kind string, stage Stage, l site.Layout, title i18n.Text) base {
	return base{id: kind + ":" + l.Domain.String(), stage: stage, title: title}
}

// siteGuard trả về một phép kiểm "Unknown" khi Layout không dùng được: domain
// zero (quên ParseDomain), tên user lạ, hoặc đường dẫn cần dùng không tuyệt đối.
// Trả nil khi mọi thứ ổn. Không có lệnh nào chạy trong nhánh Unknown.
func siteGuard(b base, l site.Layout, needUser bool, paths ...string) Check {
	var why i18n.Text
	switch {
	case l.Domain.IsZero():
		why = i18n.T("thông tin site rỗng (chưa có domain)", "the site layout is empty (no domain)")
	case needUser && !siteUserRE.MatchString(l.User):
		why = i18n.T("tên user của site không hợp lệ (\""+l.User+"\")", "the site user name is not valid (\""+l.User+"\")")
	default:
		for _, p := range paths {
			if !path.IsAbs(p) || !sitePathRE.MatchString(p) {
				why = i18n.T("đường dẫn của site phải tuyệt đối và chỉ có ký tự an toàn (nhận được \""+p+"\")", "the site path must be absolute with safe characters only (got \""+p+"\")")
				break
			}
		}
	}
	if why.IsZero() {
		return nil
	}
	return checkFunc(func(context.Context, system.System) Result { return b.unknown(why) })
}

// siteDescribe mô tả loại, quyền và chủ của một mục, làm bằng chứng của phép kiểm đọc file.
func siteDescribe(info system.FileInfo) string {
	kind := "file thường"
	switch {
	case info.Mode.IsDir():
		kind = "thư mục"
	case info.Mode&fs.ModeSymlink != 0:
		kind = "symlink"
	case info.Mode&fs.ModeSocket != 0:
		kind = "socket"
	case !info.Mode.IsRegular():
		kind = "loại khác"
	}
	return fmt.Sprintf("%s, mode %04o, chủ %s:%s", kind, uint32(info.Mode.Perm()), orRoot(info.Owner.User), orRoot(info.Owner.Group))
}

// siteStat đọc thông tin một đường dẫn qua system.System và dựng bằng chứng.
// missing=true khi đường dẫn không tồn tại (Evidence.ExitCode 1); err khác nil
// khi không đọc được vì lý do khác (ExitCode -1): phép kiểm gọi phải trả Unknown.
func siteStat(sys system.System, p string) (info system.FileInfo, ev Evidence, missing bool, err error) {
	ev = Evidence{Command: "stat " + p + " (tpan)"}
	info, err = sys.Stat(p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		ev.ExitCode, ev.Output = 1, "không có / no such file or directory"
		return info, ev, true, nil
	case err != nil:
		ev.ExitCode, ev.Output = -1, err.Error()
		return info, ev, false, err
	}
	ev.Output = siteDescribe(info)
	return info, ev, false, nil
}

func siteStatUnknown(b base, p string, err error, ev ...Evidence) Result {
	return b.unknown(i18n.T("không đọc được thông tin "+p+" ("+err.Error()+")", "could not stat "+p+" ("+err.Error()+")"), ev...)
}

// siteRunAs là lệnh chạy dưới user của site, dùng cho dòng ManualFix và bằng chứng.
func siteRunAs(l site.Layout, name string, args ...string) string {
	return system.Cmd{Name: name, Args: args, AsUser: l.User}.String()
}

// ---- Web server: vhost ----

// VhostEnabled kiểm file vhost của web server có, mang dòng Header của tpan, và
// symlink ở sites-enabled trỏ đúng vào nó. Chỉ nhìn file chưa đủ để biết site có
// được phục vụ; phép kiểm HTTPSite mới gọi thật. Nhưng phép kiểm này nói được
// ngay hỏng ở đâu (thiếu file, bị sửa tay, chưa bật) mà không cần đoán từ một mã lỗi.
func VhostEnabled(web string, l site.Layout) Check {
	b := siteBase("web.vhost", StageWeb, l,
		i18n.T("Vhost của site có, do tpan quản lý và đang được bật", "The site vhost exists, is managed by tpan and is enabled"))
	var conf, link, test, reload string
	switch web {
	case "nginx":
		conf, link = l.NginxConf, l.NginxLink
		test, reload = line("nginx", "-t"), line("systemctl", "reload", "nginx")
	case "apache":
		conf, link = l.ApacheConf, l.ApacheLink
		test, reload = line("apachectl", "configtest"), line("systemctl", "reload", "apache2")
	default:
		return checkFunc(func(context.Context, system.System) Result {
			return b.unknown(i18n.T("web server phải là nginx hoặc apache (nhận được \""+web+"\")", "web server must be nginx or apache (got \""+web+"\")"))
		})
	}
	if g := siteGuard(b, l, false, conf, link); g != nil {
		return g
	}
	dom := l.Domain.String()
	recheck := line("ls", "-l", conf, link)

	return checkFunc(func(ctx context.Context, sys system.System) Result {
		info, confEv, missing, err := siteStat(sys, conf)
		if err != nil {
			return siteStatUnknown(b, conf, err, confEv)
		}
		if missing {
			return b.bad(Fail,
				i18n.T("Không có file vhost "+conf, "The vhost file "+conf+" is missing"),
				i18n.T("File vhost chưa được ghi hoặc đã bị xoá. Không có nó, web server không biết domain "+dom+" thuộc site nào, request rơi vào site mặc định.",
					"The vhost file was never written or was deleted. Without it the web server does not know which site "+dom+" belongs to, so requests fall into the default site."),
				[]string{line("tpan", "site", "add", dom), test, reload, recheck}, confEv)
		}
		if !info.Mode.IsRegular() {
			return b.bad(Fail,
				i18n.T(conf+" không phải file thường", conf+" is not a regular file"),
				i18n.T("Có thứ khác (thư mục, symlink) nằm ở chỗ của file vhost. TPAN chỉ tin một file thường do chính nó ghi.",
					"Something else (a directory, a symlink) sits where the vhost file should be. TPAN only trusts a regular file it wrote itself."),
				[]string{line("ls", "-ld", conf), line("tpan", "site", "add", dom), recheck}, confEv)
		}

		data, err := sys.ReadFile(conf)
		readEv := Evidence{Command: "head -n 1 " + conf + " (tpan)"}
		if err != nil {
			readEv.ExitCode, readEv.Output = -1, err.Error()
			return b.unknown(i18n.T("không đọc được "+conf+" ("+err.Error()+")", "could not read "+conf+" ("+err.Error()+")"), confEv, readEv)
		}
		first, _, _ := strings.Cut(string(data), "\n")
		first = strings.TrimRight(first, "\r \t")
		readEv.Output = clip(first)
		if first != templates.Header {
			return b.bad(Fail,
				i18n.T("File vhost không có dòng đầu của tpan: có thể bị sửa tay hoặc không do tpan ghi", "The vhost file lacks the tpan header line: it may have been edited by hand or not written by tpan"),
				i18n.T("Dòng đầu \""+templates.Header+"\" mất đi nghĩa là ai đó đã thay file. TPAN không biết nội dung còn đúng không, nên không tin nó.",
					"The first line \""+templates.Header+"\" is gone, so someone replaced the file. TPAN cannot tell whether the content is still right, so it does not trust it."),
				[]string{line("head", "-n", "5", conf), line("tpan", "site", "add", dom), test, reload, line("head", "-n", "1", conf)}, confEv, readEv)
		}

		linkInfo, linkEv, missing, err := siteStat(sys, link)
		if err != nil {
			return siteStatUnknown(b, link, err, confEv, readEv, linkEv)
		}
		ln := line("ln", "-s", conf, link)
		if missing {
			return b.bad(Fail,
				i18n.T("Vhost của site chưa được bật (không có "+link+")", "The site vhost is not enabled ("+link+" is missing)"),
				i18n.T("File vhost có nhưng không có symlink ở sites-enabled nên web server không đọc nó: request tới "+dom+" rơi vào site mặc định.",
					"The vhost file exists but there is no symlink in sites-enabled, so the web server does not read it: requests for "+dom+" fall into the default site."),
				[]string{ln, test, reload, recheck}, confEv, readEv, linkEv)
		}
		if linkInfo.Mode&fs.ModeSymlink == 0 {
			return b.bad(Fail,
				i18n.T(link+" không phải symlink", link+" is not a symlink"),
				i18n.T("Ở sites-enabled có một file hoặc thư mục thường thay vì symlink tới file vhost, nên bản trong sites-available có sửa cũng không có tác dụng.",
					"A regular file or directory sits in sites-enabled instead of a symlink to the vhost file, so edits to the sites-available copy have no effect."),
				[]string{line("ls", "-ld", link), line("tpan", "site", "add", dom), test, reload, recheck}, confEv, readEv, linkEv)
		}
		target, err := sys.Readlink(link)
		if err != nil {
			linkEv.ExitCode, linkEv.Output = -1, err.Error()
			return b.unknown(i18n.T("không đọc được symlink "+link+" ("+err.Error()+")", "could not read the symlink "+link+" ("+err.Error()+")"), confEv, readEv, linkEv)
		}
		linkEv.Command = "readlink " + link + " (tpan)"
		linkEv.Output = target
		resolved := target
		if !path.IsAbs(resolved) {
			resolved = path.Join(path.Dir(link), resolved) // symlink tương đối tính từ thư mục chứa nó
		}
		if path.Clean(resolved) != conf {
			return b.bad(Fail,
				i18n.T(link+" trỏ tới "+target+", không phải "+conf, link+" points to "+target+", not "+conf),
				i18n.T("Symlink bật vhost trỏ sai chỗ, nên web server đọc một file khác (hoặc không có file nào).",
					"The symlink that enables the vhost points to the wrong place, so the web server reads another file (or nothing)."),
				[]string{line("ln", "-sfn", conf, link), test, reload, recheck}, confEv, readEv, linkEv)
		}
		return b.pass(i18n.T("Vhost của site có, đúng dòng đầu của tpan, và đã được bật", "The site vhost exists, has the tpan header and is enabled"),
			confEv, readEv, linkEv)
	})
}

// ---- Pool PHP: socket ----

// PoolSocket kiểm socket của pool: tồn tại, là socket, chủ www-data:www-data,
// quyền 0660. Web server nói chuyện với PHP qua socket này; chủ hay quyền sai
// thì web server nhận "permission denied" và trả 502.
func PoolSocket(l site.Layout) Check {
	b := siteBase("php.socket", StagePHP, l,
		i18n.T("Socket của pool PHP có, đúng chủ và đúng quyền", "The PHP pool socket exists, with the right owner and mode"))
	if g := siteGuard(b, l, false, l.Socket); g != nil {
		return g
	}
	if !sitePoolRE.MatchString(l.Pool) {
		return checkFunc(func(context.Context, system.System) Result {
			return b.unknown(i18n.T("tên pool không hợp lệ (\""+l.Pool+"\")", "the pool name is not valid (\""+l.Pool+"\")"))
		})
	}
	dom := l.Domain.String()
	// Dấu * để shell mở rộng ra mọi bản PHP; không qua line() vì line() đặt nó trong nháy.
	poolGlob := "/etc/php/*/fpm/pool.d/" + l.Pool + ".conf"
	phpUnits := "php*-fpm.service" // systemctl tự mở rộng mẫu này, nên để line() đặt trong nháy là đúng
	recheck := line("ls", "-l", l.Socket)
	const wantMode = fs.FileMode(0o660)

	return checkFunc(func(ctx context.Context, sys system.System) Result {
		info, ev, missing, err := siteStat(sys, l.Socket)
		if err != nil {
			return siteStatUnknown(b, l.Socket, err, ev)
		}
		if missing {
			return b.bad(Fail,
				i18n.T("Không có socket "+l.Socket, "The socket "+l.Socket+" is missing"),
				i18n.T("Pool PHP-FPM của site chưa chạy (file pool chưa có, chưa được nạp, hoặc PHP-FPM đang dừng). Web server không có chỗ chuyển request PHP tới: lỗi 502.",
					"The site's PHP-FPM pool is not running (the pool file is missing or not loaded, or PHP-FPM is stopped). The web server has nowhere to send PHP requests: error 502."),
				[]string{"ls -l " + poolGlob, line("systemctl", "status", phpUnits, "--no-pager"), line("tpan", "site", "add", dom),
					line("systemctl", "reload", phpUnits), recheck}, ev)
		}
		if info.Mode&fs.ModeSocket == 0 {
			return b.bad(Fail,
				i18n.T(l.Socket+" tồn tại nhưng không phải socket", l.Socket+" exists but is not a socket"),
				i18n.T("Có một file thường nằm ở chỗ của socket (thường do ai đó tạo tay), PHP-FPM không bind được vào đó.",
					"A regular file sits where the socket should be (usually created by hand), so PHP-FPM cannot bind to it."),
				[]string{line("ls", "-l", l.Socket), line("systemctl", "status", phpUnits, "--no-pager"), recheck}, ev)
		}

		var vi, en []string
		if u, g := orRoot(info.Owner.User), orRoot(info.Owner.Group); u != "www-data" || g != site.WebGroup {
			vi = append(vi, "chủ là "+u+":"+g+", phải là www-data:"+site.WebGroup)
			en = append(en, "owner is "+u+":"+g+", it must be www-data:"+site.WebGroup)
		}
		if info.Mode.Perm() != wantMode {
			vi = append(vi, fmt.Sprintf("quyền là %04o, phải là 0660", uint32(info.Mode.Perm())))
			en = append(en, fmt.Sprintf("mode is %04o, it must be 0660", uint32(info.Mode.Perm())))
		}
		if len(vi) == 0 {
			return b.pass(i18n.T("Socket của pool có, chủ www-data:"+site.WebGroup+", quyền 0660", "The pool socket exists, owned by www-data:"+site.WebGroup+", mode 0660"), ev)
		}
		return b.bad(Fail,
			i18n.T("Socket của pool sai: "+strings.Join(vi, "; "), "The pool socket is wrong: "+strings.Join(en, "; ")),
			i18n.T("Pool ghi socket bằng listen.owner, listen.group, listen.mode. Sai chủ hay quyền thì www-data (web server) không mở được socket: lỗi 502.",
				"The pool creates the socket from listen.owner, listen.group and listen.mode. With the wrong owner or mode www-data (the web server) cannot open it: error 502."),
			[]string{"grep -n '^listen' " + poolGlob, line("tpan", "site", "add", dom), line("systemctl", "reload", phpUnits), recheck}, ev)
	})
}

// ---- WordPress ----

// WPChecksums chạy `wp core verify-checksums` dưới user của site. Lệnh này tải
// danh sách checksum từ api.wordpress.org rồi so với file thật, và chạy trước
// khi nạp WordPress (nên plugin hỏng không làm nó lỗi).
//
// Phân biệt hai loại mã thoát khác 0: "không khớp checksum" là Fail (mã nguồn bị
// sửa); mọi lỗi khác (không có mạng, không có wp, user không có) là Unknown vì
// chưa biết mã nguồn có nguyên vẹn không.
func WPChecksums(l site.Layout) Check {
	b := siteBase("wp.checksums", StageWordPress, l,
		i18n.T("Mã nguồn WordPress nguyên vẹn (khớp checksum)", "The WordPress source is intact (checksums match)"))
	if g := siteGuard(b, l, true, l.Public); g != nil {
		return g
	}
	cmd := system.Cmd{Name: "wp", Args: []string{"core", "verify-checksums", "--path=" + l.Public}, AsUser: l.User}
	recheck := cmd.String()
	// Tải lại đúng bản WordPress đang cài (không phải bản mới nhất, để không nâng cấp
	// ngầm), đè lên file lõi và giữ nguyên wp-content. $(...) do shell của người gõ
	// mở rộng; Public đã qua siteGuard nên chỉ có ký tự an toàn trong nháy kép.
	// Cờ: developer.wordpress.org/cli/commands/core/download/ và core/version/.
	asUser := "runuser -u " + l.User + " -- " // User khớp siteUserRE nên không cần nháy
	download := asUser + "wp core download --force --skip-content --path=" + l.Public +
		` --version="$(` + asUser + "wp core version --path=" + l.Public + `)"`

	return checkFunc(func(ctx context.Context, sys system.System) Result {
		res, ev, err := execCmd(ctx, sys, cmd)
		if err != nil {
			return b.cantRun(err, ev)
		}
		if res.ExitCode == 0 {
			return b.pass(i18n.T("Mã nguồn WordPress khớp checksum", "The WordPress source matches the checksums"), ev)
		}
		out := ev.Output
		switch {
		case strings.Contains(out, "doesn't verify against checksum"):
			n := strings.Count(out, "Warning: File")
			first := firstLineWith(out, "Warning: File")
			return b.bad(Fail,
				i18n.T(fmt.Sprintf("Mã nguồn WordPress bị đổi: %d file không khớp checksum (%s)", n, first), fmt.Sprintf("The WordPress source was changed: %d files do not match the checksums (%s)", n, first)),
				i18n.T("File lõi của WordPress bị sửa hoặc thêm vào, thường do mã độc hoặc một lần sửa tay. Cứ để vậy thì site có thể đang chạy mã lạ.",
					"WordPress core files were modified or added, usually by malware or a manual edit. Left as is, the site may be running unknown code."),
				[]string{recheck, download, recheck}, ev)
		case strings.Contains(out, "does not seem to be a WordPress installation"):
			return b.bad(Fail,
				i18n.T("Không thấy WordPress ở "+l.Public, "No WordPress installation found at "+l.Public),
				i18n.T("Thư mục public chưa có mã WordPress hoặc đã bị xoá: site sẽ trả lỗi.", "The public directory has no WordPress code or it was deleted: the site will fail."),
				[]string{line("ls", "-l", l.Public), line("tpan", "site", "add", l.Domain.String()), recheck}, ev)
		}
		return b.unknown(i18n.T("wp core verify-checksums báo lỗi, chưa rõ mã nguồn có nguyên vẹn không (mã "+strconv.Itoa(res.ExitCode)+": "+firstLineWith(out, "Error")+")",
			"wp core verify-checksums failed, so it is not known whether the source is intact (exit "+strconv.Itoa(res.ExitCode)+": "+firstLineWith(out, "Error")+")"), ev)
	})
}

// WPConfigPerm kiểm wp-config.php là file thường, quyền 0600, chủ là user của
// site. File này chứa mật khẩu DB: quyền rộng hơn 0600 thì user khác trên máy đọc được.
func WPConfigPerm(l site.Layout) Check {
	b := siteBase("wp.config", StageWordPress, l,
		i18n.T("wp-config.php có quyền 0600 và đúng chủ", "wp-config.php has mode 0600 and the right owner"))
	if g := siteGuard(b, l, true, l.WPConfig); g != nil {
		return g
	}
	recheck := line("ls", "-l", l.WPConfig)
	fixAll := []string{line("chown", l.User, l.WPConfig), line("chmod", "600", l.WPConfig), recheck}

	return checkFunc(func(ctx context.Context, sys system.System) Result {
		info, ev, missing, err := siteStat(sys, l.WPConfig)
		if err != nil {
			return siteStatUnknown(b, l.WPConfig, err, ev)
		}
		if missing {
			return b.bad(Fail,
				i18n.T("Không có "+l.WPConfig, l.WPConfig+" is missing"),
				i18n.T("WordPress chưa được cấu hình hoặc file đã bị xoá: site không kết nối được DB.",
					"WordPress was not configured or the file was deleted: the site cannot connect to the database."),
				[]string{line("ls", "-l", l.Public), line("tpan", "site", "add", l.Domain.String()), recheck}, ev)
		}
		if !info.Mode.IsRegular() {
			return b.bad(Fail,
				i18n.T(l.WPConfig+" không phải file thường", l.WPConfig+" is not a regular file"),
				i18n.T("Có symlink hoặc thư mục ở chỗ của wp-config.php; TPAN không đặt quyền qua symlink.",
					"A symlink or directory sits where wp-config.php should be; TPAN does not set permissions through symlinks."),
				[]string{line("ls", "-ld", l.WPConfig), line("tpan", "site", "add", l.Domain.String()), recheck}, ev)
		}
		var vi, en []string
		if info.Mode.Perm() != 0o600 {
			vi = append(vi, fmt.Sprintf("quyền là %04o, phải là 0600", uint32(info.Mode.Perm())))
			en = append(en, fmt.Sprintf("mode is %04o, it must be 0600", uint32(info.Mode.Perm())))
		}
		if u := orRoot(info.Owner.User); u != l.User {
			vi = append(vi, "chủ là "+u+", phải là "+l.User)
			en = append(en, "owner is "+u+", it must be "+l.User)
		}
		if len(vi) == 0 {
			return b.pass(i18n.T("wp-config.php quyền 0600, chủ "+l.User, "wp-config.php has mode 0600, owner "+l.User), ev)
		}
		return b.bad(Fail,
			i18n.T("wp-config.php sai: "+strings.Join(vi, "; "), "wp-config.php is wrong: "+strings.Join(en, "; ")),
			i18n.T("wp-config.php chứa mật khẩu DB. Quyền rộng hơn 0600, hoặc chủ khác, thì site khác trên máy đọc được mật khẩu; chủ không phải user của site thì PHP của site không đọc được.",
				"wp-config.php holds the DB password. A wider mode or another owner lets other sites on the machine read it, and if the owner is not the site user, the site's PHP cannot read it."),
			fixAll, ev)
	})
}

// Isolated kiểm cô lập giữa hai site: dưới user của from, `test -r` file
// wp-config.php của to phải bị từ chối (mã 1). Đọc được (mã 0) là lỗi nặng: một
// site bị chiếm thì đọc được mật khẩu DB của site kia.
//
// Mã 1 chỉ tin được khi không có gì ở stderr, vì runuser cũng thoát mã 1 khi user
// không tồn tại ("runuser: user ... does not exist"). Và `test -r` cũng trả 1 khi
// file không có: nên phải thấy file của site kia tồn tại trước, nếu không thì
// "bị từ chối" chỉ là "không có gì để đọc" và không chứng minh được gì.
func Isolated(from, to site.Layout) Check {
	id := "isolation:" + from.Domain.String() + ":" + to.Domain.String()
	b := base{id: id, stage: StageWordPress,
		title: i18n.T("Site "+from.Domain.String()+" không đọc được wp-config.php của site "+to.Domain.String(),
			"Site "+from.Domain.String()+" cannot read the wp-config.php of site "+to.Domain.String())}
	unknown := func(why i18n.Text) Check {
		return checkFunc(func(context.Context, system.System) Result { return b.unknown(why) })
	}
	switch {
	case from.Domain.IsZero() || to.Domain.IsZero():
		return unknown(i18n.T("thông tin của một trong hai site rỗng", "the layout of one of the two sites is empty"))
	case from.Domain == to.Domain:
		return unknown(i18n.T("hai site là một", "both sites are the same site"))
	case !siteUserRE.MatchString(from.User):
		return unknown(i18n.T("tên user của site không hợp lệ (\""+from.User+"\")", "the site user name is not valid (\""+from.User+"\")"))
	case !path.IsAbs(to.WPConfig) || !path.IsAbs(to.Dir) || !path.IsAbs(to.Public):
		return unknown(i18n.T("đường dẫn của site đích phải tuyệt đối", "the target site paths must be absolute"))
	}
	cmd := system.Cmd{Name: "test", Args: []string{"-r", to.WPConfig}, AsUser: from.User}
	recheck := cmd.String()

	return checkFunc(func(ctx context.Context, sys system.System) Result {
		_, statEv, missing, err := siteStat(sys, to.WPConfig)
		if err != nil {
			return siteStatUnknown(b, to.WPConfig, err, statEv)
		}
		if missing {
			return b.unknown(i18n.T("site "+to.Domain.String()+" chưa có wp-config.php nên không thử được", "site "+to.Domain.String()+" has no wp-config.php yet, so there is nothing to try reading"), statEv)
		}
		res, ev, err := execCmd(ctx, sys, cmd)
		if err != nil {
			return b.cantRun(err, statEv, ev)
		}
		switch {
		case res.ExitCode == 1 && strings.TrimSpace(ev.Output) == "":
			return b.pass(i18n.T("Bị từ chối như mong đợi: "+from.User+" không đọc được wp-config.php của "+to.Domain.String(),
				"Denied as expected: "+from.User+" cannot read the wp-config.php of "+to.Domain.String()), statEv, ev)
		case res.ExitCode == 0:
			return b.bad(Fail,
				i18n.T(from.User+" ĐỌC ĐƯỢC wp-config.php của "+to.Domain.String()+": hai site không còn cô lập", from.User+" CAN read the wp-config.php of "+to.Domain.String()+": the two sites are no longer isolated"),
				i18n.T("Quyền thư mục hoặc file của site kia quá rộng (thường là other có quyền đọc/duyệt, hoặc user của site này nằm trong nhóm www-data). Một site bị chiếm sẽ lấy được mật khẩu DB của site kia.",
					"The other site's directory or file mode is too open (usually 'other' can read/search, or this site's user is in the www-data group). A compromised site could take the other site's DB password."),
				[]string{line("id", from.User),
					line("ls", "-ld", to.Dir, to.Public, to.WPConfig),
					line("chmod", "2750", to.Dir, to.Public), line("chmod", "600", to.WPConfig),
					recheck}, statEv, ev)
		}
		return b.unknown(i18n.T("không thử được: lệnh test trả mã "+strconv.Itoa(res.ExitCode)+" kèm lỗi, không phải câu trả lời \"bị từ chối\"",
			"could not try: the test command exited with "+strconv.Itoa(res.ExitCode)+" and an error, which is not a clean denial"), statEv, ev)
	})
}

// ---- DB ----

// DBAccess đăng nhập DB bằng đúng thông tin trong wp-config.php (WP-CLI đọc nó,
// nên mật khẩu không đi qua TPAN) rồi hỏi `SHOW DATABASES`. Đạt khi thấy DB của
// site và không thấy DB nào khác ngoài information_schema: user DB của site chỉ
// được có quyền trên DB của nó.
func DBAccess(l site.Layout) Check {
	b := siteBase("db.access", StageDB, l,
		i18n.T("User DB của site đăng nhập được, chỉ thấy DB của site", "The site's DB user can log in and sees only the site's database"))
	if g := siteGuard(b, l, true, l.Public); g != nil {
		return g
	}
	if !siteDBRE.MatchString(l.DB) {
		return checkFunc(func(context.Context, system.System) Result {
			return b.unknown(i18n.T("tên DB của site không hợp lệ", "the site DB name is not valid"))
		})
	}
	cmd := system.Cmd{Name: "wp", Args: []string{"db", "query", "SHOW DATABASES", "--skip-column-names", "--path=" + l.Public}, AsUser: l.User}
	recheck := cmd.String()
	// Tên DB đã khớp siteDBRE nên đặt trong nháy kép là an toàn (không có $, `, nháy), và dễ đọc hơn dạng '\'' của line().
	grants := `mariadb -e "SHOW GRANTS FOR '` + l.DB + `'@'localhost'"`
	whichDB := line("grep", "-E", "DB_(NAME|USER)", l.WPConfig) // chỉ khớp tên DB và tên user, không khớp dòng DB_PASSWORD

	return checkFunc(func(ctx context.Context, sys system.System) Result {
		res, ev, err := execCmd(ctx, sys, cmd)
		if err != nil {
			return b.cantRun(err, ev)
		}
		if res.ExitCode != 0 {
			out := ev.Output
			if strings.Contains(out, "Access denied") || strings.Contains(out, "ERROR 1045") ||
				strings.Contains(out, "ERROR 1044") || strings.Contains(out, "ERROR 1049") || strings.Contains(out, "Unknown database") {
				return b.bad(Fail,
					i18n.T("User DB của site không đăng nhập được hoặc không thấy DB: "+firstLineWith(out, "ERROR", "Error"), "The site's DB user cannot log in or cannot see its database: "+firstLineWith(out, "ERROR", "Error")),
					i18n.T("Mật khẩu trong wp-config.php không khớp user DB, user DB bị khoá hoặc đã xoá, hoặc DB không còn. Site sẽ hiện \"Error establishing a database connection\".",
						"The password in wp-config.php does not match the DB user, the DB user was locked or dropped, or the database is gone. The site will show \"Error establishing a database connection\"."),
					[]string{whichDB, grants, line("tpan", "site", "add", l.Domain.String()), recheck}, ev)
			}
			return b.unknown(i18n.T("wp db query báo lỗi, chưa rõ DB có vào được không (mã "+strconv.Itoa(res.ExitCode)+": "+firstLineWith(out, "Error", "ERROR")+")",
				"wp db query failed, so it is not known whether the DB is reachable (exit "+strconv.Itoa(res.ExitCode)+": "+firstLineWith(out, "Error", "ERROR")+")"), ev)
		}

		var extra []string
		hasOwn := false
		for _, ln := range strings.Split(res.Stdout, "\n") {
			name := strings.TrimSpace(ln)
			if name == "" {
				continue
			}
			switch {
			case name == l.DB:
				hasOwn = true
			case strings.EqualFold(name, "information_schema"):
			default:
				extra = append(extra, name)
			}
		}
		if len(extra) > 0 {
			return b.bad(Fail,
				i18n.T("User DB của site thấy DB khác: "+strings.Join(extra, ", "), "The site's DB user sees other databases: "+strings.Join(extra, ", ")),
				i18n.T("User DB có quyền rộng hơn cần thiết (thường là GRANT trên *.*). Một site bị chiếm sẽ đọc được dữ liệu của site khác.",
					"The DB user has more privileges than it needs (usually a GRANT on *.*). A compromised site could read other sites' data."),
				[]string{grants, recheck}, ev)
		}
		if !hasOwn {
			return b.bad(Fail,
				i18n.T("Đăng nhập được nhưng không thấy DB "+l.DB, "Logged in, but the database "+l.DB+" is not visible"),
				i18n.T("DB của site không còn hoặc user DB không có quyền trên nó. Site sẽ hiện \"Error establishing a database connection\".",
					"The site database is gone or the DB user has no privilege on it. The site will show \"Error establishing a database connection\"."),
				[]string{grants, `mariadb -e "SHOW DATABASES LIKE '` + l.DB + `'"`, line("tpan", "site", "add", l.Domain.String()), recheck}, ev)
		}
		return b.pass(i18n.T("User DB đăng nhập được và chỉ thấy DB "+l.DB, "The DB user logs in and sees only the database "+l.DB), ev)
	})
}

// ---- Đầu cuối ----

// siteWPSigns: dấu hiệu một trang là WordPress. WordPress in "wp-content" ở
// đường dẫn theme/plugin và thẻ generator của nó nói rõ tên.
var siteWPSigns = []string{"wp-content", "wp-includes", `content="wordpress`}

// curlRecvFailure là mã thoát 56 của curl, "Failure when receiving data from the
// peer": kết nối bị ngắt giữa chừng (nginx 444 đôi khi hiện ra như vậy).
const curlRecvFailure = 56

// siteHTTPMarker đứng trước mã trạng thái curl in ra sau thân (cờ -w). Có nhãn để
// tách mã khỏi thân dù thân kết thúc bằng chữ số, và không cần xuống dòng
// (một đối số có ký tự xuống dòng khó đọc trong Evidence.Command).
const siteHTTPMarker = "TPAN_HTTP_CODE="

// siteBodyEvidence là số byte đầu của thân đưa vào Evidence. Cần đủ để thấy trang
// là gì (tiêu đề, thông báo lỗi), không cần cả trang.
const siteBodyEvidence = 1024

// HTTPSite gọi thật vào site: curl tới http://<domain>/ nhưng nối thẳng vào
// 127.0.0.1 (--resolve), nên không phụ thuộc DNS. Đạt khi mã 200, thân có dấu
// hiệu WordPress VÀ có chính domain của site (một request rơi nhầm vào vhost khác
// hay site mặc định có thể cũng trả 200 và cũng là WordPress, nhưng không có domain này).
//
// Không theo chuyển hướng (không có -L): 301 ở đây là lỗi cần thấy.
// Lưu ý cho M5: site đã có SSL sẽ chuyển 80 sang 443, phép kiểm này sẽ cần một
// biến thể cho site có SSL.
func HTTPSite(l site.Layout) Check {
	b := siteBase("http.site", StageEndToEnd, l,
		i18n.T("Gọi HTTP vào site trả 200 và đúng là WordPress của site", "An HTTP request to the site returns 200 and is the site's WordPress"))
	if g := siteGuard(b, l, true, l.Socket, l.LogDir, l.PHPErrorLog, l.Public); g != nil {
		return g
	}
	dom := l.Domain.String()
	// Domain phải đứng riêng như một tên máy, không nằm trong tên dài hơn: trang của
	// "myshop.example.com" chứa chuỗi "shop.example.com" nhưng không phải site này, và
	// "a.vn" nằm trong "ba.vn". Trước tên: không có chữ, số, '.' hay '-'. Sau tên: không có
	// chữ, số, '-', và không có ".<chữ>" (tên dài hơn như shop.example.com.evil.net).
	domRE := regexp.MustCompile(`(^|[^a-z0-9.-])` + regexp.QuoteMeta(dom) + `($|[^a-z0-9.-]|\.($|[^a-z0-9-]))`)
	url := "http://" + dom + "/"
	resolve := dom + ":80:127.0.0.1"
	cmd := system.Cmd{Name: "curl", Args: []string{"-sS", "--max-time", "15", "--resolve", resolve, "-w", siteHTTPMarker + "%{http_code}", url}}
	// Dòng kiểm lại cho người gõ: bỏ thân, chỉ in mã.
	recheck := line("curl", "-sS", "--max-time", "15", "--resolve", resolve, "-o", "/dev/null", "-w", "%{http_code}\n", url)

	return checkFunc(func(ctx context.Context, sys system.System) Result {
		res, ev, err := execCmd(ctx, sys, cmd)
		if err != nil {
			return b.cantRun(err, ev)
		}
		body, code := siteSplitCode(res.Stdout)
		ev.Output = siteHTTPEvidence(code, body, res.Stderr)

		switch {
		case res.ExitCode == curlConnectFailed || res.ExitCode == curlTimeout:
			vi, en := "không kết nối được tới 127.0.0.1 cổng 80", "cannot connect to 127.0.0.1 port 80"
			causeVi, causeEn := "Web server không chạy hoặc không nghe cổng 80.", "The web server is not running or does not listen on port 80."
			if res.ExitCode == curlTimeout {
				vi, en = "web server không trả lời trong 15 giây", "the web server did not answer within 15 seconds"
				causeVi, causeEn = "Web server hoặc PHP treo hay quá tải.", "The web server or PHP is hung or overloaded."
			}
			return b.bad(Fail, i18n.T("Lỗi: "+vi, "Failed: "+en), i18n.T(causeVi, causeEn),
				[]string{line("systemctl", "status", "nginx", "apache2", "--no-pager"), line("ss", "-tlnp"), line("tail", "-n", "20", l.LogDir+"/error.log"), recheck}, ev)
		case res.ExitCode == curlEmptyReply || res.ExitCode == curlRecvFailure:
			return b.bad(Fail,
				i18n.T("Web server đóng kết nối không trả gì cho "+dom, "The web server closed the connection without replying for "+dom),
				i18n.T("Request mang tên "+dom+" rơi vào site mặc định (nginx đóng kết nối, mã 444): vhost của site chưa được bật hoặc chưa được nạp.",
					"The request for "+dom+" fell into the default site (nginx drops the connection, code 444): the site vhost is not enabled or not loaded."),
				siteVhostFix(dom, recheck), ev)
		case res.ExitCode != 0 || code == "":
			return b.unknown(i18n.T("curl báo lỗi (mã "+strconv.Itoa(res.ExitCode)+": "+firstLineWith(res.Stderr, "curl")+")", "curl failed (exit "+strconv.Itoa(res.ExitCode)+": "+firstLineWith(res.Stderr, "curl")+")"), ev)
		}

		switch code {
		case "200":
			low := strings.ToLower(body)
			isWP := false
			for _, s := range siteWPSigns {
				if strings.Contains(low, s) {
					isWP = true
					break
				}
			}
			switch {
			case !isWP:
				return b.bad(Fail,
					i18n.T("Trả 200 nhưng trang không phải WordPress", "Answered 200 but the page is not WordPress"),
					i18n.T("Có thứ khác trả lời cho "+dom+": trang mặc định của web server, một vhost khác, hoặc WordPress chưa được cài vào public.",
						"Something else answers for "+dom+": the web server default page, another vhost, or WordPress is not installed in public."),
					[]string{line("ls", "-l", l.Public), line("curl", "-sS", "--max-time", "15", "--resolve", resolve, url), recheck}, ev)
			case !domRE.MatchString(low):
				return b.bad(Fail,
					i18n.T("Trả 200 và là WordPress nhưng trang không nhắc tới "+dom+": có thể là site khác", "Answered 200 and it is WordPress, but the page never mentions "+dom+": it may be another site"),
					i18n.T("Request tới "+dom+" có vẻ rơi nhầm vào vhost của site khác, hoặc địa chỉ site (home/siteurl) trong WordPress đang là một tên khác.",
						"The request for "+dom+" seems to have landed in another site's vhost, or the site address (home/siteurl) in WordPress is set to another name."),
					[]string{line("grep", "-Rl", dom, "/etc/nginx/sites-enabled", "/etc/apache2/sites-enabled"),
						siteRunAs(l, "wp", "option", "get", "home", "--path="+l.Public), recheck}, ev)
			}
			return b.pass(i18n.T("Site trả 200, đúng là WordPress của "+dom, "The site answers 200 and is the WordPress of "+dom), ev)
		case "502", "504", "503":
			return b.bad(Fail,
				i18n.T("Site trả "+code+": web server không nói chuyện được với PHP", "The site answered "+code+": the web server cannot talk to PHP"),
				i18n.T("Pool PHP-FPM của site không chạy, socket sai chủ hay quyền, hoặc pool quá tải (504).",
					"The site's PHP-FPM pool is not running, the socket has the wrong owner or mode, or the pool is overloaded (504)."),
				[]string{line("ls", "-l", l.Socket), line("systemctl", "status", "php*-fpm.service", "--no-pager"), line("tail", "-n", "20", l.LogDir+"/error.log"), recheck}, ev)
		case "500":
			return b.bad(Fail,
				i18n.T("Site trả 500: PHP hoặc WordPress báo lỗi", "The site answered 500: PHP or WordPress reported an error"),
				i18n.T("Thường là lỗi PHP trong plugin hay theme, hoặc WordPress không vào được DB (\"Error establishing a database connection\").",
					"Usually a PHP error in a plugin or theme, or WordPress cannot reach the DB (\"Error establishing a database connection\")."),
				[]string{line("tail", "-n", "20", l.PHPErrorLog), line("tail", "-n", "20", l.LogDir+"/error.log"),
					siteRunAs(l, "wp", "db", "check", "--path="+l.Public), recheck}, ev)
		}
		return b.bad(Fail,
			i18n.T("Site trả "+code+", đáng lẽ 200", "The site answered "+code+", it should be 200"),
			i18n.T("Request không tới được trang chủ WordPress: chuyển hướng (301/302), bị chặn (403) hoặc không tìm thấy (404). Xem vhost và địa chỉ site trong WordPress.",
				"The request does not reach the WordPress home page: a redirect (301/302), a block (403) or not found (404). Check the vhost and the site address in WordPress."),
			[]string{siteRunAs(l, "wp", "option", "get", "home", "--path="+l.Public), line("ls", "-l", l.Public), recheck}, ev)
	})
}

// siteVhostFix là các lệnh khi nghi vhost chưa được bật; dòng cuối là recheck.
func siteVhostFix(dom, recheck string) []string {
	return []string{
		line("grep", "-Rl", dom, "/etc/nginx/sites-enabled", "/etc/apache2/sites-enabled"),
		line("tpan", "site", "add", dom),
		recheck,
	}
}

// siteSplitCode tách "<thân>TPAN_HTTP_CODE=<mã>" ở cuối stdout của curl. Không
// có nhãn (curl không chạy tới cuối) thì mã rỗng và cả stdout là thân. Mã không
// phải đúng ba chữ số cũng coi như không có.
func siteSplitCode(out string) (body, code string) {
	i := strings.LastIndex(out, siteHTTPMarker)
	if i < 0 {
		return out, ""
	}
	code = strings.TrimSpace(out[i+len(siteHTTPMarker):])
	if !httpCodeRE.MatchString(code) {
		return out[:i], ""
	}
	return out[:i], code
}

// siteHTTPEvidence dựng Output của bằng chứng: mã, kích thước thân, rồi chỉ
// vài trăm byte đầu của thân, và stderr của curl nếu có. Không dùng clip (giữ
// phần CUỐI) vì với một trang HTML phần đầu mới nói trang là gì.
func siteHTTPEvidence(code, body, stderr string) string {
	if code == "" {
		code = "?"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "HTTP %s, %d byte", code, len(body))
	if body != "" {
		head := body
		cut := false
		if len(head) > siteBodyEvidence {
			n := siteBodyEvidence
			for n > 0 && !utf8.RuneStart(head[n]) { // không cắt giữa một ký tự nhiều byte
				n--
			}
			head, cut = head[:n], true
		}
		sb.WriteString("\n" + strings.TrimRight(head, "\r\n\t "))
		if cut {
			sb.WriteString("\n[... đã cắt phần sau / rest cut ...]")
		}
	}
	if s := strings.TrimSpace(stderr); s != "" {
		sb.WriteString("\n" + clip(s))
	}
	return sb.String()
}

// ---- Gom lại ----

// SiteChecks trả mọi phép kiểm của một site theo đúng thứ tự chặng của request:
// vhost, socket, mã nguồn, quyền wp-config, DB, rồi gọi HTTP.
// Isolated cần hai site nên không nằm ở đây: core thêm khi biết site còn lại.
//
// Cú pháp PHP-FPM (PHPFPMConfig) không nằm ở đây dù thuộc chặng PHP: nó là
// phép kiểm của cả máy, đã có trong phép kiểm stack. Lặp lại ở từng site thì
// cùng một ID xuất hiện N lần, bản đồ tuyến vẽ một lỗi thành N sự cố, và health
// (M4) mở đóng sự cố theo ID sẽ va nhau. phpVersion giữ cho M12, khi mỗi site
// có thể chạy một bản PHP riêng.
func SiteChecks(web, phpVersion string, l site.Layout) []Check {
	_ = phpVersion
	return []Check{
		VhostEnabled(web, l),
		PoolSocket(l),
		WPChecksums(l),
		WPConfigPerm(l),
		DBAccess(l),
		HTTPSite(l),
	}
}
