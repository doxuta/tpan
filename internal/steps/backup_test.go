package steps

import (
	"context"
	"encoding/json"
	"errors"
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

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

// ================================================================= máy giả M9

// bkMember là một thành viên trong "file tar" giả: file tar giả là JSON của
// []bkMember, đủ để tar --create, --list --verbose, --extract giả đi vòng
// tròn với nhau và để test dựng tay các file backup độc.
type bkMember struct {
	Name   string `json:"name"`
	Type   string `json:"type"` // "-", "d", "l", "h", "c", "p"
	Data   string `json:"data,omitempty"`
	Target string `json:"target,omitempty"`
}

// bkFake là FakeSystem cộng các lệnh M9 có trạng thái: một DB giả (chuỗi),
// mariadb-dump ghi DB đó ra file, mariadb source đọc file vào DB, tar giả,
// sha256sum, du, wp, chown -R, find -exec chmod, rm -r.
type bkFake struct {
	*system.FakeSystem
	l site.Layout

	mu         sync.Mutex
	db         string // nội dung DB của site
	dbSize     string // output câu đo cỡ DB
	du         string // byte du trả về
	wp         string // output `wp core version`
	tarCode    int    // mã thoát của tar --create
	importFail string // file mà `source` lỗi giữa chừng
}

const bkTime = "2026-11-19 10:00"

func bkWPConfig(db, pw string) string {
	return "<?php\ndefine( 'DB_NAME', '" + db + "' );\ndefine( 'DB_USER', '" + db + "' );\ndefine( 'DB_PASSWORD', '" + pw + "' );\n"
}

func bkDumpOf(body string) string {
	return "-- MariaDB dump\nBODY:" + body + "\n-- Dump completed on 2026-11-19 10:00:00\n"
}

func newBkFake(t *testing.T) *bkFake {
	t.Helper()
	l := dbLayout(t)
	f := &bkFake{FakeSystem: system.NewFake(), l: l, db: "v1", dbSize: "1000", du: "5000", wp: "6.7.1"}
	own := system.Owner{User: l.User, Group: site.WebGroup}
	f.SetDir("/var/www", 0o755, system.Root)
	f.SetDir(l.Dir, fs.ModeSetgid|0o750, own)
	f.SetDir(l.Public, fs.ModeSetgid|0o750, own)
	f.SetDir(l.Public+"/wp-content", fs.ModeSetgid|0o750, own)
	f.SetFile(l.Public+"/index.php", []byte("<?php // v1"), 0o640, own)
	f.SetFile(l.Public+"/wp-content/a.jpg", []byte("jpg"), 0o640, own)
	f.SetSymlink("index.php", l.Public+"/link.php")
	f.SetFile(l.WPConfig, []byte(bkWPConfig(l.DB, "passold")), 0o600, system.Owner{User: l.User, Group: l.User})
	f.SetDir("/var/lib/tpan", 0o755, system.Root)

	ok := system.Result{}
	exit := func(code int, stdout, stderr string) system.Result {
		return system.Result{ExitCode: code, Stdout: stdout, Stderr: stderr}
	}
	under := func(root string) []string {
		var out []string
		for p := range f.Snapshot() {
			if p == root || strings.HasPrefix(p, root+"/") {
				out = append(out, p)
			}
		}
		sort.Strings(out)
		return out
	}
	// keepPerm: như fopen("w") của chương trình thật, file có sẵn giữ quyền và chủ.
	write := func(p, data string) {
		perm, owner := fs.FileMode(0o644), system.Root
		if fi, err := f.Stat(p); err == nil {
			perm, owner = fi.Mode.Perm(), fi.Owner
		}
		f.SetFile(p, []byte(data), perm, owner)
	}

	f.Handle([]string{"mariadb", "-N", "-B"}, func(c system.Cmd) system.Result {
		f.mu.Lock()
		defer f.mu.Unlock()
		sql := string(c.Stdin)
		switch {
		case strings.Contains(sql, "information_schema.TABLES"):
			return exit(0, f.dbSize+"\n", "")
		case strings.Contains(sql, "DROP DATABASE"):
			f.db = ""
		}
		return ok
	})
	f.Handle([]string{"mariadb"}, func(c system.Cmd) system.Result {
		src, _ := strings.CutPrefix(argAfter(c.Args, "-e"), "source ")
		data, err := f.ReadFile(src)
		if err != nil {
			return exit(1, "", "ERROR: Failed to open file '"+src+"'")
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if src == f.importFail {
			f.db = "half"
			return exit(1, "", "ERROR 1064 (42000) at line 2 in file: '"+src+"'")
		}
		for _, ln := range strings.Split(string(data), "\n") {
			if b, ok := strings.CutPrefix(ln, "BODY:"); ok {
				f.db = b
			}
		}
		return ok
	})
	f.Handle([]string{"mariadb-dump"}, func(c system.Cmd) system.Result {
		var file string
		for _, a := range c.Args {
			if v, ok := strings.CutPrefix(a, "--result-file="); ok {
				file = v
			}
		}
		f.mu.Lock()
		body := f.db
		f.mu.Unlock()
		write(file, bkDumpOf(body))
		return ok
	})
	f.Handle([]string{"tail", "-n", "1"}, func(c system.Cmd) system.Result {
		data, err := f.ReadFile(c.Args[len(c.Args)-1])
		if err != nil {
			return exit(1, "", err.Error())
		}
		lines := strings.SplitAfter(strings.TrimSuffix(string(data), "\n"), "\n")
		return exit(0, lines[len(lines)-1]+"\n", "")
	})
	f.Handle([]string{"sha256sum"}, func(c system.Cmd) system.Result {
		p := c.Args[len(c.Args)-1]
		data, err := f.ReadFile(p)
		if err != nil {
			return exit(1, "", err.Error())
		}
		return exit(0, sha256Hex(data)+"  "+p+"\n", "")
	})
	f.Handle([]string{"du"}, func(c system.Cmd) system.Result {
		f.mu.Lock()
		defer f.mu.Unlock()
		return exit(0, f.du+"\t"+c.Args[len(c.Args)-1]+"\n", "")
	})
	f.Handle([]string{"wp", "core", "version"}, func(system.Cmd) system.Result {
		f.mu.Lock()
		defer f.mu.Unlock()
		return exit(0, f.wp+"\n", "")
	})
	f.Handle([]string{"tar", "--create"}, func(c system.Cmd) system.Result {
		var file, dir string
		var ms []bkMember
		for _, a := range c.Args[1:] {
			switch {
			case strings.HasPrefix(a, "--file="):
				file = strings.TrimPrefix(a, "--file=")
			case strings.HasPrefix(a, "--directory="):
				dir = strings.TrimPrefix(a, "--directory=")
			case strings.HasPrefix(a, "--"):
			default:
				snap := f.Snapshot()
				for _, p := range under(dir + "/" + a) {
					e := snap[p]
					name := strings.TrimPrefix(p, dir+"/")
					switch {
					case e.Mode.IsDir():
						ms = append(ms, bkMember{Name: name + "/", Type: "d"})
					case e.Mode&fs.ModeSymlink != 0:
						ms = append(ms, bkMember{Name: name, Type: "l", Target: e.Target})
					default:
						ms = append(ms, bkMember{Name: name, Type: "-", Data: e.Data})
					}
				}
			}
		}
		data, _ := json.Marshal(ms)
		write(file, string(data))
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.tarCode != 0 {
			return exit(f.tarCode, "", "tar: public/index.php: file changed as we read it")
		}
		return ok
	})
	readArchive := func(c system.Cmd) ([]bkMember, string, error) {
		var file, dir string
		for _, a := range c.Args {
			if v, ok := strings.CutPrefix(a, "--file="); ok {
				file = v
			}
			if v, ok := strings.CutPrefix(a, "--directory="); ok {
				dir = v
			}
		}
		data, err := f.ReadFile(file)
		if err != nil {
			return nil, "", err
		}
		var ms []bkMember
		if err := json.Unmarshal(data, &ms); err != nil {
			return nil, "", err
		}
		return ms, dir, nil
	}
	f.Handle([]string{"tar", "--list"}, func(c system.Cmd) system.Result {
		ms, _, err := readArchive(c)
		if err != nil {
			return exit(2, "", "tar: "+err.Error())
		}
		var b strings.Builder
		for _, m := range ms {
			fmt.Fprintf(&b, "%srw-r--r-- root/root %9d %s %s", m.Type, len(m.Data), bkTime, m.Name)
			switch m.Type {
			case "l":
				b.WriteString(" -> " + m.Target)
			case "h":
				b.WriteString(" link to " + m.Target)
			}
			b.WriteByte('\n')
		}
		return exit(0, b.String(), "")
	})
	f.Handle([]string{"tar", "--extract"}, func(c system.Cmd) system.Result {
		ms, dir, err := readArchive(c)
		if err != nil {
			return exit(2, "", "tar: "+err.Error())
		}
		data := map[string]string{}
		for _, m := range ms {
			p := dir + "/" + strings.TrimSuffix(m.Name, "/")
			switch m.Type {
			case "d":
				f.SetDir(p, 0o755, system.Root)
			case "l":
				f.SetSymlink(m.Target, p)
			case "h":
				f.SetFile(p, []byte(data[m.Target]), 0o644, system.Root)
			default:
				f.SetFile(p, []byte(m.Data), 0o644, system.Root)
				data[m.Name] = m.Data
			}
		}
		return ok
	})
	f.Handle([]string{"chown", "-R", "-h"}, func(c system.Cmd) system.Result {
		u, g, _ := strings.Cut(c.Args[2], ":")
		for _, p := range under(c.Args[len(c.Args)-1]) {
			if err := f.Chown(p, system.Owner{User: u, Group: g}); err != nil {
				return exit(1, "", err.Error())
			}
		}
		return ok
	})
	f.Handle([]string{"find", "-P"}, func(c system.Cmd) system.Result {
		root, typ, mode := c.Args[1], c.Args[3], c.Args[6]
		n, err := strconv.ParseUint(mode, 8, 32)
		if err != nil {
			return exit(1, "", "chmod: invalid mode")
		}
		perm := fs.FileMode(n) & fs.ModePerm
		if n&0o2000 != 0 {
			perm |= fs.ModeSetgid
		}
		snap := f.Snapshot()
		for _, p := range under(root) {
			e := snap[p]
			if (typ == "d" && e.Mode.IsDir()) || (typ == "f" && e.Mode.IsRegular()) {
				if err := f.Chmod(p, perm); err != nil {
					return exit(1, "", err.Error())
				}
			}
		}
		return ok
	})
	f.Handle([]string{"rm"}, func(c system.Cmd) system.Result {
		for _, p := range slices.Backward(under(c.Args[len(c.Args)-1])) {
			if err := f.Remove(p); err != nil {
				return exit(1, "", err.Error())
			}
		}
		return ok
	})
	return f
}

func (f *bkFake) dbNow() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.db
}

func (f *bkFake) ranHas(sub string) bool {
	for _, s := range ranStrings(f.Ran()) {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

var bkNow = time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)

const bkStamp = "20261119-100000"

func bkArchive(l site.Layout) *BackupArchive {
	return &BackupArchive{Layout: l, Src: l.Dir, Dir: site.BackupDir(l), PHPVersion: "8.3", now: func() time.Time { return bkNow }}
}

// bkReadArchive đọc lại các thành viên của file tar giả.
func bkReadArchive(t *testing.T, f *bkFake, p string) map[string]bkMember {
	t.Helper()
	data, err := f.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var ms []bkMember
	if err := json.Unmarshal(data, &ms); err != nil {
		t.Fatal(err)
	}
	out := map[string]bkMember{}
	for _, m := range ms {
		out[m.Name] = m
	}
	return out
}

// ================================================================== SpaceOK

func TestSpaceOK(t *testing.T) {
	const max = ^uint64(0)
	for _, c := range []struct {
		free, total, need uint64
		want              bool
	}{
		{free: 60, total: 100, need: 10, want: true},
		{free: 12, total: 20, need: 10, want: true},  // đúng 1,2 lần, còn 2 = 10%
		{free: 12, total: 21, need: 10, want: false}, // còn 2 < 2,1
		{free: 11, total: 10, need: 10, want: false}, // < 1,2 lần
		{free: 5, total: 100, need: 4, want: false},  // 4 × 1,2 = 4,8 > ... còn 1 < 10
		{free: 50, total: 100, need: 4, want: true},  // làm tròn lên 4/5 → 1
		{free: 9, total: 100, need: 10, want: false}, // không đủ cả need
		{free: 10, total: 100, need: 0, want: true},  // DB rỗng, còn đúng 10%
		{free: 9, total: 100, need: 0, want: false},  // đĩa đã dưới 10%
		{free: max, total: max, need: max, want: false},
		{free: max, total: max, need: 1, want: true},
	} {
		if got := SpaceOK(c.free, c.total, c.need); got != c.want {
			t.Errorf("SpaceOK(free %d, total %d, need %d) = %v, muốn %v", c.free, c.total, c.need, got, c.want)
		}
	}
}

func TestMeasure(t *testing.T) {
	ctx := context.Background()

	t.Run("MeasureBackup cộng du và DB, đo đĩa ở thư mục cha gần nhất", func(t *testing.T) {
		f := newBkFake(t)
		f.SetDisk("/var/lib", system.DiskUsage{Total: 1000, Free: 700})
		need, free, total, err := MeasureBackup(ctx, f, f.l, f.l.Dir, site.BackupDir(f.l))
		if err != nil {
			t.Fatal(err)
		}
		if need != 6000 || free != 700 || total != 1000 {
			t.Errorf("need %d free %d total %d", need, free, total)
		}
		if !f.ranHas("du -sb --one-file-system -- " + f.l.Public) {
			t.Errorf("thiếu du: %v", ranStrings(f.Ran()))
		}
	})

	t.Run("MeasureDB: DB chưa có bảng là 0; output lạ là lỗi", func(t *testing.T) {
		f := newBkFake(t)
		f.dbSize = "0"
		if n, err := MeasureDB(ctx, f, f.l); err != nil || n != 0 {
			t.Errorf("= %d, %v", n, err)
		}
		f.dbSize = "NULL"
		if _, err := MeasureDB(ctx, f, f.l); err == nil {
			t.Error("muốn lỗi")
		}
	})

	t.Run("du lỗi hoặc public là symlink thì lỗi", func(t *testing.T) {
		f := newBkFake(t)
		f.Handle([]string{"du"}, func(system.Cmd) system.Result {
			return system.Result{ExitCode: 1, Stderr: "du: cannot read directory"}
		})
		if _, _, _, err := MeasureBackup(ctx, f, f.l, f.l.Dir, site.BackupDir(f.l)); err == nil || !strings.Contains(err.Error(), "cannot read") {
			t.Errorf("err = %v", err)
		}
		f = newBkFake(t)
		pub := site.TrashDir(f.l) + "/public"
		f.SetSymlink("/root", pub)
		if _, _, _, err := MeasureBackup(ctx, f, f.l, site.TrashDir(f.l), site.BackupDir(f.l)); err == nil {
			t.Error("public symlink: muốn lỗi")
		}
	})

	t.Run("nguồn lạ thì lỗi, không chạy lệnh", func(t *testing.T) {
		f := newBkFake(t)
		for _, src := range []string{"/etc", "/var/www", f.l.Dir + "/public", "/var/www/other.example.com", ""} {
			if _, _, _, err := MeasureBackup(ctx, f, f.l, src, site.BackupDir(f.l)); err == nil {
				t.Errorf("src %q: muốn lỗi", src)
			}
		}
		if len(f.Ran()) != 0 {
			t.Errorf("đã chạy %v", ranStrings(f.Ran()))
		}
	})
}

// ============================================================ BackupArchive

func TestBackupArchive(t *testing.T) {
	ctx := context.Background()

	t.Run("Apply: file 0600 trong thư mục 0700, manifest đúng, thư mục tạm đã dọn, Check true", func(t *testing.T) {
		f := newBkFake(t)
		s := bkArchive(f.l)
		if err := s.Undo(ctx, f); err == nil {
			t.Error("Undo trước Apply: muốn lỗi")
		}
		if done, err := s.Check(ctx, f); err != nil || done {
			t.Fatalf("Check trước = %v, %v", done, err)
		}
		if err := s.Apply(ctx, f); err != nil {
			t.Fatal(err)
		}
		if done, err := s.Check(ctx, f); err != nil || !done {
			t.Fatalf("Check sau = %v, %v", done, err)
		}
		dir := site.BackupDir(f.l)
		want := dir + "/shop.example.com-" + bkStamp + ".tar.gz"
		snap := f.Snapshot()
		if s.Path != want || s.WPVersion != "6.7.1" || s.Size != int64(len(snap[want].Data)) || s.SHA256 != sha256Hex([]byte(snap[want].Data)) {
			t.Errorf("kết quả = %q %q %d %q", s.Path, s.WPVersion, s.Size, s.SHA256)
		}
		if e := snap[want]; e.Mode != 0o600 || e.Owner != system.Root {
			t.Errorf("file backup %v %v", e.Mode, e.Owner)
		}
		for _, d := range []string{site.BackupRoot, dir} {
			if e := snap[d]; e.Mode != fs.ModeDir|0o700 || e.Owner != system.Root {
				t.Errorf("%s = %v %v", d, e.Mode, e.Owner)
			}
		}
		if e := snap["/var/lib/tpan"]; e.Mode != fs.ModeDir|0o755 {
			t.Errorf("/var/lib/tpan = %v", e.Mode)
		}
		for p := range snap {
			if strings.Contains(p, ".tmp-") || strings.HasSuffix(p, ".part") {
				t.Errorf("còn sót %s", p)
			}
		}
		ms := bkReadArchive(t, f, want)
		if ms["db.sql"].Data != bkDumpOf("v1") || ms["public/index.php"].Data != "<?php // v1" ||
			ms["public/link.php"].Type != "l" || ms["public/"].Type != "d" {
			t.Errorf("thành viên = %+v", ms)
		}
		var man backupManifest
		if err := json.Unmarshal([]byte(ms["manifest.json"].Data), &man); err != nil {
			t.Fatal(err)
		}
		if man != (backupManifest{Domain: "shop.example.com", WPVersion: "6.7.1", PHPVersion: "8.3", Created: "2026-11-19T10:00:00Z", DBSHA256: sha256Hex([]byte(bkDumpOf("v1")))}) {
			t.Errorf("manifest = %+v", man)
		}
		tmp := dir + "/.tmp-" + bkStamp
		for _, w := range []string{
			"mariadb-dump --single-transaction --quick --result-file=" + tmp + "/db.sql " + f.l.DB,
			"runuser -u " + f.l.User + " -- wp core version --path=" + f.l.Public,
			"tar --create --gzip --file=" + want + ".part --one-file-system --directory=" + tmp + " manifest.json db.sql --directory=" + f.l.Dir + " public",
		} {
			if !f.ranHas(w) {
				t.Errorf("thiếu %q trong %v", w, ranStrings(f.Ran()))
			}
		}
		for _, c := range f.Ran() {
			if c.Name == "tar" && slices.Contains(c.Args, "-h") || slices.Contains(c.Args, "--dereference") {
				t.Errorf("tar đi theo symlink: %v", c)
			}
		}
	})

	// Backup luôn là bản mới nên "chạy lại thì không lệnh nào" không áp dụng;
	// thay vào đó: engine chạy được (Check sau Apply đạt), và Undo trả hệ
	// thống file về y như cũ.
	t.Run("qua engine: thành công; Undo trả hệ thống file về như cũ", func(t *testing.T) {
		f := newBkFake(t)
		f.SetDir(site.BackupRoot, 0o700, system.Root)
		f.SetDir(site.BackupDir(f.l), 0o700, system.Root)
		before := f.Snapshot()
		s := bkArchive(f.l)
		if rep := runOne(t, f, s); rep.Job.Status != engine.JobSucceeded {
			t.Fatalf("%s %s", rep.Job.Status, rep.Job.Error)
		}
		if err := s.Undo(ctx, f); err != nil {
			t.Fatal(err)
		}
		if after := f.Snapshot(); !reflect.DeepEqual(before, after) {
			t.Errorf("sau Undo khác trước")
		}
	})

	t.Run("site trong thùng rác: nguồn là TrashDir, không chạy wp", func(t *testing.T) {
		f := newBkFake(t)
		if err := (&MoveTree{From: f.l.Dir, To: site.TrashDir(f.l)}).Apply(ctx, f); err != nil {
			t.Fatal(err)
		}
		s := bkArchive(f.l)
		s.Src = site.TrashDir(f.l)
		if err := s.Apply(ctx, f); err != nil {
			t.Fatal(err)
		}
		if s.WPVersion != "" || f.ranHas("wp core") {
			t.Errorf("WPVersion %q, đã chạy wp", s.WPVersion)
		}
		if !f.ranHas("--directory=" + site.TrashDir(f.l) + " public") {
			t.Errorf("tar không đọc từ thùng rác: %v", ranStrings(f.Ran()))
		}
		if bkReadArchive(t, f, s.Path)["public/index.php"].Data != "<?php // v1" {
			t.Error("thiếu file của site")
		}
	})

	t.Run("wp lỗi hoặc in rác: WPVersion rỗng, backup vẫn xong", func(t *testing.T) {
		for _, out := range []string{"Error: This does not seem to be a WordPress installation.", "6.7; rm -rf /"} {
			f := newBkFake(t)
			f.wp = out
			s := bkArchive(f.l)
			if err := s.Apply(ctx, f); err != nil || s.WPVersion != "" {
				t.Errorf("%q: %v, WPVersion %q", out, err, s.WPVersion)
			}
		}
		f := newBkFake(t)
		f.Handle([]string{"wp"}, func(system.Cmd) system.Result { return system.Result{ExitCode: 1} })
		if err := bkArchive(f.l).Apply(ctx, f); err != nil {
			t.Error(err)
		}
	})

	t.Run("không đủ chỗ: lỗi kèm số đo, không tạo gì, không xuất DB", func(t *testing.T) {
		f := newBkFake(t)
		f.SetDisk("/", system.DiskUsage{Total: 100000, Free: 7000}) // cần 6000 × 1,2 = 7200
		before := f.Snapshot()
		err := bkArchive(f.l).Apply(ctx, f)
		if err == nil || !strings.Contains(err.Error(), "không đủ chỗ trống") || !strings.Contains(err.Error(), "6000") {
			t.Errorf("err = %v", err)
		}
		if f.ranHas("mariadb-dump") || !reflect.DeepEqual(before, f.Snapshot()) {
			t.Error("đã làm gì đó dù thiếu chỗ")
		}
	})

	t.Run("tar thoát 1 (file đổi lúc đọc): chấp nhận", func(t *testing.T) {
		f := newBkFake(t)
		f.tarCode = 1
		s := bkArchive(f.l)
		if err := s.Apply(ctx, f); err != nil {
			t.Fatal(err)
		}
		if _, err := f.Stat(s.Path); err != nil {
			t.Error(err)
		}
	})

	t.Run("lỗi giữa chừng: lỗi có ngữ cảnh, dọn .part và thư mục tạm", func(t *testing.T) {
		for _, c := range []struct {
			name string
			set  func(f *bkFake)
			want string
		}{
			{"tar thoát 2", func(f *bkFake) { f.tarCode = 2 }, "nén backup"},
			{"mariadb-dump lỗi", func(f *bkFake) {
				f.Handle([]string{"mariadb-dump"}, func(system.Cmd) system.Result {
					return system.Result{ExitCode: 2, Stderr: "mariadb-dump: Got error: 2002"}
				})
			}, "2002"},
			{"sha256sum lỗi", func(f *bkFake) {
				f.Handle([]string{"sha256sum"}, func(system.Cmd) system.Result { return system.Result{ExitCode: 1, Stderr: "I/O error"} })
			}, "sha256"},
			{"rename lỗi", func(f *bkFake) {
				f.Fail("rename", site.BackupDir(f.l)+"/shop.example.com-"+bkStamp+".tar.gz.part", errors.New("EIO"))
			}, "đổi tên"},
		} {
			t.Run(c.name, func(t *testing.T) {
				f := newBkFake(t)
				f.SetDir(site.BackupRoot, 0o700, system.Root)
				f.SetDir(site.BackupDir(f.l), 0o700, system.Root)
				before := f.Snapshot()
				c.set(f)
				err := bkArchive(f.l).Apply(ctx, f)
				if err == nil || !strings.Contains(err.Error(), c.want) {
					t.Errorf("err = %v, muốn chứa %q", err, c.want)
				}
				if !reflect.DeepEqual(before, f.Snapshot()) {
					t.Errorf("còn sót: %v", f.Snapshot())
				}
			})
		}
	})

	t.Run("public là symlink: từ chối, không tạo bản backup không khôi phục được", func(t *testing.T) {
		f := newBkFake(t)
		f.SetDir("/srv/x", 0o755, system.Root)
		if err := f.Rename(f.l.Public, "/srv/x/public"); err != nil {
			t.Fatal(err)
		}
		f.SetSymlink("/srv/x/public", f.l.Public)
		if err := bkArchive(f.l).Apply(ctx, f); err == nil || f.ranHas("tar") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("tham số sai thì Check và Apply lỗi, không chạy lệnh", func(t *testing.T) {
		l := dbLayout(t)
		bad := l
		bad.DB = "tp_other"
		for _, s := range []*BackupArchive{
			{Layout: site.Layout{}, Src: l.Dir, Dir: site.BackupDir(l), PHPVersion: "8.3"},
			{Layout: bad, Src: l.Dir, Dir: site.BackupDir(l), PHPVersion: "8.3"},
			{Layout: l, Src: "/etc", Dir: site.BackupDir(l), PHPVersion: "8.3"},
			{Layout: l, Src: l.Dir + "/../../etc", Dir: site.BackupDir(l), PHPVersion: "8.3"},
			{Layout: l, Src: l.Dir, Dir: "/tmp", PHPVersion: "8.3"},
			{Layout: l, Src: l.Dir, Dir: site.BackupRoot + "/other.example.com", PHPVersion: "8.3"},
			{Layout: l, Src: l.Dir, Dir: site.BackupDir(l), PHPVersion: "8.3; id"},
			{Layout: l, Src: l.Dir, Dir: site.BackupDir(l), PHPVersion: ""},
		} {
			f := newBkFake(t)
			if _, err := s.Check(ctx, f); err == nil {
				t.Errorf("Check %+v: muốn lỗi", s)
			}
			if err := s.Apply(ctx, f); err == nil {
				t.Errorf("Apply %+v: muốn lỗi", s)
			}
			if len(f.Ran()) != 0 {
				t.Errorf("đã chạy %v", ranStrings(f.Ran()))
			}
		}
	})

	t.Run("Info: giống hệt nhau giữa hai lần dựng, không có giờ thật", func(t *testing.T) {
		l := dbLayout(t)
		a, b := bkArchive(l), bkArchive(l)
		b.now = func() time.Time { return bkNow.Add(time.Hour) }
		ia := a.Info()
		if !reflect.DeepEqual(ia, b.Info()) {
			t.Error("Info khác nhau")
		}
		if ia.ID != "backup.create:shop.example.com" || ia.Why.Vi == "" || ia.Why.En == "" || ia.Danger {
			t.Errorf("Info = %+v", ia)
		}
		all := strings.Join(ia.Commands, "\n")
		if strings.Contains(all, "2026") || !strings.Contains(all, "<YYYYMMDD-HHMMSS>.tar.gz") || !strings.Contains(all, "mariadb-dump") {
			t.Errorf("Commands = %v", ia.Commands)
		}
	})
}

// ============================================================ BackupExtract

// bkMade tạo một bản backup thật (qua tar giả) rồi đổi site để khác bản đó.
func bkMade(t *testing.T) (*bkFake, *BackupExtract) {
	t.Helper()
	f := newBkFake(t)
	a := bkArchive(f.l)
	if err := a.Apply(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	own := system.Owner{User: f.l.User, Group: site.WebGroup}
	f.SetFile(f.l.Public+"/index.php", []byte("<?php // v2"), 0o640, own)
	f.SetFile(f.l.WPConfig, []byte(bkWPConfig(f.l.DB, "passnew")), 0o600, system.Owner{User: f.l.User, Group: f.l.User})
	f.mu.Lock()
	f.db = "v2"
	f.mu.Unlock()
	f.ResetRan()
	return f, &BackupExtract{Layout: f.l, Archive: a.Path, SHA256: a.SHA256, Dir: site.RestoreDir(f.l)}
}

// bkForge ghi một file backup giả do test dựng tay.
func bkForge(t *testing.T, f *bkFake, ms []bkMember) *BackupExtract {
	t.Helper()
	data, err := json.Marshal(ms)
	if err != nil {
		t.Fatal(err)
	}
	p := site.BackupDir(f.l) + "/shop.example.com-20261118-000000.tar.gz"
	f.SetFile(p, data, 0o600, system.Root)
	return &BackupExtract{Layout: f.l, Archive: p, SHA256: sha256Hex(data), Dir: site.RestoreDir(f.l)}
}

func bkGood(domain string) []bkMember {
	return []bkMember{
		{Name: "manifest.json", Type: "-", Data: `{"domain":"` + domain + `"}`},
		{Name: "db.sql", Type: "-", Data: bkDumpOf("old")},
		{Name: "public/", Type: "d"},
		{Name: "public/index.php", Type: "-", Data: "<?php"},
	}
}

func TestBackupExtract(t *testing.T) {
	ctx := context.Background()

	t.Run("Apply: chủ và quyền đặt lại, wp-config lấy bản đang chạy, Check true; Undo về như cũ", func(t *testing.T) {
		f, s := bkMade(t)
		before := f.Snapshot()
		if err := s.Undo(ctx, f); err == nil {
			t.Error("Undo trước Apply: muốn lỗi")
		}
		if done, err := s.Check(ctx, f); err != nil || done {
			t.Fatalf("Check trước = %v, %v", done, err)
		}
		if err := s.Apply(ctx, f); err != nil {
			t.Fatal(err)
		}
		if done, err := s.Check(ctx, f); err != nil || !done {
			t.Fatalf("Check sau = %v, %v", done, err)
		}
		dir, own := site.RestoreDir(f.l), system.Owner{User: f.l.User, Group: site.WebGroup}
		snap := f.Snapshot()
		for _, d := range []string{site.RestoreRoot, dir} {
			if e := snap[d]; e.Mode != fs.ModeDir|0o700 || e.Owner != system.Root {
				t.Errorf("%s = %v %v", d, e.Mode, e.Owner)
			}
		}
		for _, d := range []string{dir + "/public", dir + "/public/wp-content"} {
			if e := snap[d]; e.Mode != fs.ModeDir|fs.ModeSetgid|0o750 || e.Owner != own {
				t.Errorf("%s = %v %v", d, e.Mode, e.Owner)
			}
		}
		if e := snap[dir+"/public/index.php"]; e.Mode != 0o640 || e.Owner != own || e.Data != "<?php // v1" {
			t.Errorf("index.php = %+v", e)
		}
		if e := snap[dir+"/public/link.php"]; e.Mode&fs.ModeSymlink == 0 || e.Owner != own {
			t.Errorf("link.php = %+v", e)
		}
		// Nhóm www-data như mọi file khác trong public: thư mục site có setgid
		// nên file WP-CLI tạo lúc dựng site mang nhóm www-data (e2e M9 đo được).
		if e := snap[dir+"/public/wp-config.php"]; e.Mode != 0o600 || e.Owner != own || !strings.Contains(e.Data, "passnew") {
			t.Errorf("wp-config.php = %+v", e)
		}
		if e := snap[dir+"/db.sql"]; e.Data != bkDumpOf("v1") {
			t.Errorf("db.sql = %q", e.Data)
		}
		want := "tar --extract --gzip --file=" + s.Archive + " --directory=" + dir + " --no-same-owner --no-same-permissions"
		if !f.ranHas(want) {
			t.Errorf("thiếu %q: %v", want, ranStrings(f.Ran()))
		}
		if err := s.Undo(ctx, f); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, f.Snapshot()) {
			t.Error("sau Undo khác trước")
		}
	})

	t.Run("site không có wp-config.php: giữ bản trong backup, quyền 600", func(t *testing.T) {
		f, s := bkMade(t)
		if err := f.Remove(f.l.WPConfig); err != nil {
			t.Fatal(err)
		}
		if err := s.Apply(ctx, f); err != nil {
			t.Fatal(err)
		}
		e := f.Snapshot()[s.Dir+"/public/wp-config.php"]
		if e.Mode != 0o600 || !strings.Contains(e.Data, "passold") {
			t.Errorf("wp-config.php = %+v", e)
		}
	})

	t.Run("wp-config.php đang chạy lạ: từ chối, dọn sạch", func(t *testing.T) {
		for _, c := range []struct {
			name string
			set  func(f *bkFake)
		}{
			{"symlink", func(f *bkFake) {
				_ = f.Remove(f.l.WPConfig)
				f.SetSymlink("/etc/shadow", f.l.WPConfig)
			}},
			{"DB_NAME của site khác", func(f *bkFake) {
				f.SetFile(f.l.WPConfig, []byte(bkWPConfig("tp_other_123456", "x")), 0o600, system.Root)
			}},
			{"không phải wp-config", func(f *bkFake) {
				f.SetFile(f.l.WPConfig, []byte("root:$6$abc:19000:0:99999:7:::\n"), 0o600, system.Root)
			}},
			{"public là symlink", func(f *bkFake) {
				f.SetDir("/srv/x", 0o755, system.Root)
				_ = f.Rename(f.l.Public, "/srv/x/public")
				f.SetSymlink("/srv/x/public", f.l.Public)
			}},
		} {
			t.Run(c.name, func(t *testing.T) {
				f, s := bkMade(t)
				c.set(f)
				before := f.Snapshot()
				if err := s.Apply(ctx, f); err == nil {
					t.Fatal("muốn lỗi")
				}
				if !reflect.DeepEqual(before, f.Snapshot()) {
					t.Error("Apply lỗi mà để lại thứ gì đó")
				}
			})
		}
	})

	t.Run("sha256 khác: backup hỏng, không giải nén", func(t *testing.T) {
		f, s := bkMade(t)
		s.SHA256 = strings.Repeat("0", 64)
		err := s.Apply(ctx, f)
		if err == nil || !strings.Contains(err.Error(), "backup hỏng") || f.ranHas("--extract") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("Dir còn sót: lỗi chỉ cách dọn, không đụng vào nó", func(t *testing.T) {
		f, s := bkMade(t)
		f.SetDir(s.Dir+"/old-public", 0o750, system.Root)
		before := f.Snapshot()
		err := s.Apply(ctx, f)
		if err == nil || !strings.Contains(err.Error(), "rm -r --one-file-system -- "+s.Dir) {
			t.Errorf("err = %v", err)
		}
		if !reflect.DeepEqual(before, f.Snapshot()) {
			t.Error("đổi hệ thống file")
		}
	})

	t.Run("backup của domain khác: lỗi, dọn Dir", func(t *testing.T) {
		f := newBkFake(t)
		s := bkForge(t, f, bkGood("evil.example.com"))
		before := f.Snapshot()
		if err := s.Apply(ctx, f); err == nil || !strings.Contains(err.Error(), "evil.example.com") {
			t.Errorf("err = %v", err)
		}
		if !reflect.DeepEqual(before, f.Snapshot()) {
			t.Error("còn sót thư mục giải nén")
		}
	})

	t.Run("danh sách thành viên độc: từ chối trước khi giải nén", func(t *testing.T) {
		good := bkGood("shop.example.com")
		with := func(extra ...bkMember) []bkMember { return append(slices.Clone(good), extra...) }
		for name, ms := range map[string][]bkMember{
			"tuyệt đối":           with(bkMember{Name: "/etc/cron.d/x", Type: "-"}),
			"có ..":               with(bkMember{Name: "public/../../etc/passwd", Type: "-"}),
			"ngoài public":        with(bkMember{Name: "etc/passwd", Type: "-"}),
			"thiết bị":            with(bkMember{Name: "public/sda", Type: "b"}),
			"thiết bị ký tự":      with(bkMember{Name: "public/null", Type: "c"}),
			"FIFO":                with(bkMember{Name: "public/fifo", Type: "p"}),
			"trùng tên":           with(bkMember{Name: "public/index.php", Type: "l", Target: "/etc/passwd"}),
			"link cứng ra ngoài":  with(bkMember{Name: "public/h", Type: "h", Target: "etc/shadow"}),
			"link cứng có ..":     with(bkMember{Name: "public/h", Type: "h", Target: "public/../../x"}),
			"thiếu db.sql":        {good[0], good[2], good[3]},
			"thiếu manifest":      {good[1], good[2], good[3]},
			"thiếu public/":       {good[0], good[1]},
			"public là symlink":   {good[0], good[1], {Name: "public", Type: "l", Target: "/etc"}},
			"db.sql là symlink":   {good[0], {Name: "db.sql", Type: "l", Target: "/etc/shadow"}, good[2]},
			"manifest là thư mục": {{Name: "manifest.json/", Type: "d"}, good[1], good[2]},
		} {
			t.Run(name, func(t *testing.T) {
				f := newBkFake(t)
				s := bkForge(t, f, ms)
				before := f.Snapshot()
				if err := s.Apply(ctx, f); err == nil || !strings.Contains(err.Error(), "từ chối backup") {
					t.Errorf("err = %v", err)
				}
				if f.ranHas("--extract") || !reflect.DeepEqual(before, f.Snapshot()) {
					t.Error("đã giải nén hoặc đổi hệ thống file")
				}
			})
		}
		f := newBkFake(t)
		s := bkForge(t, f, with(bkMember{Name: "public/h", Type: "h", Target: "public/index.php"}))
		if err := s.Apply(ctx, f); err != nil {
			t.Errorf("link cứng trong public/: %v", err)
		}
	})

	t.Run("tham số sai thì Check và Apply lỗi, không chạy lệnh", func(t *testing.T) {
		l := dbLayout(t)
		arch := site.BackupDir(l) + "/shop.example.com-x.tar.gz"
		sum := strings.Repeat("a", 64)
		for _, s := range []*BackupExtract{
			{Layout: site.Layout{}, Archive: arch, SHA256: sum, Dir: site.RestoreDir(l)},
			{Layout: l, Archive: "/etc/shadow", SHA256: sum, Dir: site.RestoreDir(l)},
			{Layout: l, Archive: site.BackupDir(l) + "/../other.example.com/a.tar.gz", SHA256: sum, Dir: site.RestoreDir(l)},
			{Layout: l, Archive: site.BackupRoot + "/other.example.com/a.tar.gz", SHA256: sum, Dir: site.RestoreDir(l)},
			{Layout: l, Archive: arch, SHA256: "ABC", Dir: site.RestoreDir(l)},
			{Layout: l, Archive: arch, SHA256: strings.Repeat("A", 64), Dir: site.RestoreDir(l)},
			{Layout: l, Archive: arch, SHA256: sum, Dir: l.Dir},
			{Layout: l, Archive: arch, SHA256: sum, Dir: site.RestoreRoot},
			{Layout: l, Archive: arch, SHA256: sum, Dir: "/"},
		} {
			f := newBkFake(t)
			if _, err := s.Check(ctx, f); err == nil {
				t.Errorf("Check %+v: muốn lỗi", s)
			}
			if err := s.Apply(ctx, f); err == nil {
				t.Errorf("Apply %+v: muốn lỗi", s)
			}
			if len(f.Ran()) != 0 {
				t.Errorf("đã chạy %v", ranStrings(f.Ran()))
			}
		}
	})

	t.Run("Info", func(t *testing.T) {
		_, s := bkMade(t)
		in := s.Info()
		if in.ID != "backup.extract:shop.example.com" || in.Why.Vi == "" || in.Why.En == "" || in.Danger || len(in.Commands) != 6 {
			t.Errorf("Info = %+v", in)
		}
	})
}

func TestParseTarLine(t *testing.T) {
	for _, c := range []struct {
		line string
		want tarMember
		ok   bool
	}{
		{"-rw-r--r-- root/root      1234 2026-11-19 10:00 public/index.php", tarMember{'-', "public/index.php", ""}, true},
		{"drwxr-x--- tp_x/www-data     0 2026-11-19 10:00 public/", tarMember{'d', "public/", ""}, true},
		{"-rw-r--r-- root/root         1 2026-11-19 10:00 public/a b.txt", tarMember{'-', "public/a b.txt", ""}, true},
		{"lrwxrwxrwx root/root         0 2026-11-19 10:00 public/a -> ../b", tarMember{'l', "public/a", "../b"}, true},
		{"hrw-r--r-- root/root         0 2026-11-19 10:00 public/c link to public/d", tarMember{'h', "public/c", "public/d"}, true},
		{"crw-rw-rw- root/root       1,3 2026-11-19 10:00 public/null", tarMember{'c', "public/null", ""}, true},
		{"public/index.php", tarMember{}, false},
		{"", tarMember{}, false},
		{"lrwxrwxrwx root/root 0 2026-11-19 10:00 public/a", tarMember{}, false},
		{"-rw-r--r-- root/root 0 2026-11-19 10:00 ", tarMember{}, false},
	} {
		got, err := parseTarLine(c.line)
		if (err == nil) != c.ok || (c.ok && got != c.want) {
			t.Errorf("%q = %+v, %v", c.line, got, err)
		}
	}
}

// ================================================================== DBImport

func bkImport(t *testing.T) (*bkFake, *DBImport) {
	t.Helper()
	f := newBkFake(t)
	file := site.BackupDir(f.l) + "/shop.example.com-db-20261118-000000.sql"
	undo := site.BackupDir(f.l) + "/shop.example.com-db-" + bkStamp + ".sql"
	f.SetFile(file, []byte(bkDumpOf("imported")), 0o600, system.Root)
	f.SetFile(undo, []byte(bkDumpOf("v1")), 0o600, system.Root)
	return f, &DBImport{Layout: f.l, File: file, UndoFile: undo}
}

func TestDBImport(t *testing.T) {
	ctx := context.Background()

	t.Run("Apply: xoá, tạo lại DB rồi nhập; Check true; Undo nhập lại bản trước", func(t *testing.T) {
		f, s := bkImport(t)
		if err := s.Undo(ctx, f); err == nil {
			t.Error("Undo trước Apply: muốn lỗi")
		}
		if done, err := s.Check(ctx, f); err != nil || done {
			t.Fatalf("Check trước = %v, %v", done, err)
		}
		if rep := runOne(t, f, s); rep.Job.Status != engine.JobSucceeded {
			t.Fatalf("%s %s", rep.Job.Status, rep.Job.Error)
		}
		if f.dbNow() != "imported" {
			t.Errorf("DB = %q", f.dbNow())
		}
		var reset, imp bool
		for _, c := range f.Ran() {
			if c.Name == "mariadb" && string(c.Stdin) == "DROP DATABASE IF EXISTS `"+f.l.DB+"`;\nCREATE DATABASE `"+f.l.DB+"` CHARACTER SET utf8mb4;\n" {
				reset = true
			}
			if c.String() == "mariadb --database="+f.l.DB+" --abort-source-on-error -e 'source "+s.File+"'" {
				imp = reset
			}
		}
		if !reset || !imp {
			t.Errorf("lệnh: %v", ranStrings(f.Ran()))
		}
		if err := s.Undo(ctx, f); err != nil {
			t.Fatal(err)
		}
		if f.dbNow() != "v1" {
			t.Errorf("sau Undo DB = %q", f.dbNow())
		}
	})

	t.Run("nhập lỗi giữa chừng: tự nhập lại UndoFile rồi báo lỗi", func(t *testing.T) {
		f, s := bkImport(t)
		f.importFail = s.File
		err := s.Apply(ctx, f)
		if err == nil || !strings.Contains(err.Error(), "1064") || !strings.Contains(err.Error(), s.File) {
			t.Errorf("err = %v", err)
		}
		if f.dbNow() != "v1" {
			t.Errorf("DB = %q, muốn về bản trước", f.dbNow())
		}
	})

	t.Run("cả hai cùng lỗi: báo cả hai", func(t *testing.T) {
		f, s := bkImport(t)
		f.Handle([]string{"mariadb", "--database=" + f.l.DB}, func(system.Cmd) system.Result {
			return system.Result{ExitCode: 1, Stderr: "ERROR 2002"}
		})
		err := s.Apply(ctx, f)
		if err == nil || !strings.Contains(err.Error(), "trả DB về") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("UndoFile hoặc File không trọn vẹn: không xoá DB", func(t *testing.T) {
		for _, which := range []string{"undo", "file", "undo-missing"} {
			f, s := bkImport(t)
			switch which {
			case "undo":
				f.SetFile(s.UndoFile, []byte("-- MariaDB dump\nINSERT"), 0o600, system.Root)
			case "file":
				f.SetFile(s.File, []byte("-- MariaDB dump\nINSERT"), 0o600, system.Root)
			default:
				if err := f.Remove(s.UndoFile); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.Apply(ctx, f); err == nil {
				t.Errorf("%s: muốn lỗi", which)
			}
			if f.ranHas("mariadb -N -B") && f.dbNow() != "v1" {
				t.Errorf("%s: đã đụng DB", which)
			}
			for _, c := range f.Ran() {
				if strings.Contains(string(c.Stdin), "DROP") {
					t.Errorf("%s: đã xoá DB", which)
				}
			}
		}
	})

	t.Run("tham số sai thì Check và Apply lỗi, không chạy lệnh", func(t *testing.T) {
		l := dbLayout(t)
		ok := site.BackupDir(l) + "/a.sql"
		undo := site.RestoreDir(l) + "/db-before.sql"
		bad := l
		bad.DB = "tp_x`; DROP DATABASE mysql; --"
		for _, s := range []*DBImport{
			{Layout: site.Layout{}, File: ok, UndoFile: undo},
			{Layout: bad, File: ok, UndoFile: undo},
			{Layout: l, File: "/etc/shadow", UndoFile: undo},
			{Layout: l, File: ok, UndoFile: ""},
			{Layout: l, File: ok, UndoFile: ok},
			{Layout: l, File: site.BackupRoot + "/a b.sql", UndoFile: undo},
			{Layout: l, File: site.BackupRoot + "/a.sql; system id", UndoFile: undo},
			{Layout: l, File: site.BackupRoot + "/$(id).sql", UndoFile: undo},
			{Layout: l, File: site.BackupRoot + "/a\nsystem id", UndoFile: undo},
			{Layout: l, File: site.BackupRoot + "/../../../etc/passwd", UndoFile: undo},
			{Layout: l, File: "/var/lib/tpan/restorex/a.sql", UndoFile: undo},
			{Layout: l, File: site.BackupRoot + "/ả.sql", UndoFile: undo},
		} {
			f := newBkFake(t)
			if _, err := s.Check(ctx, f); err == nil {
				t.Errorf("Check %q: muốn lỗi", s.File)
			}
			if err := s.Apply(ctx, f); err == nil {
				t.Errorf("Apply %q: muốn lỗi", s.File)
			}
			if len(f.Ran()) != 0 {
				t.Errorf("đã chạy %v", ranStrings(f.Ran()))
			}
		}
	})

	t.Run("Info", func(t *testing.T) {
		_, s := bkImport(t)
		in := s.Info()
		if in.ID != "db.import:shop.example.com" || !in.Danger || in.Why.Vi == "" || in.Why.En == "" || !in.NoUndo.IsZero() || len(in.Commands) != 2 {
			t.Errorf("Info = %+v", in)
		}
	})
}

// ============================================== Khôi phục: cả chuỗi bước của core

// bkBoom là bước luôn lỗi, để engine hoàn tác cả chuỗi.
type bkBoom struct{}

func (bkBoom) Info() engine.StepInfo {
	return engine.StepInfo{ID: "test.boom", Title: i18n.T("boom", "boom"), Why: i18n.T("x", "x")}
}
func (bkBoom) Check(context.Context, system.System) (bool, error) { return false, nil }
func (bkBoom) Apply(context.Context, system.System) error         { return errors.New("boom") }
func (bkBoom) Undo(context.Context, system.System) error          { return nil }

// bkRestore dựng đúng các bước của core.PrepareRestore (types_m9.go).
func bkRestore(f *bkFake, x *BackupExtract) []engine.Step {
	l, dir := f.l, site.RestoreDir(f.l)
	before := bkArchive(l)
	before.now = func() time.Time { return bkNow.Add(time.Hour) }
	return []engine.Step{
		before,
		x,
		&MoveTree{From: l.Public, To: dir + "/old-public"},
		&MoveTree{From: dir + "/public", To: l.Public},
		&MariaDBDump{Path: dir + "/db-before.sql", DB: l.DB},
		&DBImport{Layout: l, File: dir + "/db.sql", UndoFile: dir + "/db-before.sql"},
	}
}

func TestRestoreChain(t *testing.T) {
	run := func(t *testing.T, f *bkFake, steps []engine.Step) engine.Report {
		t.Helper()
		rep, err := engine.New(f.FakeSystem, engine.Options{}).Execute(context.Background(),
			engine.Plan{Kind: "test", Target: "test", Steps: steps})
		if err != nil {
			t.Fatal(err)
		}
		return rep
	}

	t.Run("thành công: site về bản backup, DB về bản backup, wp-config giữ mật khẩu mới", func(t *testing.T) {
		f, x := bkMade(t)
		if rep := run(t, f, bkRestore(f, x)); rep.Job.Status != engine.JobSucceeded {
			t.Fatalf("%s %s", rep.Job.Status, rep.Job.Error)
		}
		snap := f.Snapshot()
		if snap[f.l.Public+"/index.php"].Data != "<?php // v1" || !strings.Contains(snap[f.l.WPConfig].Data, "passnew") {
			t.Error("site không về bản backup")
		}
		if f.dbNow() != "v1" {
			t.Errorf("DB = %q", f.dbNow())
		}
		if snap[site.RestoreDir(f.l)+"/old-public/index.php"].Data != "<?php // v2" {
			t.Error("mất bản cũ của site (core gỡ sau kiểm chứng)")
		}
	})

	t.Run("bước cuối lỗi: engine hoàn tác, máy về y như trước", func(t *testing.T) {
		f, x := bkMade(t)
		before := f.Snapshot()
		rep := run(t, f, append(bkRestore(f, x), bkBoom{}))
		if rep.Job.Status == engine.JobSucceeded {
			t.Fatal("muốn lỗi")
		}
		if f.dbNow() != "v2" {
			t.Errorf("DB = %q, muốn v2", f.dbNow())
		}
		after := f.Snapshot()
		if !reflect.DeepEqual(before, after) {
			for p := range after {
				if _, ok := before[p]; !ok {
					t.Errorf("thêm %s", p)
				}
			}
			for p, e := range before {
				if !reflect.DeepEqual(after[p], e) {
					t.Errorf("khác %s: %+v → %+v", p, e, after[p])
				}
			}
		}
	})
}
