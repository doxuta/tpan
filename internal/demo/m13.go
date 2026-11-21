package demo

// Phần M13 của máy giả (mục 6.12 của docs/KE-HOACH.md): nhiều user, gói, tạm
// khoá, DB rời. Dữ liệu mẫu cho các màn Người dùng, Gói, Cơ sở dữ liệu (cột
// Chủ, DB rời), Thùng rác (DB rời), Site (cột Chủ, site tạm khoá xám):
//
//   - gói co_ban (2 site, 1 DB rời, 1024 MiB, 3 backup mỗi site);
//   - user khach_a (gói co_ban) chủ shop.example.net và DB rời khach_a_app
//     (đã đủ 1/1 DB của gói: thử tạo DB thứ hai thì thấy lời từ chối quota_dbs);
//   - user khach_b (gói default) chủ wiki.example.org và lab.example.org;
//     lab đang tạm khoá; DB rời khach_b_cu nằm trong thùng rác.
//
// Mọi thứ có job đều chạy bằng chính core (Accounts, Sites, DBs) như demo.go,
// để lịch sử job là thật. Mật khẩu của khach_a, khach_b do `tpan serve --demo`
// đặt và in ra (Users), như mật khẩu admin: gói này không đặt mật khẩu panel.
//
// Lệnh giả thêm cho M13 (gọi từ machine.mariadb và m10.curl):
//   - information_schema.COLLATIONS: vài cặp utf8mb4, latin1;
//   - charset, collation của DB (SCHEMATA), đổi bằng ALTER DATABASE;
//   - danh sách DB năm cột của DBs.List;
//   - PROCESSLIST của user DB: không có kết nối nào;
//   - gọi vào site có vhost tạm khoá: 403 kèm trang tạm khoá.

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
	"github.com/doxuta/tpan/internal/validate"
)

const (
	userA = "khach_a"
	userB = "khach_b"
	// pkgBasic: đặc tả của đợt giao việc ghi "co-ban", nhưng tên gói chỉ nhận
	// [a-z0-9_] (validate.ParsePackageName), nên là co_ban.
	pkgBasic = "co_ban"
	// siteLab là site của khach_b đang tạm khoá.
	siteLab = "lab.example.org"
	// dbApp, dbOld là phần tên của DB rời: khach_a_app (đang dùng),
	// khach_b_cu (trong thùng rác).
	dbApp = "app"
	dbOld = "cu"
)

// Users trả tên các user mẫu (vai user) của máy demo. `tpan serve --demo` đặt
// mật khẩu mới cho từng user mỗi lần chạy và in ra cùng mật khẩu admin.
func Users() []string { return []string{userA, userB} }

var (
	sqlCreateDBCharset = regexp.MustCompile("(?:CREATE|ALTER) DATABASE `([a-z0-9_]+)` CHARACTER SET ([a-z0-9_]+)(?: COLLATE ([a-z0-9_]+))?")
	sqlAlterDB         = regexp.MustCompile("^ALTER DATABASE `")
)

// demoCollations là output của câu COLLATIONS (dbsCollationsSQL): charset,
// collation, IS_DEFAULT ("Yes" hoặc rỗng), như MariaDB 10.11.
const demoCollations = "latin1\tlatin1_general_ci\t\nlatin1\tlatin1_swedish_ci\tYes\n" +
	"utf8mb4\tutf8mb4_bin\t\nutf8mb4\tutf8mb4_general_ci\tYes\nutf8mb4\tutf8mb4_unicode_520_ci\t\nutf8mb4\tutf8mb4_unicode_ci\t\n"

// defaultCollation là collation mặc định của charset trong demoCollations.
func defaultCollation(cs string) string {
	if cs == "latin1" {
		return "latin1_swedish_ci"
	}
	return "utf8mb4_general_ci"
}

// mariadbM13 trả lời các câu SQL của M13. ok=false: không phải câu của nó (câu
// CREATE DATABASE vẫn trả false sau khi ghi charset, để machine.mariadb ghi
// tiếp DB). Người gọi giữ m.mu.
func (m *machine) mariadbM13(sql string) (system.Result, bool) {
	if g := sqlCreateDBCharset.FindStringSubmatch(sql); g != nil {
		coll := g[3]
		if coll == "" {
			coll = defaultCollation(g[2])
		}
		m.dbCharset[g[1]] = [2]string{g[2], coll}
		if sqlAlterDB.MatchString(sql) {
			return okResult, true
		}
		return system.Result{}, false
	}
	switch {
	case strings.Contains(sql, "FROM information_schema.COLLATIONS"):
		return exit(0, demoCollations, ""), true
	case strings.Contains(sql, "FROM information_schema.PROCESSLIST"):
		return exit(0, "", ""), true
	case strings.HasPrefix(sql, "SELECT DEFAULT_CHARACTER_SET_NAME, DEFAULT_COLLATION_NAME FROM information_schema.SCHEMATA"):
		g := sqlStateRE.FindStringSubmatch(sql)
		if g == nil || !m.dbs[g[1]] {
			return exit(0, "", ""), true
		}
		cs := m.charsetOf(g[1])
		return exit(0, cs[0]+"\t"+cs[1]+"\n", ""), true
	case strings.Contains(sql, "s.DEFAULT_COLLATION_NAME") && strings.Contains(sql, "SCHEMA_NAME IN ("):
		// DBs.List: tên, số bảng, byte, charset, collation.
		var out strings.Builder
		for _, g := range sqlQuoted.FindAllStringSubmatch(sql[strings.Index(sql, "SCHEMA_NAME IN ("):], -1) {
			if m.dbs[g[1]] {
				cs := m.charsetOf(g[1])
				tables := wpTables
				if !strings.HasPrefix(g[1], "tp_") {
					tables = 4 // DB rời: vài bảng của ứng dụng, không phải WordPress
				}
				fmt.Fprintf(&out, "%s\t%d\t%d\t%s\t%s\n", g[1], tables, dbSize(g[1]), cs[0], cs[1])
			}
		}
		return exit(0, out.String(), ""), true
	}
	return system.Result{}, false
}

// charsetOf là charset, collation của DB; DB tạo không ghi collation (DB của
// site: CHARACTER SET utf8mb4) thì là mặc định của charset. Người gọi giữ m.mu.
func (m *machine) charsetOf(db string) [2]string {
	if cs, ok := m.dbCharset[db]; ok {
		return cs
	}
	return [2]string{"utf8mb4", defaultCollation("utf8mb4")}
}

// suspendedSite trả lời request tới site có vhost đang bật là bản tạm khoá
// (có thư mục trang tạm khoá): 403 kèm trang, ở cổng 80 (kể cả site có SSL:
// bản tạm khoá không chuyển sang https) và ở 443 khi vhost có SSL. ok=false
// thì để handler curl thường trả lời.
func (m *machine) suspendedSite(c system.Cmd) (system.Result, bool) {
	r := argAfter(c.Args, "--resolve")
	if r == "" {
		return system.Result{}, false
	}
	dom, rest, _ := strings.Cut(r, ":")
	port, _, _ := strings.Cut(rest, ":")
	l, ok := siteOf(dom)
	if !ok {
		return system.Result{}, false
	}
	var conf []byte
	for _, v := range [][2]string{{l.NginxLink, l.NginxConf}, {l.ApacheLink, l.ApacheConf}} {
		if _, err := m.Stat(v[0]); err == nil {
			conf, _ = m.ReadFile(v[1])
			break
		}
	}
	text := string(conf)
	if !strings.Contains(text, templates.SuspendedDir) || (port == "443" && !strings.Contains(text, "443")) {
		return system.Result{}, false
	}
	if slices.Contains(c.Args, "-D") {
		return exit(0, "HTTP/1.1 403 Forbidden\r\nServer: nginx\r\n\r\nTPAN_HTTP_CODE=403", ""), true
	}
	page, err := m.ReadFile(templates.SuspendedPage)
	if err != nil {
		page = []byte("<html><body>403 Forbidden</body></html>")
	}
	return exit(0, string(page)+"TPAN_HTTP_CODE=403", ""), true
}

// seedM13 tạo gói, user, đổi chủ site, tạo site tạm khoá và DB rời (xem đầu
// file). Gọi sau khi các site mẫu đã có. Thời điểm của job lùi về quá khứ như
// demo.go (pastJournal), để lịch sử trông như tích luỹ qua nhiều ngày.
func seedM13(ctx context.Context, m *machine, st *store.Store, s *core.Sites) error {
	ago := func(d time.Duration) func() time.Time { return func() time.Time { return time.Now().Add(-d) } }
	acc := &core.Accounts{Sys: m, Store: st, Sites: s, Journal: st, Now: ago(14 * 24 * time.Hour),
		Hash: store.HashPassword, Generate: store.GeneratePassword}

	if err := acc.AddPackage(ctx, account.Package{Name: pkgBasic, MaxSites: 2, MaxDBs: 1, DiskMB: 1024, MaxBackups: 3}); err != nil {
		return fmt.Errorf("tạo gói %s: %w", pkgBasic, err)
	}
	for _, u := range []struct{ name, full, email, pkg string }{
		{userA, "Khách hàng A", "a@example.com", pkgBasic},
		{userB, "Khách hàng B", "b@example.com", account.DefaultPackage},
	} {
		name, err := validate.ParseUserName(u.name)
		if err != nil {
			return err
		}
		em, err := validate.ParseEmail(u.email)
		if err != nil {
			return err
		}
		// Mật khẩu sinh ở đây bị bỏ: `tpan serve --demo` đặt mật khẩu mới và in ra.
		if _, _, err := acc.AddUser(ctx, core.UserInput{Name: name, FullName: u.full, Email: em, Role: account.RoleUser, Package: u.pkg}); err != nil {
			return fmt.Errorf("tạo user %s: %w", u.name, err)
		}
	}

	// Site của khach_b sẽ tạm khoá: tạo như mọi site mẫu rồi mới đổi chủ.
	if err := addOK(ctx, s, siteLab, "Phòng thí nghiệm", 12*24*time.Hour); err != nil {
		return err
	}
	// Ba site tên dài của M16. Phải tạo ở đây, sau khi user có, và trước
	// MeasureDisk ở cuối hàm để dung lượng của khach_a, khach_b tính cả chúng.
	// khach_a có gói co_ban (tối đa 2 site): có shop và site 44 ký tự là đủ
	// 2/2, màn Người dùng có một thanh mức dùng đầy. Chủ đặt bằng setOwner như
	// các site trên (tạo bằng quyền admin nên không đụng hạn mức gói).
	if err := addOK(ctx, s, siteLongShop, "Cửa hàng số một", 16*24*time.Hour); err != nil {
		return err
	}
	if err := addOK(ctx, s, siteLongEdu, "Học trực tuyến", 6*24*time.Hour); err != nil {
		return err
	}
	if err := addOK(ctx, s, siteLongNews, "Tin tức", 30*time.Hour); err != nil {
		return err
	}
	for dom, owner := range map[string]string{siteShop: userA, siteWiki: userB, siteLab: userB, siteLongShop: userA, siteLongNews: userB} {
		if err := setOwner(ctx, st, dom, owner); err != nil {
			return err
		}
	}

	shifted := func(d time.Duration) *core.Sites {
		c := *s
		c.Journal, c.Now = pastJournal{Journal: st, ago: d}, ago(d)
		return &c
	}
	dbsAt := func(d time.Duration) *core.DBs {
		return &core.DBs{Sys: m, Settings: st, Store: st, Sites: st, Accounts: st, Journal: pastJournal{Journal: st, ago: d}, Now: ago(d)}
	}
	run := func(what string, p *core.Planned, err error) error {
		if err != nil {
			return fmt.Errorf("chuẩn bị %s: %w", what, err)
		}
		rep, err := p.Execute(ctx, nil)
		if err != nil {
			return fmt.Errorf("%s: %w", what, err)
		}
		if rep.Job.Status != engine.JobSucceeded {
			return fmt.Errorf("%s: job %s: %s", what, rep.Job.Status, rep.Job.Error)
		}
		return nil
	}
	addDB := func(owner, short, note string, d time.Duration) error {
		o, err := validate.ParseUserName(owner)
		if err != nil {
			return err
		}
		add, err := dbsAt(d).PrepareAdd(ctx, core.DBInput{Owner: o, Name: short, Note: note})
		if err != nil {
			return fmt.Errorf("chuẩn bị tạo DB rời %s_%s: %w", owner, short, err)
		}
		return run("tạo DB rời "+owner+"_"+short, add.Planned, nil)
	}

	if err := addDB(userA, dbApp, "DB của ứng dụng đặt hàng", 8*24*time.Hour); err != nil {
		return err
	}
	if err := addDB(userB, dbOld, "", 7*24*time.Hour); err != nil {
		return err
	}
	lab, err := validate.ParseDomain(siteLab)
	if err != nil {
		return err
	}
	p, err := shifted(4*24*time.Hour).PrepareSuspend(ctx, lab)
	if err := run("tạm khoá site "+siteLab, p, err); err != nil {
		return err
	}
	// Vào thùng rác 3 ngày rưỡi trước: với số ngày giữ mặc định (7) còn hơn 3
	// ngày tới hạn xoá hẳn, bộ hẹn giờ không xoá nó trong lúc xem demo.
	p, err = dbsAt(84*time.Hour).PrepareTrash(ctx, userB+"_"+dbOld)
	if err := run("đưa DB rời "+userB+"_"+dbOld+" vào thùng rác", p, err); err != nil {
		return err
	}

	// Dung lượng của từng user cho màn Người dùng (server đo lại mỗi giờ).
	acc.DBs, acc.Now = dbsAt(0), nil
	if err := acc.MeasureDisk(ctx); err != nil {
		return fmt.Errorf("đo dung lượng user demo: %w", err)
	}
	return nil
}

// setOwner đổi chủ của site mẫu (không có ca sử dụng "đổi chủ": chủ chỉ đặt
// lúc tạo site; demo cần site cũ thuộc user khác admin).
func setOwner(ctx context.Context, st *store.Store, dom, owner string) error {
	d, err := validate.ParseDomain(dom)
	if err != nil {
		return err
	}
	rec, ok, err := st.Site(ctx, d)
	if err != nil || !ok {
		return fmt.Errorf("đọc site %s để đổi chủ: có=%v, %w", dom, ok, err)
	}
	rec.Owner = owner
	if err := st.SaveSite(ctx, rec); err != nil {
		return fmt.Errorf("đổi chủ site %s: %w", dom, err)
	}
	return nil
}
