package core

// Test luồng backup của M9 sau khi sửa cho M18: bản sao lưu nằm trên máy, trên
// kho S3, hoặc cả hai (mục 3.6, 3.7, 3.15 của docs/thay-doi/M18-thiet-ke.md).

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/s3"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
)

// m18Target đặt một đích hợp lệ trong settings và trả mã của nó.
func (be *bktEnv) m18Target(t *testing.T) string {
	t.Helper()
	setTarget(t, be.st)
	id, ok, err := CurrentTargetID(ctx, be.st)
	if err != nil || !ok {
		t.Fatalf("CurrentTargetID: ok=%v err=%v", ok, err)
	}
	return id
}

// stored đánh dấu r đã lên đích targetID (con đường duy nhất của store). gone:
// rồi dọn khỏi máy như prune (xoá file, ForgetBackupLocal), thành local=0.
func (be *bktEnv) stored(t *testing.T, r Backup, targetID string, gone bool) Backup {
	t.Helper()
	if ok, err := be.bs.MarkBackupStored(ctx, r.ID, r.Domain+"/"+r.ID+".tar.gz", targetID, bktNow); err != nil || !ok {
		t.Fatalf("MarkBackupStored %s: ok=%v err=%v", r.ID, ok, err)
	}
	if gone {
		if err := be.m.Remove(r.Path); err != nil {
			t.Fatal(err)
		}
		if deleted, err := be.bs.ForgetBackupLocal(ctx, r.ID); err != nil || deleted {
			t.Fatalf("ForgetBackupLocal %s: deleted=%v err=%v", r.ID, deleted, err)
		}
	}
	got, ok, err := be.bs.Backup(ctx, r.ID)
	if err != nil || !ok {
		t.Fatalf("đọc lại %s: ok=%v err=%v", r.ID, ok, err)
	}
	return got
}

func (be *bktEnv) row(t *testing.T, id string) (Backup, bool) {
	t.Helper()
	r, ok, err := be.b.Store.Backup(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return r, ok
}

// ghostStore thêm vào store thật các dòng local=0 mà không stored: store không
// có đường nào tạo ra trạng thái này (mục 3.15), nên test dựng nó ở đây.
type ghostStore struct {
	*bktStore
	ghosts []Backup
}

func (g *ghostStore) Backups(ctx context.Context, domain string) ([]Backup, error) {
	rows, err := g.bktStore.Backups(ctx, domain)
	if err != nil {
		return nil, err
	}
	for _, r := range g.ghosts {
		if domain == "" || r.Domain == domain {
			rows = append(rows, r)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Created.After(rows[j].Created) })
	return rows, nil
}

func (g *ghostStore) Backup(ctx context.Context, id string) (Backup, bool, error) {
	for _, r := range g.ghosts {
		if r.ID == id {
			return r, true, nil
		}
	}
	return g.bktStore.Backup(ctx, id)
}

func (g *ghostStore) DeleteBackup(ctx context.Context, id string) error {
	g.ghosts = slices.DeleteFunc(g.ghosts, func(r Backup) bool { return r.ID == id })
	return g.bktStore.DeleteBackup(ctx, id)
}

// ================================================================ B1

// B1: sao lưu cục bộ không bao giờ phụ thuộc kho. Máy đã đặt đích và có đủ
// khoá; kho "lỗi mọi lời gọi" (dựng kho là lỗi và được đếm). Job backup.create
// vẫn đạt, dòng mới là "trên máy, chưa lên kho", và không lệnh nào ra mạng.
func TestSaoLuuCucBoKhongPhuThuocKho(t *testing.T) {
	const dom = "shop.example.com"
	be := newBktEnv(t, dom)
	be.m18Target(t)
	enc, err := encodeKeys("AKIAEXAMPLEKEY123", system.NewSecret("bi-mat-rat-dai-0123456789"))
	if err != nil {
		t.Fatal(err)
	}
	be.m.SetDir("/etc/tpan", 0o700, system.Root)
	be.m.SetFile(steps.S3KeysFile, []byte(enc.Reveal()), 0o600, system.Root)
	be.m.SetFile(steps.BackupKeyFile, []byte("khoa-sao-luu-gia\n"), 0o600, system.Root)
	var dialed atomic.Int32
	old := newS3Store
	newS3Store = func(s3.Config) (steps.ObjectStore, error) {
		dialed.Add(1)
		return nil, errors.New("kho giả: lỗi mọi lời gọi")
	}
	t.Cleanup(func() { newS3Store = old })

	be.m.ResetRan()
	p, err := be.b.PrepareCreate(ctx, mustDomain(t, dom), ReasonManual)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := p.Execute(ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("chạy: %v %s (%s)", err, rep.Job.Status, rep.Job.Error)
	}
	for _, c := range be.m.Ran() {
		if c.Name == "curl" || c.Name == "wget" {
			t.Errorf("backup.create chạm mạng: %s", c.String())
		}
	}
	if n := dialed.Load(); n != 0 {
		t.Errorf("backup.create dựng kho %d lần", n)
	}
	list, _ := be.b.List(ctx, dom)
	if len(list) != 1 || !list[0].Local || list[0].Remote != site.RemoteNone || list[0].JobID != rep.Job.ID {
		t.Errorf("dòng mới = %+v; muốn một dòng Local=true, Remote=\"\"", list)
	}
}

// ================================================================ B4 và các lời từ chối

func notOnMachine(t *testing.T, err error) *RefusedError {
	t.Helper()
	if refusedCode(t, err) != CodeBackupNotOnMachine {
		t.Fatalf("lỗi = %v; muốn %s", err, CodeBackupNotOnMachine)
	}
	var re *RefusedError
	errors.As(err, &re)
	return re
}

// B4: khôi phục bản chỉ còn trên kho (ở đích đang đặt) bị từ chối trước khi
// có bất cứ thứ gì được ghi; câu chỉ đúng lệnh tải về.
func TestKhoiPhucBanTrenKhoPhaiTaiVeTruoc(t *testing.T) {
	const dom = "shop.example.com"
	be := newBktEnv(t, dom)
	d := mustDomain(t, dom)
	tid := be.m18Target(t)
	r := be.stored(t, be.seedFull(t, dom, "20261101-000000", "x", "x", ReasonManual), tid, true)
	if r.Local || r.Remote != site.RemoteStored {
		t.Fatalf("dựng dòng: %+v", r)
	}

	before := be.m.Snapshot()
	be.m.ResetRan()
	for _, id := range []string{r.ID, ""} {
		_, err := be.b.PrepareRestore(ctx, d, id)
		re := notOnMachine(t, err)
		want := "tpan backup pull " + r.ID
		if !strings.Contains(re.Message.Vi, want) || !strings.Contains(re.Message.En, want) || !strings.Contains(re.Message.Vi, dom) {
			t.Errorf("id %q: câu từ chối thiếu %q hoặc domain: %+v", id, want, re.Message)
		}
		for _, leak := range []string{"s3.example.com", "tpan-thu", "tpan/may", r.RemoteKey} {
			if strings.Contains(re.Message.Vi+re.Message.En, leak) {
				t.Errorf("câu từ chối lộ chi tiết kho %q: %+v", leak, re.Message)
			}
		}
	}
	if after := be.m.Snapshot(); !reflect.DeepEqual(before, after) {
		t.Error("PrepareRestore bị từ chối mà máy giả đã đổi")
	}
	for _, c := range mutatingRan(be.m.Ran()) {
		t.Errorf("bị từ chối mà vẫn chạy lệnh ghi: %s", c)
	}
	// RestoreSource cho CLI biết id cần tải về, kể cả khi id rỗng.
	if got, err := be.b.RestoreSource(ctx, d, ""); err != nil || got.ID != r.ID {
		t.Errorf("RestoreSource = %s, %v; muốn %s", got.ID, err, r.ID)
	}
}

// File của dòng local=1 đã mất (hay bị thay bằng symlink) mà bản còn trên kho
// ở đích đang đặt: cũng là bản cần tải về, không phải backup_corrupt.
func TestKhoiPhucFileMatMaConTrenKho(t *testing.T) {
	const dom = "shop.example.com"
	be := newBktEnv(t, dom)
	d := mustDomain(t, dom)
	tid := be.m18Target(t)
	r := be.stored(t, be.seedFull(t, dom, "20261101-000000", "x", "x", ReasonManual), tid, false)
	if err := be.m.Remove(r.Path); err != nil {
		t.Fatal(err)
	}
	_, err := be.b.PrepareRestore(ctx, d, r.ID)
	if re := notOnMachine(t, err); !strings.Contains(re.Message.Vi, "tpan backup pull "+r.ID) {
		t.Errorf("file mất: %+v", re.Message)
	}
	be.m.SetSymlink("/etc/passwd", r.Path)
	if _, err := be.b.PrepareRestore(ctx, d, r.ID); refusedCode(t, err) != CodeBackupNotOnMachine {
		t.Errorf("symlink: %v", err)
	}
}

// Bản local=0 ở đích khác, hoặc khi chưa đặt đích nào: chỉ đường đặt lại đích.
func TestKhoiPhucBanODichKhac(t *testing.T) {
	const dom = "shop.example.com"
	be := newBktEnv(t, dom)
	d := mustDomain(t, dom)
	r := be.stored(t, be.seedFull(t, dom, "20261101-000000", "x", "x", ReasonManual), "0123456789abcdef", true)
	check := func(when string) {
		t.Helper()
		_, err := be.b.PrepareRestore(ctx, d, r.ID)
		re := notOnMachine(t, err)
		if !strings.Contains(re.Message.Vi, "đích khác") || !strings.Contains(re.Message.En, "different target") ||
			strings.Contains(re.Message.Vi, "backup pull") {
			t.Errorf("%s: %+v", when, re.Message)
		}
	}
	check("chưa đặt đích")
	be.m18Target(t)
	check("đích khác")
}

// Bản trên máy còn nguyên mà cũng ở đích đang đặt: khôi phục y như M9.
func TestKhoiPhucBanCaHaiNoiNhuM9(t *testing.T) {
	const dom = "shop.example.com"
	be := newBktEnv(t, dom)
	d := mustDomain(t, dom)
	tid := be.m18Target(t)
	r := be.stored(t, be.seedFull(t, dom, "20261101-000000", "x", "x", ReasonManual), tid, false)
	p, err := be.b.PrepareRestore(ctx, d, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ex := p.Plan.Steps[1].(*steps.BackupExtract); ex.Archive != r.Path {
		t.Errorf("nguồn = %s, muốn %s", ex.Archive, r.Path)
	}
}

func TestNeedsPull(t *testing.T) {
	const dom = "shop.example.com"
	be := newBktEnv(t, dom)
	plain := be.seedFull(t, dom, "20261101-000000", "a", "a", ReasonManual)
	both := be.stored(t, be.seedFull(t, dom, "20261102-000000", "b", "b", ReasonManual), "", false)
	// Chưa đặt đích: không bản nào cần tải về. Setting s3.* hỏng không chặn
	// dòng chưa stored hay dòng có file trên máy (không đọc settings); chỉ
	// dòng không có file mới cần đích, và setting hỏng thì lỗi, không đoán.
	mustSet(t, be.st, SettingS3Endpoint, "ftp://hong")
	if need, err := be.b.NeedsPull(ctx, plain); err != nil || need {
		t.Errorf("dòng chưa lên kho, setting hỏng: %v %v", need, err)
	}
	if need, err := be.b.NeedsPull(ctx, both); err != nil || need {
		t.Errorf("dòng stored còn file, setting hỏng: %v %v", need, err)
	}
	gone := both
	gone.Local = false
	if _, err := be.b.NeedsPull(ctx, gone); err == nil {
		t.Error("dòng local=0, setting hỏng: muốn lỗi, không đoán")
	}
	mustSet(t, be.st, SettingS3Endpoint, "")
	if need, err := be.b.NeedsPull(ctx, both); err != nil || need {
		t.Errorf("chưa đặt đích: %v %v", need, err)
	}
	tid := be.m18Target(t)
	both = be.stored(t, both, tid, false)
	cases := []struct {
		name string
		r    func() Backup
		want bool
	}{
		{"chưa lên kho", func() Backup { return plain }, false},
		{"trên máy và trên kho", func() Backup { return both }, false},
		{"đích khác", func() Backup { r := both; r.RemoteTarget = "ffffffffffffffff"; r.Local = false; return r }, false},
		{"local=0", func() Backup { r := both; r.Local = false; return r }, true},
		{"failed", func() Backup { r := both; r.Remote = site.RemoteFailed; r.Local = false; return r }, false},
	}
	for _, c := range cases {
		if got, err := be.b.NeedsPull(ctx, c.r()); err != nil || got != c.want {
			t.Errorf("%s: %v %v; muốn %v", c.name, got, err, c.want)
		}
	}
	if err := be.m.Remove(both.Path); err != nil {
		t.Fatal(err)
	}
	be.m.SetDir(both.Path, 0o700, system.Root)
	if got, err := be.b.NeedsPull(ctx, both); err != nil || !got {
		t.Errorf("Path là thư mục: %v %v; muốn true", got, err)
	}
}

// ================================================================ prune

func (be *bktEnv) ids(t *testing.T, dom string) []string {
	t.Helper()
	list, err := be.b.List(ctx, dom)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, r := range list {
		out = append(out, r.ID)
	}
	return out
}

// keep = 1, ba bản full: bản stored mất file mà giữ dòng (local=0), bản chưa
// lên kho mất cả file lẫn dòng.
func TestDonTrenMayGiuDongDaLenKho(t *testing.T) {
	const dom = "shop.example.com"
	be := newBktEnv(t, dom)
	tid := be.m18Target(t)
	mustSet(t, be.st, SettingBackupKeep, "1")
	f1 := be.stored(t, be.seedFull(t, dom, "20261101-000000", "1", "1", ReasonManual), tid, false)
	f2 := be.seedFull(t, dom, "20261102-000000", "2", "2", ReasonSchedule)
	f3 := be.seedFull(t, dom, "20261103-000000", "3", "3", ReasonManual)
	if err := be.b.prune(ctx, dom); err != nil {
		t.Fatal(err)
	}
	if got, want := be.ids(t, dom), []string{f3.ID, f1.ID}; !reflect.DeepEqual(got, want) {
		t.Errorf("còn %v, muốn %v", got, want)
	}
	r1, _ := be.row(t, f1.ID)
	if r1.Local || r1.Remote != site.RemoteStored || r1.RemoteTarget != tid || be.exists(f1.Path) {
		t.Errorf("bản đã lên kho sau dọn: %+v, file còn=%v", r1, be.exists(f1.Path))
	}
	if be.exists(f2.Path) || !be.exists(f3.Path) {
		t.Errorf("file: f2 còn=%v, f3 còn=%v", be.exists(f2.Path), be.exists(f3.Path))
	}
	// Chạy lại: bản chỉ còn trên kho không bị đụng nữa.
	if err := be.b.prune(ctx, dom); err != nil {
		t.Fatal(err)
	}
	if r, ok := be.row(t, f1.ID); !ok || !reflect.DeepEqual(r, r1) {
		t.Errorf("lần dọn thứ hai đổi bản chỉ còn trên kho: %+v %v", r, ok)
	}
}

// Bản chỉ còn trên kho không chiếm chỗ trong keep và không bị dọn ở đây.
func TestDonTrenMayKhongDemBanChiTrenKho(t *testing.T) {
	const dom = "shop.example.com"
	be := newBktEnv(t, dom)
	tid := be.m18Target(t)
	mustSet(t, be.st, SettingBackupKeep, "1")
	b := be.seedFull(t, dom, "20261101-000000", "b", "b", ReasonManual)
	a := be.seedFull(t, dom, "20261103-000000", "a", "a", ReasonManual)
	ro := be.stored(t, be.seedFull(t, dom, "20261105-000000", "r", "r", ReasonManual), tid, true)
	ro2 := be.stored(t, be.seedFull(t, dom, "20261104-000000", "r2", "r2", ReasonSchedule), "0123456789abcdef", true)
	if err := be.b.prune(ctx, dom); err != nil {
		t.Fatal(err)
	}
	if got, want := be.ids(t, dom), []string{ro.ID, ro2.ID, a.ID}; !reflect.DeepEqual(got, want) {
		t.Errorf("còn %v, muốn %v (bản trên máy mới nhất a phải được giữ)", got, want)
	}
	if !be.exists(a.Path) || be.exists(b.Path) {
		t.Errorf("file: a còn=%v, b còn=%v", be.exists(a.Path), be.exists(b.Path))
	}
	for _, r := range []Backup{ro, ro2} {
		if got, ok := be.row(t, r.ID); !ok || !reflect.DeepEqual(got, r) {
			t.Errorf("bản chỉ còn trên kho bị đổi: %+v %v", got, ok)
		}
	}
}

// Dòng local=0 mà không stored: dọn dòng, không xoá file (dòng không còn nói
// file nào là của nó); bản before-purge thì không bao giờ đụng.
func TestDonTrenMayXoaDongMa(t *testing.T) {
	const dom = "shop.example.com"
	be := newBktEnv(t, dom)
	keepFile := be.seedFull(t, dom, "20261101-000000", "k", "k", ReasonManual)
	g := &ghostStore{bktStore: be.bs}
	be.b.Store = g
	ghost := Backup{ID: "aaaaaaaaaaaaaaaa", Domain: dom, Kind: BackupFull, Reason: ReasonManual,
		Path: keepFile.Path, Created: time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC), Local: false, Remote: site.RemoteNone}
	failed := ghost
	failed.ID, failed.Remote, failed.Created = "bbbbbbbbbbbbbbbb", site.RemoteFailed, ghost.Created.Add(time.Hour)
	purge := ghost
	purge.ID, purge.Reason = "cccccccccccccccc", ReasonBeforePurge
	g.ghosts = []Backup{ghost, failed, purge}
	if err := be.b.prune(ctx, dom); err != nil {
		t.Fatal(err)
	}
	if got, want := be.ids(t, dom), []string{purge.ID, keepFile.ID}; !reflect.DeepEqual(got, want) {
		t.Errorf("còn %v, muốn %v", got, want)
	}
	if !be.exists(keepFile.Path) {
		t.Error("dọn dòng ma mà xoá file")
	}
}

// ================================================================ fullCount, chọn mặc định

// Hạn mức gói chỉ đếm bản trên máy: bản cũ đã chỉ còn trên kho không chặn oan.
func TestHanMucGoiChiDemBanTrenMay(t *testing.T) {
	const dom = "shop.example.com"
	be := newBktEnv(t, dom)
	d := mustDomain(t, dom)
	tid := be.m18Target(t)
	be.bktOwn(t, dom, 2)
	be.stored(t, be.seedFull(t, dom, "20261101-000000", "1", "1", ReasonManual), tid, true)
	be.stored(t, be.seedFull(t, dom, "20261102-000000", "2", "2", ReasonSchedule), tid, true)
	be.seedFull(t, dom, "20261103-000000", "3", "3", ReasonManual)
	if n, err := be.b.fullCount(ctx, dom); err != nil || n != 1 {
		t.Errorf("fullCount = %d, %v; muốn 1", n, err)
	}
	if _, err := be.b.PrepareCreate(ctx, d, ReasonManual); err != nil {
		t.Errorf("1/2 bản trên máy mà bị chặn: %v", err)
	}
}

func TestChonMacDinhBoQuaBeforeRestore(t *testing.T) {
	const dom = "shop.example.com"
	be := newBktEnv(t, dom)
	d := mustDomain(t, dom)
	pre := be.seedFull(t, dom, "20261102-000000", "pre", "pre", ReasonBeforeRestore)
	// Chỉ có before-restore: lấy nó (như M9).
	if got, err := be.b.restoreSource(ctx, d, ""); err != nil || got.ID != pre.ID {
		t.Errorf("chỉ có before-restore: %s, %v", got.ID, err)
	}
	man := be.seedFull(t, dom, "20261101-000000", "man", "man", ReasonManual)
	if got, err := be.b.restoreSource(ctx, d, ""); err != nil || got.ID != man.ID {
		t.Errorf("id rỗng chọn %s (%s), muốn bản manual %s", got.ID, got.Reason, man.ID)
	}
	if got, err := be.b.RestoreSource(ctx, d, pre.ID); err != nil || got.ID != pre.ID {
		t.Errorf("id chỉ định: %s, %v", got.ID, err)
	}
	if _, err := be.b.RestoreSource(ctx, d, "$(id)"); refusedCode(t, err) != "backup_not_found" {
		t.Errorf("id sai dạng: %v", err)
	}
}
