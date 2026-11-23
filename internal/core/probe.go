package core

// Thử tấn công: `tpan isolation probe` (M17, mục 3.9 của
// docs/thay-doi/M17-thiet-ke.md).
//
// Luật không được phá: chỉ đụng vào thứ mang một trong hai tên miền thử của
// lượt đó (site.IsProbeDomain, và có trong sổ probe_runs); mọi đường dẫn suy
// từ site.LayoutOf(tên đó). Dòng sổ được ghi TRƯỚC khi tạo bất cứ thứ gì.
//
// Một lượt là MỘT job "isolation.probe":
//
//  1. ghi sổ (probeRecord, bên trong job);
//  2. tạo A rồi B bằng đúng các bước của site.add trừ hai bước cron;
//  3. L8 (một bước: tạo vhost lạ, kiểm, gỡ);
//  4. lượt ngoài lồng (probePass), ghi báo cáo vào sổ ngay khi xong;
//  5. đưa A, B vào lồng (cageEnableSteps, trần cố định probeLimits);
//  6. lượt trong lồng, ghi sổ;
//  7. dọn B rồi A (probeCleanupSteps).
//
// Job hỏng thì Execute chạy ngay Plan "probe.cleanup" cho đúng hai tên đó.
// Plan dọn đọc sổ BÊN TRONG job (probeLedger) và chỉ đụng tên vừa khớp mẫu vừa
// có trong sổ, nên một Plan dọn dựng lúc lượt khác đang chạy không bao giờ dọn
// nhầm lượt đó.

import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/cage"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// Probe gom những gì phép thử cần. Dựng mới cho mỗi lệnh.
type Probe struct {
	Sys      system.System
	Settings Settings
	Store    SiteStore // chỉ đọc (lấy stack, tránh tên trùng); site thử KHÔNG được ghi vào đây
	Runs     ProbeStore
	// Journal nhận job khi Execute. Plan của phép thử chạy với một Journal bọc
	// bỏ qua RecordFile, ForgetFile (file của site thử không vào managed_files).
	Journal engine.Journal
	// WPVersion, WPHash như Sites (bản WordPress của site thử).
	WPVersion, WPHash string
	Now               func() time.Time
	// Rand sinh 12 ký tự hex của tên miền thử và 16 ký tự hex của id; nil là crypto/rand.
	Rand func(n int) (string, error)
}

// ProbeJob là một lượt thử đã dựng Plan, dùng cho đúng một lần Execute.
type ProbeJob struct {
	Plan engine.Plan
	Run  ProbeRunInfo

	p      *Probe
	cfg    StackConfig
	a, b   site.Layout
	sfx    string      // 12 hex chung của hai tên miền
	report ProbeReport // các bước ghi dần vào đây
	// recorded: bước ghi sổ đã chạy, tức là từ đây có thể đã có thứ trên máy
	// mang hai tên này và phải dọn khi job hỏng.
	recorded bool
}

// ProbeRunInfo là phần của lượt thử biết được trước khi chạy.
type ProbeRunInfo struct {
	ID      string `json:"id"`
	DomainA string `json:"domain_a"`
	DomainB string `json:"domain_b"`
	Only    string `json:"only"`
}

const (
	probeKind        = "isolation.probe"
	probeCleanupKind = "probe.cleanup"
	probeOnlyOutside = "ngoai"
	probeOnlyInside  = "trong"

	probeRunning = "running"
	probeDone    = "done"
	probeFailed  = "failed"

	// probeC3MinFree: C3 ngoài lồng (cấp phát 320 MB, không có trần nào giữ)
	// chỉ chạy khi máy còn ít nhất chừng này RAM trống (MemAvailable).
	probeC3MinFree = 1 << 30
	// probeSystemdMin: --expand-environment của systemd-run có từ bản 254.
	probeSystemdMin = 254
	probeCgroupV2   = "/sys/fs/cgroup/cgroup.controllers"
	probeWWWRoot    = "/var/www"
	probeEmail      = "admin@example.com"
	// probeFilePrefix là tiền tố tên mọi file thử đặt vào public của site thử.
	probeFilePrefix = "tpan-probe-"
	probeSecret     = "TPAN_PROBE_RIENG_CUA_B"
)

// probeLimits là trần cố định của lồng trong phép thử (mục 3.9): đủ cho
// WordPress trả trang, đủ thấp để C3 (320 MB) chạm trần.
var probeLimits = account.Limits{RAMMB: 160, CPUPercent: 100, Tasks: 60, PHPWorkers: 3}

var (
	probeHexRE     = regexp.MustCompile(`^[0-9a-f]+$`)
	probeMarkRE    = regexp.MustCompile(`TPAN_PROBE=(CO|KHONG) user=(\S*) n=(-?[0-9]+)`)
	probeHTTPRE    = regexp.MustCompile(`TPAN_HTTP=([0-9]{3})`)
	probeVarRE     = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)
	probeFileRE    = regexp.MustCompile(`^tpan-probe-[a-z0-9-]+\.(php|txt)$`)
	probeSystemdRE = regexp.MustCompile(`^systemd ([0-9]+)`)
)

//go:embed probe/*.php
var probeFS embed.FS

var probeTmpl = template.Must(template.ParseFS(probeFS, "probe/*.php"))

// probePath là một đường tấn công của bảng mục 3.9.
type probePath struct {
	code  string
	title i18n.Text
	// cage: đường chỉ lồng chặn được (C1 đến C5): lộ ở ngoài lồng là
	// needs_cage, không phải exposed.
	cage bool
	// web khác rỗng: đường chỉ có trên stack đó; stack kia ra ô na.
	web WebServer
}

var probePaths = []probePath{
	{code: "L1", title: i18n.T("PHP của A include wp-config.php của B qua OPcache", "A's PHP includes B's wp-config.php through OPcache")},
	{code: "L2", title: i18n.T("opcache_get_status() gọi từ A", "opcache_get_status() called from A")},
	{code: "L3", web: Apache, title: i18n.T(".htaccess của A đặt PHP_ADMIN_VALUE open_basedir=/", "A's .htaccess sets PHP_ADMIN_VALUE open_basedir=/")},
	{code: "L4", web: Apache, title: i18n.T(".htaccess của A chuyển request sang socket PHP của B", "A's .htaccess hands a request to B's PHP socket")},
	{code: "L5", web: Nginx, title: i18n.T("Symlink của A sang file của B, gọi qua nginx", "A symlink of A to a file of B, fetched through nginx")},
	{code: "L6", title: i18n.T("A đọc access.log của B", "A reads B's access.log")},
	{code: "L7", title: i18n.T("PHP của A đếm tiến trình của user khác", "A's PHP counts processes of other users")},
	{code: "L8", title: i18n.T("Vhost lạ trùng tên miền của A", "A foreign vhost with A's domain")},
	{code: "L9", web: Apache, title: i18n.T(".htaccess của A bật FollowSymLinks rồi symlink sang file của B", "A's .htaccess turns on FollowSymLinks, then a symlink to a file of B")},
	{code: "C1", cage: true, title: i18n.T("A liệt kê /var/www: có thư mục của B không", "A lists /var/www: is B's directory there")},
	{code: "C2", cage: true, title: i18n.T("A ghi một file vào /tmp; B có thấy không", "A writes a file to /tmp; can B see it")},
	{code: "C3", cage: true, title: i18n.T("A cấp phát 320 MB", "A allocates 320 MB")},
	{code: "C4", cage: true, title: i18n.T("A đọc tên user tp_* khác qua getent passwd", "A reads other tp_* user names through getent passwd")},
	{code: "C5", cage: true, title: i18n.T("A đọc /proc/self/net/unix: có socket mang slug của B không", "A reads /proc/self/net/unix: is there a socket named after B's slug")},
}

func (p *Probe) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

// rand trả n ký tự hex thường, từ p.Rand hoặc crypto/rand. Kết quả luôn được
// kiểm dạng: nó đi vào tên miền, tên user và đường dẫn.
func (p *Probe) rand(n int) (string, error) {
	var s string
	if p.Rand != nil {
		v, err := p.Rand(n)
		if err != nil {
			return "", fmt.Errorf("sinh số ngẫu nhiên cho phép thử: %w", err)
		}
		s = v
	} else {
		b := make([]byte, (n+1)/2)
		if _, err := rand.Read(b); err != nil {
			return "", fmt.Errorf("sinh số ngẫu nhiên cho phép thử: %w", err)
		}
		s = hex.EncodeToString(b)[:n]
	}
	if len(s) != n || !probeHexRE.MatchString(s) {
		return "", fmt.Errorf("sinh số ngẫu nhiên cho phép thử: %q không phải %d ký tự hex thường", s, n)
	}
	return s, nil
}

func (p *Probe) wordpress() (version, hash string) {
	return (&Sites{WPVersion: p.WPVersion, WPHash: p.WPHash}).wordpress()
}

// stack đọc cấu hình stack; chưa cài thì từ chối như Sites.stack.
func (p *Probe) stack(ctx context.Context) (StackConfig, error) {
	return (&Sites{Settings: p.Settings}).stack(ctx)
}

// probeJournal bọc Journal của panel: ghi job và sự kiện như thường, bỏ qua
// RecordFile và ForgetFile. File của site thử (vhost, pool) bị dọn ngay trong
// lượt; ghi vào managed_files thì health báo lệch cho file không còn.
type probeJournal struct{ engine.Journal }

func (probeJournal) RecordFile(context.Context, string, string, string) error { return nil }
func (probeJournal) ForgetFile(context.Context, string) error                 { return nil }

// journal trả Journal bọc, hoặc nil khi panel không đưa Journal (chỉ xem trước).
func (p *Probe) journal() engine.Journal {
	if p.Journal == nil {
		return nil
	}
	return probeJournal{p.Journal}
}

// probeDomains dựng hai tên miền thử từ đuôi sfx.
func probeDomains(sfx string) (a, b validate.Domain, err error) {
	if a, err = validate.ParseDomain("tpan-probe-a-" + sfx + ".test"); err != nil {
		return a, b, fmt.Errorf("tên miền thử A: %w", err)
	}
	if b, err = validate.ParseDomain("tpan-probe-b-" + sfx + ".test"); err != nil {
		return a, b, fmt.Errorf("tên miền thử B: %w", err)
	}
	if !site.IsProbeDomain(a.String()) || !site.IsProbeDomain(b.String()) {
		return a, b, fmt.Errorf("tên miền thử %s, %s không khớp mẫu %s", a, b, site.ProbeDomainRE)
	}
	return a, b, nil
}

// probeSuffixOf trả đuôi 12 hex của một lượt trong sổ, sau khi kiểm hai tên
// đều khớp mẫu và cùng đuôi. Lỗi nghĩa là dòng sổ không do tpan ghi: không
// dựng bước nào cho nó.
func probeSuffixOf(r cage.ProbeRun) (string, error) {
	sa, okA := strings.CutPrefix(r.DomainA, "tpan-probe-a-")
	sb, okB := strings.CutPrefix(r.DomainB, "tpan-probe-b-")
	sa, _ = strings.CutSuffix(sa, ".test")
	sb, _ = strings.CutSuffix(sb, ".test")
	if !okA || !okB || !site.IsProbeDomain(r.DomainA) || !site.IsProbeDomain(r.DomainB) || sa != sb {
		return "", fmt.Errorf("lượt thử %s trong sổ có tên %q, %q không phải cặp tên miền thử của tpan: Plan dọn không đụng tới", r.ID, r.DomainA, r.DomainB)
	}
	return sa, nil
}

// probeStrayPath là file vhost lạ của L8 trên web server web.
func probeStrayPath(web WebServer, sfx string) string {
	dir := "/etc/nginx/sites-enabled/"
	if web == Apache {
		dir = "/etc/apache2/sites-enabled/"
	}
	return dir + "aaa-tpan-probe-" + sfx + ".conf"
}

// probeTmpFile là file C2 ghi vào /tmp.
func probeTmpFile(sfx string) string { return "/tmp/tpan-probe-" + sfx + "-c2" }

// probeCageReady trả lý do máy không dựng được lồng (điều kiện đầu của
// cage.enable: cgroup v2, systemd từ 254); lý do rỗng là dựng được. Chỉ đọc.
func probeCageReady(ctx context.Context, sys system.System) (code string, why i18n.Text, err error) {
	if _, err := sys.Stat(probeCgroupV2); errors.Is(err, fs.ErrNotExist) {
		return CodeCageCgroupV1, i18n.T(
			"máy không có cgroup v2 (thiếu "+probeCgroupV2+"), nên không dựng được lồng",
			"the machine has no cgroup v2 (missing "+probeCgroupV2+"), so no cage can be built"), nil
	} else if err != nil {
		return "", i18n.Text{}, fmt.Errorf("kiểm cgroup v2: %w", err)
	}
	res, err := sys.Run(ctx, system.Cmd{Name: "systemctl", Args: []string{"--version"}})
	if err != nil {
		return "", i18n.Text{}, fmt.Errorf("đọc bản systemd: %w", err)
	}
	m := probeSystemdRE.FindStringSubmatch(strings.TrimSpace(res.Stdout))
	if m == nil {
		return "", i18n.Text{}, fmt.Errorf("đọc bản systemd: không hiểu dòng đầu của systemctl --version: %q", probeFirstLine(res.Stdout))
	}
	if v, _ := strconv.Atoi(m[1]); v < probeSystemdMin {
		return CodeCageSystemdOld, i18n.T(
			"systemd bản "+m[1]+" (cần từ "+strconv.Itoa(probeSystemdMin)+"), nên không dựng được lồng",
			"systemd version "+m[1]+" (needs "+strconv.Itoa(probeSystemdMin)+" or later), so no cage can be built"), nil
	}
	return "", i18n.Text{}, nil
}

func probeFirstLine(s string) string {
	l, _, _ := strings.Cut(s, "\n")
	return l
}

// ---------------------------------------------------------------- Prepare

// Prepare dựng một lượt thử (mục 3.9). Từ chối: stack_not_installed,
// probe_running (còn dòng sổ "running" hoặc chưa dọn: chạy --cleanup trước),
// args_invalid (Only lạ). Chỉ đọc máy.
func (p *Probe) Prepare(ctx context.Context, o ProbeOptions) (*ProbeJob, error) {
	switch o.Only {
	case "", probeOnlyOutside, probeOnlyInside:
	default:
		return nil, refuse("args_invalid", i18n.T(
			"--only chỉ nhận "+probeOnlyOutside+" hoặc "+probeOnlyInside+" (nhận được \""+o.Only+"\")",
			"--only accepts "+probeOnlyOutside+" or "+probeOnlyInside+" (got \""+o.Only+"\")"))
	}
	if p.Runs == nil {
		return nil, errors.New("thử tấn công: thiếu sổ phép thử (probe_runs)")
	}
	cfg, err := p.stack(ctx)
	if err != nil {
		return nil, err
	}
	pending, err := p.Runs.PendingProbeRuns(ctx)
	if err != nil {
		return nil, fmt.Errorf("đọc sổ phép thử: %w", err)
	}
	if len(pending) > 0 {
		return nil, refuse(CodeProbeRunning, i18n.T(
			fmt.Sprintf("còn %d lượt thử tấn công chưa dọn (lượt %s, %s). Dọn trước bằng: sudo tpan isolation probe --cleanup", len(pending), pending[0].ID, pending[0].State),
			fmt.Sprintf("%d attack probe runs are not cleaned up (run %s, %s). Clean up first with: sudo tpan isolation probe --cleanup", len(pending), pending[0].ID, pending[0].State)))
	}

	// Lồng: chỉ đọc máy, quyết định lúc dựng Plan có bước vào lồng hay không.
	cageCode, cageWhy := "", i18n.Text{}
	if o.Only != probeOnlyOutside {
		if cageCode, cageWhy, err = probeCageReady(ctx, p.Sys); err != nil {
			return nil, err
		}
	}
	// --only trong trên máy không dựng được lồng: lượt duy nhất sẽ "bỏ qua" hết,
	// tức là tạo rồi xoá hai site WordPress mà không thử được gì. Từ chối ngay,
	// trước khi đụng máy.
	if o.Only == probeOnlyInside && cageCode != "" {
		return nil, refuse(cageCode, i18n.T(
			"--only "+probeOnlyInside+" cần máy dựng được lồng: "+cageWhy.Vi+". Chạy lượt ngoài lồng bằng: sudo tpan isolation probe --only "+probeOnlyOutside,
			"--only "+probeOnlyInside+" needs a machine that can build a cage: "+cageWhy.En+". Run the outside pass with: sudo tpan isolation probe --only "+probeOnlyOutside))
	}

	sfx, err := p.rand(12)
	if err != nil {
		return nil, err
	}
	id, err := p.rand(16)
	if err != nil {
		return nil, err
	}
	da, db, err := probeDomains(sfx)
	if err != nil {
		return nil, err
	}
	// Bảng sites không bao giờ có tên thử (Sites.PrepareAdd từ chối mẫu này);
	// có thì là dòng lạ, không đụng.
	if p.Store != nil {
		for _, d := range []validate.Domain{da, db} {
			if _, found, err := p.Store.Site(ctx, d); err != nil {
				return nil, err
			} else if found {
				return nil, fmt.Errorf("bảng sites đã có %s (tên dành cho phép thử): không chạy", d)
			}
		}
	}

	j := &ProbeJob{
		Run: ProbeRunInfo{ID: id, DomainA: da.String(), DomainB: db.String(), Only: o.Only},
		p:   p, cfg: cfg, a: site.LayoutOf(da), b: site.LayoutOf(db), sfx: sfx,
	}
	j.report = ProbeReport{
		ID: id, Web: cfg.Web, DomainA: j.Run.DomainA, DomainB: j.Run.DomainB, Only: o.Only,
		Results: probeResults(cfg.Web, o.Only, cageWhy),
	}
	if j.Plan, err = j.plan(cageCode == ""); err != nil {
		return nil, err
	}
	return j, nil
}

// probeResults dựng đủ các dòng, với những ô biết trước: na theo stack,
// skipped theo --only và theo máy không dựng được lồng.
func probeResults(web WebServer, only string, cageWhy i18n.Text) []ProbeResult {
	skip := func(vi, en string) ProbeCell {
		return ProbeCell{Status: ProbeSkipped, Detail: i18n.T(vi, en)}
	}
	out := make([]ProbeResult, 0, len(probePaths))
	for _, pp := range probePaths {
		r := ProbeResult{Path: pp.code, Title: pp.title}
		if pp.web != "" && pp.web != web {
			na := ProbeCell{Status: ProbeNA, Detail: i18n.T("không thuộc stack "+string(web), "not part of the "+string(web)+" stack")}
			r.Outside, r.Inside = na, na
			out = append(out, r)
			continue
		}
		switch only {
		case probeOnlyInside:
			r.Outside = skip("bỏ qua: --only "+probeOnlyInside, "skipped: --only "+probeOnlyInside)
		case probeOnlyOutside:
			r.Inside = skip("bỏ qua: --only "+probeOnlyOutside, "skipped: --only "+probeOnlyOutside)
		}
		if r.Inside.Status == "" && !cageWhy.IsZero() {
			r.Inside = ProbeCell{Status: ProbeSkipped, Detail: i18n.T("bỏ qua: "+cageWhy.Vi, "skipped: "+cageWhy.En)}
		}
		out = append(out, r)
	}
	return out
}

// probeSite là bản ghi (chỉ trong bộ nhớ, không vào bảng sites) của một site thử.
func (j *ProbeJob) probeSite(l site.Layout) site.Site {
	return site.Site{Domain: l.Domain, Slug: l.Slug, Status: site.Active, PHPVersion: j.cfg.PHPVersion, Htaccess: site.HtaccessSafe}
}

// plan dựng Plan của lượt thử theo thứ tự ở đầu file.
func (j *ProbeJob) plan(cageOK bool) (engine.Plan, error) {
	ss := []engine.Step{&probeRecord{j: j}}
	for i, l := range []site.Layout{j.a, j.b} {
		add, err := j.siteSteps(l, i == 0)
		if err != nil {
			return engine.Plan{}, err
		}
		ss = append(ss, add...)
	}
	if j.cellEmpty("L8") {
		ss = append(ss, &probeL8{j: j})
	}
	if j.Run.Only != probeOnlyInside {
		ss = append(ss, &probePass{j: j})
	}
	if j.Run.Only != probeOnlyOutside && cageOK {
		for _, l := range []site.Layout{j.a, j.b} {
			cs, err := cageEnableSteps(j.probeSite(l), j.cfg, probeLimits)
			if err != nil {
				return engine.Plan{}, fmt.Errorf("dựng bước đưa %s vào lồng: %w", l.Domain, err)
			}
			ss = append(ss, cs...)
		}
		ss = append(ss, &probePass{j: j, inside: true})
	}
	clean, err := probeCleanupSteps(j.cfg, j.sfx, j.b, j.a)
	if err != nil {
		return engine.Plan{}, err
	}
	ss = append(ss, clean...)
	return engine.Plan{
		Kind:   probeKind,
		Target: j.Run.DomainA,
		Title:  i18n.T("Thử tấn công giữa hai site thử "+j.Run.DomainA+" và "+j.Run.DomainB, "Attack probe between two test sites "+j.Run.DomainA+" and "+j.Run.DomainB),
		Steps:  ss,
	}, nil
}

// siteSteps là các bước của site.add cho site thử l (addPlan), trừ hai bước
// cron. Lọc theo ID của siteCronSteps để luôn khớp site.add, kể cả khi addPlan
// đổi. withArchive false: bỏ bước tải WordPress (B dùng bản A vừa tải, và hai
// bước cùng ID trong một Plan làm báo cáo khó đọc).
func (j *ProbeJob) siteSteps(l site.Layout, withArchive bool) ([]engine.Step, error) {
	dbPw, err := sitePassword()
	if err != nil {
		return nil, err
	}
	adminPw, err := sitePassword()
	if err != nil {
		return nil, err
	}
	email, err := validate.ParseEmail(probeEmail)
	if err != nil {
		return nil, err
	}
	d := l.Domain.String()
	// Mật khẩu quản trị của site thử không được in hay giữ ở đâu: site bị xoá
	// ngay trong lượt.
	install := &wpInstallStep{WPInstall: &steps.WPInstall{
		Layout: l, URL: "http://" + d, Title: d,
		AdminUser: siteAdminUser, AdminEmail: email, AdminPassword: adminPw,
	}}
	version, hash := j.p.wordpress()
	plan, err := addPlan(j.cfg.Web, j.cfg.PHPVersion, l, dbPw, version, hash, install, false, site.HtaccessSafe)
	if err != nil {
		return nil, err
	}
	cron, err := siteCronSteps(l)
	if err != nil {
		return nil, err
	}
	drop := map[string]bool{}
	for _, s := range cron {
		drop[s.Info().ID] = true
	}
	var out []engine.Step
	for _, s := range plan.Steps {
		if _, isArchive := s.(*steps.WPArchive); drop[s.Info().ID] || (isArchive && !withArchive) {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

// cellEmpty: ô nào của đường code chưa được điền lúc dựng (na, skipped).
func (j *ProbeJob) cellEmpty(code string) bool {
	r := j.result(code)
	return r != nil && (r.Outside.Status == "" || r.Inside.Status == "")
}

func (j *ProbeJob) result(code string) *ProbeResult {
	for i := range j.report.Results {
		if j.report.Results[i].Path == code {
			return &j.report.Results[i]
		}
	}
	return nil
}

// cell trả ô của đường code ở lượt inside; nil khi ô đã được điền lúc dựng
// (na, skipped) và không được ghi đè.
func (j *ProbeJob) cell(code string, inside bool) *ProbeCell {
	r := j.result(code)
	if r == nil {
		return nil
	}
	c := &r.Outside
	if inside {
		c = &r.Inside
	}
	if c.Status != "" {
		return nil
	}
	return c
}

func (j *ProbeJob) set(code string, inside bool, c ProbeCell) {
	if p := j.cell(code, inside); p != nil {
		*p = c
	}
}

// snapshot trả báo cáo hiện tại với Exposed đã đếm lại.
func (j *ProbeJob) snapshot() ProbeReport {
	r := j.report
	r.Results = append([]ProbeResult(nil), j.report.Results...)
	r.Exposed = 0
	for _, x := range r.Results {
		for _, c := range []ProbeCell{x.Outside, x.Inside} {
			if c.Status == ProbeExposed {
				r.Exposed++
			}
		}
	}
	return r
}

// save ghi báo cáo dở vào sổ (mục 3.9: kết quả vào sổ ngay khi một lượt xong).
func (j *ProbeJob) save(ctx context.Context) error {
	data, err := json.Marshal(j.snapshot())
	if err != nil {
		return fmt.Errorf("mã hoá báo cáo phép thử: %w", err)
	}
	if err := j.p.Runs.SaveProbeReport(ctx, j.Run.ID, data); err != nil {
		return fmt.Errorf("ghi báo cáo dở của lượt thử %s: %w", j.Run.ID, err)
	}
	return nil
}

// ---------------------------------------------------------------- Preview, Execute

// Preview là bản xem trước của lượt thử. Chỉ đọc máy.
func (j *ProbeJob) Preview(ctx context.Context) (engine.Preview, error) {
	return engine.New(j.p.Sys, engine.Options{}).Preview(ctx, j.Plan)
}

// Execute ghi sổ, chạy Plan, ghi kết quả. Trả báo cáo kể cả khi job hỏng giữa
// chừng (các ô chưa chạy để trống) và Report của engine.
//
// Job thành công: FinishProbeRun done, MarkProbeCleaned. Job hỏng sau khi đã
// ghi sổ: chạy ngay Plan probe.cleanup cho đúng hai tên này (bất biến 5), rồi
// FinishProbeRun failed, và MarkProbeCleaned nếu dọn xong. error khác nil khi
// không chạy được job, hoặc khi dọn không xong (người phải chạy --cleanup).
func (j *ProbeJob) Execute(ctx context.Context, onEvent func(engine.Event)) (ProbeReport, engine.Report, error) {
	p := j.p
	if p.Journal == nil {
		return j.snapshot(), engine.Report{}, errors.New("thử tấn công: thiếu Journal, không ghi được lịch sử job")
	}
	j.report.Started = p.now().UTC()
	opts := engine.Options{Journal: p.journal(), OnEvent: onEvent, Now: p.Now}
	rep, runErr := engine.New(p.Sys, opts).Execute(ctx, j.Plan)
	if runErr != nil {
		runErr = fmt.Errorf("chạy %s: %w", probeKind, runErr)
	}
	if !j.recorded {
		// Bước ghi sổ chưa chạy: chưa có gì trên máy mang hai tên này.
		return j.snapshot(), rep, runErr
	}
	// Ghi bằng ctx không bị huỷ: người bấm dừng giữa chừng thì sổ vẫn phải
	// biết kết quả, và việc dọn vẫn phải chạy.
	wctx := context.WithoutCancel(ctx)
	if runErr == nil && rep.Job.Status == engine.JobSucceeded {
		j.report.Finished, j.report.Cleaned = p.now().UTC(), true
		return j.snapshot(), rep, j.finish(wctx, probeDone)
	}

	cleaned, cleanErr := j.cleanupAfterFailure(wctx, onEvent)
	j.report.Finished, j.report.Cleaned = p.now().UTC(), cleaned
	errs := []error{runErr, cleanErr}
	if !cleaned {
		errs = append(errs, fmt.Errorf("lượt thử %s hỏng và chưa dọn xong; dọn bằng: sudo tpan isolation probe --cleanup", j.Run.ID))
	}
	errs = append(errs, j.finish(wctx, probeFailed))
	return j.snapshot(), rep, errors.Join(errs...)
}

// finish ghi trạng thái cuối và (khi đã dọn) đánh dấu đã dọn.
func (j *ProbeJob) finish(ctx context.Context, state string) error {
	snap := j.snapshot()
	data, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("mã hoá báo cáo phép thử: %w", err)
	}
	if err := j.p.Runs.FinishProbeRun(ctx, j.Run.ID, state, data, snap.Finished); err != nil {
		return fmt.Errorf("ghi kết quả lượt thử %s vào sổ: %w", j.Run.ID, err)
	}
	if !snap.Cleaned {
		return nil
	}
	if err := j.p.Runs.MarkProbeCleaned(ctx, j.Run.ID); err != nil {
		return fmt.Errorf("đánh dấu đã dọn lượt thử %s: %w", j.Run.ID, err)
	}
	return nil
}

// cleanupAfterFailure chạy Plan probe.cleanup cho đúng lượt này, trong cùng
// tiến trình (engine của job thử đã nhả khoá).
func (j *ProbeJob) cleanupAfterFailure(ctx context.Context, onEvent func(engine.Event)) (bool, error) {
	p := j.p
	run := cage.ProbeRun{ID: j.Run.ID, DomainA: j.Run.DomainA, DomainB: j.Run.DomainB, State: probeRunning}
	plan, _, err := p.cleanupPlan(j.cfg, []cage.ProbeRun{run})
	if err != nil {
		return false, fmt.Errorf("dựng Plan dọn lượt thử %s: %w", j.Run.ID, err)
	}
	rep, err := engine.New(p.Sys, engine.Options{Journal: p.journal(), OnEvent: onEvent, Now: p.Now}).Execute(ctx, plan)
	if err != nil {
		return false, fmt.Errorf("dọn lượt thử %s: %w", j.Run.ID, err)
	}
	if rep.Job.Status != engine.JobSucceeded {
		return false, fmt.Errorf("dọn lượt thử %s: job %s %s: %s", j.Run.ID, rep.Job.ID, rep.Job.Status, rep.Job.Error)
	}
	return true, nil
}

// ---------------------------------------------------------------- dọn

// PrepareCleanup dựng Plan "probe.cleanup" dọn mọi lượt có Cleaned = false
// trong sổ. nil, nil khi không có gì để dọn. after: MarkProbeCleaned từng lượt.
//
// Sổ được đọc hai lần: ở đây (để dựng bản xem trước, và trả nil khi trống) và
// ở bước đầu của job (probeLedger, đã giữ khoá toàn cục). Chỉ tên mà lần đọc
// thứ hai còn xác nhận mới được dọn: lượt thử đang chạy lúc dựng Plan thì tới
// lúc job này lấy được khoá đã tự dọn xong và bị bỏ qua.
func (p *Probe) PrepareCleanup(ctx context.Context) (*Planned, error) {
	if p.Runs == nil {
		return nil, errors.New("dọn phép thử: thiếu sổ phép thử (probe_runs)")
	}
	rows, err := p.Runs.PendingProbeRuns(ctx)
	if err != nil {
		return nil, fmt.Errorf("đọc sổ phép thử: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	cfg, err := p.stack(ctx)
	if err != nil {
		return nil, err
	}
	var valid, invalid []cage.ProbeRun
	for _, r := range rows {
		if _, err := probeSuffixOf(r); err != nil {
			invalid = append(invalid, r)
			continue
		}
		valid = append(valid, r)
	}
	plan, ledger, err := p.cleanupPlan(cfg, valid)
	if err != nil {
		return nil, err
	}
	after := func(ctx context.Context, _ engine.Report) error {
		// Dòng có tên không khớp mẫu không thể do Prepare ghi (tên luôn sinh
		// theo mẫu), nên trên máy không có gì mang tên đó do phép thử tạo:
		// chỉ đóng dòng sổ, không đụng máy.
		closeRows := append(append([]cage.ProbeRun(nil), ledger.found...), invalid...)
		var errs []error
		for _, r := range closeRows {
			if r.State == probeRunning {
				// Lượt bị ngắt giữa chừng (panel bị tắt): đóng lại là failed,
				// không thì dòng "running" chặn mọi lượt sau.
				if err := p.Runs.FinishProbeRun(ctx, r.ID, probeFailed, r.Report, p.now().UTC()); err != nil {
					errs = append(errs, fmt.Errorf("đóng lượt thử %s: %w", r.ID, err))
					continue
				}
			}
			if err := p.Runs.MarkProbeCleaned(ctx, r.ID); err != nil {
				errs = append(errs, fmt.Errorf("đánh dấu đã dọn lượt thử %s: %w", r.ID, err))
			}
		}
		return errors.Join(errs...)
	}
	return newPlanned(p.Sys, p.journal(), plan, after), nil
}

// cleanupPlan dựng Plan dọn các lượt runs: bước đầu đọc lại sổ trong job, mọi
// bước sau chỉ chạy cho tên bước đầu đã xác nhận. Tên không khớp mẫu: lỗi,
// không dựng bước nào.
func (p *Probe) cleanupPlan(cfg StackConfig, runs []cage.ProbeRun) (engine.Plan, *probeLedger, error) {
	ledger := &probeLedger{runs: p.Runs, want: runs}
	ss := []engine.Step{ledger}
	var ids []string
	for _, r := range runs {
		sfx, err := probeSuffixOf(r)
		if err != nil {
			return engine.Plan{}, nil, err
		}
		da, db, err := probeDomains(sfx)
		if err != nil {
			return engine.Plan{}, nil, err
		}
		if da.String() != r.DomainA || db.String() != r.DomainB {
			return engine.Plan{}, nil, fmt.Errorf("lượt thử %s: tên %q, %q lệch dạng chuẩn", r.ID, r.DomainA, r.DomainB)
		}
		clean, err := probeCleanupSteps(cfg, sfx, site.LayoutOf(db), site.LayoutOf(da))
		if err != nil {
			return engine.Plan{}, nil, err
		}
		for _, s := range clean {
			ss = append(ss, &probeGated{Step: s, ledger: ledger, name: probeStepOwner(s, r)})
		}
		ids = append(ids, r.ID)
	}
	return engine.Plan{
		Kind:   probeCleanupKind,
		Target: strings.Join(ids, ","),
		Title:  i18n.T("Dọn site thử của phép thử tấn công", "Clean up the attack probe's test sites"),
		Steps:  ss,
	}, ledger, nil
}

// probeStepOwner trả tên miền mà bước s thuộc về (để cổng của probeLedger
// quyết định). Bước của cả lượt (vhost lạ L8, file /tmp của C2) thuộc A.
func probeStepOwner(s engine.Step, r cage.ProbeRun) string {
	if o, ok := s.(interface{ probeDomain() string }); ok {
		return o.probeDomain()
	}
	return r.DomainA
}

// probeCleanupSteps là các bước dọn một lượt, cho từng site theo thứ tự ls
// (B rồi A), rồi các thứ của cả lượt. Mọi bước tự bỏ qua khi thứ đó không còn.
func probeCleanupSteps(cfg StackConfig, sfx string, ls ...site.Layout) ([]engine.Step, error) {
	var ss []engine.Step
	for _, l := range ls {
		web, err := steps.SiteWebAbsent(l, string(cfg.Web), cfg.PHPVersion)
		if err != nil {
			return nil, fmt.Errorf("dựng bước dọn %s: %w", l.Domain, err)
		}
		if len(web) != 3 {
			return nil, fmt.Errorf("dựng bước dọn %s: SiteWebAbsent trả %d bước, muốn 3 (link, vhost, pool)", l.Domain, len(web))
		}
		own := []engine.Step{
			web[0], web[1], // gỡ vhost trước: web server thôi trỏ vào PHP của site
			&steps.CageSocket{Layout: l, PHP: cfg.PHPVersion, Disable: true},
			&steps.CageFiles{Layout: l, PHP: cfg.PHPVersion, Remove: true},
			web[2], // pool chung
			&steps.SiteDBAbsent{Layout: l},
			&probeRemoveSiteDir{l: l},
			&steps.RemoveTree{Path: l.LogDir, Under: siteLogRoot},
			&steps.SiteUserAbsent{Layout: l},
			&probeResetFailed{l: l},
			&steps.RemoveTree{Path: l.CageRunDir(), Under: site.CageRunRoot},
		}
		for _, s := range own {
			ss = append(ss, probeOwned{Step: s, domain: l.Domain.String()})
		}
	}
	// Vhost lạ của L8 (cả hai thư mục: lượt có thể chạy trước một lần chuyển
	// web server) và file /tmp của C2. Chỉ file của web server đang dùng mới
	// cần kiểm cú pháp và reload; file của web server kia (không cài) thì
	// chỉ gỡ.
	for _, web := range []WebServer{Nginx, Apache} {
		a := &steps.Absent{
			Path:  probeStrayPath(web, sfx),
			Title: i18n.T("Gỡ vhost lạ của phép thử L8", "Remove the probe's foreign vhost (L8)"),
			Why: i18n.T(
				"Phép thử L8 tạo một vhost lạ trùng tên miền site thử rồi gỡ ngay trong cùng bước. Lượt bị ngắt đúng lúc đó thì file còn lại; để sót thì lần reload sau web server nạp một vhost không ai quản lý.",
				"Probe L8 creates a foreign vhost with the test site's domain and removes it in the same step. A run cut off right then leaves the file behind; left there, the next reload loads a vhost nobody manages."),
		}
		if web == cfg.Web {
			a.Test, a.Reload = probeWebCmds(web)
		}
		ss = append(ss, a)
	}
	ss = append(ss, &steps.Absent{
		Path:  probeTmpFile(sfx),
		Title: i18n.T("Gỡ file /tmp của phép thử C2", "Remove the probe's /tmp file (C2)"),
		Why: i18n.T(
			"Phép thử C2 cho site thử A ghi một file rỗng vào /tmp. Site thử đã bị xoá thì file đó không còn ai dùng.",
			"Probe C2 has test site A write an empty file to /tmp. Once the test site is gone nobody uses that file."),
	})
	return ss, nil
}

// probeWebCmds là lệnh kiểm cú pháp và reload của web server.
func probeWebCmds(web WebServer) (test, reload *system.Cmd) {
	if web == Apache {
		return &system.Cmd{Name: "apachectl", Args: []string{"configtest"}}, &system.Cmd{Name: "systemctl", Args: []string{"reload", "apache2"}}
	}
	return &system.Cmd{Name: "nginx", Args: []string{"-t"}}, &system.Cmd{Name: "systemctl", Args: []string{"reload", "nginx"}}
}

// probeOwned gắn tên miền chủ vào một bước dọn (probeStepOwner đọc).
type probeOwned struct {
	engine.Step
	domain string
}

func (o probeOwned) probeDomain() string { return o.domain }

// probeLedger là bước đầu của Plan dọn: đọc lại sổ bên trong job (đã giữ khoá
// toàn cục) và ghi nhận tên nào được dọn. Không đổi máy.
type probeLedger struct {
	runs   ProbeStore
	want   []cage.ProbeRun
	ok     map[string]bool
	found  []cage.ProbeRun // dòng sổ (đọc trong job) ứng với want
	loaded bool
}

func (l *probeLedger) Info() engine.StepInfo {
	return engine.StepInfo{
		ID:    "probe.ledger",
		Title: i18n.T("Đọc sổ phép thử: chỉ dọn tên có trong sổ và chưa dọn", "Read the probe ledger: clean only names it lists as not yet cleaned"),
		Why: i18n.T(
			"Plan dọn xoá user, DB và thư mục. Nó chỉ được đụng tới đúng hai tên miền mà phép thử đã ghi vào sổ trước khi tạo, đọc lại lúc đã giữ khoá: lượt thử nào xong trong lúc chờ khoá thì không bị dọn lại, và không tên nào ngoài sổ bị xoá.",
			"The cleanup deletes users, databases and directories. It may only touch the two domains the probe wrote to its ledger before creating anything, re-read while holding the lock: a run that finished while waiting is not cleaned twice, and no name outside the ledger is deleted."),
		NoUndo: i18n.T("bước này chỉ đọc sổ", "this step only reads the ledger"),
	}
}

func (l *probeLedger) Check(context.Context, system.System) (bool, error) { return l.loaded, nil }

func (l *probeLedger) Apply(ctx context.Context, _ system.System) error {
	if l.runs == nil {
		return errors.New("đọc sổ phép thử: thiếu sổ")
	}
	rows, err := l.runs.PendingProbeRuns(ctx)
	if err != nil {
		return fmt.Errorf("đọc sổ phép thử: %w", err)
	}
	l.ok, l.found = map[string]bool{}, nil
	for _, w := range l.want {
		for _, r := range rows {
			if r.ID != w.ID || r.Cleaned {
				continue
			}
			for _, d := range []string{r.DomainA, r.DomainB} {
				if site.IsProbeDomain(d) && (d == w.DomainA || d == w.DomainB) {
					l.ok[d] = true
				}
			}
			l.found = append(l.found, r)
		}
	}
	l.loaded = true
	return nil
}

func (l *probeLedger) Undo(context.Context, system.System) error { return nil }

// probeGated chỉ cho bước dọn chạy khi tên của nó đã được probeLedger xác
// nhận. Trước khi ledger chạy (xem trước) thì hỏi thẳng bước gốc.
type probeGated struct {
	engine.Step
	ledger *probeLedger
	name   string
}

func (g *probeGated) allowed() bool { return !g.ledger.loaded || g.ledger.ok[g.name] }

func (g *probeGated) Check(ctx context.Context, sys system.System) (bool, error) {
	if !g.allowed() {
		return true, nil
	}
	return g.Step.Check(ctx, sys)
}

func (g *probeGated) Apply(ctx context.Context, sys system.System) error {
	if !g.allowed() {
		return fmt.Errorf("%s không có trong sổ phép thử (hoặc đã dọn): không đụng tới", g.name)
	}
	return g.Step.Apply(ctx, sys)
}

// probeRemoveSiteDir xoá thư mục của một site thử. Từ M28, root không chạy
// `rm -r` trên đường dẫn trong /var/www (thư mục con do site kiểm soát, có thể
// bị tráo thành symlink): như xoá hẳn một site thật, thư mục được chuyển ra
// chỗ của thùng rác (site.TrashDir, ngoài vùng site) bằng steps.MoveTree rồi
// mới xoá ở đó bằng steps.RemoveTree. Tên miền của site thử là tên dành riêng
// nên không trùng thùng rác của site thật. Không còn gì ở cả hai chỗ thì đã
// xong (Plan dọn chạy lại được).
type probeRemoveSiteDir struct{ l site.Layout }

func (s *probeRemoveSiteDir) steps() (*steps.MoveTree, *steps.RemoveTree) {
	trash := site.TrashDir(s.l)
	return &steps.MoveTree{From: s.l.Dir, To: trash},
		&steps.RemoveTree{Path: trash, Under: site.TrashRoot}
}

func (s *probeRemoveSiteDir) Info() engine.StepInfo {
	mv, rm := s.steps()
	info := rm.Info()
	info.ID = "remove-tree:" + s.l.Dir
	// MoveTree đổi tên bằng Go, không có dòng lệnh: tiêu đề nói bước chuyển.
	info.Title = i18n.T("Xoá hẳn thư mục của site thử "+s.l.Domain.String()+" (chuyển ra "+mv.To+" rồi xoá ở đó)",
		"Delete the probe site directory of "+s.l.Domain.String()+" (moved to "+mv.To+", then deleted there)")
	info.Commands = append(mv.Info().Commands, info.Commands...)
	return info
}

func (s *probeRemoveSiteDir) Check(ctx context.Context, sys system.System) (bool, error) {
	mv, rm := s.steps()
	if _, err := sys.Stat(mv.From); err == nil {
		return false, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, fmt.Errorf("kiểm %s: %w", mv.From, err)
	}
	return rm.Check(ctx, sys)
}

func (s *probeRemoveSiteDir) Apply(ctx context.Context, sys system.System) error {
	mv, rm := s.steps()
	if _, err := sys.Stat(mv.From); err == nil {
		if err := mv.Apply(ctx, sys); err != nil {
			return err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("kiểm %s: %w", mv.From, err)
	}
	return rm.Apply(ctx, sys)
}

func (s *probeRemoveSiteDir) Undo(context.Context, system.System) error { return nil }

// probeResetFailed bỏ trạng thái failed của mọi unit tpan-cage-<slug>-*
// (dịch vụ PHP của lồng, lệnh tạm), để `systemctl --failed` không còn tên
// của site thử.
type probeResetFailed struct{ l site.Layout }

func (s *probeResetFailed) pattern() string { return "tpan-cage-" + s.l.Slug.String() + "-*" }

func (s *probeResetFailed) list() system.Cmd {
	// Dấu * là mẫu tên unit do chính systemctl so, không qua shell nào.
	return system.Cmd{Name: "systemctl", Args: []string{"list-units", "--all", "--state=failed", "--plain", "--no-legend", s.pattern()}}
}

func (s *probeResetFailed) reset() system.Cmd {
	return system.Cmd{Name: "systemctl", Args: []string{"reset-failed", s.pattern()}}
}

func (s *probeResetFailed) Info() engine.StepInfo {
	list, reset := s.list(), s.reset()
	return engine.StepInfo{
		ID:       "probe.reset-failed:" + s.l.Domain.String(),
		Title:    i18n.T("Bỏ trạng thái failed của unit lồng của "+s.l.Domain.String(), "Clear the failed state of "+s.l.Domain.String()+"'s cage units"),
		Why:      i18n.T("C3 trong lồng làm tiến trình PHP bị giết vì chạm trần RAM; systemd giữ tên unit ở trạng thái failed. Để lại thì `systemctl --failed` còn tên của site thử đã xoá.", "C3 inside the cage gets a PHP process killed at the RAM limit; systemd keeps that unit in the failed state. Left alone, `systemctl --failed` keeps listing a deleted test site."),
		Commands: []string{list.String(), reset.String()},
		NoUndo:   i18n.T("trạng thái failed không cần trả lại", "the failed state need not come back"),
	}
}

func (s *probeResetFailed) Check(ctx context.Context, sys system.System) (bool, error) {
	if s.l.Slug.IsZero() {
		return false, errors.New("bỏ trạng thái failed: thiếu slug")
	}
	res, err := sys.Run(ctx, s.list())
	if err != nil {
		return false, fmt.Errorf("liệt kê unit failed của %s: %w", s.l.Domain, err)
	}
	return strings.TrimSpace(res.Stdout) == "", nil
}

func (s *probeResetFailed) Apply(ctx context.Context, sys system.System) error {
	if _, err := sys.Run(ctx, s.reset()); err != nil {
		return fmt.Errorf("bỏ trạng thái failed của %s: %w", s.pattern(), err)
	}
	return nil
}

func (s *probeResetFailed) Undo(context.Context, system.System) error { return nil }

// ---------------------------------------------------------------- bước của lượt thử

// probeRecord ghi dòng sổ, TRƯỚC mọi bước tạo (mục 3.9 bước 1).
type probeRecord struct{ j *ProbeJob }

func (s *probeRecord) Info() engine.StepInfo {
	j := s.j
	return engine.StepInfo{
		ID:    "probe.record:" + j.Run.ID,
		Title: i18n.T("Ghi lượt thử "+j.Run.ID+" vào sổ trước khi tạo gì", "Record probe run "+j.Run.ID+" in the ledger before creating anything"),
		Why: i18n.T(
			"Sổ là chỗ duy nhất nói hai tên miền "+j.Run.DomainA+", "+j.Run.DomainB+" là của phép thử. Panel bị tắt giữa chừng thì lần dọn sau đọc sổ để biết phải xoá gì; không ghi trước thì đồ đã tạo bị bỏ rơi.",
			"The ledger is the only record that "+j.Run.DomainA+" and "+j.Run.DomainB+" belong to the probe. If the panel stops halfway, the next cleanup reads it to know what to delete; without it, whatever was created is left behind."),
		NoUndo: i18n.T("dòng sổ phải còn để Plan dọn tìm thấy hai tên này", "the ledger row must stay so the cleanup can find these two names"),
	}
}

func (s *probeRecord) Check(ctx context.Context, _ system.System) (bool, error) {
	_, ok, err := s.j.p.Runs.ProbeRun(ctx, s.j.Run.ID)
	return ok, err
}

func (s *probeRecord) Apply(ctx context.Context, _ system.System) error {
	j := s.j
	r := cage.ProbeRun{ID: j.Run.ID, Started: j.p.now().UTC(), State: probeRunning, DomainA: j.Run.DomainA, DomainB: j.Run.DomainB, Mode: j.Run.Only}
	if err := j.p.Runs.StartProbeRun(ctx, r); err != nil {
		return fmt.Errorf("ghi lượt thử %s vào sổ: %w", j.Run.ID, err)
	}
	j.recorded = true
	return nil
}

func (s *probeRecord) Undo(context.Context, system.System) error { return nil }

// probeL8 là L8 trong MỘT bước: tạo vhost lạ trùng tên miền của A, gọi
// checks.ServerNameUnique với Layout của hai site thử, gỡ file ngay. Không
// reload: `nginx -T` và `apache2ctl -S` đọc cấu hình từ đĩa, nên vhost lạ
// không bao giờ được web server nạp.
type probeL8 struct {
	j    *ProbeJob
	done bool
}

func (s *probeL8) path() string { return probeStrayPath(s.j.cfg.Web, s.j.sfx) }

func (s *probeL8) Info() engine.StepInfo {
	j := s.j
	cmd := "nginx -T"
	if j.cfg.Web == Apache {
		cmd = "apache2ctl -S"
	}
	return engine.StepInfo{
		ID:    "probe.l8:" + j.Run.DomainA,
		Title: i18n.T("L8: thêm vhost lạ trùng tên "+j.Run.DomainA+", kiểm health có chỉ ra không, gỡ ngay", "L8: add a foreign vhost with "+j.Run.DomainA+", check health points to it, remove it at once"),
		Why: i18n.T(
			"Ai đó thêm tay một vhost trùng tên miền thì tên miền trả trang lạ mà kiểm cú pháp vẫn đạt. Bước này chứng minh phép kiểm web.server_name.unique chỉ ra đúng file lạ. File "+s.path()+" không được reload và bị gỡ trong cùng bước.",
			"If someone adds a vhost with the same domain by hand, the domain serves a foreign page while the syntax check still passes. This step proves web.server_name.unique points to the exact foreign file. The file "+s.path()+" is never reloaded and is removed in the same step."),
		Commands: []string{cmd},
	}
}

func (s *probeL8) Check(context.Context, system.System) (bool, error) { return s.done, nil }

func (s *probeL8) Apply(ctx context.Context, sys system.System) (err error) {
	j := s.j
	path := s.path()
	if err := j.guardRun(ctx); err != nil {
		return err
	}
	dom := j.Run.DomainA
	var body string
	if j.cfg.Web == Apache {
		body = "<VirtualHost *:80>\n    ServerName " + dom + "\n</VirtualHost>\n"
	} else {
		body = "server {\n    listen 80;\n    server_name " + dom + ";\n    return 404;\n}\n"
	}
	if err := sys.WriteFileAtomic(path, []byte("# tpan isolation probe L8: file lạ, gỡ ngay trong bước này\n"+body), 0o644, system.Root); err != nil {
		return fmt.Errorf("L8: ghi vhost lạ %s: %w", path, err)
	}
	defer func() {
		if rerr := probeRemove(sys, path); rerr != nil {
			err = errors.Join(err, fmt.Errorf("L8: gỡ vhost lạ %s: %w", path, rerr))
		}
	}()
	res := checks.ServerNameUnique(string(j.cfg.Web), []site.Layout{j.a, j.b}).Run(ctx, sys)
	name := "aaa-tpan-probe-" + j.sfx + ".conf"
	var c ProbeCell
	switch {
	case res.Status == checks.Fail && probeResultMentions(res, name):
		c = ProbeCell{Status: ProbeBlocked, Detail: i18n.T("phát hiện: web.server_name.unique chỉ ra "+name, "detected: web.server_name.unique points to "+name)}
	case res.Status == checks.Fail:
		c = ProbeCell{Status: ProbeExposed, Detail: i18n.T("web.server_name.unique báo lỗi nhưng không chỉ ra "+name, "web.server_name.unique fails but does not point to "+name)}
	case res.Status == checks.Pass:
		c = ProbeCell{Status: ProbeExposed, Detail: i18n.T("web.server_name.unique đạt dù có vhost lạ trùng tên "+dom, "web.server_name.unique passes although a foreign vhost declares "+dom)}
	default:
		c = ProbeCell{Status: ProbeSkipped, Detail: i18n.T("không kết luận được: "+res.Summary.Vi, "no conclusion: "+res.Summary.En)}
	}
	c.Evidence = res.Evidence
	j.set("L8", false, c)
	in := c
	if j.Run.Only == "" {
		in.Detail = i18n.T(c.Detail.Vi+" (như ngoài lồng, không chạy lại)", c.Detail.En+" (same as outside, not run again)")
	}
	j.set("L8", true, in)
	s.done = true
	return nil
}

func (s *probeL8) Undo(_ context.Context, sys system.System) error { return probeRemove(sys, s.path()) }

// probeResultMentions: kết quả phép kiểm có nhắc tới tên file name.
func probeResultMentions(r checks.Result, name string) bool {
	parts := []string{r.Summary.Vi, r.Summary.En, r.Cause.Vi, r.Cause.En}
	parts = append(parts, r.ManualFix...)
	for _, e := range r.Evidence {
		parts = append(parts, e.Output)
	}
	for _, p := range parts {
		if strings.Contains(p, name) {
			return true
		}
	}
	return false
}

// probeRemove gỡ một file hay symlink; không có thì thôi.
func probeRemove(sys system.System, path string) error {
	if err := sys.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// guardRun đọc dòng sổ của lượt này và đòi hai tên khớp đúng tên của job.
func (j *ProbeJob) guardRun(ctx context.Context) error {
	r, ok, err := j.p.Runs.ProbeRun(ctx, j.Run.ID)
	if err != nil {
		return fmt.Errorf("đọc sổ phép thử: %w", err)
	}
	if !ok || r.DomainA != j.Run.DomainA || r.DomainB != j.Run.DomainB {
		return fmt.Errorf("lượt thử %s không có trong sổ với đúng hai tên %s, %s: không ghi gì", j.Run.ID, j.Run.DomainA, j.Run.DomainB)
	}
	if _, err := probeSuffixOf(r); err != nil {
		return err
	}
	return nil
}

// guardPublic trả thư mục public của site thử domain (A hoặc B của lượt
// này) sau khi kiểm: tên có trong sổ, khớp mẫu, thư mục bằng đúng (dấu bằng,
// không so tiền tố) site.LayoutOf(tên trong sổ).Public, và thuộc user của site
// thử. Mọi file thử chỉ được ghi vào thư mục trả về ở đây.
func (j *ProbeJob) guardPublic(ctx context.Context, sys system.System, l site.Layout) (string, error) {
	if err := j.guardRun(ctx); err != nil {
		return "", err
	}
	r, _, err := j.p.Runs.ProbeRun(ctx, j.Run.ID)
	if err != nil {
		return "", fmt.Errorf("đọc sổ phép thử: %w", err)
	}
	var inLedger string
	switch l.Domain.String() {
	case r.DomainA:
		inLedger = r.DomainA
	case r.DomainB:
		inLedger = r.DomainB
	default:
		return "", fmt.Errorf("%s không phải site thử của lượt %s: không ghi gì", l.Domain, j.Run.ID)
	}
	d, err := validate.ParseDomain(inLedger)
	if err != nil {
		return "", fmt.Errorf("tên trong sổ %q: %w", inLedger, err)
	}
	want := site.LayoutOf(d)
	if l.Public != want.Public {
		return "", fmt.Errorf("thư mục %s khác %s của tên trong sổ: không ghi gì", l.Public, want.Public)
	}
	fi, err := sys.Stat(want.Public)
	if err != nil {
		return "", fmt.Errorf("kiểm %s: %w", want.Public, err)
	}
	if !fi.Mode.IsDir() || fi.Owner.User != want.User {
		return "", fmt.Errorf("%s không phải thư mục của %s (chủ %s): không ghi gì", want.Public, want.User, fi.Owner.User)
	}
	return want.Public, nil
}

// probeVars là các giá trị tpan điền vào file PHP thử lúc ghi.
type probeVars struct {
	ADir, BDir, BWPConfig, BAccessLog, BDomain, BSlug, TmpFile string
}

func (j *ProbeJob) vars() probeVars {
	return probeVars{
		ADir: j.a.Dir, BDir: j.b.Dir, BWPConfig: j.b.WPConfig, BAccessLog: j.b.AccessLog,
		BDomain: j.b.Domain.String(), BSlug: j.b.Slug.String(), TmpFile: probeTmpFile(j.sfx),
	}
}

// probeRender sinh file PHP thử name. Mọi giá trị điền vào nằm trong chuỗi
// PHP nháy đơn, nên phải qua probeVarRE (không nháy, không gạch chéo ngược).
func probeRender(name string, v probeVars) ([]byte, error) {
	for _, s := range []string{v.ADir, v.BDir, v.BWPConfig, v.BAccessLog, v.BDomain, v.BSlug, v.TmpFile} {
		if !probeVarRE.MatchString(s) {
			return nil, fmt.Errorf("file thử %s: giá trị %q có ký tự lạ", name, s)
		}
	}
	var b bytes.Buffer
	if err := probeTmpl.ExecuteTemplate(&b, name, v); err != nil {
		return nil, fmt.Errorf("sinh file thử %s: %w", name, err)
	}
	return b.Bytes(), nil
}

// probeFiles nhớ mọi file thử đã đặt để gỡ sạch cuối lượt.
type probeFiles struct {
	sys   system.System
	paths []string
}

func (f *probeFiles) put(dir, name string, data []byte, owner system.Owner) (string, error) {
	if !probeFileRE.MatchString(name) {
		return "", fmt.Errorf("tên file thử %q không hợp lệ", name)
	}
	p := dir + "/" + name
	if err := f.sys.WriteFileAtomic(p, data, 0o640, owner); err != nil {
		return "", fmt.Errorf("ghi file thử %s: %w", p, err)
	}
	f.paths = append(f.paths, p)
	return p, nil
}

func (f *probeFiles) link(ctx context.Context, target, dir, name string, owner system.Owner) error {
	if !probeFileRE.MatchString(name) {
		return fmt.Errorf("tên symlink thử %q không hợp lệ", name)
	}
	p := dir + "/" + name
	_ = probeRemove(f.sys, p)
	// Tạo bằng chính user của A (ln -s, argv): symlink thuộc về A đúng như khi A
	// tự tạo, để SymLinksIfOwnerMatch, disable_symlinks so đúng. Không tạo bằng
	// root rồi đổi chủ: tpan không đổi chủ một symlink trong thư mục site (M28).
	// -T: p còn sót là thư mục thì ln báo lỗi thay vì tạo link bên trong nó
	// (phép thử sẽ đo nhầm chỗ mà không biết).
	cmd := system.Cmd{Name: "ln", Args: []string{"-s", "-T", "--", target, p}, AsUser: owner.User}
	if res, err := f.sys.Run(ctx, cmd); err != nil {
		return fmt.Errorf("tạo symlink thử %s: %w (%s)", p, err, strings.TrimSpace(res.Stderr))
	}
	f.paths = append(f.paths, p)
	return nil
}

func (f *probeFiles) removeAll() error {
	var errs []error
	for _, p := range f.paths {
		if err := probeRemove(f.sys, p); err != nil {
			errs = append(errs, fmt.Errorf("gỡ file thử %s: %w", p, err))
		}
	}
	f.paths = nil
	return errors.Join(errs...)
}

// probeHTTP là một lần gọi file thử qua web server.
type probeHTTP struct {
	code   string // mã HTTP; "000" khi không có trả lời
	mark   string // "CO", "KHONG" hoặc "" (không có dòng TPAN_PROBE)
	user   string
	n      int
	secret bool // thân có dấu của file riêng của B (L5, L9)
	ev     checks.Evidence
}

// probeGet gọi http://<l>/<file> qua 127.0.0.1 (curl --resolve, argv). Bằng
// chứng chỉ giữ dòng kết quả và mã HTTP, không giữ thân trang. error chỉ khi
// curl không chạy được.
func probeGet(ctx context.Context, sys system.System, l site.Layout, file string) (probeHTTP, error) {
	dom := l.Domain.String()
	cmd := system.Cmd{Name: "curl", Args: []string{
		"--silent", "--show-error", "--max-time", "60",
		"--resolve", dom + ":80:127.0.0.1",
		"--write-out", "\nTPAN_HTTP=%{http_code}",
		"http://" + dom + "/" + file,
	}}
	res, err := sys.Run(ctx, cmd)
	code := 0
	if err != nil {
		var ee *system.ExitError
		if !errors.As(err, &ee) {
			return probeHTTP{}, fmt.Errorf("gọi %s: %w", cmd.String(), err)
		}
		code = ee.Code
	}
	out := probeHTTP{code: "000"}
	if m := probeHTTPRE.FindStringSubmatch(res.Stdout); m != nil {
		out.code = m[1]
	}
	shown := "HTTP " + out.code
	if m := probeMarkRE.FindStringSubmatch(res.Stdout); m != nil {
		out.mark, out.user = m[1], m[2]
		out.n, _ = strconv.Atoi(m[3])
		shown = m[0] + " | " + shown
	}
	out.secret = strings.Contains(res.Stdout, probeSecret)
	if out.secret {
		shown += " | " + probeSecret
	}
	out.ev = checks.Evidence{Command: system.Describe(sys, cmd), ExitCode: code, Output: shown}
	return out, nil
}

// probePass là một lượt chạy mọi đường (ngoài hay trong lồng). Ghi báo cáo vào
// sổ ngay khi xong.
type probePass struct {
	j      *ProbeJob
	inside bool
	done   bool
}

func (s *probePass) where() (vi, en string) {
	if s.inside {
		return "trong lồng", "inside the cage"
	}
	return "ngoài lồng", "outside the cage"
}

func (s *probePass) Info() engine.StepInfo {
	j := s.j
	vi, en := s.where()
	id := "probe.pass:" + probeOnlyOutside
	if s.inside {
		id = "probe.pass:" + probeOnlyInside
	}
	return engine.StepInfo{
		ID:    id,
		Title: i18n.T("Chạy các đường tấn công "+vi+" (L1 đến L9, C1 đến C5)", "Run the attack paths "+en+" (L1 to L9, C1 to C5)"),
		Why: i18n.T(
			"Mỗi đường là một file PHP nhỏ đặt vào "+j.a.Public+", gọi qua web server như một request thật; nó chỉ in CÓ/KHÔNG, tên user và số đếm. Kết quả vào sổ ngay khi lượt xong, nên lượt sau hỏng thì kết quả lượt này vẫn còn. Mọi file thử bị gỡ cuối lượt.",
			"Each path is a small PHP file placed in "+j.a.Public+" and called through the web server like a real request; it only prints YES/NO, a user name and a count. Results go to the ledger as soon as the pass ends, so they survive a later failure. Every probe file is removed at the end of the pass."),
		Commands: []string{"curl --silent --show-error --max-time 60 --resolve " + j.Run.DomainA + ":80:127.0.0.1 http://" + j.Run.DomainA + "/" + probeFilePrefix + "<đường>.php"},
	}
}

func (s *probePass) Check(context.Context, system.System) (bool, error) { return s.done, nil }

func (s *probePass) Undo(_ context.Context, _ system.System) error { return nil }

func (s *probePass) Apply(ctx context.Context, sys system.System) (err error) {
	j := s.j
	pubA, err := j.guardPublic(ctx, sys, j.a)
	if err != nil {
		return err
	}
	pubB, err := j.guardPublic(ctx, sys, j.b)
	if err != nil {
		return err
	}
	files := &probeFiles{sys: sys}
	defer func() {
		if rerr := files.removeAll(); rerr != nil {
			err = errors.Join(err, rerr)
		}
	}()
	ownA := system.Owner{User: j.a.User, Group: site.WebGroup}
	ownB := system.Owner{User: j.b.User, Group: site.WebGroup}
	v := j.vars()

	put := func(dir, tmpl string, own system.Owner) error {
		data, err := probeRender(tmpl, v)
		if err != nil {
			return err
		}
		_, err = files.put(dir, probeFilePrefix+tmpl, data, own)
		return err
	}
	for _, t := range []string{"l1.php", "l2.php", "l6.php", "l7.php", "c1.php", "c2.php", "c2x.php", "c3.php", "c4.php", "c5.php"} {
		if err := put(pubA, t, ownA); err != nil {
			return err
		}
	}
	if err := put(pubB, "c2b.php", ownB); err != nil {
		return err
	}
	if j.cfg.Web == Apache {
		for _, t := range []string{"l3.php", "l4.php"} {
			if err := put(pubA, t, ownA); err != nil {
				return err
			}
		}
		if err := put(pubB, "l4b.php", ownB); err != nil {
			return err
		}
	}
	// File riêng của B (0640, chủ B) cho hai đường symlink.
	if _, err := files.put(pubB, probeFilePrefix+"rieng.txt", []byte(probeSecret+"\n"), ownB); err != nil {
		return err
	}

	r := &probeRunner{j: j, s: s, sys: sys, files: files, pubA: pubA, pubB: pubB, ownA: ownA}
	if err := r.run(ctx); err != nil {
		return err
	}
	if err := j.save(ctx); err != nil {
		return err
	}
	s.done = true
	return nil
}

// probeRunner chạy từng đường của một lượt.
type probeRunner struct {
	j          *ProbeJob
	s          *probePass
	sys        system.System
	files      *probeFiles
	pubA, pubB string
	ownA       system.Owner
}

func (r *probeRunner) get(ctx context.Context, l site.Layout, name string) (probeHTTP, error) {
	return probeGet(ctx, r.sys, l, name)
}

// leakStatus là trạng thái của một đường đã lộ: C ở ngoài lồng là needs_cage
// (đúng như đặc tả: chỉ lồng chặn được), còn lại là exposed.
func (r *probeRunner) leakStatus(code string) string {
	for _, pp := range probePaths {
		if pp.code == code && pp.cage && !r.s.inside {
			return ProbeNeedsCage
		}
	}
	return ProbeExposed
}

// phpCell chuyển kết quả của một file PHP thử thành ô: CO là lộ, KHONG là bị
// chặn, không có dòng kết quả là không kết luận (skipped kèm mã HTTP), trừ
// khi noAnswerBlocked (Apache từ chối .htaccess bằng 500: chính A hỏng, B
// không sao).
func (r *probeRunner) phpCell(code string, h probeHTTP, leak, ok i18n.Text, noAnswerBlocked bool) ProbeCell {
	c := ProbeCell{Evidence: []checks.Evidence{h.ev}}
	switch {
	case h.mark == "CO":
		c.Status, c.Detail = r.leakStatus(code), leak
	case h.mark == "KHONG":
		c.Status, c.Detail = ProbeBlocked, ok
	case noAnswerBlocked:
		c.Status, c.Detail = ProbeBlocked, i18n.T(ok.Vi+" (site A tự hỏng: HTTP "+h.code+")", ok.En+" (site A breaks itself: HTTP "+h.code+")")
	default:
		c.Status, c.Detail = ProbeSkipped, i18n.T("không kết luận được: trả lời lạ, HTTP "+h.code, "no conclusion: unexpected answer, HTTP "+h.code)
	}
	return c
}

// want: ô này còn phải chạy ở lượt hiện tại.
func (r *probeRunner) want(code string) bool { return r.j.cell(code, r.s.inside) != nil }

func (r *probeRunner) set(code string, c ProbeCell) { r.j.set(code, r.s.inside, c) }

func (r *probeRunner) run(ctx context.Context) error {
	j := r.j
	a, b := j.a, j.b
	n := func(h probeHTTP) string { return strconv.Itoa(h.n) }

	// Trang chủ của B chạy trước: wp-config.php của B vào OPcache (L1), B có
	// dòng trong access.log (L6).
	if _, err := r.get(ctx, b, ""); err != nil {
		return err
	}

	if r.want("L1") {
		var h probeHTTP
		for i := 0; i < 3; i++ {
			if _, err := r.get(ctx, b, ""); err != nil {
				return err
			}
			var err error
			if h, err = r.get(ctx, a, probeFilePrefix+"l1.php"); err != nil {
				return err
			}
			if h.mark == "CO" {
				break
			}
		}
		r.set("L1", r.phpCell("L1", h,
			i18n.T("PHP của A include wp-config.php của B: lấy được DB_PASSWORD", "A's PHP includes B's wp-config.php: DB_PASSWORD is readable"),
			i18n.T("PHP của A include wp-config.php của B: không lấy được (3 lần thử)", "A's PHP includes B's wp-config.php: not readable (3 tries)"), false))
	}
	if r.want("L2") {
		h, err := r.get(ctx, a, probeFilePrefix+"l2.php")
		if err != nil {
			return err
		}
		r.set("L2", r.phpCell("L2", h,
			i18n.T("opcache_get_status() từ A chạy được, thấy "+n(h)+" file của B", "opcache_get_status() runs from A and sees "+n(h)+" files of B"),
			i18n.T("opcache_get_status() từ A bị chặn", "opcache_get_status() from A is blocked"), false))
	}
	if j.cfg.Web == Apache {
		if err := r.apache(ctx); err != nil {
			return err
		}
	}
	if j.cfg.Web == Nginx && r.want("L5") {
		if err := r.files.link(ctx, r.pubB+"/"+probeFilePrefix+"rieng.txt", r.pubA, probeFilePrefix+"lay.txt", r.ownA); err != nil {
			return err
		}
		h, err := r.get(ctx, a, probeFilePrefix+"lay.txt")
		if err != nil {
			return err
		}
		c := ProbeCell{Status: ProbeBlocked, Evidence: []checks.Evidence{h.ev},
			Detail: i18n.T("symlink của A sang file 0640 của B qua nginx: HTTP "+h.code+", không lộ", "A's symlink to B's 0640 file through nginx: HTTP "+h.code+", not exposed")}
		if h.secret {
			c.Status, c.Detail = ProbeExposed, i18n.T("symlink của A sang file 0640 của B qua nginx: nội dung của B bị trả về", "A's symlink to B's 0640 file through nginx: B's content is served")
		}
		r.set("L5", c)
	}
	if r.want("L6") {
		if _, err := r.get(ctx, b, ""); err != nil {
			return err
		}
		h, err := r.get(ctx, a, probeFilePrefix+"l6.php")
		if err != nil {
			return err
		}
		r.set("L6", r.phpCell("L6", h,
			i18n.T("user của A đọc được access.log của B", "A's user can read B's access.log"),
			i18n.T("user của A không đọc được access.log của B", "A's user cannot read B's access.log"), false))
	}
	if r.want("L7") {
		h, err := r.get(ctx, a, probeFilePrefix+"l7.php")
		if err != nil {
			return err
		}
		r.set("L7", r.phpCell("L7", h,
			i18n.T("PHP của A thấy "+n(h)+" tiến trình của user khác", "A's PHP sees "+n(h)+" processes of other users"),
			i18n.T("PHP của A không thấy tiến trình nào của user khác", "A's PHP sees no process of another user"), false))
	}
	if r.want("C1") {
		h, err := r.get(ctx, a, probeFilePrefix+"c1.php")
		if err != nil {
			return err
		}
		r.set("C1", r.phpCell("C1", h,
			i18n.T("A thấy thư mục của B trong /var/www ("+n(h)+" mục)", "A sees B's directory in /var/www ("+n(h)+" entries)"),
			i18n.T("A không thấy thư mục của B trong /var/www ("+n(h)+" mục)", "A does not see B's directory in /var/www ("+n(h)+" entries)"), false))
	}
	if r.want("C2") {
		if err := r.c2(ctx); err != nil {
			return err
		}
	}
	if r.want("C4") {
		h, err := r.get(ctx, a, probeFilePrefix+"c4.php")
		if err != nil {
			return err
		}
		r.set("C4", r.phpCell("C4", h,
			i18n.T("A đọc ra "+n(h)+" user tp_* khác", "A reads "+n(h)+" other tp_* users"),
			i18n.T("A không đọc ra user tp_* nào khác", "A reads no other tp_* user"), false))
	}
	if r.want("C5") {
		h, err := r.get(ctx, a, probeFilePrefix+"c5.php")
		if err != nil {
			return err
		}
		r.set("C5", r.phpCell("C5", h,
			i18n.T("A thấy "+n(h)+" socket mang slug của B trong /proc/self/net/unix", "A sees "+n(h)+" sockets named after B's slug in /proc/self/net/unix"),
			i18n.T("A không thấy socket nào mang slug của B", "A sees no socket named after B's slug"), false))
	}
	// C3 chạy sau cùng: trong lồng, chạm trần RAM có thể làm systemd dừng cả
	// dịch vụ PHP của A; các đường khác không phải chờ socket đánh thức lại nó.
	if r.want("C3") {
		if err := r.c3(ctx); err != nil {
			return err
		}
	}
	return nil
}

// c2: A ghi /tmp, B xem, A xoá.
func (r *probeRunner) c2(ctx context.Context) error {
	a, b := r.j.a, r.j.b
	w, err := r.get(ctx, a, probeFilePrefix+"c2.php")
	if err != nil {
		return err
	}
	if w.mark != "CO" {
		r.set("C2", ProbeCell{Status: ProbeSkipped, Evidence: []checks.Evidence{w.ev},
			Detail: i18n.T("A không ghi được file vào /tmp (HTTP "+w.code+"), không kết luận", "A could not write to /tmp (HTTP "+w.code+"), no conclusion")})
		return nil
	}
	h, err := r.get(ctx, b, probeFilePrefix+"c2b.php")
	if err != nil {
		return err
	}
	x, err := r.get(ctx, a, probeFilePrefix+"c2x.php")
	if err != nil {
		return err
	}
	c := r.phpCell("C2", h,
		i18n.T("B thấy file A vừa ghi vào /tmp (dùng chung /tmp)", "B sees the file A just wrote to /tmp (shared /tmp)"),
		i18n.T("B không thấy file A vừa ghi vào /tmp", "B does not see the file A just wrote to /tmp"), false)
	c.Evidence = []checks.Evidence{w.ev, h.ev, x.ev}
	r.set("C2", c)
	return nil
}

// c3: A cấp phát 320 MB. Ngoài lồng chỉ chạy khi máy còn ≥ 1 GB trống. Trong
// lồng: đạt khi tiến trình không xong việc, oom_kill của slice tăng, B vẫn 200.
func (r *probeRunner) c3(ctx context.Context) error {
	j := r.j
	if !r.s.inside {
		free, err := probeMemAvailable(r.sys)
		if err != nil {
			r.set("C3", ProbeCell{Status: ProbeSkipped, Detail: i18n.T("bỏ qua: không đọc được RAM trống ("+err.Error()+")", "skipped: could not read free RAM ("+err.Error()+")")})
			return nil
		}
		if free < probeC3MinFree {
			mb := strconv.FormatInt(free>>20, 10)
			r.set("C3", ProbeCell{Status: ProbeSkipped, Detail: i18n.T(
				"bỏ qua: máy chỉ còn "+mb+" MB RAM trống (cần ít nhất 1024 MB để cấp phát 320 MB ngoài lồng mà không làm hại máy)",
				"skipped: the machine has only "+mb+" MB of free RAM (needs at least 1024 MB to allocate 320 MB outside a cage without harming it)")})
			return nil
		}
		h, err := r.get(ctx, j.a, probeFilePrefix+"c3.php")
		if err != nil {
			return err
		}
		r.set("C3", r.phpCell("C3", h,
			i18n.T("A cấp phát được "+strconv.Itoa(h.n)+" MB: không có trần RAM nào giữ", "A allocated "+strconv.Itoa(h.n)+" MB: no RAM limit holds it"),
			i18n.T("A không cấp phát được 320 MB", "A could not allocate 320 MB"), false))
		return nil
	}
	before, err := cage.ReadUsage(r.sys, j.a, j.p.now())
	if err != nil {
		return fmt.Errorf("C3: đọc cgroup của %s: %w", j.a.Domain, err)
	}
	h, err := r.get(ctx, j.a, probeFilePrefix+"c3.php")
	if err != nil {
		return err
	}
	after, err := cage.ReadUsage(r.sys, j.a, j.p.now())
	if err != nil {
		return fmt.Errorf("C3: đọc cgroup của %s: %w", j.a.Domain, err)
	}
	hb, err := r.get(ctx, j.b, "")
	if err != nil {
		return err
	}
	oom := strconv.FormatInt(after.OOMKills-before.OOMKills, 10)
	c := ProbeCell{Evidence: []checks.Evidence{h.ev, hb.ev,
		{Command: "cat " + j.a.CageCgroup() + "/memory.events", Output: "oom_kill " + strconv.FormatInt(before.OOMKills, 10) + " -> " + strconv.FormatInt(after.OOMKills, 10)}}}
	switch h.mark {
	case "CO":
		c.Status, c.Detail = ProbeExposed, i18n.T("A cấp phát được "+strconv.Itoa(h.n)+" MB trong lồng trần 160 MB", "A allocated "+strconv.Itoa(h.n)+" MB inside a cage limited to 160 MB")
	default:
		c.Status, c.Detail = ProbeBlocked, i18n.T(
			"tiến trình của A bị chặn (HTTP "+h.code+"), oom_kill tăng "+oom+", B trả HTTP "+hb.code,
			"A's process is stopped (HTTP "+h.code+"), oom_kill rose by "+oom+", B answers HTTP "+hb.code)
	}
	r.set("C3", c)
	return nil
}

// probeMemAvailable đọc MemAvailable (byte) từ /proc/meminfo.
func probeMemAvailable(sys system.System) (int64, error) {
	b, err := sys.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	for _, ln := range strings.Split(string(b), "\n") {
		rest, ok := strings.CutPrefix(ln, "MemAvailable:")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		if len(f) == 0 {
			break
		}
		kb, err := strconv.ParseInt(f[0], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("dòng MemAvailable lạ: %q", ln)
		}
		return kb * 1024, nil
	}
	return 0, errors.New("/proc/meminfo không có dòng MemAvailable")
}

// apache chạy L3, L4, L9: mỗi phép ghi tạm vài dòng lên đầu .htaccess của A,
// gọi, rồi trả .htaccess về đúng như cũ.
func (r *probeRunner) apache(ctx context.Context) error {
	j := r.j
	a, b := j.a, j.b
	ht := r.pubA + "/.htaccess"
	orig, err := r.sys.ReadFile(ht)
	existed := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("đọc %s: %w", ht, err)
	}
	perm, own := fs.FileMode(0o640), r.ownA
	if existed {
		fi, err := r.sys.Stat(ht)
		if err != nil {
			return fmt.Errorf("kiểm %s: %w", ht, err)
		}
		perm, own = fi.Mode.Perm(), fi.Owner
	}
	restore := func() error {
		if !existed {
			return probeRemove(r.sys, ht)
		}
		if err := r.sys.WriteFileAtomic(ht, orig, perm, own); err != nil {
			return fmt.Errorf("trả lại %s: %w", ht, err)
		}
		return nil
	}
	with := func(prefix string, file string) (probeHTTP, error) {
		data := append([]byte("# tpan isolation probe: dòng thử, được trả lại ngay\n"+prefix), orig...)
		if err := r.sys.WriteFileAtomic(ht, data, perm, own); err != nil {
			return probeHTTP{}, fmt.Errorf("ghi tạm %s: %w", ht, err)
		}
		h, gerr := r.get(ctx, a, file)
		if err := restore(); err != nil {
			return probeHTTP{}, errors.Join(gerr, err)
		}
		return h, gerr
	}

	if r.want("L3") {
		h, err := with("SetEnv PHP_ADMIN_VALUE \"open_basedir=/\"\nSetEnv PHP_VALUE \"open_basedir=/\"\n", probeFilePrefix+"l3.php")
		if err != nil {
			return err
		}
		r.set("L3", r.phpCell("L3", h,
			i18n.T(".htaccess của A đổi được open_basedir của A", "A's .htaccess changes A's open_basedir"),
			i18n.T(".htaccess của A không đổi được open_basedir", "A's .htaccess cannot change open_basedir"), true))
	}
	if r.want("L4") {
		sock := b.SocketFor(j.cfg.PHPVersion)
		if r.s.inside {
			sock = b.CageSocket()
		}
		h, err := with("<Files \""+probeFilePrefix+"l4.php\">\n    SetHandler \"proxy:unix:"+sock+"|fcgi://localhost\"\n    ProxyFCGISetEnvIf \"true\" SCRIPT_FILENAME \""+r.pubB+"/"+probeFilePrefix+"l4b.php\"\n</Files>\n", probeFilePrefix+"l4.php")
		if err != nil {
			return err
		}
		// Chỉ tính là lộ khi file của B thật sự chạy dưới user của B.
		if h.mark == "CO" && h.user != b.User {
			h.mark = "KHONG"
		}
		r.set("L4", r.phpCell("L4", h,
			i18n.T("request qua tên miền của A chạy trong PHP của B (user "+h.user+")", "a request through A's domain runs in B's PHP (user "+h.user+")"),
			i18n.T(".htaccess của A không chuyển được request sang socket PHP của B", "A's .htaccess cannot hand a request to B's PHP socket"), true))
	}
	if r.want("L9") {
		if err := r.files.link(ctx, r.pubB+"/"+probeFilePrefix+"rieng.txt", r.pubA, probeFilePrefix+"lay.txt", r.ownA); err != nil {
			return err
		}
		leaked := false
		var evs []checks.Evidence
		var codes []string
		for _, o := range []string{"Options FollowSymLinks", "Options +FollowSymLinks", "Options All"} {
			h, err := with(o+"\n", probeFilePrefix+"lay.txt")
			if err != nil {
				return err
			}
			leaked = leaked || h.secret
			evs = append(evs, h.ev)
			codes = append(codes, o+": HTTP "+h.code)
		}
		c := ProbeCell{Status: ProbeBlocked, Evidence: evs,
			Detail: i18n.T("symlink sang file 0640 của B sau khi .htaccess đổi Options: không lộ ("+strings.Join(codes, "; ")+")", "symlink to B's 0640 file after .htaccess changes Options: not exposed ("+strings.Join(codes, "; ")+")")}
		if leaked {
			c.Status, c.Detail = ProbeExposed, i18n.T("symlink sang file 0640 của B sau khi .htaccess đổi Options: nội dung của B bị trả về", "symlink to B's 0640 file after .htaccess changes Options: B's content is served")
		}
		r.set("L9", c)
	}
	return nil
}

// ---------------------------------------------------------------- Last

// Last trả báo cáo của lượt gần nhất đã xong; nil khi chưa có lượt nào.
func (p *Probe) Last(ctx context.Context) (*ProbeReport, error) {
	if p.Runs == nil {
		return nil, errors.New("thử tấn công: thiếu sổ phép thử (probe_runs)")
	}
	rows, err := p.Runs.ProbeRuns(ctx, 50)
	if err != nil {
		return nil, fmt.Errorf("đọc sổ phép thử: %w", err)
	}
	for _, r := range rows {
		if r.State == probeRunning || len(r.Report) == 0 {
			continue
		}
		var rep ProbeReport
		if err := json.Unmarshal(r.Report, &rep); err != nil {
			return nil, fmt.Errorf("đọc báo cáo của lượt thử %s: %w", r.ID, err)
		}
		rep.Cleaned = r.Cleaned
		if rep.Finished.IsZero() {
			rep.Finished = r.Finished
		}
		return &rep, nil
	}
	return nil, nil
}
