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
	return runPlannedOut(ctx, e, f, journal, action, target, p, nil)
}

// runPlannedOut là runPlanned với jsonOf (nil là in Report) dựng thứ in ra khi
// --json sau khi chạy: lệnh có mật khẩu hiện một lần (db add, db passwd) gói
// Report cùng mật khẩu ở một trường riêng, như admin_password của site add.
func runPlannedOut(ctx context.Context, e *env, f planFlags, journal any, action, target string, p *core.Planned, jsonOf func(engine.Report) any) (engine.Report, error) {
	if ok, err := confirmPlanned(ctx, e, f, p); !ok {
		return engine.Report{}, err
	}
	rep, err := p.Execute(ctx, planProgress(e, f, p.Plan).onEvent)
	return finishPlanned(ctx, e, f, journal, action, target, rep, err, jsonOf)
}

// confirmPlanned là nửa đầu của runPlannedOut: in bản xem trước (--dry-run,
// xong là dừng) hoặc hỏi lại khi không có --yes. ok=true: được chạy tiếp.
// Tách riêng để `backup pull` (chạy qua Remote.Pull, không qua Execute) đi
// đúng nhịp xem trước, hỏi lại như mọi lệnh job khác.
func confirmPlanned(ctx context.Context, e *env, f planFlags, p *core.Planned) (ok bool, err error) {
	if !f.dryRun && f.yes {
		return true, nil
	}
	pv, err := p.Preview(ctx)
	if err != nil {
		return false, err
	}
	if f.dryRun {
		if f.asJSON {
			return false, writeJSON(e.stdout, pv)
		}
		printPreview(e, pv)
		return false, nil
	}
	if f.asJSON || !e.isTerminal() {
		return false, fail(e, f.asJSON, &core.RefusedError{Code: "confirm_required", Message: i18n.T(
			"không hỏi xác nhận được (không có terminal hoặc đang dùng --json); xem trước bằng --dry-run rồi chạy lại với --yes",
			"cannot ask for confirmation (no terminal, or --json is set); preview with --dry-run, then run again with --yes")})
	}
	printPreview(e, pv)
	if !confirm(e) {
		printf(e.stdout, "%s\n", e.tr("Đã huỷ, không thay đổi gì.", "Cancelled, nothing changed."))
		return false, &exitCode{1}
	}
	return true, nil
}

// planProgress: --json thì stdout chỉ có JSON của Report; tiến độ ra stderr
// cho người xem.
func planProgress(e *env, f planFlags, p engine.Plan) *progress {
	out := e.stdout
	if f.asJSON {
		out = e.stderr
	}
	return newProgress(out, e.lang, f.verbose, p)
}

// finishPlanned là nửa sau của runPlannedOut: ghi audit, in Report (hoặc
// jsonOf), đổi job hỏng thành mã thoát 1.
func finishPlanned(ctx context.Context, e *env, f planFlags, journal any, action, target string, rep engine.Report, err error, jsonOf func(engine.Report) any) (engine.Report, error) {
	cliAudit(ctx, e, journal, action, target, rep.Job)
	if err != nil && rep.Job.ID == "" {
		return rep, err
	}
	if f.asJSON {
		var v any = rep
		if jsonOf != nil {
			v = jsonOf(rep)
		}
		if werr := writeJSON(e.stdout, v); werr != nil {
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
