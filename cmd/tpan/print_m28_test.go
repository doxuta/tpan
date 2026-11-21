package main

import (
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/engine"
)

// M28: đích symlink và nội dung do site viết không đưa chuỗi escape tới
// terminal của người quản trị; bản xem trước nói đúng "thay symlink".
func TestInBanXemTruocKhongChuoiEscape(t *testing.T) {
	e := &env{lang: "vi"}
	evil := "/etc/shadow\x1b]0;bi-chiem\x07\n\x9b31m"
	got := fileAction(e, engine.FileChange{Path: "/var/www/a/public/.htaccess", OldExists: true, OldLink: evil})
	if strings.ContainsAny(got, "\x1b\x07\n\u009b") {
		t.Errorf("fileAction để lọt ký tự điều khiển: %q", got)
	}
	if !strings.Contains(got, "thay symlink") {
		t.Errorf("fileAction không nói là thay symlink: %q", got)
	}
	if got := fileAction(e, engine.FileChange{Path: "/etc/x", OldExists: true, Hidden: "foreign"}); !strings.Contains(got, "foreign") {
		t.Errorf("fileAction với Hidden: %q", got)
	}
	if got := noCtl("RewriteRule\t^x\x1b[2J$ -\x07"); got != "RewriteRule\t^x?[2J$ -?" {
		t.Errorf("noCtl = %q", got)
	}
}

// Soát cuối M28, C6: output của lệnh (wp chạy mã PHP của site) và lời lỗi của
// bước in ra terminal không mang chuỗi escape.
func TestTienDoKhongChuoiEscape(t *testing.T) {
	var b strings.Builder
	p := newProgress(&b, "vi", true, engine.Plan{})
	p.onEvent(engine.Event{Kind: engine.EventOutput, StepID: "x", Message: "Error: \x1b]0;bi-chiem\x07xoá"})
	p.onEvent(engine.Event{Kind: engine.EventStepFail, StepID: "x", Message: "wp lỗi \x1b[2K"})
	if strings.ContainsAny(b.String(), "\x1b\x07") {
		t.Errorf("tiến độ để lọt ký tự điều khiển: %q", b.String())
	}
}
