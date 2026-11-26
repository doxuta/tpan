package main

// `tpan apply -f sites.yaml [--prune]` và `tpan export` (mục 7 của
// docs/KE-HOACH.md). Phần việc nằm ở core (apply.go, sitesfile.go); ở đây chỉ
// đọc file, hỏi lại, in tiến độ và in bảng kết quả từng site.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/validate"
)

func init() { register(applyCmd, exportCmd) }

// readSitesFile đọc và kiểm sites.yaml; "-" là stdin. Chỉ đọc file người dùng
// chỉ ra, không chạm máy: gọi trước requireRoot như parseDomainArg, để file sai
// bị từ chối trước mọi việc khác. Giới hạn cỡ nằm ở ParseSitesFile (đọc qua
// LimitReader), nên `-f /dev/zero` không làm tpan đọc vô hạn.
func readSitesFile(e *env, path string) ([]core.ApplySite, error) {
	r := e.stdin
	if path != "-" {
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("mở %s: %w", path, err)
		}
		defer func() { _ = f.Close() }()
		r = f
	}
	sf, err := core.ParseSitesFile(r)
	if err != nil {
		return nil, err
	}
	return sf.Resolve()
}

func applyCmd(e *env) *cobra.Command {
	var (
		file  string
		prune bool
		f     planRun
	)
	c := &cobra.Command{
		Use:   "apply -f <sites.yaml|-> [--prune]",
		Short: "Đưa máy về đúng danh sách site trong file: tạo site thiếu; --prune thì site ngoài danh sách vào thùng rác",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			list, err := readSitesFile(e, file)
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			// File đến từ stdin thì stdin đã hết: câu hỏi "Chạy? [y/N]" chỉ đọc
			// được EOF. Nói rõ thay vì huỷ lặng lẽ.
			if file == "-" && !f.dryRun && !f.yes {
				return fail(e, f.asJSON, &core.RefusedError{Code: "confirm_required", Message: i18n.T(
					"danh sách đọc từ stdin nên không hỏi xác nhận được; xem trước bằng --dry-run rồi chạy lại với --yes",
					"the list is read from stdin, so there is no way to ask for confirmation; preview with --dry-run, then run again with --yes")})
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

			ap, err := s.PrepareApply(ctx, core.ApplyInput{Sites: list, Prune: prune})
			if err != nil {
				return fail(e, f.asJSON, err)
			}
			if ap.Empty() {
				// --dry-run --json luôn in ApplyPreview (preview vắng khi Empty),
				// để script đọc một dạng duy nhất.
				if f.asJSON && f.dryRun {
					return writeJSON(e.stdout, core.ApplyPreview{Items: ap.Items})
				}
				if f.asJSON {
					return writeJSON(e.stdout, applyJSONOf(core.ApplyResult{Items: ap.Items, Created: []core.ApplySiteResult{}}))
				}
				printApplyItems(e, ap.Items)
				printf(e.stdout, "\n%s\n", e.tr("Không có gì thay đổi.", "Nothing to change."))
				return nil
			}
			if f.dryRun {
				pv, err := ap.Preview(ctx)
				if err != nil {
					return err
				}
				if f.asJSON {
					return writeJSON(e.stdout, pv)
				}
				printApplyItems(e, pv.Items)
				printf(e.stdout, "\n")
				printPreview(e, *pv.Preview)
				printApplySSLNote(e, ap.Items)
				return nil
			}
			if !f.asJSON {
				printApplyItems(e, ap.Items)
				printf(e.stdout, "\n")
			}
			preview := func() (engine.Preview, error) {
				pv, err := ap.Preview(ctx)
				if err != nil {
					return engine.Preview{}, err
				}
				return *pv.Preview, nil
			}
			if ok, err := f.previewOrConfirm(e, preview); !ok {
				return err
			}

			pr := f.progress(e, batchPlan(ap.Batch))
			res, err := ap.Execute(ctx, func(ev engine.Event) { applyEvent(pr, ev) })
			if res.Report != nil {
				cliAudit(ctx, e, s.Store, "apply", res.Report.Job.Target, res.Report.Job)
			}
			for _, r := range res.SSL {
				cliAudit(ctx, e, s.Store, "site.ssl", r.Job.Target, r.Job)
			}
			if err != nil && res.Report == nil {
				return err
			}
			if f.asJSON {
				if werr := writeJSON(e.stdout, applyJSONOf(res)); werr != nil {
					return werr
				}
			} else {
				// Mật khẩu in TRƯỚC phần SSL và lỗi: mất nó là mất tài khoản quản trị.
				printReport(e, res.Report.Report)
				printApplyUnits(e, res.Report.Units)
				printApplyIsolation(e, res.Isolation)
				printApplyPasswords(e, res.Created)
				printApplySSL(e, res)
			}
			if err != nil {
				return err
			}
			if !applyOK(res) {
				return &exitCode{1}
			}
			return nil
		},
	}
	fl := c.Flags()
	fl.StringVarP(&file, "file", "f", "", "file sites.yaml (\"-\" là đọc từ stdin)")
	fl.BoolVar(&prune, "prune", false, "đưa vào thùng rác các site đang chạy mà không có trong file")
	_ = c.MarkFlagRequired("file")
	f.flags(c)
	return c
}

// applyOK: mọi site trong job thành công và pha D không có site nào lỗi.
// Dòng bị từ chối (đã in trong bảng) không tính: chúng không chạy, và mục 7
// coi "đã có" hay "không làm được" là một phần của bản kế hoạch.
func applyOK(res core.ApplyResult) bool {
	if res.Report == nil {
		return true
	}
	if res.Report.Job.Status != engine.JobSucceeded {
		return false
	}
	for _, u := range res.Report.Units {
		if u.Status != engine.UnitSucceeded {
			return false
		}
	}
	for _, c := range res.Created {
		if c.SSLError != nil {
			return false
		}
	}
	for _, r := range res.SSL {
		if r.Job.Status != engine.JobSucceeded {
			return false
		}
	}
	return true
}

// batchPlan trải mọi bước của Batch thành một Plan phẳng, chỉ để progress có
// tiêu đề của từng bước (nó đọc tiêu đề theo StepID).
func batchPlan(b engine.Batch) engine.Plan {
	p := engine.Plan{Kind: b.Kind, Target: b.Target, Title: b.Title}
	p.Steps = append(p.Steps, b.Pre...)
	for _, u := range b.Units {
		p.Steps = append(p.Steps, u.Prepare...)
	}
	p.Steps = append(p.Steps, b.Commit...)
	for _, u := range b.Units {
		p.Steps = append(p.Steps, u.Finish...)
	}
	return p
}

// applyEvent in tiến độ như mọi job, thêm một dòng khi một site có kết quả
// (sự kiện unit.done), vì các site chạy song song nên dòng của chúng xen kẽ.
func applyEvent(p *progress, ev engine.Event) {
	if ev.Kind != engine.EventUnitDone {
		p.onEvent(ev)
		return
	}
	var ur engine.UnitResult
	if err := json.Unmarshal([]byte(ev.Message), &ur); err != nil {
		return
	}
	printf(p.w, "== %s: %s\n", ev.StepID, unitStatusText(p.lang, ur.Status))
}

func unitStatusText(lang string, s engine.UnitStatus) string {
	t := i18n.Text{}
	switch s {
	case engine.UnitSucceeded:
		t = i18n.T("xong", "done")
	case engine.UnitRolledBack:
		t = i18n.T("lỗi, đã hoàn tác", "failed, rolled back")
	case engine.UnitUndoFailed:
		t = i18n.T("lỗi, hoàn tác cũng lỗi", "failed, undo failed too")
	case engine.UnitNotRun:
		t = i18n.T("chưa chạy", "not run")
	default:
		return string(s)
	}
	return t.In(lang)
}

func applyActionText(e *env, it core.ApplyItem) string {
	switch it.Action {
	case core.ApplyCreate:
		if it.SSL {
			return e.tr("tạo + SSL", "create + SSL")
		}
		return e.tr("tạo", "create")
	case core.ApplySkip:
		return e.tr("bỏ qua (đã có)", "skip (exists)")
	case core.ApplyTrash:
		return e.tr("vào thùng rác", "to trash")
	case core.ApplyRefused:
		return e.tr("từ chối", "refused")
	}
	return string(it.Action)
}

// printApplyItems in bản kế hoạch: mỗi domain một dòng, việc sẽ làm và lý do.
func printApplyItems(e *env, items []core.ApplyItem) {
	w := e.stdout
	printf(w, "%-40s %-16s %s\n", "DOMAIN", e.tr("VIỆC", "ACTION"), e.tr("LÝ DO", "REASON"))
	for _, it := range items {
		reason := ""
		if it.Action == core.ApplyRefused {
			reason = it.Reason.In(e.lang) + " (" + it.Code + ")"
		}
		printf(w, "%-40s %-16s %s\n", it.Domain, applyActionText(e, it), reason)
	}
}

// printApplySSLNote: bản xem trước không có Plan SSL (site chưa có trong
// bảng sites), nên nói trước để người đọc không tưởng pha D bị bỏ.
func printApplySSLNote(e *env, items []core.ApplyItem) {
	for _, it := range items {
		if it.Action == core.ApplyCreate && it.SSL {
			printf(e.stdout, "\n%s\n", e.tr(
				"Site có ssl: tạo xong thì tpan chạy tiếp Plan xin SSL cho từng site, lần lượt (xem trước bằng `tpan ssl issue <domain> --dry-run` khi site đã có).",
				"Sites with ssl: once created, tpan runs the SSL Plan for each of them in turn (preview it with `tpan ssl issue <domain> --dry-run` once the site exists)."))
			return
		}
	}
}

// printApplyUnits in bảng kết quả từng site (mục 7: xong, lỗi ở bước nào, vì sao).
func printApplyUnits(e *env, units []engine.UnitResult) {
	w := e.stdout
	printf(w, "\n%-40s %-24s %s\n", "DOMAIN", e.tr("KẾT QUẢ", "RESULT"), e.tr("LỖI", "ERROR"))
	for _, u := range units {
		// not_run (phần chung trước lô lỗi) không có pha hay lỗi riêng: lỗi
		// chung đã in ở dòng "Lỗi:" của job.
		var parts []string
		if u.Phase != "" {
			parts = append(parts, u.Phase)
		}
		if u.StepID != "" {
			parts = append(parts, u.StepID)
		}
		msg := strings.Join(parts, " / ")
		if u.Error != "" {
			msg += ": " + u.Error
		}
		printf(w, "%-40s %-24s %s\n", u.Target, unitStatusText(e.lang, u.Status), msg)
	}
}

// printApplyIsolation chỉ in phép kiểm cô lập không đạt: đạt hết thì một dòng.
func printApplyIsolation(e *env, rs []checks.Result) {
	if len(rs) == 0 {
		return
	}
	bad := 0
	for _, r := range rs {
		if r.Status != checks.Pass {
			if bad == 0 {
				printf(e.stdout, "\n%s\n", e.tr("Cô lập giữa các site mới:", "Isolation between the new sites:"))
			}
			bad++
			printResult(e.stdout, e.lang, r, "  ")
		}
	}
	if bad == 0 {
		printf(e.stdout, "\n%s\n", e.tr(
			fmt.Sprintf("Cô lập giữa các site mới: %d/%d phép kiểm đạt.", len(rs), len(rs)),
			fmt.Sprintf("Isolation between the new sites: %d/%d checks passed.", len(rs), len(rs))))
	}
}

// printApplyPasswords in mật khẩu quản trị của từng site vừa cài WordPress,
// đúng một lần, như site add.
func printApplyPasswords(e *env, created []core.ApplySiteResult) {
	for _, c := range created {
		if c.DBPassword.Reveal() != "" {
			printf(e.stdout, "\n%s %s\n", e.tr("Site PHP trống", "Empty PHP site"), c.Domain)
			printDBPassword(e, c.DBName, c.DBPassword)
		}
		if !c.WPInstalled {
			continue
		}
		// Domain đã qua validate ở Resolve và PrepareApply: parse lại không lỗi.
		d, _ := validate.ParseDomain(c.Domain)
		printAdminPassword(e, core.SiteAddResult{
			Site:        site.Site{Domain: d},
			WPInstalled: true, AdminUser: c.AdminUser, AdminPassword: c.AdminPassword,
		})
	}
}

// printApplySSL in kết quả pha D: lời từ chối của từng site, rồi từng job SSL.
func printApplySSL(e *env, res core.ApplyResult) {
	for _, c := range res.Created {
		if c.SSLError != nil {
			printf(e.stderr, "SSL %s: ", c.Domain)
			printRefusal(e, c.SSLError)
		}
	}
	for _, r := range res.SSL {
		printf(e.stdout, "\nSSL %s:", r.Job.Target)
		printReport(e, r)
	}
}

// applySiteJSON là một dòng Created của --json: mật khẩu quản trị dạng chữ
// thật, đúng một lần, chỉ khi WordPress vừa được cài (như siteAddJSON).
type applySiteJSON struct {
	Domain        string             `json:"domain"`
	Status        engine.UnitStatus  `json:"status"`
	WPInstalled   bool               `json:"wp_installed"`
	AdminUser     string             `json:"admin_user,omitempty"`
	AdminPassword string             `json:"admin_password,omitempty"`
	SSLError      *core.RefusedError `json:"ssl_error,omitempty"`
}

// applyJSON là output --json của apply: core.ApplyResult với Created lộ mật khẩu.
type applyJSON struct {
	Items     []core.ApplyItem    `json:"items"`
	Report    *engine.BatchReport `json:"report,omitempty"`
	Created   []applySiteJSON     `json:"created"`
	Isolation []checks.Result     `json:"isolation,omitempty"`
	SSL       []engine.Report     `json:"ssl,omitempty"`
}

func applyJSONOf(res core.ApplyResult) applyJSON {
	out := applyJSON{Items: res.Items, Report: res.Report, Isolation: res.Isolation, SSL: res.SSL, Created: []applySiteJSON{}}
	for _, c := range res.Created {
		j := applySiteJSON{Domain: c.Domain, Status: c.Status, WPInstalled: c.WPInstalled, SSLError: c.SSLError}
		if c.WPInstalled {
			j.AdminUser, j.AdminPassword = c.AdminUser, c.AdminPassword.Reveal()
		}
		out.Created = append(out.Created, j)
	}
	return out
}

func exportCmd(e *env) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "export",
		Short: "In danh sách site đang chạy dạng sites.yaml (đưa lại vào tpan apply -f được)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, asJSON, err)
			}
			return runExport(cmd.Context(), e, asJSON)
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "in dạng JSON (cùng trường với sites.yaml)")
	return c
}

func runExport(ctx context.Context, e *env, asJSON bool) error {
	s, done, err := openSites(ctx, e, false)
	if err != nil {
		return err
	}
	defer done()
	sf, err := s.Export(ctx)
	if err != nil {
		return err
	}
	warnExportOmitted(e, sf.Omitted) // M19 S21: tên phụ và luật chuyển hướng chưa có trong file
	if asJSON {
		return writeJSON(e.stdout, sf)
	}
	out, err := sf.Marshal()
	if err != nil {
		return err
	}
	_, err = e.stdout.Write(out)
	return err
}
