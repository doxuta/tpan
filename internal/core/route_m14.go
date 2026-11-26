package core

// M14 (mục 6.13 của docs/KE-HOACH.md): "request tới tên miền này đi đường
// nào". Hợp đồng ghi ở types_m14.go.
//
// Mọi chặng đọc từ cấu hình ĐANG NẠP, không từ điều tpan nghĩ nó đã ghi:
// vhost lấy từ `nginx -T` hoặc `apache2ctl -S`, rồi root và socket đọc từ
// chính file vhost đó, pool là file pool nào thật sự nghe socket đó. Nhờ vậy
// một file ai đó thêm tay, đứng trước vhost của site, hiện ra ngay ở chặng
// vhost thay vì bị che bởi "file của tpan vẫn đúng".
//
// Phần đọc output của hai lệnh trên có sẵn trong internal/checks/m14.go
// (nginxDecls, apacheDecls) nhưng không xuất, và ở đó chỉ trả tên, cổng, dòng.
// Ở đây cần thêm root và fastcgi_pass của đúng khối server, nên file này có
// phần đọc tối thiểu của riêng nó.

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// routeTitles là tên của từng chặng.
var routeTitles = map[string]i18n.Text{
	HopDNS:         i18n.T("Tên miền trỏ về máy này", "The domain points to this machine"),
	HopVhost:       i18n.T("Vhost nhận tên miền", "Vhost that receives the domain"),
	HopRoot:        i18n.T("Thư mục gốc của vhost", "Document root of the vhost"),
	HopSocket:      i18n.T("Socket mà vhost chuyển .php tới", "Socket the vhost sends .php to"),
	HopPool:        i18n.T("Pool PHP-FPM nghe socket đó", "PHP-FPM pool listening on that socket"),
	HopUser:        i18n.T("User Linux mà pool chạy", "Linux user the pool runs as"),
	HopOpenBasedir: i18n.T("Thư mục PHP của pool được mở (open_basedir)", "Directories PHP of the pool may open (open_basedir)"),
	HopHTTP:        i18n.T("Gọi thật vào site trên chính máy", "A real request to the site on this machine"),
	// M19: tên phụ kiểu redirect dừng ở đây, không có root, socket, pool.
	HopRedirect: i18n.T("Web server chuyển tên này về tên chính (301)", "The web server redirects this name to the primary name (301)"),
}

func routeHop(key string, st checks.Status, value string, summary i18n.Text, ev ...checks.Evidence) RouteHop {
	if ev == nil {
		ev = []checks.Evidence{} // JSON ra [] chứ không null: giao diện lặp thẳng
	}
	return RouteHop{Key: key, Title: routeTitles[key], Value: value, Status: st, Summary: summary, Evidence: ev}
}

// routeBlocked là chặng không lần được vì chặng prev đứng trước không cho ra gì.
func routeBlocked(key, prev string) RouteHop {
	return routeHop(key, checks.Unknown, "", i18n.T(
		"Chưa lần được: chặng \""+routeTitles[prev].Vi+"\" ở trên không cho ra kết quả để đi tiếp",
		"Could not be traced: the hop \""+routeTitles[prev].En+"\" above gave nothing to follow"))
}

// routeFromCheck đổi kết quả một phép kiểm có sẵn thành một chặng.
func routeFromCheck(key, value string, r checks.Result) RouteHop {
	return routeHop(key, r.Status, value, r.Summary, r.Evidence...)
}

// RouteExplain: xem types_m14.go.
func (s *Sites) RouteExplain(ctx context.Context, d validate.Domain) (RouteTrace, error) {
	st, err := s.find(ctx, d)
	if err != nil {
		return RouteTrace{}, err
	}
	return s.routeTrace(ctx, st, nil)
}

// routeTrace lần đường đi tới một tên của site st: alias nil là tên chính (như
// M14), khác nil là tên phụ (M19, route_m19.go). Các chặng giống nhau; chỗ khác
// của tên phụ: DNS và vhost tìm theo tên đó, cổng 443 chỉ khi site có SSL VÀ
// tên có trong chứng chỉ, tên redirect dừng ở chặng HopRedirect, tên serve gọi
// HTTP bằng Host của tên đó.
func (s *Sites) routeTrace(ctx context.Context, st site.Site, alias *site.Name) (RouteTrace, error) {
	d := st.Domain
	if st.Status != site.Active {
		return RouteTrace{}, refuse("site_trashed", i18n.T(
			"site "+d.String()+" đang ở thùng rác nên không có request nào đi tới nó; khôi phục bằng: sudo tpan site restore "+d.String(),
			"site "+d.String()+" is in the trash, so no request reaches it; restore it with: sudo tpan site restore "+d.String()))
	}
	cfg, err := s.stack(ctx)
	if err != nil {
		return RouteTrace{}, err
	}
	l := st.Layout()
	host, ssl := d, st.SSL
	tr := RouteTrace{Domain: d.String(), Host: d.String(), Kind: string(site.KindPrimary), Web: cfg.Web}
	if alias != nil {
		host, ssl = alias.Name, st.SSL && alias.InCert
		tr.Host, tr.Kind, tr.Mode = host.String(), string(alias.Kind), string(alias.Mode)
	}
	hl := site.LayoutOf(host) // chỉ để đặt tên miền cho các phép kiểm có sẵn

	// DNS: Fail hạ xuống Warn như health (bản ghi nằm ở nhà cung cấp tên miền).
	dns := checks.Soft(checks.DNSPointsHere(hl)).Run(ctx, s.Sys)
	tr.Hops = append(tr.Hops, routeFromCheck(HopDNS, routeDNSValue(dns), dns))

	port := "80"
	if ssl {
		port = "443" // site có SSL: cổng 80 chỉ chuyển hướng, site chạy ở 443
	}
	vh, vhop := routeVhostHop(ctx, s.Sys, cfg.Web, l, host.String(), port)

	if st.Suspended != account.NotSuspended {
		// Vhost tạm khoá không trỏ tới mã nguồn hay pool nào: đường đi dừng ở đây.
		vhop.Summary = i18n.T(
			vhop.Summary.Vi+". Site đang tạm khoá: vhost trả 403 kèm trang tạm khoá cho mọi đường dẫn, không chuyển request tới PHP",
			vhop.Summary.En+". The site is suspended: the vhost answers 403 with the suspension page for every path and sends nothing to PHP")
		tr.Hops = append(tr.Hops, vhop,
			routeFromCheck(HopHTTP, routeURL(host, ssl), checks.HTTPSuspended(hl, ssl).Run(ctx, s.Sys)))
		return tr, nil
	}
	tr.Hops = append(tr.Hops, vhop)

	if alias != nil && alias.Mode == site.ModeRedirect {
		tr.Hops = append(tr.Hops, routeRedirectHop(ctx, s.Sys, d, host, st.SSL, ssl))
		return tr, nil
	}

	php := sitePHP(st, cfg)
	// Site trong lồng (M17): socket, pool và user đều đến từ file của lồng chứ
	// không từ pool.d của bản PHP chung.
	wantSocket := l.SocketFor(php)
	if st.Caged {
		wantSocket = l.CageSocket()
	}
	rootHop, sockHop, socket := routeTargets(s.Sys, cfg.Web, vh, l, host.String(), port, wantSocket)
	tr.Hops = append(tr.Hops, rootHop, sockHop)
	tr.Hops = append(tr.Hops, routePoolHops(s.Sys, socket, l, php, st.Caged)...)

	var probe checks.Check
	switch {
	case st.DBSuspended != account.NotSuspended:
		// DB đang khoá thì WordPress báo lỗi kết nối DB: điều đã biết, như health.
		probe = checks.SuspendedStage(checks.StageEndToEnd, hl, true)
	case alias != nil:
		tr.Hops = append(tr.Hops, routeAliasHTTPHop(ctx, s.Sys, host, ssl))
		return tr, nil
	default:
		probe = siteHTTPCheck(st)
	}
	tr.Hops = append(tr.Hops, routeFromCheck(HopHTTP, routeURL(host, ssl), probe.Run(ctx, s.Sys)))
	return tr, nil
}

func routeURL(host validate.Domain, ssl bool) string {
	if ssl {
		return "https://" + host.String() + "/"
	}
	return "http://" + host.String() + "/"
}

// routeDNSValue lấy các địa chỉ mà tên miền đang trỏ tới từ bằng chứng của
// phép kiểm DNS (output của getent: cột đầu mỗi dòng là một địa chỉ).
func routeDNSValue(r checks.Result) string {
	var addrs []string
	for _, ev := range r.Evidence {
		if !strings.HasPrefix(ev.Command, "getent ") {
			continue
		}
		for _, ln := range strings.Split(ev.Output, "\n") {
			f := strings.Fields(ln)
			if len(f) == 0 {
				continue
			}
			if _, err := netip.ParseAddr(f[0]); err == nil && !slices.Contains(addrs, f[0]) {
				addrs = append(addrs, f[0])
			}
		}
	}
	return strings.Join(addrs, ", ")
}

// routeRun chạy một lệnh chỉ đọc và gom bằng chứng. Mã thoát khác 0 không
// phải lỗi (như checks.execCmd); err khác nil chỉ khi lệnh không chạy được.
func routeRun(ctx context.Context, sys system.System, cmd system.Cmd) (system.Result, checks.Evidence, error) {
	res, err := sys.Run(ctx, cmd)
	ev := checks.Evidence{Command: cmd.String(), ExitCode: res.ExitCode}
	var ee *system.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
		res.ExitCode, ev.ExitCode = ee.Code, ee.Code
		err = nil
	default:
		ev.ExitCode, ev.Output = -1, err.Error()
	}
	return res, ev, err
}

// ------------------------------------------------------------- chặng vhost

// routeDecl là một chỗ khai tên miền trong cấu hình web server đang nạp, theo
// đúng thứ tự web server nạp: chỗ khai đứng trước là chỗ nhận request.
type routeDecl struct {
	name, port string
	file       string
	line       int // nginx: dòng server_name; Apache: dòng <VirtualHost>
}

func (d routeDecl) where() string { return d.file + ":" + strconv.Itoa(d.line) }

// routeMaxLines chặn số dòng bằng chứng của một chặng: bản in cấu hình của
// một máy nhiều site dài cả nghìn dòng, người đọc chỉ cần các dòng liên quan.
const routeMaxLines = 20

// routeVhostHop tìm vhost đầu tiên mang tên name (tên chính của l, hoặc một
// tên phụ của site, M19) trên cổng port; file của site là file trong l.
// Trả nil khi không tìm được vhost nào (chặng sau không có gì để lần).
func routeVhostHop(ctx context.Context, sys system.System, web WebServer, l site.Layout, name, port string) (*routeDecl, RouteHop) {
	dom := name
	var cmd system.Cmd
	var own []string
	switch web {
	case Nginx:
		cmd, own = system.Cmd{Name: "nginx", Args: []string{"-T"}}, []string{l.NginxConf, l.NginxLink}
	case Apache:
		cmd, own = system.Cmd{Name: "apache2ctl", Args: []string{"-S"}}, []string{l.ApacheConf, l.ApacheLink}
	default:
		return nil, routeHop(HopVhost, checks.Unknown, "", i18n.T(
			"web server phải là nginx hoặc apache (nhận được \""+string(web)+"\")",
			"web server must be nginx or apache (got \""+string(web)+"\")"))
	}
	res, ev, err := routeRun(ctx, sys, cmd)
	if err != nil {
		return nil, routeHop(HopVhost, checks.Unknown, "", i18n.T(
			"không chạy được "+cmd.String()+": "+err.Error(), "could not run "+cmd.String()+": "+err.Error()), ev)
	}
	if res.ExitCode != 0 {
		ev.Output = routeTail(res.Stdout + "\n" + res.Stderr)
		code := strconv.Itoa(res.ExitCode)
		return nil, routeHop(HopVhost, checks.Unknown, "", i18n.T(
			cmd.String()+" báo lỗi (mã "+code+") nên không đọc được cấu hình đang nạp",
			cmd.String()+" failed (exit "+code+"), so the loaded configuration cannot be read"), ev)
	}
	var decls []routeDecl
	var ok bool
	if web == Nginx {
		// Bản in cấu hình ở stdout; stderr chỉ có "syntax is ok" và cảnh báo.
		decls, ok = ngxDumpDecls(res.Stdout)
	} else {
		decls, ok = apacheDumpDecls(res.Stdout + "\n" + res.Stderr)
	}
	if !ok {
		ev.Output = routeTail(res.Stdout + "\n" + res.Stderr)
		return nil, routeHop(HopVhost, checks.Unknown, "", i18n.T(
			"không đọc được output của "+cmd.String()+" (định dạng lạ)",
			"could not read the output of "+cmd.String()+" (unexpected format)"), ev)
	}

	// Bằng chứng: mọi chỗ khai tên miền này, theo thứ tự nạp; kèm dòng cảnh báo
	// trùng tên của nginx nếu có.
	var shown []string
	var hit *routeDecl
	for i, d := range decls {
		if d.name != dom {
			continue
		}
		shown = append(shown, d.where()+": "+d.name+" (port "+d.port+")")
		if hit == nil && (d.port == port || d.port == "*") {
			hit = &decls[i]
		}
	}
	for _, ln := range strings.Split(res.Stderr, "\n") {
		if strings.Contains(ln, "conflicting server name") && strings.Contains(ln, dom) {
			shown = append(shown, strings.TrimSpace(ln))
		}
	}
	if len(shown) > routeMaxLines {
		shown = append(shown[:routeMaxLines], "...")
	}
	ev.Output = strings.Join(shown, "\n")

	if hit == nil {
		if ev.Output == "" {
			ev.Output = "(" + dom + " is not declared in the loaded configuration)"
		}
		return nil, routeHop(HopVhost, checks.Fail, "", i18n.T(
			"Không vhost nào đang nạp khai tên miền "+dom+" trên cổng "+port+": request rơi vào vhost mặc định, không tới site",
			"No loaded vhost declares "+dom+" on port "+port+": requests fall to the default vhost, not to the site"), ev)
	}
	if !slices.Contains(own, hit.file) {
		return hit, routeHop(HopVhost, checks.Fail, hit.where(), i18n.T(
			"Tên miền "+dom+" (cổng "+port+") đang do file lạ "+hit.where()+" nhận, không phải vhost của site ("+own[1]+"). Web server dùng vhost được nạp trước, nên các chặng dưới là của file lạ đó",
			"The domain "+dom+" (port "+port+") is received by the foreign file "+hit.where()+", not by the site's vhost ("+own[1]+"). The web server uses the vhost loaded first, so the hops below belong to that foreign file"), ev)
	}
	return hit, routeHop(HopVhost, checks.Pass, hit.where(), i18n.T(
		"Tên miền "+dom+" (cổng "+port+") do vhost của chính site nhận: "+hit.where(),
		"The domain "+dom+" (port "+port+") is received by the site's own vhost: "+hit.where()), ev)
}

// routeTail giữ vài dòng cuối không rỗng của output (dòng lỗi nằm ở cuối).
func routeTail(out string) string {
	var keep []string
	for _, ln := range strings.Split(out, "\n") {
		if strings.TrimSpace(ln) != "" {
			keep = append(keep, strings.TrimRight(ln, "\r"))
		}
	}
	if len(keep) > routeMaxLines {
		keep = keep[len(keep)-routeMaxLines:]
	}
	return strings.Join(keep, "\n")
}

// ---- nginx ----

// ngxFileRE là dòng `nginx -T` in trước nội dung từng file cấu hình.
var ngxFileRE = regexp.MustCompile(`^# configuration file (.+):$`)

// ngxStmt là một chỉ thị bên trong một khối server. depth 1 là nằm ngay
// trong khối server, lớn hơn là trong location lồng bên trong.
type ngxStmt struct {
	words []string
	line  int
	depth int
}

// ngxServer là một khối server của một file cấu hình nginx.
type ngxServer struct {
	stmts []ngxStmt
}

// names trả các tên ở server_name kèm dòng khai; ports trả các cổng ở listen
// (không có listen thì nginx nghe cổng 80).
func (s ngxServer) names() (out []routeDecl) {
	for _, st := range s.stmts {
		if st.depth == 1 && st.words[0] == "server_name" {
			for _, w := range st.words[1:] {
				out = append(out, routeDecl{name: strings.TrimSuffix(strings.ToLower(w), "."), line: st.line})
			}
		}
	}
	return out
}

func (s ngxServer) ports() (out []string) {
	for _, st := range s.stmts {
		if st.depth == 1 && st.words[0] == "listen" && len(st.words) > 1 {
			if p := ngxListenPort(st.words[1]); p != "" && !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	}
	if len(out) == 0 {
		return []string{"80"}
	}
	return out
}

// first trả chỉ thị đầu tiên mang tên name; serverOnly thì chỉ xét chỉ thị
// nằm ngay trong khối server.
func (s ngxServer) first(name string, serverOnly bool) (ngxStmt, bool) {
	for _, st := range s.stmts {
		if st.words[0] == name && len(st.words) > 1 && (!serverOnly || st.depth == 1) {
			return st, true
		}
	}
	return ngxStmt{}, false
}

// ngxListenPort lấy cổng từ tham số đầu của listen: "80", "[::]:80",
// "127.0.0.1:8080", "*:80". Chỉ có địa chỉ thì là cổng 80; socket unix thì "".
func ngxListenPort(arg string) string {
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

// ngxServers đọc một file cấu hình nginx và trả các khối server của nó, theo
// thứ tự trong file. Khối server được tính khi nằm ở cấp ngoài cùng của file
// (file trong sites-enabled, conf.d) hoặc ngay trong khối http.
//
// Luật tách từ là của nginx: "#" ở đầu một từ mở chú thích tới hết dòng; chuỗi
// trong nháy là một từ; ";" và "{" kết thúc từ đang đọc (trừ "${" của biến);
// "}" chỉ đóng khối khi đứng đầu một từ. Cùng luật với checks.nginxTokens.
//
// ponytail: không lần theo include bên trong một khối server (root hay
// fastcgi_pass nằm ở file con thì chặng đó báo "không có dòng"). Vhost của
// tpan không dùng include kiểu đó; cần thì ghép nội dung file con vào đây.
func ngxServers(lines []string) []ngxServer {
	var out []ngxServer
	var cur *ngxServer
	var stack []string // tên các khối đang mở
	var stmt []string  // chỉ thị đang đọc
	stmtLine, depth := 0, 0
	var word strings.Builder
	var quote byte
	inWord := false

	endWord := func(n int) {
		if !inWord {
			return
		}
		if len(stmt) == 0 {
			stmtLine = n
		}
		stmt = append(stmt, word.String())
		word.Reset()
		inWord = false
	}
	punct := func(c byte) {
		switch c {
		case ';':
			if cur != nil && len(stmt) > 0 {
				cur.stmts = append(cur.stmts, ngxStmt{words: stmt, line: stmtLine, depth: len(stack) - depth})
			}
		case '{':
			name := ""
			if len(stmt) > 0 {
				name = stmt[0]
			}
			if cur == nil && name == "server" && len(stmt) == 1 && (len(stack) == 0 || stack[len(stack)-1] == "http") {
				cur, depth = &ngxServer{}, len(stack)
			}
			stack = append(stack, name)
		case '}':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			if cur != nil && len(stack) == depth {
				out = append(out, *cur)
				cur = nil
			}
		}
		stmt = nil
	}

	for n, ln := range lines {
		ln = strings.TrimRight(ln, "\r")
	scan:
		for i := 0; i < len(ln); i++ {
			c := ln[i]
			switch {
			case quote != 0:
				switch {
				case c == '\\' && i+1 < len(ln):
					i++
					word.WriteByte(ln[i])
				case c == quote:
					quote = 0
					endWord(n + 1)
				default:
					word.WriteByte(c)
				}
			case c == ' ' || c == '\t':
				endWord(n + 1)
			case !inWord && c == '#':
				break scan // chú thích tới hết dòng
			case !inWord && (c == '"' || c == '\''):
				quote, inWord = c, true
			case c == ';' || (c == '{' && !strings.HasSuffix(word.String(), "$")) || (!inWord && c == '}'):
				endWord(n + 1)
				punct(c)
			default:
				inWord = true
				word.WriteByte(c)
			}
		}
		if quote != 0 {
			word.WriteByte('\n') // chuỗi trong nháy kéo sang dòng sau
		} else {
			endWord(n + 1)
		}
	}
	return out
}

// ngxDumpDecls đọc bản in của `nginx -T`: mọi cặp (tên, cổng) của từng khối
// server, theo thứ tự nginx nạp. ok=false khi không thấy dòng mở file nào.
func ngxDumpDecls(dump string) (decls []routeDecl, ok bool) {
	var file string
	var body []string
	flush := func() {
		if file != "" {
			for _, srv := range ngxServers(body) {
				for _, nm := range srv.names() {
					for _, p := range srv.ports() {
						decls = append(decls, routeDecl{name: nm.name, port: p, file: file, line: nm.line})
					}
				}
			}
		}
		body = nil
	}
	for _, ln := range strings.Split(dump, "\n") {
		if m := ngxFileRE.FindStringSubmatch(strings.TrimRight(ln, "\r")); m != nil {
			flush()
			file, ok = m[1], true
			continue
		}
		body = append(body, ln)
	}
	flush()
	return decls, ok
}

// ---- Apache ----

// Các dòng của `apache2ctl -S` (Apache 2.4): một địa chỉ có nhiều vhost thì in
// từng dòng "port <n> namevhost <tên> (<file>:<dòng>)", dưới mỗi dòng là các
// "alias <tên>"; địa chỉ chỉ có một vhost thì in gọn "<địa chỉ>:<cổng> <tên>
// (<file>:<dòng>)". Cùng định dạng mà checks.apacheDecls đọc.
var (
	apNameVhostRE = regexp.MustCompile(`^\s+port (\S+) namevhost (\S+) \((.+):([0-9]+)\)$`)
	apAliasRE     = regexp.MustCompile(`^\s+alias (\S+)$`)
	apSingleRE    = regexp.MustCompile(`^\S+:([0-9]+|\*)\s+(\S+) \((.+):([0-9]+)\)$`)
)

// apacheDumpDecls đọc output của `apache2ctl -S`, theo thứ tự Apache xét.
// ok=false khi không thấy dòng "VirtualHost configuration:".
func apacheDumpDecls(out string) (decls []routeDecl, ok bool) {
	var last routeDecl // vhost mà các dòng alias kế tiếp thuộc về; file rỗng là chưa có
	for _, ln := range strings.Split(out, "\n") {
		ln = strings.TrimRight(ln, "\r")
		if strings.HasPrefix(ln, "VirtualHost configuration:") {
			ok = true
			continue
		}
		m := apNameVhostRE.FindStringSubmatch(ln)
		if m == nil {
			m = apSingleRE.FindStringSubmatch(ln)
		}
		if m != nil {
			n, _ := strconv.Atoi(m[4])
			last = routeDecl{name: strings.TrimSuffix(strings.ToLower(m[2]), "."), port: m[1], file: m[3], line: n}
			decls = append(decls, last)
		} else if a := apAliasRE.FindStringSubmatch(ln); a != nil && last.file != "" {
			d := last
			d.name = strings.TrimSuffix(strings.ToLower(a[1]), ".")
			decls = append(decls, d)
		}
	}
	return decls, ok
}

// ---------------------------------------------------- chặng root và socket

// routeLines là bằng chứng "đọc file": đúng các dòng liên quan kèm số dòng.
func routeLines(file string, lines []string, nums ...int) checks.Evidence {
	var out []string
	for _, n := range nums {
		if n >= 1 && n <= len(lines) {
			out = append(out, strconv.Itoa(n)+": "+strings.TrimSpace(strings.TrimRight(lines[n-1], "\r")))
		}
	}
	return checks.Evidence{Command: "read " + file + " (tpan)", Output: strings.Join(out, "\n")}
}

func routeReadErr(key, file string, err error) RouteHop {
	return routeHop(key, checks.Unknown, "", i18n.T(
		"không đọc được "+file+": "+err.Error(), "could not read "+file+": "+err.Error()),
		checks.Evidence{Command: "read " + file + " (tpan)", ExitCode: 1, Output: err.Error()})
}

// routeTargets đọc chính file vhost đã nhận tên miền và trả hai chặng root,
// socket cùng đường dẫn socket tìm được (rỗng khi không có).
func routeTargets(sys system.System, web WebServer, vh *routeDecl, l site.Layout, name, port, wantSocket string) (rootHop, sockHop RouteHop, socket string) {
	if vh == nil {
		return routeBlocked(HopRoot, HopVhost), routeBlocked(HopSocket, HopVhost), ""
	}
	data, err := sys.ReadFile(vh.file)
	if err != nil {
		return routeReadErr(HopRoot, vh.file, err), routeReadErr(HopSocket, vh.file, err), ""
	}
	lines := strings.Split(string(data), "\n")

	var root, pass string // pass: đích mà vhost chuyển .php tới, nguyên văn
	var rootLine, passLine int
	found := false
	if web == Nginx {
		for _, srv := range ngxServers(lines) {
			if !slices.ContainsFunc(srv.names(), func(d routeDecl) bool { return d.name == name }) || !slices.Contains(srv.ports(), port) {
				continue
			}
			found = true
			if st, ok := srv.first("root", true); ok {
				root, rootLine = st.words[1], st.line
			}
			if st, ok := srv.first("fastcgi_pass", false); ok {
				pass, passLine = st.words[1], st.line
			}
			break
		}
	} else {
		root, rootLine, pass, passLine, found = apacheVhostTargets(lines, vh.line)
	}
	if !found {
		why := i18n.T(
			"không tìm lại được khối vhost ở "+vh.where()+" (file vừa đổi sau khi web server nạp?)",
			"could not find the vhost block at "+vh.where()+" again (was the file changed after the web server loaded it?)")
		ev := routeLines(vh.file, lines, vh.line)
		return routeHop(HopRoot, checks.Unknown, "", why, ev), routeHop(HopSocket, checks.Unknown, "", why, ev), ""
	}

	switch {
	case root == "":
		rootHop = routeHop(HopRoot, checks.Fail, "", i18n.T(
			"Vhost ở "+vh.where()+" không khai thư mục gốc nào, nên không phục vụ file của site ("+l.Public+")",
			"The vhost at "+vh.where()+" declares no document root, so it does not serve the site's files ("+l.Public+")"),
			routeLines(vh.file, lines, vh.line))
	case path.Clean(root) != l.Public:
		rootHop = routeHop(HopRoot, checks.Fail, root, i18n.T(
			"Vhost phục vụ file từ "+root+", không phải thư mục của site ("+l.Public+")",
			"The vhost serves files from "+root+", not from the site's directory ("+l.Public+")"),
			routeLines(vh.file, lines, rootLine))
	default:
		rootHop = routeHop(HopRoot, checks.Pass, root, i18n.T(
			"Vhost phục vụ file từ đúng thư mục của site", "The vhost serves files from the site's own directory"),
			routeLines(vh.file, lines, rootLine))
	}

	socket, isUnix := strings.CutPrefix(pass, "unix:")
	switch {
	case pass == "":
		sockHop = routeHop(HopSocket, checks.Fail, "", i18n.T(
			"Vhost ở "+vh.where()+" không chuyển .php tới socket nào: PHP của site không chạy qua vhost này",
			"The vhost at "+vh.where()+" sends .php to no socket: the site's PHP does not run through this vhost"),
			routeLines(vh.file, lines, vh.line))
	case !isUnix:
		socket = ""
		sockHop = routeHop(HopSocket, checks.Fail, pass, i18n.T(
			"Vhost chuyển .php tới "+pass+", không phải socket của site ("+wantSocket+")",
			"The vhost sends .php to "+pass+", not to the site's socket ("+wantSocket+")"),
			routeLines(vh.file, lines, passLine))
	case socket != wantSocket:
		sockHop = routeHop(HopSocket, checks.Fail, socket, i18n.T(
			"Vhost chuyển .php tới "+socket+", không phải socket của site ("+wantSocket+")",
			"The vhost sends .php to "+socket+", not to the site's socket ("+wantSocket+")"),
			routeLines(vh.file, lines, passLine))
	default:
		sockHop = routeHop(HopSocket, checks.Pass, socket, i18n.T(
			"Vhost chuyển .php tới đúng socket của site", "The vhost sends .php to the site's own socket"),
			routeLines(vh.file, lines, passLine))
	}
	return rootHop, sockHop, socket
}

// apacheVhostTargets đọc khối <VirtualHost> mở ở dòng open (số dòng mà
// `apache2ctl -S` in) và trả DocumentRoot cùng đích của SetHandler
// "proxy:unix:<socket>|fcgi://..." dưới dạng "unix:<socket>" (cùng dạng với
// fastcgi_pass của nginx). found=false khi dòng open không mở một VirtualHost.
//
// ponytail: chỉ nhận dạng SetHandler proxy:unix (cách tpan và tài liệu Apache
// dùng cho PHP-FPM). ProxyPassMatch tới fcgi:// thì chặng socket báo "không
// chuyển .php tới socket nào"; cần thì đọc thêm chỉ thị đó ở vòng lặp dưới.
func apacheVhostTargets(lines []string, open int) (root string, rootLine int, pass string, passLine int, found bool) {
	if open < 1 || open > len(lines) || !strings.HasPrefix(strings.ToLower(strings.TrimSpace(lines[open-1])), "<virtualhost") {
		return "", 0, "", 0, false
	}
	for i := open; i < len(lines); i++ {
		f := strings.Fields(lines[i])
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		switch name := strings.ToLower(f[0]); {
		case strings.HasPrefix(name, "</virtualhost"):
			return root, rootLine, pass, passLine, true
		case name == "documentroot" && len(f) > 1 && root == "":
			root, rootLine = strings.Trim(f[1], `"'`), i+1
		case name == "sethandler" && len(f) > 1 && pass == "":
			if h, ok := strings.CutPrefix(strings.Trim(f[1], `"'`), "proxy:unix:"); ok {
				sock, _, _ := strings.Cut(h, "|")
				pass, passLine = "unix:"+sock, i+1
			}
		}
	}
	return root, rootLine, pass, passLine, true
}

// ------------------------------------------- chặng pool, user, open_basedir

// poolKey là một dòng "khoá = giá trị" của file pool, kèm số dòng.
type poolKey struct {
	value string
	line  int
}

// poolSection là một khối [tên pool] của file pool PHP-FPM.
type poolSection struct {
	file  string
	name  string
	line  int
	lines []string
	keys  map[string]poolKey
}

// poolSections đọc một file pool (cú pháp INI của PHP: chú thích bắt đầu bằng
// dấu chấm phẩy). Khoá khai nhiều lần thì lần sau thắng, như PHP-FPM.
func poolSections(file string, data []byte) []poolSection {
	lines := strings.Split(string(data), "\n")
	var out []poolSection
	for i, raw := range lines {
		ln := strings.TrimSpace(raw)
		if ln == "" || ln[0] == ';' || ln[0] == '#' {
			continue
		}
		if ln[0] == '[' && strings.HasSuffix(ln, "]") {
			out = append(out, poolSection{file: file, name: ln[1 : len(ln)-1], line: i + 1, lines: lines, keys: map[string]poolKey{}})
			continue
		}
		k, v, ok := strings.Cut(ln, "=")
		if !ok || len(out) == 0 {
			continue
		}
		out[len(out)-1].keys[strings.TrimSpace(k)] = poolKey{value: strings.Trim(strings.TrimSpace(v), `"'`), line: i + 1}
	}
	return out
}

// routePoolHops tìm pool đang nghe socket (mọi file trong
// /etc/php/*/fpm/pool.d) rồi đọc user và open_basedir của chính pool đó.
// Site trong lồng (caged): pool nằm ở php-fpm.conf của lồng, user ở dòng User=
// của file dịch vụ systemd; pool.d không có gì của site này nữa.
func routePoolHops(sys system.System, socket string, l site.Layout, php string, caged bool) []RouteHop {
	if socket == "" {
		return []RouteHop{routeBlocked(HopPool, HopSocket), routeBlocked(HopUser, HopSocket), routeBlocked(HopOpenBasedir, HopSocket)}
	}
	var (
		pool     *poolSection
		scan     checks.Evidence
		wantFile string
		early    []RouteHop
	)
	if caged {
		wantFile = l.CageFPMConf()
		pool, scan, early = routeCagePool(sys, socket, wantFile)
	} else {
		wantFile = l.PoolConf(php)
		pool, scan, early = routeScanPools(sys, socket)
	}
	if early != nil {
		return early
	}

	listen := pool.keys["listen"]
	scan.Output = fmt.Sprintf("%s:%d: %s\n%s:%d: %s", pool.file, pool.line, strings.TrimSpace(pool.lines[pool.line-1]),
		pool.file, listen.line, strings.TrimSpace(pool.lines[listen.line-1]))
	value := pool.file + " [" + pool.name + "]"
	poolHop := routeHop(HopPool, checks.Pass, value, i18n.T(
		"Socket do đúng pool của site nghe", "The socket is served by the site's own pool"), scan)
	if pool.file != wantFile || pool.name != l.Pool {
		poolHop = routeHop(HopPool, checks.Fail, value, i18n.T(
			"Socket do pool "+pool.name+" ("+pool.file+") nghe, không phải pool của site ("+l.Pool+", "+wantFile+")",
			"The socket is served by the pool "+pool.name+" ("+pool.file+"), not by the site's pool ("+l.Pool+", "+wantFile+")"), scan)
	}

	var userHop RouteHop
	switch u, ok := pool.keys["user"]; {
	case caged:
		userHop = routeCageUser(sys, l)
	case !ok:
		userHop = routeHop(HopUser, checks.Unknown, "", i18n.T(
			"Pool "+pool.name+" không có dòng user", "The pool "+pool.name+" has no user line"),
			routeLines(pool.file, pool.lines, pool.line))
	case u.value != l.User:
		userHop = routeHop(HopUser, checks.Fail, u.value, i18n.T(
			"Pool chạy dưới user "+u.value+", không phải user của site ("+l.User+"): mã PHP của site chạy bằng quyền của user khác",
			"The pool runs as "+u.value+", not as the site's user ("+l.User+"): the site's PHP code runs with another user's rights"),
			routeLines(pool.file, pool.lines, u.line))
	default:
		userHop = routeHop(HopUser, checks.Pass, u.value, i18n.T(
			"Pool chạy dưới đúng user riêng của site", "The pool runs as the site's own user"),
			routeLines(pool.file, pool.lines, u.line))
	}

	var baseHop RouteHop
	want := l.Dir + "/"
	switch ob, ok := pool.keys["php_admin_value[open_basedir]"]; {
	case !ok:
		baseHop = routeHop(HopOpenBasedir, checks.Fail, "", i18n.T(
			"Pool "+pool.name+" không đặt php_admin_value[open_basedir]: PHP của site mở được mọi file mà user của pool đọc được",
			"The pool "+pool.name+" sets no php_admin_value[open_basedir]: the site's PHP may open any file the pool user can read"),
			routeLines(pool.file, pool.lines, pool.line))
	case ob.value != want:
		baseHop = routeHop(HopOpenBasedir, checks.Fail, ob.value, i18n.T(
			"open_basedir của pool là "+ob.value+", không phải thư mục của site ("+want+")",
			"The pool's open_basedir is "+ob.value+", not the site's directory ("+want+")"),
			routeLines(pool.file, pool.lines, ob.line))
	default:
		baseHop = routeHop(HopOpenBasedir, checks.Pass, ob.value, i18n.T(
			"PHP của site chỉ mở được file trong thư mục của chính site", "The site's PHP may open files only inside the site's own directory"),
			routeLines(pool.file, pool.lines, ob.line))
	}
	return []RouteHop{poolHop, userHop, baseHop}
}

// routeScanPools tìm file pool nghe socket trong mọi pool.d của /etc/php.
// Trả pool, hoặc early (ba chặng đã dựng) khi không tìm được.
func routeScanPools(sys system.System, socket string) (pool *poolSection, scan checks.Evidence, early []RouteHop) {
	const glob = "/etc/php/*/fpm/pool.d/*.conf"
	scan = checks.Evidence{Command: "grep -n 'listen = " + socket + "' " + glob + " (tpan)"}
	vers, err := sys.ReadDir("/etc/php")
	if err != nil {
		scan.ExitCode, scan.Output = 1, err.Error()
		return nil, scan, []RouteHop{
			routeHop(HopPool, checks.Unknown, "", i18n.T("không đọc được /etc/php: "+err.Error(), "could not read /etc/php: "+err.Error()), scan),
			routeBlocked(HopUser, HopPool), routeBlocked(HopOpenBasedir, HopPool),
		}
	}
	for _, v := range vers {
		dir := "/etc/php/" + v + "/fpm/pool.d"
		names, err := sys.ReadDir(dir)
		if err != nil {
			continue // bản PHP không có FPM (chỉ CLI): không có pool.d
		}
		for _, n := range names {
			if !strings.HasSuffix(n, ".conf") {
				continue // PHP-FPM chỉ nạp *.conf; pool tạm khoá mang đuôi .suspended
			}
			data, err := sys.ReadFile(dir + "/" + n)
			if err != nil {
				continue
			}
			for _, sec := range poolSections(dir+"/"+n, data) {
				if sec.keys["listen"].value == socket {
					return &sec, scan, nil
				}
			}
		}
	}
	scan.ExitCode, scan.Output = 1, "(no pool file listens on "+socket+")"
	return nil, scan, []RouteHop{
		routeHop(HopPool, checks.Fail, "", i18n.T(
			"Không file pool nào trong "+glob+" nghe socket "+socket+": request PHP của site sẽ báo 502",
			"No pool file in "+glob+" listens on "+socket+": PHP requests of the site will get a 502"), scan),
		routeBlocked(HopUser, HopPool), routeBlocked(HopOpenBasedir, HopPool),
	}
}

// routeCagePool đọc php-fpm.conf của lồng (file duy nhất có thể chứa pool của
// site trong lồng: pool.d đã bị cage.enable dọn) và tìm khối nghe socket.
func routeCagePool(sys system.System, socket, file string) (pool *poolSection, scan checks.Evidence, early []RouteHop) {
	scan = checks.Evidence{Command: "grep -n 'listen = " + socket + "' " + file + " (tpan)"}
	data, err := sys.ReadFile(file)
	if err != nil {
		return nil, scan, []RouteHop{
			routeReadErr(HopPool, file, err),
			routeBlocked(HopUser, HopPool), routeBlocked(HopOpenBasedir, HopPool),
		}
	}
	for _, sec := range poolSections(file, data) {
		if sec.keys["listen"].value == socket {
			return &sec, scan, nil
		}
	}
	scan.ExitCode, scan.Output = 1, "(no pool in "+file+" listens on "+socket+")"
	return nil, scan, []RouteHop{
		routeHop(HopPool, checks.Fail, "", i18n.T(
			"Không khối pool nào trong "+file+" nghe socket "+socket+": request PHP của site sẽ báo 502",
			"No pool block in "+file+" listens on "+socket+": PHP requests of the site will get a 502"), scan),
		routeBlocked(HopUser, HopPool), routeBlocked(HopOpenBasedir, HopPool),
	}
}

// routeCageUser đọc dòng User= trong mục [Service] của unit dịch vụ PHP của
// lồng: FPM của lồng không có dòng user trong pool (không chạy bằng root nên
// không đổi user được); systemd là chỗ quyết định tiến trình chạy bằng ai.
// Khai nhiều lần thì lần sau thắng, như systemd.
func routeCageUser(sys system.System, l site.Layout) RouteHop {
	file := l.CageServiceFile()
	data, err := sys.ReadFile(file)
	if err != nil {
		return routeReadErr(HopUser, file, err)
	}
	lines := strings.Split(string(data), "\n")
	var user string
	line, inService := 0, false
	for i, raw := range lines {
		ln := strings.TrimSpace(raw)
		switch {
		case ln == "" || ln[0] == '#' || ln[0] == ';':
		case ln[0] == '[':
			inService = ln == "[Service]"
		case inService:
			if k, v, ok := strings.Cut(ln, "="); ok && strings.TrimSpace(k) == "User" {
				user, line = strings.TrimSpace(v), i+1
			}
		}
	}
	switch {
	case user == "":
		return routeHop(HopUser, checks.Fail, "", i18n.T(
			"Unit "+file+" không có dòng User= trong mục [Service]: dịch vụ PHP của lồng sẽ chạy bằng root",
			"The unit "+file+" has no User= line in its [Service] section: the cage's PHP service would run as root"),
			routeLines(file, lines))
	case user != l.User:
		return routeHop(HopUser, checks.Fail, user, i18n.T(
			"Dịch vụ PHP của lồng chạy dưới user "+user+", không phải user của site ("+l.User+")",
			"The cage's PHP service runs as "+user+", not as the site's user ("+l.User+")"),
			routeLines(file, lines, line))
	}
	return routeHop(HopUser, checks.Pass, user, i18n.T(
		"Dịch vụ PHP của lồng chạy dưới đúng user riêng của site", "The cage's PHP service runs as the site's own user"),
		routeLines(file, lines, line))
}
