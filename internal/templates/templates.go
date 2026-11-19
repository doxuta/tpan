// Package templates sinh nội dung file config của stack (site mặc định của
// Nginx và Apache, logrotate) từ template nhúng trong file chạy.
//
// Vì sao tách riêng: config là nguồn duy nhất của sự thật (mục 3 và 5 của
// docs/KE-HOACH.md), nên chỉ sinh ra ở một chỗ và có golden test để thấy ngay
// khi nội dung đổi. Gói này không chạm vào máy: nó chỉ trả []byte, việc ghi file
// và kiểm cú pháp (nginx -t, apachectl configtest, logrotate -d) do Step lo.
//
// Gói này cố ý không import core hay steps (tránh vòng import), nên tự kiểm
// tham số và trả lỗi khi gặp tham số lạ. Mọi tham số đi vào template đều qua
// danh sách ký tự cho phép; template là text/template (không phải html/template)
// nên không có lớp thoát ký tự nào khác che chắn thay.
package templates

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"text/template"
)

// Header là dòng đầu của mọi file tpan sinh ra (mục 5). tpan health so dòng này
// để biết file có do tpan quản lý không, nên giữ đúng từng chữ.
const Header = "# Do tpan quản lý. Sửa tay sẽ bị tpan health báo lệch."

// ErrParam báo tham số không hợp lệ. Dùng errors.Is để nhận biết.
var ErrParam = errors.New("tham số template không hợp lệ")

//go:embed files/*.tmpl
var files embed.FS

// Các template được phân tích một lần lúc nạp gói. Template hỏng (lỗi cú pháp)
// là lỗi lập trình, nên panic ngay khi chạy test thay vì để lọt tới máy chủ.
// Option missingkey=error: tên trường gõ sai thì lỗi chứ không in "<no value>"
// vào file config.
var tmpl = template.Must(
	template.New("").Option("missingkey=error").ParseFS(files, "files/*.tmpl"),
)

// DefaultSite là tham số của site mặc định.
type DefaultSite struct {
	// AcmeRoot là webroot chung cho thử thách Let's Encrypt, ví dụ
	// "/var/lib/tpan/acme". Certbot ghi vào AcmeRoot/.well-known/acme-challenge/.
	AcmeRoot string
}

// Logrotate là tham số của /etc/logrotate.d/tpan-sites.
type Logrotate struct {
	// Web là web server đang dùng: "nginx" hoặc "apache". Quyết định lệnh
	// báo web server mở lại file log sau khi xoay.
	Web string
}

// NginxDefault sinh tpan-default.conf cho Nginx.
func NginxDefault(d DefaultSite) ([]byte, error) {
	if err := d.validate(); err != nil {
		return nil, fmt.Errorf("sinh nginx tpan-default: %w", err)
	}
	return render("nginx-default.conf.tmpl", data{Header: Header, AcmeRoot: d.AcmeRoot})
}

// ApacheDefault sinh tpan-default.conf cho Apache. Tên file sắp xếp trước mọi
// vhost của site là việc của Step; ở đây chỉ sinh nội dung.
func ApacheDefault(d DefaultSite) ([]byte, error) {
	if err := d.validate(); err != nil {
		return nil, fmt.Errorf("sinh apache tpan-default: %w", err)
	}
	return render("apache-default.conf.tmpl", data{Header: Header, AcmeRoot: d.AcmeRoot})
}

// LogrotateSites sinh /etc/logrotate.d/tpan-sites.
func LogrotateSites(l Logrotate) ([]byte, error) {
	if err := l.validate(); err != nil {
		return nil, fmt.Errorf("sinh logrotate tpan-sites: %w", err)
	}
	return render("logrotate-sites.tmpl", data{Header: Header, Web: l.Web})
}

// data là dữ liệu chung đưa vào template; trường nào template không dùng thì
// để trống.
type data struct {
	Header   string
	AcmeRoot string
	Web      string
}

func render(name string, d data) ([]byte, error) {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, d); err != nil {
		return nil, fmt.Errorf("render template %s: %w", name, err)
	}
	return buf.Bytes(), nil
}

// pathSegment: mỗi đoạn đường dẫn chỉ gồm chữ, số, '.', '_', '-'. Danh sách cho
// phép (không phải danh sách cấm) vì nó chặn luôn khoảng trắng, xuống dòng,
// dấu ; { } $ " ' # \ và ký tự lạ, tức mọi thứ có thể làm lệch cú pháp config
// của Nginx hay Apache.
var pathSegment = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// phpVersion là bản PHP dạng "8.3" (cùng luật với steps).
var phpVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+$`)

func (d DefaultSite) validate() error {
	root := d.AcmeRoot
	if root == "" {
		return fmt.Errorf("%w: AcmeRoot rỗng", ErrParam)
	}
	if !strings.HasPrefix(root, "/") {
		return fmt.Errorf("%w: AcmeRoot %q phải là đường dẫn tuyệt đối", ErrParam, root)
	}
	// Bỏ dấu '/' đầu rồi tách; đường dẫn "/" hay có "//" hay kết thúc bằng "/"
	// đều sinh ra đoạn rỗng và bị chặn ở dưới. "/" cũng bị chặn vì như vậy
	// webroot là cả ổ đĩa.
	for _, seg := range strings.Split(root[1:], "/") {
		if !pathSegment.MatchString(seg) || seg == "." || seg == ".." {
			return fmt.Errorf("%w: AcmeRoot %q có đoạn %q không hợp lệ (chỉ nhận a-z A-Z 0-9 . _ -, không có đoạn rỗng, . hay ..)", ErrParam, root, seg)
		}
	}
	return nil
}

func (l Logrotate) validate() error {
	if l.Web != "nginx" && l.Web != "apache" {
		return fmt.Errorf("%w: Web %q phải là \"nginx\" hoặc \"apache\"", ErrParam, l.Web)
	}
	return nil
}
