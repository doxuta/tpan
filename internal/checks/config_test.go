package checks

import (
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/system"
)

func TestNginxConfig(t *testing.T) {
	t.Run("pass", func(t *testing.T) {
		// Dạng thật của `nginx -t` khi đúng: cả hai dòng nằm ở stderr, mã thoát 0.
		fake := system.NewFake()
		reply(fake, []string{"nginx", "-t"}, 0, "",
			"nginx: the configuration file /etc/nginx/nginx.conf syntax is ok\n"+
				"nginx: configuration file /etc/nginx/nginx.conf test is successful\n")
		r := NginxConfig().Run(bg(), fake)
		assertShape(t, r, "config.nginx:nginx", StageWeb, Pass)
		assertEvidenceIsRan(t, r, fake)
		if r.Evidence[0].Command != "nginx -t" || !strings.Contains(r.Evidence[0].Output, "test is successful") {
			t.Errorf("Evidence = %+v", r.Evidence[0])
		}
	})
	t.Run("fail", func(t *testing.T) {
		fake := system.NewFake()
		reply(fake, []string{"nginx", "-t"}, 1, "",
			"nginx: [emerg] unknown directive \"servr_name\" in /etc/nginx/sites-enabled/tpan-example.com.conf:3\n"+
				"nginx: configuration file /etc/nginx/nginx.conf test failed\n")
		r := NginxConfig().Run(bg(), fake)
		assertShape(t, r, "config.nginx:nginx", StageWeb, Fail)
		assertEvidenceIsRan(t, r, fake)
		if !strings.Contains(r.Summary.Vi, "tpan-example.com.conf:3") {
			t.Errorf("Summary phải nêu dòng [emerg]: %q", r.Summary.Vi)
		}
		if lastFix(r) != "nginx -t" {
			t.Errorf("dòng cuối ManualFix = %q", lastFix(r))
		}
	})
	t.Run("unknown", func(t *testing.T) {
		r := NginxConfig().Run(cancelledCtx(), system.NewFake())
		assertShape(t, r, "config.nginx:nginx", StageWeb, Unknown)
	})
}

func TestApacheConfig(t *testing.T) {
	t.Run("pass kể cả khi có cảnh báo ServerName", func(t *testing.T) {
		// Dạng thật trên Ubuntu: AH00558 là cảnh báo, "Syntax OK" ở cuối, mã thoát 0.
		fake := system.NewFake()
		reply(fake, []string{"apachectl", "configtest"}, 0, "",
			"AH00558: apache2: Could not reliably determine the server's fully qualified domain name, using 127.0.1.1. Set the 'ServerName' directive globally to suppress this message\n"+
				"Syntax OK\n")
		r := ApacheConfig().Run(bg(), fake)
		assertShape(t, r, "config.apache:apache2", StageWeb, Pass)
		assertEvidenceIsRan(t, r, fake)
		if r.Evidence[0].Command != "apachectl configtest" {
			t.Errorf("Command = %q", r.Evidence[0].Command)
		}
	})
	t.Run("fail", func(t *testing.T) {
		fake := system.NewFake()
		reply(fake, []string{"apachectl", "configtest"}, 1, "",
			"AH00526: Syntax error on line 5 of /etc/apache2/sites-enabled/000-tpan-default.conf:\n"+
				"Invalid command 'Listenn', perhaps misspelled or defined by a module not included in the server configuration\n"+
				"Action 'configtest' failed.\n"+
				"The Apache error log may have more information.\n")
		r := ApacheConfig().Run(bg(), fake)
		assertShape(t, r, "config.apache:apache2", StageWeb, Fail)
		assertEvidenceIsRan(t, r, fake)
		if !strings.Contains(r.Summary.Vi, "000-tpan-default.conf") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
		if lastFix(r) != "apachectl configtest" {
			t.Errorf("dòng cuối ManualFix = %q", lastFix(r))
		}
	})
	t.Run("unknown", func(t *testing.T) {
		assertShape(t, ApacheConfig().Run(cancelledCtx(), system.NewFake()), "config.apache:apache2", StageWeb, Unknown)
	})
}

func TestPHPFPMConfig(t *testing.T) {
	t.Run("pass", func(t *testing.T) {
		fake := system.NewFake()
		reply(fake, []string{"php-fpm8.3", "-t"}, 0, "",
			"[19-Nov-2026 10:00:00] NOTICE: configuration file /etc/php/8.3/fpm/php-fpm.conf test is successful\n")
		r := PHPFPMConfig("8.3").Run(bg(), fake)
		assertShape(t, r, "config.phpfpm:8.3", StagePHP, Pass)
		assertEvidenceIsRan(t, r, fake)
		if r.Evidence[0].Command != "php-fpm8.3 -t" {
			t.Errorf("Command = %q", r.Evidence[0].Command)
		}
	})
	t.Run("fail", func(t *testing.T) {
		fake := system.NewFake()
		reply(fake, []string{"php-fpm8.3", "-t"}, 78, "",
			"[19-Nov-2026 10:00:00] ERROR: [/etc/php/8.3/fpm/pool.d/tpan-example.com.conf:4] unknown entry 'pmm'\n"+
				"[19-Nov-2026 10:00:00] ERROR: failed to post process the configuration\n"+
				"[19-Nov-2026 10:00:00] ERROR: FPM initialization failed\n")
		r := PHPFPMConfig("8.3").Run(bg(), fake)
		assertShape(t, r, "config.phpfpm:8.3", StagePHP, Fail)
		if !strings.Contains(r.Summary.Vi, "unknown entry 'pmm'") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
		if lastFix(r) != "php-fpm8.3 -t" || r.ManualFix[0] != "ls -l /etc/php/8.3/fpm/pool.d" {
			t.Errorf("ManualFix = %q", r.ManualFix)
		}
	})
	t.Run("không chạy được", func(t *testing.T) {
		assertShape(t, PHPFPMConfig("8.3").Run(cancelledCtx(), system.NewFake()), "config.phpfpm:8.3", StagePHP, Unknown)
	})
	t.Run("bản sai dạng", func(t *testing.T) {
		for _, v := range []string{"", "8", "8.3.1", "abc", "8.3; id", "../8.3", "-8.3", "8.3\n", "8.x"} {
			fake := system.NewFake()
			r := PHPFPMConfig(v).Run(bg(), fake)
			if r.Status != Unknown {
				t.Errorf("version %q: Status = %q, muốn unknown", v, r.Status)
			}
			assertNoCmd(t, fake)
		}
	})
}

func TestEvidenceBiCatKhiOutputQuaDai(t *testing.T) {
	fake := system.NewFake()
	long := strings.Repeat("nginx: [warn] dòng cảnh báo rất dài lặp đi lặp lại\n", 500)
	reply(fake, []string{"nginx", "-t"}, 1, "", long+"nginx: [emerg] LOI CUOI in /etc/nginx/nginx.conf:9\n")
	r := NginxConfig().Run(bg(), fake)
	assertShape(t, r, "config.nginx:nginx", StageWeb, Fail)
	out := r.Evidence[0].Output
	if len(out) > maxEvidenceBytes+100 || !strings.Contains(out, "đã cắt") || !strings.HasSuffix(out, "nginx.conf:9") {
		t.Errorf("Output dài %d byte, đầu %q, cuối %q", len(out), out[:60], out[len(out)-40:])
	}
}
