package s3

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Ký SigV4 (AWS4-HMAC-SHA256) trong header Authorization. Viết lại từ bản thử
// docs/bang-chung/M18-nghien-cuu/ban-thu-s3 (đã chạy với versitygw và Garage),
// khác ở chỗ hàm ký nhận một tập header tuỳ ý để kiểm được cả các vectơ mẫu
// mức object của AWS (ký thêm range, date).

const (
	algorithm     = "AWS4-HMAC-SHA256"
	emptySHA256   = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	amzDateLayout = "20060102T150405Z"
)

// uriEncode mã hoá theo luật của SigV4 (RFC 3986): chỉ giữ A-Z a-z 0-9 - _ . ~,
// mọi byte khác thành %XX viết hoa. Không dùng url.PathEscape vì nó để nguyên
// những ký tự như "$", "+", "&" mà S3 lại mã hoá khi tính chữ ký, nên hai bên
// ra hai chuỗi khác nhau và kho trả SignatureDoesNotMatch.
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

// encodePath mã hoá từng đoạn của đường dẫn đúng một lần, giữ dấu "/".
func encodePath(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = uriEncode(s)
	}
	return strings.Join(segs, "/")
}

// canonicalQuery sắp theo tên rồi theo giá trị (đều đã mã hoá), mã hoá cả hai.
// Chuỗi này vừa là phần query của chữ ký vừa là URL.RawQuery gửi đi, nên thứ
// kho nhận đúng bằng thứ đã ký.
func canonicalQuery(q url.Values) string {
	type kv struct{ k, v string }
	var pairs []kv
	for k, vs := range q {
		for _, v := range vs {
			pairs = append(pairs, kv{uriEncode(k), uriEncode(v)})
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].k != pairs[j].k {
			return pairs[i].k < pairs[j].k
		}
		return pairs[i].v < pairs[j].v
	})
	out := make([]string, len(pairs))
	for i, p := range pairs {
		out[i] = p.k + "=" + p.v
	}
	return strings.Join(out, "&")
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

// signer giữ khoá để ký. secret là giá trị thật (đã Reveal): chỉ đi vào HMAC,
// không bao giờ vào chuỗi nào trả ra ngoài.
type signer struct {
	access, secret, region string
}

// sign tính chữ ký cho một request. rawPath và rawQuery là đúng chuỗi sẽ gửi
// trên dây. hdr là các header được ký, tên chữ thường; người gọi phải đưa đủ
// host, x-amz-date, x-amz-content-sha256. Trả giá trị header Authorization và
// riêng chữ ký (để làm sạch câu lỗi, S11).
func (s signer) sign(method, rawPath, rawQuery string, hdr map[string]string, payloadHash string, t time.Time) (auth, sig string) {
	names := make([]string, 0, len(hdr))
	for n := range hdr {
		names = append(names, n)
	}
	sort.Strings(names)
	var ch strings.Builder
	for _, n := range names {
		// Giá trị bỏ khoảng trắng hai đầu và gộp khoảng trắng liên tiếp, như luật của SigV4.
		ch.WriteString(n + ":" + strings.Join(strings.Fields(hdr[n]), " ") + "\n")
	}
	signed := strings.Join(names, ";")
	canon := strings.Join([]string{method, rawPath, rawQuery, ch.String(), signed, payloadHash}, "\n")
	t = t.UTC()
	day := t.Format("20060102")
	scope := day + "/" + s.region + "/s3/aws4_request"
	toSign := strings.Join([]string{algorithm, t.Format(amzDateLayout), scope, sha256Hex([]byte(canon))}, "\n")
	k := hmacSHA256([]byte("AWS4"+s.secret), day)
	k = hmacSHA256(k, s.region)
	k = hmacSHA256(k, "s3")
	k = hmacSHA256(k, "aws4_request")
	sig = hex.EncodeToString(hmacSHA256(k, toSign))
	return algorithm + " Credential=" + s.access + "/" + scope + ", SignedHeaders=" + signed + ", Signature=" + sig, sig
}
