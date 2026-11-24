package demo

// Phần M17 của máy giả (mục 6.15 của docs/KE-HOACH.md): lồng theo site, số
// liệu tài nguyên, gợi ý trần và sổ thử tấn công, cho các màn Lồng, Số liệu,
// Gợi ý và Thử tấn công của `tpan serve --demo`.
//
//   - gói co_ban (khach_a) bật lồng với trần nhỏ; gói mới nang_cao (khach_b)
//     bật lồng với trần rộng; gói default không lồng;
//   - hai site trong lồng của hai chủ khác nhau: site tên dài của khach_a
//     (chạm trần RAM hai lần, đỉnh sát trần) và site tin tức của khach_b;
//   - các site khác của hai user đó ở trạng thái "can-vao" (gói muốn lồng, site
//     chưa vào: health có cage.state Warn, đúng như máy thật); site của admin
//     ở ngoài lồng;
//   - 7 ngày mẫu 5 phút (site tin tức: từ lúc vào lồng, 29 giờ) có ngày đêm,
//     dòng gộp theo giờ do chính core.Usages.Rollup tính;
//   - một lượt thử tấn công đã xong và đã dọn, một lượt hỏng chưa dọn.
//
// Site vào lồng bằng chính core.Cages.PrepareEnable (job thật, lùi về quá khứ
// như demo.go), nên file của lồng, vhost, pool chung đã gỡ và sổ managed_files
// đều do mã thật ghi. Phần máy giả lập thêm (m17Setup) chỉ là thứ systemd và
// kernel làm: socket do systemd tạo khi bật unit socket, cgroup của slice,
// tiến trình chính của dịch vụ PHP trong lồng (/proc/<pid>), và các lệnh mà
// phép kiểm cage.* chạy.

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/cage"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// pkgPlus là gói bật lồng với trần rộng của khach_b.
const pkgPlus = "nang_cao"

var (
	// basicLimits: trần của gói co_ban (CPU nửa nhân, 256 MiB, 60 tiến trình,
	// đọc ghi 20 MB/giây, 4 worker).
	basicLimits = account.Limits{CPUPercent: 50, RAMMB: 256, Tasks: 60, IOReadMB: 20, IOWriteMB: 20, PHPWorkers: 4}
	plusLimits  = account.Limits{CPUPercent: 100, RAMMB: 512, Tasks: 120, IOReadMB: 50, IOWriteMB: 50, PHPWorkers: 8}
)

const (
	mib = int64(1) << 20
	// cgroupTick là nhịp cập nhật file cgroup của lồng đang chạy: bộ lấy mẫu
	// 5 phút của serve thấy CPU, RAM, đĩa nhúc nhích như máy thật.
	cgroupTick = 15 * time.Second
	// systemdVersion: lồng cần systemd từ 254 (cờ --expand-environment).
	systemdVersion = "systemd 255 (255.4-1ubuntu8.6)\n+PAM +AUDIT +SELINUX +APPARMOR +IMA +SMACK +SECCOMP +GCRYPT -GNUTLS +OPENSSL +ACL +BLKID +CURL +ELFUTILS +FIDO2 +IDN2 -IDN +IPTC +KMOD +LIBCRYPTSETUP +LIBFDISK +PCRE2 -PWQUALITY +P11KIT +QRENCODE +TPM2 +BZIP2 +LZ4 +XZ +ZLIB +ZSTD -BPF_FRAMEWORK -XKBCOMMON +UTMP +SYSVINIT default-hierarchy=unified\n"
)

// m17 là trạng thái riêng của phần này: tiến trình chính của dịch vụ PHP của
// từng lồng đang chạy, theo tên unit dịch vụ.
type m17 struct {
	*machine
	mu   sync.Mutex
	main map[string]cageMain
}

type cageMain struct {
	pid     int
	fence   string    // đường dẫn file hàng rào
	fenced  time.Time // lúc ghi hàng rào (stat -c %Y)
	started time.Time // ActiveEnterTimestamp: sau lúc ghi hàng rào
}

// uidOf là uid giả, cố định theo tên user, của user Linux trên máy giả.
func uidOf(name string) int {
	switch name {
	case "root":
		return 0
	case "www-data":
		return 33
	case "nobody", "nogroup":
		return 65534
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	return 1100 + int(h.Sum32()%8000)
}

// m17Setup đăng ký các lệnh mà Plan của lồng và phép kiểm cage.* chạy. Gọi ở
// đầu seedM17, sau mọi handler khác: handler cùng độ dài prefix thì cái đăng ký
// sau thắng (getent passwd của machine.go).
func (m *machine) m17Setup() *m17 {
	x := &m17{machine: m, main: map[string]cageMain{}}
	m.SetFile("/sys/fs/cgroup/cgroup.controllers", []byte("cpuset cpu io memory hugetlb pids rdma misc\n"), 0o444, rootOwner)
	m.SetDir(site.CageCgroupRoot, 0o755, rootOwner)
	m.Handle([]string{"systemctl", "--version"}, func(system.Cmd) system.Result { return exit(0, systemdVersion, "") })
	m.Handle([]string{"getent", "passwd"}, x.getentPasswd)
	m.Handle([]string{"getent", "group"}, x.getentGroup)
	m.Handle([]string{"id", "-u"}, x.idU)
	m.Handle([]string{"id", "-un"}, func(c system.Cmd) system.Result {
		if c.AsUser == "" {
			return exit(0, "root\n", "")
		}
		return exit(0, c.AsUser+"\n", "")
	})
	m.Handle([]string{"ls", "-A"}, x.lsCaged)
	m.Handle([]string{"stat", "-c", "%Y"}, x.statFence)
	m.Handle([]string{"systemctl", "show", "--timestamp=unix"}, x.show)
	m.Handle([]string{"systemctl", "show", "-p"}, x.show)
	return x
}

// cageUser trả Layout của site có user Linux u khi site đó có hàng rào trên
// máy: đúng điều kiện mà lớp bọc lệnh (cage.Wrap) dùng để đưa lệnh vào lồng.
func (x *m17) cageUser(u string) (site.Layout, bool) {
	slug, ok := site.CageSlugOfUser(u)
	if !ok {
		return site.Layout{}, false
	}
	if _, err := x.Stat(site.CageFenceOfSlug(slug)); err != nil {
		return site.Layout{}, false
	}
	names, _ := x.ReadDir("/var/www")
	for _, n := range names {
		if l, ok := siteOf(n); ok && l.User == u {
			return l, true
		}
	}
	return site.Layout{}, false
}

func passwdLine(name, home string) string {
	shell := "/usr/sbin/nologin"
	if name == "root" {
		home, shell = "/root", "/bin/bash"
	}
	id := strconv.Itoa(uidOf(name))
	return name + ":x:" + id + ":" + id + ":" + name + ":" + home + ":" + shell
}

// getentPasswd: `getent passwd <tên>...` in mỗi tên có một dòng, mã 2 khi có
// tên không có (như getent thật). Không tên nào: cả bảng, hoặc bảng riêng của
// lồng (file passwd mà CageFiles đã ghi) khi chạy dưới user của site trong lồng.
func (x *m17) getentPasswd(c system.Cmd) system.Result {
	if l, ok := x.cageUser(c.AsUser); ok && len(c.Args) == 1 {
		b, _ := x.ReadFile(l.CagePasswd())
		return exit(0, string(b), "")
	}
	users := map[string]string{"root": "/root", "www-data": "/var/www", "nobody": "/nonexistent"}
	x.machine.mu.Lock()
	for u, home := range x.users {
		users[u] = home
	}
	x.machine.mu.Unlock()
	keys := c.Args[1:]
	if len(keys) == 0 {
		for u := range users {
			keys = append(keys, u)
		}
		slices.Sort(keys)
	}
	var out strings.Builder
	code := 0
	for _, k := range keys {
		home, ok := users[k]
		if !ok {
			code = 2
			continue
		}
		out.WriteString(passwdLine(k, home) + "\n")
	}
	return exit(code, out.String(), "")
}

// getentGroup: nhóm riêng của mỗi user, cộng root, www-data, nogroup.
func (x *m17) getentGroup(c system.Cmd) system.Result {
	var out strings.Builder
	code := 0
	for _, k := range c.Args[1:] {
		x.machine.mu.Lock()
		_, isUser := x.users[k]
		x.machine.mu.Unlock()
		if !isUser && k != "root" && k != "www-data" && k != "nogroup" {
			code = 2
			continue
		}
		out.WriteString(k + ":x:" + strconv.Itoa(uidOf(k)) + ":\n")
	}
	return exit(code, out.String(), "")
}

func (x *m17) idU(c system.Cmd) system.Result {
	name := c.AsUser
	if len(c.Args) > 1 {
		name = c.Args[1]
	}
	x.machine.mu.Lock()
	_, ok := x.users[name]
	x.machine.mu.Unlock()
	if !ok && name != "" && name != "root" && name != "www-data" {
		return exit(1, "", "id: '"+name+"': no such user\n")
	}
	return exit(0, strconv.Itoa(uidOf(name))+"\n", "")
}

// lsCaged: `ls -A` dưới user của site trong lồng thấy đúng phần lồng để lộ
// (mục 3.3): /var/www chỉ có thư mục của site, /tmp riêng và rỗng, /run chỉ có
// mysqld và systemd. Các lệnh ls khác không đổi gì như trước M17.
func (x *m17) lsCaged(c system.Cmd) system.Result {
	l, ok := x.cageUser(c.AsUser)
	if !ok || len(c.Args) < 2 {
		return okResult
	}
	switch c.Args[1] {
	case "/var/www":
		return exit(0, l.Domain.String()+"\n", "")
	case "/tmp":
		return okResult
	case "/run":
		return exit(0, "mysqld\nsystemd\n", "")
	}
	return okResult
}

// statFence: `stat -c %Y <hàng rào>` là lúc ghi hàng rào.
func (x *m17) statFence(c system.Cmd) system.Result {
	p := c.Args[len(c.Args)-1]
	x.mu.Lock()
	defer x.mu.Unlock()
	for _, cm := range x.main {
		if cm.fence == p {
			return exit(0, strconv.FormatInt(cm.fenced.Unix(), 10)+"\n", "")
		}
	}
	return okResult
}

// show trả MainPID và ActiveEnterTimestamp của dịch vụ PHP của lồng, ở cả
// dạng "Khoá=giá trị" (cage.runtime) lẫn --value (CageRestart). Dịch vụ không
// chạy: MainPID=0, như systemd.
func (x *m17) show(c system.Cmd) system.Result {
	unit := c.Args[len(c.Args)-1]
	var props []string
	value := false
	for i, a := range c.Args {
		switch {
		case a == "-p" && i+1 < len(c.Args):
			props = append(props, c.Args[i+1])
		case a == "--value":
			value = true
		}
	}
	x.machine.mu.Lock()
	running := x.units[unit]
	x.machine.mu.Unlock()
	x.mu.Lock()
	cm, known := x.main[unit]
	x.mu.Unlock()
	vals := map[string]string{"MainPID": "0", "ActiveEnterTimestamp": ""}
	if running && known {
		vals["MainPID"] = strconv.Itoa(cm.pid)
		vals["ActiveEnterTimestamp"] = "@" + strconv.FormatInt(cm.started.Unix(), 10)
	}
	var out strings.Builder
	for _, p := range props {
		if value {
			out.WriteString(vals[p] + "\n")
		} else {
			out.WriteString(p + "=" + vals[p] + "\n")
		}
	}
	return exit(0, out.String(), "")
}

// socketUnits đăng ký `systemctl enable --now` và `disable --now` của unit
// socket của lồng của mọi site active: systemd tạo file socket
// (www-data:www-data 0660) khi bật và gỡ khi tắt. Prefix có tên unit (dài hơn
// handler chung của m10.go) nên mọi unit khác vẫn đi qua m10.go.
func (x *m17) socketUnits(sites []site.Site) {
	for _, st := range sites {
		if st.Status != site.Active {
			continue
		}
		l := st.Layout()
		unit := l.CageSocketUnit()
		x.Handle([]string{"systemctl", "enable", "--now", unit}, func(system.Cmd) system.Result {
			x.machine.mu.Lock()
			x.units[unit] = true
			x.machine.mu.Unlock()
			x.SetDir(l.CageRunDir(), 0o755, rootOwner)
			x.SetSocket(l.CageSocket(), 0o660, wwwOwner)
			return okResult
		})
		x.Handle([]string{"systemctl", "disable", "--now", unit}, func(system.Cmd) system.Result {
			x.machine.mu.Lock()
			delete(x.units, unit)
			x.machine.mu.Unlock()
			_ = x.Remove(l.CageSocket())
			return okResult
		})
	}
}

// startMain giả lập dịch vụ PHP của lồng đang chạy (request đầu tiên đã đánh
// thức nó): unit active, và /proc/<pid> của tiến trình chính đúng như hàng rào
// dựng (NoNewPrivs, không capability, nằm dưới slice, các lần gắn riêng).
func (x *m17) startMain(l site.Layout, pid int, fenced, started time.Time) {
	svc := l.CageServiceUnit()
	x.machine.mu.Lock()
	x.units[svc] = true
	x.machine.mu.Unlock()
	x.mu.Lock()
	x.main[svc] = cageMain{pid: pid, fence: l.CageFence(), fenced: fenced, started: started}
	x.mu.Unlock()

	proc := "/proc/" + strconv.Itoa(pid)
	uid := strconv.Itoa(uidOf(l.User))
	status := "Name:\tphp-fpm" + phpVer + "\nUmask:\t0022\nState:\tS (sleeping)\nPid:\t" + strconv.Itoa(pid) +
		"\nUid:\t" + uid + "\t" + uid + "\t" + uid + "\t" + uid + "\nGid:\t" + uid + "\t" + uid + "\t" + uid + "\t" + uid +
		"\nCapInh:\t0000000000000000\nCapPrm:\t0000000000000000\nCapEff:\t0000000000000000\nCapBnd:\t0000000000000000\nNoNewPrivs:\t1\nSeccomp:\t2\n"
	cg := "0::/tpan.slice/" + l.CageSlice() + "/" + svc + "\n"
	pub := "/var/www/" + l.Domain.String()
	mounts := strings.Join([]string{
		"501 480 8:1 / / ro,relatime master:1 - ext4 /dev/sda1 rw",
		"502 501 0:61 / /var rw,nosuid,nodev - tmpfs tmpfs rw,mode=755",
		"503 502 8:1 " + pub + " " + pub + " rw,relatime master:1 - ext4 /dev/sda1 rw",
		"504 501 0:62 / /run rw,nosuid,nodev - tmpfs tmpfs rw,mode=755",
		"505 504 0:25 /mysqld /run/mysqld rw,nosuid,nodev master:12 - tmpfs tmpfs rw,mode=755",
		"506 504 0:25 /systemd/resolve /run/systemd/resolve ro,nosuid,nodev master:12 - tmpfs tmpfs rw,mode=755",
		"507 501 0:63 / /etc/tpan rw,nosuid,nodev - tmpfs tmpfs rw,mode=755",
		"508 501 8:1 " + l.CagePasswd() + " /etc/passwd ro,relatime master:1 - ext4 /dev/sda1 rw",
		"509 501 8:1 " + l.CageGroup() + " /etc/group ro,relatime master:1 - ext4 /dev/sda1 rw",
		"510 501 0:64 / /tmp rw,nosuid,nodev - tmpfs tmpfs rw",
	}, "\n") + "\n"
	own := system.Owner{User: l.User, Group: l.User}
	x.SetFile(proc+"/status", []byte(status), 0o444, own)
	x.SetFile(proc+"/cgroup", []byte(cg), 0o444, own)
	x.SetFile(proc+"/mountinfo", []byte(mounts), 0o444, own)
	x.SetSymlink("/usr/sbin/php-fpm"+phpVer, proc+"/exe")
}

// ---------------------------------------------------------------- số liệu

// cageLoad sinh số liệu của một lồng theo thời gian: tải theo ngày đêm (đỉnh
// 20 giờ, giờ Việt Nam) cộng nhiễu, các bộ đếm cộng dồn như cgroup. Cùng một
// bộ sinh cho mẫu trong DB và cho file cgroup, nên mẫu cuối khớp file và bộ
// lấy mẫu thật của serve nối tiếp liền mạch (bộ đếm không bao giờ giảm).
type cageLoad struct {
	l   site.Layout
	lim account.Limits

	memBase, memAmp int64   // byte
	cpuBase, cpuAmp float64 // % một nhân
	tasksBase       int64
	ioR, ioW        float64 // byte/giây lúc bận nhất
	ooms            []time.Time

	rng *rand.Rand
	at  time.Time
	u   cage.Usage
}

func newLoad(l site.Layout, lim account.Limits, from time.Time) *cageLoad {
	h := fnv.New64a()
	_, _ = h.Write([]byte(l.Domain.String()))
	seed := h.Sum64()
	g := &cageLoad{l: l, lim: lim, rng: rand.New(rand.NewPCG(seed, seed>>1)), at: from}
	g.u.Running = true
	g.u.MemMax = int64(lim.RAMMB) * mib
	g.u.CPUQuotaUs, g.u.CPUPeriod = int64(lim.CPUPercent)*1000, 100000
	g.u.TasksMax = int64(lim.Tasks)
	g.u.IORBps, g.u.IOWBps = int64(lim.IOReadMB)*1_000_000, int64(lim.IOWriteMB)*1_000_000
	return g
}

// busy là mức bận theo giờ trong ngày (0 lúc 8 giờ sáng, 1 lúc 20 giờ), giờ +7.
func busy(t time.Time) float64 {
	h := float64((t.Unix()+7*3600)%86400) / 3600
	return (1 + math.Cos(2*math.Pi*(h-20)/24)) / 2
}

// advance đưa bộ sinh tới thời điểm t (sau lần trước).
func (g *cageLoad) advance(t time.Time) {
	dt := t.Sub(g.at)
	if dt <= 0 {
		return
	}
	b, n := busy(t), g.rng.Float64()
	u := &g.u
	mem := g.memBase + int64(float64(g.memAmp)*b) + int64(n*8*float64(mib))
	want := g.cpuBase + g.cpuAmp*b + n*6
	used := want
	if q := float64(g.lim.CPUPercent); q > 0 && want > q {
		used = q
		frac := (want - q) / want
		u.ThrottledUsec += int64(frac * float64(dt.Microseconds()))
		u.NrThrottled += int64(frac * float64(dt/(100*time.Millisecond)))
	}
	u.CPUUsec += int64(used / 100 * float64(dt.Microseconds()))
	tasks := g.tasksBase + int64(math.Round(5*b+2*n))
	u.IORBytes += int64(g.ioR * (0.2 + b) * dt.Seconds() / 10)
	u.IOWBytes += int64(g.ioW * (0.2 + b) * dt.Seconds() / 10)
	for _, o := range g.ooms {
		if o.After(g.at) && !o.After(t) {
			// Một request ăn RAM tới trần: worker bị giết, memory.peak sát trần.
			u.OOMKills++
			u.MemMaxHits += 12 + int64(g.rng.IntN(9))
			mem = u.MemMax - 3*mib
			u.MemPeak = max(u.MemPeak, u.MemMax-mib/2)
			tasks += 4
		}
	}
	u.MemCurrent = mem
	u.MemPeak = max(u.MemPeak, mem)
	u.Tasks = tasks
	u.TasksPeak = max(u.TasksPeak, tasks)
	u.At = t
	g.at = t
}

// writeCgroup ghi các file cgroup mà cage.ReadUsage và cage.limits đọc, đúng
// dạng của kernel 6.8 (không có pids.events.local: bộ đếm chạm trần tiến trình
// nằm ở cgroup con, ở đây là của dịch vụ PHP).
func (g *cageLoad) writeCgroup(m *machine) {
	u, dir := g.u, g.l.CageCgroup()
	limit := func(n int64) string {
		if n <= 0 {
			return "max"
		}
		return strconv.FormatInt(n, 10)
	}
	quota := "max"
	if u.CPUQuotaUs > 0 {
		quota = strconv.FormatInt(u.CPUQuotaUs, 10)
	}
	io := ""
	if u.IORBps > 0 || u.IOWBps > 0 {
		io = "8:0 rbps=" + limit(u.IORBps) + " wbps=" + limit(u.IOWBps) + " riops=max wiops=max\n"
	}
	swap := "max"
	if u.MemMax > 0 {
		swap = "0"
	}
	user := u.CPUUsec * 7 / 10
	files := map[string]string{
		"memory.current":  strconv.FormatInt(u.MemCurrent, 10),
		"memory.peak":     strconv.FormatInt(u.MemPeak, 10),
		"memory.max":      limit(u.MemMax),
		"memory.swap.max": swap,
		"memory.events": fmt.Sprintf("low 0\nhigh 0\nmax %d\noom %d\noom_kill %d\noom_group_kill 0",
			u.MemMaxHits, u.OOMKills, u.OOMKills),
		"cpu.stat": fmt.Sprintf("usage_usec %d\nuser_usec %d\nsystem_usec %d\nnr_periods %d\nnr_throttled %d\nthrottled_usec %d\nnr_bursts 0\nburst_usec 0",
			u.CPUUsec, user, u.CPUUsec-user, u.CPUUsec/40_000, u.NrThrottled, u.ThrottledUsec),
		"cpu.max":      quota + " 100000",
		"pids.current": strconv.FormatInt(u.Tasks, 10),
		"pids.peak":    strconv.FormatInt(u.TasksPeak, 10),
		"pids.max":     limit(u.TasksMax),
		"pids.events":  "max 0",
		"io.stat": fmt.Sprintf("8:0 rbytes=%d wbytes=%d rios=%d wios=%d dbytes=0 dios=0",
			u.IORBytes, u.IOWBytes, u.IORBytes/16384, u.IOWBytes/8192),
		"io.max": strings.TrimSuffix(io, "\n"),
	}
	for name, v := range files {
		if v != "" {
			v += "\n"
		}
		m.SetFile(dir+"/"+name, []byte(v), 0o644, rootOwner)
	}
	m.SetFile(dir+"/"+g.l.CageServiceUnit()+"/pids.events", []byte("max 0\n"), 0o644, rootOwner)
}

// runCgroups cập nhật file cgroup của các lồng đang chạy theo nhịp cgroupTick
// cho tới khi ctx bị huỷ, như runProc của machine.go.
func (m *machine) runCgroups(ctx context.Context, loads []*cageLoad) {
	t := time.NewTicker(cgroupTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			for _, g := range loads {
				g.advance(now)
				g.writeCgroup(m)
			}
		}
	}
}

// ---------------------------------------------------------------- seed

// demoCage là một site mẫu vào lồng.
type demoCage struct {
	dom  string
	ago  time.Duration // vào lồng cách đây bao lâu
	pid  int
	load func(*cageLoad)
}

// seedM17 gọi sau seedM18: gói và user của seedM13 đã có, các site mẫu đã có.
func seedM17(ctx context.Context, m *machine, st *store.Store) error {
	x := m.m17Setup()
	now := time.Now().UTC()
	at := func(d time.Duration) func() time.Time { return func() time.Time { return time.Now().Add(-d) } }

	// Gói: co_ban bật lồng (trần nhỏ), nang_cao mới (trần rộng) cho khach_b.
	acc := &core.Accounts{Sys: m, Store: st, Journal: st, Now: at(10 * 24 * time.Hour)}
	basic, ok, err := st.Package(ctx, pkgBasic)
	if err != nil || !ok {
		return fmt.Errorf("đọc gói %s: có=%v, %w", pkgBasic, ok, err)
	}
	basic.Cage, basic.CPUPercent, basic.RAMMB, basic.Tasks = true, basicLimits.CPUPercent, basicLimits.RAMMB, basicLimits.Tasks
	basic.IOReadMB, basic.IOWriteMB, basic.PHPWorkers = basicLimits.IOReadMB, basicLimits.IOWriteMB, basicLimits.PHPWorkers
	if err := acc.EditPackage(ctx, basic); err != nil {
		return fmt.Errorf("bật lồng cho gói %s: %w", pkgBasic, err)
	}
	plus := account.Package{Name: pkgPlus, MaxSites: 5, MaxDBs: 3, DiskMB: 4096, MaxBackups: 5, Cage: true,
		CPUPercent: plusLimits.CPUPercent, RAMMB: plusLimits.RAMMB, Tasks: plusLimits.Tasks,
		IOReadMB: plusLimits.IOReadMB, IOWriteMB: plusLimits.IOWriteMB, PHPWorkers: plusLimits.PHPWorkers}
	if err := acc.AddPackage(ctx, plus); err != nil {
		return fmt.Errorf("tạo gói %s: %w", pkgPlus, err)
	}
	name := pkgPlus
	if _, err := acc.EditUser(ctx, userB, core.UserChange{Package: &name}, account.Identity{Role: account.RoleAdmin}); err != nil {
		return fmt.Errorf("đổi gói của %s: %w", userB, err)
	}

	all, err := st.Sites(ctx)
	if err != nil {
		return fmt.Errorf("đọc danh sách site: %w", err)
	}
	x.socketUnits(all)

	cages := []demoCage{
		{dom: siteLongShop, ago: 7*24*time.Hour + 2*time.Hour, pid: 41873, load: func(g *cageLoad) {
			g.memBase, g.memAmp, g.cpuBase, g.cpuAmp, g.tasksBase = 104*mib, 72*mib, 3, 46, 6
			g.ioR, g.ioW = 9e6, 4e6
			// Hai lần chạm trần RAM vào giờ cao điểm (20 giờ, giờ +7) của ba ngày
			// trước và hôm qua.
			for _, d := range []int{3, 1} {
				day := now.Add(-time.Duration(d) * 24 * time.Hour)
				g.ooms = append(g.ooms, time.Date(day.Year(), day.Month(), day.Day(), 13, 2, 0, 0, time.UTC))
			}
		}},
		{dom: siteLongNews, ago: 29*time.Hour + 30*time.Minute, pid: 52210, load: func(g *cageLoad) {
			g.memBase, g.memAmp, g.cpuBase, g.cpuAmp, g.tasksBase = 58*mib, 46*mib, 1.5, 13, 4
			g.ioR, g.ioW = 3e6, 1.5e6
		}},
	}
	var loads []*cageLoad
	for _, c := range cages {
		g, err := seedCage(ctx, x, st, c, now)
		if err != nil {
			return err
		}
		loads = append(loads, g)
	}
	// Dòng gộp theo giờ: chính phép gộp của serve, ở thời điểm dựng demo.
	if err := (&core.Usages{Sys: m, Store: st, Accounts: st, Samples: st, Now: func() time.Time { return now }}).Rollup(ctx); err != nil {
		return fmt.Errorf("gộp số liệu theo giờ của demo: %w", err)
	}
	if err := seedProbe(ctx, st, now); err != nil {
		return err
	}
	go m.runCgroups(ctx, loads)
	return nil
}

// seedCage đưa một site vào lồng bằng Plan thật (lùi về c.ago), cho dịch vụ
// PHP của lồng chạy, rồi ghi mẫu 5 phút từ lúc vào lồng tới now và file cgroup
// khớp mẫu cuối.
func seedCage(ctx context.Context, x *m17, st *store.Store, c demoCage, now time.Time) (*cageLoad, error) {
	d, err := validate.ParseDomain(c.dom)
	if err != nil {
		return nil, err
	}
	cg := &core.Cages{Sys: x, Settings: st, Store: st, Accounts: st,
		Journal: pastJournal{Journal: st, ago: c.ago}, Now: func() time.Time { return time.Now().Add(-c.ago) }}
	p, err := cg.PrepareEnable(ctx, d)
	if err != nil {
		return nil, fmt.Errorf("chuẩn bị đưa %s vào lồng: %w", c.dom, err)
	}
	rep, err := p.Execute(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("đưa %s vào lồng: %w", c.dom, err)
	}
	if rep.Job.Status != engine.JobSucceeded {
		return nil, fmt.Errorf("đưa %s vào lồng: job %s: %s", c.dom, rep.Job.Status, rep.Job.Error)
	}
	st2, _, err := st.Site(ctx, d)
	if err != nil {
		return nil, err
	}
	lim, ok, err := st.Package(ctx, map[string]string{userA: pkgBasic, userB: pkgPlus}[st2.Owner])
	if err != nil || !ok {
		return nil, fmt.Errorf("đọc gói của %s: có=%v, %w", c.dom, ok, err)
	}
	l := st2.Layout()
	enabled := now.Add(-c.ago)
	// Dịch vụ khởi động lại lần cuối 3 giờ trước (sau khi ghi hàng rào).
	x.startMain(l, c.pid, enabled, now.Add(-3*time.Hour))

	// Mẫu 5 phút từ lúc vào lồng tới mốc 5 phút gần nhất (≥ 288 mẫu: gợi ý
	// không bị low_data).
	last := now.Truncate(core.UsageSampleEvery)
	first := last.Add(-c.ago).Truncate(core.UsageSampleEvery).Add(core.UsageSampleEvery)
	g := newLoad(l, lim.Limits(), first.Add(-core.UsageSampleEvery))
	c.load(g)
	for t := first; !t.After(last); t = t.Add(core.UsageSampleEvery) {
		g.advance(t)
		if err := st.AddUsageSample(ctx, cage.SampleOf(c.dom, g.u)); err != nil {
			return nil, err
		}
	}
	g.writeCgroup(x.machine)
	return g, nil
}

// ---------------------------------------------------------------- thử tấn công

// probeTitles là tiêu đề của 14 đường (chép từ probePaths của core/probe.go,
// biến không xuất ra ngoài gói; đổi bên đó thì đổi ở đây). web: đường chỉ có
// trên stack đó.
var probeTitles = []struct {
	code string
	web  core.WebServer
	vi   string
	en   string
}{
	{"L1", "", "PHP của A include wp-config.php của B qua OPcache", "A's PHP includes B's wp-config.php through OPcache"},
	{"L2", "", "opcache_get_status() gọi từ A", "opcache_get_status() called from A"},
	{"L3", core.Apache, ".htaccess của A đặt PHP_ADMIN_VALUE open_basedir=/", "A's .htaccess sets PHP_ADMIN_VALUE open_basedir=/"},
	{"L4", core.Apache, ".htaccess của A chuyển request sang socket PHP của B", "A's .htaccess hands a request to B's PHP socket"},
	{"L5", core.Nginx, "Symlink của A sang file của B, gọi qua nginx", "A symlink of A to a file of B, fetched through nginx"},
	{"L6", "", "A đọc access.log của B", "A reads B's access.log"},
	{"L7", "", "PHP của A đếm tiến trình của user khác", "A's PHP counts processes of other users"},
	{"L8", "", "Vhost lạ trùng tên miền của A", "A foreign vhost with A's domain"},
	{"L9", core.Apache, ".htaccess của A bật FollowSymLinks rồi symlink sang file của B", "A's .htaccess turns on FollowSymLinks, then a symlink to a file of B"},
	{"C1", "", "A liệt kê /var/www: có thư mục của B không", "A lists /var/www: is B's directory there"},
	{"C2", "", "A ghi một file vào /tmp; B có thấy không", "A writes a file to /tmp; can B see it"},
	{"C3", "", "A cấp phát 320 MB", "A allocates 320 MB"},
	{"C4", "", "A đọc tên user tp_* khác qua getent passwd", "A reads other tp_* user names through getent passwd"},
	{"C5", "", "A đọc /proc/self/net/unix: có socket mang slug của B không", "A reads /proc/self/net/unix: is there a socket named after B's slug"},
}

// probeCurl là bằng chứng của một lần gọi file thử, đúng lệnh probeGet chạy.
func probeCurl(dom, file, mark string, n int, user, code string) checks.Evidence {
	cmd := system.Cmd{Name: "curl", Args: []string{"--silent", "--show-error", "--max-time", "60",
		"--resolve", dom + ":80:127.0.0.1", "--write-out", "\nTPAN_HTTP=%{http_code}", "http://" + dom + "/" + file}}
	out := "HTTP " + code
	if mark != "" {
		out = "TPAN_PROBE=" + mark + " user=" + user + " n=" + strconv.Itoa(n) + " | " + out
	}
	return checks.Evidence{Command: cmd.String(), Output: out}
}

// probeReport dựng báo cáo của một lượt đủ hai vòng trên stack nginx: ngoài
// lồng mọi đường L bị chặn, mọi đường C là needs_cage (trừ C3 bỏ qua vì máy
// lúc đó ít RAM trống); trong lồng mọi ô bị chặn.
func probeReport(id, a, b string, started, finished time.Time) core.ProbeReport {
	la, _ := siteOf(a)
	lb, _ := siteOf(b)
	ua := la.User
	cell := func(status, vi, en string, ev ...checks.Evidence) core.ProbeCell {
		return core.ProbeCell{Status: status, Detail: i18n.T(vi, en), Evidence: ev}
	}
	blocked := func(vi, en string, ev ...checks.Evidence) core.ProbeCell {
		return cell(core.ProbeBlocked, vi, en, ev...)
	}
	l8 := checks.Evidence{Command: "nginx -T", Output: "/etc/nginx/sites-enabled/aaa-tpan-probe-" + strings.TrimSuffix(strings.TrimPrefix(a, "tpan-probe-a-"), ".test") +
		".conf:3: " + a + " (port 80)\n/etc/nginx/sites-enabled/" + a + ".conf:4: " + a + " (port 80)"}
	l8Name := "aaa-tpan-probe-" + strings.TrimSuffix(strings.TrimPrefix(a, "tpan-probe-a-"), ".test") + ".conf"
	out := map[string][2]core.ProbeCell{
		"L1": {
			blocked("PHP của A include wp-config.php của B: không lấy được (3 lần thử)", "A's PHP includes B's wp-config.php: not readable (3 tries)", probeCurl(a, "tpan-probe-l1.php", "KHONG", 0, ua, "200")),
			blocked("PHP của A include wp-config.php của B: không lấy được (3 lần thử)", "A's PHP includes B's wp-config.php: not readable (3 tries)", probeCurl(a, "tpan-probe-l1.php", "KHONG", 0, ua, "200")),
		},
		"L2": {
			blocked("opcache_get_status() từ A bị chặn", "opcache_get_status() from A is blocked", probeCurl(a, "tpan-probe-l2.php", "KHONG", 0, ua, "200")),
			blocked("opcache_get_status() từ A bị chặn", "opcache_get_status() from A is blocked", probeCurl(a, "tpan-probe-l2.php", "KHONG", 0, ua, "200")),
		},
		"L5": {
			blocked("symlink của A sang file 0640 của B qua nginx: HTTP 403, không lộ", "A's symlink to B's 0640 file through nginx: HTTP 403, not exposed", probeCurl(a, "tpan-probe-lay.txt", "", 0, "", "403")),
			blocked("symlink của A sang file 0640 của B qua nginx: HTTP 403, không lộ", "A's symlink to B's 0640 file through nginx: HTTP 403, not exposed", probeCurl(a, "tpan-probe-lay.txt", "", 0, "", "403")),
		},
		"L6": {
			blocked("user của A không đọc được access.log của B", "A's user cannot read B's access.log", probeCurl(a, "tpan-probe-l6.php", "KHONG", 0, ua, "200")),
			blocked("user của A không đọc được access.log của B", "A's user cannot read B's access.log", probeCurl(a, "tpan-probe-l6.php", "KHONG", 0, ua, "200")),
		},
		"L7": {
			blocked("PHP của A không thấy tiến trình nào của user khác", "A's PHP sees no process of another user", probeCurl(a, "tpan-probe-l7.php", "KHONG", 0, ua, "200")),
			blocked("PHP của A không thấy tiến trình nào của user khác", "A's PHP sees no process of another user", probeCurl(a, "tpan-probe-l7.php", "KHONG", 0, ua, "200")),
		},
		"L8": {
			blocked("phát hiện: web.server_name.unique chỉ ra "+l8Name, "detected: web.server_name.unique points to "+l8Name, l8),
			blocked("phát hiện: web.server_name.unique chỉ ra "+l8Name+" (như ngoài lồng, không chạy lại)", "detected: web.server_name.unique points to "+l8Name+" (same as outside, not run again)", l8),
		},
		"C1": {
			cell(core.ProbeNeedsCage, "A thấy thư mục của B trong /var/www (10 mục)", "A sees B's directory in /var/www (10 entries)", probeCurl(a, "tpan-probe-c1.php", "CO", 10, ua, "200")),
			blocked("A không thấy thư mục của B trong /var/www (1 mục)", "A does not see B's directory in /var/www (1 entries)", probeCurl(a, "tpan-probe-c1.php", "KHONG", 1, ua, "200")),
		},
		"C2": {
			cell(core.ProbeNeedsCage, "B thấy file A vừa ghi vào /tmp (dùng chung /tmp)", "B sees the file A just wrote to /tmp (shared /tmp)",
				probeCurl(a, "tpan-probe-c2.php", "CO", 1, ua, "200"), probeCurl(b, "tpan-probe-c2b.php", "CO", 1, lb.User, "200"), probeCurl(a, "tpan-probe-c2x.php", "CO", 1, ua, "200")),
			blocked("B không thấy file A vừa ghi vào /tmp", "B does not see the file A just wrote to /tmp",
				probeCurl(a, "tpan-probe-c2.php", "CO", 1, ua, "200"), probeCurl(b, "tpan-probe-c2b.php", "KHONG", 0, lb.User, "200"), probeCurl(a, "tpan-probe-c2x.php", "CO", 1, ua, "200")),
		},
		"C3": {
			cell(core.ProbeSkipped, "bỏ qua: máy chỉ còn 812 MB RAM trống (cần ít nhất 1024 MB để cấp phát 320 MB ngoài lồng mà không làm hại máy)",
				"skipped: the machine has only 812 MB of free RAM (needs at least 1024 MB to allocate 320 MB outside a cage without harming it)"),
			blocked("tiến trình của A bị chặn (HTTP 502), oom_kill tăng 1, B trả HTTP 200", "A's process is stopped (HTTP 502), oom_kill rose by 1, B answers HTTP 200",
				probeCurl(a, "tpan-probe-c3.php", "", 0, "", "502"), probeCurl(b, "", "", 0, "", "200"),
				checks.Evidence{Command: "cat " + la.CageCgroup() + "/memory.events", Output: "oom_kill 0 -> 1"}),
		},
		"C4": {
			cell(core.ProbeNeedsCage, "A đọc ra 8 user tp_* khác", "A reads 8 other tp_* users", probeCurl(a, "tpan-probe-c4.php", "CO", 8, ua, "200")),
			blocked("A không đọc ra user tp_* nào khác", "A reads no other tp_* user", probeCurl(a, "tpan-probe-c4.php", "KHONG", 0, ua, "200")),
		},
		"C5": {
			cell(core.ProbeNeedsCage, "A thấy 1 socket mang slug của B trong /proc/self/net/unix", "A sees 1 sockets named after B's slug in /proc/self/net/unix", probeCurl(a, "tpan-probe-c5.php", "CO", 1, ua, "200")),
			blocked("A không thấy socket nào mang slug của B", "A sees no socket named after B's slug", probeCurl(a, "tpan-probe-c5.php", "KHONG", 0, ua, "200")),
		},
	}
	rep := core.ProbeReport{ID: id, Started: started, Finished: finished, Web: core.Nginx, DomainA: a, DomainB: b}
	for _, p := range probeTitles {
		r := core.ProbeResult{Path: p.code, Title: i18n.T(p.vi, p.en)}
		if p.web != "" && p.web != core.Nginx {
			na := core.ProbeCell{Status: core.ProbeNA, Detail: i18n.T("không thuộc stack nginx", "not part of the nginx stack")}
			r.Outside, r.Inside = na, na
		} else {
			r.Outside, r.Inside = out[p.code][0], out[p.code][1]
		}
		rep.Results = append(rep.Results, r)
	}
	return rep
}

// seedProbe ghi sổ probe_runs bằng hàm của store: một lượt xong và đã dọn hai
// ngày trước, một lượt hỏng 40 phút trước mà chưa dọn (dưới 2 giờ nên
// probe.leftover của health chưa báo; quá 2 giờ thì bộ hẹn giờ của serve tự
// dọn, như máy thật).
func seedProbe(ctx context.Context, st *store.Store, now time.Time) error {
	const hexDigits = "0123456789abcdef"
	run := func(started time.Duration) (cage.ProbeRun, error) {
		id, err := demoSecret(16, hexDigits)
		if err != nil {
			return cage.ProbeRun{}, err
		}
		sfx, err := demoSecret(12, hexDigits)
		if err != nil {
			return cage.ProbeRun{}, err
		}
		r := cage.ProbeRun{ID: id, Started: now.Add(-started).Truncate(time.Second),
			DomainA: "tpan-probe-a-" + sfx + ".test", DomainB: "tpan-probe-b-" + sfx + ".test"}
		if err := st.StartProbeRun(ctx, r); err != nil {
			return cage.ProbeRun{}, fmt.Errorf("ghi lượt thử demo: %w", err)
		}
		return r, nil
	}
	done, err := run(2*24*time.Hour + 3*time.Hour)
	if err != nil {
		return err
	}
	fin := done.Started.Add(4*time.Minute + 12*time.Second)
	data, err := json.Marshal(probeReport(done.ID, done.DomainA, done.DomainB, done.Started, fin))
	if err != nil {
		return err
	}
	if err := st.FinishProbeRun(ctx, done.ID, "done", data, fin); err != nil {
		return err
	}
	if err := st.MarkProbeCleaned(ctx, done.ID); err != nil {
		return err
	}
	// Lượt hỏng không có báo cáo: Probe.Last bỏ qua dòng không có báo cáo, nên
	// màn vẫn hiện lượt xong ở trên kèm câu "còn lượt chưa dọn".
	failed, err := run(40 * time.Minute)
	if err != nil {
		return err
	}
	return st.FinishProbeRun(ctx, failed.ID, "failed", nil, failed.Started.Add(2*time.Minute+40*time.Second))
}
