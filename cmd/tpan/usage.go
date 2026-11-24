package main

// `tpan usage [<domain>] [--suggest] [--json]` (M17, mục 3.8 và 3.10 của
// docs/thay-doi/M17-thiet-ke.md): số liệu tài nguyên của site trong lồng, đọc
// ngay từ cgroup lúc gọi (bất biến 6), và gợi ý trần từ mẫu 7 ngày. Hai cờ ẩn
// --sample và --rollup chạy ngay việc của bộ hẹn giờ trong `tpan serve`, cho
// e2e (không cần chờ 5 phút).

import (
	"context"
	"fmt"
	"slices"

	"github.com/spf13/cobra"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/validate"
)

func init() { register(usageCmd) }

// openUsages dựng core.Usages trên cùng DB với ca sử dụng site (openSites):
// chưa có file DB (dryRun) thì không tạo, đọc như bảng rỗng, không có bảng mẫu.
func openUsages(ctx context.Context, e *env, dryRun bool) (*core.Usages, func(), error) {
	s, done, err := openSites(ctx, e, dryRun)
	if err != nil {
		return nil, nil, err
	}
	return &core.Usages{Sys: s.Sys, Store: s.Store, Accounts: s.Accounts, Samples: s.Samples}, done, nil
}

func usageCmd(e *env) *cobra.Command {
	var (
		asJSON, suggest, sample, rollup bool
	)
	c := &cobra.Command{
		Use:   "usage [<domain>] [--suggest] [--json]",
		Short: "Số liệu RAM, CPU, tiến trình của site trong lồng; --suggest gợi ý trần từ 7 ngày gần nhất",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			var d validate.Domain
			if len(args) == 1 {
				var err error
				if d, err = parseDomainArg(args[0]); err != nil {
					return fail(e, asJSON, err)
				}
			}
			if (sample || rollup) && (suggest || !d.IsZero()) {
				return fail(e, asJSON, &core.RefusedError{Code: "args_invalid", Message: i18n.T(
					"--sample và --rollup chạy riêng, không kèm <domain> hay --suggest",
					"--sample and --rollup run on their own, without <domain> or --suggest")})
			}
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, asJSON, err)
			}
			// Không tạo DB chỉ để đọc.
			u, done, err := openUsages(ctx, e, !e.fileExists(e.dbPath))
			if err != nil {
				return err
			}
			defer done()
			switch {
			case sample || rollup:
				return usageRunTimerJobs(ctx, e, u, asJSON, sample, rollup)
			case suggest:
				return usageSuggest(ctx, e, u, d, asJSON)
			case d.IsZero():
				list, err := u.List(ctx, "")
				if err != nil {
					return fail(e, asJSON, asRefusal(err))
				}
				if asJSON {
					return writeJSON(e.stdout, list)
				}
				printUsageTable(e, list)
				return nil
			}
			one, err := u.Site(ctx, d)
			if err != nil {
				return fail(e, asJSON, asRefusal(err))
			}
			if asJSON {
				return writeJSON(e.stdout, one)
			}
			printUsageTable(e, []core.SiteUsage{one})
			return nil
		},
	}
	fl := c.Flags()
	fl.BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
	fl.BoolVar(&suggest, "suggest", false, "gợi ý trần RAM, CPU, tiến trình từ mẫu 7 ngày gần nhất")
	fl.BoolVar(&sample, "sample", false, "ghi ngay một mẫu số liệu (việc của bộ hẹn giờ trong tpan serve)")
	fl.BoolVar(&rollup, "rollup", false, "gộp ngay mẫu theo giờ (việc của bộ hẹn giờ trong tpan serve)")
	_ = fl.MarkHidden("sample")
	_ = fl.MarkHidden("rollup")
	return c
}

// usageRunTimerJobs chạy một lần việc của bộ hẹn giờ: lấy mẫu rồi gộp.
func usageRunTimerJobs(ctx context.Context, e *env, u *core.Usages, asJSON, sample, rollup bool) error {
	res := map[string]any{}
	if sample {
		n, err := u.Sample(ctx)
		res["sampled"] = n
		if err != nil {
			return fmt.Errorf("lấy mẫu (đã ghi %d): %w", n, err)
		}
	}
	if rollup {
		if err := u.Rollup(ctx); err != nil {
			return fmt.Errorf("gộp theo giờ: %w", err)
		}
		res["rolled_up"] = true
	}
	if asJSON {
		return writeJSON(e.stdout, res)
	}
	if n, ok := res["sampled"]; ok {
		printf(e.stdout, "%s\n", e.tr(fmt.Sprintf("Đã ghi %v mẫu.", n), fmt.Sprintf("Wrote %v samples.", n)))
	}
	if rollup {
		printf(e.stdout, "%s\n", e.tr("Đã gộp mẫu theo giờ.", "Rolled the samples up by hour."))
	}
	return nil
}

// usageSuggest in gợi ý trần cho mọi site trong lồng, hoặc cho một site (kèm
// các gói chứa site đó). Site ngoài lồng bị từ chối như `tpan usage <domain>`.
func usageSuggest(ctx context.Context, e *env, u *core.Usages, d validate.Domain, asJSON bool) error {
	if !d.IsZero() {
		if _, err := u.Site(ctx, d); err != nil { // site_not_found, usage_not_caged
			return fail(e, asJSON, asRefusal(err))
		}
	}
	rep, err := u.Suggestions(ctx)
	if err != nil {
		return fail(e, asJSON, asRefusal(err))
	}
	if !d.IsZero() {
		rep.Sites = slices.DeleteFunc(rep.Sites, func(s core.Suggestion) bool { return s.Domain != d.String() })
		rep.Packages = slices.DeleteFunc(rep.Packages, func(p core.PackageSuggestion) bool { return !slices.Contains(p.Sites, d.String()) })
	}
	if asJSON {
		return writeJSON(e.stdout, rep)
	}
	w := e.stdout
	if len(rep.Sites) == 0 {
		printf(w, "%s\n", e.tr("Chưa có site nào trong lồng để gợi ý.", "No caged site to suggest for."))
		return nil
	}
	for _, s := range rep.Sites {
		note := ""
		if s.LowData {
			note = e.tr(" (ít dữ liệu: dưới 24 giờ mẫu)", " (little data: under 24 hours of samples)")
		}
		printf(w, "%s (%s)%s\n", s.Domain, s.Package, note)
		printf(w, "  RAM %s -> %s MiB   CPU %s -> %s%%   %s %s -> %s\n",
			maxText(s.Current.RAMMB), maxText(s.Suggested.RAMMB), maxText(s.Current.CPUPercent), maxText(s.Suggested.CPUPercent),
			e.tr("tiến trình", "tasks"), maxText(s.Current.Tasks), maxText(s.Suggested.Tasks))
		for _, r := range s.Reasons {
			printf(w, "  - %s\n", r.In(e.lang))
		}
	}
	for _, p := range rep.Packages {
		printf(w, "%s %s: RAM %s MiB, CPU %s%%, %s %s\n", e.tr("Gói", "Package"), p.Package,
			maxText(p.Suggested.RAMMB), maxText(p.Suggested.CPUPercent), e.tr("tiến trình", "tasks"), maxText(p.Suggested.Tasks))
	}
	printf(w, "%s\n", e.tr("Chỉ là gợi ý. Áp bằng: sudo tpan package edit <gói> --ram-mb N --cpu N --tasks N (có xem trước).",
		"These are only suggestions. Apply with: sudo tpan package edit <package> --ram-mb N --cpu N --tasks N (with a preview)."))
	return nil
}

func printUsageTable(e *env, list []core.SiteUsage) {
	w := e.stdout
	if len(list) == 0 {
		printf(w, "%s\n", e.tr("Chưa có site nào trong lồng.", "No site is in a cage yet."))
		return
	}
	printf(w, "%-32s %-14s %-22s %-14s %s\n", "SITE", e.tr("CHỦ", "OWNER"), "RAM MiB", e.tr("TIẾN TRÌNH", "TASKS"), e.tr("CHẠM TRẦN", "LIMIT HITS"))
	for _, s := range list {
		u := s.Usage
		if !u.Running {
			printf(w, "%-32s %-14s %s\n", s.Domain, s.Owner, e.tr("lồng chưa chạy", "cage not running"))
			continue
		}
		printf(w, "%-32s %-14s %-22s %-14s OOM %d, %s %d\n", s.Domain, s.Owner,
			fmt.Sprintf("%d/%s (%s %d)", u.MemCurrent>>20, maxText(s.Limits.RAMMB), e.tr("đỉnh", "peak"), u.MemPeak>>20),
			fmt.Sprintf("%d/%s", u.Tasks, maxText(s.Limits.Tasks)),
			u.OOMKills, e.tr("tiến trình", "tasks"), u.TasksHits)
	}
}
