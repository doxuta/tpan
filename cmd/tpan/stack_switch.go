package main

// `tpan stack switch --to nginx|apache` (mục 6.6 và mục 9). Dây nối mỏng: Plan
// do core.Switch dựng, nhịp xem trước, hỏi lại, chạy, audit là runPlanned
// (planned.go), health đầy đủ sau job như `tpan update` (update.go).

import (
	"github.com/spf13/cobra"

	"github.com/doxuta/tpan/internal/core"
)

func stackSwitchCmd(e *env) *cobra.Command {
	var (
		to string
		f  planFlags
	)
	c := &cobra.Command{
		Use:   "switch --to nginx|apache",
		Short: "Chuyển web server tại chỗ: LEMP (nginx) ⇄ LAMP (apache), mọi site đi theo",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			w, err := core.ParseWeb(to)
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, f.asJSON, err)
			}
			// --dry-run khi chưa có DB: không tạo DB, đọc như bảng rỗng (core từ
			// chối stack_not_installed).
			sw := &core.Switch{Sys: e.sys, Settings: core.EmptySettings{}, Sites: core.EmptySiteStore{}}
			if f.dryRun && !e.fileExists(e.dbPath) {
				p, err := sw.Prepare(ctx, w)
				if err != nil {
					return fail(e, f.asJSON, err)
				}
				_, err = runPlanned(ctx, e, f, nil, "stack.switch", string(w), p)
				return err
			}
			st, err := openStore(ctx, e)
			if err != nil {
				return err
			}
			defer closeStore(st)
			sw.Settings, sw.Sites = st, st
			if !f.dryRun {
				sw.Journal = st
			}
			p, err := sw.Prepare(ctx, w)
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			rep, err := runPlanned(ctx, e, f, sw.Journal, "stack.switch", string(w), p)
			if rep.Job.ID == "" {
				return err
			}
			// Kiểm lại mọi thứ: phép kiểm của web server cũ không còn chạy, sự cố
			// của chúng đóng; site nào không đạt trên web server mới thì mở sự cố.
			return healthAfterJob(ctx, e, st, f.asJSON, err)
		},
	}
	c.Flags().StringVar(&to, "to", "", "web server mới: nginx hoặc apache")
	_ = c.MarkFlagRequired("to")
	bindChangeFlags(c, &f)
	return c
}
