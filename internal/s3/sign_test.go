package s3

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

// Khoá trong các ví dụ dưới đây là khoá mẫu công khai trong tài liệu của AWS
// ("Signature Calculations for the Authorization Header: Transferring Payload
// in a Single Chunk"), không phải khoá thật của ai.
const (
	awsAccess = "AKIAIOSFODNN7EXAMPLE"
	awsSecret = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
)

var awsTime = time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC)

func awsClient(t *testing.T) *Client {
	t.Helper()
	c, err := New(Config{
		Endpoint: "https://s3.amazonaws.com", Region: "us-east-1", Bucket: "examplebucket", Prefix: "p",
		AccessKey: awsAccess, SecretKey: secret(awsSecret),
		Now: func() time.Time { return awsTime },
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func sigOf(t *testing.T, auth string) string {
	t.Helper()
	i := strings.Index(auth, "Signature=")
	if i < 0 {
		t.Fatalf("không có Signature trong %q", auth)
	}
	return auth[i+len("Signature="):]
}

// B8: hai vectơ mức bucket đi qua đúng đường dựng request của client (kiểu
// bucket.host như ví dụ của AWS); hai vectơ mức object ký header tuỳ ý (range,
// date không phải x-amz-*) nên đi thẳng vào hàm ký.
func TestSigV4VectoAWS(t *testing.T) {
	c := awsClient(t)
	c.pathStyle = false

	bucket := []struct {
		name  string
		query url.Values
		want  string
	}{
		{"GET Bucket ?max-keys=2&prefix=J", url.Values{"max-keys": {"2"}, "prefix": {"J"}}, "34b48302e7b5fa45bde8084f4b7868a86f0a534bc59db6670ed5711ef69dc6f7"},
		{"GET Bucket ?lifecycle", url.Values{"lifecycle": {""}}, "fea454ca298b7da1c68078a5d1bdbfbbe0d65c699e0f91ac7a200a0136783543"},
	}
	for _, v := range bucket {
		t.Run(v.name, func(t *testing.T) {
			req, sig, err := c.newRequest(t.Context(), request{method: "GET", query: v.query})
			if err != nil {
				t.Fatal(err)
			}
			if sig != v.want || sigOf(t, req.Header.Get("Authorization")) != v.want {
				t.Errorf("chữ ký %s, mong %s", sig, v.want)
			}
			if req.URL.Host != "examplebucket.s3.amazonaws.com" || req.URL.EscapedPath() != "/" {
				t.Errorf("URL %s", req.URL)
			}
		})
	}

	s := signer{access: awsAccess, secret: awsSecret, region: "us-east-1"}
	t.Run("GET Object /test.txt với Range", func(t *testing.T) {
		_, sig := s.sign("GET", "/test.txt", "", map[string]string{
			"host":                 "examplebucket.s3.amazonaws.com",
			"range":                "bytes=0-9",
			"x-amz-content-sha256": emptySHA256,
			"x-amz-date":           "20130524T000000Z",
		}, emptySHA256, awsTime)
		if want := "f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"; sig != want {
			t.Errorf("chữ ký %s, mong %s", sig, want)
		}
	})
	t.Run("PUT Object /test$file.text", func(t *testing.T) {
		body := sha256Hex([]byte("Welcome to Amazon S3."))
		if body != "44ce7dd67c959e0d3524ffac1771dfbba87d2b6b4b4e99e42034a8b803f8b072" {
			t.Fatalf("sha256 của thân %s", body)
		}
		_, sig := s.sign("PUT", encodePath("/test$file.text"), "", map[string]string{
			"date":                 "Fri, 24 May 2013 00:00:00 GMT",
			"host":                 "examplebucket.s3.amazonaws.com",
			"x-amz-content-sha256": body,
			"x-amz-date":           "20130524T000000Z",
			"x-amz-storage-class":  "REDUCED_REDUNDANCY",
		}, body, awsTime)
		if want := "98ad721746da40c64f1a55b78f14c238d841ea1380cd77a1b5971af0ece108bd"; sig != want {
			t.Errorf("chữ ký %s, mong %s", sig, want)
		}
	})
}

func TestMaHoaDuongDan(t *testing.T) {
	cases := map[string]string{
		"/a b/c+d":        "/a%20b/c%2Bd",
		"/test$file.text": "/test%24file.text",
		"/dấu/~x_y-z.1":   "/d%E1%BA%A5u/~x_y-z.1",
		"/a&b=(c)":        "/a%26b%3D%28c%29",
	}
	for in, want := range cases {
		if got := encodePath(in); got != want {
			t.Errorf("encodePath(%q) = %q, mong %q", in, got, want)
		}
	}
	q := canonicalQuery(url.Values{"b": {"2", "1"}, "a b": {"x/y"}, "uploads": {""}})
	if want := "a%20b=x%2Fy&b=1&b=2&uploads="; q != want {
		t.Errorf("canonicalQuery = %q, mong %q", q, want)
	}
}
