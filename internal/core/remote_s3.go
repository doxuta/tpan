package core

import (
	"github.com/doxuta/tpan/internal/s3"
	"github.com/doxuta/tpan/internal/steps"
)

// Gán client S3 thật cho mọi Remote không đặt NewStore (test đặt NewStore để
// dùng s3.Mem). Tách file riêng để remote_target.go (đợt 0) biên dịch được
// trước khi gói s3 có client.
func init() {
	newS3Store = func(cfg s3.Config) (steps.ObjectStore, error) { return s3.New(cfg) }
}
