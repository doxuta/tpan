package main

// Bộ ảnh của mốc M18 (sao lưu ra kho S3, cài lên máy ảo mới) trên máy demo:
//
//	-bo m18   các màn và lớp nổi của M18 ở ba khổ: 1440 × 900 sáng, tối, và
//	          390 × 844 sáng. Mỗi ảnh kiểm như bộ m16: không lỗi console, cả
//	          trang không cuộn ngang, không chữ dưới 12 px. Sau đó đi bàn phím
//	          (Tab) trên từng màn và mở bản tiếng Anh tìm chữ bị tràn.
//
// Chạy qua Makefile: `make anh-m18` (ảnh vào docs/bang-chung/M18).
//
// Dữ liệu demo (internal/demo/m18.go): đích S3 đã đặt (s3.example.com), blog
// có một bản chỉ còn trên kho, shop có một bản đẩy lỗi. Trang cài lên máy mới
// được điền sha256 GIẢ (sha256 của một chuỗi cố định): máy demo không tải gì.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// m18Nhan là chữ thật của nút và tiêu đề mà hai bộ M18 bấm hay tìm, chép từ
// bản tiếng Việt của web/src/i18n/backup-dich.ts, backup.ts, cai-may-moi.ts,
// cong-cu.ts. Bấm theo chữ đọc được (như m16_that.go) để ảnh và e2e hỏng ngay
// khi chữ đổi mà quên sửa ở đây. Ô nhập thì điền theo id (ổn định hơn nhãn).
var m18Nhan = struct {
	theDich, doiDich, xemTruoc, thuKetNoi  string // thẻ Đích ngoài máy (backup-dich.ts)
	cotODau, khoiPhuc, taiVeMay, daTaiVe   string // bảng backup (backup.ts)
	tieuDeMayMoi, sinhUserData, oCongCuMoi string // cai-may-moi.ts, cong-cu.ts
}{
	theDich: "Đích ngoài máy", doiDich: "Đổi đích", xemTruoc: "Xem trước", thuKetNoi: "Thử kết nối",
	cotODau: "Ở đâu", khoiPhuc: "Khôi phục", taiVeMay: "Tải về máy",
	daTaiVe:      "Đã tải về. Bấm Khôi phục để xem trước việc khôi phục.",
	tieuDeMayMoi: "Cài lên máy ảo mới", sinhUserData: "Sinh user-data", oCongCuMoi: "Cài lên máy ảo mới",
}

// Phạm vi tìm nút của thẻ Đích ngoài máy (Card: section aria-labelledby id của h2):
// nút "Xem trước" của thẻ không được khớp nhầm "Xem trước thay đổi" của thẻ lịch.
const theDichSel = `section[aria-labelledby="backup-target"]`

// fakeSha là sha256 giả cho máy demo: sha256 của một chuỗi cố định, đúng dạng 64 hex.
func fakeSha(arch string) string {
	s := sha256.Sum256([]byte("tpan-demo-" + arch))
	return hex.EncodeToString(s[:])
}

// markRow đánh dấu bằng data-m18-hang hàng của bản id trên bảng backup đang
// mở, để bấm nút trong hàng. Bảng không lọc hiện đúng các dòng items của GET
// /api/v1/backups theo đúng thứ tự (List.tsx: rows = items), và khi có cột Ở
// đâu thì cột sha256, Đường dẫn bị ẩn: nên hàng được chọn theo chỉ số của bản
// trong API, rồi kiểm lại hàng đó ghi đúng tên site.
func markRow(c *cdp, id string) error {
	v, err := c.eval(fmt.Sprintf(`(async()=>{document.querySelectorAll('[data-m18-hang]').forEach(e=>e.removeAttribute('data-m18-hang'));
		const items=(await fetch('/api/v1/backups').then(r=>r.json())).items||[];
		const i=items.findIndex(b=>b.id===%q); if(i<0) return 'API không có bản này';
		const sel=document.querySelector('#o-backup-filter'); if(sel&&sel.value) return 'bảng đang lọc theo '+sel.value;
		const trs=[...document.querySelectorAll('main section[aria-labelledby="backup-list"] tbody tr')];
		if(trs.length!==items.length) return 'bảng có '+trs.length+' hàng, API có '+items.length;
		const tr=trs[i]; if(!tr.textContent.includes(items[i].domain)) return 'hàng '+i+' không ghi '+items[i].domain;
		tr.setAttribute('data-m18-hang','1'); return ''})()`, id))
	if err != nil {
		return err
	}
	if s := fmt.Sprint(v); s != "" {
		return fmt.Errorf("không tìm được hàng của bản %s: %s", id, s)
	}
	return nil
}

// remoteOnlyID là id bản chỉ còn trên kho đầu tiên của domain theo API ("" khi không có).
func remoteOnlyID(c *cdp, domain string) string {
	return firstID(c, fmt.Sprintf(`fetch('/api/v1/backups').then(r=>r.json()).then(j=>((j.items||[]).find(b=>b.local===false&&b.remote==='stored'&&b.domain===%q)||{}).id||'')`, domain))
}

// checkClipped tìm thứ bấm được trong <main> bị cắt: lòi ra ngoài cửa sổ, hoặc
// ngoài một khung tổ tiên cắt phần tràn (overflow hidden/clip). Khung cuộn được
// (bảng có thanh cuộn riêng) không tính. Phép "cả trang cuộn ngang" không bắt
// được ca này, vì thẻ đã cắt mất phần tràn.
func checkClipped(c *cdp, f *m16Fail, what string) {
	v, err := c.eval(`(()=>{
		const out=[];
		for (const e of document.querySelectorAll('main a[href], main button, main input, main select, main textarea, main [role=radio]')) {
			const r=e.getBoundingClientRect(), s=getComputedStyle(e);
			if (r.width<=1 || r.height<=1 || s.visibility==='hidden' || s.display==='none') continue;
			let bad = r.right > innerWidth+1 || r.left < -1, scroller = false;
			for (let a=e.parentElement; a; a=a.parentElement) {
				const st=getComputedStyle(a);
				if (st.overflowX==='auto' || st.overflowX==='scroll') { scroller = true; break; }
				if (st.overflowX==='hidden' || st.overflowX==='clip') {
					const ar=a.getBoundingClientRect();
					if (r.right > ar.right+1 || r.left < ar.left-1) bad=true;
				}
			}
			if (scroller) continue;
			if (bad) out.push(e.tagName.toLowerCase()+' «'+(e.getAttribute('aria-label')||e.textContent||e.id||'').trim().slice(0,40)+'»');
		}
		return out.slice(0,6).join('\n');
	})()`)
	if err != nil {
		f.add("%s: không chạy được phép kiểm bị cắt: %v", what, err)
		return
	}
	if s := fmt.Sprint(v); s != "" {
		for _, line := range strings.Split(s, "\n") {
			f.add("%s: thứ bấm được bị cắt hoặc lòi ra ngoài: %s", what, line)
		}
	}
}

// restoreButtonsInView khẳng định, từ 1024 px trở lên (ảnh 1440 × 900), mọi
// nút "Khôi phục" trên hàng nằm trọn trong khung nhìn của bảng khi bảng chưa
// cuộn ngang: người dùng thấy nút mà không phải kéo thanh cuộn của bảng.
// checkClipped bỏ qua khung cuộn được nên không bắt được ca này. Ở 390 px bảng
// được phép cuộn ngang, không kiểm.
func restoreButtonsInView(c *cdp) error {
	v, err := c.eval(fmt.Sprintf(`(()=>{
		if (innerWidth < 1024) return '';
		const bs=[...document.querySelectorAll('main tr button')].filter(b=>b.textContent.trim()===%q);
		if (!bs.length) return 'không có nút nào';
		const bad=[];
		for (const b of bs) {
			let box=null;
			for (let a=b.parentElement; a; a=a.parentElement) { const o=getComputedStyle(a).overflowX; if (o==='auto'||o==='scroll') { box=a; break; } }
			if (box) box.scrollLeft=0;
			const r=b.getBoundingClientRect(), ar=box?box.getBoundingClientRect():{left:0,right:innerWidth};
			if (r.left < ar.left-1 || r.right > Math.min(ar.right, innerWidth)+1) bad.push((b.getAttribute('aria-label')||'').slice(0,60)+' (phải '+Math.round(r.right)+' > khung '+Math.round(Math.min(ar.right, innerWidth))+')');
		}
		return bad.length ? bad.length+'/'+bs.length+' nút ngoài khung: '+bad.slice(0,3).join('; ') : 'ok '+bs.length;
	})()`, m18Nhan.khoiPhuc))
	if err != nil {
		return err
	}
	s := fmt.Sprint(v)
	switch {
	case s == "":
		return nil
	case strings.HasPrefix(s, "ok "):
		fmt.Printf("  %s nút «%s» nằm trọn trong khung nhìn của bảng\n", strings.TrimPrefix(s, "ok "), m18Nhan.khoiPhuc)
		return nil
	}
	return fmt.Errorf("nút «%s» trên hàng: %s", m18Nhan.khoiPhuc, s)
}

// openRemoteDialog bấm Khôi phục trên hàng bản chỉ còn trên kho và chờ hộp
// thoại "Tải về máy trước khi khôi phục?".
func openRemoteDialog(c *cdp, domain string) error {
	id := remoteOnlyID(c, domain)
	if id == "" {
		return fmt.Errorf("API không có bản nào của %s chỉ còn trên kho", domain)
	}
	if err := markRow(c, id); err != nil {
		return err
	}
	if err := c.pressText(`[data-m18-hang] button`, m18Nhan.khoiPhuc); err != nil {
		return err
	}
	return c.waitTrue(`!!document.querySelector('[role=dialog],[role=alertdialog]')`, 10*time.Second)
}

// openTargetForm mở form đổi đích trên thẻ Đích ngoài máy (máy đã có đích).
// Bấm nút Disclosure (aria-expanded) chứ không theo chữ, để dùng được cả ở bản tiếng Anh.
func openTargetForm(c *cdp) error {
	if c.str(`document.querySelector('`+theDichSel+` button[aria-expanded]')?.textContent.trim()||''`) == "" {
		return fmt.Errorf("thẻ đích không có nút «%s»", m18Nhan.doiDich)
	}
	if err := c.press(theDichSel + ` button[aria-expanded="false"]`); err != nil {
		return err
	}
	return c.waitTrue(`!!document.querySelector('#o-dich-endpoint')`, 10*time.Second)
}

// genUserData điền sha256 giả (và email nếu ô còn trống) rồi bấm Sinh user-data.
func genUserData(c *cdp) error {
	for _, n := range []struct{ sel, arch string }{{"#ci-sha-amd64", "amd64"}, {"#ci-sha-arm64", "arm64"}} {
		if err := fillID(c, n.sel, fakeSha(n.arch)); err != nil {
			return err
		}
	}
	if c.str(`(()=>{const e=document.querySelector('#ci-email'); return e&&!e.value?'trống':''})()`) != "" {
		if err := fillID(c, "#ci-email", "admin@example.com"); err != nil {
			return err
		}
	}
	// Nút gửi của form (chữ "Sinh user-data"), tìm theo type để dùng được cả ở bản tiếng Anh.
	if err := c.press(`#ci-form button[type=submit]`); err != nil {
		return err
	}
	// #ci-user-data là id của h2; thẻ (Card) là section aria-labelledby id đó.
	if err := c.waitTrue(`(document.querySelector('section[aria-labelledby="ci-user-data"]')?.textContent||'').includes('#cloud-config')`, 15*time.Second); err != nil {
		// Máy demo: chữ báo lỗi của form không có bí mật, in ra để biết vì sao.
		return fmt.Errorf("bấm «%s» không ra thẻ user-data có #cloud-config (báo lỗi trên trang: %s): %w", m18Nhan.sinhUserData,
			c.str(`[...document.querySelectorAll('main [role=alert], main [aria-invalid=true]')].map(e=>(e.id||'')+' '+e.textContent.trim().slice(0,160)).join(' | ')`), err)
	}
	return nil
}

func photosM18(c *cdp, base, out, only string) error {
	blog := "blog.example.com" // site demo có bản chỉ còn trên kho (internal/demo/m18.go)

	shots := []m16Shot{
		{name: "backup", path: "/backups"},
		{name: "backup-doi-dich", path: "/backups", prep: openTargetForm},
		{name: "backup-hop-kho", path: "/backups", dialog: true, prep: func(c *cdp) error { return openRemoteDialog(c, blog) }},
		{name: "may-moi", path: "/tools/new-server"},
		{name: "may-moi-user-data", path: "/tools/new-server", prep: genUserData},
		{name: "cong-cu", path: "/tools"},
	}

	// Điều phải thấy trên ảnh, ngoài các phép kiểm chung.
	mainHas := func(label string) error { return mainHasText(c, label) }
	expect := map[string]func() error{
		"backup": func() error {
			if c.str(`document.querySelector('`+theDichSel+` h2')?.innerText.trim()||''`) != m18Nhan.theDich {
				return fmt.Errorf("không có thẻ «%s»", m18Nhan.theDich)
			}
			if c.str(`[...document.querySelectorAll('main th')].some(th=>th.textContent.trim()===`+fmt.Sprintf("%q", m18Nhan.cotODau)+`)?'có':''`) == "" {
				return fmt.Errorf("bảng không có cột «%s»", m18Nhan.cotODau)
			}
			return restoreButtonsInView(c)
		},
		"backup-hop-kho": func() error {
			if c.str(fmt.Sprintf(`[...document.querySelectorAll('[role=dialog] button,[role=alertdialog] button')].some(b=>b.textContent.trim()===%q)?'có':''`, m18Nhan.taiVeMay)) == "" {
				return fmt.Errorf("hộp thoại không có nút «%s»", m18Nhan.taiVeMay)
			}
			return nil
		},
		"backup-doi-dich": func() error {
			if c.str(`document.querySelector('`+theDichSel+` button[aria-expanded]')?.textContent.trim()||''`) != m18Nhan.doiDich {
				return fmt.Errorf("nút mở form đích không ghi «%s»", m18Nhan.doiDich)
			}
			return nil
		},
		"may-moi": func() error {
			if c.str(`document.querySelector('#ci-form button[type=submit]')?.textContent.trim()||''`) != m18Nhan.sinhUserData {
				return fmt.Errorf("nút gửi form không ghi «%s»", m18Nhan.sinhUserData)
			}
			return mainHas(m18Nhan.tieuDeMayMoi)
		},
		"cong-cu": func() error {
			if c.str(`document.querySelector('main a[href="/tools/new-server"]')?'có':''`) == "" {
				return errors.New("trang Công cụ không có liên kết tới /tools/new-server")
			}
			return mainHas(m18Nhan.oCongCuMoi)
		},
	}
	return photoSet(c, base, out, only, "M18", shots, expect)
}

// mainHasText báo lỗi khi <main> không có chữ label.
func mainHasText(c *cdp, label string) error {
	if !strings.Contains(c.str(`document.querySelector('main')?.innerText||''`), label) {
		return fmt.Errorf("màn không có chữ «%s»", label)
	}
	return nil
}

// photoSet chụp một bộ ảnh của một mốc trên máy demo (M18, M17): mỗi ảnh ở
// 1440 sáng, 1440 tối và 390 sáng, kiểm như bộ m16 cộng expect của riêng ảnh
// đó; rồi đi bàn phím mọi màn (trừ ảnh hộp thoại) và mở bản tiếng Anh tìm chữ
// tràn. only (cờ -chi) chỉ chụp vài ảnh và bỏ phần bàn phím, tiếng Anh.
func photoSet(c *cdp, base, out, only, label string, shots []m16Shot, expect map[string]func() error) error {
	if err := c.watchConsole(); err != nil {
		return err
	}
	var fail m16Fail
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
				time.Sleep(400 * time.Millisecond) // hiệu ứng mở hộp thoại, form
			}
			checkPage(c, &fail, file, s.dialog)
			checkClipped(c, &fail, file)
			if chk := expect[s.name]; chk != nil {
				if err := chk(); err != nil {
					fail.add("%s: %v", file, err)
				}
			}
			if !s.dialog { // chụp cả trang từ đầu, kẻo thanh đầu trang dính hiện giữa ảnh
				if _, err := c.eval(`window.scrollTo(0,0)`); err != nil {
					return err
				}
				time.Sleep(200 * time.Millisecond)
			}
			// Cả trang: thẻ Đích ngoài máy nằm dưới cùng màn Backup, ngoài khung 900 px.
			// Ảnh có hộp thoại chụp đúng khung cửa sổ: lớp cố định lệch khi chụp cả trang.
			take := snapFull
			if s.dialog {
				take = snap
			}
			if err := take(c, out, file); err != nil {
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

	// Bàn phím: mọi màn, và hai trạng thái có form (form đổi đích, user-data đã
	// sinh); hộp thoại nằm ngoài <main> nên không đi bằng tabWalk.
	if err := c.viewport(width, height); err != nil {
		return err
	}
	fmt.Println("\nBàn phím (1440 × 900, tiếng Việt):")
	for _, s := range shots {
		if s.dialog {
			continue
		}
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

	// Tiếng Anh: mọi ảnh trừ hộp thoại (nút trên hàng bấm theo chữ tiếng Việt);
	// form đổi đích và user-data mở bằng selector nên chạy được ở cả hai thứ tiếng.
	fmt.Println("\nTiếng Anh (không cuộn ngang, không chữ hay nút bị cắt):")
	for _, size := range [][2]int{{width, height}, {390, 844}} {
		if err := c.viewport(size[0], size[1]); err != nil {
			return err
		}
		for _, s := range shots {
			if s.dialog {
				continue
			}
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

	fmt.Printf("\nĐã chụp %d ảnh vào %s. Không đạt: %d\n", count, out, len(fail.list))
	for _, s := range fail.list {
		fmt.Println("  -", s)
	}
	if len(fail.list) > 0 {
		return fmt.Errorf("bộ ảnh %s có điều không đạt", label)
	}
	return nil
}
