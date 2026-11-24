package templates

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/doxuta/tpan/internal/htaccess"
	"github.com/doxuta/tpan/internal/site"
)

// Template của một site WordPress (mục 5 và 6.2 của docs/KE-HOACH.md): pool
// PHP-FPM, vhost Nginx, vhost Apache, .htaccess chuẩn của WordPress.
//
// Mọi tên và đường dẫn lấy từ site.Layout. Layout chỉ đúng khi dựng bằng
// site.LayoutOf từ một validate.Domain đã kiểm, nên ở đây đòi Layout trùng
// khít LayoutOf(l.Domain): ai đó tự điền tay một trường (ví dụ Public chứa
// dấu ; hay xuống dòng) thì bị từ chối trước khi chạm tới template.

// sitePoolComment là dòng đầu của file pool. File pool theo cú pháp INI của
// PHP: từ PHP 7.0 dấu # không còn là chú thích, một dòng "# ..." bị đọc thành
// khoá không có giá trị và php-fpm -t báo lỗi "value is NULL". Nên dòng đầu là
// "; " + Header: vẫn chứa nguyên văn Header để tpan health nhận ra file của tpan.
var sitePoolComment = "; " + Header

// siteName: tên user Linux và tên pool, sinh từ slug ([a-z0-9_]) cộng tiền tố.
var siteName = regexp.MustCompile(`^[a-z0-9_-]+$`)

// siteData là dữ liệu đưa vào bốn template của site.
type siteData struct {
	Header      string
	Domain      string
	User        string
	Pool        string
	Dir         string
	Public      string
	Tmp         string
	LogDir      string
	PHPErrorLog string
	Socket      string
	AcmeRoot    string
	// Ba trường dưới chỉ vhost Apache dùng (M14): chế độ .htaccess đã kiểm, và
	// hai chuỗi của bản an-toan lấy từ gói htaccess (withHtaccess điền).
	Htaccess        string
	OverrideOptions string
	OverrideList    string
	// Tên phụ (M19, m19.go withNames điền): rỗng hết thì vhost giống từng byte
	// bản trước M19.
	ContentAliases []string
	NameBlocks80   []nameBlock
	NameBlocks443  []nameBlock
	// Luật chuyển hướng theo đường dẫn (M19, m19.go redirectLines).
	Redirects []redirectLine
}

// SitePool sinh file pool PHP-FPM của site: /etc/php/<ver>/fpm/pool.d/tpan-<slug>.conf.
// php là bản PHP của pool ("8.3"): socket mang số bản (site.Layout.SocketFor).
func SitePool(l site.Layout, php string) ([]byte, error) {
	d, err := siteDataOf(l, php)
	if err != nil {
		return nil, fmt.Errorf("sinh pool PHP-FPM: %w", err)
	}
	d.Header = sitePoolComment
	return siteRender("site-pool.conf.tmpl", d)
}

// NginxSite sinh vhost Nginx của site. php là bản PHP site đang dùng (chọn
// socket); acmeRoot là webroot chung của Let's Encrypt.
func NginxSite(l site.Layout, php, acmeRoot string) ([]byte, error) {
	d, err := siteVhostData(l, php, acmeRoot)
	if err != nil {
		return nil, fmt.Errorf("sinh vhost nginx: %w", err)
	}
	return siteRender("site-nginx.conf.tmpl", d)
}

// ApacheSite sinh vhost Apache của site. php và acmeRoot như NginxSite. mode là
// chế độ .htaccess của site (M14, mục 6.13): quyết định khối AllowOverride của
// thư mục public (files/site-apache-override.tmpl). Rỗng là an-toan.
func ApacheSite(l site.Layout, php, acmeRoot string, mode site.HtaccessMode) ([]byte, error) {
	d, err := siteVhostData(l, php, acmeRoot)
	if err == nil {
		d, err = withHtaccess(d, mode)
	}
	if err != nil {
		return nil, fmt.Errorf("sinh vhost apache: %w", err)
	}
	return siteRender("site-apache.conf.tmpl", d)
}

// withHtaccess kiểm chế độ .htaccess rồi điền các trường mà khối AllowOverride
// của vhost Apache cần. Chế độ rỗng hiểu là an-toan (mode.OrDefault()); giá trị
// ngoài ba chế độ là ErrParam, vì chuỗi này quyết định site được ghi gì vào
// cấu hình Apache. Danh sách chỉ thị lấy từ gói htaccess: cùng một hằng với
// phép quét .htaccess và phép kiểm web.hardened, nên ba nơi không lệch nhau.
func withHtaccess(d siteData, mode site.HtaccessMode) (siteData, error) {
	mode, err := site.ParseHtaccessMode(string(mode.OrDefault()))
	if err != nil {
		return siteData{}, fmt.Errorf("%w: %v", ErrParam, err)
	}
	d.Htaccess = string(mode)
	d.OverrideOptions = htaccess.OverrideOptions()
	d.OverrideList = htaccess.List()
	return d, nil
}

// WPHtaccess trả .htaccess chuẩn của WordPress (bản Apache). Không có tham số
// nên không lỗi; template hỏng là lỗi lập trình, test bắt được.
func WPHtaccess() []byte {
	out, err := siteRender("site-htaccess.tmpl", siteData{Header: Header})
	if err != nil {
		panic(err)
	}
	return out
}

func siteVhostData(l site.Layout, php, acmeRoot string) (siteData, error) {
	d, err := siteDataOf(l, php)
	if err != nil {
		return siteData{}, err
	}
	// Dùng lại đúng phép kiểm AcmeRoot của site mặc định (M1).
	if err := (DefaultSite{AcmeRoot: acmeRoot}).validate(); err != nil {
		return siteData{}, err
	}
	d.AcmeRoot = acmeRoot
	return d, nil
}

// siteDataOf kiểm Layout rồi chép các trường cần dùng. Kiểm hai lớp: Layout
// phải đúng là LayoutOf(Domain), và mọi tên, đường dẫn vẫn qua danh sách ký tự
// cho phép (lớp chặn cuối, phòng khi quy tắc của LayoutOf đổi về sau).
func siteDataOf(l site.Layout, php string) (siteData, error) {
	if l.Domain.IsZero() {
		return siteData{}, fmt.Errorf("%w: Layout zero (chưa có domain)", ErrParam)
	}
	if l != site.LayoutOf(l.Domain) {
		return siteData{}, fmt.Errorf("%w: Layout của %s không khớp site.LayoutOf", ErrParam, l.Domain)
	}
	for _, n := range []string{l.User, l.Pool} {
		if !siteName.MatchString(n) {
			return siteData{}, fmt.Errorf("%w: tên %q có ký tự không cho phép", ErrParam, n)
		}
	}
	if !phpVersion.MatchString(php) {
		return siteData{}, fmt.Errorf("%w: bản PHP %q không hợp lệ (dạng 8.3)", ErrParam, php)
	}
	for _, p := range []string{l.Dir, l.Public, l.Tmp, l.LogDir, l.PHPErrorLog, l.SocketFor(php)} {
		if err := siteCheckPath(p); err != nil {
			return siteData{}, err
		}
	}
	return siteData{
		Header:      Header,
		Domain:      l.Domain.String(),
		User:        l.User,
		Pool:        l.Pool,
		Dir:         l.Dir,
		Public:      l.Public,
		Tmp:         l.Tmp,
		LogDir:      l.LogDir,
		PHPErrorLog: l.PHPErrorLog,
		Socket:      l.SocketFor(php),
	}, nil
}

// siteCheckPath: đường dẫn tuyệt đối, mỗi đoạn qua pathSegment, không đoạn
// rỗng, "." hay "..". Cùng luật với AcmeRoot của M1.
func siteCheckPath(p string) error {
	if !strings.HasPrefix(p, "/") {
		return fmt.Errorf("%w: %q phải là đường dẫn tuyệt đối", ErrParam, p)
	}
	for _, seg := range strings.Split(p[1:], "/") {
		if !pathSegment.MatchString(seg) || seg == "." || seg == ".." {
			return fmt.Errorf("%w: %q có đoạn %q không hợp lệ", ErrParam, p, seg)
		}
	}
	return nil
}

func siteRender(name string, d siteData) ([]byte, error) {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, d); err != nil {
		return nil, fmt.Errorf("render template %s: %w", name, err)
	}
	return buf.Bytes(), nil
}
