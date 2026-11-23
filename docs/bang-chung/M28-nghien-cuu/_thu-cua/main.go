//go:build linux

// thu-cua: bản thử của đợt 0 mốc M28. So năm cách một tiến trình root đọc một
// đường dẫn nằm trong thư mục do user khác (user của site) kiểm soát:
//
//	readfile   os.ReadFile(path)                         (cách của v0.2.0 trong engine.Preview)
//	lstat      os.Lstat rồi os.ReadFile                  (cách của các Step v0.2.0)
//	osroot     os.OpenRoot(site).Open(rel)               (os.Root của Go)
//	openat2    openat2(RESOLVE_BENEATH|RESOLVE_NO_SYMLINKS)
//	walk       openat(O_NOFOLLOW) từng thành phần        (cách bản vá chọn)
//
// Ba cách cuối đều mở O_NONBLOCK, fstat trên chính fd đã mở, chỉ nhận file
// thường và có trần cỡ. Chạy trên máy ảo, không phải mã của tpan: thư mục có
// dấu "_" ở đầu nên `go build ./...` và `make check` bỏ qua.
//
// Cách dùng (root, trong máy ảo):
//
//	thu-cua ho-tro
//	thu-cua doc  <thư mục site> <đường dẫn con> <file bí mật>
//	thu-cua dua  <thư mục site> <đường dẫn con> <file bí mật> <số lần> <file|cha>
//	thu-cua do   <thư mục site> <đường dẫn con> <số lần>
package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"
)

// tran là trần cỡ của ba cách "an toàn". Hai cách đầu không có trần; ở đây
// chặn ở 64 MiB để bản thử không tự hết RAM khi đích là /dev/zero.
const (
	tran      = 1 << 20
	tranNgay  = 64 << 20
	hanMotLan = 3 * time.Second
)

type cach struct {
	ten string
	doc func(site, rel string) ([]byte, error)
}

var cacCach = []cach{
	{"readfile", docReadFile},
	{"lstat", docLstat},
	{"osroot", docOSRoot},
	{"openat2", docOpenat2},
	{"walk", docWalk},
}

func docReadFile(site, rel string) ([]byte, error) {
	f, err := os.Open(filepath.Join(site, rel))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, tranNgay))
}

func docLstat(site, rel string) ([]byte, error) {
	p := filepath.Join(site, rel)
	fi, err := os.Lstat(p)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("lstat: không phải file thường (%v)", fi.Mode().Type())
	}
	return docReadFile(site, rel)
}

func docOSRoot(site, rel string) ([]byte, error) {
	r, err := os.OpenRoot(site)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	f, err := r.OpenFile(rel, os.O_RDONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return docFD(f)
}

func docOpenat2(site, rel string) ([]byte, error) {
	dir, err := unix.Open(site, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	defer unix.Close(dir)
	fd, err := unix.Openat2(dir, rel, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_NONBLOCK | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return nil, fmt.Errorf("openat2: %w", err)
	}
	f := os.NewFile(uintptr(fd), rel)
	defer f.Close()
	return docFD(f)
}

func docWalk(site, rel string) ([]byte, error) {
	dir, err := unix.Open(site, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(rel, "/")
	for _, c := range parts[:len(parts)-1] {
		next, err := unix.Openat(dir, c, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(dir)
		if err != nil {
			return nil, fmt.Errorf("walk: thành phần %q: %w", c, err)
		}
		dir = next
	}
	defer unix.Close(dir)
	fd, err := unix.Openat(dir, parts[len(parts)-1], unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("walk: tên cuối: %w", err)
	}
	f := os.NewFile(uintptr(fd), rel)
	defer f.Close()
	return docFD(f)
}

// docFD: kiểm trên chính fd đã mở (không còn khoảng hở giữa kiểm và đọc), chỉ
// nhận file thường, có trần cỡ.
func docFD(f *os.File) ([]byte, error) {
	var st unix.Stat_t
	if err := unix.Fstat(int(f.Fd()), &st); err != nil {
		return nil, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, fmt.Errorf("fstat: không phải file thường (loại %#o)", st.Mode&unix.S_IFMT)
	}
	if st.Size > tran {
		return nil, fmt.Errorf("fstat: %d byte, quá trần %d", st.Size, tran)
	}
	b, err := io.ReadAll(io.LimitReader(f, tran+1))
	if err != nil {
		return nil, err
	}
	if len(b) > tran {
		return nil, fmt.Errorf("đọc: quá trần %d", tran)
	}
	return b, nil
}

// motLan chạy một cách đọc có hạn giờ. Hết hạn thì báo TREO; goroutine đang
// kẹt bị bỏ lại (bản thử thoát ngay sau đó).
func motLan(c cach, site, rel string) (b []byte, err error, treo bool, dt time.Duration) {
	type kq struct {
		b   []byte
		err error
	}
	ch := make(chan kq, 1)
	t0 := time.Now()
	go func() {
		b, err := c.doc(site, rel)
		ch <- kq{b, err}
	}()
	select {
	case r := <-ch:
		return r.b, r.err, false, time.Since(t0)
	case <-time.After(hanMotLan):
		return nil, nil, true, time.Since(t0)
	}
}

func nhan(b []byte, err error, treo bool, biMat []byte) string {
	switch {
	case treo:
		return fmt.Sprintf("TREO (quá %v)", hanMotLan)
	case err != nil:
		return "từ chối: " + loi(err)
	case len(biMat) > 0 && bytes.Contains(b, biMat):
		return fmt.Sprintf("LỘ: đọc được %d byte của file bí mật", len(b))
	case len(b) >= tranNgay:
		return fmt.Sprintf("ĐỌC KHÔNG TRẦN: dừng ở %d MiB (bản thật đọc tới hết RAM)", len(b)>>20)
	}
	return fmt.Sprintf("đọc được %d byte (không phải file bí mật)", len(b))
}

func loi(err error) string {
	var en unix.Errno
	if errors.As(err, &en) {
		return fmt.Sprintf("%s (%s)", unix.ErrnoName(en), err)
	}
	return err.Error()
}

func cmdDoc(site, rel, biMatFile string) {
	biMat, _ := os.ReadFile(biMatFile)
	for _, c := range cacCach {
		b, err, treo, dt := motLan(c, site, rel)
		fmt.Printf("  %-9s %-70s %6.1f ms\n", c.ten, nhan(b, err, treo, biMat), float64(dt.Microseconds())/1000)
	}
}

// cmdDua: một goroutine tráo liên tục giữa "file thường của site" và "symlink
// tới file bí mật" (kieu=file), hoặc giữa "thư mục thật" và "symlink tới thư
// mục của nạn nhân" (kieu=cha), bằng renameat2(RENAME_EXCHANGE): lúc nào ở
// tên đó cũng có một trong hai thứ. Trong lúc đó mỗi cách đọc chạy n lần.
func cmdDua(site, rel, biMatFile string, n int, kieu string) {
	biMat, err := os.ReadFile(biMatFile)
	if err != nil || len(biMat) == 0 {
		fmt.Println("không đọc được file bí mật:", err)
		os.Exit(2)
	}
	ten := filepath.Join(site, rel)
	if kieu == "cha" {
		ten = filepath.Dir(ten)
	}
	kia := ten + ".kia" // script dựng sẵn: symlink (kieu=file) hoặc symlink tới thư mục nạn nhân (kieu=cha)
	var dung atomic.Bool
	var soTrao atomic.Int64
	xong := make(chan struct{})
	go func() {
		defer close(xong)
		for !dung.Load() {
			if err := unix.Renameat2(unix.AT_FDCWD, ten, unix.AT_FDCWD, kia, unix.RENAME_EXCHANGE); err == nil {
				soTrao.Add(1)
			}
		}
	}()
	for _, c := range cacCach {
		var lo, tuChoi, ok int
		t0, s0 := time.Now(), soTrao.Load()
		for i := 0; i < n; i++ {
			b, err := c.doc(site, rel)
			switch {
			case err != nil:
				tuChoi++
			case bytes.Contains(b, biMat):
				lo++
			default:
				ok++
			}
		}
		fmt.Printf("  %-9s %d lần: LỘ %d, đọc file của site %d, từ chối %d (%d lần tráo, %v)\n",
			c.ten, n, lo, ok, tuChoi, soTrao.Load()-s0, time.Since(t0).Round(time.Millisecond))
	}
	dung.Store(true)
	<-xong
	// Trả về trạng thái "tên đó là thứ thật": tráo thêm một lần nếu đang là symlink.
	if fi, err := os.Lstat(ten); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		_ = unix.Renameat2(unix.AT_FDCWD, ten, unix.AT_FDCWD, kia, unix.RENAME_EXCHANGE)
	}
}

func cmdDo(site, rel string, n int) {
	for _, c := range cacCach {
		t0 := time.Now()
		for i := 0; i < n; i++ {
			if _, err := c.doc(site, rel); err != nil {
				fmt.Printf("  %-9s lỗi: %s\n", c.ten, loi(err))
				break
			}
		}
		fmt.Printf("  %-9s %d lần đọc: %d ns mỗi lần\n", c.ten, n, time.Since(t0).Nanoseconds()/int64(n))
	}
}

func cmdHoTro() {
	var u unix.Utsname
	_ = unix.Uname(&u)
	fmt.Printf("kernel: %s\n", unix.ByteSliceToString(u.Release[:]))
	fmt.Printf("go: %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	fd, err := unix.Openat2(unix.AT_FDCWD, "/etc/hostname", &unix.OpenHow{
		Flags: unix.O_RDONLY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_NO_SYMLINKS,
	})
	if err != nil {
		fmt.Printf("openat2: KHÔNG dùng được: %s\n", loi(err))
	} else {
		unix.Close(fd)
		fmt.Println("openat2: dùng được (RESOLVE_NO_SYMLINKS trên /etc/hostname mở được)")
	}
	if r, err := os.OpenRoot("/etc"); err != nil {
		fmt.Println("os.Root: KHÔNG dùng được:", err)
	} else {
		r.Close()
		fmt.Println("os.Root: dùng được")
	}
	for _, k := range []string{"protected_symlinks", "protected_hardlinks", "protected_fifos", "protected_regular"} {
		b, err := os.ReadFile("/proc/sys/fs/" + k)
		fmt.Printf("fs.%s = %s (%v)\n", k, strings.TrimSpace(string(b)), err)
	}
}

func so(s string) int {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil || n <= 0 {
		fmt.Println("số lần không hợp lệ:", s)
		os.Exit(2)
	}
	return n
}

func main() {
	a := os.Args[1:]
	switch {
	case len(a) == 1 && a[0] == "ho-tro":
		cmdHoTro()
	case len(a) == 4 && a[0] == "doc":
		cmdDoc(a[1], a[2], a[3])
	case len(a) == 6 && a[0] == "dua":
		cmdDua(a[1], a[2], a[3], so(a[4]), a[5])
	case len(a) == 4 && a[0] == "do":
		cmdDo(a[1], a[2], so(a[3]))
	default:
		fmt.Println("cách dùng: thu-cua ho-tro | doc <site> <rel> <bí mật> | dua <site> <rel> <bí mật> <n> <file|cha> | do <site> <rel> <n>")
		os.Exit(2)
	}
}
