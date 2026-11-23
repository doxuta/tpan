package core

// Ba kho apt ngoài Ubuntu mà M12 dùng (mục 6.11): PHP của Ondřej Surý,
// nginx.org, MariaDB. Đây là chỗ DUY NHẤT ghi địa chỉ kho và vân tay khoá. Vân
// tay ghim sẵn trong mã: khoá tải về chỉ được tin khi khớp, nên kẻ chiếm được
// đường tải khoá cũng không cài được gói của mình. Giá trị đã đối chiếu bằng
// `gpg --show-keys` trên máy ảo ngày 19/11/2026 (docs/thay-doi/M12.md); muốn
// đổi thì đối chiếu lại với trang của chính nhà phát hành.

import (
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strings"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
)

// Tên kho (steps.AptRepo.Name): file của kho là tpan-<tên>.sources, tpan-<tên>.gpg.
const (
	RepoSury    = "sury"
	RepoNginx   = "nginx"
	RepoMariaDB = "mariadb"
)

// Origin của từng kho, như dòng "Origin:" trong file Release của kho; dùng cho
// danh sách tự vá của unattended-upgrades ("origin=<giá trị>").
const (
	OriginSury    = "deb.sury.org"
	OriginNginx   = "nginx"
	OriginMariaDB = "MariaDB"
)

// suryTelemetryHost là tên mà php-fpm của kho sury (từ 8.4) hỏi DNS rồi gửi
// telemetry tới mỗi lần khởi động hoặc chạy `-t` (strace trên máy ảo M12).
// Chủ dự án quyết chặn (docs/KE-HOACH.md, tìm "telemetry"): thêm kho thì
// steps.HostsBlock trỏ tên này về 0.0.0.0 trong /etc/hosts, gỡ bản PHP cuối
// cùng của kho thì gỡ dòng đó (PrepareInstall, PrepareRemove trong php.go).
const suryTelemetryHost = "telemetry.sury.org"

var (
	suryFingerprints    = []string{"15058500A0235D97F5D10063B188E2B695BD4743"}
	nginxFingerprints   = []string{"8540A6F18833A80E9C1653A42FD21310B49F6B46", "573BFD6B3D8FBC641079A6ABABF5BD827BD9BF62", "9E9BE90EACBCDE69FE9B204CBCDCD8A38D88A2B3"}
	mariadbFingerprints = []string{"177F4010FE56CA3336300305F1656F24C74CD1D8"}
)

// RepoSourcesPath là file sources của kho name; có file này nghĩa là kho đã thêm.
func RepoSourcesPath(name string) string { return "/etc/apt/sources.list.d/tpan-" + name + ".sources" }

var codenameRE = regexp.MustCompile(`^[a-z]+$`)

// osCodename đọc tên mã của bản Ubuntu (VERSION_CODENAME trong /etc/os-release,
// "noble" với 24.04). Không ghi cứng "noble": lên 26.04 không phải sửa mã.
func osCodename(sys system.System) (string, error) {
	data, err := sys.ReadFile("/etc/os-release")
	if err != nil {
		return "", fmt.Errorf("đọc /etc/os-release: %w", err)
	}
	for line := range strings.Lines(string(data)) {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "VERSION_CODENAME="); ok {
			v = strings.Trim(v, `"'`)
			if !codenameRE.MatchString(v) {
				return "", fmt.Errorf("VERSION_CODENAME lạ trong /etc/os-release: %q", v)
			}
			return v, nil
		}
	}
	return "", errors.New("/etc/os-release không có VERSION_CODENAME")
}

func repoSources(name, uri, codename, components string) ([]byte, error) {
	return templates.AptSources(templates.AptSource{URIs: uri, Suites: codename, Components: components, KeyPath: steps.AptKeyPath(name)})
}

// repoSury dựng kho PHP của Ondřej Surý. phpVersions là MỌI bản PHP cài từ kho
// này (kể cả bản sắp cài). Ghim đã kiểm trên máy ảo: cả kho 100 để gói meta
// php-fpm và php8.3-* của Ubuntu không bị thay (bản lớn không tự nhảy); riêng
// php-common và php<bản>* của các bản lấy từ kho này 500, vì php8.4-common cần
// php-common (>= 2:95~) mà Ubuntu chỉ có 2:93.
func repoSury(codename string, phpVersions []string) (*steps.AptRepo, error) {
	src, err := repoSources(RepoSury, "https://packages.sury.org/php/", codename, "main")
	if err != nil {
		return nil, err
	}
	pkgs := []string{"php-common"}
	for _, v := range slices.Sorted(slices.Values(phpVersions)) {
		if !phpVersionRE.MatchString(v) {
			return nil, fmt.Errorf("bản PHP %q không hợp lệ (dạng 8.3)", v)
		}
		pkgs = append(pkgs, "php"+v+"*")
	}
	pin, err := templates.AptPin([]templates.AptPinRule{
		{Packages: []string{"*"}, Origin: "packages.sury.org", Priority: 100},
		{Packages: pkgs, Origin: "packages.sury.org", Priority: 500},
	})
	if err != nil {
		return nil, err
	}
	return &steps.AptRepo{
		Name:         RepoSury,
		Title:        i18n.T("kho PHP của Ondřej Surý (packages.sury.org)", "Ondřej Surý's PHP repository (packages.sury.org)"),
		KeyURL:       "https://packages.sury.org/php/apt.gpg",
		Fingerprints: suryFingerprints,
		Sources:      src,
		Pin:          pin,
	}, nil
}

// repoNginx dựng kho của nginx.org, nhánh stable hoặc mainline. Ghim 900 cho
// origin nginx.org như hướng dẫn của chính nginx.org, để gói của kho này thắng
// gói nginx của Ubuntu.
func repoNginx(codename string, mainline bool) (*steps.AptRepo, error) {
	uri, title := "https://nginx.org/packages/ubuntu", "stable"
	if mainline {
		uri, title = "https://nginx.org/packages/mainline/ubuntu", "mainline"
	}
	src, err := repoSources(RepoNginx, uri, codename, "nginx")
	if err != nil {
		return nil, err
	}
	pin, err := templates.AptPin([]templates.AptPinRule{{Packages: []string{"*"}, Origin: "nginx.org", Priority: 900}})
	if err != nil {
		return nil, err
	}
	return &steps.AptRepo{
		Name:         RepoNginx,
		Title:        i18n.T("kho nginx.org ("+title+")", "the nginx.org repository ("+title+")"),
		KeyURL:       "https://nginx.org/keys/nginx_signing.key",
		Fingerprints: nginxFingerprints,
		Sources:      src,
		Pin:          pin,
	}, nil
}

var (
	dbLineRE     = regexp.MustCompile(`^[0-9]{1,2}\.[0-9]{1,2}$`)
	phpVersionRE = regexp.MustCompile(`^[0-9]+\.[0-9]+$`)
)

// repoMariaDB dựng kho của MariaDB cho dòng line ("11.8"). Không ghim: bản của
// kho luôn cao hơn bản Ubuntu nên tự thắng, và đổi dòng là đổi file sources.
func repoMariaDB(codename, line string) (*steps.AptRepo, error) {
	if !dbLineRE.MatchString(line) {
		return nil, refuse("mariadb_line_invalid", i18n.T(
			fmt.Sprintf("phiên bản MariaDB %q không hợp lệ (dạng 11.8)", line),
			fmt.Sprintf("MariaDB series %q is not valid (like 11.8)", line)))
	}
	src, err := repoSources(RepoMariaDB, "https://dlm.mariadb.com/repo/mariadb-server/"+line+"/repo/ubuntu", codename, "main")
	if err != nil {
		return nil, err
	}
	return &steps.AptRepo{
		Name:         RepoMariaDB,
		Title:        i18n.T("kho của MariaDB (dòng "+line+")", "the MariaDB repository (series "+line+")"),
		KeyURL:       "https://mariadb.org/mariadb_release_signing_key.pgp",
		Fingerprints: mariadbFingerprints,
		Sources:      src,
	}, nil
}

// repoEnabled cho biết kho name đã được thêm (có file sources của tpan).
func repoEnabled(sys system.System, name string) (bool, error) {
	_, err := sys.Stat(RepoSourcesPath(name))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("kiểm kho %s: %w", name, err)
	}
	return true, nil
}
