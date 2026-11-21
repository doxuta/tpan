package s3

import (
	"crypto/hmac"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/doxuta/tpan/internal/system"
)

func secret(s string) system.Secret { return system.NewSecret(s) }

// fakeS3 là máy chủ S3 giả cho test của client: TÍNH LẠI chữ ký từ request
// nhận được (đường, query đúng như trên dây; sha256 của thân thật nhận) và từ
// chối khi lệch, rồi làm đủ các lời gọi client dùng, giữ object trong bộ nhớ.
type fakeS3 struct {
	t                      *testing.T
	access, secret, region string
	bucket                 string
	vhost                  string // khác rỗng: kiểu bucket.host, đây là Host phải nhận

	mu         sync.Mutex
	objects    map[string][]byte // key đầy đủ
	uploads    map[string]*fakeUpload
	nextID     int
	maxKeys    int // trang của List; 0: 1000
	maxUploads int // trang của ListMultipartUploads; 0: 1000
	requests   int
	log        []string // "METHOD đường?query" đã giải mã đường
	badSig     int
	quiet      bool // chữ ký sai là cố ý, không báo lỗi test

	// hook chạy sau khi chữ ký đúng, trước khi xử lý; trả true là đã trả lời.
	hook func(w http.ResponseWriter, r *http.Request) bool
	// dropComplete: ghép xong thì cắt kết nối, không trả lời (S19).
	dropComplete bool
	// completeErr: bước ghép trả 200 kèm thân <Error>.
	completeErr bool

	srv *httptest.Server
}

type fakeUpload struct {
	key       string
	initiated time.Time
	parts     map[int][]byte
}

func newFakeS3(t *testing.T) *fakeS3 {
	f := newFakeS3NoServer(t)
	f.srv = httptest.NewServer(f)
	t.Cleanup(f.srv.Close)
	return f
}

func newFakeS3NoServer(t *testing.T) *fakeS3 {
	return &fakeS3{
		t: t, access: "AKTEST0123456789", secret: "bimat-thu-0123456789abcdef", region: "vung-thu", bucket: "bkt",
		objects: map[string][]byte{}, uploads: map[string]*fakeUpload{},
	}
}

// client dựng Client trỏ vào máy chủ giả, nhịp thử lại rút còn 1 ms.
func (f *fakeS3) client(t *testing.T, mod func(*Config)) *Client {
	t.Helper()
	cfg := Config{
		Endpoint: f.srv.URL, Region: f.region, Bucket: f.bucket, Prefix: "tpan/may-1", PathStyle: true,
		AccessKey: f.access, SecretKey: secret(f.secret),
	}
	if mod != nil {
		mod(&cfg)
	}
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	c.backoff = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	return c
}

func (f *fakeS3) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests
}

// seen đếm số request có dòng log bắt đầu bằng s.
func (f *fakeS3) seen(s string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, l := range f.log {
		if strings.HasPrefix(l, s) {
			n++
		}
	}
	return n
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	var m strings.Builder
	_ = xml.EscapeText(&m, []byte(msg))
	_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>%s</Code><Message>%s</Message></Error>`, code, m.String())
}

func writeXML(w http.ResponseWriter, v any) {
	b, err := xml.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/xml")
	_, _ = w.Write(b)
}

// verify tính lại chữ ký SigV4 của r như một kho thật.
func (f *fakeS3) verify(r *http.Request, body []byte) error {
	rest, ok := strings.CutPrefix(r.Header.Get("Authorization"), algorithm+" ")
	if !ok {
		return errors.New("Authorization không phải AWS4-HMAC-SHA256")
	}
	fields := map[string]string{}
	for _, p := range strings.Split(rest, ", ") {
		k, v, _ := strings.Cut(p, "=")
		fields[k] = v
	}
	cred := strings.Split(fields["Credential"], "/")
	if len(cred) != 5 || cred[0] != f.access || cred[2] != f.region || cred[3] != "s3" || cred[4] != "aws4_request" {
		return fmt.Errorf("Credential sai: %q", fields["Credential"])
	}
	t, err := time.Parse(amzDateLayout, r.Header.Get("X-Amz-Date"))
	if err != nil || t.Format("20060102") != cred[1] {
		return errors.New("x-amz-date sai hoặc lệch ngày của scope")
	}
	payload := r.Header.Get("X-Amz-Content-Sha256")
	if payload != sha256Hex(body) {
		return errors.New("x-amz-content-sha256 khác sha256 của thân nhận được")
	}
	names := strings.Split(fields["SignedHeaders"], ";")
	has := map[string]bool{}
	for _, n := range names {
		has[n] = true
	}
	for _, need := range []string{"host", "x-amz-date", "x-amz-content-sha256"} {
		if !has[need] {
			return fmt.Errorf("thiếu %s trong SignedHeaders", need)
		}
	}
	for k := range r.Header {
		if lk := strings.ToLower(k); strings.HasPrefix(lk, "x-amz-") && !has[lk] {
			return fmt.Errorf("header %s không được ký", lk)
		}
	}
	hdr := map[string]string{}
	for _, n := range names {
		if n == "host" {
			hdr[n] = r.Host
		} else {
			hdr[n] = r.Header.Get(n)
		}
	}
	rawPath, rawQuery, _ := strings.Cut(r.RequestURI, "?")
	q, err := url.ParseQuery(rawQuery)
	if err != nil {
		return fmt.Errorf("query: %w", err)
	}
	canon := canonicalQuery(q)
	if canon != rawQuery {
		return fmt.Errorf("query trên dây %q khác query chuẩn %q", rawQuery, canon)
	}
	dec, err := url.PathUnescape(rawPath)
	if err != nil || encodePath(dec) != rawPath {
		return fmt.Errorf("đường trên dây %q không phải mã hoá chuẩn", rawPath)
	}
	_, want := signer{access: f.access, secret: f.secret, region: f.region}.sign(r.Method, rawPath, canon, hdr, payload, t)
	if !hmac.Equal([]byte(want), []byte(fields["Signature"])) {
		return errors.New("chữ ký lệch")
	}
	return nil
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return
	}
	rawPath, _, _ := strings.Cut(r.RequestURI, "?")
	path, _ := url.PathUnescape(rawPath)
	f.mu.Lock()
	f.requests++
	f.log = append(f.log, r.Method+" "+path+"?"+r.URL.RawQuery)
	f.mu.Unlock()

	if err := f.verify(r, body); err != nil {
		f.mu.Lock()
		f.badSig++
		f.mu.Unlock()
		if !f.quiet {
			f.t.Errorf("máy chủ giả từ chối chữ ký của %s %s: %v", r.Method, r.RequestURI, err)
		}
		writeErr(w, 403, "SignatureDoesNotMatch", err.Error())
		return
	}
	if f.hook != nil && f.hook(w, r) {
		return
	}

	// Tách bucket và key.
	var key string
	if f.vhost != "" {
		if r.Host != f.vhost {
			writeErr(w, 400, "InvalidHost", "host "+r.Host)
			return
		}
		key = strings.TrimPrefix(path, "/")
	} else {
		b, k, _ := strings.Cut(strings.TrimPrefix(path, "/"), "/")
		if b != f.bucket {
			writeErr(w, 404, "NoSuchBucket", "không có bucket")
			return
		}
		key = k
	}
	q := r.URL.Query()

	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case key == "" && r.Method == http.MethodGet && q.Get("list-type") == "2":
		f.list(w, q)
	case key == "" && r.Method == http.MethodGet && q.Has("uploads"):
		f.listUploads(w, q)
	case key == "":
		writeErr(w, 400, "BadRequest", "thao tác bucket lạ")
	case r.Method == http.MethodPut && q.Has("partNumber"):
		u, ok := f.uploads[q.Get("uploadId")]
		if !ok || u.key != key {
			writeErr(w, 404, "NoSuchUpload", "không có")
			return
		}
		n, _ := strconv.Atoi(q.Get("partNumber"))
		u.parts[n] = body
		w.Header().Set("ETag", `"`+sha256Hex(body)[:32]+`"`)
	case r.Method == http.MethodPut:
		f.objects[key] = body
	case r.Method == http.MethodPost && q.Has("uploads"):
		f.nextID++
		id := "up-" + strconv.Itoa(f.nextID)
		f.uploads[id] = &fakeUpload{key: key, initiated: time.Now(), parts: map[int][]byte{}}
		writeXML(w, struct {
			XMLName  xml.Name `xml:"InitiateMultipartUploadResult"`
			UploadID string   `xml:"UploadId"`
		}{UploadID: id})
	case r.Method == http.MethodPost && q.Has("uploadId"):
		f.complete(w, key, q.Get("uploadId"), body)
	case r.Method == http.MethodDelete && q.Has("uploadId"):
		u, ok := f.uploads[q.Get("uploadId")]
		if !ok || u.key != key {
			writeErr(w, 404, "NoSuchUpload", "không có")
			return
		}
		delete(f.uploads, q.Get("uploadId"))
		w.WriteHeader(204)
	case r.Method == http.MethodDelete:
		delete(f.objects, key)
		w.WriteHeader(204)
	case r.Method == http.MethodGet || r.Method == http.MethodHead:
		data, ok := f.objects[key]
		if !ok {
			if r.Method == http.MethodHead {
				w.WriteHeader(404)
				return
			}
			writeErr(w, 404, "NoSuchKey", "không có")
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.Header().Set("ETag", `"`+sha256Hex(data)[:32]+`"`)
		w.Header().Set("Last-Modified", time.Date(2026, 11, 21, 1, 2, 3, 0, time.UTC).Format(http.TimeFormat))
		if r.Method == http.MethodGet {
			_, _ = w.Write(data)
		}
	default:
		writeErr(w, 405, "MethodNotAllowed", "")
	}
}

func (f *fakeS3) complete(w http.ResponseWriter, key, id string, body []byte) {
	u, ok := f.uploads[id]
	if !ok || u.key != key {
		writeErr(w, 404, "NoSuchUpload", "không có")
		return
	}
	var cb struct {
		Parts []struct {
			PartNumber int
			ETag       string
		} `xml:"Part"`
	}
	if err := xml.Unmarshal(body, &cb); err != nil || len(cb.Parts) == 0 {
		writeErr(w, 400, "MalformedXML", "")
		return
	}
	if f.completeErr {
		w.WriteHeader(200)
		_, _ = io.WriteString(w, `<?xml version="1.0"?>`+"\n   "+`<Error><Code>InternalError</Code><Message>ghép hỏng</Message></Error>`)
		return
	}
	var all []byte
	for i, p := range cb.Parts {
		data, ok := u.parts[p.PartNumber]
		if p.PartNumber != i+1 || !ok || p.ETag != `"`+sha256Hex(data)[:32]+`"` {
			writeErr(w, 400, "InvalidPart", "phần "+strconv.Itoa(p.PartNumber))
			return
		}
		all = append(all, data...)
	}
	f.objects[key] = all
	delete(f.uploads, id)
	if f.dropComplete {
		hijackClose(w)
		return
	}
	writeXML(w, struct {
		XMLName xml.Name `xml:"CompleteMultipartUploadResult"`
		Key     string
	}{Key: key})
}

// hijackClose cắt kết nối ngay, không trả lời (như mạng đứt).
func hijackClose(w http.ResponseWriter) {
	if fl, ok := w.(http.Flusher); ok {
		fl.Flush()
	}
	conn, _, err := w.(http.Hijacker).Hijack()
	if err == nil {
		_ = conn.Close()
	}
}

type fakeContent struct {
	Key          string
	Size         int
	LastModified string
	ETag         string
}

func (f *fakeS3) list(w http.ResponseWriter, q url.Values) {
	prefix, after := q.Get("prefix"), q.Get("continuation-token")
	lim := 1000
	if f.maxKeys > 0 {
		lim = f.maxKeys
	}
	if m, err := strconv.Atoi(q.Get("max-keys")); err == nil && m < lim {
		lim = m
	}
	var keys []string
	for k := range f.objects {
		if strings.HasPrefix(k, prefix) && k > after {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	out := struct {
		XMLName               xml.Name `xml:"ListBucketResult"`
		IsTruncated           bool
		NextContinuationToken string `xml:",omitempty"`
		Contents              []fakeContent
	}{}
	if len(keys) > lim {
		keys = keys[:lim]
		out.IsTruncated, out.NextContinuationToken = true, keys[lim-1]
	}
	for _, k := range keys {
		out.Contents = append(out.Contents, fakeContent{Key: k, Size: len(f.objects[k]), LastModified: "2026-11-21T01:02:03.000Z", ETag: `"x"`})
	}
	writeXML(w, out)
}

type fakeUploadXML struct {
	Key       string
	UploadID  string `xml:"UploadId"`
	Initiated string
}

func (f *fakeS3) listUploads(w http.ResponseWriter, q url.Values) {
	prefix := q.Get("prefix")
	mark := q.Get("key-marker") + "\x00" + q.Get("upload-id-marker")
	var all []fakeUploadXML
	for id, u := range f.uploads {
		if strings.HasPrefix(u.key, prefix) && (q.Get("key-marker") == "" || u.key+"\x00"+id > mark) {
			all = append(all, fakeUploadXML{Key: u.key, UploadID: id, Initiated: u.initiated.UTC().Format(time.RFC3339Nano)})
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Key+"\x00"+all[i].UploadID < all[j].Key+"\x00"+all[j].UploadID })
	lim := 1000
	if f.maxUploads > 0 {
		lim = f.maxUploads
	}
	out := struct {
		XMLName            xml.Name `xml:"ListMultipartUploadsResult"`
		IsTruncated        bool
		NextKeyMarker      string          `xml:",omitempty"`
		NextUploadIDMarker string          `xml:"NextUploadIdMarker,omitempty"`
		Uploads            []fakeUploadXML `xml:"Upload"`
	}{}
	if len(all) > lim {
		all = all[:lim]
		out.IsTruncated, out.NextKeyMarker, out.NextUploadIDMarker = true, all[lim-1].Key, all[lim-1].UploadID
	}
	out.Uploads = all
	writeXML(w, out)
}
