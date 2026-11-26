package server

import (
	"net/http"
	"testing"
)

// M29: POST /api/v1/sites nhận loại site. Site PHP trống không đòi email quản
// trị; site WordPress vẫn đòi; loại lạ bị từ chối.
func TestTaoSitePHPTrongKhongCanEmail(t *testing.T) {
	e := newAuthzEnv(t)
	h := e.s.handler(authzDist)
	b := e.cookies[authzB]
	rec := e.do(h, "POST", "/api/v1/sites?dry_run=1", `{"domain":"wp.example.org"}`, b)
	if rec.Code != http.StatusBadRequest || errCode(t, rec) != "email_invalid" {
		t.Errorf("WordPress thiếu email: %d %s", rec.Code, rec.Body)
	}
	rec = e.do(h, "POST", "/api/v1/sites?dry_run=1", `{"domain":"plain.example.org","app":"php"}`, b)
	// Máy thử có đủ stack thì bản xem trước dựng được (200); không thì lời từ
	// chối phải là chuyện khác, không phải thiếu email.
	if rec.Code != http.StatusOK {
		if c := errCode(t, rec); c == "email_invalid" || c == "bad_request" {
			t.Errorf("site PHP trống không email: %d %s", rec.Code, rec.Body)
		}
	}
	rec = e.do(h, "POST", "/api/v1/sites?dry_run=1", `{"domain":"plain.example.org","app":"drupal","email":"b@example.com"}`, b)
	if errCode(t, rec) != "app_invalid" {
		t.Errorf("loại site lạ: %d %s", rec.Code, rec.Body)
	}
	// Email có mà sai dạng thì vẫn bị từ chối, kể cả với site PHP trống.
	rec = e.do(h, "POST", "/api/v1/sites?dry_run=1", `{"domain":"plain.example.org","app":"php","email":"khong-phai-email"}`, b)
	if rec.Code != http.StatusBadRequest || errCode(t, rec) != "email_invalid" {
		t.Errorf("email sai dạng: %d %s", rec.Code, rec.Body)
	}
}
