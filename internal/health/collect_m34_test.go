package health

import (
	"context"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/system"
)

// M34: DB rời đang chạy có phép kiểm db.exists gắn vào máy; DB bị xoá ngoài tpan
// mở một sự cố, có lại (hoặc được gỡ khỏi tpan) thì sự cố đóng. DB rời trong
// thùng rác (đã DROP có chủ ý) không có phép kiểm này.
func TestRunDBRoiBiXoaNgoaiTpan(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	for _, d := range []account.Database{
		{Name: "app_kho", Owner: "admin", Status: account.DBActive, Created: t0},
		{Name: "app_cu", Owner: "admin", Status: account.DBTrashed, Created: t0},
	} {
		if err := e.st.SaveStandaloneDB(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	state := "1\t1\n" // DB và user DB đều còn
	e.sys.Handle([]string{"mariadb", "-N", "-B"}, func(c system.Cmd) system.Result {
		if strings.Contains(string(c.Stdin), "information_schema.SCHEMATA WHERE SCHEMA_NAME='app_kho'") {
			return system.Result{Stdout: state}
		}
		return system.Result{}
	})
	const id = "db.exists:app_kho"
	find := func(rep Report) checks.Result {
		t.Helper()
		for _, g := range rep.Groups {
			for _, r := range g.Results {
				if r.ID == id {
					if g.Target != "" {
						t.Errorf("%s gắn vào %q, muốn gắn vào máy", id, g.Target)
					}
					return r
				}
			}
		}
		t.Fatalf("không có phép kiểm %s", id)
		return checks.Result{}
	}

	rep := e.run(t, Options{Full: true})
	if r := find(rep); r.Status != checks.Pass {
		t.Fatalf("DB còn: %s %q", r.Status, r.Summary.Vi)
	}
	if _, ok := targets(t, rep)["db.exists:app_cu"]; ok {
		t.Error("DB rời trong thùng rác không được có db.exists")
	}

	// Ai đó DROP DATABASE bằng tay.
	state = "0\t1\n"
	rep = e.run(t, Options{})
	if r := find(rep); r.Status != checks.Fail || !strings.Contains(r.Summary.Vi, "không còn trong MariaDB") {
		t.Fatalf("DB bị xoá ngoài tpan: %s %q", r.Status, r.Summary.Vi)
	}
	if !openFor(t, e, id) {
		t.Fatal("DB mất mà không mở sự cố")
	}

	// Gỡ dòng của DB khỏi tpan (việc của "tpan db rm"): phép kiểm không còn, sự cố đóng vì "gone".
	if err := e.st.DeleteStandaloneDB(ctx, "app_kho"); err != nil {
		t.Fatal(err)
	}
	rep = e.run(t, Options{Full: true})
	if _, ok := targets(t, rep)[id]; ok {
		t.Error("DB đã gỡ mà vẫn có phép kiểm")
	}
	if openFor(t, e, id) {
		t.Error("DB đã gỡ mà sự cố vẫn mở")
	}
}
