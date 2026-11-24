package core

import (
	"context"
	"errors"
	"testing"

	"github.com/doxuta/tpan/internal/site"
)

// TestPrepareAddSlugTrungB11: slug chỉ có 24 bit băm nên dò được hai domain
// trùng slug. Site mới mang slug của một site đang có (kể cả thùng rác) mà
// domain khác thì bị từ chối slug_taken, cho mọi chủ. Tìm một cặp trùng thật
// tốn hàng chục triệu lần băm, nên test chèn thẳng một dòng có domain khác mà
// slug là slug của domain sắp tạo (store thật suy slug từ domain; ở đây chỉ
// cần dòng có slug đó).
func TestPrepareAddSlugTrungB11(t *testing.T) {
	ctx := context.Background()
	se := newSiteEnv(t, Nginx)
	victim := mustDomain(t, "shop.example.net")
	for _, st := range []site.Status{site.Active, site.Trashed} {
		se.sites.mu.Lock()
		se.sites.sites["other.example.org"] = site.Site{
			Domain: mustDomain(t, "other.example.org"), Slug: site.LayoutOf(victim).Slug,
			Status: st, Owner: "khach1", PHPVersion: "8.3",
		}
		se.sites.mu.Unlock()
		for _, owner := range []string{"", "khach2"} {
			in := se.input(t, victim.String())
			in.Owner = owner
			_, err := se.s.PrepareAdd(ctx, in)
			var re *RefusedError
			if !errors.As(err, &re) || re.Code != CodeSlugTaken {
				t.Errorf("%s, chủ %q: %v; muốn từ chối %s", st, owner, err, CodeSlugTaken)
			}
		}
	}
	// Slug khác thì không đổi gì.
	if _, err := se.s.PrepareAdd(ctx, se.input(t, "blog.example.net")); err != nil {
		t.Errorf("blog.example.net: %v; muốn qua", err)
	}
}
