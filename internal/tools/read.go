package tools

// Công cụ đọc. Không công cụ nào ở đây đổi gì trên máy: chúng chỉ gọi phần
// chỉ đọc của core, store, monitor, và lệnh `tail` (đọc file).
//
// Nội dung không tin cậy (nguyên tắc 5 của mục 8.1): text do người lạ viết
// được bọc bằng ai.Untrusted và phiên bị đánh dấu.
//   - tail_log: log truy cập, lỗi, lỗi PHP chứa URL, User-Agent, tham số do
//     bất kỳ ai gửi request tới site viết ra.
//   - get_job: output lệnh trong job (WP-CLI, curl...) có thể in lại nội dung
//     của site.
//   - site_health: phép kiểm http.site (checks/site.go, HTTPSite) đưa vào
//     bằng chứng tới 1 KB đầu của THÂN trang chủ, mà thân trang có thể chứa
//     text người lạ viết được (bình luận WordPress, tên bài của tài khoản bị
//     chiếm). Bằng chứng đó được bọc, và phiên chỉ bị đánh dấu khi bằng chứng
//     thật sự có thân (có dòng thứ hai sau "HTTP <mã>, <n> byte"). Các phép
//     kiểm HTTP khác (http.default ở stack_status) chạy curl với -o /dev/null,
//     chỉ có mã trạng thái: không bọc, không đánh dấu.
// Các công cụ còn lại trả dữ liệu do tpan hoặc người quản trị viết (bảng của
// tpan, file tpan quản lý, số liệu /proc), không đánh dấu.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/ai"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/monitor"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/sitefs"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

const (
	defaultLogLines = 50
	maxLogLines     = 200
	defaultJobs     = 20
	maxJobs         = 50
	// maxPlans: list_plans trả tối đa chừng này dòng, đủ cho một phiên chẩn đoán.
	maxPlans = 50
)

// ---------------------------------------------------------------- dựng core

// Dựng core từ Deps giống server.apiSites/apiStack. Khác: Stack có Journal,
// vì apply_plan có thể chạy Plan sửa stack (server chỉ đọc stack qua API).
func (s *Session) sites() *core.Sites {
	d := s.st.d
	return &core.Sites{Sys: d.Sys, Settings: d.Store, Store: d.Store, Journal: d.Store, Backups: d.Store, Accounts: d.Store, Samples: d.Store, WPHash: d.WPHash}
}

func (s *Session) stack() *core.Stack {
	d := s.st.d
	return &core.Stack{Sys: d.Sys, Settings: d.Store, Journal: d.Store, WPCLISHA512: d.WPCLISHA512}
}

func (s *Session) planner() *core.Planner {
	return &core.Planner{
		Sites: s.sites(), Stack: s.stack(), Plans: s.st.d.Store, Checks: s.st.d.Store, Files: s.st.d.Store, Backups: s.st.d.Store,
		NotFound: store.ErrNotFound, NotPending: store.ErrPlanNotPending, Now: s.st.d.Now,
	}
}

// ---------------------------------------------------------------- site

type siteRow struct {
	Domain     string      `json:"domain"`
	Status     site.Status `json:"status"`
	PHPVersion string      `json:"php_version"`
	SSL        bool        `json:"ssl"`
	Created    time.Time   `json:"created"`
}

func listSites(ctx context.Context, c *call, _ noArgs, _ validate.Domain) (any, error) {
	list, err := c.s.sites().List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]siteRow, len(list))
	for i, x := range list {
		out[i] = siteRow{Domain: x.Domain.String(), Status: x.Status, PHPVersion: x.PHPVersion, SSL: x.SSL, Created: x.Created}
	}
	return out, nil
}

// stage là các phép kiểm của một chặng, theo đúng thứ tự đường đi của request.
type stage struct {
	Stage   checks.Stage    `json:"stage"`
	Results []checks.Result `json:"results"`
}

// byStage gom các kết quả liền nhau cùng chặng. Bộ phép kiểm đã xếp theo
// chặng (checks.SiteChecks), nên gom liền nhau giữ đúng thứ tự.
func byStage(rs []checks.Result) []stage {
	out := []stage{}
	for _, r := range rs {
		if len(out) == 0 || out[len(out)-1].Stage != r.Stage {
			out = append(out, stage{Stage: r.Stage})
		}
		out[len(out)-1].Results = append(out[len(out)-1].Results, r)
	}
	return out
}

type siteHealthOut struct {
	Domain string `json:"domain"`
	// Installed false: stack chưa cài, không phép kiểm nào chạy.
	Installed bool           `json:"installed"`
	Web       core.WebServer `json:"web"`
	Stages    []stage        `json:"stages"`
}

func siteHealth(ctx context.Context, c *call, _ domainIn, d validate.Domain) (any, error) {
	h, err := c.s.sites().Health(ctx, d)
	if err != nil {
		return nil, err
	}
	for i := range h.Results {
		r := &h.Results[i]
		if !strings.HasPrefix(r.ID, "http.site:") {
			continue
		}
		for j := range r.Evidence {
			ev := &r.Evidence[j]
			if strings.Contains(strings.TrimSpace(ev.Output), "\n") {
				ev.Output = ai.Untrusted("http-response", ev.Output)
				c.untrusted = true
			}
		}
	}
	return siteHealthOut{Domain: d.String(), Installed: h.Installed, Web: h.Config.Web, Stages: byStage(h.Results)}, nil
}

// ---------------------------------------------------------------- stack, máy

type stackOut struct {
	Status core.Status `json:"status"`
	Health core.Health `json:"health"`
}

func stackStatus(ctx context.Context, c *call, _ noArgs, _ validate.Domain) (any, error) {
	st := c.s.stack()
	status, err := st.Status(ctx)
	if err != nil {
		return nil, err
	}
	h, err := st.Health(ctx)
	if err != nil {
		return nil, err
	}
	return stackOut{Status: status, Health: h}, nil
}

func machine(ctx context.Context, c *call, _ noArgs, _ validate.Domain) (any, error) {
	return monitor.Read(ctx, c.s.st.d.Sys), nil
}

// ---------------------------------------------------------------- log

type tailOut struct {
	Domain string `json:"domain"`
	Log    string `json:"log"`
	Path   string `json:"path"`
	Lines  int    `json:"lines"`
	Text   string `json:"text"`
}

var siteLogs = []string{"access", "error", "php-error"}

func tailLog(ctx context.Context, c *call, in tailLogIn, d validate.Domain) (any, error) {
	if !slices.Contains(siteLogs, in.Log) {
		return nil, badArgs("log must be one of: " + strings.Join(siteLogs, ", "))
	}
	n := in.Lines
	switch {
	case n <= 0:
		n = defaultLogLines
	case n > maxLogLines:
		n = maxLogLines
	}
	sys, st := c.s.st.d.Sys, c.s.st.d.Store
	rec, ok, err := st.Site(ctx, d)
	if err != nil {
		return nil, fmt.Errorf("đọc site %s: %w", d, err)
	}
	// Chỉ site tpan quản lý: đường dẫn log dựng từ domain đã qua validate và
	// đã có trong bảng sites, không từ chuỗi agent đưa.
	if !ok {
		return nil, &Error{Code: "site_not_found", Message: "tpan has no site " + d.String() + "; call list_sites"}
	}
	path := rec.Layout().LogDir + "/" + in.Log + ".log"
	// tail đọc phần cuối mà không nạp cả file (log truy cập có thể vài GB).
	// argv, không shell; "--" để đường dẫn không bao giờ bị hiểu là cờ.
	res, err := sys.Run(ctx, system.Cmd{Name: "tail", Args: []string{"-n", strconv.Itoa(n), "--", path}})
	if err != nil {
		var xe *system.ExitError
		if errors.As(err, &xe) {
			return nil, &Error{Code: "log_unreadable", Message: "cannot read " + path + ": " + strings.TrimSpace(res.Stderr)}
		}
		return nil, fmt.Errorf("đọc đuôi log %s: %w", path, err)
	}
	return tailOut{Domain: d.String(), Log: in.Log, Path: path, Lines: n, Text: ai.Untrusted("log:"+in.Log, res.Stdout)}, nil
}

// ---------------------------------------------------------------- file

type managedFileOut struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	// EditedByHand: file khác bản tpan đã ghi (sha256 khác), tức có người sửa tay.
	EditedByHand   bool      `json:"edited_by_hand"`
	SHA256         string    `json:"sha256,omitempty"`
	RecordedSHA256 string    `json:"recorded_sha256"`
	JobID          string    `json:"job_id"`
	Updated        time.Time `json:"updated"`
	Content        string    `json:"content"`
}

func managedFile(ctx context.Context, c *call, in pathIn, _ validate.Domain) (any, error) {
	files, err := c.s.st.d.Store.ManagedFiles(ctx)
	if err != nil {
		return nil, err
	}
	// So khớp đúng từng ký tự với bảng managed_files: agent không đọc được
	// file nào tpan không tự ghi, kể cả qua "../" hay symlink trong đường dẫn.
	i := slices.IndexFunc(files, func(f store.ManagedFile) bool { return f.Path == in.Path })
	if i < 0 {
		return nil, &Error{Code: "file_not_managed", Message: "tpan does not manage " + in.Path + "; only files tpan wrote can be read"}
	}
	f := files[i]
	out := managedFileOut{Path: f.Path, RecordedSHA256: f.SHA256, JobID: f.JobID, Updated: f.Updated}
	data, err := c.s.st.d.Sys.ReadFile(f.Path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// File đã bị xoá: cũng là một kiểu sửa tay, nhưng không có nội dung để so.
		out.EditedByHand = true
		return out, nil
	case err != nil:
		return nil, fmt.Errorf("đọc file quản lý %s: %w", f.Path, err)
	}
	sum := sha256.Sum256(data)
	out.Exists, out.SHA256, out.Content = true, hex.EncodeToString(sum[:]), string(data)
	out.EditedByHand = out.SHA256 != f.SHA256
	// File trong thư mục site (.htaccess) do site viết lại được: là dữ liệu,
	// không phải lời của tpan (M28). ReadFile đã đi qua cửa sitefs (không theo
	// symlink, chỉ file thường của site, có trần cỡ).
	if sitefs.InZone(f.Path) {
		out.Content = ai.Untrusted("file:"+f.Path, out.Content)
	}
	return out, nil
}

// ---------------------------------------------------------------- job

func listJobs(ctx context.Context, c *call, in limitIn, _ validate.Domain) (any, error) {
	n := in.Limit
	switch {
	case n <= 0:
		n = defaultJobs
	case n > maxJobs:
		n = maxJobs
	}
	jobs, err := c.s.st.d.Store.Jobs(ctx, n)
	if err != nil {
		return nil, err
	}
	return jobs, nil
}

type jobOut struct {
	Job    engine.Job     `json:"job"`
	Events []engine.Event `json:"events"`
}

func getJob(ctx context.Context, c *call, in idIn, _ validate.Domain) (any, error) {
	st := c.s.st.d.Store
	j, err := st.Job(ctx, in.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, &Error{Code: "job_not_found", Message: "there is no job " + in.ID + "; call list_jobs"}
	}
	if err != nil {
		return nil, err
	}
	evs, err := st.Events(ctx, j.ID, 0)
	if err != nil {
		return nil, err
	}
	// Lỗi của job và của bước thường mang stderr của lệnh: cũng bọc.
	if j.Error != "" {
		j.Error = ai.Untrusted("job-error", j.Error)
	}
	for i := range evs {
		ev := &evs[i]
		switch ev.Kind {
		case engine.EventOutput:
			ev.Message = ai.Untrusted("command-output", ev.Message)
		case engine.EventStepFail:
			ev.Message = ai.Untrusted("step-error", ev.Message)
		}
		if ev.Check != nil {
			for k := range ev.Check.Evidence {
				e := &ev.Check.Evidence[k]
				e.Output = ai.Untrusted("check-output", e.Output)
			}
		}
	}
	return jobOut{Job: j, Events: evs}, nil
}

// ---------------------------------------------------------------- panel

// maxRecentViews: current_view trả mục đang mở cộng tối đa chừng này mục vừa
// đi qua (mục 8.4: "10 lần chuyển mục gần nhất").
const maxRecentViews = 10

// viewRow là một lần chuyển mục trên panel. Đường dẫn và nhãn chứa domain;
// Call che chúng như mọi kết quả khác.
type viewRow struct {
	Path  string    `json:"path"`
	Label string    `json:"label"`
	Time  time.Time `json:"time"`
}

type currentViewOut struct {
	// Current nil khi panel chưa báo mục nào.
	Current *viewRow  `json:"current"`
	Recent  []viewRow `json:"recent"`
	Note    string    `json:"note,omitempty"`
}

// currentView đọc các lần chuyển mục giao diện đã báo (store.AIViews, mới
// trước): dòng đầu là mục đang mở, các dòng sau là đường vừa đi. Đường dẫn và
// nhãn do giao diện của người đã đăng nhập gửi, không phải người lạ: không
// bọc untrusted.
func currentView(ctx context.Context, c *call, _ noArgs, _ validate.Domain) (any, error) {
	vs, err := c.s.st.d.Store.AIViews(ctx, maxRecentViews+1)
	if err != nil {
		return nil, fmt.Errorf("đọc các mục vừa mở trên panel: %w", err)
	}
	out := currentViewOut{Recent: []viewRow{}}
	if len(vs) == 0 {
		out.Note = "The panel has not reported any screen yet (no one has opened the tpan web panel since it started recording)."
		return out, nil
	}
	vs = vs[:min(len(vs), maxRecentViews+1)]
	row := func(v store.AIView) viewRow {
		// Đường dẫn trình duyệt báo có thể mã hoá %xx (domain có dấu gõ thẳng
		// trên thanh địa chỉ): giải mã trước để bộ che nhận ra domain. Giải mã
		// lỗi thì giữ nguyên.
		p := v.Path
		if u, err := url.PathUnescape(p); err == nil {
			p = u
		}
		return viewRow{Path: p, Label: v.Label, Time: v.Time}
	}
	cur := row(vs[0])
	out.Current = &cur
	for _, v := range vs[1:] {
		out.Recent = append(out.Recent, row(v))
	}
	return out, nil
}

// ---------------------------------------------------------------- Plan

// planRow là một dòng của list_plans: không có bản xem trước (get_plan có).
type planRow struct {
	ID        string            `json:"id"`
	Kind      string            `json:"kind"`
	Target    string            `json:"target"`
	Title     i18n.Text         `json:"title"`
	Origin    engine.Origin     `json:"origin"`
	SessionID string            `json:"session_id,omitempty"`
	Untrusted bool              `json:"untrusted"`
	Status    engine.PlanStatus `json:"status"`
	Created   time.Time         `json:"created"`
	Decided   time.Time         `json:"decided,omitzero"`
	DecidedBy string            `json:"decided_by,omitempty"`
	JobID     string            `json:"job_id,omitempty"`
}

var planStatuses = []engine.PlanStatus{engine.PlanPending, engine.PlanApplied, engine.PlanRejected}

func listPlans(ctx context.Context, c *call, in statusIn, _ validate.Domain) (any, error) {
	status := engine.PlanStatus(in.Status)
	if status != "" && !slices.Contains(planStatuses, status) {
		return nil, badArgs("status must be empty or one of: pending, applied, rejected")
	}
	ps, err := c.s.st.d.Store.Plans(ctx, status, maxPlans)
	if err != nil {
		return nil, err
	}
	out := make([]planRow, len(ps))
	for i, p := range ps {
		out[i] = planRow{
			ID: p.ID, Kind: p.Kind, Target: p.Target, Title: p.Title, Origin: p.Origin, SessionID: p.SessionID,
			Untrusted: p.Untrusted, Status: p.Status, Created: p.Created, Decided: p.Decided, DecidedBy: p.DecidedBy, JobID: p.JobID,
		}
	}
	return out, nil
}

// planView là Plan kèm bản xem trước dạng chữ. Trường Preview ở đây che
// trường cùng tên của PendingPlan nhúng bên trong (encoding/json chọn trường
// nông hơn).
type planView struct {
	engine.PendingPlan
	Preview previewText `json:"preview"`
}

func getPlan(ctx context.Context, c *call, in idIn, _ validate.Domain) (any, error) {
	p, err := c.s.st.d.Store.Plan(ctx, in.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, &Error{Code: "plan_not_found", Message: "there is no plan " + in.ID + "; call list_plans"}
	}
	if err != nil {
		return nil, err
	}
	return planView{PendingPlan: p, Preview: textPreview(p.Preview)}, nil
}

// previewText là engine.Preview với nội dung file dạng chữ: FileChange giữ
// []byte, encoding/json mã hoá thành base64 mà AI không đọc được.
type previewText struct {
	Kind   string     `json:"kind"`
	Target string     `json:"target"`
	Title  i18n.Text  `json:"title"`
	Steps  []stepText `json:"steps"`
}

type stepText struct {
	Info       stepInfoText `json:"info"`
	Done       bool         `json:"done"`
	CheckError string       `json:"check_error,omitempty"`
}

// stepInfoText: Files ở đây che Files của StepInfo nhúng bên trong.
type stepInfoText struct {
	engine.StepInfo
	Files []fileText `json:"files,omitempty"`
}

type fileText struct {
	Path      string `json:"path"`
	New       string `json:"new,omitempty"`
	Delete    bool   `json:"delete,omitempty"`
	Old       string `json:"old,omitempty"`
	OldExists bool   `json:"old_exists"`
	// OldLink, Hidden: như engine.FileChange (M28). Không có hai trường này
	// thì một symlink trông như "file rỗng" với AI.
	OldLink string `json:"old_link,omitempty"`
	Hidden  string `json:"hidden,omitempty"`
}

// textPreview là bản xem trước dạng chữ cho AI. Nội dung do site viết (file
// trong thư mục site, đích symlink do site đặt, output của lệnh wp chạy mã PHP
// của site trong CheckError) được bọc ai.Untrusted như log (M28, S6 của
// docs/thay-doi/M28-thiet-ke.md): đó là chỗ một site cài câu lệnh cho AI.
func textPreview(pv engine.Preview) previewText {
	out := previewText{Kind: pv.Kind, Target: pv.Target, Title: pv.Title, Steps: make([]stepText, len(pv.Steps))}
	for i, s := range pv.Steps {
		st := stepText{Info: stepInfoText{StepInfo: s.Info}, Done: s.Done, CheckError: s.CheckError}
		if st.CheckError != "" {
			st.CheckError = ai.Untrusted("check-error", st.CheckError)
		}
		for _, f := range s.Info.Files {
			ft := fileText{Path: f.Path, New: string(f.New), Delete: f.Delete, Old: string(f.Old),
				OldExists: f.OldExists, OldLink: f.OldLink, Hidden: f.Hidden}
			if sitefs.InZone(f.Path) {
				if ft.Old != "" {
					ft.Old = ai.Untrusted("file:"+f.Path, ft.Old)
				}
				if ft.OldLink != "" {
					ft.OldLink = ai.Untrusted("symlink:"+f.Path, ft.OldLink)
				}
			}
			st.Info.Files = append(st.Info.Files, ft)
		}
		out.Steps[i] = st
	}
	return out
}
