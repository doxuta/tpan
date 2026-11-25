package server

// GET /api/v1/site-check?domain=&email=: kiểm ngay lúc gõ ở màn Tạo site (ô
// domain, ô email) mà không tạo gì. Cùng luật với lúc tạo thật: domain qua
// validate.ParseDomain rồi xem đã thuộc site nào (tên chính hay tên phụ), có
// nằm trong thùng rác, có trùng tên miền của panel; email qua
// validate.ParseEmail. Bước Xem trước vẫn kiểm lại đầy đủ: ô này chỉ để báo sớm.

import (
	"errors"
	"net/http"
	"strings"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/validate"
)

// FieldCheck là kết quả kiểm một ô.
type FieldCheck struct {
	// Value là giá trị đã chuẩn hoá (domain viết thường), rỗng nếu không hợp lệ.
	Value   string     `json:"value"`
	OK      bool       `json:"ok"`
	Code    string     `json:"code,omitempty"`
	Message *i18n.Text `json:"message,omitempty"`
}

// SiteCheck là thân trả về; ô nào không gửi thì không có.
type SiteCheck struct {
	Domain *FieldCheck `json:"domain,omitempty"`
	Email  *FieldCheck `json:"email,omitempty"`
}

func failField(code string, t i18n.Text) *FieldCheck {
	return &FieldCheck{Code: code, Message: &t}
}

func (s *Server) handleSiteCheck(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var out SiteCheck
	if raw := strings.TrimSpace(q.Get("email")); raw != "" {
		if e, err := validate.ParseEmail(raw); err != nil {
			out.Email = failField(validate.CodeEmailInvalid, validateText(err, i18n.T("Email không hợp lệ.", "The email is not valid.")))
		} else {
			out.Email = &FieldCheck{Value: e.String(), OK: true}
		}
	}
	if raw := strings.TrimSpace(q.Get("domain")); raw != "" {
		fc, err := s.checkDomainFree(r, raw)
		if err != nil {
			apiFail(w, r, err)
			return
		}
		out.Domain = fc
	}
	writeJSON(w, http.StatusOK, out)
}

// checkDomainFree: domain hợp lệ và chưa ai dùng.
func (s *Server) checkDomainFree(r *http.Request, raw string) (*FieldCheck, error) {
	ctx := r.Context()
	d, err := validate.ParseDomain(raw)
	if err != nil {
		return failField(validate.CodeDomainInvalid, validateText(err, i18n.T("Tên miền không hợp lệ.", "The domain is not valid."))), nil
	}
	name := d.String()
	if pd, ok, err := s.apiPanel().Domain(ctx); err != nil {
		return nil, err
	} else if ok && pd.String() == name {
		return failField("domain_is_panel", i18n.T(name+" đang là tên miền của panel TPAN.", name+" is the TPAN panel's domain.")), nil
	}
	st, ok, err := s.deps.Store.Site(ctx, d)
	if err != nil {
		return nil, err
	}
	if ok {
		if st.Status == site.Trashed {
			return failField("site_trashed", i18n.T(name+" đang nằm trong Thùng rác: khôi phục hoặc xoá hẳn ở đó trước.", name+" is in the Trash: restore it or delete it for good there first.")), nil
		}
		return failField("site_exists", i18n.T("Đã có site "+name+".", "The site "+name+" already exists.")), nil
	}
	n, ok, err := s.deps.Store.NameSite(ctx, d)
	if err != nil {
		return nil, err
	}
	if ok {
		return failField("name_taken", i18n.T(name+" đang là tên phụ của site "+n.Site.String()+".", name+" is already an alias of the site "+n.Site.String()+".")), nil
	}
	return &FieldCheck{Value: name, OK: true}, nil
}

// validateText lấy lời của *validate.Error, không có thì dùng fallback.
func validateText(err error, fallback i18n.Text) i18n.Text {
	var ve *validate.Error
	if errors.As(err, &ve) {
		return ve.Message
	}
	return fallback
}
