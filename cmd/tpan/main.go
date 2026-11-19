// Lệnh tpan: điểm vào duy nhất của công cụ.
//
// Hiện mới có khung: `tpan version` và `tpan serve`.
// Các lệnh thật (stack, site, apply, doctor...) được thêm theo docs/KE-HOACH.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/doxuta/tpan/internal/buildinfo"
	"github.com/doxuta/tpan/internal/server"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "version":
		fmt.Println(buildinfo.String())
	case "serve":
		if err := serve(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "tpan:", err)
			os.Exit(1)
		}
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "tpan: không có lệnh %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	listen := fs.String("listen", "127.0.0.1:8080", "địa chỉ và cổng để nghe")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Ctrl+C hoặc `systemctl stop` gửi tín hiệu; ctx bị huỷ và server tắt êm.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return server.Run(ctx, *listen)
}

func usage() {
	fmt.Fprint(os.Stderr, `tpan - panel tự động hoá LAMP/LEMP và WordPress

Cách dùng:
  tpan version              in phiên bản
  tpan serve [--listen A]   chạy giao diện web và API (mặc định 127.0.0.1:8080)
`)
}
