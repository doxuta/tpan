package main

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/server"
)

func init() { register(securityCmd) }

func securityCmd(e *env) *cobra.Command {
	c := &cobra.Command{
		Use:   "security",
		Short: "Xem bảo mật của máy; bật tường lửa ufw và fail2ban; áp bản vá cô lập giữa các site",
	}
	c.AddCommand(
		securityStatusCmd(e),
		securityPlanCmd(e, "ufw", "Bật tường lửa ufw: mở cổng SSH, 80, 443 (và cổng panel nếu đang mở) trước rồi mới bật",
			"security.ufw", (*core.Security).PrepareUFW),
		securityPlanCmd(e, "fail2ban", "Cài và bật fail2ban cho SSH (chặn IP dò mật khẩu)",
			"security.fail2ban", (*core.Security).PrepareFail2ban),
		securityHardenCmd(e),
	)
	return c
}

// securityHardenCmd là `tpan security harden` (M14, mục 6.13): Plan "Áp bản vá
// cô lập" cho máy nâng từ bản trước M14. Trước câu hỏi xác nhận in thêm hai
// danh sách của core.Harden, vì bản xem trước (các bước) không nói site nào
// sau job vẫn còn ở chế độ day-du.
func securityHardenCmd(e *env) *cobra.Command {
	var f planRun
	c := &cobra.Command{
		Use:   "harden",
		Short: "Áp bản vá cô lập giữa các site: OPcache, ProtectProc, quyền log, vhost, chế độ .htaccess",
		Long: "Việc đầu tiên sau khi cập nhật tpan lên bản có M14. Một Plan làm hết: ghi file ini OPcache và\n" +
			"drop-in ProtectProc cho mọi bản PHP, ghi lại logrotate, đặt quyền log, quét .htaccess của từng site\n" +
			"(sạch thì chuyển sang an-toan; có chỉ thị ngoài danh sách thì giữ day-du và in dòng vướng), ghi lại\n" +
			"vhost của mọi site, kiểm cú pháp, reload một lần rồi chạy lại phép kiểm. Chạy lại khi mọi thứ đã\n" +
			"đúng thì không bước nào chạy.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			// Cần root cả khi --dry-run: dựng Plan phải quét .htaccess dưới user
			// của từng site (runuser), việc chỉ root làm được.
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, f.asJSON, err)
			}
			sec := &core.Security{Sys: e.sys, Settings: core.EmptySettings{}, Sites: core.EmptySiteStore{}, WPCLISHA512: e.wpcliSHA512}
			// --dry-run không tạo DB (như stack install); chưa có DB thì stack
			// chưa cài và core từ chối.
			var audit any
			if !f.dryRun || e.fileExists(e.dbPath) {
				st, err := openStore(ctx, e)
				if err != nil {
					return err
				}
				defer closeStore(st)
				sec.Settings, sec.Sites = st, st
				if !f.dryRun {
					sec.Journal, audit = st, st
				}
			}
			h, err := sec.PrepareHarden(ctx)
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			ok, err := f.showOrConfirm(e, func() (any, func(), error) {
				pv, err := h.Preview(ctx)
				// Cùng dạng với POST /api/v1/security/harden?dry_run=1.
				out := server.HardenPreview{Preview: pv, Switched: nonNil(h.Switched), Kept: nonNil(h.Kept)}
				return out, func() {
					printPreview(e, pv)
					printHardenModes(e, h.Switched, h.Kept)
				}, err
			})
			if !ok {
				return err
			}
			// --yes bỏ qua bản xem trước nhưng site giữ day-du vẫn phải được nói
			// ra: đó là việc còn lại cho người vận hành sau job.
			if f.yes && !f.asJSON {
				printHardenModes(e, h.Switched, h.Kept)
			}
			rep, err := h.Execute(ctx, f.progress(e, h.Plan).onEvent)
			cliAudit(ctx, e, audit, "security.harden", h.Plan.Target, rep.Job)
			return finishReport(e, f.asJSON, rep, err)
		},
	}
	f.flags(c)
	return c
}

// nonNil để danh sách rỗng ra JSON là [] chứ không phải null.
func nonNil[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}

// printHardenModes in site nào sẽ chuyển sang chế độ .htaccess an-toan và site
// nào giữ day-du kèm từng dòng vướng "<file>:<dòng>: <chỉ thị>".
func printHardenModes(e *env, switched []string, kept []core.HtaccessKept) {
	w := e.stdout
	if len(switched) == 0 && len(kept) == 0 {
		return
	}
	printf(w, "%s\n", e.tr("Chế độ .htaccess:", ".htaccess mode:"))
	if len(switched) > 0 {
		printf(w, e.tr("  Chuyển sang an-toan (%d site, .htaccess không có chỉ thị nào ngoài danh sách cho phép):\n",
			"  Switching to an-toan (%d site(s), their .htaccess has no directive outside the allowed list):\n"), len(switched))
		for _, d := range switched {
			printf(w, "    %s\n", d)
		}
	}
	if len(kept) > 0 {
		printf(w, e.tr("  Giữ day-du (%d site) vì .htaccess có chỉ thị ngoài danh sách cho phép; ở an-toan các dòng này làm site báo 500:\n",
			"  Staying on day-du (%d site(s)) because their .htaccess has directives outside the allowed list; on an-toan these lines make the site return 500:\n"), len(kept))
		for _, k := range kept {
			printf(w, "    %s\n", k.Domain)
			for _, fd := range k.Findings {
				printf(w, "      %s:%d: %s\n", fd.File, fd.Line, fd.Directive)
			}
		}
		printf(w, "  %s\n", e.tr(
			"Site giữ day-du vẫn ghi được cấu hình Apache và health tiếp tục cảnh báo. Sửa các dòng trên rồi chạy: sudo tpan site set <domain> --htaccess an-toan",
			"Sites left on day-du can still write Apache configuration and health keeps warning. Fix the lines above, then run: sudo tpan site set <domain> --htaccess an-toan"))
	}
	printf(w, "\n")
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
