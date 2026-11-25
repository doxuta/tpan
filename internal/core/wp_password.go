package core

import (
	"context"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// WPPasswordReset là một lần đặt lại mật khẩu quản trị WordPress đã kiểm xong.
type WPPasswordReset struct {
	*Planned
	URL  string
	step *steps.WPAdminPassword
}

// Login là tên tài khoản đã đặt lại (rỗng khi job chưa chạy xong bước đó).
func (w *WPPasswordReset) Login() string { return w.step.Login }

// Password là mật khẩu mới.
func (w *WPPasswordReset) Password() system.Secret { return w.step.Password }

// PrepareWPPassword dựng Plan "wp.password" cho site d: đặt mật khẩu ngẫu
// nhiên mới cho tài khoản quản trị WordPress đầu tiên. Chỉ đọc.
func (s *Sites) PrepareWPPassword(ctx context.Context, d validate.Domain) (*WPPasswordReset, error) {
	st, err := s.find(ctx, d)
	if err != nil {
		return nil, err
	}
	dom := d.String()
	if st.Status != site.Active {
		return nil, refuse("site_trashed", i18n.T(
			"site "+dom+" đang ở thùng rác; khôi phục nó rồi mới đặt lại mật khẩu WordPress",
			"site "+dom+" is in the trash; restore it before resetting the WordPress password"))
	}
	pw, err := sitePassword()
	if err != nil {
		return nil, err
	}
	step := &steps.WPAdminPassword{Layout: st.Layout(), Password: pw}
	plan := engine.Plan{
		Kind:   "wp.password",
		Target: dom,
		Title:  i18n.T("Đặt lại mật khẩu quản trị WordPress của "+dom, "Reset the WordPress admin password of "+dom),
		Steps:  []engine.Step{step},
	}
	scheme := "http://"
	if st.SSL {
		scheme = "https://"
	}
	return &WPPasswordReset{Planned: newPlanned(s.Sys, s.Journal, plan, nil), URL: scheme + dom + "/wp-admin/", step: step}, nil
}
