package main

// Phần trình duyệt của e2e bước 19 (mốc M16, scripts/e2e-m16.sh): Chrome không
// giao diện mở panel THẬT trên máy ảo test và làm như người dùng: đăng nhập
// qua form, mở từng màn, tạo site từ giao diện.
//
//	TPAN_PANEL_PASSWORD=… go run ./scripts/anh-readme -bo m16-that -panel https://<máy ảo>:8443 -buoc admin
//	… -buoc tao-site -site <tên miền>
//	TPAN_USER_PASSWORD=… … -buoc user -ten <user thường> -site <site của user đó>
//	… -buoc nang-cap -cho-tep <file>      (đăng nhập trên bản cũ, chờ file xuất hiện, rồi kiểm bản mới)
//
// Mỗi khẳng định in một dòng "  => ĐẠT: …" hoặc "  => KHÔNG ĐẠT: …" (script
// e2e đếm các dòng đó). Chương trình không in mật khẩu, cookie hay địa chỉ
// panel. KHÔNG chạy với panel của máy chủ thật.

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// liveCheck gom kết quả của một bước.
type liveCheck struct{ fails int }

func (l *liveCheck) ok(cond bool, format string, a ...any) bool {
	s := fmt.Sprintf(format, a...)
	if cond {
		fmt.Println("  => ĐẠT:", s)
	} else {
		fmt.Println("  => KHÔNG ĐẠT:", s)
		l.fails++
	}
	return cond
}

// str chạy JS và trả chuỗi (rỗng khi lỗi).
func (c *cdp) str(js string) string {
	v, err := c.eval(js)
	if err != nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

// formLogin đăng nhập qua form của màn Đăng nhập, gõ phím thật. name rỗng là
// để trống ô tên (admin). wantPath là nơi phải tới sau khi đăng nhập.
func formLogin(c *cdp, base, name, password, wantPath string) error {
	if err := c.goTo(base+"/", "/dang-nhap"); err != nil {
		return err
	}
	if name != "" {
		if err := c.typeInto("#ten-dang-nhap", name); err != nil {
			return err
		}
	}
	if err := c.typeInto("#mat-khau", password); err != nil {
		return err
	}
	if err := c.keyRaw("Enter"); err != nil {
		return err
	}
	if err := c.waitTrue(fmt.Sprintf(`location.pathname===%q && !!document.querySelector('main h1')`, wantPath), 30*time.Second); err != nil {
		return fmt.Errorf("sau khi gửi form đăng nhập không tới %s (đang ở %s): %w", wantPath, c.str(`location.pathname`), err)
	}
	return nil
}

// visit mở một màn và khẳng định: có tiêu đề, không lỗi console, cả trang không cuộn ngang.
func visit(c *cdp, l *liveCheck, base, path string) {
	want := path
	if i := strings.IndexAny(want, "?#"); i >= 0 {
		want = want[:i]
	}
	if err := c.goTo(base+path, want); err != nil {
		l.ok(false, "mở %s: %v", path, strings.ReplaceAll(err.Error(), base, "<panel>"))
		return
	}
	if err := settle(c); err != nil {
		l.ok(false, "mở %s: %v", path, err)
		return
	}
	h1 := c.str(`document.querySelector('main h1')?.innerText.trim()||''`)
	var bad []string
	for _, e := range c.consoleErrors() {
		bad = append(bad, strings.ReplaceAll(e, base, "<panel>"))
	}
	over, _ := c.eval(`document.documentElement.scrollWidth > innerWidth`)
	l.ok(h1 != "" && len(bad) == 0 && over != true, "mở %s: tiêu đề «%s», %d lỗi console, cuộn ngang: %v", path, h1, len(bad), over == true)
	for _, e := range bad {
		fmt.Println("       ", e)
	}
}

// openMenu bấm một nút mở menu thả, chờ menu hiện, đóng bằng Escape, rồi khẳng
// định không có lỗi console (menu của bản 2 không được vi phạm CSP).
func openMenu(c *cdp, l *liveCheck, what, selector string) {
	if c.str(fmt.Sprintf(`document.querySelector(%q)?'có':''`, selector)) == "" {
		l.ok(false, "%s: không thấy nút mở menu (%s)", what, selector)
		return
	}
	_ = c.consoleErrors()
	if err := c.press(selector); err != nil {
		l.ok(false, "%s: %v", what, err)
		return
	}
	err := c.waitTrue(`!!document.querySelector('[role=menu]')`, 5*time.Second)
	items := c.str(`[...document.querySelectorAll('[role=menu] [role^=menuitem]')].map(e=>e.textContent.trim()).join(' | ')`)
	_ = c.key("Escape")
	closed := c.waitTrue(`!document.querySelector('[role=menu]')`, 5*time.Second) == nil
	errs := c.consoleErrors()
	l.ok(err == nil && closed && len(errs) == 0, "%s: mở được, Escape đóng được, %d lỗi console; các mục: %s", what, len(errs), items)
	for _, e := range errs {
		fmt.Println("       ", e)
	}
}

func liveM16(c *cdp, base, adminPW string, o m16Opts) error {
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
	var l liveCheck
	var err error
	switch o.step {
	case "admin":
		err = liveAdmin(c, &l, base, adminPW)
	case "tao-site":
		err = liveCreateSite(c, &l, base, adminPW, o.site)
	case "user":
		err = liveUser(c, &l, base, o.userName, os.Getenv("TPAN_USER_PASSWORD"), o.site)
	case "nang-cap":
		err = liveUpgrade(c, &l, base, adminPW, o.waitFile)
	default:
		return fmt.Errorf("-buoc chỉ nhận admin, tao-site, user, nang-cap (đang là %q)", o.step)
	}
	if err != nil {
		return errors.New(strings.ReplaceAll(err.Error(), base, "<panel>"))
	}
	if l.fails > 0 {
		return fmt.Errorf("bước %s: %d khẳng định không đạt", o.step, l.fails)
	}
	return nil
}

// siteTabs là các tab của trang một site (mục 3.6 của bản thiết kế M16).
var siteTabs = []string{"phep-kiem", "log", "php", "backup", "cai-dat"}

func liveAdmin(c *cdp, l *liveCheck, base, pw string) error {
	if err := formLogin(c, base, "", pw, "/"); err != nil {
		return err
	}
	l.ok(strings.Contains(c.str(`document.querySelector('main h1').innerText`), "Bảng điều khiển"), "admin đăng nhập qua form: tới / và thấy Bảng điều khiển")
	_ = c.consoleErrors()

	site := c.str(`fetch('/api/v1/sites').then(r=>r.json()).then(l=>(l.find(s=>s.status==='active')||{}).domain||'')`)
	job := c.str(`fetch('/api/v1/jobs?limit=50').then(r=>r.json()).then(l=>l.length?l[0].id:'')`)
	paths := []string{"/", "/tools", "/sites", "/sites/new"}
	if site != "" {
		paths = append(paths, "/sites/"+site)
		for _, t := range siteTabs {
			paths = append(paths, "/sites/"+site+"?tab="+t)
		}
	}
	paths = append(paths, "/databases", "/ssl", "/backups", "/cron", "/trash", "/usage", "/incidents", "/jobs")
	if job != "" {
		paths = append(paths, "/jobs/"+job)
	}
	paths = append(paths, "/plans", "/stack", "/updates", "/security", "/ai", "/users", "/packages", "/settings")
	for _, p := range paths {
		visit(c, l, base, p)
	}

	// Menu thả không được vi phạm CSP (bản thử T2 của M16).
	if err := c.goTo(base+"/databases", "/databases"); err != nil {
		return err
	}
	if err := settle(c); err != nil {
		return err
	}
	openMenu(c, l, "menu tài khoản", `header button[aria-haspopup="menu"]`)
	openMenu(c, l, "menu ba chấm của hàng đầu ở màn Cơ sở dữ liệu", `main button[aria-haspopup="menu"]`)

	// Số ở mục "Cơ sở dữ liệu" của menu phải bằng số DB đang chạy mà API liệt kê.
	menu := c.str(`(document.querySelector('nav#menu-chinh a[href="/databases"]')?.innerText||'').replace(/\D+/g,' ').trim()`)
	api := c.str(`fetch('/api/v1/databases').then(r=>r.json()).then(l=>String(l.filter(d=>d.status==='active').length))`)
	l.ok(menu != "" && menu == api, "số ở mục Cơ sở dữ liệu của menu (%s) bằng số DB đang chạy của /api/v1/databases (%s)", menu, api)

	// Dòng phiên bản nằm cuối menu, không còn chân trang.
	ver := c.str(`fetch('/api/v1/session').then(r=>r.json()).then(j=>j.version||'')`)
	line := c.str(`document.getElementById('dong-phien-ban')?.innerText||''`)
	l.ok(ver != "" && strings.Contains(line, ver) && c.str(`document.querySelector('footer')?'có':''`) == "", "dòng phiên bản cuối menu ghi %s, không còn chân trang", ver)
	return nil
}

func liveCreateSite(c *cdp, l *liveCheck, base, pw, site string) error {
	if site == "" {
		return errors.New("-buoc tao-site cần -site <tên miền>")
	}
	if err := formLogin(c, base, "", pw, "/"); err != nil {
		return err
	}
	if err := c.goTo(base+"/sites/new", "/sites/new"); err != nil {
		return err
	}
	if err := c.typeInto("#o-domain", site); err != nil {
		return err
	}
	if err := c.typeInto("#o-email", "admin@example.com"); err != nil {
		return err
	}
	if err := c.keyRaw("Enter"); err != nil { // Enter trong ô nhập là gửi form: xem trước
		return err
	}
	if err := c.waitTrue(`!!document.querySelector('[data-plan-run]:not([disabled])')`, 60*time.Second); err != nil {
		return fmt.Errorf("không thấy bản xem trước sau khi gửi form Tạo site (trang đang ghi: %s): %w", c.str(`document.querySelector('main').innerText.slice(0,400)`), err)
	}
	steps := c.str(`document.querySelector('[data-plan-run]').innerText.trim()`)
	l.ok(true, "form Tạo site (%s): bản xem trước hiện ra, nút chạy ghi «%s»", site, steps)
	if err := c.press("[data-plan-run]"); err != nil {
		return err
	}
	// Plan có bước nguy hiểm thì có hộp hỏi lại: bấm nút làm việc của hộp.
	time.Sleep(400 * time.Millisecond)
	if c.str(`document.querySelector('[role=dialog],[role=alertdialog]')?'có':''`) != "" {
		if _, err := c.eval(`document.querySelector('[role=dialog] button,[role=alertdialog] button').click()`); err != nil {
			return err
		}
	}
	if err := c.waitTrue(`location.pathname.startsWith('/jobs/')`, 30*time.Second); err != nil {
		return fmt.Errorf("bấm chạy xong không sang trang job: %w", err)
	}
	job := strings.TrimPrefix(c.str(`location.pathname`), "/jobs/")
	status := ""
	for i := 0; i < 300; i++ {
		status = c.str(fmt.Sprintf(`fetch('/api/v1/jobs/%s').then(r=>r.json()).then(j=>j.job.status)`, job))
		if status != "running" && status != "" {
			break
		}
		time.Sleep(time.Second)
	}
	l.ok(status == "succeeded", "job tạo site %s kết thúc: %s", job, status)
	if err := settle(c); err != nil {
		return err
	}
	// Mật khẩu quản trị WordPress hiện một lần ở trang job: không in ra, chỉ khẳng định khối có mặt.
	l.ok(strings.Contains(c.str(`document.querySelector('main').innerText`), site), "trang job nhắc tới %s", site)
	var bad []string
	for _, e := range c.consoleErrors() {
		if !knownDialogCSP(e) {
			bad = append(bad, strings.ReplaceAll(e, base, "<panel>"))
		}
	}
	l.ok(len(bad) == 0, "cả lượt tạo site: %d lỗi console (không tính dòng CSP đã biết của hộp hỏi lại)", len(bad))
	for _, e := range bad {
		fmt.Println("       ", e)
	}

	visit(c, l, base, "/sites")
	l.ok(c.str(fmt.Sprintf(`[...document.querySelectorAll('main a')].some(a=>a.textContent.trim()===%q)?'có':''`, site)) != "", "site %s có trong danh sách Site", site)
	visit(c, l, base, "/")
	one := c.str(fmt.Sprintf(`(()=>{const e=[...document.querySelectorAll('[data-site-name]')].find(e=>e.textContent.trim()===%q||e.getAttribute('title')===%q); if(!e) return 'không thấy'; const lh=parseFloat(getComputedStyle(e).lineHeight)||20; return Math.round(e.getBoundingClientRect().height/lh)<=1 ? 'một dòng' : 'nhiều dòng'})()`, site, site))
	l.ok(one == "một dòng", "tên %s (%d ký tự) trên bản đồ tuyến: %s", site, len(site), one)
	return nil
}

func liveUser(c *cdp, l *liveCheck, base, name, pw, site string) error {
	if name == "" || pw == "" {
		return errors.New("-buoc user cần -ten <user> và biến môi trường TPAN_USER_PASSWORD")
	}
	if err := formLogin(c, base, name, pw, "/tools"); err != nil {
		return err
	}
	l.ok(true, "user thường %s đăng nhập qua form: tới /tools (trang chủ là Công cụ)", name)
	_ = c.consoleErrors()

	adminOnly := []string{"/users", "/packages", "/stack", "/updates", "/security", "/ai", "/plans"}
	inMenu := c.str(`[...document.querySelectorAll('nav#menu-chinh a')].map(a=>a.getAttribute('href')).join(' ')`)
	inMain := c.str(`[...document.querySelectorAll('main a')].map(a=>a.getAttribute('href')).join(' ')`)
	var leaked []string
	for _, p := range adminOnly {
		if strings.Contains(" "+inMenu+" ", " "+p+" ") || strings.Contains(" "+inMain+" ", " "+p+" ") {
			leaked = append(leaked, p)
		}
	}
	l.ok(inMenu != "" && len(leaked) == 0, "menu và trang Công cụ của user thường không có mục admin (lọt: %v)", leaked)

	paths := []string{"/tools", "/dashboard", "/sites", "/sites/new"}
	if site != "" {
		paths = append(paths, "/sites/"+site)
		for _, t := range siteTabs {
			paths = append(paths, "/sites/"+site+"?tab="+t)
		}
	}
	paths = append(paths, "/databases", "/ssl", "/backups", "/cron", "/trash", "/usage", "/incidents", "/jobs", "/settings")
	for _, p := range paths {
		visit(c, l, base, p) // request bị 403 hiện thành lỗi console, nên visit cũng bắt được
	}
	l.ok(strings.Contains(c.str(`document.querySelector('main h1').innerText`), "Cài đặt"), "màn cuối cùng user mở là Cài đặt")

	if err := c.goTo(base+"/users", "/users"); err != nil {
		return err
	}
	if err := settle(c); err != nil {
		return err
	}
	h1 := c.str(`document.querySelector('main h1').innerText.trim()`)
	errs := c.consoleErrors()
	l.ok(strings.Contains(h1, "Không có quyền") && len(errs) == 0, "user thường gõ /users: thấy «%s», %d lỗi console (không gọi API admin)", h1, len(errs))

	if _, err := c.call("Page.navigate", map[string]any{"url": base + "/"}); err != nil {
		return err
	}
	err := c.waitTrue(`location.pathname==='/tools' && !!document.querySelector('main h1')`, 20*time.Second)
	l.ok(err == nil, "user thường mở / thì được đưa tới /tools (đang ở %s)", c.str(`location.pathname`))
	return nil
}

func liveUpgrade(c *cdp, l *liveCheck, base, pw, waitFile string) error {
	if waitFile == "" {
		return errors.New("-buoc nang-cap cần -cho-tep <file>")
	}
	if err := formLogin(c, base, "", pw, "/"); err != nil {
		return err
	}
	session := `fetch('/api/v1/session').then(r=>r.json()).then(j=>(j.logged_in?'1':'0')+'|'+(j.version||'')+'|'+((j.user||{}).name||'')).catch(()=>'')`
	before := c.str(session)
	old := strings.Split(before+"||", "|")[1]
	l.ok(strings.HasPrefix(before, "1|") && old != "", "bản cũ %s: đăng nhập qua form của giao diện cũ, phiên đang dùng được", old)
	hadTools := c.str(`document.querySelector('a[href="/tools"]')?'có':''`)
	l.ok(hadTools == "", "giao diện bản cũ chưa có mục Công cụ")

	fmt.Println("CHỜ NÂNG CẤP") // script e2e thấy dòng này thì chạy tpan self-update trong máy ảo
	for i := 0; ; i++ {
		if _, err := os.Stat(waitFile); err == nil {
			break
		}
		if i > 1200 {
			return errors.New("chờ file báo đã nâng cấp quá 10 phút")
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Cùng tab, cùng cookie: hỏi lại phiên cho tới khi panel lên bản mới.
	after := ""
	for i := 0; i < 240; i++ {
		after = c.str(session)
		if strings.HasPrefix(after, "1|") && strings.Split(after+"||", "|")[1] != old {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	neu := strings.Split(after+"||", "|")[1]
	l.ok(strings.HasPrefix(after, "1|") && neu != "" && neu != old, "sau khi nâng: cùng cookie, /api/v1/session vẫn logged_in, bản %s -> %s", old, neu)

	_ = c.consoleErrors() // tab cũ có lỗi kết nối trong lúc panel khởi động lại: không tính
	if _, err := c.call("Page.reload", map[string]any{}); err != nil {
		return err
	}
	if err := c.waitTrue(`document.readyState==='complete' && !!document.querySelector('main h1') && !!document.querySelector('nav#menu-chinh')`, 30*time.Second); err != nil {
		return fmt.Errorf("tải lại trang sau khi nâng: %w", err)
	}
	if err := settle(c); err != nil {
		return err
	}
	errs := c.consoleErrors()
	l.ok(c.str(`document.querySelector('nav#menu-chinh a[href="/tools"]')?'có':''`) != "" && len(errs) == 0,
		"tải lại trang: thấy giao diện bản 2 (menu có Công cụ) bằng phiên cũ, %d lỗi console", len(errs))
	for _, e := range errs {
		fmt.Println("       ", strings.ReplaceAll(e, base, "<panel>"))
	}
	visit(c, l, base, "/tools")
	visit(c, l, base, "/settings")

	// Mật khẩu cũ vẫn đăng nhập được trên bản mới.
	if err := c.logout(); err != nil {
		return err
	}
	err := formLogin(c, base, "", pw, "/")
	l.ok(err == nil, "đăng xuất rồi đăng nhập lại bằng mật khẩu cũ trên bản mới")
	return err
}
