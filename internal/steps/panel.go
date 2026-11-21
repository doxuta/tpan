package steps

// Gói panel đã mã hoá (M18, mục 3.8 của docs/thay-doi/M18-thiet-ke.md). Hợp
// đồng ở m18_types.go.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"time"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

var _ engine.Step = (*PanelBundle)(nil)

// panelManifest là manifest.json trong gói panel. Migrations là CẢ danh sách
// (S4): các mốc vào dev không theo thứ tự số, nên "số lớn nhất" không đủ để
// biết một bản tpan có đọc được tpan.db này không.
type panelManifest struct {
	Format      int    `json:"format"`
	Created     string `json:"created"`
	Hostname    string `json:"hostname"`
	TPANVersion string `json:"tpan_version"`
	Migrations  []int  `json:"migrations"`
	DBSHA256    string `json:"db_sha256"`
}

// panelTar nén hai file trong thư mục tạm. Như tarCreate: argv, không shell,
// --directory áp cho các tên đứng sau nó.
func panelTar(file, tmp string) system.Cmd {
	return system.Cmd{Name: "tar", Args: []string{
		"--create", "--gzip", "--file=" + file,
		"--directory=" + tmp, "manifest.json", "tpan.db",
	}}
}

func (s *PanelBundle) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *PanelBundle) validate() error {
	if s.Dir != site.TPANDBBackupDir {
		return fmt.Errorf("thư mục gói panel %q không phải %s", s.Dir, site.TPANDBBackupDir)
	}
	if s.Snapshot == nil || s.Migrations == nil || s.Encrypt == nil {
		return errors.New("tạo gói panel: thiếu Snapshot, Migrations hoặc Encrypt")
	}
	return nil
}

func (s *PanelBundle) Info() engine.StepInfo {
	tmp := s.Dir + "/.tmp-" + backupLater
	return engine.StepInfo{
		ID:    "panel.bundle",
		Title: i18n.T("Tạo gói panel đã mã hoá trong "+s.Dir, "Create the encrypted panel bundle in "+s.Dir),
		Why: i18n.T(
			"Gói này mang tpan.db (danh sách site, user, gói, cài đặt và sha256 của mọi bản sao lưu), mã hoá bằng khoá sao lưu. Không có nó thì mất máy là mất cả bảng ghi đó, dù file của site vẫn còn trên kho. Bản rõ của tpan.db chỉ nằm trong một thư mục tạm của root và bị xoá ngay sau khi mã hoá.",
			"This bundle carries tpan.db (sites, users, plans, settings and the sha256 of every backup), encrypted with the backup key. Without it, losing the machine loses that record even though the site files are still on the store. The plain tpan.db only sits in a root-only temporary directory and is deleted right after encryption.",
		),
		Commands: []string{
			sha256Cmd(tmp + "/tpan.db").String(),
			panelTar(tmp+"/panel.tar.gz", tmp).String(),
			sha256Cmd(s.Dir + "/" + PanelNameLater).String(),
		},
	}
}

// Check chỉ đúng sau Apply: mỗi lần là một gói mới.
func (s *PanelBundle) Check(_ context.Context, sys system.System) (bool, error) {
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

func (s *PanelBundle) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	now := s.clock().UTC()
	stamp := now.Format(backupStamp)
	name := "tpan-" + stamp + ".tar.gz.age"
	final := s.Dir + "/" + name
	part := final + ".part"
	tmp := s.Dir + "/.tmp-" + stamp
	for _, p := range []string{final, part, tmp} {
		if _, err := sys.Stat(p); err == nil {
			return fmt.Errorf("%s đã có (hai gói panel trong cùng một giây?); chờ một giây rồi chạy lại", p)
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
	size, sum, err := s.build(ctx, sys, now, tmp, part, final)
	// Thư mục tạm giữ bản rõ của tpan.db: LUÔN xoá, thành công hay lỗi. Xoá
	// không được thì bước lỗi: sót bản rõ là lỗi, không phải cảnh báo.
	err = errors.Join(err, removeTmpDir(sys, tmp))
	if err != nil {
		return errors.Join(err, removeIfExists(sys, part), removeIfExists(sys, final))
	}
	s.Path, s.Name, s.Size, s.SHA256 = final, name, size, sum
	s.applied = true
	return nil
}

// build làm phần giữa của Apply trong thư mục tạm tmp đã có; Apply lo dọn.
func (s *PanelBundle) build(ctx context.Context, sys system.System, now time.Time, tmp, part, final string) (int64, string, error) {
	db, manifest, plain := tmp+"/tpan.db", tmp+"/manifest.json", tmp+"/panel.tar.gz"
	if err := s.Snapshot(ctx, db); err != nil {
		return 0, "", fmt.Errorf("chụp tpan.db vào %s: %w", db, err)
	}
	migs, err := s.Migrations(ctx)
	if err != nil {
		return 0, "", fmt.Errorf("đọc danh sách migration: %w", err)
	}
	if migs == nil {
		migs = []int{} // manifest ghi [] chứ không phải null
	}
	dbSum, err := sha256Of(ctx, sys, db)
	if err != nil {
		return 0, "", err
	}
	data, err := json.MarshalIndent(panelManifest{
		Format: 1, Created: now.Format(time.RFC3339), Hostname: s.Hostname,
		TPANVersion: s.Version, Migrations: migs, DBSHA256: dbSum,
	}, "", "  ")
	if err != nil {
		return 0, "", fmt.Errorf("dựng manifest.json: %w", err)
	}
	if err := sys.WriteFileAtomic(manifest, append(data, '\n'), 0o600, system.Root); err != nil {
		return 0, "", fmt.Errorf("ghi %s: %w", manifest, err)
	}
	// Mọi mã thoát khác 0 là lỗi: tpan.db là bản chụp đứng yên, không có
	// chuyện "file đổi trong lúc đọc" như cây site của M9.
	if res, err := sys.Run(ctx, panelTar(plain, tmp)); err != nil {
		return 0, "", withStdout(fmt.Errorf("nén gói panel %s: %w", plain, err), res)
	}
	if err := s.encrypt(sys, plain, part); err != nil {
		return 0, "", err
	}
	if err := sys.Rename(part, final); err != nil {
		return 0, "", fmt.Errorf("đổi tên %s thành %s: %w", part, final, err)
	}
	sum, err := sha256Of(ctx, sys, final)
	if err != nil {
		return 0, "", err
	}
	fi, err := sys.Stat(final)
	if err != nil {
		return 0, "", fmt.Errorf("đọc cỡ %s: %w", final, err)
	}
	return fi.Size, sum, nil
}

// encrypt mã hoá theo dòng plain ra part (0600 root, tạo mới). Luồng mã phải
// Close TRƯỚC file: age chỉ ghi khúc cuối lúc Close và không đóng dst; sai thứ
// tự là ra một gói cụt không giải mã được mà không ai hay.
func (s *PanelBundle) encrypt(sys system.System, plain, part string) error {
	r, _, err := sys.OpenRead(plain)
	if err != nil {
		return fmt.Errorf("mở %s: %w", plain, err)
	}
	defer func() { _ = r.Close() }()
	w, err := sys.CreateFile(part, 0o600, system.Root)
	if err != nil {
		return fmt.Errorf("tạo %s: %w", part, err)
	}
	ew, err := s.Encrypt(w)
	if err != nil {
		return errors.Join(fmt.Errorf("mở luồng mã hoá: %w", err), w.Close())
	}
	_, err = io.Copy(ew, r)
	if err != nil {
		err = fmt.Errorf("mã hoá %s: %w", plain, err)
	}
	if cerr := ew.Close(); cerr != nil {
		err = errors.Join(err, fmt.Errorf("đóng luồng mã hoá: %w", cerr))
	}
	if cerr := w.Close(); cerr != nil {
		err = errors.Join(err, fmt.Errorf("ghi %s: %w", part, cerr))
	}
	return err
}

// removeTmpDir gỡ mọi thứ trong thư mục tạm rồi chính nó. Đọc danh sách thay
// vì gỡ ba tên cố định: Snapshot (VACUUM INTO) lỗi giữa chừng có thể để lại
// file phụ như tpan.db-journal, mà thư mục còn file thì Remove lỗi. Thư mục
// là root 0700 do chính bước tạo, không ai khác đặt được thứ gì vào đó.
func removeTmpDir(sys system.System, tmp string) error {
	names, err := sys.ReadDir(tmp)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("đọc thư mục tạm %s: %w", tmp, err)
	}
	var errs []error
	for _, n := range names {
		if err := removeIfExists(sys, tmp+"/"+n); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		errs = append(errs, removeIfExists(sys, tmp))
	}
	return errors.Join(errs...)
}

// Undo xoá gói vừa tạo: bước sau (đẩy lên kho) lỗi thì gói này không thuộc
// lượt nào thành công, để lại thì thành file mồ côi.
func (s *PanelBundle) Undo(_ context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("gói panel: %w", errNotApplied)
	}
	if err := removeIfExists(sys, s.Path); err != nil {
		return err
	}
	s.applied = false
	return nil
}
