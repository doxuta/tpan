package main

// Bấm thật nút cập nhật TPAN trên panel của máy ảo test (M15, mục 6.14 của
// docs/KE-HOACH.md), bằng Chrome không giao diện, và chụp từng chặng:
//
//	TPAN_PANEL_PASSWORD=... go run ./scripts/anh-readme -bo m15-that \
//	    -panel https://<IP máy ảo>:8443 -out docs/bang-chung
//
// Vì sao cần: e2e của M15 (scripts/e2e-m15.sh) gọi API bằng curl, nên nó không
// chạy dòng mã nào của giao diện. Phần khó của giao diện là sống qua lần panel
// khởi động lại: hỏi lại máy chủ trong lúc mất kết nối, không văng về trang
// đăng nhập, rồi báo "Đã cập nhật lên <bản>". Chỉ trình duyệt thật mới kiểm được.
//
// Máy ảo phải được chuẩn bị trước: panel đang chạy bản A, setting
// update.self_url trỏ tới một nguồn tải có bản B khác A (máy chủ tải trên
// loopback của máy ảo, như e2e). Chương trình in từng điều nó thấy; điều nào
// không đúng thì dừng với lỗi. KHÔNG chạy với panel của máy chủ thật.

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// liveM15 đi hết một vòng: kiểm bản mới, cập nhật, chờ panel lên lại, tải lại
// trang, quay về bản trước.
func liveM15(c *cdp, base, out string) error {
	const (
		block  = `document.getElementById('phien-ban')`
		dialog = `document.querySelector('[role=dialog],[role=alertdialog]')`
	)
	text := func(js string) string {
		v, _ := c.eval(js)
		s, _ := v.(string)
		return s
	}
	version := func() string {
		return text(`fetch('/api/v1/session').then(r=>r.json()).then(j=>j.version||'').catch(()=>'')`)
	}
	// clickIn bấm nút đầu tiên trong scope có chữ bắt đầu bằng label (chờ tới khi nút có và bấm được).
	clickIn := func(scope, label string) error {
		find := fmt.Sprintf(`[...(%s?.querySelectorAll('button')||[])].find(b=>b.innerText.trim().startsWith(%q) && !b.disabled)`, scope, label)
		if err := c.waitTrue("!!"+find, 20*time.Second); err != nil {
			return fmt.Errorf("chờ nút %q: %w", label, err)
		}
		_, err := c.eval(find + ".click()")
		return err
	}
	blockHas := func(s string) string {
		return fmt.Sprintf(`!!%s && %s.innerText.includes(%q)`, block, block, s)
	}
	shot := func(file string) error {
		if _, err := c.eval(block + `?.scrollIntoView({block:'start'})`); err != nil {
			return err
		}
		time.Sleep(400 * time.Millisecond)
		return snap(c, out, file)
	}
	// watch ghi lại, mỗi 100ms, trang đang ở đường dẫn nào và khối đang nói gì:
	// bằng chứng là trong lúc panel restart trang không văng về đăng nhập.
	const watch = `(()=>{ if (window.__m15) clearInterval(window.__m15.t);
		const w = window.__m15 = {paths:{}, restarting:false, updating:false, loginSeen:false};
		w.t = setInterval(()=>{ w.paths[location.pathname]=1;
			const b=document.getElementById('phien-ban'); const s=b?b.innerText:'';
			if (s.includes('Panel đang khởi động lại')) w.restarting=true;
			if (s.includes('Đang cập nhật TPAN') || s.includes('Đang quay về bản trước')) w.updating=true;
			if (location.pathname.includes('dang-nhap')) w.loginSeen=true; }, 100); return true })()`
	report := func(what string) error {
		v, err := c.eval(`JSON.stringify({paths:Object.keys(window.__m15.paths), restarting:window.__m15.restarting, updating:window.__m15.updating, loginSeen:window.__m15.loginSeen})`)
		if err != nil {
			return err
		}
		fmt.Printf("%s: trong lúc chờ, trang đã thấy %v\n", what, v)
		if s, _ := v.(string); strings.Contains(s, `"loginSeen":true`) || !strings.Contains(s, `"paths":["/settings"]`) {
			return fmt.Errorf("%s: trang đã rời /settings trong lúc panel khởi động lại", what)
		}
		if s, _ := v.(string); !strings.Contains(s, `"updating":true`) {
			return fmt.Errorf("%s: không lần nào thấy dòng \"đang chạy\" trên khối", what)
		}
		return nil
	}

	if _, err := c.eval(`localStorage.setItem('tpan-theme','light');localStorage.setItem('tpan-lang','vi')`); err != nil {
		return err
	}
	if err := c.goTo(base+"/settings", "/settings"); err != nil {
		return err
	}
	from := version()
	if from == "" {
		return errors.New("không đọc được phiên bản từ /api/v1/session (chưa đăng nhập?)")
	}
	fmt.Println("bản đang chạy lúc đầu:", from)

	// 1. Kiểm bản mới: bản xem trước.
	if err := clickIn(block, "Kiểm bản mới"); err != nil {
		return err
	}
	// Bản 2 (M16) viết thường nhãn này; bản 1 ghi "SHA256 SẼ KIỂM".
	if err := c.waitTrue(blockHas("SHA256 sẽ kiểm"), 30*time.Second); err != nil {
		return fmt.Errorf("chờ bản xem trước sau khi bấm Kiểm bản mới: %w (khối đang ghi: %s)", err, text(block+`.innerText`))
	}
	if err := shot("M15-that-1-xem-truoc.png"); err != nil {
		return err
	}

	// 2. Cập nhật: hộp hỏi lại, rồi trạng thái đang chạy.
	if err := clickIn(block, "Cập nhật lên"); err != nil {
		return err
	}
	if _, err := c.eval(watch); err != nil {
		return err
	}
	start := time.Now()
	if err := clickIn(dialog, "Cập nhật lên"); err != nil {
		return err
	}
	if err := c.waitTrue(blockHas("Đang cập nhật TPAN"), 10*time.Second); err == nil {
		if err := shot("M15-that-2-dang-cap-nhat.png"); err != nil {
			return err
		}
	} else {
		fmt.Println("(lượt chạy xong trước khi kịp chụp dòng \"Đang cập nhật TPAN…\")")
	}

	// 3. Panel lên lại: "Đã cập nhật lên <bản>", trang vẫn ở /settings.
	if err := c.waitTrue(blockHas("Đã cập nhật lên "), 120*time.Second); err != nil {
		return fmt.Errorf("chờ dòng \"Đã cập nhật lên\": %w (khối đang ghi: %s)", err, text(block+`.innerText`))
	}
	fmt.Printf("từ lúc bấm tới lúc khối báo \"Đã cập nhật lên\": %.1f giây\n", time.Since(start).Seconds())
	if err := report("cập nhật"); err != nil {
		return err
	}
	to := version()
	if to == "" || to == from {
		return fmt.Errorf("sau khi khối báo đã lên, /api/v1/session vẫn trả bản %q (lúc đầu %q)", to, from)
	}
	fmt.Println("bản đang chạy sau khi cập nhật:", to, "(phiên đăng nhập cũ còn dùng được: /api/v1/session trả lời có version)")
	if err := c.waitTrue(blockHas("Đã cập nhật lên "+to), 5*time.Second); err != nil {
		return fmt.Errorf("khối không ghi đúng bản mới %s: %s", to, text(block+`.innerText`))
	}
	if err := shot("M15-that-3-da-len.png"); err != nil {
		return err
	}
	// Dòng phiên bản: bản 2 (M16) đặt ở cuối menu (#dong-phien-ban); bản 1 ở chân trang.
	footer := text(`(document.getElementById('dong-phien-ban')||document.querySelector('footer'))?.innerText||''`)
	fmt.Printf("dòng phiên bản sau khi lên: %q\n", strings.TrimSpace(footer))
	if !strings.Contains(footer, to) {
		return fmt.Errorf("dòng phiên bản chưa đổi sang %s: %q", to, footer)
	}

	// 4. Tải lại trang (nút của khối), rồi quay về bản trước.
	if err := clickIn(block, "Tải lại trang"); err != nil {
		return err
	}
	time.Sleep(time.Second)
	if err := c.waitTrue(`document.readyState==='complete' && location.pathname==='/settings' && `+blockHas("Lượt gần nhất"), 30*time.Second); err != nil {
		return fmt.Errorf("sau khi tải lại trang: %w", err)
	}
	if err := clickIn(block, "Quay về bản trước"); err != nil {
		return err
	}
	if err := c.waitTrue("!!"+dialog, 20*time.Second); err != nil {
		return fmt.Errorf("chờ hộp hỏi lại của Quay về: %w (khối đang ghi: %s)", err, text(block+`.innerText`))
	}
	if err := snap(c, out, "M15-that-4-hoi-quay-ve.png"); err != nil {
		return err
	}
	if _, err := c.eval(watch); err != nil {
		return err
	}
	start = time.Now()
	if err := clickIn(dialog, "Quay về "); err != nil {
		return err
	}
	if err := c.waitTrue(blockHas("Đã quay về "), 120*time.Second); err != nil {
		return fmt.Errorf("chờ dòng \"Đã quay về\": %w (khối đang ghi: %s)", err, text(block+`.innerText`))
	}
	fmt.Printf("từ lúc bấm tới lúc khối báo \"Đã quay về\": %.1f giây\n", time.Since(start).Seconds())
	if err := report("quay về"); err != nil {
		return err
	}
	back := version()
	fmt.Println("bản đang chạy sau khi quay về:", back)
	if back != from {
		return fmt.Errorf("quay về xong mà bản đang chạy là %q, lúc đầu là %q", back, from)
	}
	return shot("M15-that-5-da-quay-ve.png")
}
