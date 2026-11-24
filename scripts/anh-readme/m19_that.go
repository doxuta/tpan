package main

// Phần trình duyệt của e2e giao diện M19 (scripts/e2e-m19-gd.sh): Chrome không
// giao diện mở panel THẬT trên máy ảo test và làm như người dùng ở hai tab
// "Tên miền" và "Chuyển hướng" của trang một site.
//
//	TPAN_PANEL_PASSWORD=… go run ./scripts/anh-readme -bo m19-that -panel https://<máy ảo>:8443 -out <thư mục ảnh> \
//	    -buoc them-ten -site example.com
//	… -buoc chua-dat -site example.com
//	… -buoc them-luat -site example.com
//	TPAN_PANEL_PASSWORD=… TPAN_USER_PASSWORD=… … -buoc tu-choi -ten <user B> -site <site của B>
//
// Bốn bước (đánh số theo bảng 7.2 của docs/thay-doi/M19-thiet-ke.md):
//   - them-ten  E10: admin đăng nhập qua form, mở tab Tên miền của -site, gõ
//     example.net, chọn "Phục vụ cùng nội dung", Xem trước, chạy, chờ job
//     site.alias.add xong, thấy dòng example.net kiểu "phục vụ" trong bảng.
//   - tu-choi   E23: user B đăng nhập, mở tab Tên miền của site của B, gõ
//     www.example.com (tên của admin), Xem trước: thấy câu từ chối chung
//     (name_unavailable), không có bản xem trước, trang không nhắc tên site hay
//     chủ nào khác, danh sách tên của B không đổi.
//   - chua-dat  E41: admin mở tab Tên miền của -site (đã có tên nodns.example.invalid
//     từ lệnh trong máy ảo): dòng đó có DNS chưa đạt và "chưa" ở cột Chứng chỉ.
//     Chụp ảnh sáng và tối.
//   - them-luat: admin mở tab Chuyển hướng, gõ nguồn "/a b" (sai): lỗi hiện
//     ngay dưới ô nguồn, không có bản xem trước; sửa thành /cu, đích /moi, xem
//     trước, chạy, chờ job site.redirect.add, thấy dòng /cu → /moi mã 301.
//
// Mỗi khẳng định in "  => ĐẠT: …" hoặc "  => KHÔNG ĐẠT: …" (script e2e đếm).
// Ảnh chụp cả trang vào -out. Mật khẩu chỉ đi qua biến môi trường; chương
// trình không in mật khẩu, cookie hay địa chỉ panel. KHÔNG chạy với panel thật
// của máy chủ.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

// m19Nhan là chữ thật của tiêu đề, nút và câu báo mà các bộ M19 tìm, chép từ
// bản tiếng Việt của web/src/i18n/ten-mien.ts, ten-mien-ds.ts, cong-cu.ts.
// Tìm theo chữ đọc được (như m18Nhan) để ảnh và e2e hỏng ngay khi chữ đổi mà
// quên sửa ở đây. Ô nhập thì theo id (ổn định hơn nhãn).
var m19Nhan = struct {
	tenBang, themTen, luatBang, themLuat string // tiêu đề h2 của bốn thẻ
	kieuServe, dnsOk, chuaTrongCC        string // ô của bảng tên
	tuChoi                               string // câu từ chối chung của máy chủ (api_m19.go, nameRefusal)
	tabTenMien, tabChuyenHuong           string // nhãn hai tab (site.ts)
	oTenMienPhu, oChuyenHuong            string // hai ô của trang Công cụ (cong-cu.ts)
	domainsH1                            string // h1 của /domains (ten-mien-ds.ts)
}{
	tenBang: "Tên của site", themTen: "Thêm tên phụ", luatBang: "Luật chuyển hướng", themLuat: "Thêm luật chuyển hướng",
	kieuServe: "phục vụ", dnsOk: "đã trỏ về máy", chuaTrongCC: "chưa",
	tuChoi:     "Tên này không dùng được: nó đã thuộc một site khác, hoặc cùng tên miền gốc với site của người khác.",
	tabTenMien: "Tên miền", tabChuyenHuong: "Chuyển hướng",
	oTenMienPhu: "Tên miền phụ", oChuyenHuong: "Chuyển hướng",
	domainsH1: "Tên miền phụ",
}

// Tên dùng chung với scripts/e2e-m19-gd.sh và bảng tên của scripts/e2e-m19.sh.
const (
	m19TenMoi  = "example.net"           // tên phụ E10 thêm bằng giao diện
	m19TenChua = "nodns.example.invalid" // tên phụ không có DNS (E40, E41)
	m19TenCuaA = "www.example.com"       // tên của site admin, user B thử xin (E23)
)

// Phạm vi tìm trong trang một site (Card, TableList: section aria-labelledby id của h2).
const (
	m19BangTen  = `section[aria-labelledby="ten-mien-bang"]`
	m19ThemTen  = `section[aria-labelledby="ten-mien-them"]`
	m19BangLuat = `section[aria-labelledby="chuyen-huong-bang"]`
	m19ThemLuat = `section[aria-labelledby="chuyen-huong-them"]`
)

func liveM19(c *cdp, base, adminPW, out string, o m16Opts) error {
	if err := c.watchConsole(); err != nil {
		return err
	}
	if _, err := c.call("Page.navigate", map[string]any{"url": base + "/dang-nhap"}); err != nil {
		return err
	}
	if err := c.waitTrue(`document.readyState==='complete'`, 20*time.Second); err != nil {
		return err
	}
	if _, err := c.eval(`localStorage.setItem('tpan-theme','light');localStorage.setItem('tpan-lang','vi')`); err != nil {
		return err
	}
	site := o.site
	var l liveCheck
	var err error
	switch o.step {
	case "them-ten":
		err = liveThemTen(c, &l, base, adminPW, out, defaultStr(site, "example.com"))
	case "tu-choi":
		err = liveTuChoi(c, &l, base, o.userName, os.Getenv("TPAN_USER_PASSWORD"), out, site)
	case "chua-dat":
		err = liveChuaDat(c, &l, base, adminPW, out, defaultStr(site, "example.com"))
	case "them-luat":
		err = liveThemLuat(c, &l, base, adminPW, out, defaultStr(site, "example.com"))
	default:
		return fmt.Errorf("-buoc của m19-that chỉ nhận them-ten, tu-choi, chua-dat, them-luat (đang là %q)", o.step)
	}
	if err != nil {
		return errors.New(strings.ReplaceAll(err.Error(), base, "<panel>"))
	}
	if l.fails > 0 {
		return fmt.Errorf("bước %s: %d khẳng định không đạt", o.step, l.fails)
	}
	return nil
}

func defaultStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// tableRows đọc chữ từng ô của bảng trong thẻ section (hàng nào cũng là một dòng).
func tableRows(c *cdp, section string) [][]string {
	s := c.str(fmt.Sprintf(`JSON.stringify([...document.querySelectorAll(%q+' tbody tr')].map(tr=>[...tr.children].map(td=>td.textContent.trim())))`, section))
	var rows [][]string
	_ = json.Unmarshal([]byte(s), &rows)
	return rows
}

// nameRow tìm hàng của một tên trong bảng tên: ô đầu là tên, có thể kèm nhãn
// "tên chính" hay "đang xử lý" dính liền (hai thẻ Tag không có chữ ngăn cách trong textContent).
func nameRow(rows [][]string, name string) []string {
	for _, r := range rows {
		if len(r) == 0 {
			continue
		}
		switch r[0] {
		case name, name + "tên chính", name + "đang xử lý", name + "tên chínhđang xử lý":
			return r
		}
	}
	return nil
}

// cells nối các ô [from, to) của một hàng bằng " | " (an toàn khi hàng ngắn hơn).
func cells(row []string, from, to int) string {
	to = min(to, len(row))
	if from >= to {
		return ""
	}
	return strings.Join(row[from:to], " | ")
}

// cellRow tìm hàng có ô đầu đúng bằng text (bảng luật: ô đầu là nguồn).
func cellRow(rows [][]string, text string) []string {
	for _, r := range rows {
		if len(r) > 0 && r[0] == text {
			return r
		}
	}
	return nil
}

// waitRows chờ tới khi find thấy hàng trong bảng (bảng tải lại sau khi job xong).
func waitRows(c *cdp, section string, find func([][]string) []string, timeout time.Duration) []string {
	end := time.Now().Add(timeout)
	for {
		if r := find(tableRows(c, section)); r != nil {
			return r
		}
		if time.Now().After(end) {
			return nil
		}
		time.Sleep(400 * time.Millisecond)
	}
}

// openSiteTab mở /sites/<site>?tab=<tab>, chờ màn tải xong và khẳng định tab
// đó đang được chọn (Radix Tabs: nút có data-tab, aria-selected) và thẻ bảng của nó có mặt.
func openSiteTab(c *cdp, l *liveCheck, base, site, tab, section string) error {
	path := "/sites/" + site
	if err := c.goTo(base+path+"?tab="+tab, path); err != nil {
		return err
	}
	if err := settle(c); err != nil {
		return err
	}
	if err := c.waitTrue(fmt.Sprintf(`!!document.querySelector(%q)`, section), 20*time.Second); err != nil {
		return fmt.Errorf("tab %s của %s không có thẻ %s: %w", tab, site, section, err)
	}
	sel := c.str(fmt.Sprintf(`document.querySelector('[role=tab][data-tab=%q]')?.getAttribute('aria-selected')||''`, tab))
	l.ok(sel == "true", "mở /sites/%s?tab=%s: tab «%s» đang được chọn, thẻ bảng có mặt", site, tab, c.str(fmt.Sprintf(`document.querySelector('[role=tab][data-tab=%q]')?.textContent.trim()||''`, tab)))
	return nil
}

// selectValue chọn giá trị cho ô <select> của React (đặt value rồi bắn sự kiện
// change) và khẳng định React đã nhận: ô controlled mà không nhận thì tự trả về giá trị cũ.
func selectValue(c *cdp, sel, val string) error {
	v, err := c.eval(fmt.Sprintf(`(()=>{const e=document.querySelector(%q); if(!e) return 'không thấy ô'; e.scrollIntoView({block:'center'}); e.value=%q; e.dispatchEvent(new Event('change',{bubbles:true})); return ''})()`, sel, val))
	if err != nil {
		return err
	}
	if s := fmt.Sprint(v); s != "" {
		return fmt.Errorf("%s: %s", sel, s)
	}
	time.Sleep(250 * time.Millisecond)
	if got := c.str(fmt.Sprintf(`document.querySelector(%q)?.value||''`, sel)); got != val {
		return fmt.Errorf("ô %s: chọn %q nhưng ô giữ %q", sel, val, got)
	}
	return nil
}

// leakTokens trả các từ (dãy [A-Za-z0-9_.-], bỏ dấu chấm cuối) của text trùng
// NGUYÊN từ với một chuỗi trong bad. Không dò chuỗi con: tên user B gõ vào
// (www.example.com) có thể nằm trong lời, chỉ cấm nguyên tên site hay tên user khác.
var tokenRE = regexp.MustCompile(`[A-Za-z0-9_.-]+`)

func leakTokens(text string, bad ...string) []string {
	set := map[string]bool{}
	for _, b := range bad {
		set[b] = true
	}
	var hit []string
	for _, t := range tokenRE.FindAllString(text, -1) {
		if t = strings.TrimRight(t, "."); set[t] {
			hit = append(hit, t)
		}
	}
	return hit
}

// errorsFromFlow in lỗi console của một lượt (đã bỏ dòng CSP đã biết của hộp thoại Radix).
func errorsFromFlow(c *cdp, l *liveCheck, base, what string) {
	errs := flowErrors(c, base)
	l.ok(len(errs) == 0, "%s: %d lỗi console", what, len(errs))
	for _, e := range errs {
		fmt.Println("       ", e)
	}
}

// ---------------------------------------------------------------- E10

func liveThemTen(c *cdp, l *liveCheck, base, pw, out, site string) error {
	if err := formLogin(c, base, "", pw, "/"); err != nil {
		return err
	}
	if err := openSiteTab(c, l, base, site, "ten-mien", m19BangTen); err != nil {
		return err
	}
	_ = c.consoleErrors()
	l.ok(c.str(`document.querySelector('`+m19BangTen+` h2')?.innerText.replace(/\s+\d+ tên$/,'').trim()||''`) == m19Nhan.tenBang, "tab Tên miền có thẻ «%s»", m19Nhan.tenBang)
	rows := tableRows(c, m19BangTen)
	l.ok(nameRow(rows, site) != nil, "bảng có tên chính %s", site)
	l.ok(nameRow(rows, m19TenMoi) == nil, "trước khi thêm: bảng chưa có %s", m19TenMoi)

	if err := fillID(c, "#ten-mien-ten", m19TenMoi); err != nil {
		return err
	}
	if err := selectValue(c, "#ten-mien-kieu", "serve"); err != nil {
		return err
	}
	l.ok(true, "gõ %s vào ô «Tên phụ», chọn kiểu «Phục vụ cùng nội dung»", m19TenMoi)

	before := jobIDs(c)
	if err := c.press(m19ThemTen + ` button[type=submit]`); err != nil {
		return err
	}
	if err := c.waitTrue(`!!document.querySelector('[data-plan-run]:not([disabled])')`, 60*time.Second); err != nil {
		return fmt.Errorf("bấm «Xem trước» không ra bản xem trước (lỗi trên trang: %s): %w",
			c.str(`[...document.querySelectorAll('main [role=alert]')].map(e=>e.textContent.trim().slice(0,160)).join(' | ')`), err)
	}
	l.ok(true, "bấm «Xem trước»: bản xem trước hiện ra, nút chạy ghi «%s»", c.str(`document.querySelector('[data-plan-run]').innerText.trim()`))
	l.ok(strings.Contains(c.str(`document.querySelector('main').innerText`), m19TenMoi), "bản xem trước nhắc tên %s", m19TenMoi)
	if err := settle(c); err != nil {
		return err
	}
	if err := snapFull(c, out, "M19-that-1-xem-truoc.png"); err != nil {
		return err
	}
	if err := runPlan(c); err != nil {
		return err
	}
	id, st := waitNewJob(c, "site.alias.add", before, 5*time.Minute)
	l.ok(st == "succeeded", "job site.alias.add %s kết thúc: %s", id, st)
	l.ok(trackerDone(c), "thẻ theo dõi job báo «Xong»")

	row := waitRows(c, m19BangTen, func(r [][]string) []string { return nameRow(r, m19TenMoi) }, 30*time.Second)
	l.ok(row != nil, "bảng tên có dòng %s sau khi job xong", m19TenMoi)
	if row != nil {
		l.ok(len(row) > 1 && row[1] == m19Nhan.kieuServe, "dòng %s ở cột Kiểu ghi «%s» (đang là %q)", m19TenMoi, m19Nhan.kieuServe, cells(row, 1, 4))
	}
	api := c.str(fmt.Sprintf(`fetch('/api/v1/sites/%s/names').then(r=>r.json()).then(l=>{const n=l.find(x=>x.name===%q); return n?n.mode+'|'+n.state:''})`, site, m19TenMoi))
	l.ok(api == "serve|active", "GET /api/v1/sites/%s/names: %s serve và active (đang là %q)", site, m19TenMoi, api)
	if err := settle(c); err != nil {
		return err
	}
	if err := snapFull(c, out, "M19-that-2-da-them.png"); err != nil {
		return err
	}
	errorsFromFlow(c, l, base, "lượt thêm tên")
	return nil
}

// ---------------------------------------------------------------- E23

func liveTuChoi(c *cdp, l *liveCheck, base, user, pw, out, site string) error {
	if user == "" || pw == "" || site == "" {
		return errors.New("-buoc tu-choi cần -ten <user B>, -site <site của B> và biến môi trường TPAN_USER_PASSWORD")
	}
	if err := formLogin(c, base, user, pw, "/tools"); err != nil {
		return err
	}
	l.ok(true, "user thường %s đăng nhập qua form", user)
	if err := openSiteTab(c, l, base, site, "ten-mien", m19BangTen); err != nil {
		return err
	}
	_ = c.consoleErrors()
	n0 := len(tableRows(c, m19BangTen))
	if err := fillID(c, "#ten-mien-ten", m19TenCuaA); err != nil {
		return err
	}
	if err := c.press(m19ThemTen + ` button[type=submit]`); err != nil {
		return err
	}
	// Hoặc bản xem trước hiện ra (sai: tên của người khác không được dựng bản xem trước), hoặc câu từ chối.
	err := c.waitTrue(`!!document.querySelector('`+m19ThemTen+` [role=alert]') || !!document.querySelector('[data-plan-run]')`, 60*time.Second)
	if err != nil {
		return fmt.Errorf("sau «Xem trước» không thấy câu từ chối cũng không thấy bản xem trước: %w", err)
	}
	l.ok(c.str(`document.querySelector('[data-plan-run]')?'có':''`) == "", "không có bản xem trước cho tên %s của người khác", m19TenCuaA)
	alert := c.str(`document.querySelector('` + m19ThemTen + ` [role=alert]')?.innerText||''`)
	l.ok(strings.Contains(alert, m19Nhan.tuChoi), "thẻ «%s» hiện câu từ chối chung «%s»", m19Nhan.themTen, m19Nhan.tuChoi)
	l.ok(c.str(`document.querySelector('#ten-mien-ten')?.getAttribute('aria-invalid')||''`) == "true", "ô tên phụ có dấu lỗi (aria-invalid)")
	// Không lộ: nguyên từ là tên site hay user của người khác, trong cả trang; thêm "admin" trong lời từ chối.
	page := c.str(`document.body.innerText`)
	hit := leakTokens(page, "example.com", "blog.example.com", "example.org", "www.example.org", "example.net", "khach_a", "shop.example.com")
	l.ok(len(hit) == 0, "trang của B không nhắc tên site hay user nào của người khác (lọt: %v)", hit)
	hit = leakTokens(alert, "admin", "khach_a", "example.com", "blog.example.com", "www.example.org")
	l.ok(len(hit) == 0, "lời từ chối không nêu chủ hay tên site khác (lọt: %v)", hit)
	own := c.str(`fetch('/api/v1/domains').then(r=>r.json()).then(l=>l.map(x=>x.site).join(','))`)
	l.ok(own == site, "GET /api/v1/domains của B chỉ có site của B (đang là %q)", own)
	l.ok(len(tableRows(c, m19BangTen)) == n0, "bảng tên của %s vẫn %d dòng", site, n0)
	if err := snapFull(c, out, "M19-that-4-tu-choi.png"); err != nil {
		return err
	}
	errs := flowErrors(c, base)
	// Lời 409 của máy chủ hiện thành một dòng console "Failed to load resource" (như 403 ở m16-that): chỉ tính các lỗi khác.
	var other []string
	for _, e := range errs {
		if !strings.Contains(e, "409") {
			other = append(other, e)
		}
	}
	l.ok(len(other) == 0, "lượt thử tên bị từ chối: %d lỗi console ngoài dòng 409 chờ trước", len(other))
	for _, e := range other {
		fmt.Println("       ", e)
	}
	return nil
}

// ---------------------------------------------------------------- E41

func liveChuaDat(c *cdp, l *liveCheck, base, pw, out, site string) error {
	if err := formLogin(c, base, "", pw, "/"); err != nil {
		return err
	}
	if err := openSiteTab(c, l, base, site, "ten-mien", m19BangTen); err != nil {
		return err
	}
	_ = c.consoleErrors()
	row := waitRows(c, m19BangTen, func(r [][]string) []string { return nameRow(r, m19TenChua) }, 20*time.Second)
	if row == nil {
		return fmt.Errorf("bảng tên của %s không có dòng %s (thêm bằng tpan site alias add trước bước này)", site, m19TenChua)
	}
	l.ok(true, "bảng tên có dòng %s", m19TenChua)
	// Cột: Tên | Kiểu | DNS | Chứng chỉ | Việc.
	l.ok(len(row) >= 4 && row[2] != m19Nhan.dnsOk && row[2] != "", "cột DNS của %s không ghi «%s» (đang là %q)", m19TenChua, m19Nhan.dnsOk, cells(row, 2, 3))
	l.ok(len(row) >= 4 && row[3] == m19Nhan.chuaTrongCC, "cột Chứng chỉ của %s ghi «%s»: chưa có trong chứng chỉ (đang là %q)", m19TenChua, m19Nhan.chuaTrongCC, cells(row, 3, 4))
	api := c.str(fmt.Sprintf(`fetch('/api/v1/sites/%s/names').then(r=>r.json()).then(l=>{const n=l.find(x=>x.name===%q); return n?String(n.in_cert)+'|'+n.dns:''})`, site, m19TenChua))
	dns := strings.TrimPrefix(api, "false|")
	l.ok(strings.HasPrefix(api, "false|") && dns != "ok", "API: %s in_cert=false, dns=%s (không phải ok)", m19TenChua, dns)
	if err := settle(c); err != nil {
		return err
	}
	if err := snapFull(c, out, "M19-that-3-chua-dat-sang.png"); err != nil {
		return err
	}
	// Bản tối của cùng màn.
	if _, err := c.eval(`localStorage.setItem('tpan-theme','dark')`); err != nil {
		return err
	}
	if err := openSiteTab(c, l, base, site, "ten-mien", m19BangTen); err != nil {
		return err
	}
	if waitRows(c, m19BangTen, func(r [][]string) []string { return nameRow(r, m19TenChua) }, 20*time.Second) == nil {
		return fmt.Errorf("bản tối: bảng không có dòng %s", m19TenChua)
	}
	if err := settle(c); err != nil {
		return err
	}
	if err := snapFull(c, out, "M19-that-3-chua-dat-toi.png"); err != nil {
		return err
	}
	if _, err := c.eval(`localStorage.setItem('tpan-theme','light')`); err != nil {
		return err
	}
	errorsFromFlow(c, l, base, "tab Tên miền của site có tên chưa đạt")
	return nil
}

// ---------------------------------------------------------------- luật chuyển hướng

func liveThemLuat(c *cdp, l *liveCheck, base, pw, out, site string) error {
	const bad, good, dich = "/a b", "/cu", "/moi"
	if err := formLogin(c, base, "", pw, "/"); err != nil {
		return err
	}
	if err := openSiteTab(c, l, base, site, "chuyen-huong", m19BangLuat); err != nil {
		return err
	}
	_ = c.consoleErrors()
	l.ok(c.str(`document.querySelector('`+m19BangLuat+` h2')?.innerText.replace(/\s+\d+ luật$/,'').trim()||''`) == m19Nhan.luatBang, "tab Chuyển hướng có thẻ «%s»", m19Nhan.luatBang)
	l.ok(cellRow(tableRows(c, m19BangLuat), good) == nil, "trước khi thêm: bảng chưa có luật %s", good)

	// 1. Nguồn sai: lỗi hiện dưới ô nguồn, không dựng bản xem trước.
	if err := fillID(c, "#chuyen-huong-nguon", bad); err != nil {
		return err
	}
	if err := fillID(c, "#chuyen-huong-dich", dich); err != nil {
		return err
	}
	if err := c.press(m19ThemLuat + ` button[type=submit]`); err != nil {
		return err
	}
	if err := c.waitTrue(`document.querySelector('#chuyen-huong-nguon')?.getAttribute('aria-invalid')==='true' || !!document.querySelector('[data-plan-run]')`, 60*time.Second); err != nil {
		return fmt.Errorf("nguồn %q: ô nguồn không báo lỗi và cũng không có bản xem trước: %w", bad, err)
	}
	l.ok(c.str(`document.querySelector('[data-plan-run]')?'có':''`) == "", "nguồn %q: không có bản xem trước", bad)
	l.ok(c.str(`document.querySelector('#chuyen-huong-nguon')?.getAttribute('aria-invalid')||''`) == "true", "nguồn %q: ô nguồn có dấu lỗi (aria-invalid)", bad)
	l.ok(c.str(`document.querySelector('#chuyen-huong-dich')?.getAttribute('aria-invalid')||''`) == "", "ô đích không bị đánh dấu lỗi")
	// Lời lỗi nằm NGAY DƯỚI ô nguồn: cùng khối, dưới đáy ô, chồng lên ô theo chiều ngang.
	pos := c.str(`(()=>{const i=document.querySelector('#chuyen-huong-nguon'); const a=i&&i.parentElement.querySelector('[role=alert]');
		if(!a||!a.textContent.trim()) return 'không có lời lỗi cạnh ô nguồn';
		const r=i.getBoundingClientRect(), s=a.getBoundingClientRect();
		if(s.top < r.bottom-1) return 'lời lỗi nằm trên đáy ô nguồn';
		if(s.left > r.right || s.right < r.left) return 'lời lỗi lệch khỏi ô nguồn';
		return ''})()`)
	l.ok(pos == "", "lời lỗi nằm dưới ô nguồn%s", func() string {
		if pos == "" {
			return ""
		}
		return ": " + pos
	}())
	l.ok(c.str(`document.querySelector('`+m19ThemLuat+` [role=alert]')?.innerText.trim()||''`) != "", "lời lỗi của máy chủ có chữ (không để trống)")
	if err := snapFull(c, out, "M19-that-5-nguon-sai.png"); err != nil {
		return err
	}
	// 400 của máy chủ hiện một dòng console "Failed to load resource": chờ trước.
	var other []string
	for _, e := range flowErrors(c, base) {
		if !strings.Contains(e, "400") {
			other = append(other, e)
		}
	}
	l.ok(len(other) == 0, "lượt nguồn sai: %d lỗi console ngoài dòng 400 chờ trước", len(other))
	for _, e := range other {
		fmt.Println("       ", e)
	}

	// 2. Sửa nguồn rồi chạy.
	if err := fillID(c, "#chuyen-huong-nguon", good); err != nil {
		return err
	}
	before := jobIDs(c)
	if err := c.press(m19ThemLuat + ` button[type=submit]`); err != nil {
		return err
	}
	if err := c.waitTrue(`!!document.querySelector('[data-plan-run]:not([disabled])')`, 60*time.Second); err != nil {
		return fmt.Errorf("nguồn %s: «Xem trước» không ra bản xem trước (lỗi trên trang: %s): %w", good,
			c.str(`[...document.querySelectorAll('main [role=alert]')].map(e=>e.textContent.trim().slice(0,160)).join(' | ')`), err)
	}
	l.ok(c.str(`document.querySelector('#chuyen-huong-nguon')?.getAttribute('aria-invalid')||''`) == "", "sửa thành %s: dấu lỗi ở ô nguồn đã mất", good)
	l.ok(true, "bản xem trước hiện ra, nút chạy ghi «%s»", c.str(`document.querySelector('[data-plan-run]').innerText.trim()`))
	if err := runPlan(c); err != nil {
		return err
	}
	id, st := waitNewJob(c, "site.redirect.add", before, 5*time.Minute)
	l.ok(st == "succeeded", "job site.redirect.add %s kết thúc: %s", id, st)
	l.ok(trackerDone(c), "thẻ theo dõi job báo «Xong»")
	row := waitRows(c, m19BangLuat, func(r [][]string) []string { return cellRow(r, good) }, 30*time.Second)
	l.ok(row != nil, "bảng luật có dòng %s sau khi job xong", good)
	if row != nil {
		l.ok(len(row) > 2 && row[1] == dich && row[2] == "301", "dòng %s: đích %s, mã 301 (đang là %q)", good, dich, cells(row, 1, 3))
	}
	if err := settle(c); err != nil {
		return err
	}
	if err := snapFull(c, out, "M19-that-6-da-them-luat.png"); err != nil {
		return err
	}
	errorsFromFlow(c, l, base, "lượt thêm luật")
	return nil
}
