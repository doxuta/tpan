package steps

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/account"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

const (
	sdbBak     = site.BackupRoot + "/_db/" + sdbName + "/" + sdbName + "-db-20261126-050000.sql"
	sdbBakUndo = site.BackupRoot + "/_db/" + sdbName + "/" + sdbName + "-db-20261126-060000-before-import.sql"
)

func sdbDumpOf(data string) []byte {
	return []byte("-- data:" + data + "\n-- Dump completed on 2026-11-26 05:00:00\n")
}

// Nhập bản sao lưu vào DB rời đang có: dữ liệu về đúng bản sao lưu, charset và
// user DB giữ nguyên; hoàn tác đưa về dữ liệu ngay trước lúc nhập.
func TestSDBImportApplyUndo(t *testing.T) {
	f := newSDBFake()
	f.set(func(f *sdbFake) {
		sdbWithDB(f)
		d := f.dbs[sdbName]
		d.data, d.coll = "moi", "utf8mb4_bin"
		f.dbs[sdbName] = d
		f.SetFile(sdbBak, sdbDumpOf("cu"), 0o600, system.Root)
		f.SetFile(sdbBakUndo, sdbDumpOf("moi"), 0o600, system.Root)
	})
	s := &SDBImport{Name: sdbName, Charset: "utf8mb4", Collation: "utf8mb4_bin", File: sdbBak, UndoFile: sdbBakUndo}
	if ok, err := s.Check(context.Background(), f); ok || err != nil {
		t.Fatalf("Check trước Apply = %v, %v", ok, err)
	}
	if err := s.Apply(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	if d := f.dbs[sdbName]; d.data != "cu" || d.cs != "utf8mb4" || d.coll != "utf8mb4_bin" {
		t.Errorf("sau nhập: %+v", d)
	}
	if u, ok := f.users[sdbName]; !ok || u.pw != "Old0000000000000000000000000000x" || u.grant == "" {
		t.Errorf("user DB đổi sau khi nhập: %+v (có=%v)", u, ok)
	}
	if ok, _ := s.Check(context.Background(), f); !ok {
		t.Error("Check sau Apply phải true")
	}
	if err := s.Undo(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	if d := f.dbs[sdbName]; d.data != "moi" {
		t.Errorf("sau hoàn tác: %+v", d)
	}
	if !s.Info().Danger {
		t.Error("nhập đè DB phải là bước nguy hiểm")
	}
}

// Thiếu đường lùi, hay file nhập không trọn vẹn, thì không xoá gì.
func TestSDBImportNeedsBothFiles(t *testing.T) {
	for name, prep := range map[string]func(f *sdbFake){
		"thiếu file hoàn tác": func(f *sdbFake) { f.SetFile(sdbBak, sdbDumpOf("cu"), 0o600, system.Root) },
		"file nhập dở": func(f *sdbFake) {
			f.SetFile(sdbBak, []byte("-- data:cu\n-- half"), 0o600, system.Root)
			f.SetFile(sdbBakUndo, sdbDumpOf("moi"), 0o600, system.Root)
		},
	} {
		f := newSDBFake()
		f.set(func(f *sdbFake) { sdbWithDB(f); prep(f) })
		f.ResetRan()
		s := &SDBImport{Name: sdbName, Charset: "utf8mb4", Collation: "utf8mb4_general_ci", File: sdbBak, UndoFile: sdbBakUndo}
		if err := s.Apply(context.Background(), f); err == nil {
			t.Errorf("%s: vẫn nhập", name)
		}
		if got := sdbMutating(f.Ran()); len(got) != 0 {
			t.Errorf("%s: đã chạy lệnh đổi máy: %v", name, got)
		}
		if f.dbs[sdbName].data != "rows" {
			t.Errorf("%s: dữ liệu đã đổi", name)
		}
	}
}

// Nhập lỗi giữa chừng thì DB được trả về bản ngay trước lúc nhập.
func TestSDBImportFailureRestores(t *testing.T) {
	f := newSDBFake()
	n := 0
	f.set(func(f *sdbFake) {
		sdbWithDB(f)
		f.SetFile(sdbBak, sdbDumpOf("cu"), 0o600, system.Root)
		f.SetFile(sdbBakUndo, sdbDumpOf("rows"), 0o600, system.Root)
	})
	// Lần source đầu (file nhập) lỗi, lần sau (file hoàn tác) chạy thật.
	f.Handle([]string{"mariadb", "--database=" + sdbName}, func(c system.Cmd) system.Result {
		n++
		if n == 1 {
			return system.Result{ExitCode: 1, Stderr: "ERROR 1064 (42000) at line 12 in file"}
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		raw, _ := f.ReadFile(strings.TrimPrefix(c.Args[3], "source "))
		d := f.dbs[sdbName]
		d.data = strings.TrimPrefix(strings.SplitN(string(raw), "\n", 2)[0], "-- data:")
		f.dbs[sdbName] = d
		return system.Result{}
	})
	s := &SDBImport{Name: sdbName, Charset: "utf8mb4", Collation: "utf8mb4_general_ci", File: sdbBak, UndoFile: sdbBakUndo}
	err := s.Apply(context.Background(), f)
	if err == nil || !strings.Contains(err.Error(), "nhập "+sdbBak) {
		t.Fatalf("lỗi = %v", err)
	}
	if d, ok := f.dbs[sdbName]; !ok || d.data != "rows" {
		t.Errorf("DB sau lần nhập lỗi = %+v (có=%v): muốn về bản trước lúc nhập", d, ok)
	}
	if ok, _ := s.Check(context.Background(), f); ok {
		t.Error("Apply lỗi mà Check true")
	}
}

// Tạo lại DB từ bản sao lưu: nhập vào DB trống; DB đã có bảng thì từ chối.
func TestSDBLoad(t *testing.T) {
	f := newSDBFake()
	f.set(func(f *sdbFake) {
		f.dbs[sdbName] = sdbDB{cs: "utf8mb4", coll: "utf8mb4_general_ci"}
		f.SetFile(sdbBak, sdbDumpOf("cu"), 0o600, system.Root)
	})
	s := &SDBLoad{Name: sdbName, File: sdbBak}
	if rep := sdbRun(t, f, s); rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("job: %s", rep.Job.Error)
	}
	if f.dbs[sdbName].data != "cu" {
		t.Errorf("dữ liệu = %q", f.dbs[sdbName].data)
	}
	// DB đang có bảng: không nhập đè.
	err := (&SDBLoad{Name: sdbName, File: sdbBak}).Apply(context.Background(), f)
	if err == nil || !strings.Contains(err.Error(), "không nhập đè") {
		t.Errorf("DB có bảng: lỗi = %v", err)
	}
	// DB chưa có: lỗi rõ, không tự tạo.
	f.set(func(f *sdbFake) { delete(f.dbs, sdbName) })
	if err := (&SDBLoad{Name: sdbName, File: sdbBak}).Apply(context.Background(), f); err == nil || !strings.Contains(err.Error(), "DB chưa có") {
		t.Errorf("DB chưa có: lỗi = %v", err)
	}
}

// Xoá hẳn: file xuất ở thùng rác được chuyển sang thư mục sao lưu, không xoá.
func TestSDBKeepDump(t *testing.T) {
	f := newSDBFake()
	from := account.DBTrashDump(sdbName)
	f.set(func(f *sdbFake) { f.SetFile(from, sdbDumpOf("cuoi"), 0o644, system.Root) })
	s := &SDBKeepDump{Name: sdbName, To: sdbBak}
	if ok, err := s.Check(context.Background(), f); ok || err != nil {
		t.Fatalf("Check trước Apply = %v, %v", ok, err)
	}
	if rep := sdbRun(t, f, s); rep.Job.Status != engine.JobSucceeded {
		t.Fatalf("job: %s", rep.Job.Error)
	}
	if _, err := f.Stat(from); err == nil {
		t.Error("file ở thùng rác còn")
	}
	got, err := f.ReadFile(sdbBak)
	if err != nil || string(got) != string(sdbDumpOf("cuoi")) {
		t.Fatalf("file sao lưu = %q, %v", got, err)
	}
	if fi, _ := f.Stat(sdbBak); fi.Mode.Perm() != 0o600 {
		t.Errorf("quyền file sao lưu = %v, muốn 0600", fi.Mode.Perm())
	}
	if fi, err := f.Stat(site.BackupRoot + "/_db/" + sdbName); err != nil || fi.Mode.Perm() != 0o700 {
		t.Errorf("thư mục sao lưu = %+v, %v: muốn 0700", fi, err)
	}
	// Chạy lại (job sau hỏng rồi chạy lại xoá hẳn): bước coi là đã xong.
	if ok, err := (&SDBKeepDump{Name: sdbName, To: sdbBak}).Check(context.Background(), f); !ok || err != nil {
		t.Errorf("Check sau khi đã chuyển = %v, %v", ok, err)
	}
	if err := s.Undo(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Stat(from); err != nil {
		t.Error("hoàn tác không đưa file về thùng rác")
	}
	if _, err := f.Stat(sdbBak); err == nil {
		t.Error("hoàn tác mà file sao lưu còn")
	}
}

// File ở thùng rác không trọn vẹn, hoặc đích đã có: không chuyển gì.
func TestSDBKeepDumpRefuses(t *testing.T) {
	from := account.DBTrashDump(sdbName)
	f := newSDBFake()
	f.set(func(f *sdbFake) { f.SetFile(from, []byte("-- data:x\n-- half"), 0o600, system.Root) })
	if err := (&SDBKeepDump{Name: sdbName, To: sdbBak}).Apply(context.Background(), f); err == nil {
		t.Error("chuyển file xuất dở")
	}
	f = newSDBFake()
	f.set(func(f *sdbFake) {
		f.SetFile(from, sdbDumpOf("cuoi"), 0o600, system.Root)
		f.SetFile(sdbBak, sdbDumpOf("khac"), 0o600, system.Root)
	})
	if err := (&SDBKeepDump{Name: sdbName, To: sdbBak}).Apply(context.Background(), f); err == nil || !strings.Contains(err.Error(), "không ghi đè") {
		t.Errorf("đích đã có: lỗi = %v", err)
	}
	if got, _ := f.ReadFile(sdbBak); string(got) != string(sdbDumpOf("khac")) {
		t.Error("file đích bị ghi đè")
	}
}

// Thư mục sao lưu nằm trên phân vùng khác: đổi tên lỗi thì dùng mv.
func TestSDBKeepDumpCrossDevice(t *testing.T) {
	f := newSDBFake()
	from := account.DBTrashDump(sdbName)
	f.set(func(f *sdbFake) { f.SetFile(from, sdbDumpOf("cuoi"), 0o600, system.Root) })
	f.Fail("rename", sdbBak, errors.New("invalid cross-device link"))
	moved := false
	f.Handle([]string{"mv", "--"}, func(c system.Cmd) system.Result {
		data, _ := f.ReadFile(c.Args[1])
		f.SetFile(c.Args[2], data, 0o644, system.Root)
		_ = f.Remove(c.Args[1])
		moved = true
		return system.Result{}
	})
	if err := (&SDBKeepDump{Name: sdbName, To: sdbBak}).Apply(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	if !moved {
		t.Error("không gọi mv khi đổi tên lỗi")
	}
	if _, err := f.Stat(from); err == nil {
		t.Error("file ở thùng rác còn")
	}
}

// Tên, charset, đường dẫn lạ bị từ chối trước khi có lệnh nào chạy.
func TestSDBM33StepsRejectBadInput(t *testing.T) {
	badNames := []string{"a; rm -rf /", "$(id)", "../../etc/passwd", "a\nb", "", "a'b", "a`b"}
	badPaths := []string{"", "relative.sql", "/etc/passwd", site.BackupRoot + "/../x.sql", site.BackupRoot + "/a b.sql", site.BackupRoot + "/a;b.sql", site.RestoreRoot + "/x.sql"}
	var ss []engine.Step
	for _, n := range badNames {
		ss = append(ss,
			&SDBImport{Name: n, Charset: "utf8mb4", Collation: "utf8mb4_bin", File: sdbBak, UndoFile: sdbBakUndo},
			&SDBImport{Name: sdbName, Charset: n, Collation: "utf8mb4_bin", File: sdbBak, UndoFile: sdbBakUndo},
			&SDBLoad{Name: n, File: sdbBak}, &SDBKeepDump{Name: n, To: sdbBak})
	}
	for _, p := range badPaths {
		ss = append(ss,
			&SDBImport{Name: sdbName, Charset: "utf8mb4", Collation: "utf8mb4_bin", File: p, UndoFile: sdbBakUndo},
			&SDBImport{Name: sdbName, Charset: "utf8mb4", Collation: "utf8mb4_bin", File: sdbBak, UndoFile: p},
			&SDBLoad{Name: sdbName, File: p}, &SDBKeepDump{Name: sdbName, To: p})
	}
	ss = append(ss, &SDBImport{Name: sdbName, Charset: "utf8mb4", Collation: "utf8mb4_bin", File: sdbBak, UndoFile: sdbBak})
	for i, s := range ss {
		f := newSDBFake()
		f.set(sdbWithDB)
		f.ResetRan()
		if _, err := s.Check(context.Background(), f); err == nil {
			t.Errorf("bước %d (%T): Check nhận đầu vào xấu", i, s)
		}
		if err := s.Apply(context.Background(), f); err == nil {
			t.Errorf("bước %d (%T): Apply nhận đầu vào xấu", i, s)
		}
		if len(f.Ran()) != 0 {
			t.Errorf("bước %d (%T): đã chạy lệnh %v", i, s, f.Ran())
		}
	}
}
