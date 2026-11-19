package main

// Lệnh `tpan health` (mục 6.7 và 9 của docs/KE-HOACH.md). Từ M4 lệnh chạy
// health.Runner: kiểm stack, mọi site, file do tpan quản lý (lệch) và chính
// tpan, ghi kết quả và mở, đóng sự cố như bộ hẹn giờ của `tpan serve`.
// Thân lệnh do thợ của phần cmd viết lại; dưới đây là bản của M1-M3.

import (
	"github.com/spf13/cobra"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/validate"
)

func init() { register(healthCmd) }

func healthCmd(e *env) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "health [<domain>]",
		Short: "Chạy các phép kiểm của stack, hoặc của một site khi có domain (mã thoát 1 khi có lỗi)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var d validate.Domain
			if len(args) == 1 {
				var err error
				if d, err = parseDomainArg(args[0]); err != nil {
					return fail(e, asJSON, err)
				}
			}
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, asJSON, err)
			}
			st, err := openStore(cmd.Context(), e)
			if err != nil {
				return err
			}
			defer closeStore(st)
			var h core.Health
			if d.IsZero() {
				h, err = (&core.Stack{Sys: e.sys, Settings: st}).Health(cmd.Context())
			} else {
				h, err = (&core.Sites{Sys: e.sys, Settings: st, Store: st}).Health(cmd.Context(), d)
			}
			if err != nil {
				return fail(e, asJSON, err)
			}
			if asJSON {
				if err := writeJSON(e.stdout, h); err != nil {
					return err
				}
			} else {
				printHealth(e, h)
			}
			// Chưa cài cũng là mã 1: không có gì để khẳng định là khoẻ.
			if !h.Installed || h.HasFail() {
				return &exitCode{1}
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
	return c
}
