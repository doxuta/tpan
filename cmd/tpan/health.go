package main

// Lệnh `tpan health` (mục 6.7 và 9 của docs/KE-HOACH.md). Lệnh chạy
// health.Runner y như bộ hẹn giờ của `tpan serve`: kiểm máy và stack, mọi
// site, file do tpan quản lý (lệch) và chính tpan, ghi kết quả và mở, đóng sự
// cố. Nhờ vậy chạy tay một lần là sự cố đã có trên giao diện, e2e không phải
// chờ vòng 5 phút.
//
// --fix dùng đúng đường của giao diện (core.Fixes): mỗi lần sửa là một Plan
// có xem trước, xác nhận, kiểm chứng và hoàn tác, được ghi vào lịch sử job,
// nhật ký và gắn vào sự cố của phép kiểm đó.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/health"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/validate"
)

func init() { register(healthCmd) }

func healthCmd(e *env) *cobra.Command {
	var asJSON, fix, dryRun, yes bool
	c := &cobra.Command{
		Use:   "health [<domain>] [--fix [--dry-run] [--yes]]",
		Short: "Chạy các phép kiểm, ghi kết quả, mở và đóng sự cố; --fix sửa tự động (mã thoát 1 khi có lỗi)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			var d validate.Domain
			if len(args) == 1 {
				var err error
				if d, err = parseDomainArg(args[0]); err != nil {
					return fail(e, asJSON, err)
				}
			}
			// Không có --fix thì lệnh không thay đổi máy: --dry-run, --yes vô
			// nghĩa, báo luôn để người gõ không tưởng là đã sửa gì.
			if (dryRun || yes) && !fix {
				return errors.New("--dry-run và --yes chỉ dùng cùng --fix")
			}
			if err := requireRoot(e, cmd); err != nil {
				return fail(e, asJSON, err)
			}
			st, err := openStore(ctx, e)
			if err != nil {
				return err
			}
			defer closeStore(st)

			h := &healthRun{e: e, st: st, asJSON: asJSON,
				runner: &health.Runner{Sys: e.sys, Store: st, DBPath: e.dbPath}}
			if !d.IsZero() {
				h.domain = d.String()
			}
			before, err := h.run(ctx)
			if err != nil {
				return fail(e, asJSON, err)
			}
			if !fix {
				if asJSON {
					if err := writeJSON(e.stdout, before); err != nil {
						return err
					}
				}
				return healthExit(before)
			}
			return h.fix(ctx, before, dryRun, yes)
		},
	}
	f := c.Flags()
	f.BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
	f.BoolVar(&fix, "fix", false, "sửa tự động các phép kiểm không đạt có cách sửa tự động")
	f.BoolVar(&dryRun, "dry-run", false, "cùng --fix: chỉ in kế hoạch sửa, không thay đổi gì")
	f.BoolVar(&yes, "yes", false, "cùng --fix: không hỏi xác nhận")
	return c
}

// healthRun là một lần chạy lệnh: chạy Runner, in, và sửa khi có --fix.
type healthRun struct {
	e      *env
	st     *store.Store
	runner *health.Runner
	domain string
	asJSON bool
}

// run chạy đủ phép kiểm (cả phép nặng như wp.checksums: người gõ tay muốn câu
// trả lời đầy đủ) và in bản chữ khi không --json.
func (h *healthRun) run(ctx context.Context) (health.Report, error) {
	rep, err := h.runner.Run(ctx, health.Options{Domain: h.domain, Full: true})
	if err != nil {
		return rep, err
	}
	if !h.asJSON {
		printHealthReport(h.e, rep)
	}
	return rep, nil
}

// healthExit: lỗi (Fail) hoặc stack chưa cài là mã 1, vì không có gì để
// khẳng định là khoẻ. Cảnh báo không làm mã 1: máy vẫn chạy được.
func healthExit(rep health.Report) error {
	if !rep.Installed || rep.HasFail() {
		return &exitCode{1}
	}
	return nil
}

// fixOutcome là kết quả sửa một phép kiểm, cho --json.
type fixOutcome struct {
	CheckID string `json:"check_id"`
	Name    string `json:"name"`
	// Refused: không sửa tự động được (no_auto_fix...).
	Refused *core.RefusedError `json:"refused,omitempty"`
	Preview *engine.Preview    `json:"preview,omitempty"`
	// Skipped: người dùng trả lời "không" ở câu hỏi xác nhận.
	Skipped bool           `json:"skipped,omitempty"`
	Report  *engine.Report `json:"report,omitempty"`
}

// healthFixJSON là output của --fix --json: kết quả trước khi sửa, từng lần
// sửa, và kết quả kiểm lại (không có khi --dry-run).
type healthFixJSON struct {
	Before health.Report  `json:"before"`
	Fixes  []fixOutcome   `json:"fixes"`
	After  *health.Report `json:"after,omitempty"`
}

// fixable chọn các kết quả có cách sửa tự động, theo thứ tự của Runner (máy và
// stack trước site). Thứ tự này có ích: sửa dịch vụ PHP-FPM trước thì lần sửa
// socket của từng site sau đó thấy socket đã có và bỏ qua, không khởi động lại
// PHP-FPM thêm lần nào.
func fixable(rep health.Report) []checks.Result {
	seen := map[string]bool{}
	var out []checks.Result
	for _, g := range rep.Groups {
		for _, r := range g.Results {
			if r.Status == checks.Pass || r.AutoFix == "" || seen[r.ID] {
				continue
			}
			seen[r.ID] = true
			out = append(out, r)
		}
	}
	return out
}

func (h *healthRun) fix(ctx context.Context, before health.Report, dryRun, yes bool) error {
	e := h.e
	out := healthFixJSON{Before: before, Fixes: []fixOutcome{}}
	todo := fixable(before)
	if len(todo) == 0 {
		if h.asJSON {
			if err := writeJSON(e.stdout, out); err != nil {
				return err
			}
		} else {
			printf(e.stdout, "\n%s\n", e.tr("Không có phép kiểm nào sửa tự động được.", "No check can be fixed automatically."))
		}
		if dryRun {
			return nil
		}
		return healthExit(before)
	}
	// Không có --yes: phải có người trả lời từng câu hỏi. Từ chối trước khi
	// dựng Plan nào, như mọi lệnh có thay đổi khác.
	if !dryRun && !yes && (h.asJSON || !e.isTerminal()) {
		return fail(e, h.asJSON, &core.RefusedError{Code: "confirm_required", Message: i18n.T(
			"không hỏi xác nhận được (không có terminal hoặc đang dùng --json); xem trước bằng --fix --dry-run rồi chạy lại với --fix --yes",
			"cannot ask for confirmation (no terminal, or --json is set); preview with --fix --dry-run, then run again with --fix --yes")})
	}

	fx := &core.Fixes{Sys: e.sys, Settings: h.st, Store: h.st, Journal: h.st, Files: h.st}
	failed := false
	for _, r := range todo {
		o, ok, err := h.fixOne(ctx, fx, r, dryRun, yes)
		if err != nil {
			return err
		}
		failed = failed || !ok
		out.Fixes = append(out.Fixes, o)
	}

	if dryRun {
		// --dry-run là "cho xem sẽ làm gì": xem được là xong việc, mã 0.
		if h.asJSON {
			return writeJSON(e.stdout, out)
		}
		return nil
	}

	if !h.asJSON {
		printf(e.stdout, "\n%s\n", e.tr("Kiểm lại sau khi sửa:", "Checking again after the fixes:"))
	}
	after, err := h.run(ctx)
	if err != nil {
		return fail(e, h.asJSON, err)
	}
	out.After = &after
	if h.asJSON {
		if err := writeJSON(e.stdout, out); err != nil {
			return err
		}
	}
	if failed {
		return &exitCode{1}
	}
	return healthExit(after)
}

// fixOne dựng, xem trước, hỏi rồi chạy cách sửa cho r. ok=false khi job sửa
// không thành công. Lỗi trả về chỉ là lỗi phải dừng cả lệnh (DB, ngữ cảnh).
func (h *healthRun) fixOne(ctx context.Context, fx *core.Fixes, r checks.Result, dryRun, yes bool) (o fixOutcome, ok bool, err error) {
	e := h.e
	o = fixOutcome{CheckID: r.ID, Name: r.AutoFix}
	x, err := fx.Prepare(ctx, r)
	if err != nil {
		var re *core.RefusedError
		if !errors.As(err, &re) {
			return o, false, err
		}
		// Một phép kiểm không sửa tự động được không phải lý do bỏ các phép
		// kiểm khác: in lời từ chối (kèm cách sửa tay trong kết quả ở trên).
		o.Refused = re
		if !h.asJSON {
			printf(e.stdout, "\n%s %s\n", e.tr("Sửa", "Fix"), r.ID)
			printRefusal(e, re)
		}
		return o, true, nil
	}
	// Xem trước ngay lúc sắp sửa, sau các lần sửa trước: bước đã được lần sửa
	// trước làm đúng hiện là "đã đúng, bỏ qua".
	pv, err := x.Preview(ctx)
	if err != nil {
		return o, false, fmt.Errorf("xem trước cách sửa %s cho %s: %w", x.Name, x.CheckID, err)
	}
	o.Preview = &pv
	if !h.asJSON {
		printf(e.stdout, "\n%s %s (%s)\n", e.tr("Sửa", "Fix"), x.CheckID, x.Name)
		printPreview(e, pv)
		printFileDiffs(e, pv)
	}
	if dryRun {
		return o, true, nil
	}
	if !yes && !confirm(e) {
		o.Skipped = true
		printf(e.stdout, "%s\n", e.tr("Bỏ qua, không thay đổi gì.", "Skipped, nothing changed."))
		return o, true, nil
	}

	// --json: stdout chỉ có JSON; tiến độ ra stderr cho người xem.
	w := e.stdout
	if h.asJSON {
		w = e.stderr
	}
	p := newProgress(w, e.lang, false, x.Plan)
	rep, err := x.Execute(ctx, p.onEvent)
	cliAudit(ctx, e, h.st, "fix."+x.Name, x.CheckID, rep.Job)
	if rep.Job.ID != "" {
		// Gắn job vào sự cố đang mở của phép kiểm, để trang sự cố cho thấy
		// "đã làm gì ở giữa" (mục 6.7). Gắn lỗi chỉ báo ra stderr: việc trên
		// máy đã xong, không được báo hỏng vì một dòng liên kết.
		if lerr := h.st.LinkIncidentJob(ctx, x.CheckID, rep.Job.ID, time.Now()); lerr != nil {
			printf(e.stderr, "tpan: %v\n", lerr)
		}
	}
	if err != nil && rep.Job.ID == "" {
		return o, false, err
	}
	o.Report = &rep
	if !h.asJSON {
		printReport(e, rep)
	}
	if err != nil {
		printf(e.stderr, "tpan: %v\n", err)
	}
	return o, err == nil && rep.Job.Status == engine.JobSucceeded, nil
}

// printFileDiffs in chỗ khác nhau giữa file đang có và bản tpan sẽ ghi, cho mọi
// file mà cách sửa sẽ đổi nội dung (mục 6.7: "file bị sửa tay: hiện diff").
// Không có nó, `tpan health --fix --dry-run` (lệnh mà phép kiểm lệch bảo người
// dùng chạy để xem diff) chỉ nói "sửa <file>". Chỉ gọi ở health --fix: file
// của cách sửa là cấu hình (vhost, pool, .htaccess, file của stack), không có
// bí mật; printPreview dùng chung cho cả setup, site add nên không in ở đó.
func printFileDiffs(e *env, pv engine.Preview) {
	for _, s := range pv.Steps {
		if s.Done {
			continue
		}
		for _, f := range s.Info.Files {
			if f.Delete || !f.OldExists || bytes.Equal(f.Old, f.New) {
				continue
			}
			printf(e.stdout, "\n    --- %s (%s)\n    +++ %s (%s)\n", f.Path, e.tr("đang có", "current"), f.Path, e.tr("tpan sẽ ghi", "tpan will write"))
			for _, l := range lineDiff(f.Old, f.New) {
				printf(e.stdout, "    %s\n", l)
			}
		}
	}
}

// diffContext là số dòng giống nhau in quanh khúc khác; maxDiffLines chặn một
// file khác hoàn toàn khỏi đổ hàng trăm dòng ra màn hình.
const (
	diffContext  = 3
	maxDiffLines = 80
)

// lineDiff trả các dòng diff giữa old và new: " " giống, "-" chỉ có ở old, "+"
// chỉ có ở new.
// ponytail: chỉ bỏ phần đầu và phần cuối giống nhau rồi in cả khúc giữa (dòng
// cũ "-", dòng mới "+"). Đúng với mọi file; với một chỗ sửa tay (trường hợp
// thường gặp) là diff tối thiểu, với nhiều chỗ cách xa nhau thì khúc giữa dài
// hơn cần. Cần diff tối thiểu thì thay bằng Myers/LCS.
func lineDiff(old, new []byte) []string {
	if bytes.Equal(old, new) {
		return nil
	}
	a, b := splitLines(old), splitLines(new)
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	var out []string
	for _, l := range a[max(0, pre-diffContext):pre] {
		out = append(out, "  "+l)
	}
	for _, l := range a[pre : len(a)-suf] {
		out = append(out, "- "+l)
	}
	for _, l := range b[pre : len(b)-suf] {
		out = append(out, "+ "+l)
	}
	for _, l := range a[len(a)-suf : min(len(a), len(a)-suf+diffContext)] {
		out = append(out, "  "+l)
	}
	if len(out) > maxDiffLines {
		out = append(out[:maxDiffLines], fmt.Sprintf("... (+%d)", len(out)-maxDiffLines))
	}
	return out
}

func splitLines(b []byte) []string {
	s := strings.TrimSuffix(string(b), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// printHealthReport in kết quả theo từng đối tượng (máy và stack, từng site,
// tpan), rồi các file bị lệch và các sự cố vừa mở, vừa đóng.
func printHealthReport(e *env, rep health.Report) {
	w := e.stdout
	if !rep.Installed {
		printf(w, "%s\n", e.tr(
			"Stack chưa cài: chỉ kiểm được máy và tpan. Cài bằng: sudo tpan stack install --web nginx|apache",
			"The stack is not installed: only the machine and tpan were checked. Install with: sudo tpan stack install --web nginx|apache"))
	}
	for _, g := range rep.Groups {
		printf(w, "\n== %s ==\n", groupName(e, g.Target))
		printHealth(e, core.Health{Installed: true, Results: g.Results})
	}

	// Lệch dễ bị lẫn giữa hàng chục dòng đạt: nhắc lại đúng
	// đường dẫn file để người đọc biết mở file nào (e2e bước 9).
	var drifted []string
	for _, g := range rep.Groups {
		for _, r := range g.Results {
			if p, ok := strings.CutPrefix(r.ID, "drift:"); ok && r.Status != checks.Pass {
				drifted = append(drifted, p)
			}
		}
	}
	if len(drifted) > 0 {
		printf(w, "\n%s\n", e.tr("File bị sửa ngoài tpan (lệch):", "Files changed outside tpan (drift):"))
		for _, p := range drifted {
			printf(w, "  %s\n", p)
		}
		printf(w, "%s\n", e.tr("Xem cách sửa: sudo tpan health --fix --dry-run", "See the fix: sudo tpan health --fix --dry-run"))
	}

	printIncidents(e, e.tr("Sự cố vừa mở:", "Incidents opened:"), rep.Changes.Opened)
	printIncidents(e, e.tr("Sự cố vừa đóng:", "Incidents closed:"), rep.Changes.Closed)
}

func groupName(e *env, target string) string {
	switch target {
	case "":
		return e.tr("Máy và stack", "Machine and stack")
	case store.TargetTPAN:
		return "tpan"
	}
	return "Site " + target
}

func printIncidents(e *env, title string, incs []store.Incident) {
	if len(incs) == 0 {
		return
	}
	printf(e.stdout, "\n%s\n", title)
	for _, in := range incs {
		line := fmt.Sprintf("  #%d [%s] %s (%s)", in.ID, in.Stage, in.Title.In(e.lang), in.CheckID)
		if in.CloseReason != "" {
			line += " " + e.tr("lý do:", "reason:") + " " + in.CloseReason
		}
		printf(e.stdout, "%s\n", line)
	}
}
