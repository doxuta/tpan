package health

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/store"
)

func statusOf(rep Report) map[string]checks.Status {
	out := map[string]checks.Status{}
	for _, g := range rep.Groups {
		for _, r := range g.Results {
			out[r.ID] = r.Status
		}
	}
	return out
}

func TestSelfChecksM20(t *testing.T) {
	e := newEnv(t, true)
	got := targets(t, e.run(t, Options{}))
	for _, id := range []string{"tpan.2fa:admins", "tpan.2fa:attempts", "tpan.clock", "tpan.secretkey"} {
		if got[id] != store.TargetTPAN {
			t.Errorf("%s: target %q, muốn %q", id, got[id], store.TargetTPAN)
		}
	}
	for id := range got {
		if strings.HasPrefix(id, "tpan.panelcert:") {
			t.Errorf("chưa đặt tên panel mà có %s", id)
		}
	}

	// Có tên panel: thêm phép kiểm chứng chỉ ACME (máy giả không có file: Fail).
	if err := e.st.SetSetting(context.Background(), core.SettingPanelDomain, "panel.example.com"); err != nil {
		t.Fatal(err)
	}
	st := statusOf(e.run(t, Options{}))
	if s, ok := st["tpan.panelcert:panel.example.com"]; !ok || s != checks.Fail {
		t.Errorf("tpan.panelcert = %q (%v)", s, ok)
	}
	// Chưa ai bật hai bước: tpan.clock không chạy timedatectl (health của máy
	// chưa dùng hai bước không thêm lệnh nào).
	for _, c := range e.sys.Ran() {
		if c.Name == "timedatectl" {
			t.Errorf("chưa ai bật hai bước mà chạy %s", c.String())
		}
	}

	// Setting hỏng (sửa tay) không làm hỏng vòng health.
	if err := e.st.SetSetting(context.Background(), core.SettingPanelDomain, "khong hop le"); err != nil {
		t.Fatal(err)
	}
	if _, ok := statusOf(e.run(t, Options{}))["tpan.2fa:admins"]; !ok {
		t.Error("setting hỏng làm mất phép kiểm tự kiểm")
	}

	// NoSelf (--demo): không có phép kiểm nào của M20.
	e.r.NoSelf = true
	if _, ok := targets(t, e.run(t, Options{}))["tpan.2fa:admins"]; ok {
		t.Error("NoSelf mà vẫn kiểm hai bước")
	}
}

// tpan.2fa:attempts đọc audit login.2fa_locked trong 7 ngày qua (soát R5).
func TestTwoFAAttemptsFromAudit(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	add := func(at time.Time, target, result string) {
		if err := e.st.AddAudit(ctx, store.AuditEntry{Time: at, IP: "192.0.2.1", Action: "login.2fa_locked", Target: target, Result: result}); err != nil {
			t.Fatal(err)
		}
	}
	add(t0.Add(-8*24*time.Hour), "cu", "locked") // quá 7 ngày
	add(t0.Add(-time.Hour), "binh", "locked")
	add(t0.Add(-time.Hour), "an", "other")
	got, err := e.r.lockedRecently(ctx)
	if err != nil || !reflect.DeepEqual(got, []string{"binh"}) {
		t.Fatalf("lockedRecently = %v, %v", got, err)
	}
	if st := statusOf(e.run(t, Options{})); st["tpan.2fa:attempts"] != checks.Warn {
		t.Errorf("tpan.2fa:attempts = %q", st["tpan.2fa:attempts"])
	}
}

// MFAStatuses lỗi thì phép kiểm ra Unknown, vòng health vẫn chạy (chỉ chạy
// được khi store còn trả lỗi; bản đã viết xong thì bỏ qua).
func TestM20ChecksUnknownOnStoreError(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	if _, err := e.st.MFAStatuses(ctx); err != nil {
		st := statusOf(e.run(t, Options{}))
		for _, id := range []string{"tpan.2fa:admins", "tpan.secretkey"} {
			if st[id] != checks.Unknown {
				t.Errorf("%s = %q khi MFAStatuses lỗi", id, st[id])
			}
		}
	}
}
