package server

// Panel không phục vụ ở tên của một site (M20, mục 11.2 ý 2, 11.12 R2, R7,
// R12): trang PHP của site cùng tên máy (nếu bị chiếm) đọc được cookie của
// panel đặt dưới tên đó. Tên trùng domain của MỘT site bất kỳ (mọi trạng thái,
// của bất kỳ ai) thì 421; vào bằng IP hoặc bằng panel.domain luôn được, nên
// không ai bị khoá ngoài.

import (
	"context"
	"html"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/doxuta/tpan/internal/i18n"
)

// siteNamesTTL: danh sách domain được đệm 5 giây (R7). Đệm cả danh sách,
// không đệm theo Host lạ: người gửi tự đặt Host, đệm theo nó là để họ làm
// phình bộ nhớ.
const siteNamesTTL = 5 * time.Second

type siteNames struct {
	mu    sync.Mutex
	at    time.Time
	names map[string]bool
}

// siteNameSet trả tập domain của mọi site (mọi trạng thái), nạp lại khi đệm
// cũ hơn siteNamesTTL theo đồng hồ của server (test tua được).
func (s *Server) siteNameSet(ctx context.Context) (map[string]bool, error) {
	c := &s.sites
	c.mu.Lock()
	defer c.mu.Unlock()
	now := s.now()
	if c.names != nil && now.Sub(c.at) < siteNamesTTL && !now.Before(c.at) {
		return c.names, nil
	}
	list, err := s.deps.Store.Sites(ctx)
	if err != nil {
		return nil, err
	}
	names := make(map[string]bool, len(list))
	for _, st := range list {
		names[st.Domain.String()] = true
	}
	c.names, c.at = names, now
	return names, nil
}

// hostIsSite trả 421 host_is_site khi tên máy của request là tên một site;
// true nghĩa là đã trả lời. Request có cookie phiên thì phiên đó bị xoá và
// cookie bị đặt hết hạn (R1): cookie đã lộ dưới tên của site. Request có
// header Authorization được miễn (R12): nó không dùng cookie (auth_m20.go,
// hasAuthorization) nên không có gì để lộ.
func (s *Server) hostIsSite(w http.ResponseWriter, r *http.Request) bool {
	if s.deps.Store == nil || hasAuthorization(r) {
		return false
	}
	h := requestHost(r)
	if h == "" || net.ParseIP(h) != nil {
		return false
	}
	names, err := s.siteNameSet(r.Context())
	if err != nil {
		// Không đọc được DB thì panel cũng không chạy được gì; để request đi
		// tiếp (guard sẽ trả lỗi của nó), không khoá ngoài vì một lần đọc hỏng.
		slog.Error("đọc danh sách site cho 421", "err", err)
		return false
	}
	if !names[h] {
		return false
	}
	if c, err := r.Cookie(authCookieName(r)); err == nil && c.Value != "" && len(c.Value) <= 128 {
		if err := s.deps.Store.DeleteSessionByToken(r.Context(), authTokenHash(c.Value)); err != nil {
			slog.Error("xoá phiên lộ dưới tên site", "err", err)
		}
		authSetCookie(w, r, "", -1)
	}
	if err := s.dropChallenge(w, r); err != nil {
		slog.Error("xoá phiếu lộ dưới tên site", "err", err)
	}
	// Câu chung, không in tên site hay panel.domain (soát cuối F5): người lạ
	// không đọc được tên panel từ trang này.
	ways := panelWays(r)
	msg := i18n.T(
		"Panel tpan không mở ở tên này (mã PHP của một site đọc được cookie đăng nhập). Vào bằng IP của máy hoặc tên miền riêng của panel: "+strings.Join(ways, " hoặc "),
		"The tpan panel does not open under this name (a site's PHP could read the login cookie). Use the server's IP or the panel's own domain name: "+strings.Join(ways, " or "))
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeError(w, http.StatusMisdirectedRequest, "host_is_site", msg)
		return true
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusMisdirectedRequest)
	var b strings.Builder
	b.WriteString("<!doctype html><meta charset=\"utf-8\"><title>tpan</title><h1>421</h1><p>")
	b.WriteString(html.EscapeString(msg.Vi))
	b.WriteString("</p><p>")
	b.WriteString(html.EscapeString(msg.En))
	b.WriteString("</p><ul>")
	for _, u := range ways {
		b.WriteString("<li>" + html.EscapeString(u) + "</li>")
	}
	b.WriteString("</ul>")
	_, _ = w.Write([]byte(b.String()))
	return true
}

// panelWays là địa chỉ vào panel đúng cách: IP và cổng mà kết nối này đã tới
// (http.LocalAddrContextKey). Không kèm panel.domain (F5): trang 421 trả lời
// cả người chưa đăng nhập.
func panelWays(r *http.Request) []string {
	scheme := "https://"
	if r.TLS == nil {
		scheme = "http://"
	}
	var out []string
	port := "8443"
	if a, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
		if host, p, err := net.SplitHostPort(a.String()); err == nil {
			port = p
			out = append(out, scheme+net.JoinHostPort(host, p))
		}
	}
	if len(out) == 0 {
		out = append(out, scheme+"<IP>:"+port)
	}
	return out
}
