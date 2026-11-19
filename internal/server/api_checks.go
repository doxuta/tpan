package server

// Bộ nhớ đệm phép kiểm của site.
//
// Vì sao: phép kiểm của một site chạy thật trên máy (checks.SiteChecks qua
// core.Sites.Health): gọi HTTP vào site, hỏi MariaDB, và `wp core
// verify-checksums` đọc lại toàn bộ mã nguồn WordPress, có thể mất vài giây.
// Bảng điều khiển gọi /overview mỗi lần mở trang và định kỳ; chạy lại mọi phép
// kiểm của mọi site mỗi lần thì trang chậm và máy tốn tải vô ích. Nên kết quả
// được giữ 60 giây theo domain; ?refresh=1 chạy lại ngay (nút "kiểm lại").
// Job site.add, site.rm xong thì bỏ mục của domain đó, để kết quả không nói
// về một site đã khác.
//
// M4 thay chỗ này bằng health định kỳ (mục 6.7): phép kiểm chạy theo lịch,
// kết quả nằm ở bảng check_results, API chỉ đọc bảng đó.

import (
	"context"
	"time"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/validate"
)

type apiChecksEntry struct {
	at time.Time
	h  core.Health
}

// apiSiteHealth trả phép kiểm của site d, lấy từ bộ nhớ đệm nếu còn hạn.
// Hai request cùng lỡ bộ nhớ đệm thì cùng chạy; chấp nhận được với một người
// quản trị, và đỡ phải viết cơ chế gộp.
func (s *Server) apiSiteHealth(ctx context.Context, d validate.Domain, refresh bool) (core.Health, error) {
	a := s.api
	key := d.String()
	if !refresh {
		a.mu.Lock()
		e, ok := a.checks[key]
		a.mu.Unlock()
		if ok && s.now().Sub(e.at) < a.checksTTL {
			return e.h, nil
		}
	}
	h, err := s.apiSites().Health(ctx, d)
	if err != nil {
		return core.Health{}, err
	}
	a.mu.Lock()
	a.checks[key] = apiChecksEntry{at: s.now(), h: h}
	a.mu.Unlock()
	return h, nil
}
