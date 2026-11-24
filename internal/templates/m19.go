package templates

// Tên phụ của site trong vhost (M19, mục 3.4 của docs/thay-doi/M19-thiet-ke.md).
//
// Một hàm chia tên (splitNames) quyết định tên nào nằm ở khối nào; template chỉ
// in danh sách đã chia. Mọi đích chuyển hướng là tên cố định đã kiểm, không bao
// giờ là $host hay %{HTTP_HOST} (B11). Site không tên phụ thì mọi danh sách rỗng
// và vhost giống từng byte bản trước M19 (B5, TestVhostZeroNamesGolden).

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/validate"
)

// m19Name là lớp chặn cuối cho mọi tên đi vào vhost: validate.Domain đã kiểm
// chặt hơn, đây chỉ phòng một giá trị dựng sai đường (như siteCheckPath).
var m19Name = regexp.MustCompile(`^[a-z0-9.-]{1,253}$`)

// nameBlock là một khối server / VirtualHost chuyển tên phụ (files/site-*-name-redirect*.tmpl).
type nameBlock struct {
	Names string // mọi tên, cách nhau một dấu cách (server_name của nginx)
	First string // ServerName của Apache
	Rest  string // ServerAlias của Apache ("" thì không có dòng)
	To    string // <scheme>://<tên>, không gạch chéo cuối
	// Lấy từ vhost của site.
	LogDir   string
	AcmeRoot string
	Dir      string
	Public   string
	Cert     string
	Key      string
}

// nameSplit là kết quả chia tên theo bảng mục 3.4.
type nameSplit struct {
	Content  []string // khối nội dung (cổng 80, hoặc 443 khi có SSL), không gồm tên chính
	Group80  []string // một khối cổng 80 chuyển về <scheme>://<tên chính>
	Own80    []string // SSL: mỗi tên một khối cổng 80 chuyển về https://<chính tên đó>
	Group443 []string // SSL: một khối cổng 443 chuyển về https://<tên chính>
}

// splitNames chia tên phụ theo bảng mục 3.4. Khử trùng (một tên chỉ xuất hiện
// một lần, lần đầu thắng, Serve xét trước Redirect) và bỏ tên trùng tên chính,
// để server_name không bao giờ lặp một tên. Covered chỉ dùng khi ssl. Trong
// Group80, tên redirect đứng trước tên serve chưa phủ.
func splitNames(primary validate.Domain, n site.Names, ssl bool) (nameSplit, error) {
	var sp nameSplit
	covered := map[string]bool{}
	for _, c := range n.Covered {
		covered[c.String()] = true
	}
	seen := map[string]bool{primary.String(): true}
	add := func(list []validate.Domain, serve bool) error {
		for _, d := range list {
			s := d.String()
			if !m19Name.MatchString(s) {
				return fmt.Errorf("%w: tên phụ %q có ký tự không cho phép", ErrParam, s)
			}
			if seen[s] {
				continue
			}
			seen[s] = true
			switch {
			case !ssl && serve:
				sp.Content = append(sp.Content, s)
			case !ssl:
				sp.Group80 = append(sp.Group80, s)
			case serve && covered[s]:
				sp.Content = append(sp.Content, s)
				sp.Own80 = append(sp.Own80, s)
			case serve:
				// Chưa có trong chứng chỉ: không lên 443 (trình duyệt báo lỗi
				// chứng chỉ), cổng 80 chuyển về tên chính.
				sp.Group80 = append(sp.Group80, s)
			default:
				sp.Group80 = append(sp.Group80, s)
				if covered[s] {
					sp.Group443 = append(sp.Group443, s)
				}
			}
		}
		return nil
	}
	if err := add(n.Serve, true); err != nil {
		return nameSplit{}, err
	}
	// Tên serve chưa phủ đã vào Group80 trước; đưa tên redirect lên đầu khối
	// (ServerName của Apache là tên redirect đầu tiên, như bản thử t5).
	uncovered := sp.Group80
	sp.Group80 = nil
	if err := add(n.Redirect, false); err != nil {
		return nameSplit{}, err
	}
	sp.Group80 = append(sp.Group80, uncovered...)
	return sp, nil
}

// m19Rule là lớp chặn cuối cho nguồn và đích của luật (mục 3.4); độ dài
// 1..1024 kiểm riêng vì regexp của Go không nhận số lặp quá 1000.
var m19Rule = regexp.MustCompile(`^[A-Za-z0-9._~/:?=&-]+$`)

// redirectLine là một luật đã sẵn để in (files/site-*-redirects.tmpl).
type redirectLine struct {
	Source    string
	Target    string // luôn là URL tuyệt đối (S16)
	Code      int
	KeepQuery bool
}

// redirectLines dựng các dòng luật. Đích dạng đường dẫn thành
// scheme + tên chính + đường dẫn (S16): không biến nào lấy từ request (B11).
// Nguồn trùng thì giữ luật đầu (store đã chặn trùng; nginx từ chối hai
// "location =" cùng đường dẫn, nên không để một dòng hỏng làm đổ cả vhost).
// Giá trị lạ (zero, mã khác 301/302, giữ query khi đích đã có query) là
// ErrParam: core đã từ chối chúng, tới đây là lỗi lập trình.
func redirectLines(scheme, primary string, rules []site.Redirect) ([]redirectLine, error) {
	var out []redirectLine
	seen := map[string]bool{}
	ok := func(s string) bool { return len(s) <= 1024 && m19Rule.MatchString(s) }
	for _, r := range rules {
		src, dst := r.Source.String(), r.Target.String()
		if !r.Target.IsAbsolute() {
			dst = scheme + primary + dst
		}
		switch {
		case !ok(src) || !strings.HasPrefix(src, "/") || !ok(dst) || r.Target.IsZero():
			return nil, fmt.Errorf("%w: luật chuyển hướng %d có nguồn hay đích không cho phép", ErrParam, r.ID)
		case r.Code != 301 && r.Code != 302:
			return nil, fmt.Errorf("%w: luật chuyển hướng %d có mã %d", ErrParam, r.ID, r.Code)
		case r.KeepQuery && r.Target.HasQuery():
			return nil, fmt.Errorf("%w: luật chuyển hướng %d giữ query khi đích đã có query", ErrParam, r.ID)
		case seen[src]:
			continue
		}
		seen[src] = true
		out = append(out, redirectLine{Source: src, Target: dst, Code: r.Code, KeepQuery: r.KeepQuery})
	}
	return out, nil
}

// withNames chia tên, dựng luật rồi điền các trường M19 của d. cert, key chỉ
// dùng khi ssl. Bản tạm khoá không qua đây (không vẽ luật, không khối chuyển).
func withNames(d siteData, l site.Layout, cert, key string, ssl bool, n site.Names) (siteData, error) {
	sp, err := splitNames(l.Domain, n, ssl)
	if err != nil {
		return siteData{}, err
	}
	scheme := "http://"
	if ssl {
		scheme = "https://"
	}
	if d.Redirects, err = redirectLines(scheme, d.Domain, n.Rules); err != nil {
		return siteData{}, err
	}
	block := func(names []string, to string) nameBlock {
		return nameBlock{
			Names: strings.Join(names, " "), First: names[0], Rest: strings.Join(names[1:], " "),
			To: to, LogDir: d.LogDir, AcmeRoot: d.AcmeRoot, Dir: d.Dir, Public: d.Public, Cert: cert, Key: key,
		}
	}
	d.ContentAliases = sp.Content
	if len(sp.Group80) > 0 {
		d.NameBlocks80 = append(d.NameBlocks80, block(sp.Group80, scheme+d.Domain))
	}
	for _, s := range sp.Own80 {
		d.NameBlocks80 = append(d.NameBlocks80, block([]string{s}, "https://"+s))
	}
	if len(sp.Group443) > 0 {
		d.NameBlocks443 = append(d.NameBlocks443, block(sp.Group443, "https://"+d.Domain))
	}
	return d, nil
}

// suspendedAliases: tên phụ của bản tạm khoá. Cổng 80 mọi tên phụ (serve và
// redirect, khử trùng, bỏ tên chính); cổng 443 chỉ tên có trong chứng chỉ.
func suspendedAliases(l site.Layout, n site.Names, ssl bool) (a80, a443 []string, err error) {
	sp, err := splitNames(l.Domain, n, false)
	if err != nil {
		return nil, nil, err
	}
	a80 = append(sp.Content, sp.Group80...)
	if ssl {
		covered := map[string]bool{}
		for _, c := range n.Covered {
			covered[c.String()] = true
		}
		for _, s := range a80 {
			if covered[s] {
				a443 = append(a443, s)
			}
		}
	}
	return a80, a443, nil
}

// SiteVhostNames như SiteVhost (templates/ssl.go) cộng tên phụ, luật chuyển hướng
// và bộ tên trong chứng chỉ (n.Covered, chỉ dùng khi ssl).
func SiteVhostNames(l site.Layout, web, php, acmeRoot string, ssl bool, mode site.HtaccessMode, n site.Names) ([]byte, error) {
	if web != "nginx" && web != "apache" {
		return nil, fmt.Errorf("chọn vhost: %w: web %q phải là \"nginx\" hoặc \"apache\"", ErrParam, web)
	}
	// Cùng lời lỗi với NginxSite, NginxSiteSSL, ApacheSite, ApacheSiteSSL.
	what := "sinh vhost " + web
	if ssl {
		what += " SSL"
	}
	if ssl {
		d, err := sslSiteData(l, php, acmeRoot)
		if err == nil && web == "apache" {
			d.siteData, err = withHtaccess(d.siteData, mode)
		}
		if err == nil {
			d.siteData, err = withNames(d.siteData, l, d.Cert, d.Key, true, n)
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", what, err)
		}
		return sslRender("site-"+web+"-ssl.conf.tmpl", d)
	}
	d, err := siteVhostData(l, php, acmeRoot)
	if err == nil && web == "apache" {
		d, err = withHtaccess(d, mode)
	}
	if err == nil {
		d, err = withNames(d, l, "", "", false, n)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	return siteRender("site-"+web+".conf.tmpl", d)
}
