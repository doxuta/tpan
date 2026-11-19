package templates

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
)

// Template của M12 (mục 6.11 của docs/KE-HOACH.md): pool PHP-FPM giữ chỗ, kho
// apt ngoài (deb822), ghim bản của kho đó, bản vá tự động, và cầu nối cho nginx
// của nginx.org. Hợp đồng ở m12_types.go. Cùng luật với M1, M2: gói này không
// chạm máy, mọi tham số qua danh sách ký tự cho phép rồi mới vào template, lỗi
// bọc ErrParam. Không tham số nào là chuỗi người dùng gõ tự do; chúng đều do
// mã của tpan đưa vào (tên kho, bản PHP, tên gói), nhưng vẫn kiểm vì file này
// chạm tới apt, nơi một dòng lạ có thể kéo gói từ kho khác về.

// m12Data là dữ liệu đưa vào các template của M12; trường nào template không
// dùng thì để trống.
type m12Data struct {
	Header  string
	Version string
	Source  AptSource
	Rules   []aptPinData
	Origins []string
}

// aptPinData là một đoạn của file ghim, đã kiểm và ghép sẵn các gói.
type aptPinData struct {
	Packages string
	Origin   string
	Priority int
}

func m12Render(name string, d m12Data) ([]byte, error) {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, d); err != nil {
		return nil, fmt.Errorf("render template %s: %w", name, err)
	}
	return buf.Bytes(), nil
}

// PHPIdlePool sinh /etc/php/<version>/fpm/pool.d/tpan-idle.conf.
//
// Vì sao có file này: TPAN gỡ pool "www" mặc định của gói php-fpm (nó chạy 2
// tiến trình dưới www-data dù không site nào dùng), nhưng PHP-FPM từ chối chạy
// khi không có pool nào ("No pool defined"). Pool giữ chỗ này làm PHP-FPM khởi
// động được khi máy chưa có site. Nó không tốn RAM (ondemand, chưa có ai gọi
// thì không có tiến trình con) và không ai nói chuyện được với nó (socket chỉ
// root đọc ghi, còn web server chạy dưới www-data).
//
// Dòng đầu là sitePoolComment chứ không phải Header trần: file pool theo cú
// pháp INI, dòng "# ..." bị php-fpm -t báo lỗi (xem site.go).
func PHPIdlePool(version string) ([]byte, error) {
	if !phpVersion.MatchString(version) {
		return nil, fmt.Errorf("sinh pool tpan-idle: %w: bản PHP %q không hợp lệ (dạng 8.3)", ErrParam, version)
	}
	return m12Render("php-idle-pool.conf.tmpl", m12Data{Header: sitePoolComment, Version: version})
}

// AptSource là một kho apt dạng deb822 (man sources.list, "DEB822-STYLE").
type AptSource struct {
	URIs       string // https://packages.sury.org/php/
	Suites     string // noble
	Components string // main (nginx.org: nginx)
	KeyPath    string // steps.AptKeyPath(name)
}

// aptURIRE: https, tên máy, cổng tuỳ chọn, đường dẫn tuỳ chọn. Danh sách cho
// phép nên không còn khoảng trắng, xuống dòng, ; $ ( ) " ' # hay \.
var aptURIRE = regexp.MustCompile(`^https://[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?(:[0-9]{1,5})?(/[A-Za-z0-9._~+/-]*)?$`)

var (
	aptSuiteRE     = regexp.MustCompile(`^[a-z0-9.-]+$`)
	aptComponentRE = regexp.MustCompile(`^[a-z0-9.-]+( [a-z0-9.-]+)*$`)
)

// aptKeyDir là thư mục duy nhất được đặt khoá của kho ngoài (apt-secure(8)).
const aptKeyDir = "/etc/apt/keyrings/"

func (s AptSource) validate() error {
	if !aptURIRE.MatchString(s.URIs) || strings.Contains(s.URIs, "..") {
		return fmt.Errorf("%w: URIs %q phải là https://<máy>[/đường dẫn] chỉ gồm chữ, số và . _ ~ + - /", ErrParam, s.URIs)
	}
	if !aptSuiteRE.MatchString(s.Suites) {
		return fmt.Errorf("%w: Suites %q chỉ nhận a-z 0-9 . -", ErrParam, s.Suites)
	}
	if !aptComponentRE.MatchString(s.Components) {
		return fmt.Errorf("%w: Components %q chỉ nhận a-z 0-9 . - (nhiều component cách nhau một dấu cách)", ErrParam, s.Components)
	}
	if err := siteCheckPath(s.KeyPath); err != nil {
		return err
	}
	if !strings.HasPrefix(s.KeyPath, aptKeyDir) || len(s.KeyPath) == len(aptKeyDir) {
		return fmt.Errorf("%w: KeyPath %q phải là một file dưới %s", ErrParam, s.KeyPath, aptKeyDir)
	}
	return nil
}

// AptSources sinh /etc/apt/sources.list.d/tpan-<tên>.sources. Signed-By trỏ
// vào đúng một file khoá do tpan tạo: kho này chỉ tin khoá đó, không tin cả
// vòng khoá của máy.
func AptSources(s AptSource) ([]byte, error) {
	if err := s.validate(); err != nil {
		return nil, fmt.Errorf("sinh file kho apt: %w", err)
	}
	return m12Render("apt-source.sources.tmpl", m12Data{Header: Header, Source: s})
}

// AptPinRule là một đoạn của file /etc/apt/preferences.d/tpan-<tên>.
type AptPinRule struct {
	Packages []string // ["*"] hoặc tên gói / mẫu glob như "php8.4*"
	Origin   string   // host của kho, ví dụ packages.sury.org
	Priority int      // 1..1000
}

var (
	// Gói: chữ thường, số, . + - và * cho mẫu glob. Ký tự đầu không được là
	// "-" (apt đọc như cờ) nên bắt đầu bằng chữ, số hoặc *.
	aptPkgRE    = regexp.MustCompile(`^[a-z0-9*][a-z0-9.+*-]*$`)
	aptOriginRE = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$`)
)

// AptPin sinh file ghim bản. Vì sao cần: kho ngoài (sury) có bản mới hơn của
// cả gói Ubuntu đang chạy; không ghim thì apt thay bản đang dùng bằng bản của
// kho ngoài ngay lần nâng cấp sau. Mỗi luật hạ hoặc nâng độ ưu tiên của gói
// khớp mẫu khi nó đến từ Origin.
func AptPin(rules []AptPinRule) ([]byte, error) {
	if len(rules) == 0 {
		return nil, fmt.Errorf("sinh file ghim apt: %w: không có luật nào", ErrParam)
	}
	out := make([]aptPinData, 0, len(rules))
	for i, r := range rules {
		if len(r.Packages) == 0 {
			return nil, fmt.Errorf("sinh file ghim apt: %w: luật %d không có gói", ErrParam, i)
		}
		for _, p := range r.Packages {
			if !aptPkgRE.MatchString(p) {
				return nil, fmt.Errorf("sinh file ghim apt: %w: luật %d có gói %q không hợp lệ (chỉ nhận a-z 0-9 . + - *)", ErrParam, i, p)
			}
		}
		if !aptOriginRE.MatchString(r.Origin) {
			return nil, fmt.Errorf("sinh file ghim apt: %w: luật %d có Origin %q không phải tên máy", ErrParam, i, r.Origin)
		}
		if r.Priority < 1 || r.Priority > 1000 {
			return nil, fmt.Errorf("sinh file ghim apt: %w: luật %d có Priority %d ngoài khoảng 1..1000", ErrParam, i, r.Priority)
		}
		out = append(out, aptPinData{Packages: strings.Join(r.Packages, " "), Origin: r.Origin, Priority: r.Priority})
	}
	return m12Render("apt-pin.tmpl", m12Data{Header: Header, Rules: out})
}

// aptOriginPatternRE: giá trị đi vào "origin=<o>" của Origins-Pattern. Không
// có dấu phẩy (dấu ngăn các khoá của mẫu), $ (apt.conf thay ${...}), nháy hay ;.
var aptOriginPatternRE = regexp.MustCompile(`^[A-Za-z0-9.:_-]+( [A-Za-z0-9.:_-]+)*$`)

// UnattendedUpgrades sinh /etc/apt/apt.conf.d/52tpan-unattended-upgrades.
//
// Số 52 để file đọc sau 20auto-upgrades và 50unattended-upgrades của Ubuntu,
// nên giá trị ở đây thắng. Danh sách trong apt.conf được cộng dồn, nên
// Origins-Pattern thêm origin vào danh sách của Ubuntu (kho -security) chứ
// không thay nó; không truyền origin nào thì không viết khối đó.
//
// Dòng đầu là "// " + Header: apt.conf có dấu chú thích riêng là //, không
// chắc nhận "#" ở mọi vị trí.
func UnattendedUpgrades(origins []string) ([]byte, error) {
	for i, o := range origins {
		if !aptOriginPatternRE.MatchString(o) {
			return nil, fmt.Errorf("sinh unattended-upgrades: %w: origin thứ %d (%q) chỉ nhận chữ, số và . : _ - (cách nhau bằng dấu cách)", ErrParam, i, o)
		}
	}
	return m12Render("unattended-upgrades.conf.tmpl", m12Data{Header: "// " + Header, Origins: origins})
}

// NginxOrgInclude sinh /etc/nginx/conf.d/tpan.conf. Gói nginx của nginx.org
// chỉ đọc conf.d/*.conf và không có sites-enabled; tpan giữ vhost ở
// sites-enabled như với gói của Ubuntu, nên file này nối hai bên lại. Không có
// tham số nên không lỗi; trả error cho đủ chữ ký hợp đồng.
func NginxOrgInclude() ([]byte, error) {
	return m12Render("nginx-org-include.conf.tmpl", m12Data{Header: Header})
}
