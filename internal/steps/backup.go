package steps

// Backup và khôi phục (mục 6.5). Luật chung ở m9_types.go.
//
// Tài liệu đã tra (Ubuntu 24.04):
//   - https://manpages.ubuntu.com/manpages/noble/man1/tar.1.html (GNU tar
//     1.35: --no-same-owner, --no-same-permissions là mặc định của user
//     thường nhưng KHÔNG của root, nên phải ghi rõ; -C có tác dụng theo thứ
//     tự; mã thoát 1 = "some files differ" (file đổi trong lúc đọc), 2 = lỗi nặng)
//   - https://manpages.ubuntu.com/manpages/noble/man1/du.1.html (-b = byte
//     thật, -s, -x/--one-file-system, -P không theo symlink là mặc định)
//   - https://mariadb.com/kb/en/drop-database/ ("user privileges on the
//     database are not automatically dropped": GRANT trên DB còn sau khi xoá DB)
//   - https://mariadb.com/kb/en/mariadb-command-line-client/ (-e, source,
//     --abort-source-on-error; trang không nói mã thoát khi lỗi: kiểm trên máy ảo)
//   - Trang man mariadb-dump và mariadb của noble là của MySQL 8.0 (gói khác),
//     nên cờ của DBImport và MariaDBDump theo trang của MariaDB ở trên.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

var (
	_ engine.Step = (*BackupArchive)(nil)
	_ engine.Step = (*BackupExtract)(nil)
	_ engine.Step = (*DBImport)(nil)
)

// ================================================================ Đo chỗ trống

// SpaceOK là luật chặn đầy đĩa của mục 6.5: free ≥ need × 1,2 VÀ sau khi ghi
// vẫn còn ≥ 10% đĩa (free − need ≥ total / 10). Tính bằng số nguyên, làm
// tròn lên phần chia, để không bao giờ cho qua một ca sát nút mà luật không cho.
func SpaceOK(free, total, need uint64) bool {
	if free < need {
		return false
	}
	rem := free - need
	return rem >= ceilDiv(need, 5) && rem >= ceilDiv(total, 10)
}

func ceilDiv(a, b uint64) uint64 {
	q := a / b
	if a%b != 0 {
		q++
	}
	return q
}

// measureDBSQL hỏi cỡ DB. So bằng "=" (không LIKE: "_" của tên là ký tự đại
// diện trong LIKE). SUM trên không dòng nào là NULL, nên COALESCE về 0.
func measureDBSQL(name string) string {
	return "SELECT COALESCE(SUM(DATA_LENGTH + INDEX_LENGTH), 0) FROM information_schema.TABLES WHERE TABLE_SCHEMA='" + name + "';\n"
}

func measureDB(ctx context.Context, sys system.System, name string) (uint64, error) {
	if !dbNameRe.MatchString(name) {
		return 0, fmt.Errorf("tên DB %q có ký tự không cho phép", name)
	}
	res, err := sys.Run(ctx, mariadbCmd(measureDBSQL(name)))
	if err != nil {
		return 0, withStdout(fmt.Errorf("đo cỡ DB %s: %w", name, err), res)
	}
	n, err := strconv.ParseUint(strings.TrimSpace(res.Stdout), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("đo cỡ DB %s: kết quả lạ %q", name, res.Stdout)
	}
	return n, nil
}

// MeasureDB trả cỡ DB của site (DATA_LENGTH + INDEX_LENGTH trong
// information_schema), 0 khi DB chưa có bảng. Đây là ước lượng: file xuất
// .sql thường cỡ chừng đó, nên dùng làm "need" của luật chặn đầy đĩa.
func MeasureDB(ctx context.Context, sys system.System, l site.Layout) (uint64, error) {
	if err := dbCheckLayout(l); err != nil {
		return 0, err
	}
	return measureDB(ctx, sys, l.DB)
}

// statFSNear đo phân vùng sẽ chứa dir. Lần backup đầu dir chưa có, nên đo ở
// thư mục cha gần nhất đã có (thư mục sẽ được tạo trên chính phân vùng đó).
func statFSNear(sys system.System, dir string) (system.DiskUsage, error) {
	for d := dir; ; d = path.Dir(d) {
		_, err := sys.Stat(d)
		if err == nil {
			du, err := sys.StatFS(d)
			if err != nil {
				return du, fmt.Errorf("đo chỗ trống ở %s: %w", d, err)
			}
			return du, nil
		}
		if !errors.Is(err, fs.ErrNotExist) || d == "/" {
			return system.DiskUsage{}, fmt.Errorf("đo chỗ trống cho %s: %w", dir, err)
		}
	}
}

// duCmd đo cây public bằng byte thật (-b). --one-file-system: thứ được mount
// vào trong site không nằm trong backup (tar cũng chạy với cờ đó), nên không tính.
func duCmd(p string) system.Cmd {
	return system.Cmd{Name: "du", Args: []string{"-sb", "--one-file-system", "--", p}}
}

// backupSrcOK: Src chỉ có thể là thư mục site (active) hoặc chỗ của nó trong
// thùng rác; không nhận đường dẫn nào khác, vì tar chạy bằng root đọc được mọi thứ.
func backupSrcOK(l site.Layout, src string) error {
	if src != l.Dir && src != site.TrashDir(l) {
		return fmt.Errorf("thư mục nguồn %q không phải %s hay %s", src, l.Dir, site.TrashDir(l))
	}
	return nil
}

// backupPublic kiểm <src>/public là thư mục thật. Là symlink thì tar chỉ lưu
// cái link, và bản backup đó không bao giờ khôi phục được (BackupExtract đòi
// public/ là thư mục): từ chối ngay thay vì tạo ra một bản backup vô dụng.
func backupPublic(sys system.System, src string) (string, error) {
	p := src + "/public"
	fi, err := sys.Stat(p)
	if err != nil {
		return p, fmt.Errorf("kiểm %s: %w", p, err)
	}
	if !fi.Mode.IsDir() {
		return p, fmt.Errorf("%s không phải thư mục (%v); tpan không backup qua symlink", p, fi.Mode.Type())
	}
	return p, nil
}

// MeasureBackup đo cho luật chặn đầy đĩa: need = cỡ cây <src>/public (du) +
// cỡ DB; free, total của phân vùng chứa dir (hoặc thư mục cha gần nhất đã có).
func MeasureBackup(ctx context.Context, sys system.System, l site.Layout, src, dir string) (need, free, total uint64, err error) {
	if err := webCheckLayout(l); err != nil {
		return 0, 0, 0, err
	}
	if err := backupSrcOK(l, src); err != nil {
		return 0, 0, 0, err
	}
	if err := checkPath("thư mục backup", dir); err != nil {
		return 0, 0, 0, err
	}
	pub, err := backupPublic(sys, src)
	if err != nil {
		return 0, 0, 0, err
	}
	res, err := sys.Run(ctx, duCmd(pub))
	if err != nil {
		return 0, 0, 0, withStdout(fmt.Errorf("đo cỡ %s: %w", pub, err), res)
	}
	f := strings.Fields(res.Stdout)
	var files uint64
	if len(f) > 0 {
		files, err = strconv.ParseUint(f[0], 10, 64)
	}
	if len(f) == 0 || err != nil {
		return 0, 0, 0, fmt.Errorf("đo cỡ %s: kết quả lạ %q", pub, res.Stdout)
	}
	db, err := MeasureDB(ctx, sys, l)
	if err != nil {
		return 0, 0, 0, err
	}
	du, err := statFSNear(sys, dir)
	if err != nil {
		return 0, 0, 0, err
	}
	return files + db, du.Free, du.Total, nil
}

// spaceErr là lỗi của luật chặn đầy đĩa, kèm số đo để người đọc biết thiếu bao nhiêu.
func spaceErr(what string, need, free, total uint64) error {
	return fmt.Errorf("không đủ chỗ trống để %s: cần %d byte, đĩa trống %d / %d byte; luật là trống ≥ cần × 1,2 và sau khi ghi còn ≥ 10%% đĩa", what, need, free, total)
}

// dumpSpaceOK là chặn đầy đĩa của MariaDBDump một DB, gọi lúc Apply.
func dumpSpaceOK(ctx context.Context, sys system.System, db, dir string) error {
	need, err := measureDB(ctx, sys, db)
	if err != nil {
		return err
	}
	du, err := statFSNear(sys, dir)
	if err != nil {
		return err
	}
	if !SpaceOK(du.Free, du.Total, need) {
		return spaceErr("xuất DB "+db, need, du.Free, du.Total)
	}
	return nil
}

// ================================================================== Chung

var sha256Re = regexp.MustCompile(`^[0-9a-f]{64}$`)

func sha256Cmd(p string) system.Cmd {
	return system.Cmd{Name: "sha256sum", Args: []string{"--", p}}
}

// sha256Of chạy sha256sum thay vì ReadFile: file backup có thể vài GB, còn
// ReadFile giữ cả file trong bộ nhớ.
func sha256Of(ctx context.Context, sys system.System, p string) (string, error) {
	res, err := sys.Run(ctx, sha256Cmd(p))
	if err != nil {
		return "", withStdout(fmt.Errorf("tính sha256 của %s: %w", p, err), res)
	}
	f := strings.Fields(res.Stdout)
	if len(f) == 0 || !sha256Re.MatchString(f[0]) {
		return "", fmt.Errorf("tính sha256 của %s: kết quả lạ %q", p, res.Stdout)
	}
	return f[0], nil
}

// removeIfExists gỡ một file hay thư mục rỗng; không có thì thôi.
func removeIfExists(sys system.System, p string) error {
	if err := sys.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("gỡ %s: %w", p, err)
	}
	return nil
}

// backupMkdirs tạo dir (0700 root) như MariaDBDump: /var/lib/tpan giữ 0755 vì
// acme bên trong phải đi qua được; từ thư mục backup (hay restore) trở xuống 0700.
func backupMkdirs(sys system.System, root, dir string) error {
	if err := sys.MkdirAll(path.Dir(root), 0o755, system.Root); err != nil {
		return fmt.Errorf("tạo thư mục %s: %w", path.Dir(root), err)
	}
	if err := sys.MkdirAll(dir, 0o700, system.Root); err != nil {
		return fmt.Errorf("tạo thư mục %s: %w", dir, err)
	}
	return nil
}

// ================================================================ BackupArchive

// backupStamp là dạng giờ trong tên file backup, UTC.
const backupStamp = "20060102-150405"

// backupLater là chữ thay cho giờ trong Info: tên file chọn lúc Apply, mà
// Info phải giống hệt nhau giữa hai lần dựng Plan (so dấu vân tay lúc duyệt).
const backupLater = "<giờ lúc chạy>"

// wpVersionRe: bản WordPress lấy từ output của `wp` (mã PHP của site chạy,
// in gì cũng được), nên chỉ nhận dạng phiên bản trước khi ghi vào manifest.
var wpVersionRe = regexp.MustCompile(`^[0-9][0-9A-Za-z.-]{0,31}$`)

// backupManifest là manifest.json trong file backup.
type backupManifest struct {
	Domain     string `json:"domain"`
	WPVersion  string `json:"wp_version"`
	PHPVersion string `json:"php_version"`
	Created    string `json:"created"`
	DBSHA256   string `json:"db_sha256"`
}

// tarCreate: hai -C theo thứ tự, mỗi cái áp cho tên đứng sau nó. Không -h:
// symlink trong site được lưu là symlink, root không đi theo nó ra ngoài site.
func tarCreate(file, tmp, src string) system.Cmd {
	return system.Cmd{Name: "tar", Args: []string{
		"--create", "--gzip", "--file=" + file, "--one-file-system",
		"--directory=" + tmp, "manifest.json", "db.sql",
		"--directory=" + src, "public",
	}}
}

func (s *BackupArchive) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *BackupArchive) validate() error {
	l := s.Layout
	if err := webCheckLayout(l); err != nil {
		return err
	}
	if err := backupSrcOK(l, s.Src); err != nil {
		return err
	}
	if s.Dir != site.BackupDir(l) {
		return fmt.Errorf("thư mục backup %q không phải %s", s.Dir, site.BackupDir(l))
	}
	if !phpVersion.MatchString(s.PHPVersion) {
		return fmt.Errorf("bản PHP %q không đúng dạng", s.PHPVersion)
	}
	return nil
}

func (s *BackupArchive) Info() engine.StepInfo {
	l := s.Layout
	tmp := s.Dir + "/.tmp-" + backupLater
	file := s.Dir + "/" + l.Domain.String() + "-" + backupLater + ".tar.gz"
	dump := &MariaDBDump{Path: tmp + "/db.sql", DB: l.DB}
	return engine.StepInfo{
		ID:    "backup.create:" + l.Domain.String(),
		Title: i18n.T("Backup site "+l.Domain.String()+" vào "+s.Dir, "Back up site "+l.Domain.String()+" to "+s.Dir),
		Why: i18n.T(
			"Một file gồm toàn bộ mã nguồn, file tải lên và DB của site, kèm sha256 để biết file còn nguyên. Không có nó thì lỗi, xoá nhầm hay nâng cấp hỏng không còn đường lùi.",
			"One file holding all of the site's code, uploads and database, with a sha256 to tell the file is intact. Without it, a mistake, a wrong delete or a broken upgrade has no way back.",
		),
		Commands: []string{
			duCmd(s.Src + "/public").String(),
			dbShow(measureDBSQL(l.DB)),
			dump.cmd().String(),
			wpRun(l, nil, "core", "version").String(),
			tarCreate(file+".part", tmp, s.Src).String(),
			sha256Cmd(file).String(),
		},
	}
}

// Check chỉ true sau Apply của chính bước này: backup luôn là một bản mới,
// "đã có một file backup nào đó" không phải trạng thái đích.
func (s *BackupArchive) Check(_ context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	if !s.applied {
		return false, nil
	}
	if _, err := sys.Stat(s.Path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("kiểm %s: %w", s.Path, err)
	}
	return true, nil
}

func (s *BackupArchive) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	l := s.Layout
	need, free, total, err := MeasureBackup(ctx, sys, l, s.Src, s.Dir)
	if err != nil {
		return err
	}
	// Đo lại lúc Apply: core đã đo lúc dựng Plan, nhưng Plan chờ duyệt có thể
	// chạy sau đó hàng giờ, và đĩa có thể đã đầy thêm.
	if !SpaceOK(free, total, need) {
		return spaceErr("backup "+l.Domain.String(), need, free, total)
	}

	now := s.clock().UTC()
	stamp := now.Format(backupStamp)
	final := s.Dir + "/" + l.Domain.String() + "-" + stamp + ".tar.gz"
	part := final + ".part"
	tmp := s.Dir + "/.tmp-" + stamp
	for _, p := range []string{final, part, tmp} {
		if _, err := sys.Stat(p); err == nil {
			return fmt.Errorf("%s đã có (hai bản backup trong cùng một giây?); chờ một giây rồi chạy lại", p)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("kiểm %s: %w", p, err)
		}
	}
	if err := backupMkdirs(sys, site.BackupRoot, s.Dir); err != nil {
		return err
	}
	if err := sys.MkdirAll(tmp, 0o700, system.Root); err != nil {
		return fmt.Errorf("tạo thư mục tạm %s: %w", tmp, err)
	}
	manifest, dbFile := tmp+"/manifest.json", tmp+"/db.sql"
	// cleanup gỡ thư mục tạm (và .part khi lỗi). Tên trong đó do chính bước
	// này đặt, nên gỡ từng file bằng Remove, không cần rm -r.
	cleanup := func(alsoPart bool) error {
		var errs []error
		ps := []string{manifest, dbFile, tmp}
		if alsoPart {
			ps = append(ps, part)
		}
		for _, p := range ps {
			if err := removeIfExists(sys, p); err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	}
	fail := func(err error) error { return errors.Join(err, cleanup(true)) }

	if err := (&MariaDBDump{Path: dbFile, DB: l.DB}).write(ctx, sys); err != nil {
		return fail(err)
	}
	dbSum, err := sha256Of(ctx, sys, dbFile)
	if err != nil {
		return fail(err)
	}
	// Site trong thùng rác: thư mục site nằm dưới thùng rác 0700 root, user
	// của site không vào được, `wp` chắc chắn lỗi; bỏ qua, manifest để trống.
	wpVer := ""
	if s.Src == l.Dir {
		if res, err := sys.Run(ctx, wpRun(l, nil, "core", "version")); err == nil {
			if v := strings.TrimSpace(res.Stdout); wpVersionRe.MatchString(v) {
				wpVer = v
			}
		}
	}
	data, err := json.MarshalIndent(backupManifest{
		Domain:     l.Domain.String(),
		WPVersion:  wpVer,
		PHPVersion: s.PHPVersion,
		Created:    now.Format(time.RFC3339),
		DBSHA256:   dbSum,
	}, "", "  ")
	if err != nil {
		return fail(fmt.Errorf("dựng manifest.json: %w", err))
	}
	if err := sys.WriteFileAtomic(manifest, append(data, '\n'), 0o600, system.Root); err != nil {
		return fail(fmt.Errorf("ghi %s: %w", manifest, err))
	}
	// .part tạo trước với 0600: tar mở file có sẵn để ghi đè nên giữ quyền
	// này; tên thật chỉ xuất hiện khi file đã trọn vẹn.
	if err := sys.WriteFileAtomic(part, nil, 0o600, system.Root); err != nil {
		return fail(fmt.Errorf("tạo %s: %w", part, err))
	}
	if res, err := sys.Run(ctx, tarCreate(part, tmp, s.Src)); err != nil {
		// Mã 1: một file đổi trong lúc tar đọc (WordPress đang chạy, ví dụ
		// file cache). Bản backup vẫn dùng được; tar đã in tên file vào
		// output của job. Mã khác là lỗi thật.
		if ee, ok := isExit(err); !ok || ee.Code != 1 {
			return fail(withStdout(fmt.Errorf("nén backup %s: %w", part, err), res))
		}
	}
	if err := setPermOwner(sys, part, 0o600, system.Root); err != nil {
		return fail(err)
	}
	if err := sys.Rename(part, final); err != nil {
		return fail(fmt.Errorf("đổi tên %s thành %s: %w", part, final, err))
	}
	failFinal := func(err error) error {
		return errors.Join(err, removeIfExists(sys, final), cleanup(false))
	}
	sum, err := sha256Of(ctx, sys, final)
	if err != nil {
		return failFinal(err)
	}
	fi, err := sys.Stat(final)
	if err != nil {
		return failFinal(fmt.Errorf("đọc cỡ %s: %w", final, err))
	}
	if err := cleanup(false); err != nil {
		return failFinal(err)
	}
	s.Path, s.Size, s.SHA256, s.WPVersion = final, fi.Size, sum, wpVer
	s.applied = true
	return nil
}

// Undo xoá file vừa tạo: bước sau lỗi thì bản backup này không thuộc job nào
// thành công, core không ghi dòng cho nó, để lại thì thành file mồ côi.
func (s *BackupArchive) Undo(_ context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("backup %s: %w", s.Layout.Domain, errNotApplied)
	}
	if err := removeIfExists(sys, s.Path); err != nil {
		return err
	}
	s.applied = false
	return nil
}

// ================================================================ BackupExtract

func tarList(archive string) system.Cmd {
	return system.Cmd{Name: "tar", Args: []string{"--list", "--verbose", "--gzip", "--file=" + archive}}
}

// tarExtract: root mặc định giữ chủ và quyền trong header (--same-owner,
// --same-permissions), nên phải tắt rõ ràng; chủ và quyền đặt lại sau.
func tarExtract(archive, dir string) system.Cmd {
	return system.Cmd{Name: "tar", Args: []string{
		"--extract", "--gzip", "--file=" + archive, "--directory=" + dir,
		"--no-same-owner", "--no-same-permissions",
	}}
}

// Chuẩn hoá chủ và quyền trong cây đã giải nén. chown -R mặc định không đi
// theo symlink (-P), -h đổi chính cái link. find -P (mặc định) không đi theo
// symlink, -type d/f bỏ qua link, nên chmod chỉ chạm vào thư mục và file thật
// nằm trong Dir (Dir do root giữ, không ai tráo được giữa chừng).
func extractChown(l site.Layout, pub string) system.Cmd {
	return system.Cmd{Name: "chown", Args: []string{"-R", "-h", l.User + ":" + site.WebGroup, "--", pub}}
}

func extractChmod(pub, typ, mode string) system.Cmd {
	return system.Cmd{Name: "find", Args: []string{"-P", pub, "-type", typ, "-exec", "chmod", mode, "{}", "+"}}
}

// wpConfigMax: wp-config.php thật chỉ vài KB. Lớn hơn là thứ khác.
const wpConfigMax = 1 << 20

// wpDefine lấy giá trị của define('NAME', 'giá trị') trong wp-config.php.
func wpDefine(data []byte, name string) string {
	re := regexp.MustCompile(`define\(\s*['"]` + name + `['"]\s*,\s*['"]([^'"]*)['"]\s*\)`)
	m := re.FindSubmatch(data)
	if m == nil {
		return ""
	}
	return string(m[1])
}

// currentWPConfig đọc wp-config.php đang chạy của site để đặt vào bản khôi
// phục. ok=false: site không có file đó (giữ bản trong backup).
//
// Hợp đồng ghi "đọc bằng cat dưới user của site". Không làm vậy: mọi stdout
// của Run vào log job (engine.recSys), tức là mật khẩu DB và các khoá muối
// nằm trong job_events. Đọc bằng ReadFile như DBPassword (db_m9.go), với ba
// lớp chặn cho việc root đọc một đường dẫn do user của site kiểm soát:
//   - l.Dir, l.Public là thư mục thật, wp-config.php là file thường (lstat);
//   - cỡ ≤ wpConfigMax;
//   - nội dung phải có DB_NAME và DB_USER đúng tên DB của site. Lỡ file bị
//     tráo thành symlink giữa lúc lstat và lúc đọc, thứ đọc được (/etc/shadow,
//     wp-config của site khác) không mang tên DB này nên bị từ chối.
func currentWPConfig(sys system.System, l site.Layout) ([]byte, bool, error) {
	for _, d := range []string{l.Dir, l.Public} {
		fi, err := sys.Stat(d)
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, fmt.Errorf("kiểm %s: %w", d, err)
		}
		if !fi.Mode.IsDir() {
			return nil, false, fmt.Errorf("%s không phải thư mục (%v); tpan không đọc wp-config.php qua symlink", d, fi.Mode.Type())
		}
	}
	p := l.WPConfig
	fi, err := sys.Stat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("kiểm %s: %w", p, err)
	}
	if !fi.Mode.IsRegular() {
		return nil, false, fmt.Errorf("%s không phải file thường (%v); sửa bằng tay rồi khôi phục lại", p, fi.Mode.Type())
	}
	if fi.Size > wpConfigMax {
		return nil, false, fmt.Errorf("%s lớn bất thường (%d byte)", p, fi.Size)
	}
	data, err := sys.ReadFile(p)
	if err != nil {
		return nil, false, fmt.Errorf("đọc %s: %w", p, err)
	}
	if wpDefine(data, "DB_NAME") != l.DB || wpDefine(data, "DB_USER") != l.DB {
		return nil, false, fmt.Errorf("%s không có DB_NAME và DB_USER là %s; không dùng nó cho bản khôi phục", p, l.DB)
	}
	return data, true, nil
}

// tarMember là một dòng của `tar --list --verbose`.
type tarMember struct {
	typ          byte // ký tự đầu của cột quyền: '-', 'd', 'l', 'h', 'c', 'b', 'p'...
	name, target string
}

// parseTarLine đọc một dòng dạng GNU tar 1.35:
//
//	-rw-r--r-- root/root      1234 2026-11-19 10:00 public/index.php
//	lrwxrwxrwx root/root         0 2026-11-19 10:00 public/a -> b
//	hrw-r--r-- root/root         0 2026-11-19 10:00 public/c link to public/d
//
// Năm cột đầu cách nhau bằng khoảng trắng (cột cỡ được canh phải), rồi đúng
// một dấu cách, rồi tên. Dòng không đúng dạng thì lỗi (từ chối, không đoán).
func parseTarLine(line string) (tarMember, error) {
	var f [5]string
	rest := line
	for i := range f {
		rest = strings.TrimLeft(rest, " ")
		j := strings.IndexByte(rest, ' ')
		if j <= 0 {
			return tarMember{}, fmt.Errorf("dòng danh sách lạ %q", line)
		}
		f[i], rest = rest[:j], rest[j+1:]
	}
	if len(f[0]) != 10 || !strings.Contains(f[1], "/") || len(f[3]) != 10 || f[3][4] != '-' || !strings.Contains(f[4], ":") || rest == "" {
		return tarMember{}, fmt.Errorf("dòng danh sách lạ %q", line)
	}
	m := tarMember{typ: f[0][0], name: rest}
	var ok bool
	switch m.typ {
	case 'l':
		m.name, m.target, ok = strings.Cut(rest, " -> ")
	case 'h':
		m.name, m.target, ok = strings.Cut(rest, " link to ")
	default:
		ok = true
	}
	if !ok {
		return tarMember{}, fmt.Errorf("dòng danh sách lạ %q", line)
	}
	return m, nil
}

// memberSafe: tên tương đối, không có thành phần "..".
func memberSafe(name string) bool {
	if name == "" || strings.HasPrefix(name, "/") {
		return false
	}
	for _, c := range strings.Split(name, "/") {
		if c == ".." {
			return false
		}
	}
	return true
}

// checkTarList là lớp chặn thứ hai sau chính GNU tar (nó đã bỏ "/" đầu và từ
// chối ".."): chỉ nhận đúng dạng tpan tạo ra.
func checkTarList(out string) error {
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		m, err := parseTarLine(line)
		if err != nil {
			return err
		}
		if !memberSafe(m.name) {
			return fmt.Errorf("thành viên %q là đường dẫn tuyệt đối hoặc có \"..\"", m.name)
		}
		if seen[m.name] {
			return fmt.Errorf("thành viên %q có hai lần", m.name)
		}
		seen[m.name] = true
		switch {
		case m.name == "manifest.json" || m.name == "db.sql":
			if m.typ != '-' {
				return fmt.Errorf("%s phải là file thường", m.name)
			}
		case m.name == "public/":
			if m.typ != 'd' {
				return fmt.Errorf("public/ phải là thư mục")
			}
		case strings.HasPrefix(m.name, "public/"):
		default:
			return fmt.Errorf("thành viên %q nằm ngoài manifest.json, db.sql, public/", m.name)
		}
		switch m.typ {
		case '-', 'd', 'l':
		case 'h':
			// Link cứng trỏ tới một thành viên khác: phải cũng nằm trong public/.
			if !memberSafe(m.target) || !strings.HasPrefix(m.target, "public/") {
				return fmt.Errorf("link cứng %q trỏ ra ngoài public/ (%q)", m.name, m.target)
			}
		default:
			return fmt.Errorf("thành viên %q có loại %q (thiết bị, FIFO...) không được nhận", m.name, string(m.typ))
		}
	}
	for _, n := range []string{"manifest.json", "db.sql", "public/"} {
		if !seen[n] {
			return fmt.Errorf("thiếu %s", n)
		}
	}
	return nil
}

func (s *BackupExtract) validate() error {
	l := s.Layout
	if err := webCheckLayout(l); err != nil {
		return err
	}
	if err := wpCheckLayout(l); err != nil {
		return err
	}
	if err := checkPath("file backup", s.Archive); err != nil {
		return err
	}
	if !strings.HasPrefix(s.Archive, site.BackupDir(l)+"/") {
		return fmt.Errorf("file backup %q không nằm dưới %s", s.Archive, site.BackupDir(l))
	}
	if !sha256Re.MatchString(s.SHA256) {
		return fmt.Errorf("sha256 %q không đúng dạng (64 ký tự hex thường)", s.SHA256)
	}
	if s.Dir != site.RestoreDir(l) {
		return fmt.Errorf("thư mục giải nén %q không phải %s", s.Dir, site.RestoreDir(l))
	}
	return nil
}

func (s *BackupExtract) remover() *RemoveTree {
	return &RemoveTree{Path: s.Dir, Under: site.RestoreRoot}
}

func (s *BackupExtract) Info() engine.StepInfo {
	l, pub := s.Layout, s.Dir+"/public"
	return engine.StepInfo{
		ID:    "backup.extract:" + l.Domain.String(),
		Title: i18n.T("Giải nén "+s.Archive+" vào "+s.Dir, "Extract "+s.Archive+" into "+s.Dir),
		Why: i18n.T(
			"Giải nén ra một thư mục riêng chỉ root vào được, kiểm và đặt lại chủ, quyền ở đó, rồi mới tráo vào site. Giải nén thẳng đè lên site đang chạy thì lỗi giữa chừng để lại một site nửa cũ nửa mới, và file lạ trong backup có thể ghi ra ngoài site.",
			"The backup is extracted into a separate directory only root can enter, checked, and given the right owner and modes there before being swapped into the site. Extracting straight over the running site would leave it half old, half new on any error, and a crafted backup could write outside the site.",
		),
		Commands: []string{
			sha256Cmd(s.Archive).String(),
			tarList(s.Archive).String(),
			tarExtract(s.Archive, s.Dir).String(),
			extractChown(l, pub).String(),
			extractChmod(pub, "d", "2750").String(),
			extractChmod(pub, "f", "0640").String(),
		},
	}
}

func (s *BackupExtract) Check(_ context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	if !s.applied {
		return false, nil
	}
	for _, p := range []string{s.Dir + "/public", s.Dir + "/db.sql"} {
		if _, err := sys.Stat(p); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return false, nil
			}
			return false, fmt.Errorf("kiểm %s: %w", p, err)
		}
	}
	return true, nil
}

func (s *BackupExtract) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	l := s.Layout
	fi, err := sys.Stat(s.Archive)
	if err != nil {
		return fmt.Errorf("kiểm file backup %s: %w", s.Archive, err)
	}
	if !fi.Mode.IsRegular() {
		return fmt.Errorf("file backup %s không phải file thường (%v)", s.Archive, fi.Mode.Type())
	}
	sum, err := sha256Of(ctx, sys, s.Archive)
	if err != nil {
		return err
	}
	if sum != s.SHA256 {
		return fmt.Errorf("backup hỏng: sha256 của %s là %s, khác %s đã ghi lúc tạo", s.Archive, sum, s.SHA256)
	}
	res, err := sys.Run(ctx, tarList(s.Archive))
	if err != nil {
		return withStdout(fmt.Errorf("đọc danh sách trong %s: %w", s.Archive, err), res)
	}
	if err := checkTarList(res.Stdout); err != nil {
		return fmt.Errorf("từ chối backup %s: %w", s.Archive, err)
	}

	if _, err := sys.Stat(s.Dir); err == nil {
		return fmt.Errorf("%s còn sót từ một lần khôi phục trước; xem bên trong (có thể còn old-public của site), rồi xoá bằng tay: rm -r --one-file-system -- %s", s.Dir, s.Dir)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("kiểm %s: %w", s.Dir, err)
	}
	created, err := missingDirs(sys, s.Dir)
	if err != nil {
		return err
	}
	if err := backupMkdirs(sys, site.RestoreRoot, s.Dir); err != nil {
		return errors.Join(err, removeDirs(sys, created))
	}
	s.parents = created[:len(created)-1]
	fail := func(err error) error { return errors.Join(err, s.cleanup(ctx, sys)) }

	if res, err := sys.Run(ctx, tarExtract(s.Archive, s.Dir)); err != nil {
		return fail(withStdout(fmt.Errorf("giải nén %s: %w", s.Archive, err), res))
	}
	for _, c := range []struct {
		p   string
		dir bool
	}{{s.Dir + "/public", true}, {s.Dir + "/db.sql", false}, {s.Dir + "/manifest.json", false}} {
		fi, err := sys.Stat(c.p)
		if err != nil {
			return fail(fmt.Errorf("sau khi giải nén: kiểm %s: %w", c.p, err))
		}
		if c.dir != fi.Mode.IsDir() || (!c.dir && !fi.Mode.IsRegular()) {
			return fail(fmt.Errorf("sau khi giải nén: %s không đúng loại (%v)", c.p, fi.Mode.Type()))
		}
	}
	data, err := sys.ReadFile(s.Dir + "/manifest.json")
	if err != nil {
		return fail(fmt.Errorf("đọc manifest.json: %w", err))
	}
	var man backupManifest
	if err := json.Unmarshal(data, &man); err != nil {
		return fail(fmt.Errorf("đọc manifest.json: %w", err))
	}
	if man.Domain != l.Domain.String() {
		return fail(fmt.Errorf("backup này của %q, không phải của %s", man.Domain, l.Domain))
	}

	pub := s.Dir + "/public"
	for _, c := range []system.Cmd{extractChown(l, pub), extractChmod(pub, "d", "2750"), extractChmod(pub, "f", "0640")} {
		if res, err := sys.Run(ctx, c); err != nil {
			return fail(withStdout(fmt.Errorf("đặt chủ và quyền trong %s: %w", pub, err), res))
		}
	}

	// wp-config.php đặt sau cùng để lệnh chmod 640 ở trên không đè lên 600.
	// Mật khẩu DB có thể đã đổi sau lúc backup (mục 6.9) mà nhập DB không đổi
	// mật khẩu user DB, nên dùng bản đang chạy của site.
	cfg := pub + "/wp-config.php"
	cur, ok, err := currentWPConfig(sys, l)
	if err != nil {
		return fail(err)
	}
	if ok {
		// Cùng chủ và nhóm như mọi file khác trong public (extractChown): thư
		// mục site có setgid nên file WP-CLI tạo lúc dựng site mang nhóm
		// www-data, không phải nhóm riêng của user (e2e M9 đo được).
		if err := sys.WriteFileAtomic(cfg, cur, wpConfigMode, system.Owner{User: l.User, Group: site.WebGroup}); err != nil {
			return fail(fmt.Errorf("ghi %s: %w", cfg, err))
		}
	} else if fi, err := sys.Stat(cfg); err == nil && fi.Mode.IsRegular() {
		// Dir do root giữ: không ai tráo file này thành symlink giữa lstat và
		// chmod, nên dùng sys.Chmod (đi theo symlink) được.
		if err := sys.Chmod(cfg, wpConfigMode); err != nil {
			return fail(fmt.Errorf("đặt quyền 600 cho %s: %w", cfg, err))
		}
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fail(fmt.Errorf("kiểm %s: %w", cfg, err))
	}
	s.applied = true
	return nil
}

// cleanup gỡ Dir (rm -r, chỉ dưới site.RestoreRoot) và các thư mục cha bước
// này đã tạo. Thư mục cha còn đồ khác (ENOTEMPTY) thì để lại.
func (s *BackupExtract) cleanup(ctx context.Context, sys system.System) error {
	if _, err := sys.Stat(s.Dir); err == nil {
		if err := s.remover().Apply(ctx, sys); err != nil {
			return err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("kiểm %s: %w", s.Dir, err)
	}
	if err := removeDirs(sys, s.parents); err != nil &&
		!errors.Is(err, syscall.ENOTEMPTY) && !errors.Is(err, syscall.EEXIST) {
		return err
	}
	s.parents = nil
	return nil
}

func (s *BackupExtract) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("giải nén %s: %w", s.Archive, errNotApplied)
	}
	if err := s.validate(); err != nil {
		return err
	}
	if err := s.cleanup(ctx, sys); err != nil {
		return err
	}
	s.applied = false
	return nil
}

// ===================================================================== DBImport

// importPathRe: đường dẫn đi vào câu `source <File>` của client mariadb, nơi
// không có cách thoát ký tự; chỉ nhận đúng các ký tự tpan dùng khi đặt tên.
var importPathRe = regexp.MustCompile(`^[A-Za-z0-9_./-]+$`)

func importPathOK(what, p string) error {
	if err := checkPath(what, p); err != nil {
		return err
	}
	if !importPathRe.MatchString(p) {
		return fmt.Errorf("%s %q có ký tự không cho phép", what, p)
	}
	if !strings.HasPrefix(p, site.BackupRoot+"/") && !strings.HasPrefix(p, site.RestoreRoot+"/") {
		return fmt.Errorf("%s %q không nằm dưới %s hoặc %s", what, p, site.BackupRoot, site.RestoreRoot)
	}
	return nil
}

// importResetSQL xoá rồi tạo lại DB: sau khi nhập DB chỉ có đúng các bảng
// của file (bảng do plugin tạo sau lúc xuất không còn sót). GRANT trên DB
// không mất khi xoá DB (xem đầu file), nên user DB vẫn dùng được.
func importResetSQL(db string) string { return dbDropDBSQL(db) + dbCreateDBSQL(db) }

// importCmd: file nằm trên dòng lệnh (không bí mật). --abort-source-on-error:
// lỗi giữa file thì dừng, không nhập tiếp nửa sau.
func importCmd(db, file string) system.Cmd {
	return system.Cmd{Name: "mariadb", Args: []string{"--database=" + db, "--abort-source-on-error", "-e", "source " + file}}
}

func (s *DBImport) validate() error {
	if err := dbCheckLayout(s.Layout); err != nil {
		return err
	}
	if err := importPathOK("file nhập", s.File); err != nil {
		return err
	}
	if err := importPathOK("file hoàn tác", s.UndoFile); err != nil {
		return err
	}
	if s.File == s.UndoFile {
		return fmt.Errorf("file nhập và file hoàn tác trùng nhau (%s)", s.File)
	}
	return nil
}

// ready: file là bản xuất trọn vẹn (file thường, dòng cuối "-- Dump completed").
func (s *DBImport) ready(ctx context.Context, sys system.System, p string) error {
	ok, err := (&MariaDBDump{Path: p, DB: s.Layout.DB}).complete(ctx, sys)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%s không có hoặc không phải bản xuất trọn vẹn (thiếu dòng %q)", p, dumpDone)
	}
	return nil
}

func (s *DBImport) load(ctx context.Context, sys system.System, file string) error {
	db := s.Layout.DB
	if err := dbRun(ctx, sys, importResetSQL(db), "", "xoá và tạo lại DB "+db); err != nil {
		return err
	}
	if res, err := sys.Run(ctx, importCmd(db, file)); err != nil {
		return withStdout(fmt.Errorf("nhập %s vào DB %s: %w", file, db, err), res)
	}
	return nil
}

func (s *DBImport) Info() engine.StepInfo {
	l := s.Layout
	return engine.StepInfo{
		ID:    "db.import:" + l.Domain.String(),
		Title: i18n.T("Nhập "+s.File+" vào DB "+l.DB, "Import "+s.File+" into database "+l.DB),
		Why: i18n.T(
			"Đưa DB của site về đúng nội dung của bản xuất. DB được xoá và tạo lại trước, để không còn bảng nào ngoài các bảng trong file. Hoàn tác là nhập lại "+s.UndoFile+", bản xuất ngay trước bước này.",
			"Returns the site's database to exactly the contents of the dump. The database is dropped and recreated first, so no table outside the file is left behind. Undo imports "+s.UndoFile+", the dump taken just before this step.",
		),
		Commands: []string{dbShow(importResetSQL(l.DB)), importCmd(l.DB, s.File).String()},
		Danger:   true,
	}
}

func (s *DBImport) Check(_ context.Context, _ system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	return s.applied, nil
}

func (s *DBImport) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	// Kiểm cả hai file TRƯỚC khi xoá DB: không có đường lùi thì không xoá gì.
	for _, p := range []string{s.UndoFile, s.File} {
		if err := s.ready(ctx, sys, p); err != nil {
			return fmt.Errorf("nhập DB %s: %w", s.Layout.DB, err)
		}
	}
	if err := s.load(ctx, sys, s.File); err != nil {
		// Engine không Undo bước có Apply lỗi: tự nhập lại bản trước đó.
		if rerr := s.load(ctx, sys, s.UndoFile); rerr != nil {
			return errors.Join(err, fmt.Errorf("trả DB về %s: %w", s.UndoFile, rerr))
		}
		return err
	}
	s.applied = true
	return nil
}

func (s *DBImport) Undo(ctx context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("nhập DB %s: %w", s.Layout.DB, errNotApplied)
	}
	if err := s.validate(); err != nil {
		return err
	}
	if err := s.load(ctx, sys, s.UndoFile); err != nil {
		return fmt.Errorf("hoàn tác nhập DB: %w", err)
	}
	s.applied = false
	return nil
}

// ================================================================== Kiểu

// BackupArchive tạo một bản backup đầy đủ của site (mục 6.5): một file
// <Dir>/<domain>-<YYYYMMDD-HHMMSS>.tar.gz (giờ UTC LÚC APPLY, không lúc dựng
// Plan) gồm:
//
//	manifest.json  {"domain","wp_version","php_version","created","db_sha256"}
//	db.sql         mariadb-dump --single-transaction --quick <db> (--result-file)
//	public/...     cây Src/public (tar, không -h)
//
// Src là thư mục site: l.Dir khi site active, site.TrashDir(l) khi site ở
// thùng rác. Dir là site.BackupDir(l) (root 0700; BackupRoot 0700;
// /var/lib/tpan giữ 0755).
//
// Apply: đo lại chỗ trống (MeasureBackup, SpaceOK) và báo lỗi nếu không đủ
// (điều kiện có thể đã đổi từ lúc dựng Plan); xuất DB và ghi manifest vào một
// thư mục tạm <Dir>/.tmp-<giờ>/ (0700); `wp core version` dưới user của site
// (lỗi thì WPVersion rỗng, không làm hỏng backup); tar ra <file>.part (0600)
// rồi rename thành tên thật; sha256sum; xoá thư mục tạm. Lỗi giữa chừng: gỡ
// .part và thư mục tạm. GNU tar thoát 1 khi một file đổi trong lúc đọc
// (WordPress đang chạy): chấp nhận, ghi cảnh báo vào output, không coi là lỗi.
// Sau Apply, các trường Path, Size, SHA256, WPVersion có giá trị.
// Check: chỉ true sau khi Apply của chính bước này thành công (backup luôn
// là bản mới). Undo: xoá file đã tạo. Info: Commands có lệnh xuất DB và tar
// với tên file dạng "<domain>-<giờ lúc chạy>.tar.gz" (chữ cố định, để bản xem
// trước dựng hai lần giống hệt nhau).
type BackupArchive struct {
	Layout     site.Layout
	Src        string
	Dir        string
	PHPVersion string

	// Kết quả, có sau Apply.
	Path      string
	Size      int64
	SHA256    string
	WPVersion string

	now     func() time.Time // nil: time.Now; test đặt giờ cố định
	applied bool
}

// BackupExtract giải nén một bản backup đầy đủ vào Dir = site.RestoreDir(l)
// để chuẩn bị tráo vào site (mục 6.5, khôi phục). Apply:
//  1. sha256sum Archive phải bằng SHA256; khác thì lỗi "backup hỏng".
//  2. `tar -tzvf` (danh sách): chỉ manifest.json, db.sql, public/...; không
//     tuyệt đối, không "..", không thiết bị hay FIFO.
//  3. Dir chưa được có (còn sót từ lần trước: lỗi, nói rõ cách dọn); tạo Dir
//     root 0700 (RestoreRoot 0700); `tar -xzf --no-same-owner
//     --no-same-permissions -C Dir`.
//  4. manifest.json.domain phải bằng Layout.Domain.
//  5. wp-config.php: thay bản trong backup bằng bản đang chạy của site (ghi
//     bằng WriteFileAtomic vào Dir, 0600): mật khẩu DB có thể đã đổi sau lúc
//     backup (mục 6.9), và DB import không đổi mật khẩu user DB. Site không có
//     wp-config.php thì giữ bản trong backup. Đọc bằng ReadFile có kiểm, KHÔNG
//     bằng `cat` như hợp đồng ghi: lý do ở currentWPConfig.
//  6. Chuẩn hoá trong Dir/public (Dir do root giữ nên không bị tráo):
//     `chown -R -h <user>:www-data`, thư mục 2750, file 640, wp-config.php 600.
//
// Check: Dir/public và Dir/db.sql đã có VÀ bước này đã Apply. Undo: gỡ Dir
// (rm -r --one-file-system, chỉ khi nằm dưới site.RestoreRoot).
type BackupExtract struct {
	Layout  site.Layout
	Archive string
	SHA256  string
	Dir     string

	applied bool
	parents []string // thư mục cha của Dir mà Apply đã tạo, từ ngoài vào trong
}

// DBImport nhập File (bản xuất của mariadb-dump) vào DB của site sao cho sau
// đó DB chỉ có đúng các bảng của File: xoá rồi tạo lại DB (utf8mb4) trước khi
// nhập; tài khoản user DB nằm ở mysql.global_priv và GRANT trên DB ở mysql.db,
// cả hai không mất khi xoá DB (xem đầu file).
// Lệnh: `mariadb --database=<db>` với câu `source <File>` (đường dẫn do tpan
// sinh, chỉ [A-Za-z0-9_./-]; kiểm trước). Undo: làm y như vậy với UndoFile
// (bản xuất ngay trước khi nhập). Check: chỉ true sau Apply của chính bước.
// File và UndoFile phải nằm dưới site.BackupRoot hoặc site.RestoreRoot.
//
// Apply kiểm cả File lẫn UndoFile là bản xuất trọn vẹn trước khi xoá DB;
// nhập File lỗi thì tự nhập lại UndoFile rồi mới báo lỗi (engine không Undo
// bước có Apply lỗi).
type DBImport struct {
	Layout   site.Layout
	File     string
	UndoFile string

	applied bool
}
