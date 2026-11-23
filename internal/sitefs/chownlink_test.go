package sitefs

import (
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

// Soát cuối M28, C8: chownLink chỉ đổi chủ khi thứ đang ở tên đó đúng là một
// symlink. Tên bị tráo thành file thường (hardlink tới file của root, trên máy
// tắt fs.protected_hardlinks) giữa symlinkat và lúc đổi chủ thì từ chối,
// không đổi chủ file đó.
func TestCuaSymlinkKhongDoiChuThuBiTrao(t *testing.T) {
	z := newZone(t, false)
	mustWrite(t, z.p("thay"), "x", 0o600)
	if err := os.Symlink("dich", z.p("lien")); err != nil {
		t.Fatal(err)
	}
	dir, err := unix.Open(z.anchor, unix.O_RDONLY|unix.O_DIRECTORY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer closeFD(dir)
	uid, gid := os.Getuid(), os.Getgid()
	if err := chownLink(dir, "thay", uid, gid); err == nil {
		t.Fatal("chownLink trên file thường phải từ chối")
	}
	if err := chownLink(dir, "lien", uid, gid); err != nil {
		t.Fatalf("chownLink trên symlink: %v", err)
	}
	if err := chownLink(dir, "khong-co", uid, gid); err == nil {
		t.Fatal("chownLink trên tên không có phải lỗi")
	}
}
