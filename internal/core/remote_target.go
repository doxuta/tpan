package core

// Đọc đích S3 và các khoá của M18 (đợt 0, phiên chính): settings cho phần
// không bí mật, ba file chỉ root đọc cho phần bí mật (mục 3.2, 3.3 của
// docs/thay-doi/M18-thiet-ke.md). Mọi ca sử dụng của remote.go, panel.go,
// backup.go dựng kho qua đây, để luật đọc khoá chỉ có một chỗ.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/s3"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/system"
)

// maxSecretFile: ba file bí mật đều nhỏ (khoá, CA tối đa 64 KB).
const maxSecretFile = 128 << 10

// newS3Store dựng client thật. nil cho tới khi gói s3 có client (đợt 1);
// remote_s3.go gán nó. Remote.NewStore (test) thắng.
var newS3Store func(cfg s3.Config) (steps.ObjectStore, error)

func (r *Remote) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Remote) locker() system.Locker {
	if r.Lock != nil {
		return r.Lock
	}
	return system.FileLock{Path: system.DefaultRemoteLockPath}
}

func (r *Remote) lockWait() time.Duration {
	if r.LockWait > 0 {
		return r.LockWait
	}
	return RemoteLockWait
}

// readTarget đọc phần không bí mật của đích từ settings. ok=false: chưa đặt
// đích (s3.endpoint rỗng). Giá trị hỏng trong DB là lỗi, không đoán.
func readTarget(ctx context.Context, st Settings) (TargetConfig, bool, error) {
	get := func(k string) (string, error) {
		v, _, err := st.GetSetting(ctx, k)
		if err != nil {
			return "", fmt.Errorf("đọc setting %s: %w", k, err)
		}
		return v, nil
	}
	ep, err := get(SettingS3Endpoint)
	if err != nil || ep == "" {
		return TargetConfig{}, false, err
	}
	var c TargetConfig
	if c.Endpoint, err = s3.CheckEndpoint(ep); err != nil {
		return TargetConfig{}, false, fmt.Errorf("setting %s hỏng: %w", SettingS3Endpoint, err)
	}
	region, err := get(SettingS3Region)
	if err != nil {
		return TargetConfig{}, false, err
	}
	if c.Region, err = s3.CheckRegion(region); err != nil {
		return TargetConfig{}, false, fmt.Errorf("setting %s hỏng: %w", SettingS3Region, err)
	}
	if c.Bucket, err = get(SettingS3Bucket); err != nil {
		return TargetConfig{}, false, err
	}
	if err := s3.CheckBucket(c.Bucket); err != nil {
		return TargetConfig{}, false, fmt.Errorf("setting %s hỏng: %w", SettingS3Bucket, err)
	}
	if c.Prefix, err = get(SettingS3Prefix); err != nil {
		return TargetConfig{}, false, err
	}
	if err := s3.CheckPrefix(c.Prefix); err != nil {
		return TargetConfig{}, false, fmt.Errorf("setting %s hỏng: %w", SettingS3Prefix, err)
	}
	ps, err := get(SettingS3PathStyle)
	if err != nil {
		return TargetConfig{}, false, err
	}
	c.PathStyle = ps != "0"
	keep, err := get(SettingS3Keep)
	if err != nil {
		return TargetConfig{}, false, err
	}
	c.Keep = s3.DefaultKeep
	if keep != "" {
		n, err := strconv.Atoi(keep)
		if err != nil || s3.CheckKeep(n) != nil {
			return TargetConfig{}, false, fmt.Errorf("setting %s = %q hỏng (cần 1..%d)", SettingS3Keep, keep, s3.MaxKeep)
		}
		c.Keep = n
	}
	c.ID = s3.TargetID(c.Endpoint, c.Bucket, c.Prefix)
	return c, true, nil
}

// Config đọc đích đang đặt. ok=false: chưa đặt.
func (r *Remote) Config(ctx context.Context) (TargetConfig, bool, error) {
	return readTarget(ctx, r.Settings)
}

// CurrentTargetID là mã của đích đang đặt; ok=false khi chưa đặt. Dùng ở
// Backups (khôi phục: bản này có ở đích đang đặt không) mà không cần dựng Remote.
func CurrentTargetID(ctx context.Context, st Settings) (string, bool, error) {
	c, ok, err := readTarget(ctx, st)
	return c.ID, ok, err
}

// readSecret đọc một file bí mật nhỏ. ok=false: không có file. File phải là
// file thường (Stat không đi theo symlink), cỡ tối đa maxSecretFile. Lỗi không
// bao giờ chép nội dung file.
func readSecret(sys system.System, path string) ([]byte, bool, error) {
	fi, err := sys.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("kiểm %s: %w", path, err)
	}
	if !fi.Mode.IsRegular() {
		return nil, false, fmt.Errorf("%s không phải file thường (%v)", path, fi.Mode.Type())
	}
	if fi.Size > maxSecretFile {
		return nil, false, fmt.Errorf("%s lớn bất thường (%d byte)", path, fi.Size)
	}
	b, err := sys.ReadFile(path)
	if err != nil {
		return nil, false, fmt.Errorf("đọc %s: %w", path, err)
	}
	return b, true, nil
}

// s3Keys là nội dung của steps.S3KeysFile.
type s3Keys struct {
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
}

// readKeys đọc hai khoá truy cập. ok=false: chưa có file.
func readKeys(sys system.System) (access string, secret system.Secret, ok bool, err error) {
	b, ok, err := readSecret(sys, steps.S3KeysFile)
	if err != nil || !ok {
		return "", system.Secret{}, ok, err
	}
	var k s3Keys
	if err := json.Unmarshal(b, &k); err != nil {
		// Không bọc lỗi của json: câu lỗi có thể chép một đoạn nội dung file.
		return "", system.Secret{}, false, fmt.Errorf("%s hỏng: không phải JSON {access_key, secret_key}", steps.S3KeysFile)
	}
	if s3.CheckAccessKey(k.AccessKey) != nil || s3.CheckSecretKey(k.SecretKey) != nil {
		return "", system.Secret{}, false, fmt.Errorf("%s hỏng: khoá sai dạng", steps.S3KeysFile)
	}
	return k.AccessKey, system.NewSecret(k.SecretKey), true, nil
}

// encodeKeys là nội dung sẽ ghi vào steps.S3KeysFile.
func encodeKeys(access string, secret system.Secret) (system.Secret, error) {
	b, err := json.Marshal(s3Keys{AccessKey: access, SecretKey: secret.Reveal()})
	if err != nil {
		return system.Secret{}, fmt.Errorf("dựng %s: %w", steps.S3KeysFile, err)
	}
	return system.NewSecret(string(b) + "\n"), nil
}

// readBackupKey đọc khoá sao lưu (một dòng). ok=false: chưa có. Kiểm dạng là
// việc của agebox.CheckKey ở nơi dùng.
func readBackupKey(sys system.System) (system.Secret, bool, error) {
	b, ok, err := readSecret(sys, steps.BackupKeyFile)
	if err != nil || !ok {
		return system.Secret{}, ok, err
	}
	k := strings.TrimSpace(string(b))
	if k == "" {
		return system.Secret{}, false, fmt.Errorf("%s đang trống", steps.BackupKeyFile)
	}
	return system.NewSecret(k), true, nil
}

// s3Config ghép đích và khoá thành cấu hình của client.
func s3Config(c TargetConfig, access string, secret system.Secret, ca []byte, now func() time.Time) s3.Config {
	return s3.Config{
		Endpoint: c.Endpoint, Region: c.Region, Bucket: c.Bucket, Prefix: c.Prefix,
		PathStyle: c.PathStyle, CAPEM: ca, AccessKey: access, SecretKey: secret, Now: now,
	}
}

// buildStore dựng kho từ cấu hình (Remote.NewStore, không thì client thật).
func (r *Remote) buildStore(cfg s3.Config) (steps.ObjectStore, error) {
	if r.NewStore != nil {
		return r.NewStore(cfg)
	}
	if newS3Store == nil {
		return nil, errors.New("bản tpan này chưa có client S3")
	}
	return newS3Store(cfg)
}

// open đọc đích đang đặt, hai khoá, CA rồi dựng kho. Chưa đặt đích:
// RefusedError s3_not_configured.
func (r *Remote) open(ctx context.Context) (steps.ObjectStore, TargetConfig, error) {
	c, ok, err := r.Config(ctx)
	if err != nil {
		return nil, TargetConfig{}, err
	}
	if !ok {
		return nil, TargetConfig{}, errNotConfigured()
	}
	access, secret, ok, err := readKeys(r.Sys)
	if err != nil {
		return nil, c, err
	}
	if !ok {
		return nil, c, fmt.Errorf("đích đã đặt nhưng thiếu %s; đặt lại bằng: sudo tpan backup target set …", steps.S3KeysFile)
	}
	ca, _, err := readSecret(r.Sys, steps.S3CAFile)
	if err != nil {
		return nil, c, err
	}
	store, err := r.buildStore(s3Config(c, access, secret, ca, r.Now))
	if err != nil {
		return nil, c, s3Refuse(err)
	}
	return store, c, nil
}

// errNotConfigured là lời từ chối khi chưa đặt đích.
func errNotConfigured() *RefusedError {
	return refuse(s3.CodeNotConfigured, i18n.T(
		"chưa đặt đích S3; đặt bằng: sudo tpan backup target set --endpoint https://… --bucket …",
		"no S3 target is set; set one with: sudo tpan backup target set --endpoint https://… --bucket …"))
}

// s3Refuse đổi lỗi của gói s3 thành RefusedError cùng mã: lỗi kiểm trường
// giữ câu của nó; lỗi của kho mang mã phân loại (s3.Classify) và câu đã làm
// sạch của client. Lỗi khác trả nguyên.
func s3Refuse(err error) error {
	if err == nil {
		return nil
	}
	var fe *s3.FieldError
	if errors.As(err, &fe) {
		return refuse(fe.Code, fe.Msg)
	}
	var se *s3.Error
	if !errors.As(err, &se) {
		return err
	}
	code := s3.Classify(err)
	msg := se.Error()
	switch code {
	case s3.CodeAuthFailed:
		return refuse(code, i18n.T("kho từ chối khoá truy cập: "+msg, "the store rejected the access keys: "+msg))
	case s3.CodeBucketNotFound:
		return refuse(code, i18n.T("kho không có bucket này: "+msg, "the store has no such bucket: "+msg))
	case s3.CodeUnreachable:
		return refuse(code, i18n.T("không nối được tới kho: "+msg, "cannot reach the store: "+msg))
	}
	return refuse(s3.CodeError, i18n.T("kho trả lỗi: "+msg, "the store returned an error: "+msg))
}
