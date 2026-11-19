package steps

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/engine"
	"github.com/doxuta/tpan/internal/system"
)

const (
	idleConf = "/etc/php/8.3/fpm/pool.d/tpan-idle.conf"
	wwwConf  = "/etc/php/8.3/fpm/pool.d/www.conf"
)

// fakeIdleRender đứng thay templates.PHPIdlePool: nội dung mang số bản để test
// thấy Step đưa đúng bản vào.
func fakeIdleRender(v string) ([]byte, error) {
	return []byte("[tpan-idle]\nlisten = /run/php/tpan-idle-" + v + ".sock\n"), nil
}

func idleStep(v string) *PHPIdlePool {
	return &PHPIdlePool{Version: &v, Render: fakeIdleRender}
}

// phpFresh là thư mục pool như ngay sau `apt-get install php8.3-fpm`: có
// www.conf, dịch vụ đang chạy.
func phpFresh(m *machine) {
	m.SetFile(wwwConf, []byte("[www]\nuser = www-data\npm = dynamic\n"), 0o644, system.Root)
	m.units["php8.3-fpm"] = unitState{Active: true, Enabled: true}
}

func TestPHPIdlePoolLifecycle(t *testing.T) {
	cases := []lifecycle{
		{name: "PHPIdlePool máy mới cài", setup: phpFresh, step: func() engine.Step { return idleStep("8.3") }},
		{
			name: "PHPIdlePool dịch vụ đang tắt",
			setup: func(m *machine) {
				m.SetFile(wwwConf, []byte("[www]\n"), 0o644, system.Root)
				m.units["php8.3-fpm"] = unitState{}
			},
			step: func() engine.Step { return idleStep("8.3") },
		},
		{
			// www.conf đã gỡ từ trước, tpan-idle.conf là bản cũ: Undo phải trả
			// đúng bản cũ chứ không xoá.
			name: "PHPIdlePool thay tpan-idle cũ",
			setup: func(m *machine) {
				m.SetFile(idleConf, []byte("[tpan-idle]\n; bản cũ\n"), 0o600, system.Root)
				m.units["php8.3-fpm"] = unitState{Active: true}
			},
			step: func() engine.Step { return idleStep("8.3") },
		},
	}
	for _, lc := range cases {
		t.Run(lc.name, lc.run)
	}
}

func TestPHPIdlePoolApply(t *testing.T) {
	ctx := context.Background()

	t.Run("ghi tpan-idle trước khi gỡ www, rồi kiểm và reload", func(t *testing.T) {
		m := newMachine(t)
		phpFresh(m)
		if err := idleStep("8.3").Apply(ctx, m); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		snap := m.Snapshot()
		if e, ok := snap[idleConf]; !ok || e.Mode.Perm() != 0o644 || e.Owner != system.Root || !strings.Contains(e.Data, "tpan-idle-8.3.sock") {
			t.Errorf("tpan-idle.conf = %+v, %v", e, ok)
		}
		if _, ok := snap[wwwConf]; ok {
			t.Error("www.conf vẫn còn")
		}
		got := ranStrings(m.Ran())
		want := []string{"php-fpm8.3 -t", "systemctl is-active php8.3-fpm", "systemctl reload php8.3-fpm"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("lệnh = %q, muốn %q", got, want)
		}
	})

	t.Run("dịch vụ tắt thì không reload", func(t *testing.T) {
		m := newMachine(t)
		phpFresh(m)
		m.units["php8.3-fpm"] = unitState{}
		if err := idleStep("8.3").Apply(ctx, m); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if slices.Contains(ranStrings(m.Ran()), "systemctl reload php8.3-fpm") {
			t.Error("reload một dịch vụ đang tắt")
		}
	})

	t.Run("php-fpm -t lỗi thì trả cả hai file, không reload", func(t *testing.T) {
		m := newMachine(t)
		phpFresh(m)
		// Đúng lỗi đã thấy trên máy ảo khi pool.d không còn pool nào.
		m.failCmd([]string{"php-fpm8.3", "-t"}, 78, "ERROR: No pool defined. at least one pool section must be specified in config file")
		before := m.whole()
		err := idleStep("8.3").Apply(ctx, m)
		wantExit(t, err, "kiểm cấu hình sau khi thay pool www")
		if !reflect.DeepEqual(before, m.whole()) {
			t.Error("Apply lỗi mà máy không về như cũ")
		}
		if slices.Contains(ranStrings(m.Ran()), "systemctl reload php8.3-fpm") {
			t.Error("reload sau khi kiểm cú pháp lỗi")
		}
	})

	t.Run("reload lỗi thì trả cả hai file và reload lại", func(t *testing.T) {
		m := newMachine(t)
		phpFresh(m)
		m.failCmd([]string{"systemctl", "reload", "php8.3-fpm"}, 1, "Job for php8.3-fpm.service failed")
		before := m.whole()
		err := idleStep("8.3").Apply(ctx, m)
		wantExit(t, err, "nạp lại dịch vụ")
		if !reflect.DeepEqual(before, m.whole()) {
			t.Error("Apply lỗi mà máy không về như cũ")
		}
		n := 0
		for _, c := range ranStrings(m.Ran()) {
			if c == "systemctl reload php8.3-fpm" {
				n++
			}
		}
		if n != 2 {
			t.Errorf("reload %d lần, muốn 2 (lần đổi và lần trả về)", n)
		}
	})

	t.Run("ghi tpan-idle lỗi thì www.conf còn nguyên", func(t *testing.T) {
		m := newMachine(t)
		phpFresh(m)
		m.Fail("write", idleConf, errors.New("đĩa đầy"))
		before := m.whole()
		if err := idleStep("8.3").Apply(ctx, m); err == nil || !strings.Contains(err.Error(), "đĩa đầy") {
			t.Fatalf("Apply = %v, muốn lỗi ghi", err)
		}
		if !reflect.DeepEqual(before, m.whole()) {
			t.Error("máy đổi sau lỗi ghi")
		}
	})

	t.Run("gỡ www lỗi thì gỡ lại tpan-idle", func(t *testing.T) {
		m := newMachine(t)
		phpFresh(m)
		m.Fail("remove", wwwConf, errors.New("chỉ đọc"))
		before := m.whole()
		if err := idleStep("8.3").Apply(ctx, m); err == nil || !strings.Contains(err.Error(), "chỉ đọc") {
			t.Fatalf("Apply = %v, muốn lỗi gỡ", err)
		}
		if !reflect.DeepEqual(before, m.whole()) {
			t.Error("máy đổi sau lỗi gỡ www.conf")
		}
	})
}

func TestPHPIdlePoolBadParams(t *testing.T) {
	ctx := context.Background()
	str := func(s string) *string { return &s }
	cases := map[string]*PHPIdlePool{
		"Version nil":    {Render: fakeIdleRender},
		"Version rỗng":   {Version: str(""), Render: fakeIdleRender},
		"chèn lệnh":      {Version: str("8.3; rm -rf /"), Render: fakeIdleRender},
		"thoát thư mục":  {Version: str("../8.3"), Render: fakeIdleRender},
		"thay lệnh":      {Version: str("$(id)"), Render: fakeIdleRender},
		"xuống dòng":     {Version: str("8.3\n"), Render: fakeIdleRender},
		"thiếu Render":   {Version: str("8.3")},
		"Render báo lỗi": {Version: str("8.3"), Render: func(string) ([]byte, error) { return nil, errors.New("template hỏng") }},
	}
	for name, s := range cases {
		t.Run(name, func(t *testing.T) {
			m := newMachine(t)
			phpFresh(m)
			before := m.whole()
			if _, err := s.Check(ctx, m); err == nil {
				t.Error("Check không lỗi")
			}
			if err := s.Apply(ctx, m); err == nil {
				t.Error("Apply không lỗi")
			}
			if len(m.Ran()) != 0 {
				t.Errorf("đã chạy lệnh: %q", ranStrings(m.Ran()))
			}
			if !reflect.DeepEqual(before, m.whole()) {
				t.Error("máy bị đổi")
			}
		})
	}
}

func TestPHPIdlePoolInfo(t *testing.T) {
	// Lúc xem trước Plan cài stack, DetectPHP chưa chạy: Info không được đọc
	// file theo đường dẫn chưa biết bản.
	s := &PHPIdlePool{Version: new(string), Render: fakeIdleRender}
	info := s.Info()
	if !strings.Contains(info.Title.Vi, "php<bản>-fpm") || len(info.Files) != 0 {
		t.Errorf("chưa biết bản: Title %q, Files %v", info.Title.Vi, info.Files)
	}
	info = idleStep("8.3").Info()
	if len(info.Files) != 2 || info.Files[0].Path != idleConf || len(info.Files[0].New) == 0 ||
		info.Files[1].Path != wwwConf || !info.Files[1].Delete {
		t.Errorf("Files = %+v", info.Files)
	}
	if info.ID != "php.idle:8.3" || info.Why.Vi == "" || info.Why.En == "" || info.Title.En == "" {
		t.Errorf("Info thiếu: %+v", info)
	}
	if err := idleStep("8.3").Undo(context.Background(), newMachine(t)); !errors.Is(err, errNotApplied) {
		t.Errorf("Undo khi chưa Apply = %v", err)
	}
}
