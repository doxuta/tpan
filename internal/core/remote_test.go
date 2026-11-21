package core

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/s3"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

// ================================================================ môi trường thử

var rtNow = time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)

// Hai khoá đánh dấu: test quét mọi đầu ra tìm hai chuỗi này (B3).
const (
	rtAccess = "AKIARTEXAMPLE0001KEY"
	rtSecret = "rtSecretKHONGduocLO0123456789abc"
)

// rtCurID là mã của đích setTarget ghi.
var rtCurID = s3.TargetID("https://s3.example.com", "tpan-thu", "tpan/may")

type rtEnv struct {
	ctx    context.Context
	st     *store.Store
	sys    *system.FakeSystem
	mem    *s3.Mem
	obj    steps.ObjectStore // kho NewStore trả; mặc định mem
	builds int               // số lần NewStore được gọi
	r      *Remote
}

func rtSHA(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// newRtEnv: store thật trong thư mục tạm, FakeSystem có sha256sum và tar giả,
// kho giả s3.Mem, khoá lượt đẩy trong bộ nhớ, giờ cố định, đích đã đặt
// (s3.keep = 2) và hai khoá truy cập đã lưu. Không có khoá sao lưu: lượt đẩy
// không làm gói panel trừ khi test đặt khoá.
func newRtEnv(t *testing.T) *rtEnv {
	t.Helper()
	e := &rtEnv{ctx: context.Background(), st: remoteTestStore(t), sys: system.NewFake(), mem: s3.NewMem()}
	e.obj = e.mem
	e.mem.Now = func() time.Time { return rtNow }
	f := e.sys
	f.SetDir("/var/lib/tpan", 0o755, system.Root)
	f.Handle([]string{"sha256sum"}, func(c system.Cmd) system.Result {
		p := c.Args[len(c.Args)-1]
		data, err := f.ReadFile(p)
		if err != nil {
			return system.Result{ExitCode: 1, Stderr: err.Error()}
		}
		return system.Result{Stdout: rtSHA(data) + "  " + p + "\n"}
	})
	// tar giả: file nén là JSON của map[tên]nội dung (như test của steps).
	f.Handle([]string{"tar", "--create"}, func(c system.Cmd) system.Result {
		var file, dir string
		files := map[string]string{}
		for _, a := range c.Args[1:] {
			switch {
			case strings.HasPrefix(a, "--file="):
				file = strings.TrimPrefix(a, "--file=")
			case strings.HasPrefix(a, "--directory="):
				dir = strings.TrimPrefix(a, "--directory=")
			case strings.HasPrefix(a, "--"):
			default:
				data, err := f.ReadFile(dir + "/" + a)
				if err != nil {
					return system.Result{ExitCode: 2, Stderr: "tar: " + err.Error()}
				}
				files[a] = string(data)
			}
		}
		data, _ := json.Marshal(files)
		f.SetFile(file, data, 0o644, system.Root)
		return system.Result{}
	})
	setTarget(t, e.st)
	keys, err := encodeKeys(rtAccess, system.NewSecret(rtSecret))
	if err != nil {
		t.Fatal(err)
	}
	f.SetFile(steps.S3KeysFile, []byte(keys.Reveal()), 0o600, system.Root)
	e.r = &Remote{
		Sys: f, Settings: e.st, Store: e.st, Sites: e.st, Journal: e.st, Hostname: "may1", Version: "0.3.0",
		NewStore: func(s3.Config) (steps.ObjectStore, error) {
			e.builds++
			return e.obj, nil
		},
		Lock: system.NewMemLock(), LockWait: 2 * time.Second, Now: func() time.Time { return rtNow },
	}
	return e
}

// addFull thêm một bản full trên máy (file trong máy giả, dòng trong DB).
func (e *rtEnv) addFull(t *testing.T, domain, stamp string, created time.Time, reason BackupReason) Backup {
	t.Helper()
	data := []byte("ban sao luu " + domain + " " + stamp + " du dai de doi mot byte")
	p := site.BackupRoot + "/" + domain + "/" + domain + "-" + stamp + ".tar.gz"
	e.sys.SetFile(p, data, 0o600, system.Root)
	b, err := e.st.AddBackup(e.ctx, Backup{Domain: domain, Kind: BackupFull, Reason: reason, Path: p,
		Size: int64(len(data)), SHA256: rtSHA(data), Created: created})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func (e *rtEnv) row(t *testing.T, id string) Backup {
	t.Helper()
	b, ok, err := e.st.Backup(e.ctx, id)
	if err != nil || !ok {
		t.Fatalf("đọc dòng %s: ok=%v err=%v", id, ok, err)
	}
	return b
}

func (e *rtEnv) setting(t *testing.T, k string) string {
	t.Helper()
	v, _, err := e.st.GetSetting(e.ctx, k)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// remoteKey là khoá object của một bản.
func rtKey(b Backup) string { return "sites/" + b.Domain + "/" + path.Base(b.Path) }

// stored đánh dấu b đã lên kho ở targetID; withObject: đặt object đúng nội dung vào Mem.
func (e *rtEnv) stored(t *testing.T, b Backup, targetID string, withObject bool) {
	t.Helper()
	if ok, err := e.st.MarkBackupStored(e.ctx, b.ID, rtKey(b), targetID, rtNow); err != nil || !ok {
		t.Fatalf("MarkBackupStored: %v %v", ok, err)
	}
	if withObject {
		data, err := e.sys.ReadFile(b.Path)
		if err != nil {
			t.Fatal(err)
		}
		e.mem.SetObject(rtKey(b), data)
	}
}

func (e *rtEnv) sync(t *testing.T, opt SyncOptions) SyncResult {
	t.Helper()
	res, err := e.r.Sync(e.ctx, opt, nil)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	return res
}

// ================================================================ luật thuần

func TestRemoteSurvives(t *testing.T) {
	at := func(h int) time.Time { return rtNow.Add(time.Duration(-h) * time.Hour) }
	mk := func(id string, h int, reason BackupReason, stored bool, target string) Backup {
		b := Backup{ID: id, Domain: "a.com", Kind: BackupFull, Reason: reason, Created: at(h)}
		if stored {
			b.Remote, b.RemoteTarget = site.RemoteStored, target
		}
		return b
	}
	rows := []Backup{ // mới nhất trước, như Store.Backups
		mk("r2", 0, ReasonBeforeRestore, false, ""),
		mk("b5", 1, ReasonSchedule, false, ""),
		mk("r1", 2, ReasonBeforeRestore, true, "T"), // R8: không chiếm chỗ của bản lịch
		mk("b4", 3, ReasonSchedule, true, "T"),
		mk("x", 3, ReasonManual, true, "KHAC"), // đích khác: không đếm
		{ID: "o", Domain: "b.com", Kind: BackupFull, Reason: ReasonSchedule, Created: at(3), Remote: site.RemoteStored, RemoteTarget: "T"},
		mk("b3", 4, ReasonManual, true, "T"),
		mk("b2", 5, ReasonSchedule, false, ""),
		mk("p1", 9, ReasonBeforePurge, false, ""),
	}
	want := map[string]bool{"r2": true, "b5": true, "r1": false, "b4": true, "b3": true, "b2": false, "p1": true}
	for _, r := range rows {
		w, ok := want[r.ID]
		if !ok {
			continue
		}
		if got := remoteSurvives(r, rows, "T", 2); got != w {
			t.Errorf("remoteSurvives(%s) = %v, muốn %v", r.ID, got, w)
		}
	}
	// Cùng giờ: đứng trước trong rows là mới hơn.
	tie := []Backup{mk("n", 1, ReasonSchedule, true, "T"), mk("m", 1, ReasonSchedule, false, "")}
	if remoteSurvives(tie[1], tie, "T", 1) || !remoteSurvives(tie[0], tie, "T", 1) {
		t.Error("cùng giờ phải theo thứ tự của rows")
	}
}

// ================================================================ lượt đẩy

// B10: lượt đẩy không lấy khoá job của máy.
func TestSyncKhongGiuKhoaToanCuc(t *testing.T) {
	e := newRtEnv(t)
	b := e.addFull(t, "example.com", "20261121-090000", rtNow.Add(-time.Hour), ReasonSchedule)
	unlock, err := e.sys.Lock(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(e.ctx, 5*time.Second)
	defer cancel()
	res, err := e.r.Sync(ctx, SyncOptions{}, nil)
	if err != nil || len(res.Pushed) != 1 || res.Pushed[0] != b.ID {
		t.Fatalf("Sync khi khoá job đang bị giữ: %+v, %v", res, err)
	}
	if r := e.row(t, b.ID); r.Remote != site.RemoteStored || r.RemoteTarget != rtCurID || r.RemoteKey != rtKey(b) {
		t.Fatalf("dòng sau khi đẩy: %+v", r)
	}
	if len(res.Reports) != 1 || res.Reports[0].Job.Owner != "admin" {
		t.Fatalf("job đẩy phải thuộc admin: %+v", res.Reports)
	}
}

// B10: tải về không lấy khoá job của máy.
func TestPullKhongGiuKhoaToanCuc(t *testing.T) {
	e := newRtEnv(t)
	b := e.addFull(t, "example.com", "20261121-090000", rtNow.Add(-time.Hour), ReasonSchedule)
	data, _ := e.sys.ReadFile(b.Path)
	e.stored(t, b, rtCurID, true)
	if _, err := e.st.ForgetBackupLocal(e.ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.sys.Remove(b.Path); err != nil {
		t.Fatal(err)
	}
	unlock, err := e.sys.Lock(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(e.ctx, 5*time.Second)
	defer cancel()
	rep, err := e.r.Pull(ctx, b.ID, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("Pull: %+v, %v", rep.Job, err)
	}
	if !e.row(t, b.ID).Local {
		t.Fatal("sau khi tải về dòng phải local=1")
	}
	if got, err := e.sys.ReadFile(b.Path); err != nil || string(got) != string(data) {
		t.Fatalf("file tải về: %q, %v", got, err)
	}
}

// rtTamper trả dữ liệu khác thứ đã gửi khi Get (đĩa của kho hỏng sau lúc ghi).
type rtTamper struct{ *s3.Mem }

func (k rtTamper) Get(ctx context.Context, key string, w io.Writer) (int64, string, error) {
	k.Corrupt(key)
	return k.Mem.Get(ctx, key, w)
}

// B2: không đánh dấu stored khi đọc lại không khớp.
func TestDayChiDanhDauSauKhiDocLai(t *testing.T) {
	e := newRtEnv(t)
	e.obj = rtTamper{e.mem}
	b := e.addFull(t, "example.com", "20261121-090000", rtNow.Add(-time.Hour), ReasonSchedule)
	res := e.sync(t, SyncOptions{})
	if len(res.Pushed) != 0 || len(res.Failed) != 1 {
		t.Fatalf("kết quả: %+v", res)
	}
	r := e.row(t, b.ID)
	if r.Remote != site.RemoteFailed || r.RemoteTries != 1 {
		t.Fatalf("dòng phải failed, tries 1: %+v", r)
	}
	if len(e.mem.Objects()) != 0 {
		t.Fatalf("object không khớp phải bị xoá: %v", e.mem.Objects())
	}
}

// B14 và R8: bản đã bị dọn khỏi kho không bao giờ tự được đẩy lại; bản
// before-restore không đẩy bản lịch cũ nhất khỏi kho.
func TestDonTrenKhoKhongDayLai(t *testing.T) {
	e := newRtEnv(t) // s3.keep = 2
	var bs []Backup
	for i, stamp := range []string{"20261121-070000", "20261121-080000", "20261121-090000"} {
		bs = append(bs, e.addFull(t, "example.com", stamp, rtNow.Add(time.Duration(i-3)*time.Hour), ReasonSchedule))
		if res := e.sync(t, SyncOptions{}); len(res.Pushed) != 1 {
			t.Fatalf("lượt %d: %+v", i, res)
		}
	}
	b1 := e.row(t, bs[0].ID)
	if b1.Remote != site.RemoteNone || !b1.Local {
		t.Fatalf("bản cũ nhất phải bị dọn khỏi kho, còn trên máy: %+v", b1)
	}
	if _, ok := e.mem.Objects()[rtKey(bs[0])]; ok || len(e.mem.Objects()) != 2 {
		t.Fatalf("kho phải còn đúng hai bản mới: %v", e.mem.Objects())
	}
	puts := e.mem.Calls("put")
	if res := e.sync(t, SyncOptions{}); len(res.Reports) != 0 || len(res.Pushed) != 0 || e.mem.Calls("put") != puts {
		t.Fatalf("lượt sau không được đẩy lại: %+v, put %d -> %d", res, puts, e.mem.Calls("put"))
	}
	// R8: bản before-restore lên kho mà bản lịch thứ hai vẫn còn.
	rb := e.addFull(t, "example.com", "20261121-093000", rtNow.Add(-30*time.Minute), ReasonBeforeRestore)
	if res := e.sync(t, SyncOptions{}); len(res.Pushed) != 1 || res.Pushed[0] != rb.ID {
		t.Fatalf("bản before-restore: %+v", res)
	}
	if r := e.row(t, bs[1].ID); r.Remote != site.RemoteStored {
		t.Fatalf("bản before-restore không được đẩy bản lịch khỏi kho: %+v", r)
	}
}

// B9: lần tải dở cũ hơn một ngày bị huỷ.
func TestSyncHuyTaiDoCu(t *testing.T) {
	e := newRtEnv(t)
	e.mem.AddUpload("sites/x.com/cu.tar.gz", rtNow.Add(-25*time.Hour))
	e.mem.AddUpload("sites/x.com/moi.tar.gz", rtNow.Add(-time.Hour))
	e.addFull(t, "example.com", "20261121-090000", rtNow.Add(-time.Hour), ReasonSchedule)
	e.sync(t, SyncOptions{})
	if got := e.mem.Uploads(); len(got) != 1 || got[0] != "sites/x.com/moi.tar.gz" {
		t.Fatalf("còn lại: %v", got)
	}
}

func TestSyncKhongViecKhongGoiMang(t *testing.T) {
	e := newRtEnv(t)
	res := e.sync(t, SyncOptions{})
	if !res.Ran || e.mem.Calls("check") != 0 || e.builds != 0 {
		t.Fatalf("không có việc mà gọi mạng: %+v, check=%d, builds=%d", res, e.mem.Calls("check"), e.builds)
	}
	// Chưa đặt đích: không chạy, không lỗi.
	if err := e.st.SetSetting(e.ctx, SettingS3Endpoint, ""); err != nil {
		t.Fatal(err)
	}
	if res := e.sync(t, SyncOptions{}); res.Ran || res.Skipped == "" {
		t.Fatalf("chưa đặt đích: %+v", res)
	}
}

func TestSyncKhoaBanThiBo(t *testing.T) {
	e := newRtEnv(t)
	unlock, ok, err := e.r.Lock.Acquire(e.ctx, 0)
	if err != nil || !ok {
		t.Fatal(err)
	}
	defer unlock()
	if res := e.sync(t, SyncOptions{}); res.Ran || res.Skipped == "" {
		t.Fatalf("khoá lượt đẩy bận: %+v", res)
	}
}

// Cầu dao và nhịp lùi (R2).
func TestSyncCauDaoVaNhipLui(t *testing.T) {
	e := newRtEnv(t)
	b1 := e.addFull(t, "example.com", "20261121-080000", rtNow.Add(-2*time.Hour), ReasonSchedule)
	b2 := e.addFull(t, "example.com", "20261121-090000", rtNow.Add(-time.Hour), ReasonSchedule)
	e.mem.Fail("put", "", &s3.Error{Op: "put", Status: 503, Code: "SlowDown"})
	res := e.sync(t, SyncOptions{})
	if len(res.Failed) != 1 || res.Failed[0].ID != b2.ID || e.mem.Calls("put") != 1 || res.TargetError == "" {
		t.Fatalf("bản đầu hỏng thì dừng: %+v, put=%d", res, e.mem.Calls("put"))
	}
	if e.setting(t, SettingS3TargetError) == "" || e.setting(t, SettingS3TargetFailures) != "1" ||
		e.setting(t, SettingS3RetryAt) != rtNow.Add(10*time.Minute).Format(time.RFC3339) {
		t.Fatalf("lỗi và nhịp lùi: %q %q %q", e.setting(t, SettingS3TargetError), e.setting(t, SettingS3TargetFailures), e.setting(t, SettingS3RetryAt))
	}
	// R2b: Check lại vẫn đạt nên kho từ chối riêng bản này: đếm.
	if r := e.row(t, b2.ID); r.Remote != site.RemoteFailed || r.RemoteTries != 1 {
		t.Fatalf("b2: %+v", r)
	}
	if r := e.row(t, b1.ID); r.Remote != site.RemoteNone {
		t.Fatalf("b1 không được thử: %+v", r)
	}
	e.mem.Fail("put", "", nil)
	if res := e.sync(t, SyncOptions{}); res.Ran || res.Skipped == "" || e.mem.Calls("put") != 1 {
		t.Fatalf("đang lùi thì bỏ lượt: %+v", res)
	}
	res = e.sync(t, SyncOptions{Retry: true})
	if len(res.Pushed) != 2 {
		t.Fatalf("Retry phải chạy: %+v", res)
	}
	for _, k := range []string{SettingS3TargetError, SettingS3TargetFailures, SettingS3RetryAt} {
		if v := e.setting(t, k); v != "" {
			t.Fatalf("đẩy được thì xoá %s, còn %q", k, v)
		}
	}
}

// rtFlaky: Check đạt lần đầu, hỏng từ lần thứ hai (kho sập giữa lượt).
type rtFlaky struct {
	*s3.Mem
	n *int
}

func (k rtFlaky) Check(ctx context.Context) error {
	if *k.n++; *k.n > 1 {
		return &s3.Error{Op: "check", Status: 0, Code: "", Message: "dial tcp: connection refused"}
	}
	return k.Mem.Check(ctx)
}

// R2b: lỗi của đích giữa lượt mà Check lại cũng hỏng: không đếm, lỗi của
// đích là lỗi của lần Check lại.
func TestSyncKhoSapGiuaLuot(t *testing.T) {
	e := newRtEnv(t)
	e.obj = rtFlaky{e.mem, new(int)}
	b := e.addFull(t, "example.com", "20261121-090000", rtNow.Add(-time.Hour), ReasonSchedule)
	e.mem.Fail("put", "", &s3.Error{Op: "put", Status: 503, Code: "SlowDown"})
	e.sync(t, SyncOptions{})
	if r := e.row(t, b.ID); r.Remote != site.RemoteFailed || r.RemoteTries != 0 {
		t.Fatalf("dòng: %+v", r)
	}
	if te := e.setting(t, SettingS3TargetError); !strings.Contains(te, "connection refused") {
		t.Fatalf("target_error = %q", te)
	}
}

// Check của lượt hỏng: không job nào, lùi tăng dần, Check đạt chưa xoá lùi (R2a).
func TestSyncCheckHongThiLui(t *testing.T) {
	e := newRtEnv(t)
	e.addFull(t, "example.com", "20261121-090000", rtNow.Add(-time.Hour), ReasonSchedule)
	e.mem.Fail("check", "", &s3.Error{Op: "check", Status: 403, Code: "AccessDenied"})
	for i, want := range []time.Duration{10 * time.Minute, 30 * time.Minute} {
		res := e.sync(t, SyncOptions{Retry: true})
		if len(res.Reports) != 0 || res.TargetError == "" {
			t.Fatalf("lượt %d: %+v", i, res)
		}
		if got := e.setting(t, SettingS3RetryAt); got != rtNow.Add(want).Format(time.RFC3339) {
			t.Fatalf("lượt %d: retry_at %s", i, got)
		}
	}
	// Check đạt mà PUT hỏng: lùi tiếp, không về 0.
	e.mem.Fail("check", "", nil)
	e.mem.Fail("put", "", &s3.Error{Op: "put", Status: 403, Code: "AccessDenied"})
	e.sync(t, SyncOptions{Retry: true})
	if got := e.setting(t, SettingS3TargetFailures); got != "3" {
		t.Fatalf("target_failures = %q, muốn 3", got)
	}
}

// Lỗi của riêng bản (file cục bộ sai sha256): đếm, đi tiếp bản sau.
func TestSyncBanCucBoSaiSha256(t *testing.T) {
	e := newRtEnv(t)
	b1 := e.addFull(t, "example.com", "20261121-080000", rtNow.Add(-2*time.Hour), ReasonSchedule)
	b2 := e.addFull(t, "example.com", "20261121-090000", rtNow.Add(-time.Hour), ReasonSchedule)
	e.sys.SetFile(b2.Path, []byte(strings.Repeat("x", int(b2.Size))), 0o600, system.Root) // cùng cỡ, khác nội dung
	res := e.sync(t, SyncOptions{})
	if len(res.Pushed) != 1 || res.Pushed[0] != b1.ID || len(res.Failed) != 1 {
		t.Fatalf("kết quả: %+v", res)
	}
	if r := e.row(t, b2.ID); r.Remote != site.RemoteFailed || r.RemoteTries != 1 || r.RemoteError == "" {
		t.Fatalf("b2: %+v", r)
	}
	if e.mem.Calls("put") != 1 {
		t.Fatalf("bản hỏng không được gửi byte nào: put=%d", e.mem.Calls("put"))
	}
}

// R9: Only vẫn áp luật chọn; bản đã stored ở đích này chỉ đẩy lại khi object mất.
func TestSyncOnly(t *testing.T) {
	e := newRtEnv(t)
	b := e.addFull(t, "example.com", "20261121-090000", rtNow.Add(-time.Hour), ReasonSchedule)
	e.stored(t, b, rtCurID, true)
	res := e.sync(t, SyncOptions{Only: []string{b.ID, "abcdef0123456789"}, Retry: true})
	if len(res.Pushed) != 1 || e.mem.Calls("put") != 0 || len(res.Failed) != 1 || res.Failed[0].ID != "abcdef0123456789" {
		t.Fatalf("object còn thì không tải lại: %+v put=%d", res, e.mem.Calls("put"))
	}
	if err := e.mem.Delete(e.ctx, rtKey(b)); err != nil {
		t.Fatal(err)
	}
	res = e.sync(t, SyncOptions{Only: []string{b.ID}, Retry: true})
	if len(res.Pushed) != 1 || e.mem.Calls("put") != 1 || e.row(t, b.ID).Remote != site.RemoteStored {
		t.Fatalf("object mất thì đẩy lại: %+v", res)
	}
}

// ================================================================ đích

// Nối lại khi đổi đích (S18, R5).
func TestDatDichNoiLai(t *testing.T) {
	e := newRtEnv(t)
	if err := e.st.SetSetting(e.ctx, SettingS3Endpoint, "https://cu.example.com"); err != nil {
		t.Fatal(err)
	}
	oldID := s3.TargetID("https://cu.example.com", "tpan-thu", "tpan/may")
	b1 := e.addFull(t, "example.com", "20261121-080000", rtNow.Add(-2*time.Hour), ReasonSchedule)
	b2 := e.addFull(t, "example.com", "20261121-090000", rtNow.Add(-time.Hour), ReasonSchedule)
	e.stored(t, b1, oldID, true)
	e.stored(t, b2, oldID, false)
	e.mem.SetObject(rtKey(b2), []byte("sai co"))
	in := TargetInput{Endpoint: "https://S3.example.com/", Bucket: "tpan-thu", Prefix: "tpan/may", PathStyle: true, Keep: 2,
		AccessKey: rtAccess, SecretKey: system.NewSecret(rtSecret)}
	ts, err := e.r.PrepareSetTarget(e.ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := ts.Execute(e.ctx, nil)
	if err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("Execute: %+v %v", rep.Job, err)
	}
	if ts.Result == nil || ts.Result.Relinked != 1 || ts.Result.OtherTarget != 1 {
		t.Fatalf("Result = %+v", ts.Result)
	}
	if e.row(t, b1.ID).RemoteTarget != rtCurID || e.row(t, b2.ID).RemoteTarget != oldID {
		t.Fatal("nối lại sai dòng")
	}
	if e.setting(t, SettingS3Endpoint) != "https://s3.example.com" || e.setting(t, SettingS3PanelDue) != "1" || e.setting(t, SettingS3RelinkDue) != "" {
		t.Fatal("setting sau khi đặt đích sai")
	}
	if ts.NewKey.Reveal() != "" {
		t.Fatal("không GenerateKey thì không sinh khoá")
	}
}

// R5: nối lại hỏng thì cờ relink_due còn, chạy lại cùng đích thì nối tiếp.
func TestDatDichNoiLaiChayLai(t *testing.T) {
	e := newRtEnv(t)
	if err := e.st.SetSetting(e.ctx, SettingS3Endpoint, "https://cu.example.com"); err != nil {
		t.Fatal(err)
	}
	b := e.addFull(t, "example.com", "20261121-080000", rtNow.Add(-2*time.Hour), ReasonSchedule)
	e.stored(t, b, s3.TargetID("https://cu.example.com", "tpan-thu", "tpan/may"), true)
	in := TargetInput{Endpoint: "https://s3.example.com", Bucket: "tpan-thu", Prefix: "tpan/may", PathStyle: true, Keep: 2,
		AccessKey: rtAccess, SecretKey: system.NewSecret(rtSecret)}
	e.mem.Fail("list", "sites/", &s3.Error{Op: "list", Status: 503, Code: "SlowDown"})
	ts, err := e.r.PrepareSetTarget(e.ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ts.Execute(e.ctx, nil); err == nil || !strings.Contains(err.Error(), "chạy lại lệnh") || e.setting(t, SettingS3RelinkDue) != "1" {
		t.Fatalf("nối lại hỏng: %v, relink_due=%q", err, e.setting(t, SettingS3RelinkDue))
	}
	e.mem.Fail("list", "sites/", nil)
	if ts, err = e.r.PrepareSetTarget(e.ctx, in); err != nil {
		t.Fatal(err)
	}
	if _, err := ts.Execute(e.ctx, nil); err != nil || ts.Result == nil || ts.Result.Relinked != 1 || e.setting(t, SettingS3RelinkDue) != "" {
		t.Fatalf("chạy lại: %v, %+v", err, ts.Result)
	}
}

// R4: đặt, gỡ đích và sinh khoá chờ khoá lượt đẩy; lượt đẩy đang chạy thì không ghi gì.
func TestSinhKhoaChoLuotDay(t *testing.T) {
	e := newRtEnv(t)
	unlock, ok, err := e.r.Lock.Acquire(e.ctx, 0)
	if err != nil || !ok {
		t.Fatal(err)
	}
	defer unlock()
	e.r.LockWait = 50 * time.Millisecond
	kn, err := e.r.PrepareNewKey(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := kn.Execute(e.ctx, nil); err == nil {
		t.Fatal("lượt đẩy đang giữ khoá mà sinh khoá vẫn chạy")
	}
	if _, err := e.sys.Stat(steps.BackupKeyFile); err == nil {
		t.Fatal("không được ghi khoá khi chưa lấy được khoá lượt đẩy")
	}
}

// R1: giữ khoá đang lưu chỉ khi endpoint, vùng, bucket, CA không đổi; từ chối
// trước khi dựng kho (không request nào ký bằng khoá cũ đi ra ngoài).
func TestDatDichGiuKhoaChiKhiCungCho(t *testing.T) {
	e := newRtEnv(t)
	in := TargetInput{Endpoint: "https://k.example.com", Region: "vn-thu-1", Bucket: "tpan-thu", KeepKeys: true}
	_, err := e.r.PrepareSetTarget(e.ctx, in)
	var re *RefusedError
	if !errors.As(err, &re) || re.Code != s3.CodeKeysInvalid || e.builds != 0 {
		t.Fatalf("đổi endpoint mà giữ khoá: %v, builds=%d", err, e.builds)
	}
	in.Endpoint = "https://s3.example.com"
	if _, err := e.r.PrepareSetTarget(e.ctx, in); err != nil || e.builds != 1 {
		t.Fatalf("cùng chỗ thì giữ được khoá: %v, builds=%d", err, e.builds)
	}
}

func TestTargetKhongCoKhoa(t *testing.T) {
	e := newRtEnv(t)
	k := "tpan-key-1-" + strings.Repeat("a", 52)
	e.sys.SetFile(steps.BackupKeyFile, []byte(k+"\n"), 0o600, system.Root)
	v, err := e.r.Target(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(v)
	for _, s := range []string{rtAccess, rtSecret, k} {
		if strings.Contains(string(b), s) {
			t.Fatalf("Target lộ khoá: %s", b)
		}
	}
	if !v.Configured || !v.HasBackupKey || v.KeyFingerprint == "" || v.AccessKeyTail != "1KEY" || v.TargetID != rtCurID {
		t.Fatalf("Target = %s", b)
	}
}

// rtScanB64 giải base64 mọi chuỗi ở trường "old", "new" rồi quét bí mật.
func rtScanB64(t *testing.T, v any, secrets ...string) {
	t.Helper()
	switch x := v.(type) {
	case map[string]any:
		for k, c := range x {
			if s, ok := c.(string); ok && (k == "old" || k == "new") {
				dec, _ := base64.StdEncoding.DecodeString(s)
				for _, sec := range secrets {
					if strings.Contains(string(dec), sec) {
						t.Fatalf("trường %s lộ bí mật", k)
					}
				}
			}
			rtScanB64(t, c, secrets...)
		}
	case []any:
		for _, c := range x {
			rtScanB64(t, c, secrets...)
		}
	}
}

func TestGoDichXemTruocKhongCoKhoa(t *testing.T) {
	e := newRtEnv(t)
	e.sys.SetFile(steps.S3CAFile, []byte("CA-RIENG-DANH-DAU"), 0o600, system.Root)
	b := e.addFull(t, "example.com", "20261121-090000", rtNow.Add(-time.Hour), ReasonSchedule)
	e.stored(t, b, rtCurID, true)
	if _, err := e.st.ForgetBackupLocal(e.ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	p, err := e.r.PrepareRemoveTarget(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	pv, err := p.Preview(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(pv)
	for _, s := range []string{rtAccess, rtSecret, "CA-RIENG-DANH-DAU"} {
		if strings.Contains(string(raw), s) {
			t.Fatalf("bản xem trước lộ bí mật: %s", raw)
		}
	}
	var tree any
	_ = json.Unmarshal(raw, &tree)
	rtScanB64(t, tree, rtAccess, rtSecret, "CA-RIENG-DANH-DAU")
	if !strings.Contains(pv.Title.Vi, "1 bản chỉ còn trên kho") {
		t.Fatalf("tiêu đề phải nêu số bản chỉ còn trên kho: %q", pv.Title.Vi)
	}
	if rep, err := p.Execute(e.ctx, nil); err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("Execute: %+v %v", rep.Job, err)
	}
	if _, ok, _ := e.r.Config(e.ctx); ok {
		t.Fatal("gỡ xong mà vẫn còn đích")
	}
	if _, err := e.sys.Stat(steps.S3KeysFile); err == nil {
		t.Fatal("file khoá còn")
	}
	if e.row(t, b.ID).Remote != site.RemoteStored {
		t.Fatal("gỡ đích không đổi cột remote của dòng nào")
	}
}

// ================================================================ tải về, liệt kê

func TestPreparePullTuChoi(t *testing.T) {
	e := newRtEnv(t)
	code := func(err error) string {
		var re *RefusedError
		if errors.As(err, &re) {
			return re.Code
		}
		return "khong-phai-tu-choi: " + func() string {
			if err == nil {
				return "nil"
			}
			return err.Error()
		}()
	}
	local := e.addFull(t, "example.com", "20261121-080000", rtNow.Add(-2*time.Hour), ReasonSchedule)
	if _, err := e.r.PreparePull(e.ctx, local.ID); code(err) != CodeBackupAlreadyLocal {
		t.Fatalf("bản trên máy chưa lên kho: %v", err)
	}
	other := e.addFull(t, "example.com", "20261121-090000", rtNow.Add(-time.Hour), ReasonSchedule)
	e.stored(t, other, "khacdich00000000", true)
	if _, err := e.st.ForgetBackupLocal(e.ctx, other.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.r.PreparePull(e.ctx, other.ID); code(err) != CodeBackupNotOnMachine {
		t.Fatalf("bản ở đích khác: %v", err)
	}
	// R16: stored ở đích này mà file còn đúng: cho tải, bước báo đã xong.
	here := e.addFull(t, "example.com", "20261121-093000", rtNow.Add(-30*time.Minute), ReasonSchedule)
	e.stored(t, here, rtCurID, true)
	p, err := e.r.PreparePull(e.ctx, here.ID)
	if err != nil {
		t.Fatal(err)
	}
	pv, err := p.Preview(e.ctx)
	if err != nil || len(pv.Steps) != 1 || !pv.Steps[0].Done {
		t.Fatalf("Preview: %+v %v", pv, err)
	}
}

func TestListRemoteObjectLa(t *testing.T) {
	e := newRtEnv(t)
	b := e.addFull(t, "example.com", "20261121-090000", rtNow.Add(-time.Hour), ReasonSchedule)
	e.sync(t, SyncOptions{})
	gone := e.addFull(t, "b.example.com", "20261121-090000", rtNow.Add(-time.Hour), ReasonSchedule)
	e.stored(t, gone, rtCurID, false)
	e.mem.SetObject("la/khong-ai-biet.bin", []byte("x"))
	l, err := e.r.ListRemote(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, o := range l.Objects {
		got[o.Key] = o.BackupID
	}
	if id, ok := got["la/khong-ai-biet.bin"]; !ok || id != "" {
		t.Fatalf("object lạ: %+v", l.Objects)
	}
	if got[rtKey(b)] != b.ID {
		t.Fatalf("object của bản đã đẩy: %+v", l.Objects)
	}
	if len(l.Missing) != 1 || l.Missing[0].ID != gone.ID {
		t.Fatalf("Missing: %+v", l.Missing)
	}
}

// ================================================================ health

func TestRemoteStateVaProbe(t *testing.T) {
	e := newRtEnv(t)
	d1, d2 := "example.com", "b.example.com"
	for _, d := range []string{d1, d2} {
		dom := mustDomain(t, d)
		if err := e.st.SaveSite(e.ctx, site.Site{Domain: dom, Status: site.Active, PHPVersion: "8.3", Created: rtNow.Add(-72 * time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.st.SetSetting(e.ctx, SettingBackupAt, "03:00"); err != nil {
		t.Fatal(err)
	}
	b := e.addFull(t, d1, "20261121-090000", rtNow.Add(-time.Hour), ReasonSchedule)
	e.stored(t, b, rtCurID, true)
	g := e.addFull(t, d2, "20261121-090000", rtNow.Add(-time.Hour), ReasonSchedule)
	e.stored(t, g, rtCurID, false) // kho mất object
	st, err := e.r.State(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Schedule || len(st.Stale) != 0 || st.HasKey {
		t.Fatalf("State = %+v", st)
	}
	pr, err := e.r.Probe(e.ctx)
	if err != nil || pr.Problem != "" || len(pr.Missing) != 1 || !strings.HasPrefix(pr.Missing[0], d2+" (") {
		t.Fatalf("Probe = %+v, %v", pr, err)
	}
	raw, _ := json.Marshal(pr)
	if strings.Contains(string(raw), rtAccess) || strings.Contains(string(raw), rtSecret) {
		t.Fatalf("Probe lộ khoá: %s", raw)
	}
	e.mem.Fail("check", "", &s3.Error{Op: "check", Status: 403, Code: "AccessDenied"})
	if pr, err := e.r.Probe(e.ctx); err != nil || pr.Problem != s3.CodeAuthFailed {
		t.Fatalf("Probe khi kho từ chối khoá: %+v, %v", pr, err)
	}
}
