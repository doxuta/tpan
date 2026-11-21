// Bản thử của M18, đợt 0: mã hoá bản sao lưu panel bằng thư viện filippo.io/age
// (khoá X25519: máy chủ chỉ giữ khoá công khai, người quản trị giữ khoá bí mật).
// Mục đích: (1) đo RAM khi mã hoá theo dòng một file lớn, (2) chứng minh file
// do thư viện ghi giải mã được bằng lệnh `age` có sẵn trong apt của Ubuntu
// 24.04, tức là không cần tpan để mở bản sao lưu. KHÔNG phải mã của sản phẩm.
//
//	agethu keygen                       in khoá bí mật (dòng 1) và khoá công khai (dòng 2)
//	agethu enc <recipient> <in> <out>   mã hoá theo dòng
//	agethu dec <identity-file> <in> <out>
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"filippo.io/age"
)

func peakRSS() string {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return "?"
	}
	for _, ln := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(ln, "VmHWM:") {
			return strings.Join(strings.Fields(ln)[1:], " ")
		}
	}
	return "?"
}

func run(args []string) error {
	defer func() { fmt.Fprintf(os.Stderr, "(đỉnh RAM VmHWM %s)\n", peakRSS()) }()
	switch {
	case len(args) == 1 && args[0] == "keygen":
		id, err := age.GenerateX25519Identity()
		if err != nil {
			return err
		}
		fmt.Println(id.String())
		fmt.Println(id.Recipient().String())
		return nil
	case len(args) == 4 && args[0] == "enc":
		rcpt, err := age.ParseX25519Recipient(args[1])
		if err != nil {
			return err
		}
		in, err := os.Open(args[2])
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(args[3], os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		bw := bufio.NewWriterSize(out, 1<<20)
		w, err := age.Encrypt(bw, rcpt)
		if err != nil {
			return err
		}
		if _, err := io.Copy(w, in); err != nil {
			return err
		}
		if err := w.Close(); err != nil {
			return err
		}
		if err := bw.Flush(); err != nil {
			return err
		}
		return out.Close()
	case len(args) == 4 && (args[0] == "encp" || args[0] == "decp"):
		// Chế độ khoá đối xứng (mật khẩu scrypt, hệ số công 15): args[1] là file
		// chứa khoá trên một dòng.
		kb, err := os.ReadFile(args[1])
		if err != nil {
			return err
		}
		key := strings.TrimSpace(string(kb))
		in, err := os.Open(args[2])
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(args[3], os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		if args[0] == "encp" {
			rcpt, err := age.NewScryptRecipient(key)
			if err != nil {
				return err
			}
			rcpt.SetWorkFactor(15)
			w, err := age.Encrypt(out, rcpt)
			if err != nil {
				return err
			}
			if _, err := io.Copy(w, in); err != nil {
				return err
			}
			if err := w.Close(); err != nil {
				return err
			}
			return out.Close()
		}
		id, err := age.NewScryptIdentity(key)
		if err != nil {
			return err
		}
		id.SetMaxWorkFactor(15)
		r, err := age.Decrypt(bufio.NewReaderSize(in, 1<<20), id)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, r); err != nil {
			return err
		}
		return out.Close()
	case len(args) == 4 && args[0] == "dec":
		kf, err := os.Open(args[1])
		if err != nil {
			return err
		}
		ids, err := age.ParseIdentities(kf)
		kf.Close()
		if err != nil {
			return err
		}
		in, err := os.Open(args[2])
		if err != nil {
			return err
		}
		defer in.Close()
		r, err := age.Decrypt(bufio.NewReaderSize(in, 1<<20), ids...)
		if err != nil {
			return err
		}
		out, err := os.OpenFile(args[3], os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, r); err != nil {
			return err
		}
		return out.Close()
	}
	return fmt.Errorf("cách dùng: agethu keygen | enc <recipient> <in> <out> | dec <identity-file> <in> <out>")
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "LỖI:", err)
		os.Exit(1)
	}
}
