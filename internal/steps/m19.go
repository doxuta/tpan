package steps

// M19 (docs/thay-doi/M19-thiet-ke.md mục 3.5, S2 của "Trả lời soát lượt 1"):
// hai bước kiểm chứng tên phụ bằng một request HTTP thật qua 127.0.0.1, độc
// lập với việc đã ghi file vhost. Cả hai chỉ đọc: không ghi gì lên máy, Undo
// không có gì để làm. Hỏng thì engine hoàn tác bước vhost đứng trước.
//
// Thân trả về do PHP của site sinh (site kiểm soát được): chỉ dùng để so mã,
// Location và dấu hiệu WordPress; đọc tối đa nameProbeBodyMax byte, Location
// cắt nameProbeLocMax byte và bỏ ký tự điều khiển trước khi vào lỗi (mục 5.1).

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

var (
	_ engine.Step = (*NameProbe)(nil)
	_ engine.Step = (*NameGone)(nil)
	_ engine.Step = (*RedirectProbe)(nil)
)

const (
	nameProbeBodyMax = 64 << 10
	nameProbeLocMax  = 2 << 10
	// nameProbeTries: reload của web server xong thì worker cũ có thể còn trả
	// lời vài trăm mili giây (mục 2.1): thử lại tối đa 5 lần cách 1 giây.
	nameProbeTries = 5
	// Nhãn của -w: đứng sau thân, mỗi nhãn sau một ký tự xuống dòng. Giá trị
	// header không chứa được xuống dòng, nên tìm nhãn từ cuối lên là đúng dù
	// thân hay header do site đặt có chứa chuỗi giống nhãn.
	nameProbeLoc  = "\nTPAN_LOCATION="
	nameProbeXRB  = "\nTPAN_REDIRECT_BY="
	nameProbeCode = "\nTPAN_HTTP_CODE="
	// Mã thoát của curl khi web server đóng kết nối không trả gì (nginx 444).
	curlEmptyReply   = 52
	curlRecvFailure  = 56
	nameProbeWPMaker = "wordpress"
)

// nameProbeWPSigns như checks.siteWPSigns: dấu hiệu trang là WordPress.
var nameProbeWPSigns = []string{"wp-content", "wp-includes", `content="wordpress`}

// nameProbeSleep chờ giữa hai lần thử; test thay để khỏi chờ thật.
var nameProbeSleep = func(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// nameAnswer là câu trả lời đã tách của một lần curl.
type nameAnswer struct {
	code, location, redirectBy, body string
}

// splitNameAnswer tách stdout của nameCurl: <thân>\nTPAN_LOCATION=..\n
// TPAN_REDIRECT_BY=..\nTPAN_HTTP_CODE=<mã>. Thiếu nhãn thì mã rỗng.
func splitNameAnswer(out string) nameAnswer {
	var a nameAnswer
	cut := func(label string) (string, bool) {
		i := strings.LastIndex(out, label)
		if i < 0 {
			return "", false
		}
		v := out[i+len(label):]
		out = out[:i]
		return strings.TrimRight(v, "\r"), true
	}
	code, ok1 := cut(nameProbeCode)
	xrb, ok2 := cut(nameProbeXRB)
	loc, ok3 := cut(nameProbeLoc)
	if !ok1 || !ok2 || !ok3 {
		return nameAnswer{body: clipBytes(out, nameProbeBodyMax)}
	}
	a.code = code
	a.redirectBy = cleanHeader(xrb)
	a.location = cleanHeader(loc)
	a.body = clipBytes(out, nameProbeBodyMax)
	return a
}

func clipBytes(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// cleanHeader cắt nameProbeLocMax byte và bỏ ký tự điều khiển.
func cleanHeader(s string) string {
	s = clipBytes(strings.TrimSpace(s), nameProbeLocMax)
	return strings.Map(func(r rune) rune {
		if r < 0x20 || (r >= 0x7f && r < 0xa0) {
			return -1
		}
		return r
	}, s)
}

// nameCurl gọi <scheme>://<host>/ về 127.0.0.1, không theo chuyển hướng.
// -r 0-65535: xin tối đa 64 KiB thân (máy chủ có thể bỏ qua; vẫn cắt khi đọc).
// %header{} có từ curl 7.84.0 (Ubuntu 24.04 có 8.5.0).
func nameCurl(host string, https bool) system.Cmd {
	return probeCurl(host, "/", https)
}

// probeCurl như nameCurl nhưng gọi đường dẫn path (đã qua validate, bắt đầu
// bằng "/").
func probeCurl(host, path string, https bool) system.Cmd {
	scheme, port := "http", "80"
	args := []string{"-sS", "--max-time", "15"}
	if https {
		scheme, port = "https", "443"
		// Chứng chỉ có thể do máy ACME thử (pebble) cấp: kiểm tên ở đây, không kiểm CA.
		args = append(args, "-k")
	}
	args = append(args, "--resolve", host+":"+port+":127.0.0.1", "-r", "0-65535",
		"-w", `\nTPAN_LOCATION=%header{location}\nTPAN_REDIRECT_BY=%header{x-redirect-by}\nTPAN_HTTP_CODE=%{http_code}`,
		scheme+"://"+host+path)
	return system.Cmd{Name: "curl", Args: args}
}

// ================================================================== NameProbe

// NameProbe kiểm một tên của site trả lời đúng kiểu (mục 3.5, S2):
//
//   - Site SSL mà tên chưa có trong chứng chỉ (Covered): gọi cổng 80, mong 301
//     với Location đúng bằng https://<tên chính>/ (tên chưa phủ không bao giờ
//     nằm ở 443), bất kể Mode.
//   - Mode serve: 200 (hoặc 206 khi máy chủ nhận -r) kèm dấu hiệu WordPress,
//     KHÔNG đòi tên trong thân; hoặc 301/302 có X-Redirect-By: WordPress (chuyển
//     hướng canonical của WordPress, www ⇄ tên trần: request đã tới PHP của site).
//   - Mode redirect: 301, Location đúng bằng <scheme>://<tên chính>/, không có
//     X-Redirect-By (do vhost trả, không phải WordPress).
type NameProbe struct {
	Layout  site.Layout
	Host    validate.Domain
	Mode    site.NameMode
	SSL     bool
	Covered []validate.Domain

	passed bool
}

func (s *NameProbe) validate() error {
	if err := webCheckLayout(s.Layout); err != nil {
		return err
	}
	if s.Host.IsZero() {
		return errors.New("thiếu tên cần kiểm")
	}
	if s.Mode != site.ModeServe && s.Mode != site.ModeRedirect {
		return fmt.Errorf("kiểu %q của tên %s không phải serve hay redirect", s.Mode, s.Host)
	}
	return nil
}

// plan trả lệnh, và điều kiện đạt dưới dạng một hàm.
func (s *NameProbe) plan() (system.Cmd, func(nameAnswer) error) {
	host, primary := s.Host.String(), s.Layout.Domain.String()
	covered := s.SSL && (s.Host == s.Layout.Domain || slices.Contains(s.Covered, s.Host))
	redirect := func(to string) func(nameAnswer) error {
		return func(a nameAnswer) error {
			switch {
			case a.code != "301":
				return fmt.Errorf("mong 301 về %s, nhận mã %q", to, a.code)
			case a.location != to:
				return fmt.Errorf("mong 301 về %s, nhận Location %q", to, a.location)
			case a.redirectBy != "":
				return fmt.Errorf("mong 301 do vhost trả, nhận X-Redirect-By %q (trang của site, không phải khối chuyển tên phụ)", a.redirectBy)
			}
			return nil
		}
	}
	if s.SSL && !covered {
		return nameCurl(host, false), redirect("https://" + primary + "/")
	}
	scheme := "http"
	if s.SSL {
		scheme = "https"
	}
	cmd := nameCurl(host, s.SSL)
	if s.Mode == site.ModeRedirect {
		return cmd, redirect(scheme + "://" + primary + "/")
	}
	return cmd, func(a nameAnswer) error {
		switch a.code {
		case "200", "206":
			low := strings.ToLower(a.body)
			for _, sign := range nameProbeWPSigns {
				if strings.Contains(low, sign) {
					return nil
				}
			}
			return fmt.Errorf("trả %s nhưng trang không phải WordPress (vhost khác hay trang mặc định đang trả lời cho tên này)", a.code)
		case "301", "302":
			if strings.EqualFold(a.redirectBy, nameProbeWPMaker) {
				return nil
			}
			return fmt.Errorf("trả %s về %q mà không có X-Redirect-By: WordPress", a.code, a.location)
		}
		return fmt.Errorf("mong 200 với trang WordPress, nhận mã %q", a.code)
	}
}

func (s *NameProbe) Info() engine.StepInfo {
	host := s.Host.String()
	cmd, _ := s.plan()
	return engine.StepInfo{
		ID:    "name.probe:" + host,
		Title: i18n.T("Gọi thử "+host+" và kiểm cách nó trả lời", "Request "+host+" and check how it answers"),
		Why: i18n.T(
			"Ghi vhost và kiểm cú pháp chưa chứng minh tên "+host+" tới đúng site: một vhost khác có thể đang giữ tên này. Gọi thật qua 127.0.0.1 thì thấy ngay; hỏng thì vhost được trả về như cũ.",
			"Writing the vhost and passing the syntax test does not prove "+host+" reaches the right site: another vhost may hold the name. A real request through 127.0.0.1 shows it at once; on failure the vhost is put back."),
		Commands: []string{cmd.String()},
		NoUndo:   i18n.T("Bước này chỉ gọi HTTP, không đổi gì trên máy.", "This step only makes an HTTP request and changes nothing on the machine."),
	}
}

func (s *NameProbe) Check(context.Context, system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	return s.passed, nil
}

func (s *NameProbe) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	cmd, ok := s.plan()
	var last error
	for i := range nameProbeTries {
		if i > 0 {
			if err := nameProbeSleep(ctx, time.Second); err != nil {
				return err
			}
		}
		res, err := sys.Run(ctx, cmd)
		if err != nil {
			last = fmt.Errorf("gọi %s: %w", s.Host, err)
			continue
		}
		if last = ok(splitNameAnswer(res.Stdout)); last == nil {
			s.passed = true
			return nil
		}
		last = fmt.Errorf("tên %s: %w", s.Host, last)
	}
	return fmt.Errorf("%w; lệnh đã chạy: %s", last, cmd.String())
}

// Undo không làm gì (xem NoUndo trong Info).
func (s *NameProbe) Undo(context.Context, system.System) error { return nil }

// ================================================================== NameGone

// NameGone kiểm tên vừa gỡ khỏi site không còn được site phục vụ: request với
// Host đó rơi vào vhost mặc định của tpan (nginx đóng kết nối, mã 444; Apache
// trả 403). Gọi cổng 80: vhost mặc định cổng 443 từ chối bắt tay (nginx) nên
// không cho mã nào để so.
type NameGone struct {
	Layout site.Layout
	Host   validate.Domain

	passed bool
}

func (s *NameGone) validate() error {
	if err := webCheckLayout(s.Layout); err != nil {
		return err
	}
	if s.Host.IsZero() {
		return errors.New("thiếu tên cần kiểm")
	}
	return nil
}

func (s *NameGone) Info() engine.StepInfo {
	host := s.Host.String()
	return engine.StepInfo{
		ID:    "name.gone:" + host,
		Title: i18n.T("Kiểm "+host+" không còn vào site", "Check "+host+" no longer reaches the site"),
		Why: i18n.T(
			"Gỡ tên khỏi vhost mà một vhost khác vẫn khai tên đó thì lưu lượng đi sang chỗ lạ. Gọi thật thì thấy tên rơi vào site mặc định (bị từ chối) như mong muốn.",
			"If another vhost still declares the name after it leaves this vhost, traffic goes somewhere unexpected. A real request shows the name now falls into the default site (refused) as intended."),
		Commands: []string{nameCurl(host, false).String()},
		NoUndo:   i18n.T("Bước này chỉ gọi HTTP, không đổi gì trên máy.", "This step only makes an HTTP request and changes nothing on the machine."),
	}
}

func (s *NameGone) Check(context.Context, system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	return s.passed, nil
}

func (s *NameGone) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	cmd := nameCurl(s.Host.String(), false)
	var last error
	for i := range nameProbeTries {
		if i > 0 {
			if err := nameProbeSleep(ctx, time.Second); err != nil {
				return err
			}
		}
		res, err := sys.Run(ctx, cmd)
		var ee *system.ExitError
		if errors.As(err, &ee) && (ee.Code == curlEmptyReply || ee.Code == curlRecvFailure) {
			s.passed = true
			return nil
		}
		if err != nil {
			last = fmt.Errorf("gọi %s: %w", s.Host, err)
			continue
		}
		a := splitNameAnswer(res.Stdout)
		if a.code == "403" {
			s.passed = true
			return nil
		}
		last = fmt.Errorf("tên %s vẫn được trả lời (mã %q, Location %q) sau khi gỡ khỏi vhost", s.Host, a.code, a.location)
	}
	return fmt.Errorf("%w; lệnh đã chạy: %s", last, cmd.String())
}

// Undo không làm gì (xem NoUndo trong Info).
func (s *NameGone) Undo(context.Context, system.System) error { return nil }

// ============================================================== RedirectProbe

// RedirectProbe kiểm một luật chuyển hướng vừa vẽ vào vhost (mục 3.5,
// site.redirect.add): gọi <scheme>://<tên chính><Source> về 127.0.0.1 (https
// khi site có SSL), mong đúng mã Code, Location đúng bằng Location (người gọi
// tính: đích dạng đường dẫn thành <scheme>://<tên chính><đích>, S16), và không
// có X-Redirect-By (do vhost trả, không phải WordPress hay plugin).
type RedirectProbe struct {
	Layout   site.Layout
	SSL      bool
	Source   validate.RedirectSource
	Code     int
	Location string

	passed bool
}

func (s *RedirectProbe) validate() error {
	if err := webCheckLayout(s.Layout); err != nil {
		return err
	}
	if s.Source.IsZero() {
		return errors.New("thiếu đường dẫn nguồn của luật cần kiểm")
	}
	if s.Code != 301 && s.Code != 302 {
		return fmt.Errorf("mã chuyển hướng %d không phải 301 hay 302", s.Code)
	}
	if s.Location == "" {
		return errors.New("thiếu Location mong đợi")
	}
	return nil
}

func (s *RedirectProbe) cmd() system.Cmd {
	return probeCurl(s.Layout.Domain.String(), s.Source.String(), s.SSL)
}

func (s *RedirectProbe) Info() engine.StepInfo {
	src := s.Source.String()
	return engine.StepInfo{
		ID:    "redirect.probe:" + src,
		Title: i18n.T("Gọi thử "+src+" và kiểm nó chuyển về "+s.Location, "Request "+src+" and check it redirects to "+s.Location),
		Why: i18n.T(
			"Ghi vhost và kiểm cú pháp chưa chứng minh luật có hiệu lực: một location khác hay luật trong .htaccess có thể trả lời trước. Gọi thật qua 127.0.0.1 thì thấy ngay; hỏng thì vhost được trả về như cũ.",
			"Writing the vhost and passing the syntax test does not prove the rule takes effect: another location or an .htaccess rule may answer first. A real request through 127.0.0.1 shows it at once; on failure the vhost is put back."),
		Commands: []string{s.cmd().String()},
		NoUndo:   i18n.T("Bước này chỉ gọi HTTP, không đổi gì trên máy.", "This step only makes an HTTP request and changes nothing on the machine."),
	}
}

func (s *RedirectProbe) Check(context.Context, system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	return s.passed, nil
}

func (s *RedirectProbe) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	cmd, code := s.cmd(), fmt.Sprint(s.Code)
	var last error
	for i := range nameProbeTries {
		if i > 0 {
			if err := nameProbeSleep(ctx, time.Second); err != nil {
				return err
			}
		}
		res, err := sys.Run(ctx, cmd)
		if err != nil {
			last = fmt.Errorf("gọi %s: %w", s.Source, err)
			continue
		}
		a := splitNameAnswer(res.Stdout)
		switch {
		case a.code != code:
			last = fmt.Errorf("mong %s về %s, nhận mã %q", code, s.Location, a.code)
		case a.location != s.Location:
			last = fmt.Errorf("mong %s về %s, nhận Location %q", code, s.Location, a.location)
		case a.redirectBy != "":
			last = fmt.Errorf("mong chuyển hướng do vhost trả, nhận X-Redirect-By %q (trang của site trả lời trước luật)", a.redirectBy)
		default:
			s.passed = true
			return nil
		}
		last = fmt.Errorf("luật %s: %w", s.Source, last)
	}
	return fmt.Errorf("%w; lệnh đã chạy: %s", last, cmd.String())
}

// Undo không làm gì (xem NoUndo trong Info).
func (s *RedirectProbe) Undo(context.Context, system.System) error { return nil }
