package core

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

func TestDatabasesEmptySkipsQuery(t *testing.T) {
	sys := system.NewFake()
	got, err := (&Sites{Sys: sys, Store: newMemSites()}).Databases(context.Background())
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("Databases = %v, %v; muốn danh sách rỗng khác nil", got, err)
	}
	if len(sys.Ran()) != 0 {
		t.Fatalf("không có site mà vẫn chạy lệnh: %v", sys.Ran())
	}
}

func TestDatabases(t *testing.T) {
	ms := newMemSites()
	for _, d := range []string{"b.example.com", "a.example.com", "c.example.com"} {
		if err := ms.SaveSite(context.Background(), site.Site{Domain: mustDomain(t, d), Status: site.Active}); err != nil {
			t.Fatal(err)
		}
	}
	db := func(d string) string { return site.LayoutOf(mustDomain(t, d)).DB }
	a, b, c := db("a.example.com"), db("b.example.com"), db("c.example.com")
	sys := system.NewFake()
	var sql string
	sys.Handle([]string{"mariadb", "-N", "-B"}, func(cmd system.Cmd) system.Result {
		sql = string(cmd.Stdin)
		// c.example.com không có DB (không có dòng); a có DB rỗng.
		return system.Result{Stdout: a + "\t0\t0\n" + b + "\t12\t1638400\ndòng lạ\n"}
	})
	got, err := (&Sites{Sys: sys, Store: ms}).Databases(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []Database{
		{Domain: "a.example.com", DB: a, User: a, SizeBytes: 0, Tables: 0},
		{Domain: "b.example.com", DB: b, User: b, SizeBytes: 1638400, Tables: 12},
		{Domain: "c.example.com", DB: c, User: c, SizeBytes: -1, Tables: -1},
	}
	if len(got) != len(want) {
		t.Fatalf("Databases = %+v", got)
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("dòng %d = %+v, muốn %+v", i, got[i], want[i])
		}
	}
	if n := len(sys.Ran()); n != 1 {
		t.Fatalf("chạy %d lệnh, muốn đúng một câu SQL", n)
	}
	for _, s := range []string{"IN ('" + a + "', '" + b + "', '" + c + "')", "LEFT JOIN information_schema.TABLES"} {
		if !strings.Contains(sql, s) {
			t.Errorf("câu SQL thiếu %q:\n%s", s, sql)
		}
	}
	if strings.Contains(sql, "LIKE") {
		t.Errorf("câu SQL dùng LIKE (dấu _ là ký tự đại diện):\n%s", sql)
	}
}

func TestDatabasesMariaDBDown(t *testing.T) {
	ms := newMemSites()
	if err := ms.SaveSite(context.Background(), site.Site{Domain: mustDomain(t, "a.example.com"), Status: site.Active}); err != nil {
		t.Fatal(err)
	}
	sys := system.NewFake()
	sys.Handle([]string{"mariadb"}, func(system.Cmd) system.Result {
		return system.Result{ExitCode: 1, Stderr: "Can't connect to local server through socket"}
	})
	got, err := (&Sites{Sys: sys, Store: ms}).Databases(context.Background())
	if err != nil {
		t.Fatalf("MariaDB lỗi không được làm hỏng cả danh sách: %v", err)
	}
	if len(got) != 1 || got[0].SizeBytes != -1 || got[0].Tables != -1 {
		t.Fatalf("Databases = %+v, muốn một dòng với -1", got)
	}
}
