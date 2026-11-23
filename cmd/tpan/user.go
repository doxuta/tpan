package main

// `tpan user add|list|info|edit|passwd|suspend|unsuspend|trash-all|rm` (mục
// 6.12 của docs/KE-HOACH.md) và phần chung của các lệnh M13: dựng core.Sites,
// core.DBs, core.Accounts trên cùng một tpan.db, và nhịp chạy của thao tác
// chỉ ghi tpan.db (không có Plan).
//
// Dòng lệnh chạy bằng root nên luôn là admin (account.Root): không gắn
// engine.WithActor, job của CLI có actor rỗng như MCP và bộ hẹn giờ.

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/server"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/validate"
)

func init() { register(userCmd) }

// dryStore bọc store cho --dry-run của thao tác không có Plan: đọc thật, mọi
// hàm ghi là không làm gì. Nhờ vậy --dry-run chạy đúng hàm của core và từ
// chối đúng như lần chạy thật (tên trùng, admin cuối cùng, gói còn user...)
// mà không ghi gì.
type dryStore struct{ *store.Store }

func (dryStore) CreateUser(context.Context, account.User, string) error       { return nil }
func (dryStore) UpdateUser(context.Context, account.User) error               { return nil }
func (dryStore) SetUserPassword(context.Context, string, string) error        { return nil }
func (dryStore) DeleteSessionsOf(context.Context, string) error               { return nil }
func (dryStore) DeleteUser(context.Context, string) error                     { return nil }
func (dryStore) SavePackage(context.Context, account.Package) error           { return nil }
func (dryStore) DeletePackage(context.Context, string) error                  { return nil }
func (dryStore) SetDiskUsage(context.Context, string, int64, time.Time) error { return nil }
func (dryStore) SaveStandaloneDB(context.Context, account.Database) error     { return nil }
func (dryStore) DeleteStandaloneDB(context.Context, string) error             { return nil }

// m13Env là các ca sử dụng của M13 dựng trên cùng một tpan.db.
type m13Env struct {
	// st nil: --dry-run trên máy chưa có tpan.db (khi đó acc cũng nil).
	st    *store.Store
	sites *core.Sites
	dbs   *core.DBs
	acc   *core.Accounts
}

// openM13 dựng Sites (như openSites), DBs và Accounts. --dry-run mà chưa có
// tpan.db thì không tạo file: DBs không có bảng databases, Accounts nil.
func openM13(ctx context.Context, e *env, dryRun bool) (*m13Env, func(), error) {
	s, done, err := openSites(ctx, e, dryRun)
	if err != nil {
		return nil, nil, err
	}
	m := &m13Env{sites: s, dbs: &core.DBs{Sys: s.Sys, Settings: s.Settings, Sites: s.Store, Accounts: s.Accounts, Journal: s.Journal}}
	if st, ok := s.Store.(*store.Store); ok {
		m.st, m.dbs.Store = st, st
		m.acc = &core.Accounts{Sys: s.Sys, Store: st, Sites: s, DBs: m.dbs, Journal: s.Journal,
			Hash: store.HashPassword, Generate: store.GeneratePassword}
	}
	return m, done, nil
}

// needAccounts từ chối khi chưa có tpan.db: chưa có bảng users thì không có
// user hay gói nào để xem hay sửa.
func (m *m13Env) needAccounts() error {
	if m.acc != nil {
		return nil
	}
	return &core.RefusedError{Code: "setup_required", Message: i18n.T(
		"máy chưa có tpan.db nên chưa có user hay gói nào; chạy sudo tpan setup trước",
		"this machine has no tpan.db yet, so there are no users or packages; run sudo tpan setup first")}
}

// dry trả bản sao của Accounts và DBs ghi vào dryStore.
func (m *m13Env) dry() (*core.Accounts, *core.DBs) {
	ds := dryStore{m.st}
	d := *m.dbs
	d.Store = ds
	a := *m.acc
	a.Store, a.DBs = ds, &d
	return &a, &d
}

// acctOp là một thao tác chỉ ghi tpan.db (user, gói, ghi chú DB rời).
type acctOp struct {
	action, target string
	// what là câu "Sẽ: ..." in khi --dry-run và trước câu hỏi xác nhận.
	what i18n.Text
	// do chạy thao tác. Không có --yes thì do chạy hai lần: trên dryStore (để
	// từ chối trước khi hỏi) rồi trên store thật. Giá trị cần in (mật khẩu
	// sinh ra) được giữ ở biến của closure: lần chạy thật ghi đè lần khô.
	do func(ctx context.Context, a *core.Accounts, d *core.DBs) error
	// done dựng kết quả sau lần chạy thật: thứ in khi --json, và hàm in cho người.
	done func(ctx context.Context, a *core.Accounts) (asJSON any, human func(), err error)
}

// runAcctOp: đòi root (tpan.db chỉ root đọc được), kiểm bằng lần chạy khô,
// --dry-run thì in việc sẽ làm, hỏi lại (trừ --yes), chạy thật, ghi nhật ký, in.
func runAcctOp(cmd *cobra.Command, e *env, f planFlags, op acctOp) error {
	ctx := cmd.Context()
	if err := requireRoot(e, cmd); err != nil {
		return fail(e, f.asJSON, err)
	}
	m, done, err := openM13(ctx, e, f.dryRun || !e.fileExists(e.dbPath)) // chưa có tpan.db thì không tạo: needAccounts từ chối
	if err != nil {
		return err
	}
	defer done()
	if err := m.needAccounts(); err != nil {
		return fail(e, f.asJSON, err)
	}
	what := op.what.In(e.lang)
	if f.dryRun || !f.yes {
		a, d := m.dry()
		if err := op.do(ctx, a, d); err != nil {
			return fail(e, f.asJSON, asRefusal(err))
		}
		if f.dryRun {
			if f.asJSON {
				return writeJSON(e.stdout, map[string]any{"dry_run": true, "plan": what})
			}
			printf(e.stdout, "%s\n%s\n", what, e.tr("(--dry-run: chưa thay đổi gì)", "(--dry-run: nothing changed)"))
			return nil
		}
		if f.asJSON || !e.isTerminal() {
			return fail(e, f.asJSON, &core.RefusedError{Code: "confirm_required", Message: i18n.T(
				"không hỏi xác nhận được (không có terminal hoặc đang dùng --json); xem trước bằng --dry-run rồi chạy lại với --yes",
				"cannot ask for confirmation (no terminal, or --json is set); preview with --dry-run, then run again with --yes")})
		}
		printf(e.stdout, "%s\n", what)
		if !confirm(e) {
			printf(e.stdout, "%s\n", e.tr("Đã huỷ, không thay đổi gì.", "Cancelled, nothing changed."))
			return &exitCode{1}
		}
	}
	if err := op.do(ctx, m.acc, m.dbs); err != nil {
		return fail(e, f.asJSON, asRefusal(err))
	}
	auditNoJob(ctx, e, m.st, op.action, op.target)
	v, human, err := op.done(ctx, m.acc)
	if err != nil {
		return err
	}
	if f.asJSON {
		return writeJSON(e.stdout, v)
	}
	human()
	return nil
}

// runM13Plan chạy một Plan của M13 (tạm khoá, DB rời, khoá user...): đòi root
// trừ --dry-run, dựng Plan bằng prepare rồi runPlanned. Audit ghi theo Kind và
// Target của Plan. jsonOf, after như runPlannedOut: lệnh có mật khẩu hiện một
// lần (db add, db passwd) gói thêm mật khẩu vào JSON và in nó sau báo cáo.
func runM13Plan(cmd *cobra.Command, e *env, f planFlags, needAcc bool,
	prepare func(context.Context, *m13Env) (*core.Planned, error),
	jsonOf func(engine.Report) any, after func()) error {
	ctx := cmd.Context()
	if !f.dryRun {
		if err := requireRoot(e, cmd); err != nil {
			return fail(e, f.asJSON, err)
		}
	}
	m, done, err := openM13(ctx, e, f.dryRun || (needAcc && !e.fileExists(e.dbPath)))
	if err != nil {
		return err
	}
	defer done()
	if needAcc {
		if err := m.needAccounts(); err != nil {
			return fail(e, f.asJSON, err)
		}
	}
	p, err := prepare(ctx, m)
	if err != nil {
		return fail(e, f.asJSON, asRefusal(err))
	}
	rep, err := runPlannedOut(ctx, e, f, m.sites.Journal, p.Plan.Kind, p.Plan.Target, p, jsonOf)
	if err == nil && after != nil && rep.Job.ID != "" && !f.asJSON {
		after()
	}
	return err
}

// parseUserArg kiểm tên user người dùng gõ, trước mọi việc khác.
func parseUserArg(s string) (validate.UserName, error) {
	u, err := validate.ParseUserName(s)
	return u, asRefusal(err)
}

// suspensionText là trạng thái tạm khoá cho bảng của CLI.
func suspensionText(e *env, s account.Suspension) string {
	switch s {
	case account.SuspendSelf:
		return e.tr("khoá", "suspended")
	case account.SuspendByUser:
		return e.tr("khoá (theo user)", "suspended (by user)")
	}
	return "-"
}

func userCmd(e *env) *cobra.Command {
	c := &cobra.Command{
		Use:   "user",
		Short: "User của panel: thêm, xem, sửa, đổi mật khẩu, tạm khoá, xoá",
	}
	c.AddCommand(userAddCmd(e), userListCmd(e), userInfoCmd(e), userEditCmd(e), userPasswdCmd(e),
		userSuspendCmd(e), userUnsuspendCmd(e), userTrashAllCmd(e), userRmCmd(e))
	return c
}

// userFields là các cờ chung của user add và user edit.
type userFields struct {
	fullName, email, role, pkg string
}

func (u *userFields) flags(c *cobra.Command) {
	fl := c.Flags()
	fl.StringVar(&u.fullName, "full-name", "", "tên đầy đủ")
	fl.StringVar(&u.email, "email", "", "email (để trống là không có)")
	fl.StringVar(&u.role, "role", "user", "vai trò: admin hoặc user")
	fl.StringVar(&u.pkg, "package", account.DefaultPackage, "gói hạn mức (xem bằng: sudo tpan package list)")
}

func parseEmailArg(s string) (validate.Email, error) {
	if s == "" {
		return validate.Email{}, nil
	}
	em, err := validate.ParseEmail(s)
	return em, asRefusal(err)
}

func userAddCmd(e *env) *cobra.Command {
	var (
		f  planFlags
		uf userFields
	)
	c := &cobra.Command{
		Use:   "add <tên>",
		Short: "Thêm user của panel; mật khẩu do tpan tạo và chỉ in ra một lần",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := parseUserArg(args[0])
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			em, err := parseEmailArg(uf.email)
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			// Không có cờ --password: mật khẩu trên dòng lệnh lọt vào ps và
			// lịch sử shell. tpan sinh, in một lần; user tự đổi trên panel.
			in := core.UserInput{Name: name, FullName: uf.fullName, Email: em, Role: account.Role(uf.role), Package: uf.pkg}
			var pw string
			return runAcctOp(cmd, e, f, acctOp{
				action: "user.add", target: name.String(),
				what: i18n.T(
					"Sẽ: thêm user "+name.String()+" (vai trò "+uf.role+", gói "+uf.pkg+"); mật khẩu tạo ngẫu nhiên, in ra một lần.",
					"Will: add user "+name.String()+" (role "+uf.role+", package "+uf.pkg+"); a random password is generated and printed once."),
				do: func(ctx context.Context, a *core.Accounts, _ *core.DBs) error {
					_, pw, err = a.AddUser(ctx, in)
					return err
				},
				done: func(ctx context.Context, acc *core.Accounts) (any, func(), error) {
					sum, err := acc.Summary(ctx, name.String())
					if err != nil {
						return nil, nil, err
					}
					return server.UserCreated{User: userRow(sum), Password: pw}, func() {
						printf(e.stdout, "%s\n", e.tr("Đã thêm user "+name.String()+".", "User "+name.String()+" added."))
						printOncePassword(e, e.tr("Mật khẩu đăng nhập panel", "Panel login password"), pw)
					}, nil
				},
			})
		},
	}
	uf.flags(c)
	bindChangeFlags(c, &f)
	return c
}

// printOncePassword in một mật khẩu vừa sinh, kèm lời nhắc nó chỉ hiện một lần.
func printOncePassword(e *env, what, pw string) {
	printf(e.stdout, "%s %s\n    %s\n", what, e.tr("(chỉ hiện MỘT lần, hãy lưu lại ngay):", "(shown ONCE, save it now):"), pw)
}

func userRow(s core.UserSummary) server.UserRow {
	return server.UserRow{User: s.User, Limits: s.Limits, Usage: s.Usage, OverQuota: s.OverQuota}
}

// openAccountsRead mở tpan.db cho lệnh chỉ đọc user, gói (cần root).
func openAccountsRead(cmd *cobra.Command, e *env, asJSON bool) (*m13Env, func(), error) {
	if err := requireRoot(e, cmd); err != nil {
		return nil, nil, fail(e, asJSON, err)
	}
	m, done, err := openM13(cmd.Context(), e, !e.fileExists(e.dbPath))
	if err != nil {
		return nil, nil, err
	}
	if err := m.needAccounts(); err != nil {
		done()
		return nil, nil, fail(e, asJSON, err)
	}
	return m, done, nil
}

func userListCmd(e *env) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "list",
		Short: "Liệt kê user: vai, gói, số site và DB rời trên giới hạn, dung lượng, trạng thái",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			m, done, err := openAccountsRead(cmd, e, asJSON)
			if err != nil {
				return err
			}
			defer done()
			list, err := m.acc.Summaries(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([]server.UserRow, len(list))
			for i, s := range list {
				rows[i] = userRow(s)
			}
			if asJSON {
				return writeJSON(e.stdout, rows)
			}
			printUsers(e, rows)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
	return c
}

// maxText là một giới hạn của gói: 0 là không giới hạn (∞).
func maxText(most int) string {
	if most == 0 {
		return "∞"
	}
	return strconv.Itoa(most)
}

// limitText là "đang dùng/giới hạn".
func limitText(used, most int) string { return strconv.Itoa(used) + "/" + maxText(most) }

// diskText là lần đo gần nhất trên giới hạn; "?" khi chưa đo lần nào.
func diskText(u account.Usage, mb int) string {
	used := "?"
	if u.DiskBytes >= 0 {
		used = humanSize(u.DiskBytes)
	}
	if mb == 0 {
		return used
	}
	return used + "/" + strconv.Itoa(mb) + " MiB"
}

func userStatusText(e *env, r server.UserRow) string {
	s := e.tr("hoạt động", "active")
	if r.User.Status == account.Suspended {
		s = e.tr("tạm khoá", "suspended")
	}
	if r.OverQuota {
		s += e.tr(" (vượt gói)", " (over quota)")
	}
	return s
}

func printUsers(e *env, rows []server.UserRow) {
	w := e.stdout
	printf(w, "%-20s %-6s %-14s %-7s %-7s %-20s %s\n", e.tr("TÊN", "NAME"), e.tr("VAI", "ROLE"), e.tr("GÓI", "PACKAGE"),
		"SITE", "DB", e.tr("DUNG LƯỢNG", "DISK"), e.tr("TRẠNG THÁI", "STATUS"))
	for _, r := range rows {
		printf(w, "%-20s %-6s %-14s %-7s %-7s %-20s %s\n", r.User.Name, r.User.Role, r.User.Package,
			limitText(r.Usage.Sites, r.Limits.MaxSites), limitText(r.Usage.DBs, r.Limits.MaxDBs),
			diskText(r.Usage, r.Limits.DiskMB), userStatusText(e, r))
	}
}

func userInfoCmd(e *env) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "info <tên>",
		Short: "Xem một user: gói, phần đã dùng, trạng thái",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := parseUserArg(args[0])
			if err != nil {
				return fail(e, asJSON, err)
			}
			m, done, err := openAccountsRead(cmd, e, asJSON)
			if err != nil {
				return err
			}
			defer done()
			sum, err := m.acc.Summary(cmd.Context(), name.String())
			if err != nil {
				return fail(e, asJSON, err)
			}
			r := userRow(sum)
			if asJSON {
				return writeJSON(e.stdout, r)
			}
			w, u := e.stdout, r.User
			row := func(k, v string) { printf(w, "%-18s %s\n", k, v) }
			row(e.tr("Tên:", "Name:"), u.Name)
			row(e.tr("Tên đầy đủ:", "Full name:"), u.FullName)
			row("Email:", u.Email)
			row(e.tr("Vai:", "Role:"), string(u.Role))
			row(e.tr("Gói:", "Package:"), u.Package)
			row(e.tr("Trạng thái:", "Status:"), userStatusText(e, r))
			if u.Status == account.Suspended {
				row(e.tr("Khoá lúc:", "Suspended at:"), u.SuspendedAt.Local().Format("2006-01-02 15:04"))
				row(e.tr("Lý do:", "Reason:"), u.SuspendReason)
			}
			row("Site:", limitText(r.Usage.Sites, r.Limits.MaxSites))
			row(e.tr("DB rời:", "Standalone DBs:"), limitText(r.Usage.DBs, r.Limits.MaxDBs))
			row(e.tr("Dung lượng:", "Disk:"), diskText(r.Usage, r.Limits.DiskMB))
			row(e.tr("Backup mỗi site:", "Backups per site:"), maxText(r.Limits.MaxBackups))
			row(e.tr("Tạo lúc:", "Created:"), u.Created.Local().Format("2006-01-02 15:04"))
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
	return c
}

func userEditCmd(e *env) *cobra.Command {
	var (
		f  planFlags
		uf userFields
	)
	c := &cobra.Command{
		Use:   "edit <tên> [--full-name T] [--email E] [--role admin|user] [--package P]",
		Short: "Sửa tên đầy đủ, email, vai, gói của user (đổi vai thì huỷ mọi phiên của user đó)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := parseUserArg(args[0])
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			var ch core.UserChange
			var vi, en []string
			fl := cmd.Flags()
			if fl.Changed("full-name") {
				ch.FullName = &uf.fullName
				vi, en = append(vi, "tên đầy đủ"), append(en, "full name")
			}
			if fl.Changed("email") {
				em, err := parseEmailArg(uf.email)
				if err != nil {
					return fail(e, f.asJSON, err)
				}
				ch.Email = &em
				vi, en = append(vi, "email"), append(en, "email")
			}
			if fl.Changed("role") {
				r := account.Role(uf.role)
				ch.Role = &r
				vi, en = append(vi, "vai → "+uf.role+" (huỷ mọi phiên của user nếu vai đổi)"), append(en, "role → "+uf.role+" (ends every session of the user if the role changes)")
			}
			if fl.Changed("package") {
				ch.Package = &uf.pkg
				vi, en = append(vi, "gói → "+uf.pkg), append(en, "package → "+uf.pkg)
			}
			if len(vi) == 0 {
				return fail(e, f.asJSON, &core.RefusedError{Code: "args_invalid", Message: i18n.T(
					"không có gì để sửa: thêm ít nhất một trong --full-name, --email, --role, --package",
					"nothing to change: give at least one of --full-name, --email, --role, --package")})
			}
			return runAcctOp(cmd, e, f, acctOp{
				action: "user.edit", target: name.String(),
				what: i18n.T("Sẽ: sửa user "+name.String()+": "+strings.Join(vi, ", ")+".",
					"Will: change user "+name.String()+": "+strings.Join(en, ", ")+"."),
				do: func(ctx context.Context, a *core.Accounts, _ *core.DBs) error {
					_, err := a.EditUser(ctx, name.String(), ch, account.Root)
					return err
				},
				done: func(ctx context.Context, acc *core.Accounts) (any, func(), error) {
					sum, err := acc.Summary(ctx, name.String())
					if err != nil {
						return nil, nil, err
					}
					return userRow(sum), func() {
						printf(e.stdout, "%s\n", e.tr("Đã sửa user "+name.String()+".", "User "+name.String()+" updated."))
					}, nil
				},
			})
		},
	}
	uf.flags(c)
	bindChangeFlags(c, &f)
	return c
}

func userPasswdCmd(e *env) *cobra.Command {
	var f planFlags
	c := &cobra.Command{
		Use:   "passwd <tên>",
		Short: "Sinh mật khẩu mới cho user (in ra một lần) và huỷ mọi phiên của user đó",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := parseUserArg(args[0])
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			var pw string
			return runAcctOp(cmd, e, f, acctOp{
				action: "user.passwd", target: name.String(),
				what: i18n.T(
					"Sẽ: sinh mật khẩu mới cho "+name.String()+" (in ra một lần) và huỷ mọi phiên đăng nhập của user này.",
					"Will: generate a new password for "+name.String()+" (printed once) and end every login session of this user."),
				do: func(ctx context.Context, a *core.Accounts, _ *core.DBs) error {
					pw, err = a.SetPassword(ctx, name.String(), "")
					return err
				},
				done: func(context.Context, *core.Accounts) (any, func(), error) {
					return server.PasswordSetResult{Password: pw}, func() {
						printOncePassword(e, e.tr("Mật khẩu mới của "+name.String(), "New password of "+name.String()), pw)
						printf(e.stdout, "%s\n", e.tr("Mọi phiên của user này đã bị huỷ.", "Every session of this user has been ended."))
					}, nil
				},
			})
		},
	}
	bindChangeFlags(c, &f)
	return c
}

// userPlanCmd dựng lệnh chạy một Plan trên user (suspend, unsuspend, trash-all).
func userPlanCmd(e *env, use, short string, extra func(*cobra.Command),
	prepare func(context.Context, *core.Accounts, string) (*core.Planned, error)) *cobra.Command {
	var f planFlags
	c := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := parseUserArg(args[0])
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			return runM13Plan(cmd, e, f, true, func(ctx context.Context, m *m13Env) (*core.Planned, error) {
				return prepare(ctx, m.acc, name.String())
			}, nil, nil)
		},
	}
	if extra != nil {
		extra(c)
	}
	bindChangeFlags(c, &f)
	return c
}

func userSuspendCmd(e *env) *cobra.Command {
	var reason string
	return userPlanCmd(e, "suspend <tên> [--reason R]",
		"Tạm khoá user: khoá mọi site và DB rời của họ, không cho đăng nhập, huỷ mọi phiên",
		func(c *cobra.Command) {
			c.Flags().StringVar(&reason, "reason", "", "lý do (ghi lại, tối đa 200 ký tự)")
		},
		func(ctx context.Context, a *core.Accounts, name string) (*core.Planned, error) {
			return a.PrepareSuspendUser(ctx, name, reason, account.Root)
		})
}

func userUnsuspendCmd(e *env) *cobra.Command {
	return userPlanCmd(e, "unsuspend <tên>",
		"Mở khoá user và mở lại đúng những site, DB bị khoá vì user (thứ khoá riêng từ trước giữ khoá)", nil,
		func(ctx context.Context, a *core.Accounts, name string) (*core.Planned, error) {
			return a.PrepareUnsuspendUser(ctx, name)
		})
}

func userTrashAllCmd(e *env) *cobra.Command {
	return userPlanCmd(e, "trash-all <tên>",
		"Đưa mọi site và DB rời của user vào thùng rác (làm trước khi xoá user; từ chối khi có thứ đang tạm khoá)", nil,
		func(ctx context.Context, a *core.Accounts, name string) (*core.Planned, error) {
			return a.PrepareTrashAll(ctx, name)
		})
}

func userRmCmd(e *env) *cobra.Command {
	var f planFlags
	c := &cobra.Command{
		Use:   "rm <tên>",
		Short: "Xoá user (từ chối khi còn site, DB rời kể cả trong thùng rác, hoặc job đang chạy)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := parseUserArg(args[0])
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			return runAcctOp(cmd, e, f, acctOp{
				action: "user.rm", target: name.String(),
				what: i18n.T(
					"Sẽ: xoá user "+name.String()+" và mọi phiên của họ. Job và nhật ký cũ giữ nguyên tên.",
					"Will: delete user "+name.String()+" and all their sessions. Old jobs and audit entries keep the name."),
				do: func(ctx context.Context, a *core.Accounts, _ *core.DBs) error {
					return a.DeleteUser(ctx, name.String(), account.Root)
				},
				done: func(context.Context, *core.Accounts) (any, func(), error) {
					return map[string]string{"deleted": name.String()}, func() {
						printf(e.stdout, "%s\n", e.tr("Đã xoá user "+name.String()+".", "User "+name.String()+" deleted."))
					}, nil
				},
			})
		},
	}
	bindChangeFlags(c, &f)
	return c
}
