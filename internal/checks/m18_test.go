package checks

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/system"
)

var m18Now = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)

func m18Clock() time.Time { return m18Now }

func probeOf(p S3Probe, err error) func(context.Context) (S3Probe, error) {
	return func(context.Context) (S3Probe, error) { return p, err }
}

func stateOf(s S3State, err error) func(context.Context) (S3State, error) {
	return func(context.Context) (S3State, error) { return s, err }
}

func hasFix(r Result, want string) bool { return slices.Contains(r.ManualFix, want) }

// ---- tpan.s3:target ----

func TestS3TargetDat(t *testing.T) {
	ev := []Evidence{{Command: "s3 check", ExitCode: 0, Output: "ok"}, {Command: "s3 head a.example.com", ExitCode: 0, Output: "200"}}
	fake := system.NewFake()
	r := S3Target(probeOf(S3Probe{Evidence: ev}, nil)).Run(bg(), fake)
	assertShape(t, r, "tpan.s3:target", StageTPAN, Pass)
	if len(r.Evidence) != 2 || r.Evidence[1].Command != "s3 head a.example.com" {
		t.Errorf("Pass phải mang Evidence của probe: %+v", r.Evidence)
	}
	assertNoCmd(t, fake)
}

func TestS3TargetProbeLoiLaUnknown(t *testing.T) {
	r := S3Target(probeOf(S3Probe{}, errors.New("chưa có khoá truy cập"))).Run(bg(), system.NewFake())
	assertShape(t, r, "tpan.s3:target", StageTPAN, Unknown)
	if !strings.Contains(r.Summary.Vi, "chưa có khoá truy cập") {
		t.Errorf("Summary phải nêu lý do: %q", r.Summary.Vi)
	}
}

func TestS3TargetProblemTheoMa(t *testing.T) {
	for _, tc := range []struct {
		code string
		want string // đoạn nằm trong Summary.Vi
	}{
		{"s3_unreachable", "không nối được"},
		{"s3_auth_failed", "từ chối khoá"},
		{"s3_bucket_not_found", "không có bucket"},
		{"s3_error", "trả lỗi"},
		{"s3_la_hoac_chua_biet", "trả lỗi"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			p := S3Probe{
				Evidence: []Evidence{{Command: "s3 check", ExitCode: 1, Output: "403"}},
				Problem:  tc.code,
				Detail:   "HTTP 403 AccessDenied",
				Missing:  []string{"a.example.com (b1)"}, // Problem đi trước Missing
			}
			r := S3Target(probeOf(p, nil)).Run(bg(), system.NewFake())
			assertShape(t, r, "tpan.s3:target", StageTPAN, Fail)
			if !strings.Contains(r.Summary.Vi, tc.want) || !strings.Contains(r.Summary.Vi, "HTTP 403 AccessDenied") {
				t.Errorf("Summary.Vi = %q, muốn có %q và Detail", r.Summary.Vi, tc.want)
			}
			if strings.Contains(r.Summary.Vi, "mất bản") {
				t.Errorf("Problem phải đi trước Missing: %q", r.Summary.Vi)
			}
			if !hasFix(r, "sudo tpan backup target test") || !hasFix(r, "sudo tpan backup target set --endpoint URL --bucket BUCKET") {
				t.Errorf("ManualFix = %q, muốn có target test và target set", r.ManualFix)
			}
			if lastFix(r) != "sudo tpan backup target test" {
				t.Errorf("dòng cuối ManualFix phải là lệnh kiểm lại: %q", r.ManualFix)
			}
			if len(r.Evidence) != 1 {
				t.Errorf("Evidence = %+v, muốn của probe", r.Evidence)
			}
		})
	}
}

func TestS3TargetCauseKhacNhauTheoMa(t *testing.T) {
	seen := map[string]string{}
	for _, code := range []string{"s3_unreachable", "s3_auth_failed", "s3_bucket_not_found", "s3_error"} {
		r := S3Target(probeOf(S3Probe{Problem: code}, nil)).Run(bg(), system.NewFake())
		if prev, dup := seen[r.Cause.Vi]; dup {
			t.Errorf("%s và %s có cùng Cause", code, prev)
		}
		seen[r.Cause.Vi] = code
	}
}

func TestS3TargetMatBan(t *testing.T) {
	missing := []string{"a.example.com (b1)", "b.example.com (b2)", "c.example.com (b3)", "d.example.com (b4)", "e.example.com (b5)", "f.example.com (b6)", "g.example.com (b7)"}
	r := S3Target(probeOf(S3Probe{Missing: missing}, nil)).Run(bg(), system.NewFake())
	assertShape(t, r, "tpan.s3:target", StageTPAN, Fail)
	if !strings.Contains(r.Summary.Vi, "kho mất bản") || !strings.Contains(r.Summary.En, "lost backups") {
		t.Errorf("Summary = %+v", r.Summary)
	}
	if !strings.Contains(r.Summary.Vi, "a.example.com (b1)") || !strings.Contains(r.Summary.Vi, "e.example.com (b5)") {
		t.Errorf("phải nêu 5 bản đầu: %q", r.Summary.Vi)
	}
	if strings.Contains(r.Summary.Vi, "f.example.com") || !strings.Contains(r.Summary.Vi, "2 mục nữa") {
		t.Errorf("tối đa 5 mục, phần dư là '2 mục nữa': %q", r.Summary.Vi)
	}
	if !hasFix(r, "sudo tpan backup push") {
		t.Errorf("ManualFix = %q, muốn có sudo tpan backup push", r.ManualFix)
	}
}

func TestS3TargetLamSachCauLoiCuaKho(t *testing.T) {
	// Câu lỗi do kho trả: ký tự điều khiển không được lọt vào Summary.
	r := S3Target(probeOf(S3Probe{Problem: "s3_error", Detail: "lỗi\nXML\x00lạ"}, nil)).Run(bg(), system.NewFake())
	assertShape(t, r, "tpan.s3:target", StageTPAN, Fail)
	if strings.ContainsAny(r.Summary.Vi, "\n\x00") || strings.ContainsAny(r.Summary.En, "\n\x00") {
		t.Errorf("Summary còn ký tự điều khiển: %q", r.Summary.Vi)
	}
}

// ---- tpan.s3:fresh ----

// freshOK là trạng thái không có ý nào đúng.
func freshOK() S3State {
	return S3State{Schedule: true, HasKey: true, PanelLast: m18Now.Add(-time.Hour)}
}

func runFresh(s S3State) Result {
	return S3Fresh(stateOf(s, nil), m18Clock).Run(bg(), system.NewFake())
}

func TestS3FreshDatKhongYNaoDung(t *testing.T) {
	r := runFresh(freshOK())
	assertShape(t, r, "tpan.s3:fresh", StageTPAN, Pass)
}

func TestS3FreshYA(t *testing.T) {
	s := freshOK()
	s.Failed = 7
	s.FailedDomains = []string{"a.example.com", "b.example.com", "c.example.com", "d.example.com", "e.example.com"}
	s.LastError = "kho từ chối khoá"
	r := runFresh(s)
	assertShape(t, r, "tpan.s3:fresh", StageTPAN, Warn)
	for _, want := range []string{"7 bản", "a.example.com", "e.example.com", "kho từ chối khoá"} {
		if !strings.Contains(r.Summary.Vi, want) {
			t.Errorf("Summary.Vi thiếu %q: %q", want, r.Summary.Vi)
		}
	}
	if !hasFix(r, "sudo tpan backup push") || hasFix(r, "sudo tpan backup key new") {
		t.Errorf("ManualFix = %q, muốn có push và không có key new", r.ManualFix)
	}
}

func TestS3FreshYATargetErrorMotMinh(t *testing.T) {
	s := freshOK()
	s.TargetError = "kho không trả lời"
	r := runFresh(s)
	assertShape(t, r, "tpan.s3:fresh", StageTPAN, Warn)
	if !strings.Contains(r.Summary.Vi, "kho không trả lời") {
		t.Errorf("Summary phải nêu lỗi của đích: %q", r.Summary.Vi)
	}
}

func TestS3FreshYB(t *testing.T) {
	s := freshOK()
	s.Stale = []string{"a.example.com", "b.example.com"}
	r := runFresh(s)
	assertShape(t, r, "tpan.s3:fresh", StageTPAN, Warn)
	if !strings.Contains(r.Summary.Vi, "2 site") || !strings.Contains(r.Summary.Vi, "a.example.com") {
		t.Errorf("Summary = %q", r.Summary.Vi)
	}
	// Không có lịch thì ý (b) không áp dụng: máy chỉ sao lưu tay không bị cảnh báo mãi.
	s.Schedule = false
	assertShape(t, runFresh(s), "tpan.s3:fresh", StageTPAN, Pass)
}

func TestS3FreshYC(t *testing.T) {
	cases := map[string]func(*S3State){
		"chưa có gói":     func(s *S3State) { s.PanelLast = time.Time{} },
		"gói cũ":          func(s *S3State) { s.PanelLast = m18Now.Add(-72 * time.Hour) },
		"lần đẩy gói lỗi": func(s *S3State) { s.PanelError = "kho đầy" },
	}
	for name, mod := range cases {
		t.Run(name, func(t *testing.T) {
			s := freshOK()
			mod(&s)
			r := runFresh(s)
			assertShape(t, r, "tpan.s3:fresh", StageTPAN, Warn)
			if !strings.Contains(r.Summary.Vi, "gói panel") {
				t.Errorf("Summary phải nêu gói panel: %q", r.Summary.Vi)
			}
			// Không có lịch thì (c) không áp dụng; không có khoá thì chỉ (d) nói.
			s2 := s
			s2.Schedule = false
			assertShape(t, runFresh(s2), "tpan.s3:fresh", StageTPAN, Pass)
		})
	}
}

func TestS3FreshYCKhongCoKhoaChiBaoYD(t *testing.T) {
	s := freshOK()
	s.PanelLast = time.Time{}
	s.HasKey = false
	r := runFresh(s)
	assertShape(t, r, "tpan.s3:fresh", StageTPAN, Warn)
	if strings.Contains(r.Summary.Vi, "chưa có gói panel") || !strings.Contains(r.Summary.Vi, "khoá sao lưu") {
		t.Errorf("không có khoá thì chỉ nói (d): %q", r.Summary.Vi)
	}
}

func TestS3FreshYD(t *testing.T) {
	s := freshOK()
	s.HasKey = false
	r := runFresh(s)
	assertShape(t, r, "tpan.s3:fresh", StageTPAN, Warn)
	if !hasFix(r, "sudo tpan backup key new") || !hasFix(r, "sudo tpan backup push") {
		t.Errorf("ManualFix = %q, muốn có key new và push", r.ManualFix)
	}
	// Ý (d) đúng cả khi không bật lịch.
	s.Schedule = false
	assertShape(t, runFresh(s), "tpan.s3:fresh", StageTPAN, Warn)
}

func TestS3FreshCaBonY(t *testing.T) {
	s := S3State{
		Failed: 2, FailedDomains: []string{"a.example.com"}, LastError: "lỗi A",
		TargetError: "lỗi đích", Schedule: true, Stale: []string{"b.example.com"},
		HasKey: false,
	}
	r := runFresh(s)
	assertShape(t, r, "tpan.s3:fresh", StageTPAN, Warn)
	for _, want := range []string{"2 bản", "lỗi A", "lỗi đích", "b.example.com", "khoá sao lưu"} {
		if !strings.Contains(r.Summary.Vi, want) {
			t.Errorf("Summary.Vi thiếu %q: %q", want, r.Summary.Vi)
		}
	}
	s.HasKey = true // (c) thế chỗ (d): chưa có gói panel
	r = runFresh(s)
	if !strings.Contains(r.Summary.Vi, "gói panel") {
		t.Errorf("có khoá, lịch bật, chưa có gói: phải nêu gói panel: %q", r.Summary.Vi)
	}
	if lastFix(r) != "sudo tpan health" {
		t.Errorf("dòng cuối ManualFix phải là lệnh kiểm lại: %q", r.ManualFix)
	}
}

func TestS3FreshMocNamMuoiTamGio(t *testing.T) {
	for _, tc := range []struct {
		age  time.Duration
		want Status
	}{
		{47 * time.Hour, Pass},
		{48 * time.Hour, Pass}, // "cũ hơn 48 giờ": đúng 48 giờ chưa quá
		{49 * time.Hour, Warn},
		{-time.Hour, Pass}, // đồng hồ lệch: gói "ở tương lai" không bị báo cũ
	} {
		s := freshOK()
		s.PanelLast = m18Now.Add(-tc.age)
		r := runFresh(s)
		if r.Status != tc.want {
			t.Errorf("PanelLast cách %v: %s, muốn %s (%q)", tc.age, r.Status, tc.want, r.Summary.Vi)
		}
		if tc.want == Warn && !strings.Contains(r.Summary.Vi, "49 giờ") {
			t.Errorf("Summary phải nêu số giờ: %q", r.Summary.Vi)
		}
	}
}

func TestS3FreshStateLoiLaUnknown(t *testing.T) {
	r := S3Fresh(stateOf(S3State{}, errors.New("database is locked")), m18Clock).Run(bg(), system.NewFake())
	assertShape(t, r, "tpan.s3:fresh", StageTPAN, Unknown)
	if !strings.Contains(r.Summary.Vi, "database is locked") {
		t.Errorf("Summary phải nêu lý do: %q", r.Summary.Vi)
	}
}

func TestS3FreshNowNilDungGioThat(t *testing.T) {
	s := freshOK()
	s.PanelLast = time.Now().Add(-time.Hour)
	r := S3Fresh(stateOf(s, nil), nil).Run(bg(), system.NewFake())
	assertShape(t, r, "tpan.s3:fresh", StageTPAN, Pass)
}

func TestS3FreshKhongLoSoHieuRaNgoai(t *testing.T) {
	// Evidence chỉ có số liệu và cờ, không chép câu lỗi (có thể dài hay lạ).
	s := freshOK()
	s.LastError = "BI-MAT-KHONG-DUOC-LO"
	s.Failed = 1
	r := runFresh(s)
	for _, e := range r.Evidence {
		if strings.Contains(e.Output, "BI-MAT") {
			t.Errorf("Evidence không được chép câu lỗi: %q", e.Output)
		}
	}
}
