package main

// Phần kiểm "dùng được bằng bàn phím" và "tiếng Anh không chuỗi nào tràn" của
// bộ M16 (mục 7 của docs/thay-doi/M16-thiet-ke.md). Phím và chuột đi qua trình
// duyệt như người gõ thật (Input.dispatchKeyEvent), không gọi hàm JS của trang.

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// keyCodes là mã phím ảo của các phím bộ M16 dùng (Input.dispatchKeyEvent cần cả tên lẫn mã).
var keyCodes = map[string]int{"Enter": 13, "Escape": 27, "Tab": 9, " ": 32, "ArrowLeft": 37, "ArrowUp": 38, "ArrowRight": 39, "ArrowDown": 40, "Home": 36, "End": 35}

// keyRaw gõ một phím vào phần tử đang giữ focus, không chờ.
func (c *cdp) keyRaw(k string) error {
	code, ok := keyCodes[k]
	if !ok {
		return fmt.Errorf("phím %q chưa có trong keyCodes", k)
	}
	name := k
	if k == " " {
		name = "Space"
	}
	down := map[string]any{"type": "keyDown", "key": k, "code": name, "windowsVirtualKeyCode": code}
	// Enter và phím cách phải mang ký tự thì nút đang focus mới nhận "bấm".
	switch k {
	case "Enter":
		down["text"] = "\r"
	case " ":
		down["text"] = " "
	}
	if _, err := c.call("Input.dispatchKeyEvent", down); err != nil {
		return err
	}
	_, err := c.call("Input.dispatchKeyEvent", map[string]any{"type": "keyUp", "key": k, "code": name, "windowsVirtualKeyCode": code})
	return err
}

// key gõ một phím rồi chờ một nhịp: Radix dời focus bằng setTimeout sau phím
// mũi tên, đọc trạng thái ngay thì thấy trạng thái cũ (bản thử T2 của M16).
func (c *cdp) key(k string) error {
	if err := c.keyRaw(k); err != nil {
		return err
	}
	time.Sleep(150 * time.Millisecond)
	return nil
}

// press bấm chuột trái vào giữa phần tử (sự kiện con trỏ thật: Radix mở menu ở
// pointerdown, .click() của JS không mở được).
func (c *cdp) press(selector string) error {
	v, err := c.eval(fmt.Sprintf(`(()=>{const e=document.querySelector(%q); if(!e) return null; e.scrollIntoView({block:'center'}); const r=e.getBoundingClientRect(); return [r.left+r.width/2, r.top+r.height/2]})()`, selector))
	if err != nil {
		return err
	}
	xy, ok := v.([]any)
	if !ok || len(xy) != 2 {
		return fmt.Errorf("không thấy phần tử %s để bấm", selector)
	}
	for _, typ := range []string{"mousePressed", "mouseReleased"} {
		if _, err := c.call("Input.dispatchMouseEvent", map[string]any{"type": typ, "x": xy[0], "y": xy[1], "button": "left", "clickCount": 1}); err != nil {
			return err
		}
	}
	return nil
}

// tabWalk gõ Tab từ đầu trang cho tới khi đã đi hết, và khẳng định:
//  1. mọi thứ bấm được trong <main> (liên kết, nút, ô nhập, ô chọn chưa bị khoá)
//     đều được Tab tới;
//  2. phần tử đang giữ focus lúc nào cũng nhìn thấy: nằm trong cửa sổ và không
//     bị thứ khác (thanh đầu trang dính) che ở điểm giữa.
//
// Tab của Radix (role=tab) chỉ có một cái nhận Tab, các cái còn lại đi bằng mũi
// tên: đó là đúng mẫu ARIA, nên tab có tabindex=-1 không tính vào danh sách.
func tabWalk(c *cdp, f *m16Fail, what string) error {
	const mark = `(()=>{
		const vis = e => { const r=e.getBoundingClientRect(), s=getComputedStyle(e); return r.width>1 && r.height>1 && s.visibility!=='hidden' && s.display!=='none'; };
		const all = [...document.querySelectorAll('main a[href], main button, main input, main select, main textarea, main [tabindex="0"]')]
			.filter(e => vis(e) && !e.disabled && e.getAttribute('tabindex')!=='-1' && e.type!=='hidden');
		all.forEach((e,i)=>e.setAttribute('data-tw', String(i)));
		window.scrollTo(0,0);
		if (document.activeElement && document.activeElement !== document.body) document.activeElement.blur();
		return all.length;
	})()`
	v, err := c.eval(mark)
	if err != nil {
		return err
	}
	total := int(v.(float64))
	const look = `(()=>{
		const e = document.activeElement;
		if (!e || e === document.body) return JSON.stringify({body:true});
		const r = e.getBoundingClientRect();
		const cx = r.left + r.width/2, cy = r.top + r.height/2;
		const top = document.elementFromPoint(cx, cy);
		const inView = r.bottom > 0 && r.top < innerHeight && r.right > 0 && r.left < innerWidth;
		const covered = !top || !(top === e || e.contains(top) || top.contains(e));
		const cls = (e.getAttribute('class')||'').split(/\s+/).slice(0,3).join('.');
		return JSON.stringify({tw: e.getAttribute('data-tw'), inView, covered, what: e.tagName.toLowerCase()+(e.id?'#'+e.id:'')+(cls?'.'+cls:'')+' «'+(e.getAttribute('aria-label')||e.textContent||'').trim().slice(0,30)+'»'});
	})()`
	seen := map[string]bool{}
	bodyHits := 0
	// Số lần Tab tối đa: đủ cho cả thanh đầu trang và menu, cộng dư.
	for i := 0; i < total+120; i++ {
		if err := c.keyRaw("Tab"); err != nil {
			return err
		}
		v, err := c.eval(look)
		if err != nil {
			return err
		}
		var st struct {
			Body    bool    `json:"body"`
			TW      *string `json:"tw"`
			InView  bool    `json:"inView"`
			Covered bool    `json:"covered"`
			What    string  `json:"what"`
		}
		if err := json.Unmarshal([]byte(fmt.Sprint(v)), &st); err != nil {
			return err
		}
		if st.Body {
			// Focus ra khỏi trang (thanh địa chỉ) rồi quay lại: đã đi hết một vòng.
			bodyHits++
			if bodyHits >= 1 && len(seen) > 0 {
				break
			}
			continue
		}
		if !st.InView {
			f.add("%s: phần tử đang focus nằm ngoài cửa sổ: %s", what, st.What)
		} else if st.Covered {
			f.add("%s: phần tử đang focus bị che: %s", what, st.What)
		}
		if st.TW != nil {
			if seen[*st.TW] && len(seen) == total {
				break
			}
			seen[*st.TW] = true
		}
	}
	if len(seen) < total {
		v, _ := c.eval(`[...document.querySelectorAll('[data-tw]')].map(e=>e.getAttribute('data-tw')+'|'+e.tagName.toLowerCase()+' «'+(e.getAttribute('aria-label')||e.textContent||'').trim().slice(0,30)+'»').join('\n')`)
		var missed []string
		for _, line := range strings.Split(fmt.Sprint(v), "\n") {
			id, rest, _ := strings.Cut(line, "|")
			if !seen[id] {
				missed = append(missed, rest)
			}
		}
		if len(missed) > 6 {
			missed = append(missed[:6], fmt.Sprintf("… và %d thứ nữa", len(missed)-6))
		}
		f.add("%s: Tab không tới %d trên %d thứ bấm được: %s", what, total-len(seen), total, strings.Join(missed, "; "))
	} else {
		fmt.Printf("  bàn phím %s: Tab tới đủ %d thứ bấm được, không cái nào bị che\n", what, total)
	}
	_, err = c.eval(`document.querySelectorAll('[data-tw]').forEach(e=>e.removeAttribute('data-tw')); true`)
	return err
}

// overflowText tìm chữ bị tràn hoặc bị cắt mà không cố ý: phần tử có chữ, chữ
// rộng hơn ô chứa nó, và không mang title (chỗ cắt có chủ đích luôn có title
// đủ chữ). Dùng cho bản tiếng Anh, nơi chuỗi dài hơn tiếng Việt.
func overflowText(c *cdp, f *m16Fail, what string) error {
	const js = `(()=>{
		const vis = e => { const r=e.getBoundingClientRect(), s=getComputedStyle(e); return r.width>1 && r.height>1 && s.visibility!=='hidden' && s.display!=='none'; };
		const out = [];
		for (const e of document.querySelectorAll('body *')) {
			if (!vis(e) || e.children.length > 0 || !e.textContent.trim()) continue;
			const s = getComputedStyle(e);
			if (s.overflowX === 'visible' && s.overflowY === 'visible') continue; // ô không cắt thì chữ tự xuống dòng hoặc đẩy bố cục; cuộn ngang đã kiểm riêng
			if (e.scrollWidth > e.clientWidth + 1 && !e.closest('[title]') && !e.closest('pre, code, [data-allow-overflow]')) {
				out.push(e.tagName.toLowerCase()+' «'+e.textContent.trim().slice(0,40)+'»');
			}
		}
		return out.slice(0,8).join('\n');
	})()`
	v, err := c.eval(js)
	if err != nil {
		return err
	}
	if s := fmt.Sprint(v); s != "" {
		for _, line := range strings.Split(s, "\n") {
			f.add("%s: chữ bị cắt mà không có title: %s", what, line)
		}
	}
	return nil
}
