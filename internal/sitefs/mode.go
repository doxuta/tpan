package sitefs

import (
	"io/fs"

	"golang.org/x/sys/unix"
)

// toKernel đổi fs.FileMode của Go thành các bit mode của kernel, giống
// syscallMode trong package os: fs.ModeSetgid là bit 22 ở Go nhưng là 02000
// ở kernel. Đưa thẳng uint32(perm) vào fchmod sẽ lặng lẽ mất setgid và biến
// thư mục site 2750 thành 0750 (S3).
func toKernel(perm fs.FileMode) uint32 {
	m := uint32(perm.Perm())
	if perm&fs.ModeSetuid != 0 {
		m |= unix.S_ISUID
	}
	if perm&fs.ModeSetgid != 0 {
		m |= unix.S_ISGID
	}
	if perm&fs.ModeSticky != 0 {
		m |= unix.S_ISVTX
	}
	return m
}

// fromKernel dựng fs.FileMode từ Stat_t, giống fillFileStatFromSys trong
// package os, để Lstat cho cùng Mode với os.Lstat (B16).
func fromKernel(st *unix.Stat_t) fs.FileMode {
	k := uint32(st.Mode)
	m := fs.FileMode(k & 0o777)
	switch k & unix.S_IFMT {
	case unix.S_IFBLK:
		m |= fs.ModeDevice
	case unix.S_IFCHR:
		m |= fs.ModeDevice | fs.ModeCharDevice
	case unix.S_IFDIR:
		m |= fs.ModeDir
	case unix.S_IFIFO:
		m |= fs.ModeNamedPipe
	case unix.S_IFLNK:
		m |= fs.ModeSymlink
	case unix.S_IFSOCK:
		m |= fs.ModeSocket
	}
	if k&unix.S_ISGID != 0 {
		m |= fs.ModeSetgid
	}
	if k&unix.S_ISUID != 0 {
		m |= fs.ModeSetuid
	}
	if k&unix.S_ISVTX != 0 {
		m |= fs.ModeSticky
	}
	return m
}

func fileType(st *unix.Stat_t) uint32 { return uint32(st.Mode) & unix.S_IFMT }

func toInfo(st *unix.Stat_t) Info {
	return Info{Mode: fromKernel(st), Size: st.Size, UID: st.Uid, GID: st.Gid, Nlink: uint64(st.Nlink)}
}
