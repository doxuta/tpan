package core

// P7 của rà soát v0.2.0: tên user và tên đầy đủ của DB rời ("<chủ>_<tên>")
// không được trùng nhau, và job user.* luôn có chủ là admin (không để store
// suy chủ từ Target là tên user).

import (
	"context"
	"testing"

	"github.com/doxuta/tpan/internal/account"
)

func TestP7JobUserCoChuAdmin(t *testing.T) {
	ctx := context.Background()
	ue := newUsrEnv(t)
	usrHooks(t)
	// DB rời của bob tên bob_x; user cần khoá cũng có sites, DB để Plan không rỗng.
	ue.db("bob_x", "bob", nil)
	ue.site(t, "a.example.com", "bob", nil)

	sus, err := ue.a.PrepareSuspendUser(ctx, "bob", "", account.Root)
	if err != nil {
		t.Fatal(err)
	}
	uns, err := ue.a.PrepareUnsuspendUser(ctx, "bob")
	if err != nil {
		t.Fatal(err)
	}
	tr, err := ue.a.PrepareTrashAll(ctx, "bob")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []*Planned{sus, uns, tr} {
		if p.owner != account.AdminName {
			t.Errorf("%s: owner = %q, muốn %q (đặt rõ, không để store suy từ tên user)", p.Plan.Kind, p.owner, account.AdminName)
		}
	}
	// Chạy thật: engine ghi đúng chủ vào job.
	ue.as.put(account.User{Name: "carol", Role: account.RoleUser})
	p, err := ue.a.PrepareSuspendUser(ctx, "carol", "", account.Root)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := p.Execute(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Job.Owner != account.AdminName {
		t.Errorf("job user.suspend có Owner %q, muốn admin", rep.Job.Owner)
	}
}

func TestP7TenUserTrungTenDBRoi(t *testing.T) {
	ctx := context.Background()
	ue := newUsrEnv(t)
	ue.db("bob_a", "bob", nil)
	ue.db("bob_old", "bob", func(d *account.Database) { d.Status = account.DBTrashed })
	for _, name := range []string{"bob_a", "bob_old"} {
		_, _, err := ue.a.AddUser(ctx, UserInput{Name: mustUserName(t, name)})
		if got := refusedCode(t, err); got != CodeUserExists {
			t.Errorf("tạo user %s trùng tên DB rời: mã %q, muốn %q", name, got, CodeUserExists)
		}
		if _, ok, _ := ue.as.User(ctx, name); ok {
			t.Errorf("bị từ chối mà vẫn tạo user %s", name)
		}
	}
	// Tên không trùng vẫn tạo được.
	if _, _, err := ue.a.AddUser(ctx, UserInput{Name: mustUserName(t, "bob_b")}); err != nil {
		t.Errorf("bob_b: %v", err)
	}
}

func TestP7TenDBRoiTrungTenUser(t *testing.T) {
	e := newDBsEnv(t)
	e.acc.users["alice_shop"] = account.User{Name: "alice_shop", Role: account.RoleUser, Status: account.Active, Package: account.DefaultPackage}
	_, err := e.d.PrepareAdd(ctx, DBInput{Owner: dbsUser(t, "alice"), Name: "shop"})
	if got := refusedCode(t, err); got != CodeDBExists {
		t.Errorf("DB rời alice_shop trùng tên user: mã %q, muốn %q", got, CodeDBExists)
	}
	if len(e.sim.Ran()) != 0 {
		t.Errorf("từ chối phải trước mọi lệnh MariaDB, đã chạy %d lệnh", len(e.sim.Ran()))
	}
	if _, err := e.d.PrepareAdd(ctx, DBInput{Owner: dbsUser(t, "alice"), Name: "blog"}); err != nil {
		t.Errorf("alice_blog: %v", err)
	}
}
