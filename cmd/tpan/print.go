package main

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
)

// Các hàm in cho người đọc. Output cho script là --json, nên ở đây ưu tiên dễ
// đọc hơn dễ tách.

// printf bỏ qua lỗi ghi: stdout hay stderr đã hỏng thì không còn chỗ nào để báo.
func printf(w io.Writer, format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }

func statusWord(lang string, s checks.Status) string {
	t := map[checks.Status]i18n.Text{
		checks.Pass:    i18n.T("đạt", "pass"),
		checks.Warn:    i18n.T("cảnh báo", "warn"),
		checks.Fail:    i18n.T("lỗi", "fail"),
		checks.Unknown: i18n.T("chưa rõ", "unknown"),
	}[s]
	if t.IsZero() {
		return string(s)
	}
	return t.In(lang)
}

// printResult in một kết quả kiểm; khi không đạt thì thêm nguyên nhân và lệnh
// sửa tay, vì đó là thứ người vận hành cần ngay.
func printResult(w io.Writer, lang string, r checks.Result, indent string) {
	printf(w, "%s[%s] %s: %s\n", indent, statusWord(lang, r.Status), r.Title.In(lang), r.Summary.In(lang))
	if r.Status == checks.Pass {
		return
	}
	if !r.Cause.IsZero() {
		printf(w, "%s    %s %s\n", indent, i18n.T("Nguyên nhân:", "Cause:").In(lang), r.Cause.In(lang))
	}
	if len(r.ManualFix) > 0 {
		printf(w, "%s    %s\n", indent, i18n.T("Sửa tay (gõ trên máy này, thư mục nào cũng được):", "Manual fix (type on this machine, from any directory):").In(lang))
		for _, l := range r.ManualFix {
			printf(w, "%s      %s\n", indent, withSudo(l))
		}
	}
}

// withSudo thêm "sudo " vào lệnh sửa tay chưa có. Dữ liệu phép kiểm giữ lệnh
// trần; chỗ in ra cho người gõ mới thêm, vì người vận hành gõ bằng user thường
// trong phiên SSH, thiếu sudo thì gặp "permission denied". Giao diện web làm
// giống hệt ở web/src/components/CommandBlock.tsx.
func withSudo(l string) string {
	t := strings.TrimSpace(l)
	if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "sudo ") {
		return l
	}
	return "sudo " + t
}

func printRefusal(e *env, re *core.RefusedError) {
	printf(e.stderr, "%s %s\n", e.tr("Từ chối:", "Refused:"), re.Message.In(e.lang))
	for _, r := range re.Checks {
		printResult(e.stderr, e.lang, r, "  ")
	}
}

// printPreconditionWarnings in các điều kiện đầu không kiểm được (Unknown) hoặc
// cảnh báo: không đủ để từ chối, nhưng người dùng nên biết trước khi gật đầu.
func printPreconditionWarnings(e *env, rs []checks.Result) {
	for _, r := range rs {
		if r.Status == checks.Unknown || r.Status == checks.Warn {
			printResult(e.stdout, e.lang, r, "")
		}
	}
}

func printPreview(e *env, pv engine.Preview) {
	w, l := e.stdout, e.lang
	var run, skip, danger int
	for _, s := range pv.Steps {
		if s.Done {
			skip++
		} else {
			run++
		}
		if s.Info.Danger && !s.Done {
			danger++
		}
	}
	printf(w, "%s %s\n", e.tr("Kế hoạch:", "Plan:"), pv.Title.In(l))
	printf(w, e.tr("%d bước sẽ chạy, %d bước đã đúng sẽ bỏ qua, %d bước có thể gián đoạn dịch vụ.\n\n",
		"%d steps will run, %d already correct will be skipped, %d may interrupt a service.\n\n"), run, skip, danger)

	for i, s := range pv.Steps {
		mark := e.tr("sẽ chạy", "will run")
		if s.Done {
			mark = e.tr("đã đúng, bỏ qua", "already correct, skip")
		}
		printf(w, "%2d. [%s] %s  (%s)\n", i+1, mark, s.Info.Title.In(l), s.Info.ID)
		if s.Done {
			continue
		}
		if s.Info.Danger {
			printf(w, "    %s\n", e.tr("NGUY HIỂM: bước này có thể làm gián đoạn dịch vụ.", "DANGER: this step may interrupt a service."))
		}
		printf(w, "    %s %s\n", e.tr("Vì sao:", "Why:"), s.Info.Why.In(l))
		for _, c := range s.Info.Commands {
			printf(w, "    $ %s\n", c)
		}
		for _, f := range s.Info.Files {
			printf(w, "    %s %s\n", fileAction(e, f), f.Path)
		}
		if !s.Info.NoUndo.IsZero() {
			printf(w, "    %s %s\n", e.tr("Ghi chú hoàn tác:", "Undo note:"), s.Info.NoUndo.In(l))
		}
		if s.CheckError != "" {
			// Thường vì máy chưa có thứ bước trước sẽ cài; không phải lỗi.
			printf(w, "    %s %s\n", e.tr("(chưa kiểm được lúc xem trước:", "(could not check during preview:"), s.CheckError+")")
		}
	}
}

func fileAction(e *env, f engine.FileChange) string {
	switch {
	case f.Delete:
		return e.tr("xoá", "delete")
	case !f.OldExists:
		return e.tr("tạo", "create")
	case bytes.Equal(f.Old, f.New):
		return e.tr("giữ", "keep")
	}
	return e.tr("sửa", "change")
}

// progress in tiến độ của một job từ các sự kiện của engine. Output của lệnh
// được giữ lại theo bước và chỉ in khi bước đó lỗi (hoặc --verbose), để một lần
// cài trơn tru không đổ hàng nghìn dòng apt ra màn hình.
type progress struct {
	w       io.Writer
	lang    string
	verbose bool
	titles  map[string]i18n.Text
	noUndo  map[string]i18n.Text
	out     map[string][]string
}

// maxKeptLines là số dòng output cuối của một bước được giữ để in khi bước lỗi.
const maxKeptLines = 40

func newProgress(w io.Writer, lang string, verbose bool, p engine.Plan) *progress {
	pr := &progress{w: w, lang: lang, verbose: verbose, titles: map[string]i18n.Text{}, noUndo: map[string]i18n.Text{}, out: map[string][]string{}}
	for _, s := range p.Steps {
		info := s.Info()
		pr.titles[info.ID] = info.Title
		if !info.NoUndo.IsZero() {
			pr.noUndo[info.ID] = info.NoUndo
		}
	}
	return pr
}

func (p *progress) title(id string) string {
	if t, ok := p.titles[id]; ok {
		return t.In(p.lang)
	}
	return id
}

func (p *progress) tr(vi, en string) string { return i18n.T(vi, en).In(p.lang) }

func (p *progress) onEvent(ev engine.Event) {
	switch ev.Kind {
	case engine.EventJobStart:
		printf(p.w, "%s %s\n", p.tr("Bắt đầu job", "Starting job"), ev.JobID)
	case engine.EventStepStart:
		printf(p.w, "- %s ...\n", p.title(ev.StepID))
	case engine.EventStepSkip:
		printf(p.w, "  %s\n", p.tr("đã đúng, bỏ qua", "already correct, skipped"))
	case engine.EventStepDone:
		printf(p.w, "  %s\n", p.tr("xong", "done"))
	case engine.EventStepFail:
		printf(p.w, "  %s %s\n", p.tr("LỖI:", "FAILED:"), ev.Message)
		if !p.verbose {
			for _, l := range p.out[ev.StepID] {
				printf(p.w, "    | %s\n", l)
			}
		}
	case engine.EventCommand:
		if p.verbose {
			printf(p.w, "    $ %s\n", ev.Message)
		}
	case engine.EventOutput:
		if p.verbose {
			printf(p.w, "    | %s\n", ev.Message)
			return
		}
		kept := append(p.out[ev.StepID], ev.Message)
		if len(kept) > maxKeptLines {
			kept = kept[len(kept)-maxKeptLines:]
		}
		p.out[ev.StepID] = kept
	case engine.EventUndoStart:
		printf(p.w, "- %s %s ...\n", p.tr("Hoàn tác:", "Undo:"), p.title(ev.StepID))
	case engine.EventUndoDone:
		// Bước có NoUndo thì Undo không làm gì; nói thẳng như vậy để người đọc
		// không tưởng máy vừa được trả lại thứ gì.
		if n, ok := p.noUndo[ev.StepID]; ok {
			printf(p.w, "  %s %s\n", p.tr("không có gì để hoàn tác:", "nothing to undo:"), n.In(p.lang))
			return
		}
		printf(p.w, "  %s\n", p.tr("đã hoàn tác", "undone"))
	case engine.EventUndoFail:
		printf(p.w, "  %s %s\n", p.tr("HOÀN TÁC LỖI:", "UNDO FAILED:"), ev.Message)
	case engine.EventCheck:
		if ev.Check != nil {
			printf(p.w, "  [%s] %s\n", statusWord(p.lang, ev.Check.Status), ev.Check.Title.In(p.lang))
		}
	}
}

func printReport(e *env, rep engine.Report) {
	w := e.stdout
	printf(w, "\nJob %s: %s\n", rep.Job.ID, jobStatusText(e, rep.Job.Status))
	if rep.Job.Error != "" {
		printf(w, "%s %s\n", e.tr("Lỗi:", "Error:"), rep.Job.Error)
	}
	if len(rep.Checks) > 0 {
		printf(w, "%s\n", e.tr("Kiểm chứng:", "Verification:"))
		for _, r := range rep.Checks {
			printResult(w, e.lang, r, "  ")
		}
	}
}

func jobStatusText(e *env, s engine.JobStatus) string {
	switch s {
	case engine.JobSucceeded:
		return e.tr("thành công", "succeeded")
	case engine.JobRolledBack:
		return e.tr("hỏng, đã hoàn tác sạch", "failed, rolled back cleanly")
	case engine.JobUndoFailed:
		return e.tr("hỏng, và hoàn tác cũng lỗi: máy có thể đang dở dang", "failed, and undo failed too: the machine may be half-changed")
	}
	return string(s)
}

func printStatus(e *env, s core.Status) {
	w := e.stdout
	none := e.tr("(chưa có)", "(none)")
	or := func(v string) string {
		if v == "" {
			return none
		}
		return v
	}
	printf(w, "%-12s %s (%s %s)\n", "Web server:", or(string(s.Config.Web)), e.tr("nguồn", "source"), s.Config.WebSource)
	printf(w, "%-12s %s (%s %s)\n", "PHP:", or(s.Config.PHPVersion), e.tr("nguồn", "source"), s.Config.PHPSource)
	printf(w, "%-12s %s %s\n", "MariaDB:", e.tr("nguồn", "source"), s.Config.DBSource)
	installed := e.tr("chưa", "no")
	if s.Installed {
		installed = e.tr("rồi", "yes")
	}
	printf(w, "%-12s %s\n", e.tr("Đã cài:", "Installed:"), installed)
	if !s.Installed && s.Config.Web == "" {
		printf(w, "%s\n", e.tr("Cài bằng: sudo tpan stack install --web nginx|apache", "Install with: sudo tpan stack install --web nginx|apache"))
	}
	if len(s.Services) > 0 {
		printf(w, "%s\n", e.tr("Dịch vụ:", "Services:"))
		for _, r := range s.Services {
			printResult(w, e.lang, r, "  ")
		}
	}
}

func printHealth(e *env, h core.Health) {
	w := e.stdout
	if !h.Installed {
		printf(w, "%s\n", e.tr(
			"Stack chưa cài nên không có gì để kiểm. Cài bằng: sudo tpan stack install --web nginx|apache",
			"The stack is not installed, so there is nothing to check. Install with: sudo tpan stack install --web nginx|apache"))
		return
	}
	counts := map[checks.Status]int{}
	for _, r := range h.Results {
		counts[r.Status]++
		printResult(w, e.lang, r, "")
	}
	var parts []string
	for _, s := range []checks.Status{checks.Pass, checks.Warn, checks.Fail, checks.Unknown} {
		if counts[s] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[s], statusWord(e.lang, s)))
		}
	}
	printf(w, "\n%s\n", strings.Join(parts, ", "))
}

// printAdminPassword in mật khẩu quản trị WordPress đúng một lần, và chỉ khi
// lần chạy này thật sự cài WordPress (chạy lại thì mật khẩu mới không được dùng).
func printAdminPassword(e *env, res core.SiteAddResult) {
	if !res.WPInstalled {
		return
	}
	w := e.stdout
	d := res.Site.Domain.String()
	printf(w, "\n%s\n", e.tr("Tài khoản quản trị WordPress:", "WordPress admin account:"))
	printf(w, "  %-14s http://%s/wp-admin/\n", e.tr("Đăng nhập:", "Log in:"), d)
	printf(w, "  %-14s %s\n", e.tr("Tên:", "User:"), res.AdminUser)
	printf(w, "  %-14s %s\n", e.tr("Mật khẩu:", "Password:"), res.AdminPassword.Reveal())
	printf(w, "%s\n", e.tr(
		"Lưu lại ngay: tpan không giữ mật khẩu này và sẽ không in lại.",
		"Save it now: tpan does not keep this password and will not show it again."))
}

func printSiteList(e *env, list []site.Site) {
	w := e.stdout
	if len(list) == 0 {
		printf(w, "%s\n", e.tr(
			"Chưa có site nào. Tạo bằng: sudo tpan site add <domain> --email <email>",
			"No sites yet. Create one with: sudo tpan site add <domain> --email <email>"))
		return
	}
	// Cột của M13 (chủ, tạm khoá) nằm sau cột cũ: script đọc cột đầu vẫn đúng.
	printf(w, "%-40s %-8s %-5s %-4s %-16s %-14s %s\n", "DOMAIN", e.tr("TRẠNG THÁI", "STATUS"), "PHP", "SSL", e.tr("TẠO LÚC", "CREATED"),
		e.tr("CHỦ", "OWNER"), e.tr("TẠM KHOÁ", "SUSPENDED"))
	for _, s := range list {
		ssl := e.tr("không", "no")
		if s.SSL {
			ssl = e.tr("có", "yes")
		}
		printf(w, "%-40s %-8s %-5s %-4s %-16s %-14s %s\n", s.Domain, s.Status, s.PHPVersion, ssl, s.Created.Local().Format("2006-01-02 15:04"),
			s.Owner, suspensionText(e, s.Suspended))
	}
}

func printSiteInfo(e *env, info core.SiteInfo) {
	w, l := e.stdout, info.Layout
	row := func(k, v string) { printf(w, "%-16s %s\n", k, v) }
	row("Domain:", info.Site.Domain.String())
	row(e.tr("Trạng thái:", "Status:"), string(info.Site.Status))
	row(e.tr("Chủ:", "Owner:"), info.Site.Owner)
	row(e.tr("Tạm khoá:", "Suspended:"), suspensionText(e, info.Site.Suspended))
	row(e.tr("DB tạm khoá:", "DB suspended:"), suspensionText(e, info.Site.DBSuspended))
	row("PHP:", info.Site.PHPVersion)
	row(e.tr("Tạo lúc:", "Created:"), info.Site.Created.Local().Format("2006-01-02 15:04"))
	row(e.tr("User Linux:", "Linux user:"), l.User)
	row(e.tr("DB, user DB:", "DB, DB user:"), l.DB)
	row(e.tr("Thư mục:", "Directory:"), l.Dir)
	row("Document root:", l.Public)
	row("Log:", l.LogDir)
	row("Pool PHP-FPM:", l.Pool+" ("+info.Socket+")")
	switch info.Web {
	case core.Nginx:
		row("Vhost:", l.NginxConf)
	case core.Apache:
		row("Vhost:", l.ApacheConf)
		// Chỉ in trên Apache: trên nginx không có .htaccess nên chế độ này
		// chưa có tác dụng (M14, mục 6.13).
		row(e.tr("Chế độ .htaccess:", ".htaccess mode:"), string(info.Site.Htaccess.OrDefault()))
	}
	if info.Web == "" {
		printf(w, "\n%s\n", e.tr("Stack chưa cài nên không có gì để kiểm.", "The stack is not installed, so there is nothing to check."))
		return
	}
	printf(w, "\n%s\n", e.tr("Kiểm tra:", "Checks:"))
	for _, r := range info.Checks {
		printResult(w, e.lang, r, "  ")
	}
}
