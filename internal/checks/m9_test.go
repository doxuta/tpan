package checks

import (
	"errors"
	"io/fs"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

const backupDir = "/var/lib/tpan/backups/_tpan"

var m9Now = time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

func m9Clock() time.Time { return m9Now }

func backupFake(files ...string) *system.FakeSystem {
	f := system.NewFake()
	f.SetDir(backupDir, 0o700, system.Owner{})
	for _, n := range files {
		f.SetFile(backupDir+"/"+n, []byte("SQLite format 3\x00"), 0o600, system.Owner{})
	}
	return f
}

func TestTPANBackupAgePassBanMoi(t *testing.T) {
	// Bản cũ và bản mới: phải chọn theo giờ trong tên, không theo thứ tự tên
	// (ở đây trùng nhau, nên thêm một tên dạng lạ để kiểm bỏ qua).
	fake := backupFake("tpan-20260501-030000.db", "tpan-20260510-030000.db", "ghi-chu.txt")
	r := TPANBackupAge(backupDir, 36*time.Hour, m9Clock).Run(bg(), fake)
	assertShape(t, r, "tpan.backup:db", StageTPAN, Pass)
	assertNoCmd(t, fake)
	if len(r.Evidence) != 1 || r.Evidence[0].Command != "ls "+backupDir+" (tpan)" || r.Evidence[0].ExitCode != 0 {
		t.Fatalf("Evidence = %+v", r.Evidence)
	}
	if !strings.Contains(r.Evidence[0].Output, "tpan-20260510-030000.db") {
		t.Errorf("Evidence phải nêu file mới nhất: %q", r.Evidence[0].Output)
	}
}

func TestTPANBackupAgeChonMoiNhatTheoGioKhongTheoViTri(t *testing.T) {
	// Giữa tháng 9 và tháng 10: tên so sánh theo chuỗi cũng đúng, nhưng kiểm cả
	// trường hợp thứ tự thư mục bị đảo bằng cách đặt bản mới ở giữa.
	fake := backupFake("tpan-20260101-000000.db", "tpan-20260510-110000.db", "tpan-20260301-000000.db")
	r := TPANBackupAge(backupDir, 2*time.Hour, m9Clock).Run(bg(), fake)
	assertShape(t, r, "tpan.backup:db", StageTPAN, Pass) // 11:00 cách 12:00 là 1 giờ
}

func TestTPANBackupAgeCuWarn(t *testing.T) {
	fake := backupFake("tpan-20260507-030000.db") // 3 ngày 9 giờ trước = 81 giờ
	r := TPANBackupAge(backupDir, 36*time.Hour, m9Clock).Run(bg(), fake)
	assertShape(t, r, "tpan.backup:db", StageTPAN, Warn)
	if !strings.Contains(r.Summary.Vi, "81") || !strings.Contains(r.Summary.En, "81") {
		t.Errorf("Summary phải nêu số giờ: %+v", r.Summary)
	}
	if lastFix(r) != "ls "+backupDir {
		t.Errorf("dòng cuối ManualFix phải là lệnh kiểm lại: %q", r.ManualFix)
	}
	if !strings.Contains(strings.Join(r.ManualFix, "\n"), "sudo tpan backup create --all --yes") {
		t.Errorf("ManualFix thiếu lệnh tạo backup: %q", r.ManualFix)
	}
}

func TestTPANBackupAgeDungNguongLaPass(t *testing.T) {
	fake := backupFake("tpan-20260509-000000.db") // đúng 36 giờ
	r := TPANBackupAge(backupDir, 36*time.Hour, m9Clock).Run(bg(), fake)
	assertShape(t, r, "tpan.backup:db", StageTPAN, Pass)
}

func TestTPANBackupAgeChuaCoBanNao(t *testing.T) {
	cases := map[string]*system.FakeSystem{
		"thư mục rỗng":     backupFake(),
		"chỉ có tên lạ":    backupFake("tpan.db", "tpan-20261340-000000.db", "tpan-20260510-030000.db.part", "x-20260510-030000.db"),
		"không có thư mục": system.NewFake(),
	}
	for name, fake := range cases {
		t.Run(name, func(t *testing.T) {
			r := TPANBackupAge(backupDir, 36*time.Hour, m9Clock).Run(bg(), fake)
			assertShape(t, r, "tpan.backup:db", StageTPAN, Warn)
			if !strings.Contains(r.Summary.Vi, "chưa có bản sao lưu tpan.db nào") {
				t.Errorf("Summary = %q", r.Summary.Vi)
			}
			fix := strings.Join(r.ManualFix, "\n")
			for _, want := range []string{"sudo tpan backup create --all --yes", "sudo tpan backup settings --at 03:30"} {
				if !strings.Contains(fix, want) {
					t.Errorf("ManualFix thiếu %q: %q", want, r.ManualFix)
				}
			}
			if lastFix(r) != "ls "+backupDir {
				t.Errorf("dòng cuối ManualFix = %q", lastFix(r))
			}
		})
	}
}

// readDirErr là FakeSystem có ReadDir luôn lỗi: FakeSystem.Fail không hỗ trợ
// ReadDir, mà phép kiểm cần thử đúng nhánh "lỗi đọc khác không có".
type readDirErr struct {
	*system.FakeSystem
	err error
}

func (r readDirErr) ReadDir(string) ([]string, error) { return nil, r.err }

func TestTPANBackupAgeReadDirLoiKhacLaUnknown(t *testing.T) {
	for name, err := range map[string]error{
		"hỏng đĩa":         errors.New("input/output error"),
		"thiếu quyền":      &fs.PathError{Op: "open", Path: backupDir, Err: fs.ErrPermission},
		"không là thư mục": &fs.PathError{Op: "readdirent", Path: backupDir, Err: syscall.ENOTDIR},
	} {
		t.Run(name, func(t *testing.T) {
			sys := readDirErr{backupFake("tpan-20260510-030000.db"), err}
			r := TPANBackupAge(backupDir, 36*time.Hour, m9Clock).Run(bg(), sys)
			assertShape(t, r, "tpan.backup:db", StageTPAN, Unknown)
			if len(r.Evidence) != 1 || r.Evidence[0].ExitCode != -1 || r.Evidence[0].Output == "" {
				t.Errorf("Evidence = %+v", r.Evidence)
			}
		})
	}
}

func TestTPANBackupAgeFileMuiGioTuongLai(t *testing.T) {
	// Đồng hồ máy lùi lại sau khi sao lưu: tuổi âm không được coi là cũ.
	fake := backupFake("tpan-20260511-030000.db")
	r := TPANBackupAge(backupDir, 36*time.Hour, m9Clock).Run(bg(), fake)
	assertShape(t, r, "tpan.backup:db", StageTPAN, Pass)
}

func TestTPANBackupAgeMacDinh(t *testing.T) {
	// dir rỗng thì dùng site.TPANDBBackupDir; now nil thì dùng giờ thật (bản
	// quá cũ so với giờ thật nên chắc chắn Warn).
	f := system.NewFake()
	f.SetDir(site.TPANDBBackupDir, 0o700, system.Owner{})
	f.SetFile(site.TPANDBBackupDir+"/tpan-20200101-000000.db", []byte("x"), 0o600, system.Owner{})
	r := TPANBackupAge("", time.Hour, nil).Run(bg(), f)
	assertShape(t, r, "tpan.backup:db", StageTPAN, Warn)
	if !strings.Contains(r.Evidence[0].Command, site.TPANDBBackupDir) {
		t.Errorf("Evidence = %+v", r.Evidence)
	}
}
