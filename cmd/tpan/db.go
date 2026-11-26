package main

// `tpan db list | add | edit | passwd | charset | suspend | unsuspend | rm |
// restore | purge | dump | import` (mục 6.9, 6.12 và 9 của docs/KE-HOACH.md).
//
// Đối số có dấu chấm là domain: DB đi kèm site đó. Không có dấu chấm là tên
// đầy đủ của một DB rời ("<user>_<tên>", xem bằng tpan db list). DB của site
// chỉ tạo và xoá theo site; DB rời có thêm add, rm, restore, purge, edit (ghi
// chú). Không có lệnh gõ SQL tuỳ ý (mục 1). Lệnh có thay đổi đi qua
// runPlanned: xem trước, hỏi lại, chạy có tiến độ, ghi audit.

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

func init() { register(dbCmd) }

func dbCmd(e *env) *cobra.Command {
	c := &cobra.Command{
		Use:   "db",
		Short: "DB của site và DB rời: xem, tạo, đổi mật khẩu, charset, tạm khoá, thùng rác, xuất và nhập .sql",
	}
	c.AddCommand(dbListCmd(e), dbAddCmd(e), dbEditCmd(e), dbPasswdCmd(e), dbCharsetCmd(e),
		dbArgCmd(e, "suspend <domain|tên DB>", "Tạm khoá user DB (ACCOUNT LOCK) và cắt các kết nối đang mở",
			func(ctx context.Context, m *m13Env, d validate.Domain) (*core.Planned, error) {
				return m.dbs.PrepareSiteDBSuspend(ctx, d)
			},
			func(ctx context.Context, m *m13Env, n string) (*core.Planned, error) {
				return m.dbs.PrepareSuspend(ctx, n)
			}),
		dbArgCmd(e, "unsuspend <domain|tên DB>", "Mở khoá user DB (ACCOUNT UNLOCK)",
			func(ctx context.Context, m *m13Env, d validate.Domain) (*core.Planned, error) {
				return m.dbs.PrepareSiteDBUnsuspend(ctx, d)
			},
			func(ctx context.Context, m *m13Env, n string) (*core.Planned, error) {
				return m.dbs.PrepareUnsuspend(ctx, n)
			}),
		dbArgCmd(e, "rm <tên DB>", "Đưa DB rời vào thùng rác: khoá user DB, xuất .sql vào thùng rác rồi mới DROP (khôi phục bằng tpan db restore). DB đã bị xoá ngoài tpan thì gỡ dòng của nó khỏi tpan",
			nil, func(ctx context.Context, m *m13Env, n string) (*core.Planned, error) {
				return m.dbs.PrepareTrash(ctx, n)
			}),
		dbArgCmd(e, "restore <tên DB>", "Khôi phục DB rời từ thùng rác: tạo lại DB, nhập bản xuất, mở khoá user DB",
			nil, func(ctx context.Context, m *m13Env, n string) (*core.Planned, error) {
				return m.dbs.PrepareRestore(ctx, n)
			}),
		dbArgCmd(e, "purge <tên DB>", "Xoá hẳn DB rời trong thùng rác: DROP USER; bản xuất cuối được giữ làm bản sao lưu (tạo lại bằng tpan db recreate)",
			nil, func(ctx context.Context, m *m13Env, n string) (*core.Planned, error) {
				return m.dbs.PreparePurge(ctx, n)
			}),
		dbPlanCmd(e, "dump <domain|tên DB>", "Xuất DB của site, hoặc sao lưu một DB rời, ra file .sql (xem bằng tpan backup list)", "db.dump",
			func(ctx context.Context, b *core.Backups, d validate.Domain, _ string) (*core.Planned, error) {
				return b.PrepareDBDump(ctx, d)
			},
			func(ctx context.Context, m *m13Env, n, _ string) (*core.Planned, error) {
				return m.dbs.PrepareBackup(ctx, n, core.ReasonManual)
			}),
		dbPlanCmd(e, "import <domain|tên DB> --id ID", "Nhập lại một bản xuất đã có (tự xuất bản hiện tại trước để hoàn tác được)", "db.import",
			func(ctx context.Context, b *core.Backups, d validate.Domain, id string) (*core.Planned, error) {
				return b.PrepareDBImport(ctx, d, id)
			},
			func(ctx context.Context, m *m13Env, n, id string) (*core.Planned, error) {
				return m.dbs.PrepareImport(ctx, n, id)
			}),
		dbRecreateCmd(e),
	)
	return c
}

// dbRecreateCmd (M33): tạo lại một DB rời đã xoá hẳn từ bản sao lưu của nó.
// User DB cũ đã bị xoá cùng DB nên mật khẩu là mật khẩu mới, in ra một lần.
func dbRecreateCmd(e *env) *cobra.Command {
	var (
		f  planFlags
		id string
	)
	c := &cobra.Command{
		Use:   "recreate --id ID",
		Short: "Tạo lại DB rời đã xoá hẳn từ một bản sao lưu của nó (đúng tên, chủ, charset cũ; mật khẩu mới, in ra một lần)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var add *core.DBAdd
			return runM13Plan(cmd, e, f, false, func(ctx context.Context, m *m13Env) (*core.Planned, error) {
				var err error
				if add, err = m.dbs.PrepareRecreate(ctx, id); err != nil {
					return nil, err
				}
				return add.Planned, nil
			}, func(rep engine.Report) any {
				return dbSecretOut(add.DB.Name, add.Password)(rep)
			}, func() { printDBPassword(e, add.DB.Name, add.Password) })
		},
	}
	c.Flags().StringVar(&id, "id", "", "id của bản sao lưu (xem bằng: sudo tpan backup list)")
	_ = c.MarkFlagRequired("id")
	bindChangeFlags(c, &f)
	return c
}

// dbNameRe là dạng tên đầy đủ của DB rời (cũng là databaseNameRe của core):
// kiểm ở CLI để chuỗi lạ bị từ chối trước khi mở DB hay chạy gì.
var dbNameRe = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)

// parseDBArg: có dấu chấm là domain của site, không có là tên DB rời.
func parseDBArg(s string) (validate.Domain, string, error) {
	if strings.Contains(s, ".") {
		d, err := parseDomainArg(s)
		return d, "", err
	}
	if !dbNameRe.MatchString(s) {
		return validate.Domain{}, "", &core.RefusedError{Code: validate.CodeDBNameInvalid, Message: i18n.T(
			"tên DB rời chỉ gồm a-z, 0-9 và _, tối đa 64 ký tự (DB của site thì ghi domain của site)",
			"a standalone database name uses only a-z, 0-9 and _, at most 64 characters (for a site's database give the site's domain)")}
	}
	return validate.Domain{}, s, nil
}

// refuseSiteDB: thao tác chỉ có cho DB rời mà đối số là domain.
func refuseSiteDB(cmd *cobra.Command, d validate.Domain) error {
	return &core.RefusedError{Code: "args_invalid", Message: i18n.T(
		"DB đi kèm site "+d.String()+" chỉ tạo, xoá, khôi phục cùng site (sudo tpan site add|rm|restore); `tpan db "+cmd.Name()+"` dùng cho DB rời",
		"the database of site "+d.String()+" is only created, removed and restored with the site (sudo tpan site add|rm|restore); `tpan db "+cmd.Name()+"` is for standalone databases")}
}

// dbArgCmd dựng một lệnh nhận domain hoặc tên DB rời. forSite nil: lệnh chỉ có
// cho DB rời.
func dbArgCmd(e *env, use, short string,
	forSite func(context.Context, *m13Env, validate.Domain) (*core.Planned, error),
	forDB func(context.Context, *m13Env, string) (*core.Planned, error)) *cobra.Command {
	var f planFlags
	c := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, name, err := parseDBArg(args[0])
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			if !d.IsZero() && forSite == nil {
				return fail(e, f.asJSON, refuseSiteDB(cmd, d))
			}
			return runM13Plan(cmd, e, f, false, func(ctx context.Context, m *m13Env) (*core.Planned, error) {
				if !d.IsZero() {
					return forSite(ctx, m, d)
				}
				return forDB(ctx, m, name)
			}, nil, nil)
		},
	}
	bindChangeFlags(c, &f)
	return c
}

// dbSecretJSON là output --json của db add và db passwd (DB rời): mật khẩu
// user DB ở trường riêng, chữ thật, đúng một lần và chỉ khi job thành công
// (job hoàn tác thì mật khẩu đó không dùng được).
type dbSecretJSON struct {
	Report     engine.Report `json:"report"`
	DB         string        `json:"db"`
	DBUser     string        `json:"db_user"`
	DBPassword string        `json:"db_password,omitempty"`
}

func dbSecretOut(name string, pw system.Secret) func(engine.Report) any {
	return func(rep engine.Report) any {
		out := dbSecretJSON{Report: rep, DB: name, DBUser: name}
		if rep.Job.Status == engine.JobSucceeded {
			out.DBPassword = pw.Reveal()
		}
		return out
	}
}

// printDBPassword in mật khẩu user DB vừa đặt, một lần.
func printDBPassword(e *env, name string, pw system.Secret) {
	printf(e.stdout, "\n%-12s %s\n%-12s %s\n", "DB:", name, e.tr("User DB:", "DB user:"), name+"@localhost")
	printOncePassword(e, e.tr("Mật khẩu user DB", "DB user password"), pw.Reveal())
	printf(e.stdout, "%s\n", e.tr("tpan không giữ mật khẩu này và sẽ không in lại.", "tpan does not keep this password and will not show it again."))
}

func dbAddCmd(e *env) *cobra.Command {
	var (
		f                               planFlags
		owner, charset, collation, note string
	)
	c := &cobra.Command{
		Use:   "add <tên> [--owner U] [--charset C] [--collation X] [--note N]",
		Short: "Tạo DB rời <owner>_<tên> cùng user DB trùng tên; mật khẩu tạo ngẫu nhiên, in ra một lần",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			own, err := parseUserArg(owner)
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			if strings.Contains(args[0], ".") {
				d, err := parseDomainArg(args[0])
				if err != nil {
					return fail(e, f.asJSON, err)
				}
				return fail(e, f.asJSON, refuseSiteDB(cmd, d))
			}
			// Kiểm tên trước mọi việc khác (core kiểm lại).
			full, err := validate.StandaloneDBName(own, args[0])
			if err != nil {
				return fail(e, f.asJSON, asRefusal(err))
			}
			var add *core.DBAdd
			return runM13Plan(cmd, e, f, false, func(ctx context.Context, m *m13Env) (*core.Planned, error) {
				add, err = m.dbs.PrepareAdd(ctx, core.DBInput{Owner: own, Name: args[0], Charset: charset, Collation: collation, Note: note})
				if err != nil {
					return nil, err
				}
				return add.Planned, nil
			}, func(rep engine.Report) any {
				return dbSecretOut(full, add.Password)(rep)
			}, func() { printDBPassword(e, full, add.Password) })
		},
	}
	fl := c.Flags()
	fl.StringVar(&owner, "owner", account.AdminName, "user chủ của DB (tên DB là <owner>_<tên>)")
	fl.StringVar(&charset, "charset", "", "charset (mặc định utf8mb4)")
	fl.StringVar(&collation, "collation", "", "collation (mặc định: collation mặc định của charset)")
	fl.StringVar(&note, "note", "", "ghi chú (tối đa 500 byte)")
	bindChangeFlags(c, &f)
	return c
}

func dbEditCmd(e *env) *cobra.Command {
	var (
		f    planFlags
		note string
	)
	c := &cobra.Command{
		Use:   "edit <tên DB> --note N",
		Short: "Sửa ghi chú của DB rời (đổi charset: tpan db charset; đổi mật khẩu: tpan db passwd)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, name, err := parseDBArg(args[0])
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			if !d.IsZero() {
				return fail(e, f.asJSON, refuseSiteDB(cmd, d))
			}
			return runAcctOp(cmd, e, f, acctOp{
				action: "dbs.note", target: name,
				what: i18n.T("Sẽ: ghi chú của DB rời "+name+" thành "+quote(note)+".", "Will: set the note of standalone database "+name+" to "+quote(note)+"."),
				do: func(ctx context.Context, _ *core.Accounts, dbs *core.DBs) error {
					return dbs.SetNote(ctx, name, note)
				},
				done: func(context.Context, *core.Accounts) (any, func(), error) {
					return map[string]string{"db": name, "note": note}, func() {
						printf(e.stdout, "%s\n", e.tr("Đã ghi chú cho DB "+name+".", "Note saved for database "+name+"."))
					}, nil
				},
			})
		},
	}
	c.Flags().StringVar(&note, "note", "", "ghi chú (tối đa 500 byte; rỗng là xoá ghi chú)")
	_ = c.MarkFlagRequired("note")
	bindChangeFlags(c, &f)
	return c
}

func dbPasswdCmd(e *env) *cobra.Command {
	var f planFlags
	c := &cobra.Command{
		Use: "passwd <domain|tên DB>",
		Short: "Đổi mật khẩu user DB. DB của site WordPress: ghi vào wp-config.php, không in ra. " +
			"DB rời và DB của site PHP trống: chỉ đổi trong MariaDB, in mật khẩu mới một lần. " +
			"User DB đã bị xoá ngoài tpan thì tạo lại user, dữ liệu giữ nguyên (site WordPress: với mật khẩu đang ghi trong wp-config.php)",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, name, err := parseDBArg(args[0])
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			if !d.IsZero() {
				// M29: site PHP trống thì in mật khẩu mới một lần (tpan không có
				// file cấu hình nào để ghi nó vào); site WordPress thì không in.
				var sp *core.DBPassword
				has := func() bool { return sp != nil && sp.Password.Reveal() != "" }
				return runM13Plan(cmd, e, f, false, func(ctx context.Context, m *m13Env) (*core.Planned, error) {
					sp, err = (&core.Backups{Sites: m.sites, Store: backupStore(m)}).PrepareSiteDBPassword(ctx, d)
					if err != nil {
						return nil, err
					}
					return sp.Planned, nil
				}, func(rep engine.Report) any {
					if !has() {
						return rep
					}
					return dbSecretOut(sp.Name, sp.Password)(rep)
				}, func() {
					if has() {
						printDBPassword(e, sp.Name, sp.Password)
					}
				})
			}
			var p *core.DBPassword
			return runM13Plan(cmd, e, f, false, func(ctx context.Context, m *m13Env) (*core.Planned, error) {
				p, err = m.dbs.PreparePassword(ctx, name)
				if err != nil {
					return nil, err
				}
				return p.Planned, nil
			}, func(rep engine.Report) any {
				return dbSecretOut(name, p.Password)(rep)
			}, func() { printDBPassword(e, name, p.Password) })
		},
	}
	bindChangeFlags(c, &f)
	return c
}

// backupStore là bảng backups của m (nil khi --dry-run trên máy chưa có tpan.db).
func backupStore(m *m13Env) core.BackupStore {
	if m.st == nil {
		return nil
	}
	return m.st
}

func dbCharsetCmd(e *env) *cobra.Command {
	var (
		f                  planFlags
		charset, collation string
	)
	c := &cobra.Command{
		Use:   "charset <domain|tên DB> [--charset C] [--collation X]",
		Short: "Đổi charset và collation mặc định của DB (ALTER DATABASE: chỉ áp cho bảng tạo sau)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, name, err := parseDBArg(args[0])
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			if charset == "" && collation == "" {
				return fail(e, f.asJSON, &core.RefusedError{Code: "args_invalid", Message: i18n.T(
					"cần --charset, --collation hoặc cả hai (xem các cặp có sẵn: sudo mariadb -e 'SHOW COLLATION')",
					"give --charset, --collation or both (list the available pairs: sudo mariadb -e 'SHOW COLLATION')")})
			}
			return runM13Plan(cmd, e, f, false, func(ctx context.Context, m *m13Env) (*core.Planned, error) {
				if !d.IsZero() {
					return m.dbs.PrepareSiteDBCharset(ctx, d, charset, collation)
				}
				return m.dbs.PrepareCharset(ctx, name, charset, collation)
			}, nil, nil)
		},
	}
	fl := c.Flags()
	fl.StringVar(&charset, "charset", "", "charset mới, ví dụ utf8mb4 (để trống: charset của --collation)")
	fl.StringVar(&collation, "collation", "", "collation mới, ví dụ utf8mb4_unicode_ci (để trống: mặc định của --charset)")
	bindChangeFlags(c, &f)
	return c
}

// dbRow là một dòng của `tpan db list --json`, cùng tên trường với API
// (server.DatabaseRow) để script đọc được cả hai như nhau.
type dbRow struct {
	Domain    string             `json:"domain"`
	DB        string             `json:"db"`
	User      string             `json:"user"`
	SizeBytes int64              `json:"size_bytes"` // -1 khi không đọc được
	Tables    int                `json:"tables"`     // -1 khi không đọc được
	Kind      string             `json:"kind"`       // "site" hoặc "standalone" (M13)
	Owner     string             `json:"owner"`
	Charset   string             `json:"charset"`
	Collation string             `json:"collation"`
	Note      string             `json:"note,omitempty"`
	Suspended account.Suspension `json:"suspended,omitempty"`
	Status    string             `json:"status"`
	// Missing (M34): DB có trong tpan nhưng không còn trong MariaDB.
	Missing bool `json:"missing,omitempty"`
}

func dbListCmd(e *env) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "list",
		Short: "Liệt kê DB của site và DB rời: dung lượng, số bảng, loại, chủ, charset, tạm khoá",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, asJSON, err)
			}
			// Chỉ đọc: chưa có tpan.db thì không tạo, danh sách rỗng.
			m, done, err := openM13(cmd.Context(), e, true)
			if err != nil {
				return err
			}
			defer done()
			list, err := m.dbs.List(cmd.Context(), "")
			if err != nil {
				return err
			}
			rows := make([]dbRow, len(list))
			for i, d := range list {
				rows[i] = dbRow{Domain: d.Domain, DB: d.DB, User: d.User, SizeBytes: d.SizeBytes, Tables: d.Tables,
					Kind: d.Kind, Owner: d.Owner, Charset: d.Charset, Collation: d.Collation, Note: d.Note,
					Suspended: d.Suspended, Status: d.Status, Missing: d.Missing}
			}
			if asJSON {
				return writeJSON(e.stdout, rows)
			}
			printDBs(e, rows)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
	return c
}

func printDBs(e *env, rows []dbRow) {
	w := e.stdout
	if len(rows) == 0 {
		printf(w, "%s\n", e.tr("Chưa có DB nào (chưa có site, chưa có DB rời).", "No databases yet (no sites, no standalone databases)."))
		return
	}
	// Cột M13 nằm sau cột cũ: script cũ đọc bốn cột đầu vẫn đúng.
	printf(w, "%-40s %-28s %12s %6s %-10s %-14s %-10s %s\n", "DOMAIN", "DB", e.tr("DUNG LƯỢNG", "SIZE"), e.tr("BẢNG", "TABLES"),
		e.tr("LOẠI", "KIND"), e.tr("CHỦ", "OWNER"), "CHARSET", e.tr("TRẠNG THÁI", "STATUS"))
	for _, r := range rows {
		size, tables, dom := "?", "?", r.Domain
		if r.SizeBytes >= 0 {
			size = humanSize(r.SizeBytes) // backup.go
		}
		if r.Tables >= 0 {
			tables = strconv.Itoa(r.Tables)
		}
		if dom == "" {
			dom = "-"
		}
		kind := e.tr("site", "site")
		if r.Kind == core.DBKindStandalone {
			kind = e.tr("rời", "standalone")
		}
		status := suspensionText(e, r.Suspended)
		if r.Status == string(account.DBTrashed) {
			status = e.tr("thùng rác", "trash")
		}
		// M34: DB có trong tpan mà MariaDB không còn. Nói ngay ở cột trạng
		// thái, kèm việc làm được, thay cho hai dấu "?" không ai hiểu.
		if r.Missing {
			status = e.tr("KHÔNG CÒN trong MariaDB", "GONE from MariaDB")
			if r.Kind == core.DBKindStandalone {
				status += e.tr(" (gỡ: tpan db rm "+r.DB+")", " (remove: tpan db rm "+r.DB+")")
			}
		}
		printf(w, "%-40s %-28s %12s %6s %-10s %-14s %-10s %s\n", dom, r.DB, size, tables, kind, r.Owner, r.Charset, status)
	}
}

// dbPlanCmd dựng một lệnh có thay đổi trên DB của site (dump, import): kiểm
// domain trước mọi việc khác, rồi dựng Plan và đưa cho runPlanned. --id chỉ có ở import.
func dbPlanCmd(e *env, use, short, action string,
	prepare func(ctx context.Context, b *core.Backups, d validate.Domain, id string) (*core.Planned, error),
	forDB func(ctx context.Context, m *m13Env, name, id string) (*core.Planned, error)) *cobra.Command {
	var (
		f  planFlags
		id string
	)
	c := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			d, name, err := parseDBArg(args[0])
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			// M33: không có dấu chấm là tên DB rời.
			if name != "" {
				return runM13Plan(cmd, e, f, false, func(ctx context.Context, m *m13Env) (*core.Planned, error) {
					return forDB(ctx, m, name, id)
				}, nil, nil)
			}
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, f.asJSON, err)
			}
			b, done, err := openBackups(ctx, e, f.dryRun)
			if err != nil {
				return err
			}
			defer done()
			p, err := prepare(ctx, b, d, id)
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			_, err = runPlanned(ctx, e, f, b.Sites.Journal, action, d.String(), p)
			return err
		},
	}
	if action == "db.import" {
		c.Flags().StringVar(&id, "id", "", "id của bản xuất cần nhập (xem bằng: sudo tpan backup list)")
		_ = c.MarkFlagRequired("id")
	}
	bindChangeFlags(c, &f)
	return c
}
