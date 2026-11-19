package main

// Nhịp chung của các lệnh M12 chạy một core.Planned (php, update, mariadb,
// self-update): xem trước, hỏi lại, chạy có tiến độ, in báo cáo, ghi audit.
// Giống hệt đường đi của `tpan stack install`, gom một chỗ để các lệnh mới
// không chép lại.

import (
	"context"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
)

// planFlags là bốn cờ chung của lệnh có thay đổi (mục 9).
type planFlags struct {
	dryRun, yes, asJSON, verbose bool
}

// runPlanned xem trước (--dry-run), hoặc hỏi lại (không --yes) rồi chạy p.
// action, target ghi vào audit. journal là store đang mở (ghi audit). Trả
// exitCode 1 khi người dùng huỷ hoặc job không thành công.
func runPlanned(ctx context.Context, e *env, f planFlags, journal any, action, target string, p *core.Planned) (engine.Report, error) {
	if f.dryRun || !f.yes {
		pv, err := p.Preview(ctx)
		if err != nil {
			return engine.Report{}, err
		}
		if f.dryRun {
			if f.asJSON {
				return engine.Report{}, writeJSON(e.stdout, pv)
			}
			printPreview(e, pv)
			return engine.Report{}, nil
		}
		if f.asJSON || !e.isTerminal() {
			return engine.Report{}, fail(e, f.asJSON, &core.RefusedError{Code: "confirm_required", Message: i18n.T(
				"không hỏi xác nhận được (không có terminal hoặc đang dùng --json); xem trước bằng --dry-run rồi chạy lại với --yes",
				"cannot ask for confirmation (no terminal, or --json is set); preview with --dry-run, then run again with --yes")})
		}
		printPreview(e, pv)
		if !confirm(e) {
			printf(e.stdout, "%s\n", e.tr("Đã huỷ, không thay đổi gì.", "Cancelled, nothing changed."))
			return engine.Report{}, &exitCode{1}
		}
	}

	// --json: stdout chỉ có JSON của Report; tiến độ ra stderr cho người xem.
	out := e.stdout
	if f.asJSON {
		out = e.stderr
	}
	pr := newProgress(out, e.lang, f.verbose, p.Plan)
	rep, err := p.Execute(ctx, pr.onEvent)
	cliAudit(ctx, e, journal, action, target, rep.Job)
	if err != nil && rep.Job.ID == "" {
		return rep, err
	}
	if f.asJSON {
		if werr := writeJSON(e.stdout, rep); werr != nil {
			return rep, werr
		}
	} else {
		printReport(e, rep)
	}
	if err != nil {
		return rep, err
	}
	if rep.Job.Status != engine.JobSucceeded {
		return rep, &exitCode{1}
	}
	return rep, nil
}
