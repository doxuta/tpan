package main

// Phần trình duyệt của e2e giao diện M18 (scripts/e2e-m18-gd.sh): Chrome không
// giao diện mở panel THẬT trên máy ảo test và làm như người dùng.
//
//	TPAN_PANEL_PASSWORD=… TPAN_S3_ACCESS=… TPAN_S3_SECRET=… go run ./scripts/anh-readme -bo m18-that \
//	    -panel https://<máy ảo>:8443 -buoc dich -s3-endpoint http://127.0.0.1:7070 -s3-region <vùng> -s3-bucket <bucket>
//	TPAN_PANEL_PASSWORD=… … -bo m18-that -panel … -buoc khoi-phuc -site <tên miền>
//
// Bước dich: admin đăng nhập qua form, mở /backups, gõ vào form đích, bấm xem
// trước rồi chạy, chờ job backup.target.set xong; thẻ đích hiện "…" và bốn ký
// tự cuối của access key; bấm "Thử kết nối", chờ job backup.target.test đạt.
// Bước khoi-phuc: trên hàng của bản chỉ còn trên kho bấm Khôi phục, "Tải về
// máy" trong hộp thoại, chờ job backup.pull; bấm Khôi phục lần nữa, xem
// trước, chạy, chờ job backup.restore.
//
// BÍ MẬT: hai khoá S3 chỉ đi từ biến môi trường vào ô nhập bằng
// Input.insertText (như người gõ), không bao giờ nằm trong một biểu thức JS,
// không bao giờ được in, kể cả bốn ký tự cuối: mọi phép so với khoá làm trong
// Go trên chữ đọc về từ trang. Chương trình không in chữ của trang (trang vừa
// đặt đích lần đầu có thể đang hiện khoá sao lưu). KHÔNG chạy với panel thật.

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

func liveM18(c *cdp, base, adminPW string, o m16Opts) error {
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
	case "dich":
		err = liveTarget(c, &l, base, adminPW, o)
	case "khoi-phuc":
		err = liveRestoreRemote(c, &l, base, adminPW, o.site)
	default:
		return fmt.Errorf("-buoc của m18-that chỉ nhận dich, khoi-phuc (đang là %q)", o.step)
	}
	if err != nil {
		return errors.New(strings.ReplaceAll(err.Error(), base, "<panel>"))
	}
	if l.fails > 0 {
		return fmt.Errorf("bước %s: %d khẳng định không đạt", o.step, l.fails)
	}
	return nil
}

// fillID gõ text vào ô sel (thay hết chữ đang có). text đi bằng
// Input.insertText, không nằm trong biểu thức JS.
func fillID(c *cdp, sel, text string) error {
	if err := c.waitTrue(fmt.Sprintf(`!!document.querySelector(%q)`, sel), 10*time.Second); err != nil {
		return fmt.Errorf("không thấy ô %s: %w", sel, err)
	}
	if _, err := c.eval(fmt.Sprintf(`(()=>{const e=document.querySelector(%q); e.scrollIntoView({block:'center'}); e.focus(); e.select(); return true})()`, sel)); err != nil {
		return err
	}
	if _, err := c.call("Input.insertText", map[string]any{"text": text}); err != nil {
		return err
	}
	// Chỉ đọc về độ dài để biết React đã nhận đủ chữ, không đọc giá trị.
	n, err := c.eval(fmt.Sprintf(`document.querySelector(%q).value.length`, sel))
	if err != nil {
		return err
	}
	if f, _ := n.(float64); int(f) != len([]rune(text)) {
		return fmt.Errorf("ô %s: gõ %d ký tự, ô giữ %v", sel, len([]rune(text)), n)
	}
	return nil
}

// jobIDs là id của các job gần nhất, để nhận ra job mới sinh sau một cú bấm.
func jobIDs(c *cdp) map[string]bool {
	s := c.str(`fetch('/api/v1/jobs?limit=100').then(r=>r.json()).then(l=>l.map(j=>j.id).join(','))`)
	m := map[string]bool{}
	for _, id := range strings.Split(s, ",") {
		if id != "" {
			m[id] = true
		}
	}
	return m
}

// waitNewJob chờ một job loại kind không có trong before xuất hiện rồi kết thúc.
// Trả id và trạng thái cuối ("" khi hết giờ mà chưa thấy job).
func waitNewJob(c *cdp, kind string, before map[string]bool, timeout time.Duration) (id, status string) {
	end := time.Now().Add(timeout)
	for time.Now().Before(end) {
		s := c.str(fmt.Sprintf(`fetch('/api/v1/jobs?limit=50').then(r=>r.json()).then(l=>l.filter(j=>j.kind===%q).map(j=>j.id+'|'+j.status).join(','))`, kind))
		for _, p := range strings.Split(s, ",") {
			i, st, ok := strings.Cut(p, "|")
			if ok && !before[i] {
				id, status = i, st
				break
			}
		}
		if status != "" && status != "running" {
			return id, status
		}
		time.Sleep(time.Second)
	}
	return id, status
}

// runPlan bấm nút chạy của bản xem trước; Plan có bước nguy hiểm thì hộp hỏi
// lại hiện ra và nút "Chạy N bước" của hộp được bấm (web/src/i18n/duyet-plan.ts).
func runPlan(c *cdp) error {
	if err := c.press("[data-plan-run]"); err != nil {
		return err
	}
	time.Sleep(600 * time.Millisecond)
	v, err := c.eval(`(()=>{const b=[...document.querySelectorAll('[role=dialog] button,[role=alertdialog] button')].find(b=>/^Chạy \d+ bước$/.test(b.textContent.trim()));
		if(!b) return false; b.setAttribute('data-m18-chay','1'); return true})()`)
	if err != nil || v != true {
		return err // không có hộp hỏi lại
	}
	defer func() { _, _ = c.eval(`document.querySelector('[data-m18-chay]')?.removeAttribute('data-m18-chay')`) }()
	return c.press("[data-m18-chay]")
}

// trackerDone chờ thẻ theo dõi job ở góc trang báo "Xong" (khung.ts, jobDone).
// Thẻ job xong tự ẩn sau 8 giây, nên gọi ngay sau khi API báo job xong.
func trackerDone(c *cdp) bool {
	return c.waitTrue(`[...document.querySelectorAll('[data-job-tracker] p')].some(p=>p.textContent.trim()==='Xong')`, 10*time.Second) == nil
}

// flowErrors trả lỗi console từ lần gọi trước, bỏ dòng CSP đã biết của hộp thoại Radix.
func flowErrors(c *cdp, base string) []string {
	var bad []string
	for _, e := range c.consoleErrors() {
		if !knownDialogCSP(e) {
			bad = append(bad, strings.ReplaceAll(e, base, "<panel>"))
		}
	}
	return bad
}

func liveTarget(c *cdp, l *liveCheck, base, pw string, o m16Opts) error {
	access, secret := os.Getenv("TPAN_S3_ACCESS"), os.Getenv("TPAN_S3_SECRET")
	if len(access) < 12 || secret == "" {
		return errors.New("-buoc dich cần TPAN_S3_ACCESS (từ 12 ký tự, để thẻ hiện bốn ký tự cuối) và TPAN_S3_SECRET")
	}
	if o.s3Endpoint == "" || o.s3Region == "" || o.s3Bucket == "" {
		return errors.New("-buoc dich cần -s3-endpoint, -s3-region, -s3-bucket")
	}
	// noSecret: chữ đọc về từ trang không chứa khoá nào (so trong Go, không in).
	noSecret := func(s string) bool { return !strings.Contains(s, access) && !strings.Contains(s, secret) }

	if err := formLogin(c, base, "", pw, "/"); err != nil {
		return err
	}
	if err := c.goTo(base+"/backups", "/backups"); err != nil {
		return err
	}
	if err := settle(c); err != nil {
		return err
	}
	_ = c.consoleErrors()
	l.ok(c.str(`document.querySelector('`+theDichSel+` h2')?.innerText.trim()||''`) == m18Nhan.theDich, "màn Backup có thẻ «%s»", m18Nhan.theDich)
	l.ok(c.str(`fetch('/api/v1/backups/target').then(r=>r.json()).then(j=>String(j.configured))`) == "false", "trước khi đặt: GET /api/v1/backups/target báo configured=false")

	// Máy chưa có đích: form hiện sẵn trong thẻ (không cần bấm "Đổi đích").
	fields := []struct{ sel, text string }{
		{"#o-dich-endpoint", o.s3Endpoint}, {"#o-dich-region", o.s3Region}, {"#o-dich-bucket", o.s3Bucket},
	}
	if o.s3Prefix != "" {
		fields = append(fields, struct{ sel, text string }{"#o-dich-prefix", o.s3Prefix})
	}
	fields = append(fields, struct{ sel, text string }{"#o-dich-access", access}, struct{ sel, text string }{"#o-dich-secret", secret})
	for _, f := range fields {
		if err := fillID(c, f.sel, f.text); err != nil {
			return err
		}
	}
	// versitygw ở http loopback bắt buộc kiểu đường dẫn: ô "Dùng kiểu bucket.host" phải tắt.
	if c.str(`document.querySelector('#o-dich-vhost')?.checked?'bật':''`) != "" {
		if err := c.press("#o-dich-vhost"); err != nil {
			return err
		}
	}
	l.ok(c.str(`document.querySelector('#o-dich-secret')?.type||''`) == "password", "ô khoá bí mật là ô mật khẩu")
	l.ok(c.str(`document.querySelector('#o-dich-vhost')?.checked?'bật':'tắt'`) == "tắt", "ô «Dùng kiểu bucket.host» tắt (kiểu đường dẫn)")
	l.ok(true, "gõ form đích: endpoint %s, vùng %s, bucket %s, hai khoá (không in)", o.s3Endpoint, o.s3Region, o.s3Bucket)

	before := jobIDs(c)
	if err := c.pressText(theDichSel+" button", m18Nhan.xemTruoc); err != nil {
		return err
	}
	if err := c.waitTrue(`!!document.querySelector('[data-plan-run]:not([disabled])')`, 60*time.Second); err != nil {
		return fmt.Errorf("bấm «%s» không ra bản xem trước: %w", m18Nhan.xemTruoc, err)
	}
	l.ok(true, "bấm «%s»: bản xem trước hiện ra, nút chạy ghi «%s»", m18Nhan.xemTruoc, c.str(`document.querySelector('[data-plan-run]').innerText.trim()`))
	l.ok(noSecret(c.str(`document.body.innerText`)), "bản xem trước không hiện access key hay secret key")
	if err := runPlan(c); err != nil {
		return err
	}
	id, st := waitNewJob(c, "backup.target.set", before, 3*time.Minute)
	l.ok(st == "succeeded", "job backup.target.set %s kết thúc: %s", id, st)
	l.ok(trackerDone(c), "thẻ theo dõi job báo «Xong»")

	if err := settle(c); err != nil {
		return err
	}
	tail := access[len(access)-4:]
	view := c.str(`fetch('/api/v1/backups/target').then(r=>r.json()).then(j=>String(j.configured)+'|'+(j.access_key_tail||''))`)
	l.ok(view == "true|"+tail, "sau khi đặt: API báo configured=true, access_key_tail đúng bốn ký tự cuối của khoá đã gõ")
	text := c.str(`document.querySelector('main').innerText`)
	l.ok(strings.Contains(text, "…"+tail), "thẻ đích hiện «…» cộng bốn ký tự cuối của access key")
	l.ok(noSecret(text), "màn Backup không hiện access key hay secret key đầy đủ")
	vals := c.str(`[...document.querySelectorAll('input,textarea')].map(e=>e.value).join('\n')`)
	l.ok(noSecret(vals), "không ô nhập nào còn giữ khoá sau khi gửi")
	errs := flowErrors(c, base)
	l.ok(len(errs) == 0, "lượt đặt đích: %d lỗi console", len(errs))
	for _, e := range errs {
		fmt.Println("       ", e)
	}

	before = jobIDs(c)
	if err := c.pressText(theDichSel+" button", m18Nhan.thuKetNoi); err != nil {
		return err
	}
	// Thử kết nối có thể chạy ngay hoặc qua bản xem trước.
	if c.waitTrue(`!!document.querySelector('[data-plan-run]:not([disabled])')`, 3*time.Second) == nil {
		if err := runPlan(c); err != nil {
			return err
		}
	}
	id, st = waitNewJob(c, "backup.target.test", before, 2*time.Minute)
	l.ok(st == "succeeded", "bấm «%s»: job backup.target.test %s kết thúc: %s", m18Nhan.thuKetNoi, id, st)
	l.ok(trackerDone(c), "thẻ theo dõi job của lần thử báo «Xong»")
	errs = flowErrors(c, base)
	l.ok(len(errs) == 0, "lượt thử kết nối: %d lỗi console", len(errs))
	for _, e := range errs {
		fmt.Println("       ", e)
	}
	return nil
}

func liveRestoreRemote(c *cdp, l *liveCheck, base, pw, site string) error {
	if site == "" {
		return errors.New("-buoc khoi-phuc cần -site <tên miền>")
	}
	if err := formLogin(c, base, "", pw, "/"); err != nil {
		return err
	}
	if err := c.goTo(base+"/backups", "/backups"); err != nil {
		return err
	}
	if err := settle(c); err != nil {
		return err
	}
	_ = c.consoleErrors()
	l.ok(c.str(fmt.Sprintf(`[...document.querySelectorAll('main th')].some(th=>th.textContent.trim()===%q)?'có':''`, m18Nhan.cotODau)) != "", "bảng backup có cột «%s»", m18Nhan.cotODau)
	bid := remoteOnlyID(c, site)
	if bid == "" {
		return fmt.Errorf("API không có bản nào của %s chỉ còn trên kho", site)
	}
	localOf := fmt.Sprintf(`fetch('/api/v1/backups').then(r=>r.json()).then(j=>String(((j.items||[]).find(b=>b.id===%q)||{}).local))`, bid)

	if err := openRemoteDialog(c, site); err != nil {
		return err
	}
	l.ok(true, "bấm «%s» trên hàng bản %s (chỉ còn trên kho): hộp thoại hiện ra", m18Nhan.khoiPhuc, bid)
	before := jobIDs(c)
	if err := c.pressText(`[role=dialog] button,[role=alertdialog] button`, m18Nhan.taiVeMay); err != nil {
		return err
	}
	// Sau hộp thoại là bản xem trước của việc tải về.
	if err := c.waitTrue(`!document.querySelector('[role=dialog],[role=alertdialog]') && !!document.querySelector('[data-plan-run]:not([disabled])')`, 60*time.Second); err != nil {
		return fmt.Errorf("bấm «%s» trong hộp thoại không ra bản xem trước tải về: %w", m18Nhan.taiVeMay, err)
	}
	l.ok(true, "bấm «%s» trong hộp thoại: bản xem trước tải về hiện ra, nút chạy ghi «%s»", m18Nhan.taiVeMay, c.str(`document.querySelector('[data-plan-run]').innerText.trim()`))
	if err := runPlan(c); err != nil {
		return err
	}
	id, st := waitNewJob(c, "backup.pull", before, 10*time.Minute)
	l.ok(st == "succeeded", "job backup.pull %s kết thúc: %s", id, st)
	l.ok(trackerDone(c), "thẻ theo dõi job của lần tải về báo «Xong»")
	l.ok(c.waitTrue(localOf+`.then(s=>s==='true')`, 20*time.Second) == nil, "sau khi tải về: bản %s có local=true", bid)
	l.ok(c.waitTrue(fmt.Sprintf(`document.querySelector('main').innerText.includes(%q)`, m18Nhan.daTaiVe), 20*time.Second) == nil, "màn báo «%s»", m18Nhan.daTaiVe)

	// Bản xem trước tải về phải đã đóng, kẻo nút chạy cũ bị bấm nhầm lần nữa.
	if err := c.waitTrue(`!document.querySelector('[data-plan-run]')`, 20*time.Second); err != nil {
		return fmt.Errorf("bản xem trước tải về không đóng sau khi chạy: %w", err)
	}
	if err := settle(c); err != nil {
		return err
	}
	if err := markRow(c, bid); err != nil {
		return err
	}
	before = jobIDs(c)
	if err := c.pressText(`[data-m18-hang] button`, m18Nhan.khoiPhuc); err != nil {
		return err
	}
	if err := c.waitTrue(`!!document.querySelector('[data-plan-run]:not([disabled])')`, 60*time.Second); err != nil {
		return fmt.Errorf("bấm «%s» lần hai không ra bản xem trước khôi phục: %w", m18Nhan.khoiPhuc, err)
	}
	l.ok(true, "bấm «%s» lần hai: bản xem trước khôi phục hiện ra, nút chạy ghi «%s»", m18Nhan.khoiPhuc, c.str(`document.querySelector('[data-plan-run]').innerText.trim()`))
	if err := runPlan(c); err != nil {
		return err
	}
	id, st = waitNewJob(c, "backup.restore", before, 10*time.Minute)
	l.ok(st == "succeeded", "job backup.restore %s kết thúc: %s", id, st)
	l.ok(trackerDone(c), "thẻ theo dõi job báo «Xong»")
	errs := flowErrors(c, base)
	l.ok(len(errs) == 0, "cả lượt tải về rồi khôi phục: %d lỗi console (không tính dòng CSP đã biết của hộp thoại)", len(errs))
	for _, e := range errs {
		fmt.Println("       ", e)
	}
	return nil
}
