package main

// `tpan package add|list|edit|rm` (mục 6.12 của docs/KE-HOACH.md): gói hạn
// mức của user. Chỉ ghi tpan.db, không đụng máy, nên đi qua runAcctOp.

import (
	"context"
	"strings"

	"github.com/spf13/cobra"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/server"
	"github.com/doxuta/tpan/internal/validate"
)

func init() { register(packageCmd) }

func packageCmd(e *env) *cobra.Command {
	c := &cobra.Command{
		Use:   "package",
		Short: "Gói hạn mức của user: số site, số DB rời, dung lượng, số backup mỗi site",
	}
	c.AddCommand(packageAddCmd(e), packageListCmd(e), packageEditCmd(e), packageRmCmd(e))
	return c
}

// parsePackageArg kiểm tên gói người dùng gõ, trước mọi việc khác.
func parsePackageArg(s string) (string, error) {
	n, err := validate.ParsePackageName(s)
	return n, asRefusal(err)
}

// packageLimitFlags gắn bốn cờ giới hạn vào p.
func packageLimitFlags(c *cobra.Command, p *account.Package) {
	fl := c.Flags()
	fl.IntVar(&p.MaxSites, "max-sites", 0, "số site tối đa (0: không giới hạn; site trong thùng rác vẫn tính)")
	fl.IntVar(&p.MaxDBs, "max-dbs", 0, "số DB rời tối đa (0: không giới hạn; không tính DB đi kèm site)")
	fl.IntVar(&p.DiskMB, "disk-mb", 0, "dung lượng tối đa, MiB (0: không giới hạn; vượt thì chặn tạo mới)")
	fl.IntVar(&p.MaxBackups, "max-backups", 0, "số bản backup đầy đủ tối đa mỗi site (0: không giới hạn)")
}

// limitsText là bốn giới hạn của p cho câu "Sẽ: ...".
func limitsText(p account.Package) string {
	return "site " + maxText(p.MaxSites) + ", DB " + maxText(p.MaxDBs) + ", disk " + maxText(p.DiskMB) + " MiB, backup " + maxText(p.MaxBackups)
}

func packageAddCmd(e *env) *cobra.Command {
	var (
		f planFlags
		p account.Package
	)
	c := &cobra.Command{
		Use:   "add <tên> [--max-sites N] [--max-dbs N] [--disk-mb N] [--max-backups N]",
		Short: "Thêm gói (0 ở một giới hạn là không giới hạn)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := parsePackageArg(args[0])
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			p.Name = name
			return runAcctOp(cmd, e, f, acctOp{
				action: "package.add", target: name,
				what: i18n.T("Sẽ: thêm gói "+name+" ("+limitsText(p)+").", "Will: add package "+name+" ("+limitsText(p)+")."),
				do: func(ctx context.Context, a *core.Accounts, _ *core.DBs) error {
					return a.AddPackage(ctx, p)
				},
				done: packageDone(e, name, e.tr("Đã thêm gói ", "Added package ")),
			})
		},
	}
	packageLimitFlags(c, &p)
	bindChangeFlags(c, &f)
	return c
}

// packageDone in lại gói vừa ghi, đọc lại từ store (cùng dạng server.PackageRow).
func packageDone(e *env, name, verb string) func(context.Context, *core.Accounts) (any, func(), error) {
	return func(ctx context.Context, a *core.Accounts) (any, func(), error) {
		all, err := a.Packages(ctx)
		if err != nil {
			return nil, nil, err
		}
		row := server.PackageRow{Package: account.Package{Name: name}}
		for _, p := range all {
			if p.Package.Name == name {
				row = server.PackageRow{Package: p.Package, Users: p.Users}
			}
		}
		return row, func() { printf(e.stdout, "%s%s: %s.\n", verb, name, limitsText(row.Package)) }, nil
	}
}

func packageListCmd(e *env) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "list",
		Short: "Liệt kê gói: bốn giới hạn và số user đang dùng",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			m, done, err := openAccountsRead(cmd, e, asJSON)
			if err != nil {
				return err
			}
			defer done()
			all, err := m.acc.Packages(cmd.Context())
			if err != nil {
				return err
			}
			rows := make([]server.PackageRow, len(all))
			for i, p := range all {
				rows[i] = server.PackageRow{Package: p.Package, Users: p.Users}
			}
			if asJSON {
				return writeJSON(e.stdout, rows)
			}
			w := e.stdout
			printf(w, "%-20s %6s %6s %10s %8s %6s\n", e.tr("TÊN", "NAME"), "SITE", "DB", "DISK MiB", "BACKUP", "USER")
			for _, r := range rows {
				p := r.Package
				printf(w, "%-20s %6s %6s %10s %8s %6d\n", p.Name, maxText(p.MaxSites), maxText(p.MaxDBs), maxText(p.DiskMB), maxText(p.MaxBackups), r.Users)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
	return c
}

func packageEditCmd(e *env) *cobra.Command {
	var (
		f   planFlags
		set account.Package
	)
	c := &cobra.Command{
		Use:   "edit <tên> [--max-sites N] [--max-dbs N] [--disk-mb N] [--max-backups N]",
		Short: "Sửa giới hạn của gói (cờ không ghi thì giữ nguyên; hạ dưới phần đang dùng chỉ chặn tạo mới)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := parsePackageArg(args[0])
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			fl := cmd.Flags()
			var changed []string
			for _, n := range []string{"max-sites", "max-dbs", "disk-mb", "max-backups"} {
				if fl.Changed(n) {
					changed = append(changed, n)
				}
			}
			if len(changed) == 0 {
				return fail(e, f.asJSON, &core.RefusedError{Code: "args_invalid", Message: i18n.T(
					"không có gì để sửa: thêm ít nhất một trong --max-sites, --max-dbs, --disk-mb, --max-backups",
					"nothing to change: give at least one of --max-sites, --max-dbs, --disk-mb, --max-backups")})
			}
			return runAcctOp(cmd, e, f, acctOp{
				action: "package.edit", target: name,
				what: i18n.T("Sẽ: sửa gói "+name+": "+strings.Join(changed, ", ")+".", "Will: change package "+name+": "+strings.Join(changed, ", ")+"."),
				do: func(ctx context.Context, a *core.Accounts, _ *core.DBs) error {
					// EditPackage ghi đè cả bốn giới hạn: đọc gói cũ, chỉ thay cờ đã ghi.
					p, ok, err := a.Store.Package(ctx, name)
					if err != nil {
						return err
					}
					if !ok {
						p.Name = name // EditPackage tự từ chối package_not_found
					}
					for _, n := range changed {
						switch n {
						case "max-sites":
							p.MaxSites = set.MaxSites
						case "max-dbs":
							p.MaxDBs = set.MaxDBs
						case "disk-mb":
							p.DiskMB = set.DiskMB
						case "max-backups":
							p.MaxBackups = set.MaxBackups
						}
					}
					return a.EditPackage(ctx, p)
				},
				done: packageDone(e, name, e.tr("Đã sửa gói ", "Updated package ")),
			})
		},
	}
	packageLimitFlags(c, &set)
	bindChangeFlags(c, &f)
	return c
}

func packageRmCmd(e *env) *cobra.Command {
	var f planFlags
	c := &cobra.Command{
		Use:   "rm <tên>",
		Short: "Xoá gói (không xoá được gói default và gói còn user dùng)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := parsePackageArg(args[0])
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			return runAcctOp(cmd, e, f, acctOp{
				action: "package.rm", target: name,
				what: i18n.T("Sẽ: xoá gói "+name+".", "Will: delete package "+name+"."),
				do: func(ctx context.Context, a *core.Accounts, _ *core.DBs) error {
					return a.DeletePackage(ctx, name)
				},
				done: func(context.Context, *core.Accounts) (any, func(), error) {
					return map[string]string{"deleted": name}, func() {
						printf(e.stdout, "%s\n", e.tr("Đã xoá gói "+name+".", "Package "+name+" deleted."))
					}, nil
				},
			})
		},
	}
	bindChangeFlags(c, &f)
	return c
}
