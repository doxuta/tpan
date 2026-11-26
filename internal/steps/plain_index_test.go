package steps

import (
	"context"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

func TestPlainIndex(t *testing.T) {
	d, err := validate.ParseDomain("example.com")
	if err != nil {
		t.Fatal(err)
	}
	l := site.LayoutOf(d)
	ctx := context.Background()
	m := system.NewFake()
	m.Handle([]string{"dd"}, func(c system.Cmd) system.Result {
		if c.AsUser != l.User {
			t.Errorf("ghi trang mẫu dưới user %q, muốn %q", c.AsUser, l.User)
		}
		m.SetFile(strings.TrimPrefix(c.Args[0], "of="), c.Stdin, 0o640, system.Owner{User: c.AsUser, Group: c.AsUser})
		return system.Result{}
	})
	s := &PlainIndex{Layout: l}
	if ok, err := s.Check(ctx, m); err != nil || ok {
		t.Fatalf("public trống: Check = %v, %v", ok, err)
	}
	if err := s.Apply(ctx, m); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.Check(ctx, m); !ok {
		t.Fatal("sau Apply mà Check vẫn false")
	}
	// Đã có index.html của chủ site: không ghi gì.
	m2 := system.NewFake()
	m2.SetFile(l.Public+"/index.html", []byte("x"), 0o640, system.Owner{User: l.User, Group: l.User})
	if ok, err := (&PlainIndex{Layout: l}).Check(ctx, m2); err != nil || !ok {
		t.Errorf("có index.html: Check = %v, %v", ok, err)
	}
}

func TestPlainServeAnswer(t *testing.T) {
	for code, pass := range map[string]bool{"200": true, "301": true, "404": true, "403": false, "500": false, "502": false, "": false, "000": false} {
		if err := plainServeAnswer(nameAnswer{code: code}); (err == nil) != pass {
			t.Errorf("mã %q: err=%v, muốn đạt=%v", code, err, pass)
		}
	}
}
