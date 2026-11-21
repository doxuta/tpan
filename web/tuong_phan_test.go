package web

import (
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Bất biến B3, B4, B5 của docs/thay-doi/M16-thiet-ke.md (mục 4): bảng màu của
// giao diện nằm ở web/src/styles.css, hai khối biến (sáng và tối). Test đọc
// thẳng file đó, nên đổi màu mà làm tụt tương phản (hay quên đổi bên kia) thì
// `go test ./web/` đỏ ngay, không cần mở trình duyệt.
//
// Chỉ dùng thư viện chuẩn: tính độ chói theo công thức WCAG 2.1 chỉ vài dòng.

const tepStyles = "src/styles.css"

// Hai chế độ màu. Tên dùng trong thông báo lỗi.
const (
	cheDoSang = "sáng"
	cheDoToi  = "tối"
)

// Khối sáng mở bằng `:root,` rồi `[data-theme="light"] {` (chế độ sáng cũng là
// mặc định khi chưa có data-theme); khối tối mở bằng `[data-theme="dark"] {`
// đứng riêng một selector. Selector ghép như `[data-theme="dark"] .x {` không
// khớp vì sau `]` phải là `{`.
var (
	moKhoiSang = regexp.MustCompile(`:root,\s*\[data-theme="light"\]\s*\{`)
	moKhoiToi  = regexp.MustCompile(`\[data-theme="dark"\]\s*\{`)
	// Chỉ lấy biến có giá trị hex 6 số. Biến rgb(), var(), bóng đổ bị bỏ qua vì
	// không có phép đo tương phản cho chúng; chú thích sau dấu ; không cản.
	dongBienHex = regexp.MustCompile(`(?m)^\s*(--[A-Za-z0-9-]+)\s*:\s*(#[0-9A-Fa-f]{6})\s*;`)
)

// docKhoiBien trả về map tên biến (không có "--") -> mã hex viết hoa, của
// khối sáng hoặc khối tối trong css.
func docKhoiBien(css, cheDo string) (map[string]string, error) {
	mo := moKhoiSang
	if cheDo == cheDoToi {
		mo = moKhoiToi
	}
	loc := mo.FindStringIndex(css)
	if loc == nil {
		return nil, fmt.Errorf("không thấy khối biến chế độ %s trong %s", cheDo, tepStyles)
	}
	than := css[loc[1]:]
	// Khối kết thúc ở dấu } đầu tiên: trong khối chỉ có khai báo biến, không
	// lồng khối con.
	if end := strings.Index(than, "}"); end >= 0 {
		than = than[:end]
	}
	bien := map[string]string{}
	for _, m := range dongBienHex.FindAllStringSubmatch(than, -1) {
		bien[strings.TrimPrefix(m[1], "--")] = strings.ToUpper(m[2])
	}
	if len(bien) == 0 {
		return nil, fmt.Errorf("khối biến chế độ %s trong %s không có biến hex nào", cheDo, tepStyles)
	}
	return bien, nil
}

// docHaiKhoi đọc styles.css một lần và trả về hai khối. Lỗi đọc là lỗi cứng:
// test không có gì để so thì không được xanh.
func docHaiKhoi(t *testing.T) (sang, toi map[string]string) {
	t.Helper()
	raw, err := os.ReadFile(tepStyles)
	if err != nil {
		t.Fatalf("đọc %s: %v (test chạy trong thư mục web/)", tepStyles, err)
	}
	css := string(raw)
	if sang, err = docKhoiBien(css, cheDoSang); err != nil {
		t.Fatal(err)
	}
	if toi, err = docKhoiBien(css, cheDoToi); err != nil {
		t.Fatal(err)
	}
	return sang, toi
}

// hexToRGB đổi "#RRGGBB" thành ba kênh 0..255.
func hexToRGB(h string) (r, g, b int, err error) {
	if len(h) != 7 || h[0] != '#' {
		return 0, 0, 0, fmt.Errorf("mã màu %q không phải #RRGGBB", h)
	}
	v, err := strconv.ParseUint(h[1:], 16, 32)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("mã màu %q: %w", h, err)
	}
	return int(v >> 16 & 0xFF), int(v >> 8 & 0xFF), int(v & 0xFF), nil
}

// doChoi là độ chói tương đối theo WCAG 2.1: mỗi kênh /255, tới 0,03928 thì
// chia 12,92, trên đó thì ((v+0,055)/1,055)^2,4; rồi cộng có trọng số.
func doChoi(h string) (float64, error) {
	r, g, b, err := hexToRGB(h)
	if err != nil {
		return 0, err
	}
	kenh := func(c int) float64 {
		v := float64(c) / 255
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*kenh(r) + 0.7152*kenh(g) + 0.0722*kenh(b), nil
}

// tyLeTuongPhan là (L sáng + 0,05) / (L tối + 0,05); không phụ thuộc thứ tự hai màu.
func tyLeTuongPhan(a, b string) (float64, error) {
	la, err := doChoi(a)
	if err != nil {
		return 0, err
	}
	lb, err := doChoi(b)
	if err != nil {
		return 0, err
	}
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05), nil
}

// capMau: màu trước (chữ hoặc hình) phải đạt ngưỡng trên từng màu nền. Ngưỡng
// WCAG: chữ thường 4,5; thành phần giao diện và hình 3.
type capMau struct {
	truoc string
	nen   []string
	min   float64
}

const (
	nguongChu  = 4.5
	nguongHinh = 3.0
)

// bangCapMau chép đúng B3 của đặc tả. Tên biến không có "--". Đổi tên biến mà
// quên sửa bảng này thì test đỏ vì "thiếu biến", không lặng lẽ bỏ qua cặp.
var bangCapMau = []capMau{
	// Chữ >= 4,5.
	{"ink", []string{"paper", "surface", "soft", "pre", "accent-tint"}, nguongChu},
	{"ink-soft", []string{"surface", "paper", "soft"}, nguongChu},
	{"muted", []string{"paper", "surface", "soft", "pre", "accent-tint"}, nguongChu},
	{"accent", []string{"surface", "paper", "soft", "accent-tint"}, nguongChu},
	{"accent-hover", []string{"surface", "paper", "soft"}, nguongChu},
	{"on-accent", []string{"accent", "accent-hover"}, nguongChu},
	{"fail-text", []string{"paper", "surface", "soft", "pre"}, nguongChu},
	{"ok-text", []string{"paper", "surface", "soft"}, nguongChu},
	{"on-fail", []string{"fail"}, nguongChu},
	{"on-ink", []string{"ink"}, nguongChu},
	// Hình >= 3.
	{"ok", []string{"paper", "surface", "soft"}, nguongHinh},
	{"fail", []string{"paper", "surface", "soft"}, nguongHinh},
	{"unknown", []string{"paper", "surface", "soft"}, nguongHinh},
	{"warn-border", []string{"warn"}, nguongHinh},
	{"route", []string{"surface"}, nguongHinh},
	{"route-dash", []string{"surface"}, nguongHinh},
	{"field", []string{"surface", "soft", "paper"}, nguongHinh},
	{"ink", []string{"track"}, nguongHinh},
}

// kiemCapMau chạy bảng cặp trên một khối màu, trả về các dòng lỗi. Tách ra
// khỏi test để có test bảng cho chính nó.
func kiemCapMau(cheDo string, bien map[string]string, bang []capMau) []string {
	var loi []string
	thieu := map[string]bool{}
	lay := func(ten string) (string, bool) {
		v, ok := bien[ten]
		if !ok && !thieu[ten] {
			thieu[ten] = true
			loi = append(loi, fmt.Sprintf("chế độ %s: thiếu biến --%s (đổi tên biến thì sửa cả bảng cặp màu trong web/tuong_phan_test.go)", cheDo, ten))
		}
		return v, ok
	}
	for _, c := range bang {
		fg, okFg := lay(c.truoc)
		for _, n := range c.nen {
			bg, okBg := lay(n)
			if !okFg || !okBg {
				continue
			}
			ty, err := tyLeTuongPhan(fg, bg)
			if err != nil {
				loi = append(loi, fmt.Sprintf("chế độ %s: cặp %s trên %s: %v", cheDo, c.truoc, n, err))
				continue
			}
			if ty < c.min {
				loi = append(loi, fmt.Sprintf("chế độ %s: cặp %s trên %s (%s trên %s): tỷ lệ %.2f, cần ≥ %.1f", cheDo, c.truoc, n, fg, bg, ty, c.min))
			}
		}
	}
	return loi
}

// B5: hai chế độ khai cùng một bộ biến hex. Một biến chỉ có ở một bên nghĩa là
// chế độ kia rơi về giá trị mặc định (thường là của chế độ sáng) và sai màu.
func TestHaiCheDoCungBien(t *testing.T) {
	sang, toi := docHaiKhoi(t)
	for ten := range sang {
		if _, ok := toi[ten]; !ok {
			t.Errorf("biến --%s có ở chế độ sáng nhưng thiếu ở chế độ tối", ten)
		}
	}
	for ten := range toi {
		if _, ok := sang[ten]; !ok {
			t.Errorf("biến --%s có ở chế độ tối nhưng thiếu ở chế độ sáng", ten)
		}
	}
}

// B3: tương phản của các cặp màu thật sự dùng, ở cả hai chế độ.
func TestTuongPhan(t *testing.T) {
	sang, toi := docHaiKhoi(t)
	for _, l := range kiemCapMau(cheDoSang, sang, bangCapMau) {
		t.Error(l)
	}
	for _, l := range kiemCapMau(cheDoToi, toi, bangCapMau) {
		t.Error(l)
	}
}

// phaMau pha màu a với tỷ lệ p% trên nền s, theo sRGB, từng kênh
// round(a*p/100 + s*(100-p)/100). Làm bằng số nguyên (cộng 50 rồi chia 100) để
// không dính sai số số thực ở những kênh rơi đúng .5.
func phaMau(a, s string, p int) (string, error) {
	ar, ag, ab, err := hexToRGB(a)
	if err != nil {
		return "", err
	}
	sr, sg, sb, err := hexToRGB(s)
	if err != nil {
		return "", err
	}
	kenh := func(x, y int) int { return (x*p + y*(100-p) + 50) / 100 }
	return fmt.Sprintf("#%02X%02X%02X", kenh(ar, sr), kenh(ag, sg), kenh(ab, sb)), nil
}

// B4: --accent-tint là --accent pha 10% (sáng) hay 16% (tối) trên --surface.
// Màu nền mục đang mở chỉ có một nguồn sự thật là accent; tint ghi tay lệch đi
// thì test báo mã đúng cần điền.
func TestAccentTint(t *testing.T) {
	sang, toi := docHaiKhoi(t)
	for _, c := range []struct {
		cheDo string
		bien  map[string]string
		p     int
	}{{cheDoSang, sang, 10}, {cheDoToi, toi, 16}} {
		accent, okA := c.bien["accent"]
		surface, okS := c.bien["surface"]
		tint, okT := c.bien["accent-tint"]
		if !okA || !okS || !okT {
			t.Errorf("chế độ %s: cần đủ --accent, --surface, --accent-tint (có: accent=%v surface=%v accent-tint=%v)", c.cheDo, okA, okS, okT)
			continue
		}
		want, err := phaMau(accent, surface, c.p)
		if err != nil {
			t.Errorf("chế độ %s: %v", c.cheDo, err)
			continue
		}
		if !strings.EqualFold(tint, want) {
			t.Errorf("chế độ %s: --accent-tint là %s, nhưng %s pha %d%% trên %s ra %s", c.cheDo, tint, accent, c.p, surface, want)
		}
	}
}

// ---- Test bảng cho các hàm ở trên (tên có "Mau" để chạy riêng bằng -run Mau) ----

// Các giá trị kinh điển của WCAG: trắng/đen 21, cùng màu 1, #767676 trên trắng
// là màu xám nhạt nhất còn đạt 4,5 (4,54), #777777 thì không (4,48).
func TestMauTyLeWCAG(t *testing.T) {
	bang := []struct {
		ten, a, b string
		want      float64
	}{
		{"trắng và đen", "#FFFFFF", "#000000", 21},
		{"đảo thứ tự cho cùng kết quả", "#000000", "#FFFFFF", 21},
		{"cùng một màu", "#336699", "#336699", 1},
		{"xám 767676 trên trắng", "#767676", "#FFFFFF", 4.54},
		{"xám 777777 trên trắng", "#777777", "#FFFFFF", 4.48},
	}
	for _, c := range bang {
		t.Run(c.ten, func(t *testing.T) {
			got, err := tyLeTuongPhan(c.a, c.b)
			if err != nil {
				t.Fatal(err)
			}
			if math.Abs(got-c.want) > 0.01 {
				t.Errorf("tyLeTuongPhan(%s, %s) = %.3f, muốn %.2f", c.a, c.b, got, c.want)
			}
		})
	}
	for _, xau := range []string{"", "FFFFFF", "#FFF", "#GGGGGG", "#FFFFFFF"} {
		if _, err := doChoi(xau); err == nil {
			t.Errorf("doChoi(%q) phải báo lỗi", xau)
		}
	}
}

func TestMauDocKhoiBien(t *testing.T) {
	css := `
/* :root, ở chú thích không phải khối */
:root,
[data-theme="light"] {
  color-scheme: light;
  --ink: #16181a; /* chú thích sau dấu chấm phẩy */
  --paper: #ECEDEA;
  --scrim: rgb(23 26 31 / 0.45);
  --link: var(--accent);
  --short: #FFF;
}

[data-theme="dark"] {
  --ink: #E9EBED;
  --paper: #15171A;
}

[data-theme="dark"] .x {
  --khac: #123456;
}
`
	sang, err := docKhoiBien(css, cheDoSang)
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]string{"ink": "#16181A", "paper": "#ECEDEA"}; !mapBang(sang, want) {
		t.Errorf("khối sáng = %v, muốn %v (hex hoa; bỏ rgb(), var(), hex 3 số)", sang, want)
	}
	toi, err := docKhoiBien(css, cheDoToi)
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]string{"ink": "#E9EBED", "paper": "#15171A"}; !mapBang(toi, want) {
		t.Errorf("khối tối = %v, muốn %v (selector ghép không phải khối biến)", toi, want)
	}
	if _, err := docKhoiBien("body { color: red }", cheDoSang); err == nil {
		t.Error("không có khối sáng phải báo lỗi")
	}
}

func mapBang(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestMauKiemCapMau(t *testing.T) {
	bang := []capMau{{"ink", []string{"paper", "surface"}, nguongChu}}
	bang2 := []capMau{{"ink", []string{"paper"}, nguongChu}}
	cases := []struct {
		ten     string
		bien    map[string]string
		bang    []capMau
		wantLoi int
		chua    string // chuỗi phải có trong dòng lỗi đầu
	}{
		{"đạt", map[string]string{"ink": "#000000", "paper": "#FFFFFF", "surface": "#F6F7F4"}, bang, 0, ""},
		{"tụt tương phản", map[string]string{"ink": "#777777", "paper": "#FFFFFF", "surface": "#FFFFFF"}, bang, 2, "tỷ lệ 4.48"},
		{"thiếu biến nền", map[string]string{"ink": "#000000", "paper": "#FFFFFF"}, bang, 1, "thiếu biến --surface"},
		{"thiếu biến chữ chỉ báo một lần", map[string]string{"paper": "#FFFFFF", "surface": "#FFFFFF"}, bang, 1, "thiếu biến --ink"},
		{"đủ biến, ngưỡng hình thấp hơn", map[string]string{"ink": "#888888", "paper": "#FFFFFF"}, []capMau{{"ink", []string{"paper"}, nguongHinh}}, 0, ""},
		{"cùng cặp, ngưỡng chữ cao hơn", map[string]string{"ink": "#999999", "paper": "#FFFFFF"}, bang2, 1, "cặp ink trên paper"},
	}
	for _, c := range cases {
		t.Run(c.ten, func(t *testing.T) {
			loi := kiemCapMau(cheDoSang, c.bien, c.bang)
			if len(loi) != c.wantLoi {
				t.Fatalf("có %d lỗi, muốn %d: %q", len(loi), c.wantLoi, loi)
			}
			if c.chua != "" && !strings.Contains(loi[0], c.chua) {
				t.Errorf("lỗi đầu %q thiếu %q", loi[0], c.chua)
			}
		})
	}
}

// Số liệu ở đây lấy từ bảng màu trong mục 3.1 của đặc tả (sáng 10%, tối 16%).
func TestMauPhaMau(t *testing.T) {
	bang := []struct {
		ten, a, s string
		p         int
		want      string
	}{
		{"sáng 10%", "#0B4FB8", "#FFFFFF", 10, "#E7EDF8"},
		{"tối 16%", "#8FB8FF", "#1A1D21", 16, "#2D3645"},
		{"0% là nền", "#0B4FB8", "#FFFFFF", 0, "#FFFFFF"},
		{"100% là màu nhấn", "#0B4FB8", "#FFFFFF", 100, "#0B4FB8"},
		{"làm tròn lên ở .5", "#000000", "#010101", 50, "#010101"}, // 0,5 -> 1
	}
	for _, c := range bang {
		t.Run(c.ten, func(t *testing.T) {
			got, err := phaMau(c.a, c.s, c.p)
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Errorf("phaMau(%s, %s, %d) = %s, muốn %s", c.a, c.s, c.p, got, c.want)
			}
		})
	}
	if _, err := phaMau("xanh", "#FFFFFF", 10); err == nil {
		t.Error("màu không phải hex phải báo lỗi")
	}
}
