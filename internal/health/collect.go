package health

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// planned là các phép kiểm (chưa chạy) của một đối tượng, đúng thứ tự.
type planned struct {
	target string
	checks []checks.Check
}

func (r *Runner) clock() func() time.Time {
	if r.Now != nil {
		return r.Now
	}
	return time.Now
}

// collect gom phép kiểm theo đối tượng: máy và stack (""), từng site active,
// lệch file (gắn vào site sở hữu file), rồi chính tpan. installed cho biết
// stack đã cài chưa.
func (r *Runner) collect(ctx context.Context, o Options) (plan []planned, installed bool, err error) {
	stack := &core.Stack{Sys: r.Sys, Settings: r.Store, WPCLISHA512: r.WPCLISHA512, Sites: r.Store}
	sites := &core.Sites{Sys: r.Sys, Settings: r.Store, Store: r.Store}

	stackChecks, installed, err := stack.Checks(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("dựng phép kiểm của stack: %w", err)
	}

	if o.Domain != "" {
		// Domain đi từ CLI hay API: qua validate trước khi tới core.
		d, err := validate.ParseDomain(o.Domain)
		if err != nil {
			return nil, false, fmt.Errorf("health cho %q: %w", o.Domain, err)
		}
		cs, err := sites.Checks(ctx, d)
		if err != nil {
			return nil, false, fmt.Errorf("dựng phép kiểm của site %s: %w", d, err)
		}
		return []planned{{target: d.String(), checks: cs}}, installed, nil
	}

	// RAM và swap là của máy, có nghĩa cả khi stack chưa cài.
	plan = []planned{{target: "", checks: append(stackChecks, checks.Memory())}}
	idx := map[string]int{"": 0}
	add := func(target string, cs ...checks.Check) {
		i, ok := idx[target]
		if !ok {
			i = len(plan)
			idx[target] = i
			plan = append(plan, planned{target: target})
		}
		plan[i].checks = append(plan[i].checks, cs...)
	}

	all, err := r.Store.Sites(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("đọc danh sách site: %w", err)
	}
	for _, st := range all {
		if st.Status != site.Active {
			continue
		}
		cs, err := sites.Checks(ctx, st.Domain)
		if err != nil {
			return nil, false, fmt.Errorf("dựng phép kiểm của site %s: %w", st.Domain, err)
		}
		add(st.Domain.String(), cs...)
	}

	files, err := r.Store.ManagedFiles(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("đọc danh sách file quản lý: %w", err)
	}
	for _, f := range files {
		// .htaccess: tpan ghi bản đầu, nhưng WordPress tự ghi lại đoạn giữa
		// "# BEGIN WordPress" và "# END WordPress" mỗi khi lưu permalink, nên so
		// sha256 cả file là báo lệch giả (thấy trên máy ảo Apache, e2e M4).
		// ponytail: bỏ hẳn khỏi phép kiểm lệch; nâng cấp: so phần ngoài đoạn
		// BEGIN/END nếu cần bắt sửa tay ở đó.
		if path.Base(f.Path) == ".htaccess" {
			continue
		}
		// Xét cả site không active (thùng rác): file của nó vẫn là của nó, gắn
		// vào "" thì lệch hiện sai chỗ trên bản đồ.
		add(fileOwner(f.Path, all), checks.Drift(f.Path, f.SHA256, checks.DriftStage(f.Path)))
	}

	if !r.NoSelf {
		add(store.TargetTPAN, r.selfChecks()...)
	}
	return plan, installed, nil
}

// fileOwner trả domain của site sở hữu path, "" khi không thuộc site nào.
func fileOwner(path string, sites []site.Site) string {
	for _, st := range sites {
		l := st.Layout()
		if l.Dir == "" {
			continue
		}
		switch {
		case strings.HasPrefix(path, l.Dir+"/"), strings.HasPrefix(path, l.LogDir+"/"),
			path == l.NginxConf, path == l.NginxLink, path == l.ApacheConf, path == l.ApacheLink,
			isPoolConf(path, l.Pool):
			return st.Domain.String()
		}
	}
	return ""
}

// isPoolConf: path là /etc/php/<bản>/fpm/pool.d/<pool>.conf với bản bất kỳ
// (site có thể đã đổi bản PHP; file của bản cũ vẫn là của site).
func isPoolConf(path, pool string) bool {
	rest, ok := strings.CutPrefix(path, "/etc/php/")
	if !ok {
		return false
	}
	version, tail, ok := strings.Cut(rest, "/")
	return ok && version != "" && tail == "fpm/pool.d/"+pool+".conf"
}

// selfChecks là phép kiểm của chính tpan (mục 6.7). Quyền lấy đúng như
// cmd/tpan/setup.go tạo: /etc/tpan 0700, khoá TLS 0600, tpan.db 0600 (store.Open).
func (r *Runner) selfChecks() []checks.Check {
	now := r.clock()
	cs := []checks.Check{
		checks.TPANDatabase(r.Store.IntegrityCheck),
		checks.FreeSpace(StateDir, TPANMinFree, checks.StageTPAN),
		checks.PanelCert(PanelCertPath, now),
	}
	if r.DBPath != "" {
		cs = append(cs, checks.FileMode(r.DBPath, 0o600, system.Root, checks.StageTPAN))
	}
	return append(cs,
		checks.FileMode(PanelKeyPath, 0o600, system.Root, checks.StageTPAN),
		checks.FileMode(PanelConfDir, 0o700, system.Root, checks.StageTPAN),
		checks.TPANJobs(func(ctx context.Context) ([]checks.StuckJob, error) {
			jobs, err := r.Store.StuckJobs(ctx, now().Add(-StuckAfter))
			if err != nil {
				return nil, err
			}
			out := make([]checks.StuckJob, len(jobs))
			for i, j := range jobs {
				out[i] = checks.StuckJob{ID: j.ID, Kind: j.Kind, Target: j.Target, Started: j.Started}
			}
			return out, nil
		}),
		checks.TPANScheduler(r.lastBeat, SchedulerMaxAge, now),
		// M9: bản sao tpan.db do lịch backup hằng ngày tạo; 48 giờ là một ngày
		// lịch cộng một ngày cho trễ (máy tắt qua đêm, một lần chạy lỗi).
		checks.TPANBackupAge(site.TPANDBBackupDir, 48*time.Hour, now),
	)
}

// lastBeat đọc nhịp mà Scheduler ghi (RFC 3339).
func (r *Runner) lastBeat(ctx context.Context) (time.Time, bool, error) {
	v, ok, err := r.Store.GetSetting(ctx, SettingSchedulerBeat)
	if err != nil || !ok {
		return time.Time{}, false, err
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("đọc %s %q: %w", SettingSchedulerBeat, v, err)
	}
	return t, true, nil
}

// record ghi kết quả của một nhóm. Lần chạy cho một site (o.Domain): phép kiểm
// đã có dòng với target khác (cô lập "isolation:a:b" thuộc site a theo lần
// chạy đầy đủ) thì giữ target cũ, để chạy sau một job của site b không đổi
// target của dòng đó qua lại.
func (r *Runner) record(ctx context.Context, o Options, target string, rs []checks.Result, now time.Time, into *store.Transition) error {
	byTarget := map[string][]checks.Result{}
	order := []string{}
	for _, res := range rs {
		t := target
		if o.Domain != "" {
			old, ok, err := r.Store.CheckResult(ctx, res.ID)
			if err != nil {
				return err
			}
			if ok {
				t = old.Target
			}
		}
		if _, seen := byTarget[t]; !seen {
			order = append(order, t)
		}
		byTarget[t] = append(byTarget[t], res)
	}
	for _, t := range order {
		tr, err := r.Store.RecordResults(ctx, t, byTarget[t], now)
		if err != nil {
			return err
		}
		mergeTransition(into, tr)
	}
	return nil
}

func mergeTransition(into *store.Transition, tr store.Transition) {
	into.Opened = append(into.Opened, tr.Opened...)
	into.Closed = append(into.Closed, tr.Closed...)
}
