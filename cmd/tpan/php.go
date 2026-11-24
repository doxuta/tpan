package main

// tpan php: nhiều bản PHP chạy song song, mỗi site chọn bản của mình (mục 6.11).

import (
	"context"
	"errors"
	"strings"

	"github.com/spf13/cobra"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/i18n"
)

func init() { register(phpCmd) }

func phpCmd(e *env) *cobra.Command {
	c := &cobra.Command{
		Use:   "php",
		Short: "Xem, cài, gỡ bản PHP và chọn bản PHP cho từng site",
	}
	c.AddCommand(phpListCmd(e), phpInstallCmd(e), phpUseCmd(e), phpRemoveCmd(e))
	return c
}

// phpPlanFlags đăng ký bốn cờ chung (mục 9) cho một lệnh chạy core.Planned.
func phpPlanFlags(c *cobra.Command, f *planFlags) {
	fl := c.Flags()
	fl.BoolVar(&f.dryRun, "dry-run", false, "chỉ in kế hoạch, không thay đổi gì")
	fl.BoolVar(&f.yes, "yes", false, "không hỏi xác nhận")
	fl.BoolVar(&f.asJSON, "json", false, "in kết quả dạng JSON")
	fl.BoolVar(&f.verbose, "verbose", false, "in lệnh và output của mọi bước")
}

// openPHP dựng core.PHP trên cùng DB với site (openSites): --dry-run mà chưa
// có file DB thì không tạo, chỉ đọc như bảng rỗng.
func openPHP(ctx context.Context, e *env, dryRun bool) (*core.PHP, any, func(), error) {
	s, done, err := openSites(ctx, e, dryRun)
	if err != nil {
		return nil, nil, nil, err
	}
	return &core.PHP{Sys: s.Sys, Settings: s.Settings, Sites: s.Store, Journal: s.Journal, Accounts: s.Accounts}, s.Store, done, nil
}

// runPHPPlan là phần chung của install, use, remove: hỏi root (trừ --dry-run),
// mở DB, dựng Plan, rồi runPlanned. php_same_version không phải lỗi: in "không
// đổi gì" và thoát 0.
func runPHPPlan(cmd *cobra.Command, e *env, f planFlags, action, target string, prepare func(context.Context, *core.PHP) (*core.Planned, error)) error {
	ctx := cmd.Context()
	if !f.dryRun {
		if err := requireRoot(e, cmd); err != nil {
			return fail(e, f.asJSON, err)
		}
	}
	p, journal, done, err := openPHP(ctx, e, f.dryRun)
	if err != nil {
		return err
	}
	defer done()
	planned, err := prepare(ctx, p)
	if re := (*core.RefusedError)(nil); errors.As(err, &re) && re.Code == "php_same_version" {
		if f.asJSON {
			return writeJSON(e.stdout, map[string]any{"changed": false, "message": re.Message})
		}
		printf(e.stdout, "%s\n", re.Message.In(e.lang))
		return nil
	}
	if err != nil {
		return fail(e, f.asJSON, err)
	}
	_, err = runPlanned(ctx, e, f, journal, action, target, planned)
	return err
}

func phpInstallCmd(e *env) *cobra.Command {
	var f planFlags
	c := &cobra.Command{
		Use:   "install <bản>",
		Short: "Cài một bản PHP-FPM (ví dụ 8.4) chạy song song với bản đang có",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			v := args[0]
			return runPHPPlan(cmd, e, f, "php.install", v, func(ctx context.Context, p *core.PHP) (*core.Planned, error) {
				return p.PrepareInstall(ctx, v)
			})
		},
	}
	phpPlanFlags(c, &f)
	return c
}

func phpUseCmd(e *env) *cobra.Command {
	var f planFlags
	c := &cobra.Command{
		Use:   "use <domain> <bản>",
		Short: "Chuyển một site sang bản PHP khác, không gián đoạn (lỗi thì site ở lại bản cũ)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, err := parseDomainArg(args[0])
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			v := args[1]
			return runPHPPlan(cmd, e, f, "site.php", d.String(), func(ctx context.Context, p *core.PHP) (*core.Planned, error) {
				return p.PrepareUse(ctx, d, v)
			})
		},
	}
	phpPlanFlags(c, &f)
	return c
}

func phpRemoveCmd(e *env) *cobra.Command {
	var f planFlags
	c := &cobra.Command{
		Use:   "remove <bản>",
		Short: "Gỡ một bản PHP không còn site nào dùng (giữ file cấu hình)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			v := args[0]
			return runPHPPlan(cmd, e, f, "php.remove", v, func(ctx context.Context, p *core.PHP) (*core.Planned, error) {
				return p.PrepareRemove(ctx, v)
			})
		},
	}
	phpPlanFlags(c, &f)
	return c
}

func phpListCmd(e *env) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "list",
		Short: "Liệt kê bản PHP đã cài và có trong kho, kèm hạn hỗ trợ",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, asJSON, err)
			}
			p, _, done, err := openPHP(cmd.Context(), e, false)
			if err != nil {
				return err
			}
			defer done()
			list, err := p.List(cmd.Context(), true)
			if err != nil {
				return fail(e, asJSON, err)
			}
			if asJSON {
				return writeJSON(e.stdout, list)
			}
			printPHPList(e, list)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
	return c
}

func printPHPList(e *env, list core.PHPList) {
	w := e.stdout
	yes, no := e.tr("có", "yes"), "-"
	mark := func(b bool) string {
		if b {
			return yes
		}
		return no
	}
	printf(w, "%-7s %-7s %-9s %-5s %-7s %s\n", e.tr("BẢN", "VERSION"), e.tr("ĐÃ CÀI", "INSTALL"), e.tr("MẶC ĐỊNH", "DEFAULT"), "SITE", e.tr("NGUỒN", "SOURCE"), e.tr("HỖ TRỢ", "SUPPORT"))
	for _, r := range list.Versions {
		printf(w, "%-7s %-7s %-9s %-5d %-7s %s\n", r.Version, mark(r.Installed), mark(r.Default), len(r.Sites), r.Source, phpSupportText(e, r.Support))
	}
	if list.WPRecommended != "" {
		printf(w, "\n%s %s\n", e.tr("WordPress.org khuyên dùng PHP", "WordPress.org recommends PHP"), list.WPRecommended)
	}
	if !list.NotesOK {
		printf(w, "%s\n", e.tr(
			"(Không hỏi được php.net hoặc WordPress.org nên thiếu ghi chú; danh sách bản vẫn đủ.)",
			"(Could not reach php.net or WordPress.org, so some notes are missing; the version list is complete.)"))
	}
	if !list.SuryEnabled {
		printf(w, "%s\n", e.tr(
			"Danh sách chỉ có bản của Ubuntu. Cài một bản khác (sudo tpan php install <bản>) sẽ thêm kho PHP của Ondřej Surý (packages.sury.org).",
			"The list only has Ubuntu's versions. Installing another version (sudo tpan php install <version>) adds Ondřej Surý's PHP repository (packages.sury.org)."))
	}
}

// phpSupportText: "còn vá bảo mật tới 2027-12-31", "hết hỗ trợ", hoặc "-".
func phpSupportText(e *env, s *core.PHPSupport) string {
	if s == nil {
		return "-"
	}
	switch s.State {
	case "eol":
		return e.tr("hết hỗ trợ", "end of life")
	case "security":
		return i18n.T("chỉ còn vá bảo mật tới "+s.SecurityUntil, "security fixes only until "+s.SecurityUntil).In(e.lang)
	}
	return strings.TrimSpace(i18n.T("hỗ trợ đủ tới "+s.ActiveUntil+", vá bảo mật tới "+s.SecurityUntil,
		"active support until "+s.ActiveUntil+", security until "+s.SecurityUntil).In(e.lang))
}
