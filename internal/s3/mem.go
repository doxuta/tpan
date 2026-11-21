package s3

import (
	"context"
	"crypto/md5" //nolint:gosec // ETag của S3 là md5; ở đây chỉ để giống kho thật, không dùng cho an toàn
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
)

// Mem là kho S3 giả trong bộ nhớ cho test của steps, core, server (M18, đợt 0).
// Có đúng bộ phương thức của Client (steps.ObjectStore), cùng luật kiểm key,
// cộng vài hàm để gây lỗi. Key là tương đối (như với Client). An toàn khi
// dùng từ nhiều goroutine.
type Mem struct {
	mu      sync.Mutex
	objects map[string]memObject
	uploads []memUpload
	fails   map[string]error // op + "\x00" + key; key "" khớp mọi key của op đó
	// Now nil thì dùng time.Now (Modified của object, tuổi của lần tải dở).
	Now func() time.Time
	// Calls đếm số lời gọi theo op, để test khẳng định "không gọi mạng".
	calls map[string]int
}

type memObject struct {
	data     []byte
	modified time.Time
}

type memUpload struct {
	key       string
	initiated time.Time
}

// NewMem tạo một kho rỗng.
func NewMem() *Mem {
	return &Mem{objects: map[string]memObject{}, fails: map[string]error{}, calls: map[string]int{}}
}

func (m *Mem) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

// Fail làm lời gọi op trên key trả err cho tới khi gọi Fail(op, key, nil).
// op: "check", "put", "get", "head", "delete", "list", "abort". key rỗng khớp
// mọi key của op đó. Ví dụ lỗi của đích:
// &Error{Op: "put", Status: 503, Code: "SlowDown"}.
func (m *Mem) Fail(op, key string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err == nil {
		delete(m.fails, op+"\x00"+key)
		return
	}
	m.fails[op+"\x00"+key] = err
}

func (m *Mem) failed(op, key string) error {
	m.calls[op]++
	if err, ok := m.fails[op+"\x00"+key]; ok {
		return err
	}
	if err, ok := m.fails[op+"\x00"]; ok {
		return err
	}
	return nil
}

// Calls trả số lời gọi của op từ lúc tạo kho.
func (m *Mem) Calls(op string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls[op]
}

// Corrupt đổi một byte ở giữa object key: Head vẫn trả đúng cỡ, Get trả dữ
// liệu khác (như object hỏng trên đĩa của kho, bản thử 07 bước 5).
func (m *Mem) Corrupt(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.objects[key]
	if !ok || len(o.data) == 0 {
		return
	}
	d := append([]byte(nil), o.data...)
	d[len(d)/2] ^= 0xff
	o.data = d
	m.objects[key] = o
}

// SetObject đặt sẵn một object (như ai đó ghi thẳng vào bucket).
func (m *Mem) SetObject(key string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = memObject{data: append([]byte(nil), data...), modified: m.now()}
}

// Objects trả bản chép của mọi object, theo key.
func (m *Mem) Objects() map[string][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string][]byte, len(m.objects))
	for k, o := range m.objects {
		out[k] = append([]byte(nil), o.data...)
	}
	return out
}

// AddUpload giả một lần tải nhiều phần còn dở của key, bắt đầu lúc initiated.
func (m *Mem) AddUpload(key string, initiated time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.uploads = append(m.uploads, memUpload{key: key, initiated: initiated})
}

// Uploads trả key của các lần tải dở còn lại.
func (m *Mem) Uploads() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.uploads))
	for _, u := range m.uploads {
		out = append(out, u.key)
	}
	return out
}

// Check như Client.Check.
func (m *Mem) Check(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.failed("check", "")
}

// Put đọc đúng size byte từ r, lưu, trả sha256 của các byte đã đọc.
func (m *Mem) Put(_ context.Context, key string, r io.Reader, size int64, progress func(done int64)) (string, error) {
	if err := CheckKey(key); err != nil {
		return "", err
	}
	if size < 0 {
		return "", fmt.Errorf("s3: cỡ âm %d", size)
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(r, data); err != nil {
		return "", fmt.Errorf("s3 put %s: đọc nguồn: %w", key, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.failed("put", key); err != nil {
		return "", err
	}
	m.objects[key] = memObject{data: data, modified: m.now()}
	if progress != nil {
		progress(size)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// Get ghi object vào w, trả số byte và sha256.
func (m *Mem) Get(_ context.Context, key string, w io.Writer) (int64, string, error) {
	if err := CheckKey(key); err != nil {
		return 0, "", err
	}
	m.mu.Lock()
	if err := m.failed("get", key); err != nil {
		m.mu.Unlock()
		return 0, "", err
	}
	o, ok := m.objects[key]
	m.mu.Unlock()
	if !ok {
		return 0, "", &Error{Op: "get", Status: 404, Code: "NoSuchKey"}
	}
	n, err := w.Write(o.data)
	if err != nil {
		return int64(n), "", fmt.Errorf("s3 get %s: ghi: %w", key, err)
	}
	sum := sha256.Sum256(o.data)
	return int64(n), hex.EncodeToString(sum[:]), nil
}

// Head trả cỡ, lúc sửa, ETag (md5 trong nháy kép, như S3 với object một phần).
func (m *Mem) Head(_ context.Context, key string) (Object, error) {
	if err := CheckKey(key); err != nil {
		return Object{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.failed("head", key); err != nil {
		return Object{}, err
	}
	o, ok := m.objects[key]
	if !ok {
		return Object{}, &Error{Op: "head", Status: 404, Code: "Not Found"}
	}
	sum := md5.Sum(o.data) //nolint:gosec // xem import
	return Object{Key: key, Size: int64(len(o.data)), Modified: o.modified, ETag: `"` + hex.EncodeToString(sum[:]) + `"`}, nil
}

// Delete xoá; không có cũng là xong.
func (m *Mem) Delete(_ context.Context, key string) error {
	if err := CheckKey(key); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.failed("delete", key); err != nil {
		return err
	}
	delete(m.objects, key)
	return nil
}

// List gọi each cho mọi object có key bắt đầu bằng prefix, theo thứ tự key.
func (m *Mem) List(_ context.Context, prefix string, each func(Object) error) error {
	if err := CheckListPrefix(prefix); err != nil {
		return err
	}
	m.mu.Lock()
	if err := m.failed("list", prefix); err != nil {
		m.mu.Unlock()
		return err
	}
	var objs []Object
	for k, o := range m.objects {
		if strings.HasPrefix(k, prefix) {
			sum := md5.Sum(o.data) //nolint:gosec // xem import
			objs = append(objs, Object{Key: k, Size: int64(len(o.data)), Modified: o.modified, ETag: `"` + hex.EncodeToString(sum[:]) + `"`})
		}
	}
	m.mu.Unlock()
	sort.Slice(objs, func(i, j int) bool { return objs[i].Key < objs[j].Key })
	for _, o := range objs {
		if err := each(o); err != nil {
			return err
		}
	}
	return nil
}

// AbortUploads huỷ các lần tải dở có key bắt đầu bằng prefix và bắt đầu trước
// now - olderThan (olderThan 0: mọi lần), trả số lần đã huỷ.
func (m *Mem) AbortUploads(_ context.Context, prefix string, olderThan time.Duration) (int, error) {
	if err := CheckListPrefix(prefix); err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.failed("abort", prefix); err != nil {
		return 0, err
	}
	cut := m.now().Add(-olderThan)
	kept := m.uploads[:0]
	n := 0
	for _, u := range m.uploads {
		if strings.HasPrefix(u.key, prefix) && (olderThan == 0 || u.initiated.Before(cut)) {
			n++
			continue
		}
		kept = append(kept, u)
	}
	m.uploads = kept
	return n, nil
}
