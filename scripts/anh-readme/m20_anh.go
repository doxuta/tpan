package main

// Bộ ảnh của mốc M20 (đăng nhập hai bước, token API, chứng chỉ panel), mục 7.4
// của docs/thay-doi/M20-thiet-ke.md:
//
//	-bo m20   trên máy demo: trang Tài khoản (chưa bật, đang bật với mã QR,
//	          10 mã dự phòng, đã bật), hộp tạo token và token vừa tạo, thẻ mới ở
//	          Cài đặt, trang Công cụ, trang bật hai bước bắt buộc, màn Đăng nhập
//	          chặng hai (thường, mã sai, đã vào sau một lần sai). Mỗi ảnh ở ba khổ:
//	          1440 sáng, 1440 tối, 390 sáng. Mỗi lần chụp kiểm như bộ m18: không lỗi
//	          console, không cuộn ngang, không thứ bấm được nào bị cắt, cỡ chữ.
//	          Ba màn không trạng thái (Tài khoản, Cài đặt, Công cụ) còn được đi
//	          bằng bàn phím và mở bằng tiếng Anh.
//
// Chạy qua Makefile: `make anh-m20` (ảnh vào docs/bang-chung/M20).
//
// Máy demo (M20): khach_a đã bật hai bước, `tpan serve --demo` in bí mật của
// khach_a lúc khởi động ("Bí mật hai bước demo của khach_a: <base32>") để bộ này
// đăng nhập qua chặng hai được; admin có token mẫu. Bộ này ĐỔI máy demo theo
// thứ tự (bật hai bước cho admin, đặt chính sách "all"): các ảnh cần máy chưa
// đổi đứng trước. Máy demo chỉ sống trong lần chạy.
//
// Trạng thái hiện một lần (10 mã dự phòng, token vừa tạo, thẻ "Đã đăng nhập")
// không dựng lại được cho từng khổ: chụp cả ba khổ tại chỗ, đổi sáng tối bằng
// data-theme trên <html> (đúng việc web/public/theme.js làm lúc tải trang) và đổi
// khổ bằng Emulation, không tải lại trang.

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// demoTOTP giữ bí mật hai bước của user mẫu mà `tpan serve --demo` in ra.
var demoTOTP = struct {
	sync.Mutex
	m map[string]string
}{m: map[string]string{}}

var demoTOTPLine = regexp.MustCompile(`(?i)(hai bước|two-step).*?([a-z0-9_]+)\s*:\s*([A-Z2-7]{32})\s*$`)

// noteDemoSecret nhận một dòng stdout của serve --demo; là dòng bí mật hai bước
// demo ("Bí mật hai bước demo của khach_a: <base32>") thì nhớ lại và trả true
// (dòng đó không phải mật khẩu admin, main.go bỏ qua nó). Gọi từ main.go.
func noteDemoSecret(line string) bool {
	m := demoTOTPLine.FindStringSubmatch(strings.TrimSpace(line))
	if m == nil {
		return false
	}
	demoTOTP.Lock()
	demoTOTP.m[m[2]] = m[3]
	demoTOTP.Unlock()
	return true
}

func demoSecret(name string) (string, error) {
	for i := 0; i < 50; i++ {
		demoTOTP.Lock()
		s := demoTOTP.m[name]
		demoTOTP.Unlock()
		if s != "" {
			return s, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return "", fmt.Errorf("serve --demo không in bí mật hai bước của %s", name)
}

// loginStatus gọi POST /api/v1/login bằng fetch cùng origin, trả mã HTTP (204:
// vào luôn; 200: cần chặng hai).
func (c *cdp) loginStatus(name, password string) string {
	js := fmt.Sprintf(`fetch('/api/v1/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({name:%q,password:%q})}).then(r=>String(r.status)).catch(e=>'lỗi '+e)`, name, password)
	return c.str(js)
}

// postJSON gọi một API POST cùng origin, trả mã HTTP.
func (c *cdp) postJSON(path, body string) string {
	return c.str(fmt.Sprintf(`fetch(%q,{method:'POST',headers:{'Content-Type':'application/json'},body:%q}).then(r=>String(r.status)).catch(e=>'lỗi '+e)`, path, body))
}

// m20Shot là một ảnh của bộ M20.
type m20Shot struct {
	name string // tên file, không đuôi; thêm "-sang", "-toi", "-390-sang"
	path string
	// before chạy một lần trước khi mở màn (đổi người đăng nhập, đổi máy demo).
	before func() error
	// prep chạy sau khi màn tải xong (bấm nút, gõ form); với inPlace chỉ chạy một lần.
	prep func() error
	// inPlace: trạng thái hiện một lần, chụp cả ba khổ tại chỗ.
	inPlace bool
	// dialog: có hộp thoại Radix đang mở (bỏ qua dòng CSP đã biết, chụp đúng khung cửa sổ).
	dialog bool
	// simple: màn không trạng thái; đi thêm bàn phím và tiếng Anh.
	simple bool
	// want401: màn cố ý gửi một mã sai, nên đúng một dòng "401" của chính
	// request đó trong console là điều mong đợi, không phải lỗi.
	want401 string
	expect  func() error
}

func photosM20(c *cdp, base, adminPW, out, only string) error {
	if err := c.watchConsole(); err != nil {
		return err
	}
	var fail m16Fail
	k := serverClock{} // máy demo chạy ngay trên Mac: không lệch giờ
	mainHas := func(label string) error {
		if !strings.Contains(c.str(`document.querySelector('main')?.innerText||''`), label) {
			return fmt.Errorf("màn không có chữ «%s»", label)
		}
		return nil
	}
	has := func(js, what string) error {
		if c.str(`(`+js+`)?'có':''`) == "" {
			return errors.New(what)
		}
		return nil
	}
	waitSecret := func() error {
		return c.waitTrue(`(()=>{for(const e of document.querySelectorAll('button')){if(/^[A-Z2-7]{32}$/.test((e.textContent||'').replace(/\s+/g,''))) return true} return false})()`, 15*time.Second)
	}
	openTokenDialog := func() error {
		if err := c.pressText("main button", m20Nhan.taoToken); err != nil {
			return err
		}
		return c.waitTrue(`!!document.querySelector('[role=dialog] #tk-ten')`, 10*time.Second)
	}
	switchUser := func(name, pw string, want string) error {
		_ = c.logout()
		if pw == "" {
			var err error
			if pw, err = demoUserPassword(name); err != nil {
				return err
			}
		}
		if st := c.loginStatus(name, pw); st != want {
			return fmt.Errorf("đăng nhập %s: trạng thái %s, mong %s", name, st, want)
		}
		return nil
	}
	first := []m20Shot{
		{name: "tai-khoan", path: "/account", simple: true, expect: func() error {
			if err := has(`document.getElementById('hai-buoc') && document.getElementById('token')`, "không có thẻ #hai-buoc và #token"); err != nil {
				return err
			}
			return mainHas(m20Nhan.batHaiBuoc)
		}},
		{name: "tai-khoan-bat", path: "/account", prep: func() error {
			if err := c.pressText("main button", m20Nhan.batHaiBuoc); err != nil {
				return err
			}
			return waitSecret()
		}, expect: func() error {
			return has(`document.querySelector('main svg[role=img] path') && document.getElementById('hb-ma')`, "không có mã QR hay ô mã")
		}},
		{name: "tai-khoan-tao-token", path: "/account", dialog: true, prep: openTokenDialog, expect: func() error {
			return has(`document.querySelectorAll('[role=dialog] input[type=checkbox]').length>=2 && document.getElementById('tk-ngay')`, "hộp tạo token thiếu ô chọn phạm vi hay ô số ngày")
		}},
		// Hộp tạo token (Radix Dialog) vừa mở rồi đóng ở bước chuẩn bị: dòng CSP
		// đã biết của nó còn trong console.
		{name: "tai-khoan-token-moi", path: "/account", inPlace: true, dialog: true, prep: func() error {
			if err := openTokenDialog(); err != nil {
				return err
			}
			if err := c.typeInto("#tk-ten", "ci-sao-luu"); err != nil {
				return err
			}
			if _, err := c.eval(`document.querySelector('[role=dialog] input[type=checkbox]').click()`); err != nil {
				return err
			}
			if err := c.typeInto("#tk-mat-khau", adminPW); err != nil {
				return err
			}
			if err := c.pressText("[role=dialog] button", m20Nhan.taoToken); err != nil {
				return err
			}
			return c.waitTrue(fmt.Sprintf(`[...document.querySelectorAll('main button')].some(b=>b.textContent.trim()===%q)`, m20Nhan.daLuuToken), 15*time.Second)
		}, expect: func() error {
			return has(`[...document.querySelectorAll('main code')].some(e=>/tpan_[a-z2-7]{39}/.test(e.textContent))`, "không thấy token vừa tạo (tpan_…) trên trang")
		}},
		{name: "cai-dat", path: "/settings", simple: true, expect: func() error {
			return has(`document.getElementById('chinh-sach-tieu-de') && document.getElementById('token-admin-tieu-de') && document.getElementById('ten-mien-panel-tieu-de')`,
				"Cài đặt thiếu thẻ chính sách hai bước, token hay tên miền của panel")
		}},
		{name: "cong-cu", path: "/tools", simple: true, expect: func() error {
			return has(`document.querySelector('main a[href="/account#hai-buoc"]') && document.querySelector('main a[href="/account#token"]')`,
				"trang Công cụ thiếu ô Hai bước (/account#hai-buoc) hay Token API (/account#token)")
		}},
	}

	rest := []m20Shot{
		// Admin bật hai bước: 10 mã dự phòng hiện một lần.
		{name: "ma-du-phong", path: "/account", inPlace: true, prep: func() error {
			if err := c.pressText("main button", m20Nhan.batHaiBuoc); err != nil {
				return err
			}
			if err := waitSecret(); err != nil {
				return err
			}
			key, err := totpKey(readSecret(c))
			if err != nil {
				return err
			}
			code, _ := codeAfter(k, key, 0)
			if err := c.typeInto("#hb-ma", code); err != nil {
				return err
			}
			if err := c.typeInto("#hb-mat-khau", adminPW); err != nil {
				return err
			}
			if err := c.keyRaw("Enter"); err != nil {
				return err
			}
			return c.waitTrue(`document.querySelectorAll('ul[aria-label] li').length===10`, 15*time.Second)
		}, expect: func() error { return mainHas(m20Nhan.daLuuMa) }},
		{name: "tai-khoan-da-bat", path: "/account", expect: func() error { return mainHas(m20Nhan.taoLaiMa) }},
		// Chính sách "all": khach_b (chưa bật) chỉ vào được /bat-hai-buoc.
		{name: "bat-hai-buoc", path: "/bat-hai-buoc", before: func() error {
			if st := c.postJSON("/api/v1/settings/auth", `{"require_2fa":"all"}`); st != "200" {
				return fmt.Errorf("đặt chính sách all: trạng thái %s", st)
			}
			return switchUser("khach_b", "", "204")
		}, expect: func() error { return mainHas(m20Nhan.batHaiBuoc) }},
		{name: "bat-hai-buoc-qr", path: "/bat-hai-buoc", prep: func() error {
			if err := c.pressText("main button", m20Nhan.batHaiBuoc); err != nil {
				return err
			}
			return waitSecret()
		}, expect: func() error {
			return has(`document.querySelector('main svg[role=img] path')`, "không có mã QR")
		}},
		// khach_a (đã bật): chặng hai của màn Đăng nhập.
		{name: "dang-nhap-buoc-hai", path: "/dang-nhap", before: func() error { return switchUser("khach_a", "", "200") },
			expect: func() error {
				return has(`document.getElementById('ma-hai-buoc')`, "không có ô mã của chặng hai")
			}},
		{name: "dang-nhap-ma-sai", path: "/dang-nhap", inPlace: true, want401: "/api/v1/login/2fa", prep: func() error {
			s, err := demoSecret("khach_a")
			if err != nil {
				return err
			}
			key, err := totpKey(s)
			if err != nil {
				return err
			}
			if err := c.typeInto("#ma-hai-buoc", wrongCode(k, key)); err != nil {
				return err
			}
			if err := c.keyRaw("Enter"); err != nil {
				return err
			}
			return c.waitTrue(fmt.Sprintf(`[...document.querySelectorAll('[role=alert]')].some(e=>e.textContent.includes(%q))`, m20Nhan.maSai), 10*time.Second)
		}},
		{name: "dang-nhap-da-vao", path: "/dang-nhap", inPlace: true, prep: func() error {
			s, err := demoSecret("khach_a")
			if err != nil {
				return err
			}
			key, err := totpKey(s)
			if err != nil {
				return err
			}
			code, _ := codeAfter(k, key, 0)
			if err := c.typeInto("#ma-hai-buoc", code); err != nil {
				return err
			}
			if err := c.keyRaw("Enter"); err != nil {
				return err
			}
			return c.waitTrue(fmt.Sprintf(`(document.querySelector('main h1')?.innerText||'').includes(%q)`, m20Nhan.daDangNhap), 15*time.Second)
		}, expect: func() error { return mainHas(m20Nhan.vaoTPAN) }},
	}
	if only != "" {
		keep := map[string]bool{}
		for _, n := range strings.Split(only, ",") {
			keep[strings.TrimSpace(n)] = true
		}
		pick := func(l []m20Shot) []m20Shot {
			var p []m20Shot
			for _, s := range l {
				if keep[s.name] {
					p = append(p, s)
				}
			}
			return p
		}
		first, rest = pick(first), pick(rest)
		if len(first)+len(rest) == 0 {
			return fmt.Errorf("-chi=%q không khớp ảnh nào", only)
		}
	}

	open := func(path, theme, lang string) error {
		_ = c.consoleErrors() // lỗi của màn trước không tính cho màn sắp mở
		if _, err := c.eval(fmt.Sprintf(`localStorage.setItem('tpan-theme',%q);localStorage.setItem('tpan-lang',%q)`, theme, lang)); err != nil {
			return err
		}
		if err := c.goTo(base+path, path); err != nil {
			return err
		}
		return settle(c)
	}
	sizes := []struct {
		theme, suffix string
		w, h          int
	}{{"light", "sang", width, height}, {"dark", "toi", width, height}, {"light", "390-sang", 390, 844}}

	count := 0
	take := func(s m20Shot, file string) error {
		if s.want401 != "" {
			// Lấy hết dòng console ra trước (consoleErrors xả bộ đệm), bỏ đúng
			// dòng 401 mong đợi, các dòng khác vẫn là lỗi.
			for _, e := range c.consoleErrors() {
				if strings.Contains(e, "status of 401") && strings.Contains(e, s.want401) {
					continue
				}
				fail.add("%s: lỗi console: %s", file, e)
			}
		}
		checkPage(c, &fail, file, s.dialog)
		checkClipped(c, &fail, file)
		if s.expect != nil {
			if err := s.expect(); err != nil {
				fail.add("%s: %v", file, err)
			}
		}
		snapper := snapFull
		if s.dialog {
			snapper = snap
		} else {
			if _, err := c.eval(`window.scrollTo(0,0)`); err != nil {
				return err
			}
			time.Sleep(200 * time.Millisecond)
		}
		if err := snapper(c, out, file); err != nil {
			return err
		}
		count++
		checkMeasure(c, &fail, file)
		return nil
	}
	shootAll := func(list []m20Shot) error {
		for _, s := range list {
			if s.before != nil {
				if err := s.before(); err != nil {
					return fmt.Errorf("%s: %w", s.name, err)
				}
			}
			if s.inPlace {
				if err := c.viewport(width, height); err != nil {
					return err
				}
				if err := open(s.path, "light", "vi"); err != nil {
					return fmt.Errorf("%s: %w", s.name, err)
				}
				if s.prep != nil {
					if err := s.prep(); err != nil {
						return fmt.Errorf("%s: %w", s.name, err)
					}
				}
				for _, sz := range sizes {
					if _, err := c.eval(fmt.Sprintf(`document.documentElement.setAttribute('data-theme',%q);localStorage.setItem('tpan-theme',%q)`, sz.theme, sz.theme)); err != nil {
						return err
					}
					if err := c.viewport(sz.w, sz.h); err != nil {
						return err
					}
					time.Sleep(500 * time.Millisecond)
					if err := take(s, s.name+"-"+sz.suffix+".png"); err != nil {
						return err
					}
				}
				continue
			}
			for _, sz := range sizes {
				if err := c.viewport(sz.w, sz.h); err != nil {
					return err
				}
				if err := open(s.path, sz.theme, "vi"); err != nil {
					return fmt.Errorf("%s-%s: %w", s.name, sz.suffix, err)
				}
				if s.prep != nil {
					if err := s.prep(); err != nil {
						return fmt.Errorf("%s-%s: %w", s.name, sz.suffix, err)
					}
					time.Sleep(400 * time.Millisecond) // hiệu ứng mở hộp thoại, form
				}
				if err := take(s, s.name+"-"+sz.suffix+".png"); err != nil {
					return err
				}
			}
		}
		return nil
	}

	if err := shootAll(first); err != nil {
		return err
	}

	// Bàn phím và tiếng Anh cho các màn không trạng thái (như bộ m18), trước khi
	// máy demo bị đổi (admin còn chưa bật hai bước).
	if only == "" {
		if err := c.viewport(width, height); err != nil {
			return err
		}
		fmt.Println("\nBàn phím (1440 × 900, tiếng Việt):")
		for _, s := range first {
			if !s.simple {
				continue
			}
			if err := open(s.path, "light", "vi"); err != nil {
				return fmt.Errorf("bàn phím %s: %w", s.name, err)
			}
			if err := tabWalk(c, &fail, s.name); err != nil {
				return fmt.Errorf("bàn phím %s: %w", s.name, err)
			}
		}
		fmt.Println("\nTiếng Anh (không cuộn ngang, không chữ hay nút bị cắt):")
		for _, size := range [][2]int{{width, height}, {390, 844}} {
			if err := c.viewport(size[0], size[1]); err != nil {
				return err
			}
			for _, s := range first {
				if !s.simple {
					continue
				}
				what := fmt.Sprintf("tiếng Anh %s ở %d px", s.name, size[0])
				if err := open(s.path, "light", "en"); err != nil {
					return fmt.Errorf("%s: %w", what, err)
				}
				n := len(fail.list)
				checkPage(c, &fail, what, false)
				checkClipped(c, &fail, what)
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
	}

	if err := shootAll(rest); err != nil {
		return err
	}

	fmt.Printf("\nĐã chụp %d ảnh vào %s. Không đạt: %d\n", count, out, len(fail.list))
	for _, s := range fail.list {
		fmt.Println("  -", s)
	}
	if len(fail.list) > 0 {
		return errors.New("bộ ảnh M20 có điều không đạt")
	}
	return nil
}
