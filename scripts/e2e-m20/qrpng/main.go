// Command qrpng vẽ ma trận QR mà panel trả ở POST /api/v1/account/2fa/setup
// (trường "qr": mỗi chuỗi một hàng, '1' là ô tối, không kèm vùng trắng) thành
// một ảnh PNG, để e2e M20 (scripts/e2e-m20.sh, T4) đưa vào máy ảo cho zbarimg
// đọc: zbarimg đọc ra đúng trường "uri" thì ma trận của máy chủ là QR thật của
// URI đó, không phụ thuộc cách giao diện vẽ.
//
//	go run ./scripts/e2e-m20/qrpng -vao <file JSON của setup> -ra <file PNG>
//
// Đồ nghề chạy trên Mac. File vào có chuỗi bí mật: chương trình không in gì
// của nó, chỉ in cỡ ma trận; ảnh ghi với quyền 0600.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
)

// quiet là vùng trắng mỗi bên (4 module, theo ISO/IEC 18004).
const quiet = 4

func main() {
	in := flag.String("vao", "", "file JSON có trường qr (câu trả lời của setup)")
	out := flag.String("ra", "", "file PNG ghi ra")
	scale := flag.Int("co", 8, "số pixel mỗi module")
	flag.Parse()
	if *in == "" || *out == "" || *scale < 1 {
		fmt.Fprintln(os.Stderr, "cách dùng: qrpng -vao <json> -ra <png> [-co 8]")
		os.Exit(2)
	}
	if err := run(*in, *out, *scale); err != nil {
		fmt.Fprintln(os.Stderr, "qrpng:", err)
		os.Exit(1)
	}
}

func run(in, out string, scale int) error {
	b, err := os.ReadFile(in)
	if err != nil {
		return err
	}
	var v struct {
		QR []string `json:"qr"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return errors.New("file vào không phải JSON") // không in nội dung: có bí mật
	}
	n := len(v.QR)
	if n < 21 {
		return fmt.Errorf("ma trận %d hàng: QR nhỏ nhất có 21", n)
	}
	for i, row := range v.QR {
		if len(row) != n {
			return fmt.Errorf("hàng %d dài %d, ma trận không vuông (%d hàng)", i, len(row), n)
		}
		for _, ch := range row {
			if ch != '0' && ch != '1' {
				return fmt.Errorf("hàng %d có ký tự khác '0', '1'", i)
			}
		}
	}
	size := (n + 2*quiet) * scale
	img := image.NewGray(image.Rect(0, 0, size, size))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	for y, row := range v.QR {
		for x, ch := range row {
			if ch != '1' {
				continue
			}
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					img.SetGray((x+quiet)*scale+dx, (y+quiet)*scale+dy, color.Gray{Y: 0})
				}
			}
		}
	}
	f, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Printf("ma trận %d×%d module, ảnh %d×%d px (vùng trắng %d module mỗi bên)\n", n, n, size, size, quiet)
	return nil
}
