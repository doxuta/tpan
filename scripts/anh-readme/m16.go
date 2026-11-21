package main

// Đồ nghề của mốc M16 (giao diện bản 2, agent-dev/TU-CHAY-M16.md), dùng chung
// Chrome không giao diện với các bộ ảnh khác:
//
//	-bo m16-do    đo từng màn bằng do-giao-dien.js (trang dài mấy màn hình, cỡ
//	              chữ, tỷ lệ chữ mono, ô viền màu chữ...) và in một bảng
//
// Chạy qua Makefile (`make do-giao-dien`), trên máy demo hoặc trên panel của
// máy ảo test (-panel). Chỉ đọc trang; riêng -ten-dai tạo thêm ba site tên dài
// qua API của máy DEMO để bản 1 (chưa có sẵn các site đó) đo được trên cùng dữ liệu.

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// measureJS là script đo của chủ dự án (bản gốc: agent-dev/M16-do-giao-dien.js),
// nhúng nguyên văn: cùng một file dán vào Console của trình duyệt cũng chạy được.
//
//go:embed do-giao-dien.js
var measureJS string

// Ba site tên dài của dữ liệu mẫu (từ 36 ký tự, dưới example.com, .net, .org).
// Phải trùng internal/demo (longSites) để bản 1 và bản 2 đo trên cùng tên.
var longNames = []string{
	"cua-hang-so-mot.khach-hang-vi-du.example.com", // 44 ký tự
	"hoc-truc-tuyen.lop-hoc-a.example.net",         // đúng 36 ký tự: ngưỡng của M16
	"tin-tuc.to-soan-bao-vi-du.example.org",        // 37 ký tự
}

// m16Opts là các cờ riêng của bộ M16.
type m16Opts struct {
	longNames bool   // tạo ba site tên dài qua API nếu máy demo chưa có
	site      string // site dùng cho màn "trang một site"; rỗng thì lấy site tên dài đầu tiên máy có
	// passwordFile: file chứa output của `tpan setup` hoặc `tpan admin passwd`
	// trên máy ảo; dùng khi chạy tay với -panel mà không muốn mật khẩu đi qua dòng lệnh.
	passwordFile string
	// Bộ m16-that (m16_that.go).
	step, userName, waitFile string
	// threshold: bộ m16-do kiểm các ngưỡng cứng của M16 (mục 7 của
	// docs/thay-doi/M16-thiet-ke.md) và thoát mã khác 0 khi sai một cái.
	threshold bool
	// only: bộ m16 chỉ chụp các ảnh có tên này (cách nhau bằng dấu phẩy) và bỏ
	// phần bàn phím, tiếng Anh: để phiên chính soát nhanh sau mỗi đợt thợ.
	only string
}

// watchConsole bật các miền CDP phát sự kiện lỗi. Gọi một lần sau khi nối.
func (c *cdp) watchConsole() error {
	for _, m := range []string{"Runtime.enable", "Log.enable"} {
		if _, err := c.call(m, map[string]any{}); err != nil {
			return err
		}
	}
	return nil
}

// consoleErrors trả các lỗi console, ngoại lệ JS và dòng log mức lỗi của trình
// duyệt (gồm vi phạm CSP và request hỏng) từ lần gọi trước, rồi xoá bộ đệm.
// Gọi một lệnh rỗng trước để đọc hết sự kiện Chrome đã gửi tới lúc này.
func (c *cdp) consoleErrors() []string {
	_, _ = c.eval(`1`)
	var out []string
	for _, e := range c.events {
		switch e.Method {
		case "Runtime.exceptionThrown":
			var p struct {
				ExceptionDetails struct {
					Text      string `json:"text"`
					Exception struct {
						Description string `json:"description"`
					} `json:"exception"`
				} `json:"exceptionDetails"`
			}
			_ = json.Unmarshal(e.Params, &p)
			out = append(out, "ngoại lệ JS: "+strings.TrimSpace(p.ExceptionDetails.Text+" "+firstLine(p.ExceptionDetails.Exception.Description)))
		case "Runtime.consoleAPICalled":
			var p struct {
				Type string `json:"type"`
				Args []struct {
					Value       any    `json:"value"`
					Description string `json:"description"`
				} `json:"args"`
			}
			_ = json.Unmarshal(e.Params, &p)
			if p.Type != "error" && p.Type != "assert" {
				continue
			}
			var parts []string
			for _, a := range p.Args {
				if a.Description != "" {
					parts = append(parts, firstLine(a.Description))
				} else if a.Value != nil {
					parts = append(parts, fmt.Sprint(a.Value))
				}
			}
			out = append(out, "console."+p.Type+": "+strings.Join(parts, " "))
		case "Log.entryAdded":
			var p struct {
				Entry struct {
					Source string `json:"source"`
					Level  string `json:"level"`
					Text   string `json:"text"`
					URL    string `json:"url"`
				} `json:"entry"`
			}
			_ = json.Unmarshal(e.Params, &p)
			if p.Entry.Level != "error" {
				continue
			}
			out = append(out, fmt.Sprintf("log trình duyệt (%s): %s %s", p.Entry.Source, p.Entry.Text, p.Entry.URL))
		}
	}
	c.events = nil
	return out
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// settle chờ màn tải xong dữ liệu: có tiêu đề, không còn chữ "Đang tải", font đã về.
func settle(c *cdp) error {
	js := `!!document.querySelector('main h1') && !document.body.innerText.includes('Đang tải') && !document.body.innerText.includes('Loading')`
	if err := c.waitTrue(js, 25*time.Second); err != nil {
		return fmt.Errorf("chờ màn tải xong: %w", err)
	}
	if _, err := c.eval(`document.fonts.ready.then(()=>true)`); err != nil {
		return err
	}
	time.Sleep(900 * time.Millisecond)
	// Khối tải lười (ma trận cô lập, log...) có thể hiện "Đang tải" sau nhịp đầu.
	return c.waitTrue(`!document.body.innerText.includes('Đang tải')`, 25*time.Second)
}

// ensureLongSites tạo các site tên dài còn thiếu qua API (máy demo). Mỗi site
// một job; chờ job xong rồi mới tạo site sau, vì máy chỉ chạy một job một lúc.
func ensureLongSites(c *cdp) error {
	for _, d := range longNames {
		js := fmt.Sprintf(`(async()=>{
			const have = await fetch('/api/v1/sites').then(r=>r.json());
			if (have.some(s=>s.domain===%q)) return 'có sẵn';
			const r = await fetch('/api/v1/sites',{method:'POST',headers:{'Content-Type':'application/json'},
				body:JSON.stringify({domain:%q,title:'Site tên dài',email:'admin@example.com'})});
			if (!r.ok) return 'lỗi tạo: HTTP '+r.status+' '+await r.text();
			const id = (await r.json()).job_id;
			for (let i=0;i<300;i++) {
				const j = await fetch('/api/v1/jobs/'+id).then(r=>r.json());
				if (j.job.status!=='running') return j.job.status;
				await new Promise(r=>setTimeout(r,200));
			}
			return 'hết giờ';
		})()`, d, d)
		v, err := c.eval(js)
		if err != nil {
			return fmt.Errorf("tạo site %s: %w", d, err)
		}
		if s := fmt.Sprint(v); s != "có sẵn" && s != "succeeded" {
			return fmt.Errorf("tạo site %s: %s", d, s)
		}
		fmt.Printf("site tên dài %s: %v\n", d, v)
	}
	// Bảng điều khiển đọc phép kiểm đã lưu: chạy health đầy đủ để site mới có đủ ga.
	if _, err := c.eval(`fetch('/api/v1/health/run',{method:'POST'}).then(r=>r.status)`); err != nil {
		return err
	}
	return nil
}

// measureRow là các trường của do-giao-dien.js mà bảng in ra dùng tới.
type measureRow struct {
	Man      string   `json:"man"`
	CuaSo    []int    `json:"cuaSo"`
	DaiPx    int      `json:"trangDaiPx"`
	ManHinh  float64  `json:"soManHinh"`
	KyTu     int      `json:"kyTu"`
	Nho      int      `json:"phanTramChuTu13pxTroXuong"`
	Mono     int      `json:"phanTramChuMono"`
	VienDen  int      `json:"oVienDen"`
	MauVien  []string `json:"mauOVienDen"`
	ChuNho   []string `json:"chuDuoi12px"`
	ChoBam   *int     `json:"choBamDuocTrongNoiDung"`
	NhoNhat  float64  `json:"coChuNhoNhatPx"`
	CuonNgng bool     `json:"cuonNgangCaTrang"`
	TenSite  *struct {
		SoDong int    `json:"soDong"`
		Ten    string `json:"ten"`
	} `json:"tenSiteNhieuDongNhat"`
	Hang5    *int `json:"dayHangThu5Px"`
	TenBanDo []struct {
		Ten    string `json:"ten"`
		SoDong int    `json:"soDong"`
		BiCat  bool   `json:"biCat"`
	} `json:"tenSiteBanDo"`
}

// thresholds kiểm các ngưỡng cứng của M16 trên kết quả đo (cửa sổ 1440 × 900).
// key là khoá của màn trong measureM16. Trả các điều không đạt.
func thresholds(key string, r measureRow) []string {
	var bad []string
	if r.NhoNhat < 12 {
		bad = append(bad, fmt.Sprintf("có chữ %v px (ngưỡng: không chữ nào dưới 12 px)", r.NhoNhat))
	}
	if r.VienDen > 0 {
		bad = append(bad, fmt.Sprintf("%d ô viền màu chữ chính (ngưỡng: 0)", r.VienDen))
	}
	if r.CuonNgng {
		bad = append(bad, "cả trang cuộn ngang")
	}
	switch key {
	case "site-list":
		if r.Mono > 15 {
			bad = append(bad, fmt.Sprintf("chữ mono %d%% (ngưỡng: không quá 15%%)", r.Mono))
		}
	case "site", "site-hong":
		if r.ManHinh > 1.5 {
			bad = append(bad, fmt.Sprintf("tab Tổng quan dài %.1f màn hình (ngưỡng: không quá 1,5)", r.ManHinh))
		}
		if r.Mono > 30 {
			bad = append(bad, fmt.Sprintf("chữ mono %d%% (ngưỡng: không quá 30%%)", r.Mono))
		}
	case "databases":
		if r.Hang5 == nil {
			bad = append(bad, "bảng chưa có tới 5 hàng để đo (dữ liệu mẫu phải có từ 5 DB)")
		} else if r.Hang5 != nil && len(r.CuaSo) == 2 && *r.Hang5 > r.CuaSo[1] {
			bad = append(bad, fmt.Sprintf("đáy hàng thứ 5 ở %d px, quá một màn hình %d px", *r.Hang5, r.CuaSo[1]))
		}
	case "security":
		if len(r.CuaSo) == 2 && r.DaiPx > 2*r.CuaSo[1] {
			bad = append(bad, fmt.Sprintf("lúc mới mở dài %d px, quá 2 màn hình", r.DaiPx))
		}
	case "dashboard":
		if len(r.TenBanDo) == 0 {
			bad = append(bad, "không thấy tên site nào mang data-site-name trên bản đồ tuyến")
		}
		for _, n := range r.TenBanDo {
			if n.SoDong > 1 {
				bad = append(bad, fmt.Sprintf("tên %s xuống %d dòng trên bản đồ tuyến", n.Ten, n.SoDong))
			}
			if len([]rune(n.Ten)) <= 36 && n.BiCat {
				bad = append(bad, fmt.Sprintf("tên %s (%d ký tự) bị cắt trên bản đồ tuyến", n.Ten, len([]rune(n.Ten))))
			}
		}
	}
	return bad
}

// measureM16 mở lần lượt các màn chính, chạy do-giao-dien.js và in kết quả:
// mỗi màn một dòng JSON đầy đủ, rồi một bảng Markdown để dán vào tài liệu.
func measureM16(c *cdp, base string, o m16Opts) error {
	if err := c.watchConsole(); err != nil {
		return err
	}
	if _, err := c.eval(`localStorage.setItem('tpan-theme','light');localStorage.setItem('tpan-lang','vi')`); err != nil {
		return err
	}
	if o.longNames {
		if err := ensureLongSites(c); err != nil {
			return err
		}
	}
	site := o.site
	if site == "" {
		v, err := c.eval(`fetch('/api/v1/sites').then(r=>r.json()).then(l=>{const a=l.filter(s=>s.status==='active').map(s=>s.domain).sort((x,y)=>y.length-x.length);return a[0]||''})`)
		if err != nil {
			return err
		}
		site, _ = v.(string)
		if site == "" {
			return errors.New("máy chưa có site nào để đo trang một site (tạo site trước, hoặc truyền -site)")
		}
	}
	type screen struct{ key, name, path string }
	screens := []screen{
		{"dashboard", "Bảng điều khiển", "/"},
		{"tools", "Công cụ", "/tools"},
		{"site-list", "Site (danh sách)", "/sites"},
		{"site", "Trang một site", "/sites/" + site},
	}
	// Site đang hỏng của máy demo: tab Tổng quan phải đạt ngưỡng cả khi có lỗi để hiện.
	if c.str(`fetch('/api/v1/sites').then(r=>r.json()).then(l=>l.some(s=>s.domain==='wiki.example.org'&&s.status==='active')?'có':'')`) != "" && site != "wiki.example.org" {
		screens = append(screens, screen{"site-hong", "Trang một site đang hỏng", "/sites/wiki.example.org"})
	}
	screens = append(screens,
		screen{"databases", "Cơ sở dữ liệu", "/databases"},
		screen{"security", "Bảo mật", "/security"},
		screen{"settings", "Cài đặt", "/settings"},
		screen{"users", "Người dùng", "/users"},
	)
	var rows []measureRow
	var names []string
	var failed []string
	for _, s := range screens {
		if err := c.goTo(base+s.path, s.path); err != nil {
			return err
		}
		if err := settle(c); err != nil {
			return fmt.Errorf("%s: %w", s.name, err)
		}
		if v, _ := c.eval(`document.querySelector('main h1')?.innerText||''`); strings.Contains(fmt.Sprint(v), "Không có trang này") {
			fmt.Printf("%s (%s): bản này chưa có màn này\n", s.name, s.path)
			continue
		}
		v, err := c.eval(measureJS)
		if err != nil {
			return fmt.Errorf("%s: chạy script đo: %w", s.name, err)
		}
		raw, _ := v.(string)
		fmt.Printf("%s\t%s\n", s.name, raw)
		var r measureRow
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			return fmt.Errorf("%s: đọc kết quả đo: %w", s.name, err)
		}
		rows, names = append(rows, r), append(names, s.name)
		if o.threshold {
			for _, b := range thresholds(s.key, r) {
				failed = append(failed, s.name+": "+b)
			}
		}
		if errs := c.consoleErrors(); len(errs) > 0 {
			// Che địa chỉ panel (IP của máy ảo) trước khi in: log này vào repo.
			fmt.Printf("  LỖI CONSOLE ở %s:\n    %s\n", s.name, strings.ReplaceAll(strings.Join(errs, "\n    "), base, "<panel>"))
		}
	}
	w, h := 0, 0
	if len(rows) > 0 && len(rows[0].CuaSo) == 2 {
		w, h = rows[0].CuaSo[0], rows[0].CuaSo[1]
	}
	fmt.Printf("\nCửa sổ %d × %d, chế độ sáng, tiếng Việt. Trang một site: %s\n\n", w, h, site)
	fmt.Println("| Màn | Trang dài | Chữ từ 13 px trở xuống | Chữ mono | Chỗ bấm được | Ô viền màu chữ | Cỡ chữ nhỏ nhất | Tên site dài nhất |")
	fmt.Println("|---|---|---|---|---|---|---|---|")
	for i, r := range rows {
		bam := "—"
		if r.ChoBam != nil {
			bam = fmt.Sprint(*r.ChoBam)
		}
		ten := "—"
		if r.TenSite != nil {
			ten = fmt.Sprintf("%d dòng", r.TenSite.SoDong)
		}
		fmt.Printf("| %s | %s màn hình | %d%% | %d%% | %s | %d | %s px | %s |\n",
			names[i], strings.Replace(fmt.Sprintf("%.1f", r.ManHinh), ".", ",", 1), r.Nho, r.Mono, bam, r.VienDen,
			strings.Replace(fmt.Sprint(r.NhoNhat), ".", ",", 1), ten)
	}
	if !o.threshold {
		return nil
	}
	fmt.Println()
	if len(failed) == 0 {
		fmt.Println("NGƯỠNG CỨNG: đạt hết.")
		return nil
	}
	fmt.Printf("NGƯỠNG CỨNG: %d điều không đạt:\n", len(failed))
	for _, f := range failed {
		fmt.Println("  -", f)
	}
	return fmt.Errorf("%d ngưỡng cứng không đạt", len(failed))
}
