package templates

import (
	"bytes"
	"fmt"
	"regexp"

	"github.com/doxuta/tpan/internal/site"
)

// Template cron của M9 (mục 6.5, 6.10 của docs/KE-HOACH.md). Hợp đồng ở
// m9_types.go. Dòng cron do /bin/sh chạy, nên mọi giá trị đi vào file phải qua
// danh sách ký tự cho phép trước khi tới template (text/template không thoát
// ký tự): domain chỉ có a-z 0-9 . -, giờ và phút chỉ là hai chữ số.

// cronTpanBin là đường dẫn tpan trong dòng cron. Cố định vì cron có PATH hẹp và
// vì hợp đồng không cho tham số này (steps cài tpan ở đúng chỗ này).
const cronTpanBin = "/usr/local/bin/tpan"

var (
	// cronDomain: domain đã qua validate.Domain; kiểm lại ở đây vì dòng này
	// là dòng lệnh của shell (lớp chặn cuối, như siteName ở site.go).
	cronDomain = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)
	// cronAt: "HH:MM" 24 giờ, đúng hai chữ số mỗi phần.
	cronAt = regexp.MustCompile(`^([01][0-9]|2[0-3]):([0-5][0-9])$`)
)

type m9Data struct {
	Header string
	Bin    string
	Domain string
	Hour   string
	Minute string
}

func m9Render(name string, d m9Data) ([]byte, error) {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, d); err != nil {
		return nil, fmt.Errorf("render template %s: %w", name, err)
	}
	return buf.Bytes(), nil
}

// SiteCron sinh /etc/cron.d/tpan-<slug> (site.CronFile): một dòng chạy
// `tpan cron run <domain>` mỗi 5 phút bằng root, kèm chú thích giải thích vì sao.
func SiteCron(l site.Layout) ([]byte, error) {
	if l.Domain.IsZero() {
		return nil, fmt.Errorf("sinh cron của site: %w: Layout zero (chưa có domain)", ErrParam)
	}
	if l != site.LayoutOf(l.Domain) {
		return nil, fmt.Errorf("sinh cron của %s: %w: Layout không khớp site.LayoutOf", l.Domain, ErrParam)
	}
	dom := l.Domain.String()
	if !cronDomain.MatchString(dom) {
		return nil, fmt.Errorf("sinh cron của %s: %w: domain có ký tự không cho phép", dom, ErrParam)
	}
	return m9Render("site-cron.tmpl", m9Data{Header: Header, Bin: cronTpanBin, Domain: dom})
}

// BackupCron sinh /etc/cron.d/tpan-backup (site.BackupCronFile): backup mọi site
// mỗi ngày lúc at ("HH:MM", 24 giờ, theo múi giờ của máy).
func BackupCron(at string) ([]byte, error) {
	m := cronAt.FindStringSubmatch(at)
	if m == nil {
		return nil, fmt.Errorf("sinh cron backup: %w: giờ %q phải có dạng HH:MM (24 giờ, đúng hai chữ số mỗi phần, ví dụ 03:30)", ErrParam, at)
	}
	return m9Render("backup-cron.tmpl", m9Data{Header: Header, Bin: cronTpanBin, Hour: m[1], Minute: m[2]})
}
