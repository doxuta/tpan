package main

// M36 (v0.2.17): --password-stdin của `tpan db add` và `tpan db passwd`: tự
// đặt mật khẩu user DB. Mật khẩu đọc từ stdin, không bao giờ từ một cờ: cờ
// nằm trên dòng lệnh, ai trên máy cũng đọc được qua ps và nó vào lịch sử shell.

import (
	"errors"
	"io"
	"strings"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/i18n"
)

const passwordStdinHelp = "tự đặt mật khẩu user DB, đọc một dòng từ stdin (DB rời, site PHP trống; site WordPress không nhận). " +
	"Ví dụ: printf '%s\\n' \"$MK\" | tpan db passwd <tên> --password-stdin --yes"

// readChosenPassword đọc đúng một dòng từ stdin khi on. Đọc từng byte, không
// đệm: phần còn lại của stdin (câu trả lời có/không) để nguyên cho confirm.
// Dòng rỗng là lỗi: người dùng đã xin tự đặt mà không đưa mật khẩu nào.
func readChosenPassword(e *env, on bool) (string, error) {
	if !on {
		return "", nil
	}
	var b strings.Builder
	buf := make([]byte, 1)
	for b.Len() <= 256 {
		n, err := e.stdin.Read(buf)
		if n == 1 {
			if buf[0] == '\n' {
				break
			}
			b.WriteByte(buf[0])
			continue
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
	}
	pw := strings.TrimSuffix(b.String(), "\r")
	if pw == "" {
		return "", &core.RefusedError{Code: core.CodeDBPasswordInvalid, Message: i18n.T(
			"--password-stdin: stdin không có mật khẩu (dòng đầu rỗng). Luật: "+core.ChosenDBPasswordRule.Vi,
			"--password-stdin: no password on stdin (the first line is empty). Rule: "+core.ChosenDBPasswordRule.En)}
	}
	return pw, nil
}
