package steps

// Hợp đồng M6, đợt 0: ba mảnh để core dựng job hàng loạt (mục 7 của
// docs/KE-HOACH.md, engine.Batch). Thân viết ở đợt 1. Tiền tố riêng của file:
// "batch".

import (
	"context"
	"fmt"
	"sync"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

var _ engine.Step = (*ConfigReload)(nil)

// ConfigReload là bước của pha B: kiểm config một lần rồi reload một lần cho
// cả lô. Reload là việc phải làm mỗi lần chạy, không có "trạng thái đã đúng"
// để đọc trên máy, nên Check trả false, trừ đúng một lần ngay sau một Apply
// thành công: engine (runStep) gọi Check lại sau Apply và báo lỗi nếu vẫn
// false. Lần Check đó xoá cờ, để khi pha C chạy lại Commit trên cùng đối tượng
// này thì Check lại là false và reload chạy thật. Apply: chạy Test (nil thì
// bỏ), lỗi thì trả lỗi kèm output; rồi Reload. Undo: không làm gì (NoUndo:
// "reload không có gì để trả lại; các bước ghi file tự trả file cũ"). ID:
// "service.reload:<Service>". Danger false: reload không làm rớt kết nối đang mở.
type ConfigReload struct {
	// Service chỉ để đặt ID và tiêu đề: "nginx", "apache2", "php8.3-fpm".
	Service string
	Test    *system.Cmd
	Reload  *system.Cmd

	// reloaded: Apply vừa xong, Check kế tiếp trả true rồi xoá cờ. Không cần
	// khoá: engine chạy Commit lần lượt, không bao giờ song song.
	reloaded bool
}

func (s *ConfigReload) Info() engine.StepInfo {
	return engine.StepInfo{
		ID: "service.reload:" + s.Service,
		Title: i18n.T(
			"Kiểm cấu hình rồi nạp lại "+s.Service+" một lần cho cả lô",
			"Check the configuration and reload "+s.Service+" once for the whole batch",
		),
		Why: i18n.T(
			"Các bước trước chỉ ghi file, chưa nạp. Bỏ bước này thì "+s.Service+" vẫn chạy cấu hình cũ: site mới không được phục vụ, site đã gỡ vẫn còn. Nạp một lần cho cả lô thay vì mỗi site một lần, và kiểm cú pháp trước để một file sai không làm dịch vụ nạp hỏng.",
			"The earlier steps only wrote files. Without this step "+s.Service+" keeps running the old configuration: new sites are not served and removed ones still are. It reloads once for the whole batch instead of once per site, and checks the syntax first so a bad file never reaches the service.",
		),
		Commands: cmdStrings(s.Test, s.Reload),
		NoUndo: i18n.T(
			"Nạp lại không có gì để trả lại; các bước ghi file tự trả file cũ.",
			"A reload has nothing to give back; the steps that wrote files put the old files back themselves.",
		),
	}
}

func (s *ConfigReload) Check(context.Context, system.System) (bool, error) {
	done := s.reloaded
	s.reloaded = false
	return done, nil
}

func (s *ConfigReload) Apply(ctx context.Context, sys system.System) error {
	what := "ghi cấu hình của cả lô (" + s.Service + ")"
	if err := runTest(ctx, sys, s.Test, what); err != nil {
		return err
	}
	if err := runReload(ctx, sys, s.Reload, what); err != nil {
		return err
	}
	s.reloaded = true
	return nil
}

func (s *ConfigReload) Undo(context.Context, system.System) error { return nil }

// WebReload và PHPReload dựng ConfigReload với đúng lệnh kiểm và lệnh reload
// mà SiteVhost, SitePool dùng (webVhostOf, webPHPCmds của site_web.go). web là
// "nginx" | "apache"; phpVersion dạng "8.3". Tham số sai thì lỗi.
func WebReload(web string) (*ConfigReload, error) {
	// Layout rỗng: chỉ lấy lệnh Test và Reload, không dùng đường dẫn vhost.
	v, err := webVhostOf(site.Layout{}, web)
	if err != nil {
		return nil, fmt.Errorf("dựng bước nạp lại web server: %w", err)
	}
	// Tên dịch vụ lấy từ chính lệnh reload ("systemctl reload apache2") để
	// không có hai chỗ ghi "apache2".
	return &ConfigReload{Service: v.reload.Args[len(v.reload.Args)-1], Test: v.test, Reload: v.reload}, nil
}

func PHPReload(phpVersion string) (*ConfigReload, error) {
	test, reload, err := webPHPCmds(phpVersion)
	if err != nil {
		return nil, fmt.Errorf("dựng bước nạp lại PHP-FPM: %w", err)
	}
	return &ConfigReload{Service: "php" + phpVersion + "-fpm", Test: test, Reload: reload}, nil
}

// WithoutReload bỏ Reload (giữ Test) của các bước *File, *Symlink, *Absent
// trong ss, tại chỗ, rồi trả lại ss. Bước loại khác giữ nguyên. Dùng cho pha
// A: file được ghi và kiểm cú pháp ngay, nhưng dịch vụ chỉ được nạp ở pha B.
// Undo của các bước này khi đó cũng không reload; engine.ExecuteBatch chạy
// lại pha B sau khi hoàn tác một unit ở pha C.
//
// Gọi trước Serial: bước đã bọc thì không còn là *File nên bị bỏ qua.
func WithoutReload(ss []engine.Step) []engine.Step {
	for _, s := range ss {
		switch s := s.(type) {
		case *File:
			s.Reload = nil
		case *Symlink:
			s.Reload = nil
		case *Absent:
			s.Reload = nil
		}
	}
	return ss
}

// Serial bọc một bước để Apply và Undo của nó chạy dưới mu: các unit của job
// hàng loạt chạy song song, nhưng hai việc không được chạy cùng lúc:
//   - useradd/userdel/usermod (khoá /etc/passwd; tiến trình thứ hai lỗi
//     "cannot lock /etc/passwd");
//   - ghi file rồi `nginx -t`/`apachectl configtest`/`php-fpm -t` (kiểm cả
//     config: file dở của site B làm site A lỗi oan).
//
// Info và Check đi thẳng (chỉ đọc). Bước được bọc giữ nguyên ID.
func Serial(mu *sync.Mutex, s engine.Step) engine.Step { return &batchSerial{Step: s, mu: mu} }

// batchSerial nhúng Step nên Info và Check đi thẳng tới bước gốc; chỉ Apply và
// Undo bị ghi đè để lấy khoá.
type batchSerial struct {
	engine.Step
	mu *sync.Mutex
}

func (b *batchSerial) Apply(ctx context.Context, sys system.System) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Step.Apply(ctx, sys)
}

func (b *batchSerial) Undo(ctx context.Context, sys system.System) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Step.Undo(ctx, sys)
}
