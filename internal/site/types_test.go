package site

import (
	"testing"

	"github.com/doxuta/tpan/internal/validate"
)

func TestLayoutOf(t *testing.T) {
	d, err := validate.ParseDomain("Shop.Example.com")
	if err != nil {
		t.Fatal(err)
	}
	l := LayoutOf(d)
	slug := validate.SlugOf(d).String()
	want := map[string]string{
		"User":        "tp_" + slug,
		"DB":          "tp_" + slug,
		"Dir":         "/var/www/shop.example.com",
		"Public":      "/var/www/shop.example.com/public",
		"Tmp":         "/var/www/shop.example.com/tmp",
		"WPConfig":    "/var/www/shop.example.com/public/wp-config.php",
		"LogDir":      "/var/log/tpan/sites/shop.example.com",
		"PHPErrorLog": "/var/log/tpan/sites/shop.example.com/php-error.log",
		"Pool":        "tpan-" + slug,
		"Socket":      "/run/php/tpan-" + slug + "-8.3.sock",
		"NginxConf":   "/etc/nginx/sites-available/tpan-shop.example.com.conf",
		"NginxLink":   "/etc/nginx/sites-enabled/tpan-shop.example.com.conf",
		"ApacheConf":  "/etc/apache2/sites-available/tpan-shop.example.com.conf",
		"ApacheLink":  "/etc/apache2/sites-enabled/tpan-shop.example.com.conf",
		"PoolConf":    "/etc/php/8.3/fpm/pool.d/tpan-" + slug + ".conf",
	}
	got := map[string]string{
		"User": l.User, "DB": l.DB, "Dir": l.Dir, "Public": l.Public, "Tmp": l.Tmp,
		"WPConfig": l.WPConfig, "LogDir": l.LogDir, "PHPErrorLog": l.PHPErrorLog,
		"Pool": l.Pool, "Socket": l.SocketFor("8.3"), "NginxConf": l.NginxConf, "NginxLink": l.NginxLink,
		"ApacheConf": l.ApacheConf, "ApacheLink": l.ApacheLink, "PoolConf": l.PoolConf("8.3"),
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s = %q, muốn %q", k, got[k], w)
		}
	}
	// Tên user Linux tối đa 32 ký tự, tên user MariaDB tối đa 80, tên DB 64.
	if len(l.User) > 32 {
		t.Errorf("User %q dài %d > 32", l.User, len(l.User))
	}
}

func TestLayoutOfZero(t *testing.T) {
	if l := LayoutOf(validate.Domain{}); l != (Layout{}) {
		t.Errorf("Domain zero phải cho Layout zero, được %+v", l)
	}
}
