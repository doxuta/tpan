package main

import (
	"github.com/spf13/cobra"

	"github.com/doxuta/tpan/internal/core"
)

func init() { register(internalCmd) }

// internalCmd gom các lệnh chỉ chương trình khác gọi (hook của certbot), không
// phải người. Ẩn khỏi help để không ai tưởng đó là lệnh dùng hằng ngày.
func internalCmd(e *env) *cobra.Command {
	c := &cobra.Command{
		Use:    "internal",
		Short:  "Lệnh nội bộ cho các hook của tpan",
		Hidden: true,
	}
	c.AddCommand(&cobra.Command{
		Use:   "reload-web",
		Short: "Kiểm config rồi reload web server của stack (hook gia hạn chứng chỉ gọi)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, false, err)
			}
			// Chưa có DB thì stack chưa cài: không mở (openStore sẽ tạo một DB
			// rỗng từ trong hook của certbot), đọc như bảng rỗng là đủ.
			var settings core.Settings = core.EmptySettings{}
			if e.fileExists(e.dbPath) {
				st, err := openStore(cmd.Context(), e)
				if err != nil {
					return err
				}
				defer closeStore(st)
				settings = st
			}
			return core.ReloadWeb(cmd.Context(), e.sys, settings)
		},
	})
	return c
}
