package s3

import (
	"bytes"
	"context"
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
	"net/url"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/system"
)

// Client là client S3 thật (mục 3.4 của docs/thay-doi/M18-thiet-ke.md). Dựng
// bằng New. An toàn khi dùng từ nhiều goroutine.
type Client struct {
	scheme, host           string // của endpoint; host có thể kèm cổng
	region, bucket, prefix string
	pathStyle              bool
	accessKey              string
	secretKey              system.Secret
	partSize               int64
	now                    func() time.Time
	http                   *http.Client

	// Hạn giờ và nhịp thử lại. Là trường không xuất để test đặt nhỏ; New đặt
	// giá trị của mục 3.4. Hạn nối và bắt tay TLS nằm trong Transport.
	headerTimeout   time.Duration   // chờ header trả lời
	completeTimeout time.Duration   // chờ header ở bước ghép các phần (S19)
	idleTimeout     time.Duration   // không có byte thân nào đi hay về
	abortTimeout    time.Duration   // hạn của lời huỷ tải dở sau lỗi
	backoff         []time.Duration // nghỉ giữa các lần thử; số lần thử = len + 1
}

const (
	maxErrBody = 64 << 10 // thân lỗi đọc tối đa 64 KB (mục 3.4)
	maxXMLBody = 16 << 20 // câu trả lời liệt kê: 1000 key x 1 KB còn xa mới tới
)

// New kiểm cấu hình bằng các hàm Check* rồi dựng client. Không gọi mạng.
func New(cfg Config) (*Client, error) {
	ep, err := CheckEndpoint(cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	region, err := CheckRegion(cfg.Region)
	if err != nil {
		return nil, err
	}
	for _, err := range []error{
		CheckBucket(cfg.Bucket), CheckPrefix(cfg.Prefix), CheckAccessKey(cfg.AccessKey),
		CheckSecretKey(cfg.SecretKey.Reveal()), CheckCA(cfg.CAPEM),
	} {
		if err != nil {
			return nil, err
		}
	}
	if cfg.PartSize < 0 {
		return nil, fmt.Errorf("s3: cỡ phần âm %d", cfg.PartSize)
	}
	u, err := url.Parse(ep)
	if err != nil {
		return nil, fmt.Errorf("s3: endpoint %q: %w", ep, err)
	}
	// Tối thiểu TLS 1.2. Có CA riêng thì CHỈ tin các CA đó (không cộng thêm
	// kho CA của hệ thống). Cố ý không có tuỳ chọn bỏ kiểm chứng chỉ.
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if len(cfg.CAPEM) > 0 {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(cfg.CAPEM) {
			return nil, fieldErr(CodeCAInvalid, "chứng chỉ gốc không đọc được", "the CA certificate cannot be read")
		}
		tlsCfg.RootCAs = pool
	}
	c := &Client{
		scheme: u.Scheme, host: u.Host,
		region: region, bucket: cfg.Bucket, prefix: cfg.Prefix, pathStyle: cfg.PathStyle,
		accessKey: cfg.AccessKey, secretKey: cfg.SecretKey,
		partSize: cfg.PartSize, now: cfg.Now,
		headerTimeout:   60 * time.Second,
		completeTimeout: 10 * time.Minute,
		idleTimeout:     120 * time.Second,
		abortTimeout:    30 * time.Second,
		backoff:         []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second},
	}
	if c.partSize == 0 {
		c.partSize = DefaultPartSize
	}
	if c.now == nil {
		c.now = time.Now
	}
	c.http = &http.Client{
		Transport: &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			DialContext:         (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			TLSHandshakeTimeout: 15 * time.Second,
			TLSClientConfig:     tlsCfg,
			MaxIdleConnsPerHost: 2,
			IdleConnTimeout:     90 * time.Second,
		},
		// Không đi theo chuyển hướng: request sang host khác sẽ mang chữ ký
		// của ta tới đó, và một kho giả có thể dẫn ta đi dò mạng nội bộ.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return c, nil
}

// full ghép tiền tố của đích với một key tương đối đã qua CheckKey.
func (c *Client) full(key string) string { return c.prefix + "/" + key }

// ---------------------------------------------------------------- Một request

// request mô tả một lời gọi. Thân nằm trọn trong bộ nhớ (một phần tối đa cỡ
// phần) nên tính được sha256 trước khi gửi và gửi lại được khi thử lại.
type request struct {
	op            string // Error.Op
	method        string
	key           string // key ĐẦY ĐỦ trên kho (đã ghép tiền tố); "" là thao tác trên bucket
	query         url.Values
	body          []byte
	headerTimeout time.Duration // 0: c.headerTimeout
	canRetry      func() bool   // nil: luật chung; Get dùng để không thử lại sau byte đầu
}

// newRequest dựng request đã ký. URL.RawPath và URL.RawQuery đúng bằng chuỗi
// đã ký, nên kho tính lại chữ ký từ đúng thứ nó nhận. Trả thêm chữ ký để làm
// sạch câu lỗi.
func (c *Client) newRequest(ctx context.Context, r request) (*http.Request, string, error) {
	host, path := c.host, "/"+c.bucket
	if !c.pathStyle {
		host, path = c.bucket+"."+c.host, ""
	}
	if r.key != "" {
		path += "/" + r.key
	}
	if path == "" {
		path = "/"
	}
	req, err := http.NewRequestWithContext(ctx, r.method, c.scheme+"://"+host+"/", nil)
	if err != nil {
		return nil, "", fmt.Errorf("dựng request: %w", err)
	}
	req.URL.Path, req.URL.RawPath, req.URL.RawQuery = path, encodePath(path), canonicalQuery(r.query)
	// Thân luôn có ký bằng sha256 thật (không UNSIGNED-PAYLOAD): kho tự từ
	// chối một phần bị hỏng trên đường đi.
	hash := sha256Hex(r.body)
	t := c.now().UTC()
	amz := t.Format(amzDateLayout)
	req.Header.Set("X-Amz-Date", amz)
	req.Header.Set("X-Amz-Content-Sha256", hash)
	signed := map[string]string{"host": host, "x-amz-date": amz, "x-amz-content-sha256": hash}
	auth, sig := signer{access: c.accessKey, secret: c.secretKey.Reveal(), region: c.region}.
		sign(r.method, req.URL.RawPath, req.URL.RawQuery, signed, hash, t)
	req.Header.Set("Authorization", auth)
	return req, sig, nil
}

// errStalled là nguyên nhân khi đồng hồ canh của một lần thử hết giờ. Nó
// không bọc context.Canceled, nên Classify xếp vào s3_unreachable chứ không
// nhầm thành "người gọi huỷ".
var errStalled = errors.New("hết giờ: kho im lặng quá lâu (chưa trả lời, hoặc không có byte nào đi hay về)")

// call là một lần thử đang mở. Đồng hồ canh huỷ lần thử khi quá hạn: hạn chờ
// header khi đang chờ trả lời, hạn im lặng khi đang truyền thân. Mỗi lần có
// byte đi hay về thì đồng hồ đặt lại, nên file lớn trên mạng chậm vẫn chạy
// được mà không cần hạn cho cả thao tác.
type call struct {
	ctx    context.Context // của người gọi
	actx   context.Context // của lần thử, huỷ bằng đồng hồ canh
	cancel context.CancelCauseFunc
	timer  *time.Timer
	idle   time.Duration
	op     string
	sig    string
	res    *http.Response
}

// netErr đổi lỗi mạng (hoặc lỗi đọc thân) thành *Error Status 0.
func (k *call) netErr(err error) error {
	if cerr := k.ctx.Err(); cerr != nil {
		return &Error{Op: k.op, Err: cerr}
	}
	if errors.Is(context.Cause(k.actx), errStalled) {
		return &Error{Op: k.op, Err: errStalled}
	}
	return &Error{Op: k.op, Err: err}
}

// Read đọc thân trả lời và đặt lại đồng hồ canh khi có byte về.
func (k *call) Read(b []byte) (int, error) {
	n, err := k.res.Body.Read(b)
	if n > 0 {
		k.timer.Reset(k.idle)
	}
	return n, err
}

func (k *call) close() {
	k.timer.Stop()
	if k.res != nil {
		_ = k.res.Body.Close()
	}
	k.cancel(nil)
}

// readAll đọc cả thân (tối đa limit byte).
func (k *call) readAll(limit int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(k, limit+1))
	if err != nil {
		return nil, k.netErr(err)
	}
	if int64(len(b)) > limit {
		return nil, &Error{Op: k.op, Status: k.res.StatusCode, Code: "InvalidResponse", Message: "câu trả lời của kho dài quá mức cho phép"}
	}
	return b, nil
}

// decode đọc thân XML vào v.
func (k *call) decode(v any) error {
	b, err := k.readAll(maxXMLBody)
	if err != nil {
		return err
	}
	if xml.Unmarshal(b, v) != nil {
		return &Error{Op: k.op, Status: k.res.StatusCode, Code: "InvalidResponse", Message: "không đọc được câu trả lời XML của kho"}
	}
	return nil
}

// pokeReader là thân request: mỗi lần transport đọc được byte thì đặt lại
// đồng hồ canh về hạn im lặng; đọc hết thân thì chuyển sang hạn chờ header.
type pokeReader struct {
	r     *bytes.Reader
	k     *call
	after time.Duration
}

func (p *pokeReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.k.timer.Reset(p.k.idle)
	}
	if err == io.EOF {
		p.k.timer.Reset(p.after)
	}
	return n, err
}

func (p *pokeReader) Close() error { return nil }

// attempt gửi một lần. 2xx: trả call đang mở, người gọi phải close. Khác 2xx:
// đọc thân lỗi, trả *Error.
func (c *Client) attempt(ctx context.Context, r request) (*call, error) {
	if err := ctx.Err(); err != nil {
		return nil, &Error{Op: r.op, Err: err}
	}
	ht := r.headerTimeout
	if ht == 0 {
		ht = c.headerTimeout
	}
	actx, cancel := context.WithCancelCause(ctx)
	k := &call{ctx: ctx, actx: actx, cancel: cancel, idle: c.idleTimeout, op: r.op}
	first := ht
	if len(r.body) > 0 {
		first = c.idleTimeout
	}
	k.timer = time.AfterFunc(first, func() { cancel(errStalled) })
	req, sig, err := c.newRequest(actx, r)
	if err != nil {
		k.close()
		return nil, &Error{Op: r.op, Err: err}
	}
	k.sig = sig
	if len(r.body) > 0 {
		body := func() io.ReadCloser { return &pokeReader{r: bytes.NewReader(r.body), k: k, after: ht} }
		req.Body = body()
		// GetBody cho phép transport tự gửi lại khi một kết nối giữ sẵn đã chết.
		req.GetBody = func() (io.ReadCloser, error) { return body(), nil }
		req.ContentLength = int64(len(r.body))
	}
	res, err := c.http.Do(req)
	if err != nil {
		e := k.netErr(err)
		k.close()
		return nil, e
	}
	k.res = res
	k.timer.Reset(c.idleTimeout)
	if res.StatusCode/100 == 2 {
		return k, nil
	}
	defer k.close()
	return nil, c.statusErr(k)
}

// errXML là thân lỗi của S3.
type errXML struct {
	XMLName xml.Name
	Code    string
	Message string
	Region  string
}

// parseErrXML trả thân lỗi khi b là XML có gốc <Error>.
func parseErrXML(b []byte) (errXML, bool) {
	var x errXML
	if xml.Unmarshal(b, &x) != nil || x.XMLName.Local != "Error" {
		return errXML{}, false
	}
	return x, true
}

// statusErr dựng *Error từ một câu trả lời khác 2xx. Thân đọc tối đa 64 KB
// (lỗi đọc thì dùng phần đã có). Thân không phải XML thì Message rỗng: thân
// chữ của một proxy hay trang lỗi có thể chép lại bất cứ thứ gì (S11).
func (c *Client) statusErr(k *call) error {
	res := k.res
	b, _ := io.ReadAll(io.LimitReader(k, maxErrBody))
	x, ok := parseErrXML(b)
	code, msg := x.Code, x.Message
	if code == "" {
		code = http.StatusText(res.StatusCode)
	}
	if res.StatusCode/100 == 3 {
		region := res.Header.Get("x-amz-bucket-region")
		if region == "" && ok {
			region = x.Region
		}
		msg = "kho chuyển hướng, không đi theo: sai endpoint hoặc sai vùng"
		if region != "" {
			msg += " (kho nói bucket ở vùng " + region + ")"
		}
	}
	return c.newError(k.op, res.StatusCode, code, msg, k.sig)
}

// newError làm sạch Code và Message (bỏ access key, secret key, chữ ký của
// request vừa gửi, ký tự điều khiển; cắt độ dài) rồi dựng *Error.
func (c *Client) newError(op string, status int, code, msg, sig string) *Error {
	sec := []string{c.accessKey, c.secretKey.Reveal(), sig}
	return &Error{Op: op, Status: status, Code: CleanMessage(code, sec...), Message: CleanMessage(msg, sec...)}
}

// retryable: chỉ lỗi mạng, hết giờ, 5xx và 429; không bao giờ khi người gọi đã huỷ.
func retryable(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	var e *Error
	if !errors.As(err, &e) {
		return false // lỗi cục bộ (đọc nguồn, ghi đích): thử lại không giúp gì
	}
	return e.Status == 0 || e.Status >= 500 || e.Status == 429
}

// do chạy một request với luật thử lại. handle (có thể nil) đọc câu trả lời
// 2xx; lỗi nó trả cũng đi qua luật thử lại.
func (c *Client) do(ctx context.Context, r request, handle func(*call) error) error {
	for try := 0; ; try++ {
		err := c.once(ctx, r, handle)
		if err != nil && ctx.Err() != nil && !errors.Is(err, ctx.Err()) {
			// Người gọi đã huỷ thì kết quả là "đã huỷ", dù lần thử cuối nhận
			// gì: core không được tính đó là lỗi của đích hay của bản.
			return &Error{Op: r.op, Err: ctx.Err()}
		}
		if err == nil || try >= len(c.backoff) || !retryable(ctx, err) || (r.canRetry != nil && !r.canRetry()) {
			return err
		}
		t := time.NewTimer(c.backoff[try])
		select {
		case <-ctx.Done():
			t.Stop()
			return &Error{Op: r.op, Err: ctx.Err()}
		case <-t.C:
		}
	}
}

func (c *Client) once(ctx context.Context, r request, handle func(*call) error) error {
	k, err := c.attempt(ctx, r)
	if err != nil {
		return err
	}
	defer k.close()
	if handle == nil {
		return nil
	}
	return handle(k)
}

// ---------------------------------------------------------------- Thao tác

type listResult struct {
	IsTruncated           bool
	NextContinuationToken string
	Contents              []struct {
		Key          string
		Size         int64
		LastModified string
		ETag         string
	}
}

// Check thử đích: ListObjectsV2 một kết quả dưới tiền tố (cần đúng quyền mà
// sao lưu cần, không đòi quyền trên cả bucket).
func (c *Client) Check(ctx context.Context) error {
	q := url.Values{"list-type": {"2"}, "max-keys": {"1"}, "prefix": {c.prefix + "/"}}
	return c.do(ctx, request{op: "check", method: http.MethodGet, query: q}, func(k *call) error {
		var out listResult
		return k.decode(&out)
	})
}

// Get tải object theo dòng vào w, trả số byte đã ghi và sha256 của chúng.
// Chỉ thử lại khi chưa ghi byte thân nào vào w: đã ghi rồi mà đứt thì trả
// lỗi, người gọi làm lại từ đầu (S19), không bao giờ nối hai lần tải vào một w.
func (c *Client) Get(ctx context.Context, key string, w io.Writer) (int64, string, error) {
	if err := CheckKey(key); err != nil {
		return 0, "", err
	}
	var n int64
	var sum string
	r := request{op: "get", method: http.MethodGet, key: c.full(key), canRetry: func() bool { return n == 0 }}
	err := c.do(ctx, r, func(k *call) error {
		h := sha256.New()
		buf := make([]byte, 32<<10)
		for {
			m, rerr := k.Read(buf)
			if m > 0 {
				h.Write(buf[:m])
				wn, werr := w.Write(buf[:m])
				n += int64(wn)
				if werr == nil && wn < m {
					werr = io.ErrShortWrite
				}
				if werr != nil {
					// Lỗi của phía ghi (đĩa đầy...), không phải của kho.
					return fmt.Errorf("s3 get %s: ghi: %w", key, werr)
				}
			}
			if rerr == io.EOF {
				break
			}
			if rerr != nil {
				return k.netErr(rerr)
			}
		}
		if cl := k.res.ContentLength; cl >= 0 && n != cl {
			return &Error{Op: "get", Err: fmt.Errorf("nhận %d byte, kho báo %d", n, cl)}
		}
		sum = hex.EncodeToString(h.Sum(nil))
		return nil
	})
	if err != nil {
		return n, "", err
	}
	return n, sum, nil
}

// Head trả cỡ, lúc sửa, ETag. Không có object: lỗi thoả errors.Is(err, ErrNotFound).
func (c *Client) Head(ctx context.Context, key string) (Object, error) {
	if err := CheckKey(key); err != nil {
		return Object{}, err
	}
	var o Object
	err := c.do(ctx, request{op: "head", method: http.MethodHead, key: c.full(key)}, func(k *call) error {
		o = Object{Key: key, Size: k.res.ContentLength, ETag: k.res.Header.Get("ETag")}
		if t, err := http.ParseTime(k.res.Header.Get("Last-Modified")); err == nil {
			o.Modified = t
		}
		return nil
	})
	if err != nil {
		return Object{}, err
	}
	return o, nil
}

// Delete xoá object; không có cũng là xong.
func (c *Client) Delete(ctx context.Context, key string) error {
	if err := CheckKey(key); err != nil {
		return err
	}
	err := c.do(ctx, request{op: "delete", method: http.MethodDelete, key: c.full(key)}, nil)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

// List gọi each cho mọi object dưới prefix (tương đối), key đã bỏ tiền tố của
// đích, theo thứ tự kho trả (S3: thứ tự key). Đi hết các trang.
func (c *Client) List(ctx context.Context, prefix string, each func(Object) error) error {
	if err := CheckListPrefix(prefix); err != nil {
		return err
	}
	base := c.prefix + "/"
	token := ""
	for {
		q := url.Values{"list-type": {"2"}, "prefix": {base + prefix}}
		if token != "" {
			q.Set("continuation-token", token)
		}
		var out listResult
		if err := c.do(ctx, request{op: "list", method: http.MethodGet, query: q}, func(k *call) error { return k.decode(&out) }); err != nil {
			return err
		}
		for _, o := range out.Contents {
			rel, ok := strings.CutPrefix(o.Key, base)
			if !ok || rel == "" {
				continue // kho trả key ngoài tiền tố đã hỏi: không nhận
			}
			mod, _ := time.Parse(time.RFC3339Nano, o.LastModified)
			if err := each(Object{Key: rel, Size: o.Size, Modified: mod, ETag: o.ETag}); err != nil {
				return err
			}
		}
		if !out.IsTruncated {
			return nil
		}
		if out.NextContinuationToken == "" || out.NextContinuationToken == token {
			// Không có token mới thì lặp lại sẽ ra đúng trang cũ mãi.
			return &Error{Op: "list", Status: 200, Code: "InvalidResponse", Message: "kho báo còn trang sau nhưng không đưa token mới"}
		}
		token = out.NextContinuationToken
	}
}

type uploadList struct {
	IsTruncated        bool
	NextKeyMarker      string
	NextUploadIDMarker string `xml:"NextUploadIdMarker"`
	Uploads            []struct {
		Key       string
		UploadID  string `xml:"UploadId"`
		Initiated string
	} `xml:"Upload"`
}

// AbortUploads huỷ các lần tải nhiều phần còn dở có key bắt đầu bằng prefix
// (tương đối) và bắt đầu trước now - olderThan (olderThan 0: mọi lần). Trả số
// lần đã huỷ. Kho không hỗ trợ (501, NotImplemented) thì trả 0 và lỗi đó để
// người gọi bỏ qua.
func (c *Client) AbortUploads(ctx context.Context, prefix string, olderThan time.Duration) (int, error) {
	if err := CheckListPrefix(prefix); err != nil {
		return 0, err
	}
	if olderThan < 0 {
		return 0, fmt.Errorf("s3: tuổi âm %s", olderThan)
	}
	base := c.prefix + "/"
	cut := c.now().Add(-olderThan)
	type upload struct{ key, id string }
	var todo []upload
	keyMarker, idMarker := "", ""
	// Liệt kê hết rồi mới huỷ: huỷ giữa lúc phân trang làm dịch các mốc trang.
	for {
		q := url.Values{"uploads": {""}, "prefix": {base + prefix}}
		if keyMarker != "" {
			q.Set("key-marker", keyMarker)
			q.Set("upload-id-marker", idMarker)
		}
		var out uploadList
		if err := c.do(ctx, request{op: "abort", method: http.MethodGet, query: q}, func(k *call) error { return k.decode(&out) }); err != nil {
			return 0, err
		}
		for _, u := range out.Uploads {
			// Key do kho trả phải là key hợp lệ dưới tiền tố, không thì một
			// kho độc hại dẫn được lời huỷ ra ngoài tiền tố (B7).
			rel, ok := strings.CutPrefix(u.Key, base)
			if !ok || CheckKey(rel) != nil || u.UploadID == "" {
				continue
			}
			if olderThan > 0 {
				t, err := time.Parse(time.RFC3339Nano, u.Initiated)
				if err != nil || !t.Before(cut) {
					continue // không biết tuổi thì không huỷ: có thể đang tải
				}
			}
			todo = append(todo, upload{u.Key, u.UploadID})
		}
		if !out.IsTruncated {
			break
		}
		if out.NextKeyMarker == "" || (out.NextKeyMarker == keyMarker && out.NextUploadIDMarker == idMarker) {
			return 0, &Error{Op: "abort", Status: 200, Code: "InvalidResponse", Message: "kho báo còn trang sau nhưng không đưa mốc mới"}
		}
		keyMarker, idMarker = out.NextKeyMarker, out.NextUploadIDMarker
	}
	n := 0
	for _, u := range todo {
		if err := c.abortOne(ctx, u.key, u.id); err != nil {
			var e *Error
			if errors.As(err, &e) && e.Code == "NoSuchUpload" {
				continue // đã xong hoặc đã bị huỷ giữa chừng
			}
			return n, err
		}
		n++
	}
	return n, nil
}

// abortOne gọi AbortMultipartUpload. full là key đầy đủ.
func (c *Client) abortOne(ctx context.Context, full, uploadID string) error {
	q := url.Values{"uploadId": {uploadID}}
	return c.do(ctx, request{op: "abort", method: http.MethodDelete, key: full, query: q}, nil)
}
