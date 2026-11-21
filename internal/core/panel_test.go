package core

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/agebox"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
)

// rtSnapshot chụp tpan.db thật (VACUUM INTO ra đĩa thật) rồi đặt bản chụp vào
// máy giả, nơi sha256sum và tar giả đọc.
func (e *rtEnv) rtSnapshot(t *testing.T) func(ctx context.Context, p string) error {
	return func(ctx context.Context, p string) error {
		tmp := filepath.Join(t.TempDir(), "snap.db")
		if err := e.st.SnapshotDB(ctx, tmp); err != nil {
			return err
		}
		data, err := os.ReadFile(tmp)
		if err != nil {
			return err
		}
		e.sys.SetFile(p, data, 0o600, system.Root)
		return nil
	}
}

// B3: gói panel không chứa hai khoá truy cập hay khoá sao lưu.
func TestGoiPanelKhongCoKhoa(t *testing.T) {
	e := newRtEnv(t)
	e.r.Snapshot = e.rtSnapshot(t)
	// Đặt đích qua engine (CLI, sinh khoá sao lưu), để tpan.db có settings và
	// job_events thật của đúng thao tác đã chạm tới khoá.
	ts, err := e.r.PrepareSetTarget(e.ctx, TargetInput{Endpoint: "https://s3.example.com", Region: "vn-thu-1", Bucket: "tpan-thu",
		Prefix: "tpan/may", PathStyle: true, Keep: 2, AccessKey: rtAccess, SecretKey: system.NewSecret(rtSecret), GenerateKey: true})
	if err != nil {
		t.Fatal(err)
	}
	if rep, err := ts.Execute(e.ctx, nil); err != nil || rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("đặt đích: %+v %v", rep.Job, err)
	}
	key := ts.NewKey
	if agebox.CheckKey(key) != nil {
		t.Fatal("đặt đích lần đầu bằng CLI phải sinh khoá sao lưu")
	}
	res := e.sync(t, SyncOptions{}) // panel_due = 1 sau khi đặt đích
	if !panelBundleName.MatchString(res.Panel) || res.PanelError != "" {
		t.Fatalf("Sync: %+v", res)
	}
	enc, ok := e.mem.Objects()["panel/"+res.Panel]
	if !ok {
		t.Fatalf("kho không có gói: %v", e.mem.Objects())
	}
	r, err := agebox.Decrypt(strings.NewReader(string(enc)), key)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	var files map[string]string
	if err := json.Unmarshal(plain, &files); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(files["tpan.db"], "SQLite format 3") || !strings.Contains(files["manifest.json"], `"migrations"`) {
		t.Fatalf("gói thiếu tpan.db hay manifest: %v", len(files))
	}
	for _, s := range []string{rtAccess, rtSecret, key.Reveal()} {
		if strings.Contains(string(plain), s) || strings.Contains(string(enc), s) {
			t.Fatal("gói panel chứa khoá")
		}
	}
	if pl := parsePanelLast(e.setting(t, SettingS3PanelLast)); pl == nil || pl.Name != res.Panel {
		t.Fatalf("panel_last = %q", e.setting(t, SettingS3PanelLast))
	}
	if e.setting(t, SettingS3PanelDue) != "0" || len(res.Reports) != 1 || res.Reports[0].Job.Owner != "admin" {
		t.Fatalf("panel_due = %q, reports %+v", e.setting(t, SettingS3PanelDue), res.Reports)
	}
	if _, err := e.sys.Stat(site.TPANDBBackupDir + "/" + res.Panel); err != nil {
		t.Fatalf("gói phải còn trên máy: %v", err)
	}
	// Gói vừa làm, không có gì đổi: lượt sau không làm gói, không gọi mạng.
	checks := e.mem.Calls("check")
	if res := e.sync(t, SyncOptions{}); res.Panel != "" || e.mem.Calls("check") != checks {
		t.Fatalf("lượt sau: %+v", res)
	}
}

// R3, R13: khoá sao lưu sai dạng thì không tạo job, ghi panel_error chỉ nêu
// đường dẫn, rồi lùi.
func TestGoiPanelKhoaSaiThiLui(t *testing.T) {
	e := newRtEnv(t)
	bad := "tpan-key-1-THIEU-MOT-DOAN"
	e.sys.SetFile(steps.BackupKeyFile, []byte(bad+"\n"), 0o600, system.Root)
	res := e.sync(t, SyncOptions{PanelDue: true})
	pe := e.setting(t, SettingS3PanelError)
	if len(res.Reports) != 0 || res.PanelError == "" || !strings.Contains(pe, steps.BackupKeyFile) || strings.Contains(pe, bad) {
		t.Fatalf("Sync: %+v, panel_error %q", res, pe)
	}
	if e.setting(t, SettingS3PanelFailures) != "1" || e.setting(t, SettingS3PanelRetryAt) == "" {
		t.Fatal("gói hỏng phải lùi")
	}
	checks := e.mem.Calls("check")
	if e.sync(t, SyncOptions{}); e.mem.Calls("check") != checks {
		t.Fatal("đang lùi gói panel mà vẫn gọi mạng")
	}
}

func TestPreparePanelKhongCoKhoa(t *testing.T) {
	e := newRtEnv(t)
	e.r.Snapshot = e.rtSnapshot(t)
	_, err := e.r.PreparePanel(e.ctx)
	if re, ok := err.(*RefusedError); !ok || re.Code != CodeBackupKeyMissing {
		t.Fatalf("chưa có khoá: %v", err)
	}
}
