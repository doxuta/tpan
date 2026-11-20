package main

// `tpan trash list | purge <domain> | purge --expired | settings [--days N]`
// (mục 6.4 và 9 của docs/KE-HOACH.md). Đưa vào thùng rác là `tpan site rm`,
// khôi phục là `tpan site restore` (site.go). Từ M13 thùng rác có cả DB rời
// (`tpan db rm|restore|purge`, db.go): list liệt kê chúng, purge --expired
// dọn cả DB rời hết hạn như bộ hẹn giờ của tpan serve.

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/doxuta/tpan/internal/account"
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
		Short: "Liệt kê site và DB rời trong thùng rác: lúc vào thùng rác và hạn xoá hẳn",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, asJSON, err)
			}
			ctx := cmd.Context()
			m, done, err := openM13(ctx, e, false)
			if err != nil {
				return err
			}
			defer done()
			list, err := m.sites.Trash(ctx)
			if err != nil {
				return err
			}
			dbs, err := trashedDBs(ctx, m)
			if err != nil {
				return err
			}
			if asJSON {
				// M13: từ mảng site thành {sites, dbs}, cùng tên trường với
				// GET /api/v1/trash (server.TrashOverview).
				return writeJSON(e.stdout, trashListJSON{Sites: list, DBs: dbs})
			}
			printTrash(e, list, dbs)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
	return c
}

// trashListJSON là output --json của trash list.
type trashListJSON struct {
	Sites []site.Site        `json:"sites"`
	DBs   []account.Database `json:"dbs"`
}

// trashedDBs là DB rời đang ở thùng rác, hạn xoá hẳn sớm nhất trước.
func trashedDBs(ctx context.Context, m *m13Env) ([]account.Database, error) {
	out := []account.Database{}
	if m.dbs.Store == nil {
		return out, nil
	}
	all, err := m.dbs.Store.StandaloneDBs(ctx, "")
	if err != nil {
		return nil, err
	}
	for _, d := range all {
		if d.Status == account.DBTrashed {
			out = append(out, d)
		}
	}
	slices.SortFunc(out, func(a, b account.Database) int { return a.PurgeAt.Compare(b.PurgeAt) })
	return out, nil
}

func printTrash(e *env, list []site.Site, dbs []account.Database) {
	w := e.stdout
	if len(list) == 0 && len(dbs) == 0 {
		printf(w, "%s\n", e.tr("Thùng rác trống.", "The trash is empty."))
		return
	}
	const layout = "2006-01-02 15:04"
	purgeText := func(t time.Time) string {
		if t.IsZero() {
			return e.tr("(không có hạn)", "(no deadline)")
		}
		return t.Local().Format(layout)
	}
	if len(list) > 0 {
		printf(w, "%-40s %-16s %s\n", "DOMAIN", e.tr("XOÁ LÚC", "TRASHED"), e.tr("HẠN XOÁ HẲN", "PURGE AT"))
		for _, s := range list {
			printf(w, "%-40s %-16s %s\n", s.Domain, s.TrashedAt.Local().Format(layout), purgeText(s.PurgeAt))
		}
	}
	if len(dbs) > 0 {
		if len(list) > 0 {
			printf(w, "\n")
		}
		printf(w, "%-40s %-16s %-16s %s\n", e.tr("DB RỜI", "STANDALONE DB"), e.tr("CHỦ", "OWNER"), e.tr("XOÁ LÚC", "TRASHED"), e.tr("HẠN XOÁ HẲN", "PURGE AT"))
		for _, d := range dbs {
			printf(w, "%-40s %-16s %-16s %s\n", d.Name, d.Owner, d.TrashedAt.Local().Format(layout), purgeText(d.PurgeAt))
		}
	}
}

func trashPurgeCmd(e *env) *cobra.Command {
	var (
		f       planRun
		expired bool
	)
	c := &cobra.Command{
		Use:   "purge <domain> | --expired",
		Short: "Xoá hẳn một site trong thùng rác, hoặc mọi site và DB rời đã quá hạn giữ (site tự backup trước)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if expired == (len(args) == 1) {
				return fail(e, f.asJSON, &core.RefusedError{Code: "args_invalid", Message: i18n.T(
					"cần đúng một trong hai: tên domain, hoặc --expired",
					"give exactly one of: a domain name, or --expired")})
			}
			f.note = i18n.T(
				"Xoá hẳn: DB, file, log và user của site bị xoá. Bước đầu tiên tự backup site vào /var/lib/tpan/backups/<domain>/ (xem bằng: sudo tpan backup list <domain>); muốn lấy lại thì tạo lại site cùng domain rồi khôi phục bản đó.",
				"Permanent delete: the site's database, files, logs and user are removed. The first step backs the site up to /var/lib/tpan/backups/<domain>/ (see it with: sudo tpan backup list <domain>); to get it back, create the site again with the same domain and restore that backup.")
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

// purgeDBPreview là một DB rời của `trash purge --expired --dry-run --json`.
type purgeDBPreview struct {
	Name    string             `json:"name"`
	Preview *engine.Preview    `json:"preview,omitempty"`
	Error   *core.RefusedError `json:"error,omitempty"`
}

// purgeExpiredJSON là output --json của trash purge --expired (M13: từ mảng
// site thành {sites, dbs}). Khi --dry-run: Sites là []purgePreview, DBs là
// []purgeDBPreview. Khi chạy: Sites là []core.PurgeResult, DBs là tên DB rời
// đã xoá hẳn, DBError là lỗi gom của các DB không xoá được.
type purgeExpiredJSON struct {
	Sites   any    `json:"sites"`
	DBs     any    `json:"dbs"`
	DBError string `json:"db_error,omitempty"`
}

// trashPurgeExpired: xem trước từng site và DB rời quá hạn, hỏi lại một lần
// cho cả lô, rồi gọi Sites.PurgeExpired và DBs.PurgeExpired (mỗi thứ một job,
// thứ lỗi không chặn thứ sau), đúng việc bộ hẹn giờ của tpan serve làm.
func trashPurgeExpired(cmd *cobra.Command, e *env, f *planRun) error {
	ctx := cmd.Context()
	if !f.dryRun {
		if err := requireRoot(e, cmd); err != nil {
			return fail(e, f.asJSON, err)
		}
	}
	m, done, err := openM13(ctx, e, f.dryRun)
	if err != nil {
		return err
	}
	defer done()
	s := m.sites

	now := time.Now()
	exp, err := s.Expired(ctx, now)
	if err != nil {
		return err
	}
	trashed, err := trashedDBs(ctx, m)
	if err != nil {
		return err
	}
	var expDBs []account.Database
	for _, d := range trashed {
		if !d.PurgeAt.IsZero() && !d.PurgeAt.After(now) {
			expDBs = append(expDBs, d)
		}
	}
	if len(exp) == 0 && len(expDBs) == 0 {
		if f.asJSON {
			return writeJSON(e.stdout, purgeExpiredJSON{Sites: []core.PurgeResult{}, DBs: []string{}})
		}
		printf(e.stdout, "%s\n", e.tr("Không có site hay DB rời nào quá hạn giữ.", "No site or standalone database is past its retention."))
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
	dbRows := make([]purgeDBPreview, 0, len(expDBs))
	dbPlans := map[string]*core.Planned{}
	for _, d := range expDBs {
		row := purgeDBPreview{Name: d.Name}
		p, err := m.dbs.PreparePurge(ctx, d.Name)
		var re *core.RefusedError
		switch {
		case errors.As(err, &re):
			row.Error = re
		case err != nil:
			return err
		default:
			dbPlans[d.Name] = p
			all.Steps = append(all.Steps, p.Plan.Steps...)
		}
		dbRows = append(dbRows, row)
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
		for i := range dbRows {
			p := dbPlans[dbRows[i].Name]
			if p == nil {
				continue
			}
			pv, err := p.Preview(ctx)
			if err != nil {
				return nil, nil, err
			}
			dbRows[i].Preview = &pv
		}
		return purgeExpiredJSON{Sites: rows, DBs: dbRows}, func() {
			for _, r := range rows {
				printf(e.stdout, "== %s\n", r.Domain)
				if r.Error != nil {
					printRefusal(e, r.Error)
					continue
				}
				printPreview(e, *r.Preview)
			}
			for _, r := range dbRows {
				printf(e.stdout, "== %s\n", r.Name)
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

	pr := f.progress(e, all)
	res := []core.PurgeResult{}
	if len(exp) > 0 {
		if res, err = s.PurgeExpired(ctx, now, pr.onEvent); err != nil {
			return err
		}
	}
	purgedDBs := []string{}
	var dbErr error
	if len(expDBs) > 0 {
		purgedDBs, dbErr = m.dbs.PurgeExpired(ctx, now, pr.onEvent)
	}
	failed := dbErr != nil
	for _, n := range purgedDBs {
		// DBs.PurgeExpired không trả job: ghi nhật ký không kèm job (job vẫn ở lịch sử).
		auditNoJob(ctx, e, m.st, "dbs.purge", n)
	}
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
		out := purgeExpiredJSON{Sites: res, DBs: purgedDBs}
		if dbErr != nil {
			out.DBError = dbErr.Error()
		}
		if err := writeJSON(e.stdout, out); err != nil {
			return err
		}
	} else {
		for _, n := range purgedDBs {
			printf(e.stdout, "\n== %s\n%s\n", n, e.tr("Đã xoá hẳn DB rời.", "Standalone database purged."))
		}
		if dbErr != nil {
			printf(e.stdout, "%s %v\n", e.tr("Lỗi:", "Error:"), dbErr)
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
