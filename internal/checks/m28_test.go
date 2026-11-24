package checks

import (
	"io/fs"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
)

// Test của các phép kiểm M28 đợt 2 (m28.go). Output mẫu của apachectl -M và
// mariadb viết theo tài liệu, chưa lấy từ máy ảo: nằm trong mục "cần kiểm trên
// máy ảo" của báo cáo.

// ---- SiteDocroot ----

func TestSiteDocroot(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	const id = "site.docroot:shop.example.com"
	recheck := "ls -ld " + l.Public
	siteOwner := system.Owner{User: l.User, Group: site.WebGroup}

	t.Run("public là thư mục thật của site thì đạt", func(t *testing.T) {
		f := system.NewFake()
		f.SetSite(l.Dir, l.User)
		r := SiteDocroot(l).Run(bg(), f)
		assertShape(t, r, id, StageWeb, Pass)
		assertNoCmd(t, f)
	})

	t.Run("public là symlink thì lỗi, đích chỉ ở bằng chứng, đã lọc và cắt", func(t *testing.T) {
		f := system.NewFake()
		f.SetDir(l.Dir, fs.ModeSetgid|0o750, siteOwner)
		target := "/var/www/b.example.com/rieng\n\x1b[31m" + strings.Repeat("x", 300)
		f.SetSymlink(target, l.Public)
		r := SiteDocroot(l).Run(bg(), f)
		assertShape(t, r, id, StageWeb, Fail)
		assertNoCmd(t, f)
		if lastFix(r) != recheck {
			t.Errorf("ManualFix = %q", r.ManualFix)
		}
		if len(r.Evidence) != 2 {
			t.Fatalf("Evidence = %+v", r.Evidence)
		}
		out := r.Evidence[1].Output
		if !strings.HasPrefix(out, "/var/www/b.example.com/rieng") || strings.ContainsAny(out, "\n\x1b") {
			t.Errorf("đích trong bằng chứng chưa lọc: %q", out)
		}
		if n := utf8.RuneCountInString(strings.TrimSuffix(out, docrootCutMark)); n != docrootLinkMax {
			t.Errorf("đích dài %d ký tự, muốn cắt ở %d: %q", n, docrootLinkMax, out)
		}
		// Chuỗi do site viết không vào Summary hay Cause (S14).
		for _, s := range []string{r.Summary.Vi, r.Summary.En, r.Cause.Vi, r.Cause.En} {
			if strings.Contains(s, "b.example.com") {
				t.Errorf("đích của link lọt vào lời báo: %q", s)
			}
		}
	})

	t.Run("các ca hỏng khác", func(t *testing.T) {
		for name, set := range map[string]func(f *system.FakeSystem){
			"không có public": func(f *system.FakeSystem) { f.SetDir(l.Dir, 0o750, siteOwner) },
			"public là file": func(f *system.FakeSystem) {
				f.SetDir(l.Dir, 0o750, siteOwner)
				f.SetFile(l.Public, nil, 0o640, siteOwner)
			},
			"public thuộc user khác": func(f *system.FakeSystem) {
				f.SetDir(l.Dir, 0o750, siteOwner)
				f.SetDir(l.Public, 0o750, system.Owner{User: "tp_b_example_com", Group: site.WebGroup})
			},
		} {
			t.Run(name, func(t *testing.T) {
				f := system.NewFake()
				set(f)
				r := SiteDocroot(l).Run(bg(), f)
				assertShape(t, r, id, StageWeb, Fail)
				if lastFix(r) != recheck {
					t.Errorf("ManualFix = %q", r.ManualFix)
				}
			})
		}
	})

	t.Run("Layout hỏng thì chưa kiểm được, không chạm máy", func(t *testing.T) {
		bad := l
		bad.Public = "public; rm -rf /"
		f := system.NewFake()
		assertShape(t, SiteDocroot(bad).Run(bg(), f), id, StageWeb, Unknown)
		assertShape(t, SiteDocroot(site.Layout{}).Run(bg(), f), "site.docroot:", StageWeb, Unknown)
		assertNoCmd(t, f)
	})
}

// ---- DocrootGuard ----

// Vhost do template hiện tại sinh ra đạt ở cả hai web server, có SSL hay không,
// ở mọi chế độ .htaccess: template và phép kiểm không lệch nhau.
func TestDocrootGuardAcceptsTemplates(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	for _, web := range []string{"nginx", "apache"} {
		for _, ssl := range []bool{false, true} {
			for _, mode := range []site.HtaccessMode{site.HtaccessSafe, site.HtaccessOff, site.HtaccessFull} {
				body, err := templates.SiteVhost(l, web, "8.3", "/var/lib/tpan/acme", ssl, mode)
				if err != nil {
					t.Fatal(err)
				}
				f := system.NewFake()
				f.SetFile(map[string]string{"nginx": l.NginxConf, "apache": l.ApacheConf}[web], body, 0o644, system.Root)
				r := DocrootGuard(web, l).Run(bg(), f)
				assertShape(t, r, "web.docroot_guard:shop.example.com", StageWeb, Pass)
				assertNoCmd(t, f)
			}
		}
	}
}

func TestDocrootGuardNginx(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	const id = "web.docroot_guard:shop.example.com"
	run := func(lines ...string) Result {
		f := system.NewFake()
		f.SetFile(l.NginxConf, []byte(templates.Header+"\nserver {\n    root "+l.Public+";\n"+strings.Join(lines, "\n")+"\n}\n"), 0o644, system.Root)
		return DocrootGuard("nginx", l).Run(bg(), f)
	}
	if r := run("    " + templates.NginxSymlinkGuardV2); r.Status != Pass {
		t.Errorf("dòng mới phải đạt: %s", r.Summary.Vi)
	}
	for name, lines := range map[string][]string{
		"vhost cũ (from=$document_root)":       {"    " + templates.NginxSymlinkGuard},
		"không có dòng nào":                    {"    index index.php;"},
		"dòng mới chỉ nằm trong chú thích":     {"    # " + templates.NginxSymlinkGuardV2},
		"có dòng mới nhưng location tắt chặn":  {"    " + templates.NginxSymlinkGuardV2, "    location /x/ {", "        disable_symlinks off;", "    }"},
		"có dòng mới nhưng thêm dòng có from=": {"    " + templates.NginxSymlinkGuardV2, "    location /y/ {", "        " + templates.NginxSymlinkGuard, "    }"},
	} {
		t.Run(name, func(t *testing.T) {
			r := run(lines...)
			assertShape(t, r, id, StageWeb, Fail)
			if r.ManualFix[0] != hardenCmd || lastFix(r) != "grep -nE disable_symlinks "+l.NginxConf {
				t.Errorf("ManualFix = %q", r.ManualFix)
			}
			if !strings.Contains(r.Cause.Vi, "public") {
				t.Errorf("Cause phải nói về public: %q", r.Cause.Vi)
			}
		})
	}
}

func TestDocrootGuardApache(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	const id = "web.docroot_guard:shop.example.com"
	anchor := `<Directory "` + l.Dir + `">`
	vhost, err := templates.SiteVhost(l, "apache", "8.3", "/var/lib/tpan/acme", true, site.HtaccessSafe)
	if err != nil {
		t.Fatal(err)
	}
	full := string(vhost)
	// Bỏ khối neo khỏi vhost cổng 80 của bản SSL (khối neo đầu tiên của file).
	i := strings.Index(full, anchor)
	j := i + strings.Index(full[i:], "</Directory>") + len("</Directory>")
	no80 := full[:i] + full[j:]
	run := func(body string) Result {
		f := system.NewFake()
		f.SetFile(l.ApacheConf, []byte(body), 0o644, system.Root)
		r := DocrootGuard("apache", l).Run(bg(), f)
		assertNoCmd(t, f)
		return r
	}
	plain := func(dirBlocks string) string {
		return templates.Header + "\n<VirtualHost *:80>\n    ServerName shop.example.com\n" + dirBlocks +
			"    <Directory \"" + l.Public + "\">\n        Options SymLinksIfOwnerMatch\n    </Directory>\n</VirtualHost>\n"
	}

	for name, body := range map[string]string{
		"neo không nháy, chữ thường":      plain("    <directory " + l.Dir + "/>\n        options symlinksifownermatch\n    </directory>\n"),
		"khối sau ghi đè khối trước hỏng": plain("    " + anchor + "\n        Options FollowSymLinks\n    </Directory>\n    " + anchor + "\n        Options SymLinksIfOwnerMatch\n    </Directory>\n"),
	} {
		t.Run("đạt: "+name, func(t *testing.T) {
			if r := run(body); r.Status != Pass {
				t.Errorf("%s: %s", r.Status, r.Summary.Vi)
			}
		})
	}

	for _, c := range []struct{ name, body, summary string }{
		{"vhost cũ (chỉ có khối public)", plain(""), "*:80"},
		{"bản SSL thiếu khối neo ở cổng 80", no80, "*:80"},
		{"khối neo cho FollowSymLinks", plain("    " + anchor + "\n        Options FollowSymLinks\n    </Directory>\n"), "*:80"},
		{"khối neo thêm FollowSymLinks", plain("    " + anchor + "\n        Options SymLinksIfOwnerMatch FollowSymLinks\n    </Directory>\n"), "*:80"},
		{"khối sau ghi đè bằng FollowSymLinks", plain("    " + anchor + "\n        Options SymLinksIfOwnerMatch\n    </Directory>\n    " + anchor + "\n        Options +FollowSymLinks\n    </Directory>\n"), "*:80"},
		{"khối neo chỉ trong chú thích", plain("    # " + anchor + "\n    #    Options SymLinksIfOwnerMatch\n    # </Directory>\n"), "*:80"},
		{"khối cho thư mục khác cùng tiền tố", plain("    <Directory \"" + l.Dir + "-cu\">\n        Options SymLinksIfOwnerMatch\n    </Directory>\n"), "*:80"},
		{"không có VirtualHost nào", templates.Header + "\n" + anchor + "\n    Options SymLinksIfOwnerMatch\n</Directory>\n", "VirtualHost"},
	} {
		t.Run("lỗi: "+c.name, func(t *testing.T) {
			r := run(c.body)
			assertShape(t, r, id, StageWeb, Fail)
			if !strings.Contains(r.Summary.Vi, c.summary) || !strings.Contains(r.Summary.En, c.summary) {
				t.Errorf("Summary phải nêu %q: %q", c.summary, r.Summary.Vi)
			}
			if r.ManualFix[0] != hardenCmd || !strings.HasPrefix(lastFix(r), "grep -nE ") {
				t.Errorf("ManualFix = %q", r.ManualFix)
			}
		})
	}
	// Vhost cổng 443 vẫn đủ khối thì lời báo chỉ nêu vhost cổng 80.
	if r := run(no80); strings.Contains(r.Summary.Vi, "*:443") {
		t.Errorf("Summary nêu nhầm vhost 443: %q", r.Summary.Vi)
	}
}

func TestDocrootGuardUnknown(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	const id = "web.docroot_guard:shop.example.com"
	f := system.NewFake()
	assertShape(t, DocrootGuard("nginx", l).Run(bg(), f), id, StageWeb, Unknown)
	assertShape(t, DocrootGuard("apache", l).Run(bg(), f), id, StageWeb, Unknown)
	assertShape(t, DocrootGuard("caddy", l).Run(bg(), f), id, StageWeb, Unknown)
	assertShape(t, DocrootGuard("nginx", site.Layout{}).Run(bg(), f), "web.docroot_guard:", StageWeb, Unknown)
	bad := l
	bad.Dir = "/var/www/x\"y"
	assertShape(t, DocrootGuard("apache", bad).Run(bg(), f), id, StageWeb, Unknown)
	assertNoCmd(t, f)
}

// ---- PHPPool ----

func poolFixture(t *testing.T, l site.Layout, files map[string]string) *system.FakeSystem {
	t.Helper()
	f := system.NewFake()
	for ver, body := range files {
		if body == "" {
			b, err := templates.SitePool(l, ver)
			if err != nil {
				t.Fatal(err)
			}
			body = string(b)
		}
		f.SetFile(l.PoolConf(ver), []byte(body), 0o644, system.Root)
	}
	return f
}

func TestPHPPool(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	const id = "php.pool:shop.example.com"
	good, err := templates.SitePool(l, "8.3")
	if err != nil {
		t.Fatal(err)
	}
	edit := func(old, repl string) string {
		if !strings.Contains(string(good), old) {
			t.Fatalf("template không có %q", old)
		}
		return strings.Replace(string(good), old, repl, 1)
	}
	sock := l.SocketFor("8.3")

	t.Run("pool do template sinh ra thì đạt; bản PHP không có pool thì bỏ qua", func(t *testing.T) {
		f := poolFixture(t, l, map[string]string{"8.3": ""})
		r := PHPPool(l, []string{"8.3", "8.4"}).Run(bg(), f)
		assertShape(t, r, id, StagePHP, Pass)
		assertNoCmd(t, f)
		if len(r.Evidence) != 2 || !strings.Contains(r.Evidence[0].Output, "listen = "+sock) || r.Evidence[1].ExitCode != 2 {
			t.Errorf("Evidence = %+v", r.Evidence)
		}
	})
	t.Run("giá trị trong nháy và chú thích cuối dòng vẫn đạt", func(t *testing.T) {
		body := edit("listen = "+sock, `listen = "`+sock+`" ; socket`)
		r := PHPPool(l, []string{"8.3"}).Run(bg(), poolFixture(t, l, map[string]string{"8.3": body}))
		assertShape(t, r, id, StagePHP, Pass)
	})
	t.Run("hai bản PHP trong lúc đổi bản đều đúng thì đạt", func(t *testing.T) {
		r := PHPPool(l, []string{"8.3", "8.4"}).Run(bg(), poolFixture(t, l, map[string]string{"8.3": "", "8.4": ""}))
		assertShape(t, r, id, StagePHP, Pass)
	})

	for _, c := range []struct{ name, body, summary string }{
		{"listen là địa chỉ TCP", edit("listen = "+sock, "listen = 127.0.0.1:9001"), "TCP"},
		{"listen là socket của bản PHP khác", edit("listen = "+sock, "listen = "+l.SocketFor("8.4")), "listen"},
		{"user bị đổi", edit("user = "+l.User, "user = www-data"), "user"},
		{"group bị đổi", edit("group = "+l.User, "group = www-data"), "group"},
		{"listen.mode rộng hơn", edit("listen.mode = 0660", "listen.mode = 0666"), "listen.mode"},
		{"listen.owner bị đổi", edit("listen.owner = www-data", "listen.owner = "+l.User), "listen.owner"},
		{"có listen.acl_users", string(good) + "listen.acl_users = tp_khac\n", "listen.acl_users"},
		{"có listen.acl_groups", string(good) + "listen.acl_groups = tp_khac\n", "listen.acl_groups"},
		{"dòng sau đè dòng đúng", string(good) + "listen = 0.0.0.0:9000\n", "TCP"},
		{"khối pool thứ hai thiếu listen", string(good) + "\n[khac]\nuser = " + l.User + "\n", "[khac]"},
		{"file không có khối pool nào", "; trống\n", "pool"},
	} {
		t.Run("lỗi: "+c.name, func(t *testing.T) {
			r := PHPPool(l, []string{"8.3"}).Run(bg(), poolFixture(t, l, map[string]string{"8.3": c.body}))
			assertShape(t, r, id, StagePHP, Fail)
			if !strings.Contains(r.Summary.Vi, c.summary) {
				t.Errorf("Summary phải nêu %q: %q", c.summary, r.Summary.Vi)
			}
			if r.ManualFix[0] != "sudo tpan health --fix" || !strings.HasPrefix(lastFix(r), "grep -nE ") {
				t.Errorf("ManualFix = %q", r.ManualFix)
			}
		})
	}
	t.Run("file ở bản 8.4 hỏng dù bản 8.3 đúng thì vẫn lỗi", func(t *testing.T) {
		bad := strings.Replace(string(good), "listen = "+sock, "listen = 127.0.0.1:9001", 1)
		f := poolFixture(t, l, map[string]string{"8.3": "", "8.4": bad})
		assertShape(t, PHPPool(l, []string{"8.3", "8.4"}).Run(bg(), f), id, StagePHP, Fail)
	})

	t.Run("chưa kiểm được", func(t *testing.T) {
		f := system.NewFake()
		assertShape(t, PHPPool(l, []string{"8.3"}).Run(bg(), f), id, StagePHP, Unknown)
		assertShape(t, PHPPool(l, nil).Run(bg(), f), id, StagePHP, Unknown)
		for _, v := range []string{"8", "8.3; rm -rf /", "../../etc", "$(id)"} {
			assertShape(t, PHPPool(l, []string{v}).Run(bg(), f), id, StagePHP, Unknown)
		}
		bad := l
		bad.Pool = "tpan-x/../y"
		assertShape(t, PHPPool(bad, []string{"8.3"}).Run(bg(), f), id, StagePHP, Unknown)
		assertNoCmd(t, f)
		// Đọc lỗi (ở đây: đường dẫn là thư mục) thì chưa kiểm được, không phải đạt.
		f.SetDir(l.PoolConf("8.3"), 0o755, system.Root)
		assertShape(t, PHPPool(l, []string{"8.3"}).Run(bg(), f), id, StagePHP, Unknown)
	})
}

// ---- ApacheModStatus ----

const apacheModules = "Loaded Modules:\n core_module (static)\n so_module (static)\n mpm_event_module (shared)\n proxy_fcgi_module (shared)\n"

func TestApacheModStatus(t *testing.T) {
	const id = "web.mod_status:apache2"
	run := func(code int, stdout, stderr string) (Result, *system.FakeSystem) {
		f := system.NewFake()
		reply(f, []string{"apachectl", "-M"}, code, stdout, stderr)
		return ApacheModStatus().Run(bg(), f), f
	}

	r, f := run(0, apacheModules, "AH00558: apache2: Could not reliably determine the server's fully qualified domain name")
	assertShape(t, r, id, StageWeb, Pass)
	assertEvidenceIsRan(t, r, f)
	if ran := f.Ran(); len(ran) != 1 || ran[0].String() != "apachectl -M" || ran[0].AsUser != "" {
		t.Errorf("lệnh = %v", ran)
	}

	r, _ = run(0, apacheModules+" status_module (shared)\n", "")
	assertShape(t, r, id, StageWeb, Fail)
	if r.ManualFix[0] != hardenCmd || lastFix(r) != "apachectl -M" || !strings.Contains(r.Cause.Vi, "server-status") {
		t.Errorf("ManualFix = %q, Cause = %q", r.ManualFix, r.Cause.Vi)
	}

	// Tên gần giống không phải status_module.
	r, _ = run(0, apacheModules+" proxy_status_module (shared)\n", "")
	assertShape(t, r, id, StageWeb, Pass)

	r, _ = run(1, "", "AH00526: Syntax error on line 12 of /etc/apache2/sites-enabled/x.conf")
	assertShape(t, r, id, StageWeb, Unknown)
	r, _ = run(0, "", "")
	assertShape(t, r, id, StageWeb, Unknown)
	assertShape(t, ApacheModStatus().Run(cancelledCtx(), system.NewFake()), id, StageWeb, Unknown)
}

// ---- DBUserLimit ----

func dbLimitFixture(code int, stdout, stderr string) *system.FakeSystem {
	f := system.NewFake()
	reply(f, []string{"mariadb", "-N", "-B"}, code, stdout, stderr)
	return f
}

func TestDBUserLimit(t *testing.T) {
	const user = "tp_shop_example_com_1a2b3c"
	const id = "db.user_limit:" + user
	run := func(stdout string) Result {
		return DBUserLimit(user).Run(bg(), dbLimitFixture(0, stdout, ""))
	}

	t.Run("câu SQL qua stdin, dựng từ tên, không có mật khẩu", func(t *testing.T) {
		f := dbLimitFixture(0, "limit\t20\nmax\t151\n", "")
		r := DBUserLimit(user).Run(bg(), f)
		assertShape(t, r, id, StageDB, Pass)
		assertEvidenceIsRan(t, r, f)
		ran := f.Ran()
		if len(ran) != 1 || ran[0].String() != "mariadb -N -B" || ran[0].AsUser != "" {
			t.Fatalf("lệnh = %v", ran)
		}
		sql := string(ran[0].Stdin)
		for _, want := range []string{"max_user_connections", "mysql.user", "User='" + user + "'", "Host='localhost'", "@@GLOBAL.max_connections"} {
			if !strings.Contains(sql, want) {
				t.Errorf("SQL thiếu %q: %s", want, sql)
			}
		}
		if strings.Contains(strings.ToUpper(sql), "PASSWORD") || strings.Contains(strings.ToUpper(sql), "IDENTIFIED") {
			t.Errorf("SQL không được chạm mật khẩu: %s", sql)
		}
	})
	t.Run("trần bằng max_connections vẫn đạt; trần admin đã nâng vẫn đạt", func(t *testing.T) {
		assertShape(t, run("limit\t151\nmax\t151\n"), id, StageDB, Pass)
		assertShape(t, run("limit\t50\nmax\t500\n"), id, StageDB, Pass)
	})

	for _, c := range []struct{ name, out, summary, fix0 string }{
		{"không có trần (0)", "limit\t0\nmax\t151\n", "không có trần", hardenCmd},
		{"trần lớn hơn max_connections", "limit\t200\nmax\t151\n", "151", `mariadb -e "ALTER USER '` + user + `'@'localhost' WITH MAX_USER_CONNECTIONS 20;"`},
		{"user không có", "max\t151\n", "không có", `mariadb -e "SELECT User, Host FROM mysql.user WHERE User='` + user + `'"`},
	} {
		t.Run("cảnh báo: "+c.name, func(t *testing.T) {
			r := run(c.out)
			assertShape(t, r, id, StageDB, Warn)
			if !strings.Contains(r.Summary.Vi, c.summary) {
				t.Errorf("Summary phải nêu %q: %q", c.summary, r.Summary.Vi)
			}
			if r.ManualFix[0] != c.fix0 || !strings.HasPrefix(lastFix(r), "mariadb -N -B -e ") {
				t.Errorf("ManualFix = %q", r.ManualFix)
			}
		})
	}

	t.Run("chưa kiểm được", func(t *testing.T) {
		assertShape(t, run("limit\t20\n"), id, StageDB, Unknown)
		assertShape(t, run("limit\tabc\nmax\t151\n"), id, StageDB, Unknown)
		assertShape(t, DBUserLimit(user).Run(bg(), dbLimitFixture(1, "", "ERROR 2002 (HY000): Can't connect to local server through socket '/run/mysqld/mysqld.sock' (2)")), id, StageDB, Unknown)
		assertShape(t, DBUserLimit(user).Run(cancelledCtx(), system.NewFake()), id, StageDB, Unknown)
	})

	t.Run("tên lạ bị từ chối trước khi chạy lệnh", func(t *testing.T) {
		for _, bad := range []string{"", "Tp_Upper", "a'; DROP USER root; --", "a b", "$(id)", "../../etc/passwd", "tp\nx", strings.Repeat("a", 65), "tên_có_dấu"} {
			f := system.NewFake()
			r := DBUserLimit(bad).Run(bg(), f)
			if r.Status != Unknown {
				t.Errorf("%q: %s", bad, r.Status)
			}
			assertNoCmd(t, f)
		}
	})
}

// ---- Chỉ đọc ----

func TestM28ChecksOnlyRead(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	f := poolFixture(t, l, map[string]string{"8.3": ""})
	f.SetSite(l.Dir, l.User)
	for _, web := range []string{"nginx", "apache"} {
		body, err := templates.SiteVhost(l, web, "8.3", "/var/lib/tpan/acme", true, site.HtaccessSafe)
		if err != nil {
			t.Fatal(err)
		}
		f.SetFile(map[string]string{"nginx": l.NginxConf, "apache": l.ApacheConf}[web], body, 0o644, system.Root)
	}
	reply(f, []string{"apachectl", "-M"}, 0, apacheModules, "")
	reply(f, []string{"mariadb", "-N", "-B"}, 0, "limit\t20\nmax\t151\n", "")
	before := f.Snapshot()
	rs := RunAll(bg(), f, []Check{
		SiteDocroot(l), DocrootGuard("nginx", l), DocrootGuard("apache", l),
		PHPPool(l, []string{"8.3", "8.4"}), ApacheModStatus(), DBUserLimit(l.DB),
	})
	for _, r := range rs {
		if r.Status != Pass {
			t.Errorf("%s: %s: %s", r.ID, r.Status, r.Summary.Vi)
		}
	}
	for _, c := range f.Ran() {
		if c.Name != "apachectl" && c.Name != "mariadb" {
			t.Errorf("lệnh không mong đợi: %s", c)
		}
	}
	after := f.Snapshot()
	if len(before) != len(after) {
		t.Fatalf("hệ thống file đổi: %d -> %d mục", len(before), len(after))
	}
	for p, e := range before {
		if after[p] != e {
			t.Errorf("%s đổi: %+v -> %+v", p, e, after[p])
		}
	}
}
