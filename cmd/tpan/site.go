package main

import (
	"context"
	"errors"

	"github.com/spf13/cobra"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/validate"
)

func init() { register(siteCmd) }

func siteCmd(e *env) *cobra.Command {
	c := &cobra.Command{
		Use:   "site",
		Short: "Tạo, xem và xoá site WordPress",
	}
	c.AddCommand(siteAddCmd(e), siteListCmd(e), siteInfoCmd(e), siteRmCmd(e))
	return c
}

// asRefusal đổi lỗi kiểm đầu vào của validate thành lời từ chối của core, để
// fail() in nó thân thiện (hoặc {"error": ...} khi --json) như mọi lời từ chối.
func asRefusal(err error) error {
	var ve *validate.Error
	if errors.As(err, &ve) {
		return &core.RefusedError{Code: ve.Code, Message: ve.Message}
	}
	return err
}

// parseDomainArg kiểm domain người dùng gõ. Gọi trước mọi việc khác (hỏi root,
// mở DB, chạy lệnh): domain sai thì không gì chạm tới máy.
func parseDomainArg(s string) (validate.Domain, error) {
	d, err := validate.ParseDomain(s)
	return d, asRefusal(err)
}

// openSites dựng core.Sites. --dry-run không được để lại gì: chưa có file DB
// thì không tạo, chỉ đọc như bảng settings và bảng sites rỗng (giống stack install).
func openSites(ctx context.Context, e *env, dryRun bool) (*core.Sites, func(), error) {
	s := &core.Sites{Sys: e.sys, Settings: core.EmptySettings{}, Store: core.EmptySiteStore{}}
	if dryRun && !e.fileExists(e.dbPath) {
		return s, func() {}, nil
	}
	st, err := openStore(ctx, e)
	if err != nil {
		return nil, nil, err
	}
	s.Settings, s.Store = st, st
	if !dryRun {
		s.Journal = st
	}
	return s, func() { closeStore(st) }, nil
}

// planRun là phần chung của site add và site rm: xem trước, hỏi lại, rồi chạy.
type planRun struct {
	dryRun, yes, asJSON, verbose bool
}

func (f *planRun) flags(c *cobra.Command) {
	fl := c.Flags()
	fl.BoolVar(&f.dryRun, "dry-run", false, "chỉ in kế hoạch, không thay đổi gì")
	fl.BoolVar(&f.yes, "yes", false, "không hỏi xác nhận")
	fl.BoolVar(&f.asJSON, "json", false, "in kết quả dạng JSON")
	fl.BoolVar(&f.verbose, "verbose", false, "in lệnh và output của mọi bước")
}

// previewOrConfirm in bản xem trước khi --dry-run (xong là dừng) hoặc khi phải
// hỏi lại; trả proceed=true khi được chạy tiếp.
func (f *planRun) previewOrConfirm(e *env, preview func() (engine.Preview, error)) (proceed bool, err error) {
	if !f.dryRun && f.yes {
		return true, nil
	}
	pv, err := preview()
	if err != nil {
		return false, err
	}
	if f.dryRun {
		if f.asJSON {
			return false, writeJSON(e.stdout, pv)
		}
		printPreview(e, pv)
		return false, nil
	}
	// Không có --yes: phải có người trả lời câu hỏi.
	if f.asJSON || !e.isTerminal() {
		return false, fail(e, f.asJSON, &core.RefusedError{Code: "confirm_required", Message: i18n.T(
			"không hỏi xác nhận được (không có terminal hoặc đang dùng --json); xem trước bằng --dry-run rồi chạy lại với --yes",
			"cannot ask for confirmation (no terminal, or --json is set); preview with --dry-run, then run again with --yes")})
	}
	printPreview(e, pv)
	if !confirm(e) {
		printf(e.stdout, "%s\n", e.tr("Đã huỷ, không thay đổi gì.", "Cancelled, nothing changed."))
		return false, &exitCode{1}
	}
	return true, nil
}

// progress: --json thì stdout chỉ có JSON kết quả, tiến độ ra stderr cho người xem.
func (f *planRun) progress(e *env, p engine.Plan) *progress {
	out := e.stdout
	if f.asJSON {
		out = e.stderr
	}
	return newProgress(out, e.lang, f.verbose, p)
}

func siteAddCmd(e *env) *cobra.Command {
	var (
		title, email string
		withSSL      bool
		f            planRun
	)
	c := &cobra.Command{
		Use:   "add <domain> --email E [--title T] [--ssl]",
		Short: "Tạo một site WordPress (chạy lại cho domain đã có thì sửa chỗ lệch)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			d, err := parseDomainArg(args[0])
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			em, err := validate.ParseEmail(email)
			if err != nil {
				return fail(e, f.asJSON, asRefusal(err))
			}
			if !f.dryRun {
				if err := requireRoot(e, cmd); err != nil {
					return fail(e, f.asJSON, err)
				}
			}
			s, done, err := openSites(ctx, e, f.dryRun)
			if err != nil {
				return err
			}
			defer done()

			add, err := s.PrepareAdd(ctx, core.SiteInput{Domain: d, Title: title, Email: em})
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			if ok, err := f.previewOrConfirm(e, func() (engine.Preview, error) { return add.Preview(ctx) }); !ok {
				// Plan SSL chưa xem trước được: site chưa có trong bảng sites.
				if err == nil && withSSL && !f.asJSON {
					printf(e.stdout, "\n%s\n", e.tr(
						"Có --ssl: tạo site xong thì tpan chạy tiếp Plan xin SSL (xem trước được bằng `tpan ssl issue "+d.String()+" --dry-run` khi site đã có).",
						"With --ssl: once the site exists tpan goes on with the SSL Plan (preview it with `tpan ssl issue "+d.String()+" --dry-run` once the site exists)."))
				}
				return err
			}

			res, err := add.Execute(ctx, f.progress(e, add.Plan).onEvent)
			cliAudit(ctx, e, s.Store, "site.add", d.String(), res.Report.Job)
			if err != nil && res.Report.Job.ID == "" {
				return err
			}
			out := siteAddJSONOf(res)
			if !f.asJSON {
				// In mật khẩu quản trị TRƯỚC khi chạy SSL: SSL hỏng cũng không làm mất nó.
				printReport(e, res.Report)
				printAdminPassword(e, res)
			}
			siteOK := err == nil && res.Report.Job.Status == engine.JobSucceeded
			var sslErr error
			if withSSL && siteOK {
				out.SSL, out.SSLError, sslErr = siteAddSSL(ctx, e, &f, s, d, em)
				if !f.asJSON {
					switch {
					case out.SSLError != nil:
						printRefusal(e, out.SSLError)
					case out.SSL != nil:
						printReport(e, *out.SSL)
					}
				}
				if sslErr == nil && (out.SSL == nil || out.SSL.Job.Status != engine.JobSucceeded) {
					sslErr = &exitCode{1}
				}
			}
			if f.asJSON {
				if werr := writeJSON(e.stdout, out); werr != nil {
					return werr
				}
			}
			if err != nil {
				return err
			}
			if !siteOK {
				return &exitCode{1}
			}
			return sslErr
		},
	}
	fl := c.Flags()
	fl.StringVar(&email, "email", "", "email của tài khoản quản trị WordPress")
	_ = c.MarkFlagRequired("email")
	fl.StringVar(&title, "title", "", "tiêu đề site (mặc định là domain)")
	fl.BoolVar(&withSSL, "ssl", false, "tạo xong thì xin luôn chứng chỉ Let's Encrypt (email SSL chưa đặt thì dùng --email)")
	f.flags(c)
	return c
}

// siteAddJSON là output --json của site add. Mật khẩu quản trị nằm ở đây dạng
// chữ thật, đúng một lần, và chỉ khi WordPress vừa được cài: script tạo site
// không có cách nào khác để lấy nó. (core.SiteAddResult giữ nó dạng Secret,
// JSON của nó luôn là "••••".)
type siteAddJSON struct {
	Report        engine.Report `json:"report"`
	Site          site.Site     `json:"site"`
	WPInstalled   bool          `json:"wp_installed"`
	AdminUser     string        `json:"admin_user,omitempty"`
	AdminPassword string        `json:"admin_password,omitempty"`
	// SSL là báo cáo job SSL khi có --ssl và Plan SSL đã chạy; SSLError là lời
	// từ chối của nó (ví dụ DNS chưa trỏ về máy). Site vẫn được tạo trong cả hai trường hợp.
	SSL      *engine.Report     `json:"ssl,omitempty"`
	SSLError *core.RefusedError `json:"ssl_error,omitempty"`
}

func siteAddJSONOf(res core.SiteAddResult) siteAddJSON {
	out := siteAddJSON{Report: res.Report, Site: res.Site, WPInstalled: res.WPInstalled}
	if res.WPInstalled {
		out.AdminUser, out.AdminPassword = res.AdminUser, res.AdminPassword.Reveal()
	}
	return out
}

func siteListCmd(e *env) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "list",
		Short: "Liệt kê các site",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, asJSON, err)
			}
			s, done, err := openSites(cmd.Context(), e, false)
			if err != nil {
				return err
			}
			defer done()
			list, err := s.List(cmd.Context())
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(e.stdout, list)
			}
			printSiteList(e, list)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
	return c
}

func siteInfoCmd(e *env) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "info <domain>",
		Short: "Xem tên, đường dẫn và kết quả kiểm của một site",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, err := parseDomainArg(args[0])
			if err != nil {
				return fail(e, asJSON, err)
			}
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, asJSON, err)
			}
			s, done, err := openSites(cmd.Context(), e, false)
			if err != nil {
				return err
			}
			defer done()
			info, err := s.Info(cmd.Context(), d)
			if err != nil {
				return fail(e, asJSON, err)
			}
			if asJSON {
				return writeJSON(e.stdout, info)
			}
			printSiteInfo(e, info)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
	return c
}

func siteRmCmd(e *env) *cobra.Command {
	var f planRun
	c := &cobra.Command{
		Use:   "rm <domain>",
		Short: "Xoá hẳn một site: vhost, pool, DB, file, user (không lấy lại được)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			d, err := parseDomainArg(args[0])
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			if !f.dryRun {
				if err := requireRoot(e, cmd); err != nil {
					return fail(e, f.asJSON, err)
				}
			}
			s, done, err := openSites(ctx, e, f.dryRun)
			if err != nil {
				return err
			}
			defer done()

			rm, err := s.PrepareRemove(ctx, d)
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			if ok, err := f.previewOrConfirm(e, func() (engine.Preview, error) { return rm.Preview(ctx) }); !ok {
				return err
			}

			rep, err := rm.Execute(ctx, f.progress(e, rm.Plan).onEvent)
			cliAudit(ctx, e, s.Store, "site.rm", d.String(), rep.Job)
			if err != nil && rep.Job.ID == "" {
				return err
			}
			if f.asJSON {
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
	f.flags(c)
	return c
}
