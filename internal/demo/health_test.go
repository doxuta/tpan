package demo

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/health"
	"github.com/doxuta/tpan/internal/monitor"
	"github.com/doxuta/tpan/internal/system"
)

// Health chạy đầy đủ trên máy demo (như bộ hẹn giờ của `tpan serve --demo`)
// chỉ được đỏ ở đúng site hỏng: phép kiểm của máy (RAM, lệch file...) mà đỏ
// thì người xem demo thấy sự cố giả.
func TestHealthRunOnDemo(t *testing.T) {
	ctx, sys, st := newDemo(t)
	_, wpcli := Hashes()
	r := &health.Runner{Sys: sys, Store: st, WPCLISHA512: wpcli, NoSelf: true}
	rep, err := r.Run(ctx, health.Options{Full: true})
	if err != nil {
		t.Fatal(err)
	}
	var failed, warned []string
	for _, g := range rep.Groups {
		for _, x := range g.Results {
			switch x.Status {
			case checks.Fail:
				failed = append(failed, x.ID)
			case checks.Warn, checks.Unknown:
				if demoExpectedWarn(x) {
					continue
				}
				warned = append(warned, string(x.Status)+" "+x.ID+": "+x.Summary.Vi)
			}
		}
	}
	slices.Sort(failed)
	if want := []string{"http.site:" + siteWiki, "php.socket:" + siteWiki}; !slices.Equal(failed, want) {
		t.Errorf("Fail = %v; muốn %v", failed, want)
	}
	if len(warned) > 0 {
		t.Errorf("cảnh báo hoặc chưa kiểm được trên máy demo: %v", warned)
	}
	if len(rep.Changes.Opened) != 2 {
		t.Errorf("sự cố mở = %d; muốn 2", len(rep.Changes.Opened))
	}
}

// tail của máy demo: log trực tiếp (tail -F) trả vài dòng rồi thoát ngay, và
// số request đếm được từ access.log giả (tail -n) có cả 5xx ở site hỏng.
func TestDemoTail(t *testing.T) {
	ctx, sys, _ := newDemo(t)
	for _, dom := range []string{siteBlog, siteWiki} {
		l, _ := layoutOf(dom)
		for _, p := range []string{l.LogDir + "/access.log", l.LogDir + "/error.log", l.PHPErrorLog} {
			var lines []string
			done := make(chan error, 1)
			go func() {
				_, err := sys.Run(ctx, system.Cmd{Name: "tail", Args: []string{"-n", "100", "-F", p}, OnOutput: func(s string) { lines = append(lines, s) }})
				done <- err
			}()
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("tail %s: %v", p, err)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("tail -F %s treo", p)
			}
			if len(lines) == 0 {
				t.Errorf("tail %s không có dòng nào", p)
			}
		}
	}

	now := time.Now()
	for dom, want5xx := range map[string]bool{siteBlog: false, siteWiki: true} {
		l, _ := layoutOf(dom)
		tr, err := monitor.SiteTraffic(context.Background(), sys, l.LogDir+"/access.log", now, 0)
		if err != nil {
			t.Fatal(err)
		}
		if tr.Requests == 0 || (tr.Errors5xx > 0) != want5xx {
			t.Errorf("%s: traffic = %+v", dom, tr)
		}
	}

	// Log lỗi của site hỏng nói đúng lỗi: không nối được socket của pool.
	l, _ := layoutOf(siteWiki)
	res, err := sys.Run(ctx, system.Cmd{Name: "tail", Args: []string{"-n", "100", "-F", l.LogDir + "/error.log"}})
	if err != nil || !strings.Contains(res.Stdout, l.SocketFor("8.3")) {
		t.Errorf("error.log của %s: %q, %v", siteWiki, res.Stdout, err)
	}
}

// demoExpectedWarn: cảnh báo có chủ ý của máy demo. Shop, wiki, lab và ba site
// tên dài của M16 chưa xin SSL (màn SSL có nút xin để bấm thử), nên chặng TLS
// của các site đó là "chưa có chứng chỉ" ở dạng cảnh báo. Các chặng xám "tạm khoá" của lab (M13: Unknown
// có cờ Suspended) là điều đã biết, không phải sự cố. Mọi cảnh báo khác vẫn
// làm test hỏng.
func demoExpectedWarn(x checks.Result) bool {
	if x.Status == checks.Unknown && x.Suspended {
		return true
	}
	// M17 (m17.go): gói của khach_a, khach_b bật lồng mà ba site của họ chưa
	// vào, nên cage.state cảnh báo "chờ vào lồng", đúng như máy thật.
	return x.Status == checks.Warn && slices.Contains([]string{"tls.cert:" + siteShop, "tls.cert:" + siteWiki, "tls.cert:" + siteLab,
		"tls.cert:" + siteLongShop, "tls.cert:" + siteLongEdu, "tls.cert:" + siteLongNews,
		"cage.state:" + siteShop, "cage.state:" + siteWiki, "cage.state:" + siteLab}, x.ID)
}
