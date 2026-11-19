// Package server chạy HTTP server của tpan: API dưới /api/v1 và giao diện web
// (đã được nhúng sẵn vào file chạy, xem package web).
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/buildinfo"
	"github.com/doxuta/tpan/web"
)

// Run nghe ở địa chỉ listen cho tới khi ctx bị huỷ, rồi tắt êm
// (chờ các request đang chạy tối đa 10 giây).
func Run(ctx context.Context, listen string) error {
	srv := &http.Server{
		Addr:              listen,
		Handler:           NewHandler(web.DistFS()),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	slog.Info("tpan đang chạy", "listen", listen, "version", buildinfo.Version)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// NewHandler ghép API và giao diện web vào một handler. Tách riêng để test được
// mà không cần mở cổng thật.
func NewHandler(dist fs.FS) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", handleHealth)
	mux.HandleFunc("/api/", handleAPINotFound)
	mux.Handle("/", spa(dist))
	return mux
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"version": buildinfo.Version,
		"commit":  buildinfo.Commit,
	})
}

func handleAPINotFound(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// spa phục vụ giao diện một trang: đường dẫn nào trùng file thật thì trả file đó,
// còn lại trả index.html để router phía trình duyệt tự xử lý.
func spa(dist fs.FS) http.Handler {
	files := http.FileServerFS(dist)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name != "" {
			if info, err := fs.Stat(dist, name); err == nil && !info.IsDir() {
				files.ServeHTTP(w, r)
				return
			}
		}
		index, err := fs.ReadFile(dist, "index.html")
		if err != nil {
			http.Error(w, "Giao diện chưa được build. Chạy: make web", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(index)
	})
}
