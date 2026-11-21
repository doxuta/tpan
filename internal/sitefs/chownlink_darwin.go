//go:build darwin

package sitefs

import (
	"errors"

	"golang.org/x/sys/unix"
)

// chownLink: bản macOS (chỉ để unit test chạy trên máy dev). O_SYMLINK mở
// chính cái link, fstat trên fd phải là symlink, rồi fchown trên fd. Xem bản
// Linux để biết vì sao không đổi chủ theo tên.
func chownLink(dirfd int, name string, uid, gid int) error {
	fd, err := openat(dirfd, name, unix.O_SYMLINK|unix.O_RDONLY|unix.O_CLOEXEC, 0)
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
	return unix.Fchown(fd, uid, gid)
}
