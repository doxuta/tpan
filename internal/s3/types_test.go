package s3

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCheckEndpoint(t *testing.T) {
	ok := map[string]string{
		"https://S3.Example.com":      "https://s3.example.com",
		"https://s3.example.com/":     "https://s3.example.com",
		"https://s3.example.com:9000": "https://s3.example.com:9000",
		"http://127.0.0.1:7070":       "http://127.0.0.1:7070",
		"http://localhost:7070":       "http://localhost:7070",
		"http://[::1]:7070":           "http://[::1]:7070",
		"https://192.0.2.10:7443":     "https://192.0.2.10:7443",
		"https://[2001:db8::1]:443":   "https://[2001:db8::1]:443",
	}
	for in, want := range ok {
		got, err := CheckEndpoint(in)
		if err != nil || got != want {
			t.Errorf("CheckEndpoint(%q) = %q, %v; muốn %q", in, got, err, want)
		}
	}
	bad := []string{
		"", "ftp://s3.example.com", "http://s3.example.com", "http://192.0.2.10",
		"https://user:pw@s3.example.com", "https://s3.example.com/bucket",
		"https://s3.example.com?x=1", "https://s3.example.com#a", "https://s3.example.com:99999",
		"https:// s3.example.com", "https://s3.example.com\n", "s3.example.com",
	}
	for _, in := range bad {
		if _, err := CheckEndpoint(in); Classify(err) != CodeEndpointInvalid {
			t.Errorf("CheckEndpoint(%q) phải lỗi %s, nhận %v", in, CodeEndpointInvalid, err)
		}
	}
}

func TestCheckKeyVaTienTo(t *testing.T) {
	for _, k := range []string{"a", "sites/a.example.com/a.example.com-20261121-033000.tar.gz", "panel/tpan-20261121-033000.tar.gz.age", "probe/0123456789abcdef"} {
		if err := CheckKey(k); err != nil {
			t.Errorf("CheckKey(%q): %v", k, err)
		}
	}
	for _, k := range []string{"", "..", ".", "a/../b", "./a", "/a", "a/", "a//b", "a b", "a\nb", "dấu", strings.Repeat("a", 801)} {
		if err := CheckKey(k); err == nil {
			t.Errorf("CheckKey(%q) phải lỗi", k)
		}
	}
	for _, p := range []string{"", "sites/", "panel/", "sites/a.example.com/", "probe"} {
		if err := CheckListPrefix(p); err != nil {
			t.Errorf("CheckListPrefix(%q): %v", p, err)
		}
	}
	for _, p := range []string{"/", "../", "a/../", "a//"} {
		if err := CheckListPrefix(p); err == nil {
			t.Errorf("CheckListPrefix(%q) phải lỗi", p)
		}
	}
	if err := CheckPrefix("tpan/may-1"); err != nil {
		t.Error(err)
	}
	for _, p := range []string{"", "a/..", "../a", "a/./b", "/a", strings.Repeat("a", 201)} {
		if err := CheckPrefix(p); err == nil {
			t.Errorf("CheckPrefix(%q) phải lỗi", p)
		}
	}
	if got := DefaultPrefix("web 01/..x"); got != "tpan/web-01-..x" {
		t.Errorf("DefaultPrefix = %q", got)
	}
	if got := DefaultPrefix(".."); got != "tpan/may" {
		t.Errorf("DefaultPrefix(..) = %q", got)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{nil, ""},
		{&Error{Op: "put", Status: 0, Err: errors.New("dial tcp: connection refused")}, CodeUnreachable},
		{&Error{Op: "put", Status: 503, Code: "SlowDown"}, CodeUnreachable},
		{&Error{Op: "put", Status: 429}, CodeUnreachable},
		{&Error{Op: "put", Status: 403, Code: "AccessDenied"}, CodeAuthFailed},
		{&Error{Op: "check", Status: 404, Code: "NoSuchBucket"}, CodeBucketNotFound},
		{&Error{Op: "list", Status: 404}, CodeBucketNotFound},
		{&Error{Op: "put", Status: 400, Code: "EntityTooLarge"}, CodeError},
		{context.Canceled, CodeCanceled},
		{errors.New("lạ"), CodeError},
	}
	for _, c := range cases {
		if got := Classify(c.err); got != c.want {
			t.Errorf("Classify(%v) = %q, muốn %q", c.err, got, c.want)
		}
	}
	if !IsTargetError(&Error{Status: 503}) || IsTargetError(&Error{Status: 400}) || IsTargetError(context.Canceled) {
		t.Error("IsTargetError sai")
	}
	if !errors.Is(&Error{Op: "head", Status: 404}, ErrNotFound) || errors.Is(&Error{Op: "head", Status: 404, Code: "NoSuchBucket"}, ErrNotFound) {
		t.Error("Error.Is(ErrNotFound) sai")
	}
}

func TestCleanMessage(t *testing.T) {
	got := CleanMessage("No such key: AKIAEXAMPLE1234\nsig=abcdef0123\x07 xong", "AKIAEXAMPLE1234", "abcdef0123", "")
	if strings.Contains(got, "AKIAEXAMPLE1234") || strings.Contains(got, "abcdef0123") || strings.ContainsAny(got, "\n\x07") {
		t.Fatalf("CleanMessage còn khoá hoặc ký tự điều khiển: %q", got)
	}
	if long := CleanMessage(strings.Repeat("ạ", 400)); len([]rune(long)) != MaxMessage+1 {
		t.Fatalf("không cắt: %d ký tự", len([]rune(long)))
	}
}

func TestTargetIDVaTail(t *testing.T) {
	a := TargetID("https://s3.example.com", "b", "tpan/x")
	if len(a) != 16 || a == TargetID("https://s3.example.com", "b", "tpan/y") {
		t.Fatalf("TargetID = %q", a)
	}
	if AccessKeyTail("AKIAIOSFODNN7EXAMPLE") != "MPLE" || AccessKeyTail("short") != "" {
		t.Fatal("AccessKeyTail sai")
	}
}

func TestMem(t *testing.T) {
	ctx := context.Background()
	m := NewMem()
	sum, err := m.Put(ctx, "sites/a/x.tar.gz", bytes.NewReader([]byte("abc")), 3, nil)
	if err != nil || len(sum) != 64 {
		t.Fatalf("Put: %q %v", sum, err)
	}
	var buf bytes.Buffer
	n, got, err := m.Get(ctx, "sites/a/x.tar.gz", &buf)
	if err != nil || n != 3 || got != sum || buf.String() != "abc" {
		t.Fatalf("Get: %d %q %v %q", n, got, err, buf.String())
	}
	m.Corrupt("sites/a/x.tar.gz")
	buf.Reset()
	if _, got2, _ := m.Get(ctx, "sites/a/x.tar.gz", &buf); got2 == sum {
		t.Fatal("Corrupt không đổi dữ liệu")
	}
	if o, err := m.Head(ctx, "sites/a/x.tar.gz"); err != nil || o.Size != 3 {
		t.Fatalf("Head sau Corrupt: %+v %v", o, err)
	}
	if _, err := m.Head(ctx, "khong/co"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Head không có: %v", err)
	}
	var keys []string
	_ = m.List(ctx, "sites/", func(o Object) error { keys = append(keys, o.Key); return nil })
	if len(keys) != 1 {
		t.Fatalf("List: %v", keys)
	}
	m.Fail("put", "", &Error{Op: "put", Status: 503})
	if _, err := m.Put(ctx, "a", bytes.NewReader(nil), 0, nil); !IsTargetError(err) {
		t.Fatalf("Fail put: %v", err)
	}
	m.Fail("put", "", nil)
	if _, err := m.Put(ctx, "../a", bytes.NewReader(nil), 0, nil); err == nil {
		t.Fatal("Put key .. phải lỗi")
	}
	old := time.Now().Add(-48 * time.Hour)
	m.AddUpload("sites/a/cu", old)
	m.AddUpload("sites/a/moi", time.Now())
	if n, _ := m.AbortUploads(ctx, "", 24*time.Hour); n != 1 || len(m.Uploads()) != 1 {
		t.Fatalf("AbortUploads theo tuổi: %d, còn %v", n, m.Uploads())
	}
	if err := m.Delete(ctx, "sites/a/x.tar.gz"); err != nil || len(m.Objects()) != 0 {
		t.Fatalf("Delete: %v %v", err, m.Objects())
	}
	// Key sai luật bị chặn trước khi tới kho, nên chỉ hai lời gọi được đếm.
	if m.Calls("put") != 2 {
		t.Fatalf("Calls(put) = %d", m.Calls("put"))
	}
}
