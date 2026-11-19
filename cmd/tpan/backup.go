package main

// `tpan backup create <domain> | --all [--scheduled]`, `backup list [<domain>]`,
// `backup restore <domain> [--id ID]`, `backup settings [--keep N] [--at HH:MM
// | --off]` (mục 6.5 và 9 của docs/KE-HOACH.md). Lệnh một site đi qua
// runPlanned như các lệnh M12; `--all` đi như `trash purge --expired`: xem
// trước từng site, hỏi lại một lần, mỗi site một job.

import (
	"errors"
	"regexp"
	"slices"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
)

func init() { register(backupCmd) }

func backupCmd(e *env) *cobra.Command {
	c := &cobra.Command{
		Use:   "backup",
		Short: "Backup site (mã nguồn và DB), xem, khôi phục, đặt lịch backup hằng ngày",
	}
	c.AddCommand(backupCreateCmd(e), backupListCmd(e), backupRestoreCmd(e), backupSettingsCmd(e))
	return c
}

func (f *planRun) planFlags() planFlags {
	return planFlags{dryRun: f.dryRun, yes: f.yes, asJSON: f.asJSON, verbose: f.verbose}
}

func backupCreateCmd(e *env) *cobra.Command {
	var (
		f              planRun
		all, scheduled bool
	)
	c := &cobra.Command{
		Use:   "create <domain> | --all [--scheduled]",
		Short: "Backup một site, hoặc mọi site đang chạy rồi chép tpan.db",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if all == (len(args) == 1) || (scheduled && !all) {
				return fail(e, f.asJSON, &core.RefusedError{Code: "args_invalid", Message: i18n.T(
					"cần đúng một trong hai: tên domain, hoặc --all (--scheduled chỉ đi với --all)",
					"give exactly one of: a domain name, or --all (--scheduled only goes with --all)")})
			}
			if all {
				reason := core.ReasonManual
				if scheduled {
					reason = core.ReasonSchedule
				}
				return backupCreateAll(cmd, e, &f, reason)
			}
			d, err := parseDomainArg(args[0])
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			if !f.dryRun {
				if err := requireRoot(e, cmd); err != nil {
					return fail(e, f.asJSON, err)
				}
			}
			b, done, err := openBackups(ctx, e, f.dryRun)
			if err != nil {
				return err
			}
			defer done()
			p, err := b.PrepareCreate(ctx, d, core.ReasonManual)
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			_, err = runPlanned(ctx, e, f.planFlags(), b.Sites.Store, "backup.create", d.String(), p)
			return err
		},
	}
	c.Flags().BoolVar(&all, "all", false, "backup mọi site đang chạy, mỗi site một job, rồi chép tpan.db (việc lịch backup hằng ngày làm)")
	c.Flags().BoolVar(&scheduled, "scheduled", false, "ghi lý do \"theo lịch\" vào các bản backup (dòng cron của lịch backup dùng cờ này)")
	f.flags(c)
	return c
}

// backupPreviewRow là một dòng của `backup create --all --dry-run --json`.
type backupPreviewRow struct {
	Domain  string             `json:"domain"`
	Preview *engine.Preview    `json:"preview,omitempty"`
	Error   *core.RefusedError `json:"error,omitempty"`
}

// backupAllResult là kết quả `backup create --all --json`.
type backupAllResult struct {
	Sites []core.BackupResult `json:"sites"`
	// TPANDB là bản sao tpan.db vừa ghi; rỗng khi chép lỗi (xem Error).
	TPANDB string `json:"tpan_db,omitempty"`
	Error  string `json:"error,omitempty"`
}

// backupCreateAll: xem trước từng site active, hỏi lại một lần cho cả lô, rồi
// core.CreateAll (mỗi site một job, site lỗi không chặn site sau, cuối cùng
// chép tpan.db). Thoát 1 khi có site lỗi hoặc không chép được tpan.db.
func backupCreateAll(cmd *cobra.Command, e *env, f *planRun, reason core.BackupReason) error {
	ctx := cmd.Context()
	if !f.dryRun {
		if err := requireRoot(e, cmd); err != nil {
			return fail(e, f.asJSON, err)
		}
	}
	b, done, err := openBackups(ctx, e, f.dryRun)
	if err != nil {
		return err
	}
	defer done()
	sites, err := b.Sites.Store.Sites(ctx)
	if err != nil {
		return err
	}

	// Dựng Plan từng site (chỉ đọc) cho bản xem trước và cho tên bước trong
	// tiến độ; CreateAll tự dựng lại lúc chạy.
	var all engine.Plan
	rows := []backupPreviewRow{}
	plans := map[string]*core.Planned{}
	for _, st := range sites {
		if st.Status != site.Active {
			continue
		}
		row := backupPreviewRow{Domain: st.Domain.String()}
		p, err := b.PrepareCreate(ctx, st.Domain, reason)
		var re *core.RefusedError
		switch {
		case errors.As(err, &re):
			row.Error = re
		case err != nil:
			return err
		default:
			plans[row.Domain] = p
			all.Steps = append(all.Steps, p.Plan.Steps...)
		}
		rows = append(rows, row)
	}
	f.note = i18n.T("Sau các site: chép tpan.db vào "+site.TPANDBBackupDir+" (giữ "+strconv.Itoa(core.DefaultBackupKeep)+" bản mới nhất).",
		"After the sites: copy tpan.db to "+site.TPANDBBackupDir+" (keeping the newest "+strconv.Itoa(core.DefaultBackupKeep)+").")
	ok, err := f.showOrConfirm(e, func() (any, func(), error) {
		for i := range rows {
			p := plans[rows[i].Domain]
			if p == nil {
				continue
			}
			pv, err := p.Preview(ctx)
			if err != nil {
				return nil, nil, err
			}
			rows[i].Preview = &pv
		}
		return rows, func() {
			if len(rows) == 0 {
				printf(e.stdout, "%s\n", e.tr("Không có site nào đang chạy.", "No site is running."))
			}
			for _, r := range rows {
				printf(e.stdout, "== %s\n", r.Domain)
				if r.Error != nil {
					printRefusal(e, r.Error)
					continue
				}
				printPreview(e, *r.Preview)
			}
		}, nil
	})
	if !ok {
		return err
	}

	res, err := b.CreateAll(ctx, reason, f.progress(e, all).onEvent)
	if res == nil && err != nil {
		return err
	}
	out := backupAllResult{Sites: res}
	if err != nil {
		out.Error = err.Error()
	} else {
		out.TPANDB = newestSnapshot(e)
	}
	failed := err != nil
	for _, r := range res {
		cliAudit(ctx, e, b.Sites.Store, "backup.create", r.Domain, r.Report.Job)
		if r.Error != "" || r.Report.Job.Status != engine.JobSucceeded {
			failed = true
		}
		if f.asJSON {
			continue
		}
		printf(e.stdout, "\n== %s\n", r.Domain)
		if r.Report.Job.ID != "" {
			printReport(e, r.Report)
		}
		if r.Error != "" {
			printf(e.stdout, "%s %s\n", e.tr("Lỗi:", "Error:"), r.Error)
		}
	}
	if f.asJSON {
		if werr := writeJSON(e.stdout, out); werr != nil {
			return werr
		}
	} else if out.Error != "" {
		printf(e.stdout, "\n%s %s\n", e.tr("Không chép được tpan.db:", "Could not copy tpan.db:"), out.Error)
	} else {
		printf(e.stdout, "\n%s %s\n", e.tr("Bản sao tpan.db:", "Copy of tpan.db:"), out.TPANDB)
	}
	if failed {
		return &exitCode{1}
	}
	return nil
}

// snapName: tên bản sao tpan.db do core.SnapshotTPANDB đặt.
var snapName = regexp.MustCompile(`^tpan-\d{8}-\d{6}\.db$`)

// newestSnapshot là bản sao tpan.db mới nhất (tên có giờ nên sắp theo chữ là
// sắp theo thời gian). ponytail: CreateAll không trả đường dẫn này (hợp đồng
// chỉ trả kết quả từng site), nên đọc lại thư mục; trả đường dẫn từ CreateAll
// thì bỏ hàm này.
func newestSnapshot(e *env) string {
	names, err := e.sys.ReadDir(site.TPANDBBackupDir)
	if err != nil {
		return ""
	}
	var snaps []string
	for _, n := range names {
		if snapName.MatchString(n) {
			snaps = append(snaps, n)
		}
	}
	if len(snaps) == 0 {
		return ""
	}
	return site.TPANDBBackupDir + "/" + slices.Max(snaps)
}

func backupListCmd(e *env) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "list [<domain>]",
		Short: "Liệt kê bản backup (cả bản xuất DB), mới nhất trước",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			dom := ""
			if len(args) == 1 {
				d, err := parseDomainArg(args[0])
				if err != nil {
					return fail(e, asJSON, err)
				}
				dom = d.String()
			}
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, asJSON, err)
			}
			b, done, err := openBackups(ctx, e, false)
			if err != nil {
				return err
			}
			defer done()
			list, err := b.List(ctx, dom)
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(e.stdout, list)
			}
			printBackups(e, list)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
	return c
}

func printBackups(e *env, list []core.Backup) {
	w := e.stdout
	if len(list) == 0 {
		printf(w, "%s\n", e.tr("Chưa có bản backup nào.", "There are no backups yet."))
		return
	}
	printf(w, "%-16s %-30s %-4s %-14s %10s %-16s %s\n", "ID", "DOMAIN", e.tr("LOẠI", "KIND"),
		e.tr("LÝ DO", "REASON"), e.tr("CỠ", "SIZE"), e.tr("LÚC TẠO", "CREATED"), "FILE")
	for _, b := range list {
		printf(w, "%-16s %-30s %-4s %-14s %10s %-16s %s\n", b.ID, b.Domain, b.Kind, b.Reason,
			humanSize(b.Size), b.Created.Local().Format("2006-01-02 15:04"), b.Path)
	}
}

// humanSize: cỡ file cho người đọc (KiB, MiB, GiB theo 1024).
func humanSize(n int64) string {
	const k = 1024
	f := float64(n)
	switch {
	case n >= k*k*k:
		return strconv.FormatFloat(f/(k*k*k), 'f', 1, 64) + " GiB"
	case n >= k*k:
		return strconv.FormatFloat(f/(k*k), 'f', 1, 64) + " MiB"
	case n >= k:
		return strconv.FormatFloat(f/k, 'f', 1, 64) + " KiB"
	}
	return strconv.FormatInt(n, 10) + " B"
}

func backupRestoreCmd(e *env) *cobra.Command {
	var (
		f  planFlags
		id string
	)
	c := &cobra.Command{
		Use:   "restore <domain> [--id ID]",
		Short: "Khôi phục mã nguồn và DB của site từ một bản backup (tự backup hiện trạng trước)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			d, err := parseDomainArg(args[0])
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			if !f.dryRun {
				if err := requireRoot(e, cmd); err != nil {
					return fail(e, f.asJSON, err)
				}
			}
			b, done, err := openBackups(ctx, e, f.dryRun)
			if err != nil {
				return err
			}
			defer done()
			p, err := b.PrepareRestore(ctx, d, id)
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			_, err = runPlanned(ctx, e, f, b.Sites.Store, "backup.restore", d.String(), p)
			return err
		},
	}
	c.Flags().StringVar(&id, "id", "", "id bản backup (xem bằng: sudo tpan backup list <domain>); bỏ trống là bản đầy đủ mới nhất")
	bindChangeFlags(c, &f)
	return c
}

func backupSettingsCmd(e *env) *cobra.Command {
	var (
		f    planFlags
		keep int
		at   string
		off  bool
	)
	c := &cobra.Command{
		Use:   "settings [--keep N] [--at HH:MM | --off]",
		Short: "Xem hoặc đặt số bản giữ mỗi site và giờ backup hằng ngày (không cờ thì chỉ xem)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			fl := cmd.Flags()
			setKeep, setAt := fl.Changed("keep"), fl.Changed("at")
			if setAt && off {
				return fail(e, f.asJSON, &core.RefusedError{Code: "args_invalid", Message: i18n.T(
					"--at và --off không đi cùng nhau: --at đặt giờ backup, --off tắt lịch",
					"--at and --off do not go together: --at sets the backup time, --off turns the schedule off")})
			}
			change := setKeep || setAt || off
			// Xem cũng cần root: tpan.db chỉ root đọc được.
			if !change || !f.dryRun {
				if err := requireRoot(e, cmd); err != nil {
					return fail(e, f.asJSON, err)
				}
			}
			b, done, err := openBackups(ctx, e, change && f.dryRun)
			if err != nil {
				return err
			}
			defer done()
			// Đổi một nửa thì nửa kia giữ giá trị hiện tại. Đổi cả hai thì không
			// cần đọc: lệnh này cũng là cách sửa một giá trị hỏng trong DB.
			cur := core.BackupSettings{Keep: keep}
			if !setKeep || (!setAt && !off) {
				if cur, err = b.Settings(ctx); err != nil {
					return err
				}
			}
			if !change {
				if f.asJSON {
					return writeJSON(e.stdout, cur)
				}
				printBackupSettings(e, cur)
				return nil
			}
			if setKeep {
				cur.Keep = keep
			}
			if setAt {
				cur.At = at
			}
			if off {
				cur.At = ""
			}
			p, err := b.PrepareSettings(ctx, cur.Keep, cur.At)
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			_, err = runPlanned(ctx, e, f, b.Sites.Store, "backup.settings", "backup", p)
			return err
		},
	}
	c.Flags().IntVar(&keep, "keep", core.DefaultBackupKeep, "số bản backup giữ mỗi site, 1 tới "+strconv.Itoa(core.MaxBackupKeep)+" (bản cũ hơn bị xoá sau mỗi lần backup)")
	c.Flags().StringVar(&at, "at", "", "giờ backup mọi site hằng ngày, HH:MM theo giờ máy (ví dụ 03:30)")
	c.Flags().BoolVar(&off, "off", false, "tắt lịch backup hằng ngày")
	bindChangeFlags(c, &f)
	return c
}

func printBackupSettings(e *env, s core.BackupSettings) {
	w := e.stdout
	printf(w, "%s %d\n", e.tr("Số bản giữ mỗi site:", "Copies kept per site:"), s.Keep)
	if s.At == "" {
		printf(w, "%s\n", e.tr("Lịch backup hằng ngày: tắt (bật bằng: sudo tpan backup settings --at 03:30)",
			"Daily backup: off (turn on with: sudo tpan backup settings --at 03:30)"))
		return
	}
	printf(w, "%s %s (%s, %s)\n", e.tr("Lịch backup hằng ngày: lúc", "Daily backup at"), s.At,
		e.tr("giờ máy", "machine time"), site.BackupCronFile)
}
