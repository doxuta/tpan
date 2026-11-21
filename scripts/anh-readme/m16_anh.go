package main

// Bộ ảnh của mốc M16 (giao diện bản 2):
//
//	-bo m16      mọi màn ở 1440 × 900, sáng và tối, và các màn chính ở 390 × 844,
//	             trên máy demo. Mỗi lần mở màn kiểm luôn: không lỗi console, cả
//	             trang không cuộn ngang. Cuối cùng đi bàn phím và mở tiếng Anh.
//	-bo mau-v2   chụp ba trang mẫu tĩnh ở docs/mau-giao-dien/v2 (để đặt cạnh ảnh
//	             bản thật mà so); không cần máy demo.
//
// Chạy qua Makefile: `make anh-m16`.

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// demoUsers giữ mật khẩu các user mẫu mà `tpan serve --demo` in ra (mỗi lần
// chạy một mật khẩu mới): "User demo (vai user): <tên> / <mật khẩu>".
var demoUsers = struct {
	sync.Mutex
	m map[string]string
}{m: map[string]string{}}

// noteDemoUser nhận một dòng stdout của serve --demo; là dòng user mẫu thì nhớ mật khẩu.
func noteDemoUser(line string) {
	f := strings.Fields(line)
	if len(f) >= 3 && f[len(f)-2] == "/" {
		demoUsers.Lock()
		demoUsers.m[f[len(f)-3]] = f[len(f)-1]
		demoUsers.Unlock()
	}
}

// demoUserPassword chờ tới khi serve in mật khẩu của user mẫu (in ngay sau mật khẩu admin).
func demoUserPassword(name string) (string, error) {
	for i := 0; i < 50; i++ {
		demoUsers.Lock()
		pw := demoUsers.m[name]
		demoUsers.Unlock()
		if pw != "" {
			return pw, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return "", fmt.Errorf("serve --demo không in mật khẩu của user mẫu %s", name)
}

// login đăng nhập bằng fetch cùng origin (cookie HttpOnly nằm trong Chrome). name rỗng là admin.
func (c *cdp) login(name, password string) error {
	js := fmt.Sprintf(`fetch('/api/v1/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({name:%q,password:%q})}).then(r=>r.status)`, name, password)
	st, err := c.eval(js)
	if err != nil || fmt.Sprint(st) != "204" {
		return fmt.Errorf("đăng nhập %q: trạng thái %v, lỗi %v", name, st, err)
	}
	return nil
}

func (c *cdp) logout() error {
	st, err := c.eval(`fetch('/api/v1/logout',{method:'POST'}).then(r=>r.status)`)
	if err != nil || fmt.Sprint(st) != "204" {
		return fmt.Errorf("đăng xuất: trạng thái %v, lỗi %v", st, err)
	}
	return nil
}

// viewport đặt cỡ cửa sổ. Hẹp (dưới 700 px) thì giả điện thoại để trang dùng đúng bố cục màn hẹp.
func (c *cdp) viewport(w, h int) error {
	_, err := c.call("Emulation.setDeviceMetricsOverride", map[string]any{
		"width": w, "height": h, "deviceScaleFactor": 1, "mobile": w < 700})
	return err
}

// snapFull chụp cả trang (không chỉ phần đang thấy) vào out/file.
func snapFull(c *cdp, out, file string) error {
	v, err := c.eval(`JSON.stringify([Math.max(document.documentElement.scrollWidth, innerWidth), Math.max(document.documentElement.scrollHeight, innerHeight)])`)
	if err != nil {
		return err
	}
	var wh [2]float64
	if err := json.Unmarshal([]byte(fmt.Sprint(v)), &wh); err != nil {
		return err
	}
	res, err := c.call("Page.captureScreenshot", map[string]any{"format": "png", "captureBeyondViewport": true,
		"clip": map[string]any{"x": 0, "y": 0, "width": wh[0], "height": wh[1], "scale": 1}})
	if err != nil {
		return err
	}
	var r struct{ Data string }
	if err := json.Unmarshal(res, &r); err != nil {
		return err
	}
	raw, err := base64.StdEncoding.DecodeString(r.Data)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(out, file), raw, 0o644)
}

// samplesV2 chụp ba trang mẫu tĩnh của bản 2 (cả trang) vào out.
func samplesV2(c *cdp, out string) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	for _, n := range []string{"nguoi-dung", "co-so-du-lieu", "cong-cu"} {
		url := "file://" + filepath.Join(wd, "docs/mau-giao-dien/v2", n+".html")
		if _, err := c.call("Page.navigate", map[string]any{"url": url}); err != nil {
			return err
		}
		if err := c.waitTrue(`document.readyState==='complete' && location.protocol==='file:'`, 15*time.Second); err != nil {
			return fmt.Errorf("mở trang mẫu %s: %w", n, err)
		}
		if _, err := c.eval(`document.fonts.ready.then(()=>true)`); err != nil {
			return err
		}
		time.Sleep(600 * time.Millisecond)
		if err := snapFull(c, out, "mau-"+n+".png"); err != nil {
			return err
		}
		fmt.Println(filepath.Join(out, "mau-"+n+".png"))
	}
	return nil
}

// ---------------------------------------------------------------- bộ m16

// m16Shot là một ảnh của bộ M16.
type m16Shot struct {
	name string // tên file, không đuôi; chương trình thêm "-sang"/"-toi" và "-390"
	path string
	// user: tên user mẫu để đăng nhập; rỗng là admin. guest: chụp khi chưa đăng nhập.
	user  string
	guest bool
	// prep chạy sau khi màn tải xong (mở menu, bấm tab...).
	prep func(c *cdp) error
	// dialog: ảnh có hộp thoại Radix đang mở. Lúc mở, Radix cố chèn một thẻ
	// <style> và CSP chặn (mục 11 của KE-HOACH.md): bỏ qua đúng dòng đó.
	dialog bool
	// narrow: thuộc các màn chính, chụp thêm ở 390 × 844.
	narrow bool
	// full: chụp cả trang thay vì chỉ phần đang thấy (ảnh để soát, không vào repo).
}

// m16Fail gom các điều không đạt của cả lượt chạy; có một cái là thoát mã khác 0.
type m16Fail struct{ list []string }

func (f *m16Fail) add(format string, a ...any) {
	s := fmt.Sprintf(format, a...)
	f.list = append(f.list, s)
	fmt.Println("  => KHÔNG ĐẠT:", s)
}

// knownDialogCSP nhận ra dòng vi phạm CSP đã biết của Radix Dialog (thẻ <style> khoá cuộn).
func knownDialogCSP(line string) bool {
	return strings.Contains(line, "Applying inline style violates") || strings.Contains(line, "Refused to apply inline style")
}

// checkPage khẳng định hai điều đúng cho mọi màn: không lỗi console, cả trang không cuộn ngang.
func checkPage(c *cdp, f *m16Fail, what string, dialog bool) {
	for _, e := range c.consoleErrors() {
		if dialog && knownDialogCSP(e) {
			continue
		}
		f.add("%s: lỗi console: %s", what, e)
	}
	v, err := c.eval(`document.documentElement.scrollWidth - innerWidth`)
	if err != nil {
		f.add("%s: không đo được bề ngang: %v", what, err)
		return
	}
	if d, _ := v.(float64); d > 0 {
		f.add("%s: cả trang cuộn ngang, tràn %v px", what, d)
	}
}

// checkMeasure chạy do-giao-dien.js ngay trên trang đang mở và khẳng định hai
// ngưỡng cứng đúng cho mọi màn (B13): không chữ nào dưới 12 px, không ô nào
// viền màu chữ chính.
func checkMeasure(c *cdp, f *m16Fail, what string) {
	v, err := c.eval(measureJS)
	if err != nil {
		f.add("%s: không chạy được script đo: %v", what, err)
		return
	}
	var r measureRow
	if err := json.Unmarshal([]byte(fmt.Sprint(v)), &r); err != nil {
		f.add("%s: không đọc được kết quả đo: %v", what, err)
		return
	}
	if r.NhoNhat > 0 && r.NhoNhat < 12 {
		f.add("%s: có chữ %v px (dưới 12 px): %s", what, r.NhoNhat, strings.Join(r.ChuNho, "; "))
	}
	if r.VienDen > 0 {
		f.add("%s: %d ô viền màu chữ chính: %s", what, r.VienDen, strings.Join(r.MauVien, "; "))
	}
}

// pressText bấm phần tử đầu tiên khớp scope có chữ đúng bằng text (bấm chuột thật).
func (c *cdp) pressText(scope, text string) error {
	v, err := c.eval(fmt.Sprintf(`(()=>{const b=[...document.querySelectorAll(%q)].find(b=>b.textContent.trim()===%q); if(!b) return false; b.setAttribute('data-bam','1'); return true})()`, scope, text))
	if err != nil {
		return err
	}
	if v != true {
		return fmt.Errorf("không thấy «%s» trong %s", text, scope)
	}
	defer func() { _, _ = c.eval(`document.querySelector('[data-bam]')?.removeAttribute('data-bam')`) }()
	return c.press("[data-bam]")
}

// firstID lấy một giá trị từ API của panel bằng một biểu thức JS trả chuỗi (rỗng khi không có).
func firstID(c *cdp, js string) string {
	v, err := c.eval(js)
	if err != nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

func photosM16(c *cdp, base, adminPW, out, only string) error {
	if err := c.watchConsole(); err != nil {
		return err
	}
	var fail m16Fail
	long := longNames[0]
	job := firstID(c, `fetch('/api/v1/jobs?limit=100').then(r=>r.json()).then(js=>(js.find(j=>j.status==='succeeded'&&j.kind==='site.add')||{}).id||'')`)
	incident := firstID(c, `fetch('/api/v1/incidents').then(r=>r.json()).then(l=>l.length?String((l.find(i=>!i.closed)||l[0]).id):'')`)
	aiSession := firstID(c, `fetch('/api/v1/ai/sessions').then(r=>r.json()).then(l=>l.length?l[0].id:'')`)
	plan := firstID(c, `fetch('/api/v1/plans').then(r=>r.json()).then(l=>l.length?l[0].id:'')`)
	if job == "" || incident == "" {
		return errors.New("máy demo thiếu dữ liệu để chụp: cần một job site.add đã xong và một sự cố")
	}

	tab := func(v string) string { return "/sites/" + long + "?tab=" + v }
	shots := []m16Shot{
		{name: "dang-nhap", path: "/dang-nhap", guest: true},
		{name: "bang-dieu-khien", path: "/", narrow: true},
		{name: "cong-cu", path: "/tools", narrow: true},
		{name: "site", path: "/sites", narrow: true},
		{name: "site-tong-quan", path: "/sites/" + long, narrow: true},
		{name: "site-hong", path: "/sites/wiki.example.org"},
		{name: "site-phep-kiem", path: tab("phep-kiem")},
		{name: "site-log", path: tab("log")},
		{name: "site-php", path: tab("php")},
		{name: "site-backup", path: tab("backup")},
		{name: "site-cai-dat", path: tab("cai-dat")},
		{name: "tao-site", path: "/sites/new"},
		{name: "co-so-du-lieu", path: "/databases", narrow: true},
		{name: "ssl", path: "/ssl"},
		{name: "backup", path: "/backups"},
		{name: "cron", path: "/cron"},
		{name: "thung-rac", path: "/trash"},
		{name: "su-co", path: "/incidents"},
		{name: "su-co-chi-tiet", path: "/incidents/" + incident},
		{name: "lich-su", path: "/jobs"},
		{name: "job", path: "/jobs/" + job},
		{name: "plan-cho-duyet", path: "/plans"},
		{name: "stack", path: "/stack"},
		{name: "cap-nhat", path: "/updates"},
		{name: "bao-mat", path: "/security"},
		{name: "phien-ai", path: "/ai"},
		{name: "nguoi-dung", path: "/users", narrow: true},
		{name: "goi", path: "/packages"},
		{name: "cai-dat", path: "/settings"},
		{name: "khong-co-trang", path: "/khong-co-trang-nay"},
		{name: "user-cong-cu", path: "/tools", user: "khach_a", narrow: true},
		{name: "user-bang-dieu-khien", path: "/dashboard", user: "khach_a"},
		{name: "user-co-so-du-lieu", path: "/databases", user: "khach_a"},
		{name: "user-khong-co-quyen", path: "/users", user: "khach_a"},
	}
	// Lớp nổi (B14 của thiết kế): mỗi lớp một ảnh, cũng chạy qua mọi phép kiểm của màn.
	openWith := func(sel, ready string) func(c *cdp) error {
		return func(c *cdp) error {
			if err := c.press(sel); err != nil {
				return err
			}
			return c.waitTrue(ready, 5*time.Second)
		}
	}
	const menuOpen = `!!document.querySelector('[role=menu]')`
	shots = append(shots,
		m16Shot{name: "lop-menu-tai-khoan", path: "/tools", prep: openWith(`header button[aria-haspopup="menu"]`, menuOpen)},
		m16Shot{name: "lop-menu-ba-cham", path: "/databases", prep: openWith(`main button[aria-haspopup="menu"]`, menuOpen)},
		m16Shot{name: "lop-thanh-lenh", path: "/tools", dialog: true, prep: openWith(`header button[aria-haspopup="dialog"]`, `!!document.querySelector('[role=dialog]')`)},
		m16Shot{name: "lop-ngan-ai", path: "/tools", dialog: true, prep: openWith(`#nut-hoi-ai`, `!!document.getElementById('ngan-ai')`)},
		m16Shot{name: "lop-xem-truoc-plan", path: "/sites/new", prep: func(c *cdp) error {
			if err := c.typeInto("#o-domain", "moi.example.com"); err != nil {
				return err
			}
			if err := c.typeInto("#o-email", "admin@example.com"); err != nil {
				return err
			}
			if _, err := c.eval(`document.querySelector('form button[type=submit]').click()`); err != nil {
				return err
			}
			return c.waitTrue(`!!document.querySelector('[data-plan-run]')`, 15*time.Second)
		}},
		// Thẻ theo dõi job: backup một site (xem trước rồi chạy) ở màn Backup.
		m16Shot{name: "lop-theo-doi-job", path: "/backups", prep: func(c *cdp) error {
			if err := c.pressText("main button", "Backup ngay"); err != nil {
				return err
			}
			if err := c.waitTrue(`!!document.querySelector('[data-plan-run]')`, 15*time.Second); err != nil {
				return err
			}
			if err := c.press("[data-plan-run]"); err != nil {
				return err
			}
			return c.waitTrue(`!!document.querySelector('[data-job-tracker]')`, 15*time.Second)
		}},
	)
	if aiSession != "" {
		shots = append(shots, m16Shot{name: "phien-ai-chi-tiet", path: "/ai/" + aiSession})
	}
	if plan != "" {
		shots = append(shots, m16Shot{name: "plan-chi-tiet", path: "/plans/" + plan})
	}
	if only != "" {
		keep := map[string]bool{}
		for _, n := range strings.Split(only, ",") {
			keep[strings.TrimSpace(n)] = true
		}
		var picked []m16Shot
		for _, s := range shots {
			if keep[s.name] {
				picked = append(picked, s)
			}
		}
		if len(picked) == 0 {
			return fmt.Errorf("-chi=%q không khớp ảnh nào", only)
		}
		shots = picked
	}

	who := "\x00" // chưa biết đang là ai
	as := func(s m16Shot) error {
		want := s.user
		if s.guest {
			want = "\x01"
		}
		if want == who {
			return nil
		}
		if who != "\x00" && who != "\x01" {
			if err := c.logout(); err != nil {
				return err
			}
		}
		who = want
		switch {
		case s.guest:
			return nil
		case s.user == "":
			return c.login("", adminPW)
		default:
			pw, err := demoUserPassword(s.user)
			if err != nil {
				return err
			}
			return c.login(s.user, pw)
		}
	}
	// Lúc vào hàm, run() đã đăng nhập admin.
	who = ""

	count := 0
	shoot := func(s m16Shot, theme string, w, h int) error {
		if err := as(s); err != nil {
			return err
		}
		if err := c.viewport(w, h); err != nil {
			return err
		}
		if _, err := c.eval(fmt.Sprintf(`localStorage.setItem('tpan-theme',%q);localStorage.setItem('tpan-lang','vi')`, theme)); err != nil {
			return err
		}
		want := s.path
		if i := strings.IndexAny(want, "?#"); i >= 0 {
			want = want[:i]
		}
		if err := c.goTo(base+s.path, want); err != nil {
			// Trang chủ theo vai tự chuyển hướng: chấp nhận đường dẫn khác, miễn trang đã tải.
			if werr := c.waitTrue(`document.readyState==='complete'`, 5*time.Second); werr != nil {
				return err
			}
		}
		if s.guest {
			if err := c.waitTrue(`!!document.querySelector('h1')`, 15*time.Second); err != nil {
				return err
			}
			if _, err := c.eval(`document.fonts.ready.then(()=>true)`); err != nil {
				return err
			}
			time.Sleep(600 * time.Millisecond)
		} else if err := settle(c); err != nil {
			return err
		}
		if s.prep != nil {
			if err := s.prep(c); err != nil {
				return err
			}
		}
		suffix := map[string]string{"light": "sang", "dark": "toi"}[theme]
		file := s.name + "-" + suffix + ".png"
		if w < 700 {
			file = s.name + "-390-" + suffix + ".png"
		}
		checkPage(c, &fail, file, s.dialog)
		if _, err := c.eval(`window.scrollTo(0,0)`); err != nil {
			return err
		}
		count++
		// Soát nhanh (-chi) chụp cả trang để thấy phần dưới màn hình đầu; bộ đầy đủ chụp đúng cửa sổ.
		take := snap
		if only != "" {
			take = snapFull
		}
		if err := take(c, out, file); err != nil {
			return err
		}
		// Sau khi chụp, để script đo không làm đổi ảnh.
		checkMeasure(c, &fail, file)
		return nil
	}

	for _, theme := range []string{"light", "dark"} {
		for _, s := range shots {
			if err := shoot(s, theme, width, height); err != nil {
				return fmt.Errorf("%s (%s): %w", s.name, theme, err)
			}
		}
		for _, s := range shots {
			if !s.narrow {
				continue
			}
			if err := shoot(s, theme, 390, 844); err != nil {
				return fmt.Errorf("%s (390, %s): %w", s.name, theme, err)
			}
		}
	}

	if only != "" {
		fmt.Printf("\nĐã chụp %d ảnh vào %s (chỉ các ảnh của -chi, bỏ phần bàn phím và tiếng Anh). Không đạt: %d\n", count, out, len(fail.list))
		if len(fail.list) > 0 {
			return fmt.Errorf("có %d điều không đạt", len(fail.list))
		}
		return nil
	}

	// Từ đây không chụp nữa: kiểm bàn phím và bản tiếng Anh, bằng admin.
	if err := as(m16Shot{}); err != nil {
		return err
	}
	if err := c.viewport(width, height); err != nil {
		return err
	}
	open := func(path, lang string) error {
		_ = c.consoleErrors() // lỗi của màn trước (đã báo hoặc không thuộc phần kiểm này) không tính cho màn sắp mở
		if _, err := c.eval(fmt.Sprintf(`localStorage.setItem('tpan-theme','light');localStorage.setItem('tpan-lang',%q)`, lang)); err != nil {
			return err
		}
		want := path
		if i := strings.IndexAny(want, "?#"); i >= 0 {
			want = want[:i]
		}
		if err := c.goTo(base+path, want); err != nil {
			return err
		}
		return settle(c)
	}

	// Mọi màn của danh sách ảnh (trừ trang đăng nhập và ảnh lớp nổi): bàn phím và tiếng Anh.
	var pages []m16Shot
	for _, s := range shots {
		if !s.guest && s.prep == nil {
			pages = append(pages, s)
		}
	}
	fmt.Println("\nBàn phím (1440 × 900, tiếng Việt), mọi màn:")
	for _, s := range pages {
		if err := as(s); err != nil {
			return err
		}
		if err := open(s.path, "vi"); err != nil {
			return fmt.Errorf("bàn phím %s: %w", s.name, err)
		}
		if err := tabWalk(c, &fail, s.name); err != nil {
			return fmt.Errorf("bàn phím %s: %w", s.name, err)
		}
	}
	if err := as(m16Shot{}); err != nil {
		return err
	}
	// Menu ba chấm của hàng đầu ở màn Cơ sở dữ liệu: Enter mở, mũi tên xuống đi, Escape đóng, focus về nút.
	if err := open("/databases", "vi"); err != nil {
		return err
	}
	_ = c.consoleErrors()
	const kebab = `document.querySelector('main button[aria-haspopup="menu"]')`
	if v, _ := c.eval(`!!` + kebab); v != true {
		fail.add("bàn phím: màn Cơ sở dữ liệu không có nút ba chấm nào")
	} else {
		if _, err := c.eval(kebab + `.focus()`); err != nil {
			return err
		}
		if err := c.key("Enter"); err != nil {
			return err
		}
		opened := c.waitTrue(`!!document.querySelector('[role=menu]')`, 5*time.Second) == nil
		first := c.str(`document.activeElement.textContent.trim()`)
		_ = c.key("ArrowDown")
		second := c.str(`document.activeElement.textContent.trim()`)
		_ = c.key("Escape")
		closed := c.waitTrue(`!document.querySelector('[role=menu]')`, 5*time.Second) == nil
		back, _ := c.eval(`document.activeElement === ` + kebab)
		errs := c.consoleErrors()
		if !opened || !closed || back != true || first == "" || len(errs) > 0 {
			fail.add("bàn phím: menu ba chấm: mở=%v, mục đầu «%s», mục kế «%s», đóng=%v, focus về nút=%v, lỗi console=%d", opened, first, second, closed, back, len(errs))
		} else {
			fmt.Printf("  bàn phím menu ba chấm: Enter mở, tới «%s», mũi tên xuống tới «%s», Escape đóng, focus về nút, 0 lỗi console\n", first, second)
		}
	}
	// Tab của trang một site: mũi tên phải đổi tab.
	if err := open("/sites/"+long, "vi"); err != nil {
		return err
	}
	const selTab = `document.querySelector('[role=tab][aria-selected="true"]')?.getAttribute('data-tab')||''`
	before := c.str(selTab)
	if _, err := c.eval(`document.querySelector('[role=tab][aria-selected="true"]')?.focus()`); err != nil {
		return err
	}
	_ = c.key("ArrowRight")
	after := c.str(selTab)
	if before == "" || after == "" || before == after {
		fail.add("bàn phím: tab của trang site không đổi bằng mũi tên phải (trước «%s», sau «%s»)", before, after)
	} else {
		fmt.Printf("  bàn phím tab: mũi tên phải đổi từ %s sang %s\n", before, after)
	}

	fmt.Println("\nTiếng Anh (không cuộn ngang, không chữ bị cắt mà thiếu title), mọi màn:")
	for _, size := range [][2]int{{width, height}, {390, 844}} {
		if err := c.viewport(size[0], size[1]); err != nil {
			return err
		}
		for _, s := range pages {
			if err := as(s); err != nil {
				return err
			}
			what := fmt.Sprintf("tiếng Anh %s ở %d px", s.name, size[0])
			if err := open(s.path, "en"); err != nil {
				return fmt.Errorf("%s: %w", what, err)
			}
			n := len(fail.list)
			checkPage(c, &fail, what, false)
			if err := overflowText(c, &fail, what); err != nil {
				return err
			}
			if len(fail.list) == n {
				fmt.Printf("  %s: đạt\n", what)
			}
		}
	}
	if _, err := c.eval(`localStorage.setItem('tpan-lang','vi')`); err != nil {
		return err
	}

	fmt.Printf("\nĐã chụp %d ảnh vào %s. Không đạt: %d\n", count, out, len(fail.list))
	for _, s := range fail.list {
		fmt.Println("  -", s)
	}
	if len(fail.list) > 0 {
		return fmt.Errorf("bộ ảnh M16 có %d điều không đạt", len(fail.list))
	}
	return nil
}
