package steps

// Trang mẫu của site PHP trống (M29): một index.php nhỏ để site trả 200 ngay
// sau khi tạo và chủ site biết phải chép mã vào đâu. Chỉ đặt khi public chưa
// có index.php lẫn index.html: không bao giờ ghi đè mã của chủ site. Ghi bằng
// chính user của site (không phải root): thư mục public do user đó điều khiển,
// root mà ghi vào đó thì một symlink đặt sẵn sẽ lái lệnh ghi đi chỗ khác.

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/i18n"
	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/system"
)

// PlainIndex đặt trang mẫu index.php vào public của một site PHP trống.
type PlainIndex struct {
	Layout  site.Layout
	created bool
}

func (s *PlainIndex) path() string { return s.Layout.Public + "/index.php" }

func plainIndexBody(l site.Layout) []byte {
	return []byte(`<?php
// Trang mẫu do TPAN đặt. Thay file này bằng mã của bạn.
header('Content-Type: text/html; charset=utf-8');
?>
<!doctype html>
<html lang="vi"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>` + l.Domain.String() + `</title></head>
<body style="font-family: system-ui, sans-serif; max-width: 640px; margin: 48px auto; padding: 0 16px; line-height: 1.5">
<h1>` + l.Domain.String() + `</h1>
<p>Site đã sẵn sàng, PHP <?php echo PHP_MAJOR_VERSION . '.' . PHP_MINOR_VERSION; ?> đang chạy.</p>
<p>Chép mã nguồn của bạn vào thư mục <code>` + l.Public + `</code> và thay file <code>index.php</code> này.</p>
</body></html>
`)
}

// cmd ghi trang mẫu dưới user của site. dd đọc thẳng stdin được thừa hưởng:
// `install /dev/stdin` thì không được, vì mở lại /dev/stdin (ống của root) dưới
// một user khác bị từ chối (Permission denied; thấy khi thử trên máy thật).
func (s *PlainIndex) cmd() system.Cmd {
	return system.Cmd{Name: "dd", Args: []string{"of=" + s.path(), "status=none"}, Stdin: plainIndexBody(s.Layout), AsUser: s.Layout.User}
}

// chmod đặt quyền 0640 như mọi file mã nguồn tpan đặt cho site.
func (s *PlainIndex) chmod() system.Cmd {
	return system.Cmd{Name: "chmod", Args: []string{"0640", s.path()}, AsUser: s.Layout.User}
}

func (s *PlainIndex) Info() engine.StepInfo {
	c, m := s.cmd(), s.chmod()
	return engine.StepInfo{
		ID:    "site.index:" + s.Layout.Domain.String(),
		Title: i18n.T("Đặt trang mẫu index.php cho "+s.Layout.Domain.String(), "Put a sample index.php in place for "+s.Layout.Domain.String()),
		Why: i18n.T(
			"Site PHP trống chưa có mã nguồn: trang mẫu cho site trả lời ngay và nói rõ chép mã vào đâu. Đã có index.php hay index.html thì bước này bỏ qua, không ghi đè.",
			"An empty PHP site has no code yet: the sample page makes the site answer right away and says where to put your code. If index.php or index.html already exists this step is skipped and nothing is overwritten."),
		Commands: cmdStrings(&c, &m),
	}
}

func (s *PlainIndex) Check(_ context.Context, sys system.System) (bool, error) {
	if err := checkPath("thư mục public của site", s.Layout.Public); err != nil {
		return false, err
	}
	for _, name := range []string{"index.php", "index.html"} {
		_, err := sys.Stat(s.Layout.Public + "/" + name)
		if err == nil {
			return true, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return false, fmt.Errorf("kiểm %s/%s: %w", s.Layout.Public, name, err)
		}
	}
	return false, nil
}

func (s *PlainIndex) Apply(ctx context.Context, sys system.System) error {
	if res, err := sys.Run(ctx, s.cmd()); err != nil {
		return withStdout(fmt.Errorf("đặt trang mẫu %s: %w", s.path(), err), res)
	}
	s.created = true
	if res, err := sys.Run(ctx, s.chmod()); err != nil {
		return withStdout(fmt.Errorf("đặt quyền trang mẫu %s: %w", s.path(), err), res)
	}
	return nil
}

func (s *PlainIndex) Undo(ctx context.Context, sys system.System) error {
	if !s.created {
		return nil
	}
	if res, err := sys.Run(ctx, system.Cmd{Name: "rm", Args: []string{"-f", "--", s.path()}, AsUser: s.Layout.User}); err != nil {
		return withStdout(fmt.Errorf("gỡ trang mẫu %s: %w", s.path(), err), res)
	}
	return nil
}
