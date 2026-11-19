package steps

import (
	"context"
	"testing"

	"github.com/doxuta/tpan/internal/system"
)

// Lỗi thấy trên máy ảo M12: bản sao máy ảo vừa khởi động có apt-daily chạy
// `apt-get update`, và `tpan update --check` lỗi ngay vì apt 2.8 không chờ khoá
// /var/lib/apt/lists/lock dù có -o DPkg::Lock::Timeout=300. RunAptUpdate phải
// chờ rồi chạy lại khi gặp đúng lỗi đó, và chỉ lỗi đó.
func TestRunAptUpdateWaitsForListsLock(t *testing.T) {
	listsLockWait = 0
	t.Cleanup(func() { listsLockWait = 5e9 })
	const lockErr = "E: Could not get lock /var/lib/apt/lists/lock. It is held by process 1044 (apt-get)\nE: Unable to lock directory /var/lib/apt/lists/"
	for _, tc := range []struct {
		name      string
		failFirst int    // số lần đầu trả lỗi
		stderr    string // lỗi của những lần đó
		ok        bool
		calls     int
	}{
		{"không lỗi", 0, "", true, 1},
		{"khoá bị giữ 2 lần rồi nhả", 2, lockErr, true, 3},
		{"khoá bị giữ mãi", 1 << 30, lockErr, false, listsLockTries},
		{"lỗi khác không chạy lại", 1 << 30, "E: Failed to fetch https://example.com/dists/noble/InRelease", false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := system.NewFake()
			n := 0
			m.Handle([]string{"apt-get", "update"}, func(system.Cmd) system.Result {
				n++
				if n <= tc.failFirst {
					return system.Result{ExitCode: 100, Stderr: tc.stderr}
				}
				return system.Result{}
			})
			_, err := RunAptUpdate(context.Background(), m, aptUpdateLocked)
			if (err == nil) != tc.ok || n != tc.calls {
				t.Errorf("err = %v, %d lần chạy; muốn ok=%v, %d lần", err, n, tc.ok, tc.calls)
			}
		})
	}
}
