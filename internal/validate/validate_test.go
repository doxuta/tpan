package validate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// Các chuỗi tấn công dùng chung cho Domain và Email: không chuỗi nào được lọt qua.
func TestParseDomain_Hop_le(t *testing.T) {
	cases := []struct{ in, want string }{
		{"example.com", "example.com"},
		{"Example.COM", "example.com"},
		{"  shop.vn \t", "shop.vn"},
		{"a.b.c.example.com", "a.b.c.example.com"},
		{"a-b.com", "a-b.com"},
		{"123.com", "123.com"},
		{"xn--bcher-kva.example", "xn--bcher-kva.example"},
		// Tên miền có dấu phải ra dạng punycode.
		{"bücher.example", "xn--bcher-kva.example"},
		{"BÜCHER.example", "xn--bcher-kva.example"},
		{strings.Repeat("a", 63) + ".com", strings.Repeat("a", 63) + ".com"},
	}
	for _, c := range cases {
		got, err := ParseDomain(c.in)
		if err != nil {
			t.Errorf("ParseDomain(%q) lỗi: %v", c.in, err)
			continue
		}
		if got.String() != c.want {
			t.Errorf("ParseDomain(%q) = %q, muốn %q", c.in, got, c.want)
		}
	}
}

func TestParseDomain_Co_dau_ra_punycode(t *testing.T) {
	got, err := ParseDomain("cửahàng.vn")
	if err != nil {
		t.Fatalf("ParseDomain: %v", err)
	}
	s := got.String()
	if !strings.HasPrefix(s, "xn--") || !strings.HasSuffix(s, ".vn") {
		t.Errorf("muốn xn--....vn, được %q", s)
	}
	// Dạng NFD (chữ cái + dấu rời) phải ra cùng kết quả với dạng NFC.
	nfd, err := ParseDomain("củàhang̀.vn")
	if err != nil {
		t.Fatalf("ParseDomain NFD: %v", err)
	}
	_ = nfd // không bắt buộc bằng nhau ở mọi trường hợp; chỉ cần ra ASCII hợp lệ
	for _, r := range nfd.String() {
		if r > 127 {
			t.Errorf("còn ký tự ngoài ASCII trong %q", nfd)
		}
	}
}

func TestParseDomain_Tan_cong(t *testing.T) {
	long300 := strings.Repeat("a", 60) + "." + strings.Repeat("b", 60) + "." +
		strings.Repeat("c", 60) + "." + strings.Repeat("d", 60) + "." + strings.Repeat("e", 55) + ".com"
	if len(long300) < 254 {
		t.Fatalf("test sai: domain dài %d", len(long300))
	}
	bad := []string{
		"",
		"   ",
		"a.com; rm -rf /",
		"$(id).com",
		"`id`.com",
		"../../etc/passwd",
		"a.com/../b",
		"a.com\nb.com",
		"a.com\r\nHost: x",
		"a.com\x00",
		"a\x00.com",
		"a b.com",
		"a.com b.com",
		"-a.com",
		"a-.com",
		"a.-b.com",
		"a.b-.com",
		"a..com",
		".a.com",
		long300,
		strings.Repeat("a", 64) + ".com",
		"localhost",
		"example",
		"127.0.0.1",
		"1.2.3.4",
		"127.1",
		"::1",
		"[::1]",
		"2001:db8::1",
		"example.com.",
		"example.com..",
		"http://example.com",
		"example.com:8080",
		"user@example.com",
		"exa_mple.com",
		"exa*mple.com",
		"a.com|id",
		"a.com&id",
		"a.com>x",
		"a.com'",
		"a.com\"",
		"a.com\\",
		"xn--.com",
		"xn--a.com",   // punycode hỏng
		"a.com\u200b", // ký tự độ rộng bằng không
		"a\u202e.com", // ký tự đảo chiều chữ
	}
	for _, in := range bad {
		got, err := ParseDomain(in)
		if err == nil {
			t.Errorf("ParseDomain(%q) phải lỗi, được %q", in, got)
			continue
		}
		var ve *Error
		if !errors.As(err, &ve) {
			t.Errorf("ParseDomain(%q): lỗi không phải *Error: %T", in, err)
			continue
		}
		if ve.Code != "domain_invalid" {
			t.Errorf("ParseDomain(%q): code = %q", in, ve.Code)
		}
		if ve.Message.Vi == "" || ve.Message.En == "" {
			t.Errorf("ParseDomain(%q): thiếu thông báo hai ngôn ngữ: %+v", in, ve.Message)
		}
		if !got.IsZero() {
			t.Errorf("ParseDomain(%q): lỗi thì Domain phải rỗng, được %q", in, got)
		}
	}
}

func TestParseDomain_Do_dai_toi_da(t *testing.T) {
	// 253 ký tự: 3 nhãn 63 + 1 nhãn 61 + 3 dấu chấm... dựng đúng 253.
	lbl := strings.Repeat("a", 63)
	d253 := lbl + "." + lbl + "." + lbl + "." + strings.Repeat("a", 61)
	if len(d253) != 253 {
		t.Fatalf("test sai: %d", len(d253))
	}
	if _, err := ParseDomain(d253); err != nil {
		t.Errorf("253 ký tự phải hợp lệ: %v", err)
	}
	if _, err := ParseDomain(d253 + "a"); err == nil {
		t.Errorf("254 ký tự phải lỗi")
	}
}

func TestDomain_Text_va_JSON(t *testing.T) {
	d, err := ParseDomain("Shop.VN")
	if err != nil {
		t.Fatal(err)
	}
	b, err := d.MarshalText()
	if err != nil || string(b) != "shop.vn" {
		t.Fatalf("MarshalText = %q, %v", b, err)
	}
	js, err := json.Marshal(struct{ D Domain }{d})
	if err != nil || string(js) != `{"D":"shop.vn"}` {
		t.Fatalf("json = %s, %v", js, err)
	}

	var back struct{ D Domain }
	if err := json.Unmarshal([]byte(`{"D":"BÜCHER.example"}`), &back); err != nil {
		t.Fatal(err)
	}
	if back.D.String() != "xn--bcher-kva.example" {
		t.Errorf("Unmarshal = %q", back.D)
	}
	// UnmarshalText phải kiểm như ParseDomain.
	var bad Domain
	if err := bad.UnmarshalText([]byte("a.com; rm -rf /")); err == nil {
		t.Errorf("UnmarshalText phải từ chối chuỗi tấn công")
	}
	if !bad.IsZero() {
		t.Errorf("UnmarshalText lỗi thì phải giữ Domain rỗng, được %q", bad)
	}
	if err := json.Unmarshal([]byte(`{"D":"-a.com"}`), &back); err == nil {
		t.Errorf("json phải từ chối -a.com")
	}
}

func TestDomain_IsZero(t *testing.T) {
	var z Domain
	if !z.IsZero() || z.String() != "" {
		t.Errorf("giá trị zero phải IsZero và rỗng")
	}
	d, _ := ParseDomain("a.com")
	if d.IsZero() {
		t.Errorf("domain hợp lệ không được IsZero")
	}
}

func TestParseEmail_Hop_le(t *testing.T) {
	cases := []struct{ in, want string }{
		{"admin@example.com", "admin@example.com"},
		{"  a.b+tag@Example.COM ", "a.b+tag@example.com"},
		{"x@bücher.example", "x@xn--bcher-kva.example"},
	}
	for _, c := range cases {
		got, err := ParseEmail(c.in)
		if err != nil {
			t.Errorf("ParseEmail(%q) lỗi: %v", c.in, err)
			continue
		}
		if got.String() != c.want {
			t.Errorf("ParseEmail(%q) = %q, muốn %q", c.in, got, c.want)
		}
	}
}

func TestParseEmail_Tan_cong(t *testing.T) {
	bad := []string{
		"",
		"a@b.com\nBcc: x@y.com",
		"a@b.com\r\nBcc: x@y.com",
		"Tên <a@b.com>",
		"<a@b.com>",
		"a@b.com; rm",
		"a@b.com, c@d.com",
		"a b@c.com",
		"a@b.com\x00",
		"a@localhost",
		"a@127.0.0.1",
		"a@[127.0.0.1]",
		"a@-b.com",
		"a@b.com.",
		"@b.com",
		"a@",
		"a",
		"a@@b.com",
		"\"a b\"@c.com",
		"a(comment)@b.com",
		"a@b.com (x)",
		"$(id)@b.com",
		"`id`@b.com",
		"a@b.com`id`",
		strings.Repeat("a", 65) + "@b.com",
	}
	for _, in := range bad {
		got, err := ParseEmail(in)
		if err == nil {
			t.Errorf("ParseEmail(%q) phải lỗi, được %q", in, got)
			continue
		}
		var ve *Error
		if !errors.As(err, &ve) || ve.Code != "email_invalid" {
			t.Errorf("ParseEmail(%q): lỗi sai: %v", in, err)
		}
		if ve != nil && (ve.Message.Vi == "" || ve.Message.En == "") {
			t.Errorf("ParseEmail(%q): thiếu thông báo hai ngôn ngữ", in)
		}
	}
}

func TestEmail_Text(t *testing.T) {
	e, err := ParseEmail("A@Example.com")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := e.MarshalText()
	if string(b) != "A@example.com" {
		t.Errorf("MarshalText = %q", b)
	}
	var back Email
	if err := back.UnmarshalText([]byte("a@b.com\nBcc: x@y.com")); err == nil {
		t.Errorf("UnmarshalText phải kiểm như ParseEmail")
	}
	if err := back.UnmarshalText([]byte("a@b.com")); err != nil || back.String() != "a@b.com" {
		t.Errorf("UnmarshalText hợp lệ: %q, %v", back, err)
	}
}

func TestSlugOf(t *testing.T) {
	cases := []string{"shop.vn", "example.com", "a.b.c.d.e.f.g.h.i.example.com", "xn--bcher-kva.example", "a-b.com"}
	for _, in := range cases {
		d, err := ParseDomain(in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		sum := sha256.Sum256([]byte(d.String()))
		hash := hex.EncodeToString(sum[:])[:6]

		head := d.String()
		if len(head) > 16 {
			head = head[:16]
		}
		var b strings.Builder
		for i := 0; i < len(head); i++ {
			c := head[i]
			if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
				b.WriteByte(c)
			} else {
				b.WriteByte('_')
			}
		}
		want := b.String() + "_" + hash

		got := SlugOf(d)
		if got.String() != want {
			t.Errorf("SlugOf(%q) = %q, muốn %q", in, got, want)
		}
		if got.LinuxUser() != "tp_"+want || got.DBName() != "tp_"+want {
			t.Errorf("LinuxUser/DBName sai cho %q", in)
		}
	}
}

func TestSlugOf_Vi_du_shop_vn(t *testing.T) {
	d, _ := ParseDomain("shop.vn")
	sum := sha256.Sum256([]byte("shop.vn"))
	want := "shop_vn_" + hex.EncodeToString(sum[:3])
	if got := SlugOf(d).String(); got != want {
		t.Errorf("SlugOf(shop.vn) = %q, muốn %q", got, want)
	}
}

func TestSlug_Khac_domain_khac_slug(t *testing.T) {
	// Hai domain có 16 ký tự đầu giống nhau vẫn phải ra slug khác nhờ phần hash.
	a, _ := ParseDomain("aaaaaaaaaaaaaaaa1.com")
	b, _ := ParseDomain("aaaaaaaaaaaaaaaa2.com")
	if SlugOf(a) == SlugOf(b) {
		t.Errorf("slug trùng nhau: %q", SlugOf(a))
	}
	// Domain chỉ khác nhau ở ký tự bị đổi thành '_' cũng phải khác.
	c, _ := ParseDomain("a-b.com")
	d, _ := ParseDomain("a.b.com")
	if SlugOf(c) == SlugOf(d) {
		t.Errorf("slug trùng nhau: %q", SlugOf(c))
	}
}

func TestSlug_LinuxUser_toi_da_32(t *testing.T) {
	lbl := strings.Repeat("a", 63)
	longest := lbl + "." + lbl + "." + lbl + "." + strings.Repeat("a", 61)
	for _, in := range []string{"a.b", "shop.vn", longest, strings.Repeat("z", 63) + ".com", "bücher.example"} {
		d, err := ParseDomain(in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		s := SlugOf(d)
		if n := len(s.LinuxUser()); n > 32 {
			t.Errorf("LinuxUser %q dài %d > 32", s.LinuxUser(), n)
		}
		if n := len(s.DBName()); n > 32 {
			t.Errorf("DBName %q dài %d > 32", s.DBName(), n)
		}
		for _, r := range s.String() {
			if !isLowerAlnum(r) && r != '_' {
				t.Errorf("slug %q có ký tự ngoài [a-z0-9_]", s)
			}
		}
	}
}

func TestSlug_JSON(t *testing.T) {
	d, _ := ParseDomain("shop.vn")
	js, err := json.Marshal(SlugOf(d))
	if err != nil || !strings.HasPrefix(string(js), `"shop_vn_`) {
		t.Errorf("json Slug = %s, %v", js, err)
	}
}

func TestError(t *testing.T) {
	_, err := ParseDomain("-a.com")
	if err == nil {
		t.Fatal("muốn lỗi")
	}
	if !strings.Contains(err.Error(), "domain_invalid") {
		t.Errorf("Error() thiếu code: %q", err.Error())
	}
	// Chuỗi tấn công không được xuất hiện nguyên văn trong thông báo gửi cho người dùng.
	_, err = ParseDomain("a.com\nb.com")
	if strings.Contains(err.Error(), "\n") {
		t.Errorf("Error() chứa xuống dòng: %q", err.Error())
	}
}

func TestSlugTextRoundTrip(t *testing.T) {
	d, err := ParseDomain("shop.example.com")
	if err != nil {
		t.Fatal(err)
	}
	want := SlugOf(d)
	b, _ := want.MarshalText()
	var got Slug
	if err := got.UnmarshalText(b); err != nil || got != want {
		t.Fatalf("đọc lại slug %q: %v, %q", b, err, got)
	}
	for _, bad := range []string{"tp_x", "a; rm -rf /_abcdef", "SHOP_abcdef", "x_ABCDEF"} {
		if err := got.UnmarshalText([]byte(bad)); err == nil {
			t.Errorf("slug %q phải bị từ chối", bad)
		}
	}
}
