package main

import (
	"bufio"
	"strings"

	"github.com/spf13/cobra"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
)

func init() { register(stackCmd) }

func stackCmd(e *env) *cobra.Command {
	c := &cobra.Command{
		Use:   "stack",
		Short: "Cài và xem stack (web server, PHP-FPM, MariaDB)",
	}
	c.AddCommand(stackInstallCmd(e), stackStatusCmd(e), stackSwitchCmd(e))
	return c
}

func stackInstallCmd(e *env) *cobra.Command {
	var (
		web                          string
		opts                         core.InstallOptions
		dryRun, yes, asJSON, verbose bool
	)
	c := &cobra.Command{
		Use:   "install --web nginx|apache [--web-source S] [--db-source S --db-line L]",
		Short: "Cài stack LEMP (nginx) hoặc LAMP (apache) lên máy Ubuntu 24.04 sạch",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			w, err := core.ParseWeb(web)
			if err != nil {
				return fail(e, asJSON, err)
			}
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, asJSON, err)
			}

			// --dry-run không được để lại gì: chưa có file DB thì không tạo, chỉ
			// đọc như một bảng settings rỗng.
			s := &core.Stack{Sys: e.sys, Settings: core.EmptySettings{}}
			if !dryRun || e.fileExists(e.dbPath) {
				st, err := openStore(ctx, e)
				if err != nil {
					return err
				}
				defer closeStore(st)
				s.Settings = st
				if !dryRun {
					s.Journal = st
				}
			}

			// Giá trị lạ của nguồn không đi đâu ngoài core.Packages: nó từ chối
			// mọi thứ không nằm trong danh sách nguồn đã biết.
			in, err := s.PrepareInstallWith(ctx, w, opts)
			if err != nil {
				return fail(e, asJSON, err)
			}
			if !asJSON {
				printPreconditionWarnings(e, in.Preconditions)
			}

			if dryRun || !yes {
				pv, err := in.Preview(ctx)
				if err != nil {
					return err
				}
				if dryRun {
					if asJSON {
						return writeJSON(e.stdout, pv)
					}
					printPreview(e, pv)
					return nil
				}
				// Không có --yes: phải có người trả lời câu hỏi.
				if asJSON || !e.isTerminal() {
					return fail(e, asJSON, &core.RefusedError{Code: "confirm_required", Message: i18n.T(
						"không hỏi xác nhận được (không có terminal hoặc đang dùng --json); xem trước bằng --dry-run rồi chạy lại với --yes",
						"cannot ask for confirmation (no terminal, or --json is set); preview with --dry-run, then run again with --yes")})
				}
				printPreview(e, pv)
				if !confirm(e) {
					printf(e.stdout, "%s\n", e.tr("Đã huỷ, không thay đổi gì.", "Cancelled, nothing changed."))
					return &exitCode{1}
				}
			}

			// --json: stdout chỉ có JSON của Report; tiến độ ra stderr cho người xem.
			out := e.stdout
			if asJSON {
				out = e.stderr
			}
			p := newProgress(out, e.lang, verbose, in.Plan)
			rep, err := in.Execute(ctx, p.onEvent)
			cliAudit(ctx, e, s.Journal, "stack.install", string(w), rep.Job)
			if err != nil && rep.Job.ID == "" {
				return err
			}
			if asJSON {
				if werr := writeJSON(e.stdout, rep); werr != nil {
					return werr
				}
			} else {
				printReport(e, rep)
			}
			if err != nil {
				return err
			}
			if rep.Job.Status != engine.JobSucceeded {
				return &exitCode{1}
			}
			return nil
		},
	}
	f := c.Flags()
	f.StringVar(&web, "web", "", "web server: nginx hoặc apache")
	_ = c.MarkFlagRequired("web")
	f.StringVar((*string)(&opts.WebSource), "web-source", "", "nguồn gói web server: ubuntu (mặc định), nginx.org, nginx.org-mainline (chỉ khi --web nginx)")
	f.StringVar((*string)(&opts.DBSource), "db-source", "", "nguồn gói MariaDB: ubuntu (mặc định) hoặc mariadb.org")
	f.StringVar(&opts.DBLine, "db-line", "", "dòng MariaDB khi --db-source mariadb.org, ví dụ 11.8")
	f.BoolVar(&dryRun, "dry-run", false, "chỉ in kế hoạch, không thay đổi gì")
	f.BoolVar(&yes, "yes", false, "không hỏi xác nhận")
	f.BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
	f.BoolVar(&verbose, "verbose", false, "in lệnh và output của mọi bước")
	return c
}

// confirm hỏi "Chạy? [y/N]". Chỉ "y" hoặc "yes" mới là đồng ý: mặc định là không.
func confirm(e *env) bool {
	printf(e.stdout, "%s ", e.tr("Chạy? [y/N]", "Run? [y/N]"))
	line, _ := bufio.NewReader(e.stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes", "c", "có":
		return true
	}
	return false
}

func stackStatusCmd(e *env) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "status",
		Short: "Xem cấu hình stack và trạng thái ba dịch vụ",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, asJSON, err)
			}
			st, err := openStore(cmd.Context(), e)
			if err != nil {
				return err
			}
			defer closeStore(st)
			s := &core.Stack{Sys: e.sys, Settings: st}
			status, err := s.Status(cmd.Context())
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(e.stdout, status)
			}
			printStatus(e, status)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
	return c
}
