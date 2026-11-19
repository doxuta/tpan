package checks

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

// maxEvidenceBytes là độ dài tối đa của Evidence.Output. Bằng chứng để người
// đọc xem trong giao diện và lưu vào sự cố; một trang log dài vài MB chỉ làm
// rối và làm to DB. Giữ phần CUỐI vì lệnh kiểm in kết luận và lỗi ở cuối.
const maxEvidenceBytes = 4096

// checkFunc cho phép viết một phép kiểm bằng một hàm thường mà vẫn thoả Check.
type checkFunc func(ctx context.Context, sys system.System) Result

func (f checkFunc) Run(ctx context.Context, sys system.System) Result { return f(ctx, sys) }

// RunAll chạy lần lượt từng phép kiểm và giữ nguyên thứ tự. Chạy tuần tự chứ
// không song song vì các phép kiểm dùng chung một máy và một khoá: song song
// chỉ thêm tải lên máy đang có chuyện mà không nhanh hơn bao nhiêu.
func RunAll(ctx context.Context, sys system.System, cs []Check) []Result {
	out := make([]Result, 0, len(cs))
	for i, c := range cs {
		if c == nil {
			// Không panic: kết quả "chưa kiểm được" vẫn cho người dùng thấy chỗ hổng.
			b := base{id: fmt.Sprintf("check.nil:%d", i), stage: StageTPAN,
				title: i18n.T("Phép kiểm bị thiếu", "Missing check")}
			out = append(out, b.unknown(i18n.T("phép kiểm thứ "+fmt.Sprint(i)+" không có", "check #"+fmt.Sprint(i)+" is nil")))
			continue
		}
		out = append(out, c.Run(ctx, sys))
	}
	return out
}

// Lazy dựng phép kiểm thật ở lúc Run, không phải lúc gọi Lazy. Lý do: Plan
// "cài stack" được dựng trước khi apt cài php-fpm nên chưa biết bản PHP; fn đọc
// nó từ nơi đã lưu (settings) sau khi bước dò bản đã chạy. fn được gọi lại ở
// mỗi lần Run, nên health định kỳ thấy bản PHP mới sau khi nâng cấp.
// fn trả nil (chưa dựng được) thì Result là Unknown.
func Lazy(fn func() Check) Check {
	return checkFunc(func(ctx context.Context, sys system.System) Result {
		var c Check
		if fn != nil {
			c = fn()
		}
		if c == nil {
			// ID cố định "lazy:unresolved": phép kiểm thật chưa có nên chưa biết ID của nó.
			b := base{id: "lazy:unresolved", stage: StageTPAN,
				title: i18n.T("Phép kiểm chưa dựng được", "Check could not be built")}
			return b.unknown(i18n.T(
				"chưa biết cần kiểm gì (thông tin cần có chưa được lưu)",
				"it is not known yet what to check (required information is not stored yet)"))
		}
		return c.Run(ctx, sys)
	})
}

// base giữ ba trường cố định của một phép kiểm để dựng Result cho gọn.
type base struct {
	id    string
	stage Stage
	title i18n.Text
}

func (b base) result(st Status, summary i18n.Text, ev ...Evidence) Result {
	return Result{ID: b.id, Stage: b.stage, Title: b.title, Status: st, Summary: summary, Evidence: ev}
}

func (b base) pass(summary i18n.Text, ev ...Evidence) Result { return b.result(Pass, summary, ev...) }

// bad dựng kết quả Warn hoặc Fail. manual là các lệnh để người vận hành tự gõ;
// dòng cuối PHẢI là lệnh kiểm lại (người gọi chịu trách nhiệm, test kiểm).
func (b base) bad(st Status, summary, cause i18n.Text, manual []string, ev ...Evidence) Result {
	r := b.result(st, summary, ev...)
	r.Cause = cause
	r.ManualFix = manual
	return r
}

// unknown: "chưa kiểm được" kèm lý do trong Summary.
func (b base) unknown(why i18n.Text, ev ...Evidence) Result {
	return b.result(Unknown, i18n.T("Chưa kiểm được: "+why.Vi, "Could not check: "+why.En), ev...)
}

// cantRun: lệnh không chạy được (không có chương trình, ctx bị huỷ...).
func (b base) cantRun(err error, ev ...Evidence) Result {
	return b.unknown(i18n.T("không chạy được lệnh ("+err.Error()+")", "could not run the command ("+err.Error()+")"), ev...)
}

// execCmd chạy một lệnh và gom bằng chứng. Mã thoát khác 0 là câu trả lời bình
// thường của rất nhiều lệnh kiểm (systemctl is-active trả 3, dpkg-query trả 1
// khi gói không có, curl trả 52 khi máy chủ đóng kết nối): nó KHÔNG phải lỗi.
// err khác nil chỉ khi lệnh không chạy được; khi đó phép kiểm phải trả Unknown.
func execCmd(ctx context.Context, sys system.System, cmd system.Cmd) (system.Result, Evidence, error) {
	res, err := sys.Run(ctx, cmd)
	ev := Evidence{Command: cmd.String(), ExitCode: res.ExitCode, Output: clip(joinOutput(res))}
	var ee *system.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
		res.ExitCode, ev.ExitCode = ee.Code, ee.Code
		err = nil
	default:
		ev.ExitCode = -1
		if ev.Output == "" {
			ev.Output = clip(err.Error())
		}
	}
	return res, ev, err
}

// joinOutput nối stdout rồi stderr. Nhiều lệnh kiểm in kết quả ra stderr
// (nginx -t, apachectl configtest, php-fpm -t, curl -S), nên bỏ stderr là mất bằng chứng.
func joinOutput(r system.Result) string {
	out := r.Stdout
	if r.Stderr != "" {
		if out != "" && !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		out += r.Stderr
	}
	return strings.TrimRight(out, "\r\n\t ")
}

// clip giữ tối đa maxEvidenceBytes byte CUỐI của s và ghi rõ đã cắt.
func clip(s string) string {
	if len(s) <= maxEvidenceBytes {
		return s
	}
	cut := len(s) - maxEvidenceBytes
	for cut < len(s) && !utf8.RuneStart(s[cut]) { // không cắt giữa một ký tự nhiều byte
		cut++
	}
	return fmt.Sprintf("[... đã cắt %d byte đầu / first %d bytes cut ...]\n%s", cut, cut, s[cut:])
}

// line dựng một dòng lệnh để đưa vào ManualFix. Dùng Cmd.String() để tham số
// lạ được đặt trong nháy: người vận hành dán vào shell không bị chèn lệnh.
func line(name string, args ...string) string {
	return system.Cmd{Name: name, Args: args}.String()
}

// firstLineWith trả dòng đầu tiên của out chứa một trong markers; không có thì
// dòng cuối không rỗng. Dùng để đưa dòng lỗi chính lên Summary.
func firstLineWith(out string, markers ...string) string {
	var last string
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		last = l
		for _, m := range markers {
			if strings.Contains(l, m) {
				return l
			}
		}
	}
	return last
}

// Tên dịch vụ và tên gói đi vào argv của systemctl, dpkg-query. Argv không qua
// shell nên không có chèn lệnh, nhưng một tên bắt đầu bằng "-" vẫn thành cờ.
var (
	unitNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9@._:+-]*$`)
	pkgNameRE  = regexp.MustCompile(`^[a-z0-9][a-z0-9+.*:-]*$`)
)
