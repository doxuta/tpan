package main

// Phần trình duyệt của e2e giao diện M17 (scripts/e2e-m17-gd.sh): Chrome không
// giao diện mở panel THẬT trên máy ảo test và bấm như người dùng.
//
//	TPAN_PANEL_PASSWORD=… go run ./scripts/anh-readme -bo m17-that -panel https://<máy ảo>:8443 -buoc <bước> [-site <tên miền>] [-goi <gói>]
//
// Các bước (theo thứ tự của e2e):
//
//	bat-long   màn Gói: Sửa gói -goi, chọn "Đưa site của gói vào lồng", gõ
//	           CPU 50, RAM 200, tiến trình 40, worker 3, Lưu (gói chưa có site
//	           trong lồng nên lưu thẳng). Bảng ghi "có" ở cột Lồng.
//	vao-long   trang của -site: thẻ Lồng ghi chờ vào lồng, bấm "Vào lồng", xem
//	           trước, chạy, chờ job cage.enable; thẻ đổi sang nút "Ra khỏi
//	           lồng"; màn Tài nguyên có hàng của site.
//	sua-goi    màn Gói: Sửa gói -goi, RAM 256, nút của hộp là "Xem trước"
//	           (gói có site trong lồng), chạy bản xem trước, chờ job cage.sync.
//	tan-cong   màn Bảo mật: mở dòng "Thử tấn công", bấm "Thử tấn công", xem
//	           trước (hai site thử), chạy, chờ job isolation.probe (tới 20
//	           phút); dòng ghi "không ô nào lộ", không còn lượt chưa dọn.
//	ra-long    trang của -site: bấm "Ra khỏi lồng", xem trước, chạy, chờ job
//	           cage.disable; thẻ đổi lại nút "Vào lồng".
//
// Mỗi khẳng định in "=> ĐẠT"/"=> KHÔNG ĐẠT" (liveCheck). Mật khẩu admin chỉ
// đi qua biến môi trường TPAN_PANEL_PASSWORD. KHÔNG chạy với panel thật.

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// m17ThatNhan là chữ bấm theo, chép từ bản tiếng Việt của web/src/i18n/goi.ts,
// site.ts, bao-mat.ts, tai-nguyen.ts.
var m17ThatNhan = struct {
	sua, luu, xemTruoc, cotCo string // goi.ts
	vaoLong, raLong            string // site.ts (cage.enable, cage.disable)
	thuTanCong, khongLo        string // bao-mat.ts
	tieuDeTaiNguyen            string // tai-nguyen.ts
}{
	sua: "Sửa", luu: "Lưu", xemTruoc: "Xem trước", cotCo: "có",
	vaoLong: m17Nhan.vaoLong, raLong: m17Nhan.raLong,
	thuTanCong: m17Nhan.thuTanCong, khongLo: m17Nhan.khongLo,
	tieuDeTaiNguyen: m17Nhan.tieuDeTaiNguyen,
}

func liveM17(c *cdp, base, adminPW string, o m16Opts) error {
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
	if err := formLogin(c, base, "", adminPW, "/"); err != nil {
		return err
	}
	var l liveCheck
	var err error
	switch o.step {
	case "bat-long":
		err = liveCagePackage(c, &l, base, o.pkg)
	case "vao-long":
		err = liveCageSite(c, &l, base, o.site, true)
	case "sua-goi":
		err = liveEditCagedPackage(c, &l, base, o.pkg)
	case "tan-cong":
		err = liveProbe(c, &l, base)
	case "ra-long":
		err = liveCageSite(c, &l, base, o.site, false)
	default:
		return fmt.Errorf("-buoc của m17-that chỉ nhận bat-long, vao-long, sua-goi, tan-cong, ra-long (đang là %q)", o.step)
	}
	if err != nil {
		return errors.New(strings.ReplaceAll(err.Error(), base, "<panel>"))
	}
	if bad := flowErrors(c, base); len(bad) > 0 {
		l.ok(false, "không lỗi console: %s", strings.Join(bad, " | "))
	} else {
		l.ok(true, "bước %s: không lỗi console", o.step)
	}
	if l.fails > 0 {
		return fmt.Errorf("bước %s: %d khẳng định không đạt", o.step, l.fails)
	}
	return nil
}

func openScreen(c *cdp, base, path string) error {
	if err := c.goTo(base+path, strings.SplitN(path, "?", 2)[0]); err != nil {
		return err
	}
	return settle(c)
}

// openPackageEdit mở hộp "Sửa gói" của gói name trên màn Gói.
func openPackageEdit(c *cdp, base, name string) error {
	if err := openScreen(c, base, "/packages"); err != nil {
		return err
	}
	v := c.str(fmt.Sprintf(`(()=>{const tr=[...document.querySelectorAll('main tbody tr')].find(r=>r.cells[0]&&r.cells[0].textContent.trim().startsWith(%q)); if(!tr) return 'không có hàng của gói'; const b=[...tr.querySelectorAll('button')].find(b=>b.textContent.trim()===%q); if(!b) return 'hàng không có nút Sửa'; b.setAttribute('data-m17-sua','1'); return ''})()`, name, m17ThatNhan.sua))
	if v != "" {
		return fmt.Errorf("gói %s: %s", name, v)
	}
	if err := c.press("[data-m17-sua]"); err != nil {
		return err
	}
	return c.waitTrue(`!!document.querySelector('[role=dialog] #g-ram_mb')`, 10*time.Second)
}

// dialogButton bấm nút có chữ text trong hộp thoại đang mở.
func dialogButton(c *cdp, text string) error {
	return c.pressText(`[role=dialog] button`, text)
}

// packageRow trả gói name theo GET /api/v1/packages dạng "cage ram cpu tasks workers".
func packageRow(c *cdp, name string) string {
	return c.str(fmt.Sprintf(`fetch('/api/v1/packages').then(r=>r.json()).then(l=>{const p=(l.find(x=>x.package.name===%q)||{}).package; return p?[p.cage,p.ram_mb,p.cpu_percent,p.tasks,p.php_workers].join(' '):''})`, name))
}

func liveCagePackage(c *cdp, l *liveCheck, base, name string) error {
	if err := openPackageEdit(c, base, name); err != nil {
		return err
	}
	l.ok(c.str(`document.querySelector('#g-ram_mb')?.disabled?'khoá':''`) == "khoá", "hộp Sửa gói %s: sáu ô mức tối đa khoá khi chưa chọn lồng", name)
	if err := c.press("#g-long"); err != nil {
		return err
	}
	l.ok(c.str(`document.querySelector('#g-ram_mb')?.disabled?'khoá':'mở'`) == "mở", "chọn «Đưa site của gói vào lồng» thì các ô mở")
	for _, f := range []struct{ sel, v string }{{"#g-cpu_percent", "50"}, {"#g-ram_mb", "200"}, {"#g-tasks", "40"}, {"#g-php_workers", "3"}} {
		if err := fillID(c, f.sel, f.v); err != nil {
			return err
		}
	}
	l.ok(c.str(`[...document.querySelectorAll('[role=dialog] button')].some(b=>b.textContent.trim()===`+fmt.Sprintf("%q", m17ThatNhan.luu)+`)?'có':''`) == "có", "gói chưa có site trong lồng: nút của hộp là «%s» (lưu thẳng)", m17ThatNhan.luu)
	if err := dialogButton(c, m17ThatNhan.luu); err != nil {
		return err
	}
	if err := c.waitTrue(`!document.querySelector('[role=dialog]')`, 20*time.Second); err != nil {
		// Chữ của hộp (lời lỗi của form hay của server) để biết vì sao; hộp sửa gói không có bí mật.
		return fmt.Errorf("hộp Sửa gói không đóng sau khi Lưu: %w; hộp đang ghi: %s", err, c.str(`(document.querySelector('[role=dialog]')?.innerText||'').replace(/\s+/g,' ').slice(0,1500)`))
	}
	got := packageRow(c, name)
	l.ok(got == "true 200 50 40 3", "GET /api/v1/packages: gói %s có cage true, RAM 200, CPU 50, 40 tiến trình, 3 worker (đọc được: %s)", name, got)
	if err := openScreen(c, base, "/packages"); err != nil {
		return err
	}
	l.ok(c.str(fmt.Sprintf(`(()=>{const tr=[...document.querySelectorAll('main tbody tr')].find(r=>r.cells[0]&&r.cells[0].textContent.trim().startsWith(%q)); return tr&&[...tr.cells].some(td=>td.textContent.trim()===%q)?'có':''})()`, name, m17ThatNhan.cotCo)) == "có",
		"bảng gói: hàng %s ghi «%s» ở cột Lồng", name, m17ThatNhan.cotCo)
	return nil
}

// cageState trả state của site theo GET /api/v1/cage.
func cageState(c *cdp, domain string) string {
	return c.str(fmt.Sprintf(`fetch('/api/v1/cage').then(r=>r.json()).then(l=>((l||[]).find(r=>r.domain===%q)||{}).state||'')`, domain))
}

func liveCageSite(c *cdp, l *liveCheck, base, domain string, enable bool) error {
	if domain == "" {
		return errors.New("-buoc vao-long, ra-long cần -site")
	}
	from, to, kind, btn, back := "can-vao", "trong", "cage.enable", m17ThatNhan.vaoLong, m17ThatNhan.raLong
	if !enable {
		from, to, kind, btn, back = "trong", "can-vao", "cage.disable", m17ThatNhan.raLong, m17ThatNhan.vaoLong
	}
	l.ok(cageState(c, domain) == from, "trước khi bấm: GET /api/v1/cage ghi %s ở trạng thái %s", domain, from)
	if err := openScreen(c, base, "/sites/"+url.PathEscape(domain)); err != nil {
		return err
	}
	if err := c.waitTrue(fmt.Sprintf(`[...document.querySelectorAll('main button')].some(b=>b.textContent.trim()===%q)`, btn), 15*time.Second); err != nil {
		return fmt.Errorf("trang %s không có nút «%s»: %w", domain, btn, err)
	}
	before := jobIDs(c)
	if err := c.pressText("main button", btn); err != nil {
		return err
	}
	if err := c.waitTrue(`!!document.querySelector('[data-plan-run]')`, 60*time.Second); err != nil {
		return fmt.Errorf("bấm «%s» mà không có bản xem trước: %w", btn, err)
	}
	l.ok(true, "bấm «%s»: có bản xem trước", btn)
	if err := runPlan(c); err != nil {
		return err
	}
	id, st := waitNewJob(c, kind, before, 5*time.Minute)
	l.ok(st == "succeeded", "job %s %s: %s", kind, id, st)
	l.ok(trackerDone(c), "thẻ theo dõi job báo Xong")
	l.ok(cageState(c, domain) == to, "sau job: GET /api/v1/cage ghi %s ở trạng thái %s", domain, to)
	if err := openScreen(c, base, "/sites/"+url.PathEscape(domain)); err != nil {
		return err
	}
	l.ok(c.waitTrue(fmt.Sprintf(`[...document.querySelectorAll('main button')].some(b=>b.textContent.trim()===%q)`, back), 15*time.Second) == nil, "thẻ Lồng đổi sang nút «%s»", back)
	if enable {
		if err := openScreen(c, base, "/usage"); err != nil {
			return err
		}
		l.ok(c.waitTrue(fmt.Sprintf(`(document.querySelector('main')?.innerText||'').includes(%q)`, domain), 15*time.Second) == nil, "màn %s có hàng của %s", m17ThatNhan.tieuDeTaiNguyen, domain)
	}
	return nil
}

func liveEditCagedPackage(c *cdp, l *liveCheck, base, name string) error {
	if err := openPackageEdit(c, base, name); err != nil {
		return err
	}
	if err := fillID(c, "#g-ram_mb", "256"); err != nil {
		return err
	}
	l.ok(c.str(`[...document.querySelectorAll('[role=dialog] button')].some(b=>b.textContent.trim()===`+fmt.Sprintf("%q", m17ThatNhan.xemTruoc)+`)?'có':''`) == "có", "gói có site trong lồng, RAM đổi: nút của hộp là «%s»", m17ThatNhan.xemTruoc)
	before := jobIDs(c)
	if err := dialogButton(c, m17ThatNhan.xemTruoc); err != nil {
		return err
	}
	if err := c.waitTrue(`!!document.querySelector('[data-plan-run]')`, 60*time.Second); err != nil {
		return fmt.Errorf("bấm «%s» mà không có bản xem trước: %w", m17ThatNhan.xemTruoc, err)
	}
	l.ok(true, "sửa gói có site trong lồng: có bản xem trước")
	if err := runPlan(c); err != nil {
		return err
	}
	id, st := waitNewJob(c, "cage.sync", before, 5*time.Minute)
	l.ok(st == "succeeded", "job cage.sync %s: %s", id, st)
	l.ok(trackerDone(c), "thẻ theo dõi job báo Xong")
	got := packageRow(c, name)
	l.ok(strings.HasPrefix(got, "true 256 "), "GET /api/v1/packages: gói %s có RAM 256 (đọc được: %s)", name, got)
	return nil
}

func liveProbe(c *cdp, l *liveCheck, base string) error {
	if err := openScreen(c, base, "/security"); err != nil {
		return err
	}
	if err := c.waitTrue(`!!document.getElementById('`+probeRowID+`')`, 20*time.Second); err != nil {
		return fmt.Errorf("màn Bảo mật không có dòng Thử tấn công: %w", err)
	}
	l.ok(c.str(`document.getElementById('`+probeRowID+`').getAttribute('aria-expanded')`) == "false", "dòng Thử tấn công đóng sẵn lúc mới mở")
	if err := openProbeRow(c); err != nil {
		return err
	}
	before := jobIDs(c)
	if err := c.pressText("main button", m17ThatNhan.thuTanCong); err != nil {
		return err
	}
	if err := c.waitTrue(`!!document.querySelector('[data-plan-run]')`, 120*time.Second); err != nil {
		return fmt.Errorf("bấm «%s» mà không có bản xem trước: %w", m17ThatNhan.thuTanCong, err)
	}
	l.ok(c.str(`(document.querySelector('main')?.innerText||'').match(/tpan-probe-a-[0-9a-f]+\.test/)?'có':''`) == "có", "bản xem trước nói tên site thử (tpan-probe-a-….test)")
	if err := runPlan(c); err != nil {
		return err
	}
	id, st := waitNewJob(c, "isolation.probe", before, 20*time.Minute)
	l.ok(st == "succeeded", "job isolation.probe %s: %s", id, st)
	got := c.str(`fetch('/api/v1/security/probe').then(r=>r.json()).then(j=>[j.last?j.last.id:'',j.last?j.last.exposed:'',j.last?j.last.cleaned:'',j.last?j.last.results.length:'',(j.pending||[]).length].join(' '))`)
	f := strings.Fields(got)
	l.ok(len(f) == 5 && f[1] == "0" && f[2] == "true" && f[4] == "0", "GET /api/v1/security/probe: lượt gần nhất 0 ô lộ, đã dọn, không còn lượt chưa dọn (đọc được: %s)", got)
	if err := openScreen(c, base, "/security"); err != nil {
		return err
	}
	l.ok(c.waitTrue(fmt.Sprintf(`(document.getElementById('%s')?.innerText||'').includes(%q)`, probeRowID, m17ThatNhan.khongLo), 15*time.Second) == nil, "dòng Thử tấn công ghi «%s»", m17ThatNhan.khongLo)
	return nil
}
