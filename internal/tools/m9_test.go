package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/core"
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
