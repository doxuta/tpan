//go:build ignore

// Nghiên cứu của mốc M16 (đợt 0): đo tương phản WCAG 2.1 của bảng màu bản 2
// trước khi chốt, để giá trị "điểm xuất phát" của chế độ tối được đo rồi chỉnh.
//
// Chạy trên Mac, ở gốc repo:
//
//	go run docs/bang-chung/M16-nghien-cuu/t4-tuong-phan.go
//
// Công thức: độ chói tương đối và tỷ lệ tương phản của WCAG 2.1 (mục 1.4.3 và
// 1.4.11). Ngưỡng: chữ từ 4,5:1; hình và ranh giới ô nhập từ 3:1. Dòng "trang
// trí" chỉ để biết con số, không có ngưỡng.
//
// Phép kiểm lâu dài nằm ở web/tuong_phan_test.go (đọc thẳng web/src/styles.css);
// file này chỉ là bước thử các giá trị ứng viên.
package main

import (
	"fmt"
	"math"
	"strconv"
)

type rgb [3]float64

func hex(s string) rgb {
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		panic(s)
	}
	return rgb{float64(v >> 16 & 255), float64(v >> 8 & 255), float64(v & 255)}
}

func lum(c rgb) float64 {
	f := func(v float64) float64 {
		v /= 255
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*f(c[0]) + 0.7152*f(c[1]) + 0.0722*f(c[2])
}

func ratio(a, b rgb) float64 {
	la, lb := lum(a), lum(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// mix trộn p phần trăm màu a lên nền b trong sRGB, như color-mix(in srgb, a p%, b).
func mix(a, b rgb, p float64) string {
	var o [3]int
	for i := range o {
		o[i] = int(math.Round(a[i]*p/100 + b[i]*(100-p)/100))
	}
	return fmt.Sprintf("#%02X%02X%02X", o[0], o[1], o[2])
}

type palette map[string]string

func main() {
	light := palette{
		"ink": "#171A1F", "ink-soft": "#2B3138", "muted": "#556070", "paper": "#EEF1F4", "surface": "#FFFFFF", "soft": "#F6F7F9",
		"hair": "#DDE1E6", "line": "#C5CBD3", "field": "#7A8592", "accent": "#0B4FB8", "accent-hover": "#083A87", "on-accent": "#FFFFFF",
		"pre": "#E1E4E1", "track": "#D2D5D7", "route": "#171A1F", "route-dash": "#7E848A",
		"ok": "#168A3A", "ok-text": "#12752F", "warn": "#E0A100", "warn-border": "#16181A", "fail": "#C8320A", "on-fail": "#FFFFFF", "fail-text": "#B42A06", "unknown": "#7E848A",
	}
	dark := palette{
		"ink": "#E9EBED", "ink-soft": "#CDD2D7", "muted": "#A3A9AF", "paper": "#111316", "surface": "#1A1D21", "soft": "#20242A",
		"hair": "#2A2F36", "line": "#3B424B", "field": "#737C87", "accent": "#8FB8FF", "accent-hover": "#B9D2FF", "on-accent": "#0B1220",
		"pre": "#101214", "track": "#30353A", "route": "#B8BEC4", "route-dash": "#6B727A",
		"ok": "#45C97A", "ok-text": "#45C97A", "warn": "#F2C14E", "warn-border": "#15171A", "fail": "#FF6B4A", "on-fail": "#190B07", "fail-text": "#FF8A70", "unknown": "#8A9096",
	}
	light["accent-tint"] = mix(hex(light["accent"]), hex(light["surface"]), 10)
	dark["accent-tint"] = mix(hex(dark["accent"]), hex(dark["surface"]), 16)

	type pair struct {
		fg, bg string
		min    float64 // 4.5 chữ, 3 hình, 0 trang trí
	}
	var pairs []pair
	text := func(fg string, bgs ...string) {
		for _, bg := range bgs {
			pairs = append(pairs, pair{fg, bg, 4.5})
		}
	}
	shape := func(fg string, bgs ...string) {
		for _, bg := range bgs {
			pairs = append(pairs, pair{fg, bg, 3})
		}
	}
	deco := func(fg string, bgs ...string) {
		for _, bg := range bgs {
			pairs = append(pairs, pair{fg, bg, 0})
		}
	}
	text("ink", "paper", "surface", "soft", "pre", "accent-tint")
	text("ink-soft", "surface", "paper", "soft")
	text("muted", "paper", "surface", "soft", "pre")
	text("accent", "surface", "paper", "soft", "accent-tint")
	text("accent-hover", "surface", "paper", "soft")
	text("on-accent", "accent", "accent-hover")
	text("fail-text", "paper", "surface", "soft", "pre")
	text("ok-text", "paper", "surface", "soft")
	text("on-fail", "fail")
	shape("ok", "paper", "surface", "soft")
	shape("fail", "paper", "surface", "soft")
	shape("unknown", "paper", "surface", "soft")
	shape("warn-border", "warn")
	shape("route", "surface")
	shape("route-dash", "surface")
	shape("accent", "accent-tint")
	shape("field", "surface", "soft", "paper")
	shape("ink", "track") // phần đầy của thanh mức dùng trên rãnh
	deco("warn", "surface", "paper")
	deco("line", "surface", "soft")
	deco("hair", "surface", "paper")
	deco("track", "surface")

	for _, th := range []struct {
		name string
		p    palette
	}{{"sáng", light}, {"tối", dark}} {
		fmt.Printf("== Chế độ %s (accent-tint = %s)\n", th.name, th.p["accent-tint"])
		bad := 0
		for _, q := range pairs {
			r := ratio(hex(th.p[q.fg]), hex(th.p[q.bg]))
			verdict := "trang trí"
			if q.min > 0 {
				verdict = "đạt"
				if r < q.min {
					verdict = "KHÔNG ĐẠT"
					bad++
				}
			}
			fmt.Printf("  %-13s %s trên %-12s %s  %5.2f  (cần %.1f)  %s\n", q.fg, th.p[q.fg], q.bg, th.p[q.bg], r, q.min, verdict)
		}
		fmt.Printf("  => %d cặp không đạt\n\n", bad)
	}
}
