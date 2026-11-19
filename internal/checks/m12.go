package checks

import (
	"context"
	"errors"
	"io/fs"
	"strconv"
	"strings"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

// Phép kiểm của M12 (mục 6.11 của docs/KE-HOACH.md). Hợp đồng ở m12_types.go.

// ---- pool www thừa của php-fpm ----

// PHPPoolWWWAbsent kiểm pool "www" mặc định của gói php-fpm đã được gỡ và pool
// giữ chỗ tpan-idle có mặt.
//
// Vì sao: pool www chạy 2 tiến trình dưới www-data dù không site nào dùng, mỗi
// bản PHP một pool như vậy. Gỡ nó thì PHP-FPM cần ít nhất một pool khác để
// khởi động, nên tpan-idle.conf (ondemand, socket chỉ root) ở lại thay chỗ.
// Chỉ liệt kê thư mục pool.d, không đọc nội dung: tên file là đủ để biết.
//
// Còn www.conf là Warn (máy vẫn chạy, chỉ tốn tiến trình). Không còn www.conf
// mà thiếu tpan-idle.conf cũng là Warn: hợp đồng đòi cả hai, và máy mất pool
// giữ chỗ sẽ không khởi động được PHP-FPM ngay khi site cuối cùng bị xoá.
func PHPPoolWWWAbsent(version string) Check {
	b := base{id: "php.www_pool:" + version, stage: StagePHP,
		title: i18n.T("PHP-FPM "+version+" không còn pool www thừa", "PHP-FPM "+version+" has no leftover www pool")}
	if !phpVersionRE.MatchString(version) {
		return checkFunc(func(context.Context, system.System) Result {
			return b.unknown(i18n.T("bản PHP phải có dạng số.số, ví dụ 8.3 (nhận được \""+version+"\")",
				"PHP version must look like 8.3 (got \""+version+"\")"))
		})
	}
	dir := "/etc/php/" + version + "/fpm/pool.d"
	const www, idle = "www.conf", "tpan-idle.conf"
	// Chạy lại lệnh cài stack là cách gỡ www.conf và đặt tpan-idle.conf; hai
	// lệnh sau để người đọc tự thấy kết quả.
	fix := []string{
		line("sudo", "tpan", "stack", "install", "--web", "<web>"),
		line("ls", dir),
		"ps -eo user,cmd | grep [p]hp-fpm",
	}
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		ev := Evidence{Command: "ls " + dir + " (tpan)"}
		names, err := sys.ReadDir(dir)
		if err != nil {
			ev.ExitCode, ev.Output = -1, err.Error()
			if errors.Is(err, fs.ErrNotExist) {
				ev.ExitCode, ev.Output = 1, "không có / no such file or directory"
			}
			return b.unknown(i18n.T("không đọc được "+dir+" ("+sslClean(err.Error())+")", "could not read "+dir+" ("+sslClean(err.Error())+")"), ev)
		}
		ev.Output = clip(strings.Join(names, "\n"))
		switch {
		case containsStr(names, www):
			return b.bad(Warn,
				i18n.T("PHP-FPM "+version+" còn pool www mặc định", "PHP-FPM "+version+" still has the default www pool"),
				i18n.T("Pool www của gói php-fpm chạy 2 tiến trình dưới www-data dù không site nào dùng, tốn RAM và là thêm một cửa vào PHP. Máy vẫn chạy bình thường. Chạy lại lệnh cài stack: nó gỡ www.conf và đặt tpan-idle.conf thay chỗ để PHP-FPM vẫn khởi động được.",
					"The default www pool of the php-fpm package runs 2 processes as www-data even when no site uses it: wasted RAM and one more way into PHP. The machine runs normally. Re-run the stack install command: it removes www.conf and puts tpan-idle.conf in its place so PHP-FPM can still start."),
				fix, ev)
		case !containsStr(names, idle):
			return b.bad(Warn,
				i18n.T("PHP-FPM "+version+" thiếu pool giữ chỗ tpan-idle", "PHP-FPM "+version+" has no tpan-idle placeholder pool"),
				i18n.T("Pool www đã gỡ nhưng pool giữ chỗ tpan-idle.conf chưa có. Khi không còn site nào, PHP-FPM không có pool để chạy và từ chối khởi động (\"No pool defined\"). Chạy lại lệnh cài stack để đặt pool này.",
					"The www pool is gone but the placeholder pool tpan-idle.conf is missing. When no site is left, PHP-FPM has no pool to run and refuses to start (\"No pool defined\"). Re-run the stack install command to put it back."),
				fix, ev)
		}
		return b.pass(i18n.T("Không có pool www; pool giữ chỗ tpan-idle có mặt", "No www pool; the tpan-idle placeholder pool is present"), ev)
	})
}

// ---- bản vá bảo mật tự cài ----

// UnattendedUpgrades kiểm bản vá bảo mật tự cài hằng ngày.
//
// Hỏi apt chứ không đọc file: giá trị hiệu lực là kết quả gộp của mọi file
// trong apt.conf.d (Ubuntu có 20auto-upgrades, tpan có 52tpan-...), và
// `apt-config dump` cho đúng kết quả gộp đó. Khoá chưa đặt thì apt-config không
// in dòng nào. Thêm một lệnh dpkg-query vì giá trị "1" mà gói
// unattended-upgrades chưa cài thì không có gì chạy cả.
//
// Chưa cài hoặc tắt: Warn. apt-config không chạy được hay thoát mã khác 0 là
// Unknown (khi đó không dò dpkg-query, vì chưa biết giá trị). dpkg-query thoát
// mã 1 khi không có gói: đó là câu trả lời bình thường chứ không phải lỗi.
func UnattendedUpgrades() Check {
	b := base{id: "updates.auto", stage: StageMachine,
		title: i18n.T("Bản vá bảo mật tự cài hằng ngày", "Security patches install themselves daily")}
	const key = "APT::Periodic::Unattended-Upgrade"
	aptCmd := system.Cmd{Name: "apt-config", Args: []string{"dump", key}}
	dpkgCmd := system.Cmd{Name: "dpkg-query", Args: []string{"-W", "-f", `${db:Status-Status}\n`, "unattended-upgrades"}}
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		res, aptEv, err := execCmd(ctx, sys, aptCmd)
		if err != nil {
			return b.cantRun(err, aptEv)
		}
		if res.ExitCode != 0 {
			code, why := strconv.Itoa(res.ExitCode), sslClean(firstLineWith(joinOutput(res)))
			return b.unknown(i18n.T("apt-config báo lỗi (mã "+code+": "+why+")", "apt-config failed (exit "+code+": "+why+")"), aptEv)
		}
		value := aptConfigValue(res.Stdout, key)

		pres, dpkgEv, err := execCmd(ctx, sys, dpkgCmd)
		if err != nil {
			return b.cantRun(err, aptEv, dpkgEv)
		}
		if pres.ExitCode != 0 && pres.ExitCode != 1 {
			code := strconv.Itoa(pres.ExitCode)
			return b.unknown(i18n.T("dpkg-query báo lỗi (mã "+code+")", "dpkg-query failed (exit "+code+")"), aptEv, dpkgEv)
		}
		installed := strings.TrimSpace(pres.Stdout) == "installed"

		if installed && value == "1" {
			return b.pass(i18n.T("Bản vá tự cài hằng ngày đang bật ("+key+" \"1\")", "Daily automatic patches are on ("+key+" \"1\")"), aptEv, dpkgEv)
		}
		var vi, en string
		switch {
		case !installed:
			vi, en = "gói unattended-upgrades chưa cài", "the unattended-upgrades package is not installed"
		case value == "":
			vi, en = key+" chưa được đặt", key+" is not set"
		default:
			vi, en = key+" là \""+sslClean(value)+"\" (cần \"1\")", key+" is \""+sslClean(value)+"\" (needs \"1\")"
		}
		return b.bad(Warn,
			i18n.T("Bản vá bảo mật không tự cài: "+vi, "Security patches do not install themselves: "+en),
			i18n.T("Lỗ hổng đã công bố thì kẻ xấu có công cụ khai thác ngay, mà máy chỉ được vá khi có người đăng nhập và chạy cập nhật. Bật tự cài thì bản vá của kho bảo mật được cài mỗi ngày mà không cần ai làm gì.",
				"Once a vulnerability is public attackers have tools for it right away, and the machine is only patched when someone logs in and runs the update. With automatic install on, fixes from the security pocket are installed daily with nobody doing anything."),
			[]string{line("sudo", "tpan", "update", "auto", "--yes"), aptCmd.String()},
			aptEv, dpkgEv)
	})
}

// aptConfigValue lấy giá trị của khoá key từ output `apt-config dump`, dạng
// `KEY "giá trị";`. Không có dòng nào thì trả "" (khoá chưa đặt).
func aptConfigValue(out, key string) string {
	for _, ln := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(ln), " ")
		if !ok || k != key {
			continue
		}
		return strings.Trim(strings.TrimSuffix(strings.TrimSpace(v), ";"), `"`)
	}
	return ""
}
