// Chụp ảnh giao diện cho README: docs/anh/*.png.
//
// Chạy từ gốc repo (cần Chrome trên Mac, đã build web/dist bằng `make web` (pnpm)
// trong web/ vì file chạy nhúng web/dist):
//
//	go run ./scripts/anh-readme
//	go run ./scripts/anh-readme -bo m15 -out docs/bang-chung   (ảnh khối "Phiên bản và cập nhật" của M15)
//	TPAN_PANEL_PASSWORD=... go run ./scripts/anh-readme -bo m15-that -panel https://<máy ảo>:8443 -out docs/bang-chung
//	    (bấm thật nút cập nhật trên panel của máy ảo test, xem m15_that.go)
//
// Chương trình tự làm hết, rồi tự tắt cả hai tiến trình khi xong:
//  1. build tpan ra thư mục tạm rồi chạy `tpan serve --demo` (máy giả, tên máy
//     "demo", site example.com). Build ra file riêng thay vì `go run` để tắt
//     đúng tiến trình tpan chứ không chỉ tiến trình `go run` bọc ngoài.
//  2. đọc mật khẩu demo từ stdout (mỗi lần chạy một mật khẩu mới).
//  3. mở Chrome không giao diện, nối Chrome DevTools Protocol qua websocket.
//  4. đăng nhập bằng fetch cùng origin, rồi với mỗi màn: đặt sáng/tối, đi tới
//     đường dẫn, chờ dữ liệu tải xong, chụp.
//
// Vì sao chụp bằng chương trình mà không chụp tay: ảnh phải chụp lại được mỗi
// lần giao diện đổi, và bằng đúng dữ liệu demo (không lọt thông tin máy thật).
//
// Thư mục scripts/ được phép dùng os/exec (xem internal/system/noshell_test.go),
// vì đây là đồ nghề chạy trên máy dev, không nằm trong file chạy của tpan.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image/png"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"golang.org/x/net/websocket"
)

const (
	width, height = 1440, 900
	maxBytes      = 600 * 1024
)

// shot là một ảnh: mở path (có thể kèm bước điền form), chờ tới khi ready đúng.
type shot struct {
	file  string // tên file trong thư mục ra
	path  string // đường dẫn trong panel; "{job}" được thay bằng id job tạo site
	theme string // "light" hoặc "dark"
	// ready là biểu thức JS trả true khi màn đã có dữ liệu thật. Chữ "Đang
	// tải…" cũng phải biến mất (kiểm chung cho mọi màn).
	ready string
	// before chạy sau khi trang tải xong, trước khi chờ ready (điền form...).
	before func(c *cdp) error
	// after là JS chạy sau khi ready đúng, ngay trước lúc chụp (cuộn trang...).
	after string
}

func main() {
	listen := flag.String("listen", "127.0.0.1:18141", "địa chỉ cho tpan serve --demo")
	chrome := flag.String("chrome", "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "đường dẫn Chrome")
	debugPort := flag.Int("debug-port", 18142, "cổng DevTools của Chrome")
	out := flag.String("out", "docs/anh", "thư mục ghi ảnh")
	set := flag.String("bo", "readme", "bộ ảnh: readme (ảnh của README), m15 (khối Phiên bản và cập nhật ở demo), m15-that (bấm thật trên panel ở -panel), m16-do (đo từng màn, xem m16.go), m16, m16-that, m18 (màn của M18 trên demo, xem m18_anh.go) hoặc m18-that (bấm thật trên panel ở -panel, xem m18_that.go)")
	panel := flag.String("panel", "", "địa chỉ một panel đang chạy (máy ảo test), thay cho serve --demo; mật khẩu admin lấy từ biến môi trường TPAN_PANEL_PASSWORD")
	var m16 m16Opts
	flag.BoolVar(&m16.longNames, "ten-dai", false, "bộ m16-do trên demo: tạo ba site tên dài qua API nếu chưa có")
	flag.StringVar(&m16.site, "site", "", "bộ m16-do: site dùng cho màn \"trang một site\" (mặc định: site tên dài nhất của máy)")
	flag.StringVar(&m16.passwordFile, "mat-khau-tep", "", "với -panel: file chứa output của `tpan setup` hoặc `tpan admin passwd`; mật khẩu admin được đọc từ đó")
	flag.BoolVar(&m16.threshold, "nguong", false, "bộ m16-do: kiểm các ngưỡng cứng của M16, sai một cái là thoát mã khác 0")
	flag.StringVar(&m16.step, "buoc", "", "bộ m16-that: admin, tao-site, user hoặc nang-cap (xem m16_that.go); bộ m18-that: dich hoặc khoi-phuc (xem m18_that.go)")
	// Bộ m18-that, bước dich: phần KHÔNG bí mật của form đích. Hai khoá đi qua
	// biến môi trường TPAN_S3_ACCESS, TPAN_S3_SECRET, không qua dòng lệnh.
	flag.StringVar(&m16.s3Endpoint, "s3-endpoint", "", "bộ m18-that, bước dich: endpoint gõ vào form đích (máy chủ tpan gọi tới, ví dụ http://127.0.0.1:7070)")
	flag.StringVar(&m16.s3Region, "s3-region", "", "bộ m18-that, bước dich: vùng")
	flag.StringVar(&m16.s3Bucket, "s3-bucket", "", "bộ m18-that, bước dich: bucket")
	flag.StringVar(&m16.s3Prefix, "s3-prefix", "", "bộ m18-that, bước dich: tiền tố (rỗng: giữ giá trị form điền sẵn)")
	flag.StringVar(&m16.userName, "ten", "", "bộ m16-that, bước user: tên user thường (mật khẩu ở biến môi trường TPAN_USER_PASSWORD)")
	flag.StringVar(&m16.waitFile, "cho-tep", "", "bộ m16-that, bước nang-cap: file mà script e2e tạo ra khi đã nâng cấp xong")
	flag.StringVar(&m16.only, "chi", "", "bộ m16: chỉ chụp các ảnh có tên này (cách nhau bằng dấu phẩy, ví dụ cong-cu,nguoi-dung), bỏ phần bàn phím và tiếng Anh")
	flag.Parse()
	list := shots()
	switch *set {
	case "readme":
	case "m15":
		list = shotsM15()
	case "m15-that":
		list = nil // run gọi liveM15 thay cho vòng chụp
		if *panel == "" {
			fmt.Fprintln(os.Stderr, "lỗi: -bo m15-that cần -panel (panel của máy ảo test; demo không cập nhật được)")
			os.Exit(2)
		}
	case "m16-do", "m16", "mau-v2", "m18":
		list = nil
	case "m16-that", "m18-that":
		list = nil
		if *panel == "" {
			fmt.Fprintf(os.Stderr, "lỗi: -bo %s cần -panel (panel của máy ảo test)\n", *set)
			os.Exit(2)
		}
	default:
		fmt.Fprintln(os.Stderr, "lỗi: -bo chỉ nhận readme, m15, m15-that, m16-do, m16, m16-that, m18, m18-that hoặc mau-v2")
		os.Exit(2)
	}
	if err := run(*listen, *chrome, *debugPort, *out, *set, list, *panel, m16); err != nil {
		fmt.Fprintln(os.Stderr, "lỗi:", err)
		os.Exit(1)
	}
}

func run(listen, chrome string, debugPort int, out, set string, list []shot, panel string, m16 m16Opts) error {
	tmp, err := os.MkdirTemp("", "tpan-anh-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Panel có sẵn (-panel: máy ảo test) thì không dựng demo; mật khẩu đi qua
	// biến môi trường để không nằm trong dòng lệnh hay lịch sử shell.
	base, password := strings.TrimRight(panel, "/"), os.Getenv("TPAN_PANEL_PASSWORD")
	if panel != "" && password == "" && m16.passwordFile != "" {
		// Mật khẩu `tpan setup` hay `tpan admin passwd` in ra (26 ký tự base32),
		// đọc từ file output của chính lệnh đó: không đi qua dòng lệnh hay màn hình.
		b, err := os.ReadFile(m16.passwordFile)
		if err != nil {
			return fmt.Errorf("đọc -mat-khau-tep: %w", err)
		}
		password = regexp.MustCompile(`\b[A-Z2-7]{26}\b`).FindString(string(b))
	}
	if panel != "" && password == "" {
		return errors.New("-panel cần mật khẩu admin của panel đó: biến môi trường TPAN_PANEL_PASSWORD, hoặc -mat-khau-tep <file có output của tpan setup>")
	}
	needPanel := set != "mau-v2" // trang mẫu tĩnh mở thẳng từ đĩa, không cần panel
	if panel == "" && needPanel {
		// 1. build và chạy serve --demo.
		bin := filepath.Join(tmp, "tpan")
		if b, err := exec.CommandContext(ctx, "go", "build", "-o", bin, "./cmd/tpan").CombinedOutput(); err != nil {
			return fmt.Errorf("build tpan: %w\n%s", err, b)
		}
		srv := exec.Command(bin, "serve", "--demo", "--listen", listen)
		srv.Stderr = os.Stderr
		srvOut, err := srv.StdoutPipe()
		if err != nil {
			return err
		}
		srv.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := srv.Start(); err != nil {
			return fmt.Errorf("chạy tpan serve --demo: %w", err)
		}
		defer stop(srv)

		// 2. mật khẩu demo: dòng đầu có chữ "demo"/"Demo" rồi dấu hai chấm, từ cuối là mật khẩu.
		pwCh := make(chan string, 1)
		go func() {
			sc := bufio.NewScanner(srvOut)
			sent := false
			for sc.Scan() { // đọc tới hết để tiến trình không nghẽn ống
				f := strings.Fields(sc.Text())
				if !sent && len(f) > 1 && strings.Contains(strings.ToLower(sc.Text()), "demo") && strings.Contains(sc.Text(), ":") {
					pwCh <- f[len(f)-1]
					sent = true
				} else if sent {
					noteDemoUser(sc.Text()) // "User demo (vai user): <tên> / <mật khẩu>", cho bộ m16
				}
			}
		}()
		select {
		case password = <-pwCh:
		case <-time.After(30 * time.Second):
			return errors.New("không đọc được mật khẩu demo từ stdout của serve")
		}
		base = "http://" + listen
		if err := waitHTTP(ctx, base+"/api/v1/session"); err != nil {
			return fmt.Errorf("serve chưa nghe ở %s: %w", base, err)
		}
	}

	// 3. Chrome không giao diện. --remote-allow-origins: Chrome từ bản 111 từ
	// chối websocket có header Origin lạ; ta gửi đúng origin này.
	devBase := fmt.Sprintf("http://127.0.0.1:%d", debugPort)
	ch := exec.Command(chrome, "--headless=new",
		fmt.Sprintf("--remote-debugging-port=%d", debugPort),
		"--remote-allow-origins="+devBase,
		"--user-data-dir="+filepath.Join(tmp, "chrome"),
		fmt.Sprintf("--window-size=%d,%d", width, height),
		"--hide-scrollbars", "--no-first-run", "--no-default-browser-check")
	if panel != "" {
		// Panel thật dùng chứng chỉ tự ký (tpan setup); Chrome này chỉ sống trong lần chạy và chỉ mở đúng panel đó.
		ch.Args = append(ch.Args, "--ignore-certificate-errors")
	}
	ch.Args = append(ch.Args, "about:blank")
	ch.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := ch.Start(); err != nil {
		return fmt.Errorf("mở Chrome: %w", err)
	}
	defer stop(ch)
	wsURL, err := pageSocket(ctx, devBase)
	if err != nil {
		return err
	}
	c, err := dial(wsURL, devBase)
	if err != nil {
		return err
	}
	defer func() { _ = c.ws.Close() }()

	if _, err := c.call("Emulation.setDeviceMetricsOverride", map[string]any{
		"width": width, "height": height, "deviceScaleFactor": 1, "mobile": false}); err != nil {
		return err
	}

	if set == "mau-v2" {
		return samplesV2(c, out)
	}
	if set == "m16-that" {
		// Bộ này tự đăng nhập qua form của giao diện, như người dùng.
		return liveM16(c, base, password, m16)
	}
	if set == "m18-that" {
		return liveM18(c, base, password, m16)
	}

	// 4. đăng nhập bằng fetch cùng origin (cookie HttpOnly nằm trong Chrome).
	if err := c.goTo(base+"/", "/dang-nhap"); err != nil {
		return err
	}
	loginJS := fmt.Sprintf(`fetch('/api/v1/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({password:%q})}).then(r=>r.status)`, password)
	st, err := c.eval(loginJS)
	if err != nil || fmt.Sprint(st) != "204" {
		return fmt.Errorf("đăng nhập demo: trạng thái %v, lỗi %v", st, err)
	}

	switch set {
	case "m15-that":
		return liveM15(c, base, out)
	case "m16-do":
		return measureM16(c, base, m16)
	case "m16":
		return photosM16(c, base, password, out, m16.only)
	case "m18":
		return photosM18(c, base, out, m16.only)
	}
	jobID, err := siteJob(c)
	if err != nil {
		return err
	}

	for _, s := range list {
		if err := capture(c, base, jobID, out, s); err != nil {
			return fmt.Errorf("%s: %w", s.file, err)
		}
	}
	return nil
}

func shots() []shot {
	return []shot{
		{file: "bang-dieu-khien.png", path: "/", theme: "light", ready: `!!document.querySelector('main h1, main h2')`},
		{file: "site-su-co.png", path: "/sites/wiki.example.org", theme: "light", ready: `document.body.innerText.includes('wiki.example.org')`},
		{file: "tao-site-xem-truoc.png", path: "/sites/new", theme: "light",
			before: func(c *cdp) error {
				if err := c.typeInto("#o-domain", "new.example.com"); err != nil {
					return err
				}
				if err := c.typeInto("#o-email", "admin@example.com"); err != nil {
					return err
				}
				_, err := c.eval(`document.querySelector('form button[type=submit]').click()`)
				return err
			},
			ready: `/Chạy \d+ bước/.test(document.body.innerText)`,
			// Màn tự cuộn tới bản xem trước; cuộn về đầu để ảnh có cả form đã điền.
			after: `window.scrollTo(0,0)`},
		{file: "job.png", path: "/jobs/{job}", theme: "light", ready: `document.body.innerText.includes('example.com')`},
		{file: "su-co.png", path: "/incidents", theme: "light", ready: `document.body.innerText.includes('example')`},
		{file: "stack.png", path: "/stack", theme: "light", ready: `!!document.querySelector('main h1, main h2')`},
		{file: "bang-dieu-khien-toi.png", path: "/", theme: "dark", ready: `!!document.querySelector('main h1, main h2')`},
	}
}

// shotsM15 là ảnh của khối "Phiên bản và cập nhật" (mục 6.14 của KE-HOACH.md)
// trên máy demo: nguồn tải giả có bản v9.9.9-demo, máy có tpan.old. Bấm bằng
// chữ trên nút thay vì id, để ảnh hỏng ngay khi chữ trên nút đổi mà quên chụp lại.
func shotsM15() []shot {
	click := func(label string) func(c *cdp) error {
		return func(c *cdp) error {
			js := fmt.Sprintf(`[...document.querySelectorAll('#phien-ban button')].some(b=>b.innerText.trim().startsWith(%q))`, label)
			if err := c.waitTrue(js, 20*time.Second); err != nil {
				return fmt.Errorf("chờ nút %q: %w", label, err)
			}
			_, err := c.eval(fmt.Sprintf(`[...document.querySelectorAll('#phien-ban button')].find(b=>b.innerText.trim().startsWith(%q)).click()`, label))
			return err
		}
	}
	const (
		toBlock = `document.getElementById('phien-ban').scrollIntoView({block:'start'})`
		preview = `document.getElementById('phien-ban').innerText.includes('SHA256 sẽ kiểm')`
		dialog  = `!!document.querySelector('[role=dialog],[role=alertdialog]')`
	)
	return []shot{
		{file: "M15-phien-ban-sang.png", path: "/settings", theme: "light", before: click("Kiểm bản mới"), ready: preview, after: toBlock},
		{file: "M15-phien-ban-toi.png", path: "/settings", theme: "dark", before: click("Kiểm bản mới"), ready: preview, after: toBlock},
		// Không bấm "Kiểm bản mới" lần nữa: máy chủ còn nhớ lần kiểm của ảnh
		// trước nên nút "Cập nhật lên" có sẵn; bấm kiểm lại thì nút bị khoá
		// trong lúc chờ và cú bấm kế tiếp rơi vào nút đang khoá.
		{file: "M15-xac-nhan-cap-nhat-sang.png", path: "/settings", theme: "light",
			before: click("Cập nhật lên"), ready: dialog},
		{file: "M15-xac-nhan-quay-ve-toi.png", path: "/settings", theme: "dark",
			before: click("Quay về bản trước"), ready: dialog},
		{file: "M15-man-cap-nhat-sang.png", path: "/updates", theme: "light", before: click("Kiểm bản mới"), ready: preview, after: toBlock},
	}
}

func capture(c *cdp, base, jobID, out string, s shot) error {
	// Đặt sáng/tối và tiếng Việt trước khi tải: theme.js đọc localStorage lúc đầu trang.
	if _, err := c.eval(fmt.Sprintf(`localStorage.setItem('tpan-theme',%q);localStorage.setItem('tpan-lang','vi')`, s.theme)); err != nil {
		return err
	}
	path := strings.ReplaceAll(s.path, "{job}", jobID)
	if err := c.goTo(base+path, path); err != nil {
		return err
	}
	if s.before != nil {
		if err := s.before(c); err != nil {
			return err
		}
	}
	// Chờ tới khi: không còn chữ "Đang tải…" và ready đúng.
	js := fmt.Sprintf(`!document.body.innerText.includes('Đang tải') && !document.body.innerText.includes('Đang dựng') && (%s)`, s.ready)
	if err := c.waitTrue(js, 20*time.Second); err != nil {
		return fmt.Errorf("chờ dữ liệu: %w", err)
	}
	if s.after != "" {
		if _, err := c.eval(s.after); err != nil {
			return err
		}
	}
	// Chờ font và hiệu ứng chuyển cảnh xong rồi mới chụp.
	if _, err := c.eval(`document.fonts.ready.then(()=>true)`); err != nil {
		return err
	}
	time.Sleep(700 * time.Millisecond)

	return snap(c, out, s.file)
}

// snap chụp trang đang mở vào out/file (PNG, nén lại nếu quá cỡ).
func snap(c *cdp, out, file string) error {
	res, err := c.call("Page.captureScreenshot", map[string]any{"format": "png"})
	if err != nil {
		return err
	}
	var r struct{ Data string }
	if err := json.Unmarshal(res, &r); err != nil {
		return err
	}
	raw, err := base64.StdEncoding.DecodeString(r.Data)
	if err != nil {
		return err
	}
	if len(raw) > maxBytes {
		// Nén lại bằng bộ mã hoá PNG của Go ở mức cao nhất (vẫn là PNG, cùng kích thước).
		if img, err := png.Decode(bytes.NewReader(raw)); err == nil {
			var b bytes.Buffer
			if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&b, img); err == nil && b.Len() < len(raw) {
				raw = b.Bytes()
			}
		}
	}
	dst := filepath.Join(out, file)
	if err := os.WriteFile(dst, raw, 0o644); err != nil {
		return err
	}
	fmt.Printf("%s  %d KB\n", dst, (len(raw)+1023)/1024)
	if len(raw) > maxBytes {
		fmt.Printf("  CHÚ Ý: %s vượt 600 KB\n", file)
	}
	return nil
}

// siteJob lấy id job tạo site mới nhất đã thành công trong dữ liệu demo.
func siteJob(c *cdp) (string, error) {
	v, err := c.eval(`fetch('/api/v1/jobs?limit=100').then(r=>r.json()).then(js=>{
		const j = js.find(j=>j.status==='succeeded' && /site/.test(j.kind));
		return j ? j.id : '';
	})`)
	if err != nil {
		return "", err
	}
	id, _ := v.(string)
	if id == "" {
		return "", errors.New("demo không có job tạo site đã thành công")
	}
	return id, nil
}

// ---- Chrome DevTools Protocol ----

// cdp gọi tuần tự (gửi rồi đọc tới khi có trả lời cùng id): đủ cho việc chụp
// ảnh, không cần luồng đọc riêng. Sự kiện Chrome gửi xen giữa (lỗi console,
// ngoại lệ JS, dòng log của trình duyệt) được giữ lại ở events để bộ M16 kiểm
// "mở màn không lỗi console"; các bộ ảnh cũ không đọc tới nó.
type cdp struct {
	ws     *websocket.Conn
	next   int
	events []cdpEvent
}

// cdpEvent là một sự kiện của Chrome DevTools Protocol (không phải trả lời của một lệnh).
type cdpEvent struct {
	Method string
	Params json.RawMessage
}

func dial(url, origin string) (*cdp, error) {
	ws, err := websocket.Dial(url, "", origin)
	if err != nil {
		return nil, fmt.Errorf("nối DevTools %s: %w", url, err)
	}
	ws.MaxPayloadBytes = 64 << 20 // ảnh chụp trả về dạng base64 trong một khung
	return &cdp{ws: ws}, nil
}

func (c *cdp) call(method string, params any) (json.RawMessage, error) {
	c.next++
	id := c.next
	if err := websocket.JSON.Send(c.ws, map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	for {
		var m struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := websocket.JSON.Receive(c.ws, &m); err != nil {
			return nil, fmt.Errorf("%s: %w", method, err)
		}
		if m.ID != id {
			if m.Method != "" {
				c.events = append(c.events, cdpEvent{Method: m.Method, Params: m.Params})
			}
			continue
		}
		if m.Error != nil {
			return nil, fmt.Errorf("%s: %s", method, m.Error.Message)
		}
		return m.Result, nil
	}
}

// eval chạy JS trong trang (await nếu là Promise) và trả giá trị.
func (c *cdp) eval(js string) (any, error) {
	res, err := c.call("Runtime.evaluate", map[string]any{"expression": js, "awaitPromise": true, "returnByValue": true})
	if err != nil {
		return nil, err
	}
	var r struct {
		Result struct {
			Value any `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text      string `json:"text"`
			Exception struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return nil, err
	}
	if r.ExceptionDetails != nil {
		return nil, fmt.Errorf("JS lỗi: %s %s", r.ExceptionDetails.Text, r.ExceptionDetails.Exception.Description)
	}
	return r.Result.Value, nil
}

// waitTrue hỏi lại mỗi 200ms; lỗi lúc trang đang đổi (mất context) thì thử lại.
func (c *cdp) waitTrue(js string, timeout time.Duration) error {
	end := time.Now().Add(timeout)
	var last error
	for time.Now().Before(end) {
		v, err := c.eval(js)
		if err == nil && v == true {
			return nil
		}
		last = err
		time.Sleep(200 * time.Millisecond)
	}
	if last != nil {
		return fmt.Errorf("hết giờ, lỗi cuối: %w", last)
	}
	return errors.New("hết giờ, điều kiện chưa đúng")
}

// goTo tải đầy đủ một URL và chờ tới khi trang đó đã load xong. wantPath là
// đường dẫn sau khi tải (chưa đăng nhập thì panel chuyển về /dang-nhap).
func (c *cdp) goTo(url, wantPath string) error {
	if _, err := c.call("Page.navigate", map[string]any{"url": url}); err != nil {
		return err
	}
	// So cả đường dẫn: ngay sau navigate, trang cũ vẫn có thể báo readyState complete.
	if err := c.waitTrue(fmt.Sprintf(`document.readyState==='complete' && location.pathname===%q`, wantPath), 15*time.Second); err != nil {
		now, _ := c.eval(`location.href`)
		return fmt.Errorf("mở %s (đang ở %v): %w", url, now, err)
	}
	return nil
}

// typeInto đưa con trỏ vào ô rồi gõ bằng Input.insertText: React thấy như người gõ thật.
func (c *cdp) typeInto(selector, text string) error {
	if err := c.waitTrue(fmt.Sprintf(`!!document.querySelector(%q)`, selector), 10*time.Second); err != nil {
		return fmt.Errorf("không thấy ô %s: %w", selector, err)
	}
	if _, err := c.eval(fmt.Sprintf(`document.querySelector(%q).focus()`, selector)); err != nil {
		return err
	}
	_, err := c.call("Input.insertText", map[string]any{"text": text})
	return err
}

// ---- tiến trình và HTTP ----

func waitHTTP(ctx context.Context, url string) error {
	var last error
	for i := 0; i < 100; i++ {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			return nil
		}
		last = err
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return last
}

// pageSocket lấy địa chỉ websocket của tab trang đầu tiên của Chrome.
func pageSocket(ctx context.Context, devBase string) (string, error) {
	for i := 0; i < 100; i++ {
		if resp, err := http.Get(devBase + "/json/list"); err == nil {
			var targets []struct {
				Type string `json:"type"`
				WS   string `json:"webSocketDebuggerUrl"`
			}
			err := json.NewDecoder(resp.Body).Decode(&targets)
			_ = resp.Body.Close()
			if err == nil {
				for _, t := range targets {
					if t.Type == "page" && t.WS != "" {
						return t.WS, nil
					}
				}
			}
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return "", errors.New("trình duyệt Chrome không mở tab nào trong 20 giây")
}

// stop tắt cả nhóm tiến trình (Chrome đẻ nhiều tiến trình con).
func stop(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
