package main

// `tpan package add|list|edit|rm` (mục 6.12 của docs/KE-HOACH.md): gói hạn
// mức của user. Chỉ ghi tpan.db, không đụng máy, nên đi qua runAcctOp.

import (
	"context"
	"strconv"
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

// limitsText là các giới hạn của p cho câu "Sẽ: ..." (kèm trần của lồng khi gói bật lồng).
func limitsText(p account.Package) string {
	t := "site " + maxText(p.MaxSites) + ", DB " + maxText(p.MaxDBs) + ", disk " + maxText(p.DiskMB) + " MiB, backup " + maxText(p.MaxBackups)
	if p.Cage {
		t += ", lồng bật: CPU " + maxText(p.CPUPercent) + "%, RAM " + maxText(p.RAMMB) + " MiB, tiến trình " + maxText(p.Tasks) +
			", đĩa đọc " + maxText(p.IOReadMB) + "/ghi " + maxText(p.IOWriteMB) + " MB/s, worker " + strconv.Itoa(p.PHPWorkers)
	}
	return t
}

// cageFlagNames là tên bảy cờ của lồng (M17), để edit biết cờ nào được ghi.
var cageFlagNames = []string{"cage", "cpu", "ram-mb", "tasks", "io-read-mb", "io-write-mb", "php-workers"}

// cageLimitFlags là bảy cờ của lồng ở `package add` và `package edit`.
type cageLimitFlags struct {
	cage                                      string
	cpu, ram, tasks, ioRead, ioWrite, workers int
}

func (v *cageLimitFlags) bind(c *cobra.Command) {
	fl := c.Flags()
	fl.StringVar(&v.cage, "cage", "", "on hoặc off: site của gói có chạy trong lồng không (cần on để đặt các trần dưới đây)")
	fl.IntVar(&v.cpu, "cpu", 0, "trần CPU mỗi site trong lồng, % của một nhân (0: không giới hạn; 5 đến 6400)")
	fl.IntVar(&v.ram, "ram-mb", 0, "trần RAM mỗi site trong lồng, MiB (0: không giới hạn; 64 đến 1048576)")
	fl.IntVar(&v.tasks, "tasks", 0, "trần số tiến trình và luồng mỗi site trong lồng (0: không giới hạn; từ 32, và từ số worker + 15)")
	fl.IntVar(&v.ioRead, "io-read-mb", 0, "trần đọc đĩa mỗi site trong lồng, MB/giây (0: không giới hạn)")
	fl.IntVar(&v.ioWrite, "io-write-mb", 0, "trần ghi đĩa mỗi site trong lồng, MB/giây (0: không giới hạn)")
	fl.IntVar(&v.workers, "php-workers", 0, "số worker PHP-FPM (pm.max_children) của mỗi site trong lồng (0: mặc định 5; 1 đến 200)")
}

// check kiểm giá trị của --cage trước mọi việc khác.
func (v *cageLimitFlags) check(changed func(string) bool) error {
	if changed("cage") && v.cage != "on" && v.cage != "off" {
		return &core.RefusedError{Code: "args_invalid", Message: i18n.T(
			"--cage chỉ nhận on hoặc off (nhận được "+quote(v.cage)+")",
			"--cage accepts on or off (got "+quote(v.cage)+")")}
	}
	return nil
}

// apply chép các cờ đã ghi vào p; cờ không ghi thì p giữ giá trị đang có.
func (v *cageLimitFlags) apply(changed func(string) bool, p *account.Package) {
	if changed("cage") {
		p.Cage = v.cage == "on"
	}
	for _, f := range []struct {
		flag string
		in   int
		out  *int
	}{
		{"cpu", v.cpu, &p.CPUPercent}, {"ram-mb", v.ram, &p.RAMMB}, {"tasks", v.tasks, &p.Tasks},
		{"io-read-mb", v.ioRead, &p.IOReadMB}, {"io-write-mb", v.ioWrite, &p.IOWriteMB}, {"php-workers", v.workers, &p.PHPWorkers},
	} {
		if changed(f.flag) {
			*f.out = f.in
		}
	}
}

func packageAddCmd(e *env) *cobra.Command {
	var (
		f  planFlags
		p  account.Package
		cf cageLimitFlags
	)
	c := &cobra.Command{
		Use:   "add <tên> [--max-sites N] [--max-dbs N] [--disk-mb N] [--max-backups N] [--cage on|off] [--cpu N] [--ram-mb N] [--tasks N] [--io-read-mb N] [--io-write-mb N] [--php-workers N]",
		Short: "Thêm gói (0 ở một giới hạn là không giới hạn)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := parsePackageArg(args[0])
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			if err := cf.check(cmd.Flags().Changed); err != nil {
				return fail(e, f.asJSON, err)
			}
			p.Name = name
			cf.apply(cmd.Flags().Changed, &p)
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
	cf.bind(c)
	bindChangeFlags(c, &f)
	return c
}

func cageOnOff(e *env, on bool) string {
	if on {
		return e.tr("bật", "on")
	}
	return e.tr("tắt", "off")
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
			printf(w, "%-20s %6s %6s %10s %8s %6s %s\n", e.tr("TÊN", "NAME"), "SITE", "DB", "DISK MiB", "BACKUP", "USER", e.tr("LỒNG", "CAGE"))
			for _, r := range rows {
				p := r.Package
				printf(w, "%-20s %6s %6s %10s %8s %6d %s\n", p.Name, maxText(p.MaxSites), maxText(p.MaxDBs), maxText(p.DiskMB), maxText(p.MaxBackups), r.Users, cageOnOff(e, p.Cage))
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
		cf  cageLimitFlags
	)
	c := &cobra.Command{
		Use:     "edit <tên> [--max-sites N] [--max-dbs N] [--disk-mb N] [--max-backups N] [--cage on|off] [--cpu N] [--ram-mb N] [--tasks N] [--io-read-mb N] [--io-write-mb N] [--php-workers N]",
		Aliases: []string{"set"},
		Short:   "Sửa giới hạn của gói (cờ không ghi thì giữ nguyên; site trong lồng dùng gói được đồng bộ trần mới bằng một job)",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := parsePackageArg(args[0])
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			fl := cmd.Flags()
			if err := cf.check(fl.Changed); err != nil {
				return fail(e, f.asJSON, err)
			}
			var changed []string
			for _, n := range append([]string{"max-sites", "max-dbs", "disk-mb", "max-backups"}, cageFlagNames...) {
				if fl.Changed(n) {
					changed = append(changed, n)
				}
			}
			if len(changed) == 0 {
				return fail(e, f.asJSON, &core.RefusedError{Code: "args_invalid", Message: i18n.T(
					"không có gì để sửa: thêm ít nhất một trong --max-sites, --max-dbs, --disk-mb, --max-backups, --cage, --cpu, --ram-mb, --tasks, --io-read-mb, --io-write-mb, --php-workers",
					"nothing to change: give at least one of --max-sites, --max-dbs, --disk-mb, --max-backups, --cage, --cpu, --ram-mb, --tasks, --io-read-mb, --io-write-mb, --php-workers")})
			}
			// mutate đọc gói đang có rồi chỉ thay cờ đã ghi: EditPackage ghi đè
			// cả hàng, nên cờ vắng mặt phải mang giá trị cũ vào (kể cả trần của lồng).
			mutate := func(p *account.Package) {
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
				cf.apply(fl.Changed, p)
			}
			what := i18n.T("Sẽ: sửa gói "+name+": "+strings.Join(changed, ", ")+".", "Will: change package "+name+": "+strings.Join(changed, ", ")+".")
			// Đường cũ (không có site trong lồng dùng gói, hay trần không đổi): chỉ ghi dòng gói.
			plain := func() error {
				return runAcctOp(cmd, e, f, acctOp{
					action: "package.edit", target: name, what: what,
					do: func(ctx context.Context, a *core.Accounts, _ *core.DBs) error {
						p, ok, err := a.Store.Package(ctx, name)
						if err != nil {
							return err
						}
						if !ok {
							p.Name = name // EditPackage tự từ chối package_not_found
						}
						mutate(&p)
						return a.EditPackage(ctx, p)
					},
					done: packageDone(e, name, e.tr("Đã sửa gói ", "Updated package ")),
				})
			}
			return runCageEdit(cmd, e, f, "package.edit", name, what, func(ctx context.Context, c *core.Cages) (*core.Planned, error) {
				p, ok, err := c.Accounts.Package(ctx, name)
				if err != nil {
					return nil, err
				}
				if !ok {
					return nil, &core.RefusedError{Code: core.CodePackageNotFound, Message: i18n.T("không có gói "+name, "no package "+name)}
				}
				mutate(&p)
				return c.PrepareEditPackage(ctx, p)
			}, plain)
		},
	}
	packageLimitFlags(c, &set)
	cf.bind(c)
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
