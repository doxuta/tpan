package main

// Bộ ảnh của mốc M17 (lồng theo site, mức tối đa theo gói, số liệu, thử tấn
// công) trên máy demo:
//
//	-bo m17   các màn của M17 ở ba khổ (1440 × 900 sáng, tối; 390 × 844 sáng),
//	          kiểm như bộ m18 (photoSet): không lỗi console, không cuộn ngang
//	          cả trang, không chữ dưới 12 px, không nút bị cắt; rồi đi bàn phím
//	          và mở bản tiếng Anh tìm chữ tràn.
//
// Chạy qua Makefile: `make anh-m17` (ảnh vào docs/bang-chung/M17).
//
// Dữ liệu demo (internal/demo/m17.go): gói có lồng, site trong lồng và ngoài
// lồng, số liệu 7 ngày, một lượt thử tấn công đã xong. Site để chụp trang một
// site được chọn theo GET /api/v1/cage lúc chạy, không viết cứng tên.

import (
	"errors"
	"fmt"
)

// m17Nhan là chữ thật mà bộ ảnh tìm, chép từ bản tiếng Việt của
// web/src/i18n/tai-nguyen.ts, goi.ts, site.ts, bao-mat.ts: chữ đổi mà quên
// sửa ở đây thì ảnh hỏng ngay.
var m17Nhan = struct {
	tieuDeTaiNguyen, theoUser, goiY string // tai-nguyen.ts
	cotLong                         string // goi.ts
	vaoLong, raLong                 string // site.ts
	thuTanCong                      string // bao-mat.ts
	oCongCu                         string // cong-cu.ts
}{
	tieuDeTaiNguyen: "Tài nguyên đang dùng", theoUser: "Theo user", goiY: "Gợi ý mức tối đa",
	cotLong: "Lồng",
	vaoLong: "Vào lồng", raLong: "Ra khỏi lồng",
	thuTanCong: "Thử tấn công",
	oCongCu:    "Tài nguyên",
}

// cageSite trả tên miền đầu tiên có trạng thái lồng state theo GET /api/v1/cage.
func cageSite(c *cdp, state string) (string, error) {
	d := c.str(fmt.Sprintf(`fetch('/api/v1/cage').then(r=>r.json()).then(j=>((j||[]).find(r=>r.state===%q)||{}).domain||'')`, state))
	if d == "" {
		return "", fmt.Errorf("máy demo không có site nào ở trạng thái lồng %q", state)
	}
	return d, nil
}

// hasButton cho biết <main> có một nút ghi đúng chữ s.
func hasButton(c *cdp, s string) bool {
	return c.str(fmt.Sprintf(`[...document.querySelectorAll('main button')].some(b=>b.textContent.trim()===%q)?'có':''`, s)) != ""
}

func photosM17(c *cdp, base, out, only string) error {
	caged, err := cageSite(c, "trong")
	if err != nil {
		return err
	}
	free, err := cageSite(c, "ngoai")
	if err != nil {
		return err
	}
	shots := []m16Shot{
		{name: "tai-nguyen", path: "/usage"},
		{name: "goi", path: "/packages"},
		{name: "site-trong-long", path: "/sites/" + caged},
		{name: "site-ngoai-long", path: "/sites/" + free},
		{name: "bao-mat", path: "/security"},
		{name: "cong-cu", path: "/tools"},
	}
	expect := map[string]func() error{
		"tai-nguyen": func() error {
			for _, s := range []string{m17Nhan.tieuDeTaiNguyen, m17Nhan.theoUser, m17Nhan.goiY, caged} {
				if err := mainHasText(c, s); err != nil {
					return err
				}
			}
			return nil
		},
		"goi": func() error {
			if c.str(`[...document.querySelectorAll('main th')].some(th=>th.textContent.trim()===`+fmt.Sprintf("%q", m17Nhan.cotLong)+`)?'có':''`) == "" {
				return fmt.Errorf("bảng gói không có cột «%s»", m17Nhan.cotLong)
			}
			return nil
		},
		"site-trong-long": func() error {
			if !hasButton(c, m17Nhan.raLong) {
				return fmt.Errorf("trang site trong lồng không có nút «%s»", m17Nhan.raLong)
			}
			return nil
		},
		"site-ngoai-long": func() error {
			if !hasButton(c, m17Nhan.vaoLong) {
				return fmt.Errorf("trang site ngoài lồng không có nút «%s»", m17Nhan.vaoLong)
			}
			return nil
		},
		"bao-mat": func() error {
			if !hasButton(c, m17Nhan.thuTanCong) {
				return fmt.Errorf("màn Bảo mật không có nút «%s»", m17Nhan.thuTanCong)
			}
			// Bảng kết quả lượt gần nhất: có mã đường L1 và C1.
			for _, p := range []string{"L1", "C1"} {
				if err := mainHasText(c, p); err != nil {
					return err
				}
			}
			return nil
		},
		"cong-cu": func() error {
			if c.str(`document.querySelector('main a[href="/usage"]')?'có':''`) == "" {
				return errors.New("trang Công cụ không có liên kết tới /usage")
			}
			return mainHasText(c, m17Nhan.oCongCu)
		},
	}
	return photoSet(c, base, out, only, "M17", shots, expect)
}
