package web

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"unicode"
)

// Bất biến B6, B7, B8, B11 của docs/thay-doi/M16-thiet-ke.md (mục 4). Test chỉ
// đọc mã nguồn trong web/src (không cần build giao diện) và giữ các luật mà
// trình biên dịch TypeScript không biết: cỡ chữ, màu viết thẳng, class cấm,
// chỗ được dùng font-mono, thứ có thể mở lỗ XSS hay gọi ra ngoài origin.
//
// Việc đổi giao diện bản 1 sang bản 2 (mốc M16) diễn ra theo đợt, nên có hai
// danh sách "chưa đổi" (chuaDoi, chuaDoiNhan) chỉ được ngắn đi: thư mục nằm
// trong danh sách không bị kiểm luật 1 đến 6; thư mục đã sạch mà còn nằm trong
// danh sách thì test lỗi, buộc người nhận phần việc gỡ nó ra. M16 đã đổi xong
// mọi màn nên cả hai rỗng: test quét toàn bộ web/src. Không thêm lại mục nào;
// một lần đổi lớn sau này cần danh sách thì ghi lý do trong tài liệu thay đổi.

// chuaDoi: tiền tố đường dẫn (tính từ web/) của mã chưa đổi sang bản 2.
var chuaDoi = []string{}

// chuaDoiNhan: file chuỗi i18n còn nhãn viết hoa cả câu (B11). Cùng cơ chế.
var chuaDoiNhan = []string{}

// choPhepVietHoa: chuỗi i18n được phép viết hoa cả câu. Mỗi mục kèm lý do.
// Rỗng lúc đầu; chỉ thêm khi có lý do thật (ví dụ tên một lệnh phải đúng chữ hoa).
var choPhepVietHoa = map[string]string{}

// biến môi trường cho thợ kiểm riêng thư mục của mình (make kieu-dang DIR=...).
const bienMoiTruongThu = "KIEU_DANG_THU"

// ---------- Bỏ chú thích ----------

// boChuThich xoá chú thích `/* ... */` và `// ...` khỏi mã TypeScript để mẫu
// không bắt nhầm chữ trong chú thích (ví dụ chú thích nhắc tới "uppercase" hay
// ghi một đường link). Quét một lượt từ trái sang phải để chú thích nào mở
// trước thì thắng (một `/*` nằm trong chú thích `//` không mở chú thích khối).
// `//` đứng ngay sau dấu `:` không phải chú thích: để không cắt `https://x`.
// Giữ nguyên các dấu xuống dòng để số dòng trong thông báo lỗi vẫn đúng.
//
// Không hiểu chuỗi hay regex: một `//` trong chuỗi mà không đứng sau `:` bị
// cắt. Với mã giao diện (class Tailwind, đường dẫn API) điều đó chưa gây hại.
func boChuThich(src string) string {
	var sb strings.Builder
	for i := 0; i < len(src); {
		switch {
		case strings.HasPrefix(src[i:], "/*"):
			end := strings.Index(src[i+2:], "*/")
			stop := len(src)
			if end >= 0 {
				stop = i + 2 + end + 2
			}
			sb.WriteString(strings.Repeat("\n", strings.Count(src[i:stop], "\n")))
			i = stop
		case strings.HasPrefix(src[i:], "//") && (i == 0 || src[i-1] != ':'):
			for i < len(src) && src[i] != '\n' {
				i++
			}
		default:
			sb.WriteByte(src[i])
			i++
		}
	}
	return sb.String()
}

// ---------- Luật ----------

// viPham là một chỗ phạm luật trong một file.
type viPham struct {
	luat int    // 1..8 (số luật trong giao việc và mục 4 của đặc tả)
	dong int    // dòng, tính từ 1
	mau  string // đoạn mã bị bắt
	msg  string // nói rõ dùng gì thay
}

// Các cỡ chữ (px) được phép (mục 3.2): 12, 12,5, 13, 14, 16, 18, 24.
var coChuDuocPhep = []float64{12, 12.5, 13, 14, 16, 18, 24}

// Chia mã thành "token" như một chuỗi class: dãy ký tự không có khoảng trắng,
// nháy, backtick hay `:`, đứng sau đầu chuỗi, khoảng trắng, nháy, backtick hoặc
// `:` (để biến thể như `hover:uppercase` hay `md:text-sm` cũng thành token
// riêng). Nhờ vậy "đứng riêng như một class" là so sánh nguyên token, không
// phải đoán ranh giới bằng regex: `border-line` không khớp `border-line-dash`,
// `rounded` không khớp `rounded-card`, "uppercased" không khớp "uppercase".
var reToken = regexp.MustCompile("(?:^|[\\s\"'`:])([^\\s\"'`:]+)")

var (
	reCoChuPx   = regexp.MustCompile(`^text-\[([0-9]+(?:\.[0-9]+)?)px\]$`)
	reCoChuTen  = regexp.MustCompile(`^text-(?:xs|sm|base|lg|xl|[234]xl)(?:/.*)?$`)
	reBorderInk = regexp.MustCompile(`^border-ink(?:/.*)?$`)
	reBgInk     = regexp.MustCompile(`^bg-ink(?:/.*)?$`)
	reBorderLn  = regexp.MustCompile(`^border-line(?:/.*)?$`)
)

// Nơi được dùng (tiền tố; kết thúc bằng "/" là cả thư mục, còn lại là một file).
var (
	noiBorderLine = []string{"src/components/", "src/Layout.tsx"}
	noiFontMono   = []string{
		"src/components/", "src/Layout.tsx", "src/screens/site/LiveLog.tsx",
		"src/ai/AnswerText.tsx", "src/screens/duyet-plan/index.tsx",
	}
	noiBgInk = []string{
		"src/Layout.tsx", "src/screens/site/LiveLog.tsx",
		"src/components/MetricBar.tsx", "src/components/UsageMeter.tsx",
	}
	noiToUpperCase = []string{"src/components/Avatar.tsx"}
)

// duoiTienTo cho biết rel có nằm dưới (hay đúng bằng) một trong các tiền tố.
func duoiTienTo(rel string, tienTo []string) bool {
	return slices.ContainsFunc(tienTo, func(p string) bool { return strings.HasPrefix(rel, p) })
}

// xetToken áp luật 1, 2, 4, 5 lên một token class. Trả (0, "") nếu hợp lệ.
func xetToken(rel, tok string) (int, string) {
	// Dấu ! (important) ở đầu hay cuối không đổi nghĩa của class.
	tok = strings.Trim(tok, "!")

	// Luật 1: cỡ chữ.
	if m := reCoChuPx.FindStringSubmatch(tok); m != nil {
		n, _ := strconv.ParseFloat(m[1], 64)
		if !slices.Contains(coChuDuocPhep, n) {
			return 1, "cỡ chữ " + m[1] + "px không có trong {12; 12,5; 13; 14; 16; 18; 24}: dùng cỡ gần nhất trong danh sách (không chữ nào dưới 12px)"
		}
		return 0, ""
	}
	if reCoChuTen.MatchString(tok) {
		return 1, "không dùng cỡ chữ có tên (text-sm, text-xl...): viết text-[13px] hoặc cỡ khác trong {12; 12,5; 13; 14; 16; 18; 24}"
	}

	// Luật 2: class bản 1 bị bỏ, hay hình khối không có trong bản 2.
	switch {
	case reBorderInk.MatchString(tok):
		return 2, "bản 2 không có viền đậm: dùng border-hair (thẻ, kẻ) hoặc border-line (nút phụ, chỉ trong components/)"
	case tok == "border-card-border":
		return 2, "biến --card-border bị bỏ: dùng border-hair"
	case tok == "border-line-dash":
		return 2, "--line-dash đã đổi tên: dùng border-route-dash (bản đồ tuyến)"
	case tok == "uppercase":
		return 2, "nhãn viết thường (mục 3.2): bỏ uppercase"
	case tok == "tracking-[0.08em]" || tok == "tracking-[0.06em]":
		return 2, "bỏ giãn chữ của nhãn mono bản 1: nhãn bản 2 không giãn chữ"
	case strings.HasPrefix(tok, "rounded-["):
		return 2, "không đặt bo góc bằng giá trị tự do: dùng rounded-card, rounded-ctl, rounded-item hoặc rounded-full"
	case tok == "rounded", tok == "rounded-sm", tok == "rounded-md", tok == "rounded-lg", tok == "rounded-xl", tok == "rounded-2xl":
		return 2, "bo góc của Tailwind mặc định không dùng: dùng rounded-card, rounded-ctl, rounded-item hoặc rounded-full"
	case tok == "shadow" || (strings.HasPrefix(tok, "shadow-") && tok != "shadow-pop" && tok != "shadow-none"):
		return 2, "bản 2 không đổ bóng thẻ: chỉ shadow-pop (lớp nổi: menu, hộp thoại) hoặc shadow-none"
	case reBorderLn.MatchString(tok) && !duoiTienTo(rel, noiBorderLine):
		return 2, "border-line (viền nút phụ, ô tìm) chỉ ở src/components/ và src/Layout.tsx: màn dùng Button, Card hoặc border-hair"
	}

	// Luật 4: font-mono.
	if tok == "font-mono" && !duoiTienTo(rel, noiFontMono) {
		return 4, "font-mono chỉ ở src/components/, Layout.tsx, LiveLog.tsx, AnswerText.tsx, duyet-plan/index.tsx: dùng MonoText (bấm là chép), Code hoặc Code block"
	}

	// Luật 5: bg-ink.
	if reBgInk.MatchString(tok) && !duoiTienTo(rel, noiBgInk) {
		return 5, "bg-ink chỉ ở Layout.tsx, LiveLog.tsx, MetricBar.tsx, UsageMeter.tsx: dùng bg-soft hoặc bg-accent-tint"
	}
	return 0, ""
}

// luatMau là luật dò bằng regex trên cả file (không theo token). Nhóm 1 là
// đoạn bị bắt.
type luatMau struct {
	so    int
	re    *regexp.Regexp
	duocO []string // nơi được phép (rỗng: không nơi nào)
	msg   string
}

var luatMauList = []luatMau{
	// Luật 3: màu chỉ khai ở styles.css. #abc hoặc #aabbcc, sau đó phải hết từ
	// và không phải dấu `-`: `#phien-ban` không bắt đầu bằng hex; `#add-site`
	// (neo trang) không bị nhận nhầm. Đứng sau `&` là thực thể HTML (`&#123;`).
	{3, regexp.MustCompile(`(?:^|[^&])(#(?:[0-9a-fA-F]{6}|[0-9a-fA-F]{3}))(?:[^-\w]|$)`), nil,
		"màu viết thẳng: màu chỉ khai ở styles.css, dùng class (bg-accent, text-muted...) hoặc var(--tên)"},
	{3, regexp.MustCompile(`\b((?:rgba?|hsla?)\()`), nil,
		"màu viết thẳng: màu chỉ khai ở styles.css, dùng class hoặc var(--tên)"},
	// Luật 6: chữ hoa tự động.
	{6, regexp.MustCompile(`(\.toUpperCase\()`), noiToUpperCase,
		"nhãn viết thường, không đổi sang chữ hoa bằng mã: .toUpperCase() chỉ ở components/Avatar.tsx (chữ cái đầu tên)"},
	// Luật 7: lỗ XSS và gọi ra ngoài origin (CSP của panel là default-src 'self').
	{7, regexp.MustCompile(`(dangerouslySetInnerHTML)`), nil,
		"chèn HTML thô mở lỗ XSS: để React tự thoát chuỗi"},
	{7, regexp.MustCompile(`(\.innerHTML\b)`), nil,
		"chèn HTML thô mở lỗ XSS: dùng textContent hoặc JSX"},
	{7, regexp.MustCompile(`(\beval\()`), nil,
		"eval chạy chuỗi như mã, CSP cũng chặn: không dùng"},
	{7, regexp.MustCompile(`(\bnew\s+Function\()`), nil,
		"new Function chạy chuỗi như mã, CSP cũng chặn: không dùng"},
	{7, regexp.MustCompile(`(https?://)`), nil,
		"địa chỉ tuyệt đối trong mã: panel chỉ gọi cùng origin (đường dẫn /api/...), CSP chặn nguồn ngoài"},
}

const (
	tepDropdown = "src/components/Menu.tsx"
	thuVienMenu = "@radix-ui/react-dropdown-menu"
)

// kiemFile áp mọi luật 1 đến 8 lên một file nguồn (rel dạng "src/...").
// Đây là hàm các test bảng gọi: chuỗi vào, danh sách vi phạm ra.
func kiemFile(rel, raw string) []viPham {
	code := boChuThich(raw)
	dong := func(idx int) int { return 1 + strings.Count(code[:idx], "\n") }
	var out []viPham

	for _, m := range reToken.FindAllStringSubmatchIndex(code, -1) {
		tok := code[m[2]:m[3]]
		if so, msg := xetToken(rel, tok); so != 0 {
			out = append(out, viPham{so, dong(m[2]), tok, msg})
		}
	}
	for _, l := range luatMauList {
		if duoiTienTo(rel, l.duocO) {
			continue
		}
		for _, m := range l.re.FindAllStringSubmatchIndex(code, -1) {
			out = append(out, viPham{l.so, dong(m[2]), code[m[2]:m[3]], l.msg})
		}
	}

	// Luật 8: menu thả chỉ qua components/Menu.tsx, và ở đó phải có modal={false}
	// (Radix modal khoá cuộn và chặn con trỏ trên cả trang; menu của panel không cần).
	if rel != tepDropdown {
		if i := strings.Index(code, thuVienMenu); i >= 0 {
			out = append(out, viPham{8, dong(i), thuVienMenu,
				"mọi menu thả đi qua src/components/Menu.tsx: import Menu từ đó, không import Radix trực tiếp"})
		}
	} else if !strings.Contains(code, "modal={false}") {
		out = append(out, viPham{8, 1, tepDropdown,
			"Menu.tsx phải đặt modal={false} cho DropdownMenu.Root"})
	}

	slices.SortStableFunc(out, func(a, b viPham) int { return a.dong - b.dong })
	return out
}

// kiemNguonNgoai kiểm luật 7 cho hai nguồn ngoài web/src (không có chú thích
// để bỏ theo kiểu TS, nên đọc nguyên văn): web/index.html không được có địa
// chỉ tuyệt đối; web/public/* chỉ được có không gian tên SVG http://www.w3.org/.
func kiemNguonNgoai(rel, raw string) []viPham {
	const khongGianTen = "http://www.w3.org/"
	text := raw
	if strings.HasPrefix(rel, "public/") {
		text = strings.ReplaceAll(raw, khongGianTen, "")
	}
	var out []viPham
	for _, m := range regexp.MustCompile(`https?://`).FindAllStringIndex(text, -1) {
		out = append(out, viPham{7, 1 + strings.Count(text[:m[0]], "\n"), text[m[0]:m[1]],
			"địa chỉ tuyệt đối: index.html và public/ chỉ dùng đường dẫn cùng origin (public/ chỉ được có http://www.w3.org/ của SVG)"})
	}
	return out
}

// ---------- Duyệt cây file ----------

// quetKieuDang duyệt web/src (cộng index.html và public/) trong fsys, trả về
// các dòng lỗi. thu rỗng: chạy đầy đủ, luật 1-6 bỏ qua thư mục trong danh sách
// chua, rồi báo thư mục đã sạch. thu khác rỗng: chỉ file dưới các tiền tố đó,
// đủ luật 1-8, bỏ qua danh sách chua và không báo "đã đổi xong".
func quetKieuDang(fsys fs.FS, thu, chua []string) []string {
	var loi []string
	lenhThu := len(thu) > 0
	coViPham := map[string]bool{} // tiền tố trong chua -> còn vi phạm luật 1-6
	soFile := 0

	xet := func(rel string, vs []viPham) {
		soFile++
		for _, v := range vs {
			if v.luat <= 6 && !lenhThu && duoiTienTo(rel, chua) {
				for _, p := range chua {
					if strings.HasPrefix(rel, p) {
						coViPham[p] = true
					}
				}
				continue
			}
			loi = append(loi, fmt.Sprintf("%s:%d: luật %d: %q: %s", rel, v.dong, v.luat, v.mau, v.msg))
		}
	}
	docFile := func(rel string) (string, bool) {
		b, err := fs.ReadFile(fsys, rel)
		if err != nil {
			loi = append(loi, fmt.Sprintf("%s: không đọc được: %v", rel, err))
			return "", false
		}
		return string(b), true
	}
	trongPhamVi := func(rel string) bool { return !lenhThu || duoiTienTo(rel, thu) }

	err := fs.WalkDir(fsys, "src", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !trongPhamVi(p) {
			return nil
		}
		ext := path.Ext(p)
		if (ext != ".ts" && ext != ".tsx") || strings.HasSuffix(p, ".test.ts") {
			return nil
		}
		if raw, ok := docFile(p); ok {
			xet(p, kiemFile(p, raw))
		}
		return nil
	})
	if err != nil {
		loi = append(loi, fmt.Sprintf("duyệt src: %v", err))
	}

	if trongPhamVi("index.html") {
		if raw, ok := docFile("index.html"); ok {
			xet("index.html", kiemNguonNgoai("index.html", raw))
		}
	}
	_ = fs.WalkDir(fsys, "public", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !trongPhamVi(p) {
			return nil
		}
		if raw, ok := docFile(p); ok {
			xet(p, kiemNguonNgoai(p, raw))
		}
		return nil
	})

	if lenhThu {
		if soFile == 0 {
			loi = append(loi, fmt.Sprintf("%s=%q không khớp file nào: tiền tố tính từ web/, ví dụ src/screens/site/", bienMoiTruongThu, strings.Join(thu, ",")))
		}
		return loi
	}
	for _, p := range chua {
		if !coViPham[p] {
			loi = append(loi, fmt.Sprintf("đã đổi xong %s: gỡ khỏi danh sách chuaDoi trong web/kieu_dang_test.go", p))
		}
	}
	return loi
}

// docThu đọc KIEU_DANG_THU: danh sách tiền tố cách nhau bằng dấu phẩy.
func docThu() []string {
	var thu []string
	for _, p := range strings.Split(os.Getenv(bienMoiTruongThu), ",") {
		if p = strings.TrimSpace(p); p != "" {
			thu = append(thu, p)
		}
	}
	return thu
}

// TestKieuDang: B6 (luật 1-6), B7 (luật 7), B8 (luật 8).
func TestKieuDang(t *testing.T) {
	for _, l := range quetKieuDang(os.DirFS("."), docThu(), chuaDoi) {
		t.Error(l)
	}
}

// ---------- B11: nhãn không viết hoa cả câu ----------

type chuoiTS struct {
	dong int
	nd   string
}

var thoatChuoi = strings.NewReplacer(`\"`, `"`, `\'`, `'`, `\\`, `\`, `\n`, " ")

// rutChuoi lấy mọi chuỗi nháy kép, nháy đơn và mọi đoạn chữ của template
// literal (phần giữa các `${...}`) trong mã TypeScript. Có đi qua chú thích để
// dấu nháy trong chú thích không làm lệch trạng thái, và hiểu template lồng
// nhau trong `${...}`.
func rutChuoi(src string) []chuoiTS {
	var out []chuoiTS
	dong := func(i int) int { return 1 + strings.Count(src[:i], "\n") }
	var ma func(i int, trongBieuThuc bool) int
	var template func(i int) int

	// ma quét mã thường từ i. trongBieuThuc: đang ở trong `${ ... }`, trả về
	// vị trí sau dấu } đóng.
	ma = func(i int, trongBieuThuc bool) int {
		sau := 0
		for i < len(src) {
			c := src[i]
			switch {
			case strings.HasPrefix(src[i:], "//"):
				for i < len(src) && src[i] != '\n' {
					i++
				}
			case strings.HasPrefix(src[i:], "/*"):
				end := strings.Index(src[i+2:], "*/")
				if end < 0 {
					return len(src)
				}
				i += 2 + end + 2
			case c == '"' || c == '\'':
				j := i + 1
				for j < len(src) && src[j] != c && src[j] != '\n' {
					if src[j] == '\\' {
						j++
					}
					j++
				}
				if j > len(src) {
					j = len(src)
				}
				out = append(out, chuoiTS{dong(i), thoatChuoi.Replace(src[i+1 : j])})
				i = j + 1
			case c == '`':
				i = template(i + 1)
			case c == '{':
				sau++
				i++
			case c == '}':
				if trongBieuThuc && sau == 0 {
					return i + 1
				}
				sau--
				i++
			default:
				i++
			}
		}
		return i
	}
	// template quét thân template literal từ i (sau dấu ` mở), trả về vị trí
	// sau dấu ` đóng.
	template = func(i int) int {
		var seg strings.Builder
		bat := i
		xa := func() {
			if seg.Len() > 0 {
				out = append(out, chuoiTS{dong(bat), thoatChuoi.Replace(seg.String())})
				seg.Reset()
			}
		}
		for i < len(src) {
			switch {
			case src[i] == '\\' && i+1 < len(src):
				seg.WriteByte(src[i+1])
				i += 2
			case src[i] == '`':
				xa()
				return i + 1
			case strings.HasPrefix(src[i:], "${"):
				xa()
				i = ma(i+2, true)
				bat = i
			default:
				seg.WriteByte(src[i])
				i++
			}
		}
		xa()
		return i
	}
	ma(0, false)
	return out
}

// vietHoaCaCau: chuỗi không có chữ thường nào và có ít nhất hai "từ" toàn chữ
// hoa, mỗi từ từ hai chữ cái trở lên (từ = dãy chữ cái unicode liền nhau).
// "BẰNG CHỨNG", "MÃ JOB", "AI ĐỀ XUẤT" bị bắt; "PHP 8.3", "TPAN", "HTTP 502"
// chỉ có một từ nên được tha; "LEMP · nginx" có chữ thường nên được tha.
func vietHoaCaCau(s string) bool {
	tu, soChu := 0, 0
	hoa := true
	xa := func() {
		if soChu >= 2 && hoa {
			tu++
		}
		soChu, hoa = 0, true
	}
	for _, r := range s {
		if unicode.IsLower(r) {
			return false
		}
		if unicode.IsLetter(r) {
			soChu++
			hoa = hoa && unicode.IsUpper(r)
		} else {
			xa()
		}
	}
	xa()
	return tu >= 2
}

// quetNhan duyệt src/i18n/*.ts (trừ index.tsx, vốn không khớp *.ts) và trả về
// các dòng lỗi. Cùng cơ chế với quetKieuDang: thu và chua (theo file).
func quetNhan(fsys fs.FS, thu, chua []string, choPhep map[string]string) []string {
	var loi []string
	lenhThu := len(thu) > 0
	coViPham := map[string]bool{}
	files, err := fs.Glob(fsys, "src/i18n/*.ts")
	if err != nil {
		return []string{fmt.Sprintf("tìm src/i18n/*.ts: %v", err)}
	}
	for _, p := range files {
		if strings.HasSuffix(p, ".test.ts") || (lenhThu && !duoiTienTo(p, thu)) {
			continue
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			loi = append(loi, fmt.Sprintf("%s: không đọc được: %v", p, err))
			continue
		}
		trongDS := !lenhThu && slices.Contains(chua, p)
		for _, c := range rutChuoi(string(b)) {
			if _, ok := choPhep[c.nd]; ok || !vietHoaCaCau(c.nd) {
				continue
			}
			if trongDS {
				coViPham[p] = true
				continue
			}
			loi = append(loi, fmt.Sprintf("%s:%d: %q viết hoa cả câu: nhãn viết thường, chỉ hoa chữ đầu (ví dụ \"Bằng chứng\"); nếu bắt buộc thì thêm vào choPhepVietHoa kèm lý do", p, c.dong, c.nd))
		}
	}
	if !lenhThu {
		for _, p := range chua {
			if !coViPham[p] {
				loi = append(loi, fmt.Sprintf("đã đổi xong %s: gỡ khỏi danh sách chuaDoiNhan trong web/kieu_dang_test.go", p))
			}
		}
	}
	return loi
}

// TestNhanKhongVietHoa: B11.
func TestNhanKhongVietHoa(t *testing.T) {
	for _, l := range quetNhan(os.DirFS("."), docThu(), chuaDoiNhan, choPhepVietHoa) {
		t.Error(l)
	}
}

// ---------- Test bảng cho các hàm so mẫu (tên có "Mau" để chạy riêng: -run Mau) ----------

func soLuat(vs []viPham) []int {
	var out []int
	for _, v := range vs {
		out = append(out, v.luat)
	}
	slices.Sort(out)
	return out
}

func TestMauBoChuThich(t *testing.T) {
	bang := []struct{ ten, vao, ra string }{
		{"chú thích dòng", "a // b\nc", "a \nc"},
		{"link trong chuỗi không bị cắt", `x = "https://x" // ghi chú`, `x = "https://x" `},
		{"link sau dấu hai chấm giữ nguyên", "https://x/y", "https://x/y"},
		{"chú thích khối giữ số dòng", "a /* b\nc\nd */ e", "a \n\n e"},
		{"chú thích khối trong JSX", "<div>{/* uppercase */}</div>", "<div>{}</div>"},
		{"/* trong chú thích dòng không mở khối", "a // x /* y\nb", "a \nb"},
		{"khối không đóng cắt tới hết file", "a /* b", "a "},
		{"không có chú thích", "text-[13px]", "text-[13px]"},
	}
	for _, c := range bang {
		t.Run(c.ten, func(t *testing.T) {
			if got := boChuThich(c.vao); got != c.ra {
				t.Errorf("boChuThich(%q) = %q, muốn %q", c.vao, got, c.ra)
			}
		})
	}
}

// Mỗi dòng: mã vào (đã đặt trong một file có đường dẫn rel) -> các số luật bị
// bắt. Phần quan trọng là những dòng muốn "không bắt": chứng minh mẫu không
// quá tay.
func TestMauKieuDang(t *testing.T) {
	const man = "src/screens/site/Other.tsx"
	bang := []struct {
		ten  string
		rel  string
		code string
		want []int
	}{
		// Luật 1
		{"12,5px được phép", man, `<p className="text-[12.5px]">`, nil},
		{"13.0px bằng 13 được phép", man, `"text-[13.0px]"`, nil},
		{"24px được phép với biến thể", man, `"md:text-[24px]"`, nil},
		{"11px bị bắt", man, `"text-[11px]"`, []int{1}},
		{"15px bị bắt", man, `"text-[15px]"`, []int{1}},
		{"text-sm bị bắt", man, `"text-sm"`, []int{1}},
		{"text-xl với biến thể bị bắt", man, `"md:text-xl"`, []int{1}},
		{"text-2xl bị bắt", man, "`text-2xl`", []int{1}},
		{"text-sm/6 bị bắt", man, `"text-sm/6"`, []int{1}},
		{"text-muted không phải cỡ", man, `"text-muted text-ink"`, nil},
		{"text-xlarge không phải cỡ", man, `"text-xlarge"`, nil},
		{"text-[length:12px] không phải cỡ px", man, `"text-[length:12px]"`, nil},
		// Luật 2
		{"shadow-pop được phép", man, `"shadow-pop"`, nil},
		{"shadow-none được phép", man, `"hover:shadow-none"`, nil},
		{"shadow-md bị bắt", man, `"shadow-md"`, []int{2}},
		{"shadow đứng riêng bị bắt", man, `"rounded-card shadow"`, []int{2}},
		{"boxShadow không phải class", man, `style={{ boxShadow: x }}`, nil},
		{"uppercase bị bắt", man, `"text-[12px] uppercase"`, []int{2}},
		{"uppercase với biến thể bị bắt", man, `"hover:uppercase"`, []int{2}},
		{"uppercase trong chú thích dòng không bị bắt", man, "// uppercase cũ, đã bỏ\nx", nil},
		{"uppercase trong chú thích khối không bị bắt", man, "/* uppercase tracking-[0.08em] */ x", nil},
		{"uppercased không phải class", man, `"uppercased"`, nil},
		{"rounded đứng riêng bị bắt", man, `"rounded border-hair"`, []int{2}},
		{"rounded-lg bị bắt", man, `"rounded-lg"`, []int{2}},
		{"rounded-[6px] bị bắt", man, `"rounded-[6px]"`, []int{2}},
		{"rounded-card được phép", man, `"rounded-card rounded-ctl rounded-item rounded-full"`, nil},
		{"tracking-[0.08em] bị bắt", man, `"tracking-[0.08em]"`, []int{2}},
		{"tracking-[0.06em] bị bắt", man, `"tracking-[0.06em]"`, []int{2}},
		{"tracking-tight không bị bắt", man, `"tracking-tight"`, nil},
		{"border-ink bị bắt", man, `"border border-ink"`, []int{2}},
		{"border-card-border bị bắt", man, `"border-card-border"`, []int{2}},
		{"border-hair được phép", man, `"border-hair"`, nil},
		{"border-line-dash bị bắt ở mọi nơi", "src/components/A.tsx", `"border-line-dash"`, []int{2}},
		{"border-line ngoài components bị bắt", man, `"border-line"`, []int{2}},
		{"border-line trong components được phép", "src/components/Button.tsx", `"border border-line"`, nil},
		{"border-line trong Layout được phép", "src/Layout.tsx", `"border-line"`, nil},
		{"border-line-dash không bị tính là border-line", "src/components/A.tsx", `"border-line-dash"`, []int{2}},
		{"border-route-dash được phép", man, `"border-route-dash border-route"`, nil},
		// Luật 3
		{"#phien-ban không phải mã màu", man, `href="#phien-ban"`, nil},
		{"#add-site không phải mã màu", man, `href="#add-site"`, nil},
		{"thực thể &#123; không phải mã màu", man, `<span>&#123;</span>`, nil},
		{"#fff bị bắt", man, `color: "#fff"`, []int{3}},
		{"#EEF1F4 bị bắt", man, `"#EEF1F4"`, []int{3}},
		{"#12345 (5 số) không phải mã màu", man, `"#12345"`, nil},
		{"#1234567 (7 số) không phải mã màu", man, `"#1234567"`, nil},
		{"rgb( bị bắt", man, `"rgb(1 2 3)"`, []int{3}},
		{"rgba( bị bắt", man, `"rgba(0,0,0,.5)"`, []int{3}},
		{"hsl( bị bắt", man, `"hsl(10 20% 30%)"`, []int{3}},
		{"hsla( bị bắt", man, `"hsla(10,20%,30%,.5)"`, []int{3}},
		{"color-mix in srgb không bị bắt", man, `"color-mix(in srgb, red 10%, blue)"`, nil},
		{"var(--ink) không bị bắt", man, `"var(--ink)"`, nil},
		{"mã màu trong chú thích không bị bắt", man, "// #FF0000 cũ\nx", nil},
		// Luật 4
		{"font-mono ngoài chỗ cho phép bị bắt", man, `"font-mono"`, []int{4}},
		{"font-mono trong components được phép", "src/components/Code.tsx", `"font-mono"`, nil},
		{"font-mono trong Layout được phép", "src/Layout.tsx", `"font-mono"`, nil},
		{"font-mono trong LiveLog được phép", "src/screens/site/LiveLog.tsx", `"font-mono"`, nil},
		{"font-mono trong AnswerText được phép", "src/ai/AnswerText.tsx", `"font-mono"`, nil},
		{"font-mono trong duyet-plan/index được phép", "src/screens/duyet-plan/index.tsx", `"font-mono"`, nil},
		{"font-mono ở file khác của duyet-plan bị bắt", "src/screens/duyet-plan/Other.tsx", `"font-mono"`, []int{4}},
		{"font-mono với biến thể bị bắt", man, `"md:font-mono"`, []int{4}},
		{"font-monospace không phải font-mono", man, `"font-monospace"`, nil},
		// Luật 5
		{"bg-ink ngoài chỗ cho phép bị bắt", man, `"bg-ink text-on-ink"`, []int{5}},
		{"bg-ink trong Layout được phép", "src/Layout.tsx", `"bg-ink"`, nil},
		{"bg-ink trong MetricBar được phép", "src/components/MetricBar.tsx", `"bg-ink"`, nil},
		{"bg-ink trong UsageMeter được phép", "src/components/UsageMeter.tsx", `"bg-ink"`, nil},
		{"bg-ink trong Card bị bắt", "src/components/Card.tsx", `"bg-ink"`, []int{5}},
		{"bg-ink-soft không phải bg-ink", man, `"bg-ink-soft"`, nil},
		// Luật 6
		{".toUpperCase() ngoài Avatar bị bắt", man, `name.toUpperCase()`, []int{6}},
		{".toUpperCase() trong Avatar được phép", "src/components/Avatar.tsx", `name[0].toUpperCase()`, nil},
		{"toUpperCase trong chú thích không bị bắt", man, "// không dùng .toUpperCase()\nx", nil},
		{"toLowerCase không bị bắt", man, `name.toLowerCase()`, nil},
		// Luật 7
		{"dangerouslySetInnerHTML bị bắt", man, `<div dangerouslySetInnerHTML={{ __html: x }} />`, []int{7}},
		{".innerHTML bị bắt", man, `el.innerHTML = x`, []int{7}},
		{"eval( bị bắt", man, `eval(x)`, []int{7}},
		{"retrieval( không phải eval(", man, `retrieval(x)`, nil},
		{"new Function( bị bắt", man, `new Function("a", "return a")`, []int{7}},
		{"chuỗi https:// trong mã bị bắt", man, `fetch("https://x/y")`, []int{7}},
		{"chuỗi http:// trong mã bị bắt", man, `const u = "http://x"`, []int{7}},
		{"https:// trong chú thích không bị bắt", man, "// xem https://x/y\nx", nil},
		{"https:// trong chuỗi rồi chú thích: chỉ bắt một lần", man, `const u = "https://x" // ghi chú https://y`, []int{7}},
		{"fetch cùng origin không bị bắt", "src/api/client.ts", `fetch("/api/v1/x", { credentials: "same-origin" })`, nil},
		{"new EventSource cùng origin không bị bắt", "src/api/client.ts", `new EventSource("/api/v1/jobs/1/stream")`, nil},
		{"luật 7 bắt cả ở thư mục components", "src/components/A.tsx", `el.innerHTML = x`, []int{7}},
		// Luật 8
		{"import Radix dropdown ngoài Menu bị bắt", man, `import * as D from "@radix-ui/react-dropdown-menu";`, []int{8}},
		{"nhắc Radix dropdown trong chú thích không bị bắt", man, "// dùng @radix-ui/react-dropdown-menu qua Menu\nx", nil},
		{"Menu.tsx có modal={false} đạt", tepDropdown, `import * as D from "@radix-ui/react-dropdown-menu"; <D.Root modal={false}>`, nil},
		{"Menu.tsx thiếu modal={false} bị bắt", tepDropdown, `import * as D from "@radix-ui/react-dropdown-menu"; <D.Root>`, []int{8}},
		{"modal={false} chỉ nằm trong chú thích vẫn thiếu", tepDropdown, "// modal={false}\n<D.Root>", []int{8}},
		{"import Radix dialog không bị bắt", man, `import * as D from "@radix-ui/react-dialog";`, nil},
		// Nhiều luật cùng lúc
		{"nhiều luật trên một dòng", man, `"text-[11px] uppercase font-mono"`, []int{1, 2, 4}},
		{"class đứng sau backtick và dấu hai chấm", man, "`${a ? \"x\" : \"uppercase\"} md:rounded-lg`", []int{2, 2}},
	}
	for _, c := range bang {
		t.Run(c.ten, func(t *testing.T) {
			got := soLuat(kiemFile(c.rel, c.code))
			if !slices.Equal(got, c.want) {
				t.Errorf("kiemFile(%q, %q): luật bị bắt = %v, muốn %v", c.rel, c.code, got, c.want)
			}
		})
	}
}

func TestMauSoDong(t *testing.T) {
	vs := kiemFile("src/screens/x.tsx", "a\n/* hai\ndòng */\n\"uppercase\"\n")
	if len(vs) != 1 || vs[0].dong != 4 {
		t.Errorf("uppercase nằm ở dòng 4 (chú thích khối nhiều dòng phải giữ số dòng), được %+v", vs)
	}
}

func TestMauNguonNgoai(t *testing.T) {
	bang := []struct {
		ten, rel, raw string
		want          int
	}{
		{"index.html sạch", "index.html", `<script src="/theme.js"></script>`, 0},
		{"index.html có https", "index.html", `<link href="https://fonts.example.com/x">`, 1},
		{"index.html có http của SVG vẫn bị bắt", "index.html", `<svg xmlns="http://www.w3.org/2000/svg">`, 1},
		{"public/ svg có không gian tên w3", "public/tpan.svg", `<svg xmlns="http://www.w3.org/2000/svg">`, 0},
		{"public/ có http khác", "public/a.js", `fetch("http://example.com")`, 1},
		{"public/ có https", "public/a.js", `x("https://example.com")`, 1},
		{"public/ có https dù đi kèm w3", "public/a.svg", "http://www.w3.org/2000/svg\nhttps://www.w3.org/x", 1},
	}
	for _, c := range bang {
		t.Run(c.ten, func(t *testing.T) {
			if got := len(kiemNguonNgoai(c.rel, c.raw)); got != c.want {
				t.Errorf("kiemNguonNgoai(%q, %q) = %d vi phạm, muốn %d", c.rel, c.raw, got, c.want)
			}
		})
	}
}

// Danh sách "chưa đổi": chỉ ngắn đi. Chạy trên cây file giả (MapFS).
func TestMauChuaDoi(t *testing.T) {
	sach := `export const A = () => <div className="rounded-card text-[13px]" />;`
	ban := `export const A = () => <div className="uppercase" />;`
	cay := fstest.MapFS{
		"src/screens/moi/index.tsx": {Data: []byte(sach)},
		"src/screens/cu/index.tsx":  {Data: []byte(ban)},
		"src/screens/cu/x.test.ts":  {Data: []byte(ban)}, // *.test.ts không bị quét
		"src/screens/da-sach/a.tsx": {Data: []byte(sach)},
		"index.html":                {Data: []byte(`<html></html>`)},
		"public/tpan.svg":           {Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)},
	}
	t.Run("thư mục trong danh sách và còn vi phạm: im lặng", func(t *testing.T) {
		got := quetKieuDang(cay, nil, []string{"src/screens/cu/"})
		if len(got) != 0 {
			t.Errorf("muốn không lỗi, được %q", got)
		}
	})
	t.Run("thư mục trong danh sách mà đã sạch: báo đã đổi xong", func(t *testing.T) {
		got := quetKieuDang(cay, nil, []string{"src/screens/cu/", "src/screens/da-sach/"})
		if len(got) != 1 || !strings.Contains(got[0], "đã đổi xong src/screens/da-sach/: gỡ khỏi danh sách chuaDoi") {
			t.Errorf("được %q", got)
		}
	})
	t.Run("thư mục ngoài danh sách có vi phạm: báo file và dòng", func(t *testing.T) {
		got := quetKieuDang(cay, nil, nil)
		if len(got) != 1 || !strings.HasPrefix(got[0], "src/screens/cu/index.tsx:1: luật 2:") {
			t.Errorf("được %q", got)
		}
	})
	t.Run("thư mục trong danh sách không còn tồn tại: báo đã đổi xong", func(t *testing.T) {
		got := quetKieuDang(cay, nil, []string{"src/screens/cu/", "src/screens/bien-mat/"})
		if len(got) != 1 || !strings.Contains(got[0], "src/screens/bien-mat/") {
			t.Errorf("được %q", got)
		}
	})
	t.Run("KIEU_DANG_THU: chỉ kiểm thư mục đó, đủ luật, bỏ qua danh sách", func(t *testing.T) {
		got := quetKieuDang(cay, []string{"src/screens/cu/"}, []string{"src/screens/cu/", "src/screens/da-sach/"})
		if len(got) != 1 || !strings.HasPrefix(got[0], "src/screens/cu/index.tsx:1: luật 2:") {
			t.Errorf("được %q", got)
		}
		got = quetKieuDang(cay, []string{"src/screens/moi/"}, []string{"src/screens/da-sach/"})
		if len(got) != 0 {
			t.Errorf("thư mục sạch phải im lặng và không báo đã đổi xong, được %q", got)
		}
	})
	t.Run("KIEU_DANG_THU gõ sai tiền tố: báo không khớp", func(t *testing.T) {
		got := quetKieuDang(cay, []string{"src/screens/khong-co/"}, nil)
		if len(got) != 1 || !strings.Contains(got[0], "không khớp file nào") {
			t.Errorf("được %q", got)
		}
	})
	t.Run("luật 7 vẫn kiểm file trong danh sách chưa đổi", func(t *testing.T) {
		c := fstest.MapFS{
			"src/screens/cu/a.tsx": {Data: []byte(`el.innerHTML = x; "uppercase"`)},
			"index.html":           {Data: []byte(`<html></html>`)},
		}
		got := quetKieuDang(c, nil, []string{"src/screens/cu/"})
		if len(got) != 1 || !strings.Contains(got[0], "luật 7") {
			t.Errorf("được %q", got)
		}
	})
	t.Run("index.html và public/ được kiểm", func(t *testing.T) {
		c := fstest.MapFS{
			"index.html":      {Data: []byte(`<a href="https://x">`)},
			"public/tpan.svg": {Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)},
			"public/a.js":     {Data: []byte(`x("http://y")`)},
			"src/a.tsx":       {Data: []byte(`x`)},
		}
		got := quetKieuDang(c, nil, nil)
		if len(got) != 2 || !strings.HasPrefix(got[0], "index.html:1: luật 7") || !strings.HasPrefix(got[1], "public/a.js:1: luật 7") {
			t.Errorf("được %q", got)
		}
	})
}

func TestMauDocThu(t *testing.T) {
	bang := []struct {
		env  string
		want []string
	}{
		{"", nil},
		{"src/screens/site/", []string{"src/screens/site/"}},
		{" src/a/ , src/b.tsx,, ", []string{"src/a/", "src/b.tsx"}},
	}
	for _, c := range bang {
		t.Setenv(bienMoiTruongThu, c.env)
		if got := docThu(); !slices.Equal(got, c.want) {
			t.Errorf("%s=%q: docThu() = %q, muốn %q", bienMoiTruongThu, c.env, got, c.want)
		}
	}
}

func TestMauNhanVietHoa(t *testing.T) {
	bang := []struct {
		s    string
		want bool
	}{
		{"BẰNG CHỨNG", true},
		{"MÃ JOB", true},
		{"AI ĐỀ XUẤT", true},
		{"COMMANDS THAT WILL RUN", true},
		{"SỬA TAY", true},
		{"PHP 8.3", false},
		{"TPAN", false},
		{"SSL", false},
		{"LEMP · nginx", false},
		{"HTTP 502", false},
		{"Bằng chứng", false},
		{"bằng chứng", false},
		{"", false},
		{"123 456", false},
		{"A B", false},                // hai từ nhưng mỗi từ một chữ cái
		{"MÃ JOB {n} mục", false},     // có chữ thường
		{"TPAN · SSL", true},          // hai từ toàn hoa
		{"PHP-FPM", true},             // gạch nối tách thành hai từ
		{"CHỈ MỘT chữ thường", false}, // có chữ thường
	}
	for _, c := range bang {
		t.Run(c.s, func(t *testing.T) {
			if got := vietHoaCaCau(c.s); got != c.want {
				t.Errorf("vietHoaCaCau(%q) = %v, muốn %v", c.s, got, c.want)
			}
		})
	}
}

func TestMauRutChuoi(t *testing.T) {
	bang := []struct {
		ten, src string
		want     []string
	}{
		{"nháy kép", `a: "BẰNG CHỨNG", b: "x"`, []string{"BẰNG CHỨNG", "x"}},
		{"nháy kép có \\\"", `a: "nói \"MÃ JOB\" đi"`, []string{`nói "MÃ JOB" đi`}},
		{"nháy đơn", `a: 'AI ĐỀ XUẤT'`, []string{"AI ĐỀ XUẤT"}},
		{"nháy trong chú thích dòng bị bỏ qua", "// it's \"X\"\na: \"y\"", []string{"y"}},
		{"nháy trong chú thích khối bị bỏ qua", "/* \"X\" */ a: \"y\"", []string{"y"}},
		{"template không có biểu thức", "a: `MÃ JOB`", []string{"MÃ JOB"}},
		{"template tách ở ${}", "a: `HAI ${n} CÂU và ${m}`", []string{"HAI ", " CÂU và "}},
		{"template lồng trong biểu thức", "a: `x ${ y ? `LỒNG NHAU` : \"z\" } t`", []string{"x ", "LỒNG NHAU", "z", " t"}},
		{"object trong ${}", "a: `p ${ f({ k: \"v\" }) } q`", []string{"p ", "v", " q"}},
		{"backtick thoát", "a: `a \\` b`", []string{"a ` b"}},
	}
	for _, c := range bang {
		t.Run(c.ten, func(t *testing.T) {
			var got []string
			for _, s := range rutChuoi(c.src) {
				got = append(got, s.nd)
			}
			if !slices.Equal(got, c.want) {
				t.Errorf("rutChuoi(%q) = %q, muốn %q", c.src, got, c.want)
			}
		})
	}
	if got := rutChuoi("a\n\nb: \"x\""); len(got) != 1 || got[0].dong != 3 {
		t.Errorf("số dòng sai: %+v", got)
	}
}

func TestMauChuaDoiNhan(t *testing.T) {
	cay := fstest.MapFS{
		"src/i18n/moi.ts":    {Data: []byte(`export const m = { a: "Bằng chứng", b: "PHP 8.3" };`)},
		"src/i18n/cu.ts":     {Data: []byte("export const m = {\n a: \"BẰNG CHỨNG\",\n};")},
		"src/i18n/xong.ts":   {Data: []byte(`export const m = { a: "Bằng chứng" };`)},
		"src/i18n/ngoai.ts":  {Data: []byte("export const m = {\n a: `AI ${n} đề xuất`,\n b: \"MÃ JOB\",\n};")},
		"src/i18n/index.tsx": {Data: []byte(`"BẰNG CHỨNG"`)}, // không bị đọc
	}
	t.Run("file trong danh sách còn viết hoa: im lặng", func(t *testing.T) {
		got := quetNhan(cay, nil, []string{"src/i18n/cu.ts", "src/i18n/ngoai.ts"}, nil)
		if len(got) != 0 {
			t.Errorf("được %q", got)
		}
	})
	t.Run("file trong danh sách mà đã sạch: báo đã đổi xong", func(t *testing.T) {
		got := quetNhan(cay, nil, []string{"src/i18n/cu.ts", "src/i18n/ngoai.ts", "src/i18n/xong.ts"}, nil)
		if len(got) != 1 || !strings.Contains(got[0], "đã đổi xong src/i18n/xong.ts: gỡ khỏi danh sách chuaDoiNhan") {
			t.Errorf("được %q", got)
		}
	})
	t.Run("file ngoài danh sách báo từng chuỗi và dòng", func(t *testing.T) {
		got := quetNhan(cay, nil, []string{"src/i18n/cu.ts"}, nil)
		if len(got) != 1 || !strings.HasPrefix(got[0], `src/i18n/ngoai.ts:3: "MÃ JOB"`) {
			// Template "AI ${n} đề xuất" tách thành "AI " và " đề xuất": đoạn đầu
			// chỉ một từ hoa, đoạn sau có chữ thường; chỉ "MÃ JOB" bị bắt.
			t.Errorf("được %q", got)
		}
	})
	t.Run("danh sách cho phép", func(t *testing.T) {
		got := quetNhan(cay, nil, []string{"src/i18n/cu.ts"}, map[string]string{"MÃ JOB": "tên cột phải đúng như lệnh"})
		if len(got) != 0 {
			t.Errorf("được %q", got)
		}
	})
	t.Run("KIEU_DANG_THU", func(t *testing.T) {
		got := quetNhan(cay, []string{"src/i18n/cu.ts"}, []string{"src/i18n/cu.ts", "src/i18n/xong.ts"}, nil)
		if len(got) != 1 || !strings.HasPrefix(got[0], `src/i18n/cu.ts:2: "BẰNG CHỨNG"`) {
			t.Errorf("được %q", got)
		}
	})
}
