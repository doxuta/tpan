package checks

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/htaccess"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
)

// Output mẫu trong file này:
//   - `php-fpm8.3 -i`: định dạng "khoá => giá trị => giá trị gốc" lấy từ máy ảo
//     (docs/bang-chung/M14-nghien-cuu/t2-opcache.ket-qua.txt), giá trị On và
//     đường dẫn restrict_api thì viết theo định dạng đó, chưa đo;
//   - `nginx -T`: viết theo tài liệu của nginx (mỗi file mở bằng dòng
//     "# configuration file <đường dẫn>:"), dòng cảnh báo trùng tên lấy từ máy ảo
//     (t6-dinh-tuyen.ket-qua.txt);
//   - `apache2ctl -S`: viết theo hàm dump_a_vhost trong server/vhost.c của
//     Apache 2.4, chưa lấy từ máy ảo;
//   - mã thoát 35 của curl khi nginx từ chối bắt tay TLS: theo tài liệu curl.
// Các chỗ "chưa đo" nằm trong mục "cần kiểm trên máy ảo" của báo cáo.

const hardenCmd = "sudo tpan security harden"

// ---- OPcacheHardened ----

const phpInfoHardened = `phpinfo()
PHP Version => 8.3.6

Zend OPcache

Opcode Caching => Disabled
opcache.enable => On => On
opcache.restrict_api => /usr/local/lib/tpan/opcache-api/ => /usr/local/lib/tpan/opcache-api/
opcache.revalidate_freq => 2 => 2
opcache.validate_permission => On => On
opcache.validate_root => Off => Off
`

// Hai dòng đúng như máy ảo in ra trước khi vá (t2-opcache.ket-qua.txt).
const phpInfoPlain = `opcache.enable => On => On
opcache.restrict_api => no value => no value
opcache.revalidate_freq => 2 => 2
opcache.validate_permission => Off => Off
opcache.validate_root => Off => Off
`

func opcacheFixture(info string) *system.FakeSystem {
	f := system.NewFake()
	f.SetFile(templates.PHPHardeningINIPath("8.3"), templates.PHPHardeningINI(), 0o644, system.Root)
	reply(f, []string{"php-fpm8.3", "-i"}, 0, info, "")
	return f
}

func TestOPcacheHardened(t *testing.T) {
	const id = "php.opcache:8.3"
	ini := templates.PHPHardeningINIPath("8.3")

	t.Run("file đúng và PHP in đúng hai giá trị thì đạt", func(t *testing.T) {
		f := opcacheFixture(phpInfoHardened)
		r := OPcacheHardened("8.3").Run(bg(), f)
		assertShape(t, r, id, StagePHP, Pass)
		if len(r.Evidence) != 2 || r.Evidence[0].Command != "cat "+ini+" (tpan)" || r.Evidence[1].Command != "php-fpm8.3 -i" {
			t.Fatalf("Evidence = %+v", r.Evidence)
		}
		// Bằng chứng của file: các dòng có hiệu lực, không có dòng chú thích.
		if out := r.Evidence[0].Output; !strings.Contains(out, "opcache.validate_permission=1") || strings.Contains(out, "\n;") {
			t.Errorf("bằng chứng file = %q", out)
		}
		// Bằng chứng của lệnh: đúng hai dòng đang xét, không phải cả trang phpinfo.
		want := "opcache.validate_permission => On => On\nopcache.restrict_api => /usr/local/lib/tpan/opcache-api/ => /usr/local/lib/tpan/opcache-api/"
		if r.Evidence[1].Output != want {
			t.Errorf("bằng chứng lệnh = %q", r.Evidence[1].Output)
		}
	})

	fails := []struct {
		name    string
		prepare func(f *system.FakeSystem)
		info    string
		summary string
	}{
		{"không có file ini", func(f *system.FakeSystem) { _ = f.Remove(ini) }, phpInfoHardened, "không có file " + ini},
		{"file ini bị sửa", func(f *system.FakeSystem) {
			f.SetFile(ini, []byte("; sửa tay\nopcache.validate_permission=0\n"), 0o644, system.Root)
		}, phpInfoHardened, "khác nội dung tpan ghi"},
		{"validate_permission đang Off", nil, strings.Replace(phpInfoHardened, "validate_permission => On => On", "validate_permission => Off => Off", 1), "opcache.validate_permission đang là Off"},
		{"restrict_api chưa đặt", nil, strings.Replace(phpInfoHardened, "/usr/local/lib/tpan/opcache-api/ => /usr/local/lib/tpan/opcache-api/", "no value => no value", 1), "opcache.restrict_api đang là \"no value\""},
		{"máy chưa vá: cả hai giá trị sai và không có file", func(f *system.FakeSystem) { _ = f.Remove(ini) }, phpInfoPlain, "opcache.validate_permission đang là Off"},
		{"chỉ thấy một trong hai dòng", nil, "opcache.validate_permission => On => On\n", "không thấy dòng opcache.restrict_api"},
		{"file sai và PHP không in dòng OPcache nào", func(f *system.FakeSystem) { _ = f.Remove(ini) }, "phpinfo()\n", "không có file"},
	}
	for _, c := range fails {
		t.Run("lỗi: "+c.name, func(t *testing.T) {
			f := opcacheFixture(c.info)
			if c.prepare != nil {
				c.prepare(f)
			}
			r := OPcacheHardened("8.3").Run(bg(), f)
			assertShape(t, r, id, StagePHP, Fail)
			if !strings.Contains(r.Summary.Vi, c.summary) {
				t.Errorf("Summary = %q, muốn có %q", r.Summary.Vi, c.summary)
			}
			if !strings.Contains(r.Cause.Vi, "wp-config.php") || !strings.Contains(r.Cause.Vi, "opcache_reset") {
				t.Errorf("Cause phải nói hậu quả: %q", r.Cause.Vi)
			}
			if r.ManualFix[0] != hardenCmd || !strings.HasPrefix(lastFix(r), "php-fpm8.3 -i | grep") {
				t.Errorf("ManualFix = %q", r.ManualFix)
			}
			if len(r.Evidence) != 2 {
				t.Errorf("Evidence = %+v", r.Evidence)
			}
		})
	}

	t.Run("file đúng mà PHP không in dòng OPcache nào thì chưa kiểm được", func(t *testing.T) {
		r := OPcacheHardened("8.3").Run(bg(), opcacheFixture("phpinfo()\nPHP Version => 8.3.6\n"))
		assertShape(t, r, id, StagePHP, Unknown)
	})
	t.Run("php-fpm thoát mã khác 0", func(t *testing.T) {
		f := opcacheFixture("")
		reply(f, []string{"php-fpm8.3", "-i"}, 78, "", "ERROR: failed to load configuration file\n")
		r := OPcacheHardened("8.3").Run(bg(), f)
		assertShape(t, r, id, StagePHP, Unknown)
		if !strings.Contains(r.Summary.Vi, "mã 78") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
	})
	t.Run("không chạy được php-fpm", func(t *testing.T) {
		assertShape(t, OPcacheHardened("8.3").Run(cancelledCtx(), opcacheFixture(phpInfoHardened)), id, StagePHP, Unknown)
	})
	t.Run("không đọc được file ini", func(t *testing.T) {
		f := opcacheFixture(phpInfoHardened)
		_ = f.Remove(ini)
		f.SetDir(ini, 0o755, system.Root) // đọc một thư mục: lỗi khác "không có"
		r := OPcacheHardened("8.3").Run(bg(), f)
		assertShape(t, r, id, StagePHP, Unknown)
		assertNoCmd(t, f)
	})
	t.Run("bản PHP sai dạng", func(t *testing.T) {
		for _, v := range []string{"", "8", "8.3; rm -rf /", "../8.3", "8.3\n", "$(id)"} {
			f := system.NewFake()
			r := OPcacheHardened(v).Run(bg(), f)
			if r.Status != Unknown || r.ID != "php.opcache:"+v {
				t.Errorf("%q: %s %s", v, r.ID, r.Status)
			}
			assertNoCmd(t, f)
		}
	})
}

// ---- PHPProtectProc ----

func TestPHPProtectProc(t *testing.T) {
	const id = "php.protectproc:8.3"
	const cmd = "systemctl show php8.3-fpm -p ProtectProc --value"

	t.Run("invisible thì đạt", func(t *testing.T) {
		f := system.NewFake()
		reply(f, []string{"systemctl", "show"}, 0, "invisible\n", "")
		r := PHPProtectProc("8.3").Run(bg(), f)
		assertShape(t, r, id, StagePHP, Pass)
		assertEvidenceIsRan(t, r, f)
		if r.Evidence[0].Command != cmd {
			t.Errorf("lệnh = %q", r.Evidence[0].Command)
		}
	})
	t.Run("giá trị khác là cảnh báo, không phải lỗi", func(t *testing.T) {
		// "default" là giá trị máy ảo in ra trước khi vá (t7-log-va-reload.ket-qua.txt).
		for out, shown := range map[string]string{"default\n": "ProtectProc=default", "noaccess\n": "ProtectProc=noaccess", "\n": "ProtectProc=-"} {
			f := system.NewFake()
			reply(f, []string{"systemctl", "show"}, 0, out, "")
			r := PHPProtectProc("8.3").Run(bg(), f)
			assertShape(t, r, id, StagePHP, Warn)
			if !strings.Contains(r.Summary.Vi, shown) {
				t.Errorf("%q: Summary = %q", out, r.Summary.Vi)
			}
			if r.ManualFix[0] != hardenCmd || lastFix(r) != cmd {
				t.Errorf("ManualFix = %q", r.ManualFix)
			}
		}
	})
	t.Run("systemctl báo lỗi", func(t *testing.T) {
		f := system.NewFake()
		reply(f, []string{"systemctl", "show"}, 1, "", "Failed to connect to bus: No such file or directory\n")
		assertShape(t, PHPProtectProc("8.3").Run(bg(), f), id, StagePHP, Unknown)
	})
	t.Run("lệnh không chạy được", func(t *testing.T) {
		assertShape(t, PHPProtectProc("8.3").Run(cancelledCtx(), system.NewFake()), id, StagePHP, Unknown)
	})
	t.Run("bản PHP sai dạng", func(t *testing.T) {
		for _, v := range []string{"", "8", "8.3 -p Id", "-x"} {
			f := system.NewFake()
			if r := PHPProtectProc(v).Run(bg(), f); r.Status != Unknown {
				t.Errorf("%q: %s", v, r.Status)
			}
			assertNoCmd(t, f)
		}
	})
}

// ---- LogPerms ----

func logFixture(l site.Layout) *system.FakeSystem {
	grp := system.Owner{User: "root", Group: l.User}
	f := system.NewFake()
	f.SetDir(l.LogDir, 0o750, grp)
	f.SetFile(l.AccessLog, nil, 0o640, grp)
	f.SetFile(l.ErrorLog, nil, 0o640, grp)
	return f
}

func TestLogPerms(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	const id = "log.perm:shop.example.com"
	recheck := "ls -ld " + l.LogDir + " " + l.AccessLog + " " + l.ErrorLog

	t.Run("đúng chủ, nhóm và quyền thì đạt", func(t *testing.T) {
		f := logFixture(l)
		r := LogPerms(l).Run(bg(), f)
		assertShape(t, r, id, StageWeb, Pass)
		assertNoCmd(t, f) // chỉ đọc bằng Stat
		if len(r.Evidence) != 3 || r.Evidence[0].Command != "stat "+l.LogDir+" (tpan)" {
			t.Errorf("Evidence = %+v", r.Evidence)
		}
	})

	fails := []struct {
		name    string
		prepare func(f *system.FakeSystem)
		summary []string
		fix     []string
	}{
		{"máy trước M14: thư mục root:root 0755, file 0644",
			func(f *system.FakeSystem) {
				f.SetDir(l.LogDir, 0o755, system.Root)
				f.SetFile(l.AccessLog, nil, 0o644, system.Root)
				f.SetFile(l.ErrorLog, nil, 0o644, system.Root)
			},
			[]string{l.LogDir + " thuộc root:root, phải là root:" + l.User, l.LogDir + " có quyền 0755, phải là 0750", l.AccessLog + " có quyền 0644, phải là 0640", l.ErrorLog + " thuộc root:root"},
			[]string{"chown root:" + l.User + " " + l.LogDir, "chmod 750 " + l.LogDir, "chmod 640 " + l.AccessLog, "chown root:" + l.User + " " + l.ErrorLog}},
		{"file log thuộc www-data",
			func(f *system.FakeSystem) {
				f.SetFile(l.AccessLog, nil, 0o640, system.Owner{User: "www-data", Group: l.User})
			},
			[]string{l.AccessLog + " thuộc www-data:" + l.User}, []string{"chown root:" + l.User + " " + l.AccessLog}},
		{"thiếu error.log",
			func(f *system.FakeSystem) { _ = f.Remove(l.ErrorLog) },
			[]string{"không có file " + l.ErrorLog}, []string{"install -m 640 -o root -g " + l.User + " /dev/null " + l.ErrorLog}},
		{"access.log là symlink",
			func(f *system.FakeSystem) {
				_ = f.Remove(l.AccessLog)
				f.SetSymlink("/etc/shadow", l.AccessLog)
			},
			[]string{l.AccessLog + " không phải file thường"}, []string{"ls -ld " + l.AccessLog}},
	}
	for _, c := range fails {
		t.Run("lỗi: "+c.name, func(t *testing.T) {
			f := logFixture(l)
			c.prepare(f)
			r := LogPerms(l).Run(bg(), f)
			assertShape(t, r, id, StageWeb, Fail)
			for _, s := range c.summary {
				if !strings.Contains(r.Summary.Vi, s) {
					t.Errorf("Summary thiếu %q: %q", s, r.Summary.Vi)
				}
			}
			for _, s := range c.fix {
				if !containsStr(r.ManualFix, s) {
					t.Errorf("ManualFix thiếu %q: %q", s, r.ManualFix)
				}
			}
			if lastFix(r) != recheck {
				t.Errorf("ManualFix cuối = %q", lastFix(r))
			}
			if len(r.Evidence) != 3 {
				t.Errorf("Evidence = %+v", r.Evidence)
			}
			assertNoCmd(t, f)
		})
	}
	t.Run("lỗi: thiếu cả thư mục", func(t *testing.T) {
		r := LogPerms(l).Run(bg(), system.NewFake())
		assertShape(t, r, id, StageWeb, Fail)
		if r.ManualFix[0] != "install -d -m 750 -o root -g "+l.User+" "+l.LogDir {
			t.Errorf("ManualFix = %q", r.ManualFix)
		}
	})
	t.Run("không đọc được thông tin file", func(t *testing.T) {
		// Thư mục log là symlink trỏ vào chính nó: stat file bên trong báo lỗi
		// vòng lặp, không phải "không có".
		f := system.NewFake()
		f.SetSymlink(l.LogDir, l.LogDir)
		assertShape(t, LogPerms(l).Run(bg(), f), id, StageWeb, Unknown)
	})
	t.Run("Layout sai", func(t *testing.T) {
		f := logFixture(l)
		bad := l
		bad.ErrorLog = "error.log"
		assertShape(t, LogPerms(site.Layout{}).Run(bg(), f), "log.perm:", StageWeb, Unknown)
		assertShape(t, LogPerms(withUser(l, "-x")).Run(bg(), f), id, StageWeb, Unknown)
		assertShape(t, LogPerms(bad).Run(bg(), f), id, StageWeb, Unknown)
		assertNoCmd(t, f)
	})
}

// ---- VhostHardened ----

// apacheVhostBody là một vhost Apache rút gọn với khối AllowOverride cho sẵn.
func apacheVhostBody(guards []string, override ...string) string {
	var sb strings.Builder
	sb.WriteString(templates.Header + "\n<VirtualHost *:80>\n    ServerName shop.example.com\n")
	for _, g := range guards {
		sb.WriteString("    " + g + "\n")
	}
	sb.WriteString("    <Directory \"/var/www/shop.example.com/public\">\n")
	for _, o := range override {
		sb.WriteString("        " + o + "\n")
	}
	sb.WriteString("    </Directory>\n    <Directory \"/var/www/shop.example.com/public/wp-content/uploads\">\n        AllowOverride None\n    </Directory>\n</VirtualHost>\n")
	return sb.String()
}

func TestVhostHardenedNginx(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	const id = "web.hardened:shop.example.com"
	recheck := "grep -nE disable_symlinks " + l.NginxConf
	vhost := func(lines ...string) *system.FakeSystem {
		f := system.NewFake()
		f.SetFile(l.NginxConf, []byte(templates.Header+"\nserver {\n    listen 80;\n    root /var/www/shop.example.com/public;\n"+strings.Join(lines, "\n")+"\n}\n"), 0o644, system.Root)
		return f
	}

	// Dòng cũ (vhost ghi trước M28 đợt 2) và dòng mới đều đạt: phép này nằm
	// trong kiểm chứng của mọi Plan chạm site, vhost cũ không được làm hỏng Plan.
	for _, guard := range []string{templates.NginxSymlinkGuard, templates.NginxSymlinkGuardV2} {
		t.Run("có dòng chặn symlink thì đạt: "+guard, func(t *testing.T) {
			f := vhost("\t" + guard)
			r := VhostHardened("nginx", l, "").Run(bg(), f)
			assertShape(t, r, id, StageWeb, Pass)
			assertNoCmd(t, f)
			if ev := r.Evidence[0]; ev.Command != recheck+" (tpan)" || ev.Output != "5:\t"+guard {
				t.Errorf("Evidence = %+v", ev)
			}
		})
	}
	t.Run("thiếu dòng chặn, hoặc dòng chỉ nằm trong chú thích, thì lỗi", func(t *testing.T) {
		for _, f := range []*system.FakeSystem{
			vhost("    index index.php;"),
			vhost("    # " + templates.NginxSymlinkGuard),
			vhost("    # " + templates.NginxSymlinkGuardV2),
			vhost("    disable_symlinks off;"),
		} {
			r := VhostHardened("nginx", l, site.HtaccessFull).Run(bg(), f)
			assertShape(t, r, id, StageWeb, Fail)
			// Lời báo nêu dòng mới: đó là dòng `tpan security harden` sẽ ghi.
			if !strings.Contains(r.Summary.Vi, templates.NginxSymlinkGuardV2) || !strings.Contains(r.Cause.Vi, "symlink") {
				t.Errorf("Summary = %q, Cause = %q", r.Summary.Vi, r.Cause.Vi)
			}
			if r.ManualFix[0] != hardenCmd || lastFix(r) != recheck {
				t.Errorf("ManualFix = %q", r.ManualFix)
			}
		}
	})
	t.Run("không có file vhost thì chưa kiểm được", func(t *testing.T) {
		assertShape(t, VhostHardened("nginx", l, "").Run(bg(), system.NewFake()), id, StageWeb, Unknown)
	})
	t.Run("web lạ và Layout sai", func(t *testing.T) {
		f := vhost("    " + templates.NginxSymlinkGuard)
		assertShape(t, VhostHardened("caddy", l, "").Run(bg(), f), id, StageWeb, Unknown)
		assertShape(t, VhostHardened("nginx", site.Layout{}, "").Run(bg(), f), "web.hardened:", StageWeb, Unknown)
		bad := l
		bad.NginxConf = "/etc/nginx/sites-available/x y.conf"
		assertShape(t, VhostHardened("nginx", bad, "").Run(bg(), f), id, StageWeb, Unknown)
	})
}

func TestVhostHardenedApache(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	const id = "web.hardened:shop.example.com"
	guards := templates.ApacheFCGIGuards
	safe := []string{"AllowOverride " + htaccess.OverrideOptions(), "AllowOverrideList " + htaccess.List()}
	off := []string{"AllowOverride None", "FallbackResource /index.php"}
	full := []string{"AllowOverride All"}
	run := func(mode site.HtaccessMode, body string) Result {
		f := system.NewFake()
		f.SetFile(l.ApacheConf, []byte(body), 0o644, system.Root)
		r := VhostHardened("apache", l, mode).Run(bg(), f)
		assertNoCmd(t, f)
		return r
	}

	t.Run("đạt ở cả ba chế độ", func(t *testing.T) {
		for mode, override := range map[site.HtaccessMode][]string{
			"":                safe, // rỗng hiểu là an-toan
			site.HtaccessSafe: safe,
			site.HtaccessOff:  off,
			site.HtaccessFull: full,
		} {
			r := run(mode, apacheVhostBody(guards, override...))
			assertShape(t, r, id, StageWeb, Pass)
		}
	})

	shortList := "AllowOverrideList " + strings.Join(htaccess.Allowed[:len(htaccess.Allowed)-1], " ")
	fails := []struct {
		name    string
		mode    site.HtaccessMode
		body    string
		summary string
		cause   string
	}{
		{"vhost của bản tpan cũ (không gỡ biến, AllowOverride All)", site.HtaccessSafe, apacheVhostBody(nil, full...), `thiếu dòng "` + guards[0] + `"`, "open_basedir"},
		{"thiếu một trong hai dòng gỡ biến", site.HtaccessSafe, apacheVhostBody(guards[:1], safe...), `thiếu dòng "` + guards[1] + `"`, "PHP_ADMIN_VALUE"},
		{"an-toan mà còn AllowOverride All", site.HtaccessSafe, apacheVhostBody(guards, append(safe, "allowoverride all")...), `còn dòng "AllowOverride All"`, "socket PHP-FPM của site khác"},
		{"an-toan mà danh sách chỉ thị thiếu một tên", site.HtaccessSafe, apacheVhostBody(guards, safe[0], shortList), "thiếu dòng AllowOverrideList", "không khớp chế độ"},
		{"an-toan mà thiếu AllowOverride Options=", site.HtaccessSafe, apacheVhostBody(guards, "AllowOverride None", safe[1]), "thiếu dòng \"AllowOverride Options=", "không khớp chế độ"},
		{"dòng chặn chỉ nằm trong chú thích", site.HtaccessSafe, apacheVhostBody([]string{"# " + guards[0], "# " + guards[1]}, safe...), `thiếu dòng "` + guards[0] + `"`, "PHP_VALUE"},
		{"tat mà thiếu FallbackResource", site.HtaccessOff, apacheVhostBody(guards, "AllowOverride None"), `thiếu dòng "FallbackResource /index.php"`, "permalink"},
		{"tat mà còn AllowOverrideList", site.HtaccessOff, apacheVhostBody(guards, append(off, safe[1])...), "còn dòng AllowOverrideList", "không khớp chế độ"},
		{"tat mà còn AllowOverride All", site.HtaccessOff, apacheVhostBody(guards, append(off, full...)...), `còn dòng "AllowOverride All"`, "FollowSymLinks"},
		{"day-du mà vhost không có AllowOverride All", site.HtaccessFull, apacheVhostBody(guards, safe...), `thiếu dòng "AllowOverride All"`, "không khớp chế độ"},
	}
	for _, c := range fails {
		t.Run("lỗi: "+c.name, func(t *testing.T) {
			r := run(c.mode, c.body)
			assertShape(t, r, id, StageWeb, Fail)
			if !strings.Contains(r.Summary.Vi, c.summary) {
				t.Errorf("Summary = %q, muốn có %q", r.Summary.Vi, c.summary)
			}
			if !strings.Contains(r.Cause.Vi, c.cause) {
				t.Errorf("Cause = %q, muốn có %q", r.Cause.Vi, c.cause)
			}
			if r.ManualFix[0] != hardenCmd || !strings.HasPrefix(lastFix(r), "grep -nE ") || !strings.HasSuffix(lastFix(r), l.ApacheConf) {
				t.Errorf("ManualFix = %q", r.ManualFix)
			}
		})
	}
	t.Run("hai dòng thiếu cùng mở một lỗ thì Cause nói một lần", func(t *testing.T) {
		r := run(site.HtaccessFull, apacheVhostBody(nil, full...))
		assertShape(t, r, id, StageWeb, Fail)
		if n := strings.Count(r.Cause.Vi, "open_basedir"); n != 1 {
			t.Errorf("Cause nhắc open_basedir %d lần: %q", n, r.Cause.Vi)
		}
	})
	t.Run("chế độ lạ", func(t *testing.T) {
		assertShape(t, run("mo-het", apacheVhostBody(guards, full...)), id, StageWeb, Unknown)
	})
	t.Run("không có file vhost", func(t *testing.T) {
		assertShape(t, VhostHardened("apache", l, "").Run(bg(), system.NewFake()), id, StageWeb, Unknown)
	})
}

// Vhost do chính template sinh ra phải qua được phép kiểm, ở mọi web server,
// có và không có SSL, mọi chế độ .htaccess: template và phép kiểm dùng chung
// hằng, test này giữ cho chúng không lệch nhau.
func TestVhostHardenedAcceptsTemplates(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	for _, web := range []string{"nginx", "apache"} {
		for _, ssl := range []bool{false, true} {
			for _, mode := range []site.HtaccessMode{site.HtaccessSafe, site.HtaccessOff, site.HtaccessFull} {
				body, err := templates.SiteVhost(l, web, "8.3", "/var/lib/tpan/acme", ssl, mode)
				if err != nil {
					t.Fatalf("%s ssl=%v %s: %v", web, ssl, mode, err)
				}
				conf := l.NginxConf
				if web == "apache" {
					conf = l.ApacheConf
				}
				f := system.NewFake()
				f.SetFile(conf, body, 0o644, system.Root)
				if r := VhostHardened(web, l, mode).Run(bg(), f); r.Status != Pass {
					t.Errorf("%s ssl=%v %s: %s: %s", web, ssl, mode, r.Status, r.Summary.Vi)
				}
			}
		}
	}
}

// ---- ScanHtaccess, HtaccessDirectives ----

// htaccessFixture giả lập find và head dưới user của site: files là đường dẫn
// và nội dung; nội dung "\x00DENY" nghĩa là head không đọc được file đó.
func htaccessFixture(l site.Layout, paths []string, files map[string]string) *system.FakeSystem {
	f := system.NewFake()
	out := ""
	for _, p := range paths {
		out += p + "\x00"
	}
	reply(f, []string{"find"}, 0, out, "")
	f.Handle([]string{"dd"}, func(c system.Cmd) system.Result {
		file := strings.TrimPrefix(c.Args[0], "if=")
		body := files[file]
		if body == "\x00DENY" {
			return system.Result{ExitCode: 1, Stderr: "dd: failed to open '" + file + "': Permission denied\n"}
		}
		return system.Result{Stdout: body}
	})
	return f
}

const wpHtaccess = `# BEGIN WordPress
<IfModule mod_rewrite.c>
RewriteEngine On
RewriteBase /
RewriteRule ^index\.php$ - [L]
RewriteCond %{REQUEST_FILENAME} !-f
RewriteRule . /index.php [L]
</IfModule>
# END WordPress
Options -Indexes
`

func TestScanHtaccess(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	top := l.Public + "/.htaccess"
	admin := l.Public + "/wp-admin/.htaccess"
	uploads := l.Public + "/wp-content/uploads/2026/.htaccess"
	odd := l.Public + "/a b\n$(id)/.htaccess"

	t.Run("lệnh chạy dưới user của site, đúng argv; bỏ uploads và đường dẫn ngoài public", func(t *testing.T) {
		paths := []string{top, uploads, admin, "/var/www/blog.example.org/public/.htaccess", l.Public + "/../../etc/.htaccess", odd}
		f := htaccessFixture(l, paths, map[string]string{
			top:   wpHtaccess,
			admin: "AuthType Basic\nSetHandler \"proxy:unix:/run/php/tpan-khac.sock|fcgi://localhost\"\n",
			odd:   "Options +FollowSymLinks\n",
		})
		found, ev, err := ScanHtaccess(bg(), f, l)
		if err != nil {
			t.Fatal(err)
		}
		ran := f.Ran()
		wantFind := "runuser -u " + l.User + " -- find " + l.Public + " -name .htaccess '(' -type f -o -type l ')' -print0"
		if ran[0].String() != wantFind {
			t.Errorf("find = %q, muốn %q", ran[0].String(), wantFind)
		}
		// Ba file được đọc, theo thứ tự đã sắp; đường dẫn lạ đi nguyên vào một
		// phần tử argv. Đọc không chặn (M28): FIFO không làm lệnh chờ.
		var heads []string
		for _, c := range ran[1:] {
			if c.Name != "dd" || c.AsUser != l.User || len(c.Args) != 5 || !strings.HasPrefix(c.Args[0], "if=") ||
				strings.Join(c.Args[1:], " ") != "iflag=nonblock bs=262144 count=1 status=none" {
				t.Errorf("lệnh đọc sai: %+v", c)
			}
			heads = append(heads, strings.TrimPrefix(c.Args[0], "if="))
		}
		if strings.Join(heads, "|") != strings.Join([]string{top, odd, admin}, "|") {
			t.Errorf("file đã đọc = %q", heads)
		}
		want := []htaccess.Finding{
			{File: odd, Line: 1, Directive: "Options FollowSymLinks"},
			{File: admin, Line: 2, Directive: "SetHandler"},
		}
		if fmt.Sprint(found) != fmt.Sprint(want) {
			t.Errorf("found = %+v, muốn %+v", found, want)
		}
		if len(ev) != 2 || ev[0].Command != wantFind || strings.Contains(ev[0].Output, "\x00") {
			t.Fatalf("ev = %+v", ev)
		}
		if !strings.Contains(ev[1].Output, "3 .htaccess files scanned, 0 unreadable, 1 skipped under wp-content/uploads, 2 directives") {
			t.Errorf("dòng tóm tắt = %q", ev[1].Output)
		}
	})
	t.Run("không có .htaccess nào", func(t *testing.T) {
		f := htaccessFixture(l, nil, nil)
		found, ev, err := ScanHtaccess(bg(), f, l)
		if err != nil || len(found) != 0 || len(ev) != 2 || len(f.Ran()) != 1 {
			t.Errorf("found=%v ev=%v err=%v ran=%v", found, ev, err, f.Ran())
		}
	})
	t.Run("file không đọc được dưới user của site là một Finding", func(t *testing.T) {
		f := htaccessFixture(l, []string{top, admin}, map[string]string{top: wpHtaccess, admin: "\x00DENY"})
		found, ev, err := ScanHtaccess(bg(), f, l)
		if err != nil {
			t.Fatal(err)
		}
		if len(found) != 1 || found[0].File != admin || found[0].Directive != "(không đọc được)" || found[0].Line != 0 {
			t.Errorf("found = %+v", found)
		}
		if !strings.Contains(ev[1].Output, "1 unreadable") || !strings.Contains(ev[1].Output, "0 directives") {
			t.Errorf("dòng tóm tắt = %q", ev[1].Output)
		}
	})
	t.Run("tối đa 200 file", func(t *testing.T) {
		var paths []string
		for i := 0; i < 250; i++ {
			paths = append(paths, fmt.Sprintf("%s/d%03d/.htaccess", l.Public, i))
		}
		f := htaccessFixture(l, paths, nil)
		_, ev, err := ScanHtaccess(bg(), f, l)
		if err != nil {
			t.Fatal(err)
		}
		if n := len(f.Ran()) - 1; n != 200 {
			t.Errorf("đã đọc %d file, muốn 200", n)
		}
		if !strings.Contains(ev[1].Output, "250 files found, only the first 200 scanned") {
			t.Errorf("dòng tóm tắt = %q", ev[1].Output)
		}
	})
	t.Run("find lỗi thì err, không phải Finding", func(t *testing.T) {
		f := system.NewFake()
		reply(f, []string{"find"}, 1, "", "find: ‘"+l.Public+"’: No such file or directory\n")
		found, ev, err := ScanHtaccess(bg(), f, l)
		if err == nil || found != nil || len(ev) != 1 {
			t.Errorf("found=%v ev=%v err=%v", found, ev, err)
		}
		if _, _, err := ScanHtaccess(cancelledCtx(), htaccessFixture(l, nil, nil), l); err == nil {
			t.Error("ctx đã huỷ mà không có lỗi")
		}
	})
	t.Run("Layout sai thì không chạy lệnh nào", func(t *testing.T) {
		bad := l
		bad.Public = l.Public + "/$(id)"
		for _, x := range []site.Layout{{}, withUser(l, "-x"), bad} {
			f := htaccessFixture(l, []string{top}, nil)
			if _, _, err := ScanHtaccess(bg(), f, x); err == nil {
				t.Errorf("%+v: không có lỗi", x)
			}
			assertNoCmd(t, f)
		}
	})
}

func TestHtaccessDirectives(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	const id = "web.htaccess:shop.example.com"
	const recheck = "sudo tpan health shop.example.com"
	top := l.Public + "/.htaccess"
	plugin := l.Public + "/wp-content/plugins/x/.htaccess"

	t.Run("an-toan (và rỗng): mọi chỉ thị trong danh sách thì đạt", func(t *testing.T) {
		for _, mode := range []site.HtaccessMode{"", site.HtaccessSafe} {
			f := htaccessFixture(l, []string{top}, map[string]string{top: wpHtaccess})
			r := HtaccessDirectives(l, mode).Run(bg(), f)
			assertShape(t, r, id, StageWeb, Pass)
			if len(r.Evidence) != 2 || len(f.Ran()) != 2 {
				t.Errorf("Evidence = %+v, ran = %v", r.Evidence, f.Ran())
			}
		}
	})
	t.Run("an-toan: chỉ thị ngoài danh sách là lỗi, nêu đúng file và dòng", func(t *testing.T) {
		f := htaccessFixture(l, []string{top, plugin}, map[string]string{
			top:    wpHtaccess,
			plugin: "# plugin x\nphp_value upload_max_filesize 64M\nAddHandler application/x-httpd-php .php\n",
		})
		r := HtaccessDirectives(l, site.HtaccessSafe).Run(bg(), f)
		assertShape(t, r, id, StageWeb, Fail)
		if !strings.Contains(r.Summary.Vi, "2 chỉ thị") || !strings.Contains(r.Summary.Vi, plugin+":2: php_value") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
		if !strings.Contains(r.Cause.Vi, "500") {
			t.Errorf("Cause = %q", r.Cause.Vi)
		}
		last := r.Evidence[len(r.Evidence)-1]
		if len(r.Evidence) != 3 || last.Output != plugin+":2: php_value\n"+plugin+":3: AddHandler" {
			t.Errorf("Evidence = %+v", r.Evidence)
		}
		want := []string{"sed -n 2p " + plugin, "nano +2 " + plugin, "sudo tpan site set shop.example.com --htaccess day-du", recheck}
		if strings.Join(r.ManualFix, "\n") != strings.Join(want, "\n") {
			t.Errorf("ManualFix = %q, muốn %q", r.ManualFix, want)
		}
	})
	t.Run("an-toan: bằng chứng liệt kê tối đa 20 dòng", func(t *testing.T) {
		f := htaccessFixture(l, []string{top}, map[string]string{top: strings.Repeat("SetHandler x\n", 25)})
		r := HtaccessDirectives(l, site.HtaccessSafe).Run(bg(), f)
		assertShape(t, r, id, StageWeb, Fail)
		lines := strings.Split(r.Evidence[2].Output, "\n")
		if len(lines) != 21 || lines[20] != "... and 5 more" {
			t.Errorf("bằng chứng có %d dòng, dòng cuối %q", len(lines), lines[len(lines)-1])
		}
	})
	t.Run("an-toan: file không đọc được là lỗi", func(t *testing.T) {
		f := htaccessFixture(l, []string{top}, map[string]string{top: "\x00DENY"})
		r := HtaccessDirectives(l, site.HtaccessSafe).Run(bg(), f)
		assertShape(t, r, id, StageWeb, Fail)
		if !strings.Contains(r.Summary.Vi, top+": (không đọc được)") || !strings.Contains(r.Summary.En, top+": (unreadable)") {
			t.Errorf("Summary = %+v", r.Summary)
		}
		if r.ManualFix[0] != "ls -l "+top || lastFix(r) != recheck {
			t.Errorf("ManualFix = %q", r.ManualFix)
		}
		if out := r.Evidence[2].Output; out != top+": (unreadable)" {
			t.Errorf("bằng chứng = %q", out)
		}
	})
	t.Run("an-toan: không quét được thì chưa kiểm được", func(t *testing.T) {
		f := system.NewFake()
		reply(f, []string{"find"}, 1, "", "runuser: user "+l.User+" does not exist\n")
		r := HtaccessDirectives(l, site.HtaccessSafe).Run(bg(), f)
		assertShape(t, r, id, StageWeb, Unknown)
		if len(r.Evidence) != 1 {
			t.Errorf("Evidence = %+v", r.Evidence)
		}
		assertShape(t, HtaccessDirectives(l, site.HtaccessSafe).Run(cancelledCtx(), system.NewFake()), id, StageWeb, Unknown)
	})
	t.Run("day-du: luôn cảnh báo, không chạy lệnh nào", func(t *testing.T) {
		f := system.NewFake()
		r := HtaccessDirectives(l, site.HtaccessFull).Run(bg(), f)
		assertShape(t, r, id, StageWeb, Warn)
		assertNoCmd(t, f)
		if !strings.Contains(r.Summary.Vi, "AllowOverride All") || !strings.Contains(r.Cause.Vi, "user của site khác") || !strings.Contains(r.Cause.Vi, "symlink") {
			t.Errorf("Summary = %q, Cause = %q", r.Summary.Vi, r.Cause.Vi)
		}
		if r.ManualFix[0] != "sudo tpan site set shop.example.com --htaccess an-toan" || lastFix(r) != recheck {
			t.Errorf("ManualFix = %q", r.ManualFix)
		}
	})
	t.Run("tat: đạt, không chạy lệnh nào", func(t *testing.T) {
		f := system.NewFake()
		r := HtaccessDirectives(l, site.HtaccessOff).Run(bg(), f)
		assertShape(t, r, id, StageWeb, Pass)
		assertNoCmd(t, f)
		if !strings.Contains(r.Summary.Vi, "Apache không đọc .htaccess của site này") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
	})
	t.Run("chế độ lạ và Layout sai", func(t *testing.T) {
		f := system.NewFake()
		assertShape(t, HtaccessDirectives(l, "mo-het").Run(bg(), f), id, StageWeb, Unknown)
		assertShape(t, HtaccessDirectives(site.Layout{}, site.HtaccessSafe).Run(bg(), f), "web.htaccess:", StageWeb, Unknown)
		assertShape(t, HtaccessDirectives(withUser(l, "-x"), site.HtaccessSafe).Run(bg(), f), id, StageWeb, Unknown)
		assertNoCmd(t, f)
	})
}

// ---- ServerNameUnique ----

// nginxDump ghép các file thành bản in của `nginx -T`: mỗi file mở bằng dòng
// "# configuration file <đường dẫn>:" và kết thúc bằng một dòng trống.
func nginxDump(files ...[2]string) string {
	var sb strings.Builder
	for _, f := range files {
		sb.WriteString("# configuration file " + f[0] + ":\n" + f[1] + "\n")
	}
	return sb.String()
}

const (
	ngxMainConf = `user www-data;
worker_processes auto;
events {
	worker_connections 768;
}
http {
	sendfile on;
	include /etc/nginx/mime.types;
	log_format main '$remote_addr - "$request" {$status}';
	map $http_upgrade $connection_upgrade {
		default upgrade;
		''      close;
	}
	include /etc/nginx/conf.d/*.conf;
	include /etc/nginx/sites-enabled/*;
}
`
	ngxMime = `types {
    text/html                             html htm shtml;
    text/css                              css;
}
`
	ngxDefault = `# Do tpan quản lý. Sửa tay sẽ bị tpan health báo lệch.
server {
    listen 80 default_server;
    listen [::]:80 default_server;
    server_name _;
    location ^~ /.well-known/acme-challenge/ {
        root /var/lib/tpan/acme;
    }
    location / {
        return 444;
    }
}
`
	// Site a có SSL: hai khối server cùng tên, một ở cổng 80, một ở cổng 443.
	// server_name nằm ở dòng 5 và dòng 19.
	ngxSiteA = `# Do tpan quản lý. Sửa tay sẽ bị tpan health báo lệch.
server {
    listen 80;
    listen [::]:80;
    server_name a.example.com;

    location ^~ /.well-known/acme-challenge/ {
        root /var/lib/tpan/acme;
    }
    location / {
        return 301 https://$host$request_uri;
    }
}

server {
    # "ssl" ở dòng listen bật TLS; dấu { hay } trong chú thích không mở, đóng khối
    listen 443 ssl;
    listen [::]:443 ssl;
    server_name a.example.com;
    root /var/www/a.example.com/public;
    location ~ \.php$ {
        fastcgi_pass unix:/run/php/tpan-a-8.3.sock;
        fastcgi_param SCRIPT_FILENAME "${document_root}$fastcgi_script_name";
        # server_name b.example.com; trong location không phải là khai tên
        set $x "server_name b.example.com;";
    }
    location ~* "\.(txt|log){1}$" { deny all; }
}
`
	// server_name ở dòng 5.
	ngxSiteB = `# Do tpan quản lý. Sửa tay sẽ bị tpan health báo lệch.
server {
    listen 80;
    listen [::]:80;
    server_name b.example.com;
    root /var/www/b.example.com/public;
    location / { try_files $uri $uri/ /index.php?$args; }
}
`
	// File ai đó thêm tay, trùng tên của site a. server_name ở dòng 4.
	ngxStray = `# thêm tay
server {
    listen 80;
    server_name www.khac.example.net A.Example.Com.;
    location / { return 200 "trang la"; }
}
`
	ngxSyntaxOK  = "nginx: the configuration file /etc/nginx/nginx.conf syntax is ok\nnginx: configuration file /etc/nginx/nginx.conf test is successful\n"
	ngxConflict  = `2026/11/20 16:58:31 [warn] 36535#36535: conflicting server name "a.example.com" on 0.0.0.0:80, ignored` + "\n"
	ngxStrayPath = "/etc/nginx/sites-enabled/aaa-trung.conf"
)

func TestServerNameUniqueNginx(t *testing.T) {
	a, b := siteLayout(t, "a.example.com"), siteLayout(t, "b.example.com")
	sites := []site.Layout{a, b}
	const id = "web.server_name.unique"
	base := [][2]string{
		{"/etc/nginx/nginx.conf", ngxMainConf},
		{"/etc/nginx/mime.types", ngxMime},
		{"/etc/nginx/sites-enabled/tpan-default.conf", ngxDefault},
	}
	dump := func(extra ...[2]string) string { return nginxDump(append(append([][2]string{}, base...), extra...)...) }
	siteA, siteB := [2]string{a.NginxLink, ngxSiteA}, [2]string{b.NginxLink, ngxSiteB}
	run := func(stdout, stderr string) (Result, *system.FakeSystem) {
		f := system.NewFake()
		reply(f, []string{"nginx", "-T"}, 0, stdout, stderr)
		return ServerNameUnique("nginx", sites).Run(bg(), f), f
	}

	t.Run("mỗi tên một vhost thì đạt; site có SSL khai tên ở cổng 80 và 443 không phải là trùng", func(t *testing.T) {
		r, f := run(dump(siteA, siteB), ngxSyntaxOK)
		assertShape(t, r, id, StageWeb, Pass)
		assertEvidenceIsRan(t, r, f)
		want := a.NginxLink + ":5: a.example.com (port 80)\n" + a.NginxLink + ":19: a.example.com (port 443)\n" + b.NginxLink + ":5: b.example.com (port 80)"
		if r.Evidence[0].Command != "nginx -T" || r.Evidence[0].Output != want {
			t.Errorf("Evidence = %+v, muốn Output %q", r.Evidence[0], want)
		}
	})
	t.Run("file lạ trùng tên thì lỗi, nêu đúng hai file và số dòng", func(t *testing.T) {
		// aaa-trung.conf xếp trước theo tên nên nginx nạp nó trước và dùng nó.
		r, _ := run(dump([2]string{ngxStrayPath, ngxStray}, siteA, siteB), ngxConflict+ngxSyntaxOK)
		assertShape(t, r, id, StageWeb, Fail)
		want := "Tên miền a.example.com được khai ở hai nơi: " + a.NginxLink + ":5 (cổng 80) và " + ngxStrayPath + ":4 (cổng 80)"
		if r.Summary.Vi != want {
			t.Errorf("Summary = %q, muốn %q", r.Summary.Vi, want)
		}
		if !strings.Contains(r.Summary.En, ngxStrayPath+":4") || !strings.Contains(r.Cause.Vi, "nạp trước") {
			t.Errorf("Summary.En = %q, Cause = %q", r.Summary.En, r.Cause.Vi)
		}
		wantFix := []string{
			"grep -nE 'listen|server_name' " + ngxStrayPath + " " + a.NginxLink,
			"ls -l " + ngxStrayPath,
			"mv " + ngxStrayPath + " /root/",
			"nginx -t", "systemctl reload nginx", "nginx -T",
		}
		if strings.Join(r.ManualFix, "\n") != strings.Join(wantFix, "\n") {
			t.Errorf("ManualFix = %q, muốn %q", r.ManualFix, wantFix)
		}
		out := r.Evidence[0].Output
		if !strings.Contains(out, ngxStrayPath+":4: a.example.com (port 80)") || !strings.Contains(out, "conflicting server name") {
			t.Errorf("bằng chứng = %q", out)
		}
	})
	t.Run("tên chỉ nằm trong file lạ (vhost của site không được nạp)", func(t *testing.T) {
		r, _ := run(dump([2]string{ngxStrayPath, ngxStray}, siteB), ngxSyntaxOK)
		assertShape(t, r, id, StageWeb, Fail)
		if !strings.Contains(r.Summary.Vi, ngxStrayPath+":4 (cổng 80), không phải vhost của site đó") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
	})
	t.Run("file lạ khai tên ở cổng khác vẫn là lỗi", func(t *testing.T) {
		stray := "server { listen 127.0.0.1:8080; server_name b.example.com; }\n"
		r, _ := run(dump(siteA, siteB, [2]string{"/etc/nginx/conf.d/x.conf", stray}), ngxSyntaxOK)
		assertShape(t, r, id, StageWeb, Fail)
		if !strings.Contains(r.Summary.Vi, "/etc/nginx/conf.d/x.conf:1 (cổng 8080)") || !strings.Contains(r.Summary.Vi, b.NginxLink+":5 (cổng 80)") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
	})
	t.Run("khối server nằm ngay trong http của nginx.conf cũng được đọc", func(t *testing.T) {
		inline := strings.Replace(ngxMainConf, "\tsendfile on;\n", "\tsendfile on;\n\tserver { server_name b.example.com; }\n", 1)
		r, _ := run(nginxDump([2]string{"/etc/nginx/nginx.conf", inline}, siteA, siteB), ngxSyntaxOK)
		assertShape(t, r, id, StageWeb, Fail)
		// Không có listen: nginx nghe cổng 80.
		if !strings.Contains(r.Summary.Vi, "/etc/nginx/nginx.conf:8 (cổng 80)") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
	})
	t.Run("hai khối cùng tên, cùng cổng trong chính vhost của site", func(t *testing.T) {
		twice := ngxSiteB + "server {\n    listen 80;\n    server_name b.example.com;\n}\n"
		r, _ := run(dump(siteA, [2]string{b.NginxLink, twice}), ngxSyntaxOK)
		assertShape(t, r, id, StageWeb, Fail)
		if !strings.Contains(r.Summary.Vi, b.NginxLink+":5 (cổng 80) và "+b.NginxLink+":11 (cổng 80)") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
		if !containsStr(r.ManualFix, "sudo tpan health --fix") || strings.Contains(strings.Join(r.ManualFix, "\n"), "mv ") {
			t.Errorf("ManualFix = %q", r.ManualFix)
		}
	})
	t.Run("nhiều tên miền bị trùng thì Summary nói còn bao nhiêu", func(t *testing.T) {
		stray := "server { listen 80; server_name a.example.com b.example.com; }\n"
		r, _ := run(dump([2]string{ngxStrayPath, stray}, siteA, siteB), ngxSyntaxOK)
		assertShape(t, r, id, StageWeb, Fail)
		if !strings.Contains(r.Summary.Vi, "còn 1 tên miền khác") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
	})
	t.Run("tên dạng wildcard và tên dài hơn không tính là trùng", func(t *testing.T) {
		other := "server { listen 80; server_name *.example.com xa.example.com a.example.com.vn ~^a\\.example\\.com$; }\n"
		r, _ := run(dump(siteA, siteB, [2]string{"/etc/nginx/conf.d/khac.conf", other}), ngxSyntaxOK)
		assertShape(t, r, id, StageWeb, Pass)
	})
	t.Run("nginx -T thoát mã khác 0 hoặc output lạ thì chưa kiểm được", func(t *testing.T) {
		f := system.NewFake()
		reply(f, []string{"nginx", "-T"}, 1, "", "nginx: [emerg] unexpected \"}\" in /etc/nginx/sites-enabled/x.conf:3\nnginx: configuration file /etc/nginx/nginx.conf test failed\n")
		r := ServerNameUnique("nginx", sites).Run(bg(), f)
		assertShape(t, r, id, StageWeb, Unknown)
		if !strings.Contains(r.Summary.Vi, "emerg") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
		r, _ = run("server { server_name a.example.com; }\n", ngxSyntaxOK) // không có dòng mở file
		assertShape(t, r, id, StageWeb, Unknown)
		assertShape(t, ServerNameUnique("nginx", sites).Run(cancelledCtx(), system.NewFake()), id, StageWeb, Unknown)
	})
	t.Run("không có site nào thì đạt, không chạy lệnh", func(t *testing.T) {
		f := system.NewFake()
		assertShape(t, ServerNameUnique("nginx", nil).Run(bg(), f), id, StageWeb, Pass)
		assertNoCmd(t, f)
	})
	t.Run("web lạ và Layout rỗng", func(t *testing.T) {
		f := system.NewFake()
		assertShape(t, ServerNameUnique("caddy", sites).Run(bg(), f), id, StageWeb, Unknown)
		assertShape(t, ServerNameUnique("nginx", []site.Layout{a, {}}).Run(bg(), f), id, StageWeb, Unknown)
		assertNoCmd(t, f)
	})
}

func TestNginxListenPort(t *testing.T) {
	for arg, want := range map[string]string{
		"80": "80", "443": "443", "[::]:80": "80", "*:8080": "8080", "127.0.0.1:8443": "8443",
		"[::1]:9000": "9000", "localhost:8000": "8000", "127.0.0.1": "80", "[::]": "80", "unix:/run/nginx.sock": "",
	} {
		if got := nginxListenPort(arg); got != want {
			t.Errorf("nginxListenPort(%q) = %q, muốn %q", arg, got, want)
		}
	}
}

// apacheS là output của `apache2ctl -S` trên máy có site a (SSL) và site b.
// extra80 chèn thêm dòng vào khối *:80, tail thêm dòng sau các khối NameVirtualHost.
func apacheS(a, b site.Layout, extra80, tail string) string {
	return `VirtualHost configuration:
*:443                  is a NameVirtualHost
         default server tpan-default.invalid (/etc/apache2/sites-enabled/000-tpan-default-ssl.conf:7)
         port 443 namevhost tpan-default.invalid (/etc/apache2/sites-enabled/000-tpan-default-ssl.conf:7)
         port 443 namevhost a.example.com (` + a.ApacheLink + `:36)
*:80                   is a NameVirtualHost
         default server tpan-default.invalid (/etc/apache2/sites-enabled/000-tpan-default.conf:6)
         port 80 namevhost tpan-default.invalid (/etc/apache2/sites-enabled/000-tpan-default.conf:6)
` + extra80 + `         port 80 namevhost a.example.com (` + a.ApacheLink + `:5)
         port 80 namevhost b.example.com (` + b.ApacheLink + `:5)
` + tail + `ServerRoot: "/etc/apache2"
Main DocumentRoot: "/var/www/html"
Main ErrorLog: "/var/log/apache2/error.log"
Mutex default: dir="/var/run/apache2/" mechanism=default
Mutex watchdog-callback: using_defaults
PidFile: "/var/run/apache2/apache2.pid"
Define: DUMP_VHOSTS
Define: DUMP_RUN_CFG
User: name="www-data" id=33
Group: name="www-data" id=33
`
}

func TestServerNameUniqueApache(t *testing.T) {
	a, b := siteLayout(t, "a.example.com"), siteLayout(t, "b.example.com")
	sites := []site.Layout{a, b}
	const id = "web.server_name.unique"
	const stray = "/etc/apache2/sites-enabled/aaa-trung.conf"
	const fqdnWarn = "AH00558: apache2: Could not reliably determine the server's fully qualified domain name, using 127.0.1.1. Set the 'ServerName' directive globally to suppress this message\n"
	run := func(stdout string) Result {
		f := system.NewFake()
		reply(f, []string{"apache2ctl", "-S"}, 0, stdout, fqdnWarn)
		r := ServerNameUnique("apache", sites).Run(bg(), f)
		if r.Evidence[0].Command != "apache2ctl -S" {
			t.Errorf("lệnh = %q", r.Evidence[0].Command)
		}
		return r
	}

	t.Run("mỗi tên một VirtualHost thì đạt; cổng 80 và 443 của cùng site không phải là trùng", func(t *testing.T) {
		r := run(apacheS(a, b, "", ""))
		assertShape(t, r, id, StageWeb, Pass)
		want := a.ApacheLink + ":36: a.example.com (port 443)\n" + a.ApacheLink + ":5: a.example.com (port 80)\n" + b.ApacheLink + ":5: b.example.com (port 80)"
		if r.Evidence[0].Output != want {
			t.Errorf("bằng chứng = %q, muốn %q", r.Evidence[0].Output, want)
		}
	})
	t.Run("file lạ trùng ServerName thì lỗi, nêu đúng file và dòng", func(t *testing.T) {
		r := run(apacheS(a, b, "         port 80 namevhost a.example.com ("+stray+":1)\n                 alias www.a.example.com\n", ""))
		assertShape(t, r, id, StageWeb, Fail)
		want := "Tên miền a.example.com được khai ở hai nơi: " + a.ApacheLink + ":5 (cổng 80) và " + stray + ":1 (cổng 80)"
		if r.Summary.Vi != want {
			t.Errorf("Summary = %q, muốn %q", r.Summary.Vi, want)
		}
		wantFix := []string{
			"grep -nE 'VirtualHost|ServerName|ServerAlias' " + stray + " " + a.ApacheLink,
			"ls -l " + stray, "mv " + stray + " /root/",
			"apachectl configtest", "systemctl reload apache2", "apache2ctl -S",
		}
		if strings.Join(r.ManualFix, "\n") != strings.Join(wantFix, "\n") {
			t.Errorf("ManualFix = %q, muốn %q", r.ManualFix, wantFix)
		}
	})
	t.Run("file lạ khai tên của site bằng ServerAlias", func(t *testing.T) {
		r := run(apacheS(a, b, "         port 80 namevhost khac.example.net ("+stray+":3)\n                 alias www.khac.example.net\n                 alias B.example.com\n", ""))
		assertShape(t, r, id, StageWeb, Fail)
		if !strings.Contains(r.Summary.Vi, "Tên miền b.example.com") || !strings.Contains(r.Summary.Vi, stray+":3 (cổng 80)") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
	})
	t.Run("địa chỉ chỉ có một VirtualHost (dạng in gọn một dòng)", func(t *testing.T) {
		r := run(apacheS(a, b, "", "127.0.0.1:8080         b.example.com (/etc/apache2/conf-enabled/x.conf:2)\n"))
		assertShape(t, r, id, StageWeb, Fail)
		if !strings.Contains(r.Summary.Vi, "/etc/apache2/conf-enabled/x.conf:2 (cổng 8080)") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
	})
	t.Run("một VirtualHost hiện dưới hai địa chỉ không phải là trùng", func(t *testing.T) {
		r := run(apacheS(a, b, "", "[::1]:80               is a NameVirtualHost\n         default server b.example.com ("+b.ApacheLink+":5)\n         port 80 namevhost b.example.com ("+b.ApacheLink+":5)\n"))
		assertShape(t, r, id, StageWeb, Pass)
	})
	t.Run("lệnh lỗi hoặc output lạ thì chưa kiểm được", func(t *testing.T) {
		f := system.NewFake()
		reply(f, []string{"apache2ctl", "-S"}, 1, "", "AH00526: Syntax error on line 3 of /etc/apache2/sites-enabled/x.conf:\nInvalid command 'Foo'\n")
		assertShape(t, ServerNameUnique("apache", sites).Run(bg(), f), id, StageWeb, Unknown)
		assertShape(t, run("apache2: unrecognized output\n"), id, StageWeb, Unknown)
		assertShape(t, ServerNameUnique("apache", sites).Run(cancelledCtx(), system.NewFake()), id, StageWeb, Unknown)
	})
}

// ---- DefaultRejects ----

const (
	rejectName = "tpan-0a1b2c3d.invalid"
	reject80   = "curl -sS --max-time 5 -o /dev/null -w '%{http_code}' -H 'Host: " + rejectName + "' http://127.0.0.1/"
	reject443  = "curl -sS -k --max-time 5 -o /dev/null -w '%{http_code}' --resolve " + rejectName + ":443:127.0.0.1 https://" + rejectName + "/"
)

// rejectFixture trả lời riêng cho lần gọi cổng 80 và cổng 443 (lệnh cổng 443 có -k).
func rejectFixture(code80 int, out80, err80 string, code443 int, out443, err443 string) *system.FakeSystem {
	f := system.NewFake()
	reply(f, []string{"curl", "-sS", "--max-time"}, code80, out80, err80)
	reply(f, []string{"curl", "-sS", "-k"}, code443, out443, err443)
	return f
}

func TestDefaultRejects(t *testing.T) {
	const id = "web.default.rejects"
	const empty = "curl: (52) Empty reply from server\n"
	const tlsRefused = "curl: (35) OpenSSL/3.0.13: error:0A000458:SSL routines::tlsv1 unrecognized name\n"

	t.Run("nginx: đóng kết nối ở cổng 80 thì đạt, không gọi cổng 443 khi chưa có site SSL", func(t *testing.T) {
		f := rejectFixture(52, "000", empty, 0, "200", "")
		r := defaultRejects("nginx", false, rejectName).Run(bg(), f)
		assertShape(t, r, id, StageWeb, Pass)
		assertEvidenceIsRan(t, r, f)
		if len(r.Evidence) != 1 || r.Evidence[0].Command != reject80 || r.Evidence[0].ExitCode != 52 {
			t.Errorf("Evidence = %+v", r.Evidence)
		}
	})
	t.Run("nginx có SSL: cổng 443 từ chối bắt tay TLS thì đạt", func(t *testing.T) {
		f := rejectFixture(52, "000", empty, 35, "000", tlsRefused)
		r := defaultRejects("nginx", true, rejectName).Run(bg(), f)
		assertShape(t, r, id, StageWeb, Pass)
		assertEvidenceIsRan(t, r, f)
		if len(r.Evidence) != 2 || r.Evidence[1].Command != reject443 || r.Evidence[1].ExitCode != 35 {
			t.Errorf("Evidence = %+v", r.Evidence)
		}
	})
	t.Run("apache: 403 ở cả hai cổng thì đạt", func(t *testing.T) {
		f := rejectFixture(0, "403", "", 0, "403", "")
		assertShape(t, defaultRejects("apache", true, rejectName).Run(bg(), f), id, StageWeb, Pass)
	})

	wrong := []struct {
		name    string
		web     string
		f       *system.FakeSystem
		summary string
		recheck string
	}{
		{"nginx cổng 80 trả trang của một site", "nginx", rejectFixture(0, "200", "", 35, "000", tlsRefused), "cổng 80 trả mã 200", reject80},
		{"nginx cổng 80 chuyển hướng", "nginx", rejectFixture(0, "301", "", 35, "000", tlsRefused), "cổng 80 trả mã 301", reject80},
		{"nginx cổng 443 bắt tay được và trả 404", "nginx", rejectFixture(52, "000", empty, 0, "404", ""), "cổng 443 trả mã 404", reject443},
		{"nginx cổng 443 bắt tay được rồi mới đóng kết nối", "nginx", rejectFixture(52, "000", empty, 52, "000", empty), "cổng 443 cho kết quả lạ (curl mã thoát 52)", reject443},
		{"apache cổng 80 trả 200", "apache", rejectFixture(0, "200", "", 0, "403", ""), "cổng 80 trả mã 200", reject80},
		{"apache cổng 443 trả 200", "apache", rejectFixture(0, "403", "", 0, "200", ""), "cổng 443 trả mã 200", reject443},
		{"apache đóng kết nối thay vì 403", "apache", rejectFixture(52, "000", empty, 0, "403", ""), "cổng 80 cho kết quả lạ (curl mã thoát 52)", reject80},
	}
	for _, c := range wrong {
		t.Run("lỗi: "+c.name, func(t *testing.T) {
			r := defaultRejects(c.web, true, rejectName).Run(bg(), c.f)
			assertShape(t, r, id, StageWeb, Fail)
			if !strings.Contains(r.Summary.Vi, "Tên miền lạ không bị từ chối") || !strings.Contains(r.Summary.Vi, c.summary) {
				t.Errorf("Summary = %q", r.Summary.Vi)
			}
			if !strings.Contains(r.Cause.Vi, "hứng mọi tên miền") {
				t.Errorf("Cause = %q", r.Cause.Vi)
			}
			sitesDir := "/etc/nginx/sites-enabled"
			if c.web == "apache" {
				sitesDir = "/etc/apache2/sites-enabled"
			}
			if r.ManualFix[0] != "ls -l "+sitesDir || !containsStr(r.ManualFix, "sudo tpan health --fix") || lastFix(r) != c.recheck {
				t.Errorf("ManualFix = %q", r.ManualFix)
			}
			if len(r.Evidence) != 2 { // cổng 80 sai vẫn gọi cổng 443: bằng chứng phải đủ
				t.Errorf("Evidence = %+v", r.Evidence)
			}
		})
	}
	t.Run("lỗi: cả hai cổng đều sai thì Summary nêu cả hai", func(t *testing.T) {
		r := defaultRejects("nginx", true, rejectName).Run(bg(), rejectFixture(0, "200", "", 0, "200", ""))
		assertShape(t, r, id, StageWeb, Fail)
		if !strings.Contains(r.Summary.Vi, "cổng 80 trả mã 200") || !strings.Contains(r.Summary.Vi, "cổng 443 trả mã 200") || lastFix(r) != reject80 {
			t.Errorf("Summary = %q, ManualFix = %q", r.Summary.Vi, r.ManualFix)
		}
	})
	t.Run("lỗi: không kết nối được", func(t *testing.T) {
		f := rejectFixture(7, "000", "curl: (7) Failed to connect to 127.0.0.1 port 80 after 0 ms: Couldn't connect to server\n", 28, "000", "curl: (28) Connection timed out after 5001 milliseconds\n")
		r := defaultRejects("apache", true, rejectName).Run(bg(), f)
		assertShape(t, r, id, StageWeb, Fail)
		if !strings.Contains(r.Summary.Vi, "không kết nối được tới 127.0.0.1 cổng 80") || !strings.Contains(r.Summary.Vi, "cổng 443 không trả lời trong 5 giây") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
		if r.ManualFix[0] != "systemctl status apache2" || lastFix(r) != reject80 {
			t.Errorf("ManualFix = %q", r.ManualFix)
		}
	})
	t.Run("lệnh không chạy được", func(t *testing.T) {
		assertShape(t, defaultRejects("nginx", true, rejectName).Run(cancelledCtx(), system.NewFake()), id, StageWeb, Unknown)
	})
	t.Run("web lạ", func(t *testing.T) {
		f := system.NewFake()
		assertShape(t, DefaultRejects("caddy", true).Run(bg(), f), id, StageWeb, Unknown)
		assertNoCmd(t, f)
	})
	t.Run("tên thử là tpan-<8 hex>.invalid, mỗi lần dựng một tên", func(t *testing.T) {
		hostRE := regexp.MustCompile(`^Host: tpan-[0-9a-f]{8}\.invalid$`)
		seen := map[string]bool{}
		for i := 0; i < 3; i++ {
			f := rejectFixture(52, "000", empty, 35, "000", tlsRefused)
			assertShape(t, DefaultRejects("nginx", true).Run(bg(), f), id, StageWeb, Pass)
			ran := f.Ran()
			host := ran[0].Args[len(ran[0].Args)-2]
			if !hostRE.MatchString(host) {
				t.Fatalf("Host = %q", host)
			}
			name := strings.TrimPrefix(host, "Host: ")
			if ran[1].Args[len(ran[1].Args)-1] != "https://"+name+"/" || !containsStr(ran[1].Args, name+":443:127.0.0.1") {
				t.Errorf("lệnh cổng 443 không dùng cùng tên: %v", ran[1].Args)
			}
			seen[name] = true
		}
		if len(seen) != 3 {
			t.Errorf("tên thử lặp lại: %v", seen)
		}
	})
}

// Mọi phép kiểm của M14 chỉ đọc: hệ thống file giả không đổi sau khi chạy, và
// chỉ có các lệnh đọc được gọi.
func TestM14ChecksOnlyRead(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	f := logFixture(l)
	f.SetFile(templates.PHPHardeningINIPath("8.3"), templates.PHPHardeningINI(), 0o644, system.Root)
	f.SetFile(l.NginxConf, []byte(templates.Header+"\n"), 0o644, system.Root)
	f.SetFile(l.ApacheConf, []byte(templates.Header+"\n"), 0o644, system.Root)
	before := f.Snapshot()
	RunAll(bg(), f, []Check{
		OPcacheHardened("8.3"), PHPProtectProc("8.3"), LogPerms(l),
		VhostHardened("nginx", l, ""), VhostHardened("apache", l, site.HtaccessOff),
		HtaccessDirectives(l, site.HtaccessSafe),
		ServerNameUnique("nginx", []site.Layout{l}), ServerNameUnique("apache", []site.Layout{l}),
		DefaultRejects("nginx", true), DefaultRejects("apache", true),
	})
	for _, c := range f.Ran() {
		switch c.Name {
		case "php-fpm8.3", "systemctl", "find", "dd", "nginx", "apache2ctl", "curl":
		default:
			t.Errorf("lệnh không mong đợi: %s", c)
		}
		if c.Name == "systemctl" && c.Args[0] != "show" {
			t.Errorf("systemctl chỉ được đọc: %s", c)
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
