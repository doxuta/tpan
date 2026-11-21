package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

// ================================================================ máy giả

// bktMember là một thành viên của "file tar" giả (JSON của []bktMember), đủ để
// tar --create, --list --verbose, --extract giả đi vòng tròn với nhau như ở
// steps/backup_test.go.
type bktMember struct {
	Name string `json:"name"`
	Type string `json:"type"` // "-", "d", "l"
	Data string `json:"data,omitempty"`
	Link string `json:"link,omitempty"`
}

// bktStore là store thật, chỉ SnapshotDB ghi vào máy giả (store thật ghi bằng
// os.OpenFile vào /var/lib/tpan của máy chạy test).
type bktStore struct {
	*store.Store
	m *siteSim
}

func (s *bktStore) SnapshotDB(_ context.Context, p string) error {
	if _, err := s.m.Stat(p); err == nil {
		return fmt.Errorf("sao tpan.db: %s đã có", p)
	}
	if _, err := s.m.Stat(p[:strings.LastIndex(p, "/")]); err != nil {
		return fmt.Errorf("sao tpan.db: %w", err)
	}
	s.m.SetFile(p, []byte("sqlite giả"), 0o600, system.Root)
	return nil
}

// bktEnv là siteEnv cộng các lệnh của backup: DB giả theo tên (chuỗi), xuất
// và nhập DB, tar giả, sha256sum, du, chown -R, find -exec chmod.
type bktEnv struct {
	*siteEnv
	b  *Backups
	bs *bktStore

	mu         sync.Mutex
	body       map[string]string // tên DB → nội dung
	duFail     string            // du lỗi khi đường dẫn chứa chuỗi này
	importFail string            // `source <file>` lỗi khi file chứa chuỗi này
}

var bktNow = time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)

func bktWPConfig(db string) string {
	return "<?php\ndefine( 'DB_NAME', '" + db + "' );\ndefine( 'DB_USER', '" + db + "' );\ndefine( 'DB_PASSWORD', 'Abc123' );\n"
}

func bktDump(body string) string {
	return "-- MariaDB dump\nBODY:" + body + "\n-- Dump completed on 2026-11-20 08:00:00\n"
}

func bktSHA(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func argAfterPrefix(args []string, prefix string) string {
	for _, a := range args {
		if v, ok := strings.CutPrefix(a, prefix); ok {
			return v
		}
	}
	return ""
}

// newBktEnv tạo các site (nginx) bằng Plan tạo site thật trên siteSim, rồi
// thêm lệnh của backup. wp-config.php được thay bằng bản có DB_NAME, DB_USER
// như WP-CLI ghi (BackupExtract đọc lại nó).
func newBktEnv(t *testing.T, domains ...string) *bktEnv {
	t.Helper()
	se := newSiteEnv(t, Nginx)
	for _, d := range domains {
		se.add(t, d)
	}
	be := &bktEnv{siteEnv: se, bs: &bktStore{Store: se.st, m: se.m}, body: map[string]string{}}
	se.m.SetDir("/etc/cron.d", 0o755, system.Root) // gói cron của Ubuntu tạo sẵn
	be.b = &Backups{Sites: se.s, Store: be.bs, Now: func() time.Time { return bktNow }}
	m := se.m
	for _, d := range domains {
		l := site.LayoutOf(mustDomain(t, d))
		m.SetFile(l.WPConfig, []byte(bktWPConfig(l.DB)), 0o600, system.Owner{User: l.User, Group: l.User})
		m.SetFile(l.Public+"/index.php", []byte("live "+d), 0o640, system.Owner{User: l.User, Group: site.WebGroup})
		be.body[l.DB] = "live-db " + d
	}

	ok := system.Result{}
	exit := func(code int, stdout, stderr string) system.Result {
		return system.Result{ExitCode: code, Stdout: stdout, Stderr: stderr}
	}
	under := func(root string) []string {
		var out []string
		for p := range m.Snapshot() {
			if p == root || strings.HasPrefix(p, root+"/") {
				out = append(out, p)
			}
		}
		sort.Strings(out)
		return out
	}
	// write giữ quyền và chủ của file có sẵn, như fopen("w") của chương trình thật.
	write := func(p, data string) {
		perm, owner := fs.FileMode(0o644), system.Root
		if fi, err := m.Stat(p); err == nil {
			perm, owner = fi.Mode.Perm(), fi.Owner
		}
		m.SetFile(p, []byte(data), perm, owner)
	}

	// Câu đo cỡ DB của steps.MeasureDB; câu khác đi như handler của siteSim.
	m.Handle([]string{"mariadb", "-N", "-B"}, func(c system.Cmd) system.Result {
		sql := string(c.Stdin)
		if strings.Contains(sql, "information_schema.TABLES") {
			return exit(0, "1000\n", "")
		}
		return bktSQL(m, sql)
	})
	m.Handle([]string{"mariadb"}, func(c system.Cmd) system.Result {
		db := argAfterPrefix(c.Args, "--database=")
		src, _ := strings.CutPrefix(argAfter(c.Args, "-e"), "source ")
		data, err := m.ReadFile(src)
		if err != nil {
			return exit(1, "", "ERROR: Failed to open file '"+src+"'")
		}
		be.mu.Lock()
		defer be.mu.Unlock()
		if be.importFail != "" && strings.Contains(src, be.importFail) {
			be.body[db] = "half"
			return exit(1, "", "ERROR 1064 (42000) at line 2 in file: '"+src+"'")
		}
		for _, ln := range strings.Split(string(data), "\n") {
			if b, ok := strings.CutPrefix(ln, "BODY:"); ok {
				be.body[db] = b
			}
		}
		return ok
	})
	m.Handle([]string{"mariadb-dump"}, func(c system.Cmd) system.Result {
		be.mu.Lock()
		body := be.body[c.Args[len(c.Args)-1]]
		be.mu.Unlock()
		write(argAfterPrefix(c.Args, "--result-file="), bktDump(body))
		return ok
	})
	m.Handle([]string{"tail", "-n", "1"}, func(c system.Cmd) system.Result {
		data, err := m.ReadFile(c.Args[len(c.Args)-1])
		if err != nil {
			return exit(1, "", err.Error())
		}
		lines := strings.SplitAfter(strings.TrimSuffix(string(data), "\n"), "\n")
		return exit(0, lines[len(lines)-1]+"\n", "")
	})
	m.Handle([]string{"sha256sum"}, func(c system.Cmd) system.Result {
		p := c.Args[len(c.Args)-1]
		data, err := m.ReadFile(p)
		if err != nil {
			return exit(1, "", err.Error())
		}
		return exit(0, bktSHA(data)+"  "+p+"\n", "")
	})
	m.Handle([]string{"du"}, func(c system.Cmd) system.Result {
		p := c.Args[len(c.Args)-1]
		be.mu.Lock()
		bad := be.duFail != "" && strings.Contains(p, be.duFail)
		be.mu.Unlock()
		if bad {
			return exit(1, "", "du: cannot access '"+p+"': Input/output error")
		}
		return exit(0, "5000\t"+p+"\n", "")
	})
	m.Handle([]string{"wp", "core", "version"}, func(system.Cmd) system.Result { return exit(0, "6.7.1\n", "") })
	m.Handle([]string{"tar", "--create"}, func(c system.Cmd) system.Result {
		var file, dir string
		var ms []bktMember
		for _, a := range c.Args[1:] {
			switch {
			case strings.HasPrefix(a, "--file="):
				file = strings.TrimPrefix(a, "--file=")
			case strings.HasPrefix(a, "--directory="):
				dir = strings.TrimPrefix(a, "--directory=")
			case strings.HasPrefix(a, "--"):
			default:
				snap := m.Snapshot()
				for _, p := range under(dir + "/" + a) {
					e := snap[p]
					name := strings.TrimPrefix(p, dir+"/")
					switch {
					case e.Mode.IsDir():
						ms = append(ms, bktMember{Name: name + "/", Type: "d"})
					case e.Mode&fs.ModeSymlink != 0:
						ms = append(ms, bktMember{Name: name, Type: "l", Link: e.Target})
					default:
						ms = append(ms, bktMember{Name: name, Type: "-", Data: e.Data})
					}
				}
			}
		}
		data, _ := json.Marshal(ms)
		write(file, string(data))
		return ok
	})
	readArchive := func(c system.Cmd) ([]bktMember, string, error) {
		data, err := m.ReadFile(argAfterPrefix(c.Args, "--file="))
		if err != nil {
			return nil, "", err
		}
		var ms []bktMember
		if err := json.Unmarshal(data, &ms); err != nil {
			return nil, "", err
		}
		return ms, argAfterPrefix(c.Args, "--directory="), nil
	}
	m.Handle([]string{"tar", "--list"}, func(c system.Cmd) system.Result {
		ms, _, err := readArchive(c)
		if err != nil {
			return exit(2, "", "tar: "+err.Error())
		}
		var b strings.Builder
		for _, x := range ms {
			fmt.Fprintf(&b, "%srw-r--r-- root/root %9d 2026-11-19 10:00 %s", x.Type, len(x.Data), x.Name)
			if x.Type == "l" {
				b.WriteString(" -> " + x.Link)
			}
			b.WriteByte('\n')
		}
		return exit(0, b.String(), "")
	})
	m.Handle([]string{"tar", "--extract"}, func(c system.Cmd) system.Result {
		ms, dir, err := readArchive(c)
		if err != nil {
			return exit(2, "", "tar: "+err.Error())
		}
		for _, x := range ms {
			p := dir + "/" + strings.TrimSuffix(x.Name, "/")
			switch x.Type {
			case "d":
				m.SetDir(p, 0o755, system.Root)
			case "l":
				m.SetSymlink(x.Link, p)
			default:
				m.SetFile(p, []byte(x.Data), 0o644, system.Root)
			}
		}
		return ok
	})
	m.Handle([]string{"chown", "-R", "-h"}, func(c system.Cmd) system.Result {
		u, g, _ := strings.Cut(c.Args[2], ":")
		for _, p := range under(c.Args[len(c.Args)-1]) {
			if err := m.Chown(p, system.Owner{User: u, Group: g}); err != nil {
				return exit(1, "", err.Error())
			}
		}
		return ok
	})
	m.Handle([]string{"find", "-P"}, func(c system.Cmd) system.Result {
		root, typ, mode := c.Args[1], c.Args[3], c.Args[6]
		n, err := strconv.ParseUint(mode, 8, 32)
		if err != nil {
			return exit(1, "", "chmod: invalid mode")
		}
		perm := fs.FileMode(n) & fs.ModePerm
		if n&0o2000 != 0 {
			perm |= fs.ModeSetgid
		}
		snap := m.Snapshot()
		for _, p := range under(root) {
			e := snap[p]
			if (typ == "d" && e.Mode.IsDir()) || (typ == "f" && e.Mode.IsRegular()) {
				if err := m.Chmod(p, perm); err != nil {
					return exit(1, "", err.Error())
				}
			}
		}
		return ok
	})
	return be
}

// bktSQL là handler `mariadb -N -B` của siteSim (site_test.go) chép lại: cùng
// prefix thì handler đăng ký sau thắng, nên handler đo cỡ DB ở trên phải tự
// làm nốt phần của siteSim.
func bktSQL(m *siteSim, sql string) system.Result {
	m.mu.Lock()
	defer m.mu.Unlock()
	b := func(v bool) int {
		if v {
			return 1
		}
		return 0
	}
	if g := sqlStateRE.FindStringSubmatch(sql); g != nil {
		return system.Result{Stdout: fmt.Sprintf("%d\t%d\n", b(m.dbs[g[1]]), b(m.dbUsers[g[1]]))}
	}
	if g := sqlLockState.FindStringSubmatch(sql); g != nil {
		return system.Result{Stdout: fmt.Sprintf("%d\t%d\n", b(m.dbUsers[g[1]]), b(m.dbLocked[g[1]]))}
	}
	for _, g := range sqlLock.FindAllStringSubmatch(sql, -1) {
		m.dbLocked[g[1]] = g[2] == "LOCK"
	}
	for _, g := range sqlCreateDB.FindAllStringSubmatch(sql, -1) {
		m.dbs[g[1]] = true
	}
	for _, g := range sqlCreateUsr.FindAllStringSubmatch(sql, -1) {
		m.dbUsers[g[1]] = true
	}
	for _, g := range sqlDropUsr.FindAllStringSubmatch(sql, -1) {
		delete(m.dbUsers, g[1])
		delete(m.dbLocked, g[1])
	}
	for _, g := range sqlDropDB.FindAllStringSubmatch(sql, -1) {
		delete(m.dbs, g[1])
	}
	return system.Result{}
}

func (be *bktEnv) dbBody(l site.Layout) string {
	be.mu.Lock()
	defer be.mu.Unlock()
	return be.body[l.DB]
}

func (be *bktEnv) read(t *testing.T, p string) string {
	t.Helper()
	data, err := be.m.ReadFile(p)
	if err != nil {
		t.Fatalf("đọc %s: %v", p, err)
	}
	return string(data)
}

// seedFull ghi tay một bản backup đầy đủ (file tar giả và dòng trong bảng)
// với giờ stamp: BackupArchive đặt tên file theo đồng hồ thật tới từng giây,
// nên hai bản tạo bằng Plan trong cùng một test sẽ trùng tên.
func (be *bktEnv) seedFull(t *testing.T, dom, stamp, index, db string, reason BackupReason) Backup {
	t.Helper()
	l := site.LayoutOf(mustDomain(t, dom))
	ms := []bktMember{
		{Name: "manifest.json", Type: "-", Data: `{"domain":"` + dom + `"}`},
		{Name: "db.sql", Type: "-", Data: bktDump(db)},
		{Name: "public/", Type: "d"},
		{Name: "public/index.php", Type: "-", Data: index},
		{Name: "public/wp-includes/", Type: "d"},
		{Name: "public/wp-includes/version.php", Type: "-", Data: "<?php $wp_version = '7.1.5';\n"},
		{Name: "public/wp-config.php", Type: "-", Data: "<?php // bản trong backup\n"},
	}
	data, _ := json.Marshal(ms)
	return be.seedFile(t, l, dom+"-"+stamp+".tar.gz", data, BackupFull, reason, stamp)
}

func (be *bktEnv) seedFile(t *testing.T, l site.Layout, name string, data []byte, kind BackupKind, reason BackupReason, stamp string) Backup {
	t.Helper()
	at, err := time.Parse("20060102-150405", stamp)
	if err != nil {
		t.Fatal(err)
	}
	p := site.BackupDir(l) + "/" + name
	be.m.SetDir(site.BackupRoot, 0o700, system.Root)
	be.m.SetDir(site.BackupDir(l), 0o700, system.Root)
	be.m.SetFile(p, data, 0o600, system.Root)
	r, err := be.bs.AddBackup(ctx, Backup{Domain: l.Domain.String(), Kind: kind, Reason: reason, Path: p,
		Size: int64(len(data)), SHA256: bktSHA(data), Created: at})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// mutatingRan: lệnh đã chạy mà thay đổi máy (xem trước chỉ được đo: du,
// sha256sum, tail, câu SELECT, getent, chage).
func mutatingRan(ran []system.Cmd) []string {
	var bad []string
	for _, c := range ran {
		switch c.Name {
		case "du", "sha256sum", "tail", "getent", "chage", "test":
			continue
		case "mariadb":
			if strings.HasPrefix(strings.TrimSpace(string(c.Stdin)), "SELECT") {
				continue
			}
		}
		if readOnly(c) {
			continue
		}
		bad = append(bad, c.String())
	}
	return bad
}

// ================================================================ Settings

func TestBackupSettings(t *testing.T) {
	be := newBktEnv(t)
	got, err := be.b.Settings(ctx)
	if err != nil || got != (BackupSettings{Keep: 3}) {
		t.Errorf("mặc định = %+v, %v; muốn {3 \"\"}", got, err)
	}
	mustSet(t, be.st, SettingBackupKeep, "7")
	mustSet(t, be.st, SettingBackupAt, "03:30")
	if got, err := be.b.Settings(ctx); err != nil || got != (BackupSettings{Keep: 7, At: "03:30"}) {
		t.Errorf("đọc = %+v, %v", got, err)
	}
	// Giá trị hỏng trong DB: lỗi, không đoán.
	for _, kv := range [][2]string{{SettingBackupKeep, "0"}, {SettingBackupKeep, "31"}, {SettingBackupKeep, "ba"},
		{SettingBackupAt, "3:30"}, {SettingBackupAt, "24:00"}, {SettingBackupAt, "03:30; rm -rf /"}} {
		be := newBktEnv(t)
		mustSet(t, be.st, kv[0], kv[1])
		if _, err := be.b.Settings(ctx); err == nil {
			t.Errorf("%s = %q mà không lỗi", kv[0], kv[1])
		}
	}
}

func TestBackupPrepareSettings(t *testing.T) {
	be := newBktEnv(t)
	for _, keep := range []int{0, -1, MaxBackupKeep + 1} {
		if _, err := be.b.PrepareSettings(ctx, keep, ""); refusedCode(t, err) != "backup_keep_invalid" {
			t.Errorf("keep %d: muốn backup_keep_invalid", keep)
		}
	}
	for _, at := range []string{"25:00", "3:30", "03:60", "03:30\n* * * * * root id", "$(id)", "03:30; rm -rf /"} {
		if _, err := be.b.PrepareSettings(ctx, 3, at); refusedCode(t, err) != "backup_at_invalid" {
			t.Errorf("at %q: muốn backup_at_invalid", at)
		}
	}

	f := site.BackupCronFile
	p, err := be.b.PrepareSettings(ctx, 5, "03:30")
	if err != nil {
		t.Fatal(err)
	}
	if p.Plan.Kind != "backup.settings" || p.Plan.Target != "backup" || !reflect.DeepEqual(stepIDs(p.Plan), []string{"file:" + f}) {
		t.Errorf("Plan = %s/%s %v", p.Plan.Kind, p.Plan.Target, stepIDs(p.Plan))
	}
	pv, err := p.Preview(ctx)
	if err != nil || pv.Steps[0].Done || be.exists(f) {
		t.Fatalf("xem trước: %v, done=%v, file có=%v", err, pv.Steps[0].Done, be.exists(f))
	}
	rep, err := p.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("chạy: %v %s (%s)", err, rep.Job.Status, rep.Job.Error)
	}
	fi, _ := be.m.Stat(f)
	if c := be.read(t, f); !strings.Contains(c, "30 03 * * * root /usr/local/bin/tpan backup create --all --scheduled --yes") || fi.Mode.Perm() != 0o644 || fi.Owner != system.Root {
		t.Errorf("file cron %v %v:\n%s", fi.Mode, fi.Owner, c)
	}
	if got, err := be.b.Settings(ctx); err != nil || got != (BackupSettings{Keep: 5, At: "03:30"}) {
		t.Errorf("sau khi đặt: %+v, %v", got, err)
	}

	// Tắt: gỡ file, giờ về rỗng; chạy lại khi đã tắt thì bước được bỏ qua.
	for i := range 2 {
		p, err := be.b.PrepareSettings(ctx, 2, "")
		if err != nil {
			t.Fatal(err)
		}
		rep, err := p.Execute(ctx, nil)
		if err != nil || rep.Job.Status != engine.JobSucceeded {
			t.Fatalf("tắt lần %d: %v %s (%s)", i, err, rep.Job.Status, rep.Job.Error)
		}
		if be.exists(f) {
			t.Errorf("tắt lần %d: %s vẫn còn", i, f)
		}
		if i == 1 && rep.Steps[0].Outcome != "skipped" {
			t.Errorf("tắt lần hai: bước %s, muốn bỏ qua", rep.Steps[0].Outcome)
		}
	}
	if got, _ := be.b.Settings(ctx); got != (BackupSettings{Keep: 2}) {
		t.Errorf("sau khi tắt: %+v", got)
	}
}

// ================================================================ Create

func TestBackupCreate(t *testing.T) {
	const dom = "shop.example.com"
	be := newBktEnv(t, dom, "blog.example.com")
	d := mustDomain(t, dom)
	l := site.LayoutOf(d)
	// Ba bản cũ (giữ 3 thì bản cũ nhất phải đi), một bản trước khi xoá hẳn
	// (không bao giờ bị dọn), một bản sql và một bản của site khác (không đụng).
	old := []Backup{
		be.seedFull(t, dom, "20261101-000000", "a", "a", ReasonManual),
		be.seedFull(t, dom, "20261102-000000", "b", "b", ReasonSchedule),
		be.seedFull(t, dom, "20261103-000000", "c", "c", ReasonManual),
	}
	purge := be.seedFull(t, dom, "20261001-000000", "p", "p", ReasonBeforePurge)
	sqlRow := be.seedFile(t, l, dom+"-db-20261001-000000.sql", []byte(bktDump("s")), BackupSQL, ReasonManual, "20261001-000000")
	other := be.seedFull(t, "blog.example.com", "20261001-000000", "o", "o", ReasonManual)

	p, err := be.b.PrepareCreate(ctx, d, ReasonManual)
	if err != nil {
		t.Fatal(err)
	}
	if p.Plan.Kind != "backup.create" || p.Plan.Target != dom || !reflect.DeepEqual(stepIDs(p.Plan), []string{"backup.create:" + dom}) {
		t.Errorf("Plan = %s/%s %v", p.Plan.Kind, p.Plan.Target, stepIDs(p.Plan))
	}
	be.m.ResetRan()
	pv, err := p.Preview(ctx)
	if err != nil || pv.Steps[0].Done || pv.Steps[0].CheckError != "" {
		t.Fatalf("xem trước: %v %+v", err, pv.Steps[0])
	}
	for _, c := range mutatingRan(be.m.Ran()) {
		t.Errorf("xem trước chạy lệnh thay đổi máy: %s", c)
	}

	rep, err := p.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("chạy: %v %s (%s)", err, rep.Job.Status, rep.Job.Error)
	}
	list, err := be.b.List(ctx, dom)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, r := range list {
		ids = append(ids, r.ID)
	}
	nw := list[0]
	want := []string{nw.ID, old[2].ID, old[1].ID, sqlRow.ID, purge.ID}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("sau khi dọn: %v, muốn %v (bản cũ nhất %s phải đi)", ids, want, old[0].ID)
	}
	if be.exists(old[0].Path) || !be.exists(purge.Path) || !be.exists(other.Path) {
		t.Errorf("file: bản cũ nhất còn=%v, bản trước xoá hẳn còn=%v, site khác còn=%v", be.exists(old[0].Path), be.exists(purge.Path), be.exists(other.Path))
	}
	if nw.Kind != BackupFull || nw.Reason != ReasonManual || nw.JobID != rep.Job.ID || nw.PHPVersion != "8.3" ||
		nw.WPVersion != "6.7.1" || !nw.Created.Equal(bktNow) || !strings.HasPrefix(nw.Path, site.BackupDir(l)+"/"+dom+"-") {
		t.Errorf("dòng mới = %+v", nw)
	}
	data, _ := be.m.ReadFile(nw.Path)
	fi, _ := be.m.Stat(nw.Path)
	if bktSHA(data) != nw.SHA256 || fi.Size != nw.Size || fi.Mode.Perm() != 0o600 {
		t.Errorf("file %s: sha %s cỡ %d quyền %v; dòng %s %d", nw.Path, bktSHA(data), fi.Size, fi.Mode, nw.SHA256, nw.Size)
	}
	if !strings.Contains(string(data), "live "+dom) || !strings.Contains(string(data), "live-db "+dom) {
		t.Errorf("bản backup không có mã nguồn hoặc DB hiện tại:\n%s", data)
	}
}

// Site trong thùng rác: nguồn là thư mục trong thùng rác, không gọi wp (user
// của site không vào được thùng rác).
func TestBackupCreateTrashed(t *testing.T) {
	const dom = "shop.example.com"
	be := newBktEnv(t, dom)
	be.trash(t, dom)
	p, err := be.b.PrepareCreate(ctx, mustDomain(t, dom), ReasonBeforePurge)
	if err != nil {
		t.Fatal(err)
	}
	if a := p.Plan.Steps[0].Info(); !strings.Contains(strings.Join(a.Commands, "\n"), site.TrashDir(site.LayoutOf(mustDomain(t, dom)))) {
		t.Errorf("lệnh không đọc từ thùng rác: %v", a.Commands)
	}
	rep, err := p.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("chạy: %v %s (%s)", err, rep.Job.Status, rep.Job.Error)
	}
	list, _ := be.b.List(ctx, dom)
	if len(list) != 1 || list[0].Reason != ReasonBeforePurge || list[0].WPVersion != "" {
		t.Errorf("dòng = %+v", list)
	}
}

func TestBackupCreateRefused(t *testing.T) {
	const dom = "shop.example.com"
	be := newBktEnv(t, dom)
	d := mustDomain(t, dom)
	if _, err := be.b.PrepareCreate(ctx, mustDomain(t, "none.example.com"), ReasonManual); refusedCode(t, err) != "site_not_found" {
		t.Error("site không có: muốn site_not_found")
	}
	if _, err := be.b.PrepareCreate(ctx, d, ReasonBeforeRestore); err == nil {
		t.Error("lý do before-restore mà không lỗi")
	}
	// Đĩa 100 GiB còn 9 GiB: sau khi ghi còn dưới 10% thì từ chối, kèm số đo.
	be.m.SetDisk("/", system.DiskUsage{Total: 100 << 30, Free: 9 << 30})
	be.m.ResetRan()
	_, err := be.b.PrepareCreate(ctx, d, ReasonManual)
	if refusedCode(t, err) != CodeDiskSpaceLow {
		t.Fatalf("đĩa gần đầy: %v", err)
	}
	if msg := err.(*RefusedError).Message; !strings.Contains(msg.Vi, "9.0 GiB") || !strings.Contains(msg.Vi, "100.0 GiB") || !strings.Contains(msg.En, "df -h") {
		t.Errorf("lời từ chối thiếu số đo: %+v", msg)
	}
	for _, c := range mutatingRan(be.m.Ran()) {
		t.Errorf("bị từ chối mà vẫn chạy: %s", c)
	}
}

// ================================================================ Prune

func TestBackupPrune(t *testing.T) {
	const dom = "shop.example.com"
	be := newBktEnv(t, dom)
	l := site.LayoutOf(mustDomain(t, dom))
	mustSet(t, be.st, SettingBackupKeep, "2")
	f1 := be.seedFull(t, dom, "20261101-000000", "1", "1", ReasonManual)
	f2 := be.seedFull(t, dom, "20261102-000000", "2", "2", ReasonManual)
	f3 := be.seedFull(t, dom, "20261103-000000", "3", "3", ReasonManual)
	f4 := be.seedFull(t, dom, "20261104-000000", "4", "4", ReasonManual)
	pg := be.seedFull(t, dom, "20261001-000000", "p", "p", ReasonBeforePurge)
	s1 := be.seedFile(t, l, dom+"-db-20261101-000000.sql", []byte("1"), BackupSQL, ReasonManual, "20261101-000000")
	s2 := be.seedFile(t, l, dom+"-db-20261102-000000.sql", []byte("2"), BackupSQL, ReasonManual, "20261102-000000")
	s3 := be.seedFile(t, l, dom+"-db-20261103-000000.sql", []byte("3"), BackupSQL, ReasonBeforeImport, "20261103-000000")
	// File của f2 đã mất trên máy: vẫn xoá dòng.
	if err := be.m.Remove(f2.Path); err != nil {
		t.Fatal(err)
	}
	if err := be.b.prune(ctx, dom, f1.ID); err != nil {
		t.Fatal(err)
	}
	list, _ := be.b.List(ctx, dom)
	var ids []string
	for _, r := range list {
		ids = append(ids, r.ID)
	}
	// Mới nhất trước; cùng ngày thì dòng chèn sau đứng trước.
	want := []string{f4.ID, s3.ID, f3.ID, s2.ID, f1.ID, pg.ID}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("còn %v, muốn %v", ids, want)
	}
	if be.exists(s1.Path) || !be.exists(f1.Path) || !be.exists(pg.Path) {
		t.Errorf("file: sql cũ còn=%v, bản được giữ còn=%v, bản trước xoá hẳn còn=%v", be.exists(s1.Path), be.exists(f1.Path), be.exists(pg.Path))
	}

	// Dòng trỏ ra ngoài BackupRoot (sửa tay trong DB): không xoá file, giữ dòng, báo lỗi.
	be.m.SetFile("/etc/passwd", []byte("root:x:0:0"), 0o644, system.Root)
	bad, err := be.bs.AddBackup(ctx, Backup{Domain: dom, Kind: BackupFull, Reason: ReasonManual, Path: "/etc/passwd", SHA256: "x", Created: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{site.BackupRoot + "/../../../etc/shadow"} {
		if _, err := be.bs.AddBackup(ctx, Backup{Domain: dom, Kind: BackupFull, Reason: ReasonManual, Path: p, SHA256: "y", Created: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := be.b.prune(ctx, dom); err == nil || !strings.Contains(err.Error(), "không nằm dưới") {
		t.Errorf("đường dẫn ngoài BackupRoot: %v", err)
	}
	if !be.exists("/etc/passwd") {
		t.Error("prune xoá /etc/passwd")
	}
	if _, ok, _ := be.bs.Backup(ctx, bad.ID); !ok {
		t.Error("prune xoá dòng mà không xoá được file")
	}
}

// ================================================================ Restore

func restoreStepIDs(dom string, l site.Layout) []string {
	rd := site.RestoreDir(l)
	return []string{
		"backup.create:" + dom,
		"backup.extract:" + dom,
		"dir.move:" + l.Public,
		"dir.move:" + rd + "/public",
		"mariadb.dump:" + rd + "/db-before.sql",
		"db.import:" + dom,
	}
}

func TestBackupRestore(t *testing.T) {
	const dom = "shop.example.com"
	be := newBktEnv(t, dom, "blog.example.com")
	d := mustDomain(t, dom)
	l := site.LayoutOf(d)
	rd := site.RestoreDir(l)
	src := be.seedFull(t, dom, "20261101-000000", "từ backup", "db từ backup", ReasonManual)
	newer := be.seedFull(t, dom, "20261102-000000", "mới hơn", "db mới hơn", ReasonManual)
	liveCfg := be.read(t, l.WPConfig)
	// Giữ 1: sau khi dọn còn bản tự backup (mới nhất) và bản vừa dùng để khôi
	// phục (dù cũ hơn), bản newer phải đi.
	mustSet(t, be.st, SettingBackupKeep, "1")

	p, err := be.b.PrepareRestore(ctx, d, src.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Plan.Kind != "backup.restore" || p.Plan.Target != dom || len(p.Plan.Verify) == 0 {
		t.Errorf("Plan = %s/%s, %d phép kiểm", p.Plan.Kind, p.Plan.Target, len(p.Plan.Verify))
	}
	if got := stepIDs(p.Plan); !reflect.DeepEqual(got, restoreStepIDs(dom, l)) {
		t.Errorf("thứ tự bước:\n got %v\nwant %v", got, restoreStepIDs(dom, l))
	}
	// Xem trước khi RestoreDir chưa có: dựng được, không bước nào lỗi Check,
	// không lệnh nào đổi máy.
	be.m.ResetRan()
	pv, err := p.Preview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	danger := false
	for _, sp := range pv.Steps {
		if sp.CheckError != "" {
			t.Errorf("xem trước: bước %s lỗi Check: %s", sp.Info.ID, sp.CheckError)
		}
		// l.Public (đích của bước tráo thứ hai) có sẵn lúc xem trước; bước
		// đó vẫn phải hiện là sẽ chạy, không phải "đã xong".
		if sp.Done {
			t.Errorf("xem trước: bước %s hiện là đã xong", sp.Info.ID)
		}
		danger = danger || sp.Info.Danger
	}
	if !danger {
		t.Error("khôi phục mà không bước nào Danger: CLI sẽ không hỏi lại")
	}
	for _, c := range mutatingRan(be.m.Ran()) {
		t.Errorf("xem trước chạy lệnh thay đổi máy: %s", c)
	}
	if be.exists(rd) {
		t.Error("xem trước tạo thư mục khôi phục")
	}

	rep, err := p.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("chạy: %v %s (%s)%s", err, rep.Job.Status, rep.Job.Error, failedChecks(rep.Checks))
	}
	if got := be.read(t, l.Public+"/index.php"); got != "từ backup" {
		t.Errorf("index.php = %q, muốn nội dung của bản backup", got)
	}
	if got := be.dbBody(l); got != "db từ backup" {
		t.Errorf("DB = %q, muốn nội dung của bản backup", got)
	}
	// wp-config.php: bản đang chạy (mật khẩu DB hiện tại), 600.
	fi, _ := be.m.Stat(l.WPConfig)
	if be.read(t, l.WPConfig) != liveCfg || fi.Mode.Perm() != 0o600 {
		t.Errorf("wp-config.php sau khôi phục: %v\n%s", fi.Mode, be.read(t, l.WPConfig))
	}
	if be.exists(rd) {
		t.Errorf("%s còn sau khi job thành công", rd)
	}
	list, _ := be.b.List(ctx, dom)
	if len(list) != 2 || list[1].ID != src.ID {
		t.Fatalf("bảng backups = %+v; muốn bản tự backup rồi %s", list, src.ID)
	}
	if be.exists(newer.Path) || !be.exists(src.Path) {
		t.Errorf("dọn bản cũ: newer còn=%v, bản nguồn còn=%v", be.exists(newer.Path), be.exists(src.Path))
	}
	pre := list[0]
	if pre.Reason != ReasonBeforeRestore || pre.JobID != rep.Job.ID || !be.exists(pre.Path) || pre.ID == src.ID {
		t.Errorf("bản tự backup = %+v", pre)
	}
	data, _ := be.m.ReadFile(pre.Path)
	if !strings.Contains(string(data), "live "+dom) || !strings.Contains(string(data), "live-db "+dom) {
		t.Errorf("bản tự backup không có hiện trạng trước khi khôi phục:\n%s", data)
	}

	// id rỗng: bản đầy đủ mới nhất có lý do manual hoặc schedule (M18, S21),
	// tức là bản nguồn, không phải bản tự backup before-restore mới hơn.
	p, err = be.b.PrepareRestore(ctx, d, "")
	if err != nil {
		t.Fatal(err)
	}
	if ex := p.Plan.Steps[1].(*steps.BackupExtract); ex.Archive != src.Path || ex.SHA256 != src.SHA256 {
		t.Errorf("id rỗng chọn %s, muốn %s", ex.Archive, src.Path)
	}
}

func TestBackupRestoreRefused(t *testing.T) {
	const dom = "shop.example.com"
	be := newBktEnv(t, dom, "blog.example.com")
	d := mustDomain(t, dom)
	l := site.LayoutOf(d)
	if _, err := be.b.PrepareRestore(ctx, d, ""); refusedCode(t, err) != "backup_not_found" {
		t.Errorf("chưa có backup: %v", err)
	}
	src := be.seedFull(t, dom, "20261101-000000", "x", "x", ReasonManual)
	otherRow := be.seedFull(t, "blog.example.com", "20261101-000000", "o", "o", ReasonManual)
	sqlRow := be.seedFile(t, l, dom+"-db-20261101-000000.sql", []byte(bktDump("s")), BackupSQL, ReasonManual, "20261101-000000")
	for _, id := range []string{"ffffffffffffffff", otherRow.ID, sqlRow.ID, "$(id)", "../../etc/passwd", "a\nb"} {
		if _, err := be.b.PrepareRestore(ctx, d, id); refusedCode(t, err) != "backup_not_found" {
			t.Errorf("id %q: %v", id, err)
		}
	}
	if _, err := be.b.PrepareRestore(ctx, mustDomain(t, "none.example.com"), src.ID); refusedCode(t, err) != "site_not_found" {
		t.Errorf("site không có: %v", err)
	}

	// File bị sửa sau lúc tạo, rồi mất hẳn: backup_corrupt.
	be.m.SetFile(src.Path, []byte("hỏng"), 0o600, system.Root)
	if _, err := be.b.PrepareRestore(ctx, d, src.ID); refusedCode(t, err) != "backup_corrupt" {
		t.Errorf("sha256 lệch: %v", err)
	}
	if err := be.m.Remove(src.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := be.b.PrepareRestore(ctx, d, src.ID); refusedCode(t, err) != "backup_corrupt" {
		t.Errorf("file mất: %v", err)
	}

	// Thư mục khôi phục còn sót: từ chối, chỉ lệnh rm cụ thể, không tự xoá.
	good := be.seedFull(t, dom, "20261102-000000", "y", "y", ReasonManual)
	rd := site.RestoreDir(l)
	be.m.SetDir(rd+"/old-public", 0o700, system.Root)
	_, err := be.b.PrepareRestore(ctx, d, good.ID)
	if refusedCode(t, err) != "restore_dir_exists" || !strings.Contains(err.(*RefusedError).Message.Vi, "sudo rm -r --one-file-system -- "+rd) {
		t.Errorf("thư mục còn sót: %v", err)
	}
	if !be.exists(rd + "/old-public") {
		t.Error("PrepareRestore xoá thư mục còn sót")
	}
	be.m.removeUnder(site.RestoreRoot, true)

	// Không đủ chỗ cho bản tự backup trước khi khôi phục.
	be.m.SetDisk("/", system.DiskUsage{Total: 100 << 30, Free: 9 << 30})
	if _, err := be.b.PrepareRestore(ctx, d, good.ID); refusedCode(t, err) != CodeDiskSpaceLow {
		t.Errorf("đĩa gần đầy: %v", err)
	}
	be.m.SetDisk("/", system.DiskUsage{Total: 100 << 30, Free: 60 << 30})

	// Site trong thùng rác: khôi phục site trước.
	be.trash(t, dom)
	_, err = be.b.PrepareRestore(ctx, d, good.ID)
	if refusedCode(t, err) != "site_trashed" || !strings.Contains(err.(*RefusedError).Message.Vi, "tpan site restore "+dom) {
		t.Errorf("site trong thùng rác: %v", err)
	}
}

// Nhập DB lỗi giữa chừng: engine hoàn tác mọi bước, site về đúng hiện trạng,
// bảng backups không có dòng mới, bản tự backup bị xoá (không thành file mồ côi).
func TestBackupRestoreFailUndo(t *testing.T) {
	const dom = "shop.example.com"
	be := newBktEnv(t, dom)
	d := mustDomain(t, dom)
	l := site.LayoutOf(d)
	src := be.seedFull(t, dom, "20261101-000000", "từ backup", "db từ backup", ReasonManual)
	be.importFail = "/db.sql"
	p, err := be.b.PrepareRestore(ctx, d, src.ID)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := p.Execute(ctx, nil)
	if err != nil || rep.Job.Status == engine.JobSucceeded {
		t.Fatalf("chạy: %v %s, muốn job lỗi", err, rep.Job.Status)
	}
	if got := be.read(t, l.Public+"/index.php"); got != "live "+dom {
		t.Errorf("index.php = %q sau hoàn tác", got)
	}
	if got := be.dbBody(l); got != "live-db "+dom {
		t.Errorf("DB = %q sau hoàn tác", got)
	}
	if be.exists(site.RestoreDir(l)) {
		t.Error("thư mục khôi phục còn sau hoàn tác")
	}
	list, _ := be.b.List(ctx, dom)
	if len(list) != 1 || list[0].ID != src.ID {
		t.Errorf("bảng backups = %+v, muốn chỉ bản nguồn", list)
	}
	names, _ := be.m.ReadDir(site.BackupDir(l))
	if len(names) != 1 {
		t.Errorf("thư mục backup có %v, muốn chỉ bản nguồn", names)
	}
}

// ================================================================ tpan.db, CreateAll

func TestSnapshotTPANDB(t *testing.T) {
	be := newBktEnv(t)
	var paths []string
	for i := range 5 {
		at := bktNow.Add(time.Duration(i) * time.Hour)
		be.b.Now = func() time.Time { return at }
		p, err := be.b.SnapshotTPANDB(ctx)
		if err != nil {
			t.Fatalf("lần %d: %v", i, err)
		}
		paths = append(paths, p)
		if i == 0 {
			// File lạ trong thư mục: không đúng mẫu tên nên không bao giờ bị dọn.
			be.m.SetFile(site.TPANDBBackupDir+"/ghi-chu.txt", []byte("của người vận hành"), 0o600, system.Root)
		}
	}
	if paths[0] != site.TPANDBBackupDir+"/tpan-20261120-080000.db" {
		t.Errorf("tên = %s", paths[0])
	}
	names, _ := be.m.ReadDir(site.TPANDBBackupDir)
	want := []string{"ghi-chu.txt", "tpan-20261120-100000.db", "tpan-20261120-110000.db", "tpan-20261120-120000.db"}
	slices.Sort(names)
	if !reflect.DeepEqual(names, want) {
		t.Errorf("còn %v, muốn %v", names, want)
	}
	for _, d := range []string{site.BackupRoot, site.TPANDBBackupDir} {
		if fi, err := be.m.Stat(d); err != nil || fi.Mode.Perm() != 0o700 || fi.Owner != system.Root {
			t.Errorf("%s: %v %v", d, fi.Mode, err)
		}
	}
	// Cùng một giây: lỗi, không ghi đè.
	if _, err := be.b.SnapshotTPANDB(ctx); err == nil {
		t.Error("chép hai lần trong cùng giây mà không lỗi")
	}
	be.b.Store = nil
	if _, err := be.b.SnapshotTPANDB(ctx); err == nil {
		t.Error("không có store mà không lỗi")
	}
}

func TestBackupCreateAll(t *testing.T) {
	be := newBktEnv(t, "a.example.com", "b.example.com", "c.example.com", "t.example.com")
	be.trash(t, "t.example.com")
	be.duFail = "/var/www/b.example.com"
	res, err := be.b.CreateAll(ctx, ReasonSchedule, nil)
	if err != nil {
		t.Fatal(err)
	}
	var doms []string
	for _, r := range res {
		doms = append(doms, r.Domain)
	}
	if !reflect.DeepEqual(doms, []string{"a.example.com", "b.example.com", "c.example.com"}) {
		t.Fatalf("site đã chạy: %v (site trong thùng rác không được backup)", doms)
	}
	for _, i := range []int{0, 2} {
		if res[i].Error != "" || res[i].Report.Job.Status != engine.JobSucceeded {
			t.Errorf("%s: %s %s", res[i].Domain, res[i].Error, res[i].Report.Job.Status)
		}
		list, _ := be.b.List(ctx, res[i].Domain)
		if len(list) != 1 || list[0].Reason != ReasonSchedule {
			t.Errorf("%s: dòng %+v", res[i].Domain, list)
		}
	}
	if res[1].Error == "" || res[1].Report.Job.ID != "" {
		t.Errorf("site lỗi đo: %+v", res[1])
	}
	if !be.exists(site.TPANDBBackupDir + "/tpan-20261120-080000.db") {
		t.Error("chưa chép tpan.db")
	}
	if _, err := be.b.CreateAll(ctx, ReasonBeforeRestore, nil); err == nil {
		t.Error("lý do before-restore mà không lỗi")
	}

	// Không có site nào: vẫn chép tpan.db.
	be2 := newBktEnv(t)
	res, err = be2.b.CreateAll(ctx, ReasonManual, nil)
	if err != nil || len(res) != 0 || res == nil || !be2.exists(site.TPANDBBackupDir+"/tpan-20261120-080000.db") {
		t.Errorf("không site: %v %v", res, err)
	}
}

func TestBackupList(t *testing.T) {
	be := newBktEnv(t, "shop.example.com")
	be.seedFull(t, "shop.example.com", "20261101-000000", "x", "x", ReasonManual)
	// Dòng của site đã xoá hẳn vẫn hiện khi liệt kê mọi domain.
	gone := be.seedFull(t, "gone.example.com", "20261102-000000", "g", "g", ReasonBeforePurge)
	all, err := be.b.List(ctx, "")
	if err != nil || len(all) != 2 || all[0].ID != gone.ID {
		t.Errorf("mọi domain: %+v %v", all, err)
	}
	if one, _ := be.b.List(ctx, "shop.example.com"); len(one) != 1 {
		t.Errorf("một domain: %+v", one)
	}
	be.b.Store = nil
	if none, err := be.b.List(ctx, ""); err != nil || none == nil || len(none) != 0 {
		t.Errorf("không có store: %v %v", none, err)
	}
}

// ================================================================ M13: hạn mức backup

// bktOwn cho site dom thuộc bob, gói có max_backups = most.
func (be *bktEnv) bktOwn(t *testing.T, dom string, most int) *usrStore {
	t.Helper()
	as := newUsrStore(be.sites, nil)
	as.pkgs["b"] = account.Package{Name: "b", MaxBackups: most}
	as.put(account.User{Name: account.AdminName, Role: account.RoleAdmin})
	as.put(account.User{Name: "bob", Role: account.RoleUser, Package: "b"})
	st, _, _ := be.sites.Site(ctx, mustDomain(t, dom))
	st.Owner = "bob"
	if err := be.sites.SaveSite(ctx, st); err != nil {
		t.Fatal(err)
	}
	be.s.Accounts = as
	return as
}

func TestBackupQuotaM13(t *testing.T) {
	const dom = "shop.example.com"
	be := newBktEnv(t, dom)
	d := mustDomain(t, dom)
	be.bktOwn(t, dom, 2)
	be.seedFull(t, dom, "20261101-000000", "a", "a", ReasonManual)
	be.seedFull(t, dom, "20261001-000000", "p", "p", ReasonBeforePurge) // không tính
	if _, err := be.b.PrepareCreate(ctx, d, ReasonManual); err != nil {
		t.Fatalf("1/2 bản: %v", err)
	}
	be.seedFull(t, dom, "20261102-000000", "b", "b", ReasonSchedule)
	if _, err := be.b.PrepareCreate(ctx, d, ReasonManual); refusedCode(t, err) != CodeQuotaBackups {
		t.Errorf("2/2 bản, bấm tay: %v", err)
	}
	// Lịch và lưới an toàn của mục 6.5 không bao giờ bị chặn.
	for _, r := range []BackupReason{ReasonSchedule, ReasonBeforePurge} {
		if _, err := be.b.PrepareCreate(ctx, d, r); err != nil {
			t.Errorf("%s bị chặn: %v", r, err)
		}
	}
}

func TestBackupPruneByPackageM13(t *testing.T) {
	const dom = "shop.example.com"
	be := newBktEnv(t, dom)
	be.bktOwn(t, dom, 2) // Keep mặc định 3, gói cho 2
	l := site.LayoutOf(mustDomain(t, dom))
	f1 := be.seedFull(t, dom, "20261101-000000", "1", "1", ReasonManual)
	f2 := be.seedFull(t, dom, "20261102-000000", "2", "2", ReasonSchedule)
	f3 := be.seedFull(t, dom, "20261103-000000", "3", "3", ReasonSchedule)
	pg := be.seedFull(t, dom, "20261001-000000", "p", "p", ReasonBeforePurge)
	s1 := be.seedFile(t, l, dom+"-db-20261101-000000.sql", []byte("1"), BackupSQL, ReasonManual, "20261101-000000")
	s2 := be.seedFile(t, l, dom+"-db-20261102-000000.sql", []byte("2"), BackupSQL, ReasonManual, "20261102-000000")
	s3 := be.seedFile(t, l, dom+"-db-20261103-000000.sql", []byte("3"), BackupSQL, ReasonManual, "20261103-000000")
	if err := be.b.prune(ctx, dom); err != nil {
		t.Fatal(err)
	}
	list, _ := be.b.List(ctx, dom)
	var ids []string
	for _, r := range list {
		ids = append(ids, r.ID)
	}
	for _, keep := range []string{f2.ID, f3.ID, pg.ID, s1.ID, s2.ID, s3.ID} {
		if !slices.Contains(ids, keep) {
			t.Errorf("mất %s; còn %v", keep, ids)
		}
	}
	if slices.Contains(ids, f1.ID) || be.exists(f1.Path) {
		t.Errorf("bản full cũ nhất phải đi (giữ min(3, 2) = 2): %v", ids)
	}
}

func TestBackupRestoreSuspendedM13(t *testing.T) {
	const dom = "shop.example.com"
	be := newBktEnv(t, dom)
	d := mustDomain(t, dom)
	src := be.seedFull(t, dom, "20261101-000000", "x", "x", ReasonManual)
	st, _, _ := be.sites.Site(ctx, d)
	st.Suspended = account.SuspendByUser
	_ = be.sites.SaveSite(ctx, st)
	if _, err := be.b.PrepareRestore(ctx, d, src.ID); refusedCode(t, err) != CodeSiteSuspended {
		t.Errorf("khôi phục site đang khoá: %v", err)
	}
}
