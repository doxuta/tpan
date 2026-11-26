package checks

import (
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

func TestAliasJudgePlain(t *testing.T) {
	for code, pass := range map[string]bool{"200": true, "302": true, "404": true, "403": false, "500": false, "": false} {
		if why := aliasJudge(aliasAnswer{code: code}, site.ModeServe, "", true); (why == "") != pass {
			t.Errorf("site PHP trống, mã %q: %q, muốn đạt=%v", code, why, pass)
		}
	}
	// Kiểu redirect không đổi theo loại site.
	if why := aliasJudge(aliasAnswer{code: "200"}, site.ModeRedirect, "http://example.com/", true); why == "" {
		t.Error("redirect mà 200 vẫn đạt")
	}
	// Site WordPress vẫn đòi dấu hiệu WordPress.
	if why := aliasJudge(aliasAnswer{code: "200", body: "hello"}, site.ModeServe, "", false); why == "" {
		t.Error("WordPress: trang không có dấu hiệu WordPress mà đạt")
	}
}

// Site đích là site PHP trống (không có wp-config.php): phép 1 được bỏ, bốn
// phép còn lại vẫn chạy và vẫn bắt được rò rỉ.
func TestIsolatedToPlain(t *testing.T) {
	a := siteLayout(t, "shop.example.com")
	b := siteLayout(t, "blog.example.org")
	id := "isolation:shop.example.com:blog.example.org"
	sock := b.SocketFor("8.3")
	plain := func() *system.FakeSystem {
		f := system.NewFake()
		f.SetFile(b.Public+"/index.php", []byte("<?php"), 0o640, system.Owner{User: b.User, Group: b.User})
		f.SetFile(b.AccessLog, nil, 0o640, system.Owner{User: "root", Group: b.User})
		f.SetSocket(sock, 0o660, wwwData)
		return f
	}
	f := plain()
	reply(f, []string{"test"}, 1, "", "")
	r := IsolatedTo(a, b, true).Run(bg(), f)
	assertShape(t, r, id, StageWordPress, Pass)
	var ran []string
	for _, c := range f.Ran() {
		ran = append(ran, strings.Join(c.Args, " "))
	}
	want := []string{"-x " + b.Dir, "-w " + b.Public, "-r " + b.AccessLog, "-w " + sock}
	if strings.Join(ran, "\n") != strings.Join(want, "\n") {
		t.Fatalf("lệnh đã chạy = %q, muốn %q", ran, want)
	}
	// Một phép lọt (mã 0) vẫn là lỗi.
	f = plain()
	reply(f, []string{"test"}, 0, "", "")
	assertShape(t, IsolatedTo(a, b, true).Run(bg(), f), id, StageWordPress, Fail)
	// Không báo là site PHP trống thì thiếu wp-config.php vẫn là "chưa kiểm được".
	f = plain()
	reply(f, []string{"test"}, 1, "", "")
	assertShape(t, IsolatedTo(a, b, false).Run(bg(), f), id, StageWordPress, Unknown)
}
