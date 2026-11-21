package site

import (
	"testing"

	"github.com/doxuta/tpan/internal/validate"
)

func TestLayoutOwns(t *testing.T) {
	d, err := validate.ParseDomain("a.example.com")
	if err != nil {
		t.Fatal(err)
	}
	l := LayoutOf(d)
	cases := []struct {
		p    string
		want bool
	}{
		{"/var/www/a.example.com/public/.htaccess", true},
		{"/var/www/a.example.com/public/wp-config.php", true},
		{"/var/www/a.example.com/x/../public/.htaccess", true},
		{"/var/log/tpan/sites/a.example.com/error.log", true},
		{l.NginxConf, true},
		{l.NginxLink, true},
		{l.ApacheConf, true},
		{l.ApacheLink, true},
		{l.PoolConf("8.3"), true},
		{l.PoolConf("7.4") + ".suspended", true},
		{CronFile(l), true},
		{CronFile(l) + ".suspended", true},

		// Chính neo, chính thư mục log: không "nằm hẳn dưới".
		{"/var/www/a.example.com", false},
		{"/var/www/a.example.com/", false},
		{"/var/log/tpan/sites/a.example.com", false},
		// Tên giống, site khác.
		{"/var/www/a.example.com.evil/public/.htaccess", false},
		{"/var/www/a.example.co/public/.htaccess", false},
		{"/var/www/a.example.com/../b.example.com/public/.htaccess", false},
		{"/var/log/tpan/sites/a.example.com/../b.example.com/error.log", false},
		// Pool: bản rỗng, bản có thêm thành phần, pool của site khác, đuôi lạ.
		{"/etc/php//fpm/pool.d/" + l.Pool + ".conf", false},
		{"/etc/php/8.3/x/fpm/pool.d/" + l.Pool + ".conf", false},
		{"/etc/php/8.3/fpm/pool.d/tpan-b_example_com_abcdef.conf", false},
		{l.PoolConf("8.3") + ".bak", false},
		{"/etc/php/8.3/fpm/pool.d/" + l.Pool, false},
		{"/etc/php/../fpm/pool.d/" + l.Pool + ".conf", false},
		{CronFile(l) + ".bak", false},
		// File của máy.
		{"/etc/nginx/sites-available/tpan-default-ssl.conf", false},
		{"/etc/shadow", false},
		{"", false},
		{"/", false},
		{"var/www/a.example.com/public/.htaccess", false},
	}
	for _, c := range cases {
		if got := l.Owns(c.p); got != c.want {
			t.Errorf("Owns(%q) = %v, muốn %v", c.p, got, c.want)
		}
	}

	// Layout zero: Dir rỗng không được biến thành "/" (mọi thứ).
	var zero Layout
	for _, p := range []string{"/", "/etc/shadow", "/var/www/a.example.com/public/.htaccess", "/etc/php/8.3/fpm/pool.d/.conf", "", ".suspended"} {
		if zero.Owns(p) {
			t.Errorf("Layout zero Owns(%q) = true", p)
		}
	}
}
