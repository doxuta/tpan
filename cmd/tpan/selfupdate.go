package main

// Lệnh `tpan self-update` (mục 6.11 và 9 của docs/KE-HOACH.md): tải bản phát
// hành mới, kiểm sha256, thay file chạy, khởi động lại dịch vụ, giữ bản cũ ở
// /usr/local/bin/tpan.old. --rollback đổi ngược về bản đó. Đây là lệnh người
// vận hành gõ. Nút trong panel (M15, mục 6.14) chạy cùng Plan này qua lệnh nội
// bộ ở internal_selfupdate.go, trong một đơn vị systemd tạm: job chạy ngay
// trong panel sẽ bị dừng cùng dịch vụ ở bước khởi động lại.

import (
	"context"
	"errors"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/doxuta/tpan/internal/buildinfo"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/system"
)

func init() { register(selfUpdateCmd) }

func selfUpdateCmd(e *env) *cobra.Command {
	var (
		f        planFlags
		url      string
		rollback bool
	)
	c := &cobra.Command{
		Use:   "self-update [--url U] [--rollback]",
		Short: "Cập nhật chính tpan lên bản phát hành mới (--rollback: quay về bản trước)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if rollback && url != "" {
				return errors.New("--rollback không đi cùng --url: quay về dùng bản đã có ở " + core.TpanBin + ".old")
			}
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, f.asJSON, err)
			}
			// --dry-run không tạo DB (giống stack install).
			s := &core.SelfUpdate{Sys: e.sys, Settings: core.EmptySettings{}, Arch: runtime.GOARCH, Version: buildinfo.Version}
			if !f.dryRun || e.fileExists(e.dbPath) {
				st, err := openStore(ctx, e)
				if err != nil {
					return err
				}
				defer closeStore(st)
				s.Settings = st
				if !f.dryRun {
					s.Journal = st
				}
			}

			action, prepare := "self.update", func() (*core.Planned, error) { return s.Prepare(ctx, url) }
			if rollback {
				action, prepare = "self.rollback", func() (*core.Planned, error) { return s.PrepareRollback(ctx) }
			}
			p, err := prepare()
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			rep, err := runPlanned(ctx, e, f, s.Journal, action, p.Plan.Target, p)
			if err != nil || rep.Job.ID == "" {
				return err
			}
			printSelfVersions(ctx, e, f.asJSON, rollback, buildinfo.Version)
			return nil
		},
	}
	fl := c.Flags()
	fl.StringVar(&url, "url", "", "gốc tải bản phát hành (có tpan-linux-<arch> và SHA256SUMS); mặc định: setting update.self_url, không có thì "+core.DefaultSelfURL)
	fl.BoolVar(&rollback, "rollback", false, "quay về bản trước ("+core.TpanBin+".old)")
	fl.BoolVar(&f.dryRun, "dry-run", false, "chỉ in kế hoạch, không thay đổi gì")
	fl.BoolVar(&f.yes, "yes", false, "không hỏi xác nhận")
	fl.BoolVar(&f.asJSON, "json", false, "in kết quả dạng JSON")
	return c
}

// printSelfVersions in bản cũ và bản đang nằm ở TpanBin sau job, đọc bằng
// chính `tpan version` của file đó. --json thì in ra stderr: stdout đã là
// JSON của Report.
func printSelfVersions(ctx context.Context, e *env, asJSON, rollback bool, old string) {
	w := e.stdout
	if asJSON {
		w = e.stderr
	}
	res, err := e.sys.Run(ctx, system.Cmd{Name: core.TpanBin, Args: []string{"version"}})
	now := "?"
	if f := strings.Fields(res.Stdout); err == nil && len(f) >= 2 && f[0] == "tpan" {
		now = f[1]
	}
	if rollback {
		printf(w, "%s %s -> %s\n", e.tr("tpan đã quay về bản trước:", "tpan rolled back:"), old, now)
		return
	}
	if now == old {
		printf(w, "%s %s\n", e.tr("Đang là bản mới nhất, không thay gì:", "Already the latest release, nothing replaced:"), old)
		return
	}
	printf(w, "%s %s -> %s\n", e.tr("tpan đã đổi:", "tpan changed:"), old, now)
	// M14 (mục 6.13): bản mới siết cô lập giữa các site, nhưng vhost, quyền
	// log và cấu hình PHP của site đã có vẫn là của bản cũ cho tới khi Plan
	// "Áp bản vá cô lập" chạy. Nhắc ở đây vì đây là lúc duy nhất người vận
	// hành chắc chắn đang nhìn terminal; chạy lại khi đã áp thì không đổi gì.
	printf(w, "%s\n", e.tr(
		"Việc tiếp theo: chạy `sudo tpan security harden`. Bản mới siết cô lập giữa các site; site có từ trước cần áp bản vá (xem trước bằng --dry-run).",
		"Next: run `sudo tpan security harden`. The new release tightens isolation between sites; existing sites need the hardening applied (preview with --dry-run)."))
}
