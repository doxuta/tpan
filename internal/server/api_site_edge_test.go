package server

import (
	"testing"

	"github.com/doxuta/tpan/internal/checks"
)

// Trang một site hiện được chặng DNS đã kiểm ở lượt health đầy đủ (lỗi 25/11:
// Bảng điều khiển xanh mà trang site xám), và kết quả chạy ngay luôn thắng.
func TestMergeEdgeResults(t *testing.T) {
	live := []checks.Result{
		{ID: "http.site:a.test", Stage: checks.StageWeb, Status: checks.Pass},
		{ID: "tls.cert:a.test", Stage: checks.StageTLS, Status: checks.Pass},
	}
	stored := []checks.Result{
		{ID: "dns.a:a.test", Stage: checks.StageDNS, Status: checks.Pass},
		{ID: "site.alias.dns:www.a.test", Stage: checks.StageDNS, Status: checks.Warn},
		{ID: "tls.cert:a.test", Stage: checks.StageTLS, Status: checks.Fail},           // cũ, live thắng
		{ID: "wp.checksums:a.test", Stage: checks.StageWordPress, Status: checks.Fail}, // không phải chặng biên
	}
	got := mergeEdgeResults(live, stored)
	byID := map[string]checks.Result{}
	for _, r := range got {
		if _, dup := byID[r.ID]; dup {
			t.Fatalf("trùng ID %s", r.ID)
		}
		byID[r.ID] = r
	}
	if r, ok := byID["dns.a:a.test"]; !ok || r.Status != checks.Pass {
		t.Fatalf("thiếu dns.a đã ghi: %+v", got)
	}
	if _, ok := byID["site.alias.dns:www.a.test"]; !ok {
		t.Fatalf("thiếu DNS của tên phụ: %+v", got)
	}
	if byID["tls.cert:a.test"].Status != checks.Pass {
		t.Fatalf("kết quả chạy ngay phải thắng kết quả đã ghi")
	}
	if _, ok := byID["wp.checksums:a.test"]; ok {
		t.Fatalf("không được thêm phép kiểm ngoài chặng biên")
	}
	if len(got) != 4 {
		t.Fatalf("muốn 4 kết quả, có %d", len(got))
	}
}
