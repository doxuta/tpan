// Bản thử của M18, đợt 0: client S3 tự ký SigV4, chỉ dùng thư viện chuẩn.
// Mục đích: chứng minh trên máy ảo rằng (1) một client vài trăm dòng nói
// chuyện được với hai máy chủ S3 khác nhau, (2) tải lên và tải về file 2 GB
// theo dòng tốn bao nhiêu RAM, (3) kho trả lỗi giữa lúc tải nhiều phần thì dọn
// được phần tải dở. KHÔNG phải mã của sản phẩm: mã thật nằm ở internal/s3.
//
// Cấu hình qua biến môi trường (khoá sinh tại chỗ trong máy ảo, không bao giờ
// là khoá thật): S3_ENDPOINT, S3_REGION, S3_BUCKET, S3_ACCESS_KEY,
// S3_SECRET_KEY, S3_PART_MIB (mặc định 8), S3_CA_FILE, S3_VHOST=1.
//
//	s3thu test                    ghi, đọc lại, xoá một object nhỏ
//	s3thu put <file> <key>        tải lên (nhiều phần khi lớn hơn một phần)
//	s3thu get <key> <file>        tải về theo dòng, in sha256
//	s3thu head <key>              cỡ và ETag
//	s3thu list <prefix>           liệt kê object
//	s3thu rm <key>                xoá
//	s3thu mpu <prefix>            liệt kê các lần tải nhiều phần còn dở
//	s3thu mpu-abort <prefix>      huỷ hết các lần tải nhiều phần còn dở
//	s3thu proxy <nghe> <kho> <từ phần> <mã>   proxy gây lỗi: phần từ số đó trở lên nhận mã lỗi
package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------- SigV4

const (
	algorithm     = "AWS4-HMAC-SHA256"
	emptySHA256   = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	amzDateLayout = "20060102T150405Z"
)

// uriEncode mã hoá theo đúng luật của SigV4: chỉ giữ A-Z a-z 0-9 - _ . ~,
// mọi byte khác thành %XX viết hoa. Dấu / trong key do người gọi tách ra trước.
func uriEncode(s string) string {
	const hexUpper = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hexUpper[c>>4])
			b.WriteByte(hexUpper[c&15])
		}
	}
	return b.String()
}

// encodePath mã hoá từng đoạn của đường dẫn, giữ dấu /.
func encodePath(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = uriEncode(s)
	}
	return strings.Join(segs, "/")
}

// canonicalQuery sắp theo tên rồi theo giá trị, mã hoá cả hai.
func canonicalQuery(q url.Values) string {
	var pairs []string
	for k, vs := range q {
		for _, v := range vs {
			pairs = append(pairs, uriEncode(k)+"="+uriEncode(v))
		}
	}
	sort.Strings(pairs)
	return strings.Join(pairs, "&")
}

func hmacSHA256(key []byte, msg string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(msg))
	return h.Sum(nil)
}

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

type client struct {
	endpoint  *url.URL
	region    string
	bucket    string
	accessKey string
	secretKey string
	vhost     bool
	partSize  int64
	http      *http.Client
	now       func() time.Time
}

// newRequest dựng request đã ký. key rỗng là thao tác trên bucket.
// payloadHash là sha256 hex của thân (emptySHA256 khi không có thân).
func (c *client) newRequest(ctx context.Context, method, key string, q url.Values, body io.Reader, size int64, payloadHash string, extra http.Header) (*http.Request, error) {
	u := *c.endpoint
	path := "/" + c.bucket
	if c.vhost {
		u.Host = c.bucket + "." + u.Host
		path = ""
	}
	if key != "" {
		path += "/" + key
	}
	if path == "" {
		path = "/"
	}
	u.Path, u.RawPath = path, encodePath(path)
	u.RawQuery = canonicalQuery(q)
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}
	// Đường đi trên dây phải đúng bằng đường đã ký.
	req.URL.Path, req.URL.RawPath = u.Path, u.RawPath
	req.ContentLength = size
	t := c.now().UTC()
	amzDate, day := t.Format(amzDateLayout), t.Format("20060102")
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadHash)
	for k, vs := range extra {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	// Ký host và mọi header x-amz-*.
	names := []string{"host"}
	vals := map[string]string{"host": u.Host}
	for k, vs := range req.Header {
		lk := strings.ToLower(k)
		if strings.HasPrefix(lk, "x-amz-") {
			names = append(names, lk)
			vals[lk] = strings.Join(vs, ",")
		}
	}
	sort.Strings(names)
	var ch strings.Builder
	for _, n := range names {
		ch.WriteString(n + ":" + strings.TrimSpace(vals[n]) + "\n")
	}
	signed := strings.Join(names, ";")
	canon := strings.Join([]string{method, u.RawPath, u.RawQuery, ch.String(), signed, payloadHash}, "\n")
	scope := day + "/" + c.region + "/s3/aws4_request"
	toSign := strings.Join([]string{algorithm, amzDate, scope, sha256Hex([]byte(canon))}, "\n")
	k := hmacSHA256([]byte("AWS4"+c.secretKey), day)
	k = hmacSHA256(k, c.region)
	k = hmacSHA256(k, "s3")
	k = hmacSHA256(k, "aws4_request")
	sig := hex.EncodeToString(hmacSHA256(k, toSign))
	req.Header.Set("Authorization", algorithm+" Credential="+c.accessKey+"/"+scope+", SignedHeaders="+signed+", Signature="+sig)
	return req, nil
}

// ---------------------------------------------------------------- Lỗi

type s3Error struct {
	Status  int
	Code    string `xml:"Code"`
	Message string `xml:"Message"`
}

func (e *s3Error) Error() string {
	return fmt.Sprintf("S3 trả %d %s: %s", e.Status, e.Code, e.Message)
}

// do gửi request, trả response khi mã là 2xx; không thì đọc lỗi XML.
func (c *client) do(req *http.Request) (*http.Response, error) {
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode/100 == 2 {
		return res, nil
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	e := &s3Error{Status: res.StatusCode}
	if xml.Unmarshal(b, e) != nil || e.Code == "" {
		e.Code, e.Message = http.StatusText(res.StatusCode), strings.TrimSpace(string(b[:min(len(b), 200)]))
	}
	return nil, e
}

func retryable(err error) bool {
	var se *s3Error
	if errors.As(err, &se) {
		return se.Status >= 500 || se.Status == 429
	}
	return !errors.Is(err, context.Canceled)
}

// ---------------------------------------------------------------- Thao tác

func (c *client) putSmall(ctx context.Context, key string, data []byte, meta http.Header) error {
	req, err := c.newRequest(ctx, http.MethodPut, key, nil, bytes.NewReader(data), int64(len(data)), sha256Hex(data), meta)
	if err != nil {
		return err
	}
	res, err := c.do(req)
	if err != nil {
		return err
	}
	return res.Body.Close()
}

type headInfo struct {
	Size   int64
	ETag   string
	SHA256 string
}

func (c *client) head(ctx context.Context, key string) (headInfo, error) {
	req, err := c.newRequest(ctx, http.MethodHead, key, nil, nil, 0, emptySHA256, nil)
	if err != nil {
		return headInfo{}, err
	}
	res, err := c.do(req)
	if err != nil {
		return headInfo{}, err
	}
	defer res.Body.Close()
	return headInfo{Size: res.ContentLength, ETag: res.Header.Get("ETag"), SHA256: res.Header.Get("x-amz-meta-tpan-sha256")}, nil
}

// get tải object theo dòng vào w, trả số byte và sha256 hex.
func (c *client) get(ctx context.Context, key string, w io.Writer) (int64, string, error) {
	req, err := c.newRequest(ctx, http.MethodGet, key, nil, nil, 0, emptySHA256, nil)
	if err != nil {
		return 0, "", err
	}
	res, err := c.do(req)
	if err != nil {
		return 0, "", err
	}
	defer res.Body.Close()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(w, h), res.Body)
	if err != nil {
		return n, "", err
	}
	if res.ContentLength >= 0 && n != res.ContentLength {
		return n, "", fmt.Errorf("nhận %d byte, kho báo %d", n, res.ContentLength)
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}

func (c *client) remove(ctx context.Context, key string) error {
	req, err := c.newRequest(ctx, http.MethodDelete, key, nil, nil, 0, emptySHA256, nil)
	if err != nil {
		return err
	}
	res, err := c.do(req)
	if err != nil {
		return err
	}
	return res.Body.Close()
}

type listResult struct {
	IsTruncated           bool   `xml:"IsTruncated"`
	NextContinuationToken string `xml:"NextContinuationToken"`
	Contents              []struct {
		Key          string `xml:"Key"`
		Size         int64  `xml:"Size"`
		LastModified string `xml:"LastModified"`
		ETag         string `xml:"ETag"`
	} `xml:"Contents"`
}

func (c *client) list(ctx context.Context, prefix string, each func(key string, size int64, modified string)) error {
	token := ""
	for {
		q := url.Values{"list-type": {"2"}, "prefix": {prefix}}
		if token != "" {
			q.Set("continuation-token", token)
		}
		req, err := c.newRequest(ctx, http.MethodGet, "", q, nil, 0, emptySHA256, nil)
		if err != nil {
			return err
		}
		res, err := c.do(req)
		if err != nil {
			return err
		}
		var out listResult
		err = xml.NewDecoder(res.Body).Decode(&out)
		res.Body.Close()
		if err != nil {
			return fmt.Errorf("đọc danh sách: %w", err)
		}
		for _, o := range out.Contents {
			each(o.Key, o.Size, o.LastModified)
		}
		if !out.IsTruncated || out.NextContinuationToken == "" {
			return nil
		}
		token = out.NextContinuationToken
	}
}

type mpuList struct {
	Uploads []struct {
		Key       string `xml:"Key"`
		UploadID  string `xml:"UploadId"`
		Initiated string `xml:"Initiated"`
	} `xml:"Upload"`
}

func (c *client) listUploads(ctx context.Context, prefix string) (mpuList, error) {
	q := url.Values{"uploads": {""}, "prefix": {prefix}}
	req, err := c.newRequest(ctx, http.MethodGet, "", q, nil, 0, emptySHA256, nil)
	if err != nil {
		return mpuList{}, err
	}
	res, err := c.do(req)
	if err != nil {
		return mpuList{}, err
	}
	defer res.Body.Close()
	var out mpuList
	if err := xml.NewDecoder(res.Body).Decode(&out); err != nil {
		return mpuList{}, fmt.Errorf("đọc danh sách tải dở: %w", err)
	}
	return out, nil
}

func (c *client) abort(ctx context.Context, key, uploadID string) error {
	req, err := c.newRequest(ctx, http.MethodDelete, key, url.Values{"uploadId": {uploadID}}, nil, 0, emptySHA256, nil)
	if err != nil {
		return err
	}
	res, err := c.do(req)
	if err != nil {
		return err
	}
	return res.Body.Close()
}

type completePart struct {
	PartNumber int    `xml:"PartNumber"`
	ETag       string `xml:"ETag"`
}

type completeBody struct {
	XMLName xml.Name       `xml:"CompleteMultipartUpload"`
	Parts   []completePart `xml:"Part"`
}

// put tải file lên: một lần PUT khi vừa một phần, nhiều phần khi lớn hơn.
// Chỉ giữ MỘT phần trong bộ nhớ. Trả sha256 hex của toàn bộ dữ liệu đã gửi.
func (c *client) put(ctx context.Context, key string, r io.Reader, size int64, meta http.Header) (string, error) {
	whole := sha256.New()
	buf := make([]byte, c.partSize)
	if size <= c.partSize {
		n, err := io.ReadFull(r, buf[:size])
		if err != nil {
			return "", err
		}
		whole.Write(buf[:n])
		return hex.EncodeToString(whole.Sum(nil)), c.putSmall(ctx, key, buf[:n], meta)
	}
	req, err := c.newRequest(ctx, http.MethodPost, key, url.Values{"uploads": {""}}, nil, 0, emptySHA256, meta)
	if err != nil {
		return "", err
	}
	res, err := c.do(req)
	if err != nil {
		return "", err
	}
	var init struct {
		UploadID string `xml:"UploadId"`
	}
	err = xml.NewDecoder(res.Body).Decode(&init)
	res.Body.Close()
	if err != nil || init.UploadID == "" {
		return "", fmt.Errorf("mở tải nhiều phần: không đọc được UploadId (%v)", err)
	}
	fail := func(err error) (string, error) {
		// Huỷ bằng ctx riêng: ctx của người gọi có thể đã bị huỷ.
		actx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if aerr := c.abort(actx, key, init.UploadID); aerr != nil {
			return "", fmt.Errorf("%w; huỷ phần tải dở cũng lỗi: %v", err, aerr)
		}
		return "", fmt.Errorf("%w (đã huỷ phần tải dở %s)", err, init.UploadID)
	}
	var parts []completePart
	var sent int64
	for n := 1; sent < size; n++ {
		want := min(c.partSize, size-sent)
		if _, err := io.ReadFull(r, buf[:want]); err != nil {
			return fail(fmt.Errorf("đọc phần %d: %w", n, err))
		}
		whole.Write(buf[:want])
		sum := sha256Hex(buf[:want])
		q := url.Values{"partNumber": {strconv.Itoa(n)}, "uploadId": {init.UploadID}}
		var etag string
		var lastErr error
		for try := 1; try <= 4; try++ {
			req, err := c.newRequest(ctx, http.MethodPut, key, q, bytes.NewReader(buf[:want]), want, sum, nil)
			if err != nil {
				return fail(err)
			}
			res, err := c.do(req)
			if err == nil {
				etag = res.Header.Get("ETag")
				res.Body.Close()
				lastErr = nil
				break
			}
			lastErr = err
			if !retryable(err) || ctx.Err() != nil {
				break
			}
			fmt.Fprintf(os.Stderr, "phần %d lần %d lỗi: %v\n", n, try, err)
			time.Sleep(time.Duration(try) * 500 * time.Millisecond)
		}
		if lastErr != nil {
			return fail(fmt.Errorf("tải phần %d: %w", n, lastErr))
		}
		parts = append(parts, completePart{PartNumber: n, ETag: etag})
		sent += want
	}
	body, err := xml.Marshal(completeBody{Parts: parts})
	if err != nil {
		return fail(err)
	}
	req, err = c.newRequest(ctx, http.MethodPost, key, url.Values{"uploadId": {init.UploadID}}, bytes.NewReader(body), int64(len(body)), sha256Hex(body), nil)
	if err != nil {
		return fail(err)
	}
	res, err = c.do(req)
	if err != nil {
		return fail(fmt.Errorf("ghép các phần: %w", err))
	}
	// S3 có thể trả 200 kèm thân <Error>: phải đọc thân mới biết.
	b, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	res.Body.Close()
	var e s3Error
	if xml.Unmarshal(b, &e) == nil && e.Code != "" {
		e.Status = res.StatusCode
		return fail(fmt.Errorf("ghép các phần: %w", &e))
	}
	return hex.EncodeToString(whole.Sum(nil)), nil
}

// ---------------------------------------------------------------- main

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func newClient() (*client, error) {
	u, err := url.Parse(os.Getenv("S3_ENDPOINT"))
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("S3_ENDPOINT không hợp lệ: %q", os.Getenv("S3_ENDPOINT"))
	}
	mib, _ := strconv.ParseInt(env("S3_PART_MIB", "8"), 10, 64)
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 120 * time.Second,
		MaxIdleConnsPerHost:   2,
	}
	if ca := os.Getenv("S3_CA_FILE"); ca != "" {
		pem, err := os.ReadFile(ca)
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("%s không có chứng chỉ PEM nào", ca)
		}
		tr.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	}
	return &client{
		endpoint: u, region: env("S3_REGION", "us-east-1"), bucket: os.Getenv("S3_BUCKET"),
		accessKey: os.Getenv("S3_ACCESS_KEY"), secretKey: os.Getenv("S3_SECRET_KEY"),
		vhost: os.Getenv("S3_VHOST") == "1", partSize: mib << 20, now: time.Now,
		// Không đi theo chuyển hướng: chữ ký gắn với host đã ký.
		http: &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}, nil
}

// peakRSS đọc VmHWM (đỉnh RAM thật của tiến trình) từ /proc; chỉ có trên Linux.
func peakRSS() string {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return "?"
	}
	for _, ln := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(ln, "VmHWM:") {
			return strings.Join(strings.Fields(ln)[1:], " ")
		}
	}
	return "?"
}

// proxyFault chạy một proxy ngược trước máy chủ S3 thật để gây lỗi có chủ ý:
// mọi lần tải phần có số từ failFrom trở lên nhận mã status kèm thân lỗi XML
// (sau khi proxy đã đọc hết thân request, như một kho quá tải). Mọi request
// khác đi thẳng. Host của request được giữ nguyên vì chữ ký SigV4 gắn với nó.
func proxyFault(listen, backend string, failFrom, status int) error {
	target, err := url.Parse(backend)
	if err != nil {
		return err
	}
	rp := &httputil.ReverseProxy{Rewrite: func(r *httputil.ProxyRequest) {
		r.SetURL(target)
		r.Out.Host = r.In.Host
		// SetURL ghép đường dẫn bằng cách giải mã rồi mã hoá lại; giữ nguyên
		// đường dẫn đã ký.
		r.Out.URL.Path, r.Out.URL.RawPath = r.In.URL.Path, r.In.URL.RawPath
		r.Out.URL.RawQuery = r.In.URL.RawQuery
	}}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n, _ := strconv.Atoi(r.URL.Query().Get("partNumber")); r.Method == http.MethodPut && n >= failFrom {
			_, _ = io.Copy(io.Discard, r.Body)
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(status)
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>SlowDown</Code><Message>loi co chu y cua ban thu (phan `+strconv.Itoa(n)+`)</Message></Error>`)
			fmt.Fprintf(os.Stderr, "proxy: phần %d -> %d\n", n, status)
			return
		}
		rp.ServeHTTP(w, r)
	})
	return http.ListenAndServe(listen, h)
}

func run(args []string) error {
	if len(args) == 5 && args[0] == "proxy" {
		from, _ := strconv.Atoi(args[3])
		status, _ := strconv.Atoi(args[4])
		return proxyFault(args[1], args[2], from, status)
	}
	c, err := newClient()
	if err != nil {
		return err
	}
	ctx := context.Background()
	t0 := time.Now()
	defer func() {
		fmt.Fprintf(os.Stderr, "(thời gian %.1fs, đỉnh RAM VmHWM %s)\n", time.Since(t0).Seconds(), peakRSS())
	}()
	switch {
	case len(args) == 1 && args[0] == "test":
		key := "tpan-thu/" + strconv.FormatInt(time.Now().UnixNano(), 36) + " dấu cách+(ngoặc)&.txt"
		data := []byte("tpan thử ghi, đọc, xoá\n")
		if err := c.putSmall(ctx, key, data, http.Header{"x-amz-meta-tpan-sha256": {sha256Hex(data)}}); err != nil {
			return fmt.Errorf("ghi: %w", err)
		}
		hi, err := c.head(ctx, key)
		if err != nil {
			return fmt.Errorf("head: %w", err)
		}
		var buf bytes.Buffer
		_, sum, err := c.get(ctx, key, &buf)
		if err != nil {
			return fmt.Errorf("đọc lại: %w", err)
		}
		if sum != sha256Hex(data) || hi.SHA256 != sum {
			return fmt.Errorf("sha256 đọc lại %s (meta %q), mong %s", sum, hi.SHA256, sha256Hex(data))
		}
		if err := c.remove(ctx, key); err != nil {
			return fmt.Errorf("xoá: %w", err)
		}
		if _, err := c.head(ctx, key); err == nil {
			return errors.New("xoá xong mà head vẫn thấy")
		}
		fmt.Printf("ĐẠT: ghi, head (size=%d etag=%s), đọc lại sha256 khớp, xoá, head sau xoá báo không có\n", hi.Size, hi.ETag)
		return nil
	case len(args) == 3 && args[0] == "put":
		f, err := os.Open(args[1])
		if err != nil {
			return err
		}
		defer f.Close()
		fi, err := f.Stat()
		if err != nil {
			return err
		}
		sum, err := c.put(ctx, args[2], f, fi.Size(), nil)
		if err != nil {
			return err
		}
		fmt.Printf("đã tải lên %s (%d byte), sha256 %s\n", args[2], fi.Size(), sum)
		return nil
	case len(args) == 3 && args[0] == "get":
		f, err := os.OpenFile(args[2], os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		n, sum, err := c.get(ctx, args[1], f)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
		fmt.Printf("đã tải về %s (%d byte), sha256 %s\n", args[1], n, sum)
		return nil
	case len(args) == 2 && args[0] == "head":
		hi, err := c.head(ctx, args[1])
		if err != nil {
			return err
		}
		fmt.Printf("size=%d etag=%s tpan-sha256=%s\n", hi.Size, hi.ETag, hi.SHA256)
		return nil
	case len(args) == 2 && args[0] == "list":
		return c.list(ctx, args[1], func(k string, size int64, mod string) { fmt.Printf("%12d  %s  %s\n", size, mod, k) })
	case len(args) == 2 && args[0] == "rm":
		return c.remove(ctx, args[1])
	case len(args) == 2 && (args[0] == "mpu" || args[0] == "mpu-abort"):
		ups, err := c.listUploads(ctx, args[1])
		if err != nil {
			return err
		}
		fmt.Printf("%d lần tải nhiều phần còn dở\n", len(ups.Uploads))
		for _, u := range ups.Uploads {
			fmt.Printf("  %s  %s  %s\n", u.Initiated, u.UploadID, u.Key)
			if args[0] == "mpu-abort" {
				if err := c.abort(ctx, u.Key, u.UploadID); err != nil {
					return err
				}
				fmt.Println("  đã huỷ")
			}
		}
		return nil
	}
	return errors.New("cách dùng: s3thu test | put <file> <key> | get <key> <file> | head <key> | list <prefix> | rm <key> | mpu <prefix> | mpu-abort <prefix>")
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "LỖI:", err)
		os.Exit(1)
	}
}
