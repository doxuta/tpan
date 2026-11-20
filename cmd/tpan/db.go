package main

// `tpan db list | passwd <domain> | dump <domain> | import <domain> --id ID`
// (mục 6.9 và 9 của docs/KE-HOACH.md). Mỗi site có đúng một DB; không có lệnh
// tạo DB rời hay gõ SQL tuỳ ý (mục 1). Ba lệnh có thay đổi đi qua runPlanned:
// xem trước, hỏi lại, chạy có tiến độ, ghi audit.

import (
	"context"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/validate"
)

func init() { register(dbCmd) }

func dbCmd(e *env) *cobra.Command {
	c := &cobra.Command{
		Use:   "db",
		Short: "DB của các site: xem, đổi mật khẩu, xuất và nhập file .sql",
	}
	c.AddCommand(dbListCmd(e),
		dbPlanCmd(e, "passwd <domain>", "Đổi mật khẩu user DB của site (mật khẩu mới không in ra)", "db.passwd",
			func(ctx context.Context, b *core.Backups, d validate.Domain, _ string) (*core.Planned, error) {
				return b.PrepareDBPassword(ctx, d)
			}),
		dbPlanCmd(e, "dump <domain>", "Xuất DB của site ra file .sql trong thư mục backup của site", "db.dump",
			func(ctx context.Context, b *core.Backups, d validate.Domain, _ string) (*core.Planned, error) {
				return b.PrepareDBDump(ctx, d)
			}),
		dbPlanCmd(e, "import <domain> --id ID", "Nhập lại một bản xuất đã có (tự xuất bản hiện tại trước để hoàn tác được)", "db.import",
			func(ctx context.Context, b *core.Backups, d validate.Domain, id string) (*core.Planned, error) {
				return b.PrepareDBImport(ctx, d, id)
			}),
	)
	return c
}

// dbRow là một dòng của `tpan db list --json`, cùng tên trường với API
// (server.DatabaseRow) để script đọc được cả hai như nhau.
type dbRow struct {
	Domain    string `json:"domain"`
	DB        string `json:"db"`
	User      string `json:"user"`
	SizeBytes int64  `json:"size_bytes"` // -1 khi không đọc được
	Tables    int    `json:"tables"`     // -1 khi không đọc được
}

func dbListCmd(e *env) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "list",
		Short: "Liệt kê DB của mọi site: tên DB, user, dung lượng, số bảng",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, asJSON, err)
			}
			// Chỉ đọc: chưa có tpan.db thì không tạo, danh sách rỗng.
			s, done, err := openSites(cmd.Context(), e, true)
			if err != nil {
				return err
			}
			defer done()
			list, err := s.Databases(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([]dbRow, len(list))
			for i, d := range list {
				rows[i] = dbRow{Domain: d.Domain, DB: d.DB, User: d.User, SizeBytes: d.SizeBytes, Tables: d.Tables}
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
		printf(w, "%s\n", e.tr("Chưa có site nào, nên chưa có DB nào.", "No sites yet, so no databases."))
		return
	}
	printf(w, "%-40s %-28s %12s %6s\n", "DOMAIN", "DB", e.tr("DUNG LƯỢNG", "SIZE"), e.tr("BẢNG", "TABLES"))
	for _, r := range rows {
		size, tables := "?", "?"
		if r.SizeBytes >= 0 {
			size = humanSize(r.SizeBytes) // backup.go
		}
		if r.Tables >= 0 {
			tables = strconv.Itoa(r.Tables)
		}
		printf(w, "%-40s %-28s %12s %6s\n", r.Domain, r.DB, size, tables)
	}
}

// dbPlanCmd dựng một lệnh có thay đổi của nhóm db: kiểm domain trước mọi
// việc khác, rồi dựng Plan và đưa cho runPlanned. --id chỉ có ở import.
func dbPlanCmd(e *env, use, short, action string,
	prepare func(ctx context.Context, b *core.Backups, d validate.Domain, id string) (*core.Planned, error)) *cobra.Command {
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
			d, err := parseDomainArg(args[0])
			if err != nil {
				return fail(e, f.asJSON, err)
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
		c.Flags().StringVar(&id, "id", "", "id của bản xuất cần nhập (xem bằng: sudo tpan backup list <domain>)")
		_ = c.MarkFlagRequired("id")
	}
	bindChangeFlags(c, &f)
	return c
}
