package main

// `tpan trash list | purge <domain> | purge --expired | settings [--days N]`
// (mục 6.4 và 9 của docs/KE-HOACH.md). Đưa vào thùng rác là `tpan site rm`,
// khôi phục là `tpan site restore` (site.go).

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/validate"
)

func init() { register(trashCmd) }

func trashCmd(e *env) *cobra.Command {
	c := &cobra.Command{
		Use:   "trash",
		Short: "Xem thùng rác, xoá hẳn site trong đó, đặt số ngày giữ",
	}
	c.AddCommand(trashListCmd(e), trashPurgeCmd(e), trashSettingsCmd(e))
	return c
}

func trashListCmd(e *env) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "list",
		Short: "Liệt kê site trong thùng rác: lúc vào thùng rác và hạn xoá hẳn",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, asJSON, err)
			}
			s, done, err := openSites(cmd.Context(), e, false)
			if err != nil {
				return err
			}
			defer done()
			list, err := s.Trash(cmd.Context())
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(e.stdout, list)
			}
			printTrash(e, list)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
	return c
}

func printTrash(e *env, list []site.Site) {
	w := e.stdout
	if len(list) == 0 {
		printf(w, "%s\n", e.tr("Thùng rác trống.", "The trash is empty."))
		return
	}
	const layout = "2006-01-02 15:04"
	printf(w, "%-40s %-16s %s\n", "DOMAIN", e.tr("XOÁ LÚC", "TRASHED"), e.tr("HẠN XOÁ HẲN", "PURGE AT"))
	for _, s := range list {
		purge := e.tr("(không có hạn)", "(no deadline)")
		if !s.PurgeAt.IsZero() {
			purge = s.PurgeAt.Local().Format(layout)
		}
		printf(w, "%-40s %-16s %s\n", s.Domain, s.TrashedAt.Local().Format(layout), purge)
	}
}

func trashPurgeCmd(e *env) *cobra.Command {
	var (
		f       planRun
		expired bool
	)
	c := &cobra.Command{
		Use:   "purge <domain> | --expired",
		Short: "Xoá hẳn một site trong thùng rác, hoặc mọi site đã quá hạn giữ (không lấy lại được)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if expired == (len(args) == 1) {
				return fail(e, f.asJSON, &core.RefusedError{Code: "args_invalid", Message: i18n.T(
					"cần đúng một trong hai: tên domain, hoặc --expired",
					"give exactly one of: a domain name, or --expired")})
			}
			f.note = i18n.T(
				"Xoá hẳn: DB, file, log và user của site bị xoá, KHÔNG lấy lại được.",
				"Permanent delete: the site's database, files, logs and user are removed and CANNOT be recovered.")
			if expired {
				return trashPurgeExpired(cmd, e, &f)
			}
			return runSiteJob(cmd, e, &f, args[0], "site.purge",
				func(ctx context.Context, s *core.Sites, d validate.Domain) (siteJob, error) {
					p, err := s.PreparePurge(ctx, d)
					if err != nil {
						return siteJob{}, err
					}
					return siteJob{plan: p.Plan, preview: p.Preview, execute: p.Execute}, nil
				}, nil)
		},
	}
	c.Flags().BoolVar(&expired, "expired", false, "xoá hẳn mọi site đã quá hạn giữ (việc bộ hẹn giờ của tpan serve làm mỗi giờ)")
	f.flags(c)
	return c
}

// purgePreview là một dòng của `trash purge --expired --dry-run --json`.
type purgePreview struct {
	Domain  validate.Domain    `json:"domain"`
	Preview *engine.Preview    `json:"preview,omitempty"`
	Error   *core.RefusedError `json:"error,omitempty"`
}

// trashPurgeExpired: xem trước từng site quá hạn, hỏi lại một lần cho cả lô,
// rồi gọi core.PurgeExpired (mỗi site một job, site lỗi không chặn site sau).
func trashPurgeExpired(cmd *cobra.Command, e *env, f *planRun) error {
	ctx := cmd.Context()
	if !f.dryRun {
		if err := requireRoot(e, cmd); err != nil {
			return fail(e, f.asJSON, err)
		}
	}
	s, done, err := openSites(ctx, e, f.dryRun)
	if err != nil {
		return err
	}
	defer done()

	now := time.Now()
	exp, err := s.Expired(ctx, now)
	if err != nil {
		return err
	}
	if len(exp) == 0 {
		if f.asJSON {
			return writeJSON(e.stdout, []core.PurgeResult{})
		}
		printf(e.stdout, "%s\n", e.tr("Không có site nào quá hạn giữ.", "No site is past its retention."))
		return nil
	}

	// Dựng Plan của từng site (chỉ đọc): cho bản xem trước và cho tên bước
	// trong tiến độ. PurgeExpired tự dựng lại lúc chạy.
	var all engine.Plan
	rows := make([]purgePreview, 0, len(exp))
	plans := map[string]*core.SitePurge{}
	for _, st := range exp {
		row := purgePreview{Domain: st.Domain}
		p, err := s.PreparePurge(ctx, st.Domain)
		var re *core.RefusedError
		switch {
		case errors.As(err, &re):
			row.Error = re
		case err != nil:
			return err
		default:
			plans[st.Domain.String()] = p
			all.Steps = append(all.Steps, p.Plan.Steps...)
		}
		rows = append(rows, row)
	}
	ok, err := f.showOrConfirm(e, func() (any, func(), error) {
		for i := range rows {
			p := plans[rows[i].Domain.String()]
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

	res, err := s.PurgeExpired(ctx, now, f.progress(e, all).onEvent)
	if err != nil {
		return err
	}
	failed := false
	for _, r := range res {
		cliAudit(ctx, e, s.Store, "site.purge", r.Domain.String(), r.Report.Job)
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
		if err := writeJSON(e.stdout, res); err != nil {
			return err
		}
	}
	if failed {
		return &exitCode{1}
	}
	return nil
}

func trashSettingsCmd(e *env) *cobra.Command {
	var (
		days   int
		asJSON bool
	)
	c := &cobra.Command{
		Use:   "settings [--days N]",
		Short: "Xem hoặc đặt số ngày giữ site trong thùng rác trước khi xoá hẳn (không cờ thì chỉ xem)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, asJSON, err)
			}
			s, done, err := openSites(ctx, e, false)
			if err != nil {
				return err
			}
			defer done()
			if cmd.Flags().Changed("days") {
				err := s.SetTrashDays(ctx, days)
				trashSettingsAudit(ctx, e, s.Store, err)
				if err != nil {
					return fail(e, asJSON, err)
				}
			}
			n, err := s.TrashDays(ctx)
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(e.stdout, map[string]int{"days": n})
			}
			printf(e.stdout, "%s\n", e.tr(
				"Số ngày giữ site trong thùng rác: "+strconv.Itoa(n)+" (đổi số này chỉ áp cho site vào thùng rác sau đó).",
				"Days a site stays in the trash: "+strconv.Itoa(n)+" (a change only applies to sites trashed afterwards)."))
			return nil
		},
	}
	c.Flags().IntVar(&days, "days", core.DefaultTrashDays, "số ngày giữ, 0 tới 365 (0: xoá hẳn ở lần dọn kế tiếp)")
	c.Flags().BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
	return c
}

// trashSettingsAudit ghi nhật ký lần đặt số ngày giữ (kể cả bị từ chối, với
// mã từ chối), như ssl settings.
func trashSettingsAudit(ctx context.Context, e *env, w any, err error) {
	aw, ok := w.(auditWriter)
	if !ok {
		return
	}
	result := "ok"
	if re := (*core.RefusedError)(nil); errors.As(err, &re) {
		result = re.Code
	} else if err != nil {
		result = "error"
	}
	if err := aw.AddAudit(ctx, store.AuditEntry{Time: time.Now(), IP: "cli", Action: "trash.settings", Target: "trash", Result: result}); err != nil {
		printf(e.stderr, "tpan: %v\n", err)
	}
}
