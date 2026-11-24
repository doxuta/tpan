package checks

import "testing"

// File của lồng theo site (M17) là chặng PHP: lồng thay chỗ của pool PHP trên
// bản đồ tuyến. Unit tpan khác vẫn là chặng tpan (xem TestDriftStage).
func TestDriftStageCage(t *testing.T) {
	l := siteLayout(t, "shop.example.com")
	cases := map[string]Stage{
		// Chính các đường dẫn tpan ghi cho một lồng.
		l.CageSliceFile():   StagePHP,
		l.CageSocketFile():  StagePHP,
		l.CageServiceFile(): StagePHP,
		l.CageFence():       StagePHP,
		l.CageFPMConf():     StagePHP,
		l.CagePasswd():      StagePHP,
		l.CageGroup():       StagePHP,
		// Đi vòng qua ".." không đánh lừa được (Clean trước).
		"/etc/tpan/cage/x/../../tls/cert.pem": StageTPAN,
		"/etc/nginx/../tpan/cage/a/passwd":    StagePHP,
		// Vẫn là chặng tpan.
		"/etc/systemd/system/tpan.service":      StageTPAN,
		"/etc/systemd/system/tpan-health.timer": StageTPAN,
		"/etc/tpan/tls/cert.pem":                StageTPAN,
		"/etc/tpan/cage":                        StageTPAN, // chính thư mục gốc, không phải file của một lồng
		"/etc/tpan/cagex/a":                     StageTPAN,
		// Không phải của tpan.
		"/etc/systemd/system/tpanx.slice":                        StageTPAN, // tiền tố tpan* cũ giữ nguyên
		"/etc/systemd/system/other-tpan-cage-a.service.d/f.conf": StageMachine,
	}
	for p, want := range cases {
		if got := DriftStage(p); got != want {
			t.Errorf("DriftStage(%q) = %q, muốn %q", p, got, want)
		}
	}
}
