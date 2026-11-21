package main

// Lệnh con của M18 dưới `tpan backup`: target set|show|test|rm, key new|show,
// push, pull (mục 3.12 của docs/thay-doi/M18-thiet-ke.md). Đợt 0 chỉ có móc
// rỗng; thợ đợt 3 thay cả file này.

import "github.com/spf13/cobra"

// backupS3Cmds trả các lệnh con M18 của `tpan backup` (backup.go gắn chúng vào).
func backupS3Cmds(_ *env) []*cobra.Command { return nil }
