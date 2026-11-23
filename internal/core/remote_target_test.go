package core

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/s3"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

// remoteTestStore mở một tpan.db trong thư mục tạm.
func remoteTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "tpan.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// setTarget ghi các setting của một đích hợp lệ.
func setTarget(t *testing.T, st Settings) {
	t.Helper()
	ctx := context.Background()
	for k, v := range map[string]string{
		SettingS3Endpoint: "https://s3.example.com", SettingS3Region: "vn-thu-1",
		SettingS3Bucket: "tpan-thu", SettingS3Prefix: "tpan/may", SettingS3Keep: "2",
	} {
		if err := st.SetSetting(ctx, k, v); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReadTarget(t *testing.T) {
	ctx := context.Background()
	st := remoteTestStore(t)
	if _, ok, err := readTarget(ctx, st); ok || err != nil {
		t.Fatalf("chưa đặt đích: ok=%v err=%v", ok, err)
	}
	setTarget(t, st)
	c, ok, err := readTarget(ctx, st)
	if err != nil || !ok {
		t.Fatalf("readTarget: ok=%v err=%v", ok, err)
	}
	if c.Region != "vn-thu-1" || c.Keep != 2 || !c.PathStyle || c.ID != s3.TargetID("https://s3.example.com", "tpan-thu", "tpan/may") {
		t.Fatalf("readTarget = %+v", c)
	}
	if id, ok, _ := CurrentTargetID(ctx, st); !ok || id != c.ID {
		t.Fatalf("CurrentTargetID = %q, %v", id, ok)
	}
	if err := st.SetSetting(ctx, SettingS3Keep, "0"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readTarget(ctx, st); err == nil {
		t.Fatal("s3.keep = 0 phải lỗi, không đoán")
	}
}

func TestReadKeysVaKhoaSaoLuu(t *testing.T) {
	sys := system.NewFake()
	if _, _, ok, err := readKeys(sys); ok || err != nil {
		t.Fatalf("chưa có file khoá: ok=%v err=%v", ok, err)
	}
	enc, err := encodeKeys("AKIAEXAMPLEKEY123", system.NewSecret("bi-mat-rat-dai-0123456789"))
	if err != nil {
		t.Fatal(err)
	}
	sys.SetFile(steps.S3KeysFile, []byte(enc.Reveal()), 0o600, system.Root)
	ak, sk, ok, err := readKeys(sys)
	if err != nil || !ok || ak != "AKIAEXAMPLEKEY123" || sk.Reveal() != "bi-mat-rat-dai-0123456789" {
		t.Fatalf("readKeys = %q, ok=%v, err=%v", ak, ok, err)
	}
	// File hỏng: lỗi không chép nội dung file.
	sys.SetFile(steps.S3KeysFile, []byte(`{"access_key":"x","secret_key":"BI-MAT-KHONG-DUOC-LO"`), 0o600, system.Root)
	if _, _, _, err := readKeys(sys); err == nil || strings.Contains(err.Error(), "BI-MAT") {
		t.Fatalf("file hỏng: %v", err)
	}
	// Symlink thì từ chối (không đi theo).
	sys.SetFile("/etc/khac", []byte("{}"), 0o600, system.Root)
	_ = sys.Remove(steps.S3KeysFile)
	sys.SetSymlink("/etc/khac", steps.S3KeysFile)
	if _, _, _, err := readKeys(sys); err == nil {
		t.Fatal("file khoá là symlink phải lỗi")
	}
	if _, ok, err := readBackupKey(sys); ok || err != nil {
		t.Fatalf("chưa có khoá sao lưu: ok=%v err=%v", ok, err)
	}
	sys.SetFile(steps.BackupKeyFile, []byte("tpan-key-1-abc\n"), 0o600, system.Root)
	if k, ok, err := readBackupKey(sys); err != nil || !ok || k.Reveal() != "tpan-key-1-abc" {
		t.Fatalf("readBackupKey = %v, %v", ok, err)
	}
}

func TestRemoteOpen(t *testing.T) {
	ctx := context.Background()
	st := remoteTestStore(t)
	sys := system.NewFake()
	mem := s3.NewMem()
	var got s3.Config
	r := &Remote{Sys: sys, Settings: st, NewStore: func(c s3.Config) (steps.ObjectStore, error) {
		got = c
		return mem, nil
	}}
	var re *RefusedError
	if _, _, err := r.open(ctx); !errors.As(err, &re) || re.Code != s3.CodeNotConfigured {
		t.Fatalf("chưa đặt đích: %v", err)
	}
	setTarget(t, st)
	if _, _, err := r.open(ctx); err == nil {
		t.Fatal("thiếu file khoá phải lỗi")
	}
	enc, _ := encodeKeys("AKIAEXAMPLEKEY123", system.NewSecret("bi-mat-rat-dai-0123456789"))
	sys.SetFile(steps.S3KeysFile, []byte(enc.Reveal()), 0o600, system.Root)
	store, c, err := r.open(ctx)
	if err != nil || store != mem || c.Bucket != "tpan-thu" || got.AccessKey != "AKIAEXAMPLEKEY123" || got.Prefix != "tpan/may" {
		t.Fatalf("open = %v, %+v, %v", store, c, err)
	}
	// Không NewStore, client thật chưa gán (đợt 0): lỗi rõ.
	if newS3Store == nil {
		if _, err := (&Remote{}).buildStore(s3.Config{}); err == nil {
			t.Fatal("không có client phải lỗi")
		}
	}
	if _, ok := (&Remote{}).locker().(system.FileLock); !ok || (&Remote{}).lockWait() != RemoteLockWait || (&Remote{}).now().IsZero() {
		t.Fatal("giá trị mặc định của Remote sai")
	}
}

func TestS3Refuse(t *testing.T) {
	var re *RefusedError
	if err := s3Refuse(&s3.FieldError{Code: s3.CodeBucketInvalid}); !errors.As(err, &re) || re.Code != s3.CodeBucketInvalid {
		t.Fatalf("FieldError: %v", err)
	}
	cases := map[int]string{403: s3.CodeAuthFailed, 503: s3.CodeUnreachable, 400: s3.CodeError}
	for status, want := range cases {
		if err := s3Refuse(&s3.Error{Op: "put", Status: status}); !errors.As(err, &re) || re.Code != want {
			t.Fatalf("status %d: %v", status, err)
		}
	}
	if err := s3Refuse(&s3.Error{Op: "check", Status: 404, Code: "NoSuchBucket"}); !errors.As(err, &re) || re.Code != s3.CodeBucketNotFound {
		t.Fatalf("NoSuchBucket: %v", err)
	}
	plain := errors.New("lỗi khác")
	if s3Refuse(plain) != plain || s3Refuse(nil) != nil {
		t.Fatal("lỗi khác phải trả nguyên")
	}
	if errNotConfigured().Code != s3.CodeNotConfigured {
		t.Fatal("errNotConfigured sai mã")
	}
	if c := s3Config(TargetConfig{Bucket: "b"}, "a", system.NewSecret("s"), nil, nil); c.Bucket != "b" || c.AccessKey != "a" {
		t.Fatal("s3Config sai")
	}
}
