package steps

// Các bước nói chuyện với kho S3 và hai bước ghi, gỡ file bí mật (M18, mục
// 3.2, 3.3, 3.5, 3.6 của docs/thay-doi/M18-thiet-ke.md). Hợp đồng và luật
// chung ở m18_types.go.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/s3"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

var (
	_ engine.Step = (*SecretFile)(nil)
	_ engine.Step = (*SecretAbsent)(nil)
	_ engine.Step = (*S3Probe)(nil)
	_ engine.Step = (*S3Push)(nil)
	_ engine.Step = (*S3Pull)(nil)
)

// ================================================================ Bước bí mật

func secretPathOK(p string) error {
	if !IsSecretPath(p) {
		return fmt.Errorf("%q không phải một file bí mật của tpan (%s, %s, %s)", p, S3KeysFile, S3CAFile, BackupKeyFile)
	}
	return nil
}

// inner dựng *File một lần rồi giữ: nội dung cũ để Undo nằm trong nó.
func (s *SecretFile) inner() *File {
	if s.file == nil {
		s.file = &File{Path: s.Path, Content: []byte(s.Content.Reveal()), Perm: 0o600, Owner: system.Root, secret: true}
	}
	return s.file
}

// Info tự dựng, không gọi inner().Info(): Info của File khai nội dung mới
// vào Files, tức là vào bản xem trước và managed_files.
func (s *SecretFile) Info() engine.StepInfo {
	return engine.StepInfo{
		ID:    "secret.file:" + s.Path,
		Title: orText(s.Title, i18n.T("Ghi file bí mật "+s.Path, "Write secret file "+s.Path)),
		Why: orText(s.Why, i18n.T(
			"File này giữ khoá tpan cần để nói chuyện với kho ngoài máy hoặc để mã hoá gói panel. Thiếu nó thì sao lưu ra kho dừng. Chỉ root đọc được file (0600), và nội dung không hiện trong bản xem trước hay log job.",
			"This file holds a key tpan needs to talk to the off-machine store or to encrypt the panel bundle. Without it, off-machine backups stop. Only root can read it (0600), and its content never shows in the preview or the job log.",
		)),
	}
}

func (s *SecretFile) Check(ctx context.Context, sys system.System) (bool, error) {
	if err := secretPathOK(s.Path); err != nil {
		return false, err
	}
	return s.inner().Check(ctx, sys)
}

func (s *SecretFile) Apply(ctx context.Context, sys system.System) error {
	if err := secretPathOK(s.Path); err != nil {
		return err
	}
	return s.inner().Apply(ctx, sys)
}

func (s *SecretFile) Undo(ctx context.Context, sys system.System) error {
	if s.file == nil {
		return fmt.Errorf("file bí mật %s: %w", s.Path, errNotApplied)
	}
	return s.file.Undo(ctx, sys)
}

func (s *SecretAbsent) inner() *Absent {
	if s.absent == nil {
		s.absent = &Absent{Path: s.Path, secret: true}
	}
	return s.absent
}

// Info không khai Files: engine đọc nội dung cũ của mọi file trong Files vào
// bản xem trước (trường old), tức là đọc khoá ra API (S1 của bản soát).
func (s *SecretAbsent) Info() engine.StepInfo {
	return engine.StepInfo{
		ID:    "secret.absent:" + s.Path,
		Title: orText(s.Title, i18n.T("Gỡ file bí mật "+s.Path, "Remove secret file "+s.Path)),
		Why: orText(s.Why, i18n.T(
			"Gỡ đích hay khoá mà để lại file này thì khoá vẫn nằm trên máy dù tpan không còn dùng tới. Nội dung cũ chỉ được giữ trong bộ nhớ để hoàn tác, không hiện trong bản xem trước.",
			"Removing the target or key but leaving this file keeps the key on the machine although tpan no longer uses it. The old content is kept only in memory for undo and never shows in the preview.",
		)),
	}
}

func (s *SecretAbsent) Check(ctx context.Context, sys system.System) (bool, error) {
	if err := secretPathOK(s.Path); err != nil {
		return false, err
	}
	return s.inner().Check(ctx, sys)
}

func (s *SecretAbsent) Apply(ctx context.Context, sys system.System) error {
	if err := secretPathOK(s.Path); err != nil {
		return err
	}
	return s.inner().Apply(ctx, sys)
}

func (s *SecretAbsent) Undo(ctx context.Context, sys system.System) error {
	if s.absent == nil {
		return fmt.Errorf("gỡ file bí mật %s: %w", s.Path, errNotApplied)
	}
	return s.absent.Undo(ctx, sys)
}

// ================================================================== Tiến độ

// progress ghi tiến độ của một lần chuyển dữ liệu bằng engine.Note: mỗi khi
// vượt thêm một bậc 10%, hoặc sau 30 giây kể từ dòng trước (tải chậm thì
// người xem vẫn biết bước còn sống). Client thật có thể gọi update từ nhiều
// goroutine (tải nhiều phần song song) nên có khoá.
type progress struct {
	mu    sync.Mutex
	sys   system.System
	what  string
	total int64
	next  int64 // bậc phần trăm kế tiếp sẽ in
	last  time.Time
}

func newProgress(sys system.System, what string, total int64) *progress {
	return &progress{sys: sys, what: what, total: total, next: 10, last: time.Now()}
}

func (p *progress) update(done int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	pct := int64(100)
	if p.total > 0 {
		pct = done * 100 / p.total
	}
	now := time.Now()
	if pct < p.next && now.Sub(p.last) < 30*time.Second {
		return
	}
	for p.next <= pct {
		p.next += 10
	}
	p.last = now
	engine.Note(p.sys, fmt.Sprintf("%s: %d%% (%d / %d byte)", p.what, pct, done, p.total))
}

// countWriter đếm byte đi qua để báo tiến độ lúc đọc lại hay tải về (Get
// không có hàm tiến độ riêng).
type countWriter struct {
	w  io.Writer
	n  int64
	fn func(int64)
	// max > 0: nhận quá chừng này byte thì lỗi errObjectTooLong (F1 của lượt
	// soát cuối). Cỡ của object đã biết trước (cột size, hay số byte vừa
	// gửi); kho độc hại hay hỏng gửi thân dài vô hạn thì không được làm đầy
	// đĩa gốc, cũng không được giữ lượt đẩy mãi.
	max int64
}

// errObjectTooLong: kho gửi nhiều byte hơn cỡ đã biết của object.
var errObjectTooLong = errors.New("kho gửi nhiều byte hơn cỡ đã biết của object")

func (c *countWriter) Write(b []byte) (int, error) {
	if c.max > 0 && c.n+int64(len(b)) > c.max {
		return 0, fmt.Errorf("%w (%d byte)", errObjectTooLong, c.max)
	}
	n, err := c.w.Write(b)
	c.n += int64(n)
	c.fn(c.n)
	return n, err
}

// ================================================================== S3Probe

func (s *S3Probe) Info() engine.StepInfo {
	return engine.StepInfo{
		ID:    "s3.probe:" + s.Label,
		Title: i18n.T("Thử ghi, đọc, xoá trên kho "+s.Label, "Test write, read and delete on store "+s.Label),
		Why: i18n.T(
			"Đặt đích mà không thử thật thì sai khoá, thiếu quyền ghi hay thiếu quyền xoá chỉ lộ ra ở đêm sao lưu đầu tiên, lúc đã muộn. Bước này ghi một object nhỏ, đọc lại so từng byte rồi xoá, để biết chắc tpan đẩy lên và dọn được trên kho này.",
			"Setting a target without a real test means a wrong key or a missing write or delete permission only shows on the first night's backup, when it is too late. This step writes a small object, reads it back byte for byte, then deletes it, so tpan knows it can upload to and clean up this store.",
		),
		NoUndo: i18n.T(
			"Bước tự xoá object thử ngay trong lúc chạy; không còn gì để hoàn tác.",
			"The step deletes its test object while it runs; nothing is left to undo.",
		),
	}
}

func (s *S3Probe) validate() error {
	if s.Store == nil {
		return errors.New("thử kho: chưa có kho")
	}
	if s.Label == "" || strings.ContainsFunc(s.Label, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return fmt.Errorf("thử kho: nhãn %q rỗng hoặc có ký tự điều khiển", s.Label)
	}
	return nil
}

// Check chỉ đúng sau Apply của chính bước: "đã thử lúc nào đó" không phải
// trạng thái của máy.
func (s *S3Probe) Check(context.Context, system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	return s.applied, nil
}

func (s *S3Probe) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	var buf [8 + 48]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Errorf("thử kho: sinh object thử: %w", err)
	}
	key := "probe/" + hex.EncodeToString(buf[:8])
	data := buf[8:]
	// fail cố xoá object thử rồi báo lỗi; lỗi của kho được bọc bằng %w nên
	// errors.As(err, *s3.Error) của core vẫn dùng được.
	fail := func(what string, err error) error {
		err = fmt.Errorf("thử kho %s: %s %s: %w", s.Label, what, key, err)
		if derr := s.Store.Delete(ctx, key); derr != nil {
			return errors.Join(err, fmt.Errorf("xoá object thử %s: %w", key, derr))
		}
		return err
	}
	if _, err := s.Store.Put(ctx, key, bytes.NewReader(data), int64(len(data)), nil); err != nil {
		return fail("ghi", err)
	}
	obj, err := s.Store.Head(ctx, key)
	if err != nil {
		return fail("xem", err)
	}
	if obj.Size != int64(len(data)) {
		return fail("xem", fmt.Errorf("kho báo cỡ %d, đã ghi %d byte", obj.Size, len(data)))
	}
	var got bytes.Buffer
	if _, _, err := s.Store.Get(ctx, key, &countWriter{w: &got, fn: func(int64) {}, max: int64(len(data))}); err != nil {
		return fail("đọc lại", err)
	}
	if !bytes.Equal(got.Bytes(), data) {
		return fail("đọc lại", errors.New("kho trả về dữ liệu khác thứ đã ghi"))
	}
	if err := s.Store.Delete(ctx, key); err != nil {
		return fmt.Errorf("thử kho %s: xoá %s: %w", s.Label, key, err)
	}
	// Một số kho trả thành công cho DELETE mà không xoá (thiếu quyền xoá
	// nhưng chính sách nuốt lỗi). Không xoá được thì dọn kho cũng không chạy.
	_, err = s.Store.Head(ctx, key)
	switch {
	case err == nil:
		return fmt.Errorf("thử kho %s: %s vẫn còn sau khi xoá; khoá này có thể thiếu quyền xoá", s.Label, key)
	case !errors.Is(err, s3.ErrNotFound):
		return fmt.Errorf("thử kho %s: xem lại %s sau khi xoá: %w", s.Label, key, err)
	}
	engine.Note(sys, "đã thử kho "+s.Label+": ghi, đọc lại khớp từng byte, xoá đều đạt")
	s.applied = true
	return nil
}

func (s *S3Probe) Undo(context.Context, system.System) error { return nil }

// =================================================================== S3Push

// pushLocal: Path của bước S3Push phải là file sao lưu trong thư mục backup do
// chính tpan ghi (root 0700). Bước chạy bằng root đọc được mọi file; không
// nhận đường dẫn nào khác (mục 5 của thiết kế).
func pushLocal(p string) error {
	if err := checkPath("file cần đẩy", p); err != nil {
		return err
	}
	if !strings.HasPrefix(p, site.BackupRoot+"/") {
		return fmt.Errorf("file cần đẩy %q không nằm dưới %s", p, site.BackupRoot)
	}
	return nil
}

func (s *S3Push) validate() error {
	if s.Store == nil {
		return errors.New("đẩy lên kho: chưa có kho")
	}
	if s.ID == "" || strings.ContainsFunc(s.ID, func(r rune) bool { return r <= 0x20 || r == 0x7f }) {
		return fmt.Errorf("đẩy lên kho: id %q rỗng hoặc có khoảng trắng, ký tự điều khiển", s.ID)
	}
	return nil
}

// source trả đường dẫn, cỡ, sha256, key sẽ dùng. From khác nil: lấy từ kết
// quả của PanelBundle (chỉ có sau Apply của nó).
func (s *S3Push) source() (p string, size int64, sum, key string, err error) {
	if s.From == nil {
		p, size, sum, key = s.Path, s.Size, s.SHA256, s.Key
	} else {
		if !s.From.applied {
			return "", 0, "", "", errors.New("đẩy gói panel: bước tạo gói chưa chạy xong")
		}
		p, size, sum, key = s.From.Path, s.From.Size, s.From.SHA256, "panel/"+s.From.Name
	}
	switch err = pushLocal(p); {
	case err != nil:
	case size < 0:
		err = fmt.Errorf("cỡ %d âm", size)
	case !sha256Re.MatchString(sum):
		err = fmt.Errorf("sha256 %q không đúng dạng (64 ký tự hex thường)", sum)
	default:
		err = s3.CheckKey(key)
	}
	return p, size, sum, key, err
}

func (s *S3Push) Info() engine.StepInfo {
	// Có From thì không đọc s.Path, s.Key: Apply ghi vào đó, mà Info phải
	// giống hệt nhau giữa hai lần dựng Plan (dấu vân tay lúc duyệt).
	p, key := s.Path, s.Key
	if s.From != nil {
		p, key = s.From.Dir+"/"+PanelNameLater, "panel/"+PanelNameLater
	}
	return engine.StepInfo{
		ID:    "s3.push:" + s.ID,
		Title: i18n.T("Tải "+p+" lên kho ("+key+")", "Upload "+p+" to the store ("+key+")"),
		Why: i18n.T(
			"Bản sao lưu chỉ nằm trên máy thì mất máy là mất luôn bản sao lưu. Bước này đưa bản ra kho ngoài máy rồi đọc lại cả object, so sha256 với lúc tạo; chỉ khi khớp thì bản mới được ghi là đã lên kho.",
			"A backup that only lives on the machine is lost with the machine. This step uploads it to the off-machine store, then reads the whole object back and compares its sha256 with the one recorded at creation; only a match marks it as stored.",
		),
		Commands: []string{sha256Cmd(p).String()},
	}
}

// Check chỉ đúng sau Apply của chính bước: object có sẵn trên kho chưa nói
// gì về việc nó đã được đọc lại khớp sha256 (B2). Không kiểm Path, Size,
// SHA256 ở đây: với From, chúng chỉ có sau Apply của PanelBundle, mà engine
// gọi Check trước đó (lúc xem trước và ngay trước Apply).
func (s *S3Push) Check(context.Context, system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	return s.applied, nil
}

// notImplemented: kho không có lời gọi này (MinIO cũ, vài kho rẻ).
func notImplemented(err error) bool {
	var e *s3.Error
	return errors.As(err, &e) && (e.Status == 501 || e.Code == "NotImplemented")
}

// localErr bọc lỗi của bản cục bộ bằng ErrBackupLocal mà vẫn giữ lỗi gốc.
func localErr(format string, a ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrBackupLocal}, a...)...)
}

func (s *S3Push) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	p, size, sum, key, err := s.source()
	if err != nil {
		return fmt.Errorf("đẩy lên kho: %w", err)
	}
	// 1. File thường, đúng cỡ. Stat không đi theo symlink.
	fi, err := sys.Stat(p)
	if err != nil {
		return localErr("kiểm %s: %w", p, err)
	}
	if !fi.Mode.IsRegular() {
		return localErr("%s không phải file thường (%v)", p, fi.Mode.Type())
	}
	if fi.Size != size {
		return localErr("file cục bộ đã đổi: %s có %d byte, lúc tạo %d byte", p, fi.Size, size)
	}
	// 2. sha256sum là lệnh, nên có bằng chứng trong log job. Sai thì không
	// gửi byte nào: đẩy một bản hỏng lên kho chỉ tạo cảm giác an toàn giả.
	got, err := sha256Of(ctx, sys, p)
	if err != nil {
		return localErr("%w", err)
	}
	if got != sum {
		return localErr("bản cục bộ đã hỏng: sha256 của %s là %s, lúc tạo là %s", p, got, sum)
	}
	// 3. Dọn phần tải dở của chính key này (lần chạy trước bị giết giữa chừng).
	if _, err := s.Store.AbortUploads(ctx, key, 0); err != nil && !notImplemented(err) {
		return err
	}
	// drop xoá object vừa ghi khi nó không được dùng; lỗi xoá nối vào lỗi chính.
	drop := func(err error) error {
		if derr := s.Store.Delete(ctx, key); derr != nil {
			return errors.Join(err, fmt.Errorf("xoá object %s: %w", key, derr))
		}
		return err
	}
	// 4. Tải lên, theo dòng.
	r, n, err := sys.OpenRead(p)
	if err != nil {
		return localErr("mở %s: %w", p, err)
	}
	defer func() { _ = r.Close() }()
	if n != size {
		return localErr("file cục bộ đã đổi: %s có %d byte lúc mở, lúc tạo %d byte", p, n, size)
	}
	up := newProgress(sys, "tải "+key+" lên kho", size)
	sent, err := s.Store.Put(ctx, key, r, size, up.update)
	if err != nil {
		return err
	}
	if sent != sum {
		return drop(localErr("file đổi trong lúc tải lên: sha256 đã gửi %s, lúc tạo %s", sent, sum))
	}
	// 5. Đọc lại cả object: phép kiểm duy nhất không phải tin kho (mục 3.5).
	down := newProgress(sys, "đọc lại "+key+" từ kho", size)
	rn, rsum, err := s.Store.Get(ctx, key, &countWriter{w: io.Discard, fn: down.update, max: size})
	if errors.Is(err, errObjectTooLong) {
		// Kho trả dài hơn thứ đã gửi: cùng loại với sai sha256.
		return drop(fmt.Errorf("%w: đọc lại %s: %w", ErrRemoteMismatch, key, err))
	}
	if err != nil {
		// Object chưa qua đọc lại thì không để trên kho: không ai được
		// tưởng nó là một bản tốt.
		return drop(err)
	}
	if rn != size || rsum != sum {
		return drop(fmt.Errorf("%w: đọc lại %s được %d byte, sha256 %s; đã gửi %d byte, sha256 %s",
			ErrRemoteMismatch, key, rn, rsum, size, sum))
	}
	// ETag chỉ để hiển thị: Put không trả nó. Head lỗi không làm hỏng một bản
	// đã đọc lại khớp sha256, nên chỉ ghi một dòng và để trống.
	etag := ""
	if obj, err := s.Store.Head(ctx, key); err == nil {
		etag = obj.ETag
	} else {
		engine.Note(sys, "không lấy được ETag của "+key+": "+err.Error())
	}
	// 6. Dòng kết quả.
	engine.Note(sys, fmt.Sprintf("đã tải %s lên kho: %d byte, đọc lại khớp sha256 %s", key, size, sum))
	if s.From != nil {
		s.Path, s.Size, s.SHA256, s.Key = p, size, sum, key
	}
	s.ETag, s.applied = etag, true
	return nil
}

func (s *S3Push) Undo(ctx context.Context, _ system.System) error {
	if !s.applied {
		return fmt.Errorf("đẩy %s: %w", s.ID, errNotApplied)
	}
	if err := s.Store.Delete(ctx, s.Key); err != nil {
		return err
	}
	s.applied = false
	return nil
}

// =================================================================== S3Pull

func (s *S3Pull) validate() error {
	if s.Store == nil {
		return errors.New("tải từ kho: chưa có kho")
	}
	if s.ID == "" || strings.ContainsFunc(s.ID, func(r rune) bool { return r <= 0x20 || r == 0x7f }) {
		return fmt.Errorf("tải từ kho: id %q rỗng hoặc có khoảng trắng, ký tự điều khiển", s.ID)
	}
	if err := checkPath("thư mục backup", s.Dir); err != nil {
		return err
	}
	// Dir là site.BackupDir của một domain: ngay dưới BackupRoot. Bước tạo
	// thư mục và file bằng root ở đây, nên không nhận chỗ nào khác.
	if path.Dir(s.Dir) != site.BackupRoot {
		return fmt.Errorf("thư mục backup %q không nằm ngay dưới %s", s.Dir, site.BackupRoot)
	}
	if err := checkPath("file đích", s.Dest); err != nil {
		return err
	}
	if path.Dir(s.Dest) != s.Dir {
		return fmt.Errorf("file đích %q không nằm ngay dưới %s", s.Dest, s.Dir)
	}
	if s.Size <= 0 {
		return fmt.Errorf("cỡ %d không hợp lệ", s.Size)
	}
	if !sha256Re.MatchString(s.SHA256) {
		return fmt.Errorf("sha256 %q không đúng dạng (64 ký tự hex thường)", s.SHA256)
	}
	return s3.CheckKey(s.Key)
}

func (s *S3Pull) Info() engine.StepInfo {
	return engine.StepInfo{
		ID:    "s3.pull:" + s.ID,
		Title: i18n.T("Tải "+s.Key+" từ kho về "+s.Dest, "Download "+s.Key+" from the store to "+s.Dest),
		Why: i18n.T(
			"Khôi phục chỉ đọc file trên máy, mà bản này đang nằm trên kho. Bước tải về rồi so sha256 với giá trị ghi lúc tạo (lưu trên máy, không lấy từ kho); sai thì không để lại gì, để không bao giờ khôi phục một bản đã bị sửa trên kho.",
			"Restore only reads files on the machine, and this copy is on the store. The step downloads it and compares the sha256 with the value recorded at creation (kept on the machine, not taken from the store); on a mismatch nothing is left behind, so a copy altered on the store is never restored.",
		),
		Commands: []string{sha256Cmd(s.Dest).String()},
	}
}

// matches: Dest là file thường, đúng cỡ, đúng sha256.
func (s *S3Pull) matches(ctx context.Context, sys system.System) (bool, error) {
	fi, err := sys.Stat(s.Dest)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("kiểm %s: %w", s.Dest, err)
	}
	if !fi.Mode.IsRegular() || fi.Size != s.Size {
		return false, nil
	}
	sum, err := sha256Of(ctx, sys, s.Dest)
	if err != nil {
		return false, err
	}
	return sum == s.SHA256, nil
}

// Check theo trạng thái (S8): bản đã có đúng trên máy thì không tải lại.
func (s *S3Pull) Check(ctx context.Context, sys system.System) (bool, error) {
	if err := s.validate(); err != nil {
		return false, err
	}
	return s.matches(ctx, sys)
}

func (s *S3Pull) Apply(ctx context.Context, sys system.System) error {
	if err := s.validate(); err != nil {
		return err
	}
	du, err := statFSNear(sys, s.Dir)
	if err != nil {
		return err
	}
	if need := uint64(s.Size); !SpaceOK(du.Free, du.Total, need) {
		return spaceErr("tải "+s.Key+" về", need, du.Free, du.Total)
	}
	if err := backupMkdirs(sys, site.BackupRoot, s.Dir); err != nil {
		return err
	}
	part := s.Dest + ".part"
	if err := removeIfExists(sys, part); err != nil {
		return err
	}
	// Dest có mà sai (lần tải trước hỏng, hay bị sửa): xoá. Thư mục chỉ root
	// ghi, tên do tpan đặt. Undo không trả lại bản sai đó: nó vô dụng.
	if ok, err := s.matches(ctx, sys); err != nil {
		return err
	} else if ok {
		s.applied = true
		return nil
	}
	if err := removeIfExists(sys, s.Dest); err != nil {
		return err
	}
	fail := func(err error) error { return errors.Join(err, removeIfExists(sys, part)) }
	w, err := sys.CreateFile(part, 0o600, system.Root)
	if err != nil {
		return fmt.Errorf("tạo %s: %w", part, err)
	}
	down := newProgress(sys, "tải "+s.Key+" về", s.Size)
	n, sum, err := s.Store.Get(ctx, s.Key, &countWriter{w: w, fn: down.update, max: s.Size})
	// Close đồng bộ xuống đĩa: lỗi của nó là lỗi ghi, phải kiểm.
	if cerr := w.Close(); err == nil && cerr != nil {
		err = fmt.Errorf("ghi %s: %w", part, cerr)
	}
	if err != nil {
		return fail(err)
	}
	if n != s.Size || sum != s.SHA256 {
		return fail(fmt.Errorf("bản trên kho không khớp sha256 ghi lúc tạo: tải về %s (%d byte), mong %s (%d byte)", sum, n, s.SHA256, s.Size))
	}
	if err := sys.Rename(part, s.Dest); err != nil {
		return fail(fmt.Errorf("đổi tên %s thành %s: %w", part, s.Dest, err))
	}
	engine.Note(sys, fmt.Sprintf("đã tải %s về %s: %d byte, khớp sha256 %s", s.Key, s.Dest, n, sum))
	s.created, s.applied = true, true
	return nil
}

func (s *S3Pull) Undo(_ context.Context, sys system.System) error {
	if !s.applied {
		return fmt.Errorf("tải %s: %w", s.ID, errNotApplied)
	}
	if s.created {
		if err := removeIfExists(sys, s.Dest); err != nil {
			return err
		}
	}
	s.created, s.applied = false, false
	return nil
}
