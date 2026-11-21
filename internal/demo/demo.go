package demo

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/doxuta/tpan/internal/core"
	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/store"
	"github.com/doxuta/tpan/internal/system"
	"github.com/doxuta/tpan/internal/validate"
)

// Các site của máy demo. Domain dùng tên dành riêng cho ví dụ (RFC 2606).
const (
	siteBlog = "blog.example.com" // có SSL, đạt mọi chặng
	siteShop = "shop.example.net" // chưa có SSL, đạt mọi chặng
	siteWiki = "wiki.example.org" // hỏng ở chặng PHP: socket của pool mất
	siteFail = "news.example.com" // lần tạo thất bại, đã hoàn tác (không có trong bảng sites)
	// siteOld (trash.go) đang ở thùng rác.

	// Ba site tên dài (M16, mục 3.9 của docs/thay-doi/M16-thiet-ke.md) để thử
	// các màn có tên site chạy sát lề: bản đồ tuyến, bảng site, menu. Tạo ở
	// seedM13 vì hai trong ba site thuộc user mà seedM13 mới tạo.
	siteLongShop = "cua-hang-so-mot.khach-hang-vi-du.example.com" // 44 ký tự, của khach_a
	siteLongEdu  = "hoc-truc-tuyen.lop-hoc-a.example.net"         // 36 ký tự, của admin
	siteLongNews = "tin-tuc.to-soan-bao-vi-du.example.org"        // 37 ký tự, của khach_b

	// demoEmail là email quản trị của các site mẫu.
	demoEmail = "admin@example.com"
)

// NewSites dựng core.Sites cho máy demo. KHÔNG dựng core.Sites trống cho demo:
// core ghim SHA-1 của bản WordPress thật, mà tarball giả của máy demo không thể
// trùng nó, nên tạo site trên giao diện demo sẽ hỏng ở bước tải WordPress.
// Hàm này đặt mã băm của tarball giả. Journal là st: job và sự kiện vào lịch
// sử như trên máy thật.
// Hashes trả mã băm của tarball WordPress và WP-CLI giả trên máy demo, để
// server dựng core.Sites và core.Stack giống NewSites, NewStack.
func Hashes() (wordPress, wpcli string) { return wordPressSHA1(), wpCLISHA512() }

func NewSites(sys system.System, st *store.Store) *core.Sites {
	return &core.Sites{Sys: sys, Settings: st, Store: st, Journal: st, Backups: st, WPHash: wordPressSHA1()}
}

// NewStack dựng core.Stack cho máy demo, vì cùng lý do với NewSites: phép kiểm
// WP-CLI so sha512 của /usr/local/bin/wp với mã đã ghim, nên phải đặt mã của
// WP-CLI giả thì stack mới báo đạt.
func NewStack(sys system.System, st *store.Store) *core.Stack {
	return &core.Stack{Sys: sys, Settings: st, Journal: st, WPCLISHA512: wpCLISHA512()}
}

// New dựng máy giả và DB trong thư mục dir (tạo nếu chưa có; dir rỗng thì dùng
// một thư mục tạm), như doc.go mô tả.
//
// ctx sống bao lâu thì số liệu /proc của máy giả còn nhúc nhích bấy lâu: hết
// ctx thì goroutine cập nhật dừng, số liệu đứng yên (vẫn đọc được). Nên truyền
// ctx của cả tiến trình `tpan serve`, không phải ctx của một request.
//
// dir không được có sẵn tpan.db. Máy giả nằm trong bộ nhớ và dựng lại từ đầu mỗi
// lần, còn DB cũ ghi nhớ các site của lần trước: ghép hai thứ lệch nhau cho ra
// lịch sử job không khớp máy. Cũng vì vậy hàm không xoá DB cũ hộ: nếu dir lỡ
// là thư mục của DB thật thì xoá là mất dữ liệu.
func New(ctx context.Context, dir string) (*system.FakeSystem, *store.Store, error) {
	if dir == "" {
		tmp, err := os.MkdirTemp("", "tpan-demo-")
		if err != nil {
			return nil, nil, fmt.Errorf("tạo thư mục tạm cho demo: %w", err)
		}
		dir = tmp
	}
	dbPath := filepath.Join(dir, "tpan.db")
	if _, err := os.Stat(dbPath); err == nil {
		return nil, nil, fmt.Errorf("thư mục demo %s đã có tpan.db của lần chạy trước; dùng thư mục trống (hoặc bỏ --data-dir để dùng thư mục tạm)", dir)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, nil, fmt.Errorf("kiểm %s: %w", dbPath, err)
	}
	st, err := store.Open(ctx, dbPath)
	if err != nil {
		return nil, nil, fmt.Errorf("mở DB demo: %w", err)
	}
	m := newMachine()
	if err := seed(ctx, m, st); err != nil {
		_ = st.Close()
		return nil, nil, fmt.Errorf("dựng dữ liệu demo: %w", err)
	}
	go m.runProc(ctx)
	return m.FakeSystem, st, nil
}

// seed ghi cấu hình stack rồi tạo các site mẫu bằng chính core.Sites, để lịch
// sử job và sự kiện là thật chứ không bịa.
func seed(ctx context.Context, m *machine, st *store.Store) error {
	// Stack như vừa cài xong bằng `tpan stack install --web nginx` (mục 6.1).
	for k, v := range map[string]string{
		core.SettingWeb:        string(core.Nginx),
		core.SettingWebSource:  string(core.SourceUbuntu),
		core.SettingDBSource:   string(core.SourceUbuntu),
		core.SettingPHPSource:  string(core.SourceUbuntu),
		core.SettingPHPVersion: phpVer,
	} {
		if err := st.SetSetting(ctx, k, v); err != nil {
			return fmt.Errorf("ghi setting %s: %w", k, err)
		}
	}

	// M9 (m9.go): lệnh giả của backup, cron, thao tác DB và dữ liệu mẫu. Gọi
	// trước khi tạo site: wp-config.php của site mẫu phải có các dòng DB_*.
	if err := seedM9(ctx, m, st); err != nil {
		return err
	}

	// Thứ tự theo thời gian; ago là "cách đây bao lâu", để lịch sử có ngày khác
	// nhau thay vì tất cả cùng một giây. Lần thất bại nằm giữa hai site đạt.
	s := NewSites(m, st)
	if err := addOK(ctx, s, siteBlog, "Blog của tôi", 21*24*time.Hour); err != nil {
		return err
	}
	if err := addOK(ctx, s, siteShop, "Cửa hàng", 9*24*time.Hour); err != nil {
		return err
	}
	if err := addRolledBack(ctx, m, s, siteFail, 5*24*time.Hour); err != nil {
		return err
	}
	// Màn Thùng rác: một site tạo từ 30 ngày trước, vào thùng rác 3 ngày trước.
	if err := seedTrash(ctx, s, 30*24*time.Hour); err != nil {
		return err
	}
	if err := addOK(ctx, s, siteWiki, "Wiki nội bộ", 2*24*time.Hour); err != nil {
		return err
	}
	// Màn SSL: blog đã có chứng chỉ, shop chưa có (bấm xin được ngay).
	if err := seedSSL(ctx, m, st, siteBlog); err != nil {
		return err
	}
	// M13 (m13.go): gói, user, chủ của site, site tạm khoá, DB rời.
	if err := seedM13(ctx, m, st, s); err != nil {
		return err
	}
	// M18 (m18.go): đích S3 và backup ở đủ trạng thái trên máy và trên kho.
	if err := seedM18(ctx, m, st); err != nil {
		return err
	}

	// Làm hỏng sau cùng: hoàn tác ở lần thất bại ở trên reload PHP-FPM, và mọi
	// lần reload đều dựng lại socket.
	l, err := layoutOf(siteWiki)
	if err != nil {
		return err
	}
	return m.breakPool(l)
}

func layoutOf(dom string) (site.Layout, error) {
	d, err := validate.ParseDomain(dom)
	if err != nil {
		return site.Layout{}, fmt.Errorf("domain demo %q: %w", dom, err)
	}
	return site.LayoutOf(d), nil
}

func input(dom, title string) (core.SiteInput, error) {
	d, err := validate.ParseDomain(dom)
	if err != nil {
		return core.SiteInput{}, fmt.Errorf("domain demo %q: %w", dom, err)
	}
	e, err := validate.ParseEmail(demoEmail)
	if err != nil {
		return core.SiteInput{}, fmt.Errorf("email demo: %w", err)
	}
	return core.SiteInput{Domain: d, Title: title, Email: e}, nil
}

// runAdd chạy PrepareAdd và Execute với Journal đã lùi giờ ago.
func runAdd(ctx context.Context, s *core.Sites, dom, title string, ago time.Duration) (core.SiteAddResult, error) {
	in, err := input(dom, title)
	if err != nil {
		return core.SiteAddResult{}, err
	}
	// Sao Sites để đổi Journal, giữ nguyên bản của người gọi.
	shifted := *s
	shifted.Journal = pastJournal{Journal: s.Journal, ago: ago}
	a, err := shifted.PrepareAdd(ctx, in)
	if err != nil {
		return core.SiteAddResult{}, fmt.Errorf("chuẩn bị tạo site %s: %w", dom, err)
	}
	res, err := a.Execute(ctx, nil)
	if err != nil {
		return res, fmt.Errorf("tạo site %s: %w", dom, err)
	}
	return res, nil
}

// addOK tạo một site và đòi job thành công; ngày tạo của site lùi về ago.
func addOK(ctx context.Context, s *core.Sites, dom, title string, ago time.Duration) error {
	res, err := runAdd(ctx, s, dom, title, ago)
	if err != nil {
		return err
	}
	if res.Report.Job.Status != engine.JobSucceeded {
		return fmt.Errorf("tạo site %s: job %s: %s", dom, res.Report.Job.Status, res.Report.Job.Error)
	}
	st := res.Site
	st.Created = time.Now().UTC().Add(-ago)
	if err := s.Store.SaveSite(ctx, st); err != nil {
		return fmt.Errorf("đặt ngày tạo site %s: %w", dom, err)
	}
	return nil
}

// addRolledBack tạo một lần thêm site thất bại ở bước vhost rồi được hoàn tác.
// Cách gây lỗi giống e2e: một thư mục nằm ở chỗ file vhost nên ghi file không
// được. Xong thì dọn thư mục đó đi, để người dùng demo gõ lại đúng domain này
// trên giao diện thì tạo được (FakeSystem.Fail thì không gỡ ra được).
func addRolledBack(ctx context.Context, m *machine, s *core.Sites, dom string, ago time.Duration) error {
	l, err := layoutOf(dom)
	if err != nil {
		return err
	}
	m.SetDir(l.NginxConf, 0o755, rootOwner)
	res, err := runAdd(ctx, s, dom, "Tin tức", ago)
	if err != nil {
		return err
	}
	if res.Report.Job.Status != engine.JobRolledBack {
		return fmt.Errorf("lần tạo site %s đáng lẽ phải hoàn tác, job là %s", dom, res.Report.Job.Status)
	}
	if err := m.Remove(l.NginxConf); err != nil {
		return fmt.Errorf("dọn chỗ chặn vhost của %s: %w", dom, err)
	}
	return nil
}

// pastJournal ghi job và sự kiện vào Journal thật nhưng lùi mọi mốc thời gian
// đi ago, để lịch sử demo trông như đã tích luỹ qua nhiều ngày. Engine luôn
// dùng đồng hồ thật, và chỉ có chỗ này can thiệp được thời điểm ghi.
type pastJournal struct {
	engine.Journal
	ago time.Duration
}

func (p pastJournal) CreateJob(ctx context.Context, job engine.Job) error {
	job.Started = job.Started.Add(-p.ago)
	return p.Journal.CreateJob(ctx, job)
}

func (p pastJournal) FinishJob(ctx context.Context, job engine.Job) error {
	job.Started = job.Started.Add(-p.ago)
	if !job.Finished.IsZero() {
		job.Finished = job.Finished.Add(-p.ago)
	}
	return p.Journal.FinishJob(ctx, job)
}

func (p pastJournal) AddEvent(ctx context.Context, ev engine.Event) error {
	ev.Time = ev.Time.Add(-p.ago)
	return p.Journal.AddEvent(ctx, ev)
}
