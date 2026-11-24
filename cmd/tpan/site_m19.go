package main

// tpan site alias add|set|rm|list và tpan site redirect add|rm|list (M19,
// docs/thay-doi/M19-thiet-ke.md mục 3.8): tên phụ và luật chuyển hướng của
// site. Mỗi thao tác đổi là một Plan có xem trước như các
// lệnh site khác (--dry-run, --yes, --json). CLI chạy bằng root nên là admin
// (core.Sites.NotAdmin để zero).

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/validate"
)

func siteAliasCmd(e *env) *cobra.Command {
	c := &cobra.Command{
		Use:   "alias",
		Short: "Tên phụ của site (www., tên miền khác): thêm, đổi kiểu, gỡ, liệt kê",
	}
	c.AddCommand(siteAliasAddCmd(e), siteAliasSetCmd(e), siteAliasRmCmd(e), siteAliasListCmd(e))
	return c
}

// parseAliasModeArg đọc --mode; rỗng là def (def rỗng: bắt buộc).
func parseAliasModeArg(s string, def site.NameMode) (site.NameMode, error) {
	if s == "" && def != "" {
		return def, nil
	}
	if m := site.NameMode(s); m == site.ModeServe || m == site.ModeRedirect {
		return m, nil
	}
	return "", &core.RefusedError{Code: core.CodeNameModeInvalid, Message: i18n.T(
		"--mode phải là một trong: redirect (301 về tên chính), serve (phục vụ cùng nội dung)",
		"--mode must be one of: redirect (301 to the primary name), serve (serve the same content)")}
}

// parseWWWArg đọc --www của site add: redirect (mặc định), serve, none.
func parseWWWArg(s string) (site.NameMode, error) {
	if m := site.NameMode(s); m == site.ModeRedirect || m == site.ModeServe || m == core.WWWNone {
		return m, nil
	}
	return "", &core.RefusedError{Code: core.CodeNameModeInvalid, Message: i18n.T(
		"--www phải là một trong: redirect, serve, none",
		"--www must be one of: redirect, serve, none")}
}

// nameRefusalCLI:*site.NameError chưa được core dịch thì thành lời từ chối
// để fail() in thân thiện. Dòng lệnh là admin, nên giữ mã thật.
func nameRefusalCLI(err error) error {
	var ne *site.NameError
	if errors.As(err, &ne) {
		return &core.RefusedError{Code: ne.Code, Message: i18n.T(ne.Error(), ne.Error())}
	}
	return asRefusal(err)
}

func siteAliasAddCmd(e *env) *cobra.Command {
	var (
		f    planRun
		mode string
	)
	c := &cobra.Command{
		Use:   "add <domain> <name> [--mode serve|redirect]",
		Short: "Thêm tên phụ cho site (mặc định redirect: 301 về tên chính)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := parseAliasModeArg(mode, site.ModeRedirect)
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			return runAliasJob(cmd, e, &f, args, "site.alias.add",
				func(ctx context.Context, s *core.Sites, d, n validate.Domain) (*core.NamesOp, error) {
					return s.PrepareAliasAdd(ctx, d, n, m)
				})
		},
	}
	c.Flags().StringVar(&mode, "mode", "", "redirect (mặc định) hoặc serve")
	f.flags(c)
	return c
}

func siteAliasSetCmd(e *env) *cobra.Command {
	var (
		f    planRun
		mode string
	)
	c := &cobra.Command{
		Use:   "set <domain> <name> --mode serve|redirect",
		Short: "Đổi kiểu của một tên phụ: serve (phục vụ cùng nội dung) hay redirect (301 về tên chính)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := parseAliasModeArg(mode, "")
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			return runAliasJob(cmd, e, &f, args, "site.alias.mode",
				func(ctx context.Context, s *core.Sites, d, n validate.Domain) (*core.NamesOp, error) {
					return s.PrepareAliasMode(ctx, d, n, m)
				})
		},
	}
	c.Flags().StringVar(&mode, "mode", "", "serve hoặc redirect (bắt buộc)")
	f.flags(c)
	return c
}

func siteAliasRmCmd(e *env) *cobra.Command {
	var f planRun
	c := &cobra.Command{
		Use:   "rm <domain> <name>",
		Short: "Gỡ một tên phụ khỏi site (tên đang nằm trong chứng chỉ thì chứng chỉ được xin lại không có nó)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAliasJob(cmd, e, &f, args, "site.alias.rm",
				func(ctx context.Context, s *core.Sites, d, n validate.Domain) (*core.NamesOp, error) {
					return s.PrepareAliasRemove(ctx, d, n)
				})
		},
	}
	f.flags(c)
	return c
}

// runAliasJob đọc <domain> <name> rồi chạy thao tác tên phụ.
func runAliasJob(cmd *cobra.Command, e *env, f *planRun, args []string, action string,
	prepare func(context.Context, *core.Sites, validate.Domain, validate.Domain) (*core.NamesOp, error)) error {
	d, err := parseDomainArg(args[0])
	if err != nil {
		return fail(e, f.asJSON, err)
	}
	n, err := parseDomainArg(args[1])
	if err != nil {
		return fail(e, f.asJSON, err)
	}
	return runNamesOp(cmd, e, f, d, action, func(ctx context.Context, s *core.Sites) (*core.NamesOp, error) {
		return prepare(ctx, s, d, n)
	})
}

// runNamesOp là runSiteJob cho NamesOp (tên phụ và luật chuyển hướng): NamesOp
// không lộ engine.Plan, nên bộ in tiến độ dựng từ bản xem trước (luôn tính, kể
// cả khi --yes). Đầu vào đã kiểm xong trước khi gọi.
func runNamesOp(cmd *cobra.Command, e *env, f *planRun, d validate.Domain, action string,
	prepare func(context.Context, *core.Sites) (*core.NamesOp, error)) error {
	ctx := cmd.Context()
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

	op, err := prepare(ctx, s)
	if err != nil {
		return fail(e, f.asJSON, nameRefusalCLI(err))
	}
	pv, err := op.Preview(ctx)
	if err != nil {
		return fail(e, f.asJSON, nameRefusalCLI(err))
	}
	if ok, err := f.previewOrConfirm(e, func() (engine.Preview, error) { return pv, nil }); !ok {
		return err
	}

	out := e.stdout
	if f.asJSON {
		out = e.stderr
	}
	res, err := op.Execute(ctx, previewProgress(out, e.lang, f.verbose, pv).onEvent)
	cliAudit(ctx, e, s.Store, action, d.String(), res.Report.Job)
	if err != nil && res.Report.Job.ID == "" {
		return fail(e, f.asJSON, nameRefusalCLI(err))
	}
	if f.asJSON {
		if werr := writeJSON(e.stdout, res); werr != nil {
			return werr
		}
	} else {
		printReport(e, res.Report)
		for _, note := range res.Notes {
			printf(e.stdout, "%s\n", note.In(e.lang))
		}
		if res.FollowUpJob != "" {
			printf(e.stdout, "%s %s\n", e.tr("Job SSL chạy tiếp:", "Follow-up SSL job:"), res.FollowUpJob)
		}
	}
	if err != nil {
		return err
	}
	if res.Report.Job.Status != engine.JobSucceeded {
		return &exitCode{1}
	}
	return nil
}

func siteAliasListCmd(e *env) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "list <domain>",
		Short: "Liệt kê tên chính và tên phụ của site, kèm DNS và chứng chỉ (tra lúc gọi)",
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
			list, err := s.NameList(cmd.Context(), d)
			if err != nil {
				return fail(e, asJSON, nameRefusalCLI(err))
			}
			if asJSON {
				if list == nil {
					list = []core.NameView{}
				}
				return writeJSON(e.stdout, list)
			}
			printNameList(e, list)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
	return c
}

func printNameList(e *env, list []core.NameView) {
	w := e.stdout
	printf(w, "%-40s %-8s %-9s %-8s %-8s %s\n", e.tr("TÊN", "NAME"), e.tr("LOẠI", "KIND"), e.tr("KIỂU", "MODE"),
		e.tr("TRẠNG THÁI", "STATE"), "DNS", e.tr("CHỨNG CHỈ", "CERT"))
	for _, v := range list {
		cert := e.tr("không", "no")
		if v.InCert {
			cert = e.tr("có", "yes")
		}
		printf(w, "%-40s %-8s %-9s %-8s %-8s %s\n", v.Name, v.Kind, v.Mode, v.State, v.DNS, cert)
	}
}

func siteRedirectCmd(e *env) *cobra.Command {
	c := &cobra.Command{
		Use:   "redirect",
		Short: "Luật chuyển hướng theo đường dẫn của site: thêm, gỡ, liệt kê",
	}
	c.AddCommand(siteRedirectAddCmd(e), siteRedirectRmCmd(e), siteRedirectListCmd(e))
	return c
}

// parseRedirectArgs kiểm nguồn, đích, mã trước khi chạm máy hay DB. Core kiểm
// lại (vòng lặp, trùng, trần 50: những luật cần biết site).
func parseRedirectArgs(source, target string, code int) (core.RedirectInput, error) {
	src, err := validate.ParseRedirectSource(source)
	if err != nil {
		return core.RedirectInput{}, asRefusal(err)
	}
	tgt, err := validate.ParseRedirectTarget(target)
	if err != nil {
		return core.RedirectInput{}, asRefusal(err)
	}
	if code != 301 && code != 302 {
		return core.RedirectInput{}, &core.RefusedError{Code: validate.CodeRedirectInvalid, Message: i18n.T(
			"--code phải là 301 hoặc 302", "--code must be 301 or 302")}
	}
	return core.RedirectInput{Source: src.String(), Target: tgt.String(), Code: code}, nil
}

func siteRedirectAddCmd(e *env) *cobra.Command {
	var (
		f         planRun
		code      int
		keepQuery bool
	)
	c := &cobra.Command{
		Use:   "add <domain> <source> <target> [--code 301|302] [--keep-query]",
		Short: "Thêm luật chuyển hướng: <source> (đường dẫn, khớp đúng) sang <target> (đường dẫn hay URL http/https)",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, err := parseDomainArg(args[0])
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			in, err := parseRedirectArgs(args[1], args[2], code)
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			in.KeepQuery = keepQuery
			return runNamesOp(cmd, e, &f, d, "site.redirect.add",
				func(ctx context.Context, s *core.Sites) (*core.NamesOp, error) {
					return s.PrepareRedirectAdd(ctx, d, in)
				})
		},
	}
	c.Flags().IntVar(&code, "code", 301, "mã chuyển hướng: 301 (vĩnh viễn) hoặc 302 (tạm)")
	c.Flags().BoolVar(&keepQuery, "keep-query", false, "giữ phần ?query của request khi chuyển (đích không được có query)")
	f.flags(c)
	return c
}

func siteRedirectRmCmd(e *env) *cobra.Command {
	var f planRun
	c := &cobra.Command{
		Use:   "rm <domain> <id>",
		Short: "Gỡ một luật chuyển hướng theo số id (xem bằng: tpan site redirect list)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, err := parseDomainArg(args[0])
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			id, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil || id <= 0 {
				return fail(e, f.asJSON, &core.RefusedError{Code: "redirect_not_found", Message: i18n.T(
					"id luật phải là số dương (xem bằng: tpan site redirect list <domain>)",
					"the rule id must be a positive number (see: tpan site redirect list <domain>)")})
			}
			return runNamesOp(cmd, e, &f, d, "site.redirect.rm",
				func(ctx context.Context, s *core.Sites) (*core.NamesOp, error) {
					return s.PrepareRedirectRemove(ctx, d, id)
				})
		},
	}
	f.flags(c)
	return c
}

// redirectRow là một dòng của `redirect list --json`: cùng trường với
// GET /api/v1/sites/{domain}/redirects.
type redirectRow struct {
	ID        int64  `json:"id"`
	Source    string `json:"source"`
	Target    string `json:"target"`
	Code      int    `json:"code"`
	KeepQuery bool   `json:"keep_query"`
	State     string `json:"state"`
}

func siteRedirectListCmd(e *env) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "list <domain>",
		Short: "Liệt kê luật chuyển hướng của site",
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
			rules, err := s.RedirectList(cmd.Context(), d)
			if err != nil {
				return fail(e, asJSON, nameRefusalCLI(err))
			}
			rows := make([]redirectRow, 0, len(rules))
			for _, x := range rules {
				rows = append(rows, redirectRow{ID: x.ID, Source: x.Source.String(), Target: x.Target.String(),
					Code: x.Code, KeepQuery: x.KeepQuery, State: "active"})
			}
			if asJSON {
				return writeJSON(e.stdout, rows)
			}
			printRedirectList(e, rows)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
	return c
}

func printRedirectList(e *env, rows []redirectRow) {
	w := e.stdout
	printf(w, "%-6s %-32s %-44s %-5s %s\n", "ID", e.tr("NGUỒN", "SOURCE"), e.tr("ĐÍCH", "TARGET"),
		e.tr("MÃ", "CODE"), e.tr("GIỮ QUERY", "KEEP QUERY"))
	for _, r := range rows {
		keep := e.tr("không", "no")
		if r.KeepQuery {
			keep = e.tr("có", "yes")
		}
		printf(w, "%-6d %-32s %-44s %-5d %s\n", r.ID, r.Source, r.Target, r.Code, keep)
	}
}

// warnExportOmitted (S21): file export chưa mang tên phụ và luật chuyển hướng,
// nên báo ra stderr (stdout là file) khi có site mang chúng. Không có thì im.
func warnExportOmitted(e *env, o core.ExportOmitted) {
	if len(o.NameSites) > 0 {
		list := strings.Join(o.NameSites, ", ")
		printf(e.stderr, "%s\n", e.tr(
			"tpan: cảnh báo: file export chưa mang tên phụ. Site có tên phụ: "+list+". Thêm lại bằng: tpan site alias add",
			"tpan: warning: the export file does not carry secondary names. Sites with secondary names: "+list+". Add them back with: tpan site alias add"))
	}
	if len(o.RuleSites) > 0 {
		list := strings.Join(o.RuleSites, ", ")
		printf(e.stderr, "%s\n", e.tr(
			"tpan: cảnh báo: file export chưa mang luật chuyển hướng. Site có luật: "+list+". Thêm lại bằng: tpan site redirect add",
			"tpan: warning: the export file does not carry redirect rules. Sites with rules: "+list+". Add them back with: tpan site redirect add"))
	}
}
