package checks

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/htaccess"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
)

// Phép kiểm của M14: cô lập sâu giữa các site và định tuyến (mục 6.13 của
// docs/KE-HOACH.md; bảng lỗ L1 đến L9 ở đầu mục đó). Hợp đồng ở m14_types.go.
// Phép kiểm cô lập giữa hai site (Isolated) nằm ở site.go.

// m14Harden là lệnh chạy Plan "Áp bản vá cô lập": cách sửa chung của các phép
// kiểm cấp máy và cấp vhost trong file này.
var m14Harden = line("sudo", "tpan", "security", "harden")

// m14NoCtl đổi ký tự điều khiển thành dấu cách. Đường dẫn file do user của site
// đặt tên (kết quả của find) có thể mang ký tự xuống dòng; chúng đi vào Summary
// và bằng chứng, nơi một dòng phải là một dòng.
func m14NoCtl(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
}

// m14BadVersion là phép kiểm "chưa kiểm được" khi bản PHP không có dạng số.số.
// Chặn sớm để tên chương trình (php-fpm<bản>) và tên dịch vụ không bị ghép từ
// một chuỗi lạ.
func m14BadVersion(b base, version string) Check {
	return checkFunc(func(context.Context, system.System) Result {
		return b.unknown(i18n.T("bản PHP phải có dạng số.số, ví dụ 8.3 (nhận được \""+version+"\")",
			"PHP version must look like 8.3 (got \""+version+"\")"))
	})
}

// ---- OPcache (lỗ L1, L2) ----

// OPcacheHardened kiểm OPcache của PHP-FPM bản version đã được siết, bằng hai
// bằng chứng độc lập:
//
//  1. file ini của tpan (templates.PHPHardeningINIPath) có và đúng nội dung
//     tpan ghi;
//  2. `php-fpm<version> -i` (PHP tự đọc mọi file ini rồi in giá trị sau khi
//     gộp) cho opcache.validate_permission là On và opcache.restrict_api trỏ
//     vào templates.OPcacheAPIDir.
//
// Cần cả hai vì file đúng chưa chắc có hiệu lực: một file ini khác đọc sau
// (tên xếp sau 99-tpan.ini) có thể đặt lại giá trị.
//
// Vì sao đáng là lỗi: mọi pool của một bản PHP-FPM dùng chung một bộ nhớ
// OPcache. Không bật validate_permission thì PHP của site này include được
// file PHP đã cache của site khác dù quyền file và open_basedir đều cấm.
func OPcacheHardened(version string) Check {
	b := base{id: "php.opcache:" + version, stage: StagePHP,
		title: i18n.T("OPcache của PHP-FPM "+version+" kiểm quyền file và khoá API", "OPcache of PHP-FPM "+version+" validates file permissions and locks its API")}
	if !phpVersionRE.MatchString(version) {
		return m14BadVersion(b, version)
	}
	ini := templates.PHPHardeningINIPath(version)
	want := templates.PHPHardeningINI()
	cmd := system.Cmd{Name: "php-fpm" + version, Args: []string{"-i"}}
	const keyPerm, keyAPI = "opcache.validate_permission", "opcache.restrict_api"
	// Dấu | là pipe của shell người gõ, nên dòng này không dựng bằng line().
	recheck := cmd.String() + ` | grep -E '^opcache\.(validate_permission|restrict_api) '`
	fix := []string{m14Harden, line("cat", ini), recheck}
	cause := i18n.T(
		"Mọi pool của một bản PHP-FPM dùng chung một bộ nhớ OPcache. Không có opcache.validate_permission=1 thì PHP của một site include được file PHP đã nằm trong cache của site khác, ví dụ wp-config.php có mật khẩu DB, dù quyền file và open_basedir đều cấm. Không có opcache.restrict_api thì site nào cũng gọi được opcache_get_status (liệt kê đường dẫn file của site khác) và opcache_reset (xoá cache của mọi site).",
		"All pools of one PHP-FPM version share a single OPcache memory. Without opcache.validate_permission=1 the PHP of one site can include a PHP file of another site that sits in the cache, for example wp-config.php with its DB password, even though file modes and open_basedir forbid it. Without opcache.restrict_api any site can call opcache_get_status (list other sites' file paths) and opcache_reset (drop every site's cache).")

	return checkFunc(func(ctx context.Context, sys system.System) Result {
		var vi, en []string

		fileEv := Evidence{Command: "cat " + ini + " (tpan)"}
		data, err := sys.ReadFile(ini)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			fileEv.ExitCode, fileEv.Output = 1, "No such file or directory"
			vi, en = append(vi, "không có file "+ini), append(en, "the file "+ini+" is missing")
		case err != nil:
			fileEv.ExitCode, fileEv.Output = -1, err.Error()
			return b.unknown(i18n.T("không đọc được "+ini+" ("+err.Error()+")", "could not read "+ini+" ("+err.Error()+")"), fileEv)
		case !bytes.Equal(data, want):
			fileEv.Output = clip("content differs from what tpan writes; active lines:\n" + iniActiveLines(data))
			vi, en = append(vi, "file "+ini+" khác nội dung tpan ghi"), append(en, "the file "+ini+" differs from what tpan writes")
		default:
			fileEv.Output = clip("content is what tpan writes; active lines:\n" + iniActiveLines(data))
		}

		res, ev, err := execCmd(ctx, sys, cmd)
		if err != nil {
			return b.cantRun(err, fileEv, ev)
		}
		if res.ExitCode != 0 {
			code, why := strconv.Itoa(res.ExitCode), sslClean(firstLineWith(joinOutput(res), "ERROR", "error"))
			return b.unknown(i18n.T(cmd.Name+" -i báo lỗi (mã "+code+": "+why+")", cmd.Name+" -i failed (exit "+code+": "+why+")"), fileEv, ev)
		}
		// php-fpm -i in cả trang phpinfo (vài trăm dòng); bằng chứng chỉ cần hai dòng đang xét.
		permLine, perm, okPerm := phpInfoValue(res.Stdout, keyPerm)
		apiLine, api, okAPI := phpInfoValue(res.Stdout, keyAPI)
		ev.Output = strings.TrimSpace(permLine + "\n" + apiLine)
		if !okPerm && !okAPI {
			ev.Output = "(no opcache.validate_permission or opcache.restrict_api line in the output)"
			if len(vi) == 0 {
				// File đúng mà PHP không in dòng nào của OPcache: OPcache không
				// được nạp, hoặc định dạng output đã đổi. Cả hai đều là "chưa
				// biết", không phải "sai".
				return b.unknown(i18n.T("output của "+cmd.Name+" -i không có dòng "+keyPerm+" hay "+keyAPI+" (OPcache không được nạp, hoặc định dạng output đã đổi)",
					"the output of "+cmd.Name+" -i has no "+keyPerm+" or "+keyAPI+" line (OPcache is not loaded, or the output format changed)"), fileEv, ev)
			}
		}
		if okPerm && !strings.EqualFold(perm, "On") && perm != "1" {
			vi, en = append(vi, keyPerm+" đang là "+sslClean(perm)+" (cần On)"), append(en, keyPerm+" is "+sslClean(perm)+" (needs On)")
		}
		if okAPI && api != templates.OPcacheAPIDir {
			vi, en = append(vi, keyAPI+" đang là \""+sslClean(api)+"\" (cần "+templates.OPcacheAPIDir+")"), append(en, keyAPI+" is \""+sslClean(api)+"\" (needs "+templates.OPcacheAPIDir+")")
		}
		if okPerm != okAPI { // chỉ thấy một trong hai dòng
			missing := keyPerm
			if okPerm {
				missing = keyAPI
			}
			vi, en = append(vi, "không thấy dòng "+missing), append(en, "no "+missing+" line")
		}
		if len(vi) > 0 {
			return b.bad(Fail,
				i18n.T("OPcache của PHP-FPM "+version+" chưa được siết: "+strings.Join(vi, "; "), "OPcache of PHP-FPM "+version+" is not hardened: "+strings.Join(en, "; ")),
				cause, fix, fileEv, ev)
		}
		return b.pass(i18n.T("OPcache của PHP-FPM "+version+" kiểm quyền file trước khi dùng cache và khoá các hàm opcache_*", "OPcache of PHP-FPM "+version+" checks file permissions before using the cache and locks the opcache_* functions"), fileEv, ev)
	})
}

// iniActiveLines trả các dòng có hiệu lực của một file ini PHP: bỏ dòng trống
// và dòng chú thích (bắt đầu bằng ";"). Dùng làm bằng chứng cho gọn.
func iniActiveLines(data []byte) string {
	var out []string
	for _, ln := range strings.Split(string(data), "\n") {
		ln = strings.TrimSpace(ln)
		if ln != "" && !strings.HasPrefix(ln, ";") {
			out = append(out, ln)
		}
	}
	if len(out) == 0 {
		return "(none)"
	}
	return strings.Join(out, "\n")
}

// phpInfoValue tìm dòng của khoá key trong output `php-fpm -i` (phpinfo dạng
// chữ): "<khoá> => <giá trị đang dùng> => <giá trị gốc>". Trả cả dòng và giá
// trị đang dùng (cột giữa); khoá chưa đặt thì PHP in "no value".
func phpInfoValue(out, key string) (ln, value string, ok bool) {
	for _, l := range strings.Split(out, "\n") {
		parts := strings.Split(strings.TrimSpace(l), " => ")
		if len(parts) >= 2 && parts[0] == key {
			return strings.TrimSpace(l), strings.TrimSpace(parts[1]), true
		}
	}
	return "", "", false
}

// ---- /proc của PHP-FPM (lỗ L7) ----

// PHPProtectProc kiểm dịch vụ php<version>-fpm chạy với ProtectProc=invisible:
// tiến trình PHP của một site (và lệnh shell nó gọi) chỉ thấy tiến trình của
// chính user mình trong /proc.
//
// Hỏi systemd (`systemctl show`) chứ không đọc file drop-in: giá trị systemd
// trả là kết quả sau khi gộp mọi drop-in, tức là thứ dịch vụ thật sự dùng.
//
// Khác "invisible" là cảnh báo, không phải lỗi: quyền file vẫn giữ, site này
// không đọc được file của site kia; thứ lộ ra là danh sách tiến trình và dòng lệnh.
func PHPProtectProc(version string) Check {
	b := base{id: "php.protectproc:" + version, stage: StagePHP,
		title: i18n.T("PHP-FPM "+version+" không thấy tiến trình của user khác", "PHP-FPM "+version+" cannot see other users' processes")}
	if !phpVersionRE.MatchString(version) {
		return m14BadVersion(b, version)
	}
	unit := "php" + version + "-fpm"
	cmd := system.Cmd{Name: "systemctl", Args: []string{"show", unit, "-p", "ProtectProc", "--value"}}
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		res, ev, err := execCmd(ctx, sys, cmd)
		if err != nil {
			return b.cantRun(err, ev)
		}
		if res.ExitCode != 0 {
			code, why := strconv.Itoa(res.ExitCode), sslClean(firstLineWith(joinOutput(res)))
			return b.unknown(i18n.T("systemctl show báo lỗi (mã "+code+": "+why+")", "systemctl show failed (exit "+code+": "+why+")"), ev)
		}
		value := strings.TrimSpace(res.Stdout)
		if value == "invisible" {
			hidden, known, procEv := ProcHidden(ctx, sys, unit)
			if known && !hidden {
				restart := line("systemctl", "restart", unit)
				return b.bad(Warn,
					i18n.T("systemd đã nạp ProtectProc=invisible cho "+unit+" nhưng tiến trình đang chạy chưa được khởi động lại", "systemd has loaded ProtectProc=invisible for "+unit+", but the running process has not been restarted"),
					i18n.T("ProtectProc chỉ có hiệu lực khi dịch vụ khởi động: tiến trình PHP-FPM đang chạy vẫn dùng /proc cũ và thấy tiến trình của mọi user. Khởi động lại dịch vụ ngắt request PHP đang chạy trong khoảng một giây.",
						"ProtectProc only takes effect when the service starts: the running PHP-FPM process still uses the old /proc and sees every user's processes. Restarting the service interrupts running PHP requests for about a second."),
					[]string{restart, cmd.String()}, ev, procEv)
			}
			return b.pass(i18n.T("Dịch vụ "+unit+" chạy với ProtectProc=invisible", "The "+unit+" service runs with ProtectProc=invisible"), ev, procEv)
		}
		shown := sslClean(value)
		if shown == "" {
			shown = "-"
		}
		return b.bad(Warn,
			i18n.T("Dịch vụ "+unit+" có ProtectProc="+shown+", cần invisible", "The "+unit+" service has ProtectProc="+shown+", it needs invisible"),
			i18n.T("PHP của một site gọi được lệnh shell, và trong /proc nó thấy tiến trình cùng dòng lệnh của mọi user khác trên máy (tên site, đường dẫn, đôi khi cả tham số nhạy cảm). Quyền file vẫn giữ nên đây là cảnh báo. Bản vá đặt một file drop-in systemd rồi khởi động lại dịch vụ PHP-FPM.",
				"The PHP of a site can run shell commands, and in /proc it sees the processes and command lines of every other user on the machine (site names, paths, sometimes sensitive arguments). File modes still hold, so this is a warning. The fix writes a systemd drop-in file and restarts the PHP-FPM service."),
			[]string{m14Harden, cmd.String()}, ev)
	})
}

// ProcHidden hỏi tiến trình chính ĐANG CHẠY của dịch vụ unit có một /proc riêng
// gắn với hidepid=invisible không. `systemctl show -p ProtectProc` chỉ nói cấu
// hình systemd đã nạp: sau daemon-reload mà chưa restart nó đã in "invisible"
// trong khi tiến trình vẫn dùng /proc cũ (đo trên máy ảo M14). Sự thật nằm ở
// /proc/<MainPID>/mountinfo của chính tiến trình đó.
//
// known=false khi không biết được (dịch vụ không chạy nên không có MainPID,
// không đọc được mountinfo): người gọi coi như không có gì để bác bỏ.
// steps.PHPProtectProc dùng chung hàm này cho Check của nó.
func ProcHidden(ctx context.Context, sys system.System, unit string) (hidden, known bool, ev Evidence) {
	res, ev, err := execCmd(ctx, sys, system.Cmd{Name: "systemctl", Args: []string{"show", unit, "-p", "MainPID", "--value"}})
	if err != nil || res.ExitCode != 0 {
		return false, false, ev
	}
	pid, err := strconv.Atoi(strings.TrimSpace(res.Stdout))
	if err != nil || pid <= 0 {
		return false, false, ev
	}
	p := "/proc/" + strconv.Itoa(pid) + "/mountinfo"
	ev = Evidence{Command: "grep ' /proc ' " + p + " (tpan)"}
	data, err := sys.ReadFile(p)
	if err != nil {
		ev.ExitCode, ev.Output = -1, err.Error()
		return false, false, ev
	}
	// mountinfo(5): trường thứ 5 là điểm gắn; tuỳ chọn của hệ file (hidepid=...)
	// nằm ở cuối dòng. Một không gian mount có thể có nhiều dòng /proc chồng
	// nhau; dòng cuối là cái tiến trình đang thấy.
	for _, ln := range strings.Split(string(data), "\n") {
		if f := strings.Fields(ln); len(f) > 4 && f[4] == "/proc" {
			ev.Output = ln
			hidden = strings.Contains(ln, "hidepid=invisible")
		}
	}
	if ev.Output == "" {
		ev.ExitCode, ev.Output = 1, "no /proc mount line"
		return false, false, ev
	}
	return hidden, true, ev
}

// ---- Quyền log của site (lỗ L6) ----

// LogPerms kiểm thư mục và hai file log do web server ghi của site: thư mục
// root:<nhóm của site> 0750, access.log và error.log root:<nhóm của site> 0640.
// Nhóm của site mang tên của user site (useradd tạo kèm), nên so với l.User.
//
// Chủ là root để logrotate không phải đổi tên file trong một thư mục do user
// của site kiểm soát; nhóm là nhóm của site để site đọc được log của chính nó
// còn site khác thì không (mục 5 của docs/KE-HOACH.md).
func LogPerms(l site.Layout) Check {
	b := siteBase("log.perm", StageWeb, l,
		i18n.T("Log của site không đọc được bởi site khác", "The site logs cannot be read by other sites"))
	if g := siteGuard(b, l, true, l.LogDir, l.AccessLog, l.ErrorLog); g != nil {
		return g
	}
	grp := l.User
	items := []struct {
		p    string
		dir  bool
		perm fs.FileMode
	}{
		{l.LogDir, true, 0o750},
		{l.AccessLog, false, 0o640},
		{l.ErrorLog, false, 0o640},
	}
	recheck := line("ls", "-ld", l.LogDir, l.AccessLog, l.ErrorLog)

	return checkFunc(func(ctx context.Context, sys system.System) Result {
		var evs []Evidence
		var vi, en, fix []string
		for _, it := range items {
			info, ev, missing, err := siteStat(sys, it.p)
			evs = append(evs, ev)
			if err != nil {
				return siteStatUnknown(b, it.p, err, evs...)
			}
			mode := fmt.Sprintf("%o", uint32(it.perm))
			switch {
			case missing && it.dir:
				vi, en = append(vi, "không có thư mục "+it.p), append(en, "the directory "+it.p+" is missing")
				fix = append(fix, line("install", "-d", "-m", mode, "-o", "root", "-g", grp, it.p))
				continue
			case missing:
				vi, en = append(vi, "không có file "+it.p), append(en, "the file "+it.p+" is missing")
				// install chép /dev/null thành một file rỗng với đúng chủ và quyền trong một lệnh.
				fix = append(fix, line("install", "-m", mode, "-o", "root", "-g", grp, "/dev/null", it.p))
				continue
			case it.dir && !info.Mode.IsDir():
				vi, en = append(vi, it.p+" không phải thư mục"), append(en, it.p+" is not a directory")
				fix = append(fix, line("ls", "-ld", it.p))
				continue
			case !it.dir && !info.Mode.IsRegular():
				vi, en = append(vi, it.p+" không phải file thường"), append(en, it.p+" is not a regular file")
				fix = append(fix, line("ls", "-ld", it.p))
				continue
			}
			if u, g := orRoot(info.Owner.User), orRoot(info.Owner.Group); u != "root" || g != grp {
				vi = append(vi, it.p+" thuộc "+u+":"+g+", phải là root:"+grp)
				en = append(en, it.p+" is owned by "+u+":"+g+", it must be root:"+grp)
				fix = append(fix, line("chown", "root:"+grp, it.p))
			}
			if got := info.Mode.Perm(); got != it.perm {
				vi = append(vi, fmt.Sprintf("%s có quyền %04o, phải là %04o", it.p, uint32(got), uint32(it.perm)))
				en = append(en, fmt.Sprintf("%s has mode %04o, it must be %04o", it.p, uint32(got), uint32(it.perm)))
				fix = append(fix, line("chmod", mode, it.p))
			}
		}
		if len(vi) == 0 {
			return b.pass(i18n.T("Thư mục log là root:"+grp+" 0750, access.log và error.log là root:"+grp+" 0640",
				"The log directory is root:"+grp+" 0750, access.log and error.log are root:"+grp+" 0640"), evs...)
		}
		return b.bad(Fail,
			i18n.T("Quyền log của site sai: "+strings.Join(vi, "; "), "The site log permissions are wrong: "+strings.Join(en, "; ")),
			i18n.T("access.log và error.log có IP của khách và URL kèm tham số. Thư mục log phải là root:"+grp+" 0750 và hai file là 0640 để chỉ site này (và root) đọc được. Quyền rộng hơn thì một site khác bị chiếm đọc được log của site này; sai nhóm thì site không đọc được log của chính nó; thiếu file thì web server tự tạo lại với quyền 0644.",
				"access.log and error.log hold visitor IPs and URLs with their parameters. The log directory must be root:"+grp+" 0750 and both files 0640 so that only this site (and root) can read them. With wider modes another compromised site can read this site's logs; with the wrong group the site cannot read its own logs; a missing file is created again by the web server with mode 0644."),
			append(fix, recheck), evs...)
	})
}

// ---- Vhost có đủ dòng chặn (lỗ L3, L4, L5, L9) ----

// vhostGuardGap là một dòng chặn thiếu (hoặc một dòng không được có) trong
// vhost: what đi vào Summary, hole nói lỗ còn mở và đi vào Cause.
type vhostGuardGap struct{ what, hole i18n.Text }

// VhostHardened kiểm vhost của site mang các dòng chặn của M14. Phép kiểm lệch
// (Drift) chỉ so file với bản tpan đã ghi, nên một vhost ghi bởi bản tpan cũ
// vẫn "không lệch"; phép này mới nói được vhost thiếu dòng chặn.
//
// Chỉ xét dòng không phải chú thích, và so sau khi gom khoảng trắng (thụt lề
// của template không ảnh hưởng). Các dòng cần có lấy từ chính các hằng mà
// template dùng (templates.NginxSymlinkGuard, templates.ApacheFCGIGuards,
// htaccess.List), nên template và phép kiểm không lệch nhau được. Với nginx,
// dòng cũ templates.NginxSymlinkGuard cũng đạt (M28 đợt 2, S5 đến S7); dấu của
// bản vá F4 do DocrootGuard kiểm.
func VhostHardened(web string, l site.Layout, mode site.HtaccessMode) Check {
	b := siteBase("web.hardened", StageWeb, l,
		i18n.T("Vhost của site có đủ các dòng chặn cô lập", "The site vhost carries all the isolation guard lines"))
	unknown := func(why i18n.Text) Check {
		return checkFunc(func(context.Context, system.System) Result { return b.unknown(why) })
	}
	var conf, pattern string
	switch web {
	case "nginx":
		conf, pattern = l.NginxConf, "disable_symlinks"
	case "apache":
		conf, pattern = l.ApacheConf, "ProxyFCGISetEnvIf|AllowOverride|FallbackResource"
	default:
		return unknown(i18n.T("web server phải là nginx hoặc apache (nhận được \""+web+"\")", "web server must be nginx or apache (got \""+web+"\")"))
	}
	if g := siteGuard(b, l, false, conf); g != nil {
		return g
	}
	mode = mode.OrDefault()
	if _, err := site.ParseHtaccessMode(string(mode)); web == "apache" && err != nil {
		return unknown(i18n.T("chế độ .htaccess không hợp lệ (\""+string(mode)+"\")", "the .htaccess mode is not valid (\""+string(mode)+"\")"))
	}
	patternRE := regexp.MustCompile(pattern)
	// Lệnh kiểm lại cũng là lệnh ghi ở bằng chứng: tpan đọc file rồi lọc đúng
	// những dòng mà grep này in ra.
	recheck := line("grep", "-nE", pattern, conf)

	holeSymlink := i18n.T("nginx (chạy dưới www-data) đọc được mọi site qua quyền nhóm, nên một symlink trong site này trỏ sang file của site khác sẽ được trả về, kể cả mã nguồn PHP dưới một tên khác",
		"nginx (running as www-data) can read every site through the group, so a symlink in this site that points at another site's file is served, PHP source under another name included")
	holeFCGI := i18n.T(".htaccess đặt được PHP_VALUE, PHP_ADMIN_VALUE gửi sang PHP-FPM, tức là đổi được open_basedir của pool thành /",
		".htaccess can set PHP_VALUE and PHP_ADMIN_VALUE sent to PHP-FPM, that is change the pool's open_basedir to /")
	holeOverride := i18n.T(".htaccess của site dùng được mọi chỉ thị của Apache: gửi request sang socket PHP-FPM của site khác (chạy mã dưới user của site đó) và bật FollowSymLinks để đọc file của site khác",
		"the site's .htaccess may use every Apache directive: send requests to another site's PHP-FPM socket (run code as that site's user) and turn on FollowSymLinks to read another site's files")
	holeMode := i18n.T("khối AllowOverride của vhost không khớp chế độ .htaccess \""+string(mode)+"\" của site, nên .htaccess (và permalink của WordPress) không chạy như panel nói",
		"the AllowOverride block of the vhost does not match the site's .htaccess mode \""+string(mode)+"\", so .htaccess (and WordPress permalinks) do not behave as the panel says")

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
		var shown []string    // bằng chứng: như grep -n
		var active [][]string // các dòng không phải chú thích, đã tách từ
		for i, ln := range strings.Split(string(data), "\n") {
			ln = strings.TrimRight(ln, "\r")
			if patternRE.MatchString(ln) {
				shown = append(shown, strconv.Itoa(i+1)+":"+ln)
			}
			if f := strings.Fields(ln); len(f) > 0 && !strings.HasPrefix(f[0], "#") {
				active = append(active, f)
			}
		}
		ev.Output = clip(strings.Join(shown, "\n"))
		if len(shown) == 0 {
			ev.ExitCode = 1 // như grep khi không dòng nào khớp
		}
		// has: có một dòng đúng bằng want. directive: có chỉ thị name (tên chỉ thị
		// của Apache không phân biệt hoa thường) với tham số đầu arg ("" là bất kỳ).
		has := func(want string) bool {
			for _, f := range active {
				if strings.Join(f, " ") == want {
					return true
				}
			}
			return false
		}
		directive := func(name, arg string) bool {
			for _, f := range active {
				if strings.EqualFold(f[0], name) && (arg == "" || len(f) > 1 && strings.EqualFold(f[1], arg)) {
					return true
				}
			}
			return false
		}
		missing := func(want string) i18n.Text {
			return i18n.T("thiếu dòng \""+want+"\"", "the line \""+want+"\" is missing")
		}

		var gaps []vhostGuardGap
		var passed i18n.Text
		if web == "nginx" {
			// Nhận cả dòng cũ (from=$document_root, trước M28 đợt 2) lẫn dòng
			// mới: phép này nằm trong kiểm chứng của mọi Plan chạm vào site, và
			// một vhost cũ không được làm hoàn tác Plan không ghi lại vhost
			// (khôi phục backup...). Dòng mới do DocrootGuard đòi riêng.
			if !has(templates.NginxSymlinkGuard) && !has(templates.NginxSymlinkGuardV2) {
				gaps = append(gaps, vhostGuardGap{missing(templates.NginxSymlinkGuardV2), holeSymlink})
			}
			passed = i18n.T("Vhost nginx chỉ đi theo symlink khi chủ của symlink cũng là chủ của file đích", "The nginx vhost follows a symlink only when its owner also owns the target file")
		} else {
			for _, g := range templates.ApacheFCGIGuards {
				if !has(g) {
					gaps = append(gaps, vhostGuardGap{missing(g), holeFCGI})
				}
			}
			all := vhostGuardGap{i18n.T("còn dòng \"AllowOverride All\"", "the line \"AllowOverride All\" is still there"), holeOverride}
			switch mode {
			case site.HtaccessSafe:
				// Tên dài: Summary chỉ nêu tên chỉ thị, bằng chứng có cả dòng.
				if !has("AllowOverride " + htaccess.OverrideOptions()) {
					gaps = append(gaps, vhostGuardGap{missing("AllowOverride " + htaccess.OverrideOptions()), holeMode})
				}
				if !has("AllowOverrideList " + htaccess.List()) {
					gaps = append(gaps, vhostGuardGap{i18n.T("thiếu dòng AllowOverrideList với đúng danh sách chỉ thị của tpan", "the AllowOverrideList line with tpan's exact directive list is missing"), holeMode})
				}
				if directive("AllowOverride", "All") {
					gaps = append(gaps, all)
				}
			case site.HtaccessOff:
				if directive("AllowOverride", "All") {
					gaps = append(gaps, all)
				}
				if directive("AllowOverrideList", "") {
					gaps = append(gaps, vhostGuardGap{i18n.T("còn dòng AllowOverrideList", "an AllowOverrideList line is still there"), holeMode})
				}
				if !has("FallbackResource /index.php") {
					gaps = append(gaps, vhostGuardGap{missing("FallbackResource /index.php"), holeMode})
				}
			case site.HtaccessFull:
				if !directive("AllowOverride", "All") {
					gaps = append(gaps, vhostGuardGap{missing("AllowOverride All"), holeMode})
				}
			}
			passed = i18n.T("Vhost Apache gỡ PHP_VALUE, PHP_ADMIN_VALUE và khối AllowOverride khớp chế độ .htaccess \""+string(mode)+"\"",
				"The Apache vhost strips PHP_VALUE and PHP_ADMIN_VALUE, and its AllowOverride block matches the .htaccess mode \""+string(mode)+"\"")
		}
		if len(gaps) == 0 {
			return b.pass(passed, ev)
		}
		var vi, en, holeVi, holeEn []string
		for _, g := range gaps {
			vi, en = append(vi, g.what.Vi), append(en, g.what.En)
			if !containsStr(holeVi, g.hole.Vi) { // hai dòng thiếu cùng mở một lỗ: nói một lần
				holeVi, holeEn = append(holeVi, g.hole.Vi), append(holeEn, g.hole.En)
			}
		}
		return b.bad(Fail,
			i18n.T("Vhost của site chưa đủ dòng chặn: "+strings.Join(vi, "; "), "The site vhost lacks guard lines: "+strings.Join(en, "; ")),
			i18n.T("Vhost này do một bản tpan cũ ghi hoặc đã bị sửa. Lỗ còn mở: "+strings.Join(holeVi, "; ")+".",
				"This vhost was written by an older tpan or was edited. Still open: "+strings.Join(holeEn, "; ")+"."),
			[]string{m14Harden, recheck}, ev)
	})
}

// ---- .htaccess (lỗ L3, L4, L9) ----

const (
	// htaccessMaxFiles, htaccessMaxBytes: giới hạn một lần quét. Phép quét chạy
	// trong health định kỳ; một site có hàng nghìn .htaccess hay một file vài
	// trăm MB không được làm chậm cả vòng kiểm. Đây không phải hàng rào bảo
	// mật (Apache tự từ chối chỉ thị ngoài danh sách), chỉ là lời báo trước.
	htaccessMaxFiles = 200
	htaccessMaxBytes = 256 << 10
	// htaccessReadTimeout là thời gian tối đa đọc một file. User của site đặt
	// được một symlink .htaccess trỏ vào FIFO; lệnh đọc mở không chặn (dd
	// iflag=nonblock, M28) nên thường xong ngay, hạn này là lớp phòng xa.
	htaccessReadTimeout = 10 * time.Second
	// htaccessUnreadable là Finding.Directive của file không đọc được dưới user
	// của site (giá trị do hợp đồng m14_types.go định).
	htaccessUnreadable = "(không đọc được)"
)

// ScanHtaccess tìm mọi .htaccess mà Apache sẽ đọc dưới l.Public và quét từng
// file bằng htaccess.Scan. Mọi lệnh chạy dưới user của site, không bằng root:
// user của site đặt được symlink .htaccess trỏ ra ngoài, và root thì đọc được
// mọi thứ.
//
// Bỏ wp-content/uploads vì vhost đặt AllowOverride None cho thư mục đó. Đường
// dẫn find trả về chỉ đi vào một phần tử argv của dd ("if=<đường dẫn>"), không
// bao giờ vào một chuỗi lệnh; đường dẫn không nằm dưới l.Public thì bỏ.
//
// ev gồm bằng chứng của lệnh find và một dòng tóm tắt. err khác nil khi không
// tìm hay không đọc được (không phải khi có Finding).
func ScanHtaccess(ctx context.Context, sys system.System, l site.Layout) (found []htaccess.Finding, ev []Evidence, err error) {
	if l.Domain.IsZero() || !siteUserRE.MatchString(l.User) || !path.IsAbs(l.Public) || !sitePathRE.MatchString(l.Public) {
		return nil, nil, fmt.Errorf("quét .htaccess: thông tin site không hợp lệ (user %q, public %q)", l.User, l.Public)
	}
	dom := l.Domain.String()
	root := path.Clean(l.Public)
	findCmd := system.Cmd{Name: "find", AsUser: l.User,
		Args: []string{root, "-name", ".htaccess", "(", "-type", "f", "-o", "-type", "l", ")", "-print0"}}
	res, findEv, err := execCmd(ctx, sys, findCmd)
	// -print0 ngăn các đường dẫn bằng byte 0; bằng chứng để người đọc nên đổi thành xuống dòng.
	findEv.Output = clip(strings.TrimSpace(strings.ReplaceAll(joinOutput(res), "\x00", "\n")))
	ev = []Evidence{findEv}
	if err != nil {
		return nil, ev, fmt.Errorf("tìm .htaccess của %s: %w", dom, err)
	}
	if res.ExitCode != 0 {
		// Mã khác 0 là find không duyệt hết (thư mục user của site không vào
		// được, user không tồn tại): danh sách thiếu thì không kết luận được.
		return nil, ev, fmt.Errorf("tìm .htaccess của %s: find thoát mã %d: %s", dom, res.ExitCode, sslClean(firstLineWith(res.Stderr)))
	}

	var files []string
	skipped := 0
	for _, p := range strings.Split(res.Stdout, "\x00") {
		switch {
		case p == "" || !strings.HasPrefix(p, root+"/") || path.Clean(p) != p:
			// rỗng (sau byte 0 cuối) hoặc không nằm dưới public
		case strings.HasPrefix(p, root+"/wp-content/uploads/"):
			skipped++
		default:
			files = append(files, p)
		}
	}
	sort.Strings(files) // thứ tự ổn định giữa các lần chạy, để Summary không nhảy
	total := len(files)
	if total > htaccessMaxFiles {
		files = files[:htaccessMaxFiles]
	}

	// dd thay cho head (M28, S1 của docs/thay-doi/M28-thiet-ke.md): iflag=nonblock
	// để một FIFO (hay symlink tới FIFO) mà user của site đặt ở tên .htaccess
	// không làm lệnh chờ tới hết hạn rồi để lại tiến trình. Vẫn đi theo symlink,
	// như Apache khi đọc .htaccess. Đường dẫn đi nguyên vào một phần tử argv.
	read := func(f string) system.Cmd {
		return system.Cmd{Name: "dd", Args: []string{"if=" + f, "iflag=nonblock", "bs=" + strconv.Itoa(htaccessMaxBytes), "count=1", "status=none"}, AsUser: l.User}
	}
	unreadable := 0
	for _, f := range files {
		hctx, cancel := context.WithTimeout(ctx, htaccessReadTimeout)
		r, _, err := execCmd(hctx, sys, read(f))
		timedOut := hctx.Err() != nil && ctx.Err() == nil
		cancel()
		switch {
		case err != nil && !timedOut:
			return nil, ev, fmt.Errorf("đọc %s: %w", m14NoCtl(f), err)
		case err != nil || r.ExitCode != 0:
			unreadable++
			found = append(found, htaccess.Finding{File: f, Directive: htaccessUnreadable})
		default:
			found = append(found, htaccess.Scan(f, []byte(r.Stdout))...)
		}
	}

	sum := Evidence{Command: system.Describe(sys, read("FILE")) + "  (x " + strconv.Itoa(len(files)) + ")"}
	sum.Output = fmt.Sprintf("%d .htaccess files scanned, %d unreadable, %d skipped under wp-content/uploads, %d directives outside the allowed list",
		len(files), unreadable, skipped, len(found)-unreadable)
	if total > len(files) {
		sum.Output += fmt.Sprintf(" (%d files found, only the first %d scanned)", total, len(files))
	}
	return found, append(ev, sum), nil
}

// htaccessFindingLine là một dòng "<file>:<dòng>: <chỉ thị>". File không đọc
// được không có số dòng. en chọn chữ tiếng Anh cho trường hợp đó (bằng chứng
// hiện ở cả hai ngôn ngữ nên dùng bản tiếng Anh).
func htaccessFindingLine(f htaccess.Finding, en bool) string {
	file := m14NoCtl(f.File)
	if f.Directive == htaccessUnreadable {
		if en {
			return file + ": (unreadable)"
		}
		return file + ": " + htaccessUnreadable
	}
	return file + ":" + strconv.Itoa(f.Line) + ": " + m14NoCtl(f.Directive)
}

// HtaccessDirectives kiểm .htaccess của site theo chế độ của nó. core chỉ đưa
// phép này vào khi web server là Apache và site không tạm khoá.
//
//   - an-toan: quét (ScanHtaccess). Một chỉ thị ngoài danh sách của vhost làm
//     Apache trả 500 cho mọi request dưới thư mục đó, nên có là lỗi và nêu
//     đúng file, dòng (thường là plugin vừa ghi thêm).
//   - day-du: luôn cảnh báo, không chạy lệnh nào: site ghi được cấu hình Apache.
//   - tat: đạt, Apache không đọc .htaccess.
func HtaccessDirectives(l site.Layout, mode site.HtaccessMode) Check {
	b := siteBase("web.htaccess", StageWeb, l,
		i18n.T(".htaccess của site chỉ dùng chỉ thị được phép", "The site's .htaccess uses only allowed directives"))
	if g := siteGuard(b, l, true, l.Public); g != nil {
		return g
	}
	dom := l.Domain.String()
	recheck := line("sudo", "tpan", "health", dom)
	set := func(m site.HtaccessMode) string {
		return line("sudo", "tpan", "site", "set", dom, "--htaccess", string(m))
	}
	switch mode.OrDefault() {
	case site.HtaccessSafe:
	case site.HtaccessOff:
		return checkFunc(func(context.Context, system.System) Result {
			return b.pass(i18n.T("Apache không đọc .htaccess của site này (chế độ \"tat\")", "Apache does not read this site's .htaccess (mode \"tat\")"))
		})
	case site.HtaccessFull:
		return checkFunc(func(context.Context, system.System) Result {
			return b.bad(Warn,
				i18n.T("Site ở chế độ .htaccess \"day-du\": nó ghi được cấu hình Apache (AllowOverride All)", "The site is in .htaccess mode \"day-du\": it can write Apache configuration (AllowOverride All)"),
				i18n.T("Với AllowOverride All, file .htaccess do site ghi được dùng mọi chỉ thị của Apache. Nếu site bị chiếm, kẻ tấn công có thể gửi request sang socket PHP-FPM của site khác kèm cấu hình PHP do nó chọn, tức là chạy mã dưới user của site khác, và bật FollowSymLinks để đọc file của site khác qua symlink. Chế độ \"an-toan\" chỉ cho các chỉ thị WordPress và plugin phổ biến cần; tpan quét .htaccess trước khi chuyển và từ chối nếu có chỉ thị sẽ làm site báo 500.",
					"With AllowOverride All the .htaccess file, which the site can write, may use every Apache directive. If the site is compromised the attacker can send requests to another site's PHP-FPM socket with PHP settings of their choice, that is run code as another site's user, and turn on FollowSymLinks to read another site's files through a symlink. The \"an-toan\" mode allows only the directives WordPress and common plugins need; tpan scans .htaccess before switching and refuses when a directive would make the site answer 500."),
				[]string{set(site.HtaccessSafe), recheck})
		})
	default:
		return checkFunc(func(context.Context, system.System) Result {
			return b.unknown(i18n.T("chế độ .htaccess không hợp lệ (\""+string(mode)+"\")", "the .htaccess mode is not valid (\""+string(mode)+"\")"))
		})
	}

	const maxShown = 20
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		found, evs, err := ScanHtaccess(ctx, sys, l)
		if err != nil {
			return b.unknown(i18n.T("không quét được .htaccess ("+err.Error()+")", "could not scan .htaccess ("+err.Error()+")"), evs...)
		}
		if len(found) == 0 {
			return b.pass(i18n.T("Mọi .htaccess của site chỉ dùng chỉ thị trong danh sách cho phép", "Every .htaccess of the site uses only directives from the allowed list"), evs...)
		}
		var shown []string
		for i, f := range found {
			if i == maxShown {
				shown = append(shown, fmt.Sprintf("... and %d more", len(found)-maxShown))
				break
			}
			shown = append(shown, htaccessFindingLine(f, true))
		}
		evs = append(evs, Evidence{Command: "scan .htaccess (tpan)", ExitCode: 1, Output: clip(strings.Join(shown, "\n"))})

		first := found[0]
		// Hai lệnh đầu để xem và sửa đúng dòng gây lỗi; file không đọc được thì chỉ xem nó là gì.
		fix := []string{line("ls", "-l", first.File)}
		if first.Directive != htaccessUnreadable {
			n := strconv.Itoa(first.Line)
			fix = []string{line("sed", "-n", n+"p", first.File), line("nano", "+"+n, first.File)}
		}
		return b.bad(Fail,
			i18n.T(fmt.Sprintf("%d chỉ thị ngoài danh sách cho phép trong .htaccess; đầu tiên: %s", len(found), htaccessFindingLine(first, false)),
				fmt.Sprintf("%d directives outside the allowed list in .htaccess; the first: %s", len(found), htaccessFindingLine(first, true))),
			i18n.T("Site ở chế độ .htaccess \"an-toan\": vhost chỉ cho một danh sách chỉ thị cố định. Gặp một chỉ thị ngoài danh sách (hoặc một .htaccess không đọc được), Apache trả 500 cho mọi request dưới thư mục chứa file đó; thường là một plugin vừa ghi thêm. Có hai lối ra: sửa hoặc xoá dòng đó (hai lệnh đầu), hoặc nếu site thật sự cần chỉ thị này thì chuyển site sang chế độ \"day-du\" (lệnh thứ ba) và nhận cảnh báo đi kèm.",
				"The site is in .htaccess mode \"an-toan\": the vhost allows a fixed list of directives only. On a directive outside the list (or an unreadable .htaccess) Apache answers 500 to every request under the directory holding that file; usually a plugin just added it. There are two ways out: fix or delete that line (the first two commands), or if the site really needs the directive switch it to the \"day-du\" mode (the third command) and accept the warning that comes with it."),
			append(fix, set(site.HtaccessFull), recheck), evs...)
	})
}

// ---- Định tuyến: mỗi tên miền một vhost (lỗ L8) ----

// vhostDecl là một chỗ khai tên miền trong cấu hình web server đang nạp.
type vhostDecl struct {
	name, port string
	file       string
	line       int // nginx: dòng server_name; Apache: dòng <VirtualHost>
	// block nhận ra khối server/VirtualHost: hai lần khai trong cùng một khối
	// (listen 80 và listen [::]:80) không phải là trùng.
	block int
}

func (d vhostDecl) where() string { return m14NoCtl(d.file) + ":" + strconv.Itoa(d.line) }

// nameConflict là một tên miền của site bị khai ở chỗ không được khai. other
// là chỗ khai còn lại (nil khi tên chỉ nằm trong file lạ).
type nameConflict struct {
	bad     vhostDecl
	other   *vhostDecl
	foreign bool // bad nằm trong file không phải vhost của site
}

// ServerNameUnique kiểm mỗi tên miền của site tpan chỉ được khai ở đúng vhost
// của site đó, đọc từ cấu hình web server đang nạp (`nginx -T`, `apache2ctl
// -S`) chứ không từ danh sách file tpan đã ghi: một file ai đó thêm tay cũng
// nằm trong đó.
//
// Vì sao cần: khi hai vhost cùng khai một tên trên một cổng, web server chỉ
// dùng vhost nạp trước và (với nginx) chỉ in một dòng cảnh báo; `nginx -t` vẫn
// thoát mã 0. Tên miền khi đó trả trang của vhost lạ mà không phép kiểm nào
// khác chỉ ra được vì sao.
func ServerNameUnique(web string, sites []site.Layout) Check {
	b := base{id: "web.server_name.unique", stage: StageWeb,
		title: i18n.T("Mỗi tên miền chỉ được khai ở đúng vhost của site đó", "Each domain is declared only in its own site's vhost")}
	unknown := func(why i18n.Text) Check {
		return checkFunc(func(context.Context, system.System) Result { return b.unknown(why) })
	}
	var cmd system.Cmd
	var parse func(system.Result) ([]vhostDecl, bool)
	var own func(site.Layout) []string
	var grepFor, test, reload string
	switch web {
	case "nginx":
		cmd = system.Cmd{Name: "nginx", Args: []string{"-T"}}
		// Bản in cấu hình ở stdout; stderr chỉ có lời "syntax is ok" và cảnh báo.
		parse = func(r system.Result) ([]vhostDecl, bool) { return nginxDecls(r.Stdout) }
		own = func(l site.Layout) []string { return []string{l.NginxConf, l.NginxLink} }
		grepFor, test, reload = "listen|server_name", line("nginx", "-t"), line("systemctl", "reload", "nginx")
	case "apache":
		cmd = system.Cmd{Name: "apache2ctl", Args: []string{"-S"}}
		parse = func(r system.Result) ([]vhostDecl, bool) { return apacheDecls(joinOutput(r)) }
		own = func(l site.Layout) []string { return []string{l.ApacheConf, l.ApacheLink} }
		grepFor, test, reload = "VirtualHost|ServerName|ServerAlias", line("apachectl", "configtest"), line("systemctl", "reload", "apache2")
	default:
		return unknown(i18n.T("web server phải là nginx hoặc apache (nhận được \""+web+"\")", "web server must be nginx or apache (got \""+web+"\")"))
	}
	for _, l := range sites {
		if l.Domain.IsZero() {
			return unknown(i18n.T("thông tin của một site rỗng (chưa có domain)", "the layout of one site is empty (no domain)"))
		}
	}
	if len(sites) == 0 {
		return checkFunc(func(context.Context, system.System) Result {
			return b.pass(i18n.T("Chưa có site nào nên không có tên miền nào để trùng", "There is no site yet, so no domain can be declared twice"))
		})
	}

	return checkFunc(func(ctx context.Context, sys system.System) Result {
		res, ev, err := execCmd(ctx, sys, cmd)
		if err != nil {
			return b.cantRun(err, ev)
		}
		if res.ExitCode != 0 {
			code, why := strconv.Itoa(res.ExitCode), sslClean(firstLineWith(joinOutput(res), "emerg", "Syntax error", "AH0"))
			return b.unknown(i18n.T(cmd.String()+" báo lỗi nên không đọc được cấu hình đang nạp (mã "+code+": "+why+")",
				cmd.String()+" failed, so the loaded configuration cannot be read (exit "+code+": "+why+")"), ev)
		}
		decls, ok := parse(res)
		if !ok {
			return b.unknown(i18n.T("không đọc được output của "+cmd.String()+" (định dạng lạ)", "could not read the output of "+cmd.String()+" (unexpected format)"), ev)
		}

		// Bằng chứng: chỉ những chỗ khai tên miền của site tpan (bản in đủ dài cả
		// nghìn dòng), kèm dòng cảnh báo trùng tên của nginx nếu có.
		var shown []string
		var conflicts []nameConflict
		for _, l := range sites {
			dom := l.Domain.String()
			var mine []vhostDecl
			for _, d := range decls {
				if d.name == dom {
					mine = append(mine, d)
					shown = append(shown, d.where()+": "+d.name+" (port "+d.port+")")
				}
			}
			if c, found := findNameConflict(mine, own(l)); found {
				conflicts = append(conflicts, c)
			}
		}
		for _, ln := range strings.Split(res.Stderr, "\n") {
			if strings.Contains(ln, "conflicting server name") {
				shown = append(shown, strings.TrimSpace(ln))
			}
		}
		ev.Output = clip(strings.Join(shown, "\n"))
		if len(shown) == 0 {
			ev.Output = "(no tpan site name found in the loaded configuration)"
		}
		if len(conflicts) == 0 {
			return b.pass(i18n.T(fmt.Sprintf("Tên miền của %d site đều chỉ được khai ở vhost của chính site đó", len(sites)),
				fmt.Sprintf("The domains of %d sites are each declared only in their own site's vhost", len(sites))), ev)
		}

		c := conflicts[0]
		files := []string{c.bad.file}
		vi := "Tên miền " + c.bad.name + " được khai ở " + c.bad.where() + " (cổng " + c.bad.port + "), không phải vhost của site đó"
		en := "The domain " + c.bad.name + " is declared at " + c.bad.where() + " (port " + c.bad.port + "), which is not that site's vhost"
		if c.other != nil {
			if c.other.file != c.bad.file {
				files = append(files, c.other.file)
			}
			vi = "Tên miền " + c.bad.name + " được khai ở hai nơi: " + c.other.where() + " (cổng " + c.other.port + ") và " + c.bad.where() + " (cổng " + c.bad.port + ")"
			en = "The domain " + c.bad.name + " is declared in two places: " + c.other.where() + " (port " + c.other.port + ") and " + c.bad.where() + " (port " + c.bad.port + ")"
		}
		if n := len(conflicts) - 1; n > 0 {
			vi += fmt.Sprintf("; còn %d tên miền khác cũng bị trùng", n)
			en += fmt.Sprintf("; %d more domains are declared twice as well", n)
		}
		fix := []string{line("grep", append([]string{"-nE", grepFor}, files...)...)}
		if c.foreign {
			// tpan không xoá file nó không ghi. Chuyển file ra khỏi thư mục cấu
			// hình (chứ không đổi tên tại chỗ: sites-enabled nạp mọi tên file) thì
			// web server thôi đọc nó mà không mất gì.
			fix = append(fix, line("ls", "-l", c.bad.file), line("mv", c.bad.file, "/root/"))
		} else {
			fix = append(fix, line("sudo", "tpan", "health", "--fix"))
		}
		return b.bad(Fail, i18n.T(vi, en),
			i18n.T("Khi hai vhost cùng khai một tên miền trên một cổng, web server chỉ dùng vhost được nạp trước và bỏ qua vhost kia (nginx chỉ in một dòng cảnh báo, kiểm cú pháp vẫn đạt). Tên miền này có thể đang trả trang của vhost lạ thay vì site của nó. File lạ thường do ai đó thêm tay hoặc do một công cụ khác ghi; tpan không xoá file nó không ghi, nên việc gỡ là của người vận hành.",
				"When two vhosts declare the same domain on one port, the web server uses only the one loaded first and ignores the other (nginx merely prints a warning, the syntax test still passes). This domain may be answering with the other vhost's page instead of its own site. The stray file was usually added by hand or written by another tool; tpan does not delete files it did not write, so removing it is up to the operator."),
			append(fix, test, reload, cmd.String()), ev)
	})
}

// findNameConflict xét mọi chỗ khai một tên miền (mine) và trả xung đột đầu
// tiên: trước hết là chỗ khai trong file không phải vhost của site (ownFiles),
// sau đó là hai khối khác nhau cùng khai tên trên một cổng.
func findNameConflict(mine []vhostDecl, ownFiles []string) (nameConflict, bool) {
	for i, d := range mine {
		if containsStr(ownFiles, d.file) {
			continue
		}
		c := nameConflict{bad: d, foreign: true}
		// Chỗ khai còn lại để nêu trong Summary: ưu tiên vhost của site trên cùng cổng.
		for j := range mine {
			if j == i || mine[j].block == d.block {
				continue
			}
			if c.other == nil || (mine[j].port == d.port && c.other.port != d.port) {
				c.other = &mine[j]
			}
		}
		return c, true
	}
	for i, d := range mine {
		for j := range mine[:i] {
			if mine[j].port == d.port && mine[j].block != d.block {
				return nameConflict{bad: d, other: &mine[j]}, true
			}
		}
	}
	return nameConflict{}, false
}

// nginxFileRE là dòng `nginx -T` in trước nội dung từng file cấu hình.
var nginxFileRE = regexp.MustCompile(`^# configuration file (.+):$`)

// nginxDecls đọc bản in của `nginx -T` và trả mọi cặp (tên, cổng) của từng
// khối server. ok=false khi không thấy dòng mở file nào (output lạ).
//
// ponytail: không lần theo include bên trong một khối server (server_name nằm
// ở một file con) và không phân biệt địa chỉ nghe, chỉ cổng. Vhost của tpan
// không dùng cả hai; cần thì nâng lên bằng cách ghép file theo chỉ thị include.
func nginxDecls(dump string) (decls []vhostDecl, ok bool) {
	var file string
	var body []string
	block := 0
	flush := func() {
		if file != "" {
			decls = append(decls, nginxFileDecls(file, body, &block)...)
		}
		body = nil
	}
	for _, ln := range strings.Split(dump, "\n") {
		if m := nginxFileRE.FindStringSubmatch(strings.TrimRight(ln, "\r")); m != nil {
			flush()
			file, ok = m[1], true
			continue
		}
		body = append(body, ln)
	}
	flush()
	return decls, ok
}

// ngxTok là một từ của file cấu hình nginx; punct là một trong { } ;
type ngxTok struct {
	s     string
	line  int
	punct bool
}

// nginxTokens tách file cấu hình nginx thành từ, theo đúng luật của nginx: "#"
// ở đầu một từ mở chú thích tới hết dòng; chuỗi trong nháy đơn hay nháy kép là
// một từ (kể cả khi có dấu cách, dấu ; hay {}); "{" và ";" kết thúc từ đang
// đọc, trừ "${" của biến; "}" chỉ đóng khối khi đứng đầu một từ.
func nginxTokens(lines []string) []ngxTok {
	var toks []ngxTok
	var cur strings.Builder
	var quote byte
	inWord, start := false, 0
	flush := func() {
		if inWord {
			toks = append(toks, ngxTok{s: cur.String(), line: start})
			cur.Reset()
			inWord = false
		}
	}
	for n, ln := range lines {
		ln = strings.TrimRight(ln, "\r")
		for i := 0; i < len(ln); i++ {
			c := ln[i]
			switch {
			case quote != 0:
				switch {
				case c == '\\' && i+1 < len(ln):
					i++
					cur.WriteByte(ln[i])
				case c == quote:
					quote = 0
					flush()
				default:
					cur.WriteByte(c)
				}
			case c == ' ' || c == '\t':
				flush()
			case !inWord && c == '#':
				i = len(ln) // chú thích tới hết dòng
			case !inWord && (c == '"' || c == '\''):
				quote, inWord, start = c, true, n+1
			case c == ';' || (c == '{' && !strings.HasSuffix(cur.String(), "$")) || (!inWord && c == '}'):
				flush()
				toks = append(toks, ngxTok{s: string(c), line: n + 1, punct: true})
			default:
				if !inWord {
					inWord, start = true, n+1
				}
				cur.WriteByte(c)
			}
		}
		if quote != 0 {
			cur.WriteByte('\n') // chuỗi trong nháy kéo sang dòng sau
		} else {
			flush()
		}
	}
	flush()
	return toks
}

// nginxFileDecls trả các chỗ khai tên của mọi khối server trong một file. Khối
// server được tính khi nằm ở cấp ngoài cùng của file (file trong sites-enabled,
// conf.d) hoặc ngay trong khối http. block là bộ đếm khối dùng chung cho cả bản in.
func nginxFileDecls(file string, lines []string, block *int) []vhostDecl {
	var out []vhostDecl
	var stack []string // tên các khối đang mở
	var stmt []ngxTok  // chỉ thị đang đọc
	inServer, depth := false, 0
	var ports []string
	var names []ngxTok
	for _, t := range nginxTokens(lines) {
		switch {
		case !t.punct:
			stmt = append(stmt, t)
		case t.s == ";":
			// Chỉ lấy chỉ thị nằm ngay trong khối server, không lấy trong location lồng bên trong.
			if inServer && len(stack) == depth+1 && len(stmt) > 1 {
				switch stmt[0].s {
				case "listen":
					if p := nginxListenPort(stmt[1].s); p != "" && !containsStr(ports, p) {
						ports = append(ports, p)
					}
				case "server_name":
					for _, a := range stmt[1:] {
						names = append(names, ngxTok{s: strings.TrimSuffix(strings.ToLower(a.s), "."), line: stmt[0].line})
					}
				}
			}
			stmt = nil
		case t.s == "{":
			name := ""
			if len(stmt) > 0 {
				name = stmt[0].s
			}
			if !inServer && name == "server" && len(stmt) == 1 && (len(stack) == 0 || stack[len(stack)-1] == "http") {
				inServer, depth = true, len(stack)
				ports, names = nil, nil
				*block++
			}
			stack, stmt = append(stack, name), nil
		default: // "}"
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			if inServer && len(stack) == depth {
				inServer = false
				if len(ports) == 0 {
					ports = []string{"80"} // không có listen: nginx nghe *:80
				}
				for _, nm := range names {
					for _, p := range ports {
						out = append(out, vhostDecl{name: nm.s, port: p, file: file, line: nm.line, block: *block})
					}
				}
			}
			stmt = nil
		}
	}
	return out
}

// nginxListenPort lấy cổng từ tham số đầu của listen: "80", "[::]:80",
// "127.0.0.1:8080", "*:80". Chỉ có địa chỉ thì là cổng 80; socket unix thì "".
func nginxListenPort(arg string) string {
	if strings.HasPrefix(arg, "unix:") {
		return ""
	}
	if i := strings.LastIndex(arg, ":"); i >= 0 && !strings.HasSuffix(arg, "]") {
		return arg[i+1:]
	}
	if _, err := strconv.Atoi(arg); err == nil {
		return arg
	}
	return "80"
}

// Các dòng của `apache2ctl -S` (Apache 2.4, hàm dump_a_vhost trong
// server/vhost.c): một địa chỉ có nhiều vhost thì in "<địa chỉ> is a
// NameVirtualHost" rồi từng dòng "port <n> namevhost <tên> (<file>:<dòng>)",
// dưới mỗi dòng là các "alias <tên>"; địa chỉ chỉ có một vhost thì in gọn
// "<địa chỉ>:<cổng> <tên> (<file>:<dòng>)".
var (
	apacheNameVhostRE = regexp.MustCompile(`^\s+port (\S+) namevhost (\S+) \((.+):([0-9]+)\)$`)
	apacheAliasRE     = regexp.MustCompile(`^\s+alias (\S+)$`)
	apacheSingleRE    = regexp.MustCompile(`^\S+:([0-9]+|\*)\s+(\S+) \((.+):([0-9]+)\)$`)
)

// apacheDecls đọc output của `apache2ctl -S`. ok=false khi không thấy dòng
// "VirtualHost configuration:" (output lạ). Một VirtualHost có thể hiện dưới
// nhiều địa chỉ; nó được nhận ra bằng file và số dòng nơi nó được khai.
func apacheDecls(out string) (decls []vhostDecl, ok bool) {
	blocks := map[string]int{}
	var last vhostDecl // vhost mà các dòng alias kế tiếp thuộc về; block 0 là chưa có
	add := func(port, name, file, ln string) {
		n, _ := strconv.Atoi(ln)
		key := file + ":" + ln
		if _, seen := blocks[key]; !seen {
			blocks[key] = len(blocks) + 1
		}
		last = vhostDecl{name: strings.TrimSuffix(strings.ToLower(name), "."), port: port, file: file, line: n, block: blocks[key]}
		decls = append(decls, last)
	}
	for _, ln := range strings.Split(out, "\n") {
		ln = strings.TrimRight(ln, "\r")
		if strings.HasPrefix(ln, "VirtualHost configuration:") {
			ok = true
			continue
		}
		if m := apacheNameVhostRE.FindStringSubmatch(ln); m != nil {
			add(m[1], m[2], m[3], m[4])
		} else if m := apacheSingleRE.FindStringSubmatch(ln); m != nil {
			add(m[1], m[2], m[3], m[4])
		} else if m := apacheAliasRE.FindStringSubmatch(ln); m != nil && last.block != 0 {
			d := last
			d.name = strings.TrimSuffix(strings.ToLower(m[1]), ".")
			decls = append(decls, d)
		}
	}
	return decls, ok
}

// ---- Định tuyến: tên lạ bị từ chối ----

// curlTLSHandshake là mã thoát 35 của curl, "SSL connect error": bắt tay TLS
// không xong. nginx với ssl_reject_handshake on từ chối bắt tay khi tên (SNI)
// không khớp server nào, và curl thấy đúng lỗi này.
const curlTLSHandshake = 35

// DefaultRejects kiểm một request mang tên miền lạ bị vhost mặc định từ chối
// chứ không rơi vào site nào. Khác HTTPDefault (gọi bằng địa chỉ IP, không có
// tên): ở đây request mang một tên miền thật sự lạ, đúng tình huống một tên
// miền của người khác trỏ về IP của máy. Tên thử có đuôi ".invalid" (đuôi dành
// riêng, không bao giờ là tên miền thật) và phần ngẫu nhiên để không site nào
// khai trùng được.
//
//   - cổng 80: nginx phải đóng kết nối không trả gì (curl mã thoát 52), Apache
//     phải trả 403.
//   - cổng 443 (ssl=true, khi có ít nhất một site bật SSL): nginx phải từ chối
//     bắt tay TLS (curl mã thoát 35), Apache phải trả 403.
func DefaultRejects(web string, ssl bool) Check {
	var buf [4]byte
	// crypto/rand.Read không trả lỗi (Go 1.24 trở lên: hỏng nguồn ngẫu nhiên thì
	// chương trình dừng hẳn), nên không có nhánh lỗi nào để xử.
	_, _ = rand.Read(buf[:])
	return defaultRejects(web, ssl, "tpan-"+hex.EncodeToString(buf[:])+".invalid")
}

// defaultRejects là DefaultRejects với tên thử cho sẵn, để test biết trước lệnh.
func defaultRejects(web string, ssl bool, name string) Check {
	b := base{id: "web.default.rejects", stage: StageWeb,
		title: i18n.T("Request mang tên miền lạ bị vhost mặc định từ chối", "A request for an unknown domain is rejected by the default vhost")}
	var unit, sitesDir string
	switch web {
	case "nginx":
		unit, sitesDir = "nginx", "/etc/nginx/sites-enabled"
	case "apache":
		unit, sitesDir = "apache2", "/etc/apache2/sites-enabled"
	default:
		return checkFunc(func(context.Context, system.System) Result {
			return b.unknown(i18n.T("web server phải là nginx hoặc apache (nhận được \""+web+"\")", "web server must be nginx or apache (got \""+web+"\")"))
		})
	}
	type probe struct {
		port string
		cmd  system.Cmd
	}
	probes := []probe{{"80", system.Cmd{Name: "curl", Args: []string{
		"-sS", "--max-time", "5", "-o", "/dev/null", "-w", "%{http_code}", "-H", "Host: " + name, "http://127.0.0.1/"}}}}
	if ssl {
		// -k: chứng chỉ của vhost mặc định là tự ký; ở đây chỉ xét có bị từ chối không.
		probes = append(probes, probe{"443", system.Cmd{Name: "curl", Args: []string{
			"-sS", "-k", "--max-time", "5", "-o", "/dev/null", "-w", "%{http_code}", "--resolve", name + ":443:127.0.0.1", "https://" + name + "/"}}})
	}

	return checkFunc(func(ctx context.Context, sys system.System) Result {
		var evs []Evidence
		var vi, en []string
		var recheck string
		wrong := false // có cổng trả lời sai (khác với không kết nối được)
		for _, p := range probes {
			res, ev, err := execCmd(ctx, sys, p.cmd)
			evs = append(evs, ev)
			if err != nil {
				return b.cantRun(err, evs...)
			}
			code := strings.TrimSpace(res.Stdout)
			answered := res.ExitCode == 0 && httpCodeRE.MatchString(code)
			var wantVi, wantEn string
			rejected := false
			switch {
			case web == "apache":
				wantVi, wantEn = "trả 403", "answer 403"
				rejected = answered && code == "403"
			case p.port == "80":
				wantVi, wantEn = "đóng kết nối không trả gì", "close the connection with no reply"
				rejected = res.ExitCode == curlEmptyReply
			default:
				wantVi, wantEn = "từ chối bắt tay TLS", "refuse the TLS handshake"
				rejected = res.ExitCode == curlTLSHandshake
			}
			if rejected {
				continue
			}
			if recheck == "" {
				recheck = p.cmd.String()
			}
			switch {
			case res.ExitCode == curlConnectFailed:
				vi, en = append(vi, "không kết nối được tới 127.0.0.1 cổng "+p.port), append(en, "cannot connect to 127.0.0.1 port "+p.port)
			case res.ExitCode == curlTimeout:
				vi, en = append(vi, "cổng "+p.port+" không trả lời trong 5 giây"), append(en, "port "+p.port+" did not answer within 5 seconds")
			case answered:
				wrong = true
				vi, en = append(vi, "cổng "+p.port+" trả mã "+code+" cho tên lạ, đáng lẽ phải "+wantVi), append(en, "port "+p.port+" answered "+code+" to an unknown name, it should "+wantEn)
			default:
				wrong = true
				ec := strconv.Itoa(res.ExitCode)
				vi, en = append(vi, "cổng "+p.port+" cho kết quả lạ (curl mã thoát "+ec+"), đáng lẽ phải "+wantVi), append(en, "port "+p.port+" gave an unexpected result (curl exit "+ec+"), it should "+wantEn)
			}
		}
		if len(vi) == 0 {
			pass := i18n.T("Tên miền lạ bị từ chối ở cổng 80", "An unknown domain is rejected on port 80")
			if ssl {
				pass = i18n.T("Tên miền lạ bị từ chối ở cổng 80 và cổng 443", "An unknown domain is rejected on port 80 and port 443")
			}
			return b.pass(pass, evs...)
		}
		if !wrong {
			return b.bad(Fail,
				i18n.T("Lỗi: "+strings.Join(vi, "; "), "Failed: "+strings.Join(en, "; ")),
				i18n.T("Web server không chạy, treo, hoặc không nghe cổng đó (có site bật SSL thì cổng 443 phải mở).", "The web server is not running, hangs, or does not listen on that port (with an SSL site, port 443 must be open)."),
				[]string{line("systemctl", "status", unit), line("ss", "-tlnp"), line("journalctl", "-u", unit, "-n", "50"), recheck}, evs...)
		}
		return b.bad(Fail,
			i18n.T("Tên miền lạ không bị từ chối: "+strings.Join(vi, "; "), "An unknown domain is not rejected: "+strings.Join(en, "; ")),
			i18n.T("Một vhost đang hứng mọi tên miền: có một default_server (hay vhost xếp đầu) không phải của tpan, hoặc vhost mặc định của tpan đã bị gỡ. Khi đó bất kỳ tên miền nào trỏ về IP của máy cũng thấy trang của một site trên máy.",
				"Some vhost catches every domain: a default_server (or first-listed vhost) that is not tpan's, or tpan's default vhost was removed. Any domain pointed at this machine's IP then shows the page of a site on it."),
			[]string{line("ls", "-l", sitesDir), line("sudo", "tpan", "health", "--fix"), recheck}, evs...)
	})
}
