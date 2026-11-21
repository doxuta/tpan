package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/s3"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// list_backups không lọc có cả backup của site đã xoá hẳn: domain đó không
// còn trong bảng sites nên bộ che chưa biết nó lúc mở phiên. Tên thật (cả
// slug) không được lọt ra, và lọc theo domain bằng bí danh vẫn chạy.
func TestListBackupsRedactsPurgedSite(t *testing.T) {
	const gone = "gone-shop.example.net"
	e := newEnv(t, "")
	for _, b := range []core.Backup{
		{Domain: gone, Kind: core.BackupFull, Reason: core.ReasonBeforePurge, Path: "/var/lib/tpan/backups/" + gone + "/a.tar.gz", Size: 7, Created: e.now},
		{Domain: domA, Kind: core.BackupSQL, Reason: core.ReasonManual, Path: "/var/lib/tpan/backups/" + domA + "/b.sql", Size: 3, Created: e.now},
	} {
		if _, err := e.st.AddBackup(ctx, b); err != nil {
			t.Fatal(err)
		}
	}
	s := e.open(t, "")
	out := callOK(t, s, "list_backups", `{}`)
	slug := validate.SlugOf(mustDomain(t, gone)).String()
	assertNoLeak(t, "list_backups", string(out), append(secretsOf(t), gone, slug))
	var rows []backupRow
	if err := json.Unmarshal(out, &rows); err != nil || len(rows) != 2 {
		t.Fatalf("list_backups = %s (%v)", out, err)
	}
	if strings.Contains(string(out), "/var/lib/tpan") {
		t.Errorf("list_backups trả đường dẫn file: %s", out)
	}
	for _, r := range rows {
		if r.ID == "" || r.Kind == "" || r.Reason == "" || r.Created.IsZero() {
			t.Errorf("dòng thiếu trường: %+v", r)
		}
	}

	// Lọc theo bí danh của site đã xoá hẳn: chỉ dòng của nó.
	ga := alias(s, gone)
	out = callOK(t, s, "list_backups", `{"domain":"`+ga+`"}`)
	if err := json.Unmarshal(out, &rows); err != nil || len(rows) != 1 || rows[0].Reason != core.ReasonBeforePurge {
		t.Errorf("lọc theo %s = %s", ga, out)
	}

	for _, d := range []string{"a.com; rm -rf /", "$(id).example.com", "../../etc/passwd", "a.com\nb.com", strings.Repeat("a", 300) + ".com"} {
		dj, _ := json.Marshal(d)
		if err := callErr(t, s, "list_backups", `{"domain":`+string(dj)+`}`); err.Code != validate.CodeDomainInvalid {
			t.Errorf("list_backups %q: %v", d, err)
		}
		if err := callErr(t, s, "propose_backup_restore", `{"domain":`+string(dj)+`,"backup_id":"ab"}`); err.Code != validate.CodeDomainInvalid {
			t.Errorf("propose_backup_restore %q: %v", d, err)
		}
	}
	// backup_id lạ đi tới core và bị từ chối, không chạy gì.
	e.f.ResetRan()
	if err := callErr(t, s, "propose_backup_restore", `{"domain":"`+alias(s, domA)+`","backup_id":"../../etc/shadow"}`); err.Code != "backup_not_found" {
		t.Errorf("backup_id sai dạng: %v", err)
	}
	if ran := e.f.Ran(); len(ran) != 0 {
		t.Errorf("id sai dạng mà vẫn chạy lệnh: %v", ran)
	}
}

// Các dấu hiệu "bí mật" của test kho: không cái nào được ra tới agent.
const (
	s3Endpoint = "https://s3.mark-host.example"
	s3Bucket   = "mark-bucket-zz"
	s3Prefix   = "mark/prefix"
	s3Access   = "AKIAMARKACCESS0001"
	s3Secret   = "MarkSecretKey-qwerty-9876"
	s3BackKey  = "AGE-SECRET-KEY-MARKBACKUPKEY"
	s3ObjKey   = "mark-object-key/blog/x.tar.gz.age"
	s3ErrMark  = "mark-remote-error-zz"
)

// setS3 đặt đích trong settings và ghi ba file bí mật giả vào máy giả.
func (e *env) setS3(t *testing.T) string {
	t.Helper()
	for k, v := range map[string]string{
		core.SettingS3Endpoint: s3Endpoint, core.SettingS3Bucket: s3Bucket,
		core.SettingS3Prefix: s3Prefix, core.SettingS3Region: "us-east-1",
	} {
		if err := e.st.SetSetting(ctx, k, v); err != nil {
			t.Fatal(err)
		}
	}
	e.f.SetFile("/etc/tpan/s3.json", []byte(`{"access_key":"`+s3Access+`","secret_key":"`+s3Secret+`"}`+"\n"), 0o600, system.Root)
	e.f.SetFile("/etc/tpan/backup.key", []byte(s3BackKey+"\n"), 0o600, system.Root)
	return s3.TargetID(s3Endpoint, s3Bucket, s3Prefix)
}

// list_backups: năm giá trị của location, và không dòng nào lộ khoá object,
// lỗi đẩy, endpoint hay bucket.
func TestListBackupsLocation(t *testing.T) {
	e := newEnv(t, "")
	add := func(reason core.BackupReason) string {
		t.Helper()
		b, err := e.st.AddBackup(ctx, core.Backup{Domain: domA, Kind: core.BackupFull, Reason: reason,
			Path: "/var/lib/tpan/backups/" + domA + "/" + string(reason) + ".tar.gz", Size: 5, Created: e.now})
		if err != nil {
			t.Fatal(err)
		}
		return b.ID
	}
	machine := add("manual")
	both, remote, other, failed := add("schedule"), add("before-restore"), add("before-purge"), add("before-import")

	// Chưa đặt đích: dòng stored nào cũng thuộc "đích khác".
	const otherID = "ffffffffffffffff"
	for _, id := range []string{both, remote} {
		if _, err := e.st.MarkBackupStored(ctx, id, s3ObjKey, "aaaaaaaaaaaaaaaa", e.now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.st.ForgetBackupLocal(ctx, remote); err != nil {
		t.Fatal(err)
	}
	s := e.open(t, "")
	if got := locations(t, callOK(t, s, "list_backups", `{}`)); got[both] != locOtherTarget || got[remote] != locOtherTarget {
		t.Errorf("chưa đặt đích mà dòng stored không là other_target: %v", got)
	}

	// Đặt đích: dòng stored có mã khớp mới là both, remote.
	cur := e.setS3(t)
	for _, id := range []string{both, remote} {
		if _, err := e.st.MarkBackupStored(ctx, id, s3ObjKey, cur, e.now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.st.MarkBackupStored(ctx, other, s3ObjKey, otherID, e.now); err != nil {
		t.Fatal(err)
	}
	if err := e.st.MarkBackupPushFailed(ctx, failed, cur, s3ErrMark, true); err != nil {
		t.Fatal(err)
	}
	s = e.open(t, "")
	out := callOK(t, s, "list_backups", `{}`)
	want := map[string]string{machine: locMachine, both: locBoth, remote: locRemote, other: locOtherTarget, failed: locPushFailed}
	got := locations(t, out)
	for id, w := range want {
		if got[id] != w {
			t.Errorf("backup %s: location = %q, muốn %q", id, got[id], w)
		}
	}
	assertNoLeak(t, "list_backups", string(out), []string{"remote_key", "remote_error", "remote_target", s3ObjKey, s3ErrMark, s3Endpoint, s3Bucket, s3Prefix, "s3.mark-host"})
}

func locations(t *testing.T, out json.RawMessage) map[string]string {
	t.Helper()
	var rows []backupRow
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatalf("list_backups = %s (%v)", out, err)
	}
	m := map[string]string{}
	for _, r := range rows {
		m[r.ID] = r.Location
	}
	return m
}

// B3: đặt đích với khoá đánh dấu, gọi mọi công cụ đọc (cả gọi lỗi): không
// khoá truy cập, khoá sao lưu hay chi tiết kho nào ra tới agent.
func TestMCPKhongLoKhoaS3(t *testing.T) {
	e := newEnv(t, core.ApprovalClient)
	cur := e.setS3(t)
	b, err := e.st.AddBackup(ctx, core.Backup{Domain: domA, Kind: core.BackupFull, Reason: core.ReasonManual,
		Path: "/var/lib/tpan/backups/" + domA + "/x.tar.gz", Size: 5, Created: e.now})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.MarkBackupStored(ctx, b.ID, s3ObjKey, cur, e.now); err != nil {
		t.Fatal(err)
	}
	f, err := e.st.AddBackup(ctx, core.Backup{Domain: domA, Kind: core.BackupSQL, Reason: core.ReasonManual,
		Path: "/var/lib/tpan/backups/" + domA + "/y.sql", Size: 5, Created: e.now})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.MarkBackupPushFailed(ctx, f.ID, cur, s3ErrMark+" "+s3Secret, true); err != nil {
		t.Fatal(err)
	}

	s := e.open(t, "")
	a := alias(s, mustDomain(t, domA).String())
	var texts []string
	for _, c := range []struct{ name, args string }{
		{"list_sites", `{}`}, {"site_health", `{"domain":"` + a + `"}`}, {"stack_status", `{}`},
		{"machine", `{}`}, {"list_jobs", `{"limit":10}`}, {"get_job", `{"id":"` + seededJobID + `"}`},
		{"list_plans", `{}`}, {"list_backups", `{}`}, {"list_backups", `{"domain":"` + a + `"}`},
		{"current_view", `{}`},
		// Đường dẫn của ba file bí mật không nằm trong managed_files: bị từ chối.
		{"managed_file", `{"path":"/etc/tpan/s3.json"}`}, {"managed_file", `{"path":"/etc/tpan/backup.key"}`},
		{"propose_backup_restore", `{"domain":"` + a + `","backup_id":"` + b.ID + `"}`},
	} {
		out, err := s.Call(ctx, c.name, json.RawMessage(c.args))
		if err != nil {
			texts = append(texts, c.name+" lỗi: "+err.Error())
			continue
		}
		texts = append(texts, c.name+": "+string(out))
	}
	secrets := append(secretsOf(t), s3Access, s3Secret, s3BackKey, s3ObjKey, s3ErrMark, s3Endpoint, s3Bucket, s3Prefix)
	for _, x := range texts {
		assertNoLeak(t, "kết quả", x, secrets)
	}
	// Các lời gọi trả lời được phải thật sự trả lời (test không lọt vì lỗi sớm).
	if len(texts) != 13 || !strings.Contains(texts[7], `"location"`) {
		t.Errorf("list_backups không có location: %v", texts[7])
	}
}
