package checks

import (
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/templates"
)

var suspendedBody = string(templates.SuspendedHTML())

func TestSuspendedStage(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	f := system.NewFake()
	for _, c := range []struct {
		stage Stage
		db    bool
		want  string
	}{
		{StagePHP, false, "Site đang tạm khoá"},
		{StageWordPress, false, "Site đang tạm khoá"},
		{StageDB, true, "DB đang tạm khoá"},
		{StageEndToEnd, true, "DB đang tạm khoá"},
	} {
		r := SuspendedStage(c.stage, l, c.db).Run(bg(), f)
		if r.ID != "suspended."+string(c.stage)+":shop.example.com" || r.Stage != c.stage {
			t.Errorf("ID, Stage = %q, %q", r.ID, r.Stage)
		}
		// Unknown + Suspended: không mở, không đóng sự cố (quyết định 9 của M13).
		if r.Status != Unknown || !r.Suspended {
			t.Errorf("%s: Status %s, Suspended %v; muốn unknown + suspended", r.ID, r.Status, r.Suspended)
		}
		if !strings.Contains(r.Summary.Vi, c.want) {
			t.Errorf("%s: Summary %q thiếu %q", r.ID, r.Summary.Vi, c.want)
		}
		assertText(t, "Title", r.Title)
		assertText(t, "Summary", r.Summary)
		if len(r.ManualFix) != 0 || r.AutoFix != "" {
			t.Errorf("%s: kết quả tạm khoá không có cách sửa: %+v", r.ID, r)
		}
	}
	if len(f.Ran()) != 0 {
		t.Errorf("SuspendedStage chạy lệnh: %v", f.Ran())
	}
}

func TestHTTPSuspendedCommand(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	f := system.NewFake()
	reply(f, []string{"curl"}, 0, httpOut(suspendedBody, "403"), "")
	r := HTTPSuspended(l, true).Run(bg(), f)
	assertShape(t, r, "http.suspended:shop.example.com", StageEndToEnd, Pass)
	assertEvidenceIsRan(t, r, f)
	ran := f.Ran()
	if len(ran) != 2 {
		t.Fatalf("site SSL phải gọi cả cổng 80 và 443, đã gọi %d lần", len(ran))
	}
	for i, want := range [][]string{
		{"--resolve shop.example.com:80:127.0.0.1", "http://shop.example.com/"},
		{"-k", "--resolve shop.example.com:443:127.0.0.1", "https://shop.example.com/"},
	} {
		args := strings.Join(ran[i].Args, " ")
		for _, w := range append(want, "-sS", "--max-time 15") {
			if !strings.Contains(args, w) {
				t.Errorf("lần gọi %d: Args %q thiếu %q", i, args, w)
			}
		}
		if strings.Contains(args, " -L") {
			t.Errorf("không được theo chuyển hướng: %q", args)
		}
	}

	f = system.NewFake()
	reply(f, []string{"curl"}, 0, httpOut(suspendedBody, "403"), "")
	if r := HTTPSuspended(l, false).Run(bg(), f); r.Status != Pass || len(f.Ran()) != 1 {
		t.Errorf("site không SSL: %s, %d lần gọi; muốn Pass, chỉ cổng 80", r.Status, len(f.Ran()))
	}
}

func TestHTTPSuspendedCases(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	cases := []struct {
		name    string
		exit    int
		stdout  string
		stderr  string
		want    Status
		summary string
	}{
		{"403 kèm trang tạm khoá", 0, httpOut(suspendedBody, "403"), "", Pass, "403"},
		{"403 không phải trang tạm khoá", 0, httpOut("<html>403 Forbidden</html>", "403"), "", Fail, "không phải trang tạm khoá"},
		{"site vẫn trả 200", 0, httpOut(wpPage, "200"), "", Fail, "vẫn trả 200"},
		{"chuyển hướng 301", 0, httpOut("", "301"), "", Fail, "301"},
		{"404", 0, httpOut("", "404"), "", Fail, "404"},
		{"không kết nối được", 7, httpOut("", "000"), "curl: (7) Failed to connect", Fail, "cổng 80"},
		{"đóng kết nối không trả gì", 52, httpOut("", "000"), "curl: (52) Empty reply from server", Fail, "đóng kết nối"},
		{"lỗi curl khác", 3, "", "curl: (3) URL using bad/illegal format", Unknown, "mã 3"},
		{"exit 0 mà không có mã", 0, "<html>", "", Unknown, "Chưa kiểm được"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := system.NewFake()
			reply(f, []string{"curl"}, c.exit, c.stdout, c.stderr)
			r := HTTPSuspended(l, false).Run(bg(), f)
			assertShape(t, r, "http.suspended:shop.example.com", StageEndToEnd, c.want)
			assertEvidenceIsRan(t, r, f)
			if !strings.Contains(r.Summary.Vi, c.summary) {
				t.Errorf("Summary = %q, muốn có %q", r.Summary.Vi, c.summary)
			}
			if c.want == Fail {
				last := r.ManualFix[len(r.ManualFix)-1]
				if !strings.HasPrefix(last, "curl -sS --max-time 15 --resolve shop.example.com:80:127.0.0.1 -o /dev/null") {
					t.Errorf("dòng cuối ManualFix không phải lệnh kiểm lại: %q", last)
				}
			}
		})
	}
}

// Cổng 80 đạt mà 443 không: Fail, bằng chứng có cả hai lần gọi.
func TestHTTPSuspendedHTTPSFails(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	f := system.NewFake()
	reply(f, []string{"curl"}, 0, httpOut(suspendedBody, "403"), "")
	f.Handle([]string{"curl", "-sS", "-k"}, func(system.Cmd) system.Result {
		return system.Result{Stdout: httpOut(wpPage, "200")}
	})
	r := HTTPSuspended(l, true).Run(bg(), f)
	assertShape(t, r, "http.suspended:shop.example.com", StageEndToEnd, Fail)
	assertEvidenceIsRan(t, r, f)
	if !strings.Contains(r.Summary.Vi, "https://shop.example.com/") {
		t.Errorf("Summary không nói cổng nào hỏng: %q", r.Summary.Vi)
	}
	// Lệnh không chạy được: Unknown, không phải Fail.
	if r := HTTPSuspended(l, true).Run(cancelledCtx(), system.NewFake()); r.Status != Unknown {
		t.Errorf("ctx huỷ: %s; muốn unknown", r.Status)
	}
	if r := HTTPSuspended(site.Layout{}, false).Run(bg(), system.NewFake()); r.Status != Unknown {
		t.Errorf("Layout zero: %s; muốn unknown", r.Status)
	}
}
