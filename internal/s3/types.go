// Package s3 là client cho kho object tương thích S3 (M18, mục 3.4 của
// docs/thay-doi/M18-thiet-ke.md): tự ký SigV4 bằng thư viện chuẩn, không phụ
// thuộc nhà cung cấp nào (endpoint, vùng tuỳ ý; kiểu địa chỉ path-style mặc định).
//
// File này (đợt 0, hợp đồng) giữ kiểu chung, luật kiểm đầu vào, phân loại lỗi
// và mã của đích. mem.go là kho giả trong bộ nhớ cho test. Client thật (thợ
// đợt 1) nằm ở các file khác của gói và phải có:
//
//	// New kiểm cfg (CheckEndpoint, CheckRegion, CheckBucket, CheckPrefix,
//	// CheckAccessKey, CheckSecretKey, CheckCA) và dựng client.
//	func New(cfg Config) (*Client, error)
//	func (c *Client) Check(ctx context.Context) error
//	func (c *Client) Put(ctx context.Context, key string, r io.Reader, size int64, progress func(done int64)) (sha256Hex string, err error)
//	func (c *Client) Get(ctx context.Context, key string, w io.Writer) (n int64, sha256Hex string, err error)
//	func (c *Client) Head(ctx context.Context, key string) (Object, error)
//	func (c *Client) Delete(ctx context.Context, key string) error
//	func (c *Client) List(ctx context.Context, prefix string, each func(Object) error) error
//	func (c *Client) AbortUploads(ctx context.Context, prefix string, olderThan time.Duration) (int, error)
//
// Mọi key và prefix người gọi đưa là TƯƠNG ĐỐI với Config.Prefix; client tự
// ghép, và List trả key đã bỏ tiền tố. Luật của key: CheckKey; của tham số
// prefix: CheckListPrefix. Sai luật thì lỗi trước khi gọi mạng.
package s3

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/system"
)

// Hằng số mặc định (mục 3.3, 3.4).
const (
	DefaultRegion   = "us-east-1"
	DefaultKeep     = 7
	MaxKeep         = 365
	DefaultPartSize = 8 << 20 // 8 MiB: đỉnh RAM khoảng 24 MB khi tải 2 GiB (bản thử 07)
	MaxParts        = 9000    // dưới trần 10000 phần của S3, chừa chỗ
	MaxCABytes      = 64 << 10
	// MaxMessage là độ dài tối đa (ký tự) của Error.Message sau khi làm sạch.
	MaxMessage = 300
)

// Config là cấu hình của một client. SecretKey là system.Secret: in ra log
// hay JSON chỉ thấy "••••".
type Config struct {
	Endpoint  string // https://host[:cổng], đã qua CheckEndpoint
	Region    string
	Bucket    string
	Prefix    string
	PathStyle bool // true: https://host/bucket/key (mặc định); false: https://bucket.host/key
	CAPEM     []byte
	AccessKey string
	SecretKey system.Secret
	PartSize  int64            // 0: DefaultPartSize; test đặt nhỏ
	Now       func() time.Time // nil: time.Now
}

// Object là một object trên kho, key đã bỏ tiền tố của đích.
type Object struct {
	Key      string    `json:"key"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	ETag     string    `json:"etag,omitempty"`
}

// ---------------------------------------------------------------- Lỗi

// Mã lỗi (cũng là mã từ chối của core, mục 3.14).
const (
	CodeNotConfigured   = "s3_not_configured"
	CodeEndpointInvalid = "s3_endpoint_invalid"
	CodeRegionInvalid   = "s3_region_invalid"
	CodeBucketInvalid   = "s3_bucket_invalid"
	CodePrefixInvalid   = "s3_prefix_invalid"
	CodeKeysInvalid     = "s3_keys_invalid"
	CodeKeepInvalid     = "s3_keep_invalid"
	CodeCAInvalid       = "s3_ca_invalid"
	CodeUnreachable     = "s3_unreachable"
	CodeAuthFailed      = "s3_auth_failed"
	CodeBucketNotFound  = "s3_bucket_not_found"
	CodeError           = "s3_error"
	// CodeCanceled: người gọi huỷ (ctx). Không phải lỗi của kho, không phải
	// lỗi của bản; core không tăng remote_tries, không ngắt cầu dao.
	CodeCanceled = "s3_canceled"
)

// ErrNotFound: object không có (Head, Get). Dùng errors.Is.
var ErrNotFound = errors.New("s3: không có object này")

// Error là lỗi của một lời gọi tới kho. Code và Message ĐÃ LÀM SẠCH bằng
// CleanMessage (không khoá, không chữ ký, không ký tự điều khiển, tối đa
// MaxMessage ký tự): in ra log, job, API được.
type Error struct {
	Op      string // "check", "put", "part", "complete", "get", "head", "delete", "list", "abort"
	Status  int    // mã HTTP; 0 khi không có câu trả lời (lỗi mạng, hết giờ)
	Code    string // mã lỗi của kho (NoSuchBucket, SlowDown...) hoặc tên mã HTTP
	Message string
	Err     error // lỗi mạng gốc khi Status == 0 (đã không chứa khoá: ta không ký bằng query)
}

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString("S3 " + e.Op + ": ")
	if e.Status != 0 {
		b.WriteString(strconv.Itoa(e.Status) + " ")
	}
	if e.Code != "" {
		b.WriteString(e.Code)
	}
	if e.Message != "" {
		b.WriteString(": " + e.Message)
	}
	if e.Err != nil {
		b.WriteString(" (" + e.Err.Error() + ")")
	}
	return b.String()
}

func (e *Error) Unwrap() error { return e.Err }

// Is: 404 (không phải NoSuchBucket) là ErrNotFound.
func (e *Error) Is(target error) bool {
	return target == ErrNotFound && e.Status == 404 && e.Code != "NoSuchBucket"
}

// Classify xếp lỗi vào một trong các mã: CodeAuthFailed, CodeBucketNotFound,
// CodeUnreachable, CodeError, CodeCanceled; "" khi err là nil. Theo mã HTTP
// trước, tên lỗi sau (hai máy chủ thử trả câu lỗi khác nhau cho cùng một việc,
// bản thử 05). 5xx và 429 là "không với tới được": client đã thử lại 4 lần
// trước khi trả lỗi đó, nên đây là lỗi của đích, không phải của một bản.
func Classify(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return CodeCanceled
	}
	var fe *FieldError
	if errors.As(err, &fe) {
		return fe.Code
	}
	var e *Error
	if !errors.As(err, &e) {
		// Lỗi không qua client (đọc file khoá, dựng client): coi là lỗi chung.
		return CodeError
	}
	switch {
	case e.Status == 0:
		return CodeUnreachable
	case e.Status == 401 || e.Status == 403:
		return CodeAuthFailed
	case e.Code == "NoSuchBucket" || (e.Status == 404 && (e.Op == "check" || e.Op == "list")):
		return CodeBucketNotFound
	case e.Status >= 500 || e.Status == 429:
		return CodeUnreachable
	}
	return CodeError
}

// IsTargetError: lỗi là của đích (không nối được, sai khoá, mất bucket), không
// nói gì về một bản sao lưu cụ thể. Lượt đẩy dừng (cầu dao) và lùi; không tăng
// remote_tries của bản nào (mục 3.5).
func IsTargetError(err error) bool {
	switch Classify(err) {
	case CodeUnreachable, CodeAuthFailed, CodeBucketNotFound:
		return true
	}
	return false
}

// CleanMessage làm sạch một chuỗi lấy từ kho hay từ lỗi mạng trước khi nó đi
// vào Error: thay mọi lần xuất hiện của từng secrets (khác rỗng) bằng "••••",
// đổi ký tự điều khiển thành dấu cách, cắt còn MaxMessage ký tự. Client gọi
// với access key, secret key và chữ ký của request vừa gửi (S11 của bản soát).
func CleanMessage(s string, secrets ...string) string {
	for _, sec := range secrets {
		if sec != "" {
			s = strings.ReplaceAll(s, sec, "••••")
		}
	}
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || (r >= 0x7f && r < 0xa0) {
			return ' '
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > MaxMessage {
		s = string([]rune(s)[:MaxMessage]) + "…"
	}
	return s
}

// ---------------------------------------------------------------- Kiểm đầu vào

// FieldError là lời từ chối một trường cấu hình. core đổi nó thành
// RefusedError cùng mã.
type FieldError struct {
	Code string
	Msg  i18n.Text
}

func (e *FieldError) Error() string { return e.Code + ": " + e.Msg.Vi }

func fieldErr(code, vi, en string) error {
	return &FieldError{Code: code, Msg: i18n.T(vi, en)}
}

var (
	regionRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	bucketRe = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
	pathRe   = regexp.MustCompile(`^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$`)
	accessRe = regexp.MustCompile(`^[A-Za-z0-9/+=._-]{3,128}$`)
)

// CheckEndpoint kiểm và chuẩn hoá endpoint: chỉ scheme, host, cổng; https,
// hoặc http khi host là loopback. Trả dạng chuẩn "scheme://host[:cổng]" (host
// chữ thường, không "/" cuối).
func CheckEndpoint(s string) (string, error) {
	bad := func(why, whyEn string) (string, error) {
		return "", fieldErr(CodeEndpointInvalid,
			"endpoint không hợp lệ: "+why+" (cần https://host[:cổng]; http chỉ cho 127.0.0.1, ::1, localhost)",
			"invalid endpoint: "+whyEn+" (need https://host[:port]; http only for 127.0.0.1, ::1, localhost)")
	}
	if s == "" {
		return bad("đang trống", "it is empty")
	}
	if strings.IndexFunc(s, func(r rune) bool { return r <= 0x20 || r == 0x7f }) >= 0 {
		return bad("có khoảng trắng hoặc ký tự điều khiển", "it has whitespace or control characters")
	}
	u, err := url.Parse(s)
	if err != nil {
		return bad("không đọc được", "it cannot be parsed")
	}
	if u.User != nil {
		return bad("có tên đăng nhập trong URL", "it has user info in the URL")
	}
	if u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(s, "#") {
		return bad("có query hoặc fragment", "it has a query or fragment")
	}
	if u.Path != "" && u.Path != "/" {
		return bad("có đường dẫn", "it has a path")
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return bad("thiếu host", "the host is missing")
	}
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return bad("cổng sai", "bad port")
		}
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !isLoopback(host) {
			return bad("http chỉ được dùng với máy này (loopback)", "http is only allowed for this machine (loopback)")
		}
	default:
		return bad("scheme phải là https", "the scheme must be https")
	}
	hostport := host
	if strings.Contains(host, ":") {
		hostport = "[" + host + "]"
	}
	if p := u.Port(); p != "" {
		hostport += ":" + p
	}
	return u.Scheme + "://" + hostport, nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// CheckRegion kiểm vùng; rỗng thì trả DefaultRegion.
func CheckRegion(s string) (string, error) {
	if s == "" {
		return DefaultRegion, nil
	}
	if !regionRe.MatchString(s) {
		return "", fieldErr(CodeRegionInvalid, "vùng không hợp lệ (chữ thường, số, dấu gạch ngang)", "invalid region (lowercase letters, digits, hyphens)")
	}
	return s, nil
}

// CheckBucket kiểm tên bucket.
func CheckBucket(s string) error {
	if !bucketRe.MatchString(s) {
		return fieldErr(CodeBucketInvalid, "tên bucket không hợp lệ (3 đến 63 ký tự: chữ thường, số, dấu chấm, gạch ngang)", "invalid bucket name (3 to 63 characters: lowercase letters, digits, dots, hyphens)")
	}
	return nil
}

// cleanSegments: chuỗi khớp pathRe và không đoạn nào là "." hay "..".
func cleanSegments(s string) bool {
	if !pathRe.MatchString(s) {
		return false
	}
	for _, seg := range strings.Split(s, "/") {
		if seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// CheckPrefix kiểm tiền tố của đích: 1 đến 200 ký tự, các đoạn chữ, số, ".",
// "_", "-" cách nhau bằng "/", không đoạn nào là "." hay "..".
func CheckPrefix(s string) error {
	if len(s) < 1 || len(s) > 200 || !cleanSegments(s) {
		return fieldErr(CodePrefixInvalid, "tiền tố không hợp lệ (1 đến 200 ký tự; chữ, số, '.', '_', '-' và '/'; không có đoạn '.' hay '..')", "invalid prefix (1 to 200 characters; letters, digits, '.', '_', '-' and '/'; no '.' or '..' segment)")
	}
	return nil
}

// DefaultPrefix là tiền tố mặc định: tpan/<tên máy>, ký tự lạ thành "-".
func DefaultPrefix(hostname string) string {
	h := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			return r
		}
		return '-'
	}, hostname)
	if len(h) > 100 {
		h = h[:100]
	}
	if h == "" || h == "." || h == ".." {
		h = "may"
	}
	return "tpan/" + h
}

// CheckKey kiểm một khoá object (tương đối với tiền tố): như tiền tố, tối đa 800 ký tự.
func CheckKey(key string) error {
	if len(key) < 1 || len(key) > 800 || !cleanSegments(key) {
		return fmt.Errorf("s3: khoá object %q không hợp lệ", key)
	}
	return nil
}

// CheckListPrefix kiểm tham số prefix của List, AbortUploads: rỗng, hoặc một
// khoá hợp lệ có thể kèm "/" cuối.
func CheckListPrefix(p string) error {
	if p == "" {
		return nil
	}
	if err := CheckKey(strings.TrimSuffix(p, "/")); err != nil {
		return fmt.Errorf("s3: tiền tố liệt kê %q không hợp lệ", p)
	}
	return nil
}

// CheckAccessKey kiểm access key.
func CheckAccessKey(s string) error {
	if !accessRe.MatchString(s) {
		return fieldErr(CodeKeysInvalid, "access key không hợp lệ (3 đến 128 ký tự chữ, số, / + = . _ -)", "invalid access key (3 to 128 characters: letters, digits, / + = . _ -)")
	}
	return nil
}

// CheckSecretKey kiểm secret key: 8 đến 256 ký tự ASCII in được, không khoảng trắng.
func CheckSecretKey(s string) error {
	if len(s) < 8 || len(s) > 256 || strings.IndexFunc(s, func(r rune) bool { return r <= 0x20 || r >= 0x7f }) >= 0 {
		return fieldErr(CodeKeysInvalid, "secret key không hợp lệ (8 đến 256 ký tự ASCII, không khoảng trắng)", "invalid secret key (8 to 256 ASCII characters, no whitespace)")
	}
	return nil
}

// CheckKeep kiểm số bản giữ trên kho cho mỗi site.
func CheckKeep(n int) error {
	if n < 1 || n > MaxKeep {
		return fieldErr(CodeKeepInvalid, fmt.Sprintf("số bản giữ trên kho phải từ 1 tới %d", MaxKeep), fmt.Sprintf("the number of copies kept on the store must be 1 to %d", MaxKeep))
	}
	return nil
}

// CheckCA kiểm chứng chỉ gốc riêng: PEM, tối đa MaxCABytes, ít nhất một
// CERTIFICATE đọc được. Rỗng là không dùng CA riêng.
func CheckCA(b []byte) error {
	if len(b) == 0 {
		return nil
	}
	bad := fieldErr(CodeCAInvalid, "chứng chỉ gốc không hợp lệ (cần file PEM có ít nhất một CERTIFICATE, tối đa 64 KB)", "invalid CA certificate (need a PEM file with at least one CERTIFICATE, at most 64 KB)")
	if len(b) > MaxCABytes {
		return bad
	}
	rest, n := b, 0
	for {
		var blk *pem.Block
		blk, rest = pem.Decode(rest)
		if blk == nil {
			break
		}
		if blk.Type != "CERTIFICATE" {
			continue
		}
		if _, err := x509.ParseCertificate(blk.Bytes); err != nil {
			return bad
		}
		n++
	}
	if n == 0 {
		return bad
	}
	return nil
}

// TargetID là mã của đích: 16 ký tự hex đầu của sha256(endpoint, bucket,
// tiền tố). Cột backups.remote_target ghi mã này (mục 3.3).
func TargetID(endpoint, bucket, prefix string) string {
	sum := sha256.Sum256([]byte(endpoint + "\n" + bucket + "\n" + prefix))
	return hex.EncodeToString(sum[:])[:16]
}

// AccessKeyTail là bốn ký tự cuối của access key khi nó dài từ 12 ký tự;
// ngắn hơn thì rỗng (hiện bốn ký tự cuối của một khoá ngắn là lộ phần lớn
// khoá, S31). Người hiển thị in độ dài thay vào.
func AccessKeyTail(ak string) string {
	if len(ak) < 12 {
		return ""
	}
	return ak[len(ak)-4:]
}
