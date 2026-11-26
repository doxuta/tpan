package core

// Cron thật cho WordPress (mục 6.10 của docs/KE-HOACH.md). Hợp đồng ở
// types_m9.go.
//
// Mỗi site active có file /etc/cron.d/tpan-<slug> gọi `tpan cron run <domain>`
// mỗi 5 phút bằng root; tpan chạy `wp cron event run --due-now` dưới user của
// site rồi ghi kết quả vào bảng cron_runs cho màn Cron. Lý do tpan đứng giữa
// (thay vì dòng cron gọi thẳng wp) ở docs/thay-doi/M9.md.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
	"github.com/doxuta/tpan/internal/validate"
)

// cronOutputMax: chỉ giữ 2 KB cuối của output mỗi lần chạy. Dòng cron_runs
// bị ghi đè 5 phút một lần mỗi site; phần cuối là chỗ có lỗi.
const cronOutputMax = 2048

// cronTimeout: một lần chạy cron bị treo (plugin gọi mạng không trả lời) bị cắt
// trước lượt kế tiếp 5 phút sau, để các lần chạy không chồng lên nhau.
const cronTimeout = 4 * time.Minute

func (c *Cron) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return c.Sites.now()
}

// cronCmd là lệnh cron WordPress của site, dựng y như steps dựng mọi lệnh wp
// (wpRun): dưới user của site, --path trỏ public. --due-now: chỉ chạy việc
// đã tới hạn. --quiet: không in một dòng cho mỗi việc chạy thành công, để
// output giữ lại chủ yếu là lỗi.
func cronCmd(l site.Layout) system.Cmd {
	return system.Cmd{
		Name:   "wp",
		Args:   []string{"cron", "event", "run", "--due-now", "--quiet", "--path=" + l.Public},
		AsUser: l.User,
	}
}

// cronTail giữ n byte cuối của s, không cắt giữa một ký tự UTF-8.
func cronTail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	i := len(s) - n
	for i < len(s) && !utf8.RuneStart(s[i]) {
		i++
	}
	return s[i:]
}

// Run chạy cron WordPress của một site (hợp đồng ở types_m9.go). Không qua
// engine, không lấy khoá: chạy 5 phút một lần mỗi site, giữ khoá thì người
// bấm trên panel bị 409. error chỉ khác nil khi site không có trong tpan
// (không ghi dòng nào: dòng cron sai được thấy trong log của cron) hoặc khi
// không ghi được kết quả; wp lỗi thì ghi "fail" và trả nil.
func (c *Cron) Run(ctx context.Context, d validate.Domain) (CronRun, error) {
	if c.Runs == nil {
		return CronRun{}, fmt.Errorf("chạy cron của %s: chưa có DB của tpan để ghi kết quả", d)
	}
	st, err := c.Sites.find(ctx, d)
	if err != nil {
		return CronRun{}, err
	}
	start := c.now()
	run := CronRun{Domain: d.String(), Started: start.UTC()}
	switch {
	case st.Status != site.Active:
		// Site trong thùng rác: user Linux của nó bị khoá bằng hạn tài khoản,
		// nhưng root gọi runuser bỏ qua hạn đó, nên phải chặn ở đây.
		run.Status = CronSkipped
		run.Output = "site " + d.String() + " is not active (" + string(st.Status) + "), skipped"
	case st.Suspended != account.NotSuspended:
		// M13: file cron đã đổi tên .suspended nên cron không gọi tới đây;
		// chặn thêm cho lần gọi tay hay file cron sót (khoá giữa chừng).
		run.Status = CronSkipped
		run.Output = "site " + d.String() + " is suspended, skipped"
	case st.DBSuspended != account.NotSuspended:
		// DB đang khoá thì wp cron chắc chắn lỗi kết nối DB: ghi "skipped"
		// thay vì một dòng "fail" mỗi 5 phút cho điều đã biết.
		run.Status = CronSkipped
		run.Output = "site " + d.String() + " database is suspended, skipped"
	default:
		rctx, cancel := context.WithTimeout(ctx, cronTimeout)
		res, err := c.Sites.Sys.Run(rctx, cronCmd(st.Layout()))
		cancel()
		run.Duration = c.now().Sub(start)
		run.Status, run.ExitCode = CronOK, res.ExitCode
		out := strings.TrimSpace(res.Stdout + "\n" + res.Stderr)
		var ee *system.ExitError
		switch {
		case errors.As(err, &ee):
			run.Status, run.ExitCode = CronFail, ee.Code
		case err != nil:
			// Không chạy được lệnh (không có wp, ctx huỷ): không có mã thoát.
			run.Status, run.ExitCode = CronFail, -1
			out = strings.TrimSpace(out + "\n" + err.Error())
		}
		run.Output = cronTail(out, cronOutputMax)
	}
	if err := c.Runs.SaveCronRun(ctx, run); err != nil {
		return run, fmt.Errorf("ghi kết quả cron của %s: %w", d, err)
	}
	return run, nil
}

// List trả nội dung màn Cron (hợp đồng ở types_m9.go). Runs nil (máy chưa có
// tpan.db) thì không site nào có lần chạy gần nhất.
func (c *Cron) List(ctx context.Context) (CronOverview, error) {
	all, err := c.Sites.Store.Sites(ctx)
	if err != nil {
		return CronOverview{}, fmt.Errorf("đọc danh sách site: %w", err)
	}
	last := map[string]CronRun{}
	if c.Runs != nil {
		runs, err := c.Runs.CronRuns(ctx)
		if err != nil {
			return CronOverview{}, fmt.Errorf("đọc bảng cron_runs: %w", err)
		}
		for _, r := range runs {
			last[r.Domain] = r
		}
	}
	out := CronOverview{Sites: make([]CronEntry, 0, len(all))}
	for _, st := range all {
		if st.Status != site.Active && st.Status != site.Trashed {
			continue
		}
		// M29: site PHP trống không có cron của WordPress.
		if st.Plain() {
			continue
		}
		l := st.Layout()
		d := st.Domain.String()
		e := CronEntry{
			Domain:   d,
			File:     site.CronFile(l),
			Schedule: CronEvery,
			Command:  TpanBin + " cron run " + d,
		}
		// Đọc máy chứ không suy từ trạng thái: site tạo trước M9 chưa có file
		// cron, và màn Cron phải nói đúng điều đó.
		if e.Enabled, err = pathExists(c.Sites.Sys, e.File); err != nil {
			return CronOverview{}, err
		}
		if r, ok := last[d]; ok {
			e.Last = &r
		}
		out.Sites = append(out.Sites, e)
	}
	if out.Backup, err = c.Backups.Settings(ctx); err != nil {
		return CronOverview{}, err
	}
	if out.Backup.At != "" {
		out.BackupFile = site.BackupCronFile
	}
	return out, nil
}

// siteCronSteps: hai bước cron của một site cho Plan tạo site và khôi phục
// từ thùng rác (hợp đồng ở types_m9.go). Tắt cron theo lượt xem trước, rồi
// mới đặt cron thật: thứ tự ngược lại thì có lúc việc chạy từ hai nơi.
// Không cần reload cron: cron của Debian tự đọc lại /etc/cron.d mỗi phút khi
// thời điểm sửa của thư mục đổi.
func siteCronSteps(l site.Layout) ([]engine.Step, error) {
	content, err := templates.SiteCron(l)
	if err != nil {
		return nil, err
	}
	d := l.Domain.String()
	return []engine.Step{
		&steps.WPConstant{Layout: l, Name: "DISABLE_WP_CRON", Value: "true"},
		&steps.File{
			Path: site.CronFile(l), Content: content, Perm: 0o644, Owner: system.Root,
			Title: i18n.T("Đặt lịch cron WordPress của "+d+" (5 phút một lần)", "Schedule the WordPress cron of "+d+" (every 5 minutes)"),
			Why: i18n.T(
				"Việc hẹn giờ của WordPress (đăng bài hẹn giờ, gửi thư, kiểm cập nhật) cần ai đó gọi tới. Bỏ bước này thì sau khi tắt cron theo lượt xem, bài hẹn giờ không bao giờ được đăng. File thuộc root, quyền 0644: cron bỏ qua file trong /etc/cron.d mà người khác ghi được.",
				"WordPress's scheduled work (scheduled posts, emails, update checks) needs something to trigger it. Without this step, once the page-view cron is off, scheduled posts never get published. The file belongs to root with mode 0644: cron ignores files in /etc/cron.d that others can write."),
		},
	}, nil
}

// siteCronAbsent gỡ file cron của site (đưa vào thùng rác, xoá hẳn).
func siteCronAbsent(l site.Layout) engine.Step {
	d := l.Domain.String()
	return &steps.Absent{
		Path:  site.CronFile(l),
		Title: i18n.T("Gỡ lịch cron WordPress của "+d, "Remove the WordPress cron schedule of "+d),
		Why: i18n.T(
			"Site không còn chạy thì không còn việc hẹn giờ để gọi. Bỏ bước này thì cron vẫn gọi tpan mỗi 5 phút cho một site không còn, và log đầy dòng bỏ qua.",
			"A site that no longer runs has no scheduled work to trigger. Without this step cron keeps calling tpan every 5 minutes for a site that is gone, filling the log with skipped runs."),
	}
}
