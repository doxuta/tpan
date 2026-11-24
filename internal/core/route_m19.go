package core

// M19 (docs/thay-doi/M19-thiet-ke.md mục 3.8, "Route explain"): lần đường đi
// của một request mang Host là tên PHỤ của site. Khung các chặng nằm ở
// routeTrace (route_m14.go); file này lo ba việc: tra tên về site, và hai
// lần gọi thật riêng của tên phụ (redirect: thấy 301 và Location; serve: 200
// với trang WordPress hoặc WordPress tự chuyển hướng canonical).

import (
	"context"
	"strconv"
	"strings"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// CodeNameUnknown: không site nào có tên được hỏi (API: 404).
const CodeNameUnknown = "name_unknown"

func nameUnknown() error {
	return refuse(CodeNameUnknown, i18n.T(
		"không site nào có tên này; request với Host này rơi vào vhost mặc định và bị từ chối",
		"no site has this name; a request with this Host falls to the default vhost and is rejected"))
}

// routeExplainHost là thân của RouteExplainHost (types_m19.go gọi hàm này):
// host là tên chính hay tên phụ đang active của một site nào đó trên máy.
func (s *Sites) routeExplainHost(ctx context.Context, host validate.Domain) (RouteTrace, error) {
	nm, err := s.routeName(ctx, host)
	if err != nil {
		return RouteTrace{}, err
	}
	return s.routeExplainName(ctx, nm)
}

// RouteExplainName như routeExplainHost nhưng tên phải thuộc site d, không thì
// name_unknown (API `GET /sites/{domain}/route?host=`: người gọi chỉ có quyền
// trên d, nên tên của site khác phải trả y như tên không có, không lộ gì).
func (s *Sites) RouteExplainName(ctx context.Context, d, host validate.Domain) (RouteTrace, error) {
	nm, err := s.routeName(ctx, host)
	if err != nil {
		return RouteTrace{}, err
	}
	if nm.Site != d {
		return RouteTrace{}, nameUnknown()
	}
	return s.routeExplainName(ctx, nm)
}

// routeName tra host về dòng tên của nó. Tên phụ còn pending (job thêm tên
// chưa xong) chưa phục vụ request nào nên coi như không có.
func (s *Sites) routeName(ctx context.Context, host validate.Domain) (site.Name, error) {
	if ns, ok := s.nameStore(); ok {
		nm, found, err := ns.NameSite(ctx, host)
		if err != nil {
			return site.Name{}, err
		}
		if found && (nm.Kind == site.KindPrimary || nm.State == site.StateActive) {
			return nm, nil
		}
		if found {
			return site.Name{}, nameUnknown()
		}
	}
	// Chưa có bảng tên (store cũ) hoặc site có từ trước migration: tên chính
	// luôn suy được từ bảng sites.
	if _, ok, err := s.Store.Site(ctx, host); err != nil {
		return site.Name{}, err
	} else if ok {
		return site.Name{Name: host, Site: host, Kind: site.KindPrimary, Mode: site.ModeServe, State: site.StateActive}, nil
	}
	return site.Name{}, nameUnknown()
}

func (s *Sites) routeExplainName(ctx context.Context, nm site.Name) (RouteTrace, error) {
	st, err := s.find(ctx, nm.Site)
	if err != nil {
		return RouteTrace{}, err
	}
	if nm.Kind == site.KindPrimary {
		return s.routeTrace(ctx, st, nil)
	}
	return s.routeTrace(ctx, st, &nm)
}

// ---------------------------------------------------------- gọi thật bằng Host

// Nhãn của -w, cùng khuôn với steps.NameProbe: đứng sau thân, mỗi nhãn sau một
// ký tự xuống dòng; tìm từ cuối lên nên thân do site đặt có chứa chuỗi giống
// nhãn cũng không làm lệch.
const (
	routeLocLabel  = "\nTPAN_LOCATION="
	routeXRBLabel  = "\nTPAN_REDIRECT_BY="
	routeCodeLabel = "\nTPAN_HTTP_CODE="
	// routeBodyMax chặn phần thân giữ lại để tìm dấu hiệu WordPress.
	routeBodyMax = 64 << 10
	routeHdrMax  = 2 << 10
)

// routeAnswer là câu trả lời đã tách của một lần curl.
type routeAnswer struct {
	code, location, redirectBy, body string
}

func routeSplitAnswer(out string) routeAnswer {
	cut := func(label string) (string, bool) {
		i := strings.LastIndex(out, label)
		if i < 0 {
			return "", false
		}
		v := out[i+len(label):]
		out = out[:i]
		return strings.TrimRight(v, "\r"), true
	}
	code, ok1 := cut(routeCodeLabel)
	xrb, ok2 := cut(routeXRBLabel)
	loc, ok3 := cut(routeLocLabel)
	if len(out) > routeBodyMax {
		out = out[:routeBodyMax]
	}
	if !ok1 || !ok2 || !ok3 {
		return routeAnswer{body: out}
	}
	return routeAnswer{code: strings.TrimSpace(code), location: routeHeader(loc), redirectBy: routeHeader(xrb), body: out}
}

// routeHeader cắt giá trị header và bỏ ký tự điều khiển (vào bằng chứng, in ra
// terminal và giao diện).
func routeHeader(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > routeHdrMax {
		s = s[:routeHdrMax]
	}
	return strings.Map(func(r rune) rune {
		if r < 0x20 || (r >= 0x7f && r < 0xa0) {
			return -1
		}
		return r
	}, s)
}

// routeHostCmd gọi <scheme>://<host>/ về 127.0.0.1, không theo chuyển hướng.
// Cùng lệnh với steps.NameProbe nên người đọc thấy cùng một phép thử ở cả hai nơi.
func routeHostCmd(host validate.Domain, https bool) system.Cmd {
	scheme, port := "http", "80"
	args := []string{"-sS", "--max-time", "15"}
	if https {
		scheme, port = "https", "443"
		args = append(args, "-k") // chứng chỉ có thể là của máy thử; ở đây chỉ hỏi tên có tới đúng site không
	}
	args = append(args, "--resolve", host.String()+":"+port+":127.0.0.1", "-r", "0-65535",
		"-w", `\nTPAN_LOCATION=%header{location}\nTPAN_REDIRECT_BY=%header{x-redirect-by}\nTPAN_HTTP_CODE=%{http_code}`,
		scheme+"://"+host.String()+"/")
	return system.Cmd{Name: "curl", Args: args}
}

// routeHostCall chạy routeHostCmd. Output của bằng chứng là điều đã thấy (mã,
// Location, X-Redirect-By), không phải cả trang. hop khác nil khi lệnh không
// chạy được: đó là chặng Unknown dựng sẵn, người gọi trả luôn.
func routeHostCall(ctx context.Context, sys system.System, key string, host validate.Domain, https bool) (a routeAnswer, res system.Result, ev checks.Evidence, hop *RouteHop) {
	res, ev, err := routeRun(ctx, sys, routeHostCmd(host, https))
	if err != nil {
		h := routeHop(key, checks.Unknown, "", i18n.T(
			"không chạy được curl: "+err.Error(), "could not run curl: "+err.Error()), ev)
		return a, res, ev, &h
	}
	a = routeSplitAnswer(res.Stdout)
	var out []string
	if a.code != "" {
		out = append(out, "HTTP "+a.code)
	}
	if a.location != "" {
		out = append(out, "Location: "+a.location)
	}
	if a.redirectBy != "" {
		out = append(out, "X-Redirect-By: "+a.redirectBy)
	}
	if res.ExitCode != 0 {
		out = append(out, routeTail(res.Stderr))
	}
	ev.Output = strings.Join(out, "\n")
	return a, res, ev, nil
}

// routeCurlFail dựng chặng Fail cho curl thoát khác 0. Mã 52 và 56 là web
// server đóng kết nối không trả gì (nginx 444 của vhost mặc định).
func routeCurlFail(key string, host validate.Domain, https bool, res system.Result, ev checks.Evidence) RouteHop {
	port := "80"
	if https {
		port = "443"
	}
	code := strconv.Itoa(res.ExitCode)
	switch res.ExitCode {
	case 52, 56:
		return routeHop(key, checks.Fail, "", i18n.T(
			"Web server đóng kết nối không trả gì cho "+host.String()+" (cổng "+port+"): request rơi vào vhost mặc định, không tới site",
			"The web server closed the connection without replying for "+host.String()+" (port "+port+"): the request fell to the default vhost, not to the site"), ev)
	case 7, 28:
		return routeHop(key, checks.Fail, "", i18n.T(
			"Không gọi được 127.0.0.1 cổng "+port+" (curl mã "+code+"): web server không chạy, không nghe cổng đó hoặc không trả lời kịp",
			"Could not call 127.0.0.1 port "+port+" (curl exit "+code+"): the web server is down, not listening there or too slow"), ev)
	}
	return routeHop(key, checks.Unknown, "", i18n.T(
		"curl báo lỗi (mã "+code+")", "curl failed (exit "+code+")"), ev)
}

// routeRedirectHop gọi thật vào tên phụ kiểu redirect và đòi 301 về tên chính
// do vhost trả (không phải WordPress). Site có SSL mà tên chưa có trong chứng
// chỉ (https=false) thì request ở cổng 80 chuyển thẳng về https://<tên chính>/
// (bản của vhost cổng 80; tên chưa phủ không bao giờ nằm ở 443).
func routeRedirectHop(ctx context.Context, sys system.System, primary, host validate.Domain, siteSSL, https bool) RouteHop {
	want := "http://" + primary.String() + "/"
	if siteSSL {
		want = "https://" + primary.String() + "/"
	}
	a, res, ev, bad := routeHostCall(ctx, sys, HopRedirect, host, https)
	if bad != nil {
		return *bad
	}
	if res.ExitCode != 0 {
		return routeCurlFail(HopRedirect, host, https, res, ev)
	}
	url := routeURL(host, https)
	switch {
	case a.code == "":
		return routeHop(HopRedirect, checks.Unknown, "", i18n.T(
			"curl không in mã trạng thái", "curl printed no status code"), ev)
	case a.code != "301":
		return routeHop(HopRedirect, checks.Fail, a.code, i18n.T(
			url+" trả "+a.code+", đáng lẽ 301 về "+want,
			url+" answered "+a.code+", it should be 301 to "+want), ev)
	case a.location != want:
		return routeHop(HopRedirect, checks.Fail, "301 "+a.location, i18n.T(
			url+" chuyển (301) về "+a.location+", không phải tên chính "+want,
			url+" redirects (301) to "+a.location+", not to the primary name "+want), ev)
	case a.redirectBy != "":
		return routeHop(HopRedirect, checks.Fail, "301 "+a.location, i18n.T(
			url+" do trang của site chuyển ("+a.redirectBy+"), không phải khối chuyển tên phụ của vhost: tên này đang được phục vụ chứ không chuyển",
			url+" is redirected by the site itself ("+a.redirectBy+"), not by the vhost's alias redirect block: the name is being served, not redirected"), ev)
	}
	return routeHop(HopRedirect, checks.Pass, "301 "+a.location, i18n.T(
		url+" trả 301 về "+want+". Request dừng ở đây; đi tiếp: tpan route explain "+primary.String(),
		url+" answers 301 to "+want+". The request stops here; to follow it: tpan route explain "+primary.String()), ev)
}

// routeAliasHTTPHop là chặng http của tên phụ kiểu serve: gọi bằng Host của
// tên đó. Đạt khi 200 (hoặc 206) với trang WordPress. KHÔNG đòi trang nhắc
// tên phụ: WordPress đặt địa chỉ theo tên chính. 301/302 kèm X-Redirect-By:
// WordPress cũng đạt: request đã tới PHP của site, WordPress tự chuyển về tên
// chính hay www ⇄ tên trần (canonical).
func routeAliasHTTPHop(ctx context.Context, sys system.System, host validate.Domain, https bool) RouteHop {
	a, res, ev, bad := routeHostCall(ctx, sys, HopHTTP, host, https)
	if bad != nil {
		return *bad
	}
	if res.ExitCode != 0 {
		return routeCurlFail(HopHTTP, host, https, res, ev)
	}
	url := routeURL(host, https)
	switch a.code {
	case "":
		return routeHop(HopHTTP, checks.Unknown, url, i18n.T("curl không in mã trạng thái", "curl printed no status code"), ev)
	case "200", "206":
		low := strings.ToLower(a.body)
		for _, sign := range []string{"wp-content", "wp-includes", `content="wordpress`} {
			if strings.Contains(low, sign) {
				return routeHop(HopHTTP, checks.Pass, url, i18n.T(
					url+" trả "+a.code+", đúng là trang WordPress của site", url+" answers "+a.code+" with the site's WordPress page"), ev)
			}
		}
		return routeHop(HopHTTP, checks.Fail, url, i18n.T(
			url+" trả "+a.code+" nhưng trang không phải WordPress: vhost khác hay trang mặc định đang trả lời cho tên này",
			url+" answers "+a.code+" but the page is not WordPress: another vhost or the default page answers for this name"), ev)
	case "301", "302":
		if strings.EqualFold(a.redirectBy, "wordpress") {
			return routeHop(HopHTTP, checks.Pass, url, i18n.T(
				url+" trả "+a.code+" về "+a.location+" do WordPress chuyển hướng (canonical): request đã tới PHP của site",
				url+" answers "+a.code+" to "+a.location+" through WordPress canonical redirect: the request reached the site's PHP"), ev)
		}
		return routeHop(HopHTTP, checks.Fail, url, i18n.T(
			url+" trả "+a.code+" về "+a.location+" mà không phải do WordPress",
			url+" answers "+a.code+" to "+a.location+" and it is not WordPress doing it"), ev)
	}
	return routeHop(HopHTTP, checks.Fail, url, i18n.T(
		url+" trả "+a.code+", đáng lẽ 200", url+" answered "+a.code+", it should be 200"), ev)
}
