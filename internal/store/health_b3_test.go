package store

import (
	"context"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/checks"
)

// B3: sự cố đi theo tên domain. Chủ mới tạo lại một domain (chủ cũ đã xoá hẳn)
// không được đọc sự cố mở trước lúc site hiện tại được tạo; admin vẫn thấy hết.
func TestSuCoCuKhongTheoDomainSangChuMoi(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	const dom = "shop.example.com"
	// Sự cố của chủ cũ: mở 1 giờ trước khi site hiện tại được tạo (t0), rồi
	// đóng vì "gone" như sau khi xoá hẳn.
	if _, err := s.RecordResults(ctx, dom, []checks.Result{failResult("dns.a:" + dom)}, t0.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PruneResults(ctx, nil, t0.Add(-30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	saveSiteOf(t, s, dom, "an") // Created = t0
	// Sự cố của chủ mới, sau lúc tạo.
	if _, err := s.RecordResults(ctx, dom, []checks.Result{failResult("http.status:" + dom)}, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	all, err := s.Incidents(ctx, false, 0)
	if err != nil || len(all) != 2 {
		t.Fatalf("admin muốn thấy cả 2 sự cố: %v, %v", all, err)
	}
	ids := map[string]int64{}
	for _, i := range all {
		ids[i.CheckID] = i.ID
	}
	oldID, newID := ids["dns.a:"+dom], ids["http.status:"+dom]

	for _, openOnly := range []bool{false, true} {
		incs, err := s.IncidentsOf(ctx, "an", openOnly, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(incs) != 1 || incs[0].ID != newID {
			t.Errorf("IncidentsOf(an, openOnly=%v) = %v; muốn chỉ sự cố %d", openOnly, incs, newID)
		}
	}
	if o, ok, err := s.IncidentOwner(ctx, oldID); err != nil || !ok || o != "" {
		t.Errorf("IncidentOwner(sự cố cũ) = %q, %v, %v; muốn \"\" (chỉ admin)", o, ok, err)
	}
	if o, ok, err := s.IncidentOwner(ctx, newID); err != nil || !ok || o != "an" {
		t.Errorf("IncidentOwner(sự cố mới) = %q, %v, %v; muốn an", o, ok, err)
	}
}
