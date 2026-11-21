// Command faultproxy is a reverse proxy in front of a real S3 server that fails
// upload-part requests on purpose. scripts/e2e-m18.sh (part D) points the
// backup target at it to show that a multipart upload broken in the middle
// leaves no unfinished upload behind. Taken from the "proxy" mode of the M18
// research client (docs/bang-chung/M18-nghien-cuu/ban-thu-s3).
//
//	faultproxy -listen 127.0.0.1:7071 -backend http://127.0.0.1:7070 -from 3 -status 503
//
// Every PUT with partNumber >= -from gets -status and an S3 XML error body,
// after the proxy has read the whole request body (like an overloaded store).
// Every other request goes straight through. The Host header, the raw path and
// the raw query are kept as sent, because the SigV4 signature covers them.
// One line per request goes to stderr (method, path, query keys and the status
// sent back); headers are never logged, so the Authorization header never is.
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:7071", "address to listen on")
	backend := flag.String("backend", "http://127.0.0.1:7070", "endpoint of the real S3 server")
	from := flag.Int("from", 3, "first part number that gets the error")
	status := flag.Int("status", http.StatusServiceUnavailable, "HTTP status sent for those parts")
	flag.Parse()
	if err := run(*listen, *backend, *from, *status); err != nil {
		log.Fatal(err)
	}
}

func run(listen, backend string, from, status int) error {
	target, err := url.Parse(backend)
	if err != nil {
		return fmt.Errorf("-backend: %w", err)
	}
	if target.Scheme != "http" && target.Scheme != "https" || target.Host == "" {
		return fmt.Errorf("-backend must be an http:// or https:// URL with a host, got %q", backend)
	}
	if from < 1 || status < 400 || status > 599 {
		return fmt.Errorf("-from must be >= 1 and -status an HTTP error code (got %d, %d)", from, status)
	}
	srv := &http.Server{
		Addr:              listen,
		Handler:           handler(target, from, status),
		ReadHeaderTimeout: 30 * time.Second,
	}
	fmt.Fprintf(os.Stderr, "faultproxy: %s -> %s, part >= %d gets %d\n", listen, target, from, status)
	return srv.ListenAndServe()
}

func handler(target *url.URL, from, status int) http.Handler {
	rp := &httputil.ReverseProxy{Rewrite: func(r *httputil.ProxyRequest) {
		r.SetURL(target)
		r.Out.Host = r.In.Host
		// SetURL joins paths by decoding and re-encoding; keep the signed path.
		r.Out.URL.Path, r.Out.URL.RawPath = r.In.URL.Path, r.In.URL.RawPath
		r.Out.URL.RawQuery = r.In.URL.RawQuery
	}}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		part, _ := strconv.Atoi(q.Get("partNumber"))
		if r.Method == http.MethodPut && part >= from {
			_, _ = io.Copy(io.Discard, r.Body)
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(status)
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>SlowDown</Code><Message>faultproxy: deliberate error for part `+strconv.Itoa(part)+`</Message></Error>`)
			logLine(r, q, status, true)
			return
		}
		rec := &recorder{ResponseWriter: w, code: http.StatusOK}
		rp.ServeHTTP(rec, r)
		logLine(r, q, rec.code, false)
	})
}

// recorder keeps the status code the backend answered with, for the log line.
type recorder struct {
	http.ResponseWriter
	code int
}

func (r *recorder) WriteHeader(code int) {
	r.code = code
	r.ResponseWriter.WriteHeader(code)
}

// logLine prints the request without headers. Query values are dropped except
// partNumber (uploadId values are long and say nothing a reader needs).
func logLine(r *http.Request, q url.Values, code int, fault bool) {
	keys := make([]string, 0, len(q))
	for k := range q {
		if k == "partNumber" {
			k += "=" + q.Get(k)
		}
		keys = append(keys, k)
	}
	slices.Sort(keys)
	tag := ""
	if fault {
		tag = " (deliberate)"
	}
	fmt.Fprintf(os.Stderr, "faultproxy: %s %s ?%s -> %d%s\n", r.Method, r.URL.EscapedPath(), strings.Join(keys, "&"), code, tag)
}
