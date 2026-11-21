package health

// Test hai phép kiểm của kho S3 (M18, mục 3.13): chỉ có khi đã đặt đích (B11),
// tpan.s3:target là phép kiểm nặng.

import (
	"context"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/store"
)

// setS3Target ghi setting của một đích như `tpan backup target set` đã chạy.
// Không có file khoá trên máy giả: Probe báo lỗi ngay, không gọi mạng.
func setS3Target(t *testing.T, e *env) {
	t.Helper()
	for k, v := range map[string]string{
		core.SettingS3Endpoint: "https://s3.example.com",
		core.SettingS3Bucket:   "tpan-thu",
		core.SettingS3Prefix:   "tpan/may",
	} {
		if err := e.st.SetSetting(context.Background(), k, v); err != nil {
			t.Fatal(err)
		}
	}
}

// TestKhongDichKhongCoPhepKiemS3 (B11): máy chưa đặt đích thì tập ID phép
// kiểm như trước M18; đặt đích thì thêm đúng hai ID, cùng target tpan.
func TestKhongDichKhongCoPhepKiemS3(t *testing.T) {
	e := newEnv(t, true)
	before := targets(t, e.run(t, Options{Full: true}))
	for id := range before {
		if strings.HasPrefix(id, "tpan.s3:") {
			t.Errorf("chưa đặt đích mà có %s", id)
		}
	}

	setS3Target(t, e)
	after := targets(t, e.run(t, Options{Full: true}))
	var added []string
	for id, tg := range after {
		if _, ok := before[id]; ok {
			continue
		}
		added = append(added, id)
		if tg != store.TargetTPAN {
			t.Errorf("%s: target %q, muốn %q", id, tg, store.TargetTPAN)
		}
	}
	slices.Sort(added)
	if !slices.Equal(added, []string{"tpan.s3:fresh", "tpan.s3:target"}) {
		t.Errorf("đặt đích thêm %v, muốn đúng tpan.s3:fresh và tpan.s3:target", added)
	}
	if len(after) != len(before)+2 {
		t.Errorf("đặt đích đổi %d ID khác: trước %v, sau %v", len(after)-len(before)-2,
			slices.Sorted(maps.Keys(before)), slices.Sorted(maps.Keys(after)))
	}
}

// TestPhepKiemS3NangNhe: vòng nhẹ chỉ có tpan.s3:fresh (không gọi mạng);
// tpan.s3:target chỉ ở vòng đầy đủ. Thiếu file khoá thì target là Fail ngay.
func TestPhepKiemS3NangNhe(t *testing.T) {
	e := newEnv(t, true)
	setS3Target(t, e)

	light := e.run(t, Options{})
	got := map[string]checks.Result{}
	for _, g := range light.Groups {
		for _, r := range g.Results {
			got[r.ID] = r
		}
	}
	if _, ok := got["tpan.s3:fresh"]; !ok {
		t.Error("vòng nhẹ thiếu tpan.s3:fresh")
	}
	if _, ok := got["tpan.s3:target"]; ok {
		t.Error("vòng nhẹ có tpan.s3:target (phép kiểm nặng, gọi mạng)")
	}

	full := e.run(t, Options{Full: true})
	for _, g := range full.Groups {
		for _, r := range g.Results {
			if r.ID == "tpan.s3:target" && r.Status != checks.Fail {
				t.Errorf("đích thiếu file khoá: tpan.s3:target = %s, muốn Fail", r.Status)
			}
		}
	}
}

// TestPhepKiemS3SettingHong: setting của đích bị sửa tay sai thì health vẫn
// chạy xong và hai phép kiểm báo Unknown, không im lặng bỏ qua.
func TestPhepKiemS3SettingHong(t *testing.T) {
	e := newEnv(t, true)
	setS3Target(t, e)
	if err := e.st.SetSetting(context.Background(), core.SettingS3Keep, "nhieu"); err != nil {
		t.Fatal(err)
	}
	rep := e.run(t, Options{})
	for _, g := range rep.Groups {
		for _, r := range g.Results {
			if r.ID == "tpan.s3:fresh" {
				if r.Status != checks.Unknown {
					t.Errorf("tpan.s3:fresh = %s, muốn Unknown", r.Status)
				}
				return
			}
		}
	}
	t.Error("setting hỏng mà không có tpan.s3:fresh")
}
