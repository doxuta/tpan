package core

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/system"
)

// dbsDown là máy mà MariaDB không trả lời: mọi lệnh mariadb lỗi, còn lại như sim.
type dbsDown struct{ *dbsSim }

func (d dbsDown) Run(ctx context.Context, c system.Cmd) (system.Result, error) {
	if c.Name == "mariadb" {
		return system.Result{ExitCode: 1, Stderr: "ERROR 2002 (HY000): Can't connect to local server through socket"}, errors.New("mariadb: mã thoát 1")
	}
	return d.dbsSim.Run(ctx, c)
}

// dropOutside xoá DB hay user DB thẳng trong MariaDB giả, như admin gõ DROP bằng tay.
func (e *dbsEnv) dropOutside(name string, db, user bool) {
	e.sim.mu.Lock()
	defer e.sim.mu.Unlock()
	if db {
		delete(e.sim.dbs, name)
	}
	if user {
		delete(e.sim.users, name)
	}
}

func m34StepIDs(p *Planned) string {
	var ids []string
	for _, s := range p.Plan.Steps {
		ids = append(ids, strings.SplitN(s.Info().ID, ":", 2)[0])
	}
	return strings.Join(ids, " ")
}

// DB rời đã mất khỏi MariaDB: "xoá" là gỡ dòng của nó khỏi tpan, kể cả khi DB
// đang tạm khoá (trước M34: không vào thùng rác được, không xoá hẳn được, và
// chủ của nó không xoá được).
func TestDBRoiDaMatThiXoaLaGoKhoiTpan(t *testing.T) {
	for _, tc := range []struct {
		name          string
		userGone      bool
		suspended     account.Suspension
		wantInTitleVi string
	}{
		{"mất DB, user còn", false, account.NotSuspended, "xoá user DB còn sót"},
		{"mất cả DB lẫn user", true, account.NotSuspended, "không có gì để đưa vào thùng rác"},
		{"mất DB khi đang tạm khoá", true, account.SuspendSelf, "không có gì để đưa vào thùng rác"},
		{"mất DB khi đang khoá theo user", false, account.SuspendByUser, "xoá user DB còn sót"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newDBsEnv(t)
			e.add(t)
			if tc.suspended != account.NotSuspended {
				db := e.store.dbs["alice_shop"]
				db.Suspended = tc.suspended
				e.store.dbs["alice_shop"] = db
			}
			e.dropOutside("alice_shop", true, tc.userGone)

			p, err := e.d.PrepareTrash(ctx, "alice_shop")
			if err != nil {
				t.Fatalf("PrepareTrash: %v", err)
			}
			if p.Plan.Kind != "dbs.forget" || p.Plan.Target != "alice_shop" {
				t.Fatalf("Plan = %s %s, muốn dbs.forget alice_shop", p.Plan.Kind, p.Plan.Target)
			}
			if got := m34StepIDs(p); got != "mariadb.user-absent mariadb.trash-dump-absent" {
				t.Errorf("các bước = %q: không được có bước xuất hay DROP DATABASE", got)
			}
			if !strings.Contains(p.Plan.Title.Vi, "không còn trong MariaDB") || !strings.Contains(p.Plan.Title.Vi, tc.wantInTitleVi) || p.Plan.Title.En == "" {
				t.Errorf("tiêu đề phải nói rõ vì sao không vào thùng rác: %q", p.Plan.Title.Vi)
			}
			if _, err := p.Preview(ctx); err != nil {
				t.Fatalf("Preview: %v", err)
			}
			// Xem trước không đổi gì.
			if _, ok := e.store.dbs["alice_shop"]; !ok {
				t.Fatal("xem trước mà đã xoá dòng")
			}
			dbsExec(t)(p, nil)
			if _, ok := e.store.dbs["alice_shop"]; ok {
				t.Error("dòng của DB còn trong tpan sau khi gỡ")
			}
			if _, ok := e.sim.users["alice_shop"]; ok {
				t.Error("user DB còn sót sau khi gỡ")
			}
			if _, err := e.sim.Stat(account.DBTrashDump("alice_shop")); err == nil {
				t.Error("có file xuất trong thùng rác dù không có gì để xuất")
			}
			// Gỡ rồi thì tên dùng lại được.
			if _, err := e.d.PrepareAdd(ctx, DBInput{Owner: dbsUser(t, "alice"), Name: "shop"}); err != nil {
				t.Errorf("tạo lại DB cùng tên sau khi gỡ: %v", err)
			}
		})
	}
}

// DB đã mất: mọi thao tác cần DB từ chối bằng một lời chỉ đường (db_missing),
// không chạy job rồi hỏng giữa chừng, và không đổi gì.
func TestDBRoiDaMatCacThaoTacKhacTuChoi(t *testing.T) {
	e := newDBsEnv(t)
	e.add(t)
	e.dropOutside("alice_shop", true, true)
	before := e.store.dbs["alice_shop"]
	check := func(what string, err error) {
		t.Helper()
		var re *RefusedError
		if !errors.As(err, &re) || re.Code != CodeDBMissing {
			t.Errorf("%s: %v, muốn db_missing", what, err)
			return
		}
		if !strings.Contains(re.Message.Vi, "sudo tpan db rm alice_shop") || !strings.Contains(re.Message.En, "sudo tpan db rm alice_shop") {
			t.Errorf("%s: lời từ chối thiếu lệnh gỡ: %q", what, re.Message.Vi)
		}
	}
	_, err := e.d.PreparePassword(ctx, "alice_shop")
	check("đổi mật khẩu", err)
	_, err = e.d.PrepareSuspend(ctx, "alice_shop")
	check("tạm khoá", err)
	_, err = e.d.PrepareCharset(ctx, "alice_shop", "utf8mb4", "utf8mb4_unicode_ci")
	check("đổi charset", err)
	// Đang tạm khoá (khoá từ trước khi DB mất): mở khoá cũng chỉ đường gỡ.
	db := before
	db.Suspended = account.SuspendSelf
	e.store.dbs["alice_shop"] = db
	_, err = e.d.PrepareUnsuspend(ctx, "alice_shop")
	check("mở khoá", err)
	e.store.dbs["alice_shop"] = before
	if got := e.store.dbs["alice_shop"]; got != before {
		t.Errorf("dòng bị đổi sau các lần từ chối: %+v", got)
	}
	if len(e.sim.users) != 0 || len(e.sim.dbs) != 0 {
		t.Errorf("MariaDB bị đổi sau các lần từ chối: %v %v", e.sim.dbs, e.sim.users)
	}
}

// Chỉ mất user DB, DB và dữ liệu còn: tạm khoá, mở khoá từ chối và chỉ sang
// đổi mật khẩu; đổi mật khẩu tạo lại user (khoá nếu DB đang tạm khoá).
func TestDBRoiMatUserDB(t *testing.T) {
	e := newDBsEnv(t)
	e.add(t)
	e.sim.mu.Lock()
	d := e.sim.dbs["alice_shop"]
	d.data = "dữ liệu"
	e.sim.dbs["alice_shop"] = d
	e.sim.mu.Unlock()
	e.dropOutside("alice_shop", false, true)

	// Tạm khoá: trước M34 job báo thành công dù không khoá được gì.
	_, err := e.d.PrepareSuspend(ctx, "alice_shop")
	var re *RefusedError
	if !errors.As(err, &re) || re.Code != CodeDBUserMissing || !strings.Contains(re.Message.Vi, "sudo tpan db passwd alice_shop") || !strings.Contains(re.Message.Vi, "không khoá được") {
		t.Fatalf("tạm khoá khi user DB đã mất: %v", err)
	}
	if e.store.dbs["alice_shop"].Suspended != account.NotSuspended {
		t.Fatal("từ chối mà dòng vẫn mang cờ tạm khoá")
	}

	// Đổi mật khẩu = tạo lại user, không khoá, có quyền, dữ liệu nguyên.
	pw, err := e.d.PreparePassword(ctx, "alice_shop")
	if err != nil {
		t.Fatalf("PreparePassword: %v", err)
	}
	if got := m34StepIDs(pw.Planned); got != "mariadb.user-recreate" || pw.Plan.Kind != "dbs.passwd" || !strings.Contains(pw.Plan.Title.Vi, "Tạo lại user DB") {
		t.Fatalf("Plan = %s, bước %q, tiêu đề %q", pw.Plan.Kind, got, pw.Plan.Title.Vi)
	}
	dbsExec(t)(pw.Planned, nil)
	u, ok := e.sim.users["alice_shop"]
	if !ok || u.pw != pw.Password.Reveal() || u.locked {
		t.Fatalf("user sau khi tạo lại = %+v (có=%v)", u, ok)
	}
	if e.sim.dbs["alice_shop"].data != "dữ liệu" {
		t.Error("dữ liệu của DB bị đụng")
	}
	// Có user rồi thì đổi mật khẩu lại là ALTER USER như thường.
	pw2, err := e.d.PreparePassword(ctx, "alice_shop")
	if err != nil || m34StepIDs(pw2.Planned) != "mariadb.password" {
		t.Fatalf("đổi mật khẩu khi user đã có: %v, bước %q", err, m34StepIDs(pw2.Planned))
	}

	// DB đang tạm khoá mà user bị xoá: mở khoá chỉ đường; tạo lại user ở trạng
	// thái KHOÁ (tạo lại không được là cách mở khoá lén); rồi mở khoá được.
	dbsExec(t)(e.d.PrepareSuspend(ctx, "alice_shop"))
	e.dropOutside("alice_shop", false, true)
	_, err = e.d.PrepareUnsuspend(ctx, "alice_shop")
	if !errors.As(err, &re) || re.Code != CodeDBUserMissing || !strings.Contains(re.Message.Vi, "không mở khoá được") {
		t.Fatalf("mở khoá khi user DB đã mất: %v", err)
	}
	pw3, err := e.d.PreparePassword(ctx, "alice_shop")
	if err != nil {
		t.Fatal(err)
	}
	dbsExec(t)(pw3.Planned, nil)
	if u := e.sim.users["alice_shop"]; !u.locked || u.pw != pw3.Password.Reveal() {
		t.Fatalf("user tạo lại cho DB đang tạm khoá phải khoá: %+v", u)
	}
	dbsExec(t)(e.d.PrepareUnsuspend(ctx, "alice_shop"))
	if e.sim.users["alice_shop"].locked || e.store.dbs["alice_shop"].Suspended != account.NotSuspended {
		t.Error("mở khoá sau khi tạo lại user chưa xong")
	}
}

// DB trong thùng rác mà user DB (giữ khoá) bị xoá ngoài tpan: khôi phục từ
// chối và chỉ đường (xoá hẳn rồi tạo lại từ bản sao lưu); xoá hẳn vẫn chạy.
func TestDBRoiTrongThungRacMatUserDB(t *testing.T) {
	e := newDBsEnv(t)
	e.add(t)
	dbsExec(t)(e.d.PrepareTrash(ctx, "alice_shop"))
	e.dropOutside("alice_shop", false, true)
	_, err := e.d.PrepareRestore(ctx, "alice_shop")
	var re *RefusedError
	if !errors.As(err, &re) || re.Code != CodeDBUserMissing || !strings.Contains(re.Message.Vi, "Xoá hẳn") {
		t.Fatalf("khôi phục khi user DB đã mất: %v", err)
	}
	if e.store.dbs["alice_shop"].Status != account.DBTrashed {
		t.Error("từ chối mà dòng đổi trạng thái")
	}
	dbsExec(t)(e.d.PreparePurge(ctx, "alice_shop"))
	if _, ok := e.store.dbs["alice_shop"]; ok {
		t.Error("xoá hẳn xong mà dòng còn")
	}
}

// Không hỏi được MariaDB thì không được kết luận "đã mất": các thao tác giữ
// đường cũ (Plan thường), để một lần MariaDB dừng không biến "xoá" thành gỡ.
func TestDBRoiMariaDBKhongTraLoiThiKhongCoiLaMat(t *testing.T) {
	e := newDBsEnv(t)
	e.add(t)
	e.d.Sys = dbsDown{e.sim}
	p, err := e.d.PrepareTrash(ctx, "alice_shop")
	if err != nil || p.Plan.Kind != "dbs.trash" {
		t.Fatalf("PrepareTrash khi MariaDB dừng: %v, Kind %v", err, p)
	}
	if s, err := e.d.PrepareSuspend(ctx, "alice_shop"); err != nil || s.Plan.Kind != "dbs.suspend" {
		t.Errorf("PrepareSuspend khi MariaDB dừng: %v", err)
	}
	pw, err := e.d.PreparePassword(ctx, "alice_shop")
	if err != nil || m34StepIDs(pw.Planned) != "mariadb.password" {
		t.Errorf("PreparePassword khi MariaDB dừng: %v", err)
	}
	if _, ok := e.store.dbs["alice_shop"]; !ok {
		t.Error("dòng bị xoá dù MariaDB chỉ đang dừng")
	}
}

// Danh sách DB đánh dấu Missing đúng cho DB đã bị xoá ngoài tpan (của site lẫn
// DB rời), không đánh dấu DB rời trong thùng rác (đã DROP có chủ ý), và không
// đánh dấu gì khi MariaDB không trả lời (lúc đó chỉ là "không rõ").
func TestDatabaseListDanhDauDBDaMat(t *testing.T) {
	e := newDBsEnv(t)
	e.add(t)
	sa := e.site(t, "a.example.com", nil)
	e.site(t, "b.example.com", nil)
	e.store.dbs["bob_old"] = account.Database{Name: "bob_old", Owner: "bob", Status: account.DBTrashed, Charset: "latin1", Collation: "latin1_bin"}
	missing := func() map[string]bool {
		t.Helper()
		all, err := e.d.List(ctx, "")
		if err != nil || len(all) != 4 {
			t.Fatalf("List = %+v, %v", all, err)
		}
		out := map[string]bool{}
		for _, r := range all {
			if r.Missing {
				out[r.DB] = true
				if r.Tables != -1 || r.SizeBytes != -1 {
					t.Errorf("%s đã mất mà còn số liệu: %+v", r.DB, r)
				}
			}
		}
		return out
	}
	if got := missing(); len(got) != 0 {
		t.Fatalf("mọi DB đều còn mà có dòng Missing: %v", got)
	}
	e.dropOutside("alice_shop", true, false)
	e.dropOutside(sa.Layout().DB, true, true)
	got := missing()
	if len(got) != 2 || !got["alice_shop"] || !got[sa.Layout().DB] {
		t.Errorf("Missing = %v; muốn đúng alice_shop và DB của site a (không tính bob_old trong thùng rác)", got)
	}
	// MariaDB không trả lời: không DB nào bị coi là đã mất.
	e.sim.Handle([]string{"mariadb", "-N", "-B"}, func(system.Cmd) system.Result { return system.Result{ExitCode: 1} })
	if got := missing(); len(got) != 0 {
		t.Errorf("MariaDB dừng mà có dòng Missing: %v", got)
	}
}
