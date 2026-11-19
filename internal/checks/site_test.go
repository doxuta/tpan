package checks

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
	"github.com/doxuta/tpan/internal/validate"
)

// Output mẫu của wp, curl, mariadb trong file này viết theo tài liệu của từng lệnh
// (developer.wordpress.org/cli/commands, curl.se/docs/manpage.html), chưa lấy từ
// máy ảo thật: phần này nằm trong mục "cần kiểm trên máy ảo".

func siteLayout(t *testing.T, domain string) site.Layout {
	t.Helper()
	d, err := validate.ParseDomain(domain)
	if err != nil {
		t.Fatal(err)
	}
	return site.LayoutOf(d)
}

var wwwData = system.Owner{User: "www-data", Group: "www-data"}

// ---- VhostEnabled ----

// vhostFixture dựng máy có vhost đúng cho web server web.
func vhostFixture(l site.Layout, web string) *system.FakeSystem {
	conf, link := l.NginxConf, l.NginxLink
	if web == "apache" {
		conf, link = l.ApacheConf, l.ApacheLink
	}
	f := system.NewFake()
	f.SetFile(conf, []byte(templates.Header+"\nserver {}\n"), 0o644, system.Root)
	f.SetSymlink(conf, link)
	return f
}

func TestVhostEnabledPass(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	for _, web := range []string{"nginx", "apache"} {
		t.Run(web, func(t *testing.T) {
			f := vhostFixture(l, web)
			r := VhostEnabled(web, l).Run(bg(), f)
			assertShape(t, r, "web.vhost:shop.example.com", StageWeb, Pass)
			assertNoCmd(t, f)
			if len(r.Evidence) != 3 {
				t.Errorf("Evidence = %+v", r.Evidence)
			}
		})
	}
	t.Run("symlink tương đối", func(t *testing.T) {
		f := system.NewFake()
		f.SetFile(l.NginxConf, []byte(templates.Header+"\n"), 0o644, system.Root)
		f.SetSymlink("../sites-available/tpan-shop.example.com.conf", l.NginxLink)
		assertShape(t, VhostEnabled("nginx", l).Run(bg(), f), "web.vhost:shop.example.com", StageWeb, Pass)
	})
}

func TestVhostEnabledFail(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	run := func(f *system.FakeSystem) Result {
		r := VhostEnabled("nginx", l).Run(bg(), f)
		assertShape(t, r, "web.vhost:shop.example.com", StageWeb, Fail)
		return r
	}
	t.Run("không có file vhost", func(t *testing.T) {
		r := run(system.NewFake())
		if lastFix(r) != "ls -l "+l.NginxConf+" "+l.NginxLink || r.ManualFix[0] != "tpan site add shop.example.com" {
			t.Errorf("ManualFix = %q", r.ManualFix)
		}
	})
	t.Run("vhost là thư mục", func(t *testing.T) {
		f := system.NewFake()
		f.SetDir(l.NginxConf, 0o755, system.Root)
		run(f)
	})
	t.Run("mất dòng đầu của tpan", func(t *testing.T) {
		f := vhostFixture(l, "nginx")
		f.SetFile(l.NginxConf, []byte("server {}\n"), 0o644, system.Root)
		r := run(f)
		if !strings.Contains(r.Summary.Vi, "dòng đầu") || lastFix(r) != "head -n 1 "+l.NginxConf {
			t.Errorf("Summary = %q, ManualFix = %q", r.Summary.Vi, r.ManualFix)
		}
	})
	t.Run("chưa bật (không có symlink)", func(t *testing.T) {
		f := system.NewFake()
		f.SetFile(l.NginxConf, []byte(templates.Header+"\n"), 0o644, system.Root)
		r := run(f)
		if r.ManualFix[0] != "ln -s "+l.NginxConf+" "+l.NginxLink || !strings.Contains(r.Summary.Vi, "chưa được bật") {
			t.Errorf("Summary = %q, ManualFix = %q", r.Summary.Vi, r.ManualFix)
		}
		// Hai lệnh kế tiếp là kiểm cú pháp rồi reload, đúng thứ tự của mục 4.2.
		if r.ManualFix[1] != "nginx -t" || r.ManualFix[2] != "systemctl reload nginx" {
			t.Errorf("ManualFix = %q", r.ManualFix)
		}
	})
	t.Run("sites-enabled là file thường", func(t *testing.T) {
		f := system.NewFake()
		f.SetFile(l.NginxConf, []byte(templates.Header+"\n"), 0o644, system.Root)
		f.SetFile(l.NginxLink, []byte(templates.Header+"\n"), 0o644, system.Root)
		r := run(f)
		if !strings.Contains(r.Summary.Vi, "không phải symlink") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
	})
	t.Run("symlink trỏ sai", func(t *testing.T) {
		f := system.NewFake()
		f.SetFile(l.NginxConf, []byte(templates.Header+"\n"), 0o644, system.Root)
		f.SetFile("/etc/nginx/sites-available/khac.conf", []byte("x"), 0o644, system.Root)
		f.SetSymlink("/etc/nginx/sites-available/khac.conf", l.NginxLink)
		r := run(f)
		if !strings.Contains(r.Summary.Vi, "khac.conf") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
	})
	t.Run("apache dùng đường dẫn của apache", func(t *testing.T) {
		r := VhostEnabled("apache", l).Run(bg(), system.NewFake())
		assertShape(t, r, "web.vhost:shop.example.com", StageWeb, Fail)
		if !strings.Contains(r.Summary.Vi, l.ApacheConf) || r.ManualFix[1] != "apachectl configtest" {
			t.Errorf("Summary = %q, ManualFix = %q", r.Summary.Vi, r.ManualFix)
		}
	})
}

func TestVhostEnabledUnknown(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	t.Run("web server lạ", func(t *testing.T) {
		f := system.NewFake()
		assertShape(t, VhostEnabled("caddy; rm -rf /", l).Run(bg(), f), "web.vhost:shop.example.com", StageWeb, Unknown)
		assertNoCmd(t, f)
	})
	t.Run("Layout zero", func(t *testing.T) {
		r := VhostEnabled("nginx", site.Layout{}).Run(bg(), system.NewFake())
		assertShape(t, r, "web.vhost:", StageWeb, Unknown)
	})
}

// ---- PoolSocket ----

func socketFixture(l site.Layout, owner system.Owner, mode fs.FileMode) *system.FakeSystem {
	f := system.NewFake()
	f.SetSocket(l.Socket, mode, owner)
	return f
}

func TestPoolSocketPass(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	s := socketFixture(l, wwwData, 0o660)
	r := PoolSocket(l).Run(bg(), s)
	assertShape(t, r, "php.socket:shop.example.com", StagePHP, Pass)
	assertNoCmd(t, s)
}

func TestPoolSocketFail(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	run := func(s system.System) Result {
		r := PoolSocket(l).Run(bg(), s)
		assertShape(t, r, "php.socket:shop.example.com", StagePHP, Fail)
		if lastFix(r) != "ls -l "+l.Socket {
			t.Errorf("ManualFix cuối = %q", lastFix(r))
		}
		return r
	}
	t.Run("không có socket", func(t *testing.T) {
		r := run(system.NewFake())
		if !strings.HasPrefix(r.ManualFix[0], "ls -l /etc/php/*/fpm/pool.d/tpan-") {
			t.Errorf("ManualFix[0] = %q: dấu * phải để trần cho shell mở rộng", r.ManualFix[0])
		}
		if r.ManualFix[2] != "tpan site add shop.example.com" {
			t.Errorf("ManualFix = %q", r.ManualFix)
		}
	})
	t.Run("file thường chứ không phải socket", func(t *testing.T) {
		f := system.NewFake()
		f.SetFile(l.Socket, nil, 0o660, wwwData)
		r := run(f)
		if !strings.Contains(r.Summary.Vi, "không phải socket") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
	})
	t.Run("sai chủ", func(t *testing.T) {
		r := run(socketFixture(l, system.Owner{User: l.User, Group: l.User}, 0o660))
		if !strings.Contains(r.Summary.Vi, l.User+":"+l.User) || strings.Contains(r.Summary.Vi, "quyền") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
	})
	t.Run("sai quyền", func(t *testing.T) {
		r := run(socketFixture(l, wwwData, 0o666))
		if !strings.Contains(r.Summary.Vi, "0666") || strings.Contains(r.Summary.Vi, "chủ") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
	})
	t.Run("sai cả hai", func(t *testing.T) {
		r := run(socketFixture(l, system.Root, 0o600))
		if !strings.Contains(r.Summary.Vi, "chủ") || !strings.Contains(r.Summary.Vi, "quyền") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
	})
}

func TestPoolSocketUnknown(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	l.Pool = "tpan-x; rm -rf /"
	assertShape(t, PoolSocket(l).Run(bg(), system.NewFake()), "php.socket:shop.example.com", StagePHP, Unknown)
	assertShape(t, PoolSocket(site.Layout{}).Run(bg(), system.NewFake()), "php.socket:", StagePHP, Unknown)
}

// ---- WPChecksums ----

func TestWPChecksumsCommand(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	f := system.NewFake()
	reply(f, []string{"wp"}, 0, "Success: WordPress installation verifies against checksums.\n", "")
	r := WPChecksums(l).Run(bg(), f)
	assertShape(t, r, "wp.checksums:shop.example.com", StageWordPress, Pass)
	assertEvidenceIsRan(t, r, f)
	ran := f.Ran()
	if len(ran) != 1 || ran[0].Name != "wp" || ran[0].AsUser != l.User {
		t.Fatalf("lệnh đã chạy = %+v", ran)
	}
	want := []string{"core", "verify-checksums", "--path=" + l.Public}
	if strings.Join(ran[0].Args, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("Args = %q, muốn %q", ran[0].Args, want)
	}
}

func TestWPChecksumsFail(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	t.Run("file không khớp", func(t *testing.T) {
		f := system.NewFake()
		reply(f, []string{"wp"}, 1, "",
			"Warning: File doesn't verify against checksum: wp-includes/version.php\n"+
				"Warning: File doesn't verify against checksum: wp-admin/index.php\n"+
				"Error: WordPress installation doesn't verify against checksums.\n")
		r := WPChecksums(l).Run(bg(), f)
		assertShape(t, r, "wp.checksums:shop.example.com", StageWordPress, Fail)
		if !strings.Contains(r.Summary.Vi, "2 file") || !strings.Contains(r.Summary.Vi, "wp-includes/version.php") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
		recheck := f.Ran()[0].String()
		if lastFix(r) != recheck || r.ManualFix[0] != recheck {
			t.Errorf("ManualFix = %q", r.ManualFix)
		}
		wantDL := "runuser -u " + l.User + " -- wp core download --force --skip-content --path=" + l.Public +
			` --version="$(runuser -u ` + l.User + " -- wp core version --path=" + l.Public + `)"`
		if r.ManualFix[1] != wantDL {
			t.Errorf("ManualFix[1] = %q, muốn %q", r.ManualFix[1], wantDL)
		}
		for _, fix := range r.ManualFix {
			if strings.ContainsAny(fix, "<>") {
				t.Errorf("ManualFix có chỗ trống phải điền tay: %q", fix)
			}
		}
	})
	t.Run("không có WordPress", func(t *testing.T) {
		f := system.NewFake()
		reply(f, []string{"wp"}, 1, "", "Error: This does not seem to be a WordPress installation.\nPass --path=`path/to/wordpress` or run `wp core download`.\n")
		assertShape(t, WPChecksums(l).Run(bg(), f), "wp.checksums:shop.example.com", StageWordPress, Fail)
	})
}

func TestWPChecksumsUnknown(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	t.Run("không tải được checksum", func(t *testing.T) {
		f := system.NewFake()
		reply(f, []string{"wp"}, 1, "", "Error: Failed to get checksums: could not resolve host: api.wordpress.org\n")
		r := WPChecksums(l).Run(bg(), f)
		assertShape(t, r, "wp.checksums:shop.example.com", StageWordPress, Unknown)
		if !strings.Contains(r.Summary.Vi, "api.wordpress.org") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
	})
	t.Run("runuser không tìm thấy wp", func(t *testing.T) {
		f := system.NewFake()
		reply(f, []string{"wp"}, 127, "", "runuser: failed to execute wp: No such file or directory\n")
		assertShape(t, WPChecksums(l).Run(bg(), f), "wp.checksums:shop.example.com", StageWordPress, Unknown)
	})
	t.Run("lệnh không chạy được", func(t *testing.T) {
		assertShape(t, WPChecksums(l).Run(cancelledCtx(), system.NewFake()), "wp.checksums:shop.example.com", StageWordPress, Unknown)
	})
}

func TestWPChecksumsRejectsBadLayout(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	bad := []site.Layout{{}, withUser(l, "-oProxyCommand=x"), withUser(l, "tp_a; id"), withUser(l, ""), withUser(l, "root\nx")}
	for _, p := range []string{"public", "/var/www/a b/public", "/var/www/$(id)/public", "/var/www/a;id", "/var/www/a\nb", "/var/www/\"x\""} {
		rel := l
		rel.Public = p
		bad = append(bad, rel)
	}
	for _, b := range bad {
		f := system.NewFake()
		r := WPChecksums(b).Run(bg(), f)
		if r.Status != Unknown {
			t.Errorf("Layout %+v: Status = %q, muốn unknown", b, r.Status)
		}
		assertNoCmd(t, f)
	}
}

func withUser(l site.Layout, u string) site.Layout {
	l.User = u
	return l
}

// ---- WPConfigPerm ----

func TestWPConfigPerm(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	owner := system.Owner{User: l.User, Group: l.User}
	id := "wp.config:shop.example.com"

	t.Run("đạt", func(t *testing.T) {
		f := system.NewFake()
		f.SetFile(l.WPConfig, []byte("<?php"), 0o600, owner)
		r := WPConfigPerm(l).Run(bg(), f)
		assertShape(t, r, id, StageWordPress, Pass)
		assertNoCmd(t, f)
	})
	t.Run("chủ khác nhóm vẫn đạt (đặc tả chỉ đòi đúng user)", func(t *testing.T) {
		f := system.NewFake()
		f.SetFile(l.WPConfig, []byte("<?php"), 0o600, system.Owner{User: l.User, Group: "www-data"})
		assertShape(t, WPConfigPerm(l).Run(bg(), f), id, StageWordPress, Pass)
	})
	t.Run("không có file", func(t *testing.T) {
		r := WPConfigPerm(l).Run(bg(), system.NewFake())
		assertShape(t, r, id, StageWordPress, Fail)
		if lastFix(r) != "ls -l "+l.WPConfig {
			t.Errorf("ManualFix = %q", r.ManualFix)
		}
	})
	t.Run("quyền 0644", func(t *testing.T) {
		f := system.NewFake()
		f.SetFile(l.WPConfig, []byte("<?php"), 0o644, owner)
		r := WPConfigPerm(l).Run(bg(), f)
		assertShape(t, r, id, StageWordPress, Fail)
		if !strings.Contains(r.Summary.Vi, "0644") || strings.Contains(r.Summary.Vi, "chủ") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
		if r.ManualFix[1] != "chmod 600 "+l.WPConfig {
			t.Errorf("ManualFix = %q", r.ManualFix)
		}
	})
	t.Run("chủ là root", func(t *testing.T) {
		f := system.NewFake()
		f.SetFile(l.WPConfig, []byte("<?php"), 0o600, system.Root)
		r := WPConfigPerm(l).Run(bg(), f)
		assertShape(t, r, id, StageWordPress, Fail)
		if !strings.Contains(r.Summary.Vi, "chủ là root") || r.ManualFix[0] != "chown "+l.User+" "+l.WPConfig {
			t.Errorf("Summary = %q, ManualFix = %q", r.Summary.Vi, r.ManualFix)
		}
	})
	t.Run("chủ của site khác", func(t *testing.T) {
		f := system.NewFake()
		f.SetFile(l.WPConfig, []byte("<?php"), 0o600, system.Owner{User: "tp_khac", Group: "tp_khac"})
		assertShape(t, WPConfigPerm(l).Run(bg(), f), id, StageWordPress, Fail)
	})
	t.Run("là symlink", func(t *testing.T) {
		f := system.NewFake()
		f.SetFile("/tmp/x", []byte("<?php"), 0o600, owner)
		f.SetSymlink("/tmp/x", l.WPConfig)
		assertShape(t, WPConfigPerm(l).Run(bg(), f), id, StageWordPress, Fail)
	})
	t.Run("Layout zero", func(t *testing.T) {
		assertShape(t, WPConfigPerm(site.Layout{}).Run(bg(), system.NewFake()), "wp.config:", StageWordPress, Unknown)
	})
}

// ---- DBAccess ----

func TestDBAccessCommand(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	f := system.NewFake()
	reply(f, []string{"wp"}, 0, "information_schema\n"+l.DB+"\n", "")
	r := DBAccess(l).Run(bg(), f)
	assertShape(t, r, "db.access:shop.example.com", StageDB, Pass)
	assertEvidenceIsRan(t, r, f)
	ran := f.Ran()
	want := []string{"db", "query", "SHOW DATABASES", "--skip-column-names", "--path=" + l.Public}
	if len(ran) != 1 || ran[0].AsUser != l.User || strings.Join(ran[0].Args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("lệnh đã chạy = %+v", ran)
	}
	if len(ran[0].Stdin) != 0 {
		t.Errorf("không được đưa gì qua stdin: %q", ran[0].Stdin)
	}
}

func TestDBAccessFail(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	id := "db.access:shop.example.com"
	t.Run("thấy DB của site khác", func(t *testing.T) {
		f := system.NewFake()
		reply(f, []string{"wp"}, 0, "information_schema\n"+l.DB+"\ntp_khac_123456\nmysql\n", "")
		r := DBAccess(l).Run(bg(), f)
		assertShape(t, r, id, StageDB, Fail)
		if !strings.Contains(r.Summary.Vi, "tp_khac_123456") || !strings.Contains(r.Summary.Vi, "mysql") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
		if want := "mariadb -e \"SHOW GRANTS FOR '" + l.DB + "'@'localhost'\""; r.ManualFix[0] != want {
			t.Errorf("ManualFix[0] = %q, muốn %q", r.ManualFix[0], want)
		}
		if lastFix(r) != f.Ran()[0].String() {
			t.Errorf("dòng cuối phải là lệnh kiểm lại: %q", lastFix(r))
		}
	})
	t.Run("không thấy DB của chính nó", func(t *testing.T) {
		f := system.NewFake()
		reply(f, []string{"wp"}, 0, "information_schema\n", "")
		r := DBAccess(l).Run(bg(), f)
		assertShape(t, r, id, StageDB, Fail)
		if !strings.Contains(r.Summary.Vi, l.DB) {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
	})
	t.Run("sai mật khẩu", func(t *testing.T) {
		f := system.NewFake()
		reply(f, []string{"wp"}, 1, "", "ERROR 1045 (28000): Access denied for user '"+l.DB+"'@'localhost' (using password: YES)\n")
		r := DBAccess(l).Run(bg(), f)
		assertShape(t, r, id, StageDB, Fail)
		if !strings.Contains(r.Summary.Vi, "ERROR 1045") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
		// Gợi ý đọc wp-config.php chỉ được lộ tên DB và tên user, không lộ dòng mật khẩu.
		for _, fix := range r.ManualFix {
			if strings.Contains(fix, "cat ") || strings.Contains(fix, "DB_PASSWORD") {
				t.Errorf("ManualFix có thể lộ mật khẩu: %q", fix)
			}
		}
	})
	t.Run("DB không còn", func(t *testing.T) {
		f := system.NewFake()
		reply(f, []string{"wp"}, 1, "", "ERROR 1049 (42000): Unknown database '"+l.DB+"'\n")
		assertShape(t, DBAccess(l).Run(bg(), f), id, StageDB, Fail)
	})
}

func TestDBAccessUnknown(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	id := "db.access:shop.example.com"
	t.Run("không có mysql client", func(t *testing.T) {
		f := system.NewFake()
		reply(f, []string{"wp"}, 1, "", "Error: 'mysql' binary not found.\n")
		assertShape(t, DBAccess(l).Run(bg(), f), id, StageDB, Unknown)
	})
	t.Run("lệnh không chạy được", func(t *testing.T) {
		assertShape(t, DBAccess(l).Run(cancelledCtx(), system.NewFake()), id, StageDB, Unknown)
	})
	t.Run("tên DB lạ", func(t *testing.T) {
		bad := l
		bad.DB = "tp_a' OR '1'='1"
		f := system.NewFake()
		assertShape(t, DBAccess(bad).Run(bg(), f), id, StageDB, Unknown)
		assertNoCmd(t, f)
	})
	t.Run("Layout zero", func(t *testing.T) {
		assertShape(t, DBAccess(site.Layout{}).Run(bg(), system.NewFake()), "db.access:", StageDB, Unknown)
	})
}

// ---- Isolated ----

func TestIsolated(t *testing.T) {
	a := siteLayout(t, "shop.example.com")
	b := siteLayout(t, "blog.example.org")
	id := "isolation:shop.example.com:blog.example.org"
	withConfig := func() *system.FakeSystem {
		f := system.NewFake()
		f.SetFile(b.WPConfig, []byte("<?php"), 0o600, system.Owner{User: b.User, Group: b.User})
		return f
	}

	t.Run("bị từ chối thì đạt", func(t *testing.T) {
		f := withConfig()
		reply(f, []string{"test"}, 1, "", "")
		r := Isolated(a, b).Run(bg(), f)
		assertShape(t, r, id, StageWordPress, Pass)
		ran := f.Ran()
		if len(ran) != 1 || ran[0].AsUser != a.User || strings.Join(ran[0].Args, " ") != "-r "+b.WPConfig {
			t.Fatalf("lệnh đã chạy = %+v", ran)
		}
		if len(r.Evidence) != 2 || r.Evidence[1].Command != ran[0].String() {
			t.Errorf("Evidence = %+v", r.Evidence)
		}
	})
	t.Run("đọc được thì lỗi", func(t *testing.T) {
		f := withConfig()
		reply(f, []string{"test"}, 0, "", "")
		r := Isolated(a, b).Run(bg(), f)
		assertShape(t, r, id, StageWordPress, Fail)
		if lastFix(r) != f.Ran()[0].String() {
			t.Errorf("ManualFix cuối = %q", lastFix(r))
		}
		if !strings.Contains(strings.Join(r.ManualFix, "\n"), "chmod 2750 "+b.Dir+" "+b.Public) {
			t.Errorf("ManualFix = %q", r.ManualFix)
		}
	})
	t.Run("user không tồn tại không được tính là bị từ chối", func(t *testing.T) {
		f := withConfig()
		reply(f, []string{"test"}, 1, "", "runuser: user tp_x does not exist or the user entry does not contain all the required fields\n")
		assertShape(t, Isolated(a, b).Run(bg(), f), id, StageWordPress, Unknown)
	})
	t.Run("site kia chưa có wp-config.php thì không chứng minh được gì", func(t *testing.T) {
		f := system.NewFake()
		reply(f, []string{"test"}, 1, "", "")
		assertShape(t, Isolated(a, b).Run(bg(), f), id, StageWordPress, Unknown)
		assertNoCmd(t, f)
	})
	t.Run("lệnh không chạy được", func(t *testing.T) {
		assertShape(t, Isolated(a, b).Run(cancelledCtx(), withConfig()), id, StageWordPress, Unknown)
	})
	t.Run("hai site là một", func(t *testing.T) {
		f := withConfig()
		r := Isolated(b, b).Run(bg(), f)
		assertShape(t, r, "isolation:blog.example.org:blog.example.org", StageWordPress, Unknown)
		assertNoCmd(t, f)
	})
	t.Run("Layout zero và user lạ", func(t *testing.T) {
		f := withConfig()
		assertShape(t, Isolated(site.Layout{}, b).Run(bg(), f), "isolation::blog.example.org", StageWordPress, Unknown)
		assertShape(t, Isolated(withUser(a, "-x"), b).Run(bg(), f), id, StageWordPress, Unknown)
		assertNoCmd(t, f)
	})
}

// ---- HTTPSite ----

func httpOut(body, code string) string { return body + "TPAN_HTTP_CODE=" + code }

const wpPage = `<!DOCTYPE html><html><head><title>Cửa hàng</title>` +
	`<link rel="stylesheet" href="http://shop.example.com/wp-content/themes/x/style.css"></head><body>Xin chào</body></html>`

func TestHTTPSiteCommand(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	f := system.NewFake()
	reply(f, []string{"curl"}, 0, httpOut(wpPage, "200"), "")
	r := HTTPSite(l).Run(bg(), f)
	assertShape(t, r, "http.site:shop.example.com", StageEndToEnd, Pass)
	ran := f.Ran()
	if len(ran) != 1 || ran[0].Name != "curl" || ran[0].AsUser != "" {
		t.Fatalf("lệnh đã chạy = %+v", ran)
	}
	args := strings.Join(ran[0].Args, " ")
	for _, want := range []string{"-sS", "--max-time 15", "--resolve shop.example.com:80:127.0.0.1", "http://shop.example.com/"} {
		if !strings.Contains(args, want) {
			t.Errorf("Args %q thiếu %q", args, want)
		}
	}
	if strings.Contains(args, " -L") || strings.Contains(args, "-f ") {
		t.Errorf("không được theo chuyển hướng hay dùng -f: %q", args)
	}
	if len(r.Evidence) != 1 || r.Evidence[0].Command != ran[0].String() || !strings.HasPrefix(r.Evidence[0].Output, "HTTP 200,") {
		t.Errorf("Evidence = %+v", r.Evidence)
	}
}

func TestHTTPSiteCases(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	id := "http.site:shop.example.com"
	run := func(code int, stdout, stderr string) (Result, *system.FakeSystem) {
		f := system.NewFake()
		reply(f, []string{"curl"}, code, stdout, stderr)
		return HTTPSite(l).Run(bg(), f), f
	}
	recheckPrefix := "curl -sS --max-time 15 --resolve shop.example.com:80:127.0.0.1 -o /dev/null"

	cases := []struct {
		name     string
		exit     int
		stdout   string
		stderr   string
		want     Status
		summary  string // đoạn phải có trong Summary.Vi
		fixLines []string
	}{
		{"200 và là WordPress của site", 0, httpOut(wpPage, "200"), "", Pass, "200", nil},
		{"dấu hiệu qua thẻ generator", 0, httpOut(`<meta name="generator" content="WordPress 6.8"><a href="//shop.example.com">`, "200"), "", Pass, "200", nil},
		{"domain viết hoa trong thân", 0, httpOut(`wp-content SHOP.EXAMPLE.COM`, "200"), "", Pass, "200", nil},
		{"200 nhưng không phải WordPress", 0, httpOut("<html>Welcome to nginx! shop.example.com</html>", "200"), "", Fail, "không phải WordPress", nil},
		{"tên domain nằm trong tên dài hơn (myshop)", 0, httpOut(`<link href="http://myshop.example.com/wp-content/x.css">`, "200"), "", Fail, "shop.example.com", nil},
		{"domain là đuôi của tên khác (shop.example.com.evil.net)", 0, httpOut(`wp-content http://shop.example.com.evil.net/`, "200"), "", Fail, "shop.example.com", nil},
		{"domain đứng cuối câu", 0, httpOut(`wp-content Chào mừng tới shop.example.com.`, "200"), "", Pass, "200", nil},
		{"domain kèm cổng", 0, httpOut(`wp-content http://shop.example.com:80/`, "200"), "", Pass, "200", nil},
		{"200 là WordPress của site khác", 0, httpOut(`<link href="http://khac.example.net/wp-content/x.css">`, "200"), "", Fail, "shop.example.com", nil},
		{"502", 0, httpOut("<html>502 Bad Gateway</html>", "502"), "", Fail, "502", []string{"ls -l " + l.Socket}},
		{"504", 0, httpOut("", "504"), "", Fail, "504", []string{"ls -l " + l.Socket}},
		{"500", 0, httpOut("Error establishing a database connection", "500"), "", Fail, "500", []string{"tail -n 20 " + l.PHPErrorLog}},
		{"301 chuyển hướng", 0, httpOut("", "301"), "", Fail, "301", nil},
		{"404", 0, httpOut("not found", "404"), "", Fail, "404", nil},
		{"không kết nối được", 7, httpOut("", "000"), "curl: (7) Failed to connect to shop.example.com port 80 after 0 ms: Couldn't connect to server", Fail, "không kết nối được", []string{"ss -tlnp"}},
		{"quá thời gian", 28, httpOut("", "000"), "curl: (28) Operation timed out after 15001 milliseconds with 0 bytes received", Fail, "15 giây", nil},
		{"đóng kết nối không trả gì (444)", 52, httpOut("", "000"), "curl: (52) Empty reply from server", Fail, "đóng kết nối", []string{"tpan site add shop.example.com"}},
		{"lỗi curl khác", 3, "", "curl: (3) URL using bad/illegal format", Unknown, "mã 3", nil},
		{"exit 0 mà không có mã", 0, "<html>", "", Unknown, "Chưa kiểm được", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, f := run(c.exit, c.stdout, c.stderr)
			assertShape(t, r, id, StageEndToEnd, c.want)
			assertEvidenceIsRan(t, r, f)
			if !strings.Contains(r.Summary.Vi, c.summary) {
				t.Errorf("Summary = %q, muốn có %q", r.Summary.Vi, c.summary)
			}
			if c.want == Fail {
				if !strings.HasPrefix(lastFix(r), recheckPrefix) || !strings.HasSuffix(lastFix(r), "http://shop.example.com/") {
					t.Errorf("dòng cuối của ManualFix phải là lệnh kiểm lại: %q", lastFix(r))
				}
				for _, w := range c.fixLines {
					if !strings.Contains(strings.Join(r.ManualFix, "\n"), w) {
						t.Errorf("ManualFix thiếu %q: %q", w, r.ManualFix)
					}
				}
			}
		})
	}
}

func TestHTTPSiteEvidenceIsClipped(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	big := wpPage + strings.Repeat("Ồ", 50000) // ký tự nhiều byte: kiểm cả việc không cắt giữa ký tự
	f := system.NewFake()
	reply(f, []string{"curl"}, 0, httpOut(big, "200"), "")
	r := HTTPSite(l).Run(bg(), f)
	assertShape(t, r, "http.site:shop.example.com", StageEndToEnd, Pass)
	out := r.Evidence[0].Output
	if len(out) > 1300 {
		t.Errorf("Evidence quá dài: %d byte", len(out))
	}
	if !strings.HasPrefix(out, "HTTP 200, ") || !strings.Contains(out, "<title>Cửa hàng</title>") || !strings.Contains(out, "đã cắt") {
		t.Errorf("Evidence = %q", out)
	}
	if !strings.HasPrefix(out, "HTTP 200, ") || strings.ContainsRune(out, '�') {
		t.Errorf("Evidence có ký tự hỏng: %q", out)
	}
}

func TestHTTPSiteMarkerInBody(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	// Thân trang giả mạo nhãn mã: chỉ nhãn cuối cùng (do curl in) được tin.
	f := system.NewFake()
	reply(f, []string{"curl"}, 0, httpOut("wp-content shop.example.com TPAN_HTTP_CODE=200 ", "502"), "")
	assertShape(t, HTTPSite(l).Run(bg(), f), "http.site:shop.example.com", StageEndToEnd, Fail)
}

func TestHTTPSiteUnknown(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	id := "http.site:shop.example.com"
	assertShape(t, HTTPSite(l).Run(cancelledCtx(), system.NewFake()), id, StageEndToEnd, Unknown)
	f := system.NewFake()
	assertShape(t, HTTPSite(site.Layout{}).Run(bg(), f), "http.site:", StageEndToEnd, Unknown)
	assertShape(t, HTTPSite(withUser(l, "-x")).Run(bg(), f), id, StageEndToEnd, Unknown)
	assertNoCmd(t, f)
}

// ---- SiteChecks ----

func TestSiteChecksOrder(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	cs := SiteChecks("nginx", "8.3", l)
	want := []struct {
		id    string
		stage Stage
	}{
		{"web.vhost:shop.example.com", StageWeb},
		{"php.socket:shop.example.com", StagePHP},
		{"wp.checksums:shop.example.com", StageWordPress},
		{"wp.config:shop.example.com", StageWordPress},
		{"db.access:shop.example.com", StageDB},
		{"http.site:shop.example.com", StageEndToEnd},
	}
	if len(cs) != len(want) {
		t.Fatalf("có %d phép kiểm, muốn %d", len(cs), len(want))
	}
	// Máy trống: mọi phép kiểm chạy được mà không panic, và ra đúng ID, đúng chặng.
	f := system.NewFake()
	for i, r := range RunAll(bg(), f, cs) {
		if r.ID != want[i].id || r.Stage != want[i].stage {
			t.Errorf("phép kiểm %d: %s/%s, muốn %s/%s", i, r.ID, r.Stage, want[i].id, want[i].stage)
		}
		assertText(t, "Title "+r.ID, r.Title)
		if r.Status == Fail && len(r.ManualFix) == 0 {
			t.Errorf("%s: Fail mà không có ManualFix", r.ID)
		}
		if r.AutoFix != "" {
			t.Errorf("%s: AutoFix phải rỗng ở M2: %q", r.ID, r.AutoFix)
		}
	}
	// Chặng không đi lùi: web, php, wordpress, db, e2e.
	order := map[Stage]int{StageWeb: 0, StagePHP: 1, StageWordPress: 2, StageDB: 3, StageEndToEnd: 4}
	prev := -1
	for i, w := range want {
		if order[w.stage] < prev {
			t.Errorf("phép kiểm %d (%s) đi lùi chặng", i, w.id)
		}
		prev = order[w.stage]
	}
}

// Mọi phép kiểm của site chỉ đọc: không có lệnh ghi nào (rm, chmod, systemctl reload...)
// được chạy, và FakeSystem không đổi sau khi chạy.
func TestSiteChecksOnlyRead(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	f := vhostFixture(l, "nginx")
	f.SetFile(l.WPConfig, []byte("<?php"), 0o644, system.Root)
	reply(f, []string{"wp"}, 1, "", "Error: x")
	reply(f, []string{"curl"}, 0, httpOut("", "200"), "")
	before := f.Snapshot()
	RunAll(bg(), f, append(SiteChecks("nginx", "8.3", l), Isolated(l, siteLayout(t, "blog.example.org"))))
	for _, c := range f.Ran() {
		switch c.Name {
		case "wp", "curl", "php-fpm8.3", "test":
		default:
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
