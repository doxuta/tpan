package main

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/agebox"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/s3"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

// Khoá đánh dấu: thấy chúng trong stdout hay stderr là lộ khoá (B3).
const (
	tAccess   = "AKIAMARKERACCESS77"
	tSecret   = "SecretMarker-0123456789xyz"
	tEndpoint = "https://s3.example.com"
	tBucket   = "tpan-test"
)

var backupKeyRe = regexp.MustCompile(`tpan-key-1-[a-z2-7]{52}`)

// s3Mem nối Remote của CLI với kho giả, khoá trong bộ nhớ và Snapshot vào máy
// giả (store.SnapshotDB ghi đĩa thật).
func (c *cli) s3Mem() *s3.Mem {
	c.t.Helper()
	mem := s3.NewMem()
	lock := system.NewMemLock()
	remoteHook = func(r *core.Remote) {
		r.NewStore = func(s3.Config) (steps.ObjectStore, error) { return mem, nil }
		r.Lock = lock
		r.Snapshot = func(_ context.Context, p string) error {
			c.fake.SetFile(p, []byte("SQLite giả"), 0o600, system.Root)
			return nil
		}
	}
	c.t.Cleanup(func() { remoteHook = nil })
	return mem
}

// setTarget đặt đích với hai khoá đánh dấu qua --keys-stdin.
func (c *cli) setTarget(extra ...string) (int, string, string) {
	c.t.Helper()
	c.stdin = tAccess + "\n" + tSecret + "\n"
	defer func() { c.stdin = "" }()
	return c.run(append([]string{"backup", "target", "set", "--endpoint", tEndpoint, "--bucket", tBucket, "--keys-stdin", "--yes"}, extra...)...)
}

func noKeysLeak(t *testing.T, outs ...string) {
	t.Helper()
	for _, o := range outs {
		if strings.Contains(o, tAccess) || strings.Contains(o, tSecret) {
			t.Errorf("lộ khoá truy cập:\n%s", o)
		}
	}
}

// bkCreate tạo một bản backup của dom, trả id.
func (c *cli) bkCreate(dom string) string {
	c.t.Helper()
	if code, out, errs := c.run("backup", "create", dom, "--yes"); code != 0 {
		c.t.Fatalf("create: mã %d\n%s\n%s", code, out, errs)
	}
	rows := c.bkRows(dom)
	if len(rows) == 0 {
		c.t.Fatal("create không ghi dòng")
	}
	return rows[0].ID
}

func (c *cli) jobKinds() map[string][]engine.JobStatus {
	c.t.Helper()
	st, err := store.Open(context.Background(), c.db)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	jobs, err := st.Jobs(context.Background(), 100)
	if err != nil {
		c.t.Fatal(err)
	}
	m := map[string][]engine.JobStatus{}
	for _, j := range jobs {
		m[j.Kind] = append(m[j.Kind], j.Status)
	}
	return m
}

func TestBackupTargetSetKeysStdin(t *testing.T) {
	c := newCLI(t)
	c.s3Mem()
	code, out, errs := c.setTarget("--json")
	if code != 0 {
		t.Fatalf("mã %d\n%s\n%s", code, out, errs)
	}
	noKeysLeak(t, out, errs)
	var got struct {
		Report         engine.Report         `json:"report"`
		Result         *core.TargetSetResult `json:"result"`
		BackupKey      string                `json:"backup_key"`
		KeyFingerprint string                `json:"key_fingerprint"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stdout không phải JSON: %v\n%s", err, out)
	}
	if got.Report.Job.Status != engine.JobSucceeded || got.Result == nil || !backupKeyRe.MatchString(got.BackupKey) ||
		got.KeyFingerprint != agebox.Fingerprint(system.NewSecret(got.BackupKey)) {
		t.Errorf("JSON = %+v", got)
	}
	if n := len(backupKeyRe.FindAllString(out+errs, -1)); n != 1 {
		t.Errorf("khoá sao lưu in %d lần", n)
	}
	keys, err := c.fake.ReadFile(steps.S3KeysFile)
	if err != nil || !strings.Contains(string(keys), tAccess) {
		t.Errorf("%s: %v", steps.S3KeysFile, err)
	}
	if c.setting(core.SettingS3Endpoint) != tEndpoint || c.setting(core.SettingS3Bucket) != tBucket {
		t.Errorf("setting endpoint %q bucket %q", c.setting(core.SettingS3Endpoint), c.setting(core.SettingS3Bucket))
	}

	// Bản chữ trên máy mới: khoá in đúng một lần, kèm dấu vân tay và lời dặn.
	c2 := newCLI(t)
	c2.s3Mem()
	code, out, errs = c2.setTarget()
	if code != 0 {
		t.Fatalf("chữ: mã %d\n%s\n%s", code, out, errs)
	}
	noKeysLeak(t, out, errs)
	if n := len(backupKeyRe.FindAllString(out+errs, -1)); n != 1 || !strings.Contains(out, "Dấu vân tay") || !strings.Contains(out, "NGOÀI máy") {
		t.Errorf("khoá in %d lần:\n%s", n, out)
	}

	// target show: không khoá nào, có access key đã che.
	code, out, errs = c.run("backup", "target", "show")
	if code != 0 || !strings.Contains(out, tBucket) || !strings.Contains(out, "key show --reveal") {
		t.Errorf("show: mã %d\n%s\n%s", code, out, errs)
	}
	noKeysLeak(t, out, errs)
	if backupKeyRe.MatchString(out) {
		t.Errorf("show in khoá sao lưu:\n%s", out)
	}
	_, out, _ = c.run("backup", "target", "show", "--json")
	var v core.TargetView
	if err := json.Unmarshal([]byte(out), &v); err != nil || !v.Configured || !v.HasBackupKey || v.AccessKeyTail != tAccess[len(tAccess)-4:] {
		t.Errorf("show --json = %s, %v", out, err)
	}
}

// Chạy lại không --keys-stdin, không terminal: giữ khoá khi đích không đổi
// chỗ; đổi endpoint thì phải gửi lại khoá (R1).
func TestBackupTargetSetKeepKeys(t *testing.T) {
	c := newCLI(t)
	c.s3Mem()
	if code, out, errs := c.setTarget(); code != 0 {
		t.Fatalf("mã %d\n%s\n%s", code, out, errs)
	}
	before, _ := c.fake.ReadFile(steps.S3KeysFile)
	code, out, errs := c.run("backup", "target", "set", "--endpoint", tEndpoint, "--bucket", tBucket, "--keep", "3", "--yes", "--json")
	if code != 0 || strings.Contains(out, "backup_key") {
		t.Fatalf("giữ khoá: mã %d\n%s\n%s", code, out, errs)
	}
	if after, _ := c.fake.ReadFile(steps.S3KeysFile); string(after) != string(before) || c.setting(core.SettingS3Keep) != "3" {
		t.Errorf("khoá đổi hoặc keep %q", c.setting(core.SettingS3Keep))
	}
	code, out, _ = c.run("backup", "target", "set", "--endpoint", "https://other.example.com", "--bucket", tBucket, "--yes", "--json")
	if code != 1 || refusalCode(t, out) != s3.CodeKeysInvalid {
		t.Errorf("đổi endpoint không gửi khoá: mã %d\n%s", code, out)
	}
}

func TestBackupTargetSetArgs(t *testing.T) {
	c := newCLI(t)
	c.s3Mem()
	code, out, _ := c.run("backup", "target", "set", "--endpoint", tEndpoint, "--bucket", tBucket, "--ca-file", "x.pem", "--no-ca", "--json")
	if code != 1 || refusalCode(t, out) != "args_invalid" {
		t.Errorf("--ca-file với --no-ca: mã %d %s", code, out)
	}
	c.stdin = "chỉ-một-dòng\n"
	code, out, errs := c.run("backup", "target", "set", "--endpoint", tEndpoint, "--bucket", tBucket, "--keys-stdin", "--yes", "--json")
	if code != 1 || refusalCode(t, out) != s3.CodeKeysInvalid || strings.Contains(out+errs, "chỉ-một-dòng") {
		t.Errorf("một dòng: mã %d %s %s", code, out, errs)
	}
	c.root = false
	if code, _, errs := c.setTarget(); code != 1 || !strings.Contains(errs, "sudo") {
		t.Errorf("không root: mã %d %s", code, errs)
	}
}

func TestBackupPush(t *testing.T) {
	const dom = "shop.example.com"
	t.Run("đạt", func(t *testing.T) {
		c := newCLI(t)
		mem := c.s3Mem()
		c.bkSites(dom)
		id := c.bkCreate(dom)
		if code, out, errs := c.run("backup", "push", "--json"); code != 1 || refusalCode(t, out) != s3.CodeNotConfigured {
			t.Errorf("chưa đặt đích: mã %d\n%s\n%s", code, out, errs)
		}
		if code, out, errs := c.setTarget(); code != 0 {
			t.Fatalf("đặt đích: mã %d\n%s\n%s", code, out, errs)
		}
		code, out, errs := c.run("backup", "push", "--json")
		if code != 0 {
			t.Fatalf("push: mã %d\n%s\n%s", code, out, errs)
		}
		var res core.SyncResult
		if err := json.Unmarshal([]byte(out), &res); err != nil || !res.Ran || len(res.Pushed) != 1 || res.Pushed[0] != id {
			t.Errorf("push = %s, %v", out, err)
		}
		// Đã có khoá sao lưu (target set vừa sinh) và panel_due: gói panel lên kho.
		if res.Panel == "" || res.PanelError != "" {
			t.Errorf("gói panel: %q, lỗi %q", res.Panel, res.PanelError)
		}
		if rows := c.bkRows(dom); rows[0].Remote != site.RemoteStored {
			t.Errorf("dòng = %+v", rows[0])
		}
		if mem.Calls("put") == 0 {
			t.Error("không gọi put")
		}
		// Không còn gì để đẩy: ran=true, pushed=[], thoát 0.
		code, out, _ = c.run("backup", "push", "--json")
		res = core.SyncResult{}
		if err := json.Unmarshal([]byte(out), &res); code != 0 || err != nil || !res.Ran || len(res.Pushed) != 0 || res.Pushed == nil {
			t.Errorf("không có việc: mã %d %s", code, out)
		}
		if code, out, _ := c.run("backup", "push", "$(id)", "--json"); code != 1 || refusalCode(t, out) != "backup_not_found" {
			t.Errorf("id lạ: mã %d %s", code, out)
		}
	})
	t.Run("kho trả 503", func(t *testing.T) {
		c := newCLI(t)
		mem := c.s3Mem()
		c.bkSites(dom)
		id := c.bkCreate(dom)
		if code, out, errs := c.setTarget(); code != 0 {
			t.Fatalf("đặt đích: mã %d\n%s\n%s", code, out, errs)
		}
		mem.Fail("put", "", &s3.Error{Op: "put", Status: 503, Code: "SlowDown"})
		code, out, errs := c.run("backup", "push", id)
		if code != 1 || !strings.Contains(out, "LỖI "+id) {
			t.Errorf("mã %d\n%s\n%s", code, out, errs)
		}
		noKeysLeak(t, out, errs)
	})
}

// B11: chưa đặt đích thì create in y hệt máy không có kho (bỏ id job, giờ).
func TestBackupCreateKhongDichKhongDoi(t *testing.T) {
	const dom = "shop.example.com"
	norm := regexp.MustCompile(`[0-9a-f]{16}|\d{8}-\d{6}|\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}(:\d{2})?`)
	runOnce := func(withHook bool) (string, string, *s3.Mem) {
		c := newCLI(t)
		var mem *s3.Mem
		if withHook {
			mem = c.s3Mem()
		}
		c.bkSites(dom)
		code, out, errs := c.run("backup", "create", dom, "--yes")
		if code != 0 {
			t.Fatalf("mã %d\n%s\n%s", code, out, errs)
		}
		return norm.ReplaceAllString(out, "X"), norm.ReplaceAllString(errs, "X"), mem
	}
	out1, err1, _ := runOnce(false)
	out2, err2, mem := runOnce(true)
	if out1 != out2 || err1 != err2 || err2 != "" {
		t.Errorf("output khác:\n--- không kho\n%s\n%s\n--- có kho giả, chưa đặt đích\n%s\n%s", out1, err1, out2, err2)
	}
	for _, op := range []string{"check", "put", "get", "head", "list", "delete", "abort"} {
		if n := mem.Calls(op); n != 0 {
			t.Errorf("gọi kho %s %d lần", op, n)
		}
	}
}

func TestBackupCreateDayLenKho(t *testing.T) {
	// Hai domain: tên file backup tính theo giây, hai lần create cùng site
	// trong một giây đụng nhau.
	const dom, dom2 = "shop.example.com", "blog.example.com"
	c := newCLI(t)
	mem := c.s3Mem()
	c.bkSites(dom, dom2)
	if code, out, errs := c.setTarget(); code != 0 {
		t.Fatalf("đặt đích: mã %d\n%s\n%s", code, out, errs)
	}
	code, out, errs := c.run("backup", "create", dom2, "--yes")
	id := c.bkRows(dom2)[0].ID
	if code != 0 || !strings.Contains(errs, "đã lên kho: "+id) {
		t.Errorf("đạt: mã %d\n%s\n%s", code, out, errs)
	}

	// Kho lỗi: bản trên máy vẫn xong, thoát 0, nói rõ chưa lên kho.
	mem.Fail("put", "", &s3.Error{Op: "put", Status: 503, Code: "SlowDown"})
	code, out, errs = c.run("backup", "create", dom, "--yes", "--json")
	if code != 0 || !strings.Contains(errs, "CHƯA lên kho") || !strings.Contains(errs, "sudo tpan backup push") {
		t.Errorf("đẩy lỗi: mã %d\n%s\n%s", code, out, errs)
	}
	// create <domain> --json giữ dạng M9: đúng một engine.Report trên stdout.
	var rep engine.Report
	if err := json.Unmarshal([]byte(out), &rep); err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Errorf("stdout không còn là JSON của M9: %s", out)
	}
	if rows := c.bkRows(dom); len(rows) != 1 || rows[0].Remote == site.RemoteStored {
		t.Errorf("dòng mới = %+v", rows)
	}
	noKeysLeak(t, out, errs)
}

// create --all --json: thêm trường sync khi đã đặt đích, không có khi chưa.
func TestBackupCreateAllSync(t *testing.T) {
	for _, withTarget := range []bool{false, true} {
		c := newCLI(t)
		c.s3Mem()
		c.bkSites("a.example.com")
		// Chặn thư mục bản sao tpan.db như TestBackupCreateAllRun: SnapshotDB
		// của CreateAll ghi đĩa thật.
		c.fake.Fail("mkdir", site.TPANDBBackupDir, errors.New("giả lập: đĩa chỉ đọc"))
		if withTarget {
			if code, out, errs := c.setTarget(); code != 0 {
				t.Fatalf("đặt đích: mã %d\n%s\n%s", code, out, errs)
			}
		}
		_, out, errs := c.run("backup", "create", "--all", "--yes", "--json")
		var got backupAllResult
		if err := json.Unmarshal([]byte(out), &got); err != nil || len(got.Sites) != 1 {
			t.Fatalf("JSON: %v\n%s\n%s", err, out, errs)
		}
		if withTarget != (got.Sync != nil) || (withTarget && (!got.Sync.Ran || len(got.Sync.Pushed) != 1)) {
			t.Errorf("đích %v: sync = %+v", withTarget, got.Sync)
		}
		if !withTarget && strings.Contains(out, `"sync"`) {
			t.Errorf("chưa đặt đích mà có trường sync:\n%s", out)
		}
	}
}

func TestBackupListKhongDichKhongCoCot(t *testing.T) {
	const dom = "shop.example.com"
	c := newCLI(t)
	c.s3Mem()
	c.bkSites(dom)
	c.bkCreate(dom)
	_, out, _ := c.run("backup", "list")
	if strings.Contains(out, "Ở ĐÂU") || strings.Contains(out, "máy") {
		t.Errorf("chưa đặt đích mà có cột Ở ĐÂU:\n%s", out)
	}
	if code, out, errs := c.setTarget(); code != 0 {
		t.Fatalf("đặt đích: mã %d\n%s\n%s", code, out, errs)
	}
	if _, out, _ = c.run("backup", "list"); !strings.Contains(out, "Ở ĐÂU") || !strings.Contains(out, " máy ") {
		t.Errorf("đã đặt đích mà thiếu cột:\n%s", out)
	}
	if code, _, errs := c.run("backup", "push"); code != 0 {
		t.Fatalf("push: mã %d %s", code, errs)
	}
	if _, out, _ = c.run("backup", "list", dom); !strings.Contains(out, "máy+kho") {
		t.Errorf("sau push:\n%s", out)
	}
	code, out, errs := c.run("backup", "list", "--remote", "--json")
	var l core.RemoteListing
	if err := json.Unmarshal([]byte(out), &l); code != 0 || err != nil || len(l.Objects) == 0 {
		t.Errorf("--remote: mã %d %s %s %v", code, out, errs, err)
	}
	if code, out, _ := c.run("backup", "list", dom, "--remote", "--json"); code != 1 || refusalCode(t, out) != "args_invalid" {
		t.Errorf("--remote với domain: mã %d %s", code, out)
	}
}

// remoteOnly đưa bản mới nhất của dom lên kho rồi bỏ file trên máy (local=0).
func (c *cli) remoteOnly(dom string) core.Backup {
	c.t.Helper()
	id := c.bkCreate(dom)
	if code, out, errs := c.setTarget(); code != 0 {
		c.t.Fatalf("đặt đích: mã %d\n%s\n%s", code, out, errs)
	}
	if code, out, errs := c.run("backup", "push", id); code != 0 {
		c.t.Fatalf("push: mã %d\n%s\n%s", code, out, errs)
	}
	st, err := store.Open(context.Background(), c.db)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	if _, err := st.ForgetBackupLocal(context.Background(), id); err != nil {
		c.t.Fatal(err)
	}
	row := c.bkRows(dom)[0]
	if err := c.fake.Remove(row.Path); err != nil {
		c.t.Fatal(err)
	}
	if row.Local || row.Remote != site.RemoteStored {
		c.t.Fatalf("dòng = %+v", row)
	}
	return row
}

func TestBackupRestoreTuKho(t *testing.T) {
	const dom = "shop.example.com"
	c := newCLI(t)
	mem := c.s3Mem()
	c.bkSites(dom)
	row := c.remoteOnly(dom)

	// --dry-run: câu R15, không tải.
	gets := mem.Calls("get")
	code, out, errs := c.run("backup", "restore", dom, "--id", row.ID, "--dry-run")
	if code != 0 || !strings.Contains(out, "cần tải về trước khi xem được bản xem trước khôi phục") {
		t.Errorf("--dry-run: mã %d\n%s\n%s", code, out, errs)
	}
	if mem.Calls("get") != gets || c.bkExists(row.Path) || c.bkRows(dom)[0].Local {
		t.Error("--dry-run đã tải về")
	}
	// Không --yes, không terminal: từ chối trước khi tải.
	if code, out, _ := c.run("backup", "restore", dom, "--id", row.ID, "--json"); code != 1 || refusalCode(t, out) != "confirm_required" || mem.Calls("get") != gets {
		t.Errorf("không --yes: mã %d %s", code, out)
	}

	// Job khôi phục tự backup hiện trạng trước, tên file theo giây: cùng giây
	// với bản vừa tạo thì đụng tên. Chờ sang giây kế.
	time.Sleep(time.Until(time.Now().Truncate(time.Second).Add(time.Second)))
	code, out, errs = c.run("backup", "restore", dom, "--id", row.ID, "--yes")
	if !c.bkExists(row.Path) || !c.bkRows(dom)[len(c.bkRows(dom))-1].Local {
		t.Fatalf("chưa tải về: mã %d\n%s\n%s", code, out, errs)
	}
	// Hai job: tải về đạt, rồi job khôi phục dựng lại trên đúng file vừa tải.
	// ponytail: không đòi job khôi phục đạt. Máy giả ở đây không có tar
	// --list, mariadb nhập, kiểm chứng HTTP; core.TestBackupRestore đã phủ
	// phần đó, test này chỉ phủ đường nối của CLI.
	jobs := c.jobKinds()
	if len(jobs["backup.pull"]) != 1 || jobs["backup.pull"][0] != engine.JobSucceeded || len(jobs["backup.restore"]) != 1 {
		t.Errorf("jobs = %v\n%s\n%s", jobs, out, errs)
	}
	if pull, restore := strings.Index(out, "s3.pull:"+row.ID), strings.Index(out, "Giải nén "+row.Path); pull < 0 || restore < pull {
		t.Errorf("không thấy tải về rồi khôi phục từ %s (mã %d):\n%s\n%s", row.Path, code, out, errs)
	}
}

// Bản trên kho hỏng: tải về lỗi sha256, thoát khác 0, lý do ở stderr, không
// để lại file.
func TestBackupRestoreTuKhoHong(t *testing.T) {
	const dom = "shop.example.com"
	c := newCLI(t)
	mem := c.s3Mem()
	c.bkSites(dom)
	row := c.remoteOnly(dom)
	mem.Corrupt(row.RemoteKey)
	code, out, errs := c.run("backup", "restore", dom, "--id", row.ID, "--yes")
	if code == 0 || !strings.Contains(errs, "sha256") {
		t.Errorf("mã %d\n%s\n%s", code, out, errs)
	}
	if c.bkExists(row.Path) || c.bkExists(row.Path+".part") {
		t.Error("còn file sau khi tải hỏng")
	}
	if len(c.jobKinds()["backup.restore"]) != 0 {
		t.Error("tải hỏng mà vẫn khôi phục")
	}
}

func TestBackupKeyShowNew(t *testing.T) {
	c := newCLI(t)
	c.s3Mem()
	if _, out, _ := c.run("backup", "key", "show", "--json"); !strings.Contains(out, `"has_key": false`) {
		t.Errorf("chưa có khoá: %s", out)
	}
	if code, out, errs := c.setTarget(); code != 0 {
		t.Fatalf("đặt đích: mã %d\n%s\n%s", code, out, errs)
	}
	code, out, errs := c.run("backup", "key", "show")
	if code != 0 || backupKeyRe.MatchString(out+errs) || !strings.Contains(out, "Dấu vân tay") {
		t.Errorf("không --reveal: mã %d\n%s", code, out)
	}
	_, out, _ = c.run("backup", "key", "show", "--json")
	if backupKeyRe.MatchString(out) || strings.Contains(out, `"key":`) {
		t.Errorf("--json không --reveal in khoá: %s", out)
	}
	_, out, _ = c.run("backup", "key", "show", "--reveal")
	if n := len(backupKeyRe.FindAllString(out, -1)); n != 1 || !strings.Contains(out, "Cảnh báo") {
		t.Errorf("--reveal: %d lần\n%s", n, out)
	}
	old := backupKeyRe.FindString(out)

	code, out, errs = c.run("backup", "key", "new", "--yes", "--json")
	var kn struct {
		Report         engine.Report `json:"report"`
		Key            string        `json:"key"`
		KeyFingerprint string        `json:"key_fingerprint"`
	}
	if err := json.Unmarshal([]byte(out), &kn); code != 0 || err != nil || !backupKeyRe.MatchString(kn.Key) || kn.Key == old ||
		kn.KeyFingerprint != agebox.Fingerprint(system.NewSecret(kn.Key)) {
		t.Errorf("key new: mã %d\n%s\n%s", code, out, errs)
	}
	if c.setting(core.SettingS3PanelDue) != "1" {
		t.Error("key new không đặt panel_due")
	}
	_, out, _ = c.run("backup", "key", "new", "--yes")
	if n := len(backupKeyRe.FindAllString(out, -1)); n != 1 || !strings.Contains(out, "khoá CŨ") {
		t.Errorf("key new chữ: %d lần\n%s", n, out)
	}
	c.root = false
	if code, _, errs := c.run("backup", "key", "show"); code != 1 || !strings.Contains(errs, "sudo") {
		t.Errorf("không root: mã %d %s", code, errs)
	}
}

func TestBackupTargetRmTest(t *testing.T) {
	c := newCLI(t)
	c.s3Mem()
	if code, out, _ := c.run("backup", "target", "rm", "--yes", "--json"); code != 1 || refusalCode(t, out) != s3.CodeNotConfigured {
		t.Errorf("rm chưa đặt: mã %d %s", code, out)
	}
	if code, out, errs := c.setTarget(); code != 0 {
		t.Fatalf("đặt đích: mã %d\n%s\n%s", code, out, errs)
	}
	code, out, errs := c.run("backup", "target", "test", "--yes", "--json")
	var rep engine.Report
	if err := json.Unmarshal([]byte(out), &rep); code != 0 || err != nil || rep.Job.Kind != "backup.target.test" {
		t.Errorf("test: mã %d\n%s\n%s", code, out, errs)
	}
	if code, out, _ := c.run("backup", "target", "rm", "--json"); code != 1 || refusalCode(t, out) != "confirm_required" {
		t.Errorf("rm không --yes: mã %d %s", code, out)
	}
	if code, out, _ := c.run("backup", "target", "rm", "--dry-run"); code != 0 || !strings.Contains(out, "bản chỉ còn trên kho") {
		t.Errorf("rm --dry-run: mã %d\n%s", code, out)
	}
	if code, out, errs := c.run("backup", "target", "rm", "--yes"); code != 0 || c.setting(core.SettingS3Endpoint) != "" || c.bkExists(steps.S3KeysFile) {
		t.Errorf("rm: mã %d\n%s\n%s", code, out, errs)
	}
}
