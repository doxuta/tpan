//go:build linux

package sitefs

import (
	"errors"

	"golang.org/x/sys/unix"
)

// chownLink đổi chủ của chính symlink name (trong thư mục dirfd) mà không đi
// theo tên lần thứ hai: mở cái link bằng O_PATH|O_NOFOLLOW, fstat trên fd phải
// là symlink, rồi fchownat(fd, "", AT_EMPTY_PATH) trên chính fd đó. Đổi chủ
// theo tên (fchownat(dirfd, name, AT_SYMLINK_NOFOLLOW)) thì giữa symlinkat và
// lúc đổi chủ, user của site tráo được một hardlink vào tên đó và root đổi chủ
// file kia (soát cuối M28, C8; cần fs.protected_hardlinks=0).
func chownLink(dirfd int, name string, uid, gid int) error {
	fd, err := openat(dirfd, name, unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer closeFD(fd)
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFLNK {
		return errors.New("tên vừa tạo symlink đã bị thay bằng thứ khác")
	}
	return unix.Fchownat(fd, "", uid, gid, unix.AT_EMPTY_PATH)
}
