package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/validate"
)

func init() { register(sslCmd) }

func sslCmd(e *env) *cobra.Command {
	c := &cobra.Command{
		Use:   "ssl",
		Short: "Xin chứng chỉ SSL (Let's Encrypt) và đặt cấu hình SSL",
	}
	c.AddCommand(sslIssueCmd(e), sslListCmd(e), sslSettingsCmd(e))
	return c
}

// openSSL dựng core.SSL trên cùng DB với ca sử dụng site (openSites): --dry-run
// mà chưa có file DB thì không tạo, chỉ đọc như bảng rỗng.
func openSSL(ctx context.Context, e *env, dryRun bool) (*core.SSL, func(), error) {
	s, done, err := openSites(ctx, e, dryRun)
	if err != nil {
		return nil, nil, err
	}
	return &core.SSL{Sys: s.Sys, Settings: s.Settings, Store: s.Store, Journal: s.Journal}, done, nil
}

// emailOverlay là bảng settings thấy ssl.email là email của cờ --email mà
// không ghi gì: `ssl issue --dry-run --email E` xem trước đúng Plan sẽ chạy,
// trong khi --dry-run không được để lại gì.
type emailOverlay struct {
	core.Settings
	email string
}

func (o emailOverlay) GetSetting(ctx context.Context, key string) (string, bool, error) {
	if key == core.SettingSSLEmail {
		return o.email, true, nil
	}
	return o.Settings.GetSetting(ctx, key)
}

func (o emailOverlay) SetSetting(_ context.Context, key, _ string) error {
	return fmt.Errorf("ghi setting %s: --dry-run không ghi gì", key)
}

// settingsAudit ghi nhật ký cho lần đổi cấu hình SSL ở dòng lệnh. Không có job
// nên không dùng được cliAudit; như cliAudit, ghi lỗi chỉ in ra stderr.
func settingsAudit(ctx context.Context, e *env, w any, result string) {
	aw, ok := w.(auditWriter)
	if !ok {
		return
	}
	if err := aw.AddAudit(ctx, store.AuditEntry{Time: time.Now(), IP: "cli", Action: "ssl.settings", Target: "ssl", Result: result}); err != nil {
		printf(e.stderr, "tpan: %v\n", err)
	}
}

// setSSL ghi cấu hình rồi ghi nhật ký (kể cả khi bị từ chối, với mã từ chối).
func setSSL(ctx context.Context, e *env, s *core.SSL, in core.SSLSettingsInput) (core.SSLConfig, error) {
	cfg, err := s.SetConfig(ctx, in)
	result := "ok"
	if re := (*core.RefusedError)(nil); errors.As(err, &re) {
		result = re.Code
	} else if err != nil {
		result = "error"
	}
	settingsAudit(ctx, e, s.Settings, result)
	return cfg, err
}

// runSSLIssue là phần chung của `ssl issue` và `site add --ssl`: điều kiện đầu,
// xem trước hoặc hỏi lại, chạy, ghi nhật ký. ran=false là Plan không chạy (bị
// từ chối, --dry-run, người dùng huỷ); khi đó err là lời từ chối thô hoặc mã
// thoát do previewOrConfirm trả, để người gọi tự in.
func runSSLIssue(ctx context.Context, e *env, f *planRun, s *core.SSL, d validate.Domain) (rep engine.Report, ran bool, err error) {
	iss, err := s.PrepareIssue(ctx, d)
	if err != nil {
		return rep, false, err
	}
	if !f.asJSON {
		printPreconditionWarnings(e, iss.Preconditions)
	}
	if ok, err := f.previewOrConfirm(e, func() (engine.Preview, error) { return iss.Preview(ctx) }); !ok {
		return rep, false, err
	}
	rep, err = iss.Execute(ctx, f.progress(e, iss.Plan).onEvent)
	cliAudit(ctx, e, s.Store, "site.ssl", d.String(), rep.Job)
	return rep, true, err
}

// finishReport in kết quả job (JSON hoặc chữ) và đổi nó thành mã thoát, như
// site rm: job hỏng là mã 1, lỗi sau khi job đã chạy vẫn in báo cáo trước.
func finishReport(e *env, asJSON bool, rep engine.Report, err error) error {
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
}

func sslIssueCmd(e *env) *cobra.Command {
	var (
		email string
		f     planRun
	)
	c := &cobra.Command{
		Use:   "issue <domain> [--email E]",
		Short: "Xin (hoặc chạy lại) chứng chỉ Let's Encrypt cho một site và bật HTTPS",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			d, err := parseDomainArg(args[0])
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			var em validate.Email
			if email != "" {
				if em, err = validate.ParseEmail(email); err != nil {
					return fail(e, f.asJSON, asRefusal(err))
				}
			}
			if !f.dryRun {
				if err := requireRoot(e, cmd); err != nil {
					return fail(e, f.asJSON, err)
				}
			}
			s, done, err := openSSL(ctx, e, f.dryRun)
			if err != nil {
				return err
			}
			defer done()

			if !em.IsZero() {
				if f.dryRun {
					s.Settings = emailOverlay{Settings: s.Settings, email: em.String()}
				} else if _, err := setSSL(ctx, e, s, core.SSLSettingsInput{Email: &em}); err != nil {
					return fail(e, f.asJSON, err)
				}
			}
			rep, ran, err := runSSLIssue(ctx, e, &f, s, d)
			if !ran {
				return fail(e, f.asJSON, err)
			}
			return finishReport(e, f.asJSON, rep, err)
		},
	}
	c.Flags().StringVar(&email, "email", "", "email đăng ký với Let's Encrypt (ghi vào cấu hình SSL; chỉ cần lần đầu)")
	f.flags(c)
	return c
}

// siteAddSSL là phần --ssl của site add (mục 6.2, bước 11): site vừa tạo xong
// thì chạy luôn Plan SSL. ssl.email chưa có thì dùng email quản trị của site và
// ghi lại. Trả báo cáo khi Plan đã chạy, lời từ chối khi bị từ chối; err là các
// lỗi khác (kể cả mã thoát khi người dùng huỷ).
func siteAddSSL(ctx context.Context, e *env, f *planRun, sites *core.Sites, d validate.Domain, em validate.Email) (*engine.Report, *core.RefusedError, error) {
	s := &core.SSL{Sys: sites.Sys, Settings: sites.Settings, Store: sites.Store, Journal: sites.Journal}
	cfg, err := s.Config(ctx)
	if err != nil {
		return nil, nil, err
	}
	var re *core.RefusedError
	if cfg.Email == "" {
		if _, err := setSSL(ctx, e, s, core.SSLSettingsInput{Email: &em}); errors.As(err, &re) {
			return nil, re, nil
		} else if err != nil {
			return nil, nil, err
		}
	}
	rep, ran, err := runSSLIssue(ctx, e, f, s, d)
	switch {
	case errors.As(err, &re):
		return nil, re, nil
	case !ran || (err != nil && rep.Job.ID == ""):
		return nil, nil, err
	}
	return &rep, nil, err
}

func sslListCmd(e *env) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "list",
		Short: "Liệt kê các site và chứng chỉ của chúng",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, asJSON, err)
			}
			s, done, err := openSSL(cmd.Context(), e, false)
			if err != nil {
				return err
			}
			defer done()
			rows, err := s.List(cmd.Context())
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(e.stdout, rows)
			}
			printSSLList(e, rows)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
	return c
}

func sslSettingsCmd(e *env) *cobra.Command {
	var (
		email, server   string
		staging, asJSON bool
	)
	c := &cobra.Command{
		Use:   "settings [--email E] [--staging=true|false]",
		Short: "Xem hoặc đặt email cho Let's Encrypt và chế độ staging (không cờ nào thì chỉ xem)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			fl := cmd.Flags()
			var in core.SSLSettingsInput
			if fl.Changed("email") {
				em, err := validate.ParseEmail(email)
				if err != nil {
					return fail(e, asJSON, asRefusal(err))
				}
				in.Email = &em
			}
			// Changed chứ không phải giá trị: --staging=false và --server "" là
			// thay đổi thật (tắt staging, xoá server), không phải "không đặt".
			if fl.Changed("staging") {
				in.Staging = &staging
			}
			if fl.Changed("server") {
				in.Server = &server
			}
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, asJSON, err)
			}
			s, done, err := openSSL(ctx, e, false)
			if err != nil {
				return err
			}
			defer done()

			var cfg core.SSLConfig
			if in == (core.SSLSettingsInput{}) {
				cfg, err = s.Config(ctx)
			} else {
				cfg, err = setSSL(ctx, e, s, in)
			}
			if err != nil {
				return fail(e, asJSON, err)
			}
			if asJSON {
				return writeJSON(e.stdout, cfg)
			}
			printSSLConfig(e, cfg)
			return nil
		},
	}
	fl := c.Flags()
	fl.StringVar(&email, "email", "", "email đăng ký với Let's Encrypt (nhận thư báo chứng chỉ sắp hết hạn)")
	fl.BoolVar(&staging, "staging", true, "dùng máy chủ thử của Let's Encrypt (chứng chỉ không được trình duyệt tin, nhưng không tốn hạn mức)")
	fl.StringVar(&server, "server", "", "địa chỉ thư mục ACME khác Let's Encrypt (chỉ cho test); rỗng là xoá")
	// Ẩn: chỉ e2e dùng, với một máy chủ ACME thử chạy trong máy ảo.
	_ = fl.MarkHidden("server")
	fl.BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
	return c
}

func printSSLConfig(e *env, cfg core.SSLConfig) {
	w := e.stdout
	email := cfg.Email
	if email == "" {
		email = e.tr("(chưa có; đặt bằng: sudo tpan ssl settings --email <email>)", "(none; set it with: sudo tpan ssl settings --email <email>)")
	}
	staging := e.tr("tắt (chứng chỉ thật)", "off (real certificates)")
	if cfg.Staging {
		staging = e.tr("bật (chứng chỉ thử, trình duyệt không tin)", "on (test certificates, not trusted by browsers)")
	}
	printf(w, "%-10s %s\n", "Email:", email)
	printf(w, "%-10s %s\n", "Staging:", staging)
	printf(w, "%-10s %s\n", "ACME:", cfg.ServerURL())
}

func printSSLList(e *env, rows []core.SSLSiteRow) {
	w := e.stdout
	if len(rows) == 0 {
		printf(w, "%s\n", e.tr(
			"Chưa có site nào. Tạo bằng: sudo tpan site add <domain> --email <email> --ssl",
			"No sites yet. Create one with: sudo tpan site add <domain> --email <email> --ssl"))
		return
	}
	printf(w, "%-40s %-6s %s\n", "DOMAIN", "SSL", e.tr("CHỨNG CHỈ", "CERTIFICATE"))
	for _, r := range rows {
		if r.Cert == nil {
			printf(w, "%-40s %-6s %s\n", r.Domain, e.tr("không", "no"), e.tr("xin bằng: sudo tpan ssl issue ", "get one with: sudo tpan ssl issue ")+r.Domain.String())
			continue
		}
		printf(w, "%-40s %-6s [%s] %s\n", r.Domain, e.tr("có", "yes"), statusWord(e.lang, r.Cert.Status), r.Cert.Summary.In(e.lang))
	}
}
