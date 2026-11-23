package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/cage"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

// ---------------------------------------------------------------- môi trường

const (
	probeTestSfx = "0123456789ab"
	probeTestID  = "00112233445566ff"
	probeTestA   = "tpan-probe-a-" + probeTestSfx + ".test"
	probeTestB   = "tpan-probe-b-" + probeTestSfx + ".test"
)

// probeEnv là siteSim (máy giả của site) cộng các câu trả lời mà phép thử cần:
// curl gọi file thử, systemd của lồng (chưa có unit nào), nginx -T, meminfo.
type probeEnv struct {
	*siteEnv
	p *Probe

	mu sync.Mutex
	// answer: dòng TPAN_PROBE mà file thử <tên> trả; thiếu là KHONG.
	answer map[string]string
	// recorded: mọi đường dẫn đã tới Journal.RecordFile hay ForgetFile.
	recorded []string
}

// probeRecJournal là Journal thật (store) mà ghi lại mọi lần RecordFile,
// ForgetFile tới được nó.
type probeRecJournal struct {
	engine.Journal
	e *probeEnv
}

func (j probeRecJournal) RecordFile(ctx context.Context, path, sum, job string) error {
	j.e.mu.Lock()
	j.e.recorded = append(j.e.recorded, path)
	j.e.mu.Unlock()
	return j.Journal.RecordFile(ctx, path, sum, job)
}

func (j probeRecJournal) ForgetFile(ctx context.Context, path string) error {
	j.e.mu.Lock()
	j.e.recorded = append(j.e.recorded, path)
	j.e.mu.Unlock()
	return j.Journal.ForgetFile(ctx, path)
}

func newProbeEnv(t *testing.T, web WebServer) *probeEnv {
	t.Helper()
	se := newSiteEnv(t, web)
	e := &probeEnv{siteEnv: se, answer: map[string]string{
		// Ngoài lồng (trên máy giả không có lồng): đường C lộ, như máy thật.
		"c1.php": "CO", "c2.php": "CO", "c2b.php": "CO", "c2x.php": "CO",
		"c3.php": "CO", "c4.php": "CO", "c5.php": "CO",
	}}
	m := se.m
	exit := func(code int, out string) system.Result { return system.Result{ExitCode: code, Stdout: out} }
	// Như máy thật: /var/lib/tpan (chỗ của tpan.db) và gốc thùng rác có sẵn;
	// Plan dọn chuyển thư mục site thử qua đó trước khi xoá (probeRemoveSiteDir).
	for _, d := range []string{"/var/lib", "/var/lib/tpan", site.TrashRoot} {
		m.SetDir(d, 0o755, system.Root)
	}

	// Unit của lồng chưa từng có: is-enabled, is-active thoát khác 0.
	m.Handle([]string{"systemctl", "is-active"}, func(system.Cmd) system.Result { return exit(3, "inactive\n") })
	m.Handle([]string{"systemctl", "is-enabled"}, func(system.Cmd) system.Result { return exit(1, "disabled\n") })
	m.Handle([]string{"systemctl", "--version"}, func(system.Cmd) system.Result { return exit(0, "systemd 255 (255.4-1ubuntu8)\n+PAM\n") })
	m.SetFile("/proc/meminfo", []byte("MemTotal:        8000000 kB\nMemAvailable:    4000000 kB\n"), 0o444, system.Root)

	// curl của phép thử: file thử phải có thật trong public của site được gọi.
	m.Handle([]string{"curl", "--silent", "--show-error"}, func(c system.Cmd) system.Result {
		url := c.Args[len(c.Args)-1]
		rest := strings.TrimPrefix(url, "http://")
		dom, file, _ := strings.Cut(rest, "/")
		if file == "" {
			return exit(0, "<html>trang chủ</html>\nTPAN_HTTP=200")
		}
		pub := "/var/www/" + dom + "/public/" + file
		if _, err := m.ReadFile(pub); err != nil {
			return exit(0, "<html>404</html>\nTPAN_HTTP=404")
		}
		if strings.HasSuffix(file, ".txt") {
			return exit(0, "<html>403</html>\nTPAN_HTTP=403")
		}
		name := strings.TrimPrefix(file, probeFilePrefix)
		e.mu.Lock()
		mark := e.answer[name]
		e.mu.Unlock()
		if mark == "" {
			mark = "KHONG"
		}
		user := site.LayoutOf(mustDomain(t, dom)).User
		return exit(0, fmt.Sprintf("\nTPAN_PROBE=%s user=%s n=2\n\nTPAN_HTTP=200", mark, user))
	})
	// nginx -T: mỗi file trong sites-enabled mở đầu bằng dòng "# configuration file".
	m.Handle([]string{"nginx", "-T"}, func(system.Cmd) system.Result {
		var b strings.Builder
		names, _ := m.ReadDir("/etc/nginx/sites-enabled")
		for _, n := range names {
			p := "/etc/nginx/sites-enabled/" + n
			if data, err := m.ReadFile(p); err == nil {
				b.WriteString("# configuration file " + p + ":\n" + string(data) + "\n")
			}
		}
		return exit(0, b.String())
	})

	i := 0
	e.p = &Probe{
		Sys: m, Settings: se.st, Store: se.sites, Runs: se.st,
		Journal:   probeRecJournal{Journal: se.st, e: e},
		WPVersion: "7.1.5", WPHash: fakeWPSHA1(),
		Rand: func(n int) (string, error) {
			i++
			if n == 12 {
				return probeTestSfx, nil
			}
			return probeTestID, nil
		},
	}
	return e
}

// leftovers trả các đường dẫn mang tên phép thử còn trên hệ thống file giả.
func (e *probeEnv) leftovers(t *testing.T) []string {
	t.Helper()
	marks := []string{"tpan-probe"}
	for _, d := range []string{probeTestA, probeTestB} {
		l := site.LayoutOf(mustDomain(t, d))
		marks = append(marks, l.Slug.String(), l.CageRunDir())
	}
	var out []string
	for p := range e.m.Snapshot() {
		for _, mk := range marks {
			if strings.Contains(p, mk) {
				out = append(out, p)
				break
			}
		}
	}
	return out
}

// noProbeUsers: không còn user Linux, DB, user DB nào của site thử.
func (e *probeEnv) noProbeUsers(t *testing.T) {
	t.Helper()
	e.m.mu.Lock()
	defer e.m.mu.Unlock()
	for _, d := range []string{probeTestA, probeTestB} {
		l := site.LayoutOf(mustDomain(t, d))
		if _, ok := e.m.users[l.User]; ok {
			t.Errorf("còn user Linux %s", l.User)
		}
		if e.m.dbs[l.DB] || e.m.dbUsers[l.DB] {
			t.Errorf("còn DB hoặc user DB %s", l.DB)
		}
	}
}

func (e *probeEnv) row(t *testing.T) cage.ProbeRun {
	t.Helper()
	r, ok, err := e.st.ProbeRun(context.Background(), probeTestID)
	if err != nil || !ok {
		t.Fatalf("sổ không có lượt %s: ok=%v err=%v", probeTestID, ok, err)
	}
	return r
}

func probeCellOf(t *testing.T, rep ProbeReport, code string) ProbeResult {
	t.Helper()
	for _, r := range rep.Results {
		if r.Path == code {
			return r
		}
	}
	t.Fatalf("báo cáo không có đường %s", code)
	return ProbeResult{}
}

// ---------------------------------------------------------------- test

// Prepare từ chối khi sổ còn lượt chưa dọn, khi --only lạ, khi stack chưa cài.
func TestProbePrepareTuChoi(t *testing.T) {
	ctx := context.Background()
	e := newProbeEnv(t, Nginx)
	if _, err := e.p.Prepare(ctx, ProbeOptions{Only: "ca-hai"}); refusedCode(t, err) != "args_invalid" {
		t.Fatalf("--only lạ: muốn args_invalid")
	}
	if err := e.st.StartProbeRun(ctx, cage.ProbeRun{ID: "aaaaaaaaaaaaaaaa", Started: time.Now(), State: probeRunning, DomainA: probeTestA, DomainB: probeTestB}); err != nil {
		t.Fatal(err)
	}
	_, err := e.p.Prepare(ctx, ProbeOptions{})
	if refusedCode(t, err) != CodeProbeRunning {
		t.Fatalf("sổ còn lượt chưa dọn: muốn %s", CodeProbeRunning)
	}
	var re *RefusedError
	errors.As(err, &re)
	if !strings.Contains(re.Message.Vi, "sudo tpan isolation probe --cleanup") {
		t.Errorf("lời từ chối thiếu lệnh dọn: %q", re.Message.Vi)
	}

	none := newProbeEnv(t, "")
	if _, err := none.p.Prepare(ctx, ProbeOptions{}); refusedCode(t, err) != "stack_not_installed" {
		t.Fatalf("stack chưa cài: muốn stack_not_installed")
	}
}

// Sổ được ghi ở bước đầu của job, không lúc Prepare; bước đầu đứng trước mọi
// bước tạo.
func TestProbeSoGhiTrongJob(t *testing.T) {
	ctx := context.Background()
	e := newProbeEnv(t, Nginx)
	j, err := e.p.Prepare(ctx, ProbeOptions{Only: probeOnlyOutside})
	if err != nil {
		t.Fatal(err)
	}
	if rows, _ := e.st.PendingProbeRuns(ctx); len(rows) != 0 {
		t.Fatalf("Prepare đã ghi sổ: %+v", rows)
	}
	if _, ok := j.Plan.Steps[0].(*probeRecord); !ok {
		t.Fatalf("bước đầu là %T, muốn probeRecord", j.Plan.Steps[0])
	}
	if _, err := j.Preview(ctx); err != nil {
		t.Fatalf("xem trước: %v", err)
	}
	if rows, _ := e.st.PendingProbeRuns(ctx); len(rows) != 0 {
		t.Fatalf("xem trước đã ghi sổ: %+v", rows)
	}
	// Lệnh đầu tiên đổi máy (useradd) chạy khi sổ đã có dòng.
	seen := false
	e.m.Handle([]string{"useradd"}, func(c system.Cmd) system.Result {
		if _, ok, _ := e.st.ProbeRun(ctx, probeTestID); ok {
			seen = true
		}
		e.m.mu.Lock()
		e.m.users[c.Args[len(c.Args)-1]] = argAfter(c.Args, "--home-dir")
		e.m.mu.Unlock()
		return system.Result{}
	})
	if _, _, err := j.Execute(ctx, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !seen {
		t.Fatal("useradd chạy trước khi sổ có dòng của lượt thử")
	}
}

// Một lượt trọn trên nginx, máy giả không có cgroup v2: đủ 14 đường, ô na
// đúng stack, lượt trong lồng skipped kèm lý do, mọi thứ được dọn, sổ ghi
// done và đã dọn, không file nào vào managed_files.
func TestProbeLuotTronNginx(t *testing.T) {
	ctx := context.Background()
	e := newProbeEnv(t, Nginx)
	before := e.m.Snapshot()
	j, err := e.p.Prepare(ctx, ProbeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	rep, erep, err := j.Execute(ctx, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if erep.Job.Status != engine.JobSucceeded {
		t.Fatalf("job %s: %s; bước %+v", erep.Job.Status, erep.Job.Error, erep.Steps)
	}
	if len(rep.Results) != 14 {
		t.Fatalf("báo cáo có %d đường, muốn 14", len(rep.Results))
	}
	for _, c := range []string{"L3", "L4", "L9"} {
		r := probeCellOf(t, rep, c)
		if r.Outside.Status != ProbeNA || r.Inside.Status != ProbeNA {
			t.Errorf("%s trên nginx: muốn na/na, có %s/%s", c, r.Outside.Status, r.Inside.Status)
		}
	}
	for _, c := range []string{"L1", "L2", "L5", "L6", "L7", "L8"} {
		if s := probeCellOf(t, rep, c).Outside.Status; s != ProbeBlocked {
			t.Errorf("%s ngoài lồng: muốn blocked, có %s (%s)", c, s, probeCellOf(t, rep, c).Outside.Detail.Vi)
		}
	}
	for _, c := range []string{"C1", "C2", "C3", "C4", "C5"} {
		if s := probeCellOf(t, rep, c).Outside.Status; s != ProbeNeedsCage {
			t.Errorf("%s ngoài lồng: muốn needs_cage, có %s (%s)", c, s, probeCellOf(t, rep, c).Outside.Detail.Vi)
		}
	}
	for _, r := range rep.Results {
		if r.Path == "L3" || r.Path == "L4" || r.Path == "L9" {
			continue
		}
		if r.Inside.Status != ProbeSkipped || !strings.Contains(r.Inside.Detail.Vi, "cgroup v2") {
			t.Errorf("%s trong lồng (cgroup v1): muốn skipped kèm lý do, có %s %q", r.Path, r.Inside.Status, r.Inside.Detail.Vi)
		}
	}
	if rep.Exposed != 0 || !rep.Cleaned {
		t.Errorf("Exposed=%d Cleaned=%v; muốn 0, true", rep.Exposed, rep.Cleaned)
	}
	if left := e.leftovers(t); len(left) != 0 {
		t.Errorf("còn đồ của phép thử: %v", left)
	}
	e.noProbeUsers(t)
	after := e.m.Snapshot()
	for p := range after {
		if _, ok := before[p]; !ok && !strings.HasPrefix(p, "/var/cache/tpan/") {
			t.Errorf("file mới sau lượt thử: %s", p)
		}
	}
	if len(e.recorded) != 0 {
		t.Errorf("job ghi managed_files: %v", e.recorded)
	}
	r := e.row(t)
	if r.State != probeDone || !r.Cleaned {
		t.Errorf("sổ: state=%s cleaned=%v; muốn done, true", r.State, r.Cleaned)
	}
	last, err := e.p.Last(ctx)
	if err != nil || last == nil || last.ID != probeTestID || len(last.Results) != 14 {
		t.Fatalf("Last: %+v, %v", last, err)
	}
	// Lượt sau không bị chặn bởi lượt vừa xong.
	if _, err := e.p.Prepare(ctx, ProbeOptions{}); err != nil {
		t.Fatalf("Prepare lượt sau: %v", err)
	}
}

// Ô lộ ở đường L là exposed và được đếm.
func TestProbeDuongLLoLaExposed(t *testing.T) {
	ctx := context.Background()
	e := newProbeEnv(t, Nginx)
	e.answer["l7.php"] = "CO"
	j, err := e.p.Prepare(ctx, ProbeOptions{Only: probeOnlyOutside})
	if err != nil {
		t.Fatal(err)
	}
	rep, _, err := j.Execute(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s := probeCellOf(t, rep, "L7").Outside.Status; s != ProbeExposed || rep.Exposed != 1 {
		t.Fatalf("L7 lộ: status=%s Exposed=%d; muốn exposed, 1", s, rep.Exposed)
	}
	if s := probeCellOf(t, rep, "L7").Inside.Status; s != ProbeSkipped {
		t.Fatalf("--only ngoai: ô trong lồng muốn skipped, có %s", s)
	}
}

// Apache: L5 là na; L3, L4, L9 chạy và .htaccess của A được trả về như cũ.
func TestProbeApache(t *testing.T) {
	ctx := context.Background()
	e := newProbeEnv(t, Apache)
	e.m.Handle([]string{"apache2ctl", "-S"}, func(system.Cmd) system.Result {
		var b strings.Builder
		b.WriteString("VirtualHost configuration:\n*:80                   is a NameVirtualHost\n")
		names, _ := e.m.ReadDir("/etc/apache2/sites-enabled")
		for _, n := range names {
			file := "/etc/apache2/sites-enabled/" + n
			data, err := e.m.ReadFile(file)
			if err != nil {
				continue
			}
			for i, ln := range strings.Split(string(data), "\n") {
				if v, ok := strings.CutPrefix(strings.TrimSpace(ln), "ServerName "); ok {
					fmt.Fprintf(&b, "         port 80 namevhost %s (%s:%d)\n", v, file, i+1)
				}
			}
		}
		return system.Result{Stdout: b.String()}
	})
	var hts []string
	e.m.Handle([]string{"curl", "--silent", "--show-error"}, func(c system.Cmd) system.Result {
		if strings.Contains(c.Args[len(c.Args)-1], probeTestA+"/") {
			if data, err := e.m.ReadFile("/var/www/" + probeTestA + "/public/.htaccess"); err == nil {
				hts = append(hts, string(data))
			}
		}
		url := c.Args[len(c.Args)-1]
		if strings.HasSuffix(url, ".php") {
			return system.Result{Stdout: "<html>500</html>\nTPAN_HTTP=500"}
		}
		if strings.HasSuffix(url, ".txt") {
			return system.Result{Stdout: "<html>403</html>\nTPAN_HTTP=403"}
		}
		return system.Result{Stdout: "ok\nTPAN_HTTP=200"}
	})
	j, err := e.p.Prepare(ctx, ProbeOptions{Only: probeOnlyOutside})
	if err != nil {
		t.Fatal(err)
	}
	rep, erep, err := j.Execute(ctx, nil)
	if err != nil || erep.Job.Status != engine.JobSucceeded {
		t.Fatalf("Execute: %v; job %s %s", err, erep.Job.Status, erep.Job.Error)
	}
	if r := probeCellOf(t, rep, "L5"); r.Outside.Status != ProbeNA {
		t.Errorf("L5 trên Apache: muốn na, có %s", r.Outside.Status)
	}
	for _, c := range []string{"L3", "L4", "L9", "L8"} {
		if s := probeCellOf(t, rep, c).Outside.Status; s != ProbeBlocked {
			t.Errorf("%s: muốn blocked, có %s (%s)", c, s, probeCellOf(t, rep, c).Outside.Detail.Vi)
		}
	}
	sawSetHandler := false
	for _, h := range hts {
		if strings.Contains(h, "SetHandler") {
			sawSetHandler = true
		}
	}
	if !sawSetHandler {
		t.Error("L4 không ghi khối SetHandler tạm vào .htaccess của A")
	}
	if left := e.leftovers(t); len(left) != 0 {
		t.Errorf("còn đồ của phép thử: %v", left)
	}
}

// Bất biến 5: một bước hỏng giữa chừng (sau khi A, B đã tạo) thì mọi thứ tạo
// được đều được dọn, sổ ghi failed và đã dọn.
func TestProbeHongGiuaChungVanDon(t *testing.T) {
	ctx := context.Background()
	e := newProbeEnv(t, Nginx)
	before := e.m.Snapshot()
	// Lượt ngoài lồng không ghi được file thử C1: bước hỏng sau khi hai site
	// đã dựng xong.
	e.m.Fail("write", "/var/www/"+probeTestA+"/public/"+probeFilePrefix+"c1.php", errors.New("đĩa đầy (giả lập)"))
	j, err := e.p.Prepare(ctx, ProbeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	rep, erep, err := j.Execute(ctx, nil)
	if err != nil {
		t.Fatalf("Execute: %v (dọn được thì không phải lỗi)", err)
	}
	if erep.Job.Status == engine.JobSucceeded {
		t.Fatal("job thử lẽ ra phải hỏng")
	}
	if !rep.Cleaned {
		t.Error("báo cáo: Cleaned = false")
	}
	if left := e.leftovers(t); len(left) != 0 {
		t.Errorf("còn đồ của phép thử: %v", left)
	}
	e.noProbeUsers(t)
	for p := range e.m.Snapshot() {
		if _, ok := before[p]; !ok && !strings.HasPrefix(p, "/var/cache/tpan/") {
			t.Errorf("file mới sau lượt hỏng: %s", p)
		}
	}
	r := e.row(t)
	if r.State != probeFailed || !r.Cleaned {
		t.Errorf("sổ: state=%s cleaned=%v; muốn failed, true", r.State, r.Cleaned)
	}
	if rows, _ := e.st.PendingProbeRuns(ctx); len(rows) != 0 {
		t.Errorf("sổ còn lượt chưa dọn: %+v", rows)
	}
}

// Hoàn tác của engine không trả lại được (bước tạo hỏng, rồi Undo của bước
// tạo user cũng hỏng): job undo_failed, Plan dọn vẫn xoá phần còn lại.
func TestProbeHongKhiTaoSiteVanDon(t *testing.T) {
	ctx := context.Background()
	e := newProbeEnv(t, Nginx)
	e.m.mu.Lock()
	e.m.failTar = 2
	e.m.mu.Unlock()
	// userdel hỏng đúng lần đầu (SiteUser.Undo của engine), lần sau (Plan
	// dọn) chạy như thường.
	first := true
	e.m.Handle([]string{"userdel"}, func(c system.Cmd) system.Result {
		e.m.mu.Lock()
		defer e.m.mu.Unlock()
		if first {
			first = false
			return system.Result{ExitCode: 8, Stderr: "userdel: user is currently used by process 1234\n"}
		}
		delete(e.m.users, c.Args[len(c.Args)-1])
		return system.Result{}
	})
	j, err := e.p.Prepare(ctx, ProbeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	rep, erep, err := j.Execute(ctx, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if erep.Job.Status != engine.JobUndoFailed {
		t.Fatalf("job: %s, muốn %s", erep.Job.Status, engine.JobUndoFailed)
	}
	if !rep.Cleaned {
		t.Error("báo cáo: Cleaned = false")
	}
	if left := e.leftovers(t); len(left) != 0 {
		t.Errorf("còn đồ của phép thử: %v", left)
	}
	e.noProbeUsers(t)
	if r := e.row(t); r.State != probeFailed || !r.Cleaned {
		t.Errorf("sổ: state=%s cleaned=%v", r.State, r.Cleaned)
	}
}

// probeLeftoverSite đặt lên máy giả phần của một site thử còn sót (panel bị
// tắt giữa lượt): user, DB, thư mục, vhost.
func probeLeftoverSite(t *testing.T, e *probeEnv, dom string) site.Layout {
	t.Helper()
	l := site.LayoutOf(mustDomain(t, dom))
	e.m.mu.Lock()
	e.m.users[l.User] = l.Dir
	e.m.dbs[l.DB], e.m.dbUsers[l.DB] = true, true
	e.m.mu.Unlock()
	e.m.SetDir(l.Public, 0o2750, system.Owner{User: l.User, Group: site.WebGroup})
	e.m.SetFile(l.Public+"/index.php", []byte("<?php\n"), 0o640, system.Owner{User: l.User, Group: l.User})
	e.m.SetDir(l.LogDir, 0o750, system.Owner{User: "root", Group: l.User})
	e.m.SetFile(l.NginxConf, []byte("server {}\n"), 0o644, system.Root)
	e.m.SetSymlink(l.NginxConf, l.NginxLink)
	return l
}

// PrepareCleanup chỉ dọn tên có trong sổ (đọc lại bên trong job), đóng lượt
// "running" thành failed và đánh dấu đã dọn; site thật cùng tiền tố không bị đụng.
func TestProbeCleanupChiNhanTenTrongSo(t *testing.T) {
	ctx := context.Background()
	e := newProbeEnv(t, Nginx)
	if p, err := e.p.PrepareCleanup(ctx); err != nil || p != nil {
		t.Fatalf("sổ trống: muốn nil, nil; có %v, %v", p, err)
	}
	if err := e.st.StartProbeRun(ctx, cage.ProbeRun{ID: probeTestID, Started: time.Now(), State: probeRunning, DomainA: probeTestA, DomainB: probeTestB}); err != nil {
		t.Fatal(err)
	}
	probeLeftoverSite(t, e, probeTestA)
	probeLeftoverSite(t, e, probeTestB)
	real := probeLeftoverSite(t, e, "tpan-probe-shop.example.com")
	e.m.SetFile(probeStrayPath(Nginx, probeTestSfx), []byte("server {}\n"), 0o644, system.Root)
	e.m.SetFile(probeTmpFile(probeTestSfx), nil, 0o644, system.Root)

	pl, err := e.p.PrepareCleanup(ctx)
	if err != nil || pl == nil {
		t.Fatalf("PrepareCleanup: %v, %v", pl, err)
	}
	if _, err := pl.Preview(ctx); err != nil {
		t.Fatalf("xem trước: %v", err)
	}
	rep, err := pl.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("Execute: %v; job %s %s; bước %+v", err, rep.Job.Status, rep.Job.Error, rep.Steps)
	}
	var left []string
	for _, p := range e.leftovers(t) {
		if !strings.HasPrefix(p, real.Dir) && !strings.Contains(p, "tpan-probe-shop") {
			left = append(left, p)
		}
	}
	if len(left) != 0 {
		t.Errorf("còn đồ của lượt thử: %v", left)
	}
	e.noProbeUsers(t)
	if _, err := e.m.Stat(real.Public + "/index.php"); err != nil {
		t.Errorf("site thật %s bị đụng: %v", real.Domain, err)
	}
	if _, err := e.m.Stat(real.NginxLink); err != nil {
		t.Errorf("vhost của site thật bị gỡ: %v", err)
	}
	e.m.mu.Lock()
	_, userOK := e.m.users[real.User]
	e.m.mu.Unlock()
	if !userOK {
		t.Error("user của site thật bị xoá")
	}
	r := e.row(t)
	if r.State != probeFailed || !r.Cleaned {
		t.Errorf("sổ: state=%s cleaned=%v; muốn failed, true", r.State, r.Cleaned)
	}
	if p, err := e.p.PrepareCleanup(ctx); err != nil || p != nil {
		t.Fatalf("dọn xong: muốn nil, nil; có %v, %v", p, err)
	}
}

// Bất biến 10: Plan dọn từ chối tên của site thật, và tên khớp mẫu mà không
// có trong sổ thì không bị đụng (cổng đọc sổ bên trong job).
func TestProbeCleanupTuChoiTenLa(t *testing.T) {
	ctx := context.Background()
	e := newProbeEnv(t, Nginx)
	cfg := StackConfig{Web: Nginx, PHPVersion: "8.3"}

	for _, bad := range []cage.ProbeRun{
		{ID: "1", DomainA: "tpan-probe-shop.example.com", DomainB: probeTestB},
		{ID: "2", DomainA: probeTestA, DomainB: "shop.example.com"},
		{ID: "3", DomainA: probeTestA, DomainB: "tpan-probe-b-ffffffffffff.test"}, // hai đuôi khác nhau
		{ID: "4", DomainA: "tpan-probe-a-0123456789ab.test.example.com", DomainB: probeTestB},
	} {
		if _, _, err := e.p.cleanupPlan(cfg, []cage.ProbeRun{bad}); err == nil {
			t.Errorf("cleanupPlan nhận %q, %q; muốn lỗi", bad.DomainA, bad.DomainB)
		}
	}

	// Tên khớp mẫu, không có trong sổ: Plan dựng được nhưng không đụng gì.
	la := probeLeftoverSite(t, e, probeTestA)
	probeLeftoverSite(t, e, probeTestB)
	before := e.m.Snapshot()
	plan, ledger, err := e.p.cleanupPlan(cfg, []cage.ProbeRun{{ID: probeTestID, DomainA: probeTestA, DomainB: probeTestB}})
	if err != nil {
		t.Fatal(err)
	}
	e.m.ResetRan()
	rep, err := engine.New(e.m, engine.Options{}).Execute(ctx, plan)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("Execute: %v; %s %s", err, rep.Job.Status, rep.Job.Error)
	}
	if len(ledger.found) != 0 || len(ledger.ok) != 0 {
		t.Errorf("ledger nhận tên không có trong sổ: %+v %+v", ledger.found, ledger.ok)
	}
	for _, c := range e.m.Ran() {
		t.Errorf("chạy lệnh cho tên không có trong sổ: %s", c.String())
	}
	after := e.m.Snapshot()
	if len(after) != len(before) {
		t.Errorf("hệ thống file đổi: %d → %d mục", len(before), len(after))
	}
	if _, err := e.m.Stat(la.Public); err != nil {
		t.Errorf("thư mục của tên không có trong sổ bị xoá: %v", err)
	}

	// Lượt đã dọn trong sổ (Cleaned) cũng không được dọn lại.
	if err := e.st.StartProbeRun(ctx, cage.ProbeRun{ID: probeTestID, Started: time.Now(), State: probeDone, DomainA: probeTestA, DomainB: probeTestB}); err != nil {
		t.Fatal(err)
	}
	if err := e.st.MarkProbeCleaned(ctx, probeTestID); err != nil {
		t.Fatal(err)
	}
	plan, _, _ = e.p.cleanupPlan(cfg, []cage.ProbeRun{{ID: probeTestID, DomainA: probeTestA, DomainB: probeTestB}})
	if _, err := engine.New(e.m, engine.Options{}).Execute(ctx, plan); err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.Stat(la.Public); err != nil {
		t.Errorf("lượt đã dọn bị dọn lại: %v", err)
	}
}

// Job thử không gọi RecordFile hay ForgetFile, kể cả Plan dọn sau khi hỏng.
func TestProbeKhongGhiManagedFiles(t *testing.T) {
	ctx := context.Background()
	for _, fail := range []bool{false, true} {
		e := newProbeEnv(t, Nginx)
		if fail {
			e.m.Fail("write", "/var/www/"+probeTestA+"/public/"+probeFilePrefix+"l2.php", errors.New("giả lập"))
		}
		j, err := e.p.Prepare(ctx, ProbeOptions{Only: probeOnlyOutside})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := j.Execute(ctx, nil); err != nil {
			t.Fatalf("hỏng=%v: Execute: %v", fail, err)
		}
		if len(e.recorded) != 0 {
			t.Errorf("hỏng=%v: Journal nhận RecordFile/ForgetFile: %v", fail, e.recorded)
		}
		files, err := e.st.ManagedFiles(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if strings.Contains(f.Path, "tpan-probe") || strings.Contains(f.Path, site.LayoutOf(mustDomain(t, probeTestA)).Slug.String()) {
				t.Errorf("hỏng=%v: managed_files có %s", fail, f.Path)
			}
		}
	}
}

// Máy cgroup v1: lượt trong lồng skipped kèm lý do, không phải lỗi. Riêng
// --only trong bị từ chối ngay (lượt duy nhất sẽ bỏ qua hết: tạo rồi xoá hai
// site WordPress mà không thử được gì).
func TestProbeCgroupV1(t *testing.T) {
	ctx := context.Background()
	e := newProbeEnv(t, Nginx)
	var re *RefusedError
	if ji, err := e.p.Prepare(ctx, ProbeOptions{Only: probeOnlyInside}); !errors.As(err, &re) || re.Code != CodeCageCgroupV1 || ji != nil {
		t.Fatalf("--only trong trên cgroup v1: muốn từ chối %s, có %v", CodeCageCgroupV1, err)
	}
	j, err := e.p.Prepare(ctx, ProbeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range j.Plan.Steps {
		if strings.HasPrefix(s.Info().ID, "cage.reap:") || s.Info().ID == "probe.pass:"+probeOnlyInside {
			t.Errorf("máy cgroup v1 mà Plan có bước %s", s.Info().ID)
		}
	}
	if c := probeCellOf(t, j.report, "C3").Inside; c.Status != ProbeSkipped || !strings.Contains(c.Detail.Vi, "cgroup v2") {
		t.Errorf("C3 trong lồng: %s %q", c.Status, c.Detail.Vi)
	}

	// systemd cũ cũng vậy.
	e.m.SetFile(probeCgroupV2, []byte("cpu memory pids\n"), 0o444, system.Root)
	e.m.Handle([]string{"systemctl", "--version"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: "systemd 249 (249.11-0ubuntu3)\n"}
	})
	js, err := e.p.Prepare(ctx, ProbeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if c := probeCellOf(t, js.report, "C1").Inside; c.Status != ProbeSkipped || !strings.Contains(c.Detail.Vi, "systemd bản 249") {
		t.Errorf("systemd 249: C1 trong lồng %s %q", c.Status, c.Detail.Vi)
	}
}

// Máy dựng được lồng: Plan có bước của cage.enable cho A rồi B (kể cả
// CageReap) với trần cố định, rồi lượt trong lồng, rồi dọn B trước A.
func TestProbePlanCoLong(t *testing.T) {
	ctx := context.Background()
	e := newProbeEnv(t, Nginx)
	e.m.SetFile(probeCgroupV2, []byte("cpu memory pids\n"), 0o444, system.Root)
	j, err := e.p.Prepare(ctx, ProbeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, s := range j.Plan.Steps {
		ids = append(ids, s.Info().ID)
	}
	idx := func(id string) int {
		for i, x := range ids {
			if x == id {
				return i
			}
		}
		t.Fatalf("Plan thiếu bước %s; có %v", id, ids)
		return -1
	}
	rec := idx("probe.record:" + probeTestID)
	out := idx("probe.pass:" + probeOnlyOutside)
	reapA, reapB := idx("cage.reap:"+probeTestA), idx("cage.reap:"+probeTestB)
	in := idx("probe.pass:" + probeOnlyInside)
	userB, userA := idx("probe.reset-failed:"+probeTestB), idx("probe.reset-failed:"+probeTestA)
	inOrder := rec == 0 && rec < out && out < reapA && reapA < reapB && reapB < in && in < userB && userB < userA
	if !inOrder {
		t.Errorf("thứ tự bước sai: %v", ids)
	}
	for _, id := range ids {
		if strings.Contains(id, "cron") || strings.Contains(id, "DISABLE_WP_CRON") {
			t.Errorf("Plan có bước cron: %s", id)
		}
	}
	if probeLimits.RAMMB != 160 || probeLimits.CPUPercent != 100 || probeLimits.Tasks != 60 || probeLimits.PHPWorkers != 3 {
		t.Errorf("trần của phép thử: %+v", probeLimits)
	}
}

// File PHP thử: sinh được, có dòng kết quả, không nhận tham số từ request,
// không gọi shell; giá trị lạ bị từ chối.
func TestProbeFilePHP(t *testing.T) {
	e := newProbeEnv(t, Nginx)
	j, err := e.p.Prepare(context.Background(), ProbeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	v := j.vars()
	names := []string{"l1.php", "l2.php", "l3.php", "l4.php", "l4b.php", "l6.php", "l7.php", "c1.php", "c2.php", "c2b.php", "c2x.php", "c3.php", "c4.php", "c5.php"}
	for _, n := range names {
		b, err := probeRender(n, v)
		if err != nil {
			t.Fatalf("%s: %v", n, err)
		}
		s := string(b)
		if !strings.HasPrefix(s, "<?php") || !strings.Contains(s, "tp_kq(") {
			t.Errorf("%s: thiếu mở đầu PHP hay dòng kết quả", n)
		}
		for _, bad := range []string{"$_GET", "$_POST", "$_REQUEST", "$_COOKIE", "$_SERVER", "shell_exec", "system(", "passthru", "`", "{{"} {
			if strings.Contains(s, bad) {
				t.Errorf("%s chứa %q", n, bad)
			}
		}
		// open_basedir của pool chặn PHP mở file: một mô tả "file" trong
		// proc_open (từng là /dev/null cho stderr) làm proc_open trả false, và
		// đường gọi lệnh ra "bị chặn" giả (e2e M17). Lệnh không chạy được thì
		// không được in dòng kết quả.
		if strings.Contains(s, "tp_run(") {
			if strings.Contains(s, "['file'") || strings.Contains(s, "'/dev/null'") {
				t.Errorf("%s: proc_open mở file (open_basedir chặn): %s", n, s)
			}
			if !strings.Contains(s, "if (!is_resource($p)) {\n        echo \"\\nTPAN_PROBE_LOI=proc_open\\n\";\n        exit;") {
				t.Errorf("%s: tp_run không chạy được lệnh mà không dừng trước dòng kết quả", n)
			}
		}
	}
	bad := v
	bad.BDir = "/var/www/x'; phpinfo(); '"
	if _, err := probeRender("l2.php", bad); err == nil {
		t.Error("giá trị có nháy đơn lọt vào file PHP")
	}
}

// Báo cáo trong sổ giữ được dạng JSON của ProbeReport.
func TestProbeBaoCaoJSON(t *testing.T) {
	r := probeResults(Apache, "", i18n.T("máy giả", "fake machine"))
	data, err := json.Marshal(ProbeReport{Results: r})
	if err != nil {
		t.Fatal(err)
	}
	var back ProbeReport
	if err := json.Unmarshal(data, &back); err != nil || len(back.Results) != 14 {
		t.Fatalf("JSON: %v, %d dòng", err, len(back.Results))
	}
	if back.Results[4].Path != "L5" || back.Results[4].Outside.Status != ProbeNA {
		t.Errorf("L5 trên Apache: %+v", back.Results[4])
	}
}

// Lượt trong lồng (chạy riêng bước probePass, không qua bước của lồng): đường
// C bị chặn là blocked; C3 bị giết (không có dòng kết quả) mà oom_kill của
// slice tăng thì blocked, kèm số đếm và mã HTTP của B làm bằng chứng.
func TestProbeLuotTrongLong(t *testing.T) {
	ctx := context.Background()
	e := newProbeEnv(t, Nginx)
	e.m.SetFile(probeCgroupV2, []byte("cpu memory pids\n"), 0o444, system.Root)
	for _, n := range []string{"c1.php", "c2b.php", "c4.php", "c5.php"} {
		e.answer[n] = "KHONG"
	}
	la := site.LayoutOf(mustDomain(t, probeTestA))
	e.m.SetFile(la.CageCgroup()+"/memory.events", []byte("low 0\nhigh 0\nmax 3\noom 1\noom_kill 1\n"), 0o444, system.Root)
	e.m.Handle([]string{"curl", "--silent", "--show-error", "--max-time", "60", "--resolve", probeTestA + ":80:127.0.0.1", "--write-out", "\nTPAN_HTTP=%{http_code}", "http://" + probeTestA + "/" + probeFilePrefix + "c3.php"},
		func(system.Cmd) system.Result {
			e.m.SetFile(la.CageCgroup()+"/memory.events", []byte("low 0\nhigh 0\nmax 9\noom 2\noom_kill 2\n"), 0o444, system.Root)
			return system.Result{Stdout: "<html>502</html>\nTPAN_HTTP=502"}
		})
	j, err := e.p.Prepare(ctx, ProbeOptions{Only: probeOnlyInside})
	if err != nil {
		t.Fatal(err)
	}
	// Bỏ các bước của cage.enable (thợ khác viết, cần systemd giả đầy đủ):
	// giữ ghi sổ, tạo site, L8, lượt trong lồng, dọn.
	var keep []engine.Step
	inEnable := false
	for _, s := range j.Plan.Steps {
		id := s.Info().ID
		switch {
		case strings.HasPrefix(id, "probe.l8:"):
			keep = append(keep, s)
			inEnable = true // từ sau L8 tới lượt trong lồng là các bước của cage.enable
			continue
		case id == "probe.pass:"+probeOnlyInside:
			inEnable = false
		}
		if !inEnable {
			keep = append(keep, s)
		}
	}
	j.Plan.Steps = keep
	rep, erep, err := j.Execute(ctx, nil)
	if err != nil || erep.Job.Status != engine.JobSucceeded {
		t.Fatalf("Execute: %v; job %s %s; bước %+v", err, erep.Job.Status, erep.Job.Error, erep.Steps)
	}
	for _, c := range []string{"C1", "C2", "C3", "C4", "C5"} {
		cell := probeCellOf(t, rep, c)
		if cell.Inside.Status != ProbeBlocked {
			t.Errorf("%s trong lồng: muốn blocked, có %s (%s)", c, cell.Inside.Status, cell.Inside.Detail.Vi)
		}
		if cell.Outside.Status != ProbeSkipped {
			t.Errorf("%s --only trong: ô ngoài lồng muốn skipped, có %s", c, cell.Outside.Status)
		}
	}
	c3 := probeCellOf(t, rep, "C3").Inside
	if !strings.Contains(c3.Detail.Vi, "oom_kill tăng 1") || !strings.Contains(c3.Detail.Vi, "B trả HTTP 200") {
		t.Errorf("C3 trong lồng thiếu bằng chứng: %q", c3.Detail.Vi)
	}
	if s := probeCellOf(t, rep, "L8").Inside.Status; s != ProbeBlocked {
		t.Errorf("L8 --only trong: muốn blocked, có %s", s)
	}
	_ = e.m.Remove(la.CageCgroup() + "/memory.events") // file giả của test, không do job tạo
	_ = e.m.Remove(la.CageCgroup())
	if left := e.leftovers(t); len(left) != 0 {
		t.Errorf("còn đồ của phép thử: %v", left)
	}
}
