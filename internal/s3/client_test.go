package s3

import (
	"bytes"
	"context"
	"encoding/pem"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var _ interface {
	Check(context.Context) error
	Put(context.Context, string, io.Reader, int64, func(int64)) (string, error)
	Get(context.Context, string, io.Writer) (int64, string, error)
	Head(context.Context, string) (Object, error)
	Delete(context.Context, string) error
	List(context.Context, string, func(Object) error) error
	AbortUploads(context.Context, string, time.Duration) (int, error)
} = (*Client)(nil)

func data(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte('a' + i%26)
	}
	return b
}

func asErr(t *testing.T, err error) *Error {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("lỗi %v (%T) không phải *s3.Error", err, err)
	}
	return e
}

func TestNewKiemCauHinh(t *testing.T) {
	good := Config{Endpoint: "https://S3.example.com/", Bucket: "bkt", Prefix: "tpan/may", AccessKey: "AKTEST", SecretKey: secret("12345678")}
	c, err := New(good)
	if err != nil {
		t.Fatal(err)
	}
	if c.host != "s3.example.com" || c.region != DefaultRegion || c.partSize != DefaultPartSize {
		t.Errorf("chuẩn hoá: host %q vùng %q cỡ phần %d", c.host, c.region, c.partSize)
	}
	bad := []func(*Config){
		func(c *Config) { c.Endpoint = "http://s3.example.com" },
		func(c *Config) { c.Region = "Vùng" },
		func(c *Config) { c.Bucket = "A" },
		func(c *Config) { c.Prefix = "../x" },
		func(c *Config) { c.AccessKey = "a b" },
		func(c *Config) { c.SecretKey = secret("ngan") },
		func(c *Config) { c.CAPEM = []byte("không phải PEM") },
		func(c *Config) { c.PartSize = -1 },
	}
	for i, mod := range bad {
		cfg := good
		mod(&cfg)
		if _, err := New(cfg); err == nil {
			t.Errorf("ca %d: New nhận cấu hình sai", i)
		}
	}
}

func TestCoPhanTheoMaxParts(t *testing.T) {
	if got := partSizeFor(DefaultPartSize, 2<<30); got != DefaultPartSize {
		t.Errorf("2 GiB: %d", got)
	}
	big := int64(DefaultPartSize)*MaxParts + 1
	got := partSizeFor(DefaultPartSize, big)
	if got%(1<<20) != 0 || (big+got-1)/got > MaxParts || got <= DefaultPartSize {
		t.Errorf("%d byte: cỡ phần %d", big, got)
	}
}

// B8: mọi lời gọi của client qua được máy chủ giả tính lại chữ ký.
func TestMayChuGiaKiemChuKy(t *testing.T) {
	f := newFakeS3(t)
	c := f.client(t, func(c *Config) { c.PartSize = 7 })
	ctx := t.Context()

	if err := c.Check(ctx); err != nil {
		t.Fatalf("Check: %v", err)
	}
	small, big := []byte("hello"), data(30)
	var last int64
	for key, d := range map[string][]byte{"site/a.tar.gz": small, "site/b-1_2.tar.gz": big} {
		sum, err := c.Put(ctx, key, bytes.NewReader(d), int64(len(d)), func(n int64) { last = n })
		if err != nil {
			t.Fatalf("Put %s: %v", key, err)
		}
		if sum != sha256Hex(d) || last != int64(len(d)) {
			t.Errorf("Put %s: sha %s, progress %d", key, sum, last)
		}
		if !bytes.Equal(f.objects["tpan/may-1/"+key], d) {
			t.Errorf("kho giữ %q cho %s", f.objects["tpan/may-1/"+key], key)
		}
		var buf bytes.Buffer
		n, gsum, err := c.Get(ctx, key, &buf)
		if err != nil || n != int64(len(d)) || gsum != sum || !bytes.Equal(buf.Bytes(), d) {
			t.Errorf("Get %s: %d %s %v", key, n, gsum, err)
		}
		o, err := c.Head(ctx, key)
		if err != nil || o.Size != int64(len(d)) || o.Key != key || o.ETag == "" || o.Modified.IsZero() {
			t.Errorf("Head %s: %+v %v", key, o, err)
		}
	}
	if f.seen("POST /bkt/tpan/may-1/site/b-1_2.tar.gz?uploads") != 1 || f.seen("PUT /bkt/tpan/may-1/site/b-1_2.tar.gz?partNumber=5") != 1 {
		t.Errorf("không thấy tải nhiều phần 5 phần: %v", f.log)
	}
	if len(f.uploads) != 0 {
		t.Errorf("còn %d lần tải dở", len(f.uploads))
	}

	// Key có dấu cách, "+", "~", "$", "&", chữ có dấu: CheckKey không cho
	// người gọi đưa vào, nhưng object do người khác đặt vẫn có thể có tên như
	// vậy, và đây là chỗ dễ ký sai nhất. Gọi thẳng đường request nội bộ.
	odd := c.full("thư mục/a b+c~d$e&f (1).txt")
	if err := c.do(ctx, request{op: "put", method: http.MethodPut, key: odd, body: []byte("x")}, nil); err != nil {
		t.Fatalf("PUT key lạ: %v", err)
	}
	if string(f.objects[odd]) != "x" {
		t.Errorf("kho không có %q: %v", odd, f.log)
	}
	if err := c.do(ctx, request{op: "head", method: http.MethodHead, key: odd}, nil); err != nil {
		t.Errorf("HEAD key lạ: %v", err)
	}

	var keys []string
	if err := c.List(ctx, "", func(o Object) error { keys = append(keys, o.Key); return nil }); err != nil {
		t.Fatal(err)
	}
	if want := "site/a.tar.gz,site/b-1_2.tar.gz,thư mục/a b+c~d$e&f (1).txt"; strings.Join(keys, ",") != want {
		t.Errorf("List: %q", keys)
	}
	keys = nil
	if err := c.List(ctx, "site/", func(o Object) error { keys = append(keys, o.Key); return nil }); err != nil || len(keys) != 2 {
		t.Errorf("List site/: %q %v", keys, err)
	}

	if err := c.Delete(ctx, "site/a.tar.gz"); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(ctx, "site/a.tar.gz"); err != nil {
		t.Errorf("xoá lần hai: %v", err)
	}
	if _, err := c.Head(ctx, "site/a.tar.gz"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Head sau xoá: %v", err)
	}
	if _, _, err := c.Get(ctx, "site/a.tar.gz", io.Discard); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get sau xoá: %v", err)
	}
	if f.badSig != 0 {
		t.Errorf("%d request ký sai", f.badSig)
	}

	// Máy chủ giả thật sự kiểm: sai secret thì bị từ chối.
	f2 := newFakeS3(t)
	f2.quiet = true // lỗi chữ ký ở đây là cố ý
	wrong := f2.client(t, func(c *Config) { c.SecretKey = secret("khoa-sai-0123456789") })
	if err := wrong.Check(ctx); Classify(err) != CodeAuthFailed {
		t.Errorf("sai secret: %v", err)
	}
}

// B7: key và prefix sai luật bị chặn trước khi gọi mạng.
func TestKeyKhongRaNgoaiTienTo(t *testing.T) {
	f := newFakeS3(t)
	c := f.client(t, nil)
	ctx := t.Context()
	for _, k := range []string{"..", "a/../b", "./a", "/a", "", "a/", "a//b", "../x", "a/..", "a b", "x\n"} {
		if _, err := c.Put(ctx, k, strings.NewReader("x"), 1, nil); err == nil {
			t.Errorf("Put %q", k)
		}
		if _, _, err := c.Get(ctx, k, io.Discard); err == nil {
			t.Errorf("Get %q", k)
		}
		if _, err := c.Head(ctx, k); err == nil {
			t.Errorf("Head %q", k)
		}
		if err := c.Delete(ctx, k); err == nil {
			t.Errorf("Delete %q", k)
		}
	}
	for _, p := range []string{"..", "../", "/a", "a/../b", "./", "a//", "a b/"} {
		if err := c.List(ctx, p, func(Object) error { return nil }); err == nil {
			t.Errorf("List %q", p)
		}
		if _, err := c.AbortUploads(ctx, p, 0); err == nil {
			t.Errorf("AbortUploads %q", p)
		}
	}
	if n := f.count(); n != 0 {
		t.Errorf("máy chủ nhận %d request, mong 0: %v", n, f.log)
	}
}

// AbortUploads không huỷ lần tải dở mà kho báo key ngoài tiền tố (kho độc hại).
func TestAbortUploadsBoQuaKeyLa(t *testing.T) {
	f := newFakeS3(t)
	c := f.client(t, nil)
	f.uploads["u1"] = &fakeUpload{key: "tpan/may-1/../may-2/x", initiated: time.Now()}
	f.uploads["u2"] = &fakeUpload{key: "tpan/may-1/a", initiated: time.Now()}
	n, err := c.AbortUploads(t.Context(), "", 0)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if _, ok := f.uploads["u1"]; !ok {
		t.Error("đã huỷ lần tải có key ra ngoài tiền tố")
	}
}

// B9: phần thứ 3 lỗi 503 mãi: thử lại đủ 4 lần rồi huỷ lần tải dở.
func TestPutLoiThiHuy(t *testing.T) {
	f := newFakeS3(t)
	c := f.client(t, func(c *Config) { c.PartSize = 4 })
	var part3 atomic.Int32
	f.hook = func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method == http.MethodPut && r.URL.Query().Get("partNumber") == "3" {
			part3.Add(1)
			writeErr(w, 503, "SlowDown", "chậm lại")
			return true
		}
		return false
	}
	_, err := c.Put(t.Context(), "site/x.tar.gz", bytes.NewReader(data(20)), 20, nil)
	e := asErr(t, err)
	if e.Op != "part" || e.Status != 503 || e.Code != "SlowDown" || Classify(err) != CodeUnreachable {
		t.Errorf("lỗi %v", err)
	}
	if part3.Load() != 4 {
		t.Errorf("phần 3 gửi %d lần, mong 4", part3.Load())
	}
	if f.seen("DELETE /bkt/tpan/may-1/site/x.tar.gz?uploadId=") != 1 || len(f.uploads) != 0 {
		t.Errorf("không huỷ lần tải dở: %v", f.log)
	}
	if f.seen("PUT /bkt/tpan/may-1/site/x.tar.gz?partNumber=4") != 0 || len(f.objects) != 0 {
		t.Error("vẫn tải tiếp sau phần lỗi")
	}
}

// Người gọi huỷ giữa chừng: lỗi là CodeCanceled, lần tải dở vẫn được huỷ
// bằng context riêng.
func TestPutBiHuyVanDon(t *testing.T) {
	f := newFakeS3(t)
	c := f.client(t, func(c *Config) { c.PartSize = 4 })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f.hook = func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Query().Get("partNumber") == "2" {
			cancel()
			writeErr(w, 503, "SlowDown", "")
			return true
		}
		return false
	}
	_, err := c.Put(ctx, "site/x.tar.gz", bytes.NewReader(data(20)), 20, nil)
	if Classify(err) != CodeCanceled || !errors.Is(err, context.Canceled) {
		t.Errorf("lỗi %v, mã %s", err, Classify(err))
	}
	if len(f.uploads) != 0 {
		t.Errorf("còn lần tải dở: %v", f.log)
	}
}

// Bước ghép trả 200 kèm <Error>: là lỗi, và lần tải dở bị huỷ.
func TestGhep200CoErrorLaLoi(t *testing.T) {
	f := newFakeS3(t)
	f.completeErr = true
	c := f.client(t, func(c *Config) { c.PartSize = 4 })
	_, err := c.Put(t.Context(), "site/x.tar.gz", bytes.NewReader(data(10)), 10, nil)
	e := asErr(t, err)
	if e.Op != "complete" || e.Status != 200 || e.Code != "InternalError" {
		t.Errorf("lỗi %v", err)
	}
	if len(f.uploads) != 0 || len(f.objects) != 0 {
		t.Errorf("còn lại: %v", f.log)
	}
}

// S19: bước ghép đứt mạng sau khi kho đã ghép xong: Head thấy đúng cỡ là đạt.
func TestGhepLoiMangMaObjectDaCo(t *testing.T) {
	f := newFakeS3(t)
	f.dropComplete = true
	c := f.client(t, func(c *Config) { c.PartSize = 4 })
	d := data(10)
	sum, err := c.Put(t.Context(), "site/x.tar.gz", bytes.NewReader(d), 10, nil)
	if err != nil || sum != sha256Hex(d) {
		t.Fatalf("Put: %s %v (%v)", sum, err, f.log)
	}
	if f.seen("HEAD /bkt/tpan/may-1/site/x.tar.gz") != 1 || f.seen("DELETE ") != 0 {
		t.Errorf("không đi đường Head, hoặc lại huỷ: %v", f.log)
	}
}

// Ghép đứt mạng và object không có (hoặc sai cỡ): lỗi, và huỷ.
func TestGhepLoiMangObjectSaiCo(t *testing.T) {
	f := newFakeS3(t)
	c := f.client(t, func(c *Config) { c.PartSize = 4 })
	f.hook = func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method == http.MethodPost && r.URL.Query().Has("uploadId") {
			hijackClose(w)
			return true
		}
		return false
	}
	_, err := c.Put(t.Context(), "site/x.tar.gz", bytes.NewReader(data(10)), 10, nil)
	if e := asErr(t, err); e.Op != "complete" || e.Status != 0 || Classify(err) != CodeUnreachable {
		t.Errorf("lỗi %v", err)
	}
	if f.seen("HEAD ") != 1 || len(f.uploads) != 0 {
		t.Errorf("%v", f.log)
	}
}

func TestThuLaiKhi503RoiDat(t *testing.T) {
	f := newFakeS3(t)
	c := f.client(t, nil)
	f.objects["tpan/may-1/a"] = []byte("abc")
	var n atomic.Int32
	f.hook = func(w http.ResponseWriter, r *http.Request) bool {
		if n.Add(1) <= 2 {
			writeErr(w, 503, "SlowDown", "")
			return true
		}
		return false
	}
	var buf bytes.Buffer
	if _, _, err := c.Get(t.Context(), "a", &buf); err != nil || buf.String() != "abc" {
		t.Fatalf("Get: %v %q", err, buf.String())
	}
	if n.Load() != 3 {
		t.Errorf("%d lần gửi, mong 3", n.Load())
	}
}

func TestThuLaiHetLuot(t *testing.T) {
	f := newFakeS3(t)
	c := f.client(t, nil)
	f.hook = func(w http.ResponseWriter, r *http.Request) bool { writeErr(w, 429, "SlowDown", ""); return true }
	err := c.Check(t.Context())
	if asErr(t, err).Status != 429 || Classify(err) != CodeUnreachable || f.count() != 4 {
		t.Errorf("lỗi %v sau %d lần", err, f.count())
	}
}

func TestLoi400KhongThuLai(t *testing.T) {
	f := newFakeS3(t)
	c := f.client(t, nil)
	f.hook = func(w http.ResponseWriter, r *http.Request) bool {
		writeErr(w, 400, "InvalidArgument", "sai")
		return true
	}
	_, err := c.Put(t.Context(), "a", strings.NewReader("x"), 1, nil)
	if e := asErr(t, err); e.Status != 400 || e.Code != "InvalidArgument" || e.Message != "sai" || e.Op != "put" {
		t.Errorf("lỗi %v", err)
	}
	if f.count() != 1 {
		t.Errorf("%d lần gửi, mong 1", f.count())
	}
}

// S19: Get đứt sau byte đầu thì lỗi, không thử lại, không ghi thêm.
func TestGetDutSauByteDau(t *testing.T) {
	f := newFakeS3(t)
	c := f.client(t, nil)
	f.hook = func(w http.ResponseWriter, r *http.Request) bool {
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(200)
		_, _ = w.Write(data(10))
		hijackClose(w)
		return true
	}
	var buf bytes.Buffer
	n, sum, err := c.Get(t.Context(), "a", &buf)
	if err == nil || sum != "" || n != 10 || buf.Len() != 10 {
		t.Errorf("n=%d sum=%q len=%d err=%v", n, sum, buf.Len(), err)
	}
	if e := asErr(t, err); e.Status != 0 || Classify(err) != CodeUnreachable {
		t.Errorf("lỗi %v", err)
	}
	if f.count() != 1 {
		t.Errorf("%d lần gửi, mong 1", f.count())
	}
}

// Đứt trước byte thân đầu tiên thì vẫn thử lại.
func TestGetDutTruocByteDauThiThuLai(t *testing.T) {
	f := newFakeS3(t)
	c := f.client(t, nil)
	f.objects["tpan/may-1/a"] = []byte("abc")
	var n atomic.Int32
	f.hook = func(w http.ResponseWriter, r *http.Request) bool {
		if n.Add(1) == 1 {
			hijackClose(w)
			return true
		}
		return false
	}
	var buf bytes.Buffer
	if _, _, err := c.Get(t.Context(), "a", &buf); err != nil || buf.String() != "abc" {
		t.Errorf("Get: %v %q", err, buf.String())
	}
}

// Kho chuyển hướng sang host khác: không đi theo, lỗi nói sai endpoint/vùng.
func TestKhongDiTheoChuyenHuong(t *testing.T) {
	var other atomic.Int32
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { other.Add(1) }))
	defer evil.Close()
	f := newFakeS3(t)
	c := f.client(t, nil)
	f.hook = func(w http.ResponseWriter, r *http.Request) bool {
		w.Header().Set("Location", evil.URL+r.URL.Path)
		w.Header().Set("x-amz-bucket-region", "eu-west-1")
		w.WriteHeader(http.StatusTemporaryRedirect)
		return true
	}
	ctx := t.Context()
	errs := []error{c.Check(ctx)}
	_, err := c.Put(ctx, "a", strings.NewReader("x"), 1, nil)
	errs = append(errs, err)
	_, err = c.Head(ctx, "a")
	errs = append(errs, err)
	for _, err := range errs {
		e := asErr(t, err)
		if e.Status != 307 || !strings.Contains(e.Message, "eu-west-1") || !strings.Contains(e.Message, "sai endpoint hoặc sai vùng") || Classify(err) != CodeError {
			t.Errorf("lỗi %v", err)
		}
	}
	if other.Load() != 0 {
		t.Errorf("host khác nhận %d request", other.Load())
	}
}

func TestLoiDuocLamSach(t *testing.T) {
	f := newFakeS3(t)
	c := f.client(t, nil)
	ctx := t.Context()
	cases := []struct {
		name     string
		status   int
		body     string
		code     string
		checkMsg func(string) bool
	}{
		{"ký tự điều khiển, dài", 400, "<Error><Code>Bad\u0085Code</Code><Message>dòng 1\r\ndòng 2\t\u009b31m" + strings.Repeat("x", 2000) + "</Message></Error>", "Bad Code",
			func(m string) bool {
				return !strings.ContainsAny(m, "\r\n\t\u009b") && len([]rune(m)) <= MaxMessage+1 && strings.HasPrefix(m, "dòng 1 dòng 2  31m")
			}},
		{"thân XML quá 64 KB", 400, "<Error><Code>X</Code><Message>" + strings.Repeat("y", 70<<10) + "</Message></Error>", "Bad Request",
			func(m string) bool { return m == "" }},
		{"thân chữ, không XML", 403, "Forbidden: tên đăng nhập nội bộ của proxy", "Forbidden",
			func(m string) bool { return m == "" }},
		{"XML không phải Error", 500, "<html><body>lỗi</body></html>", "Internal Server Error",
			func(m string) bool { return m == "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f.hook = func(w http.ResponseWriter, r *http.Request) bool {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
				return true
			}
			_, err := c.Put(ctx, "a", strings.NewReader("x"), 1, nil)
			e := asErr(t, err)
			if e.Status != tc.status || e.Code != tc.code || !tc.checkMsg(e.Message) {
				t.Errorf("Status %d Code %q Message %q", e.Status, e.Code, e.Message)
			}
		})
	}
}

// B3, S11: kho chép lại header Authorization, access key, secret key vào câu lỗi.
func TestLoiKhongCoKhoa(t *testing.T) {
	f := newFakeS3(t)
	c := f.client(t, nil)
	var mu sync.Mutex
	var sigs []string
	f.hook = func(w http.ResponseWriter, r *http.Request) bool {
		auth := r.Header.Get("Authorization")
		mu.Lock()
		sigs = append(sigs, auth[strings.Index(auth, "Signature=")+len("Signature="):])
		mu.Unlock()
		writeErr(w, 403, "Denied-"+f.access, "No such key: "+f.access+"; auth "+auth+"; sk "+f.secret)
		return true
	}
	ctx := t.Context()
	var errs []error
	_, err := c.Put(ctx, "a", strings.NewReader("x"), 1, nil)
	errs = append(errs, err)
	_, err = c.Head(ctx, "a")
	errs = append(errs, err)
	errs = append(errs, c.List(ctx, "", func(Object) error { return nil }))
	_, err = c.AbortUploads(ctx, "", 0)
	errs = append(errs, err)
	for _, err := range errs {
		if err == nil {
			t.Fatal("không có lỗi")
		}
		s := err.Error()
		bad := append([]string{f.access, f.secret}, sigs...)
		for _, b := range bad {
			if strings.Contains(s, b) {
				t.Errorf("câu lỗi lộ %q: %s", b, s)
			}
		}
		if Classify(err) != CodeAuthFailed {
			t.Errorf("mã %s", Classify(err))
		}
	}
	if e := asErr(t, errs[0]); !strings.Contains(e.Message, "••••") || e.Code != "Denied-••••" {
		t.Errorf("không thay bằng ••••: %q %q", e.Code, e.Message)
	}
	if len(sigs) == 0 {
		t.Fatal("hook không chạy")
	}
}

func TestListPhanTrang(t *testing.T) {
	f := newFakeS3(t)
	f.maxKeys = 2
	c := f.client(t, nil)
	for _, k := range []string{"s/1", "s/2", "s/3", "s/4", "s/5", "t/1"} {
		f.objects["tpan/may-1/"+k] = []byte(k)
	}
	f.objects["tpan/may-10/s/1"] = []byte("máy khác, chung đầu chuỗi")
	var keys []string
	err := c.List(t.Context(), "s/", func(o Object) error {
		keys = append(keys, o.Key)
		if o.Size != 3 || o.Modified.IsZero() {
			t.Errorf("%+v", o)
		}
		return nil
	})
	if err != nil || strings.Join(keys, ",") != "s/1,s/2,s/3,s/4,s/5" {
		t.Errorf("%q %v", keys, err)
	}
	if n := f.seen("GET /bkt?"); n != 3 {
		t.Errorf("%d trang, mong 3", n)
	}
	keys = nil
	if err := c.List(t.Context(), "", func(o Object) error { keys = append(keys, o.Key); return nil }); err != nil || len(keys) != 6 {
		t.Errorf("List rỗng: %q %v", keys, err)
	}
	stop := errors.New("dừng")
	if err := c.List(t.Context(), "", func(Object) error { return stop }); !errors.Is(err, stop) {
		t.Errorf("lỗi của each: %v", err)
	}
}

func TestAbortUploadsTheoTuoi(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	f := newFakeS3(t)
	f.maxUploads = 1
	c := f.client(t, func(c *Config) { c.Now = func() time.Time { return now } })
	add := func(id, key string, age time.Duration) {
		f.uploads[id] = &fakeUpload{key: key, initiated: now.Add(-age), parts: map[int][]byte{}}
	}
	add("u1", "tpan/may-1/site/a", 48*time.Hour)
	add("u2", "tpan/may-1/site/b", time.Hour)
	add("u3", "tpan/may-1/khac/c", 48*time.Hour)
	add("u4", "tpan/may-2/site/d", 48*time.Hour)
	add("u5", "tpan/may-1/site/a", 30*time.Hour) // hai lần tải dở cùng key

	n, err := c.AbortUploads(t.Context(), "site/", 24*time.Hour)
	if err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	for _, id := range []string{"u2", "u3", "u4"} {
		if _, ok := f.uploads[id]; !ok {
			t.Errorf("%s bị huỷ oan", id)
		}
	}
	n, err = c.AbortUploads(t.Context(), "", 0)
	if err != nil || n != 2 || len(f.uploads) != 1 || f.uploads["u4"] == nil {
		t.Errorf("n=%d err=%v còn %d", n, err, len(f.uploads))
	}
}

func TestAbortUploadsKhoKhongHoTro(t *testing.T) {
	f := newFakeS3(t)
	c := f.client(t, nil)
	f.hook = func(w http.ResponseWriter, r *http.Request) bool {
		writeErr(w, 501, "NotImplemented", "không hỗ trợ")
		return true
	}
	n, err := c.AbortUploads(t.Context(), "", 0)
	if n != 0 || asErr(t, err).Status != 501 || asErr(t, err).Code != "NotImplemented" {
		t.Errorf("n=%d err=%v", n, err)
	}
}

// Kiểu bucket.host: bucket nằm trong Host (đã ký), đường chỉ còn key.
func TestKieuBucketHost(t *testing.T) {
	f := newFakeS3(t)
	addr := strings.TrimPrefix(f.srv.URL, "http://")
	_, port, _ := net.SplitHostPort(addr)
	f.vhost = "bkt.localhost:" + port
	c := f.client(t, func(c *Config) { c.Endpoint = "http://localhost:" + port; c.PathStyle = false; c.PartSize = 4 })
	// bkt.localhost không chắc phân giải được trên mọi máy: nối thẳng vào máy chủ giả.
	tr := c.http.Transport.(*http.Transport)
	tr.Proxy = nil
	tr.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	ctx := t.Context()
	d := data(10)
	if _, err := c.Put(ctx, "site/x", bytes.NewReader(d), 10, nil); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(f.objects["tpan/may-1/site/x"], d) {
		t.Errorf("kho: %v", f.log)
	}
	var buf bytes.Buffer
	if _, _, err := c.Get(ctx, "site/x", &buf); err != nil || !bytes.Equal(buf.Bytes(), d) {
		t.Errorf("Get: %v", err)
	}
	if err := c.List(ctx, "", func(Object) error { return nil }); err != nil {
		t.Errorf("List: %v", err)
	}
	if f.seen("GET /?list-type=2") != 1 {
		t.Errorf("đường của List: %v", f.log)
	}
}

func TestTLSVoiCARieng(t *testing.T) {
	f := newFakeS3NoServer(t)
	f.srv = httptest.NewUnstartedServer(f)
	f.srv.Config.ErrorLog = log.New(io.Discard, "", 0) // lỗi bắt tay của ca "không CA" là cố ý
	f.srv.StartTLS()
	t.Cleanup(f.srv.Close)
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.srv.Certificate().Raw})
	c := f.client(t, func(c *Config) { c.CAPEM = ca })
	if c.http.Transport.(*http.Transport).TLSClientConfig.MinVersion < 0x0303 {
		t.Error("TLS dưới 1.2")
	}
	ctx := t.Context()
	if _, err := c.Put(ctx, "a", strings.NewReader("abc"), 3, nil); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, _, err := c.Get(ctx, "a", &buf); err != nil || buf.String() != "abc" {
		t.Errorf("Get: %v", err)
	}
	// Không có CA riêng: chứng chỉ tự ký bị từ chối.
	c2 := f.client(t, nil)
	err := c2.Check(ctx)
	if e := asErr(t, err); e.Status != 0 || Classify(err) != CodeUnreachable {
		t.Errorf("không CA: %v", err)
	}
}

// Hết hạn chờ header: lỗi Status 0 không bọc context.Canceled (là lỗi của
// đích, không phải người gọi huỷ), đã thử lại.
func TestHetGioChoHeader(t *testing.T) {
	f := newFakeS3(t)
	c := f.client(t, nil)
	c.headerTimeout = 50 * time.Millisecond
	f.hook = func(w http.ResponseWriter, r *http.Request) bool {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
		return true
	}
	_, err := c.Head(t.Context(), "a")
	e := asErr(t, err)
	if e.Status != 0 || !errors.Is(err, errStalled) || errors.Is(err, context.Canceled) || Classify(err) != CodeUnreachable {
		t.Errorf("lỗi %v", err)
	}
	if f.count() != 4 {
		t.Errorf("%d lần gửi, mong 4", f.count())
	}
}

// Bước ghép có hạn chờ header riêng, dài hơn hạn thường.
func TestGhepCoHanRieng(t *testing.T) {
	f := newFakeS3(t)
	c := f.client(t, func(c *Config) { c.PartSize = 4 })
	// Hạn im lặng cũng nhỏ: thân của bước ghép gửi xong thì đồng hồ phải
	// chuyển sang hạn chờ header của bước ghép, không giữ hạn im lặng.
	c.headerTimeout, c.idleTimeout = 50*time.Millisecond, 50*time.Millisecond
	c.completeTimeout = 5 * time.Second
	f.hook = func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method == http.MethodPost && r.URL.Query().Has("uploadId") {
			time.Sleep(200 * time.Millisecond)
		}
		return false
	}
	if _, err := c.Put(t.Context(), "a", bytes.NewReader(data(10)), 10, nil); err != nil {
		t.Fatal(err)
	}
	if f.seen("POST /bkt/tpan/may-1/a?uploadId") != 1 || f.seen("HEAD ") != 0 {
		t.Errorf("%v", f.log)
	}
}

// Thân đang về rồi im lặng quá hạn: huỷ lần thử; đã ghi byte nên không thử lại.
func TestHetGioImLang(t *testing.T) {
	f := newFakeS3(t)
	c := f.client(t, nil)
	c.idleTimeout = 50 * time.Millisecond
	f.hook = func(w http.ResponseWriter, r *http.Request) bool {
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(200)
		_, _ = w.Write(data(5))
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
		return true
	}
	start := time.Now()
	n, _, err := c.Get(t.Context(), "a", io.Discard)
	if n != 5 || !errors.Is(err, errStalled) || Classify(err) != CodeUnreachable {
		t.Errorf("n=%d err=%v", n, err)
	}
	if time.Since(start) > time.Second {
		t.Errorf("chờ %s, hạn im lặng không có tác dụng", time.Since(start))
	}
}

func TestNguoiGoiHuy(t *testing.T) {
	f := newFakeS3(t)
	c := f.client(t, nil)
	ctx, cancel := context.WithCancel(t.Context())
	f.hook = func(w http.ResponseWriter, r *http.Request) bool {
		cancel()
		<-r.Context().Done()
		return true
	}
	_, err := c.Head(ctx, "a")
	if e := asErr(t, err); e.Status != 0 || Classify(err) != CodeCanceled {
		t.Errorf("lỗi %v, mã %s", err, Classify(err))
	}
	if f.count() != 1 {
		t.Errorf("%d lần gửi sau khi huỷ", f.count())
	}
	if _, err := c.Head(ctx, "a"); Classify(err) != CodeCanceled || f.count() != 1 {
		t.Errorf("ctx đã huỷ vẫn gọi mạng: %v", err)
	}
}

// Nguồn ngắn hơn size: lỗi cục bộ, không phải lỗi của đích.
func TestPutNguonThieu(t *testing.T) {
	f := newFakeS3(t)
	c := f.client(t, func(c *Config) { c.PartSize = 4 })
	for _, size := range []int64{3, 10} {
		_, err := c.Put(t.Context(), "a", strings.NewReader("ab"), size, nil)
		if err == nil || IsTargetError(err) {
			t.Errorf("size %d: %v", size, err)
		}
	}
	if len(f.uploads) != 0 || len(f.objects) != 0 {
		t.Errorf("còn lại: %v", f.log)
	}
	if _, err := c.Put(t.Context(), "a", strings.NewReader(""), 0, nil); err != nil || f.objects["tpan/may-1/a"] == nil {
		t.Errorf("object rỗng: %v", err)
	}
}
