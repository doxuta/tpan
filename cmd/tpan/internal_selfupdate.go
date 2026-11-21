package main

// Lệnh ẩn `tpan internal self-update [--rollback]` (M15, mục 6.14 của
// docs/KE-HOACH.md). Người dùng không gõ lệnh này: khi admin bấm "Cập nhật"
// hoặc "Quay về bản trước" trong panel, `tpan serve` nhờ systemd khởi một đơn
// vị tạm (tpan-self-update) và đơn vị đó chạy lệnh này. Nằm ở cgroup riêng nên
// nó sống qua bước `systemctl restart tpan`, điều mà một job chạy trong panel
// không làm được.
//
// Nó chạy đúng Plan của `tpan self-update`, và làm thêm hai việc mà lệnh kia
// không làm: ghi kết quả vào settings (update.self_last) để màn đọc lại sau
// khi panel lên bản mới, và ghi dòng audit mang tên người bấm, IP của họ và
// "<bản cũ> -> <bản mới>". Người bấm và IP đọc từ settings (core.SelfUpdate.
// Start đã ghi), không đi qua dòng lệnh: lệnh chỉ nhận đúng một cờ --rollback.
//
// stdout và stderr của đơn vị tạm đi vào journal: `journalctl -u
// tpan-self-update` cho xem lại tiến độ từng bước của lượt chạy.

import (
	"context"
	"errors"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"github.com/doxuta/tpan/internal/buildinfo"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/store"
)

func internalSelfUpdateCmd(e *env) *cobra.Command {
	var rollback bool
	c := &cobra.Command{
		Use:   "self-update [--rollback]",
		Short: "Chạy một lượt cập nhật tpan do panel khởi (đơn vị systemd tạm " + core.SelfUnit + " gọi)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, false, err)
			}
			st, err := openStore(ctx, e)
			if err != nil {
				return err
			}
			defer closeStore(st)
			s := &core.SelfUpdate{Sys: e.sys, Settings: st, Journal: st, Arch: runtime.GOARCH, Version: buildinfo.Version,
				// M17: quay về từ nút của panel cũng từ chối khi còn lồng hay lượt thử chưa dọn.
				Sites: st, Runs: st}

			action, auditAction := core.SelfActionUpdate, "self.update"
			prepare := func() (*core.Planned, error) { return s.Prepare(ctx, "") }
			if rollback {
				action, auditAction = core.SelfActionRollback, "self.rollback"
				prepare = func() (*core.Planned, error) { return s.PrepareRollback(ctx) }
			}
			// Run tự dựng Plan của nó. Dựng thêm một lần ở đây (chỉ đọc máy) chỉ
			// để lấy tên từng bước cho dòng tiến độ; bị từ chối thì Run sẽ gặp
			// đúng lời từ chối đó và ghi lại, ở đây không cần làm gì.
			var titles engine.Plan
			if p, err := prepare(); err == nil {
				titles = p.Plan
			}
			pr := newProgress(e.stdout, e.lang, false, titles)

			run, rep, err := s.Run(ctx, action, time.Now, pr.onEvent)
			if run.Status == "" {
				// Chưa bắt đầu được (không đọc nổi settings): không có gì để ghi.
				return err
			}

			to := run.To
			if to == "" {
				to = "?"
			}
			var refused *core.RefusedError
			result := run.Status // "ok" | "same"
			switch {
			case run.Status != core.SelfFailed:
			case rep.Job.ID != "":
				result = string(rep.Job.Status) // "rolled_back", "undo_failed"
			case errors.As(err, &refused):
				result = refused.Code
			}
			// Ghi audit trước khi in, và bằng ctx không huỷ được: đơn vị tạm bị
			// dừng giữa chừng thì dòng nhật ký vẫn phải có (ở M15 đợt 0, dòng này
			// từng mất với lỗi "context canceled").
			entry := store.AuditEntry{Time: time.Now(), IP: run.IP, Actor: run.Actor, Action: auditAction, Target: run.From + " -> " + to, Result: result, JobID: run.JobID}
			if aerr := st.AddAudit(context.WithoutCancel(ctx), entry); aerr != nil {
				printf(e.stderr, "tpan: %v\n", aerr)
			}

			if rep.Job.ID != "" {
				printReport(e, rep)
			}
			switch {
			case refused != nil:
				printRefusal(e, refused)
				return &exitCode{1}
			case err != nil:
				// Job đã chạy nhưng kết quả không ghi được vào settings.
				return err
			case run.Status == core.SelfFailed:
				if rep.Job.ID == "" {
					printf(e.stderr, "tpan: %s\n", run.Error)
				}
				return &exitCode{1}
			case rollback:
				printf(e.stdout, "%s %s -> %s\n", e.tr("tpan đã quay về bản trước:", "tpan rolled back:"), run.From, to)
			case run.Status == core.SelfSame:
				printf(e.stdout, "%s %s\n", e.tr("Đang là bản mới nhất, không thay gì:", "Already the latest release, nothing replaced:"), run.From)
			default:
				printf(e.stdout, "%s %s -> %s\n", e.tr("tpan đã đổi:", "tpan changed:"), run.From, to)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&rollback, "rollback", false, "quay về bản trước ("+core.TpanBin+".old)")
	return c
}
