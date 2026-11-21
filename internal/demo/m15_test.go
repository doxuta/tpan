package demo

import (
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

const demoSelfRunning = "v0.2.0-demo"

func demoSelf(sys system.System, st *store.Store, arch string) *core.SelfUpdate {
	return &core.SelfUpdate{Sys: sys, Settings: st, Journal: st, Arch: arch, Version: demoSelfRunning}
}

// Khối "Phiên bản và cập nhật" trên demo: có bản mới để kiểm, có bản trước để
// xem hộp xác nhận quay về, không lượt nào đang chạy.
func TestSelfOnDemo(t *testing.T) {
	ctx, sys, st := newDemo(t)

	// Máy dev chạy --demo có thể là amd64 hoặc arm64 (server truyền runtime.GOARCH).
	for _, arch := range []string{"amd64", "arm64"} {
		c, err := demoSelf(sys, st, arch).Check(ctx)
		if err != nil {
			t.Fatalf("Check %s: %v", arch, err)
		}
		want := core.SelfCheck{
			Current: demoSelfRunning, Latest: demoSelfLatest, URL: core.DefaultSelfURL,
			Asset: "tpan-linux-" + arch, SHA256: demoSelfSum(arch),
		}
		if c != want {
			t.Errorf("Check %s = %+v, muốn %+v (up_to_date false: có bản mới)", arch, c, want)
		}
	}
	if demoSelfSum("amd64") == demoSelfSum("arm64") || len(demoSelfSum("amd64")) != 64 {
		t.Errorf("sha256 giả: %s, %s", demoSelfSum("amd64"), demoSelfSum("arm64"))
	}

	su := demoSelf(sys, st, "amd64")
	status, err := su.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := (core.SelfStatus{Version: demoSelfRunning, URL: core.DefaultSelfURL, HasOld: true}); status != want {
		t.Errorf("Status = %+v, muốn %+v", status, want)
	}
	if busy, err := su.Busy(ctx); err != nil || busy {
		t.Errorf("Busy = %v, %v; muốn false (đơn vị tạm không chạy)", busy, err)
	}
	if run, ok, err := su.Last(ctx); err != nil || ok {
		t.Errorf("Last = %+v, %v, %v; muốn chưa có lượt nào", run, ok, err)
	}

	sys.ResetRan()
	p, err := su.RollbackPreview(ctx)
	if err != nil {
		t.Fatalf("RollbackPreview: %v", err)
	}
	if want := (core.SelfRollbackPreview{Current: demoSelfRunning, Old: demoSelfOld}); p != want {
		t.Errorf("RollbackPreview = %+v, muốn %+v", p, want)
	}
	// Máy giả không có /var/lib/tpan/tpan.db: phép thử `tpan.old site list` bị
	// bỏ qua, lệnh duy nhất chạy trên file cũ là `version`.
	for _, c := range sys.Ran() {
		if strings.HasSuffix(c.Name, "tpan.old") && c.String() != core.TpanBin+".old version" {
			t.Errorf("bản xem trước đã chạy %q trên file cũ", c.String())
		}
	}
}

// Nguồn tải riêng (settings update.self_url) không có trên máy giả: "Kiểm bản
// mới" báo lỗi kèm đủ current, url, asset, không trả nhầm bản của nguồn mặc định.
func TestSelfOnDemoNguonKhac(t *testing.T) {
	ctx, sys, st := newDemo(t)
	const other = "https://dl.example.com/tpan"
	if err := st.SetSetting(ctx, core.SettingSelfURL, other); err != nil {
		t.Fatal(err)
	}
	c, err := demoSelf(sys, st, "amd64").Check(ctx)
	if err == nil {
		t.Fatalf("Check với nguồn tải lạ = %+v, muốn lỗi", c)
	}
	if want := (core.SelfCheck{Current: demoSelfRunning, URL: other, Asset: "tpan-linux-amd64"}); c != want {
		t.Errorf("Check khi lỗi = %+v, muốn %+v", c, want)
	}
}
