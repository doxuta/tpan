package main

// `tpan isolation probe` (M17, mục 3.9 của docs/thay-doi/M17-thiet-ke.md): thử
// tấn công thật. Tạo hai site WordPress thử (tên dành riêng, không vào bảng
// sites), chạy các đường tấn công từ site này sang site kia ở ngoài lồng rồi
// trong lồng, rồi dọn. Thoát mã 1 khi có ô "lộ" (đường lẽ ra phải bị chặn mà
// lộ), để chạy được trong script và CI.

import (
	"context"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/server"
	"github.com/doxuta/tpan/internal/store"
)

func init() { register(isolationCmd) }

func isolationCmd(e *env) *cobra.Command {
	c := &cobra.Command{
		Use:   "isolation",
		Short: "Kiểm tra cô lập giữa các site bằng thử tấn công thật",
	}
	c.AddCommand(isolationProbeCmd(e))
	return c
}

// openProbe dựng core.Probe trên cùng DB với ca sử dụng site (openSites). Sổ
// phép thử nằm trong tpan.db nên chưa có DB là chưa chạy được (cũng là chưa có
// stack): từ chối setup_required, không tạo DB, kể cả với --dry-run.
func openProbe(ctx context.Context, e *env, dryRun bool) (*core.Probe, any, func(), error) {
	s, done, err := openSites(ctx, e, dryRun)
	if err != nil {
		return nil, nil, nil, err
	}
	st, ok := s.Store.(*store.Store)
	if !ok {
		done()
		return nil, nil, nil, &core.RefusedError{Code: "setup_required", Message: i18n.T(
			"máy chưa có tpan.db và stack nên chưa thử tấn công được; chạy sudo tpan setup rồi sudo tpan stack install trước",
			"this machine has no tpan.db and no stack yet, so there is nothing to probe; run sudo tpan setup, then sudo tpan stack install first")}
	}
	return &core.Probe{Sys: s.Sys, Settings: s.Settings, Store: s.Store, Runs: st, Journal: s.Journal}, s.Store, done, nil
}

func isolationProbeCmd(e *env) *cobra.Command {
	var (
		f       planFlags
		only    string
		cleanup bool
	)
	c := &cobra.Command{
		Use:   "probe [--only ngoai|trong] [--cleanup] [--dry-run] [--yes] [--json]",
		Short: "Thử tấn công giữa hai site thử, ngoài lồng và trong lồng; --cleanup dọn lượt còn sót",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			switch only {
			case "", "ngoai", "trong":
			default:
				return fail(e, f.asJSON, &core.RefusedError{Code: "args_invalid", Message: i18n.T(
					"--only chỉ nhận ngoai hoặc trong (nhận được "+quote(only)+")",
					"--only accepts ngoai or trong (got "+quote(only)+")")})
			}
			if cleanup && only != "" {
				return fail(e, f.asJSON, &core.RefusedError{Code: "args_invalid", Message: i18n.T(
					"--cleanup dọn lượt sót, không đi cùng --only", "--cleanup removes leftovers and does not take --only")})
			}
			if !f.dryRun {
				if err := requireRoot(e, cmd); err != nil {
					return fail(e, f.asJSON, err)
				}
			}
			p, journal, done, err := openProbe(ctx, e, f.dryRun)
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			defer done()
			if cleanup {
				return runProbeCleanup(ctx, e, f, journal, p)
			}
			return runProbe(ctx, e, f, journal, p, only)
		},
	}
	fl := c.Flags()
	fl.StringVar(&only, "only", "", "chỉ một lượt: ngoai (ngoài lồng) hoặc trong (trong lồng); bỏ trống là cả hai")
	fl.BoolVar(&cleanup, "cleanup", false, "dọn các lượt thử còn sót (panel bị tắt giữa chừng) rồi thôi")
	bindChangeFlags(c, &f)
	return c
}

func runProbeCleanup(ctx context.Context, e *env, f planFlags, journal any, p *core.Probe) error {
	plan, err := p.PrepareCleanup(ctx)
	if err != nil {
		return fail(e, f.asJSON, asRefusal(err))
	}
	if plan == nil {
		if f.asJSON {
			return writeJSON(e.stdout, map[string]any{"cleaned": false, "message": e.tr("Không có lượt thử nào chưa dọn.", "No probe run is left to clean up.")})
		}
		printf(e.stdout, "%s\n", e.tr("Không có lượt thử nào chưa dọn.", "No probe run is left to clean up."))
		return nil
	}
	_, err = runPlanned(ctx, e, f, journal, "isolation.probe.cleanup", plan.Plan.Target, plan)
	return err
}

func runProbe(ctx context.Context, e *env, f planFlags, journal any, p *core.Probe, only string) error {
	job, err := p.Prepare(ctx, core.ProbeOptions{Only: only})
	if err != nil {
		return fail(e, f.asJSON, asRefusal(err))
	}
	if f.dryRun || !f.yes {
		pv, err := job.Preview(ctx)
		if err != nil {
			return err
		}
		if f.dryRun {
			if f.asJSON {
				return writeJSON(e.stdout, server.ProbePreview{Preview: pv, Run: job.Run})
			}
			printPreview(e, pv)
			printf(e.stdout, "%s\n", e.tr("Hai site thử: "+job.Run.DomainA+", "+job.Run.DomainB+".", "The two probe sites: "+job.Run.DomainA+", "+job.Run.DomainB+"."))
			return nil
		}
		if f.asJSON || !e.isTerminal() {
			return fail(e, f.asJSON, &core.RefusedError{Code: "confirm_required", Message: i18n.T(
				"không hỏi xác nhận được (không có terminal hoặc đang dùng --json); xem trước bằng --dry-run rồi chạy lại với --yes",
				"cannot ask for confirmation (no terminal, or --json is set); preview with --dry-run, then run again with --yes")})
		}
		printPreview(e, pv)
		if !confirm(e) {
			printf(e.stdout, "%s\n", e.tr("Đã huỷ, không thay đổi gì.", "Cancelled, nothing changed."))
			return &exitCode{1}
		}
	}

	out := e.stdout
	if f.asJSON {
		out = e.stderr
	}
	pr := newProgress(out, e.lang, f.verbose, job.Plan)
	report, rep, err := job.Execute(ctx, pr.onEvent)
	cliAudit(ctx, e, journal, "isolation.probe", job.Run.ID, rep.Job)
	if err != nil && rep.Job.ID == "" {
		return err
	}
	if f.asJSON {
		if werr := writeJSON(e.stdout, report); werr != nil {
			return werr
		}
	} else {
		printProbeReport(e, report)
		printReport(e, rep)
	}
	if err != nil {
		return err
	}
	// Có ô lộ hay job không thành công đều là không đạt.
	if report.Exposed > 0 || rep.Job.Status != engine.JobSucceeded {
		return &exitCode{1}
	}
	return nil
}

// probeCellText là chữ của một ô.
func probeCellText(e *env, status string) string {
	switch status {
	case core.ProbeBlocked:
		return e.tr("chặn", "blocked")
	case core.ProbeExposed:
		return e.tr("LỘ", "EXPOSED")
	case core.ProbeNeedsCage:
		return e.tr("cần lồng", "needs cage")
	case core.ProbeSkipped:
		return e.tr("bỏ qua", "skipped")
	case core.ProbeNA:
		return "-"
	}
	return status
}

func printProbeReport(e *env, r core.ProbeReport) {
	w := e.stdout
	printf(w, "\n%s %s (%s, %s, %s)\n", e.tr("Lượt thử", "Probe run"), r.ID, r.Web, r.DomainA, r.DomainB)
	printf(w, "%-5s %-11s %-11s %s\n", e.tr("ĐƯỜNG", "PATH"), e.tr("NGOÀI LỒNG", "OUTSIDE"), e.tr("TRONG LỒNG", "INSIDE"), e.tr("MÔ TẢ", "WHAT"))
	for _, x := range r.Results {
		printf(w, "%-5s %-11s %-11s %s\n", x.Path, probeCellText(e, x.Outside.Status), probeCellText(e, x.Inside.Status), x.Title.In(e.lang))
	}
	// Chi tiết chỉ cho ô cần người đọc: lộ, và ô bỏ qua (kèm lý do).
	for _, x := range r.Results {
		for _, c := range []struct {
			where string
			cell  core.ProbeCell
		}{{e.tr("ngoài lồng", "outside"), x.Outside}, {e.tr("trong lồng", "inside"), x.Inside}} {
			if c.cell.Status == core.ProbeExposed || c.cell.Status == core.ProbeSkipped {
				printf(w, "  %s (%s) %s: %s\n", x.Path, c.where, probeCellText(e, c.cell.Status), c.cell.Detail.In(e.lang))
			}
		}
	}
	if r.Exposed > 0 {
		printf(w, "\n%s\n", e.tr("Có "+strconv.Itoa(r.Exposed)+" ô LỘ: đường lẽ ra phải bị chặn mà không bị.", strconv.Itoa(r.Exposed)+" cell(s) EXPOSED: paths that should be blocked are not."))
	} else {
		printf(w, "\n%s\n", e.tr("Không có ô nào lộ.", "No cell is exposed."))
	}
}
