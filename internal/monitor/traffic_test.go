package monitor

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/system"
)

const logPath = "/var/log/tpan/a.example.com/access.log"

var trafficNow = time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)

// logLine dựng một dòng "combined" ở thời điểm at (giờ UTC).
func logLine(at time.Time, status int) string {
	return fmt.Sprintf(`203.0.113.7 - - [%s] "GET / HTTP/1.1" %d 1234 "-" "curl/8.5"`+"\n", at.UTC().Format(combinedTime), status)
}

// fakeTail giả lệnh tail: trả stdout cho mọi lệnh bắt đầu bằng "tail".
func fakeTail(stdout string) *system.FakeSystem {
	f := system.NewFake()
	f.Handle([]string{"tail"}, func(system.Cmd) system.Result { return system.Result{Stdout: stdout} })
	return f
}

func TestSiteTrafficDem(t *testing.T) {
	ago := func(n int) time.Time { return trafficNow.Add(-time.Duration(n) * time.Minute) }
	out := logLine(ago(120), 200) + // quá cũ
		logLine(ago(61), 500) + // quá cũ, dù là 5xx
		logLine(ago(59), 200) +
		logLine(ago(30), 404) +
		logLine(ago(20), 500) +
		logLine(ago(10), 502) +
		logLine(ago(5), 599) +
		logLine(ago(1), 301) +
		logLine(trafficNow.Add(time.Minute), 500) // ở tương lai (đồng hồ lệch): không tính
	f := fakeTail(out)
	got, err := SiteTraffic(context.Background(), f, logPath, trafficNow, TrafficWindow)
	if err != nil {
		t.Fatal(err)
	}
	want := Traffic{Requests: 6, Errors5xx: 3, Since: trafficNow.Add(-time.Hour)}
	if got != want {
		t.Errorf("got %+v, muốn %+v", got, want)
	}
	ran := f.Ran()
	if len(ran) != 1 || ran[0].String() != "tail -n 50000 "+logPath {
		t.Errorf("lệnh đã chạy = %v", ran)
	}
}

func TestSiteTrafficBienKhoang(t *testing.T) {
	// Đúng đầu khoảng và đúng now đều tính; một giây ngoài thì không.
	since := trafficNow.Add(-time.Hour)
	out := logLine(since.Add(-time.Second), 200) + logLine(since, 200) + logLine(trafficNow, 500) + logLine(trafficNow.Add(time.Second), 200)
	got, err := SiteTraffic(context.Background(), fakeTail(out), logPath, trafficNow, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if got.Requests != 2 || got.Errors5xx != 1 {
		t.Errorf("got %+v", got)
	}
}

func TestSiteTrafficMuiGio(t *testing.T) {
	// 17:00 +0700 là 10:00 UTC: cùng một thời điểm, nên nằm trong khoảng.
	line := `203.0.113.7 - - [19/Nov/2026:16:30:00 +0700] "GET / HTTP/1.1" 503 1 "-" "-"` + "\n"
	got, err := SiteTraffic(context.Background(), fakeTail(line), logPath, trafficNow, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if got.Requests != 1 || got.Errors5xx != 1 {
		t.Errorf("got %+v", got)
	}
}

func TestSiteTrafficBoQuaDongHong(t *testing.T) {
	ok := logLine(trafficNow.Add(-time.Minute), 500)
	bad := []string{
		"",
		"\n",
		"khong phai log\n",
		`203.0.113.7 - - [khong-phai-gio] "GET / HTTP/1.1" 500 1 "-" "-"` + "\n",
		`203.0.113.7 - - [19/Nov/2026:09:59:00 +0000 "GET / HTTP/1.1" 500 1 "-" "-"` + "\n",   // thiếu ]
		`203.0.113.7 - - [19/Nov/2026:09:59:00 +0000] GET / HTTP/1.1 500 1 "-" "-"` + "\n",    // request không có nháy
		`203.0.113.7 - - [19/Nov/2026:09:59:00 +0000] "GET / HTTP/1.1 500 1 "-" "-"` + "\n",   // nháy không đóng (xét tới hết dòng)
		`203.0.113.7 - - [19/Nov/2026:09:59:00 +0000] "GET / HTTP/1.1"` + "\n",                // thiếu mã
		`203.0.113.7 - - [19/Nov/2026:09:59:00 +0000] "GET / HTTP/1.1" abc 1 "-" "-"` + "\n",  // mã không phải số
		`203.0.113.7 - - [19/Nov/2026:09:59:00 +0000] "GET / HTTP/1.1" 600 1 "-" "-"` + "\n",  // ngoài 100..599
		`203.0.113.7 - - [19/Nov/2026:09:59:00 +0000] "GET / HTTP/1.1" 5000 1 "-" "-"` + "\n", // không phải 3 chữ số
		"\x00\xff\xfe [ ] \"\n",
	}
	got, err := SiteTraffic(context.Background(), fakeTail(strings.Join(bad, "")+ok), logPath, trafficNow, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if got.Requests != 1 || got.Errors5xx != 1 {
		t.Errorf("got %+v, chỉ dòng cuối là hợp lệ", got)
	}
}

func TestSiteTrafficChuoiLaCuaKhach(t *testing.T) {
	at := trafficNow.Add(-time.Minute).Format(combinedTime)
	// Request chứa nháy kép thoát, "]" và "[" giả; User-Agent chứa chuỗi giống một dòng log khác.
	// Mã thật là 200, không phải các số giả nằm trong chuỗi của khách.
	lines := []string{
		fmt.Sprintf(`203.0.113.7 - - [%s] "GET /a\"b\" 500 HTTP/1.1" 200 12 "-" "-"`, at),
		fmt.Sprintf(`203.0.113.7 - - [%s] "GET /x[1] HTTP/1.1" 502 12 "-" "-"`, at),
		fmt.Sprintf(`203.0.113.7 - - [%s] "GET / HTTP/1.1" 200 12 "-" "ua \"x\" [01/Jan/2000:00:00:00 +0000] \"GET / HTTP/1.1\" 500"`, at),
		fmt.Sprintf(`203.0.113.7 - - [%s] "GET /\x22 500 HTTP/1.1" 404 12 "-" "-"`, at), // dạng \x22 của Nginx
	}
	got, err := SiteTraffic(context.Background(), fakeTail(strings.Join(lines, "\n")+"\n"), logPath, trafficNow, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if got.Requests != 4 || got.Errors5xx != 1 {
		t.Errorf("got %+v, muốn 4 request và đúng 1 lỗi 5xx", got)
	}
}

func TestSiteTrafficDongCuoiKhongCoXuongDong(t *testing.T) {
	out := logLine(trafficNow.Add(-time.Minute), 200) + strings.TrimSuffix(logLine(trafficNow.Add(-time.Second), 500), "\n")
	got, err := SiteTraffic(context.Background(), fakeTail(out), logPath, trafficNow, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if got.Requests != 2 || got.Errors5xx != 1 {
		t.Errorf("got %+v", got)
	}
}

func TestSiteTrafficChuaCoFile(t *testing.T) {
	cases := map[string]system.Result{
		"tail báo không có file":            {ExitCode: 1, Stderr: "tail: cannot open '" + logPath + "' for reading: No such file or directory\n"},
		"tail báo không có file (stderr 2)": {ExitCode: 1, Stderr: "tail: " + logPath + ": No such file or directory"},
	}
	for name, res := range cases {
		t.Run(name, func(t *testing.T) {
			f := system.NewFake()
			f.Handle([]string{"tail"}, func(system.Cmd) system.Result { return res })
			got, err := SiteTraffic(context.Background(), f, logPath, trafficNow, time.Hour)
			if err != nil {
				t.Fatalf("file chưa có không phải lỗi, nhưng: %v", err)
			}
			if want := (Traffic{Since: trafficNow.Add(-time.Hour)}); got != want {
				t.Errorf("got %+v, muốn %+v", got, want)
			}
		})
	}
	t.Run("log rỗng", func(t *testing.T) {
		got, err := SiteTraffic(context.Background(), fakeTail(""), logPath, trafficNow, time.Hour)
		if err != nil || got.Requests != 0 || got.Partial {
			t.Errorf("got %+v, %v", got, err)
		}
	})
}

func TestSiteTrafficLoiThat(t *testing.T) {
	t.Run("tail lỗi khác", func(t *testing.T) {
		f := system.NewFake()
		f.Handle([]string{"tail"}, func(system.Cmd) system.Result {
			return system.Result{ExitCode: 1, Stderr: "tail: cannot open '" + logPath + "' for reading: Permission denied"}
		})
		_, err := SiteTraffic(context.Background(), f, logPath, trafficNow, time.Hour)
		if err == nil || !strings.Contains(err.Error(), logPath) || !strings.Contains(err.Error(), "Permission denied") {
			t.Errorf("err = %v, muốn lỗi có ngữ cảnh", err)
		}
	})
	t.Run("không chạy được lệnh", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := SiteTraffic(ctx, system.NewFake(), logPath, trafficNow, time.Hour); err == nil {
			t.Error("ctx đã huỷ mà không lỗi")
		}
	})
	t.Run("đường dẫn không tuyệt đối", func(t *testing.T) {
		f := system.NewFake()
		for _, p := range []string{"", "access.log", "-f", "../x"} {
			if _, err := SiteTraffic(context.Background(), f, p, trafficNow, time.Hour); err == nil {
				t.Errorf("%q: muốn lỗi", p)
			}
		}
		if len(f.Ran()) != 0 {
			t.Errorf("không được chạy lệnh nào, nhưng đã chạy %v", f.Ran())
		}
	})
}

func TestSiteTrafficWindowMacDinh(t *testing.T) {
	got, err := SiteTraffic(context.Background(), fakeTail(""), logPath, trafficNow, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Since.Equal(trafficNow.Add(-TrafficWindow)) {
		t.Errorf("Since = %v", got.Since)
	}
}

func TestSiteTrafficPartial(t *testing.T) {
	// n dòng, dòng đầu cách now `first` phút, đều tăm tắp tới gần now.
	build := func(n int, first time.Duration) string {
		var b strings.Builder
		step := first / time.Duration(n)
		for i := range n {
			b.WriteString(logLine(trafficNow.Add(-first+time.Duration(i)*step), 200))
		}
		return b.String()
	}
	cases := []struct {
		name    string
		out     string
		partial bool
	}{
		{"đủ 50000 dòng và dòng cũ nhất vẫn trong khoảng: có thể còn nữa", build(trafficTailLines, 30*time.Minute), true},
		{"đủ 50000 dòng nhưng dòng cũ nhất đã quá khoảng: phủ hết", build(trafficTailLines, 90*time.Minute), false},
		{"dưới 50000 dòng: đã đọc hết file", build(trafficTailLines-1, 30*time.Minute), false},
		{"đủ 50000 dòng nhưng không dòng nào đọc được", strings.Repeat("rac\n", trafficTailLines), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := SiteTraffic(context.Background(), fakeTail(c.out), logPath, trafficNow, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			if got.Partial != c.partial {
				t.Errorf("Partial = %v, muốn %v (requests %d)", got.Partial, c.partial, got.Requests)
			}
		})
	}
}
