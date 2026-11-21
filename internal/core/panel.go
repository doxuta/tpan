package core

// Gói panel (M18, mục 3.8 của docs/thay-doi/M18-thiet-ke.md): tpan.db đã mã
// hoá bằng khoá sao lưu, đẩy lên kho ở cuối mỗi lượt đẩy có thay đổi. Bước
// tạo gói (steps.PanelBundle) không đọc khoá; core đưa vào hàm mã hoá đọc
// khoá từ file đúng lúc mã hoá.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/agebox"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/s3"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
)

// panelBundleName: tên gói panel do PanelBundle đặt (giờ UTC), nên sắp theo
// chữ là sắp theo thời gian.
var panelBundleName = regexp.MustCompile(`^tpan-\d{8}-\d{6}\.tar\.gz\.age$`)

// panelRun giữ bước tạo gói (tên, sha256 sau Apply) và lỗi thật của bước đẩy.
type panelRun struct {
	bundle *steps.PanelBundle
	h      *pushHolder
}

// PreparePanel dựng "backup.panel". Job chỉ được chạy dưới khoá lượt đẩy
// (Sync làm việc đó); Planned không lấy khoá job của máy.
func (r *Remote) PreparePanel(ctx context.Context) (*Planned, error) {
	store, c, err := r.open(ctx)
	if err != nil {
		return nil, err
	}
	p, _, err := r.preparePanel(ctx, store, c)
	return p, err
}

func (r *Remote) preparePanel(_ context.Context, store steps.ObjectStore, c TargetConfig) (*Planned, *panelRun, error) {
	k, ok, err := readBackupKey(r.Sys)
	switch {
	case err != nil:
		// err chỉ nêu đường dẫn (readSecret), không nêu nội dung.
		return nil, nil, refuse(CodeBackupKeyMissing, i18n.T("không đọc được khoá sao lưu: "+err.Error(), "cannot read the backup key: "+err.Error()))
	case !ok:
		return nil, nil, refuse(CodeBackupKeyMissing, i18n.T(
			"chưa có khoá sao lưu nên chưa tạo được gói panel; sinh khoá bằng: sudo tpan backup key new",
			"there is no backup key yet, so no panel bundle can be made; generate one with: sudo tpan backup key new"))
	case agebox.CheckKey(k) != nil:
		return nil, nil, refuse(CodeBackupKeyMissing, i18n.T(
			"khoá sao lưu sai dạng ("+steps.BackupKeyFile+"); xem bằng: sudo tpan backup key show, hoặc sinh khoá mới: sudo tpan backup key new",
			"the backup key is malformed ("+steps.BackupKeyFile+"); check it with: sudo tpan backup key show, or generate a new one: sudo tpan backup key new"))
	}
	if r.Snapshot == nil || r.Store == nil {
		return nil, nil, errors.New("tạo gói panel: thiếu Snapshot hoặc DB của tpan")
	}
	bundle := &steps.PanelBundle{
		Dir:        site.TPANDBBackupDir,
		Snapshot:   r.Snapshot,
		Migrations: r.Store.AppliedMigrations,
		Encrypt:    r.panelEncrypt,
		Hostname:   r.Hostname,
		Version:    r.Version,
	}
	h := &pushHolder{}
	plan := engine.Plan{
		Kind:   "backup.panel",
		Target: "panel",
		Title:  i18n.T("Tạo gói panel đã mã hoá và đẩy lên kho", "Create the encrypted panel bundle and upload it to the store"),
		Steps: []engine.Step{
			&steps.Dir{Path: site.BackupRoot, Perm: 0o700, Owner: system.Root},
			&steps.Dir{Path: site.TPANDBBackupDir, Perm: 0o700, Owner: system.Root},
			bundle,
			&capturePush{S3Push: &steps.S3Push{Store: store, ID: "panel", From: bundle}, h: h},
		},
	}
	p := newPlanned(heldLockSystem{r.Sys}, r.Journal, plan, func(ctx context.Context, _ engine.Report) error {
		dbctx := context.WithoutCancel(ctx)
		last := bundle.Name + "|" + r.now().UTC().Format(time.RFC3339) + "|" + bundle.SHA256
		if err := r.setSettings(dbctx, SettingS3PanelLast, last, SettingS3PanelError, "", SettingS3PanelDue, "0",
			SettingS3PanelFailures, "", SettingS3PanelRetryAt, ""); err != nil {
			return err
		}
		return errors.Join(r.targetOK(dbctx), r.prunePanelsLocal(), r.prunePanelsRemote(ctx, store, c.Keep))
	})
	p.owner = account.AdminName
	return p, &panelRun{bundle: bundle, h: h}, nil
}

// panelEncrypt đọc khoá ngay lúc mã hoá (mục 3.8): `backup key new` chạy giữa
// lúc dựng Plan và lúc chạy thì gói mang khoá mới, không phải khoá cũ.
func (r *Remote) panelEncrypt(dst io.Writer) (io.WriteCloser, error) {
	k, ok, err := readBackupKey(r.Sys)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New(steps.BackupKeyFile + " không còn")
	}
	return agebox.Encrypt(dst, k)
}

// prunePanelsLocal giữ PanelKeepLocal gói mới nhất trên máy.
func (r *Remote) prunePanelsLocal() error {
	names, err := r.Sys.ReadDir(site.TPANDBBackupDir)
	if err != nil {
		return fmt.Errorf("đọc %s để dọn gói panel cũ: %w", site.TPANDBBackupDir, err)
	}
	var bundles []string
	for _, n := range names {
		if panelBundleName.MatchString(n) {
			bundles = append(bundles, n)
		}
	}
	slices.Sort(bundles)
	var errs []error
	for _, n := range bundles[:max(0, len(bundles)-PanelKeepLocal)] {
		if err := r.Sys.Remove(site.TPANDBBackupDir + "/" + n); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, fmt.Errorf("dọn gói panel cũ %s: %w", n, err))
		}
	}
	return errors.Join(errs...)
}

// prunePanelsRemote giữ keep (s3.keep) gói mới nhất trên kho.
func (r *Remote) prunePanelsRemote(ctx context.Context, store steps.ObjectStore, keep int) error {
	var keys []string
	if err := store.List(ctx, "panel/", func(o s3.Object) error {
		if panelBundleName.MatchString(strings.TrimPrefix(o.Key, "panel/")) {
			keys = append(keys, o.Key)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("liệt kê gói panel trên kho để dọn: %w", err)
	}
	slices.Sort(keys)
	var errs []error
	for _, k := range keys[:max(0, len(keys)-keep)] {
		if err := store.Delete(ctx, k); err != nil {
			errs = append(errs, fmt.Errorf("dọn gói panel cũ %s trên kho: %w", k, err))
		}
	}
	return errors.Join(errs...)
}
