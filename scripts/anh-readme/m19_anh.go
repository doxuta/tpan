package main

// Bộ ảnh của mốc M19 (tên phụ, chuyển hướng theo đường dẫn) trên máy demo:
//
//	-bo m19   các màn của M19 ở ba khổ: 1440 × 900 sáng, tối, và 390 × 844
//	          sáng: tab Tên miền và tab Chuyển hướng của trang một site, màn
//	          /domains và trang Công cụ (hai ô mới). Mỗi ảnh kiểm như bộ m16 và
//	          m18: không lỗi console, cả trang không cuộn ngang, không chữ dưới
//	          12 px, không thứ bấm được bị cắt, mọi ô nhập có nhãn. Sau đó đi
//	          bàn phím (Tab) trên từng màn và mở bản tiếng Anh tìm chữ bị tràn.
//
// Chạy qua Makefile: `make anh-m19` (ảnh vào docs/bang-chung/M19).
//
// Máy demo chưa có tên phụ hay luật nào: chương trình tạo qua API (như
// ensureLongSites của m16, mỗi việc một job, chờ xong rồi mới làm việc sau)
// cho site -site (mặc định example.com): các tên www.example.com, example.net,
// nodns.example.invalid và hai luật /ban-cu, /khuyen-mai. Chạy lại được: việc
// đã có thì bỏ qua. Cột DNS và Chứng chỉ của demo do máy giả trả lời, nên ảnh
// không khẳng định DNS "đạt hay chưa" (chỉ e2e giao diện trên máy ảo thật khẳng định).

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// m19Job gọi một API ghi (POST) của panel bằng fetch cùng origin và chờ job xong.
// Trả "succeeded" hoặc lời lỗi (HTTP và thân, không có bí mật vì demo).
func m19Job(c *cdp, path, body string) (string, error) {
	v, err := c.eval(fmt.Sprintf(`(async()=>{
		const r = await fetch(%q,{method:'POST',headers:{'Content-Type':'application/json'},body:%q});
		if (!r.ok) return 'HTTP '+r.status+' '+(await r.text()).slice(0,200);
		const id = (await r.json()).job_id;
		for (let i=0;i<300;i++) {
			const j = await fetch('/api/v1/jobs/'+id).then(r=>r.json());
			if (j.job.status!=='running') return j.job.status;
			await new Promise(r=>setTimeout(r,200));
		}
		return 'hết giờ';
	})()`, path, body))
	if err != nil {
		return "", err
	}
	return fmt.Sprint(v), nil
}

// ensureM19Demo tạo tên phụ và luật chuyển hướng còn thiếu cho site.
func ensureM19Demo(c *cdp, site string) error {
	names := []struct{ name, mode string }{
		{"www." + site, "redirect"}, {m19TenMoi, "serve"}, {m19TenChua, "serve"},
	}
	for _, n := range names {
		have := c.str(fmt.Sprintf(`fetch('/api/v1/sites/%s/names').then(r=>r.json()).then(l=>String(l.some(x=>x.name===%q)))`, site, n.name))
		if have == "true" {
			fmt.Printf("tên %s của %s: có sẵn\n", n.name, site)
			continue
		}
		st, err := m19Job(c, "/api/v1/sites/"+site+"/names", fmt.Sprintf(`{"name":%q,"mode":%q}`, n.name, n.mode))
		if err != nil {
			return fmt.Errorf("thêm tên %s: %w", n.name, err)
		}
		if st != "succeeded" {
			return fmt.Errorf("thêm tên %s cho %s: %s (máy demo chưa nhận được thao tác tên phụ?)", n.name, site, st)
		}
		fmt.Printf("tên %s của %s: %s\n", n.name, site, st)
	}
	rules := []struct{ src, dst, code, keep string }{
		{"/ban-cu", "/ban-moi", "301", "false"}, {"/khuyen-mai", "https://example.org/sale", "302", "true"},
	}
	for _, r := range rules {
		have := c.str(fmt.Sprintf(`fetch('/api/v1/sites/%s/redirects').then(r=>r.json()).then(l=>String(l.some(x=>x.source===%q)))`, site, r.src))
		if have == "true" {
			fmt.Printf("luật %s của %s: có sẵn\n", r.src, site)
			continue
		}
		st, err := m19Job(c, "/api/v1/sites/"+site+"/redirects", fmt.Sprintf(`{"source":%q,"target":%q,"code":%s,"keep_query":%s}`, r.src, r.dst, r.code, r.keep))
		if err != nil {
			return fmt.Errorf("thêm luật %s: %w", r.src, err)
		}
		if st != "succeeded" {
			return fmt.Errorf("thêm luật %s cho %s: %s", r.src, site, st)
		}
		fmt.Printf("luật %s của %s: %s\n", r.src, site, st)
	}
	return nil
}

// checkLabels khẳng định mọi ô nhập, ô chọn, vùng chữ thấy được trong <main> có nhãn đọc được:
// <label> (for hay bọc), aria-label hoặc aria-labelledby.
func checkLabels(c *cdp, f *m16Fail, what string) {
	v, err := c.eval(`(()=>{
		const out=[];
		for (const e of document.querySelectorAll('main input, main select, main textarea')) {
			if (e.type==='hidden') continue;
			const r=e.getBoundingClientRect(), s=getComputedStyle(e);
			if (r.width<=1 || r.height<=1 || s.visibility==='hidden' || s.display==='none') continue;
			const ok=(e.labels&&e.labels.length>0) || e.getAttribute('aria-label') || e.getAttribute('aria-labelledby');
			if (!ok) out.push(e.tagName.toLowerCase()+'#'+(e.id||'?'));
		}
		return out.slice(0,6).join(', ');
	})()`)
	if err != nil {
		f.add("%s: không chạy được phép kiểm nhãn: %v", what, err)
		return
	}
	if s := fmt.Sprint(v); s != "" {
		f.add("%s: ô nhập không có nhãn: %s", what, s)
	}
}

func photosM19(c *cdp, base, out, only, siteFlag string) error {
	if err := c.watchConsole(); err != nil {
		return err
	}
	var fail m16Fail
	site := defaultStr(siteFlag, "example.com")
	if err := ensureM19Demo(c, site); err != nil {
		return err
	}

	tab := func(v string) string { return "/sites/" + site + "?tab=" + v }
	shots := []m16Shot{
		{name: "ten-mien", path: tab("ten-mien")},
		{name: "ten-mien-xem-truoc", path: tab("ten-mien"), prep: func(c *cdp) error {
			if err := fillID(c, "#ten-mien-ten", "www.example.net"); err != nil {
				return err
			}
			if err := selectValue(c, "#ten-mien-kieu", "serve"); err != nil {
				return err
			}
			if err := c.press(m19ThemTen + ` button[type=submit]`); err != nil {
				return err
			}
			return c.waitTrue(`!!document.querySelector('[data-plan-run]:not([disabled])')`, 30*time.Second)
		}},
		{name: "chuyen-huong", path: tab("chuyen-huong")},
		{name: "chuyen-huong-loi", path: tab("chuyen-huong"), prep: func(c *cdp) error {
			if err := fillID(c, "#chuyen-huong-nguon", "/a b"); err != nil {
				return err
			}
			if err := fillID(c, "#chuyen-huong-dich", "/ban-moi"); err != nil {
				return err
			}
			if err := c.press(m19ThemLuat + ` button[type=submit]`); err != nil {
				return err
			}
			return c.waitTrue(`document.querySelector('#chuyen-huong-nguon')?.getAttribute('aria-invalid')==='true'`, 30*time.Second)
		}},
		{name: "ten-mien-ds", path: "/domains"},
		{name: "ten-mien-ds-luat", path: "/domains?tab=chuyen-huong"},
		{name: "cong-cu", path: "/tools"},
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

	// Điều phải thấy trên ảnh, ngoài các phép kiểm chung.
	h2Is := func(section, want string) error {
		got := c.str(`document.querySelector('` + section + ` h2')?.innerText.replace(/\s+\d+ (tên|luật)$/,'').trim()||''`)
		if got != want {
			return fmt.Errorf("thẻ %s: tiêu đề %q, chờ %q", section, got, want)
		}
		return nil
	}
	rowsAtLeast := func(section string, n int) error {
		if got := len(tableRows(c, section)); got < n {
			return fmt.Errorf("bảng %s có %d dòng, chờ ít nhất %d", section, got, n)
		}
		return nil
	}
	hasLink := func(href string) error {
		if c.str(fmt.Sprintf(`document.querySelector('main a[href=%q]')?'có':''`, href)) == "" {
			return fmt.Errorf("không có liên kết tới %s", href)
		}
		return nil
	}
	expect := map[string]func() error{
		"ten-mien": func() error {
			if err := h2Is(m19BangTen, m19Nhan.tenBang); err != nil {
				return err
			}
			if err := h2Is(m19ThemTen, m19Nhan.themTen); err != nil {
				return err
			}
			if nameRow(tableRows(c, m19BangTen), m19TenMoi) == nil {
				return fmt.Errorf("bảng tên không có dòng %s", m19TenMoi)
			}
			return rowsAtLeast(m19BangTen, 3)
		},
		"ten-mien-xem-truoc": func() error {
			if c.str(`document.querySelector('[data-plan-run]')?'có':''`) == "" {
				return errors.New("không có bản xem trước (nút chạy)")
			}
			return nil
		},
		"chuyen-huong": func() error {
			if err := h2Is(m19BangLuat, m19Nhan.luatBang); err != nil {
				return err
			}
			if err := h2Is(m19ThemLuat, m19Nhan.themLuat); err != nil {
				return err
			}
			if cellRow(tableRows(c, m19BangLuat), "/ban-cu") == nil {
				return errors.New("bảng luật không có dòng /ban-cu")
			}
			return rowsAtLeast(m19BangLuat, 2)
		},
		"chuyen-huong-loi": func() error {
			if c.str(`document.querySelector('#chuyen-huong-nguon')?.parentElement.querySelector('[role=alert]')?.textContent.trim()||''`) == "" {
				return errors.New("không có lời lỗi dưới ô nguồn")
			}
			return nil
		},
		"ten-mien-ds": func() error {
			if got := c.str(`document.querySelector('main h1')?.innerText.trim()||''`); got != m19Nhan.domainsH1 {
				return fmt.Errorf("h1 là %q, chờ %q", got, m19Nhan.domainsH1)
			}
			if err := hasLink("/sites/" + site + "?tab=ten-mien"); err != nil {
				return err
			}
			return rowsAtLeast("main section", 1)
		},
		"ten-mien-ds-luat": func() error {
			if err := hasLink("/sites/" + site + "?tab=chuyen-huong"); err != nil {
				return err
			}
			if got := c.str(`document.querySelector('main h1')?.innerText.trim()||''`); got != m19Nhan.oChuyenHuong {
				return fmt.Errorf("h1 là %q, chờ %q", got, m19Nhan.oChuyenHuong)
			}
			return nil
		},
		"cong-cu": func() error {
			if err := hasLink("/domains?tab=ten-mien"); err != nil {
				return err
			}
			if err := hasLink("/domains?tab=chuyen-huong"); err != nil {
				return err
			}
			text := c.str(`document.querySelector('main')?.innerText||''`)
			for _, want := range []string{m19Nhan.oTenMienPhu, m19Nhan.oChuyenHuong} {
				if !strings.Contains(text, want) {
					return fmt.Errorf("trang Công cụ không có ô «%s»", want)
				}
			}
			return nil
		},
	}

	open := func(path, theme, lang string) error {
		_ = c.consoleErrors() // lỗi của màn trước không tính cho màn sắp mở
		if _, err := c.eval(fmt.Sprintf(`localStorage.setItem('tpan-theme',%q);localStorage.setItem('tpan-lang',%q)`, theme, lang)); err != nil {
			return err
		}
		p := path
		if i := strings.IndexAny(p, "?#"); i >= 0 {
			p = p[:i]
		}
		if err := c.goTo(base+path, p); err != nil {
			return err
		}
		return settle(c)
	}

	// Ba khổ: 1440 sáng, 1440 tối, 390 sáng.
	sizes := []struct {
		theme, suffix string
		w, h          int
	}{{"light", "sang", width, height}, {"dark", "toi", width, height}, {"light", "390-sang", 390, 844}}
	count := 0
	for _, sz := range sizes {
		if err := c.viewport(sz.w, sz.h); err != nil {
			return err
		}
		for _, s := range shots {
			file := s.name + "-" + sz.suffix + ".png"
			if err := open(s.path, sz.theme, "vi"); err != nil {
				return fmt.Errorf("%s: %w", file, err)
			}
			if s.prep != nil {
				if err := s.prep(c); err != nil {
					return fmt.Errorf("%s: %w", file, err)
				}
				time.Sleep(400 * time.Millisecond) // bản xem trước, lời lỗi vừa hiện
			}
			// Lời 400 của máy chủ (nguồn sai) hiện một dòng console "Failed to load resource": đã chờ trước.
			if s.name == "chuyen-huong-loi" {
				for _, e := range c.consoleErrors() {
					if !strings.Contains(e, "400") {
						fail.add("%s: lỗi console: %s", file, e)
					}
				}
			} else {
				checkPage(c, &fail, file, false)
			}
			checkClipped(c, &fail, file)
			checkLabels(c, &fail, file)
			if chk := expect[s.name]; chk != nil {
				if err := chk(); err != nil {
					fail.add("%s: %v", file, err)
				}
			}
			if _, err := c.eval(`window.scrollTo(0,0)`); err != nil {
				return err
			}
			time.Sleep(200 * time.Millisecond)
			if err := snapFull(c, out, file); err != nil {
				return err
			}
			count++
			checkMeasure(c, &fail, file)
		}
	}

	if only != "" {
		fmt.Printf("\nĐã chụp %d ảnh vào %s (chỉ các ảnh của -chi, bỏ phần bàn phím và tiếng Anh). Không đạt: %d\n", count, out, len(fail.list))
		if len(fail.list) > 0 {
			return fmt.Errorf("có %d điều không đạt", len(fail.list))
		}
		return nil
	}

	// Bàn phím: mọi màn, kể cả hai trạng thái có bản xem trước và có lời lỗi.
	if err := c.viewport(width, height); err != nil {
		return err
	}
	fmt.Println("\nBàn phím (1440 × 900, tiếng Việt):")
	for _, s := range shots {
		if err := open(s.path, "light", "vi"); err != nil {
			return fmt.Errorf("bàn phím %s: %w", s.name, err)
		}
		if s.prep != nil {
			if err := s.prep(c); err != nil {
				return fmt.Errorf("bàn phím %s: %w", s.name, err)
			}
			// Sau cú bấm của prep, Chrome bắt đầu vòng Tab từ nút vừa bấm (giữa
			// trang); đưa điểm bắt đầu về đầu trang như lúc vừa mở màn.
			if _, err := c.eval(`(()=>{const e=document.body.firstElementChild; e.setAttribute('tabindex','-1'); e.focus(); e.blur(); e.removeAttribute('tabindex'); return true})()`); err != nil {
				return err
			}
		}
		if err := tabWalk(c, &fail, s.name); err != nil {
			return fmt.Errorf("bàn phím %s: %w", s.name, err)
		}
	}

	// Tiếng Anh: mọi ảnh; form dùng id nên chạy được ở cả hai thứ tiếng.
	fmt.Println("\nTiếng Anh (không cuộn ngang, không chữ hay nút bị cắt):")
	for _, size := range [][2]int{{width, height}, {390, 844}} {
		if err := c.viewport(size[0], size[1]); err != nil {
			return err
		}
		for _, s := range shots {
			what := fmt.Sprintf("tiếng Anh %s ở %d px", s.name, size[0])
			if err := open(s.path, "light", "en"); err != nil {
				return fmt.Errorf("%s: %w", what, err)
			}
			if s.prep != nil {
				if err := s.prep(c); err != nil {
					return fmt.Errorf("%s: %w", what, err)
				}
			}
			n := len(fail.list)
			if s.name == "chuyen-huong-loi" {
				_ = c.consoleErrors() // dòng 400 chờ trước, như ở trên
			} else {
				checkPage(c, &fail, what, false)
			}
			checkClipped(c, &fail, what)
			checkLabels(c, &fail, what)
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
		return errors.New("bộ ảnh M19 có điều không đạt")
	}
	return nil
}
