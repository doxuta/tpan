package demo

// Phần M18 của máy giả (mục 6.16 của docs/KE-HOACH.md): dữ liệu cho các màn
// sao lưu ra kho S3 của `tpan serve --demo`.
//
//   - Đích: kho trong tên miền ví dụ (s3.example.com), bucket tpan-demo;
//   - gói panel mới nhất đã lên kho (setting s3.panel_last);
//   - backup của blog: bản cũ nhất chỉ còn trên kho, bản giữa ở cả hai nơi,
//     bản mới nhất chỉ trên máy; backup đầy đủ của shop đẩy lỗi.
//
// Kho là một s3.Mem trong bộ nhớ (S3), có đúng nội dung của các bản đã lên
// kho, nên các nút của giao diện (thử đích, lưu đích, tải về rồi khôi phục)
// chạy được mà không gọi ra mạng. Khoá truy cập và khoá sao lưu của demo SINH
// NGẪU NHIÊN mỗi lần chạy và chỉ nằm trong máy giả (FakeSystem): không khoá
// nào nằm trong repo. Bộ đẩy nền tắt ở chế độ demo.
//
// Các cột kho của bảng backups chỉ được đặt bằng hàm của store
// (MarkBackupStored, MarkBackupPushFailed, ForgetBackupLocal), đúng như mã thật
// làm, để dòng demo luôn hợp lệ (không có local=0 mà remote khác stored).

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"time"

	"github.com/doxuta/tpan/internal/agebox"
	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/s3"
	"github.com/doxuta/tpan/internal/steps"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
)

// Đích của máy demo. Tên miền ví dụ theo RFC 2606.
const (
	demoS3Endpoint = "https://s3.example.com"
	demoS3Bucket   = "tpan-demo"
	demoS3Prefix   = "tpan/demo"
	demoS3Keep     = "7"
)

// demoS3 là kho của lần New gần nhất (một tiến trình chỉ dựng một máy demo).
var demoS3 = s3.NewMem()

// S3 trả kho S3 giả của máy demo, cho server.Deps.S3 (cmd/tpan serve --demo).
func S3() *s3.Mem { return demoS3 }

// demoSecret sinh một chuỗi ngẫu nhiên n ký tự trong bảng chữ alphabet.
func demoSecret(n int, alphabet string) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b), nil
}

// seedM18Keys ghi hai khoá truy cập và khoá sao lưu vào máy giả, sinh mới mỗi
// lần chạy. Màn Backup hiện bốn ký tự cuối của access key và dấu vân tay của
// khoá sao lưu như trên máy thật.
func seedM18Keys(m *machine) error {
	access, err := demoSecret(20, "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	if err != nil {
		return err
	}
	secret, err := demoSecret(40, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	if err != nil {
		return err
	}
	keys, err := json.Marshal(map[string]string{"access_key": access, "secret_key": secret})
	if err != nil {
		return err
	}
	bk, err := agebox.GenerateKey()
	if err != nil {
		return err
	}
	if err := m.MkdirAll("/etc/tpan", 0o700, system.Root); err != nil {
		return err
	}
	for p, data := range map[string][]byte{steps.S3KeysFile: keys, steps.BackupKeyFile: []byte(bk.Reveal() + "\n")} {
		if err := m.WriteFileAtomic(p, data, 0o600, system.Root); err != nil {
			return fmt.Errorf("ghi %s của máy demo: %w", p, err)
		}
	}
	return nil
}

// seedM18 gọi sau seedM13: cần các dòng backups do seedM9 ghi.
func seedM18(ctx context.Context, m *machine, st *store.Store) error {
	demoS3 = s3.NewMem()
	if err := seedM18Keys(m); err != nil {
		return err
	}
	for k, v := range map[string]string{
		core.SettingS3Endpoint:  demoS3Endpoint,
		core.SettingS3Bucket:    demoS3Bucket,
		core.SettingS3Prefix:    demoS3Prefix,
		core.SettingS3Keep:      demoS3Keep,
		core.SettingS3PathStyle: "1",
	} {
		if err := st.SetSetting(ctx, k, v); err != nil {
			return fmt.Errorf("ghi setting %s: %w", k, err)
		}
	}
	now := time.Now().UTC().Truncate(time.Second)

	// Gói panel của lần đẩy đêm qua: "<tên>|<RFC 3339>|<sha256>". Không có
	// object thật; sha256 là của chính cái tên, đủ để màn hiện một chuỗi hợp lệ.
	panelAt := now.Add(-9 * time.Hour)
	panelName := "tpan-" + panelAt.Format("20060102-150405") + ".tar.gz.age"
	sum := sha256.Sum256([]byte(panelName))
	if err := st.SetSetting(ctx, core.SettingS3PanelLast,
		panelName+"|"+panelAt.Format(time.RFC3339)+"|"+hex.EncodeToString(sum[:])); err != nil {
		return fmt.Errorf("ghi setting %s: %w", core.SettingS3PanelLast, err)
	}

	target := s3.TargetID(demoS3Endpoint, demoS3Bucket, demoS3Prefix)

	// Backup đầy đủ của blog, mới nhất đứng đầu (store sắp created_at giảm dần).
	blog, err := fullBackups(ctx, st, siteBlog)
	if err != nil {
		return err
	}
	if len(blog) < 3 {
		return fmt.Errorf("M18 demo cần ít nhất 3 backup đầy đủ của %s, có %d", siteBlog, len(blog))
	}
	// Bản cũ nhất: đã lên kho, file trên máy đã bị dọn (chỉ còn trên kho).
	oldest := blog[len(blog)-1]
	if err := markStored(ctx, m, st, oldest, target); err != nil {
		return err
	}
	if _, err := st.ForgetBackupLocal(ctx, oldest.ID); err != nil {
		return fmt.Errorf("đánh dấu backup %s chỉ còn trên kho: %w", oldest.ID, err)
	}
	if err := m.Remove(oldest.Path); err != nil {
		return fmt.Errorf("gỡ file backup %s khỏi máy demo: %w", oldest.Path, err)
	}
	// Bản giữa: ở cả hai nơi. Bản mới nhất không đụng: chỉ trên máy.
	if err := markStored(ctx, m, st, blog[1], target); err != nil {
		return err
	}

	// Backup đầy đủ gần nhất của shop: lần đẩy lỗi. Lỗi của đích nên không
	// tính vào số lần thử của riêng bản (countTry=false).
	shop, err := fullBackups(ctx, st, siteShop)
	if err != nil {
		return err
	}
	if len(shop) == 0 {
		return fmt.Errorf("M18 demo cần một backup đầy đủ của %s", siteShop)
	}
	if err := st.MarkBackupPushFailed(ctx, shop[0].ID, target,
		"không nối được tới kho: dial tcp: connection refused", false); err != nil {
		return fmt.Errorf("đánh dấu backup %s đẩy lỗi: %w", shop[0].ID, err)
	}
	return nil
}

// fullBackups trả các backup đầy đủ của một site, mới nhất trước.
func fullBackups(ctx context.Context, st *store.Store, dom string) ([]core.Backup, error) {
	all, err := st.Backups(ctx, dom)
	if err != nil {
		return nil, fmt.Errorf("đọc backup của %s: %w", dom, err)
	}
	var out []core.Backup
	for _, b := range all {
		if b.Kind == core.BackupFull {
			out = append(out, b)
		}
	}
	return out, nil
}

// markStored ghi bản b đã lên kho ít phút sau lúc tạo, và đặt đúng nội dung
// file của nó vào kho giả (tải về rồi khôi phục chạy được, khớp sha256). Khoá
// object theo dạng của mã thật: sites/<domain>/<tên file>.
func markStored(ctx context.Context, m *machine, st *store.Store, b core.Backup, target string) error {
	key := "sites/" + b.Domain + "/" + path.Base(b.Path)
	data, err := m.ReadFile(b.Path)
	if err != nil {
		return fmt.Errorf("đọc file backup %s của máy demo: %w", b.Path, err)
	}
	demoS3.SetObject(key, data)
	ok, err := st.MarkBackupStored(ctx, b.ID, key, target, b.Created.Add(10*time.Minute))
	if err != nil {
		return fmt.Errorf("đánh dấu backup %s đã lên kho: %w", b.ID, err)
	}
	if !ok {
		return fmt.Errorf("đánh dấu backup %s đã lên kho: dòng không còn", b.ID)
	}
	return nil
}
