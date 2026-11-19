package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func testHandler() http.Handler {
	return NewHandler(fstest.MapFS{
		"index.html":    {Data: []byte("<html>tpan</html>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
	})
}

func get(t *testing.T, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	testHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestHealth(t *testing.T) {
	rec := get(t, "/api/v1/health")
	if rec.Code != http.StatusOK {
		t.Fatalf("mã trả về = %d, muốn 200", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("thân trả về không phải JSON: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("status = %q, muốn \"ok\"", body["status"])
	}
}

func TestUnknownAPIIsJSON404(t *testing.T) {
	rec := get(t, "/api/v1/khong-co")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("mã trả về = %d, muốn 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, muốn JSON", ct)
	}
}

func TestSPA(t *testing.T) {
	cases := []struct {
		name, target, want string
	}{
		{"trang chủ", "/", "<html>tpan</html>"},
		{"file thật", "/assets/app.js", "console.log(1)"},
		{"đường dẫn của router", "/sites/shop.vn", "<html>tpan</html>"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := get(t, c.target)
			if rec.Code != http.StatusOK {
				t.Fatalf("mã trả về = %d, muốn 200", rec.Code)
			}
			if got := rec.Body.String(); got != c.want {
				t.Fatalf("thân = %q, muốn %q", got, c.want)
			}
		})
	}
}

func TestSPAWithoutBuild(t *testing.T) {
	rec := httptest.NewRecorder()
	NewHandler(fstest.MapFS{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("mã trả về = %d, muốn 503 khi chưa build giao diện", rec.Code)
	}
}
