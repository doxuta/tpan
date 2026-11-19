package core

// Backup và khôi phục (mục 6.5 của docs/KE-HOACH.md). Hợp đồng ở types_m9.go.
//
// Mỗi thao tác có thay đổi là một Planned (types_m12.go): Prepare* chỉ đọc
// máy, từ chối sớm (đầy đĩa, backup hỏng, thư mục khôi phục còn sót), và
// phần ghi bảng backups chạy trong after, tức là chỉ khi job thành công.
// Nhờ vậy bảng backups không bao giờ có dòng trỏ tới một file mà job đã
// hoàn tác (BackupArchive.Undo xoá file của nó).

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
	"github.com/doxuta/tpan/internal/validate"
)

// backupIDRe: id do store sinh là 16 hex. Nhận rộng hơn một chút (dòng ghi tay
// vẫn đọc được) nhưng chặn mọi ký tự lạ trước khi id đi vào câu trả lời.
var backupIDRe = regexp.MustCompile(`^[0-9a-f]{1,64}$`)

// tpanSnapName: tên bản sao tpan.db, khớp đúng mẫu mà checks.TPANBackupAge đọc.
var tpanSnapName = regexp.MustCompile(`^tpan-\d{8}-\d{6}\.db$`)

// List trả backup, mới nhất trước. Chưa có tpan.db (xem trước trên máy mới)
// thì như bảng rỗng.
func (b *Backups) List(ctx context.Context, domain string) ([]Backup, error) {
	if b.Store == nil {
		return []Backup{}, nil
	}
	out, err := b.Store.Backups(ctx, domain)
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách backup: %w", err)
	}
	return out, nil
}

// Settings đọc cấu hình backup. Giá trị hỏng trong DB là lỗi, không đoán: đoán
// sai số bản giữ là xoá mất backup.
func (b *Backups) Settings(ctx context.Context) (BackupSettings, error) {
	get := func(k string) (string, error) {
		v, _, err := b.Sites.Settings.GetSetting(ctx, k)
		if err != nil {
			return "", fmt.Errorf("đọc setting %s: %w", k, err)
		}
		return v, nil
	}
	out := BackupSettings{Keep: DefaultBackupKeep}
	keep, err := get(SettingBackupKeep)
	if err != nil {
		return BackupSettings{}, err
	}
	if keep != "" {
		n, err := strconv.Atoi(keep)
		if err != nil || n < 1 || n > MaxBackupKeep {
			return BackupSettings{}, fmt.Errorf("setting %s = %q không hợp lệ (cần số nguyên 1..%d); đặt lại bằng: sudo tpan backup settings --keep N", SettingBackupKeep, keep, MaxBackupKeep)
		}
		out.Keep = n
	}
	if out.At, err = get(SettingBackupAt); err != nil {
		return BackupSettings{}, err
	}
	if out.At != "" {
		if _, err := templates.BackupCron(out.At); err != nil {
			return BackupSettings{}, fmt.Errorf("setting %s = %q không hợp lệ (cần HH:MM); đặt lại bằng: sudo tpan backup settings --at HH:MM hoặc --off", SettingBackupAt, out.At)
		}
	}
	return out, nil
}

// diskLow là lời từ chối của luật chặn đầy đĩa (mục 6.5), kèm số đo để người
// đọc biết thiếu bao nhiêu và dọn ở đâu.
func diskLow(what i18n.Text, d string, need, free, total uint64) *RefusedError {
	n, f, t := dbHumanBytes(need), dbHumanBytes(free), dbHumanBytes(total)
	return refuse(CodeDiskSpaceLow, i18n.T(
		"không đủ chỗ trống để "+what.Vi+": cần khoảng "+n+" (mã nguồn và DB của site), đĩa còn trống "+f+" trên tổng "+t+". Luật: chỗ trống phải ít nhất gấp 1,2 lần chỗ cần và sau khi ghi vẫn còn 10% đĩa. Xem bản cũ bằng: sudo tpan backup list "+d+", và xem đĩa bằng: df -h "+site.BackupRoot,
		"not enough free space to "+what.En+": needs about "+n+" (the site's code and database), the disk has "+f+" free of "+t+". Rule: free space must be at least 1.2 times what is needed and 10% of the disk must stay free afterwards. See old copies with: sudo tpan backup list "+d+", and check the disk with: df -h "+site.BackupRoot))
}

// backupSpace đo và áp luật chặn đầy đĩa cho một bản backup đầy đủ của src.
func (b *Backups) backupSpace(ctx context.Context, l site.Layout, src string, what i18n.Text) error {
	need, free, total, err := steps.MeasureBackup(ctx, b.Sites.Sys, l, src, site.BackupDir(l))
	if err != nil {
		return fmt.Errorf("đo chỗ cần cho backup %s: %w", l.Domain, err)
	}
	if !steps.SpaceOK(free, total, need) {
		return diskLow(what, l.Domain.String(), need, free, total)
	}
	return nil
}

// archived đọc kết quả của bước BackupArchive sau khi job thành công thành một
// dòng của bảng backups. Path rỗng nghĩa là bước không chạy (không thể xảy ra
// vì Check của nó chỉ true sau Apply), báo lỗi thay vì ghi một dòng rỗng.
func archived(a *steps.BackupArchive, d validate.Domain, reason BackupReason, jobID string) (Backup, error) {
	if a.Path == "" || a.SHA256 == "" {
		return Backup{}, fmt.Errorf("bước backup %s không để lại file nào", d)
	}
	return Backup{
		Domain: d.String(), Kind: BackupFull, Reason: reason,
		Path: a.Path, Size: a.Size, SHA256: a.SHA256,
		WPVersion: a.WPVersion, PHPVersion: a.PHPVersion, JobID: jobID,
	}, nil
}

// PrepareCreate dựng Plan "backup.create" cho một site active hoặc trong thùng rác.
func (b *Backups) PrepareCreate(ctx context.Context, d validate.Domain, reason BackupReason) (*Planned, error) {
	switch reason {
	case ReasonManual, ReasonSchedule, ReasonBeforePurge:
	default:
		return nil, fmt.Errorf("backup %s: lý do %q không dùng cho backup.create", d, reason)
	}
	st, err := b.Sites.find(ctx, d)
	if err != nil {
		return nil, err
	}
	cfg, err := b.Sites.stack(ctx)
	if err != nil {
		return nil, err
	}
	l := st.Layout()
	src := l.Dir
	if st.Status == site.Trashed {
		src = site.TrashDir(l)
	}
	what := i18n.T("backup "+d.String(), "back up "+d.String())
	if err := b.backupSpace(ctx, l, src, what); err != nil {
		return nil, err
	}
	a := &steps.BackupArchive{Layout: l, Src: src, Dir: site.BackupDir(l), PHPVersion: sitePHP(st, cfg)}
	plan := engine.Plan{
		Kind:   "backup.create",
		Target: d.String(),
		Title:  i18n.T("Backup site "+d.String(), "Back up site "+d.String()),
		Steps:  []engine.Step{a},
	}
	return newPlanned(b.Sites.Sys, b.Sites.Journal, plan, func(ctx context.Context, rep engine.Report) error {
		if b.Store == nil {
			return fmt.Errorf("ghi backup %s: chưa có DB của tpan", d)
		}
		row, err := archived(a, d, reason, rep.Job.ID)
		if err != nil {
			return err
		}
		row.Created = dbNow(b)
		added, err := b.Store.AddBackup(ctx, row)
		if err != nil {
			return fmt.Errorf("ghi backup %s vào bảng backups (file %s vẫn còn trên máy): %w", d, a.Path, err)
		}
		return b.prune(ctx, d.String(), added.ID)
	}), nil
}

// prune dọn bản cũ của một domain: giữ Keep bản full và Keep bản sql mới nhất.
// Không bao giờ xoá bản ReasonBeforePurge (đường lùi duy nhất của site đã xoá
// hẳn; chúng cũng không chiếm chỗ trong Keep) và không xoá các id trong protect
// (bản vừa ghi, bản vừa dùng để khôi phục). File xoá trước, dòng sau: lỗi giữa
// chừng thì còn dòng trỏ tới file đã mất (lần sau dọn tiếp), không bao giờ
// còn file mồ côi không ai biết.
func (b *Backups) prune(ctx context.Context, domain string, protect ...string) error {
	if b.Store == nil {
		return nil
	}
	set, err := b.Settings(ctx)
	if err != nil {
		return err
	}
	rows, err := b.Store.Backups(ctx, domain)
	if err != nil {
		return fmt.Errorf("dọn backup cũ của %s: %w", domain, err)
	}
	seen := map[BackupKind]int{}
	var errs []error
	for _, r := range rows {
		if r.Domain != domain || r.Reason == ReasonBeforePurge {
			continue
		}
		seen[r.Kind]++
		if seen[r.Kind] <= set.Keep || slices.Contains(protect, r.ID) {
			continue
		}
		if err := b.removeBackupFile(r.Path); err != nil {
			errs = append(errs, fmt.Errorf("dọn backup %s của %s: %w", r.ID, domain, err))
			continue
		}
		if err := b.Store.DeleteBackup(ctx, r.ID); err != nil {
			errs = append(errs, fmt.Errorf("dọn backup %s của %s: %w", r.ID, domain, err))
		}
	}
	return errors.Join(errs...)
}

// removeBackupFile xoá một file backup mà đường dẫn đọc từ DB. tpan chạy bằng
// root, nên chỉ xoá khi đường dẫn nằm hẳn dưới site.BackupRoot (dòng bị sửa
// tay trỏ ra /etc thì dừng, giữ cả file lẫn dòng). File đã mất thì coi là xong.
func (b *Backups) removeBackupFile(p string) error {
	if !path.IsAbs(p) || path.Clean(p) != p || !strings.HasPrefix(p, site.BackupRoot+"/") {
		return fmt.Errorf("đường dẫn %q không nằm dưới %s; không xoá, kiểm dòng này bằng tay", p, site.BackupRoot)
	}
	if err := b.Sites.Sys.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("xoá %s: %w", p, err)
	}
	return nil
}

// CreateAll backup lần lượt mọi site active, mỗi site một job; site lỗi không
// chặn site sau. Cuối cùng chép tpan.db, kể cả khi không có site nào: bảng
// backups trong đó là cách duy nhất biết file backup nào thuộc site nào.
func (b *Backups) CreateAll(ctx context.Context, reason BackupReason, onEvent func(engine.Event)) ([]BackupResult, error) {
	if reason != ReasonManual && reason != ReasonSchedule {
		return nil, fmt.Errorf("backup mọi site: lý do %q không hợp lệ (manual hoặc schedule)", reason)
	}
	all, err := b.Sites.Store.Sites(ctx)
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách site: %w", err)
	}
	out := []BackupResult{}
	for _, st := range all {
		if st.Status != site.Active {
			continue
		}
		r := BackupResult{Domain: st.Domain.String()}
		p, err := b.PrepareCreate(ctx, st.Domain, reason)
		if err == nil {
			r.Report, err = p.Execute(ctx, onEvent)
		}
		if err != nil {
			r.Error = err.Error()
		}
		out = append(out, r)
	}
	if _, err := b.SnapshotTPANDB(ctx); err != nil {
		return out, err
	}
	return out, nil
}

// SnapshotTPANDB chép tpan.db vào site.TPANDBBackupDir và giữ DefaultBackupKeep
// bản mới nhất. Không phải job: VACUUM INTO của SQLite tự nhất quán, và đây là
// bản sao của chính DB đang ghi lịch sử job.
func (b *Backups) SnapshotTPANDB(ctx context.Context) (string, error) {
	if b.Store == nil {
		return "", errors.New("sao tpan.db: chưa có DB của tpan")
	}
	sys := b.Sites.Sys
	// /var/lib/tpan giữ 0755 (acme bên trong phải đi qua được); từ backups trở
	// xuống 0700 vì bản sao có cả lịch sử job và bản ghi của mọi site.
	// MkdirAll đặt perm cho mọi thư mục nó tạo, nên tạo từng tầng.
	for _, d := range []struct {
		p    string
		perm fs.FileMode
	}{{path.Dir(site.BackupRoot), 0o755}, {site.BackupRoot, 0o700}, {site.TPANDBBackupDir, 0o700}} {
		if err := sys.MkdirAll(d.p, d.perm, system.Root); err != nil {
			return "", fmt.Errorf("sao tpan.db: tạo thư mục %s: %w", d.p, err)
		}
	}
	p := site.TPANDBBackupDir + "/tpan-" + dbNow(b).UTC().Format(dbStamp) + ".db"
	if err := b.Store.SnapshotDB(ctx, p); err != nil {
		return "", err
	}
	names, err := sys.ReadDir(site.TPANDBBackupDir)
	if err != nil {
		return p, fmt.Errorf("sao tpan.db xong (%s) nhưng không đọc được %s để dọn bản cũ: %w", p, site.TPANDBBackupDir, err)
	}
	var snaps []string
	for _, n := range names {
		if tpanSnapName.MatchString(n) {
			snaps = append(snaps, n)
		}
	}
	// Tên có giờ dạng YYYYMMDD-HHMMSS nên sắp theo chữ là sắp theo thời gian.
	slices.Sort(snaps)
	var errs []error
	for _, n := range snaps[:max(0, len(snaps)-DefaultBackupKeep)] {
		if err := sys.Remove(site.TPANDBBackupDir + "/" + n); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, fmt.Errorf("dọn bản sao cũ %s: %w", n, err))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return p, fmt.Errorf("sao tpan.db xong (%s): %w", p, err)
	}
	return p, nil
}

// restoreSource tìm bản backup đầy đủ sẽ khôi phục. id rỗng: bản mới nhất.
func (b *Backups) restoreSource(ctx context.Context, d validate.Domain, id string) (Backup, error) {
	notFound := func(vi, en string) error {
		return refuse("backup_not_found", i18n.T(vi+"; xem các bản có bằng: sudo tpan backup list "+d.String(),
			en+"; list the copies with: sudo tpan backup list "+d.String()))
	}
	if id != "" && !backupIDRe.MatchString(id) {
		return Backup{}, notFound("id backup không đúng dạng (chữ số hex)", "the backup id is not in the right form (hex digits)")
	}
	if b.Store == nil {
		return Backup{}, notFound("site "+d.String()+" chưa có bản backup đầy đủ nào", "site "+d.String()+" has no full backup")
	}
	if id == "" {
		rows, err := b.Store.Backups(ctx, d.String())
		if err != nil {
			return Backup{}, fmt.Errorf("đọc backup của %s: %w", d, err)
		}
		for _, r := range rows {
			if r.Kind == BackupFull {
				return r, nil
			}
		}
		return Backup{}, notFound("site "+d.String()+" chưa có bản backup đầy đủ nào (tạo bằng: sudo tpan backup create "+d.String()+")",
			"site "+d.String()+" has no full backup (make one with: sudo tpan backup create "+d.String()+")")
	}
	r, ok, err := b.Store.Backup(ctx, id)
	if err != nil {
		return Backup{}, fmt.Errorf("đọc backup %s: %w", id, err)
	}
	if !ok || r.Domain != d.String() || r.Kind != BackupFull {
		return Backup{}, notFound("không có bản backup đầy đủ "+id+" của site "+d.String(),
			"there is no full backup "+id+" of site "+d.String())
	}
	return r, nil
}

// checkIntact đòi file của dòng backup còn đó và sha256 khớp, trước khi dựng
// Plan: người bấm khôi phục biết ngay bản đó hỏng, thay vì sau khi job đã tự
// backup xong rồi mới lỗi ở bước giải nén (bước đó cũng kiểm lại lúc chạy).
func (b *Backups) checkIntact(ctx context.Context, l site.Layout, r Backup) error {
	corrupt := func(vi, en string) error {
		return refuse("backup_corrupt", i18n.T("bản backup "+r.ID+" ("+r.Path+") không dùng được: "+vi+". Chọn bản khác: sudo tpan backup list "+l.Domain.String(),
			"backup "+r.ID+" ("+r.Path+") cannot be used: "+en+". Pick another one: sudo tpan backup list "+l.Domain.String()))
	}
	if path.Clean(r.Path) != r.Path || !strings.HasPrefix(r.Path, site.BackupDir(l)+"/") {
		return corrupt("đường dẫn không nằm dưới "+site.BackupDir(l), "the path is not under "+site.BackupDir(l))
	}
	fi, err := b.Sites.Sys.Stat(r.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return corrupt("file không còn trên máy", "the file is no longer on the machine")
	}
	if err != nil {
		return fmt.Errorf("kiểm %s: %w", r.Path, err)
	}
	if !fi.Mode.IsRegular() {
		return corrupt("không phải file thường", "it is not a regular file")
	}
	sum, err := dbFileSHA256(ctx, b.Sites.Sys, r.Path)
	if err != nil {
		return err
	}
	if sum != r.SHA256 {
		return corrupt("sha256 của file là "+sum+", khác "+r.SHA256+" ghi lúc tạo", "the file's sha256 is "+sum+", not "+r.SHA256+" recorded when it was made")
	}
	return nil
}

// PrepareRestore dựng Plan "backup.restore" (thứ tự bước ở types_m9.go).
func (b *Backups) PrepareRestore(ctx context.Context, d validate.Domain, id string) (*Planned, error) {
	st, err := b.Sites.find(ctx, d)
	if err != nil {
		return nil, err
	}
	if st.Status == site.Trashed {
		return nil, refuse("site_trashed", i18n.T(
			"site "+d.String()+" đang ở thùng rác; khôi phục site trước bằng: sudo tpan site restore "+d.String()+", rồi mới khôi phục backup",
			"site "+d.String()+" is in the trash; restore the site first with: sudo tpan site restore "+d.String()+", then restore the backup"))
	}
	cfg, err := b.Sites.stack(ctx)
	if err != nil {
		return nil, err
	}
	l := st.Layout()
	src, err := b.restoreSource(ctx, d, id)
	if err != nil {
		return nil, err
	}
	if err := b.checkIntact(ctx, l, src); err != nil {
		return nil, err
	}
	rd := site.RestoreDir(l)
	left, err := pathExists(b.Sites.Sys, rd)
	if err != nil {
		return nil, err
	}
	if left {
		// Không tự xoá: thư mục còn sót có thể là old-public của site từ một lần
		// khôi phục hỏng giữa chừng, tức là bản duy nhất của mã nguồn cũ.
		return nil, refuse("restore_dir_exists", i18n.T(
			rd+" còn sót từ một lần khôi phục trước. Xem bên trong trước (old-public có thể là mã nguồn cũ của site), rồi xoá bằng: sudo rm -r --one-file-system -- "+rd,
			rd+" is left over from an earlier restore. Look inside first (old-public may be the site's old code), then remove it with: sudo rm -r --one-file-system -- "+rd))
	}
	if err := b.backupSpace(ctx, l, l.Dir, i18n.T("tự backup "+d.String()+" trước khi khôi phục", "back up "+d.String()+" before restoring")); err != nil {
		return nil, err
	}
	php := sitePHP(st, cfg)
	others, err := b.Sites.others(ctx, d)
	if err != nil {
		return nil, err
	}
	pre := &steps.BackupArchive{Layout: l, Src: l.Dir, Dir: site.BackupDir(l), PHPVersion: php}
	dn := d.String()
	aside := rd + "/old-public"
	setAside := &steps.MoveTree{From: l.Public, To: aside,
		Title: i18n.T("Cất mã nguồn hiện tại của "+dn+" sang "+aside, "Set the current code of "+dn+" aside in "+aside),
		Why: i18n.T("Đổi tên, không xoá: kiểm chứng sau khôi phục mà lỗi thì engine chuyển nó về chỗ cũ. Thư mục này chỉ bị xoá khi job đã thành công.",
			"A rename, not a delete: if the checks after the restore fail, the engine moves it back. The directory is only removed once the job has succeeded.")}
	moveIn := &afterAside{aside: aside, MoveTree: &steps.MoveTree{From: rd + "/public", To: l.Public,
		Title: i18n.T("Đưa mã nguồn từ bản backup vào "+l.Public, "Move the code from the backup into "+l.Public),
		Why: i18n.T("Một lần đổi tên trên cùng phân vùng: site không bao giờ nằm ở trạng thái nửa cũ nửa mới.",
			"One rename on the same partition: the site is never left half old, half new.")}}
	plan := engine.Plan{
		Kind:   "backup.restore",
		Target: dn,
		Title:  i18n.T("Khôi phục site "+dn+" từ bản backup "+src.ID, "Restore site "+dn+" from backup "+src.ID),
		Steps: []engine.Step{
			pre,
			&steps.BackupExtract{Layout: l, Archive: src.Path, SHA256: src.SHA256, Dir: rd},
			setAside,
			moveIn,
			&steps.MariaDBDump{DB: l.DB, Path: rd + "/db-before.sql"},
			&steps.DBImport{Layout: l, File: rd + "/db.sql", UndoFile: rd + "/db-before.sql"},
		},
		Verify: siteChecks(cfg.Web, php, l, others, st.SSL),
	}
	return newPlanned(b.Sites.Sys, b.Sites.Journal, plan, func(ctx context.Context, rep engine.Report) error {
		var errs []error
		protect := []string{src.ID}
		if b.Store == nil {
			errs = append(errs, fmt.Errorf("ghi bản tự backup %s: chưa có DB của tpan", pre.Path))
		} else if row, err := archived(pre, d, ReasonBeforeRestore, rep.Job.ID); err != nil {
			errs = append(errs, err)
		} else {
			row.Created = dbNow(b)
			added, err := b.Store.AddBackup(ctx, row)
			if err != nil {
				errs = append(errs, fmt.Errorf("ghi bản tự backup %s vào bảng backups: %w", pre.Path, err))
			} else {
				protect = append(protect, added.ID)
			}
		}
		// Gỡ thư mục khôi phục cả khi ghi dòng lỗi: để lại thì lần khôi phục sau
		// bị từ chối, mà site đã chạy đúng bản mới.
		if err := (&steps.RemoveTree{Path: rd, Under: site.RestoreRoot}).Apply(ctx, b.Sites.Sys); err != nil {
			errs = append(errs, fmt.Errorf("gỡ %s (xoá tay bằng: sudo rm -r --one-file-system -- %s): %w", rd, rd, err))
		}
		if len(protect) > 1 {
			if err := b.prune(ctx, dn, protect...); err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	}), nil
}

// afterAside là bước tráo thứ hai của khôi phục (RestoreDir/public → l.Public):
// chỉ "đã đúng" khi mã nguồn cũ đã nằm ở aside (RestoreDir/old-public). Lúc
// xem trước, l.Public (đích của nó) đang có sẵn nên Check của MoveTree một mình
// báo "đã xong, bỏ qua", và người duyệt thấy sai đúng bước quan trọng nhất.
//
// Không dùng afterStep{dep: bước cất đi} (oneway.go): sau khi bước này chạy,
// cả l.Public lẫn old-public đều có, Check của bước cất đi trả lỗi "cả hai
// đều có", nên lần kiểm lại sau Apply của engine sẽ hỏng. Hỏi thẳng old-public
// có chưa thì đúng cả lúc xem trước, trước và sau Apply.
type afterAside struct {
	*steps.MoveTree
	aside string
}

func (a *afterAside) Check(ctx context.Context, sys system.System) (bool, error) {
	ok, err := pathExists(sys, a.aside)
	if err != nil || !ok {
		return false, err
	}
	return a.MoveTree.Check(ctx, sys)
}

// PrepareSettings dựng Plan "backup.settings": ghi hoặc gỡ file cron của lịch
// backup. Cron của Debian tự đọc lại /etc/cron.d mỗi phút, không cần reload.
func (b *Backups) PrepareSettings(ctx context.Context, keep int, at string) (*Planned, error) {
	if keep < 1 || keep > MaxBackupKeep {
		return nil, refuse("backup_keep_invalid", i18n.T(
			fmt.Sprintf("số bản giữ mỗi site phải từ 1 tới %d (nhận được %d)", MaxBackupKeep, keep),
			fmt.Sprintf("the number of copies kept per site must be 1 to %d (got %d)", MaxBackupKeep, keep)))
	}
	f := site.BackupCronFile
	var step engine.Step
	title := i18n.T(fmt.Sprintf("Tắt lịch backup, giữ %d bản mỗi site", keep), fmt.Sprintf("Turn the backup schedule off, keep %d copies per site", keep))
	if at == "" {
		step = &steps.Absent{Path: f,
			Title: i18n.T("Gỡ lịch backup hằng ngày "+f, "Remove the daily backup schedule "+f),
			Why: i18n.T("Không còn file này thì cron không chạy backup tự động nữa; backup bằng tay vẫn chạy được.",
				"Without this file cron no longer runs the automatic backup; manual backups still work.")}
	} else {
		content, err := templates.BackupCron(at)
		if err != nil {
			return nil, refuse("backup_at_invalid", i18n.T(
				"giờ backup "+strconv.Quote(at)+" phải có dạng HH:MM (24 giờ, ví dụ 03:30)",
				"the backup time "+strconv.Quote(at)+" must be HH:MM (24-hour, for example 03:30)"))
		}
		step = &steps.File{Path: f, Content: content, Perm: 0o644, Owner: system.Root,
			Title: i18n.T("Đặt lịch backup hằng ngày lúc "+at, "Schedule the daily backup at "+at),
			Why: i18n.T("Cron đọc file này và gọi tpan backup mọi site đang chạy rồi chép tpan.db mỗi ngày. Không có nó thì chỉ có backup khi ai đó nhớ bấm.",
				"Cron reads this file and has tpan back up every running site and copy tpan.db each day. Without it there is only a backup when someone remembers to make one.")}
		title = i18n.T(fmt.Sprintf("Backup hằng ngày lúc %s, giữ %d bản mỗi site", at, keep), fmt.Sprintf("Back up daily at %s, keep %d copies per site", at, keep))
	}
	plan := engine.Plan{Kind: "backup.settings", Target: "backup", Title: title, Steps: []engine.Step{step}}
	return newPlanned(b.Sites.Sys, b.Sites.Journal, plan, func(ctx context.Context, _ engine.Report) error {
		if err := b.Sites.Settings.SetSetting(ctx, SettingBackupKeep, strconv.Itoa(keep)); err != nil {
			return fmt.Errorf("ghi số bản giữ: %w", err)
		}
		if err := b.Sites.Settings.SetSetting(ctx, SettingBackupAt, at); err != nil {
			return fmt.Errorf("ghi giờ backup: %w", err)
		}
		return nil
	}), nil
}
