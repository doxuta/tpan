package checks

import (
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/system"
)

// ---- Isolated: socket của lồng ở site đích ----

// isolatedFixture dựng site đích b đủ năm đích như TestIsolated (wp-config, log,
// socket của pool).
func isolatedFixture(t *testing.T) (*system.FakeSystem, string) {
	t.Helper()
	b := siteLayout(t, "blog.example.org")
	f := system.NewFake()
	f.SetFile(b.WPConfig, []byte("<?php"), 0o600, system.Owner{User: b.User, Group: b.User})
	f.SetFile(b.AccessLog, nil, 0o640, system.Owner{User: "root", Group: b.User})
	sock := b.SocketFor("8.3")
	f.SetSocket(sock, 0o660, wwwData)
	return f, sock
}

func TestIsolatedCageSocket(t *testing.T) {
	a := siteLayout(t, "shop.example.com")
	b := siteLayout(t, "blog.example.org")
	id := "isolation:shop.example.com:blog.example.org"

	t.Run("không có socket của lồng: bằng chứng và Summary y như trước M17", func(t *testing.T) {
		f, _ := isolatedFixture(t)
		reply(f, []string{"test"}, 1, "", "")
		r := Isolated(a, b).Run(bg(), f)
		assertShape(t, r, id, StageWordPress, Pass)
		assertEvidenceIsRan(t, r, f)
		if len(r.Evidence) != 5 || !strings.Contains(r.Summary.Vi, "(5 phép thử)") || strings.Contains(r.Summary.Vi, "bỏ") {
			t.Errorf("Evidence = %d, Summary = %q", len(r.Evidence), r.Summary.Vi)
		}
		for _, e := range r.Evidence {
			if strings.Contains(e.Command, "tpan-cage") || strings.Contains(e.Command, b.CageSocket()) {
				t.Errorf("không được nhắc tới lồng: %q", e.Command)
			}
		}
	})
	t.Run("có socket của lồng: thử thêm, bị từ chối thì đạt", func(t *testing.T) {
		f, _ := isolatedFixture(t)
		f.SetSocket(b.CageSocket(), 0o660, wwwData)
		reply(f, []string{"test"}, 1, "", "")
		r := Isolated(a, b).Run(bg(), f)
		assertShape(t, r, id, StageWordPress, Pass)
		assertEvidenceIsRan(t, r, f)
		ran := f.Ran()
		if len(ran) != 6 {
			t.Fatalf("đã chạy %d lệnh, muốn 6", len(ran))
		}
		if last := ran[5]; last.AsUser != a.User || strings.Join(last.Args, " ") != "-w "+b.CageSocket() {
			t.Errorf("phép thử cuối = %+v", last)
		}
		if !strings.Contains(r.Summary.Vi, "(6 phép thử)") {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
	})
	t.Run("site đích chỉ có socket của lồng (không còn pool chung)", func(t *testing.T) {
		f := system.NewFake()
		f.SetFile(b.WPConfig, []byte("<?php"), 0o600, system.Owner{User: b.User, Group: b.User})
		f.SetFile(b.AccessLog, nil, 0o640, system.Owner{User: "root", Group: b.User})
		f.SetSocket(b.CageSocket(), 0o660, wwwData)
		reply(f, []string{"test"}, 1, "", "")
		r := Isolated(a, b).Run(bg(), f)
		assertShape(t, r, id, StageWordPress, Pass)
		if len(f.Ran()) != 5 || strings.Contains(r.Summary.Vi, "bỏ") {
			t.Errorf("đã chạy %d lệnh, Summary = %q", len(f.Ran()), r.Summary.Vi)
		}
	})
	t.Run("socket của lồng ghi được: lỗi, sửa tay chỉ tới unit socket", func(t *testing.T) {
		f, _ := isolatedFixture(t)
		f.SetSocket(b.CageSocket(), 0o660, wwwData)
		reply(f, []string{"test"}, 1, "", "")
		reply(f, []string{"test", "-w", b.CageSocket()}, 0, "", "")
		r := Isolated(a, b).Run(bg(), f)
		assertShape(t, r, id, StageWordPress, Fail)
		if !strings.Contains(r.Summary.Vi, "ghi được vào socket PHP-FPM "+b.CageSocket()) {
			t.Errorf("Summary = %q", r.Summary.Vi)
		}
		if !containsStr(r.ManualFix, "systemctl cat "+b.CageSocketUnit()+" --no-pager") || strings.Contains(strings.Join(r.ManualFix, "\n"), "grep -n '^listen'") {
			t.Errorf("ManualFix = %q", r.ManualFix)
		}
		if want := "runuser -u " + a.User + " -- test -w " + b.CageSocket(); lastFix(r) != want {
			t.Errorf("ManualFix cuối = %q, muốn %q", lastFix(r), want)
		}
	})
	t.Run("dòng kiểm lại dùng Describe của System", func(t *testing.T) {
		f, _ := isolatedFixture(t)
		reply(f, []string{"test"}, 0, "", "")
		r := Isolated(a, b).Run(bg(), descSys{f})
		assertShape(t, r, id, StageWordPress, Fail)
		for _, s := range r.ManualFix[len(r.ManualFix)-5:] {
			if !strings.HasPrefix(s, "systemd-run --unit=x -p User="+a.User+" -- test ") {
				t.Errorf("dòng kiểm lại = %q", s)
			}
		}
	})
}

// ---- Dòng sửa tay dựng bằng system.Describe ----

func TestWPChecksumsManualFixUsesDescribe(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	failOut := "Warning: File doesn't verify against checksum: wp-includes/version.php\nError: WordPress installation doesn't verify against checksums.\n"

	t.Run("site ngoài lồng: giữ nguyên dạng runuser như trước", func(t *testing.T) {
		f := system.NewFake()
		reply(f, []string{"wp"}, 1, "", failOut)
		r := WPChecksums(l).Run(bg(), f)
		assertShape(t, r, "wp.checksums:shop.example.com", StageWordPress, Fail)
		u := "runuser -u " + l.User + " -- "
		want := []string{
			u + "wp core verify-checksums --path=" + l.Public,
			u + "wp core download --force --skip-content --path=" + l.Public + ` --version="$(` + u + "wp core version --path=" + l.Public + `)"`,
			u + "wp core verify-checksums --path=" + l.Public,
		}
		if strings.Join(r.ManualFix, "\n") != strings.Join(want, "\n") {
			t.Errorf("ManualFix = %q\nmuốn %q", r.ManualFix, want)
		}
	})
	t.Run("site trong lồng: systemd-run, không có dòng runuser nào", func(t *testing.T) {
		f := system.NewFake()
		reply(f, []string{"wp"}, 1, "", failOut)
		r := WPChecksums(l).Run(bg(), descSys{f})
		assertShape(t, r, "wp.checksums:shop.example.com", StageWordPress, Fail)
		d := "systemd-run --unit=x -p User=" + l.User + " -- "
		want := []string{
			d + "wp core verify-checksums --path=" + l.Public,
			d + "wp core download --force --skip-content --path=" + l.Public + ` --version="$(` + d + "wp core version --path=" + l.Public + `)"`,
			d + "wp core verify-checksums --path=" + l.Public,
		}
		if strings.Join(r.ManualFix, "\n") != strings.Join(want, "\n") {
			t.Errorf("ManualFix = %q\nmuốn %q", r.ManualFix, want)
		}
		for _, line := range r.ManualFix {
			if strings.Contains(line, "runuser") {
				t.Errorf("còn dòng runuser: %q", line)
			}
		}
		if got := r.Evidence[0].Command; got != want[0] {
			t.Errorf("bằng chứng = %q, muốn %q", got, want[0])
		}
	})
}

func TestDBAccessManualFixUsesDescribe(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	f := system.NewFake()
	reply(f, []string{"wp"}, 1, "", "ERROR 1045 (28000): Access denied for user")
	r := DBAccess(l).Run(bg(), descSys{f})
	assertShape(t, r, "db.access:shop.example.com", StageDB, Fail)
	want := "systemd-run --unit=x -p User=" + l.User + " -- wp db query 'SHOW DATABASES' --skip-column-names --path=" + l.Public
	if lastFix(r) != want {
		t.Errorf("ManualFix cuối = %q, muốn %q", lastFix(r), want)
	}
	// Site ngoài lồng: như trước.
	f = system.NewFake()
	reply(f, []string{"wp"}, 1, "", "ERROR 1045 (28000): Access denied for user")
	r = DBAccess(l).Run(bg(), f)
	if !strings.HasPrefix(lastFix(r), "runuser -u "+l.User+" -- wp db query") {
		t.Errorf("ManualFix cuối = %q", lastFix(r))
	}
}

func TestHTTPSiteManualFixUsesDescribe(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	f := system.NewFake()
	reply(f, []string{"curl"}, 0, httpOut("", "301"), "")
	r := HTTPSite(l).Run(bg(), descSys{f})
	assertShape(t, r, "http.site:shop.example.com", StageEndToEnd, Fail)
	want := "systemd-run --unit=x -p User=" + l.User + " -- wp option get home --path=" + l.Public
	if !containsStr(r.ManualFix, want) {
		t.Errorf("ManualFix = %q, muốn có %q", r.ManualFix, want)
	}
	for _, line := range r.ManualFix {
		if strings.Contains(line, "runuser") {
			t.Errorf("còn dòng runuser: %q", line)
		}
	}
	// Site ngoài lồng: như trước.
	f = system.NewFake()
	reply(f, []string{"curl"}, 0, httpOut("", "301"), "")
	r = HTTPSite(l).Run(bg(), f)
	if !containsStr(r.ManualFix, "runuser -u "+l.User+" -- wp option get home --path="+l.Public) {
		t.Errorf("ManualFix = %q", r.ManualFix)
	}
}
