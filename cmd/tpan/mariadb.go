package main

// Lệnh `tpan mariadb upgrade --to <dòng>` (mục 6.11 của docs/KE-HOACH.md):
// nâng MariaDB lên một dòng LTS mới hơn. Một chiều: bản xem trước nói rõ đường
// lùi (file xuất toàn bộ DB). Xong job thì chạy lại toàn bộ health như
// `tpan update`.

import (
	"github.com/spf13/cobra"
)

func init() { register(mariadbCmd) }

func mariadbCmd(e *env) *cobra.Command {
	c := &cobra.Command{
		Use:   "mariadb",
		Short: "Nâng MariaDB lên phiên bản mới",
	}
	c.AddCommand(mariadbUpgradeCmd(e))
	return c
}

func mariadbUpgradeCmd(e *env) *cobra.Command {
	var (
		f    planFlags
		line string
	)
	c := &cobra.Command{
		Use:   "upgrade --to <dòng>",
		Short: "Nâng MariaDB lên một dòng LTS mới hơn (một chiều; xuất toàn bộ DB trước)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, f.asJSON, err)
			}
			u, st, done, err := openUpdates(ctx, e, f.dryRun)
			if err != nil {
				return err
			}
			defer done()
			p, err := u.PrepareMariaDB(ctx, line)
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			rep, err := runPlanned(ctx, e, f, u.Journal, "mariadb.upgrade", line, p)
			if rep.Job.ID == "" {
				return err
			}
			return healthAfterJob(ctx, e, st, f.asJSON, err)
		},
	}
	c.Flags().StringVar(&line, "to", "", "phiên bản MariaDB đích, ví dụ 11.8 (xem các phiên bản nâng được: sudo tpan update --check)")
	_ = c.MarkFlagRequired("to")
	bindChangeFlags(c, &f)
	return c
}
