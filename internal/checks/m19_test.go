package checks

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// Output mẫu của getent, curl trong file này viết theo tài liệu của từng lệnh,
// chưa lấy từ máy ảo thật: nằm trong mục "cần kiểm trên máy ảo".

func m19Dom(t *testing.T, s string) validate.Domain {
	t.Helper()
	d, err := validate.ParseDomain(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func m19Doms(t *testing.T, ss ...string) []validate.Domain {
	t.Helper()
	var out []validate.Domain
	for _, s := range ss {
		out = append(out, m19Dom(t, s))
	}
	return out
}

// named bọc từng Layout thành NamedSite không có tên phụ: các test cũ của
// ServerNameUnique (M14) không đổi ý nghĩa.
func named(ls []site.Layout) []NamedSite {
	if ls == nil {
		return nil
	}
	out := make([]NamedSite, len(ls))
	for i, l := range ls {
		out[i] = NamedSite{Layout: l}
	}
	return out
}

// getentFor trả lời getent riêng cho một tên (prefix dài hơn thắng prefix chung).
func getentFor(f *system.FakeSystem, name string, a4Exit int, a4 string) {
	reply(f, []string{"getent", "ahostsv4", name}, a4Exit, a4, "")
	reply(f, []string{"getent", "ahostsv6", name}, 2, "", "")
}

func addrOf(ip, name string) string {
	return ip + "     STREAM " + name + "\n" + ip + "     DGRAM  \n" + ip + "     RAW    \n"
}

// ---- site.alias.dns ----

// B4: tên phụ chưa trỏ DNS chỉ là cảnh báo. Mọi kiểu trả lời của getent và ip
// cho Pass, Warn hay Unknown; không bao giờ Fail.
func TestAliasDNSNeverFail(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	name := m19Dom(t, "www.shop.example.com")
	const id = "site.alias.dns:www.shop.example.com"
	cases := []struct {
		name string
		f    *system.FakeSystem
		want Status
	}{
		{"không có bản ghi A", dnsFake("", 2, "", 2, ipOut, 0), Warn},
		{"không có A và ip lỗi", dnsFake("", 2, "", 2, "", 1), Warn},
		{"A trỏ nơi khác", dnsFake(a4Other, 0, "", 2, ipOut, 0), Warn},
		{"AAAA trỏ nơi khác", dnsFake(a4Mine, 0, "2001:db8::99 STREAM x\n", 0, ipOut, 0), Warn},
		{"A trỏ về máy", dnsFake(a4Mine, 0, "", 2, ipOut, 0), Pass},
		{"getent lỗi", dnsFake("", 1, "", 2, ipOut, 0), Unknown},
		{"output rác", dnsFake("\n  \nrác x\n", 0, "", 2, "inet\n", 0), Warn},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := AliasDNS(l, name).Run(bg(), c.f)
			if r.Status == Fail {
				t.Fatalf("tên phụ không bao giờ Fail: %+v", r)
			}
			assertShape(t, r, id, StageDNS, c.want)
			if c.want == Warn && !strings.HasPrefix(lastFix(r), "getent ahosts") {
				t.Errorf("dòng cuối của ManualFix phải là lệnh kiểm lại: %q", lastFix(r))
			}
			for _, cmd := range c.f.Ran() {
				if !strings.Contains(cmd.String(), "www.shop.example.com") && cmd.Name == "getent" {
					t.Errorf("getent tra tên sai: %s", cmd)
				}
			}
		})
	}
	t.Run("quá hạn thì Unknown, không phải Fail", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(bg(), time.Nanosecond)
		defer cancel()
		time.Sleep(2 * time.Millisecond)
		r := AliasDNS(l, name).Run(ctx, system.NewFake())
		assertShape(t, r, id, StageDNS, Unknown)
	})
	t.Run("đầu vào rỗng", func(t *testing.T) {
		f := system.NewFake()
		assertShape(t, AliasDNS(site.Layout{}, name).Run(bg(), f), "site.alias.dns:www.shop.example.com", StageDNS, Unknown)
		r := AliasDNS(l, validate.Domain{}).Run(bg(), f)
		if r.Status != Unknown {
			t.Errorf("Status = %s", r.Status)
		}
		assertNoCmd(t, f)
	})
}

// ---- site.alias.http ----

const aliasWPPage = "<html><link href=/wp-content/themes/x.css></html>"

func curlAnswer(body, loc, xrb, code string) string {
	return body + "\nTPAN_LOCATION=" + loc + "\nTPAN_REDIRECT_BY=" + xrb + "\nTPAN_HTTP_CODE=" + code
}

func TestAliasHTTP(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	name := m19Dom(t, "www.shop.example.com")
	const id = "site.alias.http:www.shop.example.com"
	run := func(mode site.NameMode, ssl bool, covered []validate.Domain, exit int, out, stderr string) (Result, *system.FakeSystem) {
		f := system.NewFake()
		reply(f, []string{"curl"}, exit, out, stderr)
		return AliasHTTP(l, name, mode, ssl, covered).Run(bg(), f), f
	}
	cases := []struct {
		name    string
		mode    site.NameMode
		ssl     bool
		covered []validate.Domain
		exit    int
		out     string
		want    Status
	}{
		{"serve: 200 và WordPress", site.ModeServe, false, nil, 0, curlAnswer(aliasWPPage, "", "", "200"), Pass},
		{"serve: 200 mà không phải WordPress", site.ModeServe, false, nil, 0, curlAnswer("trang mặc định", "", "", "200"), Warn},
		{"serve: 301 của WordPress", site.ModeServe, false, nil, 0, curlAnswer("", "http://shop.example.com/", "WordPress", "301"), Pass},
		{"serve: 301 không phải của WordPress", site.ModeServe, false, nil, 0, curlAnswer("", "http://shop.example.com/", "", "301"), Warn},
		{"serve: 404", site.ModeServe, false, nil, 0, curlAnswer("", "", "", "404"), Warn},
		{"redirect: 301 đúng đích", site.ModeRedirect, false, nil, 0, curlAnswer("", "http://shop.example.com/", "", "301"), Pass},
		{"redirect: đích sai", site.ModeRedirect, false, nil, 0, curlAnswer("", "http://khac.example.net/", "", "301"), Warn},
		{"redirect: là chuyển hướng của WordPress", site.ModeRedirect, false, nil, 0, curlAnswer("", "http://shop.example.com/", "WordPress", "301"), Warn},
		{"redirect: 200", site.ModeRedirect, false, nil, 0, curlAnswer(aliasWPPage, "", "", "200"), Warn},
		{"ssl, tên chưa phủ: 301 về https", site.ModeServe, true, nil, 0, curlAnswer("", "https://shop.example.com/", "", "301"), Pass},
		{"ssl, tên chưa phủ: 200 là sai dù kiểu serve", site.ModeServe, true, nil, 0, curlAnswer(aliasWPPage, "", "", "200"), Warn},
		{"ssl, tên đã phủ, serve", site.ModeServe, true, m19Doms(t, "www.shop.example.com"), 0, curlAnswer(aliasWPPage, "", "", "200"), Pass},
		{"ssl, tên đã phủ, redirect", site.ModeRedirect, true, m19Doms(t, "www.shop.example.com"), 0, curlAnswer("", "https://shop.example.com/", "", "301"), Pass},
		{"nginx đóng kết nối (52)", site.ModeServe, false, nil, 52, "", Warn},
		{"ssl đã phủ mà bắt tay TLS hỏng (35)", site.ModeServe, true, m19Doms(t, "www.shop.example.com"), 35, "", Warn},
		{"không kết nối được (7) là chưa kiểm được", site.ModeServe, false, nil, 7, "", Unknown},
		{"curl không in nhãn", site.ModeServe, false, nil, 0, "<html>", Unknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, f := run(c.mode, c.ssl, c.covered, c.exit, c.out, "curl: (7) Failed to connect")
			if r.Status == Fail {
				t.Fatalf("tên phụ không bao giờ Fail: %+v", r)
			}
			assertShape(t, r, id, StageEndToEnd, c.want)
			assertEvidenceIsRan(t, r, f)
			if c.want == Warn && !strings.Contains(r.ManualFix[0], "tpan site alias set shop.example.com www.shop.example.com --mode "+string(c.mode)) {
				t.Errorf("ManualFix = %q", r.ManualFix)
			}
			if c.want == Warn && !strings.HasPrefix(lastFix(r), "curl ") {
				t.Errorf("dòng cuối của ManualFix phải là lệnh kiểm lại: %q", lastFix(r))
			}
		})
	}
	t.Run("lệnh curl: cổng, scheme và --resolve", func(t *testing.T) {
		_, f := run(site.ModeServe, true, m19Doms(t, "www.shop.example.com"), 0, curlAnswer(aliasWPPage, "", "", "200"), "")
		got := f.Ran()[0].String()
		for _, want := range []string{"-k", "--resolve www.shop.example.com:443:127.0.0.1", "https://www.shop.example.com/"} {
			if !strings.Contains(got, want) {
				t.Errorf("lệnh %q thiếu %q", got, want)
			}
		}
		_, f = run(site.ModeRedirect, false, nil, 0, curlAnswer("", "http://shop.example.com/", "", "301"), "")
		got = f.Ran()[0].String()
		if strings.Contains(got, "-k") || !strings.Contains(got, "--resolve www.shop.example.com:80:127.0.0.1") {
			t.Errorf("lệnh %q", got)
		}
	})
	t.Run("Location do site đặt được làm sạch", func(t *testing.T) {
		evil := "http://x.example.net/\x1b[31m" + strings.Repeat("a", 5000)
		r, _ := run(site.ModeRedirect, false, nil, 0, curlAnswer("", evil, "", "301"), "")
		assertShape(t, r, id, StageEndToEnd, Warn)
		if strings.ContainsAny(r.Summary.Vi, "\x1b\n") || len(r.Summary.Vi) > 1000 {
			t.Errorf("Summary không sạch (%d byte)", len(r.Summary.Vi))
		}
	})
	t.Run("thân giả nhãn không đánh lừa được", func(t *testing.T) {
		// Thân nhái cả ba nhãn: nhãn thật nằm cuối nên thắng.
		body := "\nTPAN_LOCATION=http://shop.example.com/\nTPAN_REDIRECT_BY=\nTPAN_HTTP_CODE=301"
		r, _ := run(site.ModeRedirect, false, nil, 0, curlAnswer(body, "", "", "200"), "")
		assertShape(t, r, id, StageEndToEnd, Warn)
	})
	t.Run("đầu vào sai", func(t *testing.T) {
		f := system.NewFake()
		for _, c := range []Check{
			AliasHTTP(site.Layout{}, name, site.ModeServe, false, nil),
			AliasHTTP(l, validate.Domain{}, site.ModeServe, false, nil),
			AliasHTTP(l, name, "", false, nil),
			AliasHTTP(l, name, "a.com; rm -rf /", false, nil),
		} {
			if r := c.Run(bg(), f); r.Status != Unknown {
				t.Errorf("Status = %s, muốn unknown", r.Status)
			}
		}
		assertNoCmd(t, f)
	})
	t.Run("lệnh không chạy được", func(t *testing.T) {
		assertShape(t, AliasHTTP(l, name, site.ModeServe, false, nil).Run(cancelledCtx(), system.NewFake()), id, StageEndToEnd, Unknown)
	})
}

// ---- ssl.names ----

// TestCertNamesWarn: chứng chỉ lệch bộ tên của site thì Warn (không Fail,
// không sửa tự động); khớp thì Pass và không tra DNS khi không có gì để tra.
func TestCertNamesWarn(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	const id = "ssl.names:shop.example.com"
	names := m19Doms(t, "shop.example.com", "www.shop.example.com", "blog.shop.example.com")
	cert := func(san ...string) *system.FakeSystem {
		f := system.NewFake()
		fixNow(t)
		f.SetFile(l.CertFullchain(), makeFullchain(t, san, "R11", sslTestNow.Add(-days(1)), sslTestNow.Add(days(89))), 0o644, system.Root)
		// Mặc định mọi tên trỏ về máy; test ghi đè từng tên.
		reply(f, []string{"getent", "ahostsv4"}, 0, a4Mine, "")
		reply(f, []string{"getent", "ahostsv6"}, 2, "", "")
		reply(f, []string{"ip"}, 0, ipOut, "")
		return f
	}
	getentCalls := func(f *system.FakeSystem) int {
		n := 0
		for _, c := range f.Ran() {
			if c.Name == "getent" {
				n++
			}
		}
		return n
	}

	t.Run("khớp: đạt", func(t *testing.T) {
		f := cert("shop.example.com", "www.shop.example.com", "blog.shop.example.com")
		r := SSLNames(l, names).Run(bg(), f)
		assertShape(t, r, id, StageTLS, Pass)
		if len(r.Evidence) != 1 || !strings.HasPrefix(r.Evidence[0].Command, "read "+l.CertFullchain()) {
			t.Errorf("Evidence = %+v", r.Evidence)
		}
	})
	t.Run("khớp hai tên: tra DNS từng tên phụ trong chứng chỉ, tên chính thì không", func(t *testing.T) {
		f := cert("shop.example.com", "www.shop.example.com")
		r := SSLNames(l, names[:2]).Run(bg(), f)
		assertShape(t, r, id, StageTLS, Pass)
		if n := getentCalls(f); n != 2 { // www: v4 và v6
			t.Errorf("%d lần getent, muốn 2", n)
		}
	})
	t.Run("site chưa có tên phụ: không tra DNS", func(t *testing.T) {
		f := cert("shop.example.com")
		assertShape(t, SSLNames(l, names[:1]).Run(bg(), f), id, StageTLS, Pass)
		if n := len(f.Ran()); n != 0 {
			t.Errorf("chạy %d lệnh, muốn 0", n)
		}
	})
	t.Run("tên mới thêm, DNS đã trỏ về máy, chưa có trong chứng chỉ", func(t *testing.T) {
		f := cert("shop.example.com", "www.shop.example.com")
		r := SSLNames(l, names).Run(bg(), f)
		assertShape(t, r, id, StageTLS, Warn)
		if !strings.Contains(r.Summary.Vi, "blog.shop.example.com") || !strings.Contains(r.Summary.Vi, "chưa có trong chứng chỉ") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
		if r.ManualFix[0] != "sudo tpan ssl issue shop.example.com" || !strings.HasPrefix(lastFix(r), "openssl x509 -in "+l.CertFullchain()) {
			t.Errorf("ManualFix = %q", r.ManualFix)
		}
		if AutoFixLess := r.AutoFix; AutoFixLess != "" {
			t.Errorf("không có sửa tự động (tốn hạn mức): %q", AutoFixLess)
		}
	})
	t.Run("tên mới thêm mà DNS chưa trỏ: không nhắc (alias.dns lo)", func(t *testing.T) {
		f := cert("shop.example.com", "www.shop.example.com")
		getentFor(f, "blog.shop.example.com", 2, "")
		assertShape(t, SSLNames(l, names).Run(bg(), f), id, StageTLS, Pass)
	})
	t.Run("tên mới thêm, A không phải của máy (có thể sau NAT): không nhắc", func(t *testing.T) {
		f := cert("shop.example.com", "www.shop.example.com")
		getentFor(f, "blog.shop.example.com", 0, addrOf("198.51.100.9", "blog.shop.example.com"))
		assertShape(t, SSLNames(l, names).Run(bg(), f), id, StageTLS, Pass)
	})
	t.Run("chứng chỉ còn tên đã gỡ khỏi site", func(t *testing.T) {
		f := cert("shop.example.com", "www.shop.example.com", "old.shop.example.com")
		r := SSLNames(l, names[:2]).Run(bg(), f)
		assertShape(t, r, id, StageTLS, Warn)
		if !strings.Contains(r.Summary.Vi, "old.shop.example.com") || !strings.Contains(r.Summary.Vi, "không còn dùng") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
		if !strings.Contains(r.Cause.Vi, "gia hạn") {
			t.Errorf("Cause phải nói gia hạn sẽ hỏng: %q", r.Cause.Vi)
		}
	})
	t.Run("tên trong chứng chỉ không còn bản ghi A", func(t *testing.T) {
		f := cert("shop.example.com", "www.shop.example.com")
		getentFor(f, "www.shop.example.com", 2, "")
		r := SSLNames(l, names[:2]).Run(bg(), f)
		assertShape(t, r, id, StageTLS, Warn)
		if !strings.Contains(r.Summary.Vi, "không còn trỏ về máy: www.shop.example.com") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
		if !containsStr(r.ManualFix, "getent ahostsv4 www.shop.example.com") {
			t.Errorf("ManualFix = %q", r.ManualFix)
		}
	})
	t.Run("tên trong chứng chỉ trỏ máy khác, tên chính vẫn về máy này", func(t *testing.T) {
		f := cert("shop.example.com", "www.shop.example.com")
		getentFor(f, "www.shop.example.com", 0, addrOf("198.51.100.9", "www.shop.example.com"))
		assertShape(t, SSLNames(l, names[:2]).Run(bg(), f), id, StageTLS, Warn)
	})
	t.Run("máy sau NAT (tên chính cũng không khớp): chỉ thiếu bản ghi A mới tính", func(t *testing.T) {
		f := cert("shop.example.com", "www.shop.example.com")
		reply(f, []string{"getent", "ahostsv4"}, 0, a4Other, "")
		assertShape(t, SSLNames(l, names[:2]).Run(bg(), f), id, StageTLS, Pass)
	})
	t.Run("chưa có chứng chỉ, file hỏng, đọc lỗi: chưa kiểm được", func(t *testing.T) {
		f := system.NewFake()
		assertShape(t, SSLNames(l, names).Run(bg(), f), id, StageTLS, Unknown)
		f.SetFile(l.CertFullchain(), []byte("không phải PEM"), 0o644, system.Root)
		assertShape(t, SSLNames(l, names).Run(bg(), f), id, StageTLS, Unknown)
		assertNoCmd(t, f)
	})
	t.Run("không bao giờ Fail", func(t *testing.T) {
		for _, san := range [][]string{{}, {"x.example.net"}, {"shop.example.com", "a.example.net", "b.example.net"}} {
			f := cert(san...)
			getentFor(f, "www.shop.example.com", 2, "")
			if r := SSLNames(l, names).Run(bg(), f); r.Status == Fail {
				t.Errorf("SAN %v: Fail %+v", san, r)
			}
		}
	})
	t.Run("layout rỗng", func(t *testing.T) {
		f := system.NewFake()
		assertShape(t, SSLNames(site.Layout{}, names).Run(bg(), f), "ssl.names:", StageTLS, Unknown)
		assertNoCmd(t, f)
	})
}

// ---- names.root.shared ----

func TestRootSharedWarn(t *testing.T) {
	const id = "names.root.shared"
	own := func(site, owner string, names ...string) OwnedNames {
		return OwnedNames{Site: m19Dom(t, site), Owner: owner, Names: m19Doms(t, names...)}
	}
	t.Run("một chủ cho mỗi tên miền gốc: đạt", func(t *testing.T) {
		r := NamesRootShared([]OwnedNames{
			own("example.com", "alice", "example.com", "www.example.com"),
			own("blog.example.com", "alice", "blog.example.com"),
			own("example.net", "bob", "example.net", "www.example.net"),
		}).Run(bg(), system.NewFake())
		assertShape(t, r, id, StageTPAN, Pass)
	})
	t.Run("hai chủ cùng tên miền gốc: cảnh báo, nêu tên miền gốc và các site", func(t *testing.T) {
		f := system.NewFake()
		r := NamesRootShared([]OwnedNames{
			own("shop.example.com", "alice", "shop.example.com"),
			own("blog.example.com", "bob", "blog.example.com", "www.blog.example.com"),
			own("example.net", "bob", "example.net"),
		}).Run(bg(), f)
		assertShape(t, r, id, StageTPAN, Warn)
		for _, want := range []string{"example.com", "2 chủ", "blog.example.com", "shop.example.com"} {
			if !strings.Contains(r.Summary.Vi, want) {
				t.Errorf("Summary = %q, thiếu %q", r.Summary.Vi, want)
			}
		}
		if strings.Contains(r.Summary.Vi+r.Evidence[0].Output, "alice") || strings.Contains(r.Summary.Vi+r.Evidence[0].Output, "bob") {
			t.Errorf("không đưa tên chủ vào kết quả: %q / %q", r.Summary.Vi, r.Evidence[0].Output)
		}
		assertNoCmd(t, f)
		if AutoFixName := r.AutoFix; AutoFixName != "" {
			t.Errorf("AutoFix = %q", AutoFixName)
		}
	})
	t.Run("tên phụ của chủ khác cũng tính", func(t *testing.T) {
		r := NamesRootShared([]OwnedNames{
			own("example.com", "alice", "example.com"),
			own("example.net", "bob", "example.net", "www.example.com"),
		}).Run(bg(), system.NewFake())
		assertShape(t, r, id, StageTPAN, Warn)
	})
	t.Run("nhiều tên miền gốc bị chia: nói còn bao nhiêu", func(t *testing.T) {
		r := NamesRootShared([]OwnedNames{
			own("a.example.com", "x", "a.example.com"), own("b.example.com", "y", "b.example.com"),
			own("a.example.net", "x", "a.example.net"), own("b.example.net", "y", "b.example.net"),
		}).Run(bg(), system.NewFake())
		assertShape(t, r, id, StageTPAN, Warn)
		if !strings.Contains(r.Summary.Vi, "còn 1 tên miền gốc khác") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
	})
	t.Run("hậu tố công cộng không có tên miền gốc: bỏ qua", func(t *testing.T) {
		r := NamesRootShared([]OwnedNames{
			own("alice.github.io", "alice", "alice.github.io"),
			own("bob.github.io", "bob", "bob.github.io"),
		}).Run(bg(), system.NewFake())
		assertShape(t, r, id, StageTPAN, Pass)
	})
	t.Run("không có site nào", func(t *testing.T) {
		assertShape(t, NamesRootShared(nil).Run(bg(), system.NewFake()), id, StageTPAN, Pass)
	})
}

// ---- web.server_name.unique mở rộng ----

func TestServerNameUniqueAliases(t *testing.T) {
	const id = "web.server_name.unique"
	a, b := siteLayout(t, "a.example.com"), siteLayout(t, "b.example.com")
	www := m19Dom(t, "www.a.example.com")
	// Site a khai thêm www.a.example.com cùng khối server với tên chính.
	siteA := [2]string{a.NginxLink, "# Do tpan quản lý.\nserver {\n    listen 80;\n    listen [::]:80;\n    server_name a.example.com www.a.example.com;\n}\n"}
	siteB := [2]string{b.NginxLink, ngxSiteB}
	base := [][2]string{
		{"/etc/nginx/nginx.conf", ngxMainConf},
		{"/etc/nginx/sites-enabled/tpan-default.conf", ngxDefault},
	}
	dump := func(extra ...[2]string) string { return nginxDump(append(append([][2]string{}, base...), extra...)...) }
	run := func(sites []NamedSite, stdout string) Result {
		f := system.NewFake()
		reply(f, []string{"nginx", "-T"}, 0, stdout, ngxSyntaxOK)
		return ServerNameUnique("nginx", sites).Run(bg(), f)
	}
	withAlias := []NamedSite{{Layout: a, Names: []validate.Domain{www}}, {Layout: b}}

	t.Run("tên phụ chỉ khai ở vhost của site: đạt, Summary đếm cả tên phụ", func(t *testing.T) {
		r := run(withAlias, dump(siteA, siteB))
		assertShape(t, r, id, StageWeb, Pass)
		if !strings.Contains(r.Summary.Vi, "3 tên miền của 2 site") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
		if !strings.Contains(r.Evidence[0].Output, a.NginxLink+":5: www.a.example.com (port 80)") {
			t.Errorf("bằng chứng thiếu tên phụ: %q", r.Evidence[0].Output)
		}
	})
	t.Run("không có tên phụ: Summary như M14", func(t *testing.T) {
		r := run(named([]site.Layout{a, b}), dump(siteA, siteB))
		assertShape(t, r, id, StageWeb, Pass)
		if r.Summary.Vi != "Tên miền của 2 site đều chỉ được khai ở vhost của chính site đó" {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
	})
	t.Run("file lạ khai trùng tên phụ: lỗi, nêu tên phụ và hai file", func(t *testing.T) {
		stray := "server {\n    listen 80;\n    server_name www.a.example.com;\n}\n"
		r := run(withAlias, dump([2]string{ngxStrayPath, stray}, siteA, siteB))
		assertShape(t, r, id, StageWeb, Fail)
		for _, want := range []string{"www.a.example.com", ngxStrayPath + ":3", a.NginxLink + ":5"} {
			if !strings.Contains(r.Summary.Vi, want) {
				t.Errorf("Summary = %q, thiếu %q", r.Summary.Vi, want)
			}
		}
	})
	t.Run("site khác khai trùng tên phụ của site này", func(t *testing.T) {
		twin := [2]string{b.NginxLink, "server {\n    listen 80;\n    server_name b.example.com www.a.example.com;\n}\n"}
		r := run(withAlias, dump(siteA, twin))
		assertShape(t, r, id, StageWeb, Fail)
		if !strings.Contains(r.Summary.Vi, "www.a.example.com") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
	})
	t.Run("tên phụ không được kê cho site thì file lạ khai nó không bị báo", func(t *testing.T) {
		stray := "server {\n    listen 80;\n    server_name www.a.example.com;\n}\n"
		r := run(named([]site.Layout{a, b}), dump([2]string{ngxStrayPath, stray}, siteB))
		assertShape(t, r, id, StageWeb, Pass)
	})
	t.Run("tên phụ kê trùng tên chính hay lặp: tính một lần", func(t *testing.T) {
		dup := []NamedSite{{Layout: a, Names: []validate.Domain{a.Domain, www, www}}, {Layout: b}}
		r := run(dup, dump(siteA, siteB))
		assertShape(t, r, id, StageWeb, Pass)
		if !strings.Contains(r.Summary.Vi, "3 tên miền của 2 site") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
	})
	t.Run("Apache: ServerAlias của site được tính", func(t *testing.T) {
		out := "VirtualHost configuration:\n*:80                   is a NameVirtualHost\n" +
			"         default server a.example.com (" + a.ApacheLink + ":1)\n" +
			"         port 80 namevhost a.example.com (" + a.ApacheLink + ":1)\n" +
			"                 alias www.a.example.com\n" +
			"         port 80 namevhost www.a.example.com (/etc/apache2/sites-enabled/zz-stray.conf:1)\n"
		f := system.NewFake()
		reply(f, []string{"apache2ctl", "-S"}, 0, out, "")
		r := ServerNameUnique("apache", withAlias[:1]).Run(bg(), f)
		assertShape(t, r, id, StageWeb, Fail)
		if !strings.Contains(r.Summary.Vi, "www.a.example.com") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
	})
	t.Run("layout rỗng trong NamedSite", func(t *testing.T) {
		assertShape(t, ServerNameUnique("nginx", []NamedSite{{Layout: a}, {}}).Run(bg(), system.NewFake()), id, StageWeb, Unknown)
	})
}
