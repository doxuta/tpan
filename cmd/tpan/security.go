package main

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
)

func init() { register(securityCmd) }

func securityCmd(e *env) *cobra.Command {
	c := &cobra.Command{
		Use:   "security",
		Short: "Xem bảo mật của máy; bật tường lửa ufw và fail2ban",
	}
	c.AddCommand(
		securityStatusCmd(e),
		securityPlanCmd(e, "ufw", "Bật tường lửa ufw: mở cổng SSH, 80, 443 (và cổng panel nếu đang mở) trước rồi mới bật",
			"security.ufw", (*core.Security).PrepareUFW),
		securityPlanCmd(e, "fail2ban", "Cài và bật fail2ban cho SSH (chặn IP dò mật khẩu)",
			"security.fail2ban", (*core.Security).PrepareFail2ban),
	)
	return c
}

func securityStatusCmd(e *env) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "status",
		Short: "Xem các dòng của trang bảo mật: ufw, fail2ban, SSH, swap, cập nhật, cổng panel",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// sshd -T và ufw status cần root; thiếu thì mọi dòng ra "chưa rõ".
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, asJSON, err)
			}
			st, err := (&core.Security{Sys: e.sys}).Status(cmd.Context())
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(e.stdout, st)
			}
			for _, r := range st.Results {
				printResult(e.stdout, e.lang, r, "")
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
	return c
}

// securityPlanCmd dựng `security ufw` và `security fail2ban`: hai lệnh chỉ
// khác nhau ở Plan.
func securityPlanCmd(e *env, use, short, action string, prepare func(*core.Security, context.Context) (*core.SecurityPlan, error)) *cobra.Command {
	var f planRun
	c := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			// Cần root cả khi --dry-run (như stack install): dựng Plan ufw phải
			// đọc `sshd -T`, thiếu root thì sẽ báo sai là không biết cổng SSH.
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, f.asJSON, err)
			}
			sec := &core.Security{Sys: e.sys}
			// --dry-run không mở DB: không tạo file nào, và không cần Journal.
			var audit any
			if !f.dryRun {
				st, err := openStore(ctx, e)
				if err != nil {
					return err
				}
				defer closeStore(st)
				sec.Journal, audit = st, st
			}
			p, err := prepare(sec, ctx)
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			if ok, err := f.previewOrConfirm(e, func() (engine.Preview, error) { return p.Preview(ctx) }); !ok {
				return err
			}
			rep, err := p.Execute(ctx, f.progress(e, p.Plan).onEvent)
			cliAudit(ctx, e, audit, action, p.Plan.Target, rep.Job)
			return finishReport(e, f.asJSON, rep, err)
		},
	}
	f.flags(c)
	return c
}
