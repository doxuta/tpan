package main

// `tpan cron list` và `tpan cron run <domain>` (mục 6.10 và 9 của
// docs/KE-HOACH.md). `cron run` là lệnh dòng cron của từng site gọi mỗi 5
// phút (file /etc/cron.d/tpan-<slug>), không phải lệnh cho người gõ hằng ngày.

import (
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/doxuta/tpan/internal/core"
)

func init() { register(cronCmd) }

func cronCmd(e *env) *cobra.Command {
	c := &cobra.Command{
		Use:   "cron",
		Short: "Cron WordPress thật của các site và lịch backup",
	}
	c.AddCommand(cronListCmd(e), cronRunCmd(e))
	return c
}

func cronListCmd(e *env) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "list",
		Short: "Liệt kê lịch cron của mọi site, lần chạy gần nhất và lịch backup",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, asJSON, err)
			}
			// Chỉ đọc: chưa có tpan.db thì không tạo.
			c, done, err := openCron(ctx, e, true)
			if err != nil {
				return err
			}
			defer done()
			ov, err := c.List(ctx)
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(e.stdout, ov)
			}
			printCron(e, ov)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
	return c
}

func printCron(e *env, ov core.CronOverview) {
	w := e.stdout
	if len(ov.Sites) == 0 {
		printf(w, "%s\n", e.tr("Chưa có site nào.", "No sites yet."))
	} else {
		printf(w, "%s %s\n\n", e.tr("Mỗi site chạy `wp cron event run --due-now` theo lịch", "Each site runs `wp cron event run --due-now` on the schedule"), core.CronEvery)
		printf(w, "%-40s %-6s %-16s %-8s %s\n", "DOMAIN", e.tr("BẬT", "ON"), e.tr("CHẠY LÚC", "LAST RUN"), e.tr("KẾT QUẢ", "RESULT"), e.tr("MẤT", "TOOK"))
		for _, s := range ov.Sites {
			on := e.tr("tắt", "off")
			if s.Enabled {
				on = e.tr("bật", "on")
			}
			at, res, took := "-", "-", "-"
			if r := s.Last; r != nil {
				at = r.Started.Local().Format("2006-01-02 15:04")
				res = string(r.Status)
				if r.Status == core.CronFail {
					res += " (" + strconv.Itoa(r.ExitCode) + ")"
				}
				took = r.Duration.Round(time.Millisecond).String()
			}
			printf(w, "%-40s %-6s %-16s %-8s %s\n", s.Domain, on, at, res, took)
		}
	}
	printf(w, "\n")
	if ov.Backup.At == "" {
		printf(w, "%s\n", e.tr(
			"Lịch backup: tắt. Bật bằng: sudo tpan backup settings --at HH:MM",
			"Backup schedule: off. Turn it on with: sudo tpan backup settings --at HH:MM"))
		return
	}
	printf(w, "%s\n", e.tr(
		"Lịch backup: mọi site lúc "+ov.Backup.At+" hằng ngày, giữ "+strconv.Itoa(ov.Backup.Keep)+" bản mỗi site ("+ov.BackupFile+")",
		"Backup schedule: every site daily at "+ov.Backup.At+", keeping "+strconv.Itoa(ov.Backup.Keep)+" copies per site ("+ov.BackupFile+")"))
}

func cronRunCmd(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "run <domain>",
		Short: "Chạy cron WordPress của một site (dòng cron của tpan gọi lệnh này mỗi 5 phút)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			d, err := parseDomainArg(args[0])
			if err != nil {
				return fail(e, false, err)
			}
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, false, err)
			}
			c, done, err := openCron(ctx, e, false)
			if err != nil {
				return err
			}
			defer done()
			// Thành công hay wp lỗi đều im lặng và thoát 0: kết quả đã nằm ở
			// tpan.db (màn Cron), còn cron gửi thư hay ghi log mỗi khi lệnh in
			// ra gì đó, 5 phút một lần mỗi site. Chỉ in và thoát khác 0 khi
			// không ghi được kết quả hoặc site không có (dòng cron sai).
			_, err = c.Run(ctx, d)
			return fail(e, false, err)
		},
	}
}
