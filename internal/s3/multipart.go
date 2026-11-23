package s3

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

// partSizeFor trả cỡ phần cho một object size byte: cỡ phần của client, tăng
// lên (làm tròn lên MiB) khi cần để không quá MaxParts phần.
func partSizeFor(ps, size int64) int64 {
	if size <= ps*MaxParts {
		return ps
	}
	const mib = 1 << 20
	n := (size + MaxParts - 1) / MaxParts
	return (n + mib - 1) / mib * mib
}

// Put đọc đúng size byte từ r và tải lên key (tương đối). Vừa một phần thì
// một PUT; lớn hơn thì tải nhiều phần, tuần tự, chỉ giữ một phần trong bộ
// nhớ. Trả sha256 hex của các byte đã gửi; progress (có thể nil) được gọi với
// tổng số byte đã lên kho mỗi khi có thêm.
func (c *Client) Put(ctx context.Context, key string, r io.Reader, size int64, progress func(done int64)) (string, error) {
	if err := CheckKey(key); err != nil {
		return "", err
	}
	if size < 0 {
		return "", fmt.Errorf("s3 put %s: cỡ âm %d", key, size)
	}
	if size > c.partSize {
		return c.putMultipart(ctx, key, r, size, partSizeFor(c.partSize, size), progress)
	}
	buf := make([]byte, size)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", fmt.Errorf("s3 put %s: đọc nguồn: %w", key, err)
	}
	if err := c.do(ctx, request{op: "put", method: http.MethodPut, key: c.full(key), body: buf}, nil); err != nil {
		return "", err
	}
	if progress != nil {
		progress(size)
	}
	return sha256Hex(buf), nil
}

type completePart struct {
	PartNumber int
	ETag       string
}

type completeBody struct {
	XMLName xml.Name       `xml:"CompleteMultipartUpload"`
	Parts   []completePart `xml:"Part"`
}

// putMultipart mở một lần tải nhiều phần; lỗi ở bất kỳ đâu sau khi mở thì huỷ
// nó để không để lại phần tải dở chiếm chỗ trên kho (B9).
func (c *Client) putMultipart(ctx context.Context, key string, r io.Reader, size, ps int64, progress func(int64)) (string, error) {
	full := c.full(key)
	var init struct {
		UploadID string `xml:"UploadId"`
	}
	err := c.do(ctx, request{op: "put", method: http.MethodPost, key: full, query: url.Values{"uploads": {""}}},
		func(k *call) error { return k.decode(&init) })
	if err != nil {
		return "", err
	}
	if init.UploadID == "" {
		return "", &Error{Op: "put", Status: 200, Code: "InvalidResponse", Message: "kho không trả UploadId"}
	}
	sum, err := c.uploadParts(ctx, key, init.UploadID, r, size, ps, progress)
	if err == nil {
		return sum, nil
	}
	// Huỷ bằng context riêng có hạn: ctx của người gọi có thể chính là thứ đã
	// bị huỷ, mà phần tải dở vẫn phải dọn.
	actx, cancel := context.WithTimeout(context.Background(), c.abortTimeout)
	defer cancel()
	if aerr := c.abortOne(actx, full, init.UploadID); aerr != nil {
		return "", fmt.Errorf("%w; huỷ lần tải dở cũng lỗi: %w", err, aerr)
	}
	return "", err
}

func (c *Client) uploadParts(ctx context.Context, key, uploadID string, r io.Reader, size, ps int64, progress func(int64)) (string, error) {
	full := c.full(key)
	whole := sha256.New()
	buf := make([]byte, ps)
	var parts []completePart
	var sent int64
	for n := 1; sent < size; n++ {
		part := buf[:min(ps, size-sent)]
		if _, err := io.ReadFull(r, part); err != nil {
			return "", fmt.Errorf("s3 put %s: đọc nguồn ở phần %d: %w", key, n, err)
		}
		whole.Write(part)
		q := url.Values{"partNumber": {strconv.Itoa(n)}, "uploadId": {uploadID}}
		var etag string
		err := c.do(ctx, request{op: "part", method: http.MethodPut, key: full, query: q, body: part},
			func(k *call) error { etag = k.res.Header.Get("ETag"); return nil })
		if err != nil {
			return "", err
		}
		if etag == "" {
			return "", &Error{Op: "part", Status: 200, Code: "InvalidResponse", Message: "kho không trả ETag cho phần " + strconv.Itoa(n)}
		}
		parts = append(parts, completePart{PartNumber: n, ETag: etag})
		sent += int64(len(part))
		if progress != nil {
			progress(sent)
		}
	}
	sum := hex.EncodeToString(whole.Sum(nil))

	body, err := xml.Marshal(completeBody{Parts: parts})
	if err != nil {
		return "", fmt.Errorf("s3 put %s: dựng danh sách phần: %w", key, err)
	}
	// Bước ghép có hạn chờ header riêng: kho có thể ghép hàng chục GB trước
	// khi trả lời (S19). Câu trả lời 200 vẫn có thể là <Error>.
	err = c.do(ctx, request{op: "complete", method: http.MethodPost, key: full, query: url.Values{"uploadId": {uploadID}}, body: body, headerTimeout: c.completeTimeout},
		func(k *call) error {
			b, err := k.readAll(maxErrBody)
			if err != nil {
				return err
			}
			if x, ok := parseErrXML(b); ok {
				return c.newError("complete", k.res.StatusCode, x.Code, x.Message, k.sig)
			}
			return nil
		})
	if err == nil {
		return sum, nil
	}
	// Lỗi mạng, hết giờ, hoặc NoSuchUpload (lần trước đã ghép xong nhưng câu
	// trả lời bị mất, rồi lần thử lại không còn thấy upload): hỏi thẳng kho
	// object đã có chưa. Đúng cỡ thì coi là đã ghép; người gọi còn đọc lại để
	// kiểm sha256.
	var e *Error
	if ctx.Err() == nil && errors.As(err, &e) && (e.Status == 0 || e.Code == "NoSuchUpload") {
		if o, herr := c.Head(ctx, key); herr == nil && o.Size == size {
			return sum, nil
		}
	}
	return "", err
}
