package main

// Kiểm bộ ký của bản thử với các ví dụ công khai trong tài liệu của AWS
// ("Signature Calculations for the Authorization Header: Transferring Payload
// in a Single Chunk (AWS Signature Version 4)"). Khoá trong các ví dụ đó là
// khoá mẫu của tài liệu, không phải khoá thật của ai. Mục đích: xác nhận các
// vectơ này trước khi giao cho thợ viết internal/s3 làm test.
//
//	go test -C docs/bang-chung/M18-nghien-cuu/ban-thu-s3 -v ./...

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func exampleClient(t *testing.T) *client {
	t.Helper()
	u, err := url.Parse("https://s3.amazonaws.com")
	if err != nil {
		t.Fatal(err)
	}
	return &client{
		endpoint: u, region: "us-east-1", bucket: "examplebucket", vhost: true,
		accessKey: "AKIAIOSFODNN7EXAMPLE", secretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		now: func() time.Time { return time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC) },
	}
}

func signatureOf(t *testing.T, req *http.Request) string {
	t.Helper()
	auth := req.Header.Get("Authorization")
	i := strings.Index(auth, "Signature=")
	if i < 0 {
		t.Fatalf("không có Signature trong %q", auth)
	}
	return auth[i+len("Signature="):]
}

func TestVectoAWS(t *testing.T) {
	ctx := context.Background()
	c := exampleClient(t)

	t.Run("GET Bucket (List Objects) ?max-keys=2&prefix=J", func(t *testing.T) {
		req, err := c.newRequest(ctx, http.MethodGet, "", url.Values{"max-keys": {"2"}, "prefix": {"J"}}, nil, 0, emptySHA256, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("URL: %s", req.URL.String())
		if got, want := signatureOf(t, req), "34b48302e7b5fa45bde8084f4b7868a86f0a534bc59db6670ed5711ef69dc6f7"; got != want {
			t.Errorf("chữ ký %s, mong %s", got, want)
		}
	})

	t.Run("GET Bucket Lifecycle ?lifecycle", func(t *testing.T) {
		req, err := c.newRequest(ctx, http.MethodGet, "", url.Values{"lifecycle": {""}}, nil, 0, emptySHA256, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := signatureOf(t, req), "fea454ca298b7da1c68078a5d1bdbfbbe0d65c699e0f91ac7a200a0136783543"; got != want {
			t.Errorf("chữ ký %s, mong %s", got, want)
		}
	})

	t.Run("GET Object /test.txt với Range", func(t *testing.T) {
		// Ví dụ của AWS ký cả header range; bản thử chỉ ký host và x-amz-*, nên
		// ở đây tự ký lại theo đúng danh sách header của ví dụ không làm được
		// bằng newRequest. Bỏ qua, ghi lại chữ ký của ví dụ để thợ dùng nếu
		// internal/s3 có đường ký header tuỳ ý:
		// f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41
		t.Skip("bản thử không ký header ngoài x-amz-*")
	})

	t.Run("PUT Object /test$file.text", func(t *testing.T) {
		// Ví dụ của AWS ký thêm header date và x-amz-storage-class; date không
		// phải x-amz-* nên cũng không tái hiện được bằng newRequest. Chữ ký của
		// ví dụ: 98ad721746da40c64f1a55b78f14c238d841ea1380cd77a1b5971af0ece108bd
		t.Skip("bản thử không ký header ngoài x-amz-*")
	})
}

func TestMaHoaDuongDan(t *testing.T) {
	cases := map[string]string{
		"/a b/c+d":      "/a%20b/c%2Bd",
		"/test$file.text": "/test%24file.text",
		"/dấu/~x_y-z.1": "/d%E1%BA%A5u/~x_y-z.1",
		"/a&b=(c)":      "/a%26b%3D%28c%29",
	}
	for in, want := range cases {
		if got := encodePath(in); got != want {
			t.Errorf("encodePath(%q) = %q, mong %q", in, got, want)
		}
	}
}
