package checks

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

// PackagesAbsent kiểm các gói CHƯA được cài, dùng trước khi cài stack (mục 6.1:
// chưa có nginx, apache2, mariadb, mysql). Gói nào đang ở trạng thái đã cài
// thì Fail và nêu tên gói.
//
// Lệnh: dpkg-query -W -f '${Package}\t${db:Status-Status}\n' <gói>...
// dpkg-query tự hiểu "\t" và "\n" trong định dạng. Trạng thái (dpkg-query(1)):
// "not-installed" và "config-files" (đã gỡ nhưng còn file cấu hình) là vắng
// mặt; mọi trạng thái khác (installed, half-installed, unpacked...) là có mặt.
//
// dpkg-query thoát mã 1 khi có tên gói nó không biết ("no packages found
// matching"), và đó là tin tốt ở đây. Nên mã 1 không phải lỗi: vẫn đọc stdout.
// Mã khác 0 và 1 (ví dụ khoá cơ sở dữ liệu dpkg) mới là "không kiểm được".
func PackagesAbsent(pkgs ...string) Check {
	list := strings.Join(pkgs, ",")
	b := base{id: "packages.absent:" + list, stage: StageMachine,
		title: i18n.T("Chưa cài các gói: "+strings.Join(pkgs, ", "), "These packages are not installed: "+strings.Join(pkgs, ", "))}
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		if len(pkgs) == 0 {
			return b.pass(i18n.T("Không có gói nào cần kiểm", "No packages to check"))
		}
		for _, p := range pkgs {
			if !pkgNameRE.MatchString(p) {
				return b.unknown(i18n.T("tên gói không hợp lệ: \""+p+"\"", "invalid package name: \""+p+"\""))
			}
		}
		args := append([]string{"-W", "-f", `${Package}\t${db:Status-Status}\n`}, pkgs...)
		cmd := system.Cmd{Name: "dpkg-query", Args: args}
		res, ev, err := execCmd(ctx, sys, cmd)
		if err != nil {
			return b.cantRun(err, ev)
		}
		if res.ExitCode != 0 && res.ExitCode != 1 {
			return b.unknown(i18n.T("dpkg-query báo lỗi (mã "+strconv.Itoa(res.ExitCode)+")", "dpkg-query failed (exit "+strconv.Itoa(res.ExitCode)+")"), ev)
		}

		var present []string
		for _, ln := range strings.Split(res.Stdout, "\n") {
			name, status, ok := strings.Cut(strings.TrimSpace(ln), "\t")
			if !ok || status == "not-installed" || status == "config-files" {
				continue
			}
			present = append(present, name)
		}
		if len(present) == 0 {
			return b.pass(i18n.T("Các gói này chưa được cài: "+strings.Join(pkgs, ", "), "None of these packages is installed: "+strings.Join(pkgs, ", ")), ev)
		}
		sort.Strings(present)
		names := strings.Join(present, ", ")
		return b.bad(Fail,
			i18n.T("Đã có gói được cài: "+names, "Packages already installed: "+names),
			i18n.T("Máy không sạch. TPAN chỉ cài trên máy chưa có các gói này, để không đè lên cấu hình đang chạy. Dùng máy mới, hoặc tự gỡ sau khi đã sao lưu.",
				"The machine is not clean. TPAN only installs where these packages are absent, so it never overwrites a running setup. Use a fresh machine, or remove them yourself after taking a backup."),
			[]string{line("dpkg", append([]string{"-l"}, pkgs...)...)}, ev)
	})
}
