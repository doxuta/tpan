package core

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/account"
)

// pkgWith dựng gói có lồng bật (trừ khi muốn khác) và đúng một trần đặt sẵn.
func pkgWith(cage bool, set func(p *account.Package)) account.Package {
	p := account.Package{Name: "gt", Cage: cage}
	if set != nil {
		set(&p)
	}
	return p
}

// wantRefused: err phải là *RefusedError mang mã code, lời hai thứ tiếng, và
// (nếu có) mọi chuỗi in trong cả hai lời.
func wantRefused(t *testing.T, err error, code string, in ...string) {
	t.Helper()
	var re *RefusedError
	if !errors.As(err, &re) {
		t.Fatalf("lỗi = %v; muốn *RefusedError mã %s", err, code)
	}
	if re.Code != code {
		t.Errorf("mã = %s (%s); muốn %s", re.Code, re.Message.Vi, code)
	}
	if re.Message.Vi == "" || re.Message.En == "" {
		t.Errorf("thiếu lời ở một ngôn ngữ: %+v", re.Message)
	}
	for _, w := range in {
		if !strings.Contains(re.Message.Vi, w) || !strings.Contains(re.Message.En, w) {
			t.Errorf("lời từ chối thiếu %q:\n  vi: %s\n  en: %s", w, re.Message.Vi, re.Message.En)
		}
	}
}

// Biên của từng trường: 0 hợp lệ, min và max hợp lệ, min-1 và max+1 bị từ chối
// kèm tên trường và khoảng cho phép.
func TestCheckPackageLimitsRanges(t *testing.T) {
	fields := []struct {
		name     string
		min, max int
		set      func(p *account.Package, v int)
	}{
		{"cpu_percent", CageCPUMin, CageCPUMax, func(p *account.Package, v int) { p.CPUPercent = v }},
		{"ram_mb", CageRAMMin, CageRAMMax, func(p *account.Package, v int) { p.RAMMB = v }},
		{"tasks", CageTasksMin, CageTasksMax, func(p *account.Package, v int) { p.Tasks = v }},
		{"io_read_mb", CageIOMin, CageIOMax, func(p *account.Package, v int) { p.IOReadMB = v }},
		{"io_write_mb", CageIOMin, CageIOMax, func(p *account.Package, v int) { p.IOWriteMB = v }},
		{"php_workers", CageWorkersMin, CageWorkersMax, func(p *account.Package, v int) { p.PHPWorkers = v }},
	}
	for _, f := range fields {
		t.Run(f.name, func(t *testing.T) {
			build := func(v int) account.Package { return pkgWith(true, func(p *account.Package) { f.set(p, v) }) }
			for _, ok := range []int{0, f.min, f.min + 1, f.max - 1, f.max} {
				// tasks ≥ worker + slack: với worker mặc định 5 thì tối thiểu 20 < 32 nên không vướng.
				if err := checkPackageLimits(build(ok)); err != nil {
					t.Errorf("%s = %d phải hợp lệ: %v", f.name, ok, err)
				}
			}
			for _, bad := range []int{f.min - 1, f.max + 1, -1} {
				if bad == 0 {
					continue // CageIOMin-1 = 0 là hợp lệ
				}
				err := checkPackageLimits(build(bad))
				wantRefused(t, err, CodePackageLimitInvalid, f.name, fmt.Sprint(f.min), fmt.Sprint(f.max))
			}
		})
	}
}

// Lời từ chối ngoài khoảng nêu giá trị đã gõ để người dùng thấy mình gõ gì.
func TestCheckPackageLimitsMessageNamesValue(t *testing.T) {
	err := checkPackageLimits(pkgWith(true, func(p *account.Package) { p.RAMMB = 10 }))
	wantRefused(t, err, CodePackageLimitInvalid, "ram_mb", "10", "64", "1048576")
}

// tasks ≥ số worker + CageTasksSlack, số worker là PHPWorkers hoặc mặc định 5.
func TestCheckPackageLimitsTasksVsWorkers(t *testing.T) {
	tests := []struct {
		name           string
		workers, tasks int
		ok             bool
	}{
		// Số worker nhỏ: khoảng riêng của tasks (≥ 32) là ràng buộc chặt hơn.
		{"10 worker, tasks 31 (dưới min của tasks)", 10, 31, false},
		{"10 worker, tasks 32 (min)", 10, 32, true},
		{"mặc định 5 worker, tasks 19", 0, 19, false},
		{"mặc định 5 worker, tasks 20 (dưới min của tasks)", 0, 20, false},
		{"mặc định 5 worker, tasks 32 (min)", 0, 32, true},
		{"mặc định, tasks 0 (không giới hạn)", 0, 0, true},
		{"worker 1, tasks 32", 1, 32, true},
		// Số worker lớn: worker + 15 vượt min, ràng buộc này mới có tác dụng.
		{"20 worker, tasks 34", 20, 34, false},
		{"20 worker, tasks 35", 20, 35, true},
		{"30 worker, tasks 44", 30, 44, false},
		{"30 worker, tasks 45", 30, 45, true},
		{"200 worker, tasks 214", 200, 214, false},
		{"200 worker, tasks 215", 200, 215, true},
		{"200 worker, tasks 100000", 200, 100000, true},
		{"tasks 0 mặc kệ worker", 200, 0, true},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			err := checkPackageLimits(pkgWith(true, func(p *account.Package) { p.PHPWorkers, p.Tasks = c.workers, c.tasks }))
			if c.ok {
				if err != nil {
					t.Fatalf("muốn hợp lệ: %v", err)
				}
				return
			}
			wantRefused(t, err, CodePackageLimitInvalid, "tasks")
		})
	}

	// Lời từ chối nêu số cần có và số worker đang dùng.
	err := checkPackageLimits(pkgWith(true, func(p *account.Package) { p.PHPWorkers, p.Tasks = 20, 34 }))
	wantRefused(t, err, CodePackageLimitInvalid, "tasks", "php_workers", "20", "35")

	// Với worker mặc định 5, worker + slack = 20 < CageTasksMin: ràng buộc này
	// chưa bao giờ chạm tới. Nếu ai nâng slack hay hạ CageTasksMin thì phải để
	// mắt tới ca "mặc định" ở bảng trên.
	if account.DefaultPHPWorkers+CageTasksSlack > CageTasksMin {
		t.Errorf("mặc định %d + slack %d = %d vượt CageTasksMin %d: thêm ca tasks nằm giữa vào bảng",
			account.DefaultPHPWorkers, CageTasksSlack, account.DefaultPHPWorkers+CageTasksSlack, CageTasksMin)
	}
}

// Trần mà gói không bật lồng: từ chối, kể cả php_workers (pool ngoài lồng luôn 5).
func TestCheckPackageLimitsNeedCage(t *testing.T) {
	sets := map[string]func(p *account.Package){
		"cpu_percent": func(p *account.Package) { p.CPUPercent = 100 },
		"ram_mb":      func(p *account.Package) { p.RAMMB = 512 },
		"tasks":       func(p *account.Package) { p.Tasks = 100 },
		"io_read_mb":  func(p *account.Package) { p.IOReadMB = 50 },
		"io_write_mb": func(p *account.Package) { p.IOWriteMB = 50 },
		"php_workers": func(p *account.Package) { p.PHPWorkers = 8 },
	}
	for name, set := range sets {
		t.Run(name, func(t *testing.T) {
			err := checkPackageLimits(pkgWith(false, set))
			wantRefused(t, err, CodePackageLimitsNeedCage, name, "--cage on")
			// Cùng trần mà bật lồng thì đạt.
			if err := checkPackageLimits(pkgWith(true, set)); err != nil {
				t.Errorf("có lồng mà vẫn từ chối: %v", err)
			}
		})
	}
	// Không trần nào: gói không lồng đạt (mọi gói cũ), gói có lồng cũng đạt.
	for _, cage := range []bool{false, true} {
		if err := checkPackageLimits(pkgWith(cage, nil)); err != nil {
			t.Errorf("cage=%v không trần mà bị từ chối: %v", cage, err)
		}
	}
	// Nhiều trần: lời từ chối nêu hết các trường đang đặt.
	err := checkPackageLimits(pkgWith(false, func(p *account.Package) { p.RAMMB, p.CPUPercent = 256, 50 }))
	wantRefused(t, err, CodePackageLimitsNeedCage, "ram_mb", "cpu_percent")
}

// Thứ tự: giới hạn cũ âm là args_invalid như trước M17; ngoài khoảng đến trước
// "cần lồng" (sửa số trước rồi mới tới cờ).
func TestCheckPackageLimitsOrder(t *testing.T) {
	err := checkPackageLimits(account.Package{MaxSites: -1, Cage: true, RAMMB: 10})
	wantRefused(t, err, "args_invalid")
	err = checkPackageLimits(pkgWith(false, func(p *account.Package) { p.RAMMB = 10 }))
	wantRefused(t, err, CodePackageLimitInvalid, "ram_mb")
}

// AddPackage và EditPackage gọi checkPackageLimits: không lưu gì khi từ chối.
func TestPackageM17ThroughAddAndEdit(t *testing.T) {
	ue := newUsrEnv(t)
	bad := account.Package{Name: "gt", RAMMB: 512} // cần lồng
	wantRefused(t, ue.a.AddPackage(ctx, bad), CodePackageLimitsNeedCage)
	if _, ok, _ := ue.a.Store.Package(ctx, "gt"); ok {
		t.Error("gói bị từ chối vẫn được lưu")
	}

	good := account.Package{Name: "gt", Cage: true, RAMMB: 512, CPUPercent: 100, PHPWorkers: 20, Tasks: 35}
	if err := ue.a.AddPackage(ctx, good); err != nil {
		t.Fatalf("AddPackage: %v", err)
	}
	got, ok, _ := ue.a.Store.Package(ctx, "gt")
	if !ok || got.RAMMB != 512 || !got.Cage || got.Tasks != 35 {
		t.Fatalf("gói đã lưu = %+v (ok %v)", got, ok)
	}

	good.Tasks = 34
	wantRefused(t, ue.a.EditPackage(ctx, good), CodePackageLimitInvalid, "tasks")
	if got, _, _ := ue.a.Store.Package(ctx, "gt"); got.Tasks != 35 {
		t.Errorf("EditPackage bị từ chối vẫn đổi gói: tasks = %d", got.Tasks)
	}
	good.Tasks, good.Cage = 35, false
	wantRefused(t, ue.a.EditPackage(ctx, good), CodePackageLimitsNeedCage)
}
