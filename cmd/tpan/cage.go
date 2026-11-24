package main

// `tpan cage status|enable|disable` (M17, mục 3.10 của
// docs/thay-doi/M17-thiet-ke.md): đưa một site vào lồng (PHP-FPM riêng, hàng rào
// file, trần tài nguyên của gói) hoặc đưa ra. `--all` và `--stale` là vòng lặp,
// mỗi site một job: hỏng một site không kéo theo site khác, mã thoát khác 0 khi
// có site hỏng.

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/validate"
)

func init() { register(cageCmd) }

func cageCmd(e *env) *cobra.Command {
	c := &cobra.Command{
		Use:   "cage",
		Short: "Lồng theo site: PHP-FPM riêng, hàng rào file và trần tài nguyên của gói",
	}
	c.AddCommand(cageStatusCmd(e), cageEnableCmd(e), cageDisableCmd(e))
	return c
}

// openCages dựng core.Cages trên cùng DB với ca sử dụng site (openSites):
// --dry-run mà chưa có file DB thì không tạo, chỉ đọc như bảng rỗng (Accounts
// nil: mọi site coi như thuộc gói default). Trả thêm store để runPlanned ghi
// nhật ký.
func openCages(ctx context.Context, e *env, dryRun bool) (*core.Cages, any, func(), error) {
	s, done, err := openSites(ctx, e, dryRun)
	if err != nil {
		return nil, nil, nil, err
	}
	return &core.Cages{Sys: s.Sys, Settings: s.Settings, Store: s.Store, Accounts: s.Accounts, Journal: s.Journal}, s.Store, done, nil
}

// runCageEdit là chỗ chung của `package edit` và `user edit`: nếu có site trong
// lồng bị ảnh hưởng (prepare trả Plan) thì chạy Plan cage.sync (dòng gói hay
// user ghi ở móc before, trước job); không thì fallback là đường cũ chỉ ghi
// tpan.db. Chưa có tpan.db thì đi thẳng đường cũ (nó từ chối setup_required).
// what là câu "Sẽ: ..." in trước bản xem trước.
func runCageEdit(cmd *cobra.Command, e *env, f planFlags, action, target string, what i18n.Text,
	prepare func(context.Context, *core.Cages) (*core.Planned, error), fallback func() error) error {
	ctx := cmd.Context()
	if err := requireRoot(e, cmd); err != nil {
		return fail(e, f.asJSON, err)
	}
	if !e.fileExists(e.dbPath) {
		return fallback()
	}
	c, journal, done, err := openCages(ctx, e, f.dryRun)
	if err != nil {
		return err
	}
	plan, err := prepare(ctx, c)
	if err != nil {
		done()
		return fail(e, f.asJSON, asRefusal(err))
	}
	if plan == nil {
		done()
		return fallback()
	}
	defer done()
	if !f.asJSON {
		printf(e.stdout, "%s\n", what.In(e.lang))
	}
	_, err = runPlanned(ctx, e, f, journal, action, target, plan)
	return err
}

// ---------------------------------------------------------------- status

func cageStatusCmd(e *env) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "status [<domain>] [--json]",
		Short: "Xem site nào đang trong lồng, gói có muốn lồng không, số liệu đang dùng",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var d validate.Domain
			if len(args) == 1 {
				var err error
				if d, err = parseDomainArg(args[0]); err != nil {
					return fail(e, asJSON, err)
				}
			}
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, asJSON, err)
			}
			// Chỉ đọc: chưa có tpan.db thì không tạo, bảng rỗng.
			cages, _, done, err := openCages(cmd.Context(), e, !e.fileExists(e.dbPath))
			if err != nil {
				return err
			}
			defer done()
			rows, err := cages.Status(cmd.Context(), d)
			if err != nil {
				return fail(e, asJSON, asRefusal(err))
			}
			if rows == nil {
				rows = []core.CageRow{}
			}
			if asJSON {
				return writeJSON(e.stdout, rows)
			}
			printCageRows(e, rows)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
	return c
}

// cageStateText là trạng thái của CageRow bằng chữ cho người đọc.
func cageStateText(e *env, state string) string {
	switch state {
	case "trong":
		return e.tr("trong lồng", "caged")
	case "ngoai":
		return e.tr("ngoài lồng", "not caged")
	case "can-vao":
		return e.tr("cần vào lồng", "should enter")
	case "can-ra":
		return e.tr("cần ra khỏi lồng", "should leave")
	}
	return state
}

func printCageRows(e *env, rows []core.CageRow) {
	w := e.stdout
	if len(rows) == 0 {
		printf(w, "%s\n", e.tr("Chưa có site nào.", "No sites yet."))
		return
	}
	printf(w, "%-32s %-14s %-12s %-18s %s\n", e.tr("SITE", "SITE"), e.tr("CHỦ", "OWNER"), e.tr("GÓI", "PACKAGE"), e.tr("TRẠNG THÁI", "STATE"), e.tr("RAM / TIẾN TRÌNH", "RAM / TASKS"))
	for _, r := range rows {
		use := "-"
		if r.Usage != nil {
			use = fmt.Sprintf("%d/%s MiB, %d/%s", r.Usage.MemCurrent>>20, maxText(r.Limits.RAMMB), r.Usage.Tasks, maxText(r.Limits.Tasks))
		}
		state := cageStateText(e, r.State)
		if r.Suspended {
			state += " (" + e.tr("tạm khoá", "suspended") + ")"
		}
		printf(w, "%-32s %-14s %-12s %-18s %s\n", r.Domain, r.Owner, r.Package, state, use)
	}
}

// ---------------------------------------------------------------- enable, disable

// cageSelect là cách chọn site của một lần chạy.
type cageSelect struct {
	all, stale bool
}

func cageEnableCmd(e *env) *cobra.Command {
	var (
		f   planFlags
		sel cageSelect
	)
	c := &cobra.Command{
		Use:   "enable <domain> | --all [--dry-run] [--yes] [--json]",
		Short: "Đưa site vào lồng (--all: mọi site mà gói muốn lồng nhưng chưa có lồng)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCage(cmd, e, f, sel, args, true)
		},
	}
	bindChangeFlags(c, &f)
	c.Flags().BoolVar(&sel.all, "all", false, "mọi site gói muốn lồng mà chưa có lồng (mỗi site một job)")
	return c
}

func cageDisableCmd(e *env) *cobra.Command {
	var (
		f   planFlags
		sel cageSelect
	)
	c := &cobra.Command{
		Use:   "disable <domain> | --all | --stale [--dry-run] [--yes] [--json]",
		Short: "Đưa site ra khỏi lồng (--all: mọi site có file của lồng; --stale: chỉ site mà gói không còn muốn lồng)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCage(cmd, e, f, sel, args, false)
		},
	}
	bindChangeFlags(c, &f)
	c.Flags().BoolVar(&sel.all, "all", false, "mọi site có file của lồng trên máy (mỗi site một job)")
	c.Flags().BoolVar(&sel.stale, "stale", false, "chỉ site có file của lồng mà gói không còn muốn lồng")
	return c
}

// cagePick chọn site theo CageRow (mục 3.10). enable --all: gói muốn mà chưa
// vào. disable --all: file của lồng có trên máy (đọc máy, không đọc cột
// caged: panel có thể bị tắt giữa cage.enable hay cage.disable). --stale: như
// --all nhưng bỏ site mà gói còn muốn lồng.
func cagePick(enable bool, sel cageSelect) func(core.CageRow) bool {
	switch {
	case enable:
		return func(r core.CageRow) bool { return r.Want && !r.Caged }
	case sel.stale:
		return func(r core.CageRow) bool { return r.Files && !r.Want }
	default:
		return func(r core.CageRow) bool { return r.Files }
	}
}

func cageChooseText(enable bool) i18n.Text {
	if enable {
		return i18n.T("chọn đúng một cách: một <domain>, hoặc --all", "choose exactly one way: a <domain>, or --all")
	}
	return i18n.T("chọn đúng một cách: một <domain>, hoặc --all, hoặc --stale", "choose exactly one way: a <domain>, or --all, or --stale")
}

// runCage kiểm đối số rồi chạy một site hoặc cả vòng lặp. Đối số sai bị từ chối
// trước khi mở DB hay đụng máy.
func runCage(cmd *cobra.Command, e *env, f planFlags, sel cageSelect, args []string, enable bool) error {
	ctx := cmd.Context()
	multi := sel.all || sel.stale
	if (len(args) == 1) == multi || (sel.all && sel.stale) {
		// Đúng một cách chọn: một <domain>, hoặc cờ chọn nhiều.
		return fail(e, f.asJSON, &core.RefusedError{Code: "args_invalid", Message: cageChooseText(enable)})
	}
	action, prepare := "cage.disable", func(c *core.Cages, d validate.Domain) (*core.Planned, error) { return c.PrepareDisable(ctx, d) }
	if enable {
		action, prepare = "cage.enable", func(c *core.Cages, d validate.Domain) (*core.Planned, error) { return c.PrepareEnable(ctx, d) }
	}

	if !multi {
		d, err := parseDomainArg(args[0])
		if err != nil {
			return fail(e, f.asJSON, err)
		}
		if !f.dryRun {
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, f.asJSON, err)
			}
		}
		c, journal, done, err := openCages(ctx, e, f.dryRun)
		if err != nil {
			return err
		}
		defer done()
		p, err := prepare(c, d)
		if err != nil {
			return fail(e, f.asJSON, asRefusal(err))
		}
		_, err = runPlanned(ctx, e, f, journal, action, d.String(), p)
		return err
	}

	if !f.dryRun {
		if err := requireRoot(e, cmd); err != nil {
			return fail(e, f.asJSON, err)
		}
	}
	c, journal, done, err := openCages(ctx, e, f.dryRun)
	if err != nil {
		return err
	}
	defer done()
	rows, err := c.Status(ctx, validate.Domain{})
	if err != nil {
		return fail(e, f.asJSON, asRefusal(err))
	}
	var picked []core.CageRow
	for _, r := range rows {
		if cagePick(enable, sel)(r) {
			picked = append(picked, r)
		}
	}
	return runCageBatch(ctx, e, f, journal, action, picked, func(d validate.Domain) (*core.Planned, error) { return prepare(c, d) })
}

// cageBatchItem là một dòng của kết quả --all và --stale với --json.
type cageBatchItem struct {
	Domain  string          `json:"domain"`
	Preview *engine.Preview `json:"preview,omitempty"` // --dry-run
	Report  *engine.Report  `json:"report,omitempty"`  // đã chạy
	Error   string          `json:"error,omitempty"`   // không dựng hay không chạy được
}

// runCageBatch xem trước mọi site, hỏi một lần (trừ --yes), rồi chạy từng site
// một job. Stdout chỉ có một tài liệu JSON khi --json (mỗi lần gọi runPlanned
// ghi một tài liệu riêng, N site thì không phải JSON hợp lệ); tiến độ ra stderr.
func runCageBatch(ctx context.Context, e *env, f planFlags, journal any, action string, rows []core.CageRow,
	prepare func(validate.Domain) (*core.Planned, error)) error {
	if len(rows) == 0 {
		if f.asJSON {
			return writeJSON(e.stdout, []cageBatchItem{})
		}
		printf(e.stdout, "%s\n", e.tr("Không có site nào cần làm.", "No site needs this."))
		return nil
	}
	type job struct {
		item cageBatchItem
		plan *core.Planned
	}
	jobs := make([]job, len(rows))
	failed := false
	for i, r := range rows {
		jobs[i].item.Domain = r.Domain
		d, err := validate.ParseDomain(r.Domain)
		if err == nil {
			jobs[i].plan, err = prepare(d)
		}
		if err != nil {
			jobs[i].item.Error = errText(e, err)
			failed = true
		}
	}

	// emit in kết quả của lượt xem trước hoặc lượt chạy: một tài liệu JSON, hoặc
	// từng site một đoạn.
	emitJSON := func() error {
		items := make([]cageBatchItem, len(jobs))
		for i := range jobs {
			items[i] = jobs[i].item
		}
		return writeJSON(e.stdout, items)
	}
	showPreviews := func() {
		for _, j := range jobs {
			printf(e.stdout, "== %s\n", j.item.Domain)
			if j.item.Preview != nil {
				printPreview(e, *j.item.Preview)
			} else {
				printf(e.stdout, "%s %s\n", e.tr("Lỗi:", "Error:"), j.item.Error)
			}
		}
	}

	if f.dryRun || !f.yes {
		for i := range jobs {
			if jobs[i].plan == nil {
				continue
			}
			pv, err := jobs[i].plan.Preview(ctx)
			if err != nil {
				jobs[i].item.Error, failed = errText(e, err), true
				jobs[i].plan = nil
				continue
			}
			jobs[i].item.Preview = &pv
		}
		if f.dryRun {
			if f.asJSON {
				if err := emitJSON(); err != nil {
					return err
				}
			} else {
				showPreviews()
			}
			if failed {
				return &exitCode{1}
			}
			return nil
		}
		if f.asJSON || !e.isTerminal() {
			return fail(e, f.asJSON, &core.RefusedError{Code: "confirm_required", Message: i18n.T(
				"không hỏi xác nhận được (không có terminal hoặc đang dùng --json); xem trước bằng --dry-run rồi chạy lại với --yes",
				"cannot ask for confirmation (no terminal, or --json is set); preview with --dry-run, then run again with --yes")})
		}
		showPreviews()
		if !confirm(e) {
			printf(e.stdout, "%s\n", e.tr("Đã huỷ, không thay đổi gì.", "Cancelled, nothing changed."))
			return &exitCode{1}
		}
	}

	out := e.stdout
	if f.asJSON {
		out = e.stderr
	}
	for i := range jobs {
		j := &jobs[i]
		if j.plan == nil {
			continue
		}
		printf(out, "\n== %s\n", j.item.Domain)
		pr := newProgress(out, e.lang, f.verbose, j.plan.Plan)
		rep, err := j.plan.Execute(ctx, pr.onEvent)
		cliAudit(ctx, e, journal, action, j.item.Domain, rep.Job)
		if rep.Job.ID != "" {
			j.item.Report = &rep
		}
		switch {
		case err != nil:
			j.item.Error = errText(e, err)
			failed = true
		case rep.Job.Status != engine.JobSucceeded:
			failed = true
		}
		if !f.asJSON && rep.Job.ID != "" {
			printReport(e, rep)
		}
	}

	if f.asJSON {
		if err := emitJSON(); err != nil {
			return err
		}
	} else {
		for _, j := range jobs {
			if j.item.Error != "" {
				printf(e.stderr, "tpan: %s: %s\n", j.item.Domain, j.item.Error)
			}
		}
	}
	if failed {
		return &exitCode{1}
	}
	return nil
}

// errText là lời của một lỗi cho người đọc: lời từ chối in theo ngôn ngữ, lỗi
// khác in nguyên văn.
func errText(e *env, err error) string {
	var re *core.RefusedError
	if errors.As(err, &re) {
		return re.Code + ": " + re.Message.In(e.lang)
	}
	return strings.TrimSpace(err.Error())
}
