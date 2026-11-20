package core

// Thao tác DB của site (mục 6.9 của docs/KE-HOACH.md): đổi mật khẩu user DB,
// xuất DB ra file .sql, nhập lại một bản đã xuất. Hợp đồng ở types_m9.go.
//
// Ba Plan đều dựng qua Planned (types_m12.go). Bản xuất DB là một dòng Kind
// sql của bảng backups: nhập chỉ nhận file tpan đã ghi và đã kiểm sha256,
// không nhận đường dẫn tuỳ ý (file .sql lạ chạy bằng root là chạy SQL tuỳ ý).
//
// Tên hàm phụ trong file này có tiền tố db: thợ backup.go viết song song
// trong cùng gói, tên chung (now, sha256Of...) dễ trùng.

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// dbStamp là dạng giờ trong tên file xuất (UTC, như tên file backup).
const dbStamp = "20060102-150405"

// dbNow là giờ của Backups: Now, không có thì Sites.now (hợp đồng).
func dbNow(b *Backups) time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return b.Sites.now()
}

// dbSite đọc site và đòi trạng thái hợp với thao tác: active, hoặc thêm
// trashed khi allowTrashed (xuất DB: DB của site trong thùng rác vẫn còn).
func dbSite(ctx context.Context, b *Backups, d validate.Domain, allowTrashed bool) (site.Site, error) {
	st, err := b.Sites.find(ctx, d)
	if err != nil {
		return site.Site{}, err
	}
	switch {
	case st.Status == site.Active, st.Status == site.Trashed && allowTrashed:
		return st, nil
	case st.Status == site.Trashed:
		return site.Site{}, refuse("site_trashed", i18n.T(
			"site "+d.String()+" đang ở thùng rác; khôi phục trước bằng: sudo tpan site restore "+d.String(),
			"site "+d.String()+" is in the trash; restore it first with: sudo tpan site restore "+d.String()))
	}
	return site.Site{}, fmt.Errorf("site %s có trạng thái lạ %q trong DB", d, st.Status)
}

// dbNotSuspended (M13) từ chối đổi mật khẩu và nhập DB khi site hay DB của
// site đang tạm khoá: cả hai kiểm chứng bằng WP-CLI đăng nhập DB và site trả
// 200, mà site tạm khoá trả 403 (pool đã tắt) còn user DB khoá thì không đăng
// nhập được, nên job luôn hỏng và bị hoàn tác. Xuất DB thì vẫn được (root).
func dbNotSuspended(st site.Site) error {
	d := st.Domain.String()
	if st.Suspended != account.NotSuspended {
		return refuse(CodeSiteSuspended, i18n.T(
			"site "+d+" đang tạm khoá; mở khoá trước bằng: sudo tpan site unsuspend "+d,
			"site "+d+" is suspended; unsuspend it first with: sudo tpan site unsuspend "+d))
	}
	if st.DBSuspended != account.NotSuspended {
		return refuse(CodeDBSuspended, i18n.T(
			"DB của site "+d+" đang tạm khoá; mở khoá trước bằng: sudo tpan db unsuspend "+d,
			"the database of "+d+" is suspended; unsuspend it first with: sudo tpan db unsuspend "+d))
	}
	return nil
}

// dbDumpPath là file xuất mới trong thư mục backup của site. suffix phân biệt
// bản tự xuất trước khi nhập với bản người dùng xuất, để hai bản xuất trong
// cùng một giây không bao giờ trùng tên (nhập bản vừa xuất ngay giây đó).
// File đã có thì từ chối: MariaDBDump coi file trọn vẹn sẵn có là "đã xong"
// và bỏ qua, và cột path của bảng backups là UNIQUE.
func dbDumpPath(b *Backups, l site.Layout, suffix string) (string, error) {
	p := site.BackupDir(l) + "/" + l.Domain.String() + "-db-" + dbNow(b).UTC().Format(dbStamp) + suffix + ".sql"
	exists, err := pathExists(b.Sites.Sys, p)
	if err != nil {
		return "", err
	}
	if exists {
		return "", refuse("backup_exists", i18n.T(
			p+" đã có (vừa xuất trong cùng giây này); chờ một giây rồi chạy lại",
			p+" already exists (a dump was taken this same second); wait a second and run again"))
	}
	return p, nil
}

// dbDiskNear đo phân vùng sẽ chứa dir; dir chưa có (lần xuất đầu) thì đo thư
// mục cha gần nhất đã có, vì thư mục sẽ được tạo trên chính phân vùng đó.
func dbDiskNear(sys system.System, dir string) (system.DiskUsage, error) {
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

// dbHumanBytes in cỡ cho người đọc (KiB, MiB, GiB theo 1024).
func dbHumanBytes(n uint64) string {
	const k = 1024
	f := float64(n)
	switch {
	case n >= k*k*k:
		return fmt.Sprintf("%.1f GiB", f/(k*k*k))
	case n >= k*k:
		return fmt.Sprintf("%.1f MiB", f/(k*k))
	case n >= k:
		return fmt.Sprintf("%.1f KiB", f/k)
	}
	return fmt.Sprintf("%d B", n)
}

// dbSpaceCheck là chặn đầy đĩa của mục 6.5 cho một bản xuất DB, chạy lúc dựng
// Plan để từ chối trước khi người dùng bấm. MariaDBDump đo lại lúc Apply (Plan
// chờ duyệt có thể chạy sau đó hàng giờ).
func dbSpaceCheck(ctx context.Context, b *Backups, l site.Layout) error {
	sys := b.Sites.Sys
	need, err := steps.MeasureDB(ctx, sys, l)
	if err != nil {
		return err
	}
	du, err := dbDiskNear(sys, site.BackupDir(l))
	if err != nil {
		return err
	}
	if steps.SpaceOK(du.Free, du.Total, need) {
		return nil
	}
	n, f, t := dbHumanBytes(need), dbHumanBytes(du.Free), dbHumanBytes(du.Total)
	return refuse(CodeDiskSpaceLow, i18n.T(
		"không đủ chỗ trống để xuất DB "+l.DB+": cần khoảng "+n+", đĩa còn trống "+f+" trên tổng "+t+". Luật: chỗ trống phải ít nhất gấp 1,2 lần cỡ bản xuất và sau khi ghi vẫn còn 10% đĩa. Dọn bớt bằng: sudo tpan backup list "+l.Domain.String()+" rồi xoá bản cũ, hoặc xem: df -h "+site.BackupRoot,
		"not enough free space to dump database "+l.DB+": needs about "+n+", the disk has "+f+" free of "+t+". Rule: free space must be at least 1.2 times the dump and 10% of the disk must stay free afterwards. Free some up with: sudo tpan backup list "+l.Domain.String()+" and delete old copies, or check: df -h "+site.BackupRoot))
}

// dbFileSHA256 tính sha256 bằng lệnh sha256sum, không bằng ReadFile: file
// xuất có thể vài GB, còn ReadFile giữ cả file trong bộ nhớ.
func dbFileSHA256(ctx context.Context, sys system.System, p string) (string, error) {
	res, err := sys.Run(ctx, system.Cmd{Name: "sha256sum", Args: []string{"--", p}})
	if err != nil {
		return "", fmt.Errorf("tính sha256 của %s: %w", p, err)
	}
	f := strings.Fields(res.Stdout)
	if len(f) == 0 || len(f[0]) != 64 || strings.Trim(f[0], "0123456789abcdef") != "" {
		return "", fmt.Errorf("tính sha256 của %s: kết quả lạ %q", p, res.Stdout)
	}
	return f[0], nil
}

// dbRecord ghi một bản xuất vừa xong vào bảng backups (chạy trong after,
// sau khi job thành công): sha256 và cỡ đọc lại từ chính file trên máy. Rồi
// dọn bản cũ như backup đầy đủ (prune ở backup.go: giữ Keep bản sql mỗi
// site), không xoá bản vừa ghi và các id trong protect.
func dbRecord(ctx context.Context, b *Backups, d validate.Domain, p string, reason BackupReason, jobID string, protect ...string) error {
	if b.Store == nil {
		return fmt.Errorf("ghi bản xuất %s: chưa có DB của tpan", p)
	}
	sys := b.Sites.Sys
	sum, err := dbFileSHA256(ctx, sys, p)
	if err != nil {
		return err
	}
	fi, err := sys.Stat(p)
	if err != nil {
		return fmt.Errorf("đọc cỡ %s: %w", p, err)
	}
	added, err := b.Store.AddBackup(ctx, Backup{
		Domain: d.String(), Kind: BackupSQL, Reason: reason,
		Path: p, Size: fi.Size, SHA256: sum, JobID: jobID, Created: dbNow(b),
	})
	if err != nil {
		return fmt.Errorf("ghi bản xuất %s vào bảng backups (file vẫn còn trên máy): %w", p, err)
	}
	return b.prune(ctx, d.String(), append(protect, added.ID)...)
}

// dbVerifyWait, dbVerifyEvery: trần và nhịp thử lại phép kiểm đầu cuối sau
// thao tác DB. Biến để test rút ngắn.
var (
	dbVerifyWait  = 6 * time.Second
	dbVerifyEvery = 300 * time.Millisecond
)

// dbRetry chạy lại phép kiểm bên trong tới khi hết Fail hoặc hết dbVerifyWait.
// Lý do: giữa ALTER USER và lúc wp-config.php có mật khẩu mới, site trả 500;
// lúc viết lo opcache (opcache.revalidate_freq=2) giữ file cũ thêm tới 2 giây.
// Đo trên máy ảo (docs/thay-doi/M9.md): các lần 500 chỉ nằm trong lúc job
// chạy, dưới 0,3 giây, sau job không còn. Lớp thử lại vẫn giữ làm phòng hờ:
// không thử lại mà gặp một lần 500 trễ thì Plan bị hoàn tác oan. Pass, Warn, Unknown trả ngay: chỉ Fail mới có thể là
// khoảng chờ đó.
type dbRetry struct{ inner checks.Check }

func (r dbRetry) Run(ctx context.Context, sys system.System) checks.Result {
	deadline := time.Now().Add(dbVerifyWait)
	for {
		res := r.inner.Run(ctx, sys)
		if res.Status != checks.Fail || time.Now().After(deadline) {
			return res
		}
		select {
		case <-ctx.Done():
			return res
		case <-time.After(dbVerifyEvery):
		}
	}
}

// dbVerify là kiểm chứng sau đổi mật khẩu và nhập DB: chặng DB (user DB đăng
// nhập được bằng đúng wp-config.php) và đầu cuối (site trả 200, đúng là
// WordPress của site), bản https khi site có SSL. Không chạy cả siteChecks:
// vhost, pool, mã nguồn không đổi, còn kiểm checksum WordPress là phép kiểm
// nặng. DBAccess không cần thử lại: WP-CLI đọc wp-config.php mới mỗi lần.
func dbVerify(st site.Site) []checks.Check {
	l := st.Layout()
	e2e := checks.HTTPSite(l)
	if st.SSL {
		e2e = checks.HTTPSSite(l)
	}
	return []checks.Check{checks.DBAccess(l), dbRetry{e2e}}
}

// PrepareDBPassword dựng Plan "db.passwd" (hợp đồng ở types_m9.go).
func (b *Backups) PrepareDBPassword(ctx context.Context, d validate.Domain) (*Planned, error) {
	st, err := dbSite(ctx, b, d, false)
	if err != nil {
		return nil, err
	}
	if err := dbNotSuspended(st); err != nil {
		return nil, err
	}
	pw, err := sitePassword()
	if err != nil {
		return nil, err
	}
	s := b.Sites
	return newPlanned(s.Sys, s.Journal, engine.Plan{
		Kind:   "db.passwd",
		Target: d.String(),
		Title:  i18n.T("Đổi mật khẩu DB của site "+d.String(), "Change the database password of "+d.String()),
		Steps:  []engine.Step{&steps.DBPassword{Layout: st.Layout(), Password: pw}},
		Verify: dbVerify(st),
	}, nil), nil
}

// PrepareDBDump dựng Plan "db.dump" (hợp đồng ở types_m9.go). Tên file chọn
// lúc dựng Plan: Plan này không đi qua Planner nên không bị dựng lại.
func (b *Backups) PrepareDBDump(ctx context.Context, d validate.Domain) (*Planned, error) {
	st, err := dbSite(ctx, b, d, true)
	if err != nil {
		return nil, err
	}
	l := st.Layout()
	if err := dbSpaceCheck(ctx, b, l); err != nil {
		return nil, err
	}
	p, err := dbDumpPath(b, l, "")
	if err != nil {
		return nil, err
	}
	s := b.Sites
	return newPlanned(s.Sys, s.Journal, engine.Plan{
		Kind:   "db.dump",
		Target: d.String(),
		Title:  i18n.T("Xuất DB của site "+d.String()+" ra file .sql", "Dump the database of "+d.String()+" to a .sql file"),
		Steps:  []engine.Step{&steps.MariaDBDump{DB: l.DB, Path: p}},
	}, func(ctx context.Context, rep engine.Report) error {
		return dbRecord(ctx, b, d, p, ReasonManual, rep.Job.ID)
	}), nil
}

// dbBackupNotFound là lời từ chối khi id không trỏ tới một bản xuất của site.
// id lạ (backupIDRe ở backup.go) không được in lại: nó do người gõ, có thể
// mang ký tự điều khiển của terminal.
func dbBackupNotFound(d validate.Domain, id string) *RefusedError {
	if !backupIDRe.MatchString(id) {
		id = "?"
	}
	return refuse("backup_not_found", i18n.T(
		"không có bản xuất DB "+id+" của site "+d.String()+"; xem các bản đã có bằng: sudo tpan backup list "+d.String(),
		"there is no database dump "+id+" of "+d.String()+"; list the existing ones with: sudo tpan backup list "+d.String()))
}

// PrepareDBImport dựng Plan "db.import" (hợp đồng ở types_m9.go).
func (b *Backups) PrepareDBImport(ctx context.Context, d validate.Domain, id string) (*Planned, error) {
	st, err := dbSite(ctx, b, d, false)
	if err != nil {
		return nil, err
	}
	if err := dbNotSuspended(st); err != nil {
		return nil, err
	}
	l := st.Layout()
	// Store nil: máy chưa có tpan.db (--dry-run lần đầu), bảng backups rỗng.
	if b.Store == nil || !backupIDRe.MatchString(id) {
		return nil, dbBackupNotFound(d, id)
	}
	bk, ok, err := b.Store.Backup(ctx, id)
	if err != nil {
		return nil, err
	}
	if !ok || bk.Domain != d.String() || bk.Kind != BackupSQL {
		return nil, dbBackupNotFound(d, id)
	}
	// Dòng do tpan ghi luôn trỏ vào thư mục backup của chính site; khác thế
	// là dòng bị sửa tay, không nhập (file nằm chỗ khác có thể do ai cũng ghi được).
	corrupt := func(vi, en string) *RefusedError {
		return refuse("backup_corrupt", i18n.T(
			"bản xuất "+id+" ("+bk.Path+") "+vi+"; không nhập. Xuất bản mới bằng: sudo tpan db dump "+d.String(),
			"dump "+id+" ("+bk.Path+") "+en+"; not importing it. Take a new one with: sudo tpan db dump "+d.String()))
	}
	if path.Dir(bk.Path) != site.BackupDir(l) {
		return nil, corrupt("không nằm trong "+site.BackupDir(l), "is not inside "+site.BackupDir(l))
	}
	fi, err := b.Sites.Sys.Stat(bk.Path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, corrupt("không còn trên đĩa", "is no longer on disk")
	case err != nil:
		return nil, fmt.Errorf("kiểm %s: %w", bk.Path, err)
	case !fi.Mode.IsRegular():
		return nil, corrupt("không phải file thường", "is not a regular file")
	}
	sum, err := dbFileSHA256(ctx, b.Sites.Sys, bk.Path)
	if err != nil {
		return nil, err
	}
	if sum != bk.SHA256 {
		return nil, corrupt("có sha256 khác lúc xuất (file đã bị sửa hoặc hỏng)", "has a different sha256 than when it was taken (the file was changed or damaged)")
	}
	if err := dbSpaceCheck(ctx, b, l); err != nil {
		return nil, err
	}
	undo, err := dbDumpPath(b, l, "-before-import")
	if err != nil {
		return nil, err
	}
	s := b.Sites
	return newPlanned(s.Sys, s.Journal, engine.Plan{
		Kind:   "db.import",
		Target: d.String(),
		Title:  i18n.T("Nhập bản xuất "+id+" vào DB của site "+d.String(), "Import dump "+id+" into the database of "+d.String()),
		Steps: []engine.Step{
			&steps.MariaDBDump{DB: l.DB, Path: undo},
			&steps.DBImport{Layout: l, File: bk.Path, UndoFile: undo},
		},
		Verify: dbVerify(st),
	}, func(ctx context.Context, rep engine.Report) error {
		// Bản vừa nhập cũng được giữ: nó là trạng thái hiện tại của DB.
		return dbRecord(ctx, b, d, undo, ReasonBeforeImport, rep.Job.ID, bk.ID)
	}), nil
}
