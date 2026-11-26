package core

import (
	"errors"
	"strings"
	"testing"

	"github.com/doxuta/tpan/internal/site"
	"github.com/doxuta/tpan/internal/steps"
)

// M36: luật mật khẩu tự đặt. Lời từ chối không bao giờ chứa mật khẩu.
func TestCheckChosenDBPassword(t *testing.T) {
	for pw, ok := range map[string]bool{
		"Ab3-kho-bi-mat":               true,
		"x9.y8:z7@w6^v5~":              true,
		strings.Repeat("a1", 32):       true,  // 64 ký tự
		"Ab3-ngan":                     false, // 8 ký tự
		strings.Repeat("a1", 32) + "x": false, // 65 ký tự
		"co khoang trang 1":            false,
		"co'nhay-don-1234":             false,
		`co\gach-nguoc-123`:            false,
		"co$do-la-12345":               false,
		"co&va;cham-phay1":             false,
		"chicochucai-khongso":          false,
		"123456789012345":              false,
		"mat-khau-alice_shop-1":        false, // chứa tên DB
		"Mật-khẩu-có-dấu-123":          false,
	} {
		err := checkChosenDBPassword(pw, "alice_shop")
		if (err == nil) != ok {
			t.Errorf("%q: lỗi %v, muốn nhận=%v", pw, err, ok)
		}
		var re *RefusedError
		if err != nil && (!errors.As(err, &re) || re.Code != CodeDBPasswordInvalid) {
			t.Errorf("%q: lỗi không phải từ chối %s: %v", pw, CodeDBPasswordInvalid, err)
		}
		if err != nil && (strings.Contains(re.Message.Vi, pw) || strings.Contains(re.Message.En, pw)) {
			t.Errorf("%q: lời từ chối chứa mật khẩu", pw)
		}
	}
}

// DB rời: tạo với mật khẩu tự đặt, rồi đổi sang mật khẩu tự đặt khác. MariaDB
// giả nhận đúng mật khẩu đó; DBAdd và DBPassword không trả mật khẩu lại.
func TestDBRoiMatKhauTuDat(t *testing.T) {
	e := newDBsEnv(t)
	a, err := e.d.PrepareAdd(ctx, DBInput{Owner: dbsUser(t, "alice"), Name: "shop", Password: "Ung-dung-cua-toi-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !a.Chosen || a.Password.Reveal() != "" {
		t.Fatalf("DBAdd: Chosen=%v, Password trả lại %q", a.Chosen, a.Password.Reveal())
	}
	dbsExec(t)(a.Planned, nil)
	if u := e.sim.users["alice_shop"]; u.pw != "Ung-dung-cua-toi-1" {
		t.Fatalf("user DB được tạo với mật khẩu khác mật khẩu đã đặt")
	}

	p, err := e.d.PreparePasswordWith(ctx, "alice_shop", "Doi-sang-cai-nay-2")
	if err != nil {
		t.Fatal(err)
	}
	if !p.Chosen || p.Password.Reveal() != "" {
		t.Fatalf("DBPassword: Chosen=%v, Password trả lại %q", p.Chosen, p.Password.Reveal())
	}
	// Xem trước không được lộ mật khẩu.
	for _, s := range p.Plan.Steps {
		for _, c := range s.Info().Commands {
			if strings.Contains(c, "Doi-sang-cai-nay-2") {
				t.Fatalf("xem trước lộ mật khẩu: %s", c)
			}
		}
	}
	dbsExec(t)(p.Planned, nil)
	if u := e.sim.users["alice_shop"]; u.pw != "Doi-sang-cai-nay-2" {
		t.Fatal("ALTER USER không đặt đúng mật khẩu đã chọn")
	}

	// Không đặt: sinh 32 ký tự và trả để hiện một lần, như trước.
	p2, err := e.d.PreparePassword(ctx, "alice_shop")
	if err != nil || p2.Chosen || len(p2.Password.Reveal()) != 32 {
		t.Fatalf("mật khẩu sinh: %v, Chosen=%v, độ dài %d", err, p2.Chosen, len(p2.Password.Reveal()))
	}

	// Sai luật: từ chối trước khi dựng Plan, máy không đổi gì.
	if _, err := e.d.PreparePasswordWith(ctx, "alice_shop", "ngan1"); dbsCode(err) != CodeDBPasswordInvalid {
		t.Fatalf("mật khẩu ngắn: %v", err)
	}
	if _, err := e.d.PrepareAdd(ctx, DBInput{Owner: dbsUser(t, "alice"), Name: "blog", Password: "co khoang trang 1"}); dbsCode(err) != CodeDBPasswordInvalid {
		t.Fatalf("tạo DB với mật khẩu sai luật: %v", err)
	}
}

// User DB của DB rời đã mất: tạo lại user bằng mật khẩu tự đặt.
func TestDBRoiMatUserTaoLaiBangMatKhauTuDat(t *testing.T) {
	e := newDBsEnv(t)
	e.add(t)
	e.dropOutside("alice_shop", false, true)
	p, err := e.d.PreparePasswordWith(ctx, "alice_shop", "Tao-lai-user-3")
	if err != nil {
		t.Fatal(err)
	}
	if m34StepIDs(p.Planned) != "mariadb.user-recreate" || p.Password.Reveal() != "" {
		t.Fatalf("bước %q, mật khẩu trả lại %q", m34StepIDs(p.Planned), p.Password.Reveal())
	}
	dbsExec(t)(p.Planned, nil)
	if u, ok := e.sim.users["alice_shop"]; !ok || u.pw != "Tao-lai-user-3" {
		t.Fatal("user không được tạo lại với mật khẩu đã đặt")
	}
}

// Site WordPress: từ chối mật khẩu tự đặt (mọi trạng thái của user DB). Site
// PHP trống: nhận, bước ALTER USER mang đúng mật khẩu, không trả lại.
func TestSiteDBMatKhauTuDat(t *testing.T) {
	de := newDBEnv(t, false)
	d := mustDomain(t, dbDom)
	for _, userGone := range []bool{false, true} {
		de.gone(false, userGone)
		_, err := de.b.PrepareSiteDBPasswordWith(ctx, d, "Wordpress-tu-dat-1")
		var re *RefusedError
		if !errors.As(err, &re) || re.Code != CodeDBPasswordWordPress || !strings.Contains(re.Message.Vi, "wp-config.php") {
			t.Fatalf("site WordPress (user mất=%v): %v", userGone, err)
		}
	}

	de.gone(false, false)
	de.mutate(t, func(s *site.Site) { s.App = site.AppPHP })
	p, err := de.b.PrepareSiteDBPasswordWith(ctx, d, "Php-trong-tu-dat-1")
	if err != nil {
		t.Fatal(err)
	}
	if !p.Chosen || p.Password.Reveal() != "" {
		t.Fatalf("Chosen=%v, Password trả lại %q", p.Chosen, p.Password.Reveal())
	}
	st, ok := p.Plan.Steps[0].(*steps.DBUserPassword)
	if !ok || st.Password.Reveal() != "Php-trong-tu-dat-1" {
		t.Fatalf("bước đầu = %T, không mang mật khẩu đã đặt", p.Plan.Steps[0])
	}
	if _, err := de.b.PrepareSiteDBPasswordWith(ctx, d, "Php-trong-tu-dat"); dbsCode(err) != CodeDBPasswordInvalid {
		t.Fatalf("không có chữ số: %v", err)
	}

	// User DB của site PHP trống đã mất: tạo lại bằng mật khẩu tự đặt.
	de.gone(false, true)
	p, err = de.b.PrepareSiteDBPasswordWith(ctx, d, "Php-tao-lai-user-2")
	if err != nil {
		t.Fatal(err)
	}
	rc, ok := p.Plan.Steps[0].(*steps.DBUserRecreate)
	if !ok || rc.Password.Reveal() != "Php-tao-lai-user-2" || p.Password.Reveal() != "" {
		t.Fatalf("bước đầu = %T, mật khẩu trả lại %q", p.Plan.Steps[0], p.Password.Reveal())
	}
}
