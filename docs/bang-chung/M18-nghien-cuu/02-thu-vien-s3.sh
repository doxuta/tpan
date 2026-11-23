#!/usr/bin/env bash
# M18, đợt 0, câu "dùng thư viện S3 nào hay tự ký SigV4: xét số phụ thuộc kéo
# vào file chạy". Chạy trên Mac (chỉ cần Go và mạng tới proxy.golang.org):
#
#   make m18-nc S=02-thu-vien-s3
#
# Dựng bốn chương trình nhỏ trong một thư mục tạm, mỗi cái làm cùng một việc
# (ghi, đọc, liệt kê, xoá object, tải nhiều phần) bằng một cách khác nhau, rồi
# đo: số module kéo vào, số gói được biên dịch, cỡ file chạy linux/amd64 (cùng
# cờ với `make build-linux`). Chương trình "nền" chỉ dùng thư viện chuẩn, nên
# hiệu số là phần tăng thêm do thư viện.
#
# Không lưu go.mod, go.sum của bản thử vào repo: kết quả ở 02-thu-vien-s3.out.
set -euo pipefail

W="$(mktemp -d)"
trap 'rm -rf "$W"' EXIT
export GOFLAGS=-mod=mod GOWORK=off CGO_ENABLED=0 GOOS=linux GOARCH=amd64

echo "== ngay tra: $(date -u +%Y-%m-%dT%H:%M:%SZ), $(go version)"

measure() {
  # $1 = tên thư mục, $2... = module cần go get (có thể rỗng)
  local name="$1"
  shift
  local d="$W/$name"
  (
    cd "$d"
    go mod init "thu/$name" >/dev/null 2>&1
    for m in "$@"; do go get "$m" >/dev/null 2>&1 || { echo "go get $m LOI"; go get "$m"; }; done
    go mod tidy >/dev/null 2>&1
    go build -trimpath -ldflags '-s -w' -o "$W/$name.bin" . || { echo "BUILD LOI: $name"; exit 1; }
    local mods pkgs size
    mods="$(go list -m all | tail -n +2 | wc -l | tr -d ' ')"
    pkgs="$(go list -deps . | wc -l | tr -d ' ')"
    size="$(wc -c <"$W/$name.bin" | tr -d ' ')"
    echo
    echo "== $name: module ngoai=$mods, goi bien dich=$pkgs, file chay=$size byte"
    go list -m all | tail -n +2 | sed 's/^/   /'
  )
}

mkdir -p "$W/nen" "$W/aws" "$W/minio" "$W/age"

# ---- nền: thư viện chuẩn, ký HMAC-SHA256 và gọi HTTP, đọc XML.
cat >"$W/nen/main.go" <<'EOF'
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
)

type listResult struct {
	Contents []struct {
		Key  string `xml:"Key"`
		Size int64  `xml:"Size"`
	} `xml:"Contents"`
}

func sign(key []byte, msg string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(msg))
	return h.Sum(nil)
}

func main() {
	req, _ := http.NewRequest(http.MethodGet, os.Args[1], nil)
	req.Header.Set("Authorization", hex.EncodeToString(sign([]byte("k"), "m")))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer res.Body.Close()
	var out listResult
	_ = xml.NewDecoder(res.Body).Decode(&out)
	_, _ = io.Copy(os.Stdout, res.Body)
}
EOF

# ---- aws-sdk-go-v2: client S3 với khoá tĩnh, endpoint tuỳ ý, path-style.
cat >"$W/aws/main.go" <<'EOF'
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func main() {
	ctx := context.Background()
	c := s3.New(s3.Options{
		Region:       "us-east-1",
		BaseEndpoint: aws.String(os.Args[1]),
		UsePathStyle: true,
		Credentials:  credentials.NewStaticCredentialsProvider("a", "b", ""),
	})
	f, _ := os.Open(os.Args[2])
	_, err := c.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("b"), Key: aws.String("k"), Body: f})
	fmt.Println(err)
	g, err := c.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String("b"), Key: aws.String("k")})
	fmt.Println(g, err)
	_, err = c.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String("b"), Key: aws.String("k")})
	fmt.Println(err)
	_, err = c.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String("b")})
	fmt.Println(err)
	_, err = c.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String("b"), Key: aws.String("k")})
	fmt.Println(err)
	m, err := c.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{Bucket: aws.String("b"), Key: aws.String("k")})
	fmt.Println(m, err)
	p, err := c.UploadPart(ctx, &s3.UploadPartInput{Bucket: aws.String("b"), Key: aws.String("k"), UploadId: aws.String("u"), PartNumber: aws.Int32(1), Body: f})
	fmt.Println(p, err)
	_, err = c.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{Bucket: aws.String("b"), Key: aws.String("k"), UploadId: aws.String("u"),
		MultipartUpload: &types.CompletedMultipartUpload{Parts: []types.CompletedPart{{PartNumber: aws.Int32(1), ETag: aws.String("e")}}}})
	fmt.Println(err)
	_, err = c.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{Bucket: aws.String("b"), Key: aws.String("k"), UploadId: aws.String("u")})
	fmt.Println(err)
}
EOF

# ---- minio-go v7.
cat >"$W/minio/main.go" <<'EOF'
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func main() {
	ctx := context.Background()
	c, err := minio.New(os.Args[1], &minio.Options{
		Creds:        credentials.NewStaticV4("a", "b", ""),
		Secure:       true,
		Region:       "us-east-1",
		BucketLookup: minio.BucketLookupPath,
	})
	fmt.Println(err)
	f, _ := os.Open(os.Args[2])
	_, err = c.PutObject(ctx, "b", "k", f, -1, minio.PutObjectOptions{})
	fmt.Println(err)
	o, err := c.GetObject(ctx, "b", "k", minio.GetObjectOptions{})
	fmt.Println(o, err)
	_, err = c.StatObject(ctx, "b", "k", minio.StatObjectOptions{})
	fmt.Println(err)
	for x := range c.ListObjects(ctx, "b", minio.ListObjectsOptions{}) {
		fmt.Println(x.Key)
	}
	fmt.Println(c.RemoveObject(ctx, "b", "k", minio.RemoveObjectOptions{}))
}
EOF

# ---- age: mã hoá và giải mã theo dòng bằng khoá X25519.
cat >"$W/age/main.go" <<'EOF'
package main

import (
	"fmt"
	"io"
	"os"

	"filippo.io/age"
)

func main() {
	id, err := age.GenerateX25519Identity()
	fmt.Println(err)
	w, err := age.Encrypt(os.Stdout, id.Recipient())
	fmt.Println(err)
	_, _ = io.Copy(w, os.Stdin)
	_ = w.Close()
	r, err := age.Decrypt(os.Stdin, id)
	fmt.Println(err)
	_, _ = io.Copy(os.Stdout, r)
}
EOF

measure nen
measure aws github.com/aws/aws-sdk-go-v2/service/s3@latest github.com/aws/aws-sdk-go-v2/credentials@latest
measure minio github.com/minio/minio-go/v7@latest
measure age filippo.io/age@latest

echo
echo "== hieu so file chay so voi nen (byte)"
base="$(wc -c <"$W/nen.bin" | tr -d ' ')"
for n in aws minio age; do
  s="$(wc -c <"$W/$n.bin" | tr -d ' ')"
  echo "   $n: +$((s - base)) byte ($(((s - base) / 1024)) KiB)"
done
