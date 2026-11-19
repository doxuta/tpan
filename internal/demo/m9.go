package demo

// Phần M9 của máy giả (backup mục 6.5, thao tác DB mục 6.9, cron mục 6.10 của
// docs/KE-HOACH.md): các lệnh mà bước backup, khôi phục, đổi mật khẩu, xuất
// và nhập DB gọi, và dữ liệu mẫu cho màn Backup và màn Cron.
//
// seedM9 được gọi TRƯỚC khi tạo các site mẫu (demo.go), vì hai lẽ:
//   - wp-config.php của site phải có các dòng DB_NAME, DB_USER, DB_PASSWORD
//     như file `wp config create` thật ghi: đổi mật khẩu DB đọc mật khẩu cũ
//     từ đó, khôi phục backup chép wp-config.php đang chạy vào bản khôi phục;
//   - `wp config get/set` của hằng (DISABLE_WP_CRON) phải trả lời đúng ngay
//     từ lúc tạo site, khi Plan tạo site có các bước cron.
//
// Vì vậy dòng backup và lần chạy cron mẫu được ghi thẳng vào store chứ không
// chạy job: site chưa có lúc đó, và bảng backups, cron_runs không có khoá
// ngoại tới sites. Job backup thật thì đặt tên file theo giờ tới từng giây,
// nên nhiều bản của cùng một site trong một lần seed sẽ trùng tên.
//
// File backup trên máy giả không phải tar.gz thật: là một dòng đánh dấu rồi
// JSON (manifest.json và db.sql). `tar --create/--list/--extract` dưới đây đọc
// ghi đúng dạng đó, và sha256sum tính trên đúng các byte đó, nên kiểm sha256
// lúc khôi phục và nhập DB chạy như trên máy thật.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io/fs"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
)

const (
	// sitePurged là site đã xoá hẳn: không còn trong bảng sites, chỉ còn bản
	// tự backup trước khi xoá hẳn trên màn Backup.
	sitePurged = "legacy.example.org"
	// demoBackupAt là giờ backup hằng ngày của máy demo.
	demoBackupAt = "02:30"
	// demoWPVersion khớp version.php mà tar giả của WordPress dựng (machine.go).
	demoWPVersion = "7.1.5"
	// demoArchiveMark là dòng đầu của file backup giả.
	demoArchiveMark = "tpan-demo-backup\n"
)

var (
	// Câu đo cỡ DB của steps (measureDBSQL).
	sqlTableSize = regexp.MustCompile(`FROM information_schema\.TABLES WHERE TABLE_SCHEMA='([a-z0-9_]+)'`)
	// Dòng define của wp-config.php giả (cùng dạng `wp config create` ghi).
	wpDefinePW = regexp.MustCompile(`define\( 'DB_PASSWORD', '[^']*' \);`)
)

// m9 giữ trạng thái của các handler M9.
type m9 struct {
	*machine
	mu sync.Mutex
	// consts: hằng đặt bằng `wp config set ... --type=constant`, theo user
	// của site, giá trị dạng raw (true, false, số).
	consts map[string]map[string]string
}

// seedM9 đăng ký lệnh giả của M9 rồi ghi dữ liệu mẫu (xem đầu file).
func seedM9(ctx context.Context, m *machine, st *store.Store) error {
	x := &m9{machine: m, consts: map[string]map[string]string{}}
	x.handlers()
	if err := x.seedSettings(ctx, st); err != nil {
		return err
	}
	if err := x.seedBackups(ctx, st); err != nil {
		return err
	}
	return x.seedCron(ctx, st)
}

func (x *m9) handlers() {
	// Đăng ký sau machine.handlers với cùng prefix: handler này thắng và
	// chuyển câu SQL khác về m.mariadb.
	x.Handle([]string{"mariadb", "-N", "-B"}, x.mariadb)
	x.Handle([]string{"du"}, func(c system.Cmd) system.Result {
		p := c.Args[len(c.Args)-1]
		return exit(0, fmt.Sprintf("%d\t%s\n", treeSize(p), p), "")
	})
	x.Handle([]string{"sha256sum"}, x.sha256sum)
	x.Handle([]string{"tar", "--create"}, x.tarCreate)
	x.Handle([]string{"tar", "--list"}, x.tarList)
	x.Handle([]string{"tar", "--extract"}, x.tarExtract)
	x.Handle([]string{"wp", "config", "create"}, x.wpConfigCreate)
	x.Handle([]string{"wp", "config", "get"}, x.wpConfigGet)
	x.Handle([]string{"wp", "config", "set"}, x.wpConfigSet)
	x.Handle([]string{"wp", "config", "delete"}, x.wpConfigDelete)
	x.Handle([]string{"wp", "core", "version"}, func(system.Cmd) system.Result {
		return exit(0, demoWPVersion+"\n", "")
	})
}

// treeSize cho cỡ giả của một cây mã nguồn: 30 tới dưới 60 MiB, cố định theo
// đường dẫn như dbSize.
func treeSize(p string) int64 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(p))
	return 30<<20 + int64(h.Sum32()%(30<<20))
}

// ---------------------------------------------------------------- MariaDB

// mariadb trả lời câu đo cỡ DB và câu xoá rồi tạo lại DB của bước nhập DB;
// câu khác chuyển cho machine.mariadb. Câu xoá rồi tạo lại phải xử lý ở đây:
// machine.mariadb xét CREATE trước DROP, nên cùng một câu sẽ làm mất DB.
func (x *m9) mariadb(c system.Cmd) system.Result {
	sql := string(c.Stdin)
	if g := sqlTableSize.FindStringSubmatch(sql); g != nil {
		x.machine.mu.Lock()
		has := x.dbs[g[1]]
		x.machine.mu.Unlock()
		n := int64(0)
		if has {
			n = dbSize(g[1])
		}
		return exit(0, fmt.Sprintf("%d\n", n), "")
	}
	drop, create := sqlDropDB.FindStringSubmatch(sql), sqlCreateDB.FindStringSubmatch(sql)
	if drop != nil && create != nil && drop[1] == create[1] {
		x.machine.mu.Lock()
		x.dbs[drop[1]] = true
		x.machine.mu.Unlock()
		return okResult
	}
	return x.machine.mariadb(c)
}

// sha256sum tính trên đúng byte của file giả.
func (x *m9) sha256sum(c system.Cmd) system.Result {
	p := c.Args[len(c.Args)-1]
	data, err := x.ReadFile(p)
	if err != nil {
		return exit(1, "", "sha256sum: "+p+": No such file or directory\n")
	}
	sum := sha256.Sum256(data)
	return exit(0, hex.EncodeToString(sum[:])+"  "+p+"\n", "")
}

// ---------------------------------------------------------------- tar

// demoArchive là nội dung file backup giả.
type demoArchive struct {
	Manifest string `json:"manifest"`
	DB       string `json:"db"`
}

func (a demoArchive) encode() []byte {
	data, _ := json.Marshal(a) // hai chuỗi: không bao giờ lỗi
	return append([]byte(demoArchiveMark), data...)
}

func (x *m9) readArchive(p string) (demoArchive, system.Result, bool) {
	data, err := x.ReadFile(p)
	if err != nil {
		return demoArchive{}, exit(2, "", "tar: "+p+": Cannot open: No such file or directory\ntar: Error is not recoverable: exiting now\n"), false
	}
	var a demoArchive
	body, ok := strings.CutPrefix(string(data), demoArchiveMark)
	if !ok || json.Unmarshal([]byte(body), &a) != nil {
		return demoArchive{}, exit(2, "", "gzip: stdin: not in gzip format\ntar: Child returned status 1\ntar: Error is not recoverable: exiting now\n"), false
	}
	return a, okResult, true
}

// tarCreate: `tar --create --gzip --file=<f> --one-file-system
// --directory=<tmp> manifest.json db.sql --directory=<src> public`. Chỉ lấy
// manifest.json và db.sql; cây public của file giả luôn là vài file cố định.
func (x *m9) tarCreate(c system.Cmd) system.Result {
	file := argPrefix(c.Args, "--file=")
	tmp := argPrefix(c.Args, "--directory=")
	man, err1 := x.ReadFile(tmp + "/manifest.json")
	db, err2 := x.ReadFile(tmp + "/db.sql")
	if file == "" || err1 != nil || err2 != nil {
		return exit(2, "", "tar: manifest.json: Cannot stat: No such file or directory\ntar: Exiting with failure status due to previous errors\n")
	}
	x.SetFile(file, demoArchive{Manifest: string(man), DB: string(db)}.encode(), 0o600, rootOwner)
	return okResult
}

// demoPublic là cây public trong mọi file backup giả.
var demoPublic = []string{"index.php", "wp-config.php", "wp-includes/", "wp-includes/version.php"}

// tarList in danh sách như `tar --list --verbose` của GNU tar 1.35.
func (x *m9) tarList(c system.Cmd) system.Result {
	a, res, ok := x.readArchive(argPrefix(c.Args, "--file="))
	if !ok {
		return res
	}
	var man struct {
		Created string `json:"created"`
	}
	_ = json.Unmarshal([]byte(a.Manifest), &man)
	when := "2026-11-19 10:00"
	if t, err := time.Parse(time.RFC3339, man.Created); err == nil {
		when = t.Format("2006-01-02 15:04")
	}
	var b strings.Builder
	line := func(mode string, size int, name string) {
		fmt.Fprintf(&b, "%s root/root %9d %s %s\n", mode, size, when, name)
	}
	line("-rw-------", len(a.Manifest), "manifest.json")
	line("-rw-------", len(a.DB), "db.sql")
	line("drwxr-s---", 0, "public/")
	for _, n := range demoPublic {
		if strings.HasSuffix(n, "/") {
			line("drwxr-s---", 0, "public/"+n)
		} else {
			line("-rw-r-----", 512, "public/"+n)
		}
	}
	return exit(0, b.String(), "")
}

// tarExtract: `tar --extract --gzip --file=<f> --directory=<dir> ...`. Dựng
// manifest.json, db.sql và cây public. Chủ và quyền của cây public đặt sẵn
// như sau bước chown và chmod của BackupExtract (hai lệnh đó trên máy giả
// không đổi gì), giống cây mà tar giả của WordPress dựng lúc tạo site.
func (x *m9) tarExtract(c system.Cmd) system.Result {
	a, res, ok := x.readArchive(argPrefix(c.Args, "--file="))
	if !ok {
		return res
	}
	dir := argPrefix(c.Args, "--directory=")
	var man struct {
		Domain string `json:"domain"`
	}
	_ = json.Unmarshal([]byte(a.Manifest), &man)
	l, ok := siteOf(man.Domain)
	if dir == "" || !ok {
		return exit(2, "", "tar: manifest.json lạ\n")
	}
	x.SetFile(dir+"/manifest.json", []byte(a.Manifest), 0o600, rootOwner)
	x.SetFile(dir+"/db.sql", []byte(a.DB), 0o600, rootOwner)
	pub := dir + "/public"
	// Thư mục public giống của site đang chạy (MoveTree tráo nguyên thư mục).
	if fi, err := x.Stat(l.Public); err == nil && fi.Mode.IsDir() {
		x.SetDir(pub, fi.Mode, fi.Owner)
	} else {
		x.SetDir(pub, 0o750|fs.ModeSetgid, system.Owner{User: l.User, Group: site.WebGroup})
	}
	own := system.Owner{User: l.User, Group: l.User}
	x.SetFile(pub+"/index.php", []byte("<?php\n"), 0o644, own)
	x.SetFile(pub+"/wp-includes/version.php", []byte("<?php $wp_version = '"+demoWPVersion+"';\n"), 0o644, own)
	x.SetFile(pub+"/wp-config.php", []byte("<?php // wp-config.php trong bản backup giả\n"), 0o600, own)
	return okResult
}

// ---------------------------------------------------------------- WP-CLI

// fakeDBPassword là giá trị DB_PASSWORD máy giả ghi vào wp-config.php thay
// cho mật khẩu nhận qua stdin: cùng dạng tpan sinh (32 chữ và số, đổi mật
// khẩu DB đòi đúng dạng đó), nhưng không phải chính mật khẩu, để phép kiểm
// "mật khẩu chỉ đi qua stdin" của demo_test vẫn soi được mọi file của máy giả.
// Máy giả không so mật khẩu với MariaDB nên giá trị nào cũng chạy.
func fakeDBPassword(stdin []byte) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(string(stdin))))
	return hex.EncodeToString(sum[:16])
}

// wpConfigCreate ghi wp-config.php có các dòng DB_* như `wp config create`
// thật (mật khẩu đến qua stdin, --prompt=dbpass; xem fakeDBPassword). Site
// tạo lại sau khi xoá hẳn bắt đầu không có hằng nào.
func (x *m9) wpConfigCreate(c system.Cmd) system.Result {
	u, pub := c.AsUser, argPrefix(c.Args, "--path=")
	pw := fakeDBPassword(c.Stdin)
	body := "<?php\n// wp-config giả của chế độ demo\n" +
		"define( 'DB_NAME', '" + argPrefix(c.Args, "--dbname=") + "' );\n" +
		"define( 'DB_USER', '" + argPrefix(c.Args, "--dbuser=") + "' );\n" +
		"define( 'DB_PASSWORD', '" + pw + "' );\n" +
		"define( 'DB_HOST', '" + argPrefix(c.Args, "--dbhost=") + "' );\n"
	x.SetFile(pub+"/wp-config.php", []byte(body), 0o644, system.Owner{User: u, Group: u})
	x.mu.Lock()
	delete(x.consts, u)
	x.mu.Unlock()
	return okResult
}

func wpNotDefined(name string) system.Result {
	return exit(1, "", "Error: The constant '"+name+"' is not defined in the 'wp-config.php' file.\n")
}

// wpConfigGet: `wp config get <NAME> --type=constant --format=json`.
func (x *m9) wpConfigGet(c system.Cmd) system.Result {
	if len(c.Args) < 3 {
		return exit(1, "", "usage: wp config get <name>\n")
	}
	x.mu.Lock()
	v, ok := x.consts[c.AsUser][c.Args[2]]
	x.mu.Unlock()
	if !ok {
		return wpNotDefined(c.Args[2])
	}
	return exit(0, v+"\n", "")
}

// wpConfigSet: `wp config set DB_PASSWORD --prompt ...` (mật khẩu qua stdin,
// sửa dòng define trong file) hoặc `wp config set <NAME> <giá trị> --raw ...`.
func (x *m9) wpConfigSet(c system.Cmd) system.Result {
	if len(c.Args) < 3 {
		return exit(1, "", "usage: wp config set <name> <value>\n")
	}
	name, u := c.Args[2], c.AsUser
	if name == "DB_PASSWORD" && slices.Contains(c.Args, "--prompt") {
		p := argPrefix(c.Args, "--path=") + "/wp-config.php"
		fi, err := x.Stat(p)
		data, rerr := x.ReadFile(p)
		if err != nil || rerr != nil || !wpDefinePW.Match(data) || len(strings.TrimSpace(string(c.Stdin))) == 0 {
			return exit(1, "", "Error: Could not process the 'wp-config.php' transformation.\n")
		}
		data = wpDefinePW.ReplaceAll(data, []byte("define( 'DB_PASSWORD', '"+fakeDBPassword(c.Stdin)+"' );"))
		x.SetFile(p, data, fi.Mode.Perm(), fi.Owner)
		return okResult
	}
	if len(c.Args) < 4 || strings.HasPrefix(c.Args[3], "--") {
		return exit(1, "", "usage: wp config set <name> <value>\n")
	}
	x.mu.Lock()
	if x.consts[u] == nil {
		x.consts[u] = map[string]string{}
	}
	x.consts[u][name] = c.Args[3]
	x.mu.Unlock()
	return okResult
}

// wpConfigDelete: `wp config delete <NAME> --type=constant`.
func (x *m9) wpConfigDelete(c system.Cmd) system.Result {
	if len(c.Args) < 3 {
		return exit(1, "", "usage: wp config delete <name>\n")
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	if _, ok := x.consts[c.AsUser][c.Args[2]]; !ok {
		return wpNotDefined(c.Args[2])
	}
	delete(x.consts[c.AsUser], c.Args[2])
	return okResult
}

// ---------------------------------------------------------------- dữ liệu mẫu

// seedSettings bật lịch backup hằng ngày, giữ 3 bản mỗi site, và đặt file
// cron của lịch đó như sau một lần `tpan backup settings --at 02:30` thật.
func (x *m9) seedSettings(ctx context.Context, st *store.Store) error {
	for k, v := range map[string]string{
		core.SettingBackupKeep: strconv.Itoa(core.DefaultBackupKeep),
		core.SettingBackupAt:   demoBackupAt,
	} {
		if err := st.SetSetting(ctx, k, v); err != nil {
			return fmt.Errorf("ghi setting %s: %w", k, err)
		}
	}
	data, err := templates.BackupCron(demoBackupAt)
	if err != nil {
		return fmt.Errorf("sinh cron của lịch backup: %w", err)
	}
	x.SetFile(site.BackupCronFile, data, 0o644, rootOwner)
	return nil
}

// demoDump là bản xuất DB giả, dòng cuối như mariadb-dump thật (MariaDBDump
// và DBImport đòi dòng đó).
func demoDump(db string, at time.Time) []byte {
	return []byte("-- MariaDB dump 10.19  Distrib 10.11.13-MariaDB, for debian-linux-gnu (x86_64)\n--\n-- Host: localhost    Database: " + db + "\n" +
		"CREATE TABLE `wp_options` (`option_id` bigint(20) unsigned NOT NULL AUTO_INCREMENT, PRIMARY KEY (`option_id`));\n" +
		"-- Dump completed on " + at.Format("2006-01-02 15:04:05") + "\n")
}

// seedBackups ghi file và dòng của các bản backup mẫu: vài bản theo lịch và
// bấm tay của blog và shop, một bản của site đang ở thùng rác (từ trước khi
// vào thùng rác), một bản tự backup trước khi xoá hẳn của site không còn.
func (x *m9) seedBackups(ctx context.Context, st *store.Store) error {
	rows := []struct {
		dom    string
		kind   core.BackupKind
		reason core.BackupReason
		ago    time.Duration
	}{
		{sitePurged, core.BackupFull, core.ReasonBeforePurge, 12 * 24 * time.Hour},
		{siteOld, core.BackupFull, core.ReasonSchedule, 4*24*time.Hour + 5*time.Hour},
		{siteShop, core.BackupSQL, core.ReasonManual, 4*24*time.Hour + 2*time.Hour},
		{siteBlog, core.BackupFull, core.ReasonSchedule, 2*24*time.Hour + 5*time.Hour},
		{siteBlog, core.BackupFull, core.ReasonSchedule, 24*time.Hour + 5*time.Hour},
		{siteShop, core.BackupFull, core.ReasonSchedule, 24*time.Hour + 5*time.Hour},
		{siteBlog, core.BackupSQL, core.ReasonManual, 7 * time.Hour},
		{siteBlog, core.BackupFull, core.ReasonManual, 3 * time.Hour},
	}
	if err := x.MkdirAll(site.BackupRoot, 0o700, rootOwner); err != nil {
		return fmt.Errorf("tạo %s: %w", site.BackupRoot, err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	for _, r := range rows {
		l, err := layoutOf(r.dom)
		if err != nil {
			return err
		}
		at := now.Add(-r.ago)
		dir := site.BackupDir(l)
		if err := x.MkdirAll(dir, 0o700, rootOwner); err != nil {
			return fmt.Errorf("tạo %s: %w", dir, err)
		}
		dump := demoDump(l.DB, at)
		b := core.Backup{Domain: r.dom, Kind: r.kind, Reason: r.reason, Created: at}
		var data []byte
		if r.kind == core.BackupSQL {
			b.Path = dir + "/" + r.dom + "-db-" + at.Format("20060102-150405") + ".sql"
			data = dump
			b.Size = dbSize(l.DB)
		} else {
			dbSum := sha256.Sum256(dump)
			man, err := json.MarshalIndent(map[string]string{
				"domain": r.dom, "wp_version": demoWPVersion, "php_version": phpVer,
				"created": at.Format(time.RFC3339), "db_sha256": hex.EncodeToString(dbSum[:]),
			}, "", "  ")
			if err != nil {
				return fmt.Errorf("dựng manifest giả của %s: %w", r.dom, err)
			}
			b.Path = dir + "/" + r.dom + "-" + at.Format("20060102-150405") + ".tar.gz"
			data = demoArchive{Manifest: string(man) + "\n", DB: string(dump)}.encode()
			b.WPVersion, b.PHPVersion = demoWPVersion, phpVer
			// Cỡ như một bản thật (nén còn khoảng một phần ba); file giả nhỏ
			// hơn nhiều, nhưng không gì trong tpan so cỡ với file.
			b.Size = (treeSize(l.Public) + dbSize(l.DB)) / 3
		}
		x.SetFile(b.Path, data, 0o600, rootOwner)
		sum := sha256.Sum256(data)
		b.SHA256 = hex.EncodeToString(sum[:])
		if _, err := st.AddBackup(ctx, b); err != nil {
			return fmt.Errorf("ghi backup mẫu %s: %w", b.Path, err)
		}
	}
	return nil
}

// seedCron đặt file cron của các site sẽ active (site trong thùng rác không
// có: file bị gỡ khi vào thùng rác) và lần chạy gần nhất: blog, shop chạy
// được, wiki lỗi vì một plugin.
func (x *m9) seedCron(ctx context.Context, st *store.Store) error {
	now := time.Now().UTC()
	for _, r := range []struct {
		dom    string
		status core.CronStatus
		code   int
		took   time.Duration
		out    string
	}{
		{siteBlog, core.CronOK, 0, 1400 * time.Millisecond, ""},
		{siteShop, core.CronOK, 0, 900 * time.Millisecond, ""},
		{siteWiki, core.CronFail, 255, 3100 * time.Millisecond, ""},
	} {
		l, err := layoutOf(r.dom)
		if err != nil {
			return err
		}
		data, err := templates.SiteCron(l)
		if err != nil {
			return fmt.Errorf("sinh cron của %s: %w", r.dom, err)
		}
		x.SetFile(site.CronFile(l), data, 0o644, rootOwner)
		out := r.out
		if r.status == core.CronFail {
			plugin := l.Public + "/wp-content/plugins/wiki-sync/cron.php"
			out = "PHP Fatal error:  Uncaught Error: Call to undefined function wiki_sync_pages() in " + plugin + ":42\n" +
				"Stack trace:\n#0 " + l.Public + "/wp-includes/class-wp-hook.php(324): wiki_sync_run()\n#1 {main}\n  thrown in " + plugin + " on line 42"
		}
		if err := st.SaveCronRun(ctx, core.CronRun{
			Domain: r.dom, Started: now.Add(-2*time.Minute - r.took), Duration: r.took,
			Status: r.status, ExitCode: r.code, Output: out,
		}); err != nil {
			return fmt.Errorf("ghi lần chạy cron mẫu của %s: %w", r.dom, err)
		}
	}
	return nil
}
