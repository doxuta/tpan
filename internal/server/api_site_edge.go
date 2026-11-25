package server

import (
	"context"
	"strings"

	"github.com/doxuta/tpan/internal/checks"
	"github.com/doxuta/tpan/internal/store"
)

// siteEdgePrefixes là ID của các phép kiểm chỉ health xét (core.siteEdgeChecks,
// core.siteNameEdgeChecks): DNS của tên chính và tên phụ, TLS khi site chưa xin
// SSL, chứng chỉ phủ đủ tên. Chúng không nằm trong bộ phép kiểm chạy ngay của
// Sites.Info, nên trước đây trang một site hiện chặng DNS xám ("chưa có phép
// kiểm nào") dù Bảng điều khiển đã xanh.
var siteEdgePrefixes = []string{"dns.a:", "site.alias.dns:", "ssl.names", "tls.cert:"}

// withEdgeResults thêm vào live kết quả đã ghi của các phép kiểm chỉ health xét
// cho site domain, khi live chưa có ID đó. Kết quả chạy ngay luôn thắng kết quả
// đã ghi. Đọc store lỗi thì trả live như cũ: trang vẫn hiện phần chạy ngay,
// chặng còn thiếu hiện xám như trước.
func (s *Server) withEdgeResults(ctx context.Context, domain string, live []checks.Result) []checks.Result {
	stored, err := s.deps.Store.CheckResultsOf(ctx, "")
	if err != nil {
		return live
	}
	return mergeEdgeResults(live, storedOfTarget(stored, domain))
}

// mergeEdgeResults là phần thuần của withEdgeResults, tách ra để test không cần store.
func mergeEdgeResults(live, stored []checks.Result) []checks.Result {
	have := make(map[string]bool, len(live))
	for _, c := range live {
		have[c.ID] = true
	}
	for _, r := range stored {
		if have[r.ID] || !isSiteEdgeID(r.ID) {
			continue
		}
		have[r.ID] = true
		live = append(live, r)
	}
	return live
}

func isSiteEdgeID(id string) bool {
	for _, p := range siteEdgePrefixes {
		if strings.HasPrefix(id, p) {
			return true
		}
	}
	return false
}

// storedOfTarget lấy kết quả đã ghi của đúng site target.
func storedOfTarget(all []store.StoredResult, target string) []checks.Result {
	var out []checks.Result
	for _, sr := range all {
		if sr.Target == target {
			out = append(out, sr.Result)
		}
	}
	return out
}
